package web

import (
	"context"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The guards in this file cover the reference line that opens the card of BOTH
// Kanban boards: the id badge reading #<id> in the neutral bg-secondary-lt
// variant, then the type badge reading the TaskType value in the variant the
// task type table assigns to it, then the title on the next line (SPEC/WEB.md
// § Roadmap Tasks Page, Card content, item 1; § Sprint Detail Sub-Template, The
// card; § Status, Priority, and Severity Badge Colours, task type table;
// Acceptance Criteria 177 to 179).

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

// typedTask is one task of the reference-line fixture: one per TaskType.
type typedTask struct {
	taskType models.TaskType
	title    string
	id       int
}

// referenceLineFixture is a roadmap holding one OPEN sprint whose ten member
// tasks carry the ten task types, one each, so every type has a card on BOTH
// boards.
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
		id, cerr := seedTask(database, &models.Task{
			Title:                  f.tasks[i].title,
			Type:                   f.tasks[i].taskType,
			Status:                 models.StatusBacklog,
			Priority:               6,
			Severity:               4,
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

// referenceLineBoards serves both boards and returns each board's region, keyed
// by a name for messages.
func referenceLineBoards(t *testing.T, f *referenceLineFixture) map[string]string {
	t.Helper()

	mux := buildMux()
	return map[string]string{
		"the tasks board":  boardRegion(t, servePage(t, mux, f.tasksPath())),
		"the sprint board": memberBoardRegion(t, servePage(t, mux, f.sprintPath())),
	}
}

// TestBoardCards_EveryTaskTypeRendersItsVariant is the gate for Acceptance
// Criterion 177: a task of each of the ten types, on each of the two boards, and
// all twenty type badges asserted — text exactly as the enum spells it, with no
// prefix, and the variant the SPEC's table assigns — because a mapping that is
// wrong for one type passes on every other.
//
// The badge is read from the card's reference line, and as the SECOND of its
// badges, so a type badge rendered anywhere else on the card does not satisfy it.
func TestBoardCards_EveryTaskTypeRendersItsVariant(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedReferenceLineFixture(t, "merchant-settlement")

	asserted := 0
	for where, region := range referenceLineBoards(t, &f) {
		for _, task := range f.tasks {
			card := cardMarkupOf(t, region, task.id, where)
			badges := pageBadges(spanWithRole(t, card, "task-card-ref"))
			if len(badges) != 2 {
				t.Errorf("%s: task #%d's reference line carries %d badges, want 2\ncard: %s",
					where, task.id, len(badges), card)
				continue
			}
			typeBadge := badges[1]
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
		t.Errorf("asserted %d type badges, want %d — every type on both boards", asserted, want)
	}
}

// TestBoardCards_TypeMappingReachesNoOtherSurface is the exclusion clause of
// Acceptance Criterion 177: outside the board cards, no badge on either page reads
// a task type, so the type filter's options and the modal shell carry no type
// badge, and the type filter still offers the ten values as plain options.
func TestBoardCards_TypeMappingReachesNoOtherSurface(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedReferenceLineFixture(t, "merchant-settlement")
	mux := buildMux()

	for _, path := range []string{f.tasksPath(), f.sprintPath()} {
		cards, outside := splitBoardCards(t, servePage(t, mux, path))
		if len(cards) == 0 {
			t.Fatalf("%s renders no board card", path)
		}
		for _, b := range pageBadges(outside) {
			if models.IsValidTaskType(b.text) {
				t.Errorf("%s: a badge outside a board card reads the task type %q; the type is "+
					"coloured on the two board cards and nowhere else", path, b.text)
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

// TestBoardCards_IDBadgeIsNeutral is the gate for Acceptance Criterion 178: on
// both boards, the id badge reads #<id> and carries bg-secondary-lt whatever the
// task's type. It is asserted on every NON-CHORE task — CHORE maps to the same
// variant, so a card whose id badge took the type's colour would pass on it — and
// the test fails if the fixture offers none.
func TestBoardCards_IDBadgeIsNeutral(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedReferenceLineFixture(t, "merchant-settlement")

	discriminating := 0
	for where, region := range referenceLineBoards(t, &f) {
		for _, task := range f.tasks {
			if task.taskType == models.TypeChore {
				continue
			}
			discriminating++
			card := cardMarkupOf(t, region, task.id, where)
			badges := pageBadges(spanWithRole(t, card, "task-card-ref"))
			if len(badges) == 0 {
				t.Errorf("%s: task #%d's card has no reference-line badge\ncard: %s",
					where, task.id, card)
				continue
			}
			idBadge := badges[0]
			if want := "#" + itoa(task.id); idBadge.text != want {
				t.Errorf("%s: task #%d's id badge reads %q, want %q", where, task.id, idBadge.text, want)
			}
			if want := "badge bg-secondary-lt"; idBadge.classes != want {
				t.Errorf("%s: task #%d (%s) id badge carries the classes %q, want %q — the id "+
					"badge takes no colour from any table", where, task.id, task.taskType,
					idBadge.classes, want)
			}
		}
	}
	if discriminating == 0 {
		t.Fatalf("the fixture has no non-CHORE task, so the id badge assertion proves nothing")
	}
}

// TestBoardCards_OpenWithIDThenTypeThenTitle is the gate for Acceptance Criterion
// 179: on both boards, the reference line is the FIRST line of the card body and
// carries exactly two badges, id then type; the title follows it directly on the
// next line; and the card's accessible name is unchanged.
//
// "First" and "directly" are asserted on the markup between the elements being
// nothing but whitespace, so a card that put the badges after the title, or
// anything between the reference line and the title, fails here even though it
// would satisfy Acceptance Criteria 177 and 178.
func TestBoardCards_OpenWithIDThenTypeThenTitle(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedReferenceLineFixture(t, "merchant-settlement")

	const bodyOpen = `<span class="card-body d-block">`

	for where, region := range referenceLineBoards(t, &f) {
		for _, task := range f.tasks {
			card := cardMarkupOf(t, region, task.id, where)

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
			ref := spanWithRole(t, card, "task-card-ref")
			if ref == "" || !strings.HasPrefix(afterBody, ref) {
				t.Errorf("%s: task #%d's card does not open with its reference line\ncard: %s",
					where, task.id, card)
				continue
			}

			wantRef := `<span class="d-flex flex-wrap gap-1 mb-1" data-role="task-card-ref">` +
				`<span class="badge bg-secondary-lt">#` + itoa(task.id) + `</span>` +
				`<span class="badge ` + wantTaskTypeVariant[task.taskType] + `">` +
				string(task.taskType) + `</span></span>`
			if ref != wantRef {
				t.Errorf("%s: task #%d's reference line is\n  %s\nwant exactly the id badge then "+
					"the type badge:\n  %s", where, task.id, ref, wantRef)
			}

			afterRef := strings.TrimLeft(afterBody[len(ref):], " \t\r\n")
			title := `<span class="d-block fw-bold text-break mb-1" data-role="task-card-title">` +
				task.title + `</span>`
			if !strings.HasPrefix(afterRef, title) {
				t.Errorf("%s: task #%d's title does not follow its reference line directly; "+
					"want %s next\ncard: %s", where, task.id, title, card)
			}
		}
	}
}
