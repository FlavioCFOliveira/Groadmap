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
            "-t", "Test task",
            "-fr", "Functional requirements",
            "-tr", "Technical requirements",
            "-ac", "Acceptance criteria"
        ])
        assert "id" in result

        print("✓ Use roadmap test passed")

    def test_create_and_get_task(self):
        """Test creating and retrieving tasks."""
        roadmap = self.test.create_roadmap()

        # Create task
        task_id = self.test.create_task(
            roadmap=roadmap,
            title="Implement user authentication",
            functional_requirements="Users need to authenticate to access the system",
            technical_requirements="Create login and signup endpoints using JWT tokens",
            acceptance_criteria="Users can login, logout and access protected resources"
        )
        assert task_id > 0

        # Get task
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert len(result) == 1
        assert result[0]["id"] == task_id
        assert result[0]["title"] == "Implement user authentication"
        assert result[0]["functional_requirements"] == "Users need to authenticate to access the system"
        assert result[0]["technical_requirements"] == "Create login and signup endpoints using JWT tokens"
        assert result[0]["acceptance_criteria"] == "Users can login, logout and access protected resources"
        assert result[0]["status"] == "BACKLOG"
        assert result[0]["type"] == "TASK"

        print("✓ Create and get task test passed")

    def test_create_task_with_all_fields(self):
        """Test creating task with all optional fields."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(
            roadmap=roadmap,
            title="Fix critical security vulnerability",
            functional_requirements="Prevent SQL injection attacks",
            technical_requirements="Patch user query validation",
            acceptance_criteria="No more SQL injection vulnerabilities",
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
            title="Original title",
            functional_requirements="Original functional requirements",
            technical_requirements="Original technical requirements",
            acceptance_criteria="Original acceptance criteria"
        )

        # Edit title
        self.test.run_cmd([
            "task", "edit", "-r", roadmap, str(task_id),
            "-t", "Updated title"
        ])

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["title"] == "Updated title"
        assert result[0]["functional_requirements"] == "Original functional requirements"  # Unchanged

        # Edit multiple fields
        self.test.run_cmd([
            "task", "edit", "-r", roadmap, str(task_id),
            "-fr", "Updated functional requirements",
            "-tr", "Updated technical requirements",
            "-ac", "Updated acceptance criteria",
            "-p", "5"
        ])

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["functional_requirements"] == "Updated functional requirements"
        assert result[0]["technical_requirements"] == "Updated technical requirements"
        assert result[0]["acceptance_criteria"] == "Updated acceptance criteria"
        assert result[0]["priority"] == 5

        print("✓ Edit task test passed")

    def test_remove_task(self):
        """Test removing tasks."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(
            roadmap=roadmap,
            title="Task to be removed",
            functional_requirements="Requirements",
            technical_requirements="Technical details",
            acceptance_criteria="Criteria"
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
                title=f"Task {i+1}",
                functional_requirements=f"Functional requirements {i+1}",
                technical_requirements=f"Technical requirements {i+1}",
                acceptance_criteria=f"Acceptance criteria {i+1}"
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
        self.test.create_task(roadmap, "Task 1", "Functional 1", "Technical 1", "Criteria 1")
        self.test.create_task(roadmap, "Task 2", "Functional 2", "Technical 2", "Criteria 2")

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
            roadmap, "High priority task", "Functional", "Technical", "Criteria",
            priority=8, severity=3
        )
        task2 = self.test.create_task(
            roadmap, "Medium priority task", "Functional", "Technical", "Criteria",
            priority=5, severity=5
        )
        task3 = self.test.create_task(
            roadmap, "Low priority task", "Functional", "Technical", "Criteria",
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
