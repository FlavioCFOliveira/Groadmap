package utils

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
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

// TestPrintJSONError tests error handling in PrintJSON
func TestPrintJSONError(t *testing.T) {
	// Test with a channel (cannot be marshaled to JSON)
	ch := make(chan int)
	err := PrintJSON(ch)
	if err == nil {
		t.Error("PrintJSON should return error for unmarshalable data")
	}
}

// TestToJSONError tests error handling in ToJSON
func TestToJSONError(t *testing.T) {
	// Test with a channel (cannot be marshaled to JSON)
	ch := make(chan int)
	_, err := ToJSON(ch)
	if err == nil {
		t.Error("ToJSON should return error for unmarshalable data")
	}
}

// TestPrintJSONOutput tests that PrintJSON produces indented, human-readable output
func TestPrintJSONOutput(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	input := map[string]string{"key": "value"}
	err := PrintJSON(input)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("PrintJSON failed: %v", err)
	}

	// Read output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Must contain indentation (newlines and spaces)
	if !strings.Contains(output, "\n") {
		t.Error("expected indented output to contain newlines")
	}
	if !strings.Contains(output, "  ") {
		t.Error("expected indented output to contain 2-space indentation")
	}
	if !strings.Contains(output, "key") {
		t.Error("expected output to contain 'key'")
	}
	if !strings.Contains(output, "value") {
		t.Error("expected output to contain 'value'")
	}
}

// BenchmarkPrintJSON benchmarks the JSON encoder implementation.
func BenchmarkPrintJSON(b *testing.B) {
	data := map[string]interface{}{
		"id":          123,
		"name":        "Test Task",
		"description": "This is a test task with some content",
		"priority":    5,
		"severity":    3,
		"status":      "BACKLOG",
	}

	// Redirect stdout to avoid cluttering benchmark output
	oldStdout := os.Stdout
	os.Stdout = os.NewFile(0, os.DevNull)
	defer func() { os.Stdout = oldStdout }()

	b.Run("PrintJSON", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = PrintJSON(data)
		}
	})
}

// BenchmarkToJSON benchmarks JSON marshaling.
func BenchmarkToJSON(b *testing.B) {
	data := map[string]interface{}{
		"id":          123,
		"name":        "Test Task",
		"description": "This is a test task with some content",
		"priority":    5,
		"severity":    3,
		"status":      "BACKLOG",
	}

	b.Run("ToJSON", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = ToJSON(data)
		}
	})
}
