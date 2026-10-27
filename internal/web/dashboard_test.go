package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file is the gate of the Roadmap Dashboard Page (SPEC/WEB.md § Roadmap
// Dashboard Page; Acceptance Criteria 265 to 286), of its chart data block
// (SPEC/DATA_FORMATS.md § Dashboard Chart Data), and of the vendored chart assets
// (SPEC/BUILD.md § Vendored Web Assets, rules 7 and 8, and its verification
// checklist). The statement count and the single transaction of the page's read
// are measured at the driver boundary in internal/db (dashboard_test.go); this
// file measures what the page states.

// dashNow is the request instant every fixture of this file is built around: a
// Wednesday, so the week holding it began two days before.
var dashNow = time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)

// setDashboardNow fixes the request instant the dashboard handler reads.
func setDashboardNow(t *testing.T, now time.Time) {
	t.Helper()
	saved := dashboardNow
	dashboardNow = func() time.Time { return now }
	t.Cleanup(func() { dashboardNow = saved })
}

// iso writes an instant in the canonical stored form.
func iso(at time.Time) string { return utils.FormatISO8601(at) }

// dashTask is one task of a dashboard fixture: its type, status, severity,
// priority, and the timestamps the dashboard reads. A nil timestamp is stored
// NULL; created defaults to three weeks before dashNow.
type dashTask struct {
	created  *time.Time
	started  *time.Time
	closed   *time.Time
	title    string
	typ      models.TaskType
	status   models.TaskStatus
	severity int
	priority int
}

func at(t time.Time) *time.Time { return &t }

// openDashRoadmap creates the roadmap and returns its writable handle.
func openDashRoadmap(t *testing.T, name string) *db.DB {
	t.Helper()
	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

// addDashTasks inserts the tasks through the statement `task create` runs, then
// writes the status and the timestamps each one names directly, as the other
// fixtures of this package do for the states a create cannot produce.
func addDashTasks(t *testing.T, database *db.DB, tasks []dashTask) []int {
	t.Helper()
	ids := make([]int, len(tasks))
	for i, task := range tasks {
		created := dashNow.AddDate(0, 0, -21)
		if task.created != nil {
			created = *task.created
		}
		id, err := seedTask(database, &models.Task{
			Title:                  task.title,
			Type:                   task.typ,
			Status:                 models.StatusBacklog,
			Priority:               task.priority,
			Severity:               task.severity,
			FunctionalRequirements: "Operators see the settlement state of every batch",
			CreatedAt:              iso(created),
		})
		if err != nil {
			t.Fatalf("creating task %q: %v", task.title, err)
		}
		ids[i] = id
	}
	return ids
}

// setDashTaskStates writes each task's status and timestamps. It runs after the
// sprint membership is in place, because adding a task to a sprint moves it to
// SPRINT and refuses a COMPLETED one.
func setDashTaskStates(t *testing.T, database *db.DB, ids []int, tasks []dashTask) {
	t.Helper()
	opt := func(p *time.Time) any {
		if p == nil {
			return nil
		}
		return iso(*p)
	}
	for i, task := range tasks {
		if _, err := database.Exec(`UPDATE tasks SET status = ?, started_at = ?, closed_at = ? WHERE id = ?`,
			task.status, opt(task.started), opt(task.closed), ids[i]); err != nil {
			t.Fatalf("setting task %d: %v", ids[i], err)
		}
	}
}

// addDashSprint inserts a PENDING sprint at the given execution order and puts the
// given tasks in it.
func addDashSprint(t *testing.T, database *db.DB, title string, order int, members []int) int {
	t.Helper()
	id, err := seedSprint(database, &models.Sprint{
		Title: title, Description: title, Status: models.SprintPending,
		CreatedAt: iso(dashNow.AddDate(0, -2, 0)), Order: order,
	})
	if err != nil {
		t.Fatalf("creating sprint %q: %v", title, err)
	}
	if len(members) > 0 {
		if err := database.AddTasksToSprint(context.Background(), id, members); err != nil {
			t.Fatalf("adding tasks to sprint %q: %v", title, err)
		}
	}
	return id
}

// setDashSprint writes a sprint's status, start, close, and capacity.
func setDashSprint(t *testing.T, database *db.DB, id int, status models.SprintStatus, started, closed *time.Time, maxTasks any) {
	t.Helper()
	opt := func(p *time.Time) any {
		if p == nil {
			return nil
		}
		return iso(*p)
	}
	if _, err := database.Exec(`UPDATE sprints SET status = ?, started_at = ?, closed_at = ?, max_tasks = ? WHERE id = ?`,
		status, opt(started), opt(closed), maxTasks, id); err != nil {
		t.Fatalf("setting sprint %d: %v", id, err)
	}
}

// addDependency records that task depends on dependsOn.
func addDependency(t *testing.T, database *db.DB, task, dependsOn int) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO task_dependencies (task_id, depends_on_task_id) VALUES (?, ?)`,
		task, dependsOn); err != nil {
		t.Fatalf("recording that task %d depends on %d: %v", task, dependsOn, err)
	}
}

// dashFixture is the seeded roadmap of Acceptance Criteria 268, 269, 271, 272,
// 280, and 286: 12 tasks — 3 BACKLOG, 2 SPRINT, 2 DOING, 1 TESTING, 4 COMPLETED —
// of 6 TASK (4 open, 2 completed), 4 BUG (3 open, 1 completed), and 2 CHORE
// (1 open, 1 completed), the 3 BACKLOG tasks being 2 TASK and 1 CHORE; an OPEN sprint with max_tasks 8, started 3 days and 5
// hours before dashNow, holding 2 SPRINT, 2 DOING, 1 TESTING, and 1 COMPLETED
// task; one CLOSED sprint, so the average velocity is not zero; and one PENDING
// sprint titled with markup.
type dashFixture struct {
	name      string
	ids       []int
	openID    int
	closedID  int
	pendingID int
	startedAt string
}

// The two titles of Acceptance Criterion 280.
const (
	dashScriptTitle = "</script><script>alert(1)</script>"
	dashMarkupTitle = "A & B <C>"
)

func seedDashFixture(t *testing.T, name string) dashFixture {
	t.Helper()
	database := openDashRoadmap(t, name)
	started := dashNow.Add(-(3*24 + 5) * time.Hour)
	tasks := []dashTask{
		{title: "Draft the settlement reconciliation runbook", typ: models.TypeTask, status: models.StatusBacklog, severity: 2, priority: 3},
		{title: dashScriptTitle, typ: models.TypeTask, status: models.StatusBacklog, severity: 9, priority: 1},
		{title: "Rotate the settlement service credentials", typ: models.TypeChore, status: models.StatusBacklog, severity: 6, priority: 4},
		{title: "Expose the batch status endpoint", typ: models.TypeTask, status: models.StatusSprint, severity: 3, priority: 5},
		{title: "Duplicate rows in the nightly ledger export", typ: models.TypeBug, status: models.StatusSprint, severity: 8, priority: 7},
		{title: "Migrate the settlement scheduler to the new queue", typ: models.TypeTask, status: models.StatusDoing, severity: 5, priority: 6,
			started: at(dashNow.Add(-50 * time.Hour))},
		{title: "Timeout when the acquirer feed is slow", typ: models.TypeBug, status: models.StatusDoing, severity: 7, priority: 8,
			started: at(dashNow.Add(-26 * time.Hour))},
		{title: "Rounding drift in the FX conversion report", typ: models.TypeBug, status: models.StatusTesting, severity: 4, priority: 2,
			started: at(dashNow.Add(-74 * time.Hour))},
		{title: "Add the batch identifier to the audit trail", typ: models.TypeTask, status: models.StatusCompleted, severity: 3, priority: 3,
			started: at(started), closed: at(dashNow.Add(-26 * time.Hour))},
		{title: "Publish the reconciliation dashboard", typ: models.TypeTask, status: models.StatusCompleted, severity: 2, priority: 5,
			started: at(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)), closed: at(time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC))},
		{title: "Negative balance shown after a refund", typ: models.TypeBug, status: models.StatusCompleted, severity: 9, priority: 9,
			started: at(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)), closed: at(time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC))},
		{title: "Upgrade the ledger client library", typ: models.TypeChore, status: models.StatusCompleted, severity: 1, priority: 2,
			started: at(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)), closed: at(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC))},
	}
	ids := addDashTasks(t, database, tasks)
	f := dashFixture{name: name, ids: ids, startedAt: iso(started)}

	f.closedID = addDashSprint(t, database, "Settlement visibility", 1, []int{ids[9], ids[10]})
	f.openID = addDashSprint(t, database, "Ledger cut-over", 2, ids[3:9])
	f.pendingID = addDashSprint(t, database, dashMarkupTitle, 3, nil)
	setDashTaskStates(t, database, ids, tasks)
	setDashSprint(t, database, f.closedID, models.SprintClosed,
		at(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)), at(time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)), nil)
	setDashSprint(t, database, f.openID, models.SprintOpen, at(started), nil, 8)

	// A SPRINT task depends on a DOING task: blocked. A BACKLOG task depends on a
	// COMPLETED task: not blocked. A COMPLETED task depends on an open task: never
	// counted.
	addDependency(t, database, ids[3], ids[5])
	addDependency(t, database, ids[0], ids[9])
	addDependency(t, database, ids[10], ids[2])
	return f
}

// getDashboard serves GET /roadmaps/{name} at the fixed request instant and
// returns the recorder.
func getDashboard(t *testing.T, name string, now time.Time) *httptest.ResponseRecorder {
	t.Helper()
	setDashboardNow(t, now)
	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmaps/"+name, nil))
	return rec
}

// dashboardBody serves the dashboard and fails unless it answers 200.
func dashboardBody(t *testing.T, name string, now time.Time) string {
	t.Helper()
	rec := getDashboard(t, name, now)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /roadmaps/%s: status = %d, want 200; body=%q", name, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// dashCard returns the markup of the dashboard card carrying data-role role, up
// to the next card of the grid or the end of the grid.
func dashCard(t *testing.T, body, role string) string {
	t.Helper()
	open := `<div class="card" data-role="` + role + `">`
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatalf("the dashboard renders no %s card", role)
	}
	rest := body[i+len(open):]
	end := len(rest)
	if j := strings.Index(rest, `<div class="card" data-role="`); j >= 0 {
		end = j
	}
	if j := strings.Index(rest, `<script type="application/json"`); j >= 0 && j < end {
		end = j
	}
	return rest[:end]
}

var (
	dashDataRe  = regexp.MustCompile(`(?s)<script type="application/json" id="dashboard-data">(.*?)</script>`)
	dashRowRe   = regexp.MustCompile(`(?s)<tr>(.*?)</tr>`)
	dashCellRe  = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	dashColRe   = regexp.MustCompile(`<div class="(col-[^"]+)">`)
	dashTitleRe = regexp.MustCompile(`<h3 class="card-title">([^<]+)(?:<a [^>]*>([^<]*)</a>)?</h3>|<div class="subheader">([^<]+)</div>`)
	dashChartRe = regexp.MustCompile(`<div [^>]*id="(chart-[a-z-]+)"`)
	dashDonutRe = regexp.MustCompile(`<div id="(chart-[a-z]+-by-type)" role="img" aria-label="([^"]*)"></div>`)
)

// dashKPIRow is the row-and-column layout of the Open tasks, Completed, and
// Backlog KPI cards (SPEC/WEB.md § Roadmap Dashboard Page, Block 1).
const dashKPIRow = `<div class="row align-items-center">`

// dashGridCols returns the column classes of the grid's blocks, in order: every
// col-* column except the col-auto donut columns inside the KPI cards, which are
// not blocks of the grid.
func dashGridCols(main string) []string {
	var cols []string
	for _, m := range dashColRe.FindAllStringSubmatch(main, -1) {
		if m[1] != "col-auto" {
			cols = append(cols, m[1])
		}
	}
	return cols
}

// dashCharts decodes the page's one chart data block.
func dashCharts(t *testing.T, body string) dashboardCharts {
	t.Helper()
	m := dashDataRe.FindAllStringSubmatch(body, -1)
	if len(m) != 1 {
		t.Fatalf("the dashboard carries %d chart data blocks, want exactly 1", len(m))
	}
	var c dashboardCharts
	if err := json.Unmarshal([]byte(m[0][1]), &c); err != nil {
		t.Fatalf("the chart data block is not JSON: %v\n%s", err, m[0][1])
	}
	return c
}

// dashAriaLabels returns each chart container's aria-label by container id,
// unescaped: the two line charts and the KPI type-breakdown donuts.
func dashAriaLabels(body string) map[string]string {
	labels := map[string]string{}
	for _, m := range dashDonutRe.FindAllStringSubmatch(body, -1) {
		labels[m[1]] = htmlUnescape(m[2])
	}
	return labels
}

// htmlUnescape undoes the entity escaping html/template applies to attribute and
// text values, for the characters the dashboard's texts can hold.
func htmlUnescape(s string) string {
	return strings.NewReplacer("&#34;", `"`, "&#39;", "'", "&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

// dashRows returns the <td> cell markup of every body row of a card's table.
func dashRows(card string) [][]string {
	var rows [][]string
	for _, r := range dashRowRe.FindAllStringSubmatch(card, -1) {
		cells := dashCellRe.FindAllStringSubmatch(r[1], -1)
		if len(cells) == 0 {
			continue // the header row holds <th> cells only
		}
		row := make([]string, len(cells))
		for i, c := range cells {
			row[i] = strings.TrimSpace(c[1])
		}
		rows = append(rows, row)
	}
	return rows
}

// ==================== AC 265: THE LANDING PAGE ====================

// TestDashboard_IsTheLandingPage is Acceptance Criterion 265.
func TestDashboard_IsTheLandingPage(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	setServerHostname(t, "thinkpad")

	rec := getDashboard(t, f.name, dashNow)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	body := rec.Body.String()

	if !strings.Contains(body, "<title>settlement-platform - Dashboard - thinkpad</title>") {
		t.Errorf("the document title is not <roadmap> - Dashboard - <hostname>")
	}
	header := regionBetween(t, body, `<div class="page-header d-print-none">`, `<main class="page-body">`)
	if !strings.Contains(header, `<h2 class="page-title">Dashboard</h2>`) {
		t.Errorf("the header title is not Dashboard; header=%q", header)
	}
	for _, absent := range []string{"page-pretitle", `class="badge`, "ms-auto", "<a ", "<form", "<select", "<input"} {
		if strings.Contains(header, absent) {
			t.Errorf("the header carries %q; the dashboard header has no pretitle, no badge, and no actions", absent)
		}
	}

	main := regionBetween(t, body, `<main class="page-body">`, `</main>`)
	if n := strings.Count(main, `<div class="row row-deck row-cards">`); n != 1 {
		t.Fatalf("the page body holds %d row-deck row-cards grids, want 1", n)
	}
	// The one grid, and inside it only the row-and-column layout of the three
	// KPI cards that carry a type-breakdown donut.
	if strings.Count(main, dashKPIRow) != 3 || strings.Count(main, `class="row`)-strings.Count(main, dashKPIRow) != 1 {
		t.Errorf("the page body holds a row other than the one grid and the three KPI card rows")
	}
	cols := dashGridCols(main)
	wantCols := []string{
		"col-sm-6 col-lg-3", "col-sm-6 col-lg-3", "col-sm-6 col-lg-3", "col-sm-6 col-lg-3",
		"col-12", "col-lg-6", "col-lg-6", "col-12", "col-12",
	}
	if strings.Join(cols, "|") != strings.Join(wantCols, "|") {
		t.Errorf("the grid's columns are %q, want %q", cols, wantCols)
	}
	titleMatches := dashTitleRe.FindAllStringSubmatch(main, -1)
	titles := make([]string, 0, len(titleMatches))
	for _, m := range titleMatches {
		titles = append(titles, m[1]+m[2]+m[3])
	}
	wantTitles := []string{
		"Open tasks", "Completed", "Backlog", "Average velocity", "Current sprint: Ledger cut-over",
		"Work in progress", "Highest-severity open tasks", "Upcoming sprints", "Recent activity",
	}
	if strings.Join(titles, "|") != strings.Join(wantTitles, "|") {
		t.Errorf("the blocks read %q, want %q", titles, wantTitles)
	}
	// The KPI cards carry no card-header.
	for _, role := range []string{"kpi-open", "kpi-completed", "kpi-backlog", "kpi-velocity"} {
		if strings.Contains(dashCard(t, body, role), "card-header") {
			t.Errorf("the %s KPI card carries a card-header", role)
		}
	}

	// Every roadmap link of the index points to the dashboard.
	index := servePage(t, buildMux(), "/")
	if !strings.Contains(index, `<a class="btn btn-primary" href="/roadmaps/settlement-platform">`) {
		t.Errorf("the index does not link the roadmap to /roadmaps/{name}")
	}

	// 404 for a missing roadmap and an invalid name, each still no-store.
	for _, path := range []string{"/roadmaps/no-such-roadmap", "/roadmaps/..%2fetc", "/roadmaps/Bad_Name!"} {
		rec := httptest.NewRecorder()
		handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("GET %s: Cache-Control = %q, want no-store", path, got)
		}
	}
}

// ==================== AC 266: THE SIDEBAR ====================

// sidebarRoadmapLinks returns the (label, href) of each link of the sidebar's
// per-roadmap section, in order, and the label of each entry marked active.
var sidebarLinkRe = regexp.MustCompile(`(?s)<li class="nav-item( active)?">\s*<a class="nav-link" href="([^"]+)"( aria-current="page")?>.*?<span class="nav-link-title">([^<]+)</span>`)

// TestDashboard_SidebarListsFiveViews is Acceptance Criterion 266.
func TestDashboard_SidebarListsFiveViews(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	setDashboardNow(t, dashNow)
	mux := buildMux()
	base := "/roadmaps/" + f.name

	want := []string{
		"Dashboard " + base, "Sprints " + base + "/sprints", "Tasks " + base + "/tasks",
		"Audit " + base + "/audit", "Graph " + base + "/graph",
	}
	active := map[string]string{
		base:                                "Dashboard",
		base + "/sprints":                   "Sprints",
		base + "/tasks":                     "Tasks",
		base + "/audit":                     "Audit",
		base + "/graph":                     "Knowledge graph",
		base + "/sprints/" + itoa(f.openID): "Sprints",
		base + "/tasks/" + strconv.Itoa(f.ids[0]): "Tasks",
	}
	for path, activeView := range active {
		body := servePage(t, mux, path)
		section := body[strings.Index(body, `<li class="nav-section-title">`):strings.Index(body, "</aside>")]
		var got, marked []string
		for _, m := range sidebarLinkRe.FindAllStringSubmatch(section, -1) {
			got = append(got, m[4]+" "+m[2])
			if (m[1] != "") != (m[3] != "") {
				t.Errorf("%s: the %s entry carries active and aria-current unequally", path, m[4])
			}
			if m[1] != "" {
				marked = append(marked, m[4])
			}
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: the per-roadmap links are %q, want %q", path, got, want)
		}
		wantMarked := activeView
		if activeView == "Knowledge graph" {
			wantMarked = "Graph"
		}
		if len(marked) != 1 || marked[0] != wantMarked {
			t.Errorf("%s: the active per-roadmap entries are %q, want exactly %q", path, marked, wantMarked)
		}
	}
}

// ==================== AC 267: THE SPRINTS PAGE MOVED ====================

// TestDashboard_SprintsPageAtSprintsRoute is Acceptance Criterion 267.
func TestDashboard_SprintsPageAtSprintsRoute(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	setServerHostname(t, "thinkpad")
	setDashboardNow(t, dashNow)
	h := handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmaps/"+f.name+"/sprints", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sprints: status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("GET /sprints: Cache-Control = %q, want no-store", got)
	}
	sprints := rec.Body.String()
	for _, want := range []string{
		"<title>settlement-platform - Sprints - thinkpad</title>",
		`id="tab-current"`, ">Próximos ", ">Actual ", ">Concluídos ",
		`<a href="#tab-current" class="nav-link active" data-bs-toggle="tab" role="tab" aria-selected="true">Actual`,
	} {
		if !strings.Contains(sprints, want) {
			t.Errorf("the sprints page lacks %q", want)
		}
	}
	for _, path := range []string{"/roadmaps/no-such-roadmap/sprints", "/roadmaps/Bad_Name!/sprints"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
		}
	}

	target := `href="/roadmaps/` + f.name + `/sprints"`
	sprintPage := servePage(t, buildMux(), "/roadmaps/"+f.name+"/sprints/"+itoa(f.openID))
	if !strings.Contains(sprintPage, `<a class="btn btn-outline-secondary" `+target+`><i class="ti ti-arrow-left me-1"></i>Back to sprints</a>`) {
		t.Errorf("the sprint page's back link does not point to the sprints page")
	}
	if !strings.Contains(sprintPage, `<a class="nav-link" `+target) {
		t.Errorf("the sidebar's Sprints link does not point to the sprints page")
	}

	// The No open sprint empty state's action link.
	empty := "treasury-ops"
	openDashRoadmap(t, empty)
	card := dashCard(t, dashboardBody(t, empty, dashNow), "current-sprint")
	if !strings.Contains(card, `<a class="btn btn-primary" href="/roadmaps/`+empty+`/sprints">View sprints</a>`) {
		t.Errorf("the No open sprint action link does not point to the sprints page; card=%q", card)
	}

	// No link anywhere presents /roadmaps/{name} as the sprints page.
	linkRe := regexp.MustCompile(`(?s)<a [^>]*href="/roadmaps/` + f.name + `"[^>]*>(.*?)</a>`)
	for _, path := range allPagePaths(f.name) {
		if strings.HasSuffix(path, "/tasks/1") || strings.HasSuffix(path, "/sprints/1") {
			continue // allPagePaths assumes seedRoadmap's ids
		}
		body := servePage(t, buildMux(), path)
		for _, m := range linkRe.FindAllStringSubmatch(body, -1) {
			if strings.Contains(m[1], "Sprints") || strings.Contains(m[1], "sprints") {
				t.Errorf("%s: a link to /roadmaps/%s reads %q, presenting the landing page as the sprints page", path, f.name, m[1])
			}
		}
	}
}

// ==================== AC 268: THE KPI ROW ====================

// TestDashboard_KPIRowStatesTheFourFigures is Acceptance Criterion 268.
func TestDashboard_KPIRowStatesTheFourFigures(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	body := dashboardBody(t, f.name, dashNow)

	open := dashCard(t, body, "kpi-open")
	for _, want := range []string{`<div class="subheader">Open tasks</div>`, `<div class="h1 mb-3">8</div>`, ">3 in progress, 1 blocked<"} {
		if !strings.Contains(open, want) {
			t.Errorf("the Open tasks card lacks %q; card=%q", want, open)
		}
	}
	completed := dashCard(t, body, "kpi-completed")
	for _, want := range []string{
		`<div class="subheader">Completed</div>`, `<div class="h1 mb-3">4</div>`, "<div>4 of 12 tasks</div>",
		`<div class="ms-auto">33%</div>`,
		`<progress class="progress progress-sm" value="4" max="12" aria-label="Completed tasks"></progress>`,
	} {
		if !strings.Contains(completed, want) {
			t.Errorf("the Completed card lacks %q; card=%q", want, completed)
		}
	}
	// The completion row is one d-flex mb-2 row of exactly two div children, a
	// classless leading div and a trailing ms-auto div, with no style attribute
	// on any of the three, and the progress bar is the element immediately after
	// it. The pattern pins every tag in full, so an extra attribute, a third
	// child, or a missing class fails it.
	completionRow := regexp.MustCompile(`<div class="d-flex mb-2">\s*<div>4 of 12 tasks</div>\s*` +
		`<div class="ms-auto">33%</div>\s*</div>\s*` +
		`<progress class="progress progress-sm" value="4" max="12" aria-label="Completed tasks"></progress>`)
	if !completionRow.MatchString(completed) {
		t.Errorf("the Completed card's completion row is not the d-flex mb-2 row of a classless div and an ms-auto div, followed by the progress bar; card=%q", completed)
	}
	backlog := dashCard(t, body, "kpi-backlog")
	for _, want := range []string{`<div class="subheader">Backlog</div>`, `<div class="h1 mb-3">3</div>`, ">tasks in no sprint<"} {
		if !strings.Contains(backlog, want) {
			t.Errorf("the Backlog card lacks %q; card=%q", want, backlog)
		}
	}

	// The value `rmp stats` reports: the command reads db.GetRoadmapStats.
	database, err := db.OpenReadOnly(f.name)
	if err != nil {
		t.Fatalf("opening the roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup
	stats, err := database.GetRoadmapStats(context.Background(), f.name)
	if err != nil {
		t.Fatalf("GetRoadmapStats: %v", err)
	}
	if stats.AverageVelocity == 0 {
		t.Fatalf("the fixture's average velocity is 0; the comparison would be vacuous")
	}
	velocity := dashCard(t, body, "kpi-velocity")
	for _, want := range []string{
		`<div class="subheader">Average velocity</div>`,
		`<div class="h1 mb-3">` + strconv.FormatFloat(stats.AverageVelocity, 'f', 1, 64) + `</div>`,
		">tasks per day, last 5 closed sprints<",
	} {
		if !strings.Contains(velocity, want) {
			t.Errorf("the Average velocity card lacks %q; card=%q", want, velocity)
		}
	}
}

// ==================== AC 269 AND 270: THE CURRENT SPRINT ====================

// TestDashboard_CurrentSprintStatesItsProgress is Acceptance Criterion 269.
func TestDashboard_CurrentSprintStatesItsProgress(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	body := dashboardBody(t, f.name, dashNow)
	card := dashCard(t, body, "current-sprint")

	// The card title names the sprint: "Current sprint: " is plain text and the
	// sprint's title alone is the link to the sprint's page.
	sprintHref := `/roadmaps/` + f.name + `/sprints/` + itoa(f.openID)
	wantHeader := `<h3 class="card-title">Current sprint: <a href="` + sprintHref +
		`" data-role="current-sprint-link">Ledger cut-over</a></h3>`
	if !strings.Contains(card, wantHeader) {
		t.Errorf("the Current sprint card title is not %q; card=%q", wantHeader, card)
	}
	_, bodyPart, ok := strings.Cut(card, `<div class="card-body">`)
	if !ok {
		t.Fatalf("the Current sprint card has no card-body; card=%q", card)
	}
	for _, absent := range []string{"<a ", sprintHref, "Ledger cut-over"} {
		if strings.Contains(bodyPart, absent) {
			t.Errorf("the Current sprint card body carries %q; the sprint link lives in the card title; body=%q", absent, bodyPart)
		}
	}

	// The body opens with the progress row, the completion text at its leading
	// edge and the start text in its ms-auto child, immediately followed by the
	// status breakdown; no element of the card carries a style attribute.
	progressRow := regexp.MustCompile(`^\s*<div class="d-flex mb-2"><div>16% complete</div>` +
		`<div class="ms-auto">Started <time datetime="` + regexp.QuoteMeta(f.startedAt) + `">[^<]+</time>, 3 days elapsed</div></div>` +
		`\s*<div class="progress-stacked mb-2" data-role="sprint-breakdown">`)
	if !progressRow.MatchString(bodyPart) {
		t.Errorf("the card body does not open with the progress row followed by the breakdown; body=%q", bodyPart)
	}
	if strings.Contains(card, "style=") {
		t.Errorf("the Current sprint card carries a style attribute; card=%q", card)
	}
	for _, gone := range []string{"text-secondary mb-1", `data-role="current-sprint-started"`, `data-role="current-sprint-complete"`} {
		if strings.Contains(card, gone) {
			t.Errorf("the Current sprint card still carries the separate line %q", gone)
		}
	}

	// In order: the progress row, the breakdown, the legend, and the capacity
	// line.
	order := []string{
		`<div class="d-flex mb-2">`,
		`data-role="sprint-breakdown">`,
		`data-role="sprint-breakdown-legend">`,
		">Capacity 5/8<",
	}
	last := -1
	for _, want := range order {
		i := strings.Index(bodyPart, want)
		if i < 0 {
			t.Errorf("the Current sprint card body lacks %q; body=%q", want, bodyPart)
			continue
		}
		if i < last {
			t.Errorf("%q is out of order in the Current sprint card body", want)
		}
		last = i
	}

	segRe := regexp.MustCompile(`(?s)<div class="progress" role="progressbar" aria-label="([A-Z]+) (\d+)" aria-valuemin="0" aria-valuemax="100" aria-valuenow="([0-9.]+)" data-percent="([0-9.]+)">\s*<div class="progress-bar ([a-z-]+)"></div>`)
	segMatches := segRe.FindAllStringSubmatch(card, -1)
	segs := make([]string, 0, len(segMatches))
	for _, m := range segMatches {
		if m[3] != m[4] {
			t.Errorf("segment %s: aria-valuenow %s differs from data-percent %s", m[1], m[3], m[4])
		}
		segs = append(segs, m[1]+" "+m[2]+" "+m[4]+" "+m[5])
	}
	wantSegs := []string{"SPRINT 2 33.33 bg-cyan", "DOING 2 33.33 bg-blue", "TESTING 1 16.67 bg-yellow", "COMPLETED 1 16.67 bg-green"}
	if strings.Join(segs, "|") != strings.Join(wantSegs, "|") {
		t.Errorf("the status breakdown reads %q, want %q", segs, wantSegs)
	}
	if !strings.Contains(card, `<div class="progress-stacked mb-2" data-role="sprint-breakdown">`) {
		t.Errorf("the breakdown is not a progress-stacked bar")
	}

	// The Tabler 1.6.1 legend list; the deprecated empty <span class="legend">
	// dot appears nowhere on the page.
	if !strings.Contains(card, `<div class="legend-list mb-2" data-role="sprint-breakdown-legend">`) {
		t.Errorf("the legend is not a Tabler legend-list")
	}
	legendRe := regexp.MustCompile(`<span class="legend (legend-[a-z]+)"><span class="legend-dot"></span>([A-Z]+)<span class="legend-value">(\d+)</span></span>`)
	legendMatches := legendRe.FindAllStringSubmatch(card, -1)
	legend := make([]string, 0, len(legendMatches))
	for _, m := range legendMatches {
		legend = append(legend, m[2]+" "+m[3]+" "+m[1])
	}
	wantLegend := []string{"SPRINT 2 legend-cyan", "DOING 2 legend-blue", "TESTING 1 legend-yellow", "COMPLETED 1 legend-green"}
	if strings.Join(legend, "|") != strings.Join(wantLegend, "|") {
		t.Errorf("the legend reads %q, want %q", legend, wantLegend)
	}
	if regexp.MustCompile(`<span class="legend(?: [^"]*)?"></span>`).MatchString(body) {
		t.Errorf("the page carries an empty legend span")
	}

	// The card holds no chart container: no burndown, no chart-lg, no drawing.
	for _, absent := range []string{"chart-", `role="img"`, "Burndown", "burndown", `<div class="empty">`} {
		if strings.Contains(card, absent) {
			t.Errorf("the Current sprint card carries %q; it draws no chart", absent)
		}
	}

	for _, absent := range []string{"deadline", "Deadline", "end date", "End date", "remaining<", "days remaining", "Days remaining"} {
		if strings.Contains(card, absent) {
			t.Errorf("the Current sprint card states %q; a sprint carries no end date", absent)
		}
	}

	// With max_tasks unset, no capacity line.
	rw := openDashRoadmap(t, f.name)
	if _, err := rw.Exec(`UPDATE sprints SET max_tasks = NULL WHERE id = ?`, f.openID); err != nil {
		t.Fatalf("clearing max_tasks: %v", err)
	}
	if card := dashCard(t, dashboardBody(t, f.name, dashNow), "current-sprint"); strings.Contains(card, "Capacity") {
		t.Errorf("the card renders a capacity line with max_tasks unset")
	}
}

// TestDashboard_CurrentSprintTitleIsEscaped is the escaping half of Acceptance
// Criterion 269: the sprint's title in the card title is the roadmap's data and
// is HTML-escaped inside the link, never interpreted as markup.
func TestDashboard_CurrentSprintTitleIsEscaped(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	rw := openDashRoadmap(t, f.name)
	const title = "Ledger <cut-over> & FX revaluation"
	if _, err := rw.Exec(`UPDATE sprints SET title = ? WHERE id = ?`, title, f.openID); err != nil {
		t.Fatalf("renaming the open sprint: %v", err)
	}
	card := dashCard(t, dashboardBody(t, f.name, dashNow), "current-sprint")
	want := `<h3 class="card-title">Current sprint: <a href="/roadmaps/` + f.name + `/sprints/` + itoa(f.openID) +
		`" data-role="current-sprint-link">Ledger &lt;cut-over&gt; &amp; FX revaluation</a></h3>`
	if !strings.Contains(card, want) {
		t.Errorf("the Current sprint card title is not %q; card=%q", want, card)
	}
	if strings.Contains(card, "<cut-over>") || strings.Contains(card, title) {
		t.Errorf("the sprint title reaches the card unescaped; card=%q", card)
	}
}

// TestDashboard_NoOpenSprintShowsAnEmptyState is Acceptance Criterion 270.
func TestDashboard_NoOpenSprintShowsAnEmptyState(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	database := openDashRoadmap(t, "treasury-ops")
	ids := addDashTasks(t, database, []dashTask{
		{title: "Reconcile the treasury positions", typ: models.TypeTask, status: models.StatusSprint, severity: 3, priority: 3},
	})
	pending := addDashSprint(t, database, "Treasury close", 1, ids)
	body := dashboardBody(t, "treasury-ops", dashNow)
	card := dashCard(t, body, "current-sprint")
	header := regionBetween(t, card, `<div class="card-header">`, `<div class="card-body">`)
	if !strings.Contains(header, `<h3 class="card-title">Current sprint</h3>`) || strings.Contains(header, "<a ") ||
		strings.Contains(header, "Treasury close") || strings.Contains(header, "/sprints/") {
		t.Errorf("with no OPEN sprint the card title is not exactly \"Current sprint\" without a link; header=%q", header)
	}
	for _, want := range []string{
		`<div class="empty">`,
		`<p class="empty-title">No open sprint</p>`,
		`<p class="empty-subtitle text-secondary">Sprints are started with the CLI, rmp sprint start.</p>`,
		`<div class="empty-action">`,
		`<a class="btn btn-primary" href="/roadmaps/treasury-ops/sprints">View sprints</a>`,
	} {
		if !strings.Contains(card, want) {
			t.Errorf("the no-open-sprint card lacks %q; card=%q", want, card)
		}
	}
	if strings.Contains(body, "progress-stacked") || strings.Contains(card, "chart-") || strings.Contains(card, `role="img"`) {
		t.Errorf("the page renders a status breakdown or a chart container with no sprint OPEN")
	}

	// An OPEN sprint with no completed member: the completion line, the status
	// breakdown, and the legend, and no empty state.
	setDashSprint(t, database, pending, models.SprintOpen, at(dashNow.Add(-48*time.Hour)), nil, nil)
	body = dashboardBody(t, "treasury-ops", dashNow)
	card = dashCard(t, body, "current-sprint")
	for _, want := range []string{
		`<h3 class="card-title">Current sprint: <a href="/roadmaps/treasury-ops/sprints/` + itoa(pending) +
			`" data-role="current-sprint-link">Treasury close</a></h3>`,
		`<div class="d-flex mb-2"><div>0% complete</div><div class="ms-auto">Started <time`,
		"</time>, 2 days elapsed</div></div>",
		`<div class="progress-stacked mb-2" data-role="sprint-breakdown">`,
		`aria-label="SPRINT 1" aria-valuemin="0" aria-valuemax="100" aria-valuenow="100" data-percent="100"`,
		`<div class="legend-list mb-2" data-role="sprint-breakdown-legend">`,
		`<span class="legend legend-cyan"><span class="legend-dot"></span>SPRINT<span class="legend-value">1</span></span>`,
		`<span class="legend legend-green"><span class="legend-dot"></span>COMPLETED<span class="legend-value">0</span></span>`,
	} {
		if !strings.Contains(card, want) {
			t.Errorf("the open sprint with no completion lacks %q; card=%q", want, card)
		}
	}
	for _, absent := range []string{`<div class="empty">`, "No completed task yet", "chart-", `role="img"`} {
		if strings.Contains(card, absent) {
			t.Errorf("the open sprint with no completion carries %q; card=%q", absent, card)
		}
	}

	// An OPEN sprint whose started_at is unset reads "Started —" and no days.
	if _, err := database.Exec(`UPDATE sprints SET started_at = NULL WHERE id = ?`, pending); err != nil {
		t.Fatalf("clearing started_at: %v", err)
	}
	card = dashCard(t, dashboardBody(t, "treasury-ops", dashNow), "current-sprint")
	if !strings.Contains(card, `<div class="d-flex mb-2"><div>0% complete</div><div class="ms-auto">Started &mdash;</div></div>`) ||
		strings.Contains(card, "elapsed") {
		t.Errorf("an unset started_at does not read \"Started —\" with no elapsed days; card=%q", card)
	}
}

// ==================== AC 271, 272, AND 273: THE DONUT DATA AND THE LAYOUT ====================

// TestDashboard_DonutDataCountsPerType is Acceptance Criterion 271 and
// SPEC/DATA_FORMATS.md § Dashboard Chart Data, rules 2 and 3.
func TestDashboard_DonutDataCountsPerType(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	c := dashCharts(t, dashboardBody(t, f.name, dashNow))

	byType, _ := json.Marshal(c.TasksByType)
	if string(byType) != `{"types":["TASK","BUG","CHORE"],"open":[4,3,1],"completed":[2,1,1],"backlog":[2,0,1]}` {
		t.Errorf("tasks_by_type = %s", byType)
	}

	// Every type in enum order, each with its own per-card counts, and a type
	// whose every task is COMPLETED keeps zero open and backlog entries.
	database := openDashRoadmap(t, "treasury-ops")
	tasks := []dashTask{
		{title: "Close the treasury books", typ: models.TypeSpike, status: models.StatusBacklog},
		{title: "Forecast the quarter-end cash position", typ: models.TypeUserStory, status: models.StatusDoing, started: at(dashNow.Add(-time.Hour))},
		{title: "Model the intraday liquidity buffer", typ: models.TypeSpike, status: models.StatusSprint},
		{title: "Treasury workstation redesign", typ: models.TypeDesignUX, status: models.StatusCompleted, closed: at(dashNow.Add(-time.Hour))},
		{title: "Faster bank statement import", typ: models.TypeImprovement, status: models.StatusBacklog},
		{title: "Split the cash pooling service", typ: models.TypeRefactor, status: models.StatusTesting, started: at(dashNow.Add(-time.Hour))},
	}
	setDashTaskStates(t, database, addDashTasks(t, database, tasks), tasks)
	c = dashCharts(t, dashboardBody(t, "treasury-ops", dashNow))
	byType, _ = json.Marshal(c.TasksByType)
	if string(byType) != `{"types":["USER_STORY","REFACTOR","SPIKE","DESIGN_UX","IMPROVEMENT"],`+
		`"open":[1,1,2,0,1],"completed":[0,0,0,1,0],"backlog":[0,0,1,0,1]}` {
		t.Errorf("tasks_by_type = %s", byType)
	}
	for i := range c.TasksByType.Types {
		if c.TasksByType.Backlog[i] > c.TasksByType.Open[i] {
			t.Errorf("type %s: backlog %d exceeds open %d", c.TasksByType.Types[i], c.TasksByType.Backlog[i], c.TasksByType.Open[i])
		}
	}
}

// TestDashboard_CurrentSprintTakesTheFullWidth is the markup half of Acceptance
// Criterion 272: the Current sprint card stands alone in a col-12, and the next
// row holds Work in progress and Highest-severity open tasks, each in a
// col-lg-6. The removed blocks are gone and the page holds no chart other than
// the three donuts. The 1440px measurement is a browser's.
func TestDashboard_CurrentSprintTakesTheFullWidth(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	body := dashboardBody(t, f.name, dashNow)
	main := regionBetween(t, body, `<main class="page-body">`, `</main>`)

	assertOrdered(t, "the grid", main, []string{
		`<div class="col-sm-6 col-lg-3">` + "\n" + `              <div class="card" data-role="kpi-velocity">`,
		`<div class="col-12">` + "\n" + `              <div class="card" data-role="current-sprint">`,
		`<div class="col-lg-6">` + "\n" + `              <div class="card" data-role="work-in-progress">`,
		`<div class="col-lg-6">` + "\n" + `              <div class="card" data-role="most-severe">`,
	})
	// Directly adjacent: nothing stands between the KPI row and Current sprint,
	// nor between Current sprint and Work in progress.
	cols := dashGridCols(main)
	if len(cols) < 7 || cols[4] != "col-12" || cols[5] != "col-lg-6" || cols[6] != "col-lg-6" {
		t.Errorf("the grid's columns after the KPI row are %q", cols)
	}
	for _, absent := range []string{
		"tasks-by-type", "tasks-by-status", "tasks-per-sprint", "Tasks by type", "Tasks by status",
		"Tasks per sprint", "No started sprint yet", "No tasks yet", "tasks_by_status", "tasks_per_sprint",
	} {
		if strings.Contains(body, absent) {
			t.Errorf("the page still carries %q", absent)
		}
	}

	chartMatches := dashChartRe.FindAllStringSubmatch(main, -1)
	charts := make([]string, 0, len(chartMatches))
	for _, m := range chartMatches {
		charts = append(charts, m[1])
	}
	want := []string{"chart-open-by-type", "chart-completed-by-type", "chart-backlog-by-type"}
	if strings.Join(charts, ",") != strings.Join(want, ",") {
		t.Errorf("the page's charts are %v, want %v", charts, want)
	}

	// The page script draws no removed chart.
	js := stripJSComments(readEmbeddedAsset(t, "static/dashboard.js"))
	for _, absent := range []string{"tasks_by_status", "tasks_per_sprint", "chart-tasks-", `"bar"`, "stacked", "STATUS_COLOURS"} {
		if strings.Contains(js, absent) {
			t.Errorf("dashboard.js still carries %q", absent)
		}
	}
}

// TestDashboard_ZeroValueKPICardDrawsNoDonut is Acceptance Criterion 273.
func TestDashboard_ZeroValueKPICardDrawsNoDonut(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	database := openDashRoadmap(t, "card-issuing")
	done := func(title string, typ models.TaskType, daysAgo int) dashTask {
		return dashTask{title: title, typ: typ, status: models.StatusCompleted, closed: at(dashNow.AddDate(0, 0, -daysAgo))}
	}
	tasks := []dashTask{
		done("Issue virtual cards", models.TypeTask, 40),
		done("Tokenise the PAN", models.TypeTask, 30),
		done("Card controls screen", models.TypeTask, 20),
		done("Declined contactless payments abroad", models.TypeBug, 15),
		done("PIN change not propagated to the processor", models.TypeBug, 10),
	}
	setDashTaskStates(t, database, addDashTasks(t, database, tasks), tasks)
	body := dashboardBody(t, "card-issuing", dashNow)

	for _, role := range []string{"kpi-open", "kpi-backlog"} {
		card := dashCard(t, body, role)
		if !strings.Contains(card, `<div class="h1 mb-3">0</div>`) {
			t.Errorf("%s does not read 0; card=%q", role, card)
		}
		if !strings.Contains(card, dashKPIRow) || !strings.Contains(card, `<div class="col">`) {
			t.Errorf("%s lost its row or its col; card=%q", role, card)
		}
		for _, absent := range []string{`<div class="col-auto">`, `role="img"`, "-by-type", `<div class="empty">`} {
			if strings.Contains(card, absent) {
				t.Errorf("%s, whose value is 0, carries %q; card=%q", role, absent, card)
			}
		}
	}
	completed := dashCard(t, body, "kpi-completed")
	if !strings.Contains(completed, `<div class="col-auto">`) {
		t.Errorf("the Completed card holds no donut column; card=%q", completed)
	}
	if got := dashAriaLabels(body)["chart-completed-by-type"]; got != "Completed tasks by type: TASK 3 (60%), BUG 2 (40%)" {
		t.Errorf("the Completed donut's aria-label is %q", got)
	}
	if n := len(dashDonutRe.FindAllString(body, -1)); n != 1 {
		t.Errorf("%d donut containers, want 1", n)
	}
	byType, _ := json.Marshal(dashCharts(t, body).TasksByType)
	if string(byType) != `{"types":["TASK","BUG"],"open":[0,0],"completed":[3,2],"backlog":[0,0]}` {
		t.Errorf("tasks_by_type = %s", byType)
	}
}

// ==================== AC 286: THE KPI TYPE-BREAKDOWN DONUTS ====================

// TestDashboard_KPICardsCarryTypeDonuts is the markup and source half of
// Acceptance Criterion 286; the drawn half — 64 px square, no legend, the
// tooltip text on hover — is a browser's.
func TestDashboard_KPICardsCarryTypeDonuts(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	body := dashboardBody(t, f.name, dashNow)

	cardRe := func(label, role string) *regexp.Regexp {
		return regexp.MustCompile(`(?s)^\s*<div class="card-body">\s*` + regexp.QuoteMeta(dashKPIRow) +
			`\s*<div class="col">\s*<div class="subheader">` + label + `</div>\s*<div class="h1 mb-3">\d+</div>.*?` +
			`</div>\s*<div class="col-auto">\s*<div id="chart-` + role + `-by-type" role="img" aria-label="[^"]*"></div>\s*</div>\s*</div>\s*</div>\s*</div>\s*</div>\s*<div class="col-sm-6 col-lg-3">\s*$`)
	}
	for _, c := range []struct{ role, label, chart string }{
		{"kpi-open", "Open tasks", "open"},
		{"kpi-completed", "Completed", "completed"},
		{"kpi-backlog", "Backlog", "backlog"},
	} {
		card := dashCard(t, body, c.role)
		if !cardRe(c.label, c.chart).MatchString(card) {
			t.Errorf("%s is not a row of a col (label, value, subline) and a col-auto holding its donut; card=%q", c.role, card)
		}
		if strings.Count(card, `role="img"`) != 1 {
			t.Errorf("%s holds %d donut containers, want 1", c.role, strings.Count(card, `role="img"`))
		}
	}
	velocity := dashCard(t, body, "kpi-velocity")
	for _, absent := range []string{"col-auto", `role="img"`, dashKPIRow} {
		if strings.Contains(velocity, absent) {
			t.Errorf("the Average velocity card carries %q", absent)
		}
	}
	// The open card's subline stays in the col, after the value.
	assertOrdered(t, "the Open tasks card", dashCard(t, body, "kpi-open"), []string{
		`<div class="col">`, `<div class="subheader">Open tasks</div>`, `<div class="h1 mb-3">8</div>`,
		">3 in progress, 1 blocked<", `<div class="col-auto">`,
	})

	labels := dashAriaLabels(body)
	for id, want := range map[string]string{
		"chart-open-by-type":      "Open tasks by type: TASK 4 (50%), BUG 3 (37%), CHORE 1 (12%)",
		"chart-completed-by-type": "Completed tasks by type: TASK 2 (50%), BUG 1 (25%), CHORE 1 (25%)",
		"chart-backlog-by-type":   "Backlog tasks by type: TASK 2 (66%), CHORE 1 (33%)",
	} {
		if labels[id] != want {
			t.Errorf("%s aria-label = %q, want %q", id, labels[id], want)
		}
	}
	// The containers, in card order, carry no class and no style attribute.
	donuts := dashDonutRe.FindAllStringSubmatch(body, -1)
	if len(donuts) != 3 || donuts[0][1] != "chart-open-by-type" || donuts[1][1] != "chart-completed-by-type" ||
		donuts[2][1] != "chart-backlog-by-type" {
		t.Errorf("the donut containers are %v", donuts)
	}

	// The script: a donut per card from its array, 64 by 64, sparkline, no
	// legend, no data labels, the tooltip writing <n> (<p>%) with p floored.
	js := strings.Join(strings.Fields(stripJSComments(readEmbeddedAsset(t, "static/dashboard.js"))), " ")
	for _, want := range []string{
		`var DONUT_SIZE = 64;`,
		`type: "donut", height: DONUT_SIZE, width: DONUT_SIZE, fontFamily: "inherit", parentHeightOffset: 0, sparkline: { enabled: true }, toolbar: { show: false }, animations: { enabled: false }`,
		`legend: { show: false }, dataLabels: { enabled: false }, tooltip: { fillSeriesColor: false,`,
		`return value + " (" + Math.floor(value * 100 / total) + "%)";`,
		`if (counts[i] > 0) { labels.push(types[i]); series.push(counts[i]); colours.push(TYPE_COLOURS[types[i]]);`,
		`draw("chart-open-by-type", typeDonutOptions(byType.types, byType.open));`,
		`draw("chart-completed-by-type", typeDonutOptions(byType.types, byType.completed));`,
		`draw("chart-backlog-by-type", typeDonutOptions(byType.types, byType.backlog));`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("dashboard.js lacks %q", want)
		}
	}
	if regexp.MustCompile(`width:\s*"[0-9.]+%"`).MatchString(js) {
		t.Errorf("a chart width is a percentage")
	}

	// Each type's slice colour is the solid counterpart of its badge variant,
	// var(--tblr-<colour>) for bg-<colour>-lt, for every type of the enum.
	block := regexp.MustCompile(`var TYPE_COLOURS = \{(.*?)\};`).FindStringSubmatch(js)
	if block == nil {
		t.Fatal("dashboard.js declares no TYPE_COLOURS")
	}
	entries := regexp.MustCompile(`([A-Z_]+): "([^"]+)"`).FindAllStringSubmatch(block[1], -1)
	if len(entries) != len(models.ValidTaskTypes) {
		t.Errorf("TYPE_COLOURS holds %d entries, want %d", len(entries), len(models.ValidTaskTypes))
	}
	colours := map[string]string{}
	for _, e := range entries {
		colours[e[1]] = e[2]
	}
	for _, typ := range models.ValidTaskTypes {
		badge := taskTypeBadge(typ)
		want := "var(--tblr-" + strings.TrimSuffix(strings.TrimPrefix(badge, "bg-"), "-lt") + ")"
		if colours[string(typ)] != want {
			t.Errorf("TYPE_COLOURS[%s] = %q, want %q, the solid counterpart of %s", typ, colours[string(typ)], want, badge)
		}
	}
}

// TestDashboard_TypeDonutLabel pins the donut label's derivation: the slices
// whose count is greater than 0, in type order, each with its floored share of
// the card's value, and no label for a value of 0.
func TestDashboard_TypeDonutLabel(t *testing.T) {
	types := []models.TaskType{models.TypeTask, models.TypeBug, models.TypeChore}
	for _, tc := range []struct {
		counts []int
		total  int
		want   string
	}{
		{[]int{2, 0, 1}, 3, "Backlog tasks by type: TASK 2 (66%), CHORE 1 (33%)"},
		{[]int{0, 0, 0}, 0, ""},
		{[]int{0, 7, 0}, 7, "Backlog tasks by type: BUG 7 (100%)"},
	} {
		if got := typeDonutLabel("Backlog tasks by type: ", types, tc.counts, tc.total); got != tc.want {
			t.Errorf("typeDonutLabel(%v, %d) = %q, want %q", tc.counts, tc.total, got, tc.want)
		}
	}
}

// ==================== AC 274: NO THROUGHPUT, NO BURNDOWN ====================

// TestDashboard_NoThroughputAndNoBurndown is the page half of Acceptance
// Criterion 274, on a roadmap with an OPEN sprint holding a completed member and
// on one with no sprint OPEN: no Throughput card, no chart-lg, no chart in the
// Current sprint card, and a chart data block holding tasks_by_type alone. The
// statement counts (eight and nine) are measured in internal/db at the driver
// boundary.
func TestDashboard_NoThroughputAndNoBurndown(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	database := openDashRoadmap(t, "acquirer-feed")
	tasks := []dashTask{
		{title: "Parse the acquirer settlement file", typ: models.TypeTask, status: models.StatusCompleted,
			created: at(dashNow.AddDate(0, 0, -20)), closed: at(dashNow.AddDate(0, 0, -2))},
		{title: "Alert on a missing acquirer file", typ: models.TypeBug, status: models.StatusDoing,
			created: at(dashNow.AddDate(0, 0, -10)), started: at(dashNow.AddDate(0, 0, -1))},
	}
	setDashTaskStates(t, database, addDashTasks(t, database, tasks), tasks)

	for _, name := range []string{f.name, "acquirer-feed"} {
		body := dashboardBody(t, name, dashNow)
		main := regionBetween(t, body, `<main class="page-body">`, `</main>`)
		for _, absent := range []string{
			`<h3 class="card-title">Throughput</h3>`, `data-role="throughput"`, "chart-lg",
			"chart-burndown", "chart-throughput", "No activity in the last 12 weeks", "No completed task yet",
		} {
			if strings.Contains(main, absent) {
				t.Errorf("%s: the dashboard carries %q", name, absent)
			}
		}
		for _, m := range dashChartRe.FindAllStringSubmatch(main, -1) {
			if !strings.HasSuffix(m[1], "-by-type") {
				t.Errorf("%s: the page holds the chart container %s, not a KPI donut", name, m[1])
			}
		}
		raw := dashDataRe.FindStringSubmatch(body)[1]
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			t.Fatalf("%s: the chart data block is not a JSON object: %v", name, err)
		}
		if _, ok := obj["tasks_by_type"]; len(obj) != 1 || !ok {
			t.Errorf("%s: the chart data block is %s, want tasks_by_type alone", name, raw)
		}
	}
	// The fixture's OPEN sprint has a completed member, so a burndown would have
	// had data: its absence is not an empty-series artefact.
	if !strings.Contains(dashCard(t, dashboardBody(t, f.name, dashNow), "current-sprint"), "COMPLETED 1") {
		t.Fatalf("the fixture's OPEN sprint holds no completed member; the check above would be vacuous")
	}
}

// ==================== AC 275 AND 276: THE TWO TASK TABLES ====================

// TestDashboard_WorkInProgressOldestFirst is Acceptance Criterion 275.
func TestDashboard_WorkInProgressOldestFirst(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	database := openDashRoadmap(t, "merchant-onboarding")
	// Twelve tasks in progress. The ages of the criterion, a tie on started_at
	// broken by id, and older ones past the cap of ten.
	starts := []time.Duration{
		5 * time.Hour, 30 * time.Hour, (7*24 + 2) * time.Hour, 10 * 24 * time.Hour, 10 * 24 * time.Hour,
		2 * time.Hour, 3 * time.Hour, 4 * time.Hour, 6 * time.Hour, 7 * time.Hour, time.Hour, 30 * time.Minute,
	}
	tasks := make([]dashTask, 0, len(starts)+3)
	for i, d := range starts {
		status := models.StatusDoing
		if i%3 == 0 {
			status = models.StatusTesting
		}
		tasks = append(tasks, dashTask{title: "Verify merchant document set " + itoa(i+1), typ: models.TypeTask,
			status: status, started: at(dashNow.Add(-d))})
	}
	tasks = append(tasks,
		dashTask{title: "Collect the merchant tax form", typ: models.TypeTask, status: models.StatusBacklog},
		dashTask{title: "Screen the merchant against sanctions", typ: models.TypeTask, status: models.StatusSprint},
		dashTask{title: "Approve the merchant risk tier", typ: models.TypeTask, status: models.StatusCompleted,
			started: at(dashNow.AddDate(0, 0, -30)), closed: at(dashNow.AddDate(0, 0, -20))},
	)
	ids := addDashTasks(t, database, tasks)
	setDashTaskStates(t, database, ids, tasks)

	body := dashboardBody(t, "merchant-onboarding", dashNow)
	rows := dashRows(dashCard(t, body, "work-in-progress"))
	if len(rows) != 10 {
		t.Fatalf("the Work in progress table holds %d rows, want 10", len(rows))
	}
	// Oldest first: 10d (ids[3], ids[4] by id), 7d2h, 30h, 7h, 6h, 5h, 4h, 3h, 2h.
	order := []int{3, 4, 2, 1, 9, 8, 0, 7, 6, 5}
	for i, k := range order {
		task := tasks[k]
		id := itoa(ids[k])
		want := []string{
			`<span class="badge bg-black text-white">#` + id + `</span>`,
			`<a href="/roadmaps/merchant-onboarding/tasks/` + id + `">` + task.title + `</a>`,
			`<span class="badge ` + taskStatusBadge(task.status) + `">` + string(task.status) + `</span>`,
		}
		for j, w := range want {
			if rows[i][j] != w {
				t.Errorf("row %d cell %d = %q, want %q", i, j, rows[i][j], w)
			}
		}
	}
	ages := map[int]string{0: "10 days", 1: "10 days", 2: "7 days", 3: "1 day", 6: "0 days"}
	for i, want := range ages {
		if rows[i][3] != want {
			t.Errorf("row %d age = %q, want %q", i, rows[i][3], want)
		}
	}
	if !strings.Contains(dashCard(t, body, "work-in-progress"), `<span class="badge bg-blue-lt">DOING</span>`) ||
		!strings.Contains(dashCard(t, body, "work-in-progress"), `<span class="badge bg-yellow-lt">TESTING</span>`) {
		t.Errorf("the status badges are not coloured bg-blue-lt for DOING and bg-yellow-lt for TESTING")
	}
	for _, absent := range []string{"merchant tax form", "against sanctions", "risk tier"} {
		if strings.Contains(dashCard(t, body, "work-in-progress"), absent) {
			t.Errorf("a task not in progress (%q) is listed", absent)
		}
	}

	// A task in progress whose started_at is unset is ordered after every one
	// that is set, and reads the em dash.
	extra := addDashTasks(t, database, []dashTask{{title: "Recheck the merchant bank account", typ: models.TypeTask}})
	if _, err := database.Exec(`UPDATE tasks SET status = 'DOING', started_at = NULL WHERE id = ?`, extra[0]); err != nil {
		t.Fatalf("starting the extra task: %v", err)
	}
	if _, err := database.Exec(`UPDATE tasks SET status = 'BACKLOG' WHERE id IN (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ids[0], ids[1], ids[3], ids[4], ids[5], ids[6], ids[7], ids[8], ids[9], ids[10]); err != nil {
		t.Fatalf("returning tasks to the backlog: %v", err)
	}
	rows = dashRows(dashCard(t, dashboardBody(t, "merchant-onboarding", dashNow), "work-in-progress"))
	if len(rows) != 3 || !strings.Contains(rows[2][0], "#"+itoa(extra[0])) || rows[2][3] != absentPlaceholder {
		t.Errorf("the task with no started_at is not last with an em dash age: %q", rows)
	}
}

// TestDashboard_HighestSeverityOpenTasks is Acceptance Criterion 276.
func TestDashboard_HighestSeverityOpenTasks(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	database := openDashRoadmap(t, "fraud-scoring")
	tasks := []dashTask{
		{title: "Score drift on weekend traffic", typ: models.TypeBug, status: models.StatusBacklog, severity: 9, priority: 2},
		{title: "Chargeback spike not flagged", typ: models.TypeBug, status: models.StatusDoing, severity: 9, priority: 7, started: at(dashNow.Add(-time.Hour))},
		{title: "Velocity rule misses split payments", typ: models.TypeBug, status: models.StatusSprint, severity: 5, priority: 5},
		{title: "Device fingerprint collisions", typ: models.TypeBug, status: models.StatusBacklog, severity: 5, priority: 5},
		{title: "Tune the scoring thresholds", typ: models.TypeTask, status: models.StatusTesting, severity: 3, priority: 9, started: at(dashNow.Add(-time.Hour))},
		{title: "Rename the scoring dashboard", typ: models.TypeChore, status: models.StatusBacklog, severity: 1, priority: 1},
		{title: "Model served stale features", typ: models.TypeBug, status: models.StatusCompleted, severity: 9, priority: 9, closed: at(dashNow.Add(-time.Hour))},
	}
	ids := addDashTasks(t, database, tasks)
	setDashTaskStates(t, database, ids, tasks)

	card := dashCard(t, dashboardBody(t, "fraud-scoring", dashNow), "most-severe")
	rows := dashRows(card)
	if len(rows) != 5 {
		t.Fatalf("the table holds %d rows, want 5", len(rows))
	}
	for i, k := range []int{1, 0, 2, 3, 4} {
		task := tasks[k]
		id := itoa(ids[k])
		want := []string{
			`<span class="badge bg-black text-white">#` + id + `</span>`,
			`<a href="/roadmaps/fraud-scoring/tasks/` + id + `">` + task.title + `</a>`,
			`<span class="badge ` + severityBadge(task.severity) + `">S` + itoa(task.severity) + `</span>`,
			`<span class="badge ` + priorityBadge(task.priority) + `">P` + itoa(task.priority) + `</span>`,
		}
		for j, w := range want {
			if rows[i][j] != w {
				t.Errorf("row %d cell %d = %q, want %q", i, j, rows[i][j], w)
			}
		}
	}
	for _, absent := range []string{"stale features", "Rename the scoring"} {
		if strings.Contains(card, absent) {
			t.Errorf("%q is listed; it is COMPLETED or below the five most severe", absent)
		}
	}
}

// TestDashboard_TaskCellsEqualTheTasksPage pins "exactly as in a row of the tasks
// page": the ID, Title, Status, Severity, and Priority cells of a dashboard row
// are byte for byte the cells of the same task's row on the tasks page.
func TestDashboard_TaskCellsEqualTheTasksPage(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	body := dashboardBody(t, f.name, dashNow)
	tasksPage := servePage(t, buildMux(), "/roadmaps/"+f.name+"/tasks?size=100&status=BACKLOG&status=SPRINT&status=DOING&status=TESTING&status=COMPLETED")

	listCells := map[string][]string{}
	for _, row := range dashRows(tasksPage) {
		listCells[row[0]] = row
	}
	check := func(role string, columns map[int]int) {
		for _, row := range dashRows(dashCard(t, body, role)) {
			list, ok := listCells[row[0]]
			if !ok {
				t.Errorf("%s: the tasks page has no row for %s", role, row[0])
				continue
			}
			for dashCol, listCol := range columns {
				if row[dashCol] != list[listCol] {
					t.Errorf("%s: cell %d %q differs from the tasks page's %q", role, dashCol, row[dashCol], list[listCol])
				}
			}
		}
	}
	// Tasks page columns: ID 0, Title 1, Type 2, Status 3, Severity 4, Priority 5.
	check("work-in-progress", map[int]int{0: 0, 1: 1, 2: 3})
	check("most-severe", map[int]int{0: 0, 1: 1, 2: 4, 3: 5})
	if !strings.Contains(dashCard(t, body, "most-severe"), `<td class="task-list__title">`) {
		t.Errorf("the title cell does not carry the tasks page's title class")
	}
}

// ==================== AC 277 AND 278 ====================

// TestDashboard_RecentActivityRepeatsTheAuditHead is Acceptance Criterion 277.
func TestDashboard_RecentActivityRepeatsTheAuditHead(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmapWithAudit(t, "chargeback-desk", 23)
	database := openDashRoadmap(t, name)
	if _, err := database.Exec(`INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
		VALUES ('TASK_STATUS_BACKLOG', 'TASK', 7, 3, '9f2c4e1ab07d', '2026-02-01T10:00:00.000Z')`); err != nil {
		t.Fatalf("seeding an entry with a counterpart and a commit: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO audit (operation, entity_type, entity_id, performed_at)
		VALUES ('LEGACY_IMPORT', 'TASK', 8, '2026-02-01T11:00:00.000Z')`); err != nil {
		t.Fatalf("seeding an entry outside the catalogue: %v", err)
	}

	body := dashboardBody(t, name, dashNow)
	card := dashCard(t, body, "recent-activity")
	if !strings.Contains(card, `<a class="d-inline-block py-1" href="/roadmaps/`+name+`/audit">View audit log</a>`) {
		t.Errorf("the card header lacks the View audit log link")
	}
	if !strings.Contains(card, `<div class="table-responsive">`) || !strings.Contains(card, `<table class="table card-table table-vcenter">`) {
		t.Errorf("the card-table is not inside table-responsive")
	}
	got := dashRowRe.FindAllString(regionBetween(t, card, "<tbody>", "</tbody>"), -1)
	audit := servePage(t, buildMux(), "/roadmaps/"+name+"/audit")
	want := dashRowRe.FindAllString(regionBetween(t, audit, "<tbody>", "</tbody>"), -1)
	if len(got) != 10 || len(want) < 10 {
		t.Fatalf("the dashboard shows %d audit rows and the audit page %d; want 10 and at least 10", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("row %d differs from the audit page:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
	head := strings.Join(got, "")
	for _, wantText := range []string{"LEGACY_IMPORT", "9f2c4e1ab07d", absentPlaceholder} {
		if !strings.Contains(head, wantText) {
			t.Errorf("the head of the log lacks %q", wantText)
		}
	}
	thead := regionBetween(t, card, "<thead>", "</thead>")
	auditHead := regionBetween(t, audit, "<thead>", "</thead>")
	if strings.Join(strings.Fields(thead), "") != strings.Join(strings.Fields(auditHead), "") {
		t.Errorf("the column headings differ from the audit page's")
	}
}

// TestDashboard_UpcomingSprintsListsTheNextFive is Acceptance Criterion 278.
func TestDashboard_UpcomingSprintsListsTheNextFive(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	database := openDashRoadmap(t, "payouts")
	ids := addDashTasks(t, database, []dashTask{
		{title: "Batch payouts per currency", typ: models.TypeTask},
		{title: "Retry failed payouts", typ: models.TypeTask},
		{title: "Payout statement export", typ: models.TypeTask},
	})
	orders := []int{40, 10, 70, 20, 60, 30, 50}
	pending := map[int]int{}
	for _, order := range orders {
		var members []int
		if order == 10 {
			members = ids[:2]
		}
		pending[order] = addDashSprint(t, database, "Payouts wave "+itoa(order), order, members)
	}
	open := addDashSprint(t, database, "Payouts wave 5", 5, ids[2:])
	closed := addDashSprint(t, database, "Payouts wave 1", 1, nil)
	setDashSprint(t, database, open, models.SprintOpen, at(dashNow.AddDate(0, 0, -1)), nil, nil)
	setDashSprint(t, database, closed, models.SprintClosed, at(dashNow.AddDate(0, 0, -9)), at(dashNow.AddDate(0, 0, -2)), nil)

	rows := dashRows(dashCard(t, dashboardBody(t, "payouts", dashNow), "upcoming-sprints"))
	if len(rows) != 5 {
		t.Fatalf("the table holds %d rows, want 5", len(rows))
	}
	for i, order := range []int{10, 20, 30, 40, 50} {
		id := itoa(pending[order])
		if want := `<a class="d-inline-block py-1" href="/roadmaps/payouts/sprints/` + id + `">Payouts wave ` + itoa(order) + `</a>`; rows[i][0] != want {
			t.Errorf("row %d sprint cell = %q, want %q", i, rows[i][0], want)
		}
		wantCount := "0"
		if order == 10 {
			wantCount = "2"
		}
		if rows[i][1] != wantCount {
			t.Errorf("row %d task count = %q, want %s", i, rows[i][1], wantCount)
		}
	}
}

// ==================== AC 279: THE EMPTY ROADMAP ====================

// TestDashboard_EmptyRoadmapRendersEmptyStates is Acceptance Criterion 279.
func TestDashboard_EmptyRoadmapRendersEmptyStates(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	openDashRoadmap(t, "greenfield")
	body := dashboardBody(t, "greenfield", dashNow)

	for role, want := range map[string][]string{
		"kpi-open":      {`<div class="h1 mb-3">0</div>`, ">0 in progress, 0 blocked<"},
		"kpi-completed": {`<div class="h1 mb-3">0</div>`, "<div>0 of 0 tasks</div>", `<div class="ms-auto">0%</div>`, `value="0" max="1"`},
		"kpi-backlog":   {`<div class="h1 mb-3">0</div>`},
		"kpi-velocity":  {`<div class="h1 mb-3">0.0</div>`},
	} {
		card := dashCard(t, body, role)
		for _, w := range want {
			if !strings.Contains(card, w) {
				t.Errorf("%s lacks %q; card=%q", role, w, card)
			}
		}
	}
	empties := map[string][2]string{
		"current-sprint":   {"Current sprint", "No open sprint"},
		"work-in-progress": {"Work in progress", "No task in progress"},
		"most-severe":      {"Highest-severity open tasks", "No open task"},
		"recent-activity":  {"Recent activity", "No activity yet"},
		"upcoming-sprints": {"Upcoming sprints", "No upcoming sprint"},
	}
	for role, texts := range empties {
		card := dashCard(t, body, role)
		if !strings.Contains(card, `<h3 class="card-title">`+texts[0]+`</h3>`) {
			t.Errorf("%s lost its header", role)
		}
		if !strings.Contains(card, `<div class="empty">`) || !strings.Contains(card, `<p class="empty-title">`+texts[1]+`</p>`) {
			t.Errorf("%s does not show the empty state %q", role, texts[1])
		}
	}
	if !strings.Contains(dashCard(t, body, "most-severe"), "Tasks are created with the CLI, rmp task create.") {
		t.Errorf("the no-open-task subtitle of a roadmap with no task is not the create hint")
	}
	if strings.Contains(body, "chart-lg") || dashChartRe.MatchString(body) || strings.Contains(body, "<table") {
		t.Errorf("the empty roadmap renders a chart container or a table")
	}
	// No KPI type-breakdown donut: each of the three cards keeps its row and its
	// col, and holds no col-auto, no container, and no empty state.
	for _, role := range []string{"kpi-open", "kpi-completed", "kpi-backlog"} {
		card := dashCard(t, body, role)
		if !strings.Contains(card, dashKPIRow) || !strings.Contains(card, `<div class="col">`) {
			t.Errorf("%s lost its row or its col", role)
		}
		if strings.Contains(card, `<div class="col-auto">`) || strings.Contains(card, `role="img"`) || strings.Contains(card, `<div class="empty">`) {
			t.Errorf("%s of an empty roadmap carries a donut or an empty state; card=%q", role, card)
		}
	}
	if strings.Contains(body, `class="col-auto"`) || strings.Contains(body, "-by-type") {
		t.Errorf("the empty roadmap renders a KPI type-breakdown donut")
	}

	c := dashCharts(t, body)
	raw := dashDataRe.FindStringSubmatch(body)[1]
	if strings.Contains(raw, "null") {
		t.Errorf("the chart data block carries null: %s", raw)
	}
	byType, _ := json.Marshal(c.TasksByType)
	if string(byType) != `{"types":[],"open":[],"completed":[],"backlog":[]}` ||
		raw != `{"tasks_by_type":{"types":[],"open":[],"completed":[],"backlog":[]}}` {
		t.Errorf("the empty roadmap's chart data is %s", raw)
	}

	// Every task completed: the no-open-task subtitle says so.
	database := openDashRoadmap(t, "greenfield")
	done := []dashTask{{title: "Register the company", typ: models.TypeTask, status: models.StatusCompleted, closed: at(dashNow.Add(-time.Hour))}}
	setDashTaskStates(t, database, addDashTasks(t, database, done), done)
	if card := dashCard(t, dashboardBody(t, "greenfield", dashNow), "most-severe"); !strings.Contains(card, "Every task of this roadmap is completed.") {
		t.Errorf("the no-open-task subtitle of a completed roadmap is wrong; card=%q", card)
	}
}

// ==================== AC 280: THE CHART DATA BLOCK ====================

// TestDashboard_ChartDataBlock is Acceptance Criterion 280 and
// SPEC/DATA_FORMATS.md § Dashboard Chart Data.
func TestDashboard_ChartDataBlock(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	body := dashboardBody(t, f.name, dashNow)

	blocks := dashDataRe.FindAllStringSubmatch(body, -1)
	if len(blocks) != 1 {
		t.Fatalf("%d chart data blocks, want exactly 1", len(blocks))
	}
	raw := blocks[0][1]

	// The members, in order, and their types.
	dec := json.NewDecoder(strings.NewReader(raw))
	var top []string
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		t.Fatalf("the block is not a JSON object: %v", err)
	}
	if tok, _ := dec.Token(); tok != json.Delim('{') {
		t.Fatalf("the block does not open an object")
	}
	for dec.More() {
		key, _ := dec.Token()
		top = append(top, key.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatalf("decoding member %v: %v", key, err)
		}
	}
	if strings.Join(top, ",") != "tasks_by_type" {
		t.Errorf("the members are %v", top)
	}
	members := map[string]string{
		"tasks_by_type": "types,open,completed,backlog",
	}
	for member, keys := range members {
		d := json.NewDecoder(bytes.NewReader(obj[member]))
		_, _ = d.Token()
		var got []string
		for d.More() {
			k, _ := d.Token()
			got = append(got, k.(string))
			var skip json.RawMessage
			_ = d.Decode(&skip)
		}
		if strings.Join(got, ",") != keys {
			t.Errorf("%s holds %v, want %s", member, got, keys)
		}
	}
	c := dashCharts(t, body)
	if regexp.MustCompile(`\d\.\d`).MatchString(strings.ReplaceAll(raw, "-", "")) {
		t.Errorf("a number of the block is not an integer: %s", raw)
	}
	if regexp.MustCompile(`T\d\d:\d\d|\d{4}-\d\d-\d\d`).MatchString(raw) {
		t.Errorf("the block carries a date or a timestamp: %s", raw)
	}

	// The values equal the figures the page states.
	labels := dashAriaLabels(body)
	sum := func(v []int) int {
		n := 0
		for _, x := range v {
			n += x
		}
		return n
	}
	byType := &c.TasksByType
	open, completed, backlog := sum(byType.Open), sum(byType.Completed), sum(byType.Backlog)
	if labels["chart-open-by-type"] != typeDonutLabel("Open tasks by type: ", byType.Types, byType.Open, open) ||
		labels["chart-completed-by-type"] != typeDonutLabel("Completed tasks by type: ", byType.Types, byType.Completed, completed) ||
		labels["chart-backlog-by-type"] != typeDonutLabel("Backlog tasks by type: ", byType.Types, byType.Backlog, backlog) ||
		len(labels) != 3 {
		t.Errorf("an aria-label does not state the block's values: %v", labels)
	}
	// Each donut's slices sum to its card's value.
	for role, value := range map[string]int{"kpi-open": open, "kpi-completed": completed, "kpi-backlog": backlog} {
		if !strings.Contains(dashCard(t, body, role), `<div class="h1 mb-3">`+itoa(value)+`</div>`) {
			t.Errorf("the %s value disagrees with tasks_by_type, whose entries sum to %d", role, value)
		}
	}

	// No free text, no closing tag, nothing unescaped.
	for _, forbidden := range []string{dashScriptTitle, dashMarkupTitle, "</script", "<", ">", "&", "alert", "Ledger cut-over"} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("the chart data block contains %q", forbidden)
		}
	}
	// The titles themselves are on the page, escaped.
	if !strings.Contains(body, "A &amp; B &lt;C&gt;") || !strings.Contains(body, "&lt;/script&gt;&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("the fixture's titles are not on the page; the absence check above would be vacuous")
	}
}

// ==================== AC 281: NO INLINE SCRIPT, NO INLINE STYLE ====================

var (
	scriptElementRe = regexp.MustCompile(`(?s)<script([^>]*)>(.*?)</script>`)
	eventHandlerRe  = regexp.MustCompile(`(?i)<[^>]*\son[a-z]+\s*=`)
)

// TestDashboard_NoInlineScriptOrStyle is the markup half of Acceptance Criterion
// 281; the browser half is measured with headless Chromium (see the task report).
func TestDashboard_NoInlineScriptOrStyle(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	for _, name := range []string{"settlement-platform", "greenfield"} {
		if name == "greenfield" {
			openDashRoadmap(t, name)
		} else {
			seedDashFixture(t, name)
		}
		rec := getDashboard(t, name, dashNow)
		body := rec.Body.String()
		if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
			t.Errorf("%s: Content-Security-Policy = %q", name, got)
		}
		data := 0
		for _, m := range scriptElementRe.FindAllStringSubmatch(body, -1) {
			if m[1] == ` type="application/json" id="dashboard-data"` {
				data++
				continue
			}
			if !strings.Contains(m[1], ` src="/static/`) || m[2] != "" {
				t.Errorf("%s: a script element carries no src or has content: <script%s>%s", name, m[1], m[2])
			}
		}
		if data != 1 {
			t.Errorf("%s: %d data blocks", name, data)
		}
		if strings.Contains(body, "style=") {
			t.Errorf("%s: an element carries a style attribute", name)
		}
		if m := eventHandlerRe.FindString(body); m != "" {
			t.Errorf("%s: an element carries an event-handler attribute: %s", name, m)
		}
		main := regionBetween(t, body, `<main class="page-body">`, `</main>`)
		for _, absent := range []string{"<form", "<button", "<input", "method=\"post\""} {
			if strings.Contains(strings.ToLower(main), absent) {
				t.Errorf("%s: the read-only dashboard carries %s", name, absent)
			}
		}
	}

	// The page script writes widths through the CSSOM only, and sets no theme
	// and no toolbar.
	js := stripJSComments(readEmbeddedAsset(t, "static/dashboard.js"))
	for _, want := range []string{
		`segments[i].style.width = share + "%";`, `getAttribute("data-percent")`,
		`toolbar: { show: false }`, `animations: { enabled: false }`, `fontFamily: "inherit"`, `parentHeightOffset: 0`,
		`dataLabels: { enabled: false }`, `fillSeriesColor: false`,
		`JSON.parse(block.textContent)`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("dashboard.js lacks %q", want)
		}
	}
	for _, forbidden := range []string{"setAttribute(\"style\"", "theme:", "fetch(", "XMLHttpRequest", "localStorage",
		"sessionStorage", "indexedDB", "document.cookie", "innerHTML", "#"} {
		if forbidden == "#" {
			// No literal colour value: every series colour is a Tabler custom property.
			if regexp.MustCompile(`["']#[0-9a-fA-F]{3,8}["']|rgba?\(`).MatchString(js) {
				t.Errorf("dashboard.js writes a literal colour value")
			}
			continue
		}
		if strings.Contains(js, forbidden) {
			t.Errorf("dashboard.js uses %q", forbidden)
		}
	}
}

// TestDashboard_ChartOptionsFollowTabler is the source half of Acceptance
// Criterion 285: the three KPI donuts are the script's only charts, the one
// configuration it passes to ApexCharts carries the inherited font family, the
// zero parent-height offset, the hidden toolbar, and the disabled animations
// (SPEC/WEB.md § Dashboard Chart Library, rule 4) — the donut options are pinned
// by TestDashboard_KPICardsCarryTypeDonuts — and no line-chart option or helper
// survives.
func TestDashboard_ChartOptionsFollowTabler(t *testing.T) {
	js := stripJSComments(readEmbeddedAsset(t, "static/dashboard.js"))
	norm := strings.Join(strings.Fields(js), " ")
	if n := strings.Count(norm, "chart: {"); n != 1 {
		t.Errorf("dashboard.js builds %d chart configurations, want the donut's alone", n)
	}
	if n := strings.Count(norm, "new ApexCharts("); n != 1 {
		t.Errorf("dashboard.js constructs ApexCharts at %d sites, want 1", n)
	}
	if n := strings.Count(norm, `draw("`); n != 3 {
		t.Errorf("dashboard.js draws %d charts, want the three donuts", n)
	}
	for _, want := range []string{
		`fontFamily: "inherit", parentHeightOffset: 0,`, `toolbar: { show: false }, animations: { enabled: false }`,
	} {
		if !strings.Contains(norm, want) {
			t.Errorf("dashboard.js lacks %q", want)
		}
	}
	for _, forbidden := range []string{
		`"smooth"`, `"100%"`, "theme", "HEIGHT", "GRID_PADDING", "LEGEND", "stepline", "straight", "stroke",
		"xaxis", "yaxis", "countAxis", "wholeNumber", "lineOptions", "baseChart", "largest", "MAX_TICKS",
		"PRIMARY", "GREEN", "burndown", "throughput", `"line"`,
	} {
		if strings.Contains(js, forbidden) {
			t.Errorf("dashboard.js carries %s", forbidden)
		}
	}
	if regexp.MustCompile(`height:\s*"[0-9.]+%"`).MatchString(js) {
		t.Errorf("a chart height is a percentage")
	}
}

// ==================== AC 282: THE VENDORED CHART ASSETS ====================

// vendoredChartReleases records, per vendored Tabler release, the ApexCharts
// release its core/package.json pins and the SHA-256 of the two upstream files,
// as published in the npm tarballs @tabler/core-<release>.tgz
// (package/dist/css/tabler-vendors.min.css) and apexcharts-<release>.tgz
// (package/dist/apexcharts.min.js), whose integrity was checked against the
// registry. A Tabler upgrade that does not record its pinned ApexCharts release
// here fails TestVendoredChartAssets (SPEC/BUILD.md § Vendored Web Assets,
// rules 7 and 8).
var vendoredChartReleases = map[string]struct {
	apexcharts    string
	apexSHA256    string
	vendorsSHA256 string
}{
	"1.6.1": {
		apexcharts:    "7.0.0",
		apexSHA256:    "a2e892d841ef25f62517c1663ce6aaa759df51eff6f85a9a1105cba299075910",
		vendorsSHA256: "351a2c128fecb8a640f8f11be39da19d75cde2ffd27cb30e3792a7c5b8f8cac2",
	},
}

var (
	tablerVendorsBannerRe = regexp.MustCompile(`^/\*!\s*\*\s*Tabler Vendors v(\d+\.\d+\.\d+) \(https://tabler\.io\)`)
	apexBannerRe          = regexp.MustCompile(`^/\*!\s*\*\s*ApexCharts v(\d+\.\d+\.\d+)\s`)
)

// TestVendoredChartAssets is Acceptance Criterion 282 and the three
// SPEC/BUILD.md checklist criteria of rules 7 and 8.
func TestVendoredChartAssets(t *testing.T) {
	tabler := tablerBannerRe.FindStringSubmatch(readEmbeddedAsset(t, "static/vendor/tabler/tabler.min.css"))
	if tabler == nil {
		t.Fatal("tabler.min.css names no release")
	}
	release, known := vendoredChartReleases[tabler[1]]
	if !known {
		t.Fatalf("Tabler %s is vendored, but the ApexCharts release it pins is not recorded", tabler[1])
	}

	vendors := readEmbeddedAsset(t, "static/vendor/tabler/tabler-vendors.min.css")
	if m := tablerVendorsBannerRe.FindStringSubmatch(vendors); m == nil || m[1] != tabler[1] {
		t.Errorf("tabler-vendors.min.css names release %v, want Tabler %s", m, tabler[1])
	}
	if !strings.Contains(vendors, "--apx-") || !strings.Contains(vendors, ".apexcharts") {
		t.Errorf("tabler-vendors.min.css carries no ApexCharts styling")
	}
	apex := readEmbeddedAsset(t, "static/vendor/apexcharts/apexcharts.min.js")
	if m := apexBannerRe.FindStringSubmatch(apex); m == nil || m[1] != release.apexcharts {
		t.Errorf("apexcharts.min.js names release %v, want %s, the release Tabler %s pins", m, release.apexcharts, tabler[1])
	}
	if got := sha256Hex([]byte(apex)); got != release.apexSHA256 {
		t.Errorf("apexcharts.min.js is not byte for byte the upstream file: sha256 %s, want %s", got, release.apexSHA256)
	}
	if got := sha256Hex([]byte(vendors)); got != release.vendorsSHA256 {
		t.Errorf("tabler-vendors.min.css is not byte for byte the upstream file: sha256 %s, want %s", got, release.vendorsSHA256)
	}

	licences := readEmbeddedAsset(t, "static/vendor/LICENSES.md")
	for _, row := range []string{
		"| Tabler vendors stylesheet (ApexCharts styling) | `tabler/tabler-vendors.min.css` | Tabler | MIT |",
		"| ApexCharts (dashboard chart library) | `apexcharts/apexcharts.min.js` | ApexCharts | ApexCharts dual licence (Community licence; Commercial and OEM licences) |",
	} {
		if !strings.Contains(licences, row) {
			t.Errorf("LICENSES.md lacks the row %q", row)
		}
	}

	// The dashboard links and loads them, in order; no other page loads either
	// script or the vendors stylesheet; each is served, cacheable, from /static/.
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	body := dashboardBody(t, f.name, dashNow)
	assertOrdered(t, "dashboard head", body, []string{
		`<link rel="stylesheet" href="/static/vendor/tabler/tabler.min.css">`,
		`<link rel="stylesheet" href="/static/vendor/tabler/tabler-vendors.min.css">`,
		`<script defer src="/static/vendor/apexcharts/apexcharts.min.js"></script>`,
		`<script defer src="/static/dashboard.js"></script>`,
	})
	head := regionBetween(t, body, "<head>", "</head>")
	if !strings.Contains(head, "<link rel=\"stylesheet\" href=\"/static/vendor/tabler/tabler.min.css\">\n  <link rel=\"stylesheet\" href=\"/static/vendor/tabler/tabler-vendors.min.css\">") {
		t.Errorf("the vendors stylesheet does not directly follow the Tabler stylesheet")
	}
	for _, path := range allPagePaths(f.name)[1:] {
		if path == "/roadmaps/"+f.name || strings.HasSuffix(path, "/1") {
			continue
		}
		page := servePage(t, buildMux(), path)
		for _, asset := range []string{"apexcharts.min.js", "dashboard.js", "tabler-vendors.min.css"} {
			if strings.Contains(page, asset) {
				t.Errorf("%s loads %s; only the dashboard does", path, asset)
			}
		}
	}
	for _, asset := range []string{"/static/vendor/tabler/tabler-vendors.min.css", "/static/vendor/apexcharts/apexcharts.min.js", "/static/dashboard.js"} {
		rec := httptest.NewRecorder()
		handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, asset, nil))
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Errorf("GET %s: status %d, %d bytes", asset, rec.Code, rec.Body.Len())
		}
		if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "no-store") {
			t.Errorf("GET %s carries Cache-Control %q; a static asset stays cacheable", asset, cc)
		}
	}
	if regexp.MustCompile(`(?:src|href)="(?:https?:)?//`).MatchString(body) {
		t.Errorf("the dashboard references a remote origin")
	}
}

// sha256Hex is the lowercase hexadecimal SHA-256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ==================== AC 283: ONE READ, NO AUDIT ENTRY ====================

// countingDashboardSource counts the dashboard's reads.
type countingDashboardSource struct {
	*db.DB
	reads int
}

func (c *countingDashboardSource) ReadDashboard(ctx context.Context) (*db.Dashboard, error) {
	c.reads++
	return c.DB.ReadDashboard(ctx)
}

// TestDashboard_OneReadAndNoAuditEntry is the page half of Acceptance Criterion
// 283: the page performs its whole read through the one transaction of
// db.ReadDashboard, once per render whatever the roadmap holds, and serving it
// writes no audit entry. The statement count of that read is measured in
// internal/db.
func TestDashboard_OneReadAndNoAuditEntry(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")

	database, err := db.OpenReadOnly(f.name)
	if err != nil {
		t.Fatalf("opening the roadmap: %v", err)
	}
	src := &countingDashboardSource{DB: database}
	if _, err := readDashboard(context.Background(), src, f.name, dashNow); err != nil {
		t.Fatalf("readDashboard: %v", err)
	}
	if src.reads != 1 {
		t.Errorf("the page issued %d dashboard reads, want 1", src.reads)
	}
	before, err := database.CountAuditEntries(context.Background())
	if err != nil {
		t.Fatalf("counting audit entries: %v", err)
	}
	_ = database.Close()

	for range 3 {
		dashboardBody(t, f.name, dashNow)
		rec := httptest.NewRecorder()
		handler().ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/roadmaps/"+f.name, nil))
	}

	database, err = db.OpenReadOnly(f.name)
	if err != nil {
		t.Fatalf("reopening the roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup
	after, err := database.CountAuditEntries(context.Background())
	if err != nil {
		t.Fatalf("counting audit entries: %v", err)
	}
	if after != before {
		t.Errorf("serving the dashboard changed the audit log from %d to %d entries", before, after)
	}
}

// ==================== AC 284: THE PHONE LAYOUT (MARKUP) ====================

// TestDashboard_ResponsiveMarkup is the markup half of Acceptance Criterion 284:
// every card-table sits in a table-responsive container, the only chart
// containers are the three KPI donuts, and every column class is a breakpoint
// class, so below sm every block takes the full width. The 375px measurement is
// taken with headless Chromium (see the task report).
func TestDashboard_ResponsiveMarkup(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedDashFixture(t, "settlement-platform")
	seedRoadmapWithAudit(t, f.name, 3)
	body := dashboardBody(t, f.name, dashNow)
	main := regionBetween(t, body, `<main class="page-body">`, `</main>`)

	tables := strings.Count(main, "<table")
	wrapped := strings.Count(main, `<div class="table-responsive">`+"\n"+`                  <table class="table card-table table-vcenter">`)
	if tables != 4 || wrapped != tables {
		t.Errorf("%d tables, %d of them directly in table-responsive; want 4 and 4", tables, wrapped)
	}
	if strings.Contains(main, "chart-lg") {
		t.Errorf("the page carries a chart-lg container")
	}
	if n, all := len(dashDonutRe.FindAllString(main, -1)), len(dashChartRe.FindAllString(main, -1)); n != 3 || all != 3 {
		t.Errorf("%d donut containers among %d chart containers, want 3 and 3", n, all)
	}
	for _, col := range dashGridCols(main) {
		for _, class := range strings.Fields(col) {
			if class != "col-12" && !strings.HasPrefix(class, "col-sm-") && !strings.HasPrefix(class, "col-lg-") {
				t.Errorf("column class %q fixes a width below sm", class)
			}
		}
	}
}

// ==================== HELPERS OF THE DERIVATION ====================

// TestDashboardDerivation_Helpers pins the closed definitions the page states its
// figures with (SPEC/WEB.md § Roadmap Dashboard Page, definitions 5 to 7).
func TestDashboardDerivation_Helpers(t *testing.T) {
	for _, tc := range []struct{ part, whole, want int }{{0, 0, 0}, {4, 12, 33}, {1, 6, 16}, {2, 3, 66}, {5, 5, 100}, {99, 100, 99}} {
		if got := floorPercent(tc.part, tc.whole); got != tc.want {
			t.Errorf("floorPercent(%d, %d) = %d, want %d", tc.part, tc.whole, got, tc.want)
		}
	}
	for _, tc := range []struct {
		part, whole int
		want        string
	}{{2, 6, "33.33"}, {1, 6, "16.67"}, {1, 2, "50"}, {1, 8, "12.5"}, {3, 3, "100"}, {1, 0, "0"}} {
		if got := sharePercent(tc.part, tc.whole); got != tc.want {
			t.Errorf("sharePercent(%d, %d) = %q, want %q", tc.part, tc.whole, got, tc.want)
		}
	}
	for n, want := range map[int]string{0: "0 days", 1: "1 day", 2: "2 days", 7: "7 days"} {
		if got := daysText(n); got != want {
			t.Errorf("daysText(%d) = %q, want %q", n, got, want)
		}
	}
	for _, tc := range []struct {
		ago  time.Duration
		want int
	}{{5 * time.Hour, 0}, {30 * time.Hour, 1}, {(7*24 + 2) * time.Hour, 7}, {-5 * time.Hour, 0}} {
		got, ok := wholeDaysElapsed(iso(dashNow.Add(-tc.ago)), dashNow)
		if !ok || got != tc.want {
			t.Errorf("wholeDaysElapsed(%v ago) = %d, %v; want %d", tc.ago, got, ok, tc.want)
		}
	}
	// The days elapsed of the current sprint are `rmp sprint stats`'s own.
	started := iso(dashNow.Add(-(3*24 + 5) * time.Hour))
	stats := models.CalculateSprintStats(1, nil)
	stats.ApplySprintMetrics(&models.Sprint{Status: models.SprintOpen, StartedAt: &started}, nil, iso(dashNow))
	if stats.DaysElapsed == nil || *stats.DaysElapsed != 3 {
		t.Errorf("rmp sprint stats reports %v days elapsed, want 3", stats.DaysElapsed)
	}
}
