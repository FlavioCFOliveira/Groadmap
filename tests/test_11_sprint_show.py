#!/usr/bin/env python3
"""
Test 11: Sprint Show Command
Tests the comprehensive sprint status report command with progress calculation,
severity distribution, and criticality distribution.
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestSprintShow:
    """Test sprint show command functionality."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_show_sprint_with_valid_id(self):
        """Test showing sprint with valid sprint ID returns complete report."""
        roadmap = self.test.create_roadmap("enterprise-platform")

        # Create sprint with realistic description
        sprint_id = self.test.create_sprint(
            roadmap=roadmap,
            description="Sprint 12 - March 2026: Authentication Module Implementation"
        )

        # Start the sprint
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Get sprint show report
        result = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])

        # Verify JSON structure has all required fields
        assert "sprint_id" in result, "Missing sprint_id field"
        assert "sprint_description" in result, "Missing sprint_description field"
        assert "status" in result, "Missing status field"
        assert "summary" in result, "Missing summary field"
        assert "progress" in result, "Missing progress field"
        assert "severity_distribution" in result, "Missing severity_distribution field"
        assert "criticality_distribution" in result, "Missing criticality_distribution field"

        # Verify field values
        assert result["sprint_id"] == sprint_id, "sprint_id mismatch"
        assert result["sprint_description"] == "Sprint 12 - March 2026: Authentication Module Implementation"
        assert result["status"] == "OPEN"

        # Verify summary structure
        summary = result["summary"]
        assert "total_tasks" in summary, "Missing total_tasks in summary"
        assert "pending" in summary, "Missing pending in summary"
        assert "in_progress" in summary, "Missing in_progress in summary"
        assert "completed" in summary, "Missing completed in summary"

        # Verify progress structure
        progress = result["progress"]
        assert "pending_percentage" in progress, "Missing pending_percentage"
        assert "in_progress_percentage" in progress, "Missing in_progress_percentage"
        assert "completed_percentage" in progress, "Missing completed_percentage"

        # Percentages should sum to 100 (with floating point tolerance)
        total_pct = progress["pending_percentage"] + progress["in_progress_percentage"] + progress["completed_percentage"]

        # Verify distribution structures
        sev_dist = result["severity_distribution"]
        assert "0-2" in sev_dist, "Missing severity range 0-2"
        assert "3-5" in sev_dist, "Missing severity range 3-5"
        assert "6-7" in sev_dist, "Missing severity range 6-7"
        assert "8-9" in sev_dist, "Missing severity range 8-9"

        for key in ["0-2", "3-5", "6-7", "8-9"]:
            assert "count" in sev_dist[key], f"Missing count in severity_{key}"
            assert "percentage" in sev_dist[key], f"Missing percentage in severity_{key}"

        crit_dist = result["criticality_distribution"]
        assert "low" in crit_dist, "Missing criticality level low"
        assert "medium" in crit_dist, "Missing criticality level medium"
        assert "high" in crit_dist, "Missing criticality level high"
        assert "critical" in crit_dist, "Missing criticality level critical"

        for key in ["low", "medium", "high", "critical"]:
            assert "count" in crit_dist[key], f"Missing count in criticality_{key}"
            assert "percentage" in crit_dist[key], f"Missing percentage in criticality_{key}"

        print("✓ Show sprint with valid ID test passed")

    def test_show_empty_sprint(self):
        """Test showing sprint with no tasks."""
        roadmap = self.test.create_roadmap("analytics-dashboard")

        # Create empty sprint
        sprint_id = self.test.create_sprint(
            roadmap=roadmap,
            description="Sprint 5 - Planning Phase"
        )

        result = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])

        # Verify empty sprint data
        assert result["sprint_id"] == sprint_id
        assert result["summary"]["total_tasks"] == 0
        assert result["summary"]["pending"] == 0
        assert result["summary"]["in_progress"] == 0
        assert result["summary"]["completed"] == 0

        # Progress should all be 0.0 for empty sprint
        assert result["progress"]["pending_percentage"] == 0.0
        assert result["progress"]["in_progress_percentage"] == 0.0
        assert result["progress"]["completed_percentage"] == 0.0

        # All severity distributions should have 0 count
        for key in ["0-2", "3-5", "6-7", "8-9"]:
            assert result["severity_distribution"][key]["count"] == 0
            assert result["severity_distribution"][key]["percentage"] == 0.0

        # All criticality distributions should have 0 count
        for key in ["low", "medium", "high", "critical"]:
            assert result["criticality_distribution"][key]["count"] == 0
            assert result["criticality_distribution"][key]["percentage"] == 0.0

        print("✓ Show empty sprint test passed")

    def test_progress_calculation_mixed_statuses(self):
        """Test progress calculation with tasks in various states."""
        roadmap = self.test.create_roadmap("customer-portal-v2")

        sprint_id = self.test.create_sprint(
            roadmap=roadmap,
            description="Sprint 8 - Feature Development"
        )

        # Create 10 tasks with varying severities
        tasks = []
        # Create tasks that will be in different statuses
        for i in range(10):
            task_id = self.test.create_task(
                roadmap=roadmap,
                title=f"Feature Task {i+1}",
                functional_requirements=f"Implement feature {i+1} for user workflow",
                technical_requirements=f"Create module with proper error handling",
                acceptance_criteria=f"Feature works correctly and tests pass",
                severity=(i % 10)  # severities 0-9 distributed
            )
            tasks.append(task_id)

        # Add all tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(str(t) for t in tasks)
        ])

        # Status distribution:
        # Task 1: BACKLOG (pending)
        # Tasks 2-3: SPRINT (pending)
        # Tasks 4-5: DOING (in_progress)
        # Tasks 6-7: TESTING (in_progress)
        # Tasks 8-10: COMPLETED (completed)

        # Tasks 0-1 stay in SPRINT status (which is pending)

        # Move tasks 2-3 to DOING (indices 2,3)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[2]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[3]), "DOING"])

        # Move tasks 4-5 to DOING then TESTING (indices 4,5)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[4]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[4]), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[5]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[5]), "TESTING"])

        # Complete tasks 6-9 (indices 6,7,8,9)
        for idx in [6, 7, 8, 9]:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[idx]), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[idx]), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[idx]), "COMPLETED"])

        # Get sprint show report
        result = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])

        # Verify counts
        # Pending = SPRINT status (tasks 0-1) = 2
        # In Progress = DOING (tasks 2-3) + TESTING (tasks 4-5) = 4
        # Completed = COMPLETED (tasks 6-9) = 4
        assert result["summary"]["total_tasks"] == 10
        assert result["summary"]["pending"] == 2, f"Expected 2 pending, got {result['summary']['pending']}"
        assert result["summary"]["in_progress"] == 4, f"Expected 4 in_progress, got {result['summary']['in_progress']}"
        assert result["summary"]["completed"] == 4, f"Expected 4 completed, got {result['summary']['completed']}"

        # Verify percentages
        assert result["progress"]["pending_percentage"] == 20.0
        assert result["progress"]["in_progress_percentage"] == 40.0
        assert result["progress"]["completed_percentage"] == 40.0

        # Verify percentages sum to 100
        total = sum([
            result["progress"]["pending_percentage"],
            result["progress"]["in_progress_percentage"],
            result["progress"]["completed_percentage"]
        ])
        assert total == 100.0, f"Progress percentages sum to {total}"

        print("✓ Progress calculation with mixed statuses test passed")

    def test_severity_distribution_accuracy(self):
        """Test severity distribution calculation accuracy."""
        roadmap = self.test.create_roadmap("payment-gateway")

        sprint_id = self.test.create_sprint(
            roadmap=roadmap,
            description="Sprint 3 - Security Hardening"
        )

        # Create tasks with specific severity distribution
        # Severity 0-2 (low): 2 tasks
        # Severity 3-5 (medium): 4 tasks
        # Severity 6-7 (high): 3 tasks
        # Severity 8-9 (critical): 1 task
        severity_groups = {
            "0-2": [0, 1],
            "3-5": [3, 4, 4, 5],
            "6-7": [6, 6, 7],
            "8-9": [9]
        }

        tasks = []
        for severity in severity_groups["0-2"] + severity_groups["3-5"] + severity_groups["6-7"] + severity_groups["8-9"]:
            task_id = self.test.create_task(
                roadmap=roadmap,
                title=f"Security Task Severity {severity}",
                functional_requirements="Implement security requirement",
                technical_requirements="Technical implementation details",
                acceptance_criteria="Security validation passes",
                severity=severity
            )
            tasks.append(task_id)

        # Add to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(str(t) for t in tasks)
        ])

        result = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])

        sev_dist = result["severity_distribution"]

        # Verify counts
        assert sev_dist["0-2"]["count"] == 2, f"Expected 2 tasks in severity 0-2, got {sev_dist['0-2']['count']}"
        assert sev_dist["3-5"]["count"] == 4, f"Expected 4 tasks in severity 3-5, got {sev_dist['3-5']['count']}"
        assert sev_dist["6-7"]["count"] == 3, f"Expected 3 tasks in severity 6-7, got {sev_dist['6-7']['count']}"
        assert sev_dist["8-9"]["count"] == 1, f"Expected 1 task in severity 8-9, got {sev_dist['8-9']['count']}"

        # Verify percentages (10 total tasks)
        assert sev_dist["0-2"]["percentage"] == 20.0, f"Expected 20.0%, got {sev_dist['0-2']['percentage']}%"
        assert sev_dist["3-5"]["percentage"] == 40.0, f"Expected 40.0%, got {sev_dist['3-5']['percentage']}%"
        assert sev_dist["6-7"]["percentage"] == 30.0, f"Expected 30.0%, got {sev_dist['6-7']['percentage']}%"
        assert sev_dist["8-9"]["percentage"] == 10.0, f"Expected 10.0%, got {sev_dist['8-9']['percentage']}%"

        # Verify sum of percentages is 100
        total_pct = sum(sev_dist[k]["percentage"] for k in ["0-2", "3-5", "6-7", "8-9"])
        assert abs(total_pct - 100.0) < 0.01, f"Severity percentages sum to {total_pct}"

        print("✓ Severity distribution accuracy test passed")

    def test_criticality_distribution_accuracy(self):
        """Test criticality distribution calculation accuracy."""
        roadmap = self.test.create_roadmap("ml-pipeline-service")

        sprint_id = self.test.create_sprint(
            roadmap=roadmap,
            description="Sprint 7 - Machine Learning Pipeline"
        )

        # Create tasks mapped to criticality levels
        # low (severity 0-2): 3 tasks
        # medium (severity 3-5): 3 tasks
        # high (severity 6-7): 2 tasks
        # critical (severity 8-9): 2 tasks
        criticality_mapping = {
            "low": [0, 1, 2],
            "medium": [3, 4, 5],
            "high": [6, 7],
            "critical": [8, 9]
        }

        tasks = []
        for severity in (criticality_mapping["low"] + criticality_mapping["medium"] +
                        criticality_mapping["high"] + criticality_mapping["critical"]):
            task_id = self.test.create_task(
                roadmap=roadmap,
                title=f"ML Pipeline Component Severity {severity}",
                functional_requirements="Implement ML pipeline component",
                technical_requirements="Integration with existing infrastructure",
                acceptance_criteria="Component validated in staging environment",
                severity=severity
            )
            tasks.append(task_id)

        # Add to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(str(t) for t in tasks)
        ])

        result = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])

        crit_dist = result["criticality_distribution"]

        # Verify counts by criticality level
        assert crit_dist["low"]["count"] == 3, f"Expected 3 low criticality tasks, got {crit_dist['low']['count']}"
        assert crit_dist["medium"]["count"] == 3, f"Expected 3 medium criticality tasks, got {crit_dist['medium']['count']}"
        assert crit_dist["high"]["count"] == 2, f"Expected 2 high criticality tasks, got {crit_dist['high']['count']}"
        assert crit_dist["critical"]["count"] == 2, f"Expected 2 critical tasks, got {crit_dist['critical']['count']}"

        # Verify percentages (10 total tasks)
        assert crit_dist["low"]["percentage"] == 30.0, f"Expected 30.0% low, got {crit_dist['low']['percentage']}%"
        assert crit_dist["medium"]["percentage"] == 30.0, f"Expected 30.0% medium, got {crit_dist['medium']['percentage']}%"
        assert crit_dist["high"]["percentage"] == 20.0, f"Expected 20.0% high, got {crit_dist['high']['percentage']}%"
        assert crit_dist["critical"]["percentage"] == 20.0, f"Expected 20.0% critical, got {crit_dist['critical']['percentage']}%"

        # Verify sum of percentages is 100
        total_pct = sum(crit_dist[k]["percentage"] for k in ["low", "medium", "high", "critical"])
        assert abs(total_pct - 100.0) < 0.01, f"Criticality percentages sum to {total_pct}"

        print("✓ Criticality distribution accuracy test passed")

    def test_sprint_show_closed_sprint(self):
        """Test showing closed sprint."""
        roadmap = self.test.create_roadmap("api-gateway-redesign")

        sprint_id = self.test.create_sprint(
            roadmap=roadmap,
            description="Sprint 4 - API Gateway Migration"
        )

        # Create and add tasks
        task1 = self.test.create_task(
            roadmap, "Migrate Auth Service", "Auth migration", "Technical details", "Tests pass",
            severity=8
        )
        task2 = self.test.create_task(
            roadmap, "Update Rate Limiting", "Rate limit update", "Technical details", "Tests pass",
            severity=5
        )

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task1},{task2}"
        ])

        # Start, complete tasks, and close sprint
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Complete tasks
        for task in [task1, task2]:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task), "COMPLETED"])

        # Close sprint
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint_id)])

        result = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])

        # Verify sprint status is CLOSED
        assert result["status"] == "CLOSED", f"Expected CLOSED status, got {result['status']}"

        # Verify all tasks completed
        assert result["summary"]["completed"] == 2
        assert result["progress"]["completed_percentage"] == 100.0
        assert result["progress"]["pending_percentage"] == 0.0
        assert result["progress"]["in_progress_percentage"] == 0.0

        print("✓ Show closed sprint test passed")

    def test_sprint_show_pending_sprint(self):
        """Test showing pending sprint (not started)."""
        roadmap = self.test.create_roadmap("microservices-communication")

        sprint_id = self.test.create_sprint(
            roadmap=roadmap,
            description="Sprint 6 - Message Queue Integration"
        )

        # Add tasks but don't start sprint
        task = self.test.create_task(
            roadmap, "Configure RabbitMQ", "Message broker setup", "Technical config", "Service running",
            severity=7
        )
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task)
        ])

        result = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])

        # Verify sprint status is PENDING
        assert result["status"] == "PENDING", f"Expected PENDING status, got {result['status']}"

        # Tasks added to sprint should show as pending
        assert result["summary"]["total_tasks"] == 1
        assert result["summary"]["pending"] == 1

        print("✓ Show pending sprint test passed")

    def test_error_invalid_sprint_id(self):
        """Test error handling for non-existent sprint ID."""
        roadmap = self.test.create_roadmap("mobile-app-v3")

        # Try to show non-existent sprint
        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "show", "-r", roadmap, "99999"],
            check=False
        )

        assert exit_code == 4, f"Expected exit code 4, got {exit_code}"
        assert "not found" in stderr.lower() or "sprint" in stderr.lower()

        print("✓ Error handling for invalid sprint ID test passed")

    def test_error_missing_roadmap_flag(self):
        """Test error handling when roadmap is not specified."""
        # Try without -r flag and no default roadmap
        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "show", "1"],
            check=False
        )

        assert exit_code == 3, f"Expected exit code 3 (no roadmap), got {exit_code}"
        assert "roadmap" in stderr.lower()

        print("✓ Error handling for missing roadmap flag test passed")

    def test_error_non_numeric_sprint_id(self):
        """Test error handling for non-numeric sprint ID."""
        roadmap = self.test.create_roadmap("data-migration-service")

        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "show", "-r", roadmap, "abc"],
            check=False
        )

        assert exit_code != 0, f"Expected non-zero exit code, got {exit_code}"

        print("✓ Error handling for non-numeric sprint ID test passed")

    def test_comprehensive_sprint_report(self):
        """Test comprehensive sprint report with all task states and severities."""
        roadmap = self.test.create_roadmap("enterprise-inventory-system")

        sprint_id = self.test.create_sprint(
            roadmap=roadmap,
            description="Sprint 10 - Inventory Management Release"
        )

        # Create tasks spanning all severity ranges and status states
        # Total: 12 tasks
        task_configs = [
            # (severity, status_transition_count)
            (0, 0),   # severity 0, SPRINT (pending)
            (1, 0),   # severity 1, SPRINT (pending)
            (2, 0),   # severity 2, SPRINT (pending)
            (3, 1),   # severity 3, DOING (in_progress)
            (4, 1),   # severity 4, DOING (in_progress)
            (5, 2),   # severity 5, TESTING (in_progress)
            (6, 2),   # severity 6, TESTING (in_progress)
            (7, 3),   # severity 7, COMPLETED
            (8, 3),   # severity 8, COMPLETED
            (9, 3),   # severity 9, COMPLETED
            (5, 0),   # severity 5, SPRINT (pending - another medium)
            (7, 2),   # severity 7, TESTING (in_progress)
        ]

        tasks = []
        for severity, transitions in task_configs:
            task_id = self.test.create_task(
                roadmap=roadmap,
                title=f"Inventory Task Severity {severity} State {transitions}",
                functional_requirements="Inventory management feature implementation",
                technical_requirements="Database integration with REST API",
                acceptance_criteria="Feature tested and verified in staging",
                severity=severity
            )
            tasks.append((task_id, transitions))

        # Add all tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(str(t[0]) for t in tasks)
        ])

        # Apply status transitions
        for task_id, transitions in tasks:
            if transitions >= 1:
                self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            if transitions >= 2:
                self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
            if transitions >= 3:
                self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        result = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])

        # Verify total
        assert result["summary"]["total_tasks"] == 12

        # Pending: tasks 0-2 (severity 0,1,2 in SPRINT), task 10 (severity 5 in SPRINT) = 4
        assert result["summary"]["pending"] == 4

        # In progress: tasks 3,4 (DOING), tasks 5,6,11 (TESTING) = 5
        assert result["summary"]["in_progress"] == 5

        # Completed: tasks 7,8,9 = 3
        assert result["summary"]["completed"] == 3

        # Verify percentages (allowing for floating point variance)
        assert abs(result["progress"]["pending_percentage"] - (4/12)*100) < 0.01
        assert abs(result["progress"]["in_progress_percentage"] - (5/12)*100) < 0.01
        assert abs(result["progress"]["completed_percentage"] - (3/12)*100) < 0.01

        # Severity distribution (12 total):
        # 0-2: severities 0,1,2 = 3 tasks (25%)
        # 3-5: severities 3,4,5,5 = 4 tasks (33.33%)
        # 6-7: severities 6,7,7 = 3 tasks (25%)
        # 8-9: severities 8,9 = 2 tasks (16.67%)
        sev_dist = result["severity_distribution"]
        assert sev_dist["0-2"]["count"] == 3
        assert sev_dist["3-5"]["count"] == 4
        assert sev_dist["6-7"]["count"] == 3
        assert sev_dist["8-9"]["count"] == 2

        # Criticality distribution:
        # low (0-2): 3 tasks
        # medium (3-5): 4 tasks
        # high (6-7): 3 tasks
        # critical (8-9): 2 tasks
        crit_dist = result["criticality_distribution"]
        assert crit_dist["low"]["count"] == 3
        assert crit_dist["medium"]["count"] == 4
        assert crit_dist["high"]["count"] == 3
        assert crit_dist["critical"]["count"] == 2

        print("✓ Comprehensive sprint report test passed")

    def test_severity_boundary_values(self):
        """Test sprint show with severity boundary values (0 and 9)."""
        roadmap = self.test.create_roadmap("edge-case-service")

        sprint_id = self.test.create_sprint(
            roadmap=roadmap,
            description="Sprint 1 - Edge Case Testing"
        )

        # Create tasks with boundary severities
        task_min = self.test.create_task(
            roadmap, "Minimum Severity Task", "Functional", "Technical", "Criteria", severity=0
        )
        task_max = self.test.create_task(
            roadmap, "Maximum Severity Task", "Functional", "Technical", "Criteria", severity=9
        )

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task_min},{task_max}"
        ])

        result = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])

        sev_dist = result["severity_distribution"]
        crit_dist = result["criticality_distribution"]

        # Severity 0 goes to 0-2 range (low criticality)
        assert sev_dist["0-2"]["count"] == 1
        assert crit_dist["low"]["count"] == 1

        # Severity 9 goes to 8-9 range (critical criticality)
        assert sev_dist["8-9"]["count"] == 1
        assert crit_dist["critical"]["count"] == 1

        # Each should be 50%
        assert sev_dist["0-2"]["percentage"] == 50.0
        assert sev_dist["8-9"]["percentage"] == 50.0
        assert crit_dist["low"]["percentage"] == 50.0
        assert crit_dist["critical"]["percentage"] == 50.0

        print("✓ Severity boundary values test passed")


def main():
    """Run all tests."""
    test = TestSprintShow()

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
