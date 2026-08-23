package web

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for the roadmap tasks board's three header filters — the
// type filter, the minimum-priority filter and the minimum-severity filter
// (SPEC/WEB.md § Roadmap Tasks Page, Header filter controls through How the
// criteria compose; Acceptance Criteria 112 to 117).
//
// The filters are not a second narrowing mechanism beside the search: each is ONE
// criterion in the same conjunction, computed in one place on the server
// (taskView.matches) and in one place in the browser (static/task-search.js,
// `matches`). The property that matters is therefore the same property the search
// has, widened to four controls: for any roadmap and any combination of a term
// and the three filters, the board reached by operating the controls and the
// board reached by requesting the URL carrying those values are the SAME board.
// TestTaskFilters_ServerAndClientProduceTheSameBoard compares the two over a full
// sweep of combinations, and TestTaskFilters_ScriptAppliesTheSameConjunction pins
// that the served script really is the rule that comparison re-expresses.

// ==================== FIXTURE ====================

// filterTask is one seeded task together with the four values the header controls
// act on, so an expectation can be computed from the FIXTURE rather than read
// back out of the page under test.
type filterTask struct {
	title    string
	taskType models.TaskType
	status   models.TaskStatus
	created  string
	priority int
	severity int
	id       int
}

// filterFixture is a roadmap seeded so that no filter assertion can pass by
// accident.
type filterFixture struct {
	name  string
	tasks []filterTask
}

// filterSeed is the seed, and it is built around four properties:
//
//  1. All TEN TaskType values appear, so the type dropdown can be exercised
//     across the whole enum and an option set that lost one is visible.
//  2. Type, priority and severity are mutually UNCORRELATED: no type maps onto a
//     priority band and no priority onto a severity band, so a filter that
//     silently compared the wrong dimension selects a different set.
//  3. Both boundaries of every threshold are populated — there are tasks at
//     priority 0 and at 9, and at severity 0 and at 9 — so `>=` and `>` select
//     different sets and an off-by-one is visible.
//  4. The worked example of Acceptance Criterion 114, `?q=cache&type=BUG&
//     priority=7`, has a task excluded by EACH of its three criteria and no
//     other: cacheReport and cacheInvalidation match, cacheWarmup fails only the
//     priority, cacheCatalogue fails only the type, and cacheCurrency fails only
//     the type while legacyCache fails only the priority. Dropping any one of the
//     three criteria therefore changes the answer.
var filterSeed = []filterTask{
	// The "cache" family: the worked example's discriminators.
	{title: "Cache the acquirer settlement report", taskType: models.TypeBug, priority: 8, severity: 7, status: models.StatusBacklog, created: "2026-03-01T09:00:00Z"},
	{title: "Cache invalidation drops the refund receipt", taskType: models.TypeBug, priority: 7, severity: 3, status: models.StatusDoing, created: "2026-03-02T09:00:00Z"},
	{title: "Cache warmup exceeds the deployment window", taskType: models.TypeBug, priority: 6, severity: 9, status: models.StatusBacklog, created: "2026-03-03T09:00:00Z"},
	{title: "Cache the merchant catalogue in the edge tier", taskType: models.TypeEpic, priority: 9, severity: 2, status: models.StatusTesting, created: "2026-03-04T09:00:00Z"},
	{title: "Cache the currency conversion table", taskType: models.TypeTask, priority: 7, severity: 7, status: models.StatusTesting, created: "2026-03-05T09:00:00Z"},
	{title: "Retire the legacy settlement cache", taskType: models.TypeBug, priority: 2, severity: 9, status: models.StatusCompleted, created: "2026-03-06T09:00:00Z"},
	// A high-priority BUG that says nothing about a cache, so dropping the term
	// from the worked example widens the board and the term is provably doing work.
	{title: "Duplicate payout on a retried webhook", taskType: models.TypeBug, priority: 9, severity: 5, status: models.StatusDoing, created: "2026-03-18T09:00:00Z"},

	// The remaining types, spread across the priority and severity ranges.
	{title: "Rotate the acquirer signing keys", taskType: models.TypeChore, priority: 9, severity: 8, status: models.StatusBacklog, created: "2026-03-07T09:00:00Z"},
	{title: "Publish the acquirer onboarding runbook", taskType: models.TypeChore, priority: 0, severity: 0, status: models.StatusBacklog, created: "2026-03-08T09:00:00Z"},
	{title: "Draft the payout reconciliation story", taskType: models.TypeUserStory, priority: 5, severity: 1, status: models.StatusBacklog, created: "2026-03-09T09:00:00Z"},
	{title: "Split the ledger writer into its own package", taskType: models.TypeRefactor, priority: 4, severity: 0, status: models.StatusCompleted, created: "2026-03-10T09:00:00Z"},
	{title: "Investigate the settlement latency spike", taskType: models.TypeSpike, priority: 3, severity: 6, status: models.StatusDoing, created: "2026-03-11T09:00:00Z"},
	{title: "Redesign the refund confirmation screen", taskType: models.TypeDesignUX, priority: 2, severity: 2, status: models.StatusTesting, created: "2026-03-12T09:00:00Z"},
	{title: "Backfill the dispute evidence index", taskType: models.TypeSubTask, priority: 1, severity: 5, status: models.StatusCompleted, created: "2026-03-13T09:00:00Z"},
	{title: "Improve the payout scheduling heuristics", taskType: models.TypeImprovement, priority: 7, severity: 4, status: models.StatusBacklog, created: "2026-03-14T09:00:00Z"},
	{title: "Migrate the chargeback importer to the new API", taskType: models.TypeTask, priority: 6, severity: 6, status: models.StatusDoing, created: "2026-03-15T09:00:00Z"},

	// Two tasks that join a sprint, which is what puts cards in the SPRINT column.
	{title: "Harden the webhook retry budget", taskType: models.TypeTask, priority: 8, severity: 3, status: models.StatusSprint, created: "2026-03-16T09:00:00Z"},
	{title: "Meter the dispute evidence uploads", taskType: models.TypeImprovement, priority: 9, severity: 9, status: models.StatusSprint, created: "2026-03-17T09:00:00Z"},
}

// seedFilterFixture creates the roadmap of filterSeed and records the id each
// task received, so every expectation below binds ids that actually exist.
func seedFilterFixture(t *testing.T, name string) filterFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	fixture := filterFixture{name: name, tasks: append([]filterTask(nil), filterSeed...)}

	sprintMembers := []int{}
	for i := range fixture.tasks {
		seed := &fixture.tasks[i]

		// A task reaches the SPRINT column by joining a sprint, which is the only
		// transition into it the state machine admits; everything else is created
		// directly in its own column.
		created := seed.status
		if created == models.StatusSprint {
			created = models.StatusBacklog
		}

		id, cerr := seedTask(database, &models.Task{
			Title:                  seed.title,
			Type:                   seed.taskType,
			Status:                 created,
			Priority:               seed.priority,
			Severity:               seed.severity,
			FunctionalRequirements: "Operators must be able to narrow the board to this work.",
			TechnicalRequirements:  "Read-only on the web side, over the roadmap database.",
			AcceptanceCriteria:     "The board shows the task under every control that admits it.",
			CreatedAt:              seed.created,
		})
		if cerr != nil {
			t.Fatalf("creating task %q: %v", seed.title, cerr)
		}
		seed.id = id
		if seed.status == models.StatusSprint {
			sprintMembers = append(sprintMembers, id)
		}
	}

	sprint := newSprint(t, database, "Dispute and payout hardening",
		"Harden the dispute evidence pipeline and the payout scheduler.")
	if err := database.AddTasksToSprint(ctx, sprint, sprintMembers); err != nil {
		t.Fatalf("adding the sprint members: %v", err)
	}

	return fixture
}

// expect computes, per column and in render order, the ids the board MUST show
// for a predicate.
//
// The order comes from the unnarrowed board — narrowing removes cards, it never
// reorders the ones that remain, which TestTaskSearch_PreservesTheOrderWithinAColumn
// pins — while the verdict comes from the FIXTURE's own field values. Neither
// comes from the narrowed page under test, so a page that filtered by the wrong
// dimension cannot satisfy its own expectation.
func (f filterFixture) expect(unnarrowed boardState, keep func(*filterTask) bool) map[string][]int {
	admitted := make(map[int]bool, len(f.tasks))
	for i := range f.tasks {
		if keep(&f.tasks[i]) {
			admitted[f.tasks[i].id] = true
		}
	}

	expected := make(map[string][]int, len(unnarrowed.columns))
	for _, column := range unnarrowed.columns {
		ids := []int{}
		for _, card := range column.cards {
			if admitted[card.id] {
				ids = append(ids, card.id)
			}
		}
		expected[column.status] = ids
	}
	return expected
}

// assertBoardShows checks a served board against an expectation, on all three
// things the board states about itself: the cards each column shows, in order,
// the count on each column header, and each column's empty state.
func assertBoardShows(t *testing.T, what string, state boardState, expected map[string][]int) {
	t.Helper()

	if len(state.columns) != len(models.ValidTaskStatuses) {
		t.Fatalf("%s: the board has %d columns, want %d — narrowing may drop no column",
			what, len(state.columns), len(models.ValidTaskStatuses))
	}
	for i, column := range state.columns {
		if got, want := column.status, string(models.ValidTaskStatuses[i]); got != want {
			t.Errorf("%s: column %d is %s, want %s — narrowing may reorder no column", what, i, got, want)
		}
		want := expected[column.status]
		if got := shownOf(column); !equalIDs(got, want) {
			t.Errorf("%s: column %s shows %v, want %v", what, column.status, got, want)
		}
		if column.count != len(want) {
			t.Errorf("%s: column %s counts %d, want %d — the count states what the column shows",
				what, column.status, column.count, len(want))
		}
		if got := column.emptyShown; got != (len(want) == 0) {
			t.Errorf("%s: column %s shows its empty state = %v, want %v",
				what, column.status, got, len(want) == 0)
		}
	}
}

// ==================== THE HEADER CONTROLS ====================

var (
	reSelectBlock = regexp.MustCompile(`(?s)<select[^>]*\bid="([^"]+)"[^>]*>(.*?)</select>`)
	reOptionTag   = regexp.MustCompile(`<option value="([^"]*)"([^>]*)>([^<]*)</option>`)
)

// selectOption is one rendered <option>.
type selectOption struct {
	value    string
	label    string
	selected bool
}

// readSelect returns the options of the select carrying an id, and fails when the
// page renders no such control.
func readSelect(t *testing.T, body, id string) []selectOption {
	t.Helper()

	for _, block := range reSelectBlock.FindAllStringSubmatch(body, -1) {
		if block[1] != id {
			continue
		}
		options := []selectOption{}
		for _, option := range reOptionTag.FindAllStringSubmatch(block[2], -1) {
			options = append(options, selectOption{
				value:    option[1],
				label:    option[3],
				selected: strings.Contains(option[2], "selected"),
			})
		}
		return options
	}
	t.Fatalf("the page renders no <select id=%q>", id)
	return nil
}

// selectedValue returns the value of the one selected option, and fails unless
// exactly one is selected.
func selectedValue(t *testing.T, options []selectOption, id string) string {
	t.Helper()

	selected := []string{}
	for _, option := range options {
		if option.selected {
			selected = append(selected, option.value)
		}
	}
	if len(selected) != 1 {
		t.Fatalf("select %q has %d selected options (%v), want exactly 1", id, len(selected), selected)
	}
	return selected[0]
}

// searchInputValue returns the term the served search input echoes back, decoded.
func searchInputValue(t *testing.T, body string) string {
	t.Helper()

	input := reSearchInput.FindString(body)
	if input == "" {
		t.Fatalf("the page renders no search input")
	}
	value := reInputValue.FindStringSubmatch(input)
	if value == nil {
		t.Fatalf("the search input carries no value attribute: %s", input)
	}
	return html.UnescapeString(value[1])
}

// filterControlIDs is the three dropdowns, in the order the header renders them,
// paired with the URL parameter each carries.
var filterControlIDs = []struct{ id, param string }{
	{"task-filter-type", "type"},
	{"task-filter-priority", "priority"},
	{"task-filter-severity", "severity"},
}

// TestTaskFilters_HeaderCarriesTheThreeDropdowns is the gate for Acceptance
// Criterion 112: the actions column carries exactly three filter dropdowns beside
// the search input, each offering its dimension's fixed option set with a
// no-filter first option, each carrying a real programmatically associated label,
// each keyboard-operable, and none of them filtering by status.
func TestTaskFilters_HeaderCarriesTheThreeDropdowns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedFilterFixture(t, "delivery-platform")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")
	header := headerRegion(t, "/roadmaps/"+f.name+"/tasks", body)

	// Exactly three dropdowns, all of them in the header's actions column.
	if got := len(reSelectBlock.FindAllString(header, -1)); got != 3 {
		t.Errorf("the header carries %d <select> controls, want exactly 3", got)
	}
	if got := len(reSelectBlock.FindAllString(body, -1)); got != 3 {
		t.Errorf("the page carries %d <select> controls, want exactly 3 — all of them in the header", got)
	}
	if !strings.Contains(header, `data-role="task-search"`) {
		t.Errorf("the header lost the search input the filters sit beside")
	}

	// Every control names its dimension through a REAL label, associated by
	// for/id. A first option is a value, not a name, and a placeholder is neither.
	for _, control := range append([]struct{ id, param string }{{"task-search", "q"}}, filterControlIDs...) {
		label := regexp.MustCompile(`<label[^>]*\bfor="` + control.id + `"[^>]*>([^<]+)</label>`).
			FindStringSubmatch(header)
		if label == nil {
			t.Errorf("control %q carries no <label for=%q>; a placeholder or a first option may not stand in for one",
				control.id, control.id)
			continue
		}
		if strings.TrimSpace(label[1]) == "" {
			t.Errorf("control %q carries an empty label", control.id)
		}
		if !strings.Contains(header, `id="`+control.id+`"`) {
			t.Errorf("control %q is labelled but not rendered", control.id)
		}
	}

	// The controls are native form elements, so the keyboard reaches and operates
	// them through the browser's own behaviour. A div wearing role="combobox" or
	// a tabindex would take focus while remaining impossible to operate, which is
	// the defect the sprint's earlier keyboard work removed from this page.
	for _, forbidden := range []string{`role="combobox"`, `role="listbox"`, `role="button"`, `tabindex=`} {
		if strings.Contains(header, forbidden) {
			t.Errorf("the header carries %s; the controls must be native form elements", forbidden)
		}
	}

	// The type dropdown: the no-filter option, then the ten TaskType values in the
	// enum's own order and spelling.
	types := readSelect(t, body, "task-filter-type")
	if len(types) != len(models.ValidTaskTypes)+1 {
		t.Fatalf("the type filter offers %d options, want %d (the ten TaskType values plus the no-filter option)",
			len(types), len(models.ValidTaskTypes)+1)
	}
	if types[0].value != "" || !types[0].selected {
		t.Errorf("the type filter's first option is %+v, want the empty no-filter value, selected", types[0])
	}
	if strings.TrimSpace(types[0].label) == "" {
		t.Errorf("the type filter's no-filter option carries no text")
	}
	for i, taskType := range models.ValidTaskTypes {
		if got := types[i+1]; got.value != string(taskType) || got.label != string(taskType) {
			t.Errorf("type option %d is %+v, want the enum value %q", i+1, got, taskType)
		}
	}

	// The two threshold dropdowns: the no-filter option, then 1 to 9. 0 is NOT
	// offered — a threshold of 0 admits every task and IS the unfiltered board,
	// which already has its own option and its own URL form.
	for _, id := range []string{"task-filter-priority", "task-filter-severity"} {
		options := readSelect(t, body, id)
		if len(options) != 10 {
			t.Fatalf("%s offers %d options, want 10 (thresholds 1 to 9 plus the no-filter option)", id, len(options))
		}
		if options[0].value != "" || !options[0].selected {
			t.Errorf("%s's first option is %+v, want the empty no-filter value, selected", id, options[0])
		}
		for i := 1; i <= 9; i++ {
			if got, want := options[i].value, strconv.Itoa(i); got != want {
				t.Errorf("%s option %d has value %q, want %q", id, i, got, want)
			}
		}
		for _, option := range options {
			if option.value == "0" {
				t.Errorf("%s offers the threshold 0, which is the unfiltered board and would give it a second URL", id)
			}
		}
	}

	// NO status filter. The columns already are the status, so no control offers a
	// TaskStatus value and no control narrows the columns themselves.
	for _, status := range models.ValidTaskStatuses {
		if strings.Contains(header, `value="`+string(status)+`"`) {
			t.Errorf("the header offers %s as a filter value; the board offers no status filter", status)
		}
	}
	if strings.Contains(header, `id="task-filter-status"`) || strings.Contains(header, `name="status"`) {
		t.Errorf("the header carries a status filter, which the board deliberately does not offer")
	}
}

// ==================== EACH FILTER, ON THE SERVER ====================

// TestTaskFilters_EachDimensionNarrowsAndCountsFollow is the gate for Acceptance
// Criterion 113: the type filter is an EQUALITY, the priority and severity
// filters are THRESHOLDS, and every column count follows the narrowed set.
func TestTaskFilters_EachDimensionNarrowsAndCountsFollow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedFilterFixture(t, "delivery-platform")
	mux := buildMux()

	unnarrowed, _ := servedBoard(t, mux, f.name, clientControls{})

	// Type: exact equality against the enum's own spelling, over all ten values.
	for _, taskType := range models.ValidTaskTypes {
		wanted := taskType
		state, _ := servedBoard(t, mux, f.name, clientControls{Type: string(taskType)})
		assertBoardShows(t, "?type="+string(taskType), state,
			f.expect(unnarrowed, func(task *filterTask) bool { return task.taskType == wanted }))
	}

	// Priority and severity: ">= n", over every offered threshold. The seed puts
	// tasks at both ends of the range, so ">" and ">=" select different sets and
	// the two comparisons cannot be confused for one another.
	for threshold := minFilterThreshold; threshold <= maxFilterThreshold; threshold++ {
		bound := threshold

		priority, _ := servedBoard(t, mux, f.name, clientControls{Priority: strconv.Itoa(threshold)})
		assertBoardShows(t, "?priority="+strconv.Itoa(threshold), priority,
			f.expect(unnarrowed, func(task *filterTask) bool { return task.priority >= bound }))

		severity, _ := servedBoard(t, mux, f.name, clientControls{Severity: strconv.Itoa(threshold)})
		assertBoardShows(t, "?severity="+strconv.Itoa(threshold), severity,
			f.expect(unnarrowed, func(task *filterTask) bool { return task.severity >= bound }))
	}

	// The threshold really is "at least", not "exactly": a task ABOVE the
	// threshold is shown. Asserting it separately makes the equality mutation of
	// the comparison fail here too, in one obvious place.
	nine, _ := servedBoard(t, mux, f.name, clientControls{Priority: "8"})
	shown := map[int]bool{}
	for _, column := range nine.columns {
		for _, id := range shownOf(column) {
			shown[id] = true
		}
	}
	above := 0
	for _, task := range f.tasks {
		if task.priority > 8 {
			above++
			if !shown[task.id] {
				t.Errorf("task #%d has priority %d and is not shown under ?priority=8; the filter is a threshold, not an equality",
					task.id, task.priority)
			}
		}
	}
	if above == 0 {
		t.Fatalf("the fixture holds no task above priority 8, so the threshold assertion is vacuous")
	}
}

// ==================== THE CONJUNCTION ====================

// TestTaskFilters_ComposeConjunctively is the gate for Acceptance Criterion 114:
// the shown set is the set of tasks satisfying EVERY active control, the worked
// example selects exactly what the specification says it selects, and activating
// a further control can only shrink the shown set.
func TestTaskFilters_ComposeConjunctively(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedFilterFixture(t, "delivery-platform")
	mux := buildMux()

	unnarrowed, _ := servedBoard(t, mux, f.name, clientControls{})

	// The specification's own worked example. It is asserted against an id set
	// named from the seed rather than against a count, so a board that showed the
	// right NUMBER of wrong cards fails.
	worked := clientControls{Term: "cache", Type: string(models.TypeBug), Priority: "7"}
	wantWorked := []int{}
	for _, task := range f.tasks {
		if strings.Contains(strings.ToLower(task.title), "cache") &&
			task.taskType == models.TypeBug && task.priority >= 7 {
			wantWorked = append(wantWorked, task.id)
		}
	}
	if len(wantWorked) != 2 {
		t.Fatalf("the fixture makes the worked example select %d tasks, want 2", len(wantWorked))
	}
	state, _ := servedBoard(t, mux, f.name, worked)
	assertBoardShows(t, "?q=cache&type=BUG&priority=7", state,
		f.expect(unnarrowed, func(task *filterTask) bool {
			return strings.Contains(strings.ToLower(task.title), "cache") &&
				task.taskType == models.TypeBug && task.priority >= 7
		}))

	// Each of the three criteria must be doing work: dropping any one of them
	// changes the answer. Without this, a board that ignored a criterion entirely
	// could still satisfy the assertion above.
	for _, weaker := range []clientControls{
		{Type: string(models.TypeBug), Priority: "7"},
		{Term: "cache", Priority: "7"},
		{Term: "cache", Type: string(models.TypeBug)},
	} {
		wider, _ := servedBoard(t, mux, f.name, weaker)
		if boardShownCount(wider) <= len(wantWorked) {
			t.Errorf("dropping a criterion from the worked example did not widen the board (%d cards, "+
				"want more than %d): that criterion selects nothing of its own and the assertion above is weak",
				boardShownCount(wider), len(wantWorked))
		}
	}

	// A sweep over combinations of all four controls: the shown set is always the
	// conjunction, computed from the fixture.
	for _, combination := range filterCombinations {
		combo := combination
		served, _ := servedBoard(t, mux, f.name, combo)
		assertBoardShows(t, "?"+combo.query(), served,
			f.expect(unnarrowed, func(task *filterTask) bool { return filterKeeps(combo, task) }))
	}

	// Monotonicity: adding a control never grows the shown set, and no control
	// re-admits a task another excluded.
	base := clientControls{Term: "the"}
	previous := boardShownCount(mustBoard(t, mux, f.name, base))
	for _, added := range []clientControls{
		{Term: "the", Priority: "3"},
		{Term: "the", Priority: "3", Severity: "3"},
		{Term: "the", Priority: "3", Severity: "3", Type: string(models.TypeTask)},
	} {
		now := boardShownCount(mustBoard(t, mux, f.name, added))
		if now > previous {
			t.Errorf("?%s shows %d cards, more than the %d of the weaker controls before it; "+
				"activating a control may only shrink the shown set", added.query(), now, previous)
		}
		// The chain must actually shrink, or the check above would hold for a
		// board that ignored every control added after the first.
		if now == previous {
			t.Errorf("?%s shows the same %d cards as the weaker controls before it; the control "+
				"added here narrows nothing and the monotonicity check is vacuous", added.query(), now)
		}
		previous = now
	}
}

// filterCombinations is the sweep the conjunction and the URL round trip are both
// asserted over: every arity from none to all four, with values that are known to
// admit some tasks and values that are known to admit none.
var filterCombinations = []clientControls{
	{},
	{Term: "cache"},
	{Type: string(models.TypeBug)},
	{Priority: "7"},
	{Severity: "6"},
	{Term: "cache", Type: string(models.TypeBug)},
	{Term: "cache", Priority: "7"},
	{Term: "cache", Severity: "9"},
	{Type: string(models.TypeBug), Priority: "7"},
	{Type: string(models.TypeChore), Severity: "8"},
	{Priority: "5", Severity: "5"},
	{Term: "cache", Type: string(models.TypeBug), Priority: "7"},
	{Term: "cache", Type: string(models.TypeBug), Severity: "9"},
	{Term: "the", Type: string(models.TypeTask), Priority: "6", Severity: "3"},
	{Term: "settlement", Type: string(models.TypeSpike), Priority: "1", Severity: "1"},
	// Combinations that admit nothing, so the no-match message is exercised by a
	// filter alone as well as by a term.
	{Type: string(models.TypeDesignUX), Priority: "9"},
	{Priority: "9", Severity: "9", Type: string(models.TypeBug)},
	{Term: "zzz-nothing-matches"},
	{Term: "zzz-nothing-matches", Type: string(models.TypeBug), Priority: "3", Severity: "3"},
}

// filterKeeps is the conjunction, computed over a seeded task's own fields. Its
// SHAPE is written from the SPECIFICATION — substring over title or "#<id>",
// equality on type, ">=" on the two thresholds — and not from the implementation.
//
// The FOLD inside it is the one thing not re-expressed: it calls the server's own
// foldSearch and foldSearchTerm. A harness that spelled the folding rule out a
// third time would agree with itself for every term this suite happens to use
// while the two real paths disagreed about some other one, which is precisely how
// a divergence between Go's case conversion and the browser's survived here
// unseen (SPEC/WEB.md § Roadmap Tasks Page, One rule, and only one
// implementation of it).
func filterKeeps(c clientControls, task *filterTask) bool {
	term := foldSearchTerm(c.Term)
	if term != "" &&
		!strings.Contains(searchableText(task.title), term) &&
		!strings.Contains("#"+strconv.Itoa(task.id), term) {
		return false
	}
	if c.Type != "" && string(task.taskType) != c.Type {
		return false
	}
	if c.Priority != "" && task.priority < mustAtoi(c.Priority) {
		return false
	}
	if c.Severity != "" && task.severity < mustAtoi(c.Severity) {
		return false
	}
	return true
}

func mustAtoi(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil {
		panic("filter test: " + raw + " is not an integer")
	}
	return n
}

// boardShownCount totals the cards a board is showing.
func boardShownCount(state boardState) int {
	total := 0
	for _, column := range state.columns {
		total += len(shownOf(column))
	}
	return total
}

func mustBoard(t *testing.T, mux *http.ServeMux, roadmap string, c clientControls) boardState {
	t.Helper()
	state, _ := servedBoard(t, mux, roadmap, c)
	return state
}

// ==================== NO FILTER VALUE IS AN ERROR ====================

// TestTaskFilters_NoValueIsAnError is the gate for Acceptance Criterion 115: a
// value a dimension does not accept applies NO filter on that dimension, answers
// HTTP 200, leaves the other dimensions applied, and never produces an error page.
func TestTaskFilters_NoValueIsAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedFilterFixture(t, "delivery-platform")
	mux := buildMux()
	srv := handler()

	unnarrowed, _ := servedBoard(t, mux, f.name, clientControls{})

	// Every way a value can fail to be accepted. Each must leave the board exactly
	// as the parameter's ABSENCE leaves it.
	for _, unusable := range []struct{ name, query string }{
		{"a type that is not one of the ten", "type=NOT_A_TYPE"},
		{"a type differing only in case", "type=bug"},
		{"a type differing only in case, mixed", "type=Bug"},
		{"a comma-packed type", "type=BUG,EPIC"},
		{"a space-packed type", "type=BUG%20EPIC"},
		{"a type with surrounding spaces", "type=%20BUG%20"},
		{"an empty type", "type="},
		{"an undecodable type", "type=%zz"},
		{"a priority of 0, which is no filter", "priority=0"},
		{"a priority above the range", "priority=10"},
		{"a negative priority", "priority=-1"},
		{"a signed priority", "priority=%2B7"},
		{"a zero-padded priority", "priority=07"},
		{"a spaced priority", "priority=%207"},
		{"a non-integer priority", "priority=high"},
		{"a fractional priority", "priority=7.0"},
		{"an empty priority", "priority="},
		{"an undecodable priority", "priority=%zz"},
		{"a severity of 0, which is no filter", "severity=0"},
		{"a severity above the range", "severity=10"},
		{"a non-integer severity", "severity=critical"},
		{"an empty severity", "severity="},
		{"an undecodable severity", "severity=%zz"},
		{"a status parameter, which this page does not offer", "status=DOING"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+f.name+"/tasks?"+unusable.query, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s (?%s): status = %d, want 200", unusable.name, unusable.query, rec.Code)
			continue
		}

		state, body := servedBoardQuery(t, mux, f.name, unusable.query)
		assertBoardShows(t, unusable.name+" (?"+unusable.query+")", state,
			f.expect(unnarrowed, func(*filterTask) bool { return true }))
		if state.messageShown {
			t.Errorf("%s (?%s): the board says nothing matches, but no control is in force",
				unusable.name, unusable.query)
		}
		// And the control shows its no-filter option, not the caller's value.
		for _, control := range filterControlIDs {
			if got := selectedValue(t, readSelect(t, body, control.id), control.id); got != "" {
				t.Errorf("%s (?%s): %s is on %q, want the no-filter option",
					unusable.name, unusable.query, control.id, got)
			}
		}
	}

	// The dimensions are INDEPENDENT: an unusable type leaves the accepted
	// priority and the term applying.
	mixed, _ := servedBoardQuery(t, mux, f.name, "q=cache&type=nope&priority=7")
	assertBoardShows(t, "?q=cache&type=nope&priority=7", mixed,
		f.expect(unnarrowed, func(task *filterTask) bool {
			return strings.Contains(strings.ToLower(task.title), "cache") && task.priority >= 7
		}))
	if boardShownCount(mixed) == 0 {
		t.Fatalf("the independence assertion is vacuous: q=cache&priority=7 admits no task")
	}

	// One value is read per dimension: a repeated parameter is its FIRST
	// occurrence, so ?type=BUG&type=EPIC is the BUG board and not the EPIC one.
	repeated, _ := servedBoardQuery(t, mux, f.name, "type=BUG&type=EPIC")
	assertBoardShows(t, "?type=BUG&type=EPIC", repeated,
		f.expect(unnarrowed, func(task *filterTask) bool { return task.taskType == models.TypeBug }))
	epic, _ := servedBoard(t, mux, f.name, clientControls{Type: string(models.TypeEpic)})
	if equalIDs(shownOf(repeated.columns[0]), shownOf(epic.columns[0])) &&
		boardShownCount(repeated) == boardShownCount(epic) {
		t.Errorf("?type=BUG&type=EPIC produced the EPIC board; the first occurrence is the one read")
	}

	// A comma-packed value is ONE string, matches no TaskType, and is ignored
	// whole — never partly applied.
	packed, _ := servedBoardQuery(t, mux, f.name, "type=BUG,EPIC")
	if boardShownCount(packed) != boardShownCount(unnarrowed) {
		t.Errorf("?type=BUG,EPIC narrowed the board to %d cards; it names no TaskType and must be ignored whole",
			boardShownCount(packed))
	}

	// The parameters are independent of their ORDER in the query string.
	forward, _ := servedBoardQuery(t, mux, f.name, "q=cache&type=BUG&priority=7")
	reverse, _ := servedBoardQuery(t, mux, f.name, "priority=7&type=BUG&q=cache")
	for i := range forward.columns {
		if !equalIDs(shownOf(forward.columns[i]), shownOf(reverse.columns[i])) {
			t.Errorf("column %s differs between the two parameter orders", forward.columns[i].status)
		}
	}
}

// ==================== THE URL, AND THE PROPERTY THAT MATTERS ====================

// TestTaskFilters_URLRoundTripsAndControlsShowTheValue is the gate for Acceptance
// Criterion 116 on the server path: a cold load of a URL carrying any combination
// renders that board with each control already showing the value that produced it,
// and clearing every control restores the full board with its true counts and the
// bare page URL.
func TestTaskFilters_URLRoundTripsAndControlsShowTheValue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedFilterFixture(t, "delivery-platform")
	mux := buildMux()

	unnarrowed, bareBody := servedBoard(t, mux, f.name, clientControls{})

	for _, combination := range filterCombinations {
		combo := combination
		state, body := servedBoard(t, mux, f.name, combo)

		// The board arrived already narrowed by the SERVER, in its final state.
		assertBoardShows(t, "cold load ?"+combo.query(), state,
			f.expect(unnarrowed, func(task *filterTask) bool { return filterKeeps(combo, task) }))

		// And every control already shows the value that produced it.
		if got := searchInputValue(t, body); got != combo.Term {
			t.Errorf("cold load ?%s: the search input shows %q, want %q", combo.query(), got, combo.Term)
		}
		for i, control := range filterControlIDs {
			want := []string{combo.Type, combo.Priority, combo.Severity}[i]
			if got := selectedValue(t, readSelect(t, body, control.id), control.id); got != want {
				t.Errorf("cold load ?%s: %s shows %q, want %q", combo.query(), control.id, got, want)
			}
		}

		// A dimension with no filter leaves no parameter behind, which is what
		// makes the bare page URL the unfiltered board's URL.
		if combo.Type == "" && strings.Contains(combo.query(), "type=") {
			t.Errorf("an inactive type filter put a parameter in the URL: %q", combo.query())
		}
	}

	// Clearing every control restores the full board with its TRUE counts, and the
	// URL that carries no parameter at all is that board.
	if got, total := boardShownCount(unnarrowed), len(f.tasks); got != total {
		t.Errorf("the unnarrowed board shows %d cards, want the roadmap's %d", got, total)
	}
	assertBoardShows(t, "the bare page URL", unnarrowed,
		f.expect(unnarrowed, func(*filterTask) bool { return true }))
	if unnarrowed.messageShown {
		t.Errorf("the unnarrowed board says nothing matches")
	}
	for _, control := range filterControlIDs {
		if got := selectedValue(t, readSelect(t, bareBody, control.id), control.id); got != "" {
			t.Errorf("on the bare page URL, %s is on %q, want the no-filter option", control.id, got)
		}
	}
}

// TestTaskFilters_ServerAndClientProduceTheSameBoard is the gate for the property
// the specification calls out and for Acceptance Criterion 116's equivalence
// clause: for ANY combination of a term and the three filters, the board the
// server renders for the URL and the board the browser produces by narrowing the
// unnarrowed page are the SAME — the same cards, in the same columns, in the same
// order, with the same counts and the same empty states.
//
// One side is the served page for the combination; the other is the served bare
// page with the script's own rule applied to it (boardState.narrow, which
// TestTaskFilters_ScriptAppliesTheSameConjunction pins to the served script).
func TestTaskFilters_ServerAndClientProduceTheSameBoard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedFilterFixture(t, "delivery-platform")
	mux := buildMux()

	unnarrowed, _ := servedBoard(t, mux, f.name, clientControls{})

	// A full sweep: every term against every type against every threshold pairing
	// is more than this needs, so the sweep crosses the named combinations with a
	// second axis of terms, which is enough to make a divergence in any single
	// criterion visible.
	terms := []string{"", "cache", "CACHE", "  cache  ", "the", "settlement latency",
		"#" + strconv.Itoa(f.tasks[0].id), "zzz"}
	types := []string{"", string(models.TypeBug), string(models.TypeTask), string(models.TypeChore)}
	thresholds := []string{"", "1", "5", "7", "9"}

	compared := 0
	for _, term := range terms {
		for _, taskType := range types {
			for _, priority := range thresholds {
				for _, severity := range thresholds {
					controls := clientControls{Term: term, Type: taskType, Priority: priority, Severity: severity}
					server, _ := servedBoard(t, mux, f.name, controls)
					client := unnarrowed.narrow(controls)
					assertSameBoard(t, "?"+controls.query(), server, client)
					compared++
				}
			}
		}
	}
	if compared != len(terms)*len(types)*len(thresholds)*len(thresholds) {
		t.Fatalf("the sweep compared %d combinations; the loop is not covering what it claims", compared)
	}
}

// assertSameBoard compares two boards on everything a board states about itself.
func assertSameBoard(t *testing.T, what string, server, client boardState) {
	t.Helper()

	if len(server.columns) != len(client.columns) {
		t.Fatalf("%s: %d columns on the server and %d in the browser",
			what, len(server.columns), len(client.columns))
	}
	for i := range server.columns {
		sc, cc := server.columns[i], client.columns[i]
		if sc.status != cc.status {
			t.Errorf("%s: column %d is %s on the server and %s in the browser", what, i, sc.status, cc.status)
		}
		if got, want := shownOf(sc), shownOf(cc); !equalIDs(got, want) {
			t.Errorf("%s: column %s shows %v on the server and %v in the browser", what, sc.status, got, want)
		}
		if sc.count != cc.count {
			t.Errorf("%s: column %s counts %d on the server and %d in the browser",
				what, sc.status, sc.count, cc.count)
		}
		if sc.emptyShown != cc.emptyShown {
			t.Errorf("%s: column %s shows its empty state on the server=%v, browser=%v",
				what, sc.status, sc.emptyShown, cc.emptyShown)
		}
	}
	if server.messageShown != client.messageShown {
		t.Errorf("%s: the no-match message shows on the server=%v, browser=%v",
			what, server.messageShown, client.messageShown)
	}
	if server.messageTermShown != client.messageTermShown {
		t.Errorf("%s: the message names the term on the server=%v, browser=%v",
			what, server.messageTermShown, client.messageTermShown)
	}
	if server.messageTerm != client.messageTerm {
		t.Errorf("%s: the message names %q on the server and %q in the browser",
			what, server.messageTerm, client.messageTerm)
	}
}

// ==================== THE SCRIPT ====================

// TestTaskFilters_ScriptAppliesTheSameConjunction pins that the served script is
// the rule TestTaskFilters_ServerAndClientProduceTheSameBoard re-expresses: ONE
// conjunction over the term and the three filters, each filter reading the card
// attribute the server wrote, the type compared by equality and the two
// thresholds by ">=", with the URL kept in step in place.
func TestTaskFilters_ScriptAppliesTheSameConjunction(t *testing.T) {
	script := stripJSComments(readEmbeddedAsset(t, "static/task-search.js"))

	// The three controls, the three card attributes, and the three URL parameters
	// the script must know about — and no fourth.
	for _, fragment := range []string{
		`document.querySelector('[data-role="task-filter-type"]')`,
		`document.querySelector('[data-role="task-filter-priority"]')`,
		`document.querySelector('[data-role="task-filter-severity"]')`,
		`attribute: "data-type"`,
		`attribute: "data-priority"`,
		`attribute: "data-severity"`,
		`param: "type"`,
		`param: "priority"`,
		`param: "severity"`,
	} {
		if !strings.Contains(script, fragment) {
			t.Errorf("the script does not carry %q", fragment)
		}
	}

	// The two comparisons, and each bound to the right dimensions.
	if !strings.Contains(script, "return cardValue === filterValue") {
		t.Errorf("the script's type comparison is not an equality")
	}
	if !strings.Contains(script, "return Number(cardValue) >= Number(filterValue)") {
		t.Errorf("the script's threshold comparison is not >=")
	}
	if strings.Contains(script, "Number(cardValue) > Number(filterValue)") ||
		strings.Contains(script, "Number(cardValue) === Number(filterValue)") {
		t.Errorf("the script compares a threshold with > or ===; the filter is 'at least'")
	}

	// ONE conjunction: the term criterion and the filter criteria are decided in
	// the same function, each failing criterion rejects the card outright, and an
	// inactive filter is skipped rather than compared. The fragments pin the
	// CONJUNCTION and not merely the presence of the operands: a rule that
	// admitted a card on any one criterion would not carry an early `return
	// false` per criterion.
	conjunction := functionBody(t, script, "function matches(card, state)")
	for _, fragment := range []string{
		"if (!matchesTerm(card, state.term)) {\n      return false;",
		"var value = state.filters[i];",
		"if (value === \"\") {\n        continue;",
		"if (!filters[i].compare(card.getAttribute(filters[i].attribute) || \"\", value)) {\n        return false;",
		"return true;",
	} {
		if !strings.Contains(conjunction, fragment) {
			t.Errorf("the board's verdict function does not carry %q; the term and the three filters "+
				"must be ONE conjunction, each criterion able to reject a card on its own", fragment)
		}
	}
	// And there is exactly one such function: a second verdict would be a second
	// filtering model.
	if got := strings.Count(script, "function matches("); got != 1 {
		t.Errorf("the script declares %d verdict functions, want exactly 1", got)
	}

	// The URL: each parameter set when its control holds a value and REMOVED when
	// it does not, in place, never pushed.
	for _, fragment := range []string{
		`url.searchParams.delete(filters[i].param)`,
		`url.searchParams.set(filters[i].param, state.filters[i])`,
		"window.history.replaceState",
	} {
		if !strings.Contains(script, fragment) {
			t.Errorf("the script does not keep the filter parameters in step with the controls: missing %q", fragment)
		}
	}
	if strings.Contains(script, "pushState") {
		t.Errorf("the script pushes a history entry; changing a dropdown would turn Back into an undo key")
	}

	// The dropdowns drive the same entry point the search input drives, so the
	// four controls cannot drift into four behaviours.
	if !strings.Contains(script, `addEventListener("change", narrow)`) {
		t.Errorf("the dropdowns are not wired to the board's single narrowing entry point")
	}
	if !strings.Contains(script, `input.addEventListener("input", narrow)`) {
		t.Errorf("the search input is not wired to the board's single narrowing entry point")
	}

	// Narrowing still reaches neither the network nor a navigation.
	for _, forbidden := range []string{"fetch(", "XMLHttpRequest", "location.assign", "location.replace", "submit("} {
		if strings.Contains(script, forbidden) {
			t.Errorf("the narrowing script reaches the network or navigates: found %q", forbidden)
		}
	}
}

// functionBody returns the source of the function whose signature is given, up to
// its closing brace at the same indentation, so an assertion can be made about
// ONE function rather than about the file.
func functionBody(t *testing.T, script, signature string) string {
	t.Helper()

	at := strings.Index(script, signature)
	if at < 0 {
		t.Fatalf("the script carries no %q", signature)
	}
	rest := script[at:]
	end := strings.Index(rest, "\n  }")
	if end < 0 {
		t.Fatalf("the body of %q is not delimited as expected", signature)
	}
	return rest[:end]
}

// ==================== READ COST, AND WHAT REACHES THE PAGE ====================

// TestTaskFilters_AddNoDatabaseQueryAndEchoNoValue is the gate for Acceptance
// Criterion 117: a filter contributes no clause to the page's read, no read of its
// own, and no per-dimension query; and no filter value is echoed into the page.
func TestTaskFilters_AddNoDatabaseQueryAndEchoNoValue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedFilterFixture(t, "delivery-platform")
	mux := buildMux()

	// The page's read is the same three reads, narrowed or not.
	src := openCounting(t, f.name)
	controls := newBoardControls("cache", models.TypeBug, 7, 3)
	if _, err := readTasks(context.Background(), src, f.name, controls); err != nil {
		t.Fatalf("readTasks with every control in force: %v", err)
	}
	if src.taskList != 1 || src.groupedCommentCounts != 1 || src.groupedTaskSprints != 1 {
		t.Errorf("a board narrowed by all four controls issued %d task-list, %d comment-count and %d sprint "+
			"queries; want 1, 1 and 1", src.taskList, src.groupedCommentCounts, src.groupedTaskSprints)
	}
	if src.perTaskComments != 0 || src.boundedTaskList != 0 {
		t.Errorf("a narrowed render took a read the board must not take")
	}

	// The read stays UNBOUNDED: a filter narrows what the board shows, never what
	// the page reads, which is what lets the browser widen it again with no round
	// trip. Every card is still in the document.
	state, _ := servedBoard(t, mux, f.name, clientControls{Type: string(models.TypeBug)})
	present := 0
	for _, column := range state.columns {
		present += len(column.cards)
	}
	if present != len(f.tasks) {
		t.Errorf("a filtered board carries %d cards in the document, want all %d: a filter must narrow "+
			"what the board SHOWS, not what it reads", present, len(f.tasks))
	}

	// No filter value is echoed into the page. The options are the server's own
	// enumeration, so a hostile value selects the no-filter option and reaches the
	// document nowhere at all.
	hostile := `"><script>alert(1)</script>`
	srv := handler()
	req := httptest.NewRequest(http.MethodGet,
		"/roadmaps/"+f.name+"/tasks?type=%22%3E%3Cscript%3Ealert(1)%3C%2Fscript%3E"+
			"&priority=%22%3E%3Cscript%3E&severity=%22%3E%3Cscript%3E", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, hostile) || strings.Contains(body, "alert(1)") {
		t.Errorf("a filter value reached the page")
	}
	if got := strings.Count(body, "<script"); got != 3 {
		t.Errorf("the page carries %d script elements, want the 3 it loads", got)
	}
	// The policy is untouched by the filters.
	if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Errorf("Content-Security-Policy = %q, want the unchanged policy", got)
	}
	// And no inline style or inline script came in with the dropdowns.
	if strings.Contains(headerRegion(t, "/roadmaps/"+f.name+"/tasks", body), "style=") {
		t.Errorf("the header carries an inline style attribute")
	}
}
