package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestUnknownFlag_StatsAndRoadmapListRefuseIt is the regression guard for
// rmp task 461.
//
// `rmp stats -r <name> --zzz-unknown` returned the whole statistics report and
// exit 0, and `rmp roadmap list --zzz-unknown` returned the whole listing and
// exit 0. They were the only two commands of the CLI that did: every other one
// refuses an unrecognised flag with exit 2 and the line the shared parser words
// (SPEC/COMMANDS.md § Positional Arguments, rule 5).
//
// Neither reached that parser. `HandleStats` read its arguments with
// requireRoadmap, whose loop keeps -r and appends every other token to a
// `remaining` slice the call site then bound to `_`; `roadmap list` was
// registered as a closure that dropped its arguments before the handler saw
// them. A token that names nothing was therefore discarded, and the command
// produced output the caller had not asked for under an argument the caller
// believed had an effect.
//
// The case drives the two defective commands and, in the same table, two
// commands that already refused. Without the controls a refusal wired to no
// command at all would still pass every row about the two, because the rows
// would all be asserting the same broken thing.
func TestUnknownFlag_StatsAndRoadmapListRefuseIt(t *testing.T) {
	f, cleanup := newArityFixture(t, "unknown-flag-roadmap")
	defer cleanup()

	cases := []struct {
		label  string
		family string
		args   []string
		flag   string
	}{
		// The two commands rmp task 461 names.
		{"stats", "stats", []string{"-r", f.roadmap, "--zzz-unknown"}, "--zzz-unknown"},
		{"roadmap list", "roadmap", []string{"list", "--zzz-unknown"}, "--zzz-unknown"},
		{"roadmap ls (alias)", "roadmap", []string{"ls", "--zzz-unknown"}, "--zzz-unknown"},
		// Controls: two commands that refused before the fix and must still.
		{"task list", "task", []string{"list", "-r", f.roadmap, "--zzz-unknown"}, "--zzz-unknown"},
		{"audit stats", "audit", []string{"stats", "-r", f.roadmap, "--zzz-unknown"}, "--zzz-unknown"},
	}

	for _, tc := range cases {
		stdout, err := dispatchInvocation(t, tc.family, tc.args...)
		if err == nil {
			t.Errorf("%s: an unrecognised flag was accepted and the command ran; "+
				"stdout was %q", tc.label, stdout)
			continue
		}
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("%s: error = %v, want it to wrap utils.ErrInvalidInput (exit 2)",
				tc.label, err)
		}
		want := "invalid input: unknown flag: " + tc.flag
		if err.Error() != want {
			t.Errorf("%s: message = %q, want %q", tc.label, err.Error(), want)
		}
		if stdout != "" {
			t.Errorf("%s: a refused invocation wrote to stdout: %q "+
				"(SPEC/COMMANDS.md § Failing Invocations Write Nothing to Stdout)",
				tc.label, stdout)
		}
	}
}

// TestUnknownFlag_TheRefusalIsWordedInOnePlace pins the reason the two
// commands now print the same line as the rest of the CLI: they call the same
// constructor the shared flag parser calls.
//
// A second literal would be a second sentence to keep in step with
// SPEC/COMMANDS.md, and the two would drift -- which is how `roadmap list`
// came to have no line at all.
func TestUnknownFlag_TheRefusalIsWordedInOnePlace(t *testing.T) {
	fromParser := NewFlagParser([]FlagDef{{Name: "--title", Field: "Title", Type: "string"}}).
		parseUnknown(t, "--zzz-unknown")
	fromRejector := rejectUnknownFlags([]string{"--zzz-unknown"})

	if fromRejector == nil {
		t.Fatal("rejectUnknownFlags accepted a token that names no flag")
	}
	if fromParser.Error() != fromRejector.Error() {
		t.Errorf("the two refusal sites word the line differently:\n  parser:   %q\n  rejector: %q",
			fromParser.Error(), fromRejector.Error())
	}
	if !strings.HasPrefix(fromRejector.Error(), "invalid input: unknown flag: ") {
		t.Errorf("the refusal no longer carries the published wording: %q", fromRejector.Error())
	}
}

// parseUnknown runs the parser over one unrecognised token and returns the
// error it produced, failing the test when it produced none.
func (fp *FlagParser) parseUnknown(t *testing.T, token string) error {
	t.Helper()
	_, err := fp.Parse([]string{token})
	if err == nil {
		t.Fatalf("the flag parser accepted %q, which names none of its flags", token)
	}
	return err
}

// TestUnknownFlag_RejectUnknownFlagsAdmitsWhatItMust is the inverse assertion.
// A refusal that fired on everything would pass the table above while breaking
// both commands, so what the rejector must ACCEPT is asserted too: the empty
// argument list every well-formed invocation of these two commands leaves
// behind, and a positional token, which is the arity point's to judge and not
// this function's.
func TestUnknownFlag_RejectUnknownFlagsAdmitsWhatItMust(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{},
		{"a-positional-token"},
		{"one", "two"},
	} {
		if err := rejectUnknownFlags(args); err != nil {
			t.Errorf("rejectUnknownFlags(%v) refused an argument list carrying no flag: %v",
				args, err)
		}
	}
	// A GNU-style "--flag=value" is reported by the flag's name alone, exactly
	// as the parser reports it.
	err := rejectUnknownFlags([]string{"--zzz-unknown=7"})
	if err == nil {
		t.Fatal("rejectUnknownFlags accepted --zzz-unknown=7")
	}
	if got, want := err.Error(), "invalid input: unknown flag: --zzz-unknown"; got != want {
		t.Errorf("message = %q, want %q (the value is not part of the flag's name)", got, want)
	}
}

// TestUnknownFlag_TheTwoCommandsStillWork is the second inverse assertion, at
// the level of the commands themselves: the fix refuses the token and changes
// nothing else. Without it, a handler that refused every invocation would pass
// every case above.
func TestUnknownFlag_TheTwoCommandsStillWork(t *testing.T) {
	f, cleanup := newArityFixture(t, "unknown-flag-happy-roadmap")
	defer cleanup()

	stdout, err := dispatchInvocation(t, "stats", "-r", f.roadmap)
	if err != nil {
		t.Errorf("stats without the flag was refused: %v", err)
	}
	if !strings.Contains(stdout, "\"roadmap\"") {
		t.Errorf("stats without the flag wrote no report: %q", stdout)
	}

	stdout, err = dispatchInvocation(t, "roadmap", "list")
	if err != nil {
		t.Errorf("roadmap list without the flag was refused: %v", err)
	}
	if !strings.Contains(stdout, f.roadmap) {
		t.Errorf("roadmap list without the flag did not list the fixture roadmap: %q", stdout)
	}
}
