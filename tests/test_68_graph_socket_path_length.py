#!/usr/bin/env python3
"""
Test 68: the platform's socket-path bound, end to end (rmp task #412).

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

## The rule splits on who chose the path

`--socket` over the bound fails all three subcommands, and `graph execute` in
particular does NOT fall back to the store: the caller named a socket no process
can create. A DERIVED path over the bound fails `serve` and `client` -- neither
has anywhere else to go -- while `execute` opens the store, exactly as it does
for a socket nobody is listening on. TestTheDerivedPathIsValidatedOnTheSameRule
asserts all three outcomes, because the whole value of the rule is in the split:
an implementation that refused every surface would pass a check of the server
alone while withdrawing the two surfaces that still work.
"""

import inspect
import json
import os
import re
import shutil
import signal
import socket as socketlib
import subprocess
import sys
import tempfile
import time
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase, measure_socket_path_bound
from tests.test_65_graph_server_client_e2e import GraphServeProcess

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
        rc, out, err = self.run_cli(["graph", "execute", "-r", self.roadmap, "--query", SEED])
        # The seed runs through `graph execute` on the direct path, which is what
        # materialises ~/.roadmaps/<name>/graph/. Under DEEP_HOME that is the
        # criterion-66 execute half firing inside the fixture, so the failure
        # says so rather than reading as a broken harness.
        assert rc == EXIT_OK, (
            f"seeding failed: exit={rc} out={out!r} err={err!r}"
            + (
                "\n\nThe derived socket path of this fixture is over the platform's "
                "bound, and `graph execute` must still open the store: a derived "
                "path over the bound is a definite negative, not a refusal "
                "(SPEC/GRAPH.md 'Socket Path Length', rule 6)."
                if self.DEEP_HOME else ""
            )
        )

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

    def test_all_three_subcommands_refuse_a_supplied_path_over_the_bound(self):
        """SPEC/GRAPH.md 'Socket Path Length', rule 5: all three subcommands that
        publish --socket refuse it, with the same line and the same exit code.
        """
        socket_path = self.socket_path_of_length(self.bound + 1)
        invocations = {
            "serve": ["graph", "serve", "-r", self.roadmap, "--socket", socket_path],
            "client": ["graph", "client", "-r", self.roadmap, "--socket", socket_path, "--query", READ],
            "execute": ["graph", "execute", "-r", self.roadmap, "--socket", socket_path, "--query", READ],
        }
        lines = {}
        for name, args in invocations.items():
            rc, out, err = self.run_cli(args)
            self.assert_path_length_refusal(rc, out, err, socket_path)
            lines[name] = err.splitlines()[0]

        assert len(set(lines.values())) == 1, (
            f"the three subcommands publish different lines for one condition: {lines!r}"
        )


# ---------------------------------------------------------------------------
# Rule 5: execute names a socket rather than merely failing to find one
# ---------------------------------------------------------------------------

class TestExecuteDoesNotFallBackOnASuppliedPath(SocketLengthBase):
    """`graph execute --socket <over the bound>` must NOT run the statement
    against the store.

    Asserting the exit code alone would not prove it: a fallback that then
    failed for some other reason would look the same. The statement here is a
    WRITE, and the check is that the graph is unchanged afterwards -- read back
    through an invocation that DOES reach the store.
    """

    def test_a_supplied_path_over_the_bound_writes_nothing_to_the_store(self):
        socket_path = self.socket_path_of_length(self.bound + 1)
        write = "CREATE (:Spec {key:'must-not-be-written'})"

        rc, out, err = self.run_cli(
            ["graph", "execute", "-r", self.roadmap, "--socket", socket_path, "--query", write]
        )
        self.assert_path_length_refusal(rc, out, err, socket_path)

        rc, out, err = self.run_cli(
            ["graph", "execute", "-r", self.roadmap, "--query",
             "MATCH (s:Spec {key:'must-not-be-written'}) RETURN count(s) AS n"]
        )
        assert rc == EXIT_OK, f"reading back failed: exit={rc} stderr={err!r}"
        assert json.loads(out)["rows"] == [[0]], (
            f"the refused invocation reached the store and committed its write: {out!r}. "
            f"A path the caller NAMED and that cannot exist is evidence the invocation "
            f"was misunderstood, not evidence that the roadmap is unserved "
            f"(SPEC/GRAPH.md 'Socket Path Length', rule 5)"
        )

    def test_a_supplied_path_at_the_bound_still_falls_back_to_the_store(self):
        """The control that keeps the test above from being satisfied by an
        implementation that simply stopped falling back.

        Nothing is listening on this path either, but it is a path a socket
        could lawfully occupy, so the invocation takes the direct path and
        succeeds -- exactly as it did before the flag existed.
        """
        socket_path = self.socket_path_of_length(self.bound)
        assert not os.path.exists(socket_path)

        rc, out, err = self.run_cli(
            ["graph", "execute", "-r", self.roadmap, "--socket", socket_path, "--query", READ]
        )
        assert rc == EXIT_OK, (
            f"an absent but LAWFUL socket path must send the invocation to the "
            f"store, as it always did: exit={rc} stderr={err!r}"
        )
        assert json.loads(out)["rows"] == [["socket-path-length"]], out


# ---------------------------------------------------------------------------
# Criterion 66: the derived path, and the three-way split
# ---------------------------------------------------------------------------

class TestTheDerivedPathIsValidatedOnTheSameRule(SocketLengthBase):
    """A roadmap whose DERIVED socket path is over the bound, reached the way it
    was reached in practice: a three-character roadmap name under a deep home
    directory.

    All three outcomes are asserted, because the whole value of the rule is in
    the split. An implementation that refused every surface would pass a check
    of the server alone while withdrawing the two surfaces that still work.
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

    def test_execute_runs_against_the_store_and_exits_zero(self):
        rc, out, err = self.run_cli(["graph", "execute", "-r", self.roadmap, "--query", READ])
        assert rc == EXIT_OK, (
            f"`graph execute` against a roadmap whose derived socket path is over "
            f"the bound exited {rc}. A derived path over the bound is a definite "
            f"negative -- no server can exist there -- so the statement runs "
            f"against the store, exactly as it does for a socket that is absent "
            f"(SPEC/GRAPH.md 'Socket Path Length', rule 6). stderr={err!r}"
        )
        assert json.loads(out)["rows"] == [["socket-path-length"]], (
            f"the statement exited 0 without returning the seeded graph: {out!r}"
        )

    def test_execute_still_writes_and_commits(self):
        """The reading surface is not merely non-failing: it is the whole store.

        A roadmap whose home directory is deep enough to push the derived path
        over the bound loses the server and keeps both of the surfaces that
        existed before the server did.
        """
        rc, out, err = self.run_cli(
            ["graph", "execute", "-r", self.roadmap, "--query",
             "CREATE (:Spec {key:'deep-home-write'})"]
        )
        assert rc == EXIT_OK, f"exit={rc} stderr={err!r}"

        rc, out, err = self.run_cli(
            ["graph", "execute", "-r", self.roadmap, "--query",
             "MATCH (s:Spec {key:'deep-home-write'}) RETURN count(s) AS n"]
        )
        assert rc == EXIT_OK, f"exit={rc} stderr={err!r}"
        assert json.loads(out)["rows"] == [[1]], (
            f"the write did not commit through the direct path: {out!r}"
        )

    def test_a_server_on_a_short_supplied_socket_still_serves_this_roadmap(self):
        """The control for the three checks above.

        The roadmap's DERIVED path is unusable, but --socket names a short one,
        and the server starts, answers and stops. Without this, the class would
        also pass on an implementation that had simply stopped serving roadmaps
        under a deep home rather than refusing the derived path alone.
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
