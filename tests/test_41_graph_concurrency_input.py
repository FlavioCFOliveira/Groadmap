#!/usr/bin/env python3
"""
Test 41: write concurrency inside the graph server, and Cypher input validation
at `rmp graph client`.

End-to-end backstop against the compiled ./bin/rmp for five audit findings.

The graph is reachable only through a running `rmp graph serve`, spoken to by
`rmp graph client` (SPEC/GRAPH.md section "The Dedicated Graph Server"). Every
case below that has to reach a graph therefore starts a server first and sends
its statement through the client. The cases that are refused before a socket is
ever resolved deliberately run with NOTHING listening, and each one asserts that
nothing is: an argument-level refusal that needed a server would not be an
argument-level refusal.

- #39 (CRITICAL): concurrent graph writers must not lose acknowledged writes.
  Two writers that interleave their open -> commit -> checkpoint -> WAL-truncate
  sequences could once let one writer's full-snapshot checkpoint overwrite the
  other's committed-but-unseen write and then truncate the WAL holding it,
  silently dropping an acknowledged write.

  The race now runs where the product is actually built to take it: eight
  writers against ONE server, each through its own `rmp graph client`
  (SPEC/GRAPH.md section "Concurrency Inside the Server"). The store detects a
  serialisation collision rather than preventing it, and the client re-sends the
  losing statement under its retry policy, so contention is ordinarily invisible
  from the outside; a writer that loses every attempt of that budget exits 1 with
  the published write-conflict line and has written nothing. The invariant is the
  finding's own, and it is asserted by READ-BACK rather than by exit codes: the
  set of nodes in the store equals EXACTLY the set of writers that returned exit
  0, one node each. No acknowledged write is lost, no failed write leaves a
  phantom node, and no retry applies a committed write twice.
  (SPEC/GRAPH.md section "Concurrency and Recovery"; Acceptance Criterion 16.)

- #26/#27 (#52): `--query` with no value, or whose value is the next flag, must
  fail with exit 2 (SPEC/GRAPH.md section "Cypher Input Source and Precedence",
  rule 4), never silently fall back to stdin or swallow the following flag.

- #28 (#57): an unknown flag to a graph subcommand must fail with exit 2
  (SPEC/ARCHITECTURE.md unknown-flag rule), not be silently ignored.

- #81: a `--query` value that is a negative numeric literal (for example
  `-1 RETURN 1` or `-.5`) is NOT flag-like; it is a legitimate query value and
  must reach the ENGINE, failing exit 1 only on its own Cypher invalidity, never
  be rejected as a missing value with exit 2 (SPEC/GRAPH.md section "Cypher Input
  Source and Precedence", rule 4). Reaching the engine is now an observable
  event rather than an inference: the statement is sent to a running server and
  the refusal that comes back is the engine's own parse diagnostic, told apart
  from the identically-coded "no server is listening" by the published line each
  carries.

- #181: the standard-input read is BOUNDED and a standard input that supplies no
  query is refused at once. Both halves of one unbounded read, and they carry
  DIFFERENT exit codes on purpose:

  - a producer that writes too much. 256 MiB offered to the statement-running
    subcommand reached 867 MB of resident memory and 15.9 s of wall time before
    anything rejected it. A query over 1 MiB is now refused with exit 6 after the
    read has consumed a bounded amount (SPEC/GRAPH.md sections "Maximum Query
    Length" and "Bounded Standard-Input Read"; Acceptance Criterion 40).
  - a producer that writes nothing. With `--query` absent and a terminal on
    standard input, the command waited for a query nobody was going to type: one
    invocation hung for roughly forty minutes, printing nothing. Standard input
    that is empty, whitespace only, or a terminal is now refused with exit 2
    (SPEC/GRAPH.md section "Standard Input That Supplies No Query"; Acceptance
    Criterion 41).

  Both refusals are made by `readQuery`, which runs before the roadmap's
  existence is checked and long before a socket is resolved, so they are asserted
  here with no server listening at all.

  The over-long refusal on the `--query` FLAG is not reachable from here and is
  proved by the Go test instead: execve caps a single argument at 128 KiB on
  Linux, so no argument vector can carry a query over 1 MiB. The cap is applied
  to both sources in internal/commands/graph.go, and
  internal/commands/graph_query_length_test.go asserts the flag door directly.
"""

import json
import os
import pty
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase, assert_graph_write_shape


EXIT_OK = 0
EXIT_DB = 1
EXIT_MISUSE = 2
EXIT_VALIDATION = 6

# The maximum query length and the two refusals, spelled exactly as SPEC/GRAPH.md
# publishes them. They are literals rather than values derived from the binary, so
# a change to either the number or the wording fails this file.
MAX_QUERY_BYTES = 1048576
TOO_LONG_MESSAGE = (
    "Error: validation error: query exceeds maximum length of 1048576 bytes"
)
NO_QUERY_MESSAGE = "Error: required parameter missing: no query supplied"

# The two exit-1 lines this module has to tell apart, published by
# SPEC/COMMANDS.md section "Client Error Cases". `client` reports the engine's
# refusal and the absence of a server with the same exit code, so a test claiming
# a statement "reached the engine" has to assert the line and not the code.
ENGINE_FAILURE_PREFIX = "Error: graph engine error: graph query failed: "
NO_SERVER_MESSAGE = "no graph server is listening"

# The line a writer prints when it lost every attempt of the retry policy. It is
# the ONLY acceptable non-success for a concurrent writer: the statement is valid
# and nothing was written, which is the invariant this module exists to defend.
WRITE_CONFLICT_LINE = (
    "Error: graph engine error: graph write conflict: another writer committed "
    "first on every attempt within the 2.5s retry budget; nothing was written. "
    "The statement is valid — run it again, and spread concurrent writes "
    "across distinct nodes."
)

# How long the command may take to refuse a standard input that supplies no
# query. The contract is that it does not wait at all, so the honest budget is
# milliseconds; ten seconds is chosen only so a loaded machine cannot produce a
# false failure, and it is still far below the forty minutes the defect burned.
NO_WAIT_BUDGET_SECONDS = 10.0


class TestGraphConcurrencyInput:

    # The eight files sprint 43 touched in the graph packages. Eight distinct
    # writers, each creating one node, is what makes the read-back invariant
    # legible: one acknowledged write, one node, and the paths say which writer
    # produced which.
    CONCURRENT_WRITER_PATHS = [
        "internal/commands/graph.go",
        "internal/commands/graph_client.go",
        "internal/commands/graph_serve.go",
        "internal/commands/graph_socket.go",
        "internal/commands/registry_graph.go",
        "internal/graphlock/graphlock.go",
        "internal/graphserve/graphserve.go",
        "internal/web/data.go",
    ]

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        # Started on demand rather than in setup: the cases that assert an
        # argument-level refusal must run with nothing listening, and starting a
        # server for them would destroy the very thing they demonstrate.
        self.server = None

    def teardown_method(self):
        self.test.teardown()

    # ---- fixture helpers ---------------------------------------------

    def serving(self):
        """Start this roadmap's graph server on first use and return it.

        Starting a server against a roadmap that has never had a graph is what
        CREATES the graph (SPEC/GRAPH.md section "Server Startup", step 1), so
        this is also the only thing that has to happen before the first
        statement can be sent.
        """
        if self.server is None:
            self.server = self.test.start_graph_server(self.roadmap)
        return self.server

    def assert_nothing_is_listening(self):
        """Assert that this roadmap has no server, so a refusal observed next
        was reached without one.

        The socket is the whole of the evidence: `client` derives
        ~/.roadmaps/<name>/graph.sock and a server binds exactly that path, so a
        path that does not exist is a roadmap nothing is serving.
        """
        socket_path = self.test.default_socket_path(self.roadmap)
        assert self.server is None, (
            "this case must run with no server: it asserts a refusal that "
            "happens before a socket is resolved"
        )
        assert not os.path.exists(socket_path), (
            f"something is bound at {socket_path}; this case is no longer "
            f"proving that the refusal precedes the socket"
        )

    def _client_env(self):
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        return env

    def _popen_writer(self, path):
        """Launch one `rmp graph client` writer without waiting for it, so the
        eight of them are in flight together.

        The harness's own graph_client() blocks until the invocation finishes,
        which is right for every other case here and wrong for this one: a race
        needs the writers started before any of them is collected.
        """
        return subprocess.Popen(
            [
                self.test.cli_path, "graph", "client", "-r", self.roadmap,
                "--query", f"CREATE (n:CodeFile {{path:'{path}'}})",
            ],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
            env=self._client_env(),
        )

    # ---- #39: concurrent writers never lose an acknowledged write ----

    def test_concurrent_writes_lose_nothing(self):
        server = self.serving()

        # Launch all writers as close to simultaneously as possible, against the
        # one server that owns the store.
        procs = {path: self._popen_writer(path) for path in self.CONCURRENT_WRITER_PATHS}
        acknowledged = set()
        for path, proc in procs.items():
            stdout, stderr = proc.communicate(timeout=60)
            if proc.returncode == EXIT_OK:
                acknowledged.add(path)
                # Each acknowledgement is checked for WHAT it acknowledged: one
                # node, one property, one label. A retry that re-applied a
                # committed statement would show up here as a second node before
                # the read-back below ever ran.
                assert_graph_write_shape(
                    json.loads(stdout),
                    context=f"the writer for {path!r}",
                    counters={
                        "nodesCreated": 1,
                        "propertiesWritten": 1,
                        "labelsAdded": 1,
                    },
                )
            else:
                # The only acceptable non-success is an exhausted retry budget,
                # and it is asserted as the published line rather than as a bare
                # exit 1 -- which the engine also uses for a statement it
                # refused, and the client for a server it never reached.
                assert proc.returncode == EXIT_DB, (
                    f"the writer for {path!r} failed with unexpected exit "
                    f"{proc.returncode}; stderr={stderr!r}"
                )
                assert WRITE_CONFLICT_LINE in stderr, (
                    f"the writer for {path!r} exited 1 for something other than "
                    f"an exhausted retry budget; stderr={stderr!r}"
                )

        assert acknowledged, (
            "expected at least one concurrent writer to succeed; the server's "
            f"stderr was {server.stderr_text()!r}"
        )

        # Invariant: the store's contents equal EXACTLY the writers that returned
        # exit 0 -- no acknowledged write was lost, no failed write left a
        # phantom node, and no retry wrote a node twice. The ORDER BY makes the
        # row list comparable to the sorted set, so a duplicate fails the
        # comparison instead of vanishing into a set.
        result = self.test.graph_ok(
            self.roadmap, query="MATCH (n:CodeFile) RETURN n.path ORDER BY n.path")
        present = [row[0] for row in result["rows"]]
        assert present == sorted(acknowledged), (
            f"store contents must equal acknowledged writes, once each; "
            f"present={present!r} acknowledged={sorted(acknowledged)!r}"
        )
        print(f"✓ {len(acknowledged)} of {len(self.CONCURRENT_WRITER_PATHS)} "
              f"concurrent writers were acknowledged, and the store holds "
              f"exactly those")

    # ---- #52: --query value handling --------------------------------

    def test_query_flag_without_value_fails_exit_2(self):
        self.assert_nothing_is_listening()
        code, stdout, stderr = self.test.graph_client(self.roadmap, extra=["--query"])
        assert code == EXIT_MISUSE, (
            f"--query with no value must exit {EXIT_MISUSE}, got {code}; "
            f"stderr={stderr!r}"
        )
        assert NO_QUERY_MESSAGE in stderr, (
            f"the refusal must be the missing-value one; stderr={stderr!r}")
        assert stdout == "", f"a failing invocation writes nothing to stdout; got {stdout!r}"

    def test_query_flag_followed_by_flag_fails_exit_2(self):
        self.assert_nothing_is_listening()
        code, stdout, stderr = self.test.graph_client(
            self.roadmap, extra=["--query", "--bogus"])
        assert code == EXIT_MISUSE, (
            f"--query whose value is a flag must exit {EXIT_MISUSE} (not swallow "
            f"it), got {code}; stderr={stderr!r}"
        )
        assert NO_QUERY_MESSAGE in stderr, (
            f"the following flag must be reported as a missing value, not "
            f"consumed as one; stderr={stderr!r}")
        assert stdout == "", f"a failing invocation writes nothing to stdout; got {stdout!r}"

    # ---- #81: negative numeric --query value reaches the engine ------

    def _assert_reaches_the_engine(self, query, description):
        """Send `query` to a running server and assert the ENGINE refused it.

        Exit 1 alone proves nothing here: `client` exits 1 both for a statement
        the engine rejected and for a roadmap nothing is serving. The published
        lines are what separate them, so both are asserted -- the engine's own
        prefix must be present and the no-server line must be absent.
        """
        self.serving()
        code, stdout, stderr = self.test.graph_client(self.roadmap, query=query)
        assert code == EXIT_DB, (
            f"{description}: a negative-numeric --query value must be accepted "
            f"as the value and handed to the engine (exit {EXIT_DB}), not "
            f"rejected as missing (exit {EXIT_MISUSE}); got {code}, "
            f"stderr={stderr!r}"
        )
        assert NO_SERVER_MESSAGE not in stderr, (
            f"{description}: the statement never reached a server, so this case "
            f"proves nothing about the engine; stderr={stderr!r}"
        )
        assert stderr.startswith(ENGINE_FAILURE_PREFIX), (
            f"{description}: the refusal must be the engine's own diagnostic, "
            f"which is what shows the value was handed to it; stderr={stderr!r}"
        )
        assert stdout == "", f"{description}: stdout={stdout!r}"

    def test_query_negative_numeric_value_reaches_engine(self):
        # "-1 RETURN 1" is a negative numeric literal, not a flag. It must be
        # accepted as the query value and handed to the engine, which rejects it
        # as invalid Cypher — NOT rejected as a missing value with exit 2.
        self._assert_reaches_the_engine("-1 RETURN 1", "a '-1 RETURN 1' value")

    def test_query_leading_decimal_point_value_reaches_engine(self):
        # "-.5" begins with '-' then a decimal point: a numeric literal, not a
        # flag. It too must reach the engine, exercising the decimal-point branch
        # of the flag-like check — never the missing-value exit 2.
        self._assert_reaches_the_engine("-.5", "a '-.5' value")

    # ---- #57: unknown flags rejected --------------------------------

    def test_unknown_flag_rejected_exit_2(self):
        # A valid query is supplied via stdin so the only problem is the unknown
        # flag, and no server is started so the refusal is shown to precede the
        # socket as well as the engine.
        self.assert_nothing_is_listening()
        code, stdout, stderr = self.test.graph_client(
            self.roadmap,
            stdin_text="MATCH (n:CodeFile) RETURN n.path",
            extra=["--bogus"],
        )
        assert code == EXIT_MISUSE, (
            f"an unknown graph flag must exit {EXIT_MISUSE}, got {code}; "
            f"stderr={stderr!r}"
        )
        assert "unknown flag: --bogus" in stderr, (
            f"the flag must be named in the refusal; stderr={stderr!r}")
        assert stdout == "", f"a failing invocation writes nothing to stdout; got {stdout!r}"

    # ---- #181: the bounded read and the refusals it does not collapse ----

    def _source_file_paths(self):
        """Every SourceFile path in the roadmap's graph, as a sorted list.

        Used to assert that a refused query changed nothing. The refusal happens
        before the roadmap's existence is checked and before a socket is
        resolved, so the graph must be byte-for-byte the graph it was.
        """
        result = self.test.graph_ok(
            self.roadmap,
            query="MATCH (n:SourceFile) RETURN n.path ORDER BY n.path")
        return [row[0] for row in result["rows"]]

    def test_oversized_stdin_query_is_refused_without_draining_the_writer(self):
        """A stream far larger than the maximum is refused, and the read that
        refuses it is BOUNDED (SPEC/GRAPH.md Acceptance Criterion 40).

        The bound is the security property. The unbounded read this replaces let
        whoever was writing decide how much this process buffered: 256 MiB
        offered to the statement-running subcommand produced 867 MB of peak
        resident memory and 15.9 s of wall time, all spent on a query that was
        never going to be accepted. Any pipeline feeding rmp from an untrusted
        source -- a fetched file, an agent's tool output -- could drive the
        machine into swap through a command whose largest acceptable input is
        1 MiB.

        The writer below keeps sending until the pipe breaks and reports how much
        it managed to send. The ceiling asserted is deliberately generous: the
        reader takes 1 MiB plus one byte, the operating system's pipe buffer
        holds a further 64 KiB the command never reads, and the writer is in the
        middle of a chunk when the pipe breaks. Anything within a few megabytes
        proves the read stopped; 64 MiB proves it did not.

        A server runs for the sentinel and the read-back only. The refusal
        itself needs none -- it precedes the socket -- which is why the graph is
        seeded before the oversized attempt and compared with itself after it.
        """
        self.serving()
        self.test.graph_ok(
            self.roadmap,
            query="CREATE (n:SourceFile {path:'internal/commands/graph_client.go'})")
        before = self._source_file_paths()
        assert before == ["internal/commands/graph_client.go"], before

        chunk = b"a" * (64 * 1024)
        offered = 64 * 1024 * 1024
        proc = subprocess.Popen(
            [self.test.cli_path, "graph", "client", "-r", self.roadmap],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env=self._client_env(),
        )

        sent = 0
        try:
            while sent < offered:
                proc.stdin.write(chunk)
                proc.stdin.flush()
                sent += len(chunk)
        except (BrokenPipeError, OSError, ValueError):
            # BrokenPipeError once rmp has exited; ValueError from a flush on the
            # writer Python closed when the pipe broke. Both mean the same thing:
            # the reader stopped before the writer ran out of data.
            pass
        finally:
            try:
                proc.stdin.close()
            except (BrokenPipeError, OSError, ValueError):
                pass

        # communicate() would flush the writer Python already closed on the
        # broken pipe (ValueError), so the streams are drained directly. Both are
        # small -- an error line on stderr, nothing on stdout -- so reading them
        # to end of stream cannot deadlock.
        stdout = proc.stdout.read()
        stderr = proc.stderr.read().decode()
        proc.wait(timeout=30)

        assert proc.returncode == EXIT_VALIDATION, (
            f"a query over the maximum must be refused with exit "
            f"{EXIT_VALIDATION}; got {proc.returncode}, stderr={stderr!r}"
        )
        assert TOO_LONG_MESSAGE in stderr, (
            f"the refusal must be the length check's own, not an engine "
            f"diagnostic nor a missing server; stderr={stderr!r}"
        )
        assert stdout == b"", (
            f"a failing invocation writes nothing to stdout; got {stdout!r}"
        )
        assert sent < 8 * 1024 * 1024, (
            f"the reader consumed at least {sent} bytes of the {offered} offered: "
            f"the standard-input query read is no longer bounded"
        )
        assert self._source_file_paths() == before, (
            "a refused query must leave the graph exactly as it was"
        )
        print("✓ an oversized standard-input query is refused after "
              f"{sent} bytes, not after draining the stream")

    def test_several_hundred_kilobyte_query_from_stdin_executes_normally(self):
        """The bound refuses only what the maximum forbids: a legitimate query of
        several hundred kilobytes, supplied the same way, still exits 0 and does
        its work (SPEC/GRAPH.md Acceptance Criterion 40).

        This is the half that keeps the maximum honest. A cap tight enough to
        catch ordinary work would be widened later, and widening a published limit
        is worse than choosing it well once, which is why 64 KiB was declined.
        The query below is one CREATE statement carrying 6000 patterns, the shape
        a graph bootstrap script takes, and it is sent through the client to a
        running server exactly as such a script would send it.
        """
        self.serving()
        patterns = ",".join(
            "(c%d:SourceFile {path:'internal/commands/module_%04d.go'})" % (i, i)
            for i in range(6000)
        )
        query = "CREATE " + patterns
        assert 300_000 < len(query) < MAX_QUERY_BYTES, (
            f"the query is {len(query)} bytes; it must be hundreds of kilobytes "
            f"and under the maximum to prove the bound is not too tight"
        )

        result = self.test.graph_ok(self.roadmap, stdin_text=query, timeout=60.0)
        assert_graph_write_shape(
            result,
            context=f"the {len(query)}-byte statement",
            counters={
                "nodesCreated": 6000,
                "propertiesWritten": 6000,
                "labelsAdded": 6000,
            },
        )

        # Executed, not merely parsed nor merely counted: every node the
        # statement creates is readable back through a second invocation.
        created = self._source_file_paths()
        assert len(created) == 6000, (
            f"the {len(query)}-byte query must create all 6000 nodes; "
            f"found {len(created)}"
        )
        assert created[0] == "internal/commands/module_0000.go", created[0]
        assert created[-1] == "internal/commands/module_5999.go", created[-1]
        print(f"✓ a {len(query)}-byte query from standard input executes normally")

    def _assert_no_query_refusal(self, stdin_arg, description):
        """Run `rmp graph client` with the given standard input and assert the
        missing-query refusal: exit 2, the exact message, nothing on stdout, and
        a process that did NOT wait.

        No server is listening, and that is deliberate. `readQuery` runs before
        the roadmap's existence is checked and before a socket is resolved, so a
        refusal that arrives here arrives without one; were the order otherwise,
        the command would fail with the no-server line instead, which the exit
        code alone would not distinguish.

        The wall-clock assertion is not decoration. The defect being closed is a
        command that never returns, and a test that checked only the exit code
        could not discriminate it: a hung process never produces one.
        """
        self.assert_nothing_is_listening()
        start = time.monotonic()
        proc = subprocess.Popen(
            [self.test.cli_path, "graph", "client", "-r", self.roadmap],
            stdin=stdin_arg, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env=self._client_env(),
        )
        try:
            stdout, stderr = proc.communicate(timeout=NO_WAIT_BUDGET_SECONDS)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.communicate()
            raise AssertionError(
                f"{description}: the command was still running after "
                f"{NO_WAIT_BUDGET_SECONDS}s. It is waiting for input that will "
                f"never arrive instead of failing at once"
            ) from None
        elapsed = time.monotonic() - start

        assert proc.returncode == EXIT_MISUSE, (
            f"{description}: standard input that supplies no query must exit "
            f"{EXIT_MISUSE}; got {proc.returncode}, stderr={stderr.decode()!r}"
        )
        assert NO_QUERY_MESSAGE in stderr.decode(), (
            f"{description}: stderr={stderr.decode()!r}"
        )
        assert NO_SERVER_MESSAGE not in stderr.decode(), (
            f"{description}: the refusal must precede the socket, so it cannot "
            f"be the no-server line; stderr={stderr.decode()!r}"
        )
        assert stdout == b"", (
            f"{description}: a failing invocation writes nothing to stdout; "
            f"got {stdout!r}"
        )
        assert elapsed < NO_WAIT_BUDGET_SECONDS, (
            f"{description}: took {elapsed:.2f}s to refuse"
        )
        return elapsed

    def test_stdin_at_end_of_stream_fails_exit_2_at_once(self):
        """Standard input already at end of stream -- here /dev/null, which the
        specification names -- supplies no query (SPEC/GRAPH.md Acceptance
        Criterion 41).
        """
        elapsed = self._assert_no_query_refusal(
            subprocess.DEVNULL, "standard input at end of stream"
        )
        print(f"✓ an empty standard input is refused in {elapsed * 1000:.0f} ms")

    def test_whitespace_only_stdin_fails_exit_2_at_once(self):
        """Standard input carrying only whitespace trims to nothing, so it
        supplies no query and is refused with the SAME exit code and message as an
        empty one -- exit 2, not the exit 6 an over-long query carries
        (SPEC/GRAPH.md Acceptance Criterion 41).
        """
        read_fd, write_fd = os.pipe()
        os.write(write_fd, b"   \n\t\r\n  ")
        os.close(write_fd)
        try:
            elapsed = self._assert_no_query_refusal(
                read_fd, "whitespace-only standard input"
            )
        finally:
            os.close(read_fd)
        print(f"✓ a whitespace-only standard input is refused in "
              f"{elapsed * 1000:.0f} ms")

    def test_terminal_stdin_fails_exit_2_without_waiting(self):
        """Standard input connected to a TERMINAL is refused WITHOUT BEING READ
        (SPEC/GRAPH.md Acceptance Criterion 41).

        This is the case that regressed into a hang, and the only one whose proof
        has to be a clock. An invocation that omitted --query, with a terminal on
        standard input, printed nothing and never returned; it was killed after
        roughly forty minutes. Nothing on the command line looked wrong and no
        diagnostic appeared, so a script, a CI step, or an agent driving the
        binary simply stopped.

        The pseudo-terminal below is never written to, so the terminal carries no
        input at all: exactly the situation the defect hung in. An implementation
        that read before deciding would sit here until the budget expires, and the
        helper turns that into a failure rather than a hang of the suite.
        """
        master, slave = pty.openpty()
        try:
            elapsed = self._assert_no_query_refusal(
                slave, "a terminal on standard input"
            )
        finally:
            os.close(slave)
            os.close(master)
        print(f"✓ a terminal on standard input is refused in "
              f"{elapsed * 1000:.0f} ms, without waiting for a query")


def _run_all():
    instance_cls = TestGraphConcurrencyInput
    method_names = [m for m in dir(instance_cls) if m.startswith("test_")]
    passed = 0
    failed = 0
    failures = []
    for m in method_names:
        instance = instance_cls()
        instance.setup_method()
        try:
            getattr(instance, m)()
            passed += 1
            print(f"✓ {m}")
        except AssertionError as exc:
            failed += 1
            failures.append((m, exc))
            print(f"✗ {m}")
        except Exception as exc:  # noqa: BLE001
            failed += 1
            failures.append((m, exc))
            print(f"✗ {m} (error)")
        finally:
            instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Graph concurrency/input tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
