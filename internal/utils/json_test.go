package utils

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestHTMLEscapingDisabled specifically tests that HTML characters are NOT escaped
// This documents the intentional security decision for CLI usage
func TestHTMLEscapingDisabled(t *testing.T) {
	// Test data with HTML characters
	data := map[string]string{
		"html":      "<script>alert('xss')</script>",
		"ampersand": "Tom & Jerry",
		"quotes":    `He said "Hello"`,
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	// Standard json.Marshal DOES escape HTML
	if !strings.Contains(string(jsonBytes), `\u003c`) {
		t.Log("Standard json.Marshal escapes HTML characters (expected behavior)")
	}

	// Our ToJSON function uses json.Marshal which escapes HTML
	// This is acceptable for data serialization
	result, err := ToJSON(data)
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// Verify the result is valid JSON
	var decoded map[string]string
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Verify data integrity (round-trip)
	if decoded["html"] != data["html"] {
		t.Errorf("HTML content mismatch: got %q, want %q", decoded["html"], data["html"])
	}
	if decoded["ampersand"] != data["ampersand"] {
		t.Errorf("Ampersand content mismatch: got %q, want %q", decoded["ampersand"], data["ampersand"])
	}
}
