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
        """
        Simulate a full development cycle:
        - Create roadmap for a web application
        - Plan multiple sprints
        - Assign tasks to sprints
        - Progress tasks through states
        - Complete sprints
        """
        # Create roadmap for web application project
        roadmap = self.test.create_roadmap("web-app-v1")
        self.test.use_roadmap(roadmap)

        # Create backlog tasks
        tasks = {
            "auth": self.test.create_task(
                roadmap, "Implement user authentication",
                "Create JWT-based auth system with login/logout endpoints",
                "Users can securely authenticate and maintain sessions",
                priority=9, severity=8, specialists="backend-dev,security-team"
            ),
            "database": self.test.create_task(
                roadmap, "Set up database schema",
                "Design and implement PostgreSQL schema with migrations",
                "Database schema supports all application features",
                priority=8, severity=7, specialists="dba,backend-dev"
            ),
            "api": self.test.create_task(
                roadmap, "Build REST API",
                "Implement CRUD endpoints for core resources",
                "API follows REST conventions and returns proper status codes",
                priority=8, severity=6, specialists="backend-dev"
            ),
            "frontend": self.test.create_task(
                roadmap, "Create React frontend",
                "Build responsive UI with React and Tailwind CSS",
                "Frontend is responsive and accessible",
                priority=7, severity=5, specialists="frontend-dev,ux-designer"
            ),
            "tests": self.test.create_task(
                roadmap, "Write integration tests",
                "Create comprehensive test suite with 80% coverage",
                "All critical paths have automated tests",
                priority=6, severity=4, specialists="qa-engineer,backend-dev"
            ),
            "docs": self.test.create_task(
                roadmap, "Write API documentation",
                "Document all API endpoints with OpenAPI/Swagger",
                "API documentation is complete and accurate",
                priority=5, severity=3, specialists="technical-writer"
            ),
            "deploy": self.test.create_task(
                roadmap, "Set up CI/CD pipeline",
                "Configure GitHub Actions for automated testing and deployment",
                "Code changes trigger automated tests and deployment",
                priority=7, severity=6, specialists="devops-engineer"
            ),
            "monitoring": self.test.create_task(
                roadmap, "Add monitoring and logging",
                "Integrate Prometheus and Grafana for metrics",
                "System health is monitored with alerts configured",
                priority=6, severity=7, specialists="devops-engineer,backend-dev"
            ),
        }

        # Create Sprint 1: Foundation
        sprint1 = self.test.create_sprint(roadmap, "Sprint 1: Foundation - Database and Auth")

        # Add foundation tasks to sprint 1
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint1),
            f"{tasks['database']},{tasks['auth']}"
        ])

        # Start sprint 1
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint1)])

        # Progress database task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['database']), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['database']), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['database']), "COMPLETED"])

        # Progress auth task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['auth']), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['auth']), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['auth']), "COMPLETED"])

        # Close sprint 1
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint1)])

        # Create Sprint 2: API Development
        sprint2 = self.test.create_sprint(roadmap, "Sprint 2: API Development")

        # Add API and tests to sprint 2
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint2),
            f"{tasks['api']},{tasks['tests']}"
        ])

        # Start sprint 2
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint2)])

        # Progress API task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['api']), "DOING"])

        # Tests task depends on API, so it stays in SPRINT
        # Move tests to DOING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['tests']), "DOING"])

        # Complete API
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['api']), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['api']), "COMPLETED"])

        # Complete tests
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['tests']), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['tests']), "COMPLETED"])

        # Close sprint 2
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint2)])

        # Create Sprint 3: Frontend and Documentation
        sprint3 = self.test.create_sprint(roadmap, "Sprint 3: Frontend and Documentation")

        # Add frontend and docs to sprint 3
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint3),
            f"{tasks['frontend']},{tasks['docs']}"
        ])

        # Start sprint 3
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint3)])

        # Complete frontend
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['frontend']), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['frontend']), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['frontend']), "COMPLETED"])

        # Complete docs
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['docs']), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['docs']), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['docs']), "COMPLETED"])

        # Close sprint 3
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint3)])

        # Create Sprint 4: DevOps and Deployment
        sprint4 = self.test.create_sprint(roadmap, "Sprint 4: DevOps and Deployment")

        # Add deployment and monitoring to sprint 4
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint4),
            f"{tasks['deploy']},{tasks['monitoring']}"
        ])

        # Start sprint 4
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint4)])

        # Complete deployment
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['deploy']), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['deploy']), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['deploy']), "COMPLETED"])

        # Complete monitoring
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['monitoring']), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['monitoring']), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks['monitoring']), "COMPLETED"])

        # Close sprint 4
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint4)])

        # Verify all tasks are completed
        result = self.test.list_tasks(roadmap, status="COMPLETED")
        assert len(result) == 8, f"Expected 8 completed tasks, got {len(result)}"

        # Verify all sprints are closed
        result = self.test.list_sprints(roadmap, status="CLOSED")
        assert len(result) == 4

        print("✓ Full development cycle test passed")

    def test_sprint_with_mixed_task_progress(self):
        """Test sprint with tasks in various states of completion."""
        roadmap = self.test.create_roadmap("mixed-progress")

        # Create tasks
        task1 = self.test.create_task(roadmap, "Completed Task", "Action", "Result")
        task2 = self.test.create_task(roadmap, "In Progress Task", "Action", "Result")
        task3 = self.test.create_task(roadmap, "Testing Task", "Action", "Result")
        task4 = self.test.create_task(roadmap, "Not Started Task", "Action", "Result")

        # Create sprint and add all tasks
        sprint_id = self.test.create_sprint(roadmap, "Mixed Progress Sprint")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task1},{task2},{task3},{task4}"
        ])

        # Start sprint
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Task 1: Complete
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "COMPLETED"])

        # Task 2: In Progress (DOING)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task2), "DOING"])

        # Task 3: In Testing
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task3), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task3), "TESTING"])

        # Task 4: Still in SPRINT (not started)

        # Check sprint stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 4
        assert result["completed_tasks"] == 1
        assert result["progress_percentage"] == 25.0
        assert result["status_distribution"]["COMPLETED"] == 1
        assert result["status_distribution"]["DOING"] == 1
        assert result["status_distribution"]["TESTING"] == 1
        assert result["status_distribution"]["SPRINT"] == 1

        # Close sprint (even with incomplete tasks)
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint_id)])

        # Verify sprint is closed
        self.test.assert_sprint_status(roadmap, sprint_id, "CLOSED")

        print("✓ Sprint with mixed task progress test passed")

    def test_task_reassignment_during_sprint(self):
        """Test reassigning tasks between sprints during active development."""
        roadmap = self.test.create_roadmap("task-reassignment")

        # Create tasks
        task1 = self.test.create_task(roadmap, "Task 1", "Action", "Result")
        task2 = self.test.create_task(roadmap, "Task 2", "Action", "Result")
        task3 = self.test.create_task(roadmap, "Task 3", "Action", "Result")

        # Create two sprints
        sprint1 = self.test.create_sprint(roadmap, "Sprint 1")
        sprint2 = self.test.create_sprint(roadmap, "Sprint 2")

        # Add all tasks to sprint 1
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint1),
            f"{task1},{task2},{task3}"
        ])

        # Start sprint 1
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint1)])

        # Realize task3 should be in sprint 2
        self.test.run_cmd([
            "sprint", "move-tasks", "-r", roadmap, str(sprint1), str(sprint2), str(task3)
        ])

        # Verify task3 is now in sprint 2
        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint2)])
        assert len(result) == 1
        assert result[0]["id"] == task3

        # Verify sprint 1 has 2 tasks
        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint1)])
        assert len(result) == 2

        print("✓ Task reassignment during sprint test passed")

    def test_reopen_completed_task(self):
        """Test reopening a completed task (bug found in production)."""
        roadmap = self.test.create_roadmap("reopen-task")

        # Create and complete task
        task_id = self.test.create_task(
            roadmap, "User login feature",
            "Implement login with email and password",
            "Users can log in with valid credentials"
        )

        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])

        # Complete the task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        # Verify completed
        self.test.assert_task_status(roadmap, task_id, "COMPLETED")

        # Bug found - reopen task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "BACKLOG"])
        self.test.assert_task_status(roadmap, task_id, "BACKLOG")

        # Verify completed_at is cleared
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["completed_at"] is None

        # Re-add to sprint and fix
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])

        # Edit to reflect the fix
        self.test.run_cmd([
            "task", "edit", "-r", roadmap, str(task_id),
            "-d", "User login feature - FIXED: Handle edge case with special characters in password"
        ])

        # Complete again
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])
        self.test.assert_task_status(roadmap, task_id, "COMPLETED")

        print("✓ Reopen completed task test passed")

    def test_parallel_sprint_management(self):
        """Test managing multiple parallel sprints."""
        roadmap = self.test.create_roadmap("parallel-sprints")

        # Create multiple sprints
        sprint1 = self.test.create_sprint(roadmap, "Backend Sprint")
        sprint2 = self.test.create_sprint(roadmap, "Frontend Sprint")
        sprint3 = self.test.create_sprint(roadmap, "DevOps Sprint")

        # Create tasks for each team
        backend_tasks = [
            self.test.create_task(roadmap, f"Backend Task {i}", "Action", "Result")
            for i in range(1, 4)
        ]
        frontend_tasks = [
            self.test.create_task(roadmap, f"Frontend Task {i}", "Action", "Result")
            for i in range(1, 4)
        ]
        devops_tasks = [
            self.test.create_task(roadmap, f"DevOps Task {i}", "Action", "Result")
            for i in range(1, 4)
        ]

        # Assign tasks to respective sprints
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint1),
            ",".join(map(str, backend_tasks))
        ])
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint2),
            ",".join(map(str, frontend_tasks))
        ])
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint3),
            ",".join(map(str, devops_tasks))
        ])

        # Start all sprints
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint1)])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint2)])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint3)])

        # Verify all are open
        result = self.test.list_sprints(roadmap, status="OPEN")
        assert len(result) == 3

        # Complete backend sprint
        for task_id in backend_tasks:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint1)])

        # Frontend and DevOps still open
        result = self.test.list_sprints(roadmap, status="OPEN")
        assert len(result) == 2

        result = self.test.list_sprints(roadmap, status="CLOSED")
        assert len(result) == 1

        print("✓ Parallel sprint management test passed")

    def test_get_next_pending_task(self):
        """Test finding the next highest priority pending task."""
        roadmap = self.test.create_roadmap("next-pending")

        # Create tasks with different priorities
        low = self.test.create_task(roadmap, "Low Priority", "Action", "Result", priority=2)
        medium = self.test.create_task(roadmap, "Medium Priority", "Action", "Result", priority=5)
        high = self.test.create_task(roadmap, "High Priority", "Action", "Result", priority=8)
        critical = self.test.create_task(roadmap, "Critical", "Action", "Result", priority=9)

        # Get highest priority BACKLOG task
        result = self.test.list_tasks(roadmap, status="BACKLOG", limit=1)
        assert len(result) == 1
        assert result[0]["id"] == critical
        assert result[0]["priority"] == 9

        # Add critical to sprint
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(critical)
        ])

        # Now highest priority BACKLOG should be 'high'
        result = self.test.list_tasks(roadmap, status="BACKLOG", limit=1)
        assert len(result) == 1
        assert result[0]["id"] == high

        print("✓ Get next pending task test passed")

    def test_identify_open_vs_closed_tasks(self):
        """Test identifying open vs closed tasks across sprints."""
        roadmap = self.test.create_roadmap("open-closed-tasks")

        # Create tasks
        task1 = self.test.create_task(roadmap, "Task 1", "Action", "Result")
        task2 = self.test.create_task(roadmap, "Task 2", "Action", "Result")
        task3 = self.test.create_task(roadmap, "Task 3", "Action", "Result")
        task4 = self.test.create_task(roadmap, "Task 4", "Action", "Result")

        # Create two sprints
        sprint1 = self.test.create_sprint(roadmap, "Sprint 1")
        sprint2 = self.test.create_sprint(roadmap, "Sprint 2")

        # Add tasks to sprints
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint1),
            f"{task1},{task2}"
        ])
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint2),
            f"{task3},{task4}"
        ])

        # Complete task1
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "COMPLETED"])

        # task2 in DOING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task2), "DOING"])

        # task3 in TESTING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task3), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task3), "TESTING"])

        # task4 still in SPRINT

        # Count tasks by status
        backlog = self.test.list_tasks(roadmap, status="BACKLOG")
        sprint_status = self.test.list_tasks(roadmap, status="SPRINT")
        doing = self.test.list_tasks(roadmap, status="DOING")
        testing = self.test.list_tasks(roadmap, status="TESTING")
        completed = self.test.list_tasks(roadmap, status="COMPLETED")

        assert len(backlog) == 0  # All tasks are in sprints
        assert len(sprint_status) == 1  # task4
        assert len(doing) == 1  # task2
        assert len(testing) == 1  # task3
        assert len(completed) == 1  # task1

        # Open tasks (not completed)
        open_tasks = sprint_status + doing + testing
        assert len(open_tasks) == 3

        print("✓ Identify open vs closed tasks test passed")


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
