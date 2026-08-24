// Package models defines the data structures for Groadmap entities.
package models

import (
	"errors"
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// Sentinel errors for task validation.
//
// Each of these supplies the OPENING CLAUSE of the message it is returned in,
// through a %w verb at the front of the format string:
//
//	fmt.Errorf("%w: %q", ErrInvalidTaskType, s)  ->  invalid task type: "BOGUS"
//
// The literal must never restate the sentinel's own text. Building the same
// rejection as fmt.Errorf("invalid task type: %q: %w", s, ErrInvalidTaskType)
// renders that text twice, which is what users saw until this was corrected.
// internal/models/error_message_dedup_test.go pins every rendered message.
var (
	ErrInvalidTaskType      = errors.New("invalid task type")
	ErrInvalidTaskStatus    = errors.New("invalid task status")
	ErrInvalidStatus        = errors.New("invalid status")
	ErrInvalidType          = errors.New("invalid type")
	ErrInvalidCurrentStatus = errors.New("invalid current status")
	ErrInvalidTargetStatus  = errors.New("invalid target status")
	ErrCannotTransition     = errors.New("cannot transition")
	// The four field names below come from the shared definition in
	// internal/utils, not from a literal here, so a task's "is required" refusal
	// and its control-character and length refusals cannot end up calling one
	// field two things (SPEC/COMMANDS.md § Published Field Names in Validation
	// Messages). ErrTitleRequired is shared with sprint validation, which is
	// sound because the two entities publish the same name for their title.
	ErrTitleRequired         = errors.New(utils.RequiredFieldMessage(utils.FieldTaskTitle))
	ErrFuncReqRequired       = errors.New(utils.RequiredFieldMessage(utils.FieldTaskFunctionalRequirements))
	ErrTechReqRequired       = errors.New(utils.RequiredFieldMessage(utils.FieldTaskTechnicalRequirements))
	ErrAcceptanceCriteriaReq = errors.New(utils.RequiredFieldMessage(utils.FieldTaskAcceptanceCriteria))
	ErrPriorityOutOfRange    = errors.New("priority must be between 0 and 9")
	ErrSeverityOutOfRange    = errors.New("severity must be between 0 and 9")
	ErrInvalidCommitHash     = errors.New("invalid commit hash")
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
	return "", fmt.Errorf("%w: %q", ErrInvalidTaskType, s)
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
	return "", fmt.Errorf("%w: %q", ErrInvalidTaskStatus, s)
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

	// Define valid transitions. DOING's only valid target is TESTING:
	// STATE_MACHINE.md forbids DOING -> SPRINT (the SPRINT status is set
	// exclusively by `sprint add-tasks`, and the rejection rule blocks manual
	// transitions to SPRINT from any source). Finding #55.
	transitions := map[TaskStatus][]TaskStatus{
		StatusBacklog:   {StatusSprint},
		StatusSprint:    {StatusBacklog, StatusDoing},
		StatusDoing:     {StatusTesting},
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
		return fmt.Errorf("%w: %q", ErrInvalidCurrentStatus, currentStatus)
	}

	// Validate new status
	if !IsValidTaskStatus(newStatus) {
		return fmt.Errorf("%w: %q", ErrInvalidTargetStatus, newStatus)
	}

	current := TaskStatus(currentStatus)
	target := TaskStatus(newStatus)

	if !current.CanTransitionTo(target) {
		return fmt.Errorf("%w from %q to %q", ErrCannotTransition, currentStatus, newStatus)
	}

	return nil
}

// GetValidTransitions returns the list of valid next statuses for a given status.
func GetValidTransitions(status TaskStatus) []TaskStatus {
	// DOING -> SPRINT is intentionally absent: STATE_MACHINE.md forbids it
	// (SPRINT is set only by `sprint add-tasks`). Finding #55.
	transitions := map[TaskStatus][]TaskStatus{
		StatusBacklog:   {StatusSprint},
		StatusSprint:    {StatusBacklog, StatusDoing},
		StatusDoing:     {StatusTesting},
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
	MaxTaskCompletionSummary      = 4096
)

// Commit hash length bounds, in characters (SPEC/MODELS.md § Task, Commit Hash
// Constraint). The lower bound admits the conventional abbreviated hash; the
// upper bound admits both the 40-character SHA-1 hash and the 64-character
// SHA-256 hash a repository created with `git init --object-format=sha256`
// produces.
const (
	MinCommitHashLength = 7
	MaxCommitHashLength = 64
)

// Task represents a task in the roadmap.
// Field order optimized for memory layout (248 bytes, zero padding on 64-bit
// systems) and enforced by the govet:fieldalignment linter.
// Groups: Tracking fields (pointers), Content fields (strings), Dependencies
// (slices), Metadata (ints). See SPEC/MODELS.md § Memory Layout Optimization.
type Task struct {
	ParentTaskID           *int       `json:"parent_task_id"`
	CompletionSummary      *string    `json:"completion_summary"`
	CommitOpen             *string    `json:"commit_open"`
	CommitClose            *string    `json:"commit_close"`
	TestedAt               *string    `json:"tested_at"`
	ClosedAt               *string    `json:"closed_at"`
	StartedAt              *string    `json:"started_at"`
	AcceptanceCriteria     string     `json:"acceptance_criteria"`
	CreatedAt              string     `json:"created_at"`
	Status                 TaskStatus `json:"status"`
	TechnicalRequirements  string     `json:"technical_requirements"`
	FunctionalRequirements string     `json:"functional_requirements"`
	Type                   TaskType   `json:"type"`
	Title                  string     `json:"title"`
	DependsOn              []int      `json:"depends_on"`
	Blocks                 []int      `json:"blocks"`
	ID                     int        `json:"id"`
	Priority               int        `json:"priority"`
	Severity               int        `json:"severity"`
	SubtaskCount           int        `json:"subtask_count"`
}

// Validate checks if the task data is valid.
//
// Every length cap below measures CHARACTERS — Unicode code points — through
// utils.CheckFieldLength, which is the one place the unit is defined. These caps
// used to measure bytes with len(), which refused a title of 102 CJK characters
// for exceeding "255 characters" (rmp task 296). The value reaching this method
// is the trimmed one every writer stores, so the cap measures what the column
// holds.
func (t *Task) Validate() error {
	if t.Title == "" {
		return ErrTitleRequired
	}
	if err := utils.CheckFieldLength(t.Title, utils.FieldTaskTitle, MaxTaskTitle); err != nil {
		return err
	}
	if t.FunctionalRequirements == "" {
		return ErrFuncReqRequired
	}
	if err := utils.CheckFieldLength(t.FunctionalRequirements, utils.FieldTaskFunctionalRequirements, MaxTaskFunctionalRequirements); err != nil {
		return err
	}
	if t.TechnicalRequirements == "" {
		return ErrTechReqRequired
	}
	if err := utils.CheckFieldLength(t.TechnicalRequirements, utils.FieldTaskTechnicalRequirements, MaxTaskTechnicalRequirements); err != nil {
		return err
	}
	if t.AcceptanceCriteria == "" {
		return ErrAcceptanceCriteriaReq
	}
	if err := utils.CheckFieldLength(t.AcceptanceCriteria, utils.FieldTaskAcceptanceCriteria, MaxTaskAcceptanceCriteria); err != nil {
		return err
	}
	if t.Priority < 0 || t.Priority > 9 {
		// Chain utils.ErrValidation so this maps to exit 6 (invalid data) per
		// SPEC/ARCHITECTURE.md; the local ErrPriorityOutOfRange sentinel is kept
		// for internal callers. Previously only the local sentinel was chained,
		// so handleError fell through to exit 1 (finding #46).
		return fmt.Errorf("%w: %w, got %d", utils.ErrValidation, ErrPriorityOutOfRange, t.Priority)
	}
	if t.Severity < 0 || t.Severity > 9 {
		return fmt.Errorf("%w: %w, got %d", utils.ErrValidation, ErrSeverityOutOfRange, t.Severity)
	}
	if !IsValidTaskStatus(string(t.Status)) {
		return fmt.Errorf("%w: %q", ErrInvalidStatus, t.Status)
	}
	if !IsValidTaskType(string(t.Type)) {
		return fmt.Errorf("%w: %q", ErrInvalidType, t.Type)
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

// NormalizeCommitHash validates a git commit hash and returns it in the single
// form Groadmap stores: lowercase.
//
// The rule is SPEC/MODELS.md § Task, Commit Hash Constraint, and it is exactly
// three clauses. The value must consist solely of hexadecimal characters, its
// length must be between MinCommitHashLength and MaxCommitHashLength inclusive,
// and any letter case is accepted on input and normalised to lowercase on
// output — so two callers who supply the same hash in different cases produce
// the same stored value. The database enforces the same rule as a backstop
// through the CHECK constraint on each commit column, and that CHECK is
// case-sensitive, so skipping this normalisation is a failed write rather than
// an unnormalised row (SPEC/DATABASE.md § Commit Hash Format Constraint).
//
// NO OTHER TRANSFORMATION IS APPLIED. In particular the value is NOT trimmed: a
// leading or trailing space is a non-hexadecimal character and is rejected like
// any other. An empty value is rejected because its length is below the lower
// bound.
//
// Groadmap never derives the value and never verifies that it names a commit
// that exists in any repository: it runs no git command and reads no working
// directory, so only the format is checked.
//
// Every rejection chains utils.ErrValidation, which SPEC/ARCHITECTURE.md maps to
// exit code 6, and additionally ErrInvalidCommitHash for callers that need to
// discriminate this failure from other validation failures.
//
// The returned string aliases the input when it is already lowercase, so the
// common path allocates nothing.
func NormalizeCommitHash(hash string) (string, error) {
	// Length is measured in bytes. Every accepted value is pure ASCII, where
	// bytes and characters coincide, so this agrees with the character count
	// SPEC/MODELS.md states for every value the function accepts; a multi-byte
	// value that slips past this gate is then rejected by the hexadecimal check
	// below.
	if len(hash) < MinCommitHashLength || len(hash) > MaxCommitHashLength {
		return "", fmt.Errorf("%w: commit hash must be between %d and %d hexadecimal characters, got %d: %w",
			utils.ErrValidation, MinCommitHashLength, MaxCommitHashLength, len(hash), ErrInvalidCommitHash)
	}

	// Single pass: classify each byte and lowercase in place, copying the input
	// only once an uppercase byte proves a copy is needed.
	var lowered []byte
	for i := range len(hash) {
		c := hash[i]
		switch {
		case (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'):
			// Already in the stored form.
		case c >= 'A' && c <= 'F':
			if lowered == nil {
				lowered = []byte(hash)
			}
			lowered[i] = c + ('a' - 'A')
		default:
			return "", fmt.Errorf("%w: commit hash %q contains a non-hexadecimal character at position %d: %w",
				utils.ErrValidation, hash, i, ErrInvalidCommitHash)
		}
	}

	if lowered == nil {
		return hash, nil
	}
	return string(lowered), nil
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
	Priority               *int
	Severity               *int
}

// HasChanges returns true if any field is set to be updated.
func (u *TaskUpdate) HasChanges() bool {
	return u.Title != nil || u.FunctionalRequirements != nil || u.TechnicalRequirements != nil ||
		u.AcceptanceCriteria != nil || u.Priority != nil || u.Severity != nil
}

// Validate checks if the update values are valid.
//
// The length caps measure CHARACTERS through utils.CheckFieldLength, the same
// unit and the same helper Task.Validate uses, and they are applied in the order
// SPEC/COMMANDS.md declares the fields — title, functional, technical,
// acceptance — so an update that breaks the cap on two fields always names the
// same one.
func (u *TaskUpdate) Validate() error {
	for _, f := range []struct {
		value *string
		field utils.Field
		limit int
	}{
		{u.Title, utils.FieldTaskTitle, MaxTaskTitle},
		{u.FunctionalRequirements, utils.FieldTaskFunctionalRequirements, MaxTaskFunctionalRequirements},
		{u.TechnicalRequirements, utils.FieldTaskTechnicalRequirements, MaxTaskTechnicalRequirements},
		{u.AcceptanceCriteria, utils.FieldTaskAcceptanceCriteria, MaxTaskAcceptanceCriteria},
	} {
		if f.value == nil {
			continue
		}
		if err := utils.CheckFieldLength(*f.value, f.field, f.limit); err != nil {
			return err
		}
	}
	if u.Priority != nil && (*u.Priority < 0 || *u.Priority > 9) {
		return fmt.Errorf("%w: %w, got %d", utils.ErrValidation, ErrPriorityOutOfRange, *u.Priority)
	}
	if u.Severity != nil && (*u.Severity < 0 || *u.Severity > 9) {
		return fmt.Errorf("%w: %w, got %d", utils.ErrValidation, ErrSeverityOutOfRange, *u.Severity)
	}
	return nil
}
