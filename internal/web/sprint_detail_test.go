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

// TestSprintDetail_FullBlockOnlyOnSprintPage asserts that the full sprint detail
// block — the exact summary line, the metadata datagrid (ID/Status/Capacity/
// Tasks/Created/Started/Closed), and the full member-tasks table headers
// (ID/Title/Status/Type/Priority/Severity) in execution order — is rendered ONLY
// on the single Roadmap Sprint Page, and that the Actual tab of the roadmap
// sprints page does NOT render it for the OPEN sprint: there the OPEN sprint is
// shown through the shared sprint-card partial, with no summary line, no
// datagrid, no member-tasks table, and no per-task modal (SPEC/WEB.md § Shared
// Sprint-Card Partial, § Sprint Detail Sub-Template; Acceptance Criteria
// 8/12/38/39).
func TestSprintDetail_FullBlockOnlyOnSprintPage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-shared-detail")
	mux := buildMux()

	// The OPEN sprint's two tasks are both in SPRINT status (added via
	// AddTasksToSprint), so the completion summary is deterministic:
	// P=2 (both SPRINT), A=0, C=0, T=2, Pct=0.
	wantLine := "0% - P:2 A:0 C:0 - T:2"

	sprintsPage := servePage(t, mux, "/roadmaps/"+f.name)
	sprintPage := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.openID))

	// Slice the Actual pane out of the sprints page so the absence assertions
	// target the OPEN sprint's card, not some other tab.
	current := paneSlice(t, sprintsPage, `<div id="tab-current"`)

	datagridTitles := []string{
		">ID<", ">Status<", ">Capacity<", ">Tasks<", ">Created<", ">Started<", ">Closed<",
	}
	tableHeaders := []string{
		"<th>ID</th>", "<th>Title</th>", "<th>Status</th>",
		"<th>Type</th>", "<th>Priority</th>", "<th>Severity</th>",
	}

	// The single sprint page MUST carry the full detail block.
	if !strings.Contains(sprintPage, wantLine) {
		t.Errorf("single sprint page: missing exact summary line %q", wantLine)
	}
	if !strings.Contains(sprintPage, `data-role="sprint-summary"`) {
		t.Errorf("single sprint page: missing the sprint summary line element")
	}
	for _, m := range datagridTitles {
		if !strings.Contains(sprintPage, m) {
			t.Errorf("single sprint page: detail block missing datagrid title %q", m)
		}
	}
	for _, h := range tableHeaders {
		if !strings.Contains(sprintPage, h) {
			t.Errorf("single sprint page: detail block missing task table header %q", h)
		}
	}
	if !strings.Contains(sprintPage, "Build the read-only sprint page route and template") {
		t.Errorf("single sprint page: detail block missing the OPEN sprint's member task")
	}
	if !strings.Contains(sprintPage, `data-bs-target="#task-modal-`+itoa(f.openTaskID)+`"`) {
		t.Errorf("single sprint page: member task row not wired to its task modal")
	}

	// The Actual tab MUST NOT carry any part of the full detail block.
	if strings.Contains(current, wantLine) || strings.Contains(current, `data-role="sprint-summary"`) {
		t.Errorf("Actual tab must not render the sprint status summary line")
	}
	for _, m := range datagridTitles {
		if strings.Contains(current, m) {
			t.Errorf("Actual tab must not render the metadata datagrid title %q", m)
		}
	}
	for _, h := range tableHeaders {
		if strings.Contains(current, h) {
			t.Errorf("Actual tab must not render the member-tasks table header %q", h)
		}
	}
	if strings.Contains(current, `data-bs-target="#task-modal-`) {
		t.Errorf("Actual tab must not render a per-task modal trigger")
	}
	// The OPEN sprint IS present on the Actual tab, as a card linking to its page.
	if !strings.Contains(current, "/sprints/"+itoa(f.openID)) {
		t.Errorf("Actual tab missing the OPEN sprint card link")
	}
	if !strings.Contains(current, "2 task(s)") {
		t.Errorf("Actual tab card does not show the OPEN sprint's task count")
	}
}

// TestSprintsPage_SharedCardAcrossAllTabs asserts that all three tabs of the
// roadmap sprints page render their sprints through the SINGLE shared
// sprint-card partial, so the card markup is identical across Próximos, Actual,
// and Concluídos. It checks the partial's distinctive markup (the
// "card card-sm card-link" link wrapping a "Sprint #<ID>" header and a
// "task(s)" footer) appears in every populated pane, and that the total number
// of cards equals the total number of sprints — proving no tab uses a divergent
// layout and the OPEN sprint is a card, not an expanded block (SPEC/WEB.md
// § Shared Sprint-Card Partial; Acceptance Criteria 8/12/13/38).
func TestSprintsPage_SharedCardAcrossAllTabs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-shared-card")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name)

	const cardMarker = `class="card card-sm card-link text-reset"`

	// The fixture seeds 5 sprints (2 PENDING, 1 OPEN, 2 CLOSED); every one must be
	// rendered as exactly one shared card.
	if got := strings.Count(body, cardMarker); got != 5 {
		t.Errorf("expected 5 shared sprint cards (one per sprint), found %d", got)
	}

	// Each populated pane uses the shared card markup, proving no tab diverges.
	panes := map[string]string{
		"Próximos":   paneSlice(t, body, `<div id="tab-upcoming"`),
		"Actual":     paneSlice(t, body, `<div id="tab-current"`),
		"Concluídos": paneSlice(t, body, `<div id="tab-closed"`),
	}
	for label, pane := range panes {
		if !strings.Contains(pane, cardMarker) {
			t.Errorf("%s tab does not render the shared sprint-card markup", label)
		}
		if !strings.Contains(pane, "task(s)") {
			t.Errorf("%s tab card does not show a task-count footer", label)
		}
	}

	// Every card carries a status badge in its header (Acceptance Criterion 13).
	if got := strings.Count(body, `<div class="card-actions"><span class="badge bg-blue-lt">`); got != 5 {
		t.Errorf("expected 5 card status badges (one per card), found %d", got)
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
