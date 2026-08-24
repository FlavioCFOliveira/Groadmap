#!/usr/bin/env python3
"""
Test 54: The enriched audit contract, end to end against the compiled binary
(rmp task #268).

v1.15.0's audit change touches every layer between a schema migration and the
JSON a caller parses: the `audit` table gains `related_entity_id` and
`commit_hash`, the operation catalogue splits `TASK_STATUS_CHANGE` into five
destination-specific operations and `SPRINT_MOVE_TASK` into a directional pair,
`sprint update` and `task edit` now write one row per field instead of one
generic row, and the `1.11.0` to `1.12.0` migration reclassifies exactly the
historical rows the stored data determines without guessing at the rest
(SPEC/DATABASE.md, SPEC/VERSION.md § Migration 1.11.0 to 1.12.0). Unit tests in
internal/db already gate the migration's SQL in isolation
(internal/db/migration_audit_columns_test.go); what is missing, and what this
module supplies, is proof that the SQL layer, the Go model layer, and the JSON
a consumer reads all agree once a real user drives them through ./bin/rmp.

Coverage map (SPEC references inline at each class):
  TestFiveStatusTransitionsWriteTheirOwnOperation  -- the five TASK_STATUS_*
    operations and the commit hash rule (DOING/COMPLETED only)
  TestRelationalOperationsNameBothEntities         -- the mirrored pairs,
    including the SPRINT_MOVE_TASK_OUT/IN move pair, and the full seven-key
    JSON shape on real relational rows
  TestOneRowPerChangedFieldOnEdit                  -- task edit / sprint update
  TestAuditListFiltersEveryOperationAndLegacyValues -- audit list -o over the
    WHOLE catalogue, fetched live from --ai-help rather than hardcoded, so a
    future addition to the catalogue is covered automatically
  TestAuditStatsCountsNewOperations                -- audit stats by_operation
  TestMigration1_11_0FourRowClasses                -- the fixture migration,
    the module's centre of gravity
  TestNonVacuityProofs                             -- for four ways this
    module's own assertions could pass against a broken product, injects the
    defect, shows the assertion fail, and reverts it

Every assertion reads actual field values out of real JSON responses (or, for
the migration and the non-vacuity proofs, out of the SQLite file directly) --
never an exit code alone.
"""

import json
import os
import sqlite3
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase  # noqa: E402


EXIT_OK = 0
EXIT_MISUSE = 2
EXIT_INVALID = 6

# The seven keys DATA_FORMATS.md § Audit Entry requires on every entry, no
# more and no fewer.
AUDIT_ENTRY_KEYS = frozenset({
    "id", "operation", "entity_type", "entity_id",
    "related_entity_id", "commit_hash", "performed_at",
})

# The two operations DATABASE.md § The Commit Hash of an Audit Entry names as
# the only carriers of commit_hash.
COMMIT_CARRYING_OPERATIONS = frozenset({"TASK_STATUS_DOING", "TASK_STATUS_COMPLETED"})

# Realistic, well-formed commit hashes (this repository's own short SHA-1
# form), distinct per use so a test that mixes them up is caught by value.
OPEN_HASH_1 = "9c41af0"
OPEN_HASH_2 = "d206b57"
CLOSE_HASH_1 = "47eaa19"
CLOSE_HASH_2 = "1bf90ce"
INJECTED_HASH = "0af5e21"


def _first_line(text: str) -> str:
    return text.strip().splitlines()[0] if text.strip() else ""


def assert_entry_shape(entry: dict, context: str = "") -> None:
    """The seven-key JSON contract of DATA_FORMATS.md § Audit Entry, with
    types. Shared by every class below that reads a real entry."""
    got = set(entry.keys())
    assert got == AUDIT_ENTRY_KEYS, (
        f"{context}: audit entry keys are {sorted(got)}, want exactly "
        f"{sorted(AUDIT_ENTRY_KEYS)} (SPEC/DATA_FORMATS.md § Audit Entry)"
    )
    assert isinstance(entry["id"], int), f"{context}: id is not an int: {entry['id']!r}"
    assert isinstance(entry["operation"], str) and entry["operation"], (
        f"{context}: operation is not a non-empty string: {entry['operation']!r}"
    )
    assert entry["entity_type"] in ("TASK", "SPRINT"), (
        f"{context}: entity_type is {entry['entity_type']!r}, want TASK or SPRINT"
    )
    assert isinstance(entry["entity_id"], int) and entry["entity_id"] > 0, (
        f"{context}: entity_id is not a positive int: {entry['entity_id']!r}"
    )
    assert entry["related_entity_id"] is None or (
        isinstance(entry["related_entity_id"], int) and entry["related_entity_id"] > 0
    ), f"{context}: related_entity_id is neither null nor a positive int: {entry['related_entity_id']!r}"
    assert entry["commit_hash"] is None or (
        isinstance(entry["commit_hash"], str) and 7 <= len(entry["commit_hash"]) <= 64
    ), f"{context}: commit_hash is neither null nor a 7..64-char string: {entry['commit_hash']!r}"
    assert isinstance(entry["performed_at"], str) and entry["performed_at"], (
        f"{context}: performed_at is not a non-empty string: {entry['performed_at']!r}"
    )


class TestFiveStatusTransitionsWriteTheirOwnOperation:
    """SPEC/DATABASE.md § `audit` Table (the five TASK_STATUS_* operations) and
    § The Commit Hash of an Audit Entry (DOING and COMPLETED only, NULL
    elsewhere, including on TASK_REOPEN)."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("customs-manifest-reconciliation")
        self.sprint_id = self.test.create_sprint(
            self.roadmap,
            "Retire the manual manifest reconciliation spreadsheet before the Q3 audit.",
            title="Manifest reconciliation programme",
        )

    def teardown_method(self):
        self.test.teardown()

    # -- fixture helpers -----------------------------------------------

    def _new_task(self, title: str) -> int:
        return self.test.create_task(
            self.roadmap,
            title=title,
            functional_requirements="Every consignment manifest reconciles against the carrier's own filing.",
            technical_requirements="Stream each manifest line against the customs declaration index.",
            acceptance_criteria="A mismatched HS code raises a discrepancy record within the hour.",
        )

    def _newest_row(self, task_id: int) -> dict:
        history = self.test.run_cmd_json(
            ["audit", "history", "-r", self.roadmap, "TASK", str(task_id)]
        )
        assert history, f"task {task_id} has no audit history at all"
        return history[0]  # newest first (performed_at DESC)

    # -- BACKLOG ---------------------------------------------------------

    def test_backlog_via_task_stat_writes_task_status_backlog_with_no_counterpart(self):
        task_id = self._new_task("Validate consignment weight against the carrier manifest")
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id), str(task_id)])

        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(task_id), "BACKLOG"])

        entry = self._newest_row(task_id)
        assert_entry_shape(entry, "BACKLOG via task stat")
        assert entry["operation"] == "TASK_STATUS_BACKLOG", entry
        assert entry["related_entity_id"] is None, (
            "task stat has no sprint counterpart, so related_entity_id must be null "
            "(SPEC/DATABASE.md § The Two Entities of a Relational Operation, rule "
            "'One operation value, two producing commands, one rule')"
        )
        assert entry["commit_hash"] is None
        print("✓ task stat <id> BACKLOG writes TASK_STATUS_BACKLOG with a null counterpart")

    # -- SPRINT ------------------------------------------------------------

    def test_sprint_via_add_tasks_writes_task_status_sprint(self):
        task_id = self._new_task("Cross-check HS tariff codes against the customs declaration")

        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id), str(task_id)])

        entry = self._newest_row(task_id)
        assert_entry_shape(entry, "SPRINT via add-tasks")
        assert entry["operation"] == "TASK_STATUS_SPRINT", entry
        assert entry["related_entity_id"] == self.sprint_id, (
            f"TASK_STATUS_SPRINT must name the sprint the task entered; got {entry['related_entity_id']!r}"
        )
        assert entry["commit_hash"] is None
        task = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(task_id)])[0]
        assert task["status"] == "SPRINT"
        print("✓ sprint add-tasks writes TASK_STATUS_SPRINT naming the sprint")

    # -- DOING ---------------------------------------------------------------

    def test_doing_writes_task_status_doing_with_commit_hash(self):
        task_id = self._new_task("Automate discrepancy alerts for under-declared consignments")
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id), str(task_id)])

        self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, str(task_id), "DOING", "--commit-open", OPEN_HASH_1]
        )

        entry = self._newest_row(task_id)
        assert_entry_shape(entry, "DOING")
        assert entry["operation"] == "TASK_STATUS_DOING", entry
        assert entry["related_entity_id"] is None
        assert entry["commit_hash"] == OPEN_HASH_1, (
            f"TASK_STATUS_DOING must carry the supplied --commit-open value; got {entry['commit_hash']!r}"
        )
        print("✓ task stat DOING writes TASK_STATUS_DOING carrying commit_open")

    # -- TESTING -----------------------------------------------------------

    def test_testing_writes_task_status_testing_with_no_commit_hash(self):
        task_id = self._new_task("Reconcile the weekly non-conformance report against the ledger")
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id), str(task_id)])
        self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, str(task_id), "DOING", "--commit-open", OPEN_HASH_1]
        )

        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(task_id), "TESTING"])

        entry = self._newest_row(task_id)
        assert_entry_shape(entry, "TESTING")
        assert entry["operation"] == "TASK_STATUS_TESTING", entry
        assert entry["related_entity_id"] is None
        assert entry["commit_hash"] is None, (
            f"TASK_STATUS_TESTING must never carry a commit_hash; got {entry['commit_hash']!r}"
        )
        print("✓ task stat TESTING writes TASK_STATUS_TESTING with no commit_hash")

    # -- COMPLETED -----------------------------------------------------------

    def test_completed_writes_task_status_completed_with_commit_hash(self):
        task_id = self._new_task("Archive superseded manifest revisions past the retention window")
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id), str(task_id)])
        self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, str(task_id), "DOING", "--commit-open", OPEN_HASH_1]
        )
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(task_id), "TESTING"])

        self.test.run_cmd([
            "task", "stat", "-r", self.roadmap, str(task_id), "COMPLETED",
            "--commit-close", CLOSE_HASH_1, "--summary",
            "Superseded revisions now purge automatically at the retention boundary.",
        ])

        entry = self._newest_row(task_id)
        assert_entry_shape(entry, "COMPLETED")
        assert entry["operation"] == "TASK_STATUS_COMPLETED", entry
        assert entry["related_entity_id"] is None
        assert entry["commit_hash"] == CLOSE_HASH_1, (
            f"TASK_STATUS_COMPLETED must carry the supplied --commit-close value; got {entry['commit_hash']!r}"
        )
        print("✓ task stat COMPLETED writes TASK_STATUS_COMPLETED carrying commit_close")

    # -- TASK_REOPEN is not TASK_STATUS_BACKLOG -----------------------------

    def test_reopen_writes_task_reopen_not_task_status_backlog(self):
        task_id = self._new_task("Rotate the customs broker API credentials")
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id), str(task_id)])
        self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, str(task_id), "DOING", "--commit-open", OPEN_HASH_1]
        )
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(task_id), "TESTING"])
        self.test.run_cmd([
            "task", "stat", "-r", self.roadmap, str(task_id), "COMPLETED",
            "--commit-close", CLOSE_HASH_1, "--summary", "Credentials rotate on a 90-day schedule now.",
        ])

        self.test.run_cmd(["task", "reopen", "-r", self.roadmap, str(task_id)])

        entry = self._newest_row(task_id)
        assert_entry_shape(entry, "task reopen")
        assert entry["operation"] == "TASK_REOPEN", (
            f"task reopen must write TASK_REOPEN alone, never TASK_STATUS_BACKLOG "
            f"(SPEC/DATABASE.md § `audit` Table, TASK_REOPEN); got {entry['operation']!r}"
        )
        assert entry["related_entity_id"] is None
        assert entry["commit_hash"] is None, (
            "TASK_REOPEN must not copy the task's stored commit values onto itself "
            "(SPEC/DATABASE.md § The Commit Hash of an Audit Entry)"
        )

        history = self.test.run_cmd_json(["audit", "history", "-r", self.roadmap, "TASK", str(task_id)])
        backlog_after_reopen = [
            e for e in history
            if e["operation"] == "TASK_STATUS_BACKLOG" and e["id"] > entry["id"]
        ]
        assert backlog_after_reopen == [], (
            "no TASK_STATUS_BACKLOG entry may follow the TASK_REOPEN entry from the same invocation"
        )

        completed_entries = [e for e in history if e["operation"] == "TASK_STATUS_COMPLETED"]
        assert len(completed_entries) == 1 and completed_entries[0]["commit_hash"] == CLOSE_HASH_1, (
            "the earlier TASK_STATUS_COMPLETED entry must survive the reopening with its hash intact "
            "(SPEC/DATABASE.md § The Commit Hash of an Audit Entry, 'The audit row is immutable')"
        )
        print("✓ task reopen writes TASK_REOPEN, never TASK_STATUS_BACKLOG, and carries no commit_hash")


class TestRelationalOperationsNameBothEntities:
    """SPEC/DATABASE.md § The Two Entities of a Relational Operation: every row
    of a mirrored pair carries the OTHER entity's id in related_entity_id, the
    two rows of one invocation share performed_at, and sprint move-tasks writes
    the OUT/IN pair and no TASK_STATUS_* row at all."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("port-authority-berth-scheduling")

    def teardown_method(self):
        self.test.teardown()

    def _new_task(self, title: str) -> int:
        return self.test.create_task(
            self.roadmap,
            title=title,
            functional_requirements="A berth allocation never overlaps two vessels of conflicting draught.",
            technical_requirements="Reserve the berth window transactionally against the tide table.",
            acceptance_criteria="No two confirmed bookings share an overlapping berth window.",
        )

    def _new_sprint(self, description: str) -> int:
        return self.test.create_sprint(self.roadmap, description)

    def _rows_since(self, before_ids: set) -> list:
        """Every audit row added since `before_ids` was captured, oldest first."""
        rows = self.test.run_cmd_json(["audit", "list", "-r", self.roadmap, "-l", "500"])
        added = [r for r in rows if r["id"] not in before_ids]
        added.sort(key=lambda r: r["id"])
        return added

    def _snapshot_ids(self) -> set:
        rows = self.test.run_cmd_json(["audit", "list", "-r", self.roadmap, "-l", "500"])
        return {r["id"] for r in rows}

    # -- add-tasks mirror ----------------------------------------------------

    def test_add_tasks_writes_the_mirrored_pair(self):
        sprint_id = self._new_sprint("Sequence inbound container vessels against berth capacity")
        task_id = self._new_task("Reserve the west quay berth window for the inbound feeder")

        before = self._snapshot_ids()
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)])
        added = self._rows_since(before)

        for entry in added:
            assert_entry_shape(entry, "add-tasks mirror")
        by_op = {e["operation"]: e for e in added}
        assert set(by_op) == {"SPRINT_ADD_TASK", "TASK_STATUS_SPRINT"}, (
            f"sprint add-tasks on one task must write exactly these two operations; got {sorted(by_op)}"
        )
        sprint_row = by_op["SPRINT_ADD_TASK"]
        task_row = by_op["TASK_STATUS_SPRINT"]

        assert sprint_row["entity_type"] == "SPRINT" and sprint_row["entity_id"] == sprint_id
        assert sprint_row["related_entity_id"] == task_id, (
            "SPRINT_ADD_TASK must name the task added, or two of a sprint's entries read identically"
        )
        assert task_row["entity_type"] == "TASK" and task_row["entity_id"] == task_id
        assert task_row["related_entity_id"] == sprint_id, (
            "TASK_STATUS_SPRINT must name the sprint the task entered"
        )
        assert sprint_row["performed_at"] == task_row["performed_at"], (
            "the two rows of one invocation must share one performed_at "
            "(SPEC/DATABASE.md § `audit` Table, 'The rows of one command share one timestamp')"
        )
        print("✓ sprint add-tasks writes SPRINT_ADD_TASK / TASK_STATUS_SPRINT with transposed ids")

    # -- remove-tasks mirror ---------------------------------------------

    def test_remove_tasks_writes_the_mirrored_pair(self):
        sprint_id = self._new_sprint("Release berths held past their confirmed departure window")
        task_id = self._new_task("Hold the east quay berth for the delayed bulk carrier")
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)])

        before = self._snapshot_ids()
        self.test.run_cmd(["sprint", "remove-tasks", "-r", self.roadmap, str(sprint_id), str(task_id)])
        added = self._rows_since(before)

        for entry in added:
            assert_entry_shape(entry, "remove-tasks mirror")
        by_op = {e["operation"]: e for e in added}
        assert set(by_op) == {"SPRINT_REMOVE_TASK", "TASK_STATUS_BACKLOG"}, sorted(by_op)

        sprint_row = by_op["SPRINT_REMOVE_TASK"]
        task_row = by_op["TASK_STATUS_BACKLOG"]
        assert sprint_row["entity_id"] == sprint_id and sprint_row["related_entity_id"] == task_id
        assert task_row["entity_id"] == task_id and task_row["related_entity_id"] == sprint_id, (
            "the TASK_STATUS_BACKLOG entry written by sprint remove-tasks must name the sprint the "
            "task left, unlike the one task stat BACKLOG writes"
        )
        assert sprint_row["performed_at"] == task_row["performed_at"]
        print("✓ sprint remove-tasks writes SPRINT_REMOVE_TASK / TASK_STATUS_BACKLOG with transposed ids")

    # -- move-tasks: the OUT/IN pair, and no TASK_STATUS_* row --------------

    def test_move_tasks_writes_the_out_in_pair_and_no_status_row(self):
        source = self._new_sprint("Original berth plan for the transhipment window")
        destination = self._new_sprint("Revised berth plan after the tide table update")
        task_id = self._new_task("Re-sequence the transhipment call ahead of the spring tide")
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(source), str(task_id)])

        before = self._snapshot_ids()
        self.test.run_cmd([
            "sprint", "move-tasks", "-r", self.roadmap, str(source), str(destination), str(task_id)
        ])
        added = self._rows_since(before)

        for entry in added:
            assert_entry_shape(entry, "move-tasks pair")
        by_op = {e["operation"]: e for e in added}
        assert set(by_op) == {"SPRINT_MOVE_TASK_OUT", "SPRINT_MOVE_TASK_IN"}, (
            f"sprint move-tasks must write exactly the OUT/IN pair and NO TASK_STATUS_* row "
            f"(SPEC/DATABASE.md § `audit` Table, 'sprint move-tasks writes no TASK_STATUS_* row at "
            f"all'); got {sorted(by_op)}"
        )
        out_row, in_row = by_op["SPRINT_MOVE_TASK_OUT"], by_op["SPRINT_MOVE_TASK_IN"]
        assert out_row["entity_type"] == "SPRINT" and out_row["entity_id"] == source
        assert out_row["related_entity_id"] == task_id
        assert in_row["entity_type"] == "SPRINT" and in_row["entity_id"] == destination
        assert in_row["related_entity_id"] == task_id
        assert out_row["performed_at"] == in_row["performed_at"]

        task = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(task_id)])[0]
        assert task["status"] == "SPRINT", "move-tasks must preserve the task's status"
        print("✓ sprint move-tasks writes the SPRINT_MOVE_TASK_OUT / SPRINT_MOVE_TASK_IN pair only")

    # -- dependency pair, both directions -------------------------------

    def test_add_dep_and_remove_dep_write_both_directions(self):
        upstream = self._new_task("Publish the tide table feed the berth planner consumes")
        downstream = self._new_task("Consume the tide table feed in the berth allocation engine")

        before = self._snapshot_ids()
        self.test.run_cmd(["task", "add-dep", "-r", self.roadmap, str(downstream), str(upstream)])
        added = self._rows_since(before)
        for entry in added:
            assert_entry_shape(entry, "add-dep pair")
        assert len(added) == 2 and all(e["operation"] == "TASK_ADD_DEP" for e in added), added
        by_entity = {e["entity_id"]: e for e in added}
        assert set(by_entity) == {upstream, downstream}
        assert by_entity[downstream]["related_entity_id"] == upstream
        assert by_entity[upstream]["related_entity_id"] == downstream
        assert added[0]["performed_at"] == added[1]["performed_at"]

        before = self._snapshot_ids()
        self.test.run_cmd(["task", "remove-dep", "-r", self.roadmap, str(downstream), str(upstream)])
        removed = self._rows_since(before)
        for entry in removed:
            assert_entry_shape(entry, "remove-dep pair")
        assert len(removed) == 2 and all(e["operation"] == "TASK_REMOVE_DEP" for e in removed), removed
        by_entity = {e["entity_id"]: e for e in removed}
        assert by_entity[downstream]["related_entity_id"] == upstream
        assert by_entity[upstream]["related_entity_id"] == downstream
        print("✓ task add-dep / remove-dep each write a row against both tasks, naming each other")

    # -- sprint remove: one row, no per-task row -----------------------------

    def test_sprint_remove_writes_one_row_and_no_per_task_row(self):
        sprint_id = self._new_sprint("Decommission the temporary overflow berth allocation")
        member = self._new_task("Wind down bookings on the temporary overflow berth")
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(member)])

        before = self._snapshot_ids()
        self.test.run_cmd(["sprint", "remove", "-r", self.roadmap, str(sprint_id)])
        added = self._rows_since(before)

        for entry in added:
            assert_entry_shape(entry, "sprint remove")
        assert len(added) == 1, (
            f"sprint remove must write exactly one SPRINT_DELETE row and no per-task row, even though "
            f"the cascade resets the member task's own status; got {added}"
        )
        assert added[0]["operation"] == "SPRINT_DELETE"
        assert added[0]["entity_id"] == sprint_id
        assert added[0]["related_entity_id"] is None

        task = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(member)])[0]
        assert task["status"] == "BACKLOG", (
            "the member task's status is reset by the cascade even though no audit row records it "
            "against the task (SPEC/DATABASE.md § `audit` Table, 'sprint remove writes one "
            "SPRINT_DELETE row and no per-task row')"
        )
        print("✓ sprint remove writes exactly one SPRINT_DELETE row, no per-task row, despite the cascade")


class TestOneRowPerChangedFieldOnEdit:
    """SPEC/COMMANDS.md § Edit Task and § Update Sprint: N supplied fields
    write N rows, sharing one performed_at, each naming the field's own
    operation rather than a generic TASK_UPDATE / SPRINT_UPDATE row."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("cold-chain-shipment-monitoring")

    def teardown_method(self):
        self.test.teardown()

    def _new_task(self) -> int:
        return self.test.create_task(
            self.roadmap,
            title="Flag a cold-chain excursion before the shipment reaches the distribution centre",
            functional_requirements="A temperature excursion beyond the labelled range raises an alert.",
            technical_requirements="Sample the logger feed every five minutes against the product profile.",
            acceptance_criteria="An excursion longer than fifteen minutes pages the cold-chain duty officer.",
        )

    def _rows_since(self, roadmap: str, before_ids: set) -> list:
        rows = self.test.run_cmd_json(["audit", "list", "-r", roadmap, "-l", "500"])
        added = [r for r in rows if r["id"] not in before_ids]
        added.sort(key=lambda r: r["id"])
        return added

    def _snapshot_ids(self, roadmap: str) -> set:
        rows = self.test.run_cmd_json(["audit", "list", "-r", roadmap, "-l", "500"])
        return {r["id"] for r in rows}

    def test_task_edit_seven_fields_writes_seven_distinct_rows(self):
        task_id = self._new_task()
        before = self._snapshot_ids(self.roadmap)

        self.test.run_cmd([
            "task", "edit", "-r", self.roadmap, str(task_id),
            "-t", "Flag a cold-chain excursion the moment the logger reports it",
            "-fr", "A temperature excursion beyond the labelled range raises an alert immediately.",
            "-tr", "Sample the logger feed every minute against the product profile during transit.",
            "-ac", "An excursion longer than five minutes pages the cold-chain duty officer.",
            "-y", "BUG",
            "-p", "7",
            "--severity", "8",
        ])

        added = self._rows_since(self.roadmap, before)
        for entry in added:
            assert_entry_shape(entry, "task edit, seven fields")
        operations = [e["operation"] for e in added]
        expected = {
            "TASK_TITLE_CHANGE", "TASK_FUNCTIONAL_REQUIREMENTS_CHANGE",
            "TASK_TECHNICAL_REQUIREMENTS_CHANGE", "TASK_ACCEPTANCE_CRITERIA_CHANGE",
            "TASK_TYPE_CHANGE", "TASK_PRIORITY_CHANGE", "TASK_SEVERITY_CHANGE",
        }
        assert set(operations) == expected, (
            f"seven flags were supplied, want exactly these seven operations; got {sorted(operations)}"
        )
        assert len(operations) == 7, f"want exactly 7 rows for 7 supplied fields, got {len(operations)}"
        assert "TASK_UPDATE" not in operations, "TASK_UPDATE is LEGACY; task edit must never write it"
        assert len({e["performed_at"] for e in added}) == 1, (
            "all seven rows of one invocation must share one performed_at"
        )
        for e in added:
            assert e["entity_type"] == "TASK" and e["entity_id"] == task_id
            assert e["related_entity_id"] is None and e["commit_hash"] is None
        print("✓ task edit with seven flags writes exactly seven distinct per-field rows")

    def test_task_edit_one_field_writes_exactly_one_row(self):
        task_id = self._new_task()
        before = self._snapshot_ids(self.roadmap)

        self.test.run_cmd(["task", "prio", "-r", self.roadmap, str(task_id), "4"])
        added_via_prio = self._rows_since(self.roadmap, before)
        assert [e["operation"] for e in added_via_prio] == ["TASK_PRIORITY_CHANGE"], added_via_prio

        before = self._snapshot_ids(self.roadmap)
        self.test.run_cmd(["task", "edit", "-r", self.roadmap, str(task_id), "-p", "4"])
        added_via_edit = self._rows_since(self.roadmap, before)
        assert [e["operation"] for e in added_via_edit] == ["TASK_PRIORITY_CHANGE"], (
            "task edit -p must write the SAME single operation task prio writes, even though the "
            "supplied value equals the value already stored (SPEC/COMMANDS.md § Edit Task, "
            "'The trigger is the presence of the flag, not a difference in value')"
        )
        print("✓ a single supplied field writes exactly one row, from either command that sets it")

    def test_task_edit_with_no_fields_writes_zero_rows(self):
        task_id = self._new_task()
        before = self._snapshot_ids(self.roadmap)

        exit_code, stdout, _ = self.test.run_cmd(["task", "edit", "-r", self.roadmap, str(task_id)])
        assert exit_code == EXIT_OK
        added = self._rows_since(self.roadmap, before)
        assert added == [], f"a no-op edit must write zero audit rows; got {added}"
        print("✓ task edit with no field flags writes zero audit rows")

    def test_sprint_update_three_fields_writes_three_distinct_rows(self):
        sprint_id = self.test.create_sprint(
            self.roadmap, "Track cold-chain logger firmware across the reefer fleet")
        before = self._snapshot_ids(self.roadmap)

        self.test.run_cmd([
            "sprint", "update", "-r", self.roadmap, str(sprint_id),
            "-t", "Track cold-chain logger firmware across the whole reefer fleet",
            "-d", "Every reefer container reports its logger firmware version on manifest.",
            "--max-tasks", "40",
        ])

        added = self._rows_since(self.roadmap, before)
        for entry in added:
            assert_entry_shape(entry, "sprint update, three fields")
        operations = {e["operation"] for e in added}
        assert operations == {
            "SPRINT_TITLE_CHANGE", "SPRINT_DESCRIPTION_CHANGE", "SPRINT_MAX_TASKS_CHANGE",
        }, sorted(operations)
        assert len(added) == 3
        assert "SPRINT_UPDATE" not in [e["operation"] for e in added], "SPRINT_UPDATE is LEGACY"
        assert len({e["performed_at"] for e in added}) == 1
        for e in added:
            assert e["entity_type"] == "SPRINT" and e["entity_id"] == sprint_id
            assert e["related_entity_id"] is None and e["commit_hash"] is None
        print("✓ sprint update with three flags writes exactly three distinct per-field rows")

    def test_sprint_update_order_alone_writes_one_row(self):
        sprint_id = self.test.create_sprint(
            self.roadmap, "Stage the annual reefer fleet firmware rollout")
        before = self._snapshot_ids(self.roadmap)

        self.test.run_cmd(["sprint", "update", "-r", self.roadmap, str(sprint_id), "--order", "4200"])

        added = self._rows_since(self.roadmap, before)
        assert [e["operation"] for e in added] == ["SPRINT_ORDER_CHANGE"], added
        assert added[0]["related_entity_id"] is None and added[0]["commit_hash"] is None
        print("✓ sprint update --order alone writes exactly one SPRINT_ORDER_CHANGE row")


class TestAuditListFiltersEveryOperationAndLegacyValues:
    """SPEC/COMMANDS.md § List Audit Log: `-o/--operation` accepts exactly the
    published catalogue, LEGACY values included, and rejects anything else.

    The catalogue is read live from `--ai-help` rather than hardcoded here, so
    a future operation added to enums.AuditOperation is exercised automatically
    the next time this module runs -- the same anti-staleness the module's own
    docstring asks of the migration fixture below, applied to the filter list.
    """

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("grain-export-terminal-loadout")
        # Enough real activity that at least a few operations are non-vacuously
        # non-empty: TASK_CREATE, SPRINT_CREATE, SPRINT_ADD_TASK, TASK_STATUS_SPRINT.
        self.sprint_id = self.test.create_sprint(
            self.roadmap, "Sequence hopper-car unloading against vessel loadout capacity")
        self.task_a = self.test.create_task(
            self.roadmap,
            title="Reconcile hopper-car weighbridge tickets against the vessel loadout log",
            functional_requirements="Every hopper car unloaded matches one weighbridge ticket.",
            technical_requirements="Match tickets to cars by RFID tag, not by arrival order.",
            acceptance_criteria="An unmatched ticket after the shift raises a reconciliation alert.",
        )
        self.task_b = self.test.create_task(
            self.roadmap,
            title="Calibrate the belt scale against the certified reference weight",
            functional_requirements="The belt scale reading matches the certified reference within tolerance.",
            technical_requirements="Run the reference weight through the belt scale at shift start.",
            acceptance_criteria="A drift beyond the certified tolerance blocks loadout until recalibrated.",
        )
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id), str(self.task_a)
        ])

    def teardown_method(self):
        self.test.teardown()

    def _catalogue(self):
        contract = self.test.run_cmd_json(["--ai-help"])
        return contract["enums"]["AuditOperation"]["values"]

    def test_every_writable_operation_is_an_accepted_filter(self):
        writable = [v["value"] for v in self._catalogue() if v.get("legacy") is False]
        assert len(writable) >= 30, (
            f"only {len(writable)} writable operations were read from --ai-help; the catalogue "
            f"fetch itself is suspect, so this test would be vacuous"
        )

        non_empty_checked = 0
        for op in writable:
            exit_code, stdout, stderr = self.test.run_cmd(
                ["audit", "list", "-r", self.roadmap, "-o", op, "-l", "500"], check=False
            )
            assert exit_code == EXIT_OK, (
                f"`audit list -o {op}` exited {exit_code}, want 0: every writable operation is a "
                f"documented accepted filter value (SPEC/COMMANDS.md § List Audit Log)\nstderr: {stderr}"
            )
            rows = json.loads(stdout) if stdout.strip() else []
            assert isinstance(rows, list), f"`audit list -o {op}` did not return a JSON array: {stdout!r}"
            for row in rows:
                assert row["operation"] == op, (
                    f"`audit list -o {op}` returned a row whose operation is {row['operation']!r}; "
                    f"the filter must match by exact equality only"
                )
            if op in ("TASK_CREATE", "SPRINT_CREATE", "SPRINT_ADD_TASK", "TASK_STATUS_SPRINT"):
                assert rows, (
                    f"`audit list -o {op}` returned no rows even though the fixture setup wrote at "
                    f"least one; an always-empty result here would not prove the filter narrows anything"
                )
                non_empty_checked += 1

        assert non_empty_checked == 4, (
            f"only {non_empty_checked} of the 4 non-vacuous filter checks ran; the fixture setup "
            f"must have stopped producing the rows this assertion depends on"
        )
        print(f"✓ all {len(writable)} writable operations are accepted `audit list -o` filter values")

    def test_every_legacy_operation_is_an_accepted_filter_returning_empty(self):
        legacy = [v["value"] for v in self._catalogue() if v.get("legacy") is True]
        assert len(legacy) == 4, f"want the 4 documented LEGACY operations, got {sorted(legacy)}"

        for op in legacy:
            exit_code, stdout, stderr = self.test.run_cmd(
                ["audit", "list", "-r", self.roadmap, "-o", op], check=False
            )
            assert exit_code == EXIT_OK, (
                f"`audit list -o {op}` exited {exit_code}, want 0: LEGACY values stay accepted so "
                f"older entries carrying them remain reachable by name\nstderr: {stderr}"
            )
            assert stdout.strip() == "null" or stdout.strip() == "[]", (
                f"`audit list -o {op}` returned {stdout!r} on a roadmap written only at the current "
                f"schema; no command writes a LEGACY operation, so the result must be empty"
            )
        print(f"✓ all {len(legacy)} LEGACY operations are accepted filters, and match nothing here")

    def test_an_operation_outside_the_catalogue_is_rejected(self):
        exit_code, stdout, stderr = self.test.run_cmd(
            ["audit", "list", "-r", self.roadmap, "-o", "TASK_TELEPORT"], check=False
        )
        assert exit_code == EXIT_INVALID, f"exit {exit_code}, want {EXIT_INVALID}"
        assert _first_line(stderr) == "Error: validation error: invalid operation: TASK_TELEPORT", (
            f"got {_first_line(stderr)!r}"
        )
        assert stdout.strip() == "", "a rejected filter must write nothing to stdout"
        print("✓ an operation outside the catalogue is rejected with exit 6 and the exact message")


class TestAuditStatsCountsNewOperations:
    """SPEC/COMMANDS.md § Audit Statistics: `by_operation` counts every
    operation under its own key, including the operations the audit
    enrichment introduced -- TASK_STATUS_SPRINT never existed before 1.12.0,
    and SPRINT_MOVE_TASK_OUT / SPRINT_MOVE_TASK_IN replace the single legacy
    SPRINT_MOVE_TASK."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("bonded-warehouse-inventory-audit")

    def teardown_method(self):
        self.test.teardown()

    def _new_task(self, title: str) -> int:
        return self.test.create_task(
            self.roadmap,
            title=title,
            functional_requirements="Every bonded SKU movement is traceable to a customs entry.",
            technical_requirements="Post the movement to the bond ledger before the physical move.",
            acceptance_criteria="A movement with no matching bond ledger entry blocks release.",
        )

    def test_stats_counts_the_enriched_operations_separately(self):
        sprint_a = self.test.create_sprint(self.roadmap, "Q3 bonded warehouse cycle count")
        sprint_b = self.test.create_sprint(self.roadmap, "Q3 bonded warehouse cycle count, phase 2")
        tasks = [
            self._new_task(f"Cycle-count bonded aisle {aisle}") for aisle in ("A1", "A2", "A3")
        ]

        for task_id in tasks:
            self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(sprint_a), str(task_id)])
        # 3x SPRINT_ADD_TASK, 3x TASK_STATUS_SPRINT

        self.test.run_cmd([
            "sprint", "move-tasks", "-r", self.roadmap, str(sprint_a), str(sprint_b),
            ",".join(str(t) for t in tasks[:2]),
        ])
        # 2x SPRINT_MOVE_TASK_OUT, 2x SPRINT_MOVE_TASK_IN

        self.test.run_cmd(["sprint", "remove-tasks", "-r", self.roadmap, str(sprint_b), str(tasks[0])])
        # 1x SPRINT_REMOVE_TASK, 1x TASK_STATUS_BACKLOG (with a sprint counterpart)

        stats = self.test.run_cmd_json(["audit", "stats", "-r", self.roadmap])
        by_op = stats["by_operation"]

        expected = {
            "SPRINT_ADD_TASK": 3,
            "TASK_STATUS_SPRINT": 3,
            "SPRINT_MOVE_TASK_OUT": 2,
            "SPRINT_MOVE_TASK_IN": 2,
            "SPRINT_REMOVE_TASK": 1,
            "TASK_STATUS_BACKLOG": 1,
        }
        for op, want in expected.items():
            got = by_op.get(op, 0)
            assert got == want, (
                f"audit stats by_operation[{op!r}] = {got}, want {want} "
                f"(full by_operation: {by_op})"
            )
        assert "SPRINT_MOVE_TASK" not in by_op, (
            "the LEGACY SPRINT_MOVE_TASK must never be incremented by a real move; the directional "
            "pair replaces it entirely"
        )
        assert sum(by_op.values()) == stats["total_entries"], (
            "the per-operation buckets must sum to total_entries with no orphan bucket"
        )
        print("✓ audit stats counts TASK_STATUS_SPRINT and the SPRINT_MOVE_TASK_OUT/IN pair exactly")


# ============================================================================
# The 1.11.0 -> 1.12.0 audit migration, driven end to end through ./bin/rmp.
#
# internal/db/migration_audit_columns_test.go already gates the migrating SQL
# in isolation, against the Go package directly. What it cannot show is that a
# real user, opening a real roadmap with the real binary, sees the same
# outcome through `rmp audit list` / `rmp audit history` -- the JSON a caller
# actually parses. That is the gap this class closes: the fixture is built
# with nothing but sqlite3 (never through internal/db), the migration is
# triggered by an ordinary `rmp` invocation, and every outcome is read back
# out of the CLI's own JSON, not out of the database file, except where the
# database file is the only way to confirm the migration touched NOTHING else.
# ============================================================================

# auditDDL1110 is the audit table exactly as schema 1.11.0 declared it --
# transcribed verbatim from internal/db/migration_audit_columns_test.go's
# auditDDL1110, which is itself transcribed from the historical schema. A
# fixture for a historical shape must not follow later changes to today's
# schema, or the migration it exercises stops being the migration that ships.
AUDIT_DDL_1_11_0 = """
CREATE TABLE audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation TEXT NOT NULL,
    entity_type TEXT NOT NULL CHECK(entity_type IN ('TASK', 'SPRINT')),
    entity_id INTEGER NOT NULL,
    performed_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_operation ON audit(operation);
CREATE INDEX IF NOT EXISTS idx_audit_performed_at ON audit(performed_at);
CREATE INDEX IF NOT EXISTS idx_audit_date ON audit(performed_at DESC);
"""

# Fixture timestamps, millisecond-precision ISO 8601 UTC, the format the
# application itself writes. Reclassification turns on EXACT STRING EQUALITY,
# so every instant below is deliberately distinct except where a case names
# the coincidence on purpose.
TS_DOING = "2026-05-04T09:12:33.501Z"
TS_TESTING = "2026-05-05T14:47:02.118Z"
TS_COMPLETED = "2026-05-06T18:03:47.960Z"
TS_NO_MATCH = "2026-05-07T07:20:11.004Z"
TS_DELETED = "2026-05-08T11:55:29.377Z"


class TestMigration1_11_0FourRowClasses:
    """SPEC/VERSION.md § Migration 1.11.0 to 1.12.0, driven against the
    compiled binary with a fixture built directly in SQLite. Four row classes,
    one per rule the migration must apply or refuse to apply:

      1. determinable  -- performed_at equals exactly one of the owning task's
         three lifecycle timestamps: reclassified to the matching
         TASK_STATUS_* operation.
      2. no-timestamp-match -- performed_at matches none of the task's three
         timestamps (a transition to BACKLOG stamps none of them): stays
         TASK_STATUS_CHANGE.
      3. deleted-task -- entity_id names a task that no longer exists, so its
         timestamps went with it: stays TASK_STATUS_CHANGE.
      4. TASK_UPDATE -- never reclassified under any circumstance, proved here
         by seeding it at a timestamp that DOES equal a real lifecycle
         timestamp of another task, so only the operation predicate -- not a
         coincidence of clocks -- can be what protects it.
    """

    ROADMAP = "grain-terminal-audit-trail-migration"

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def _db_path(self, roadmap: str):
        return self.test.roadmaps_dir / roadmap / "project.db"

    def _schema_version(self, roadmap: str):
        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            row = con.execute(
                "SELECT value FROM _metadata WHERE key = 'schema_version'").fetchone()
            return row[0] if row else None
        finally:
            con.close()

    def _build_fixture(self):
        """Create a real roadmap through the CLI, then take its `audit` table
        back to the verbatim 1.11.0 shape and seed the four row classes by
        hand. Returns a dict of {label: {"id": task_id_or_None, "row_id": int}}.
        """
        roadmap = self.ROADMAP
        self.test.create_roadmap(roadmap)
        db_path = self._db_path(roadmap)
        assert db_path.exists(), f"precondition: the CLI must have created {db_path}"

        lifecycle_id = self.test.create_task(
            roadmap,
            title="Reconcile the vessel loadout tally against the export licence quota",
            functional_requirements="A loadout tally must never exceed the licensed export quota.",
            technical_requirements="Sum belt-scale readings per hold and compare against the licence.",
            acceptance_criteria="An over-tally blocks the bill of lading until corrected.",
        )
        no_match_id = self.test.create_task(
            roadmap,
            title="Draft the contingency plan for a belt-scale outage during loadout",
            functional_requirements="Loadout can continue on a certified backup weighing method.",
            technical_requirements="Fail over to the certified static scale within one shift.",
            acceptance_criteria="A belt-scale outage never halts loadout for more than one shift.",
        )
        deleted_id = self.test.create_task(
            roadmap,
            title="Retire the superseded pre-2026 loadout tally spreadsheet",
            functional_requirements="No loadout tally is computed from the retired spreadsheet.",
            technical_requirements="Migrate its formulas into the belt-scale reconciliation service.",
            acceptance_criteria="The spreadsheet is deleted once the service reproduces every formula.",
        )

        con = sqlite3.connect(str(db_path))
        try:
            # lifecycle_id: a full BACKLOG -> DOING -> TESTING -> COMPLETED run,
            # each transition at its own controlled instant.
            con.execute(
                "UPDATE tasks SET status = 'COMPLETED', started_at = ?, tested_at = ?, "
                "closed_at = ? WHERE id = ?",
                (TS_DOING, TS_TESTING, TS_COMPLETED, lifecycle_id),
            )
            # no_match_id: stays BACKLOG, every lifecycle timestamp NULL.
            # (already true of a freshly created task; nothing to set)

            # deleted_id: completed at TS_DELETED, then the task itself removed.
            con.execute(
                "UPDATE tasks SET status = 'COMPLETED', started_at = ?, tested_at = ?, "
                "closed_at = ? WHERE id = ?",
                (TS_DELETED, TS_DELETED, TS_DELETED, deleted_id),
            )

            # Replace `audit` with its verbatim 1.11.0 shape, discarding the
            # TASK_CREATE rows the CLI calls above produced, so the fixture is
            # exactly the four rows seeded below and every assertion is exhaustive.
            con.execute("DROP TABLE audit")
            con.executescript(AUDIT_DDL_1_11_0)

            def seed(operation, entity_id, performed_at):
                cur = con.execute(
                    "INSERT INTO audit (operation, entity_type, entity_id, performed_at) "
                    "VALUES (?, 'TASK', ?, ?)",
                    (operation, entity_id, performed_at),
                )
                return cur.lastrowid

            rows = {
                "doing": seed("TASK_STATUS_CHANGE", lifecycle_id, TS_DOING),
                "testing": seed("TASK_STATUS_CHANGE", lifecycle_id, TS_TESTING),
                "completed": seed("TASK_STATUS_CHANGE", lifecycle_id, TS_COMPLETED),
                "no_match": seed("TASK_STATUS_CHANGE", no_match_id, TS_NO_MATCH),
                "deleted": seed("TASK_STATUS_CHANGE", deleted_id, TS_DELETED),
                # Seeded at TS_DOING on purpose: lifecycle_id's OWN started_at.
                # Only the `operation = 'TASK_UPDATE'` predicate can protect
                # this row -- a coincidence of clocks must not be enough to
                # reclassify it.
                "task_update": seed("TASK_UPDATE", lifecycle_id, TS_DOING),
            }

            # The task that no longer exists is removed LAST, so its audit
            # entry is already stored when it goes: a deleted task leaves its
            # history behind.
            con.execute("DELETE FROM tasks WHERE id = ?", (deleted_id,))

            con.execute("UPDATE _metadata SET value = '1.11.0' WHERE key = 'schema_version'")
            con.commit()
        finally:
            con.close()

        return {
            "lifecycle_id": lifecycle_id,
            "no_match_id": no_match_id,
            "deleted_id": deleted_id,
            "rows": rows,
        }

    def _snapshot(self, roadmap: str):
        """Read every audit row directly, naming every column, so the
        migrated table's different physical column order (VERSION.md § the
        physical column order differs) is immaterial to the comparison."""
        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            cur = con.execute(
                "SELECT id, operation, entity_type, entity_id, related_entity_id, "
                "commit_hash, performed_at FROM audit ORDER BY id"
            )
            return cur.fetchall()
        finally:
            con.close()

    def _row_count(self, roadmap: str) -> int:
        """A schema-shape-agnostic row count: usable BEFORE the migration
        runs, when the audit table still has only its 1.11.0 columns, unlike
        _snapshot() above which names related_entity_id and commit_hash."""
        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            return con.execute("SELECT COUNT(*) FROM audit").fetchone()[0]
        finally:
            con.close()

    def test_the_four_row_classes_migrate_exactly_as_specified(self):
        fx = self._build_fixture()
        roadmap = self.ROADMAP
        assert self._schema_version(roadmap) == "1.11.0", "fixture precondition"

        before_row_count = self._row_count(roadmap)
        assert before_row_count == 6, f"fixture must seed exactly 6 rows, seeded {before_row_count}"

        # The next command against the roadmap runs the whole migration chain.
        self.test.run_cmd(["task", "list", "-r", roadmap])

        fresh = self.test.create_roadmap()
        assert self._schema_version(roadmap) == self._schema_version(fresh), (
            "a migrated database must land on the same schema version a fresh one is stamped with"
        )

        snapshot = self._snapshot(roadmap)
        by_id = {row[0]: row for row in snapshot}
        # column order: id, operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at

        rows = fx["rows"]

        # -- class 1: determinable ------------------------------------------
        assert by_id[rows["doing"]][1] == "TASK_STATUS_DOING", (
            f"a row at performed_at == started_at (and matching neither other timestamp) must become "
            f"TASK_STATUS_DOING; got {by_id[rows['doing']][1]!r}"
        )
        assert by_id[rows["testing"]][1] == "TASK_STATUS_TESTING", by_id[rows["testing"]]
        assert by_id[rows["completed"]][1] == "TASK_STATUS_COMPLETED", by_id[rows["completed"]]

        # -- class 2: no-timestamp-match --------------------------------------
        assert by_id[rows["no_match"]][1] == "TASK_STATUS_CHANGE", (
            f"a row whose performed_at matches none of the task's (all-NULL) timestamps must stay "
            f"TASK_STATUS_CHANGE; got {by_id[rows['no_match']][1]!r}"
        )

        # -- class 3: deleted task --------------------------------------------
        assert by_id[rows["deleted"]][1] == "TASK_STATUS_CHANGE", (
            f"a row whose task no longer exists must stay TASK_STATUS_CHANGE, because its timestamps "
            f"were deleted with the task; got {by_id[rows['deleted']][1]!r}"
        )

        # -- class 4: TASK_UPDATE ----------------------------------------------
        assert by_id[rows["task_update"]][1] == "TASK_UPDATE", (
            f"a TASK_UPDATE row must never be reclassified, even at a timestamp that equals a real "
            f"lifecycle timestamp of its own task; got {by_id[rows['task_update']][1]!r}"
        )

        # -- invariants that must hold across ALL six rows ---------------------
        assert len(snapshot) == 6, f"the migration must delete and insert nothing; got {len(snapshot)} rows"
        assert {row[0] for row in snapshot} == set(rows.values()), (
            "the migration must not renumber any row"
        )
        for row in snapshot:
            row_id, _operation, entity_type, entity_id, related_entity_id, commit_hash, _performed_at = row
            assert entity_type == "TASK"
            assert related_entity_id is None and commit_hash is None, (
                f"row {row_id} migrated with related_entity_id={related_entity_id!r} "
                f"commit_hash={commit_hash!r}; neither new column is backfilled on a pre-existing row "
                f"(SPEC/VERSION.md § Migration 1.11.0 to 1.12.0, 'Neither new column is backfilled')"
            )

        print("✓ all four row classes migrate exactly as SPEC/VERSION.md specifies")

    def test_the_migration_is_visible_end_to_end_through_the_cli_json(self):
        """The same fixture, read back through `rmp audit history` / `rmp audit
        list` -- the JSON surface a real caller parses -- rather than through
        SQLite. This is what actually closes the loop task #268 asks for: the
        SQL layer and the JSON layer must agree."""
        fx = self._build_fixture()
        roadmap = self.ROADMAP
        rows = fx["rows"]

        self.test.run_cmd(["task", "list", "-r", roadmap])  # triggers the migration

        history = self.test.run_cmd_json(
            ["audit", "history", "-r", roadmap, "TASK", str(fx["lifecycle_id"])]
        )
        by_id = {e["id"]: e for e in history}
        for label, want_op in (
            ("doing", "TASK_STATUS_DOING"),
            ("testing", "TASK_STATUS_TESTING"),
            ("completed", "TASK_STATUS_COMPLETED"),
        ):
            row_id = rows[label]
            assert row_id in by_id, (
                f"`audit history TASK {fx['lifecycle_id']}` does not return row {row_id} ({label}) at all"
            )
            entry = by_id[row_id]
            assert_entry_shape(entry, f"migrated row {label}")
            assert entry["operation"] == want_op, (
                f"the CLI reports row {row_id} as {entry['operation']!r}, want {want_op!r}: the SQL "
                f"layer and the JSON layer disagree"
            )
            assert entry["related_entity_id"] is None and entry["commit_hash"] is None

        # The TASK_UPDATE row against the SAME task must also still be there,
        # unreclassified, reachable in the same history call.
        task_update_entry = by_id.get(rows["task_update"])
        assert task_update_entry is not None, "the TASK_UPDATE row vanished from the task's own history"
        assert task_update_entry["operation"] == "TASK_UPDATE"

        # The no-timestamp-match and deleted-task rows are reachable by
        # filtering on the surviving LEGACY operation, exactly as
        # SPEC/COMMANDS.md promises for an older roadmap's entries.
        legacy_rows = self.test.run_cmd_json(
            ["audit", "list", "-r", roadmap, "-o", "TASK_STATUS_CHANGE", "-l", "500"]
        )
        legacy_ids = {e["id"] for e in legacy_rows}
        assert rows["no_match"] in legacy_ids, "the no-timestamp-match row is not reachable by filter"
        assert rows["deleted"] in legacy_ids, "the deleted-task row is not reachable by filter"
        for row_id in (rows["doing"], rows["testing"], rows["completed"]):
            assert row_id not in legacy_ids, (
                f"row {row_id} was reclassified away from TASK_STATUS_CHANGE, so it must not appear "
                f"in a TASK_STATUS_CHANGE filter any more"
            )

        # The deleted task really is gone from the entity surface, while its
        # audit history survives it -- the whole point of an immutable log.
        exit_code, _stdout, _stderr = self.test.run_cmd(
            ["task", "get", "-r", roadmap, str(fx["deleted_id"])], check=False
        )
        assert exit_code == 4, f"task get on the deleted fixture task: exit {exit_code}, want 4"

        deleted_history = self.test.run_cmd_json(
            ["audit", "history", "-r", roadmap, "TASK", str(fx["deleted_id"])]
        )
        deleted_history_ids = {e["id"] for e in deleted_history}
        assert rows["deleted"] in deleted_history_ids, (
            "the deleted task's own audit history must still return its surviving row, even though "
            "`task get` on the same id now returns exit 4"
        )
        deleted_entry = next(e for e in deleted_history if e["id"] == rows["deleted"])
        assert deleted_entry["operation"] == "TASK_STATUS_CHANGE"
        assert_entry_shape(deleted_entry, "deleted task's surviving row")

        print("✓ the migration's outcome agrees end to end between SQLite and the CLI's own JSON")

    def test_the_migration_is_idempotent_through_two_binary_invocations(self):
        """Running the CLI against the same roadmap a second time must not
        touch a row the first invocation already settled."""
        fx = self._build_fixture()
        roadmap = self.ROADMAP

        self.test.run_cmd(["task", "list", "-r", roadmap])  # first open: migrates
        after_first = self._snapshot(roadmap)

        self.test.run_cmd(["task", "list", "-r", roadmap])  # second open: must be a no-op
        after_second = self._snapshot(roadmap)

        assert after_first == after_second, (
            "a second invocation against an already-migrated roadmap changed the audit table; the "
            "migration must be a no-op once already applied"
        )
        print("✓ a second binary invocation against the migrated roadmap changes nothing further")


# ============================================================================
# Non-vacuity: for each of the four ways this module's assertions could pass
# against a broken product (CLAUDE.md's own repeated lesson -- "a test that
# passes against the broken product is worth nothing"), inject the defect
# directly into the fixture's SQLite file, show the SAME assertion this module
# uses elsewhere go red, and revert it. The database manipulated is the real
# roadmap's project.db under this test's own throwaway HOME
# (GroadmapTestBase.setup() -> tempfile.mkdtemp()), which is already outside
# the repository and discarded on teardown -- never a file under tests/ or any
# production source.
# ============================================================================

class TestNonVacuityProofs:
    """Proves the four families of assertion above are not vacuous: each can
    in fact fail. `operation` carries no CHECK constraint and neither does the
    RULE that ties commit_hash or related_entity_id to a particular operation
    (SPEC/DATABASE.md § `audit` Table, 'A stored row may carry an operation the
    catalogue does not list') -- these are application-level invariants, so
    SQLite itself will accept every corrupted value written below, which is
    exactly why an assertion is the only thing that can catch the defect."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("meat-cold-store-traceability-audit")
        self.sprint_id = self.test.create_sprint(
            self.roadmap, "Trace every cold-store pallet move back to its consignment")

    def teardown_method(self):
        self.test.teardown()

    def _con(self):
        db_path = self.test.roadmaps_dir / self.roadmap / "project.db"
        assert db_path.exists(), f"precondition: {db_path} must exist before any corruption"
        return sqlite3.connect(str(db_path))

    def _set_operation(self, row_id: int, operation: str):
        con = self._con()
        try:
            con.execute("UPDATE audit SET operation = ? WHERE id = ?", (operation, row_id))
            con.commit()
        finally:
            con.close()

    def _set_commit_hash(self, row_id: int, value):
        con = self._con()
        try:
            con.execute("UPDATE audit SET commit_hash = ? WHERE id = ?", (value, row_id))
            con.commit()
        finally:
            con.close()

    def _set_related_entity_id(self, row_id: int, value):
        con = self._con()
        try:
            con.execute("UPDATE audit SET related_entity_id = ? WHERE id = ?", (value, row_id))
            con.commit()
        finally:
            con.close()

    def _row(self, row_id: int) -> dict:
        rows = self.test.run_cmd_json(["audit", "list", "-r", self.roadmap, "-l", "500"])
        for row in rows:
            if row["id"] == row_id:
                return row
        raise AssertionError(f"audit row {row_id} is not present in `audit list` at all")

    # -- 1. an operation written against the wrong transition ---------------

    def _assert_operation_is(self, row_id: int, expected_op: str):
        row = self._row(row_id)
        assert row["operation"] == expected_op, (
            f"audit row {row_id} carries operation {row['operation']!r}, want {expected_op!r}"
        )

    def test_proof_the_wrong_transition_operation_is_caught(self):
        task_id = self.test.create_task(
            self.roadmap,
            title="Trace the ribeye consignment through the blast-chill cycle",
            functional_requirements="Every pallet move logs the consignment it belongs to.",
            technical_requirements="Read the RFID tag at every cold-store door transition.",
            acceptance_criteria="A pallet move with no consignment match blocks put-away.",
        )
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id), str(task_id)])
        self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, str(task_id), "DOING", "--commit-open", OPEN_HASH_1]
        )
        history = self.test.run_cmd_json(["audit", "history", "-r", self.roadmap, "TASK", str(task_id)])
        doing_row_id = history[0]["id"]
        self._assert_operation_is(doing_row_id, "TASK_STATUS_DOING")  # green, before injection

        # INJECT: rewrite the row as if the writer had recorded the wrong
        # destination for the transition.
        self._set_operation(doing_row_id, "TASK_STATUS_TESTING")
        try:
            self._assert_operation_is(doing_row_id, "TASK_STATUS_DOING")
        except AssertionError as caught:
            print(f"✓ RED confirmed (wrong transition operation): {caught}")
        else:
            raise AssertionError(
                "the operation assertion did not catch a row rewritten to the wrong transition; "
                "this module's checks would be vacuous against that defect"
            )

        # REVERT and confirm GREEN again.
        self._set_operation(doing_row_id, "TASK_STATUS_DOING")
        self._assert_operation_is(doing_row_id, "TASK_STATUS_DOING")
        print("✓ reverted; the same assertion is green again")

    # -- 2. a commit hash missing where required, or present where forbidden -

    def _assert_commit_hash_rule(self, roadmap: str, task_id: int):
        for entry in self.test.run_cmd_json(["audit", "history", "-r", roadmap, "TASK", str(task_id)]):
            carries_one = entry["commit_hash"] is not None
            should_carry_one = entry["operation"] in COMMIT_CARRYING_OPERATIONS
            assert carries_one == should_carry_one, (
                f"row {entry['id']} ({entry['operation']}) has commit_hash={entry['commit_hash']!r}, "
                f"but should carry one: {should_carry_one} (SPEC/DATABASE.md § The Commit Hash of an "
                f"Audit Entry)"
            )

    def test_proof_a_missing_or_misplaced_commit_hash_is_caught(self):
        task_id = self.test.create_task(
            self.roadmap,
            title="Confirm the blast-chill setpoint before releasing the pallet",
            functional_requirements="A pallet only releases once the core temperature is confirmed.",
            technical_requirements="Poll the core-temperature probe every thirty seconds during chill.",
            acceptance_criteria="A release without a confirmed core temperature is blocked.",
        )
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id), str(task_id)])
        self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, str(task_id), "DOING", "--commit-open", OPEN_HASH_2]
        )
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(task_id), "TESTING"])

        self._assert_commit_hash_rule(self.roadmap, task_id)  # green, before injection

        history = self.test.run_cmd_json(["audit", "history", "-r", self.roadmap, "TASK", str(task_id)])
        doing_row = next(e for e in history if e["operation"] == "TASK_STATUS_DOING")
        testing_row = next(e for e in history if e["operation"] == "TASK_STATUS_TESTING")

        # INJECT (a): the hash goes missing from the row that must carry one.
        self._set_commit_hash(doing_row["id"], None)
        try:
            self._assert_commit_hash_rule(self.roadmap, task_id)
        except AssertionError as caught:
            print(f"✓ RED confirmed (commit_hash missing where required): {caught}")
        else:
            raise AssertionError(
                "the commit-hash rule did not catch a TASK_STATUS_DOING row with no commit_hash"
            )
        self._set_commit_hash(doing_row["id"], OPEN_HASH_2)  # REVERT
        self._assert_commit_hash_rule(self.roadmap, task_id)

        # INJECT (b): a hash appears on a row that must never carry one.
        # SQLite's own CHECK only bounds the FORMAT (7..64 lowercase hex), so
        # a well-formed hash on the wrong row is accepted at the storage
        # layer -- the application-level rule is the only thing that can
        # reject it, which is exactly what this proves.
        self._set_commit_hash(testing_row["id"], INJECTED_HASH)
        try:
            self._assert_commit_hash_rule(self.roadmap, task_id)
        except AssertionError as caught:
            print(f"✓ RED confirmed (commit_hash present where forbidden): {caught}")
        else:
            raise AssertionError(
                "the commit-hash rule did not catch a TASK_STATUS_TESTING row carrying a commit_hash"
            )
        self._set_commit_hash(testing_row["id"], None)  # REVERT
        self._assert_commit_hash_rule(self.roadmap, task_id)
        print("✓ reverted both injections; the same assertion is green again")

    # -- 3. a relational row missing its counterpart -------------------------

    def _assert_mirror(self, sprint_row_id: int, task_row_id: int, task_id: int, sprint_id: int):
        sprint_row = self._row(sprint_row_id)
        task_row = self._row(task_row_id)
        assert sprint_row["related_entity_id"] == task_id, (
            f"SPRINT_ADD_TASK row {sprint_row_id} names related_entity_id="
            f"{sprint_row['related_entity_id']!r}, want the task id {task_id}"
        )
        assert task_row["related_entity_id"] == sprint_id, (
            f"TASK_STATUS_SPRINT row {task_row_id} names related_entity_id="
            f"{task_row['related_entity_id']!r}, want the sprint id {sprint_id}"
        )

    def test_proof_a_relational_row_missing_its_counterpart_is_caught(self):
        task_id = self.test.create_task(
            self.roadmap,
            title="Bind the pallet RFID tag to its consignment on intake",
            functional_requirements="Every pallet RFID tag resolves to exactly one consignment.",
            technical_requirements="Write the binding at the dock door reader, not at put-away.",
            acceptance_criteria="An unbound tag is rejected at the dock door, not discovered later.",
        )
        before = {r["id"] for r in self.test.run_cmd_json(["audit", "list", "-r", self.roadmap, "-l", "500"])}
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id), str(task_id)])
        added = [
            r for r in self.test.run_cmd_json(["audit", "list", "-r", self.roadmap, "-l", "500"])
            if r["id"] not in before
        ]
        sprint_row_id = next(r["id"] for r in added if r["operation"] == "SPRINT_ADD_TASK")
        task_row_id = next(r["id"] for r in added if r["operation"] == "TASK_STATUS_SPRINT")

        self._assert_mirror(sprint_row_id, task_row_id, task_id, self.sprint_id)  # green

        # INJECT: the task-side row loses its counterpart.
        self._set_related_entity_id(task_row_id, None)
        try:
            self._assert_mirror(sprint_row_id, task_row_id, task_id, self.sprint_id)
        except AssertionError as caught:
            print(f"✓ RED confirmed (relational row missing its counterpart): {caught}")
        else:
            raise AssertionError(
                "the mirror assertion did not catch a TASK_STATUS_SPRINT row with no related_entity_id"
            )

        # REVERT and confirm GREEN again.
        self._set_related_entity_id(task_row_id, self.sprint_id)
        self._assert_mirror(sprint_row_id, task_row_id, task_id, self.sprint_id)
        print("✓ reverted; the same assertion is green again")

    # -- 4. the migration reclassifying a row it must leave alone -----------

    def test_proof_the_migration_reclassifying_a_protected_row_is_caught(self):
        roadmap = "meat-cold-store-migration-non-vacuity-proof"
        self.test.create_roadmap(roadmap)
        db_path = self.test.roadmaps_dir / roadmap / "project.db"

        protected_task = self.test.create_task(
            roadmap,
            title="Investigate the cold-store door sensor that never reported a close event",
            functional_requirements="Every door-open event is matched by a close event within an hour.",
            technical_requirements="Alert when a door sensor reports open with no matching close.",
            acceptance_criteria="An unmatched open event pages the shift supervisor within the hour.",
        )

        con = sqlite3.connect(str(db_path))
        try:
            # This row must be LEFT ALONE by the migration: performed_at
            # matches none of the task's (all-NULL) lifecycle timestamps.
            con.execute("DROP TABLE audit")
            con.executescript(AUDIT_DDL_1_11_0)
            cur = con.execute(
                "INSERT INTO audit (operation, entity_type, entity_id, performed_at) "
                "VALUES ('TASK_STATUS_CHANGE', 'TASK', ?, ?)",
                (protected_task, TS_NO_MATCH),
            )
            protected_row_id = cur.lastrowid
            con.execute("UPDATE _metadata SET value = '1.11.0' WHERE key = 'schema_version'")
            con.commit()
        finally:
            con.close()

        self.test.run_cmd(["task", "list", "-r", roadmap])  # runs the real migration

        def assert_still_legacy():
            legacy_rows = self.test.run_cmd_json(
                ["audit", "list", "-r", roadmap, "-o", "TASK_STATUS_CHANGE", "-l", "500"]
            )
            legacy_ids = {r["id"] for r in legacy_rows}
            assert protected_row_id in legacy_ids, (
                f"row {protected_row_id} is not reachable under TASK_STATUS_CHANGE any more; the "
                f"migration must leave a no-timestamp-match row alone"
            )

        assert_still_legacy()  # green: the real migration behaved correctly

        # INJECT, directly against the migrated database: simulate a defective
        # migration that reclassified this protected row anyway.
        con = sqlite3.connect(str(db_path))
        try:
            con.execute(
                "UPDATE audit SET operation = 'TASK_STATUS_DOING' WHERE id = ?", (protected_row_id,)
            )
            con.commit()
        finally:
            con.close()

        try:
            assert_still_legacy()
        except AssertionError as caught:
            print(f"✓ RED confirmed (migration reclassified a protected row): {caught}")
        else:
            raise AssertionError(
                "assert_still_legacy() did not catch a protected row that had been reclassified; "
                "the migration test above would be vacuous against that defect"
            )

        # REVERT and confirm GREEN again.
        con = sqlite3.connect(str(db_path))
        try:
            con.execute(
                "UPDATE audit SET operation = 'TASK_STATUS_CHANGE' WHERE id = ?", (protected_row_id,)
            )
            con.commit()
        finally:
            con.close()
        assert_still_legacy()
        print("✓ reverted; the protected row is reachable under TASK_STATUS_CHANGE again")


def _run_all():
    """Discover every `Test*` class defined in THIS module by introspecting
    the module's own globals, rather than a hardcoded tuple of classes. A
    hardcoded list is exactly the harness trap rmp task #303 records: a class
    appended below the runner without also being added to a fixed tuple never
    runs, and the comment promising otherwise goes stale silently. Discovering
    by `dir()`/globals() means a class added later is picked up automatically,
    matching how test_05_audit_reporting.py already discovers its own test_*
    methods.
    """
    module = sys.modules[__name__]
    test_classes = [
        obj for name, obj in sorted(vars(module).items())
        if name.startswith("Test") and isinstance(obj, type)
    ]
    assert test_classes, "no Test* class was discovered in this module at all"

    passed = 0
    failed = 0
    failures = []
    classes_run = 0
    methods_run = 0

    for cls in test_classes:
        method_names = sorted(m for m in dir(cls) if m.startswith("test_"))
        if not method_names:
            continue
        classes_run += 1
        for name in method_names:
            methods_run += 1
            label = f"{cls.__name__}.{name}"
            instance = cls()
            instance.setup_method()
            try:
                getattr(instance, name)()
                passed += 1
            except AssertionError as exc:
                failed += 1
                failures.append((label, exc))
                print(f"✗ {label}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((label, exc))
                print(f"✗ {label} (error)")
            finally:
                instance.teardown_method()

    print("\n" + "=" * 60)
    print(f"Audit enrichment E2E tests: {passed} passed, {failed} failed "
          f"({methods_run} methods across {classes_run} classes)")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
