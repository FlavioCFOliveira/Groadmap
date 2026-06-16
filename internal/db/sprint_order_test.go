package db

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestCreateSprintAutoAssignsSequentialOrder verifies that CreateSprint
// auto-assigns MAX(order_index)+1 when the caller omits an explicit order: the
// first sprint receives 1 and each subsequent sprint the next value
// (SPEC/DATABASE.md § Create Sprint).
func TestCreateSprintAutoAssignsSequentialOrder(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := testContext()

	for i := 1; i <= 3; i++ {
		s := &models.Sprint{
			Status:      models.SprintPending,
			Title:       "Sprint",
			Description: "desc",
			CreatedAt:   utils.NowISO8601(),
		}
		id, err := db.CreateSprint(ctx, s)
		if err != nil {
			t.Fatalf("CreateSprint #%d: %v", i, err)
		}
		if s.Order != i {
			t.Errorf("auto-assigned order for sprint #%d = %d, want %d", i, s.Order, i)
		}

		got, err := db.GetSprint(ctx, id)
		if err != nil {
			t.Fatalf("GetSprint #%d: %v", id, err)
		}
		if got.Order != i {
			t.Errorf("persisted order for sprint #%d = %d, want %d", id, got.Order, i)
		}
	}
}

// TestCreateSprintExplicitOrderRespected verifies that an explicit positive
// order is used verbatim rather than auto-assigned.
func TestCreateSprintExplicitOrderRespected(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := testContext()

	s := &models.Sprint{
		Status:      models.SprintPending,
		Title:       "Sprint",
		Description: "desc",
		CreatedAt:   utils.NowISO8601(),
		Order:       42,
	}
	id, err := db.CreateSprint(ctx, s)
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	got, err := db.GetSprint(ctx, id)
	if err != nil {
		t.Fatalf("GetSprint: %v", err)
	}
	if got.Order != 42 {
		t.Errorf("order = %d, want 42", got.Order)
	}

	// A later auto-assigned sprint must continue from MAX+1 = 43.
	s2 := &models.Sprint{Status: models.SprintPending, Title: "S2", Description: "d", CreatedAt: utils.NowISO8601()}
	if _, err := db.CreateSprint(ctx, s2); err != nil {
		t.Fatalf("CreateSprint #2: %v", err)
	}
	if s2.Order != 43 {
		t.Errorf("auto order after explicit 42 = %d, want 43", s2.Order)
	}
}

// TestCreateSprintDuplicateOrderRejected verifies that creating a sprint with an
// order already in use fails with ErrAlreadyExists (exit code 5), enforced by the
// idx_sprints_order unique index.
func TestCreateSprintDuplicateOrderRejected(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := testContext()

	first := &models.Sprint{Status: models.SprintPending, Title: "A", Description: "d", CreatedAt: utils.NowISO8601(), Order: 7}
	if _, err := db.CreateSprint(ctx, first); err != nil {
		t.Fatalf("CreateSprint first: %v", err)
	}

	dup := &models.Sprint{Status: models.SprintPending, Title: "B", Description: "d", CreatedAt: utils.NowISO8601(), Order: 7}
	_, err := db.CreateSprint(ctx, dup)
	if err == nil {
		t.Fatal("expected duplicate-order create to fail, got nil")
	}
	if !errors.Is(err, utils.ErrAlreadyExists) {
		t.Errorf("duplicate order error = %v, want ErrAlreadyExists (exit 5)", err)
	}
}

// TestMigrateV1_7_0_toV1_8_0Backfill is the regression gate for the order_index
// migration: it must add the column, backfill a unique, positive, deterministic
// 1..N sequence ordered by created_at ASC then id ASC, create the unique index,
// and be idempotent (SPEC/VERSION.md § Migration 1.7.0 → 1.8.0).
func TestMigrateV1_7_0_toV1_8_0Backfill(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	// Build a pre-1.8.0 sprints table (no order_index) and seed rows whose
	// created_at order differs from id order, so the backfill ordering is tested.
	if _, err := sqlDB.Exec(`CREATE TABLE sprints (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		status TEXT NOT NULL DEFAULT 'PENDING',
		title TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL,
		created_at TEXT NOT NULL,
		started_at TEXT,
		closed_at TEXT,
		max_tasks INTEGER
	)`); err != nil {
		t.Fatalf("create legacy sprints: %v", err)
	}

	// id 1 created latest, id 2 earliest, id 3 middle → expected order: 2,3,1.
	seed := []struct {
		id        int
		createdAt string
	}{
		{1, "2026-03-03T00:00:00.000Z"},
		{2, "2026-03-01T00:00:00.000Z"},
		{3, "2026-03-02T00:00:00.000Z"},
	}
	for _, s := range seed {
		if _, err := sqlDB.Exec(
			"INSERT INTO sprints (id, status, description, created_at) VALUES (?, 'PENDING', 'd', ?)",
			s.id, s.createdAt,
		); err != nil {
			t.Fatalf("seed sprint %d: %v", s.id, err)
		}
	}

	applyMigration := func() {
		tx, err := sqlDB.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if err := migrateV1_7_0_toV1_8_0(tx); err != nil {
			tx.Rollback() //nolint:errcheck
			t.Fatalf("migrateV1_7_0_toV1_8_0: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	applyMigration()

	want := map[int]int{2: 1, 3: 2, 1: 3} // id -> order_index
	for id, wantOrder := range want {
		var got int
		if err := sqlDB.QueryRow("SELECT order_index FROM sprints WHERE id = ?", id).Scan(&got); err != nil {
			t.Fatalf("read order_index for id %d: %v", id, err)
		}
		if got != wantOrder {
			t.Errorf("backfilled order_index for id %d = %d, want %d", id, got, wantOrder)
		}
	}

	// Uniqueness: the unique index must reject a duplicate order_index.
	if _, err := sqlDB.Exec(
		"INSERT INTO sprints (status, description, created_at, order_index) VALUES ('PENDING', 'd', '2026-03-04T00:00:00.000Z', 1)",
	); err == nil {
		t.Error("expected duplicate order_index insert to be rejected by idx_sprints_order, got nil")
	}

	// Idempotent: a second application must not error and must preserve the values.
	applyMigration()
	var orderForID2 int
	if err := sqlDB.QueryRow("SELECT order_index FROM sprints WHERE id = 2").Scan(&orderForID2); err != nil {
		t.Fatalf("re-read order_index: %v", err)
	}
	if orderForID2 != 1 {
		t.Errorf("order_index for id 2 after idempotent re-run = %d, want 1", orderForID2)
	}
}
