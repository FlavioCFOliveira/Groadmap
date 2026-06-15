package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestCommandSummaryLines_CoversEveryRegisteredCommand is a regression gate for
// finding #51: the global `rmp --help` command list is generated from the
// registry, so EVERY registered top-level command (including `web`, which had
// silently drifted out of the old hardcoded list) must appear by name.
func TestCommandSummaryLines_CoversEveryRegisteredCommand(t *testing.T) {
	out := commandSummaryLines()
	for _, c := range commands.AppRegistry().Commands {
		if !strings.Contains(out, c.Name) {
			t.Errorf("global help command list is missing registered command %q; output:\n%s", c.Name, out)
		}
	}
	// Explicitly guard the command that regressed.
	if !strings.Contains(out, "web") {
		t.Error("global help must list the 'web' command (finding #51)")
	}
}

func TestHandleError_NilError(t *testing.T) {
	code := handleError(nil)
	if code != ExitSuccess {
		t.Errorf("handleError(nil) = %d, want %d", code, ExitSuccess)
	}
}

func TestHandleError_SentinelErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{"ErrNotFound", utils.ErrNotFound, ExitNotFound},
		{"ErrAlreadyExists", utils.ErrAlreadyExists, ExitExists},
		{"ErrNoRoadmap", utils.ErrNoRoadmap, ExitNoRoadmap},
		{"ErrInvalidInput", utils.ErrInvalidInput, ExitMisuse},
		{"ErrValidation", utils.ErrValidation, ExitInvalidData},
		{"ErrRequired", utils.ErrRequired, ExitMisuse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := handleError(tt.err)
			if code != tt.expected {
				t.Errorf("handleError(%v) = %d, want %d", tt.err, code, tt.expected)
			}
		})
	}
}

func TestHandleError_WrappedErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{"wrapped ErrNotFound", fmt.Errorf("task: %w", utils.ErrNotFound), ExitNotFound},
		{"wrapped ErrAlreadyExists", fmt.Errorf("roadmap: %w", utils.ErrAlreadyExists), ExitExists},
		{"wrapped ErrNoRoadmap", fmt.Errorf("context: %w", utils.ErrNoRoadmap), ExitNoRoadmap},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := handleError(tt.err)
			if code != tt.expected {
				t.Errorf("handleError(%v) = %d, want %d", tt.err, code, tt.expected)
			}
		})
	}
}

func TestHandleError_UnwrappedFallsBackToFailure(t *testing.T) {
	// Errors that do not wrap a utils.Err* sentinel collapse to ExitFailure.
	// Internal callers must always wrap with %w; this guards against drift.
	tests := []struct {
		name string
		err  error
	}{
		{"plain not-found text", errors.New("resource not found in database")},
		{"plain already-exists text", errors.New("item already exists")},
		{"plain invalid text", errors.New("invalid input provided")},
		{"plain required text", errors.New("field is required")},
		{"plain no-roadmap text", errors.New("no roadmap selected")},
		{"generic error", errors.New("something went wrong")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := handleError(tt.err)
			if code != ExitFailure {
				t.Errorf("handleError(%v) = %d, want %d (ExitFailure)", tt.err, code, ExitFailure)
			}
		})
	}
}

func TestVersionConstant(t *testing.T) {
	// Verify version is set and not empty
	if version == "" {
		t.Error("version constant should not be empty")
	}

	// Version should follow semantic versioning format (x.x.x)
	if len(version) < 5 {
		t.Errorf("version %q seems too short, expected format: x.x.x", version)
	}
}

func TestAppNameConstant(t *testing.T) {
	if appName != "Groadmap" {
		t.Errorf("appName = %q, want %q", appName, "Groadmap")
	}
}

func TestExitCodes(t *testing.T) {
	// Verify all exit codes are unique and correctly defined
	tests := []struct {
		name  string
		code  int
		value int
	}{
		{"ExitSuccess", ExitSuccess, 0},
		{"ExitFailure", ExitFailure, 1},
		{"ExitMisuse", ExitMisuse, 2},
		{"ExitNoRoadmap", ExitNoRoadmap, 3},
		{"ExitNotFound", ExitNotFound, 4},
		{"ExitExists", ExitExists, 5},
		{"ExitInvalidData", ExitInvalidData, 6},
		{"ExitNotExecutable", ExitNotExecutable, 126},
		{"ExitCmdNotFound", ExitCmdNotFound, 127},
		{"ExitSigint", ExitSigint, 130},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.value {
				t.Errorf("%s = %d, want %d", tt.name, tt.code, tt.value)
			}
		})
	}
}

func TestExitCodes_Uniqueness(t *testing.T) {
	codes := []int{
		ExitSuccess,
		ExitFailure,
		ExitMisuse,
		ExitNoRoadmap,
		ExitNotFound,
		ExitExists,
		ExitInvalidData,
		ExitNotExecutable,
		ExitCmdNotFound,
		ExitSigint,
	}

	seen := make(map[int]bool)
	for _, code := range codes {
		if seen[code] {
			t.Errorf("exit code %d is duplicated", code)
		}
		seen[code] = true
	}
}
