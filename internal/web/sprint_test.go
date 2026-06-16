package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// seededTask builds a valid task with the long free-text fields populated, so
// the task detail modal has functional/technical/acceptance text to render. The
// title is unique per id so an assertion can target one task's modal.
func seededTask(now, title string) *models.Task {
	return &models.Task{
		Priority:               4,
		Severity:               3,
		Status:                 models.StatusBacklog,
		Title:                  title,
		FunctionalRequirements: "Operators can inspect a sprint and its task list from the browser",
		TechnicalRequirements:  "Render the sprint page server-side from project.db, read-only",
		AcceptanceCriteria:     "The sprint page lists every member task in execution order",
		CreatedAt:              now,
	}
}

// seedSprintFixture creates a roadmap with three sprints — one PENDING, one
// OPEN, two CLOSED — and member tasks, so the sprints-page classification and
// the sprint page can be exercised against genuine SQLite data. It returns the
// roadmap name and the ids it created. The two CLOSED sprints get distinct
// closed_at timestamps (set directly via the embedded *sql.DB) so the
// closed_at-descending order in the Concluídos tab is deterministic.
//
// Sprint creation order (ascending id): PENDING(1), CLOSED-older, OPEN, then
// CLOSED-newer. Sprint ids therefore do NOT match closed_at order, so the
// Concluídos assertion proves the sort is by closed_at, not by id.
type sprintFixture struct {
	name        string
	pendingID   int
	openID      int
	closedOlder int
	closedNewer int
	openTaskID  int
	pendTaskID  int
	pendTaskID2 int
	openTaskID2 int
}

func seedSprintFixture(t *testing.T, name string) sprintFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	mkTask := func(title string) int {
		id, terr := database.CreateTask(ctx, seededTask(now, title))
		if terr != nil {
			t.Fatalf("creating task %q: %v", title, terr)
		}
		return id
	}
	mkSprint := func(desc string) int {
		id, serr := database.CreateSprint(ctx, &models.Sprint{
			Status:      models.SprintPending,
			Title:       desc,
			Description: desc,
			CreatedAt:   now,
		})
		if serr != nil {
			t.Fatalf("creating sprint %q: %v", desc, serr)
		}
		return id
	}

	f := sprintFixture{name: name}

	// 1) PENDING sprint (stays PENDING) with two member tasks (ascending ids).
	f.pendingID = mkSprint("Plan the read-only web sprint presentation")
	f.pendTaskID = mkTask("Classify sprints into Proximos, Actual, Concluidos tabs")
	f.pendTaskID2 = mkTask("Order Proximos by ascending sprint id")
	if aerr := database.AddTasksToSprint(ctx, f.pendingID, []int{f.pendTaskID, f.pendTaskID2}); aerr != nil {
		t.Fatalf("adding tasks to pending sprint: %v", aerr)
	}

	// 2) An OLDER closed sprint (created before the OPEN sprint, so it has a
	//    lower id than the newer closed sprint).
	f.closedOlder = mkSprint("Ship the initial knowledge-graph viewer")
	setClosed(t, database, f.closedOlder, "2026-01-10T09:00:00Z")

	// 3) OPEN sprint with two member tasks (the Actual tab shows their status).
	f.openID = mkSprint("Deliver the sprint detail page and task modal")
	f.openTaskID = mkTask("Build the read-only sprint page route and template")
	f.openTaskID2 = mkTask("Render one task detail modal per shown task")
	if aerr := database.AddTasksToSprint(ctx, f.openID, []int{f.openTaskID, f.openTaskID2}); aerr != nil {
		t.Fatalf("adding tasks to open sprint: %v", aerr)
	}
	if serr := database.UpdateSprintStatus(ctx, f.openID, models.SprintOpen); serr != nil {
		t.Fatalf("opening sprint: %v", serr)
	}

	// 4) A NEWER closed sprint (higher id, but a LATER closed_at than the older
	//    one), so closed_at-descending ranks it first while id-descending would
	//    also — to break that ambiguity, the older sprint has a LOWER id AND an
	//    EARLIER closed_at, and we additionally verify a closed sprint with NO
	//    closed_at sorts last below.
	f.closedNewer = mkSprint("Harden the web read path against malformed input")
	setClosed(t, database, f.closedNewer, "2026-05-20T18:30:00Z")

	return f
}

// setClosed marks a sprint CLOSED with a specific closed_at timestamp, writing
// directly through the embedded *sql.DB so the test controls the value (the
// db.UpdateSprintStatus helper always stamps "now").
func setClosed(t *testing.T, database *db.DB, id int, closedAt string) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(),
		"UPDATE sprints SET status = ?, closed_at = ? WHERE id = ?",
		string(models.SprintClosed), closedAt, id,
	); err != nil {
		t.Fatalf("setting sprint %d closed_at: %v", id, err)
	}
}

// TestSprints_SprintTabsLabelsAndDefault asserts the sprints landing page
// renders the three sprint tabs with the exact Portuguese labels in
// left-to-right order — Proximos, Actual, Concluidos — and that Actual is the
// active/default tab on load (SPEC/WEB.md § Roadmap Sprints Page, Acceptance
// Criterion 11).
func TestSprints_SprintTabsLabelsAndDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-sprint-tabs")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name)

	// Labels appear in the required left-to-right order.
	assertOrdered(t, "sprint-tab-labels", body, []string{
		">Próximos ", ">Actual ", ">Concluídos ",
	})

	// The Actual tab link carries the active class and aria-selected="true",
	// and it is the only tab that does.
	activeTab := `<a href="#tab-current" class="nav-link active" data-bs-toggle="tab" role="tab" aria-selected="true">Actual`
	if !strings.Contains(body, activeTab) {
		t.Errorf("Actual tab is not the active/default tab; missing %q", activeTab)
	}
	if strings.Count(body, `aria-selected="true"`) != 1 {
		t.Errorf("exactly one tab must be aria-selected=\"true\" (the Actual default)")
	}
	// The Actual tab pane is the shown/active pane.
	if !strings.Contains(body, `<div id="tab-current" class="tab-pane active show"`) {
		t.Errorf("the Actual tab pane is not the active/shown pane by default")
	}
}

// TestSprints_SprintClassificationAndLinks asserts each sprint is classified
// into the correct tab by status and that every sprint links to its page. The
// PENDING sprint must be under Próximos, the OPEN sprint (with its tasks'
// statuses) under Actual, and both CLOSED sprints under Concluídos in
// closed_at-descending order (SPEC/WEB.md § Roadmap Sprints Page, Acceptance
// Criteria 12/13).
func TestSprints_SprintClassificationAndLinks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-sprint-classify")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name)

	// Every sprint links to its own page.
	for _, id := range []int{f.pendingID, f.openID, f.closedOlder, f.closedNewer} {
		link := "/roadmaps/" + f.name + "/sprints/" + itoa(id)
		if !strings.Contains(body, link) {
			t.Errorf("detail page missing sprint link %q", link)
		}
	}

	// Slice the three tab panes apart so a sprint link is asserted in the
	// RIGHT pane, not merely somewhere on the page.
	current := paneSlice(t, body, `<div id="tab-current"`)
	upcoming := paneSlice(t, body, `<div id="tab-upcoming"`)
	closed := paneSlice(t, body, `<div id="tab-closed"`)

	// PENDING -> Próximos.
	if !strings.Contains(upcoming, "/sprints/"+itoa(f.pendingID)) {
		t.Errorf("PENDING sprint #%d not under the Próximos tab", f.pendingID)
	}
	// OPEN -> Actual, and the Actual pane shows the OPEN sprint's task statuses.
	if !strings.Contains(current, "/sprints/"+itoa(f.openID)) {
		t.Errorf("OPEN sprint #%d not under the Actual tab", f.openID)
	}
	if !strings.Contains(current, "Build the read-only sprint page route and template") {
		t.Errorf("Actual tab does not show the OPEN sprint's task title")
	}
	if !strings.Contains(current, `<span class="badge bg-blue-lt">SPRINT</span>`) {
		t.Errorf("Actual tab does not show the OPEN sprint's task status badge")
	}

	// CLOSED -> Concluídos, ordered closed_at descending: the NEWER closed
	// sprint (2026-05) appears before the OLDER one (2026-01), even though the
	// newer one has the higher id (so this proves the closed_at sort).
	idxNewer := strings.Index(closed, "/sprints/"+itoa(f.closedNewer))
	idxOlder := strings.Index(closed, "/sprints/"+itoa(f.closedOlder))
	if idxNewer < 0 || idxOlder < 0 {
		t.Fatalf("both CLOSED sprints must appear under Concluídos; newer=%d older=%d", idxNewer, idxOlder)
	}
	if idxNewer > idxOlder {
		t.Errorf("Concluídos order wrong: newer-closed sprint #%d must precede older-closed sprint #%d", f.closedNewer, f.closedOlder)
	}
}

// TestClassifySprints_OrderingRules unit-tests the classification and ordering
// comparator directly with crafted sprints, covering the closed_at-descending
// order, the nil/empty closed_at-sorts-last rule, and the descending-id
// tiebreak — the parts that are awkward to force through real data
// (SPEC/WEB.md § Roadmap Detail Page; Acceptance Criterion 11).
func TestClassifySprints_OrderingRules(t *testing.T) {
	at := func(s string) *string { return &s }

	views := []sprintView{
		{Sprint: models.Sprint{ID: 5, Status: models.SprintPending}},
		{Sprint: models.Sprint{ID: 2, Status: models.SprintPending}},
		{Sprint: models.Sprint{ID: 9, Status: models.SprintOpen}},
		{Sprint: models.Sprint{ID: 3, Status: models.SprintOpen}},
		{Sprint: models.Sprint{ID: 7, Status: models.SprintClosed, ClosedAt: at("2026-03-01T00:00:00Z")}},
		{Sprint: models.Sprint{ID: 1, Status: models.SprintClosed, ClosedAt: at("2026-06-01T00:00:00Z")}},
		{Sprint: models.Sprint{ID: 8, Status: models.SprintClosed, ClosedAt: nil}},                        // no closed_at -> last
		{Sprint: models.Sprint{ID: 4, Status: models.SprintClosed, ClosedAt: at("")}},                     // empty closed_at -> last
		{Sprint: models.Sprint{ID: 6, Status: models.SprintClosed, ClosedAt: at("2026-03-01T00:00:00Z")}}, // tie with #7
	}

	up, cur, cl := classifySprints(views)

	assertIDs := func(label string, got []sprintView, want []int) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: got %d sprints, want %d", label, len(got), len(want))
		}
		for i := range want {
			if got[i].Sprint.ID != want[i] {
				t.Errorf("%s: position %d = sprint #%d, want #%d (full order %v)",
					label, i, got[i].Sprint.ID, want[i], idsOf(got))
			}
		}
	}

	// Próximos: ascending id.
	assertIDs("Próximos", up, []int{2, 5})
	// Actual: ascending id.
	assertIDs("Actual", cur, []int{3, 9})
	// Concluídos: closed_at descending; #1 (2026-06) first, then the
	// 2026-03 tie (#7 and #6, descending id => #7 before #6), then the two
	// with no usable closed_at last, descending id => #8 before #4.
	assertIDs("Concluídos", cl, []int{1, 7, 6, 8, 4})
}

// TestSprintPage_HappyPath drives handleSprint against a sprint of an existing
// roadmap: 200 HTML showing all sprint fields and the member-task list, with
// every task row clickable to a modal and no edit affordance (SPEC/WEB.md
// § Roadmap Sprint Page; Acceptance Criterion 13).
func TestSprintPage_HappyPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-sprint-page")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.openID))

	// All sprint detail fields are present.
	for _, field := range []string{"Sprint #" + itoa(f.openID), "Status", "Capacity", "Tasks", "Created", "Started", "Closed"} {
		if !strings.Contains(body, field) {
			t.Errorf("sprint page missing field %q", field)
		}
	}
	if !strings.Contains(body, "Deliver the sprint detail page and task modal") {
		t.Errorf("sprint page missing the sprint description")
	}
	// Member tasks listed, each row clickable to a modal.
	if !strings.Contains(body, "Build the read-only sprint page route and template") {
		t.Errorf("sprint page does not list its member task")
	}
	if !strings.Contains(body, `data-bs-target="#task-modal-`+itoa(f.openTaskID)+`"`) {
		t.Errorf("sprint page task row is not wired to its task modal")
	}
	// Read-only: no form, no submit, no edit control.
	low := strings.ToLower(body)
	if strings.Contains(low, "<form") || strings.Contains(low, "<input") || strings.Contains(low, "type=\"submit\"") {
		t.Errorf("sprint page must be read-only: no form/input/submit allowed")
	}
}

// TestSprintPage_TaskOrder asserts the sprint page lists tasks in sprint_tasks
// position order (the planned in-sprint execution order), not by id or status
// (SPEC/WEB.md § Roadmap Sprint Page; Acceptance Criterion 13). The OPEN sprint
// was seeded with openTaskID added before openTaskID2.
func TestSprintPage_TaskOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-sprint-order")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.openID))

	first := strings.Index(body, "Build the read-only sprint page route and template")
	second := strings.Index(body, "Render one task detail modal per shown task")
	if first < 0 || second < 0 {
		t.Fatalf("sprint page is missing one of its member tasks (first=%d second=%d)", first, second)
	}
	if first > second {
		t.Errorf("sprint page tasks out of execution order: task #%d must precede task #%d", f.openTaskID, f.openTaskID2)
	}
}

// TestSprintPage_NotFoundCases asserts the sprint route's 404 rules: a
// non-integer id, and a syntactically valid but nonexistent id, both return
// 404; and a non-read method returns 405 (SPEC/WEB.md § Routes and Pages,
// path-parameter rule 3; Acceptance Criterion 13).
func TestSprintPage_NotFoundCases(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-sprint-404")
	mux := buildMux()

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"non-integer id", http.MethodGet, "/roadmaps/" + f.name + "/sprints/abc", http.StatusNotFound},
		{"valid but nonexistent id", http.MethodGet, "/roadmaps/" + f.name + "/sprints/999999", http.StatusNotFound},
		{"invalid roadmap name", http.MethodGet, "/roadmaps/NotValid/sprints/1", http.StatusNotFound},
		{"nonexistent roadmap", http.MethodGet, "/roadmaps/no-such-roadmap/sprints/1", http.StatusNotFound},
		{"post is 405", http.MethodPost, "/roadmaps/" + f.name + "/sprints/" + itoa(f.openID), http.StatusMethodNotAllowed},
		{"delete is 405", http.MethodDelete, "/roadmaps/" + f.name + "/sprints/" + itoa(f.openID), http.StatusMethodNotAllowed},
		{"negative id", http.MethodGet, "/roadmaps/" + f.name + "/sprints/-3", http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("%s %s: status = %d, want %d", tc.method, tc.path, rec.Code, tc.wantStatus)
			}
		})
	}
}

// TestTaskModal_WiringAndContent asserts the read-only task detail modal
// mechanism on every page that shows clickable tasks: the tasks page, the
// sprints landing page's Actual tab, and the sprint page. Each clickable task
// is wired with data-bs-toggle="modal" to a matching modal element, the modal
// shows the long free-text fields, and it contains no form/input/submit. The
// asserted task (f.openTaskID) is a member of the OPEN sprint, so its modal is
// rendered on the sprints page's Actual tab and on the sprint page, and it also
// appears in the full task table on the tasks page (SPEC/WEB.md § Task Detail
// Modal; Acceptance Criterion 15).
func TestTaskModal_WiringAndContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-task-modal")
	mux := buildMux()

	for _, path := range []string{
		"/roadmaps/" + f.name + "/tasks",                     // tasks page (full task table)
		"/roadmaps/" + f.name,                                // sprints landing page (Actual tab)
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.openID), // sprint page
	} {
		body := servePage(t, mux, path)

		// A clickable control is wired to a modal via Bootstrap data attributes.
		if !strings.Contains(body, `data-bs-toggle="modal"`) {
			t.Errorf("page %s has no modal-toggling control", path)
		}
		target := `data-bs-target="#task-modal-` + itoa(f.openTaskID) + `"`
		if !strings.Contains(body, target) {
			t.Errorf("page %s missing modal trigger %q", path, target)
		}
		// The matching modal element exists.
		modal := `id="task-modal-` + itoa(f.openTaskID) + `"`
		if !strings.Contains(body, modal) {
			t.Errorf("page %s missing modal element %q", path, modal)
		}
		// The modal shows the long free-text fields.
		for _, section := range []string{
			"Functional requirements", "Technical requirements", "Acceptance criteria", "Completion summary",
			"Operators can inspect a sprint and its task list from the browser", // functional text
		} {
			if !strings.Contains(body, section) {
				t.Errorf("page %s task modal missing %q", path, section)
			}
		}
		// Read-only: the modal (and the page) carry no form/input/submit.
		low := strings.ToLower(body)
		if strings.Contains(low, "<form") || strings.Contains(low, "<input") || strings.Contains(low, "type=\"submit\"") {
			t.Errorf("page %s task modal must be read-only: no form/input/submit", path)
		}
	}
}

// paneSlice returns the substring of body starting at the opening marker of one
// tab pane and ending just before the next pane's opening <div id="tab-, so an
// assertion can confirm a sprint link lives in the EXPECTED pane. The sprints
// template renders the panes in the source order current, upcoming, closed.
func paneSlice(t *testing.T, body, marker string) string {
	t.Helper()
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("tab pane marker %q not found", marker)
	}
	rest := body[start+len(marker):]
	if next := strings.Index(rest, `<div id="tab-`); next >= 0 {
		return rest[:next]
	}
	return rest
}

// itoa is a readable alias for strconv.Itoa used throughout the assertions to
// build sprint/task ids into URL paths and DOM ids.
func itoa(n int) string { return strconv.Itoa(n) }

// idsOf extracts sprint ids from a slice of views for diagnostic output.
func idsOf(vs []sprintView) []int {
	out := make([]int, len(vs))
	for i := range vs {
		out[i] = vs[i].Sprint.ID
	}
	return out
}
