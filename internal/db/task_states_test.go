package db

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The tests in this file pin the lean reads of SPEC/IMPLEMENTATION.md, "Read only
// the columns the caller uses": each returns, for the columns it projects,
// exactly the rows and the order the full read it replaces returns, so a command
// switched from one to the other decides and reports the same.

// statesOfTasks projects full task rows onto the lean projection.
func statesOfTasks(tasks []models.Task) []TaskState {
	out := make([]TaskState, len(tasks))
	for i := range tasks {
		out[i] = TaskState{ID: tasks[i].ID, Status: tasks[i].Status, Severity: tasks[i].Severity}
	}
	return out
}

// seedStatesFixture creates tasks in every status and severity and a sprint
// holding most of them in a position order different from id order.
func seedStatesFixture(t *testing.T, database *DB) (ids []int, sprintID int) {
	t.Helper()
	ctx := testContext()
	for i := range 230 {
		ids = append(ids, mustSeedTask(t, database, &models.Task{
			Title:                  fmt.Sprintf("Audit the chargeback evidence of dispute %d", i+1),
			Status:                 models.StatusBacklog,
			Priority:               i % 10,
			Severity:               (i * 7) % 10,
			FunctionalRequirements: "Every disputed charge carries the evidence the network requires.",
			TechnicalRequirements:  "Attach the evidence bundle through the dispute API.",
			AcceptanceCriteria:     "No dispute is submitted without its evidence bundle.",
			CreatedAt:              "2026-06-01T09:00:00.000Z",
		}))
	}
	sprintID = newTestSprintWithCap(t, database, "Chargeback evidence", 0)
	members := slices.Clone(ids[:120])
	slices.Reverse(members)
	if err := database.AddTasksToSprint(ctx, sprintID, members); err != nil {
		t.Fatalf("adding members: %v", err)
	}
	statuses := []models.TaskStatus{models.StatusSprint, models.StatusDoing, models.StatusTesting, models.StatusCompleted, models.StatusBacklog}
	for i, id := range members {
		forceTaskLifecycle(t, database, []int{id}, statuses[i%len(statuses)])
	}
	return ids, sprintID
}

// TestGetTaskStatesMatchesGetTasks compares the lean batch read with GetTasks
// over an id set above one chunk, unsorted, with ids that do not exist.
func TestGetTaskStatesMatchesGetTasks(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := testContext()
	ids, _ := seedStatesFixture(t, database)

	asked := slices.Clone(ids)
	slices.Reverse(asked)
	asked = append(asked, 99991, 99992)

	full, err := database.GetTasks(ctx, asked)
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	lean, err := database.GetTaskStates(ctx, asked)
	if err != nil {
		t.Fatalf("GetTaskStates: %v", err)
	}
	if len(lean) != len(ids) {
		t.Fatalf("GetTaskStates returned %d tasks, want %d", len(lean), len(ids))
	}
	if want := statesOfTasks(full); !slices.Equal(lean, want) {
		t.Errorf("GetTaskStates differs from GetTasks:\n got %v\nwant %v", lean, want)
	}

	empty, err := database.GetTaskStates(ctx, nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("GetTaskStates(nil) = %v, %v; want an empty, non-nil slice", empty, err)
	}
}

// TestSprintTaskStatesMatchTheFullReads compares the lean sprint reads with the
// full rows, member for member and in order, and the active count with the
// active members.
func TestSprintTaskStatesMatchTheFullReads(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := testContext()
	_, sprintID := seedStatesFixture(t, database)

	full, err := database.GetSprintTasksFull(ctx, sprintID, nil, false)
	if err != nil {
		t.Fatalf("GetSprintTasksFull: %v", err)
	}
	lean, err := database.GetSprintTaskStates(ctx, sprintID)
	if err != nil {
		t.Fatalf("GetSprintTaskStates: %v", err)
	}
	if want := statesOfTasks(full); !slices.Equal(lean, want) {
		t.Errorf("GetSprintTaskStates differs from GetSprintTasksFull:\n got %v\nwant %v", lean, want)
	}

	var wantActive []TaskState
	for _, st := range statesOfTasks(full) {
		switch st.Status {
		case models.StatusSprint, models.StatusDoing, models.StatusTesting:
			wantActive = append(wantActive, st)
		}
	}
	active, err := database.GetActiveSprintTaskStates(ctx, sprintID)
	if err != nil {
		t.Fatalf("GetActiveSprintTaskStates: %v", err)
	}
	if len(wantActive) == 0 || !slices.Equal(active, wantActive) {
		t.Errorf("GetActiveSprintTaskStates:\n got %v\nwant %v", active, wantActive)
	}
	count, err := database.CountActiveSprintTasks(ctx, sprintID)
	if err != nil {
		t.Fatalf("CountActiveSprintTasks: %v", err)
	}
	if count != len(wantActive) {
		t.Errorf("CountActiveSprintTasks = %d, want %d", count, len(wantActive))
	}

	// A sprint with no member reads as empty, not as an error.
	emptySprint := newTestSprintWithCap(t, database, "Dispute tooling backlog", 0)
	if states, err := database.GetSprintTaskStates(ctx, emptySprint); err != nil || len(states) != 0 {
		t.Errorf("an empty sprint reads as %v, %v", states, err)
	}
}

// TestSprintTaskStatesDriveFromSprintTasks pins the plan of the two lean sprint
// reads: sprint_tasks is searched by sprint_id in position order, and each
// member task is looked up by its primary key, with no sort step.
func TestSprintTaskStatesDriveFromSprintTasks(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	seedIndexFixture(t, database)

	for name, query := range map[string]string{
		"all members":    sprintTaskStatesQuery,
		"active members": activeSprintTaskStatesQuery,
	} {
		plan := queryPlan(t, database, query, 1)
		if !strings.HasPrefix(plan, "SEARCH st USING INDEX idx_sprint_tasks_order (sprint_id=?) | SEARCH t USING INTEGER PRIMARY KEY") {
			t.Errorf("%s: the read is not driven from idx_sprint_tasks_order.\nplan: %s", name, plan)
		}
		if strings.Contains(plan, "TEMP B-TREE") {
			t.Errorf("%s: the read sorts in a temporary B-tree.\nplan: %s", name, plan)
		}
	}
}

// TestCheckSprintExistsFailsLikeGetSprint requires the existence check to
// succeed for a sprint GetSprint finds and to fail with GetSprint's own error
// for one it does not.
func TestCheckSprintExistsFailsLikeGetSprint(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := testContext()

	sprintID := newTestSprintWithCap(t, database, "Dispute evidence retention", 0)
	if err := database.CheckSprintExists(ctx, sprintID); err != nil {
		t.Errorf("CheckSprintExists(%d) = %v for an existing sprint", sprintID, err)
	}

	const missing = 4242
	_, want := database.GetSprint(ctx, missing)
	got := database.CheckSprintExists(ctx, missing)
	if want == nil || got == nil || got.Error() != want.Error() {
		t.Fatalf("CheckSprintExists(%d) = %v, want GetSprint's %v", missing, got, want)
	}
	if !errors.Is(got, utils.ErrNotFound) {
		t.Errorf("CheckSprintExists(%d) = %v, which does not wrap utils.ErrNotFound", missing, got)
	}
}
