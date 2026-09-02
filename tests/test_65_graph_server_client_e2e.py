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
SPEC/COMMANDS.md "Graph Management" (the Execute/Serve/Client option, output,
exit-code and error-case blocks, and "Graph Server Socket Error Lines") are
canonical for every assertion made here; nothing below restates a rule this
module does not also verify against the binary.

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

  1. "already serving"   -- TestServeSocketErrorLines.test_already_serving_...
  2. "cannot bind"        -- TestServeSocketErrorLines.test_cannot_bind_...
  3. "cannot take the graph store lock" (the LOCK line)
                          -- TestServeSocketErrorLines.test_second_serve_...
  4. "no graph server is listening"
                          -- TestGraphClient.test_no_server_listening_... (x2:
                             a socket that never existed, and a stale one)
  5. "graph server unreachable"
                          -- TestSocketUnreachable (both execute and client)
  6. "the connection ... was lost"
                          -- TestServerConnectionFailureModes
                             .test_connection_lost_after_statement_sent
  7. "did not answer within ... ; the statement's outcome is unknown"
                          -- TestServerConnectionFailureModes
                             .test_server_unanswered_within_backstop_deadline

Every failure path below asserts the FULL published line (after substituting
the one placeholder each carries -- the resolved socket path) AND the exit
code, never the code alone, per this task's acceptance criteria.

## The two expensive, timing-calibrated reproductions

Lines 6 and 7 require a statement whose SERVER-side cut-and-undo genuinely
outlasts the caller's own 7.5s backstop -- and SPEC/GRAPH.md "Statement Time
Budget" is explicit that the multiplier over the budget is a property of the
statement and the machine, with "nothing measured establishes a ceiling".
Calibrated by hand against this repository's own dev machine before being
encoded here (a slower or faster machine would need different numbers, which
is precisely the residual the specification names): a 3000-node cartesian
product hit by a bare `CREATE ()` reliably exceeds the 7.5s backstop while
the server stays alive to answer eventually; a 300-node one hit by a heavier
per-row write (`CREATE (:Anomaly {...})`) reliably still runs one second
after being launched, which is what "the connection lost after send" test
needs a window for.

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
import queue
import re
import signal
import socket as socketlib
import subprocess
import sys
import tempfile
import threading
import time

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


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

# A Unix domain socket path is capped at 108 bytes on Linux -- sun_path's
# size -- and a HOME rooted under a long build/session directory blows past
# it the moment a roadmap name is appended (rmp task #367 FINDING #266,
# measured there against exactly this failure). tempfile.mkdtemp() defaults
# to $TMPDIR or /tmp, which is short; this constant is the guard that turns a
# violation into a diagnosable setup failure instead of a mysterious "bind:
# invalid argument" deep inside a signal-handling test.
_MAX_SUN_PATH = 108


def _assert_socket_path_fits(path: str):
    """Guard the trap SPEC/GRAPH.md documents: a derived socket path over 108
    bytes fails to bind for a reason ("bind: invalid argument") that gives no
    hint the path itself is the cause. Failing here, with the path and its
    length spelled out, is what makes that diagnosable instead of mysterious.
    """
    encoded = os.fsencode(path)
    assert len(encoded) < _MAX_SUN_PATH, (
        f"derived socket path is {len(encoded)} bytes, at or over the "
        f"AF_UNIX sun_path limit of {_MAX_SUN_PATH}: {path!r}. The harness "
        f"must use a short HOME (tempfile.mkdtemp() under $TMPDIR/tmp) and a "
        f"short roadmap name."
    )


class _StreamDrain:
    """Reads one subprocess pipe (stdout or stderr) on a background thread so
    the writer never blocks on a full OS pipe buffer, and hands the reader a
    thread-safe, timeout-capable view of what has arrived.

    A graph server's own stdout carries exactly one line (the startup JSON)
    and then nothing until it exits; its stderr carries the two engine
    warnings early and nothing else on the happy path. Both must be drained
    continuously regardless, because a client-under-test can print to either
    at any point in the process's life, and an un-drained pipe backs up and
    wedges the child the moment its buffer fills.
    """

    def __init__(self, stream):
        self._lines = []
        self._lock = threading.Lock()
        self._queue: "queue.Queue[str]" = queue.Queue()
        self._thread = threading.Thread(target=self._run, args=(stream,), daemon=True)
        self._thread.start()

    def _run(self, stream):
        try:
            for line in iter(stream.readline, ""):
                with self._lock:
                    self._lines.append(line)
                self._queue.put(line)
        finally:
            # A sentinel so a blocked waiter (wait_for / wait_for_json_object)
            # unblocks the instant the pipe reaches EOF -- typically because
            # the child has exited -- instead of sitting out its whole
            # timeout for a line that will never arrive.
            self._queue.put(None)
            try:
                stream.close()
            except OSError:
                pass

    def snapshot(self):
        with self._lock:
            return list(self._lines)

    def text(self):
        return "".join(self.snapshot())

    def wait_for(self, predicate, timeout):
        """Block until a line already collected -- or a new one -- satisfies
        predicate, or timeout elapses. Returns the matching line, or None.
        """
        deadline = time.monotonic() + timeout
        for line in self.snapshot():
            if predicate(line):
                return line
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return None
            try:
                line = self._queue.get(timeout=remaining)
            except queue.Empty:
                return None
            if line is None:
                return None
            if predicate(line):
                return line

    def wait_for_json_object(self, timeout):
        """Block until the lines collected so far (from the start of the
        stream) parse as one JSON value, or timeout elapses.

        `rmp graph serve`'s startup object is pretty-printed across several
        lines (two-space indentation, per DATA_FORMATS.md "Implementation
        Notes"), so a single-line read never sees a complete object; this
        accumulates lines and re-attempts the parse after each one, which
        works for a pretty-printed object of any number of lines without
        this harness hardcoding how many `rmp` happens to emit today.
        """
        deadline = time.monotonic() + timeout
        buf = "".join(self.snapshot())
        while True:
            candidate = buf.strip()
            if candidate:
                try:
                    return json.loads(candidate)
                except json.JSONDecodeError:
                    pass
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return None
            try:
                line = self._queue.get(timeout=remaining)
            except queue.Empty:
                return None
            if line is None:
                return None
            buf += line


class GraphServeProcess:
    """One `rmp graph serve` child process, spawned and torn down by hand.

    This is the harness every scenario in this module that needs a live
    server builds on: it owns the subprocess, the two drained pipes, parsing
    the startup JSON off stdout, and sending a signal or a kill with a bounded
    wait for the exit that must follow. Nothing here talks Bolt -- the tests
    reach the server exclusively through `rmp graph client` and
    `rmp graph execute`, which is what makes this an end-to-end suite for the
    CLI contract rather than a second, private protocol client.
    """

    def __init__(self, harness: GroadmapTestBase, roadmap: str, socket_path: str = None):
        self.harness = harness
        self.roadmap = roadmap
        self.socket_path_flag = socket_path
        self.proc = None
        self.socket = None
        self._out = None
        self._err = None

    def start(self, timeout: float = 15.0):
        """Launch the server and block until its startup JSON line has been
        read off stdout (SPEC/GRAPH.md "Server Startup" step 7: the line is
        written only after the store has been opened, so seeing it is seeing
        the whole startup sequence complete, not merely the socket bound).

        Raises AssertionError, with the process's own stdout/stderr attached,
        on early exit or on a startup that never announces within `timeout`.
        """
        args = [self.harness.cli_path, "graph", "serve", "-r", self.roadmap]
        if self.socket_path_flag is not None:
            args += ["--socket", self.socket_path_flag]

        env = os.environ.copy()
        env["HOME"] = str(self.harness.home_dir)

        self.proc = subprocess.Popen(
            args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env,
        )
        self._out = _StreamDrain(self.proc.stdout)
        self._err = _StreamDrain(self.proc.stderr)

        obj = self._out.wait_for_json_object(timeout)
        if obj is None:
            self.proc.poll()
            self._finish_teardown_if_dead()
            raise AssertionError(
                f"rmp graph serve -r {self.roadmap} printed no complete startup "
                f"object within {timeout}s (exited={self.proc.returncode}); "
                f"stdout={self._out.text()!r} stderr={self._err.text()!r}"
            )
        self.socket = obj["socket"]
        return obj

    def _finish_teardown_if_dead(self):
        if self.proc.poll() is not None:
            return
        try:
            self.proc.wait(timeout=1.0)
        except subprocess.TimeoutExpired:
            pass

    def stop(self, sig=signal.SIGINT, timeout: float = 15.0) -> int:
        """Signal the server and block for its exit. Returns the exit code.

        Raises AssertionError, rather than leaving a wedged child behind, if
        the process does not exit inside `timeout`.
        """
        assert self.proc is not None, "start() was never called"
        if self.proc.poll() is not None:
            return self.proc.returncode
        self.proc.send_signal(sig)
        try:
            self.proc.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait(timeout=5.0)
            raise AssertionError(
                f"rmp graph serve -r {self.roadmap} did not exit within {timeout}s "
                f"of signal {sig!r}; it was force-killed. "
                f"stdout={self._out.text()!r} stderr={self._err.text()!r}"
            )
        return self.proc.returncode

    def kill_dash_9(self, timeout: float = 10.0):
        """SIGKILL the server -- uncatchable, no drain, no checkpoint -- and
        wait for the process table entry to clear. Used by every scenario
        that needs a stale socket or a genuinely severed connection rather
        than a graceful stop.
        """
        assert self.proc is not None, "start() was never called"
        if self.proc.poll() is None:
            self.proc.kill()
        self.proc.wait(timeout=timeout)
        return self.proc.returncode

    def is_alive(self) -> bool:
        return self.proc is not None and self.proc.poll() is None

    def stderr_text(self) -> str:
        return self._err.text() if self._err else ""

    def stdout_text(self) -> str:
        return self._out.text() if self._out else ""

    def wait_for_exit(self, timeout: float):
        """Block for a NATURAL exit -- no signal sent -- returning the exit
        code, or None if it is still running when `timeout` elapses.
        """
        try:
            return self.proc.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            return None


class GraphServerTestBase:
    """Shared fixture for every class below: a fresh temporary HOME (short,
    per the sun_path guard) and bookkeeping that force-kills any server a
    test spawned but did not itself stop, so one failing assertion never
    leaks a process into the rest of the suite's run.
    """

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self._servers = []

    def teardown_method(self):
        for server in self._servers:
            try:
                if server.is_alive():
                    server.kill_dash_9()
            except Exception:
                pass
        self.test.teardown()

    # ---- server lifecycle -------------------------------------------------

    def start_server(self, roadmap: str, socket_path: str = None, timeout: float = 15.0):
        """Start a graph server for `roadmap` (seeding one first when the
        caller has not already), track it for teardown, and return the
        started GraphServeProcess.
        """
        server = GraphServeProcess(self.test, roadmap, socket_path=socket_path)
        self._servers.append(server)
        server.start(timeout=timeout)
        return server

    def seeded_roadmap(self, name: str, seed_query: str) -> str:
        """Create `name` and run `seed_query` through the direct (unserved)
        path, which is what materialises ~/.roadmaps/<name>/graph/ --
        `rmp graph serve` refuses to serve a roadmap with no graph store yet
        (SPEC/COMMANDS.md "Serve").
        """
        self.test.create_roadmap(name)
        rc, out, err = self.run_cli(["graph", "execute", "-r", name, "--query", seed_query])
        assert rc == 0, f"seeding {name!r} failed: exit={rc} out={out!r} err={err!r}"
        return name

    # ---- process-level invocations -----------------------------------

    def run_cli(self, args, stdin_text: str = None, timeout: float = 20.0):
        """One `./bin/rmp` invocation against this fixture's HOME, returning
        (exit_code, stdout, stderr). Distinct from GroadmapTestBase.run_cmd:
        this never raises on a non-zero exit (every caller here inspects the
        code itself) and it can feed standard input, which run_cmd cannot.
        """
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        result = subprocess.run(
            [self.test.cli_path] + args,
            input=stdin_text,
            capture_output=True,
            text=True,
            env=env,
            timeout=timeout,
        )
        return result.returncode, result.stdout, result.stderr

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
        return str(self.test.home_dir / ".roadmaps" / roadmap / "graph.sock")

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
    catchable signals stopping the server gracefully, and the refusal to
    serve (or create) a roadmap with no graph store yet.
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

    def test_refuses_to_serve_a_roadmap_with_no_graph_store(self):
        """SPEC/COMMANDS.md "Serve": serve creates no graph directory that
        does not already exist. Unlike `execute`, which creates one on first
        use, a bare `graph serve` against a roadmap that has never run a
        graph statement must fail rather than materialise an empty store.
        """
        roadmap = self.test.create_roadmap("greenfield-project")
        server = GraphServeProcess(self.test, roadmap)
        self._servers.append(server)
        raised = False
        started = time.monotonic()
        try:
            server.start(timeout=5.0)
        except AssertionError:
            raised = True
        elapsed = time.monotonic() - started
        assert raised, "serving a roadmap with no graph store must not print a startup object"
        assert elapsed < 3.0, (
            f"the refusal must be immediate rather than waiting out the "
            f"startup timeout; took {elapsed:.2f}s"
        )
        assert server.proc.returncode == EXIT_DATABASE, (
            f"serving a roadmap with no graph store must exit 1; got "
            f"{server.proc.returncode}"
        )
        assert "no graph store" in server.stderr_text(), (
            f"expected a diagnostic naming the missing store; got "
            f"{server.stderr_text()!r}"
        )
        graph_dir = self.test.home_dir / ".roadmaps" / roadmap / "graph"
        assert not graph_dir.exists(), (
            "a refused serve must create no graph directory"
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
            f'Error: database error: cannot take the graph store lock for '
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

        challenger = GraphServeProcess(self.test, roadmap_b, socket_path=shared_socket)
        self._servers.append(challenger)
        raised = False
        try:
            challenger.start(timeout=PROBE_DEADLINE_S + 5.0)
        except AssertionError:
            raised = True
        assert raised, "the challenger must not announce a socket of its own"
        assert challenger.proc.returncode == EXIT_DATABASE, (
            f"got {challenger.proc.returncode}"
        )
        expected = f"Error: database error: a graph server is already serving {shared_socket}"
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

        server = GraphServeProcess(self.test, roadmap, socket_path=bad_socket)
        self._servers.append(server)
        raised = False
        try:
            server.start(timeout=5.0)
        except AssertionError:
            raised = True
        assert raised
        assert server.proc.returncode == EXIT_DATABASE, f"got {server.proc.returncode}"
        prefix = f"Error: database error: cannot bind {bad_socket}: "
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
        then again after the server has stopped and the graph has been
        reopened directly -- proving it was checkpointed, not merely held in
        the connection.
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
        assert json.loads(out) == {"ok": True}, out

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

        rc3, out3, err3 = self.run_cli(
            ["graph", "execute", "-r", roadmap, "--query",
             "MATCH (c:Component {key:'auth-api'})-[:GOVERNED_BY]->(d:Decision) "
             "RETURN d.key"]
        )
        assert rc3 == EXIT_OK, err3
        assert json.loads(out3) == {"columns": ["d.key"], "rows": [["use-oauth2"]]}, (
            "the write must have been checkpointed to the store the direct "
            "path reopens after the server has stopped"
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
        assert err.startswith("Error: database error: graph query failed: "), err
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
        expected = f"Error: database error: no graph server is listening on {socket_path}"
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
        expected = f"Error: database error: no graph server is listening on {socket_path}"
        assert err.splitlines()[0] == expected, err
        assert out == ""
        assert os.path.exists(socket_path), (
            "the stale socket is neither an error nor the caller's to remove "
            "(SPEC/GRAPH.md \"Server Resolution\" rule 1)"
        )


class TestExecuteRoutesThroughServer(GraphServerTestBase):
    """SPEC/GRAPH.md "Server Resolution" and "Serving on a Non-Default
    Socket": `rmp graph execute` resolves the socket before opening anything,
    sends the statement to a server that answers, and only takes the
    exclusive lock when nothing does. `--socket` lets it follow a server off
    the default path exactly as `graph client` does.
    """

    def test_execute_writes_reach_the_server_and_client_reads_them_back(self):
        roadmap = self.seeded_roadmap(
            "observability-platform-2",
            "CREATE (:Component {key:'metrics-collector', language:'go'})",
        )
        server = self.start_server(roadmap)

        rc, out, err = self.run_cli(
            ["graph", "execute", "-r", roadmap, "--query",
             "MATCH (c:Component {key:'metrics-collector'}) "
             "CREATE (a:Decision {key:'sample-at-1hz', "
             "title:'Sample metrics at 1Hz to bound cardinality'}) "
             "CREATE (c)-[:GOVERNED_BY]->(a)"]
        )
        assert rc == EXIT_OK, f"execute against a served roadmap must succeed; err={err!r}"
        assert json.loads(out) == {"ok": True}, out

        rc2, out2, err2 = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query",
             "MATCH (:Component)-[:GOVERNED_BY]->(d:Decision) RETURN d.key"]
        )
        assert rc2 == EXIT_OK, err2
        assert json.loads(out2) == {"columns": ["d.key"], "rows": [["sample-at-1hz"]]}, (
            "the write execute made must be visible through the SAME server "
            "the client reads from -- proving execute did not open its own "
            "store"
        )
        server.stop(signal.SIGINT)

    def test_execute_does_not_contend_for_the_lock_when_the_roadmap_is_served(self):
        """Regression guard for rmp task #366 DECISION #250: before `execute`
        carried `--socket` and resolution, a running server made every
        `execute` against that roadmap wait the whole 7.5s wait budget and
        then fail. A served `execute` must return promptly instead.
        """
        roadmap = self.seeded_roadmap(
            "observability-platform-3",
            "CREATE (:Component {key:'metrics-collector', language:'go'})",
        )
        server = self.start_server(roadmap)

        started = time.monotonic()
        rc, out, err = self.run_cli(
            ["graph", "execute", "-r", roadmap, "--query", "MATCH (n) RETURN count(n)"],
            timeout=WAIT_BUDGET_S,
        )
        elapsed = time.monotonic() - started

        assert rc == EXIT_OK, f"got {rc}, stderr={err!r}"
        assert elapsed < 2.0, (
            f"a served execute must not wait for the store lock; took "
            f"{elapsed:.2f}s (the wait budget is {WAIT_BUDGET_S}s)"
        )
        server.stop(signal.SIGINT)

    def test_execute_socket_flag_reaches_a_server_on_a_non_default_socket(self):
        roadmap = self.seeded_roadmap(
            "checkout-service-3",
            "CREATE (:Component {key:'cart-api', language:'go'})",
        )
        custom = str(self.test.home_dir / "custom2" / "checkout.sock")
        os.makedirs(os.path.dirname(custom), exist_ok=True)
        _assert_socket_path_fits(custom)
        server = self.start_server(roadmap, socket_path=custom)

        rc, out, err = self.run_cli(
            ["graph", "execute", "-r", roadmap, "--socket", custom,
             "--query", "MATCH (n) RETURN count(n)"]
        )
        assert rc == EXIT_OK, err
        assert json.loads(out) == {"columns": ["count(n)"], "rows": [[1]]}, out
        server.stop(signal.SIGINT)

    def test_execute_without_socket_flag_falls_into_the_lock_when_server_is_elsewhere(self):
        """SPEC/GRAPH.md "Serving on a Non-Default Socket", point 3: a server
        started on a non-default socket is followed only by an invocation
        that is GIVEN the same --socket. One that omits the flag resolves the
        derived (empty) path, finds nothing served there, and falls onto the
        direct path -- straight into the lock the non-default server holds
        for the roadmap, for the whole wait budget.
        """
        roadmap = self.seeded_roadmap(
            "checkout-service-4",
            "CREATE (:Component {key:'cart-api', language:'go'})",
        )
        custom = str(self.test.home_dir / "custom3" / "checkout.sock")
        os.makedirs(os.path.dirname(custom), exist_ok=True)
        _assert_socket_path_fits(custom)
        server = self.start_server(roadmap, socket_path=custom)

        started = time.monotonic()
        rc, out, err = self.run_cli(
            ["graph", "execute", "-r", roadmap, "--query", "MATCH (n) RETURN count(n)"],
            timeout=WAIT_BUDGET_S + 10,
        )
        elapsed = time.monotonic() - started

        assert rc == EXIT_DATABASE, f"got {rc}, stderr={err!r}"
        assert elapsed >= WAIT_BUDGET_S - 0.5, (
            f"the direct path must have waited the whole lock-contention "
            f"budget; took {elapsed:.2f}s"
        )
        server.stop(signal.SIGINT)


class TestSocketUnreachable(GraphServerTestBase):
    """SPEC/GRAPH.md "Server Resolution", the `Unreachable` state: the path
    is not a socket at all. This is the third of the ways
    internal/graphclient's own unit suite drives this state
    (TestResolve_APathThatIsNotASocketIsUnreachable), reproduced here through
    the built binary for both surfaces that resolve a socket and can fall
    back or fail on it.
    """

    def test_execute_reports_unreachable_for_a_regular_file_at_the_socket_path(self):
        roadmap = self.seeded_roadmap(
            "identity-service-4",
            "CREATE (:Component {key:'auth-api', language:'go'})",
        )
        socket_path = self.default_socket_path(roadmap)
        with open(socket_path, "w") as fh:
            fh.write("not a socket\n")

        rc, out, err = self.run_cli(
            ["graph", "execute", "-r", roadmap, "--query", "MATCH (n) RETURN count(n)"],
            timeout=PROBE_DEADLINE_S + 5,
        )
        assert rc == EXIT_DATABASE, f"got {rc}, stderr={err!r}"
        prefix = f"Error: database error: graph server unreachable at {socket_path}: "
        assert err.startswith(prefix), err
        assert out == "", "a failed resolution must not fall back and must write nothing"

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
        prefix = f"Error: database error: graph server unreachable at {socket_path}: "
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
            f"Error: database error: the connection to the graph server at "
            f"{socket_path} was lost; the statement's outcome is unknown"
        )
        assert err.splitlines()[0] == expected, err
        assert out == ""

        # Durability check, once the store is reachable again: the killed
        # statement must have left no partial write (SPEC/GRAPH.md
        # "Statement Time Budget" rule 2, "a cut statement rolls back
        # whole" -- here cut by the kill rather than by the deadline, and the
        # commit protocol is what makes the two indistinguishable in outcome).
        rc, out2, err2 = self.run_cli(
            ["graph", "execute", "-r", roadmap, "--query",
             "MATCH (n:Anomaly) RETURN count(n)"]
        )
        assert rc == EXIT_OK, err2
        assert json.loads(out2) == {"columns": ["count(n)"], "rows": [[0]]}, (
            f"an unacknowledged write must leave no partial trace; got {out2!r}"
        )

    def test_server_unanswered_within_the_backstop_deadline(self):
        """A write large enough that its forward pass is still running when
        the server's own 5s statement budget cuts it, and whose undo replay
        (SPEC/GRAPH.md "Statement Time Budget": "a statement cut while it is
        writing does not return promptly... nothing measured establishes a
        ceiling") outlasts the caller's 7.5s backstop while the CONNECTION
        stays intact and the server stays alive.
        """
        roadmap = self.seeded_roadmap(
            "observability-platform-5", self._seed_bulk(3000),
        )
        server = self.start_server(roadmap)
        socket_path = server.socket

        started = time.monotonic()
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query",
             "MATCH (a:MetricSample),(b:MetricSample),(c:MetricSample) CREATE ()"],
            timeout=WAIT_BUDGET_S + 20,
        )
        elapsed = time.monotonic() - started

        assert rc == EXIT_DATABASE, f"got {rc}, stderr={err!r}"
        expected = (
            f"Error: database error: the graph server at {socket_path} did "
            f"not answer within {WAIT_BUDGET_S}s; the statement's outcome is "
            f"unknown"
        )
        assert err.splitlines()[0] == expected, err
        assert out == ""
        assert elapsed >= WAIT_BUDGET_S - 0.5, (
            f"the failure must come from the caller's OWN backstop, not an "
            f"earlier one; took {elapsed:.2f}s"
        )

        assert server.is_alive(), (
            "the server must still be alive: this line is specified as "
            "\"the connection is intact... the server is alive\", which is "
            "what distinguishes it from a lost connection"
        )

        # Let the still-running undo finish and the server return to
        # quiescence before a clean stop, rather than compounding this test
        # with the separate (and separately tested) shutdown-under-load path.
        rc2 = server.stop(signal.SIGINT, timeout=60.0)
        assert rc2 == EXIT_OK, (
            f"the server must still shut down cleanly once the slow "
            f"statement finally finishes; got {rc2}"
        )

        # The cut write rolled back whole and left no checkpoint behind it.
        rc3, out3, err3 = self.run_cli(
            ["graph", "execute", "-r", roadmap, "--query",
             "MATCH (n) WHERE NOT n:MetricSample RETURN count(n)"]
        )
        assert rc3 == EXIT_OK, err3
        assert json.loads(out3) == {"columns": ["count(n)"], "rows": [[0]]}, (
            f"the cut CREATE() must have written nothing durable; got {out3!r}"
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
            "Error: database error: graph query exceeded the 5s statement "
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
        # (unsignalled) budget cut does.
        rc, out2, err2 = self.run_cli(
            ["graph", "execute", "-r", roadmap, "--query",
             "MATCH (n:Anomaly) RETURN count(n)"]
        )
        assert rc == EXIT_OK, err2
        assert json.loads(out2) == {"columns": ["count(n)"], "rows": [[0]]}, out2


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
            assert json.loads(out) == {"ok": True}, out

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
            assert json.loads(out) == {"ok": True}, out
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
