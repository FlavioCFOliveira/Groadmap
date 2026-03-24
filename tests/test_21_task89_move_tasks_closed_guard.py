#!/usr/bin/env python3
"""
Test 21: Task #89 -- Validate Task Moves Against Sprint Status (Reject Moves to/from Closed Sprint)

Acceptance Criteria validated:
  AC-1: sprint move-tasks TO a CLOSED sprint returns a non-zero exit code with message
         indicating sprint ID and status CLOSED
  AC-2: sprint move-tasks FROM a CLOSED sprint returns a non-zero exit code with message
         indicating sprint ID and status CLOSED
  AC-3: sprint move-tasks between OPEN and PENDING sprints succeeds (exit code 0)
  AC-4: sprint move-tasks from OPEN to OPEN sprint succeeds (exit code 0)

Sequential-open constraint:
  Only one sprint can be OPEN at a time. To reach CLOSED status for a sprint
  while another sprint is present, we must close the first sprint, then start
  and close the second, then start a third. Helper _close_sprint_empty() closes
  a sprint that has no active (SPRINT/DOING/TESTING) tasks.

Setup strategy per AC:
  AC-1: sprint A -> OPEN, sprint B -> started->CLOSED (close A first, start B, close B empty),
        sprint C -> OPEN with task, attempt move C -> B (CLOSED destination)
  AC-2: sprint A -> OPEN with task completed -> CLOSED (source),
        sprint B -> PENDING, attempt move A (CLOSED source) -> B
  AC-3: source OPEN, dest PENDING (sequential constraint met since only one OPEN)
  AC-4: source OPEN, dest PENDING (same approach; move is accepted by implementation)
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestTask89MoveTasksClosedGuard:
    """Validate Task #89: Reject sprint move-tasks to/from a CLOSED sprint."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = "qa-task89-test"
        self.test.run_cmd(["roadmap", "create", self.roadmap])

    def teardown_method(self):
        self.test.teardown()

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _create_task(self, title: str) -> int:
        return self.test.create_task(
            self.roadmap,
            title,
            "Implement " + title + " following the architecture guidelines",
            "Apply domain patterns, write unit tests, handle edge cases",
            title + " behaves correctly under all documented inputs",
        )

    def _create_sprint(self, description: str) -> int:
        return self.test.create_sprint(self.roadmap, description)

    def _start_sprint(self, sprint_id: int):
        self.test.run_cmd(["sprint", "start", "-r", self.roadmap, str(sprint_id)])

    def _close_sprint_empty(self, sprint_id: int):
        """Close a sprint that contains no active tasks (no --force needed)."""
        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "close", "-r", self.roadmap, str(sprint_id)],
            check=False,
        )
        assert ec == 0, (
            "Setup FAIL: could not close empty sprint #" + str(sprint_id)
            + ". stderr=" + repr(stderr)
        )

    def _complete_task(self, task_id: int):
        """Advance a task from SPRINT all the way to COMPLETED."""
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(task_id), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(task_id), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(task_id), "COMPLETED"])

    def _assert_task_in_sprint(self, sprint_id: int, task_id: int):
        """Assert a task is present in the given sprint."""
        result = self.test.run_cmd_json(
            ["sprint", "tasks", "-r", self.roadmap, str(sprint_id)]
        )
        ids_in_sprint = [t["id"] for t in result]
        assert task_id in ids_in_sprint, (
            "Task " + str(task_id) + " is not in sprint #" + str(sprint_id)
            + ". ids_in_sprint=" + repr(ids_in_sprint)
        )

    def _assert_task_not_in_sprint(self, sprint_id: int, task_id: int):
        """Assert a task is absent from the given sprint (move was rejected)."""
        result = self.test.run_cmd_json(
            ["sprint", "tasks", "-r", self.roadmap, str(sprint_id)]
        )
        ids_in_sprint = [t["id"] for t in result]
        assert task_id not in ids_in_sprint, (
            "Task " + str(task_id) + " must not be in sprint #" + str(sprint_id)
            + " after a rejected move. ids_in_sprint=" + repr(ids_in_sprint)
        )

    # ------------------------------------------------------------------
    # AC-1: move-tasks TO a CLOSED sprint must fail
    #
    # Setup:
    #   1. Sprint A created and started (OPEN) — no tasks
    #   2. Close sprint A (empty sprint, no --force needed)
    #   3. Sprint B created and started (OPEN) — with the task
    #   4. Sprint C created, kept PENDING (this is the CLOSED destination)
    #      Actually: we re-use A as CLOSED destination (sprint A is now CLOSED)
    #   Move: B (OPEN) -> A (CLOSED) — must fail
    # ------------------------------------------------------------------

    def test_ac1_move_tasks_to_closed_sprint_fails(self):
        """AC-1: sprint move-tasks to a CLOSED sprint must return non-zero exit code."""
        # Sprint that will become CLOSED (destination in the failing move)
        dest_closed_id = self._create_sprint("Sprint Andromeda Destination Closed")
        self._start_sprint(dest_closed_id)
        self._close_sprint_empty(dest_closed_id)
        self.test.assert_sprint_status(self.roadmap, dest_closed_id, "CLOSED")

        # Source sprint is now OPEN with the task
        source_open_id = self._create_sprint("Sprint Betelgeuse Source Open")
        self._start_sprint(source_open_id)
        self.test.assert_sprint_status(self.roadmap, source_open_id, "OPEN")

        task_id = self._create_task("Request throttling middleware")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(source_open_id), str(task_id),
        ])
        self.test.assert_task_status(self.roadmap, task_id, "SPRINT")

        # Attempt move to CLOSED destination — must fail
        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "move-tasks", "-r", self.roadmap,
             str(source_open_id), str(dest_closed_id), str(task_id)],
            check=False,
        )
        assert ec != 0, (
            "AC-1 FAIL: Expected non-zero exit code when moving task to CLOSED sprint, got exit "
            + str(ec)
        )

        # Task must still be in source sprint — operation was rejected
        self._assert_task_in_sprint(source_open_id, task_id)

    def test_ac1_error_message_references_dest_sprint_id_and_closed(self):
        """AC-1 message: Error must include the destination sprint ID and word CLOSED."""
        dest_closed_id = self._create_sprint("Sprint Cygnus Destination Closed")
        self._start_sprint(dest_closed_id)
        self._close_sprint_empty(dest_closed_id)

        source_open_id = self._create_sprint("Sprint Draco Source Open")
        self._start_sprint(source_open_id)

        task_id = self._create_task("Circuit breaker pattern implementation")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(source_open_id), str(task_id),
        ])

        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "move-tasks", "-r", self.roadmap,
             str(source_open_id), str(dest_closed_id), str(task_id)],
            check=False,
        )
        assert ec != 0, "AC-1 message FAIL: Command should have failed."
        combined = stdout + stderr
        assert str(dest_closed_id) in combined, (
            "AC-1 message FAIL: Destination sprint ID " + str(dest_closed_id)
            + " not found in error output. stderr=" + repr(stderr)
        )
        assert "closed" in combined.lower(), (
            "AC-1 message FAIL: Word CLOSED not found in error output. stderr=" + repr(stderr)
        )

    def test_ac1_exit_code_is_6_for_closed_destination(self):
        """AC-1 exit code: ErrInvalidInput (CLOSED destination) must map to exit code 6."""
        dest_closed_id = self._create_sprint("Sprint Eridanus Destination Closed")
        self._start_sprint(dest_closed_id)
        self._close_sprint_empty(dest_closed_id)

        source_open_id = self._create_sprint("Sprint Fornax Source Open")
        self._start_sprint(source_open_id)

        task_id = self._create_task("Distributed lock manager")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(source_open_id), str(task_id),
        ])

        ec, _, _ = self.test.run_cmd(
            ["sprint", "move-tasks", "-r", self.roadmap,
             str(source_open_id), str(dest_closed_id), str(task_id)],
            check=False,
        )
        assert ec == 6, (
            "AC-1 exit code FAIL: Expected exit code 6 (ErrInvalidInput), got " + str(ec)
        )

    # ------------------------------------------------------------------
    # AC-2: move-tasks FROM a CLOSED sprint must fail
    #
    # Setup:
    #   1. Sprint A created and started (OPEN), task added and completed
    #   2. Sprint A closed (no active tasks remain)
    #   3. Sprint B created, kept PENDING (destination)
    #   Attempt move: A (CLOSED) -> B (PENDING) — must fail
    # ------------------------------------------------------------------

    def test_ac2_move_tasks_from_closed_sprint_fails(self):
        """AC-2: sprint move-tasks from a CLOSED sprint must return non-zero exit code."""
        source_closed_id = self._create_sprint("Sprint Gemini Source Closed")
        self._start_sprint(source_closed_id)

        task_id = self._create_task("Saga pattern orchestrator")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(source_closed_id), str(task_id),
        ])
        self._complete_task(task_id)
        self._close_sprint_empty(source_closed_id)
        self.test.assert_sprint_status(self.roadmap, source_closed_id, "CLOSED")

        dest_pending_id = self._create_sprint("Sprint Hercules Destination Pending")
        self.test.assert_sprint_status(self.roadmap, dest_pending_id, "PENDING")

        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "move-tasks", "-r", self.roadmap,
             str(source_closed_id), str(dest_pending_id), str(task_id)],
            check=False,
        )
        assert ec != 0, (
            "AC-2 FAIL: Expected non-zero exit code when moving task from CLOSED sprint, got exit "
            + str(ec)
        )

    def test_ac2_error_message_references_source_sprint_id_and_closed(self):
        """AC-2 message: Error must include the source sprint ID and word CLOSED."""
        source_closed_id = self._create_sprint("Sprint Indus Source Closed")
        self._start_sprint(source_closed_id)

        task_id = self._create_task("Event sourcing aggregate root")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(source_closed_id), str(task_id),
        ])
        self._complete_task(task_id)
        self._close_sprint_empty(source_closed_id)

        dest_pending_id = self._create_sprint("Sprint Lacerta Destination Pending")

        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "move-tasks", "-r", self.roadmap,
             str(source_closed_id), str(dest_pending_id), str(task_id)],
            check=False,
        )
        assert ec != 0, "AC-2 message FAIL: Command should have failed."
        combined = stdout + stderr
        assert str(source_closed_id) in combined, (
            "AC-2 message FAIL: Source sprint ID " + str(source_closed_id)
            + " not found in error output. stderr=" + repr(stderr)
        )
        assert "closed" in combined.lower(), (
            "AC-2 message FAIL: Word CLOSED not found in error output. stderr=" + repr(stderr)
        )

    def test_ac2_exit_code_is_6_for_closed_source(self):
        """AC-2 exit code: ErrInvalidInput (CLOSED source) must map to exit code 6."""
        source_closed_id = self._create_sprint("Sprint Monoceros Source Closed")
        self._start_sprint(source_closed_id)

        task_id = self._create_task("CQRS command handler pipeline")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(source_closed_id), str(task_id),
        ])
        self._complete_task(task_id)
        self._close_sprint_empty(source_closed_id)

        dest_pending_id = self._create_sprint("Sprint Norma Destination Pending")

        ec, _, _ = self.test.run_cmd(
            ["sprint", "move-tasks", "-r", self.roadmap,
             str(source_closed_id), str(dest_pending_id), str(task_id)],
            check=False,
        )
        assert ec == 6, (
            "AC-2 exit code FAIL: Expected exit code 6 (ErrInvalidInput), got " + str(ec)
        )

    # ------------------------------------------------------------------
    # AC-3: move-tasks from OPEN to PENDING succeeds
    # ------------------------------------------------------------------

    def test_ac3_move_tasks_open_to_pending_succeeds(self):
        """AC-3: sprint move-tasks from OPEN to PENDING must succeed (exit code 0)."""
        source_sprint_id = self._create_sprint("Sprint Ophiuchus Source Open")
        self._start_sprint(source_sprint_id)
        self.test.assert_sprint_status(self.roadmap, source_sprint_id, "OPEN")

        dest_sprint_id = self._create_sprint("Sprint Perseus Destination Pending")
        self.test.assert_sprint_status(self.roadmap, dest_sprint_id, "PENDING")

        task_id = self._create_task("Telemetry pipeline aggregation service")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(source_sprint_id), str(task_id),
        ])
        self.test.assert_task_status(self.roadmap, task_id, "SPRINT")

        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "move-tasks", "-r", self.roadmap,
             str(source_sprint_id), str(dest_sprint_id), str(task_id)],
            check=False,
        )
        assert ec == 0, (
            "AC-3 FAIL: move-tasks from OPEN to PENDING must exit 0, got "
            + str(ec) + ". stderr=" + repr(stderr)
        )

        # Task must now appear in destination sprint
        self._assert_task_in_sprint(dest_sprint_id, task_id)

    def test_ac3_move_tasks_pending_to_open_succeeds(self):
        """AC-3 variant: sprint move-tasks from PENDING to OPEN must succeed (exit code 0).

        Due to the sequential-open constraint, only one sprint can be OPEN at a time.
        We place the task in the PENDING sprint first (add-tasks works on PENDING sprints),
        then start the destination sprint — but this would require the source to be closed
        first since it cannot be started while dest is open.

        Instead: source PENDING with task -> dest OPEN (close a prior sprint first so
        dest can be started). The move PENDING -> OPEN is valid per the implementation.
        """
        # We need dest to be OPEN. Create a throwaway sprint, start it, close it, then
        # create the real dest and start it so no sequencing conflict arises.
        placeholder_id = self._create_sprint("Sprint Placeholder Before Reticulum")
        self._start_sprint(placeholder_id)
        self._close_sprint_empty(placeholder_id)

        # Source sprint stays PENDING
        source_pending_id = self._create_sprint("Sprint Reticulum Source Pending")
        self.test.assert_sprint_status(self.roadmap, source_pending_id, "PENDING")

        task_id = self._create_task("Blue-green deployment orchestrator")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(source_pending_id), str(task_id),
        ])
        self.test.assert_task_status(self.roadmap, task_id, "SPRINT")

        # Destination sprint: OPEN
        dest_open_id = self._create_sprint("Sprint Scorpius Destination Open")
        self._start_sprint(dest_open_id)
        self.test.assert_sprint_status(self.roadmap, dest_open_id, "OPEN")

        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "move-tasks", "-r", self.roadmap,
             str(source_pending_id), str(dest_open_id), str(task_id)],
            check=False,
        )
        assert ec == 0, (
            "AC-3 variant FAIL: move-tasks from PENDING to OPEN must exit 0, got "
            + str(ec) + ". stderr=" + repr(stderr)
        )

        self._assert_task_in_sprint(dest_open_id, task_id)

    # ------------------------------------------------------------------
    # AC-4: move-tasks from OPEN to non-CLOSED succeeds
    #
    # The sequential-open constraint means two concurrent OPEN sprints are not
    # possible. AC-4 is therefore validated as OPEN -> PENDING (identical to AC-3
    # but with emphasis on the guard logic accepting neither-CLOSED combination).
    # ------------------------------------------------------------------

    def test_ac4_move_tasks_open_to_pending_succeeds(self):
        """AC-4: sprint move-tasks from OPEN to PENDING must succeed (exit code 0).

        verifySprintsExist only blocks when source or destination is CLOSED.
        Moving from OPEN to PENDING is the canonical non-CLOSED -> non-CLOSED case.
        """
        source_sprint_id = self._create_sprint("Sprint Telescopium Source Open AC4")
        self._start_sprint(source_sprint_id)

        dest_sprint_id = self._create_sprint("Sprint Ursa Destination Pending AC4")
        self.test.assert_sprint_status(self.roadmap, dest_sprint_id, "PENDING")

        task_id = self._create_task("Zero-downtime database migration runner")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(source_sprint_id), str(task_id),
        ])
        self.test.assert_task_status(self.roadmap, task_id, "SPRINT")

        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "move-tasks", "-r", self.roadmap,
             str(source_sprint_id), str(dest_sprint_id), str(task_id)],
            check=False,
        )
        assert ec == 0, (
            "AC-4 FAIL: move-tasks between non-CLOSED sprints (OPEN->PENDING) must exit 0, got "
            + str(ec) + ". stderr=" + repr(stderr)
        )

    def test_ac4_move_multiple_tasks_open_to_pending_succeeds(self):
        """AC-4 bulk: moving multiple tasks between non-CLOSED sprints must succeed."""
        source_sprint_id = self._create_sprint("Sprint Vela Source Open Multi")
        self._start_sprint(source_sprint_id)

        dest_sprint_id = self._create_sprint("Sprint Vulpecula Destination Pending Multi")

        task1_id = self._create_task("Webhook event fanout dispatcher")
        task2_id = self._create_task("Idempotency key management service")
        task3_id = self._create_task("Tenant isolation enforcement layer")

        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap, str(source_sprint_id),
            str(task1_id) + "," + str(task2_id) + "," + str(task3_id),
        ])

        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "move-tasks", "-r", self.roadmap,
             str(source_sprint_id), str(dest_sprint_id),
             str(task1_id) + "," + str(task2_id) + "," + str(task3_id)],
            check=False,
        )
        assert ec == 0, (
            "AC-4 bulk FAIL: Moving 3 tasks from OPEN to PENDING must exit 0, got "
            + str(ec) + ". stderr=" + repr(stderr)
        )

        result = self.test.run_cmd_json(
            ["sprint", "tasks", "-r", self.roadmap, str(dest_sprint_id)]
        )
        ids_in_dest = {t["id"] for t in result}
        for tid in (task1_id, task2_id, task3_id):
            assert tid in ids_in_dest, (
                "AC-4 bulk FAIL: Task " + str(tid)
                + " not found in destination sprint after bulk move."
            )

    # ------------------------------------------------------------------
    # Regression: both sprints CLOSED must still fail (double guard)
    # ------------------------------------------------------------------

    def test_both_sprints_closed_fails(self):
        """Regression: move-tasks when BOTH sprints are CLOSED must fail."""
        # Sprint A: start, add task, complete task, close
        source_closed_id = self._create_sprint("Sprint Xi Source Closed Both")
        self._start_sprint(source_closed_id)

        task_id = self._create_task("Cross-region replication controller")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(source_closed_id), str(task_id),
        ])
        self._complete_task(task_id)
        self._close_sprint_empty(source_closed_id)

        # Sprint B: start (A is now CLOSED so no sequencing conflict), close empty
        dest_closed_id = self._create_sprint("Sprint Psi Destination Closed Both")
        self._start_sprint(dest_closed_id)
        self._close_sprint_empty(dest_closed_id)

        self.test.assert_sprint_status(self.roadmap, source_closed_id, "CLOSED")
        self.test.assert_sprint_status(self.roadmap, dest_closed_id, "CLOSED")

        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "move-tasks", "-r", self.roadmap,
             str(source_closed_id), str(dest_closed_id), str(task_id)],
            check=False,
        )
        assert ec != 0, (
            "Regression FAIL: move-tasks between two CLOSED sprints must fail, got exit "
            + str(ec)
        )
        combined = stdout + stderr
        assert "closed" in combined.lower(), (
            "Regression FAIL: Error output must mention CLOSED. stderr=" + repr(stderr)
        )

    # ------------------------------------------------------------------
    # Output hygiene: successful move must produce no stdout
    # ------------------------------------------------------------------

    def test_successful_move_produces_no_stdout(self):
        """Output hygiene: a successful move-tasks must not write to stdout."""
        source_sprint_id = self._create_sprint("Sprint Omega Source Open Hygiene")
        self._start_sprint(source_sprint_id)

        dest_sprint_id = self._create_sprint("Sprint Alpha2 Dest Pending Hygiene")

        task_id = self._create_task("Structured logging context propagation")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap,
            str(source_sprint_id), str(task_id),
        ])

        ec, stdout, stderr = self.test.run_cmd(
            ["sprint", "move-tasks", "-r", self.roadmap,
             str(source_sprint_id), str(dest_sprint_id), str(task_id)],
            check=False,
        )
        assert ec == 0, "Output hygiene FAIL: Expected exit 0, got " + str(ec)
        assert stdout.strip() == "", (
            "Output hygiene FAIL: Expected empty stdout on success, got: " + repr(stdout)
        )
