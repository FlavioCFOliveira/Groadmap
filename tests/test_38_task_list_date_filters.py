#!/usr/bin/env python3
"""
Test 38: task list --created-since / --created-until date filters.

Closes a measured coverage gap: parseFilterDate (internal/commands/task_query.go)
and the task-list creation-date filter path were exercised by no test, so a
regression in date parsing or filtering would pass silently.

Covers, per SPEC/COMMANDS.md (task list):
  - --created-since / --created-until accept full RFC3339 and date-only (YYYY-MM-DD)
  - date-only values are interpreted as start-of-day UTC
  - a since bound in the past includes tasks; in the future excludes them
  - an until bound in the future includes tasks; in the past excludes them
  - combined since+until range
  - an invalid date returns exit code 6 (validation error) naming the flag
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase

# Extreme bounds avoid any wall-clock flakiness: every task is created "now",
# which is unambiguously after PAST and before FUTURE.
PAST_DATE = "2000-01-01"
FUTURE_DATE = "2099-01-01"
PAST_RFC3339 = "2000-01-01T00:00:00Z"
FUTURE_RFC3339 = "2099-01-01T00:00:00Z"

EXIT_VALIDATION = 6


class TestTaskListDateFilters:
    """Validate task list creation-date filtering and its error paths."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("scheduling_engine")
        # Three real tasks, all created now.
        self.task_ids = [
            self.test.create_task(
                self.roadmap,
                title=title,
                functional_requirements="Deterministic scheduling guarantees required",
                technical_requirements="Priority queue with monotonic clock",
                acceptance_criteria="Tasks dispatched in deadline order",
            )
            for title in (
                "Implement deadline scheduler",
                "Add backpressure handling",
                "Wire up metrics exporter",
            )
        ]

    def teardown_method(self):
        self.test.teardown()

    def _ids(self, rows):
        return sorted(r["id"] for r in rows)

    # ---------------- since (date-only) ----------------

    def test_created_since_past_includes_all(self):
        """A since bound in the past returns every task (date-only form)."""
        rows = self.test.run_cmd_json(
            ["task", "list", "-r", self.roadmap, "--created-since", PAST_DATE]
        )
        assert self._ids(rows) == sorted(self.task_ids), (
            f"Expected all {self.task_ids}, got {self._ids(rows)}"
        )

    def test_created_since_future_excludes_all(self):
        """A since bound in the future returns no task (date-only form)."""
        rows = self.test.run_cmd_json(
            ["task", "list", "-r", self.roadmap, "--created-since", FUTURE_DATE]
        )
        assert rows == [], f"Expected no tasks, got {rows}"

    def test_created_since_past_rfc3339_includes_all(self):
        """The RFC3339 branch of parseFilterDate is honoured for --created-since."""
        rows = self.test.run_cmd_json(
            ["task", "list", "-r", self.roadmap, "--created-since", PAST_RFC3339]
        )
        assert self._ids(rows) == sorted(self.task_ids)

    # ---------------- until (date-only) ----------------

    def test_created_until_future_includes_all(self):
        """An until bound in the future returns every task (date-only form)."""
        rows = self.test.run_cmd_json(
            ["task", "list", "-r", self.roadmap, "--created-until", FUTURE_DATE]
        )
        assert self._ids(rows) == sorted(self.task_ids)

    def test_created_until_past_excludes_all(self):
        """An until bound in the past returns no task (date-only form)."""
        rows = self.test.run_cmd_json(
            ["task", "list", "-r", self.roadmap, "--created-until", PAST_DATE]
        )
        assert rows == [], f"Expected no tasks, got {rows}"

    def test_created_until_future_rfc3339_includes_all(self):
        """The RFC3339 branch of parseFilterDate is honoured for --created-until."""
        rows = self.test.run_cmd_json(
            ["task", "list", "-r", self.roadmap, "--created-until", FUTURE_RFC3339]
        )
        assert self._ids(rows) == sorted(self.task_ids)

    # ---------------- combined range ----------------

    def test_combined_range_spanning_now_includes_all(self):
        """A [past, future] range includes every task."""
        rows = self.test.run_cmd_json(
            ["task", "list", "-r", self.roadmap,
             "--created-since", PAST_DATE, "--created-until", FUTURE_DATE]
        )
        assert self._ids(rows) == sorted(self.task_ids)

    def test_combined_range_entirely_in_past_excludes_all(self):
        """A [past, past] range excludes every task created now."""
        rows = self.test.run_cmd_json(
            ["task", "list", "-r", self.roadmap,
             "--created-since", "1999-01-01", "--created-until", PAST_DATE]
        )
        assert rows == [], f"Expected no tasks, got {rows}"

    # ---------------- error paths ----------------

    def test_invalid_created_since_exits_validation(self):
        """A malformed --created-since returns exit 6 and names the flag."""
        exit_code, stdout, stderr = self.test.run_cmd(
            ["task", "list", "-r", self.roadmap, "--created-since", "not-a-date"],
            check=False,
        )
        assert exit_code == EXIT_VALIDATION, (
            f"Expected exit {EXIT_VALIDATION}, got {exit_code} (stderr={stderr!r})"
        )
        assert "--created-since" in stderr, f"Error must name the flag; got {stderr!r}"

    def test_invalid_created_until_exits_validation(self):
        """A malformed --created-until returns exit 6 and names the flag."""
        exit_code, stdout, stderr = self.test.run_cmd(
            ["task", "list", "-r", self.roadmap, "--created-until", "13-13-13"],
            check=False,
        )
        assert exit_code == EXIT_VALIDATION, (
            f"Expected exit {EXIT_VALIDATION}, got {exit_code} (stderr={stderr!r})"
        )
        assert "--created-until" in stderr, f"Error must name the flag; got {stderr!r}"


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
