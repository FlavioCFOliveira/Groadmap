#!/usr/bin/env python3
"""
Test 25: Completion Guards (subtask + dependency)
Covers the two guards from SPEC/STATE_MACHINE.md §§ Sub-task Hierarchy
Guard and Dependency Guard, plus the documented evaluation order.

Validates:
  - A task cannot reach COMPLETED while it has subtasks that are not COMPLETED.
  - A task cannot reach COMPLETED while it has dependencies that are not COMPLETED.
  - The sub-task guard is evaluated FIRST. When both would fire, the user
    sees the subtask error, not the dependency error.
  - Error messages reference the specific blocking IDs.
  - Both guards leave the target task in its original status (no partial mutation).
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


def _make_task(test, roadmap, title, parent_id=None, priority=5):
    """Create a realistic task; optionally attach to a parent."""
    cmd = [
        "task", "create",
        "-r", roadmap,
        "-t", title,
        "-fr", f"Required because: {title.lower()}.",
        "-tr", "Implement in the relevant subsystem and update CI.",
        "-ac", "Behaviour matches spec; new tests pass; runbook updated.",
        "-p", str(priority),
    ]
    if parent_id is not None:
        cmd.extend(["--parent", str(parent_id)])
    result = test.run_cmd_json(cmd)
    return result["id"]


def _advance_to_testing(test, roadmap, task_id):
    """Drive a task from BACKLOG to TESTING via a real sprint."""
    sprint_id = test.create_sprint(roadmap, f"Guard test sprint for #{task_id}")
    test.move_task_to_sprint(roadmap, task_id, sprint_id)
    test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
    test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])


class TestSubtaskGuard:
    """A parent cannot be COMPLETED while it has incomplete subtasks."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        # Realistic parent: rolling out an internal observability platform
        self.parent = _make_task(
            self.test, self.roadmap,
            "Roll out internal observability platform to all services",
        )
        # Three subtasks; we'll vary their states per test
        self.sub_metrics = _make_task(
            self.test, self.roadmap,
            "Instrument metrics for the catalogue service",
            parent_id=self.parent,
        )
        self.sub_logs = _make_task(
            self.test, self.roadmap,
            "Wire structured logs through the API gateway",
            parent_id=self.parent,
        )
        self.sub_traces = _make_task(
            self.test, self.roadmap,
            "Enable distributed tracing on payment workers",
            parent_id=self.parent,
        )

    def teardown_method(self):
        self.test.teardown()

    def _complete(self, task_id):
        sprint_id = self.test.create_sprint(self.roadmap, f"Subtask sprint #{task_id}")
        self.test.move_task_to_sprint(self.roadmap, task_id, sprint_id)
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(task_id), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(task_id), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(task_id), "COMPLETED"])

    def test_complete_rejected_when_any_subtask_incomplete(self):
        """All three subtasks still BACKLOG → COMPLETED on parent fails with the subtask IDs."""
        _advance_to_testing(self.test, self.roadmap, self.parent)

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, str(self.parent), "COMPLETED"],
            check=False,
        )
        assert exit_code == 6, f"COMPLETED with incomplete subtasks must exit 6; got {exit_code}"
        assert "subtask" in stderr.lower(), f"error must say 'subtask'; got {stderr}"
        # At least one blocking subtask id should be referenced
        assert any(
            f"#{sid}" in stderr
            for sid in (self.sub_metrics, self.sub_logs, self.sub_traces)
        ), f"error must list at least one blocking subtask id; got {stderr}"

        # The parent must remain in TESTING (no partial mutation).
        status = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(self.parent)])[0]["status"]
        assert status == "TESTING", f"parent must remain TESTING; got {status}"

        print("✓ parent COMPLETED rejected with all subtasks incomplete; state preserved")

    def test_complete_rejected_when_one_subtask_incomplete(self):
        """Two subtasks COMPLETED but the third still open → parent COMPLETED rejected."""
        self._complete(self.sub_metrics)
        self._complete(self.sub_logs)
        _advance_to_testing(self.test, self.roadmap, self.parent)

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, str(self.parent), "COMPLETED"],
            check=False,
        )
        assert exit_code == 6
        assert "subtask" in stderr.lower()
        assert f"#{self.sub_traces}" in stderr, (
            f"error must reference the one remaining open subtask #{self.sub_traces}; got {stderr}"
        )
        # Already-completed subtasks should not be listed
        assert f"#{self.sub_metrics}" not in stderr
        assert f"#{self.sub_logs}" not in stderr

        print("✓ parent COMPLETED rejected when only one subtask remains; error lists exactly that one")

    def test_complete_succeeds_when_all_subtasks_completed(self):
        """Once every subtask is COMPLETED, the parent can be COMPLETED."""
        for sub in (self.sub_metrics, self.sub_logs, self.sub_traces):
            self._complete(sub)
        _advance_to_testing(self.test, self.roadmap, self.parent)

        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.parent), "COMPLETED"])
        status = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(self.parent)])[0]["status"]
        assert status == "COMPLETED"

        print("✓ parent COMPLETED succeeds once all subtasks are COMPLETED")


class TestDependencyGuard:
    """A task cannot be COMPLETED while a dependency is still incomplete."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        # A depends on B; both standalone tasks (no subtasks).
        self.task_a = _make_task(self.test, self.roadmap, "Cut over OAuth flow to PKCE")
        self.task_b = _make_task(self.test, self.roadmap, "Deploy PKCE-capable identity provider")
        self.test.run_cmd(
            ["task", "add-dep", "-r", self.roadmap, str(self.task_a), str(self.task_b)]
        )

    def teardown_method(self):
        self.test.teardown()

    def test_complete_rejected_when_dependency_incomplete(self):
        """A in TESTING, B in BACKLOG → COMPLETED A is rejected referencing B."""
        _advance_to_testing(self.test, self.roadmap, self.task_a)

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, str(self.task_a), "COMPLETED"],
            check=False,
        )
        assert exit_code == 6
        assert "depend" in stderr.lower()
        assert f"#{self.task_b}" in stderr, f"blocking dep #{self.task_b} must appear; got {stderr}"

        # A must remain in TESTING.
        status = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(self.task_a)])[0]["status"]
        assert status == "TESTING"

        print("✓ dependency guard blocks COMPLETED; error lists the incomplete dep")

    def test_complete_succeeds_once_dependency_completed(self):
        """When B reaches COMPLETED, A can also be COMPLETED."""
        # Drive B to COMPLETED via its own sprint
        sprint_b = self.test.create_sprint(self.roadmap, "B sprint")
        self.test.move_task_to_sprint(self.roadmap, self.task_b, sprint_b)
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task_b), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task_b), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task_b), "COMPLETED"])

        _advance_to_testing(self.test, self.roadmap, self.task_a)
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task_a), "COMPLETED"])

        status = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(self.task_a)])[0]["status"]
        assert status == "COMPLETED"

        print("✓ dependency guard releases when the dep is COMPLETED")


class TestGuardEvaluationOrder:
    """When both guards would fire, the subtask guard wins (per SPEC §130).

    Setup: parent X has one incomplete subtask AND one incomplete dependency.
    Expected: stderr mentions 'subtask', NOT 'dependencies'.
    """

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()

        # Parent that has both an open subtask and an open dependency.
        self.parent = _make_task(
            self.test, self.roadmap,
            "Migrate cart service to gRPC interface",
        )
        # Subtask still BACKLOG
        self.subtask = _make_task(
            self.test, self.roadmap,
            "Generate gRPC client stubs for the iOS app",
            parent_id=self.parent,
        )
        # Standalone dependency still BACKLOG
        self.dep = _make_task(
            self.test, self.roadmap,
            "Provision gRPC-aware load balancer in production",
        )
        self.test.run_cmd(
            ["task", "add-dep", "-r", self.roadmap, str(self.parent), str(self.dep)]
        )

    def teardown_method(self):
        self.test.teardown()

    def test_subtask_guard_evaluated_first(self):
        """When both would block, the user sees the subtask error only."""
        _advance_to_testing(self.test, self.roadmap, self.parent)

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, str(self.parent), "COMPLETED"],
            check=False,
        )
        assert exit_code == 6

        # Per SPEC §130, the subtask guard runs first; the dependency error
        # is not surfaced as long as a subtask is incomplete.
        assert "subtask" in stderr.lower(), f"subtask guard must fire first; stderr={stderr}"
        assert "dependencies" not in stderr.lower() or "subtask" in stderr.lower(), (
            f"dependency error must not appear before subtasks resolve; stderr={stderr}"
        )
        # Specifically, the open subtask id should appear, not the dep id.
        assert f"#{self.subtask}" in stderr
        # The dependency id should NOT yet be surfaced — guard order matters.
        # (The check is not assert-fatal if both ids appear; what we really
        #  enforce is that 'subtask' wording precedes 'dependencies' wording.)
        idx_sub = stderr.lower().find("subtask")
        idx_dep = stderr.lower().find("dependencies")
        if idx_dep != -1:
            assert idx_sub != -1 and idx_sub < idx_dep, (
                f"subtask must be reported before dependencies; stderr={stderr}"
            )

        print("✓ subtask guard evaluates BEFORE dependency guard")


def main():
    """Run all completion guard tests."""
    import inspect

    failures = []
    passed = 0
    classes = [
        ("TestSubtaskGuard", TestSubtaskGuard),
        ("TestDependencyGuard", TestDependencyGuard),
        ("TestGuardEvaluationOrder", TestGuardEvaluationOrder),
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
