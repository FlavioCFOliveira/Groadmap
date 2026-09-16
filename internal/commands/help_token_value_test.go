package commands

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// registeredSubcommand returns the registry entry of family's subcommand name,
// or the single entry of a leaf command when family takes no subcommand.
func registeredSubcommand(t *testing.T, family, name string) *Subcommand {
	t.Helper()
	cmd := AppRegistry().FindCommand(family)
	if cmd == nil {
		t.Fatalf("family %q missing from the registry", family)
	}
	if !cmd.HasSubcommand {
		sub := leafSubcommand(family)
		if sub == nil {
			t.Fatalf("leaf command %q has no single registry entry", family)
		}
		return sub
	}
	sub := cmd.FindSubcommand(name)
	if sub == nil {
		t.Fatalf("subcommand %s %s missing from the registry", family, name)
	}
	return sub
}

// TestHasHelpFlag_OnlyInATokenPosition is the unit guard for rmp task 476.
//
// hasHelpFlag counted `--help`, `-h` and `help` anywhere in the argument list,
// so `rmp task create -t help ...` wrote the help instead of creating a task
// titled help, and `rmp task list -r help` wrote the help instead of refusing a
// reserved roadmap name. SPEC/HELP.md § Help tokens counts a help token only in a
// token position: never as the value of a flag the subcommand declares with a
// type other than boolean, `-r` included, while `--help` and `-h` are never a
// value in the separate form and a joined-form token is never a help token.
//
// Each group drives both positions over the roadmap selector, a free-text flag
// and an enum flag, so a predicate that ignored the declaration, or that read
// every flag as value-taking, fails a row.
func TestHasHelpFlag_OnlyInATokenPosition(t *testing.T) {
	taskCreate := registeredSubcommand(t, "task", "create")
	taskList := registeredSubcommand(t, "task", "list")
	sprintTasks := registeredSubcommand(t, "sprint", "tasks")
	stats := registeredSubcommand(t, "stats", "")
	web := registeredSubcommand(t, "web", "")

	cases := []struct {
		label string
		sub   *Subcommand
		args  []string
		want  bool
	}{
		// The roadmap selector.
		{"-r help: the selector's value", taskList, []string{"-r", "help"}, false},
		{"--roadmap help: the selector's value", taskList, []string{"--roadmap", "help"}, false},
		{"-r <name> help: after the value", taskList, []string{"-r", "ledger", "help"}, true},
		{"-r --help: never a value", taskList, []string{"-r", "--help"}, true},
		{"stats -r help: the selector's value", stats, []string{"-r", "help"}, false},
		{"stats -r <name> help: after the value", stats, []string{"-r", "ledger", "help"}, true},

		// A free-text flag.
		{"-t help: the title", taskCreate, []string{"-t", "help"}, false},
		{"--title help: the title", taskCreate, []string{"--title", "help"}, false},
		{"-t <title> help: after the value", taskCreate, []string{"-t", "Settle refunds", "help"}, true},
		{"-t --help: never a value", taskCreate, []string{"-t", "--help"}, true},
		{"-t -h: never a value", taskCreate, []string{"-t", "-h"}, true},
		{"--title=help: the joined form is never a help token", taskCreate, []string{"--title=help"}, false},

		// An enum flag.
		{"-s help: the status", taskList, []string{"-s", "help"}, false},
		{"--status help: the status", taskList, []string{"--status", "help"}, false},
		{"-s BACKLOG help: after the value", taskList, []string{"-s", "BACKLOG", "help"}, true},
		{"-s --help: never a value", taskList, []string{"-s", "--help"}, true},

		// Flags that take no value.
		{"a boolean flag takes no value", sprintTasks, []string{"-r", "ledger", "5", "--order-by-priority", "help"}, true},
		{"an undeclared flag takes no value", taskList, []string{"-r", "ledger", "--foo", "help"}, true},

		// Tokens that are not help tokens.
		{"--help=1", taskList, []string{"-r", "ledger", "--help=1"}, false},
		{"--help=", taskList, []string{"--help="}, false},
		{"-h=1", taskList, []string{"-h=1"}, false},
		{"another letter case", taskList, []string{"--HELP"}, false},

		// The web command's own declaration.
		{"web --host help: the host", web, []string{"--host", "help"}, false},
		{"web --no-open help: a boolean flag", web, []string{"--no-open", "help"}, true},

		// A nil entry declares no flag.
		{"nil declares nothing", nil, []string{"-r", "help"}, true},
	}

	for _, tc := range cases {
		if got := hasHelpFlag(tc.sub, tc.args); got != tc.want {
			t.Errorf("%s: hasHelpFlag(%q) = %v, want %v", tc.label, tc.args, got, tc.want)
		}
	}
}

// TestHelpTokenAsAFlagValue_IsThatFlagsValue drives the outcomes SPEC/HELP.md
// § Help tokens fixes through the dispatcher: the value `help` creates a task and
// a sprint, and as the selector's value it is refused as a reserved roadmap name
// on a family subcommand and on the leaf command `stats`.
func TestHelpTokenAsAFlagValue_IsThatFlagsValue(t *testing.T) {
	f, cleanup := newArityFixture(t, "help-value-roadmap")
	defer cleanup()
	r := f.roadmap

	for _, title := range [][]string{{"-t", "help"}, {"--title=help"}} {
		args := append([]string{"create", "-r", r}, title...)
		args = append(args,
			"-fr", "Operators need a task literally titled help",
			"-tr", "Store the title verbatim",
			"-ac", "The stored title reads help")
		out, err := dispatchInvocation(t, "task", args...)
		if err != nil {
			t.Fatalf("task create %q: %v", title, err)
		}
		var created map[string]any
		if err := json.Unmarshal([]byte(out), &created); err != nil {
			t.Fatalf("task create %q wrote %q, not the created object: %v", title, out, err)
		}
		id, ok := created["id"].(float64)
		if !ok {
			t.Fatalf("task create %q wrote %q, with no numeric id", title, out)
		}
		if got := taskFieldViaGet(t, r, int(id), "title"); got != "help" {
			t.Errorf("task create %q stored the title %v, want help", title, got)
		}
	}

	out, err := dispatchInvocation(t, "sprint", "create", "-r", r, "-t", "Help desk rollout", "-d", "help")
	if err != nil {
		t.Fatalf("sprint create -d help: %v", err)
	}
	var sprint map[string]any
	if err := json.Unmarshal([]byte(out), &sprint); err != nil {
		t.Fatalf("sprint create -d help wrote %q, not the created object: %v", out, err)
	}
	sprintID, ok := sprint["id"].(float64)
	if !ok {
		t.Fatalf("sprint create -d help wrote %q, with no numeric id", out)
	}
	out, err = dispatchInvocation(t, "sprint", "get", "-r", r, itoa(int(sprintID)))
	if err != nil {
		t.Fatalf("sprint get: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("sprint get wrote %q: %v", out, err)
	}
	if got["description"] != "help" {
		t.Errorf("sprint create -d help stored the description %v, want help", got["description"])
	}

	const reserved = `Error: validation error: "help": roadmap name is a reserved system name`
	for _, invocation := range []struct {
		family string
		args   []string
	}{
		{"task", []string{"list", "-r", "help"}},
		{"stats", []string{"-r", "help"}},
	} {
		out, err := dispatchInvocation(t, invocation.family, invocation.args...)
		if !errors.Is(err, utils.ErrValidation) {
			t.Errorf("%s %q: error = %v, want the reserved-name refusal (exit 6); stdout=%.120q",
				invocation.family, invocation.args, err, out)
			continue
		}
		if errorLine(err) != reserved {
			t.Errorf("%s %q: line = %q, want %q", invocation.family, invocation.args, errorLine(err), reserved)
		}
		if out != "" {
			t.Errorf("%s %q: a refused invocation wrote to stdout: %.120q", invocation.family, invocation.args, out)
		}
	}
}

// TestWebHelp_ATokenCarryingAValueIsAnUnknownFlag is the dispatcher guard for rmp
// task 477 on `rmp web`: its own parser read the part of `--help=1` before the
// "=" and served the help. The token is an unrecognised flag (SPEC/HELP.md § Help
// tokens), refused with exit 2 and named without the "=value" tail.
func TestWebHelp_ATokenCarryingAValueIsAnUnknownFlag(t *testing.T) {
	cases := []struct {
		token string
		name  string
	}{
		{"--help=1", "--help"},
		{"--help=", "--help"},
		{"-h=1", "-h"},
		{"--zzz=1", "--zzz"},
	}
	for _, tc := range cases {
		stdout, err := dispatchWebBounded(t, "--no-open", tc.token)
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("rmp web --no-open %s: error = %v, want utils.ErrInvalidInput (exit 2); stdout=%.160q",
				tc.token, err, stdout)
			continue
		}
		if got, want := err.Error(), "invalid input: unknown flag: "+tc.name; got != want {
			t.Errorf("rmp web --no-open %s: message = %q, want %q", tc.token, got, want)
		}
		if stdout != "" {
			t.Errorf("rmp web --no-open %s: a refused invocation wrote to stdout: %.160q", tc.token, stdout)
		}
	}
}
