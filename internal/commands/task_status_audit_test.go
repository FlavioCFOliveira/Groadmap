// Package commands — regression tests for what a status change records in the
// audit log (rmp task #262).
//
// The audit log used to answer "the status of this task changed" and stop
// there. Reading `TASK_STATUS_CHANGE` told you nothing about where the task
// went, so the only way to learn the outcome was to correlate the entry with
// the task's current status — which a later transition, or a reopening, has
// already destroyed. SPEC/DATABASE.md § One Row per Thing That Happened
// replaces that single operation with five that name the destination, and
// SPEC/DATABASE.md § The Commit Hash of an Audit Entry has two of them carry the
// commit that brackets the work.
//
// Four properties are pinned here, each separately because each can be
// implemented almost right:
//
//  1. **The operation names the destination.** Every transition writes the
//     operation of the state entered, and no code path writes the legacy value
//     any more — while that value stays reachable as an `--operation` filter, so
//     the rows already carrying it are not stranded.
//  2. **The commit hash reaches the entry and nothing else does.** The two
//     transitions that take a commit flag record it, normalised, equal to what
//     the task row received; every other operation in the table records NULL.
//     The last part is asserted as the table-wide invariant SPEC/DATABASE.md
//     states, not as a per-command spot check.
//  3. **One row per task, one timestamp per invocation.** A CSV of ids leaves
//     each task's own history complete, and the rows of one invocation are
//     recognisable as one invocation.
//  4. **An entry is immutable.** `task reopen` clears `tasks.commit_close` and
//     must leave every stored entry — id, operation, and hash — untouched, so
//     the record of the commit that once concluded the task survives the
//     reopening.
//
// The fixture is the one in task_commit_tracking_test.go: a started sprint with
// four member tasks, all built through the real commands.
package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ---------------------------------------------------------------------------
// Reading the audit table
// ---------------------------------------------------------------------------

// auditRecord is one stored audit row, every column of it. The two nullable
// columns are kept as sql.Null* rather than dereferenced, so an assertion can
// tell SQL NULL from a zero value — which is the whole distinction these tests
// are about.
type auditRecord struct {
	operation       string
	entityType      string
	performedAt     string
	commitHash      sql.NullString
	relatedEntityID sql.NullInt64
	id              int
	entityID        int
}

// readAuditTable reads every audit row of a roadmap, ordered by id, straight out
// of SQLite. It deliberately bypasses GetAuditEntries: these tests are about
// what was stored, and a read path that dropped a column would otherwise hide
// the defect from the very tests meant to catch it.
//
// The columns are named explicitly, because a migrated audit table carries
// related_entity_id and commit_hash appended after performed_at while a fresh
// one declares them before it.
func readAuditTable(t *testing.T, database *db.DB) []auditRecord {
	t.Helper()

	rows, err := database.Query(`SELECT id, operation, entity_type, entity_id,
	                                    related_entity_id, commit_hash, performed_at
	                             FROM audit ORDER BY id`)
	if err != nil {
		t.Fatalf("reading the audit table: %v", err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup

	records := make([]auditRecord, 0, 32)
	for rows.Next() {
		var r auditRecord
		if err := rows.Scan(&r.id, &r.operation, &r.entityType, &r.entityID,
			&r.relatedEntityID, &r.commitHash, &r.performedAt); err != nil {
			t.Fatalf("scanning an audit row: %v", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the audit table: %v", err)
	}
	return records
}

// auditRecordsFor returns the rows recorded against one task, in id order.
func auditRecordsFor(t *testing.T, database *db.DB, taskID int) []auditRecord {
	t.Helper()

	all := readAuditTable(t, database)
	out := make([]auditRecord, 0, len(all))
	for _, r := range all {
		if r.entityType == string(models.EntityTask) && r.entityID == taskID {
			out = append(out, r)
		}
	}
	return out
}

// operationsOf renders the operation column of a row set, for failure messages
// that show the whole history rather than the one row that disagreed.
func operationsOf(records []auditRecord) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = r.operation
	}
	return out
}

// countRows runs a COUNT(*) with the given WHERE clause and returns the count.
func countRows(t *testing.T, database *db.DB, query string, args ...any) int {
	t.Helper()

	var n int
	if err := database.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("counting with %q: %v", query, err)
	}
	return n
}

// ---------------------------------------------------------------------------
// 1. The operation names the destination
// ---------------------------------------------------------------------------

// TestTaskStat_EveryTransitionWritesItsOwnDestinationOperation walks one task
// through every transition `task stat` can perform and requires each one to add
// exactly one entry carrying the operation of the state entered.
//
// The walk is one task rather than four, on purpose: the four entries land in
// one history, in order, so an implementation that wrote the right number of
// entries with the wrong operations, or the right operations in the wrong
// order, fails on the sequence rather than on a count.
//
// The fifth destination operation, TASK_STATUS_SPRINT, is not reachable from
// this command at all and is asserted absent below: `task stat` rejects the
// SPRINT target, and SPEC/DATABASE.md gives that operation the single writer
// `sprint add-tasks`.
func TestTaskStat_EveryTransitionWritesItsOwnDestinationOperation(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "status-audit-destination")

	id := f.taskIDs[0]
	before := len(auditRecordsFor(t, f.database, id))

	forbiddenBefore := map[models.AuditOperation]int{}
	for _, op := range []models.AuditOperation{models.OpTaskStatusChange, models.OpTaskStatusSprint} {
		forbiddenBefore[op] = countRows(t, f.database,
			`SELECT COUNT(*) FROM audit WHERE operation = ?`, string(op))
	}

	// SPRINT → DOING → TESTING → COMPLETED → BACKLOG: every transition the
	// state machine allows from a sprint member, in one pass.
	f.stat(t, itoa(id), "DOING", "--commit-open", commitWorkStarted)
	f.stat(t, itoa(id), "TESTING")
	f.stat(t, itoa(id), "COMPLETED", "--commit-close", commitWorkConcluded)
	f.stat(t, itoa(id), "BACKLOG")

	want := []string{
		string(models.OpTaskStatusDoing),
		string(models.OpTaskStatusTesting),
		string(models.OpTaskStatusCompleted),
		string(models.OpTaskStatusBacklog),
	}

	records := auditRecordsFor(t, f.database, id)
	if len(records) != before+len(want) {
		t.Fatalf("the four transitions wrote %d entries, want %d; history: %v",
			len(records)-before, len(want), operationsOf(records))
	}

	got := operationsOf(records[before:])
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("transition %d recorded %s, want %s; the operation names the state the task ENTERED, "+
				"so a reader learns the outcome from it alone (SPEC/DATABASE.md § One Row per Thing That "+
				"Happened)\nfull history: %v", i, got[i], want[i], got)
		}
	}

	// Neither the retired operation nor the one this command cannot produce.
	// The count is taken over the whole table rather than over this task's
	// history, because a stray entry against any entity is the defect; the
	// TASK_STATUS_SPRINT baseline is not zero, since the fixture's
	// `sprint add-tasks` legitimately wrote one entry per seeded task, so what
	// is asserted is that the four transitions added none.
	for _, forbidden := range []models.AuditOperation{models.OpTaskStatusChange, models.OpTaskStatusSprint} {
		if n := countRows(t, f.database,
			`SELECT COUNT(*) FROM audit WHERE operation = ?`, string(forbidden)); n != forbiddenBefore[forbidden] {
			t.Errorf("`task stat` wrote %d %s entries; it writes neither (SPEC/COMMANDS.md § Change "+
				"Status (stat), acceptance criterion 4)", n-forbiddenBefore[forbidden], forbidden)
		}
	}
}

// TestTaskStat_EveryEntryNamesItsOwnTaskAndNoCounterpart pins the other two
// columns of a `task stat` entry: it is recorded against the task itself, and it
// names no second entity.
//
// The NULL is not an omission. `sprint remove-tasks` writes the very same
// TASK_STATUS_BACKLOG operation and does name the sprint the task left, so a
// reader has to be able to tell "this operation had no counterpart" from "it had
// one and it went unrecorded" (SPEC/DATABASE.md § The Two Entities of a
// Relational Operation, acceptance criterion 3).
func TestTaskStat_EveryEntryNamesItsOwnTaskAndNoCounterpart(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "status-audit-subject")

	id := f.taskIDs[0]
	before := len(auditRecordsFor(t, f.database, id))

	f.stat(t, itoa(id), "BACKLOG")

	records := auditRecordsFor(t, f.database, id)
	if len(records) != before+1 {
		t.Fatalf("`task stat BACKLOG` wrote %d entries, want 1", len(records)-before)
	}

	entry := records[before]
	if entry.entityType != string(models.EntityTask) || entry.entityID != id {
		t.Errorf("the entry is recorded against %s #%d, want TASK #%d",
			entry.entityType, entry.entityID, id)
	}
	if entry.relatedEntityID.Valid {
		t.Errorf("the entry names entity %d as a counterpart; no sprint is party to a `task stat` "+
			"invocation, so related_entity_id must be NULL", entry.relatedEntityID.Int64)
	}
}

// ---------------------------------------------------------------------------
// 2. The commit hash reaches the entry, and nothing else does
// ---------------------------------------------------------------------------

// TestTaskStat_TheTwoCommitTransitionsRecordTheirHash requires the entry to
// carry the hash the transition was given, in the stored lowercase form, and to
// carry exactly the value that landed in the task's own column.
//
// The hashes are supplied in upper case, which is what makes the assertion
// worth making twice over: it proves the entry took the NORMALISED value rather
// than the raw argument, and comparing it against the task column proves the two
// writes cannot drift apart.
func TestTaskStat_TheTwoCommitTransitionsRecordTheirHash(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "status-audit-commit-hash")

	id := f.taskIDs[0]

	f.stat(t, itoa(id), "DOING", "--commit-open", strings.ToUpper(commitWorkStarted))
	assertNewestEntry(t, f, id, models.OpTaskStatusDoing, commitWorkStarted)

	f.stat(t, itoa(id), "TESTING")
	assertNewestEntry(t, f, id, models.OpTaskStatusTesting, "")

	f.stat(t, itoa(id), "COMPLETED", "--commit-close", strings.ToUpper(commitWorkConcluded))
	assertNewestEntry(t, f, id, models.OpTaskStatusCompleted, commitWorkConcluded)

	// The entry and the task column hold the same value, from the same write.
	open, closed := f.commitValues(t, id)
	if open != "«"+commitWorkStarted+"»" || closed != "«"+commitWorkConcluded+"»" {
		t.Errorf("task columns are commit_open=%s commit_close=%s, want «%s» and «%s»; the audit entries "+
			"assert against these values, so the comparison above means nothing if they are wrong",
			open, closed, commitWorkStarted, commitWorkConcluded)
	}
}

// assertNewestEntry checks the operation and commit hash of the last entry
// recorded against a task. An empty wantHash means the entry must carry SQL NULL.
func assertNewestEntry(t *testing.T, f *commitTrackingFixture, taskID int,
	wantOp models.AuditOperation, wantHash string) {
	t.Helper()

	records := auditRecordsFor(t, f.database, taskID)
	if len(records) == 0 {
		t.Fatalf("task #%d has no audit entry at all", taskID)
	}
	newest := records[len(records)-1]

	if newest.operation != string(wantOp) {
		t.Errorf("the newest entry of task #%d is %s, want %s; history: %v",
			taskID, newest.operation, wantOp, operationsOf(records))
		return
	}
	switch {
	case wantHash == "" && newest.commitHash.Valid:
		t.Errorf("the %s entry carries commit_hash %q; only TASK_STATUS_DOING and "+
			"TASK_STATUS_COMPLETED carry one (SPEC/DATABASE.md § The Commit Hash of an Audit Entry)",
			wantOp, newest.commitHash.String)
	case wantHash != "" && !newest.commitHash.Valid:
		t.Errorf("the %s entry carries no commit_hash; it must carry %q, the value the transition "+
			"also wrote to the task", wantOp, wantHash)
	case wantHash != "" && newest.commitHash.String != wantHash:
		t.Errorf("the %s entry carries commit_hash %q, want %q — the supplied hash normalised to "+
			"lowercase (SPEC/MODELS.md § Task, Commit Hash Constraint)",
			wantOp, newest.commitHash.String, wantHash)
	}
}

// TestAudit_CommitHashIsNullOnEveryOtherOperation asserts the table-wide
// invariant SPEC/DATABASE.md § The Commit Hash of an Audit Entry states as
// acceptance criterion 3, with the query it states it in.
//
// It is a table-wide COUNT rather than a per-command check on purpose: a spot
// check has to be extended by hand for every operation added afterwards, and the
// operation that eventually breaks the rule is the one nobody thought to add.
// The workout below drives a broad slice of the catalogue first — task
// lifecycle, priority, severity, reopening, dependencies, comments, and the
// sprint operations — so the COUNT is taken over a table that actually holds
// entries the rule could be broken on.
func TestAudit_CommitHashIsNullOnEveryOtherOperation(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "status-audit-hash-invariant")

	first, second, third := f.taskIDs[0], f.taskIDs[1], f.taskIDs[2]

	// A completed task, so a TASK_STATUS_COMPLETED entry with a hash exists and
	// the COUNT below is taken over a table that holds the two operations the
	// rule permits as well as the ones it forbids.
	f.walkToCompleted(t, first)

	// Reopening clears tasks.commit_close. An implementation that copied a
	// task's stored commit values onto unrelated entries would break the
	// invariant right here.
	run(t, func() error { return taskReopen([]string{"-r", f.roadmap, itoa(first)}) })

	// Priority, severity and a dependency pair.
	run(t, func() error { return taskSetPriority([]string{"-r", f.roadmap, itoa(second), "7"}) })
	run(t, func() error { return taskSetSeverity([]string{"-r", f.roadmap, itoa(second), "4"}) })
	run(t, func() error { return taskAddDep([]string{"-r", f.roadmap, itoa(second), itoa(third)}) })
	run(t, func() error { return taskRemoveDep([]string{"-r", f.roadmap, itoa(second), itoa(third)}) })

	// A comment, whose entry is recorded against the parent task.
	run(t, func() error {
		return taskCommentAdd([]string{"-r", f.roadmap, itoa(second), "--type", "DECISION",
			"--body", "The rotation window is one hour, agreed with the platform team."})
	})

	// Sprint membership and lifecycle.
	run(t, func() error { return sprintRemoveTasks([]string{"-r", f.roadmap, itoa(f.sprintID), itoa(third)}) })
	run(t, func() error { return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.sprintID), itoa(third)}) })
	run(t, func() error { return sprintClose([]string{"-r", f.roadmap, itoa(f.sprintID), "--force"}) })
	run(t, func() error { return sprintReopen([]string{"-r", f.roadmap, itoa(f.sprintID)}) })

	// The workout has to have produced entries of more than the two permitted
	// operations, or the invariant below would hold vacuously.
	distinct := countRows(t, f.database,
		`SELECT COUNT(DISTINCT operation) FROM audit
		 WHERE operation NOT IN ('TASK_STATUS_DOING', 'TASK_STATUS_COMPLETED')`)
	const minOtherOperations = 8
	if distinct < minOtherOperations {
		t.Fatalf("the workout produced only %d distinct operations outside the two that carry a hash, "+
			"want at least %d; the invariant below would be measuring almost nothing",
			distinct, minOtherOperations)
	}

	// SPEC/DATABASE.md § The Commit Hash of an Audit Entry, acceptance
	// criterion 3, verbatim.
	offenders := countRows(t, f.database,
		`SELECT COUNT(*) FROM audit
		 WHERE commit_hash IS NOT NULL
		   AND operation NOT IN ('TASK_STATUS_DOING', 'TASK_STATUS_COMPLETED')`)
	if offenders != 0 {
		t.Errorf("%d audit entries carry a commit_hash on an operation that does not take one "+
			"(the query is SPEC/DATABASE.md § The Commit Hash of an Audit Entry, acceptance criterion 3, "+
			"verbatim)\ntable:\n%v", offenders, operationsOf(readAuditTable(t, f.database)))
	}

	// The converse: the two operations that DO take a hash always carry one at
	// schema 1.12.0, because both flags are mandatory on the transitions that
	// write them. Without this half, an implementation that never wrote the
	// column at all would satisfy the invariant above.
	missing := countRows(t, f.database,
		`SELECT COUNT(*) FROM audit
		 WHERE commit_hash IS NULL
		   AND operation IN ('TASK_STATUS_DOING', 'TASK_STATUS_COMPLETED')`)
	if missing != 0 {
		t.Errorf("%d TASK_STATUS_DOING / TASK_STATUS_COMPLETED entries carry no commit_hash; both "+
			"commit flags are mandatory, so an entry written at schema 1.12.0 always carries one", missing)
	}
}

// run executes a command handler, discarding its stdout, and fails on error.
func run(t *testing.T, fn func() error) {
	t.Helper()

	var err error
	_ = captureStdout(t, func() { err = fn() })
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 3. One row per task, one timestamp per invocation
// ---------------------------------------------------------------------------

// TestTaskStat_CSVWritesOneEntryPerTaskSharingOneTimestamp covers the bulk form.
// Each task gets an entry of its own naming itself, so neither task's history is
// silent about the transition, and all the entries of the invocation share one
// performed_at, so they are recognisable as one invocation
// (SPEC/COMMANDS.md § Change Status (stat), acceptance criterion 1).
func TestTaskStat_CSVWritesOneEntryPerTaskSharingOneTimestamp(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "status-audit-batch")

	batch := []int{f.taskIDs[0], f.taskIDs[1], f.taskIDs[2]}
	before := len(readAuditTable(t, f.database))

	f.stat(t, f.idCSV(batch...), "DOING", "--commit-open", commitWorkStarted)

	all := readAuditTable(t, f.database)
	written := all[before:]
	if len(written) != len(batch) {
		t.Fatalf("a %d-id invocation wrote %d entries, want one per task: %v",
			len(batch), len(written), operationsOf(written))
	}

	named := make(map[int]bool, len(batch))
	for _, r := range written {
		if r.operation != string(models.OpTaskStatusDoing) {
			t.Errorf("entry %d of the batch is %s, want %s", r.id, r.operation, models.OpTaskStatusDoing)
		}
		if r.entityType != string(models.EntityTask) {
			t.Errorf("entry %d is recorded against %s, want TASK", r.id, r.entityType)
		}
		if !r.commitHash.Valid || r.commitHash.String != commitWorkStarted {
			t.Errorf("entry %d for task #%d carries commit_hash %v, want %q; one hash applies to the "+
				"whole batch (SPEC/COMMANDS.md § Change Status (stat), Commit Tracking Behavior)",
				r.id, r.entityID, r.commitHash, commitWorkStarted)
		}
		named[r.entityID] = true
	}

	for _, id := range batch {
		if !named[id] {
			t.Errorf("no entry names task #%d; every task of the batch gets an entry naming itself, or "+
				"its own history is silent about the transition", id)
		}
	}

	// One invocation, one timestamp.
	stamp := written[0].performedAt
	for _, r := range written[1:] {
		if r.performedAt != stamp {
			t.Errorf("entry %d was stamped %s and entry %d %s; all the entries of one invocation share "+
				"one performed_at", written[0].id, stamp, r.id, r.performedAt)
		}
	}
}

// TestTaskStat_AFailedTransactionLeavesNoAuditEntry proves the entry and the
// status update commit together, at the level the SPEC states it: the audit
// write is inside the same transaction as the update, so a failure anywhere in
// it leaves neither behind.
//
// The failure is injected the only way it can be without touching production
// code — by moving the audit table aside, so the UPDATE succeeds and the audit
// INSERT that follows it fails. Asserting on a validation rejection instead would
// prove nothing about the transaction: those rejections happen before the
// database is even opened.
func TestTaskStat_AFailedTransactionLeavesNoAuditEntry(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "status-audit-rollback")

	id := f.taskIDs[0]
	before := readAuditTable(t, f.database)
	statusBefore := f.snapshot(t, id)

	// The handler reopens the roadmap, so the schema change has to be committed.
	if err := f.database.WithTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE audit RENAME TO audit_moved_aside`)
		return err
	}); err != nil {
		t.Fatalf("moving the audit table aside: %v", err)
	}

	err := f.statErr(t, itoa(id), "DOING", "--commit-open", commitWorkStarted)
	if !strings.Contains(err.Error(), "audit") {
		t.Fatalf("the injected failure is not the audit write, so the rollback below proves nothing: %v", err)
	}

	if err := f.database.WithTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE audit_moved_aside RENAME TO audit`)
		return err
	}); err != nil {
		t.Fatalf("restoring the audit table: %v", err)
	}

	if after := readAuditTable(t, f.database); len(after) != len(before) {
		t.Errorf("the rolled-back transaction left %d audit entries behind (%d → %d)",
			len(after)-len(before), len(before), len(after))
	}
	assertUnchanged(t, statusBefore, f.snapshot(t, id),
		"a transaction that could not write its audit entry")
}

// ---------------------------------------------------------------------------
// 4. An entry is immutable
// ---------------------------------------------------------------------------

// TestTaskReopen_LeavesEveryStoredEntryUntouched is the case
// SPEC/DATABASE.md § The Commit Hash of an Audit Entry singles out: reopening
// clears tasks.commit_close, and it MUST NOT alter, delete, or blank any audit
// entry. The historical record of the commit that once concluded the task
// therefore survives a reopening even though the task no longer carries it.
//
// The comparison is over the whole row set, field by field, so an implementation
// that blanked one column, renumbered an id, or deleted and rewrote an entry all
// fail — not only one that cleared the hash.
func TestTaskReopen_LeavesEveryStoredEntryUntouched(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "status-audit-reopen-immutable")

	id := f.taskIDs[0]
	f.walkToCompleted(t, id)

	before := readAuditTable(t, f.database)
	completed := findEntry(t, before, models.OpTaskStatusCompleted, id)
	if !completed.commitHash.Valid || completed.commitHash.String != commitWorkConcluded {
		t.Fatalf("the TASK_STATUS_COMPLETED entry carries commit_hash %v, want %q; the survival "+
			"assertion below would prove nothing", completed.commitHash, commitWorkConcluded)
	}

	run(t, func() error { return taskReopen([]string{"-r", f.roadmap, itoa(id)}) })

	after := readAuditTable(t, f.database)
	if len(after) != len(before)+1 {
		t.Fatalf("the reopening changed the entry count by %d, want exactly +1 (the TASK_REOPEN entry); "+
			"history: %v", len(after)-len(before), operationsOf(after))
	}
	for i := range before {
		if after[i] != before[i] {
			t.Errorf("the reopening rewrote a stored audit entry\n  before: %+v\n  after:  %+v\n"+
				"an audit entry is immutable: no command updates or deletes one "+
				"(SPEC/DATABASE.md § The Commit Hash of an Audit Entry)", before[i], after[i])
		}
	}

	// It wrote TASK_REOPEN, and no TASK_STATUS_BACKLOG entry, even though the
	// task ends in BACKLOG (SPEC/COMMANDS.md § Reopen Task, rule 1).
	newest := after[len(after)-1]
	if newest.operation != string(models.OpTaskReopen) {
		t.Errorf("the reopening wrote %s, want %s", newest.operation, models.OpTaskReopen)
	}
	if newest.commitHash.Valid {
		t.Errorf("the TASK_REOPEN entry carries commit_hash %q; the reopening clears the task's "+
			"commit_close and writes no hash anywhere", newest.commitHash.String)
	}

	// The task itself has lost commit_close, which is what makes the entry the
	// only surviving record of that commit.
	if _, closed := f.commitValues(t, id); closed != "<NULL>" {
		t.Errorf("tasks.commit_close = %s after the reopening, want NULL", closed)
	}
}

// TestTaskStat_RecompletingAddsASecondEntryRatherThanReplacingTheFirst covers
// acceptance criterion 5 of the same section. Two completions leave two entries
// carrying the two different hashes, so the history says which commit concluded
// which cycle.
func TestTaskStat_RecompletingAddsASecondEntryRatherThanReplacingTheFirst(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "status-audit-recomplete")

	id := f.taskIDs[0]
	f.walkToCompleted(t, id)
	run(t, func() error { return taskReopen([]string{"-r", f.roadmap, itoa(id)}) })

	// A second cycle, concluded at a different commit. The reopening left the
	// task in BACKLOG with its sprint membership intact, and BACKLOG's only
	// valid target is SPRINT, which nothing but `sprint add-tasks` may set — so
	// the membership is cycled to put the task back in a state it can be
	// started from.
	const secondConclusion = "b7b8d7b"
	run(t, func() error { return sprintRemoveTasks([]string{"-r", f.roadmap, itoa(f.sprintID), itoa(id)}) })
	run(t, func() error { return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.sprintID), itoa(id)}) })
	f.stat(t, itoa(id), "DOING", "--commit-open", commitWorkResumed)
	f.stat(t, itoa(id), "TESTING")
	f.stat(t, itoa(id), "COMPLETED", "--commit-close", secondConclusion)

	var hashes []string
	for _, r := range auditRecordsFor(t, f.database, id) {
		if r.operation == string(models.OpTaskStatusCompleted) {
			hashes = append(hashes, r.commitHash.String)
		}
	}
	if len(hashes) != 2 {
		t.Fatalf("task #%d has %d TASK_STATUS_COMPLETED entries, want 2: re-completing adds an entry "+
			"rather than replacing the first", id, len(hashes))
	}
	if hashes[0] != commitWorkConcluded || hashes[1] != secondConclusion {
		t.Errorf("the two TASK_STATUS_COMPLETED entries carry %v, want [%q %q] in that order",
			hashes, commitWorkConcluded, secondConclusion)
	}

	// The same holds for a re-entry into DOING, which replaces the task's
	// commit_open without touching the entry written on the first entry.
	var openHashes []string
	for _, r := range auditRecordsFor(t, f.database, id) {
		if r.operation == string(models.OpTaskStatusDoing) {
			openHashes = append(openHashes, r.commitHash.String)
		}
	}
	if len(openHashes) != 2 || openHashes[0] != commitWorkStarted || openHashes[1] != commitWorkResumed {
		t.Errorf("the TASK_STATUS_DOING entries carry %v, want [%q %q]; a re-entry adds an entry and "+
			"leaves the first one alone", openHashes, commitWorkStarted, commitWorkResumed)
	}
}

// findEntry returns the single entry of the given operation recorded against a
// task, failing when there is not exactly one.
func findEntry(t *testing.T, records []auditRecord, op models.AuditOperation, taskID int) auditRecord {
	t.Helper()

	var found []auditRecord
	for _, r := range records {
		if r.operation == string(op) && r.entityType == string(models.EntityTask) && r.entityID == taskID {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("task #%d has %d %s entries, want exactly 1", taskID, len(found), op)
	}
	return found[0]
}

// ---------------------------------------------------------------------------
// The legacy operation: never written, always reachable
// ---------------------------------------------------------------------------

// TestAudit_LegacyStatusChangeIsNeverWrittenButStaysReachable pins both halves
// of what LEGACY means, because either half alone is a defect.
//
// Writing the value again would undo the whole point of naming the destination;
// dropping it from the valid set would strand the entries a pre-1.12.0 binary
// wrote, and that the migration could not reclassify, behind a filter value the
// CLI rejects. The seeded entry stands in for exactly those rows: it is written
// with the verbatim INSERT the 1.11.0 binary used, because LogAuditTx will not
// write a legacy operation and must not be able to.
func TestAudit_LegacyStatusChangeIsNeverWrittenButStaysReachable(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "status-audit-legacy")

	id := f.taskIDs[0]
	f.walkToCompleted(t, id)
	run(t, func() error { return taskReopen([]string{"-r", f.roadmap, itoa(id)}) })

	// Half 1: nothing the commands did wrote the legacy operation.
	if n := countRows(t, f.database,
		`SELECT COUNT(*) FROM audit WHERE operation = ?`, string(models.OpTaskStatusChange)); n != 0 {
		t.Errorf("%d %s entries were written by commands running at schema 1.12.0; the operation is "+
			"LEGACY and no code path may produce one (SPEC/DATABASE.md § audit Table, Legacy)",
			n, models.OpTaskStatusChange)
	}

	// A stored row from before the catalogue was refined.
	const legacyStamp = "2026-01-04T09:15:00.000Z"
	if _, err := f.database.Exec(
		`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
		string(models.OpTaskStatusChange), string(models.EntityTask), id, legacyStamp,
	); err != nil {
		t.Fatalf("seeding the legacy audit row: %v", err)
	}

	// Half 2: the filter still accepts the name and reaches the row.
	if !models.IsValidAuditOperation(string(models.OpTaskStatusChange)) {
		t.Fatalf("%s is no longer in the valid set, so `audit list --operation %s` is rejected and the "+
			"stored rows carrying it are unreachable by name", models.OpTaskStatusChange, models.OpTaskStatusChange)
	}

	var listErr error
	out := captureStdout(t, func() {
		listErr = auditList([]string{"-r", f.roadmap, "--operation", string(models.OpTaskStatusChange)})
	})
	if listErr != nil {
		t.Fatalf("`audit list --operation %s` failed: %v", models.OpTaskStatusChange, listErr)
	}

	var listed []models.AuditEntry
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("decoding `audit list` output %q: %v", out, err)
	}
	if len(listed) != 1 {
		t.Fatalf("`audit list --operation %s` returned %d entries, want the 1 legacy entry stored",
			models.OpTaskStatusChange, len(listed))
	}
	if listed[0].PerformedAt != legacyStamp || listed[0].EntityID != id {
		t.Errorf("the returned entry is %+v, want the seeded one (task #%d at %s)",
			listed[0], id, legacyStamp)
	}
	if listed[0].CommitHash != nil || listed[0].RelatedEntityID != nil {
		t.Errorf("the legacy entry reports commit_hash %v and related_entity_id %v; a row written "+
			"before both columns existed carries NULL in each",
			listed[0].CommitHash, listed[0].RelatedEntityID)
	}
}

// TestAuditEntry_BothNullableKeysAreAlwaysPresent pins the JSON shape
// SPEC/DATA_FORMATS.md § Audit Entry requires: seven keys on every entry, the
// two nullable ones present with the value null rather than omitted, so an agent
// can parse an entry without per-operation knowledge.
//
// The history read here is mixed on purpose. It holds the TASK_STATUS_SPRINT
// entry the fixture's `sprint add-tasks` wrote, which names the sprint, and the
// `task stat` entries, which name nothing, so both renderings of the same key
// are asserted in one pass — an entry shape that omitted the key when it is
// null, or one that rendered the absent counterpart as 0, fails here.
func TestAuditEntry_BothNullableKeysAreAlwaysPresent(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "status-audit-json-shape")

	id := f.taskIDs[0]
	f.stat(t, itoa(id), "DOING", "--commit-open", commitWorkStarted)
	f.stat(t, itoa(id), "TESTING")

	var err error
	out := captureStdout(t, func() {
		err = auditHistory([]string{"-r", f.roadmap, "TASK", itoa(id)})
	})
	if err != nil {
		t.Fatalf("`audit history TASK %d` failed: %v", id, err)
	}

	var raw []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("decoding `audit history` output %q: %v", out, err)
	}
	if len(raw) < 2 {
		t.Fatalf("`audit history TASK %d` returned %d entries, want at least the two transitions", id, len(raw))
	}

	wantKeys := []string{"id", "operation", "entity_type", "entity_id",
		"related_entity_id", "commit_hash", "performed_at"}
	for i, entry := range raw {
		if len(entry) != len(wantKeys) {
			t.Errorf("entry %d carries %d keys, want exactly %d: %v", i, len(entry), len(wantKeys), entry)
		}
		for _, key := range wantKeys {
			if _, present := entry[key]; !present {
				t.Errorf("entry %d omits the key %q; every entry carries all seven, the nullable ones "+
					"with the value null (SPEC/DATA_FORMATS.md § Audit Entry)", i, key)
			}
		}
		// related_entity_id is null on every `task stat` entry and carries the
		// sprint id on the one `sprint add-tasks` wrote, so the key is
		// exercised in both renderings here rather than assumed in either.
		want := "null"
		if string(entry["operation"]) == strconv.Quote(string(models.OpTaskStatusSprint)) {
			want = itoa(f.sprintID)
		}
		if got := string(entry["related_entity_id"]); got != want {
			t.Errorf("entry %d (%s) renders related_entity_id as %s, want %s",
				i, entry["operation"], got, want)
		}
	}

	newest := raw[0] // GetAuditEntries orders by performed_at DESC.
	if got := string(newest["commit_hash"]); got != "null" {
		t.Errorf("the TASK_STATUS_TESTING entry renders commit_hash as %s, want null — never 0 or \"\"", got)
	}
}

// ---------------------------------------------------------------------------
// The read path returns what was stored
// ---------------------------------------------------------------------------

// TestGetAuditEntries_RoundTripsBothNullableColumns closes the gap between the
// two halves of this feature: the writer can be perfect and the reader still
// report nil for every entry, or — worse — report one entry's hash on all of
// them, which is what happens when a scan target is hoisted out of the row loop.
//
// The assertion is per entry over a mixed result, so a reader that shared one
// backing value across the rows fails on the entries that must be null. Both
// nullable columns are round-tripped against what SQLite actually holds:
// related_entity_id is non-NULL on the TASK_STATUS_SPRINT entry the fixture's
// `sprint add-tasks` wrote and NULL on every `task stat` entry, so the column
// is read back in both states.
func TestGetAuditEntries_RoundTripsBothNullableColumns(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "status-audit-read-path")

	id := f.taskIDs[0]
	f.walkToCompleted(t, id)

	entries, err := f.database.GetEntityHistory(context.Background(), string(models.EntityTask), id)
	if err != nil {
		t.Fatalf("reading the history of task #%d: %v", id, err)
	}

	stored := auditRecordsFor(t, f.database, id)
	if len(entries) != len(stored) {
		t.Fatalf("the read path returned %d entries and the table holds %d", len(entries), len(stored))
	}

	byID := make(map[int]auditRecord, len(stored))
	for _, r := range stored {
		byID[r.id] = r
	}

	nonNil, named := 0, 0
	for i := range entries {
		want, ok := byID[entries[i].ID]
		if !ok {
			t.Errorf("the read path returned entry %d, which is not in the table", entries[i].ID)
			continue
		}
		switch {
		case want.commitHash.Valid && entries[i].CommitHash == nil:
			t.Errorf("entry %d stores commit_hash %q but the read path reports nil",
				want.id, want.commitHash.String)
		case !want.commitHash.Valid && entries[i].CommitHash != nil:
			t.Errorf("entry %d stores NULL in commit_hash but the read path reports %q; a scan target "+
				"hoisted out of the row loop reports the last row's value on every entry",
				want.id, *entries[i].CommitHash)
		case want.commitHash.Valid:
			if *entries[i].CommitHash != want.commitHash.String {
				t.Errorf("entry %d stores commit_hash %q but the read path reports %q",
					want.id, want.commitHash.String, *entries[i].CommitHash)
			}
			nonNil++
		}
		switch {
		case want.relatedEntityID.Valid && entries[i].RelatedEntityID == nil:
			t.Errorf("entry %d stores related_entity_id %d but the read path reports nil",
				want.id, want.relatedEntityID.Int64)
		case !want.relatedEntityID.Valid && entries[i].RelatedEntityID != nil:
			t.Errorf("entry %d stores NULL in related_entity_id but the read path reports %d; no "+
				"`task stat` entry names a counterpart", want.id, *entries[i].RelatedEntityID)
		case want.relatedEntityID.Valid:
			if int64(*entries[i].RelatedEntityID) != want.relatedEntityID.Int64 {
				t.Errorf("entry %d stores related_entity_id %d but the read path reports %d",
					want.id, want.relatedEntityID.Int64, *entries[i].RelatedEntityID)
			}
			named++
		}
	}

	// The history holds both kinds of each column, so no branch above passed
	// vacuously: two entries carry a hash, and exactly one names a counterpart.
	if nonNil != 2 {
		t.Errorf("the history of a task taken to COMPLETED reported %d entries carrying a hash, want 2 "+
			"(the entry into DOING and the entry into COMPLETED)", nonNil)
	}
	if named != 1 {
		t.Errorf("the history reported %d entries naming a counterpart, want 1 (the TASK_STATUS_SPRINT "+
			"entry `sprint add-tasks` wrote, which names the sprint the task entered)", named)
	}
}
