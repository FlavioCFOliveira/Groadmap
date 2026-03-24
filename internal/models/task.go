// Package models defines the data structures for Groadmap entities.
package models

import (
	"fmt"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TaskStatus represents the current state of a task.
type TaskStatus string

// Task status constants following the state machine in SPEC/DATA_FORMATS.md.
const (
	StatusBacklog   TaskStatus = "BACKLOG"
	StatusSprint    TaskStatus = "SPRINT"
	StatusDoing     TaskStatus = "DOING"
	StatusTesting   TaskStatus = "TESTING"
	StatusCompleted TaskStatus = "COMPLETED"
)

// TaskType represents the classification of a task.
type TaskType string

// Task type constants as defined in SPEC/MODELS.md.
const (
	TypeUserStory   TaskType = "USER_STORY"
	TypeTask        TaskType = "TASK"
	TypeBug         TaskType = "BUG"
	TypeSubTask     TaskType = "SUB_TASK"
	TypeEpic        TaskType = "EPIC"
	TypeRefactor    TaskType = "REFACTOR"
	TypeChore       TaskType = "CHORE"
	TypeSpike       TaskType = "SPIKE"
	TypeDesignUX    TaskType = "DESIGN_UX"
	TypeImprovement TaskType = "IMPROVEMENT"
)

// ValidTaskTypes contains all valid task types.
var ValidTaskTypes = []TaskType{
	TypeUserStory,
	TypeTask,
	TypeBug,
	TypeSubTask,
	TypeEpic,
	TypeRefactor,
	TypeChore,
	TypeSpike,
	TypeDesignUX,
	TypeImprovement,
}

// validTypeMap provides O(1) lookup for type validation.
var validTypeMap = map[string]TaskType{
	"USER_STORY":  TypeUserStory,
	"TASK":        TypeTask,
	"BUG":         TypeBug,
	"SUB_TASK":    TypeSubTask,
	"EPIC":        TypeEpic,
	"REFACTOR":    TypeRefactor,
	"CHORE":       TypeChore,
	"SPIKE":       TypeSpike,
	"DESIGN_UX":   TypeDesignUX,
	"IMPROVEMENT": TypeImprovement,
}

// IsValidTaskType checks if a string is a valid task type.
func IsValidTaskType(s string) bool {
	_, ok := validTypeMap[s]
	return ok
}

// ParseTaskType parses a string into a TaskType.
func ParseTaskType(s string) (TaskType, error) {
	if taskType, ok := validTypeMap[s]; ok {
		return taskType, nil
	}
	return "", fmt.Errorf("invalid task type: %q", s)
}

// ValidTaskStatuses contains all valid task statuses.
var ValidTaskStatuses = []TaskStatus{
	StatusBacklog,
	StatusSprint,
	StatusDoing,
	StatusTesting,
	StatusCompleted,
}

// validStatusMap provides O(1) lookup for status validation.
// Initialized once at package initialization for performance.
var validStatusMap = map[string]TaskStatus{
	"BACKLOG":   StatusBacklog,
	"SPRINT":    StatusSprint,
	"DOING":     StatusDoing,
	"TESTING":   StatusTesting,
	"COMPLETED": StatusCompleted,
}

// IsValidTaskStatus checks if a string is a valid task status.
// Uses O(1) map lookup instead of O(n) slice iteration.
func IsValidTaskStatus(s string) bool {
	_, ok := validStatusMap[s]
	return ok
}

// ParseTaskStatus parses a string into a TaskStatus.
// Uses O(1) map lookup for validation.
func ParseTaskStatus(s string) (TaskStatus, error) {
	if status, ok := validStatusMap[s]; ok {
		return status, nil
	}
	return "", fmt.Errorf("invalid task status: %q", s)
}

// CanTransitionTo checks if a status transition is valid according to the state machine.
// See SPEC/STATE_MACHINE.md for the state diagram.
// Returns false if:
// - The current status is not a valid task status
// - The transition is not allowed according to the state machine rules
func (ts TaskStatus) CanTransitionTo(newStatus TaskStatus) bool {
	// Validate current status is a valid task status
	if !IsValidTaskStatus(string(ts)) {
		return false
	}

	// Validate target status is a valid task status
	if !IsValidTaskStatus(string(newStatus)) {
		return false
	}

	// Define valid transitions
	transitions := map[TaskStatus][]TaskStatus{
		StatusBacklog:   {StatusSprint},
		StatusSprint:    {StatusBacklog, StatusDoing},
		StatusDoing:     {StatusSprint, StatusTesting},
		StatusTesting:   {StatusDoing, StatusCompleted},
		StatusCompleted: {StatusBacklog},
	}

	validTargets, ok := transitions[ts]
	if !ok {
		return false
	}

	for _, target := range validTargets {
		if target == newStatus {
			return true
		}
	}
	return false
}

// ValidateStatusTransition validates a status transition and returns a detailed error if invalid.
// Use this when you need to provide specific error messages to users.
func ValidateStatusTransition(currentStatus, newStatus string) error {
	// Validate current status
	if !IsValidTaskStatus(currentStatus) {
		return fmt.Errorf("invalid current status: %q", currentStatus)
	}

	// Validate new status
	if !IsValidTaskStatus(newStatus) {
		return fmt.Errorf("invalid target status: %q", newStatus)
	}

	current := TaskStatus(currentStatus)
	target := TaskStatus(newStatus)

	if !current.CanTransitionTo(target) {
		return fmt.Errorf("cannot transition from %q to %q", currentStatus, newStatus)
	}

	return nil
}

// GetValidTransitions returns the list of valid next statuses for a given status.
func GetValidTransitions(status TaskStatus) []TaskStatus {
	transitions := map[TaskStatus][]TaskStatus{
		StatusBacklog:   {StatusSprint},
		StatusSprint:    {StatusBacklog, StatusDoing},
		StatusDoing:     {StatusSprint, StatusTesting},
		StatusTesting:   {StatusDoing, StatusCompleted},
		StatusCompleted: {StatusBacklog},
	}

	if valid, ok := transitions[status]; ok {
		return valid
	}
	return nil
}

// Task field size limits
const (
	MaxTaskTitle                  = 255
	MaxTaskFunctionalRequirements = 4096
	MaxTaskTechnicalRequirements  = 4096
	MaxTaskAcceptanceCriteria     = 4096
	MaxTaskSpecialists            = 500
	MaxTaskCompletionSummary      = 4096
)

// Task represents a task in the roadmap.
// Field order optimized for memory layout (zero padding on 64-bit systems).
// Groups: Content fields (strings), Tracking fields (pointers), Metadata (ints).
type Task struct {
	// Group 1: Content fields - frequently accessed together (112 bytes total)
	Title                  string     `json:"title"`                   // Task title/summary
	Status                 TaskStatus `json:"status"`                  // Current status
	Type                   TaskType   `json:"type"`                    // Task classification
	FunctionalRequirements string     `json:"functional_requirements"` // Why: functional requirements
	TechnicalRequirements  string     `json:"technical_requirements"`  // How: technical description
	AcceptanceCriteria     string     `json:"acceptance_criteria"`     // How to verify: completion criteria
	CreatedAt              string     `json:"created_at"`              // ISO 8601 UTC, auto-set on creation

	// Group 2: Nullable tracking fields - lifecycle timestamps and hierarchy
	Specialists       *string `json:"specialists"`        // Comma-separated specialists
	StartedAt         *string `json:"started_at"`         // ISO 8601 UTC, auto-set on DOING transition
	TestedAt          *string `json:"tested_at"`          // ISO 8601 UTC, auto-set on TESTING transition
	ClosedAt          *string `json:"closed_at"`          // ISO 8601 UTC, auto-set on COMPLETED transition
	CompletionSummary *string `json:"completion_summary"` // Optional summary set on TESTING → COMPLETED transition
	ParentTaskID      *int    `json:"parent_task_id"`     // NULL for top-level tasks; non-NULL links to parent task

	// Group 3: Numeric metadata fields
	ID           int `json:"id"`            // Primary key
	Priority     int `json:"priority"`      // 0-9 priority level
	Severity     int `json:"severity"`      // 0-9 severity level
	SubtaskCount int `json:"subtask_count"` // Computed: number of direct subtasks (not stored in DB)

	// Dependency fields (fetched from task_dependencies table)
	DependsOn []int `json:"depends_on"` // IDs of tasks this task depends on
	Blocks    []int `json:"blocks"`     // IDs of tasks that depend on this task
}

// Validate checks if the task data is valid.
func (t *Task) Validate() error {
	if t.Title == "" {
		return fmt.Errorf("title is required")
	}
	if len(t.Title) > MaxTaskTitle {
		return fmt.Errorf("%w: title exceeds maximum length of %d characters", utils.ErrFieldTooLarge, MaxTaskTitle)
	}
	if t.FunctionalRequirements == "" {
		return fmt.Errorf("functional_requirements is required")
	}
	if len(t.FunctionalRequirements) > MaxTaskFunctionalRequirements {
		return fmt.Errorf("%w: functional_requirements exceeds maximum length of %d characters", utils.ErrFieldTooLarge, MaxTaskFunctionalRequirements)
	}
	if t.TechnicalRequirements == "" {
		return fmt.Errorf("technical_requirements is required")
	}
	if len(t.TechnicalRequirements) > MaxTaskTechnicalRequirements {
		return fmt.Errorf("%w: technical_requirements exceeds maximum length of %d characters", utils.ErrFieldTooLarge, MaxTaskTechnicalRequirements)
	}
	if t.AcceptanceCriteria == "" {
		return fmt.Errorf("acceptance_criteria is required")
	}
	if len(t.AcceptanceCriteria) > MaxTaskAcceptanceCriteria {
		return fmt.Errorf("%w: acceptance_criteria exceeds maximum length of %d characters", utils.ErrFieldTooLarge, MaxTaskAcceptanceCriteria)
	}
	if t.Specialists != nil && len(*t.Specialists) > MaxTaskSpecialists {
		return fmt.Errorf("%w: specialists exceeds maximum length of %d characters", utils.ErrFieldTooLarge, MaxTaskSpecialists)
	}
	if t.Priority < 0 || t.Priority > 9 {
		return fmt.Errorf("priority must be between 0 and 9, got %d", t.Priority)
	}
	if t.Severity < 0 || t.Severity > 9 {
		return fmt.Errorf("severity must be between 0 and 9, got %d", t.Severity)
	}
	if !IsValidTaskStatus(string(t.Status)) {
		return fmt.Errorf("invalid status: %q", t.Status)
	}
	if !IsValidTaskType(string(t.Type)) {
		return fmt.Errorf("invalid type: %q", t.Type)
	}

	// Validate dates
	if err := t.validateDates(); err != nil {
		return err
	}

	return nil
}

// validateDates validates task date fields.
// - created_at must not be in the future (with 1 minute tolerance)
// - closed_at must not be before created_at
// - tested_at must not be before started_at
// - started_at must not be before created_at
func (t *Task) validateDates() error {
	// Parse and validate created_at
	if t.CreatedAt != "" {
		createdTime, err := utils.ParseISO8601(t.CreatedAt)
		if err != nil {
			return fmt.Errorf("invalid created_at: %w", err)
		}

		// Validate created_at is not in the future
		if err := utils.ValidateNotFuture(createdTime); err != nil {
			return fmt.Errorf("invalid created_at: %w", err)
		}

		// Parse and validate started_at if present
		if t.StartedAt != nil && *t.StartedAt != "" {
			startedTime, err := utils.ParseISO8601(*t.StartedAt)
			if err != nil {
				return fmt.Errorf("invalid started_at: %w", err)
			}
			if err := utils.ValidateDateOrder(createdTime, startedTime); err != nil {
				return fmt.Errorf("invalid date order: started_at before created_at: %w", err)
			}
		}

		// Parse and validate tested_at if present
		if t.TestedAt != nil && *t.TestedAt != "" {
			testedTime, err := utils.ParseISO8601(*t.TestedAt)
			if err != nil {
				return fmt.Errorf("invalid tested_at: %w", err)
			}
			if err := utils.ValidateDateOrder(createdTime, testedTime); err != nil {
				return fmt.Errorf("invalid date order: tested_at before created_at: %w", err)
			}
		}

		// Parse and validate closed_at if present
		if t.ClosedAt != nil && *t.ClosedAt != "" {
			closedTime, err := utils.ParseISO8601(*t.ClosedAt)
			if err != nil {
				return fmt.Errorf("invalid closed_at: %w", err)
			}
			if err := utils.ValidateDateOrder(createdTime, closedTime); err != nil {
				return fmt.Errorf("invalid date order: closed_at before created_at: %w", err)
			}
		}
	}

	return nil
}

// IsComplete returns true if the task status is COMPLETED.
func (t *Task) IsComplete() bool {
	return t.Status == StatusCompleted
}

// ParseSpecialists parses a comma-separated list of specialists.
func ParseSpecialists(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// FormatSpecialists formats a slice of specialists as a comma-separated string.
func FormatSpecialists(specialists []string) string {
	if len(specialists) == 0 {
		return ""
	}
	return strings.Join(specialists, ",")
}

// TaskUpdate represents a type-safe update operation for tasks.
// Use pointer fields to indicate which fields should be updated (nil = no change).
// This provides compile-time type safety and deterministic SQL generation
// compared to map[string]interface{}.
type TaskUpdate struct {
	Title                  *string
	FunctionalRequirements *string
	TechnicalRequirements  *string
	AcceptanceCriteria     *string
	Specialists            *string
	Priority               *int
	Severity               *int
}

// HasChanges returns true if any field is set to be updated.
func (u *TaskUpdate) HasChanges() bool {
	return u.Title != nil || u.FunctionalRequirements != nil || u.TechnicalRequirements != nil ||
		u.AcceptanceCriteria != nil || u.Specialists != nil || u.Priority != nil || u.Severity != nil
}

// Validate checks if the update values are valid.
func (u *TaskUpdate) Validate() error {
	if u.Title != nil && len(*u.Title) > MaxTaskTitle {
		return fmt.Errorf("title exceeds maximum length of %d characters", MaxTaskTitle)
	}
	if u.FunctionalRequirements != nil && len(*u.FunctionalRequirements) > MaxTaskFunctionalRequirements {
		return fmt.Errorf("functional_requirements exceeds maximum length of %d characters", MaxTaskFunctionalRequirements)
	}
	if u.TechnicalRequirements != nil && len(*u.TechnicalRequirements) > MaxTaskTechnicalRequirements {
		return fmt.Errorf("technical_requirements exceeds maximum length of %d characters", MaxTaskTechnicalRequirements)
	}
	if u.AcceptanceCriteria != nil && len(*u.AcceptanceCriteria) > MaxTaskAcceptanceCriteria {
		return fmt.Errorf("acceptance_criteria exceeds maximum length of %d characters", MaxTaskAcceptanceCriteria)
	}
	if u.Specialists != nil && len(*u.Specialists) > MaxTaskSpecialists {
		return fmt.Errorf("specialists exceeds maximum length of %d characters", MaxTaskSpecialists)
	}
	if u.Priority != nil && (*u.Priority < 0 || *u.Priority > 9) {
		return fmt.Errorf("priority must be between 0 and 9, got %d", *u.Priority)
	}
	if u.Severity != nil && (*u.Severity < 0 || *u.Severity > 9) {
		return fmt.Errorf("severity must be between 0 and 9, got %d", *u.Severity)
	}
	return nil
}
