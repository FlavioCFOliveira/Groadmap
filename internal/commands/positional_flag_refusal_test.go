package commands

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestSplitPositionals_ClassifiesEveryPosition pins the classification
// SPEC/COMMANDS.md § Positional Arguments states for a "-"-prefixed token written
// beside a command's positional arguments (rmp tasks 465 and 484).
//
// After every declared slot is filled, or between two positional arguments, the
// token stands in no slot and is a stray the caller refuses as an unknown flag.
// Before the first positional argument, or with no positional argument after it
// while a slot is still empty, it stands in that slot and is left for the slot's
// own check. The positional argument that follows a between-token fills the next
// slot.
func TestSplitPositionals_ClassifiesEveryPosition(t *testing.T) {
	cases := []struct {
		name        string
		tokens      []string
		declared    int
		positionals []string
		strays      []string
	}{
		{"after the only slot", []string{"8", "--foo"}, 1, []string{"8"}, []string{"--foo"}},
		{"after both slots", []string{"5", "8", "--foo"}, 2, []string{"5", "8"}, []string{"--foo"}},
		{"between two slots", []string{"12", "--foo", "3"}, 2, []string{"12", "3"}, []string{"--foo"}},
		{"before the first slot", []string{"--foo", "12", "3"}, 2, []string{"--foo", "12", "3"}, nil},
		{"alone in the only slot", []string{"--foo"}, 1, []string{"--foo"}, nil},
		{"no positional after it, slot still empty", []string{"12", "--foo"}, 2, []string{"12", "--foo"}, nil},
		{"both gaps of three slots", []string{"1", "--foo", "5", "--bar", "0"}, 3, []string{"1", "5", "0"}, []string{"--foo", "--bar"}},
		{"strays kept in command-line order", []string{"7", "--bar", "5", "--foo"}, 2, []string{"7", "5"}, []string{"--bar", "--foo"}},
		{"between, then standing in the last slot", []string{"1", "--foo", "5", "--bar"}, 3, []string{"1", "5", "--bar"}, []string{"--foo"}},
		{"a joined-form token is a stray like any other", []string{"8", "--foo=1"}, 1, []string{"8"}, []string{"--foo=1"}},
		{"a negative number between two slots is a flag", []string{"1", "-5", "3"}, 3, []string{"1", "3"}, []string{"-5"}},
		{"nothing written", nil, 2, []string{}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			positionals, strays := splitPositionals(tc.tokens, tc.declared)
			if !reflect.DeepEqual(positionals, tc.positionals) {
				t.Errorf("positionals = %q, want %q", positionals, tc.positionals)
			}
			if !reflect.DeepEqual(strays, tc.strays) {
				t.Errorf("strays = %q, want %q", strays, tc.strays)
			}
		})
	}
}

// taskFieldViaGet reads one field of one task back through `task get`, so the
// assertion is about what the command line observes and not about a model type.
func taskFieldViaGet(t *testing.T, roadmap string, id int, field string) any {
	t.Helper()
	out, err := dispatchInvocation(t, "task", "get", "-r", roadmap, itoa(id))
	if err != nil {
		t.Fatalf("task get %d: %v", id, err)
	}
	var tasks []map[string]any
	if err := json.Unmarshal([]byte(out), &tasks); err != nil || len(tasks) != 1 {
		t.Fatalf("task get %d wrote %q (decode error %v)", id, out, err)
	}
	return tasks[0][field]
}

// TestUnknownFlagBesidePositionals_IsRefusedAndChangesNothing is the in-process
// regression guard for rmp tasks 465 and 484.
//
// Before the fix, `task remove -r <name> <id> --foo` deleted the task and exited
// 0, `task get` wrote the task, `task prio -r <name> <id> --foo 3` was refused as
// an invalid priority, and `sprint add-tasks` refused `--foo` as a malformed task
// id. Each row must now be refused with exit code 2 and the CLI-wide unknown-flag
// line, write nothing to stdout, and leave the fixture as it was, which the
// read-back after the loop proves.
func TestUnknownFlagBesidePositionals_IsRefusedAndChangesNothing(t *testing.T) {
	f, cleanup := newArityFixture(t, "stray-flag-roadmap")
	defer cleanup()

	r := f.roadmap
	id := itoa(f.tasks[0])
	sprint := itoa(f.sprints[0])

	priorityBefore := taskFieldViaGet(t, r, f.tasks[0], "priority")
	severityBefore := taskFieldViaGet(t, r, f.tasks[0], "severity")

	cases := []struct {
		label  string
		family string
		args   []string
		flag   string
	}{
		{"task remove, after the ids", "task", []string{"remove", "-r", r, id, "--foo"}, "--foo"},
		{"task get, after the ids", "task", []string{"get", "-r", r, id, "--foo"}, "--foo"},
		{"task prio, between", "task", []string{"prio", "-r", r, id, "--foo", "3"}, "--foo"},
		{"task sev, between", "task", []string{"sev", "-r", r, id, "--foo", "3"}, "--foo"},
		{"sprint add-tasks, after both slots", "sprint", []string{"add-tasks", "-r", r, sprint, id, "--foo"}, "--foo"},
		{"the joined form is named without its value", "task", []string{"get", "-r", r, id, "--foo=1"}, "--foo"},
		{"the first stray in command-line order is named", "task", []string{"prio", "-r", r, id, "--bar", "3", "--foo"}, "--bar"},
		{"task stat: refused before --summary is validated", "task", []string{"stat", "-r", r, id, "BACKLOG", "--foo", "--summary", "Shipped"}, "--foo"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			out, err := dispatchInvocation(t, tc.family, tc.args...)
			if !errors.Is(err, utils.ErrInvalidInput) {
				t.Fatalf("error = %v, want it to wrap utils.ErrInvalidInput (exit 2); stdout=%q", err, out)
			}
			if got, want := errorLine(err), "Error: invalid input: unknown flag: "+tc.flag; got != want {
				t.Errorf("line = %q, want %q", got, want)
			}
			if out != "" {
				t.Errorf("a refused invocation wrote to stdout: %q", out)
			}
		})
	}

	// Nothing changed: the task still exists with its priority and severity, and
	// the sprint gained no member.
	if got := taskFieldViaGet(t, r, f.tasks[0], "priority"); got != priorityBefore {
		t.Errorf("priority = %v after the refusals, want %v", got, priorityBefore)
	}
	if got := taskFieldViaGet(t, r, f.tasks[0], "severity"); got != severityBefore {
		t.Errorf("severity = %v after the refusals, want %v", got, severityBefore)
	}
	out, err := dispatchInvocation(t, "sprint", "tasks", "-r", r, sprint)
	if err != nil {
		t.Fatalf("sprint tasks: %v", err)
	}
	var members []map[string]any
	if err := json.Unmarshal([]byte(out), &members); err != nil {
		t.Fatalf("sprint tasks wrote %q: %v", out, err)
	}
	if len(members) != 0 {
		t.Errorf("the sprint gained %d member(s) from a refused add-tasks: %v", len(members), members)
	}
}

// TestUnknownFlagBesidePositionals_WhereTheRefusalFalls pins the placement
// SPEC/COMMANDS.md § Positional Arguments gives the refusal: after the checks a
// command makes on its positional arguments without the roadmap, and before
// every check that needs the roadmap. A "-"-prefixed token standing in a slot
// keeps that slot's refusal (criteria 14, 15 and 18).
func TestUnknownFlagBesidePositionals_WhereTheRefusalFalls(t *testing.T) {
	f, cleanup := newArityFixture(t, "stray-order-roadmap")
	defer cleanup()

	r := f.roadmap
	id := itoa(f.tasks[0])
	const absent = "ghost-ledger-465"

	cases := []struct {
		label    string
		family   string
		args     []string
		sentinel error
		line     string // "" when only the class is asserted
	}{
		{"a range refusal on the id comes first", "task", []string{"remove", "-r", r, "0", "--foo"},
			utils.ErrValidation, "Error: validation error: task_id must be between 1 and 2147483647, got 0"},
		{"a malformed id comes first", "task", []string{"remove", "-r", r, "abc", "--foo"},
			utils.ErrInvalidInput, `Error: invalid input: invalid task ID: "abc" (must be a positive integer)`},
		{"a range refusal on the priority comes first", "task", []string{"prio", "-r", r, id, "--foo", "99"},
			utils.ErrValidation, "Error: validation error: priority must be between 0 and 9, got 99"},
		{"before the first positional: the slot's refusal", "task", []string{"prio", "-r", r, "--foo", id, "3"},
			utils.ErrInvalidInput, `Error: invalid input: invalid task ID: "--foo" (must be a positive integer)`},
		{"no positional after it: the slot's refusal", "task", []string{"prio", "-r", r, id, "--foo"},
			utils.ErrInvalidInput, `Error: invalid input: invalid priority: "--foo" is not an integer in 0-9`},
		{"task get with the token in its only slot", "task", []string{"get", "-r", r, "--foo"},
			utils.ErrInvalidInput, `Error: invalid input: invalid task ID: "--foo" (must be a positive integer)`},
		{"task next with the token in its only slot", "task", []string{"next", "-r", r, "--foo"},
			utils.ErrValidation, ""},
		{"the refusal precedes the roadmap (after)", "task", []string{"remove", "-r", absent, id, "--foo"},
			utils.ErrInvalidInput, "Error: invalid input: unknown flag: --foo"},
		{"the refusal precedes the roadmap (between)", "task", []string{"prio", "-r", absent, id, "--foo", "3"},
			utils.ErrInvalidInput, "Error: invalid input: unknown flag: --foo"},
		{"roadmap remove: the refusal precedes the existence check", "roadmap", []string{"remove", absent, "--foo"},
			utils.ErrInvalidInput, "Error: invalid input: unknown flag: --foo"},
		{"the selector is resolved first", "task", []string{"remove", id, "--foo"},
			utils.ErrNoRoadmap, ""},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			out, err := dispatchInvocation(t, tc.family, tc.args...)
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("error = %v, want it to wrap %v; stdout=%q", err, tc.sentinel, out)
			}
			if tc.line != "" && errorLine(err) != tc.line {
				t.Errorf("line = %q, want %q", errorLine(err), tc.line)
			}
			if out != "" {
				t.Errorf("a refused invocation wrote to stdout: %q", out)
			}
		})
	}
}
