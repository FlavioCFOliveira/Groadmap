#!/usr/bin/env python3
"""
Test 05: Audit and Reporting
Tests audit log functionality and various reporting scenarios.
"""

import sys
import os
from datetime import datetime, timezone
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestAuditReporting:
    """Test audit log and reporting features."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_audit_log_created_on_task_create(self):
        """Test that audit log is created when task is created."""
        roadmap = self.test.create_roadmap()

        # Create a task
        task_id = self.test.create_task(
            roadmap, "Test task", "Action", "Result"
        )

        # Check audit log
        result = self.test.run_cmd_json(["audit", "list", "-r", roadmap])
        assert len(result) >= 1

        # Find task creation entry
        task_creates = [e for e in result if e["operation"] == "TASK_CREATE"]
        assert len(task_creates) >= 1
        assert task_creates[0]["entity_type"] == "TASK"
        assert task_creates[0]["entity_id"] == task_id

        print("✓ Audit log on task create test passed")

    def test_audit_log_tracks_task_updates(self):
        """Test that audit log tracks task updates."""
        roadmap = self.test.create_roadmap()

        # Create and modify task
        task_id = self.test.create_task(roadmap, "Task", "Action", "Result")

        self.test.run_cmd([
            "task", "edit", "-r", roadmap, str(task_id),
            "-d", "Updated description"
        ])

        self.test.run_cmd([
            "task", "prio", "-r", roadmap, str(task_id), "5"
        ])

        self.test.run_cmd([
            "task", "sev", "-r", roadmap, str(task_id), "3"
        ])

        # Check audit log
        result = self.test.run_cmd_json(["audit", "list", "-r", roadmap])

        operations = [e["operation"] for e in result]
        assert "TASK_CREATE" in operations
        assert "TASK_UPDATE" in operations
        assert "TASK_PRIORITY_CHANGE" in operations
        assert "TASK_SEVERITY_CHANGE" in operations

        print("✓ Audit log tracks task updates test passed")

    def test_audit_log_tracks_status_changes(self):
        """Test that audit log tracks task status changes."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(roadmap, "Task", "Action", "Result")
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")

        # Add to sprint (changes status to SPRINT)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])

        # Change status
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        # Check audit log
        result = self.test.run_cmd_json(["audit", "list", "-r", roadmap])

        status_changes = [e for e in result if e["operation"] == "TASK_STATUS_CHANGE"]
        assert len(status_changes) >= 3  # At least DOING, TESTING, COMPLETED

        print("✓ Audit log tracks status changes test passed")

    def test_audit_log_tracks_sprint_operations(self):
        """Test that audit log tracks sprint operations."""
        roadmap = self.test.create_roadmap()

        # Create sprint
        sprint_id = self.test.create_sprint(roadmap, "Test Sprint")

        # Lifecycle operations
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint_id)])
        self.test.run_cmd(["sprint", "reopen", "-r", roadmap, str(sprint_id)])

        # Check audit log
        result = self.test.run_cmd_json(["audit", "list", "-r", roadmap])

        operations = [e["operation"] for e in result]
        assert "SPRINT_CREATE" in operations
        assert "SPRINT_START" in operations
        assert "SPRINT_CLOSE" in operations
        assert "SPRINT_REOPEN" in operations

        print("✓ Audit log tracks sprint operations test passed")

    def test_audit_log_filters(self):
        """Test audit log filtering options."""
        roadmap = self.test.create_roadmap()

        # Create multiple entities
        task1 = self.test.create_task(roadmap, "Task 1", "Action", "Result")
        task2 = self.test.create_task(roadmap, "Task 2", "Action", "Result")
        sprint_id = self.test.create_sprint(roadmap, "Sprint")

        # Filter by operation
        result = self.test.run_cmd_json([
            "audit", "list", "-r", roadmap,
            "-o", "TASK_CREATE"
        ])
        assert all(e["operation"] == "TASK_CREATE" for e in result)

        # Filter by entity type
        result = self.test.run_cmd_json([
            "audit", "list", "-r", roadmap,
            "-e", "SPRINT"
        ])
        assert all(e["entity_type"] == "SPRINT" for e in result)

        # Filter by entity ID
        result = self.test.run_cmd_json([
            "audit", "list", "-r", roadmap,
            "--entity-id", str(task1)
        ])
        assert all(e["entity_id"] == task1 for e in result)

        print("✓ Audit log filters test passed")

    def test_entity_history(self):
        """Test entity history command."""
        roadmap = self.test.create_roadmap()

        # Create and modify task
        task_id = self.test.create_task(roadmap, "Task", "Action", "Result")
        self.test.run_cmd(["task", "prio", "-r", roadmap, str(task_id), "5"])
        self.test.run_cmd(["task", "sev", "-r", roadmap, str(task_id), "3"])

        # Get task history
        result = self.test.run_cmd_json([
            "audit", "history", "-r", roadmap, "TASK", str(task_id)
        ])
        assert len(result) >= 3  # CREATE, PRIORITY_CHANGE, SEVERITY_CHANGE

        # Create and modify sprint
        sprint_id = self.test.create_sprint(roadmap, "Sprint")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Get sprint history
        result = self.test.run_cmd_json([
            "audit", "history", "-r", roadmap, "SPRINT", str(sprint_id)
        ])
        assert len(result) >= 2  # CREATE, START

        print("✓ Entity history test passed")

    def test_audit_stats(self):
        """Test audit statistics."""
        roadmap = self.test.create_roadmap()

        # Create some activity
        task_id = self.test.create_task(roadmap, "Task", "Action", "Result")
        self.test.run_cmd(["task", "prio", "-r", roadmap, str(task_id), "5"])
        sprint_id = self.test.create_sprint(roadmap, "Sprint")

        # Get stats
        result = self.test.run_cmd_json(["audit", "stats", "-r", roadmap])

        assert "total_entries" in result
        assert "by_operation" in result
        assert result["total_entries"] >= 3

        print("✓ Audit stats test passed")

    def test_audit_date_range_filter(self):
        """Test audit log date range filtering."""
        roadmap = self.test.create_roadmap()

        # Create task
        task_id = self.test.create_task(roadmap, "Task", "Action", "Result")

        # Get current time in ISO format
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "Z"

        # Filter since now
        result = self.test.run_cmd_json([
            "audit", "list", "-r", roadmap,
            "--since", "2020-01-01T00:00:00.000Z"
        ])
        assert len(result) >= 1

        # Filter until now
        result = self.test.run_cmd_json([
            "audit", "list", "-r", roadmap,
            "--until", now
        ])
        assert len(result) >= 1

        print("✓ Audit date range filter test passed")

    def test_task_priority_and_severity_changes(self):
        """Test changing task priority and severity."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(
            roadmap, "Task", "Action", "Result", priority=0, severity=0
        )

        # Change priority
        for priority in range(1, 10):
            self.test.run_cmd([
                "task", "prio", "-r", roadmap, str(task_id), str(priority)
            ])
            result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
            assert result[0]["priority"] == priority

        # Change severity
        for severity in range(1, 10):
            self.test.run_cmd([
                "task", "sev", "-r", roadmap, str(task_id), str(severity)
            ])
            result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
            assert result[0]["severity"] == severity

        print("✓ Task priority and severity changes test passed")

    def test_list_tasks_by_priority(self):
        """Test listing tasks filtered by priority."""
        roadmap = self.test.create_roadmap()

        # Create tasks with different priorities
        low = self.test.create_task(roadmap, "Low", "Action", "Result", priority=2)
        medium = self.test.create_task(roadmap, "Medium", "Action", "Result", priority=5)
        high = self.test.create_task(roadmap, "High", "Action", "Result", priority=8)
        critical = self.test.create_task(roadmap, "Critical", "Action", "Result", priority=9)

        # Filter by priority
        result = self.test.list_tasks(roadmap, priority=5)
        ids = [t["id"] for t in result]
        assert medium in ids
        assert high in ids
        assert critical in ids
        assert low not in ids

        result = self.test.list_tasks(roadmap, priority=8)
        ids = [t["id"] for t in result]
        assert high in ids
        assert critical in ids
        assert low not in ids
        assert medium not in ids

        print("✓ List tasks by priority test passed")

    def test_list_tasks_by_severity(self):
        """Test listing tasks filtered by severity."""
        roadmap = self.test.create_roadmap()

        # Create tasks with different severities
        low = self.test.create_task(roadmap, "Low", "Action", "Result", severity=1)
        medium = self.test.create_task(roadmap, "Medium", "Action", "Result", severity=4)
        high = self.test.create_task(roadmap, "High", "Action", "Result", severity=7)
        critical = self.test.create_task(roadmap, "Critical", "Action", "Result", severity=9)

        # Filter by severity
        result = self.test.list_tasks(roadmap, severity=4)
        ids = [t["id"] for t in result]
        assert medium in ids
        assert high in ids
        assert critical in ids
        assert low not in ids

        print("✓ List tasks by severity test passed")


def main():
    """Run all tests."""
    test = TestAuditReporting()

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
