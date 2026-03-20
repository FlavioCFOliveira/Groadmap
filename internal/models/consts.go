package models

// Task limit constants for list operations
const (
	// DefaultTaskLimit is the default number of tasks returned in list operations
	DefaultTaskLimit = 100

	// MaxTaskLimit is the maximum number of tasks that can be requested in a single list operation
	MaxTaskLimit = 100
)

// Priority and Severity bounds
const (
	// MinPriority is the minimum valid priority value (0 = lowest)
	MinPriority = 0

	// MaxPriority is the maximum valid priority value (9 = highest)
	MaxPriority = 9

	// MinSeverity is the minimum valid severity value (0 = lowest)
	MinSeverity = 0

	// MaxSeverity is the maximum valid severity value (9 = highest)
	MaxSeverity = 9
)

// Sprint description limits
const (
	// MaxSprintDescriptionLength is the maximum length for sprint descriptions
	MaxSprintDescriptionLength = 500
)
