// Package commands — regression tests for the two commit-tracking fields of the
// task state machine, `commit_open` and `commit_close` (rmp task #254).
//
// The feature has three properties that are easy to implement almost right, so
// each one is pinned separately here:
//
//  1. **The flags are mandatory exactly where the SPEC says, and rejected
//     everywhere else.** A rejection must be total: SPEC/COMMANDS.md § Change
//     Status (stat) places these checks at step 4 of a normative validation
//     order, before the database is even opened, so a multi-ID invocation whose
//     other IDs were perfectly valid must come out untouched. Every rejection
//     test below therefore snapshots the whole batch before the call and
//     compares it field by field afterwards — asserting the error alone would
//     pass against an implementation that rejects *after* writing.
//
//  2. **The clearing on a return to BACKLOG is asymmetric.** All four routes
//     back to BACKLOG — `task stat <ids> BACKLOG`, `task reopen`,
//     `sprint remove-tasks` and `sprint remove` — clear `commit_close` and
//     *preserve* `commit_open`, which is exactly where the lifecycle timestamps
//     and this pair part company (SPEC/STATE_MACHINE.md § Commit Tracking
//     Fields, rules 4 and 5). Each of the four is asserted on both fields at
//     once: an implementation that cleared both, or neither, would satisfy half
//     of any weaker assertion.
//
//  3. **What lands in the column is the normalised value.** The caller may type
//     the hash in any case; the stored form is lowercase.
//
// The exit codes these errors map to, and the verbatim stderr line each one
// renders, are pinned separately in cmd/rmp/commit_tracking_exit_test.go, where
// the real error-to-exit-code mapping lives.
package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// Commit hashes used throughout. They are real short hashes from this
// repository's history, so the fixtures read like a roadmap someone actually
// worked through rather than like filler.
const (
	commitWorkStarted   = "5f93b51" // the commit a task's work starts from
	commitWorkResumed   = "2578d18" // the commit a re-entry into DOING starts from
	commitWorkConcluded = "391cff7" // the commit a task is concluded at
)

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

// commitTrackingFixture is a roadmap holding one started sprint and four member
// tasks, all sitting in SPRINT status and ready to be transitioned.
type commitTrackingFixture struct {
	roadmap  string
	database *db.DB
	sprintID int
	taskIDs  []int
}

// setupCommitTrackingRoadmap builds the fixture through the real commands, so
// every state the tests meet is a state the CLI can actually produce.
func setupCommitTrackingRoadmap(t *testing.T, name string) *commitTrackingFixture {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	database, cleanup := setupTestTaskRoadmap(t, name)
	t.Cleanup(cleanup)

	fixture := &commitTrackingFixture{roadmap: name, database: database}

	seedTasks := []struct{ title, functional, technical, acceptance string }{
		{
			"Rotate the JWT signing key without downtime",
			"Tokens signed with the previous key keep verifying during the rotation window.",
			"Publish both keys in the JWKS document and retire the old one after the window closes.",
			"A token minted before the rotation still verifies one hour after it.",
		},
		{
			"Move session tokens to the encrypted store",
			"Session tokens are never written in clear text.",
			"Route every write through the compliance store and migrate the existing rows.",
			"No clear-text token remains in the sessions table after the migration.",
		},
		{
			"Rate-limit the password reset endpoint",
			"A single address cannot request more than five resets an hour.",
			"Token bucket keyed on the account, backed by the shared counter store.",
			"The sixth request inside the hour is answered with 429.",
		},
		{
			"Record the audit row inside the mutation transaction",
			"A rolled-back mutation leaves no audit row behind.",
			"Move the audit insert inside the same transaction as the write it describes.",
			"A forced rollback in the middle of a batch leaves the audit table untouched.",
		},
	}
	for _, s := range seedTasks {
		_ = captureStdout(t, func() {
			if err := taskCreate([]string{
				"-r", name, "-t", s.title, "-fr", s.functional, "-tr", s.technical, "-ac", s.acceptance,
			}); err != nil {
				t.Fatalf("seeding task %q: %v", s.title, err)
			}
		})
	}

	tasks, err := database.ListTasks(context.Background(), nil)
	if err != nil {
		t.Fatalf("reading the seeded tasks back: %v", err)
	}
	if len(tasks) != len(seedTasks) {
		t.Fatalf("seeded %d tasks, found %d", len(seedTasks), len(tasks))
	}
	for i := range tasks {
		fixture.taskIDs = append(fixture.taskIDs, tasks[i].ID)
	}

	_ = captureStdout(t, func() {
		if err := sprintCreate([]string{
			"-r", name, "-t", "Authentication hardening",
			"-d", "Close the key-rotation and session-storage findings raised by the audit.",
		}); err != nil {
			t.Fatalf("seeding the sprint: %v", err)
		}
	})
	sprints, err := database.ListSprints(context.Background(), nil)
	if err != nil {
		t.Fatalf("reading the seeded sprint back: %v", err)
	}
	if len(sprints) != 1 {
		t.Fatalf("seeded 1 sprint, found %d", len(sprints))
	}
	fixture.sprintID = sprints[0].ID

	_ = captureStdout(t, func() {
		if err := sprintAddTasks([]string{
			"-r", name, itoa(fixture.sprintID), fixture.idCSV(fixture.taskIDs...),
		}); err != nil {
			t.Fatalf("adding the tasks to the sprint: %v", err)
		}
	})
	_ = captureStdout(t, func() {
		if err := sprintStart([]string{"-r", name, itoa(fixture.sprintID)}); err != nil {
			t.Fatalf("starting the sprint: %v", err)
		}
	})

	return fixture
}

// idCSV renders task IDs the way the CLI takes them.
func (f *commitTrackingFixture) idCSV(ids ...int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = itoa(id)
	}
	return strings.Join(parts, ",")
}

// stat runs `task stat` and fails the test if it returns an error. Extra
// arguments are appended after the target state, which is where the flags go.
func (f *commitTrackingFixture) stat(t *testing.T, ids string, state string, extra ...string) {
	t.Helper()

	args := append([]string{"-r", f.roadmap, ids, state}, extra...)
	out := captureStdout(t, func() {
		if err := taskSetStatus(args); err != nil {
			t.Fatalf("task stat %v: %v", args, err)
		}
	})
	if out != "" {
		t.Errorf("task stat %v printed %q; SPEC/COMMANDS.md gives it an empty success output", args, out)
	}
}

// statErr runs `task stat` expecting a rejection, and returns the error.
func (f *commitTrackingFixture) statErr(t *testing.T, ids string, state string, extra ...string) error {
	t.Helper()

	args := append([]string{"-r", f.roadmap, ids, state}, extra...)
	var err error
	_ = captureStdout(t, func() { err = taskSetStatus(args) })
	if err == nil {
		t.Fatalf("task stat %v was accepted; the SPEC rejects it", args)
	}
	return err
}

// walkToCompleted takes one task the whole way through the lifecycle, so it ends
// COMPLETED with both commit columns and every lifecycle timestamp populated.
// That is the only source state from which the asymmetric clearing is
// observable on both fields at once.
func (f *commitTrackingFixture) walkToCompleted(t *testing.T, id int) {
	t.Helper()

	f.stat(t, itoa(id), "DOING", "--commit-open", commitWorkStarted)
	f.stat(t, itoa(id), "TESTING")
	f.stat(t, itoa(id), "COMPLETED", "--commit-close", commitWorkConcluded,
		"--summary", "Both keys are published and the old one retires on schedule.")
}

// ---------------------------------------------------------------------------
// Snapshots
// ---------------------------------------------------------------------------

// taskSnapshot is every field a status transition may touch. Rejection tests
// compare the whole struct, so a write to any of them fails the assertion — not
// only a write to the two fields the test is nominally about.
type taskSnapshot struct {
	Status            string
	StartedAt         string
	TestedAt          string
	ClosedAt          string
	CompletionSummary string
	CommitOpen        string
	CommitClose       string
}

// nullable renders a nullable column so that SQL NULL is distinguishable from
// the empty string: without this an implementation that wrote "" where the SPEC
// wants NULL would compare equal to one that wrote nothing.
func nullable(v *string) string {
	if v == nil {
		return "<NULL>"
	}
	return "«" + *v + "»"
}

// snapshot reads the given tasks straight out of the database. It deliberately
// does not go through the command layer: the point is to see what was stored,
// not what a command says it stored.
func (f *commitTrackingFixture) snapshot(t *testing.T, ids ...int) map[int]taskSnapshot {
	t.Helper()

	tasks, err := f.database.GetTasks(context.Background(), ids)
	if err != nil {
		t.Fatalf("reading tasks %v: %v", ids, err)
	}
	if len(tasks) != len(ids) {
		t.Fatalf("asked for %d tasks, got %d", len(ids), len(tasks))
	}

	out := make(map[int]taskSnapshot, len(tasks))
	for i := range tasks {
		out[tasks[i].ID] = taskSnapshot{
			Status:            string(tasks[i].Status),
			StartedAt:         nullable(tasks[i].StartedAt),
			TestedAt:          nullable(tasks[i].TestedAt),
			ClosedAt:          nullable(tasks[i].ClosedAt),
			CompletionSummary: nullable(tasks[i].CompletionSummary),
			CommitOpen:        nullable(tasks[i].CommitOpen),
			CommitClose:       nullable(tasks[i].CommitClose),
		}
	}
	return out
}

// assertUnchanged fails with a field-level diff when a rejected command turns
// out to have written something.
func assertUnchanged(t *testing.T, before, after map[int]taskSnapshot, what string) {
	t.Helper()

	for id, want := range before {
		got, ok := after[id]
		if !ok {
			t.Errorf("%s: task #%d disappeared", what, id)
			continue
		}
		if got != want {
			t.Errorf("%s: task #%d changed\n  before: %+v\n  after:  %+v\n"+
				"a rejected command must make no change at all "+
				"(SPEC/COMMANDS.md § Change Status (stat), Validation Order)",
				what, id, want, got)
		}
	}
}

// commitValues returns the two commit columns of one task, already dereferenced,
// so an assertion reads plainly.
func (f *commitTrackingFixture) commitValues(t *testing.T, id int) (open, closed string) {
	t.Helper()

	snap := f.snapshot(t, id)[id]
	return snap.CommitOpen, snap.CommitClose
}

// assertRejected checks that an error is a validation rejection (exit code 6)
// carrying exactly the message the SPEC's error table prescribes.
func assertRejected(t *testing.T, err error, wantMessage string) {
	t.Helper()

	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf("error %v does not chain utils.ErrValidation, so it would not map to exit code 6", err)
	}
	if errors.Is(err, utils.ErrRequired) {
		t.Errorf("error %v chains utils.ErrRequired, which maps to exit code 2; the SPEC gives this "+
			"case exit code 6", err)
	}
	if got := err.Error(); got != wantMessage {
		t.Errorf("message = %q, want %q verbatim (SPEC/COMMANDS.md § Change Status (stat), error table)",
			got, wantMessage)
	}
}

// ---------------------------------------------------------------------------
// The flags are mandatory where the SPEC says
// ---------------------------------------------------------------------------

// TestTaskStat_DoingWithoutCommitOpenIsRejected covers the entry into DOING with
// the flag absent, on both routes into that state: the first entry from SPRINT
// and the re-entry from TESTING. Both are listed in SPEC/STATE_MACHINE.md
// § Mandatory Values on Entry into DOING and COMPLETED, and it would be easy to
// enforce only the first.
func TestTaskStat_DoingWithoutCommitOpenIsRejected(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-doing-required")

	fromSprint := f.taskIDs[0]
	fromTesting := f.taskIDs[1]

	// Put the second task in TESTING so the re-entry route is exercised too.
	f.stat(t, itoa(fromTesting), "DOING", "--commit-open", commitWorkStarted)
	f.stat(t, itoa(fromTesting), "TESTING")

	for _, tc := range []struct {
		name string
		id   int
	}{
		{"SPRINT to DOING", fromSprint},
		{"TESTING to DOING", fromTesting},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := f.snapshot(t, tc.id)
			err := f.statErr(t, itoa(tc.id), "DOING")
			assertRejected(t, err, "--commit-open is required when transitioning to DOING")
			assertUnchanged(t, before, f.snapshot(t, tc.id), "DOING without --commit-open")
		})
	}
}

// TestTaskStat_CompletedWithoutCommitCloseIsRejected covers the only transition
// into COMPLETED with the flag absent, both with and without the optional
// --summary: a summary must not make the command look complete enough to run.
func TestTaskStat_CompletedWithoutCommitCloseIsRejected(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-completed-required")

	id := f.taskIDs[0]
	f.stat(t, itoa(id), "DOING", "--commit-open", commitWorkStarted)
	f.stat(t, itoa(id), "TESTING")

	for _, tc := range []struct {
		name  string
		extra []string
	}{
		{"bare", nil},
		{"with a summary", []string{"--summary", "Rotation completed inside the window."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := f.snapshot(t, id)
			err := f.statErr(t, itoa(id), "COMPLETED", tc.extra...)
			assertRejected(t, err, "--commit-close is required when transitioning to COMPLETED")
			assertUnchanged(t, before, f.snapshot(t, id), "COMPLETED without --commit-close")
		})
	}
}

// TestTaskStat_CommitFlagOnTheWrongTargetStateIsRejected walks each flag across
// every target state `task stat` accepts other than its own. A hash that is
// perfectly well formed is used throughout, so what is under test is the
// target-state rule alone and not the format check behind it.
func TestTaskStat_CommitFlagOnTheWrongTargetStateIsRejected(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-wrong-target")

	// One task in SPRINT (accepts BACKLOG and DOING), one in DOING (accepts
	// TESTING), one in TESTING (accepts DOING and COMPLETED). Between them the
	// three cover every target state a commit flag can be wrongly paired with.
	inSprint, inDoing, inTesting := f.taskIDs[0], f.taskIDs[1], f.taskIDs[2]
	f.stat(t, itoa(inDoing), "DOING", "--commit-open", commitWorkStarted)
	f.stat(t, itoa(inTesting), "DOING", "--commit-open", commitWorkStarted)
	f.stat(t, itoa(inTesting), "TESTING")

	const (
		openMessage  = "--commit-open flag is only allowed when transitioning to DOING"
		closeMessage = "--commit-close flag is only allowed when transitioning to COMPLETED"
	)

	for _, tc := range []struct {
		name    string
		flag    string
		target  string
		message string
		id      int
	}{
		{"--commit-open on BACKLOG", "--commit-open", "BACKLOG", openMessage, inSprint},
		{"--commit-open on TESTING", "--commit-open", "TESTING", openMessage, inDoing},
		{"--commit-open on COMPLETED", "--commit-open", "COMPLETED", openMessage, inTesting},
		{"-co on TESTING", "-co", "TESTING", openMessage, inDoing},
		{"--commit-close on BACKLOG", "--commit-close", "BACKLOG", closeMessage, inSprint},
		{"--commit-close on DOING", "--commit-close", "DOING", closeMessage, inSprint},
		{"--commit-close on TESTING", "--commit-close", "TESTING", closeMessage, inDoing},
		{"-cc on DOING", "-cc", "DOING", closeMessage, inSprint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := f.snapshot(t, tc.id)
			err := f.statErr(t, itoa(tc.id), tc.target, tc.flag, commitWorkConcluded)
			assertRejected(t, err, tc.message)
			assertUnchanged(t, before, f.snapshot(t, tc.id), tc.name)
		})
	}
}

// TestTaskStat_RejectionPrecedesTheRequirementCheck pins sub-step order inside
// step 4. `task stat <id> DOING --commit-close <hash>` breaks two rules at once:
// --commit-close is on the wrong target state, and --commit-open is missing. The
// SPEC numbers the four checks, and the "wrong target state" pair comes first,
// so the caller is told about the flag they actually wrote rather than about one
// they did not.
func TestTaskStat_RejectionPrecedesTheRequirementCheck(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-substep-order")

	id := f.taskIDs[0]
	before := f.snapshot(t, id)
	err := f.statErr(t, itoa(id), "DOING", "--commit-close", commitWorkConcluded)
	assertRejected(t, err, "--commit-close flag is only allowed when transitioning to COMPLETED")
	assertUnchanged(t, before, f.snapshot(t, id), "DOING with --commit-close and no --commit-open")
}

// ---------------------------------------------------------------------------
// Format validation
// ---------------------------------------------------------------------------

// TestTaskStat_MalformedCommitHashIsRejected covers both ends of the length
// range and the character class, on both flags. The 6- and 65-character cases
// sit one step outside the accepted 7..64 window, which is where an off-by-one
// in the bound would show.
func TestTaskStat_MalformedCommitHashIsRejected(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-malformed-hash")

	inSprint, inTesting := f.taskIDs[0], f.taskIDs[1]
	f.stat(t, itoa(inTesting), "DOING", "--commit-open", commitWorkStarted)
	f.stat(t, itoa(inTesting), "TESTING")

	sixChars := strings.Repeat("a", 6)
	sixtyFiveChars := strings.Repeat("a", 65)

	for _, tc := range []struct {
		name  string
		value string
		why   string
	}{
		{"six characters", sixChars, "one short of the 7-character minimum"},
		{"sixty-five characters", sixtyFiveChars, "one past the 64-character maximum"},
		{"empty", "", "no value at all was supplied between the quotes"},
		{"non-hexadecimal", "zzzzzzz", "z is not a hexadecimal digit"},
		{"a git ref rather than a hash", "refs/heads/main", "a ref name is not a hash"},
		{"leading whitespace", " 5f93b51", "the value is taken verbatim and never trimmed"},
		{"trailing newline", "5f93b51\n", "a stray newline must not be silently absorbed"},
		// A token that looks like another flag is still a token, so the SPEC's
		// "written with no value after it" row does not apply to it: it lands on
		// the malformed-hash row instead. No commit hash can begin with '-', so
		// either way the command is refused; only the exit code differs, and the
		// SPEC's wording decides it.
		{"another flag used as the value", "--summary", "a flag name is not a hash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, flag := range []struct {
				name   string
				target string
				id     int
			}{
				{"--commit-open", "DOING", inSprint},
				{"--commit-close", "COMPLETED", inTesting},
			} {
				before := f.snapshot(t, flag.id)
				err := f.statErr(t, itoa(flag.id), flag.target, flag.name, tc.value)
				want := "invalid commit hash for " + flag.name + ": " +
					quoteForMessage(tc.value) + " (expected 7 to 64 hexadecimal characters)"
				assertRejected(t, err, want)
				if !errors.Is(err, models.ErrInvalidCommitHash) {
					t.Errorf("%s %q (%s): error %v does not chain models.ErrInvalidCommitHash, so the "+
						"command layer is no longer validating through models.NormalizeCommitHash",
						flag.name, tc.value, tc.why, err)
				}
				assertUnchanged(t, before, f.snapshot(t, flag.id), flag.name+" with a malformed hash")
			}
		})
	}
}

// quoteForMessage renders a rejected value the way the error message does. The
// message quotes with %q on purpose: a hash carrying control characters must not
// reach the terminal raw, so the expectation is built the same way rather than
// with a hand-written pair of quotes.
func quoteForMessage(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteString("\"")
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\t':
			b.WriteString("\\t")
		case '\r':
			b.WriteString("\\r")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("\"")
	return b.String()
}

// TestTaskStat_CommitFlagWithNoValueIsMisuse pins the one commit-flag case the
// SPEC gives exit code 2 rather than 6. A flag written with nothing after it is
// a malformed command line; an absent flag is a rejected transition. Conflating
// the two would make a typo indistinguishable from a policy violation, so the
// test asserts the sentinel in both directions.
func TestTaskStat_CommitFlagWithNoValueIsMisuse(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-flag-no-value")

	inSprint, inTesting := f.taskIDs[0], f.taskIDs[1]
	f.stat(t, itoa(inTesting), "DOING", "--commit-open", commitWorkStarted)
	f.stat(t, itoa(inTesting), "TESTING")

	for _, tc := range []struct {
		name    string
		flag    string
		target  string
		message string
		id      int
	}{
		{"--commit-open", "--commit-open", "DOING", "--commit-open requires a value", inSprint},
		{"-co", "-co", "DOING", "--commit-open requires a value", inSprint},
		{"--commit-close", "--commit-close", "COMPLETED", "--commit-close requires a value", inTesting},
		{"-cc", "-cc", "COMPLETED", "--commit-close requires a value", inTesting},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := f.snapshot(t, tc.id)
			err := f.statErr(t, itoa(tc.id), tc.target, tc.flag)

			if !errors.Is(err, utils.ErrRequired) {
				t.Errorf("error %v does not chain utils.ErrRequired, so it would not map to exit code 2", err)
			}
			if errors.Is(err, utils.ErrValidation) {
				t.Errorf("error %v chains utils.ErrValidation, which maps to exit code 6; the SPEC keeps "+
					"the no-value case distinct from the absent-flag case at exit code 2", err)
			}
			if got := err.Error(); got != tc.message {
				t.Errorf("message = %q, want %q verbatim (SPEC/COMMANDS.md § Change Status (stat), "+
					"error table)", got, tc.message)
			}
			// The short form is reported under the long name: the SPEC's error
			// table gives one message per flag, not one per spelling.
			assertUnchanged(t, before, f.snapshot(t, tc.id), tc.flag+" with no value")
		})
	}
}

// ---------------------------------------------------------------------------
// What gets written
// ---------------------------------------------------------------------------

// TestTaskStat_CommitHashIsStoredLowercase asserts the normalisation reaches the
// column. The value returned by models.NormalizeCommitHash is what must be
// written; an implementation that validated the normalised form but stored the
// caller's original would pass every rejection test above.
func TestTaskStat_CommitHashIsStoredLowercase(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-lowercased")

	id := f.taskIDs[0]
	const (
		typedOpen  = "5F93B51ABCDEF"
		typedClose = "391CFF7"
	)

	f.stat(t, itoa(id), "DOING", "--commit-open", typedOpen)
	open, _ := f.commitValues(t, id)
	if open != "«5f93b51abcdef»" {
		t.Errorf("commit_open = %s after --commit-open %s, want the lowercased form; "+
			"SPEC/MODELS.md § Task stores the hash lowercase", open, typedOpen)
	}

	f.stat(t, itoa(id), "TESTING")
	f.stat(t, itoa(id), "COMPLETED", "--commit-close", typedClose)
	_, closed := f.commitValues(t, id)
	if closed != "«391cff7»" {
		t.Errorf("commit_close = %s after --commit-close %s, want the lowercased form", closed, typedClose)
	}
}

// TestTaskStat_ReEntryIntoDoingReplacesCommitOpen pins the replacement rule.
// TESTING → DOING is the one transition that writes a column that already holds
// a value, and the SPEC keeps no history: the new value replaces the old one.
func TestTaskStat_ReEntryIntoDoingReplacesCommitOpen(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-reentry-replaces")

	id := f.taskIDs[0]
	f.stat(t, itoa(id), "DOING", "--commit-open", commitWorkStarted)
	f.stat(t, itoa(id), "TESTING")

	open, _ := f.commitValues(t, id)
	if open != "«"+commitWorkStarted+"»" {
		t.Fatalf("commit_open = %s after the first entry into DOING, want «%s»; the re-entry assertion "+
			"below would prove nothing", open, commitWorkStarted)
	}

	f.stat(t, itoa(id), "DOING", "--commit-open", commitWorkResumed)

	open, closed := f.commitValues(t, id)
	if open != "«"+commitWorkResumed+"»" {
		t.Errorf("commit_open = %s after TESTING → DOING, want «%s»; the re-entry must replace the "+
			"value stored on the previous entry (SPEC/STATE_MACHINE.md § Commit Tracking Fields, rule 2)",
			open, commitWorkResumed)
	}
	if closed != "<NULL>" {
		t.Errorf("commit_close = %s after TESTING → DOING, want NULL; that transition writes no "+
			"commit_close", closed)
	}
}

// TestTaskStat_CompletedWritesCommitCloseAndKeepsCommitOpen asserts the pair
// after the only transition into COMPLETED: commit_close takes the supplied
// value, and commit_open is left exactly as the entry into DOING wrote it.
func TestTaskStat_CompletedWritesCommitCloseAndKeepsCommitOpen(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-completed-writes")

	id := f.taskIDs[0]
	f.walkToCompleted(t, id)

	open, closed := f.commitValues(t, id)
	if open != "«"+commitWorkStarted+"»" {
		t.Errorf("commit_open = %s after the transition into COMPLETED, want «%s» untouched",
			open, commitWorkStarted)
	}
	if closed != "«"+commitWorkConcluded+"»" {
		t.Errorf("commit_close = %s after the transition into COMPLETED, want «%s»",
			closed, commitWorkConcluded)
	}
}

// TestTaskStat_AuditRecordsTheTransitionOnlyWhenItHappens checks that the audit
// trail agrees with the columns. A commit-writing transition records the same
// TASK_STATUS_CHANGE operation as any other status change — the feature invents
// no operation of its own — and a rejected transition records nothing, which is
// the audit-side proof that the rejection really did precede every write.
func TestTaskStat_AuditRecordsTheTransitionOnlyWhenItHappens(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-audit-trail")

	id := f.taskIDs[0]

	before := f.auditOperations(t, id)
	_ = f.statErr(t, itoa(id), "DOING")
	if afterRejection := f.auditOperations(t, id); len(afterRejection) != len(before) {
		t.Errorf("a rejected `task stat DOING` wrote %d audit entries (%v → %v); a command rejected in "+
			"validation must not touch the database at all",
			len(afterRejection)-len(before), before, afterRejection)
	}

	f.stat(t, itoa(id), "DOING", "--commit-open", commitWorkStarted)

	after := f.auditOperations(t, id)
	if len(after) != len(before)+1 {
		t.Fatalf("audit operations after an accepted transition = %v, want exactly one more entry than %v",
			after, before)
	}
	gained := countOperation(after, string(models.OpTaskStatusChange)) -
		countOperation(before, string(models.OpTaskStatusChange))
	if gained != 1 {
		t.Errorf("the accepted transition added %d %s entries (%v → %v), want exactly 1; the commit "+
			"fields are recorded by the ordinary status-change entry, not by an operation of their own",
			gained, models.OpTaskStatusChange, before, after)
	}
}

// auditOperations returns every audit operation recorded against one task, in
// the order the audit log reports them.
func (f *commitTrackingFixture) auditOperations(t *testing.T, taskID int) []string {
	t.Helper()

	entries, err := f.database.GetEntityHistory(context.Background(), string(models.EntityTask), taskID)
	if err != nil {
		t.Fatalf("reading the audit history of task %d: %v", taskID, err)
	}
	ops := make([]string, 0, len(entries))
	for i := range entries {
		ops = append(ops, entries[i].Operation)
	}
	return ops
}

// countOperation counts how many times one operation appears in an audit trail.
// Counting rather than indexing keeps the assertion independent of the order
// GetEntityHistory reports entries in.
func countOperation(ops []string, want string) int {
	n := 0
	for _, op := range ops {
		if op == want {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// The asymmetric clearing: all four routes back to BACKLOG
// ---------------------------------------------------------------------------

// assertClearedAsymmetrically is the shared assertion of the four route tests
// below. Both fields are checked in one place so that neither half can be
// forgotten: clearing both columns, or clearing neither, is exactly the mistake
// this rule invites.
func assertClearedAsymmetrically(t *testing.T, f *commitTrackingFixture, id int, route string) {
	t.Helper()

	snap := f.snapshot(t, id)[id]

	if snap.Status != string(models.StatusBacklog) {
		t.Fatalf("%s: status = %s, want BACKLOG; the route did not run", route, snap.Status)
	}
	if snap.CommitClose != "<NULL>" {
		t.Errorf("%s: commit_close = %s, want NULL; every return to BACKLOG clears it "+
			"(SPEC/STATE_MACHINE.md § Commit Tracking Fields, rule 4)", route, snap.CommitClose)
	}
	if snap.CommitOpen != "«"+commitWorkStarted+"»" {
		t.Errorf("%s: commit_open = %s, want «%s» preserved; no route back to BACKLOG clears it, which "+
			"is where it parts company with the lifecycle timestamps "+
			"(SPEC/STATE_MACHINE.md § Commit Tracking Fields, rule 5)",
			route, snap.CommitOpen, commitWorkStarted)
	}
	// The timestamps and the summary go, which is what makes the surviving
	// commit_open a deliberate exception rather than an oversight.
	for _, field := range []struct{ name, value string }{
		{"started_at", snap.StartedAt},
		{"tested_at", snap.TestedAt},
		{"closed_at", snap.ClosedAt},
		{"completion_summary", snap.CompletionSummary},
	} {
		if field.value != "<NULL>" {
			t.Errorf("%s: %s = %s, want NULL", route, field.name, field.value)
		}
	}
}

// TestTaskStatBacklog_ClearsCommitCloseAndPreservesCommitOpen is route 1 of 4.
func TestTaskStatBacklog_ClearsCommitCloseAndPreservesCommitOpen(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-backlog-route")

	id := f.taskIDs[0]
	f.walkToCompleted(t, id)
	f.stat(t, itoa(id), "BACKLOG")

	assertClearedAsymmetrically(t, f, id, "task stat <id> BACKLOG")
}

// TestTaskReopen_ClearsCommitCloseAndPreservesCommitOpen is route 2 of 4. Two
// source states are covered: COMPLETED, where commit_close holds a value, and
// DOING, which only `task reopen` can return to BACKLOG at all.
func TestTaskReopen_ClearsCommitCloseAndPreservesCommitOpen(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-reopen-route")

	completed, doing := f.taskIDs[0], f.taskIDs[1]
	f.walkToCompleted(t, completed)
	f.stat(t, itoa(doing), "DOING", "--commit-open", commitWorkStarted)

	_ = captureStdout(t, func() {
		if err := taskReopen([]string{"-r", f.roadmap, f.idCSV(completed, doing)}); err != nil {
			t.Fatalf("task reopen: %v", err)
		}
	})

	assertClearedAsymmetrically(t, f, completed, "task reopen (from COMPLETED)")
	assertClearedAsymmetrically(t, f, doing, "task reopen (from DOING)")
}

// TestSprintRemoveTasks_ClearsCommitCloseAndPreservesCommitOpen is route 3 of 4.
func TestSprintRemoveTasks_ClearsCommitCloseAndPreservesCommitOpen(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-remove-tasks-route")

	completed, testing := f.taskIDs[0], f.taskIDs[1]
	f.walkToCompleted(t, completed)
	f.stat(t, itoa(testing), "DOING", "--commit-open", commitWorkStarted)
	f.stat(t, itoa(testing), "TESTING")

	_ = captureStdout(t, func() {
		if err := sprintRemoveTasks([]string{
			"-r", f.roadmap, itoa(f.sprintID), f.idCSV(completed, testing),
		}); err != nil {
			t.Fatalf("sprint remove-tasks: %v", err)
		}
	})

	assertClearedAsymmetrically(t, f, completed, "sprint remove-tasks (from COMPLETED)")
	assertClearedAsymmetrically(t, f, testing, "sprint remove-tasks (from TESTING)")
}

// TestSprintRemove_ClearsCommitCloseAndPreservesCommitOpen is route 4 of 4. The
// sprint cascade resets every member task at once, so it is asserted on three
// tasks in three different source states.
func TestSprintRemove_ClearsCommitCloseAndPreservesCommitOpen(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-sprint-remove-route")

	completed, testing, doing := f.taskIDs[0], f.taskIDs[1], f.taskIDs[2]
	f.walkToCompleted(t, completed)
	f.stat(t, itoa(testing), "DOING", "--commit-open", commitWorkStarted)
	f.stat(t, itoa(testing), "TESTING")
	f.stat(t, itoa(doing), "DOING", "--commit-open", commitWorkStarted)

	_ = captureStdout(t, func() {
		if err := sprintRemove([]string{"-r", f.roadmap, itoa(f.sprintID)}); err != nil {
			t.Fatalf("sprint remove: %v", err)
		}
	})

	assertClearedAsymmetrically(t, f, completed, "sprint remove (from COMPLETED)")
	assertClearedAsymmetrically(t, f, testing, "sprint remove (from TESTING)")
	assertClearedAsymmetrically(t, f, doing, "sprint remove (from DOING)")
}

// ---------------------------------------------------------------------------
// Batch atomicity
// ---------------------------------------------------------------------------

// TestTaskStat_BatchRejectionLeavesEveryTaskUntouched is the fail-fast property
// the validation order exists to deliver. Each case sends three tasks that would
// all transition happily, and breaks exactly one thing about the invocation; the
// assertion is that none of the three moved, not merely that the command failed.
//
// The three cases straddle the whole validation order: the missing flag and the
// malformed hash are rejected at step 4, before the database is opened at all,
// while the ineligible task is only discovered at step 6, with the database open
// and the batch already read — the point at which a fail-fast implementation is
// most tempted to have written something.
func TestTaskStat_BatchRejectionLeavesEveryTaskUntouched(t *testing.T) {
	f := setupCommitTrackingRoadmap(t, "commit-batch-atomicity")

	// Three tasks that can all move to DOING, plus one that cannot: the fourth
	// is sent back to BACKLOG, from where DOING is refused.
	eligible := []int{f.taskIDs[0], f.taskIDs[1], f.taskIDs[2]}
	ineligible := f.taskIDs[3]
	f.stat(t, itoa(ineligible), "BACKLOG")

	for _, tc := range []struct {
		name  string
		ids   []int
		extra []string
	}{
		{
			name: "the flag the target state requires is missing",
			ids:  eligible,
		},
		{
			name:  "the hash is malformed",
			ids:   eligible,
			extra: []string{"--commit-open", "not-a-hash"},
		},
		{
			name:  "one task of the batch cannot make the transition",
			ids:   append(append([]int{}, eligible...), ineligible),
			extra: []string{"--commit-open", commitWorkStarted},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := f.snapshot(t, tc.ids...)
			err := f.statErr(t, f.idCSV(tc.ids...), "DOING", tc.extra...)
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("error %v does not chain utils.ErrValidation, so it would not map to exit code 6", err)
			}
			assertUnchanged(t, before, f.snapshot(t, tc.ids...), "batch: "+tc.name)
		})
	}

	// A control run proves the batch really was capable of moving: without it,
	// every assertion above would also pass against a command that can never
	// transition anything.
	before := f.snapshot(t, eligible...)
	f.stat(t, f.idCSV(eligible...), "DOING", "--commit-open", commitWorkStarted)
	after := f.snapshot(t, eligible...)
	for _, id := range eligible {
		if after[id] == before[id] {
			t.Errorf("control: task #%d did not move on a valid batch invocation; the rejection "+
				"assertions above prove nothing", id)
		}
		if after[id].CommitOpen != "«"+commitWorkStarted+"»" {
			t.Errorf("control: task #%d has commit_open = %s, want «%s»; every task of a batch receives "+
				"the same supplied hash", id, after[id].CommitOpen, commitWorkStarted)
		}
	}
}
