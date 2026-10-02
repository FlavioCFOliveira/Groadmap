#!/usr/bin/env python3
"""
Test 09: Volume and Bulk Operations

Tests behaviour with large volumes of data and bulk operations: that the
counts, the identifiers, the ordering and the state transitions stay correct at
scale.

It measures nothing. SPEC/BUILD.md "No Benchmarks and No Performance-Measurement
Tests" forbids a test that asserts a duration or compares two of them, so the
timing boundaries this module used to carry are gone along with the one test
whose only subject was how long a listing took. What survives is every assertion
about the DATA, which is what a volume test is for.
"""

import sys
import os
import random
import threading
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestStressLoad:
    """Correctness with large data volumes."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def _generate_task_title(self, index: int) -> str:
        """Generate a realistic task title with index for uniqueness."""
        prefixes = ["Implement", "Refactor", "Optimise", "Fix", "Deploy",
                    "Configure", "Migrate", "Integrate", "Document", "Test"]
        components = ["authentication service", "payment gateway", "data pipeline",
                      "API rate limiter", "caching layer", "message broker",
                      "monitoring dashboard", "CI/CD pipeline", "search index",
                      "notification system"]
        prefix = prefixes[index % len(prefixes)]
        component = components[index % len(components)]
        return f"{prefix} {component} [{index:04d}]"

    # ==================== Volume: 1000 Tasks ====================

    def test_create_1000_tasks(self):
        """Create 1000 tasks; verify count via stats (not limited list)."""
        roadmap = self.test.create_roadmap("large-backlog-project")

        task_ids = []

        for i in range(1000):
            task_id = self.test.create_task(
                roadmap,
                title=self._generate_task_title(i),
                functional_requirements=f"Business capability required by stakeholders (requirement {i})",
                technical_requirements=f"Engineering implementation approach for item {i}",
                acceptance_criteria=f"Feature verified in production environment (item {i})",
                priority=i % 10,
                severity=i % 10,
            )
            task_ids.append(task_id)
            if i > 0 and i % 200 == 0:
                print(f"  Created {i} tasks...")

        # Verify count using stats (avoids default list limit of 100)
        stats = self.test.run_cmd_json(["stats", "-r", roadmap])
        assert stats["tasks"]["backlog"] == 1000, \
            f"Expected 1000 tasks in backlog, got {stats['tasks']['backlog']}"

        # Verify IDs are sequential starting from 1
        assert task_ids[0] >= 1, "First task ID should be >= 1"
        assert task_ids[-1] == task_ids[0] + 999, \
            f"Task IDs should be sequential, first={task_ids[0]}, last={task_ids[-1]}"

    # ==================== Volume: 50 Sprints ====================

    def test_create_50_sprints(self):
        """Create 50 sprints and verify all are listed."""
        roadmap = self.test.create_roadmap("long-running-project")

        sprint_ids = []

        for i in range(1, 51):
            sprint_id = self.test.create_sprint(
                roadmap,
                description=f"Sprint {i:02d}: Development Phase — iteration {i}"
            )
            sprint_ids.append(sprint_id)
            if i % 10 == 0:
                print(f"  Created {i} sprints...")

        result = self.test.run_cmd_json(["sprint", "list", "-r", roadmap])
        assert len(result) == 50, f"Expected 50 sprints, got {len(result)}"

    # ==================== Bulk Sprint Population ====================

    def test_sprint_with_50_tasks(self):
        """Sprint with 50 tasks: add, reorder, and bulk-complete."""
        roadmap = self.test.create_roadmap("large-sprint-project")

        task_ids = [
            self.test.create_task(
                roadmap,
                title=self._generate_task_title(i),
                functional_requirements=f"Stakeholder requirement for sprint item {i}",
                technical_requirements=f"Technical specification for sprint item {i}",
                acceptance_criteria=f"Definition of done for sprint item {i}",
                priority=i % 10,
            )
            for i in range(50)
        ]

        sprint = self.test.create_sprint(roadmap, "Large Sprint with 50 Tasks")

        # Add all 50 tasks
        self.test.run_cmd(
            ["sprint", "add-tasks", "-r", roadmap, str(sprint), ",".join(str(t) for t in task_ids)]
        )

        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint)])

        sprint_data = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint)])
        assert sprint_data["summary"]["total_tasks"] == 50, \
            f"Sprint should have 50 tasks, got {sprint_data['summary']['total_tasks']}"

        # Reorder in reverse
        reversed_ids = list(reversed(task_ids))
        self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint),
            ",".join(str(t) for t in reversed_ids)
        ])

        sprint_data = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint)])
        assert sprint_data["task_order"] == reversed_ids, "Task order should be reversed"

        # Bulk-complete all tasks in batches of 10
        for batch_start in range(0, 50, 10):
            batch = task_ids[batch_start:batch_start + 10]
            ids_str = ",".join(str(t) for t in batch)
            self.test.run_cmd(["task", "stat", "-r", roadmap, ids_str, "DOING", "--commit-open", "021fa2f"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, ids_str, "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, ids_str, "COMPLETED", "--commit-close", "8007175"])

        stats = self.test.run_cmd_json(["stats", "-r", roadmap])
        assert stats["tasks"]["completed"] == 50, \
            f"Expected 50 completed tasks, got {stats['tasks']['completed']}"

        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint)])

    # ==================== Rapid Sequential Sprints ====================

    def test_rapid_sequential_sprints(self):
        """10 sprints opened and closed in sequence with tasks completing each cycle."""
        roadmap = self.test.create_roadmap("rapid-release-train")

        total_completed = 0

        for sprint_num in range(1, 11):
            # Create 5 tasks per sprint
            task_ids = [
                self.test.create_task(
                    roadmap,
                    title=f"Sprint {sprint_num:02d} — {self._generate_task_title(i)}",
                    functional_requirements=f"Functional requirement for sprint {sprint_num}, task {i}",
                    technical_requirements=f"Technical spec for sprint {sprint_num}, task {i}",
                    acceptance_criteria=f"Acceptance criteria for sprint {sprint_num}, task {i}",
                )
                for i in range(5)
            ]

            sprint = self.test.create_sprint(roadmap, f"Release Train Sprint {sprint_num:02d}")
            self.test.run_cmd(
                ["sprint", "add-tasks", "-r", roadmap, str(sprint), ",".join(str(t) for t in task_ids)]
            )
            self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint)])

            ids_str = ",".join(str(t) for t in task_ids)
            self.test.run_cmd(["task", "stat", "-r", roadmap, ids_str, "DOING", "--commit-open", "abd481c"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, ids_str, "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, ids_str, "COMPLETED", "--commit-close", "8a82583"])
            self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint)])

            total_completed += 5

        stats = self.test.run_cmd_json(["stats", "-r", roadmap])
        assert stats["tasks"]["completed"] == 50, \
            f"Expected 50 completed tasks, got {stats['tasks']['completed']}"
        assert stats["sprints"]["completed"] == 10, \
            f"Expected 10 completed sprints, got {stats['sprints']['completed']}"

    # ==================== Bulk Priority Updates ====================

    def test_bulk_priority_updates(self):
        """Batch priority changes on 100 tasks remain consistent."""
        roadmap = self.test.create_roadmap("priority-rebalancing")

        task_ids = [
            self.test.create_task(
                roadmap,
                title=self._generate_task_title(i),
                functional_requirements=f"Requirement {i} for the platform",
                technical_requirements=f"Implementation approach {i}",
                acceptance_criteria=f"Verification criteria {i}",
                priority=0,
            )
            for i in range(100)
        ]

        # Bulk set all to priority 9
        ids_str = ",".join(str(t) for t in task_ids)
        self.test.run_cmd(["task", "prio", "-r", roadmap, ids_str, "9"])

        # Spot-check 10 random tasks
        sample = random.sample(task_ids, 10)
        for task_id in sample:
            result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
            assert result[0]["priority"] == 9, \
                f"Task {task_id} priority should be 9, got {result[0]['priority']}"

        # Bulk update to priority 3
        self.test.run_cmd(["task", "prio", "-r", roadmap, ids_str, "3"])
        for task_id in sample:
            result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
            assert result[0]["priority"] == 3, \
                f"Task {task_id} priority should be 3, got {result[0]['priority']}"

    # ==================== Concurrent Read Access ====================

    def test_concurrent_read_access(self):
        """Multiple concurrent read operations return consistent results."""
        roadmap = self.test.create_roadmap("concurrent-reads")

        # Create a known dataset
        task_ids = [
            self.test.create_task(
                roadmap,
                title=self._generate_task_title(i),
                functional_requirements=f"Concurrent read requirement {i}",
                technical_requirements=f"Implementation detail {i}",
                acceptance_criteria=f"Verification {i}",
            )
            for i in range(20)
        ]

        errors = []
        results = []

        def read_tasks():
            try:
                result = self.test.run_cmd_json(["task", "list", "-r", roadmap, "-l", "50"])
                results.append(len(result))
            except Exception as e:
                errors.append(str(e))

        # Launch 5 concurrent readers
        threads = [threading.Thread(target=read_tasks) for _ in range(5)]
        for t in threads:
            t.start()
        for t in threads:
            t.join(timeout=30)

        assert not errors, f"Concurrent reads produced errors: {errors}"
        assert all(r == 20 for r in results), \
            f"Concurrent reads returned inconsistent counts: {results}"

    # ==================== Stats Accuracy at Scale ====================

    def test_stats_accuracy_at_scale(self):
        """Stats command accurately reflects task distribution across 200 tasks."""
        roadmap = self.test.create_roadmap("stats-at-scale")

        task_ids = [
            self.test.create_task(
                roadmap,
                title=self._generate_task_title(i),
                functional_requirements=f"Requirement {i}",
                technical_requirements=f"Implementation {i}",
                acceptance_criteria=f"Acceptance {i}",
            )
            for i in range(200)
        ]

        sprint = self.test.create_sprint(roadmap, "Scale Test Sprint")
        self.test.run_cmd(
            ["sprint", "add-tasks", "-r", roadmap, str(sprint), ",".join(str(t) for t in task_ids[:150])]
        )
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint)])

        # Move 50 to DOING
        ids_doing = ",".join(str(t) for t in task_ids[:50])
        self.test.run_cmd(["task", "stat", "-r", roadmap, ids_doing, "DOING", "--commit-open", "5d6a2cd"])

        # Move 30 to TESTING
        ids_testing = ",".join(str(t) for t in task_ids[:30])
        self.test.run_cmd(["task", "stat", "-r", roadmap, ids_testing, "TESTING"])

        # Complete 15
        ids_completed = ",".join(str(t) for t in task_ids[:15])
        self.test.run_cmd(["task", "stat", "-r", roadmap, ids_completed, "COMPLETED", "--commit-close", "b0ab692"])

        stats = self.test.run_cmd_json(["stats", "-r", roadmap])

        # tasks 150-199: BACKLOG (50)
        assert stats["tasks"]["backlog"] == 50, \
            f"Expected 50 backlog, got {stats['tasks']['backlog']}"
        # tasks 50-149: SPRINT (100 — not yet advanced)
        assert stats["tasks"]["sprint"] == 100, \
            f"Expected 100 sprint (tasks 50-149), got {stats['tasks']['sprint']}"
        # tasks 30-49: DOING (20 — advanced past first 30 to TESTING)
        assert stats["tasks"]["doing"] == 20, \
            f"Expected 20 doing (tasks 30-49), got {stats['tasks']['doing']}"
        # tasks 15-29: TESTING (15 — advanced past first 15 to COMPLETED)
        assert stats["tasks"]["testing"] == 15, \
            f"Expected 15 testing (tasks 15-29), got {stats['tasks']['testing']}"
        # tasks 0-14: COMPLETED (15)
        assert stats["tasks"]["completed"] == 15, \
            f"Expected 15 completed, got {stats['tasks']['completed']}"

        total = sum(stats["tasks"].values())
        assert total == 200, f"Expected 200 total, got {total}"


def main():
    """Run all tests."""
    test = TestStressLoad()
    methods = [m for m in dir(test) if m.startswith("test_")]
    passed = 0
    failed = 0

    for method_name in methods:
        test.setup_method()
        try:
            getattr(test, method_name)()
            passed += 1
        except Exception as e:
            print(f"✗ {method_name} failed: {e}")
            failed += 1
        finally:
            test.teardown_method()

    print(f"\n{passed} passed, {failed} failed")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if main() else 1)
