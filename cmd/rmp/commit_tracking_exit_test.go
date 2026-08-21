package main

import (
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/aihelp"
	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
)

// TestCommitTracking_RejectionExitCodesAndMessages is the end-to-end half of the
// commit-tracking error table in SPEC/COMMANDS.md § Change Status (stat).
//
// internal/commands can only assert which sentinel an error carries; the
// sentinel-to-code mapping and the "Error: " rendering both live here, in
// handleError and printError. This test closes that gap by running the real
// registry dispatch main() runs, feeding the resulting error to the real
// handleError, and comparing the literal exit code and the literal first line of
// stderr against the SPEC's table. Two rows of that table are one digit apart on
// purpose — a flag written with no value after it is exit 2, an absent flag is
// exit 6 — and only a test that observes the number can tell them apart.
//
// The roadmap named here does not exist, which is deliberate: every case below
// is a step-1-to-4 rejection, and SPEC/COMMANDS.md makes those steps run before
// the database is opened. The control at the end proves the point — the same
// command line with a valid hash gets as far as the roadmap lookup and fails
// with exit 4 — so none of the assertions above it can be passing merely because
// the roadmap is missing.
func TestCommitTracking_RejectionExitCodesAndMessages(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	taskCmd := commands.AppRegistry().FindCommand("task")
	if taskCmd == nil {
		t.Fatal("task command missing from registry")
	}

	run := func(args ...string) (int, string) {
		t.Helper()
		aihelp.ResetHintForTesting()
		var code int
		stderr := captureStderrForExitTest(t, func() {
			code = handleError(taskCmd.DispatchFamily(args))
		})
		first, _, _ := strings.Cut(stderr, "\n")
		return code, first
	}

	for _, tc := range []struct {
		name     string
		args     []string
		wantLine string
		wantCode int
	}{
		{
			name:     "DOING without --commit-open",
			args:     []string{"stat", "-r", "ledger-service", "41", "DOING"},
			wantLine: "Error: --commit-open is required when transitioning to DOING",
			wantCode: ExitInvalidData,
		},
		{
			name:     "COMPLETED without --commit-close",
			args:     []string{"stat", "-r", "ledger-service", "41", "COMPLETED"},
			wantLine: "Error: --commit-close is required when transitioning to COMPLETED",
			wantCode: ExitInvalidData,
		},
		{
			name:     "--commit-open on a target other than DOING",
			args:     []string{"stat", "-r", "ledger-service", "41", "TESTING", "--commit-open", "5f93b51"},
			wantLine: "Error: --commit-open flag is only allowed when transitioning to DOING",
			wantCode: ExitInvalidData,
		},
		{
			name:     "--commit-close on a target other than COMPLETED",
			args:     []string{"stat", "-r", "ledger-service", "41", "DOING", "--commit-close", "2578d18"},
			wantLine: "Error: --commit-close flag is only allowed when transitioning to COMPLETED",
			wantCode: ExitInvalidData,
		},
		{
			name:     "malformed --commit-open value",
			args:     []string{"stat", "-r", "ledger-service", "41", "DOING", "--commit-open", "not-a-hash"},
			wantLine: `Error: invalid commit hash for --commit-open: "not-a-hash" (expected 7 to 64 hexadecimal characters)`,
			wantCode: ExitInvalidData,
		},
		{
			name:     "malformed --commit-close value",
			args:     []string{"stat", "-r", "ledger-service", "41", "COMPLETED", "--commit-close", "abcdef"},
			wantLine: `Error: invalid commit hash for --commit-close: "abcdef" (expected 7 to 64 hexadecimal characters)`,
			wantCode: ExitInvalidData,
		},
		{
			name:     "--commit-open written with no value after it",
			args:     []string{"stat", "-r", "ledger-service", "41", "DOING", "--commit-open"},
			wantLine: "Error: --commit-open requires a value",
			wantCode: ExitMisuse,
		},
		{
			name:     "--commit-close written with no value after it",
			args:     []string{"stat", "-r", "ledger-service", "41", "COMPLETED", "--commit-close"},
			wantLine: "Error: --commit-close requires a value",
			wantCode: ExitMisuse,
		},
		{
			name:     "the short form is reported under the long name",
			args:     []string{"stat", "-r", "ledger-service", "41", "DOING", "-co"},
			wantLine: "Error: --commit-open requires a value",
			wantCode: ExitMisuse,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, line := run(tc.args...)
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d (SPEC/COMMANDS.md § Change Status (stat), error table); "+
					"stderr: %s", code, tc.wantCode, line)
			}
			if line != tc.wantLine {
				t.Errorf("stderr first line =\n  %s\nwant\n  %s\n(the SPEC gives this message verbatim, "+
					"and printError supplies the \"Error: \" prefix)", line, tc.wantLine)
			}
		})
	}

	// Control: the same shape of command line, correct in every respect the
	// steps above police, reaches the roadmap lookup and fails there instead.
	// Without this, a bug that rejected every `task stat` invocation outright
	// would satisfy every assertion above.
	code, line := run("stat", "-r", "ledger-service", "41", "DOING", "--commit-open", "5f93b51")
	if code != ExitNotFound {
		t.Errorf("control: a well-formed invocation exited %d, want %d (the missing roadmap); the "+
			"rejections above may not be reaching the checks they claim to. stderr: %s",
			code, ExitNotFound, line)
	}
}
