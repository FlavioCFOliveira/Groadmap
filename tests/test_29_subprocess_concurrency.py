#!/usr/bin/env python3
"""
Test 29: Subprocess-level concurrency
test_07_concurrency exercises in-process threads sharing one CLI
binary; test_29 verifies the behaviour the user actually experiences,
where multiple `rmp` invocations from the shell race against the same
roadmap file. SQLite + WAL mode is supposed to serialise writers and
let readers proceed concurrently — this suite checks that promise
holds when the writers and readers are independent OS processes.
"""

import os
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor, as_completed

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


def _run_rmp(cli_path, home_dir, args, timeout=15):
    """Spawn `rmp ...` as a fresh subprocess, return (returncode, stdout, stderr).

    Each invocation gets the same HOME (so it sees the shared
    ~/.roadmaps), which is exactly what concurrent users on a shared
    machine — or two terminal tabs — produce.
    """
    env = os.environ.copy()
    env["HOME"] = str(home_dir)
    proc = subprocess.run(
        [cli_path, *args],
        env=env,
        capture_output=True,
        text=True,
        timeout=timeout,
    )
    return proc.returncode, proc.stdout, proc.stderr


class TestParallelTaskCreates:
    """Several rmp processes creating tasks at the same time must all succeed."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()

    def teardown_method(self):
        self.test.teardown()

    def test_concurrent_subprocess_creates_no_data_loss(self):
        """Fan out 8 subprocess creates × 3 tasks each = 24 inserts.

        SQLite serialises writes; the retry/backoff machinery in db.WithTransaction
        is supposed to absorb SQLITE_BUSY collisions. After everyone returns,
        the task list must contain exactly 24 tasks — no losses, no duplicates.
        """
        N_PROCESSES = 8
        N_PER_PROCESS = 3
        expected_total = N_PROCESSES * N_PER_PROCESS

        def worker(proc_index):
            outcomes = []
            for j in range(N_PER_PROCESS):
                rc, _, stderr = _run_rmp(
                    self.test.cli_path, self.test.home_dir,
                    [
                        "task", "create", "-r", self.roadmap,
                        "-t", f"Concurrent probe task #{proc_index}-{j}",
                        "-fr", "Created in parallel by an independent rmp subprocess.",
                        "-tr", "Verifies SQLite WAL + retry logic absorbs concurrent writers.",
                        "-ac", "All 24 tasks land in the roadmap with no SQLITE_BUSY surfacing.",
                    ],
                )
                outcomes.append((rc, stderr))
            return outcomes

        with ThreadPoolExecutor(max_workers=N_PROCESSES) as ex:
            futures = [ex.submit(worker, i) for i in range(N_PROCESSES)]
            results = []
            for fut in as_completed(futures):
                results.extend(fut.result())

        failed = [(rc, err) for rc, err in results if rc != 0]
        assert not failed, (
            f"Concurrent subprocess writes must all succeed; "
            f"first failure: rc={failed[0][0]}, stderr={failed[0][1]!r}"
        )

        tasks = self.test.run_cmd_json(["task", "list", "-r", self.roadmap, "--limit", "100"])
        assert len(tasks) == expected_total, (
            f"Expected {expected_total} tasks after concurrent creates, got {len(tasks)}"
        )

        print(f"✓ {N_PROCESSES} subprocesses × {N_PER_PROCESS} creates each → {len(tasks)} tasks landed")


class TestParallelReadsDuringWrites:
    """Readers must see a consistent snapshot while writers are active (WAL mode)."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        # Seed a real backlog so list operations have something to scan.
        self.seeded_ids = []
        for i in range(20):
            tid = self.test.create_task(
                self.roadmap,
                title=f"Seeded backlog task #{i+1} for concurrent-read test",
                functional_requirements="Pre-existing item to make list/stats non-trivial.",
                technical_requirements="No specific implementation; this is data setup.",
                acceptance_criteria="Visible to the reader subprocesses.",
                priority=(i % 9) + 1,
            )
            self.seeded_ids.append(tid)

    def teardown_method(self):
        self.test.teardown()

    def test_reads_never_fail_while_writer_runs(self):
        """A bursting writer in one process must not break readers in others.

        Spawn 10 reader subprocesses (task list + task get + stats) while
        a single writer process performs 10 status changes back-to-back.
        Every reader must exit 0 and return well-formed JSON.
        """
        sprint_id = self.test.create_sprint(self.roadmap, "Concurrent-read coverage sprint")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap, str(sprint_id),
            ",".join(str(i) for i in self.seeded_ids),
        ])

        def writer():
            outcomes = []
            for tid in self.seeded_ids[:10]:
                rc, _, stderr = _run_rmp(
                    self.test.cli_path, self.test.home_dir,
                    ["task", "stat", "-r", self.roadmap, str(tid), "DOING"],
                )
                outcomes.append((rc, stderr))
            return outcomes

        def reader(op):
            rc, stdout, stderr = _run_rmp(
                self.test.cli_path, self.test.home_dir,
                op,
            )
            return op, rc, stdout, stderr

        read_ops = [
            ["task", "list", "-r", self.roadmap, "--limit", "100"],
            ["stats", "-r", self.roadmap],
            ["sprint", "stats", "-r", self.roadmap, str(sprint_id)],
        ] * 4  # 12 reads total

        with ThreadPoolExecutor(max_workers=len(read_ops) + 1) as ex:
            writer_fut = ex.submit(writer)
            reader_futs = [ex.submit(reader, op) for op in read_ops]
            writer_outcomes = writer_fut.result()
            reader_outcomes = [f.result() for f in reader_futs]

        # Every writer call should have succeeded.
        for rc, err in writer_outcomes:
            assert rc == 0, f"writer subprocess failed: rc={rc}, stderr={err!r}"

        # Every reader should have succeeded with non-empty JSON output.
        for op, rc, stdout, err in reader_outcomes:
            assert rc == 0, f"reader {op} failed during writer pressure: rc={rc}, stderr={err!r}"
            assert stdout.strip().startswith(("{", "[")), (
                f"reader {op} must return JSON; got {stdout[:80]!r}"
            )

        print(
            f"✓ {len(writer_outcomes)} writer ops + {len(reader_outcomes)} reader ops succeeded under WAL"
        )


def main():
    """Run all subprocess concurrency tests."""
    import inspect

    failures = []
    passed = 0
    classes = [
        ("TestParallelTaskCreates", TestParallelTaskCreates),
        ("TestParallelReadsDuringWrites", TestParallelReadsDuringWrites),
    ]
    for cls_name, cls in classes:
        for meth_name, meth in inspect.getmembers(cls, predicate=inspect.isfunction):
            if not meth_name.startswith("test_"):
                continue
            inst = cls()
            if hasattr(inst, "setup_method"):
                inst.setup_method()
            try:
                meth(inst)
                passed += 1
            except Exception as e:
                failures.append(f"{cls_name}.{meth_name}: {e}")
            if hasattr(inst, "teardown_method"):
                try:
                    inst.teardown_method()
                except Exception:
                    pass

    print(f"\n{passed} passed, {len(failures)} failed")
    for f in failures:
        print(f"  ✗ {f}")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
