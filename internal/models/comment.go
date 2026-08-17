package models

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
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

// commentBodyReadChunk is the size of one read from the body stream. It bounds
// how much unvalidated input is held at any instant; the retained state is
// bounded separately by the rune budget in commentBodyScanner.
const commentBodyReadChunk = 4096

// ReadCommentBody reads a comment body from src under a BOUNDED memory budget
// and returns the value the body rules must be applied to. It returns an error
// only for an I/O failure of the stream: every VERDICT about the body is left to
// ValidateCommentBody, so the validation ORDER SPEC/COMMANDS.md pins for the
// comment subcommands is unchanged (the parent-exists check at step 6 still
// precedes the body's length and control-character rules at step 7).
//
// An empty return value means the stream carried no body at all — the caller
// decides whether that is a missing parameter or a no-op, exactly as it does for
// an absent --body flag.
//
// # Why this is not io.ReadAll
//
// The previous implementation drained the stream to EOF and only then measured
// it against the 4096-character cap, so the process buffered whatever a hostile
// writer chose to send before rejecting it. Measured on this program: 64 MiB of
// input produced 246 MB of peak RSS, 256 MiB produced 868 MB, and 512 MiB
// produced 1.27 GB — an amplification of roughly 2.5x to 4x (io.ReadAll's
// doubling growth plus the []byte-to-string copy) for input that was always
// going to be refused. A pipeline that feeds rmp from an untrusted source (a
// fetched file, an agent's tool output) could therefore drive the machine into
// swap or the OOM killer through a command whose largest acceptable input is
// about 16 KiB (CWE-400 / CWE-789: allocation sized by attacker-controlled
// input).
//
// # What it returns, and why every verdict is unchanged
//
// The returned value is not a truncation of the input: it is a value chosen so
// that ValidateCommentBody reaches the SAME verdict on it that it would have
// reached on the whole stream.
//
//   - A body within the cap and free of forbidden control characters returns as
//     its trimmed form — byte for byte what TrimSpace would have produced,
//     including bytes that are not valid UTF-8, which are retained as supplied
//     rather than re-encoded through the replacement rune.
//   - A body whose trimmed content exceeds the cap returns a prefix that is
//     itself over the cap, so ValidateCommentBody reports the identical
//     utils.ErrFieldTooLarge. Reading stops there: no later byte could change
//     that verdict, and continuing to read is the cost this function exists to
//     avoid.
//   - A forbidden control character that occurred in the leading or trailing
//     whitespace — which TrimSpace would have removed, which is why the rule is
//     applied to the body AS SUPPLIED — is carried back by appending that one
//     whitespace rune to the returned value. It is stripped again by the trim
//     inside ValidateCommentBody, so it cannot affect the length verdict or the
//     stored text, and it is only ever appended to a value that is about to be
//     refused.
//   - A body whose content is empty returns "" with no error even when its
//     whitespace held a forbidden control character: TrimSpace strips VT and FF,
//     so such a body counts as absent (exit code 2), which is what the
//     read-everything-then-TrimSpace implementation did.
func ReadCommentBody(src io.Reader) (string, error) {
	var (
		s     commentBodyScanner
		chunk = make([]byte, commentBodyReadChunk)
		carry []byte
	)

	for !s.done {
		n, err := src.Read(chunk)
		if n > 0 {
			carry = append(carry, chunk[:n]...)
			consumed := s.scan(carry, false)
			carry = append(carry[:0], carry[consumed:]...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", fmt.Errorf("%w: reading the comment body from standard input: %v", utils.ErrDatabase, err)
		}
	}

	// At end of stream an incomplete trailing sequence is no longer incomplete:
	// it is malformed input, and decodes to one replacement rune per byte
	// exactly as ranging over the equivalent string would.
	if !s.done {
		s.scan(carry, true)
	}
	return s.result(), nil
}

// commentBodyScanner accumulates the decision state ReadCommentBody carries
// across chunk boundaries. Every retained buffer is bounded by MaxCommentBody
// runes, so peak retention is independent of how much the writer sends.
type commentBodyScanner struct {
	content []byte // raw bytes of the content span, as supplied
	pending []byte // whitespace after the content, still undecided
	ctrlWS  []byte // raw bytes of the first forbidden rune seen outside the content
	// done reports that the retained value already determines the verdict (the
	// content is over the cap), so no further input can change it and reading
	// stops.
	done         bool
	contentRunes int
	pendingRunes int
}

// scan consumes as many whole runes as buf holds and returns how many bytes it
// consumed. When atEOF is false it stops before a trailing incomplete sequence
// so the next chunk can complete it; when true it decodes every remaining byte.
func (s *commentBodyScanner) scan(buf []byte, atEOF bool) int {
	consumed := 0
	for consumed < len(buf) && !s.done {
		if !atEOF && !utf8.FullRune(buf[consumed:]) {
			break
		}
		r, size := utf8.DecodeRune(buf[consumed:])
		s.rune(r, buf[consumed:consumed+size])
		consumed += size
	}
	return consumed
}

// rune folds one decoded rune, together with the raw bytes it was decoded from,
// into the scanner state. The RAW bytes are what gets retained: a body may
// contain bytes that are not valid UTF-8, and re-encoding the decoded
// replacement rune would silently rewrite what is stored.
func (s *commentBodyScanner) rune(r rune, raw []byte) {
	if unicode.IsSpace(r) {
		s.space(r, raw)
		return
	}

	// A non-space rune ends any pending whitespace run: that run is interior
	// whitespace, which counts towards the cap and is part of the stored text.
	if s.pendingRunes > 0 {
		s.content = append(s.content, s.pending...)
		s.contentRunes += s.pendingRunes
		s.pending = s.pending[:0]
		s.pendingRunes = 0
	}
	s.content = append(s.content, raw...)
	s.contentRunes++
	if s.contentRunes > MaxCommentBody {
		// The retained value is already over the cap, which is the verdict
		// ValidateCommentBody will reach; nothing further needs to be read.
		s.done = true
	}
}

// space folds a whitespace rune. Leading whitespace is dropped as it arrives;
// whitespace after the content is buffered only until the buffered total is
// enough to prove the cap is exceeded, which is what keeps an arbitrarily long
// interior run (a body of "a", a gigabyte of spaces, then "b") bounded without
// losing the over-cap verdict.
func (s *commentBodyScanner) space(r rune, raw []byte) {
	if utils.IsForbiddenControlChar(r) && s.ctrlWS == nil {
		// Recorded, not reported: this rune sits in a span TrimSpace removes, so
		// it is carried back at the end to let ValidateCommentBody see the body
		// as supplied.
		s.ctrlWS = append([]byte(nil), raw...)
	}
	if s.contentRunes == 0 {
		return // leading whitespace: trimmed away, never retained
	}
	if s.contentRunes+s.pendingRunes > MaxCommentBody {
		return // already enough buffered to settle the verdict either way
	}
	s.pending = append(s.pending, raw...)
	s.pendingRunes++
}

// result returns the value the body rules are applied to, per the contract
// documented on ReadCommentBody.
func (s *commentBodyScanner) result() string {
	if s.contentRunes == 0 {
		return ""
	}
	if s.ctrlWS != nil {
		return string(s.content) + string(s.ctrlWS)
	}
	return string(s.content)
}
