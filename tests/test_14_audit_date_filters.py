#!/usr/bin/env python3
"""
Test 14: Audit Date Filters
Tests audit list --since and --until date filters.
"""

import sys
import os
import json
from datetime import datetime, timedelta, timezone
import time

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestAuditDateFilters:
    """Test audit date filtering functionality."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_audit_since_filter(self):
        """Test --since filter for audit list."""
        roadmap = self.test.create_roadmap()

        # Record current time
        before = datetime.now(timezone.utc).isoformat()

        # Create first task (this will have audit entry)
        task1_id = self.test.create_task(roadmap, "Task before filter", "Functional", "Technical", "Criteria")

        # Wait briefly to ensure time separation
        time.sleep(0.1)
        mid_time = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create second task
        task2_id = self.test.create_task(roadmap, "Task after filter", "Functional", "Technical", "Criteria")

        after = datetime.now(timezone.utc).isoformat()

        # Test --since filter (should only show entries after mid_time)
        result = self.test.run_cmd_json(["audit", "list", "-r", roadmap, "--since", mid_time])

        # Should contain TASK_CREATE for task2 but not task1
        task_creates = [e for e in result if e["operation"] == "TASK_CREATE"]
        entity_ids = [e["entity_id"] for e in task_creates]

        assert task2_id in entity_ids, "Should include task created after since filter"
        assert task1_id not in entity_ids, "Should exclude task created before since filter"

        print("✓ Audit --since filter test passed")

    def test_audit_until_filter(self):
        """Test --until filter for audit list."""
        roadmap = self.test.create_roadmap()

        # Record start time
        start_time = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create first task
        task1_id = self.test.create_task(roadmap, "Task before cutoff", "Functional", "Technical", "Criteria")

        time.sleep(0.1)
        cutoff_time = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create second task
        task2_id = self.test.create_task(roadmap, "Task after cutoff", "Functional", "Technical", "Criteria")

        # Test --until filter (should only show entries before cutoff_time)
        result = self.test.run_cmd_json(["audit", "list", "-r", roadmap, "--until", cutoff_time])

        # Should contain TASK_CREATE for task1 but not task2
        task_creates = [e for e in result if e["operation"] == "TASK_CREATE"]
        entity_ids = [e["entity_id"] for e in task_creates]

        assert task1_id in entity_ids, "Should include task created before until filter"
        assert task2_id not in entity_ids, "Should exclude task created after until filter"

        print("✓ Audit --until filter test passed")

    def test_audit_combined_date_range_filter(self):
        """Test combined --since and --until filters."""
        roadmap = self.test.create_roadmap()

        # Record start time
        start_time = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create first task (before range)
        task1_id = self.test.create_task(roadmap, "Task before range", "Functional", "Technical", "Criteria")

        time.sleep(0.1)
        range_start = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create second task (within range)
        task2_id = self.test.create_task(roadmap, "Task in range", "Functional", "Technical", "Criteria")

        time.sleep(0.1)
        range_end = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create third task (after range)
        task3_id = self.test.create_task(roadmap, "Task after range", "Functional", "Technical", "Criteria")

        # Test combined filters
        result = self.test.run_cmd_json([
            "audit", "list", "-r", roadmap,
            "--since", range_start,
            "--until", range_end
        ])

        task_creates = [e for e in result if e["operation"] == "TASK_CREATE"]
        entity_ids = [e["entity_id"] for e in task_creates]

        assert task1_id not in entity_ids, "Should exclude task before range"
        assert task2_id in entity_ids, "Should include task within range"
        assert task3_id not in entity_ids, "Should exclude task after range"

        print("✓ Audit combined date range filter test passed")

    def test_audit_date_filter_with_other_filters(self):
        """Test date filters combined with other filters."""
        roadmap = self.test.create_roadmap()

        start_time = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create task
        task_id = self.test.create_task(roadmap, "Test task", "Functional", "Technical", "Criteria")

        time.sleep(0.1)
        mid_time = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Edit task
        self.test.run_cmd([
            "task", "edit", "-r", roadmap, str(task_id),
            "-t", "Updated title"
        ])

        # Test since filter combined with operation filter. The edit above
        # supplied --title, so it is recorded as TASK_TITLE_CHANGE; the generic
        # TASK_UPDATE is LEGACY and no command writes it.
        result = self.test.run_cmd_json([
            "audit", "list", "-r", roadmap,
            "--since", mid_time,
            "-o", "TASK_TITLE_CHANGE"
        ])

        # Should only show TASK_TITLE_CHANGE operations after mid_time
        assert len(result) >= 1
        for entry in result:
            assert entry["operation"] == "TASK_TITLE_CHANGE"
            assert entry["entity_id"] == task_id

        print("✓ Audit date filter with other filters test passed")


class TestAuditDateOnlyForm:
    """Regression cover for rmp task #324.

    `audit list --since/--until` and `audit stats --since/--until` parsed
    through utils.ParseISO8601, which took RFC3339 only, while
    `task list --created-since/--created-until` parsed through the CLI's
    date-filter parser, which took RFC3339 OR a bare calendar date. One
    published flag type, two acceptance rules, and the narrower one was the
    outlier: SPEC/COMMANDS.md calls `--since` an "ISO 8601 date" (a bare
    calendar date is one), the machine-readable contract publishes
    `rmp audit stats -r myproject --since 2026-01-01 --until 2026-01-31` as a
    working EXAMPLE, `rmp audit list --help` prints "date-only also accepted",
    and README.md writes the date-only form on three lines.

    These tests fail if either audit filter narrows back. Each asserts three
    things, and they fail for different reasons:

      1. the date-only form is ACCEPTED (exit 0), which is the narrowing itself;
      2. it selects EXACTLY what its RFC3339 midnight twin selects, because a
         form that is accepted and then read as some other instant filters
         wrongly at exit 0, where no exit code reveals it;
      3. a bound one day the other side of the entries selects nothing, so
         assertions 1 and 2 are not being satisfied by a filter that is parsed
         and then ignored.
    """

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def _seed(self):
        """Create a roadmap holding two audit entries, and return it with
        today's and tomorrow's calendar dates in UTC."""
        roadmap = self.test.create_roadmap()
        self.test.create_task(roadmap, "Widen the audit date filters",
                              "The published surfaces promise the date-only form",
                              "Route --since/--until through the shared parser",
                              "Both forms accepted and equivalent")
        self.test.create_task(roadmap, "Publish the widened acceptance rule",
                              "The specification must state the accepted forms",
                              "Add the date rows to the audit tables",
                              "The published rejection string matches the binary")
        now = datetime.now(timezone.utc)
        today = now.strftime("%Y-%m-%d")
        tomorrow = (now + timedelta(days=1)).strftime("%Y-%m-%d")
        return roadmap, today, tomorrow

    def _ids(self, entries):
        return sorted(e["id"] for e in entries)

    def test_audit_list_since_accepts_the_date_only_form(self):
        """`audit list --since 2026-03-20`: accepted, and it means midnight."""
        roadmap, today, tomorrow = self._seed()

        code, stdout, stderr = self.test.run_cmd(
            ["audit", "list", "-r", roadmap, "--since", today], check=False)
        assert code == 0, (
            "`audit list --since <date>` was refused; this is task #324 "
            f"returning. exit={code} stderr={stderr}")

        date_only = json.loads(stdout)
        twin = self.test.run_cmd_json(
            ["audit", "list", "-r", roadmap, "--since", today + "T00:00:00.000Z"])
        assert self._ids(date_only) == self._ids(twin), (
            "the date-only bound selected a different set from its RFC3339 "
            "midnight twin, so it is accepted but read as another instant: "
            f"{self._ids(date_only)} vs {self._ids(twin)}")
        assert len(date_only) >= 2, (
            "the seeded entries were not selected, so the comparison below "
            f"proves nothing: {date_only}")

        empty = self.test.run_cmd_json(
            ["audit", "list", "-r", roadmap, "--since", tomorrow])
        assert empty == [], (
            "a lower bound one day after every entry still selected entries, "
            f"so the filter is parsed but not applied: {empty}")

        print("[OK] audit list --since accepts the date-only form (#324)")

    def test_audit_list_until_accepts_the_date_only_form(self):
        """`audit list --until 2026-03-31`: accepted, and it means midnight."""
        roadmap, today, tomorrow = self._seed()

        code, stdout, stderr = self.test.run_cmd(
            ["audit", "list", "-r", roadmap, "--until", tomorrow], check=False)
        assert code == 0, (
            "`audit list --until <date>` was refused; this is task #324 "
            f"returning. exit={code} stderr={stderr}")

        date_only = json.loads(stdout)
        twin = self.test.run_cmd_json(
            ["audit", "list", "-r", roadmap, "--until", tomorrow + "T00:00:00.000Z"])
        assert self._ids(date_only) == self._ids(twin), (
            "the date-only bound selected a different set from its RFC3339 "
            f"midnight twin: {self._ids(date_only)} vs {self._ids(twin)}")
        assert len(date_only) >= 2, (
            f"the seeded entries were not selected: {date_only}")

        # Today's midnight is BEFORE every entry seeded moments ago, which is
        # also the assertion that a bare date means the START of its day.
        empty = self.test.run_cmd_json(
            ["audit", "list", "-r", roadmap, "--until", today])
        assert empty == [], (
            "an upper bound at the start of today selected entries stamped "
            "later today, so the bare date is not being read as midnight: "
            f"{empty}")

        print("[OK] audit list --until accepts the date-only form (#324)")

    def test_audit_stats_accepts_the_date_only_form(self):
        """`audit stats --since/--until <date>`: the contract's own example."""
        roadmap, today, tomorrow = self._seed()

        code, stdout, stderr = self.test.run_cmd(
            ["audit", "stats", "-r", roadmap, "--since", today, "--until", tomorrow],
            check=False)
        assert code == 0, (
            "`audit stats --since <date> --until <date>` was refused, and it is "
            "the invocation the machine-readable contract publishes as a "
            f"working example. exit={code} stderr={stderr}")

        date_only = json.loads(stdout)
        twin = self.test.run_cmd_json([
            "audit", "stats", "-r", roadmap,
            "--since", today + "T00:00:00.000Z",
            "--until", tomorrow + "T00:00:00.000Z",
        ])
        assert date_only == twin, (
            "the date-only window aggregated a different set from its RFC3339 "
            f"twin: {date_only} vs {twin}")
        assert date_only["total_entries"] >= 2, (
            f"the seeded entries were not aggregated: {date_only}")

        empty = self.test.run_cmd_json(
            ["audit", "stats", "-r", roadmap, "--since", tomorrow])
        assert empty["total_entries"] == 0, (
            "a window starting after every entry still aggregated entries: "
            f"{empty}")
        assert empty["first_entry_at"] is None and empty["last_entry_at"] is None

        print("[OK] audit stats accepts the date-only form (#324)")

    def test_audit_still_accepts_every_timestamp_form(self):
        """Widening must take nothing away: every form accepted before #324
        is still accepted, and still selects the same entries."""
        roadmap, today, _ = self._seed()

        forms = [
            today + "T00:00:00.000Z",
            today + "T00:00:00Z",
            today + "T00:00:00.000000Z",
            today + "T00:00:00+00:00",
        ]
        baseline = None
        for form in forms:
            code, stdout, stderr = self.test.run_cmd(
                ["audit", "list", "-r", roadmap, "--since", form], check=False)
            assert code == 0, (
                f"`audit list --since {form}` was refused; this form was "
                f"accepted before #324 and must remain so. stderr={stderr}")
            ids = self._ids(json.loads(stdout))
            if baseline is None:
                baseline = ids
                assert len(baseline) >= 2, (
                    f"no entry was selected, so this test proves nothing: {ids}")
            assert ids == baseline, (
                f"`--since {form}` selected {ids}, the other timestamp forms "
                f"selected {baseline}")

        print("[OK] audit still accepts every pre-#324 timestamp form")

    def test_audit_still_refuses_what_is_not_a_date(self):
        """The refusal path is unchanged in kind: exit 6, and the message now
        names the flag it refused, which the pre-#324 message did not."""
        expected = (
            'Error: validation error: {flag}: invalid date format: expected '
            'RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01): "{value}"'
        )
        roadmap = self.test.create_roadmap()

        cases = [
            ("list", "--since", "not-a-date"),
            ("list", "--until", "24/05/2026"),
            ("stats", "--since", "2026-13-01"),
            ("stats", "--until", "2026-02-30"),
        ]
        for subcommand, flag, value in cases:
            code, stdout, stderr = self.test.run_cmd(
                ["audit", subcommand, "-r", roadmap, flag, value], check=False)
            assert code == 6, (
                f"`audit {subcommand} {flag} {value}` exited {code}, want 6: a "
                "value that is not a date is a validation failure")
            assert stdout.strip() == "", (
                f"a refused invocation wrote to stdout: {stdout!r}")
            first = stderr.splitlines()[0]
            assert first == expected.format(flag=flag, value=value), (
                f"the published refusal has moved:\n  got:  {first}\n"
                f"  want: {expected.format(flag=flag, value=value)}")

        print("[OK] audit still refuses a non-date with exit 6 (#324)")


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
