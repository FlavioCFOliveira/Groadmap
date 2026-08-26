package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/aihelp"
	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// cmd/rmp — regression suite for the suppression of stderr part 4.
//
// SPEC/HELP.md § Stderr part order defines part 4 as "One blank line,
// then the AI-agent hint line", present "unless a suppression rule
// below applies". The blank line and the hint are one part, so every
// suppression rule must remove both.
//
// The defect. The separator was written by writeFailureReport itself,
// immediately before calling the emitter, and the emitter was then a
// no-op under the deduplication rule. Every failing invocation under
// AI_AGENT=1 therefore ended with a blank line that introduced
// nothing:
//
//	AI agents: run `rmp --ai-help` for a machine-readable command contract.
//
//	Error: resource not found: roadmap "mobile-checkout"
//	                                                      <- orphan
//
// It is cosmetic for a human and not cosmetic for the agent the whole
// AI_AGENT mechanism exists to serve: an agent splitting stderr into
// the four documented parts finds a fifth, empty one the contract does
// not explain.
//
// What these tests assert. The EXACT final bytes of stderr, not the
// absence of a substring. The orphan carries no text of its own, so
// only a byte-level assertion can see it — which is why the defect
// survived a suite that already counted hint occurrences, pinned the
// part order, and checked that the hint came last.
//
// Coverage. Both suppression rules, on both stderr layouts:
//
//	deduplication (AI_AGENT=1)  x  plain error / dispatch failure
//	contract already emitted    x  plain error / dispatch failure
//
// plus the unsuppressed control, so that deleting the separator
// outright fails here too rather than turning these tests green.

// hintBlock is what an emitted hint contributes to stderr: the hint
// line, its newline, and the blank line that closes the block.
const hintBlock = commands.AIBannerLine + "\n\n"

// notFoundError is a realistic failure from the not-found class: the
// error `rmp task get -r mobile-checkout 1` raises when the roadmap
// does not exist. Built here rather than probed through the database
// so the test owns the exact expected bytes of the error line.
func notFoundError() error {
	return fmt.Errorf("%w: roadmap %q", utils.ErrNotFound, "mobile-checkout")
}

const notFoundErrorLine = "Error: resource not found: roadmap \"mobile-checkout\"\n"

// emitEnvHint reproduces exactly what main() does at process entry
// when aihelp.IsAIAgentEnvActive() reports true: the leading hint goes
// out before anything else, which spends the process's single
// emission and arms the deduplication rule for the error path.
func emitEnvHint() {
	aihelp.EmitHintOnce(os.Stderr, commands.AIBannerLine)
}

// markContractEmitted flips aihelp.WasInvoked() through the production
// seam — a successful contract emission — rather than through a
// testing back door, so the state under test is the one a real
// `rmp --ai-help` produces. Output is discarded; only the sentinel
// matters here.
func markContractEmitted(t *testing.T) {
	t.Helper()
	if handled, code := maybeHandleAIHelp([]string{"--ai-help"}, io.Discard, io.Discard); !handled || code != ExitSuccess {
		t.Fatalf("contract emission did not succeed: handled=%v code=%d", handled, code)
	}
	if !aihelp.WasInvoked() {
		t.Fatal("aihelp.WasInvoked() is false after a successful contract emission")
	}
}

// assertNoOrphanSeparator is the assertion the defect defeats. stderr
// must end with the last part actually written, and never on the blank
// line that a suppressed part 4 would have been introduced by.
func assertNoOrphanSeparator(t *testing.T, label, stderr string) {
	t.Helper()
	if stderr == "" {
		t.Errorf("%s: stderr is empty; the error line is never suppressed", label)
		return
	}
	if strings.HasSuffix(stderr, "\n\n") {
		t.Errorf("%s: stderr ends on a blank line. Part 4 is suppressed here, so its separating "+
			"blank line must be suppressed with it (SPEC/HELP.md § Stderr part order). "+
			"Final 12 bytes: %q", label, tailBytes(stderr, 12))
	}
	if !strings.HasSuffix(stderr, "\n") {
		t.Errorf("%s: stderr does not end with a newline; final 12 bytes: %q",
			label, tailBytes(stderr, 12))
	}
	if strings.Count(stderr, commands.AIBannerLine) != 1 {
		t.Errorf("%s: the hint sentence appears %d times, want exactly 1 (the leading one)",
			label, strings.Count(stderr, commands.AIBannerLine))
	}
}

// tailBytes returns the last n bytes of s, for error messages that
// need to show what the assertion actually saw.
func tailBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// ---------------------------------------------------------------------------
// Rule 1: deduplication under AI_AGENT=1
// ---------------------------------------------------------------------------

// TestHintSuppression_DedupPlainErrorEndsAtTheErrorLine pins the whole
// of stderr, byte for byte, for the simplest failing invocation under
// AI_AGENT=1. Every byte is known in advance here, so the assertion is
// equality rather than a suffix check: the leading hint block, then the
// error line, then nothing.
func TestHintSuppression_DedupPlainErrorEndsAtTheErrorLine(t *testing.T) {
	resetHintState()
	streams := captureStreams(t, func() {
		emitEnvHint()
		handleError(notFoundError())
	})

	want := hintBlock + notFoundErrorLine
	if streams.stderr != want {
		t.Errorf("AI_AGENT=1 + not-found:\n got %q\nwant %q", streams.stderr, want)
	}
	assertNoOrphanSeparator(t, "AI_AGENT=1 + not-found", streams.stderr)
	if streams.stdout != "" {
		t.Errorf("failing invocation wrote %d bytes to stdout, want zero", len(streams.stdout))
	}
}

// TestHintSuppression_DedupUnresolvedCommandEndsAtTheRecoveryHelp
// covers the layout the defect was found on: a dispatch failure writes
// part 3 (a blank line and the recovery help) between the error line
// and the suppressed part 4, so the last thing on stderr is the help
// body's own final newline.
//
// The expectation is built from writeGlobalHelpBody, the same function
// main() uses for `rmp --help`, so the test cannot drift when the help
// text changes — while still asserting the exact final bytes, because
// stderr must end with that body in full and nothing after it.
func TestHintSuppression_DedupUnresolvedCommandEndsAtTheRecoveryHelp(t *testing.T) {
	if commands.AppRegistry().FindCommand(unresolvedName) != nil {
		t.Fatalf("%q resolves to a command; this test no longer models a dispatch failure", unresolvedName)
	}
	resetHintState()
	streams := captureStreams(t, func() {
		emitEnvHint()
		handleError(commands.NewUnknownCommandError(unresolvedName))
	})

	var help bytes.Buffer
	writeGlobalHelpBody(&help)
	want := hintBlock + "Error: unknown command: " + unresolvedName + "\n\n" + help.String()
	if streams.stderr != want {
		t.Errorf("AI_AGENT=1 + unresolved command: stderr is not the four documented parts minus "+
			"the suppressed one.\n final 24 bytes got:  %q\n final 24 bytes want: %q",
			tailBytes(streams.stderr, 24), tailBytes(want, 24))
	}
	assertNoOrphanSeparator(t, "AI_AGENT=1 + unresolved command", streams.stderr)
}

// TestHintSuppression_DedupUnresolvedSubcommandEndsAtTheRecoveryHelp
// repeats the check for every command family that dispatches
// subcommands, rather than a sample: the recovery help for each is a
// different body, and the assertion is on where each body ends.
func TestHintSuppression_DedupUnresolvedSubcommandEndsAtTheRecoveryHelp(t *testing.T) {
	for _, family := range dispatchFamilies {
		cmd := commands.AppRegistry().FindCommand(family)
		if cmd == nil {
			t.Fatalf("family %q missing from the registry", family)
		}
		resetHintState()
		streams := captureStreams(t, func() {
			emitEnvHint()
			handleError(cmd.DispatchFamily([]string{unresolvedName}))
		})

		var help bytes.Buffer
		cmd.WriteHelpBody(&help)
		label := "AI_AGENT=1 + `rmp " + family + " " + unresolvedName + "`"
		if !strings.HasSuffix(streams.stderr, help.String()) {
			t.Errorf("%s: stderr does not end with the family recovery help.\n final 24 bytes: %q",
				label, tailBytes(streams.stderr, 24))
		}
		assertNoOrphanSeparator(t, label, streams.stderr)
	}
}

// ---------------------------------------------------------------------------
// Rule 2: the contract was already emitted
// ---------------------------------------------------------------------------

// TestHintSuppression_ContractEmittedPlainErrorEndsAtTheErrorLine
// covers the other suppression rule at the writeFailureReport seam.
// This rule returns before writing anything of part 4, so it was
// already free of the orphan — the test locks that in, because the
// obvious wrong fix for the deduplication rule is a second condition
// around the separator, and a second condition is exactly what drifts
// out of step with this one.
func TestHintSuppression_ContractEmittedPlainErrorEndsAtTheErrorLine(t *testing.T) {
	resetHintState()
	markContractEmitted(t)
	streams := captureStreams(t, func() { handleError(notFoundError()) })

	if streams.stderr != notFoundErrorLine {
		t.Errorf("contract already emitted + not-found:\n got %q\nwant %q",
			streams.stderr, notFoundErrorLine)
	}
	if strings.Contains(streams.stderr, commands.AIBannerLine) {
		t.Error("the hint was emitted although the contract had already been delivered")
	}
}

// TestHintSuppression_ContractEmittedInTheAIHelpWiring covers the third
// call site, writeAIHelpError. It is the one place the contract-already
// -emitted rule can actually fire in production (a stdout write failure
// after Generate has succeeded), and it carried the same
// separator-then-emitter shape as writeFailureReport.
func TestHintSuppression_ContractEmittedInTheAIHelpWiring(t *testing.T) {
	resetHintState()
	markContractEmitted(t)

	var stderr bytes.Buffer
	writeAIHelpError(&stderr, "write contract to stdout: write /dev/stdout: no space left on device")

	want := "Error: write contract to stdout: write /dev/stdout: no space left on device\n"
	if stderr.String() != want {
		t.Errorf("writeAIHelpError with the contract already emitted:\n got %q\nwant %q",
			stderr.String(), want)
	}
}

// TestHintSuppression_DedupInTheAIHelpWiring covers writeAIHelpError
// under the OTHER rule: the leading hint has already gone out, so the
// emitter is a no-op and must contribute nothing at all.
func TestHintSuppression_DedupInTheAIHelpWiring(t *testing.T) {
	resetHintState()
	var leading bytes.Buffer
	aihelp.EmitHintOnce(&leading, commands.AIBannerLine)

	var stderr bytes.Buffer
	writeAIHelpError(&stderr, "unknown command: "+unresolvedName)

	want := "Error: unknown command: " + unresolvedName + "\n"
	if stderr.String() != want {
		t.Errorf("writeAIHelpError under the deduplication rule:\n got %q\nwant %q",
			stderr.String(), want)
	}
}

// ---------------------------------------------------------------------------
// The unsuppressed control
// ---------------------------------------------------------------------------

// TestHintSuppression_UnsuppressedKeepsTheSeparator is the other half
// of the guard. Moving the separator into the emitter must not lose it:
// when no suppression rule applies, part 4 is still one blank line
// followed by the hint.
//
// Without this test, deleting the separator entirely would satisfy
// every assertion above.
func TestHintSuppression_UnsuppressedKeepsTheSeparator(t *testing.T) {
	resetHintState()
	streams := captureStreams(t, func() { handleError(notFoundError()) })

	want := notFoundErrorLine + "\n" + hintBlock
	if streams.stderr != want {
		t.Errorf("no suppression rule applies:\n got %q\nwant %q", streams.stderr, want)
	}
}

// TestHintSuppression_UnsuppressedDispatchFailureKeepsTheSeparator is
// the same control on the dispatch-failure layout, where part 3 sits
// between the error line and part 4.
func TestHintSuppression_UnsuppressedDispatchFailureKeepsTheSeparator(t *testing.T) {
	resetHintState()
	streams := captureStreams(t, func() {
		handleError(commands.NewUnknownCommandError(unresolvedName))
	})

	var help bytes.Buffer
	writeGlobalHelpBody(&help)
	want := "Error: unknown command: " + unresolvedName + "\n\n" + help.String() + "\n" + hintBlock
	if streams.stderr != want {
		t.Errorf("no suppression rule applies (dispatch failure):\n final 24 bytes got:  %q\n"+
			" final 24 bytes want: %q", tailBytes(streams.stderr, 24), tailBytes(want, 24))
	}
}
