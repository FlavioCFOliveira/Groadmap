package web

import (
	"context"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The guards in this file cover the badges a task carries on the card of the
// sprint page's member-tasks board and in the row of the tasks page's list: the id
// badge reading #<id> with the fixed classes bg-black and text-white, the
// severity badge, the priority badge, and the type badge reading the TaskType
// value in the variant the task type table assigns to it, and on the card the
// title leading and the badge line following it (SPEC/WEB.md § Sprint Detail
// Sub-Template, The card; § Roadmap Tasks Page, Row content; § Status, Priority,
// and Severity Badge Colours, task type table; Acceptance Criteria 177 to 179).

// wantTaskTypeVariant is the task type table of SPEC/WEB.md § Status, Priority,
// and Severity Badge Colours, written out here rather than read from
// taskTypeBadge: the board checks below compare the served bytes against the
// SPEC's table, so a helper that is wrong for one type fails them instead of
// agreeing with itself.
var wantTaskTypeVariant = map[models.TaskType]string{
	models.TypeBug:         "bg-red-lt",
	models.TypeUserStory:   "bg-green-lt",
	models.TypeTask:        "bg-blue-lt",
	models.TypeSubTask:     "bg-azure-lt",
	models.TypeEpic:        "bg-purple-lt",
	models.TypeRefactor:    "bg-indigo-lt",
	models.TypeImprovement: "bg-teal-lt",
	models.TypeSpike:       "bg-yellow-lt",
	models.TypeDesignUX:    "bg-pink-lt",
	models.TypeChore:       "bg-secondary-lt",
}

// TestTaskTypeBadge asserts the FULL task-type -> Tabler colour-variant mapping,
// plus the neutral fallback for an out-of-enum value, proving the helper is total
// (SPEC rule 1).
func TestTaskTypeBadge(t *testing.T) {
	for taskType, want := range wantTaskTypeVariant {
		if got := taskTypeBadge(taskType); got != want {
			t.Errorf("taskTypeBadge(%q) = %q, want %q", taskType, got, want)
		}
	}
	if got := taskTypeBadge(models.TaskType("GARBAGE")); got != badgeSecondary {
		t.Errorf("taskTypeBadge(out-of-enum) = %q, want the neutral fallback %q", got, badgeSecondary)
	}
}

// TestTaskTypeBadge_CoversEveryEnumValue guards against a TaskType being added
// in MODELS.md without a row in the SPEC's table: it iterates the canonical
// ValidTaskTypes list, requires the table to cover exactly that set, and fails if
// any value other than CHORE falls through to the neutral fallback.
func TestTaskTypeBadge_CoversEveryEnumValue(t *testing.T) {
	if len(models.ValidTaskTypes) != len(wantTaskTypeVariant) {
		t.Errorf("the enum has %d task types and the SPEC's table %d; the mapping must be total",
			len(models.ValidTaskTypes), len(wantTaskTypeVariant))
	}
	for _, taskType := range models.ValidTaskTypes {
		if _, ok := wantTaskTypeVariant[taskType]; !ok {
			t.Errorf("the task type %q has no row in the SPEC's task type table", taskType)
		}
		got := taskTypeBadge(taskType)
		if got == "" {
			t.Errorf("taskTypeBadge(%q) returned an empty class; the mapping must be total", taskType)
		}
		if got == badgeSecondary && taskType != models.TypeChore {
			t.Errorf("taskTypeBadge(%q) = neutral fallback %q; add an explicit mapping per SPEC",
				taskType, got)
		}
	}
}

// typedTask is one task of the card-layout fixture: one per TaskType, each with
// its own priority and severity.
type typedTask struct {
	taskType models.TaskType
	title    string
	id       int
	priority int
	severity int
}

// referenceLineFixture is a roadmap holding one OPEN sprint whose ten member
// tasks carry the ten task types, one each, so every type has a card on the
// sprint board and a row in the tasks page's list. The ten tasks also carry ten different priorities and ten different
// severities, running in opposite directions, so the badges whose colour must
// NOT follow a value (the id badge) are asserted across every band of both
// scales, and the severity and priority badges of one card never read the same
// integer.
type referenceLineFixture struct {
	name     string
	tasks    []typedTask
	sprintID int
}

func (f *referenceLineFixture) sprintPath() string {
	return "/roadmaps/" + f.name + "/sprints/" + itoa(f.sprintID)
}

func (f *referenceLineFixture) tasksPath() string {
	return "/roadmaps/" + f.name + "/tasks"
}

// seedReferenceLineFixture builds the fixture. The titles are distinct from every
// type name, so a title can never be mistaken for a type badge's text.
func seedReferenceLineFixture(t *testing.T, name string) referenceLineFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	f := referenceLineFixture{name: name, tasks: []typedTask{
		{taskType: models.TypeBug, title: "Refund totals drop the cents on multi-currency orders"},
		{taskType: models.TypeUserStory, title: "Let a merchant download its monthly settlement statement"},
		{taskType: models.TypeTask, title: "Provision the reconciliation worker queue in staging"},
		{taskType: models.TypeSubTask, title: "Parse the acquirer file trailer record"},
		{taskType: models.TypeEpic, title: "Move card settlement onto the new ledger"},
		{taskType: models.TypeRefactor, title: "Extract the fee calculator from the checkout handler"},
		{taskType: models.TypeImprovement, title: "Show the payout date on the merchant dashboard"},
		{taskType: models.TypeSpike, title: "Evaluate idempotency keys for acquirer webhooks"},
		{taskType: models.TypeDesignUX, title: "Wireframe the dispute evidence upload flow"},
		{taskType: models.TypeChore, title: "Rotate the sandbox acquirer credentials"},
	}}
	if len(f.tasks) != len(models.ValidTaskTypes) {
		t.Fatalf("the fixture seeds %d types, want one of each of the %d", len(f.tasks),
			len(models.ValidTaskTypes))
	}

	ids := make([]int, 0, len(f.tasks))
	for i := range f.tasks {
		f.tasks[i].priority = i
		f.tasks[i].severity = len(f.tasks) - 1 - i
		id, cerr := seedTask(database, &models.Task{
			Title:                  f.tasks[i].title,
			Type:                   f.tasks[i].taskType,
			Status:                 models.StatusBacklog,
			Priority:               f.tasks[i].priority,
			Severity:               f.tasks[i].severity,
			FunctionalRequirements: "The settlement team must be able to track this work from the board.",
			TechnicalRequirements:  "Implemented against the payments ledger service.",
			AcceptanceCriteria:     "The change is live in staging and verified by the settlement team.",
		})
		if cerr != nil {
			t.Fatalf("creating the %s task: %v", f.tasks[i].taskType, cerr)
		}
		f.tasks[i].id = id
		ids = append(ids, id)
	}

	f.sprintID = newSprint(t, database, "Settlement statement release",
		"Ship the merchant settlement statement with the ledger migration groundwork.")
	if aerr := database.AddTasksToSprint(context.Background(), f.sprintID, ids); aerr != nil {
		t.Fatalf("adding the tasks to the sprint: %v", aerr)
	}
	forceSprintOpen(t, database, f.sprintID)
	return f
}

// referenceLineSurfaces serves the sprint board and the tasks page's list and
// returns, for each, the badges of every fixture task in the order the surface
// renders them: the card's badge line (id, severity, priority, type) or the row's
// badges (id, type, status, severity, priority).
func referenceLineSurfaces(t *testing.T, f *referenceLineFixture) map[string]map[int][]badge {
	t.Helper()

	mux := buildMux()
	board := memberBoardRegion(t, servePage(t, mux, f.sprintPath()))
	list := boardRegion(t, servePage(t, mux, f.tasksPath()))

	surfaces := map[string]map[int][]badge{"the sprint board": {}, "the tasks list": {}}
	for _, task := range f.tasks {
		card := cardMarkupOf(t, board, task.id, "the sprint board")
		surfaces["the sprint board"][task.id] = pageBadges(spanWithRole(t, card, "task-card-badges"))
		surfaces["the tasks list"][task.id] = pageBadges(rowMarkupOf(t, list, task.id, "the tasks list"))
	}
	return surfaces
}

// typeBadgeOf and idBadgeOf pick a badge by its place on each surface.
func typeBadgeOf(where string, badges []badge) (badge, bool) {
	switch {
	case where == "the sprint board" && len(badges) == 4:
		return badges[3], true
	case where == "the tasks list" && len(badges) == 5:
		return badges[1], true
	}
	return badge{}, false
}

// TestBoardCards_EveryTaskTypeRendersItsVariant is the gate for Acceptance
// Criterion 177: a task of each of the ten types, on each of the two pages, and
// all twenty type badges asserted — text exactly as the enum spells it, with no
// badge label, and the variant the SPEC's table assigns — because a mapping that
// is wrong for one type passes on every other.
//
// The badge is read by its place: the fourth and last badge of the card's badge
// line, and the second badge of the list row, in its Type cell.
func TestBoardCards_EveryTaskTypeRendersItsVariant(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedReferenceLineFixture(t, "merchant-settlement")

	asserted := 0
	for where, byTask := range referenceLineSurfaces(t, &f) {
		for _, task := range f.tasks {
			typeBadge, ok := typeBadgeOf(where, byTask[task.id])
			if !ok {
				t.Errorf("%s: task #%d carries %d badges, not the surface's full set",
					where, task.id, len(byTask[task.id]))
				continue
			}
			if typeBadge.text != string(task.taskType) {
				t.Errorf("%s: task #%d's type badge reads %q, want %q exactly as the enum spells it",
					where, task.id, typeBadge.text, task.taskType)
			}
			if want := "badge " + wantTaskTypeVariant[task.taskType]; typeBadge.classes != want {
				t.Errorf("%s: task #%d's %s type badge carries the classes %q, want %q",
					where, task.id, task.taskType, typeBadge.classes, want)
			}
			asserted++
		}
	}
	if want := 2 * len(models.ValidTaskTypes); asserted != want {
		t.Errorf("asserted %d type badges, want %d — every type on both pages", asserted, want)
	}
}

// TestBoardCards_TypeMappingReachesNoOtherSurface is the exclusion clause of
// Acceptance Criterion 177: outside the board cards and the list rows, no badge on
// either page reads a task type, and the type filter offers the ten values as plain
// options with no colour.
func TestBoardCards_TypeMappingReachesNoOtherSurface(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedReferenceLineFixture(t, "merchant-settlement")
	mux := buildMux()

	for _, path := range []string{f.tasksPath(), f.sprintPath()} {
		cards, outsideCards := splitBoardCards(t, servePage(t, mux, path))
		rows, outside := splitListRows(t, outsideCards)
		if len(cards)+len(rows) == 0 {
			t.Fatalf("%s renders no board card and no list row", path)
		}
		for _, b := range pageBadges(outside) {
			if models.IsValidTaskType(b.text) {
				t.Errorf("%s: a badge outside a board card and a list row reads the task type %q; "+
					"the type is coloured on the sprint board's card and in the list's row and "+
					"nowhere else", path, b.text)
			}
		}
	}

	page := servePage(t, mux, f.tasksPath())
	for _, taskType := range models.ValidTaskTypes {
		option := `<option value="` + string(taskType) + `">` + string(taskType) + `</option>`
		if !strings.Contains(page, option) {
			t.Errorf("the type filter no longer offers %q as the plain option %s", taskType, option)
		}
	}
}

// TestBoardCards_IDBadgeIsBlackWithWhiteText is the gate for Acceptance
// Criterion 178: on the sprint board and in the list, the id badge reads #<id> and
// carries exactly the
// classes bg-black and text-white, whatever the task's type, severity, or
// priority. The fixture spans all ten types, all ten severities, and all ten
// priorities, and the test fails if it stops doing so, because an id badge whose
// colour followed any one of those values would pass on a fixture that held it
// constant.
//
// The fixed variant is also checked against every table of the badge colour
// mapping: no status, type, priority, severity, or comment-type value may be
// assigned bg-black, because the SPEC names bg-black as the mark of the id badge
// and a value badge carrying it would be indistinguishable from it.
func TestBoardCards_IDBadgeIsBlackWithWhiteText(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedReferenceLineFixture(t, "merchant-settlement")

	const wantClasses = "badge bg-black text-white"

	types, severities, priorities := map[models.TaskType]bool{}, map[int]bool{}, map[int]bool{}
	for where, byTask := range referenceLineSurfaces(t, &f) {
		for _, task := range f.tasks {
			types[task.taskType] = true
			severities[task.severity] = true
			priorities[task.priority] = true

			badges := byTask[task.id]
			if len(badges) == 0 {
				t.Errorf("%s: task #%d carries no badge", where, task.id)
				continue
			}
			idBadge := badges[0]
			if want := "#" + itoa(task.id); idBadge.text != want {
				t.Errorf("%s: task #%d's id badge reads %q, want %q", where, task.id, idBadge.text, want)
			}
			if idBadge.classes != wantClasses {
				t.Errorf("%s: task #%d (%s, severity %d, priority %d) id badge carries the classes "+
					"%q, want %q — the id badge takes no colour from any table",
					where, task.id, task.taskType, task.severity, task.priority,
					idBadge.classes, wantClasses)
			}
		}
	}
	if len(types) != len(models.ValidTaskTypes) || len(severities) != 10 || len(priorities) != 10 {
		t.Fatalf("the fixture spans %d types, %d severities, and %d priorities, want %d, 10, "+
			"and 10: an id badge whose colour followed a value held constant here would pass",
			len(types), len(severities), len(priorities), len(models.ValidTaskTypes))
	}

	// No table of the mapping hands out the id badge's variant.
	mapped := make([]string, 0, len(models.ValidTaskStatuses)+len(models.ValidSprintStatuses)+
		len(models.ValidTaskTypes)+2*10+1)
	for _, s := range models.ValidTaskStatuses {
		mapped = append(mapped, taskStatusBadge(s))
	}
	for _, s := range models.ValidSprintStatuses {
		mapped = append(mapped, sprintStatusBadge(s))
	}
	for _, tt := range models.ValidTaskTypes {
		mapped = append(mapped, taskTypeBadge(tt))
	}
	for v := 0; v <= 9; v++ {
		mapped = append(mapped, priorityBadge(v), severityBadge(v))
	}
	mapped = append(mapped, commentTypeBadge(""))
	for _, variant := range mapped {
		for _, token := range strings.Fields(variant) {
			if token == "bg-black" || token == "text-white" {
				t.Errorf("a table of the badge colour mapping assigns %q (from %q); bg-black and "+
					"text-white are the id badge's alone", token, variant)
			}
		}
	}
}

// TestBoardCards_LeadWithTitleThenBadgeLine is the gate for Acceptance Criterion
// 179: on the sprint board, the title is the FIRST line of the card body, and the next
// line opens with exactly four badges, in this order: the id badge, the severity
// badge, the priority badge, and the type badge; and the card's accessible name is
// unchanged.
//
// "First" and "next" are asserted on the markup between the elements being
// nothing but whitespace — plus the opening tag of the line that also carries the
// counters — so a card that put the badges before the
// title, the priority before the severity, or the type before the id fails here
// even though it would satisfy Acceptance Criteria 177 and 178.
func TestBoardCards_LeadWithTitleThenBadgeLine(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedReferenceLineFixture(t, "merchant-settlement")

	const (
		bodyOpen = `<span class="card-body d-block">`
		// The sprint board's second line holds the badge line at its leading edge
		// and the counters at its trailing edge (Acceptance Criterion 133).
		sprintLineOpen = `<span class="d-flex flex-wrap align-items-center ` +
			`justify-content-between gap-1" data-role="task-card-summary">`
	)

	board := memberBoardRegion(t, servePage(t, buildMux(), f.sprintPath()))
	for _, where := range []string{"the sprint board"} {
		for _, task := range f.tasks {
			card := cardMarkupOf(t, board, task.id, where)

			// The accessible name is unchanged (Acceptance Criteria 86 and 135).
			label := `aria-label="Open details for task #` + itoa(task.id) + `: ` + task.title + `"`
			if !strings.Contains(card, label) {
				t.Errorf("%s: task #%d's card does not carry the accessible name %s\ncard: %s",
					where, task.id, label, card)
			}

			bodyAt := strings.Index(card, bodyOpen)
			if bodyAt < 0 {
				t.Fatalf("%s: task #%d's card has no card body\ncard: %s", where, task.id, card)
			}
			afterBody := strings.TrimLeft(card[bodyAt+len(bodyOpen):], " \t\r\n")
			title := `<span class="d-block fw-bold text-break mb-1" data-role="task-card-title">` +
				task.title + `</span>`
			if !strings.HasPrefix(afterBody, title) {
				t.Errorf("%s: task #%d's card does not open with its title; want %s first\ncard: %s",
					where, task.id, title, card)
				continue
			}

			next := strings.TrimLeft(afterBody[len(title):], " \t\r\n")
			if !strings.HasPrefix(next, sprintLineOpen) {
				t.Errorf("%s: task #%d's title is not followed by the line carrying the badges "+
					"and the counters\ncard: %s", where, task.id, card)
				continue
			}
			next = strings.TrimLeft(next[len(sprintLineOpen):], " \t\r\n")

			wantLine := `<span class="d-flex flex-wrap gap-1" data-role="task-card-badges">` +
				`<span class="badge bg-black text-white">#` + itoa(task.id) + `</span>` +
				`<span class="badge ` + severityBadge(task.severity) + `">S` +
				itoa(task.severity) + `</span>` +
				`<span class="badge ` + priorityBadge(task.priority) + `">P` +
				itoa(task.priority) + `</span>` +
				`<span class="badge ` + wantTaskTypeVariant[task.taskType] + `">` +
				string(task.taskType) + `</span></span>`
			if !strings.HasPrefix(next, wantLine) {
				t.Errorf("%s: task #%d's title is not followed directly by the badge line\n  %s\n"+
					"want exactly the id, severity, priority, and type badges:\n  %s",
					where, task.id, spanWithRole(t, card, "task-card-badges"), wantLine)
			}
		}
	}
}

// TestListRows_CarryTheCardsBadgesInTheirCells is the list half of Acceptance
// Criterion 179: the same four badges the sprint board's card carries — with the
// same texts, labels, and colours — fill the ID, Type, Severity, and Priority cells
// of the tasks page's row, and the row's columns place severity before priority as
// the card does (Acceptance Criterion 85).
func TestListRows_CarryTheCardsBadgesInTheirCells(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedReferenceLineFixture(t, "merchant-settlement")
	surfaces := referenceLineSurfaces(t, &f)

	for _, task := range f.tasks {
		card := surfaces["the sprint board"][task.id]
		row := surfaces["the tasks list"][task.id]
		if len(card) != 4 || len(row) != 5 {
			t.Fatalf("task #%d: the card carries %d badges and the row %d, want 4 and 5",
				task.id, len(card), len(row))
		}
		// Card order: id, severity, priority, type. Row order: id, type, status,
		// severity, priority.
		for _, pair := range []struct {
			what      string
			card, row badge
		}{
			{"id", card[0], row[0]},
			{"type", card[3], row[1]},
			{"severity", card[1], row[3]},
			{"priority", card[2], row[4]},
		} {
			if pair.card.markup != pair.row.markup {
				t.Errorf("task #%d: the %s badge reads %s on the card and %s in the row; the list "+
					"repeats the card's badge", task.id, pair.what, pair.card.markup, pair.row.markup)
			}
		}
		if want := `<span class="badge ` + taskStatusBadge(models.StatusSprint) + `">SPRINT</span>`; row[2].markup != want {
			t.Errorf("task #%d: the row's status badge is %s, want %s", task.id, row[2].markup, want)
		}
	}
}
