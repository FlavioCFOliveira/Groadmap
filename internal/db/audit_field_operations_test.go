// Package db — tests for the writer of per-field audit rows (rmp task #264).
//
// `task edit` and `sprint update` record one row per field the invocation
// supplied, and every row of one invocation carries the same performed_at
// (SPEC/COMMANDS.md § Edit Task and § Update Sprint). LogAuditFieldsTx is the
// function both commands call, so it is where those two properties are pinned
// at the level that can actually prove them: the commands top out at seven and
// four fields respectively, and seven rows are written inside a single
// millisecond, so a re-stamped write would be indistinguishable from a correct
// one there.
package db

import (
	"database/sql"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// fieldAuditBatch is the number of rows the timestamp test writes. It is far
// larger than any invocation the CLI can produce, because the subject is the
// writer rather than the commands: what has to be observable is the difference
// between one timestamp and one per row, and that difference only exists once
// the batch takes longer to write than the timestamp's millisecond resolution.
// The test measures that premise rather than assuming it — see the control
// batch there.
const fieldAuditBatch = 400

// repeatedOps returns n copies of op. The operation is immaterial to the two
// properties under test; the count is not.
func repeatedOps(op models.AuditOperation, n int) []models.AuditOperation {
	ops := make([]models.AuditOperation, n)
	for i := range ops {
		ops[i] = op
	}
	return ops
}

// distinctStamps returns the number of rows recorded against one entity and the
// number of distinct performed_at values among them.
func distinctStamps(t *testing.T, db *DB, entityID int) (rows, stamps int) {
	t.Helper()

	if err := db.QueryRow(
		`SELECT COUNT(*), COUNT(DISTINCT performed_at) FROM audit WHERE entity_id = ?`, entityID,
	).Scan(&rows, &stamps); err != nil {
		t.Fatalf("counting the rows of entity %d: %v", entityID, err)
	}
	return rows, stamps
}

// TestLogAuditFieldsTx_WritesOneRowPerOperationInTheOrderGiven pins the shape of
// the write: one row per operation, none merged and none dropped, stored in the
// order the caller listed them so the history reads in the order the UPDATE
// applied the columns.
//
// The operations are all different and deliberately not in catalogue order, so a
// writer that sorted them, de-duplicated them, or kept only the first fails on
// the sequence rather than on the count.
func TestLogAuditFieldsTx_WritesOneRowPerOperationInTheOrderGiven(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	const (
		taskID = 42
		now    = "2026-08-21T09:00:00.000Z"
	)
	want := []models.AuditOperation{
		models.OpTaskTypeChange,
		models.OpTaskTitleChange,
		models.OpTaskAcceptanceCriteriaChange,
		models.OpTaskPriorityChange,
	}

	if err := db.WithTransaction(func(tx *sql.Tx) error {
		return LogAuditFieldsTx(tx, models.EntityTask, taskID, now, want...)
	}); err != nil {
		t.Fatalf("writing %d field rows: %v", len(want), err)
	}

	rows, err := db.Query(
		`SELECT operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at
		 FROM audit ORDER BY id`)
	if err != nil {
		t.Fatalf("reading the rows back: %v", err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup

	got := make([]models.AuditOperation, 0, len(want))
	for rows.Next() {
		var (
			operation, entityType, performedAt string
			entityID                           int
			related                            sql.NullInt64
			hash                               sql.NullString
		)
		if err := rows.Scan(&operation, &entityType, &entityID, &related, &hash, &performedAt); err != nil {
			t.Fatalf("scanning a row: %v", err)
		}
		if entityType != string(models.EntityTask) || entityID != taskID {
			t.Errorf("a %s row is recorded against %s #%d, want TASK #%d",
				operation, entityType, entityID, taskID)
		}
		if performedAt != now {
			t.Errorf("a %s row carries performed_at %q, want %q", operation, performedAt, now)
		}
		// A field edit has no counterpart entity and brackets no commit, so
		// both nullable columns stay NULL (SPEC/DATABASE.md § The Two Entities
		// of a Relational Operation and § The Commit Hash of an Audit Entry).
		if related.Valid {
			t.Errorf("a %s row names entity %d as a counterpart; a field edit has none",
				operation, related.Int64)
		}
		if hash.Valid {
			t.Errorf("a %s row carries the commit hash %q; only the two status operations do",
				operation, hash.String)
		}
		got = append(got, models.AuditOperation(operation))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the rows: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("%d operations wrote %d rows, want %d: %v", len(want), len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d carries %s, want %s; the rows are stored in the order the caller "+
				"listed the fields\ngot:  %v\nwant: %v", i, got[i], want[i], got, want)
		}
	}
}

// TestLogAuditFieldsTx_NoOperationsWritesNoRow covers the no-op `task edit`
// documents: an invocation that supplies no field writes no entry and is not an
// error (SPEC/COMMANDS.md § Edit Task, acceptance criterion 2).
func TestLogAuditFieldsTx_NoOperationsWritesNoRow(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	if err := db.WithTransaction(func(tx *sql.Tx) error {
		return LogAuditFieldsTx(tx, models.EntityTask, 42, utils.NowISO8601())
	}); err != nil {
		t.Fatalf("writing no field rows: %v, want nil", err)
	}

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit`).Scan(&rows); err != nil {
		t.Fatalf("counting the audit table: %v", err)
	}
	if rows != 0 {
		t.Errorf("an invocation that supplied no field wrote %d rows, want 0", rows)
	}
}

// TestLogAuditFieldsTx_OnePerformedAtForTheWholeInvocation pins the property
// that makes the rows of one edit recognisable as one edit: they all carry the
// same performed_at.
//
// The batch is deliberately large, and the test proves its own premise rather
// than asserting it. performed_at has millisecond resolution, so over the four
// or seven rows a real invocation writes, a writer that stamped each row as it
// wrote it would be indistinguishable from a correct one — the assertion would
// hold by accident. The control batch below writes the same number of rows with
// exactly that defect and requires it to show: if the control collapses to a
// single timestamp, the machine wrote the whole batch inside one millisecond and
// this gate could not have detected the defect in the subject either, which is
// reported as a failure of the test rather than as a pass.
func TestLogAuditFieldsTx_OnePerformedAtForTheWholeInvocation(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	const (
		subjectID = 1 // rows written by LogAuditFieldsTx
		controlID = 2 // rows written with one timestamp per row
	)
	ops := repeatedOps(models.OpTaskTitleChange, fieldAuditBatch)

	// The control first, so a premise that does not hold is reported before the
	// subject's result is read and possibly misinterpreted.
	if err := db.WithTransaction(func(tx *sql.Tx) error {
		for _, op := range ops {
			if err := LogAuditTx(tx, op, models.EntityTask, controlID, utils.NowISO8601()); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("writing the control batch: %v", err)
	}

	controlRows, controlStamps := distinctStamps(t, db, controlID)
	if controlRows != fieldAuditBatch {
		t.Fatalf("the control batch wrote %d rows, want %d", controlRows, fieldAuditBatch)
	}
	if controlStamps < 2 {
		t.Fatalf("the control batch stamped every row separately and still produced %d distinct "+
			"performed_at value(s): %d rows were written inside one millisecond, so this gate cannot "+
			"tell a per-row timestamp from a shared one. Raise fieldAuditBatch above %d",
			controlStamps, fieldAuditBatch, fieldAuditBatch)
	}

	// The subject: the same number of rows through the real writer.
	if err := db.WithTransaction(func(tx *sql.Tx) error {
		return LogAuditFieldsTx(tx, models.EntityTask, subjectID, utils.NowISO8601(), ops...)
	}); err != nil {
		t.Fatalf("writing the subject batch: %v", err)
	}

	subjectRows, subjectStamps := distinctStamps(t, db, subjectID)
	if subjectRows != fieldAuditBatch {
		t.Fatalf("the invocation wrote %d rows, want %d", subjectRows, fieldAuditBatch)
	}
	if subjectStamps != 1 {
		t.Errorf("the %d rows of one invocation carry %d distinct performed_at values, want 1; the "+
			"timestamp is captured once for the whole command, not once per row (the control batch "+
			"produced %d, so the difference is observable here)",
			subjectRows, subjectStamps, controlStamps)
	}
}
