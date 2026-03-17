// Package utils provides utility functions for the Groadmap CLI application.
package utils

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Common sanitization errors
var (
	ErrNullByte       = fmt.Errorf("input contains null byte")
	ErrControlChar    = fmt.Errorf("input contains invalid control characters")
	ErrInvalidUnicode = fmt.Errorf("input contains invalid Unicode")
)

// SanitizeString sanitizes a string by:
// - Rejecting null bytes (0x00)
// - Removing or rejecting control characters (0x00-0x1F, except \n, \t, \r)
// - Normalizing Unicode to NFC form
// Returns the sanitized string or an error if invalid characters are found.
func SanitizeString(input string) (string, error) {
	if input == "" {
		return "", nil
	}

	// Check for null bytes first
	if strings.ContainsRune(input, 0x00) {
		return "", ErrNullByte
	}

	// Check for invalid control characters
	var result strings.Builder
	for _, r := range input {
		// Allow printable characters
		if unicode.IsPrint(r) {
			result.WriteRune(r)
			continue
		}

		// Allow specific whitespace characters
		if r == '\n' || r == '\t' || r == '\r' {
			result.WriteRune(r)
			continue
		}

		// Reject other control characters
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: found U+%04X", ErrControlChar, r)
		}

		// Allow other Unicode characters (will be normalized)
		result.WriteRune(r)
	}

	// Normalize Unicode to NFC
	normalized := norm.NFC.String(result.String())

	return normalized, nil
}

// SanitizeStringStrict sanitizes a string and rejects any invalid characters
// (same as SanitizeString but with stricter error handling).
func SanitizeStringStrict(input string) (string, error) {
	return SanitizeString(input)
}

// ContainsControlChars checks if a string contains control characters
// (excluding allowed whitespace: \n, \t, \r).
func ContainsControlChars(input string) bool {
	for _, r := range input {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return true
		}
	}
	return false
}

// ContainsNullByte checks if a string contains null bytes.
func ContainsNullByte(input string) bool {
	return strings.ContainsRune(input, 0x00)
}

// IsNormalizedNFC checks if a string is in NFC normalized form.
func IsNormalizedNFC(input string) bool {
	return norm.NFC.IsNormalString(input)
}

// NormalizeNFC normalizes a string to NFC form.
func NormalizeNFC(input string) string {
	return norm.NFC.String(input)
}
