#!/usr/bin/env python3
"""
Test 01: Basic CRUD Operations
Tests fundamental create, read, update, delete operations for roadmaps and tasks.
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestBasicCRUD:
    """Test basic CRUD operations."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_create_and_list_roadmaps(self):
        """Test creating and listing roadmaps."""
        # Initially no roadmaps
        result = self.test.run_cmd_json(["roadmap", "list"])
        assert result == [], "Expected empty roadmap list initially"

        # Create first roadmap
        name1 = self.test.create_roadmap("project-alpha")
        result = self.test.run_cmd_json(["roadmap", "list"])
        assert len(result) == 1
        assert result[0]["name"] == name1

        # Create second roadmap
        name2 = self.test.create_roadmap("project-beta")
        result = self.test.run_cmd_json(["roadmap", "list"])
        assert len(result) == 2
        names = [r["name"] for r in result]
        assert name1 in names
        assert name2 in names

        print("✓ Create and list roadmaps test passed")

    def test_use_roadmap(self):
        """Test setting default roadmap."""
        name = self.test.create_roadmap()

        # Set as default
        self.test.run_cmd(["roadmap", "use", name])

        # Create task without specifying roadmap (should use default)
        result = self.test.run_cmd_json([
            "task", "create",
            "-d", "Test task",
            "-a", "Do something",
            "-e", "Result achieved"
        ])
        assert "id" in result

        print("✓ Use roadmap test passed")

    def test_create_and_get_task(self):
        """Test creating and retrieving tasks."""
        roadmap = self.test.create_roadmap()

        # Create task
        task_id = self.test.create_task(
            roadmap=roadmap,
            description="Implement user authentication",
            action="Create login and signup endpoints",
            expected_result="Users can authenticate successfully"
        )
        assert task_id > 0

        # Get task
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert len(result) == 1
        assert result[0]["id"] == task_id
        assert result[0]["description"] == "Implement user authentication"
        assert result[0]["action"] == "Create login and signup endpoints"
        assert result[0]["expected_result"] == "Users can authenticate successfully"
        assert result[0]["status"] == "BACKLOG"

        print("✓ Create and get task test passed")

    def test_create_task_with_all_fields(self):
        """Test creating task with all optional fields."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(
            roadmap=roadmap,
            description="Fix critical security vulnerability",
            action="Patch SQL injection in user query",
            expected_result="No more SQL injection vulnerabilities",
            priority=9,
            severity=9,
            specialists="security-team,backend-dev"
        )

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        task = result[0]

        assert task["priority"] == 9
        assert task["severity"] == 9
        assert task["specialists"] == "security-team,backend-dev"

        print("✓ Create task with all fields test passed")

    def test_edit_task(self):
        """Test editing task properties."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(
            roadmap=roadmap,
            description="Original description",
            action="Original action",
            expected_result="Original result"
        )

        # Edit description
        self.test.run_cmd([
            "task", "edit", "-r", roadmap, str(task_id),
            "-d", "Updated description"
        ])

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["description"] == "Updated description"
        assert result[0]["action"] == "Original action"  # Unchanged

        # Edit multiple fields
        self.test.run_cmd([
            "task", "edit", "-r", roadmap, str(task_id),
            "-a", "Updated action",
            "-e", "Updated result",
            "-p", "5"
        ])

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["action"] == "Updated action"
        assert result[0]["expected_result"] == "Updated result"
        assert result[0]["priority"] == 5

        print("✓ Edit task test passed")

    def test_remove_task(self):
        """Test removing tasks."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(
            roadmap=roadmap,
            description="Task to be removed",
            action="Do something",
            expected_result="Done"
        )

        # Verify task exists
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert len(result) == 1

        # Remove task
        self.test.run_cmd(["task", "remove", "-r", roadmap, str(task_id)])

        # Verify task is gone (returns empty list with exit code 0)
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert len(result) == 0, f"Expected empty list, got {result}"

        # Verify task count decreased
        result = self.test.run_cmd_json(["task", "list", "-r", roadmap])
        assert len(result) == 0

        print("✓ Remove task test passed")

    def test_bulk_task_operations(self):
        """Test bulk operations on multiple tasks."""
        roadmap = self.test.create_roadmap()

        # Create multiple tasks
        task_ids = []
        for i in range(5):
            task_id = self.test.create_task(
                roadmap=roadmap,
                description=f"Task {i+1}",
                action=f"Action {i+1}",
                expected_result=f"Result {i+1}"
            )
            task_ids.append(task_id)

        # Get all tasks at once
        result = self.test.run_cmd_json(
            ["task", "get", "-r", roadmap, ",".join(map(str, task_ids))]
        )
        assert len(result) == 5

        # Set priority for multiple tasks
        self.test.run_cmd([
            "task", "prio", "-r", roadmap,
            ",".join(map(str, task_ids[:3])),
            "7"
        ])

        # Verify priority change
        result = self.test.run_cmd_json(
            ["task", "get", "-r", roadmap, str(task_ids[0])]
        )
        assert result[0]["priority"] == 7

        # Remove multiple tasks
        self.test.run_cmd([
            "task", "remove", "-r", roadmap,
            ",".join(map(str, task_ids[:2]))
        ])

        # Verify removal
        result = self.test.run_cmd_json(["task", "list", "-r", roadmap])
        assert len(result) == 3

        print("✓ Bulk task operations test passed")

    def test_remove_roadmap(self):
        """Test removing a roadmap."""
        roadmap = self.test.create_roadmap()

        # Create some tasks
        self.test.create_task(roadmap, "Task 1", "Action 1", "Result 1")
        self.test.create_task(roadmap, "Task 2", "Action 2", "Result 2")

        # Remove roadmap
        self.test.run_cmd(["roadmap", "remove", roadmap])

        # Verify roadmap is gone
        result = self.test.run_cmd_json(["roadmap", "list"])
        names = [r["name"] for r in result]
        assert roadmap not in names

        print("✓ Remove roadmap test passed")

    def test_task_list_filters(self):
        """Test task listing with various filters."""
        roadmap = self.test.create_roadmap()

        # Create tasks with different properties
        task1 = self.test.create_task(
            roadmap, "High priority task", "Action", "Result",
            priority=8, severity=3
        )
        task2 = self.test.create_task(
            roadmap, "Medium priority task", "Action", "Result",
            priority=5, severity=5
        )
        task3 = self.test.create_task(
            roadmap, "Low priority task", "Action", "Result",
            priority=2, severity=7
        )

        # Filter by priority
        result = self.test.list_tasks(roadmap, priority=5)
        assert len(result) == 2  # priority >= 5

        # Filter by severity
        result = self.test.list_tasks(roadmap, severity=5)
        assert len(result) == 2  # severity >= 5

        # Filter by status
        result = self.test.list_tasks(roadmap, status="BACKLOG")
        assert len(result) == 3

        # Limit results
        result = self.test.list_tasks(roadmap, limit=2)
        assert len(result) == 2

        print("✓ Task list filters test passed")


def main():
    """Run all tests."""
    test = TestBasicCRUD()

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
