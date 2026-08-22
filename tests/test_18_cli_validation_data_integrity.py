#!/usr/bin/env python3
"""
Test 18: CLI Validation and Data Integrity (Sprint 10)
Tests sequential sprint opening enforcement, task deletion restriction to BACKLOG,
and the dedicated task reopen command.
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import subprocess

from tests.base_test import GroadmapTestBase, commit_flags_for


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

        # Verify task is gone: task get now fail-fasts with exit 4 on an unknown
        # ID (finding #44), so the removed task must no longer resolve.
        exit_code, _, stderr = self.test.run_cmd(
            ["task", "get", "-r", roadmap, str(task_id)], check=False
        )
        assert exit_code == 4, f"removed task get must exit 4, got {exit_code}: {stderr}"

        print("✓ BACKLOG task removed successfully")

    def test_remove_sprint_task_returns_error(self):
        """task remove of a SPRINT task returns error."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_task(roadmap)

        self.test.move_task_to_sprint(roadmap, task_id)

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

        self.test.move_task_to_sprint(roadmap, task_id)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING", "--commit-open", "021fa2f"])

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

        self.test.move_task_to_sprint(roadmap, task_id)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING", "--commit-open", "abd481c"])
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

        self.test.move_task_to_sprint(roadmap, task_id)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING", "--commit-open", "5d6a2cd"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED", "--commit-close", "cf507b0"])

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

        self.test.move_task_to_sprint(roadmap, task_doing)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_doing), "DOING", "--commit-open", "5f93b51"])

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

        self.test.move_task_to_sprint(roadmap, task_id)

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
        """Advance task through states up to target_status.

        SPRINT transition uses `sprint add-tasks` since manual `task stat SPRINT`
        is rejected per SPEC/STATE_MACHINE.md.
        """
        if target_status == "BACKLOG":
            return
        self.test.move_task_to_sprint(roadmap, task_id)
        if target_status == "SPRINT":
            return
        for status in ["DOING", "TESTING", "COMPLETED"]:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), status]
                              + commit_flags_for(status))
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


class TestFreeTextEmptinessAndTrimming:
    """Task #278 -- SPEC/COMMANDS.md, Emptiness Constraint (All Required
    Free-Text Fields), and SPEC/MODELS.md, Free-Text Emptiness and Trimming
    Constraint.

    The constraint has three steps, and the order between them is the whole
    point:

      1. the UTF-8 encoding rule and the control-character rule, on the value
         AS SUPPLIED;
      2. the trim;
      3. the emptiness judgement, on the TRIMMED value.

    VT (0x0B) and FF (0x0C) are forbidden control characters that
    strings.TrimSpace also removes, so trimming first would let a leading or
    trailing one through with the character silently discarded (CWE-150). The
    observable signature of the correct order, asserted below, is that a value
    made only of VT is refused as a CONTROL CHARACTER and never as empty.

    These run against the compiled binary, which is SPEC/COMMANDS.md acceptance
    criterion 10.
    """

    # Whitespace that carries no forbidden control character: every one of these
    # trims away to nothing, so every one is an emptiness refusal. The set is
    # Go's unicode.IsSpace, wider than ASCII, which is why NBSP and NEL are here
    # (acceptance criterion 7). They are written as escapes because they are
    # invisible in a source file.
    WHITESPACE_ONLY = [
        ("three spaces", "   "),
        ("a TAB", "\t"),
        ("an LF", "\n"),
        ("a CR", "\r"),
        ("a mixture", " \t\r\n "),
        ("a no-break space (U+00A0)", "\u00a0"),
        ("a NEL (U+0085)", "\u0085"),
    ]

    VT = "\v"  # 0x0B, forbidden AND whitespace
    FF = "\f"  # 0x0C, forbidden AND whitespace

    CONTROL_REFUSAL = "control characters are not allowed"

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        self.task = self.test.create_task(
            self.roadmap,
            "Reject an expired refresh token",
            "A refresh token past its exp must not mint an access token",
            "Check exp inside the refresh handler before the signature lookup",
            "A table-driven test covers the second on either side of exp",
        )
        self.sprint = self.test.create_sprint(
            self.roadmap,
            "Close the JWT boundary-second defect and lock it behind a regression test.",
            title="Expiry hardening",
        )

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers -------------------------------------------------------

    def _refused(self, args, code, message):
        """Run a command that must fail, and assert the whole contract of the
        refusal: the exit code, the message on stderr, and silent stdout."""
        exit_code, stdout, stderr = self.test.run_cmd(args, check=False)
        assert exit_code == code, (
            f"rmp {' '.join(args)!r}\n  expected exit {code}, got {exit_code}\n  stderr: {stderr!r}"
        )
        assert message in stderr, (
            f"rmp {' '.join(args)!r}\n  stderr does not carry the refusal\n"
            f"  expected substring: {message!r}\n  got: {stderr!r}"
        )
        assert stdout.strip() == "", f"a failing invocation wrote to stdout: {stdout!r}"

    def _sprint_count(self):
        return len(self.test.run_cmd_json(["sprint", "list", "-r", self.roadmap]))

    def _task_count(self):
        return len(self.test.run_cmd_json(["task", "list", "-r", self.roadmap]))

    def _audit_count(self):
        return len(self.test.run_cmd_json(["audit", "list", "-r", self.roadmap]))

    def _sprint(self, sprint_id):
        return self.test.run_cmd_json(["sprint", "get", "-r", self.roadmap, str(sprint_id)])

    def _task(self, task_id):
        result = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(task_id)])
        return result[0] if isinstance(result, list) else result

    # ---- criteria 1 and 2 ----------------------------------------------

    def test_sprint_create_refuses_a_whitespace_only_title_or_description(self):
        """Criteria 1 and 2: the defect this task was opened for.

        Before this change `rmp sprint create -t '   '` exited 0 and created a
        sprint whose title was three spaces, so every reader surface showed a
        sprint with no visible name.
        """
        before = self._sprint_count()

        self._refused(
            ["sprint", "create", "-r", self.roadmap, "-t", "   ", "-d", "A real macro goal."],
            6, "Error: validation error: title cannot be empty",
        )
        self._refused(
            ["sprint", "create", "-r", self.roadmap, "-t", "A real title", "-d", "   "],
            6, "Error: validation error: description cannot be empty",
        )

        assert self._sprint_count() == before, "a refused sprint create created a sprint"
        print("[OK] sprint create refuses a whitespace-only title and description")

    # ---- criterion 3 ---------------------------------------------------

    def test_sprint_update_refuses_whitespace_and_writes_no_audit_entry(self):
        """Criterion 3: the stored value is unchanged and no audit entry is written."""
        before = self._sprint(self.sprint)
        audit_before = self._audit_count()

        self._refused(
            ["sprint", "update", "-r", self.roadmap, str(self.sprint), "-t", "   "],
            6, "Error: validation error: title cannot be empty",
        )
        self._refused(
            ["sprint", "update", "-r", self.roadmap, str(self.sprint), "-d", "   "],
            6, "Error: validation error: description cannot be empty",
        )

        after = self._sprint(self.sprint)
        assert after["title"] == before["title"], "a refused sprint update changed the title"
        assert after["description"] == before["description"], "a refused sprint update changed the description"
        assert self._audit_count() == audit_before, "a refused sprint update wrote an audit entry"
        print("[OK] sprint update refuses whitespace, changes nothing, audits nothing")

    # ---- criterion 4 ---------------------------------------------------

    def test_task_create_names_the_field_for_whitespace_and_the_flag_when_omitted(self):
        """Criterion 4, and the third decision on this task.

        A whitespace-only value DID reach the application, so the refusal names
        the FIELD with exit 6. An omitted flag, or one carrying the literal empty
        string, never delivered a value, so it stays exit 2 naming the FLAG.
        Before this change the first case was reported as the second.
        """
        fields = [
            ("-t", "--title", "title"),
            ("-fr", "--functional-requirements", "functional_requirements"),
            ("-tr", "--technical-requirements", "technical_requirements"),
            ("-ac", "--acceptance-criteria", "acceptance_criteria"),
        ]
        valid = {
            "-t": "Reject an expired refresh token",
            "-fr": "A refresh token past its exp must not mint an access token",
            "-tr": "Check exp inside the refresh handler",
            "-ac": "A table-driven test covers both sides of exp",
        }
        before = self._task_count()

        for flag, long_flag, field in fields:
            args = ["task", "create", "-r", self.roadmap]
            for f, v in valid.items():
                args.extend([f, "   " if f == flag else v])
            self._refused(args, 6, f"Error: validation error: {field} cannot be empty")

            # The literal empty string, and the flag omitted entirely, both stay
            # exit 2 naming the flag.
            empty_args = ["task", "create", "-r", self.roadmap]
            for f, v in valid.items():
                empty_args.extend([f, "" if f == flag else v])
            self._refused(empty_args, 2, f"Error: required parameter missing: {long_flag}")

            omitted = ["task", "create", "-r", self.roadmap]
            for f, v in valid.items():
                if f != flag:
                    omitted.extend([f, v])
            self._refused(omitted, 2, f"Error: required parameter missing: {long_flag}")

        assert self._task_count() == before, "a refused task create created a task"
        print("[OK] task create names the field for whitespace and the flag when no value arrived")

    # ---- criterion 5 ---------------------------------------------------

    def test_task_edit_refuses_whitespace_only_values_naming_the_field(self):
        """Criterion 5: unchanged behaviour, pinned so it stays unchanged."""
        before = self._task(self.task)

        for flag, field in [
            ("-t", "title"),
            ("-fr", "functional_requirements"),
            ("-tr", "technical_requirements"),
            ("-ac", "acceptance_criteria"),
        ]:
            self._refused(
                ["task", "edit", "-r", self.roadmap, str(self.task), flag, "   "],
                6, f"Error: validation error: {field} cannot be empty",
            )

        after = self._task(self.task)
        for key in ("title", "functional_requirements", "technical_requirements", "acceptance_criteria"):
            assert after[key] == before[key], f"a refused task edit changed {key}"
        print("[OK] task edit refuses a whitespace-only value on all four fields")

    # ---- criterion 6 ---------------------------------------------------

    def test_comment_subcommands_refuse_a_whitespace_only_body_with_exit_two(self):
        """Criterion 6: the comment body is the one required free-text field
        whose empty refusal is NOT exit 6.

        A body that is empty once trimmed is the same condition as a body that
        never arrived, so all four subcommands report a missing parameter under
        the rule SPEC/COMMANDS.md (Comment Body Input Source and Precedence)
        already fixes, and this constraint leaves that rule alone.
        """
        task_comment = self.test.run_cmd_json([
            "task", "comment-add", "-r", self.roadmap, str(self.task),
            "--type", "FINDING", "--body", "The refresh path reuses the access-token clock.",
        ])["id"]
        sprint_comment = self.test.run_cmd_json([
            "sprint", "comment-add", "-r", self.roadmap, str(self.sprint),
            "--type", "DECISION", "--body", "The sprint closes only once the regression test is green.",
        ])["id"]

        missing = "Error: required parameter missing: no comment body supplied"
        for args in [
            ["task", "comment-add", "-r", self.roadmap, str(self.task), "--type", "FINDING", "--body", "   "],
            ["task", "comment-edit", "-r", self.roadmap, str(task_comment), "--body", "   "],
            ["sprint", "comment-add", "-r", self.roadmap, str(self.sprint), "--type", "DECISION", "--body", "   "],
            ["sprint", "comment-edit", "-r", self.roadmap, str(sprint_comment), "--body", "   "],
        ]:
            self._refused(args, 2, missing)

        # The standard-input path reaches the same verdict.
        env = dict(os.environ, HOME=str(self.test.home_dir))
        proc = subprocess.run(
            [self.test.cli_path, "task", "comment-add", "-r", self.roadmap,
             str(self.task), "--type", "FINDING"],
            input="   \t \n ", capture_output=True, text=True, env=env,
        )
        assert proc.returncode == 2, f"stdin path: expected exit 2, got {proc.returncode}: {proc.stderr!r}"
        assert missing in proc.stderr, f"stdin path stderr: {proc.stderr!r}"

        bodies = self.test.run_cmd_json(["task", "comment-list", "-r", self.roadmap, str(self.task)])
        assert len(bodies) == 1, f"a refused comment write changed the log: {bodies}"
        assert bodies[0]["body"] == "The refresh path reuses the access-token clock."
        print("[OK] all four comment subcommands refuse a whitespace-only body with exit 2")

    # ---- criterion 7 ---------------------------------------------------

    def test_every_kind_of_whitespace_is_refused_not_only_spaces(self):
        """Criterion 7: the criterion is what the trim leaves behind, not which
        whitespace character the caller supplied. TAB, LF, CR, mixtures, and the
        non-ASCII U+00A0 and U+0085 all count."""
        for label, value in self.WHITESPACE_ONLY:
            self._refused(
                ["sprint", "create", "-r", self.roadmap, "-t", value, "-d", "A real macro goal."],
                6, "Error: validation error: title cannot be empty",
            )
            self._refused(
                ["sprint", "update", "-r", self.roadmap, str(self.sprint), "-d", value],
                6, "Error: validation error: description cannot be empty",
            )
            args = ["task", "create", "-r", self.roadmap, "-t", value,
                    "-fr", "A real requirement", "-tr", "A real approach", "-ac", "A real check"]
            self._refused(args, 6, "Error: validation error: title cannot be empty")
        print(f"[OK] all {len(self.WHITESPACE_ONLY)} kinds of whitespace are refused")

    # ---- criterion 8 ---------------------------------------------------

    def test_padded_values_are_accepted_and_stored_trimmed(self):
        """Criterion 8, first half: a value with a non-empty core survives its
        padding, and what is read back is the trimmed value."""
        title = "Refresh-token guard"
        description = "Close the refresh path and lock it behind a regression test."

        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", self.roadmap,
            "-t", f"  {title}  ", "-d", f"\t{description}\n",
        ])["id"]
        stored = self._sprint(sprint_id)
        assert stored["title"] == title, f"stored title {stored['title']!r}, want {title!r}"
        assert stored["description"] == description, f"stored description {stored['description']!r}"

        self.test.run_cmd(["sprint", "update", "-r", self.roadmap, str(sprint_id), "-t", f"   {title} II   "])
        assert self._sprint(sprint_id)["title"] == f"{title} II"

        task_title = "Reject a refresh token whose exp is the current second"
        task_id = self.test.run_cmd_json([
            "task", "create", "-r", self.roadmap,
            "-t", f"  {task_title}  ", "-fr", "  A real requirement  ",
            "-tr", "  A real approach  ", "-ac", "  A real check  ",
        ])["id"]
        stored_task = self._task(task_id)
        assert stored_task["title"] == task_title, f"stored task title {stored_task['title']!r}"
        assert stored_task["functional_requirements"] == "A real requirement"
        assert stored_task["technical_requirements"] == "A real approach"
        assert stored_task["acceptance_criteria"] == "A real check"

        self.test.run_cmd(["task", "edit", "-r", self.roadmap, str(task_id), "-t", f"   {task_title} II   "])
        assert self._task(task_id)["title"] == f"{task_title} II"
        print("[OK] a padded value is accepted and read back trimmed")

    def test_the_cap_measures_the_trimmed_value(self):
        """Criterion 8, second half, and the side defect the trim closes.

        Before this change `sprint create` and `sprint update` measured the value
        AS SUPPLIED, so 255 real characters wrapped in spaces were refused there
        while `task create` accepted them -- one cap, two answers, for a value the
        column would have held either way.

        The paired negative case is what stops this being vacuous: one character
        over the maximum, unpadded, is still refused.
        """
        at_limit = "A" * 255
        over = "A" * 256

        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", self.roadmap,
            "-t", f"   {at_limit}   ", "-d", "A real macro goal.",
        ])["id"]
        assert self._sprint(sprint_id)["title"] == at_limit
        self._refused(
            ["sprint", "create", "-r", self.roadmap, "-t", over, "-d", "A real macro goal."],
            6, "title exceeds maximum length of 255 characters",
        )

        self.test.run_cmd(["sprint", "update", "-r", self.roadmap, str(sprint_id), "-t", f" {at_limit} "])
        self._refused(
            ["sprint", "update", "-r", self.roadmap, str(sprint_id), "-t", over],
            6, "title exceeds maximum length of 255 characters",
        )

        desc_at_limit = "D" * 2048
        other = self.test.run_cmd_json([
            "sprint", "create", "-r", self.roadmap,
            "-t", "Description cap probe", "-d", f"   {desc_at_limit}   ",
        ])["id"]
        assert self._sprint(other)["description"] == desc_at_limit

        task_id = self.test.run_cmd_json([
            "task", "create", "-r", self.roadmap, "-t", f"  {at_limit}  ",
            "-fr", "A real requirement", "-tr", "A real approach", "-ac", "A real check",
        ])["id"]
        assert self._task(task_id)["title"] == at_limit
        print("[OK] the cap measures the trimmed value on the sprint pair as well as the task pair")

    # ---- criterion 9 ---------------------------------------------------

    def test_a_value_of_only_VT_is_refused_as_a_control_character_not_as_empty(self):
        """Criterion 9, and the visible signature of the specified ORDER.

        VT is a forbidden control character AND whitespace, so it is refused
        either way and the exit code is 6 either way. What separates the two
        possible orders is WHICH rule answers. Asserting a non-zero exit here
        would prove nothing.
        """
        probes = [("only VT", self.VT), ("only FF", self.FF), ("VT among spaces", f"  {self.VT}  ")]

        for label, value in probes:
            for args, field in [
                (["sprint", "create", "-r", self.roadmap, "-t", value, "-d", "A real macro goal."], "title"),
                (["sprint", "create", "-r", self.roadmap, "-t", "A real title", "-d", value], "description"),
                (["sprint", "update", "-r", self.roadmap, str(self.sprint), "-t", value], "title"),
                (["sprint", "update", "-r", self.roadmap, str(self.sprint), "-d", value], "description"),
                (["task", "create", "-r", self.roadmap, "-t", value, "-fr", "A real requirement",
                  "-tr", "A real approach", "-ac", "A real check"], "title"),
                (["task", "create", "-r", self.roadmap, "-t", "A real title", "-fr", value,
                  "-tr", "A real approach", "-ac", "A real check"], "functional_requirements"),
            ]:
                exit_code, _, stderr = self.test.run_cmd(args, check=False)
                assert exit_code == 6, f"{label}: expected exit 6, got {exit_code}: {stderr!r}"
                assert self.CONTROL_REFUSAL in stderr, (
                    f"{label} on {' '.join(args[:2])} was refused as EMPTY rather than as a control "
                    f"character: the trim ran ahead of the control-character check (CWE-150)\n"
                    f"  stderr: {stderr!r}"
                )
                assert f"{field}: {self.CONTROL_REFUSAL}" in stderr, f"wrong field named: {stderr!r}"
        print("[OK] a value made only of VT or FF is refused as a control character, never as empty")

    def test_a_leading_or_trailing_VT_or_FF_is_refused_not_discarded(self):
        """Criterion 9, the half that carries the security consequence: the value
        has real content, so the trim-first order does not refuse it at all -- it
        strips the forbidden character and stores the rest with exit 0."""
        content = "Deliver the refresh-token guard"
        edges = [
            ("leading VT", self.VT + content),
            ("trailing VT", content + self.VT),
            ("leading FF", self.FF + content),
            ("trailing FF", content + self.FF),
        ]
        for label, value in edges:
            for args in [
                ["sprint", "create", "-r", self.roadmap, "-t", value, "-d", "A real macro goal."],
                ["sprint", "update", "-r", self.roadmap, str(self.sprint), "-d", value],
                ["task", "create", "-r", self.roadmap, "-t", value, "-fr", "A real requirement",
                 "-tr", "A real approach", "-ac", "A real check"],
            ]:
                exit_code, stdout, stderr = self.test.run_cmd(args, check=False)
                assert exit_code == 6, (
                    f"{label} on {' '.join(args[:2])}: expected exit 6, got {exit_code}. "
                    f"The character was discarded in silence (CWE-150). stdout: {stdout!r}"
                )
                assert self.CONTROL_REFUSAL in stderr, f"{label}: stderr {stderr!r}"
        print("[OK] a leading or trailing VT or FF is refused, not silently discarded")

    def _stdin_cmd(self, args, body):
        """Run the binary with body on standard input, so the bounded reader
        models.ReadCommentBody is the code under test rather than the flag."""
        env = dict(os.environ, HOME=str(self.test.home_dir))
        proc = subprocess.run(
            [self.test.cli_path, *args],
            input=body, capture_output=True, text=True, env=env,
        )
        return proc.returncode, proc.stdout, proc.stderr

    def _comment_body_writers(self, task_comment, sprint_comment):
        """The eight ways a comment body reaches the application: each of the
        four subcommands, on the --body path and on the standard-input path.

        The two comment-edit standard-input entries supply no --type, because
        standard input is read only when the type flag is absent (SPEC/COMMANDS.md,
        Comment Body Input Source and Precedence, rule 2).

        The third element is the missing-parameter refusal that writer emits for a
        body that trims away to nothing and carries no forbidden character. It is
        "no comment body supplied" everywhere except a comment-edit reading
        standard input, which requested no other change either.
        """
        supplied = "Error: required parameter missing: no comment body supplied"
        no_change = ("Error: required parameter missing: at least one of --type "
                     "or --body is required")
        task, sprint = str(self.task), str(self.sprint)
        return [
            ("task comment-add --body", "flag",
             ["task", "comment-add", "-r", self.roadmap, task, "--type", "FINDING"], supplied),
            ("task comment-add <stdin>", "stdin",
             ["task", "comment-add", "-r", self.roadmap, task, "--type", "FINDING"], supplied),
            ("task comment-edit --body", "flag",
             ["task", "comment-edit", "-r", self.roadmap, str(task_comment)], supplied),
            ("task comment-edit <stdin>", "stdin",
             ["task", "comment-edit", "-r", self.roadmap, str(task_comment)], no_change),
            ("sprint comment-add --body", "flag",
             ["sprint", "comment-add", "-r", self.roadmap, sprint, "--type", "DECISION"], supplied),
            ("sprint comment-add <stdin>", "stdin",
             ["sprint", "comment-add", "-r", self.roadmap, sprint, "--type", "DECISION"], supplied),
            ("sprint comment-edit --body", "flag",
             ["sprint", "comment-edit", "-r", self.roadmap, str(sprint_comment)], supplied),
            ("sprint comment-edit <stdin>", "stdin",
             ["sprint", "comment-edit", "-r", self.roadmap, str(sprint_comment)], no_change),
        ]

    def _run_comment_writer(self, origin, args, body):
        if origin == "stdin":
            return self._stdin_cmd(args, body)
        return self.test.run_cmd([*args, "--body", body], check=False)

    def _seed_comments(self):
        task_comment = self.test.run_cmd_json([
            "task", "comment-add", "-r", self.roadmap, str(self.task),
            "--type", "FINDING", "--body", "The refresh path reuses the access-token clock.",
        ])["id"]
        sprint_comment = self.test.run_cmd_json([
            "sprint", "comment-add", "-r", self.roadmap, str(self.sprint),
            "--type", "DECISION", "--body", "The sprint closes only once the regression test is green.",
        ])["id"]
        return task_comment, sprint_comment

    # ---- rmp task 301 --------------------------------------------------

    def test_task_edit_refuses_a_VT_or_FF_on_all_four_fields(self):
        """rmp task 301: `task edit` was the residual after task 278 aligned the
        other nine writers. It trimmed while building its updates map, so a
        leading or trailing VT or FF was gone before the control-character rule
        ever saw it -- the invocation exited 0 with the byte discarded in silence
        (CWE-150) -- and a value made only of VT was reported as EMPTY.

        Both halves are asserted here, on all four fields, at both edges, for both
        characters. The message is what discriminates: a control-character refusal
        and an emptiness refusal are both exit 6, so asserting the exit code alone
        would prove nothing.
        """
        content = "Deliver the refresh-token guard"
        before = self._task(self.task)

        for flag, field in [
            ("-t", "title"),
            ("-fr", "functional_requirements"),
            ("-tr", "technical_requirements"),
            ("-ac", "acceptance_criteria"),
        ]:
            # A value made only of VT or FF: refused as a CONTROL CHARACTER,
            # never as empty. This is the signature of the order.
            for label, value in [
                ("only VT", self.VT),
                ("only FF", self.FF),
                ("VT among spaces", f"  {self.VT}  "),
            ]:
                exit_code, _, stderr = self.test.run_cmd(
                    ["task", "edit", "-r", self.roadmap, str(self.task), flag, value], check=False)
                assert exit_code == 6, f"{flag} {label}: expected exit 6, got {exit_code}: {stderr!r}"
                assert f"{field}: {self.CONTROL_REFUSAL}" in stderr, (
                    f"task edit {flag} refused {label} as an EMPTY value rather than as a control "
                    f"character: the trim ran ahead of the control-character check (CWE-150)\n"
                    f"  stderr: {stderr!r}"
                )

            # A leading or trailing VT or FF in front of real content: refused,
            # not stripped and stored.
            for label, value in [
                ("leading VT", self.VT + content),
                ("trailing VT", content + self.VT),
                ("leading FF", self.FF + content),
                ("trailing FF", content + self.FF),
            ]:
                exit_code, stdout, stderr = self.test.run_cmd(
                    ["task", "edit", "-r", self.roadmap, str(self.task), flag, value], check=False)
                assert exit_code == 6, (
                    f"task edit {flag} ACCEPTED a {label}: the character was discarded in silence "
                    f"(CWE-150). exit {exit_code}, stdout {stdout!r}"
                )
                assert f"{field}: {self.CONTROL_REFUSAL}" in stderr, f"{flag} {label}: {stderr!r}"

        after = self._task(self.task)
        for key in ("title", "functional_requirements", "technical_requirements", "acceptance_criteria"):
            assert after[key] == before[key], f"a refused task edit changed {key}"
        print("[OK] task edit refuses a leading or trailing VT or FF on all four fields")

    def test_a_VT_only_comment_body_is_a_control_character_not_an_absent_body(self):
        """rmp task 301, the comment layer, on BOTH body origins.

        The comment `body` answers an absent value with exit code 2 rather than 6,
        so here the two possible orders are separated by the exit CLASS as well as
        by the message: trimming first made a body of one VT look like a body that
        never arrived (exit 2), with the forbidden character discarded in silence.
        """
        task_comment, sprint_comment = self._seed_comments()

        for label, value in [
            ("only VT", self.VT),
            ("only FF", self.FF),
            ("VT among spaces", f"  {self.VT}  "),
            ("FF among TAB and LF", f"\t{self.FF}\n"),
        ]:
            for name, origin, args, _absent in self._comment_body_writers(task_comment, sprint_comment):
                exit_code, stdout, stderr = self._run_comment_writer(origin, args, value)
                assert exit_code == 6, (
                    f"{name} with {label}: expected exit 6, got {exit_code}. A body made only of a "
                    f"forbidden control character was reported as a body that never ARRIVED, with the "
                    f"character discarded in silence (CWE-150).\n  stderr: {stderr!r}"
                )
                assert f"body: {self.CONTROL_REFUSAL}" in stderr, f"{name} with {label}: {stderr!r}"
                assert stdout.strip() == "", f"{name} wrote to stdout on a refusal: {stdout!r}"

        bodies = self.test.run_cmd_json(["task", "comment-list", "-r", self.roadmap, str(self.task)])
        assert len(bodies) == 1, f"a refused comment write changed the log: {bodies}"
        assert bodies[0]["body"] == "The refresh path reuses the access-token clock."
        print("[OK] a VT-only comment body is refused as a control character on both origins")

    def test_a_leading_or_trailing_VT_or_FF_in_a_comment_body_is_refused(self):
        """The other half for the comment layer, on both origins: the body has
        real content, so a trim-first order does not refuse it at all."""
        task_comment, sprint_comment = self._seed_comments()
        content = "The refresh path reuses the access-token clock."

        for label, value in [
            ("leading VT", self.VT + content),
            ("trailing VT", content + self.VT),
            ("leading FF", self.FF + content),
            ("trailing FF", content + self.FF),
        ]:
            for name, origin, args, _absent in self._comment_body_writers(task_comment, sprint_comment):
                exit_code, stdout, stderr = self._run_comment_writer(origin, args, value)
                assert exit_code == 6, (
                    f"{name} ACCEPTED a body carrying a {label}: exit {exit_code}, stdout {stdout!r}. "
                    f"The character was discarded in silence (CWE-150)."
                )
                assert f"body: {self.CONTROL_REFUSAL}" in stderr, f"{name} with {label}: {stderr!r}"

        bodies = self.test.run_cmd_json(["task", "comment-list", "-r", self.roadmap, str(self.task)])
        assert len(bodies) == 1 and bodies[0]["updated_at"] is None, (
            f"a refused comment write reached the table: {bodies}"
        )
        print("[OK] a leading or trailing VT or FF in a comment body is refused on both origins")

    def test_a_whitespace_only_comment_body_is_still_an_absent_body(self):
        """The guard on the two tests above: moving the emptiness judgement behind
        the content rules must refuse NOTHING new.

        Every probe here trims away to nothing and carries no forbidden character,
        so every one must still reach the missing-parameter verdict, exit 2, on
        both origins -- exactly as it did before rmp task 301.
        """
        task_comment, sprint_comment = self._seed_comments()

        for label, value in self.WHITESPACE_ONLY:
            for name, origin, args, absent in self._comment_body_writers(task_comment, sprint_comment):
                exit_code, stdout, stderr = self._run_comment_writer(origin, args, value)
                assert exit_code == 2, (
                    f"{name} with {label}: expected exit 2 (a body that never arrived), got {exit_code}."
                    f"\n  stderr: {stderr!r}"
                )
                assert absent in stderr, f"{name} with {label}\n  stderr: {stderr!r}"
                assert stdout.strip() == "", f"{name} wrote to stdout on a refusal: {stdout!r}"

        bodies = self.test.run_cmd_json(["task", "comment-list", "-r", self.roadmap, str(self.task)])
        assert len(bodies) == 1, f"a refused comment write changed the log: {bodies}"
        print("[OK] a whitespace-only comment body is still an absent body on both origins")

    def test_the_completion_summary_is_stored_trimmed_and_refuses_a_control_character(self):
        """completion_summary is the one free-text field Rule 1 does not govern
        -- it is optional -- but Rule 2 and the ORDER both apply to it.

        The order matters here for the same reason: this path used to trim the
        value at flag-extraction time, so a leading VT was gone before the
        control-character rule ever saw it and the summary was stored with the
        byte discarded and exit code 0.
        """
        summary = "Closed the boundary second and covered it with a table-driven test."

        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(self.sprint), str(self.task)])
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task), "DOING",
                           *commit_flags_for("DOING")])
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task), "TESTING"])

        self._refused(
            ["task", "stat", "-r", self.roadmap, str(self.task), "COMPLETED",
             *commit_flags_for("COMPLETED"), "--summary", self.VT + summary],
            6, f"completion_summary: {self.CONTROL_REFUSAL}",
        )

        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task), "COMPLETED",
                           *commit_flags_for("COMPLETED"), "--summary", f"   {summary}   "])
        assert self._task(self.task)["completion_summary"] == summary, (
            f"completion_summary stored as {self._task(self.task)['completion_summary']!r}"
        )
        print("[OK] completion_summary is stored trimmed and refuses a leading control character")


if __name__ == "__main__":
    import inspect
    import traceback as _tb

    _suites = [obj for name, obj in sorted(globals().items())
               if name.startswith("Test") and inspect.isclass(obj)
               and any(m.startswith("test_") for m in dir(obj))]
    _passed = 0
    _failed = 0
    _failures = []
    for _suite_class in _suites:
        for _method_name in sorted(m for m in dir(_suite_class) if m.startswith("test_")):
            _suite = _suite_class()
            if hasattr(_suite, "setup_method"):
                _suite.setup_method()
            try:
                getattr(_suite, _method_name)()
                _passed += 1
            except Exception as _exc:
                _label = f"{_suite_class.__name__}.{_method_name}"
                print(f"FAIL  {_label}: {_exc}")
                _tb.print_exc()
                _failures.append((_label, str(_exc)))
                _failed += 1
            finally:
                if hasattr(_suite, "teardown_method"):
                    _suite.teardown_method()
    _total = _passed + _failed
    print()
    print("=" * 65)
    print(f"Total: {_total} | Passed: {_passed} | Failed: {_failed}")
    if _failures:
        print("\nFailed tests:")
        for _label, _msg in _failures:
            print(f"  [X] {_label}")
            print(f"      -> {_msg}")
    print()
    print("OVERALL: PASS" if _failed == 0 else f"OVERALL: FAIL ({_failed} tests failed)")
    sys.exit(0 if _failed == 0 else 1)
