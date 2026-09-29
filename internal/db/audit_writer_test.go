package db

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The tests in this file pin SPEC/DATABASE.md § Insert Audit Entry, "One
// prepared statement per transaction": a transaction that writes several audit
// rows prepares the INSERT once and executes it once per row, and batching the
// writes changes no stored value.

// storedAuditRows reads every audit row in id order, with every column the
// writers set.
func storedAuditRows(t *testing.T, database *DB) []string {
	t.Helper()
	rows, err := database.Query(`SELECT operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at
		FROM audit ORDER BY id`)
	if err != nil {
		t.Fatalf("reading the audit rows: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var op, entityType, performedAt string
		var entityID int
		var related sql.NullInt64
		var hash sql.NullString
		if err := rows.Scan(&op, &entityType, &entityID, &related, &hash, &performedAt); err != nil {
			t.Fatalf("scanning an audit row: %v", err)
		}
		out = append(out, fmt.Sprintf("%s %s %d %v %v %s", op, entityType, entityID, related, hash, performedAt))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the audit rows: %v", err)
	}
	return out
}

// auditRowSpec is one row a test writes through either writer.
type auditRowSpec struct {
	op         models.AuditOperation
	entityType models.EntityType
	entityID   int
	opts       []AuditOption
}

// auditRowSpecs covers each shape of row: no optional column, a related entity,
// and a commit hash.
var auditRowSpecs = []auditRowSpec{
	{models.OpTaskCreate, models.EntityTask, 41, nil},
	{models.OpSprintAddTask, models.EntitySprint, 7, []AuditOption{WithRelatedEntity(41)}},
	{models.OpTaskStatusSprint, models.EntityTask, 41, []AuditOption{WithRelatedEntity(7)}},
	{models.OpTaskStatusDoing, models.EntityTask, 41, []AuditOption{WithCommitHash("9f2c4e1ab")}},
	{models.OpTaskPriorityChange, models.EntityTask, 42, nil},
	{models.OpTaskStatusCompleted, models.EntityTask, 41, []AuditOption{WithCommitHash("0d4ee51c7a")}},
}

// TestAuditWriterStoresTheRowsLogAuditTxStores writes the same rows once through
// LogAuditTx, one call per row, and once through one AuditWriter, and requires
// the stored rows to be identical.
func TestAuditWriterStoresTheRowsLogAuditTxStores(t *testing.T) {
	const performedAt = "2026-05-11T08:30:00.000Z"

	single, _, cleanupSingle := setupCountingDB(t)
	defer cleanupSingle()
	err := single.WithTransaction(func(tx *sql.Tx) error {
		for _, r := range auditRowSpecs {
			if err := LogAuditTx(tx, r.op, r.entityType, r.entityID, performedAt, r.opts...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("writing through LogAuditTx: %v", err)
	}

	batched, counter, cleanupBatched := setupCountingDB(t)
	defer cleanupBatched()
	counter.reset()
	err = batched.WithTransaction(func(tx *sql.Tx) error {
		w := NewAuditWriter(tx)
		defer w.Close()
		for _, r := range auditRowSpecs {
			if err := w.Log(r.op, r.entityType, r.entityID, performedAt, r.opts...); err != nil {
				return err
			}
		}
		return w.Close()
	})
	if err != nil {
		t.Fatalf("writing through AuditWriter: %v", err)
	}

	if got, want := storedAuditRows(t, batched), storedAuditRows(t, single); !slices.Equal(got, want) {
		t.Errorf("the batched rows differ from the single-row writes:\n got %v\nwant %v", got, want)
	}
	if got := counter.prepared(auditInsertSQL); got != 1 {
		t.Errorf("the audit INSERT was prepared %d times in one transaction, want once", got)
	}
	if got := counter.executed(auditInsertSQL); got != len(auditRowSpecs) {
		t.Errorf("the audit INSERT was executed %d times, want %d", got, len(auditRowSpecs))
	}
}

// TestAuditWriterValidatesEveryRow requires the per-operation column rules to
// bite on a batched write exactly as on a single one: a row that breaks them is
// refused with the same sentinel and is not written, and a writer refused on its
// first row has prepared nothing.
func TestAuditWriterValidatesEveryRow(t *testing.T) {
	database, counter, cleanup := setupCountingDB(t)
	defer cleanup()
	const performedAt = "2026-05-11T08:30:00.000Z"

	cases := []struct {
		name string
		row  auditRowSpec
		want error
	}{
		{"commit hash on an operation that carries none",
			auditRowSpec{models.OpTaskPriorityChange, models.EntityTask, 3, []AuditOption{WithCommitHash("9f2c4e1ab")}},
			ErrAuditCommitHashNotAllowed},
		{"related entity on an operation that carries none",
			auditRowSpec{models.OpTaskCreate, models.EntityTask, 3, []AuditOption{WithRelatedEntity(9)}},
			ErrAuditRelatedEntityNotAllowed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			counter.reset()
			err := database.WithTransaction(func(tx *sql.Tx) error {
				w := NewAuditWriter(tx)
				defer w.Close()
				return w.Log(c.row.op, c.row.entityType, c.row.entityID, performedAt, c.row.opts...)
			})
			if !errors.Is(err, c.want) {
				t.Fatalf("Log returned %v, want %v", err, c.want)
			}
			if got := counter.prepared(auditInsertSQL); got != 0 {
				t.Errorf("a writer refused on its first row prepared the INSERT %d times", got)
			}
		})
	}

	// A refusal in the middle of a batch leaves the rows before it to the
	// caller's transaction, which rolls back: nothing is stored.
	err := database.WithTransaction(func(tx *sql.Tx) error {
		w := NewAuditWriter(tx)
		defer w.Close()
		if err := w.Log(models.OpTaskCreate, models.EntityTask, 3, performedAt); err != nil {
			return err
		}
		return w.Log(models.OpTaskCreate, models.EntityTask, 3, performedAt, WithCommitHash("9f2c4e1ab"))
	})
	if !errors.Is(err, ErrAuditCommitHashNotAllowed) {
		t.Fatalf("a batch with an invalid second row returned %v", err)
	}
	if rows := storedAuditRows(t, database); len(rows) != 0 {
		t.Errorf("a refused batch stored %d rows, want none: %v", len(rows), rows)
	}
}

// TestAuditWriterCloseIsIdempotent closes a writer that prepared nothing, one
// that prepared its statement, and one already closed.
func TestAuditWriterCloseIsIdempotent(t *testing.T) {
	database, _, cleanup := setupCountingDB(t)
	defer cleanup()
	err := database.WithTransaction(func(tx *sql.Tx) error {
		idle := NewAuditWriter(tx)
		if err := idle.Close(); err != nil {
			return fmt.Errorf("closing an idle writer: %w", err)
		}
		w := NewAuditWriter(tx)
		if err := w.Log(models.OpTaskCreate, models.EntityTask, 5, "2026-05-11T08:30:00.000Z"); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return fmt.Errorf("closing a writer: %w", err)
		}
		return w.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestBatchCommandsPrepareTheAuditInsertOnce drives the db-layer batch writers
// over many tasks and requires one preparation of the audit INSERT per
// transaction, one execution per row, and no row written outside the prepared
// statement.
func TestBatchCommandsPrepareTheAuditInsertOnce(t *testing.T) {
	database, counter, cleanup := setupCountingDB(t)
	defer cleanup()
	ctx := testContext()

	const n = 40
	ids := make([]int, n)
	for i := range ids {
		ids[i] = newTestTask(t, database, fmt.Sprintf("Migrate the payout scheduler of cluster %d", i+1))
	}
	from := newTestSprintWithCap(t, database, "Payout scheduler migration", 0)
	to := newTestSprintWithCap(t, database, "Payout scheduler cut-over", 0)

	steps := []struct {
		name string
		run  func() error
		rows int
	}{
		{"sprint add-tasks", func() error { return database.AddTasksToSprint(ctx, from, ids) }, 2 * n},
		{"sprint move-tasks", func() error { return database.MoveTasksBetweenSprints(ctx, from, to, ids) }, 2 * n},
		{"task add-dep", func() error { return database.AddTaskDependencyWithAudit(ctx, ids[0], ids[1]) }, 2},
		{"task remove-dep", func() error { return database.RemoveTaskDependencyWithAudit(ctx, ids[0], ids[1]) }, 2},
		{"field edit", func() error {
			return database.WithTransaction(func(tx *sql.Tx) error {
				return LogAuditFieldsTx(tx, models.EntityTask, ids[2], "2026-05-11T08:30:00.000Z",
					models.OpTaskTitleChange, models.OpTaskTypeChange, models.OpTaskPriorityChange)
			})
		}, 3},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			counter.reset()
			if err := s.run(); err != nil {
				t.Fatalf("%s: %v", s.name, err)
			}
			if got := counter.prepared(auditInsertSQL); got != 1 {
				t.Errorf("the audit INSERT was prepared %d times, want once", got)
			}
			if got := counter.executed(auditInsertSQL); got != s.rows {
				t.Errorf("the audit INSERT was executed %d times, want %d", got, s.rows)
			}
		})
	}
}
