#!/usr/bin/env python3
"""
Test 23: Backlog Management
Covers `rmp backlog list` and `rmp backlog show-next` per SPEC/COMMANDS.md.

Validates:
  - backlog list returns only BACKLOG-status tasks
  - filtering by --priority, --type
  - --sort across all valid orderings
  - --limit bounds and clamping
  - aliases (ls)
  - show-next default count of 5, explicit count, priority ordering
  - error paths: invalid sort, invalid limit, invalid count
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestBacklogList:
    """rmp backlog list — listing BACKLOG-status tasks with filters."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        self._seed_tasks()

    def teardown_method(self):
        self.test.teardown()

    def _seed_tasks(self):
        """Create a representative spread of backlog tasks."""
        # Three BACKLOG tasks at different priorities/types
        self.t_high = self.test.create_task(
            self.roadmap,
            title="Investigate authentication token leak in production logs",
            functional_requirements="Auth tokens are appearing in /var/log/auth.log when login fails",
            technical_requirements="Add redaction filter to log middleware; rotate exposed tokens",
            acceptance_criteria="No tokens visible in logs across 100 simulated failed logins",
            priority=8,
            severity=7,
        )
        self.t_mid = self.test.create_task(
            self.roadmap,
            title="Migrate user preferences endpoint to v2 schema",
            functional_requirements="The v1 schema lacks the timezone field",
            technical_requirements="Add migrations, dual-write during cutover, deprecate v1 after 30 days",
            acceptance_criteria="Both v1 and v2 return the same row; v2 also returns timezone",
            priority=5,
            severity=3,
        )
        self.t_low = self.test.create_task(
            self.roadmap,
            title="Polish onboarding tooltip copy",
            functional_requirements="The welcome tooltip uses internal jargon",
            technical_requirements="Update i18n strings in en.json and pt-PT.json",
            acceptance_criteria="Tooltip text is approved by product",
            priority=2,
            severity=1,
        )
        # One task moved to SPRINT so we can verify it never shows up in backlog list
        sprint_id = self.test.create_sprint(self.roadmap, "Active sprint for filter test")
        self.t_in_sprint = self.test.create_task(
            self.roadmap,
            title="Roll out feature flag for dark mode beta",
            functional_requirements="Dark mode is gated for the beta cohort only",
            technical_requirements="LaunchDarkly rule keyed on cohort_id",
            acceptance_criteria="Beta cohort sees dark theme; others see light",
            priority=7,
        )
        self.test.move_task_to_sprint(self.roadmap, self.t_in_sprint, sprint_id)

    def test_backlog_list_returns_only_backlog_tasks(self):
        """backlog list excludes tasks in SPRINT/DOING/TESTING/COMPLETED."""
        result = self.test.run_cmd_json(["backlog", "list", "-r", self.roadmap])
        ids = {t["id"] for t in result}
        assert ids == {self.t_high, self.t_mid, self.t_low}, (
            f"Backlog must include only BACKLOG tasks; got ids={ids}"
        )
        for t in result:
            assert t["status"] == "BACKLOG", f"non-BACKLOG task leaked: {t}"

        print("✓ backlog list returns only BACKLOG-status tasks")

    def test_backlog_list_ls_alias(self):
        """`backlog ls` is equivalent to `backlog list`."""
        long_form = self.test.run_cmd_json(["backlog", "list", "-r", self.roadmap])
        alias = self.test.run_cmd_json(["backlog", "ls", "-r", self.roadmap])
        assert sorted(t["id"] for t in long_form) == sorted(t["id"] for t in alias)

        print("✓ backlog ls alias matches backlog list")

    def test_backlog_list_priority_filter(self):
        """--priority N keeps only tasks with priority >= N."""
        result = self.test.run_cmd_json(
            ["backlog", "list", "-r", self.roadmap, "--priority", "5"]
        )
        ids = {t["id"] for t in result}
        assert ids == {self.t_high, self.t_mid}, (
            f"Priority>=5 filter must keep t_high (8) and t_mid (5); got {ids}"
        )

        print("✓ --priority filters by minimum value")

    def test_backlog_list_type_filter(self):
        """--type TASK keeps only TASK-typed tasks (the default in this suite)."""
        # All seeded tasks default to TASK type; create a BUG to distinguish.
        bug_id = self.test.create_task(
            self.roadmap,
            title="Order summary shows wrong currency for euro region",
            functional_requirements="Carts in EU display USD",
            technical_requirements="Locale resolution must consult billing_country, not browser locale",
            acceptance_criteria="EU carts show EUR with 2 decimals",
            priority=6,
        )
        # Re-type to BUG via edit
        self.test.run_cmd(
            ["task", "edit", "-r", self.roadmap, str(bug_id), "--type", "BUG"]
        )

        bugs = self.test.run_cmd_json(
            ["backlog", "list", "-r", self.roadmap, "--type", "BUG"]
        )
        assert len(bugs) == 1 and bugs[0]["id"] == bug_id
        assert bugs[0]["type"] == "BUG"

        print("✓ --type filters by task type")

    def test_backlog_list_sort_priority_default(self):
        """Default sort is priority DESC, then created_at ASC."""
        result = self.test.run_cmd_json(["backlog", "list", "-r", self.roadmap])
        priorities = [t["priority"] for t in result]
        assert priorities == sorted(priorities, reverse=True), (
            f"Default order must be priority DESC; got {priorities}"
        )

        print("✓ default sort is priority DESC")

    def test_backlog_list_sort_created(self):
        """--sort created orders by created_at ASC."""
        result = self.test.run_cmd_json(
            ["backlog", "list", "-r", self.roadmap, "--sort", "created"]
        )
        created = [t["created_at"] for t in result]
        assert created == sorted(created), (
            f"--sort created must produce ascending created_at; got {created}"
        )

        print("✓ --sort created orders ascending")

    def test_backlog_list_sort_severity(self):
        """--sort severity orders by severity DESC."""
        result = self.test.run_cmd_json(
            ["backlog", "list", "-r", self.roadmap, "--sort", "severity"]
        )
        severities = [t["severity"] for t in result]
        assert severities == sorted(severities, reverse=True)

        print("✓ --sort severity orders DESC")

    def test_backlog_list_invalid_sort_rejected(self):
        """Unknown --sort value is rejected with exit 6."""
        exit_code, _, stderr = self.test.run_cmd(
            ["backlog", "list", "-r", self.roadmap, "--sort", "alphabetical"],
            check=False,
        )
        assert exit_code == 6, f"invalid sort must exit 6, got {exit_code}; stderr={stderr}"
        assert "sort" in stderr.lower()

        print("✓ invalid --sort rejected with exit 6")

    def test_backlog_list_limit(self):
        """--limit N caps the result count."""
        result = self.test.run_cmd_json(
            ["backlog", "list", "-r", self.roadmap, "--limit", "2"]
        )
        assert len(result) == 2

        print("✓ --limit caps result count")

    def test_backlog_list_limit_out_of_range_rejected(self):
        """--limit 0 or > MaxTaskLimit is rejected."""
        exit_code, _, _ = self.test.run_cmd(
            ["backlog", "list", "-r", self.roadmap, "--limit", "0"],
            check=False,
        )
        assert exit_code == 6, "--limit 0 must exit 6"

        exit_code, _, _ = self.test.run_cmd(
            ["backlog", "list", "-r", self.roadmap, "--limit", "10000"],
            check=False,
        )
        assert exit_code == 6, "--limit beyond MaxTaskLimit must exit 6"

        print("✓ --limit out of range rejected")

    def test_backlog_list_combined_filters(self):
        """--priority and --type compose correctly."""
        bug_high = self.test.create_task(
            self.roadmap,
            title="Critical: payment webhook drops events under load",
            functional_requirements="Webhooks from Stripe are not always processed during high traffic",
            technical_requirements="Add idempotency, retry queue, and DLQ",
            acceptance_criteria="No webhook drops in 60-min load test at 500 rps",
            priority=9,
            severity=9,
        )
        self.test.run_cmd(
            ["task", "edit", "-r", self.roadmap, str(bug_high), "--type", "BUG"]
        )

        result = self.test.run_cmd_json([
            "backlog", "list", "-r", self.roadmap,
            "--type", "BUG", "--priority", "8",
        ])
        ids = {t["id"] for t in result}
        assert ids == {bug_high}, f"composed filter should keep only bug_high; got {ids}"

        print("✓ combined --type + --priority filters compose")


class TestBacklogShowNext:
    """rmp backlog show-next — sprint planning shortcut."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        self.task_ids = []
        # Seven backlog tasks with distinct priorities so ordering is unambiguous.
        priorities = [9, 8, 7, 5, 4, 2, 1]
        for i, p in enumerate(priorities):
            tid = self.test.create_task(
                self.roadmap,
                title=f"Sprint candidate #{i+1}: deliverable for priority-{p} initiative",
                functional_requirements=f"Initiative {i+1} requires a self-contained change",
                technical_requirements=f"Implement module-{i+1} per the design doc",
                acceptance_criteria=f"All acceptance scenarios from spec-{i+1} pass",
                priority=p,
            )
            self.task_ids.append(tid)

    def teardown_method(self):
        self.test.teardown()

    def test_show_next_default_returns_top_5(self):
        """No argument => default count of 5 per SPEC."""
        result = self.test.run_cmd_json(["backlog", "show-next", "-r", self.roadmap])
        assert len(result) == 5, f"default count must be 5, got {len(result)}"

        print("✓ show-next defaults to 5")

    def test_show_next_priority_ordering(self):
        """Top N comes back priority DESC."""
        result = self.test.run_cmd_json(["backlog", "show-next", "-r", self.roadmap, "3"])
        priorities = [t["priority"] for t in result]
        assert priorities == [9, 8, 7], f"expected [9,8,7], got {priorities}"

        print("✓ show-next returns highest priorities in order")

    def test_show_next_explicit_count(self):
        """Explicit count is respected when within bounds."""
        result = self.test.run_cmd_json(["backlog", "show-next", "-r", self.roadmap, "2"])
        assert len(result) == 2

        result = self.test.run_cmd_json(["backlog", "show-next", "-r", self.roadmap, "7"])
        assert len(result) == 7

        print("✓ show-next honours explicit count")

    def test_show_next_count_clamped_to_available(self):
        """Asking for more than exists returns everything in backlog."""
        result = self.test.run_cmd_json(["backlog", "show-next", "-r", self.roadmap, "100"])
        assert len(result) == 7, f"only 7 backlog tasks exist; got {len(result)}"

        print("✓ show-next clamps to available task count")

    def test_show_next_invalid_count_rejected(self):
        """Non-numeric or non-positive count fails with exit 6."""
        for bad in ["0", "-1", "abc", "1.5"]:
            exit_code, _, _ = self.test.run_cmd(
                ["backlog", "show-next", "-r", self.roadmap, bad],
                check=False,
            )
            assert exit_code == 6, f"show-next {bad!r} must exit 6, got {exit_code}"

        print("✓ show-next rejects invalid count with exit 6")

    def test_show_next_excludes_non_backlog(self):
        """Tasks moved out of BACKLOG must not appear in show-next output."""
        sprint_id = self.test.create_sprint(self.roadmap, "Move-out target sprint")
        # Move the top-priority task out
        self.test.move_task_to_sprint(self.roadmap, self.task_ids[0], sprint_id)

        result = self.test.run_cmd_json(["backlog", "show-next", "-r", self.roadmap, "3"])
        ids = [t["id"] for t in result]
        assert self.task_ids[0] not in ids, "task moved to sprint should not appear in backlog"
        # The next-highest (priority 8) should now lead.
        assert result[0]["priority"] == 8

        print("✓ show-next excludes tasks moved out of BACKLOG")


def main():
    """Run all backlog management tests."""
    import inspect

    failures = []
    passed = 0
    for cls_name, cls in [
        ("TestBacklogList", TestBacklogList),
        ("TestBacklogShowNext", TestBacklogShowNext),
    ]:
        for meth_name, meth in inspect.getmembers(cls, predicate=inspect.isfunction):
            if not meth_name.startswith("test_"):
                continue
            inst = cls()
            if hasattr(inst, "setup_method"):
                inst.setup_method()
            try:
                meth(inst)
                passed += 1
            except Exception as e:
                failures.append(f"{cls_name}.{meth_name}: {e}")
            if hasattr(inst, "teardown_method"):
                try:
                    inst.teardown_method()
                except Exception:
                    pass

    print(f"\n{passed} passed, {len(failures)} failed")
    for f in failures:
        print(f"  ✗ {f}")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
