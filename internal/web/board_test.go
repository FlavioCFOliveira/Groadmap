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

// This file is the gate for the roadmap tasks page's Kanban board: the five
// fixed columns, the placement and ordering of the cards inside them, the card's
// content, the modal a card opens, the empty states, and the page's read cost
// (SPEC/WEB.md § Roadmap Tasks Page; Acceptance Criteria 81 to 92).
//
// It replaces the assertions that pinned the fifteen-column task table the board
// supersedes: the page renders no table at all any more, so a test written
// against that table would fail for the wrong reason — its subject is gone.

// ==================== FIXTURE ====================

// boardFixture names the rows seedBoardFixture created, so the assertions bind
// the ids that actually exist rather than assuming an autoincrement sequence.
type boardFixture struct {
	name string

	checkoutSprint int
	ledgerSprint   int

	// BACKLOG: four tasks whose priority order, creation order, and id order all
	// differ, so an ordering assertion over them cannot pass by accident.
	runbook   int
	retire    int
	translate int
	rotate    int

	passkey  int // SPRINT:    in the checkout sprint, with specialists and comments
	ledger   int // DOING:     in the ledger sprint, with subtasks and a dependency
	cookies  int // TESTING:   no sprint and no other indicator at all
	parser   int // COMPLETED: subtask of ledger
	backfill int // COMPLETED: subtask of ledger
}

// tasksByStatus is the placement the fixture must produce, keyed by status.
func (f *boardFixture) tasksByStatus() map[models.TaskStatus][]int {
	return map[models.TaskStatus][]int{
		models.StatusBacklog:   {f.runbook, f.retire, f.translate, f.rotate},
		models.StatusSprint:    {f.passkey},
		models.StatusDoing:     {f.ledger},
		models.StatusTesting:   {f.cookies},
		models.StatusCompleted: {f.parser, f.backfill},
	}
}

// seedBoardFixture builds a roadmap that populates all five board columns, with
// two sprints, a dependency edge, a parent/subtask hierarchy, specialists, and
// comments, so every card indicator has at least one card that shows it and at
// least one card that must not.
//
// The BACKLOG tasks are seeded so that priority order, created_at order, and id
// order are three DIFFERENT orders (see boardOrderingSeed below): a board that
// re-sorted the cards, or that dropped the read's order and fell back to id
// order, would produce a different sequence and fail the ordering assertion.
func seedBoardFixture(t *testing.T, name string) boardFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	f := boardFixture{name: name}

	newTask := func(title, created string, priority, severity int,
		taskType models.TaskType, status models.TaskStatus,
		specialists *string, parent *int) int {
		t.Helper()
		id, cerr := database.CreateTask(ctx, &models.Task{
			Title:                  title,
			Type:                   taskType,
			Status:                 status,
			Priority:               priority,
			Severity:               severity,
			Specialists:            specialists,
			ParentTaskID:           parent,
			FunctionalRequirements: "Operators must be able to follow this work from the board.",
			TechnicalRequirements:  "Implemented against the roadmap database, read-only on the web side.",
			AcceptanceCriteria:     "The board shows the task in the column of its status.",
			CreatedAt:              created,
		})
		if cerr != nil {
			t.Fatalf("creating task %q: %v", title, cerr)
		}
		return id
	}

	// BACKLOG, created in an order that is neither the priority order nor the
	// created_at order the board must render.
	for _, seed := range boardOrderingSeed {
		id := newTask(seed.title, seed.created, seed.priority, 2,
			models.TypeTask, models.StatusBacklog, nil, nil)
		switch seed.title {
		case backlogRunbook:
			f.runbook = id
		case backlogRetire:
			f.retire = id
		case backlogTranslate:
			f.translate = id
		case backlogRotate:
			f.rotate = id
		}
	}

	specialists := "go-developer, security-review"
	f.passkey = newTask("Add WebAuthn passkey support to checkout",
		"2026-02-01T09:00:00Z", 7, 4, models.TypeUserStory, models.StatusBacklog, &specialists, nil)
	f.ledger = newTask("Reconcile the settlement ledger against the acquirer report",
		"2026-02-02T09:00:00Z", 8, 6, models.TypeTask, models.StatusBacklog, nil, nil)
	f.cookies = newTask("Audit the session-cookie flags",
		"2026-02-03T09:00:00Z", 5, 2, models.TypeChore, models.StatusTesting, nil, nil)
	f.parser = newTask("Extract the acquirer report parser",
		"2026-02-04T09:00:00Z", 4, 1, models.TypeSubTask, models.StatusCompleted, nil, &f.ledger)
	f.backfill = newTask("Backfill the reconciliation report totals",
		"2026-02-05T09:00:00Z", 4, 1, models.TypeSubTask, models.StatusCompleted, nil, &f.ledger)

	// Two sprints, so a card that names a sprint names ITS OWN sprint: a
	// resolution that returned one sprint for every task could not pass.
	f.checkoutSprint = newSprint(t, database, "Checkout hardening",
		"Harden the checkout flow against credential-stuffing and replay.")
	f.ledgerSprint = newSprint(t, database, "Ledger reconciliation",
		"Reconcile the settlement ledger daily and alert on any residual.")

	// Sprint membership forces the member's status to SPRINT, which is the status
	// the passkey task must end in; the ledger task is then advanced to DOING, as
	// the state machine requires of a task in progress.
	if err := database.AddTasksToSprint(ctx, f.checkoutSprint, []int{f.passkey}); err != nil {
		t.Fatalf("adding the passkey task to the checkout sprint: %v", err)
	}
	if err := database.AddTasksToSprint(ctx, f.ledgerSprint, []int{f.ledger}); err != nil {
		t.Fatalf("adding the ledger task to the ledger sprint: %v", err)
	}
	if err := database.UpdateTaskStatus(ctx, []int{f.ledger}, models.StatusDoing); err != nil {
		t.Fatalf("moving the ledger task to DOING: %v", err)
	}

	// One dependency edge: the ledger work depends on the passkey work, so one
	// card shows a depends-on count and the other a blocks count.
	if err := database.AddTaskDependencyWithAudit(ctx, f.ledger, f.passkey); err != nil {
		t.Fatalf("making the ledger task depend on the passkey task: %v", err)
	}

	// Comments: two on the passkey task, one on a BACKLOG task, none anywhere
	// else, so a comment count appears on some cards and on no other.
	addTaskCommentTo(t, database, f.passkey, models.CommentDecision,
		"Passkeys are gated behind the existing risk score until the rollout completes.",
		"2026-02-06T09:00:00Z")
	addTaskCommentTo(t, database, f.passkey, models.CommentProgress,
		"The registration ceremony works end to end against the staging authenticator.",
		"2026-02-07T09:00:00Z")
	addTaskCommentTo(t, database, f.runbook, models.CommentNote,
		"The runbook must cover the acquirer's weekend settlement window.",
		"2026-02-08T09:00:00Z")

	return f
}

// The BACKLOG titles, named so the ordering seed and the assertions agree on
// them without repeating string literals.
const (
	backlogRunbook   = "Document the settlement reconciliation runbook"
	backlogRetire    = "Retire the legacy checkout endpoint"
	backlogTranslate = "Translate the refund notification emails to Portuguese"
	backlogRotate    = "Rotate the acquirer API credentials"
)

// boardOrderingSeed is the BACKLOG seed, in CREATION order. The board must render
// these four cards in priority DESC, created_at ASC order, which is:
//
//	rotate (9, Jan 20), retire (9, Feb 10), runbook (3, Jan 05), translate (3, Mar 20)
//
// That sequence is neither the creation/id order (runbook, retire, translate,
// rotate) nor the created_at order (runbook, rotate, retire, translate), so a
// board that preserved the wrong order, or applied a sort of its own, renders a
// different sequence.
var boardOrderingSeed = []struct {
	title    string
	created  string
	priority int
}{
	{backlogRunbook, "2026-01-05T09:00:00Z", 3},
	{backlogRetire, "2026-02-10T09:00:00Z", 9},
	{backlogTranslate, "2026-03-20T09:00:00Z", 3},
	{backlogRotate, "2026-01-20T09:00:00Z", 9},
}

// newSprint creates one PENDING sprint and returns its id.
func newSprint(t *testing.T, database *db.DB, title, description string) int {
	t.Helper()

	id, err := database.CreateSprint(context.Background(), &models.Sprint{
		Status:      models.SprintPending,
		Title:       title,
		Description: description,
		CreatedAt:   "2026-01-02T09:00:00Z",
	})
	if err != nil {
		t.Fatalf("creating sprint %q: %v", title, err)
	}
	return id
}

// ==================== MARKUP HELPERS ====================

// boardRegion returns the page's <main> element, which holds the board and
// nothing else. The task detail modals are rendered AFTER it, outside the page
// wrapper, so slicing here keeps a modal's copy of a task's title from being
// mistaken for a card.
func boardRegion(t *testing.T, body string) string {
	t.Helper()

	const open = `<main class="page-body">`
	start := strings.Index(body, open)
	end := strings.Index(body, "</main>")
	if start < 0 || end < start {
		t.Fatalf("the tasks page has no <main class=\"page-body\"> region to slice")
	}
	return body[start+len(open) : end]
}

// boardColumns returns the markup of each board column, in the order the page
// renders them.
func boardColumns(t *testing.T, body string) []string {
	t.Helper()

	region := boardRegion(t, body)
	if !strings.Contains(region, `data-role="task-board"`) {
		t.Fatalf("the tasks page renders no Kanban board")
	}

	parts := strings.Split(region, `data-role="task-board-column"`)
	if len(parts) < 2 {
		t.Fatalf("the board renders no column")
	}
	return parts[1:]
}

// reColumnHeader captures a column's status name and the count its badge shows.
var reColumnHeader = regexp.MustCompile(
	`<h3 class="card-title">([A-Z]+) <span class="badge bg-secondary-lt ms-2">(\d+)</span></h3>`)

// columnHeader returns the status name and the count badge of one column.
func columnHeader(t *testing.T, column string) (status string, count int) {
	t.Helper()

	m := reColumnHeader.FindStringSubmatch(column)
	if m == nil {
		t.Fatalf("a board column has no Tabler card-title header with a count badge")
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		t.Fatalf("column %s: count badge %q is not a number: %v", m[1], m[2], err)
	}
	return m[1], n
}

// cardMarker is the attribute that identifies the card of one task: the modal it
// opens. It appears once per card and nowhere else in the board region.
func cardMarker(taskID int) string {
	return `data-bs-target="#task-modal-` + itoa(taskID) + `"`
}

// cardOpen is the opening markup of a board card, used both to count cards and to
// find a card's boundaries.
const cardOpen = `<div class="card card-sm task-card"`

// cardSlice returns the markup of one task's card within a column.
func cardSlice(t *testing.T, column string, taskID int) string {
	t.Helper()

	at := strings.Index(column, cardMarker(taskID))
	if at < 0 {
		t.Fatalf("task #%d has no card in this column", taskID)
	}
	start := strings.LastIndex(column[:at], cardOpen)
	if start < 0 {
		t.Fatalf("task #%d's card does not open with the Tabler card markup %s", taskID, cardOpen)
	}
	rest := column[start+len(cardOpen):]
	if next := strings.Index(rest, cardOpen); next >= 0 {
		return column[start : start+len(cardOpen)+next]
	}
	return column[start:]
}

// ==================== THE FIVE FIXED COLUMNS ====================

// TestTaskBoard_RendersFiveFixedColumnsFromTheEnum is the gate for Acceptance
// Criterion 81: exactly five columns, one per TaskStatus, left to right in the
// order of the task state machine's flow, each titled with the status identifier
// in upper case, present on every request whatever the data holds — and no task
// table anywhere on the page.
//
// The expected titles are read from models.ValidTaskStatuses rather than written
// out here, because the SPEC requires the set and the order to come from the
// model. The enum itself is pinned against the SPEC's five values first, so a
// change to the model cannot silently redefine what this test asserts.
func TestTaskBoard_RendersFiveFixedColumnsFromTheEnum(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	want := []models.TaskStatus{
		models.StatusBacklog,
		models.StatusSprint,
		models.StatusDoing,
		models.StatusTesting,
		models.StatusCompleted,
	}
	if len(models.ValidTaskStatuses) != len(want) {
		t.Fatalf("models.ValidTaskStatuses holds %d values, want the 5 the board has columns for",
			len(models.ValidTaskStatuses))
	}
	for i := range want {
		if models.ValidTaskStatuses[i] != want[i] {
			t.Fatalf("models.ValidTaskStatuses[%d] = %s, want %s; the board takes its column order "+
				"from the enum, which must follow the state machine's flow",
				i, models.ValidTaskStatuses[i], want[i])
		}
	}

	populated := seedBoardFixture(t, "payment-platform")
	if err := createEmptyRoadmap("payment-platform-empty"); err != nil {
		t.Fatalf("creating the empty roadmap: %v", err)
	}
	mux := buildMux()

	// The columns are fixed: the same five, in the same order, for a roadmap full
	// of tasks and for one with none.
	for _, name := range []string{populated.name, "payment-platform-empty"} {
		body := servePage(t, mux, "/roadmaps/"+name+"/tasks")
		columns := boardColumns(t, body)

		if len(columns) != len(want) {
			t.Fatalf("%s: the board renders %d columns, want exactly %d", name, len(columns), len(want))
		}
		for i, column := range columns {
			status, _ := columnHeader(t, column)
			if status != string(want[i]) {
				t.Errorf("%s: column %d is titled %q, want %q", name, i, status, want[i])
			}
		}

		// The board is the page's only task presentation: no table, and no table
		// header of the fifteen-column table it replaced.
		region := boardRegion(t, body)
		if strings.Contains(region, "<table") {
			t.Errorf("%s: the tasks page renders a task table; the board is its only task presentation", name)
		}
		if strings.Contains(region, "<th") {
			t.Errorf("%s: the tasks page renders table headers", name)
		}
	}
}

// TestTaskBoard_PlacesEveryTaskOnceInItsStatusColumn is the gate for Acceptance
// Criterion 82: every task of the roadmap appears on the board exactly once, in
// the column of its own status, and the five column counts sum to the roadmap's
// task count.
func TestTaskBoard_PlacesEveryTaskOnceInItsStatusColumn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")
	region := boardRegion(t, body)
	columns := boardColumns(t, body)

	placed := 0
	for i, status := range models.ValidTaskStatuses {
		for _, taskID := range f.tasksByStatus()[status] {
			placed++
			// Exactly one card for the task on the whole board: not omitted, not
			// duplicated across columns.
			if got := strings.Count(region, cardMarker(taskID)); got != 1 {
				t.Errorf("task #%d has %d cards on the board, want exactly 1", taskID, got)
			}
			// And that one card is in the column of the task's own status.
			if !strings.Contains(columns[i], cardMarker(taskID)) {
				t.Errorf("task #%d is not in the %s column, which is its status", taskID, status)
			}
		}
	}

	// The counts sum to the roadmap's task count, read from the database rather
	// than from the fixture's own arithmetic.
	total := 0
	for _, column := range columns {
		_, count := columnHeader(t, column)
		total += count
	}
	tasks := countRoadmapTasks(t, f.name)
	if total != tasks {
		t.Errorf("the column counts sum to %d, want the roadmap's %d tasks", total, tasks)
	}
	if placed != tasks {
		t.Errorf("the fixture placed %d tasks but the roadmap holds %d; the seed and the "+
			"assertion have drifted apart", placed, tasks)
	}
}

// TestTaskBoard_ColumnCountMatchesItsCards is the gate for Acceptance Criterion
// 83: the badge on a column header carries the number of tasks in that column,
// which equals the number of cards rendered in it, and an empty column shows 0.
func TestTaskBoard_ColumnCountMatchesItsCards(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	columns := boardColumns(t, servePage(t, mux, "/roadmaps/"+f.name+"/tasks"))
	byStatus := f.tasksByStatus()

	for i, status := range models.ValidTaskStatuses {
		name, count := columnHeader(t, columns[i])
		cards := strings.Count(columns[i], cardOpen)

		if count != cards {
			t.Errorf("column %s shows the count %d but renders %d cards", name, count, cards)
		}
		if want := len(byStatus[status]); count != want {
			t.Errorf("column %s shows the count %d, want %d", name, count, want)
		}
	}

	// A column with no task shows 0 rather than no badge at all. The roadmap
	// seeded by seedRoadmap holds a single SPRINT task, so its other four columns
	// are empty on an otherwise populated board.
	sparse := seedRoadmap(t, "checkout-rollout")
	for i, column := range boardColumns(t, servePage(t, mux, "/roadmaps/"+sparse+"/tasks")) {
		name, count := columnHeader(t, column)
		want := 0
		if models.ValidTaskStatuses[i] == models.StatusSprint {
			want = 1
		}
		if count != want {
			t.Errorf("on a sparse board, column %s shows the count %d, want %d", name, count, want)
		}
		if got := strings.Count(column, cardOpen); got != want {
			t.Errorf("on a sparse board, column %s renders %d cards, want %d", name, got, want)
		}
	}
}

// TestTaskBoard_CardOrderIsPriorityThenCreatedAt is the gate for Acceptance
// Criterion 84: within a column the cards follow priority DESC, created_at ASC —
// the order the page's own read returns — and the board applies no sort of its
// own.
//
// The seed is built so that this order differs from the id order and from the
// created_at order; both alternatives are asserted to be DIFFERENT sequences
// here, so the test cannot pass against a board that preserved the wrong one.
func TestTaskBoard_CardOrderIsPriorityThenCreatedAt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	backlog := boardColumns(t, servePage(t, mux, "/roadmaps/"+f.name+"/tasks"))[0]
	if status, _ := columnHeader(t, backlog); status != string(models.StatusBacklog) {
		t.Fatalf("the first column is %s, want BACKLOG", status)
	}

	// priority DESC, then created_at ASC.
	wantOrder := []int{f.rotate, f.retire, f.runbook, f.translate}
	// The two orders the board must NOT produce.
	idOrder := []int{f.runbook, f.retire, f.translate, f.rotate}
	createdOrder := []int{f.runbook, f.rotate, f.retire, f.translate}

	// The seed really does discriminate: if any of these coincided, the assertion
	// below would pass for a board that ignored priority entirely.
	if equalIDs(wantOrder, idOrder) || equalIDs(wantOrder, createdOrder) {
		t.Fatalf("the ordering seed does not discriminate: the expected order %v coincides with "+
			"the id order %v or the created_at order %v", wantOrder, idOrder, createdOrder)
	}

	positions := make([]int, len(wantOrder))
	for i, id := range wantOrder {
		at := strings.Index(backlog, cardMarker(id))
		if at < 0 {
			t.Fatalf("task #%d has no card in the BACKLOG column", id)
		}
		positions[i] = at
	}
	for i := 1; i < len(positions); i++ {
		if positions[i-1] >= positions[i] {
			t.Errorf("the BACKLOG cards are out of order: task #%d must precede task #%d "+
				"(priority DESC, created_at ASC); rendered positions %v",
				wantOrder[i-1], wantOrder[i], positions)
		}
	}
}

// ==================== CARD CONTENT ====================

// TestTaskBoard_CardContent is the gate for Acceptance Criterion 85: the card
// shows the reference line, the title, the priority and severity badges in the
// SPEC's colour variants, and a metadata footer holding only the indicators the
// task actually has — and it shows no status badge, because the column already
// states the status.
func TestTaskBoard_CardContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	columns := boardColumns(t, servePage(t, mux, "/roadmaps/"+f.name+"/tasks"))
	card := cardSlice(t, columns[1], f.passkey) // the SPRINT column's single card

	// 1. The reference line: #id and the task type, as muted text, with no colour
	//    applied to the type (no badge, no bg-*-lt class on it).
	ref := `<div class="small text-secondary" data-role="task-card-ref">#` +
		itoa(f.passkey) + ` &middot; ` + string(models.TypeUserStory) + `</div>`
	if !strings.Contains(card, ref) {
		t.Errorf("the card's reference line is not %q\ncard: %s", ref, card)
	}

	// 2. The title, as the card's prominent main content.
	if !strings.Contains(card,
		`data-role="task-card-title">Add WebAuthn passkey support to checkout</div>`) {
		t.Errorf("the card does not show the task title as its main content\ncard: %s", card)
	}

	// 3. The priority and severity badges, in the variants the mapping assigns:
	//    priority 7 -> bg-red-lt (high band), severity 4 -> bg-yellow-lt (medium).
	if !strings.Contains(card, `<span class="badge bg-red-lt">7</span>`) {
		t.Errorf("the card does not show priority 7 as a bg-red-lt badge\ncard: %s", card)
	}
	if !strings.Contains(card, `<span class="badge bg-yellow-lt">4</span>`) {
		t.Errorf("the card does not show severity 4 as a bg-yellow-lt badge\ncard: %s", card)
	}

	// 4. No status badge: the column states the status. SPRINT maps to bg-cyan-lt,
	//    which must appear nowhere on the card.
	if strings.Contains(card, "bg-cyan-lt") || strings.Contains(card, ">SPRINT<") {
		t.Errorf("the card carries a status badge; the column already states the status\ncard: %s", card)
	}

	// 5. The metadata footer, holding the indicators this task has: its sprint,
	//    its specialists, the task it blocks, and its two comments.
	meta := metaFooter(t, card)
	for _, want := range []string{
		"Checkout hardening (Sprint #" + itoa(f.checkoutSprint) + ")",
		"go-developer, security-review",
		"Blocks: 1",
		"Comments: 2",
	} {
		if !strings.Contains(meta, want) {
			t.Errorf("the card's metadata footer does not show %q\nfooter: %s", want, meta)
		}
	}
	// And not the ones it does not have: it has no subtask and depends on nothing.
	for _, absent := range []string{"Subtasks:", "Depends on:"} {
		if strings.Contains(meta, absent) {
			t.Errorf("the card's metadata footer shows %q for a task that has none\nfooter: %s", absent, meta)
		}
	}

	// The card of the DOING task carries the two counts the passkey card does not,
	// so the assertions above cannot pass through a footer that simply omits
	// everything.
	doing := cardSlice(t, columns[2], f.ledger)
	doingMeta := metaFooter(t, doing)
	for _, want := range []string{
		"Ledger reconciliation (Sprint #" + itoa(f.ledgerSprint) + ")",
		"Subtasks: 2",
		"Depends on: 1",
	} {
		if !strings.Contains(doingMeta, want) {
			t.Errorf("the DOING card's metadata footer does not show %q\nfooter: %s", want, doingMeta)
		}
	}
	for _, absent := range []string{"Blocks:", "Comments:"} {
		if strings.Contains(doingMeta, absent) {
			t.Errorf("the DOING card's metadata footer shows %q for a task that has none\nfooter: %s",
				absent, doingMeta)
		}
	}
}

// metaFooter returns the metadata footer of one card, or "" when the card renders
// none.
func metaFooter(t *testing.T, card string) string {
	t.Helper()

	at := strings.Index(card, `data-role="task-card-meta"`)
	if at < 0 {
		return ""
	}
	end := strings.Index(card[at:], "</div>")
	if end < 0 {
		t.Fatalf("the card's metadata footer is not closed\ncard: %s", card)
	}
	return card[at : at+end]
}

// TestTaskBoard_AbsentMetadataRendersNothing is the gate for the second half of
// Acceptance Criterion 85 and for Acceptance Criterion 91: an indicator whose
// value is absent, empty, or zero renders nothing at all — no dash and no
// placeholder — and a task with none of the six renders no metadata footer.
func TestTaskBoard_AbsentMetadataRendersNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	columns := boardColumns(t, servePage(t, mux, "/roadmaps/"+f.name+"/tasks"))

	// The TESTING task belongs to no sprint, names no specialist, has no subtask,
	// no dependency, nothing it blocks, and no comment: its card renders no
	// metadata footer at all.
	bare := cardSlice(t, columns[3], f.cookies)
	if strings.Contains(bare, `data-role="task-card-meta"`) {
		t.Errorf("a task with no metadata renders a metadata footer\ncard: %s", bare)
	}
	for _, placeholder := range []string{"&mdash;", "None", "Sprint #", "Subtasks:", "Depends on:",
		"Blocks:", "Comments:"} {
		if strings.Contains(bare, placeholder) {
			t.Errorf("a task with no metadata renders %q on its card\ncard: %s", placeholder, bare)
		}
	}
	// The card itself is still a full card: the absence is of indicators, not of
	// the task.
	if !strings.Contains(bare, "Audit the session-cookie flags") {
		t.Errorf("the metadata-free card lost its title\ncard: %s", bare)
	}
	if !strings.Contains(bare, `<span class="badge bg-yellow-lt">5</span>`) {
		t.Errorf("the metadata-free card lost its priority badge\ncard: %s", bare)
	}

	// A partial footer holds only what the task has: the BACKLOG task with one
	// comment and nothing else shows exactly one indicator.
	partial := metaFooter(t, cardSlice(t, columns[0], f.runbook))
	if !strings.Contains(partial, "Comments: 1") {
		t.Errorf("the card of a commented BACKLOG task does not show its comment count\nfooter: %s", partial)
	}
	for _, absent := range []string{"Sprint #", "Subtasks:", "Depends on:", "Blocks:", "&mdash;"} {
		if strings.Contains(partial, absent) {
			t.Errorf("a partial metadata footer shows %q\nfooter: %s", absent, partial)
		}
	}
}

// TestTaskBoard_SprintIndicator is the gate for Acceptance Criterion 91: the card
// of a task that belongs to a sprint names that sprint by its title together with
// "Sprint #<id>", as plain text and not as a link, exactly once and never as a
// list; the card of a task that belongs to no sprint names none at all.
func TestTaskBoard_SprintIndicator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	columns := boardColumns(t, servePage(t, mux, "/roadmaps/"+f.name+"/tasks"))

	// Each card names ITS OWN sprint: the two sprinted tasks belong to different
	// sprints, so a resolution that answered with one sprint for every task, or
	// with the wrong one, fails here.
	for _, c := range []struct {
		wantSprint string
		column     int
		taskID     int
	}{
		{"Checkout hardening (Sprint #" + itoa(f.checkoutSprint) + ")", 1, f.passkey},
		{"Ledger reconciliation (Sprint #" + itoa(f.ledgerSprint) + ")", 2, f.ledger},
	} {
		card := cardSlice(t, columns[c.column], c.taskID)

		if got := strings.Count(card, `data-role="task-card-sprint"`); got != 1 {
			t.Errorf("task #%d's card names %d sprints, want exactly 1", c.taskID, got)
		}
		if !strings.Contains(card, c.wantSprint) {
			t.Errorf("task #%d's card does not name %q\ncard: %s", c.taskID, c.wantSprint, card)
		}
		// Plain text, not a link: the whole card is the single activation target.
		if strings.Contains(card, "<a ") || strings.Contains(card, "href=") {
			t.Errorf("task #%d's card carries a link; the sprint indicator is plain text\ncard: %s",
				c.taskID, card)
		}
	}

	// A task in no sprint shows no sprint indicator at all — no dash, no "None",
	// no empty slot. Every BACKLOG task, the TESTING task, and both COMPLETED
	// subtasks are in no sprint.
	for _, c := range []struct {
		column int
		taskID int
	}{
		{0, f.runbook}, {0, f.rotate}, {3, f.cookies}, {4, f.parser}, {4, f.backfill},
	} {
		card := cardSlice(t, columns[c.column], c.taskID)
		if strings.Contains(card, `data-role="task-card-sprint"`) || strings.Contains(card, "Sprint #") {
			t.Errorf("task #%d belongs to no sprint but its card names one\ncard: %s", c.taskID, card)
		}
	}
}

// ==================== THE MODAL, AND READ-ONLY ====================

// TestTaskBoard_CardOpensTheReadOnlyModal is the gate for Acceptance Criteria 86
// and 87: selecting a card opens that task's read-only detail modal, the card
// carries the same keyboard and ARIA treatment as the sprint page's clickable
// task rows, and the board itself offers no control that changes anything.
func TestTaskBoard_CardOpensTheReadOnlyModal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")
	region := boardRegion(t, body)
	columns := boardColumns(t, body)

	for status, ids := range f.tasksByStatus() {
		for _, id := range ids {
			column := columns[statusIndex(t, status)]
			card := cardSlice(t, column, id)

			// The card is the modal's trigger, and is operable from the keyboard
			// with the same role and label the sprint page's task rows carry.
			for _, attr := range []string{
				`data-bs-toggle="modal"`,
				`data-bs-target="#task-modal-` + itoa(id) + `"`,
				`tabindex="0"`,
				`role="button"`,
				`aria-label="Open details for task #` + itoa(id) + `"`,
			} {
				if !strings.Contains(card, attr) {
					t.Errorf("task #%d's card is missing %s\ncard: %s", id, attr, card)
				}
			}

			// Exactly one modal per task, rendered into the page itself, so opening
			// it costs no request and no query.
			if got := strings.Count(body, `id="task-modal-`+itoa(id)+`"`); got != 1 {
				t.Errorf("task #%d has %d detail modals on the page, want exactly 1", id, got)
			}
		}
	}

	// The board region is read-only: no form, no input, no button, no link, and no
	// write-method submission anywhere in it. (The modals live outside this region
	// and carry only Bootstrap's own dismiss control.)
	lower := strings.ToLower(region)
	for _, forbidden := range []string{"<form", "<input", "<button", "<select", "<textarea", "<a ",
		`method="post"`, "draggable=", "ondrag"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("the board is read-only but contains %q", forbidden)
		}
	}
}

// statusIndex returns the board column index of a status.
func statusIndex(t *testing.T, status models.TaskStatus) int {
	t.Helper()

	for i, s := range models.ValidTaskStatuses {
		if s == status {
			return i
		}
	}
	t.Fatalf("status %s has no board column", status)
	return -1
}

// ==================== EMPTY STATES ====================

// TestTaskBoard_EmptyStates is the gate for Acceptance Criterion 88: an empty
// column renders its own empty state inside the column while keeping its title
// and its 0 count badge, and a roadmap with no task renders the whole board with
// all five columns present, each showing that empty state — never a page-level
// empty state in place of the board, and never a dropped column.
func TestTaskBoard_EmptyStates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	if err := createEmptyRoadmap("payment-platform-empty"); err != nil {
		t.Fatalf("creating the empty roadmap: %v", err)
	}
	mux := buildMux()

	// A populated board: the columns that hold tasks show cards, and no empty
	// state; a column holding none shows the empty state and keeps its header.
	// seedRoadmap's roadmap holds one SPRINT task, so its other four columns are
	// empty on an otherwise populated board.
	sparse := seedRoadmap(t, "checkout-rollout")
	for i, column := range boardColumns(t, servePage(t, mux, "/roadmaps/"+sparse+"/tasks")) {
		status, count := columnHeader(t, column)
		empty := strings.Contains(column, `data-role="task-board-column-empty"`)

		if models.ValidTaskStatuses[i] == models.StatusSprint {
			if empty {
				t.Errorf("column %s holds a task but renders the empty state", status)
			}
			continue
		}
		if !empty {
			t.Errorf("column %s holds no task but renders no empty state", status)
		}
		if count != 0 {
			t.Errorf("the empty column %s shows the count %d, want 0", status, count)
		}
		// Tabler's own empty-state markup, and the column's identity intact.
		for _, want := range []string{`<p class="empty-title">`, `class="empty-subtitle`, status} {
			if !strings.Contains(column, want) {
				t.Errorf("the empty column %s does not render %q", status, want)
			}
		}
	}

	// A roadmap with no task at all: five columns, five empty states, no cards,
	// and the board itself is still there.
	body := servePage(t, mux, "/roadmaps/payment-platform-empty/tasks")
	columns := boardColumns(t, body)
	if len(columns) != len(models.ValidTaskStatuses) {
		t.Fatalf("the empty board renders %d columns, want %d", len(columns), len(models.ValidTaskStatuses))
	}
	for i, column := range columns {
		status, count := columnHeader(t, column)
		if status != string(models.ValidTaskStatuses[i]) {
			t.Errorf("the empty board's column %d is titled %q, want %q",
				i, status, models.ValidTaskStatuses[i])
		}
		if count != 0 {
			t.Errorf("the empty board's column %s shows the count %d, want 0", status, count)
		}
		if !strings.Contains(column, `data-role="task-board-column-empty"`) {
			t.Errorf("the empty board's column %s renders no empty state", status)
		}
		if strings.Contains(column, cardOpen) {
			t.Errorf("the empty board's column %s renders a card", status)
		}
	}
	// The board was not replaced by a page-level empty state.
	if !strings.Contains(boardRegion(t, body), `data-role="task-board"`) {
		t.Errorf("a roadmap with no task renders no board; the five columns are fixed")
	}
	// The populated roadmap is unaffected by any of this.
	if got := len(boardColumns(t, servePage(t, mux, "/roadmaps/"+f.name+"/tasks"))); got != 5 {
		t.Errorf("the populated board renders %d columns, want 5", got)
	}
}

// ==================== MARKUP RULES ====================

// TestTaskBoard_MarkupObeysTheRulesInForce is the gate for Acceptance Criterion
// 90 on a FULLY POPULATED board: every class the board emits resolves to a rule
// in one of the embedded stylesheets, no element carries an inline style, and the
// components Tabler provides are Tabler's own.
//
// The general guard, TestTablerFidelity_NoClassOutsideTheVendoredStylesheets,
// renders the pages of a roadmap whose single task carries almost no metadata, so
// most of the card's classes and icons never reach it. This test runs the same
// check over a board where every indicator is present.
func TestTaskBoard_MarkupObeysTheRulesInForce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")
	region := boardRegion(t, body)

	sheets := make([]string, 0, len(styleSheetPaths))
	for _, p := range styleSheetPaths {
		b, err := staticFS.ReadFile(p)
		if err != nil {
			t.Fatalf("reading embedded stylesheet %s: %v", p, err)
		}
		sheets = append(sheets, string(b))
	}

	seen := 0
	for _, m := range classAttrRe.FindAllStringSubmatch(region, -1) {
		for _, class := range strings.Fields(m[1]) {
			seen++
			if _, ok := structuralHookClasses[class]; ok {
				continue
			}
			if !classDefined(sheets, class) {
				t.Errorf("the board emits class %q, which no embedded stylesheet defines and which "+
					"is not a recorded structural hook", class)
			}
		}
	}
	// Falsifiability control: an extraction that found nothing would make the
	// assertion above vacuous. A populated board emits far more than this.
	if seen < 30 {
		t.Fatalf("extracted only %d class tokens from the board; the extraction is broken", seen)
	}

	// No inline style anywhere in the board.
	if strings.Contains(region, "style=") {
		t.Errorf("the board carries an inline style attribute")
	}

	// The components Tabler provides are Tabler's: cards for the columns and the
	// task cards, the card-header idiom for the column headers, badges for the
	// counts and the values, and the empty-state markup for an empty column.
	for _, want := range []string{
		`<div class="card task-board__column"`,
		`<div class="card-header">`,
		`<h3 class="card-title">`,
		cardOpen,
		`<span class="badge bg-secondary-lt ms-2">`,
	} {
		if !strings.Contains(region, want) {
			t.Errorf("the board does not use Tabler's own markup %q", want)
		}
	}
}

// ==================== READ COST ====================

// TestTasksPage_IssuesThreeReadsAndNoneMore is the gate for Acceptance Criteria
// 89 and 92: rendering the board issues exactly three reads — the task list, one
// grouped comment query, one grouped sprint query — whatever the number of tasks,
// of sprints, and of columns; and a roadmap with no task issues only the task
// list.
//
// It measures on the same counting source Acceptance Criterion 70 is measured
// with, so the two counts are taken on one instrument rather than two.
func TestTasksPage_IssuesThreeReadsAndNoneMore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// A board with tasks spread over two sprints and five columns.
	f := seedBoardFixture(t, "payment-platform")
	src := openCounting(t, f.name)

	data, err := readTasks(context.Background(), src, f.name)
	if err != nil {
		t.Fatalf("readTasks: %v", err)
	}

	if src.taskList != 1 {
		t.Errorf("the board issued %d task-list queries, want 1", src.taskList)
	}
	if src.groupedTaskComments != 1 {
		t.Errorf("the board issued %d comment queries, want exactly 1", src.groupedTaskComments)
	}
	if src.boundedTaskList != 0 {
		t.Errorf("the board issued %d bounded task-list queries, want 0: it reads every task",
			src.boundedTaskList)
	}
	if src.groupedTaskSprints != 1 {
		t.Errorf("the board issued %d sprint queries, want exactly 1", src.groupedTaskSprints)
	}
	if src.perTaskComments != 0 {
		t.Errorf("the board issued %d per-task comment queries, want 0", src.perTaskComments)
	}

	// The one sprint query covered EVERY rendered task, which is what makes one
	// query sufficient rather than merely few.
	if len(src.lastSprintIDs) != len(data.Tasks) {
		t.Errorf("the sprint query was given %d ids, want the board's %d tasks",
			len(src.lastSprintIDs), len(data.Tasks))
	}
	for i := range data.Tasks {
		if i < len(src.lastSprintIDs) && src.lastSprintIDs[i] != data.Tasks[i].ID {
			t.Errorf("the sprint query id at %d is #%d, want #%d",
				i, src.lastSprintIDs[i], data.Tasks[i].ID)
		}
	}

	// The five columns were grouped in memory: the counts add up to the task list
	// the single read returned, with no query of their own.
	grouped := 0
	for i := range data.Columns {
		grouped += data.Columns[i].Count
	}
	if grouped != len(data.Tasks) {
		t.Errorf("the columns hold %d tasks, want the %d the task list returned", grouped, len(data.Tasks))
	}

	// The count does not grow with the number of tasks: the same three reads for
	// 1, 3, and 12 of them.
	for _, taskCount := range []int{1, 3, 12} {
		name := "reconciliation-window-" + itoa(taskCount)
		seedTasksWithComments(t, name, taskCount)
		counted := openCounting(t, name)

		if _, err := readTasks(context.Background(), counted, name); err != nil {
			t.Fatalf("%d tasks: readTasks: %v", taskCount, err)
		}
		if counted.taskList != 1 || counted.groupedTaskComments != 1 || counted.groupedTaskSprints != 1 {
			t.Errorf("%d tasks: the board issued %d task-list, %d comment, and %d sprint queries; "+
				"want 1, 1 and 1", taskCount, counted.taskList,
				counted.groupedTaskComments, counted.groupedTaskSprints)
		}

		// The control that makes those counts falsifiable: the per-task alternative
		// the SPEC forbids, measured on the same instrument.
		counted.groupedTaskSprints = 0
		for i := range taskCount {
			if _, err := counted.GetSprintsByTasks(context.Background(), []int{i + 1}); err != nil {
				t.Fatalf("%d tasks: per-task control read: %v", taskCount, err)
			}
		}
		if counted.groupedTaskSprints != taskCount {
			t.Errorf("%d tasks: the per-task control issued %d reads, want %d; the instrument "+
				"does not track reads one-for-one", taskCount, counted.groupedTaskSprints, taskCount)
		}
	}

	// A roadmap with no task issues the task-list read only: both grouped reads
	// take a set of rendered task ids, and that set is empty.
	const emptyName = "reconciliation-window-none"
	seedTasksWithComments(t, emptyName, 0)
	emptySrc := openCounting(t, emptyName)

	emptyData, err := readTasks(context.Background(), emptySrc, emptyName)
	if err != nil {
		t.Fatalf("empty roadmap: readTasks: %v", err)
	}
	if len(emptyData.Tasks) != 0 {
		t.Fatalf("the empty roadmap carries %d tasks, want 0", len(emptyData.Tasks))
	}
	if emptySrc.taskList != 1 {
		t.Errorf("the empty roadmap issued %d task-list queries, want 1", emptySrc.taskList)
	}
	if emptySrc.groupedTaskComments != 0 {
		t.Errorf("the empty roadmap issued %d comment queries, want 0", emptySrc.groupedTaskComments)
	}
	if emptySrc.groupedTaskSprints != 0 {
		t.Errorf("the empty roadmap issued %d sprint queries, want 0", emptySrc.groupedTaskSprints)
	}
	// The board is still five columns, all of them empty.
	if len(emptyData.Columns) != len(models.ValidTaskStatuses) {
		t.Errorf("the empty roadmap's board has %d columns, want %d",
			len(emptyData.Columns), len(models.ValidTaskStatuses))
	}
	for i := range emptyData.Columns {
		if emptyData.Columns[i].Count != 0 {
			t.Errorf("the empty roadmap's %s column holds %d tasks, want 0",
				emptyData.Columns[i].Status, emptyData.Columns[i].Count)
		}
	}
}

// ==================== SHARED TEST UTILITIES ====================

// createEmptyRoadmap creates a roadmap holding no task at all.
func createEmptyRoadmap(name string) error {
	database, err := db.Open(name)
	if err != nil {
		return err
	}
	return database.Close()
}

// countRoadmapTasks returns the number of tasks a roadmap holds, read from the
// database rather than counted from a fixture's own bookkeeping.
func countRoadmapTasks(t *testing.T, name string) int {
	t.Helper()

	database, err := db.OpenReadOnly(name)
	if err != nil {
		t.Fatalf("opening roadmap %q read-only: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	tasks, err := database.ListTasks(context.Background(), &db.TaskListFilter{Limit: models.MaxTaskLimit})
	if err != nil {
		t.Fatalf("listing the tasks of %q: %v", name, err)
	}
	return len(tasks)
}

// equalIDs reports whether two id sequences are the same, in the same order.
func equalIDs(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTaskBoard_ReadsEveryTaskBeyondTheListingLimit is the gate for the board's
// unbounded read (SPEC/WEB.md § Roadmap Tasks Page, Unbounded read;
// SPEC/DATABASE.md § Main SQL Queries, "List All").
//
// This is the test that would have caught the defect it now prevents. The board
// read through the CLI listing, whose limit is capped at models.MaxTaskLimit
// (100), so a roadmap with more tasks than that had its extra cards silently
// dropped while every column header went on presenting its count as a fact about
// the roadmap. On the project's own roadmap — 185 tasks — the page showed 100 of
// them and announced BACKLOG 3, SPRINT 6, DOING 1, COMPLETED 90.
//
// The seed is deliberately larger than the cap, and larger by an amount that
// spreads across several columns, so a truncated read cannot pass by accident.
func TestTaskBoard_ReadsEveryTaskBeyondTheListingLimit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const (
		name  = "settlement-platform"
		total = models.MaxTaskLimit + 63 // 163: well past the cap
	)
	statuses := models.ValidTaskStatuses
	wantPerColumn := seedTasksAcrossColumns(t, name, total, statuses)
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+name+"/tasks")
	region := boardRegion(t, body)
	columns := boardColumns(t, body)

	// Every card is on the board: the count of rendered cards is the roadmap's
	// whole task count, not the listing cap.
	if got := strings.Count(region, cardOpen); got != total {
		t.Errorf("the board renders %d cards, want every one of the roadmap's %d tasks "+
			"(the CLI listing cap is %d)", got, total, models.MaxTaskLimit)
	}

	// And the counts on the column headers sum to it, which is the property the
	// unbounded read exists for: a count presented as a fact must be one.
	sum := 0
	for i, column := range columns {
		status, count := columnHeader(t, column)
		sum += count

		if want := wantPerColumn[statuses[i]]; count != want {
			t.Errorf("column %s shows the count %d, want %d", status, count, want)
		}
		if cards := strings.Count(column, cardOpen); cards != count {
			t.Errorf("column %s shows the count %d but renders %d cards", status, count, cards)
		}
	}
	if sum != total {
		t.Errorf("the column counts sum to %d, want the roadmap's %d tasks", sum, total)
	}
	if sum <= models.MaxTaskLimit {
		t.Fatalf("the seed produced %d tasks, which does not exceed the listing cap of %d; "+
			"this test would pass against a truncated read", sum, models.MaxTaskLimit)
	}

	// Each task is reachable individually, so the cards beyond the cap are real
	// cards and not filler: the last one seeded opens its own detail.
	tasks := roadmapTaskTitles(t, name)
	if len(tasks) != total {
		t.Fatalf("the roadmap holds %d tasks, want %d", len(tasks), total)
	}
	for id := range tasks {
		if !strings.Contains(region, cardMarker(id)) {
			t.Errorf("task #%d has no card on the board", id)
		}
	}
}

// seedTasksAcrossColumns creates n tasks spread over the given statuses and
// returns how many landed in each. The spread is deterministic and uneven, so a
// column-count assertion measures the grouping rather than a uniform division.
func seedTasksAcrossColumns(t *testing.T, name string, n int,
	statuses []models.TaskStatus) map[models.TaskStatus]int {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	counts := make(map[models.TaskStatus]int, len(statuses))
	for i := range n {
		// An uneven spread: the columns receive 1/2, 1/4, 1/8, ... of the tasks.
		status := statuses[len(statuses)-1]
		for s, step := 0, 2; s < len(statuses)-1; s, step = s+1, step*2 {
			if i%step == 0 {
				status = statuses[s]
				break
			}
		}
		if _, cerr := database.CreateTask(ctx, &models.Task{
			Priority:               i % 10,
			Severity:               i % 10,
			Status:                 status,
			Type:                   models.TypeTask,
			Title:                  "Reconcile settlement window " + itoa(i+1),
			FunctionalRequirements: "Every settlement window must balance against the acquirer report.",
			TechnicalRequirements:  "Match both sides by window and report the residual.",
			AcceptanceCriteria:     "A day's windows reconcile with a zero residual.",
			CreatedAt:              "2026-03-01T08:00:00Z",
		}); cerr != nil {
			t.Fatalf("creating task %d: %v", i+1, cerr)
		}
		counts[status]++
	}
	return counts
}

// roadmapTaskTitles returns every task of a roadmap keyed by id, read through the
// same read-only path the pages use. Reading a title from the roadmap rather than
// from the markup under test is what keeps an assertion about rendered text
// non-circular.
func roadmapTaskTitles(t *testing.T, roadmap string) map[int]string {
	t.Helper()

	database, err := db.OpenReadOnly(roadmap)
	if err != nil {
		t.Fatalf("opening roadmap %q read-only: %v", roadmap, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	tasks, err := database.ListAllTasks(context.Background())
	if err != nil {
		t.Fatalf("listing the tasks of %q: %v", roadmap, err)
	}
	titles := make(map[int]string, len(tasks))
	for i := range tasks {
		titles[tasks[i].ID] = tasks[i].Title
	}
	return titles
}
