package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The comment log on the read-only web interface (SPEC/WEB.md § Task Detail
// Modal, comments timeline; § Sprint Detail Sub-Template, Comments card;
// Acceptance Criteria 64-73).
//
// Every assertion here is made against markup rendered from a real on-disk
// SQLite roadmap seeded through the production write API, so what is measured is
// what a browser receives.

// Seeded comment bodies. They are declared as constants because most assertions
// look for them verbatim in the rendered page, and because two of them carry
// authored line breaks that must survive rendering: the newline is written as the
// Go escape \n, never as a literal control character in the source.
const (
	// The three comments of the "logged" task, oldest first.
	bodyFinding = "The settlement reconciliation drifts by one cent on windows " +
		"that close mid-second: the rounding runs before the currency conversion."
	bodyHypothesis = "Converting first and rounding once at the end should remove the drift.\n" +
		"The ledger export of 2026-08-12 is the reference to compare against."
	bodyDecisionOriginal = "Round once, after the conversion."
	bodyDecisionEdited   = "Round once, after the conversion, and keep the residual per window " +
		"in the reconciliation report.\nThe per-window residual is what makes a drift traceable."

	// The comment whose body carries markup: it must render as text.
	bodyMarkup = "Regression input: <script>alert('drift')</script> and a <b>bold</b> label.\n" +
		"Both must reach the page as text."

	// The comment of the task that belongs to no sprint.
	bodyLoose = "Deferred: the currency table refresh is out of scope for this window."

	// The sprint's own two comments, oldest first.
	bodySprintProgress = "Two of the six planned tasks are closed; the reconciliation " +
		"work is on track for the window freeze."
	bodySprintDecisionOriginal = "The conversion rewrite moves to the next sprint."
	bodySprintDecisionEdited   = "The conversion rewrite moves to the next sprint: it needs the " +
		"currency table refresh, which is not in this sprint's scope."
)

// Seeded comment timestamps. created_at drives the oldest-first order and
// updated_at marks an entry as edited, so both are fixed rather than taken from
// the clock.
const (
	createdFinding        = "2026-08-14T09:12:00.000Z"
	createdHypothesis     = "2026-08-14T11:40:00.000Z"
	createdDecision       = "2026-08-15T08:05:00.000Z"
	updatedDecision       = "2026-08-16T07:30:00.000Z"
	createdMarkup         = "2026-08-14T13:05:00.000Z"
	createdLoose          = "2026-08-15T16:20:00.000Z"
	createdSprintProgress = "2026-08-13T17:00:00.000Z"
	createdSprintDecision = "2026-08-16T09:15:00.000Z"
	updatedSprintDecision = "2026-08-16T10:00:00.000Z"
)

// commentFixture is the seeded shape every comment-rendering assertion reads. It
// deliberately contains all four interesting cases side by side: a task with a
// log (including an edited entry), a task whose comment body is markup, a task
// with no comments at all, and a task outside any sprint; plus a sprint with its
// own log and a second sprint with none.
type commentFixture struct {
	name          string
	sprintID      int // OPEN sprint: three member tasks and two comments of its own
	quietSprintID int // PENDING sprint: no comments at all
	loggedTaskID  int // member task with three comments, the last one edited
	markupTaskID  int // member task with one comment whose body carries HTML
	quietTaskID   int // member task with no comments
	looseTaskID   int // task in no sprint, with one comment
}

// seedCommentFixture creates the roadmap on disk under the test's temporary HOME.
// The caller must have redirected HOME first.
func seedCommentFixture(t *testing.T, name string) commentFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	const now = "2026-08-10T08:00:00Z"

	mkTask := func(title string) int {
		id, terr := seedTask(database, seededTask(now, title))
		if terr != nil {
			t.Fatalf("creating task %q: %v", title, terr)
		}
		return id
	}
	mkSprint := func(title string, order int) int {
		id, serr := seedSprint(database, &models.Sprint{
			Status:      models.SprintPending,
			Title:       title,
			Description: title,
			CreatedAt:   now,
			Order:       order,
		})
		if serr != nil {
			t.Fatalf("creating sprint %q: %v", title, serr)
		}
		return id
	}

	f := commentFixture{name: name}

	f.sprintID = mkSprint("Reconcile the settlement windows to the cent", 10)
	f.loggedTaskID = mkTask("Remove the one-cent drift from the settlement reconciliation")
	f.markupTaskID = mkTask("Reject a reconciliation input that carries markup")
	f.quietTaskID = mkTask("Publish the residual per settlement window")
	if aerr := database.AddTasksToSprint(ctx, f.sprintID,
		[]int{f.loggedTaskID, f.markupTaskID, f.quietTaskID}); aerr != nil {
		t.Fatalf("adding tasks to sprint: %v", aerr)
	}
	forceSprintOpen(t, database, f.sprintID)

	f.quietSprintID = mkSprint("Refresh the currency table from the reference feed", 20)
	f.looseTaskID = mkTask("Retire the legacy settlement importer")

	// The logged task's work log, seeded oldest first. The DECISION entry is then
	// edited, which is what stamps its updated_at: an edit is the only way that
	// column is ever written, so seeding it through the edit path is the only
	// faithful way to produce an edited comment.
	addTaskCommentTo(t, database, f.loggedTaskID, models.CommentFinding, bodyFinding, createdFinding)
	addTaskCommentTo(t, database, f.loggedTaskID, models.CommentHypothesis, bodyHypothesis, createdHypothesis)
	decisionID := addTaskCommentTo(t, database, f.loggedTaskID, models.CommentDecision,
		bodyDecisionOriginal, createdDecision)
	editTaskCommentBody(t, database, decisionID, bodyDecisionEdited, updatedDecision)

	addTaskCommentTo(t, database, f.markupTaskID, models.CommentTest, bodyMarkup, createdMarkup)
	addTaskCommentTo(t, database, f.looseTaskID, models.CommentNote, bodyLoose, createdLoose)

	// The sprint's own progression log; the DECISION entry is edited too.
	addSprintCommentTo(t, database, f.sprintID, models.CommentProgress,
		bodySprintProgress, createdSprintProgress)
	sprintDecisionID := addSprintCommentTo(t, database, f.sprintID, models.CommentDecision,
		bodySprintDecisionOriginal, createdSprintDecision)
	editSprintCommentBody(t, database, sprintDecisionID, bodySprintDecisionEdited, updatedSprintDecision)

	return f
}

// addTaskCommentTo inserts one task comment through the production write path and
// returns its id.
func addTaskCommentTo(t *testing.T, database *db.DB, taskID int,
	commentType models.CommentType, body, createdAt string) int {
	t.Helper()

	var id int
	err := database.WithTransaction(func(tx *sql.Tx) error {
		newID, ierr := db.InsertTaskCommentTx(tx, &models.TaskComment{
			TaskID:    taskID,
			Type:      commentType,
			Body:      body,
			CreatedAt: createdAt,
		})
		id = newID
		return ierr
	})
	if err != nil {
		t.Fatalf("inserting %s comment on task %d: %v", commentType, taskID, err)
	}
	return id
}

// addSprintCommentTo inserts one sprint comment through the production write path
// and returns its id.
func addSprintCommentTo(t *testing.T, database *db.DB, sprintID int,
	commentType models.CommentType, body, createdAt string) int {
	t.Helper()

	var id int
	err := database.WithTransaction(func(tx *sql.Tx) error {
		newID, ierr := db.InsertSprintCommentTx(tx, &models.SprintComment{
			SprintID:  sprintID,
			Type:      commentType,
			Body:      body,
			CreatedAt: createdAt,
		})
		id = newID
		return ierr
	})
	if err != nil {
		t.Fatalf("inserting %s comment on sprint %d: %v", commentType, sprintID, err)
	}
	return id
}

// editTaskCommentBody rewrites a task comment's body and stamps updated_at, the
// only path on which that column is ever written.
func editTaskCommentBody(t *testing.T, database *db.DB, id int, body, updatedAt string) {
	t.Helper()

	if err := database.WithTransaction(func(tx *sql.Tx) error {
		return db.UpdateTaskCommentTx(tx, id, &db.CommentUpdate{Body: &body}, updatedAt)
	}); err != nil {
		t.Fatalf("editing task comment %d: %v", id, err)
	}
}

// editSprintCommentBody rewrites a sprint comment's body and stamps updated_at.
func editSprintCommentBody(t *testing.T, database *db.DB, id int, body, updatedAt string) {
	t.Helper()

	if err := database.WithTransaction(func(tx *sql.Tx) error {
		return db.UpdateSprintCommentTx(tx, id, &db.CommentUpdate{Body: &body}, updatedAt)
	}); err != nil {
		t.Fatalf("editing sprint comment %d: %v", id, err)
	}
}

// modalSlice returns the substring of body covering exactly ONE task's detail
// modal: from that modal's id attribute to the next modal's, or to the end of the
// document for the last one. Every page renders its modals consecutively after
// the page body, so this isolates one task's markup and an assertion about it can
// never accidentally read another task's modal.
func modalSlice(t *testing.T, body string, taskID int) string {
	t.Helper()

	// The opening tag of the modal, not the bare id: the modal's title element
	// carries id="task-modal-<id>-title", so matching the whole opening tag is what
	// keeps the region from being cut short at the modal header.
	marker := modalOpenTag + ` id="task-modal-` + itoa(taskID) + `"`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("no detail modal for task #%d in the rendered page", taskID)
	}
	rest := body[start+len(marker):]
	if next := strings.Index(rest, modalOpenTag); next >= 0 {
		return rest[:next]
	}
	return rest
}

// modalOpenTag is the opening tag every task detail modal starts with.
const modalOpenTag = `<div class="modal modal-blur fade"`

// modalCommentsSlice returns the comments block of one task's detail modal: from
// the block's own title to the modal footer, which follows the modal body. It is
// the region Acceptance Criteria 64, 65, 67, 71 and 72 speak about.
func modalCommentsSlice(t *testing.T, body string, taskID int) string {
	t.Helper()

	modal := modalSlice(t, body, taskID)
	start := strings.Index(modal, `<div class="datagrid-title mb-1">Comments</div>`)
	if start < 0 {
		t.Fatalf("task #%d detail modal has no comments block", taskID)
	}
	rest := modal[start:]
	if end := strings.Index(rest, `modal-footer`); end >= 0 {
		return rest[:end]
	}
	return rest
}

// sprintCommentsCardSlice returns the sprint page's Comments card: from its card
// header to the first task detail modal, which the page renders after the whole
// sprint detail block. It is the region Acceptance Criteria 68, 69 and 72 speak
// about, and bounding it before the modals is what makes "the card shows only the
// sprint's own comments" a falsifiable assertion.
func sprintCommentsCardSlice(t *testing.T, body string) string {
	t.Helper()

	start := strings.Index(body, `<h3 class="card-title">Comments`)
	if start < 0 {
		t.Fatalf("the sprint page renders no Comments card")
	}
	rest := body[start:]
	if end := strings.Index(rest, modalOpenTag); end >= 0 {
		return rest[:end]
	}
	return rest
}

// Markup fragments the timeline is asserted against verbatim, so a change to the
// Tabler structure fails the test instead of silently degrading the component.
const (
	timelineList  = `<ul class="timeline">`
	timelineEvent = `<li class="timeline-event">`
	timelineIcon  = `<div class="timeline-event-icon"><i class="ti ti-message"></i></div>`
	timelineCard  = `<div class="card timeline-event-card">`
	preWrapBlock  = `<div class="task-modal__text">`
)

// typeBadge renders the markup a comment type's badge must produce: the neutral
// variant, for every type value (Acceptance Criterion 66).
func typeBadge(commentType models.CommentType) string {
	return `<span class="badge bg-secondary-lt">` + string(commentType) + `</span>`
}

// rendered returns a seeded body as the page carries it. Every roadmap-derived
// value goes out through html/template's contextual auto-escaping, so an assertion
// that looks for a body containing an apostrophe, an ampersand, or a tag must look
// for the ESCAPED form — which is the point of Acceptance Criterion 73.
func rendered(text string) string { return html.EscapeString(text) }

// fetchTaskDetail requests one task's detail endpoint and returns the status and
// the raw body. It is the read the modal performs when a user opens a task.
func fetchTaskDetail(t *testing.T, mux *http.ServeMux, roadmap string, taskID int) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+roadmap+"/tasks/"+itoa(taskID)+"/data", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// decodeTaskDetail requests a task's detail endpoint and decodes the 200 body.
func decodeTaskDetail(t *testing.T, mux *http.ServeMux, roadmap string, taskID int) taskDetailView {
	t.Helper()

	status, body := fetchTaskDetail(t, mux, roadmap, taskID)
	if status != http.StatusOK {
		t.Fatalf("GET the detail of task #%d: status = %d, want 200; body=%q", taskID, status, body)
	}
	var view taskDetailView
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("decoding the detail of task #%d: %v; body=%q", taskID, err, body)
	}
	return view
}

// TestTaskDetail_CommentLog is the gate for Acceptance Criteria 64, 65 and 94 on
// the task surface: the detail a modal is filled from carries the task's comments
// oldest first, every one of them, each with its type, its created_at, its
// updated_at when it has one, and its body.
//
// The comments no longer travel inside the served page: the page carries one empty
// modal shell, and this endpoint is what a modal is filled from, so this is where
// the log's order and completeness are now measured. That the script renders them
// as a Tabler timeline, as text, is pinned in task_modal_test.go.
func TestTaskDetail_CommentLog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	view := decodeTaskDetail(t, mux, f.name, f.loggedTaskID)

	if view.Task.ID != f.loggedTaskID {
		t.Errorf("the endpoint returned task #%d, want #%d", view.Task.ID, f.loggedTaskID)
	}
	if len(view.Comments) != 3 {
		t.Fatalf("the logged task carries %d comments, want its whole log of 3", len(view.Comments))
	}

	// Oldest first, exactly the order `rmp task comment-list` returns and the order
	// the timeline presents.
	wantTypes := []models.CommentType{
		models.CommentFinding, models.CommentHypothesis, models.CommentDecision,
	}
	wantBodies := []string{bodyFinding, bodyHypothesis, bodyDecisionEdited}
	for i := range wantTypes {
		if view.Comments[i].Type != wantTypes[i] {
			t.Errorf("comment %d is a %s, want %s", i, view.Comments[i].Type, wantTypes[i])
		}
		if view.Comments[i].Body != wantBodies[i] {
			t.Errorf("comment %d body = %q, want %q", i, view.Comments[i].Body, wantBodies[i])
		}
		if view.Comments[i].CreatedAt == "" {
			t.Errorf("comment %d carries no created_at", i)
		}
	}
	for i := 1; i < len(view.Comments); i++ {
		if view.Comments[i-1].CreatedAt > view.Comments[i].CreatedAt {
			t.Errorf("the log is not oldest first: %q precedes %q",
				view.Comments[i-1].CreatedAt, view.Comments[i].CreatedAt)
		}
	}

	// Exactly one entry was edited, and only that one carries updated_at: an edit
	// is the only thing that writes that column.
	edited := 0
	for i := range view.Comments {
		if view.Comments[i].UpdatedAt != nil {
			edited++
			if view.Comments[i].Type != models.CommentDecision {
				t.Errorf("the %s entry carries updated_at; only the edited DECISION does",
					view.Comments[i].Type)
			}
		}
	}
	if edited != 1 {
		t.Errorf("%d entries carry updated_at, want exactly the 1 that was edited", edited)
	}

	// The page itself carries none of this: no comment body reaches the served
	// document, on either surface that shows a clickable task.
	for _, path := range []string{
		"/roadmaps/" + f.name + "/tasks",
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.sprintID),
	} {
		page := servePage(t, mux, path)
		for _, body := range wantBodies {
			if strings.Contains(page, body) || strings.Contains(page, rendered(body)) {
				t.Errorf("%s: a task comment body reached the served document: %q", path, body)
			}
		}
	}
}

// TestTaskDetail_CommentEmptyState is the gate for Acceptance Criterion 67 at the
// data layer: a task with no comment yields an EMPTY ARRAY, never null, so the
// script walks it unconditionally and renders its empty-state message.
func TestTaskDetail_CommentEmptyState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	status, body := fetchTaskDetail(t, mux, f.name, f.quietTaskID)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !strings.Contains(body, `"comments": []`) {
		t.Errorf("a task with no comment must serialise comments as [], never null; body=%q", body)
	}

	var view taskDetailView
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if view.Comments == nil {
		t.Error("the decoded comments are nil; the endpoint must send an empty array")
	}
	if len(view.Comments) != 0 {
		t.Errorf("the quiet task carries %d comments, want 0", len(view.Comments))
	}

	// The message the script shows in place of the timeline lives in the script,
	// where the empty state is now rendered.
	script := readEmbeddedAsset(t, "static/task-modal.js")
	if !strings.Contains(script, "No comments have been recorded on this task yet.") {
		t.Error("the modal script carries no comment empty-state message")
	}
}

// TestTaskDetail_CommentBodyTravelsAsAJSONString is the gate for Acceptance
// Criterion 73 under the new data path: a comment body is free text a user wrote,
// so a body containing markup must travel as a JSON string VALUE and must not
// reach the page as markup.
//
// Two properties, measured where each now lives: the served page carries no
// comment body at all, and the endpoint carries it as a string whose markup
// characters are JSON-escaped by the encoder. How the client writes it into the
// DOM — as text, never as markup — is pinned in task_modal_test.go.
func TestTaskDetail_CommentBodyTravelsAsAJSONString(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	page := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")
	for _, raw := range []string{"<script>alert(", "<b>bold</b>", bodyMarkup} {
		if strings.Contains(page, raw) {
			t.Errorf("a comment body reached the page as markup: found %q", raw)
		}
	}
	if strings.Contains(page, rendered(bodyMarkup)) {
		t.Errorf("a comment body reached the page at all; the modal is filled from the endpoint")
	}
	// The page's script elements are the three it loads, opened and closed once
	// each. A body that became markup would raise either count.
	if got := strings.Count(page, "<script"); got != 3 {
		t.Errorf("page has %d <script elements, want exactly 3 (the vendored bundle, the "+
			"modal script and the board's search script)", got)
	}
	if got := strings.Count(page, "</script>"); got != 3 {
		t.Errorf("page has %d </script> closers, want exactly 3", got)
	}

	// The endpoint carries the body as a JSON string. Go's encoder escapes the
	// HTML-significant characters, so the markup cannot terminate a script element
	// even if the payload were ever embedded in one.
	status, body := fetchTaskDetail(t, mux, f.name, f.markupTaskID)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if strings.Contains(body, "<script>") || strings.Contains(body, "</script>") {
		t.Errorf("the endpoint emitted an unescaped script tag in its JSON body: %q", body)
	}
	if !strings.Contains(body, `\u003c`) {
		t.Errorf("the endpoint did not JSON-escape the markup characters of the comment body: %q", body)
	}

	// And the value round-trips: the client receives exactly what the user wrote.
	view := decodeTaskDetail(t, mux, f.name, f.markupTaskID)
	if len(view.Comments) != 1 {
		t.Fatalf("the markup task carries %d comments, want 1", len(view.Comments))
	}
	if view.Comments[0].Body != bodyMarkup {
		t.Errorf("the comment body decoded to %q, want %q", view.Comments[0].Body, bodyMarkup)
	}
}

// TestSprintPage_CommentsCard is the gate for Acceptance Criterion 68: the sprint
// page renders a Comments card AFTER the member-tasks board, as the last card of
// the sprint detail sub-template, showing the sprint's own comments oldest first
// with a card header carrying the comment count.
//
// The placement anchor is the board, because that is what the Comments card now
// follows: the member-tasks table it used to follow no longer exists (SPEC/WEB.md
// § Sprint Detail Sub-Template, rule 2; Acceptance Criterion 130).
func TestSprintPage_CommentsCard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.sprintID))

	// Placement: after the member-tasks board, and last (only the task modal shell,
	// which is not part of the sub-template, follows it). The anchor is the LAST
	// column of the board rather than the board's opening tag, so a Comments card
	// rendered between two columns would fail this assertion too.
	boardAt := strings.Index(body, `data-role="task-board"`)
	lastColumnAt := strings.LastIndex(body, `data-role="task-board-column"`)
	commentsCardAt := strings.Index(body, `<h3 class="card-title">Comments <span`)
	if boardAt < 0 || lastColumnAt < 0 || commentsCardAt < 0 {
		t.Fatalf("sprint page is missing a region (board=%d last column=%d comments=%d)",
			boardAt, lastColumnAt, commentsCardAt)
	}
	if commentsCardAt < lastColumnAt {
		t.Errorf("the Comments card is rendered before the end of the member-tasks board")
	}
	// And the Sprint details card still precedes the board, so the three keep the
	// order the sub-template fixes.
	detailsCardAt := strings.Index(body, `<h3 class="card-title">Sprint details</h3>`)
	if detailsCardAt < 0 || detailsCardAt > boardAt {
		t.Errorf("the Sprint details card does not precede the member-tasks board "+
			"(details=%d board=%d)", detailsCardAt, boardAt)
	}

	// Header: the title and a badge with the number of comments.
	if !strings.Contains(body, `<h3 class="card-title">Comments <span class="badge bg-secondary-lt ms-2">2</span></h3>`) {
		t.Errorf("the Comments card header does not carry the comment-count badge for 2 comments")
	}

	card := sprintCommentsCardSlice(t, body)

	// The same timeline structure the modal uses, one event per comment.
	if !strings.Contains(card, timelineList) {
		t.Errorf("the Comments card renders no %s", timelineList)
	}
	if got := strings.Count(card, timelineEvent); got != 2 {
		t.Errorf("the Comments card has %d timeline events, want 2", got)
	}
	if got := strings.Count(card, timelineIcon); got != 2 {
		t.Errorf("the Comments card has %d timeline event icons, want 2", got)
	}
	if got := strings.Count(card, timelineCard); got != 2 {
		t.Errorf("the Comments card has %d timeline event cards, want 2", got)
	}

	// Oldest first, with the type badges, the timestamps, and the edited marker.
	progressAt := strings.Index(card, rendered(bodySprintProgress))
	decisionAt := strings.Index(card, rendered(bodySprintDecisionEdited))
	if progressAt < 0 || decisionAt < 0 {
		t.Fatalf("the Comments card is missing a seeded sprint comment (progress=%d decision=%d)",
			progressAt, decisionAt)
	}
	if progressAt > decisionAt {
		t.Errorf("the Comments card is not oldest first (progress=%d decision=%d)", progressAt, decisionAt)
	}
	for _, commentType := range []models.CommentType{models.CommentProgress, models.CommentDecision} {
		if !strings.Contains(card, typeBadge(commentType)) {
			t.Errorf("the Comments card is missing the neutral %s badge", commentType)
		}
	}
	if !strings.Contains(card, `<span class="text-secondary">`+createdSprintProgress+`</span>`) {
		t.Errorf("the Comments card does not show a comment's created_at timestamp")
	}
	if !strings.Contains(card, `<span class="text-secondary">edited `+updatedSprintDecision+`</span>`) {
		t.Errorf("the Comments card does not mark the edited comment with its updated_at")
	}
	if got := strings.Count(card, "edited "); got != 1 {
		t.Errorf("the Comments card shows %d edited markers, want exactly 1", got)
	}
	// The line breaks of the sprint's own comments are preserved the same way.
	if got := strings.Count(card, preWrapBlock); got != 2 {
		t.Errorf("%d sprint comment bodies use the pre-wrap block, want 2", got)
	}
	if !strings.Contains(card, "the next sprint: it needs the currency table refresh") {
		t.Errorf("the Comments card does not show the stored (edited) body of the edited comment")
	}
}

// TestSprintPage_CommentsCardEmptyState is the other half of Acceptance Criterion
// 68: a sprint with no comments STILL renders the card, showing an empty state in
// place of the timeline.
func TestSprintPage_CommentsCardEmptyState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.quietSprintID))

	if !strings.Contains(body, `<h3 class="card-title">Comments <span class="badge bg-secondary-lt ms-2">0</span></h3>`) {
		t.Errorf("a sprint with no comments does not render the Comments card with a zero count badge")
	}
	card := sprintCommentsCardSlice(t, body)
	if strings.Contains(card, timelineList) || strings.Contains(card, timelineEvent) {
		t.Errorf("a sprint with no comments renders a timeline instead of an empty state")
	}
	for _, marker := range []string{
		`<p class="empty-title">No comments</p>`,
		"Nothing has been recorded on this sprint yet.",
	} {
		if !strings.Contains(card, marker) {
			t.Errorf("the Comments card of a sprint with no comments is missing %q", marker)
		}
	}
}

// TestSprintPage_CommentsCardHoldsOnlySprintOwnComments is the gate for Acceptance
// Criterion 69: the card shows the comments of the sprint ITSELF. A comment written
// against a member task appears in that task's detail modal and nowhere in the card,
// and no aggregate of task comments is presented at sprint level.
func TestSprintPage_CommentsCardHoldsOnlySprintOwnComments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.sprintID))
	card := sprintCommentsCardSlice(t, body)

	// No member task's comment leaks into the sprint's card.
	for _, taskBody := range []string{rendered(bodyFinding), rendered(bodyHypothesis), rendered(bodyDecisionEdited), rendered(bodyMarkup)} {
		if strings.Contains(card, taskBody) {
			t.Errorf("the sprint Comments card shows a member task's comment: %q", taskBody)
		}
	}
	// Exactly the sprint's own two comments are in the card.
	if got := strings.Count(card, timelineEvent); got != 2 {
		t.Errorf("the sprint Comments card holds %d entries, want exactly the sprint's own 2", got)
	}

	// The member task's log is reachable, but from that task's own endpoint — the
	// modal is filled on demand — and it is the task's log, not the sprint's.
	view := decodeTaskDetail(t, mux, f.name, f.loggedTaskID)
	if len(view.Comments) != 3 || view.Comments[0].Body != bodyFinding {
		t.Errorf("the member task's own log is not served by its detail endpoint: %+v", view.Comments)
	}
	for _, sprintBody := range []string{bodySprintProgress, bodySprintDecisionEdited} {
		for i := range view.Comments {
			if view.Comments[i].Body == sprintBody {
				t.Errorf("a sprint comment leaked into a task's detail: %q", sprintBody)
			}
		}
	}
}

// TestTasksPage_CoversTasksOutsideAnySprint pins that the board's grouped count
// read covers EVERY task the page renders, not just those in a sprint: a task
// that belongs to no sprint shows its comment count on its card and serves its
// log from its own endpoint, and is absent from the sprint page altogether.
func TestTasksPage_CoversTasksOutsideAnySprint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	// The card of the sprint-less task carries its count, which the grouped
	// counting read supplied.
	tasksBody := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")
	card := cardSlice(t, boardColumns(t, tasksBody)[0], f.looseTaskID)
	if !strings.Contains(card, "Comments: 1") {
		t.Errorf("the card of the task in no sprint does not show its comment count\ncard: %s", card)
	}

	// And its log is served by its own detail endpoint.
	view := decodeTaskDetail(t, mux, f.name, f.looseTaskID)
	if len(view.Comments) != 1 {
		t.Fatalf("the detail of the task in no sprint carries %d comments, want 1", len(view.Comments))
	}
	if view.Comments[0].Body != bodyLoose {
		t.Errorf("the detail of the task in no sprint shows %q, want %q", view.Comments[0].Body, bodyLoose)
	}
	if view.Comments[0].Type != models.CommentNote {
		t.Errorf("the comment type is %s, want NOTE", view.Comments[0].Type)
	}

	// The sprint page renders only its member tasks, so that task has no trigger
	// there and its comment is nowhere on it.
	sprintBody := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.sprintID))
	if strings.Contains(sprintBody, `data-task-id="`+itoa(f.looseTaskID)+`"`) {
		t.Errorf("the sprint page offers a trigger for a task that is not a member of the sprint")
	}
	if strings.Contains(sprintBody, rendered(bodyLoose)) {
		t.Errorf("the sprint page shows the comment of a task that is not a member of the sprint")
	}
}

// TestSprintsLandingPage_RendersNoCommentLog pins that the sprints landing page is
// unchanged by this feature: it renders every sprint as a compact card, opens no
// task detail modal, and therefore shows no comment log of any kind — neither the
// sprint's own nor a member task's (SPEC/WEB.md § Shared Sprint-Card Partial).
func TestSprintsLandingPage_RendersNoCommentLog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name)

	for _, marker := range []string{
		timelineList, timelineEvent, timelineIcon,
		`<h3 class="card-title">Comments`, `id="task-modal-`,
	} {
		if strings.Contains(body, marker) {
			t.Errorf("the sprints landing page must not render %q", marker)
		}
	}
	for _, commentBody := range []string{
		rendered(bodyFinding), rendered(bodyHypothesis), rendered(bodyDecisionEdited), rendered(bodyMarkup),
		rendered(bodySprintProgress), rendered(bodySprintDecisionEdited),
	} {
		if strings.Contains(body, commentBody) {
			t.Errorf("the sprints landing page shows a comment body: %q", commentBody)
		}
	}
}

// TestCommentTypeBadge_NeutralForEveryType is the gate for Acceptance Criterion 66
// at the helper level: the comment-type badge is the neutral bg-secondary-lt variant
// for every one of the seven type values, and the semantic mapping for status,
// priority and severity is not extended to comment types.
func TestCommentTypeBadge_NeutralForEveryType(t *testing.T) {
	seen := make(map[models.CommentType]bool, len(models.ValidTaskCommentTypes))
	for _, commentType := range models.ValidTaskCommentTypes {
		seen[commentType] = true
		if got := commentTypeBadge(commentType); got != badgeSecondary {
			t.Errorf("commentTypeBadge(%s) = %q, want the neutral %q", commentType, got, badgeSecondary)
		}
	}
	if len(models.ValidTaskCommentTypes) != 7 {
		t.Errorf("the task comment type set has %d values, want 7", len(models.ValidTaskCommentTypes))
	}

	// The four values a sprint comment accepts are a subset of the seven, and each
	// renders through the same neutral badge on the sprint surface.
	for _, commentType := range models.ValidSprintCommentTypes {
		if !seen[commentType] {
			t.Errorf("sprint comment type %s is outside the task type set", commentType)
		}
		if !models.IsValidSprintCommentType(string(commentType)) {
			t.Errorf("models.IsValidSprintCommentType rejects %s, which is in ValidSprintCommentTypes", commentType)
		}
		if got := commentTypeBadge(commentType); got != badgeSecondary {
			t.Errorf("commentTypeBadge(%s) = %q, want the neutral %q", commentType, got, badgeSecondary)
		}
	}

	// The helper is total: a value outside the enum still yields a usable class, so
	// a badge can never render with an empty class attribute.
	if got := commentTypeBadge(models.CommentType("SOMETHING_ELSE")); got != badgeSecondary {
		t.Errorf("commentTypeBadge on an out-of-enum value = %q, want %q", got, badgeSecondary)
	}
}

// ==================== ONE GROUPED COMMENT QUERY, NEVER N+1 ====================

// countingSource wraps a REAL read-only roadmap connection and counts, per read,
// the queries a page's read path issues. It is the instrument Acceptance Criterion
// 70 asks for at the page level: the count must be 1 for the task comments of a
// page with N clickable tasks (plus 1 for the sprint's own comments on the sprint
// page), independent of N, and 0 for a page that renders no task.
//
// Counting happens on the page's read surface (tasksSource / sprintSource), and
// each counted read is one statement: the driver-level statement counter in
// internal/db/comments_stmtcount_test.go measures that the grouped read issues
// exactly ONE statement for 1, 3 and 12 parents and none for an empty id set, and
// that the single-parent listing is one statement too. Composing the two
// measurements gives the statement count of a page render without either test
// having to assume what the other proves.
//
// perTaskComments counts the read no page path may take: the per-task comment
// listing. Since the grouped listing was removed, this is the ONLY read that can
// bring a comment BODY onto a page path, so a zero here carries two guarantees at
// once — no N+1, and no page reads comment text. It is unreachable through the
// narrow interfaces the loaders are handed (taskCommentCounter does not carry it),
// so the counter is a falsifiable guard that the seam still holds if that
// interface is ever widened; the sprint-page and endpoint tests exercise it
// directly to prove it counts.
// groupedTaskSprints counts the tasks page's third read, the grouped sprint
// resolution, and lastSprintIDs records the id set it was given, so Acceptance
// Criterion 92 is measured on the same instrument as Criterion 70: one query for
// the whole set of rendered task ids, and none for a page that renders no task.
//
// sprintListings counts the sprints page's ONLY read, the sprint listing, and
// sprintTasks counts the per-sprint member-task read that page must never take:
// it renders every sprint as a card with no member tasks on it, and the footer
// count it shows is carried by the sprint record the listing already returned
// (SPEC/WEB.md § Tasks and Sprints from SQLite). The member read is unreachable
// through the sprintsSource interface, so sprintTasks is the falsifiable guard
// that the seam still holds if that interface is ever widened — the sprints-page
// read-cost test exercises it directly to prove it counts.
type countingSource struct {
	*db.DB
	lastGroupedIDs       []int
	lastSprintIDs        []int
	groupedCommentCounts int
	groupedTaskSprints   int
	perTaskComments      int
	sprintComments       int
	sprintListings       int
	taskList             int
	boundedTaskList      int
	sprintTasks          int
}

// ListSprints is the read the sprints page performs: the roadmap's sprints, with
// the membership of every one of them resolved in the same bounded number of
// statements whatever the number of sprints (SPEC/COMMANDS.md § List Sprints).
func (c *countingSource) ListSprints(ctx context.Context,
	status *models.SprintStatus) ([]models.Sprint, error) {
	c.sprintListings++
	return c.DB.ListSprints(ctx, status)
}

// ListAllTasks is the read the board performs: unbounded, every task of the
// roadmap.
func (c *countingSource) ListAllTasks(ctx context.Context) ([]models.Task, error) {
	c.taskList++
	return c.DB.ListAllTasks(ctx)
}

// ListTasks is the CLI's bounded listing, which the board must NOT use: its limit
// is capped at models.MaxTaskLimit, so a roadmap with more tasks than that would
// lose cards while the column headers still presented their counts as facts. It is
// unreachable through the tasksSource interface; counting it here keeps that seam
// falsifiable if the interface is ever widened.
func (c *countingSource) ListTasks(ctx context.Context, filter *db.TaskListFilter) ([]models.Task, error) {
	c.boundedTaskList++
	return c.DB.ListTasks(ctx, filter)
}

func (c *countingSource) GetSprintTasksFull(ctx context.Context, sprintID int,
	status *models.TaskStatus, orderByPriority bool) ([]models.Task, error) {
	c.sprintTasks++
	return c.DB.GetSprintTasksFull(ctx, sprintID, status, orderByPriority)
}

// CountTaskCommentsByTasks is the comment read a page performs: the count, never
// the bodies.
func (c *countingSource) CountTaskCommentsByTasks(ctx context.Context,
	taskIDs []int) (map[int]int, error) {
	c.groupedCommentCounts++
	c.lastGroupedIDs = append([]int(nil), taskIDs...)
	return c.DB.CountTaskCommentsByTasks(ctx, taskIDs)
}

func (c *countingSource) ListTaskComments(ctx context.Context, taskID int,
	commentType *models.CommentType) ([]models.TaskComment, error) {
	c.perTaskComments++
	return c.DB.ListTaskComments(ctx, taskID, commentType)
}

func (c *countingSource) GetSprintsByTasks(ctx context.Context,
	taskIDs []int) (map[int]db.SprintRef, error) {
	c.groupedTaskSprints++
	c.lastSprintIDs = append([]int(nil), taskIDs...)
	return c.DB.GetSprintsByTasks(ctx, taskIDs)
}

func (c *countingSource) ListSprintComments(ctx context.Context, sprintID int,
	commentType *models.CommentType) ([]models.SprintComment, error) {
	c.sprintComments++
	return c.DB.ListSprintComments(ctx, sprintID, commentType)
}

// openCounting opens the roadmap read-only, exactly as the handlers do, and wraps
// it so the reads a page performs can be counted. The connection is real: the
// queries run against the seeded SQLite file.
func openCounting(t *testing.T, name string) *countingSource {
	t.Helper()

	database, err := db.OpenReadOnly(name)
	if err != nil {
		t.Fatalf("opening roadmap %q read-only: %v", name, err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return &countingSource{DB: database}
}

// seedTasksWithComments creates a roadmap holding n tasks, each with two comments,
// and returns the task ids in creation order. n may be 0: the roadmap then exists
// with no task at all, which is the case Acceptance Criterion 70 requires to issue
// no task-comment query.
func seedTasksWithComments(t *testing.T, name string, n int) []int {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	const now = "2026-08-10T08:00:00Z"
	ids := make([]int, 0, n)
	for i := 0; i < n; i++ {
		id, terr := seedTask(database, seededTask(now, "Balance settlement window "+itoa(i+1)))
		if terr != nil {
			t.Fatalf("creating task %d: %v", i+1, terr)
		}
		addTaskCommentTo(t, database, id, models.CommentFinding,
			"Window "+itoa(i+1)+" balances to the cent after the rounding fix.", createdFinding)
		addTaskCommentTo(t, database, id, models.CommentProgress,
			"Window "+itoa(i+1)+" now reports its residual in the reconciliation report.", createdDecision)
		ids = append(ids, id)
	}
	return ids
}

// TestTasksPage_OneGroupedCommentCountQueryIndependentOfTaskCount is the gate for
// Acceptance Criterion 70 on the tasks page: rendering a page with N clickable
// tasks issues exactly ONE comment query for all N tasks — a COUNT over the whole
// set of rendered task ids, never a listing of their bodies — for every N, and
// none at all when the page renders no task.
//
// The board shows a number on each card. Reading the text of every comment of
// every task in order to display a number is work the page throws away, so the
// grouped LISTING must not be issued here at all; a task's comment text is read
// only when a user opens that task's modal, by the task detail endpoint
// (SPEC/DATABASE.md § Count Comments for Many Parents (Grouped)).
func TestTasksPage_OneGroupedCommentCountQueryIndependentOfTaskCount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// The same parent counts the driver-level statement counter measures for the
	// grouped read itself, so the two measurements line up.
	for _, taskCount := range []int{1, 3, 12} {
		name := "settlement-window-" + itoa(taskCount)
		ids := seedTasksWithComments(t, name, taskCount)
		src := openCounting(t, name)

		data, err := readTasks(context.Background(), src, name, boardControls{})
		if err != nil {
			t.Fatalf("%d tasks: readTasks: %v", taskCount, err)
		}

		if src.groupedCommentCounts != 1 {
			t.Errorf("%d tasks: the page issued %d comment-count queries, want exactly 1",
				taskCount, src.groupedCommentCounts)
		}
		if src.perTaskComments != 0 {
			t.Errorf("%d tasks: the page issued %d per-task comment queries, want 0 (that is the N+1 pattern)",
				taskCount, src.perTaskComments)
		}
		if src.taskList != 1 {
			t.Errorf("%d tasks: the page issued %d task-list queries, want 1", taskCount, src.taskList)
		}
		if src.boundedTaskList != 0 {
			t.Errorf("%d tasks: the page issued %d BOUNDED task-list queries, want 0: the board "+
				"reads every task", taskCount, src.boundedTaskList)
		}

		// The single query covered EVERY rendered task, which is what makes one query
		// sufficient rather than merely few.
		if len(src.lastGroupedIDs) != taskCount {
			t.Errorf("%d tasks: the grouped read was given %d ids, want %d",
				taskCount, len(src.lastGroupedIDs), taskCount)
		}
		for i := range ids {
			if i < len(src.lastGroupedIDs) && src.lastGroupedIDs[i] != ids[i] {
				t.Errorf("%d tasks: the grouped read id at %d is #%d, want #%d",
					taskCount, i, src.lastGroupedIDs[i], ids[i])
			}
		}

		// And every rendered task really did receive its count from that one query —
		// the number its card shows, with no comment body anywhere in the view.
		if len(data.Tasks) != taskCount {
			t.Fatalf("%d tasks: the page carries %d tasks, want %d", taskCount, len(data.Tasks), taskCount)
		}
		for i := range data.Tasks {
			if data.Tasks[i].CommentCount != 2 {
				t.Errorf("%d tasks: task #%d carries the comment count %d, want 2",
					taskCount, data.Tasks[i].ID, data.Tasks[i].CommentCount)
			}
		}

		// The control that makes the assertion above falsifiable: the alternative the
		// SPEC forbids — one listing per task — measured on the same instrument. An
		// instrument that always read 1 would pass the assertion for the wrong reason.
		for _, id := range ids {
			if _, lerr := src.ListTaskComments(context.Background(), id, nil); lerr != nil {
				t.Fatalf("%d tasks: per-task control read of task #%d: %v", taskCount, id, lerr)
			}
		}
		if src.perTaskComments != taskCount {
			t.Errorf("%d tasks: the per-task control issued %d reads, want %d; "+
				"the instrument does not track reads one-for-one",
				taskCount, src.perTaskComments, taskCount)
		}
	}

	// A page that renders no task issues no task-comment query at all.
	const emptyName = "settlement-window-none"
	seedTasksWithComments(t, emptyName, 0)
	emptySrc := openCounting(t, emptyName)

	emptyData, err := readTasks(context.Background(), emptySrc, emptyName, boardControls{})
	if err != nil {
		t.Fatalf("empty roadmap: readTasks: %v", err)
	}
	if len(emptyData.Tasks) != 0 {
		t.Fatalf("empty roadmap: the page carries %d tasks, want 0", len(emptyData.Tasks))
	}
	if emptySrc.groupedCommentCounts != 0 {
		t.Errorf("a page with no task issued %d comment-count queries, want 0", emptySrc.groupedCommentCounts)
	}
	if emptySrc.perTaskComments != 0 {
		t.Errorf("a page with no task issued %d per-task comment queries, want 0", emptySrc.perTaskComments)
	}
}

// TestSprintPage_CommentQueryCount is the gate for Acceptance Criterion 70 on the
// sprint page: TWO comment queries, whatever the number of member tasks — the
// sprint's own listing, which the Comments card renders in full as a log, and ONE
// grouped COUNT over the whole set of rendered member-task ids, which is what gives
// each board card its comment number.
//
// The page reads no comment BODY for a task it renders: a card shows a number, and
// the text of a member task's comments is fetched only when a user opens that
// task's modal, one task at a time, by the task detail endpoint. That is what the
// zero on the per-task listing states (SPEC/WEB.md § Tasks and Sprints from SQLite;
// § Sprint Detail Sub-Template, Read cost; Acceptance Criterion 137).
func TestSprintPage_CommentQueryCount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	src := openCounting(t, f.name)

	data, err := readSprint(context.Background(), src, f.name, f.sprintID)
	if err != nil {
		t.Fatalf("readSprint: %v", err)
	}

	// Three member tasks were seeded, and together they cost ONE comment count.
	if len(data.Tasks) != 3 {
		t.Fatalf("the sprint page carries %d member tasks, want 3", len(data.Tasks))
	}
	if src.groupedCommentCounts != 1 {
		t.Errorf("the sprint page issued %d task-comment-count queries, want exactly 1: one "+
			"grouped count over the whole set of rendered member-task ids",
			src.groupedCommentCounts)
	}
	// That one query covered EVERY rendered card, which is what makes one query
	// sufficient rather than merely few.
	if len(src.lastGroupedIDs) != len(data.Tasks) {
		t.Errorf("the comment count was given %d ids, want the board's %d member tasks",
			len(src.lastGroupedIDs), len(data.Tasks))
	}
	for i := range data.Tasks {
		if i < len(src.lastGroupedIDs) && src.lastGroupedIDs[i] != data.Tasks[i].ID {
			t.Errorf("the comment-count id at %d is #%d, want #%d",
				i, src.lastGroupedIDs[i], data.Tasks[i].ID)
		}
	}
	if src.perTaskComments != 0 {
		t.Errorf("the sprint page issued %d per-task comment queries, want 0: a card shows a "+
			"count and a member task's comment TEXT is read only through its own modal",
			src.perTaskComments)
	}
	if src.sprintComments != 1 {
		t.Errorf("the sprint page issued %d sprint-comment queries, want exactly 1", src.sprintComments)
	}
	if src.sprintTasks != 1 {
		t.Errorf("the sprint page issued %d member-task queries, want 1", src.sprintTasks)
	}
	// The tasks page's grouped sprint read must not leak into this page: the
	// sprint page renders one known sprint, so resolving the sprint of its member
	// tasks would be a query for a value its markup never shows (SPEC/WEB.md
	// § Roadmap Tasks Page, the sprint indicator).
	if src.groupedTaskSprints != 0 {
		t.Errorf("the sprint page issued %d sprint-resolution queries, want 0", src.groupedTaskSprints)
	}

	// The sprint's own log is what the page reads and renders.
	if len(data.Comments) != 2 {
		t.Errorf("the sprint carries %d comments of its own, want 2", len(data.Comments))
	}

	// The control that makes the count and the zero falsifiable: both reads are
	// reachable on this same instrument and both are counted when taken, so "1" is
	// a measurement rather than an instrument that never moves.
	if _, err := src.CountTaskCommentsByTasks(context.Background(),
		[]int{f.loggedTaskID, f.markupTaskID}); err != nil {
		t.Fatalf("control count read: %v", err)
	}
	if _, err := src.ListTaskComments(context.Background(), f.loggedTaskID, nil); err != nil {
		t.Fatalf("control listing read: %v", err)
	}
	if src.groupedCommentCounts != 2 || src.perTaskComments != 1 {
		t.Errorf("after the control reads the instrument registers %d counts and %d listings, "+
			"want 2 and 1; it does not track reads one-for-one",
			src.groupedCommentCounts, src.perTaskComments)
	}
}

// ==================== READ-ONLY: NO ROUTE, NO ENDPOINT, NO WRITE PATH ====================

// TestCommentSurface_HasNoWriteAffordance is the gate for Acceptance Criterion 72
// on the markup: the sprint Comments card contains no form, no input, no button
// and no link — nothing through which a comment could be created, edited or
// deleted from the browser — and the script that fills the task modal builds no
// such control either.
//
// The card is asserted as a region rather than the whole page, because the page
// legitimately carries controls that submit nothing (the modal's Close button, the
// sidebar links): a page-wide assertion would either fail on those or have to be
// weakened until it proved nothing. The task modal's own timeline is no longer
// server-rendered, so its read-only property is asserted on the script that builds
// it.
func TestCommentSurface_HasNoWriteAffordance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	sprintBody := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.sprintID))

	// Anything that could carry a change to the server, plus the attributes that
	// would make an element do so.
	forbidden := []string{
		"<form", "<input", "<button", "<textarea", "<select", "<a ",
		"href=", "action=", "formaction=", "method=", "onclick=", "onsubmit=",
		"data-bs-toggle=", "contenteditable",
	}
	card := strings.ToLower(sprintCommentsCardSlice(t, sprintBody))
	for _, bad := range forbidden {
		if strings.Contains(card, bad) {
			t.Errorf("the sprint Comments card must be read-only but contains %q", bad)
		}
	}

	// The modal script creates only presentational elements, and reaches the
	// server only through a read: no form, no control, and no non-GET request.
	script := readEmbeddedAsset(t, "static/task-modal.js")
	for _, bad := range []string{
		`createElement("form")`, `createElement("input")`, `createElement("button")`,
		`createElement("a")`, `createElement("textarea")`, `createElement("select")`,
		"method:", "POST", "PUT", "PATCH", "DELETE", "FormData", "XMLHttpRequest",
	} {
		if strings.Contains(script, bad) {
			t.Errorf("the modal script must be read-only but contains %q", bad)
		}
	}
}

// wantRoutePatterns is the complete, pinned set of patterns the read-only mux
// registers. Every roadmap page is registered for GET and for HEAD only; the bare
// "/" is the catch-all that answers 404 for an unknown read and 405 for any other
// method. The comment log added none of them: it is rendered into pages that already
// existed (Acceptance Criterion 72).
var wantRoutePatterns = []string{
	"/",
	"GET /roadmaps/{name}",
	"GET /roadmaps/{name}/audit",
	"GET /roadmaps/{name}/graph",
	"GET /roadmaps/{name}/graph/data",
	"GET /roadmaps/{name}/sprints/{id}",
	"GET /roadmaps/{name}/tasks",
	"GET /roadmaps/{name}/tasks/{id}/data",
	"GET /static/",
	"GET /{$}",
	"HEAD /roadmaps/{name}",
	"HEAD /roadmaps/{name}/audit",
	"HEAD /roadmaps/{name}/graph",
	"HEAD /roadmaps/{name}/graph/data",
	"HEAD /roadmaps/{name}/sprints/{id}",
	"HEAD /roadmaps/{name}/tasks",
	"HEAD /roadmaps/{name}/tasks/{id}/data",
	"HEAD /static/",
	"HEAD /{$}",
}

// TestRoutes_RegisteredSetIsGetHeadOnly is the gate for Acceptance Criterion 72 on
// the server surface: the set of registered routes is exactly wantRoutePatterns, so
// no route, handler or endpoint was added, and every pattern but the catch-all is
// bound to GET or HEAD — there is no method through which the interface could accept
// a change.
//
// The set is read from the registration source itself (routes.go, parsed as Go),
// because http.ServeMux exposes no way to enumerate what was registered on it. A new
// route therefore fails this test the moment it is registered, whether or not any
// test drives it.
func TestRoutes_RegisteredSetIsGetHeadOnly(t *testing.T) {
	got := registeredRoutePatterns(t)

	slices.Sort(got)
	want := append([]string(nil), wantRoutePatterns...)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("the registered route set changed:\n got: %v\nwant: %v", got, want)
	}

	for _, pattern := range got {
		if pattern == "/" {
			continue // the catch-all, which answers 404/405 and serves nothing
		}
		if !strings.HasPrefix(pattern, "GET ") && !strings.HasPrefix(pattern, "HEAD ") {
			t.Errorf("route %q is not restricted to GET or HEAD", pattern)
		}
	}
}

// registeredRoutePatterns parses routes.go and returns the pattern of every
// mux.Handle / mux.HandleFunc registration in it. A registration whose pattern is
// not a string literal fails the test: it would make the route set unreadable, which
// is exactly what this gate exists to prevent.
func registeredRoutePatterns(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "routes.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing routes.go: %v", err)
	}

	patterns := make([]string, 0, len(wantRoutePatterns))
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			t.Errorf("route registered at %s with a non-literal pattern; the route set must stay readable",
				fset.Position(call.Pos()))
			return true
		}
		pattern, uerr := strconv.Unquote(lit.Value)
		if uerr != nil {
			t.Errorf("route pattern %s at %s is not a valid string literal: %v",
				lit.Value, fset.Position(call.Pos()), uerr)
			return true
		}
		patterns = append(patterns, pattern)
		return true
	})
	return patterns
}

// TestCommentPages_AnswerReadMethodsOnly drives the pinned route set: every page
// that now carries a comment log answers GET and HEAD, and answers 405 to every
// method that could carry a change. It is the runtime half of Acceptance Criterion
// 72 — the parsed route set above is the structural half.
func TestCommentPages_AnswerReadMethodsOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	paths := []string{
		"/roadmaps/" + f.name,
		"/roadmaps/" + f.name + "/tasks",
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.sprintID),
	}
	for _, path := range paths {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			req := httptest.NewRequest(method, path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("%s %s: status = %d, want 200", method, path, rec.Code)
			}
		}
		for _, method := range []string{
			http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		} {
			req := httptest.NewRequest(method, path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status = %d, want 405 (the CLI is the sole write path)",
					method, path, rec.Code)
			}
		}
	}
}

// TestWebPackage_ReferencesNoCommentWriteAPI is the compile-surface half of
// Acceptance Criterion 72: the web package names none of the comment mutations, so
// there is no code path — reachable or not — through which the interface could
// create, edit or delete a comment. The CLI remains the sole write path.
func TestWebPackage_ReferencesNoCommentWriteAPI(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("listing the web package directory: %v", err)
	}

	mutations := []string{
		"InsertTaskCommentTx", "UpdateTaskCommentTx", "DeleteTaskCommentTx",
		"InsertSprintCommentTx", "UpdateSprintCommentTx", "DeleteSprintCommentTx",
	}
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, rerr := os.ReadFile(name) // #nosec G304 -- fixed package directory listing, test-only
		if rerr != nil {
			t.Fatalf("reading %s: %v", name, rerr)
		}
		checked++
		for _, mutation := range mutations {
			if strings.Contains(string(source), mutation) {
				t.Errorf("%s references the comment mutation %s: the web interface must never write a comment",
					name, mutation)
			}
		}
	}
	if checked == 0 {
		t.Fatalf("no non-test source file was scanned; the assertion would be vacuous")
	}
}

// ==================== VENDORED ASSETS, OFFLINE, NO INLINE STYLE ====================

// TestCommentTimeline_ClassesComeFromVendoredCSS is the gate for Acceptance
// Criterion 71: the timeline uses only the Tabler Timeline classes already present
// in the vendored stylesheet, and the feature adds no CSS file, no JavaScript file
// and no vendored asset.
//
// Every class the rendered timeline uses is looked up in the embedded stylesheets,
// so a class that exists nowhere — an invented one that would need new CSS — fails
// the test.
func TestCommentTimeline_ClassesComeFromVendoredCSS(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	// The four Timeline classes the SPEC names are in the vendored Tabler CSS.
	tabler := readEmbeddedAsset(t, "static/vendor/tabler/tabler.min.css")
	for _, class := range []string{
		".timeline{", ".timeline-event{", ".timeline-event-icon{", ".timeline-event-card{",
	} {
		if !strings.Contains(tabler, class) {
			t.Errorf("the vendored tabler.min.css does not define %s; the timeline cannot rely on it", class)
		}
	}

	// Every class the rendered timeline actually uses is defined in one of the
	// embedded stylesheets — nothing was invented and nothing new was needed.
	styles := tabler +
		readEmbeddedAsset(t, "static/vendor/tabler-icons/tabler-icons.min.css") +
		readEmbeddedAsset(t, "static/style.css")

	body := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.sprintID))

	// The sprint Comments card is server-rendered and scanned as markup.
	for _, class := range classTokens(sprintCommentsCardSlice(t, body)) {
		if !strings.Contains(styles, "."+class) {
			t.Errorf("the sprint Comments card uses the class %q, which no embedded stylesheet "+
				"defines; the feature must add no CSS", class)
		}
	}

	// The task modal's timeline is built by the script, so its classes never appear
	// in served markup and the fidelity guard cannot see them. They are scanned at
	// their source instead: every class name the script assigns must resolve to a
	// rule in the same embedded stylesheets (SPEC/WEB.md § UI Framework, rule 10).
	for _, class := range scriptClassTokens(t, readEmbeddedAsset(t, "static/task-modal.js")) {
		// The same documented structural hooks the markup guard allows: Tabler
		// component skeletons that carry no rule of their own
		// (tabler_fidelity_test.go, structuralHookClasses).
		if _, hook := structuralHookClasses[class]; hook {
			continue
		}
		if !strings.Contains(styles, "."+class) {
			t.Errorf("the modal script builds an element with the class %q, which no embedded "+
				"stylesheet defines and which is not a recorded structural hook", class)
		}
	}
}

// reScriptClass captures the class names the modal script assigns: the second
// argument of its el(tag, className, text) helper, and any className assignment.
var reScriptClass = regexp.MustCompile(`(?:el\("[a-z]+", "([^"]*)"|className = "([^"]*)")`)

// scriptClassTokens returns every class name the script assigns to an element.
// The concatenated forms (a base class plus a badge variable) are split on the
// quote boundary by the pattern itself, so only literal class names are returned.
func scriptClassTokens(t *testing.T, script string) []string {
	t.Helper()

	seen := map[string]bool{}
	tokens := make([]string, 0, 16)
	for _, m := range reScriptClass.FindAllStringSubmatch(script, -1) {
		for _, group := range m[1:] {
			for _, class := range strings.Fields(group) {
				if class == "" || seen[class] {
					continue
				}
				seen[class] = true
				tokens = append(tokens, class)
			}
		}
	}
	// Falsifiability control: a pattern that matched nothing would make the
	// assertion above vacuous. The script builds well over a dozen elements.
	if len(tokens) < 10 {
		t.Fatalf("extracted only %d class tokens from the modal script; the extraction is broken",
			len(tokens))
	}
	return tokens
}

// classAttrRe captures the value of every class attribute in a markup region.
var classAttrRe = regexp.MustCompile(`class="([^"]*)"`)

// classTokens returns the deduplicated class names used in a markup region.
func classTokens(region string) []string {
	seen := make(map[string]bool)
	tokens := make([]string, 0, 16)
	for _, match := range classAttrRe.FindAllStringSubmatch(region, -1) {
		for _, token := range strings.Fields(match[1]) {
			if !seen[token] {
				seen[token] = true
				tokens = append(tokens, token)
			}
		}
	}
	return tokens
}

// readEmbeddedAsset returns an embedded static asset as text, failing the test if
// it is absent from the binary.
func readEmbeddedAsset(t *testing.T, path string) string {
	t.Helper()

	data, err := staticFS.ReadFile(path)
	if err != nil {
		t.Fatalf("embedded asset %q is missing: %v", path, err)
	}
	return string(data)
}

// TestCommentPages_RenderOfflineWithoutInlineStyle is the gate for Acceptance
// Criteria 62 and 71 on the comment-bearing pages, and for the offline guarantee:
// the pages that render a comment log carry no inline style attribute, reference no
// remote origin, and load exactly the embedded asset chain — so they render with the
// machine disconnected.
//
// The existing no-inline-style and no-remote-origin gates cover a roadmap whose
// tasks have no comments; this one covers the populated timeline, the empty state,
// and the sprint Comments card in both of its branches.
func TestCommentPages_RenderOfflineWithoutInlineStyle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	paths := []string{
		"/roadmaps/" + f.name + "/tasks",
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.sprintID),      // comments present
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.quietSprintID), // empty state
	}
	for _, path := range paths {
		body := servePage(t, mux, path)

		assertNoInlineStyle(t, path, body)

		if loc := remoteOriginRe.FindString(body); loc != "" {
			t.Errorf("page %s references a remote-origin asset (%q); every asset is served from /static/",
				path, loc)
		}
		for _, bad := range []string{"cdn.", "fonts.googleapis", "fonts.gstatic", "unpkg", "jsdelivr", "cdnjs"} {
			if strings.Contains(strings.ToLower(body), bad) {
				t.Errorf("page %s references the banned remote origin %q", path, bad)
			}
		}
		// The asset chain is exactly the embedded one: four stylesheets and one
		// script, all from /static/. The comment log added none of them.
		for _, asset := range []string{
			`<link rel="stylesheet" href="/static/vendor/inter/inter.css">`,
			`<link rel="stylesheet" href="/static/vendor/tabler/tabler.min.css">`,
			`<link rel="stylesheet" href="/static/vendor/tabler-icons/tabler-icons.min.css">`,
			`<link rel="stylesheet" href="/static/style.css">`,
			`<script src="/static/vendor/tabler/tabler.min.js"></script>`,
		} {
			if !strings.Contains(body, asset) {
				t.Errorf("page %s is missing the embedded asset %s", path, asset)
			}
		}
		if got := strings.Count(body, "<link rel=\"stylesheet\""); got != 4 {
			t.Errorf("page %s loads %d stylesheets, want the 4 embedded ones", path, got)
		}
	}
}

// TestCommentPages_RenderOnAMigratedLegacyRoadmap pins the posture the read path
// depends on: OpenReadOnly never migrates, so the comment tables exist on the read
// path only because the startup migration ran before the server bound its port
// (SPEC/WEB.md § Startup Schema Migration; § Read-Only Data Flow, rule 2).
//
// A roadmap left at the v1.6.0 schema — which predates the comment tables entirely —
// is migrated by that startup step, after which both comment-bearing pages render
// with the empty state rather than failing the read.
func TestCommentPages_RenderOnAMigratedLegacyRoadmap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmapName = "legacy-settlement-ledger"
	buildStaleSchemaDB(t, roadmapName)

	// What serve() does before it binds.
	migrateRoadmapsAtStartup()

	mux := buildMux()

	// The stale fixture seeds one sprint (id 1) and no task, so the sprint page
	// exercises the Comments card's empty state and the tasks page renders no modal.
	sprintBody := servePage(t, mux, "/roadmaps/"+roadmapName+"/sprints/1")
	if !strings.Contains(sprintBody,
		`<h3 class="card-title">Comments <span class="badge bg-secondary-lt ms-2">0</span></h3>`) {
		t.Errorf("the migrated legacy roadmap's sprint page does not render the Comments card")
	}
	if !strings.Contains(sprintBody, `<p class="empty-title">No comments</p>`) {
		t.Errorf("the migrated legacy roadmap's Comments card does not show its empty state")
	}

	tasksBody := servePage(t, mux, "/roadmaps/"+roadmapName+"/tasks")
	if strings.Contains(tasksBody, timelineList) {
		t.Errorf("the migrated legacy roadmap's tasks page renders a timeline with no task")
	}
}
