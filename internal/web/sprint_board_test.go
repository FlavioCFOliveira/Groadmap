package web

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for the Roadmap Sprint Page's member-tasks board: the
// three fixed columns, the placement and ordering of the cards inside them, the
// identity between the column counts and the sprint status summary line, the
// card's content, the board's bounded height, and the page's comment read cost
// (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4; Acceptance Criteria 130 to
// 140). The COLOUR of each column's count badge is guarded separately, together
// with the tasks board's, in board_column_badge_test.go.
//
// It replaces the assertions that pinned the six-column member-tasks table the
// board supersedes: the page renders no table at all any more, so a test written
// against that table would fail for the wrong reason — its subject is gone.
//
// The markup helpers of the tasks board (boardRegion, columnHeader, cardSlice,
// spanWithRole, cardOpen, cardMarker, shownEmptyState in board_test.go) are reused
// verbatim wherever they apply, because the two boards emit the same classes and
// the same data-role hooks: they are one presentation rendered on two pages, and
// a helper that worked on only one of them would be evidence they had diverged.
// The one helper NOT reused here is metaFooter, and its absence is the point: this
// board's card carries no metadata footer at all, because its two counters share
// the badge line (SPEC/WEB.md § Sprint Detail Sub-Template, The two cards differ
// here; Acceptance Criterion 133).

// ==================== FIXTURE ====================

// sprintBoardFixture names the rows seedSprintBoardFixture created, so the
// assertions bind the ids that actually exist rather than assuming an
// autoincrement sequence.
//
// The six member tasks populate all three columns, with two statuses in WAITING
// and two in DOING, so a board that mapped one status per column — or that
// grouped by a categorisation of its own — could not produce the counts the
// summary line states.
type sprintBoardFixture struct {
	name     string
	sprintID int

	// The member tasks, in sprint_tasks POSITION order (1 to 6). Neither the id
	// order nor the priority order matches it; see seedSprintBoardFixture.
	// The counters noted here are the numbers the cards must SHOW. Every card of
	// this board renders both, including the zeros, so the interesting cases are
	// the card with two zeros and the two cards carrying one real number beside
	// one zero (Acceptance Criterion 134).
	runbook   int // BACKLOG   -> WAITING. 0 subtasks, 0 comments.
	reconcile int // SPRINT    -> WAITING. 2 subtasks, 3 comments.
	dashboard int // DOING     -> DOING.   0 subtasks, 0 comments.
	retries   int // TESTING   -> DOING.   1 subtask,  0 comments.
	alerting  int // SPRINT    -> WAITING. 0 subtasks, 2 comments.
	schema    int // COMPLETED -> CLOSED.  0 subtasks, 0 comments.
}

// wantColumns is the placement the fixture must produce, per column heading, in
// the order the cards must appear inside that column.
func (f *sprintBoardFixture) wantColumns() [][]int {
	return [][]int{
		{f.runbook, f.reconcile, f.alerting}, // WAITING: positions 1, 2 and 5
		{f.dashboard, f.retries},             // DOING:   positions 3 and 4
		{f.schema},                           // CLOSED:  position 6
	}
}

// wantSummaryLine is the sprint status summary line the fixture produces: three
// WAITING, two DOING, one CLOSED, one of six completed (17%).
//
// The three counts are pairwise distinct and none is zero, which is what makes
// the identity assertion of Acceptance Criterion 131 discriminating: a board that
// mapped the categories to the wrong columns would show three numbers that no
// longer line up with P, A and C, where equal or zero counts would let a
// mis-mapping pass unnoticed.
const wantSummaryLine = "17% - P:3 A:2 C:1 - T:6"

// The member-task titles, named so the fixture and the assertions agree on them
// without repeating string literals.
const (
	sprintTaskRunbook   = "Write the settlement incident runbook"
	sprintTaskReconcile = "Reconcile the acquirer settlement file against the ledger"
	sprintTaskDashboard = "Publish the reconciliation dashboard to the operations team"
	sprintTaskRetries   = "Retry the acquirer webhook delivery with exponential backoff"
	sprintTaskAlerting  = "Alert on residual balances after the nightly reconciliation"
	sprintTaskSchema    = "Version the settlement export schema"
)

// sprintBoardSpecialists is the specialists value of the card asserted against in
// full. The card must NOT show it: the field is reached through the task detail
// modal the card opens (Acceptance Criterion 133).
const sprintBoardSpecialists = "go-developer, exhaustive-qa-engineer"

// seedSprintBoardFixture builds a roadmap holding one OPEN sprint with six member
// tasks spread over all three board columns, a parent/subtask hierarchy, a
// dependency edge, specialists, and comments, so every card indicator has at
// least one card that shows it and at least one card that must not.
//
// The three orders are deliberately different. Tasks are CREATED in one order
// (which fixes the id order), added to the sprint in a SECOND order (which fixes
// the sprint_tasks position order the board must render), and carry priorities in
// a THIRD order:
//
//	position:      runbook, reconcile, dashboard, retries, alerting, schema
//	id:            reconcile, alerting, retries, schema, dashboard, runbook
//	priority DESC: reconcile(9), runbook(8), retries(7), schema(5), alerting(3), dashboard(2)
//
// A board that re-sorted its cards, or that fell back to the id order the read
// did not give it, therefore renders a different sequence and fails the ordering
// assertion (Acceptance Criterion 132).
func seedSprintBoardFixture(t *testing.T, name string) sprintBoardFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	f := sprintBoardFixture{name: name}

	newTask := func(title, created string, priority, severity int,
		taskType models.TaskType, specialists *string, parent *int) int {
		t.Helper()
		id, cerr := database.CreateTask(ctx, &models.Task{
			Title:                  title,
			Type:                   taskType,
			Status:                 models.StatusBacklog,
			Priority:               priority,
			Severity:               severity,
			Specialists:            specialists,
			ParentTaskID:           parent,
			FunctionalRequirements: "Operators must be able to follow this work from the sprint board.",
			TechnicalRequirements:  "Implemented against the roadmap database, read-only on the web side.",
			AcceptanceCriteria:     "The board shows the task in the column of its status.",
			CreatedAt:              created,
		})
		if cerr != nil {
			t.Fatalf("creating task %q: %v", title, cerr)
		}
		return id
	}

	// Creation order fixes the id order, which is neither of the two orders below.
	specialists := sprintBoardSpecialists
	f.reconcile = newTask(sprintTaskReconcile, "2026-03-01T09:00:00Z", 9, 2,
		models.TypeUserStory, &specialists, nil)
	f.alerting = newTask(sprintTaskAlerting, "2026-03-02T09:00:00Z", 3, 5,
		models.TypeTask, nil, nil)
	f.retries = newTask(sprintTaskRetries, "2026-03-03T09:00:00Z", 7, 6,
		models.TypeBug, nil, nil)
	f.schema = newTask(sprintTaskSchema, "2026-03-04T09:00:00Z", 5, 3,
		models.TypeChore, nil, nil)
	f.dashboard = newTask(sprintTaskDashboard, "2026-03-05T09:00:00Z", 2, 1,
		models.TypeTask, nil, nil)
	f.runbook = newTask(sprintTaskRunbook, "2026-03-06T09:00:00Z", 8, 4,
		models.TypeChore, nil, nil)

	// Subtasks, which are tasks of the roadmap but NOT members of the sprint, so
	// they raise a member's subtask_count without appearing on the board.
	newTask("Parse the acquirer settlement file header", "2026-03-07T09:00:00Z", 4, 2,
		models.TypeSubTask, nil, &f.reconcile)
	newTask("Match settlement lines to ledger entries by reference", "2026-03-08T09:00:00Z", 4, 2,
		models.TypeSubTask, nil, &f.reconcile)
	newTask("Cap the webhook retry budget per acquirer", "2026-03-09T09:00:00Z", 4, 2,
		models.TypeSubTask, nil, &f.retries)

	f.sprintID = newSprint(t, database, "Harden the settlement reconciliation pipeline",
		"Close the reconciliation gaps the March incident exposed, end to end.")

	// Membership in POSITION order, which AddTasksToSprint assigns sequentially.
	// It differs from both the id order and the priority order above.
	members := []int{f.runbook, f.reconcile, f.dashboard, f.retries, f.alerting, f.schema}
	if aerr := database.AddTasksToSprint(ctx, f.sprintID, members); aerr != nil {
		t.Fatalf("adding the member tasks to the sprint: %v", aerr)
	}
	if serr := database.UpdateSprintStatus(ctx, f.sprintID, models.SprintOpen); serr != nil {
		t.Fatalf("opening the sprint: %v", serr)
	}

	// Membership forces every member to SPRINT, so the statuses that populate the
	// other columns are set from there through the production write path. runbook
	// goes back to BACKLOG, which is the second status the WAITING column holds:
	// SPEC/WEB.md assigns BACKLOG and SPRINT to that one column, and the sprint
	// summary line counts both as pending, so a member in BACKLOG is a state the
	// board must place, not a contrived one.
	for status, ids := range map[models.TaskStatus][]int{
		models.StatusBacklog:   {f.runbook},
		models.StatusDoing:     {f.dashboard},
		models.StatusTesting:   {f.retries},
		models.StatusCompleted: {f.schema},
	} {
		if uerr := database.UpdateTaskStatus(ctx, ids, status); uerr != nil {
			t.Fatalf("moving %v to %s: %v", ids, status, uerr)
		}
	}

	// One dependency edge between two members, so the card of each carries a
	// non-empty DependsOn / Blocks and the assertion that neither is rendered is
	// not vacuous.
	if derr := database.AddTaskDependencyWithAudit(ctx, f.reconcile, f.runbook); derr != nil {
		t.Fatalf("making the reconciliation task depend on the runbook task: %v", derr)
	}

	// Comments: three on the reconciliation task, two on the alerting task, none
	// anywhere else, so a comment count appears on some cards and on no other.
	addTaskCommentTo(t, database, f.reconcile, models.CommentDecision,
		"The ledger is authoritative; the acquirer file is replayed against it, never the reverse.",
		"2026-03-10T09:00:00Z")
	addTaskCommentTo(t, database, f.reconcile, models.CommentProgress,
		"The parser reads the March files end to end against the staging ledger.",
		"2026-03-11T09:00:00Z")
	addTaskCommentTo(t, database, f.reconcile, models.CommentNote,
		"The acquirer settles on weekends, so the nightly window must cover Saturday.",
		"2026-03-12T09:00:00Z")
	addTaskCommentTo(t, database, f.alerting, models.CommentNote,
		"A residual under one cent is rounding, not a break, and must not page anyone.",
		"2026-03-13T09:00:00Z")
	addTaskCommentTo(t, database, f.alerting, models.CommentProgress,
		"The alert fires against the staging ledger with the residual threshold applied.",
		"2026-03-14T09:00:00Z")

	return f
}

// sprintBoardPath is the route the board is rendered on.
func (f *sprintBoardFixture) path() string {
	return "/roadmaps/" + f.name + "/sprints/" + itoa(f.sprintID)
}

// seedSprintWithMembers creates a roadmap holding one sprint with n member tasks
// and returns the sprint id. n may be 0: the sprint then exists with no member
// task at all, which is the case Acceptance Criterion 137 requires to issue no
// grouped comment count.
//
// Each member carries two comments, so the grouped count has rows to return and a
// per-card alternative would have something to read.
func seedSprintWithMembers(t *testing.T, name string, n int) int {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	sprintID := newSprint(t, database, "Reconcile the acquirer settlement window",
		"Bring the nightly reconciliation window back inside its service level.")

	ids := make([]int, 0, n)
	for i := range n {
		title := "Close reconciliation break " + itoa(i+1) + " of the March settlement window"
		id, cerr := database.CreateTask(ctx, &models.Task{
			Title:                  title,
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			Priority:               5,
			Severity:               4,
			FunctionalRequirements: "The break is explained and cleared against the ledger.",
			TechnicalRequirements:  "Reconciled through the settlement pipeline, not by hand.",
			AcceptanceCriteria:     "The break no longer appears in the nightly residual report.",
			CreatedAt:              "2026-04-01T09:00:00Z",
		})
		if cerr != nil {
			t.Fatalf("creating task %q: %v", title, cerr)
		}
		ids = append(ids, id)
		addTaskCommentTo(t, database, id, models.CommentProgress,
			"The break was traced to a duplicated acquirer settlement line.",
			"2026-04-02T09:00:00Z")
		addTaskCommentTo(t, database, id, models.CommentNote,
			"The acquirer confirmed the duplicate and reissued the file.",
			"2026-04-03T09:00:00Z")
	}

	if len(ids) > 0 {
		if aerr := database.AddTasksToSprint(ctx, sprintID, ids); aerr != nil {
			t.Fatalf("adding %d member tasks to the sprint: %v", len(ids), aerr)
		}
	}
	return sprintID
}

// ==================== MARKUP HELPERS ====================

// memberBoardRegion returns the markup of the sprint page's member-tasks board:
// from the board container's own marker to the Comments card that follows it.
//
// Bounding it before the Comments card is what makes every "the board shows X"
// and "the board shows no X" assertion falsifiable: the sprint page renders three
// cards outside the board — the Sprint details card above it, the Comments card
// below it, and the single modal shell after the page wrapper — and a page-wide
// check would answer for their content as readily as for the board's.
//
// The container's marker is `data-role="task-board">`, with the closing angle
// bracket: `data-role="task-board"` alone is a prefix of the column's own
// `data-role="task-board-column"` hook.
func memberBoardRegion(t *testing.T, body string) string {
	t.Helper()

	start := strings.Index(body, `data-role="task-board">`)
	if start < 0 {
		t.Fatalf("the sprint page renders no member-tasks board")
	}
	rest := body[start:]
	end := strings.Index(rest, `<h3 class="card-title">Comments `)
	if end < 0 {
		t.Fatalf("the sprint page renders no Comments card after the board, so the board " +
			"region has no end to slice at")
	}
	return rest[:end]
}

// memberBoardColumns returns the markup of each of the board's columns, in the
// order the page renders them, failing the test when there are not exactly three.
func memberBoardColumns(t *testing.T, body string) []string {
	t.Helper()

	parts := strings.Split(memberBoardRegion(t, body), `data-role="task-board-column"`)
	if len(parts) != 4 {
		t.Fatalf("the member-tasks board renders %d columns, want exactly 3", len(parts)-1)
	}
	return parts[1:]
}

// memberCardIDs returns the task ids of a column's cards, in document order,
// which is the order the reader sees them in.
func memberCardIDs(t *testing.T, column string) []int {
	t.Helper()

	matches := reModalTarget.FindAllStringSubmatch(column, -1)
	ids := make([]int, 0, len(matches))
	for _, m := range matches {
		id, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("a card carries the non-integer task id %q", m[1])
		}
		ids = append(ids, id)
	}
	return ids
}

// reSummaryLine captures the five values of the sprint status summary line.
var reSummaryLine = regexp.MustCompile(
	`data-role="sprint-summary">(\d+)% - P:(\d+) A:(\d+) C:(\d+) - T:(\d+)<`)

// summaryLineCounts returns the P, A, C and T values of the summary line the page
// rendered, read out of the served HTML rather than computed by the test.
func summaryLineCounts(t *testing.T, body string) (pending, inProgress, completed, total int) {
	t.Helper()

	m := reSummaryLine.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the sprint page renders no sprint status summary line in the documented format")
	}
	value := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("the summary line carries the non-integer value %q: %v", s, err)
		}
		return n
	}
	return value(m[2]), value(m[3]), value(m[4]), value(m[5])
}

// ==================== THE THREE FIXED COLUMNS ====================

// TestSprintBoard_RendersThreeFixedColumns is the gate for Acceptance Criterion
// 130: exactly three columns, left to right with the headings WAITING, DOING and
// CLOSED, each holding exactly the sprint's tasks in the statuses assigned to it,
// with every member task on the board once and no table of any kind on the page.
//
// The headings are compared against the production values (sprintBoardColumns),
// which are also what fixes the columns' order, so the set and the order cannot
// drift from the mapping the grouping actually applies. Those production values
// are pinned against the specification's three spellings first, so a change to
// them cannot silently redefine what this test asserts.
func TestSprintBoard_RendersThreeFixedColumns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	// The production headings are the specification's, in the specification's
	// order — asserted here so every expectation below rests on the SPEC and not
	// merely on the code agreeing with itself.
	wantHeadings := []string{"WAITING", "DOING", "CLOSED"}
	if len(sprintBoardColumns) != len(wantHeadings) {
		t.Fatalf("the board defines %d columns, want %d", len(sprintBoardColumns), len(wantHeadings))
	}
	for i, want := range wantHeadings {
		if sprintBoardColumns[i].heading != want {
			t.Errorf("column %d is headed %q, want %q", i, sprintBoardColumns[i].heading, want)
		}
	}

	body := servePage(t, mux, f.path())
	columns := memberBoardColumns(t, body)

	// The headings and the placement, column by column.
	for i, column := range columns {
		heading, count := columnHeader(t, column)
		if heading != wantHeadings[i] {
			t.Errorf("board column %d is headed %q, want %q", i, heading, wantHeadings[i])
		}
		want := f.wantColumns()[i]
		if count != len(want) {
			t.Errorf("the %s column's badge reads %d, want %d", heading, count, len(want))
		}
		got := memberCardIDs(t, column)
		if !equalIDs(got, want) {
			t.Errorf("the %s column holds the tasks %v, want %v", heading, got, want)
		}
	}

	// Every member task is on the board exactly once: none omitted, none
	// duplicated. Counting over the whole board region, not per column, is what
	// makes "exactly once" mean what it says.
	region := memberBoardRegion(t, body)
	for _, id := range []int{f.runbook, f.reconcile, f.dashboard, f.retries, f.alerting, f.schema} {
		if got := strings.Count(region, cardMarker(id)); got != 1 {
			t.Errorf("task #%d appears on the board %d times, want exactly 1", id, got)
		}
	}
	if got := strings.Count(region, cardOpen); got != 6 {
		t.Errorf("the board renders %d cards, want the sprint's 6 member tasks", got)
	}

	// No table of tasks anywhere on the page: the board replaced it outright.
	for _, absent := range []string{"<table", "<thead", "<tbody", "<tr", "<th>", "<td>", "card-table"} {
		if strings.Contains(body, absent) {
			t.Errorf("the sprint page still renders a table (%q); the member tasks are a board now", absent)
		}
	}

	// The two cards that surround the board keep their positions.
	detailsAt := strings.Index(body, `<h3 class="card-title">Sprint details</h3>`)
	boardAt := strings.Index(body, `data-role="task-board">`)
	commentsAt := strings.Index(body, `<h3 class="card-title">Comments `)
	if detailsAt < 0 || boardAt < 0 || commentsAt < 0 {
		t.Fatalf("the sprint page is missing a region (details=%d board=%d comments=%d)",
			detailsAt, boardAt, commentsAt)
	}
	if !(detailsAt < boardAt && boardAt < commentsAt) {
		t.Errorf("the sub-template renders details=%d, board=%d, comments=%d; the board must sit "+
			"between the Sprint details card and the Comments card", detailsAt, boardAt, commentsAt)
	}
}

// TestSprintBoard_EmptySprintIsAnEmptyBoard is the gate for the second half of
// Acceptance Criterion 130: a sprint with no member task renders the board with
// all three columns present, each keeping its heading and its `0` count badge and
// showing the in-column empty state in place of its card list — not a page-level
// empty state, and not an absent board.
func TestSprintBoard_EmptySprintIsAnEmptyBoard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const name = "settlement-platform"
	sprintID := seedSprintWithMembers(t, name, 0)
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+name+"/sprints/"+itoa(sprintID))
	columns := memberBoardColumns(t, body)

	for i, column := range columns {
		heading, count := columnHeader(t, column)
		if count != 0 {
			t.Errorf("the %s column of an empty sprint reads %d, want 0", heading, count)
		}
		if !shownEmptyState(column) {
			t.Errorf("the %s column of an empty sprint shows no in-column empty state", heading)
		}
		if strings.Contains(column, cardOpen) {
			t.Errorf("the %s column of an empty sprint renders a card", heading)
		}
		if heading != sprintBoardColumns[i].heading {
			t.Errorf("board column %d is headed %q, want %q", i, heading, sprintBoardColumns[i].heading)
		}
	}

	// The board itself is present, and no page-level empty state stands in for it.
	if !strings.Contains(body, `data-role="task-board">`) {
		t.Errorf("an empty sprint renders no board at all")
	}
	for _, absent := range []string{"No tasks in this sprint", "This sprint has no tasks assigned yet"} {
		if strings.Contains(body, absent) {
			t.Errorf("an empty sprint renders the page-level empty state %q in place of the board", absent)
		}
	}
}

// ==================== THE COUNTS ARE THE SUMMARY LINE'S OWN ====================

// TestSprintBoard_ColumnCountsAreTheSummaryLinesOwnNumbers is the gate for
// Acceptance Criterion 131: each column's badge equals its counterpart in the
// sprint status summary line rendered at the top of the same page — WAITING is P,
// DOING is A, CLOSED is C — and the three sum to T.
//
// Both sides are read out of ONE served page and compared against each other,
// which is what the criterion asks for and is stronger than comparing each
// against a number the test computes: the property under test is that the board
// and the line group the sprint's tasks by the SAME categorisation, and a board
// that grouped the statuses differently could still show three counts that each
// looked plausible on its own.
//
// The fixture's three counts are pairwise distinct and none is zero, so a board
// that mapped the categories to the wrong columns cannot satisfy the comparison by
// coincidence.
func TestSprintBoard_ColumnCountsAreTheSummaryLinesOwnNumbers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	body := servePage(t, mux, f.path())

	// The line is exactly the documented format, so the numbers below are read
	// from the rendering the reader sees.
	if !strings.Contains(body, wantSummaryLine) {
		t.Fatalf("the sprint page's summary line is not %q", wantSummaryLine)
	}
	pending, inProgress, completed, total := summaryLineCounts(t, body)

	// Falsifiability control: with equal or zero counts a mis-mapped board would
	// satisfy the comparison below without grouping anything correctly.
	if pending == inProgress || inProgress == completed || pending == completed {
		t.Fatalf("the summary line reads P:%d A:%d C:%d; the three must differ for the "+
			"comparison to discriminate", pending, inProgress, completed)
	}
	if pending == 0 || inProgress == 0 || completed == 0 {
		t.Fatalf("the summary line reads P:%d A:%d C:%d; none may be zero for the comparison "+
			"to discriminate", pending, inProgress, completed)
	}

	columns := memberBoardColumns(t, body)
	wantCounts := []struct {
		label string
		value int
	}{
		{"P", pending}, {"A", inProgress}, {"C", completed},
	}

	sum := 0
	for i, column := range columns {
		heading, count := columnHeader(t, column)
		if count != wantCounts[i].value {
			t.Errorf("the %s column's badge reads %d and the summary line's %s reads %d; the "+
				"board and the line must group the sprint's tasks by the same categorisation",
				heading, count, wantCounts[i].label, wantCounts[i].value)
		}
		// The badge states what the column actually holds, not a number carried
		// beside it: a count that disagreed with the cards would be false about
		// the very thing it counts.
		if cards := strings.Count(column, cardOpen); cards != count {
			t.Errorf("the %s column's badge reads %d but the column holds %d cards",
				heading, count, cards)
		}
		sum += count
	}
	if sum != total {
		t.Errorf("the three column badges sum to %d and the summary line's T reads %d", sum, total)
	}
	if cards := strings.Count(memberBoardRegion(t, body), cardOpen); cards != total {
		t.Errorf("the board renders %d cards and the summary line's T reads %d", cards, total)
	}
}

// ==================== ORDER WITHIN A COLUMN ====================

// sprintOrderFixture names the twelve member tasks seedSprintOrderFixture
// created — four in each of the board's three columns — so the ordering
// assertions bind the ids that actually exist rather than assuming an
// autoincrement sequence.
//
// Four per column is the smallest set that can carry every case the ordering
// rule states at once: two cards separated by their timestamp, two carrying the
// SAME timestamp (the tie), and one carrying none at all (SPEC/WEB.md § Sprint
// Detail Sub-Template, rule 4, The tiebreaker is the plan; Acceptance Criterion
// 132).
//
// The fields are grouped by column and declared in ID order within each group,
// which is one of the three orders the test plays off against the other two.
type sprintOrderFixture struct {
	name     string
	sprintID int

	// WAITING (positions 4, 10, 1 and 7). backfillLedger is the one member
	// returned to BACKLOG inside the sprint; the other three are SPRINT, and
	// the column holds both statuses.
	publishRules      int // SPRINT,  position 4
	backfillLedger    int // BACKLOG, position 10
	chartOfAccounts   int // SPRINT,  position 1
	reconcileBalances int // SPRINT,  position 7

	// DOING (positions 8, 5, 11 and 2), ordered by started_at descending.
	freezeLegacy       int // DOING,   position 8,  started_at ABSENT
	postingEngine      int // DOING,   position 5,  started_at 2026-03-18T14:05
	validateMarchClose int // TESTING, position 11, started_at 2026-03-17T09:30 (tie)
	portPayoutLedger   int // TESTING, position 2,  started_at 2026-03-17T09:30 (tie)

	// CLOSED (positions 3, 9, 12 and 6), ordered by closed_at descending.
	agreeCutover        int // position 3,  closed_at ABSENT
	shipSchemaMigration int // position 9,  closed_at 2026-03-25T17:45
	balanceAssertions   int // position 12, closed_at 2026-03-24T10:00 (tie)
	documentInvariants  int // position 6,  closed_at 2026-03-24T10:00 (tie)
}

// path is the route the board is rendered on.
func (f *sprintOrderFixture) path() string {
	return "/roadmaps/" + f.name + "/sprints/" + itoa(f.sprintID)
}

// wantColumns is the order the three columns must render in, per column, before
// the sprint is reordered.
//
//   - WAITING follows the sprint_tasks position order: 1, 4, 7, 10.
//   - DOING follows started_at descending: the 18th, then the two cards tied on
//     the 17th in position order (2 before 11), then the card carrying no
//     started_at at all, which sorts last however early its position is.
//   - CLOSED follows closed_at descending, on the same three rules: the 25th,
//     then the two tied on the 24th in position order (6 before 12), then the
//     card carrying no closed_at.
func (f *sprintOrderFixture) wantColumns() [][]int {
	return [][]int{
		{f.chartOfAccounts, f.publishRules, f.reconcileBalances, f.backfillLedger},
		{f.postingEngine, f.portPayoutLedger, f.validateMarchClose, f.freezeLegacy},
		{f.shipSchemaMigration, f.documentInvariants, f.balanceAssertions, f.agreeCutover},
	}
}

// positionOrder is the order the three columns would render in if the board
// ordered ALL of them by sprint_tasks position — the board this one replaced.
//
// It is the control the ordering assertions are read against: for WAITING it is
// the specified order, and for DOING and CLOSED it must differ from it, or an
// assertion could pass on a board that never learnt the difference.
func (f *sprintOrderFixture) positionOrder() [][]int {
	return [][]int{
		{f.chartOfAccounts, f.publishRules, f.reconcileBalances, f.backfillLedger},
		{f.portPayoutLedger, f.postingEngine, f.freezeLegacy, f.validateMarchClose},
		{f.agreeCutover, f.documentInvariants, f.shipSchemaMigration, f.balanceAssertions},
	}
}

// idOrder is the order the three columns would render in if the board fell back
// to the task id — the order SQLite hands back rows in when an ORDER BY is lost.
func (f *sprintOrderFixture) idOrder() [][]int {
	return [][]int{
		{f.publishRules, f.backfillLedger, f.chartOfAccounts, f.reconcileBalances},
		{f.freezeLegacy, f.postingEngine, f.validateMarchClose, f.portPayoutLedger},
		{f.agreeCutover, f.shipSchemaMigration, f.balanceAssertions, f.documentInvariants},
	}
}

// reordered is the sprint's task order after the reorder half of the test, in
// the order `rmp sprint reorder` would be given the ids.
//
// It reverses the WAITING column and moves every DOING and CLOSED card to a new
// position, so a board ordering all three columns by position would render all
// three differently afterwards. What it does NOT change is the relative position
// of the two tied cards in each of those columns: the tie IS broken by position,
// so reversing a tied pair would legitimately reorder its column and the "DOING
// and CLOSED are unchanged" half of the assertion would be asserting the wrong
// thing.
func (f *sprintOrderFixture) reordered() []int {
	return []int{
		f.backfillLedger, f.freezeLegacy, f.documentInvariants,
		f.reconcileBalances, f.portPayoutLedger, f.balanceAssertions,
		f.publishRules, f.postingEngine, f.agreeCutover,
		f.chartOfAccounts, f.validateMarchClose, f.shipSchemaMigration,
	}
}

// wantColumnsAfterReorder is the board the reordered sprint must render: a
// WAITING column following the NEW position order, and a DOING and a CLOSED
// column identical to what they were, because neither is ordered by position.
func (f *sprintOrderFixture) wantColumnsAfterReorder() [][]int {
	return [][]int{
		{f.backfillLedger, f.reconcileBalances, f.publishRules, f.chartOfAccounts},
		{f.postingEngine, f.portPayoutLedger, f.validateMarchClose, f.freezeLegacy},
		{f.shipSchemaMigration, f.documentInvariants, f.balanceAssertions, f.agreeCutover},
	}
}

// positionOrderAfterReorder is what the three columns would render in after the
// reorder if the board ordered all of them by position. Every one of the three
// differs from the corresponding entry of positionOrder, which is what makes
// "DOING and CLOSED did not move" a claim about the board's ordering rule rather
// than a claim about the reorder having done nothing.
func (f *sprintOrderFixture) positionOrderAfterReorder() [][]int {
	return [][]int{
		{f.backfillLedger, f.reconcileBalances, f.publishRules, f.chartOfAccounts},
		{f.freezeLegacy, f.portPayoutLedger, f.postingEngine, f.validateMarchClose},
		{f.documentInvariants, f.balanceAssertions, f.agreeCutover, f.shipSchemaMigration},
	}
}

// The ordering timestamps the fixture installs. They are written out here, once,
// so the expected orders above can be read against them.
//
// The format is utils.ISO8601Format (YYYY-MM-DDTHH:mm:ss.sssZ), which is what
// every production write of these columns produces: the board compares the
// stored strings, and a fixture writing some other spelling would be testing a
// value the application cannot store.
const (
	orderStartedPostingEngine = "2026-03-18T14:05:00.000Z"
	orderStartedTiedPair      = "2026-03-17T09:30:00.000Z" // shared by two cards
	orderTestedValidate       = "2026-03-21T08:00:00.000Z" // the LATEST tested_at
	orderTestedPortPayout     = "2026-03-20T11:00:00.000Z"
	orderClosedShipMigration  = "2026-03-25T17:45:00.000Z"
	orderClosedTiedPair       = "2026-03-24T10:00:00.000Z" // shared by two cards
)

// The three UPDATE statements the fixture uses to install a controlled lifecycle
// timestamp. They are constants with no interpolation of any kind: the column is
// part of the literal and the value and the id are bound, so the helper below
// can set exactly these three columns and nothing else.
const (
	sqlSetTaskStartedAt = "UPDATE tasks SET started_at = ? WHERE id = ?"
	sqlSetTaskTestedAt  = "UPDATE tasks SET tested_at = ? WHERE id = ?"
	sqlSetTaskClosedAt  = "UPDATE tasks SET closed_at = ? WHERE id = ?"
)

// setTaskLifecycleTimestamp writes one task lifecycle timestamp directly into
// the fixture database, through the *sql.DB the roadmap database embeds. A nil
// value writes SQL NULL.
//
// THIS IS A FIXTURE-ONLY WRITE, and it is deliberate. started_at, tested_at and
// closed_at are set by the task state machine and never by a caller: every
// production write of them stamps utils.NowISO8601 (SPEC/STATE_MACHINE.md § Date
// Tracking Fields), so driving the tasks through `task stat` alone would give the
// test whatever instants the clock happened to produce — it could not choose
// which card is the most recent, could not make the timestamp order differ from
// the position order and the id order on purpose, and could not produce the two
// cases the ordering rule is explicitly written for: two cards carrying the SAME
// instant and a card carrying NONE.
//
// So the fixture drives every task through the production status path first,
// which is what puts the tasks in their statuses and stamps them, and only then
// rewrites the stamped values to the ones the assertions are written against.
// Nothing in the production code writes a timestamp this way; the same technique
// already seeds the sprint closed_at values in sprint_test.go (setClosed).
//
// The NULL cases are the deliberate part of the same argument. MODELS.md § Task
// makes all three fields nullable and SPEC/WEB.md states where a card carrying no
// ordering timestamp sorts, so the rule has a case that the state machine's own
// transitions cannot reach and that must still be covered.
func setTaskLifecycleTimestamp(t *testing.T, database *db.DB, statement string, id int, value *string) {
	t.Helper()

	if _, err := database.ExecContext(context.Background(), statement, value, id); err != nil {
		t.Fatalf("setting the lifecycle timestamp of task %d: %v", id, err)
	}
}

// seedSprintOrderFixture builds a roadmap holding one OPEN sprint with twelve
// member tasks, four in each board column, seeded so that the three candidate
// orders of every column differ from one another:
//
//	created in one order       -> fixes the id order
//	added to the sprint in a second order -> fixes the sprint_tasks position order
//	stamped in a third order   -> fixes the started_at / closed_at order
//
// No assertion of the ordering test can therefore pass on an order that merely
// coincides with the specified one (Acceptance Criterion 132), and each column's
// specified order is checked against both alternatives explicitly.
//
// The DOING column additionally carries two TESTING cards whose tested_at values
// are the LATEST timestamps in the whole fixture and rank the two differently
// from started_at, so a board that ordered a TESTING card by tested_at renders a
// different column and fails.
func seedSprintOrderFixture(t *testing.T, name string) sprintOrderFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	f := sprintOrderFixture{name: name}

	newTask := func(title, created string, priority, severity int, taskType models.TaskType) int {
		t.Helper()
		id, cerr := database.CreateTask(ctx, &models.Task{
			Title:                  title,
			Type:                   taskType,
			Status:                 models.StatusBacklog,
			Priority:               priority,
			Severity:               severity,
			FunctionalRequirements: "Every ledger movement must be expressed as a balanced double-entry posting.",
			TechnicalRequirements:  "Implemented against the posting engine; the legacy writer stays read-only.",
			AcceptanceCriteria:     "The March close reconciles to zero against the double-entry postings.",
			CreatedAt:              created,
		})
		if cerr != nil {
			t.Fatalf("creating task %q: %v", title, cerr)
		}
		return id
	}

	// Creation order fixes the id order, and it interleaves the three columns so
	// that no column's id order can coincide with its position order by accident.
	f.publishRules = newTask("Publish the double-entry posting rules to the finance team",
		"2026-02-02T09:00:00Z", 9, 3, models.TypeTask)
	f.freezeLegacy = newTask("Freeze the legacy single-entry write path",
		"2026-02-03T09:00:00Z", 8, 7, models.TypeChore)
	f.agreeCutover = newTask("Agree the double-entry migration cutover window",
		"2026-02-04T09:00:00Z", 6, 2, models.TypeTask)
	f.backfillLedger = newTask("Backfill the historical ledger into double-entry postings",
		"2026-02-05T09:00:00Z", 6, 8, models.TypeUserStory)
	f.postingEngine = newTask("Write the double-entry posting engine",
		"2026-02-06T09:00:00Z", 3, 9, models.TypeUserStory)
	f.shipSchemaMigration = newTask("Ship the posting-engine schema migration",
		"2026-02-07T09:00:00Z", 2, 4, models.TypeTask)
	f.chartOfAccounts = newTask("Model the chart of accounts for the double-entry ledger",
		"2026-02-08T09:00:00Z", 4, 5, models.TypeUserStory)
	f.validateMarchClose = newTask("Validate the posting engine against the March close",
		"2026-02-09T09:00:00Z", 5, 6, models.TypeBug)
	f.balanceAssertions = newTask("Instrument the posting engine with balance assertions",
		"2026-02-10T09:00:00Z", 9, 1, models.TypeChore)
	f.reconcileBalances = newTask("Reconcile the legacy balance sheet against the new postings",
		"2026-02-11T09:00:00Z", 1, 8, models.TypeTask)
	f.portPayoutLedger = newTask("Port the payout ledger to the posting engine",
		"2026-02-12T09:00:00Z", 7, 3, models.TypeUserStory)
	f.documentInvariants = newTask("Document the double-entry posting invariants",
		"2026-02-13T09:00:00Z", 4, 2, models.TypeChore)

	f.sprintID = newSprint(t, database, "Migrate the ledger to double-entry postings",
		"Replace the single-entry ledger with balanced postings, cutover included.")

	// Membership in POSITION order, which AddTasksToSprint assigns sequentially.
	// It matches no column's id order and, in DOING and CLOSED, no column's
	// timestamp order either.
	members := []int{
		f.chartOfAccounts, f.portPayoutLedger, f.agreeCutover, f.publishRules,
		f.postingEngine, f.documentInvariants, f.reconcileBalances, f.freezeLegacy,
		f.shipSchemaMigration, f.backfillLedger, f.validateMarchClose, f.balanceAssertions,
	}
	if aerr := database.AddTasksToSprint(ctx, f.sprintID, members); aerr != nil {
		t.Fatalf("adding the member tasks to the sprint: %v", aerr)
	}
	if serr := database.UpdateSprintStatus(ctx, f.sprintID, models.SprintOpen); serr != nil {
		t.Fatalf("opening the sprint: %v", serr)
	}

	// The statuses, set through the production write path and in the order the
	// state machine allows: SPRINT -> DOING -> TESTING -> COMPLETED. Membership
	// already put every member in SPRINT, so the WAITING column needs no move
	// beyond the single task returned to BACKLOG inside the sprint.
	statusMoves := []struct {
		status models.TaskStatus
		ids    []int
	}{
		{models.StatusBacklog, []int{f.backfillLedger}},
		{models.StatusDoing, []int{
			f.freezeLegacy, f.postingEngine, f.validateMarchClose, f.portPayoutLedger,
			f.agreeCutover, f.shipSchemaMigration, f.balanceAssertions, f.documentInvariants,
		}},
		{models.StatusTesting, []int{
			f.validateMarchClose, f.portPayoutLedger,
			f.agreeCutover, f.shipSchemaMigration, f.balanceAssertions, f.documentInvariants,
		}},
		{models.StatusCompleted, []int{
			f.agreeCutover, f.shipSchemaMigration, f.balanceAssertions, f.documentInvariants,
		}},
	}
	for _, move := range statusMoves {
		if uerr := database.UpdateTaskStatus(ctx, move.ids, move.status); uerr != nil {
			t.Fatalf("moving %v to %s: %v", move.ids, move.status, uerr)
		}
	}

	// The controlled ordering timestamps, replacing the "now" the transitions
	// above stamped. See setTaskLifecycleTimestamp for why the fixture writes
	// these directly instead of letting the clock choose them.
	startedPostingEngine := orderStartedPostingEngine
	startedTied := orderStartedTiedPair
	testedValidate := orderTestedValidate
	testedPortPayout := orderTestedPortPayout
	closedShipMigration := orderClosedShipMigration
	closedTied := orderClosedTiedPair

	for _, stamp := range []struct {
		statement string
		value     *string
		id        int
	}{
		// DOING column, by started_at descending.
		{sqlSetTaskStartedAt, &startedPostingEngine, f.postingEngine},
		{sqlSetTaskStartedAt, &startedTied, f.validateMarchClose},
		{sqlSetTaskStartedAt, &startedTied, f.portPayoutLedger},
		{sqlSetTaskStartedAt, nil, f.freezeLegacy},
		// tested_at, which must order nothing: the two TESTING cards carry the
		// latest timestamps in the fixture, and they rank the pair the other way
		// round from started_at plus the position tiebreak.
		{sqlSetTaskTestedAt, &testedValidate, f.validateMarchClose},
		{sqlSetTaskTestedAt, &testedPortPayout, f.portPayoutLedger},
		// CLOSED column, by closed_at descending.
		{sqlSetTaskClosedAt, &closedShipMigration, f.shipSchemaMigration},
		{sqlSetTaskClosedAt, &closedTied, f.balanceAssertions},
		{sqlSetTaskClosedAt, &closedTied, f.documentInvariants},
		{sqlSetTaskClosedAt, nil, f.agreeCutover},
	} {
		setTaskLifecycleTimestamp(t, database, stamp.statement, stamp.id, stamp.value)
	}

	return f
}

// assertBoardOrder checks the three columns of the rendered board against the
// order each must be in, naming the column in every failure.
func assertBoardOrder(t *testing.T, body string, want [][]int, when string) {
	t.Helper()

	columns := memberBoardColumns(t, body)
	for i, column := range columns {
		heading, _ := columnHeader(t, column)
		got := memberCardIDs(t, column)
		if !equalIDs(got, want[i]) {
			t.Errorf("%s, the %s column renders the cards %v, want %v",
				when, heading, got, want[i])
		}
	}
}

// assertOrdersDiffer fails the test when a column's specified order coincides
// with one of the orders a defective board would produce, which would make the
// corresponding assertion vacuous.
func assertOrdersDiffer(t *testing.T, want, alternative [][]int, columns []string, what string) {
	t.Helper()

	for i := range want {
		if equalIDs(want[i], alternative[i]) {
			t.Fatalf("the fixture's %s column order %v is also its %s; the ordering "+
				"assertion would pass on a board that used the latter",
				columns[i], want[i], what)
		}
	}
}

// sprintOrderColumnHeadings names the three columns for the failure messages of
// the helpers above, left to right.
var sprintOrderColumnHeadings = []string{"WAITING", "DOING", "CLOSED"}

// TestSprintBoard_EachColumnOrdersByItsOwnKey is the gate for Acceptance
// Criterion 132: the three columns of the sprint's member-tasks board do NOT
// share one order. WAITING follows the sprint_tasks position order the page
// reads, DOING follows started_at descending, and CLOSED follows closed_at
// descending; where two cards of a column carry the same ordering timestamp, and
// where a card carries none, the cards fall back to position ascending, and a
// card carrying no timestamp sorts last in its column.
//
// The test asserts ALL THREE orders, not just the one that changed, and it
// asserts BOTH HALVES of the split the criterion names: reordering the sprint
// through the production write path reorders the WAITING column AND leaves the
// DOING and CLOSED columns exactly as they were. Each half on its own is
// satisfied by a board that got the rule wrong — a board ordering all three
// columns by position satisfies the first, a board ordering all three by recency
// satisfies the second — so neither half is evidence without the other.
//
// Every assertion is guarded against coincidence. The fixture's position order,
// timestamp order and id order differ from one another in every column, and the
// guards below state that explicitly, so an assertion cannot pass on an order
// that merely happens to look like the specified one.
func TestSprintBoard_EachColumnOrdersByItsOwnKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintOrderFixture(t, "ledger-migration")
	mux := buildMux()

	// The controls first: if the fixture ever loses the property that its three
	// candidate orders differ, the assertions below stop discriminating and the
	// test must say so rather than pass.
	assertOrdersDiffer(t, f.wantColumns()[1:], f.positionOrder()[1:],
		sprintOrderColumnHeadings[1:], "sprint_tasks position order")
	assertOrdersDiffer(t, f.wantColumns(), f.idOrder(),
		sprintOrderColumnHeadings, "task id order")

	body := servePage(t, mux, f.path())
	assertBoardOrder(t, body, f.wantColumns(), "as the sprint was planned")

	columns := memberBoardColumns(t, body)
	doing, closed := memberCardIDs(t, columns[1]), memberCardIDs(t, columns[2])

	// THE TIE. Two cards of the DOING column entered DOING at the same instant
	// and two cards of the CLOSED column were closed at the same instant, which
	// is the ordinary case rather than a contrived one: a bulk `rmp task stat`
	// stamps a whole batch alike. Each tied pair falls back to the sprint_tasks
	// position, ascending — and in both pairs the id order is the REVERSE of the
	// position order, so a board tiebreaking on the id fails here.
	assertCardBefore(t, doing, f.portPayoutLedger, f.validateMarchClose,
		"the DOING column's two cards tied on started_at fall back to position ascending")
	assertCardBefore(t, closed, f.documentInvariants, f.balanceAssertions,
		"the CLOSED column's two cards tied on closed_at fall back to position ascending")

	// THE ABSENT TIMESTAMP. A card that states no time sorts last in its column,
	// after every card that states one — and it does so despite holding a
	// position that is not last, so "last" is the rule at work and not the
	// position order showing through.
	assertCardLast(t, doing, f.freezeLegacy, "a DOING card carrying no started_at")
	assertCardLast(t, closed, f.agreeCutover, "a CLOSED card carrying no closed_at")
	if f.positionOrder()[1][len(f.positionOrder()[1])-1] == f.freezeLegacy {
		t.Fatalf("the fixture's card with no started_at is also last by position; the " +
			"assertion would pass on a board that never applied the rule")
	}
	if f.positionOrder()[2][len(f.positionOrder()[2])-1] == f.agreeCutover {
		t.Fatalf("the fixture's card with no closed_at is also last by position; the " +
			"assertion would pass on a board that never applied the rule")
	}

	// tested_at ORDERS NOTHING. The DOING column groups DOING and TESTING and
	// started_at orders both: a TESTING card takes its place from when its task
	// entered DOING. The two TESTING cards carry the fixture's LATEST timestamps
	// in tested_at, so a board that ordered a TESTING card by tested_at would put
	// one of them at the head of the column instead of the DOING card that is
	// there.
	if doing[0] != f.postingEngine {
		t.Errorf("the DOING column is headed by task #%d, want the most recently STARTED "+
			"task #%d; a TESTING card takes its place from started_at and never from "+
			"tested_at", doing[0], f.postingEngine)
	}

	// THE SPLIT. Reordering the sprint through the production write path moves
	// every card to a new position, in all three columns.
	reorderSprintTasks(t, f.name, f.sprintID, f.reordered())

	// The reorder must actually change what a position-ordered board would show
	// in DOING and CLOSED, or "those two columns did not move" is a claim about
	// the reorder rather than about the board.
	assertOrdersDiffer(t, f.positionOrderAfterReorder()[1:], f.positionOrder()[1:],
		sprintOrderColumnHeadings[1:], "position order before the reorder")
	assertOrdersDiffer(t, f.wantColumnsAfterReorder()[:1], f.wantColumns()[:1],
		sprintOrderColumnHeadings[:1], "order before the reorder")

	assertBoardOrder(t, servePage(t, mux, f.path()), f.wantColumnsAfterReorder(),
		"after reordering the sprint")
}

// assertCardBefore fails the test unless the card of task `first` appears above
// the card of task `second` in the column's rendered order.
func assertCardBefore(t *testing.T, column []int, first, second int, why string) {
	t.Helper()

	at, other := indexOfID(column, first), indexOfID(column, second)
	if at < 0 || other < 0 {
		t.Fatalf("%s: the column %v does not hold both task #%d and task #%d",
			why, column, first, second)
	}
	if at > other {
		t.Errorf("%s: task #%d is rendered at %d, below task #%d at %d",
			why, first, at, second, other)
	}
}

// assertCardLast fails the test unless the card of task `id` is the last of the
// column.
func assertCardLast(t *testing.T, column []int, id int, what string) {
	t.Helper()

	at := indexOfID(column, id)
	if at < 0 {
		t.Fatalf("%s (task #%d) is not in the column %v at all", what, id, column)
	}
	if at != len(column)-1 {
		t.Errorf("%s (task #%d) is rendered at %d of %d, want last: a card whose ordering "+
			"timestamp is absent sorts after every card that carries one",
			what, id, at, len(column))
	}
}

// indexOfID returns the position of a task id in a column's rendered order, or
// -1 when the column does not hold it.
func indexOfID(column []int, id int) int {
	for i := range column {
		if column[i] == id {
			return i
		}
	}
	return -1
}

// sprintTieCutoverServices names the fifteen services the tie fixture's member
// tasks cut over, one task each. Fifteen in-progress tasks in one sprint is an
// ordinary size rather than a stress case: SPEC/WEB.md's own worked example of a
// sprint reads `33% - P:8 A:29 C:18 - T:55`.
var sprintTieCutoverServices = [...]string{
	"card authorisation", "refunds", "chargebacks", "payouts", "settlement import",
	"fee calculation", "invoicing", "dunning", "tax engine", "currency conversion",
	"wallet top-up", "direct debit", "payment links", "subscription billing",
	"fraud scoring",
}

// sprintTieMemberOrder is the order the fifteen tasks are added to the sprint,
// as indices into sprintTieCutoverServices — so it is the sprint_tasks position
// order, and it is a permutation of the creation order rather than a shuffle
// computed at run time, because a fixture whose data changes per run cannot be
// read against a failure message.
var sprintTieMemberOrder = [...]int{9, 2, 14, 5, 0, 11, 7, 3, 13, 1, 8, 12, 4, 10, 6}

// sprintTieBatches groups the same fifteen tasks into the three bulk status
// changes that move them into DOING, most recent batch first. Every task of a
// batch carries the batch's started_at, so the column holds three groups of five
// cards tied on their ordering timestamp.
var sprintTieBatches = [...][5]int{
	{0, 4, 7, 11, 13},
	{1, 3, 8, 10, 14},
	{2, 5, 6, 9, 12},
}

// sprintTieStartedAt is the started_at each batch carries, in the same order,
// descending. The values are fixed rather than left to the clock so the expected
// column order is the same on every run.
var sprintTieStartedAt = [...]string{
	"2026-04-10T09:15:00.000Z",
	"2026-04-09T16:40:00.000Z",
	"2026-04-08T11:05:00.000Z",
}

// TestSprintBoard_TiedCardsKeepThePlannedOrderAtColumnScale pins the ONE
// property the tiebreaker rests on: the sort that orders a column by its
// timestamp is STABLE, so the cards the timestamp does not separate come out in
// the sprint_tasks position order the read delivered them in (SPEC/WEB.md
// § Sprint Detail Sub-Template, rule 4, The tiebreaker is the plan; Acceptance
// Criterion 132).
//
// It exists because that property is INVISIBLE in a small column. Go's
// sort.Slice sorts a short slice by insertion, which happens to be stable, so a
// column of four cards renders identically whether the board sorts stably or not
// and a test built on one cannot tell the two apart. Above the insertion-sort
// threshold the unstable sort reorders equal elements, and the difference becomes
// observable — which is exactly when a real sprint would notice it, because the
// case is ordinary: a bulk `rmp task stat` moves a batch of tasks in one
// operation and stamps every one of them alike (SPEC/COMMANDS.md § Change Status
// (stat)).
//
// The fixture therefore builds a DOING column of fifteen cards in three bulk
// batches of five, each batch tied on its own started_at. The expected order is
// derived from the fixture's own data by the rule the specification states —
// batch by batch, most recent first, and inside a batch by position ascending —
// and is checked against both the plain position order and the plain id order, so
// it cannot pass on either.
func TestSprintBoard_TiedCardsKeepThePlannedOrderAtColumnScale(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name, sprintID, ids := seedSprintTieFixture(t, "payments-cutover")
	mux := buildMux()

	// Position of each task in the sprint plan, keyed by the index the fixture
	// names it by.
	position := make(map[int]int, len(sprintTieMemberOrder))
	for pos, created := range sprintTieMemberOrder {
		position[created] = pos
	}

	// The expected column: the batches in started_at order, each batch's cards in
	// position order. This walks the fixture's declared data by the rule the
	// specification states; it does not consult the board.
	want := make([]int, 0, len(sprintTieCutoverServices))
	for _, batch := range sprintTieBatches {
		ordered := append([]int(nil), batch[:]...)
		sort.Slice(ordered, func(i, j int) bool { return position[ordered[i]] < position[ordered[j]] })
		for _, created := range ordered {
			want = append(want, ids[created])
		}
	}

	// The controls. Neither the plain position order nor the plain id order may
	// coincide with it, or the assertion below would hold on a board that ordered
	// the column by one of them and never sorted at all.
	byPosition := make([]int, 0, len(sprintTieMemberOrder))
	for _, created := range sprintTieMemberOrder {
		byPosition = append(byPosition, ids[created])
	}
	byID := append([]int(nil), ids...)
	sort.Ints(byID)
	if equalIDs(want, byPosition) {
		t.Fatalf("the fixture's expected order %v is also its position order", want)
	}
	if equalIDs(want, byID) {
		t.Fatalf("the fixture's expected order %v is also its id order", want)
	}

	columns := memberBoardColumns(t, servePage(t, mux,
		"/roadmaps/"+name+"/sprints/"+itoa(sprintID)))
	got := memberCardIDs(t, columns[1])
	if !equalIDs(got, want) {
		t.Errorf("the DOING column renders the cards\n got %v\nwant %v\n"+
			"cards tied on started_at must come out in sprint_tasks position order, which "+
			"they only do if the column's sort is stable", got, want)
	}
}

// seedSprintTieFixture builds a roadmap holding one OPEN sprint whose DOING
// column carries fifteen cards in three bulk-stamped batches of five, and returns
// the roadmap name, the sprint id, and the task ids in creation order.
//
// The batches are moved through the PRODUCTION status path, one bulk
// UpdateTaskStatus per batch, which is the shape `rmp task stat <id,id,...>
// DOING` produces and the reason equal timestamps are ordinary. Their stamped
// values are then replaced with the fixed ones in sprintTieStartedAt, so the
// expected order does not depend on how far apart the clock happened to place two
// consecutive batches; see setTaskLifecycleTimestamp for why a fixture writes
// these directly.
func seedSprintTieFixture(t *testing.T, name string) (string, int, []int) {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	ids := make([]int, 0, len(sprintTieCutoverServices))
	for i, service := range sprintTieCutoverServices {
		id, cerr := database.CreateTask(ctx, &models.Task{
			Title:                  "Cut the " + service + " service over to the shared payment ledger",
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			Priority:               (i % 9) + 1,
			Severity:               (i % 7) + 1,
			FunctionalRequirements: "The service must post to the shared ledger without a dual-write window.",
			TechnicalRequirements:  "Move the service's writes behind the ledger client and retire its own store.",
			AcceptanceCriteria:     "The service's postings reconcile against the shared ledger for a full day.",
			CreatedAt:              "2026-04-0" + itoa((i%9)+1) + "T09:00:00Z",
		})
		if cerr != nil {
			t.Fatalf("creating the %s cutover task: %v", service, cerr)
		}
		ids = append(ids, id)
	}

	sprintID := newSprint(t, database, "Cut the payment services over to the shared ledger",
		"Retire every service-local ledger in favour of the shared double-entry ledger.")

	members := make([]int, 0, len(sprintTieMemberOrder))
	for _, created := range sprintTieMemberOrder {
		members = append(members, ids[created])
	}
	if aerr := database.AddTasksToSprint(ctx, sprintID, members); aerr != nil {
		t.Fatalf("adding the member tasks to the sprint: %v", aerr)
	}
	if serr := database.UpdateSprintStatus(ctx, sprintID, models.SprintOpen); serr != nil {
		t.Fatalf("opening the sprint: %v", serr)
	}

	for b := range sprintTieBatches {
		batch := make([]int, 0, len(sprintTieBatches[b]))
		for _, created := range sprintTieBatches[b] {
			batch = append(batch, ids[created])
		}
		if uerr := database.UpdateTaskStatus(ctx, batch, models.StatusDoing); uerr != nil {
			t.Fatalf("moving the batch %v to DOING: %v", batch, uerr)
		}
		started := sprintTieStartedAt[b]
		for _, id := range batch {
			setTaskLifecycleTimestamp(t, database, sqlSetTaskStartedAt, id, &started)
		}
	}

	return name, sprintID, ids
}

// reorderSprintTasks sets the sprint's task order through the production write
// path, so the value under test travels the same route `rmp sprint reorder`
// travels.
func reorderSprintTasks(t *testing.T, roadmap string, sprintID int, taskIDs []int) {
	t.Helper()

	database, err := db.Open(roadmap)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", roadmap, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	if rerr := database.ReorderSprintTasks(sprintID, taskIDs); rerr != nil {
		t.Fatalf("reordering the tasks of sprint %d: %v", sprintID, rerr)
	}
}

// ==================== THE CARD ====================

// TestSprintBoard_CardShowsSixDataPointsInOrder is the gate for Acceptance
// Criterion 133: the card shows exactly six data points, on THREE lines, in this
// order — the title leading the card, the reference `#<id>` on its own line as
// secondary text, and one line carrying the priority badge and the severity badge
// at its leading edge and the number of comments followed by the number of
// subtasks at its trailing edge, each counter as its icon followed by its number.
//
// The COUNTER ORDER is asserted explicitly, and the criterion requires that: a
// card showing the subtask count before the comment count satisfies every other
// clause, so an order left implicit is an order the template is free to flip. It
// is also the order the tasks board's footer does NOT use, which is why the two
// are stated separately (SPEC/WEB.md § Sprint Detail Sub-Template, The counter
// order differs from the tasks board's too).
//
// The badge classes are taken FROM the semantic mapping (priorityBadge and
// severityBadge) rather than written out here, so this test states that the card
// is wired to the mapping and leaves the mapping itself to the test that pins it
// (Acceptance Criterion 61). The card's priority and severity fall in DIFFERENT
// bands, so the two badges carry different classes and a card that read one field
// for both, or swapped them, fails here.
//
// Each badge writes its value behind the one-letter prefix that names it — P9 and
// S2 — exactly as the tasks board's card does, because the rule is stated once for
// the card of both boards (SPEC/WEB.md § Roadmap Tasks Page, Card content, item 3;
// Acceptance Criteria 85 and 133). The prefix is a label and not a value: the
// class each badge carries is still the one the mapping assigns to the integer
// alone, which is why the classes below are still read from the helpers.
func TestSprintBoard_CardShowsSixDataPointsInOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	columns := memberBoardColumns(t, servePage(t, mux, f.path()))
	card := cardSlice(t, columns[0], f.reconcile) // the WAITING column's fullest card

	// 1. The title, leading the card as its prominent main content.
	title := `<span class="d-block fw-bold text-break" data-role="task-card-title">` +
		sprintTaskReconcile + `</span>`
	// 2. The reference, on its own line as secondary muted text, carrying the id
	//    and nothing else.
	ref := `<span class="d-block small text-secondary mb-1" data-role="task-card-ref">#` +
		itoa(f.reconcile) + `</span>`
	// 3 and 4. The two badges, at the LEADING edge of the card's third line: the
	//          prefixed value, in the variant the semantic mapping assigns to it.
	priority := `<span class="badge ` + priorityBadge(9) + `">P9</span>`
	severity := `<span class="badge ` + severityBadge(2) + `">S2</span>`
	// 5 and 6. The counters, at the TRAILING edge of that same line, each an icon
	//          followed by its number, and the COMMENT count first.
	comments := counterMarkup("task-card-comments", "ti ti-message", 3)
	subtasks := counterMarkup("task-card-subtasks", "ti ti-subtask", 2)

	if priorityBadge(9) == severityBadge(2) {
		t.Fatalf("the fixture's priority and severity fall in the same band (%s); a card that "+
			"read one field for both would pass", priorityBadge(9))
	}

	// Present, and in that order. The indices are taken from the card slice, so
	// the order asserted is the order the reader meets.
	ordered := []struct {
		what   string
		markup string
	}{
		{"title", title},
		{"reference", ref},
		{"priority badge", priority},
		{"severity badge", severity},
		{"comment counter", comments},
		{"subtask counter", subtasks},
	}
	previous := -1
	for _, item := range ordered {
		at := strings.Index(card, item.markup)
		if at < 0 {
			t.Fatalf("the card does not show its %s as %q\ncard: %s", item.what, item.markup, card)
		}
		if at <= previous {
			t.Errorf("the card's %s is out of order (at %d, after %d)\ncard: %s",
				item.what, at, previous, card)
		}
		previous = at
	}

	// A badge carrying the bare integer does not satisfy Acceptance Criterion 133,
	// so the unprefixed form is asserted ABSENT rather than left unasserted: a card
	// rendering both forms would otherwise pass the presence checks above.
	for _, unprefixed := range []string{
		`<span class="badge ` + priorityBadge(9) + `">9</span>`,
		`<span class="badge ` + severityBadge(2) + `">2</span>`,
	} {
		if strings.Contains(card, unprefixed) {
			t.Errorf("the card renders %s; the priority and severity badges name the value they "+
				"carry with a one-letter prefix, exactly as the tasks board's card does "+
				"(Acceptance Criteria 85 and 133)\ncard: %s", unprefixed, card)
		}
	}

	// The four values above sit on ONE line, not on two: the badges and the
	// counters are both inside the card's third line, which is what makes the
	// order asserted above an order WITHIN a line rather than an order of lines.
	// The line's own layout — trailing edge, wrapping, no separate footer — is the
	// subject of TestSprintBoard_CardMergesBadgesAndCountersOntoOneLine.
	line := spanWithRole(t, card, "task-card-summary")
	if line == "" {
		t.Fatalf("the card renders no third line carrying both groups\ncard: %s", card)
	}
	for _, want := range []string{priority, severity, comments, subtasks} {
		if !strings.Contains(line, want) {
			t.Errorf("the card's third line does not carry %q; the badges and the counters "+
				"share one line (Acceptance Criterion 133)\nline: %s", want, line)
		}
	}

	// And nothing else. Each of these is a value the task HAS — so the assertion
	// is about the card omitting it, not about the roadmap lacking it — and each
	// is reached through the task detail modal the card opens.
	for what, absent := range map[string]string{
		"a status badge":       taskStatusBadge(models.StatusSprint),
		"the status value":     ">SPRINT<",
		"the task type":        string(models.TypeUserStory),
		"the specialists":      sprintBoardSpecialists,
		"a specialists icon":   "ti ti-users",
		"a depends-on count":   "Depends on:",
		"a blocks count":       "Blocks:",
		"a dependency icon":    "ti ti-link",
		"a blocked-tasks icon": "ti ti-lock",
		"a sprint indicator":   "ti ti-flag",
	} {
		if strings.Contains(card, absent) {
			t.Errorf("the card shows %s (%q); the column states the status and the modal the "+
				"card opens carries every field\ncard: %s", what, absent, card)
		}
	}

	// The controls that keep those absences from being vacuous: the task really
	// does carry the values the card omits.
	if !strings.Contains(servePage(t, mux, "/roadmaps/"+f.name+"/tasks"), sprintBoardSpecialists) {
		t.Errorf("the reconciliation task names no specialists at all, so asserting the sprint " +
			"card omits them proves nothing")
	}
	view := decodeTaskDetail(t, mux, f.name, f.reconcile)
	if len(view.Task.Blocks) == 0 && len(view.Task.DependsOn) == 0 {
		t.Errorf("the reconciliation task has no dependency edge at all, so asserting the card " +
			"omits the counts proves nothing")
	}
	if view.Task.Type != models.TypeUserStory {
		t.Errorf("the reconciliation task's type is %q, not the distinctive value the absence "+
			"assertion is written against", view.Task.Type)
	}
}

// TestSprintBoard_BothCountersAlwaysRender is the gate for Acceptance Criterion
// 134: the comment count and the subtask count are present on EVERY card of this
// board, including when either or both are `0`, so the trailing edge of the card's
// third line carries both numbers on every card the board renders.
//
// The subject is the card whose two counts are both zero, because that is the only
// card the criterion discriminates on: a card that has something to count renders
// the same markup whether the rule holds or not. The runbook task carries neither
// a subtask nor a comment, and the two mixed cards below keep the zero from being
// produced by a counter group that simply prints two zeros whatever the task
// holds.
//
// The counters are asserted as their whole INDICATOR MARKUP — the icon followed by
// the number, inside the element that names it — and not as the digit alone: a
// rendering that printed an icon with nothing beside it, or a bare `0` with no
// icon, would satisfy a check for the digit and state nothing to the reader.
func TestSprintBoard_BothCountersAlwaysRender(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	columns := memberBoardColumns(t, servePage(t, mux, f.path()))

	// A task with NEITHER counter still renders both, each showing 0: the runbook
	// task has no subtask and no comment.
	bare := cardSlice(t, columns[0], f.runbook)
	if !strings.Contains(bare, `data-role="task-card-counters"`) {
		t.Errorf("a member task with no subtask and no comment renders no counter group; both "+
			"counters close the third line of every card of this board (Acceptance "+
			"Criterion 134)\ncard: %s", bare)
	}
	for what, want := range map[string]string{
		"comment counter": counterMarkup("task-card-comments", "ti ti-message", 0),
		"subtask counter": counterMarkup("task-card-subtasks", "ti ti-subtask", 0),
	} {
		if !strings.Contains(bare, want) {
			t.Errorf("the card of a task with nothing to count does not render its %s as %q; a 0 "+
				"STATES that the task has none, where an absent counter leaves the reader "+
				"unable to tell \"no comments\" from \"this card does not show comments\""+
				"\ncard: %s", what, want, bare)
		}
	}
	// The zero is the counter's own text and nothing else stands in for it: no
	// dash, no placeholder, no word.
	for _, absent := range []string{"&mdash;", "None", "Subtasks:", "Comments:"} {
		if strings.Contains(bare, absent) {
			t.Errorf("a member task with no counter renders %q on its card; the counter shows the "+
				"number 0 and nothing else\ncard: %s", absent, bare)
		}
	}
	// The card itself is still a full card: what the criterion adds is the two
	// zeros, not a card reduced to them.
	if !strings.Contains(bare, sprintTaskRunbook) {
		t.Errorf("the counter-free card lost its title\ncard: %s", bare)
	}
	if !strings.Contains(bare, `<span class="badge `+priorityBadge(8)+`">P8</span>`) {
		t.Errorf("the counter-free card lost its priority badge, which reads P8 whatever the "+
			"card's counters\ncard: %s", bare)
	}

	// The two mixed cards, which are what keep the assertion above from passing on
	// a board that prints "0" for every counter of every card. The retries task has
	// one subtask and no comment; the alerting task has two comments and no
	// subtask. Each renders one real number beside one zero, and the two are
	// mirror images, so a counter group indifferent to the data fails on both.
	for _, tc := range []struct {
		what     string
		taskID   int
		column   int
		subtasks int
		comments int
	}{
		{"one subtask and no comment", f.retries, 1, 1, 0},
		{"no subtask and two comments", f.alerting, 0, 0, 2},
	} {
		group := spanWithRole(t, cardSlice(t, columns[tc.column], tc.taskID), "task-card-counters")
		for what, want := range map[string]string{
			"comment counter": counterMarkup("task-card-comments", "ti ti-message", tc.comments),
			"subtask counter": counterMarkup("task-card-subtasks", "ti ti-subtask", tc.subtasks),
		} {
			if !strings.Contains(group, want) {
				t.Errorf("the card of a task with %s does not render its %s as %q; both counters "+
					"are rendered on every card and each carries its own number"+
					"\ncounters: %s", tc.what, what, want, group)
			}
		}
	}

	// And every card of the board carries the pair, so the shape is the same one
	// whatever the sprint holds — the property the criterion is written for, which
	// no single card can establish on its own.
	region := memberBoardRegion(t, servePage(t, mux, f.path()))
	cards := strings.Count(region, cardOpen)
	groups := strings.Count(region, `data-role="task-card-counters"`)
	if cards != 6 {
		t.Fatalf("the board renders %d cards, want the fixture's 6; the count below would be "+
			"measured against the wrong number", cards)
	}
	if groups != cards {
		t.Errorf("the board renders %d cards and %d counter groups; every card of this board "+
			"carries one (Acceptance Criterion 134)", cards, groups)
	}
	for _, role := range []string{"task-card-comments", "task-card-subtasks"} {
		if got := strings.Count(region, `data-role="`+role+`"`); got != cards {
			t.Errorf("the board renders %d cards and %d %s indicators; both counters are present "+
				"on every card", cards, got, role)
		}
	}
}

// TestSprintBoard_CardMergesBadgesAndCountersOntoOneLine is the gate for the part
// of Acceptance Criterion 133 that governs the card's SHAPE rather than its
// contents: the badges and the counters share one line, the counters close it at
// its trailing edge, that line wraps inside the card instead of overflowing it on
// a narrow column, and the card renders no separate footer row for the counters.
//
// The contents and their order are asserted by
// TestSprintBoard_CardShowsSixDataPointsInOrder; what is asserted here is that the
// four values are laid out as ONE line rather than two, which is the whole of the
// change and is invisible to any check that only looks for the values.
//
// The layout is asserted through the utility classes the line carries, because the
// classes are what a Go test can observe and are exactly what the browser resolves
// the behaviour from: `justify-content-between` is what puts the first flex item at
// the leading edge and the last at the trailing one, and `flex-wrap` is what turns
// "too narrow to hold both" into a wrap rather than an overflow — a wrapped flex
// line holding a single item resolves `space-between` to `flex-start`, so the
// counters drop directly below the badges inside the same card. Without
// `flex-wrap` the two groups would be squeezed onto one line and the card would
// overflow its column, which Acceptance Criteria 27 and 133 both forbid.
//
// The tasks board's card is asserted UNCHANGED in the same test, because "this
// board has no metadata footer" states nothing unless the other board still has
// one: a template that had dropped the footer from both cards would satisfy every
// absence assertion here.
func TestSprintBoard_CardMergesBadgesAndCountersOntoOneLine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	sprintPage := servePage(t, mux, f.path())
	columns := memberBoardColumns(t, sprintPage)
	card := cardSlice(t, columns[0], f.reconcile)

	line := spanWithRole(t, card, "task-card-summary")
	if line == "" {
		t.Fatalf("the card renders no line carrying both the badges and the counters\ncard: %s", card)
	}

	// The line is a flex row that wraps, with its two groups at its two edges.
	//
	// The classes are read from the line's OWN opening tag and not from its whole
	// subtree: both inner groups are flex containers that wrap as well, so a check
	// against the subtree would find `d-flex` and `flex-wrap` on a line that
	// carried neither, and would pass on markup that overflows the card.
	own := elementClassTokens(t, card, `data-role="task-card-summary"`,
		"the sprint board's card")
	for class, why := range map[string]string{
		"d-flex":                  "the two groups share a line only inside a flex container",
		"flex-wrap":               "without it the line overflows the card instead of wrapping",
		"justify-content-between": "it is what puts the counters at the trailing edge",
		"align-items-center":      "the badges are taller than the counters and the line centres them",
	} {
		if !own[class] {
			t.Errorf("the card's badge-and-counter line does not itself carry %q: %s\nline: %s",
				class, why, line)
		}
	}

	// The two groups are the line's own children, the badges first and the
	// counters last, which is what "leading edge" and "trailing edge" mean once
	// the line is `justify-content-between`.
	badges := spanWithRole(t, line, "task-card-badges")
	counters := spanWithRole(t, line, "task-card-counters")
	if badges == "" || counters == "" {
		t.Fatalf("the card's line does not hold both groups (badges %q, counters %q)\nline: %s",
			badges, counters, line)
	}
	if strings.Index(line, badges) > strings.Index(line, counters) {
		t.Errorf("the counters precede the badges on the card's line; the badges lead it and "+
			"the counters close it\nline: %s", line)
	}
	// Neither group leaks into the other: a single flat row of four spans would
	// satisfy every "contains" assertion above and would place nothing at either
	// edge, because `justify-content-between` spreads FOUR items across the line
	// instead of pinning two groups to its two ends.
	if strings.Contains(badges, "task-card-comments") || strings.Contains(badges, "task-card-subtasks") {
		t.Errorf("a counter sits inside the badge group\nbadges: %s", badges)
	}
	if strings.Contains(counters, "badge ") {
		t.Errorf("a badge sits inside the counter group\ncounters: %s", counters)
	}
	// The counter group carries both counters and nothing else, so the trailing
	// edge of the line is the two numbers.
	if !strings.Contains(counters, counterMarkup("task-card-comments", "ti ti-message", 3)) ||
		!strings.Contains(counters, counterMarkup("task-card-subtasks", "ti ti-subtask", 2)) {
		t.Errorf("the trailing group does not carry both of the card's counters\ncounters: %s", counters)
	}

	// No card of the board renders a separate footer row: not under the tasks
	// board's role, not under the row's own trailing-edge alignment, and not with
	// the top margin that separated it from the badges. A template that merely
	// renamed the footer, or that kept a second row beside the merged line, keeps
	// at least one of the three, so all three are asserted absent from EVERY card
	// rather than from the one card sliced above.
	//
	// The board is scanned card by card so the guard cannot fail for something the
	// column header or the empty state emits, and cannot pass because the one card
	// examined happens to be clean.
	for _, ids := range f.wantColumns() {
		for _, id := range ids {
			each := cardSlice(t, memberBoardRegion(t, sprintPage), id)
			for _, gone := range []string{
				`data-role="task-card-meta"`, // the tasks board's footer, which this card has not
				"justify-content-end",        // that footer's own trailing-edge alignment
				"mt-2",                       // the gap that separated the footer from the badges
			} {
				if strings.Contains(each, gone) {
					t.Errorf("the card of task #%d still renders %q; the counters share the badge "+
						"line and the card has no separate footer row (Acceptance Criterion "+
						"133)\ncard: %s", id, gone, each)
				}
			}
		}
	}

	// The control that keeps those absences from being vacuous: the ROADMAP TASKS
	// page's card is untouched by this criterion. It still renders its metadata
	// footer, and that footer still lists the subtask count BEFORE the comment
	// count — the order this board deliberately reverses.
	tasksPage := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")
	tasksBoard := boardRegion(t, tasksPage)
	if !strings.Contains(tasksBoard, `data-role="task-card-meta"`) {
		t.Fatalf("the roadmap tasks page's board renders no metadata footer at all, so asserting " +
			"the sprint board has none proves nothing; that card is unchanged by Acceptance " +
			"Criterion 133")
	}
	if strings.Contains(tasksBoard, `data-role="task-card-summary"`) {
		t.Errorf("the roadmap tasks page's card grew the sprint card's merged line; that card " +
			"keeps its separate metadata footer (Acceptance Criterion 133)")
	}
	// The reconciliation task carries two subtasks and three comments, so its card
	// on the tasks board renders both indicators; it sits in that board's SPRINT
	// column, which is its second.
	tasksFooter := metaFooter(t, cardSlice(t, boardColumns(t, tasksPage)[1], f.reconcile))
	sub := strings.Index(tasksFooter, `data-role="task-card-subtasks"`)
	com := strings.Index(tasksFooter, `data-role="task-card-comments"`)
	if sub < 0 || com < 0 {
		t.Fatalf("the tasks board's control card does not render both counters (subtasks at %d, "+
			"comments at %d), so the order comparison below is vacuous\nfooter: %s",
			sub, com, tasksFooter)
	}
	if sub > com {
		t.Errorf("the tasks board's metadata footer now lists the comment count before the "+
			"subtask count; that footer keeps its own order, and the sprint card's reversed "+
			"order is stated separately from it\nfooter: %s", tasksFooter)
	}
}

// counterMarkup is the whole indicator markup of one counter on a sprint board
// card: the element that names it, the icon, and the number.
//
// It is built here rather than written out at each assertion so that a check for
// "the counter reads 0" cannot degrade into a check for the digit 0 appearing
// somewhere in the card, which the priority and severity badges alone would
// satisfy on most fixtures.
func counterMarkup(role, icon string, n int) string {
	return `<span data-role="` + role + `"><i class="` + icon + ` me-1"></i>` + itoa(n) + `</span>`
}

// ==================== READ-ONLY ====================

// TestSprintBoard_IsReadOnly is the gate for Acceptance Criterion 138: the board
// offers no drag-and-drop and no control of any other kind that moves a task
// between columns, reorders cards, changes a task's status, or creates or edits
// anything. The only button in the board is the card itself, and activating it
// opens the read-only modal.
//
// The assertion is made on the board REGION rather than on the page, because the
// page legitimately carries controls that submit nothing — the page header's
// "Back to sprints" link, the modal's Close button, the sidebar links — and a
// page-wide check would either fail on those or have to be weakened until it
// proved nothing.
func TestSprintBoard_IsReadOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	region := memberBoardRegion(t, servePage(t, mux, f.path()))
	low := strings.ToLower(region)

	// Anything that could carry a change to the server, plus the attributes that
	// would make an element do so or make a card draggable.
	for _, bad := range []string{
		"<form", "<input", "<textarea", "<select", "<a ", "href=", "action=", "formaction=",
		"method=", "onclick=", "onsubmit=", "ondrop=", "ondragstart=", "draggable=",
		"contenteditable", "sortable",
	} {
		if strings.Contains(low, bad) {
			t.Errorf("the member-tasks board must be read-only but contains %q", bad)
		}
	}

	// The only buttons in the board are the cards, and every one of them is a
	// modal trigger. A count of zero would make this vacuous, so it is checked.
	buttons := strings.Count(region, "<button")
	if buttons != 6 {
		t.Errorf("the board carries %d buttons, want the 6 cards of its member tasks", buttons)
	}
	if got := strings.Count(region, cardOpen); got != buttons {
		t.Errorf("the board carries %d buttons of which %d are cards; every button in the board "+
			"must be a card", buttons, got)
	}
	if got := strings.Count(region, `data-bs-toggle="modal"`); got != buttons {
		t.Errorf("the board carries %d buttons and %d modal triggers; the only thing a card does "+
			"is open the read-only modal", buttons, got)
	}
}

// ==================== READ COST ====================

// TestSprintBoard_CommentCountIsOneGroupedQueryWhateverN is the gate for
// Acceptance Criterion 137: the comment number on a card comes from ONE grouped
// counting query over the whole set of rendered member-task ids — never one per
// card — so an instrumented count is 1 whatever the number of member tasks, the
// count of comment-listing queries for member tasks is 0, and a sprint with no
// member task issues no such query at all.
//
// It measures on the same counting source Acceptance Criterion 70 is measured
// with, so the two counts are taken on one instrument rather than two.
func TestSprintBoard_CommentCountIsOneGroupedQueryWhateverN(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// The same three reads for 1, 3 and 12 member tasks: the query count does not
	// grow with N.
	for _, members := range []int{1, 3, 12} {
		name := "settlement-window-" + itoa(members)
		sprintID := seedSprintWithMembers(t, name, members)
		src := openCounting(t, name)

		data, err := readSprint(context.Background(), src, name, sprintID)
		if err != nil {
			t.Fatalf("%d members: readSprint: %v", members, err)
		}
		if len(data.Tasks) != members {
			t.Fatalf("%d members: the page carries %d member tasks", members, len(data.Tasks))
		}
		if src.groupedCommentCounts != 1 {
			t.Errorf("%d members: the page issued %d comment-count queries, want exactly 1",
				members, src.groupedCommentCounts)
		}
		if src.perTaskComments != 0 {
			t.Errorf("%d members: the page issued %d comment-listing queries for member tasks, "+
				"want 0: a card shows a number and reading every comment body to display one "+
				"would be work the page throws away", members, src.perTaskComments)
		}
		if src.sprintComments != 1 {
			t.Errorf("%d members: the page issued %d sprint-comment queries, want exactly 1",
				members, src.sprintComments)
		}
		if src.sprintTasks != 1 {
			t.Errorf("%d members: the page issued %d member-task queries, want 1",
				members, src.sprintTasks)
		}

		// The one query covered EVERY rendered card, which is what makes one query
		// sufficient rather than merely few.
		if len(src.lastGroupedIDs) != members {
			t.Errorf("%d members: the comment count was given %d ids, want %d",
				members, len(src.lastGroupedIDs), members)
		}

		// The subtask number costs no query of its own: the member-task read
		// already returned it, and nothing else was read.
		if src.boundedTaskList != 0 || src.groupedTaskSprints != 0 {
			t.Errorf("%d members: the page issued %d bounded task-list and %d sprint-resolution "+
				"queries, want 0 and 0", members, src.boundedTaskList, src.groupedTaskSprints)
		}

		// The grouping into three columns and the counting of each column happen
		// in memory over the rows already read: no query per column, none per card.
		grouped := 0
		for i := range data.Columns {
			grouped += len(data.Columns[i].Tasks)
		}
		if grouped != members {
			t.Errorf("%d members: the columns hold %d cards, want the %d the member-task read "+
				"returned", members, grouped, members)
		}
		if len(data.Columns) != len(sprintBoardColumns) {
			t.Errorf("%d members: the board has %d columns, want %d",
				members, len(data.Columns), len(sprintBoardColumns))
		}

		// The control that makes those counts falsifiable: the per-card alternative
		// the SPEC forbids, measured on the same instrument.
		src.groupedCommentCounts = 0
		for i := range data.Tasks {
			if _, err := src.CountTaskCommentsByTasks(context.Background(),
				[]int{data.Tasks[i].ID}); err != nil {
				t.Fatalf("%d members: per-card control read: %v", members, err)
			}
		}
		if src.groupedCommentCounts != members {
			t.Errorf("%d members: the per-card control issued %d reads, want %d; the instrument "+
				"does not track reads one-for-one", members, src.groupedCommentCounts, members)
		}
	}

	// A sprint with no member task issues no grouped count at all: the query takes
	// a set of rendered task ids and that set is empty. The sprint's own comment
	// listing is still issued, because the Comments card is always present.
	const emptyName = "settlement-window-none"
	emptySprint := seedSprintWithMembers(t, emptyName, 0)
	emptySrc := openCounting(t, emptyName)

	emptyData, err := readSprint(context.Background(), emptySrc, emptyName, emptySprint)
	if err != nil {
		t.Fatalf("empty sprint: readSprint: %v", err)
	}
	if len(emptyData.Tasks) != 0 {
		t.Fatalf("the empty sprint carries %d member tasks, want 0", len(emptyData.Tasks))
	}
	if emptySrc.groupedCommentCounts != 0 {
		t.Errorf("the empty sprint issued %d comment-count queries, want 0",
			emptySrc.groupedCommentCounts)
	}
	if emptySrc.perTaskComments != 0 {
		t.Errorf("the empty sprint issued %d comment-listing queries, want 0", emptySrc.perTaskComments)
	}
	if emptySrc.sprintComments != 1 {
		t.Errorf("the empty sprint issued %d sprint-comment queries, want exactly 1: the "+
			"Comments card is always present", emptySrc.sprintComments)
	}
	// The board is still three columns, all of them empty.
	if len(emptyData.Columns) != len(sprintBoardColumns) {
		t.Errorf("the empty sprint's board has %d columns, want %d",
			len(emptyData.Columns), len(sprintBoardColumns))
	}
	for i := range emptyData.Columns {
		if len(emptyData.Columns[i].Tasks) != 0 {
			t.Errorf("the empty sprint's %s column holds %d cards, want 0",
				emptyData.Columns[i].Heading, len(emptyData.Columns[i].Tasks))
		}
	}
}

// TestGroupIntoSprintBoardColumns_ReusesTheSummaryCategorisation asserts the
// grouping is driven by models.CategorizeTaskStatus and by nothing else: every
// one of the five task statuses lands in the column its category names, and a
// category no column claims places its task in no column rather than in an
// invented fourth one.
//
// This is the unit-level half of Acceptance Criterion 131. The page-level test
// above compares two renderings of one sprint; this one states WHY they can never
// disagree — there is one mapping from status to bucket, and the board reads it.
func TestGroupIntoSprintBoardColumns_ReusesTheSummaryCategorisation(t *testing.T) {
	// Every status of the closed enum, and the column its category assigns it.
	for _, status := range models.ValidTaskStatuses {
		category := models.CategorizeTaskStatus(status)
		want, claimed := sprintBoardColumnOf(category)
		if !claimed {
			t.Errorf("status %s falls in category %v, which no board column claims; the enum is "+
				"closed and every value must land in a column", status, category)
			continue
		}

		views := newTaskViews([]models.Task{{ID: 1, Status: status, Title: "Reconcile the ledger"}})
		columns := groupIntoSprintBoardColumns(views)
		if len(columns) != len(sprintBoardColumns) {
			t.Fatalf("the grouping produced %d columns, want %d", len(columns), len(sprintBoardColumns))
		}
		for i := range columns {
			held := len(columns[i].Tasks)
			if i == want && held != 1 {
				t.Errorf("status %s: the %s column holds %d tasks, want 1",
					status, columns[i].Heading, held)
			}
			if i != want && held != 0 {
				t.Errorf("status %s: the %s column holds %d tasks, want 0",
					status, columns[i].Heading, held)
			}
		}
	}

	// A category outside the three is placed in no column, and no fourth column is
	// invented for it. The status enum is closed and a CHECK constraint restricts
	// tasks.status to its five values, so this is defensive only — but the branch
	// exists and an untested one is a branch that can quietly place a card wrongly.
	if _, claimed := sprintBoardColumnOf(models.CategoryOther); claimed {
		t.Errorf("models.CategoryOther is claimed by a board column; the board defines three " +
			"columns and none of them holds an uncategorised task")
	}
	columns := groupIntoSprintBoardColumns(newTaskViews([]models.Task{
		{ID: 1, Status: models.TaskStatus("ARCHIVED"), Title: "Retire the legacy settlement export"},
	}))
	if len(columns) != len(sprintBoardColumns) {
		t.Errorf("an uncategorised task produced %d columns, want %d",
			len(columns), len(sprintBoardColumns))
	}
	for i := range columns {
		if held := len(columns[i].Tasks); held != 0 {
			t.Errorf("an uncategorised task was placed in the %s column (%d cards)",
				columns[i].Heading, held)
		}
	}

	// All three columns are built whatever the data holds: an empty sprint is an
	// empty board, not an absent one.
	empty := groupIntoSprintBoardColumns(nil)
	if len(empty) != len(sprintBoardColumns) {
		t.Fatalf("a sprint with no member task produced %d columns, want %d",
			len(empty), len(sprintBoardColumns))
	}
	for i := range empty {
		if empty[i].Heading != sprintBoardColumns[i].heading {
			t.Errorf("column %d of an empty board is headed %q, want %q",
				i, empty[i].Heading, sprintBoardColumns[i].heading)
		}
		if len(empty[i].Tasks) != 0 {
			t.Errorf("column %d of an empty board holds %d cards, want 0", i, len(empty[i].Tasks))
		}
	}
}
