package web

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for the roadmap tasks page: one paginated task list in a
// Tabler card, filtered and paginated on the server (SPEC/WEB.md § Roadmap Tasks
// Page; Acceptance Criteria 9, 81 to 93, 100, 102 to 104, 106, 107, 112 to 117,
// 128, 129, 232 to 242, and 248). The search text rules are gated in
// tasks_search_test.go, and the filter-state cookie, the defaults, and the
// route's caching in tasks_filter_state_test.go.
//
// A request carrying none of the six parameters is BARE and takes its state from
// the filter-state cookie or the defaults, which exclude COMPLETED; a test that
// means "every task" therefore requests the page explicitly, with size=25.
//
// Every expectation is computed from the task data the fixture created — its own
// record of each task's status, type, priority, severity, sprint, creation time and
// title — and never read back from the page under test.

// ==================== FIXTURE ====================

// listTask is the fixture's own record of one task it created.
type listTask struct {
	title    string
	created  string
	status   models.TaskStatus
	taskType models.TaskType
	id       int
	priority int
	severity int
	sprint   int // 0 when the task belongs to no sprint
}

// listFixture is a roadmap whose tasks vary every dimension the page filters by,
// with two sprints and tasks in no sprint.
type listFixture struct {
	name    string
	tasks   []listTask
	sprintA int
	sprintB int
}

// listTitlePhrases are the realistic title stems the fixture cycles through. Two
// of the eight carry the word the search tests look for, in two cases.
var listTitlePhrases = []string{
	"Invalidate the settlement cache after a refund",
	"Rotate the acquirer API credentials",
	"Reconcile the nightly payout file",
	"Warm the Cache of merchant fee schedules",
	"Alert on residual balances after close",
	"Export the ledger to the finance warehouse",
	"Harden webhook signature verification",
	"Backfill missing dispute evidence records",
}

// seedListFixture creates n tasks. Task i (0-based) carries:
//   - priority (g*7)%10 and created_at minute g, where g = i/3, so every group of
//     three consecutive tasks is equal on BOTH ordering keys and only the id
//     separates them (Acceptance Criterion 84);
//   - severity (i*3)%10, type ValidTaskTypes[i%10], status
//     ValidTaskStatuses[(i/4)%5];
//   - sprint A when i%3 == 0, sprint B when i%3 == 1, and no sprint otherwise, so
//     a BACKLOG task can be a sprint member and a SPRINT task can be in none.
func seedListFixture(t *testing.T, name string, n int) listFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	f := listFixture{name: name}
	f.sprintA = newSprint(t, database, "Checkout hardening", "Close the checkout attack surface before the peak season.")
	f.sprintB = newSprint(t, database, "Settlement reconciliation", "Reconcile acquirer settlement files nightly.")

	var inA, inB []int
	for i := range n {
		g := i / 3
		task := listTask{
			title:    fmt.Sprintf("%s (batch %d)", listTitlePhrases[i%len(listTitlePhrases)], i+1),
			created:  fmt.Sprintf("2026-03-01T09:%02d:00.000Z", g%60),
			status:   models.ValidTaskStatuses[(i/4)%len(models.ValidTaskStatuses)],
			taskType: models.ValidTaskTypes[i%len(models.ValidTaskTypes)],
			priority: (g * 7) % 10,
			severity: (i * 3) % 10,
		}
		id, terr := seedTask(database, &models.Task{
			Title:                  task.title,
			Type:                   task.taskType,
			Status:                 models.StatusBacklog,
			Priority:               task.priority,
			Severity:               task.severity,
			FunctionalRequirements: "The checkout must reject a replayed payment token.",
			TechnicalRequirements:  "Keep the nonce store in the payments database.",
			AcceptanceCriteria:     "A replayed token is refused with a clear error.",
			CreatedAt:              task.created,
		})
		if terr != nil {
			t.Fatalf("creating task %d: %v", i+1, terr)
		}
		task.id = id
		switch i % 3 {
		case 0:
			task.sprint = f.sprintA
			inA = append(inA, id)
		case 1:
			task.sprint = f.sprintB
			inB = append(inB, id)
		}
		f.tasks = append(f.tasks, task)
	}
	ctx := context.Background()
	if err := database.AddTasksToSprint(ctx, f.sprintA, inA); err != nil {
		t.Fatalf("adding tasks to sprint A: %v", err)
	}
	if err := database.AddTasksToSprint(ctx, f.sprintB, inB); err != nil {
		t.Fatalf("adding tasks to sprint B: %v", err)
	}
	// The statuses are set after the membership, which a sprint add rewrites.
	for _, status := range models.ValidTaskStatuses {
		var ids []int
		for _, task := range f.tasks {
			if task.status == status {
				ids = append(ids, task.id)
			}
		}
		forceTaskLifecycle(t, database, ids, status)
	}
	return f
}

// expect returns the ids of the fixture's tasks that keep admits, in the page's
// order: priority descending, created_at ascending, id ascending.
func (f listFixture) expect(keep func(listTask) bool) []int {
	var matched []listTask
	for _, task := range f.tasks {
		if keep(task) {
			matched = append(matched, task)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if a.priority != b.priority {
			return a.priority > b.priority
		}
		if a.created != b.created {
			return a.created < b.created
		}
		return a.id < b.id
	})
	ids := make([]int, len(matched))
	for i, task := range matched {
		ids[i] = task.id
	}
	return ids
}

// all admits every task.
func all(listTask) bool { return true }

// explicitTasks is the tasks page's input for an explicit request carrying the
// given query parameters: its state comes from the URL alone.
func explicitTasks(values url.Values) *tasksRequest {
	return &tasksRequest{values: values, explicit: true}
}

// bareTasks is the tasks page's input for a bare request, carrying the
// filter-state cookie's raw value when cookie is not nil.
func bareTasks(cookie *string) *tasksRequest {
	req := &tasksRequest{values: url.Values{}}
	if cookie != nil {
		req.cookie, req.hasCookie = *cookie, true
	}
	return req
}

// everyTask is the query of an explicit request with no filter at the default
// page size: it lists every task of the roadmap, COMPLETED ones included.
func everyTask() url.Values { return url.Values{"size": {"25"}} }

// listPath is the tasks page's path with the given query parameters.
func listPath(name string, params url.Values) string {
	if len(params) == 0 {
		return "/roadmaps/" + name + "/tasks"
	}
	return "/roadmaps/" + name + "/tasks?" + params.Encode()
}

// ==================== PARSING THE SERVED PAGE ====================

// servedOption is one option of a served select.
type servedOption struct {
	value, label string
	selected     bool
}

// servedPageItem is one item of the served pagination bar.
type servedPageItem struct {
	text     string
	href     string
	current  bool
	ellipsis bool
}

// servedSizeLink is one link of the served rows-per-page selector.
type servedSizeLink struct {
	text, href string
	active     bool
	current    bool
}

// servedCheck is one checkbox of a served dropdown menu.
type servedCheck struct {
	value, label string
	checked      bool
}

// servedDropdownToggle is a served dropdown's toggle button: its id, the id its
// aria-describedby names, and the text of the span that id names.
type servedDropdownToggle struct {
	id, describedBy, spanID, text string
}

// servedList is what a served tasks page states, read from its HTML alone.
type servedList struct {
	selects    map[string][]servedOption
	checks     map[string][]servedCheck
	toggles    map[string]servedDropdownToggle
	body       string
	search     string
	hiddenSize string
	resetHref  string
	emptyTitle string
	prevHref   string
	nextHref   string
	ids        []int
	pageItems  []servedPageItem
	sizeLinks  []servedSizeLink
	first      int
	last       int
	total      int
	hasFooter  bool
}

var (
	reListRowID   = regexp.MustCompile(`<tr>\s*<td><span class="badge bg-black text-white">#(\d+)</span></td>`)
	reRangeText   = regexp.MustCompile(`<p class="m-0 text-secondary">Showing <span>(\d+)</span> to <span>(\d+)</span> of <span>(\d+)</span> entries</p>`)
	reDropdown    = regexp.MustCompile(`(?s)<div class="dropdown">\s*<button type="button" class="form-select form-select-sm" id="([^"]*)" data-bs-toggle="dropdown" data-bs-auto-close="outside" aria-describedby="([^"]*)"><span id="([^"]*)">([^<]*)</span></button>\s*<div class="dropdown-menu">(.*?)</div>\s*</div>`)
	reCheck       = regexp.MustCompile(`<label class="dropdown-item"><input type="checkbox" class="form-check-input" name="([a-z]+)" value="([^"]*)"( checked)?>([^<]*)</label>`)
	reSelectBlock = regexp.MustCompile(`(?s)<select class="form-select form-select-sm(?: task-list__sprint-select)?" id="task-filter-[a-z]+" name="([a-z]+)">(.*?)</select>`)
	reOption      = regexp.MustCompile(`<option value="([^"]*)"( selected)?>([^<]*)</option>`)
	reSearchInput = regexp.MustCompile(`<input type="search" class="form-control form-control-sm" id="task-filter-q" name="q" placeholder="Search" value="([^"]*)">`)
	reHiddenSize  = regexp.MustCompile(`<input type="hidden" name="size" value="([^"]*)">`)
	reResetLink   = regexp.MustCompile(`<a class="btn" href="([^"]*)">Reset</a>`)
	reEmptyTitle  = regexp.MustCompile(`<p class="empty-title">([^<]*)</p>`)
	rePageItem    = regexp.MustCompile(`<li class="page-item( active| disabled)?"( aria-current="page")?>\s*(?:<a class="page-link" href="([^"]*)"(?: aria-label="(?:Previous|Next)")?>([^<]*|<i class="ti ti-chevron-(?:left|right)"></i>)</a>|<span class="page-link"(?: aria-label="(?:Previous|Next)")?(?: aria-disabled="true")?>([^<]*|<i class="ti ti-chevron-(?:left|right)"></i>)</span>)\s*</li>`)
	reSizeLink    = regexp.MustCompile(`<a class="btn btn-sm( active)?" href="([^"]*)"( aria-current="true")?>(\d+)</a>`)
)

// parseList reads a served tasks page.
func parseList(t *testing.T, body string) servedList {
	t.Helper()

	s := servedList{body: body, selects: map[string][]servedOption{}, checks: map[string][]servedCheck{}, toggles: map[string]servedDropdownToggle{}}
	for _, m := range reDropdown.FindAllStringSubmatch(body, -1) {
		var name string
		for _, c := range reCheck.FindAllStringSubmatch(m[5], -1) {
			name = c[1]
			s.checks[name] = append(s.checks[name], servedCheck{
				value: html.UnescapeString(c[2]), label: html.UnescapeString(c[4]), checked: c[3] != "",
			})
		}
		s.toggles[name] = servedDropdownToggle{id: m[1], describedBy: m[2], spanID: m[3], text: html.UnescapeString(m[4])}
	}
	for _, m := range reListRowID.FindAllStringSubmatch(body, -1) {
		s.ids = append(s.ids, mustInt(t, m[1]))
	}
	if m := reRangeText.FindStringSubmatch(body); m != nil {
		s.first, s.last, s.total = mustInt(t, m[1]), mustInt(t, m[2]), mustInt(t, m[3])
	}
	s.hasFooter = strings.Contains(body, `<div class="card-footer">`)
	for _, m := range reSelectBlock.FindAllStringSubmatch(body, -1) {
		for _, o := range reOption.FindAllStringSubmatch(m[2], -1) {
			s.selects[m[1]] = append(s.selects[m[1]], servedOption{
				value: html.UnescapeString(o[1]), label: html.UnescapeString(o[3]), selected: o[2] != "",
			})
		}
	}
	if m := reSearchInput.FindStringSubmatch(body); m != nil {
		s.search = html.UnescapeString(m[1])
	} else {
		t.Fatalf("the served page has no search input of the specified shape")
	}
	if m := reHiddenSize.FindStringSubmatch(body); m != nil {
		s.hiddenSize = m[1]
	}
	if m := reResetLink.FindStringSubmatch(body); m != nil {
		s.resetHref = html.UnescapeString(m[1])
	}
	if m := reEmptyTitle.FindStringSubmatch(body); m != nil {
		s.emptyTitle = m[1]
	}
	if nav := strings.Index(body, `<nav aria-label="Task list pages">`); nav >= 0 {
		region := body[nav : nav+strings.Index(body[nav:], "</nav>")]
		items := rePageItem.FindAllStringSubmatch(region, -1)
		for i, m := range items {
			text := m[4] + m[5]
			href := html.UnescapeString(m[3])
			switch {
			case i == 0:
				s.prevHref = href
			case i == len(items)-1:
				s.nextHref = href
			default:
				s.pageItems = append(s.pageItems, servedPageItem{
					text: text, href: href, current: m[1] == " active", ellipsis: text == "&hellip;",
				})
			}
		}
	}
	for _, m := range reSizeLink.FindAllStringSubmatch(body, -1) {
		s.sizeLinks = append(s.sizeLinks, servedSizeLink{
			text: m[4], href: html.UnescapeString(m[2]), active: m[1] != "", current: m[3] != "",
		})
	}
	return s
}

// selected returns the value of the one selected option of a select, failing
// unless exactly one option is selected.
func (s *servedList) selected(t *testing.T, name string) string {
	t.Helper()

	var picked []string
	for _, o := range s.selects[name] {
		if o.selected {
			picked = append(picked, o.value)
		}
	}
	if len(picked) != 1 {
		t.Fatalf("the %s select marks %d options selected (%v), want exactly 1", name, len(picked), picked)
	}
	return picked[0]
}

// checked returns the values of a dropdown's checked boxes, in menu order,
// failing when the page carries no such dropdown.
func (s *servedList) checked(t *testing.T, name string) []string {
	t.Helper()

	boxes, ok := s.checks[name]
	if !ok {
		t.Fatalf("the served page has no %s dropdown of the specified shape", name)
	}
	values := []string{}
	for _, box := range boxes {
		if box.checked {
			values = append(values, box.value)
		}
	}
	return values
}

// toggleText is the visible text of a dropdown's toggle.
func (s *servedList) toggleText(t *testing.T, name string) string {
	t.Helper()

	toggle, ok := s.toggles[name]
	if !ok {
		t.Fatalf("the served page has no %s dropdown toggle of the specified shape", name)
	}
	return toggle.text
}

// currentPage is the number of the pagination bar's active item.
func (s *servedList) currentPage(t *testing.T) int {
	t.Helper()
	for _, item := range s.pageItems {
		if item.current {
			return mustInt(t, item.text)
		}
	}
	t.Fatalf("the pagination bar has no active item")
	return 0
}

// mustIndex is strings.Index for a substring the markup must carry: it fails the
// test instead of returning -1.
func mustIndex(t *testing.T, s, sub string) int {
	t.Helper()
	at := strings.Index(s, sub)
	if at < 0 {
		t.Fatalf("the markup does not carry %q", sub)
	}
	return at
}

// mustInt parses a decimal captured from served markup.
func mustInt(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("%q is not a decimal number: %v", s, err)
	}
	return n
}

// fetchList serves one tasks page and parses it.
func fetchList(t *testing.T, mux *http.ServeMux, path string) servedList {
	t.Helper()
	return parseList(t, servePage(t, mux, path))
}

// allPages follows the page numbers from page 1 and returns the concatenated row
// ids of every page, failing if a page's range text disagrees with its rows.
func allPages(t *testing.T, mux *http.ServeMux, name string, params url.Values, size int) []int {
	t.Helper()

	var ids []int
	for page := 1; ; page++ {
		q := url.Values{}
		for k, v := range params {
			q[k] = v
		}
		q.Set("size", strconv.Itoa(size))
		q.Set("page", strconv.Itoa(page))
		s := fetchList(t, mux, listPath(name, q))
		if s.total == 0 {
			return ids
		}
		if got := s.currentPage(t); got != page {
			t.Fatalf("page %d at size %d renders page %d", page, size, got)
		}
		if s.first != (page-1)*size+1 || s.last != min(page*size, s.total) || len(s.ids) != s.last-s.first+1 {
			t.Fatalf("page %d at size %d: range %d to %d of %d over %d rows", page, size, s.first, s.last, s.total, len(s.ids))
		}
		ids = append(ids, s.ids...)
		if s.last == s.total {
			return ids
		}
	}
}

// ==================== THE LIST AND ITS CARD ====================

// TestTaskList_IsOneListCard is the gate for Acceptance Criteria 9 and 81: the
// page answers 200 with exactly one list card holding one table-responsive
// container and one table, one <tbody> row per task of the rendered page, tasks of
// all five statuses in the one table, and no board, no column per status, no card
// per task, and no element whose class begins task-board. An unknown roadmap and a
// name that violates the roadmap-name rules answer 404, whatever the query.
func TestTaskList_IsOneListCard(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 20)
	mux := buildMux()

	body := servePage(t, mux, listPath(f.name, everyTask()))
	main := boardRegion(t, body)
	if got := strings.Count(main, `<div class="card">`); got != 1 {
		t.Errorf("the page renders %d list cards, want exactly 1", got)
	}
	if got := strings.Count(main, `<div class="table-responsive">`); got != 1 {
		t.Errorf("the page renders %d table-responsive containers, want exactly 1", got)
	}
	if got := strings.Count(main, "<table"); got != 1 {
		t.Errorf("the page renders %d tables, want exactly 1", got)
	}
	if !strings.Contains(main, `<table class="table table-vcenter card-table">`) {
		t.Errorf("the table does not carry exactly table, table-vcenter and card-table")
	}
	s := parseList(t, body)
	if len(s.ids) != 20 || strings.Count(main, "<tr>") != 21 {
		t.Errorf("the page renders %d task rows (%d <tr>), want 20 rows plus the header row",
			len(s.ids), strings.Count(main, "<tr>"))
	}
	statuses := map[models.TaskStatus]bool{}
	for _, task := range f.tasks {
		statuses[task.status] = true
	}
	if len(statuses) != len(models.ValidTaskStatuses) {
		t.Fatalf("the fixture spans %d statuses, want all %d", len(statuses), len(models.ValidTaskStatuses))
	}
	for _, forbidden := range []string{"task-board", "card-sm card-link", `data-role="task-board`} {
		if strings.Contains(main, forbidden) {
			t.Errorf("the page carries %q; it renders one list and no board or card per task", forbidden)
		}
	}

	srv := handler()
	for _, path := range []string{
		"/roadmaps/no-such-roadmap/tasks",
		"/roadmaps/no-such-roadmap/tasks?status=DOING&page=2",
		"/roadmaps/..%2Fetc/tasks",
		"/roadmaps/bad%20name/tasks?q=x",
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
		}
	}
}

// TestTaskList_CardFollowsTheTablerExamples is the gate for Acceptance Criterion
// 232, asserted element by element: the card's first child is the header with
// flex-wrap and gap-2; the header's first child is a <div> holding one
// <h2 class="card-title">Task list</h2> and no card-subtitle, its second child the
// card-actions container holding the form and nothing else; then the
// table-responsive table with its nine headings; then the footer's row of three
// col-auto columns, the third carrying ms-auto.
func TestTaskList_CardFollowsTheTablerExamples(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 30)
	body := servePage(t, buildMux(), listPath(f.name, nil))
	main := boardRegion(t, body)

	// The skeleton with every element's own content cut out, so the structure is
	// compared as a sequence of tags.
	card := main[mustIndex(t, main, `<div class="card">`):]
	want := []string{
		`<div class="card">`,
		`<div class="card-header flex-wrap gap-2">`,
		`<div>`, `<h2 class="card-title">Task list</h2>`, `</div>`,
		`<div class="card-actions">`, `<form class="row g-2 align-items-end justify-content-end" method="get" action="/roadmaps/` + f.name + `/tasks">`,
	}
	cursor := 0
	for _, piece := range want {
		at := strings.Index(card[cursor:], piece)
		if at < 0 {
			t.Fatalf("the card does not carry %q where the Tabler examples put it", piece)
		}
		if between := strings.TrimSpace(stripTemplateComments(card[cursor : cursor+at])); between != "" {
			t.Fatalf("the card carries %q before %q; the examples put nothing there", between, piece)
		}
		cursor += at + len(piece)
	}
	header := card[:mustIndex(t, card, `<div class="table-responsive">`)]
	if strings.Contains(header, "card-subtitle") {
		t.Errorf("the card header carries a card-subtitle; the footer's range text states the count")
	}
	actions := header[mustIndex(t, header, `<div class="card-actions">`):]
	formEnd := strings.Index(actions, "</form>")
	if rest := strings.TrimSpace(actions[formEnd+len("</form>"):]); !strings.HasPrefix(rest, "</div>") ||
		strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(rest, "</div>")), "</div>")) != "" {
		t.Errorf("the card-actions container holds more than the form: %q", rest)
	}

	// The table and its headings.
	table := card[mustIndex(t, card, `<div class="table-responsive">`):]
	if !strings.HasPrefix(strings.TrimSpace(table[len(`<div class="table-responsive">`):]), `<table class="table table-vcenter card-table">`) {
		t.Errorf("the table-responsive container does not hold the table directly")
	}
	if strings.Contains(table, "table-selectable") {
		t.Errorf("the table carries table-selectable; the list has no selection")
	}
	reHeading := regexp.MustCompile(`<th( class="[^"]*")?>([^<]*)</th>`)
	headings, headingClasses := make([]string, 0, 9), make([]string, 0, 9)
	for _, m := range reHeading.FindAllStringSubmatch(table[:mustIndex(t, table, "</thead>")], -1) {
		headings = append(headings, m[2])
		headingClasses = append(headingClasses, m[1])
	}
	if want := []string{"ID", "Title", "Type", "Status", "Severity", "Priority", "Created"}; !slices.Equal(headings, want) {
		t.Errorf("the headings are %v, want %v", headings, want)
	} else if headingClasses[0] != ` class="w-1"` || slices.ContainsFunc(headingClasses[1:], func(c string) bool { return c != "" }) {
		t.Errorf("the headings carry the classes %q, want w-1 on ID alone", headingClasses)
	}

	// The footer, last child of the card.
	footer := table[mustIndex(t, table, `<div class="card-footer">`):]
	reFooter := regexp.MustCompile(`(?s)^<div class="card-footer">\s*<div class="row g-2 align-items-center">\s*` +
		`<div class="col-auto">\s*<p class="m-0 text-secondary">Showing.*?</p>\s*</div>\s*` +
		`<div class="col-auto">\s*<span class="text-secondary me-2" id="task-list-size-label">Rows per page</span>\s*<div class="btn-group" role="group" aria-labelledby="task-list-size-label">.*?</div>\s*</div>\s*` +
		`<div class="col-auto ms-auto">\s*<nav aria-label="Task list pages">.*?</nav>\s*</div>\s*</div>\s*</div>` +
		// The footer closes the card, and the card closes the page's container.
		`\s*</div>\s*</div>\s*$`)
	if !reFooter.MatchString(strings.TrimSpace(footer)) {
		t.Errorf("the card footer is not a row g-2 align-items-center of exactly three col-auto columns "+
			"(range text, rows-per-page selector, pagination bar with ms-auto), closing the card:\n%s", footer)
	}
}

// stripTemplateComments removes whitespace-only noise html/template leaves where
// a template comment stood.
func stripTemplateComments(s string) string { return strings.TrimSpace(s) }

// TestTaskList_RowContent is the gate for Acceptance Criteria 85, 86, 91, and 93
// on the list, and for the list half of Acceptance Criterion 61: each row carries,
// in order, the id badge, the title link, the type badge, the status badge, the S
// and P badges, and the created date in a <time> preceded by the calendar icon in
// a text-secondary cell, and nothing else — no sprint, no Sprint #<id> text, no em
// dash, no View link; exactly one link, the title, to the task's page, with no
// aria-label; no row carrying an href, a role, a tabindex, or an event handler;
// and no comment, subtask, or dependency count.
func TestTaskList_RowContent(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 12)
	mux := buildMux()
	body := servePage(t, mux, listPath(f.name, nil))
	main := boardRegion(t, body)

	inSprint := 0
	for _, task := range f.tasks {
		if task.sprint != 0 {
			inSprint++
		}
		row := rowMarkupOf(t, main, task.id, "the tasks list")
		link := `/roadmaps/` + f.name + `/tasks/` + itoa(task.id)
		want := `<tr>` +
			`<td><span class="badge bg-black text-white">#` + itoa(task.id) + `</span></td>` +
			`<td class="task-list__title"><a href="` + link + `">` + rendered(task.title) + `</a></td>` +
			`<td><span class="badge ` + wantTaskTypeVariant[task.taskType] + `">` + string(task.taskType) + `</span></td>` +
			`<td><span class="badge ` + taskStatusBadge(task.status) + `">` + string(task.status) + `</span></td>` +
			`<td><span class="badge ` + severityBadge(task.severity) + `">S` + itoa(task.severity) + `</span></td>` +
			`<td><span class="badge ` + priorityBadge(task.priority) + `">P` + itoa(task.priority) + `</span></td>` +
			`<td class="text-secondary text-nowrap"><i class="ti ti-calendar me-1" aria-hidden="true"></i>` +
			`<time datetime="` + task.created + `">` + displayOf(task.created) + `</time></td>` +
			`</tr>`
		if got := collapseMarkup(row); got != want {
			t.Errorf("task #%d's row is\n  %s\nwant\n  %s", task.id, got, want)
		}
		if got := strings.Count(row, "<a "); got != 1 {
			t.Errorf("task #%d's row carries %d links, want exactly 1", task.id, got)
		}
		for _, forbidden := range []string{"ti-message", "ti-subtask", "ti-link", "Comments", "Subtasks", "Depends", "Blocks",
			"Sprint #", "&mdash;", ">View<", "aria-label", "task-list__sprint"} {
			if strings.Contains(row, forbidden) {
				t.Errorf("task #%d's row carries %q; the row shows no count", task.id, forbidden)
			}
		}
	}

	if inSprint == 0 || inSprint == len(f.tasks) {
		t.Fatalf("the fixture's tasks are all in sprints or all in none; the absence of a sprint cell proves nothing")
	}
	table := main[mustIndex(t, main, "<table"):mustIndex(t, main, "</table>")]
	for _, forbidden := range []string{"<th>Sprint</th>", "Actions</th>", "Sprint #", "&mdash;"} {
		if strings.Contains(table, forbidden) {
			t.Errorf("the table carries %q; it shows no task's sprint and has no Actions column", forbidden)
		}
	}

	// No <tr> carries an href, a role, a tabindex, or an event handler, and nothing
	// carries role="button" or a tabindex (Acceptance Criterion 93).
	reTR := regexp.MustCompile(`<tr\b([^>]*)>`)
	for _, m := range reTR.FindAllStringSubmatch(body, -1) {
		if strings.TrimSpace(m[1]) != "" {
			t.Errorf("a table row carries attributes %q; a row is not a link", m[1])
		}
	}
	for _, forbidden := range []string{`role="button"`, "tabindex=", " onclick="} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the page carries %q", forbidden)
		}
	}

	// Following a row's link serves that task's page.
	task := f.tasks[0]
	page := servePage(t, mux, "/roadmaps/"+f.name+"/tasks/"+itoa(task.id))
	if !strings.Contains(page, `<div class="page-pretitle">Task #`+itoa(task.id)+` <span class="badge `) {
		t.Errorf("following task #%d's link does not serve its page", task.id)
	}
}

// TestTaskList_TitleColumnIsNotSqueezed is the gate for the served-markup and
// stylesheet half of Acceptance Criteria 128 and 244: no cell of the table
// carries text-break, every Title cell carries the class the project stylesheet
// floors at 16rem, the stylesheet carries no Sprint-cell rule, and the table sits
// inside its table-responsive container. A long title in the fixture
// proves the classes do not depend on the title's length.
func TestTaskList_TitleColumnIsNotSqueezed(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 6)
	database, err := db.Open(f.name)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	long := "Reconcile every acquirer settlement file against the ledger before the nightly payout batch closes for the merchants"
	if _, err := seedTask(database, &models.Task{
		Title: long, Type: models.TypeTask, Status: models.StatusBacklog, Priority: 9, Severity: 1,
		FunctionalRequirements: "Match every settlement line.", TechnicalRequirements: "Stream the file.",
		AcceptanceCriteria: "No unmatched line remains.", CreatedAt: "2026-03-01T08:00:00.000Z",
	}); err != nil {
		t.Fatalf("creating the long-titled task: %v", err)
	}
	_ = database.Close()

	body := servePage(t, buildMux(), listPath(f.name, nil))
	table := body[mustIndex(t, body, `<div class="table-responsive">`):mustIndex(t, body, "</table>")]
	if strings.Contains(table, "text-break") {
		t.Errorf("a cell of the table carries text-break, which lets the title column shrink to a few characters")
	}
	rows := strings.Count(table, "<tr>") - 1
	if rows != 7 {
		t.Fatalf("the table renders %d rows, want 7", rows)
	}
	if got := strings.Count(table, `<td class="task-list__title"><a href=`); got != rows {
		t.Errorf("%d of %d Title cells carry task-list__title", got, rows)
	}
	if !strings.Contains(table, `<td class="task-list__title"><a href="/roadmaps/`+f.name+`/tasks/`) || !strings.Contains(table, long) {
		t.Errorf("the long title is not rendered whole in its Title cell")
	}
	sheet := projectStyleSheet(t)
	if got := cssDeclarations(soleCSSRule(t, sheet, ".task-list__title"), "min-width"); !slices.Equal(got, []string{"16rem"}) {
		t.Errorf("static/style.css declares min-width %v for .task-list__title, want 16rem", got)
	}
	if blocks := cssRuleBlocks(sheet, ".task-list__sprint"); len(blocks) != 0 {
		t.Errorf("static/style.css still carries a rule for the removed Sprint cell: %v", blocks)
	}
}

// displayOf is the display form of a canonical stored timestamp, computed here
// rather than by the helper under test: the date, a space, and the time to the
// second (SPEC/WEB.md § Date and Time Display).
func displayOf(stored string) string {
	return stored[:10] + " " + stored[11:19]
}

// reInterTag matches the whitespace a template leaves between two tags.
var reInterTag = regexp.MustCompile(`>\s+<`)

// collapseMarkup removes the whitespace between tags, so a row can be compared
// with its expected markup whatever the template's indentation.
func collapseMarkup(s string) string {
	return reInterTag.ReplaceAllString(strings.TrimSpace(s), "><")
}

// TestTaskList_ReadOnlyAndScriptFree is the gate for Acceptance Criteria 87, 104,
// 107, and 122: the table carries no selection checkbox and no table-selectable,
// the card no add-task button, the page no modal; the one form is a GET form, and
// its only checkboxes and type="button" buttons are the Status and Type dropdowns';
// the page loads the vendored tabler.min.js alone and no inline script; its
// Content-Security-Policy is the fixed one; no style attribute; and the same URL
// requested twice serves the same list.
func TestTaskList_ReadOnlyAndScriptFree(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 30)
	srv := handler()

	path := listPath(f.name, url.Values{"status": {"DOING"}, "size": {"10"}})
	get := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", path, rec.Code)
		}
		return rec
	}
	rec := get()
	body := rec.Body.String()

	for _, forbidden := range []string{
		"table-selectable", `class="modal`, `data-bs-toggle="modal"`, "New Task",
		"style=", "task-search.js", "sprint-board.js",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the page carries %q", forbidden)
		}
	}
	// Every checkbox and every type="button" button is the filter bar's: one box
	// per TaskStatus and TaskType value, and the two dropdown toggles.
	form := body[mustIndex(t, body, "<form"):mustIndex(t, body, "</form>")]
	wantBoxes := len(models.ValidTaskStatuses) + len(models.ValidTaskTypes)
	if got, inForm := strings.Count(body, `type="checkbox"`), strings.Count(form, `type="checkbox"`); got != wantBoxes || inForm != wantBoxes {
		t.Errorf("the page carries %d checkboxes, %d of them in the filter bar; want the %d dropdown boxes alone", got, inForm, wantBoxes)
	}
	if got, inForm := strings.Count(body, `<button type="button"`), strings.Count(form, `<button type="button"`); got != 2 || inForm != 2 {
		t.Errorf("the page carries %d type=\"button\" buttons, %d in the filter bar; want the two dropdown toggles alone", got, inForm)
	}
	if got := strings.Count(body, "<form"); got != 1 || !strings.Contains(body, `method="get"`) {
		t.Errorf("the page carries %d forms (method get present: %v), want exactly one GET form",
			got, strings.Contains(body, `method="get"`))
	}
	scripts := regexp.MustCompile(`<script\b[^>]*>`).FindAllString(body, -1)
	if len(scripts) != 1 || scripts[0] != `<script src="/static/vendor/tabler/tabler.min.js">` {
		t.Errorf("the page loads the scripts %v, want the vendored tabler.min.js alone", scripts)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Errorf("Content-Security-Policy = %q, want %q", got, contentSecurityPolicy)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	// The list the HTML carries is final: the same URL answers the same list.
	if again := get().Body.String(); !slices.Equal(parseList(t, again).ids, parseList(t, body).ids) {
		t.Errorf("the same URL served two different lists")
	}
}

// ==================== ORDER AND PAGINATION ====================

// TestTaskList_OrderAndPagesCoverEveryTaskOnce is the gate for Acceptance
// Criteria 82, 84, and 239: at every page size the pages together carry every task
// exactly once, in the order priority descending, created_at ascending, id
// ascending — over data holding ties on both of the first two keys — each page
// holding exactly its positions, and only the rows of the requested page in the
// HTML.
func TestTaskList_OrderAndPagesCoverEveryTaskOnce(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 60)
	mux := buildMux()
	want := f.expect(all)

	// The ties the order's id key exists for are in the data.
	ties := 0
	for i := 1; i < len(f.tasks); i++ {
		if f.tasks[i].priority == f.tasks[i-1].priority && f.tasks[i].created == f.tasks[i-1].created {
			ties++
		}
	}
	if ties == 0 {
		t.Fatalf("the fixture holds no two tasks equal on priority and created_at")
	}

	for size, pages := range map[int]int{10: 6, 25: 3, 50: 2, 100: 1} {
		got := allPages(t, mux, f.name, nil, size)
		if !slices.Equal(got, want) {
			t.Errorf("size %d: the pages carry %v, want %v", size, got, want)
		}
		last := fetchList(t, mux, listPath(f.name, url.Values{"size": {itoa(size)}, "page": {itoa(pages)}}))
		if last.currentPage(t) != pages || last.last != 60 {
			t.Errorf("size %d: page %d is not the last page (active %d, last row %d)", size, pages, last.currentPage(t), last.last)
		}
	}

	// No size renders 25 rows, and only the rows of that page are in the HTML.
	s := fetchList(t, mux, listPath(f.name, url.Values{"page": {"2"}}))
	if !slices.Equal(s.ids, want[25:50]) {
		t.Errorf("page 2 at the default size carries %v, want %v", s.ids, want[25:50])
	}
	for _, id := range append(slices.Clone(want[:25]), want[50:]...) {
		if strings.Contains(s.body, `/tasks/`+itoa(id)+`"`) {
			t.Errorf("page 2 carries task #%d, which belongs to another page", id)
		}
	}
}

// TestTaskList_ReadsEveryTaskBeyondTheListingLimit is the gate for the unbounded
// half of Acceptance Criterion 82: a roadmap of more than 100 tasks, requested with
// no criterion, states its full task count, and its pages carry every task.
func TestTaskList_ReadsEveryTaskBeyondTheListingLimit(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const total = models.MaxTaskLimit + 30
	f := seedListFixture(t, "payments-platform", total)
	mux := buildMux()

	s := fetchList(t, mux, listPath(f.name, everyTask()))
	if s.total != total {
		t.Errorf("the range text states %d entries, want the roadmap's %d", s.total, total)
	}
	if got := allPages(t, mux, f.name, nil, 100); !slices.Equal(got, f.expect(all)) {
		t.Errorf("the pages at size 100 carry %d tasks, want all %d in order", len(got), total)
	}
}

// TestTaskList_RangeText is the gate for Acceptance Criterion 83: the footer states
// the filtered total and the positions of the page's first and last rows, each in
// its own span inside <p class="m-0 text-secondary">.
func TestTaskList_RangeText(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 60)
	mux := buildMux()

	for _, c := range []struct {
		params             url.Values
		first, last, total int
	}{
		{everyTask(), 1, 25, 60},
		{url.Values{"page": {"3"}}, 51, 60, 60},
		{url.Values{"status": {"DOING"}}, 1, len(f.expect(func(x listTask) bool { return x.status == models.StatusDoing })),
			len(f.expect(func(x listTask) bool { return x.status == models.StatusDoing }))},
	} {
		body := servePage(t, mux, listPath(f.name, c.params))
		want := fmt.Sprintf(`<p class="m-0 text-secondary">Showing <span>%d</span> to <span>%d</span> of <span>%d</span> entries</p>`,
			c.first, c.last, c.total)
		if !strings.Contains(body, want) {
			t.Errorf("%v: the range text is not %s", c.params, want)
		}
	}
}

// TestTaskList_EmptyStates is the gate for Acceptance Criteria 88 and 102, and the
// empty-state half of 249: with no task to show, the card keeps its header and
// filter bar, renders no table and no footer, and shows Tabler's empty state in a
// card-body. A roadmap that holds no task shows "No tasks yet", with no Reset link,
// whatever the active filter state — the defaults, URL filters, cookie filters, or
// no criterion. A roadmap holding tasks none of which satisfies the active
// criteria — a term, filters, both, or the defaults over a roadmap whose tasks are
// all COMPLETED — shows "No task matches the filters" with the Reset link, whose
// href carries the four default status values. Each answers 200.
func TestTaskList_EmptyStates(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	if err := createEmptyRoadmap("clearing-house"); err != nil {
		t.Fatalf("creating the empty roadmap: %v", err)
	}
	f := seedListFixture(t, "payments-platform", 20)
	closed := seedListFixture(t, "archived-settlements", 6)
	database, err := db.Open(closed.name)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	ids := make([]int, 0, len(closed.tasks))
	for _, task := range closed.tasks {
		ids = append(ids, task.id)
	}
	forceTaskLifecycle(t, database, ids, models.StatusCompleted)
	_ = database.Close()
	mux := buildMux()

	assertEmpty := func(req *http.Request, wantTitle string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", req.URL, rec.Code)
		}
		body := rec.Body.String()
		main := boardRegion(t, body)
		for _, gone := range []string{"<table", `<div class="card-footer">`, "Task list pages", "Rows per page"} {
			if strings.Contains(main, gone) {
				t.Errorf("%s: the empty list still carries %q", req.URL, gone)
			}
		}
		for _, kept := range []string{`<div class="card-header flex-wrap gap-2">`, "<form", `<div class="card-body">`, `<div class="empty">`} {
			if !strings.Contains(main, kept) {
				t.Errorf("%s: the empty list lost %q", req.URL, kept)
			}
		}
		if got := parseList(t, body).emptyTitle; got != wantTitle {
			t.Errorf("%s: the empty state reads %q, want %q", req.URL, got, wantTitle)
		}
		return main
	}
	get := func(path string) *http.Request { return httptest.NewRequest(http.MethodGet, path, nil) }
	withCookie := func(path, value string) *http.Request {
		req := get(path)
		req.AddCookie(&http.Cookie{Name: tasksFilterCookie, Value: value})
		return req
	}

	// A roadmap with no task: "No tasks yet" and no Reset, whatever the state.
	for _, req := range []*http.Request{
		get(listPath("clearing-house", nil)),                                         // the defaults
		get(listPath("clearing-house", everyTask())),                                 // no criterion
		get(listPath("clearing-house", url.Values{"status": {"DOING"}})),             // URL filter
		get(listPath("clearing-house", url.Values{"q": {"cache"}, "type": {"BUG"}})), // term and filter
		withCookie(listPath("clearing-house", nil), "status=DOING&size=25"),          // cookie filter
	} {
		main := assertEmpty(req, "No tasks yet")
		if !strings.Contains(main, "rmp task create") || strings.Contains(main, "empty-action") || strings.Contains(main, ">Reset</a>") {
			t.Errorf("%s: the no-task state does not name rmp task create, or carries a Reset action", req.URL)
		}
	}

	resetHref := "/roadmaps/" + f.name + "/tasks?status=BACKLOG&amp;status=SPRINT&amp;status=DOING&amp;status=TESTING"
	for _, req := range []*http.Request{
		get(listPath(f.name, url.Values{"q": {"no task is titled like this"}})),
		get(listPath(f.name, url.Values{"status": {"DOING"}, "type": {"CHORE"}, "sprint": {"none"}})),
		get(listPath(f.name, url.Values{"q": {"cache"}, "sprint": {"none"}, "status": {"COMPLETED"}, "type": {"SPIKE"}})),
		withCookie(listPath(f.name, nil), "q=no+task+is+titled+like+this&size=25"),
		get(listPath(closed.name, nil)), // the defaults over a roadmap whose tasks are all COMPLETED
	} {
		main := assertEmpty(req, "No task matches the filters")
		action := `<div class="empty-action">`
		at := strings.Index(main, action)
		if at < 0 {
			t.Errorf("%s: the no-match state carries no empty-action", req.URL)
			continue
		}
		name := f.name
		if strings.Contains(req.URL.Path, closed.name) {
			name = closed.name
		}
		want := strings.Replace(resetHref, f.name, name, 1)
		if !strings.Contains(main[at:], `<a class="btn" href="`+want+`">Reset</a>`) {
			t.Errorf("%s: the empty-action does not hold the Reset link to the defaults %s", req.URL, want)
		}
		// The empty state's link is the page's only Reset control.
		if got := strings.Count(main, ">Reset</a>"); got != 1 {
			t.Errorf("%s: the page carries %d Reset links, want the empty state's alone", req.URL, got)
		}
	}
	// The all-COMPLETED roadmap lists its tasks once the defaults are replaced.
	if s := fetchList(t, mux, listPath(closed.name, everyTask())); s.total != len(closed.tasks) {
		t.Errorf("the all-COMPLETED roadmap lists %d tasks with no filter, want %d", s.total, len(closed.tasks))
	}
}

// ==================== READ COST AND BOUND PARAMETERS ====================

// TestTaskList_ReadCost is the gate for Acceptance Criteria 89, 92, and 105 (its
// read half): the sprint titles, the task listing, and the page-rows read — the
// last only when the page renders a row, selecting exactly the rendered page's
// ids and never more than size of them — and no sprint-resolution query, plus
// exactly one task count when, and only when, the
// filtered list is empty and the task read carried a sprint, status, or type
// predicate; the same count for 10 tasks and 300, for every page, page size and
// number of filters, with a term or without, whether the URL, the cookie, or the
// defaults supplied the state; no comment read.
func TestTaskList_ReadCost(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	small := seedListFixture(t, "payments-small", 10)
	large := seedListFixture(t, "payments-large", 300)
	if err := createEmptyRoadmap("clearing-house"); err != nil {
		t.Fatalf("creating the empty roadmap: %v", err)
	}

	cookie := func(v string) *string { return &v }
	type readCase struct {
		req       *tasksRequest
		wantCount int
	}
	for _, f := range []listFixture{small, large} {
		for _, c := range []readCase{
			{explicitTasks(url.Values{}), 0},
			{explicitTasks(url.Values{"page": {"2"}, "size": {"10"}}), 0},
			{explicitTasks(url.Values{"size": {"100"}}), 0},
			// Non-empty lists under predicates, in both fixtures: no count.
			{explicitTasks(url.Values{"status": {"BACKLOG", "SPRINT"}, "type": {"TASK", "BUG", "USER_STORY"}}), 0},
			{explicitTasks(url.Values{"sprint": {itoa(f.sprintA)}, "status": {"BACKLOG", "DOING"}}), 0},
			{explicitTasks(url.Values{"priority": {"3"}, "severity": {"2"}}), 0},
			{explicitTasks(url.Values{"q": {"cache"}, "sprint": {"none"}}), 0},
			{bareTasks(nil), 0},                            // the defaults, rows found
			{bareTasks(cookie("status=DOING&size=10")), 0}, // the cookie, rows found
			// An empty filtered list with no predicate: the listing returned every
			// task, so no count is issued.
			{explicitTasks(url.Values{"q": {"no task is titled like this"}}), 0},
			// An empty filtered list whose read carried a predicate: one count.
			{explicitTasks(url.Values{"q": {"no task is titled like this"}, "status": {"DOING", "TESTING"}}), 1},
			{explicitTasks(url.Values{"status": {"DOING"}, "type": {"CHORE"}, "sprint": {"none"}}), 1},
			{bareTasks(cookie("q=no+task+is+titled+like+this&type=BUG&size=25")), 1},
		} {
			src := openCounting(t, f.name)
			data, err := readTaskList(context.Background(), src, f.name, c.req)
			if err != nil {
				t.Fatalf("%d tasks %+v: readTaskList: %v", len(f.tasks), c.req, err)
			}
			if src.sprintTitles != 1 || src.taskList != 1 || src.groupedTaskSprints != 0 || src.taskCounts != c.wantCount {
				t.Errorf("%d tasks %+v (%d rows): reads = %d sprint titles, %d listings, %d sprint resolutions, %d counts; want 1, 1, 0, %d",
					len(f.tasks), c.req, len(data.Rows), src.sprintTitles, src.taskList, src.groupedTaskSprints, src.taskCounts, c.wantCount)
			}
			if src.groupedCommentCounts+src.perTaskComments+src.sprintComments+src.boundedTaskList+src.sprintListings+src.sprintTasks != 0 {
				t.Errorf("%d tasks %+v: the page issued a read beyond its own", len(f.tasks), c.req)
			}
			// The case data is what the criterion needs: a counted case renders an
			// empty list, and every predicate case without a count renders rows.
			if (c.wantCount == 1) != (len(data.Rows) == 0) && data.query.hasPredicate() {
				t.Fatalf("%d tasks %+v: %d rows; the case does not exercise what it claims", len(f.tasks), c.req, len(data.Rows))
			}
			assertPageRowsRead(t, src, &data, fmt.Sprintf("%d tasks %+v", len(f.tasks), c.req))
		}
	}

	// Every page at every page size: the page-rows read selects the rendered
	// page's ids, in the rendered order, and never more than size of them.
	for _, size := range taskPageSizes {
		for page := 1; ; page++ {
			src := openCounting(t, large.name)
			data, err := readTaskList(context.Background(), src, large.name,
				explicitTasks(url.Values{"size": {itoa(size)}, "page": {itoa(page)}, "status": {"BACKLOG", "SPRINT", "DOING", "TESTING", "COMPLETED"}}))
			if err != nil {
				t.Fatalf("size %d page %d: readTaskList: %v", size, page, err)
			}
			label := fmt.Sprintf("size %d page %d", size, page)
			assertPageRowsRead(t, src, &data, label)
			if len(src.lastPageIDs) > size {
				t.Errorf("%s: the page-rows read selected %d ids, more than the page size", label, len(src.lastPageIDs))
			}
			if data.Page >= data.Pages {
				if data.Total != len(large.tasks) {
					t.Fatalf("%s: total %d, want every one of the %d tasks", label, data.Total, len(large.tasks))
				}
				break
			}
		}
	}

	// A roadmap with no task: the defaults carry a status predicate, so the empty
	// list takes the count, which says the roadmap holds nothing; an empty list
	// with no predicate needs none.
	for _, c := range []readCase{{bareTasks(nil), 1}, {explicitTasks(everyTask()), 0}} {
		src := openCounting(t, "clearing-house")
		data, err := readTaskList(context.Background(), src, "clearing-house", c.req)
		if err != nil {
			t.Fatalf("empty roadmap: readTaskList: %v", err)
		}
		if src.taskCounts != c.wantCount || !data.NoTasks || data.NoMatch {
			t.Errorf("empty roadmap %+v: %d counts (want %d), NoTasks %v, NoMatch %v; want the no-task state",
				c.req, src.taskCounts, c.wantCount, data.NoTasks, data.NoMatch)
		}
	}
}

// assertPageRowsRead asserts the page-rows half of Acceptance Criterion 89 on one
// render: the read is issued exactly when the page renders a row, and the ids it
// selects are exactly the rendered rows' ids, in the rendered order.
func assertPageRowsRead(t *testing.T, src *countingSource, data *tasksData, label string) {
	t.Helper()
	want := 0
	if len(data.Rows) > 0 {
		want = 1
	}
	if src.pageRows != want {
		t.Errorf("%s: %d page-rows reads for %d rendered rows, want %d", label, src.pageRows, len(data.Rows), want)
	}
	rendered := make([]int, len(data.Rows))
	for i := range data.Rows {
		rendered[i] = data.Rows[i].ID
	}
	if !slices.Equal(src.lastPageIDs, rendered) && (len(src.lastPageIDs) != 0 || len(rendered) != 0) {
		t.Errorf("%s: the page-rows read selected %v, the page rendered %v", label, src.lastPageIDs, rendered)
	}
}

// TestTaskList_FilterValuesAreBoundAndHostileValuesReachNothing is the gate for
// the web half of Acceptance Criteria 113, 117, and 254: an accepted value reaches
// the task read as a field of the listing's filter — each distinct value once; the
// listing binds each one as a parameter, which internal/db's
// TestTaskListing_BindsEveryFilterValue pins on the SQL text — and a hostile
// value, from the URL or from the cookie, is ignored and reaches no statement at
// all; the roadmap's tasks are intact afterwards; q, page and size never reach
// the read, nor does any priority or severity parameter; and no filter value is
// echoed into the page as text.
func TestTaskList_FilterValuesAreBoundAndHostileValuesReachNothing(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 30)

	src := openCounting(t, f.name)
	if _, err := readTaskList(context.Background(), src, f.name, explicitTasks(url.Values{
		"status": {"TESTING", "DOING", "doing", "DOING"}, "type": {"BUG", "EPIC", "BUG"}, "priority": {"4"}, "severity": {"0"},
		"sprint": {itoa(f.sprintB)}, "q": {"cache"}, "page": {"2"}, "size": {"10"},
	})); err != nil {
		t.Fatalf("readTaskList: %v", err)
	}
	got := src.lastTaskFilter
	if got == nil || !slices.Equal(got.Statuses, []models.TaskStatus{models.StatusDoing, models.StatusTesting}) ||
		!slices.Equal(got.TaskTypes, []models.TaskType{models.TypeBug, models.TypeEpic}) ||
		got.SprintID == nil || *got.SprintID != f.sprintB || got.NoSprint || got.Status != nil || got.TaskType != nil {
		t.Errorf("the accepted values did not all reach the listing's filter, each once: %+v", got)
	}
	// priority and severity are not parameters of the page: whatever the request
	// carries, the read has no priority and no severity predicate.
	if got != nil && (got.MinPriority != nil || got.MinSeverity != nil) {
		t.Errorf("a priority or severity parameter reached the listing's filter: %+v", got)
	}
	if got != nil && (got.Limit != 0 || got.Sort != "") {
		t.Errorf("the page set a limit or sort on the listing (%d, %q); page and size never reach it", got.Limit, got.Sort)
	}

	before := countRoadmapTasks(t, f.name)
	hostiles := []url.Values{
		{"status": {"DOING' OR '1'='1"}},
		{"type": {"BUG;DROP TABLE tasks"}},
		{"sprint": {"1 OR 1=1"}},
		{"priority": {"1; DELETE FROM tasks"}},
		{"severity": {"0) OR (1=1"}},
	}
	for _, hostile := range hostiles {
		for _, req := range []*tasksRequest{explicitTasks(hostile), bareTasks(new(hostile.Encode()))} {
			src := openCounting(t, f.name)
			if _, err := readTaskList(context.Background(), src, f.name, req); err != nil {
				t.Fatalf("%v: readTaskList: %v", hostile, err)
			}
			if g := src.lastTaskFilter; g == nil || len(g.Statuses) != 0 || len(g.TaskTypes) != 0 || g.Status != nil ||
				g.TaskType != nil || g.MinPriority != nil || g.MinSeverity != nil || g.SprintID != nil || g.NoSprint {
				t.Errorf("%v (explicit %v): the ignored value reached the listing's filter: %+v", hostile, req.explicit, g)
			}
		}
		body := servePage(t, buildMux(), listPath(f.name, hostile))
		for _, v := range hostile {
			if strings.Contains(body, html.EscapeString(v[0])) || strings.Contains(body, url.QueryEscape(v[0])) {
				t.Errorf("%v: the ignored value is echoed into the page", hostile)
			}
		}
	}
	if after := countRoadmapTasks(t, f.name); after != before {
		t.Errorf("the roadmap held %d tasks before the hostile requests and %d after", before, after)
	}
}

// ==================== THE FILTER BAR ====================

// TestTaskList_FilterBarControls is the gate for Acceptance Criteria 100, 112, 116,
// 234, and the served-markup half of 243: the form's controls, in order — the
// search input, the sprint select, the Status and Type dropdowns, and Apply —
// each the compact Tabler variant with exactly one programmatically associated
// label carrying visually-hidden, and nothing else: no priority or severity
// control and no Reset control. Each dropdown is a toggle button styled as a small
// select, described by the span holding its text, over a menu of one checkbox per
// enum value in enum order and no any box. On every response the sprint select
// marks exactly one option selected, each dropdown checks exactly the active
// values, and its toggle reads the any text, the one value, or "<n> selected" —
// for zero, one, two, and every value. The sprint select carries the class the
// project stylesheet caps at 16rem; for a roadmap with no sprint it offers its two
// fixed options only, and two sprints of one title differ by their Sprint #<id>
// text.
func TestTaskList_FilterBarControls(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 10)
	mux := buildMux()
	body := servePage(t, mux, listPath(f.name, everyTask()))
	form := body[mustIndex(t, body, "<form"):mustIndex(t, body, "</form>")]

	// The controls, in order, each labelled by a visually hidden label.
	order := []string{
		`<form class="row g-2 align-items-end justify-content-end" method="get" action="/roadmaps/` + f.name + `/tasks">`,
		`<label class="visually-hidden" for="task-filter-q">Search</label>`,
		`<input type="search" class="form-control form-control-sm" id="task-filter-q" name="q" placeholder="Search" value="">`,
		`<label class="visually-hidden" for="task-filter-sprint">Sprint</label>`,
		`<select class="form-select form-select-sm task-list__sprint-select" id="task-filter-sprint" name="sprint">`,
		`<label class="visually-hidden" for="task-filter-status">Status</label>`,
		`<div class="dropdown">`,
		`<button type="button" class="form-select form-select-sm" id="task-filter-status" data-bs-toggle="dropdown" data-bs-auto-close="outside" aria-describedby="task-filter-status-text"><span id="task-filter-status-text">Any status</span></button>`,
		`<div class="dropdown-menu">`,
		`<label class="visually-hidden" for="task-filter-type">Type</label>`,
		`<div class="dropdown">`,
		`<button type="button" class="form-select form-select-sm" id="task-filter-type" data-bs-toggle="dropdown" data-bs-auto-close="outside" aria-describedby="task-filter-type-text"><span id="task-filter-type-text">Any type</span></button>`,
		`<div class="dropdown-menu">`,
		`<button type="submit" class="btn btn-primary btn-sm">Apply</button>`,
	}
	// Exactly one visually hidden label per filter control; every other label of
	// the form is a checkbox's dropdown-item (Acceptance Criterion 243).
	reLabel := regexp.MustCompile(`<label([^>]*)>`)
	labels := reLabel.FindAllStringSubmatch(form, -1)
	hidden := 0
	for _, l := range labels {
		switch l[1] {
		case ` class="dropdown-item"`:
		default:
			hidden++
			if !strings.Contains(l[1], `class="visually-hidden"`) {
				t.Errorf("the label %q does not carry visually-hidden", l[0])
			}
		}
	}
	if hidden != 4 {
		t.Errorf("the filter bar carries %d control labels, want exactly one for each of its four filter controls", hidden)
	}
	if want := 4 + len(models.ValidTaskStatuses) + len(models.ValidTaskTypes); len(labels) != want {
		t.Errorf("the filter bar carries %d labels, want %d: four control labels and one per checkbox", len(labels), want)
	}
	for _, forbidden := range []string{`name="priority"`, `name="severity"`, "Reset", "<a "} {
		if strings.Contains(form, forbidden) {
			t.Errorf("the filter bar carries %q; it offers no priority or severity filter and no Reset control", forbidden)
		}
	}
	for _, id := range []string{"q", "sprint", "status", "type"} {
		if got := strings.Count(form, `for="task-filter-`+id+`"`); got != 1 {
			t.Errorf("the control task-filter-%s has %d labels, want exactly 1", id, got)
		}
		if got := strings.Count(body, `id="task-filter-`+id+`"`); got != 1 {
			t.Errorf("the id task-filter-%s is carried %d times, want exactly 1", id, got)
		}
	}
	// The id each toggle's aria-describedby names is unique in the page and is
	// the span inside that toggle.
	for _, name := range []string{"status", "type"} {
		toggle := parseList(t, body).toggles[name]
		if toggle.describedBy == "" || toggle.describedBy != toggle.spanID || strings.Count(body, `id="`+toggle.spanID+`"`) != 1 {
			t.Errorf("the %s toggle's aria-describedby %q does not name its own span (%q) uniquely", name, toggle.describedBy, toggle.spanID)
		}
	}
	// The sprint select's class is capped at 16rem by the project stylesheet, from
	// the 576px breakpoint up only (Acceptance Criterion 243).
	var inside, outside []string
	for _, rule := range parseCSSRules(projectStyleSheet(t)) {
		for _, sel := range strings.Split(rule.prelude, ",") {
			if normaliseSelector(sel) != ".task-list__sprint-select" {
				continue
			}
			if rule.nested {
				inside = append(inside, cssDeclarations(rule.decls, "max-width")...)
			} else {
				outside = append(outside, cssDeclarations(rule.decls, "max-width")...)
			}
		}
	}
	if !slices.Equal(inside, []string{"16rem"}) || len(outside) != 0 {
		t.Errorf("static/style.css caps .task-list__sprint-select at %v inside a media query and %v outside one; "+
			"want 16rem inside only", inside, outside)
	}
	if !regexp.MustCompile(`@media \(min-width: 576px\) \{\s*\.task-list__sprint-select \{ max-width: 16rem; \}\s*\}`).MatchString(projectStyleSheet(t)) {
		t.Errorf("the 16rem cap of the sprint select is not inside @media (min-width: 576px)")
	}
	cursor := 0
	for _, piece := range order {
		at := strings.Index(form[cursor:], piece)
		if at < 0 {
			t.Fatalf("the filter bar does not carry %s in its place", piece)
		}
		cursor += at + len(piece)
	}
	if strings.Count(form, "placeholder=") != 1 || strings.Contains(form, `name="page"`) {
		t.Errorf("the form carries a placeholder other than the search input's, or a page field")
	}
	if !strings.Contains(form, `<input type="hidden" name="size" value="25">`) {
		t.Errorf("the form does not carry the active page size in a hidden size input")
	}
	// Every control sits in a col-12 col-sm-auto column of the form's grid
	// (Acceptance Criterion 129).
	if got := strings.Count(form, `<div class="col-12 col-sm-auto">`); got != 5 {
		t.Errorf("the form holds %d col-12 col-sm-auto columns, want 5", got)
	}

	s := parseList(t, body)
	sprintValues := make([]string, 0, len(s.selects["sprint"]))
	sprintLabels := make([]string, 0, len(s.selects["sprint"]))
	for _, o := range s.selects["sprint"] {
		sprintValues = append(sprintValues, o.value)
		sprintLabels = append(sprintLabels, o.label)
	}
	if want := []string{"", "none", itoa(f.sprintA), itoa(f.sprintB)}; !slices.Equal(sprintValues, want) {
		t.Errorf("the sprint select offers %v, want %v", sprintValues, want)
	}
	if want := []string{"Any sprint", "No sprint", "Sprint #" + itoa(f.sprintA) + " Checkout hardening",
		"Sprint #" + itoa(f.sprintB) + " Settlement reconciliation"}; !slices.Equal(sprintLabels, want) {
		t.Errorf("the sprint select reads %v, want %v", sprintLabels, want)
	}
	boxValues := func(name string) []string {
		values := make([]string, 0, len(s.checks[name]))
		for _, c := range s.checks[name] {
			if c.label != c.value {
				t.Errorf("the %s box %q is labelled %q; its label is its value", name, c.value, c.label)
			}
			values = append(values, c.value)
		}
		return values
	}
	wantStatuses := make([]string, 0, len(models.ValidTaskStatuses))
	for _, st := range models.ValidTaskStatuses {
		wantStatuses = append(wantStatuses, string(st))
	}
	if got := boxValues("status"); !slices.Equal(got, []string{"BACKLOG", "SPRINT", "DOING", "TESTING", "COMPLETED"}) ||
		!slices.Equal(got, wantStatuses) {
		t.Errorf("the status menu offers %v", got)
	}
	wantTypes := make([]string, 0, len(models.ValidTaskTypes))
	for _, tt := range models.ValidTaskTypes {
		wantTypes = append(wantTypes, string(tt))
	}
	if got := boxValues("type"); !slices.Equal(got, wantTypes) {
		t.Errorf("the type menu offers %v, want %v", got, wantTypes)
	}
	if len(s.selects) != 1 || len(s.checks) != 2 {
		t.Errorf("the filter bar carries %d selects and %d dropdowns, want the sprint select and the two dropdowns", len(s.selects), len(s.checks))
	}
	if got := s.selected(t, "sprint"); got != "" {
		t.Errorf("with no filter the sprint select selects %q, want its any option", got)
	}
	for name, anyText := range map[string]string{"status": "Any status", "type": "Any type"} {
		if got := s.checked(t, name); len(got) != 0 || s.toggleText(t, name) != anyText {
			t.Errorf("with no filter the %s dropdown checks %v and reads %q, want nothing checked and %q",
				name, got, s.toggleText(t, name), anyText)
		}
	}

	// Each control shows the active value that produced the list — zero, one, two,
	// and every value of a dimension — and the search input shows the term.
	allTypes := make([]string, 0, len(models.ValidTaskTypes))
	for _, tt := range models.ValidTaskTypes {
		allTypes = append(allTypes, string(tt))
	}
	for _, c := range []struct {
		params               url.Values
		sprint               string
		statuses, types      []string
		statusText, typeText string
	}{
		{url.Values{"q": {"Refund cache"}, "sprint": {"none"}, "status": {"TESTING"}, "type": {"EPIC"}},
			"none", []string{"TESTING"}, []string{"EPIC"}, "TESTING", "EPIC"},
		{url.Values{"status": {"TESTING", "DOING"}, "type": {"BUG", "TASK", "BUG"}, "sprint": {itoa(f.sprintB)}},
			itoa(f.sprintB), []string{"DOING", "TESTING"}, []string{"TASK", "BUG"}, "2 selected", "2 selected"},
		{url.Values{"status": {"COMPLETED", "TESTING", "DOING", "SPRINT", "BACKLOG"}, "type": allTypes},
			"", []string{"BACKLOG", "SPRINT", "DOING", "TESTING", "COMPLETED"}, allTypes, "5 selected", "10 selected"},
	} {
		chosen := parseList(t, servePage(t, mux, listPath(f.name, c.params)))
		if got := chosen.selected(t, "sprint"); got != c.sprint {
			t.Errorf("%v: the sprint select selects %q, want %q", c.params, got, c.sprint)
		}
		if got := chosen.checked(t, "status"); !slices.Equal(got, c.statuses) || chosen.toggleText(t, "status") != c.statusText {
			t.Errorf("%v: the status dropdown checks %v and reads %q, want %v and %q", c.params, got, chosen.toggleText(t, "status"), c.statuses, c.statusText)
		}
		if got := chosen.checked(t, "type"); !slices.Equal(got, c.types) || chosen.toggleText(t, "type") != c.typeText {
			t.Errorf("%v: the type dropdown checks %v and reads %q, want %v and %q", c.params, got, chosen.toggleText(t, "type"), c.types, c.typeText)
		}
		if chosen.search != c.params.Get("q") {
			t.Errorf("%v: the search input shows %q, want the active q", c.params, chosen.search)
		}
	}

	// A roadmap with no sprint, and a roadmap with two sprints of one title.
	if err := createEmptyRoadmap("clearing-house"); err != nil {
		t.Fatalf("creating the empty roadmap: %v", err)
	}
	if got := parseList(t, servePage(t, mux, listPath("clearing-house", nil))).selects["sprint"]; len(got) != 2 {
		t.Errorf("a roadmap with no sprint offers %d sprint options, want Any sprint and No sprint only", len(got))
	}
	database, err := db.Open("twin-sprints")
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	first := newSprint(t, database, "Quarterly hardening", "The first of two sprints sharing a title.")
	second := newSprint(t, database, "Quarterly hardening", "The second of two sprints sharing a title.")
	_ = database.Close()
	twin := parseList(t, servePage(t, mux, listPath("twin-sprints", nil))).selects["sprint"]
	if len(twin) != 4 || twin[2].label != "Sprint #"+itoa(first)+" Quarterly hardening" ||
		twin[3].label != "Sprint #"+itoa(second)+" Quarterly hardening" {
		t.Errorf("two sprints of one title offer %+v; their options must differ by the Sprint #<id> text", twin)
	}
}

// TestTaskList_EachFilterMatchesItsDimension is the gate for Acceptance Criteria
// 113 and 235: status and type are equalities, and sprint is membership — none
// admits the tasks of no sprint whatever their status, an id the tasks of that
// sprint, and the lists of none and of every sprint id together hold every task
// exactly once; priority and severity are not parameters of the page, and with any
// value either lists exactly what the request lists without it and reaches no
// generated link.
func TestTaskList_EachFilterMatchesItsDimension(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 45)
	mux := buildMux()

	check := func(params url.Values, keep func(listTask) bool) []int {
		t.Helper()
		got := allPages(t, mux, f.name, params, 100)
		if want := f.expect(keep); !slices.Equal(got, want) {
			t.Errorf("%v lists %v, want %v", params, got, want)
		}
		return got
	}
	for _, status := range models.ValidTaskStatuses {
		check(url.Values{"status": {string(status)}}, func(x listTask) bool { return x.status == status })
	}
	for _, taskType := range models.ValidTaskTypes {
		check(url.Values{"type": {string(taskType)}}, func(x listTask) bool { return x.taskType == taskType })
	}
	// The page offers no priority or severity filter: either parameter, with any
	// value, lists exactly what the request lists without it, and no generated
	// link carries it.
	for _, param := range []string{"priority", "severity"} {
		for _, value := range []string{"7", "0", "9", "abc", ""} {
			check(url.Values{param: {value}}, all)
			check(url.Values{param: {value}, "status": {"DOING"}}, func(x listTask) bool { return x.status == models.StatusDoing })
			s := fetchList(t, mux, listPath(f.name, url.Values{param: {value}, "size": {"10"}}))
			for _, link := range generatedLinks(&s) {
				if u, err := url.Parse(link); err != nil || u.Query().Has(param) {
					t.Errorf("%s=%q: the generated link %q carries it", param, value, link)
				}
			}
		}
	}

	// Sprint membership, and the partition it makes.
	backlogInSprint := false
	for _, task := range f.tasks {
		if task.status == models.StatusBacklog && task.sprint != 0 {
			backlogInSprint = true
		}
	}
	if !backlogInSprint {
		t.Fatalf("the fixture holds no BACKLOG task that is a sprint member")
	}
	none := check(url.Values{"sprint": {"none"}}, func(x listTask) bool { return x.sprint == 0 })
	a := check(url.Values{"sprint": {itoa(f.sprintA)}}, func(x listTask) bool { return x.sprint == f.sprintA })
	b := check(url.Values{"sprint": {itoa(f.sprintB)}}, func(x listTask) bool { return x.sprint == f.sprintB })
	union := append(append(slices.Clone(none), a...), b...)
	slices.Sort(union)
	every := make([]int, 0, len(f.tasks))
	for _, task := range f.tasks {
		every = append(every, task.id)
	}
	slices.Sort(every)
	if !slices.Equal(union, every) {
		t.Errorf("the lists of none and of every sprint hold %v, want every task once %v", union, every)
	}
	check(url.Values{"sprint": {itoa(f.sprintA)}, "status": {"DOING"}}, func(x listTask) bool {
		return x.sprint == f.sprintA && x.status == models.StatusDoing
	})
}

// TestTaskList_CriteriaComposeInEveryCombination is the gate for Acceptance
// Criteria 114 and 238: each of the 16 combinations of the four criteria present or
// absent lists exactly the tasks satisfying every present criterion, in the order
// of Acceptance Criterion 84, with the range text stating their number; the order
// of the parameters in the query string changes nothing.
func TestTaskList_CriteriaComposeInEveryCombination(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 48)
	mux := buildMux()

	type criterion struct {
		key, value string
		keep       func(listTask) bool
	}
	criteria := []criterion{
		{"q", "cache", func(x listTask) bool { return strings.Contains(strings.ToLower(x.title), "cache") }},
		{"sprint", itoa(f.sprintA), func(x listTask) bool { return x.sprint == f.sprintA }},
		{"status", "SPRINT", func(x listTask) bool { return x.status == models.StatusSprint }},
		{"type", "TASK", func(x listTask) bool { return x.taskType == models.TypeTask }},
	}
	// Each criterion excludes a different subset.
	seen := map[string]bool{}
	for _, c := range criteria {
		key := fmt.Sprint(f.expect(c.keep))
		if seen[key] || len(f.expect(c.keep)) == len(f.tasks) {
			t.Fatalf("criterion %s=%s excludes nothing or the same subset as another", c.key, c.value)
		}
		seen[key] = true
	}

	nonEmpty := 0
	for mask := 0; mask < 1<<len(criteria); mask++ {
		params := url.Values{}
		var present []criterion
		for i, c := range criteria {
			if mask&(1<<i) != 0 {
				params.Set(c.key, c.value)
				present = append(present, c)
			}
		}
		want := f.expect(func(x listTask) bool {
			for _, c := range present {
				if !c.keep(x) {
					return false
				}
			}
			return true
		})
		if len(want) > 0 {
			nonEmpty++
		}
		got := allPages(t, mux, f.name, params, 100)
		if !slices.Equal(got, want) {
			t.Errorf("mask %04b %v lists %v, want %v", mask, params, got, want)
		}
		if len(want) > 0 {
			explicit := params
			if len(explicit) == 0 {
				explicit = everyTask() // a request with no parameter would be bare
			}
			if s := fetchList(t, mux, listPath(f.name, explicit)); s.total != len(want) {
				t.Errorf("mask %04b: the range text states %d entries, want %d", mask, s.total, len(want))
			}
		}
	}
	if nonEmpty < 8 {
		t.Errorf("only %d of the 16 combinations list any task; the data does not exercise the conjunction", nonEmpty)
	}

	// The order of the parameters carries no meaning.
	forward := servePage(t, mux, "/roadmaps/"+f.name+"/tasks?status=SPRINT&type=TASK&q=cache")
	backward := servePage(t, mux, "/roadmaps/"+f.name+"/tasks?q=cache&type=TASK&status=SPRINT")
	if !slices.Equal(parseList(t, forward).ids, parseList(t, backward).ids) {
		t.Errorf("the same parameters in another order list different tasks")
	}
}

// TestTaskList_UnacceptableParametersAreIgnored is the gate for Acceptance
// Criteria 115 and 236: for each filter parameter and each unacceptable value the
// response is 200 with Cache-Control no-store, lists exactly what the request
// lists without that parameter, shows the parameter's any state — the sprint
// select's first option, or no box of the dropdown checked — and carries the value
// in no generated link; the other parameters stay applied. For status and type the
// rule applies to each occurrence on its own; a repeated sprint is read as its
// first occurrence; unknown parameters are ignored.
func TestTaskList_UnacceptableParametersAreIgnored(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 40)
	other := seedListFixture(t, "treasury-platform", 3)
	// A sprint id of the other roadmap that is not a sprint of this one.
	database, err := db.Open(other.name)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	foreign := newSprint(t, database, "Treasury close", "A sprint of another roadmap.")
	_ = database.Close()
	if foreign == f.sprintA || foreign == f.sprintB {
		t.Fatalf("the foreign sprint id %d collides with this roadmap's sprints", foreign)
	}
	srv := handler()

	serve := func(rawQuery string) (servedList, *httptest.ResponseRecorder) {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmaps/"+f.name+"/tasks?"+rawQuery, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("?%s: status = %d, want 200", rawQuery, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("?%s: Cache-Control = %q, want no-store", rawQuery, got)
		}
		return parseList(t, rec.Body.String()), rec
	}

	// Each case carries an accepted value of another filter, so every case also
	// proves the other parameters stay applied.
	bad := map[string][]string{
		"status": {"doing", "Doing", "BLOCKED", "%20DOING", "DOING%20", "", "DOING,SPRINT", "%zz"},
		"type":   {"bug", "Bug", "FEATURE", "%20BUG", "", "BUG,EPIC", "%zz"},
		"sprint": {"NONE", "None", "007", "0", itoa(foreign), "999", "%20none", "", "-1", "%zz"},
	}
	keep := map[string]string{"status": "type=TASK", "type": "status=SPRINT", "sprint": "status=SPRINT"}
	for param, values := range bad {
		baseline, _ := serve(keep[param] + "&size=100")
		keptParam, keptValue, _ := strings.Cut(keep[param], "=")
		for _, value := range values {
			s, rec := serve(param + "=" + value + "&" + keep[param] + "&size=100")
			if !slices.Equal(s.ids, baseline.ids) {
				t.Errorf("%s=%q lists %v, want the list without it %v", param, value, s.ids, baseline.ids)
			}
			if param == "sprint" {
				if got := s.selected(t, param); got != "" {
					t.Errorf("%s=%q: the select selects %q, want its any option", param, value, got)
				}
			} else if got := s.checked(t, param); len(got) != 0 {
				t.Errorf("%s=%q: the dropdown checks %v, want no box", param, value, got)
			}
			if got := s.checked(t, keptParam); !slices.Equal(got, []string{keptValue}) {
				t.Errorf("%s=%q: the accepted %s is no longer the one checked value (%v)", param, value, keptParam, got)
			}
			for _, link := range generatedLinks(&s) {
				if u, perr := url.Parse(link); perr != nil || u.Query().Has(param) {
					t.Errorf("%s=%q: the generated link %q carries the ignored parameter", param, value, link)
				}
			}
			_ = rec
		}
	}
	// priority and severity are not parameters of the page: any value, accepted by
	// the CLI or not, lists what the request lists without it.
	unfiltered, _ := serve("size=100")
	for _, param := range []string{"priority", "severity"} {
		for _, value := range []string{"5", "0", "10", "-1", "abc", "", "%zz"} {
			s, _ := serve(param + "=" + value + "&size=100")
			if !slices.Equal(s.ids, unfiltered.ids) {
				t.Errorf("%s=%q changed the list", param, value)
			}
		}
	}

	// Each occurrence of a repeatable parameter is validated on its own: an
	// unaccepted one is ignored and the others stay applied; two accepted ones both
	// apply. A repeated sprint is read as its first occurrence.
	bugs := f.expect(func(x listTask) bool { return x.taskType == models.TypeBug })
	for _, query := range []string{"type=BUG&type=bug", "type=bug&type=BUG", "type=BUG&type=%zz", "type=BUG,EPIC&type=BUG"} {
		if got, _ := serve(query + "&size=100"); !slices.Equal(got.ids, bugs) || !slices.Equal(got.checked(t, "type"), []string{"BUG"}) {
			t.Errorf("%s lists %v with %v checked, want the BUG tasks %v with BUG alone checked", query, got.ids, got.checked(t, "type"), bugs)
		}
	}
	repeated, _ := serve("type=BUG&type=EPIC&size=100")
	if want := f.expect(func(x listTask) bool {
		return x.taskType == models.TypeBug || x.taskType == models.TypeEpic
	}); !slices.Equal(repeated.ids, want) {
		t.Errorf("type=BUG&type=EPIC lists %v, want the BUG and EPIC tasks %v", repeated.ids, want)
	}
	firstSprint, _ := serve("sprint=" + itoa(f.sprintA) + "&sprint=" + itoa(f.sprintB) + "&size=100")
	if want := f.expect(func(x listTask) bool { return x.sprint == f.sprintA }); !slices.Equal(firstSprint.ids, want) {
		t.Errorf("a repeated sprint lists %v, want the first sprint's tasks %v", firstSprint.ids, want)
	}
	// Parameters not among the six are ignored.
	unknown, _ := serve("assignee=alice&sort=title&limit=5&size=100")
	if !slices.Equal(unknown.ids, unfiltered.ids) {
		t.Errorf("unknown parameters changed the list")
	}
}

// generatedLinks returns every link the page generates to itself: the pagination
// bar's items and chevrons, the rows-per-page links, and the no-match empty
// state's Reset link where the page renders it.
func generatedLinks(s *servedList) []string {
	var links []string
	for _, item := range s.pageItems {
		if item.href != "" {
			links = append(links, item.href)
		}
	}
	for _, link := range s.sizeLinks {
		links = append(links, link.href)
	}
	for _, href := range []string{s.prevHref, s.nextHref, s.resetHref} {
		if href != "" {
			links = append(links, href)
		}
	}
	return links
}

// submitFilterBar submits the served filter bar as a browser does: every named
// field as the served HTML defines it — the search input and the hidden size, the
// sprint select's selected option, and one field per checked box — with the given
// fields changed, returning the URL the submission requests. A change replaces
// every occurrence of its field; a nil change of status or type unchecks every box
// of that dropdown.
func submitFilterBar(t *testing.T, body string, changes map[string][]string) string {
	t.Helper()
	form := body[mustIndex(t, body, "<form"):mustIndex(t, body, "</form>")]
	action := regexp.MustCompile(`action="([^"]*)"`).FindStringSubmatch(form)[1]
	fields := url.Values{}
	for _, m := range regexp.MustCompile(`<input type="(?:hidden|search)"[^>]* name="([a-z]+)"(?: placeholder="[^"]*")? value="([^"]*)">`).FindAllStringSubmatch(form, -1) {
		fields.Add(m[1], html.UnescapeString(m[2]))
	}
	s := parseList(t, body)
	fields.Set("sprint", s.selected(t, "sprint"))
	for _, name := range []string{"status", "type"} {
		for _, value := range s.checked(t, name) {
			fields.Add(name, value)
		}
	}
	if !fields.Has("q") || !fields.Has("size") || !fields.Has("sprint") {
		t.Fatalf("the served form submits %v; want q, sprint, and size, and one field per checked box", fields)
	}
	for name, values := range changes {
		if values == nil {
			fields.Del(name)
			continue
		}
		fields[name] = values
	}
	return html.UnescapeString(action) + "?" + fields.Encode()
}

// TestTaskList_ApplyingTheFormReturnsToPageOneAndKeepsTheSize is the gate for
// Acceptance Criteria 116 and 237: on page 3 at size 10, submitting the real form
// with a new status box checked requests a URL carrying size=10 and no page and
// renders page 1 of the new list at size 10; reloading it renders the same list;
// submitting the form again with the sprint select on Any sprint, every box
// unchecked, and an empty search input renders page 1 of the unfiltered list —
// COMPLETED tasks included — at size 10. At the default size the form still
// submits size=25.
func TestTaskList_ApplyingTheFormReturnsToPageOneAndKeepsTheSize(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 60)
	mux := buildMux()

	start := servePage(t, mux, listPath(f.name, url.Values{"page": {"3"}, "size": {"10"}, "q": {"the"}}))
	if startList := parseList(t, start); startList.currentPage(t) != 3 {
		t.Fatalf("the starting page is not page 3")
	}
	submitted := submitFilterBar(t, start, map[string][]string{"status": {"DOING"}})
	u, _ := url.Parse(submitted)
	if u.Query().Has("page") || u.Query().Get("size") != "10" {
		t.Fatalf("the submitted form requests %q; want no page and size 10", submitted)
	}

	want := f.expect(func(x listTask) bool {
		return x.status == models.StatusDoing && strings.Contains(strings.ToLower(x.title), "the")
	})
	filteredBody := servePage(t, mux, submitted)
	filtered := parseList(t, filteredBody)
	if filtered.currentPage(t) != 1 || !slices.Equal(filtered.ids, want[:min(10, len(want))]) || !slices.Equal(filtered.checked(t, "status"), []string{"DOING"}) {
		t.Errorf("the submitted form renders page %d with %v, want page 1 of %v at size 10", filtered.currentPage(t), filtered.ids, want)
	}
	if reload := fetchList(t, mux, submitted); !slices.Equal(reload.ids, filtered.ids) {
		t.Errorf("reloading the submitted URL renders a different list")
	}

	// Clearing: Any sprint, every box unchecked, and an empty search input.
	cleared := fetchList(t, mux, submitFilterBar(t, filteredBody, map[string][]string{"q": {""}, "sprint": {""}, "status": nil, "type": nil}))
	if cleared.currentPage(t) != 1 || !slices.Equal(cleared.ids, f.expect(all)[:10]) || len(cleared.checked(t, "status")) != 0 ||
		cleared.total != len(f.tasks) {
		t.Errorf("clearing the form renders page %d with %v, want page 1 of the unfiltered list at size 10", cleared.currentPage(t), cleared.ids)
	}

	// At the default page size the form submits size=25.
	if got := submitFilterBar(t, servePage(t, mux, listPath(f.name, nil)), nil); !strings.Contains(got, "size=25") ||
		!strings.Contains(got, "status=BACKLOG&status=SPRINT&status=DOING&status=TESTING") {
		t.Errorf("under the defaults the form requests %q, want size=25 and the four checked default statuses", got)
	}
}

// TestTaskList_ResetIsTheEmptyStatesAlone is the gate for the Reset halves of
// Acceptance Criteria 88, 241, and 249: the page's only Reset control is the
// no-match empty state's link, carrying btn; its href carries the four default
// status values and no other filter, no q, and no page, keeping size only when it
// is not 25; following it renders page 1 of the default list at that size.
func TestTaskList_ResetIsTheEmptyStatesAlone(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 30)
	mux := buildMux()
	defaults := "status=BACKLOG&status=SPRINT&status=DOING&status=TESTING"
	notCompleted := f.expect(func(x listTask) bool { return x.status != models.StatusCompleted })

	if s := fetchList(t, mux, listPath(f.name, url.Values{"status": {"DOING"}, "size": {"10"}})); s.resetHref != "" {
		t.Errorf("a list with rows carries a Reset link %q; the bar has none", s.resetHref)
	}
	noMatch := fetchList(t, mux, listPath(f.name, url.Values{
		"q": {"no task is titled like this"}, "sprint": {"none"}, "status": {"DOING"}, "type": {"BUG"}, "size": {"10"}, "page": {"2"},
	}))
	if noMatch.resetHref != "/roadmaps/"+f.name+"/tasks?"+defaults+"&size=10" {
		t.Errorf("the no-match Reset links to %q, want the defaults with size=10", noMatch.resetHref)
	}
	reset := fetchList(t, mux, noMatch.resetHref)
	if reset.currentPage(t) != 1 || !slices.Equal(reset.ids, notCompleted[:10]) || reset.total != len(notCompleted) ||
		!slices.Equal(reset.checked(t, "status"), []string{"BACKLOG", "SPRINT", "DOING", "TESTING"}) || reset.toggleText(t, "status") != "4 selected" {
		t.Errorf("following Reset renders page %d with %v (%d in all), want page 1 of the default list at size 10",
			reset.currentPage(t), reset.ids, reset.total)
	}
	if got := fetchList(t, mux, listPath(f.name, url.Values{"q": {"no task is titled like this"}})).resetHref; got != "/roadmaps/"+f.name+"/tasks?"+defaults {
		t.Errorf("at the default size Reset links to %q, want the defaults alone", got)
	}
}

// TestTaskList_PageAndSizeFallBack is the gate for Acceptance Criterion 240: an
// unacceptable page renders page 1, a page beyond the last renders the last — of
// the filtered list — and an unacceptable size renders 25 rows with 25 active,
// each answering 200 with the range text and the bar's active item agreeing.
func TestTaskList_PageAndSizeFallBack(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 60)
	mux := buildMux()
	want := f.expect(all)

	for _, value := range []string{"0", "-1", "abc", "1.5", "02", "%202", ""} {
		s := fetchList(t, mux, "/roadmaps/"+f.name+"/tasks?page="+value)
		if s.currentPage(t) != 1 || s.first != 1 || !slices.Equal(s.ids, want[:25]) {
			t.Errorf("page=%q renders page %d (first row %d), want page 1", value, s.currentPage(t), s.first)
		}
	}
	for _, value := range []string{"4", "999", "99999999999999999999999"} {
		s := fetchList(t, mux, "/roadmaps/"+f.name+"/tasks?page="+value)
		if s.currentPage(t) != 3 || s.first != 51 || s.last != 60 || !slices.Equal(s.ids, want[50:]) {
			t.Errorf("page=%q renders page %d (%d to %d), want the last page 3", value, s.currentPage(t), s.first, s.last)
		}
	}
	for _, value := range []string{"20", "0", "-10", "abc", "025", ""} {
		s := fetchList(t, mux, "/roadmaps/"+f.name+"/tasks?size="+value)
		if len(s.ids) != 25 {
			t.Errorf("size=%q renders %d rows, want 25", value, len(s.ids))
		}
		for _, link := range s.sizeLinks {
			if link.active != (link.text == "25") {
				t.Errorf("size=%q: the rows-per-page link %s is active=%v", value, link.text, link.active)
			}
		}
	}
	// Beyond the last page of a FILTERED list renders that list's last page.
	doing := f.expect(func(x listTask) bool { return x.status == models.StatusDoing })
	pages := (len(doing) + 9) / 10
	s := fetchList(t, mux, "/roadmaps/"+f.name+"/tasks?status=DOING&size=10&page=50")
	if s.currentPage(t) != pages || !slices.Equal(s.ids, doing[(pages-1)*10:]) {
		t.Errorf("page 50 of the DOING list renders page %d, want its last page %d", s.currentPage(t), pages)
	}
}

// TestTaskList_GeneratedLinksKeepTheFilters is the gate for Acceptance Criteria
// 103 and 241: every link the page generates to itself carries each filter of the
// active filter state — one status or type occurrence per active value, whether
// the URL or the defaults supplied it — and no ignored parameter and no parameter
// the page does not accept, priority and severity included; page only above 1,
// size only when not 25, q only while the term is not empty after the trim, and
// size=25 on a link that would otherwise carry nothing; following each link shows
// the same filters applied at the page and size the link named.
func TestTaskList_GeneratedLinksKeepTheFilters(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 180)
	mux := buildMux()

	accepted := url.Values{"q": {"the"}, "sprint": {"none"}}
	start := listPath(f.name, url.Values{"q": {"the"}, "sprint": {"none"}, "status": {"doing"}, "priority": {"0"}, "severity": {"1"},
		"type": {"bogus"}, "size": {"10"}, "page": {"2"}})
	s := fetchList(t, mux, start)
	if len(s.pageItems) < 3 {
		t.Fatalf("the filtered list has too few pages (%d items) to exercise the bar", len(s.pageItems))
	}
	want := f.expect(func(x listTask) bool {
		return strings.Contains(strings.ToLower(x.title), "the") && x.sprint == 0
	})

	for _, link := range generatedLinks(&s) {
		u, err := url.Parse(link)
		if err != nil {
			t.Fatalf("the generated link %q does not parse: %v", link, err)
		}
		q := u.Query()
		for key, value := range accepted {
			if q.Get(key) != value[0] {
				t.Errorf("the link %q carries %s=%q, want %q", link, key, q.Get(key), value[0])
			}
		}
		for _, ignored := range []string{"type", "status", "priority", "severity"} {
			if q.Has(ignored) {
				t.Errorf("the link %q carries %s, which the page ignores", link, ignored)
			}
		}
		if q.Get("page") == "1" || q.Get("size") == "25" {
			t.Errorf("the link %q carries a default page or size", link)
		}
		// Following it shows the filters applied at the named page and size.
		followed := fetchList(t, mux, link)
		size := 25
		if q.Has("size") {
			size = mustInt(t, q.Get("size"))
		}
		page := 1
		if q.Has("page") {
			page = mustInt(t, q.Get("page"))
		}
		if followed.currentPage(t) != page || !slices.Equal(followed.ids, want[(page-1)*size:min(page*size, len(want))]) {
			t.Errorf("following %q renders page %d with %v, want page %d at size %d of the filtered list",
				link, followed.currentPage(t), followed.ids, page, size)
		}
	}

	// A term that is empty after the trim travels in no generated link.
	blank := fetchList(t, mux, listPath(f.name, url.Values{"q": {"   "}, "size": {"10"}}))
	for _, link := range generatedLinks(&blank) {
		if u, _ := url.Parse(link); u.Query().Has("q") {
			t.Errorf("the link %q carries q although the term is empty after the trim", link)
		}
	}
	if blank.total != len(f.tasks) {
		t.Errorf("a whitespace-only term lists %d tasks, want every one of %d", blank.total, len(f.tasks))
	}

	// Every active value of a repeatable parameter travels in every link.
	multi := fetchList(t, mux, listPath(f.name, url.Values{"status": {"DOING", "TESTING"}, "size": {"10"}}))
	multiLinks := generatedLinks(&multi)
	if len(multiLinks) < 4 {
		t.Fatalf("the DOING or TESTING list generates %d links, too few to exercise", len(multiLinks))
	}
	for _, link := range multiLinks {
		u, _ := url.Parse(link)
		if got := u.Query()["status"]; !slices.Equal(got, []string{"DOING", "TESTING"}) {
			t.Errorf("the link %q carries status %v, want both DOING and TESTING", link, got)
		}
	}

	// A bare request answered from the defaults: every link carries the four
	// default status values, and so reproduces the default list explicitly.
	bare := fetchList(t, mux, listPath(f.name, nil))
	for _, link := range generatedLinks(&bare) {
		u, _ := url.Parse(link)
		if got := u.Query()["status"]; !slices.Equal(got, []string{"BACKLOG", "SPRINT", "DOING", "TESTING"}) {
			t.Errorf("under the defaults the link %q carries status %v, want the four default values", link, got)
		}
	}

	// A link that would carry no parameter at all carries size=25, so following it
	// is an explicit request for that list rather than the stored state.
	plain := fetchList(t, mux, listPath(f.name, url.Values{"size": {"10"}, "page": {"2"}}))
	for _, link := range append(generatedLinks(&plain), plain.prevHref) {
		u, _ := url.Parse(link)
		if len(u.Query()) == 0 {
			t.Errorf("the link %q carries no parameter; a generated link is always explicit", link)
		}
	}
	if want := "/roadmaps/" + f.name + "/tasks?size=25"; !slices.ContainsFunc(plain.sizeLinks, func(l servedSizeLink) bool { return l.href == want }) {
		t.Errorf("the rows-per-page link to 25 is not %s: %+v", want, plain.sizeLinks)
	}
}

// TestTaskList_PaginationBarAndSizeSelector is the gate for Acceptance Criterion
// 242: for every page count from 1 to 20 and every current page, the bar sits in
// <nav aria-label="Task list pages"> and follows the audit bar's sliding window,
// chevrons, and markup; the rows-per-page selector is the visible text followed by
// a labelled btn-group of four links, the active one alone carrying active and
// aria-current="true".
func TestTaskList_PaginationBarAndSizeSelector(t *testing.T) {
	for pages := 1; pages <= 20; pages++ {
		for current := 1; current <= pages; current++ {
			q := tasksQuery{Size: 10}
			data := tasksData{
				Name:      "payments-platform",
				Rows:      []db.TaskRow{{ID: 1, Title: "Reconcile the payout file", Type: models.TypeTask, Status: models.StatusDoing, CreatedAt: "2026-03-01T09:00:00.000Z"}},
				PageItems: taskPageLinks(&q, "payments-platform", current, pages),
				Total:     pages * 10, First: 1, Last: 1, Page: current, Pages: pages,
				Filters: taskFilterBar{Size: 10},
			}
			if current > 1 {
				data.PrevURL = q.link("payments-platform", current-1, 10)
			}
			if current < pages {
				data.NextURL = q.link("payments-platform", current+1, 10)
			}
			var buf strings.Builder
			if err := pageTemplates.ExecuteTemplate(&buf, "tasks.html", data); err != nil {
				t.Fatalf("rendering: %v", err)
			}
			s := parseList(t, buf.String())

			wantItems := paginationItems(current, pages)
			if len(s.pageItems) != len(wantItems) {
				t.Fatalf("%d pages, current %d: the bar holds %d items, want %d", pages, current, len(s.pageItems), len(wantItems))
			}
			for i, w := range wantItems {
				got := s.pageItems[i]
				switch {
				case w.IsEllipsis:
					if !got.ellipsis || got.href != "" {
						t.Errorf("%d/%d item %d: want a disabled ellipsis, got %+v", pages, current, i, got)
					}
				case w.IsCurrent:
					if !got.current || got.href != "" || got.text != itoa(w.Number) {
						t.Errorf("%d/%d item %d: want the active page %d, got %+v", pages, current, i, w.Number, got)
					}
				default:
					wantHref := "/roadmaps/payments-platform/tasks?size=10"
					if w.Number > 1 {
						wantHref = "/roadmaps/payments-platform/tasks?page=" + itoa(w.Number) + "&size=10"
					}
					if got.text != itoa(w.Number) || got.href != wantHref {
						t.Errorf("%d/%d item %d: want a link to page %d (%s), got %+v", pages, current, i, w.Number, wantHref, got)
					}
				}
			}
			if (s.prevHref == "") != (current == 1) || (s.nextHref == "") != (current == pages) {
				t.Errorf("%d/%d: the chevrons are prev %q next %q", pages, current, s.prevHref, s.nextHref)
			}
			if pages == 1 && (len(s.pageItems) != 1 || !s.pageItems[0].current) {
				t.Errorf("a list of one page does not show its single page as the active item")
			}
		}
	}

	// The served selector on a real page.
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 30)
	s := fetchList(t, buildMux(), listPath(f.name, url.Values{"size": {"50"}}))
	texts := make([]string, 0, len(s.sizeLinks))
	for _, link := range s.sizeLinks {
		texts = append(texts, link.text)
		if link.active != (link.text == "50") || link.current != link.active {
			t.Errorf("the rows-per-page link %s carries active=%v aria-current=%v", link.text, link.active, link.current)
		}
	}
	if !slices.Equal(texts, []string{"10", "25", "50", "100"}) {
		t.Errorf("the rows-per-page links read %v, want 10, 25, 50, 100", texts)
	}
	if !strings.Contains(s.body, `<span class="text-secondary me-2" id="task-list-size-label">Rows per page</span>`) ||
		!strings.Contains(s.body, `<div class="btn-group" role="group" aria-labelledby="task-list-size-label">`) {
		t.Errorf("the rows-per-page selector is not the visible text labelling a Tabler btn-group")
	}
	if !strings.Contains(s.body, `<nav aria-label="Task list pages">`) || !strings.Contains(s.body, `<ul class="pagination m-0">`) {
		t.Errorf("the pagination bar is not a Tabler pagination inside <nav aria-label=\"Task list pages\">")
	}
}

// TestTaskList_ListingReadsTitlesOnlyForATerm is the gate for the projection rule
// of SPEC/WEB.md § Roadmap Tasks Page, Read cost, and DATABASE.md § List All: the
// task listing is asked for the titles exactly when the request carries a search
// term — a q that is not empty after the trim — and for the ids alone otherwise,
// whether the state comes from the URL, from the cookie, or from the defaults. A
// whitespace-only q carries no term, so it reads the ids alone and renders the
// same list as no q at all; a term still finds its tasks by title and by
// reference.
func TestTaskList_ListingReadsTitlesOnlyForATerm(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 30)

	type outcome struct {
		ids   []int
		total int
	}
	read := func(label string, req *tasksRequest, wantTitle bool) outcome {
		t.Helper()
		src := openCounting(t, f.name)
		data, err := readTaskList(context.Background(), src, f.name, req)
		if err != nil {
			t.Fatalf("%s: readTaskList: %v", label, err)
		}
		if src.taskList != 1 || src.lastWithTitle != wantTitle {
			t.Errorf("%s: %d listings, titles requested = %v; want 1 listing with titles requested = %v",
				label, src.taskList, src.lastWithTitle, wantTitle)
		}
		ids := make([]int, len(data.Rows))
		for i := range data.Rows {
			ids[i] = data.Rows[i].ID
		}
		return outcome{ids: ids, total: data.Total}
	}

	all := url.Values{"size": {"100"}}
	withQ := func(q string) url.Values {
		v := url.Values{"size": {"100"}}
		v.Set("q", q)
		return v
	}

	none := read("no q", explicitTasks(all), false)
	if none.total != 30 {
		t.Fatalf("no q: %d tasks listed, want all 30", none.total)
	}
	// Each of these is empty after the trim of SPEC/WEB.md § Roadmap Tasks Page,
	// The trim rule, which the page applies through trimSearchTerm.
	for _, blank := range []string{"", " ", "\t \t", "\u00a0\u3000 "} {
		if trimSearchTerm(blank) != "" {
			t.Fatalf("q=%q is not empty after the trim; the case proves nothing", blank)
		}
		got := read(fmt.Sprintf("q=%q", blank), explicitTasks(withQ(blank)), false)
		if got.total != none.total || !slices.Equal(got.ids, none.ids) {
			t.Errorf("q=%q: listed %d tasks %v, want the list with no q, %d tasks %v", blank, got.total, got.ids, none.total, none.ids)
		}
	}
	read("defaults", bareTasks(nil), false)
	read("cookie without q", bareTasks(new("status=DOING")), false)
	read("cookie with a whitespace-only q", bareTasks(new("q=+++&status=DOING")), false)
	read("cookie with q", bareTasks(new("q=cache&status=DOING")), true)

	byTitle := read("q=cache", explicitTasks(withQ("  cache  ")), true)
	if byTitle.total == 0 || byTitle.total == none.total {
		t.Errorf("q=cache: %d tasks listed, want a proper subset of the %d", byTitle.total, none.total)
	}
	byRef := read("q=#7", explicitTasks(withQ("#7")), true)
	if len(byRef.ids) == 0 {
		t.Errorf("q=#7: no task found by its reference")
	}
}
