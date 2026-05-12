package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// MaxInt32 is the maximum valid ID value (prevents integer overflow).
const MaxInt32 = math.MaxInt32 // 2,147,483,647

// ValidateID validates that an ID is positive and within safe limits.
// Returns an error if the ID is invalid, nil otherwise.
//
// Validation rules:
//   - ID must be > 0 (positive)
//   - ID must be <= MaxInt32 (2,147,483,647) to prevent overflow
//
// Example:
//
//	err := ValidateID(42, "task")
//	if err != nil {
//	    return err
//	}
func ValidateID(id int, entity string) error {
	if id <= 0 {
		return fmt.Errorf("%w: invalid %s ID: %d (must be positive)", ErrInvalidInput, entity, id)
	}
	if id > MaxInt32 {
		return fmt.Errorf("%w: invalid %s ID: %d (exceeds maximum value %d)", ErrInvalidInput, entity, id, MaxInt32)
	}
	return nil
}

// ValidateIDList validates a slice of IDs, returning the first error encountered.
// Duplicate IDs are allowed (will be handled by the database).
//
// Example:
//
//	ids := []int{1, 2, 3}
//	err := ValidateIDList(ids, "task")
func ValidateIDList(ids []int, entity string) error {
	for _, id := range ids {
		if err := ValidateID(id, entity); err != nil {
			return err
		}
	}
	return nil
}

// ValidateNumericRange checks that val is within the inclusive [min, max]
// range and returns a wrapped ErrInvalidInput error otherwise. Used for
// CLI inputs like priority and severity that share an identical bounds
// check and error format.
func ValidateNumericRange(val, min, max int, field string) error {
	if val < min || val > max {
		return fmt.Errorf("%w: invalid %s: must be %d-%d (got %d)", ErrInvalidInput, field, min, max, val)
	}
	return nil
}

// ParseCommaSeparatedIDs parses a comma-separated list of IDs and validates
// each one through ValidateIDString. Returns the parsed slice or the first
// validation error encountered.
//
// Example:
//
//	ids, err := ParseCommaSeparatedIDs("1,2, 3", "task")
//	// ids == []int{1, 2, 3}
func ParseCommaSeparatedIDs(s string, entity string) ([]int, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("%w: %s ID(s) required", ErrRequired, entity)
	}
	parts := strings.Split(s, ",")
	ids := make([]int, 0, len(parts))
	for _, p := range parts {
		id, err := ValidateIDString(strings.TrimSpace(p), entity)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ValidateIDString parses and validates an ID from a string.
// Returns the parsed ID if valid, or an error if invalid.
//
// Example:
//
//	id, err := ValidateIDString("42", "task")
//	if err != nil {
//	    return err
//	}
func ValidateIDString(s string, entity string) (int, error) {
	// Trim whitespace
	s = strings.TrimSpace(s)

	// Check for empty string
	if s == "" {
		return 0, fmt.Errorf("%w: invalid %s ID: %q (must be a positive integer)", ErrInvalidInput, entity, s)
	}

	// Check for non-digit characters (except leading minus for negative)
	// This ensures we reject "12abc" which Sscanf would parse as 12
	for i, r := range s {
		if i == 0 && r == '-' {
			continue // Allow leading minus for negative detection
		}
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%w: invalid %s ID: %q (must be a positive integer)", ErrInvalidInput, entity, s)
		}
	}

	// Parse the integer. The digit-only check above guarantees Atoi cannot
	// fail on syntax, so any error here is an overflow.
	id, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid %s ID: %q (must be a positive integer)", ErrInvalidInput, entity, s)
	}
	if err := ValidateID(id, entity); err != nil {
		return 0, err
	}
	return id, nil
}
