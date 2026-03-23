#!/usr/bin/env python3
"""
Test 15: Roadmap Statistics
Tests the `rmp stats` command that provides comprehensive statistics
about a roadmap, including sprint and task distribution.
Covers empty roadmaps, various task/sprint distributions, error cases,
and validates JSON output structure per SPEC/COMMANDS.md.
"""

import sys
import os
import json
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestRoadmapStats:
    """Test roadmap statistics command."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    # ==================== JSON Output Structure ====================

    def test_stats_output_has_required_fields(self):
        """Stats output must contain roadmap, sprints, and tasks fields."""
        roadmap = self.test.create_roadmap("analytics_platform")
        self.test.use_roadmap(roadmap)

        result = self.test.run_cmd_json(["stats", "-r", roadmap])

        assert "roadmap" in result, "Missing 'roadmap' field"
        assert "sprints" in result, "Missing 'sprints' field"
        assert "tasks" in result, "Missing 'tasks' field"

    def test_stats_sprints_fields(self):
        """Sprint stats must contain current, total, completed, pending."""
        roadmap = self.test.create_roadmap("backend_services")
        self.test.use_roadmap(roadmap)

        result = self.test.run_cmd_json(["stats", "-r", roadmap])
        sprints = result["sprints"]

        assert "current" in sprints, "Missing 'sprints.current' field"
        assert "total" in sprints, "Missing 'sprints.total' field"
        assert "completed" in sprints, "Missing 'sprints.completed' field"
        assert "pending" in sprints, "Missing 'sprints.pending' field"

    def test_stats_tasks_fields(self):
        """Task stats must contain backlog, sprint, doing, testing, completed."""
        roadmap = self.test.create_roadmap("frontend_dashboard")
        self.test.use_roadmap(roadmap)

        result = self.test.run_cmd_json(["stats", "-r", roadmap])
        tasks = result["tasks"]

        assert "backlog" in tasks, "Missing 'tasks.backlog' field"
        assert "sprint" in tasks, "Missing 'tasks.sprint' field"
        assert "doing" in tasks, "Missing 'tasks.doing' field"
        assert "testing" in tasks, "Missing 'tasks.testing' field"
        assert "completed" in tasks, "Missing 'tasks.completed' field"

    def test_stats_roadmap_name_matches(self):
        """The roadmap field must match the requested roadmap name."""
        roadmap = self.test.create_roadmap("inventory_system")
        self.test.use_roadmap(roadmap)

        result = self.test.run_cmd_json(["stats", "-r", roadmap])

        assert result["roadmap"] == roadmap, (
            f"Expected roadmap '{roadmap}', got '{result['roadmap']}'"
        )

    # ==================== Empty Roadmap ====================

    def test_stats_empty_roadmap(self):
        """Stats for a roadmap with no tasks or sprints should return all zeros."""
        roadmap = self.test.create_roadmap("greenfield_project")
        self.test.use_roadmap(roadmap)

        result = self.test.run_cmd_json(["stats", "-r", roadmap])

        assert result["sprints"]["current"] is None
        assert result["sprints"]["total"] == 0
        assert result["sprints"]["completed"] == 0
        assert result["sprints"]["pending"] == 0
        assert result["tasks"]["backlog"] == 0
        assert result["tasks"]["sprint"] == 0
        assert result["tasks"]["doing"] == 0
        assert result["tasks"]["testing"] == 0
        assert result["tasks"]["completed"] == 0

    # ==================== Task Distribution ====================

    def test_stats_tasks_all_backlog(self):
        """All tasks in BACKLOG status should be counted correctly."""
        roadmap = self.test.create_roadmap("migration_toolkit")
        self.test.use_roadmap(roadmap)

        for i in range(3):
            self.test.create_task(
                roadmap,
                title=f"Migrate legacy service {i + 1}",
                functional_requirements="Service must be migrated to new platform",
                technical_requirements="Use blue-green deployment strategy",
                acceptance_criteria="Zero downtime during migration"
            )

        result = self.test.run_cmd_json(["stats", "-r", roadmap])

        assert result["tasks"]["backlog"] == 3
        assert result["tasks"]["sprint"] == 0
        assert result["tasks"]["doing"] == 0
        assert result["tasks"]["testing"] == 0
        assert result["tasks"]["completed"] == 0

    def test_stats_tasks_mixed_statuses(self):
        """Tasks across multiple statuses should each be counted correctly."""
        roadmap = self.test.create_roadmap("payment_gateway")
        self.test.use_roadmap(roadmap)

        # Create tasks
        task_ids = []
        titles = [
            "Implement Stripe integration",
            "Add PayPal support",
            "Build refund workflow",
            "Create transaction ledger",
            "Setup webhook handlers",
        ]
        for title in titles:
            tid = self.test.create_task(
                roadmap,
                title=title,
                functional_requirements="Payment processing capability required",
                technical_requirements="PCI DSS compliant implementation",
                acceptance_criteria="All transactions logged with audit trail"
            )
            task_ids.append(tid)

        # Create and open a sprint, add some tasks
        sprint_id = self.test.create_sprint(roadmap, "Payment integration sprint")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap,
            str(sprint_id), str(task_ids[0]), str(task_ids[1]),
            str(task_ids[2]), str(task_ids[3])
        ])

        # After add-tasks, tasks 0-3 are in SPRINT status. Task 4 remains BACKLOG.
        # Advance tasks through the pipeline:
        # task_ids[0]: SPRINT -> DOING (stays in DOING)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[0]), "DOING"])
        # task_ids[1]: SPRINT -> DOING -> TESTING (stays in TESTING)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[1]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[1]), "TESTING"])
        # task_ids[2]: SPRINT -> DOING -> TESTING -> COMPLETED
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[2]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[2]), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[2]), "COMPLETED"])
        # task_ids[3] stays in SPRINT status

        result = self.test.run_cmd_json(["stats", "-r", roadmap])

        # task_ids[4] stays BACKLOG (not in sprint)
        assert result["tasks"]["backlog"] == 1, f"Expected 1 backlog, got {result['tasks']['backlog']}"
        # task_ids[3] in sprint but not advanced
        assert result["tasks"]["sprint"] == 1, f"Expected 1 sprint, got {result['tasks']['sprint']}"
        # task_ids[0] stays in DOING
        assert result["tasks"]["doing"] == 1, f"Expected 1 doing, got {result['tasks']['doing']}"
        # task_ids[1] moved to TESTING via DOING
        assert result["tasks"]["testing"] == 1, f"Expected 1 testing, got {result['tasks']['testing']}"
        # task_ids[2] moved to COMPLETED
        assert result["tasks"]["completed"] == 1, f"Expected 1 completed, got {result['tasks']['completed']}"

    def test_stats_task_sum_equals_total(self):
        """Sum of all task status counts must equal total number of tasks."""
        roadmap = self.test.create_roadmap("observability_stack")
        self.test.use_roadmap(roadmap)

        for i in range(7):
            self.test.create_task(
                roadmap,
                title=f"Setup monitoring component {i + 1}",
                functional_requirements="Full observability coverage required",
                technical_requirements="Prometheus and Grafana integration",
                acceptance_criteria="Alerts fire within SLA thresholds"
            )

        result = self.test.run_cmd_json(["stats", "-r", roadmap])
        tasks = result["tasks"]
        total = tasks["backlog"] + tasks["sprint"] + tasks["doing"] + tasks["testing"] + tasks["completed"]

        assert total == 7, f"Expected total 7 tasks, got {total}"

    # ==================== Sprint Distribution ====================

    def test_stats_no_sprints(self):
        """Roadmap with tasks but no sprints should show zero sprint counts."""
        roadmap = self.test.create_roadmap("documentation_hub")
        self.test.use_roadmap(roadmap)

        self.test.create_task(
            roadmap,
            title="Write API reference documentation",
            functional_requirements="Complete API docs for all endpoints",
            technical_requirements="OpenAPI 3.0 spec generation",
            acceptance_criteria="All public endpoints documented"
        )

        result = self.test.run_cmd_json(["stats", "-r", roadmap])

        assert result["sprints"]["current"] is None
        assert result["sprints"]["total"] == 0
        assert result["sprints"]["completed"] == 0
        assert result["sprints"]["pending"] == 0

    def test_stats_single_open_sprint(self):
        """A single open sprint should show as current and pending."""
        roadmap = self.test.create_roadmap("auth_service")
        self.test.use_roadmap(roadmap)

        sprint_id = self.test.create_sprint(roadmap, "Authentication overhaul sprint")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        result = self.test.run_cmd_json(["stats", "-r", roadmap])

        assert result["sprints"]["current"] == sprint_id
        assert result["sprints"]["total"] == 1
        assert result["sprints"]["completed"] == 0
        assert result["sprints"]["pending"] == 1

    def test_stats_closed_sprints(self):
        """Closed sprints should be counted in completed, not pending."""
        roadmap = self.test.create_roadmap("release_pipeline")
        self.test.use_roadmap(roadmap)

        # Create and close two sprints
        for desc in ["Pipeline setup sprint", "Pipeline hardening sprint"]:
            sid = self.test.create_sprint(roadmap, desc)
            self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sid)])
            self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sid)])

        result = self.test.run_cmd_json(["stats", "-r", roadmap])

        assert result["sprints"]["current"] is None
        assert result["sprints"]["total"] == 2
        assert result["sprints"]["completed"] == 2
        assert result["sprints"]["pending"] == 0

    def test_stats_mixed_sprints(self):
        """Mix of open and closed sprints should be counted correctly."""
        roadmap = self.test.create_roadmap("microservices_platform")
        self.test.use_roadmap(roadmap)

        # Create and close one sprint
        sid1 = self.test.create_sprint(roadmap, "Service mesh implementation")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sid1)])
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sid1)])

        # Create and open another sprint
        sid2 = self.test.create_sprint(roadmap, "API gateway configuration")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sid2)])

        result = self.test.run_cmd_json(["stats", "-r", roadmap])

        assert result["sprints"]["current"] == sid2
        assert result["sprints"]["total"] == 2
        assert result["sprints"]["completed"] == 1
        assert result["sprints"]["pending"] == 1

    # ==================== Default Roadmap ====================

    def test_stats_uses_default_roadmap(self):
        """Stats should work with default roadmap when -r is not specified."""
        roadmap = self.test.create_roadmap("default_project")
        self.test.use_roadmap(roadmap)

        self.test.create_task(
            roadmap,
            title="Configure CI pipeline",
            functional_requirements="Automated builds on every push",
            technical_requirements="GitHub Actions with caching",
            acceptance_criteria="Build completes in under 5 minutes"
        )

        result = self.test.run_cmd_json(["stats"])

        assert result["roadmap"] == roadmap
        assert result["tasks"]["backlog"] == 1

    # ==================== Error Cases ====================

    def test_stats_roadmap_not_found(self):
        """Stats for nonexistent roadmap should return exit code 4."""
        exit_code, stdout, stderr = self.test.run_cmd(
            ["stats", "-r", "nonexistent_platform"], check=False
        )

        assert exit_code == 4, f"Expected exit code 4, got {exit_code}"
        assert "not found" in stderr.lower() or "not found" in stdout.lower(), (
            f"Expected 'not found' in output, got stdout='{stdout}' stderr='{stderr}'"
        )

    def test_stats_roadmap_not_specified_no_default(self):
        """Stats without roadmap and no default should return exit code 3 (no roadmap)."""
        exit_code, stdout, stderr = self.test.run_cmd(["stats"], check=False)

        assert exit_code == 3, f"Expected exit code 3, got {exit_code}"

    # ==================== Help ====================

    def test_stats_help_flag(self):
        """Stats --help should display help text and exit 0."""
        exit_code, stdout, stderr = self.test.run_cmd(["stats", "--help"], check=False)

        assert exit_code == 0, f"Expected exit code 0, got {exit_code}"
        assert "usage" in stdout.lower() or "usage" in stderr.lower(), (
            "Help output should contain 'Usage'"
        )

    def test_stats_help_short_flag(self):
        """Stats -h should display help text and exit 0."""
        exit_code, stdout, stderr = self.test.run_cmd(["stats", "-h"], check=False)

        assert exit_code == 0, f"Expected exit code 0, got {exit_code}"

    # ==================== Large Dataset ====================

    def test_stats_with_many_tasks(self):
        """Stats should handle a roadmap with many tasks correctly."""
        roadmap = self.test.create_roadmap("enterprise_erp")
        self.test.use_roadmap(roadmap)

        task_count = 20
        for i in range(task_count):
            self.test.create_task(
                roadmap,
                title=f"Implement ERP module {i + 1}",
                functional_requirements="Module must integrate with core ERP",
                technical_requirements="RESTful API with versioning",
                acceptance_criteria="Module passes integration tests"
            )

        result = self.test.run_cmd_json(["stats", "-r", roadmap])
        tasks = result["tasks"]
        total = tasks["backlog"] + tasks["sprint"] + tasks["doing"] + tasks["testing"] + tasks["completed"]

        assert total == task_count, f"Expected {task_count} total tasks, got {total}"
        assert tasks["backlog"] == task_count


if __name__ == "__main__":
    import pytest
    pytest.main([__file__, "-v"])
