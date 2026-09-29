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
// 128, 129, and 232 to 242). The search text rules are gated in
// tasks_search_test.go.
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

// servedList is what a served tasks page states, read from its HTML alone.
type servedList struct {
	selects    map[string][]servedOption
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

	s := servedList{body: body, selects: map[string][]servedOption{}}
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

	body := servePage(t, mux, listPath(f.name, nil))
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
// the card no add-task button, the page no modal; the one form is a GET form; the
// page loads the admin shell's script alone, from /static/, and no inline script;
// its Content-Security-Policy is the fixed one; every class it emits and no
// style attribute; and the same URL requested twice serves the same list.
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
		`type="checkbox"`, "table-selectable", `class="modal`, `data-bs-toggle="modal"`, "New Task",
		"style=", "task-search.js", "sprint-board.js", "<button type=\"button\"",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the page carries %q", forbidden)
		}
	}
	if got := strings.Count(body, "<form"); got != 1 || !strings.Contains(body, `method="get"`) {
		t.Errorf("the page carries %d forms (method get present: %v), want exactly one GET form",
			got, strings.Contains(body, `method="get"`))
	}
	scripts := regexp.MustCompile(`<script\b[^>]*>`).FindAllString(body, -1)
	if len(scripts) != 1 || scripts[0] != `<script src="/static/vendor/tabler/tabler.min.js">` {
		t.Errorf("the page loads the scripts %v, want the admin shell's tabler.min.js alone", scripts)
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

	s := fetchList(t, mux, listPath(f.name, nil))
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
		{url.Values{}, 1, 25, 60},
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

// TestTaskList_EmptyStates is the gate for Acceptance Criteria 88 and 102: with no
// task to show, the card keeps its header and filter bar, renders no table and no
// footer, and shows Tabler's empty state in a card-body — "No tasks yet" for an
// empty roadmap with no criterion, "No task matches the filters" with the Reset
// link in its empty-action for a request whose criteria nothing satisfies,
// whether the criterion is a term, a filter, or both — each answering 200.
func TestTaskList_EmptyStates(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	if err := createEmptyRoadmap("clearing-house"); err != nil {
		t.Fatalf("creating the empty roadmap: %v", err)
	}
	f := seedListFixture(t, "payments-platform", 20)
	mux := buildMux()

	assertEmpty := func(path, wantTitle string) string {
		t.Helper()
		body := servePage(t, mux, path)
		main := boardRegion(t, body)
		for _, gone := range []string{"<table", `<div class="card-footer">`, "Task list pages", "Rows per page"} {
			if strings.Contains(main, gone) {
				t.Errorf("%s: the empty list still carries %q", path, gone)
			}
		}
		for _, kept := range []string{`<div class="card-header flex-wrap gap-2">`, "<form", `<div class="card-body">`, `<div class="empty">`} {
			if !strings.Contains(main, kept) {
				t.Errorf("%s: the empty list lost %q", path, kept)
			}
		}
		if got := parseList(t, body).emptyTitle; got != wantTitle {
			t.Errorf("%s: the empty state reads %q, want %q", path, got, wantTitle)
		}
		return main
	}

	noTasks := assertEmpty(listPath("clearing-house", nil), "No tasks yet")
	if !strings.Contains(noTasks, "rmp task create") || strings.Contains(noTasks, "empty-action") {
		t.Errorf("the no-task state does not name rmp task create, or carries a Reset action")
	}

	for _, params := range []url.Values{
		{"q": {"no task is titled like this"}},
		{"status": {"DOING"}, "type": {"CHORE"}, "sprint": {"none"}},
		{"q": {"cache"}, "sprint": {"none"}, "status": {"COMPLETED"}, "type": {"SPIKE"}},
	} {
		main := assertEmpty(listPath(f.name, params), "No task matches the filters")
		action := `<div class="empty-action">`
		at := strings.Index(main, action)
		if at < 0 {
			t.Errorf("%v: the no-match state carries no empty-action", params)
			continue
		}
		if !strings.Contains(main[at:], `<a class="btn" href="/roadmaps/`+f.name+`/tasks">Reset</a>`) {
			t.Errorf("%v: the empty-action does not hold the Reset link to the bare path", params)
		}
		// The empty state's link is the page's only Reset control.
		if got := strings.Count(main, ">Reset</a>"); got != 1 {
			t.Errorf("%v: the page carries %d Reset links, want the empty state's alone", params, got)
		}
	}
	// An accepted criterion on an empty roadmap is still a request no task
	// satisfies.
	assertEmpty(listPath("clearing-house", url.Values{"status": {"DOING"}}), "No task matches the filters")
}

// ==================== READ COST AND BOUND PARAMETERS ====================

// TestTaskList_ReadCost is the gate for Acceptance Criteria 89, 92, and 105 (its
// read half): exactly two reads whether or not the page renders a row — the
// sprint titles and the task listing — and no sprint-resolution query; the same
// count for 10 tasks and 300, for every page, page size and number of filters,
// with a term or without; no comment read.
func TestTaskList_ReadCost(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	small := seedListFixture(t, "payments-small", 10)
	large := seedListFixture(t, "payments-large", 300)

	for _, f := range []listFixture{small, large} {
		for _, params := range []url.Values{
			{},
			{"page": {"2"}, "size": {"10"}},
			{"size": {"100"}},
			{"status": {"DOING"}, "type": {"TASK"}, "sprint": {itoa(f.sprintA)}},
			{"priority": {"3"}, "severity": {"2"}},
			{"q": {"cache"}, "sprint": {"none"}},
			{"q": {"no task is titled like this"}},
		} {
			src := openCounting(t, f.name)
			data, err := readTaskList(context.Background(), src, f.name, params)
			if err != nil {
				t.Fatalf("%d tasks %v: readTaskList: %v", len(f.tasks), params, err)
			}
			if src.sprintTitles != 1 || src.taskList != 1 || src.groupedTaskSprints != 0 {
				t.Errorf("%d tasks %v (%d rows): reads = %d sprint titles, %d listings, %d sprint resolutions; want 1, 1, 0",
					len(f.tasks), params, len(data.Rows), src.sprintTitles, src.taskList, src.groupedTaskSprints)
			}
			if src.groupedCommentCounts+src.perTaskComments+src.sprintComments+src.boundedTaskList+src.sprintListings+src.sprintTasks != 0 {
				t.Errorf("%d tasks %v: the page issued a read beyond its two", len(f.tasks), params)
			}
		}
	}
}

// TestTaskList_FilterValuesAreBoundAndHostileValuesReachNothing is the gate for
// the web half of Acceptance Criteria 113 and 117: an accepted value reaches the task read
// as a field of the listing's filter — the listing binds each one as a parameter,
// which internal/db's TestListAllTasks_BindsEveryFilterValue pins on the SQL text —
// and a hostile value is ignored and reaches no statement at all; the roadmap's
// tasks are intact afterwards; q, page and size never reach the read, nor does any
// priority or severity parameter; and no filter value is echoed into the page as
// text.
func TestTaskList_FilterValuesAreBoundAndHostileValuesReachNothing(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 30)

	src := openCounting(t, f.name)
	if _, err := readTaskList(context.Background(), src, f.name, url.Values{
		"status": {"DOING"}, "type": {"BUG"}, "priority": {"4"}, "severity": {"0"},
		"sprint": {itoa(f.sprintB)}, "q": {"cache"}, "page": {"2"}, "size": {"10"},
	}); err != nil {
		t.Fatalf("readTaskList: %v", err)
	}
	got := src.lastTaskFilter
	if got == nil || got.Status == nil || *got.Status != models.StatusDoing || got.TaskType == nil ||
		*got.TaskType != models.TypeBug || got.SprintID == nil || *got.SprintID != f.sprintB || got.NoSprint {
		t.Errorf("the accepted values did not all reach the listing's filter: %+v", got)
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
	for _, hostile := range []url.Values{
		{"status": {"DOING' OR '1'='1"}},
		{"type": {"BUG;DROP TABLE tasks"}},
		{"sprint": {"1 OR 1=1"}},
		{"priority": {"1; DELETE FROM tasks"}},
		{"severity": {"0) OR (1=1"}},
	} {
		src := openCounting(t, f.name)
		if _, err := readTaskList(context.Background(), src, f.name, hostile); err != nil {
			t.Fatalf("%v: readTaskList: %v", hostile, err)
		}
		if g := src.lastTaskFilter; g == nil || g.Status != nil || g.TaskType != nil || g.MinPriority != nil ||
			g.MinSeverity != nil || g.SprintID != nil || g.NoSprint {
			t.Errorf("%v: the ignored value reached the listing's filter: %+v", hostile, g)
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
// 234, and the served-markup half of 243: the form's controls, in order, each the
// compact Tabler variant with exactly one programmatically associated label
// carrying visually-hidden, the search input's Search placeholder, the option sets
// of the three selects, the hidden size, and the Apply control — and nothing else:
// no priority or severity control and no Reset control — and on every response
// exactly one selected option per select; the sprint select carries the
// class the project stylesheet caps at 16rem, and the form is trailing-aligned;
// for a roadmap with no sprint the sprint select offers its two fixed options
// only, and two sprints of one title differ by their Sprint #<id> text.
func TestTaskList_FilterBarControls(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 10)
	mux := buildMux()
	body := servePage(t, mux, listPath(f.name, nil))
	form := body[mustIndex(t, body, "<form"):mustIndex(t, body, "</form>")]

	// The controls, in order, each labelled by a visually hidden label.
	order := []string{
		`<form class="row g-2 align-items-end justify-content-end" method="get" action="/roadmaps/` + f.name + `/tasks">`,
		`<label class="visually-hidden" for="task-filter-q">Search</label>`,
		`<input type="search" class="form-control form-control-sm" id="task-filter-q" name="q" placeholder="Search" value="">`,
		`<label class="visually-hidden" for="task-filter-sprint">Sprint</label>`,
		`<select class="form-select form-select-sm task-list__sprint-select" id="task-filter-sprint" name="sprint">`,
		`<label class="visually-hidden" for="task-filter-status">Status</label>`,
		`<select class="form-select form-select-sm" id="task-filter-status" name="status">`,
		`<label class="visually-hidden" for="task-filter-type">Type</label>`,
		`<select class="form-select form-select-sm" id="task-filter-type" name="type">`,
		`<button type="submit" class="btn btn-primary btn-sm">Apply</button>`,
	}
	// Exactly one label per control, each carrying visually-hidden, and no label
	// is displayed (Acceptance Criterion 243).
	reLabel := regexp.MustCompile(`<label([^>]*)>`)
	labels := reLabel.FindAllStringSubmatch(form, -1)
	if len(labels) != 4 {
		t.Errorf("the filter bar carries %d labels, want exactly one for each of its four filter controls", len(labels))
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
		if got := strings.Count(form, `id="task-filter-`+id+`"`); got != 1 {
			t.Errorf("the id task-filter-%s is carried %d times, want exactly 1", id, got)
		}
	}
	for _, l := range labels {
		if !strings.Contains(l[1], `class="visually-hidden"`) {
			t.Errorf("the label %q does not carry visually-hidden", l[0])
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
	optionValues := func(name string) (values, labels []string) {
		for _, o := range s.selects[name] {
			values = append(values, o.value)
			labels = append(labels, o.label)
		}
		return values, labels
	}
	sprintValues, sprintLabels := optionValues("sprint")
	if want := []string{"", "none", itoa(f.sprintA), itoa(f.sprintB)}; !slices.Equal(sprintValues, want) {
		t.Errorf("the sprint select offers %v, want %v", sprintValues, want)
	}
	if want := []string{"Any sprint", "No sprint", "Sprint #" + itoa(f.sprintA) + " Checkout hardening",
		"Sprint #" + itoa(f.sprintB) + " Settlement reconciliation"}; !slices.Equal(sprintLabels, want) {
		t.Errorf("the sprint select reads %v, want %v", sprintLabels, want)
	}
	statusValues, statusLabels := optionValues("status")
	if want := []string{"", "BACKLOG", "SPRINT", "DOING", "TESTING", "COMPLETED"}; !slices.Equal(statusValues, want) ||
		statusLabels[0] != "Any status" {
		t.Errorf("the status select offers %v / %v", statusValues, statusLabels)
	}
	typeValues, typeLabels := optionValues("type")
	wantTypes := make([]string, 0, len(models.ValidTaskTypes)+1)
	wantTypes = append(wantTypes, "")
	for _, tt := range models.ValidTaskTypes {
		wantTypes = append(wantTypes, string(tt))
	}
	if !slices.Equal(typeValues, wantTypes) || typeLabels[0] != "Any type" {
		t.Errorf("the type select offers %v / %v", typeValues, typeLabels)
	}
	if len(s.selects) != 3 {
		t.Errorf("the filter bar carries %d selects, want sprint, status, and type alone", len(s.selects))
	}
	for _, name := range []string{"sprint", "status", "type"} {
		if got := s.selected(t, name); got != "" {
			t.Errorf("with no parameter the %s select selects %q, want its any option", name, got)
		}
	}

	// Each select shows the accepted value that produced the list, and the search
	// input shows the term.
	chosen := parseList(t, servePage(t, mux, listPath(f.name, url.Values{
		"q": {"Refund cache"}, "sprint": {"none"}, "status": {"TESTING"}, "type": {"EPIC"},
	})))
	for name, want := range map[string]string{"sprint": "none", "status": "TESTING", "type": "EPIC"} {
		if got := chosen.selected(t, name); got != want {
			t.Errorf("the %s select selects %q, want %q", name, got, want)
		}
	}
	if chosen.search != "Refund cache" {
		t.Errorf("the search input shows %q, want the q the request carried", chosen.search)
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
			if s := fetchList(t, mux, listPath(f.name, params)); s.total != len(want) {
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
// lists without that parameter, selects the any option, and carries the value in
// no generated link; the other parameters stay applied; a repeated parameter is
// read as its first occurrence; unknown parameters are ignored.
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
			if got := s.selected(t, param); got != "" {
				t.Errorf("%s=%q: the select selects %q, want its any option", param, value, got)
			}
			if got := s.selected(t, keptParam); got != keptValue {
				t.Errorf("%s=%q: the accepted %s is no longer selected (%q)", param, value, keptParam, got)
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

	// A repeated parameter is read as its first occurrence.
	repeated, _ := serve("type=BUG&type=EPIC&size=100")
	if want := f.expect(func(x listTask) bool { return x.taskType == models.TypeBug }); !slices.Equal(repeated.ids, want) {
		t.Errorf("type=BUG&type=EPIC lists %v, want the BUG tasks %v", repeated.ids, want)
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
// field as the served HTML defines it, with the given fields changed, returning
// the URL the submission requests.
func submitFilterBar(t *testing.T, body string, changes map[string]string) string {
	t.Helper()
	form := body[mustIndex(t, body, "<form"):mustIndex(t, body, "</form>")]
	action := regexp.MustCompile(`action="([^"]*)"`).FindStringSubmatch(form)[1]
	fields := url.Values{}
	for _, m := range regexp.MustCompile(`<input type="(?:hidden|search)"[^>]* name="([a-z]+)"(?: placeholder="[^"]*")? value="([^"]*)">`).FindAllStringSubmatch(form, -1) {
		fields.Add(m[1], html.UnescapeString(m[2]))
	}
	s := parseList(t, body)
	for name := range s.selects {
		fields.Add(name, s.selected(t, name))
	}
	if !fields.Has("q") || !fields.Has("size") || len(fields) != 5 {
		t.Fatalf("the served form submits %v; want q, sprint, status, type, and size", fields)
	}
	for name, value := range changes {
		fields.Set(name, value)
	}
	return html.UnescapeString(action) + "?" + fields.Encode()
}

// TestTaskList_ApplyingTheFormReturnsToPageOneAndKeepsTheSize is the gate for
// Acceptance Criteria 116 and 237: on page 3 at size 10, submitting the real form
// with a new status requests a URL carrying size=10 and no page and renders page 1
// of the new list at size 10; reloading it renders the same list; submitting the
// form again with every select on its any option and an empty search input renders
// page 1 of the unfiltered list at size 10. At the default size the form still
// submits size=25.
func TestTaskList_ApplyingTheFormReturnsToPageOneAndKeepsTheSize(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 60)
	mux := buildMux()

	start := servePage(t, mux, listPath(f.name, url.Values{"page": {"3"}, "size": {"10"}, "q": {"the"}}))
	if startList := parseList(t, start); startList.currentPage(t) != 3 {
		t.Fatalf("the starting page is not page 3")
	}
	submitted := submitFilterBar(t, start, map[string]string{"status": "DOING"})
	u, _ := url.Parse(submitted)
	if u.Query().Has("page") || u.Query().Get("size") != "10" {
		t.Fatalf("the submitted form requests %q; want no page and size 10", submitted)
	}

	want := f.expect(func(x listTask) bool {
		return x.status == models.StatusDoing && strings.Contains(strings.ToLower(x.title), "the")
	})
	filteredBody := servePage(t, mux, submitted)
	filtered := parseList(t, filteredBody)
	if filtered.currentPage(t) != 1 || !slices.Equal(filtered.ids, want[:min(10, len(want))]) || filtered.selected(t, "status") != "DOING" {
		t.Errorf("the submitted form renders page %d with %v, want page 1 of %v at size 10", filtered.currentPage(t), filtered.ids, want)
	}
	if reload := fetchList(t, mux, submitted); !slices.Equal(reload.ids, filtered.ids) {
		t.Errorf("reloading the submitted URL renders a different list")
	}

	// Clearing: every select on its any option and an empty search input.
	cleared := fetchList(t, mux, submitFilterBar(t, filteredBody, map[string]string{"q": "", "sprint": "", "status": "", "type": ""}))
	if cleared.currentPage(t) != 1 || !slices.Equal(cleared.ids, f.expect(all)[:10]) || cleared.selected(t, "status") != "" {
		t.Errorf("clearing the form renders page %d with %v, want page 1 of the unfiltered list at size 10", cleared.currentPage(t), cleared.ids)
	}

	// At the default page size the form submits size=25.
	if got := submitFilterBar(t, servePage(t, mux, listPath(f.name, nil)), nil); !strings.Contains(got, "size=25") {
		t.Errorf("at the default size the form requests %q, want size=25", got)
	}
}

// TestTaskList_ResetIsTheEmptyStatesAlone is the gate for the Reset halves of
// Acceptance Criteria 88 and 241: the page's only Reset control is the no-match
// empty state's link, carrying btn; its href is the bare path, keeping size only
// when it is not 25 and carrying no filter, no q, and no page; following it
// renders page 1 of the unfiltered list at that size.
func TestTaskList_ResetIsTheEmptyStatesAlone(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 30)
	mux := buildMux()

	if s := fetchList(t, mux, listPath(f.name, url.Values{"status": {"DOING"}, "size": {"10"}})); s.resetHref != "" {
		t.Errorf("a list with rows carries a Reset link %q; the bar has none", s.resetHref)
	}
	noMatch := fetchList(t, mux, listPath(f.name, url.Values{
		"q": {"no task is titled like this"}, "sprint": {"none"}, "status": {"DOING"}, "type": {"BUG"}, "size": {"10"}, "page": {"2"},
	}))
	if noMatch.resetHref != "/roadmaps/"+f.name+"/tasks?size=10" {
		t.Errorf("the no-match Reset links to %q, want the path with size=10 alone", noMatch.resetHref)
	}
	reset := fetchList(t, mux, noMatch.resetHref)
	if reset.currentPage(t) != 1 || !slices.Equal(reset.ids, f.expect(all)[:10]) {
		t.Errorf("following Reset renders page %d with %v, want page 1 of the unfiltered list at size 10", reset.currentPage(t), reset.ids)
	}
	if got := fetchList(t, mux, listPath(f.name, url.Values{"q": {"no task is titled like this"}})).resetHref; got != "/roadmaps/"+f.name+"/tasks" {
		t.Errorf("at the default size Reset links to %q, want the bare path", got)
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
// 103 and 241: every link the page generates to itself carries each accepted
// filter with its accepted value, and no ignored parameter and no parameter the page
// does not accept, priority and severity included; page only
// above 1, size only when not 25, q only while the term is not empty after the
// trim; following each link shows the same filters applied at the page and size
// the link named.
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
				Rows:      []models.Task{{ID: 1, Title: "Reconcile the payout file", Type: models.TypeTask, Status: models.StatusDoing, CreatedAt: "2026-03-01T09:00:00.000Z"}},
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
