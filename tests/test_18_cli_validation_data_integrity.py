#!/usr/bin/env python3
"""
Test 18: CLI Validation and Data Integrity (Sprint 10)
Tests sequential sprint opening enforcement, task deletion restriction to BACKLOG,
and the dedicated task reopen command.
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestSequentialSprintOpening:
    """Task #77 — Prevent opening multiple sprints simultaneously."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_cannot_start_second_sprint_while_one_is_open(self):
        """sprint start fails with error when another sprint is OPEN."""
        roadmap = self.test.create_roadmap()

        sprint_a = self.test.create_sprint(roadmap, "Sprint Alpha — Backend Foundations")
        sprint_b = self.test.create_sprint(roadmap, "Sprint Beta — Frontend Integration")

        # Start first sprint successfully
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_a)])
        self.test.assert_sprint_status(roadmap, sprint_a, "OPEN")

        # Attempt to start second sprint while first is OPEN — must fail
        exit_code, stdout, stderr = self.test.run_cmd(
            ["sprint", "start", "-r", roadmap, str(sprint_b)],
            check=False
        )
        assert exit_code != 0, "Should not be able to start a sprint when another is OPEN"
        assert str(sprint_a) in stderr, f"Error message must include blocking sprint ID {sprint_a}"
        self.test.assert_sprint_status(roadmap, sprint_b, "PENDING")

        print("✓ Cannot start second sprint while one is OPEN")

    def test_error_message_includes_blocking_sprint_id(self):
        """Error message includes the ID of the blocking sprint."""
        roadmap = self.test.create_roadmap()

        sprint_a = self.test.create_sprint(roadmap, "Sprint One — Data Layer")
        sprint_b = self.test.create_sprint(roadmap, "Sprint Two — API Layer")

        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_a)])

        _, _, stderr = self.test.run_cmd(
            ["sprint", "start", "-r", roadmap, str(sprint_b)],
            check=False
        )
        assert str(sprint_a) in stderr, "Error must name the blocking sprint"

        print("✓ Error message includes blocking sprint ID")

    def test_start_succeeds_when_no_sprint_is_open(self):
        """sprint start succeeds when no other sprint is OPEN."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint One — Clean Slate")
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])
        self.test.assert_sprint_status(roadmap, sprint_id, "OPEN")

        print("✓ Sprint starts successfully when no other sprint is OPEN")

    def test_start_succeeds_after_blocking_sprint_is_closed(self):
        """sprint start succeeds after the blocking sprint is closed."""
        roadmap = self.test.create_roadmap()

        sprint_a = self.test.create_sprint(roadmap, "Sprint Alpha — Closed First")
        sprint_b = self.test.create_sprint(roadmap, "Sprint Beta — Opens After")

        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_a)])

        # Verify second start is blocked
        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "start", "-r", roadmap, str(sprint_b)],
            check=False
        )
        assert exit_code != 0

        # Close the first sprint
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint_a)])
        self.test.assert_sprint_status(roadmap, sprint_a, "CLOSED")

        # Now second sprint can start
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_b)])
        self.test.assert_sprint_status(roadmap, sprint_b, "OPEN")

        print("✓ Sprint starts after blocking sprint is closed")

    def test_reopen_blocked_when_another_sprint_is_open(self):
        """sprint reopen fails when another sprint is already OPEN."""
        roadmap = self.test.create_roadmap()

        sprint_a = self.test.create_sprint(roadmap, "Sprint Alpha — Will Stay Open")
        sprint_b = self.test.create_sprint(roadmap, "Sprint Beta — Close and Reopen")

        # Open sprint_a, then open and close sprint_b
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_a)])
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint_a)])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_b)])
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint_b)])

        # Open sprint_a again
        self.test.run_cmd(["sprint", "reopen", "-r", roadmap, str(sprint_a)])
        self.test.assert_sprint_status(roadmap, sprint_a, "OPEN")

        # Attempting to reopen sprint_b while sprint_a is open must fail
        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "reopen", "-r", roadmap, str(sprint_b)],
            check=False
        )
        assert exit_code != 0, "Reopen should fail when another sprint is OPEN"
        assert str(sprint_a) in stderr

        print("✓ Sprint reopen blocked when another sprint is OPEN")


class TestTaskDeletionBacklogOnly:
    """Task #78 — Restrict task deletion to BACKLOG status only."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def _create_task(self, roadmap: str) -> int:
        return self.test.create_task(
            roadmap=roadmap,
            title="Authentication Middleware Refactor",
            functional_requirements="Replace session-based auth with JWT tokens to support stateless horizontal scaling",
            technical_requirements="Update internal/auth/middleware.go to validate HS256 JWT; add token expiry check",
            acceptance_criteria="Login returns signed JWT; protected endpoints reject expired tokens; unit tests pass",
        )

    def test_remove_backlog_task_succeeds(self):
        """task remove of a BACKLOG task succeeds."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self.test.run_cmd(["task", "remove", "-r", roadmap, str(task_id)])

        # Verify task is gone (task get returns empty array when not found)
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result == [], f"Expected empty array after deletion, got: {result}"

        print("✓ BACKLOG task removed successfully")

    def test_remove_sprint_task_returns_error(self):
        """task remove of a SPRINT task returns error."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "SPRINT"])

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "remove", "-r", roadmap, str(task_id)],
            check=False
        )
        assert exit_code != 0
        assert str(task_id) in stderr
        assert "SPRINT" in stderr

        print("✓ SPRINT task cannot be removed")

    def test_remove_doing_task_returns_error(self):
        """task remove of a DOING task returns error."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "SPRINT"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "remove", "-r", roadmap, str(task_id)],
            check=False
        )
        assert exit_code != 0
        assert "DOING" in stderr

        print("✓ DOING task cannot be removed")

    def test_remove_testing_task_returns_error(self):
        """task remove of a TESTING task returns error."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "SPRINT"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "remove", "-r", roadmap, str(task_id)],
            check=False
        )
        assert exit_code != 0
        assert "TESTING" in stderr

        print("✓ TESTING task cannot be removed")

    def test_remove_completed_task_returns_error(self):
        """task remove of a COMPLETED task returns error."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "SPRINT"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "remove", "-r", roadmap, str(task_id)],
            check=False
        )
        assert exit_code != 0
        assert "COMPLETED" in stderr

        print("✓ COMPLETED task cannot be removed")

    def test_batch_remove_fails_if_any_task_not_backlog(self):
        """Batch removal fails entirely if any task is not in BACKLOG."""
        roadmap = self.test.create_roadmap()

        task_backlog = self._create_task(roadmap)
        task_doing = self._create_task(roadmap)

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_doing), "SPRINT"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_doing), "DOING"])

        ids = f"{task_backlog},{task_doing}"
        exit_code, _, stderr = self.test.run_cmd(
            ["task", "remove", "-r", roadmap, ids],
            check=False
        )
        assert exit_code != 0
        assert str(task_doing) in stderr

        # Backlog task must still exist (whole batch rejected)
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_backlog)])
        assert len(result) == 1 and result[0]["id"] == task_backlog

        print("✓ Batch remove rejected entirely when any task is not in BACKLOG")

    def test_error_message_includes_task_id_and_status(self):
        """Error message specifies the task ID and its current status."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "SPRINT"])

        _, _, stderr = self.test.run_cmd(
            ["task", "remove", "-r", roadmap, str(task_id)],
            check=False
        )
        assert str(task_id) in stderr, "Error must include task ID"
        assert "SPRINT" in stderr, "Error must include current status"

        print("✓ Error includes task ID and status")


class TestTaskReopenCommand:
    """Task #79 — Dedicated task reopen command with bulk support."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def _create_task(self, roadmap: str, title: str = "Database Index Optimization") -> int:
        return self.test.create_task(
            roadmap=roadmap,
            title=title,
            functional_requirements="Query latency on tasks table exceeds 200ms at 10k rows; add composite index",
            technical_requirements="ALTER TABLE tasks ADD INDEX idx_status_priority (status, priority DESC)",
            acceptance_criteria="Query latency under 20ms at 10k rows; EXPLAIN shows index usage",
        )

    def _advance_to(self, roadmap: str, task_id: int, target_status: str):
        """Advance task through states up to target_status."""
        path = ["SPRINT", "DOING", "TESTING", "COMPLETED"]
        for status in path:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), status])
            if status == target_status:
                break

    def test_reopen_completed_task_goes_to_backlog(self):
        """task reopen transitions a COMPLETED task to BACKLOG."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self._advance_to(roadmap, task_id, "COMPLETED")

        self.test.run_cmd(["task", "reopen", "-r", roadmap, str(task_id)])

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["status"] == "BACKLOG"

        print("✓ COMPLETED task reopened to BACKLOG")

    def test_reopen_clears_lifecycle_timestamps(self):
        """All timestamps started_at, tested_at, closed_at are NULL after reopen."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self._advance_to(roadmap, task_id, "COMPLETED")

        self.test.run_cmd(["task", "reopen", "-r", roadmap, str(task_id)])

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        task = result[0]
        assert task["started_at"] is None, "started_at must be NULL after reopen"
        assert task["tested_at"] is None, "tested_at must be NULL after reopen"
        assert task["closed_at"] is None, "closed_at must be NULL after reopen"

        print("✓ Lifecycle timestamps cleared after reopen")

    def test_reopen_doing_task(self):
        """task reopen transitions a DOING task to BACKLOG."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self._advance_to(roadmap, task_id, "DOING")

        self.test.run_cmd(["task", "reopen", "-r", roadmap, str(task_id)])

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["status"] == "BACKLOG"

        print("✓ DOING task reopened to BACKLOG")

    def test_reopen_testing_task(self):
        """task reopen transitions a TESTING task to BACKLOG."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self._advance_to(roadmap, task_id, "TESTING")

        self.test.run_cmd(["task", "reopen", "-r", roadmap, str(task_id)])

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["status"] == "BACKLOG"

        print("✓ TESTING task reopened to BACKLOG")

    def test_reopen_sprint_task(self):
        """task reopen transitions a SPRINT task to BACKLOG."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self._advance_to(roadmap, task_id, "SPRINT")

        self.test.run_cmd(["task", "reopen", "-r", roadmap, str(task_id)])

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["status"] == "BACKLOG"

        print("✓ SPRINT task reopened to BACKLOG")

    def test_reopen_bulk_multiple_tasks(self):
        """task reopen <id1>,<id2> reopens multiple tasks in one call."""
        roadmap = self.test.create_roadmap()
        task_a = self._create_task(roadmap, "Rate Limiter Implementation")
        task_b = self._create_task(roadmap, "Circuit Breaker Integration")

        self._advance_to(roadmap, task_a, "COMPLETED")
        self._advance_to(roadmap, task_b, "TESTING")

        ids = f"{task_a},{task_b}"
        self.test.run_cmd(["task", "reopen", "-r", roadmap, ids])

        for tid in [task_a, task_b]:
            result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(tid)])
            assert result[0]["status"] == "BACKLOG", f"Task {tid} should be BACKLOG after reopen"

        print("✓ Bulk reopen transitions multiple tasks to BACKLOG")

    def test_reopen_already_backlog_is_not_an_error(self):
        """Task already in BACKLOG returns informational message, not error."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)
        # task is already BACKLOG

        exit_code, _, _ = self.test.run_cmd(
            ["task", "reopen", "-r", roadmap, str(task_id)],
            check=False
        )
        assert exit_code == 0, "Reopening a BACKLOG task should not return error"

        print("✓ Reopening already-BACKLOG task succeeds without error")

    def test_reopen_invalid_id_fails_entire_batch(self):
        """Invalid task ID returns error for entire batch."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self._advance_to(roadmap, task_id, "COMPLETED")

        invalid_id = 999999
        ids = f"{task_id},{invalid_id}"

        exit_code, _, _ = self.test.run_cmd(
            ["task", "reopen", "-r", roadmap, ids],
            check=False
        )
        assert exit_code != 0, "Batch with invalid ID must fail"

        # Valid task must not have been modified (transaction rolled back)
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["status"] == "COMPLETED", "Task should remain COMPLETED after failed batch"

        print("✓ Invalid ID fails entire batch without modifying valid tasks")

    def test_reopen_appears_in_task_help(self):
        """rmp task --help lists reopen as a valid subcommand."""
        _, stdout, _ = self.test.run_cmd(["task", "--help"])
        assert "reopen" in stdout

        print("✓ reopen appears in task --help")

    def test_reopen_audit_log_recorded(self):
        """Audit log records each reopen operation individually."""
        roadmap = self.test.create_roadmap()
        task_a = self._create_task(roadmap, "Event Sourcing Spike")
        task_b = self._create_task(roadmap, "CQRS Pattern Evaluation")

        self._advance_to(roadmap, task_a, "COMPLETED")
        self._advance_to(roadmap, task_b, "DOING")

        ids = f"{task_a},{task_b}"
        self.test.run_cmd(["task", "reopen", "-r", roadmap, ids])

        audit = self.test.run_cmd_json(["audit", "list", "-r", roadmap])
        reopen_entries = [e for e in audit if e.get("operation") == "TASK_REOPEN"]
        reopen_ids = {e["entity_id"] for e in reopen_entries}

        assert task_a in reopen_ids, f"Audit must include TASK_REOPEN for task {task_a}"
        assert task_b in reopen_ids, f"Audit must include TASK_REOPEN for task {task_b}"

        print("✓ TASK_REOPEN audit logged for each reopened task")
