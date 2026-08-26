// Package commands — the gates for what the three position-ordering commands
// record in the audit log (rmp task #320).
//
// SPEC/COMMANDS.md § Audit of the ordering commands states the rule without
// qualification: the ordering commands "write one entry per invocation, against
// the sprint, with NULL related_entity_id and NULL commit_hash", and "A no-op
// move (moving a task to the position it already holds) still writes its entry,
// on the same rule that governs `task edit`: the audit log records the command
// issued, not the delta it produced."
//
// The measured defect: `sprint move-to`, `sprint top` and `sprint bottom` wrote
// nothing at all when the task already held the target slot, while every one of
// those invocations exited 0 and printed a success payload. The caller was told
// the command had succeeded and the log held no trace of it having been issued
// — which is precisely the question an audit log exists to answer. The three
// commands share ONE guard, in db.MoveTaskToPosition, so one entry point
// produced all three symptoms.
//
// Two gates, and they are deliberately complementary:
//
//  1. TestOrderingCommands_ANoOpInvocationStillWritesItsEntry drives each of the
//     three commands against a task that already holds the target position and
//     requires exactly one new row, of the published shape, per invocation.
//  2. TestOrderingCommands_ARealMoveStillWritesExactlyOneEntry drives the same
//     three commands as REAL moves. It is what stops the first gate from being
//     satisfied by a change that writes the entry twice, or that writes it from
//     the guard and again from the tail of the routine.
//
// Both gates compare the WHOLE audit table before and after each invocation
// rather than counting one operation. That is what lets them also pin the
// second half of the same specification paragraph — "These commands write no
// TASK_STATUS_* entry and no entry against any task" — without a separate
// assertion: the single row the table gains is named, and any other row the
// invocation might have added would be a row the comparison does not expect.
//
// The rows are read straight out of SQLite through the helpers in
// task_status_audit_test.go, for the reason given there: a read path that
// dropped a column would otherwise hide the defect from the very tests meant to
// catch it.
package commands

import (
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ---------------------------------------------------------------------------
// Assertions shared by the two gates
// ---------------------------------------------------------------------------

// assertOneNewAuditRow requires that an invocation left the stored history
// untouched and appended exactly one row, which it returns.
//
// The prefix is compared row by row rather than by length, so an implementation
// that rewrote or replaced an earlier entry — instead of appending — fails here
// and not silently.
func assertOneNewAuditRow(t *testing.T, before, after []auditRecord, label string) auditRecord {
	t.Helper()

	if len(after) != len(before)+1 {
		t.Fatalf("%s took the audit table from %d rows to %d, want exactly one more row; "+
			"the history now reads %v",
			label, len(before), len(after), operationsOf(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("%s changed the stored audit row at index %d: %+v became %+v",
				label, i, before[i], after[i])
		}
	}
	return after[len(after)-1]
}

// assertMovePositionShape pins the form SPEC/COMMANDS.md § Audit of the
// ordering commands publishes for the entry: the operation is
// SPRINT_TASK_MOVE_POSITION, it is recorded against the SPRINT rather than the
// moved task, and both nullable columns are SQL NULL.
//
// The row is taken by pointer because auditRecord carries every column of the
// audit table, and copying the whole row into each assertion is waste the
// linter is right to flag.
func assertMovePositionShape(t *testing.T, r *auditRecord, sprintID int, label string) {
	t.Helper()

	if r.operation != string(models.OpSprintTaskMovePosition) {
		t.Errorf("%s wrote operation %q, want %q",
			label, r.operation, models.OpSprintTaskMovePosition)
	}
	if r.entityType != string(models.EntitySprint) {
		t.Errorf("%s recorded the entry against entity type %q, want %q: ordering changes the "+
			"membership rows of a sprint, so the sprint is the entity whose state changed",
			label, r.entityType, models.EntitySprint)
	}
	if r.entityID != sprintID {
		t.Errorf("%s recorded the entry against entity %d, want sprint %d",
			label, r.entityID, sprintID)
	}
	if r.relatedEntityID.Valid {
		t.Errorf("%s stored related_entity_id = %d, want NULL: the operation names no counterpart entity",
			label, r.relatedEntityID.Int64)
	}
	if r.commitHash.Valid {
		t.Errorf("%s stored commit_hash = %q, want NULL: only the two commit-carrying task "+
			"transitions record a hash", label, r.commitHash.String)
	}
	if r.performedAt == "" {
		t.Errorf("%s stored an empty performed_at", label)
	}
}

// movePositionRows counts the SPRINT_TASK_MOVE_POSITION rows in a roadmap. It
// exists for failure messages that state how many entries the whole run
// produced, next to the row-level assertions above.
func movePositionRows(records []auditRecord) int {
	n := 0
	for _, r := range records {
		if r.operation == string(models.OpSprintTaskMovePosition) {
			n++
		}
	}
	return n
}

// orderingInvocation is one command under test, named as the specification
// names it and invoked through the real handler.
type orderingInvocation struct {
	invoke func() error
	name   string
	// want is the member order the invocation must leave behind.
	want []int
}

// runOrderingGate drives a sequence of invocations against one sprint,
// asserting after each that the audit table gained exactly one row of the
// published shape and that the sprint holds the expected order.
func runOrderingGate(t *testing.T, f *densityFixture, database *db.DB, invocations []orderingInvocation) {
	t.Helper()

	for _, inv := range invocations {
		t.Run(inv.name, func(t *testing.T) {
			before := readAuditTable(t, database)

			run(t, inv.invoke)

			after := readAuditTable(t, database)
			entry := assertOneNewAuditRow(t, before, after, "`"+inv.name+"`")
			assertMovePositionShape(t, &entry, f.running, "`"+inv.name+"`")

			if got := f.order(t, f.running); !sameIDs(got, inv.want) {
				t.Errorf("`%s` left the sprint holding %v, want %v", inv.name, got, inv.want)
			}
			f.assertDense(t, inv.name)
		})
	}
}

// ---------------------------------------------------------------------------
// 1. A no-op invocation still writes its entry
// ---------------------------------------------------------------------------

// TestOrderingCommands_ANoOpInvocationStillWritesItsEntry is the regression
// gate for the defect itself: each of the three commands, asked to move a task
// to the position it already holds, must write exactly one entry.
//
// The three run in sequence against one sprint, and each asserts the order is
// the SAME order the invocation found. That second half matters as much as the
// count: the fix removes the audit entry from behind the equality guard, and it
// must not remove the guard, which is what keeps a no-op from rewriting every
// membership row of the sprint for nothing.
func TestOrderingCommands_ANoOpInvocationStillWritesItsEntry(t *testing.T) {
	f := setupDensityRoadmap(t, "ordering-noop-audit")

	members := f.order(t, f.running)
	if len(members) != densityMembers {
		t.Fatalf("the fixture sprint holds %d members, want %d", len(members), densityMembers)
	}
	unchanged := append([]int(nil), members...)
	first, middle, last := members[0], members[2], members[len(members)-1]

	startedWith := movePositionRows(readAuditTable(t, f.database))
	if startedWith != 0 {
		t.Fatalf("the fixture already holds %d SPRINT_TASK_MOVE_POSITION rows, so this gate "+
			"cannot attribute the rows it counts to its own invocations", startedWith)
	}

	runOrderingGate(t, f, f.database, []orderingInvocation{
		{
			// The task sits at index 2 of a dense run, and index 2 is the target.
			name: "sprint move-to onto the position the task already holds",
			invoke: func() error {
				return sprintMoveTo([]string{"-r", f.roadmap, itoa(f.running), itoa(middle), "2"})
			},
			want: unchanged,
		},
		{
			// `top` targets position 0, and this task already holds it.
			name: "sprint top on the task that is already first",
			invoke: func() error {
				return sprintTop([]string{"-r", f.roadmap, itoa(f.running), itoa(first)})
			},
			want: unchanged,
		},
		{
			// `bottom` derives its target from the member count, so this task
			// already holds it.
			name: "sprint bottom on the task that is already last",
			invoke: func() error {
				return sprintBottom([]string{"-r", f.roadmap, itoa(f.running), itoa(last)})
			},
			want: unchanged,
		},
	})

	if got := movePositionRows(readAuditTable(t, f.database)); got != 3 {
		t.Errorf("three no-op invocations wrote %d SPRINT_TASK_MOVE_POSITION rows, want 3", got)
	}
}

// ---------------------------------------------------------------------------
// 2. A real move still writes exactly one entry
// ---------------------------------------------------------------------------

// TestOrderingCommands_ARealMoveStillWritesExactlyOneEntry is the other half of
// the rule, and the guard against over-correcting the first half. A move that
// really moves the task wrote one entry before this change and must write one
// entry after it — one, not two, and still against the sprint with both
// nullable columns NULL.
//
// The three moves run in sequence over one sprint, each asserting the exact
// resulting order, so an implementation that wrote the right number of entries
// while reordering wrongly fails on the order rather than on the count.
func TestOrderingCommands_ARealMoveStillWritesExactlyOneEntry(t *testing.T) {
	f := setupDensityRoadmap(t, "ordering-real-move-audit")

	members := f.order(t, f.running)
	if len(members) != densityMembers {
		t.Fatalf("the fixture sprint holds %d members, want %d", len(members), densityMembers)
	}
	a, b, c, d, e := members[0], members[1], members[2], members[3], members[4]

	runOrderingGate(t, f, f.database, []orderingInvocation{
		{
			// The first member takes index 3; the three it passes shift up one.
			name: "sprint move-to down the list",
			invoke: func() error {
				return sprintMoveTo([]string{"-r", f.roadmap, itoa(f.running), itoa(a), "3"})
			},
			want: []int{b, c, d, a, e},
		},
		{
			// The last member takes the first slot.
			name: "sprint top from the last slot",
			invoke: func() error {
				return sprintTop([]string{"-r", f.roadmap, itoa(f.running), itoa(e)})
			},
			want: []int{e, b, c, d, a},
		},
		{
			// The first member takes the last slot.
			name: "sprint bottom from the first slot",
			invoke: func() error {
				return sprintBottom([]string{"-r", f.roadmap, itoa(f.running), itoa(e)})
			},
			want: []int{b, c, d, a, e},
		},
	})

	if got := movePositionRows(readAuditTable(t, f.database)); got != 3 {
		t.Errorf("three real moves wrote %d SPRINT_TASK_MOVE_POSITION rows, want 3", got)
	}
}
