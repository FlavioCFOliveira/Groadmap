package utils

import (
	"strings"
	"testing"
)

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
		errType error
	}{
		{
			name:    "empty string",
			input:   "",
			want:    "",
			wantErr: false,
		},
		{
			name:    "normal text",
			input:   "Hello World",
			want:    "Hello World",
			wantErr: false,
		},
		{
			name:    "text with newlines",
			input:   "Line 1\nLine 2\nLine 3",
			want:    "Line 1\nLine 2\nLine 3",
			wantErr: false,
		},
		{
			name:    "text with tabs",
			input:   "Column 1\tColumn 2",
			want:    "Column 1\tColumn 2",
			wantErr: false,
		},
		{
			name:    "text with carriage return",
			input:   "Line 1\r\nLine 2",
			want:    "Line 1\r\nLine 2",
			wantErr: false,
		},
		{
			name:    "unicode text",
			input:   "Hello 世界 🌍",
			want:    "Hello 世界 🌍",
			wantErr: false,
		},
		{
			name:    "null byte",
			input:   "Hello\x00World",
			wantErr: true,
			errType: ErrNullByte,
		},
		{
			name:    "null byte at start",
			input:   "\x00Hello",
			wantErr: true,
			errType: ErrNullByte,
		},
		{
			name:    "null byte only",
			input:   "\x00",
			wantErr: true,
			errType: ErrNullByte,
		},
		{
			name:    "bell character",
			input:   "Hello\x07World",
			wantErr: true,
			errType: ErrControlChar,
		},
		{
			name:    "escape character",
			input:   "Hello\x1BWorld",
			wantErr: true,
			errType: ErrControlChar,
		},
		{
			name:    "form feed",
			input:   "Hello\x0CWorld",
			wantErr: true,
			errType: ErrControlChar,
		},
		{
			name:    "vertical tab",
			input:   "Hello\x0BWorld",
			wantErr: true,
			errType: ErrControlChar,
		},
		{
			name:    "backspace",
			input:   "Hello\x08World",
			wantErr: true,
			errType: ErrControlChar,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SanitizeString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SanitizeString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errType != nil && !strings.Contains(err.Error(), tt.errType.Error()) {
				t.Errorf("SanitizeString() error = %v, should contain %v", err, tt.errType)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("SanitizeString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContainsNullByte(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"no null", "Hello World", false},
		{"with null", "Hello\x00World", true},
		{"null at start", "\x00Hello", true},
		{"null at end", "Hello\x00", true},
		{"only null", "\x00", true},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainsNullByte(tt.input); got != tt.expected {
				t.Errorf("ContainsNullByte(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestContainsControlChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"no control chars", "Hello World", false},
		{"newline allowed", "Hello\nWorld", false},
		{"tab allowed", "Hello\tWorld", false},
		{"carriage return allowed", "Hello\rWorld", false},
		{"bell not allowed", "Hello\x07World", true},
		{"escape not allowed", "Hello\x1BWorld", true},
		{"null not allowed", "Hello\x00World", true},
		{"form feed not allowed", "Hello\x0CWorld", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainsControlChars(tt.input); got != tt.expected {
				t.Errorf("ContainsControlChars(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeNFC(t *testing.T) {
	// Test combining characters
	// é can be represented as:
	// - NFC: U+00E9 (single character)
	// - NFD: U+0065 + U+0301 (e + combining acute accent)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "already NFC",
			input:    "café",
			expected: "café",
		},
		{
			name:     "ASCII only",
			input:    "Hello",
			expected: "Hello",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeNFC(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeNFC() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestIsNormalizedNFC(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"ASCII", "Hello", true},
		{"empty", "", true},
		{"unicode", "café", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNormalizedNFC(tt.input); got != tt.expected {
				t.Errorf("IsNormalizedNFC(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// TestSanitizeStringMaliciousInputs tests against potentially malicious inputs
func TestSanitizeStringMaliciousInputs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "ANSI escape sequence",
			input:   "\x1B[31mRed Text\x1B[0m",
			wantErr: true,
		},
		{
			name:    "bell character spam",
			input:   strings.Repeat("\x07", 10),
			wantErr: true,
		},
		{
			name:    "mixed valid and invalid",
			input:   "Hello\x07World\x00Test",
			wantErr: true,
		},
		{
			name:    "backspace characters",
			input:   "Hello\x08\x08\x08World",
			wantErr: true,
		},
		{
			name:    "form feed",
			input:   "Page1\x0CPage2",
			wantErr: true,
		},
		{
			name:    "vertical tab",
			input:   "Line1\x0BLine2",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SanitizeString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SanitizeString() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
