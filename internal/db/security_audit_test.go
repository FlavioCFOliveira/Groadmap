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
	id, err := db.CreateTask(testContext(), &models.Task{
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
	id, err := db.CreateSprint(testContext(), &models.Sprint{
		Status:      models.SprintPending,
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
		if _, err := db.LogAuditEntry(testContext(), &models.AuditEntry{
			Operation:   "TASK_CREATE",
			EntityType:  "TASK",
			EntityID:    i + 1,
			PerformedAt: ts,
		}); err != nil {
			t.Fatalf("logging audit entry %d: %v", i, err)
		}
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

// ==================== #65: DeleteSprint atomicity ====================

// TestDeleteSprintAtomic verifies DeleteSprint resets member tasks to BACKLOG,
// removes the sprint and its sprint_tasks rows, and writes the SPRINT_DELETE
// audit entry — all consistently — so the post-call state never leaves a task
// marked SPRINT with its sprint gone (finding #65).
func TestDeleteSprintAtomic(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	sprintID := newTestSprintWithCap(t, db, "Deletable sprint", 0)
	taskIDs := []int{
		newTestTask(t, db, "Implement auth"),
		newTestTask(t, db, "Write migration"),
	}
	if err := db.AddTasksToSprint(testContext(), sprintID, taskIDs); err != nil {
		t.Fatalf("adding tasks: %v", err)
	}

	if err := db.DeleteSprint(testContext(), sprintID); err != nil {
		t.Fatalf("DeleteSprint: %v", err)
	}

	// Sprint is gone.
	if _, err := db.GetSprint(testContext(), sprintID); err == nil {
		t.Error("expected sprint to be deleted")
	}

	// No orphan sprint_tasks rows remain.
	var stCount int
	if err := db.QueryRowContext(testContext(),
		"SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ?", sprintID).Scan(&stCount); err != nil {
		t.Fatalf("counting sprint_tasks: %v", err)
	}
	if stCount != 0 {
		t.Errorf("expected 0 sprint_tasks rows, got %d", stCount)
	}

	// Member tasks were reset to BACKLOG (status/membership consistent).
	for _, id := range taskIDs {
		task, err := db.GetTask(testContext(), id)
		if err != nil {
			t.Fatalf("getting task %d: %v", id, err)
		}
		if task.Status != models.StatusBacklog {
			t.Errorf("task %d: expected BACKLOG, got %q", id, task.Status)
		}
	}

	// The SPRINT_DELETE audit entry was written in the same transaction.
	op := string(models.OpSprintDelete)
	entries, err := db.GetAuditEntries(testContext(), &AuditFilter{Operation: &op})
	if err != nil {
		t.Fatalf("GetAuditEntries: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.EntityID == sprintID && e.EntityType == string(models.EntitySprint) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a SPRINT_DELETE audit entry for the deleted sprint")
	}
}

// ==================== #66: RemoveTasksFromSprint atomicity ====================

// TestRemoveTasksFromSprintAtomic verifies that after RemoveTasksFromSprint the
// removed tasks have no sprint_tasks membership AND are reset to BACKLOG, while
// untouched tasks keep their SPRINT membership/status. Membership and
// tasks.status never diverge (finding #66).
func TestRemoveTasksFromSprintAtomic(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	sprintID := newTestSprintWithCap(t, db, "Active sprint", 0)
	taskIDs := []int{
		newTestTask(t, db, "Build API"),
		newTestTask(t, db, "Add cache"),
		newTestTask(t, db, "Tune index"),
	}
	if err := db.AddTasksToSprint(testContext(), sprintID, taskIDs); err != nil {
		t.Fatalf("adding tasks: %v", err)
	}

	removed := taskIDs[:2]
	if err := db.RemoveTasksFromSprint(testContext(), removed); err != nil {
		t.Fatalf("RemoveTasksFromSprint: %v", err)
	}

	// Removed tasks: BACKLOG status AND no membership row.
	for _, id := range removed {
		task, err := db.GetTask(testContext(), id)
		if err != nil {
			t.Fatalf("getting task %d: %v", id, err)
		}
		if task.Status != models.StatusBacklog {
			t.Errorf("removed task %d: expected BACKLOG, got %q", id, task.Status)
		}
		var member int
		if err := db.QueryRowContext(testContext(),
			"SELECT COUNT(*) FROM sprint_tasks WHERE task_id = ?", id).Scan(&member); err != nil {
			t.Fatalf("counting membership for %d: %v", id, err)
		}
		if member != 0 {
			t.Errorf("removed task %d: expected no membership, got %d rows", id, member)
		}
	}

	// Remaining task keeps SPRINT status and membership.
	remaining := taskIDs[2]
	task, err := db.GetTask(testContext(), remaining)
	if err != nil {
		t.Fatalf("getting remaining task: %v", err)
	}
	if task.Status != models.StatusSprint {
		t.Errorf("remaining task %d: expected SPRINT, got %q", remaining, task.Status)
	}
	var member int
	if err := db.QueryRowContext(testContext(),
		"SELECT COUNT(*) FROM sprint_tasks WHERE task_id = ?", remaining).Scan(&member); err != nil {
		t.Fatalf("counting membership: %v", err)
	}
	if member != 1 {
		t.Errorf("remaining task %d: expected 1 membership row, got %d", remaining, member)
	}
}

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

// TestMigrationsIdempotent verifies every ALTER TABLE ADD COLUMN migration is a
// no-op (not a "duplicate column name" error) when applied to a database that
// already has the column (finding #68, SPEC/DATABASE.md § Migration Idempotency).
func TestMigrationsIdempotent(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// CreateSchema already produced the latest schema, so every ADD-COLUMN
	// target column is present. Re-running each ALTER-bearing migration must be
	// a no-op rather than raising "duplicate column name".
	type migCase struct {
		name string
		fn   MigrationFunc
	}
	cases := []migCase{
		{"v1.0.0->v1.1.0 (sprint_tasks.position)", migrateV1_0_0_toV1_1_0},
		{"v1.2.0->v1.3.0 (tasks.completion_summary)", migrateV1_2_0_toV1_3_0},
		{"v1.3.0->v1.4.0 (sprints.max_tasks)", migrateV1_3_0_toV1_4_0},
		{"v1.4.0->v1.5.0 (tasks.parent_task_id)", migrateV1_4_0_toV1_5_0},
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
