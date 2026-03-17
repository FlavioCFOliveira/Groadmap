#!/usr/bin/env python3
"""
Test 04: Sprint Task Management
Tests adding, removing, and moving tasks between sprints.
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestSprintTaskManagement:
    """Test sprint task management operations."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_add_tasks_to_sprint(self):
        """Test adding tasks to a sprint."""
        roadmap = self.test.create_roadmap()

        # Create tasks
        task1 = self.test.create_task(roadmap, "Task 1", "Action 1", "Result 1")
        task2 = self.test.create_task(roadmap, "Task 2", "Action 2", "Result 2")
        task3 = self.test.create_task(roadmap, "Task 3", "Action 3", "Result 3")

        # Create sprint
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")

        # Add single task
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task1)
        ])

        # Verify task is in sprint
        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint_id)])
        assert len(result) == 1
        assert result[0]["id"] == task1

        # Add multiple tasks
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task2},{task3}"
        ])

        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint_id)])
        assert len(result) == 3

        print("✓ Add tasks to sprint test passed")

    def test_remove_tasks_from_sprint(self):
        """Test removing tasks from a sprint."""
        roadmap = self.test.create_roadmap()

        # Create tasks
        task1 = self.test.create_task(roadmap, "Task 1", "Action 1", "Result 1")
        task2 = self.test.create_task(roadmap, "Task 2", "Action 2", "Result 2")
        task3 = self.test.create_task(roadmap, "Task 3", "Action 3", "Result 3")

        # Create sprint and add all tasks
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task1},{task2},{task3}"
        ])

        # Verify all tasks are in sprint
        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint_id)])
        assert len(result) == 3

        # Remove single task
        self.test.run_cmd([
            "sprint", "remove-tasks", "-r", roadmap, str(sprint_id), str(task1)
        ])

        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint_id)])
        assert len(result) == 2

        # Verify task1 is back to BACKLOG
        self.test.assert_task_status(roadmap, task1, "BACKLOG")

        # Remove multiple tasks
        self.test.run_cmd([
            "sprint", "remove-tasks", "-r", roadmap, str(sprint_id),
            f"{task2},{task3}"
        ])

        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint_id)])
        assert len(result) == 0

        print("✓ Remove tasks from sprint test passed")

    def test_move_tasks_between_sprints(self):
        """Test moving tasks between sprints."""
        roadmap = self.test.create_roadmap()

        # Create tasks
        task1 = self.test.create_task(roadmap, "Task 1", "Action 1", "Result 1")
        task2 = self.test.create_task(roadmap, "Task 2", "Action 2", "Result 2")

        # Create two sprints
        sprint1 = self.test.create_sprint(roadmap, "Sprint 1")
        sprint2 = self.test.create_sprint(roadmap, "Sprint 2")

        # Add tasks to sprint 1
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint1),
            f"{task1},{task2}"
        ])

        # Verify tasks are in sprint 1
        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint1)])
        assert len(result) == 2

        # Move tasks to sprint 2
        self.test.run_cmd([
            "sprint", "move-tasks", "-r", roadmap, str(sprint1), str(sprint2),
            f"{task1},{task2}"
        ])

        # Verify tasks are now in sprint 2
        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint2)])
        assert len(result) == 2

        # Verify sprint 1 is empty
        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint1)])
        assert len(result) == 0

        print("✓ Move tasks between sprints test passed")

    def test_sprint_tasks_with_status_filter(self):
        """Test filtering sprint tasks by status."""
        roadmap = self.test.create_roadmap()

        # Create tasks
        task1 = self.test.create_task(roadmap, "Task 1", "Action 1", "Result 1")
        task2 = self.test.create_task(roadmap, "Task 2", "Action 2", "Result 2")
        task3 = self.test.create_task(roadmap, "Task 3", "Action 3", "Result 3")

        # Create sprint and add all tasks
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task1},{task2},{task3}"
        ])

        # Move tasks to different statuses (following state machine rules)
        # Task 1: SPRINT -> DOING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "DOING"])
        # Task 2: SPRINT -> DOING -> TESTING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task2), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task2), "TESTING"])
        # Task 3: SPRINT -> DOING -> TESTING -> COMPLETED
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task3), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task3), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task3), "COMPLETED"])

        # Filter by SPRINT status
        result = self.test.run_cmd_json([
            "sprint", "tasks", "-r", roadmap, str(sprint_id), "--status", "SPRINT"
        ])
        assert len(result) == 0  # All moved out of SPRINT

        # Filter by DOING status
        result = self.test.run_cmd_json([
            "sprint", "tasks", "-r", roadmap, str(sprint_id), "--status", "DOING"
        ])
        assert len(result) == 1
        assert result[0]["id"] == task1

        # Filter by TESTING status
        result = self.test.run_cmd_json([
            "sprint", "tasks", "-r", roadmap, str(sprint_id), "--status", "TESTING"
        ])
        assert len(result) == 1
        assert result[0]["id"] == task2

        # Filter by COMPLETED status
        result = self.test.run_cmd_json([
            "sprint", "tasks", "-r", roadmap, str(sprint_id), "--status", "COMPLETED"
        ])
        assert len(result) == 1
        assert result[0]["id"] == task3

        print("✓ Sprint tasks with status filter test passed")

    def test_sprint_statistics(self):
        """Test sprint statistics calculation."""
        roadmap = self.test.create_roadmap()

        # Create tasks with different priorities
        task1 = self.test.create_task(roadmap, "Task 1", "Action", "Result", priority=9)
        task2 = self.test.create_task(roadmap, "Task 2", "Action", "Result", priority=5)
        task3 = self.test.create_task(roadmap, "Task 3", "Action", "Result", priority=3)
        task4 = self.test.create_task(roadmap, "Task 4", "Action", "Result", priority=1)

        # Create sprint and add all tasks
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task1},{task2},{task3},{task4}"
        ])

        # Get initial stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 4
        assert result["completed_tasks"] == 0
        assert result["progress_percentage"] == 0.0
        assert result["status_distribution"]["SPRINT"] == 4

        # Progress some tasks
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "COMPLETED"])

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task2), "DOING"])

        # Get updated stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 4
        assert result["completed_tasks"] == 1
        assert result["progress_percentage"] == 25.0
        assert result["status_distribution"]["COMPLETED"] == 1
        assert result["status_distribution"]["DOING"] == 1
        assert result["status_distribution"]["SPRINT"] == 2

        # Complete all tasks
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task2), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task2), "COMPLETED"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task3), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task3), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task3), "COMPLETED"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task4), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task4), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task4), "COMPLETED"])

        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["completed_tasks"] == 4
        assert result["progress_percentage"] == 100.0

        print("✓ Sprint statistics test passed")

    def test_sprint_task_reassignment(self):
        """Test reassigning task from one sprint to another."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(roadmap, "Shared task", "Action", "Result")

        sprint1 = self.test.create_sprint(roadmap, "Sprint 1")
        sprint2 = self.test.create_sprint(roadmap, "Sprint 2")

        # Add to sprint 1
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint1), str(task_id)
        ])

        # Verify in sprint 1
        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint1)])
        assert len(result) == 1

        # Add same task to sprint 2 (should move it)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint2), str(task_id)
        ])

        # Verify moved to sprint 2
        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint2)])
        assert len(result) == 1

        # Sprint 1 should be empty
        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint1)])
        assert len(result) == 0

        print("✓ Sprint task reassignment test passed")

    def test_remove_sprint_with_tasks(self):
        """Test removing a sprint that contains tasks."""
        roadmap = self.test.create_roadmap()

        # Create tasks
        task1 = self.test.create_task(roadmap, "Task 1", "Action 1", "Result 1")
        task2 = self.test.create_task(roadmap, "Task 2", "Action 2", "Result 2")

        # Create sprint and add tasks
        sprint_id = self.test.create_sprint(roadmap, "Sprint to remove")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task1},{task2}"
        ])

        # Move one task to DOING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "DOING"])

        # Remove sprint
        self.test.run_cmd(["sprint", "remove", "-r", roadmap, str(sprint_id)])

        # Verify tasks are back to BACKLOG
        self.test.assert_task_status(roadmap, task1, "BACKLOG")
        self.test.assert_task_status(roadmap, task2, "BACKLOG")

        # Verify tasks still exist
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task1)])
        assert len(result) == 1

        print("✓ Remove sprint with tasks test passed")


def main():
    """Run all tests."""
    test = TestSprintTaskManagement()

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
