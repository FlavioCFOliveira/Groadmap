#!/usr/bin/env python3
"""
Test 19: completion_summary Feature (Sprint 11)
Covers tasks #93-#96: schema migration 1.3.0, CompletionSummary model field,
--summary flag on task stat, and state-machine reset on reopen.
"""

import sys
import os
from typing import Optional
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

def _get_task(test: GroadmapTestBase, roadmap: str, task_id: int) -> dict:
    """Fetch a single task and always return a dict."""
    result = test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
    if isinstance(result, list):
        return result[0] if result else {}
    return result


def _create_feature_task(test: GroadmapTestBase, roadmap: str) -> int:
    """Create a realistic task and return its ID."""
    return test.create_task(
        roadmap=roadmap,
        title="Implement OAuth2 device-code flow for CLI authentication",
        functional_requirements=(
            "Users need a way to authenticate the CLI against the API without "
            "entering credentials in the terminal. The OAuth2 device-code flow "
            "allows browser-based login and stores the resulting token securely."
        ),
        technical_requirements=(
            "Add internal/auth/device_flow.go; call POST /oauth2/device_authorize, "
            "poll POST /oauth2/token until approved or timeout; persist token to "
            "~/.roadmaps/.credentials using 0600 permissions."
        ),
        acceptance_criteria=(
            "rmp auth login opens browser and stores token; "
            "rmp auth status shows authenticated user; "
            "expired token triggers refresh automatically."
        ),
        priority=7,
    )


def _advance_to_testing(test: GroadmapTestBase, roadmap: str, task_id: int):
    """Drive a task from BACKLOG through SPRINT → DOING → TESTING."""
    test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "SPRINT"])
    test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
    test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])


def _advance_to_completed(
    test: GroadmapTestBase,
    roadmap: str,
    task_id: int,
    summary: Optional[str] = None,
):
    """Drive a task from BACKLOG all the way to COMPLETED."""
    _advance_to_testing(test, roadmap, task_id)
    cmd = ["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"]
    if summary is not None:
        cmd.extend(["--summary", summary])
    test.run_cmd(cmd)


# ---------------------------------------------------------------------------
# Happy path
# ---------------------------------------------------------------------------

class TestCompletionSummaryHappyPath:
    """Acceptance criteria 1, 5, 6 — store, null, bulk."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_summary_stored_on_completed_transition(self):
        """--summary value is persisted and returned in task get JSON."""
        roadmap = self.test.create_roadmap()
        task_id = _create_feature_task(self.test, roadmap)
        expected = "Implemented device-code flow; token stored at ~/.roadmaps/.credentials; all unit tests green."

        _advance_to_testing(self.test, roadmap, task_id)
        self.test.run_cmd([
            "task", "stat", "-r", roadmap, str(task_id), "COMPLETED",
            "--summary", expected,
        ])

        task = _get_task(self.test, roadmap, task_id)
        assert task.get("status") == "COMPLETED", f"Expected COMPLETED, got {task.get('status')}"
        assert task.get("completion_summary") == expected, (
            f"Expected completion_summary={expected!r}, got {task.get('completion_summary')!r}"
        )
        assert task.get("closed_at") is not None, "closed_at must be set on COMPLETED"

        print("✓ completion_summary stored correctly on COMPLETED transition")

    def test_summary_stored_using_short_flag(self):
        """-s shorthand stores the summary identically to --summary."""
        roadmap = self.test.create_roadmap()
        task_id = _create_feature_task(self.test, roadmap)
        expected = "Used -s shorthand; feature verified in staging environment."

        _advance_to_testing(self.test, roadmap, task_id)
        self.test.run_cmd([
            "task", "stat", "-r", roadmap, str(task_id), "COMPLETED",
            "-s", expected,
        ])

        task = _get_task(self.test, roadmap, task_id)
        assert task.get("completion_summary") == expected, (
            f"Short flag -s did not store summary correctly: {task.get('completion_summary')!r}"
        )

        print("✓ -s shorthand stores completion_summary correctly")

    def test_completed_without_summary_leaves_field_null(self):
        """Transitioning to COMPLETED without --summary leaves completion_summary null."""
        roadmap = self.test.create_roadmap()
        task_id = _create_feature_task(self.test, roadmap)

        _advance_to_testing(self.test, roadmap, task_id)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        task = _get_task(self.test, roadmap, task_id)
        assert task.get("status") == "COMPLETED"
        assert task.get("completion_summary") is None, (
            f"Expected null completion_summary, got {task.get('completion_summary')!r}"
        )

        print("✓ completion_summary is null when --summary is omitted")

    def test_completion_summary_field_present_in_task_list(self):
        """completion_summary appears in task list JSON output (not omitted)."""
        roadmap = self.test.create_roadmap()
        task_id = _create_feature_task(self.test, roadmap)
        summary = "Merged to main after code review; canary deployment successful."

        _advance_to_completed(self.test, roadmap, task_id, summary)

        tasks = self.test.run_cmd_json(["task", "list", "-r", roadmap])
        matching = [t for t in tasks if t.get("id") == task_id]
        assert matching, f"Task {task_id} not found in task list output"
        assert matching[0].get("completion_summary") == summary, (
            f"completion_summary missing or wrong in task list: {matching[0].get('completion_summary')!r}"
        )

        print("✓ completion_summary present in task list output")

    def test_bulk_stat_applies_same_summary_to_all_ids(self):
        """Bulk task stat applies the same completion_summary to every task ID."""
        roadmap = self.test.create_roadmap()

        task_ids = []
        for _ in range(3):
            tid = _create_feature_task(self.test, roadmap)
            _advance_to_testing(self.test, roadmap, tid)
            task_ids.append(tid)

        bulk_arg = ",".join(str(i) for i in task_ids)
        shared_summary = "Sprint review accepted; performance benchmarks within SLA thresholds."

        self.test.run_cmd([
            "task", "stat", "-r", roadmap, bulk_arg, "COMPLETED",
            "--summary", shared_summary,
        ])

        for tid in task_ids:
            task = _get_task(self.test, roadmap, tid)
            assert task.get("status") == "COMPLETED", f"Task {tid} not COMPLETED"
            assert task.get("completion_summary") == shared_summary, (
                f"Task {tid} has wrong completion_summary: {task.get('completion_summary')!r}"
            )

        print("✓ Bulk stat applies same completion_summary to all task IDs")


# ---------------------------------------------------------------------------
# Validation errors
# ---------------------------------------------------------------------------

class TestCompletionSummaryValidation:
    """Acceptance criteria 2, 3, 4 — invalid target, oversized, exact boundary."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def _create_and_advance(self, roadmap: str, target_status: str) -> int:
        """Create a task and advance it to target_status."""
        task_id = _create_feature_task(self.test, roadmap)
        status_path = {
            "BACKLOG": [],
            "SPRINT": ["SPRINT"],
            "DOING": ["SPRINT", "DOING"],
            "TESTING": ["SPRINT", "DOING", "TESTING"],
        }
        for status in status_path[target_status]:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), status])
        return task_id

    def test_summary_on_doing_target_fails(self):
        """--summary with DOING target returns non-zero exit code and no task is modified."""
        roadmap = self.test.create_roadmap()
        task_id = _create_feature_task(self.test, roadmap)

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "DOING", "--summary", "premature summary"],
            check=False,
        )
        assert exit_code != 0, "--summary on DOING should fail"
        assert "COMPLETED" in stderr.upper() or "summary" in stderr.lower(), (
            f"Error message should reference COMPLETED or summary: {stderr!r}"
        )

        # Task must remain in BACKLOG — no side effects
        task = _get_task(self.test, roadmap, task_id)
        assert task.get("status") == "BACKLOG", (
            f"Task must not be modified on --summary validation failure; got {task.get('status')}"
        )

        print("✓ --summary on DOING target rejected; task unchanged")

    def test_summary_on_testing_target_fails(self):
        """--summary with TESTING target returns non-zero exit code."""
        roadmap = self.test.create_roadmap()
        task_id = self._create_and_advance(roadmap, "DOING")

        exit_code, _, _ = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "TESTING",
             "--summary", "should not be stored"],
            check=False,
        )
        assert exit_code != 0, "--summary on TESTING should fail"

        # Task must remain DOING
        task = _get_task(self.test, roadmap, task_id)
        assert task.get("status") == "DOING", (
            f"Task must not advance to TESTING on failure; got {task.get('status')}"
        )

        print("✓ --summary on TESTING target rejected; task unchanged")

    def test_summary_on_sprint_target_fails(self):
        """--summary with SPRINT target returns non-zero exit code."""
        roadmap = self.test.create_roadmap()
        task_id = _create_feature_task(self.test, roadmap)

        exit_code, _, _ = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "SPRINT",
             "--summary", "invalid target"],
            check=False,
        )
        assert exit_code != 0, "--summary on SPRINT should fail"

        task = _get_task(self.test, roadmap, task_id)
        assert task.get("status") == "BACKLOG", (
            f"Task must not change status on failure; got {task.get('status')}"
        )

        print("✓ --summary on SPRINT target rejected; task unchanged")

    def test_summary_4097_chars_fails(self):
        """--summary with 4097 characters is rejected with non-zero exit code."""
        roadmap = self.test.create_roadmap()
        task_id = _create_feature_task(self.test, roadmap)
        _advance_to_testing(self.test, roadmap, task_id)

        oversized_summary = "A" * 4097

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "COMPLETED",
             "--summary", oversized_summary],
            check=False,
        )
        assert exit_code != 0, "4097-char summary should be rejected"

        # Task must remain in TESTING — no partial write
        task = _get_task(self.test, roadmap, task_id)
        assert task.get("status") == "TESTING", (
            f"Task must stay TESTING on oversized summary; got {task.get('status')}"
        )
        assert task.get("completion_summary") is None, (
            "completion_summary must not be written when validation fails"
        )

        print("✓ 4097-char summary rejected; task unchanged in TESTING")

    def test_summary_exactly_4096_chars_accepted(self):
        """--summary with exactly 4096 characters is accepted at the boundary."""
        roadmap = self.test.create_roadmap()
        task_id = _create_feature_task(self.test, roadmap)
        _advance_to_testing(self.test, roadmap, task_id)

        boundary_summary = "B" * 4096

        self.test.run_cmd([
            "task", "stat", "-r", roadmap, str(task_id), "COMPLETED",
            "--summary", boundary_summary,
        ])

        task = _get_task(self.test, roadmap, task_id)
        assert task.get("status") == "COMPLETED", (
            f"4096-char summary should be accepted; task status: {task.get('status')}"
        )
        assert task.get("completion_summary") == boundary_summary, (
            f"Boundary summary not stored correctly (length={len(task.get('completion_summary', '') or '')})"
        )

        print("✓ 4096-char summary accepted at exact boundary")


# ---------------------------------------------------------------------------
# Reopen resets completion_summary
# ---------------------------------------------------------------------------

class TestCompletionSummaryClearOnReopen:
    """Acceptance criteria 7, 8, 9 — BACKLOG transition and task reopen clear the field."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_stat_backlog_clears_completion_summary(self):
        """task stat BACKLOG (COMPLETED → BACKLOG) sets completion_summary to null."""
        roadmap = self.test.create_roadmap()
        task_id = _create_feature_task(self.test, roadmap)
        original_summary = "Integration tests passed in staging; approved for production rollout."

        _advance_to_completed(self.test, roadmap, task_id, original_summary)

        # Verify it was set
        task = _get_task(self.test, roadmap, task_id)
        assert task.get("completion_summary") == original_summary

        # Reopen via task stat BACKLOG
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "BACKLOG"])

        task = _get_task(self.test, roadmap, task_id)
        assert task.get("status") == "BACKLOG", (
            f"Expected BACKLOG after reopen, got {task.get('status')}"
        )
        assert task.get("completion_summary") is None, (
            f"completion_summary must be null after BACKLOG transition, got {task.get('completion_summary')!r}"
        )
        # Lifecycle timestamps must also be cleared
        assert task.get("started_at") is None, "started_at must be cleared on reopen"
        assert task.get("tested_at") is None, "tested_at must be cleared on reopen"
        assert task.get("closed_at") is None, "closed_at must be cleared on reopen"

        print("✓ task stat BACKLOG clears completion_summary and all lifecycle timestamps")

    def test_task_reopen_clears_completion_summary(self):
        """task reopen clears completion_summary alongside lifecycle timestamps."""
        roadmap = self.test.create_roadmap()
        task_id = _create_feature_task(self.test, roadmap)
        original_summary = "Load test confirmed p99 latency under 50 ms; feature flag enabled globally."

        _advance_to_completed(self.test, roadmap, task_id, original_summary)

        # Verify the summary was stored
        task = _get_task(self.test, roadmap, task_id)
        assert task.get("completion_summary") == original_summary

        # Reopen via dedicated task reopen command
        self.test.run_cmd(["task", "reopen", "-r", roadmap, str(task_id)])

        task = _get_task(self.test, roadmap, task_id)
        assert task.get("status") == "BACKLOG", (
            f"Expected BACKLOG after reopen, got {task.get('status')}"
        )
        assert task.get("completion_summary") is None, (
            f"completion_summary must be null after task reopen, got {task.get('completion_summary')!r}"
        )
        assert task.get("started_at") is None, "started_at must be cleared"
        assert task.get("tested_at") is None, "tested_at must be cleared"
        assert task.get("closed_at") is None, "closed_at must be cleared"

        print("✓ task reopen clears completion_summary and all lifecycle timestamps")

    def test_created_at_unchanged_after_reopen(self):
        """created_at is preserved across reopen cycles."""
        roadmap = self.test.create_roadmap()
        task_id = _create_feature_task(self.test, roadmap)

        task_before = _get_task(self.test, roadmap, task_id)
        original_created_at = task_before.get("created_at")
        assert original_created_at is not None

        _advance_to_completed(self.test, roadmap, task_id, "First cycle complete.")
        self.test.run_cmd(["task", "reopen", "-r", roadmap, str(task_id)])

        task_after = _get_task(self.test, roadmap, task_id)
        assert task_after.get("created_at") == original_created_at, (
            f"created_at changed after reopen: {original_created_at!r} → {task_after.get('created_at')!r}"
        )

        print("✓ created_at is preserved across reopen")

    def test_new_summary_can_be_set_after_reopen(self):
        """After reopen, a second development cycle can store a different summary."""
        roadmap = self.test.create_roadmap()
        task_id = _create_feature_task(self.test, roadmap)

        first_summary = "First attempt: passed unit tests but failed performance benchmark."
        second_summary = "Second attempt: rewrote hot path; all benchmarks within budget."

        # First cycle
        _advance_to_completed(self.test, roadmap, task_id, first_summary)
        task = _get_task(self.test, roadmap, task_id)
        assert task.get("completion_summary") == first_summary

        # Reopen
        self.test.run_cmd(["task", "reopen", "-r", roadmap, str(task_id)])
        task = _get_task(self.test, roadmap, task_id)
        assert task.get("completion_summary") is None

        # Second cycle
        _advance_to_completed(self.test, roadmap, task_id, second_summary)
        task = _get_task(self.test, roadmap, task_id)
        assert task.get("completion_summary") == second_summary, (
            f"Expected second_summary, got {task.get('completion_summary')!r}"
        )

        print("✓ New completion_summary can be set after reopen (second cycle)")

    def test_bulk_reopen_clears_completion_summary_for_all(self):
        """Bulk task stat BACKLOG clears completion_summary for every task."""
        roadmap = self.test.create_roadmap()

        task_ids = []
        for i in range(3):
            tid = _create_feature_task(self.test, roadmap)
            _advance_to_completed(self.test, roadmap, tid, f"Cycle 1 complete for task index {i}.")
            task_ids.append(tid)

        bulk_arg = ",".join(str(i) for i in task_ids)
        self.test.run_cmd(["task", "stat", "-r", roadmap, bulk_arg, "BACKLOG"])

        for tid in task_ids:
            task = _get_task(self.test, roadmap, tid)
            assert task.get("status") == "BACKLOG", f"Task {tid} not BACKLOG"
            assert task.get("completion_summary") is None, (
                f"Task {tid} completion_summary not cleared: {task.get('completion_summary')!r}"
            )

        print("✓ Bulk BACKLOG transition clears completion_summary for all tasks")


# ---------------------------------------------------------------------------
# Entry point for direct execution
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    import traceback

    suites = [
        TestCompletionSummaryHappyPath,
        TestCompletionSummaryValidation,
        TestCompletionSummaryClearOnReopen,
    ]

    passed = 0
    failed = 0

    for suite_class in suites:
        suite = suite_class()
        methods = [m for m in dir(suite) if m.startswith("test_")]
        for method_name in methods:
            suite.setup_method()
            try:
                getattr(suite, method_name)()
                passed += 1
            except Exception as exc:
                print(f"FAIL  {suite_class.__name__}.{method_name}: {exc}")
                traceback.print_exc()
                failed += 1
            finally:
                suite.teardown_method()

    total = passed + failed
    print(f"\n{passed}/{total} tests passed", "✓" if failed == 0 else "✗")
    sys.exit(0 if failed == 0 else 1)
