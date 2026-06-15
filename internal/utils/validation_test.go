package utils

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateIDString_OverflowExitClass is a regression gate for finding #58:
// an int64-overflowing all-digits ID must map to ErrValidation (exit 6), the
// same class as an in-range value that exceeds the valid ID maximum — NOT
// ErrInvalidInput (exit 2), which is reserved for genuine syntax errors.
func TestValidateIDString_OverflowExitClass(t *testing.T) {
	_, err := ValidateIDString("999999999999999999999", "task")
	if err == nil {
		t.Fatal("expected error for overflowing ID, got nil")
	}
	if !errors.Is(err, ErrValidation) {
		t.Errorf("overflow ID must wrap ErrValidation (exit 6), got: %v", err)
	}
	if errors.Is(err, ErrInvalidInput) {
		t.Errorf("overflow ID must NOT be classified as ErrInvalidInput (exit 2): %v", err)
	}

	// A genuine syntax error (non-digit) stays ErrInvalidInput (exit 2).
	_, err = ValidateIDString("12abc", "task")
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("non-numeric ID must remain ErrInvalidInput (exit 2), got: %v", err)
	}
}

// TestValidateRoadmapName_SpecVerbatimMessages is a regression gate for finding
// #60: the roadmap-name validation messages must match SPEC/COMMANDS.md verbatim
// (no sentinel prefix) while still wrapping ErrValidation (exit 6).
func TestValidateRoadmapName_SpecVerbatimMessages(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{"empty", "", "Roadmap name is required"},
		{"too long", strings.Repeat("a", MaxRoadmapNameLength+1),
			"Roadmap name must not exceed 50 characters (got 51)"},
		{"invalid chars", "Bad_UPPER",
			"Roadmap name must only contain lowercase letters, numbers, underscores, and hyphens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRoadmapName(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
			if err.Error() != tc.wantMsg {
				t.Errorf("message = %q, want SPEC-verbatim %q", err.Error(), tc.wantMsg)
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("must wrap ErrValidation (exit 6): %v", err)
			}
		})
	}
}

func TestValidateID(t *testing.T) {
	tests := []struct {
		name    string
		id      int
		entity  string
		wantErr bool
		errMsg  string
	}{
		// Valid IDs
		{"valid positive ID", 1, "task", false, ""},
		{"valid ID 42", 42, "task", false, ""},
		{"valid ID 1000", 1000, "task", false, ""},
		{"valid ID MaxInt32", MaxInt32, "task", false, ""},
		{"valid sprint ID", 5, "sprint", false, ""},

		// Invalid - zero
		{"zero ID", 0, "task", true, "must be positive"},

		// Invalid - negative
		{"negative ID -1", -1, "task", true, "must be positive"},
		{"negative ID -100", -100, "task", true, "must be positive"},

		// Invalid - overflow
		{"overflow ID", MaxInt32 + 1, "task", true, "exceeds maximum"},
		{"large overflow", MaxInt32 + 1000000, "task", true, "exceeds maximum"},

		// Invalid - entity in error message
		{"zero task ID", 0, "task", true, "task"},
		{"zero sprint ID", 0, "sprint", true, "sprint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateID(tt.id, tt.entity)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateID(%d, %q) error = %v, wantErr %v", tt.id, tt.entity, err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateID(%d, %q) error message = %v, should contain %q", tt.id, tt.entity, err, tt.errMsg)
				}
			}
		})
	}
}

func TestValidateIDList(t *testing.T) {
	tests := []struct {
		name    string
		ids     []int
		entity  string
		wantErr bool
	}{
		// Valid lists
		{"empty list", []int{}, "task", false},
		{"single valid ID", []int{1}, "task", false},
		{"multiple valid IDs", []int{1, 2, 3}, "task", false},
		{"duplicate IDs", []int{1, 1, 2}, "task", false},
		{"large valid ID", []int{MaxInt32}, "task", false},

		// Invalid - contains zero
		{"list with zero", []int{1, 0, 3}, "task", true},

		// Invalid - contains negative
		{"list with negative", []int{1, -1, 3}, "task", true},

		// Invalid - contains overflow
		{"list with overflow", []int{1, MaxInt32 + 1}, "task", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIDList(tt.ids, tt.entity)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIDList(%v, %q) error = %v, wantErr %v", tt.ids, tt.entity, err, tt.wantErr)
			}
		})
	}
}

func TestValidateIDString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		entity  string
		wantID  int
		wantErr bool
		errMsg  string
	}{
		// Valid strings
		{"valid ID 1", "1", "task", 1, false, ""},
		{"valid ID 42", "42", "task", 42, false, ""},
		{"valid ID with spaces", "  100  ", "task", 100, false, ""},
		{"valid sprint ID", "5", "sprint", 5, false, ""},

		// Invalid - not a number
		{"invalid text", "abc", "task", 0, true, "must be a positive integer"},
		{"mixed text", "12abc", "task", 0, true, "must be a positive integer"},
		{"empty string", "", "task", 0, true, "must be a positive integer"},

		// Invalid - zero
		{"zero", "0", "task", 0, true, "must be positive"},

		// Invalid - negative
		{"negative", "-1", "task", 0, true, "must be positive"},
		{"negative large", "-999", "task", 0, true, "must be positive"},

		// Invalid - overflow (exceeds the valid ID range, parses into int64)
		{"overflow", "99999999999999999", "task", 0, true, "exceeds maximum"},
		// Invalid - int64 overflow (too big for Atoi). Regression for finding
		// #58: classified as a magnitude/range failure ("exceeds maximum"),
		// consistent with the in-range overflow above, not a syntax error.
		{"int64 overflow", "999999999999999999999", "task", 0, true, "exceeds maximum"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, err := ValidateIDString(tt.input, tt.entity)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIDString(%q, %q) error = %v, wantErr %v", tt.input, tt.entity, err, tt.wantErr)
				return
			}
			if !tt.wantErr && gotID != tt.wantID {
				t.Errorf("ValidateIDString(%q, %q) = %d, want %d", tt.input, tt.entity, gotID, tt.wantID)
			}
			if err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateIDString(%q, %q) error message = %v, should contain %q", tt.input, tt.entity, err, tt.errMsg)
				}
			}
		})
	}
}
