#!/usr/bin/env python3
"""
Test 65: end-to-end tests for the dedicated graph server and its client
(rmp task #371).

`rmp graph serve` and `rmp graph client` are exercised here against the
compiled `./bin/rmp`, never against a package under test in isolation: every
scenario below spawns the real server process, signals it, kills it, or
races a client against it exactly as an operator would from a shell. The Go
suites `internal/graphserve` and `internal/graphclient` already prove the
server's and the client's OWN mechanics (the drain's quiescence rule, the
resolution states, the checkpoint watch); this module's job is the one thing
those suites cannot do at all -- prove the behaviour survives the PROCESS
boundary: a real `SIGINT`/`SIGTERM`/`SIGKILL`, a real Unix domain socket, a
real second process racing the first for the store's advisory lock.

SPEC/GRAPH.md "The Dedicated Graph Server" (all nine subsections) and
SPEC/COMMANDS.md "Graph Management" (the Serve/Client option, output,
exit-code and error-case blocks, and "Graph Server Socket Error Lines") are
canonical for every assertion made here; nothing below restates a rule this
module does not also verify against the binary.

## The graph is reached through a running server and through nothing else

`rmp graph execute` is WITHDRAWN. `rmp graph <anything but serve|client>`
exits 127 with `unknown graph subcommand: <name>`, and no invocation of any
kind opens a graph store except a server: `graph client` needs one listening
for every statement, and with nothing there it fails rather than falling back.

Two consequences run through this module. FIRST, the fixtures are the other
way up. A server used to need a store that only `execute` could create, so
every fixture seeded on the direct path and started a server over the result;
now starting a server against a roadmap that has never had a graph is what
CREATES one (SPEC/COMMANDS.md "Serve"), so the server comes first and the seed
goes through the client -- see `seeded_roadmap` below, and
`TestServeLifecycleAndSignals`
.test_serving_a_roadmap_that_has_never_had_a_graph_creates_and_serves_one,
which is the inversion of a case that used to assert the refusal.

SECOND, every case whose subject was `execute` itself is RETIRED rather than
translated, because its subject no longer exists: how `execute` routed to a
running server, and what it did when none answered. The retirements are
recorded where the code was, each naming what covers the behaviour now or
saying plainly that nothing does -- see the block where
`TestExecuteRoutesThroughServer` stood, and the docstrings of
`TestSocketUnreachable` and
`TestGraphClient.test_client_write_through_a_running_server_is_durable`.
A durability check that used to reopen the store with `execute` now reopens it
by starting a further server, which is the same observation made by a process
that was not running when the write was made.

## The seven socket error lines

SPEC/COMMANDS.md "Graph Server Socket Error Lines" publishes seven strings as
PROSE rather than in a table or a fenced block -- deliberately, per rmp task
#366 NOTE #263, because `test_55_error_string_parity.py`'s corpus extraction
only recognises table cells and fenced blocks, and its own
`test_zz_coverage_report` fails on any published string neither reached nor
exempted. Driving all seven here is this module's own obligation; moving them
into the tables (so `test_55` picks them up automatically) is a follow-up
change to SPEC/COMMANDS.md and test_55 together, and is reported rather than
made here, because SPEC/ belongs to `specification-manager`. Each of the
seven has its own test below, cross-referenced by the line's own name:

  1. "already serving"   -- TestServeFlagsAndErrorCases.test_already_serving_...
  2. "cannot bind"        -- TestServeFlagsAndErrorCases.test_cannot_bind_...
  3. "cannot take the graph store lock" (the LOCK line)
                          -- TestServeFlagsAndErrorCases.test_second_serve_...
  4. "no graph server is listening"
                          -- TestGraphClient.test_no_server_listening_... (x2:
                             a socket that never existed, and a stale one), and
                             TestGraphClient.test_client_without_socket_flag_...
                             (a third state: a server serving elsewhere)
  5. "graph server unreachable"
                          -- TestSocketUnreachable (the client, the one surface
                             that resolves a socket)
  6. "the connection ... was lost"
                          -- TestServerConnectionFailureModes
                             .test_connection_lost_after_statement_sent
  7. "did not answer within ... ; the statement's outcome is unknown"
                          -- TestServerConnectionFailureModes
                             .test_server_unanswered_within_the_backstop_deadline

Every failure path below asserts the FULL published line (after substituting
the one placeholder each carries -- the resolved socket path) AND the exit
code, never the code alone, per this task's acceptance criteria.

## The two expensive reproductions, and the one calibration that survives

Lines 6 and 7 both need a statement that is still running on the server when
the test acts on that server, which a cartesian-product write against a seeded
store supplies.

Line 6 needs no more than that, and its number IS a calibration against this
repository's own dev machine: a 300-node product hit by a per-row write
(`CREATE (:Anomaly {...})`) reliably still runs one second after being
launched, which is the window the kill needs. A much slower or faster machine
would want a different number, which is precisely the residual SPEC/GRAPH.md
"Statement Time Budget" names.

Line 7 used to be reached the same way, by requiring the SERVER-side cut-and-undo
of a 3000-node product to outlast the caller's own 7.5s backstop. It no longer
is. That overrun is a quantity the specification bounds in NEITHER direction --
"a property of the statement rather than a constant", with "nothing measured
establishes a ceiling" and no floor established either -- and measured 48 times
on this machine it landed on both sides of the backstop, failing the case in 16
of those runs (rmp task #404; the distribution is recorded at
BACKSTOP_FREEZE_DELAYS_S). The case now freezes the server with SIGSTOP while
the statement is in flight: the signal is uncatchable, so the server provably
cannot answer, while the connection, the session and the socket are all left
intact -- which is the state rule 7 actually specifies for that line, reached by
a mechanism with margins in seconds instead of a race decided in tenths.

## What this module deliberately does not chase

Four open defects are named in SPEC/GRAPH.md and in rmp tasks #380, #381,
#382 and #384: unbounded peak RSS on a cut write, a `SET` on a `MERGE`-created
relationship being discarded, a temporal-format contradiction, and a ~1%
retry-ladder exhaustion under sixteen-way single-node contention. Where a
test here touches the same mechanics (the cut-write undo replay in
particular), it asserts what the product does TODAY -- eventual correct,
durable, drained shutdown -- and does not encode any of those four defects as
though they were correct behaviour, per this task's own scope.
"""

import inspect
import json
import os
import re
import signal
import subprocess
import sys
import tempfile
import threading
import time

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import (GraphServeProcess, GroadmapTestBase,
                             assert_graph_write_shape,
                             measure_socket_path_bound)


EXIT_OK = 0
EXIT_DATABASE = 1
EXIT_INVALID_INPUT = 2
EXIT_NO_ROADMAP = 3
EXIT_NOT_FOUND = 4
EXIT_VALIDATION = 6

# SPEC/GRAPH.md "Server Resolution": the whole probe carries the project's
# backoff total as its deadline.
PROBE_DEADLINE_S = 2.5
# SPEC/GRAPH.md "Server Options" / "Lock Contention": the wait budget a
# caller (and the drain) survive a statement against -- statement budget (5s)
# plus backoff total (2.5s).
WAIT_BUDGET_S = 7.5
STATEMENT_BUDGET_S = 5.0

# The three points, in seconds after the client is launched, at which the
# in-flight server is frozen so that the caller's own backstop is the thing that
# answers. Used by
# TestServerConnectionFailureModes.test_server_unanswered_within_the_backstop_deadline.
#
# WHAT THE OLD SHAPE MEASURED, and why it had to go. Before rmp task #404 that
# case required the server's own undo replay to outlast the 7.5s backstop
# unaided. Driven 48 times on this machine against the binary at 77d51de, with
# nothing changed between runs, the server answered inside the backstop -- at
# 7.22s to 7.38s, so the case FAILED -- 16 times, and after it in the other 32,
# by no more than 0.16s to 0.77s. The whole distribution fits inside a band about
# one second wide with the 7.5s boundary in the middle of it, and which side a
# run lands on is decided by the ambient state of the machine rather than by
# anything the case controls: two batches minutes apart, at the same load
# average, produced 16 failures in 18 and then 0 in 12.
#
# WHY A HEAVIER SEED WAS NOT THE ANSWER. The seed is the only knob that moves the
# replay at all, and it moves it slowly: the same statement was answered at
# 5.94-6.11s over 600 seed nodes, 6.52-6.90s over 1500, 7.22-8.27s over 3000,
# 8.27-9.07s over 6000 and 10.08-11.09s over 12000, while widening the product to
# four dimensions over 3000 nodes moved nothing at all (7.36-7.66s, still astride
# the boundary). A bigger seed therefore buys a MEASURED margin on THIS machine
# for a quantity SPEC/GRAPH.md "Statement Time Budget" refuses to bound -- the
# overrun is "a property of the statement rather than a constant", "nothing
# measured establishes a ceiling", and nothing establishes a floor either, which
# is the half a case asserting the backstop needs. It would move the boundary,
# not remove it.
#
# WHAT THE FREEZE GUARANTEES INSTEAD. SIGSTOP is uncatchable, so from the instant
# it is delivered the server cannot answer, and neither margin is a measurement
# of an unbounded quantity any more. On the LATE side the server cannot answer
# before its own statement budget cuts the write, 5s by SPEC/GRAPH.md "Statement
# Time Budget", and the earliest answer observed for this statement across those
# 48 runs was 7.22s: freezing at 2.0s leaves 3.0s of specified margin and 5.2s of
# measured margin. On the EARLY side the statement has to have been SENT, and a
# whole first `rmp graph client` round trip against a freshly started server --
# process spawn, roadmap open, socket resolution, connect, handshake, RUN,
# answer -- measured 8.0ms to 9.1ms over 10 runs: freezing at 0.4s leaves about
# forty times that. The early side is self-proving as well, because a freeze that
# landed before the RUN was written would produce the "unreachable" line rather
# than the one asserted here.
#
# WHAT THE REPETITION IS FOR. Not a rate: there is no longer one to sample. The
# three delays sample the WINDOW -- at its start, at its middle, and at a point
# where the forward pass has written enough that the undo replay after the resume
# is the longest of the three -- so a change that narrowed that window from
# either end fails the case instead of quietly shrinking a margin nobody reads.
# COST IS MEASURED RATHER THAN FEARED: one iteration is a real server, a real
# 7.5s wait, a real resumed undo and a real drain, and the three cost 8.4s, 9.2s
# and 9.2s.
BACKSTOP_FREEZE_DELAYS_S = (0.4, 1.0, 2.0)

# A Unix domain socket path is bounded by sun_path, and a HOME rooted under a
# long build/session directory blows past it the moment a roadmap name is
# appended (rmp task #367 FINDING #266, measured there against exactly this
# failure). tempfile.mkdtemp() defaults to $TMPDIR or /tmp, which is short;
# this guard turns a violation into a diagnosable setup failure instead of a
# mysterious "bind: invalid argument" deep inside a signal-handling test.
#
# The bound is MEASURED rather than written down. This module used to declare
# 108 -- Linux's sun_path size -- which is right here and too PERMISSIVE on
# macOS, FreeBSD and OpenBSD, where the bound is 103 (rmp task #412). A guard
# that is too permissive is worse than none: it passes, and then the failure it
# exists to explain arrives anyway, with the errno it exists to replace.
# base_test owns the one measurement; the result is cached because binding a
# few hundred sockets once per module is cheap and once per call is not.
_measured_bound = None


def _max_sun_path() -> int:
    """The greatest socket-path length this platform binds, measured once."""
    global _measured_bound
    if _measured_bound is None:
        probe = tempfile.mkdtemp(prefix="sunpath-")
        try:
            _measured_bound = measure_socket_path_bound(probe)
        finally:
            os.rmdir(probe)
    return _measured_bound


def _assert_socket_path_fits(path: str):
    """Guard the trap SPEC/GRAPH.md documents: a derived socket path over the
    platform's bound fails to bind for a reason ("bind: invalid argument") that
    gives no hint the path itself is the cause. Failing here, with the path and
    its length spelled out, is what makes that diagnosable instead of
    mysterious.
    """
    encoded = os.fsencode(path)
    bound = _max_sun_path()
    assert len(encoded) <= bound, (
        f"derived socket path is {len(encoded)} bytes, over this platform's "
        f"measured AF_UNIX sun_path bound of {bound}: {path!r}. The harness "
        f"must use a short HOME (tempfile.mkdtemp() under $TMPDIR/tmp) and a "
        f"short roadmap name."
    )


class GraphServerTestBase:
    """Shared fixture for every class below, and for the modules that import it
    (test_69_graph_field_length.py): a fresh temporary HOME (short, per the
    sun_path guard) and bookkeeping that force-kills any server a test spawned
    but did not itself stop, so one failing assertion never leaks a process
    into the rest of the suite's run.

    Its body is DELEGATION, not implementation. The process lifecycle, the
    drained pipes and the plain invocation all live on GroadmapTestBase now
    (base_test.py, "The graph server, and reaching a graph through it"),
    because every module that touches a graph needs them rather than only the
    one that first did. What stays here is the NAME and the shape the classes
    below -- and test_69 -- are written against, plus the three things
    base_test has no reason to carry: an invocation launched without being
    waited for, and the two socket assertions.
    """

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        # GroadmapTestBase.teardown() kills every server started through it
        # BEFORE it removes the temporary HOME, which is the order that
        # matters: a live server holds a store open underneath that HOME.
        self.test.teardown()

    # ---- server lifecycle -------------------------------------------------

    def start_server(self, roadmap: str, socket_path: str = None, timeout: float = 15.0):
        """Start a graph server for `roadmap`, block until it has announced its
        socket, track it for teardown, and return the started
        GraphServeProcess.
        """
        return self.test.start_graph_server(
            roadmap, socket_path=socket_path, timeout=timeout)

    def spawn_server(self, roadmap: str, socket_path: str = None) -> GraphServeProcess:
        """A tracked but NOT started server, for the cases whose subject is the
        start itself failing.

        Tracking it on the harness rather than in a second list of this
        fixture's own keeps teardown in one place: whatever is still alive when
        a test ends is killed there, however it was created.
        """
        server = GraphServeProcess(self.test, roadmap, socket_path=socket_path)
        self.test._graph_servers().append(server)
        return server

    def seeded_roadmap(self, name: str, seed_query: str) -> str:
        """Create `name`, put `seed_query` into its graph, and return the name
        with nothing left running.

        WHY THE ORDER USED TO BE THE OTHER WAY ROUND. This ran its seed through
        `rmp graph execute` because no server could have run it: `serve` refused
        a roadmap with no graph store, and `execute` was the only thing that
        created one. The store therefore had to be materialised on the direct
        path before a server could be started over it, and that ordering is the
        only reason this method reached for `execute` at all.

        NEITHER HALF OF THAT HOLDS ANY MORE. `rmp graph execute` is withdrawn,
        and starting a server is now what CREATES a graph: `serve` against a
        roadmap that has never had one creates ~/.roadmaps/<name>/graph/ and
        serves it empty (SPEC/COMMANDS.md "Serve"). The chicken-and-egg has
        dissolved -- the server comes first and the seed goes through the
        client.

        The server is stopped again before returning, so the contract every
        caller was written against is unchanged: a roadmap whose graph carries
        the seed, and no process holding its store. A caller that wants one
        running calls start_server() itself, exactly as it always did.
        """
        self.test.create_roadmap(name)
        server = self.start_server(name)
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", name, "--query", seed_query])
        assert rc == EXIT_OK, (
            f"seeding {name!r} with {seed_query!r} failed: "
            f"exit={rc} out={out!r} err={err!r}")
        stop_rc = server.stop(signal.SIGINT)
        assert stop_rc == EXIT_OK, (
            f"the seeding server for {name!r} did not stop cleanly (exit="
            f"{stop_rc}), so the seed may not have been checkpointed; "
            f"stderr={server.stderr_text()!r}")
        return name

    def read_through_a_fresh_server(self, roadmap: str, query: str):
        """Run `query` against `roadmap` through a server started for the
        occasion and stopped again, and return its parsed result.

        This is how a durability check is made now. With no direct path left,
        "what is actually on disk" is observed by REOPENING the store in a new
        process -- which is precisely what starting a server is -- rather than
        by an invocation that opened the store itself. It is the stronger of the
        two observations: the process doing the reading was not running when the
        write was made and inherits nothing from the one that made it.
        """
        server = self.start_server(roadmap)
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query", query])
        stop_rc = server.stop(signal.SIGINT)
        assert rc == EXIT_OK, (
            f"reading {query!r} back through a fresh server failed: "
            f"exit={rc} stdout={out!r} stderr={err!r}")
        assert stop_rc == EXIT_OK, (
            f"the reading server did not stop cleanly; got {stop_rc}, "
            f"stderr={server.stderr_text()!r}")
        return json.loads(out)

    # ---- process-level invocations -----------------------------------

    def run_cli(self, args, stdin_text: str = None, timeout: float = 20.0):
        """One `./bin/rmp` invocation against this fixture's HOME, returning
        (exit_code, stdout, stderr). See GroadmapTestBase.run_cli for why the
        graph modules use this rather than run_cmd.
        """
        return self.test.run_cli(args, stdin_text=stdin_text, timeout=timeout)

    def run_cli_async(self, args, stdin_text: str = None):
        """Launch an `./bin/rmp` invocation without waiting for it, returning
        the Popen. Used by the timing-sensitive scenarios that must observe
        or act on the SERVER while a CLIENT invocation is still in flight.
        """
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        stdin_arg = subprocess.PIPE if stdin_text is not None else subprocess.DEVNULL
        proc = subprocess.Popen(
            [self.test.cli_path] + args,
            stdin=stdin_arg,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            env=env,
        )
        if stdin_text is not None:
            proc.stdin.write(stdin_text)
            proc.stdin.close()
        return proc

    def default_socket_path(self, roadmap: str) -> str:
        return self.test.default_socket_path(roadmap)

    # ---- shared assertions -------------------------------------------

    def assert_socket_mode_0600(self, socket_path: str):
        mode = os.stat(socket_path).st_mode & 0o777
        assert mode == 0o600, f"{socket_path}: mode {oct(mode)}, want 0600"

    def assert_is_socket(self, path: str):
        assert os.path.exists(path), f"{path}: does not exist"
        import stat as statmod
        assert statmod.S_ISSOCK(os.stat(path).st_mode), f"{path}: not a socket"


class TestSocketPathAndPermissions(GraphServerTestBase):
    """SPEC/GRAPH.md "Socket Path and Permissions": the default derivation,
    the `--socket` override, the explicit 0600 mode, and the 0700 roadmap
    home as the outer fence -- driven against a real bind, not asserted from
    the derivation function alone.
    """

    def test_default_socket_path_is_derived_from_the_roadmap(self):
        roadmap = self.seeded_roadmap(
            "payments-gateway",
            "CREATE (:Component {key:'billing-api', language:'go'})",
        )
        expected = self.default_socket_path(roadmap)
        _assert_socket_path_fits(expected)

        server = self.start_server(roadmap)
        assert server.socket == expected, (
            f"a caller supplying no --socket must see the derived path; "
            f"got {server.socket!r}, want {expected!r}"
        )
        self.assert_is_socket(server.socket)
        self.assert_socket_mode_0600(server.socket)

        roadmap_home = self.test.home_dir / ".roadmaps" / roadmap
        home_mode = os.stat(roadmap_home).st_mode & 0o777
        assert home_mode == 0o700, (
            f"the roadmap home is the outer fence and must stay 0700; got "
            f"{oct(home_mode)}"
        )

        rc = server.stop(signal.SIGINT)
        assert rc == EXIT_OK, f"clean stop must exit 0; got {rc}, stderr={server.stderr_text()!r}"
        assert not os.path.exists(server.socket), (
            "the socket file must be gone once the server has stopped "
            "(SPEC/GRAPH.md \"Socket Path and Permissions\" rule 7)"
        )

    def test_custom_socket_path_via_flag_overrides_the_derivation(self):
        roadmap = self.seeded_roadmap(
            "identity-service",
            "CREATE (:Component {key:'auth-api', language:'go'})",
        )
        custom = str(self.test.home_dir / "sockets" / "identity-graph.sock")
        os.makedirs(os.path.dirname(custom), exist_ok=True)
        _assert_socket_path_fits(custom)

        server = self.start_server(roadmap, socket_path=custom)
        assert server.socket == custom, (
            f"--socket must be followed literally; got {server.socket!r}, want {custom!r}"
        )
        assert server.socket != self.default_socket_path(roadmap), (
            "the derived path must NOT have been used once --socket was given"
        )
        self.assert_is_socket(custom)
        self.assert_socket_mode_0600(custom)

        rc = server.stop(signal.SIGTERM)
        assert rc == EXIT_OK
        assert not os.path.exists(custom)

    def test_socket_mode_is_explicit_0600_not_the_process_umask(self):
        """SPEC/GRAPH.md rule 3: the mode is set explicitly rather than left
        to the process umask. A permissive umask (0000, "everyone may write")
        must not leak into the socket's own mode.
        """
        roadmap = self.seeded_roadmap(
            "notification-hub",
            "CREATE (:Component {key:'dispatcher', language:'go'})",
        )
        old_umask = os.umask(0o000)
        try:
            server = self.start_server(roadmap)
            self.assert_socket_mode_0600(server.socket)
            server.stop(signal.SIGINT)
        finally:
            os.umask(old_umask)


class TestServeLifecycleAndSignals(GraphServerTestBase):
    """SPEC/GRAPH.md "Server Startup" and "Server Shutdown and the Drain":
    the startup announcement, the two expected engine warnings, both
    catchable signals stopping the server gracefully, and the CREATION of a
    graph for a roadmap that has never had one.
    """

    def test_startup_announces_socket_and_warns_about_auth_and_tls(self):
        roadmap = self.seeded_roadmap(
            "observability-platform",
            "CREATE (:Component {key:'metrics-collector', language:'go'})",
        )
        server = self.start_server(roadmap)
        assert server.socket == self.default_socket_path(roadmap)

        stderr = server.stderr_text()
        assert re.search(r"(?i)auth", stderr), (
            f"the no-authentication engine warning is expected on stderr at "
            f"startup (SPEC/GRAPH.md \"Socket Path and Permissions\" rule 5); "
            f"got {stderr!r}"
        )
        assert re.search(r"(?i)tls|transport security", stderr), (
            f"the no-transport-security engine warning is expected on stderr "
            f"at startup (rule 6); got {stderr!r}"
        )

        rc = server.stop(signal.SIGINT)
        assert rc == EXIT_OK

    def test_sigint_drains_checkpoints_and_exits_0(self):
        roadmap = self.seeded_roadmap(
            "inventory-service",
            "CREATE (:Component {key:'stock-ledger', language:'go'})",
        )
        server = self.start_server(roadmap)
        rc = server.stop(signal.SIGINT, timeout=15.0)
        assert rc == EXIT_OK, (
            f"SIGINT must stop the server gracefully with exit 0 "
            f"(SPEC/ARCHITECTURE.md \"Exit Codes of the Graph Server and "
            f"Client\": a graceful stop is 0, not 130); got {rc}, "
            f"stderr={server.stderr_text()!r}"
        )
        assert not os.path.exists(server.socket)

    def test_sigterm_drains_checkpoints_and_exits_0(self):
        roadmap = self.seeded_roadmap(
            "billing-reconciler",
            "CREATE (:Component {key:'ledger-sync', language:'go'})",
        )
        server = self.start_server(roadmap)
        rc = server.stop(signal.SIGTERM, timeout=15.0)
        assert rc == EXIT_OK, f"SIGTERM must also stop gracefully; got {rc}"
        assert not os.path.exists(server.socket)

    def test_serving_a_roadmap_that_has_never_had_a_graph_creates_and_serves_one(self):
        """SPEC/COMMANDS.md "Serve": starting a server is how a roadmap graph
        comes into being, and nothing else creates one. Against a roadmap that
        has never had a graph, serve creates ~/.roadmaps/<name>/graph/ with mode
        0700 and serves it EMPTY.

        This case used to assert the opposite -- that such a serve was refused,
        exit 1, "no graph store", no directory created -- because `rmp graph
        execute` was then the only thing that could materialise a store. That
        subcommand is withdrawn, and with it the only other way a graph could
        have come into existence: a serve that refused here would leave the
        roadmap's graph unreachable for ever.

        What is created is asserted to be a REAL store rather than a scratch
        one: it answers as empty, it takes a write, and the write is still there
        when a later process reopens it.
        """
        roadmap = self.test.create_roadmap("greenfield-project")
        graph_dir = self.test.home_dir / ".roadmaps" / roadmap / "graph"
        assert not graph_dir.exists(), (
            "the fixture is only meaningful over a roadmap that has never had a "
            "graph; this one already has one"
        )

        server = self.start_server(roadmap)
        assert server.socket == self.default_socket_path(roadmap)
        self.assert_is_socket(server.socket)

        assert graph_dir.is_dir(), (
            "serving a roadmap with no graph must CREATE its graph directory "
            "(SPEC/COMMANDS.md \"Serve\"); nothing else does"
        )
        graph_mode = os.stat(graph_dir).st_mode & 0o777
        assert graph_mode == 0o700, (
            f"the created graph directory must be 0700; got {oct(graph_mode)}"
        )

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query", "MATCH (n) RETURN count(n)"]
        )
        assert rc == EXIT_OK, f"the created graph must answer; exit={rc} err={err!r}"
        assert json.loads(out) == {"columns": ["count(n)"], "rows": [[0]]}, (
            f"a graph created by serve is served EMPTY; got {out!r}"
        )

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query",
             "CREATE (:Component {key:'greenfield-api', language:'go'})"]
        )
        assert rc == EXIT_OK, f"the created graph must take a write; err={err!r}"
        assert_graph_write_shape(
            json.loads(out), "the first write into a graph serve created",
            {"nodesCreated": 1, "propertiesWritten": 2, "labelsAdded": 1})

        rc = server.stop(signal.SIGINT)
        assert rc == EXIT_OK, f"got {rc}, stderr={server.stderr_text()!r}"
        assert not os.path.exists(server.socket)

        assert self.read_through_a_fresh_server(
            roadmap, "MATCH (c:Component) RETURN c.key"
        ) == {"columns": ["c.key"], "rows": [["greenfield-api"]]}, (
            "what serve created is a durable store, not a scratch one: the "
            "write must still be there when a later process reopens it"
        )


class TestServeFlagsAndErrorCases(GraphServerTestBase):
    """SPEC/COMMANDS.md "Serve Options" / "Serve Error Cases", and three of
    the seven socket error lines: the LOCK line (a second serve against the
    same roadmap), the "already serving" line (a --socket collision across
    two DIFFERENT roadmaps, engineered so the lock is free and the socket
    probe is what fires -- see rmp task #367 TEST #269), and the "cannot
    bind" line.
    """

    def test_roadmap_not_specified_exits_3(self):
        rc, out, err = self.run_cli(["graph", "serve"])
        assert rc == EXIT_NO_ROADMAP, f"got {rc}"
        assert err.strip().startswith(
            "Error: no roadmap selected: use -r <name> or --roadmap <name>"
        ), err
        assert out == ""

    def test_roadmap_not_found_exits_4(self):
        rc, out, err = self.run_cli(["graph", "serve", "-r", "no-such-roadmap"])
        assert rc == EXIT_NOT_FOUND, f"got {rc}"
        assert err.splitlines()[0] == 'Error: resource not found: roadmap "no-such-roadmap" not found', err
        assert out == ""

    def test_unknown_flag_exits_2(self):
        roadmap = self.seeded_roadmap(
            "release-orchestrator",
            "CREATE (:Component {key:'deploy-pipeline', language:'go'})",
        )
        rc, out, err = self.run_cli(["graph", "serve", "-r", roadmap, "--bogus"])
        assert rc == EXIT_INVALID_INPUT, f"got {rc}, stderr={err!r}"
        assert "unknown flag: --bogus" in err, err
        assert out == ""

    def test_unexpected_positional_argument_exits_2(self):
        roadmap = self.seeded_roadmap(
            "search-indexer",
            "CREATE (:Component {key:'crawler', language:'go'})",
        )
        rc, out, err = self.run_cli(["graph", "serve", "-r", roadmap, "start"])
        assert rc == EXIT_INVALID_INPUT, f"got {rc}, stderr={err!r}"
        assert 'unexpected argument "start"' in err, err
        assert out == ""

    def test_socket_flag_empty_value_exits_2(self):
        roadmap = self.seeded_roadmap(
            "search-indexer-2",
            "CREATE (:Component {key:'crawler', language:'go'})",
        )
        rc, out, err = self.run_cli(["graph", "serve", "-r", roadmap, "--socket", ""])
        assert rc == EXIT_INVALID_INPUT, f"got {rc}, stderr={err!r}"
        assert "required parameter missing: --socket" in err, err
        assert out == ""

    def test_help_flag_prints_usage_and_exits_0(self):
        rc, out, err = self.run_cli(["graph", "serve", "-h"])
        assert rc == EXIT_OK, f"got {rc}"
        assert "Usage: rmp graph serve -r <roadmap> [--socket <path>]" in out, out
        assert "--socket <path>" in out

    def test_second_serve_against_the_same_roadmap_gets_the_lock_line(self):
        """SPEC/GRAPH.md "Server Startup" step 2/3: the lock is taken BEFORE
        the socket is probed, so a second `serve` against the SAME roadmap is
        refused by the LOCK, never by the "already serving" probe -- and the
        incumbent's own socket is left untouched (rmp task #367 TEST #269).
        """
        roadmap = self.seeded_roadmap(
            "checkout-service",
            "CREATE (:Component {key:'cart-api', language:'go'})",
        )
        incumbent = self.start_server(roadmap)

        started = time.monotonic()
        rc, out, err = self.run_cli(
            ["graph", "serve", "-r", roadmap], timeout=WAIT_BUDGET_S + 10,
        )
        elapsed = time.monotonic() - started

        assert rc == EXIT_DATABASE, f"got {rc}, stderr={err!r}"
        expected = (
            f'Error: graph store error: cannot take the graph store lock for '
            f'roadmap "{roadmap}": another rmp graph serve may already be '
            f'running for it'
        )
        assert err.splitlines()[0] == expected, f"got {err.splitlines()[0]!r}\nwant {expected!r}"
        assert out == ""
        assert elapsed >= WAIT_BUDGET_S - 0.5, (
            f"the refusal must come from the BOUNDED WAIT for the lock "
            f"(SPEC/GRAPH.md \"Lock Contention\"), so it must not return "
            f"appreciably before {WAIT_BUDGET_S}s; took {elapsed:.2f}s"
        )

        assert incumbent.is_alive(), "the incumbent server must survive the refused challenger"
        self.assert_is_socket(incumbent.socket)
        self.assert_socket_mode_0600(incumbent.socket)

        rc2, out2, err2 = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query", "MATCH (n) RETURN count(n)"]
        )
        assert rc2 == EXIT_OK, (
            f"the incumbent must still be answering after the refused "
            f"challenger; exit={rc2} err={err2!r}"
        )

        rc3 = incumbent.stop(signal.SIGINT)
        assert rc3 == EXIT_OK

    def test_already_serving_line_when_a_socket_is_shared_across_roadmaps(self):
        """Engineered per rmp task #367 TEST #269's own recipe: two DIFFERENT
        roadmaps (so their store locks never collide) share one --socket
        path. The first server binds it; the second's own lock is free, so it
        reaches step 3 and is refused by the LIVE SOCKET PROBE instead --
        which is the only way to provoke this line rather than the lock one.
        """
        roadmap_a = self.seeded_roadmap(
            "payments-gateway-2",
            "CREATE (:Component {key:'billing-api', language:'go'})",
        )
        roadmap_b = self.seeded_roadmap(
            "identity-service-2",
            "CREATE (:Component {key:'auth-api', language:'go'})",
        )
        shared_socket = str(self.test.home_dir / "shared" / "graph.sock")
        os.makedirs(os.path.dirname(shared_socket), exist_ok=True)
        _assert_socket_path_fits(shared_socket)

        server_a = self.start_server(roadmap_a, socket_path=shared_socket)
        assert server_a.socket == shared_socket

        challenger = self.spawn_server(roadmap_b, socket_path=shared_socket)
        raised = False
        try:
            challenger.start(timeout=PROBE_DEADLINE_S + 5.0)
        except AssertionError:
            raised = True
        assert raised, "the challenger must not announce a socket of its own"
        assert challenger.proc.returncode == EXIT_DATABASE, (
            f"got {challenger.proc.returncode}"
        )
        expected = f"Error: graph server error: a graph server is already serving {shared_socket}"
        assert expected in challenger.stderr_text(), challenger.stderr_text()

        assert server_a.is_alive(), "the incumbent must be untouched by the refused challenger"
        self.assert_is_socket(shared_socket)
        rc2, _, err2 = self.run_cli(
            ["graph", "client", "-r", roadmap_a, "--socket", shared_socket,
             "--query", "MATCH (n) RETURN count(n)"]
        )
        assert rc2 == EXIT_OK, f"incumbent unreachable after refusal: {err2!r}"

        server_a.stop(signal.SIGINT)

    def test_cannot_bind_line_when_the_socket_directory_does_not_exist(self):
        roadmap = self.seeded_roadmap(
            "fulfillment-service",
            "CREATE (:Component {key:'warehouse-router', language:'go'})",
        )
        bad_socket = str(self.test.home_dir / "no-such-directory" / "graph.sock")
        _assert_socket_path_fits(bad_socket)

        server = self.spawn_server(roadmap, socket_path=bad_socket)
        raised = False
        try:
            server.start(timeout=5.0)
        except AssertionError:
            raised = True
        assert raised
        assert server.proc.returncode == EXIT_DATABASE, f"got {server.proc.returncode}"
        prefix = f"Error: graph server error: cannot bind {bad_socket}: "
        assert server.stderr_text().startswith(prefix), (
            f"got {server.stderr_text()!r}, want prefix {prefix!r}"
        )
        tail = server.stderr_text()[len(prefix):].splitlines()[0]
        assert tail.strip() != "", "the OS diagnostic tail must be non-empty"


class TestGraphClient(GraphServerTestBase):
    """SPEC/COMMANDS.md "Client Options" / "Client Output" / "Client Exit
    Codes" / "Client Error Cases", plus the "no graph server is listening"
    socket line in both of the states it covers (rule: "it covers both the
    socket that does not exist and the socket file a killed server left
    behind, because the two are one condition for this subcommand").
    """

    def test_client_read_through_a_running_server(self):
        roadmap = self.seeded_roadmap(
            "payments-gateway-3",
            "CREATE (:Component {key:'billing-api', language:'go', "
            "owner:'platform-team'})",
        )
        server = self.start_server(roadmap)
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query",
             "MATCH (c:Component {key:'billing-api'}) RETURN c.owner"]
        )
        assert rc == EXIT_OK, f"got {rc}, stderr={err!r}"
        body = json.loads(out)
        assert body == {"columns": ["c.owner"], "rows": [["platform-team"]]}, body
        server.stop(signal.SIGINT)

    def test_client_write_through_a_running_server_is_durable(self):
        """The write must actually reach the SERVER's store (not merely
        return success): read it back through a SECOND client invocation,
        then again after that server has stopped and a LATER one has reopened
        the store from scratch -- proving it was checkpointed, not merely held
        in the connection.

        The second reader used to be `rmp graph execute` reopening the store
        in-process. With that subcommand withdrawn, reopening the store is what
        starting a server is, and reading through a server that was not running
        when the write was made proves the same thing about disk.
        """
        roadmap = self.seeded_roadmap(
            "identity-service-3",
            "CREATE (:Component {key:'auth-api', language:'go'})",
        )
        server = self.start_server(roadmap)

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query",
             "MATCH (c:Component {key:'auth-api'}) "
             "SET c.deployed_version = 'v2.3.1' "
             "CREATE (d:Decision {key:'use-oauth2', title:'Adopt OAuth2 for "
             "third-party integrations'}) "
             "CREATE (c)-[:GOVERNED_BY]->(d)"]
        )
        assert rc == EXIT_OK, f"got {rc}, stderr={err!r}"
        # One SET, one labelled two-property node, one relationship: the whole
        # of what the statement applied, published beside the {"ok": true}.
        assert_graph_write_shape(
            json.loads(out), "a multi-clause write through a running server",
            {"nodesCreated": 1, "relationshipsCreated": 1,
             "propertiesWritten": 3, "labelsAdded": 1})

        rc2, out2, err2 = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query",
             "MATCH (c:Component {key:'auth-api'})-[:GOVERNED_BY]->(d:Decision) "
             "RETURN c.deployed_version, d.title"]
        )
        assert rc2 == EXIT_OK, err2
        assert json.loads(out2) == {
            "columns": ["c.deployed_version", "d.title"],
            "rows": [["v2.3.1", "Adopt OAuth2 for third-party integrations"]],
        }, out2

        server.stop(signal.SIGINT)

        assert self.read_through_a_fresh_server(
            roadmap,
            "MATCH (c:Component {key:'auth-api'})-[:GOVERNED_BY]->(d:Decision) "
            "RETURN d.key",
        ) == {"columns": ["d.key"], "rows": [["use-oauth2"]]}, (
            "the write must have been checkpointed to the store a later server "
            "reopens once the first one has stopped"
        )

    def test_client_query_from_standard_input(self):
        roadmap = self.seeded_roadmap(
            "notification-hub-2",
            "CREATE (:Component {key:'dispatcher', language:'go'})",
        )
        server = self.start_server(roadmap)
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap],
            stdin_text="MATCH (n) RETURN count(n)\n",
        )
        assert rc == EXIT_OK, err
        assert json.loads(out) == {"columns": ["count(n)"], "rows": [[1]]}, out
        server.stop(signal.SIGINT)

    def test_client_short_form_query_flag(self):
        roadmap = self.seeded_roadmap(
            "search-indexer-3",
            "CREATE (:Component {key:'crawler', language:'go'})",
        )
        server = self.start_server(roadmap)
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "-q", "MATCH (n) RETURN count(n)"]
        )
        assert rc == EXIT_OK, err
        assert json.loads(out) == {"columns": ["count(n)"], "rows": [[1]]}, out
        server.stop(signal.SIGINT)

    def test_client_reaches_a_server_on_a_non_default_socket(self):
        roadmap = self.seeded_roadmap(
            "checkout-service-2",
            "CREATE (:Component {key:'cart-api', language:'go'})",
        )
        custom = str(self.test.home_dir / "custom" / "checkout.sock")
        os.makedirs(os.path.dirname(custom), exist_ok=True)
        _assert_socket_path_fits(custom)
        server = self.start_server(roadmap, socket_path=custom)

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--socket", custom,
             "--query", "MATCH (n) RETURN count(n)"]
        )
        assert rc == EXIT_OK, err
        assert json.loads(out) == {"columns": ["count(n)"], "rows": [[1]]}, out
        server.stop(signal.SIGINT)

    def test_client_without_socket_flag_does_not_follow_a_server_serving_elsewhere(self):
        """SPEC/GRAPH.md "Serving on a Non-Default Socket", point 3: a server
        started on a non-default socket is reached only by an invocation GIVEN
        the same --socket. One that omits the flag resolves the derived path,
        finds nothing served there, and is answered with the listening line.

        It must be answered PROMPTLY. This is the surviving half of the retired
        TestExecuteRoutesThroughServer
        .test_execute_without_socket_flag_falls_into_the_lock_when_server_is_elsewhere:
        `graph execute` used to fall onto the direct path here and sit out the
        whole 7.5s wait budget against the lock the non-default server holds.
        `graph client` opens no store and takes no lock, so there is no lock to
        fall into and nothing to wait for.
        """
        roadmap = self.seeded_roadmap(
            "checkout-service-5",
            "CREATE (:Component {key:'cart-api', language:'go'})",
        )
        custom = str(self.test.home_dir / "custom4" / "checkout.sock")
        os.makedirs(os.path.dirname(custom), exist_ok=True)
        _assert_socket_path_fits(custom)
        server = self.start_server(roadmap, socket_path=custom)
        derived = self.default_socket_path(roadmap)
        assert not os.path.exists(derived), (
            "the server was asked for a non-default socket and must have bound "
            "nothing at the derived path"
        )

        started = time.monotonic()
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query", "MATCH (n) RETURN count(n)"],
            timeout=WAIT_BUDGET_S + 10,
        )
        elapsed = time.monotonic() - started

        assert rc == EXIT_DATABASE, f"got {rc}, stderr={err!r}"
        expected = f"Error: graph server error: no graph server is listening on {derived}"
        assert err.splitlines()[0] == expected, err
        assert out == ""
        assert elapsed < 2.0, (
            f"the client neither opens a store nor waits for a lock, so this "
            f"refusal must be prompt; took {elapsed:.2f}s"
        )

        # The control: the server WAS serving all along, and naming its socket
        # reaches it. Without this the case would also pass against a server
        # that had died.
        rc2, out2, err2 = self.run_cli(
            ["graph", "client", "-r", roadmap, "--socket", custom,
             "--query", "MATCH (n) RETURN count(n)"]
        )
        assert rc2 == EXIT_OK, f"got {rc2}, stderr={err2!r}"
        assert json.loads(out2) == {"columns": ["count(n)"], "rows": [[1]]}, out2

        server.stop(signal.SIGINT)

    def test_client_malformed_cypher_reports_engine_diagnostic_exit_1(self):
        roadmap = self.seeded_roadmap(
            "fulfillment-service-2",
            "CREATE (:Component {key:'warehouse-router', language:'go'})",
        )
        server = self.start_server(roadmap)
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query", "MATCH (n RETURN n"]
        )
        assert rc == EXIT_DATABASE, f"got {rc}, stderr={err!r}"
        assert err.startswith("Error: graph engine error: graph query failed: "), err
        assert out == ""
        server.stop(signal.SIGINT)

    def test_client_roadmap_not_specified_exits_3(self):
        rc, out, err = self.run_cli(["graph", "client", "--query", "MATCH (n) RETURN n"])
        assert rc == EXIT_NO_ROADMAP, err
        assert err.splitlines()[0] == "Error: no roadmap selected: use -r <name> or --roadmap <name>"

    def test_client_roadmap_not_found_exits_4(self):
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", "no-such-roadmap", "--query", "MATCH (n) RETURN n"]
        )
        assert rc == EXIT_NOT_FOUND, err
        assert err.splitlines()[0] == 'Error: resource not found: roadmap "no-such-roadmap" not found'

    def test_client_no_query_supplied_exits_2(self):
        roadmap = self.seeded_roadmap(
            "search-indexer-4",
            "CREATE (:Component {key:'crawler', language:'go'})",
        )
        rc, out, err = self.run_cli(["graph", "client", "-r", roadmap], stdin_text="")
        assert rc == EXIT_INVALID_INPUT, f"got {rc}, stderr={err!r}"
        assert err.splitlines()[0] == "Error: required parameter missing: no query supplied"

    def test_client_socket_flag_empty_value_exits_2(self):
        roadmap = self.seeded_roadmap(
            "search-indexer-5",
            "CREATE (:Component {key:'crawler', language:'go'})",
        )
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--socket", "", "--query", "MATCH (n) RETURN n"]
        )
        assert rc == EXIT_INVALID_INPUT, f"got {rc}, stderr={err!r}"
        assert "required parameter missing: --socket" in err, err

    def test_client_stray_positional_argument_exits_2(self):
        roadmap = self.seeded_roadmap(
            "search-indexer-6",
            "CREATE (:Component {key:'crawler', language:'go'})",
        )
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "MATCH (n) RETURN n"]
        )
        assert rc == EXIT_INVALID_INPUT, f"got {rc}, stderr={err!r}"
        assert "graph queries use --query or stdin" in err, err

    def test_client_query_over_maximum_length_exits_6(self):
        roadmap = self.seeded_roadmap(
            "search-indexer-7",
            "CREATE (:Component {key:'crawler', language:'go'})",
        )
        oversized = "MATCH (n) WHERE n.x = '" + ("a" * 1048577) + "' RETURN n"
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap], stdin_text=oversized,
        )
        assert rc == EXIT_VALIDATION, f"got {rc}, stderr={err!r}"
        assert err.splitlines()[0] == (
            "Error: validation error: query exceeds maximum length of 1048576 bytes"
        )

    def test_client_help_flag_prints_usage_and_exits_0(self):
        rc, out, err = self.run_cli(["graph", "client", "-h"])
        assert rc == EXIT_OK, err
        assert "Usage: rmp graph client -r <roadmap> [-q <cypher>] [--socket <path>]" in out

    def test_no_server_listening_on_a_socket_that_never_existed(self):
        roadmap = self.seeded_roadmap(
            "billing-reconciler-2",
            "CREATE (:Component {key:'ledger-sync', language:'go'})",
        )
        socket_path = self.default_socket_path(roadmap)
        assert not os.path.exists(socket_path)

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query", "MATCH (n) RETURN count(n)"]
        )
        assert rc == EXIT_DATABASE, f"got {rc}, stderr={err!r}"
        expected = f"Error: graph server error: no graph server is listening on {socket_path}"
        assert err.splitlines()[0] == expected, err
        assert out == ""

    def test_no_server_listening_on_a_stale_socket_left_by_a_killed_server(self):
        roadmap = self.seeded_roadmap(
            "billing-reconciler-3",
            "CREATE (:Component {key:'ledger-sync', language:'go'})",
        )
        server = self.start_server(roadmap)
        socket_path = server.socket
        server.kill_dash_9()
        assert os.path.exists(socket_path), (
            "a SIGKILLed server must leave its socket file behind (stale)"
        )

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query", "MATCH (n) RETURN count(n)"]
        )
        assert rc == EXIT_DATABASE, f"got {rc}, stderr={err!r}"
        expected = f"Error: graph server error: no graph server is listening on {socket_path}"
        assert err.splitlines()[0] == expected, err
        assert out == ""
        assert os.path.exists(socket_path), (
            "the stale socket is neither an error nor the caller's to remove "
            "(SPEC/GRAPH.md \"Server Resolution\" rule 1)"
        )


# --------------------------------------------------------------------------
# RETIRED: TestExecuteRoutesThroughServer
#
# The class drove `rmp graph execute` in the two modes it had -- routed to a
# running server, or falling onto the direct path when none answered -- and
# both modes are gone with the subcommand. `rmp graph <anything but
# serve|client>` exits 127, and nothing opens a graph store except a server.
# The four cases, and what covers each now:
#
#   test_execute_writes_reach_the_server_and_client_reads_them_back
#       COVERED by TestGraphClient
#       .test_client_write_through_a_running_server_is_durable, which makes a
#       multi-clause write through the server, reads it back through a second
#       client invocation, and then through a later server that reopened the
#       store -- a stronger check than the original, which only proved the
#       write was visible to the same server.
#
#   test_execute_does_not_contend_for_the_lock_when_the_roadmap_is_served
#       NOTHING COVERS IT, and nothing can. It was a regression guard for one
#       surface contending with another for the store lock, and there is no
#       longer a second surface to contend: `graph client` opens no store and
#       takes no lock at all. The lock itself is still driven, by
#       TestServeFlagsAndErrorCases
#       .test_second_serve_against_the_same_roadmap_gets_the_lock_line, which
#       is now the only way to reach it -- two servers.
#
#   test_execute_socket_flag_reaches_a_server_on_a_non_default_socket
#       COVERED by TestGraphClient
#       .test_client_reaches_a_server_on_a_non_default_socket, the same
#       scenario at the one surface that remains.
#
#   test_execute_without_socket_flag_falls_into_the_lock_when_server_is_elsewhere
#       REPLACED by TestGraphClient
#       .test_client_without_socket_flag_does_not_follow_a_server_serving_elsewhere.
#       The rule it tested -- a non-default socket is followed only by an
#       invocation given the same --socket -- is unchanged; what it asserted
#       ABOUT that rule, a fall onto the direct path and a full wait-budget
#       block against the lock, has no counterpart. The replacement asserts the
#       published listening line and a prompt refusal instead.
# --------------------------------------------------------------------------


class TestSocketUnreachable(GraphServerTestBase):
    """SPEC/GRAPH.md "Server Resolution", the `Unreachable` state: the path
    is not a socket at all. This is the third of the ways
    internal/graphclient's own unit suite drives this state
    (TestResolve_APathThatIsNotASocketIsUnreachable), reproduced here through
    the built binary.

    It used to be driven at two surfaces, because `rmp graph execute` resolved
    a socket as well and had somewhere else to go when the resolution failed.
    Its case here (test_execute_reports_unreachable_for_a_regular_file_at_the
    _socket_path) is RETIRED with the subcommand: what it added over the client
    case was that a failed resolution did not fall back to the store, and there
    is no store-opening surface left to fall back. `graph client` is now the
    only invocation that resolves a socket at all, and the case below covers
    it.
    """

    def test_client_reports_unreachable_for_a_regular_file_at_the_socket_path(self):
        roadmap = self.seeded_roadmap(
            "identity-service-5",
            "CREATE (:Component {key:'auth-api', language:'go'})",
        )
        socket_path = self.default_socket_path(roadmap)
        with open(socket_path, "w") as fh:
            fh.write("not a socket\n")

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query", "MATCH (n) RETURN count(n)"],
            timeout=PROBE_DEADLINE_S + 5,
        )
        assert rc == EXIT_DATABASE, f"got {rc}, stderr={err!r}"
        prefix = f"Error: graph server error: graph server unreachable at {socket_path}: "
        assert err.startswith(prefix), err
        assert out == ""


class TestServerConnectionFailureModes(GraphServerTestBase):
    """The two hardest of the seven socket error lines: a connection that
    dies mid-statement, and a server that stays alive but does not answer
    inside the caller's own backstop. Both are reached through a genuinely
    slow write against a running server -- see the module docstring for the
    calibration this relies on.
    """

    @staticmethod
    def _seed_bulk(count: int) -> str:
        return f"UNWIND range(1, {count}) AS i CREATE (:MetricSample {{seq:i}})"

    def test_connection_lost_after_statement_sent(self):
        """A write is sent, the server is killed while it is still executing
        (well inside the server's own 5s statement budget, so the send has
        definitely completed), and the client must report the connection
        lost rather than silently falling back to the store.
        """
        roadmap = self.seeded_roadmap(
            "observability-platform-4", self._seed_bulk(300),
        )
        server = self.start_server(roadmap)
        socket_path = server.socket

        client = self.run_cli_async(
            ["graph", "client", "-r", roadmap, "--query",
             "MATCH (a:MetricSample),(b:MetricSample),(c:MetricSample) "
             "CREATE (:Anomaly {correlated_seq:a.seq})"]
        )
        time.sleep(0.6)
        assert client.poll() is None, (
            "the client must still be waiting on the server when it is "
            "killed, or this does not test what it claims to"
        )
        server.kill_dash_9()

        try:
            out, err = client.communicate(timeout=15.0)
        except subprocess.TimeoutExpired:
            client.kill()
            out, err = client.communicate()
            self.fail_msg = "client did not observe the killed server in time"
            assert False, self.fail_msg

        assert client.returncode == EXIT_DATABASE, f"got {client.returncode}, stderr={err!r}"
        expected = (
            f"Error: graph server error: the connection to the graph server at "
            f"{socket_path} was lost; the statement's outcome is unknown"
        )
        assert err.splitlines()[0] == expected, err
        assert out == ""

        # Durability check, once the store is reachable again: the killed
        # statement must have left no partial write (SPEC/GRAPH.md
        # "Statement Time Budget" rule 2, "a cut statement rolls back
        # whole" -- here cut by the kill rather than by the deadline, and the
        # commit protocol is what makes the two indistinguishable in outcome).
        # The reader is a NEW server over the store the killed one left behind,
        # which is the only way anything reopens a graph now.
        assert self.read_through_a_fresh_server(
            roadmap, "MATCH (n:Anomaly) RETURN count(n)"
        ) == {"columns": ["count(n)"], "rows": [[0]]}, (
            "an unacknowledged write must leave no partial trace"
        )

    def test_server_unanswered_within_the_backstop_deadline(self):
        """The seventh published socket line, driven by a server that provably
        cannot answer rather than by one that is merely expected to answer late.

        A cartesian-product write is sent to a real server and left in flight,
        and the server is then frozen with SIGSTOP. The signal is uncatchable,
        so from that instant the process cannot answer anything -- while its
        session, its connection and its socket are left exactly as they were:
        nothing is closed, no FIN or RST reaches the peer, and the kernel holds
        both ends of the socket open throughout. That is the antecedent
        SPEC/GRAPH.md "Server Resolution" rule 7 states for this line in so many
        words -- "the connection is intact, the server is alive, and the
        statement's outcome is unknown" -- and it is the process-level twin of
        internal/graphclient's own TestSend_ClassifiesAServerThatDoesNotAnswer,
        which reaches the same classification in process with a scripted server
        that stays silent on RUN.

        WHAT THIS CASE DELIBERATELY NO LONGER DEPENDS ON. Until rmp task #404 it
        required the server's own undo replay to outlast the caller's 7.5s
        backstop unaided, and the specification refuses to promise that in
        either direction. It asserts nothing here about how long that replay
        takes, and it would still hold if the engine returned the write path to
        the 1.000x the read path already achieves. The measurements that
        condemned the old shape, and the margins this one has instead, are
        recorded at BACKSTOP_FREEZE_DELAYS_S.

        WHAT IT ASSERTS, and what guarantees each:

        * The client fails with exit code 1 on the published unanswered line,
          character for character (SPEC/COMMANDS.md "Graph Server Socket Error
          Lines" for the line, and "Graph Management" for the code every socket
          failure carries).
        * The failure is the caller's OWN backstop rather than an earlier
          deadline: it takes at least the wait budget, which rule 7 fixes as
          "the statement budget plus the backoff total, and never the statement
          budget itself".
        * The connection was intact when that backstop fired. This is asserted
          BY the line rather than beside it: internal/graphclient classifies a
          transport failure after the send by what ended the wait, so a
          connection that had been lost produces the sixth line instead of the
          seventh, and a freeze that landed before the RUN was written produces
          the "unreachable" line. Only an intact connection whose own deadline
          expired produces this one.
        * The server is alive: still frozen ('T' in /proc) at the instant the
          client gives up, and it resumes, finishes and stops afterwards.
        * The stop is clean: exit 0 with the socket file gone (SPEC/GRAPH.md
          "Server Shutdown and the Drain", steps 6 and 7).
        * The cut write rolled back whole: not one of the nodes it was creating
          survives into a later invocation (SPEC/GRAPH.md "Statement Time
          Budget", rule 2 -- "A cut statement rolls back whole. There is no
          partial write to reconcile and no torn state on disk").
        """
        roadmap = self.seeded_roadmap(
            "observability-platform-5", self._seed_bulk(3000),
        )
        total = len(BACKSTOP_FREEZE_DELAYS_S)

        for run_index, freeze_after in enumerate(BACKSTOP_FREEZE_DELAYS_S, start=1):
            label = (
                f"run {run_index} of {total}, the server frozen {freeze_after}s "
                f"after the client was launched"
            )
            server = self.start_server(roadmap)
            socket_path = server.socket

            started = time.monotonic()
            client = self.run_cli_async(
                ["graph", "client", "-r", roadmap, "--query",
                 "MATCH (a:MetricSample),(b:MetricSample),(c:MetricSample) CREATE ()"]
            )
            time.sleep(freeze_after)
            assert client.poll() is None, (
                f"{label}: the client had already finished before the server "
                f"was frozen, so nothing below is driving a server that cannot "
                f"answer"
            )
            frozen = server.pause()
            assert frozen == "T", (
                f"{label}: SIGSTOP left the server in state {frozen!r} rather "
                f"than 'T'. A server that is still running is not provably "
                f"unable to answer, and this case would be a race again"
            )

            try:
                out, err = client.communicate(timeout=WAIT_BUDGET_S + 10)
            except subprocess.TimeoutExpired:
                server.resume()
                client.kill()
                client.communicate()
                assert False, (
                    f"{label}: the client never gave up on a server that could "
                    f"not answer. Its backstop is {WAIT_BUDGET_S}s and nothing "
                    f"else was ever going to end that wait"
                )
            elapsed = time.monotonic() - started

            assert client.returncode == EXIT_DATABASE, (
                f"{label}: got {client.returncode}, stderr={err!r}"
            )
            expected = (
                f"Error: graph server error: the graph server at {socket_path} did "
                f"not answer within {WAIT_BUDGET_S}s; the statement's outcome is "
                f"unknown"
            )
            assert err.splitlines()[0] == expected, f"{label}: {err!r}"
            assert out == "", (
                f"{label}: a statement that failed must write nothing to "
                f"stdout; got {out!r}"
            )
            assert elapsed >= WAIT_BUDGET_S - 0.5, (
                f"{label}: the failure must come from the caller's OWN "
                f"backstop, not an earlier one; took {elapsed:.2f}s"
            )

            still_frozen = server.state()
            assert still_frozen == "T", (
                f"{label}: the server must still be alive -- and, having been "
                f"frozen throughout, must still be in state 'T' rather than "
                f"{still_frozen!r} -- at the instant the client gave up. This "
                f"line is specified as \"the connection is intact... the server "
                f"is alive\", which is what distinguishes it from a lost "
                f"connection"
            )

            server.resume()
            assert server.is_alive(), (
                f"{label}: the server must survive the statement it was frozen "
                f"in the middle of; stderr={server.stderr_text()!r}"
            )

            # Let the resumed statement be cut by its own budget, undone, and
            # the server return to quiescence before a clean stop, rather than
            # compounding this case with the separate (and separately tested)
            # shutdown-under-load path.
            rc2 = server.stop(signal.SIGINT, timeout=60.0)
            assert rc2 == EXIT_OK, (
                f"{label}: the server must still shut down cleanly once the "
                f"resumed statement finishes; got {rc2}, "
                f"stderr={server.stderr_text()!r}"
            )
            assert not os.path.exists(socket_path), (
                f"{label}: the socket must be removed on exit"
            )

            # The cut write rolled back whole and left no checkpoint behind it,
            # observed by reopening the store in a further server.
            durable = self.read_through_a_fresh_server(
                roadmap, "MATCH (n) WHERE NOT n:MetricSample RETURN count(n)")
            assert durable == {"columns": ["count(n)"], "rows": [[0]]}, (
                f"{label}: the cut CREATE() must have written nothing durable; "
                f"got {durable!r}"
            )


class TestShutdownDrainsAStatementInFlight(GraphServerTestBase):
    """SPEC/GRAPH.md "Server Shutdown and the Drain": a signal does not cut a
    statement outright. It stops accepting new connections and waits, bounded
    by the wait budget, for what is in flight to reach a quiescent point --
    and a statement that COMPLETES during that wait (including one the
    server's own budget cuts and rolls back) is answered before the process
    exits.

    This is deliberately a REGRESSION test rather than a smoke test: it
    asserts a TIMING FLOOR on the shutdown (proof the drain actually waited)
    together with the exact answer the client must have received, so a drain
    that regressed to a no-op -- exactly rmp task #369's own mutation M4,
    "the drain removed (`quiescent` -> `true`)" -- fails it rather than
    passing it by accident. See the module's task report for how this was
    verified to go red under that mutation.
    """

    def test_drain_waits_for_an_in_flight_write_then_the_cut_is_answered_before_exit(self):
        roadmap = self.seeded_roadmap(
            "observability-platform-6",
            "UNWIND range(1, 300) AS i CREATE (:MetricSample {seq:i})",
        )
        server = self.start_server(roadmap)

        client = self.run_cli_async(
            ["graph", "client", "-r", roadmap, "--query",
             "MATCH (a:MetricSample),(b:MetricSample),(c:MetricSample) "
             "CREATE (:Anomaly {correlated_seq:a.seq})"]
        )
        time.sleep(1.0)
        assert client.poll() is None, (
            "the write must still be in flight -- inside the server's own "
            "5s statement budget -- when the signal arrives, or this does "
            "not test the drain at all"
        )

        signalled_at = time.monotonic()
        server.proc.send_signal(signal.SIGINT)
        server_rc = server.wait_for_exit(timeout=30.0)
        shutdown_elapsed = time.monotonic() - signalled_at

        assert server_rc == EXIT_OK, (
            f"the server must still exit 0 once the in-flight write has "
            f"been cut and rolled back; got {server_rc}, "
            f"stderr={server.stderr_text()!r}"
        )
        assert not os.path.exists(server.socket), "the socket must be removed on exit"

        assert shutdown_elapsed >= 2.5, (
            f"the drain must have waited for the write's own 5s budget to "
            f"cut it (the signal arrived ~4s before that point) rather than "
            f"cutting the connection immediately; shutdown took only "
            f"{shutdown_elapsed:.2f}s, which is what a no-op drain looks "
            f"like from outside"
        )

        out, err = client.communicate(timeout=15.0)
        assert client.returncode == EXIT_DATABASE, f"got {client.returncode}, stderr={err!r}"
        assert err.startswith(
            "Error: graph engine error: graph query exceeded the 5s statement "
            "time budget; nothing was written."
        ), (
            f"the drain's guarantee is that a statement which COMPLETES "
            f"during the wait is answered before the server stops -- here "
            f"that completion is the server's own budget cutting and "
            f"rolling the statement back, so the client must receive the "
            f"ordinary typed budget failure and not a broken connection; "
            f"got {err!r}"
        )
        assert out == ""

        # The cut write left nothing behind, exactly as an ordinary
        # (unsignalled) budget cut does -- read back through a server started
        # over the store the drained one left.
        remaining = self.read_through_a_fresh_server(
            roadmap, "MATCH (n:Anomaly) RETURN count(n)")
        assert remaining == {"columns": ["count(n)"], "rows": [[0]]}, remaining


class TestDurabilityAcrossKill(GraphServerTestBase):
    """SPEC/GRAPH.md "Durability and Checkpointing in a Long-Lived Process":
    a commit is durable before it is acknowledged, so it survives an
    uncatchable SIGKILL exactly as it survives a graceful stop. Driven end to
    end: several distinct writes acknowledged through `rmp graph client`,
    then the server is SIGKILLed outright (no drain, no checkpoint), and a
    relaunch over the resulting stale socket must serve every one of them.
    """

    def test_writes_acknowledged_before_a_sigkill_are_present_after_relaunch(self):
        roadmap = self.seeded_roadmap(
            "release-orchestrator-2",
            "CREATE (:Component {key:'deploy-pipeline', language:'go'})",
        )
        server = self.start_server(roadmap)
        socket_path = server.socket

        components = [
            ("canary-rollout", "Progressively shift traffic to the new build"),
            ("blue-green-switch", "Flip the load balancer to the green fleet"),
            ("rollback-guard", "Automatically revert on an elevated error rate"),
        ]
        for key, title in components:
            rc, out, err = self.run_cli(
                ["graph", "client", "-r", roadmap, "--query",
                 f"MATCH (p:Component {{key:'deploy-pipeline'}}) "
                 f"CREATE (s:Decision {{key:'{key}', title:'{title}'}}) "
                 f"CREATE (p)-[:GOVERNED_BY]->(s)"]
            )
            assert rc == EXIT_OK, f"{key}: exit={rc} err={err!r}"
            assert_graph_write_shape(
                json.loads(out), f"{key}: a write through a running server",
                {"nodesCreated": 1, "relationshipsCreated": 1,
                 "propertiesWritten": 2, "labelsAdded": 1})

        server.kill_dash_9()
        assert os.path.exists(socket_path), "a SIGKILLed server must leave a stale socket"

        relaunched = self.start_server(roadmap)
        assert relaunched.socket == socket_path

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query",
             "MATCH (:Component)-[:GOVERNED_BY]->(d:Decision) "
             "RETURN d.key ORDER BY d.key"]
        )
        assert rc == EXIT_OK, err
        got = json.loads(out)
        want_keys = sorted(key for key, _ in components)
        assert got == {"columns": ["d.key"], "rows": [[k] for k in want_keys]}, (
            f"every acknowledged commit must survive the kill; got {got!r}"
        )

        relaunched.stop(signal.SIGINT)


class TestConcurrentClients(GraphServerTestBase):
    """SPEC/GRAPH.md "Concurrency Inside the Server": readers never block and
    writers do not exclude one another. A light end-to-end demonstration
    through the built binary -- concurrent `rmp graph client` PROCESSES, not
    goroutines inside a test binary -- that every read and every write lands,
    which is the property `internal/graphserve`'s own benchmarks establish in
    depth (rmp task #370) and this module only has to confirm survives the
    CLI.
    """

    def test_concurrent_reads_and_writes_all_land(self):
        roadmap = self.seeded_roadmap(
            "search-indexer-8",
            "CREATE (:Component {key:'crawler', language:'go'})",
        )
        server = self.start_server(roadmap)

        writers = [
            self.run_cli_async(
                ["graph", "client", "-r", roadmap, "--query",
                 f"CREATE (:Component {{key:'crawler-shard-{i}', language:'go'}})"]
            )
            for i in range(8)
        ]
        readers = [
            self.run_cli_async(
                ["graph", "client", "-r", roadmap, "--query",
                 "MATCH (n:Component) RETURN count(n)"]
            )
            for _ in range(8)
        ]

        for i, proc in enumerate(writers):
            out, err = proc.communicate(timeout=20.0)
            assert proc.returncode == EXIT_OK, f"writer {i}: exit={proc.returncode} err={err!r}"
            assert_graph_write_shape(json.loads(out), f"writer {i}")
        for i, proc in enumerate(readers):
            out, err = proc.communicate(timeout=20.0)
            assert proc.returncode == EXIT_OK, f"reader {i}: exit={proc.returncode} err={err!r}"
            json.loads(out)  # a well-formed count; the exact value races the writers

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query", "MATCH (n:Component) RETURN count(n)"]
        )
        assert rc == EXIT_OK, err
        assert json.loads(out) == {"columns": ["count(n)"], "rows": [[9]]}, (
            f"1 seed + 8 concurrent writers, all landed; got {out!r}"
        )

        server.stop(signal.SIGINT)


class TestHotNodeContention(GraphServerTestBase):
    """SPEC/GRAPH.md acceptance criterion 54 (rmp task #384).

    The defect this exists against: under concurrent writers converging on a
    SINGLE node, a fraction of statements exhausted the client's retry ladder
    and failed, although each was valid and the store healthy. The fix was to
    retry a serialisation conflict under the project's FULL-JITTER delay
    shape rather than its fixed ladder (SPEC/IMPLEMENTATION.md "Retry Logic";
    SPEC/GRAPH.md "Concurrency Inside the Server", rule 4).

    Why the shape matters more at THIS boundary than inside a process. Sixteen
    processes launched together, each walking the same fixed ladder, re-collide
    in lockstep: a herd that loses together sleeps the same 100ms and re-sends
    together. Writers inside one process are desynchronised for free by their
    own varying completion times. Measured here, at this exact load: the fixed
    ladder exhausts on 2.81%-3.44% of invocations, in every repetition, where
    the same load measured in-process exhausted on 0.18%-0.43%. Full jitter
    exhausted on none of 7,040.

    Why every exit code is asserted rather than a sample. The criterion says
    so, and the reason is arithmetic: the failure is a few percent of
    invocations at worst and was under one percent when it was first reported,
    so a sampled assertion steps straight over it.

    Why 320 invocations rather than 16. One statement per client would meet
    the words of the criterion and establish nothing: at a three-percent
    failure rate, sixteen statements go green about six times in ten under the
    very shape the criterion exists to reject. Sixteen clients each driving
    twenty sequential invocations puts the ladder's expected failure count at
    about ten, so reverting the shape fails this test with near-certainty --
    and it costs under a second under the shape that passes.
    """

    WRITERS = 16
    ROUNDS = 20

    def test_sixteen_clients_writing_one_hot_node_all_exit_zero(self):
        # One node, one property, and every writer stamping the same property
        # on it -- which is the shape this project produces itself when
        # several agents stamp provenance on one knowledge-graph node
        # (GRAPH.md "Concurrency Inside the Server", rule 8).
        roadmap = self.seeded_roadmap(
            "provenance-stamp",
            "CREATE (:Component {key:'internal/backoff', stamped_by:'seed'})",
        )
        server = self.start_server(roadmap)

        total = self.WRITERS * self.ROUNDS
        outcomes = [None] * total

        def stamp(writer_index):
            for round_index in range(self.ROUNDS):
                value = f"agent-{writer_index}-round-{round_index}"
                started = time.monotonic()
                rc, out, err = self.run_cli(
                    ["graph", "client", "-r", roadmap, "--query",
                     "MATCH (n:Component {key:'internal/backoff'}) "
                     f"SET n.stamped_by = '{value}'"],
                    timeout=40.0,
                )
                outcomes[writer_index * self.ROUNDS + round_index] = (
                    value, rc, out.strip(), (err.splitlines()[0] if err else ""),
                    time.monotonic() - started,
                )

        threads = [threading.Thread(target=stamp, args=(w,))
                   for w in range(self.WRITERS)]
        wall_start = time.monotonic()
        for t in threads:
            t.start()
        for t in threads:
            t.join(timeout=300.0)
        elapsed = time.monotonic() - wall_start

        # Nothing is allowed to be missing. A writer thread that died or hung
        # would otherwise leave holes that the exit-code sweep below would
        # skip over, and the test would pass having asserted less than it
        # claims.
        missing = [i for i, o in enumerate(outcomes) if o is None]
        assert not missing, (
            f"{len(missing)} of {total} invocations never recorded an outcome "
            f"(indices {missing[:8]}...): a writer thread did not finish, so "
            f"the assertions below would have skipped them"
        )

        # EVERY exit code, not a sample.
        failed = [o for o in outcomes if o[1] != EXIT_OK]
        assert not failed, (
            f"{len(failed)} of {total} invocations failed under "
            f"{self.WRITERS} concurrent clients writing ONE node in "
            f"{elapsed:.2f}s ({total / elapsed:.0f} invocations/s). Every one "
            f"was a valid statement against a healthy store, so every one had "
            f"to succeed: a serialisation conflict is a normal outcome and is "
            f"retried, under the full-jitter shape of the project's single "
            f"retry policy (SPEC/IMPLEMENTATION.md 'Retry Logic'). Reverting "
            f"that shape to the fixed ladder is what this failure looks "
            f"like.\n  first failures: "
            + "\n    ".join(f"{v}: exit={rc} {msg}" for v, rc, _out, msg, _t in failed[:5])
        )

        # A write that reports success must have reported the write shape.
        def is_write_shape(stdout):
            result = json.loads(stdout)
            return (isinstance(result, dict) and result.get("ok") is True
                    and not set(result) - {"ok", "counters"})

        wrong_shape = [o for o in outcomes if not is_write_shape(o[2])]
        assert not wrong_shape, (
            f"{len(wrong_shape)} invocation(s) exited 0 without the write "
            f"result shape; first: {wrong_shape[0][2]!r}"
        )

        # The criterion's own non-vacuity guard, and it is not a formality: a
        # MATCH that matched NOTHING would make every SET above a no-op, every
        # invocation would exit 0 with {"ok": true}, and nothing would ever
        # have contended. Reading the node back proves the statements found it
        # and applied to it -- and that they all found the SAME one.
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query",
             "MATCH (n:Component {key:'internal/backoff'}) RETURN n.stamped_by"]
        )
        assert rc == EXIT_OK, f"reading the hot node back failed: {err!r}"
        read_back = json.loads(out)
        assert read_back["columns"] == ["n.stamped_by"], read_back
        assert len(read_back["rows"]) == 1, (
            f"the writers were meant to converge on ONE node; the graph holds "
            f"{len(read_back['rows'])} matching it, so they did not contend "
            f"for anything: {read_back!r}"
        )

        final = read_back["rows"][0][0]
        assert final != "seed", (
            "the node still carries the seeded value: not one of the 320 "
            "writes landed, so the statements matched nothing and this test "
            "asserted nothing"
        )
        # Each client writes its rounds in order and blocks on each, so the
        # LAST value to commit is necessarily some client's final round.
        last_round = {f"agent-{w}-round-{self.ROUNDS - 1}" for w in range(self.WRITERS)}
        assert final in last_round, (
            f"the node carries {final!r}, which is not the final round of any "
            f"client. Each client blocks on every invocation and issues its "
            f"rounds in order, so the last write to commit must be some "
            f"client's last"
        )

        server.stop(signal.SIGINT)


def _run_all():
    """Discover and run every Test* class defined in this module.

    Enumerating the module's own namespace rather than naming the classes in
    a fixed list means a class added later cannot silently fail to run (see
    test_64_graph_schema_management.py's identical _run_all for the model
    tests/run_tests.py's own dynamic-discovery exemption recognises).
    """
    passed = failed = 0
    failures = []
    classes = [
        obj for _name, obj in sorted(inspect.getmembers(sys.modules[__name__], inspect.isclass))
        if obj.__module__ == __name__ and _name.startswith("Test")
    ]
    print(f"Discovered {len(classes)} test classes: "
          f"{', '.join(cls.__name__ for cls in classes)}")
    for cls in classes:
        for m in sorted(name for name in dir(cls) if name.startswith("test_")):
            label = f"{cls.__name__}.{m}"
            instance = cls()
            instance.setup_method()
            started = time.monotonic()
            try:
                getattr(instance, m)()
                passed += 1
                print(f"OK   {label} ({time.monotonic() - started:.2f}s)")
            except AssertionError as exc:
                failed += 1
                failures.append((label, exc))
                print(f"FAIL {label} ({time.monotonic() - started:.2f}s)")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((label, exc))
                print(f"ERROR {label} ({time.monotonic() - started:.2f}s)")
            finally:
                instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Graph server/client E2E tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\nFAIL {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
