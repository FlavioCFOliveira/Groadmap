package web

import (
	"context"
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The comment log on the read-only web interface (SPEC/WEB.md § Roadmap Task
// Page, Comments card; § Sprint Detail Sub-Template, Comments card; Acceptance
// Criteria 64-73).
//
// Every assertion here is made against markup rendered from a real on-disk
// SQLite roadmap seeded through the production write API, so what is measured is
// what a browser receives.

// Seeded comment bodies. They are declared as constants because most assertions
// look for them verbatim in the rendered pages, and
// because three of them carry authored line breaks, which the Markdown renderer
// turns into <br>: the newline is written as the Go escape \n, never as a literal
// control character in the source.
const (
	// The three comments of the "logged" task, oldest first.
	bodyFinding = "The settlement reconciliation drifts by one cent on windows " +
		"that close mid-second: the rounding runs before the currency conversion."
	bodyHypothesis = "Converting first and rounding once at the end should remove the drift.\n" +
		"The ledger export of 2026-08-12 is the reference to compare against."
	bodyDecisionOriginal = "Round once, after the conversion."
	bodyDecisionEdited   = "Round once, after the conversion, and keep the residual per window " +
		"in the reconciliation report.\nThe per-window residual is what makes a drift traceable."

	// The comment whose body carries markup: its raw HTML never reaches a page.
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

// sprintCommentsCardSlice returns the sprint page's Comments card: from its card
// header to the end of the page body, which the card closes as the last card of
// the sprint detail sub-template. It is the region Acceptance Criteria 68, 69 and
// 72 speak about.
func sprintCommentsCardSlice(t *testing.T, body string) string {
	t.Helper()

	start := strings.Index(body, `<h3 class="card-title">Comments`)
	if start < 0 {
		t.Fatalf("the sprint page renders no Comments card")
	}
	rest := body[start:]
	if end := strings.Index(rest, "</main>"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// taskCommentsCardSlice returns the task page's Comments card: from its opening
// element to the end of the page body. The card is the last card of the main
// column, and the main column is the last column of the page's row, so nothing
// else of the page lies inside the region.
func taskCommentsCardSlice(t *testing.T, body string) string {
	t.Helper()

	start := strings.Index(body, `data-role="task-comments-card"`)
	if start < 0 {
		t.Fatalf("the task page renders no Comments card")
	}
	rest := body[start:]
	if end := strings.Index(rest, "</main>"); end >= 0 {
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
	markdownBlock = `<div class="markdown">`
)

// typeBadge renders the markup a comment type's badge must produce: the neutral
// variant, for every type value (Acceptance Criterion 66).
func typeBadge(commentType models.CommentType) string {
	return `<span class="badge bg-secondary-lt">` + string(commentType) + `</span>`
}

// rendered returns a seeded value as html/template's contextual auto-escaping
// writes it into a page, which is how every value but a Markdown field is
// rendered: an assertion that looks for a value containing an apostrophe, an
// ampersand, or a tag must look for the ESCAPED form.
func rendered(text string) string { return html.EscapeString(text) }

// renderedMarkdownText returns a seeded plain-text comment body as the page
// carries it. A comment body is a Markdown field, and the Markdown renderer writes
// the text of a paragraph with &, <, >, and " escaped and nothing else — an
// apostrophe stays as it is (SPEC/WEB.md § Markdown Rendering, rule 10;
// Acceptance Criterion 73).
func renderedMarkdownText(text string) string { return markdownTextEscaper.Replace(text) }

// markdownTextEscaper escapes text exactly as the Markdown renderer escapes the
// text of a paragraph.
var markdownTextEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// TestTaskPage_CommentLog is the gate for Acceptance Criteria 64 and 65 on the task
// surface: the task page renders the task's comments as a timeline in its Comments
// card, placed after the Completion summary card and last in the main column,
// oldest first, every one of them, each with its type badge, its created_at, its
// updated_at when it has one, and its body; the card header carries the count.
func TestTaskPage_CommentLog(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/tasks/"+itoa(f.loggedTaskID))

	// Placement: after the Completion summary card, and the last card of the main
	// column, which is the last column of the row.
	summaryAt := strings.Index(body, `<h3 class="card-title">Completion summary</h3>`)
	commentsAt := strings.Index(body, `data-role="task-comments-card"`)
	if summaryAt < 0 || commentsAt < 0 || commentsAt < summaryAt {
		t.Fatalf("the Comments card does not follow the Completion summary card (summary=%d comments=%d)",
			summaryAt, commentsAt)
	}
	card := taskCommentsCardSlice(t, body)
	if strings.Contains(card, `<div class="card mb-3"`) || strings.Contains(card, `data-role="task-field-card"`) {
		t.Errorf("a card follows the Comments card in the main column")
	}

	if !strings.Contains(card, `<h3 class="card-title">Comments <span class="badge bg-secondary-lt ms-2">3</span></h3>`) {
		t.Errorf("the Comments card header does not carry the comment-count badge for 3 comments")
	}
	if !strings.Contains(card, timelineList) {
		t.Fatalf("the Comments card renders no %s", timelineList)
	}
	for _, part := range []string{timelineEvent, timelineIcon, timelineCard, markdownBlock} {
		if got := strings.Count(card, part); got != 3 {
			t.Errorf("the Comments card carries %d of %q, want 3 (one per comment)", got, part)
		}
	}

	// Oldest first, exactly the order `rmp task comment-list` returns.
	wantTypes := []models.CommentType{models.CommentFinding, models.CommentHypothesis, models.CommentDecision}
	wantBodies := []string{bodyFinding, "Converting first and rounding once at the end should remove the drift.",
		"Round once, after the conversion, and keep the residual per window"}
	last := -1
	for i := range wantBodies {
		at := strings.Index(card, renderedMarkdownText(wantBodies[i]))
		if at < 0 {
			t.Fatalf("the Comments card does not show comment %d: %q", i, wantBodies[i])
		}
		if at < last {
			t.Errorf("the Comments card is not oldest first: comment %d precedes comment %d", i, i-1)
		}
		last = at
		if !strings.Contains(card, typeBadge(wantTypes[i])) {
			t.Errorf("the Comments card is missing the neutral %s badge", wantTypes[i])
		}
	}

	// The timestamps, in the display form, with the edited marker outside <time>.
	for _, want := range []string{
		`<span class="text-secondary"><time datetime="` + createdFinding + `">2026-08-14 09:12:00</time></span>`,
		`<span class="text-secondary"><time datetime="` + createdDecision + `">2026-08-15 08:05:00</time></span>`,
		`<span class="text-secondary">edited <time datetime="` + updatedDecision + `">2026-08-16 07:30:00</time></span>`,
	} {
		if !strings.Contains(card, want) {
			t.Errorf("the Comments card does not show %s", want)
		}
	}
	if got := strings.Count(card, "edited "); got != 1 {
		t.Errorf("the Comments card shows %d edited markers, want exactly 1", got)
	}
	// The authored line break of the hypothesis survives as a <br>.
	if !strings.Contains(card, "should remove the drift.<br>") {
		t.Errorf("the hypothesis body lost its authored line break")
	}

	// The boards carry none of this: no comment body reaches either board page.
	for _, path := range []string{
		"/roadmaps/" + f.name + "/tasks",
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.sprintID),
	} {
		page := servePage(t, mux, path)
		for _, raw := range []string{bodyFinding, bodyDecisionEdited} {
			if strings.Contains(page, raw) || strings.Contains(page, rendered(raw)) ||
				strings.Contains(page, renderedMarkdownText(raw)) {
				t.Errorf("%s: a task comment body reached a board page: %q", path, raw)
			}
		}
	}
}

// TestTaskPage_CommentEmptyState is the gate for Acceptance Criterion 67: the page
// of a task with no comments renders its Comments card with a clear empty-state
// message in place of the timeline — not an empty list, and not a missing card.
func TestTaskPage_CommentEmptyState(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/tasks/"+itoa(f.quietTaskID))
	card := taskCommentsCardSlice(t, body)

	if !strings.Contains(card, `<h3 class="card-title">Comments <span class="badge bg-secondary-lt ms-2">0</span></h3>`) {
		t.Errorf("a task with no comments does not render the Comments card with a zero count badge")
	}
	if strings.Contains(card, timelineList) || strings.Contains(card, timelineEvent) {
		t.Errorf("a task with no comments renders a timeline instead of an empty state")
	}
	for _, marker := range []string{
		`<p class="empty-title">No comments</p>`,
		"Nothing has been recorded on this task yet.",
	} {
		if !strings.Contains(card, marker) {
			t.Errorf("the Comments card of a task with no comments is missing %q", marker)
		}
	}
}

// TestTaskPage_CommentBodyMarkupNeverReachesThePage is the gate for Acceptance
// Criterion 73 on the task page: a comment body carrying raw HTML renders through
// the Markdown renderer, which emits none of that raw HTML, so no element and no
// script of the author's reaches the page, and a < that is not raw HTML renders
// as the character itself.
func TestTaskPage_CommentBodyMarkupNeverReachesThePage(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	page := servePage(t, mux, "/roadmaps/"+f.name+"/tasks/"+itoa(f.markupTaskID))
	card := taskCommentsCardSlice(t, page)
	for _, raw := range []string{"<script>alert(", "<b>bold</b>", "</script> and"} {
		if strings.Contains(page, raw) {
			t.Errorf("a comment body reached the page as markup: found %q", raw)
		}
	}
	if !strings.Contains(card, "Both must reach the page as text.") {
		t.Errorf("the markup comment's text is missing from the Comments card")
	}
	// The page's script elements are exactly the one it loads, the vendored
	// framework. A body that became markup would raise either count.
	if got := strings.Count(page, "<script"); got != 1 {
		t.Errorf("page has %d <script elements, want exactly 1 (the vendored bundle)", got)
	}
	if got := strings.Count(page, "</script>"); got != 1 {
		t.Errorf("page has %d </script> closers, want exactly 1", got)
	}

	// The boards never carry a comment body at all.
	tasksPage := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")
	if strings.Contains(tasksPage, "Both must reach the page as text.") {
		t.Errorf("a comment body reached the tasks page")
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
	t.Setenv("HOME", shortHome(t))
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.sprintID))

	// Placement: after the member-tasks board, and last. The anchor is the LAST
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

	// The same timeline structure the task page uses, one event per comment.
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
	progressAt := strings.Index(card, renderedMarkdownText(bodySprintProgress))
	decisionAt := strings.Index(card, renderedMarkdownText(bodySprintDecisionEdited))
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
	// Both timestamps are displayed in the display form of SPEC/WEB.md § Date and
	// Time Display, inside a <time> element; the edited marker stays outside it.
	if !strings.Contains(card, `<span class="text-secondary"><time datetime="`+createdSprintProgress+
		`">2026-08-13 17:00:00</time></span>`) {
		t.Errorf("the Comments card does not show a comment's created_at timestamp")
	}
	if !strings.Contains(card, `<span class="text-secondary">edited <time datetime="`+updatedSprintDecision+
		`">2026-08-16 10:00:00</time></span>`) {
		t.Errorf("the Comments card does not mark the edited comment with its updated_at")
	}
	if got := strings.Count(card, "edited "); got != 1 {
		t.Errorf("the Comments card shows %d edited markers, want exactly 1", got)
	}
	// Each of the sprint's own comment bodies is rendered as Markdown, in the
	// Tabler markdown container (Acceptance Criterion 180).
	if got := strings.Count(card, markdownBlock); got != 2 {
		t.Errorf("%d sprint comment bodies sit in the markdown container, want 2", got)
	}
	if !strings.Contains(card, "the next sprint: it needs the currency table refresh") {
		t.Errorf("the Comments card does not show the stored (edited) body of the edited comment")
	}
}

// TestSprintPage_CommentsCardEmptyState is the other half of Acceptance Criterion
// 68: a sprint with no comments STILL renders the card, showing an empty state in
// place of the timeline.
func TestSprintPage_CommentsCardEmptyState(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
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
// against a member task appears on that task's own page and nowhere in the card,
// and no aggregate of task comments is presented at sprint level.
func TestSprintPage_CommentsCardHoldsOnlySprintOwnComments(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.sprintID))
	card := sprintCommentsCardSlice(t, body)

	// No member task's comment leaks into the sprint's card.
	for _, taskBody := range []string{
		rendered(bodyFinding), rendered(bodyHypothesis), rendered(bodyDecisionEdited), rendered(bodyMarkup),
		renderedMarkdownText(bodyFinding), renderedMarkdownText(bodyHypothesis),
		renderedMarkdownText(bodyDecisionEdited), renderedMarkdownText(bodyMarkup),
	} {
		if strings.Contains(card, taskBody) {
			t.Errorf("the sprint Comments card shows a member task's comment: %q", taskBody)
		}
	}
	// Exactly the sprint's own two comments are in the card.
	if got := strings.Count(card, timelineEvent); got != 2 {
		t.Errorf("the sprint Comments card holds %d entries, want exactly the sprint's own 2", got)
	}

	// The member task's log is on that task's own page, and it is the task's log,
	// not the sprint's.
	taskCard := taskCommentsCardSlice(t, servePage(t, mux, "/roadmaps/"+f.name+"/tasks/"+itoa(f.loggedTaskID)))
	if got := strings.Count(taskCard, timelineEvent); got != 3 ||
		!strings.Contains(taskCard, renderedMarkdownText(bodyFinding)) {
		t.Errorf("the member task's own log is not on its page (%d entries)", got)
	}
	for _, sprintBody := range []string{bodySprintProgress, bodySprintDecisionEdited} {
		if strings.Contains(taskCard, renderedMarkdownText(sprintBody)) {
			t.Errorf("a sprint comment leaked into a task's page: %q", sprintBody)
		}
	}
}

// TestTasksPage_ShowsNoCommentInformation pins that the tasks page's list shows
// no comment information — no count and no text — for a task in no sprint and for
// a member task alike, while that task's own page still shows its log, and the
// sprint page renders only its member tasks (SPEC/WEB.md § Roadmap Tasks Page,
// Row content; Acceptance Criteria 70 and 85).
func TestTasksPage_ShowsNoCommentInformation(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	tasksBody := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")
	list := boardRegion(t, tasksBody)
	if !strings.Contains(list, cardMarker(f.looseTaskID)) {
		t.Fatalf("the task in no sprint has no row on the tasks page")
	}
	for _, forbidden := range []string{"ti-message", "Comments:", renderedMarkdownText(bodyLoose), renderedMarkdownText(bodyFinding)} {
		if strings.Contains(list, forbidden) {
			t.Errorf("the tasks page's list carries comment information %q; the row shows none", forbidden)
		}
	}

	// The task's log is on its own page.
	loose := taskCommentsCardSlice(t, servePage(t, mux, "/roadmaps/"+f.name+"/tasks/"+itoa(f.looseTaskID)))
	if got := strings.Count(loose, timelineEvent); got != 1 {
		t.Fatalf("the page of the task in no sprint carries %d comments, want 1", got)
	}
	if !strings.Contains(loose, renderedMarkdownText(bodyLoose)) {
		t.Errorf("the page of the task in no sprint does not show %q", bodyLoose)
	}
	if !strings.Contains(loose, typeBadge(models.CommentNote)) {
		t.Errorf("the comment type badge is not NOTE")
	}

	// The sprint page renders only its member tasks, so that task has no card
	// there and its comment is nowhere on it.
	sprintBody := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.sprintID))
	if strings.Contains(sprintBody, cardMarker(f.looseTaskID)) {
		t.Errorf("the sprint page offers a card for a task that is not a member of the sprint")
	}
	if strings.Contains(sprintBody, rendered(bodyLoose)) || strings.Contains(sprintBody, renderedMarkdownText(bodyLoose)) {
		t.Errorf("the sprint page shows the comment of a task that is not a member of the sprint")
	}
}

// TestSprintsLandingPage_RendersNoCommentLog pins that the sprints landing page is
// unchanged by this feature: it renders every sprint as a compact card, links to
// no task page, and therefore shows no comment log of any kind — neither the
// sprint's own nor a member task's (SPEC/WEB.md § Shared Sprint-Card Partial).
func TestSprintsLandingPage_RendersNoCommentLog(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name)

	for _, marker := range []string{
		timelineList, timelineEvent, timelineIcon,
		`<h3 class="card-title">Comments`, `data-role="task-comments-card"`,
	} {
		if strings.Contains(body, marker) {
			t.Errorf("the sprints landing page must not render %q", marker)
		}
	}
	for _, commentBody := range []string{
		rendered(bodyFinding), rendered(bodyHypothesis), rendered(bodyDecisionEdited), rendered(bodyMarkup),
		rendered(bodySprintProgress), rendered(bodySprintDecisionEdited),
		renderedMarkdownText(bodySprintProgress), renderedMarkdownText(bodySprintDecisionEdited),
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
	sprintTitles         int
	taskList             int
	boundedTaskList      int
	sprintTasks          int
	lastTaskFilter       *db.TaskListFilter
}

// ListSprintTitles is the tasks page's sprint read: the id and title of every
// sprint, for the sprint filter (SPEC/DATABASE.md § List Sprint Titles).
func (c *countingSource) ListSprintTitles(ctx context.Context) ([]db.SprintRef, error) {
	c.sprintTitles++
	return c.DB.ListSprintTitles(ctx)
}

// ListSprints is the read the sprints page performs: the roadmap's sprints, with
// the membership of every one of them resolved in the same bounded number of
// statements whatever the number of sprints (SPEC/COMMANDS.md § List Sprints).
func (c *countingSource) ListSprints(ctx context.Context,
	status *models.SprintStatus) ([]models.Sprint, error) {
	c.sprintListings++
	return c.DB.ListSprints(ctx, status)
}

// ListAllTasks is the read the tasks page performs: every task the structured
// filters admit, never bounded by a page. The filter is recorded so a test can
// assert which predicates the page asked for.
func (c *countingSource) ListAllTasks(ctx context.Context, filter *db.TaskListFilter) ([]models.Task, error) {
	c.taskList++
	c.lastTaskFilter = filter
	return c.DB.ListAllTasks(ctx, filter)
}

// ListTasks is the CLI's bounded listing, which the tasks page must NOT use: its
// limit is capped at models.MaxTaskLimit, so a roadmap with more tasks than that
// would publish a wrong total. It is unreachable through the tasksSource
// interface; counting it here keeps that seam falsifiable if the interface is ever
// widened.
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

// TestTasksPage_IssuesNoCommentQuery is the gate for Acceptance Criterion 70 on
// the tasks page: the list shows no comment information, so rendering it issues
// no comment query of any kind — neither a grouped count nor a per-task listing —
// for every N, and none when the page renders no task.
func TestTasksPage_IssuesNoCommentQuery(t *testing.T) {
	t.Setenv("HOME", shortHome(t))

	for _, taskCount := range []int{0, 1, 3, 12} {
		name := "settlement-window-" + itoa(taskCount)
		ids := seedTasksWithComments(t, name, taskCount)
		src := openCounting(t, name)

		data, err := readTaskList(context.Background(), src, name, url.Values{})
		if err != nil {
			t.Fatalf("%d tasks: readTaskList: %v", taskCount, err)
		}
		if len(data.Rows) != taskCount {
			t.Fatalf("%d tasks: the page renders %d rows, want %d", taskCount, len(data.Rows), taskCount)
		}
		if src.groupedCommentCounts != 0 || src.perTaskComments != 0 || src.sprintComments != 0 {
			t.Errorf("%d tasks: the page issued comment queries (grouped %d, per task %d, sprint %d), want none",
				taskCount, src.groupedCommentCounts, src.perTaskComments, src.sprintComments)
		}

		// The control that makes the zero falsifiable: the instrument does count a
		// comment read when one is issued.
		for _, id := range ids {
			if _, lerr := src.ListTaskComments(context.Background(), id, nil); lerr != nil {
				t.Fatalf("%d tasks: per-task control read of task #%d: %v", taskCount, id, lerr)
			}
		}
		if src.perTaskComments != taskCount {
			t.Errorf("%d tasks: the per-task control issued %d reads, want %d; the instrument does "+
				"not track reads one-for-one", taskCount, src.perTaskComments, taskCount)
		}
	}
}

// TestSprintPage_CommentQueryCount is the gate for Acceptance Criterion 70 on the
// sprint page: TWO comment queries, whatever the number of member tasks — the
// sprint's own listing, which the Comments card renders in full as a log, and ONE
// grouped COUNT over the whole set of rendered member-task ids, which is what gives
// each board card its comment number.
//
// The page reads no comment BODY for a task it renders: a card shows a number, and
// the text of a member task's comments is read only by that task's own page, one
// task at a time. That is what the
// zero on the per-task listing states (SPEC/WEB.md § Tasks and Sprints from SQLite;
// § Sprint Detail Sub-Template, Read cost; Acceptance Criterion 137).
func TestSprintPage_CommentQueryCount(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
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
			"count and a member task's comment TEXT is read only by its own page",
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
// on the markup: neither the sprint Comments card nor the task page's Comments
// card contains a form, an input, a button, or a link — nothing through which a
// comment could be created, edited or deleted from the browser.
//
// Each card is asserted as a region rather than the whole page, because a page
// legitimately carries links that submit nothing (the sidebar, the back link): a
// page-wide assertion would either fail on those or have to be weakened until it
// proved nothing.
func TestCommentSurface_HasNoWriteAffordance(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	// Anything that could carry a change to the server, plus the attributes that
	// would make an element do so.
	forbidden := []string{
		"<form", "<input", "<button", "<textarea", "<select", "<a ",
		"href=", "action=", "formaction=", "method=", "onclick=", "onsubmit=",
		"data-bs-toggle=", "contenteditable",
	}
	regions := map[string]string{
		"the sprint Comments card": sprintCommentsCardSlice(t,
			servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.sprintID))),
		"the task page's Comments card": taskCommentsCardSlice(t,
			servePage(t, mux, "/roadmaps/"+f.name+"/tasks/"+itoa(f.loggedTaskID))),
	}
	for label, region := range regions {
		lower := strings.ToLower(region)
		if !strings.Contains(lower, "timeline-event") {
			t.Fatalf("%s holds no timeline; the region is wrong", label)
		}
		for _, bad := range forbidden {
			if strings.Contains(lower, bad) {
				t.Errorf("%s must be read-only but contains %q", label, bad)
			}
		}
	}
}

// wantRoutePatterns is the complete, pinned set of patterns the read-only mux
// registers. Every roadmap page is registered for GET and for HEAD only; the bare
// "/" is the catch-all that answers 404 for an unknown read and 405 for any other
// method. No route below /roadmaps/{name}/tasks/{id} exists: the interface serves
// no JSON for a task (SPEC/WEB.md § Routes and Pages, rule 5; Acceptance
// Criteria 72 and 96).
var wantRoutePatterns = []string{
	"/",
	"GET /roadmaps/{name}",
	"GET /roadmaps/{name}/audit",
	"GET /roadmaps/{name}/graph",
	"GET /roadmaps/{name}/graph/data",
	"GET /roadmaps/{name}/sprints/{id}",
	"GET /roadmaps/{name}/tasks",
	"GET /roadmaps/{name}/tasks/{id}",
	"GET /static/",
	"GET /{$}",
	"HEAD /roadmaps/{name}",
	"HEAD /roadmaps/{name}/audit",
	"HEAD /roadmaps/{name}/graph",
	"HEAD /roadmaps/{name}/graph/data",
	"HEAD /roadmaps/{name}/sprints/{id}",
	"HEAD /roadmaps/{name}/tasks",
	"HEAD /roadmaps/{name}/tasks/{id}",
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
	t.Setenv("HOME", shortHome(t))
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	paths := []string{
		"/roadmaps/" + f.name,
		"/roadmaps/" + f.name + "/tasks",
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.sprintID),
		"/roadmaps/" + f.name + "/tasks/" + itoa(f.loggedTaskID),
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
	t.Setenv("HOME", shortHome(t))
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

	// The task page's Comments card is the same partial, server-rendered too.
	taskBody := servePage(t, mux, "/roadmaps/"+f.name+"/tasks/"+itoa(f.loggedTaskID))
	for _, class := range classTokens(taskCommentsCardSlice(t, taskBody)) {
		if !strings.Contains(styles, "."+class) {
			t.Errorf("the task page's Comments card uses the class %q, which no embedded stylesheet "+
				"defines; the feature must add no CSS", class)
		}
	}
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
	t.Setenv("HOME", shortHome(t))
	f := seedCommentFixture(t, "settlement-reconciliation")
	mux := buildMux()

	paths := []string{
		"/roadmaps/" + f.name + "/tasks",
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.sprintID),      // comments present
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.quietSprintID), // empty state
		"/roadmaps/" + f.name + "/tasks/" + itoa(f.loggedTaskID),    // task log
		"/roadmaps/" + f.name + "/tasks/" + itoa(f.quietTaskID),     // task empty state
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
		// The asset chain is exactly the embedded one: the four stylesheets of
		// every page, plus the syntax-highlighting stylesheet on every page that
		// can render a Markdown field — not the tasks page, which renders none —
		// and the vendored script, all from /static/. The comment log added none
		// of them (SPEC/WEB.md § Markdown Rendering, rule 7).
		assets := []string{
			`<link rel="stylesheet" href="/static/vendor/inter/inter.css">`,
			`<link rel="stylesheet" href="/static/vendor/tabler/tabler.min.css">`,
			`<link rel="stylesheet" href="/static/vendor/tabler-icons/tabler-icons.min.css">`,
			`<link rel="stylesheet" href="/static/style.css">`,
			`<script src="/static/vendor/tabler/tabler.min.js"></script>`,
		}
		wantSheets := 4
		if !strings.HasSuffix(path, "/tasks") {
			assets = append(assets, `<link rel="stylesheet" href="/static/highlight.css">`)
			wantSheets = 5
		} else if strings.Contains(body, "/static/highlight.css") {
			t.Errorf("page %s links highlight.css, but it renders no Markdown field", path)
		}
		for _, asset := range assets {
			if !strings.Contains(body, asset) {
				t.Errorf("page %s is missing the embedded asset %s", path, asset)
			}
		}
		if got := strings.Count(body, "<link rel=\"stylesheet\""); got != wantSheets {
			t.Errorf("page %s loads %d stylesheets, want the %d embedded ones", path, got, wantSheets)
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
	t.Setenv("HOME", shortHome(t))

	const roadmapName = "legacy-settlement-ledger"
	buildStaleSchemaDB(t, roadmapName)

	// What serve() does before it binds.
	migrateRoadmapsAtStartup()

	mux := buildMux()

	// The stale fixture seeds one sprint (id 1) and no task, so the sprint page
	// exercises the Comments card's empty state and the tasks page renders no card.
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
