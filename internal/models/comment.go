package models

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// Sentinel errors for comment validation.
var (
	// ErrInvalidCommentType indicates a comment type the entity being commented
	// on does not accept: either a value outside the enum entirely, or a value
	// that is valid for the other entity only. Its text is the opening clause of
	// the message SPEC/COMMANDS.md pins, so the Parse helpers chain it as a
	// message fragment through a second %w verb rather than appending it.
	ErrInvalidCommentType = errors.New("invalid comment type")

	// ErrCommentBodyRequired indicates a comment body that is empty or contains
	// nothing but whitespace. The database accepts an empty body by design (no
	// CHECK forbids it, see SPEC/DATABASE.md), so this rule is enforced here and
	// nowhere else.
	ErrCommentBodyRequired = errors.New("body is required")
)

// CommentType classifies a comment. There is one enum with seven values; each
// entity accepts a subset of it, and the subset that applies is the one of the
// entity being commented on (SPEC/MODELS.md § Comment Type).
type CommentType string

// Comment type constants as defined in SPEC/MODELS.md § Comment Type.
const (
	CommentFinding    CommentType = "FINDING"
	CommentHypothesis CommentType = "HYPOTHESIS"
	CommentTest       CommentType = "TEST"
	CommentDecision   CommentType = "DECISION"
	CommentProgress   CommentType = "PROGRESS"
	CommentUpdate     CommentType = "UPDATE"
	CommentNote       CommentType = "NOTE"
)

// Entity names as they appear in the validation message SPEC/COMMANDS.md pins
// ("... for a task comment", "... for a sprint comment").
const (
	commentEntityTask   = "task"
	commentEntitySprint = "sprint"
)

// ValidTaskCommentTypes lists, in canonical order, the seven values a task
// comment accepts.
var ValidTaskCommentTypes = []CommentType{
	CommentFinding,
	CommentHypothesis,
	CommentTest,
	CommentDecision,
	CommentProgress,
	CommentUpdate,
	CommentNote,
}

// ValidSprintCommentTypes lists, in canonical order, the four values a sprint
// comment accepts. The task-only values HYPOTHESIS, TEST and NOTE are absent:
// a sprint comment records the progression of the work, not the work itself.
var ValidSprintCommentTypes = []CommentType{
	CommentFinding,
	CommentDecision,
	CommentProgress,
	CommentUpdate,
}

// validTaskCommentTypeMap provides O(1) lookup for task comment type validation.
var validTaskCommentTypeMap = map[string]CommentType{
	"FINDING":    CommentFinding,
	"HYPOTHESIS": CommentHypothesis,
	"TEST":       CommentTest,
	"DECISION":   CommentDecision,
	"PROGRESS":   CommentProgress,
	"UPDATE":     CommentUpdate,
	"NOTE":       CommentNote,
}

// validSprintCommentTypeMap provides O(1) lookup for sprint comment type
// validation.
var validSprintCommentTypeMap = map[string]CommentType{
	"FINDING":  CommentFinding,
	"DECISION": CommentDecision,
	"PROGRESS": CommentProgress,
	"UPDATE":   CommentUpdate,
}

// The accepted sets rendered for the validation message, computed once. Package
// variable initialisation order resolves the dependency on the slices above, so
// no init() function is needed.
var (
	taskCommentTypeList   = FormatCommentTypes(ValidTaskCommentTypes)
	sprintCommentTypeList = FormatCommentTypes(ValidSprintCommentTypes)
)

// FormatCommentTypes renders a comment type set as the comma-separated list the
// validation message, the command help, and the AI Agent Contract all publish
// (SPEC/COMMANDS.md § Comment Field Constraints).
func FormatCommentTypes(types []CommentType) string {
	values := make([]string, 0, len(types))
	for _, t := range types {
		values = append(values, string(t))
	}
	return strings.Join(values, ", ")
}

// IsValidTaskCommentType reports whether s is one of the seven values a task
// comment accepts. Uses O(1) map lookup instead of O(n) slice iteration.
func IsValidTaskCommentType(s string) bool {
	_, ok := validTaskCommentTypeMap[s]
	return ok
}

// IsValidSprintCommentType reports whether s is one of the four values a sprint
// comment accepts. The task-only values HYPOTHESIS, TEST and NOTE return false.
// Uses O(1) map lookup instead of O(n) slice iteration.
func IsValidSprintCommentType(s string) bool {
	_, ok := validSprintCommentTypeMap[s]
	return ok
}

// ParseTaskCommentType parses a string into the CommentType of a task comment.
// An unaccepted value yields the message SPEC/COMMANDS.md § Add Task Comment
// pins, naming the seven task values, and chains utils.ErrValidation so it maps
// to exit code 6.
func ParseTaskCommentType(s string) (CommentType, error) {
	return parseCommentType(s, commentEntityTask, validTaskCommentTypeMap, taskCommentTypeList)
}

// ParseSprintCommentType parses a string into the CommentType of a sprint
// comment. A task-only value (HYPOTHESIS, TEST, NOTE) is rejected exactly like
// a value outside the enum, with the message SPEC/COMMANDS.md § Add Sprint
// Comment pins, naming the four sprint values, and chains utils.ErrValidation
// so it maps to exit code 6.
func ParseSprintCommentType(s string) (CommentType, error) {
	return parseCommentType(s, commentEntitySprint, validSprintCommentTypeMap, sprintCommentTypeList)
}

// parseCommentType is the per-entity lookup both Parse helpers share. The
// rejection message names the valid set of the entity in question rather than
// the enum as a whole, which is what tells a caller that a type valid on the
// other entity is not valid here.
//
// Both utils.ErrValidation and ErrInvalidCommentType are chained through %w
// verbs: the first renders the "validation error: " prefix and carries the exit
// code, the second is the message's own opening clause. Appending the sentinel
// after the type list instead would have altered the pinned text.
func parseCommentType(s, entity string, lookup map[string]CommentType, validList string) (CommentType, error) {
	if commentType, ok := lookup[s]; ok {
		return commentType, nil
	}
	return "", fmt.Errorf("%w: %w %q for a %s comment; valid types: %s",
		utils.ErrValidation, ErrInvalidCommentType, s, entity, validList)
}

// Comment field size limits
const (
	// MaxCommentBody is the maximum length of a comment body, counted in
	// characters. The unit is deliberate: the database enforces the same cap
	// with CHECK(length(body) <= 4096), and SQLite's length() counts characters
	// on a TEXT value, so counting bytes here would make this layer stricter
	// than the schema for any multi-byte body.
	MaxCommentBody = 4096
)

// TaskComment represents one comment attached to a task.
// Field order optimized for memory layout (72 bytes, zero padding on 64-bit systems).
type TaskComment struct {
	UpdatedAt *string     `json:"updated_at"` // ISO 8601 UTC; null until the comment is first edited
	Type      CommentType `json:"type"`       // Mandatory classification; one of the seven task values
	Body      string      `json:"body"`       // Comment text, 1-4096 chars
	CreatedAt string      `json:"created_at"` // ISO 8601 UTC, auto-set on creation, never changed
	ID        int         `json:"id"`         // Primary key, unique within task_comments only
	TaskID    int         `json:"task_id"`    // Owning task
}

// SprintComment represents one comment attached to a sprint.
// Field order optimized for memory layout (72 bytes, zero padding on 64-bit systems).
type SprintComment struct {
	UpdatedAt *string     `json:"updated_at"` // ISO 8601 UTC; null until the comment is first edited
	Type      CommentType `json:"type"`       // Mandatory classification; one of the four sprint values
	Body      string      `json:"body"`       // Comment text, 1-4096 chars
	CreatedAt string      `json:"created_at"` // ISO 8601 UTC, auto-set on creation, never changed
	ID        int         `json:"id"`         // Primary key, unique within sprint_comments only
	SprintID  int         `json:"sprint_id"`  // Owning sprint
}

// Validate checks that the task comment carries an accepted type and a valid
// body. The type is checked first, matching the order SPEC/COMMANDS.md pins.
//
// Every error returned maps to exit code 6.
func (c *TaskComment) Validate() error {
	if _, err := ParseTaskCommentType(string(c.Type)); err != nil {
		return err
	}
	_, err := ValidateCommentBody(c.Body)
	return err
}

// Validate checks that the sprint comment carries an accepted type and a valid
// body. The type is checked against the four sprint values, so a task-only
// value is rejected here even though the same value passes on a TaskComment.
//
// Every error returned maps to exit code 6.
func (c *SprintComment) Validate() error {
	if _, err := ParseSprintCommentType(string(c.Type)); err != nil {
		return err
	}
	_, err := ValidateCommentBody(c.Body)
	return err
}

// NormalizeCommentBody returns the canonical storable form of a comment body:
// leading and trailing whitespace removed, interior line breaks untouched
// (SPEC/COMMANDS.md § Comment Body Input Source and Precedence, rule 5).
//
// A body that normalizes to the empty string counts as absent. The command
// layer uses that to distinguish "no body supplied" — a missing parameter,
// exit code 2 — from a body that is present but invalid.
func NormalizeCommentBody(body string) string {
	return strings.TrimSpace(body)
}

// ValidateCommentBody validates a comment body as supplied and returns the
// canonical form to store. It owns every rule the body is subject to, so both
// entities and every command that writes a comment share one implementation.
//
// The rules, in the order SPEC/COMMANDS.md pins them, are: non-empty after
// trimming, at most MaxCommentBody characters, and no forbidden control
// character. Each failure chains a utils sentinel that maps to exit code 6.
func ValidateCommentBody(body string) (string, error) {
	stored := NormalizeCommentBody(body)
	if stored == "" {
		return "", fmt.Errorf("%w: %w", utils.ErrValidation, ErrCommentBodyRequired)
	}

	// Characters, not bytes: see MaxCommentBody. The cap applies to the stored
	// form, because trimming happens before validation and before storage.
	if utf8.RuneCountInString(stored) > MaxCommentBody {
		return "", fmt.Errorf("%w: body exceeds maximum length of %d characters", utils.ErrFieldTooLarge, MaxCommentBody)
	}

	// The control-character rule is checked against the body as supplied, not
	// against the trimmed form. strings.TrimSpace strips VT (0x0B) and FF
	// (0x0C), both of which the SPEC forbids, so validating the trimmed form
	// would let a leading or trailing VT or FF pass unreported instead of
	// rejecting the input that contains it.
	if err := utils.ValidateNoControlChars(body, "body"); err != nil {
		return "", err
	}

	return stored, nil
}
