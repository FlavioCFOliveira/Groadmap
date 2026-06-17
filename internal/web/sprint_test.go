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

// seedSprintFixture creates a roadmap with sprints — two PENDING, one OPEN, two
// CLOSED — and member tasks, so the sprints-page classification, ordering, and
// the sprint page can be exercised against genuine SQLite data. It returns the
// roadmap name and the ids it created.
//
// Each sprint is created with an EXPLICIT execution Order chosen to diverge from
// creation order (and therefore from id order), so the Próximos (Order ASC) and
// Concluídos (Order DESC) ordering assertions prove the sort is driven by
// Sprint.Order, not by id.
//
//   - pendingID     created first  (lowest id)  -> Order 30
//   - pendingID2    created second               -> Order 10  (lower Order than pendingID)
//   - closedLower   created third                -> Order 15
//   - openID        created fourth               -> Order 99
//   - closedHigher  created last   (highest id)  -> Order 40  (higher Order than closedLower)
//
// Próximos (Order ASC) must therefore list pendingID2 (10) before pendingID
// (30) even though pendingID has the LOWER id. Concluídos (Order DESC) must list
// closedHigher (40) before closedLower (15); closedHigher has the higher id, so
// to make the assertion discriminate between Order and id, closedHigher also
// gets the EARLIER closed_at — proving the sort is by Order, not by id or
// closed_at.
type sprintFixture struct {
	name         string
	pendingID    int
	pendingID2   int
	openID       int
	closedLower  int // lower Order (15)
	closedHigher int // higher Order (40)
	openTaskID   int
	pendTaskID   int
	pendTaskID2  int
	openTaskID2  int
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
	// mkSprint creates a PENDING sprint with an EXPLICIT execution Order. The
	// fixture chooses Order values that deliberately diverge from creation order
	// (and therefore from id order), so the sprints-page ordering assertions
	// prove the sort is driven by Sprint.Order, not by id.
	mkSprint := func(desc string, order int) int {
		id, serr := database.CreateSprint(ctx, &models.Sprint{
			Status:      models.SprintPending,
			Title:       desc,
			Description: desc,
			CreatedAt:   now,
			Order:       order,
		})
		if serr != nil {
			t.Fatalf("creating sprint %q: %v", desc, serr)
		}
		return id
	}

	f := sprintFixture{name: name}

	// 1) First PENDING sprint (lowest id) with a HIGHER Order (30), so it must
	//    appear AFTER pendingID2 under Próximos (Order ASC) despite the lower id.
	f.pendingID = mkSprint("Plan the read-only web sprint presentation", 30)
	f.pendTaskID = mkTask("Classify sprints into Proximos, Actual, Concluidos tabs")
	f.pendTaskID2 = mkTask("Order Proximos by ascending sprint order")
	if aerr := database.AddTasksToSprint(ctx, f.pendingID, []int{f.pendTaskID, f.pendTaskID2}); aerr != nil {
		t.Fatalf("adding tasks to pending sprint: %v", aerr)
	}

	// 2) Second PENDING sprint with a LOWER Order (10): the next sprint to
	//    execute, so it heads the Próximos tab even though its id is higher.
	f.pendingID2 = mkSprint("Vendor the Tabler admin shell and dark theme", 10)

	// 3) A CLOSED sprint with the LOWER Order (15) and the LATER closed_at, so
	//    Order-descending ranks it AFTER closedHigher while closed_at-descending
	//    would rank it FIRST — proving the Concluídos sort is by Order.
	f.closedLower = mkSprint("Ship the initial knowledge-graph viewer", 15)
	setClosed(t, database, f.closedLower, "2026-05-20T18:30:00Z")

	// 4) OPEN sprint with two member tasks (its card footer shows "2 task(s)"; the
	//    member tasks themselves are shown only on the single sprint page).
	f.openID = mkSprint("Deliver the sprint detail page and task modal", 99)
	f.openTaskID = mkTask("Build the read-only sprint page route and template")
	f.openTaskID2 = mkTask("Render one task detail modal per shown task")
	if aerr := database.AddTasksToSprint(ctx, f.openID, []int{f.openTaskID, f.openTaskID2}); aerr != nil {
		t.Fatalf("adding tasks to open sprint: %v", aerr)
	}
	if serr := database.UpdateSprintStatus(ctx, f.openID, models.SprintOpen); serr != nil {
		t.Fatalf("opening sprint: %v", serr)
	}

	// 5) A CLOSED sprint (highest id) with the HIGHER Order (40) and the EARLIER
	//    closed_at: Order-descending ranks it FIRST in Concluídos, while id and
	//    closed_at would each rank it differently, so the assertion isolates Order.
	f.closedHigher = mkSprint("Harden the web read path against malformed input", 40)
	setClosed(t, database, f.closedHigher, "2026-01-10T09:00:00Z")

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
	for _, id := range []int{f.pendingID, f.pendingID2, f.openID, f.closedLower, f.closedHigher} {
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

	// PENDING -> Próximos, ordered by ascending Order: pendingID2 (Order 10)
	// must precede pendingID (Order 30) even though pendingID2 has the HIGHER id,
	// proving the sort is by Sprint.Order, not by id.
	idxPend2 := strings.Index(upcoming, "/sprints/"+itoa(f.pendingID2))
	idxPend := strings.Index(upcoming, "/sprints/"+itoa(f.pendingID))
	if idxPend2 < 0 || idxPend < 0 {
		t.Fatalf("both PENDING sprints must appear under Próximos; pend2=%d pend=%d", idxPend2, idxPend)
	}
	if idxPend2 > idxPend {
		t.Errorf("Próximos order wrong: sprint #%d (Order 10) must precede sprint #%d (Order 30) despite its higher id", f.pendingID2, f.pendingID)
	}

	// OPEN -> Actual, rendered through the SAME shared sprint-card partial as the
	// other tabs: the card links to the sprint page and shows the description and
	// the task count, but it does NOT expand into an inline member-tasks table or
	// per-task modals (SPEC/WEB.md § Shared Sprint-Card Partial; Acceptance
	// Criteria 8/12/38).
	if !strings.Contains(current, "/sprints/"+itoa(f.openID)) {
		t.Errorf("OPEN sprint #%d not under the Actual tab", f.openID)
	}
	if !strings.Contains(current, "Deliver the sprint detail page and task modal") {
		t.Errorf("Actual tab does not show the OPEN sprint's description in its card")
	}
	if !strings.Contains(current, "2 task(s)") {
		t.Errorf("Actual tab card does not show the OPEN sprint's task count")
	}
	// The OPEN sprint must NOT be expanded: no member-task title and no per-task
	// modal trigger on the sprints landing page.
	if strings.Contains(current, "Build the read-only sprint page route and template") {
		t.Errorf("Actual tab must not show the OPEN sprint's member task title (no inline table)")
	}
	if strings.Contains(current, `data-bs-target="#task-modal-`) {
		t.Errorf("Actual tab must not render a per-task modal trigger")
	}

	// CLOSED -> Concluídos, ordered by descending Order: closedHigher (Order 40)
	// must precede closedLower (Order 15). closedHigher has the higher id AND the
	// EARLIER closed_at, so neither an id-descending nor a closed_at-descending
	// sort would produce this order — only Order-descending does.
	idxHigher := strings.Index(closed, "/sprints/"+itoa(f.closedHigher))
	idxLower := strings.Index(closed, "/sprints/"+itoa(f.closedLower))
	if idxHigher < 0 || idxLower < 0 {
		t.Fatalf("both CLOSED sprints must appear under Concluídos; higher=%d lower=%d", idxHigher, idxLower)
	}
	if idxHigher > idxLower {
		t.Errorf("Concluídos order wrong: sprint #%d (Order 40) must precede sprint #%d (Order 15)", f.closedHigher, f.closedLower)
	}
}

// TestClassifySprints_OrderingRules unit-tests the classification and ordering
// directly with crafted sprints: Próximos by ascending Sprint.Order, Actual by
// ascending Sprint.Order, and Concluídos by descending Sprint.Order. The Order
// values are chosen so they diverge from id order in every group, so each
// assertion proves the sort is driven by Order, not by id (SPEC/WEB.md § Roadmap
// Sprints Page; Acceptance Criterion 12).
func TestClassifySprints_OrderingRules(t *testing.T) {
	views := []sprintView{
		// PENDING: id-ascending would yield [2, 5]; Order (10, 30) inverts that
		// to [5, 2], so the assertion proves the sort is by Order, not id.
		{Sprint: models.Sprint{ID: 5, Status: models.SprintPending, Order: 10}},
		{Sprint: models.Sprint{ID: 2, Status: models.SprintPending, Order: 30}},
		// OPEN: id-ascending would yield [3, 9]; Order (1 for id 9, 2 for id 3)
		// inverts that to [9, 3], so the assertion proves the sort is by Order.
		{Sprint: models.Sprint{ID: 9, Status: models.SprintOpen, Order: 1}},
		{Sprint: models.Sprint{ID: 3, Status: models.SprintOpen, Order: 2}},
		// CLOSED: ids and Order deliberately uncorrelated.
		{Sprint: models.Sprint{ID: 7, Status: models.SprintClosed, Order: 15}},
		{Sprint: models.Sprint{ID: 1, Status: models.SprintClosed, Order: 40}},
		{Sprint: models.Sprint{ID: 8, Status: models.SprintClosed, Order: 5}},
		{Sprint: models.Sprint{ID: 4, Status: models.SprintClosed, Order: 25}},
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

	// Próximos: ascending Order -> #5 (Order 10) before #2 (Order 30). Since #5
	// has the HIGHER id, id-ascending would give [2, 5]; getting [5, 2] proves
	// the sort is by Order.
	assertIDs("Próximos", up, []int{5, 2})
	// Actual: ascending Order -> #9 (Order 1) before #3 (Order 2). Since #9 has
	// the HIGHER id, id-ascending would give [3, 9]; getting [9, 3] proves the
	// sort is by Order.
	assertIDs("Actual", cur, []int{9, 3})
	// Concluídos: descending Order -> #1 (40), #4 (25), #7 (15), #8 (5). The id
	// order is uncorrelated, so this proves the sort is by Order, not id.
	assertIDs("Concluídos", cl, []int{1, 4, 7, 8})
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
// mechanism on every page that shows clickable tasks: the tasks page and the
// sprint page. Each clickable task is wired with data-bs-toggle="modal" to a
// matching modal element, the modal shows the long free-text fields, and it
// contains no form/input/submit. The asserted task (f.openTaskID) is a member of
// the OPEN sprint, so its modal is rendered on the sprint page, and it also
// appears in the full task table on the tasks page. The sprints landing page is
// deliberately excluded: it renders every sprint as a compact card and opens no
// task detail modal (SPEC/WEB.md § Task Detail Modal, § Shared Sprint-Card
// Partial; Acceptance Criteria 8/15/38).
func TestTaskModal_WiringAndContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-task-modal")
	mux := buildMux()

	for _, path := range []string{
		"/roadmaps/" + f.name + "/tasks",                     // tasks page (full task table)
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
