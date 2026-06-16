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
	MaxSprintDescriptionLength = 2048

	// MaxSprintMaxTasks is the maximum value accepted for a sprint's --max-tasks
	// capacity cap. A value above this is rejected with exit code 6
	// (SPEC/COMMANDS.md § Create/Update Sprint).
	MaxSprintMaxTasks = 10000
)

// Audit result limits
const (
	// MaxAuditLimit is the maximum number of audit entries that may be requested
	// in a single list operation. It bounds the CLI --limit flag (exit 6 when
	// exceeded) and is reused as the server-side cap in the DB layer
	// (SPEC/COMMANDS.md § Audit List, SPEC/DATABASE.md § Audit Result Limit).
	MaxAuditLimit = 500
)
