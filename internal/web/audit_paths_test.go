package web

import (
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// entry builds one audit entry the way the audit table stores it: an operation,
// the entity type it concerns, that entity's id, and when it was performed.
func entry(id int, op models.AuditOperation, entityType models.EntityType, entityID int, at string) models.AuditEntry {
	return models.AuditEntry{
		ID:          id,
		Operation:   string(op),
		EntityType:  string(entityType),
		EntityID:    entityID,
		PerformedAt: at,
	}
}

// TestBuildAuditHistory_AssignsEveryEntryToItsPath covers the four cases of
// SPEC/WEB.md § Audit History Paths, rule 2, on one page of entries: a task that
// belongs to a sprint, a task that belongs to none, a sprint's own entries, and
// entries spanning more than one sprint.
func TestBuildAuditHistory_AssignsEveryEntryToItsPath(t *testing.T) {
	entries := []models.AuditEntry{
		entry(9, models.OpSprintClose, models.EntitySprint, 12, "2026-03-04T10:00:00.000Z"),
		entry(8, models.OpTaskStatusChange, models.EntityTask, 101, "2026-03-04T09:00:00.000Z"),
		entry(7, models.OpTaskCreate, models.EntityTask, 250, "2026-03-03T18:00:00.000Z"),
		entry(6, models.OpSprintAddTask, models.EntitySprint, 13, "2026-03-03T17:00:00.000Z"),
		entry(5, models.OpTaskPriorityChange, models.EntityTask, 204, "2026-03-03T16:00:00.000Z"),
		entry(4, models.OpSprintCreate, models.EntitySprint, 13, "2026-03-03T15:00:00.000Z"),
	}
	// Task 101 is in sprint 12, task 204 in sprint 13; task 250 is in none, so
	// it is absent from the map exactly as GetSprintsByTasks would leave it.
	sprintOf := map[int]db.SprintRef{
		101: {ID: 12, Title: "Harden the read-only guard rail"},
		204: {ID: 13, Title: "Web interface improvements"},
	}

	views := buildAuditHistory(entries, sprintOf)

	if len(views) != len(entries) {
		t.Fatalf("built %d views for %d entries; every entry must appear exactly once", len(views), len(entries))
	}

	want := []struct {
		path   string
		label  string
		opens  bool
		merges bool
	}{
		{"sprint/12", "Sprint #12", false, true},  // SPRINT_CLOSE merges back
		{"sprint/12", "Sprint #12", false, false}, // task 101 rides its sprint's path
		{"backlog", "Backlog", false, false},      // task 250 belongs to no sprint
		{"sprint/13", "Sprint #13", false, false}, // the sprint's own entry
		{"sprint/13", "Sprint #13", false, false}, // task 204 rides sprint 13
		{"sprint/13", "Sprint #13", true, false},  // SPRINT_CREATE opens the path
	}
	for i, w := range want {
		got := views[i]
		if got.Path != w.path || got.PathLabel != w.label {
			t.Errorf("entry #%d (%s on %s %d): path = %q/%q, want %q/%q",
				got.ID, got.Operation, got.EntityType, got.EntityID, got.Path, got.PathLabel, w.path, w.label)
		}
		if got.Opens != w.opens || got.Merges != w.merges {
			t.Errorf("entry #%d (%s): opens/merges = %v/%v, want %v/%v",
				got.ID, got.Operation, got.Opens, got.Merges, w.opens, w.merges)
		}
		// The entry's own fields survive the assignment untouched: the view
		// embeds the entry rather than copying selected fields.
		if got.ID != entries[i].ID || got.PerformedAt != entries[i].PerformedAt {
			t.Errorf("view %d does not carry its entry: got #%d at %s, want #%d at %s",
				i, got.ID, got.PerformedAt, entries[i].ID, entries[i].PerformedAt)
		}
	}
}

// TestBuildAuditHistory_PreservesOrder asserts the model does not reorder the
// page. The caller reads entries performed_at descending and the table renders
// that order; reversing for the tree is the client's job, and doing it here
// would silently change what the table shows (SPEC/WEB.md § Audit History Tree,
// rule 2).
func TestBuildAuditHistory_PreservesOrder(t *testing.T) {
	entries := []models.AuditEntry{
		entry(3, models.OpTaskCreate, models.EntityTask, 1, "2026-03-03T12:00:00.000Z"),
		entry(2, models.OpTaskCreate, models.EntityTask, 2, "2026-03-02T12:00:00.000Z"),
		entry(1, models.OpTaskCreate, models.EntityTask, 3, "2026-03-01T12:00:00.000Z"),
	}

	views := buildAuditHistory(entries, nil)

	for i, view := range views {
		if view.ID != entries[i].ID {
			t.Fatalf("position %d holds entry #%d, want #%d: the model reordered the page", i, view.ID, entries[i].ID)
		}
	}
}

// TestBuildAuditHistory_SprintLifecycleShapesThePath asserts which operations
// open and close a path, and that no other operation claims to (SPEC/WEB.md
// § Audit History Paths, rule 3). A reopened sprint branches from the roadmap
// line a second time, so REOPEN opens rather than merges.
func TestBuildAuditHistory_SprintLifecycleShapesThePath(t *testing.T) {
	cases := []struct {
		op     models.AuditOperation
		opens  bool
		merges bool
	}{
		{models.OpSprintCreate, true, false},
		{models.OpSprintReopen, true, false},
		{models.OpSprintClose, false, true},
		{models.OpSprintStart, false, false},
		{models.OpSprintUpdate, false, false},
		{models.OpSprintAddTask, false, false},
		{models.OpSprintRemoveTask, false, false},
		{models.OpSprintMoveTask, false, false},
		{models.OpSprintDelete, false, false},
	}
	for _, c := range cases {
		views := buildAuditHistory(
			[]models.AuditEntry{entry(1, c.op, models.EntitySprint, 7, "2026-03-01T12:00:00.000Z")}, nil)
		got := views[0]
		if got.Opens != c.opens || got.Merges != c.merges {
			t.Errorf("%s: opens/merges = %v/%v, want %v/%v", c.op, got.Opens, got.Merges, c.opens, c.merges)
		}
		if got.Path != "sprint/7" {
			t.Errorf("%s: path = %q, want %q", c.op, got.Path, "sprint/7")
		}
	}
}

// TestBuildAuditHistory_TaskOperationsNeverShapeThePath asserts that a task
// operation is always an ordinary point: only a sprint's own lifecycle branches
// or merges a path. A task status change reaching COMPLETED looks like a merge
// and is not one — the audit does not even record the status it changed to.
func TestBuildAuditHistory_TaskOperationsNeverShapeThePath(t *testing.T) {
	for _, op := range []models.AuditOperation{
		models.OpTaskCreate, models.OpTaskStatusChange, models.OpTaskReopen,
		models.OpTaskDelete, models.OpTaskAddDep, models.OpTaskCommentCreate,
	} {
		views := buildAuditHistory(
			[]models.AuditEntry{entry(1, op, models.EntityTask, 42, "2026-03-01T12:00:00.000Z")},
			map[int]db.SprintRef{42: {ID: 5}})
		if views[0].Opens || views[0].Merges {
			t.Errorf("%s: a task operation claims to open or merge a path", op)
		}
		if views[0].Path != "sprint/5" {
			t.Errorf("%s: path = %q, want %q", op, views[0].Path, "sprint/5")
		}
	}
}

// TestBuildAuditHistory_DeletedTaskFallsToBacklog documents the boundary the
// membership lookup leaves: a task that no longer exists is absent from
// sprint_tasks exactly as a backlog task is, so the two are indistinguishable
// from the audit alone and both land on the backlog path (SPEC/WEB.md § Audit
// History Paths, rule 1).
func TestBuildAuditHistory_DeletedTaskFallsToBacklog(t *testing.T) {
	views := buildAuditHistory(
		[]models.AuditEntry{entry(1, models.OpTaskDelete, models.EntityTask, 999, "2026-03-01T12:00:00.000Z")},
		map[int]db.SprintRef{})

	if views[0].Path != auditPathBacklog {
		t.Errorf("a deleted task's entry landed on %q, want %q", views[0].Path, auditPathBacklog)
	}
}

// TestAuditTaskIDs_AsksAboutExactlyThePageTasks asserts the membership lookup is
// given the tasks the page shows and no others: sprint entries contribute no id,
// a task named twice is asked about once, and the order is first appearance so
// the query text stays stable across identical requests.
func TestAuditTaskIDs_AsksAboutExactlyThePageTasks(t *testing.T) {
	entries := []models.AuditEntry{
		entry(5, models.OpTaskCreate, models.EntityTask, 30, "2026-03-05T12:00:00.000Z"),
		entry(4, models.OpSprintCreate, models.EntitySprint, 12, "2026-03-04T12:00:00.000Z"),
		entry(3, models.OpTaskStatusChange, models.EntityTask, 10, "2026-03-03T12:00:00.000Z"),
		entry(2, models.OpTaskStatusChange, models.EntityTask, 30, "2026-03-02T12:00:00.000Z"),
		entry(1, models.OpSprintClose, models.EntitySprint, 11, "2026-03-01T12:00:00.000Z"),
	}

	got := auditTaskIDs(entries)

	want := []int{30, 10}
	if len(got) != len(want) {
		t.Fatalf("asked about %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("asked about %v, want %v (order is first appearance)", got, want)
		}
	}

	// A page of nothing but sprint entries asks about no task at all, which the
	// grouped lookup answers without issuing a statement.
	if ids := auditTaskIDs([]models.AuditEntry{
		entry(1, models.OpSprintStart, models.EntitySprint, 3, "2026-03-01T12:00:00.000Z"),
	}); len(ids) != 0 {
		t.Errorf("a page with no task entry asked about %v, want no id", ids)
	}
}
