package main

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/aihelp"
	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
)

// TestSpecialistsRemoval_AssignUnassignExitMisuse is the end-to-end half of the
// exit-code claim in SPEC/COMMANDS.md § Command Aliases Reference: `rmp task
// assign` and `rmp task unassign` are rejected by the SAME unknown-subcommand
// path as any other unrecognised name, at exit code 2, with no reserved code of
// their own.
//
// internal/commands can only assert which sentinel the error carries; the
// sentinel-to-code mapping lives here, in handleError. This test closes the gap
// by running the real registry dispatch main() runs and feeding the resulting
// error to the real handleError, so the literal 2 is observed rather than
// inferred.
//
// The control case is what makes it more than a tautology: `nonexistent-sub`, a
// name that was never a subcommand, must produce the identical code. Equality
// between the two is the actual claim — "assign" is not special.
func TestSpecialistsRemoval_AssignUnassignExitMisuse(t *testing.T) {
	taskCmd := commands.AppRegistry().FindCommand("task")
	if taskCmd == nil {
		t.Fatal("task command missing from registry")
	}

	run := func(sub string) (int, string) {
		t.Helper()
		aihelp.ResetHintForTesting()
		var code int
		stderr := captureStderrForExitTest(t, func() {
			err := taskCmd.DispatchFamily([]string{sub, "-r", "myproject", "7", "backend-team"})
			code = handleError(err)
		})
		return code, stderr
	}

	controlCode, controlStderr := run("nonexistent-sub")
	if controlCode != ExitMisuse {
		t.Fatalf("control: `rmp task nonexistent-sub` exited %d, want %d (ExitMisuse); the "+
			"unknown-subcommand path itself is broken, so the comparisons below prove nothing (stderr: %s)",
			controlCode, ExitMisuse, controlStderr)
	}

	for _, sub := range []string{"assign", "unassign"} {
		code, stderr := run(sub)
		if code != ExitMisuse {
			t.Errorf("`rmp task %s` exited %d, want %d (ExitMisuse); stderr: %s",
				sub, code, ExitMisuse, stderr)
		}
		if code != controlCode {
			t.Errorf("`rmp task %s` exited %d but the never-registered control exited %d; the two "+
				"retired names must not have an exit code of their own",
				sub, code, controlCode)
		}
		if !strings.Contains(stderr, "unknown task subcommand: "+sub) {
			t.Errorf("`rmp task %s`: stderr = %q, want the generic unknown-subcommand message", sub, stderr)
		}
		if !strings.HasPrefix(stderr, "Error:") {
			t.Errorf("`rmp task %s`: stderr = %q, want it to start with \"Error:\" "+
				"(SPEC/HELP.md § Error message format)", sub, stderr)
		}
	}
}

// exitCodeConstBlock locates the exit-code const block in main.go and
// exitCodeConstLine matches one constant inside it. Both anchor at the start of
// a line so a mention elsewhere in the file cannot redirect the scan — the same
// rule the SPEC scanners in internal/aihelp use.
var (
	exitCodeBlockStart = "// Exit codes as defined in SPEC/ARCHITECTURE.md\nconst (\n"
	exitCodeConstLine  = regexp.MustCompile(`(?m)^\t(Exit[A-Za-z]+)\s*=\s*(\d+)$`)
)

// TestSpecialistsRemoval_NoNewExitCodeExists guards the second half of the
// contract: `rmp task assign` must fail through the EXISTING path, so the
// removal must not have added a code for it.
//
// TestExitCodes already pins the value of each known constant, but it cannot
// notice an eleventh one being introduced — it only checks the ten it lists. So
// this test reads the const block out of main.go and requires the set of names
// to be exactly the documented one (SPEC/ARCHITECTURE.md § Exit Codes). Adding a
// constant fails here; removing one fails here and in TestExitCodes.
func TestSpecialistsRemoval_NoNewExitCodeExists(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	idx := bytes.Index(src, []byte(exitCodeBlockStart))
	if idx < 0 {
		t.Fatalf("could not locate the exit-code const block in main.go; the scan cannot report "+
			"an absence it never measured (looked for %q)", exitCodeBlockStart)
	}
	block := string(src[idx+len(exitCodeBlockStart):])
	if closeIdx := strings.Index(block, "\n)"); closeIdx >= 0 {
		block = block[:closeIdx]
	}

	found := map[string]int{}
	for _, m := range exitCodeConstLine.FindAllStringSubmatch(block, -1) {
		value, convErr := strconv.Atoi(m[2])
		if convErr != nil {
			t.Fatalf("exit code %s has a non-numeric value %q", m[1], m[2])
		}
		found[m[1]] = value
	}

	want := map[string]int{
		"ExitSuccess":       ExitSuccess,
		"ExitFailure":       ExitFailure,
		"ExitMisuse":        ExitMisuse,
		"ExitNoRoadmap":     ExitNoRoadmap,
		"ExitNotFound":      ExitNotFound,
		"ExitExists":        ExitExists,
		"ExitInvalidData":   ExitInvalidData,
		"ExitNotExecutable": ExitNotExecutable,
		"ExitCmdNotFound":   ExitCmdNotFound,
		"ExitSigint":        ExitSigint,
	}
	if len(found) != len(want) {
		t.Errorf("main.go declares %d exit codes, want exactly %d; declared: %v", len(found), len(want), found)
	}
	for name, value := range found {
		wantValue, known := want[name]
		if !known {
			t.Errorf("main.go declares an undocumented exit code %s = %d; the retired subcommands must "+
				"reuse the existing unknown-subcommand path, not gain a code", name, value)
			continue
		}
		if value != wantValue {
			t.Errorf("main.go declares %s = %d, want %d", name, value, wantValue)
		}
	}
	for name := range want {
		if _, ok := found[name]; !ok {
			t.Errorf("main.go no longer declares %s", name)
		}
	}
}

// captureStderrForExitTest redirects os.Stderr for the duration of fn.
// handleError writes through printError, which targets os.Stderr directly, so
// the redirect is the only way to observe it.
func captureStderrForExitTest(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w

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
	os.Stderr = old
	<-done
	return buf.String()
}
