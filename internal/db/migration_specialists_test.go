package db

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== MIGRATION 1.9.0 -> 1.10.0 ====================
//
// The regression gates for rmp task #246: the tasks.specialists column is
// dropped, its values are deliberately destroyed, and the two assignment audit
// operations are retired from the valid set WITHOUT deleting the rows already
// recorded under them (SPEC/VERSION.md § Migration 1.9.0 → 1.10.0,
// SPEC/DATABASE.md § Migration Idempotency (ALTER TABLE DROP COLUMN)).

// tasksDDL190 is the tasks table exactly as schema 1.9.0 declared it: the
// specialists column sits in the middle of the nullable tracking group, between
// created_at and started_at, and every CHECK, DEFAULT, index and the
// parent_task_id self-reference are the ones that shipped.
//
// The DDL is transcribed rather than derived, for the same reason
// migrateV1_8_0_toV1_9_0 carries its own copy: a fixture for a historical schema
// must not follow later changes to the fresh-schema definition, or the migration
// it exercises stops being the migration that ships. Dropping a column from the
// MIDDLE of the table is what the shipped databases require, and it is the case
// a fixture that merely appends the column at the end would never reach.
const tasksDDL190 = `
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
`

// specialistsFixture records what buildRoadmapAtSchema190 seeded, so the
// assertions after the migration compare against real values rather than
// restating literals.
type specialistsFixture struct {
	parentTaskID  int
	childTaskID   int
	unstaffedID   int
	auditRowCount int
}

// buildRoadmapAtSchema190 creates a real on-disk roadmap under the test HOME and
// takes it back to schema 1.9.0.
//
// The roadmap is first created through the production path, which yields the
// current schema and every table 1.9.0 had; the tasks table is then replaced by
// its verbatim 1.9.0 declaration (tasksDDL190) and repopulated with rows that
// carry specialists values. Only tasks changed between 1.9.0 and 1.10.0, so
// rebuilding that one table is enough to produce a faithful 1.9.0 database while
// the other seven tables stay correct by construction.
//
// Two audit rows are written under the retired operations TASK_ASSIGN and
// TASK_UNASSIGN, exactly as the 1.9.0 binary wrote them, so the retention rule
// is asserted against rows a real roadmap would hold.
func buildRoadmapAtSchema190(t *testing.T, roadmapName string) specialistsFixture {
	t.Helper()

	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("creating roadmap %q: %v", roadmapName, err)
	}

	// Replace the tasks table with its 1.9.0 shape. The table is still empty at
	// this point, so no rows depend on it and the drop cannot cascade.
	if _, err := database.Exec("DROP TABLE tasks"); err != nil {
		t.Fatalf("dropping the current tasks table: %v", err)
	}
	if _, err := database.Exec(tasksDDL190); err != nil {
		t.Fatalf("creating the 1.9.0 tasks table: %v", err)
	}

	now := utils.NowISO8601()

	// Seed three tasks: two carrying specialists values whose loss is the point
	// of the migration, and one that never had any. The child task exercises the
	// parent_task_id self-reference across the drop.
	insert := `INSERT INTO tasks (title, status, type, functional_requirements, technical_requirements,
	                              acceptance_criteria, created_at, specialists, priority, severity, parent_task_id)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	parentID := insertFixtureTask(t, database, insert, &fixtureTaskRow{
		title:       "Migrate the settlement ledger to the new acquirer schema",
		specialists: sql.NullString{String: "go-developer,storage-systems-auditor", Valid: true},
		priority:    9,
		severity:    2,
		createdAt:   now,
	})
	childID := insertFixtureTask(t, database, insert, &fixtureTaskRow{
		title:       "Backfill the acquirer reference on historical settlements",
		specialists: sql.NullString{String: "go-developer", Valid: true},
		priority:    6,
		severity:    1,
		createdAt:   now,
		parent:      sql.NullInt64{Int64: int64(parentID), Valid: true},
	})
	unstaffedID := insertFixtureTask(t, database, insert, &fixtureTaskRow{
		title:     "Publish the reconciliation runbook",
		priority:  3,
		severity:  0,
		createdAt: now,
	})

	// Audit rows written by the 1.9.0 binary, through the same LogAuditTx every
	// production writer uses. The two retired operations are passed as plain
	// strings because the constants no longer exist — which is precisely the
	// contract break these rows must survive.
	for _, row := range []struct {
		op       models.AuditOperation
		entityID int
	}{
		{models.OpTaskCreate, parentID},
		{models.AuditOperation("TASK_ASSIGN"), parentID},
		{models.AuditOperation("TASK_ASSIGN"), childID},
		{models.AuditOperation("TASK_UNASSIGN"), parentID},
	} {
		if err := database.WithTransaction(func(tx *sql.Tx) error {
			return LogAuditTx(tx, row.op, models.EntityTask, row.entityID, now)
		}); err != nil {
			t.Fatalf("seeding audit row %s for task %d: %v", row.op, row.entityID, err)
		}
	}

	if _, err := database.Exec(
		"UPDATE _metadata SET value = '1.9.0' WHERE key = 'schema_version'"); err != nil {
		t.Fatalf("setting schema_version to 1.9.0: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("closing the 1.9.0 fixture: %v", err)
	}

	return specialistsFixture{
		parentTaskID:  parentID,
		childTaskID:   childID,
		unstaffedID:   unstaffedID,
		auditRowCount: 4,
	}
}

// fixtureTaskRow is one seeded task in the 1.9.0 fixture.
type fixtureTaskRow struct {
	specialists sql.NullString
	parent      sql.NullInt64
	title       string
	createdAt   string
	priority    int
	severity    int
}

// insertFixtureTask writes one fixture row and returns its id. The statement is
// parameterized; nothing from the row reaches the SQL text.
func insertFixtureTask(t *testing.T, database *DB, insert string, row *fixtureTaskRow) int {
	t.Helper()

	res, err := database.Exec(insert,
		row.title,
		string(models.StatusBacklog),
		string(models.TypeTask),
		"Settlement windows must reconcile against the acquirer report to the cent.",
		"Read both ledgers per window and compare the totals.",
		"An unbalanced window raises an alert within one hour.",
		row.createdAt,
		row.specialists,
		row.priority,
		row.severity,
		row.parent,
	)
	if err != nil {
		t.Fatalf("seeding task %q: %v", row.title, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("reading the id of seeded task %q: %v", row.title, err)
	}
	return int(id)
}

// columnCount reports how many columns of table are named column, read through
// pragma_table_info — the same primitive the migration's guard uses.
func columnCount(t *testing.T, database *DB, table, column string) int {
	t.Helper()

	var n int
	// table is a test-local literal; column is bound.
	query := "SELECT COUNT(*) FROM pragma_table_info('" + table + "') WHERE name = ?"
	if err := database.QueryRow(query, column).Scan(&n); err != nil {
		t.Fatalf("counting %s.%s: %v", table, column, err)
	}
	return n
}

// TestMigrateV1_9_0_toV1_10_0_OnNextOpen is the gate for acceptance criterion 1
// of rmp task #246: a database created at 1.9.0 must reach 1.10.0 on the next
// open, with NO user action, and must come out with no specialists column.
//
// It also pins what the drop is required to preserve, which is everything else:
// SPEC/DATABASE.md § Migration Idempotency (ALTER TABLE DROP COLUMN) states that
// the remaining columns keep their values, their CHECK constraints and their
// DEFAULT clauses, that the indexes and foreign keys survive, and that
// AUTOINCREMENT continues from where it was. A migration that quietly rebuilt
// the table and lost one of those would still pass a bare "column is gone"
// assertion, so each is asserted here.
func TestMigrateV1_9_0_toV1_10_0_OnNextOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmapName = "acquirer-settlement"
	fx := buildRoadmapAtSchema190(t, roadmapName)

	// The production entry point every rmp command goes through.
	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("opening the 1.9.0 database: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	version, err := database.GetSchemaVersion()
	if err != nil {
		t.Fatalf("reading schema version after open: %v", err)
	}
	// Open runs the WHOLE migration chain, not just the one under test, so the
	// database lands on the current schema version rather than on 1.10.0. What
	// this assertion guards is that the chain ran at all and reached its end;
	// the 1.9.0 -> 1.10.0 step itself is asserted by the column checks below.
	if version != SchemaVersion {
		t.Fatalf("schema_version after open = %q, want %q (the newest version)", version, SchemaVersion)
	}

	if got := columnCount(t, database, "tasks", "specialists"); got != 0 {
		t.Errorf("pragma_table_info('tasks') still reports %d specialists column(s) after the "+
			"migration; SPEC/VERSION.md § Migration 1.9.0 → 1.10.0 requires it gone", got)
	}

	// The column is gone from the table itself, not merely hidden: naming it
	// must now be an error.
	if _, err := database.Query("SELECT specialists FROM tasks"); err == nil { //nolint:rowserrcheck // the query is expected to fail
		t.Error("SELECT specialists still succeeds after the migration; the column was not dropped")
	}

	// Every row survives, with every other value intact.
	assertRowCount(t, database, 3, "tasks survive the drop", "SELECT COUNT(*) FROM tasks")

	var title string
	var priority, severity int
	var parent sql.NullInt64
	if err := database.QueryRow(
		"SELECT title, priority, severity, parent_task_id FROM tasks WHERE id = ?", fx.childTaskID,
	).Scan(&title, &priority, &severity, &parent); err != nil {
		t.Fatalf("re-reading task %d: %v", fx.childTaskID, err)
	}
	if title != "Backfill the acquirer reference on historical settlements" {
		t.Errorf("task %d title = %q after the migration; the drop must not touch other columns",
			fx.childTaskID, title)
	}
	if priority != 6 || severity != 1 {
		t.Errorf("task %d priority/severity = %d/%d after the migration, want 6/1",
			fx.childTaskID, priority, severity)
	}
	if !parent.Valid || int(parent.Int64) != fx.parentTaskID {
		t.Errorf("task %d parent_task_id = %v after the migration, want %d",
			fx.childTaskID, parent, fx.parentTaskID)
	}

	// The seven task indexes survive.
	for _, index := range []string{
		"idx_tasks_status", "idx_tasks_type", "idx_tasks_priority", "idx_tasks_created_at",
		"idx_tasks_status_priority", "idx_tasks_priority_created", "idx_tasks_parent_task_id",
	} {
		var name string
		err := database.QueryRow(
			"SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?", index).Scan(&name)
		if err != nil {
			t.Errorf("index %s did not survive the drop: %v", index, err)
		}
	}

	// The CHECK constraints still bite: priority is capped at 9.
	if _, err := database.Exec(
		`INSERT INTO tasks (title, status, type, functional_requirements, technical_requirements,
		                    acceptance_criteria, created_at, priority, severity)
		 VALUES (?, 'BACKLOG', 'TASK', ?, ?, ?, ?, 10, 0)`,
		"Priority out of range", "f", "t", "a", utils.NowISO8601(),
	); err == nil {
		t.Error("priority = 10 was accepted after the migration; the CHECK constraint did not survive")
	}

	// The parent_task_id self-reference still bites.
	if _, err := database.Exec(
		`INSERT INTO tasks (title, status, type, functional_requirements, technical_requirements,
		                    acceptance_criteria, created_at, priority, severity, parent_task_id)
		 VALUES (?, 'BACKLOG', 'TASK', ?, ?, ?, ?, 0, 0, ?)`,
		"Orphan", "f", "t", "a", utils.NowISO8601(), 999999,
	); err == nil {
		t.Error("a task naming a non-existent parent was accepted after the migration; the " +
			"parent_task_id foreign key did not survive")
	}

	// AUTOINCREMENT continues rather than restarting: a table rebuild that lost
	// sqlite_sequence would hand out an id already used.
	newID, err := database.CreateTask(testContext(), &models.Task{
		Title:                  "Retire the legacy settlement importer",
		Type:                   models.TypeTask,
		Status:                 models.StatusBacklog,
		FunctionalRequirements: "The legacy importer must stop running.",
		TechnicalRequirements:  "Remove the cron entry and the importer binary.",
		AcceptanceCriteria:     "No import runs after the cutover window.",
		CreatedAt:              utils.NowISO8601(),
	})
	if err != nil {
		t.Fatalf("creating a task after the migration: %v", err)
	}
	if newID <= fx.unstaffedID {
		t.Errorf("the task created after the migration got id %d, which is not above the highest "+
			"pre-migration id %d; AUTOINCREMENT did not continue", newID, fx.unstaffedID)
	}
}

// TestAssignmentAuditRowsSurviveTheMigration is the gate for acceptance
// criterion 5 of rmp task #246.
//
// TASK_ASSIGN and TASK_UNASSIGN leave the valid set, but the rows already
// recorded under them are history and the migration deletes none of them:
// audit.operation carries no CHECK, so they need no schema accommodation, and
// they must keep appearing in an unfiltered read (SPEC/DATABASE.md § audit
// Table, "A stored row may carry an operation the catalogue does not list";
// SPEC/VERSION.md § Migration 1.9.0 → 1.10.0, "The audit log is not rewritten").
func TestAssignmentAuditRowsSurviveTheMigration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmapName = "settlement-history"
	fx := buildRoadmapAtSchema190(t, roadmapName)

	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("opening the 1.9.0 database: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	// Not one row was deleted.
	assertRowCount(t, database, fx.auditRowCount,
		"every audit row survives the migration", "SELECT COUNT(*) FROM audit")
	assertRowCount(t, database, 2, "the TASK_ASSIGN rows survive",
		"SELECT COUNT(*) FROM audit WHERE operation = ?", "TASK_ASSIGN")
	assertRowCount(t, database, 1, "the TASK_UNASSIGN row survives",
		"SELECT COUNT(*) FROM audit WHERE operation = ?", "TASK_UNASSIGN")

	// They are returned by the production read path with no filter applied, so
	// a roadmap's recorded history reads complete.
	entries, err := database.GetAuditEntries(testContext(), nil)
	if err != nil {
		t.Fatalf("listing audit entries after the migration: %v", err)
	}
	seen := make(map[string]int, len(entries))
	for i := range entries {
		seen[entries[i].Operation]++
	}
	if seen["TASK_ASSIGN"] != 2 || seen["TASK_UNASSIGN"] != 1 {
		t.Errorf("an unfiltered audit listing returned %d TASK_ASSIGN and %d TASK_UNASSIGN rows, "+
			"want 2 and 1; the retired operations must still be listed", seen["TASK_ASSIGN"], seen["TASK_UNASSIGN"])
	}

	// And they are counted in the statistics, which read the same table.
	stats, err := database.GetAuditStats(testContext(), nil, nil)
	if err != nil {
		t.Fatalf("reading audit stats after the migration: %v", err)
	}
	if stats.ByOperation["TASK_ASSIGN"] != 2 || stats.ByOperation["TASK_UNASSIGN"] != 1 {
		t.Errorf("audit stats report %d TASK_ASSIGN and %d TASK_UNASSIGN, want 2 and 1",
			stats.ByOperation["TASK_ASSIGN"], stats.ByOperation["TASK_UNASSIGN"])
	}
	if stats.TotalEntries != fx.auditRowCount {
		t.Errorf("audit stats report %d entries in total, want %d", stats.TotalEntries, fx.auditRowCount)
	}
}

// TestMigrateV1_9_0_toV1_10_0_IsIdempotent is the gate for acceptance criterion
// 2 of rmp task #246, and it is deliberately in two halves.
//
// The first half runs the migration twice against a database that HAS the
// column: pass one drops it, pass two must be a silent no-op. The second half
// proves the guard is load-bearing rather than decorative, by issuing the
// unguarded statement the migration would otherwise have run and asserting that
// SQLite does reject it with "no such column". Without that half the test would
// pass just as happily against a migration with no guard at all.
func TestMigrateV1_9_0_toV1_10_0_IsIdempotent(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// CreateSchema produced the 1.10.0 shape, so put the column back to obtain a
	// database the migration still has work to do on.
	if _, err := database.Exec("ALTER TABLE tasks ADD COLUMN specialists TEXT"); err != nil {
		t.Fatalf("re-adding the specialists column to build the fixture: %v", err)
	}
	if got := columnCount(t, database, "tasks", "specialists"); got != 1 {
		t.Fatalf("fixture has %d specialists columns, want 1", got)
	}

	for pass := 1; pass <= 2; pass++ {
		tx, err := database.Begin()
		if err != nil {
			t.Fatalf("begin (pass %d): %v", pass, err)
		}
		if err := migrateV1_9_0_toV1_10_0(tx); err != nil {
			tx.Rollback() //nolint:errcheck // the migration error is the one reported
			t.Fatalf("migration pass %d returned an error, so it is not idempotent: %v", pass, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit (pass %d): %v", pass, err)
		}
		if got := columnCount(t, database, "tasks", "specialists"); got != 0 {
			t.Fatalf("after pass %d the tasks table still has %d specialists column(s)", pass, got)
		}
	}

	// The guard is what makes pass two a no-op. Prove the statement it skips
	// would in fact have failed.
	_, err := database.Exec("ALTER TABLE tasks DROP COLUMN specialists")
	if err == nil {
		t.Fatal("an unguarded second DROP COLUMN succeeded, so this test cannot show that the " +
			"column-existence guard is doing any work; the SQLite behaviour it defends against " +
			"has changed and the guard's justification must be re-derived")
	}
	if !strings.Contains(err.Error(), "no such column") {
		t.Errorf("an unguarded second DROP COLUMN failed with %q; the expected failure is "+
			"\"no such column\", which is what SPEC/DATABASE.md § Migration Idempotency "+
			"(ALTER TABLE DROP COLUMN) requires the guard to prevent", err)
	}
}
