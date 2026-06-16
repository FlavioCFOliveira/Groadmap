package web

import (
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// taskWith builds a Task carrying only the fields the completion summary reads.
// The title makes a rendered row identifiable; the status drives the bucket.
func taskWith(status models.TaskStatus, title string) models.Task {
	return models.Task{Status: status, Title: title}
}

// TestNewSprintCompletion_CountsAndLine asserts the precomputed completion
// summary derives the P/A/C/T counts and the rounded percentage straight from a
// sprint's loaded member tasks, and that Line renders the exact documented
// format `<pct>% - P:<p> A:<a> C:<c> - T:<t>` (SPEC/WEB.md § Shared Sprint
// Presentation Sub-Template, sprint status summary line; Acceptance Criterion
// 39).
func TestNewSprintCompletion_CountsAndLine(t *testing.T) {
	cases := []struct {
		name     string
		tasks    []models.Task
		want     sprintCompletion
		wantLine string
	}{
		{
			name:     "no tasks is 0%",
			tasks:    nil,
			want:     sprintCompletion{Pending: 0, InProgress: 0, Completed: 0, Total: 0, Pct: 0},
			wantLine: "0% - P:0 A:0 C:0 - T:0",
		},
		{
			name: "two thirds completed rounds to 67%",
			tasks: []models.Task{
				taskWith(models.StatusCompleted, "Define the read-only data flow"),
				taskWith(models.StatusCompleted, "Render the sprint detail sub-template"),
				taskWith(models.StatusBacklog, "Document the completion summary line"),
			},
			want:     sprintCompletion{Pending: 1, InProgress: 0, Completed: 2, Total: 3, Pct: 67},
			wantLine: "67% - P:1 A:0 C:2 - T:3",
		},
		{
			name: "mixed statuses across all buckets",
			tasks: []models.Task{
				taskWith(models.StatusBacklog, "Backlog item"),
				taskWith(models.StatusSprint, "Pulled into sprint"),
				taskWith(models.StatusDoing, "In development"),
				taskWith(models.StatusTesting, "Under test"),
				taskWith(models.StatusCompleted, "Shipped"),
			},
			want:     sprintCompletion{Pending: 2, InProgress: 2, Completed: 1, Total: 5, Pct: 20},
			wantLine: "20% - P:2 A:2 C:1 - T:5",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := newSprintCompletion(c.tasks)
			if got != c.want {
				t.Errorf("newSprintCompletion = %+v, want %+v", got, c.want)
			}
			if line := got.Line(); line != c.wantLine {
				t.Errorf("Line() = %q, want %q", line, c.wantLine)
			}
		})
	}
}

// TestSprintDetail_SharedBlockAndSummaryLine asserts that the Actual tab of the
// roadmap sprints page and the single Roadmap Sprint Page render the SAME shared
// sprint presentation sub-template for the OPEN sprint: both carry the exact
// summary line, the metadata datagrid (ID/Status/Capacity/Tasks/Created/
// Started/Closed), and the full member-tasks table headers
// (ID/Title/Status/Type/Priority/Severity) in execution order (SPEC/WEB.md
// § Shared Sprint Presentation Sub-Template; Acceptance Criteria 38/39).
func TestSprintDetail_SharedBlockAndSummaryLine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-shared-detail")
	mux := buildMux()

	// The OPEN sprint's two tasks are both in SPRINT status (added via
	// AddTasksToSprint), so the completion summary is deterministic:
	// P=2 (both SPRINT), A=0, C=0, T=2, Pct=0.
	wantLine := "0% - P:2 A:0 C:0 - T:2"

	sprintsPage := servePage(t, mux, "/roadmaps/"+f.name)
	sprintPage := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.openID))

	// Slice the Actual pane out of the sprints page so the assertions target the
	// OPEN sprint's block, not some other tab.
	current := paneSlice(t, sprintsPage, `<div id="tab-current"`)

	pages := map[string]string{
		"sprints page (Actual tab)": current,
		"single sprint page":        sprintPage,
	}

	// Markers that must appear in the shared block on BOTH call sites.
	datagridTitles := []string{
		">ID<", ">Status<", ">Capacity<", ">Tasks<", ">Created<", ">Started<", ">Closed<",
	}
	tableHeaders := []string{
		"<th>ID</th>", "<th>Title</th>", "<th>Status</th>",
		"<th>Type</th>", "<th>Priority</th>", "<th>Severity</th>",
	}

	for label, body := range pages {
		// Exact summary line in the documented format.
		if !strings.Contains(body, wantLine) {
			t.Errorf("%s: missing exact summary line %q", label, wantLine)
		}
		if !strings.Contains(body, `data-role="sprint-summary"`) {
			t.Errorf("%s: missing the sprint summary line element", label)
		}
		// The metadata datagrid.
		for _, m := range datagridTitles {
			if !strings.Contains(body, m) {
				t.Errorf("%s: shared detail block missing datagrid title %q", label, m)
			}
		}
		// The full member-tasks table headers (Type/Priority/Severity prove it is
		// the full table, not the old reduced Actual-tab card).
		for _, h := range tableHeaders {
			if !strings.Contains(body, h) {
				t.Errorf("%s: shared detail block missing task table header %q", label, h)
			}
		}
		// The OPEN sprint's member task is listed and clickable to its modal.
		if !strings.Contains(body, "Build the read-only sprint page route and template") {
			t.Errorf("%s: shared detail block missing the OPEN sprint's member task", label)
		}
		if !strings.Contains(body, `data-bs-target="#task-modal-`+itoa(f.openTaskID)+`"`) {
			t.Errorf("%s: member task row not wired to its task modal", label)
		}
	}
}

// TestSprintsPage_ClosedCardsShowTaskCount asserts every sprint card under the
// Concluídos tab shows the sprint's total task count (SPEC/WEB.md § Roadmap
// Sprints Page, Concluídos; Acceptance Criterion 40). The Próximos cards already
// show their task count, asserted indirectly by the existing fixture, but the
// closed cards previously showed only the closed_at date.
func TestSprintsPage_ClosedCardsShowTaskCount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-closed-counts")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name)
	closed := paneSlice(t, body, `<div id="tab-closed"`)

	// Both CLOSED sprints in the fixture have zero member tasks, so each card
	// must show "0 task(s)". The marker is unambiguous to the count display.
	if got := strings.Count(closed, "task(s)"); got != 2 {
		t.Errorf("Concluídos: found %d task-count displays, want 2 (one per closed card)", got)
	}
	if !strings.Contains(closed, "0 task(s)") {
		t.Errorf("Concluídos: a closed sprint card does not show its task count")
	}
}

// TestSprintsPage_UpcomingCardsShowTaskCount asserts every sprint card under the
// Próximos tab shows the sprint's total task count (SPEC/WEB.md § Roadmap
// Sprints Page, Próximos; Acceptance Criterion 40). The fixture's first PENDING
// sprint has two member tasks; the second has none.
func TestSprintsPage_UpcomingCardsShowTaskCount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-upcoming-counts")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name)
	upcoming := paneSlice(t, body, `<div id="tab-upcoming"`)

	if got := strings.Count(upcoming, "task(s)"); got != 2 {
		t.Errorf("Próximos: found %d task-count displays, want 2 (one per pending card)", got)
	}
	// pendingID has two member tasks; pendingID2 has none.
	if !strings.Contains(upcoming, "2 task(s)") {
		t.Errorf("Próximos: the two-task pending sprint card does not show '2 task(s)'")
	}
	if !strings.Contains(upcoming, "0 task(s)") {
		t.Errorf("Próximos: the empty pending sprint card does not show '0 task(s)'")
	}
}
