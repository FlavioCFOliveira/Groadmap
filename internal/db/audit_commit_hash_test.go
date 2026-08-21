// Package db — tests for the commit hash an audit row may carry (rmp task #262).
//
// LogAuditTx is the only audit writer the binary has, and that is what makes the
// rule in SPEC/DATABASE.md § The Commit Hash of an Audit Entry enforceable
// rather than merely stated: commit_hash belongs to exactly two of the 43
// operations in the catalogue, and the invariant that no other operation carries
// one is checked at the single INSERT instead of at every call site.
//
// Three layers guard the same rule, and each is pinned separately here because
// each catches a different mistake:
//
//  1. The writer refuses a hash on an operation that does not take one, before
//     any statement is executed. This is the layer that catches a caller
//     attaching a task's stored commit values to an unrelated row.
//  2. The column CHECK refuses a malformed hash whatever the operation. This is
//     the backstop the SPEC requires the database to provide, and it holds for
//     rows written by anything at all, not only by this function.
//  3. The row shares the fate of the transaction that wrote it, so a mutation
//     that rolls back leaves no record of a commit that never bracketed
//     anything.
package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The hashes below are real short hashes from this repository's history, so the
// fixtures read like a roadmap someone actually worked through.
const (
	auditHashWorkStarted   = "5f93b51"
	auditHashWorkConcluded = "391cff7"
)

// TestLogAuditTx_TheTwoCommitOperationsStoreTheirHash writes one row of each of
// the two operations that carry a commit hash and reads both columns back off
// the table.
//
// The read is a direct SELECT rather than GetAuditEntries: this test is about
// what the writer stored, and routing it through the reader would let a writer
// that dropped the column and a reader that invented one cancel out.
func TestLogAuditTx_TheTwoCommitOperationsStoreTheirHash(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	const startedAt = "2026-08-18T08:00:00.000Z"
	const concludedAt = "2026-08-18T17:30:00.000Z"

	if err := database.WithTransaction(func(tx *sql.Tx) error {
		if err := LogAuditTx(tx, models.OpTaskStatusDoing, models.EntityTask, 11, startedAt,
			WithCommitHash(auditHashWorkStarted)); err != nil {
			return err
		}
		return LogAuditTx(tx, models.OpTaskStatusCompleted, models.EntityTask, 11, concludedAt,
			WithCommitHash(auditHashWorkConcluded))
	}); err != nil {
		t.Fatalf("writing the two commit-carrying rows: %v", err)
	}

	rows := snapshotAuditTable(t, database)
	if len(rows) != 2 {
		t.Fatalf("the audit table holds %d rows, want 2", len(rows))
	}

	for i, want := range []struct {
		operation models.AuditOperation
		hash      string
	}{
		{models.OpTaskStatusDoing, auditHashWorkStarted},
		{models.OpTaskStatusCompleted, auditHashWorkConcluded},
	} {
		got := rows[i]
		if got.operation != string(want.operation) {
			t.Errorf("row %d is %s, want %s", i, got.operation, want.operation)
			continue
		}
		if !got.commitHash.Valid {
			t.Errorf("the %s row carries no commit_hash, want %q", want.operation, want.hash)
			continue
		}
		if got.commitHash.String != want.hash {
			t.Errorf("the %s row carries commit_hash %q, want %q",
				want.operation, got.commitHash.String, want.hash)
		}
		// A commit hash is not a counterpart: the other nullable column stays
		// NULL on both operations.
		if got.relatedEntityID.Valid {
			t.Errorf("the %s row names entity %d as a counterpart; neither operation has one",
				want.operation, got.relatedEntityID.Int64)
		}
	}
}

// TestLogAuditTx_RefusesAHashOnAnOperationThatCarriesNone covers the first
// guard. The rule the SPEC states as a MUST NOT — no command writes commit_hash
// on any other operation, TASK_REOPEN emphatically included — is enforced by the
// writer rather than left to the discipline of the call sites, so a future
// caller that reaches for a task's stored commit values gets an error instead of
// a quietly wrong audit log.
//
// The rejection must also be total: nothing may reach the table.
func TestLogAuditTx_RefusesAHashOnAnOperationThatCarriesNone(t *testing.T) {
	// TASK_REOPEN is the case the SPEC calls out by name, because a reopening
	// is precisely when a task's commit_close is in the caller's hand. The other
	// three stand for the rest of the catalogue: a sibling status operation, an
	// unrelated task operation, and a sprint operation.
	for _, op := range []models.AuditOperation{
		models.OpTaskReopen,
		models.OpTaskStatusTesting,
		models.OpTaskCreate,
		models.OpSprintAddTask,
	} {
		t.Run(string(op), func(t *testing.T) {
			database, cleanup := setupTestDB(t)
			defer cleanup()

			entityType := models.EntityTask
			if strings.HasPrefix(string(op), "SPRINT_") {
				entityType = models.EntitySprint
			}

			err := database.WithTransaction(func(tx *sql.Tx) error {
				return LogAuditTx(tx, op, entityType, 3, "2026-08-18T09:00:00.000Z",
					WithCommitHash(auditHashWorkConcluded))
			})
			if err == nil {
				t.Fatalf("LogAuditTx accepted a commit hash on %s; the column belongs to "+
					"TASK_STATUS_DOING and TASK_STATUS_COMPLETED alone "+
					"(SPEC/DATABASE.md § The Commit Hash of an Audit Entry)", op)
			}
			if !errors.Is(err, ErrAuditCommitHashNotAllowed) {
				t.Errorf("the rejection is %v, which does not chain ErrAuditCommitHashNotAllowed; a "+
					"caller cannot tell this refusal from a database failure", err)
			}
			if !strings.Contains(err.Error(), string(op)) {
				t.Errorf("the rejection %q does not name the offending operation", err)
			}

			if rows := snapshotAuditTable(t, database); len(rows) != 0 {
				t.Errorf("the refused write left %d rows behind: %+v", len(rows), rows)
			}
		})
	}
}

// TestLogAuditTx_WritesNoHashWhenNoneIsGiven is the converse of the two tests
// above, and it is the case almost every call site is in. An option list left
// empty must store SQL NULL — not the empty string, which the column CHECK
// rejects, and not a zero value that a reader could not tell from a real one.
func TestLogAuditTx_WritesNoHashWhenNoneIsGiven(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// The two operations that MAY carry a hash, invoked without one. A writer
	// that derived the value from the operation rather than from the caller
	// would fail here.
	if err := database.WithTransaction(func(tx *sql.Tx) error {
		if err := LogAuditTx(tx, models.OpTaskStatusDoing, models.EntityTask, 5,
			"2026-08-18T10:00:00.000Z"); err != nil {
			return err
		}
		return LogAuditTx(tx, models.OpTaskStatusTesting, models.EntityTask, 5,
			"2026-08-18T11:00:00.000Z")
	}); err != nil {
		t.Fatalf("writing the two hashless rows: %v", err)
	}

	for _, row := range snapshotAuditTable(t, database) {
		if row.commitHash.Valid {
			t.Errorf("the %s row carries commit_hash %q although the caller supplied none",
				row.operation, row.commitHash.String)
		}
	}
}

// TestLogAuditTx_MalformedHashFailsTheColumnCheck covers the second guard, which
// is acceptance criterion 6 of SPEC/DATABASE.md § The Commit Hash of an Audit
// Entry. The application normalises and validates a hash long before it reaches
// this function, so the CHECK is a backstop — but a backstop that has never been
// observed to fire is an assumption, not a guarantee.
//
// Every case below is a value the format rule excludes: too short, too long,
// non-hexadecimal, upper case (a stored value is always lowercase), empty, and
// whitespace-padded.
func TestLogAuditTx_MalformedHashFailsTheColumnCheck(t *testing.T) {
	for _, tc := range []struct {
		name string
		hash string
		why  string
	}{
		{"too short", "5f93b5", "six characters is below the 7-character lower bound"},
		{"too long", strings.Repeat("a", 65), "65 characters is above the 64-character upper bound"},
		{"not hexadecimal", "5f93b5z", "z is not a hexadecimal digit"},
		{"upper case", "5F93B51", "a stored hash is always lowercase"},
		{"empty", "", "an empty value is below the lower bound"},
		{"padded", " 5f93b51", "the application trims nothing, so the space is a non-hexadecimal character"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, cleanup := setupTestDB(t)
			defer cleanup()

			err := database.WithTransaction(func(tx *sql.Tx) error {
				return LogAuditTx(tx, models.OpTaskStatusDoing, models.EntityTask, 9,
					"2026-08-18T12:00:00.000Z", WithCommitHash(tc.hash))
			})
			if err == nil {
				t.Fatalf("the column CHECK accepted %q, but %s", tc.hash, tc.why)
			}
			if rows := snapshotAuditTable(t, database); len(rows) != 0 {
				t.Errorf("the rejected INSERT left %d rows behind", len(rows))
			}
		})
	}
}

// TestLogAuditTx_ACommitCarryingRowRollsBackWithItsTransaction covers the third
// guard on the row that matters most. SPEC/ARCHITECTURE.md § Security Guarantees
// forbids a committed audit entry for a change that rolled back, and a
// TASK_STATUS_COMPLETED row is the worst one to leave behind: it would assert
// that a commit concluded a task the transaction never completed.
func TestLogAuditTx_ACommitCarryingRowRollsBackWithItsTransaction(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	err := database.WithTransaction(func(tx *sql.Tx) error {
		if logErr := LogAuditTx(tx, models.OpTaskStatusCompleted, models.EntityTask, 4,
			"2026-08-18T13:00:00.000Z", WithCommitHash(auditHashWorkConcluded)); logErr != nil {
			return logErr
		}
		return errIntentional
	})
	if !errors.Is(err, errIntentional) {
		t.Fatalf("expected the intentional error to surface, got %v", err)
	}

	if rows := snapshotAuditTable(t, database); len(rows) != 0 {
		t.Errorf("the rolled-back transaction left %d audit rows behind: %+v", len(rows), rows)
	}
}

// TestGetAuditEntries_ReportsEachRowsOwnHash pins the read path against the
// defect a shared scan target produces: hoisting the sql.NullString out of the
// row loop and taking its address makes every entry point at one value, so the
// whole result reports the LAST row's hash.
//
// The fixture is deliberately mixed — a row with a hash, then one without, then
// another with a different hash — because a result whose rows all carry the same
// value cannot tell the two implementations apart.
func TestGetAuditEntries_ReportsEachRowsOwnHash(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	seeded := []struct {
		operation   models.AuditOperation
		performedAt string
		hash        string
	}{
		{models.OpTaskStatusDoing, "2026-08-18T08:00:00.000Z", auditHashWorkStarted},
		{models.OpTaskStatusTesting, "2026-08-18T09:00:00.000Z", ""},
		{models.OpTaskStatusCompleted, "2026-08-18T10:00:00.000Z", auditHashWorkConcluded},
	}
	if err := database.WithTransaction(func(tx *sql.Tx) error {
		for _, s := range seeded {
			var opts []AuditOption
			if s.hash != "" {
				opts = append(opts, WithCommitHash(s.hash))
			}
			if err := LogAuditTx(tx, s.operation, models.EntityTask, 6, s.performedAt, opts...); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seeding the audit rows: %v", err)
	}

	entries, err := database.GetAuditEntries(testContext(), &AuditFilter{Limit: 10})
	if err != nil {
		t.Fatalf("GetAuditEntries: %v", err)
	}
	if len(entries) != len(seeded) {
		t.Fatalf("the read path returned %d entries, want %d", len(entries), len(seeded))
	}

	// GetAuditEntries orders by performed_at DESC, so the seeded rows come back
	// last first.
	for i, want := range []struct {
		operation models.AuditOperation
		hash      string
	}{
		{models.OpTaskStatusCompleted, auditHashWorkConcluded},
		{models.OpTaskStatusTesting, ""},
		{models.OpTaskStatusDoing, auditHashWorkStarted},
	} {
		got := entries[i]
		if got.Operation != string(want.operation) {
			t.Errorf("entry %d is %s, want %s", i, got.Operation, want.operation)
			continue
		}
		switch {
		case want.hash == "" && got.CommitHash != nil:
			t.Errorf("the %s entry reports commit_hash %q, want nil; a scan target shared across the "+
				"row loop reports the last row's value on every entry", want.operation, *got.CommitHash)
		case want.hash != "" && got.CommitHash == nil:
			t.Errorf("the %s entry reports no commit_hash, want %q", want.operation, want.hash)
		case want.hash != "" && *got.CommitHash != want.hash:
			t.Errorf("the %s entry reports commit_hash %q, want %q",
				want.operation, *got.CommitHash, want.hash)
		}
		if got.RelatedEntityID != nil {
			t.Errorf("the %s entry reports related_entity_id %d, want nil",
				want.operation, *got.RelatedEntityID)
		}
	}
}
