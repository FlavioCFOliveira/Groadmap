package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The tests in this file pin SPEC/COMMANDS.md § Repeated Flags: no flag of any
// command is repeatable, and an invocation that supplies one flag twice, in any
// spelling, exits 2 with `invalid input: repeated flag: <flag>`, naming the
// second occurrence as the command line spells it, before any value of it is
// examined and after a help token has had its chance to be served.

// repeatedFlagValue is a value the first occurrence of f can carry without
// being refused on its own, so the refusal a case observes is the repetition
// and not the value.
func repeatedFlagValue(f *Flag) string {
	switch f.Long {
	case "--roadmap":
		return "" // filled in by the caller
	case "--port":
		return "8787"
	case "--host":
		return "127.0.0.1"
	case "--socket":
		return "/tmp/rmp-repeated-flag.sock"
	case "--query":
		return "MATCH (n) RETURN n"
	case "--commit-open", "--commit-close":
		return "3f9a2c1"
	case "--order", "--entity-id", "--max-tasks", "--limit", "--parent":
		return "3"
	}
	switch f.Type {
	case "integer":
		return "3"
	case "date":
		return "2026-01-01"
	}
	return "Settlement reconciliation"
}

// repeatedFlagPositional is a well-formed value for a required positional
// argument.
func repeatedFlagPositional(p *Argument) string {
	switch p.Enum {
	case "TaskStatus":
		return "TESTING"
	case "AuditEntityType":
		return "TASK"
	}
	return "1"
}

// TestRepeatedFlag_EveryDeclaredFlagOfEverySubcommand drives the registry: for
// every subcommand and every flag it declares other than --help, the flag is
// written twice and the invocation must be refused with the published line.
// The registry is the source of the cases, so a flag added later is covered
// without this test being edited.
func TestRepeatedFlag_EveryDeclaredFlagOfEverySubcommand(t *testing.T) {
	f, cleanup := newArityFixture(t, "repeated-flag-registry")
	defer cleanup()

	cases := 0
	for ci := range AppRegistry().Commands {
		cmd := &AppRegistry().Commands[ci]
		for si := range cmd.Subcommands {
			sub := &cmd.Subcommands[si]
			takesRoadmap := false
			for _, fl := range sub.Flags {
				if fl.Long == "--roadmap" {
					takesRoadmap = true
				}
			}
			for _, fl := range sub.Flags {
				if fl.Long == "--help" {
					continue
				}
				var args []string
				if cmd.HasSubcommand {
					args = append(args, sub.Name)
				}
				if takesRoadmap && fl.Long != "--roadmap" {
					args = append(args, "-r", f.roadmap)
				}
				// The required positionals stand before the flags, so no
				// flag is read as one of them.
				for _, p := range sub.Positional {
					if p.Required {
						args = append(args, repeatedFlagPositional(&p))
					}
				}
				first, second := []string{fl.Long}, []string{fl.Long}
				if fl.Type != "boolean" {
					v := repeatedFlagValue(&fl)
					if fl.Long == "--roadmap" {
						v = f.roadmap
					}
					first = append(first, v)
					second = append(second, v)
				}
				args = append(args, first...)
				args = append(args, second...)

				label := strings.TrimSpace(cmd.Name+" "+sub.Name) + " " + fl.Long
				stdout, err := dispatchInvocation(t, cmd.Name, args...)
				cases++
				if err == nil {
					t.Errorf("%s: a repeated flag was accepted; stdout %q", label, stdout)
					continue
				}
				if !errors.Is(err, utils.ErrInvalidInput) {
					t.Errorf("%s: error %v does not carry ErrInvalidInput (exit 2)", label, err)
				}
				if want := "invalid input: repeated flag: " + fl.Long; err.Error() != want {
					t.Errorf("%s: message %q, want %q", label, err.Error(), want)
				}
				if stdout != "" {
					t.Errorf("%s: a refused invocation wrote to stdout: %q", label, stdout)
				}
			}
		}
	}
	if cases < 50 {
		t.Errorf("only %d flag cases were driven; the registry walk found too few flags to mean anything", cases)
	}
}

// TestRepeatedFlag_SpellingsOrderAndPrecedence pins the four rules of the
// section that a registry walk over one spelling cannot: every spelling of one
// flag counts as that flag and the line names the refused occurrence as
// written; the selector is a flag like any other; a value of a repeated flag
// is never validated; a help token is still served; and a missing selector,
// which is refused at an earlier step, keeps its place.
func TestRepeatedFlag_SpellingsOrderAndPrecedence(t *testing.T) {
	f, cleanup := newArityFixture(t, "repeated-flag-spellings")
	defer cleanup()
	r := f.roadmap

	refused := []struct {
		label  string
		family string
		args   []string
		named  string
	}{
		{"short then long", "task", []string{"create", "-r", r, "-t", "First", "--title", "Second"}, "--title"},
		{"long then short", "task", []string{"create", "-r", r, "--title", "First", "-t", "Second"}, "-t"},
		{"joined second occurrence", "task", []string{"create", "-r", r, "-t", "First", "--title=Second"}, "--title"},
		{"same value twice", "task", []string{"create", "-r", r, "-t", "First", "-t", "First"}, "-t"},
		{"selector twice", "task", []string{"list", "-r", r, "-r", "beta"}, "-r"},
		{"selector in both spellings, same roadmap", "task", []string{"list", "-r", r, "--roadmap", r}, "--roadmap"},
		{"boolean twice", "sprint", []string{"close", "-r", r, "1", "--force", "--force"}, "--force"},
		{"value never validated", "task", []string{"list", "-r", r, "-p", "12", "-p", "3"}, "-p"},
		{"entity id never parsed", "audit", []string{"list", "-r", r, "--entity-id", "abc", "--entity-id", "2"}, "--entity-id"},
		{"stat summary", "task", []string{"stat", "-r", r, "1", "COMPLETED", "-s", "Shipped", "--summary", "Again"}, "--summary"},
		{"comment body", "task", []string{"comment-add", "-r", r, "1", "-y", "NOTE", "-b", "First", "--body=Second"}, "--body"},
		{"graph query", "graph", []string{"client", "-r", r, "-q", "RETURN 1", "--query=RETURN 2"}, "--query"},
		{"graph socket", "graph", []string{"client", "-r", r, "--socket=/tmp/a.sock", "--socket", "/tmp/b.sock", "-q", "RETURN 1"}, "--socket"},
	}
	for _, tc := range refused {
		stdout, err := dispatchInvocation(t, tc.family, tc.args...)
		if err == nil {
			t.Errorf("%s: accepted", tc.label)
			continue
		}
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("%s: %v does not carry ErrInvalidInput", tc.label, err)
		}
		if want := "invalid input: repeated flag: " + tc.named; err.Error() != want {
			t.Errorf("%s: message %q, want %q", tc.label, err.Error(), want)
		}
		if stdout != "" {
			t.Errorf("%s: wrote to stdout: %q", tc.label, stdout)
		}
	}

	// A help token is served before any flag is refused.
	stdout, err := dispatchInvocation(t, "task", "create", "-r", r, "-t", "First", "-t", "Second", "--help")
	if err != nil || !strings.Contains(stdout, "Usage: rmp task create") {
		t.Errorf("help after a repeated flag: err %v, stdout %q; want the help and no error", err, stdout)
	}

	// The selector is read before the subcommand's own flags, so its absence
	// keeps its exit code 3 even when a flag after it is repeated.
	if _, err := dispatchInvocation(t, "task", "create", "-t", "First", "-t", "Second"); !errors.Is(err, utils.ErrNoRoadmap) {
		t.Errorf("missing selector with a repeated flag: %v, want the no-roadmap refusal", err)
	}

	// --help and -h are not flags in the sense of the rule.
	stdout, err = dispatchInvocation(t, "task", "list", "--help", "--help")
	if err != nil || !strings.Contains(stdout, "Usage: rmp task list") {
		t.Errorf("--help twice: err %v; want the help", err)
	}

	// Nothing was created by any refused invocation.
	out, err := dispatchInvocation(t, "task", "list", "-r", r, "-l", "100")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\"First\"") || strings.Contains(out, "Settlement reconciliation") {
		t.Errorf("a refused task create wrote a task: %s", out)
	}
}

// TestRepeatedFlag_OneOccurrenceStillWorks is the inverse control: the
// spellings above, each written once, are accepted.
func TestRepeatedFlag_OneOccurrenceStillWorks(t *testing.T) {
	f, cleanup := newArityFixture(t, "repeated-flag-control")
	defer cleanup()
	r := f.roadmap

	if _, err := dispatchInvocation(t, "task", "create", "-r", r, "--title=Expire abandoned carts",
		"-fr", "Carts must not hold stock forever", "-tr", "Add an expiry sweep", "-ac", "Stock is released after 30 minutes"); err != nil {
		t.Errorf("joined title, written once: %v", err)
	}
	if _, err := dispatchInvocation(t, "task", "list", "--roadmap", r, "-p", "3"); err != nil {
		t.Errorf("long selector and one priority: %v", err)
	}
	if _, err := dispatchInvocation(t, "audit", "list", "-r", r, "--entity-id", "1"); err != nil {
		t.Errorf("one entity id: %v", err)
	}
}

// TestFlagOccurrences_RecordsCanonicalNames pins the tracker every parser
// shares: a canonical name is refused on its second note, under the token the
// command line wrote, without its "=value" tail.
func TestFlagOccurrences_RecordsCanonicalNames(t *testing.T) {
	var seen utils.FlagOccurrences
	if err := seen.Note("--title", "-t"); err != nil {
		t.Fatalf("first occurrence refused: %v", err)
	}
	if err := seen.Note("--priority", "--priority=3"); err != nil {
		t.Fatalf("a second, different flag refused: %v", err)
	}
	err := seen.Note("--title", "--title=Second")
	if err == nil || err.Error() != "invalid input: repeated flag: --title" || !errors.Is(err, utils.ErrInvalidInput) {
		t.Errorf("second --title: %v", err)
	}
}
