package db

import (
	"errors"
	"fmt"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The in-transaction halves of rmp tasks 490 and 575 for the two assignment
// paths that live in this package. The command layer refuses the same requests
// first; these calls go straight to the transaction, which is the authoritative
// check (SPEC/DATABASE.md § Transactional Atomicity Guarantees #3), so what is
// proved here is that the transaction itself counts and refuses the same way.

// TestAddTasksToSprint_TheTransactionCountsTheTasksThatBecomeActive drives the
// capacity check inside AddTasksToSprint directly: a sprint capped at 3 holding
// two SPRINT members and one COMPLETED member has a load of 2; naming both
// members and one outsider counts one task, which fits; naming two outsiders
// counts two, which does not, with the line the command prints.
func TestAddTasksToSprint_TheTransactionCountsTheTasksThatBecomeActive(t *testing.T) {
	database, _, cleanup := setupCountingDB(t)
	defer cleanup()
	ctx := testContext()

	sprintID := newTestSprintWithCap(t, database, "Payout controls", 3)
	a := newTestTask(t, database, "Cap the payout amount per merchant")
	b := newTestTask(t, database, "Hold payouts to a new bank account")
	done := newTestTask(t, database, "Log every payout override")
	if err := database.AddTasksToSprint(ctx, sprintID, []int{a, b, done}); err != nil {
		t.Fatalf("adding three tasks under a cap of 3: %v", err)
	}
	// A COMPLETED member does not count against the cap: the load is now 2.
	forceTaskLifecycle(t, database, []int{done}, models.StatusCompleted)

	x := newTestTask(t, database, "Alert on a payout above the limit")
	y := newTestTask(t, database, "Report held payouts daily")

	if err := database.AddTasksToSprint(ctx, sprintID, []int{a, b, x}); err != nil {
		t.Fatalf("two members and one outsider count one task against a load of 2 and a cap of 3: %v", err)
	}

	err := database.AddTasksToSprint(ctx, sprintID, []int{y, a})
	want := fmt.Sprintf("validation error: adding 1 task(s) would exceed sprint #%d capacity (3/3 tasks active)", sprintID)
	if !errors.Is(err, utils.ErrValidation) || err == nil || err.Error() != want {
		t.Errorf("an outsider past the cap: %v, want %q", err, want)
	}
	var member int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sprint_tasks WHERE task_id = ?`, y).Scan(&member); err != nil {
		t.Fatalf("reading membership: %v", err)
	}
	if member != 0 {
		t.Error("the refused addition left the outsider a member of the sprint")
	}
}

// TestAssignmentTransactions_RefuseACompletedTask pins rule 4 of the invariant
// inside both transactions: a COMPLETED task is refused by AddTasksToSprint,
// into any sprint, and by MoveTasksBetweenSprints, with the published line, and
// nothing is written.
func TestAssignmentTransactions_RefuseACompletedTask(t *testing.T) {
	database, _, cleanup := setupCountingDB(t)
	defer cleanup()
	ctx := testContext()

	from := newTestSprintWithCap(t, database, "Dispute evidence", 0)
	to := newTestSprintWithCap(t, database, "Dispute automation", 0)
	open := newTestTask(t, database, "Collect the delivery proof")
	done := newTestTask(t, database, "Collect the signed receipt")
	if err := database.AddTasksToSprint(ctx, from, []int{open, done}); err != nil {
		t.Fatalf("seeding the sprint: %v", err)
	}
	forceTaskLifecycle(t, database, []int{done}, models.StatusCompleted)

	var auditsBefore int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit`).Scan(&auditsBefore); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}

	want := fmt.Sprintf("validation error: task %d is COMPLETED in sprint #%d; a completed task stays in the "+
		"sprint it was completed in", done, from)
	for label, call := range map[string]func() error{
		"add to another sprint": func() error { return database.AddTasksToSprint(ctx, to, []int{open, done}) },
		"add to its own sprint": func() error { return database.AddTasksToSprint(ctx, from, []int{done}) },
		"move":                  func() error { return database.MoveTasksBetweenSprints(ctx, from, to, []int{open, done}) },
	} {
		err := call()
		if !errors.Is(err, utils.ErrValidation) || err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %q", label, err, want)
		}
	}

	var auditsAfter, inFrom int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit`).Scan(&auditsAfter); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ?`, from).Scan(&inFrom); err != nil {
		t.Fatalf("counting members: %v", err)
	}
	if auditsAfter != auditsBefore || inFrom != 2 {
		t.Errorf("the refusals wrote %d audit rows and left %d members in the source, want none and 2",
			auditsAfter-auditsBefore, inFrom)
	}
}
