package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxInt32 is the maximum valid ID value (prevents integer overflow).
const MaxInt32 = math.MaxInt32 // 2,147,483,647

// ValidateFreeText applies both content rules a free-text field is subject to,
// in the one order SPEC/MODELS.md fixes for them: the Free-Text UTF-8 Encoding
// Constraint first, then the Free-Text Control-Character Constraint. It returns
// the first refusal, or nil when the value satisfies both. Every refusal maps to
// exit code 6.
//
// # Why the two rules are one call and not two
//
// The encoding rule is specified RELATIONALLY: it runs "immediately before the
// control-character check, on every command and for every field the two rules
// govern", and no other check moves. Welding the pair into a single call is what
// makes that hold by construction — the commands do not each repeat an ordering
// they could get wrong, and a later change to WHERE the pair sits (rmp task 302
// revisits that) carries the order with it instead of leaving one half behind.
//
// The order itself is not a preference. The control-character rule is defined
// over decoded CODE POINTS, and an invalid byte decodes to U+FFFD, which is not
// a forbidden code point — so before this pairing existed, invalid UTF-8 passed
// the control-character check and was written verbatim into a TEXT column. The
// encoding check is what makes the rule that follows it meaningful.
//
// The value is checked AS SUPPLIED, before any trimming, for the reason
// ValidateNoControlChars states below. That is sound for the encoding rule too,
// and for a stronger reason: strings.TrimSpace can only remove runes for which
// unicode.IsSpace is true, and no invalid byte decodes to one, so trimming can
// neither introduce nor remove an encoding failure.
func ValidateFreeText(value string, field Field) error {
	switch InspectFreeText(value) {
	case FreeTextInvalidUTF8:
		return InvalidUTF8Error(field)
	case FreeTextControlChars:
		return ControlCharError(field)
	default:
		return nil
	}
}

// InspectFreeText applies the same two content rules ValidateFreeText applies,
// in the same order, and reports WHICH one the value breaks instead of building
// a refusal from a Field. It is the single implementation of the ordered pair:
// ValidateFreeText is this call plus the choice of constructor, so the order can
// no longer be stated twice and drift.
//
// It exists because the graph write path applies the identical pair to Cypher
// property values (SPEC/GRAPH.md § Cypher Query and Property Value Content Rules)
// and has no
// Field to name them by — a property key is chosen by the caller and is not one
// of the eight published names. See FreeTextViolation for why that separation is
// the reuse and not a second realisation of the policy.
//
// The order is not a preference and is documented on ValidateFreeText: the
// control-character rule is defined over decoded CODE POINTS, and every byte
// that is not valid UTF-8 decodes to U+FFFD, which is not a forbidden code
// point. Running the encoding rule first is what makes the rule that follows it
// meaningful.
func InspectFreeText(value string) FreeTextViolation {
	switch {
	case !utf8.ValidString(value):
		return FreeTextInvalidUTF8
	case containsForbiddenControlChar(value):
		return FreeTextControlChars
	default:
		return FreeTextValid
	}
}

// TrimFreeText is THE ordering every free-text write path in the application
// runs. It applies the three rules a capped free-text value is subject to, in
// the one order that governs all of them, and returns the value to store:
//
//  1. the LENGTH cap, against the value as it will be STORED;
//  2. the Free-Text UTF-8 Encoding Constraint, against the value AS SUPPLIED;
//  3. the Free-Text Control-Character Constraint, against the value AS SUPPLIED;
//
// and only then the trim. It returns the first refusal, so a value that breaks
// more than one rule is always refused for the earliest one in that list.
//
// It is the whole of the constraint for the one free-text field that is optional,
// `completion_summary`, which Rule 2 governs and Rule 1 does not. Every required
// field goes through RequireFreeText below, which adds step 4 of SPEC/MODELS.md
// § Free-Text Emptiness and Trimming Constraint, the emptiness judgement.
//
// # Why the cap is in here, and why it is first
//
// The cap used to be the caller's business, and that is exactly why rmp task 302
// exists: `task create` and `sprint create` ran the content rules inline and left
// the cap to the model's Validate(), which runs afterwards, while every other
// path capped first. A value at once over-long and carrying a BEL was refused as
// a control character by `task create -t` and for its length by `task edit -t`,
// and `sprint create` disagreed with ITSELF — length on --title, control
// characters on --description — because the title had an inline cap and the
// description did not. Six paths stated the sequence for themselves and two of
// them stated it differently. The fix is not to correct the two: it is to leave
// the sequence stated in one place, here, so a call site has no order to get
// wrong.
//
// The cap is FIRST because the comment body's bounded standard-input reader
// (models.ReadCommentBody) settles the length verdict WITHOUT ever buffering the
// whole value — it stops reading as soon as the content cannot fit, which is the
// CWE-400 defence that reader exists for — and SPEC/COMMANDS.md requires the
// verdict a user sees to be the verdict a read-to-EOF implementation would
// reach. A reader cannot judge the encoding of bytes it has refused to read, so
// any order that put the encoding rule first would force that path to buffer
// whatever a hostile writer chose to send. Cap-first is therefore the only order
// the whole set of paths can share, and the other six were moved onto it.
//
// # An over-long value that is also invalid UTF-8 is refused for its LENGTH
//
// That is a consequence of the order above and it is deliberate. FieldLength is
// well defined on malformed input — every byte that decodes to no valid rune
// counts as one, so the count is never lower than SQLite's length() would
// return — and the cap is therefore answerable on a value whose encoding has not
// been established. It is also the verdict models.ReadCommentBody already had to
// produce: it carries invalid bytes through unrepaired precisely so that the
// length verdict, not the encoding verdict, is what such a body receives on both
// of its input paths alike.
//
// # Why the trim cannot come first
//
// VT (0x0B) and FF (0x0C) are forbidden by the control-character rule AND are
// whitespace to strings.TrimSpace, so trimming first hands the check a value the
// offending character has already been removed from: the input is accepted, the
// character is discarded in silence, and the CWE-150 guard fails at the position
// where such a character is easiest to hide. The single observable signature of
// the correct order — a value made only of VT refused as a CONTROL CHARACTER and
// never as empty — holds everywhere at once because the sequence is written here
// and nowhere else.
//
// # What the cap measures
//
// The STORED form, which is the trimmed value (SPEC/MODELS.md § Free-Text
// Emptiness and Trimming Constraint, Rule 2), so the application can never
// accept a value that CHECK(length(<column>) <= N) would then reject. Trimming
// before measuring is safe on a value whose encoding is still unestablished:
// strings.TrimSpace removes only runes for which unicode.IsSpace is true, and no
// invalid byte decodes to one, so the trim can neither introduce nor remove an
// encoding failure. Every call site used to compute that trim for itself in
// order to pass it to CheckFieldLength; folding it in removes that repetition
// too, because the trim and the value the cap measures are the same thing.
//
// limit is the field's maximum in code points. The maximums are declared in
// package models (SPEC/MODELS.md), which cannot be imported here — models
// imports this package — so the caller supplies it.
func TrimFreeText(value string, field Field, limit int) (string, error) {
	stored := strings.TrimSpace(value)
	if err := CheckFieldLength(stored, field, limit); err != nil {
		return "", err
	}
	if err := ValidateFreeText(value, field); err != nil {
		return "", err
	}
	return stored, nil
}

// RequireFreeText is TrimFreeText plus step 4: the emptiness judgement, applied
// to the TRIMMED value, for one of the seven free-text fields that are required
// to be non-empty (SPEC/MODELS.md § Free-Text Emptiness and Trimming Constraint,
// Rule 1). It returns the value to store, or the first refusal.
//
// A value made only of whitespace leaves nothing behind and counts as absent, so
// it is refused with FieldEmptyError — naming the FIELD, because a value did
// reach the application and broke a rule about its content. The absence of a
// required flag is the other case entirely and keeps the flag's own spelling; a
// caller that has such a flag decides that question BEFORE calling this, against
// the value as supplied (SPEC/COMMANDS.md § Emptiness Constraint (All Required
// Free-Text Fields)).
//
// The emptiness judgement sits AFTER the cap for the same reason it sits after
// the content rules — it is step 3 and they are step 1 — and putting the cap
// ahead of it changes no verdict in either direction: a value that trims to
// nothing is zero code points long, so no input exists for which the two could
// both answer.
func RequireFreeText(value string, field Field, limit int) (string, error) {
	trimmed, err := TrimFreeText(value, field, limit)
	if err != nil {
		return "", err
	}
	if trimmed == "" {
		return "", FieldEmptyError(field)
	}
	return trimmed, nil
}

// ValidateUTF8 rejects a value whose bytes are not a well-formed UTF-8 sequence,
// returning an ErrValidation-wrapped error (exit 6) and nil otherwise. It
// enforces SPEC/MODELS.md § Free-Text UTF-8 Encoding Constraint, which defines
// well-formedness as the Unicode Standard, Table 3-7 does — the same definition
// utf8.ValidString implements, so the five malformed shapes the SPEC enumerates
// (an unintroduced continuation byte, a byte that never occurs in UTF-8 at all,
// an overlong encoding, a surrogate code point, and a sequence the input ends
// before completing) are refused by that one call and need no enumeration here.
//
// Callers reach it through ValidateFreeText, which is what pins the order it
// runs in. It is exported in its own right because it is the rule itself, and
// because a caller that has already applied the control-character rule by
// another route — the streaming comment-body reader does — needs the encoding
// half alone.
//
// It calls utf8.ValidString directly rather than routing through
// InspectFreeText, even though InspectFreeText answers FreeTextInvalidUTF8 on
// exactly the same inputs. Routing it there would make every caller that wants
// the encoding half ALONE also scan the value for control characters it is
// deliberately not applying — an extra pass over the whole value on the
// streaming comment path, bought to avoid repeating one standard-library call.
// What must not be repeated is the POLICY, and the policy here is the choice of
// definition (Unicode Table 3-7, which utf8.ValidString implements) and the
// wording of the refusal; both are stated once, above and in freeTextReasons.
func ValidateUTF8(value string, field Field) error {
	if !utf8.ValidString(value) {
		return InvalidUTF8Error(field)
	}
	return nil
}

// ValidateNoControlChars rejects free-text input that contains control or
// Unicode bidirectional/format code points, returning an ErrValidation-wrapped
// error (exit 6) otherwise nil. It enforces SPEC/MODELS.md § Free-Text
// Control-Character Constraint, blocking terminal escape-sequence injection
// (CWE-150) and Trojan Source attacks (CVE-2021-42574).
//
// Rejected code points:
//   - ASCII control bytes below 0x20, EXCEPT TAB (0x09), LF (0x0A) and CR (0x0D)
//   - DEL (0x7F)
//   - Unicode bidi/format controls: U+200E, U+200F, U+202A-U+202E,
//     U+2066-U+2069, and U+FEFF
//
// Legitimate Unicode (accents, emoji, CJK, etc.) is accepted unchanged.
//
// The field is named by a Field, not by a string, so the refusal can only carry
// the published name the SPEC assigns it: see the note on Field in fields.go for
// why that parameter is not a string.
func ValidateNoControlChars(value string, field Field) error {
	if containsForbiddenControlChar(value) {
		return ControlCharError(field)
	}
	return nil
}

// containsForbiddenControlChar reports whether value carries any code point
// IsForbiddenControlChar rejects. It is the whole-value form of the rule,
// written once so ValidateNoControlChars and InspectFreeText scan for the same
// set by construction rather than by two loops agreeing.
func containsForbiddenControlChar(value string) bool {
	for _, r := range value {
		if IsForbiddenControlChar(r) {
			return true
		}
	}
	return false
}

// IsForbiddenControlChar reports whether r is one of the code points
// ValidateNoControlChars rejects. It is the rule itself, exposed one rune at a
// time so a caller that consumes free text as a STREAM — the bounded comment
// body reader in package models — applies the identical rule without first
// materialising the whole value in memory.
func IsForbiddenControlChar(r rune) bool {
	switch {
	case r == '\t' || r == '\n' || r == '\r':
		// Explicitly permitted whitespace controls.
		return false
	case r < 0x20 || r == 0x7F:
		return true
	default:
		return isBidiOrFormatControl(r)
	}
}

// isBidiOrFormatControl reports whether r is one of the Unicode
// bidirectional/format code points forbidden in free-text fields.
func isBidiOrFormatControl(r rune) bool {
	switch r {
	case 0x200E, // LEFT-TO-RIGHT MARK
		0x200F,                                 // RIGHT-TO-LEFT MARK
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E, // embedding/override controls
		0x2066, 0x2067, 0x2068, 0x2069, // isolate controls
		0xFEFF: // zero-width no-break space / BOM
		return true
	}
	return false
}

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
		return fmt.Errorf("%w: invalid %s ID: %d (must be positive)", ErrValidation, entity, id)
	}
	if id > MaxInt32 {
		return fmt.Errorf("%w: invalid %s ID: %d (exceeds maximum value %d)", ErrValidation, entity, id, MaxInt32)
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

// ValidateNumericRange checks that val is within the inclusive [min, max]
// range and returns a wrapped ErrValidation error otherwise. Used for
// CLI inputs like priority and severity that share an identical bounds
// check and error format. The value is well-formed (it parsed as an
// integer); it is the value that is out of the allowed range, so this is
// a data-validation failure (exit 6), not a syntax error (exit 2).
func ValidateNumericRange(val, min, max int, field string) error {
	if val < min || val > max {
		return fmt.Errorf("%w: invalid %s: must be %d-%d (got %d)", ErrValidation, field, min, max, val)
	}
	return nil
}

// ParseCommaSeparatedIDs parses a comma-separated list of IDs and validates
// each one through ValidateIDString. Returns the parsed slice or the first
// validation error encountered.
//
// Example:
//
//	ids, err := ParseCommaSeparatedIDs("1,2, 3", "task")
//	// ids == []int{1, 2, 3}
func ParseCommaSeparatedIDs(s string, entity string) ([]int, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("%w: %s ID(s) required", ErrRequired, entity)
	}
	parts := strings.Split(s, ",")
	ids := make([]int, 0, len(parts))
	for _, p := range parts {
		id, err := ValidateIDString(strings.TrimSpace(p), entity)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
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

	// Parse the integer. The digit-only check above guarantees Atoi cannot
	// fail on syntax, so any error here is an overflow: an all-digits value
	// larger than the platform int range. That is a magnitude/range failure,
	// NOT bad syntax, so it is classified as ErrValidation (exit 6) — the same
	// class as ValidateID's "exceeds maximum value" case, keeping both
	// out-of-range paths consistent (finding #58).
	id, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid %s ID: %q (exceeds maximum value %d)", ErrValidation, entity, s, MaxInt32)
	}
	if err := ValidateID(id, entity); err != nil {
		return 0, err
	}
	return id, nil
}
