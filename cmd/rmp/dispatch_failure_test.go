package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/aihelp"
	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// cmd/rmp — regression suite for the dispatch-failure contract.
//
// A dispatch failure is a name rmp cannot resolve to a command or to a
// subcommand of a command. SPEC/HELP.md § Error message format pins six
// things about it, and every one of them regressed independently before
// this suite existed:
//
//	exit code       127 at BOTH levels. An unresolved subcommand used to
//	                exit 2, because the error was wrapped in
//	                utils.ErrInvalidInput.
//	stdout          zero bytes. `rmp nadadisto` used to print 2 KB of
//	                general help to stdout.
//	recovery help   the help for the level that failed to resolve,
//	                written to stderr after the error line.
//	the banner      absent from that recovery help. It carries the same
//	                sentence as the trailing hint, so including it would
//	                put the hint on stderr twice — which is exactly the
//	                shape `rmp nadadisto` used to produce.
//	part order      Error line, blank, recovery help, blank, hint. The
//	                hint is last on every error path.
//	wording         lowercase "unknown …", with no sentinel prefix. The
//	                two levels used to disagree: "Unknown command:"
//	                against "invalid input: unknown task subcommand:".
//
// Scope of this file. It exercises the production seams in process:
// Command.DispatchFamily for the subcommand level, the registry lookup
// plus commands.NewUnknownCommandError for the command level, and the
// real handleError for both. The single line in main() that joins the
// registry miss to handleError cannot be reached in process (main calls
// os.Exit), and is covered end to end by tests/test_27_exit_code_extremes.py.

// dispatchFamilies are the six commands that dispatch subcommands, and
// for which an unresolved subcommand name can therefore arise
// (SPEC/HELP.md § Recovery help after a dispatch failure). `stats`,
// `web` and `ai-help` take no subcommand and are checked separately by
// TestDispatchFailure_LeafCommandsTakeNoDispatchPath.
var dispatchFamilies = []string{"roadmap", "task", "sprint", "backlog", "audit", "graph"}

// unresolvedName is a token that names no command, no subcommand, and no
// alias of either. Kept in one place so a future registry entry that
// happened to collide would break every test at once rather than
// silently turning one of them green for the wrong reason.
const unresolvedName = "nadadisto"

// capturedStreams holds the two streams of one probed invocation.
type capturedStreams struct {
	stdout string
	stderr string
}

// captureStreams redirects BOTH os.Stdout and os.Stderr for the duration
// of fn and returns what each received.
//
// Both streams are needed together: the claim under test is not merely
// "stderr looks right" but "stderr looks right AND stdout received
// nothing". Capturing only stderr would leave the stdout-silence
// assertion unwritten, which is the half that regressed.
//
// Each pipe is drained by its own goroutine, so a help body larger than
// the pipe buffer cannot deadlock the writer.
func captureStreams(t *testing.T, fn func()) capturedStreams {
	t.Helper()

	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (stdout): %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (stderr): %v", err)
	}
	os.Stdout, os.Stderr = outW, errW

	var outBuf, errBuf bytes.Buffer
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(&outBuf, outR); done <- struct{}{} }()
	go func() { _, _ = io.Copy(&errBuf, errR); done <- struct{}{} }()

	fn()

	if err := outW.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	if err := errW.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	os.Stdout, os.Stderr = oldOut, oldErr
	<-done
	<-done
	return capturedStreams{stdout: outBuf.String(), stderr: errBuf.String()}
}

// resetHintState clears BOTH process-global sentinels the failure path
// consults, so a probe observes the same state main() gives a fresh
// process.
//
// Resetting only the hint's sync.Once is not enough, and the omission is
// an order-dependent flake rather than an honest failure: any earlier
// test in this package that emits the AI contract flips
// aihelp.WasInvoked() to true for the rest of the process, and
// writeFailureReport then suppresses the trailing hint. The probe would
// see a two-part stderr and blame the code under test.
func resetHintState() {
	aihelp.ResetForTesting()
	aihelp.ResetHintForTesting()
}

// probeSubcommandMiss runs the real dispatch for `rmp <family> nadadisto`
// and feeds the resulting error to the real handleError, returning the
// exit code and both streams.
func probeSubcommandMiss(t *testing.T, family string) (int, capturedStreams) {
	t.Helper()
	cmd := commands.AppRegistry().FindCommand(family)
	if cmd == nil {
		t.Fatalf("family %q missing from the registry", family)
	}
	resetHintState()
	var code int
	streams := captureStreams(t, func() {
		code = handleError(cmd.DispatchFamily([]string{unresolvedName}))
	})
	return code, streams
}

// probeCommandMiss runs the top-level lookup for `rmp nadadisto` and
// feeds the resulting error to the real handleError.
//
// The registry lookup is performed here rather than assumed, so the test
// fails loudly if `nadadisto` ever becomes a real command instead of
// quietly asserting nothing.
func probeCommandMiss(t *testing.T) (int, capturedStreams) {
	t.Helper()
	if commands.AppRegistry().FindCommand(unresolvedName) != nil {
		t.Fatalf("%q resolves to a command; the probe no longer models a dispatch failure", unresolvedName)
	}
	resetHintState()
	var code int
	streams := captureStreams(t, func() {
		code = handleError(commands.NewUnknownCommandError(unresolvedName))
	})
	return code, streams
}

// ---------------------------------------------------------------------------
// Exit code
// ---------------------------------------------------------------------------

// TestDispatchFailure_UnresolvedSubcommandExits127 covers every family
// that dispatches subcommands, not a sample of three: the exit code is
// decided by one shared path, so a family left behind would mean that
// family had grown a path of its own.
func TestDispatchFailure_UnresolvedSubcommandExits127(t *testing.T) {
	for _, family := range dispatchFamilies {
		code, streams := probeSubcommandMiss(t, family)
		if code != ExitCmdNotFound {
			t.Errorf("`rmp %s %s` exited %d, want %d (ExitCmdNotFound); stderr: %s",
				family, unresolvedName, code, ExitCmdNotFound, streams.stderr)
		}
	}
}

// TestDispatchFailure_UnresolvedCommandExits127 pins the other level. The
// two levels are the same failure observed at two depths of the command
// tree, and the exit code does not distinguish them (SPEC/HELP.md § Exit
// code of a dispatch failure).
func TestDispatchFailure_UnresolvedCommandExits127(t *testing.T) {
	code, streams := probeCommandMiss(t)
	if code != ExitCmdNotFound {
		t.Errorf("`rmp %s` exited %d, want %d (ExitCmdNotFound); stderr: %s",
			unresolvedName, code, ExitCmdNotFound, streams.stderr)
	}
}

// TestDispatchFailure_SentinelIsUnknownCommandNotInvalidInput pins the
// cause rather than the symptom. The exit code is derived from the
// sentinel, so an error that also wrapped utils.ErrInvalidInput would be
// one reordering of handleError's switch away from exiting 2 again
// (SPEC/ARCHITECTURE.md § Sentinel Error Catalogue).
func TestDispatchFailure_SentinelIsUnknownCommandNotInvalidInput(t *testing.T) {
	probes := map[string]error{
		"rmp " + unresolvedName: commands.NewUnknownCommandError(unresolvedName),
	}
	for _, family := range dispatchFamilies {
		cmd := commands.AppRegistry().FindCommand(family)
		if cmd == nil {
			t.Fatalf("family %q missing from the registry", family)
		}
		probes["rmp "+family+" "+unresolvedName] = cmd.DispatchFamily([]string{unresolvedName})
	}

	for label, err := range probes {
		if err == nil {
			t.Errorf("%s: expected an error, got nil", label)
			continue
		}
		if !errors.Is(err, utils.ErrUnknownCommand) {
			t.Errorf("%s: error = %v, want it to wrap utils.ErrUnknownCommand", label, err)
		}
		if errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("%s: error also wraps utils.ErrInvalidInput, which maps to exit 2; "+
				"a dispatch failure must be carried by ErrUnknownCommand alone (error: %v)", label, err)
		}
		var dispatch *commands.DispatchError
		if !errors.As(err, &dispatch) {
			t.Errorf("%s: error = %v, want a *commands.DispatchError so the error path can "+
				"render the recovery help", label, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Stdout silence
// ---------------------------------------------------------------------------

// TestDispatchFailure_StdoutStaysEmpty is the assertion that would have
// caught the original defect: `rmp nadadisto` wrote 2091 bytes of general
// help to stdout, so a consumer piping stdout to a JSON parser received a
// help screen on a failure (SPEC/HELP.md § Stdout silence on failure).
func TestDispatchFailure_StdoutStaysEmpty(t *testing.T) {
	if _, streams := probeCommandMiss(t); streams.stdout != "" {
		t.Errorf("`rmp %s` wrote %d bytes to stdout, want zero: %q",
			unresolvedName, len(streams.stdout), truncate(streams.stdout))
	}
	for _, family := range dispatchFamilies {
		if _, streams := probeSubcommandMiss(t, family); streams.stdout != "" {
			t.Errorf("`rmp %s %s` wrote %d bytes to stdout, want zero: %q",
				family, unresolvedName, len(streams.stdout), truncate(streams.stdout))
		}
	}
}

// ---------------------------------------------------------------------------
// Recovery help
// ---------------------------------------------------------------------------

// TestDispatchFailure_RecoveryHelpIsTheInvokedLevelsHelp requires the
// help written after the error to be the body of the level at which the
// name could not be resolved: the family body for an unresolved
// subcommand, the global body for an unresolved command.
//
// The family assertion compares against the body the family's own
// --help prints, captured from the same printer, so it cannot drift into
// asserting a hardcoded string that the help no longer contains.
func TestDispatchFailure_RecoveryHelpIsTheInvokedLevelsHelp(t *testing.T) {
	for _, family := range dispatchFamilies {
		cmd := commands.AppRegistry().FindCommand(family)
		if cmd == nil {
			t.Fatalf("family %q missing from the registry", family)
		}
		var want bytes.Buffer
		cmd.WriteHelpBody(&want)
		if want.Len() == 0 {
			t.Fatalf("family %q rendered an empty help body; the comparison below would be vacuous", family)
		}

		_, streams := probeSubcommandMiss(t, family)
		if !strings.Contains(streams.stderr, want.String()) {
			t.Errorf("`rmp %s %s`: stderr does not contain the %s family help body; stderr:\n%s",
				family, unresolvedName, family, truncate(streams.stderr))
		}
	}

	var wantGlobal bytes.Buffer
	writeGlobalHelpBody(&wantGlobal)
	_, streams := probeCommandMiss(t)
	if !strings.Contains(streams.stderr, wantGlobal.String()) {
		t.Errorf("`rmp %s`: stderr does not contain the global help body; stderr:\n%s",
			unresolvedName, truncate(streams.stderr))
	}
}

// TestDispatchFailure_RecoveryHelpCarriesNoAIBanner is the guard on the
// trap this change was most likely to fall into. Every --help body on
// stdout opens with the AI-agent banner, and the banner sentence is
// character-for-character the trailing hint. Reusing the stdout help
// path for the recovery help would emit that sentence twice on stderr —
// which is precisely the duplication the old `rmp nadadisto` produced
// across its two streams (SPEC/HELP.md § Recovery help after a dispatch
// failure).
func TestDispatchFailure_RecoveryHelpCarriesNoAIBanner(t *testing.T) {
	check := func(label, stderr string) {
		t.Helper()
		if n := strings.Count(stderr, commands.AIBannerLine); n != 1 {
			t.Errorf("%s: the AI-agent sentence appears %d times on stderr, want exactly 1; stderr:\n%s",
				label, n, truncate(stderr))
		}
		if strings.HasPrefix(stderr, commands.AIBannerLine) {
			t.Errorf("%s: stderr opens with the AI-agent banner; the recovery help must omit it "+
				"and the hint must stay last", label)
		}
	}
	_, streams := probeCommandMiss(t)
	check("rmp "+unresolvedName, streams.stderr)
	for _, family := range dispatchFamilies {
		_, s := probeSubcommandMiss(t, family)
		check("rmp "+family+" "+unresolvedName, s.stderr)
	}
}

// TestDispatchFailure_HintAppearsExactlyOnceUnderAIAgentEnv covers the
// other half of the deduplication rule. When AI_AGENT=1 is active, main()
// writes the hint at the top of stderr before dispatch; the trailing hint
// is then suppressed by EmitHintOnce's sync.Once. Adding the recovery
// help between them must not reintroduce a second copy
// (SPEC/HELP.md § Deduplication).
func TestDispatchFailure_HintAppearsExactlyOnceUnderAIAgentEnv(t *testing.T) {
	resetHintState()
	streams := captureStreams(t, func() {
		// Exactly what main() does when IsAIAgentEnvActive() reports true.
		aihelp.EmitHintOnce(os.Stderr, commands.AIBannerLine)
		cmd := commands.AppRegistry().FindCommand("task")
		handleError(cmd.DispatchFamily([]string{unresolvedName}))
	})
	if n := strings.Count(streams.stderr, commands.AIBannerLine); n != 1 {
		t.Errorf("AI_AGENT=1 dispatch failure: the hint appears %d times, want exactly 1; stderr:\n%s",
			n, truncate(streams.stderr))
	}
	if !strings.HasPrefix(streams.stderr, commands.AIBannerLine) {
		t.Error("AI_AGENT=1: the hint must be the first line of stderr (SPEC/HELP.md § Ordering)")
	}
	if streams.stdout != "" {
		t.Errorf("AI_AGENT=1 dispatch failure wrote %d bytes to stdout, want zero", len(streams.stdout))
	}
}

// TestDispatchFailure_NonDispatchErrorsGetNoHelp pins the narrow scope of
// the recovery help. A missing parameter, an unknown flag, an invalid
// value, a not-found and a database failure each produce the error line
// and the hint alone; appending help to every input error is explicitly
// not what the SPEC asks for (SPEC/HELP.md § Recovery help after a
// dispatch failure).
func TestDispatchFailure_NonDispatchErrorsGetNoHelp(t *testing.T) {
	for _, err := range []error{
		utils.ErrRequired, utils.ErrInvalidInput, utils.ErrValidation,
		utils.ErrNotFound, utils.ErrAlreadyExists, utils.ErrDatabase,
	} {
		resetHintState()
		streams := captureStreams(t, func() { handleError(err) })
		if strings.Contains(streams.stderr, "Usage:") {
			t.Errorf("%v: stderr carries a usage block; only a dispatch failure appends help; stderr:\n%s",
				err, truncate(streams.stderr))
		}
		wantLines := 3 // "Error: …", blank, hint, then EmitHintOnce's trailing newline.
		if got := strings.Count(streams.stderr, "\n"); got != wantLines+1 {
			t.Errorf("%v: stderr has %d newlines, want %d (error line, blank, hint); stderr: %q",
				err, got, wantLines+1, streams.stderr)
		}
	}
}

// TestDispatchFailure_LeafCommandsTakeNoDispatchPath guards the SPEC's
// statement that `stats`, `web` and `ai-help` accept no subcommand, so no
// dispatch failure can arise for them: a stray positional is an invalid
// argument to the command, not an unresolved name, and must not exit 127.
func TestDispatchFailure_LeafCommandsTakeNoDispatchPath(t *testing.T) {
	for _, family := range []string{"stats", "web"} {
		cmd := commands.AppRegistry().FindCommand(family)
		if cmd == nil {
			t.Fatalf("family %q missing from the registry", family)
		}
		if cmd.HasSubcommand {
			t.Errorf("%q dispatches subcommands; it is no longer a leaf command and this test is wrong", family)
			continue
		}
		err := cmd.DispatchFamily([]string{unresolvedName})
		if err == nil {
			t.Errorf("`rmp %s %s`: expected an error, got nil", family, unresolvedName)
			continue
		}
		if errors.Is(err, utils.ErrUnknownCommand) {
			t.Errorf("`rmp %s %s`: error = %v, want it NOT to be a dispatch failure; a command with "+
				"no subcommands cannot fail to resolve one", family, unresolvedName, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Stderr part order and wording
// ---------------------------------------------------------------------------

// TestDispatchFailure_StderrPartOrder pins the four-part layout of
// SPEC/HELP.md § Stderr part order for the dispatch-failure case: the
// error line, one blank line, the recovery help, one blank line, the
// hint. The hint stays last.
func TestDispatchFailure_StderrPartOrder(t *testing.T) {
	assertOrder := func(label, wantErrLine, wantHelpFirstLine, stderr string) {
		t.Helper()
		lines := strings.Split(stderr, "\n")
		if len(lines) < 5 {
			t.Errorf("%s: stderr has %d lines, too few to carry error + blank + help + blank + hint: %q",
				label, len(lines), stderr)
			return
		}
		if lines[0] != wantErrLine {
			t.Errorf("%s: stderr line 1 = %q, want %q", label, lines[0], wantErrLine)
		}
		if lines[1] != "" {
			t.Errorf("%s: stderr line 2 = %q, want a blank line between the error and the help",
				label, lines[1])
		}
		if lines[2] != wantHelpFirstLine {
			t.Errorf("%s: stderr line 3 = %q, want the first line of the recovery help %q",
				label, lines[2], wantHelpFirstLine)
		}
		// The hint is last: trailing blank lines aside, the final piece of
		// text on stderr is the hint, preceded by a blank line.
		trimmed := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
		last := len(trimmed) - 1
		if trimmed[last] != commands.AIBannerLine {
			t.Errorf("%s: last stderr line = %q, want the AI-agent hint", label, trimmed[last])
		}
		if last > 0 && trimmed[last-1] != "" {
			t.Errorf("%s: line before the hint = %q, want a blank line", label, trimmed[last-1])
		}
	}

	_, streams := probeCommandMiss(t)
	var globalHelp bytes.Buffer
	writeGlobalHelpBody(&globalHelp)
	assertOrder("rmp "+unresolvedName,
		"Error: unknown command: "+unresolvedName,
		firstLine(globalHelp.String()), streams.stderr)

	for _, family := range dispatchFamilies {
		cmd := commands.AppRegistry().FindCommand(family)
		var familyHelp bytes.Buffer
		cmd.WriteHelpBody(&familyHelp)
		_, s := probeSubcommandMiss(t, family)
		assertOrder("rmp "+family+" "+unresolvedName,
			"Error: unknown "+family+" subcommand: "+unresolvedName,
			firstLine(familyHelp.String()), s.stderr)
	}
}

// TestDispatchFailure_MessageIsLowercaseAndUnprefixed pins the wording.
// The two levels used to disagree — "Error: Unknown command:" against
// "Error: invalid input: unknown task subcommand:" — and the SPEC settles
// both on a lowercase "unknown …" with no sentinel prefix
// (SPEC/HELP.md § Error text of a dispatch failure).
func TestDispatchFailure_MessageIsLowercaseAndUnprefixed(t *testing.T) {
	check := func(label, want, stderr string) {
		t.Helper()
		got := firstLine(stderr)
		if got != "Error: "+want {
			t.Errorf("%s: error line = %q, want %q", label, got, "Error: "+want)
		}
		if strings.Contains(stderr, "invalid input:") {
			t.Errorf("%s: error line carries the ErrInvalidInput prefix, which misreports the "+
				"failure class: %q", label, got)
		}
		if strings.Contains(got, "Unknown") {
			t.Errorf("%s: error line is capitalised: %q", label, got)
		}
	}

	_, streams := probeCommandMiss(t)
	check("rmp "+unresolvedName, "unknown command: "+unresolvedName, streams.stderr)
	for _, family := range dispatchFamilies {
		_, s := probeSubcommandMiss(t, family)
		check("rmp "+family+" "+unresolvedName,
			"unknown "+family+" subcommand: "+unresolvedName, s.stderr)
	}
}

// ---------------------------------------------------------------------------
// Deliberate carve-out and unchanged help paths
// ---------------------------------------------------------------------------

// TestDispatchFailure_AIHelpScopeSelectorStaysExit2 protects the
// documented exception. A name preceding --ai-help is a scope selector
// for the contract emitter, not a name being dispatched, so an unusable
// selector is an invalid argument to --ai-help: exit 2, and no help
// (SPEC/COMMANDS.md § AI Help; SPEC/ARCHITECTURE.md § Failure modes).
func TestDispatchFailure_AIHelpScopeSelectorStaysExit2(t *testing.T) {
	cases := []struct {
		label string
		args  []string
	}{
		{"rmp " + unresolvedName + " --ai-help", []string{unresolvedName, "--ai-help"}},
		{"rmp task " + unresolvedName + " --ai-help", []string{"task", unresolvedName, "--ai-help"}},
	}
	for _, tc := range cases {
		resetHintState()
		var stdout, stderr bytes.Buffer
		handled, code := maybeHandleAIHelp(tc.args, &stdout, &stderr)
		if !handled {
			t.Errorf("%s: the AI-help wiring did not take the invocation", tc.label)
			continue
		}
		if code != ExitMisuse {
			t.Errorf("%s: exited %d, want %d (ExitMisuse); the scope-selector carve-out is deliberate "+
				"and must not follow the dispatch failure to 127", tc.label, code, ExitMisuse)
		}
		if stdout.Len() != 0 {
			t.Errorf("%s: wrote %d bytes to stdout, want zero", tc.label, stdout.Len())
		}
		if strings.Contains(stderr.String(), "Usage:") {
			t.Errorf("%s: stderr carries a usage block; the carve-out prints no help; stderr:\n%s",
				tc.label, truncate(stderr.String()))
		}
	}
}

// TestDispatchFailure_HelpRequestsRemainOnStdout guards what must NOT
// have moved. Help the reader asked for is not a failure: it exits 0 and
// keeps its body on stdout, banner and all
// (SPEC/HELP.md § Stdout silence on failure).
func TestDispatchFailure_HelpRequestsRemainOnStdout(t *testing.T) {
	// `rmp` with no arguments, and `rmp --help`, both route to printHelp.
	streams := captureStreams(t, printHelp)
	if !strings.HasPrefix(streams.stdout, commands.AIBannerLine) {
		t.Errorf("global help must still open with the AI-agent banner on stdout; got:\n%s",
			truncate(streams.stdout))
	}
	if !strings.Contains(streams.stdout, "Usage: rmp [command] [subcommand] [arguments] [options]") {
		t.Errorf("global help lost its usage line; stdout:\n%s", truncate(streams.stdout))
	}
	if streams.stderr != "" {
		t.Errorf("global help wrote %d bytes to stderr, want zero: %q",
			len(streams.stderr), truncate(streams.stderr))
	}

	// `rmp <family>` and `rmp <family> --help` both route to the family
	// help printer, on stdout, with no error.
	for _, family := range dispatchFamilies {
		cmd := commands.AppRegistry().FindCommand(family)
		for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"help"}} {
			var err error
			s := captureStreams(t, func() { err = cmd.DispatchFamily(args) })
			label := "rmp " + family + " " + strings.Join(args, " ")
			if err != nil {
				t.Errorf("%s: returned %v, want nil", label, err)
			}
			if !strings.HasPrefix(s.stdout, commands.AIBannerLine) {
				t.Errorf("%s: family help must open with the AI-agent banner on stdout; got:\n%s",
					label, truncate(s.stdout))
			}
			if s.stderr != "" {
				t.Errorf("%s: wrote %d bytes to stderr, want zero: %q",
					label, len(s.stderr), truncate(s.stderr))
			}
		}
	}
}

// TestDispatchFailure_HelpBodyRedirectionIsRestored guards the mechanism
// itself. WriteHelpBodyTo redirects the package-level help destination
// for the duration of one printer call; if it failed to restore it, every
// later `--help` in the same process would silently write to a stale
// writer instead of stdout.
func TestDispatchFailure_HelpBodyRedirectionIsRestored(t *testing.T) {
	cmd := commands.AppRegistry().FindCommand("task")

	var diverted bytes.Buffer
	cmd.WriteHelpBody(&diverted)
	if diverted.Len() == 0 {
		t.Fatal("WriteHelpBody wrote nothing; the restoration check below would be vacuous")
	}

	streams := captureStreams(t, func() { _ = cmd.DispatchFamily([]string{"--help"}) })
	if streams.stdout == "" {
		t.Error("after a redirected help body, the next --help wrote nothing to stdout; " +
			"WriteHelpBodyTo did not restore the destination")
	}
}

// firstLine returns s up to its first newline.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// truncate shortens a captured stream for a failure message, so a 10 KB
// help body does not bury the assertion that failed.
func truncate(s string) string {
	const max = 400
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n… (truncated)"
}
