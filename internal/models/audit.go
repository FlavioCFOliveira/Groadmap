package models

import (
	"fmt"
)

// AuditOperation represents the type of operation logged.
type AuditOperation string

// Audit operation constants as defined in SPEC/DATABASE.md.
const (
	// Task operations
	OpTaskCreate         AuditOperation = "TASK_CREATE"
	OpTaskUpdate         AuditOperation = "TASK_UPDATE"
	OpTaskDelete         AuditOperation = "TASK_DELETE"
	OpTaskStatusChange   AuditOperation = "TASK_STATUS_CHANGE"
	OpTaskPriorityChange AuditOperation = "TASK_PRIORITY_CHANGE"
	OpTaskSeverityChange AuditOperation = "TASK_SEVERITY_CHANGE"
	OpTaskTypeChange     AuditOperation = "TASK_TYPE_CHANGE"

	// Sprint operations
	OpSprintCreate     AuditOperation = "SPRINT_CREATE"
	OpSprintUpdate     AuditOperation = "SPRINT_UPDATE"
	OpSprintDelete     AuditOperation = "SPRINT_DELETE"
	OpSprintStart      AuditOperation = "SPRINT_START"
	OpSprintClose      AuditOperation = "SPRINT_CLOSE"
	OpSprintReopen     AuditOperation = "SPRINT_REOPEN"
	OpSprintAddTask    AuditOperation = "SPRINT_ADD_TASK"
	OpSprintRemoveTask AuditOperation = "SPRINT_REMOVE_TASK"
	OpSprintMoveTask   AuditOperation = "SPRINT_MOVE_TASK"

	// Sprint task ordering operations
	OpSprintReorderTasks     AuditOperation = "SPRINT_REORDER_TASKS"
	OpSprintTaskMovePosition AuditOperation = "SPRINT_TASK_MOVE_POSITION"
	OpSprintTaskSwap         AuditOperation = "SPRINT_TASK_SWAP"
)

// ValidAuditOperations contains all valid audit operations.
var ValidAuditOperations = []AuditOperation{
	OpTaskCreate,
	OpTaskUpdate,
	OpTaskDelete,
	OpTaskStatusChange,
	OpTaskPriorityChange,
	OpTaskSeverityChange,
	OpTaskTypeChange,
	OpSprintCreate,
	OpSprintUpdate,
	OpSprintDelete,
	OpSprintStart,
	OpSprintClose,
	OpSprintReopen,
	OpSprintAddTask,
	OpSprintRemoveTask,
	OpSprintMoveTask,
	OpSprintReorderTasks,
	OpSprintTaskMovePosition,
	OpSprintTaskSwap,
}

// IsValidAuditOperation checks if a string is a valid audit operation.
func IsValidAuditOperation(s string) bool {
	for _, op := range ValidAuditOperations {
		if string(op) == s {
			return true
		}
	}
	return false
}

// ParseAuditOperation parses a string into an AuditOperation.
func ParseAuditOperation(s string) (AuditOperation, error) {
	if !IsValidAuditOperation(s) {
		return "", fmt.Errorf("invalid audit operation: %q", s)
	}
	return AuditOperation(s), nil
}

// EntityType represents the type of entity being audited.
type EntityType string

// Entity type constants.
const (
	EntityTask   EntityType = "TASK"
	EntitySprint EntityType = "SPRINT"
)

// IsValidEntityType checks if a string is a valid entity type.
func IsValidEntityType(s string) bool {
	return s == string(EntityTask) || s == string(EntitySprint)
}

// ParseEntityType parses a string into an EntityType.
func ParseEntityType(s string) (EntityType, error) {
	if !IsValidEntityType(s) {
		return "", fmt.Errorf("invalid entity type: %q", s)
	}
	return EntityType(s), nil
}

// AuditEntry represents a single audit log entry.
// Field order optimized for memory alignment (largest fields first).
type AuditEntry struct {
	// 16-byte fields
	Operation   string `json:"operation"`
	EntityType  string `json:"entity_type"`
	PerformedAt string `json:"performed_at"` // ISO 8601 UTC

	// 8-byte fields
	ID       int `json:"id"`
	EntityID int `json:"entity_id"`
}

// Validate checks if the audit entry data is valid.
func (a *AuditEntry) Validate() error {
	if !IsValidAuditOperation(a.Operation) {
		return fmt.Errorf("invalid operation: %q", a.Operation)
	}
	if !IsValidEntityType(a.EntityType) {
		return fmt.Errorf("invalid entity type: %q", a.EntityType)
	}
	if a.EntityID <= 0 {
		return fmt.Errorf("entity_id must be positive, got %d", a.EntityID)
	}
	if a.PerformedAt == "" {
		return fmt.Errorf("performed_at is required")
	}
	return nil
}

// Roadmap represents a roadmap file metadata.
type Roadmap struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// AuditStats represents statistics for audit entries.
type AuditStats struct {
	TotalEntries int            `json:"total_entries"`
	ByOperation  map[string]int `json:"by_operation"`
	ByEntityType map[string]int `json:"by_entity_type"`
	FirstEntryAt string         `json:"first_entry_at"`
	LastEntryAt  string         `json:"last_entry_at"`
}
