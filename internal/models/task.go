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
	MaxTaskDescription    = 500
	MaxTaskAction         = 1000
	MaxTaskExpectedResult = 1000
	MaxTaskSpecialists    = 500
)

// Task represents a task in the roadmap.
// Field order optimized for memory alignment (largest fields first).
// Layout: 16-byte fields, then 8-byte fields to minimize padding.
type Task struct {
	// 16-byte fields (string header: data pointer + length)
	Description    string     `json:"description"`
	Action         string     `json:"action"`
	ExpectedResult string     `json:"expected_result"`
	CreatedAt      string     `json:"created_at"` // ISO 8601 UTC
	Status         TaskStatus `json:"status"`     // string underlying type

	// 8-byte fields (pointers and integers)
	Specialists *string `json:"specialists"`  // Nullable
	CompletedAt *string `json:"completed_at"` // ISO 8601 UTC, nullable
	ID          int     `json:"id"`
	Priority    int     `json:"priority"`
	Severity    int     `json:"severity"`
}

// Validate checks if the task data is valid.
func (t *Task) Validate() error {
	if t.Description == "" {
		return fmt.Errorf("description is required")
	}
	if len(t.Description) > MaxTaskDescription {
		return fmt.Errorf("%w: description exceeds maximum length of %d characters", utils.ErrFieldTooLarge, MaxTaskDescription)
	}
	if t.Action == "" {
		return fmt.Errorf("action is required")
	}
	if len(t.Action) > MaxTaskAction {
		return fmt.Errorf("%w: action exceeds maximum length of %d characters", utils.ErrFieldTooLarge, MaxTaskAction)
	}
	if t.ExpectedResult == "" {
		return fmt.Errorf("expected_result is required")
	}
	if len(t.ExpectedResult) > MaxTaskExpectedResult {
		return fmt.Errorf("%w: expected_result exceeds maximum length of %d characters", utils.ErrFieldTooLarge, MaxTaskExpectedResult)
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

	// Validate dates
	if err := t.validateDates(); err != nil {
		return err
	}

	return nil
}

// validateDates validates task date fields.
// - created_at must not be in the future (with 1 minute tolerance)
// - completed_at must not be before created_at
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

		// Parse and validate completed_at if present
		if t.CompletedAt != nil && *t.CompletedAt != "" {
			completedTime, err := utils.ParseISO8601(*t.CompletedAt)
			if err != nil {
				return fmt.Errorf("invalid completed_at: %w", err)
			}

			// Validate completed_at is not before created_at
			if err := utils.ValidateDateOrder(createdTime, completedTime); err != nil {
				return fmt.Errorf("invalid date order: %w", err)
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
	Description    *string
	Action         *string
	ExpectedResult *string
	Specialists    *string
	Priority       *int
	Severity       *int
}

// HasChanges returns true if any field is set to be updated.
func (u *TaskUpdate) HasChanges() bool {
	return u.Description != nil || u.Action != nil || u.ExpectedResult != nil ||
		u.Specialists != nil || u.Priority != nil || u.Severity != nil
}

// Validate checks if the update values are valid.
func (u *TaskUpdate) Validate() error {
	if u.Description != nil && len(*u.Description) > MaxTaskDescription {
		return fmt.Errorf("description exceeds maximum length of %d characters", MaxTaskDescription)
	}
	if u.Action != nil && len(*u.Action) > MaxTaskAction {
		return fmt.Errorf("action exceeds maximum length of %d characters", MaxTaskAction)
	}
	if u.ExpectedResult != nil && len(*u.ExpectedResult) > MaxTaskExpectedResult {
		return fmt.Errorf("expected_result exceeds maximum length of %d characters", MaxTaskExpectedResult)
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
