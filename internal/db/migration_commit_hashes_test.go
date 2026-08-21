package db

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== MIGRATION 1.10.0 -> 1.11.0 ====================
//
// The regression gates for rmp task #253: the tasks table gains the two
// commit-tracking columns, commit_open and commit_close, each carrying the
// format CHECK, with no backfill and no index (SPEC/VERSION.md § Migration
// 1.10.0 → 1.11.0, SPEC/DATABASE.md § Commit Hash Format Constraint).
//
// The application-layer half of the same rule — models.NormalizeCommitHash — is
// exercised over the same accept/reject matrix in
// internal/models/commit_hash_test.go. The two layers must agree, so the matrix
// below is deliberately the same one.

// tasksDDL1100 is the tasks table exactly as schema 1.10.0 declared it: the two
// commit columns are absent and everything else — every CHECK, DEFAULT, index
// and the parent_task_id self-reference — is what shipped.
//
// The DDL is transcribed rather than derived, for the same reason
// migrateV1_8_0_toV1_9_0 carries its own copy: a fixture for a historical schema
// must not follow later changes to the fresh-schema definition, or the migration
// it exercises stops being the migration that ships.
const tasksDDL1100 = `
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

// Commit hashes used across these tests. They are real hashes of this
// repository, so the fixtures are the values the command layer will actually be
// handed rather than invented digit strings.
const (
	// The abbreviated form git log --oneline prints: the shortest accepted value.
	dbSevenCharHash = "5f93b51"
	// A full SHA-1 commit hash.
	dbSHA1Hash = "1d0f66a0b91387206c493a857d39b9642b477bb2"
	// A second full SHA-1 commit hash, so open and close are distinguishable.
	dbSHA1HashOther = "2578d18e1a6b4c9f0d3e8a7b5c2f1e94d6a08b37"
	// A full SHA-256 commit hash, as a repository created with
	// git init --object-format=sha256 produces.
	dbSHA256Hash = "9a7d3f21c05b48e6ff1c2d84b7e0a6539cc41d8b27fe5a0369b4c1de82f7a05c"
)

// commitHashCase is one row of the accept/reject matrix the CHECK constraint
// must implement. want is whether the storage layer accepts the value.
type commitHashCase struct {
	name  string
	value any // string, or nil for SQL NULL
	why   string
	want  bool
}

// commitHashMatrix is the full storage-layer contract of SPEC/DATABASE.md
// § Commit Hash Format Constraint, in one place so both commit columns, a fresh
// database and a migrated one are all held to exactly the same rule.
var commitHashMatrix = []commitHashCase{
	{
		name: "NULL", value: nil, want: true,
		why: "the column is nullable and NULL is always valid; every task migrates with NULL",
	},
	{
		name: "seven characters, the lower bound", value: dbSevenCharHash, want: true,
		why: "length BETWEEN 7 AND 64 is inclusive at the lower end",
	},
	{
		name: "forty characters, a full SHA-1", value: dbSHA1Hash, want: true,
		why: "SHA-1 is git's default object format",
	},
	{
		name: "sixty-four characters, a full SHA-256", value: dbSHA256Hash, want: true,
		why: "the upper bound exists to admit git init --object-format=sha256",
	},
	{
		name: "six characters, one below the lower bound", value: "5f93b5", want: false,
		why: "length 6 is outside BETWEEN 7 AND 64",
	},
	{
		name: "sixty-five characters, one above the upper bound", value: dbSHA256Hash + "0", want: false,
		why: "length 65 is outside BETWEEN 7 AND 64",
	},
	{
		name: "empty string", value: "", want: false,
		why: "length 0 is below the lower bound; the empty string is not a stand-in for NULL",
	},
	{
		name: "non-hexadecimal letter", value: "5f93b5g", want: false,
		why: "g is matched by GLOB '*[^0-9a-f]*'",
	},
	{
		name: "uppercase hexadecimal", value: strings.ToUpper(dbSHA1Hash), want: false,
		why: "SQLite's GLOB is case-sensitive, so the CHECK is the backstop for the " +
			"application's lowercase normalisation rather than a restatement of the hex alphabet",
	},
	{
		name: "leading space", value: " " + dbSHA1Hash, want: false,
		why: "the application does not trim, and a space is matched by GLOB '*[^0-9a-f]*'",
	},
	{
		name: "trailing space", value: dbSHA1Hash + " ", want: false,
		why: "the application does not trim, and a space is matched by GLOB '*[^0-9a-f]*'",
	},
}

// commitFixture records what buildRoadmapAtSchema1100 seeded, so the assertions
// after the migration compare against real values rather than restating literals.
type commitFixture struct {
	completedID int
	doingID     int
	backlogID   int
	childID     int
	taskCount   int
}

// buildRoadmapAtSchema1100 creates a real on-disk roadmap under the test HOME
// and takes it back to schema 1.10.0.
//
// The roadmap is first created through the production path, which yields the
// current schema and every table 1.10.0 had; the tasks table is then replaced by
// its verbatim 1.10.0 declaration (tasksDDL1100) and repopulated. Only tasks
// changed between 1.10.0 and 1.11.0, so rebuilding that one table is enough to
// produce a faithful 1.10.0 database while the other seven tables stay correct by
// construction.
//
// The seeded rows are the ones the no-backfill rule is about: a COMPLETED task
// that reached its end state before the columns existed, a DOING task that was
// started before they existed, a task still in BACKLOG, and a child task that
// exercises the parent_task_id self-reference across the migration. None of them
// can be given a truthful commit hash by any migration, which is exactly why
// SPEC/VERSION.md § Migration 1.10.0 → 1.11.0 refuses to invent one.
func buildRoadmapAtSchema1100(t *testing.T, roadmapName string) commitFixture {
	t.Helper()

	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("creating roadmap %q: %v", roadmapName, err)
	}

	// Replace the tasks table with its 1.10.0 shape. The table is still empty at
	// this point, so no rows depend on it and the drop cannot cascade.
	if _, err := database.Exec("DROP TABLE tasks"); err != nil {
		t.Fatalf("dropping the current tasks table: %v", err)
	}
	if _, err := database.Exec(tasksDDL1100); err != nil {
		t.Fatalf("creating the 1.10.0 tasks table: %v", err)
	}

	now := utils.NowISO8601()

	insert := `INSERT INTO tasks (title, status, type, functional_requirements, technical_requirements,
	                              acceptance_criteria, created_at, started_at, tested_at, closed_at,
	                              completion_summary, priority, severity, parent_task_id)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	completedID := insertCommitFixtureTask(t, database, insert, &commitFixtureRow{
		title:     "Rotate the payment gateway signing keys",
		status:    models.StatusCompleted,
		createdAt: now,
		startedAt: sql.NullString{String: now, Valid: true},
		testedAt:  sql.NullString{String: now, Valid: true},
		closedAt:  sql.NullString{String: now, Valid: true},
		summary: sql.NullString{
			String: "Keys rotated in both regions and the old pair revoked.",
			Valid:  true,
		},
		priority: 9,
		severity: 3,
	})
	doingID := insertCommitFixtureTask(t, database, insert, &commitFixtureRow{
		title:     "Move the webhook dispatcher onto the retry queue",
		status:    models.StatusDoing,
		createdAt: now,
		startedAt: sql.NullString{String: now, Valid: true},
		priority:  7,
		severity:  1,
	})
	backlogID := insertCommitFixtureTask(t, database, insert, &commitFixtureRow{
		title:     "Document the acquirer failover procedure",
		status:    models.StatusBacklog,
		createdAt: now,
		priority:  4,
	})
	childID := insertCommitFixtureTask(t, database, insert, &commitFixtureRow{
		title:     "Add the acquirer failover runbook to the on-call handbook",
		status:    models.StatusBacklog,
		createdAt: now,
		priority:  2,
		parent:    sql.NullInt64{Int64: int64(backlogID), Valid: true},
	})

	if _, err := database.Exec(
		"UPDATE _metadata SET value = '1.10.0' WHERE key = 'schema_version'"); err != nil {
		t.Fatalf("setting schema_version to 1.10.0: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("closing the 1.10.0 fixture: %v", err)
	}

	return commitFixture{
		completedID: completedID,
		doingID:     doingID,
		backlogID:   backlogID,
		childID:     childID,
		taskCount:   4,
	}
}

// commitFixtureRow is one seeded task in the 1.10.0 fixture.
type commitFixtureRow struct {
	startedAt sql.NullString
	testedAt  sql.NullString
	closedAt  sql.NullString
	summary   sql.NullString
	parent    sql.NullInt64
	title     string
	createdAt string
	status    models.TaskStatus
	priority  int
	severity  int
}

// insertCommitFixtureTask writes one fixture row and returns its id. The
// statement is parameterized; nothing from the row reaches the SQL text.
func insertCommitFixtureTask(t *testing.T, database *DB, insert string, row *commitFixtureRow) int {
	t.Helper()

	res, err := database.Exec(insert,
		row.title,
		string(row.status),
		string(models.TypeTask),
		"Card settlements must keep clearing while the acquirer is unavailable.",
		"Queue the dispatch and replay it once the acquirer answers again.",
		"A simulated acquirer outage loses no settlement.",
		row.createdAt,
		row.startedAt,
		row.testedAt,
		row.closedAt,
		row.summary,
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

// setCommitColumn attempts to write value into the named commit column of the
// named task and reports the error, if any. column is a test-local literal; the
// value is bound.
func setCommitColumn(database *DB, column string, taskID int, value any) error {
	// column is one of two test-local literals at every call site; the value is
	// bound. Matches how columnCount builds its pragma_table_info query.
	_, err := database.Exec("UPDATE tasks SET "+column+" = ? WHERE id = ?", value, taskID)
	return err
}

// readCommitColumns returns the two commit columns of the named task.
func readCommitColumns(t *testing.T, database *DB, taskID int) (open, close sql.NullString) {
	t.Helper()
	if err := database.QueryRow(
		"SELECT commit_open, commit_close FROM tasks WHERE id = ?", taskID,
	).Scan(&open, &close); err != nil {
		t.Fatalf("reading the commit columns of task %d: %v", taskID, err)
	}
	return open, close
}

// TestMigrateV1_10_0_toV1_11_0_OnNextOpen is the primary gate: a database
// created at 1.10.0 must reach the current schema version on the next open, with
// NO user action, gaining both commit columns while every existing row survives
// untouched and receives NULL in each of them.
//
// The version it lands on is the newest one, not 1.11.0: RunMigrations applies
// every pending migration in order, so a 1.10.0 database passes through this
// migration and continues to the current schema. What this test pins is the
// EFFECT of the 1.10.0 to 1.11.0 step; that later migrations run afterwards is
// what TestSchemaVersionConstant covers.
func TestMigrateV1_10_0_toV1_11_0_OnNextOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmapName = "payment-gateway"
	fx := buildRoadmapAtSchema1100(t, roadmapName)

	// The production entry point every rmp command goes through.
	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("opening the 1.10.0 database: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	version, err := database.GetSchemaVersion()
	if err != nil {
		t.Fatalf("reading schema version after open: %v", err)
	}
	if version != "1.12.0" {
		t.Fatalf("schema_version after open = %q, want 1.12.0 (SPEC/VERSION.md § Current Schema Version)", version)
	}
	if version != SchemaVersion {
		t.Errorf("schema_version after open = %q but the SchemaVersion constant is %q; a migrated "+
			"database must land on the version a fresh one is created at", version, SchemaVersion)
	}

	for _, column := range []string{"commit_open", "commit_close"} {
		if got := columnCount(t, database, "tasks", column); got != 1 {
			t.Errorf("pragma_table_info('tasks') reports %d %s column(s) after the migration, want 1",
				got, column)
		}
	}

	// Every row survives, and the migration touched nothing else.
	assertRowCount(t, database, fx.taskCount, "every task survives the migration",
		"SELECT COUNT(*) FROM tasks")

	// THE NO-BACKFILL RULE: both columns are NULL on every pre-existing task,
	// including the one that was already COMPLETED and the one already in DOING.
	// Groadmap holds no truthful value for either, and inventing one is exactly
	// what SPEC/VERSION.md § Migration 1.10.0 → 1.11.0 refuses to do.
	assertRowCount(t, database, fx.taskCount, "no task was backfilled",
		"SELECT COUNT(*) FROM tasks WHERE commit_open IS NULL AND commit_close IS NULL")

	for _, id := range []int{fx.completedID, fx.doingID, fx.backlogID, fx.childID} {
		open, closed := readCommitColumns(t, database, id)
		if open.Valid || closed.Valid {
			t.Errorf("task %d migrated with commit_open=%v commit_close=%v; every pre-existing task "+
				"must migrate with NULL in both", id, open, closed)
		}
	}

	// The rest of the COMPLETED task is intact: the migration adds columns, it
	// does not rewrite rows.
	var title, closedAt, summary string
	var priority, severity int
	if err := database.QueryRow(
		"SELECT title, closed_at, completion_summary, priority, severity FROM tasks WHERE id = ?",
		fx.completedID,
	).Scan(&title, &closedAt, &summary, &priority, &severity); err != nil {
		t.Fatalf("re-reading task %d: %v", fx.completedID, err)
	}
	if title != "Rotate the payment gateway signing keys" {
		t.Errorf("task %d title = %q after the migration; the ADD COLUMN must not touch other columns",
			fx.completedID, title)
	}
	if closedAt == "" || summary == "" {
		t.Errorf("task %d lost closed_at (%q) or completion_summary (%q) across the migration",
			fx.completedID, closedAt, summary)
	}
	if priority != 9 || severity != 3 {
		t.Errorf("task %d priority/severity = %d/%d after the migration, want 9/3",
			fx.completedID, priority, severity)
	}

	// The parent_task_id self-reference survives.
	var parent sql.NullInt64
	if err := database.QueryRow(
		"SELECT parent_task_id FROM tasks WHERE id = ?", fx.childID).Scan(&parent); err != nil {
		t.Fatalf("re-reading the parent of task %d: %v", fx.childID, err)
	}
	if !parent.Valid || int(parent.Int64) != fx.backlogID {
		t.Errorf("task %d parent_task_id = %v after the migration, want %d",
			fx.childID, parent, fx.backlogID)
	}

	// THE CHECK CONSTRAINT IS LIVE ON THE MIGRATED TABLE. SQLite accepts a CHECK
	// clause on ADD COLUMN and enforces it from that moment on, which is what
	// lets the migrated schema match the fresh one (SPEC/VERSION.md § Migration
	// 1.10.0 → 1.11.0). Without this the two would silently diverge: the columns
	// would exist but accept anything.
	assertCommitCheckMatrix(t, database, fx.doingID, "migrated")

	// No index is created on either column: neither is a filter, a sort key or a
	// join key for any query (SPEC/VERSION.md § Migration 1.10.0 → 1.11.0).
	assertRowCount(t, database, 0, "the migration creates no index on either commit column",
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'tasks'
		   AND (IFNULL(sql, '') LIKE '%commit_open%' OR IFNULL(sql, '') LIKE '%commit_close%')`)
}

// assertCommitCheckMatrix runs the whole storage-layer accept/reject matrix
// against both commit columns of the named task, and restores NULL afterwards so
// the caller's fixture is unchanged.
func assertCommitCheckMatrix(t *testing.T, database *DB, taskID int, what string) {
	t.Helper()

	for _, column := range []string{"commit_open", "commit_close"} {
		for _, tc := range commitHashMatrix {
			err := setCommitColumn(database, column, taskID, tc.value)
			switch {
			case tc.want && err != nil:
				t.Errorf("[%s] %s rejected %s (%v); SPEC/DATABASE.md § Commit Hash Format "+
					"Constraint accepts it because %s", what, column, tc.name, err, tc.why)
			case !tc.want && err == nil:
				t.Errorf("[%s] %s accepted %s (%#v); SPEC/DATABASE.md § Commit Hash Format "+
					"Constraint rejects it because %s", what, column, tc.name, tc.value, tc.why)
			case !tc.want && err != nil && !strings.Contains(err.Error(), "CHECK constraint failed"):
				t.Errorf("[%s] %s rejected %s with %q; the rejection must come from the CHECK "+
					"constraint, not from something else", what, column, tc.name, err)
			}
		}
		if err := setCommitColumn(database, column, taskID, nil); err != nil {
			t.Fatalf("[%s] restoring %s to NULL: %v", what, column, err)
		}
	}
}

// TestCommitColumnCheckConstraintOnAFreshSchema holds a freshly created database
// to the same matrix the migrated one is held to, so the CHECK in
// CreateSchema's DDL and the CHECK in the migration's ADD COLUMN statements are
// proven equivalent rather than assumed so.
func TestCommitColumnCheckConstraintOnAFreshSchema(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, err := database.CreateTask(testContext(), &models.Task{
		Title:                  "Settle the acquirer reconciliation window",
		Type:                   models.TypeTask,
		Status:                 models.StatusBacklog,
		FunctionalRequirements: "Every settlement window must reconcile to the cent.",
		TechnicalRequirements:  "Compare both ledgers per window and report the delta.",
		AcceptanceCriteria:     "An unbalanced window raises an alert within one hour.",
		CreatedAt:              utils.NowISO8601(),
	})
	if err != nil {
		t.Fatalf("creating the fixture task: %v", err)
	}

	// A task created through the production write path starts with NULL in both
	// columns: `task create` does not accept a hash (SPEC/COMMANDS.md).
	open, closed := readCommitColumns(t, database, taskID)
	if open.Valid || closed.Valid {
		t.Errorf("a newly created task has commit_open=%v commit_close=%v, want NULL in both",
			open, closed)
	}

	assertCommitCheckMatrix(t, database, taskID, "fresh")
}

// TestMigratedAndFreshTasksTablesEnforceTheSameCommitRule pins the guarantee
// SPEC/VERSION.md § Migration 1.10.0 → 1.11.0 states: because the CHECK travels
// with each ADD COLUMN statement, a migrated database enforces the commit-hash
// format exactly as a fresh one does.
//
// The property asserted is behavioural, not textual, and deliberately so. ALTER
// TABLE ADD COLUMN can only append, so the migrated table lists the two columns
// after parent_task_id while the fresh table declares them before it; the DDL
// text in sqlite_master therefore differs and always will. That difference is
// immaterial — every read path names its columns explicitly — whereas a
// difference in what the two tables ACCEPT would be a genuine schema fork, and
// is what this test rules out.
//
// The DDL is intentionally duplicated between CreateSchema and
// migrateV1_10_0_toV1_11_0 (a migration is a historical record of one version's
// shape), so this test is what stops the two copies drifting apart.
func TestMigratedAndFreshTasksTablesEnforceTheSameCommitRule(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	migrated := func() *DB {
		buildRoadmapAtSchema1100(t, "migrated-shape")
		database, err := Open("migrated-shape")
		if err != nil {
			t.Fatalf("opening the migrated database: %v", err)
		}
		return database
	}()
	defer migrated.Close() //nolint:errcheck // test cleanup

	fresh, err := Open("fresh-shape")
	if err != nil {
		t.Fatalf("creating the fresh database: %v", err)
	}
	defer fresh.Close() //nolint:errcheck // test cleanup

	// 1. Both tables declare both columns, with the same type and the same
	//    nullability.
	for _, column := range []string{"commit_open", "commit_close"} {
		freshCol := commitColumnInfo(t, fresh, column)
		migratedCol := commitColumnInfo(t, migrated, column)
		if freshCol != migratedCol {
			t.Errorf("%s is %+v on a fresh database but %+v on a migrated one", column, freshCol, migratedCol)
		}
		if freshCol.typ != "TEXT" {
			t.Errorf("%s has type %q, want TEXT (SPEC/DATABASE.md § tasks Table)", column, freshCol.typ)
		}
		if freshCol.notNull != 0 {
			t.Errorf("%s is NOT NULL; SPEC/VERSION.md § Migration 1.10.0 → 1.11.0 requires it "+
				"nullable, because no truthful value exists for work already done", column)
		}
	}

	// 2. Both tables answer the whole accept/reject matrix identically. This is
	//    the property the SPEC actually promises.
	freshTask := seedCommitCheckTask(t, fresh)
	migratedTask := seedCommitCheckTask(t, migrated)
	assertCommitCheckMatrix(t, fresh, freshTask, "fresh")
	assertCommitCheckMatrix(t, migrated, migratedTask, "migrated")

	// 3. Neither carries an index on either column.
	for _, c := range []struct {
		name string
		db   *DB
	}{{"fresh", fresh}, {"migrated", migrated}} {
		assertRowCount(t, c.db, 0, c.name+" database has no index on either commit column",
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'tasks'
			   AND (IFNULL(sql, '') LIKE '%commit_open%' OR IFNULL(sql, '') LIKE '%commit_close%')`)
	}
}

// commitColumnDescriptor is the part of pragma_table_info that has to agree
// between a fresh and a migrated database. The column's ORDINAL POSITION is
// deliberately excluded: ADD COLUMN can only append, so it cannot agree, and no
// read path depends on it.
type commitColumnDescriptor struct {
	typ        string
	dfltValue  sql.NullString
	notNull    int
	primaryKey int
}

// commitColumnInfo reads one column's declaration from pragma_table_info.
func commitColumnInfo(t *testing.T, database *DB, column string) commitColumnDescriptor {
	t.Helper()
	var d commitColumnDescriptor
	if err := database.QueryRow(
		`SELECT type, "notnull", dflt_value, pk FROM pragma_table_info('tasks') WHERE name = ?`, column,
	).Scan(&d.typ, &d.notNull, &d.dfltValue, &d.primaryKey); err != nil {
		t.Fatalf("reading the declaration of tasks.%s: %v", column, err)
	}
	return d
}

// seedCommitCheckTask creates one task through the production write path and
// returns its id.
func seedCommitCheckTask(t *testing.T, database *DB) int {
	t.Helper()
	id, err := database.CreateTask(testContext(), &models.Task{
		Title:                  "Replay the failed settlement batch",
		Type:                   models.TypeTask,
		Status:                 models.StatusBacklog,
		FunctionalRequirements: "A failed batch must be replayable without duplicating a payment.",
		TechnicalRequirements:  "Key the replay on the batch id and make the write idempotent.",
		AcceptanceCriteria:     "Replaying the same batch twice settles it once.",
		CreatedAt:              utils.NowISO8601(),
	})
	if err != nil {
		t.Fatalf("creating the fixture task: %v", err)
	}
	return id
}

// TestMigrateV1_10_0_toV1_11_0_IsIdempotent has three parts.
//
// The first runs the migration twice against a database that has NEITHER column:
// pass one adds both, pass two must be a silent no-op. The second proves the
// guards are load-bearing rather than decorative, by issuing the unguarded
// statement the migration would otherwise have run and asserting that SQLite does
// reject it with "duplicate column name". Without that part the test would pass
// just as happily against a migration with no guard at all.
//
// The third covers what SPEC/VERSION.md § Migration 1.10.0 → 1.11.0 says about
// the two guards being INDEPENDENT: a database that somehow carries one column
// and not the other must be brought to the full shape, which a single shared
// guard would not do.
func TestMigrateV1_10_0_toV1_11_0_IsIdempotent(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	dropBoth := func() {
		t.Helper()
		for _, column := range []string{"commit_open", "commit_close"} {
			if _, err := database.Exec("ALTER TABLE tasks DROP COLUMN " + column); err != nil {
				t.Fatalf("removing %s to build the fixture: %v", column, err)
			}
		}
	}

	// CreateSchema produced the 1.11.0 shape, so take both columns away to obtain
	// a database the migration still has work to do on.
	dropBoth()

	for pass := 1; pass <= 2; pass++ {
		applyCommitMigration(t, database, pass)
		for _, column := range []string{"commit_open", "commit_close"} {
			if got := columnCount(t, database, "tasks", column); got != 1 {
				t.Fatalf("after pass %d the tasks table has %d %s column(s), want 1", pass, got, column)
			}
		}
	}

	// The guards are what make pass two a no-op. Prove the statements they skip
	// would in fact have failed.
	_, err := database.Exec(`ALTER TABLE tasks ADD COLUMN commit_open TEXT`)
	if err == nil {
		t.Fatal("an unguarded second ADD COLUMN succeeded, so this test cannot show that the " +
			"column-existence guard is doing any work; the SQLite behaviour it defends against " +
			"has changed and the guard's justification must be re-derived")
	}
	if !strings.Contains(err.Error(), "duplicate column name") {
		t.Errorf("an unguarded second ADD COLUMN failed with %q; the expected failure is "+
			"\"duplicate column name\", which is what SPEC/DATABASE.md § Migration Idempotency "+
			"(ALTER TABLE ADD COLUMN) requires the guard to prevent", err)
	}

	// The two guards are independent: half a migration is completed, not skipped.
	dropBoth()
	if _, err := database.Exec(
		`ALTER TABLE tasks ADD COLUMN commit_open TEXT CHECK(commit_open IS NULL OR (length(commit_open) BETWEEN 7 AND 64 AND commit_open NOT GLOB '*[^0-9a-f]*'))`,
	); err != nil {
		t.Fatalf("re-adding commit_open alone to build the half-migrated fixture: %v", err)
	}
	applyCommitMigration(t, database, 1)
	for _, column := range []string{"commit_open", "commit_close"} {
		if got := columnCount(t, database, "tasks", column); got != 1 {
			t.Errorf("a database carrying commit_open but not commit_close came out of the "+
				"migration with %d %s column(s), want 1: the two guards must be independent",
				got, column)
		}
	}
}

// applyCommitMigration runs migrateV1_10_0_toV1_11_0 in its own transaction and
// fails the test if it errors. pass names the attempt in the failure message.
func applyCommitMigration(t *testing.T, database *DB, pass int) {
	t.Helper()
	tx, err := database.Begin()
	if err != nil {
		t.Fatalf("begin (pass %d): %v", pass, err)
	}
	if err := migrateV1_10_0_toV1_11_0(tx); err != nil {
		tx.Rollback() //nolint:errcheck // the migration error is the one reported
		t.Fatalf("migration pass %d returned an error, so it is not idempotent: %v", pass, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit (pass %d): %v", pass, err)
	}
}

// TestCommitHashesRoundTripThroughEveryTaskReadPath is the regression gate for
// the read side. models.Task gained two fields and every SELECT that
// materialises one had to gain two columns; a missed projection does not fail
// loudly, it silently hands back a task whose commit fields are nil.
//
// So this exercises EVERY exported read path in this package that returns a
// models.Task, against tasks whose commit columns are set, and fails naming the
// path that dropped them.
func TestCommitHashesRoundTripThroughEveryTaskReadPath(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := testContext()

	// A sprint, opened, so the sprint-scoped and "next" read paths have
	// something to return.
	sprintID, err := database.CreateSprint(ctx, &models.Sprint{
		Title:       "Acquirer failover hardening",
		Description: "Keep settlements clearing while an acquirer is unavailable.",
		Status:      models.SprintPending,
		CreatedAt:   utils.NowISO8601(),
	})
	if err != nil {
		t.Fatalf("creating the sprint: %v", err)
	}
	if err := database.UpdateSprintStatus(ctx, sprintID, models.SprintOpen); err != nil {
		t.Fatalf("opening the sprint: %v", err)
	}

	newTask := func(title string, parent *int) int {
		t.Helper()
		id, err := database.CreateTask(ctx, &models.Task{
			Title:                  title,
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			FunctionalRequirements: "Settlements must keep clearing during an acquirer outage.",
			TechnicalRequirements:  "Queue the dispatch and replay once the acquirer answers.",
			AcceptanceCriteria:     "A simulated outage loses no settlement.",
			CreatedAt:              utils.NowISO8601(),
			Priority:               5,
			ParentTaskID:           parent,
		})
		if err != nil {
			t.Fatalf("creating task %q: %v", title, err)
		}
		return id
	}

	blockerID := newTask("Add the acquirer health probe", nil)
	blockedID := newTask("Fail the dispatcher over on a failed probe", nil)
	childID := newTask("Emit a metric for every probe result", &blockerID)

	if err := database.AddTasksToSprint(ctx, sprintID, []int{blockerID, blockedID, childID}); err != nil {
		t.Fatalf("adding the tasks to the sprint: %v", err)
	}
	if err := database.AddTaskDependencyWithAudit(ctx, blockedID, blockerID); err != nil {
		t.Fatalf("declaring the dependency: %v", err)
	}

	// Write both hashes on all three tasks, exactly as the transitions in task
	// #254 will: a value already normalised to lowercase.
	for _, id := range []int{blockerID, blockedID, childID} {
		if err := setCommitColumn(database, "commit_open", id, dbSHA1Hash); err != nil {
			t.Fatalf("setting commit_open on task %d: %v", id, err)
		}
		if err := setCommitColumn(database, "commit_close", id, dbSHA1HashOther); err != nil {
			t.Fatalf("setting commit_close on task %d: %v", id, err)
		}
	}

	assertCarriesHashes := func(path string, tasks []models.Task) {
		t.Helper()
		if len(tasks) == 0 {
			t.Fatalf("%s returned no tasks, so it proves nothing; fix the fixture", path)
		}
		for i := range tasks {
			task := &tasks[i]
			if task.CommitOpen == nil {
				t.Errorf("%s returned task %d with a nil CommitOpen; its SELECT is missing "+
					"t.commit_open, so the field reads back empty", path, task.ID)
			} else if *task.CommitOpen != dbSHA1Hash {
				t.Errorf("%s returned task %d with CommitOpen = %q, want %q",
					path, task.ID, *task.CommitOpen, dbSHA1Hash)
			}
			if task.CommitClose == nil {
				t.Errorf("%s returned task %d with a nil CommitClose; its SELECT is missing "+
					"t.commit_close, so the field reads back empty", path, task.ID)
			} else if *task.CommitClose != dbSHA1HashOther {
				t.Errorf("%s returned task %d with CommitClose = %q, want %q",
					path, task.ID, *task.CommitClose, dbSHA1HashOther)
			}
		}
	}

	one, err := database.GetTask(ctx, blockerID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	assertCarriesHashes("GetTask", []models.Task{*one})

	// GetTasks goes through the cached OpGetTasks template rather than an inline
	// projection, so it is a second copy of the SELECT that can drift on its own.
	batch, err := database.GetTasks(ctx, []int{blockerID, blockedID, childID})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	assertCarriesHashes("GetTasks (cached OpGetTasks template)", batch)

	listed, err := database.ListTasks(ctx, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	assertCarriesHashes("ListTasks", listed)

	all, err := database.ListAllTasks(ctx)
	if err != nil {
		t.Fatalf("ListAllTasks: %v", err)
	}
	assertCarriesHashes("ListAllTasks", all)

	subtasks, err := database.GetSubTasks(ctx, blockerID)
	if err != nil {
		t.Fatalf("GetSubTasks: %v", err)
	}
	assertCarriesHashes("GetSubTasks", subtasks)

	next, err := database.GetNextTasks(ctx, 10)
	if err != nil {
		t.Fatalf("GetNextTasks: %v", err)
	}
	assertCarriesHashes("GetNextTasks", next)

	blockers, err := database.GetBlockers(ctx, blockedID)
	if err != nil {
		t.Fatalf("GetBlockers: %v", err)
	}
	assertCarriesHashes("GetBlockers", blockers)

	blocking, err := database.GetBlocking(ctx, blockerID)
	if err != nil {
		t.Fatalf("GetBlocking: %v", err)
	}
	assertCarriesHashes("GetBlocking", blocking)

	active, err := database.GetActiveSprintTasks(ctx, sprintID)
	if err != nil {
		t.Fatalf("GetActiveSprintTasks: %v", err)
	}
	assertCarriesHashes("GetActiveSprintTasks", active)

	full, err := database.GetSprintTasksFull(ctx, sprintID, nil, false)
	if err != nil {
		t.Fatalf("GetSprintTasksFull: %v", err)
	}
	assertCarriesHashes("GetSprintTasksFull", full)

	open, err := database.GetOpenSprintTasks(ctx, sprintID, true)
	if err != nil {
		t.Fatalf("GetOpenSprintTasks: %v", err)
	}
	assertCarriesHashes("GetOpenSprintTasks", open)
}

// TestCommitHashesReadBackIndependentlyPerRow guards the aliasing hazard the
// existing nullable fields already carry a comment about in scanTasksWithDeps:
// the sql.NullString scan targets live OUTSIDE the row loop, so taking the
// address of one directly would make every task in a multi-row result share one
// backing string and serialise the LAST row's value.
//
// A per-row copy is what prevents it, and only a multi-row read with DIFFERENT
// values per row can tell the two apart.
func TestCommitHashesReadBackIndependentlyPerRow(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := testContext()

	type seeded struct {
		open  string
		close string
		id    int
	}

	rows := []seeded{
		{open: dbSevenCharHash, close: dbSHA1Hash},
		{open: dbSHA1HashOther, close: dbSHA256Hash},
		{open: dbSHA256Hash, close: dbSevenCharHash},
	}

	for i := range rows {
		id, err := database.CreateTask(ctx, &models.Task{
			Title:                  "Reconcile settlement window " + string(rune('A'+i)),
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			FunctionalRequirements: "Each window must reconcile to the cent.",
			TechnicalRequirements:  "Compare both ledgers per window.",
			AcceptanceCriteria:     "An unbalanced window raises an alert.",
			CreatedAt:              utils.NowISO8601(),
		})
		if err != nil {
			t.Fatalf("creating fixture task %d: %v", i, err)
		}
		rows[i].id = id
		if err := setCommitColumn(database, "commit_open", id, rows[i].open); err != nil {
			t.Fatalf("setting commit_open on task %d: %v", id, err)
		}
		if err := setCommitColumn(database, "commit_close", id, rows[i].close); err != nil {
			t.Fatalf("setting commit_close on task %d: %v", id, err)
		}
	}

	// One task deliberately left with NULL in both, so a scan that reused the
	// previous row's value would be caught here too.
	nullID, err := database.CreateTask(ctx, &models.Task{
		Title:                  "Archive the reconciled windows",
		Type:                   models.TypeTask,
		Status:                 models.StatusBacklog,
		FunctionalRequirements: "Reconciled windows must be archived within a day.",
		TechnicalRequirements:  "Move the rows to the archive table nightly.",
		AcceptanceCriteria:     "No reconciled window older than a day remains live.",
		CreatedAt:              utils.NowISO8601(),
	})
	if err != nil {
		t.Fatalf("creating the NULL-hash fixture task: %v", err)
	}

	ids := make([]int, 0, len(rows)+1)
	for _, r := range rows {
		ids = append(ids, r.id)
	}
	ids = append(ids, nullID)

	got, err := database.GetTasks(ctx, ids)
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	if len(got) != len(ids) {
		t.Fatalf("GetTasks returned %d tasks, want %d", len(got), len(ids))
	}

	byID := make(map[int]*models.Task, len(got))
	for i := range got {
		byID[got[i].ID] = &got[i]
	}

	for _, want := range rows {
		task := byID[want.id]
		if task == nil {
			t.Fatalf("task %d missing from the result", want.id)
		}
		if task.CommitOpen == nil || *task.CommitOpen != want.open {
			t.Errorf("task %d read back CommitOpen = %v, want %q: the scan is sharing one "+
				"backing value across rows", want.id, derefOrNil(task.CommitOpen), want.open)
		}
		if task.CommitClose == nil || *task.CommitClose != want.close {
			t.Errorf("task %d read back CommitClose = %v, want %q: the scan is sharing one "+
				"backing value across rows", want.id, derefOrNil(task.CommitClose), want.close)
		}
	}

	if nullTask := byID[nullID]; nullTask == nil {
		t.Fatalf("task %d missing from the result", nullID)
	} else if nullTask.CommitOpen != nil || nullTask.CommitClose != nil {
		t.Errorf("task %d has no commit hashes but read back CommitOpen = %v, CommitClose = %v; "+
			"a NULL column must not inherit the previous row's value",
			nullID, derefOrNil(nullTask.CommitOpen), derefOrNil(nullTask.CommitClose))
	}
}

// derefOrNil renders a *string for a failure message without panicking on nil.
func derefOrNil(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
