package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

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
		{"ErrInvalidInput", utils.ErrInvalidInput, ExitInvalidData},
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

func TestHandleError_StringMatching(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{"not found in message", errors.New("resource not found in database"), ExitNotFound},
		{"already exists in message", errors.New("item already exists"), ExitExists},
		{"invalid in message", errors.New("invalid input provided"), ExitInvalidData},
		{"required in message", errors.New("field is required"), ExitMisuse},
		{"no roadmap in message", errors.New("no roadmap selected"), ExitNoRoadmap},
		{"generic error", errors.New("something went wrong"), ExitFailure},
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
