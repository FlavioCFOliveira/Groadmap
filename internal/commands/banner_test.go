// Package commands — tests for the SPEC-mandated AI-agent banner.
//
// These tests enforce the contract in SPEC/HELP.md § AI agent banner:
//
//  1. Every plain-text help printer reachable through the registry
//     dispatch path emits the banner as its first line, followed by
//     exactly one blank line, before the existing help body.
//  2. The banner string is the verbatim SPEC literal (backticks
//     included, no surrounding decoration).
//  3. The banner is NOT emitted on the AI Agent Contract path (that
//     path bypasses the registry dispatch entirely; this file does
//     not exercise it — the contract path's no-banner property is
//     covered by the E2E suite that runs the binary).
//
// The walk uses the registry as the single source of truth for the
// list of help paths. Adding a new subcommand automatically extends
// the test coverage; no second list of names exists.

package commands

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout during fn, returning the bytes
// written. Mirrors the helper in integration_test.go but kept local to
// avoid coupling this test file to that file's test-ordering.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	os.Stdout = old
	<-done
	return buf.String()
}

// TestAIBanner_StringMatchesSpec is a guard against accidental edits
// to the banner literal. Any drift from the SPEC text would mean LLM
// agents looking for the documented banner string would miss it.
func TestAIBanner_StringMatchesSpec(t *testing.T) {
	const wantSpecLiteral = "AI agents: run `rmp --ai-help` for a machine-readable command contract."
	if AIBannerLine != wantSpecLiteral {
		t.Fatalf("AIBannerLine drifted from SPEC/HELP.md § AI agent banner:\n  got:  %q\n  want: %q", AIBannerLine, wantSpecLiteral)
	}
}

// TestAIBanner_WriteAIBannerShape asserts the exact byte layout:
// banner line, newline, blank line, newline. No extra whitespace,
// no missing terminator.
func TestAIBanner_WriteAIBannerShape(t *testing.T) {
	var buf bytes.Buffer
	WriteAIBanner(&buf)

	want := AIBannerLine + "\n\n"
	if got := buf.String(); got != want {
		t.Fatalf("WriteAIBanner output mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestAIBanner_EveryFamilyHelpStartsWithBanner walks every top-level
// command family in the registry and asserts that its family-level
// HelpPrinter, when invoked through the dispatch path (`<family>
// --help`), emits the banner as the first line followed by a blank
// line. The ai-help family is exempt: its --help token is intercepted
// upstream by the contract emitter (see cmd/rmp/aihelp_wiring.go) and
// never reaches DispatchFamily.
func TestAIBanner_EveryFamilyHelpStartsWithBanner(t *testing.T) {
	reg := AppRegistry()
	for i := range reg.Commands {
		cmd := &reg.Commands[i]
		if cmd.Name == "ai-help" {
			continue // intercepted upstream; covered by E2E.
		}
		t.Run(cmd.Name, func(t *testing.T) {
			if cmd.HelpPrinter == nil && !cmd.HasSubcommand {
				// Leaf commands route --help through their own Handler;
				// skip the family-level invocation and let the
				// per-subcommand test cover them.
				return
			}
			out := captureStdout(t, func() {
				// `rmp <family> --help` path: DispatchFamily receives
				// ["--help"] and routes to the family HelpPrinter.
				if err := cmd.DispatchFamily([]string{"--help"}); err != nil {
					t.Fatalf("DispatchFamily(--help) returned error: %v", err)
				}
			})
			assertBannerPrefix(t, out, "rmp "+cmd.Name+" --help")
		})
	}
}

// TestAIBanner_EverySubcommandHelpStartsWithBanner is the
// comprehensive coverage test: for every (family, subcommand) pair in
// the registry, invoke the `<family> <subcommand> --help` dispatch
// path and assert the banner prefix. This is the contract guard for
// SPEC/COMMANDS.md § AI Help "Discoverability requirements" rule 1.
func TestAIBanner_EverySubcommandHelpStartsWithBanner(t *testing.T) {
	reg := AppRegistry()
	for i := range reg.Commands {
		cmd := &reg.Commands[i]
		if cmd.Name == "ai-help" {
			continue // intercepted upstream.
		}
		if !cmd.HasSubcommand {
			// Leaf command (e.g. stats): exercise the leaf handler's
			// own --help shortcut. The leaf's Handler is the only
			// Subcommand entry's Handler.
			if len(cmd.Subcommands) != 1 {
				t.Fatalf("leaf command %q must have exactly one Subcommands entry", cmd.Name)
			}
			t.Run(cmd.Name+"/--help", func(t *testing.T) {
				out := captureStdout(t, func() {
					if err := cmd.Subcommands[0].Handler([]string{"--help"}); err != nil {
						t.Fatalf("Handler(--help) returned error: %v", err)
					}
				})
				assertBannerPrefix(t, out, "rmp "+cmd.Name+" --help")
			})
			continue
		}
		for j := range cmd.Subcommands {
			sub := &cmd.Subcommands[j]
			if sub.Name == "" {
				continue
			}
			label := cmd.Name + "/" + sub.Name
			t.Run(label, func(t *testing.T) {
				out := captureStdout(t, func() {
					if err := cmd.DispatchFamily([]string{sub.Name, "--help"}); err != nil {
						t.Fatalf("DispatchFamily(%s --help) returned error: %v", sub.Name, err)
					}
				})
				assertBannerPrefix(t, out, "rmp "+cmd.Name+" "+sub.Name+" --help")
			})
		}
	}
}

// assertBannerPrefix verifies that out starts with the banner line,
// followed by a blank line, followed by at least one non-empty body
// line. The label is the human-readable invocation form used in the
// failure message so test output points directly at the broken path.
func assertBannerPrefix(t *testing.T, out, label string) {
	t.Helper()
	lines := strings.SplitN(out, "\n", 4)
	if len(lines) < 3 {
		t.Fatalf("%s: output has fewer than 3 lines, got %d:\n%s", label, len(lines), out)
	}
	if lines[0] != AIBannerLine {
		t.Errorf("%s: first line does not match SPEC banner\n  got:  %q\n  want: %q", label, lines[0], AIBannerLine)
	}
	if lines[1] != "" {
		t.Errorf("%s: second line must be empty (blank line after banner), got %q", label, lines[1])
	}
	if len(lines) >= 3 && lines[2] == "" {
		t.Errorf("%s: third line must start the help body, got empty (would be two blank lines after banner)", label)
	}
}
