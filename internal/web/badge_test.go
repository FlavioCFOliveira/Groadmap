package web

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// TestTaskStatusBadge asserts the FULL task-status -> Tabler colour-variant
// mapping defined in SPEC/WEB.md § Status, Priority, and Severity Badge Colours.
// Every canonical TaskStatus is covered, plus the neutral fallback for an
// out-of-enum value, proving the helper is total (SPEC rule 1).
func TestTaskStatusBadge(t *testing.T) {
	cases := []struct {
		status models.TaskStatus
		want   string
	}{
		{models.StatusCompleted, "bg-green-lt"},
		{models.StatusTesting, "bg-yellow-lt"},
		{models.StatusDoing, "bg-blue-lt"},
		{models.StatusSprint, "bg-cyan-lt"},
		{models.StatusBacklog, "bg-secondary-lt"},
		{models.TaskStatus("GARBAGE"), "bg-secondary-lt"}, // out-of-enum -> neutral
	}
	for _, c := range cases {
		if got := taskStatusBadge(c.status); got != c.want {
			t.Errorf("taskStatusBadge(%q) = %q, want %q", c.status, got, c.want)
		}
	}
}

// TestTaskStatusBadge_CoversEveryEnumValue guards against a future TaskStatus
// being added in MODELS.md without a mapping here: it iterates the canonical
// ValidTaskStatuses list and fails if any value falls through to the neutral
// fallback by accident (only BACKLOG legitimately maps to bg-secondary-lt).
func TestTaskStatusBadge_CoversEveryEnumValue(t *testing.T) {
	for _, s := range models.ValidTaskStatuses {
		got := taskStatusBadge(s)
		if got == "" {
			t.Errorf("taskStatusBadge(%q) returned empty class; mapping must be total", s)
		}
		if got == "bg-secondary-lt" && s != models.StatusBacklog {
			t.Errorf("taskStatusBadge(%q) = neutral fallback %q; add an explicit mapping per SPEC", s, got)
		}
	}
}

// TestSprintStatusBadge asserts the FULL sprint-status -> Tabler colour-variant
// mapping defined in SPEC/WEB.md § Status, Priority, and Severity Badge Colours.
func TestSprintStatusBadge(t *testing.T) {
	cases := []struct {
		status models.SprintStatus
		want   string
	}{
		{models.SprintClosed, "bg-green-lt"},
		{models.SprintOpen, "bg-blue-lt"},
		{models.SprintPending, "bg-secondary-lt"},
		{models.SprintStatus("GARBAGE"), "bg-secondary-lt"}, // out-of-enum -> neutral
	}
	for _, c := range cases {
		if got := sprintStatusBadge(c.status); got != c.want {
			t.Errorf("sprintStatusBadge(%q) = %q, want %q", c.status, got, c.want)
		}
	}
}

// TestSprintStatusBadge_CoversEveryEnumValue guards against a future
// SprintStatus being added without a mapping: every canonical value must
// resolve, and only PENDING legitimately maps to the neutral variant.
func TestSprintStatusBadge_CoversEveryEnumValue(t *testing.T) {
	for _, s := range models.ValidSprintStatuses {
		got := sprintStatusBadge(s)
		if got == "" {
			t.Errorf("sprintStatusBadge(%q) returned empty class; mapping must be total", s)
		}
		if got == "bg-secondary-lt" && s != models.SprintPending {
			t.Errorf("sprintStatusBadge(%q) = neutral fallback %q; add an explicit mapping per SPEC", s, got)
		}
	}
}

// TestPriorityBadge asserts the priority band boundaries from SPEC/WEB.md
// § Status, Priority, and Severity Badge Colours: 7-9 -> red, 4-6 -> yellow,
// 0-3 -> secondary. It checks every band boundary (0, 3, 4, 6, 7, 9) and that
// the whole 0-9 range resolves to a non-empty class with no gap (totality).
func TestPriorityBadge(t *testing.T) {
	cases := []struct {
		priority int
		want     string
	}{
		{0, "bg-secondary-lt"},
		{3, "bg-secondary-lt"},
		{4, "bg-yellow-lt"},
		{6, "bg-yellow-lt"},
		{7, "bg-red-lt"},
		{9, "bg-red-lt"},
	}
	for _, c := range cases {
		if got := priorityBadge(c.priority); got != c.want {
			t.Errorf("priorityBadge(%d) = %q, want %q", c.priority, got, c.want)
		}
	}
	for p := 0; p <= 9; p++ {
		if priorityBadge(p) == "" {
			t.Errorf("priorityBadge(%d) returned empty class; the 0-9 range must be total", p)
		}
	}
}

// TestSeverityBadge asserts the severity band boundaries from SPEC/WEB.md
// § Status, Priority, and Severity Badge Colours: 8-9 -> red, 6-7 -> orange,
// 3-5 -> yellow, 0-2 -> secondary. It checks every band boundary
// (0, 2, 3, 5, 6, 7, 8, 9) and that the whole 0-9 range resolves (totality).
func TestSeverityBadge(t *testing.T) {
	cases := []struct {
		severity int
		want     string
	}{
		{0, "bg-secondary-lt"},
		{2, "bg-secondary-lt"},
		{3, "bg-yellow-lt"},
		{5, "bg-yellow-lt"},
		{6, "bg-orange-lt"},
		{7, "bg-orange-lt"},
		{8, "bg-red-lt"},
		{9, "bg-red-lt"},
	}
	for _, c := range cases {
		if got := severityBadge(c.severity); got != c.want {
			t.Errorf("severityBadge(%d) = %q, want %q", c.severity, got, c.want)
		}
	}
	for s := 0; s <= 9; s++ {
		if severityBadge(s) == "" {
			t.Errorf("severityBadge(%d) returned empty class; the 0-9 range must be total", s)
		}
	}
}

// seedBadgeRoadmap creates an on-disk roadmap whose single task carries
// distinctive, non-neutral status/priority/severity values, and an OPEN sprint,
// so the rendered HTML carries unambiguous, distinct Tabler colour variants the
// template-level assertions below can detect. The caller must already have
// redirected HOME with t.Setenv.
func seedBadgeRoadmap(t *testing.T, name string) (roadmap string, sprintID int) {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	now := time.Now().UTC().Format(time.RFC3339)

	// The task is created and then added to a sprint, which transitions its
	// status to SPRINT (-> bg-cyan-lt). priority 8 -> bg-red-lt, severity 9 ->
	// bg-red-lt. SPRINT is a distinctive, non-neutral status so the rendered
	// colour is unambiguous.
	task := &models.Task{
		Priority:               8,
		Severity:               9,
		Status:                 models.StatusBacklog,
		Title:                  "Render semantic badge colours across the web UI",
		FunctionalRequirements: "Map status, priority, and severity to Tabler colour variants",
		TechnicalRequirements:  "html/template FuncMap helpers driven by models enums",
		AcceptanceCriteria:     "Every badge uses the SPEC colour for its value",
		CreatedAt:              now,
	}
	taskID, err := database.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("creating task: %v", err)
	}

	// OPEN -> bg-blue-lt on the sprint card, header, and datagrid.
	sprint := &models.Sprint{
		Status:      models.SprintOpen,
		Title:       "Apply the semantic badge colour mapping",
		Description: "Apply the semantic badge colour mapping",
		Order:       1,
		StartedAt:   &now,
		CreatedAt:   now,
	}
	sprintID, err = database.CreateSprint(context.Background(), sprint)
	if err != nil {
		t.Fatalf("creating sprint: %v", err)
	}
	if err := database.AddTasksToSprint(context.Background(), sprintID, []int{taskID}); err != nil {
		t.Fatalf("adding task to sprint: %v", err)
	}
	return name, sprintID
}

// TestTasksPage_RendersSemanticBadgeColours proves the helpers are actually wired
// into the tasks template and emit the SPEC colour variant in the rendered HTML:
// a priority 8 / severity 9 task renders bg-red-lt badges on its board card
// (SPEC/WEB.md § Status, Priority, and Severity Badge Colours, rule 2).
//
// The STATUS badge is deliberately not asserted here. The card shows none — the
// column it sits in already states the status — and the modal that does show one
// is now filled by /static/task-modal.js from the task detail endpoint, so no
// status badge is server-rendered on this page at all. The script's own mapping
// is pinned against these same Go helpers, value by value, in
// TestTaskModalScript_BadgeMappingMatchesTheServerHelpers.
func TestTasksPage_RendersSemanticBadgeColours(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name, _ := seedBadgeRoadmap(t, "badge-colours")

	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/tasks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("tasks status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// Priority 8 -> bg-red-lt badge reading P8, on the board card. The prefix names
	// the field the value belongs to and selects no colour: the variant is the one
	// the mapping assigns to the integer 8 (SPEC/WEB.md § Status, Priority, and
	// Severity Badge Colours, rule 2).
	if !strings.Contains(body, `<span class="badge bg-red-lt">P8</span>`) {
		t.Errorf("tasks page missing priority badge with bg-red-lt reading P8 for priority 8")
	}
	// Severity 9 -> bg-red-lt badge reading S9, on the board card. Priority and
	// severity share the variant here, so the prefix is the only thing that tells
	// the two badges apart — which is the reason the card carries one.
	if !strings.Contains(body, `<span class="badge bg-red-lt">S9</span>`) {
		t.Errorf("tasks page missing severity badge with bg-red-lt reading S9 for severity 9")
	}
	// No status badge is server-rendered on this page: not on the card, and not
	// in the shell, which carries an empty badge element the script fills.
	if strings.Contains(body, `>SPRINT</span>`) {
		t.Errorf("tasks page renders a status badge; the column states the status and the modal " +
			"is filled by the script")
	}
}

// TestSprintPage_RendersSemanticStatusBadge proves the semantic badge helpers are
// wired into the sprint page: the sprint status helper into the page header and
// the sprint detail datagrid, where an OPEN sprint must render bg-blue-lt, and the
// priority and severity helpers into the member-tasks board's cards, where the
// priority 8 / severity 9 member task must render both of its bands
// (SPEC/WEB.md § Status, Priority, and Severity Badge Colours, rule 2).
//
// The member task's STATUS badge is asserted ABSENT, which is the half that moved
// with the board: the card carries no status badge, because the column the card
// sits in already states the status (SPEC/WEB.md § Sprint Detail Sub-Template,
// rule 4, The card; Acceptance Criterion 133). The sprint's own status badge is
// unaffected and is still required above.
func TestSprintPage_RendersSemanticStatusBadge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name, sprintID := seedBadgeRoadmap(t, "badge-colours")

	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/sprints/"+itoa(sprintID), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("sprint status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// OPEN sprint status -> bg-blue-lt (appears in header and datagrid).
	if !strings.Contains(body, "bg-blue-lt") || !strings.Contains(body, ">OPEN<") {
		t.Errorf("sprint page missing OPEN status badge with bg-blue-lt")
	}
	// Member task badges on the board card: priority 8 and severity 9 both fall in
	// the high band, so both take bg-red-lt.
	// Each badge writes its value behind the one-letter prefix that names it, the
	// same form the tasks board's card renders (Acceptance Criteria 85 and 133);
	// the prefix is a label and the variant is still the value's own.
	if !strings.Contains(body, `<span class="badge bg-red-lt">P8</span>`) {
		t.Errorf("the member-tasks board card is missing the priority badge with bg-red-lt reading P8 for priority 8")
	}
	if !strings.Contains(body, `<span class="badge bg-red-lt">S9</span>`) {
		t.Errorf("the member-tasks board card is missing the severity badge with bg-red-lt reading S9 for severity 9")
	}
	// And no status badge for the member task: the task is SPRINT after being
	// added to the sprint, whose badge variant is bg-cyan-lt, and neither the
	// variant nor the value may appear on the card.
	if strings.Contains(body, `<span class="badge bg-cyan-lt">SPRINT</span>`) {
		t.Errorf("the member-tasks board card renders a status badge; the WAITING column it " +
			"sits in already states the status")
	}
	if strings.Contains(body, ">SPRINT<") {
		t.Errorf("the sprint page renders the member task's status value; the board states it " +
			"by the column and the modal is filled by the script")
	}
}

// tabBadgeMarkup builds the exact markup one sprints-page tab must render: the
// tab's label, then its count badge carrying class and count. The leading ">"
// anchors the match to the end of the tab link's opening tag, so the assertion
// is tied to a specific tab and cannot be satisfied by an identical badge
// belonging to another tab.
func tabBadgeMarkup(label, class string, count int) string {
	return ">" + label + ` <span class="badge ` + class + ` ms-1">` + strconv.Itoa(count) + "</span>"
}

// sprintTabColours returns the three tab colours the sprint status mapping
// dictates, taken FROM the mapping rather than written down again here, and
// fails the test if they are not pairwise distinct.
//
// The distinctness check is what stops the assertions built on these values from
// passing vacuously. Every expectation below is computed from sprintStatusBadge,
// so a mapping that collapsed to one colour for every status would agree with a
// template that hardcodes that colour on all three tabs, and both would be
// wrong. Requiring three distinct colours makes an all-bg-secondary-lt rendering
// unreachable: it cannot satisfy three different expected classes.
func sprintTabColours(t *testing.T) (pending, open, closed string) {
	t.Helper()
	pending = sprintStatusBadge(models.SprintPending)
	open = sprintStatusBadge(models.SprintOpen)
	closed = sprintStatusBadge(models.SprintClosed)
	if pending == open || pending == closed || open == closed {
		t.Fatalf("the sprint status mapping gives PENDING/OPEN/CLOSED the non-distinct "+
			"classes %q/%q/%q; the tab assertions below would pass on a rendering that "+
			"paints all three tabs one colour", pending, open, closed)
	}
	return pending, open, closed
}

// TestSprintsPage_TabCountBadgesCarryTheirTabStatusColour proves the three tabs
// on the Roadmap Sprints Page render count badges whose TEXT is the tab's sprint
// count and whose COLOUR is the variant the sprint status mapping assigns to the
// status that tab groups: Próximos (PENDING) bg-secondary-lt, Actual (OPEN)
// bg-blue-lt, Concluídos (CLOSED) bg-green-lt (SPEC/WEB.md § Roadmap Sprints
// Page; § Status, Priority, and Severity Badge Colours, rule 2; Acceptance
// Criteria 60 and 120).
//
// The three tabs are asserted TOGETHER, and that is the point of the test.
// PENDING maps to bg-secondary-lt, which is exactly the fixed class the template
// carried before this mapping was applied, so the Próximos badge renders
// identically whether the mapping colours it or not and an assertion on Próximos
// alone would pass against the unfixed template. Only Actual and Concluídos can
// fail, so the rule is exercised only when all three are checked at once: a
// rendering that gives all three tabs bg-secondary-lt fails here on two of them.
//
// The expected classes come from sprintStatusBadge itself rather than from
// literals, so the test follows the mapping if the SPEC reassigns a colour; the
// distinctness guard in sprintTabColours is what keeps that from making the
// assertions vacuous.
func TestSprintsPage_TabCountBadgesCarryTheirTabStatusColour(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// The fixture holds 2 PENDING, 1 OPEN and 2 CLOSED sprints, so each tab
	// shows a non-zero count and no two tabs are told apart by count alone.
	f := seedSprintFixture(t, "sprint-tab-badge-colours")
	pending, open, closed := sprintTabColours(t)

	mux := buildMux()
	header := cardHeaderSlice(t, servePage(t, mux, "/roadmaps/"+f.name))

	cases := []struct {
		label  string
		class  string
		status models.SprintStatus
		count  int
	}{
		{"Próximos", pending, models.SprintPending, 2},
		{"Actual", open, models.SprintOpen, 1},
		{"Concluídos", closed, models.SprintClosed, 2},
	}
	for _, c := range cases {
		want := tabBadgeMarkup(c.label, c.class, c.count)
		if !strings.Contains(header, want) {
			t.Errorf("the %s tab (%s sprints) does not render %q; the tab badge must carry "+
				"the count as its text and the %s colour variant %q as its class",
				c.label, c.status, want, c.status, c.class)
		}
	}
}

// TestSprintsPage_EmptyTabKeepsItsStatusColour proves the tab colour follows the
// TAB's status and not the sprints inside it: a tab holding no sprint shows the
// count 0 and keeps its own colour (SPEC/WEB.md § Roadmap Sprints Page;
// Acceptance Criterion 120).
//
// The seeded roadmap has a single OPEN sprint, so Próximos and Concluídos are
// both empty. Concluídos is the discriminating case — an empty tab that must
// still render bg-green-lt — and, as above, the three tabs are asserted together
// so the neutral Próximos badge is never the only evidence.
func TestSprintsPage_EmptyTabKeepsItsStatusColour(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name, _ := seedBadgeRoadmap(t, "empty-sprint-tabs")
	pending, open, closed := sprintTabColours(t)

	mux := buildMux()
	header := cardHeaderSlice(t, servePage(t, mux, "/roadmaps/"+name))

	cases := []struct {
		label string
		class string
		count int
	}{
		{"Próximos", pending, 0},  // no PENDING sprint: 0, still the PENDING colour
		{"Actual", open, 1},       // the one OPEN sprint
		{"Concluídos", closed, 0}, // no CLOSED sprint: 0, still the CLOSED colour
	}
	for _, c := range cases {
		want := tabBadgeMarkup(c.label, c.class, c.count)
		if !strings.Contains(header, want) {
			t.Errorf("the %s tab does not render %q; an empty tab shows the count 0 and "+
				"keeps the colour of the status it groups", c.label, want)
		}
	}
}

// TestSprintsTemplate_TabBadgeClassComesFromTheHelper proves the template DECIDES
// each tab's class by calling the semantic helper with that tab's status, rather
// than carrying a class that happens to read the same as the helper's answer.
//
// It re-parses the embedded templates with sprintStatusBadge replaced by a probe
// that returns a sentinel class naming the status it was called with, then
// renders the sprints page and looks for the sentinels. A hardcoded class
// survives the substitution unchanged and fails; only a template that calls the
// helper renders "probe-PENDING", "probe-OPEN" and "probe-CLOSED".
//
// This is what makes the Próximos tab non-vacuous on its own terms. Against the
// real mapping, Próximos is indistinguishable from a fixed bg-secondary-lt; under
// the probe, a fixed bg-secondary-lt is exactly what a non-conforming template
// still shows, while a conforming one shows probe-PENDING. The test also pins
// each tab to the RIGHT status: swapping two tabs' statuses would keep three
// distinct classes but produce the sentinels in the wrong places.
//
// The view model is the zero value, so every tab holds no sprint and shows 0 —
// which also proves the colour is chosen with no sprint to read a status from.
func TestSprintsTemplate_TabBadgeClassComesFromTheHelper(t *testing.T) {
	funcs := make(map[string]any, len(badgeFuncMap))
	for name, fn := range badgeFuncMap {
		funcs[name] = fn
	}
	funcs["sprintStatusBadge"] = func(s models.SprintStatus) string { return "probe-" + string(s) }

	tmpl, err := template.New("").Funcs(funcs).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		t.Fatalf("parsing the embedded templates with the probe helper: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "sprints.html", sprintsData{Name: "probe"}); err != nil {
		t.Fatalf("rendering sprints.html with the probe helper: %v", err)
	}
	out := buf.String()

	cases := []struct {
		label  string
		status models.SprintStatus
	}{
		{"Próximos", models.SprintPending},
		{"Actual", models.SprintOpen},
		{"Concluídos", models.SprintClosed},
	}
	for _, c := range cases {
		want := tabBadgeMarkup(c.label, "probe-"+string(c.status), 0)
		if !strings.Contains(out, want) {
			t.Errorf("the %s tab does not render %q under the probe helper: its class is not "+
				"produced by sprintStatusBadge(%s) — either the class is written into the "+
				"template or the tab is passing the wrong status", c.label, want, c.status)
		}
	}
	// Control: the probe replaced the real mapping, so no tab may still carry a
	// real Tabler variant. A leftover bg-*-lt on a tab badge is a hardcoded class.
	for _, c := range cases {
		for _, class := range []string{badgeSecondary, badgeBlue, badgeGreen} {
			if strings.Contains(out, tabBadgeMarkup(c.label, class, 0)) {
				t.Errorf("the %s tab renders the fixed class %q even though sprintStatusBadge "+
					"was replaced; the class is hardcoded in the template", c.label, class)
			}
		}
	}
}

// TestSprintsData_TabStatusMatchesTheClassification pins the status each tab is
// coloured by to the status of the sprints that tab actually holds. The template
// names the status beside the tab it belongs to (Próximos PENDING, Actual OPEN,
// Concluídos CLOSED) while classifySprints does the partitioning, and this test
// is what keeps the two from drifting apart — a drift would leave every tab
// coloured, plausibly, and wrongly (SPEC/WEB.md § Roadmap Sprints Page;
// Acceptance Criterion 120).
func TestSprintsData_TabStatusMatchesTheClassification(t *testing.T) {
	upcoming, current, closed := classifySprints([]sprintView{
		{Sprint: models.Sprint{ID: 1, Status: models.SprintPending, Order: 1}},
		{Sprint: models.Sprint{ID: 2, Status: models.SprintOpen, Order: 2}},
		{Sprint: models.Sprint{ID: 3, Status: models.SprintClosed, Order: 3}},
	})

	cases := []struct {
		label   string
		status  models.SprintStatus
		sprints []sprintView
	}{
		{"Próximos", models.SprintPending, upcoming},
		{"Actual", models.SprintOpen, current},
		{"Concluídos", models.SprintClosed, closed},
	}
	for _, c := range cases {
		if len(c.sprints) == 0 {
			t.Fatalf("the %s tab holds no sprint; the assertion below would be vacuous", c.label)
		}
		for i := range c.sprints {
			if got := c.sprints[i].Sprint.Status; got != c.status {
				t.Errorf("the %s tab holds a %s sprint but is coloured as %s; the tab's colour "+
					"would state a status the tab does not group", c.label, got, c.status)
			}
		}
	}
}
