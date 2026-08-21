package db

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file covers the grouped membership read of SPEC/DATABASE.md § Read the
// Membership of Many Sprints (Grouped) and the guarantee it exists to serve:
// EVERY read that returns a Sprint object publishes that sprint's membership, so
// `rmp sprint list`, `rmp sprint get` and `rmp sprint tasks` never disagree about
// the same sprint (SPEC/COMMANDS.md § List Sprints, "One sprint, one answer";
// SPEC/MODELS.md § Sprint Field Constraints).
//
// The defect these gates exist for (rmp task #233): the listing left both
// computed fields at their zero values, so every sprint reported `task_count` 0
// and `tasks` null whatever it actually held, while GetSprint reported the truth
// for the same sprint at the same moment. A listing that cannot say how much work
// a sprint carries is a listing nobody can plan from.
//
// As elsewhere in this package the properties are measured apart, each on the
// instrument that can actually falsify it: what the read RETURNS is measured here
// against a real schema, what it COSTS is measured with the driver-level
// statement counter of comments_stmtcount_test.go, and how it is PLANNED is
// measured with EXPLAIN QUERY PLAN in index_test.go.

// membershipFixture names what seedSprintMembershipFixture created, so the
// assertions bind ids that exist rather than assuming an autoincrement sequence.
type membershipFixture struct {
	// wantTasks maps each seeded sprint to the member ids its Tasks field must
	// carry, in the exact order it must carry them: ascending task id.
	wantTasks map[int][]int
	// sprintIDs are the seeded sprints, largest membership first, so a failure
	// message reads in a predictable order.
	sprintIDs []int
	// empty is the sprint that holds no task at all.
	empty int
	// scrambled is the sprint whose planned in-sprint positions are deliberately
	// NOT the ascending-id order, so an assertion about ascending id cannot pass
	// by accident.
	scrambled int
	// backlogMember is a member task parked in BACKLOG status: membership is a
	// sprint_tasks row, not a task status, so it must still be published and
	// counted (SPEC/STATE_MACHINE.md § Sprint Membership and the BACKLOG Status).
	backlogMember int
}

// seedSprintMembershipFixture creates four sprints of four different sizes —
// four members, two, one, and none — through the production write paths.
func seedSprintMembershipFixture(t *testing.T, db *DB) membershipFixture {
	t.Helper()

	reconciliation := newTestSprintWithCap(t, db, "Settlement reconciliation", 0)
	authentication := newTestSprintWithCap(t, db, "Authentication hardening", 0)
	observability := newTestSprintWithCap(t, db, "Observability rollout", 0)
	archival := newTestSprintWithCap(t, db, "Ledger archival", 0)

	members := make([]int, 0, 7)
	for _, title := range []string{
		"Reconcile the settlement ledger against the acquirer report",
		"Alert on any settlement window that fails to balance",
		"Replay a settlement window from the acquirer feed",
		"Document the settlement reconciliation runbook",
		"Rate-limit the token endpoint",
		"Audit the session-cookie flags",
		"Trace the settlement pipeline end to end",
	} {
		members = append(members, newTestTask(t, db, title))
	}

	// The ids go in deliberately scrambled, so sprint_tasks.position — the
	// sprint's planned execution order — is not the ascending-id order the Tasks
	// field publishes. An assertion about ascending id would otherwise be
	// satisfied by any read that simply preserved insertion order.
	addTasksToSprint(t, db, reconciliation, members[3], members[1], members[0], members[2])
	addTasksToSprint(t, db, authentication, members[5], members[4])
	addTasksToSprint(t, db, observability, members[6])

	// One member is put back in BACKLOG. AddTasksToSprint moves a task to SPRINT,
	// so without this every member would share one status and the
	// status-independence of membership would go untested.
	backlogMember := members[3]
	if err := db.UpdateTaskStatus(testContext(), []int{backlogMember}, models.StatusBacklog); err != nil {
		t.Fatalf("parking member task %d in BACKLOG: %v", backlogMember, err)
	}

	return membershipFixture{
		wantTasks: map[int][]int{
			reconciliation: sortedCopy(members[0], members[1], members[2], members[3]),
			authentication: sortedCopy(members[4], members[5]),
			observability:  sortedCopy(members[6]),
			archival:       {},
		},
		sprintIDs:     []int{reconciliation, authentication, observability, archival},
		empty:         archival,
		scrambled:     reconciliation,
		backlogMember: backlogMember,
	}
}

// sortedCopy returns the given ids in ascending order.
func sortedCopy(ids ...int) []int {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

// TestSprintListingPublishesTheMembershipOfEverySprint is the regression gate for
// rmp task #233: every Sprint object the listing returns carries its real
// membership, in every size of sprint including the empty one, and the two
// computed fields always agree with each other.
func TestSprintListingPublishesTheMembershipOfEverySprint(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	fixture := seedSprintMembershipFixture(t, db)

	listed := listedSprintsByID(t, db, nil)

	for _, sprintID := range fixture.sprintIDs {
		want := fixture.wantTasks[sprintID]
		got, present := listed[sprintID]
		if !present {
			t.Fatalf("the listing does not return sprint %d at all", sprintID)
		}

		if got.Tasks == nil {
			t.Errorf("sprint %d is listed with a nil Tasks field; the listing must resolve the "+
				"membership of every sprint it returns (SPEC/COMMANDS.md § List Sprints)", sprintID)
		}
		if !slices.Equal(got.Tasks, want) {
			t.Errorf("the listing gives sprint %d the members %v, want %v", sprintID, got.Tasks, want)
		}
		if got.TaskCount != len(want) {
			t.Errorf("the listing gives sprint %d task_count %d, want %d", sprintID, got.TaskCount, len(want))
		}
		// The two fields are two readings of one result, so they can never
		// disagree — not even if one of them were computed some other way.
		if got.TaskCount != len(got.Tasks) {
			t.Errorf("sprint %d is listed with task_count %d but %d ids in tasks; the two fields "+
				"must always agree", sprintID, got.TaskCount, len(got.Tasks))
		}
	}

	// The member parked in BACKLOG is still a member: membership is a
	// sprint_tasks row and status is a tasks column.
	if !slices.Contains(listed[fixture.scrambled].Tasks, fixture.backlogMember) {
		t.Errorf("the listing drops member task %d because its status is BACKLOG; membership does "+
			"not depend on task status (SPEC/STATE_MACHINE.md § Sprint Membership and the BACKLOG Status)",
			fixture.backlogMember)
	}
}

// TestEmptySprintIsListedWithAnEmptyArrayNotNull pins the JSON the caller
// actually receives for a sprint that holds nothing: `"tasks": []`, never
// `"tasks": null` (SPEC/DATA_FORMATS.md § Implementation Notes, Empty arrays).
//
// The assertion is made on the marshalled bytes rather than on the Go value,
// because it is the JSON that the rule constrains and a non-nil empty slice is
// the only Go value that produces it.
func TestEmptySprintIsListedWithAnEmptyArrayNotNull(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	fixture := seedSprintMembershipFixture(t, db)

	listed := listedSprintsByID(t, db, nil)
	fromListing := listed[fixture.empty]
	fromGet, err := db.GetSprint(testContext(), fixture.empty)
	if err != nil {
		t.Fatalf("GetSprint(%d): %v", fixture.empty, err)
	}

	for name, sprint := range map[string]models.Sprint{
		"sprint list": fromListing,
		"sprint get":  *fromGet,
	} {
		encoded, err := json.Marshal(sprint)
		if err != nil {
			t.Fatalf("marshalling the %s object of sprint %d: %v", name, fixture.empty, err)
		}
		if !strings.Contains(string(encoded), `"tasks":[]`) {
			t.Errorf("%s renders the empty sprint %d as %s; it must carry \"tasks\":[] and never null",
				name, fixture.empty, encoded)
		}
		if !strings.Contains(string(encoded), `"task_count":0`) {
			t.Errorf("%s renders the empty sprint %d as %s; it must carry \"task_count\":0",
				name, fixture.empty, encoded)
		}
	}
}

// TestSprintListingGetAndTasksNeverDisagree is the cross-command gate: for every
// sprint, at the same moment, the listing, the single-sprint read and the member
// task listing report the same membership (SPEC/COMMANDS.md § List Sprints, "One
// sprint, one answer").
//
// It fails if any ONE of the three drifts, which is what makes it a gate rather
// than three separate assertions: the defect it guards against was precisely two
// reads of one sprint returning different answers.
func TestSprintListingGetAndTasksNeverDisagree(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	fixture := seedSprintMembershipFixture(t, db)

	for _, filter := range []*models.SprintStatus{nil, sprintStatusPtr(models.SprintPending)} {
		listed := listedSprintsByID(t, db, filter)

		for _, sprintID := range fixture.sprintIDs {
			fromListing, present := listed[sprintID]
			if !present {
				t.Fatalf("sprint %d is missing from the listing (filter %v)", sprintID, filter)
			}

			fromGet, err := db.GetSprint(testContext(), sprintID)
			if err != nil {
				t.Fatalf("GetSprint(%d): %v", sprintID, err)
			}
			memberTasks, err := db.GetSprintTasksFull(testContext(), sprintID, nil, false)
			if err != nil {
				t.Fatalf("GetSprintTasksFull(%d): %v", sprintID, err)
			}

			memberIDs := make([]int, len(memberTasks))
			for i := range memberTasks {
				memberIDs[i] = memberTasks[i].ID
			}

			if !slices.Equal(fromListing.Tasks, fromGet.Tasks) {
				t.Errorf("sprint %d: the listing reports members %v and the single read reports %v; "+
					"the two must never disagree", sprintID, fromListing.Tasks, fromGet.Tasks)
			}
			if fromListing.TaskCount != fromGet.TaskCount {
				t.Errorf("sprint %d: the listing reports task_count %d and the single read reports %d",
					sprintID, fromListing.TaskCount, fromGet.TaskCount)
			}
			// `sprint tasks` returns whole task records in the sprint's planned
			// order, so it is the same SET of ids, not the same sequence.
			if !slices.Equal(sortedCopy(memberIDs...), fromListing.Tasks) {
				t.Errorf("sprint %d: `sprint tasks` returns the members %v and the listing reports %v",
					sprintID, sortedCopy(memberIDs...), fromListing.Tasks)
			}
			if len(memberIDs) != fromListing.TaskCount {
				t.Errorf("sprint %d: `sprint tasks` returns %d tasks and the listing reports task_count %d",
					sprintID, len(memberIDs), fromListing.TaskCount)
			}
		}
	}
}

// TestSprintMembershipIsPublishedInAscendingTaskID pins the order EVERY read
// that publishes the field returns it in, and pins it as a property fixed by the statement rather than one
// inherited from how the rows happen to arrive.
//
// All three reads that return a models.Sprint are driven over the SAME sprint at
// the same moment — the listing, the by-id read, and the open-sprint read — so a
// single one of them drifting fails the test. SPEC/MODELS.md § Sprint Field
// Constraints binds every read that returns a Sprint object, not a chosen two.
//
// The fixture inserts a sprint's members in a scrambled order, so the sprint's
// planned position order genuinely differs from ascending id. The test asserts
// that difference first: without it, "the ids come back ascending" would be
// satisfied by a read that merely preserved insertion order, and the assertion
// would prove nothing.
func TestSprintMembershipIsPublishedInAscendingTaskID(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	fixture := seedSprintMembershipFixture(t, db)

	plannedOrder, err := db.GetSprintTasksFull(testContext(), fixture.scrambled, nil, false)
	if err != nil {
		t.Fatalf("GetSprintTasksFull(%d): %v", fixture.scrambled, err)
	}
	planned := make([]int, len(plannedOrder))
	for i := range plannedOrder {
		planned[i] = plannedOrder[i].ID
	}
	if slices.IsSorted(planned) {
		t.Fatalf("the fixture sprint's planned order %v is already ascending, so an ascending-id "+
			"assertion would prove nothing; the fixture must scramble the positions", planned)
	}

	listed := listedSprintsByID(t, db, nil)
	fromGet, err := db.GetSprint(testContext(), fixture.scrambled)
	if err != nil {
		t.Fatalf("GetSprint(%d): %v", fixture.scrambled, err)
	}

	// The third read that publishes the field. Only one sprint may be OPEN at a
	// time, so the scrambled sprint is opened here rather than in the shared
	// fixture: that keeps the fixture all-PENDING for the status-filter gate
	// above, and puts the read under test on the sprint whose planned order is
	// deliberately not the ascending one.
	if err := db.UpdateSprintStatus(testContext(), fixture.scrambled, models.SprintOpen); err != nil {
		t.Fatalf("opening sprint %d: %v", fixture.scrambled, err)
	}
	fromOpen, err := db.GetOpenSprint(testContext())
	if err != nil {
		t.Fatalf("GetOpenSprint: %v", err)
	}
	if fromOpen.ID != fixture.scrambled {
		t.Fatalf("GetOpenSprint returned sprint %d, want the one just opened (%d)",
			fromOpen.ID, fixture.scrambled)
	}

	want := fixture.wantTasks[fixture.scrambled]
	for name, got := range map[string][]int{
		"sprint list":          listed[fixture.scrambled].Tasks,
		"sprint get":           fromGet.Tasks,
		"the open-sprint read": fromOpen.Tasks,
	} {
		if got == nil {
			t.Errorf("%s publishes a nil Tasks field for sprint %d; every read that returns a "+
				"Sprint object must resolve both computed fields", name, fixture.scrambled)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s publishes sprint %d's members as %v, want %v in ascending task id "+
				"(SPEC/MODELS.md § Sprint Field Constraints); the planned order %v belongs to "+
				"`sprint tasks`, not to this field",
				name, fixture.scrambled, got, want, planned)
		}
	}
	if fromOpen.TaskCount != len(want) {
		t.Errorf("the open-sprint read gives sprint %d task_count %d, want %d",
			fixture.scrambled, fromOpen.TaskCount, len(want))
	}
}

// TestTheOpenSprintReadPublishesAnEmptyArrayNotNull covers the third read's
// empty case, which is the one an aggregate ORDER BY could plausibly have
// broken: the outer join still contributes a single NULL row for a sprint that
// holds nothing, and it must still read back as [] and never null
// (SPEC/DATA_FORMATS.md § Implementation Notes, Empty arrays).
func TestTheOpenSprintReadPublishesAnEmptyArrayNotNull(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	fixture := seedSprintMembershipFixture(t, db)

	if err := db.UpdateSprintStatus(testContext(), fixture.empty, models.SprintOpen); err != nil {
		t.Fatalf("opening the empty sprint %d: %v", fixture.empty, err)
	}
	fromOpen, err := db.GetOpenSprint(testContext())
	if err != nil {
		t.Fatalf("GetOpenSprint: %v", err)
	}
	if fromOpen.ID != fixture.empty {
		t.Fatalf("GetOpenSprint returned sprint %d, want the empty one (%d)", fromOpen.ID, fixture.empty)
	}

	encoded, err := json.Marshal(fromOpen)
	if err != nil {
		t.Fatalf("marshalling the open-sprint object: %v", err)
	}
	if !strings.Contains(string(encoded), `"tasks":[]`) {
		t.Errorf("the open-sprint read renders the empty sprint as %s; it must carry \"tasks\":[] "+
			"and never null", encoded)
	}
	if !strings.Contains(string(encoded), `"task_count":0`) {
		t.Errorf("the open-sprint read renders the empty sprint as %s; it must carry \"task_count\":0",
			encoded)
	}
}

// TestEverySprintMembershipAggregationStatesItsOrder is the structural guard
// against a THIRD instance of this defect class.
//
// The class appeared twice — GetSprint and GetOpenSprint each aggregated a
// sprint's member ids with json_group_array and neither stated an order, so both
// returned ascending ids only because DISTINCT dedupes through a sorted
// ephemeral index and the join happens to walk idx_sprint_tasks_lookup in
// (sprint_id, task_id) order. Both are properties of a query plan, and
// SPEC/MODELS.md § Sprint Field Constraints specifies an order that may not rest
// on one.
//
// The behavioural tests above catch a read that is WRONG today; this one catches
// a read added tomorrow that is right only by accident. It scans the production
// SQL of this package for the aggregation and requires every occurrence to carry
// its own ORDER BY.
//
// It reads STRING LITERALS only, taken from the parsed syntax tree, so a doc
// comment that names the function cannot satisfy it and cannot trip it either.
func TestEverySprintMembershipAggregationStatesItsOrder(t *testing.T) {
	const aggregate = "json_group_array("

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, filepath.Join(moduleRoot(t), "internal", "db"),
		func(entry fs.FileInfo) bool { return !strings.HasSuffix(entry.Name(), "_test.go") },
		0)
	if err != nil {
		t.Fatalf("parsing the production sources of internal/db: %v", err)
	}

	found := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				lit, ok := node.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				for offset := 0; ; {
					at := strings.Index(lit.Value[offset:], aggregate)
					if at < 0 {
						return true
					}
					call := balancedCall(lit.Value[offset+at+len(aggregate)-1:])
					found++
					if !strings.Contains(call, "ORDER BY") {
						t.Errorf("%s:%d: json_group_array%s aggregates sprint membership without "+
							"stating an ORDER BY; the ids it publishes would be ordered only by "+
							"the query plan (SPEC/MODELS.md § Sprint Field Constraints)",
							filepath.Base(path), fset.Position(lit.Pos()).Line, call)
					}
					offset += at + len(aggregate)
				}
			})
		}
	}

	// The sweep is only meaningful if it actually found the aggregations. Both
	// reads that use one must be seen; a count below that means the scan is not
	// looking where it thinks it is.
	if found < 2 {
		t.Fatalf("the scan found %d json_group_array call(s) in the production SQL of internal/db, "+
			"want at least 2 (GetSprint and GetOpenSprint); the scan is not looking where it thinks "+
			"it is", found)
	}
}

// balancedCall returns the parenthesised argument list that starts at the "(" at
// the head of s, including both parentheses. It stops at the matching close, so
// a nested call inside the arguments does not truncate the result.
func balancedCall(s string) string {
	depth := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[:i+1]
			}
		}
	}
	return s
}

// TestGroupedSprintMembershipRead covers the grouped read on its own terms: it
// keys every sprint by its own members, leaves a sprint that holds nothing out of
// the map, tolerates ids that name no sprint at all, and returns an empty map for
// an empty id set.
func TestGroupedSprintMembershipRead(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	fixture := seedSprintMembershipFixture(t, db)

	// One id that names no sprint at all, alongside the four real ones. The
	// grouped read is fed whatever the listing returned, so it must be robust to
	// an id with no row rather than assume one exists.
	const noSuchSprint = 987654
	ids := append(slices.Clone(fixture.sprintIDs), noSuchSprint)

	membership, err := db.tasksBySprints(testContext(), ids)
	if err != nil {
		t.Fatalf("tasksBySprints: %v", err)
	}

	for _, sprintID := range fixture.sprintIDs {
		want := fixture.wantTasks[sprintID]
		got, present := membership[sprintID]
		if len(want) == 0 {
			// A sprint with no member has no sprint_tasks row, so the absence of
			// an entry IS the answer.
			if present {
				t.Errorf("the empty sprint %d is present in the map with %v; a sprint with no "+
					"member must be absent", sprintID, got)
			}
			continue
		}
		if !slices.Equal(got, want) {
			t.Errorf("the grouped read gives sprint %d the members %v, want %v", sprintID, got, want)
		}
	}
	if got, present := membership[noSuchSprint]; present {
		t.Errorf("sprint id %d names no sprint but carries %v in the map", noSuchSprint, got)
	}

	// Each sprint resolves to ITS OWN members, so a read that returned every
	// member for every sprint could not pass.
	for _, a := range fixture.sprintIDs {
		for _, b := range fixture.sprintIDs {
			if a == b {
				continue
			}
			for _, id := range membership[a] {
				if slices.Contains(membership[b], id) {
					t.Errorf("task %d is reported as a member of both sprint %d and sprint %d", id, a, b)
				}
			}
		}
	}

	// An empty id set: an empty map, no error, and never a nil one. (That it
	// also issues no statement is proven below.)
	empty, err := db.tasksBySprints(testContext(), nil)
	if err != nil {
		t.Fatalf("tasksBySprints with no ids: %v", err)
	}
	if empty == nil {
		t.Error("tasksBySprints returned a nil map for an empty id set; it must return an empty map")
	}
	if len(empty) != 0 {
		t.Errorf("tasksBySprints with no ids returned %d entries, want 0", len(empty))
	}

	// Duplicate ids are harmless: the members of a sprint named twice are still
	// listed once.
	duplicated, err := db.tasksBySprints(testContext(),
		[]int{fixture.scrambled, fixture.scrambled})
	if err != nil {
		t.Fatalf("tasksBySprints with duplicate ids: %v", err)
	}
	if got := duplicated[fixture.scrambled]; !slices.Equal(got, fixture.wantTasks[fixture.scrambled]) {
		t.Errorf("a duplicated sprint id yielded the members %v, want %v",
			got, fixture.wantTasks[fixture.scrambled])
	}
}

// TestSprintListingResolvesMembershipInABoundedNumberOfStatements is the
// database-level gate for the read-cost rule of SPEC/COMMANDS.md § List Sprints:
// the listing resolves the membership of every sprint it returns in a bounded
// number of statements that does not grow with the number of sprints.
//
// The bound is TWO — the sprint rows, then one grouped membership read — and it
// is measured at the driver boundary, which counts round trips to SQLite and is
// blind to how the SQL was built.
//
// The per-sprint alternative the SPEC forbids is measured on the same instrument,
// so the assertion is falsifiable rather than a count that would read 2 however
// the listing behaved.
func TestSprintListingResolvesMembershipInABoundedNumberOfStatements(t *testing.T) {
	db, counter, cleanup := setupCountingDB(t)
	defer cleanup()

	// A roadmap with no sprint at all costs ONE statement: the grouped read is
	// skipped outright rather than sent with an empty IN list.
	counter.reset()
	none, err := db.ListSprints(testContext(), nil)
	if err != nil {
		t.Fatalf("ListSprints on an empty roadmap: %v", err)
	}
	if got := counter.count(); got != 1 {
		t.Errorf("listing a roadmap with no sprint issued %d statements, want exactly 1", got)
	}
	if none == nil || len(none) != 0 {
		t.Errorf("listing a roadmap with no sprint returned %v, want a non-nil empty slice", none)
	}

	// Sprints are added one at a time, each with a different number of members,
	// and the cost is measured after every addition: a per-sprint read would
	// climb, a bounded one does not.
	titles := []string{
		"Settlement reconciliation",
		"Authentication hardening",
		"Observability rollout",
		"Ledger archival",
		"Acquirer feed migration",
	}
	for size, title := range titles {
		sprintID := newTestSprintWithCap(t, db, title, 0)
		for member := range size {
			taskID := newTestTask(t, db,
				title+", member task "+string(rune('A'+member)))
			addTasksToSprint(t, db, sprintID, taskID)
		}
		if counter.count() == 0 {
			t.Fatal("the statement counter did not observe the seeding writes, so it is not counting")
		}

		counter.reset()
		sprints, err := db.ListSprints(testContext(), nil)
		if err != nil {
			t.Fatalf("ListSprints with %d sprints: %v", size+1, err)
		}
		if got := counter.count(); got != 2 {
			t.Errorf("listing %d sprints issued %d statements, want exactly 2 (the sprint rows, "+
				"then ONE grouped membership read)", size+1, got)
		}
		if len(sprints) != size+1 {
			t.Fatalf("the listing returned %d sprints, want %d", len(sprints), size+1)
		}
		for i := range sprints {
			if sprints[i].Tasks == nil {
				t.Errorf("sprint %d is listed with a nil Tasks field", sprints[i].ID)
			}
			if sprints[i].TaskCount != len(sprints[i].Tasks) {
				t.Errorf("sprint %d is listed with task_count %d and %d ids",
					sprints[i].ID, sprints[i].TaskCount, len(sprints[i].Tasks))
			}
		}

		// The alternative the SPEC forbids, on the same instrument: the listing
		// followed by one read per sprint. This is what makes the bound above
		// falsifiable — it climbs with the number of sprints, and the bounded
		// read does not.
		counter.reset()
		listed, err := db.ListSprints(testContext(), nil)
		if err != nil {
			t.Fatalf("ListSprints control read: %v", err)
		}
		for i := range listed {
			if _, err := db.GetSprint(testContext(), listed[i].ID); err != nil {
				t.Fatalf("per-sprint control read of sprint %d: %v", listed[i].ID, err)
			}
		}
		if got, want := counter.count(), 2+len(listed); got != want {
			t.Errorf("the per-sprint control over %d sprints issued %d statements, want %d; the "+
				"instrument does not track statements one-for-one", len(listed), got, want)
		}
	}

	// The grouped read alone, on an empty id set, issues nothing at all.
	counter.reset()
	if _, err := db.tasksBySprints(testContext(), nil); err != nil {
		t.Fatalf("tasksBySprints with no ids: %v", err)
	}
	if got := counter.count(); got != 0 {
		t.Errorf("the grouped membership read of an empty id set issued %d statements, want 0", got)
	}
}

// listedSprintsByID runs the production listing and keys the result by sprint id.
func listedSprintsByID(t *testing.T, db *DB, status *models.SprintStatus) map[int]models.Sprint {
	t.Helper()

	sprints, err := db.ListSprints(testContext(), status)
	if err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	byID := make(map[int]models.Sprint, len(sprints))
	for i := range sprints {
		byID[sprints[i].ID] = sprints[i]
	}
	return byID
}

// sprintStatusPtr returns a pointer to a sprint status, for the optional filter.
func sprintStatusPtr(status models.SprintStatus) *models.SprintStatus {
	return &status
}
