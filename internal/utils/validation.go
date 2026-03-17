package utils

import (
	"fmt"
	"math"
)

// ValidateID validates that an ID is a positive integer within acceptable bounds.
// IDs must be > 0 and <= MaxInt32 to prevent overflow and ensure database compatibility.
func ValidateID(id int, entity string) error {
	if id <= 0 {
		return fmt.Errorf("invalid %s ID: %d (must be positive)", entity, id)
	}
	if id > math.MaxInt32 {
		return fmt.Errorf("invalid %s ID: %d (exceeds maximum value)", entity, id)
	}
	return nil
}
