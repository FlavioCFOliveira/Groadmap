#!/usr/bin/env python3
"""
Test 68: the platform's socket-path bound, end to end (rmp tasks #412, #427).

SPEC/GRAPH.md "Socket Path Length" is canonical for everything asserted here,
and Acceptance Criteria 64, 65, 66 and 67 are the four obligations this module
discharges. SPEC/COMMANDS.md "Graph Server Socket Error Lines" publishes the
line; character-for-character parity against that publication is
test_55_error_string_parity.py's job, and this module asserts the parts of the
line the criteria name -- the two numbers, the path, the remedy, and the absence
of the operating system's own text -- alongside the BEHAVIOUR none of the Go
suites can reach: three separate processes, a real server, and a real bind.

## The defect

    rmp graph serve -r <rdm> --socket /tmp/<a 122-byte path>.sock

    Error: graph server error: cannot bind <path>: listen unix <path>: bind: invalid argument

The path appears twice, the length appears nowhere, the limit appears nowhere,
and `invalid argument` is the kernel's phrase for "this does not fit in
sun_path" -- which nothing in the line says. It is reachable through the DEFAULT
path too: `~/.roadmaps/<name>/graph.sock` under a deep enough home directory goes
over the bound with an ordinary three-character roadmap name.

## The bound is measured here, never written down

Every length this module uses is derived from a figure it measures itself, by
binding real AF_UNIX listeners at increasing path lengths until one is refused
(see `measure_socket_path_bound`). That is Acceptance Criterion 67's requirement
in as many words, and the reason for it is that the bound is 107 bytes on Linux
and Windows and 103 on macOS, FreeBSD and OpenBSD. A module that asserted the
literal 107 would agree with a hard-coded implementation on this host and would
confirm the defect, silently, on the three operating systems where both are
wrong.

## The rule is uniform, however the path was chosen

An over-long resolved socket path fails every subcommand that resolves one, and
it fails BEFORE the probe, settled from the path alone. That holds for a
`--socket` value the caller named and for the path derived from the roadmap
alike: a path over the bound is evidence that no server can EVER answer there,
which is not the same fact as "none happens to be listening" and does not
warrant the same answer.

The rule used to be split on WHO chose the path -- an over-bound `--socket`
refused all three subcommands, while an over-bound DERIVED path refused `serve`
and `client` and let `graph execute` open the store and commit. rmp task #427
made it uniform, and the split is now doubly gone: `rmp graph execute` is
WITHDRAWN, `rmp graph <anything but serve|client>` exits 127, and no invocation
opens a graph store except a server. There is no surface left that could
resolve a path, find it unusable, and go somewhere else instead, so the tests
that asserted the split at that surface are retired rather than inverted a
second time -- each one recorded where its code was, with what covers it now.

Two subcommands resolve a socket, and both are asserted here: `serve` binds it
and `client` connects to it. What the uniform rule costs is asserted too, and
so is the recovery: a roadmap whose DERIVED path is over the bound is not lost,
because `--socket` naming a path inside the bound is accepted by both, so a
server can be started for it and reached.
TestTheDerivedPathIsValidatedOnTheSameRule asserts the refusal at both
subcommands AND that recovery, because a class that asserted only the refusals
would be satisfied by an implementation that had withdrawn the roadmap's graph
unconditionally.
"""

import inspect
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import (GraphServeProcess, GroadmapTestBase,
                             measure_socket_path_bound)

EXIT_OK = 0
EXIT_ERROR = 1

# The sentinel every refusal in this module carries. It names the failure class
# and determines the exit code (SPEC/ARCHITECTURE.md "Sentinel Error Catalogue"),
# and SPEC/GRAPH.md "Socket Path Length" rule 7 requires it to be UNCHANGED by
# this refusal: the message differs, the classification does not.
SENTINEL = "Error: graph server error: "

# The fragment of rmp's own text that identifies the line, carrying neither of
# the two numbers so it cannot accidentally assert one of them.
TOO_LONG = "socket path is too long"

# The kernel's phrase for a path that does not fit in sun_path. Its presence in
# the line is the defect this module exists against.
ERRNO_TEXT = "invalid argument"

# The two numbers the line carries, read back out of captured stderr.
REPORTED_LENGTH = re.compile(r" is (\d+) bytes and this platform allows at most (\d+)\.")

# A roadmap name of the shape the derived path was first observed to overflow
# with: three characters, under a deep home directory (rmp task #411's
# verification, at 139 bytes). Short on purpose -- the point of criterion 66 is
# that the bound is reached without an unusual roadmap name.
DEEP_ROADMAP = "ctr"

# One directory segment of the deep HOME. A real workspace path, not padding: the
# scenario is an agent working several directories down, which is where this was
# met in practice.
DEEP_SEGMENT = "deeply-nested-agent-workspace"

SEED = "CREATE (:Spec {key:'socket-path-length', status:'implemented'})"
READ = "MATCH (s:Spec) RETURN s.key"


# ---------------------------------------------------------------------------
# Measuring the bound
# ---------------------------------------------------------------------------

def reported_numbers(stderr: str):
    """Return (length, limit) as the binary reported them, or None."""
    match = REPORTED_LENGTH.search(stderr)
    if match is None:
        return None
    return int(match.group(1)), int(match.group(2))


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

class SocketLengthBase:
    """A roadmap with a materialised graph store, a short directory to build
    measured socket paths in, and the measured bound itself.
    """

    # Set by a subclass that needs a HOME deep enough to overflow the DERIVED
    # socket path. None means the ordinary short HOME the harness creates.
    DEEP_HOME = False
    ROADMAP_NAME = None

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

        if self.DEEP_HOME:
            self.test.home_dir = self._make_deep_home()
            self.test.roadmaps_dir = self.test.home_dir / ".roadmaps"

        # A short directory of its own for the measured socket paths, so their
        # length is decided by this module rather than by how deep the harness
        # happened to put HOME.
        self.socket_dir = tempfile.mkdtemp(prefix="rmpsock")
        self.bound = measure_socket_path_bound(self.socket_dir)

        self._servers = []

        self.roadmap = self.test.create_roadmap(self.ROADMAP_NAME)
        self.seed_the_graph()

    def teardown_method(self):
        for server in getattr(self, "_servers", []):
            try:
                if server.is_alive():
                    server.kill_dash_9()
            except Exception:  # noqa: BLE001 - teardown must not mask a failure
                pass
        if getattr(self, "socket_dir", None):
            shutil.rmtree(self.socket_dir, ignore_errors=True)
        if getattr(self, "test", None):
            self.test.teardown()

    def _make_deep_home(self) -> Path:
        """Build a HOME deep enough that ~/.roadmaps/<name>/graph.sock exceeds
        the platform's bound, out of ordinary directory names.

        The measurement has not run yet at this point, so the depth is grown
        against a generous ceiling rather than the measured figure; the fixture
        that uses it asserts the derived path really is over the bound before
        any test runs, so a short $TMPDIR cannot turn criterion 66 into a test
        of the ordinary case.
        """
        home = Path(self.test.test_dir)
        while len(str(home)) < 160:
            home = home / DEEP_SEGMENT
        home = home / "home"
        home.mkdir(parents=True)
        return home

    # ---- invocations ------------------------------------------------------

    def seed_socket_path(self):
        """The socket the fixture's seeding server binds.

        Under DEEP_HOME the roadmap's DERIVED path is over the bound and every
        surface refuses it, so the fixture takes the remedy the published line
        names: --socket pointing at a path inside the bound (SPEC/GRAPH.md
        'Socket Path Length', rule 6). Everywhere else the derivation is left to
        do its work, which is the ordinary case.
        """
        return self.socket_path_of_length(self.bound) if self.DEEP_HOME else None

    def seed_the_graph(self):
        """Materialise this fixture's graph store and put SEED into it.

        The seed used to run through `graph execute` on the direct path, which
        was then the only thing that created a store. That subcommand is
        withdrawn and nothing opens a store any more except a server, so the
        order is now the other way up: STARTING a server is what creates
        ~/.roadmaps/<name>/graph/ (SPEC/COMMANDS.md "Serve"), and the seed goes
        to it through `graph client`. The server is stopped again, so what the
        tests below meet is a roadmap with a graph and nothing holding it.

        Under DEEP_HOME this is also the fixture's own exercise of the recovery
        the published line promises, so a failure here says so rather than
        reading as a broken harness.
        """
        socket_path = self.seed_socket_path()
        server = self.start_server(socket_path=socket_path)
        args = ["graph", "client", "-r", self.roadmap]
        if socket_path is not None:
            args += ["--socket", socket_path]
        rc, out, err = self.run_cli(args + ["--query", SEED])
        stop_rc = server.stop(signal.SIGINT)
        assert rc == EXIT_OK, (
            f"seeding failed: exit={rc} out={out!r} err={err!r}"
            + (
                "\n\nThe derived socket path of this fixture is over the platform's "
                "bound, so both the server and the client name a short one with "
                "--socket. A lawful path inside the bound must be accepted by "
                "both, which is the recovery the published line names "
                "(SPEC/GRAPH.md 'Socket Path Length', rule 6)."
                if self.DEEP_HOME else ""
            )
        )
        assert stop_rc == EXIT_OK, (
            f"the seeding server did not stop cleanly (exit={stop_rc}), so the "
            f"seed may not have been checkpointed; stderr={server.stderr_text()!r}"
        )

    def run_cli(self, args, timeout: float = 30.0):
        """One ./bin/rmp invocation against this fixture's HOME, returning
        (exit_code, stdout, stderr). Never raises on a non-zero exit: every
        caller here inspects the code itself.
        """
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        result = subprocess.run(
            [self.test.cli_path] + args,
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            env=env,
            timeout=timeout,
        )
        return result.returncode, result.stdout, result.stderr

    def start_server(self, socket_path: str = None, timeout: float = 15.0):
        server = GraphServeProcess(self.test, self.roadmap, socket_path=socket_path)
        self._servers.append(server)
        server.start(timeout=timeout)
        return server

    # ---- measured paths ---------------------------------------------------

    def socket_path_of_length(self, length: int) -> str:
        padding = length - len(self.socket_dir) - 1
        assert padding >= 1, (
            f"cannot build a {length}-byte path inside {self.socket_dir!r}"
        )
        path = os.path.join(self.socket_dir, "s" * padding)
        assert len(path) == length, (len(path), length)
        return path

    def derived_socket_path(self) -> str:
        return str(self.test.home_dir / ".roadmaps" / self.roadmap / "graph.sock")

    # ---- shared assertions -----------------------------------------------

    def assert_path_length_refusal(self, rc, out, err, path):
        """Assert one invocation was refused by the path-length rule, with the
        published line and nothing on stdout.

        Every clause of Acceptance Criterion 65 is here: the exit code, the
        sentinel, the path, the two numbers ASSERTED SEPARATELY, the remedy, and
        the absence of the operating system's own text.
        """
        assert rc == EXIT_ERROR, (
            f"exit={rc}, want {EXIT_ERROR}. stdout={out!r} stderr={err!r}"
        )
        assert out == "", (
            f"a failing invocation wrote to stdout: {out!r} "
            f"(SPEC/COMMANDS.md 'Failing Invocations Write Nothing to Stdout')"
        )

        line = err.splitlines()[0] if err else ""
        assert line.startswith(SENTINEL), (
            f"the refusal does not carry the graph-server sentinel, which is what "
            f"determines the exit code: {line!r}"
        )
        assert TOO_LONG in line, (
            f"the refusal does not name the cause: {line!r}"
        )
        assert path in line, (
            f"the refusal does not name the resolved path {path!r}: {line!r}"
        )
        assert ERRNO_TEXT not in line, (
            f"the refusal carries the operating system's own text, which is the "
            f"defect rmp task #412 exists against: {line!r}"
        )
        assert "--socket" in line, (
            f"the refusal names no remedy: {line!r}"
        )

        numbers = reported_numbers(line)
        assert numbers is not None, (
            f"the refusal reports no length/limit pair: {line!r}"
        )
        length, limit = numbers
        assert length == len(path), (
            f"the refusal reports {length} bytes for a path of {len(path)}; the "
            f"number must be the length of the path supplied, not the limit "
            f"printed twice. line={line!r}"
        )
        assert limit == self.bound, (
            f"the refusal reports a limit of {limit} and this platform was "
            f"MEASURED to bind at most {self.bound} bytes. A hard-coded limit is "
            f"the usual cause, and it is wrong on three of the five operating "
            f"systems this project targets. line={line!r}"
        )
        assert length != limit, (
            f"the two numbers coincide ({length}), so this assertion cannot tell "
            f"a message that prints one of them twice from one that prints both"
        )
        return length, limit


# ---------------------------------------------------------------------------
# Criterion 64 and 65: a supplied path, at the bound and one byte over
# ---------------------------------------------------------------------------

class TestASuppliedPathAtAndOverTheBound(SocketLengthBase):
    """Both halves of Acceptance Criterion 64, against one roadmap.

    Keeping the at-the-bound half is what the criterion insists on: a check of
    the refusal alone passes on an implementation that is one byte too strict,
    and such an implementation refuses a path the kernel accepts on every
    platform at once.
    """

    def test_a_path_of_exactly_the_bound_binds_and_serves(self):
        socket_path = self.socket_path_of_length(self.bound)

        server = self.start_server(socket_path=socket_path)
        assert server.socket == socket_path, (
            f"the server announced {server.socket!r} for --socket {socket_path!r}; "
            f"a path of exactly the bound must be followed literally"
        )
        assert os.path.exists(socket_path), (
            f"the server announced {socket_path!r} and bound nothing there"
        )

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", self.roadmap, "--socket", socket_path, "--query", READ]
        )
        assert rc == EXIT_OK, (
            f"a server on a path of exactly the bound did not answer: "
            f"exit={rc} stderr={err!r} server_stderr={server.stderr_text()!r}"
        )
        result = json.loads(out)
        assert result["rows"] == [["socket-path-length"]], (
            f"the server answered, but not with the seeded graph: {result!r}"
        )

        assert server.stop(signal.SIGINT) == EXIT_OK, (
            "a server on a path of exactly the bound must stop cleanly"
        )

    def test_a_path_one_byte_over_the_bound_is_refused(self):
        socket_path = self.socket_path_of_length(self.bound + 1)

        rc, out, err = self.run_cli(["graph", "serve", "-r", self.roadmap, "--socket", socket_path])
        self.assert_path_length_refusal(rc, out, err, socket_path)

        assert not os.path.exists(socket_path), (
            f"a refused server left a file at {socket_path!r}; the refusal "
            f"precedes the unlink and the bind, so a server that cannot start "
            f"touches nothing (SPEC/GRAPH.md 'Server Startup', step 1)"
        )

    def test_the_reported_limit_is_the_measured_one(self):
        """Acceptance Criterion 67, stated as its own check.

        The comparison is against a figure this module measured by binding real
        sockets, so it fails against any hard-coded value on at least one
        supported platform and against a correct derivation on none.
        """
        socket_path = self.socket_path_of_length(self.bound + 1)
        rc, out, err = self.run_cli(["graph", "serve", "-r", self.roadmap, "--socket", socket_path])
        assert rc == EXIT_ERROR, f"exit={rc}: {err!r}"

        numbers = reported_numbers(err)
        assert numbers is not None, f"no length/limit pair in {err!r}"
        _, limit = numbers
        assert limit == self.bound, (
            f"the binary reports a limit of {limit}; binding real sockets on this "
            f"platform measured {self.bound}"
        )

    def test_both_subcommands_refuse_a_supplied_path_over_the_bound(self):
        """SPEC/GRAPH.md 'Socket Path Length', rule 5: every subcommand that
        publishes --socket refuses it, with the same line and the same exit code.

        There are two of them. The third invocation this case used to make,
        `graph execute --socket <over the bound>`, is retired with the
        subcommand: `rmp graph <anything but serve|client>` exits 127, and the
        refusal it asserted has no surface to be published at.
        """
        socket_path = self.socket_path_of_length(self.bound + 1)
        invocations = {
            "serve": ["graph", "serve", "-r", self.roadmap, "--socket", socket_path],
            "client": ["graph", "client", "-r", self.roadmap, "--socket", socket_path, "--query", READ],
        }
        lines = {}
        for name, args in invocations.items():
            rc, out, err = self.run_cli(args)
            self.assert_path_length_refusal(rc, out, err, socket_path)
            lines[name] = err.splitlines()[0]

        assert len(set(lines.values())) == 1, (
            f"the two subcommands publish different lines for one condition: {lines!r}"
        )

    def test_a_lawful_supplied_path_with_nothing_listening_is_a_different_refusal(self):
        """The control that keeps the refusal above from being satisfied by an
        implementation that refuses every unserved path.

        Nothing is listening on this path either, and it is one byte shorter
        than the path refused above. Because it is a path a socket could
        lawfully occupy, the length check passes and the invocation is answered
        with the OTHER published line -- the one for a socket nothing is serving
        -- which is what makes the refusal at bound + 1 a statement about the
        LENGTH rather than about the absence of a server.

        It replaces TestExecuteDoesNotFallBackOnASuppliedPath
        .test_a_supplied_path_at_the_bound_still_falls_back_to_the_store, whose
        control was that `graph execute` reached the store from here. There is
        no store to reach any more, so what is asserted instead is the line the
        one remaining surface publishes.
        """
        socket_path = self.socket_path_of_length(self.bound)
        assert not os.path.exists(socket_path)

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", self.roadmap, "--socket", socket_path, "--query", READ]
        )
        assert rc == EXIT_ERROR, f"exit={rc}, want {EXIT_ERROR}. stderr={err!r}"
        assert out == "", f"a failing invocation wrote to stdout: {out!r}"
        line = err.splitlines()[0]
        assert line == (
            f"Error: graph server error: no graph server is listening on {socket_path}"
        ), f"got {line!r}"
        assert TOO_LONG not in line, (
            f"a path of exactly the bound is lawful, and must not be refused for "
            f"its length: {line!r}"
        )


# ---------------------------------------------------------------------------
# RETIRED: TestExecuteDoesNotFallBackOnASuppliedPath
#
# The class existed because `rmp graph execute` had somewhere else to go when a
# socket could not be reached, and its subject was that it must NOT go there
# when the path was over the bound. The subcommand is withdrawn and no
# invocation opens a graph store any more, so there is no fallback to forbid
# and nothing left to assert. Its two cases:
#
#   test_a_supplied_path_over_the_bound_writes_nothing_to_the_store
#       UNREACHABLE, and vacuous if written against `graph client`: a client
#       refused for the path's length has no store to write to even if the
#       refusal were removed, so the read-back could not fail. The refusal
#       itself is asserted by TestASuppliedPathAtAndOverTheBound
#       .test_both_subcommands_refuse_a_supplied_path_over_the_bound, which
#       checks the exit code, the sentinel, both numbers, the path, the remedy
#       and the absence of the kernel's own text.
#
#   test_a_supplied_path_at_the_bound_still_falls_back_to_the_store
#       REPLACED by TestASuppliedPathAtAndOverTheBound
#       .test_a_lawful_supplied_path_with_nothing_listening_is_a_different_refusal.
#       It was the control proving the refusal one byte higher is about the
#       LENGTH and not about the absence of a listener; the replacement makes
#       the same point at the surviving surface, by asserting the OTHER
#       published line. The at-the-bound path is also proved bindable and
#       serviceable by test_a_path_of_exactly_the_bound_binds_and_serves.
# ---------------------------------------------------------------------------


# ---------------------------------------------------------------------------
# Criterion 66: the derived path, refused on the same rule
# ---------------------------------------------------------------------------

class TestTheDerivedPathIsValidatedOnTheSameRule(SocketLengthBase):
    """A roadmap whose DERIVED socket path is over the bound, reached the way it
    was reached in practice: a three-character roadmap name under a deep home
    directory.

    Both subcommands are asserted to refuse it with one line, and the RECOVERY
    is asserted beside them. Both halves are load-bearing. Without the refusals,
    an implementation that validated only a path the caller NAMED would pass;
    without the recovery, one that had withdrawn the roadmap's graph
    unconditionally would pass too, and that is not what the rule says
    (SPEC/GRAPH.md 'Socket Path Length', rules 5 and 6).

    The class used to assert a third refusal and a second recovery, both at
    `rmp graph execute`, because that subcommand needed no socket and reached
    the store regardless: the case for the refusal was rmp task #427's own
    inversion, and the case for the recovery was that `--socket` inside the
    bound sent the statement to the store. Both are retired with the
    subcommand -- see the comments where they stood.
    """

    DEEP_HOME = True
    ROADMAP_NAME = DEEP_ROADMAP

    def setup_method(self):
        super().setup_method()
        self.derived = self.derived_socket_path()
        assert len(os.fsencode(self.derived)) > self.bound, (
            f"the derived path is {len(os.fsencode(self.derived))} bytes and the "
            f"measured bound is {self.bound}, so this class is exercising the "
            f"ordinary case rather than the one it exists for: {self.derived!r}"
        )

    def test_serve_refuses_the_derived_path_with_the_same_line(self):
        rc, out, err = self.run_cli(["graph", "serve", "-r", self.roadmap])
        self.assert_path_length_refusal(rc, out, err, self.derived)
        assert "--socket" not in " ".join(["graph", "serve", "-r", self.roadmap]), (
            "this check must run with NO --socket flag at all"
        )

    def test_client_refuses_the_derived_path_with_the_same_line(self):
        rc, out, err = self.run_cli(["graph", "client", "-r", self.roadmap, "--query", READ])
        self.assert_path_length_refusal(rc, out, err, self.derived)

    # RETIRED: test_execute_refuses_the_derived_path_with_the_same_line.
    # It asserted the half rmp task #427 inverted -- that `graph execute`, the
    # one command-line surface with somewhere else to go, was bound by the rule
    # too. `rmp graph execute` is withdrawn, so the surface it guarded no longer
    # exists and nothing needs to cover it; the rule itself is asserted at both
    # remaining surfaces by the two cases above and the one below.

    def test_both_subcommands_refuse_the_derived_path_with_one_line(self):
        """One condition, one line, on every surface that publishes one.

        Asserting the two separately leaves room for two wordings of the same
        refusal; the whole point of the uniform rule is that an operator meets
        the same answer at whichever surface they reach first.
        """
        invocations = {
            "serve": ["graph", "serve", "-r", self.roadmap],
            "client": ["graph", "client", "-r", self.roadmap, "--query", READ],
        }
        lines = {}
        for name, args in invocations.items():
            rc, out, err = self.run_cli(args)
            self.assert_path_length_refusal(rc, out, err, self.derived)
            lines[name] = err.splitlines()[0]

        assert len(set(lines.values())) == 1, (
            f"the two subcommands publish different lines for one derived-path "
            f"condition: {lines!r}"
        )

    # RETIRED: test_execute_does_not_reach_the_store_on_the_derived_path.
    # Its subject was that the refusal was TOTAL -- that `graph execute`, having
    # been refused the derived path, did not quietly reach the store and commit
    # anyway -- and it proved it by reading the graph back. With `rmp graph
    # execute` withdrawn there is no invocation that can open a store, so a
    # refused `graph client` has nowhere to write even if its refusal were
    # removed, and the read-back could not fail: written against the surviving
    # surface the case would pass vacuously. What it guarded is now structural
    # rather than tested, and the refusal itself is asserted by
    # test_client_refuses_the_derived_path_with_the_same_line.

    # RETIRED: test_execute_recovers_through_a_socket_path_inside_the_bound.
    # It was Acceptance Criterion 66's recovery half at the `graph execute`
    # surface: a short --socket passed the length check, resolved as not served,
    # and the statement ran against the store. That route is gone with the
    # subcommand. The recovery itself is NOT gone and is still asserted, twice:
    # by test_a_server_on_a_short_supplied_socket_still_serves_this_roadmap
    # below, and by this fixture's own seeding, which is the only way its graph
    # comes to hold anything at all.

    def test_a_server_on_a_short_supplied_socket_still_serves_this_roadmap(self):
        """Acceptance Criterion 66's recovery half, and the control for the
        refusals above.

        The roadmap's DERIVED path is unusable, but --socket names a short one,
        and the server starts, answers and stops. Without this, the class would
        also pass on an implementation that had simply stopped serving roadmaps
        under a deep home rather than refusing the derived path alone -- and it
        is what makes the remedy the published line names truthful: a roadmap
        whose derived path is over the bound is reachable, through both
        subcommands, by naming a path that is not.
        """
        socket_path = self.socket_path_of_length(self.bound)
        server = self.start_server(socket_path=socket_path)
        assert server.socket == socket_path

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", self.roadmap, "--socket", socket_path, "--query", READ]
        )
        assert rc == EXIT_OK, f"exit={rc} stderr={err!r} server={server.stderr_text()!r}"
        assert json.loads(out)["rows"] == [["socket-path-length"]], out

        assert server.stop(signal.SIGINT) == EXIT_OK


def _run_all():
    """Discover and run every Test* class defined in this module.

    Enumerating the module's own namespace rather than naming the classes in a
    fixed list means a class added later cannot silently fail to run.
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
        for method in sorted(name for name in dir(cls) if name.startswith("test_")):
            label = f"{cls.__name__}.{method}"
            instance = cls()
            started = time.monotonic()
            try:
                # The fixture runs INSIDE the try: a fixture that cannot be built
                # is a finding about the code under test as often as it is a
                # broken harness, and one that escaped here would abort the whole
                # module and report the remaining checks as neither passed nor
                # failed.
                instance.setup_method()
                getattr(instance, method)()
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
    print(f"Socket path length tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\nFAIL {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if _run_all() else 1)
