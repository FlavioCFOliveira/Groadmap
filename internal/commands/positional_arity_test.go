package commands

// Regression suite for the single shared enforcement point of positional
// argument arity (internal/commands/positional_arity.go, called from
// Command.DispatchFamily).
//
// # The defect these tests close
//
// FlagParser.Parse collected every unrecognised positional argument into
// ParseResult.Args, and no caller inspected the slice. A token the user meant
// to matter was therefore accepted and silently discarded:
//
//	rmp roadmap create alpha-service beta-service
//	{"name": "alpha-service"}                       exit 0
//
// `beta-service` vanished and the roadmap the user asked for did not exist.
// Eleven commands behaved that way. Nothing failed, because nothing compared
// what was supplied against what the command declares it accepts.
//
// # What the tests hold
//
// SPEC/COMMANDS.md § Positional Arguments is canonical. Its rules are pinned
// here one at a time, and each test asserts an OUTCOME rather than an exit
// code alone: no roadmap is created, no task row is deleted, no store is
// opened. A test that only read the exit code would keep passing if the
// refusal moved to AFTER the work it is supposed to prevent.
//
// Two failure modes are guarded explicitly because both are easy to write and
// neither shows up in a suite of over-arity probes alone:
//
//  1. A blanket refusal of everything past the first positional argument.
//     TestPositionalArity_FullDeclaredArityIsAccepted drives commands of
//     declared arity 2 and 3 at their full arity and requires them to succeed.
//  2. A classification that mistakes a flag's value for a positional argument.
//     TestPositionalArity_FlagValuesAreNotPositional drives the invocations
//     that broke while this enforcement was being written, and requires each
//     one to keep reporting the failure it reported before.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// dispatchInvocation routes one invocation exactly as cmd/rmp/main.go does:
// the family is resolved in the registry and DispatchFamily is handed the
// remaining tokens. Calling a handler directly would bypass the enforcement
// point and make every assertion below vacuous.
func dispatchInvocation(t *testing.T, family string, args ...string) (string, error) {
	t.Helper()

	cmd := AppRegistry().FindCommand(family)
	if cmd == nil {
		t.Fatalf("family %q missing from the registry", family)
	}

	var err error
	out := captureStdout(t, func() { err = cmd.DispatchFamily(args) })
	return out, err
}

// requireCanonicalRefusal asserts the complete contract of a refused
// invocation: the sentinel that decides the exit code, the exact published
// line, and the silence of stdout.
func requireCanonicalRefusal(t *testing.T, label, stdout string, err error, offending string) {
	t.Helper()

	if err == nil {
		t.Errorf("%s: expected a refusal, got nil", label)
		return
	}
	if !errors.Is(err, utils.ErrInvalidInput) {
		t.Errorf("%s: error = %v, want it to wrap utils.ErrInvalidInput (exit 2)", label, err)
	}
	want := fmt.Sprintf("invalid input: unexpected argument %q", offending)
	if err.Error() != want {
		t.Errorf("%s: message = %q, want %q", label, err.Error(), want)
	}
	if stdout != "" {
		t.Errorf("%s: a refused invocation wrote to stdout: %q", label, stdout)
	}
}

// arityFixture builds one roadmap holding four tasks and two sprints, which is
// enough to drive every command of declared arity 1, 2 and 3 at its full
// arity with ids that really exist.
type arityFixture struct {
	database *db.DB
	roadmap  string
	tasks    []int
	sprints  []int
}

func newArityFixture(t *testing.T, roadmap string) (*arityFixture, func()) {
	t.Helper()

	database, cleanup := setupTestTaskRoadmap(t, roadmap)

	f := &arityFixture{database: database, roadmap: roadmap}
	titles := []string{
		"Capture authorisation on the payment gateway",
		"Reconcile settlement batches nightly",
		"Expire abandoned checkout sessions",
		"Publish the refund audit trail",
	}
	for i, title := range titles {
		f.tasks = append(f.tasks, createTaskViaCommand(t, roadmap, title,
			fmt.Sprintf("Merchants need outcome %d to be visible", i+1),
			fmt.Sprintf("Extend the settlement service, step %d", i+1),
			fmt.Sprintf("The %d-th flow is observable end to end", i+1)))
	}
	f.sprints = append(f.sprints,
		createSprintViaCommand(t, roadmap, "Payment capture", "Deliver the payment capture flow"),
		createSprintViaCommand(t, roadmap, "Refund flow", "Deliver the refund flow"))

	return f, cleanup
}

// ---------------------------------------------------------------------------
// The rule, across every family
// ---------------------------------------------------------------------------

// TestPositionalArity_ExcessPositionalIsRefused covers at least one command
// per family and every declared arity the CLI uses (0, 1, 2 and 3), so a
// refusal wired into one family only cannot pass.
func TestPositionalArity_ExcessPositionalIsRefused(t *testing.T) {
	f, cleanup := newArityFixture(t, "arity-refusal-roadmap")
	defer cleanup()

	r := f.roadmap
	id := itoa(f.tasks[0])
	sprint := itoa(f.sprints[0])

	cases := []struct {
		label     string
		family    string
		args      []string
		offending string
	}{
		// Declared arity 0.
		{"roadmap list", "roadmap", []string{"list", "unscheduled"}, "unscheduled"},
		{"task list", "task", []string{"list", "-r", r, "unscheduled"}, "unscheduled"},
		{"task create", "task", []string{"create", "-r", r, "unscheduled"}, "unscheduled"},
		{"sprint list", "sprint", []string{"list", "-r", r, "unscheduled"}, "unscheduled"},
		{"backlog list", "backlog", []string{"list", "-r", r, "unscheduled"}, "unscheduled"},
		{"audit list", "audit", []string{"list", "-r", r, "unscheduled"}, "unscheduled"},
		{"audit stats", "audit", []string{"stats", "-r", r, "unscheduled"}, "unscheduled"},
		{"stats", "stats", []string{"-r", r, "unscheduled"}, "unscheduled"},

		// Declared arity 1.
		{"roadmap create", "roadmap", []string{"create", "alpha-service", "beta-service"}, "beta-service"},
		{"roadmap remove", "roadmap", []string{"remove", "alpha-service", "beta-service"}, "beta-service"},
		{"task get", "task", []string{"get", "-r", r, id, itoa(f.tasks[1])}, itoa(f.tasks[1])},
		{"task edit", "task", []string{"edit", "-r", r, id, "-t", "New title", "surplus"}, "surplus"},
		{"sprint show", "sprint", []string{"show", "-r", r, sprint, "surplus"}, "surplus"},
		{"sprint tasks", "sprint", []string{"tasks", "-r", r, sprint, "surplus"}, "surplus"},
		{"backlog show-next", "backlog", []string{"show-next", "-r", r, "5", "surplus"}, "surplus"},
		{"task comment-list", "task", []string{"comment-list", "-r", r, id, "surplus"}, "surplus"},

		// Declared arity 2.
		{"task stat", "task", []string{"stat", "-r", r, id, "SPRINT", "surplus"}, "surplus"},
		{"task prio", "task", []string{"prio", "-r", r, id, "7", "surplus"}, "surplus"},
		{"task add-dep", "task", []string{"add-dep", "-r", r, id, itoa(f.tasks[1]), "surplus"}, "surplus"},
		{"audit history", "audit", []string{"history", "-r", r, "TASK", id, "surplus"}, "surplus"},
		{"sprint add-tasks", "sprint", []string{"add-tasks", "-r", r, sprint, id, itoa(f.tasks[1])}, itoa(f.tasks[1])},

		// Declared arity 3.
		{"sprint move-tasks", "sprint", []string{"move-tasks", "-r", r, sprint, itoa(f.sprints[1]), id, "surplus"}, "surplus"},
		{"sprint move-to", "sprint", []string{"move-to", "-r", r, sprint, id, "1", "surplus"}, "surplus"},
		{"sprint swap", "sprint", []string{"swap", "-r", r, sprint, id, itoa(f.tasks[1]), "surplus"}, "surplus"},
	}

	for _, c := range cases {
		out, err := dispatchInvocation(t, c.family, c.args...)
		requireCanonicalRefusal(t, c.label, out, err, c.offending)
	}
}

// TestPositionalArity_FullDeclaredArityIsAccepted is the other half of the
// rule, and the half a suite of over-arity probes alone would leave unwritten:
// a blanket refusal of everything past the first positional argument passes
// every test above and fails every one here.
func TestPositionalArity_FullDeclaredArityIsAccepted(t *testing.T) {
	f, cleanup := newArityFixture(t, "arity-acceptance-roadmap")
	defer cleanup()

	r := f.roadmap
	first, second, third := itoa(f.tasks[0]), itoa(f.tasks[1]), itoa(f.tasks[2])
	sprintA, sprintB := itoa(f.sprints[0]), itoa(f.sprints[1])

	cases := []struct {
		label  string
		family string
		args   []string
	}{
		// Arity 1, required and optional.
		{"task get at arity 1", "task", []string{"get", "-r", r, first}},
		{"backlog show-next at arity 1", "backlog", []string{"show-next", "-r", r, "5"}},
		// A comma-separated list is ONE positional argument (rule 4).
		{"task get with a list at arity 1", "task", []string{"get", "-r", r, first + "," + second}},
		// Arity 2.
		{"task prio at arity 2", "task", []string{"prio", "-r", r, first, "7"}},
		{"sprint add-tasks at arity 2", "sprint", []string{"add-tasks", "-r", r, sprintA, first + "," + second}},
		{"audit history at arity 2", "audit", []string{"history", "-r", r, "TASK", first}},
		// Arity 3.
		{"sprint move-to at arity 3", "sprint", []string{"move-to", "-r", r, sprintA, first, "1"}},
		{"sprint swap at arity 3", "sprint", []string{"swap", "-r", r, sprintA, first, second}},
		{"sprint move-tasks at arity 3", "sprint", []string{"move-tasks", "-r", r, sprintA, sprintB, second}},
	}

	for _, c := range cases {
		if _, err := dispatchInvocation(t, c.family, c.args...); err != nil {
			t.Errorf("%s: %v — an invocation within its declared arity must be accepted", c.label, err)
		}
	}

	// The third task is untouched by the cases above and proves the fixture
	// still holds what it claims, so a run in which every command silently
	// failed could not read as a pass.
	if _, err := f.database.GetTask(context.Background(), f.tasks[2]); err != nil {
		t.Fatalf("fixture task %s vanished: %v", third, err)
	}
}

// TestPositionalArity_PositionOfTheOffendingTokenDoesNotMatter pins rule 3.
// What is refused is whatever remains once the flags and their values have
// been consumed, not a particular slot on the command line.
func TestPositionalArity_PositionOfTheOffendingTokenDoesNotMatter(t *testing.T) {
	f, cleanup := newArityFixture(t, "arity-position-roadmap")
	defer cleanup()

	between := []string{"list", "-r", f.roadmap, "unscheduled", "--limit", "5"}
	out, err := dispatchInvocation(t, "task", between...)
	requireCanonicalRefusal(t, "excess token written between two flags", out, err, "unscheduled")

	trailing := []string{"list", "-r", f.roadmap, "--limit", "5", "unscheduled"}
	out, err = dispatchInvocation(t, "task", trailing...)
	requireCanonicalRefusal(t, "excess token written at the end", out, err, "unscheduled")
}

// TestPositionalArity_FirstOffendingTokenIsNamed pins rule 2: with several
// tokens over the maximum, the command names the first of them and stops.
func TestPositionalArity_FirstOffendingTokenIsNamed(t *testing.T) {
	out, err := dispatchInvocation(t, "roadmap", "create", "alpha-service", "beta-service", "gamma-service")
	requireCanonicalRefusal(t, "roadmap create with two excess tokens", out, err, "beta-service")
}

// ---------------------------------------------------------------------------
// Outcomes: what a refused invocation must NOT have done
// ---------------------------------------------------------------------------

// TestPositionalArity_RoadmapCreateCreatesNothing is acceptance criterion 1.
// The named defect created `alpha-service` and discarded `beta-service`; the
// rule creates neither.
func TestPositionalArity_RoadmapCreateCreatesNothing(t *testing.T) {
	const wanted, discarded = "alpha-service", "beta-service"
	cleanupTestRoadmap(t, wanted)
	cleanupTestRoadmap(t, discarded)

	out, err := dispatchInvocation(t, "roadmap", "create", wanted, discarded)
	requireCanonicalRefusal(t, "roadmap create with an excess token", out, err, discarded)

	dataDir, dirErr := utils.GetDataDir()
	if dirErr != nil {
		t.Fatalf("GetDataDir: %v", dirErr)
	}
	for _, name := range []string{wanted, discarded} {
		if _, statErr := os.Stat(filepath.Join(dataDir, name)); !os.IsNotExist(statErr) {
			t.Errorf("roadmap home for %q exists after a refused create (stat error = %v); "+
				"a refused invocation creates nothing", name, statErr)
		}
	}
}

// TestPositionalArity_RefusalPrecedesOpeningTheStore is rule 6 and the probe
// the rule is worth: a roadmap that does not exist would fail with exit 4 on
// its own, so an exit-2 verdict can only mean the refusal landed BEFORE the
// database was opened.
func TestPositionalArity_RefusalPrecedesOpeningTheStore(t *testing.T) {
	const ghost = "roadmap-that-does-not-exist"
	cleanupTestRoadmap(t, ghost)

	out, err := dispatchInvocation(t, "task", "remove", "-r", ghost, "3", "4")
	requireCanonicalRefusal(t, "task remove against an absent roadmap", out, err, "4")
	if errors.Is(err, utils.ErrNotFound) {
		t.Errorf("error = %v, wraps utils.ErrNotFound (exit 4): the store was opened before the refusal", err)
	}
}

// TestPositionalArity_TaskRemoveDeletesNothing asserts the outcome, not the
// exit code: the rows a refused `task remove` names must all still be there.
func TestPositionalArity_TaskRemoveDeletesNothing(t *testing.T) {
	f, cleanup := newArityFixture(t, "arity-removal-roadmap")
	defer cleanup()

	out, err := dispatchInvocation(t, "task", "remove", "-r", f.roadmap, itoa(f.tasks[0]), itoa(f.tasks[1]))
	requireCanonicalRefusal(t, "task remove with space-separated ids", out, err, itoa(f.tasks[1]))

	for _, id := range f.tasks {
		if _, getErr := f.database.GetTask(context.Background(), id); getErr != nil {
			t.Errorf("task %d is gone after a refused `task remove`: %v; "+
				"a refused invocation deletes nothing", id, getErr)
		}
	}
}

// TestPositionalArity_ExcessBeatsAValueThatWouldFailWithSix is acceptance
// criterion 5. An invocation carrying both an excess positional argument and
// a value that validation would reject with exit 6 exits 2: the refusal comes
// first, so the value is never reached.
func TestPositionalArity_ExcessBeatsAValueThatWouldFailWithSix(t *testing.T) {
	f, cleanup := newArityFixture(t, "arity-precedence-roadmap")
	defer cleanup()

	id := itoa(f.tasks[0])

	// Sanity: the value alone really is an exit-6 verdict, so the assertion
	// below distinguishes two live outcomes rather than one.
	if _, err := dispatchInvocation(t, "task", "prio", "-r", f.roadmap, id, "99"); !errors.Is(err, utils.ErrValidation) {
		t.Fatalf("`task prio %s 99` error = %v, want utils.ErrValidation; the probe no longer models the collision", id, err)
	}

	out, err := dispatchInvocation(t, "task", "prio", "-r", f.roadmap, id, "99", "surplus")
	requireCanonicalRefusal(t, "task prio with a bad value AND an excess token", out, err, "surplus")
	if errors.Is(err, utils.ErrValidation) {
		t.Errorf("error = %v, wraps utils.ErrValidation (exit 6): the value was validated before the refusal", err)
	}
}

// ---------------------------------------------------------------------------
// What the rule must NOT change
// ---------------------------------------------------------------------------

// TestPositionalArity_SelfRefusingCommandsKeepTheirWording is acceptance
// criterion 6 for the two families reachable in this package. Each refused an
// excess positional argument before the CLI-wide rule was stated, and each
// publishes a line of its own that SPEC/COMMANDS.md § Positional Arguments
// keeps: the shared point defers instead of overriding them.
//
// `ai-help` is the third, and is checked in cmd/rmp, where its early-pass
// interception lives.
func TestPositionalArity_SelfRefusingCommandsKeepTheirWording(t *testing.T) {
	f, cleanup := newArityFixture(t, "arity-selfrefusal-roadmap")
	defer cleanup()

	cases := []struct {
		label  string
		family string
		args   []string
		want   string
	}{
		{
			label:  "graph query with a bare Cypher query",
			family: "graph",
			args:   []string{"query", "-r", f.roadmap, "MATCH (n:Incident) RETURN n"},
			want:   `invalid input: unexpected argument "MATCH (n:Incident) RETURN n" (graph queries use --query or stdin)`,
		},
		{
			label:  "web with a positional argument",
			family: "web",
			args:   []string{"monitoring-dashboard", "--no-open"},
			want:   "invalid input: unexpected argument: monitoring-dashboard",
		},
	}

	for _, c := range cases {
		out, err := dispatchInvocation(t, c.family, c.args...)
		if err == nil {
			t.Errorf("%s: expected a refusal, got nil", c.label)
			continue
		}
		if err.Error() != c.want {
			t.Errorf("%s: message = %q, want %q — the shared enforcement point must not override "+
				"a command that publishes its own line", c.label, err.Error(), c.want)
		}
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("%s: error = %v, want it to wrap utils.ErrInvalidInput (exit 2)", c.label, err)
		}
		if out != "" {
			t.Errorf("%s: a refused invocation wrote to stdout: %q", c.label, out)
		}
	}
}

// TestPositionalArity_FlagValuesAreNotPositional drives the invocations that
// broke while this enforcement was being written. Every one of them supplies
// a token that LOOKS like a positional argument and is not: the value of a
// flag, a negative number, an empty string, or a token that follows an
// unknown flag. Each must still report the failure it reported before, so
// counting positional arguments never converts one error class into another.
func TestPositionalArity_FlagValuesAreNotPositional(t *testing.T) {
	f, cleanup := newArityFixture(t, "arity-classification-roadmap")
	defer cleanup()

	r := f.roadmap
	id := itoa(f.tasks[0])

	cases := []struct {
		label  string
		family string
		args   []string
		want   string
	}{
		{
			// `--body` is present with no usable value: the token after it is
			// itself a flag. Consuming it would push `NOTE` into the
			// positional count and refuse the invocation as over-arity
			// instead of reporting the missing body.
			label:  "comment-add with a --body that has no value",
			family: "task",
			args:   []string{"comment-add", "-r", r, id, "--body", "--type", "NOTE"},
			want:   "required parameter missing: no comment body supplied",
		},
		{
			// On the comment subcommands every "-"-prefixed token is a flag,
			// digits included (SPEC/COMMANDS.md § Comment Positional Argument
			// Contract, rule 2).
			label:  "comment-add with a stray -1",
			family: "task",
			args:   []string{"comment-add", "-r", r, id, "-1", "--type", "NOTE", "--body", "Recorded during triage"},
			want:   "invalid input: unknown flag: -1",
		},
		{
			// A negative integer IS a value, and reaches the handler so the
			// range verdict is the one the user sees.
			label:  "task prio with a negative priority",
			family: "task",
			args:   []string{"prio", "-r", r, id, "-1"},
			want:   "validation error: priority must be between 0 and 9, got -1",
		},
		{
			// An unknown flag with an empty-string value. "" is not
			// "-"-prefixed, so it must be read as that flag's value and not
			// as a second positional argument.
			label:  "task edit with a retired flag and an empty value",
			family: "task",
			args:   []string{"edit", "-r", r, id, "-sp", ""},
			want:   "invalid input: unknown flag: -sp",
		},
	}

	for _, c := range cases {
		out, err := dispatchInvocation(t, c.family, c.args...)
		if err == nil {
			t.Errorf("%s: expected an error, got nil", c.label)
			continue
		}
		if err.Error() != c.want {
			t.Errorf("%s: message = %q, want %q", c.label, err.Error(), c.want)
		}
		if out != "" {
			t.Errorf("%s: a failing invocation wrote to stdout: %q", c.label, out)
		}
	}
}

// TestPositionalArity_HelpIsServedNotRefused records the one deliberate
// carve-out: a help token anywhere in the invocation is a request for the
// help body, which every level answers with exit 0 and always did. The arity
// rule governs work, not documentation, and moving the check ahead of the
// help path would silently retire `rmp <cmd> <sub> ... --help`.
func TestPositionalArity_HelpIsServedNotRefused(t *testing.T) {
	out, err := dispatchInvocation(t, "roadmap", "create", "alpha-service", "beta-service", "--help")
	if err != nil {
		t.Errorf("help request refused: %v", err)
	}
	if !strings.Contains(out, "rmp roadmap create") {
		t.Errorf("help body not written to stdout; got %q", out)
	}
}
