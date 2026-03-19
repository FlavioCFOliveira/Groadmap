#!/usr/bin/env python3
"""
Test 08: Complex Multi-Sprint Workflow
Tests a realistic development scenario with multiple sprints,
task assignments, and state transitions.
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestComplexWorkflow:
    """Test complex multi-sprint workflow scenarios."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_full_development_cycle(self):
        """Simulate a full development cycle."""
        roadmap = self.test.create_roadmap("web-app-v1")
        self.test.use_roadmap(roadmap)

        # Create backlog tasks
        tasks = {
            "auth": self.test.create_task(
                roadmap, "Implement user authentication",
                "Users need secure authentication",
                "Create JWT-based auth system",
                "Users can authenticate",
                priority=9, severity=8
            ),
            "database": self.test.create_task(
                roadmap, "Set up database schema",
                "Database needs to support features",
                "Design PostgreSQL schema",
                "Database schema works",
                priority=8, severity=7
            ),
        }

        # Create Sprint 1: Foundation
        sprint1 = self.test.create_sprint(roadmap, "Sprint 1: Foundation")
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint1), f"{tasks['database']},{tasks['auth']}"])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint1)])

        # Complete tasks (following state machine: DOING -> TESTING -> COMPLETED)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['database']), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['database']), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['database']), "COMPLETED"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['auth']), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['auth']), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['auth']), "COMPLETED"])
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint1)])

        print("✓ Full development cycle test passed")

    def test_parallel_sprint_management(self):
        """Test managing multiple sprints in parallel."""
        roadmap = self.test.create_roadmap("parallel-sprints")

        sprint1 = self.test.create_sprint(roadmap, "Team A Sprint")
        sprint2 = self.test.create_sprint(roadmap, "Team B Sprint")

        tasks_a = [self.test.create_task(roadmap, f"Task A{i}", f"Func {i}", f"Tech {i}", f"Crit {i}") for i in range(1, 3)]
        tasks_b = [self.test.create_task(roadmap, f"Task B{i}", f"Func {i}", f"Tech {i}", f"Crit {i}") for i in range(1, 3)]

        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint1)] + [str(t) for t in tasks_a])
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint2)] + [str(t) for t in tasks_b])

        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint1)])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint2)])

        result = self.test.run_cmd_json(["sprint", "list", "-r", roadmap, "--status", "OPEN"])
        assert len(result) == 2

        print("✓ Parallel sprint management test passed")


def main():
    """Run all tests."""
    test = TestComplexWorkflow()
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
