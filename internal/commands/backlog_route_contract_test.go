// Package commands — gates that tie what the binary SAYS about the routes into
// and out of BACKLOG to what it DOES (rmp task #232).
//
// SPEC/STATE_MACHINE.md § Sprint Membership and the BACKLOG Status binds
// membership and status by one invariant: a sprint member is never in BACKLOG,
// and a task in SPRINT, DOING or TESTING belongs to a sprint. Several
// descriptions inside the binary describe that relationship, and prose drifts
// silently, so each is pinned here to the behaviour it describes, by a test that
// first OBSERVES the behaviour and only then reads the sentence.
//
// The observation always comes first, and the assertion is two-way wherever the
// sentence has an opposite: a gate that only checked "the summary contains this
// phrase" would keep passing after the behaviour changed; the branch on the
// observed value is what makes it fail then.
//
// The doc comment on taskSetStatus is pinned by its own file,
// task_stat_doc_comment_test.go, which reuses the fixture below.
package commands

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

// backlogRouteFixture is a roadmap holding one OPEN sprint and nothing else.
// Tasks are manufactured on demand, each into whichever state the caller asks
// for, so no test depends on a state an earlier one left behind and no test has
// to count how many tasks the fixture happened to seed.
type backlogRouteFixture struct {
	database *db.DB
	roadmap  string
	sprintID int
	seq      int
}

// Commit hashes used by the walks below. Real short hashes from this
// repository's history, so the fixture reads like a roadmap someone worked
// through rather than like filler.
const (
	backlogRouteCommitOpen  = "5f93b51"
	backlogRouteCommitClose = "391cff7"
)

// setupBacklogRouteRoadmap builds the fixture through the real commands, so
// every state the tests meet is a state the CLI can actually produce.
func setupBacklogRouteRoadmap(t *testing.T, name string) *backlogRouteFixture {
	t.Helper()

	t.Setenv("HOME", shortHome(t))

	database, cleanup := setupTestTaskRoadmap(t, name)
	t.Cleanup(cleanup)

	f := &backlogRouteFixture{roadmap: name, database: database}

	run(t, func() error {
		return sprintCreate([]string{
			"-r", name, "-t", "Session store hardening",
			"-d", "Persist sessions to the shared store so a node restart keeps every live session.",
		})
	})
	sprints, err := database.ListSprints(context.Background(), nil)
	if err != nil {
		t.Fatalf("reading the seeded sprint back: %v", err)
	}
	if len(sprints) != 1 {
		t.Fatalf("seeded 1 sprint, found %d", len(sprints))
	}
	f.sprintID = sprints[0].ID

	run(t, func() error { return sprintStart([]string{"-r", name, itoa(f.sprintID)}) })

	return f
}

// backlogRouteTitles are the task titles the fixture cycles through, so a
// failure message names a task that reads like real roadmap work.
var backlogRouteTitles = []string{
	"Rotate the JWT signing key without downtime",
	"Move session tokens to the encrypted store",
	"Rate-limit the password reset endpoint",
	"Record the audit row inside the mutation transaction",
	"Retire the unauthenticated health endpoint",
}

// newTask creates one task and returns the id the command itself reported. The
// id is read out of the command's stdout rather than out of the table, so a
// task this fixture believes it created is a task the CLI said it created.
func (f *backlogRouteFixture) newTask(t *testing.T) int {
	t.Helper()

	f.seq++
	title := backlogRouteTitles[f.seq%len(backlogRouteTitles)]

	out := captureStdout(t, func() {
		if err := taskCreate([]string{
			"-r", f.roadmap,
			"-t", title + " (" + itoa(f.seq) + ")",
			"-fr", "The behaviour survives a restart of every node in the pool.",
			"-tr", "Route the write through the shared store and migrate the existing rows.",
			"-ac", "A restart leaves every live session usable.",
		}); err != nil {
			t.Fatalf("creating a task: %v", err)
		}
	})

	var created struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("task create printed %q, which is not the {\"id\": N} object SPEC/COMMANDS.md promises: %v",
			out, err)
	}
	if created.ID <= 0 {
		t.Fatalf("task create reported id %d", created.ID)
	}
	return created.ID
}

// taskInState manufactures a fresh task and walks it, through the real
// commands, into the requested state. The walk is verified before the id is
// handed back, so a test can never assert about a source state it did not reach.
func (f *backlogRouteFixture) taskInState(t *testing.T, status models.TaskStatus) int {
	t.Helper()

	id := f.newTask(t)
	if status == models.StatusBacklog {
		return id
	}

	f.addToSprint(t, id)
	switch status {
	case models.StatusSprint:
	case models.StatusDoing:
		f.mustStat(t, id, models.StatusDoing, "--commit-open", backlogRouteCommitOpen)
	case models.StatusTesting:
		f.mustStat(t, id, models.StatusDoing, "--commit-open", backlogRouteCommitOpen)
		f.mustStat(t, id, models.StatusTesting)
	case models.StatusCompleted:
		f.mustStat(t, id, models.StatusDoing, "--commit-open", backlogRouteCommitOpen)
		f.mustStat(t, id, models.StatusTesting)
		f.mustStat(t, id, models.StatusCompleted, "--commit-close", backlogRouteCommitClose)
	default:
		t.Fatalf("no route to state %s", status)
	}

	if got := f.statusOf(t, id); got != status {
		t.Fatalf("task #%d was walked to %s but reads %s", id, status, got)
	}
	return id
}

func (f *backlogRouteFixture) addToSprint(t *testing.T, id int) {
	t.Helper()
	run(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.sprintID), itoa(id)})
	})
}

// mustStat runs `task stat` and fails the test if the command refuses.
func (f *backlogRouteFixture) mustStat(t *testing.T, id int, status models.TaskStatus, extra ...string) {
	t.Helper()
	args := append([]string{"-r", f.roadmap, itoa(id), string(status)}, extra...)
	_ = captureStdout(t, func() {
		if err := taskSetStatus(args); err != nil {
			t.Fatalf("task stat %v: %v", args, err)
		}
	})
}

// tryStat runs `task stat` and returns whatever it returned, error or nil.
func (f *backlogRouteFixture) tryStat(t *testing.T, id int, status models.TaskStatus, extra ...string) error {
	t.Helper()
	args := append([]string{"-r", f.roadmap, itoa(id), string(status)}, extra...)
	var err error
	_ = captureStdout(t, func() { err = taskSetStatus(args) })
	return err
}

func (f *backlogRouteFixture) statusOf(t *testing.T, id int) models.TaskStatus {
	t.Helper()
	tasks, err := f.database.GetTasks(context.Background(), []int{id})
	if err != nil {
		t.Fatalf("reading task #%d back: %v", id, err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task #%d is gone", id)
	}
	return tasks[0].Status
}

// isSprintMember reads the membership straight out of sprint_tasks. The row is
// the fact these tests are about, and a read path that filtered by status would
// hide from the assertion exactly the row it exists to observe.
func (f *backlogRouteFixture) isSprintMember(t *testing.T, id int) bool {
	t.Helper()
	var n int
	if err := f.database.QueryRow(
		"SELECT COUNT(*) FROM sprint_tasks WHERE task_id = ?", id).Scan(&n); err != nil {
		t.Fatalf("reading sprint_tasks for task #%d: %v", id, err)
	}
	return n > 0
}

// ---------------------------------------------------------------------------
// Site 3: the `backlog` family summary
// ---------------------------------------------------------------------------

// backlogListIDs runs `backlog list` and returns the ids it printed.
func (f *backlogRouteFixture) backlogListIDs(t *testing.T) map[int]bool {
	t.Helper()
	out := captureStdout(t, func() {
		if err := backlogList([]string{"-r", f.roadmap}); err != nil {
			t.Fatalf("backlog list: %v", err)
		}
	})
	return decodeTaskIDs(t, "backlog list", out)
}

// backlogShowNextIDs runs `backlog show-next` and returns the ids it printed.
func (f *backlogRouteFixture) backlogShowNextIDs(t *testing.T) map[int]bool {
	t.Helper()
	out := captureStdout(t, func() {
		if err := backlogShowNext([]string{"-r", f.roadmap, "100"}); err != nil {
			t.Fatalf("backlog show-next: %v", err)
		}
	})
	return decodeTaskIDs(t, "backlog show-next", out)
}

func decodeTaskIDs(t *testing.T, label, out string) map[int]bool {
	t.Helper()
	var tasks []struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &tasks); err != nil {
		t.Fatalf("%s printed %q, which is not a task array: %v", label, out, err)
	}
	ids := make(map[int]bool, len(tasks))
	for _, task := range tasks {
		ids[task.ID] = true
	}
	return ids
}

// backlogFamilySummary resolves the `backlog` family through the registry and
// returns its published summary. Resolving through the registry rather than
// reaching into the builder is what makes the gate fail when the family is
// renamed, instead of passing against a copy nothing publishes.
func backlogFamilySummary(t *testing.T) string {
	t.Helper()
	cmd := AppRegistry().FindCommand("backlog")
	if cmd == nil {
		t.Fatal("the backlog family is not registered")
	}
	return cmd.Summary
}

// backlogStatusOnlyMarker is the phrase the summary must carry while the two
// subcommands filter on the status alone. It is a marker, not decoration: the
// gate below rejects a summary that lacks it, so rewording the summary means
// coming back here and re-establishing what the code does.
const backlogStatusOnlyMarker = "status alone"

// backlogNoSprintMarker is the phrase the summary and the `task next` help must
// carry while every task the backlog subcommands return belongs to no sprint,
// which the sprint membership invariant guarantees (SPEC/STATE_MACHINE.md
// § Sprint Membership and the BACKLOG Status).
const backlogNoSprintMarker = "belongs to no sprint"

// backlogFixtureTasks manufactures the three tasks every backlog gate below
// observes: a sprint member in SPRINT, a task that never joined a sprint, and a
// task that left its sprint through `sprint remove-tasks`, the one route back
// to BACKLOG for a member. It also confirms that `task stat <id> BACKLOG`
// refuses the member and leaves it where it was, because under the invariant
// that is how the state the old contract described can no longer be reached.
func backlogFixtureTasks(t *testing.T, f *backlogRouteFixture) (member, loner, departed int) {
	t.Helper()

	member = f.taskInState(t, models.StatusSprint)
	if err := f.tryStat(t, member, models.StatusBacklog); err == nil {
		t.Fatalf("`task stat %d BACKLOG` accepted a sprint member; SPEC/STATE_MACHINE.md refuses it", member)
	}
	if got := f.statusOf(t, member); got != models.StatusSprint || !f.isSprintMember(t, member) {
		t.Fatalf("the refused `task stat %d BACKLOG` changed the task (status %s, member %v)",
			member, got, f.isSprintMember(t, member))
	}

	loner = f.taskInState(t, models.StatusBacklog)

	departed = f.taskInState(t, models.StatusSprint)
	run(t, func() error {
		return sprintRemoveTasks([]string{"-r", f.roadmap, itoa(f.sprintID), itoa(departed)})
	})
	if got := f.statusOf(t, departed); got != models.StatusBacklog || f.isSprintMember(t, departed) {
		t.Fatalf("`sprint remove-tasks` left task #%d in %s (member %v); want BACKLOG outside every sprint",
			departed, got, f.isSprintMember(t, departed))
	}
	return member, loner, departed
}

// TestBacklogSummary_MatchesWhatTheSubcommandsReturn pins the `backlog` family
// summary to what `backlog list` and `backlog show-next` actually return.
//
// Both subcommands build a db.TaskListFilter carrying nothing but Status:
// BACKLOG, and under the sprint membership invariant a BACKLOG task belongs to
// no sprint, so the listing is also the set of tasks outside every sprint. The
// gate observes both halves — what is listed, and whether any listed task is a
// sprint member — before it reads the summary, so it fails whichever side moves.
func TestBacklogSummary_MatchesWhatTheSubcommandsReturn(t *testing.T) {
	f := setupBacklogRouteRoadmap(t, "backlog-summary-contract")
	member, loner, departed := backlogFixtureTasks(t, f)

	listed := f.backlogListIDs(t)
	nextListed := f.backlogShowNextIDs(t)

	for _, id := range []int{loner, departed} {
		if !listed[id] || !nextListed[id] {
			t.Fatalf("the BACKLOG task #%d is missing from the listings (list=%v show-next=%v); the "+
				"observation below would be vacuous", id, listed, nextListed)
		}
	}
	if listed[member] || nextListed[member] {
		t.Fatalf("the SPRINT member #%d is listed as backlog (list=%v show-next=%v); the listings filter "+
			"on BACKLOG status", member, listed[member], nextListed[member])
	}

	// THE OBSERVATION: does any listed task belong to a sprint?
	listsSprintMembers := false
	for id := range listed {
		if f.isSprintMember(t, id) {
			listsSprintMembers = true
		}
	}
	for id := range nextListed {
		if f.isSprintMember(t, id) {
			listsSprintMembers = true
		}
	}

	summary := backlogFamilySummary(t)
	if !strings.Contains(summary, backlogStatusOnlyMarker) {
		t.Errorf("both backlog subcommands filter on the status alone, but the published summary does not "+
			"say so (missing %q): %q", backlogStatusOnlyMarker, summary)
	}
	switch {
	case listsSprintMembers && strings.Contains(summary, backlogNoSprintMarker):
		t.Errorf("a backlog subcommand returned a sprint member, but the published summary claims a "+
			"BACKLOG task %q: %q", backlogNoSprintMarker, summary)
	case !listsSprintMembers && !strings.Contains(summary, backlogNoSprintMarker):
		t.Errorf("no task the backlog subcommands returned is a sprint member, but the published summary "+
			"does not say that a BACKLOG task %q: %q", backlogNoSprintMarker, summary)
	}
}

// TestTaskNextHelp_MatchesWhatBacklogShowNextReturns pins the same fact where
// `task next` help draws the comparison with `backlog show-next`.
func TestTaskNextHelp_MatchesWhatBacklogShowNextReturns(t *testing.T) {
	f := setupBacklogRouteRoadmap(t, "task-next-help-contract")
	backlogFixtureTasks(t, f)

	returnsSprintMembers := false
	returned := f.backlogShowNextIDs(t)
	if len(returned) == 0 {
		t.Fatal("`backlog show-next` returned nothing; the observation below would be vacuous")
	}
	for id := range returned {
		if f.isSprintMember(t, id) {
			returnsSprintMembers = true
		}
	}

	help := captureStdout(t, printTaskNextHelp)
	if !strings.Contains(help, "backlog show-next") {
		t.Fatalf("`task next` help no longer compares itself to `backlog show-next`; this gate pins that "+
			"comparison and can no longer see it:\n%s", help)
	}
	claims := strings.Contains(help, backlogNoSprintMarker)
	if returnsSprintMembers == claims {
		t.Errorf("`backlog show-next` returned a sprint member: %v; the `task next` help claims a BACKLOG "+
			"task %q: %v. The two must disagree in exactly one of them being true:\n%s",
			returnsSprintMembers, backlogNoSprintMarker, claims, help)
	}
}

// ---------------------------------------------------------------------------
// Site 4: the published side effects of `task reopen`
// ---------------------------------------------------------------------------

// reopenSourceStates are the three states `task reopen` changes.
var reopenSourceStates = []models.TaskStatus{
	models.StatusDoing,
	models.StatusTesting,
	models.StatusCompleted,
}

// TestTaskReopenSideEffects_NameTheKeptMembership pins the published
// `side_effects.database` of `task reopen` to the writes it performs.
//
// Under the sprint membership invariant a reopening returns the task to SPRINT
// in its sprint and never touches sprint_tasks (SPEC/COMMANDS.md § Reopen
// Task). The contract used to name a DELETE FROM sprint_tasks, which the command
// no longer runs. The membership and the position are observed per source state
// first, and the published text is read against the observation both ways.
func TestTaskReopenSideEffects_NameTheKeptMembership(t *testing.T) {
	f := setupBacklogRouteRoadmap(t, "reopen-side-effects-contract")

	kept := map[models.TaskStatus]bool{}
	for _, source := range reopenSourceStates {
		id := f.taskInState(t, source)
		if !f.isSprintMember(t, id) {
			t.Fatalf("task #%d walked to %s is not in sprint_tasks; the observation would be vacuous",
				id, source)
		}

		run(t, func() error { return taskReopen([]string{"-r", f.roadmap, itoa(id)}) })

		if got := f.statusOf(t, id); got != models.StatusSprint {
			t.Fatalf("task reopen left task #%d (from %s) in %s, want SPRINT", id, source, got)
		}
		if f.isSprintMember(t, id) {
			kept[source] = true
		}
	}

	text := subcommandSideEffects(t, "task", "reopen")
	if len(kept) == len(reopenSourceStates) {
		if strings.Contains(text, "DELETE FROM sprint_tasks") {
			t.Errorf("`task reopen` kept every reopened task in its sprint, but the published side "+
				"effects still name DELETE FROM sprint_tasks: %q", text)
		}
		if !strings.Contains(text, "sprint_tasks is never touched") {
			t.Errorf("`task reopen` kept every reopened task in its sprint, but the published side "+
				"effects do not say sprint_tasks is never touched: %q", text)
		}
		return
	}
	if strings.Contains(text, "sprint_tasks is never touched") {
		t.Errorf("`task reopen` took a task out of its sprint (kept only from %s), but the published side "+
			"effects say sprint_tasks is never touched: %q", statusSetString(kept), text)
	}
}

// TestTaskReopenSideEffects_NameTheSkipOfAnAlreadyBacklogTask pins the other
// claim the published text makes: a task already in SPRINT or in BACKLOG is
// skipped entirely. The command reports it on stderr and exits 0, so a contract
// promising an UPDATE and an audit entry for every id on the command line would
// be wrong about a call that is legal and idempotent.
func TestTaskReopenSideEffects_NameTheSkipOfAnAlreadyBacklogTask(t *testing.T) {
	f := setupBacklogRouteRoadmap(t, "reopen-skip-contract")

	member := f.taskInState(t, models.StatusSprint)
	loner := f.taskInState(t, models.StatusBacklog)

	for _, id := range []int{member, loner} {
		before := len(auditRecordsFor(t, f.database, id))
		status, wasMember := f.statusOf(t, id), f.isSprintMember(t, id)

		run(t, func() error { return taskReopen([]string{"-r", f.roadmap, itoa(id)}) })

		if got := len(auditRecordsFor(t, f.database, id)); got != before {
			t.Errorf("task reopen wrote %d audit entries for task #%d already in %s; the published side "+
				"effects say it is skipped entirely", got-before, id, status)
		}
		if got := f.statusOf(t, id); got != status || f.isSprintMember(t, id) != wasMember {
			t.Errorf("task reopen changed task #%d already in %s (now %s, member %v)",
				id, status, got, f.isSprintMember(t, id))
		}
	}

	text := subcommandSideEffects(t, "task", "reopen")
	for _, phrase := range []string{"already in SPRINT or in BACKLOG", "skipped"} {
		if !strings.Contains(text, phrase) {
			t.Errorf("`task reopen` skips a task already in SPRINT or in BACKLOG, but the published side "+
				"effects do not say so (missing %q): %q", phrase, text)
		}
	}
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// taskStatusOrder is the state machine's own listing order, used so two status
// sets rendered into a failure message line up for the reader.
var taskStatusOrder = []models.TaskStatus{
	models.StatusBacklog, models.StatusSprint, models.StatusDoing,
	models.StatusTesting, models.StatusCompleted,
}

// statusSetString renders a status set for a failure message.
func statusSetString(set map[models.TaskStatus]bool) string {
	parts := make([]string, 0, len(set))
	for _, s := range taskStatusOrder {
		if set[s] {
			parts = append(parts, string(s))
		}
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, ", ")
}
