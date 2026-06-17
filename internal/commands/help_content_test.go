// Package commands — help content structural tests.
//
// These tests lock in the structural requirements for every plain-text
// help output mandated by the recent help/contract review (commits
// e901bbf, 8290fd0, 83ee2e6, 88136b6). They complement the banner
// tests in banner_test.go and the registry tests in registry_test.go.
//
// Coverage:
//  1. Every help output contains an "Exit codes:" or "Exit Codes:" block
//     that includes code 0.
//  2. sprint create --help and sprint update --help document --title,
//     --description, and --order (with >0 and CLOSED-immutable rules).
//  3. The sprint family --help documents exit code 5.
//  4. sprint tasks --help documents the -s, --status short form.
//  5. Graph subcommand helps (create/query/update/delete/search) each
//     contain an "Output (stdout JSON):" block and the -q, --query short form.
//  6. No hard TAB characters appear in any help output.
//  7. Regression: task stat with an unrecognised status wraps ErrValidation
//     (exit 6), not a bare ErrInvalidTaskStatus (exit 1).
package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// allHelpOutputs walks the entire registry and collects the help output
// for every family and every subcommand, returning a slice of
// (label, output) pairs. The ai-help family is skipped because its
// --help is intercepted upstream by the contract emitter.
func allHelpOutputs(t *testing.T) []struct{ label, out string } {
	t.Helper()
	reg := AppRegistry()
	var pairs []struct{ label, out string }

	for i := range reg.Commands {
		cmd := &reg.Commands[i]
		if cmd.Name == "ai-help" {
			continue
		}

		// Family-level help (when a family HelpPrinter exists or it is a leaf).
		if cmd.HelpPrinter != nil || !cmd.HasSubcommand {
			out := captureStdout(t, func() {
				_ = cmd.DispatchFamily([]string{"--help"})
			})
			pairs = append(pairs, struct{ label, out string }{
				label: "rmp " + cmd.Name + " --help",
				out:   out,
			})
		}

		// Per-subcommand help.
		for j := range cmd.Subcommands {
			sub := &cmd.Subcommands[j]
			if sub.Name == "" {
				continue
			}
			subName := sub.Name
			label := "rmp " + cmd.Name + " " + subName + " --help"
			out := captureStdout(t, func() {
				_ = cmd.DispatchFamily([]string{subName, "--help"})
			})
			pairs = append(pairs, struct{ label, out string }{label: label, out: out})
		}
	}
	return pairs
}

// ---------------------------------------------------------------------------
// 1. Every help output contains an "Exit codes:" block mentioning code 0.
// ---------------------------------------------------------------------------

// TestHelpContent_EveryOutputContainsExitCodesBlock verifies that every
// help printer emits an exit-codes section (case-insensitive) and that
// code 0 appears in the text below the section heading. This pins the
// SPEC/HELP.md requirement that exit codes are always documented.
func TestHelpContent_EveryOutputContainsExitCodesBlock(t *testing.T) {
	for _, pair := range allHelpOutputs(t) {
		lower := strings.ToLower(pair.out)
		hasBlock := strings.Contains(lower, "exit code") || strings.Contains(lower, "exit codes")
		if !hasBlock {
			t.Errorf("%s: missing 'Exit codes:' / 'Exit Codes:' block in help output", pair.label)
			continue
		}
		// Find the section and check that "0" appears after it.
		idx := strings.Index(lower, "exit code")
		tail := pair.out[idx:]
		if !strings.Contains(tail, "0") {
			t.Errorf("%s: exit-codes block does not mention code 0", pair.label)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Sprint create/update help documents --title, --description, --order.
// ---------------------------------------------------------------------------

// TestHelpContent_SprintCreateDocumentsRequiredFlags checks that the
// sprint create help printer documents --title, --description, and --order
// including the positive-integer (>0) constraint.
func TestHelpContent_SprintCreateDocumentsRequiredFlags(t *testing.T) {
	reg := AppRegistry()
	sprintCmd := reg.FindCommand("sprint")
	if sprintCmd == nil {
		t.Fatal("sprint command missing from registry")
	}

	out := captureStdout(t, func() {
		_ = sprintCmd.DispatchFamily([]string{"create", "--help"})
	})

	for _, want := range []string{"--title", "--description", "--order"} {
		if !strings.Contains(out, want) {
			t.Errorf("sprint create --help: missing flag %q", want)
		}
	}

	// --order rules: positive integer ("> 0" or "positive").
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "> 0") && !strings.Contains(lower, "positive") {
		t.Errorf("sprint create --help: --order must document the >0 constraint")
	}
}

// TestHelpContent_SprintUpdateDocumentsRequiredFlags checks that the
// sprint update help printer documents --title, --description, and --order
// including the immutability rule for CLOSED sprints.
func TestHelpContent_SprintUpdateDocumentsRequiredFlags(t *testing.T) {
	reg := AppRegistry()
	sprintCmd := reg.FindCommand("sprint")
	if sprintCmd == nil {
		t.Fatal("sprint command missing from registry")
	}

	out := captureStdout(t, func() {
		_ = sprintCmd.DispatchFamily([]string{"update", "--help"})
	})

	for _, want := range []string{"--title", "--description", "--order"} {
		if !strings.Contains(out, want) {
			t.Errorf("sprint update --help: missing flag %q", want)
		}
	}

	lower := strings.ToLower(out)
	// Must mention immutability once CLOSED.
	if !strings.Contains(lower, "closed") || !strings.Contains(lower, "immutable") {
		t.Errorf("sprint update --help: must document --order CLOSED-immutable rule (missing 'CLOSED' or 'immutable')")
	}
	// Must mention the >0 / positive constraint.
	if !strings.Contains(lower, "> 0") && !strings.Contains(lower, "positive") {
		t.Errorf("sprint update --help: --order must document the >0 constraint")
	}
}

// ---------------------------------------------------------------------------
// 3. Sprint family --help documents exit code 5.
// ---------------------------------------------------------------------------

// TestHelpContent_SprintFamilyDocumentsExitCode5 verifies that the sprint
// family-level help printer mentions exit code 5 (order collision /
// ErrAlreadyExists) so agents know the full exit-code surface before
// drilling into per-subcommand help.
func TestHelpContent_SprintFamilyDocumentsExitCode5(t *testing.T) {
	reg := AppRegistry()
	sprintCmd := reg.FindCommand("sprint")
	if sprintCmd == nil {
		t.Fatal("sprint command missing from registry")
	}

	out := captureStdout(t, func() {
		_ = sprintCmd.DispatchFamily([]string{"--help"})
	})

	lower := strings.ToLower(out)
	hasExitFive := strings.Contains(lower, "exit 5") ||
		strings.Contains(lower, "exit code 5") ||
		strings.Contains(lower, "rejected exit 5")
	if !hasExitFive {
		t.Errorf("sprint --help: must document exit code 5 (order collision / already-exists); got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// 4. sprint tasks --help documents -s, --status short form.
// ---------------------------------------------------------------------------

// TestHelpContent_SprintTasksDocumentsStatusShortForm verifies that the
// sprint tasks subcommand help documents the short form -s for --status.
func TestHelpContent_SprintTasksDocumentsStatusShortForm(t *testing.T) {
	reg := AppRegistry()
	sprintCmd := reg.FindCommand("sprint")
	if sprintCmd == nil {
		t.Fatal("sprint command missing from registry")
	}

	out := captureStdout(t, func() {
		_ = sprintCmd.DispatchFamily([]string{"tasks", "--help"})
	})

	if !strings.Contains(out, "-s") {
		t.Errorf("sprint tasks --help: missing -s short form for --status")
	}
	if !strings.Contains(out, "--status") {
		t.Errorf("sprint tasks --help: missing --status flag")
	}
}

// ---------------------------------------------------------------------------
// 5. Graph subcommand helps contain Output block and -q short form.
// ---------------------------------------------------------------------------

// TestHelpContent_GraphSubcommandsOutputAndQueryShortForm verifies that
// every graph subcommand help contains an "Output (stdout JSON):" block
// and documents the -q short form of --query.
func TestHelpContent_GraphSubcommandsOutputAndQueryShortForm(t *testing.T) {
	reg := AppRegistry()
	graphCmd := reg.FindCommand("graph")
	if graphCmd == nil {
		t.Fatal("graph command missing from registry")
	}

	for _, subName := range []string{"create", "query", "update", "delete", "search"} {
		subName := subName
		t.Run(subName, func(t *testing.T) {
			out := captureStdout(t, func() {
				_ = graphCmd.DispatchFamily([]string{subName, "--help"})
			})

			lower := strings.ToLower(out)
			if !strings.Contains(lower, "output (stdout json)") {
				t.Errorf("graph %s --help: missing 'Output (stdout JSON):' block", subName)
			}
			if !strings.Contains(out, "-q") {
				t.Errorf("graph %s --help: missing -q short form for --query", subName)
			}
			if !strings.Contains(out, "--query") {
				t.Errorf("graph %s --help: missing --query flag", subName)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 6. No hard TAB characters in any help output.
// ---------------------------------------------------------------------------

// TestHelpContent_NoHardTabsInAnyHelpOutput scans every help output for
// hard TAB characters (\t). Backlog and stats were previously offenders.
// Tabs break terminal alignment and violate SPEC formatting rules.
func TestHelpContent_NoHardTabsInAnyHelpOutput(t *testing.T) {
	for _, pair := range allHelpOutputs(t) {
		if strings.Contains(pair.out, "\t") {
			t.Errorf("%s: contains hard TAB character (\\t); use spaces for alignment", pair.label)
		}
	}
}

// ---------------------------------------------------------------------------
// 7. Regression: task stat invalid status must wrap ErrValidation (exit 6).
// ---------------------------------------------------------------------------

// TestHelpContent_TaskStatInvalidStatusWrapsErrValidation is a unit-level
// regression guard for the bug where ParseTaskStatus returned
// ErrInvalidTaskStatus without wrapping utils.ErrValidation, causing
// handleError to map it to exit 1 instead of the mandated exit 6 per
// SPEC/ARCHITECTURE.md § Exit Codes and the task stat help text.
//
// Fix location: internal/commands/task_mutate.go — the error returned by
// models.ParseTaskStatus is wrapped as:
//
//	fmt.Errorf("%w: %w", utils.ErrValidation, err)
//
// before being returned to the caller.
func TestHelpContent_TaskStatInvalidStatusWrapsErrValidation(t *testing.T) {
	const testRoadmap = "test-stat-invalid-status-regression"
	_, cleanup := setupTestTaskRoadmap(t, testRoadmap)
	defer cleanup()

	// Seed a task so the ID lookup does not short-circuit before status parsing.
	if err := taskCreate([]string{
		"-r", testRoadmap,
		"-t", "Regression guard: invalid status exit code",
		"-fr", "task stat with an unrecognised status value must exit 6",
		"-tr", "ParseTaskStatus error must be wrapped with utils.ErrValidation",
		"-ac", "handleError maps the error to EXIT_INVALID_DATA (6)",
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	err := HandleTask([]string{"stat", "-r", testRoadmap, "1", "DEFINITELY_NOT_A_STATUS"})
	if err == nil {
		t.Fatal("task stat with invalid status must return an error, got nil")
	}
	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf(
			"error from task stat with invalid status must wrap utils.ErrValidation "+
				"(exit 6 / EXIT_INVALID_DATA); got error that does NOT wrap it: %v",
			err,
		)
	}
}
