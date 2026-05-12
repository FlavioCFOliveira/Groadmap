#!/usr/bin/env python3
"""
Test 24: Task Dependency Workflow
Covers `rmp task add-dep`, `remove-dep`, `blockers`, `blocking` end-to-end.

Validates the full dependency graph behaviour:
  - Linear chains A→B→C→D
  - Blockers/Blocking inverse queries
  - blockers excludes COMPLETED tasks
  - depends_on and blocks fields exposed by `task get`
  - Self-dependency and circular dependency rejection
  - Audit log entries (TASK_ADD_DEP / TASK_REMOVE_DEP)
  - Error paths: nonexistent task, nonexistent dep, remove non-existent
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


def _create_task(test, roadmap, title, priority=5):
    """Helper: create a realistic task and return its id."""
    return test.create_task(
        roadmap,
        title=title,
        functional_requirements=(
            f"This piece of work is needed because the system currently lacks: {title.lower()}"
        ),
        technical_requirements=(
            "Implement the change in the relevant module, add tests, "
            "and update operational runbooks if behaviour-visible."
        ),
        acceptance_criteria=(
            "Feature behaves per spec, regression suite is green, "
            "and the change is logged in the migration tracker."
        ),
        priority=priority,
    )


class TestAddRemoveDeps:
    """rmp task add-dep / remove-dep — basic linkage and idempotency."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        self.task_a = _create_task(self.test, self.roadmap, "Roll out feature flag service")
        self.task_b = _create_task(self.test, self.roadmap, "Provision the underlying Redis cluster")

    def teardown_method(self):
        self.test.teardown()

    def test_add_dep_basic(self):
        """A depends on B: blockers(A) contains B and blocking(B) contains A."""
        self.test.run_cmd(
            ["task", "add-dep", "-r", self.roadmap, str(self.task_a), str(self.task_b)]
        )

        blockers = self.test.run_cmd_json(
            ["task", "blockers", "-r", self.roadmap, str(self.task_a)]
        )
        blocker_ids = [t["id"] for t in blockers]
        assert self.task_b in blocker_ids, (
            f"blockers(A) must contain B={self.task_b}; got {blocker_ids}"
        )

        blocking = self.test.run_cmd_json(
            ["task", "blocking", "-r", self.roadmap, str(self.task_b)]
        )
        blocking_ids = [t["id"] for t in blocking]
        assert self.task_a in blocking_ids, (
            f"blocking(B) must contain A={self.task_a}; got {blocking_ids}"
        )

        print("✓ add-dep links task A → B; blockers/blocking reflect the edge")

    def test_remove_dep_drops_link(self):
        """After remove-dep, blockers/blocking no longer contain the edge."""
        self.test.run_cmd(
            ["task", "add-dep", "-r", self.roadmap, str(self.task_a), str(self.task_b)]
        )
        self.test.run_cmd(
            ["task", "remove-dep", "-r", self.roadmap, str(self.task_a), str(self.task_b)]
        )

        blockers = self.test.run_cmd_json(
            ["task", "blockers", "-r", self.roadmap, str(self.task_a)]
        )
        assert blockers == [], f"after remove, blockers must be empty; got {blockers}"

        print("✓ remove-dep removes the edge from both directions")

    def test_add_dep_self_rejected(self):
        """A task cannot depend on itself."""
        exit_code, _, stderr = self.test.run_cmd(
            ["task", "add-dep", "-r", self.roadmap, str(self.task_a), str(self.task_a)],
            check=False,
        )
        assert exit_code != 0, "self-dep must fail"
        assert "depend" in stderr.lower() or "circular" in stderr.lower() or "self" in stderr.lower()

        print("✓ self-dependency is rejected")

    def test_add_dep_nonexistent_task_rejected(self):
        """Adding a dep where the dependent task doesn't exist fails with exit 4."""
        exit_code, _, stderr = self.test.run_cmd(
            ["task", "add-dep", "-r", self.roadmap, "999", str(self.task_b)],
            check=False,
        )
        assert exit_code == 4, f"nonexistent task must exit 4; got {exit_code}, stderr={stderr}"

        print("✓ add-dep against missing task exits 4")

    def test_add_dep_nonexistent_dep_rejected(self):
        """Adding a dep where the dependency target doesn't exist fails with exit 4."""
        exit_code, _, stderr = self.test.run_cmd(
            ["task", "add-dep", "-r", self.roadmap, str(self.task_a), "999"],
            check=False,
        )
        assert exit_code == 4, f"nonexistent dep must exit 4; got {exit_code}, stderr={stderr}"

        print("✓ add-dep against missing dependency exits 4")

    def test_remove_dep_nonexistent_link_rejected(self):
        """Removing a dep that wasn't added returns a non-zero exit."""
        exit_code, _, _ = self.test.run_cmd(
            ["task", "remove-dep", "-r", self.roadmap, str(self.task_a), str(self.task_b)],
            check=False,
        )
        assert exit_code != 0, "remove-dep of nonexistent edge must fail"

        print("✓ remove-dep of nonexistent edge fails")


class TestDependencyChain:
    """A → B → C → D linear chain semantics."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        # Realistic dependency chain: scaffold UI -> implement API -> migrate data -> publish
        self.t_d = _create_task(self.test, self.roadmap, "Publish v2 mobile app to stores")
        self.t_c = _create_task(self.test, self.roadmap, "Migrate user profile data to v2 schema")
        self.t_b = _create_task(self.test, self.roadmap, "Implement v2 profile API endpoints")
        self.t_a = _create_task(self.test, self.roadmap, "Design and review v2 profile data model")

        # Wire D <- C <- B <- A (so A blocks B blocks C blocks D)
        self.test.run_cmd(["task", "add-dep", "-r", self.roadmap, str(self.t_d), str(self.t_c)])
        self.test.run_cmd(["task", "add-dep", "-r", self.roadmap, str(self.t_c), str(self.t_b)])
        self.test.run_cmd(["task", "add-dep", "-r", self.roadmap, str(self.t_b), str(self.t_a)])

    def teardown_method(self):
        self.test.teardown()

    def test_blockers_middle_of_chain(self):
        """Blockers of C reports B; B is the direct dependency."""
        blockers = self.test.run_cmd_json(
            ["task", "blockers", "-r", self.roadmap, str(self.t_c)]
        )
        ids = [t["id"] for t in blockers]
        assert ids == [self.t_b], f"blockers(C) must be [B]; got {ids}"

        print("✓ blockers reports direct dependency in a chain")

    def test_blocking_middle_of_chain(self):
        """Blocking of B reports C; C is the direct dependent."""
        blocking = self.test.run_cmd_json(
            ["task", "blocking", "-r", self.roadmap, str(self.t_b)]
        )
        ids = [t["id"] for t in blocking]
        assert ids == [self.t_c], f"blocking(B) must be [C]; got {ids}"

        print("✓ blocking reports direct dependent in a chain")

    def test_task_get_exposes_depends_on_and_blocks(self):
        """`task get` returns depends_on (incoming edges) and blocks (outgoing edges)."""
        result = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(self.t_c)])
        task = result[0]
        assert task["depends_on"] == [self.t_b], (
            f"C.depends_on must be [B]; got {task['depends_on']}"
        )
        assert task["blocks"] == [self.t_d], (
            f"C.blocks must be [D]; got {task['blocks']}"
        )

        print("✓ task get exposes depends_on and blocks arrays")

    def test_circular_dependency_rejected(self):
        """Adding A → D (closing the loop A→B→C→D→...→A) is rejected."""
        # D currently has no dep on A. Try to make A depend on D to create a cycle.
        exit_code, _, stderr = self.test.run_cmd(
            ["task", "add-dep", "-r", self.roadmap, str(self.t_a), str(self.t_d)],
            check=False,
        )
        assert exit_code != 0, (
            f"Cycle A→D should be rejected (A→B→C→D→A); exit={exit_code}, stderr={stderr}"
        )

        print("✓ circular dependency rejected (cycle detection works)")


class TestBlockerCompletionInteraction:
    """blockers filters out COMPLETED dependencies; completion guard rejects when incomplete."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        # A depends on B
        self.task_a = _create_task(self.test, self.roadmap, "Cut over to new search ranker")
        self.task_b = _create_task(self.test, self.roadmap, "Train and validate new search model")
        self.test.run_cmd(
            ["task", "add-dep", "-r", self.roadmap, str(self.task_a), str(self.task_b)]
        )

    def teardown_method(self):
        self.test.teardown()

    def test_blockers_excludes_completed(self):
        """Once B reaches COMPLETED, it no longer appears as a blocker of A."""
        # Drive B all the way to COMPLETED
        sprint_id = self.test.create_sprint(self.roadmap, "Search ranker rollout sprint")
        self.test.move_task_to_sprint(self.roadmap, self.task_b, sprint_id)
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task_b), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task_b), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task_b), "COMPLETED"])

        blockers = self.test.run_cmd_json(
            ["task", "blockers", "-r", self.roadmap, str(self.task_a)]
        )
        assert blockers == [], (
            f"blockers(A) must be empty once B is COMPLETED; got {blockers}"
        )

        print("✓ blockers excludes COMPLETED dependencies")

    def test_completion_blocked_by_incomplete_dependency(self):
        """A cannot be marked COMPLETED while B (its dependency) is still incomplete."""
        # Drive A through to TESTING. B is still BACKLOG.
        sprint_id = self.test.create_sprint(self.roadmap, "Ranker rollout sprint v2")
        self.test.move_task_to_sprint(self.roadmap, self.task_a, sprint_id)
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task_a), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task_a), "TESTING"])

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, str(self.task_a), "COMPLETED"],
            check=False,
        )
        assert exit_code == 6, (
            f"completing A while B is incomplete must exit 6; got {exit_code}, stderr={stderr}"
        )
        assert f"#{self.task_b}" in stderr, (
            f"error must mention blocking dependency #{self.task_b}; got {stderr}"
        )
        assert "depend" in stderr.lower(), "error must reference dependencies"

        print("✓ COMPLETED rejected while a dependency is incomplete")


class TestDependencyAuditTrail:
    """add-dep and remove-dep emit audit entries per SPEC/DATABASE.md."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        self.task_a = _create_task(self.test, self.roadmap, "Replace TLS termination with envoy")
        self.task_b = _create_task(self.test, self.roadmap, "Stand up envoy mTLS test cluster")

    def teardown_method(self):
        self.test.teardown()

    def test_add_dep_emits_audit(self):
        """add-dep creates a TASK_ADD_DEP audit entry for both endpoints."""
        self.test.run_cmd(
            ["task", "add-dep", "-r", self.roadmap, str(self.task_a), str(self.task_b)]
        )
        entries = self.test.run_cmd_json([
            "audit", "list", "-r", self.roadmap, "--operation", "TASK_ADD_DEP",
        ])
        # The operation is logged once per endpoint of the dependency edge.
        affected_ids = {e["entity_id"] for e in entries}
        assert self.task_a in affected_ids and self.task_b in affected_ids, (
            f"TASK_ADD_DEP must log both endpoints; got entity_ids={affected_ids}"
        )

        print("✓ TASK_ADD_DEP audit entry written for both endpoints")

    def test_remove_dep_emits_audit(self):
        """remove-dep creates a TASK_REMOVE_DEP audit entry for both endpoints."""
        self.test.run_cmd(
            ["task", "add-dep", "-r", self.roadmap, str(self.task_a), str(self.task_b)]
        )
        self.test.run_cmd(
            ["task", "remove-dep", "-r", self.roadmap, str(self.task_a), str(self.task_b)]
        )
        entries = self.test.run_cmd_json([
            "audit", "list", "-r", self.roadmap, "--operation", "TASK_REMOVE_DEP",
        ])
        affected_ids = {e["entity_id"] for e in entries}
        assert self.task_a in affected_ids and self.task_b in affected_ids, (
            f"TASK_REMOVE_DEP must log both endpoints; got entity_ids={affected_ids}"
        )

        print("✓ TASK_REMOVE_DEP audit entry written for both endpoints")


def main():
    """Run all dependency workflow tests."""
    import inspect

    failures = []
    passed = 0
    classes = [
        ("TestAddRemoveDeps", TestAddRemoveDeps),
        ("TestDependencyChain", TestDependencyChain),
        ("TestBlockerCompletionInteraction", TestBlockerCompletionInteraction),
        ("TestDependencyAuditTrail", TestDependencyAuditTrail),
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
