#!/usr/bin/env python3
"""
Test 03: Task State Machine
Tests task status transitions following the state machine rules.
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestTaskStateMachine:
    """Test task state machine transitions."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_backlog_to_sprint_transition(self):
        """Test BACKLOG -> SPRINT transition."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(
            roadmap, "Test task", "Action", "Result"
        )
        self.test.assert_task_status(roadmap, task_id, "BACKLOG")

        # Create sprint and add task
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])

        self.test.assert_task_status(roadmap, task_id, "SPRINT")

        print("✓ BACKLOG to SPRINT transition test passed")

    def test_sprint_to_doing_transition(self):
        """Test SPRINT -> DOING transition."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(roadmap, "Test task", "Action", "Result")
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])

        # Transition to DOING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        self.test.assert_task_status(roadmap, task_id, "DOING")

        print("✓ SPRINT to DOING transition test passed")

    def test_doing_to_testing_transition(self):
        """Test DOING -> TESTING transition."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(roadmap, "Test task", "Action", "Result")
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])

        # Transition to TESTING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self.test.assert_task_status(roadmap, task_id, "TESTING")

        print("✓ DOING to TESTING transition test passed")

    def test_testing_to_completed_transition(self):
        """Test TESTING -> COMPLETED transition."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(roadmap, "Test task", "Action", "Result")
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])

        # Transition to COMPLETED
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])
        self.test.assert_task_status(roadmap, task_id, "COMPLETED")

        # Verify completed_at is set
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["completed_at"] is not None

        print("✓ TESTING to COMPLETED transition test passed")

    def test_completed_to_backlog_transition(self):
        """Test COMPLETED -> BACKLOG transition (reopen)."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(roadmap, "Test task", "Action", "Result")
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")

        # Complete the task
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        # Reopen to BACKLOG
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "BACKLOG"])
        self.test.assert_task_status(roadmap, task_id, "BACKLOG")

        # Verify completed_at is cleared
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["completed_at"] is None

        print("✓ COMPLETED to BACKLOG transition test passed")

    def test_doing_to_sprint_transition(self):
        """Test DOING -> SPRINT transition (pause)."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(roadmap, "Test task", "Action", "Result")
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])

        # Pause - return to SPRINT
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "SPRINT"])
        self.test.assert_task_status(roadmap, task_id, "SPRINT")

        print("✓ DOING to SPRINT transition test passed")

    def test_testing_to_doing_transition(self):
        """Test TESTING -> DOING transition (failed test)."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(roadmap, "Test task", "Action", "Result")
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])

        # Failed test - return to DOING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        self.test.assert_task_status(roadmap, task_id, "DOING")

        print("✓ TESTING to DOING transition test passed")

    def test_sprint_to_backlog_transition(self):
        """Test SPRINT -> BACKLOG transition (remove from sprint)."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(roadmap, "Test task", "Action", "Result")
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])

        # Remove from sprint
        self.test.run_cmd([
            "sprint", "remove-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])
        self.test.assert_task_status(roadmap, task_id, "BACKLOG")

        print("✓ SPRINT to BACKLOG transition test passed")

    def test_invalid_transitions(self):
        """Test invalid state transitions are rejected."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(roadmap, "Test task", "Action", "Result")

        # BACKLOG cannot go directly to DOING
        exit_code, _, _ = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "DOING"],
            check=False
        )
        assert exit_code != 0, "BACKLOG -> DOING should be invalid"

        # BACKLOG cannot go directly to TESTING
        exit_code, _, _ = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "TESTING"],
            check=False
        )
        assert exit_code != 0, "BACKLOG -> TESTING should be invalid"

        # BACKLOG cannot go directly to COMPLETED
        exit_code, _, _ = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"],
            check=False
        )
        assert exit_code != 0, "BACKLOG -> COMPLETED should be invalid"

        # Add to sprint first
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])

        # SPRINT cannot go directly to TESTING
        exit_code, _, _ = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "TESTING"],
            check=False
        )
        assert exit_code != 0, "SPRINT -> TESTING should be invalid"

        # SPRINT cannot go directly to COMPLETED
        exit_code, _, _ = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"],
            check=False
        )
        assert exit_code != 0, "SPRINT -> COMPLETED should be invalid"

        print("✓ Invalid transitions test passed")

    def test_full_task_workflow(self):
        """Test a complete task workflow through all states."""
        roadmap = self.test.create_roadmap()

        # Create task
        task_id = self.test.create_task(
            roadmap,
            "Implement feature X",
            "Write code and tests",
            "Feature X working in production"
        )
        self.test.assert_task_status(roadmap, task_id, "BACKLOG")

        # Add to sprint
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])
        self.test.assert_task_status(roadmap, task_id, "SPRINT")

        # Start working
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        self.test.assert_task_status(roadmap, task_id, "DOING")

        # Submit for testing
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self.test.assert_task_status(roadmap, task_id, "TESTING")

        # Test failed, return to development
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        self.test.assert_task_status(roadmap, task_id, "DOING")

        # Submit for testing again
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self.test.assert_task_status(roadmap, task_id, "TESTING")

        # Complete
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])
        self.test.assert_task_status(roadmap, task_id, "COMPLETED")

        # Reopen (bug found in production)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "BACKLOG"])
        self.test.assert_task_status(roadmap, task_id, "BACKLOG")

        print("✓ Full task workflow test passed")

    def test_bulk_status_change(self):
        """Test changing status of multiple tasks at once."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")

        # Create multiple tasks
        task_ids = []
        for i in range(5):
            task_id = self.test.create_task(
                roadmap, f"Task {i+1}", f"Action {i+1}", f"Result {i+1}"
            )
            task_ids.append(task_id)

        # Add all to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Move all to DOING
        self.test.run_cmd([
            "task", "stat", "-r", roadmap,
            ",".join(map(str, task_ids)),
            "DOING"
        ])

        # Verify all are DOING
        for task_id in task_ids:
            self.test.assert_task_status(roadmap, task_id, "DOING")

        print("✓ Bulk status change test passed")


def main():
    """Run all tests."""
    test = TestTaskStateMachine()

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
