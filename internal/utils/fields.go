package utils

import (
	"fmt"
	"strconv"
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

// ControlCharError is the refusal a forbidden control character produces,
// spelled once so every caller reports the identical message and the identical
// sentinel (exit code 6): the whole-value check in ValidateNoControlChars, and
// the streaming comment-body reader in package models, which applies the same
// rule one rune at a time through IsForbiddenControlChar and must not diverge
// from it (internal/models/comment_read_test.go pins the agreement).
func ControlCharError(field Field) error {
	return fmt.Errorf("%w: %s: control characters are not allowed", ErrValidation, field)
}

// FieldTooLargeError is the refusal of a value longer than the field's maximum,
// carrying ErrFieldTooLarge (exit code 6). limit is the maximum the field
// accepts, in whatever unit the field's own SPEC entry counts — bytes for the
// task and sprint fields, characters for a comment body.
func FieldTooLargeError(field Field, limit int) error {
	return fmt.Errorf("%w: %s exceeds maximum length of %d characters", ErrFieldTooLarge, field, limit)
}

// FieldEmptyError is the refusal of an empty value supplied for a field that
// requires one, carrying ErrValidation (exit code 6). It is the wording the
// update commands use — `task edit`, `sprint update` — where the flag is
// optional but its value may not be empty.
//
// It names the FIELD, not the flag, because a value did reach the application
// and that value broke a rule about its content. The absence of a required flag
// is the other case and keeps the flag's own spelling: see
// SPEC/COMMANDS.md § Published Field Names in Validation Messages.
func FieldEmptyError(field Field) error {
	return fmt.Errorf("%w: %s cannot be empty", ErrValidation, field)
}

// RequiredFieldMessage is the text of the refusal of a missing value for a field
// that requires one, in the wording the model-level validators use.
//
// Unlike the three constructors above it returns the MESSAGE and not an error,
// because its callers are the package-level sentinels of internal/models, which
// are compared with errors.Is and must stay exactly what they were: plain
// errors.New values carrying no sentinel of their own. Chaining ErrValidation
// here would change the exit code they map to. What they take from this
// definition is the name, not the wrapping.
func RequiredFieldMessage(field Field) string {
	return field.String() + " is required"
}
