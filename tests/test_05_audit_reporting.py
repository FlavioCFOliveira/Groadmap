#!/usr/bin/env python3
"""
Test 05: Audit and Reporting
Tests audit log functionality and various reporting scenarios.
"""

import sys
import os
from datetime import datetime, timezone
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase, commit_flags_for


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
            roadmap, "Test task", "Functional", "Technical", "Criteria"
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
        """Test that audit log names the field each edit supplied.

        `task edit -t` writes TASK_TITLE_CHANGE, not the generic TASK_UPDATE:
        that operation is LEGACY and no command writes it any more
        (SPEC/COMMANDS.md § Edit Task).
        """
        roadmap = self.test.create_roadmap()

        # Create and modify task
        task_id = self.test.create_task(roadmap, "Task", "Functional", "Technical", "Criteria")

        self.test.run_cmd([
            "task", "edit", "-r", roadmap, str(task_id),
            "-t", "Updated title"
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
        assert "TASK_TITLE_CHANGE" in operations
        assert "TASK_PRIORITY_CHANGE" in operations
        assert "TASK_SEVERITY_CHANGE" in operations
        assert "TASK_UPDATE" not in operations, (
            f"TASK_UPDATE is LEGACY and must never be written; got: {operations}"
        )

        print("✓ Audit log tracks task updates test passed")

    def test_audit_log_tracks_status_changes(self):
        """Test that audit log tracks task status changes."""
        roadmap = self.test.create_roadmap()

        task_id = self.test.create_task(roadmap, "Task", "Functional", "Technical", "Criteria")
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")

        # Add to sprint (changes status to SPRINT)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])

        # Change status
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING", "--commit-open", "5d6a2cd"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED", "--commit-close", "8256fd0"])

        # Check audit log
        result = self.test.run_cmd_json(["audit", "list", "-r", roadmap])

        # An entry names the state the task ENTERED, so the three transitions
        # above leave three different operations rather than three copies of one
        # (SPEC/DATABASE.md - One Row per Thing That Happened).
        operations = [e["operation"] for e in result]
        for entered in ("TASK_STATUS_DOING", "TASK_STATUS_TESTING", "TASK_STATUS_COMPLETED"):
            assert entered in operations, f"{entered} missing from {operations}"

        # TASK_STATUS_CHANGE is LEGACY: readable, so the rows a pre-1.12.0
        # binary wrote stay reachable by name, but never written again.
        assert "TASK_STATUS_CHANGE" not in operations

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
        task1 = self.test.create_task(roadmap, "Task 1", "Functional", "Technical", "Criteria")
        task2 = self.test.create_task(roadmap, "Task 2", "Functional", "Technical", "Criteria")
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
        task_id = self.test.create_task(roadmap, "Task", "Functional", "Technical", "Criteria")
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
        task_id = self.test.create_task(roadmap, "Task", "Functional", "Technical", "Criteria")
        self.test.run_cmd(["task", "prio", "-r", roadmap, str(task_id), "5"])
        sprint_id = self.test.create_sprint(roadmap, "Sprint")

        # Get stats
        result = self.test.run_cmd_json(["audit", "stats", "-r", roadmap])

        assert "total_entries" in result
        assert "by_operation" in result
        assert result["total_entries"] >= 3

        # With entries present, timestamps must be real ISO 8601 UTC strings,
        # not null and not empty.
        assert result["first_entry_at"] is not None
        assert result["last_entry_at"] is not None
        assert isinstance(result["first_entry_at"], str) and result["first_entry_at"] != ""
        assert isinstance(result["last_entry_at"], str) and result["last_entry_at"] != ""
        assert result["first_entry_at"] <= result["last_entry_at"]

        print("✓ Audit stats test passed")

    def test_audit_stats_empty_log_null_timestamps(self):
        """Regression: empty audit stats must emit JSON null (not "") for
        first_entry_at/last_entry_at per SPEC/COMMANDS.md and DATA_FORMATS.md."""
        roadmap = self.test.create_roadmap()

        # No activity beyond roadmap creation; restrict the window to a past
        # range guaranteed to contain no entries so the result set is empty.
        result = self.test.run_cmd_json([
            "audit", "stats", "-r", roadmap,
            "--since", "2000-01-01T00:00:00.000Z",
            "--until", "2000-01-02T00:00:00.000Z",
        ])

        assert result["total_entries"] == 0
        # Keys must be present and explicitly null (Python None), never "".
        assert "first_entry_at" in result
        assert "last_entry_at" in result
        assert result["first_entry_at"] is None
        assert result["last_entry_at"] is None

        print("✓ Audit stats empty-log null timestamps test passed")

    def test_audit_date_range_filter(self):
        """Test audit log date range filtering."""
        roadmap = self.test.create_roadmap()

        # Create task
        task_id = self.test.create_task(roadmap, "Task", "Functional", "Technical", "Criteria")

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
            roadmap, "Task", "Functional", "Technical", "Criteria", priority=0, severity=0
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
        low = self.test.create_task(roadmap, "Low", "Action", "Technical", "Result", priority=2)
        medium = self.test.create_task(roadmap, "Medium", "Action", "Technical", "Result", priority=5)
        high = self.test.create_task(roadmap, "High", "Action", "Technical", "Result", priority=8)
        critical = self.test.create_task(roadmap, "Critical", "Action", "Technical", "Result", priority=9)

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
        low = self.test.create_task(roadmap, "Low", "Action", "Technical", "Result", severity=1)
        medium = self.test.create_task(roadmap, "Medium", "Action", "Technical", "Result", severity=4)
        high = self.test.create_task(roadmap, "High", "Action", "Technical", "Result", severity=7)
        critical = self.test.create_task(roadmap, "Critical", "Action", "Technical", "Result", severity=9)

        # Filter by severity
        result = self.test.list_tasks(roadmap, severity=4)
        ids = [t["id"] for t in result]
        assert medium in ids
        assert high in ids
        assert critical in ids
        assert low not in ids

        print("✓ List tasks by severity test passed")


    # ------------------------------------------------------------------
    # Acceptance criterion 3 of rmp task #266: the published side effects of
    # the six mutating subcommands name the operations each one writes.
    #
    # This is the EMPIRICAL half of that criterion. The Go gate in
    # internal/commands/registry_audit_side_effects_test.go checks that the
    # published text names a declared set of operations; it cannot observe a
    # write, so a set that is merely plausible would satisfy it. Here the
    # commands are actually run against the compiled binary, the rows they
    # produce are read back out of the audit log, and every operation OBSERVED
    # must appear in the text the contract publishes for that subcommand.
    # ------------------------------------------------------------------

    def _ai_help_side_effects(self, contract, family, subcommand):
        """Return side_effects.database for one subcommand of the contract."""
        for cmd in contract["commands"]:
            if cmd["name"] != family:
                continue
            for sub in cmd["subcommands"]:
                if sub["name"] == subcommand:
                    return sub["side_effects"]["database"]
            raise AssertionError(
                f"`{family} {subcommand}` is absent from the --ai-help contract"
            )
        raise AssertionError(f"command family `{family}` is absent from the --ai-help contract")

    def _audit_ids(self, roadmap):
        """Return the set of audit entry ids currently recorded for a roadmap."""
        rows = self.test.run_cmd_json(["audit", "list", "-r", roadmap, "-l", "500"])
        return {row["id"] for row in rows}

    def _operations_written_by(self, roadmap, run):
        """Run `run` and return the operations of the audit rows it added.

        Attribution is by entry id rather than by timestamp: several entries of
        one invocation deliberately share a single performed_at, and a
        timestamp window would also sweep in rows written by the setup.
        """
        before = self._audit_ids(roadmap)
        run()
        rows = self.test.run_cmd_json(["audit", "list", "-r", roadmap, "-l", "500"])
        added = [row for row in rows if row["id"] not in before]
        assert added, "the invocation added no audit entry at all, so this case observes nothing"
        return {row["operation"] for row in added}

    def test_audit_side_effects_name_every_operation_actually_written(self):
        """The six mutating subcommands publish the operations they really write.

        An agent reads side_effects.database to learn what a write did. Saying
        "audit log" tells it that a row appeared without telling it which
        `audit list --operation` filter finds the row again, which is the only
        thing it can do with the knowledge.
        """
        roadmap = self.test.create_roadmap()
        contract = self.test.run_cmd_json(["--ai-help"])

        observed = {}

        # --- task edit: one operation per supplied field ---------------------
        edit_task = self.test.create_task(
            roadmap, "Harden the audit writer", "Operators need a trustworthy log",
            "Wrap every write in the transaction that performs it",
            "Every mutation has exactly one entry"
        )
        observed[("task", "edit")] = self._operations_written_by(
            roadmap,
            lambda: self.test.run_cmd([
                "task", "edit", "-r", roadmap, str(edit_task),
                "-t", "Harden the audit writer and its tests",
                "-tr", "Wrap every write in the transaction that performs it, and prove it",
                "-p", "7",
            ]),
        )

        # --- task stat: one operation per destination status -----------------
        stat_task = self.test.create_task(
            roadmap, "Publish the operation catalogue", "Agents filter on operations",
            "Render the help block from the catalogue", "Both surfaces list all values"
        )
        stat_sprint = self.test.create_sprint(roadmap, "Audit catalogue delivery")
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(stat_sprint), str(stat_task)])

        def walk_the_lifecycle():
            for status in ("DOING", "TESTING", "COMPLETED", "BACKLOG"):
                self.test.run_cmd(
                    ["task", "stat", "-r", roadmap, str(stat_task), status]
                    + commit_flags_for(status)
                )

        observed[("task", "stat")] = self._operations_written_by(roadmap, walk_the_lifecycle)

        # --- sprint update: one operation per supplied field -----------------
        upd_sprint = self.test.create_sprint(roadmap, "Contract enrichment")
        observed[("sprint", "update")] = self._operations_written_by(
            roadmap,
            lambda: self.test.run_cmd([
                "sprint", "update", "-r", roadmap, str(upd_sprint),
                "-t", "Contract enrichment and help parity",
                "-d", "Carry the enriched catalogue into both published surfaces",
            ]),
        )

        # --- sprint add-tasks: the mirrored pair -----------------------------
        add_sprint = self.test.create_sprint(roadmap, "Entity type classification")
        add_task = self.test.create_task(
            roadmap, "Declare the entity type of every operation",
            "The help must not guess whose history a row belongs to",
            "Declare the classification beside the constants",
            "A gate fails on an unclassified value"
        )
        observed[("sprint", "add-tasks")] = self._operations_written_by(
            roadmap,
            lambda: self.test.run_cmd([
                "sprint", "add-tasks", "-r", roadmap, str(add_sprint), str(add_task)
            ]),
        )

        # --- sprint remove-tasks: the mirrored pair back out -----------------
        observed[("sprint", "remove-tasks")] = self._operations_written_by(
            roadmap,
            lambda: self.test.run_cmd([
                "sprint", "remove-tasks", "-r", roadmap, str(add_sprint), str(add_task)
            ]),
        )

        # --- sprint move-tasks: out of one sprint and into another -----------
        move_from = self.test.create_sprint(roadmap, "Source of the move")
        move_to = self.test.create_sprint(roadmap, "Destination of the move")
        move_task = self.test.create_task(
            roadmap, "Name the counterpart entity", "related_entity_id reads as a duplicate",
            "State the rule on both audit help surfaces",
            "Neither surface leaves the key unexplained"
        )
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(move_from), str(move_task)])
        observed[("sprint", "move-tasks")] = self._operations_written_by(
            roadmap,
            lambda: self.test.run_cmd([
                "sprint", "move-tasks", "-r", roadmap,
                str(move_from), str(move_to), str(move_task)
            ]),
        )

        assert len(observed) == 6, (
            f"{len(observed)} subcommands were driven, want the 6 named by the acceptance criterion"
        )

        checked = 0
        for (family, sub), operations in sorted(observed.items()):
            text = self._ai_help_side_effects(contract, family, sub)
            assert operations, f"`{family} {sub}` wrote no audit row, so this case asserts nothing"
            for op in sorted(operations):
                assert op in text, (
                    f"`rmp {family} {sub}` really writes {op} — the row was read back out of the "
                    f"audit log — but the contract's side_effects.database for it never names the "
                    f"operation, so an agent cannot find the row again.\n  text: {text}"
                )
                checked += 1

        assert checked >= 12, (
            f"only {checked} operation observations were made across the six subcommands; four of "
            f"them write two or more operations, so a total this low means the driving stopped "
            f"producing rows and the assertions above ran on empty sets"
        )

        print("✓ Audit side effects name every operation actually written test passed")

    def test_audit_side_effects_do_not_claim_a_legacy_operation(self):
        """No subcommand writes one of the four LEGACY operations.

        The catalogue keeps them accepted so old entries stay filterable. If a
        command started writing one again, the help would be telling readers to
        stop filtering on a value that is still being recorded.
        """
        legacy = {"TASK_STATUS_CHANGE", "TASK_UPDATE", "SPRINT_UPDATE", "SPRINT_MOVE_TASK"}

        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap, "Exercise every writer", "The legacy set must stay empty",
            "Drive the mutating subcommands", "No legacy operation is recorded"
        )
        sprint_a = self.test.create_sprint(roadmap, "First sprint of the sweep")
        sprint_b = self.test.create_sprint(roadmap, "Second sprint of the sweep")

        self.test.run_cmd(["task", "edit", "-r", roadmap, str(task_id), "-t", "Exercise every writer twice"])
        self.test.run_cmd([
            "sprint", "update", "-r", roadmap, str(sprint_a),
            "-d", "First sprint of the sweep, restated",
        ])
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_a), str(task_id)])
        self.test.run_cmd([
            "sprint", "move-tasks", "-r", roadmap, str(sprint_a), str(sprint_b), str(task_id)
        ])
        self.test.run_cmd(["sprint", "remove-tasks", "-r", roadmap, str(sprint_b), str(task_id)])

        rows = self.test.run_cmd_json(["audit", "list", "-r", roadmap, "-l", "500"])
        assert len(rows) >= 8, f"only {len(rows)} audit rows were written, so the sweep did not run"

        written = {row["operation"] for row in rows}
        offenders = sorted(written & legacy)
        assert not offenders, (
            f"the sweep recorded LEGACY operation(s) {offenders}; both published surfaces state "
            f"that no command writes them, so either a writer was reinstated or the LEGACY marking "
            f"is now false"
        )

        # Non-vacuity: each LEGACY value must still be an ACCEPTED filter, which
        # is the whole reason it stays in the catalogue.
        for op in sorted(legacy):
            code, _, _ = self.test.run_cmd(
                ["audit", "list", "-r", roadmap, "-o", op], check=False
            )
            assert code == 0, (
                f"`audit list --operation {op}` exited {code}; the value is published as an accepted "
                f"filter that returns the older entries carrying it"
            )

        print("✓ Audit side effects do not claim a legacy operation test passed")


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
