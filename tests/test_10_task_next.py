#!/usr/bin/env python3
"""
Test 10: Task Next Command
Tests retrieving next tasks from the currently open sprint with priority ordering.
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestTaskNext:
    """Test task next command functionality."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_next_task_no_open_sprint(self):
        """Test that task next fails when no sprint is open."""
        roadmap = self.test.create_roadmap()

        # Create some backlog tasks (not in sprint)
        self.test.create_task(
            roadmap=roadmap,
            title="Implement user authentication",
            functional_requirements="Users need secure login functionality",
            technical_requirements="Implement JWT-based authentication",
            acceptance_criteria="Users can login and receive valid tokens"
        )

        # Try to get next task without an open sprint - should fail
        exit_code, stdout, stderr = self.test.run_cmd(
            ["task", "next", "-r", roadmap],
            check=False
        )

        # Should return exit code 4 (not found) with error message
        assert exit_code == 4, f"Expected exit code 4, got {exit_code}"
        assert "no sprint is currently open" in stderr.lower() or \
               "no open sprint" in stderr.lower(), \
               f"Expected error about no open sprint, got: {stderr}"

        print("✓ Next task with no open sprint test passed")

    def test_next_task_empty_sprint(self):
        """Test that task next returns empty array when sprint has no open tasks."""
        roadmap = self.test.create_roadmap()

        # Create a sprint and open it
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1 - Initial Setup")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Sprint is open but has no tasks
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap])

        # Should return empty array with exit code 0
        assert result == [], f"Expected empty array, got: {result}"

        print("✓ Next task with empty sprint test passed")

    def test_next_single_task_default(self):
        """Test getting next single task (default N=1)."""
        roadmap = self.test.create_roadmap()

        # Create a sprint and open it
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1 - Core Features")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Create tasks in backlog
        task_id = self.test.create_task(
            roadmap=roadmap,
            title="Implement database migration system",
            functional_requirements="Database schema needs versioned migrations",
            technical_requirements="Create migration runner with rollback support",
            acceptance_criteria="Migrations run in order and are reversible",
            priority=5,
            severity=7
        )

        # Add task to sprint (moves from BACKLOG to SPRINT)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])

        # Get next task (should return the one task)
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap])

        assert len(result) == 1, f"Expected 1 task, got {len(result)}"
        assert result[0]["id"] == task_id, f"Expected task ID {task_id}, got {result[0]['id']}"
        assert result[0]["title"] == "Implement database migration system"
        assert result[0]["status"] in ["SPRINT", "DOING", "TESTING"], \
               f"Expected open status, got {result[0]['status']}"

        print("✓ Next single task default test passed")

    def test_next_multiple_tasks(self):
        """Test getting next N tasks."""
        roadmap = self.test.create_roadmap()

        # Create a sprint and open it
        sprint_id = self.test.create_sprint(roadmap, "Sprint 2 - API Development")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Create multiple tasks
        task_ids = []
        tasks_data = [
            {
                "title": "Design REST API endpoints",
                "functional": "API needs clear endpoints for client communication",
                "technical": "Define RESTful routes with proper HTTP methods",
                "criteria": "All endpoints documented and validated",
                "priority": 8,
                "severity": 6
            },
            {
                "title": "Implement request validation",
                "functional": "Prevent invalid data from entering system",
                "technical": "Add middleware for input validation",
                "criteria": "All inputs validated against schema",
                "priority": 7,
                "severity": 8
            },
            {
                "title": "Add API rate limiting",
                "functional": "Prevent abuse of API endpoints",
                "technical": "Implement token bucket rate limiter",
                "criteria": "Rate limits enforced per client",
                "priority": 6,
                "severity": 5
            },
            {
                "title": "Create API documentation",
                "functional": "Developers need API reference",
                "technical": "Generate OpenAPI specification",
                "criteria": "Documentation accessible and accurate",
                "priority": 5,
                "severity": 4
            }
        ]

        for task_data in tasks_data:
            task_id = self.test.create_task(
                roadmap=roadmap,
                title=task_data["title"],
                functional_requirements=task_data["functional"],
                technical_requirements=task_data["technical"],
                acceptance_criteria=task_data["criteria"],
                priority=task_data["priority"],
                severity=task_data["severity"]
            )
            task_ids.append(task_id)

        # Add all tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Get next 2 tasks
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap, "2"])

        assert len(result) == 2, f"Expected 2 tasks, got {len(result)}"

        # Verify returned tasks are from our set
        returned_ids = {task["id"] for task in result}
        assert returned_ids.issubset(set(task_ids)), \
               f"Returned IDs {returned_ids} not in expected {set(task_ids)}"

        print("✓ Next multiple tasks test passed")

    def test_next_task_ordering(self):
        """Test that tasks are returned in sprint task_order (position ordering)."""
        roadmap = self.test.create_roadmap()

        # Create a sprint and open it
        sprint_id = self.test.create_sprint(roadmap, "Sprint 3 - Security Features")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Create tasks with varying priorities and severities
        # Note: task next now uses sprint task_order, not severity/priority
        tasks_config = [
            {"title": "Fix critical SQL injection vulnerability", "priority": 9, "severity": 9},
            {"title": "Patch authentication bypass", "priority": 8, "severity": 9},
            {"title": "Implement password hashing", "priority": 9, "severity": 8},
            {"title": "Add session timeout handling", "priority": 7, "severity": 8},
            {"title": "Update security headers", "priority": 6, "severity": 7},
            {"title": "Review access control logs", "priority": 5, "severity": 6},
        ]

        task_ids = []

        for config in tasks_config:
            task_id = self.test.create_task(
                roadmap=roadmap,
                title=config["title"],
                functional_requirements="Critical security improvement required",
                technical_requirements="Implement secure coding practices",
                acceptance_criteria="Security review passed",
                priority=config["priority"],
                severity=config["severity"]
            )
            task_ids.append(task_id)

        # Add all tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Define a custom order using sprint reorder (reverse order)
        custom_order = list(reversed(task_ids))
        self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id),
            ",".join(map(str, custom_order))
        ])

        # Get all next tasks
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap, "10"])

        assert len(result) == len(tasks_config), \
               f"Expected {len(tasks_config)} tasks, got {len(result)}"

        # Verify ordering matches custom task_order (reversed)
        returned_ids = [task["id"] for task in result]
        assert returned_ids == custom_order, \
               f"Tasks not in expected order. Expected: {custom_order}, got: {returned_ids}"

        print("✓ Next task ordering by sprint task_order test passed")

    def test_next_exceeds_available_tasks(self):
        """Test requesting more tasks than available."""
        roadmap = self.test.create_roadmap()

        # Create a sprint and open it
        sprint_id = self.test.create_sprint(roadmap, "Sprint 4 - Bug Fixes")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Create only 2 tasks
        task1 = self.test.create_task(
            roadmap=roadmap,
            title="Fix memory leak in worker pool",
            functional_requirements="System stability requires memory efficiency",
            technical_requirements="Profile and fix goroutine leak",
            acceptance_criteria="Memory usage stable over 24 hours",
            priority=8,
            severity=8
        )
        task2 = self.test.create_task(
            roadmap=roadmap,
            title="Resolve database connection timeout",
            functional_requirements="Database connections must be reliable",
            technical_requirements="Implement connection pooling with retry",
            acceptance_criteria="No timeouts under normal load",
            priority=7,
            severity=7
        )

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task1},{task2}"
        ])

        # Request 10 tasks but only 2 available
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap, "10"])

        # Should return all available tasks (2)
        assert len(result) == 2, f"Expected 2 tasks, got {len(result)}"

        # Verify correct tasks returned
        returned_ids = {task["id"] for task in result}
        assert returned_ids == {task1, task2}, f"Expected IDs {task1, task2}, got {returned_ids}"

        print("✓ Next task exceeds available test passed")

    def test_next_ignores_non_sprint_tasks(self):
        """Test that only SPRINT, DOING, and TESTING tasks are considered."""
        roadmap = self.test.create_roadmap()

        # Create a sprint and open it
        sprint_id = self.test.create_sprint(roadmap, "Sprint 5 - Mixed Status Tasks")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Create tasks with different statuses
        backlog_task = self.test.create_task(
            roadmap=roadmap,
            title="Plan next quarter features",
            functional_requirements="Need roadmap for Q2",
            technical_requirements="Research and design",
            acceptance_criteria="Feature list approved",
            priority=9,
            severity=9
        )
        # Keep this in BACKLOG (don't add to sprint)

        sprint_task = self.test.create_task(
            roadmap=roadmap,
            title="Refactor authentication module",
            functional_requirements="Improve code maintainability",
            technical_requirements="Extract service layer",
            acceptance_criteria="Tests pass, coverage maintained",
            priority=8,
            severity=8
        )

        doing_task = self.test.create_task(
            roadmap=roadmap,
            title="Implement caching layer",
            functional_requirements="Reduce database load",
            technical_requirements="Add Redis cache wrapper",
            acceptance_criteria="Cache hit ratio above 80%",
            priority=7,
            severity=7
        )

        testing_task = self.test.create_task(
            roadmap=roadmap,
            title="Validate payment integration",
            functional_requirements="Payment processing must be reliable",
            technical_requirements="End-to-end payment testing",
            acceptance_criteria="All payment flows verified",
            priority=6,
            severity=6
        )

        completed_task = self.test.create_task(
            roadmap=roadmap,
            title="Setup CI/CD pipeline",
            functional_requirements="Automated deployment required",
            technical_requirements="Configure GitHub Actions",
            acceptance_criteria="Build and deploy automated",
            priority=5,
            severity=5
        )

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{sprint_task},{doing_task},{testing_task},{completed_task}"
        ])

        # Move tasks to different statuses (following state machine: SPRINT -> DOING -> TESTING -> COMPLETED)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(doing_task), "DOING"])
        # For testing_task: need SPRINT -> DOING -> TESTING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(testing_task), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(testing_task), "TESTING"])
        # For completed_task: need SPRINT -> DOING -> TESTING -> COMPLETED
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(completed_task), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(completed_task), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(completed_task), "COMPLETED"])

        # Get next tasks
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap, "10"])

        # Should only return SPRINT, DOING, and TESTING tasks
        returned_ids = {task["id"] for task in result}
        expected_ids = {sprint_task, doing_task, testing_task}

        assert returned_ids == expected_ids, \
               f"Expected IDs {expected_ids}, got {returned_ids}\n" \
               f"Tasks: {[(t['id'], t['title'], t['status']) for t in result]}"

        # Verify BACKLOG and COMPLETED tasks are not included
        assert backlog_task not in returned_ids, \
               f"BACKLOG task {backlog_task} should not be in results"
        assert completed_task not in returned_ids, \
               f"COMPLETED task {completed_task} should not be in results"

        # Verify statuses
        for task in result:
            assert task["status"] in ["SPRINT", "DOING", "TESTING"], \
                   f"Task {task['id']} has unexpected status {task['status']}"

        print("✓ Next task ignores non-sprint tasks test passed")

    def test_next_with_specific_roadmap(self):
        """Test task next with explicit roadmap flag."""
        # Create two roadmaps
        roadmap1 = self.test.create_roadmap("project-alpha")
        roadmap2 = self.test.create_roadmap("project-beta")

        # Create sprint in roadmap1 and open it
        sprint1 = self.test.create_sprint(roadmap1, "Alpha Sprint 1")
        self.test.run_cmd(["sprint", "start", "-r", roadmap1, str(sprint1)])

        # Create task in roadmap1
        task1 = self.test.create_task(
            roadmap=roadmap1,
            title="Implement Alpha feature",
            functional_requirements="Alpha needs feature X",
            technical_requirements="Implement X module",
            acceptance_criteria="X works",
            priority=9,
            severity=9
        )
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap1, str(sprint1), str(task1)
        ])

        # Create sprint in roadmap2 and open it
        sprint2 = self.test.create_sprint(roadmap2, "Beta Sprint 1")
        self.test.run_cmd(["sprint", "start", "-r", roadmap2, str(sprint2)])

        # Create task in roadmap2
        task2 = self.test.create_task(
            roadmap=roadmap2,
            title="Implement Beta feature",
            functional_requirements="Beta needs feature Y",
            technical_requirements="Implement Y module",
            acceptance_criteria="Y works",
            priority=8,
            severity=8
        )
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap2, str(sprint2), str(task2)
        ])

        # Get next task from roadmap1 explicitly
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap1])

        assert len(result) == 1, f"Expected 1 task, got {len(result)}"
        assert result[0]["id"] == task1, f"Expected task {task1}, got {result[0]['id']}"
        assert "Alpha" in result[0]["title"]

        # Get next task from roadmap2 explicitly
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap2])

        assert len(result) == 1, f"Expected 1 task, got {len(result)}"
        assert result[0]["id"] == task2, f"Expected task {task2}, got {result[0]['id']}"
        assert "Beta" in result[0]["title"]

        print("✓ Next task with specific roadmap test passed")

    def test_next_no_roadmap_specified(self):
        """Test task next fails when no roadmap specified and no default set."""
        # Don't set default roadmap

        # Try to get next task without specifying roadmap
        exit_code, stdout, stderr = self.test.run_cmd(
            ["task", "next"],
            check=False
        )

        # Should fail with appropriate error (exit code 3 = no roadmap)
        assert exit_code == 3, f"Expected exit code 3, got {exit_code}"
        assert "roadmap" in stderr.lower(), \
               f"Expected error about roadmap, got: {stderr}"

        print("✓ Next task no roadmap specified test passed")

    def test_next_task_json_structure(self):
        """Test that task next returns properly structured JSON."""
        roadmap = self.test.create_roadmap()

        # Create a sprint and open it
        sprint_id = self.test.create_sprint(roadmap, "Sprint 6 - JSON Validation")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Create a task with all fields
        task_id = self.test.create_task(
            roadmap=roadmap,
            title="Implement comprehensive logging",
            functional_requirements="System needs observability",
            technical_requirements="Add structured logging with correlation IDs",
            acceptance_criteria="Logs queryable in centralized system",
            priority=7,
            severity=6,
            specialists="backend,devops"
        )

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])

        # Get next task
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap])

        assert len(result) == 1, f"Expected 1 task, got {len(result)}"

        task = result[0]

        # Verify all expected fields exist
        required_fields = [
            "id", "title", "status", "functional_requirements",
            "technical_requirements", "acceptance_criteria",
            "created_at", "priority", "severity"
        ]

        for field in required_fields:
            assert field in task, f"Required field '{field}' missing from response"

        # Verify field types and values
        assert isinstance(task["id"], int), f"id should be int, got {type(task['id'])}"
        assert isinstance(task["title"], str), f"title should be str"
        assert isinstance(task["priority"], int), f"priority should be int"
        assert isinstance(task["severity"], int), f"severity should be int"
        assert task["status"] in ["SPRINT", "DOING", "TESTING"]

        # Verify task values
        assert task["title"] == "Implement comprehensive logging"
        assert task["priority"] == 7
        assert task["severity"] == 6
        assert task["specialists"] == "backend,devops"

        print("✓ Next task JSON structure test passed")

    def test_next_zero_tasks_requested(self):
        """Test requesting zero tasks."""
        roadmap = self.test.create_roadmap()

        # Create a sprint and open it
        sprint_id = self.test.create_sprint(roadmap, "Sprint 7 - Edge Cases")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Create a task
        task_id = self.test.create_task(
            roadmap=roadmap,
            title="Optimize database queries",
            functional_requirements="Improve response times",
            technical_requirements="Add query optimization",
            acceptance_criteria="Response time under 100ms"
        )

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])

        # Request 0 tasks - should fail with invalid input (must be positive)
        exit_code, stdout, stderr = self.test.run_cmd(
            ["task", "next", "-r", roadmap, "0"],
            check=False
        )

        # Should return exit code 6 (invalid input)
        assert exit_code == 6, f"Expected exit code 6 for zero, got {exit_code}"
        assert "positive" in stderr.lower() or "invalid" in stderr.lower(), \
               f"Expected error about positive integer, got: {stderr}"

        print("✓ Next task zero requested test passed")

    def test_next_invalid_argument(self):
        """Test task next with invalid argument."""
        roadmap = self.test.create_roadmap()

        # Create a sprint and open it
        sprint_id = self.test.create_sprint(roadmap, "Sprint 8 - Error Handling")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Try with invalid (non-numeric) argument
        exit_code, stdout, stderr = self.test.run_cmd(
            ["task", "next", "-r", roadmap, "invalid"],
            check=False
        )

        # Should fail with error (exit code 6 = invalid input)
        assert exit_code == 6, f"Expected exit code 6, got {exit_code}"
        assert "invalid" in stderr.lower() or "number" in stderr.lower() or \
               "integer" in stderr.lower(), \
               f"Expected error about invalid argument, got: {stderr}"

        # Try with negative number
        exit_code, stdout, stderr = self.test.run_cmd(
            ["task", "next", "-r", roadmap, "-5"],
            check=False
        )

        assert exit_code == 6, f"Expected exit code 6 for negative, got {exit_code}"

        print("✓ Next task invalid argument test passed")

    def test_next_after_task_status_changes(self):
        """Test that task next reflects status changes."""
        roadmap = self.test.create_roadmap()

        # Create a sprint and open it
        sprint_id = self.test.create_sprint(roadmap, "Sprint 9 - Status Workflow")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Create tasks
        task1 = self.test.create_task(
            roadmap=roadmap,
            title="Implement feature A",
            functional_requirements="Need feature A",
            technical_requirements="Build A",
            acceptance_criteria="A works",
            priority=9,
            severity=9
        )
        task2 = self.test.create_task(
            roadmap=roadmap,
            title="Implement feature B",
            functional_requirements="Need feature B",
            technical_requirements="Build B",
            acceptance_criteria="B works",
            priority=8,
            severity=8
        )

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task1},{task2}"
        ])

        # Initially both tasks should appear
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap, "10"])
        assert len(result) == 2, f"Expected 2 tasks initially, got {len(result)}"

        # Move task1 to DOING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "DOING"])

        # task1 should still appear
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap, "10"])
        returned_ids = {task["id"] for task in result}
        assert task1 in returned_ids, "DOING task should still appear"
        assert task2 in returned_ids, "SPRINT task should appear"

        # Move task1 to TESTING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "TESTING"])

        # task1 should still appear
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap, "10"])
        returned_ids = {task["id"] for task in result}
        assert task1 in returned_ids, "TESTING task should still appear"
        assert task2 in returned_ids, "SPRINT task should appear"

        # Complete task1
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "COMPLETED"])

        # task1 should NOT appear anymore
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap, "10"])
        returned_ids = {task["id"] for task in result}
        assert task1 not in returned_ids, "COMPLETED task should not appear"
        assert task2 in returned_ids, "SPRINT task should still appear"

        # Return task1 to BACKLOG
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "BACKLOG"])

        # task1 should still NOT appear (not in sprint)
        result = self.test.run_cmd_json(["task", "next", "-r", roadmap, "10"])
        returned_ids = {task["id"] for task in result}
        assert task1 not in returned_ids, "BACKLOG task should not appear"
        assert task2 in returned_ids, "SPRINT task should still appear"

        print("✓ Next task after status changes test passed")


def main():
    """Run all tests."""
    test = TestTaskNext()

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
            import traceback
            traceback.print_exc()
            failed += 1
        finally:
            test.teardown_method()

    print(f"\n{passed} passed, {failed} failed")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if main() else 1)
