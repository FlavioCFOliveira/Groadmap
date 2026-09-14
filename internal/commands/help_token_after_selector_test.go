package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestHelpToken_ServedInEveryPosition is the regression guard for rmp task 474.
//
// Task 461 made `stats` and `roadmap list` refuse an unrecognised flag through
// rejectUnknownFlags. That function assumes the help tokens have already been
// consumed. The assumption holds for `roadmap list`, whose help is served by
// Command.DispatchFamily before the handler runs, but not for `stats`: it is a
// leaf command, and its handler checked for a help token in the first position
// only. `rmp stats -r <name> --help` therefore reached the rejector and exited 2
// with `Error: invalid input: unknown flag: --help`, while
// `rmp task list -r <name> --help` printed its help and exited 0.
//
// SPEC/HELP.md § Help levels routes `--help`, `-h` and the literal word `help`
// anywhere in the argument list to the matching printer before any other
// parsing runs, so the help is reachable even when -r is missing.
// SPEC/COMMANDS.md § Roadmap Statistics requires `stats` to be
// indistinguishable from `task list` in how it resolves its arguments.
//
// Every row goes through the dispatcher and must produce exactly the bytes the
// command's own printer produces, banner included, with no error. An exact
// comparison is needed because the stats help body contains a sample report,
// so a check for a substring could not distinguish the help from the report.
// The `task list` rows are controls: they pin the behaviour `stats` must
// match, so a change to the shared mechanism shows up here as well.
func TestHelpToken_ServedInEveryPosition(t *testing.T) {
	f, cleanup := newArityFixture(t, "help-token-roadmap")
	defer cleanup()

	statsHelp := captureStdout(t, func() { invokeHelpPrinter(printStatsHelp) })
	roadmapListHelp := captureStdout(t, func() { invokeHelpPrinter(printRoadmapListHelp) })
	taskListHelp := captureStdout(t, func() { invokeHelpPrinter(printTaskListHelp) })

	cases := []struct {
		label  string
		family string
		args   []string
		want   string
	}{
		// stats: the defect of rmp task 474, and every other position of the
		// three help tokens relative to the selector.
		{"stats -r <name> --help", "stats", []string{"-r", f.roadmap, "--help"}, statsHelp},
		{"stats -r <name> -h", "stats", []string{"-r", f.roadmap, "-h"}, statsHelp},
		{"stats -r <name> help", "stats", []string{"-r", f.roadmap, "help"}, statsHelp},
		{"stats --roadmap <name> --help", "stats", []string{"--roadmap", f.roadmap, "--help"}, statsHelp},
		{"stats --help -r <name>", "stats", []string{"--help", "-r", f.roadmap}, statsHelp},
		{"stats -r <name> --zzz-unknown --help", "stats", []string{"-r", f.roadmap, "--zzz-unknown", "--help"}, statsHelp},
		{"stats --zzz-unknown --help (no selector)", "stats", []string{"--zzz-unknown", "--help"}, statsHelp},

		// roadmap list takes no selector and no argument, so the only place a
		// help token can be written is after the subcommand name.
		{"roadmap list --help", "roadmap", []string{"list", "--help"}, roadmapListHelp},
		{"roadmap list -h", "roadmap", []string{"list", "-h"}, roadmapListHelp},
		{"roadmap list help", "roadmap", []string{"list", "help"}, roadmapListHelp},
		{"roadmap ls --help (alias)", "roadmap", []string{"ls", "--help"}, roadmapListHelp},
		{"roadmap list --zzz-unknown --help", "roadmap", []string{"list", "--zzz-unknown", "--help"}, roadmapListHelp},

		// Controls: the roadmap-scoped command stats must behave like.
		{"task list -r <name> --help", "task", []string{"list", "-r", f.roadmap, "--help"}, taskListHelp},
		{"task list -r <name> --zzz-unknown --help", "task", []string{"list", "-r", f.roadmap, "--zzz-unknown", "--help"}, taskListHelp},
	}

	for _, tc := range cases {
		stdout, err := dispatchInvocation(t, tc.family, tc.args...)
		if err != nil {
			t.Errorf("%s: a help request was refused: %v (stdout %q)", tc.label, err, stdout)
			continue
		}
		if stdout != tc.want {
			t.Errorf("%s: stdout is not the command's help.\n got: %.160q\nwant: %.160q",
				tc.label, stdout, tc.want)
		}
	}
}

// TestHelpToken_OnlyTheExactTokensAreHelp is the inverse assertion. If the fix
// treated any token that merely looks like help as a help request, every row
// above would still pass while an unrecognised flag was silently accepted on
// both commands. A token that is not exactly `--help`, `-h` or `help` must
// still be refused with exit 2 and the CLI-wide line
// (SPEC/COMMANDS.md § Positional Arguments, rule 5), and nothing may be
// written to stdout.
func TestHelpToken_OnlyTheExactTokensAreHelp(t *testing.T) {
	f, cleanup := newArityFixture(t, "help-token-lookalike-roadmap")
	defer cleanup()

	cases := []struct {
		label  string
		family string
		args   []string
		flag   string
	}{
		{"stats -r <name> --helpful", "stats", []string{"-r", f.roadmap, "--helpful"}, "--helpful"},
		{"stats -r <name> -help", "stats", []string{"-r", f.roadmap, "-help"}, "-help"},
		{"roadmap list --helpful", "roadmap", []string{"list", "--helpful"}, "--helpful"},
		{"roadmap list -help", "roadmap", []string{"list", "-help"}, "-help"},
	}

	for _, tc := range cases {
		stdout, err := dispatchInvocation(t, tc.family, tc.args...)
		if err == nil {
			t.Errorf("%s: a token that is not a help token was accepted; stdout was %.160q",
				tc.label, stdout)
			continue
		}
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("%s: error = %v, want it to wrap utils.ErrInvalidInput (exit 2)", tc.label, err)
		}
		if got, want := err.Error(), "invalid input: unknown flag: "+tc.flag; got != want {
			t.Errorf("%s: message = %q, want %q", tc.label, got, want)
		}
		if stdout != "" {
			t.Errorf("%s: a refused invocation wrote to stdout: %.160q", tc.label, stdout)
		}
	}

	// The report itself is still produced when no help token is present, so
	// the fix did not turn every invocation into a help request.
	stdout, err := dispatchInvocation(t, "stats", "-r", f.roadmap)
	if err != nil {
		t.Fatalf("stats -r <name> was refused: %v", err)
	}
	if strings.HasPrefix(stdout, "AI agents:") || !strings.Contains(stdout, `"average_velocity"`) {
		t.Errorf("stats -r <name> did not write the statistics report: %.160q", stdout)
	}
}
