#!/usr/bin/env python3
"""
Test 20: Task #90 -- Block Add-Tasks to Closed Sprint and Warn on Close with Unstarted Tasks

Acceptance Criteria validated:
  AC-1: sprint add-tasks on a CLOSED sprint returns a non-zero exit code
  AC-2: The error message references the sprint ID and status CLOSED
  AC-3: sprint close with tasks in SPRINT status (assigned but never started) includes them
         in the error/warning output
  AC-4: sprint close --force closes the sprint despite SPRINT-status tasks, printing a warning
  AC-5: sprint add-tasks on OPEN and PENDING sprints works normally (exit code 0)
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


def _start_sprint(test, roadmap, sprint_id):
    test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])


def _close_sprint(test, roadmap, sprint_id, force=False):
    cmd = ["sprint", "close", "-r", roadmap, str(sprint_id)]
    if force:
        cmd.append("--force")
    return test.run_cmd(cmd, check=False)


def _advance_task_to_doing(test, roadmap, task_id):
    test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])


def _advance_task_to_testing(test, roadmap, task_id):
    test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])


def _advance_task_to_completed(test, roadmap, task_id):
    test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])


class TestTask90SprintClosedGuard:
    """Validate Task #90: Block add-tasks to closed sprints and warn on close with unstarted tasks."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = "qa-task90-test"
        self.test.run_cmd(["roadmap", "create", self.roadmap])

    def teardown_method(self):
        self.test.teardown()

    def _create_task(self, title):
        return self.test.create_task(
            self.roadmap,
            title,
            "Implement " + title + " according to specifications",
            "Use established patterns for " + title + "; add unit tests",
            title + " behaves as specified under all documented inputs",
        )

    def _create_sprint(self, description):
        return self.test.create_sprint(self.roadmap, description)

    # ----------------------------------------------------------------
    # AC-1: sprint add-tasks on a CLOSED sprint returns non-zero exit code
    # ----------------------------------------------------------------

    def test_ac1_add_tasks_to_closed_sprint_fails(self):
        """AC-1: sprint add-tasks on a CLOSED sprint must return a non-zero exit code."""
        task_id = self._create_task("Authentication middleware refactor")
        sprint_id = self._create_sprint("Sprint Alpha Closed Guard")
        _start_sprint(self.test, self.roadmap, sprint_id)
        ec, _, _ = _close_sprint(self.test, self.roadmap, sprint_id)
        assert ec == 0, "Expected empty sprint close to succeed with exit 0"
        self.test.assert_sprint_status(self.roadmap, sprint_id, "CLOSED")
        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)],
            check=False,
        )
        assert ec != 0, (
            "AC-1 FAIL: Expected non-zero exit code when adding task to CLOSED sprint, got exit " + str(ec)
        )
        # Task must remain BACKLOG -- operation was a no-op
        self.test.assert_task_status(self.roadmap, task_id, "BACKLOG")

    # ----------------------------------------------------------------
    # AC-2: Error message references sprint ID and "CLOSED"
    # ----------------------------------------------------------------

    def test_ac2_error_message_references_sprint_id_and_closed(self):
        """AC-2: Error must include sprint ID and the word CLOSED."""
        task_id = self._create_task("Database connection pooling enhancement")
        sprint_id = self._create_sprint("Sprint Beta Error Message Validation")
        _start_sprint(self.test, self.roadmap, sprint_id)
        _close_sprint(self.test, self.roadmap, sprint_id)
        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)],
            check=False,
        )
        assert ec != 0, "AC-2 FAIL: Command should have failed."
        combined = stdout + stderr
        assert str(sprint_id) in combined, (
            "AC-2 FAIL: Sprint ID " + str(sprint_id) + " not found in error output. stderr=" + repr(stderr)
        )
        assert "closed" in combined.lower(), (
            "AC-2 FAIL: Word CLOSED not found in error output. stderr=" + repr(stderr)
        )

    # ----------------------------------------------------------------
    # AC-3: sprint close with SPRINT-status tasks is blocked
    # ----------------------------------------------------------------

    def test_ac3_close_with_sprint_status_tasks_blocked(self):
        """AC-3: sprint close must fail when tasks are in SPRINT status."""
        task1_id = self._create_task("API rate limiter implementation")
        task2_id = self._create_task("Cache invalidation strategy")
        sprint_id = self._create_sprint("Sprint Gamma Unstarted Task Guard")
        _start_sprint(self.test, self.roadmap, sprint_id)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(sprint_id), str(task1_id) + "," + str(task2_id)
        ])
        self.test.assert_task_status(self.roadmap, task1_id, "SPRINT")
        self.test.assert_task_status(self.roadmap, task2_id, "SPRINT")
        ec, stdout, stderr = _close_sprint(self.test, self.roadmap, sprint_id)
        assert ec != 0, (
            "AC-3 FAIL: Expected non-zero exit when closing sprint with SPRINT-status tasks, got exit " + str(ec)
        )
        combined = stdout + stderr
        assert str(task1_id) in combined, (
            "AC-3 FAIL: Task ID " + str(task1_id) + " not found in error output. stderr=" + repr(stderr)
        )
        assert str(task2_id) in combined, (
            "AC-3 FAIL: Task ID " + str(task2_id) + " not found in error output. stderr=" + repr(stderr)
        )
        self.test.assert_sprint_status(self.roadmap, sprint_id, "OPEN")

    def test_ac3_close_with_doing_tasks_also_blocked(self):
        """AC-3 extended: sprint close must also fail when tasks are in DOING status."""
        task_id = self._create_task("CI/CD pipeline optimisation")
        sprint_id = self._create_sprint("Sprint Delta In-Progress Task Guard")
        _start_sprint(self.test, self.roadmap, sprint_id)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)
        ])
        _advance_task_to_doing(self.test, self.roadmap, task_id)
        self.test.assert_task_status(self.roadmap, task_id, "DOING")
        ec, stdout, stderr = _close_sprint(self.test, self.roadmap, sprint_id)
        assert ec != 0, "AC-3 extended FAIL: Expected blocking for DOING task, got exit " + str(ec)
        assert str(task_id) in (stdout + stderr), (
            "AC-3 extended FAIL: Task " + str(task_id) + " not referenced in error output."
        )

    def test_ac3_close_with_testing_tasks_also_blocked(self):
        """AC-3 extended: sprint close must also fail when tasks are in TESTING status."""
        task_id = self._create_task("Load balancer health-check endpoint")
        sprint_id = self._create_sprint("Sprint Delta2 Testing Task Guard")
        _start_sprint(self.test, self.roadmap, sprint_id)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)
        ])
        _advance_task_to_doing(self.test, self.roadmap, task_id)
        _advance_task_to_testing(self.test, self.roadmap, task_id)
        self.test.assert_task_status(self.roadmap, task_id, "TESTING")
        ec, stdout, stderr = _close_sprint(self.test, self.roadmap, sprint_id)
        assert ec != 0, "AC-3 extended FAIL: Expected blocking for TESTING task, got exit " + str(ec)
        assert str(task_id) in (stdout + stderr), (
            "AC-3 extended FAIL: Task " + str(task_id) + " not referenced in error output."
        )

    def test_ac3_close_with_mixed_sprint_and_doing_tasks(self):
        """AC-3 mixed: Sprint has one SPRINT-status task and one DOING task; both must appear in error."""
        task_unstarted = self._create_task("Observability metrics integration")
        task_in_progress = self._create_task("Error boundary implementation")
        sprint_id = self._create_sprint("Sprint Epsilon Mixed Status Guard")
        _start_sprint(self.test, self.roadmap, sprint_id)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(sprint_id), str(task_unstarted) + "," + str(task_in_progress)
        ])
        _advance_task_to_doing(self.test, self.roadmap, task_in_progress)
        self.test.assert_task_status(self.roadmap, task_unstarted, "SPRINT")
        self.test.assert_task_status(self.roadmap, task_in_progress, "DOING")
        ec, stdout, stderr = _close_sprint(self.test, self.roadmap, sprint_id)
        assert ec != 0, (
            "AC-3 mixed FAIL: Expected blocking on mixed SPRINT+DOING tasks, got exit " + str(ec)
        )
        combined = stdout + stderr
        assert str(task_unstarted) in combined, (
            "AC-3 mixed FAIL: Unstarted task " + str(task_unstarted) + " missing from error."
        )
        assert str(task_in_progress) in combined, (
            "AC-3 mixed FAIL: In-progress task " + str(task_in_progress) + " missing from error."
        )
        self.test.assert_sprint_status(self.roadmap, sprint_id, "OPEN")

    # ----------------------------------------------------------------
    # AC-4: sprint close --force closes despite SPRINT-status tasks
    # ----------------------------------------------------------------

    def test_ac4_force_close_with_sprint_status_tasks_succeeds(self):
        """AC-4: sprint close --force must succeed (exit 0) and warn on stderr."""
        task1_id = self._create_task("Search indexing worker refactor")
        task2_id = self._create_task("Notification delivery queue")
        sprint_id = self._create_sprint("Sprint Zeta Force Close Validation")
        _start_sprint(self.test, self.roadmap, sprint_id)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(sprint_id), str(task1_id) + "," + str(task2_id)
        ])
        self.test.assert_task_status(self.roadmap, task1_id, "SPRINT")
        self.test.assert_task_status(self.roadmap, task2_id, "SPRINT")
        ec, stdout, stderr = _close_sprint(self.test, self.roadmap, sprint_id, force=True)
        assert ec == 0, (
            "AC-4 FAIL: sprint close --force with SPRINT-status tasks must exit 0, got " + str(ec)
            + ". stderr=" + repr(stderr)
        )
        self.test.assert_sprint_status(self.roadmap, sprint_id, "CLOSED")
        assert stderr.strip(), "AC-4 FAIL: Expected warning on stderr, was empty."
        assert "warning" in stderr.lower(), (
            "AC-4 FAIL: stderr does not contain word warning. stderr=" + repr(stderr)
        )
        assert str(task1_id) in stderr, (
            "AC-4 FAIL: Task " + str(task1_id) + " missing from --force warning. stderr=" + repr(stderr)
        )
        assert str(task2_id) in stderr, (
            "AC-4 FAIL: Task " + str(task2_id) + " missing from --force warning. stderr=" + repr(stderr)
        )

    def test_ac4_force_close_with_doing_and_sprint_tasks(self):
        """AC-4 mixed: sprint close --force with both DOING and SPRINT tasks must succeed and warn."""
        task_unstarted = self._create_task("Feature flag evaluation service")
        task_in_progress = self._create_task("OAuth2 token refresh handler")
        sprint_id = self._create_sprint("Sprint Eta Force Close Mixed Statuses")
        _start_sprint(self.test, self.roadmap, sprint_id)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(sprint_id), str(task_unstarted) + "," + str(task_in_progress)
        ])
        _advance_task_to_doing(self.test, self.roadmap, task_in_progress)
        ec, stdout, stderr = _close_sprint(self.test, self.roadmap, sprint_id, force=True)
        assert ec == 0, "AC-4 mixed FAIL: --force must succeed, got exit " + str(ec)
        self.test.assert_sprint_status(self.roadmap, sprint_id, "CLOSED")
        assert str(task_unstarted) in stderr, (
            "AC-4 mixed FAIL: Unstarted task " + str(task_unstarted) + " missing from warning."
        )
        assert str(task_in_progress) in stderr, (
            "AC-4 mixed FAIL: In-progress task " + str(task_in_progress) + " missing from warning."
        )

    # ----------------------------------------------------------------
    # AC-5: sprint add-tasks on OPEN and PENDING sprints works normally
    # ----------------------------------------------------------------

    def test_ac5_add_tasks_to_open_sprint_succeeds(self):
        """AC-5: sprint add-tasks on an OPEN sprint must succeed (exit 0)."""
        task_id = self._create_task("Distributed tracing instrumentation")
        sprint_id = self._create_sprint("Sprint Theta Open Sprint Add-Tasks")
        _start_sprint(self.test, self.roadmap, sprint_id)
        self.test.assert_sprint_status(self.roadmap, sprint_id, "OPEN")
        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)],
            check=False,
        )
        assert ec == 0, (
            "AC-5 FAIL: sprint add-tasks on OPEN sprint must exit 0, got " + str(ec)
            + ". stderr=" + repr(stderr)
        )
        self.test.assert_task_status(self.roadmap, task_id, "SPRINT")

    def test_ac5_add_tasks_to_pending_sprint_succeeds(self):
        """AC-5: sprint add-tasks on a PENDING sprint must succeed (exit 0)."""
        task_id = self._create_task("GraphQL schema federation layer")
        sprint_id = self._create_sprint("Sprint Iota Pending Sprint Add-Tasks")
        self.test.assert_sprint_status(self.roadmap, sprint_id, "PENDING")
        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)],
            check=False,
        )
        assert ec == 0, (
            "AC-5 FAIL: sprint add-tasks on PENDING sprint must exit 0, got " + str(ec)
            + ". stderr=" + repr(stderr)
        )
        self.test.assert_task_status(self.roadmap, task_id, "SPRINT")

    # ----------------------------------------------------------------
    # Regression: Completed tasks must not block close
    # ----------------------------------------------------------------

    def test_completed_tasks_do_not_block_close(self):
        """Regression: COMPLETED tasks must NOT block sprint close."""
        task_id = self._create_task("Async job queue worker")
        sprint_id = self._create_sprint("Sprint Kappa Completed Task Regression")
        _start_sprint(self.test, self.roadmap, sprint_id)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)
        ])
        _advance_task_to_doing(self.test, self.roadmap, task_id)
        _advance_task_to_testing(self.test, self.roadmap, task_id)
        _advance_task_to_completed(self.test, self.roadmap, task_id)
        self.test.assert_task_status(self.roadmap, task_id, "COMPLETED")
        ec, stdout, stderr = _close_sprint(self.test, self.roadmap, sprint_id)
        assert ec == 0, (
            "Regression FAIL: COMPLETED task must not block sprint close, got exit " + str(ec)
            + ". stderr=" + repr(stderr)
        )
        self.test.assert_sprint_status(self.roadmap, sprint_id, "CLOSED")

    # ----------------------------------------------------------------
    # Exit code precision tests
    # ----------------------------------------------------------------

    def test_ac1_exit_code_is_6_for_closed_sprint(self):
        """AC-1 exit code: ErrInvalidInput must map to exit 6."""
        task_id = self._create_task("Telemetry event batching service")
        sprint_id = self._create_sprint("Sprint Lambda Exit Code Precision")
        _start_sprint(self.test, self.roadmap, sprint_id)
        _close_sprint(self.test, self.roadmap, sprint_id)
        ec, _, _ = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)],
            check=False,
        )
        assert ec == 6, (
            "AC-1 exit code FAIL: Expected exit code 6 (ErrInvalidInput), got " + str(ec)
        )

    def test_ac3_exit_code_is_6_for_sprint_status_tasks(self):
        """AC-3 exit code: sprint close blocked by SPRINT-status tasks must return exit 6."""
        task_id = self._create_task("Content delivery network integration")
        sprint_id = self._create_sprint("Sprint Mu Exit Code for SPRINT Tasks")
        _start_sprint(self.test, self.roadmap, sprint_id)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)
        ])
        self.test.assert_task_status(self.roadmap, task_id, "SPRINT")
        ec, _, _ = _close_sprint(self.test, self.roadmap, sprint_id)
        assert ec == 6, (
            "AC-3 exit code FAIL: Expected exit code 6 (ErrInvalidInput), got " + str(ec)
        )

    # ----------------------------------------------------------------
    # Output hygiene: --force warning must go to stderr, not stdout
    # ----------------------------------------------------------------

    def test_ac4_force_close_produces_no_stdout(self):
        """AC-4 output hygiene: sprint close --force must not write to stdout on success."""
        task_id = self._create_task("Service mesh configuration manager")
        sprint_id = self._create_sprint("Sprint Nu Force Close Output Hygiene")
        _start_sprint(self.test, self.roadmap, sprint_id)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)
        ])
        ec, stdout, stderr = _close_sprint(self.test, self.roadmap, sprint_id, force=True)
        assert ec == 0, "AC-4 output hygiene FAIL: --force must exit 0, got " + str(ec)
        assert stdout.strip() == "", (
            "AC-4 output hygiene FAIL: Expected empty stdout, got: " + repr(stdout)
        )
        assert stderr.strip() != "", (
            "AC-4 output hygiene FAIL: Expected warning on stderr, stderr was empty."
        )
