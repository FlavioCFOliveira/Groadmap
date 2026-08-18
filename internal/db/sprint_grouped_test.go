package db

import (
	"testing"
)

// This file covers the grouped sprint resolution of SPEC/DATABASE.md § Resolve
// the Sprint of Many Tasks (Grouped): the read that answers "which sprint does
// each of these tasks belong to?" in one round trip, which the read-only web
// interface uses to name the sprint on every card of its Kanban board
// (SPEC/WEB.md § Roadmap Tasks Page).
//
// The two properties are tested apart, as they are for the grouped comment read:
// what the read RETURNS is measured here against a real schema, and that it
// issues exactly ONE statement is measured with the driver-level statement
// counter of comments_stmtcount_test.go, which counts what the connection sends.

// addTasksToSprint puts tasks in a sprint through the production write path.
func addTasksToSprint(t *testing.T, db *DB, sprintID int, taskIDs ...int) {
	t.Helper()

	if err := db.AddTasksToSprint(testContext(), sprintID, taskIDs); err != nil {
		t.Fatalf("adding tasks %v to sprint %d: %v", taskIDs, sprintID, err)
	}
}

// TestGroupedTaskSprintsRead proves the grouped read keys every task by its own
// sprint, returns at most one entry per task, leaves a task that belongs to no
// sprint out of the map, and returns an empty map for an empty id set.
func TestGroupedTaskSprintsRead(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	authentication := newTestSprintWithCap(t, db, "Authentication hardening", 0)
	settlement := newTestSprintWithCap(t, db, "Settlement reconciliation", 0)

	inAuthentication := newTestTask(t, db, "Rate-limit the token endpoint")
	inSettlement := newTestTask(t, db, "Reconcile the settlement ledger against the acquirer report")
	alsoInSettlement := newTestTask(t, db, "Alert on any settlement window that fails to balance")
	inNoSprint := newTestTask(t, db, "Document the settlement reconciliation runbook")

	addTasksToSprint(t, db, authentication, inAuthentication)
	addTasksToSprint(t, db, settlement, inSettlement, alsoInSettlement)

	sprints, err := db.GetSprintsByTasks(testContext(),
		[]int{inNoSprint, alsoInSettlement, inAuthentication, inSettlement})
	if err != nil {
		t.Fatalf("GetSprintsByTasks: %v", err)
	}

	// Three of the four tasks are in a sprint; the fourth is absent from the map,
	// which is the answer rather than a missing one.
	if len(sprints) != 3 {
		t.Errorf("the grouped read returned %d entries (%v), want 3: a task in no sprint is absent",
			len(sprints), sprints)
	}
	if _, present := sprints[inNoSprint]; present {
		t.Errorf("task %d belongs to no sprint but is present in the map", inNoSprint)
	}
	if got := sprints[inNoSprint]; got != (SprintRef{}) {
		t.Errorf("sprints[%d] = %+v, want the zero SprintRef for an absent key", inNoSprint, got)
	}

	// Each task resolves to ITS OWN sprint, identified by both id and title, so a
	// read that returned the same sprint for every task could not pass.
	for _, c := range []struct {
		wantTitle string
		taskID    int
		wantID    int
	}{
		{"Authentication hardening", inAuthentication, authentication},
		{"Settlement reconciliation", inSettlement, settlement},
		{"Settlement reconciliation", alsoInSettlement, settlement},
	} {
		got := sprints[c.taskID]
		if got.ID != c.wantID || got.Title != c.wantTitle {
			t.Errorf("sprints[%d] = %+v, want sprint #%d %q", c.taskID, got, c.wantID, c.wantTitle)
		}
	}

	// An empty id set: an empty map, no error, and never a nil one. (That it also
	// issues no statement is proven below.)
	empty, err := db.GetSprintsByTasks(testContext(), nil)
	if err != nil {
		t.Fatalf("GetSprintsByTasks with no ids: %v", err)
	}
	if empty == nil {
		t.Error("GetSprintsByTasks returned a nil map for an empty id set; it must return an empty map")
	}
	if len(empty) != 0 {
		t.Errorf("GetSprintsByTasks with no ids returned %d entries, want 0", len(empty))
	}

	// Duplicate ids are harmless: the map still carries one entry per task.
	duplicated, err := db.GetSprintsByTasks(testContext(),
		[]int{inSettlement, inSettlement, inAuthentication})
	if err != nil {
		t.Fatalf("GetSprintsByTasks with duplicate ids: %v", err)
	}
	if len(duplicated) != 2 {
		t.Errorf("a duplicated id yielded %d entries (%v), want 2", len(duplicated), duplicated)
	}
	if got := duplicated[inSettlement]; got.ID != settlement {
		t.Errorf("duplicated[%d] = %+v, want sprint #%d", inSettlement, got, settlement)
	}
}

// TestGroupedTaskSprintsReadIsOneRowPerTaskAfterAMove pins the "at most one row
// per task" guarantee against the one operation that could break it: moving a
// task from one sprint to another. The guarantee is the schema's — the UNIQUE
// constraint on sprint_tasks.task_id — so this test also proves the constraint is
// really in force, and that the read therefore needs no de-duplication step.
func TestGroupedTaskSprintsReadIsOneRowPerTaskAfterAMove(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	first := newTestSprintWithCap(t, db, "Authentication hardening", 0)
	second := newTestSprintWithCap(t, db, "Settlement reconciliation", 0)
	moved := newTestTask(t, db, "Audit the session-cookie flags")

	addTasksToSprint(t, db, first, moved)
	if err := db.MoveTasksBetweenSprints(testContext(), first, second, []int{moved}); err != nil {
		t.Fatalf("moving task %d from sprint %d to sprint %d: %v", moved, first, second, err)
	}

	// The membership row count is the property under test: one row, for the sprint
	// the task was moved to.
	var rows int
	if err := db.QueryRowContext(testContext(),
		"SELECT COUNT(*) FROM sprint_tasks WHERE task_id = ?", moved).Scan(&rows); err != nil {
		t.Fatalf("counting sprint_tasks rows of task %d: %v", moved, err)
	}
	if rows != 1 {
		t.Fatalf("task %d has %d sprint_tasks rows, want exactly 1", moved, rows)
	}

	sprints, err := db.GetSprintsByTasks(testContext(), []int{moved})
	if err != nil {
		t.Fatalf("GetSprintsByTasks: %v", err)
	}
	if len(sprints) != 1 {
		t.Fatalf("the grouped read returned %d entries for one task, want 1", len(sprints))
	}
	if got := sprints[moved]; got.ID != second {
		t.Errorf("sprints[%d] = %+v, want the sprint the task was moved to (#%d)", moved, got, second)
	}
}

// TestGroupedTaskSprintsReadIssuesOneStatement is the database-level gate for
// Acceptance Criterion 92: the sprint of N tasks is resolved with exactly ONE
// statement, whatever N and whatever the number of distinct sprints involved, and
// with none at all for an empty id set.
//
// The per-task alternative the SPEC forbids is measured on the same instrument,
// so the single-statement assertion is falsifiable rather than a count that would
// read 1 however the read behaved.
func TestGroupedTaskSprintsReadIssuesOneStatement(t *testing.T) {
	db, counter, cleanup := setupCountingDB(t)
	defer cleanup()

	// Twelve tasks spread over three sprints, so the read that resolves them all
	// crosses several sprints rather than one.
	sprintIDs := []int{
		newTestSprintWithCap(t, db, "Authentication hardening", 0),
		newTestSprintWithCap(t, db, "Settlement reconciliation", 0),
		newTestSprintWithCap(t, db, "Observability rollout", 0),
	}
	taskIDs := make([]int, 0, 12)
	for i := range 12 {
		id := newTestTask(t, db, "Reconcile settlement window "+string(rune('A'+i)))
		addTasksToSprint(t, db, sprintIDs[i%len(sprintIDs)], id)
		taskIDs = append(taskIDs, id)
	}
	if counter.count() == 0 {
		t.Fatal("the statement counter did not observe the seeding writes, so it is not counting")
	}

	for _, tasks := range []int{1, 3, 12} {
		ids := taskIDs[:tasks]

		counter.reset()
		sprints, err := db.GetSprintsByTasks(testContext(), ids)
		if err != nil {
			t.Fatalf("GetSprintsByTasks(%d tasks): %v", tasks, err)
		}
		if got := counter.count(); got != 1 {
			t.Errorf("the grouped read of %d tasks issued %d statements, want exactly 1", tasks, got)
		}
		if len(sprints) != tasks {
			t.Errorf("the grouped read of %d tasks returned %d entries, want %d", tasks, len(sprints), tasks)
		}
		for i, id := range ids {
			if got := sprints[id]; got.ID != sprintIDs[i%len(sprintIDs)] {
				t.Errorf("sprints[%d] = %+v, want sprint #%d", id, got, sprintIDs[i%len(sprintIDs)])
			}
		}

		// The alternative the SPEC forbids, on the same instrument: one statement
		// per task. This is what makes the assertion above falsifiable.
		counter.reset()
		for _, id := range ids {
			if _, err := db.GetSprintsByTasks(testContext(), []int{id}); err != nil {
				t.Fatalf("per-task control read of task %d: %v", id, err)
			}
		}
		if got := counter.count(); got != tasks {
			t.Errorf("the per-task loop over %d tasks issued %d statements, want %d; "+
				"the instrument does not track statements one-for-one", tasks, got, tasks)
		}
	}

	// An empty id set issues no statement at all: the read is skipped outright
	// rather than sent with an empty IN list.
	counter.reset()
	sprints, err := db.GetSprintsByTasks(testContext(), nil)
	if err != nil {
		t.Fatalf("GetSprintsByTasks with no ids: %v", err)
	}
	if got := counter.count(); got != 0 {
		t.Errorf("the grouped read of an empty id set issued %d statements, want 0", got)
	}
	if len(sprints) != 0 {
		t.Errorf("the grouped read of an empty id set returned %d entries, want 0", len(sprints))
	}
}
