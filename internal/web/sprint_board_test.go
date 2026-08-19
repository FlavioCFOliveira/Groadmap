package web

import (
	"context"
	"regexp"
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
// 139).
//
// It replaces the assertions that pinned the six-column member-tasks table the
// board supersedes: the page renders no table at all any more, so a test written
// against that table would fail for the wrong reason — its subject is gone.
//
// The markup helpers of the tasks board (boardRegion, columnHeader, cardSlice,
// metaFooter, cardOpen, cardMarker, shownEmptyState in board_test.go) are reused
// verbatim wherever they apply, because the two boards emit the same classes and
// the same data-role hooks: they are one presentation rendered on two pages, and
// a helper that worked on only one of them would be evidence they had diverged.

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
	runbook   int // BACKLOG   -> WAITING. No subtask, no comment: no card footer.
	reconcile int // SPRINT    -> WAITING. 2 subtasks, 3 comments, both counters.
	dashboard int // DOING     -> DOING.   No subtask, no comment: no card footer.
	retries   int // TESTING   -> DOING.   1 subtask, no comment: one counter.
	alerting  int // SPRINT    -> WAITING. No subtask, 2 comments: one counter.
	schema    int // COMPLETED -> CLOSED.  No subtask, no comment: no card footer.
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

// TestSprintBoard_CardOrderIsTheSprintTaskPosition is the gate for Acceptance
// Criterion 132: within every column the cards appear in the sprint_tasks
// position order the page reads, and the board applies no sort of its own.
//
// The fixture's position order is neither the id order nor the priority order, so
// a board that re-sorted its cards — or that lost the read's order and fell back
// to the id order SQLite would return unordered rows in — renders a different
// sequence. Both alternatives are asserted against explicitly, because "the cards
// are in position order" is satisfied vacuously by any order when the three
// coincide.
//
// The second half reorders the sprint through the production write path and
// re-renders: the cards follow, which is what proves the order is READ rather
// than computed.
func TestSprintBoard_CardOrderIsTheSprintTaskPosition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	columns := memberBoardColumns(t, servePage(t, mux, f.path()))
	for i, column := range columns {
		heading, _ := columnHeader(t, column)
		got := memberCardIDs(t, column)
		want := f.wantColumns()[i]
		if !equalIDs(got, want) {
			t.Errorf("the %s column renders the cards %v, want the position order %v",
				heading, got, want)
		}
	}

	// The controls that make the assertion above discriminating: the WAITING
	// column's position order differs from both alternatives a defective board
	// would produce.
	waiting := f.wantColumns()[0]
	byID := []int{f.reconcile, f.alerting, f.runbook}
	byPriority := []int{f.reconcile, f.runbook, f.alerting}
	if equalIDs(waiting, byID) {
		t.Fatalf("the fixture's WAITING position order %v is also its id order; the ordering "+
			"assertion would pass on a board that ignored the read's order", waiting)
	}
	if equalIDs(waiting, byPriority) {
		t.Fatalf("the fixture's WAITING position order %v is also its priority order; the "+
			"ordering assertion would pass on a board that sorted by priority", waiting)
	}

	// Reordering through the production path reorders the cards. The reversal
	// moves a task within its column (runbook from first to last of WAITING) and
	// swaps the two DOING cards, so both columns change.
	reordered := []int{f.schema, f.alerting, f.retries, f.dashboard, f.reconcile, f.runbook}
	reorderSprintTasks(t, f.name, f.sprintID, reordered)

	columns = memberBoardColumns(t, servePage(t, mux, f.path()))
	wantAfter := [][]int{
		{f.alerting, f.reconcile, f.runbook}, // WAITING: positions 2, 5 and 6
		{f.retries, f.dashboard},             // DOING:   positions 3 and 4
		{f.schema},                           // CLOSED:  position 1
	}
	for i, column := range columns {
		heading, _ := columnHeader(t, column)
		got := memberCardIDs(t, column)
		if !equalIDs(got, wantAfter[i]) {
			t.Errorf("after reordering the sprint, the %s column renders the cards %v, want %v",
				heading, got, wantAfter[i])
		}
	}
}

// reorderSprintTasks sets the sprint's task order through the production write
// path, so the value under test travels the same route `rmp sprint reorder-tasks`
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
// Criterion 133: the card shows exactly six data points, in this order — the
// title leading the card, the reference `#<id>` on its own line as secondary
// text, a priority badge, a severity badge, and a trailing-edge footer holding the
// number of subtasks and the number of comments, each as its icon followed by its
// number.
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
	// 3 and 4. The two badges: the prefixed value, in the variant the semantic
	//          mapping assigns to that value.
	priority := `<span class="badge ` + priorityBadge(9) + `">P9</span>`
	severity := `<span class="badge ` + severityBadge(2) + `">S2</span>`
	// 5 and 6. The counters, each an icon followed by its number.
	subtasks := `<span data-role="task-card-subtasks"><i class="ti ti-subtask me-1"></i>2</span>`
	comments := `<span data-role="task-card-comments"><i class="ti ti-message me-1"></i>3</span>`

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
		{"subtask counter", subtasks},
		{"comment counter", comments},
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

	// The footer is aligned to the TRAILING edge of the card, which is where the
	// two counters sit and what distinguishes this row from the tasks board's
	// leading-edge metadata footer.
	footer := metaFooter(t, card)
	if !strings.Contains(footer, "justify-content-end") {
		t.Errorf("the card's counter footer is not aligned to the trailing edge of the card\n"+
			"footer: %s", footer)
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

// TestSprintBoard_ZeroCountersRenderNothing is the gate for Acceptance Criterion
// 134: a subtask count of 0 and a comment count of 0 each render nothing at all —
// no icon, no number, no dash, no placeholder, no empty slot — and a task with
// neither renders no footer row.
//
// Every absence is asserted against the INDICATOR'S MARKUP, not against the digit
// `0`: a rendering that printed an icon with nothing beside it would still occupy
// the space this criterion removes, and a check for the digit alone would accept
// it.
func TestSprintBoard_ZeroCountersRenderNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	columns := memberBoardColumns(t, servePage(t, mux, f.path()))

	const (
		subtaskIcon = "ti ti-subtask"
		commentIcon = "ti ti-message"
	)

	// A task with neither counter renders no footer element at all: the runbook
	// task has no subtask and no comment.
	bare := cardSlice(t, columns[0], f.runbook)
	if strings.Contains(bare, `data-role="task-card-meta"`) {
		t.Errorf("a member task with no subtask and no comment renders a counter footer\ncard: %s", bare)
	}
	for _, absent := range []string{subtaskIcon, commentIcon, "&mdash;", "None", "Subtasks:", "Comments:"} {
		if strings.Contains(bare, absent) {
			t.Errorf("a member task with no counter renders %q on its card\ncard: %s", absent, bare)
		}
	}
	// The card itself is still a full card: what is absent is the indicators, not
	// the task.
	if !strings.Contains(bare, sprintTaskRunbook) {
		t.Errorf("the counter-free card lost its title\ncard: %s", bare)
	}
	if !strings.Contains(bare, `<span class="badge `+priorityBadge(8)+`">P8</span>`) {
		t.Errorf("the counter-free card lost its priority badge, which reads P8 whatever the "+
			"card's counters\ncard: %s", bare)
	}

	// A task with ONE counter renders that one and no slot for the other: the
	// retries task has one subtask and no comment.
	subtaskOnly := metaFooter(t, cardSlice(t, columns[1], f.retries))
	if !strings.Contains(subtaskOnly,
		`<span data-role="task-card-subtasks"><i class="`+subtaskIcon+` me-1"></i>1</span>`) {
		t.Errorf("the card of a task with one subtask does not show its subtask count\nfooter: %s",
			subtaskOnly)
	}
	if strings.Contains(subtaskOnly, commentIcon) || strings.Contains(subtaskOnly, "task-card-comments") {
		t.Errorf("the card of a task with no comment renders a comment indicator\nfooter: %s", subtaskOnly)
	}

	// And the mirror image, so neither absence can come from a footer that simply
	// omits everything: the alerting task has two comments and no subtask.
	commentOnly := metaFooter(t, cardSlice(t, columns[0], f.alerting))
	if !strings.Contains(commentOnly,
		`<span data-role="task-card-comments"><i class="`+commentIcon+` me-1"></i>2</span>`) {
		t.Errorf("the card of a commented task does not show its comment count\nfooter: %s", commentOnly)
	}
	if strings.Contains(commentOnly, subtaskIcon) || strings.Contains(commentOnly, "task-card-subtasks") {
		t.Errorf("the card of a task with no subtask renders a subtask indicator\nfooter: %s", commentOnly)
	}
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
