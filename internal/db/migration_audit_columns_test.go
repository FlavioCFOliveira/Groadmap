package db

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== MIGRATION 1.11.0 -> 1.12.0 ====================
//
// The regression gates for rmp task #261: the audit table gains
// related_entity_id and commit_hash, each carrying its CHECK, and the legacy
// TASK_STATUS_CHANGE entries whose destination the stored data determines by
// EXACT EQUALITY are reclassified (SPEC/VERSION.md § Migration 1.11.0 to 1.12.0,
// SPEC/DATABASE.md § `audit` Table and § Commit Hash Format Constraint).
//
// Three properties carry the weight here, and each is tested by an assertion
// that can fail:
//
//  1. The migration ADDS and RECLASSIFIES, and does nothing else. No row is
//     deleted, renumbered, or otherwise rewritten, and neither new column is
//     backfilled, because no truthful backfill exists for either.
//  2. Reclassification never infers. Only exact timestamp equality moves an
//     entry, so the fixture below carries one row for each way the evidence can
//     fall short — no match, two matches, and a task that no longer exists — and
//     each of them must still read TASK_STATUS_CHANGE afterwards.
//  3. A migrated database and a fresh one enforce the same rules. The audit DDL
//     exists twice (CreateSchema and this migration, which is a historical record
//     of one version's shape), so TestMigratedAndFreshAuditTablesAreEquivalent is
//     what stops the two copies drifting apart.
//
// The commit-hash half of the accept/reject matrix is NOT restated here: it is
// commitHashMatrix in migration_commit_hashes_test.go, and audit.commit_hash
// takes the same constraint as tasks.commit_open and tasks.commit_close by
// specification ("Any future column that stores a commit hash MUST take this same
// constraint rather than a variant of it"). Reusing the one matrix is what proves
// the three columns really do agree.

// auditDDL1110 is the audit table exactly as schema 1.11.0 declared it: the two
// new columns are absent, and the entity_type CHECK and all four indexes are what
// shipped.
//
// The DDL is transcribed rather than derived, for the same reason
// migrateV1_8_0_toV1_9_0 carries its own copy: a fixture for a historical schema
// must not follow later changes to the fresh-schema definition, or the migration
// it exercises stops being the migration that ships.
const auditDDL1110 = `
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

-- Composite index for audit date range queries (TASK-P001)
CREATE INDEX IF NOT EXISTS idx_audit_date ON audit(performed_at DESC);
`

// auditEntityTypeDDL1120 is the audit table's entity_type declaration,
// indentation included. The migration must leave it untouched: it adds columns
// and alters none, so the line SQLite reports for a migrated table has to be the
// same line a fresh table reports.
//
// It is the same string internal/models/spec_enum_coverage_test.go reconciles
// between SPEC/DATABASE.md and internal/db/schema.go. That gate proves the
// specification and the fresh DDL agree; this one proves the migration does not
// pull the migrated DDL away from either.
const auditEntityTypeDDL1120 = "    entity_type TEXT NOT NULL CHECK(entity_type IN ('TASK', 'SPRINT')),"

// The operations the reclassification produces. They are string literals rather
// than models constants because the constants do not exist yet: rmp task #262
// adds them. The migration writes these values into stored rows, so the literals
// here are what the storage layer must actually hold.
const (
	opStatusChangeLegacy = "TASK_STATUS_CHANGE"
	opStatusDoing        = "TASK_STATUS_DOING"
	opStatusTesting      = "TASK_STATUS_TESTING"
	opStatusCompleted    = "TASK_STATUS_COMPLETED"
)

// Lifecycle timestamps of the fixture, in the format the application writes
// (utils.ISO8601Format, millisecond precision). Reclassification turns on exact
// string equality, so every one of them is distinct except where a test case
// deliberately ties two together.
const (
	auditTSCreated     = "2026-04-02T08:15:22.418Z" // the lifecycle task was created
	auditTSDoing       = "2026-04-03T09:41:07.902Z" // ... entered DOING
	auditTSTesting     = "2026-04-06T14:22:55.310Z" // ... entered TESTING
	auditTSCompleted   = "2026-04-07T17:03:11.774Z" // ... entered COMPLETED
	auditTSBacklog     = "2026-04-08T07:55:40.129Z" // ... was sent back to BACKLOG, which stamps nothing
	auditTSAssign      = "2026-04-02T08:16:03.550Z" // ... was assigned, on a version that still had TASK_ASSIGN
	auditTSReopened    = "2026-04-09T11:07:19.006Z" // a transition on the task later reopened
	auditTSSameInstant = "2026-04-10T16:30:00.000Z" // started_at and closed_at of one task, to the millisecond
	auditTSTiedStart   = "2026-04-11T08:02:44.881Z" // the tied task entered DOING
	auditTSTied        = "2026-04-12T19:45:02.663Z" // ... and its tested_at equals its closed_at
	auditTSDoingOnly   = "2026-04-13T10:18:37.245Z" // a task still in DOING: tested_at and closed_at are NULL
	auditTSTestStart   = "2026-04-14T09:00:12.501Z" // a task in TESTING entered DOING
	auditTSTestReady   = "2026-04-15T13:26:58.037Z" // ... and then TESTING, with closed_at still NULL
	auditTSDeleted     = "2026-04-16T15:11:26.912Z" // a transition of a task since removed
	auditTSSprintMove  = "2026-04-17T12:40:05.483Z" // a sprint move, on a version that still had SPRINT_MOVE_TASK
)

// auditSeed is one row of the 1.11.0 audit fixture together with the operation
// the migration must leave it carrying. why records the rule that decides it, so
// a failure names the clause that broke rather than only the value.
type auditSeed struct {
	operation   string
	entityType  string
	performedAt string
	want        string
	why         string
	id          int
	entityID    int
}

// auditFixture is what buildRoadmapAtSchema1110 seeded: the rows, and the
// aggregate facts SPEC/VERSION.md § Migration 1.11.0 to 1.12.0 requires the
// migration to preserve.
type auditFixture struct {
	seeds []auditSeed
	rows  int
	minID int
	maxID int
}

// buildRoadmapAtSchema1110 creates a real on-disk roadmap under the test HOME and
// takes it back to schema 1.11.0.
//
// The roadmap is first created through the production path, which yields the
// current schema and every table 1.11.0 had; the tasks and sprints are written
// through the shipped write path; and the audit table is then replaced by its
// verbatim 1.11.0 declaration (auditDDL1110) and seeded by hand. Only audit
// changed between 1.11.0 and 1.12.0, so rebuilding that one table is enough to
// produce a faithful 1.11.0 database while the other seven tables stay correct by
// construction. The rebuild also discards the audit rows the production writes
// above produced, so the fixture holds exactly the rows seeded below and every
// assertion over the table is exhaustive.
//
// The seeded rows are the reclassification matrix: one row for each destination
// the evidence determines, and one for each way the evidence can fall short.
func buildRoadmapAtSchema1110(t *testing.T, roadmapName string) auditFixture {
	t.Helper()

	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("creating roadmap %q: %v", roadmapName, err)
	}

	ctx := testContext()

	sprintID, err := database.CreateSprint(ctx, &models.Sprint{
		Title:       "Settlement resilience",
		Description: "Keep card settlements clearing while an acquirer is unavailable.",
		Status:      models.SprintPending,
		CreatedAt:   auditTSCreated,
	})
	if err != nil {
		t.Fatalf("creating the fixture sprint: %v", err)
	}

	newTask := func(title, functional string) int {
		t.Helper()
		id, err := database.CreateTask(ctx, &models.Task{
			Title:                  title,
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			FunctionalRequirements: functional,
			TechnicalRequirements:  "Queue the dispatch and replay it once the acquirer answers again.",
			AcceptanceCriteria:     "A simulated acquirer outage loses no settlement.",
			CreatedAt:              auditTSCreated,
			Priority:               5,
		})
		if err != nil {
			t.Fatalf("creating fixture task %q: %v", title, err)
		}
		return id
	}

	// A task that ran the full lifecycle, each transition at its own instant:
	// the three unambiguous reclassifications come from this one.
	lifecycleID := newTask(
		"Harden the settlement retry ladder",
		"A failed settlement must be retried on a bounded backoff ladder.")
	setLifecycleTimestamps(t, database, lifecycleID,
		models.StatusCompleted, auditTSDoing, auditTSTesting, auditTSCompleted)

	// A task that was reopened, so the transition's timestamp was cleared: the
	// entry now matches nothing at all.
	reopenedID := newTask(
		"Re-key the acquirer webhook secrets",
		"Webhook secrets must be rotated without dropping a callback.")
	setLifecycleTimestamps(t, database, reopenedID, models.StatusBacklog, "", "", "")

	// A task whose started_at and closed_at are the same instant: an entry at
	// that instant matches two of the three timestamps and stays ambiguous.
	sameInstantID := newTask(
		"Publish the settlement outage postmortem",
		"Every acquirer outage must be written up within five working days.")
	setLifecycleTimestamps(t, database, sameInstantID,
		models.StatusCompleted, auditTSSameInstant, "", auditTSSameInstant)

	// A task whose tested_at and closed_at are the same instant: its DOING entry
	// is still unambiguous, its later entry is not.
	tiedID := newTask(
		"Rotate the settlement signing certificate",
		"The signing certificate must be replaced before it expires.")
	setLifecycleTimestamps(t, database, tiedID,
		models.StatusCompleted, auditTSTiedStart, auditTSTied, auditTSTied)

	// A task still in DOING: tested_at and closed_at are NULL, which is the
	// IS NULL leg of the two exclusion clauses.
	doingID := newTask(
		"Migrate the ledger to the new chart of accounts",
		"Ledger entries must map onto the new account tree without loss.")
	setLifecycleTimestamps(t, database, doingID, models.StatusDoing, auditTSDoingOnly, "", "")

	// A task in TESTING: closed_at is NULL, so the TESTING statement's own
	// exclusion clause is exercised against a NULL too.
	testingID := newTask(
		"Reconcile the acquirer settlement file nightly",
		"The nightly settlement file must reconcile to the cent.")
	setLifecycleTimestamps(t, database, testingID,
		models.StatusTesting, auditTSTestStart, auditTSTestReady, "")

	// A task that has since been removed. Its audit entry outlives it, and the
	// timestamps that would have decided the entry went with it.
	deletedID := newTask(
		"Draft the acquirer failover runbook",
		"On-call must have a written failover procedure.")
	setLifecycleTimestamps(t, database, deletedID, models.StatusCompleted,
		auditTSDeleted, auditTSDeleted, auditTSDeleted)

	// Replace the audit table with its 1.11.0 shape, discarding the rows the
	// writes above produced so the fixture is exactly the matrix below.
	if _, err := database.Exec("DROP TABLE audit"); err != nil {
		t.Fatalf("dropping the current audit table: %v", err)
	}
	if _, err := database.Exec(auditDDL1110); err != nil {
		t.Fatalf("creating the 1.11.0 audit table: %v", err)
	}

	seeds := []auditSeed{
		{
			operation: string(models.OpTaskCreate), entityType: string(models.EntityTask),
			entityID: lifecycleID, performedAt: auditTSCreated,
			want: string(models.OpTaskCreate),
			why:  "only TASK_STATUS_CHANGE entries are reclassified at all",
		},
		{
			operation: opStatusChangeLegacy, entityType: string(models.EntityTask),
			entityID: lifecycleID, performedAt: auditTSDoing,
			want: opStatusDoing,
			why:  "performed_at equals started_at and neither tested_at nor closed_at",
		},
		{
			operation: opStatusChangeLegacy, entityType: string(models.EntityTask),
			entityID: lifecycleID, performedAt: auditTSTesting,
			want: opStatusTesting,
			why:  "performed_at equals tested_at and neither started_at nor closed_at",
		},
		{
			operation: opStatusChangeLegacy, entityType: string(models.EntityTask),
			entityID: lifecycleID, performedAt: auditTSCompleted,
			want: opStatusCompleted,
			why:  "performed_at equals closed_at and neither started_at nor tested_at",
		},
		{
			operation: opStatusChangeLegacy, entityType: string(models.EntityTask),
			entityID: lifecycleID, performedAt: auditTSBacklog,
			want: opStatusChangeLegacy,
			why: "a transition to BACKLOG stamps no timestamp, so performed_at matches none of " +
				"the three even though the task carries all three",
		},
		{
			// The same instant as the DOING transition above. Without the
			// `operation = 'TASK_STATUS_CHANGE'` predicate this row would be
			// rewritten to TASK_STATUS_DOING, which is exactly what
			// SPEC/VERSION.md forbids: a field edit leaves no trace of which
			// field it touched.
			operation: string(models.OpTaskUpdate), entityType: string(models.EntityTask),
			entityID: lifecycleID, performedAt: auditTSDoing,
			want: string(models.OpTaskUpdate),
			why:  "TASK_UPDATE is never reclassified, whatever its performed_at happens to equal",
		},
		{
			// entity_id collides with a task id on purpose: without the
			// `entity_type = 'TASK'` predicate this sprint-scoped row would be
			// rewritten from a task's timestamps.
			operation: opStatusChangeLegacy, entityType: string(models.EntitySprint),
			entityID: lifecycleID, performedAt: auditTSDoing,
			want: opStatusChangeLegacy,
			why: "the entry belongs to a sprint's history; a task's lifecycle timestamps say " +
				"nothing about it, even when the ids collide",
		},
		{
			operation: opStatusChangeLegacy, entityType: string(models.EntityTask),
			entityID: reopenedID, performedAt: auditTSReopened,
			want: opStatusChangeLegacy,
			why:  "the task was reopened, which cleared the timestamps that would have matched",
		},
		{
			operation: opStatusChangeLegacy, entityType: string(models.EntityTask),
			entityID: sameInstantID, performedAt: auditTSSameInstant,
			want: opStatusChangeLegacy,
			why: "performed_at equals both started_at and closed_at, and two transitions recorded " +
				"at the same instant leave no evidence of which one the entry recorded",
		},
		{
			operation: opStatusChangeLegacy, entityType: string(models.EntityTask),
			entityID: tiedID, performedAt: auditTSTiedStart,
			want: opStatusDoing,
			why:  "a tie between the other two timestamps does not make this entry ambiguous",
		},
		{
			operation: opStatusChangeLegacy, entityType: string(models.EntityTask),
			entityID: tiedID, performedAt: auditTSTied,
			want: opStatusChangeLegacy,
			why:  "performed_at equals both tested_at and closed_at",
		},
		{
			operation: opStatusChangeLegacy, entityType: string(models.EntityTask),
			entityID: doingID, performedAt: auditTSDoingOnly,
			want: opStatusDoing,
			why: "tested_at and closed_at are NULL, and the exclusion clauses admit a NULL " +
				"rather than treating it as a match",
		},
		{
			operation: opStatusChangeLegacy, entityType: string(models.EntityTask),
			entityID: testingID, performedAt: auditTSTestStart,
			want: opStatusDoing,
			why:  "performed_at equals started_at; closed_at is NULL and tested_at differs",
		},
		{
			operation: opStatusChangeLegacy, entityType: string(models.EntityTask),
			entityID: testingID, performedAt: auditTSTestReady,
			want: opStatusTesting,
			why:  "performed_at equals tested_at; closed_at is NULL and started_at differs",
		},
		{
			operation: opStatusChangeLegacy, entityType: string(models.EntityTask),
			entityID: deletedID, performedAt: auditTSDeleted,
			want: opStatusChangeLegacy,
			why:  "the task named by entity_id no longer exists and took its timestamps with it",
		},
		{
			operation: string(models.OpSprintUpdate), entityType: string(models.EntitySprint),
			entityID: sprintID, performedAt: auditTSDoing,
			want: string(models.OpSprintUpdate),
			why:  "SPRINT_UPDATE is never reclassified, for the same reason as TASK_UPDATE",
		},
		{
			operation: string(models.OpSprintMoveTask), entityType: string(models.EntitySprint),
			entityID: sprintID, performedAt: auditTSSprintMove,
			want: string(models.OpSprintMoveTask),
			why: "a SPRINT_MOVE_TASK entry names one sprint and no task, so neither the pair of " +
				"sprints nor the task that moved is recoverable",
		},
		{
			// An operation the catalogue does not list at all. The migration
			// deletes no audit row, so a roadmap's recorded history stays
			// complete (SPEC/DATABASE.md § `audit` Table).
			operation: "TASK_ASSIGN", entityType: string(models.EntityTask),
			entityID: lifecycleID, performedAt: auditTSAssign,
			want: "TASK_ASSIGN",
			why:  "no migration deletes or rewrites an uncatalogued row",
		},
	}

	for i := range seeds {
		seeds[i].id = insertAuditFixtureRow(t, database, &seeds[i])
	}

	// The task that no longer exists is removed last, so its audit entry is
	// already stored when it goes: a deleted task leaves its history behind.
	if _, err := database.Exec("DELETE FROM tasks WHERE id = ?", deletedID); err != nil {
		t.Fatalf("removing the deleted-task fixture %d: %v", deletedID, err)
	}

	if _, err := database.Exec(
		"UPDATE _metadata SET value = '1.11.0' WHERE key = 'schema_version'"); err != nil {
		t.Fatalf("setting schema_version to 1.11.0: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("closing the 1.11.0 fixture: %v", err)
	}

	fx := auditFixture{seeds: seeds, rows: len(seeds), minID: seeds[0].id, maxID: seeds[0].id}
	for i := range seeds {
		if seeds[i].id < fx.minID {
			fx.minID = seeds[i].id
		}
		if seeds[i].id > fx.maxID {
			fx.maxID = seeds[i].id
		}
	}
	return fx
}

// setLifecycleTimestamps writes a task's status and its three lifecycle
// timestamps, treating "" as SQL NULL. The statement is parameterized; nothing
// from the caller reaches the SQL text.
//
// The timestamps are written directly rather than through the status-transition
// path because the fixture needs exact, chosen instants: reclassification turns
// on string equality with these very values, so a clock-derived timestamp would
// make the matrix untestable.
func setLifecycleTimestamps(t *testing.T, database *DB, taskID int, status models.TaskStatus, started, tested, closed string) {
	t.Helper()

	nullable := func(v string) sql.NullString {
		return sql.NullString{String: v, Valid: v != ""}
	}

	if _, err := database.Exec(
		`UPDATE tasks SET status = ?, started_at = ?, tested_at = ?, closed_at = ? WHERE id = ?`,
		string(status), nullable(started), nullable(tested), nullable(closed), taskID,
	); err != nil {
		t.Fatalf("setting the lifecycle timestamps of task %d: %v", taskID, err)
	}
}

// insertAuditFixtureRow writes one row into the 1.11.0 audit table and returns
// its id.
//
// The INSERT is the verbatim statement LogAuditTx executed at 1.11.0 — the same
// four columns in the same order — so the seeded rows are the rows that binary
// wrote. It cannot go through LogAuditTx itself: the fixture must write legacy
// and uncatalogued operation values, and it needs the id back.
func insertAuditFixtureRow(t *testing.T, database *DB, seed *auditSeed) int {
	t.Helper()

	res, err := database.Exec(
		`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
		seed.operation, seed.entityType, seed.entityID, seed.performedAt,
	)
	if err != nil {
		t.Fatalf("seeding audit row (%s %s %d): %v", seed.operation, seed.entityType, seed.entityID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("reading the id of the seeded audit row (%s): %v", seed.operation, err)
	}
	return int(id)
}

// auditRow is one stored audit row, every column of it.
type auditRow struct {
	operation       string
	entityType      string
	performedAt     string
	commitHash      sql.NullString
	relatedEntityID sql.NullInt64
	id              int
	entityID        int
}

// snapshotAuditTable reads the whole audit table, ordered by id. It names every
// column explicitly, which is what makes the differing physical column order of a
// migrated and a fresh table immaterial.
func snapshotAuditTable(t *testing.T, database *DB) []auditRow {
	t.Helper()

	rows, err := database.Query(`SELECT id, operation, entity_type, entity_id,
	                                    related_entity_id, commit_hash, performed_at
	                             FROM audit ORDER BY id`)
	if err != nil {
		t.Fatalf("reading the audit table: %v", err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup

	snapshot := make([]auditRow, 0, 32)
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.id, &r.operation, &r.entityType, &r.entityID,
			&r.relatedEntityID, &r.commitHash, &r.performedAt); err != nil {
			t.Fatalf("scanning an audit row: %v", err)
		}
		snapshot = append(snapshot, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the audit table: %v", err)
	}
	return snapshot
}

// assertReclassification holds every seeded row to the operation the
// specification says it must carry, and to the rule that nothing else about it
// changed. when names the moment being asserted, so a failure says whether the
// first or the second run of the migration broke it.
func assertReclassification(t *testing.T, database *DB, fx *auditFixture, when string) {
	t.Helper()

	stored := snapshotAuditTable(t, database)
	byID := make(map[int]*auditRow, len(stored))
	for i := range stored {
		byID[stored[i].id] = &stored[i]
	}

	for i := range fx.seeds {
		seed := &fx.seeds[i]
		row := byID[seed.id]
		if row == nil {
			t.Errorf("[%s] audit row %d (seeded as %s) is gone; the migration deletes no entry",
				when, seed.id, seed.operation)
			continue
		}
		if row.operation != seed.want {
			t.Errorf("[%s] audit row %d was seeded as %s and reads %s after the migration, want %s: %s",
				when, seed.id, seed.operation, row.operation, seed.want, seed.why)
		}
		// Only the operation may change. Everything else the entry recorded is
		// the historical fact the migration must not disturb.
		if row.entityType != seed.entityType {
			t.Errorf("[%s] audit row %d entity_type = %q, want %q: the migration rewrites operation "+
				"and nothing else", when, seed.id, row.entityType, seed.entityType)
		}
		if row.entityID != seed.entityID {
			t.Errorf("[%s] audit row %d entity_id = %d, want %d: the migration rewrites operation "+
				"and nothing else", when, seed.id, row.entityID, seed.entityID)
		}
		if row.performedAt != seed.performedAt {
			t.Errorf("[%s] audit row %d performed_at = %q, want %q: the migration rewrites operation "+
				"and nothing else", when, seed.id, row.performedAt, seed.performedAt)
		}
		if row.relatedEntityID.Valid || row.commitHash.Valid {
			t.Errorf("[%s] audit row %d migrated with related_entity_id=%v commit_hash=%v; there is no "+
				"truthful backfill for either column, so every pre-existing row must keep NULL in both",
				when, seed.id, row.relatedEntityID, row.commitHash)
		}
	}

	if len(stored) != fx.rows {
		t.Errorf("[%s] the audit table holds %d rows, want %d: the migration deletes and inserts nothing "+
			"(it writes no audit entry of its own either)", when, len(stored), fx.rows)
	}
}

// TestMigrateV1_11_0_toV1_12_0_OnNextOpen is the primary gate: a database created
// at 1.11.0 must reach 1.12.0 on the next open, with NO user action, gaining both
// audit columns, reclassifying exactly the entries the stored data determines,
// and leaving every other entry and every other column exactly as it was.
func TestMigrateV1_11_0_toV1_12_0_OnNextOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmapName = "settlement-platform"
	fx := buildRoadmapAtSchema1110(t, roadmapName)

	// The production entry point every rmp command goes through.
	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("opening the 1.11.0 database: %v", err)
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

	for _, column := range []string{"related_entity_id", "commit_hash"} {
		if got := columnCount(t, database, "audit", column); got != 1 {
			t.Errorf("pragma_table_info('audit') reports %d %s column(s) after the migration, want 1",
				got, column)
		}
	}

	// THE RECLASSIFICATION MATRIX, and with it the no-backfill rule.
	assertReclassification(t, database, &fx, "first run")

	// Neither the row count nor the id range moves: no DELETE, no id rewrite, no
	// compaction (SPEC/VERSION.md § Migration 1.11.0 to 1.12.0, acceptance
	// criteria 1 and 3).
	assertRowCount(t, database, fx.rows, "every audit row survives the migration",
		"SELECT COUNT(*) FROM audit")
	var minID, maxID int
	if err := database.QueryRow("SELECT MIN(id), MAX(id) FROM audit").Scan(&minID, &maxID); err != nil {
		t.Fatalf("reading the audit id range: %v", err)
	}
	if minID != fx.minID || maxID != fx.maxID {
		t.Errorf("the audit id range is %d..%d after the migration, want %d..%d: the migration "+
			"renumbers nothing", minID, maxID, fx.minID, fx.maxID)
	}
	assertRowCount(t, database, 0, "neither new column is backfilled on any row",
		"SELECT COUNT(*) FROM audit WHERE commit_hash IS NOT NULL OR related_entity_id IS NOT NULL")

	// The reclassification never produces TASK_STATUS_SPRINT or the sprint form
	// of TASK_STATUS_BACKLOG, which is what lets SPEC/DATABASE.md state that
	// every stored TASK_STATUS_SPRINT row names a sprint on ANY database,
	// migrated or fresh.
	assertRowCount(t, database, 0, "the migration produces no TASK_STATUS_SPRINT row",
		"SELECT COUNT(*) FROM audit WHERE operation = ?", "TASK_STATUS_SPRINT")
	assertRowCount(t, database, 0, "the migration produces no TASK_STATUS_BACKLOG row",
		"SELECT COUNT(*) FROM audit WHERE operation = ?", "TASK_STATUS_BACKLOG")

	// THE TABLE WAS NOT REBUILT. ADD COLUMN can only append, so the two new
	// columns must sit AFTER performed_at; a replacement table would have
	// produced the fresh layout instead. The AUTOINCREMENT counter is the second
	// witness: a rebuild that copied rows would have had to restore it by hand.
	// equalStrings is the package's existing slice comparison (permissions_test.go).
	if got, want := auditColumnOrder(t, database), migratedAuditColumnOrder; !equalStrings(got, want) {
		t.Errorf("the migrated audit table's columns are %v, want %v: ALTER TABLE ADD COLUMN appends, "+
			"so a different order means the table was rebuilt", got, want)
	}
	var seq int
	if err := database.QueryRow(
		"SELECT seq FROM sqlite_sequence WHERE name = 'audit'").Scan(&seq); err != nil {
		t.Fatalf("reading the audit AUTOINCREMENT counter: %v", err)
	}
	if seq != fx.maxID {
		t.Errorf("the audit AUTOINCREMENT counter is %d after the migration, want %d: it must survive "+
			"the migration, or the next entry reuses an id", seq, fx.maxID)
	}

	// The entity_type CHECK is untouched: the migration adds columns and alters
	// none (SPEC/VERSION.md § Migration 1.11.0 to 1.12.0).
	assertAuditEntityTypeDeclaration(t, database, "migrated")

	// No index is created on either column: neither is a predicate of any audit
	// read (SPEC/DATABASE.md § `audit` Table).
	assertRowCount(t, database, 0, "the migration creates no index on either new column",
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'audit'
		   AND (IFNULL(sql, '') LIKE '%related_entity_id%' OR IFNULL(sql, '') LIKE '%commit_hash%')`)

	// THE CHECK CONSTRAINTS ARE LIVE ON THE MIGRATED TABLE. SQLite accepts a
	// CHECK clause on ADD COLUMN and enforces it from that moment on, which is
	// what lets the migrated schema match the fresh one. Without this the two
	// would silently diverge: the columns would exist but accept anything.
	//
	// This runs last because it writes and restores values on a real row.
	assertAuditCheckMatrices(t, database, fx.seeds[0].id, "migrated")

	// The same two constraints on the INSERT path, which is the one every writer
	// uses and the one SPEC/VERSION.md § Migration 1.11.0 to 1.12.0 names in its
	// tenth acceptance criterion. The matrices above write through UPDATE; a
	// constraint that somehow held only there would leave the write path open.
	for _, tc := range []struct {
		column string
		value  any
		why    string
	}{
		{
			column: "related_entity_id", value: int64(0),
			why: "0 is not an entity id",
		},
		{
			column: "commit_hash", value: "ABC1234",
			why: "GLOB is case-sensitive, so an unnormalised hash must not reach storage",
		},
	} {
		// tc.column is one of two test-local literals; the value is bound.
		_, err := database.Exec(
			"INSERT INTO audit (operation, entity_type, entity_id, performed_at, "+tc.column+") VALUES (?, ?, ?, ?, ?)",
			string(models.OpTaskCreate), string(models.EntityTask), 1, utils.NowISO8601(), tc.value)
		switch {
		case err == nil:
			t.Errorf("an INSERT with %s = %#v succeeded; it must fail because %s", tc.column, tc.value, tc.why)
			if _, derr := database.Exec("DELETE FROM audit WHERE id = (SELECT MAX(id) FROM audit)"); derr != nil {
				t.Fatalf("removing the row that should never have been inserted: %v", derr)
			}
		case !strings.Contains(err.Error(), "CHECK constraint failed"):
			t.Errorf("an INSERT with %s = %#v failed with %q; the rejection must come from the CHECK "+
				"constraint, not from something else", tc.column, tc.value, err)
		}
	}
}

// migratedAuditColumnOrder and freshAuditColumnOrder are the two physical column
// layouts SPEC/VERSION.md § Migration 1.11.0 to 1.12.0 predicts, and pinning both
// is what makes "no table rebuild" an assertion rather than a claim.
//
// The difference is immaterial to every reader — no statement in this package
// uses SELECT * or positional binding — and no migration reorders the columns to
// hide it, because a reorder would require the rebuild the specification refuses.
var (
	migratedAuditColumnOrder = []string{
		"id", "operation", "entity_type", "entity_id", "performed_at", "related_entity_id", "commit_hash",
	}
	freshAuditColumnOrder = []string{
		"id", "operation", "entity_type", "entity_id", "related_entity_id", "commit_hash", "performed_at",
	}
)

// auditColumnOrder returns the audit table's column names in physical order.
func auditColumnOrder(t *testing.T, database *DB) []string {
	t.Helper()

	rows, err := database.Query(`SELECT name FROM pragma_table_info('audit') ORDER BY cid`)
	if err != nil {
		t.Fatalf("reading the audit column order: %v", err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup

	names := make([]string, 0, len(freshAuditColumnOrder))
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning a column name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the audit columns: %v", err)
	}
	return names
}

// assertAuditEntityTypeDeclaration requires the stored DDL of the audit table to
// contain the entity_type declaration verbatim. SQLite rewrites the stored CREATE
// TABLE text when a column is added — it appends the new column definition before
// the closing parenthesis — so the surviving line is real evidence that the
// migration altered no existing column.
func assertAuditEntityTypeDeclaration(t *testing.T, database *DB, what string) {
	t.Helper()

	var ddl string
	if err := database.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'audit'`).Scan(&ddl); err != nil {
		t.Fatalf("[%s] reading the stored audit DDL: %v", what, err)
	}
	if !strings.Contains(ddl, auditEntityTypeDDL1120) {
		t.Errorf("[%s] the stored audit DDL does not contain\n  %q\nSPEC/VERSION.md § Migration 1.11.0 "+
			"to 1.12.0 requires entity_type to keep its CHECK unaltered, and the catalogue's argument "+
			"that the entity_type value set is closed rests on it\nstored DDL:\n%s",
			what, auditEntityTypeDDL1120, ddl)
	}

	// The behavioural half: whatever the text says, SQLite must reject a third
	// entity type.
	_, err := database.Exec(
		`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
		string(models.OpTaskCreate), "COMMENT", 1, utils.NowISO8601())
	if err == nil {
		t.Errorf("[%s] the audit table accepted entity_type = 'COMMENT'; the value set is closed by the "+
			"table definition itself (SPEC/DATABASE.md § `audit` Table)", what)
	} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("[%s] entity_type = 'COMMENT' was rejected with %q; the rejection must come from the "+
			"CHECK constraint, not from something else", what, err)
	}
}

// auditCheckCase is one row of the accept/reject matrix for related_entity_id.
type auditCheckCase struct {
	name  string
	value any // int64, or nil for SQL NULL
	why   string
	want  bool
}

// relatedEntityIDMatrix is the storage-layer contract of
// SPEC/DATABASE.md § `audit` Table for related_entity_id: NULL or a positive
// entity id, and nothing else.
var relatedEntityIDMatrix = []auditCheckCase{
	{
		name: "NULL", value: nil, want: true,
		why: "the column is nullable, and a NULL says the operation had no counterpart",
	},
	{
		name: "one, the smallest entity id", value: int64(1), want: true,
		why: "ids are positive and AUTOINCREMENT starts at 1",
	},
	{
		name: "a large entity id", value: int64(2147483647), want: true,
		why: "the CHECK bounds the value below and not above",
	},
	{
		name: "zero", value: int64(0), want: false,
		why: "0 is not an entity id, and NULL is the way to say 'no counterpart'",
	},
	{
		name: "minus one", value: int64(-1), want: false,
		why: "a negative id names no entity",
	},
}

// setAuditColumn attempts to write value into the named column of the named audit
// row and reports the error, if any. column is a test-local literal at every call
// site; the value is bound.
func setAuditColumn(database *DB, column string, auditID int, value any) error {
	_, err := database.Exec("UPDATE audit SET "+column+" = ? WHERE id = ?", value, auditID)
	return err
}

// assertAuditCheckMatrices runs both accept/reject matrices against the named
// audit row and restores NULL afterwards, so the caller's fixture is unchanged.
//
// commit_hash is held to commitHashMatrix — the very matrix tasks.commit_open and
// tasks.commit_close are held to — because SPEC/DATABASE.md § Commit Hash Format
// Constraint states the three constraints are identical apart from the column
// name. Sharing the matrix is what proves it.
func assertAuditCheckMatrices(t *testing.T, database *DB, auditID int, what string) {
	t.Helper()

	for _, tc := range commitHashMatrix {
		err := setAuditColumn(database, "commit_hash", auditID, tc.value)
		switch {
		case tc.want && err != nil:
			t.Errorf("[%s] audit.commit_hash rejected %s (%v); SPEC/DATABASE.md § Commit Hash Format "+
				"Constraint accepts it because %s", what, tc.name, err, tc.why)
		case !tc.want && err == nil:
			t.Errorf("[%s] audit.commit_hash accepted %s (%#v); SPEC/DATABASE.md § Commit Hash Format "+
				"Constraint rejects it because %s", what, tc.name, tc.value, tc.why)
		case !tc.want && err != nil && !strings.Contains(err.Error(), "CHECK constraint failed"):
			t.Errorf("[%s] audit.commit_hash rejected %s with %q; the rejection must come from the "+
				"CHECK constraint, not from something else", what, tc.name, err)
		}
	}
	if err := setAuditColumn(database, "commit_hash", auditID, nil); err != nil {
		t.Fatalf("[%s] restoring audit.commit_hash to NULL: %v", what, err)
	}

	for _, tc := range relatedEntityIDMatrix {
		err := setAuditColumn(database, "related_entity_id", auditID, tc.value)
		switch {
		case tc.want && err != nil:
			t.Errorf("[%s] audit.related_entity_id rejected %s (%v); SPEC/DATABASE.md § `audit` Table "+
				"accepts it because %s", what, tc.name, err, tc.why)
		case !tc.want && err == nil:
			t.Errorf("[%s] audit.related_entity_id accepted %s (%#v); SPEC/DATABASE.md § `audit` Table "+
				"rejects it because %s", what, tc.name, tc.value, tc.why)
		case !tc.want && err != nil && !strings.Contains(err.Error(), "CHECK constraint failed"):
			t.Errorf("[%s] audit.related_entity_id rejected %s with %q; the rejection must come from "+
				"the CHECK constraint, not from something else", what, tc.name, err)
		}
	}
	if err := setAuditColumn(database, "related_entity_id", auditID, nil); err != nil {
		t.Fatalf("[%s] restoring audit.related_entity_id to NULL: %v", what, err)
	}
}

// auditColumnDescriptor is the part of pragma_table_info that has to agree
// between a fresh audit table and a migrated one. The ordinal position is
// deliberately excluded and compared separately: ADD COLUMN can only append, so
// it CANNOT agree, and no read path depends on it.
type auditColumnDescriptor struct {
	name       string
	typ        string
	dfltValue  sql.NullString
	notNull    int
	primaryKey int
}

// auditColumnDescriptors reads every audit column's declaration, keyed by name.
func auditColumnDescriptors(t *testing.T, database *DB) map[string]auditColumnDescriptor {
	t.Helper()

	rows, err := database.Query(
		`SELECT name, type, "notnull", dflt_value, pk FROM pragma_table_info('audit')`)
	if err != nil {
		t.Fatalf("reading the audit column declarations: %v", err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup

	declarations := make(map[string]auditColumnDescriptor, len(freshAuditColumnOrder))
	for rows.Next() {
		var d auditColumnDescriptor
		if err := rows.Scan(&d.name, &d.typ, &d.notNull, &d.dfltValue, &d.primaryKey); err != nil {
			t.Fatalf("scanning a column declaration: %v", err)
		}
		declarations[d.name] = d
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the audit column declarations: %v", err)
	}
	return declarations
}

// auditIndexes returns every index on the audit table as "name: sql", ordered by
// name.
func auditIndexes(t *testing.T, database *DB) []string {
	t.Helper()

	rows, err := database.Query(
		`SELECT name, IFNULL(sql, '') FROM sqlite_master
		 WHERE type = 'index' AND tbl_name = 'audit' ORDER BY name`)
	if err != nil {
		t.Fatalf("reading the audit indexes: %v", err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup

	indexes := make([]string, 0, 4)
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			t.Fatalf("scanning an index: %v", err)
		}
		indexes = append(indexes, name+": "+ddl)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the audit indexes: %v", err)
	}
	return indexes
}

// TestMigratedAndFreshAuditTablesAreEquivalent pins the guarantee SPEC/VERSION.md
// § Migration 1.11.0 to 1.12.0 states: fresh databases receive both columns, with
// the same constraints, directly from the CREATE TABLE statement, so a migrated
// database and a fresh one end up equivalent.
//
// "Equivalent" is the exact word, and the test says what it excludes: the
// physical column order differs, permanently and by design, because ADD COLUMN
// can only append. Both layouts are pinned rather than glossed over, and
// everything else — the declaration of every column, every index, and what the
// two tables ACCEPT — must agree exactly.
//
// The DDL is intentionally duplicated between CreateSchema and
// migrateV1_11_0_toV1_12_0 (a migration is a historical record of one version's
// shape), so this test is what stops the two copies drifting apart.
func TestMigratedAndFreshAuditTablesAreEquivalent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fx := buildRoadmapAtSchema1110(t, "migrated-audit-shape")
	migrated, err := Open("migrated-audit-shape")
	if err != nil {
		t.Fatalf("opening the migrated database: %v", err)
	}
	defer migrated.Close() //nolint:errcheck // test cleanup

	fresh, err := Open("fresh-audit-shape")
	if err != nil {
		t.Fatalf("creating the fresh database: %v", err)
	}
	defer fresh.Close() //nolint:errcheck // test cleanup

	// 1. The same seven columns, each declared identically.
	freshCols := auditColumnDescriptors(t, fresh)
	migratedCols := auditColumnDescriptors(t, migrated)
	if len(freshCols) != len(migratedCols) {
		t.Fatalf("the fresh audit table has %d columns and the migrated one %d", len(freshCols), len(migratedCols))
	}
	for name, freshCol := range freshCols {
		migratedCol, ok := migratedCols[name]
		if !ok {
			t.Errorf("the migrated audit table has no %s column", name)
			continue
		}
		if freshCol != migratedCol {
			t.Errorf("audit.%s is %+v on a fresh database but %+v on a migrated one", name, freshCol, migratedCol)
		}
	}
	for _, c := range []struct {
		name    string
		typ     string
		notNull int
	}{
		{name: "related_entity_id", typ: "INTEGER", notNull: 0},
		{name: "commit_hash", typ: "TEXT", notNull: 0},
	} {
		got := freshCols[c.name]
		if got.typ != c.typ {
			t.Errorf("audit.%s has type %q, want %q (SPEC/DATABASE.md § `audit` Table)", c.name, got.typ, c.typ)
		}
		if got.notNull != c.notNull {
			t.Errorf("audit.%s is NOT NULL; SPEC/VERSION.md § Migration 1.11.0 to 1.12.0 requires it "+
				"nullable, because no truthful backfill exists for the rows already stored", c.name)
		}
		if got.dfltValue.Valid {
			t.Errorf("audit.%s has DEFAULT %q; every pre-existing row must receive NULL",
				c.name, got.dfltValue.String)
		}
	}

	// 2. The physical order differs, in exactly the way the specification says.
	if got := auditColumnOrder(t, fresh); !equalStrings(got, freshAuditColumnOrder) {
		t.Errorf("a fresh audit table's columns are %v, want %v (SPEC/DATABASE.md § `audit` Table)",
			got, freshAuditColumnOrder)
	}
	if got := auditColumnOrder(t, migrated); !equalStrings(got, migratedAuditColumnOrder) {
		t.Errorf("a migrated audit table's columns are %v, want %v (SPEC/VERSION.md § Migration 1.11.0 "+
			"to 1.12.0): ADD COLUMN appends, and no migration reorders the columns afterwards",
			got, migratedAuditColumnOrder)
	}

	// 3. The same indexes, and none of them on either new column.
	freshIdx := auditIndexes(t, fresh)
	migratedIdx := auditIndexes(t, migrated)
	if !equalStrings(freshIdx, migratedIdx) {
		t.Errorf("the audit indexes differ:\n  fresh:    %v\n  migrated: %v", freshIdx, migratedIdx)
	}
	if len(freshIdx) != 4 {
		t.Errorf("the fresh audit table carries %d indexes, want the 4 of SPEC/DATABASE.md "+
			"§ `audit` Table: %v", len(freshIdx), freshIdx)
	}

	// 4. Both enforce the same rules — the property the specification actually
	//    promises, and the one a reader depends on.
	seedAuditEntry(t, fresh, &models.AuditEntry{
		Operation:   string(models.OpTaskCreate),
		EntityType:  string(models.EntityTask),
		EntityID:    1,
		PerformedAt: utils.NowISO8601(),
	})
	var freshRowID int
	if err := fresh.QueryRow("SELECT MAX(id) FROM audit").Scan(&freshRowID); err != nil {
		t.Fatalf("reading the id of the fresh fixture row: %v", err)
	}

	assertAuditEntityTypeDeclaration(t, fresh, "fresh")
	assertAuditEntityTypeDeclaration(t, migrated, "migrated")
	assertAuditCheckMatrices(t, fresh, freshRowID, "fresh")
	assertAuditCheckMatrices(t, migrated, fx.seeds[0].id, "migrated")
}

// TestMigrateV1_11_0_toV1_12_0_IsIdempotent has three parts.
//
// The first runs the migration twice against a database that has NEITHER column:
// pass one adds both, pass two must be a silent no-op. The second proves the
// guards are load-bearing rather than decorative, by issuing the unguarded
// statement the migration would otherwise have run and asserting that SQLite does
// reject it with "duplicate column name". Without that part the test would pass
// just as happily against a migration with no guard at all.
//
// The third covers what SPEC/VERSION.md § Migration 1.11.0 to 1.12.0 says about
// the two guards being INDEPENDENT: a database that somehow carries one column
// and not the other must be brought to the full shape, which a single shared
// guard would not do.
func TestMigrateV1_11_0_toV1_12_0_IsIdempotent(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	newColumns := []string{"related_entity_id", "commit_hash"}

	dropBoth := func() {
		t.Helper()
		for _, column := range newColumns {
			if _, err := database.Exec("ALTER TABLE audit DROP COLUMN " + column); err != nil {
				t.Fatalf("removing %s to build the fixture: %v", column, err)
			}
		}
	}

	// CreateSchema produced the 1.12.0 shape, so take both columns away to obtain
	// a database the migration still has work to do on.
	dropBoth()

	for pass := 1; pass <= 2; pass++ {
		applyAuditMigration(t, database, pass)
		for _, column := range newColumns {
			if got := columnCount(t, database, "audit", column); got != 1 {
				t.Fatalf("after pass %d the audit table has %d %s column(s), want 1", pass, got, column)
			}
		}
	}

	// The guards are what make pass two a no-op. Prove the statements they skip
	// would in fact have failed.
	_, err := database.Exec(`ALTER TABLE audit ADD COLUMN commit_hash TEXT`)
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
		`ALTER TABLE audit ADD COLUMN related_entity_id INTEGER CHECK(related_entity_id IS NULL OR related_entity_id > 0)`,
	); err != nil {
		t.Fatalf("re-adding related_entity_id alone to build the half-migrated fixture: %v", err)
	}
	applyAuditMigration(t, database, 1)
	for _, column := range newColumns {
		if got := columnCount(t, database, "audit", column); got != 1 {
			t.Errorf("a database carrying related_entity_id but not commit_hash came out of the "+
				"migration with %d %s column(s), want 1: the two guards must be independent",
				got, column)
		}
	}
}

// TestMigrateV1_11_0_toV1_12_0_SecondRunChangesNothing is the other half of
// idempotency, and the half the column guards say nothing about: the three
// reclassification statements run again on a database they have already
// rewritten, and must leave every row exactly as it was.
//
// They are idempotent by construction — an entry the first run rewrote no longer
// carries TASK_STATUS_CHANGE, so a second run matches nothing — and this compares
// the WHOLE table before and after to prove it, rather than the operation column
// alone.
func TestMigrateV1_11_0_toV1_12_0_SecondRunChangesNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fx := buildRoadmapAtSchema1110(t, "settlement-platform-rerun")
	database, err := Open("settlement-platform-rerun")
	if err != nil {
		t.Fatalf("opening the 1.11.0 database: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	before := snapshotAuditTable(t, database)

	applyAuditMigration(t, database, 2)

	after := snapshotAuditTable(t, database)
	if len(before) != len(after) {
		t.Fatalf("the audit table holds %d rows after a second run and %d after the first",
			len(after), len(before))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("audit row %d changed on the second run:\n  after one run:  %+v\n  after two runs: %+v",
				before[i].id, before[i], after[i])
		}
	}

	// And the expectations still hold, so a second run cannot quietly agree with
	// itself on a wrong answer.
	assertReclassification(t, database, &fx, "second run")
}

// applyAuditMigration runs migrateV1_11_0_toV1_12_0 in its own transaction and
// fails the test if it errors. pass names the attempt in the failure message.
func applyAuditMigration(t *testing.T, database *DB, pass int) {
	t.Helper()

	tx, err := database.Begin()
	if err != nil {
		t.Fatalf("begin (pass %d): %v", pass, err)
	}
	if err := migrateV1_11_0_toV1_12_0(tx); err != nil {
		tx.Rollback() //nolint:errcheck // the migration error is the one reported
		t.Fatalf("migration pass %d returned an error, so it is not idempotent: %v", pass, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit (pass %d): %v", pass, err)
	}
}
