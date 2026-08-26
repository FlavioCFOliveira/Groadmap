package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// newTestTask inserts a minimal BACKLOG task and returns its id.
func newTestTask(t *testing.T, db *DB, title string) int {
	t.Helper()
	id, err := seedTask(db, &models.Task{
		Priority:               1,
		Severity:               1,
		Status:                 models.StatusBacklog,
		Title:                  title,
		FunctionalRequirements: "Why",
		TechnicalRequirements:  "How",
		AcceptanceCriteria:     "Verify",
		CreatedAt:              time.Now().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("creating task %q: %v", title, err)
	}
	return id
}

// newTestSprintWithCap inserts a sprint and, when cap > 0, sets its max_tasks.
func newTestSprintWithCap(t *testing.T, db *DB, desc string, cap int) int {
	t.Helper()
	id, err := seedSprint(db, &models.Sprint{
		Status:      models.SprintPending,
		Title:       desc,
		Description: desc,
		CreatedAt:   time.Now().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("creating sprint: %v", err)
	}
	if cap > 0 {
		if _, err := db.ExecContext(testContext(),
			"UPDATE sprints SET max_tasks = ? WHERE id = ?", cap, id); err != nil {
			t.Fatalf("setting max_tasks: %v", err)
		}
	}
	return id
}

// ==================== #64: GetAuditEntries server-side cap ====================

// TestGetAuditEntriesHardCap verifies the defense-in-depth cap clamps an
// unbounded (0) or oversized (> MaxAuditLimit) request to MaxAuditLimit, so the
// query never scans an unbounded result set even when called programmatically
// (finding #64, SPEC/DATABASE.md § Audit Result Limit).
func TestGetAuditEntriesHardCap(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert MaxAuditLimit + 50 audit rows so a clamp is observable.
	total := models.MaxAuditLimit + 50
	now := time.Now()
	for i := 0; i < total; i++ {
		// Distinct, monotonically increasing timestamps so ORDER BY is stable.
		ts := now.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		seedAuditEntry(t, db, &models.AuditEntry{
			Operation:   "TASK_CREATE",
			EntityType:  "TASK",
			EntityID:    i + 1,
			PerformedAt: ts,
		})
	}

	cases := []struct {
		name  string
		limit int
	}{
		{"zero limit is treated as unbounded then capped", 0},
		{"negative limit is capped", -1},
		{"oversized limit is capped", models.MaxAuditLimit + 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := db.GetAuditEntries(testContext(), &AuditFilter{Limit: tc.limit})
			if err != nil {
				t.Fatalf("GetAuditEntries: %v", err)
			}
			if len(entries) != models.MaxAuditLimit {
				t.Errorf("expected result capped to %d, got %d", models.MaxAuditLimit, len(entries))
			}
		})
	}

	// A valid in-range limit is honored unchanged (not forced up to the cap).
	entries, err := db.GetAuditEntries(testContext(), &AuditFilter{Limit: 10})
	if err != nil {
		t.Fatalf("GetAuditEntries (in-range): %v", err)
	}
	if len(entries) != 10 {
		t.Errorf("expected in-range limit 10 honored, got %d", len(entries))
	}
}

// ==================== #66: RemoveTasksFromSprint atomicity ====================

// The finding-#66 gate that stood here — after removing tasks from a sprint,
// membership and status agree — moved to internal/commands, because the method
// it drove (RemoveTasksFromSprint) was a copy the binary never ran and is gone
// (task #188). It is now
// TestSprintRemoveTasksKeepsMembershipAndStatusInAgreement in
// internal/commands/sprint_remove_tasks_atomicity_test.go, driving the command.

// ==================== #67: capacity enforced inside the tx ====================

// TestAddTasksToSprintCapacityEnforced verifies the authoritative capacity check
// inside AddTasksToSprint's transaction rejects a batch that would exceed
// max_tasks, returns an ErrValidation-class error, and leaves the database
// unchanged (no partial insert) — closing the TOCTOU window (finding #67).
func TestAddTasksToSprintCapacityEnforced(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	sprintID := newTestSprintWithCap(t, db, "Capped sprint", 2)
	taskIDs := []int{
		newTestTask(t, db, "Task one"),
		newTestTask(t, db, "Task two"),
		newTestTask(t, db, "Task three"),
	}

	// Adding 3 tasks to a cap-2 sprint must be rejected atomically.
	err := db.AddTasksToSprint(testContext(), sprintID, taskIDs)
	if err == nil {
		t.Fatal("expected capacity error, got nil")
	}
	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
	if !strings.Contains(err.Error(), "exceed sprint") {
		t.Errorf("expected capacity message, got %q", err.Error())
	}

	// No rows inserted: the transaction rolled back fully.
	var count int
	if err := db.QueryRowContext(testContext(),
		"SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ?", sprintID).Scan(&count); err != nil {
		t.Fatalf("counting sprint_tasks: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no rows after rejected add, got %d", count)
	}

	// A within-capacity batch succeeds.
	if err := db.AddTasksToSprint(testContext(), sprintID, taskIDs[:2]); err != nil {
		t.Fatalf("within-capacity add failed: %v", err)
	}
	if err := db.QueryRowContext(testContext(),
		"SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ?", sprintID).Scan(&count); err != nil {
		t.Fatalf("counting sprint_tasks: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 rows after valid add, got %d", count)
	}

	// A further add that overflows the cap is rejected, leaving the count at 2.
	if err := db.AddTasksToSprint(testContext(), sprintID, taskIDs[2:]); err == nil {
		t.Error("expected overflow add to be rejected")
	}
	if err := db.QueryRowContext(testContext(),
		"SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ?", sprintID).Scan(&count); err != nil {
		t.Fatalf("counting sprint_tasks: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count to stay 2 after rejected overflow, got %d", count)
	}
}

// ==================== #68: migration idempotency ====================

// TestMigrationsIdempotent verifies every ALTER TABLE migration is a no-op when
// applied to a database whose schema already matches: an ADD COLUMN must not
// raise "duplicate column name" when the column is present, and a DROP COLUMN
// must not raise "no such column" when it is already gone (finding #68 and rmp
// task #246; SPEC/DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)
// and § Migration Idempotency (ALTER TABLE DROP COLUMN)).
func TestMigrationsIdempotent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// CreateSchema already produced the latest schema, so every ADD-COLUMN
	// target column is present and every DROP-COLUMN target column is already
	// gone. Re-running each ALTER-bearing migration must be a no-op rather than
	// raising "duplicate column name" or "no such column".
	type migCase struct {
		name string
		fn   MigrationFunc
	}
	cases := []migCase{
		{"v1.0.0->v1.1.0 (sprint_tasks.position)", migrateV1_0_0_toV1_1_0},
		{"v1.2.0->v1.3.0 (tasks.completion_summary)", migrateV1_2_0_toV1_3_0},
		{"v1.3.0->v1.4.0 (sprints.max_tasks)", migrateV1_3_0_toV1_4_0},
		{"v1.4.0->v1.5.0 (tasks.parent_task_id)", migrateV1_4_0_toV1_5_0},
		{"v1.9.0->v1.10.0 (tasks.specialists, DROP)", migrateV1_9_0_toV1_10_0},
		{"v1.10.0->v1.11.0 (tasks.commit_open, tasks.commit_close)", migrateV1_10_0_toV1_11_0},
		{"v1.11.0->v1.12.0 (audit.related_entity_id, audit.commit_hash)", migrateV1_11_0_toV1_12_0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Run twice in a row; both must succeed.
			for pass := 1; pass <= 2; pass++ {
				tx, err := db.Begin()
				if err != nil {
					t.Fatalf("begin (pass %d): %v", pass, err)
				}
				if err := tc.fn(tx); err != nil {
					tx.Rollback() //nolint:errcheck
					t.Fatalf("migration pass %d returned error (not idempotent): %v", pass, err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatalf("commit (pass %d): %v", pass, err)
				}
			}
		})
	}
}

// TestColumnExists verifies the column-existence guard reports presence and
// absence correctly — the primitive that makes the ADD COLUMN migrations
// idempotent (finding #68).
func TestColumnExists(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck

	present, err := columnExists(tx, "sprints", "max_tasks")
	if err != nil {
		t.Fatalf("columnExists(present): %v", err)
	}
	if !present {
		t.Error("expected sprints.max_tasks to exist")
	}

	absent, err := columnExists(tx, "sprints", "no_such_column")
	if err != nil {
		t.Fatalf("columnExists(absent): %v", err)
	}
	if absent {
		t.Error("expected sprints.no_such_column to be absent")
	}
}

// ==================== rmp task #305: columnExists table-name shape guard ====

// TestColumnExistsRefusesUnsafeTableIdentifier is the regression test for rmp
// task #305 (CWE-89).
//
// columnExists INTERPOLATES its table argument, because SQLite does not accept a
// bound parameter as a PRAGMA argument. It was safe only because all of its
// callers happen to pass string literals — a property of the callers, which no
// caller is obliged to preserve and which the #nosec at the interpolation would
// have kept the scanner quiet about. The guard makes the safety a property of
// the FUNCTION, and this test is what holds the guard in place.
//
// The cases are the ones that discriminate. A test that only exercised valid
// names would pass against the unguarded code and would prove nothing, so every
// case here is a name the unguarded function would have interpolated: one
// carrying a quote (which closes the literal), one carrying a semicolon (which
// ends the statement and starts another), one carrying whitespace (which is not
// an identifier at all), and the empty string (which asks about a table with no
// name and answers "the column is absent" for every column, sending an ALTER at
// a table nobody named).
func TestColumnExistsRefusesUnsafeTableIdentifier(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck // read-only probe; the deferred rollback is the cleanup

	cases := []struct {
		name  string
		table string
	}{
		{
			name:  "quote closes the literal",
			table: `sprints') AND 1=1 --`,
		},
		{
			name:  "semicolon appends a second statement",
			table: `sprints'); DROP TABLE sprints; --`,
		},
		{
			name:  "whitespace is not an identifier",
			table: "sprint tasks",
		},
		{
			name:  "empty string names no table",
			table: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exists, err := columnExists(tx, tc.table, "max_tasks")
			if err == nil {
				t.Fatalf("columnExists(%q) returned no error; the name reached the statement", tc.table)
			}
			if !errors.Is(err, errUnsafeTableIdentifier) {
				t.Fatalf("columnExists(%q) failed with %v; expected the shape guard's refusal, "+
					"which means the name reached SQLite instead of being refused before the interpolation",
					tc.table, err)
			}
			if exists {
				t.Errorf("columnExists(%q) reported the column present while refusing the name", tc.table)
			}
		})
	}

	// The refusals changed nothing. sprints is still there, still carries
	// max_tasks, and is still readable on the same transaction — so neither the
	// DROP payload nor the refusals themselves left the schema or the
	// transaction damaged.
	present, err := columnExists(tx, "sprints", "max_tasks")
	if err != nil {
		t.Fatalf("columnExists after the refusals: %v", err)
	}
	if !present {
		t.Error("sprints.max_tasks is gone after the refused names; the guard did not hold")
	}
	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM sprints").Scan(&count); err != nil {
		t.Fatalf("reading sprints after the refused names: %v", err)
	}
}

// TestColumnExistsAdmitsEveryTableTheMigrationsName is the guard's other half:
// the class has to admit every name the migrations actually pass, or the guard
// would break the idempotency checks it sits inside.
//
// The four names are the complete set the ten call sites in migrations.go use,
// and each is probed with a column that exists on it and one that does not, so
// the test proves the guard passes the name THROUGH rather than merely not
// erroring on it.
func TestColumnExistsAdmitsEveryTableTheMigrationsName(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck // read-only probe; the deferred rollback is the cleanup

	cases := []struct {
		table   string
		present string
	}{
		{table: "tasks", present: "completion_summary"},
		{table: "sprints", present: "order_index"},
		{table: "sprint_tasks", present: "position"},
		{table: "audit", present: "related_entity_id"},
	}

	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			if !safeTableIdentifier.MatchString(tc.table) {
				t.Fatalf("%s is a name the migrations pass and the guard rejects", tc.table)
			}
			exists, err := columnExists(tx, tc.table, tc.present)
			if err != nil {
				t.Fatalf("columnExists(%s, %s): %v", tc.table, tc.present, err)
			}
			if !exists {
				t.Errorf("expected %s.%s to exist", tc.table, tc.present)
			}
			absent, err := columnExists(tx, tc.table, "no_such_column")
			if err != nil {
				t.Fatalf("columnExists(%s, no_such_column): %v", tc.table, err)
			}
			if absent {
				t.Errorf("expected %s.no_such_column to be absent", tc.table)
			}
		})
	}
}
