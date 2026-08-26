package commands

import (
	"context"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the finding-#66 gate: after `sprint remove-tasks`, sprint_tasks
// membership and tasks.status agree — the removed tasks have no membership row
// and are back in BACKLOG, and the tasks left behind keep both their membership
// and their SPRINT status (SPEC/DATABASE.md § Transactional Atomicity
// Guarantees #2).
//
// It used to live in internal/db, against a RemoveTasksFromSprint method there.
// The command layer had replaced that method with its own transaction long
// before, so the gate was guarding a copy the binary never ran — and the copy
// had drifted: it deleted membership by task_id alone, ignoring the sprint
// argument, which is precisely the corruption finding #40 fixed in the shipped
// path, and it reset the status without clearing the lifecycle timestamps,
// which is finding #49. Neither was reported, because nothing exercised the
// shipped path here. The method is gone and this gate now drives the command
// (task #188), so a future drift has nowhere to hide.
func TestSprintRemoveTasksKeepsMembershipAndStatusInAgreement(t *testing.T) {
	const roadmap = "settlement-membership-atomicity"

	t.Setenv("HOME", t.TempDir())
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	taskIDs := createAtomicityTasks(t, database, roadmap)
	sprintID := createAtomicitySprint(t, database, roadmap)

	run(t, func() error {
		return sprintAddTasks([]string{"-r", roadmap, itoa(sprintID), csv(taskIDs...)})
	})

	removed, kept := taskIDs[:2], taskIDs[2]
	run(t, func() error {
		return sprintRemoveTasks([]string{"-r", roadmap, itoa(sprintID), csv(removed...)})
	})

	for _, id := range removed {
		task, err := database.GetTask(context.Background(), id)
		if err != nil {
			t.Fatalf("reading removed task %d: %v", id, err)
		}
		if task.Status != models.StatusBacklog {
			t.Errorf("removed task %d: status %q, want BACKLOG", id, task.Status)
		}
		if got := membershipRows(t, database, id); got != 0 {
			t.Errorf("removed task %d: %d sprint_tasks rows, want 0", id, got)
		}
	}

	task, err := database.GetTask(context.Background(), kept)
	if err != nil {
		t.Fatalf("reading the task left in the sprint: %v", err)
	}
	if task.Status != models.StatusSprint {
		t.Errorf("task %d left in the sprint: status %q, want SPRINT", kept, task.Status)
	}
	if got := membershipRows(t, database, kept); got != 1 {
		t.Errorf("task %d left in the sprint: %d sprint_tasks rows, want 1", kept, got)
	}
}

// membershipRows counts the sprint_tasks rows a task holds. The UNIQUE
// constraint on task_id caps it at one, so the only interesting values are 0
// and 1 — and a value above 1 would itself be the defect.
func membershipRows(t *testing.T, database *db.DB, taskID int) int {
	t.Helper()

	var n int
	if err := database.QueryRow(
		"SELECT COUNT(*) FROM sprint_tasks WHERE task_id = ?", taskID,
	).Scan(&n); err != nil {
		t.Fatalf("counting membership rows of task %d: %v", taskID, err)
	}
	return n
}

// createAtomicityTasks seeds three BACKLOG tasks through `task create` and
// returns their ids in creation order.
func createAtomicityTasks(t *testing.T, database *db.DB, roadmap string) []int {
	t.Helper()

	for _, s := range []struct{ title, functional, technical, acceptance string }{
		{
			"Reconcile the acquirer settlement file against the ledger",
			"Every settlement line must be matched to a ledger entry before payout.",
			"Stream the acquirer file and match on the settlement reference.",
			"A day's file reconciles with no unmatched line.",
		},
		{
			"Publish the residual of each settlement window",
			"An operator can see, per window, what did not balance.",
			"Aggregate the unmatched amounts per window and expose them on the report.",
			"The report shows a residual per window, zero when the window balances.",
		},
		{
			"Retry a failed payout instruction without duplicating it",
			"A transient failure must not cost the merchant a second payout.",
			"Key the instruction on the settlement reference and make the submit idempotent.",
			"Two submissions of one instruction result in a single payout.",
		},
	} {
		run(t, func() error {
			return taskCreate([]string{
				"-r", roadmap, "-t", s.title, "-fr", s.functional, "-tr", s.technical, "-ac", s.acceptance,
			})
		})
	}

	tasks, err := database.ListTasks(context.Background(), nil)
	if err != nil {
		t.Fatalf("reading the seeded tasks back: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("seeded 3 tasks, found %d", len(tasks))
	}

	ids := make([]int, 0, len(tasks))
	for i := range tasks {
		ids = append(ids, tasks[i].ID)
	}
	return ids
}

// createAtomicitySprint seeds one PENDING sprint through `sprint create` and
// returns its id.
func createAtomicitySprint(t *testing.T, database *db.DB, roadmap string) int {
	t.Helper()

	run(t, func() error {
		return sprintCreate([]string{
			"-r", roadmap,
			"-t", "Settlement reconciliation",
			"-d", "Close the reconciliation gaps the March incident exposed.",
		})
	})

	sprints, err := database.ListSprints(context.Background(), nil)
	if err != nil {
		t.Fatalf("reading the seeded sprint back: %v", err)
	}
	if len(sprints) != 1 {
		t.Fatalf("seeded 1 sprint, found %d", len(sprints))
	}
	return sprints[0].ID
}
