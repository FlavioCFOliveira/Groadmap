#!/usr/bin/env python3
"""
Test 12: Sprint Statistics
Tests sprint statistics calculation with various task distributions.
Covers empty sprints, single status distributions, mixed statuses,
and validates percentage calculations and task ordering.
"""

import sys
import os
import json
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestSprintStats:
    """Test sprint statistics functionality."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_stats_for_empty_sprint(self):
        """Test statistics for a sprint with no tasks."""
        roadmap = self.test.create_roadmap("infrastructure_platform")

        # Create empty sprint
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1 - Initial Setup")

        # Get stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])

        # Verify empty sprint stats
        assert result["sprint_id"] == sprint_id, f"Expected sprint_id {sprint_id}, got {result['sprint_id']}"
        assert result["total_tasks"] == 0, f"Expected total_tasks 0, got {result['total_tasks']}"
        assert result["completed_tasks"] == 0, f"Expected completed_tasks 0, got {result['completed_tasks']}"
        assert result["progress_percentage"] == 0.0, f"Expected progress_percentage 0.0, got {result['progress_percentage']}"
        assert result["status_distribution"] == {}, f"Expected empty status_distribution, got {result['status_distribution']}"
        assert result["task_order"] == [], f"Expected empty task_order, got {result['task_order']}"

        print("✓ Stats for empty sprint test passed")

    def test_stats_for_sprint_with_single_status(self):
        """Test statistics for a sprint where all tasks are in a single status."""
        roadmap = self.test.create_roadmap("api_development")

        # Create sprint
        sprint_id = self.test.create_sprint(roadmap, "Sprint 2 - API Development")

        # Create tasks and add to sprint (all start in SPRINT status)
        task1 = self.test.create_task(
            roadmap, "Implement REST endpoints",
            "Users need API endpoints to access data",
            "Create REST controllers with proper routing",
            "All endpoints return valid JSON responses"
        )
        task2 = self.test.create_task(
            roadmap, "Add authentication middleware",
            "Secure API endpoints from unauthorized access",
            "Implement JWT validation middleware",
            "Requests without valid tokens are rejected"
        )
        task3 = self.test.create_task(
            roadmap, "Create rate limiting",
            "Prevent API abuse and ensure fair usage",
            "Implement token bucket algorithm",
            "Rate limits enforced per API key"
        )

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task1},{task2},{task3}"
        ])

        # Get stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])

        # Verify stats
        assert result["sprint_id"] == sprint_id
        assert result["total_tasks"] == 3
        assert result["completed_tasks"] == 0
        assert result["progress_percentage"] == 0.0
        assert result["status_distribution"]["SPRINT"] == 3
        assert len(result["status_distribution"]) == 1
        assert sorted(result["task_order"]) == sorted([task1, task2, task3])

        print("✓ Stats for sprint with single status test passed")

    def test_stats_for_sprint_with_mixed_statuses(self):
        """Test statistics for a sprint with tasks in various statuses."""
        roadmap = self.test.create_roadmap("frontend_integration")

        # Create sprint
        sprint_id = self.test.create_sprint(roadmap, "Sprint 3 - Frontend Integration")

        # Create tasks with different priorities
        task_sprint = self.test.create_task(
            roadmap, "Design component library",
            "Establish consistent UI components",
            "Create reusable React components",
            "Components follow design system guidelines"
        )
        task_doing = self.test.create_task(
            roadmap, "Implement user dashboard",
            "Users need an overview of their data",
            "Build dashboard with data visualization",
            "Dashboard displays real-time metrics"
        )
        task_testing = self.test.create_task(
            roadmap, "Add form validation",
            "Prevent invalid data submission",
            "Implement client-side validation logic",
            "Forms validate before submission"
        )
        task_completed = self.test.create_task(
            roadmap, "Setup routing",
            "Enable navigation between pages",
            "Configure React Router",
            "All routes resolve correctly"
        )

        # Add all tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task_sprint},{task_doing},{task_testing},{task_completed}"
        ])

        # Move tasks to different statuses
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_doing), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_testing), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_testing), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_completed), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_completed), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_completed), "COMPLETED"])

        # Get stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])

        # Verify stats
        assert result["sprint_id"] == sprint_id
        assert result["total_tasks"] == 4
        assert result["completed_tasks"] == 1
        assert result["progress_percentage"] == 25.0

        # Verify status distribution
        status_dist = result["status_distribution"]
        assert status_dist["SPRINT"] == 1, f"Expected 1 SPRINT task, got {status_dist.get('SPRINT', 0)}"
        assert status_dist["DOING"] == 1, f"Expected 1 DOING task, got {status_dist.get('DOING', 0)}"
        assert status_dist["TESTING"] == 1, f"Expected 1 TESTING task, got {status_dist.get('TESTING', 0)}"
        assert status_dist["COMPLETED"] == 1, f"Expected 1 COMPLETED task, got {status_dist.get('COMPLETED', 0)}"

        print("✓ Stats for sprint with mixed statuses test passed")

    def test_percentage_calculations(self):
        """Test that percentage calculations are accurate."""
        roadmap = self.test.create_roadmap("data_pipeline")

        sprint_id = self.test.create_sprint(roadmap, "Sprint 4 - Data Pipeline")

        # Create 10 tasks
        tasks = []
        for i in range(10):
            task = self.test.create_task(
                roadmap, f"Pipeline task {i+1}",
                f"Data processing requirement {i+1}",
                f"Implementation details for task {i+1}",
                f"Verification criteria for task {i+1}"
            )
            tasks.append(task)

        # Add all tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(str(t) for t in tasks)
        ])

        # Complete 3 tasks
        for i in range(3):
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "COMPLETED"])

        # Get stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])

        # Verify calculations
        assert result["total_tasks"] == 10
        assert result["completed_tasks"] == 3
        assert result["progress_percentage"] == 30.0

        # Complete 2 more tasks
        for i in range(3, 5):
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "COMPLETED"])

        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["progress_percentage"] == 50.0

        # Complete remaining tasks
        for i in range(5, 10):
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "COMPLETED"])

        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["progress_percentage"] == 100.0
        assert result["completed_tasks"] == 10

        print("✓ Percentage calculations test passed")

    def test_status_distribution_percentages_sum_to_100(self):
        """Test that status distribution counts sum to total tasks."""
        roadmap = self.test.create_roadmap("microservices_migration")

        sprint_id = self.test.create_sprint(roadmap, "Sprint 5 - Microservices Migration")

        # Create tasks
        tasks = []
        for i in range(8):
            task = self.test.create_task(
                roadmap, f"Service migration {i+1}",
                f"Migrate service {i+1} to microservice architecture",
                f"Containerize and deploy service {i+1}",
                f"Service {i+1} passes integration tests"
            )
            tasks.append(task)

        # Add to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(str(t) for t in tasks)
        ])

        # Distribute tasks across statuses
        # SPRINT: 2 tasks
        # DOING: 2 tasks
        # TESTING: 2 tasks
        # COMPLETED: 2 tasks

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[2]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[3]), "DOING"])

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[4]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[4]), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[5]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[5]), "TESTING"])

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[6]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[6]), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[6]), "COMPLETED"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[7]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[7]), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[7]), "COMPLETED"])

        # Get stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])

        # Verify distribution sums to total
        status_dist = result["status_distribution"]
        total_in_distribution = sum(status_dist.values())
        assert total_in_distribution == result["total_tasks"], (
            f"Status distribution sum ({total_in_distribution}) does not match "
            f"total_tasks ({result['total_tasks']})"
        )

        # Verify each status count
        assert status_dist.get("SPRINT", 0) == 2
        assert status_dist.get("DOING", 0) == 2
        assert status_dist.get("TESTING", 0) == 2
        assert status_dist.get("COMPLETED", 0) == 2

        print("✓ Status distribution percentages sum test passed")

    def test_stats_with_priority_distribution(self):
        """Test sprint stats with tasks of varying priorities."""
        roadmap = self.test.create_roadmap("security_audit")

        sprint_id = self.test.create_sprint(roadmap, "Sprint 6 - Security Audit")

        # Create tasks with different priorities
        critical_task = self.test.create_task(
            roadmap, "Fix authentication bypass vulnerability",
            "Critical security flaw allows unauthorized access",
            "Patch authentication logic to validate all tokens",
            "Unauthorized requests are rejected with 401",
            priority=9, severity=9
        )
        high_task = self.test.create_task(
            roadmap, "Implement audit logging",
            "Track all sensitive operations for compliance",
            "Add structured logging to security events",
            "All authentication events are logged",
            priority=7, severity=5
        )
        medium_task = self.test.create_task(
            roadmap, "Update SSL certificates",
            "Certificates expire in 30 days",
            "Generate and deploy new certificates",
            "HTTPS connections use valid certificates",
            priority=5, severity=3
        )
        low_task = self.test.create_task(
            roadmap, "Review documentation",
            "Ensure security procedures are documented",
            "Update security runbooks",
            "Documentation matches current implementation",
            priority=2, severity=1
        )

        # Add to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{critical_task},{high_task},{medium_task},{low_task}"
        ])

        # Get stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])

        # Verify basic stats
        assert result["total_tasks"] == 4
        assert result["completed_tasks"] == 0
        assert result["progress_percentage"] == 0.0

        # Progress the high priority task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(critical_task), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(critical_task), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(critical_task), "COMPLETED"])

        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["completed_tasks"] == 1
        assert result["progress_percentage"] == 25.0

        print("✓ Stats with priority distribution test passed")

    def test_stats_with_severity_distribution(self):
        """Test sprint stats with tasks of varying severities."""
        roadmap = self.test.create_roadmap("bug_fix_release")

        sprint_id = self.test.create_sprint(roadmap, "Sprint 7 - Bug Fix Release")

        # Create tasks with different severities (all bugs)
        critical_bug = self.test.create_task(
            roadmap, "Fix database corruption issue",
            "Data loss occurs during high load",
            "Add transaction retry logic",
            "No data loss under concurrent writes",
            priority=9, severity=9
        )
        high_bug = self.test.create_task(
            roadmap, "Resolve memory leak",
            "Application memory grows indefinitely",
            "Implement proper resource cleanup",
            "Memory usage remains stable over time",
            priority=8, severity=7
        )
        medium_bug = self.test.create_task(
            roadmap, "Fix UI alignment issues",
            "Components overlap on small screens",
            "Update responsive CSS rules",
            "Layout correct on all screen sizes",
            priority=5, severity=5
        )
        low_bug = self.test.create_task(
            roadmap, "Update error messages",
            "Error messages are not user-friendly",
            "Improve error message copy",
            "Error messages are clear and actionable",
            priority=3, severity=2
        )

        # Add to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{critical_bug},{high_bug},{medium_bug},{low_bug}"
        ])

        # Complete critical and medium bugs
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(critical_bug), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(critical_bug), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(critical_bug), "COMPLETED"])

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(medium_bug), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(medium_bug), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(medium_bug), "COMPLETED"])

        # Get stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])

        # Verify stats
        assert result["total_tasks"] == 4
        assert result["completed_tasks"] == 2
        assert result["progress_percentage"] == 50.0

        # Verify status distribution
        status_dist = result["status_distribution"]
        assert status_dist["COMPLETED"] == 2
        assert status_dist["SPRINT"] == 2

        print("✓ Stats with severity distribution test passed")

    def test_task_order_field_validation(self):
        """Test that task_order field accurately reflects sprint task ordering."""
        roadmap = self.test.create_roadmap("mobile_app_development")

        sprint_id = self.test.create_sprint(roadmap, "Sprint 8 - Mobile Features")

        # Create tasks in specific order
        task_auth = self.test.create_task(
            roadmap, "Implement biometric authentication",
            "Users want secure biometric login",
            "Integrate Face ID and fingerprint APIs",
            "Biometric authentication works on iOS and Android"
        )
        task_sync = self.test.create_task(
            roadmap, "Add offline data synchronization",
            "Users need access to data without internet",
            "Implement local database with sync queue",
            "Data syncs automatically when online"
        )
        task_push = self.test.create_task(
            roadmap, "Configure push notifications",
            "Users need real-time updates",
            "Set up Firebase Cloud Messaging",
            "Notifications received on all platforms"
        )
        task_analytics = self.test.create_task(
            roadmap, "Add analytics tracking",
            "Product team needs usage metrics",
            "Integrate analytics SDK",
            "Events tracked for all user actions"
        )

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task_auth},{task_sync},{task_push},{task_analytics}"
        ])

        # Get initial task order
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        initial_order = result["task_order"]

        # Verify initial order matches insertion order
        assert len(initial_order) == 4
        assert initial_order[0] == task_auth
        assert initial_order[1] == task_sync
        assert initial_order[2] == task_push
        assert initial_order[3] == task_analytics

        # Reorder tasks: move analytics to top
        self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id),
            f"{task_analytics},{task_auth},{task_sync},{task_push}"
        ])

        # Get updated task order
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        reordered = result["task_order"]

        # Verify new order
        assert reordered[0] == task_analytics
        assert reordered[1] == task_auth
        assert reordered[2] == task_sync
        assert reordered[3] == task_push

        # Move sync to bottom
        self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id), str(task_sync), "3"
        ])

        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        after_move = result["task_order"]

        # Sync should now be at position 3 (last)
        assert after_move[3] == task_sync
        assert len(after_move) == 4

        print("✓ Task order field validation test passed")

    def test_stats_exit_codes(self):
        """Test that sprint stats returns appropriate exit codes."""
        roadmap = self.test.create_roadmap("platform_migration")

        # Create a sprint
        sprint_id = self.test.create_sprint(roadmap, "Sprint 9 - Platform Migration")

        # Valid request should succeed
        exit_code, stdout, stderr = self.test.run_cmd(
            ["sprint", "stats", "-r", roadmap, str(sprint_id)],
            check=False
        )
        assert exit_code == 0, f"Valid stats request failed with exit code {exit_code}: {stderr}"

        # Invalid sprint ID returns empty stats (not an error)
        exit_code, stdout, stderr = self.test.run_cmd(
            ["sprint", "stats", "-r", roadmap, "99999"],
            check=False
        )
        assert exit_code == 0, f"Expected exit code 0 for non-existent sprint, got {exit_code}"
        result = json.loads(stdout)
        assert result["total_tasks"] == 0, "Expected 0 tasks for non-existent sprint"

        # Missing sprint ID should fail
        exit_code, stdout, stderr = self.test.run_cmd(
            ["sprint", "stats", "-r", roadmap],
            check=False
        )
        assert exit_code == 2, f"Expected exit code 2 for missing sprint ID, got {exit_code}"

        # Invalid sprint ID format should fail
        exit_code, stdout, stderr = self.test.run_cmd(
            ["sprint", "stats", "-r", roadmap, "not_a_number"],
            check=False
        )
        assert exit_code == 6, f"Expected exit code 6 for invalid sprint ID, got {exit_code}"

        # Missing roadmap should fail
        exit_code, stdout, stderr = self.test.run_cmd(
            ["sprint", "stats", str(sprint_id)],
            check=False
        )
        assert exit_code == 1, f"Expected exit code 1 for missing roadmap, got {exit_code}"

        print("✓ Stats exit codes test passed")

    def test_stats_after_removing_tasks(self):
        """Test that stats update correctly when tasks are removed from sprint."""
        roadmap = self.test.create_roadmap("feature_rollout")

        sprint_id = self.test.create_sprint(roadmap, "Sprint 10 - Feature Rollout")

        # Create and add tasks
        task1 = self.test.create_task(
            roadmap, "Enable feature flag",
            "Gradually roll out new feature",
            "Implement percentage-based rollout",
            "Feature enabled for target user percentage"
        )
        task2 = self.test.create_task(
            roadmap, "Monitor error rates",
            "Track errors during rollout",
            "Configure alerting thresholds",
            "Alerts trigger on elevated error rates"
        )
        task3 = self.test.create_task(
            roadmap, "Collect user feedback",
            "Gather feedback from beta users",
            "Implement in-app feedback form",
            "Feedback stored in analytics database"
        )

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task1},{task2},{task3}"
        ])

        # Complete one task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "COMPLETED"])

        # Get initial stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 3
        assert result["completed_tasks"] == 1

        # Remove task2 from sprint
        self.test.run_cmd([
            "sprint", "remove-tasks", "-r", roadmap, str(sprint_id), str(task2)
        ])

        # Get updated stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 2
        assert result["completed_tasks"] == 1
        assert result["progress_percentage"] == 50.0

        # Remove remaining incomplete task
        self.test.run_cmd([
            "sprint", "remove-tasks", "-r", roadmap, str(sprint_id), str(task3)
        ])

        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 1
        assert result["completed_tasks"] == 1
        assert result["progress_percentage"] == 100.0

        print("✓ Stats after removing tasks test passed")

    def test_stats_for_large_sprint(self):
        """Test statistics calculation for a sprint with many tasks."""
        roadmap = self.test.create_roadmap("enterprise_deployment")

        sprint_id = self.test.create_sprint(roadmap, "Sprint 11 - Enterprise Deployment")

        # Create 25 tasks
        tasks = []
        for i in range(25):
            task = self.test.create_task(
                roadmap, f"Enterprise task {i+1}",
                f"Requirement for enterprise feature {i+1}",
                f"Technical implementation for feature {i+1}",
                f"Acceptance criteria for feature {i+1}",
                priority=i % 10,
                severity=(24 - i) % 10
            )
            tasks.append(task)

        # Add all tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(str(t) for t in tasks)
        ])

        # Complete 10 tasks (40%)
        for i in range(10):
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "COMPLETED"])

        # Move 5 tasks to DOING (20%)
        for i in range(10, 15):
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "DOING"])

        # Move 5 tasks to TESTING (20%)
        for i in range(15, 20):
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[i]), "TESTING"])

        # Get stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])

        # Verify large sprint stats
        assert result["total_tasks"] == 25
        assert result["completed_tasks"] == 10
        assert result["progress_percentage"] == 40.0

        # Verify status distribution
        status_dist = result["status_distribution"]
        assert status_dist["COMPLETED"] == 10
        assert status_dist["DOING"] == 5
        assert status_dist["TESTING"] == 5
        assert status_dist["SPRINT"] == 5

        # Verify all tasks are accounted for
        total_in_dist = sum(status_dist.values())
        assert total_in_dist == 25

        # Verify task order has all 25 tasks
        assert len(result["task_order"]) == 25

        print("✓ Stats for large sprint test passed")

    def test_stats_with_all_tasks_completed(self):
        """Test statistics when all sprint tasks are completed."""
        roadmap = self.test.create_roadmap("project_completion")

        sprint_id = self.test.create_sprint(roadmap, "Sprint 12 - Project Completion")

        # Create tasks
        tasks = []
        for i in range(5):
            task = self.test.create_task(
                roadmap, f"Final task {i+1}",
                f"Final requirement {i+1}",
                f"Final implementation {i+1}",
                f"Final acceptance criteria {i+1}"
            )
            tasks.append(task)

        # Add to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(str(t) for t in tasks)
        ])

        # Complete all tasks
        for task_id in tasks:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        # Get stats
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])

        # Verify all completed
        assert result["total_tasks"] == 5
        assert result["completed_tasks"] == 5
        assert result["progress_percentage"] == 100.0
        assert result["status_distribution"]["COMPLETED"] == 5
        assert len(result["status_distribution"]) == 1  # Only COMPLETED status

        print("✓ Stats with all tasks completed test passed")

    def test_stats_task_count_accuracy(self):
        """Test that task counts are accurate after multiple operations."""
        roadmap = self.test.create_roadmap("continuous_integration")

        sprint_id = self.test.create_sprint(roadmap, "Sprint 13 - CI Pipeline")

        # Create tasks
        task1 = self.test.create_task(
            roadmap, "Setup build server",
            "Automate build process",
            "Configure Jenkins instance",
            "Build triggered on every commit"
        )
        task2 = self.test.create_task(
            roadmap, "Add unit tests",
            "Ensure code quality",
            "Write comprehensive test suite",
            "All critical paths covered"
        )
        task3 = self.test.create_task(
            roadmap, "Configure deployment",
            "Automate production deployments",
            "Setup blue-green deployment",
            "Zero-downtime deployments work"
        )

        # Verify stats before adding tasks
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 0

        # Add first task
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task1)
        ])
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 1

        # Add remaining tasks one by one
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task2)
        ])
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 2

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task3)
        ])
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 3

        # Move tasks through workflow and verify counts
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "DOING"])
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 3  # Total unchanged

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "TESTING"])
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 3

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "COMPLETED"])
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        assert result["total_tasks"] == 3
        assert result["completed_tasks"] == 1

        print("✓ Stats task count accuracy test passed")


def main():
    """Run all tests."""
    test = TestSprintStats()

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
