#!/usr/bin/env python3
"""
Test 41: `rmp graph` write concurrency safety and Cypher input validation.

End-to-end backstop against the compiled ./bin/rmp for three audit findings:

- #39 (CRITICAL): concurrent graph writers must not lose acknowledged writes.
  Two writers that interleave their open -> commit -> checkpoint -> WAL-truncate
  sequences could previously let one writer's full-snapshot checkpoint overwrite
  the other's committed-but-unseen write and then truncate the WAL holding it,
  silently dropping an acknowledged write. With the per-store exclusive write
  lock, contention surfaces as exit 1 and the invariant holds:
  every write that returns exit 0 is present in the store, and nothing else is.
  (SPEC/GRAPH.md § Concurrency and Recovery rule 2; Acceptance Criterion 16.)

- #26/#27 (#52): `--query` with no value, or whose value is the next flag, must
  fail with exit 2 (SPEC/GRAPH.md § Cypher Input Source and Precedence rule 4),
  never silently fall back to stdin or swallow the following flag.

- #28 (#57): an unknown flag to a graph subcommand must fail with exit 2
  (SPEC/ARCHITECTURE.md unknown-flag rule), not be silently ignored.

- #81: a `--query` value that is a negative numeric literal (for example
  `-1 RETURN 1` or `-0.5`) is NOT flag-like; it is a legitimate query value and
  must reach the engine (failing exit 1 only on its own Cypher invalidity),
  never be rejected as a missing value with exit 2
  (SPEC/GRAPH.md § Cypher Input Source and Precedence rule 4).

- #181: the standard-input read is BOUNDED and a standard input that supplies no
  query is refused at once. Both halves of one unbounded read, and they carry
  DIFFERENT exit codes on purpose:

  - a producer that writes too much. 256 MiB offered to `rmp graph query`
    reached 867 MB of resident memory and 15.9 s of wall time before anything
    rejected it. A query over 1 MiB is now refused with exit 6 after the read has
    consumed a bounded amount (SPEC/GRAPH.md § Maximum Query Length, § Bounded
    Standard-Input Read; Acceptance Criterion 40).
  - a producer that writes nothing. With `--query` absent and a terminal on
    standard input, the command waited for a query nobody was going to type: one
    invocation hung for roughly forty minutes, printing nothing. Standard input
    that is empty, whitespace only, or a terminal is now refused with exit 2
    (SPEC/GRAPH.md § Standard Input That Supplies No Query; Acceptance
    Criterion 41).

  The over-long refusal on the `--query` FLAG is not reachable from here and is
  proved by the Go test instead: execve caps a single argument at 128 KiB on
  Linux, so no argument vector can carry a query over 1 MiB. The cap is applied
  to both sources in internal/commands/graph.go, and
  internal/commands/graph_query_length_test.go asserts the flag door directly.
"""

import os
import pty
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


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

# How long the command may take to refuse a standard input that supplies no
# query. The contract is that it does not wait at all, so the honest budget is
# milliseconds; ten seconds is chosen only so a loaded machine cannot produce a
# false failure, and it is still far below the forty minutes the defect burned.
NO_WAIT_BUDGET_SECONDS = 10.0


class TestGraphConcurrencyInput:

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()

    def teardown_method(self):
        self.test.teardown()

    def _popen_create(self, key):
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        return subprocess.Popen(
            [
                self.test.cli_path, "graph", "create", "-r", self.roadmap,
                "--query", f'CREATE (n:Conc {{k:"{key}"}})',
            ],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env,
        )

    # ---- #39: concurrent writers never lose an acknowledged write ----

    def test_concurrent_writes_lose_nothing(self):
        keys = [f"w{i}" for i in range(8)]

        # Launch all writers as close to simultaneously as possible.
        procs = {key: self._popen_create(key) for key in keys}
        succeeded = set()
        for key, p in procs.items():
            p.communicate()
            if p.returncode == EXIT_OK:
                succeeded.add(key)
            else:
                # The only acceptable non-success is contention (exit 1).
                assert p.returncode == EXIT_DB, (
                    f"writer {key} failed with unexpected exit {p.returncode}"
                )

        assert succeeded, "expected at least one concurrent writer to succeed"

        # Invariant: the set of nodes present in the store equals EXACTLY the
        # set of writers that returned exit 0 — no acknowledged write was lost,
        # and no failed write left a phantom node.
        result = self.test.run_cmd_json(
            ["graph", "query", "-r", self.roadmap,
             "--query", "MATCH (n:Conc) RETURN n.k"]
        )
        present = {row[0] for row in result.get("rows", [])}
        assert present == succeeded, (
            f"store contents must equal acknowledged writes; "
            f"present={sorted(present)} acknowledged={sorted(succeeded)}"
        )

    # ---- #52: --query value handling --------------------------------

    def test_query_flag_without_value_fails_exit_2(self):
        code, _, _ = self.test.run_cmd(
            ["graph", "query", "-r", self.roadmap, "--query"], check=False
        )
        assert code == EXIT_MISUSE, f"--query with no value must exit 2, got {code}"

    def test_query_flag_followed_by_flag_fails_exit_2(self):
        code, _, _ = self.test.run_cmd(
            ["graph", "query", "-r", self.roadmap, "--query", "--bogus"], check=False
        )
        assert code == EXIT_MISUSE, (
            f"--query whose value is a flag must exit 2 (not swallow it), got {code}"
        )

    # ---- #81: negative numeric --query value reaches the engine ------

    def test_query_negative_numeric_value_reaches_engine(self):
        # "-1 RETURN 1" is a negative numeric literal, not a flag. It must be
        # accepted as the query value and handed to the engine, which rejects it
        # as invalid Cypher (exit 1) — NOT rejected as a missing value (exit 2).
        code, _, _ = self.test.run_cmd(
            ["graph", "query", "-r", self.roadmap, "--query", "-1 RETURN 1"],
            check=False,
        )
        assert code == EXIT_DB, (
            f"negative-numeric --query value must reach the engine (exit 1), "
            f"not be rejected as missing (exit 2); got {code}"
        )

    def test_query_leading_decimal_point_value_reaches_engine(self):
        # "-.5" begins with '-' then a decimal point: a numeric literal, not a
        # flag. It too must reach the engine and fail exit 1 on Cypher validity,
        # exercising the decimal-point branch of the flag-like check — never the
        # missing-value exit 2.
        code, _, _ = self.test.run_cmd(
            ["graph", "query", "-r", self.roadmap, "--query", "-.5"],
            check=False,
        )
        assert code == EXIT_DB, (
            f"a '-.5' --query value must reach the engine (exit 1), "
            f"not be rejected as missing (exit 2); got {code}"
        )

    # ---- #57: unknown flags rejected --------------------------------

    def test_unknown_flag_rejected_exit_2(self):
        # Provide a valid query via stdin so the only problem is the unknown flag.
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        result = subprocess.run(
            [self.test.cli_path, "graph", "query", "-r", self.roadmap, "--bogus"],
            input="MATCH (n) RETURN n", capture_output=True, text=True, env=env,
        )
        assert result.returncode == EXIT_MISUSE, (
            f"unknown graph flag must exit 2, got {result.returncode}"
        )


    # ---- #181: the bounded read and the refusals it does not collapse ----

    def _graph_env(self):
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        return env

    def _graph_node_keys(self):
        """Every node key in the roadmap's graph, as a sorted list.

        Used to assert that a refused query changed nothing. The refusal happens
        before the guard rail classifies anything and before the store is opened,
        so the graph must be byte-for-byte the graph it was.
        """
        result = self.test.run_cmd_json(
            ["graph", "query", "-r", self.roadmap,
             "--query", "MATCH (n:Bounded) RETURN n.key ORDER BY n.key"]
        )
        return sorted(row[0] for row in result.get("rows", []))

    def test_oversized_stdin_query_is_refused_without_draining_the_writer(self):
        """A stream far larger than the maximum is refused, and the read that
        refuses it is BOUNDED (SPEC/GRAPH.md Acceptance Criterion 40).

        The bound is the security property. The unbounded read this replaces let
        whoever was writing decide how much this process buffered: 256 MiB
        offered to `rmp graph query` produced 867 MB of peak resident memory and
        15.9 s of wall time, all spent on a query that was never going to be
        accepted. Any pipeline feeding rmp from an untrusted source -- a fetched
        file, an agent's tool output -- could drive the machine into swap through
        a command whose largest acceptable input is 1 MiB.

        The writer below keeps sending until the pipe breaks and reports how much
        it managed to send. The ceiling asserted is deliberately generous: the
        reader takes 1 MiB plus one byte, the operating system's pipe buffer
        holds a further 64 KiB the command never reads, and the writer is in the
        middle of a chunk when the pipe breaks. Anything within a few megabytes
        proves the read stopped; 64 MiB proves it did not.
        """
        self.test.run_cmd(
            ["graph", "create", "-r", self.roadmap,
             "--query", "CREATE (n:Bounded {key:'sentinel-before-the-refusal'})"]
        )
        before = self._graph_node_keys()
        assert before == ["sentinel-before-the-refusal"], before

        chunk = b"a" * (64 * 1024)
        offered = 64 * 1024 * 1024
        proc = subprocess.Popen(
            [self.test.cli_path, "graph", "query", "-r", self.roadmap],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env=self._graph_env(),
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
            f"diagnostic; stderr={stderr!r}"
        )
        assert stdout == b"", (
            f"a failing invocation writes nothing to stdout; got {stdout!r}"
        )
        assert sent < 8 * 1024 * 1024, (
            f"the reader consumed at least {sent} bytes of the {offered} offered: "
            f"the standard-input query read is no longer bounded"
        )
        assert self._graph_node_keys() == before, (
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
        a graph bootstrap script takes.
        """
        patterns = ",".join(
            "(c%d:Bounded {key:'internal/commands/module_%04d.go'})" % (i, i)
            for i in range(6000)
        )
        query = "CREATE " + patterns
        assert 300_000 < len(query) < MAX_QUERY_BYTES, (
            f"the query is {len(query)} bytes; it must be hundreds of kilobytes "
            f"and under the maximum to prove the bound is not too tight"
        )

        result = subprocess.run(
            [self.test.cli_path, "graph", "create", "-r", self.roadmap],
            input=query.encode(), capture_output=True, env=self._graph_env(),
        )
        assert result.returncode == EXIT_OK, (
            f"a {len(query)}-byte query is under the maximum and must succeed; "
            f"got {result.returncode}, stderr={result.stderr.decode()!r}"
        )

        # Executed, not merely parsed: every node the statement creates is there.
        created = self._graph_node_keys()
        assert len(created) == 6000, (
            f"the {len(query)}-byte query must create all 6000 nodes; "
            f"found {len(created)}"
        )
        assert created[0] == "internal/commands/module_0000.go", created[0]
        assert created[-1] == "internal/commands/module_5999.go", created[-1]
        print(f"✓ a {len(query)}-byte query from standard input executes normally")

    def _assert_no_query_refusal(self, stdin_arg, description):
        """Run `rmp graph query` with the given standard input and assert the
        missing-query refusal: exit 2, the exact message, nothing on stdout, and
        a process that did NOT wait.

        The wall-clock assertion is not decoration. The defect being closed is a
        command that never returns, and a test that checked only the exit code
        could not discriminate it: a hung process never produces one.
        """
        start = time.monotonic()
        proc = subprocess.Popen(
            [self.test.cli_path, "graph", "query", "-r", self.roadmap],
            stdin=stdin_arg, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env=self._graph_env(),
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
