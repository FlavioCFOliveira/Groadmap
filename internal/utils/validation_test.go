package utils

import (
	"math"
	"strings"
	"testing"
)

func TestValidateID(t *testing.T) {
	tests := []struct {
		name    string
		id      int
		entity  string
		wantErr bool
		errMsg  string
	}{
		// Valid IDs
		{"positive ID 1", 1, "task", false, ""},
		{"positive ID 100", 100, "task", false, ""},
		{"max int32", math.MaxInt32, "sprint", false, ""},

		// Invalid - zero
		{"zero ID", 0, "task", true, "must be positive"},

		// Invalid - negative
		{"negative ID -1", -1, "task", true, "must be positive"},
		{"negative ID -100", -100, "sprint", true, "must be positive"},

		// Invalid - exceeds max
		{"exceeds max int32", math.MaxInt32 + 1, "task", true, "exceeds maximum"},
		{"very large ID", 999999999999, "sprint", true, "exceeds maximum"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateID(tt.id, tt.entity)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateID(%d, %q) error = %v, wantErr %v", tt.id, tt.entity, err, tt.wantErr)
				return
			}
			if err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateID(%d, %q) error message = %q, should contain %q", tt.id, tt.entity, err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidateID_ErrorMessages(t *testing.T) {
	// Test that error messages include entity name
	err := ValidateID(0, "task")
	if err == nil {
		t.Error("expected error for ID 0")
	}
	if !strings.Contains(err.Error(), "task") {
		t.Error("error message should contain entity name 'task'")
	}

	err = ValidateID(-1, "sprint")
	if err == nil {
		t.Error("expected error for ID -1")
	}
	if !strings.Contains(err.Error(), "sprint") {
		t.Error("error message should contain entity name 'sprint'")
	}
}
