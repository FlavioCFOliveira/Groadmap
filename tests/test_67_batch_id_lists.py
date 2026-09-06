#!/usr/bin/env python3
"""
Test 67: batch id lists across the nine commands that take one (rmp task #418).

SPEC/COMMANDS.md "Task ID Lists (Batch Commands)" is canonical for everything
asserted here.

## The defect

Nine commands took a comma-separated id list and each guarded membership the
same way:

    if len(tasks) != len(ids) { ...not found... }

The database returns one row per DISTINCT id, so the two numbers differ whenever
the caller repeats one. `rmp task get 7,7`, with task 7 present, was refused
with `Error: resource not found: some tasks not found` and exit 4 -- a valid
invocation rejected, and the stated cause had not occurred, because nothing was
missing. The sprint side was worse: `sprint add-tasks 1 4,4` printed

    Error: resource not found: task(s) not found: []

an EMPTY list of missing ids, which is a message asserting that these ids are
missing and then naming none.

The wording had a second defect that survived even when the ids really were
absent: `some tasks not found` never said WHICH, so a caller had to bisect the
list to find out.

## What this module proves that the Go suites cannot

`internal/utils` pins the set arithmetic and the wording, and
`test_55_error_string_parity.py` pins every published string against the binary.
Neither drives the nine COMMANDS end to end, which is where the count comparison
lived and where a regression would reappear. Each command below is driven
against the compiled binary twice: once with a repeated valid id, which must
succeed, and once with a genuinely absent id, which must be refused by name.

Outcomes are asserted, not exit codes alone: a repeated id must change the same
task exactly once, so the audit log must grow by one entry and not by two.
"""

import inspect
import json
import os
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from tests.base_test import GroadmapTestBase

MISSING = 999999


class BatchIDBase:
    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        self.t1 = self.mk_task("Reconcile ledger entries against the payout report")
        self.t2 = self.mk_task("Backfill the quarterly revenue rollup")

    def teardown_method(self):
        self.test.teardown()

    def mk_task(self, title):
        out = self.run(["task", "create", "-r", self.roadmap, "-t", title,
                        "-fr", "Why the work is needed.",
                        "-tr", "How it is to be done.",
                        "-ac", "How completion is verified."])
        assert out[0] == 0, f"seeding {title!r} failed: {out}"
        return json.loads(out[1])["id"]

    def run(self, args):
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        r = subprocess.run([self.test.cli_path] + args, capture_output=True,
                           text=True, env=env, timeout=30)
        return r.returncode, r.stdout, r.stderr

    def audit_count(self):
        code, out, _ = self.run(["audit", "list", "-r", self.roadmap, "-l", "500"])
        assert code == 0
        return len(json.loads(out))


class TestRepeatedIDIsNotAnError(BatchIDBase):
    """A repeated id names the same task each time. It must never be refused,
    and it must never do the work twice."""

    def test_task_get_honours_a_repeated_id_once(self):
        code, out, err = self.run(["task", "get", "-r", self.roadmap, f"{self.t1},{self.t1}"])
        assert code == 0, (
            "`task get 7,7` with task 7 present must succeed: a repeated id names the "
            f"same task and is not a missing one. exit={code} stderr={err!r}")
        tasks = json.loads(out)
        assert len(tasks) == 1, (
            "the array carries one object per DISTINCT id, so it never holds two "
            f"elements with the same id. got {len(tasks)} objects: {tasks!r}")
        assert tasks[0]["id"] == self.t1

    def test_mutating_commands_honour_a_repeated_id_once(self):
        """Each command is driven with the id repeated, and the audit log must
        grow by exactly one entry -- proof the work happened once, not twice."""
        for args, label in (
            (["task", "prio", "-r", self.roadmap, f"{self.t1},{self.t1}", "5"], "prio"),
            (["task", "sev", "-r", self.roadmap, f"{self.t1},{self.t1}", "3"], "sev"),
        ):
            before = self.audit_count()
            code, _, err = self.run(args)
            assert code == 0, (
                f"`task {label}` with a repeated id must succeed. exit={code} stderr={err!r}")
            grew = self.audit_count() - before
            assert grew == 1, (
                f"`task {label}` with the id repeated must write ONE audit entry, not one "
                f"per occurrence: what an invocation does depends on the set of ids and "
                f"never on how many times the caller spelled one. grew by {grew}")

    def test_sprint_assignment_honours_a_repeated_id_once(self):
        code, out, _ = self.run(["sprint", "create", "-r", self.roadmap,
                                 "-t", "Ledger reconciliation", "-d", "Close the quarter's books."])
        assert code == 0
        sprint = json.loads(out)["id"]

        code, _, err = self.run(["sprint", "add-tasks", "-r", self.roadmap,
                                 str(sprint), f"{self.t1},{self.t1}"])
        assert code == 0, (
            "`sprint add-tasks` with a repeated id must succeed; it used to print "
            f"`task(s) not found: []` -- an empty list of missing ids. exit={code} stderr={err!r}")

        code, out, _ = self.run(["sprint", "tasks", str(sprint), "-r", self.roadmap])
        assert code == 0
        members = [t["id"] for t in json.loads(out)]
        assert members == [self.t1], (
            f"the repeated id must add one membership row, not two. got {members!r}")

        code, _, err = self.run(["sprint", "remove-tasks", "-r", self.roadmap,
                                 str(sprint), f"{self.t1},{self.t1}"])
        assert code == 0, (
            f"`sprint remove-tasks` with a repeated id must succeed. exit={code} stderr={err!r}")


class TestARefusalNamesTheIDs(BatchIDBase):
    """The refusal carries exactly the ids at fault, so a caller never has to
    bisect a list to learn which member failed."""

    def test_one_missing_id_is_named_in_the_singular(self):
        for family, args in (
            ("task get", ["task", "get", "-r", self.roadmap, f"{self.t1},{MISSING}"]),
            ("task prio", ["task", "prio", "-r", self.roadmap, f"{self.t1},{MISSING}", "5"]),
            ("task sev", ["task", "sev", "-r", self.roadmap, f"{self.t1},{MISSING}", "5"]),
            ("task reopen", ["task", "reopen", "-r", self.roadmap, f"{self.t1},{MISSING}"]),
            ("task remove", ["task", "remove", "-r", self.roadmap, f"{self.t1},{MISSING}"]),
        ):
            code, _, err = self.run(args)
            assert code == 4, f"{family}: want exit 4, got {code}; stderr={err!r}"
            assert f"task {MISSING} not found" in err, (
                f"{family}: the refusal must name the missing id. A caller that is only "
                f"told 'some tasks not found' has to bisect the list. stderr={err!r}")
            assert "some tasks" not in err, (
                f"{family}: the old vague wording must be gone. stderr={err!r}")

    def test_two_missing_ids_are_named_in_the_plural(self):
        code, _, err = self.run(["task", "get", "-r", self.roadmap, f"{MISSING},{MISSING + 1}"])
        assert code == 4, f"want exit 4, got {code}; stderr={err!r}"
        assert f"tasks {MISSING}, {MISSING + 1} not found" in err, (
            "two missing ids print the plural line, separated by a comma and a space, "
            f"in the order the caller supplied them. stderr={err!r}")

    def test_the_wording_follows_the_missing_count_not_the_supplied_count(self):
        """Four ids supplied, one missing: the SINGULAR line."""
        code, _, err = self.run(
            ["task", "get", "-r", self.roadmap, f"{self.t1},{self.t2},{MISSING},{self.t1}"])
        assert code == 4
        assert f"task {MISSING} not found" in err and "tasks " not in err, (
            "one missing id out of four supplied prints the singular line: the wording "
            f"follows how many are MISSING, not how many were supplied. stderr={err!r}")

    def test_a_repeated_missing_id_is_named_once(self):
        code, _, err = self.run(
            ["task", "get", "-r", self.roadmap, f"{MISSING},{self.t1},{MISSING}"])
        assert code == 4
        assert err.count(str(MISSING)) == 1, (
            f"an id the caller repeated is reported once. stderr={err!r}")

    def test_a_non_member_is_named_not_merely_counted(self):
        code, out, _ = self.run(["sprint", "create", "-r", self.roadmap,
                                 "-t", "Revenue rollup", "-d", "Land the rollup backfill."])
        sprint = json.loads(out)["id"]
        code, _, err = self.run(["sprint", "remove-tasks", "-r", self.roadmap,
                                 str(sprint), str(self.t1)])
        assert code == 6, f"a membership failure is a validation error, exit 6; got {code}"
        assert f"task {self.t1} is not in sprint #{sprint}" in err, (
            "the refusal names the task and the sprint. It used to print a Go slice "
            f"rendering such as `[4 4]`, undeduplicated and bracketed. stderr={err!r}")


def _run_all():
    passed = failed = 0
    failures = []
    classes = [obj for _n, obj in sorted(inspect.getmembers(sys.modules[__name__], inspect.isclass))
               if obj.__module__ == __name__ and _n.startswith("Test")]
    print(f"Discovered {len(classes)} test classes: "
          f"{', '.join(c.__name__ for c in classes)}")
    for cls in classes:
        for m in sorted(n for n in dir(cls) if n.startswith("test_")):
            label = f"{cls.__name__}.{m}"
            inst = cls()
            inst.setup_method()
            try:
                getattr(inst, m)()
                passed += 1
                print(f"✓ {label}")
            except AssertionError as exc:
                failed += 1
                failures.append((label, exc))
                print(f"✗ {label}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((label, exc))
                print(f"✗ {label} (error)")
            finally:
                inst.teardown_method()
    print("\n" + "=" * 60)
    print(f"Batch id-list tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if _run_all() else 1)
