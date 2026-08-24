// Package commands — gates that tie what the binary SAYS about the
// SPRINT-to-BACKLOG route to what it DOES (rmp task #232).
//
// SPEC/STATE_MACHINE.md § Sprint Membership and the BACKLOG Status describes a
// state the published contract used to deny existed: a task whose status reads
// BACKLOG while it is still a member of a sprint, reached by
// `task stat <ids> BACKLOG` from the SPRINT source state. Several descriptions
// inside the binary repeated that denial, and each was wrong about code that
// was right. Correcting prose is worthless on its own — prose drifts silently —
// so each corrected sentence is pinned here to the behaviour it describes, by a
// test that first OBSERVES the behaviour and only then reads the sentence.
//
// The observation always comes first, and the assertion is two-way wherever the
// sentence has an opposite. A gate that only checked "the summary contains this
// phrase" would keep passing after someone narrowed `backlog list` to exclude
// sprint members; the branch on the observed value is what makes it fail then.
//
// The doc comment on taskSetStatus is pinned by its own file,
// task_stat_doc_comment_test.go, which reuses the fixture below.
package commands

import (
	"context"
	"encoding/json"
	"regexp"
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

	t.Setenv("HOME", t.TempDir())

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

// backlogExclusionClaims are the ways a summary can claim the opposite — that
// the listing is confined to tasks outside a sprint. Any of them appearing
// while the observed behaviour is inclusive is the exact defect #232 closed.
var backlogExclusionClaims = []string{
	"not yet in a sprint",
	"not in a sprint",
	"outside a sprint",
	"never in a sprint",
}

// TestBacklogSummary_MatchesWhatTheSubcommandsReturn pins the `backlog` family
// summary to what `backlog list` and `backlog show-next` actually return.
//
// The summary used to call the family "a planning view for tasks not yet in a
// sprint". Both subcommands build a db.TaskListFilter carrying nothing but
// Status: BACKLOG, so a task moved to BACKLOG by `task stat <ids> BACKLOG`
// keeps its sprint_tasks row and is listed all the same — and an agent that
// believed the summary would read the listing as a sprint-membership query and
// plan the next sprint on a set that is not the one it thought.
func TestBacklogSummary_MatchesWhatTheSubcommandsReturn(t *testing.T) {
	f := setupBacklogRouteRoadmap(t, "backlog-summary-contract")

	// A task that reaches BACKLOG the way the SPEC describes: from SPRINT, by
	// `task stat`, which never touches sprint_tasks.
	member := f.taskInState(t, models.StatusSprint)
	f.mustStat(t, member, models.StatusBacklog)

	// A task that was never in a sprint, so the listing has something to return
	// either way and an empty result cannot be mistaken for agreement.
	loner := f.taskInState(t, models.StatusBacklog)

	if got := f.statusOf(t, member); got != models.StatusBacklog {
		t.Fatalf("task #%d should read BACKLOG after `task stat`, reads %s", member, got)
	}
	if !f.isSprintMember(t, member) {
		t.Fatal("`task stat <id> BACKLOG` detached the task from its sprint; " +
			"SPEC/STATE_MACHINE.md § Sprint Membership and the BACKLOG Status says the row survives, " +
			"and every claim in this file is built on that")
	}

	listed := f.backlogListIDs(t)
	nextListed := f.backlogShowNextIDs(t)

	if !listed[loner] || !nextListed[loner] {
		t.Fatalf("the never-in-a-sprint task #%d is missing from the listings "+
			"(list=%v show-next=%v); the observation below would be vacuous", loner, listed, nextListed)
	}
	if listed[member] != nextListed[member] {
		t.Fatalf("`backlog list` and `backlog show-next` disagree about the sprint-member BACKLOG task "+
			"#%d (list=%v show-next=%v); one summary describes both, so it cannot be true of one and "+
			"false of the other", member, listed[member], nextListed[member])
	}

	// THE OBSERVATION. Everything asserted about the published text branches on
	// it, so the gate fails whichever side of the pair moves.
	listsSprintMembers := listed[member]

	summary := backlogFamilySummary(t)

	if listsSprintMembers {
		if !strings.Contains(summary, backlogStatusOnlyMarker) {
			t.Errorf("both backlog subcommands returned the sprint-member BACKLOG task #%d, but the "+
				"published summary does not say the filter is the %q: %q",
				member, backlogStatusOnlyMarker, summary)
		}
		for _, claim := range backlogExclusionClaims {
			if strings.Contains(strings.ToLower(summary), claim) {
				t.Errorf("the published summary claims %q, but both backlog subcommands returned the "+
					"sprint-member BACKLOG task #%d: %q", claim, member, summary)
			}
		}
		return
	}

	// The other direction. If the filter ever narrows to non-members, the
	// corrected summary becomes the false one and this branch is what says so.
	if strings.Contains(summary, backlogStatusOnlyMarker) {
		t.Errorf("the backlog subcommands excluded the sprint-member BACKLOG task #%d, but the published "+
			"summary still says the filter is the %q: %q", member, backlogStatusOnlyMarker, summary)
	}
}

// TestTaskNextHelp_MatchesWhatBacklogShowNextReturns pins the same fact where
// `task next` help draws the comparison with `backlog show-next`. The line
// there read "operates on BACKLOG only (not yet in a sprint)" — the same false
// exclusion as the registry summary, in the text a human reads at the terminal.
func TestTaskNextHelp_MatchesWhatBacklogShowNextReturns(t *testing.T) {
	f := setupBacklogRouteRoadmap(t, "task-next-help-contract")

	member := f.taskInState(t, models.StatusSprint)
	f.mustStat(t, member, models.StatusBacklog)
	if !f.isSprintMember(t, member) {
		t.Fatal("`task stat <id> BACKLOG` detached the task; the observation below would be vacuous")
	}

	returnsSprintMembers := f.backlogShowNextIDs(t)[member]

	help := captureStdout(t, printTaskNextHelp)
	if !strings.Contains(help, "backlog show-next") {
		t.Fatalf("`task next` help no longer compares itself to `backlog show-next`; this gate pins that "+
			"comparison and can no longer see it:\n%s", help)
	}

	for _, claim := range backlogExclusionClaims {
		mentions := strings.Contains(strings.ToLower(help), claim)
		if returnsSprintMembers && mentions {
			t.Errorf("`task next` help claims backlog show-next is %q, but it returned the sprint-member "+
				"BACKLOG task #%d", claim, member)
		}
	}
}

// ---------------------------------------------------------------------------
// Site 4: the published side effects of `task reopen`
// ---------------------------------------------------------------------------

// reopenSourceStates are the four states `task reopen` accepts as a source.
var reopenSourceStates = []models.TaskStatus{
	models.StatusSprint,
	models.StatusDoing,
	models.StatusTesting,
	models.StatusCompleted,
}

// statusToken matches a task-status word inside published prose.
var statusToken = regexp.MustCompile(`\b(BACKLOG|SPRINT|DOING|TESTING|COMPLETED)\b`)

// sentenceSplit breaks published prose at a full stop or a semicolon. The
// semicolon matters: a clause joined that way carries its own status words, and
// folding it into its neighbour would let one sentence's states be read as
// another's.
var sentenceSplit = regexp.MustCompile(`(?:\.\s+|;\s+)`)

// sentencesNaming returns every sentence of a text that contains needle.
func sentencesNaming(text, needle string) []string {
	out := make([]string, 0, 2)
	for _, part := range sentenceSplit.Split(text, -1) {
		if strings.Contains(part, needle) {
			out = append(out, part)
		}
	}
	return out
}

// TestTaskReopenSideEffects_NameTheSprintTasksDeletion pins the published
// `side_effects.database` of `task reopen` to the writes it performs.
//
// The contract used to read "UPDATE tasks + audit log per task; one
// transaction", which omits the DELETE FROM sprint_tasks the command runs for
// every task whose source state is SPRINT, DOING or TESTING. An agent reading
// that contract would expect a reopened task to stay in its sprint — true only
// from the COMPLETED source state, which is precisely the distinction the old
// text erased.
//
// The deletion is observed per source state first, and the two state sets are
// derived from the observation, so a change to either side breaks the gate.
func TestTaskReopenSideEffects_NameTheSprintTasksDeletion(t *testing.T) {
	f := setupBacklogRouteRoadmap(t, "reopen-side-effects-contract")

	detaching := map[models.TaskStatus]bool{}
	keeping := map[models.TaskStatus]bool{}

	for _, source := range reopenSourceStates {
		id := f.taskInState(t, source)
		if !f.isSprintMember(t, id) {
			t.Fatalf("task #%d walked to %s is not in sprint_tasks; the observation would be vacuous",
				id, source)
		}

		run(t, func() error { return taskReopen([]string{"-r", f.roadmap, itoa(id)}) })

		if got := f.statusOf(t, id); got != models.StatusBacklog {
			t.Fatalf("task reopen left task #%d in %s", id, got)
		}
		if f.isSprintMember(t, id) {
			keeping[source] = true
		} else {
			detaching[source] = true
		}
	}

	if len(detaching) == 0 {
		t.Fatal("`task reopen` detached nothing from any source state; either the fixture never produced " +
			"a member or the command stopped writing sprint_tasks entirely")
	}

	text := subcommandSideEffects(t, "task", "reopen")

	if !strings.Contains(text, "sprint_tasks") {
		t.Fatalf("`task reopen` removes the sprint_tasks row from %s, but the published side effects "+
			"never name the table: %q", statusSetString(detaching), text)
	}

	// The sentence that names the DELETE must name exactly the source states
	// observed to lose the row — no more, no fewer.
	deleteSentences := sentencesNaming(text, "DELETE FROM sprint_tasks")
	if len(deleteSentences) != 1 {
		t.Fatalf("the published side effects should name DELETE FROM sprint_tasks in exactly one "+
			"sentence, found %d: %q", len(deleteSentences), text)
	}
	declared := map[models.TaskStatus]bool{}
	for _, m := range statusToken.FindAllString(deleteSentences[0], -1) {
		if status := models.TaskStatus(m); status != models.StatusBacklog {
			// BACKLOG is the destination of every reopening, never a source.
			declared[status] = true
		}
	}
	if !sameStatusSet(declared, detaching) {
		t.Errorf("the published side effects say DELETE FROM sprint_tasks runs for %s, but it was "+
			"observed to run for %s.\n  sentence: %q",
			statusSetString(declared), statusSetString(detaching), deleteSentences[0])
	}

	// And every state whose row survives must be named as surviving, or an
	// agent reading the corrected text learns half a rule.
	for _, source := range reopenSourceStates {
		if !keeping[source] {
			continue
		}
		found := false
		for _, sentence := range sentencesNaming(text, "sprint_tasks") {
			if sentence != deleteSentences[0] && strings.Contains(sentence, string(source)) {
				found = true
			}
		}
		if !found {
			t.Errorf("a task reopened from %s keeps its sprint_tasks row, but no other sentence of the "+
				"published side effects says so: %q", source, text)
		}
	}
}

// TestTaskReopenSideEffects_NameTheSkipOfAnAlreadyBacklogTask pins the other
// claim the corrected text makes: a task already in BACKLOG is skipped
// entirely, membership included. The command reports it on stderr and exits 0,
// so a contract promising an UPDATE and an audit entry for every id on the
// command line would be wrong about a call that is legal and idempotent.
func TestTaskReopenSideEffects_NameTheSkipOfAnAlreadyBacklogTask(t *testing.T) {
	f := setupBacklogRouteRoadmap(t, "reopen-skip-contract")

	// A BACKLOG task that IS a member: reached from SPRINT by `task stat`.
	member := f.taskInState(t, models.StatusSprint)
	f.mustStat(t, member, models.StatusBacklog)
	if !f.isSprintMember(t, member) {
		t.Fatal("`task stat <id> BACKLOG` detached the task; the skip below would prove nothing")
	}

	before := len(auditRecordsFor(t, f.database, member))

	run(t, func() error { return taskReopen([]string{"-r", f.roadmap, itoa(member)}) })

	if got := len(auditRecordsFor(t, f.database, member)); got != before {
		t.Errorf("task reopen wrote %d audit entries for an already-BACKLOG task; the published side "+
			"effects say it is skipped entirely", got-before)
	}
	if !f.isSprintMember(t, member) {
		t.Error("task reopen detached an already-BACKLOG task from its sprint; the published side " +
			"effects say its membership is untouched")
	}

	text := subcommandSideEffects(t, "task", "reopen")
	for _, phrase := range []string{"already in BACKLOG", "skipped"} {
		if !strings.Contains(text, phrase) {
			t.Errorf("`task reopen` skips an already-BACKLOG task, but the published side effects do "+
				"not say so (missing %q): %q", phrase, text)
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

func sameStatusSet(a, b map[models.TaskStatus]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
