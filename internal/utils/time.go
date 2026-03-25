// Package utils provides utility functions for the Groadmap CLI application.
package utils

import (
	"errors"
	"fmt"
	"time"
)

// ISO8601Format is the required date format: YYYY-MM-DDTHH:mm:ss.sssZ
const ISO8601Format = "2006-01-02T15:04:05.000Z"

// Date range constants for validation
var (
	// MinValidDate is the minimum valid date (1970-01-01T00:00:00.000Z - Unix epoch)
	MinValidDate = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

	// MaxValidDate is the maximum valid date (9999-12-31T23:59:59.999Z)
	MaxValidDate = time.Date(9999, 12, 31, 23, 59, 59, 999000000, time.UTC)
)

// Common date validation errors
var (
	ErrDateTooEarly = errors.New("date is before minimum valid date (1970-01-01)")
	ErrDateTooLate  = errors.New("date is after maximum valid date (9999-12-31)")
	ErrDateInFuture = errors.New("date is in the future")
)

// FormatISO8601 formats a time.Time to ISO 8601 UTC string with milliseconds.
// Returns empty string for zero time.
func FormatISO8601(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(ISO8601Format)
}

// ParseISO8601 parses an ISO 8601 UTC string to time.Time.
// Accepts the primary format YYYY-MM-DDTHH:mm:ss.sssZ and also RFC3339 variants
// (e.g. with +00:00 offset or microseconds) for compatibility with standard libraries.
// Always returns the time in UTC.
// Validates that the date is within the valid range (1970-01-01 to 9999-12-31).
func ParseISO8601(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("parsing ISO 8601 date: empty string")
	}

	// Try formats in order of preference.
	formats := []string{
		ISO8601Format,    // YYYY-MM-DDTHH:mm:ss.sssZ (primary)
		time.RFC3339Nano, // accepts +00:00 offset and sub-second precision
		time.RFC3339,     // accepts +00:00 offset without fractional seconds
	}

	var t time.Time
	var err error
	for _, f := range formats {
		t, err = time.Parse(f, s)
		if err == nil {
			break
		}
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing ISO 8601 date %q: %w", s, err)
	}

	t = t.UTC()

	// Validate date range
	if err := ValidateDateRange(t); err != nil {
		return time.Time{}, fmt.Errorf("validating date %q: %w", s, err)
	}

	return t, nil
}

// ValidateDateRange checks if a date is within the valid range.
// Valid range: 1970-01-01 to 9999-12-31
func ValidateDateRange(t time.Time) error {
	if t.Before(MinValidDate) {
		return ErrDateTooEarly
	}
	if t.After(MaxValidDate) {
		return ErrDateTooLate
	}
	return nil
}

// ValidateNotFuture checks if a date is not in the future.
// Allows a 1-minute tolerance for clock drift.
func ValidateNotFuture(t time.Time) error {
	// Add 1 minute tolerance for clock drift
	if t.After(time.Now().UTC().Add(time.Minute)) {
		return ErrDateInFuture
	}
	return nil
}

// ValidateDateOrder checks if end date is not before start date.
func ValidateDateOrder(start, end time.Time) error {
	if end.Before(start) {
		return fmt.Errorf("end date (%s) is before start date (%s)",
			FormatISO8601(end), FormatISO8601(start))
	}
	return nil
}

// NowISO8601 returns the current time formatted as ISO 8601 UTC string.
func NowISO8601() string {
	return FormatISO8601(time.Now())
}

// IsValidISO8601 checks if a string is a valid ISO 8601 date.
func IsValidISO8601(s string) bool {
	_, err := ParseISO8601(s)
	return err == nil
}
