package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for the task detail endpoint and the single modal shell
// it fills (SPEC/WEB.md § Task Detail Endpoint; § Task Detail Modal; Acceptance
// Criteria 94 to 99).
//
// The page used to carry one fully populated modal per task, whether or not the
// user opened one: on a 100-task board that was 774,484 of the document's 930,188
// bytes — 83 percent — for content a user opens one of at a time. The page now
// carries one empty shell, and a task's data travels only when a user asks for it.
//
// Moving those values out of html/template and into JSON moved the escaping
// responsibility to the client script, so the security property this file pins is
// the one that matters most: every value the script writes into the DOM is
// written as TEXT.

// ==================== THE ENDPOINT ====================

// TestTaskDetailEndpoint_ReturnsTheTaskAndItsComments is the gate for Acceptance
// Criterion 94: the endpoint answers 200 with exactly two members — the task's
// full field set and its comments, oldest first — in the object shapes
// DATA_FORMATS.md already fixes, introducing no new shape.
func TestTaskDetailEndpoint_ReturnsTheTaskAndItsComments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	req := httptest.NewRequest(http.MethodGet,
		"/roadmaps/"+f.name+"/tasks/"+itoa(f.loggedTaskID)+"/data", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeJSON {
		t.Errorf("content-type = %q, want %q", ct, contentTypeJSON)
	}

	// Exactly two members, named as the SPEC names them.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decoding the envelope: %v", err)
	}
	if len(envelope) != 2 {
		t.Errorf("the response carries %d members (%v), want exactly task and comments",
			len(envelope), envelope)
	}
	for _, member := range []string{"task", "comments"} {
		if _, ok := envelope[member]; !ok {
			t.Errorf("the response carries no %q member", member)
		}
	}

	// The task object is the DATA_FORMATS Task shape: the same field names the CLI
	// emits, which is what "introduces no new object shape" means. The set is taken
	// from the model's own JSON tags rather than restated here.
	var task map[string]any
	if err := json.Unmarshal(envelope["task"], &task); err != nil {
		t.Fatalf("decoding the task: %v", err)
	}
	for _, field := range taskJSONFields(t) {
		if _, ok := task[field]; !ok {
			t.Errorf("the task object carries no %q field", field)
		}
	}
	if len(task) != len(taskJSONFields(t)) {
		t.Errorf("the task object carries %d fields, want the %d of the Task shape",
			len(task), len(taskJSONFields(t)))
	}

	// Every field the modal displays is present and carries the stored value.
	view := decodeTaskDetail(t, mux, f.name, f.loggedTaskID)
	if view.Task.ID != f.loggedTaskID {
		t.Errorf("task id = %d, want %d", view.Task.ID, f.loggedTaskID)
	}
	if view.Task.Title == "" || view.Task.FunctionalRequirements == "" ||
		view.Task.TechnicalRequirements == "" || view.Task.AcceptanceCriteria == "" {
		t.Errorf("the task detail is missing a field the modal shows: %+v", view.Task)
	}
	if len(view.Comments) != 3 {
		t.Errorf("the task detail carries %d comments, want its whole log of 3", len(view.Comments))
	}
}

// taskJSONFields returns the JSON field names of the Task shape, read from the
// model's own tags so the assertion cannot drift from DATA_FORMATS.md § Task.
func taskJSONFields(t *testing.T) []string {
	t.Helper()

	encoded, err := json.Marshal(models.Task{})
	if err != nil {
		t.Fatalf("marshalling the Task shape: %v", err)
	}
	var shape map[string]any
	if err := json.Unmarshal(encoded, &shape); err != nil {
		t.Fatalf("decoding the Task shape: %v", err)
	}
	fields := make([]string, 0, len(shape))
	for field := range shape {
		fields = append(fields, field)
	}
	return fields
}

// TestTaskDetailEndpoint_PathDiscipline is the gate for Acceptance Criterion 95:
// the endpoint enforces the same path-parameter discipline as every other roadmap
// route, serves GET and HEAD only, and carries the no-store header every
// data-derived response carries.
func TestTaskDetailEndpoint_PathDiscipline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	// A second roadmap, so "a task of another roadmap" is a real case rather than
	// a hypothetical one.
	other := seedRoadmap(t, "platform-core")
	srv := handler()

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	// The happy path, so the 404 cases below are not passing for want of a route.
	ok := get("/roadmaps/" + f.name + "/tasks/" + itoa(f.loggedTaskID) + "/data")
	if ok.Code != http.StatusOK {
		t.Fatalf("the endpoint answers %d for a task of the roadmap, want 200", ok.Code)
	}
	if cc := ok.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}

	// A task of ANOTHER roadmap is not reachable through this roadmap's path
	// space. The id is one that exists in the other roadmap and not in this one.
	otherTasks := roadmapTaskTitles(t, other)
	if len(otherTasks) == 0 {
		t.Fatal("the second roadmap seeded no task, so the cross-roadmap case is untested")
	}
	thisRoadmap := roadmapTaskTitles(t, f.name)
	foreign := 0
	for id := range otherTasks {
		if _, present := thisRoadmap[id]; !present {
			foreign = id
			break
		}
	}
	if foreign == 0 {
		// Every id of the other roadmap also exists here; use an id beyond both.
		foreign = len(thisRoadmap) + len(otherTasks) + 1000
	}

	for _, c := range []struct {
		name string
		path string
	}{
		{"invalid roadmap name", "/roadmaps/NotValid/tasks/1/data"},
		{"path traversal in the name", "/roadmaps/..%2fetc/tasks/1/data"},
		{"unknown roadmap", "/roadmaps/no-such-roadmap/tasks/1/data"},
		{"non-integer id", "/roadmaps/" + f.name + "/tasks/not-a-number/data"},
		{"negative id", "/roadmaps/" + f.name + "/tasks/-1/data"},
		{"unknown task", "/roadmaps/" + f.name + "/tasks/99999/data"},
		{"task of another roadmap", "/roadmaps/" + f.name + "/tasks/" + itoa(foreign) + "/data"},
		{"the page path without /data", "/roadmaps/" + f.name + "/tasks/" + itoa(f.loggedTaskID)},
	} {
		if got := get(c.path).Code; got != http.StatusNotFound {
			t.Errorf("%s: GET %s = %d, want 404", c.name, c.path, got)
		}
	}

	// HEAD is served; every write method is refused.
	path := "/roadmaps/" + f.name + "/tasks/" + itoa(f.loggedTaskID) + "/data"
	head := httptest.NewRecorder()
	srv.ServeHTTP(head, httptest.NewRequest(http.MethodHead, path, nil))
	if head.Code != http.StatusOK {
		t.Errorf("HEAD %s = %d, want 200", path, head.Code)
	}
	for _, method := range []string{
		http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 405", method, path, rec.Code)
		}
	}
}

// TestTaskDetailEndpoint_ReadsOneTaskAndItsComments pins the endpoint's read
// cost: two reads for the one task requested, and none of the page-level grouped
// reads, so opening a modal never reintroduces a per-task query into page
// rendering (SPEC/WEB.md § Task Detail Endpoint, Reads).
func TestTaskDetailEndpoint_ReadsOneTaskAndItsComments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	src := openCounting(t, f.name)

	view, err := readTaskDetail(context.Background(), src, f.loggedTaskID)
	if err != nil {
		t.Fatalf("readTaskDetail: %v", err)
	}

	if len(view.Comments) != 3 {
		t.Errorf("the detail carries %d comments, want 3", len(view.Comments))
	}
	if src.perTaskComments != 1 {
		t.Errorf("the endpoint issued %d comment listings, want exactly 1 for the one task it serves",
			src.perTaskComments)
	}
	for label, count := range map[string]int{
		"grouped comment count": src.groupedCommentCounts,
		"grouped sprint":        src.groupedTaskSprints,
		"task list":             src.taskList,
	} {
		if count != 0 {
			t.Errorf("the endpoint issued %d %s queries, want 0: it serves one task", count, label)
		}
	}
}

// ==================== ONE MODAL SHELL, AND THE DOCUMENT IT SAVES ====================

// TestTasksPage_CarriesOneModalShellAndNoTaskDetail is the gate for Acceptance
// Criterion 96: the page contains exactly one modal element, carries no task's
// modal content at all, and its size therefore grows only with the cards.
//
// The reduction is measured rather than asserted in prose: the recorded baseline
// is 930,188 bytes for 100 tasks, of which 774,484 (83 percent) were the rendered
// modals. The page is measured here at two task counts, so the per-task marginal
// cost is measured too — a card, not a card plus a 7.7 KB modal.
func TestTasksPage_CarriesOneModalShellAndNoTaskDetail(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const (
		baselineBytes    = 930188 // the recorded document for 100 tasks
		baselineModals   = 774484 // of which the per-task modals were this much
		baselineTasks    = 100
		halfTasks        = 50
		wantMarginalCap  = 2500 // bytes per task: a card, not a card plus a modal
		oldPerTaskBudget = (baselineBytes - baselineModals) / baselineTasks
	)

	full := seedTasksWithComments(t, "settlement-window-full", baselineTasks)
	half := seedTasksWithComments(t, "settlement-window-half", halfTasks)
	if len(full) != baselineTasks || len(half) != halfTasks {
		t.Fatalf("seeded %d and %d tasks, want %d and %d", len(full), len(half), baselineTasks, halfTasks)
	}
	mux := buildMux()

	fullBody := servePage(t, mux, "/roadmaps/settlement-window-full/tasks")
	halfBody := servePage(t, mux, "/roadmaps/settlement-window-half/tasks")

	// Exactly one modal element on the page, and it is the shell.
	if got := strings.Count(fullBody, modalOpenTag); got != 1 {
		t.Errorf("the page carries %d modal elements, want exactly 1 for %d tasks", got, baselineTasks)
	}
	if got := strings.Count(fullBody, `id="task-modal"`); got != 1 {
		t.Errorf("the page carries %d modal shells, want exactly 1", got)
	}
	if rePerTaskModal.MatchString(fullBody) {
		t.Errorf("the page still renders a per-task modal")
	}

	// No task's modal content travels in the document: the fields the modal shows
	// and the comment bodies are absent.
	for _, absent := range []string{
		"Functional requirements", "Technical requirements", "Acceptance criteria",
		"Completion summary", "balances to the cent after the rounding fix",
	} {
		if strings.Contains(fullBody, absent) {
			t.Errorf("the page carries the modal content %q; it must be fetched on demand", absent)
		}
	}

	// The measurement. The document is a fraction of the baseline, and the
	// marginal bytes per task are card-sized.
	marginal := (len(fullBody) - len(halfBody)) / (baselineTasks - halfTasks)
	t.Logf("tasks page: %d bytes for %d tasks (baseline %d), %d bytes for %d tasks; "+
		"marginal %d bytes/task (was %d card + ~7.7 KB modal)",
		len(fullBody), baselineTasks, baselineBytes, len(halfBody), halfTasks,
		marginal, oldPerTaskBudget)

	if len(fullBody) >= baselineBytes-baselineModals+baselineTasks*wantMarginalCap {
		t.Errorf("the page is %d bytes for %d tasks; the modal share of the %d-byte baseline "+
			"is not gone", len(fullBody), baselineTasks, baselineBytes)
	}
	if marginal >= wantMarginalCap {
		t.Errorf("each task adds %d bytes to the document, want under %d: the per-task cost must "+
			"be a card, not a card plus a modal", marginal, wantMarginalCap)
	}
	// And the saving against the recorded baseline is at least the modal share.
	if saved := baselineBytes - len(fullBody); saved < baselineModals {
		t.Errorf("the page saves %d bytes against the %d-byte baseline, want at least the %d "+
			"bytes the modals cost", saved, baselineBytes, baselineModals)
	}
}

// ==================== THE SCRIPT: TEXT ONLY ====================

// TestTaskModalScript_WritesEveryValueAsText is the gate for Acceptance Criterion
// 97, the security property this change turns on: every value the script writes
// into the DOM is written as text, never as markup.
//
// With no browser in the test environment, the property is asserted at its
// source — the script the binary serves — and it is asserted as an ABSENCE of
// every markup-parsing sink plus the presence of the text-only ones. A script
// that wrote a value as markup would have to use one of the forbidden sinks, so
// this test fails the moment one appears.
func TestTaskModalScript_WritesEveryValueAsText(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// The scan runs on the CODE, with comments stripped: the file's own header
	// explains which sinks it must never use, and naming them in prose must not
	// read as using them.
	script := stripJSComments(readEmbeddedAsset(t, "static/task-modal.js"))

	// Every sink that parses markup, and every dynamic-code sink.
	for _, sink := range []string{
		"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write",
		"eval(", "new Function", "srcdoc", "createContextualFragment",
		"setHTMLUnsafe", "javascript:",
	} {
		if strings.Contains(script, sink) {
			t.Errorf("the modal script uses %q; every value it writes must go in as text", sink)
		}
	}

	// The text-only sink is used, and used for the caller-authored values: the
	// helper that builds every element assigns through textContent.
	if !strings.Contains(script, "node.textContent = String(text)") {
		t.Error("the modal script's element helper does not assign its text through textContent")
	}
	if got := strings.Count(script, "textContent"); got < 10 {
		t.Errorf("the modal script assigns textContent %d times; the modal writes far more "+
			"values than that, so some other sink is in use", got)
	}
	// Containers are emptied structurally rather than by assigning markup.
	if !strings.Contains(script, "replaceChildren(") {
		t.Error("the modal script does not clear its containers with replaceChildren")
	}

	// setAttribute must never be used to set an event handler or a URL-bearing
	// attribute from fetched data.
	for _, attr := range []string{`setAttribute("on`, `setAttribute("href`, `setAttribute("src`} {
		if strings.Contains(script, attr) {
			t.Errorf("the modal script sets %q from data; that is a markup sink in disguise", attr)
		}
	}
}

// TestTaskModal_HostileValuesNeverReachThePageAsMarkup is the data-path half of
// Acceptance Criterion 97: a task whose title, completion summary, requirement
// text or comment body contains HTML markup carries that markup as a JSON string
// value, escaped by the encoder, and never reaches the served page as markup.
//
// It fails if the endpoint ever interpolated a value into markup, and — together
// with TestTaskModalScript_WritesEveryValueAsText, which fails if the script ever
// wrote one as markup — the two cover the whole path from the database to the DOM.
func TestTaskModal_HostileValuesNeverReachThePageAsMarkup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const (
		hostileTaskTitle = `Reject <script>alert("xss")</script> in a title`
		hostileSummary   = `Closed with <img src=x onerror=alert(1)> in the summary`
		hostileComment   = `The parser rejected <iframe src="javascript:alert(1)"></iframe> as input`
	)

	name := "hostile-values"
	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	taskID, err := database.CreateTask(context.Background(), &models.Task{
		Priority:               5,
		Severity:               5,
		Status:                 models.StatusBacklog,
		Title:                  hostileTaskTitle,
		FunctionalRequirements: `Requirements with <b>markup</b> in them`,
		TechnicalRequirements:  `Implemented with <script>ignored()</script>`,
		AcceptanceCriteria:     `Verified against <div>markup</div>`,
		CreatedAt:              "2026-08-10T08:00:00Z",
	})
	if err != nil {
		t.Fatalf("creating the hostile task: %v", err)
	}
	// The completion summary is written only by the COMPLETED transition, whose
	// statement is issued here directly so the value under test is stored exactly
	// as that transition stores it.
	if _, err := database.ExecContext(context.Background(),
		"UPDATE tasks SET status = ?, closed_at = ?, completion_summary = ? WHERE id = ?",
		models.StatusCompleted, "2026-08-12T08:00:00Z", hostileSummary, taskID); err != nil {
		t.Fatalf("setting the hostile completion summary: %v", err)
	}
	addTaskCommentTo(t, database, taskID, models.CommentFinding, hostileComment, "2026-08-11T08:00:00Z")
	if err := database.Close(); err != nil {
		t.Fatalf("closing the roadmap: %v", err)
	}

	mux := buildMux()
	page := servePage(t, mux, "/roadmaps/"+name+"/tasks")

	// The page renders the title (on the card) escaped, and carries none of the
	// values that live only in the modal.
	if strings.Contains(page, hostileTaskTitle) {
		t.Errorf("the raw task title reached the page as markup")
	}
	if !strings.Contains(page, rendered(hostileTaskTitle)) {
		t.Errorf("the card does not show the task title escaped")
	}
	for _, absent := range []string{
		hostileSummary, hostileComment, "<img src=x", "<iframe", "onerror=",
		`<script>alert("xss")</script>`, "<b>markup</b>",
	} {
		if strings.Contains(page, absent) {
			t.Errorf("the page carries %q; the modal's values are fetched on demand", absent)
		}
	}
	// The page's script elements are exactly the two it loads.
	if got := strings.Count(page, "<script"); got != 2 {
		t.Errorf("the page has %d <script elements, want 2", got)
	}

	// The endpoint carries every hostile value as a JSON string, with the
	// HTML-significant characters escaped by the encoder.
	status, body := fetchTaskDetail(t, mux, name, taskID)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	for _, raw := range []string{"<script", "</script>", "<img", "<iframe", "<b>", "<div>"} {
		if strings.Contains(body, raw) {
			t.Errorf("the endpoint emitted %q unescaped in its JSON body", raw)
		}
	}
	// And every value round-trips: the client receives exactly what was written.
	view := decodeTaskDetail(t, mux, name, taskID)
	for label, got := range map[string]string{
		"title":              view.Task.Title,
		"completion summary": derefString(view.Task.CompletionSummary),
		"comment body":       view.Comments[0].Body,
	} {
		want := map[string]string{
			"title":              hostileTaskTitle,
			"completion summary": hostileSummary,
			"comment body":       hostileComment,
		}[label]
		if got != want {
			t.Errorf("the %s decoded to %q, want %q", label, got, want)
		}
	}
}

// derefString returns a *string's value, or "" when it is nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// TestTaskModalScript_BadgeMappingMatchesTheServerHelpers pins the script's badge
// tables against badge.go, which is the single source of truth for the mapping
// SPEC/WEB.md § Status, Priority, and Severity Badge Colours fixes.
//
// The modal's badges are now painted by the script, so the mapping exists twice:
// once in Go for the server-rendered surfaces, once in JavaScript for the modal.
// Two copies drift unless something compares them — this is that something, and
// it compares every value of every enum rather than a sample.
func TestTaskModalScript_BadgeMappingMatchesTheServerHelpers(t *testing.T) {
	script := readEmbeddedAsset(t, "static/task-modal.js")

	// Task status: every value of the enum.
	statuses := scriptObjectMap(t, script, "STATUS_BADGE")
	if len(statuses) != len(models.ValidTaskStatuses) {
		t.Errorf("the script's status table has %d entries, want the enum's %d",
			len(statuses), len(models.ValidTaskStatuses))
	}
	for _, status := range models.ValidTaskStatuses {
		if got, want := statuses[string(status)], taskStatusBadge(status); got != want {
			t.Errorf("the script maps %s to %q, want %q (badge.go)", status, got, want)
		}
	}

	// Priority and severity: every value of the 0-9 range, in order.
	priorities := scriptArrayTable(t, script, "PRIORITY_BADGE")
	severities := scriptArrayTable(t, script, "SEVERITY_BADGE")
	if len(priorities) != 10 || len(severities) != 10 {
		t.Fatalf("the script's priority/severity tables have %d/%d entries, want 10 each",
			len(priorities), len(severities))
	}
	for value := range 10 {
		if got, want := priorities[value], priorityBadge(value); got != want {
			t.Errorf("the script maps priority %d to %q, want %q (badge.go)", value, got, want)
		}
		if got, want := severities[value], severityBadge(value); got != want {
			t.Errorf("the script maps severity %d to %q, want %q (badge.go)", value, got, want)
		}
	}

	// The comment type badge is the neutral variant for every type.
	if !strings.Contains(script, `var COMMENT_TYPE_BADGE = "`+badgeSecondary+`"`) {
		t.Errorf("the script's comment-type badge is not the neutral %q", badgeSecondary)
	}
}

// scriptObjectMap parses a `var NAME = { KEY: "value", ... };` table out of the
// script.
func scriptObjectMap(t *testing.T, script, name string) map[string]string {
	t.Helper()

	block := scriptBlock(t, script, name, "{", "}")
	entries := regexp.MustCompile(`(\w+): "([^"]*)"`).FindAllStringSubmatch(block, -1)
	if len(entries) == 0 {
		t.Fatalf("the script's %s table has no entries; the extraction is broken", name)
	}
	table := make(map[string]string, len(entries))
	for _, entry := range entries {
		table[entry[1]] = entry[2]
	}
	return table
}

// scriptArrayTable parses a `var NAME = [ "value", ... ];` table out of the
// script, preserving order so the index is the enum value.
func scriptArrayTable(t *testing.T, script, name string) []string {
	t.Helper()

	block := scriptBlock(t, script, name, "[", "]")
	entries := regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(block, -1)
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		values = append(values, entry[1])
	}
	return values
}

// scriptBlock returns the text between the delimiters of a named declaration.
func scriptBlock(t *testing.T, script, name, open, close string) string {
	t.Helper()

	at := strings.Index(script, "var "+name+" = "+open)
	if at < 0 {
		t.Fatalf("the script declares no %s table", name)
	}
	rest := script[at:]
	end := strings.Index(rest, close)
	if end < 0 {
		t.Fatalf("the script's %s table is not closed", name)
	}
	return rest[:end]
}

// ==================== FAILURE IS VISIBLE ====================

// TestTaskModal_ReportsAFailedFetchInTheModal is the gate for Acceptance
// Criterion 99: a failed fetch opens the modal on a clear message rather than
// leaving it blank, closing it, or leaving the previously opened task on display.
//
// The shell's own markup and the script's failure path are asserted together: the
// shell carries the error element the script fills, and the script clears the
// previous task's content before every fetch and reports on every failure mode the
// SPEC names — a network error, a non-200 response, and a body that does not parse.
func TestTaskModal_ReportsAFailedFetchInTheModal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+name+"/tasks")
	for _, element := range []string{
		`id="task-modal-error"`, `role="alert"`, `id="task-modal-loading"`, `id="task-modal-content"`,
	} {
		if !strings.Contains(body, element) {
			t.Errorf("the modal shell carries no %s element for the failure state", element)
		}
	}

	script := readEmbeddedAsset(t, "static/task-modal.js")
	// The failure path exists, is reached from every failure mode, and says what
	// happened.
	for _, fragment := range []string{
		"function showError(",
		"errorEl.hidden = false",
		"This task's detail could not be loaded",
		".catch(function ()", // a network error
		"if (!resp.ok)",      // a non-200 response
		"malformed body",     // a body that does not parse
	} {
		if !strings.Contains(script, fragment) {
			t.Errorf("the modal script has no %q in its failure path", fragment)
		}
	}
	// The previous task is cleared BEFORE the fetch, so a failure can never leave
	// it on display, and a late response for another task is discarded.
	if !strings.Contains(script, "reset();") {
		t.Error("the modal script does not clear the previous task before fetching")
	}
	if !strings.Contains(script, "token !== requestToken") {
		t.Error("the modal script does not discard a response for a task no longer being shown")
	}
	// The failure path writes nothing.
	if strings.Contains(script, "retry") && strings.Contains(script, "POST") {
		t.Error("the modal script's failure path offers a write")
	}
}

// stripJSComments removes // and /* */ comments from JavaScript source, leaving
// string literals intact, so a scan for a forbidden construct measures the code
// rather than the prose that describes it.
func stripJSComments(src string) string {
	var out strings.Builder
	out.Grow(len(src))

	const (
		code = iota
		lineComment
		blockComment
		doubleQuoted
		singleQuoted
	)
	state := code

	for i := 0; i < len(src); i++ {
		c := src[i]
		switch state {
		case code:
			switch {
			case c == '/' && i+1 < len(src) && src[i+1] == '/':
				state = lineComment
				i++
			case c == '/' && i+1 < len(src) && src[i+1] == '*':
				state = blockComment
				i++
			case c == '"':
				state = doubleQuoted
				out.WriteByte(c)
			case c == '\'':
				state = singleQuoted
				out.WriteByte(c)
			default:
				out.WriteByte(c)
			}
		case lineComment:
			if c == '\n' {
				state = code
				out.WriteByte(c)
			}
		case blockComment:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				state = code
				i++
			}
		case doubleQuoted, singleQuoted:
			out.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				out.WriteByte(src[i])
				continue
			}
			if (state == doubleQuoted && c == '"') || (state == singleQuoted && c == '\'') {
				state = code
			}
		}
	}
	return out.String()
}
