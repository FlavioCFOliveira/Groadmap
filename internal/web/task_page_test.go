package web

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file is the gate for the Roadmap Task Page, GET /roadmaps/{name}/tasks/{id}:
// every field of one task, its comments, and the context of its sprint, rendered
// on the server (SPEC/WEB.md § Roadmap Task Page; Acceptance Criteria 15, 94 to
// 99, 188, 206, and 221 to 227). The comment log itself is gated in
// comments_test.go (Acceptance Criteria 64 to 67).

// ==================== FIXTURE ====================

// The task the acceptance criteria describe: task 42 of the roadmap `payments`,
// titled `Rotate the signing keys`, in status DOING, the member at position 2 of
// sprint 7 `Payments hardening` (OPEN, eleven member tasks, four COMPLETED); its
// parent is task 40, it depends on tasks 12 and 17, and task 51 depends on it.
const (
	pageRoadmap      = "payments"
	pageTaskID       = 42
	pageTaskTitle    = "Rotate the signing keys"
	pageSprintID     = 7
	pageSprintTitle  = "Payments hardening"
	pageParentID     = 40
	pageBlockedID    = 51
	pageCommitOpen   = "5f93b518375f7f65df4f275f1ee9b2b2e2fd17f0"
	pageEmptySprint  = 6 // PENDING, two member tasks, none COMPLETED
	pageLooseTaskID  = 55
	pageClosedTaskID = 20 // the first member of sprint 7, COMPLETED, every timestamp set
	pageBacklogID    = 29 // the last member of sprint 7, back in BACKLOG
)

// pageMembers are sprint 7's member tasks in sprint_tasks position order: task 42
// is at stored position 2, the first four are COMPLETED, and the last is BACKLOG.
var pageMembers = []int{20, 21, pageTaskID, 22, 23, 24, 25, 26, 27, 28, pageBacklogID}

// pageTimestamps are the fixed lifecycle timestamps of the COMPLETED task 20.
var pageTimestamps = [4]string{
	"2026-09-01T08:15:30.125Z", "2026-09-02T09:20:31.250Z",
	"2026-09-03T10:25:32.375Z", "2026-09-04T11:30:33.500Z",
}

// seedTaskPageFixture creates the roadmap `payments` under the test's HOME.
func seedTaskPageFixture(t *testing.T) {
	t.Helper()

	database, err := db.Open(pageRoadmap)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", pageRoadmap, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	const now = "2026-08-30T08:00:00.000Z"

	areas := []string{"card tokenisation", "refund ledger", "chargeback intake", "payout batching",
		"fraud scoring", "webhook delivery", "currency conversion", "settlement export"}
	for i := 1; i <= 60; i++ {
		task := seededTask(now, fmt.Sprintf("Harden the %s path, step %d", areas[i%len(areas)], i))
		switch i {
		case pageTaskID:
			task.Title = pageTaskTitle
			task.Priority = 7
			task.Severity = 2
			task.Type = models.TypeTask
			parent := pageParentID
			task.ParentTaskID = &parent
			task.FunctionalRequirements = "Rotate the **signing keys** of the payment gateway every 90 days."
			task.TechnicalRequirements = "Use the `kms` rotation API.\n\n| step | owner |\n|---|---|\n| rotate | ops |"
			task.AcceptanceCriteria = "- [x] new key active\n- [ ] old key revoked"
		case pageParentID:
			task.Title = "Harden the payment gateway key management"
		}
		id, terr := seedTask(database, task)
		if terr != nil {
			t.Fatalf("creating task %d: %v", i, terr)
		}
		if id != i {
			t.Fatalf("task %d was created with id %d; the fixture relies on the sequence", i, id)
		}
	}

	for i := 1; i <= pageSprintID; i++ {
		title := fmt.Sprintf("Payments maintenance window %d", i)
		switch i {
		case pageSprintID:
			title = pageSprintTitle
		case pageEmptySprint:
			title = "Refund ledger cleanup"
		}
		id, serr := seedSprint(database, &models.Sprint{
			Status: models.SprintPending, Title: title, Description: title, CreatedAt: now,
		})
		if serr != nil || id != i {
			t.Fatalf("creating sprint %d: id %d, %v", i, id, serr)
		}
	}
	if err := database.AddTasksToSprint(ctx, pageSprintID, pageMembers); err != nil {
		t.Fatalf("adding members to sprint %d: %v", pageSprintID, err)
	}
	if err := database.AddTasksToSprint(ctx, pageEmptySprint, []int{30, 31}); err != nil {
		t.Fatalf("adding members to sprint %d: %v", pageEmptySprint, err)
	}
	forceSprintOpen(t, database, pageSprintID)
	forceTaskLifecycle(t, database, pageMembers[:2], models.StatusCompleted)
	forceTaskLifecycle(t, database, []int{22, 23}, models.StatusCompleted)
	forceTaskLifecycle(t, database, []int{pageTaskID}, models.StatusDoing)
	forceTaskLifecycle(t, database, []int{24}, models.StatusTesting)
	forceTaskLifecycle(t, database, []int{pageBacklogID}, models.StatusBacklog)

	for _, dep := range [][2]int{{pageTaskID, 12}, {pageTaskID, 17}, {pageBlockedID, pageTaskID}} {
		if err := database.AddTaskDependencyWithAudit(ctx, dep[0], dep[1]); err != nil {
			t.Fatalf("adding dependency %v: %v", dep, err)
		}
	}
	mustExec(t, database, `UPDATE tasks SET commit_open = ? WHERE id = ?`, pageCommitOpen, pageTaskID)
	mustExec(t, database,
		`UPDATE tasks SET status = ?, started_at = ?, tested_at = ?, closed_at = ?, completion_summary = ?, commit_open = ?, commit_close = ? WHERE id = ?`,
		models.StatusCompleted, pageTimestamps[1], pageTimestamps[2], pageTimestamps[3],
		"Tokens now expire after **15 minutes**.", pageCommitOpen, "2578d18abc1234567890abcdef1234567890abcd",
		pageClosedTaskID)
	mustExec(t, database, `UPDATE tasks SET created_at = ? WHERE id = ?`, pageTimestamps[0], pageClosedTaskID)
}

// mustExec runs one fixture statement, failing the test on error.
func mustExec(t *testing.T, database *db.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.Exec(query, args...); err != nil {
		t.Fatalf("fixture statement %q: %v", query, err)
	}
}

// taskPagePath is the path of a task's page in the fixture roadmap.
func taskPagePath(id int) string { return "/roadmaps/" + pageRoadmap + "/tasks/" + itoa(id) }

// ==================== MARKUP HELPERS ====================

// regionBetween returns the markup from the first occurrence of start up to the
// first occurrence of end after it, failing the test when start is absent.
func regionBetween(t *testing.T, body, start, end string) string {
	t.Helper()

	at := strings.Index(body, start)
	if at < 0 {
		t.Fatalf("the page carries no %q", start)
	}
	rest := body[at:]
	if stop := strings.Index(rest, end); stop >= 0 {
		return rest[:stop]
	}
	return rest
}

// taskSprintCardSlice returns the task page's Sprint card.
func taskSprintCardSlice(t *testing.T, body string) string {
	t.Helper()
	return regionBetween(t, body, `data-role="task-sprint-card"`, `data-role="task-details-card"`)
}

// detailsCardSlice returns the task page's Details card.
func detailsCardSlice(t *testing.T, body string) string {
	t.Helper()
	return regionBetween(t, body, `data-role="task-details-card"`, `<div class="col-12 col-lg-8">`)
}

// cardEnd returns the length of the Markdown field card titled title, measured
// from its card title to the next card title of the page.
func cardEnd(t *testing.T, body, title string) int {
	t.Helper()
	rest := regionBetween(t, body, `<h3 class="card-title">`+title+`</h3>`, `<h3 class="card-title">Comments`)
	if next := strings.Index(rest[1:], `<h3 class="card-title">`); next >= 0 {
		return next + 1
	}
	return len(rest)
}

// fieldCardSlice returns the Markdown field card titled title.
func fieldCardSlice(t *testing.T, body, title string) string {
	t.Helper()
	return regionBetween(t, body, `<h3 class="card-title">`+title+`</h3>`, `<h3 class="card-title">Comments`)[:cardEnd(t, body, title)]
}

// reDatagridItem captures one datagrid item's title and its content element.
var reDatagridItem = regexp.MustCompile(`(?s)<div class="datagrid-title">([^<]*)</div>\s*(<div class="datagrid-content[^"]*">.*?</div>)\s*</div>`)

// datagridItems returns the titles and the content elements of a datagrid, in
// document order.
func datagridItems(t *testing.T, region string) (titles, contents []string) {
	t.Helper()
	for _, m := range reDatagridItem.FindAllStringSubmatch(region, -1) {
		titles = append(titles, m[1])
		contents = append(contents, m[2])
	}
	return titles, contents
}

// datagridContent returns the content element of the datagrid item labelled label.
func datagridContent(t *testing.T, region, label string) string {
	t.Helper()
	titles, contents := datagridItems(t, region)
	for i := range titles {
		if titles[i] == label {
			return contents[i]
		}
	}
	t.Fatalf("the datagrid has no item labelled %q (items: %v)", label, titles)
	return ""
}

// absentContent is the content element the Details card renders for an absent
// value: the em dash, muted, through the audit log's own helper.
const absentContent = `<div class="datagrid-content text-secondary">—</div>`

// ==================== AC 15 AND 94: EVERY FIELD, SERVER-RENDERED ====================

// TestTaskPage_ServesEveryFieldInTheHTML is the gate for Acceptance Criteria 15
// and 94: for a task of each of the five statuses the page answers 200 with HTML
// that already carries every field of the task, in the document the server sends;
// HEAD answers 200 with the GET's headers and no body.
func TestTaskPage_ServesEveryFieldInTheHTML(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)
	srv := handler()
	live := httptest.NewServer(srv)
	defer live.Close()

	for _, c := range []struct {
		status models.TaskStatus
		id     int
	}{
		{models.StatusBacklog, pageBacklogID},
		{models.StatusSprint, 25},
		{models.StatusDoing, pageTaskID},
		{models.StatusTesting, 24},
		{models.StatusCompleted, pageClosedTaskID},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, taskPagePath(c.id), nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET task #%d: status = %d, want 200", c.id, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != contentTypeHTML {
			t.Errorf("task #%d: content-type = %q, want %q", c.id, ct, contentTypeHTML)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `<div class="page-pretitle">Task #`+itoa(c.id)+` <span class="badge `+
			taskStatusBadge(c.status)+`">`+string(c.status)+`</span></div>`) {
			t.Errorf("task #%d: the header does not state its id and its %s status", c.id, c.status)
		}

		task := readFixtureTask(t, c.id)
		details := detailsCardSlice(t, body)
		wantLabels := []string{"Type", "Severity", "Priority", "Parent task", "Subtasks", "Depends on",
			"Blocks", "Created", "Started", "Tested", "Closed", "Commit open", "Commit close"}
		titles, _ := datagridItems(t, details)
		if strings.Join(titles, "|") != strings.Join(wantLabels, "|") {
			t.Errorf("task #%d: the Details card holds %v, want %v", c.id, titles, wantLabels)
		}
		for _, want := range []string{
			`<h2 class="page-title">` + rendered(task.Title) + `</h2>`,
			`<div class="datagrid-content">` + string(task.Type) + `</div>`,
			`">` + itoa(task.Priority) + `</span>`,
			`<div class="datagrid-content">` + itoa(task.SubtaskCount) + `</div>`,
			`<time datetime="` + task.CreatedAt + `">`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("task #%d: the page does not carry %s", c.id, want)
			}
		}
		for _, title := range []string{"Functional requirements", "Technical requirements",
			"Acceptance criteria", "Completion summary"} {
			fieldCardSlice(t, body, title)
		}
		taskCommentsCardSlice(t, body)

		// HEAD is measured through a real server, which is what discards the body.
		head, err := http.Head(live.URL + taskPagePath(c.id))
		if err != nil {
			t.Fatalf("HEAD task #%d: %v", c.id, err)
		}
		n, _ := io.Copy(io.Discard, head.Body) //nolint:errcheck // the byte count is the assertion
		_ = head.Body.Close()                  // response body of a completed request
		if head.StatusCode != http.StatusOK || n != 0 {
			t.Errorf("HEAD task #%d: status %d with %d body bytes, want 200 and none", c.id, head.StatusCode, n)
		}
		for _, h := range []string{"Content-Type", "Cache-Control", "Content-Security-Policy"} {
			if head.Header.Get(h) != rec.Header().Get(h) {
				t.Errorf("HEAD task #%d: %s = %q, the GET carries %q", c.id, h, head.Header.Get(h), rec.Header().Get(h))
			}
		}
	}
}

// readFixtureTask reads one task back from the fixture roadmap, so expectations
// are composed from the stored row rather than restated.
func readFixtureTask(t *testing.T, id int) *models.Task {
	t.Helper()
	return storedTask(t, pageRoadmap, id)
}

// storedTask reads one task of a roadmap back from its database, read-only.
func storedTask(t *testing.T, roadmap string, id int) *models.Task {
	t.Helper()
	database, err := db.OpenReadOnly(roadmap)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup
	task, err := database.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("reading task %d: %v", id, err)
	}
	return task
}

// ==================== AC 95: PATH, METHODS, CACHE ====================

// TestTaskPage_PathDiscipline is the gate for Acceptance Criterion 95: an invalid
// or nonexistent roadmap, a non-integer id, and an id that is not a task of the
// named roadmap each answer 404; any method but GET and HEAD answers 405; every
// response, the 404 included, carries Cache-Control: no-store and the security
// headers.
func TestTaskPage_PathDiscipline(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)
	// A second roadmap holding task 60 of its own does not exist: platform-core has
	// one task, so id 42 is a task of `payments` only.
	other := seedRoadmap(t, "platform-core")
	srv := handler()

	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, taskPagePath(pageTaskID), http.StatusOK},
		{http.MethodGet, "/roadmaps/INVALID/tasks/1", http.StatusNotFound},
		{http.MethodGet, "/roadmaps/..%2fetc/tasks/1", http.StatusNotFound},
		{http.MethodGet, "/roadmaps/no_such_roadmap/tasks/1", http.StatusNotFound},
		{http.MethodGet, "/roadmaps/" + pageRoadmap + "/tasks/not-a-number", http.StatusNotFound},
		{http.MethodGet, "/roadmaps/" + pageRoadmap + "/tasks/-1", http.StatusNotFound},
		{http.MethodGet, "/roadmaps/" + pageRoadmap + "/tasks/999999", http.StatusNotFound},
		{http.MethodGet, "/roadmaps/" + other + "/tasks/" + itoa(pageTaskID), http.StatusNotFound},
		{http.MethodHead, "/roadmaps/" + other + "/tasks/" + itoa(pageTaskID), http.StatusNotFound},
		{http.MethodPost, taskPagePath(pageTaskID), http.StatusMethodNotAllowed},
		{http.MethodPut, taskPagePath(pageTaskID), http.StatusMethodNotAllowed},
		{http.MethodPatch, taskPagePath(pageTaskID), http.StatusMethodNotAllowed},
		{http.MethodDelete, taskPagePath(pageTaskID), http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("%s %s: status = %d, want %d", c.method, c.path, rec.Code, c.want)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s %s: Cache-Control = %q, want no-store", c.method, c.path, got)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
			t.Errorf("%s %s: Content-Security-Policy = %q", c.method, c.path, got)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Errorf("%s %s: the security headers are missing", c.method, c.path)
		}
	}
	// The other roadmap's own task 1 is served under its own path only.
	if rec := httptest.NewRecorder(); true {
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmaps/"+other+"/tasks/1", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET the other roadmap's own task: status = %d, want 200", rec.Code)
		}
	}
}

// ==================== AC 97 AND 98: ESCAPING AND NO SCRIPT ====================

// TestTaskPage_HostileValuesNeverReachThePageAsMarkup is the gate for Acceptance
// Criterion 97: a hostile task title renders as visible characters in the page
// header and in the document title, and a requirement field and a comment body
// holding raw HTML render through the Markdown renderer, so no element, attribute,
// or script of the author's reaches the page.
func TestTaskPage_HostileValuesNeverReachThePageAsMarkup(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedCommentFixture(t, "settlement-reconciliation")
	renameTask(t, f.name, f.markupTaskID, hostileTitle)

	database, err := db.Open(f.name)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	mustExec(t, database, `UPDATE tasks SET functional_requirements = ?, completion_summary = ? WHERE id = ?`,
		"Block <script>alert('fr')</script> and <img src=x onerror=alert(1)> inputs.",
		`<iframe src="https://example.com"></iframe> closed <b onclick="alert(2)">here</b>`, f.markupTaskID)
	_ = database.Close() // test cleanup; a close error on a fixture handle is not under test

	body := servePage(t, buildMux(), "/roadmaps/"+f.name+"/tasks/"+itoa(f.markupTaskID))
	main := regionBetween(t, body, `<main class="page-body">`, "</main>")

	if got := strings.Count(body, "<script"); got != 1 {
		t.Errorf("the page carries %d <script elements, want only the vendored bundle", got)
	}
	if strings.Contains(body, hostileTitle) || strings.Contains(body, `"quoted"`) {
		t.Errorf("the hostile title reached the page unescaped")
	}
	for _, raw := range []string{"<b>bold</b>", "<script", "<img", "onerror", "<iframe", "onclick", "<b onclick"} {
		if strings.Contains(main, raw) {
			t.Errorf("%q reached the page body as markup", raw)
		}
	}
	escaped := rendered(hostileTitle)
	if !strings.Contains(body, `<h2 class="page-title">`+escaped+`</h2>`) {
		t.Errorf("the page header does not show the hostile title as visible characters")
	}
	if !strings.Contains(body, "<title>#"+itoa(f.markupTaskID)+" "+escaped+" - settlement-reconciliation") {
		t.Errorf("the document title does not lead with the task reference and the escaped title")
	}
	if strings.Contains(regionBetween(t, body, "<title>", "</title>"), "<b>") {
		t.Errorf("the document title carries markup")
	}
	if !strings.Contains(fieldCardSlice(t, body, "Functional requirements"), "inputs.") {
		t.Errorf("the hostile requirement field lost its text")
	}
}

// TestTaskPage_LoadsNoScriptOfItsOwn is the gate for Acceptance Criterion 98: the
// page loads only the vendored framework from /static/, carries no inline script
// and no inline event-handler attribute, its Content-Security-Policy is exactly
// the fixed value, and it references no origin but its own.
func TestTaskPage_LoadsNoScriptOfItsOwn(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)

	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, taskPagePath(pageTaskID), nil))
	body := rec.Body.String()

	if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Errorf("Content-Security-Policy = %q, want %q", got, contentSecurityPolicy)
	}
	scripts := regexp.MustCompile(`<script\b[^>]*>`).FindAllString(body, -1)
	if len(scripts) != 1 || scripts[0] != `<script src="/static/vendor/tabler/tabler.min.js">` {
		t.Errorf("the page loads %v, want only the vendored framework", scripts)
	}
	if regexp.MustCompile(`(?i)\son[a-z]+=`).MatchString(body) {
		t.Errorf("the page carries an inline event-handler attribute")
	}
	for _, m := range regexp.MustCompile(`(?:src|href)="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		if strings.HasPrefix(m[1], "http:") || strings.HasPrefix(m[1], "https:") || strings.HasPrefix(m[1], "//") {
			t.Errorf("the page references the remote origin %q", m[1])
		}
	}
	if strings.Contains(body, "fetch(") || strings.Contains(body, "/data") {
		t.Errorf("the page carries a data fetch")
	}
}

// ==================== AC 99: READ FAILURE ====================

// TestTaskPage_ReadFailureIs500AndLoggedOnce is the gate for Acceptance Criterion
// 99: a read that fails for a reason other than not-found answers 500 with no
// detail and one ERROR record naming the error; a task that does not exist writes
// no record.
func TestTaskPage_ReadFailureIs500AndLoggedOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	const name = "corrupt-roadmap"
	seedRoadmap(t, name)

	buf := captureLog(t)
	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/tasks/9999", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown task: status = %d, want 404", rec.Code)
	}
	if buf.Len() != 0 {
		t.Errorf("a request for a task that does not exist wrote a record: %q", buf.String())
	}

	dbPath := filepath.Join(home, ".roadmaps", name, utils.DBFileName)
	if err := os.WriteFile(dbPath, []byte("this is not a SQLite database at all"), 0o600); err != nil {
		t.Fatalf("corrupting database: %v", err)
	}
	rec = httptest.NewRecorder()
	handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/tasks/1", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	mustContainAll(t, oneRecord(t, buf),
		"level=ERROR",
		`msg="task page load failed"`,
		"method=GET",
		"roadmap="+name,
		"task=1",
		"status=500",
		"err=",
	)
	if body := rec.Body.String(); strings.TrimSpace(body) != "internal server error" {
		t.Errorf("response body = %q, want the opaque internal server error text", body)
	}
}

// ==================== AC 221: HEADER, ACTIVE VIEW, WAY BACK ====================

// TestTaskPage_HeaderActiveViewAndWayBack is the gate for Acceptance Criterion
// 221.
func TestTaskPage_HeaderActiveViewAndWayBack(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)
	mux := buildMux()

	body := servePage(t, mux, taskPagePath(pageTaskID))
	if !strings.Contains(body, `<div class="page-pretitle">Task #42 <span class="badge bg-blue-lt">DOING</span></div>`) {
		t.Errorf("the pretitle does not read Task #42 followed by the bg-blue-lt DOING badge")
	}
	if !strings.Contains(body, `<h2 class="page-title">Rotate the signing keys</h2>`) {
		t.Errorf("the title does not read the task's title alone")
	}

	// The sidebar's Tasks entry is the active one, and no other entry is.
	if !strings.Contains(body, `<li class="nav-item active">
            <a class="nav-link" href="/roadmaps/payments/tasks" aria-current="page">`) {
		t.Errorf("the sidebar's Tasks entry is not the active one")
	}
	if got := strings.Count(body, " active\""); got != 1 {
		t.Errorf("%d sidebar entries are active, want exactly the Tasks entry", got)
	}
	if got := strings.Count(body, `aria-current="page"`); got != 1 {
		t.Errorf("%d elements carry aria-current, want 1", got)
	}

	// The actions column holds exactly one link, Back to tasks, with no query.
	header := regionBetween(t, body, `<div class="page-header d-print-none">`, `<main class="page-body">`)
	actions := regionBetween(t, header, `<div class="col-12 col-sm-auto ms-auto d-print-none">`, "</div>")
	if got := strings.Count(actions, "<a "); got != 1 {
		t.Errorf("the actions column holds %d links, want exactly 1", got)
	}
	if !strings.Contains(actions, `href="/roadmaps/payments/tasks">`) || !strings.Contains(actions, "Back to tasks</a>") {
		t.Errorf("the actions column is not the Back to tasks link to /roadmaps/payments/tasks: %s", actions)
	}
	for _, gone := range []string{"/sprints/", "/graph", "?"} {
		if strings.Contains(header, gone) {
			t.Errorf("the page header carries %q", gone)
		}
	}

	// Each of the five statuses carries its own badge variant.
	for id, status := range map[int]models.TaskStatus{
		pageBacklogID: models.StatusBacklog, 25: models.StatusSprint, pageTaskID: models.StatusDoing,
		24: models.StatusTesting, pageClosedTaskID: models.StatusCompleted,
	} {
		page := servePage(t, mux, taskPagePath(id))
		want := `<div class="page-pretitle">Task #` + itoa(id) + ` <span class="badge ` + taskStatusBadge(status) +
			`">` + string(status) + `</span></div>`
		if !strings.Contains(page, want) {
			t.Errorf("task #%d: the pretitle is not %s", id, want)
		}
	}
}

// ==================== AC 222 AND 223: THE SPRINT CARD ====================

// TestTaskPage_SprintCardOfATaskInASprint is the gate for Acceptance Criterion
// 222.
func TestTaskPage_SprintCardOfATaskInASprint(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)
	mux := buildMux()

	card := taskSprintCardSlice(t, servePage(t, mux, taskPagePath(pageTaskID)))
	ordered := []string{
		`<h3 class="card-title">Sprint</h3>`,
		`href="/roadmaps/payments/sprints/7" data-role="task-sprint-link">Sprint #7 Payments hardening</a>`,
		`<span class="badge bg-blue-lt">OPEN</span>`,
		`>Position 3 of 11</div>`,
		`>4 of 11 tasks completed</div>`,
		`<progress class="progress progress-sm" value="4" max="11" aria-label="Sprint progress"></progress>`,
	}
	last := -1
	for _, want := range ordered {
		at := strings.Index(card, want)
		if at < 0 {
			t.Fatalf("the Sprint card does not carry %s\ncard: %s", want, card)
		}
		if at < last {
			t.Errorf("the Sprint card shows %s out of order", want)
		}
		last = at
	}
	// The badge sits outside the link.
	link := regionBetween(t, card, `<a `, "</a>")
	if strings.Contains(link, "badge") {
		t.Errorf("the sprint status badge is inside the sprint link")
	}
	if strings.Contains(card, "style=") {
		t.Errorf("the Sprint card carries an inline style")
	}
	// Context, not a sprint view.
	for _, gone := range []string{"markdown", "<time", "task-card", "timeline", "Payments maintenance"} {
		if strings.Contains(card, gone) {
			t.Errorf("the Sprint card shows %q, which belongs to the sprint page", gone)
		}
	}

	for id, want := range map[int]string{
		pageMembers[0]: "Position 1 of 11", pageBacklogID: "Position 11 of 11", 23: "Position 5 of 11",
	} {
		c := taskSprintCardSlice(t, servePage(t, mux, taskPagePath(id)))
		if !strings.Contains(c, ">"+want+"</div>") || !strings.Contains(c, ">4 of 11 tasks completed</div>") {
			t.Errorf("task #%d: the Sprint card does not read %s and 4 of 11\ncard: %s", id, want, c)
		}
	}
	// A BACKLOG member shows the sprint form, not the backlog text.
	backlog := taskSprintCardSlice(t, servePage(t, mux, taskPagePath(pageBacklogID)))
	if strings.Contains(backlog, "In the backlog") || !strings.Contains(backlog, "Sprint #7 Payments hardening") {
		t.Errorf("a BACKLOG member of a sprint does not show the sprint form")
	}
	// A sprint with no COMPLETED member reads 0 of <m>.
	empty := taskSprintCardSlice(t, servePage(t, mux, taskPagePath(30)))
	for _, want := range []string{">0 of 2 tasks completed</div>", `value="0" max="2"`, `<span class="badge bg-secondary-lt">PENDING</span>`} {
		if !strings.Contains(empty, want) {
			t.Errorf("a task of a sprint with no COMPLETED member: the card lacks %s\ncard: %s", want, empty)
		}
	}
}

// TestTaskPage_SprintCardOfATaskInNoSprint is the gate for Acceptance Criterion
// 223, including the second half: moving the task into a sprint and requesting
// the page again shows the sprint form, because the page reads the membership on
// every request.
func TestTaskPage_SprintCardOfATaskInNoSprint(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)
	mux := buildMux()

	card := taskSprintCardSlice(t, servePage(t, mux, taskPagePath(pageLooseTaskID)))
	body := regionBetween(t, card, `<div class="card-body">`, "</div>\n              </div>")
	text := strings.TrimSpace(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(body, ""))
	if text != "In the backlog: this task belongs to no sprint." {
		t.Errorf("the backlog Sprint card body reads %q", text)
	}
	for _, gone := range []string{"<a ", "badge", "Position", "<progress"} {
		if strings.Contains(card, gone) {
			t.Errorf("the backlog Sprint card carries %q", gone)
		}
	}

	database, err := db.Open(pageRoadmap)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	if err := database.AddTasksToSprint(context.Background(), pageSprintID, []int{pageLooseTaskID}); err != nil {
		t.Fatalf("adding the task to sprint 7: %v", err)
	}
	_ = database.Close() // test cleanup; a close error on a fixture handle is not under test

	moved := taskSprintCardSlice(t, servePage(t, mux, taskPagePath(pageLooseTaskID)))
	for _, want := range []string{"Sprint #7 Payments hardening", ">Position 12 of 12</div>", ">4 of 12 tasks completed</div>"} {
		if !strings.Contains(moved, want) {
			t.Errorf("after sprint add-tasks the Sprint card lacks %s\ncard: %s", want, moved)
		}
	}
}

// ==================== AC 224: THE DETAILS CARD ====================

// TestTaskPage_DetailsCard is the gate for Acceptance Criterion 224.
func TestTaskPage_DetailsCard(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)
	mux := buildMux()

	body := servePage(t, mux, taskPagePath(pageTaskID))
	details := detailsCardSlice(t, body)
	if !strings.Contains(details, `<h3 class="card-title">Details</h3>`) || !strings.Contains(details, `<div class="datagrid task-details-grid">`) {
		t.Fatalf("the Details card is not a Tabler datagrid titled Details")
	}
	titles, _ := datagridItems(t, details)
	want := []string{"Type", "Severity", "Priority", "Parent task", "Subtasks", "Depends on",
		"Blocks", "Created", "Started", "Tested", "Closed", "Commit open", "Commit close"}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Fatalf("the Details card holds %v, want exactly %v", titles, want)
	}

	checks := map[string]string{
		"Type":         `<div class="datagrid-content">TASK</div>`,
		"Priority":     `<div class="datagrid-content"><span class="badge ` + priorityBadge(7) + `">7</span></div>`,
		"Severity":     `<div class="datagrid-content"><span class="badge ` + severityBadge(2) + `">2</span></div>`,
		"Parent task":  `<div class="datagrid-content"><a href="/roadmaps/payments/tasks/40">#40</a></div>`,
		"Subtasks":     `<div class="datagrid-content">0</div>`,
		"Depends on":   `<div class="datagrid-content d-flex flex-wrap gap-2"><a href="/roadmaps/payments/tasks/12">#12</a><a href="/roadmaps/payments/tasks/17">#17</a></div>`,
		"Blocks":       `<div class="datagrid-content d-flex flex-wrap gap-2"><a href="/roadmaps/payments/tasks/51">#51</a></div>`,
		"Tested":       `<div class="datagrid-content text-secondary">&mdash;</div>`,
		"Closed":       `<div class="datagrid-content text-secondary">&mdash;</div>`,
		"Commit open":  `<div class="datagrid-content ` + taskHashClass + `">` + pageCommitOpen + `</div>`,
		"Commit close": absentContent,
	}
	for label, wantContent := range checks {
		if got := datagridContent(t, details, label); got != wantContent {
			t.Errorf("%s = %s, want %s", label, got, wantContent)
		}
	}
	if strings.Contains(details, ">P7<") || strings.Contains(details, ">S2<") {
		t.Errorf("the Details card carries a P or S badge label")
	}
	if strings.Contains(datagridContent(t, details, "Type"), "badge") {
		t.Errorf("the type is rendered as a badge")
	}
	for _, label := range []string{"ID", "Title", "Status"} {
		if strings.Contains(details, `<div class="datagrid-title">`+label+`</div>`) {
			t.Errorf("the Details card carries a %s field", label)
		}
	}
	// No task-reference link carries a target or a rel, and each one serves the
	// referenced task's page.
	for _, m := range regexp.MustCompile(`<a ([^>]*)>#(\d+)</a>`).FindAllStringSubmatch(details, -1) {
		if strings.Contains(m[1], "target=") || strings.Contains(m[1], "rel=") {
			t.Errorf("the reference #%s carries a target or a rel: %s", m[2], m[1])
		}
		page := servePage(t, mux, taskPagePath(atoi(t, m[2])))
		if !strings.Contains(page, `<div class="page-pretitle">Task #`+m[2]+` `) {
			t.Errorf("following #%s does not serve that task's page", m[2])
		}
	}
	// The commit hash carries no link.
	if strings.Contains(datagridContent(t, details, "Commit open"), "<a") {
		t.Errorf("the commit hash is a link")
	}

	// A task with no parent, no dependency, and no commit shows the em dash, and
	// an em dash is never a link.
	loose := detailsCardSlice(t, servePage(t, mux, taskPagePath(pageLooseTaskID)))
	for _, label := range []string{"Parent task", "Depends on", "Blocks", "Commit open", "Commit close"} {
		if got := datagridContent(t, loose, label); got != absentContent {
			t.Errorf("an absent %s = %s, want %s", label, got, absentContent)
		}
	}
	for _, label := range []string{"Started", "Tested", "Closed"} {
		if got := datagridContent(t, loose, label); got != `<div class="datagrid-content text-secondary">&mdash;</div>` {
			t.Errorf("an unset %s = %s, want the em dash and no <time>", label, got)
		}
	}
	// The parent shows its subtask count.
	parent := detailsCardSlice(t, servePage(t, mux, taskPagePath(pageParentID)))
	if got := datagridContent(t, parent, "Subtasks"); got != `<div class="datagrid-content">1</div>` {
		t.Errorf("the parent's Subtasks = %s, want 1", got)
	}
}

// ==================== AC 206: TIMESTAMPS ====================

// TestTaskPage_TimestampsUseTheDisplayForm is the gate for Acceptance Criterion
// 206 on the Details card: every set lifecycle timestamp is shown in the display
// form, inside a <time> carrying the stored value, formatted on the server.
func TestTaskPage_TimestampsUseTheDisplayForm(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)

	details := detailsCardSlice(t, servePage(t, buildMux(), taskPagePath(pageClosedTaskID)))
	for i, label := range []string{"Created", "Started", "Tested", "Closed"} {
		display, ok := canonicalTimestampDisplay(pageTimestamps[i])
		if !ok {
			t.Fatalf("fixture timestamp %q is not canonical", pageTimestamps[i])
		}
		want := `<div class="datagrid-content text-secondary"><time datetime="` + pageTimestamps[i] + `">` + display + `</time></div>`
		if got := datagridContent(t, details, label); got != want {
			t.Errorf("%s = %s, want %s", label, got, want)
		}
	}
	if !strings.Contains(details, ">2026-09-01 08:15:30</time>") {
		t.Errorf("the created_at display form is not truncated to the second")
	}
}

// ==================== AC 188: MARKDOWN FIELDS ====================

// TestTaskPage_MarkdownFieldCards is the gate for Acceptance Criterion 188: each
// of the four Markdown fields and each comment body is the renderer's output for
// its stored text, inside a <div class="markdown">; a null completion summary and
// an empty field show the em dash and no container.
func TestTaskPage_MarkdownFieldCards(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)
	mux := buildMux()

	closed := readFixtureTask(t, pageClosedTaskID)
	body := servePage(t, mux, taskPagePath(pageClosedTaskID))
	for _, f := range []struct{ title, field, source string }{
		{"Functional requirements", "functional_requirements", closed.FunctionalRequirements},
		{"Technical requirements", "technical_requirements", closed.TechnicalRequirements},
		{"Acceptance criteria", "acceptance_criteria", closed.AcceptanceCriteria},
		{"Completion summary", "completion_summary", derefString(closed.CompletionSummary)},
	} {
		want, err := renderMarkdown(f.source, taskFieldIDPrefix(pageClosedTaskID, f.field), markdownInteractive)
		if err != nil {
			t.Fatalf("rendering %s: %v", f.field, err)
		}
		if card := fieldCardSlice(t, body, f.title); !strings.Contains(card, `<div class="markdown">`+want+`</div>`) {
			t.Errorf("the %s card does not hold the renderer's output in a markdown container\ncard: %s", f.title, card)
		}
	}
	if !strings.Contains(fieldCardSlice(t, body, "Completion summary"), "<strong>15 minutes</strong>") {
		t.Errorf("the completion summary is not rendered as Markdown")
	}

	// The task 42 fields exercise a table and a task list.
	page := servePage(t, mux, taskPagePath(pageTaskID))
	if !strings.Contains(fieldCardSlice(t, page, "Technical requirements"), "<table") {
		t.Errorf("the technical requirements table is not rendered")
	}
	if !strings.Contains(fieldCardSlice(t, page, "Acceptance criteria"), `type="checkbox"`) {
		t.Errorf("the acceptance-criteria task list is not rendered")
	}
	// A null completion summary keeps its card, with the em dash and no container.
	summary := fieldCardSlice(t, page, "Completion summary")
	if strings.Contains(summary, `class="markdown"`) || !strings.Contains(summary, `<span class="text-secondary">—</span>`) {
		t.Errorf("a null completion summary does not show the em dash alone\ncard: %s", summary)
	}

	// An empty field does the same.
	database, err := db.Open(pageRoadmap)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	mustExec(t, database, `UPDATE tasks SET technical_requirements = '' WHERE id = ?`, pageLooseTaskID)
	_ = database.Close() // test cleanup; a close error on a fixture handle is not under test
	empty := fieldCardSlice(t, servePage(t, mux, taskPagePath(pageLooseTaskID)), "Technical requirements")
	if strings.Contains(empty, `class="markdown"`) || !strings.Contains(empty, `<span class="text-secondary">—</span>`) {
		t.Errorf("an empty field does not show the em dash alone\ncard: %s", empty)
	}
	if got := strings.Count(page, `data-role="task-field-card"`); got != 4 {
		t.Errorf("the page carries %d Markdown field cards, want 4", got)
	}
}

// ==================== AC 225: LAYOUT ====================

// TestTaskPage_Layout is the gate for Acceptance Criterion 225 in the served
// markup: one row row-cards of two columns, the side column first in the document
// carrying col-12 col-lg-4 and order-lg-last and holding the Sprint card then the
// Details card, the main column carrying col-12 col-lg-8 and holding the four
// field cards in order followed by the Comments card. The vendored stylesheet
// places an order-lg-last element after its siblings at the lg breakpoint, which
// is what stands the side column to the right of the main column there.
func TestTaskPage_Layout(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)

	body := servePage(t, buildMux(), taskPagePath(pageTaskID))
	main := regionBetween(t, body, `<main class="page-body">`, "</main>")
	if got := strings.Count(main, `<div class="row row-cards task-page">`); got != 1 {
		t.Fatalf("the page body holds %d task rows, want 1", got)
	}
	order := []string{
		`<div class="col-12 col-lg-4 order-lg-last">`,
		`data-role="task-sprint-card"`,
		`data-role="task-details-card"`,
		`<div class="col-12 col-lg-8">`,
		`<h3 class="card-title">Functional requirements</h3>`,
		`<h3 class="card-title">Technical requirements</h3>`,
		`<h3 class="card-title">Acceptance criteria</h3>`,
		`<h3 class="card-title">Completion summary</h3>`,
		`data-role="task-comments-card"`,
	}
	last := -1
	for _, want := range order {
		at := strings.Index(main, want)
		if at < 0 {
			t.Fatalf("the page body carries no %s", want)
		}
		if at < last {
			t.Errorf("%s is out of order", want)
		}
		last = at
	}
	if got := strings.Count(main, `class="col-`); got != 2 {
		t.Errorf("the row holds %d columns, want 2", got)
	}
	tabler := readEmbeddedAsset(t, "static/vendor/tabler/tabler.min.css")
	if !regexp.MustCompile(`@media \(min-width:992px\)\{[^@]*\.order-lg-last\{order:6!important\}`).MatchString(tabler) {
		t.Errorf("the vendored stylesheet does not order .order-lg-last last from 992px")
	}
	if strings.Contains(body, "full-height-page") {
		t.Errorf("the task page is marked as a full-height page region")
	}
}

// ==================== AC 226: READ COST ====================

// taskPageCounter wraps a real read-only connection and counts each read of the
// task page's read surface, and records the task id the comment listing was
// given.
type taskPageCounter struct {
	*db.DB
	commentTaskIDs []int
	tasks          int
	comments       int
	sprintResolves int
	sprints        int
	members        int
}

func (c *taskPageCounter) GetTask(ctx context.Context, id int) (*models.Task, error) {
	c.tasks++
	return c.DB.GetTask(ctx, id)
}

func (c *taskPageCounter) ListTaskComments(ctx context.Context, taskID int, ct *models.CommentType) ([]models.TaskComment, error) {
	c.comments++
	c.commentTaskIDs = append(c.commentTaskIDs, taskID)
	return c.DB.ListTaskComments(ctx, taskID, ct)
}

func (c *taskPageCounter) GetSprintsByTasks(ctx context.Context, ids []int) (map[int]db.SprintRef, error) {
	c.sprintResolves++
	return c.DB.GetSprintsByTasks(ctx, ids)
}

func (c *taskPageCounter) GetSprint(ctx context.Context, id int) (*models.Sprint, error) {
	c.sprints++
	return c.DB.GetSprint(ctx, id)
}

func (c *taskPageCounter) GetSprintTasksFull(ctx context.Context, sprintID int, status *models.TaskStatus, byPriority bool) ([]models.Task, error) {
	c.members++
	return c.DB.GetSprintTasksFull(ctx, sprintID, status, byPriority)
}

func (c *taskPageCounter) total() int {
	return c.tasks + c.comments + c.sprintResolves + c.sprints + c.members
}

// TestTaskPage_ReadCostIsFixed is the gate for Acceptance Criterion 226: five
// reads for a task in a sprint and three for a task in none, the same for a task
// with no comment and with fifty and for a sprint of one member and of fifty;
// exactly one comment listing, for this task; and no audit entry.
func TestTaskPage_ReadCostIsFixed(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const name = "payment-read-cost"

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	const now = "2026-08-30T08:00:00.000Z"
	mk := func(title string) int {
		id, terr := seedTask(database, seededTask(now, title))
		if terr != nil {
			t.Fatalf("creating task: %v", terr)
		}
		return id
	}
	quiet := mk("Document the refund reversal runbook")
	chatty := mk("Trace the duplicate payout reports")
	for i := 0; i < 50; i++ {
		addTaskCommentTo(t, database, chatty, models.CommentProgress,
			fmt.Sprintf("Payout batch %d traced to the retry queue.", i+1), now)
	}
	solo := mk("Rotate the webhook signing secret")
	crowded := mk("Reconcile the chargeback ledger")
	members := make([]int, 0, 50)
	members = append(members, crowded)
	for i := 0; i < 49; i++ {
		members = append(members, mk(fmt.Sprintf("Backfill settlement batch %d", i+1)))
	}
	soloSprint, _ := seedSprint(database, &models.Sprint{Status: models.SprintPending, Title: "Webhook hygiene", Description: "Webhook hygiene", CreatedAt: now})
	crowdedSprint, _ := seedSprint(database, &models.Sprint{Status: models.SprintPending, Title: "Ledger backfill", Description: "Ledger backfill", CreatedAt: now})
	if err := database.AddTasksToSprint(context.Background(), soloSprint, []int{solo}); err != nil {
		t.Fatalf("membership: %v", err)
	}
	if err := database.AddTasksToSprint(context.Background(), crowdedSprint, members); err != nil {
		t.Fatalf("membership: %v", err)
	}
	var auditBefore int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit`).Scan(&auditBefore); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	_ = database.Close() // test cleanup; a close error on a fixture handle is not under test

	for _, c := range []struct {
		label string
		id    int
		want  int
	}{
		{"a task in no sprint with no comment", quiet, 3},
		{"a task in no sprint with fifty comments", chatty, 3},
		{"a task in a sprint of one member", solo, 5},
		{"a task in a sprint of fifty members", crowded, 5},
	} {
		ro, oerr := db.OpenReadOnly(name)
		if oerr != nil {
			t.Fatalf("opening read-only: %v", oerr)
		}
		src := &taskPageCounter{DB: ro}
		if _, rerr := readTask(context.Background(), src, name, c.id); rerr != nil {
			t.Fatalf("%s: readTask: %v", c.label, rerr)
		}
		_ = ro.Close() // test cleanup
		if src.total() != c.want {
			t.Errorf("%s: %d reads, want %d", c.label, src.total(), c.want)
		}
		if src.tasks != 1 || src.comments != 1 || src.sprintResolves != 1 {
			t.Errorf("%s: task=%d comments=%d resolves=%d, want 1 each", c.label, src.tasks, src.comments, src.sprintResolves)
		}
		if len(src.commentTaskIDs) != 1 || src.commentTaskIDs[0] != c.id {
			t.Errorf("%s: the comment listing was issued for %v, want only task #%d", c.label, src.commentTaskIDs, c.id)
		}
	}

	// Serving the pages through the handler writes no audit entry.
	mux := buildMux()
	for _, id := range []int{quiet, chatty, solo, crowded} {
		servePage(t, mux, "/roadmaps/"+name+"/tasks/"+itoa(id))
	}
	ro, err := db.OpenReadOnly(name)
	if err != nil {
		t.Fatalf("opening read-only: %v", err)
	}
	defer ro.Close() //nolint:errcheck // test cleanup
	var auditAfter int
	if err := ro.QueryRow(`SELECT COUNT(*) FROM audit`).Scan(&auditAfter); err != nil && err != sql.ErrNoRows {
		t.Fatalf("counting audit rows: %v", err)
	}
	if auditAfter != auditBefore {
		t.Errorf("serving the task pages wrote %d audit entries", auditAfter-auditBefore)
	}
}

// ==================== AC 227: NARROW VIEWPORT STRUCTURE ====================

// TestTaskPage_StacksOnANarrowViewport pins the markup Acceptance Criterion 227
// depends on: below the lg breakpoint both columns are col-12, so they stack in
// document order — Sprint card, Details card, the four field cards, the Comments
// card — each spanning the page body; the page carries no inline style; the back
// link is a Tabler button, and the sprint link is padded, so both present a
// touch target larger than their text; and a rendered table sits in the
// Markdown container whose stylesheet scrolls it inside its own box.
func TestTaskPage_StacksOnANarrowViewport(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)

	body := servePage(t, buildMux(), taskPagePath(pageTaskID))
	if strings.Contains(body, "style=") {
		t.Errorf("the task page carries an inline style attribute")
	}
	for _, col := range regexp.MustCompile(`<div class="(col-[^"]*)">`).FindAllStringSubmatch(regionBetween(t, body, `<main class="page-body">`, "</main>"), -1) {
		if !strings.HasPrefix(col[1], "col-12 ") {
			t.Errorf("a column is %q; below lg every column must span the row", col[1])
		}
	}
	if !strings.Contains(body, `<a class="btn btn-outline-secondary" href="/roadmaps/payments/tasks">`) {
		t.Errorf("the back link is not a Tabler button")
	}
	if !strings.Contains(body, `<a class="d-inline-block py-1" href="/roadmaps/payments/sprints/7"`) {
		t.Errorf("the sprint link carries no padding of its own")
	}
	if !strings.Contains(fieldCardSlice(t, body, "Technical requirements"), `<div class="markdown"><p>Use the <code>kms</code> rotation API.</p>
<table>`) {
		t.Errorf("the table does not sit directly in the Markdown container")
	}
	css := readEmbeddedAsset(t, "static/style.css")
	if !regexp.MustCompile(`\.markdown table\s*\{[^}]*overflow-x:\s*auto`).MatchString(css) {
		t.Errorf("the stylesheet does not scroll a Markdown table inside its own box")
	}
}

// ==================== AC 224 AND 228: THE DETAILS GRID, WHOLE HASHES, NO OVERFLOW ====================

// TestTaskPage_DetailsGridColumnsAndWholeHashes pins what Acceptance Criteria 224
// and 228 rest on in the served bytes: the Details datagrid carries the
// task-details-grid class, the project stylesheet gives that class exactly two
// equal columns below 992px and one from 992px up, lets each item shrink below
// its content, and a 64-character commit hash is served whole, in the wrapping
// monospaced class, never truncated. The rendered column count and the absence of
// page overflow at every width of criterion 228 are measured in a browser (see
// the task report); the vendored datagrid's own 15rem auto-fit is what they would
// fall back to without these rules.
func TestTaskPage_DetailsGridColumnsAndWholeHashes(t *testing.T) {
	css := stripSpace(embeddedSheet(t, "static/style.css"))
	for _, want := range []string{
		".task-details-grid{grid-template-columns:repeat(2,minmax(0,1fr));}",
		".task-details-grid>.datagrid-item{min-width:0;}",
		"@media(min-width:992px){.task-details-grid{grid-template-columns:minmax(0,1fr);}}",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css lacks %q", want)
		}
	}
	if strings.Count(css, ".task-details-grid") != 3 {
		t.Errorf("the Details grid is styled by %d rules, want exactly the 3 above", strings.Count(css, ".task-details-grid"))
	}
	if !strings.Contains(embeddedSheet(t, "static/vendor/tabler/tabler.min.css"), "--tblr-datagrid-item-width:15rem") {
		t.Error("the vendored datagrid no longer declares its 15rem item width; revisit the override")
	}

	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)
	hash := strings.Repeat("a1b2c3d4", 8)
	database, err := db.Open(pageRoadmap)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	mustExec(t, database, `UPDATE tasks SET commit_open = ? WHERE id = ?`, hash, pageTaskID)
	_ = database.Close() // test cleanup; a close error on a fixture handle is not under test

	body := servePage(t, buildMux(), taskPagePath(pageTaskID))
	details := detailsCardSlice(t, body)
	if got := strings.Count(details, `<div class="datagrid task-details-grid">`); got != 1 {
		t.Fatalf("the Details card carries %d task-details-grid datagrids, want 1", got)
	}
	want := `<div class="datagrid-content font-monospace text-break">` + hash + `</div>`
	if got := datagridContent(t, details, "Commit open"); got != want {
		t.Errorf("a 64-character hash renders %s, want it whole in the wrapping class: %s", got, want)
	}
	if strings.Contains(details, "text-truncate") || strings.Contains(details, "…") {
		t.Error("the Details card truncates a value")
	}
	if strings.Contains(body, "style=") {
		t.Error("the task page carries an inline style attribute")
	}
}

// ==================== AC 229: VISIBLE KEYBOARD FOCUS ====================

// TestFocusVisible_BackLinksAndSidebarLinks pins UI Framework rule 21 in the
// stylesheet and the markup (Acceptance Criterion 229): one rule gives the header
// back link and every sidebar nav-link a solid 2px outline in #dce1e7 on
// :focus-visible only; a focused back link keeps its resting colours, so the
// outline stands against the dark page (11.5:1 against #262626, computed below);
// the selectors are the ones the task page's and the sprint page's markup
// actually carry. The outline in a real browser, its contrast against the
// computed backgrounds, and its absence on a pointer focus are measured in a
// browser (see the task report).
func TestFocusVisible_BackLinksAndSidebarLinks(t *testing.T) {
	css := stripSpace(embeddedSheet(t, "static/style.css"))
	for _, want := range []string{
		".page-headera.btn:focus-visible,.navbar-vertical.nav-link:focus-visible{outline:2pxsolid#dce1e7;outline-offset:2px;}",
		".page-headera.btn:focus-visible{--tblr-btn-hover-color:var(--tblr-btn-color);--tblr-btn-hover-bg:var(--tblr-btn-bg);--tblr-btn-hover-border-color:var(--tblr-btn-border-color);}",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css lacks %q", want)
		}
	}
	// No rule outside :focus-visible puts an outline on these elements, so a
	// pointer focus shows none.
	if regexp.MustCompile(`\.page-headera\.btn(?::focus)?\{[^}]*outline`).MatchString(css) ||
		regexp.MustCompile(`\.nav-link(?::focus)?\{[^}]*outline`).MatchString(css) {
		t.Error("an outline is set outside :focus-visible")
	}
	surface := relativeLuminance(0x26, 0x26, 0x26)
	if ratio := contrastRatio(relativeLuminance(0xdc, 0xe1, 0xe7), surface); ratio < 3 {
		t.Errorf("the focus outline has a contrast of %.2f:1 against #262626, below 3:1", ratio)
	}

	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)
	mux := buildMux()
	for path, back := range map[string]string{
		taskPagePath(pageTaskID): `<a class="btn btn-outline-secondary" href="/roadmaps/payments/tasks">`,
		"/roadmaps/" + pageRoadmap + "/sprints/" + itoa(pageSprintID): `<a class="btn btn-outline-secondary" href="/roadmaps/payments">`,
	} {
		body := servePage(t, mux, path)
		header := regionBetween(t, body, `<div class="page-header d-print-none">`, `<main class="page-body">`)
		if !strings.Contains(header, back) {
			t.Errorf("%s: the back link is not the a.btn the focus rule selects", path)
		}
		sidebar := regionBetween(t, body, `<aside class="navbar navbar-vertical`, "</aside>")
		if got := strings.Count(sidebar, `class="nav-link"`); got != 5 {
			t.Errorf("%s: %d sidebar nav-links, want 5", path, got)
		}
		if strings.Contains(body, "style=") {
			t.Errorf("%s carries an inline style attribute", path)
		}
	}
}

// ==================== AC 230: THE RECORD PAGES' ACTIONS COLUMN ====================

// TestRecordPages_ActionsColumnWrapsBelowTheTitle pins Acceptance Criterion 230 in
// the served markup: the task page and the sprint page emit their actions column
// as col-12 col-sm-auto ms-auto d-print-none, directly after the title column in
// the header row, and the other pages with actions keep col-auto. The column
// positions at 375px and 576px are measured in a browser (see the task report).
func TestRecordPages_ActionsColumnWrapsBelowTheTitle(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTaskPageFixture(t)
	mux := buildMux()
	for path, want := range map[string]string{
		taskPagePath(pageTaskID): `<div class="col-12 col-sm-auto ms-auto d-print-none">`,
		"/roadmaps/" + pageRoadmap + "/sprints/" + itoa(pageSprintID): `<div class="col-12 col-sm-auto ms-auto d-print-none">`,
		"/roadmaps/" + pageRoadmap + "/graph":                         `<div class="col-auto ms-auto d-print-none">`,
	} {
		header := regionBetween(t, servePage(t, mux, path), `<div class="page-header d-print-none">`, `<main class="page-body">`)
		if got := strings.Count(header, "ms-auto d-print-none"); got != 1 {
			t.Errorf("%s: %d actions columns, want 1", path, got)
		}
		if !strings.Contains(header, want) {
			t.Errorf("%s: the actions column is not %s", path, want)
		}
		if strings.Index(header, `<div class="col">`) > strings.Index(header, want) {
			t.Errorf("%s: the actions column precedes the title column", path)
		}
	}
}

// ==================== AC 231: BOTH COMMENTS CARDS STATE THEIR ORDER ====================

// TestCommentsCards_StateTheirOrder is the gate for Acceptance Criterion 231: on
// the task page and the sprint page, with comments and without, the Comments
// card's header holds, after its title and count badge, exactly one
// <div class="card-actions text-secondary">Oldest first</div>, which holds no
// link, button, or form control.
func TestCommentsCards_StateTheirOrder(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()
	const want = `<div class="card-actions text-secondary">Oldest first</div>`
	for _, path := range []string{
		"/roadmaps/" + f.name + "/tasks/" + itoa(f.loggedTaskID),
		"/roadmaps/" + f.name + "/tasks/" + itoa(f.quietTaskID),
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.sprintID),
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.quietSprintID),
	} {
		body := servePage(t, mux, path)
		header := regionBetween(t, body, `<h3 class="card-title">Comments <span class="badge bg-secondary-lt ms-2">`, `<div class="card-body">`)
		if got := strings.Count(header, "card-actions"); got != 1 || !strings.Contains(header, "</h3>\n") {
			t.Errorf("%s: the Comments header holds %d card-actions elements, want 1 after the title", path, got)
		}
		if !strings.Contains(header, want) {
			t.Errorf("%s: the Comments header does not carry %s\nheader: %s", path, want, header)
		}
		if strings.Index(header, want) < strings.Index(header, "</h3>") {
			t.Errorf("%s: the order statement precedes the card title", path)
		}
		for _, bad := range []string{"<a ", "<button", "<input", "<select"} {
			if strings.Contains(header, bad) {
				t.Errorf("%s: the Comments header holds %q", path, bad)
			}
		}
	}
}
