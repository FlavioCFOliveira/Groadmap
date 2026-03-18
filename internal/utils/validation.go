package utils

import (
	"fmt"
	"math"
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

	// Parse the integer
	var id int
	_, err := fmt.Sscanf(s, "%d", &id)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid %s ID: %q (must be a positive integer)", ErrInvalidInput, entity, s)
	}
	if err := ValidateID(id, entity); err != nil {
		return 0, err
	}
	return id, nil
}
