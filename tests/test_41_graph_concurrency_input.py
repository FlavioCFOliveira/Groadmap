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
"""

import os
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


EXIT_OK = 0
EXIT_DB = 1
EXIT_MISUSE = 2


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
