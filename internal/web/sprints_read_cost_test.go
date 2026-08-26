package web

import (
	"context"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The Roadmap Sprints Page reads the roadmap's sprints and each sprint's total
// task count for its card footer, but NO member tasks, because the page renders
// every sprint as a card with no member tasks on it (SPEC/WEB.md § Tasks and
// Sprints from SQLite; § Roadmap Sprints Page; § Task Detail Modal).
//
// This file is the gate for that rule. It measures what a render of the page
// costs, on a real roadmap, and pins the two properties that make the cost
// bounded rather than merely small:
//
//  1. the page issues ONE read whatever the number of sprints, and never the
//     per-sprint member-task read; and
//  2. the card footer's number is the sprint's own task_count, which the listing
//     already resolved, so removing the per-sprint read changed no rendered
//     value.
//
// Both are measured on the same instrument the tasks page and the sprint page
// are measured with (countingSource, comments_test.go), so the three page costs
// are taken on one instrument rather than three. Each counted read is one
// statement: the driver-level statement counter in internal/db proves that
// ListSprints costs exactly two statements for 1..5 sprints and one for none
// (TestSprintListingResolvesMembershipInABoundedNumberOfStatements), so
// composing the two measurements gives the statement count of a page render
// without either test having to assume what the other proves.

// ==================== READ COST ====================

// TestSprintsPage_IssuesOneReadAndNoneMore is the gate for the sprints page's
// read cost: rendering the page issues exactly ONE read — the sprint listing —
// whatever the number of sprints, and never a member-task read.
//
// The per-sprint alternative the SPEC forbids is measured on the same
// instrument, so the count is falsifiable rather than a number that would read 1
// however the page behaved.
func TestSprintsPage_IssuesOneReadAndNoneMore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Five sprints holding 2, 0, 0, 2 and 0 member tasks: the roadmap has several
	// sprints, and both a sprint that holds work and one that holds none.
	f := seedSprintFixture(t, "settlement-platform")
	src := openCounting(t, f.name)

	data, err := readSprints(context.Background(), src, f.name)
	if err != nil {
		t.Fatalf("readSprints: %v", err)
	}

	if src.sprintListings != 1 {
		t.Errorf("the sprints page issued %d sprint listings, want exactly 1", src.sprintListings)
	}
	if src.sprintTasks != 0 {
		t.Errorf("the sprints page issued %d member-task reads, want 0: it renders every sprint "+
			"as a card with no member tasks on it, and the footer count comes from the sprint "+
			"record the listing returned", src.sprintTasks)
	}
	if src.groupedCommentCounts != 0 || src.perTaskComments != 0 || src.sprintComments != 0 {
		t.Errorf("the sprints page issued %d grouped, %d per-task and %d sprint comment reads, "+
			"want 0 of each: no card shows a comment",
			src.groupedCommentCounts, src.perTaskComments, src.sprintComments)
	}
	if src.taskList != 0 || src.boundedTaskList != 0 {
		t.Errorf("the sprints page issued %d unbounded and %d bounded task-list reads, want 0 of "+
			"each: it does not render the task table", src.taskList, src.boundedTaskList)
	}

	// The one listing covered EVERY rendered sprint, which is what makes one read
	// sufficient rather than merely few.
	rendered := len(data.SprintsUpcoming) + len(data.SprintsCurrent) + len(data.SprintsClosed)
	if rendered != 5 {
		t.Fatalf("the page rendered %d sprints, want the fixture's 5", rendered)
	}

	// The cost does not grow with the number of sprints: the same single read for
	// 1, 3 and 12 of them, and one for a roadmap with no sprint at all.
	for _, sprintCount := range []int{0, 1, 3, 12} {
		name := "acquirer-feed-migration-" + itoa(sprintCount)
		seedSprintsWithMembers(t, name, sprintCount)
		counted := openCounting(t, name)

		page, rerr := readSprints(context.Background(), counted, name)
		if rerr != nil {
			t.Fatalf("%d sprints: readSprints: %v", sprintCount, rerr)
		}
		if counted.sprintListings != 1 || counted.sprintTasks != 0 {
			t.Errorf("%d sprints: the page issued %d sprint listings and %d member-task reads; "+
				"want 1 and 0", sprintCount, counted.sprintListings, counted.sprintTasks)
		}
		got := len(page.SprintsUpcoming) + len(page.SprintsCurrent) + len(page.SprintsClosed)
		if got != sprintCount {
			t.Errorf("%d sprints: the page rendered %d of them", sprintCount, got)
		}

		// The control that makes those counts falsifiable: the per-sprint
		// alternative the SPEC forbids, measured on the same instrument. It climbs
		// with the number of sprints; the page's cost does not.
		counted.sprintTasks = 0
		for i := range sprintCount {
			if _, terr := counted.GetSprintTasksFull(context.Background(), i+1, nil, false); terr != nil {
				t.Fatalf("%d sprints: per-sprint control read: %v", sprintCount, terr)
			}
		}
		if counted.sprintTasks != sprintCount {
			t.Errorf("%d sprints: the per-sprint control issued %d reads, want %d; the instrument "+
				"does not track reads one-for-one", sprintCount, counted.sprintTasks, sprintCount)
		}
	}
}

// TestSprintsPage_ReadsNothingBeyondTheListing proves the claim above at the
// seam rather than by counting: readSprints is handed a source that implements
// ListSprints and NOTHING else, and renders the page from it.
//
// The stub carries no database at all, so any read beyond the listing would fail
// to compile — either because sprintsSource does not offer the method, or
// because widening sprintsSource to offer it would leave the stub no longer
// satisfying the interface. The page's read surface is therefore narrow by
// construction, not by discipline.
//
// It is also where the card footer's provenance is isolated: the stub returns a
// sprint whose TaskCount disagrees with the length of its Tasks id list, so the
// asserted footer can only have come from TaskCount.
func TestSprintsPage_ReadsNothingBeyondTheListing(t *testing.T) {
	// TaskCount and len(Tasks) are deliberately inconsistent, which the database
	// never produces (resolveSprintMembership sets one from the other). It is a
	// probe: it makes the two candidate sources of the footer number distinguish-
	// able, so the assertion identifies which one the card reads.
	src := &listingOnlySource{sprints: []models.Sprint{
		{ID: 4, Status: models.SprintOpen, Order: 1, Title: "Settlement reconciliation",
			TaskCount: 7, Tasks: []int{11, 12}},
		{ID: 9, Status: models.SprintPending, Order: 2, Title: "Ledger archival",
			TaskCount: 0, Tasks: []int{}},
	}}

	data, err := readSprints(context.Background(), src, "settlement-platform")
	if err != nil {
		t.Fatalf("readSprints over a listing-only source: %v", err)
	}
	if src.calls != 1 {
		t.Errorf("readSprints called the listing %d times, want exactly 1", src.calls)
	}
	if len(data.SprintsCurrent) != 1 || len(data.SprintsUpcoming) != 1 {
		t.Fatalf("the page classified the two sprints as %d current and %d upcoming, want 1 and 1",
			len(data.SprintsCurrent), len(data.SprintsUpcoming))
	}

	if got := data.SprintsCurrent[0].Card("settlement-platform").TaskCount; got != 7 {
		t.Errorf("the card footer of sprint #4 shows %d, want its task_count 7; %d would mean the "+
			"footer is counting a loaded member-task list instead", got, len(src.sprints[0].Tasks))
	}
	if got := data.SprintsUpcoming[0].Card("settlement-platform").TaskCount; got != 0 {
		t.Errorf("the card footer of the empty sprint #9 shows %d, want 0", got)
	}
}

// listingOnlySource is the sprints page's read surface and nothing more: it
// answers the sprint listing from a fixed slice and offers no other method. It
// satisfies sprintsSource; it satisfies no wider interface, which is the point.
type listingOnlySource struct {
	sprints []models.Sprint
	calls   int
}

func (s *listingOnlySource) ListSprints(_ context.Context,
	_ *models.SprintStatus) ([]models.Sprint, error) {
	s.calls++
	return s.sprints, nil
}

// The sprints page reads through this interface, and a *db.DB is what the
// handler supplies, so both facts are pinned at compile time.
var (
	_ sprintsSource = (*listingOnlySource)(nil)
	_ sprintsSource = (*db.DB)(nil)
)

// ==================== NOTHING COMPUTED THAT NOTHING RENDERS ====================

// TestSprintView_CarriesOnlyWhatTheCardRenders pins the shape of the sprints
// page's per-sprint view model: the sprint record, and nothing else.
//
// The page renders every sprint through the shared sprintCard partial, whose
// context is .Name, .Sprint and .TaskCount. A field beyond Sprint would be a
// value the page computes and no template reads — which is how the member-task
// slice and the completion summary survived on this path after sprint 35
// removed the inline task table that used to render them. This assertion is what
// stops that recurring: adding a field here fails the test, and the failure says
// where the value must live instead.
func TestSprintView_CarriesOnlyWhatTheCardRenders(t *testing.T) {
	view := reflect.TypeOf(sprintView{})

	got := make([]string, 0, view.NumField())
	for i := range view.NumField() {
		got = append(got, view.Field(i).Name)
	}

	want := []string{"Sprint"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sprintView carries the fields %v, want exactly %v.\n"+
			"The sprints page renders every sprint as a card with no member tasks on it, so it "+
			"must compute nothing beyond the sprint record: the footer count is Sprint.TaskCount, "+
			"which the listing already resolved. Member tasks and the completion summary belong "+
			"to sprintPageData, which the single Roadmap Sprint Page renders through the "+
			"sprintDetail sub-template.", got, want)
	}
}

// ==================== THE RENDERED FOOTER ====================

// sprintCardFooter matches one sprint card's link and its footer count in the
// order the shared partial emits them, so a card's number is read from that
// card's own markup rather than from somewhere on the page.
var sprintCardFooter = regexp.MustCompile(
	`/sprints/(\d+)"[^>]*>(?s:.*?)<span class="text-secondary">(\d+) task\(s\)</span>`)

// TestSprintsPage_CardFooterShowsTheSprintsOwnTaskCount asserts that every card
// on the rendered page shows the sprint's total member-task count, and that the
// number is the same one a full member-task read would have produced — the read
// the page no longer takes.
//
// It is the end-to-end half of the change: the counting test above proves the
// member read is gone, and this proves nothing rendered changed when it went,
// including for a sprint that holds no task at all.
func TestSprintsPage_CardFooterShowsTheSprintsOwnTaskCount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Sprints holding 0, 1, 2, 3 and 4 member tasks, so the assertion discriminates
	// between the counts rather than reading one number five times, and covers the
	// empty sprint.
	const name = "chargeback-automation"
	memberCounts := seedSprintsWithMembers(t, name, 5)
	if len(memberCounts) != 5 {
		t.Fatalf("the fixture seeded %d sprints, want 5", len(memberCounts))
	}
	distinct := map[int]bool{}
	for _, n := range memberCounts {
		distinct[n] = true
	}
	if len(distinct) != len(memberCounts) {
		t.Fatalf("the seeded member counts %v are not all distinct; the assertion would not "+
			"discriminate between cards", memberCounts)
	}

	body := servePage(t, buildMux(), "/roadmaps/"+name)

	rendered := map[int]int{}
	for _, m := range sprintCardFooter.FindAllStringSubmatch(body, -1) {
		id, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("parsing sprint id %q from a card: %v", m[1], err)
		}
		count, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("parsing the footer count %q of sprint #%d: %v", m[2], id, err)
		}
		if prev, seen := rendered[id]; seen {
			t.Fatalf("sprint #%d is rendered twice, with footers %d and %d", id, prev, count)
		}
		rendered[id] = count
	}
	if len(rendered) != len(memberCounts) {
		t.Fatalf("the page renders %d sprint cards, want %d; the footer extraction is broken "+
			"(found %v)", len(rendered), len(memberCounts), rendered)
	}

	// The authority the footer is compared against is a FULL member-task read of
	// each sprint — exactly the read loadSprints used to take. Comparing against it
	// is what makes "the same count it showed before" a measurement rather than an
	// assertion.
	database, err := db.OpenReadOnly(name)
	if err != nil {
		t.Fatalf("opening roadmap %q read-only: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // read-only handle; test cleanup

	empties := 0
	for id, want := range memberCounts {
		members, merr := database.GetSprintTasksFull(context.Background(), id, nil, false)
		if merr != nil {
			t.Fatalf("reading the member tasks of sprint #%d: %v", id, merr)
		}
		if len(members) != want {
			t.Fatalf("sprint #%d holds %d member tasks, want the seeded %d", id, len(members), want)
		}
		if len(members) == 0 {
			empties++
		}
		if got := rendered[id]; got != len(members) {
			t.Errorf("the card footer of sprint #%d shows %d task(s), but the sprint holds %d "+
				"member tasks", id, got, len(members))
		}
	}
	if empties == 0 {
		t.Error("no seeded sprint is empty, so the empty-sprint case was never exercised")
	}
}

// seedSprintsWithMembers creates a roadmap holding n sprints, the i-th of which
// holds i member tasks, and returns the member count keyed by sprint id. n may be
// 0: the roadmap then exists with no sprint at all, which is the case the page
// must still render with a single read.
//
// The first sprint therefore holds none, so every fixture with at least one
// sprint exercises the empty-sprint footer.
func seedSprintsWithMembers(t *testing.T, name string, n int) map[int]int {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	titles := []string{
		"Settlement reconciliation", "Authentication hardening", "Observability rollout",
		"Ledger archival", "Acquirer feed migration", "Chargeback automation",
		"Dispute intake rework", "Payout scheduling", "Fee schedule versioning",
		"Merchant onboarding checks", "Refund reversal handling", "Statement generation",
	}

	counts := make(map[int]int, n)
	for i := range n {
		title := titles[i%len(titles)] + " " + itoa(i+1)
		sprintID, serr := seedSprint(database, &models.Sprint{
			Status:      models.SprintPending,
			Title:       title,
			Description: "Deliver the " + strings.ToLower(title) + " workstream end to end.",
			CreatedAt:   now,
			Order:       i + 1,
		})
		if serr != nil {
			t.Fatalf("creating sprint %q: %v", title, serr)
		}

		taskIDs := make([]int, 0, i)
		for j := range i {
			taskID, terr := seedTask(database, seededTask(now, title+", member task "+itoa(j+1)))
			if terr != nil {
				t.Fatalf("creating a member task of sprint %q: %v", title, terr)
			}
			taskIDs = append(taskIDs, taskID)
		}
		if len(taskIDs) > 0 {
			if aerr := database.AddTasksToSprint(ctx, sprintID, taskIDs); aerr != nil {
				t.Fatalf("adding %d tasks to sprint %q: %v", len(taskIDs), title, aerr)
			}
		}
		counts[sprintID] = i
	}
	return counts
}
