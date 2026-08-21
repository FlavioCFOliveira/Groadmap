#!/usr/bin/env python3
"""
Test 51: Removal of the task `specialists` field — E2E coverage.

The task entity's `specialists` field, the `task assign` / `task unassign`
subcommands, the `-sp`/`--specialists` flag on `task create` and `task edit`,
the `-sp`/`--specialists` filter on `task list`, and the `TASK_ASSIGN` /
`TASK_UNASSIGN` audit operations were all removed together (rmp task #246;
SPEC/VERSION.md § Migration 1.9.0 -> 1.10.0; SPEC/DATABASE.md § Migration
Idempotency (ALTER TABLE DROP COLUMN)). Every other test module that used to
exercise those routes now asserts they are GONE instead (test_01, test_10,
test_36, test_37, test_42, test_44); this module is the single place that
proves the removal itself, exhaustively, plus the schema migration that
enacts it.

Coverage matrix
----------------
Route removal (each checks the EXACT documented exit code):
  1.  `task assign` / `task unassign`             -> exit 2 (unknown subcommand)
  2.  `task assign --help` / `task unassign --help` -> exit 2 (not resurrected by --help)
  3.  `task create -sp/--specialists`              -> exit 2 (unknown flag)
  4.  `task edit -sp/--specialists`                -> exit 2 (unknown flag)
  5.  `task list -sp/--specialists`                -> exit 2 (unknown flag)
  6.  `audit list -o TASK_ASSIGN` / `TASK_UNASSIGN` -> exit 6 (invalid operation)

Field absence:
  7.  `task get` / `task list` / `task next` carry exactly 20 keys, none
      named "specialists".
  8.  No help output (family or subcommand) and no `--ai-help` payload
      mentions "specialist" in any casing.

Schema migration 1.9.0 -> 1.10.0 (built from the verbatim historical DDL, not
by dropping a column from a fresh 1.10.0 database — see
_build_1_9_0_fixture):
  9.  The next command against a 1.9.0-shape database runs the migration chain,
      drops `tasks.specialists`, and every other column/row survives.
  10. Pre-existing `TASK_ASSIGN` / `TASK_UNASSIGN` audit rows survive the
      migration and remain visible in an UNFILTERED `audit list` / `audit
      stats`, even though the operation is no longer reachable by name via
      the `-o` filter.
  11. A second migrated open is a clean, idempotent no-op.
"""

import os
import sqlite3
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase  # noqa: E402


EXIT_MISUSE = 2      # unknown subcommand / unknown flag
EXIT_INVALID = 6      # validation error (unknown audit operation)

# The exact 20 keys a task JSON object must carry post-removal
# (SPEC/MODELS.md; mirrors GroadmapTestBase.TASK_KEYS, which is "specialists"
# removed at schema 1.10.0 and commit_open/commit_close added at 1.11.0).
EXPECTED_TASK_KEYS = frozenset([
    "id", "title", "status", "type",
    "functional_requirements", "technical_requirements", "acceptance_criteria",
    "created_at",
    "started_at", "tested_at", "closed_at", "completion_summary",
    "commit_open", "commit_close",
    "parent_task_id", "priority", "severity",
    "subtask_count", "depends_on", "blocks",
])

# tasksDDL190 is the tasks table exactly as schema 1.9.0 declared it: the
# specialists column sits in the middle of the nullable-tracking group,
# between created_at and started_at, with every CHECK, DEFAULT, index and the
# parent_task_id self-reference the shipped 1.9.0 schema carried (verbatim
# transcription of internal/db/migration_specialists_test.go's tasksDDL190,
# the Go regression fixture for the same migration). Building the fixture
# from this verbatim DDL — rather than from a fresh 1.10.0 database with the
# column dropped back on at the END of the table — is what makes the test
# non-vacuous: dropping a MIDDLE column is what the real 1.9.0 databases in
# the field require, and a fixture that merely appended the column would
# never reach that code path.
TASKS_DDL_1_9_0 = """
CREATE TABLE tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- Group 1: Content fields (TEXT) - frequently accessed together
    title TEXT NOT NULL CHECK(length(title) <= 255),
    status TEXT NOT NULL DEFAULT 'BACKLOG' CHECK(status IN ('BACKLOG', 'SPRINT', 'DOING', 'TESTING', 'COMPLETED')),
    type TEXT NOT NULL DEFAULT 'TASK' CHECK(type IN ('USER_STORY', 'TASK', 'BUG', 'SUB_TASK', 'EPIC', 'REFACTOR', 'CHORE', 'SPIKE', 'DESIGN_UX', 'IMPROVEMENT')),
    functional_requirements TEXT NOT NULL CHECK(length(functional_requirements) <= 4096),
    technical_requirements TEXT NOT NULL CHECK(length(technical_requirements) <= 4096),
    acceptance_criteria TEXT NOT NULL CHECK(length(acceptance_criteria) <= 4096),
    created_at TEXT NOT NULL,

    -- Group 2: Nullable tracking fields - lifecycle timestamps
    specialists TEXT,
    started_at TEXT,
    tested_at TEXT,
    closed_at TEXT,
    completion_summary TEXT CHECK(completion_summary IS NULL OR length(completion_summary) <= 4096),
    parent_task_id INTEGER REFERENCES tasks(id),

    -- Group 3: Numeric metadata fields
    priority INTEGER NOT NULL DEFAULT 0 CHECK(priority >= 0 AND priority <= 9),
    severity INTEGER NOT NULL DEFAULT 0 CHECK(severity >= 0 AND severity <= 9)
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_type ON tasks(type);
CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority);
CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);
CREATE INDEX IF NOT EXISTS idx_tasks_status_priority ON tasks(status, priority DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_priority_created ON tasks(priority DESC, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_parent_task_id ON tasks(parent_task_id);
"""

# Family and subcommand helps sampled for the "no mention anywhere" sweep.
# Not exhaustive of the whole CLI surface (test_44 owns that inventory); this
# list targets every family the field could plausibly still be documented
# under, plus the task family in full, since that is where it lived.
HELP_FAMILIES = ["roadmap", "task", "sprint", "backlog", "audit", "stats", "graph", "web"]
TASK_SUBCOMMAND_HELPS = [
    "list", "create", "get", "next", "edit", "remove",
    "stat", "reopen", "prio", "sev", "subtasks",
    "add-dep", "remove-dep", "blockers", "blocking",
]


class TestSpecialistsFieldRemoved:
    """CLI-surface proof that the specialists field and its routes are gone."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("payment-gateway-hardening")
        self.task_id = self.test.create_task(
            self.roadmap,
            title="Rotate the acquirer webhook signing secret",
            functional_requirements="Compromised secrets must be rotatable without downtime",
            technical_requirements="Support two active signing keys during the rollover window",
            acceptance_criteria="Old key rejected 24h after rotation; no dropped webhooks during rollover",
            priority=6,
            severity=5,
        )

    def teardown_method(self):
        self.test.teardown()

    # ---- 1. task assign / task unassign : unknown subcommand, exit 2 ------

    def test_task_assign_subcommand_rejected(self):
        code, out, err = self.test.run_cmd(
            ["task", "assign", "-r", self.roadmap, str(self.task_id), "Dev One"],
            check=False,
        )
        assert code == EXIT_MISUSE, f"task assign must exit {EXIT_MISUSE}, got {code}: {err}"
        assert out == "", f"rejected subcommand must write nothing to stdout: {out!r}"
        assert "unknown" in err.lower() and "assign" in err.lower(), (
            f"stderr must name the unknown subcommand: {err!r}"
        )
        # No state change: the task the attempt targeted reads back unchanged.
        after = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(self.task_id)])[0]
        assert "specialists" not in after
        assert after["title"] == "Rotate the acquirer webhook signing secret"

    def test_task_unassign_subcommand_rejected(self):
        code, out, err = self.test.run_cmd(
            ["task", "unassign", "-r", self.roadmap, str(self.task_id), "Dev One"],
            check=False,
        )
        assert code == EXIT_MISUSE, f"task unassign must exit {EXIT_MISUSE}, got {code}: {err}"
        assert out == "", f"rejected subcommand must write nothing to stdout: {out!r}"
        assert "unknown" in err.lower() and "unassign" in err.lower(), (
            f"stderr must name the unknown subcommand: {err!r}"
        )

    def test_task_assign_help_does_not_resurrect_the_subcommand(self):
        """`--help` must not be a backdoor: `task assign --help` is still an
        unknown subcommand, not a rendered help screen."""
        for sub in ("assign", "unassign"):
            code, out, err = self.test.run_cmd(
                ["task", sub, "--help", "-r", self.roadmap], check=False)
            assert code == EXIT_MISUSE, (
                f"task {sub} --help must exit {EXIT_MISUSE} like any other unknown "
                f"subcommand, got {code}: {err}"
            )
            assert out == "", f"task {sub} --help must write nothing to stdout: {out!r}"

    # ---- 3/4/5. -sp / --specialists flags: unknown flag, exit 2 -----------

    def test_task_create_short_flag_rejected(self):
        before = self.test.run_cmd_json(["task", "list", "-r", self.roadmap])
        code, out, err = self.test.run_cmd(
            ["task", "create", "-r", self.roadmap, "-t", "Add anomaly alerts to the settlement job",
             "-fr", "fr", "-tr", "tr", "-ac", "ac", "-sp", "Dev One,Dev Two"],
            check=False,
        )
        assert code == EXIT_MISUSE, f"task create -sp must exit {EXIT_MISUSE}, got {code}: {err}"
        assert out == "", f"rejected create must write nothing to stdout: {out!r}"
        assert "-sp" in err or "flag" in err.lower(), err
        after = self.test.run_cmd_json(["task", "list", "-r", self.roadmap])
        assert len(after) == len(before), "a rejected create must not create a task"

    def test_task_create_long_flag_rejected(self):
        code, out, err = self.test.run_cmd(
            ["task", "create", "-r", self.roadmap, "-t", "Add anomaly alerts to the settlement job",
             "-fr", "fr", "-tr", "tr", "-ac", "ac", "--specialists", "Dev One"],
            check=False,
        )
        assert code == EXIT_MISUSE, f"task create --specialists must exit {EXIT_MISUSE}, got {code}: {err}"
        assert out == ""

    def test_task_edit_flag_rejected(self):
        before = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(self.task_id)])[0]
        code, out, err = self.test.run_cmd(
            ["task", "edit", "-r", self.roadmap, str(self.task_id), "-sp", "Dev Three"],
            check=False,
        )
        assert code == EXIT_MISUSE, f"task edit -sp must exit {EXIT_MISUSE}, got {code}: {err}"
        assert out == ""
        after = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(self.task_id)])[0]
        assert after == before, "a rejected edit must not change the task"

    def test_task_list_filter_rejected(self):
        code, out, err = self.test.run_cmd(
            ["task", "list", "-r", self.roadmap, "-sp", "Dev One"], check=False)
        assert code == EXIT_MISUSE, f"task list -sp must exit {EXIT_MISUSE}, got {code}: {err}"
        assert out == ""

    # ---- 6. audit list -o TASK_ASSIGN/TASK_UNASSIGN : invalid op, exit 6 --

    def test_audit_list_task_assign_operation_filter_rejected(self):
        code, out, err = self.test.run_cmd(
            ["audit", "list", "-r", self.roadmap, "-o", "TASK_ASSIGN"], check=False)
        assert code == EXIT_INVALID, f"audit list -o TASK_ASSIGN must exit {EXIT_INVALID}, got {code}: {err}"
        assert out == ""
        assert "TASK_ASSIGN" in err, err

    def test_audit_list_task_unassign_operation_filter_rejected(self):
        code, out, err = self.test.run_cmd(
            ["audit", "list", "-r", self.roadmap, "-o", "TASK_UNASSIGN"], check=False)
        assert code == EXIT_INVALID, f"audit list -o TASK_UNASSIGN must exit {EXIT_INVALID}, got {code}: {err}"
        assert out == ""

    # ---- 7. field absent from task get / list / next ----------------------

    def test_field_absent_from_get_list_and_next(self):
        sprint_id = self.test.create_sprint(self.roadmap, "Q3 payments reliability sprint")
        self.test.run_cmd(["sprint", "start", "-r", self.roadmap, str(sprint_id)])
        self.test.run_cmd(["sprint", "add-tasks", "-r", self.roadmap, str(sprint_id), str(self.task_id)])

        got = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(self.task_id)])[0]
        listed = self.test.run_cmd_json(["task", "list", "-r", self.roadmap])
        nxt = self.test.run_cmd_json(["task", "next", "-r", self.roadmap])

        assert listed, "task list returned no rows"
        assert nxt, "task next returned no rows"
        listed_task = next(t for t in listed if t["id"] == self.task_id)
        next_task = next(t for t in nxt if t["id"] == self.task_id)

        for label, task in (("task get", got), ("task list", listed_task), ("task next", next_task)):
            keys = set(task.keys())
            assert keys == EXPECTED_TASK_KEYS, (
                f"{label} JSON shape diverges from the post-removal contract:\n"
                f"  missing: {sorted(EXPECTED_TASK_KEYS - keys)}\n"
                f"  extra:   {sorted(keys - EXPECTED_TASK_KEYS)}"
            )
            assert len(keys) == 20, f"{label}: expected exactly 20 keys, got {len(keys)}: {sorted(keys)}"
            assert "specialists" not in task, f"{label}: specialists key still present: {task!r}"

    # ---- 8. no mention anywhere in help / --ai-help ------------------------

    def test_no_help_output_mentions_specialist(self):
        offenders = []
        for family in HELP_FAMILIES:
            _, out, _ = self.test.run_cmd([family, "--help"], check=False)
            if "specialist" in out.lower():
                offenders.append(f"rmp {family} --help")
        for sub in TASK_SUBCOMMAND_HELPS:
            _, out, _ = self.test.run_cmd(["task", sub, "--help"], check=False)
            if "specialist" in out.lower():
                offenders.append(f"rmp task {sub} --help")
        assert not offenders, f"help output still mentions 'specialist': {offenders}"

    def test_ai_help_does_not_mention_specialist(self):
        _, out, _ = self.test.run_cmd(["--ai-help"], check=False)
        assert "specialist" not in out.lower(), (
            "--ai-help contract still mentions 'specialist'"
        )


class TestSpecialistsMigration1_9_0_to_1_10_0:
    """Schema migration coverage: a 1.9.0-shape database reaches 1.10.0 on the
    next open, the specialists column is gone, and pre-existing TASK_ASSIGN /
    TASK_UNASSIGN audit rows survive (SPEC/VERSION.md § Migration 1.9.0 ->
    1.10.0)."""

    ROADMAP = "regional-clearing-platform"

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    # ---- fixture construction ---------------------------------------------

    def _db_path(self, roadmap):
        return self.test.roadmaps_dir / roadmap / "project.db"

    def _build_1_9_0_fixture(self, roadmap):
        """Create a real roadmap through the CLI (yields correct 1.10.0-shaped
        tables everywhere except `tasks`, since only `tasks` changed between
        1.9.0 and 1.10.0), then rebuild `tasks` to its verbatim 1.9.0 shape,
        seed it with rows that carry specialists values, seed audit rows under
        the retired TASK_ASSIGN/TASK_UNASSIGN operations exactly as the 1.9.0
        binary would have written them, and roll the recorded schema_version
        back to 1.9.0. Returns the seeded ids and expected counts so the
        assertions after migration compare against real values, not
        restated literals."""
        self.test.create_roadmap(roadmap)
        db_path = self._db_path(roadmap)
        assert db_path.exists(), f"precondition: CLI must have created {db_path}"

        con = sqlite3.connect(str(db_path))
        try:
            # The table is still empty, so the drop/rebuild cannot cascade.
            con.execute("DROP TABLE tasks")
            con.executescript(TASKS_DDL_1_9_0)

            now = "2025-11-03T09:15:00.000Z"
            insert = (
                "INSERT INTO tasks (title, status, type, functional_requirements, "
                "technical_requirements, acceptance_criteria, created_at, specialists, "
                "priority, severity, parent_task_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
            )

            def seed_task(title, specialists, priority, severity, parent=None):
                cur = con.execute(insert, (
                    title, "BACKLOG", "TASK",
                    "Settlement windows must reconcile against the acquirer report to the cent.",
                    "Read both ledgers per window and compare the totals.",
                    "An unbalanced window raises an alert within one hour.",
                    now, specialists, priority, severity, parent,
                ))
                return cur.lastrowid

            parent_id = seed_task(
                "Migrate the settlement ledger to the new acquirer schema",
                "go-developer,storage-systems-auditor", 9, 2)
            child_id = seed_task(
                "Backfill the acquirer reference on historical settlements",
                "go-developer", 6, 1, parent=parent_id)
            unstaffed_id = seed_task(
                "Publish the reconciliation runbook", None, 3, 0)

            audit_insert = (
                "INSERT INTO audit (operation, entity_type, entity_id, performed_at) "
                "VALUES (?, ?, ?, ?)"
            )
            audit_rows = [
                ("TASK_CREATE", "TASK", parent_id),
                ("TASK_ASSIGN", "TASK", parent_id),
                ("TASK_ASSIGN", "TASK", child_id),
                ("TASK_UNASSIGN", "TASK", parent_id),
            ]
            for op, entity_type, entity_id in audit_rows:
                con.execute(audit_insert, (op, entity_type, entity_id, now))

            con.execute("UPDATE _metadata SET value = '1.9.0' WHERE key = 'schema_version'")
            con.commit()
        finally:
            con.close()

        return {
            "parent_id": parent_id,
            "child_id": child_id,
            "unstaffed_id": unstaffed_id,
            "audit_row_count": len(audit_rows),
        }

    def _current_schema_version(self):
        """The schema_version a freshly created roadmap is stamped with.

        The assertions below compare a MIGRATED database against this rather
        than against a hard-coded number. Opening a 1.9.0-shape database runs
        the WHOLE migration chain, not only the 1.9.0 -> 1.10.0 step under test,
        so the database lands on whatever the newest version is; what the test
        must prove is that a migrated database and a fresh one agree, which is
        exactly the guarantee SPEC/VERSION.md makes and which does not rot on
        the next schema bump.
        """
        fresh = self.test.create_roadmap()
        return self._schema_version(fresh)

    def _schema_version(self, roadmap):
        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            row = con.execute(
                "SELECT value FROM _metadata WHERE key = 'schema_version'").fetchone()
            return row[0] if row else None
        finally:
            con.close()

    def _specialists_column_count(self, roadmap):
        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            row = con.execute(
                "SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name = 'specialists'"
            ).fetchone()
            return row[0]
        finally:
            con.close()

    def _audit_op_counts(self, roadmap):
        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            rows = con.execute(
                "SELECT operation, COUNT(*) FROM audit GROUP BY operation").fetchall()
            return dict(rows)
        finally:
            con.close()

    # ---- 9. migration drops the column, preserves the rest ----------------

    def test_migration_drops_column_and_preserves_other_data(self):
        roadmap = self.ROADMAP
        fx = self._build_1_9_0_fixture(roadmap)

        # A precondition check on the fixture itself: it really is 1.9.0-shaped
        # before any command touches it (non-vacuousness of the test below).
        assert self._schema_version(roadmap) == "1.9.0"
        assert self._specialists_column_count(roadmap) == 1

        # Any real command reopens the database and runs the pending migration.
        listed = self.test.run_cmd_json(["task", "list", "-r", roadmap])

        assert self._schema_version(roadmap) == self._current_schema_version(), (
            "the next open after a 1.9.0-shape database must run the migration "
            "chain to its end, landing on the same version a fresh roadmap is "
            "created at"
        )
        assert self._specialists_column_count(roadmap) == 0, (
            "tasks.specialists must be dropped by the 1.9.0 -> 1.10.0 migration"
        )
        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            try:
                con.execute("SELECT specialists FROM tasks")
                raised = False
            except sqlite3.OperationalError:
                raised = True
            assert raised, "SELECT specialists must fail: the column must be truly gone, not merely hidden"
        finally:
            con.close()

        # Every task row survives with its non-specialists data intact, read
        # back through the production CLI path (not raw SQL).
        assert len(listed) == 3, f"expected 3 surviving tasks, got {len(listed)}"
        by_id = {t["id"]: t for t in listed}
        assert "specialists" not in by_id[fx["child_id"]]
        assert by_id[fx["child_id"]]["title"] == (
            "Backfill the acquirer reference on historical settlements")
        assert by_id[fx["child_id"]]["priority"] == 6
        assert by_id[fx["child_id"]]["severity"] == 1
        assert by_id[fx["child_id"]]["parent_task_id"] == fx["parent_id"], (
            "parent_task_id self-reference must survive the column drop"
        )
        assert by_id[fx["parent_id"]]["subtask_count"] == 1

        # The CHECK constraints that survive the drop still bite via the CLI.
        code, _, _ = self.test.run_cmd(
            ["task", "create", "-r", roadmap, "-t", "Priority out of range",
             "-fr", "f", "-tr", "t", "-ac", "a", "-p", "10"], check=False)
        assert code == EXIT_INVALID, f"priority CHECK must still be enforced post-migration, got {code}"

        # AUTOINCREMENT continues rather than restarting.
        new_id = self.test.run_cmd_json(
            ["task", "create", "-r", roadmap, "-t", "Retire the legacy settlement importer",
             "-fr", "The legacy importer must stop running.",
             "-tr", "Remove the cron entry and the importer binary.",
             "-ac", "No import runs after the cutover window."])["id"]
        assert new_id > fx["unstaffed_id"], (
            f"new task id {new_id} is not above the highest pre-migration id "
            f"{fx['unstaffed_id']}; AUTOINCREMENT did not continue"
        )

    # ---- 10. TASK_ASSIGN / TASK_UNASSIGN audit rows survive ---------------

    def test_migration_retains_assignment_audit_rows(self):
        roadmap = self.ROADMAP + "-audit"
        fx = self._build_1_9_0_fixture(roadmap)

        # Trigger the migration.
        self.test.run_cmd(["task", "list", "-r", roadmap])

        # Not one audit row was deleted (direct SQL: ground truth on-disk).
        counts = self._audit_op_counts(roadmap)
        assert counts.get("TASK_ASSIGN") == 2, counts
        assert counts.get("TASK_UNASSIGN") == 1, counts
        assert sum(counts.values()) == fx["audit_row_count"], counts

        # They still appear in an UNFILTERED `audit list` through the
        # production read path — "retained on purpose", not merely on disk.
        entries = self.test.run_cmd_json(["audit", "list", "-r", roadmap])
        by_op = {}
        for e in entries:
            by_op[e["operation"]] = by_op.get(e["operation"], 0) + 1
        assert by_op.get("TASK_ASSIGN") == 2, by_op
        assert by_op.get("TASK_UNASSIGN") == 1, by_op

        # And in `audit stats`, which reads the same table.
        stats = self.test.run_cmd_json(["audit", "stats", "-r", roadmap])
        assert stats["by_operation"].get("TASK_ASSIGN") == 2, stats
        assert stats["by_operation"].get("TASK_UNASSIGN") == 1, stats
        assert stats["total_entries"] == fx["audit_row_count"], stats

        # But they are unreachable BY NAME through the `-o` filter: the
        # retired operations left the valid set even though their rows live on.
        code, out, err = self.test.run_cmd(
            ["audit", "list", "-r", roadmap, "-o", "TASK_ASSIGN"], check=False)
        assert code == EXIT_INVALID, f"expected exit {EXIT_INVALID}, got {code}: {err}"
        assert out == ""

    # ---- 11. idempotent second open ----------------------------------------

    def test_second_open_after_migration_is_idempotent(self):
        roadmap = self.ROADMAP + "-idempotent"
        self._build_1_9_0_fixture(roadmap)

        first = self.test.run_cmd_json(["task", "list", "-r", roadmap])
        first_version = self._schema_version(roadmap)
        first_audit = self._audit_op_counts(roadmap)

        code, _, err = self.test.run_cmd(["task", "list", "-r", roadmap], check=False)
        assert code == 0, f"a second open of an already-migrated db must succeed, got {code}: {err}"

        second = self.test.run_cmd_json(["task", "list", "-r", roadmap])
        assert self._schema_version(roadmap) == first_version == self._current_schema_version()
        assert self._audit_op_counts(roadmap) == first_audit
        assert [t["id"] for t in second] == [t["id"] for t in first], (
            "a second open must not alter the migrated data"
        )
        assert self._specialists_column_count(roadmap) == 0


def _run_all():
    passed = 0
    failed = 0
    failures = []
    for cls in (TestSpecialistsFieldRemoved, TestSpecialistsMigration1_9_0_to_1_10_0):
        method_names = [m for m in dir(cls) if m.startswith("test_")]
        for m in method_names:
            instance = cls()
            instance.setup_method()
            try:
                getattr(instance, m)()
                passed += 1
                print(f"✓ {cls.__name__}.{m}")
            except AssertionError as exc:
                failed += 1
                failures.append((f"{cls.__name__}.{m}", exc))
                print(f"✗ {cls.__name__}.{m}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((f"{cls.__name__}.{m}", exc))
                print(f"✗ {cls.__name__}.{m} (error)")
            finally:
                instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Specialists field removal tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
