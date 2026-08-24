package utils

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

// Field identifies one of Groadmap's eight free-text fields.
//
// It is the single shared definition that
// SPEC/COMMANDS.md § Published Field Names in Validation Messages requires:
// every validation message that names a field takes the name from here, and no
// command spells a field name inline. The defect it exists to prevent is not a
// typo but the absence of the definition — while each call site chose its own
// literal, `task create` refused a control character in
// `functional-requirements` and `task edit` refused the identical value in
// `functional_requirements`, one field with two names, and `task create` also
// contradicted itself by reporting the same field as `functional_requirements`
// when it was too long.
//
// # Why the underlying type is an integer and not a string
//
// The published name is reachable only through a Field value, and Field's
// underlying type is an integer, so an untyped string constant does not convert
// to it. A call site that goes back to spelling the name itself,
//
//	utils.ValidateNoControlChars(value, "functional-requirements")
//
// does not compile. Had Field been declared `type Field string`, that same call
// would have compiled in silence — an untyped string constant is assignable to
// any string-based type — and the second spelling would be back with nothing to
// report it. The integer base is what turns
// SPEC/COMMANDS.md acceptance criterion 6 from a test into a build failure.
//
// The one hole a type cannot close is a call site that hand-builds a governed
// message instead of calling the constructors below. That is what the gate in
// published_field_names_test.go watches.
//
// # Adding a field
//
// Add the row to the SPEC table first, then the constant here and its entry in
// publishedNames. TestPublishedNamesMatchTheSpecTable compares the two and fails
// on either half done alone.
type Field uint8

// The eight free-text fields, one constant per row of the table in
// SPEC/COMMANDS.md § Published Field Names in Validation Messages.
//
// Task and Sprint each have a `title`, and the two publish the same name. They
// are still separate constants because the SPEC table lists them as separate
// fields and because a rule added later may need to tell one from the other —
// the identity of their published names is asserted, not assumed, by
// TestTaskAndSprintTitlePublishTheSameName.
//
// The zero value is deliberately not a field: a Field that was never assigned
// must not pass for one.
const (
	FieldTaskTitle Field = iota + 1
	FieldTaskFunctionalRequirements
	FieldTaskTechnicalRequirements
	FieldTaskAcceptanceCriteria
	FieldTaskCompletionSummary
	FieldSprintTitle
	FieldSprintDescription
	FieldCommentBody
)

// publishedNames maps each Field to the name every validation message uses for
// it. The name is the lowercase, underscored one, which is also the database
// column that stores the field (SPEC/DATABASE.md). It is neither the flag name,
// which is kebab-case and carries a leading `--`, nor the Go struct field name
// declared in SPEC/MODELS.md.
//
// Index 0 is empty on purpose: it is the zero value of Field, which names no
// field.
var publishedNames = [...]string{
	FieldTaskTitle:                  "title",
	FieldTaskFunctionalRequirements: "functional_requirements",
	FieldTaskTechnicalRequirements:  "technical_requirements",
	FieldTaskAcceptanceCriteria:     "acceptance_criteria",
	FieldTaskCompletionSummary:      "completion_summary",
	FieldSprintTitle:                "title",
	FieldSprintDescription:          "description",
	FieldCommentBody:                "body",
}

// String returns the field's published name, so a Field can be written straight
// into a message with %s and no call site ever holds the name as a string.
//
// A value that names no field — the zero Field, or an integer converted to Field
// out of range — renders as "Field(N)" rather than as an empty or invented name,
// so a message built from one is visibly wrong instead of quietly plausible.
// Production code cannot produce such a value by accident: every constant above
// is in range, and TestNoProductionCodeConvertsAnIntegerToField refuses the
// conversion that would be needed to invent one.
func (f Field) String() string {
	if int(f) < len(publishedNames) {
		if name := publishedNames[f]; name != "" {
			return name
		}
	}
	return "Field(" + strconv.Itoa(int(f)) + ")"
}

// FreeTextViolation names which of the two free-text CONTENT rules a value
// breaks — the Free-Text UTF-8 Encoding Constraint or the Free-Text
// Control-Character Constraint — for a caller whose value is not one of the
// eight fields above and therefore has no Field to name it by.
//
// # Why this type exists
//
// The eight fields are addressed by FLAGS and their names are a closed table, so
// Field can be a closed integer enum and the refusal can be built from it. A
// Cypher property value written by `rmp graph create` / `graph update` is
// subject to the very same two rules (SPEC/GRAPH.md § Property Value Content
// Rules), but the name that identifies it — the property key — is chosen by the
// caller at the keyboard. It is unbounded, so it cannot be a Field, and the type
// note above says exactly why it must not become one: an untyped string that
// converts to Field is the hole the integer base was chosen to close.
//
// Separating WHICH RULE BROKE from WHICH NAME TO PUBLISH is what lets both
// callers share one implementation of the rules and one wording of each refusal,
// while each publishes the name its own surface has. Without it the graph would
// need its own copy of the pair — one policy realised twice, which is the defect
// rmp task 294 removed elsewhere in this package.
//
// The zero value is the absence of a violation, so a FreeTextViolation that was
// never assigned reads as "the value is fine" rather than as some rule.
type FreeTextViolation uint8

// The verdicts InspectFreeText returns, in the order the rules are applied.
const (
	// FreeTextValid is a value both content rules accept.
	FreeTextValid FreeTextViolation = iota
	// FreeTextInvalidUTF8 is a value whose bytes are not a well-formed UTF-8
	// sequence (SPEC/MODELS.md § Free-Text UTF-8 Encoding Constraint).
	FreeTextInvalidUTF8
	// FreeTextControlChars is a value carrying a forbidden control or Unicode
	// bidirectional/format code point (SPEC/MODELS.md § Free-Text
	// Control-Character Constraint).
	FreeTextControlChars
)

// freeTextReasons is the wording of each refusal, and the ONLY place either
// wording is spelled. InvalidUTF8Error and ControlCharError below take their
// text from here, so a caller that has a Field and a caller that has only a
// property key publish the same sentence by construction rather than by two
// authors agreeing.
//
// Index 0 is empty on purpose: FreeTextValid names no rule and has no refusal.
var freeTextReasons = [...]string{
	FreeTextInvalidUTF8:  "the value is not valid UTF-8",
	FreeTextControlChars: "control characters are not allowed",
}

// Reason returns the governed wording of the rule this violation breaks, with no
// field name and no sentinel attached, for a caller that must name the offending
// value some other way. It returns "" for FreeTextValid, which breaks no rule.
//
// A caller that HAS a Field must not use this: InvalidUTF8Error and
// ControlCharError build the whole message, including the published name, and
// are what SPEC/COMMANDS.md § Published Field Names in Validation Messages
// requires those call sites to use.
func (v FreeTextViolation) Reason() string {
	if int(v) < len(freeTextReasons) {
		return freeTextReasons[v]
	}
	return ""
}

// InvalidUTF8Error is the refusal of a value whose bytes are not a well-formed
// UTF-8 sequence (SPEC/MODELS.md § Free-Text UTF-8 Encoding Constraint). It
// carries ErrValidation, so it is the same failure CLASS and the same exit code,
// 6, as ControlCharError: the two constraints govern the same eight fields, and
// either one alone is grounds to refuse the input.
//
// It is the fourth governed message class, and the one this definition was
// written to be extended by. The constraint that defines it states the wording
// and then defers the field's name to SPEC/COMMANDS.md § Published Field Names
// in Validation Messages rather than restating the mapping — which is precisely
// what taking a Field here, and nothing else, makes true of the code as well.
//
// Its wording is listed in governedFragments, so the gate in
// published_field_names_test.go watches this class as it watches the other
// three: rewording the message here without updating that list fails the gate,
// and hand-building this message in any other file fails it too.
func InvalidUTF8Error(field Field) error {
	return fmt.Errorf("%w: %s: %s", ErrValidation, field, FreeTextInvalidUTF8.Reason())
}

// ControlCharError is the refusal a forbidden control character produces,
// spelled once so every caller reports the identical message and the identical
// sentinel (exit code 6): the whole-value check in ValidateNoControlChars, and
// the streaming comment-body reader in package models, which applies the same
// rule one rune at a time through IsForbiddenControlChar and must not diverge
// from it (internal/models/comment_read_test.go pins the agreement).
func ControlCharError(field Field) error {
	return fmt.Errorf("%w: %s: %s", ErrValidation, field, FreeTextControlChars.Reason())
}

// FieldTooLargeError is the refusal of a value longer than the field's maximum,
// carrying ErrFieldTooLarge (exit code 6). limit is the maximum the field
// accepts, counted in the one unit every one of the eight fields is measured in:
// Unicode code points. See FieldLength, which is what measures it, and
// CheckFieldLength, which is what every cap in the application calls.
//
// The message says "characters" and it is answerable for the word. It used to
// name characters while the task and sprint caps counted BYTES, so a title of
// 102 CJK characters — 306 bytes — was refused for exceeding "255 characters".
// The wording did not change when that was fixed; what changed is that it became
// true (rmp task 296).
func FieldTooLargeError(field Field, limit int) error {
	return fmt.Errorf("%w: %s exceeds maximum length of %d characters", ErrFieldTooLarge, field, limit)
}

// FieldLength returns the length of a free-text value in the unit Groadmap
// measures every one of its eight free-text fields in: Unicode code points.
//
// # Why code points, and why one function
//
// SPEC/MODELS.md states each cap as a number of CHARACTERS, and the database
// backs each one with CHECK(length(<column>) <= <n>), where SQLite's length()
// counts characters on a TEXT value. Code points are the reading that makes the
// application, the specification and the schema say the same thing, and a value
// the application accepts can therefore never trip the CHECK.
//
// It is one exported function rather than a call to utf8.RuneCountInString at
// each cap because the defect it closes was not a wrong count in one place — it
// was TWO UNITS in one codebase. The comment body counted code points while the
// seven task and sprint caps counted bytes with len(), so a body of 4096 CJK
// characters was accepted at its documented maximum while a title of 102 CJK
// characters was refused for exceeding one of 255. A single definition is what
// makes that divergence impossible to reintroduce a field at a time;
// internal/utils/field_length_unit_test.go is the gate that keeps every cap
// routed through it.
//
// # Code points, deliberately not graphemes
//
// A code point count is NOT a grapheme count and is not meant to be. "é" written
// as U+0065 U+0301 (e + COMBINING ACUTE ACCENT) counts 2 here and 2 in SQLite,
// while the precomposed U+00E9 counts 1 in both. Groadmap does not normalise
// free text — it stores exactly the bytes the caller supplied (SPEC/MODELS.md
// § Free-Text UTF-8 Encoding Constraint) — so counting graphemes would make the
// application disagree with the CHECK constraint that guards the same column,
// which is the very disagreement this unit exists to remove.
//
// The value is expected to be valid UTF-8, which the encoding rule guarantees
// for anything that reaches storage (SPEC/MODELS.md § Free-Text UTF-8 Encoding
// Constraint). The count is still well defined without that guarantee: each byte
// that decodes to no valid rune counts as one, which is never fewer than the
// non-continuation bytes SQLite's length() would count, so the cap stays at
// least as strict as the schema on any input at all.
func FieldLength(value string) int {
	return utf8.RuneCountInString(value)
}

// CheckFieldLength returns the FieldTooLargeError refusal when value is longer
// than limit code points, and nil otherwise. It is the whole of a length cap:
// every cap on a task, sprint or comment free-text field is this call and
// nothing else, so no cap can measure in a unit of its own.
//
// value must be the string that is STORED, not the string as supplied. Where a
// field is trimmed before storage the trimmed value is what the column holds and
// therefore what the cap must measure (SPEC/MODELS.md § Free-Text Emptiness and
// Trimming Constraint, Rule 2); every caller passes it accordingly.
func CheckFieldLength(value string, field Field, limit int) error {
	if FieldLength(value) > limit {
		return FieldTooLargeError(field, limit)
	}
	return nil
}

// FieldEmptyError is the refusal of a value that names nothing, supplied for a
// field that requires one, carrying ErrValidation (exit code 6).
//
// It names the FIELD, not the flag, because a value did reach the application
// and that value broke a rule about its content. The absence of a required flag
// is the other case and keeps the flag's own spelling: see
// SPEC/COMMANDS.md § Published Field Names in Validation Messages.
//
// All four commands that write a required task or sprint free-text field emit
// it, and they emit it for the identical condition — the value is empty once
// trimmed (SPEC/COMMANDS.md § Emptiness Constraint (All Required Free-Text
// Fields)). Where they still differ is only the LITERAL empty string: on
// `task edit` and `sprint update` the flag is optional, so an empty value is a
// rejected value and reaches this refusal; on `task create` and `sprint create`
// the flag is a required parameter, so an empty value means the parameter never
// arrived and the refusal names the flag with exit code 2 instead. A value made
// of whitespace falls on this side of that boundary on all four, because the
// caller did supply text and the text turns out to name nothing.
func FieldEmptyError(field Field) error {
	return fmt.Errorf("%w: %s cannot be empty", ErrValidation, field)
}

// RequiredFieldMessage is the text of the refusal of a missing value for a field
// that requires one, in the wording the model-level validators use.
//
// Unlike the four constructors above it returns the MESSAGE and not an error,
// because its callers are the package-level sentinels of internal/models, which
// are compared with errors.Is and must stay exactly what they were: plain
// errors.New values carrying no sentinel of their own. Chaining ErrValidation
// here would change the exit code they map to. What they take from this
// definition is the name, not the wrapping.
func RequiredFieldMessage(field Field) string {
	return field.String() + " is required"
}
