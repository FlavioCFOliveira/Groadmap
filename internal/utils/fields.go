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

// ---------------------------------------------------------------------------
// The numeric range rule.
// ---------------------------------------------------------------------------

// RangedField identifies one of the numeric values Groadmap refuses when it
// falls outside a fixed inclusive range: the task fields `priority` and
// `severity`, the `--limit` of a list command, the id of each entity a command
// addresses, and a sprint's `--max-tasks` capacity cap.
//
// # Why a second type instead of more Field constants
//
// Field is the closed set of Groadmap's EIGHT FREE-TEXT fields, and
// SPEC/COMMANDS.md § Published Field Names in Validation Messages is canonical
// for it: that table has eight rows, and TestPublishedNamesMatchTheSpecTable
// compares the constants against it row by row. None of the values here is free
// text, none is in that table, and the rule that refuses them is not one of the
// four that section governs. A ninth Field constant would therefore put the type
// in contradiction with the specification that defines it.
//
// What they DO share with Field is the reason the underlying type is an
// integer. The name a range refusal publishes is reachable only through a
// RangedField value, so a call site that goes back to spelling the name itself,
//
//	utils.NumericRangeMessage("priority", 0, 9)
//
// does not compile. Had this been declared `type RangedField string` that call
// would have compiled in silence, and a second spelling of one field's name
// would be back with nothing to report it.
//
// The zero value is deliberately not a field: a RangedField that was never
// assigned must not pass for one.
type RangedField uint8

// The values a range rule governs, one constant per name a refusal publishes.
//
// FieldListLimit serves all three commands that publish a `--limit`, because the
// three publish one NAME. Their maxima genuinely differ — `task list` and
// `backlog list` cap at 100, `audit list` at 500 — but a bound is not part of a
// name: NumericRangeMessage takes it as a parameter, so one constant words all
// three sentences without pretending the caps are equal (rmp task 329).
//
// The five id fields are the subjects of the ID RANGE rule, which is the same
// rule instantiated at the same two bounds for every one of them: an id must lie
// in MinID..MaxInt32. They are five constants and not one because they name five
// different fields — a comment id is not a task id — and because each already
// has a name the specification publishes for it. What they share is the FORM of
// the refusal, not its subject (rmp task 330).
//
// The one place two surfaces share a SUBJECT is FieldEntityID. The `--entity-id`
// flag of `audit list` and the second positional of `audit history` address the
// identical field: SPEC/COMMANDS.md § Entity History defines the second as
// "Equivalent to `rmp audit list -r <name> -e <entity-type> --entity-id
// <entity-id>`", and the value is the `entity_id` column of the audit table in
// both cases (SPEC/DATABASE.md § audit Table). One field, one constant, therefore
// one sentence — which is the half of rmp task 330 that converges a NAME rather
// than a form.
//
// FieldSprintMaxTasks is the last of the ranged values to arrive, and it is one
// constant for TWO call sites rather than two: `sprint create` and `sprint
// update` bound the identical field of the identical entity at the identical
// bounds. They already agreed with each other — which is why this was never the
// defect tasks 318 and 329 fixed — and disagreed with everything else, keeping
// the prefixed, parenthesised `--max-tasks must be between 1 and 10000 (got 0)`
// after every other range rule had retired that form. A form that survives only
// where no task has reached is not a second contract, it is the absence of one
// (rmp task 338).
//
// The zero value is deliberately not a field, as above.
const (
	FieldTaskPriority RangedField = iota + 1
	FieldTaskSeverity
	FieldListLimit
	FieldTaskID
	FieldSprintID
	FieldEntityID
	FieldCommentID
	FieldDependencyTaskID
	FieldSprintMaxTasks
)

// rangedNames maps each RangedField to the name its refusal publishes. Each is
// the lowercase word the flag or positional uses, with its leading `--` dropped
// and its hyphens written as underscores, so a refusal names the VALUE that
// broke the rule and never the flag that carried it — the same boundary
// SPEC/COMMANDS.md § Published Field Names in Validation Messages draws for the
// free-text fields, and the axis on which `audit list` used to disagree with
// `task list` and `backlog list`.
//
// For `priority` and `severity` the word is also the database column that stores
// the value (SPEC/DATABASE.md). `limit` stores nothing — it bounds a result set
// — so the flag's own word is the whole of its name. `max_tasks` is a column
// too: it is the `max_tasks` column of the sprints table, and the key the sprint
// JSON already publishes it under, so dropping the `--` and writing the hyphen
// as an underscore lands on the name the rest of the application uses for the
// value rather than on a name invented for the refusal.
//
// The five id names are not invented here either. Each is the argument's own
// name in SPEC/COMMANDS.md — `task-id`, `sprint-id`, `entity-id`, `comment-id`,
// and the dependency positional — written the way every other published field
// name in this package is written. `entity_id` lands on the audit column of the
// same name, and on the name internal/models already published for that field
// before this rule reached it.
//
// Index 0 is empty on purpose: it is the zero value of RangedField, which names
// no field.
var rangedNames = [...]string{
	FieldTaskPriority:     "priority",
	FieldTaskSeverity:     "severity",
	FieldListLimit:        "limit",
	FieldTaskID:           "task_id",
	FieldSprintID:         "sprint_id",
	FieldEntityID:         "entity_id",
	FieldCommentID:        "comment_id",
	FieldDependencyTaskID: "dependency_task_id",
	FieldSprintMaxTasks:   "max_tasks",
}

// idEntityWords maps each id field to the word its FORMAT refusal publishes —
// the `<entity>` of "invalid <entity> ID: \"X\" (must be a positive integer)".
//
// # Why the two names live in one table each and not in one string each
//
// An id is subject to TWO rules, and the two are deliberately not merged. The
// FORMAT rule refuses a token that is not an integer at all and is exit code 2
// misuse; the RANGE rule refuses an integer that fell outside MinID..MaxInt32
// and is a validation failure. rmp task 318 left the format refusal alone for
// exactly that reason, and rmp task 330 converged the range refusal without
// disturbing it.
//
// Each rule therefore publishes its own name for the same argument, and the two
// must be recognisably the same argument: `entity_id` for the range,
// `entity` for the format. They are stored as two tables over ONE constant so a
// call site can no more choose the format word than it can choose the range
// name, and TestIDEntityWordAgreesWithThePublishedName pins the relationship —
// the range name is the entity word with its spaces written as underscores and
// `_id` appended — so neither can move without the other.
//
// A RangedField that is not an id has no entry: IDEntity returns "" for it, the
// way FreeTextViolation.Reason returns "" for the violation that is not one.
var idEntityWords = [...]string{
	FieldTaskID:           "task",
	FieldSprintID:         "sprint",
	FieldEntityID:         "entity",
	FieldCommentID:        "comment",
	FieldDependencyTaskID: "dependency task",
}

// IDEntity returns the word this field's id FORMAT refusal names it by, and ""
// for a RangedField that is not an id.
//
// It is also the word the surrounding prose of an id message uses — "task 42 not
// found", "comment ID required" — so a command that holds the field holds every
// spelling it needs and never carries a label of its own beside it.
func (f RangedField) IDEntity() string {
	if int(f) < len(idEntityWords) {
		return idEntityWords[f]
	}
	return ""
}

// String returns the field's published name, so a RangedField can be written
// straight into a message with %s and no call site ever holds the name as a
// string.
//
// A value that names no field renders as "RangedField(N)" rather than as an
// empty or invented name, so a message built from one is visibly wrong instead
// of quietly plausible.
func (f RangedField) String() string {
	if int(f) < len(rangedNames) {
		if name := rangedNames[f]; name != "" {
			return name
		}
	}
	return "RangedField(" + strconv.Itoa(int(f)) + ")"
}

// NumericRangeMessage is the wording of the rule "this field's value must fall
// inside these bounds", and the ONLY place that wording is spelled.
//
// # Why it exists
//
// The rule that `priority` and `severity` must lie in 0-9 used to be realised
// twice: once inline in package models, which announced it as
// `priority must be between 0 and 9, got 99`, and once in a generic helper in
// this package, which announced the identical verdict on the identical value as
// `invalid priority: must be 0-9 (got 99)`. Both were true, both carried
// ErrValidation and exit code 6, and which one a caller read depended on the
// command that happened to apply the rule — `task create` printed the first,
// `task prio`, `task sev` and `task edit` the second. The wording of a refusal
// is a property of the RULE, not of the code path that reached it, so the
// specification had begun to publish both, which is how such a split becomes
// permanent (rmp task 318).
//
// The surviving wording is the one above, because it belongs with the sentinels
// that own the rule (models.ErrPriorityOutOfRange, models.ErrSeverityOutOfRange)
// and because it names the field the way every other validation message in
// Groadmap names one: the published name first, then what is wrong with it.
//
// # The same split, a second time
//
// `--limit` had repeated it across three commands: `task list` and
// `backlog list` refused an out-of-range value as
// `limit must be between 1 and 100`, while `audit list` refused it as
// `--limit must be between 1 and 500 (got 0)`. Two of those differences were
// accidental — the `--` prefix, and whether the offending value was echoed —
// and one, the maximum, is real, because the two caps are genuinely different
// numbers. The three now take the sentence from here and differ in the bound
// alone (rmp task 329).
//
// # Why the message and not the check
//
// This returns the SENTENCE and not an error, and it performs no comparison.
// The bounds check belongs to the package that owns the value, which is
// internal/models: ValidatePriority, ValidateSeverity, ValidateTaskLimit and
// ValidateAuditLimit are the callers, and this package must not import models.
// What is shared here is the one thing that must not be written twice: the
// words. Compare RequiredFieldMessage above, which exists for exactly the same
// reason.
//
// Its wording is listed in the gate in published_field_names_test.go, so a
// second spelling introduced in any other production file is a test failure.
func NumericRangeMessage(field RangedField, min, max int) string {
	return fmt.Sprintf("%s must be between %d and %d", field, min, max)
}

// NumericRangeError completes the refusal NumericRangeMessage words: it chains
// ErrValidation, so the failure maps to exit code 6 (SPEC/ARCHITECTURE.md), then
// the rule that was broken, then the value that broke it.
//
// rule is the package-level sentinel that OWNS the rule and whose text is a
// NumericRangeMessage. It is chained with %w rather than rendered with %s so
// errors.Is can still tell which field was refused; both verbs render the same
// bytes, and %s would silently discard the chain.
//
// Taking the sentinel as a parameter is what lets the assembly — the ", got N"
// suffix included — be written once while each caller keeps a sentinel of its
// own. Building the whole error here from a RangedField instead would have
// forced every caller to share one sentinel, or forced the suffix to be spelled
// a second time at the call site that needed its own; both are the defect this
// function removes, in a smaller form.
func NumericRangeError(rule error, got int) error {
	return fmt.Errorf("%w: %w%s", ErrValidation, rule, offendingValue(strconv.Itoa(got)))
}

// offendingValue is the ONLY spelling of how a range refusal names the value
// that broke the rule. Both assemblies below end with it, so the punctuation and
// the wording of the echo cannot drift between them — which is one of the two
// axes on which `audit list` used to disagree with `task list` before rmp task
// 329, and one of the axes on which the id refusals disagreed with everything
// before rmp task 330.
//
// It takes the value as TEXT rather than as an int because one caller has no int
// to give: an all-digits token too large for the platform's int never reaches a
// comparison, and echoing the token is the only way to name the value that was
// actually refused. Every caller that does hold an int renders it with
// strconv.Itoa, so the two paths produce the same bytes for the same value.
func offendingValue(got string) string {
	return ", got " + got
}

// IDRangeMessage is the wording of the ID RANGE rule for one id field: an id
// must lie in MinID..MaxInt32, whichever entity it identifies and whichever
// surface carried it.
//
// It exists so that the bounds of that rule are named in one place. Before rmp
// task 330 they were written out at four sites, in three different sentences,
// two of which stated only the bound that happened to be crossed — so an id of 0
// was refused for not being "positive" without the reader ever learning what the
// rule was. The rule has two bounds and the sentence now states both.
//
// The wording itself is not spelled here: it is NumericRangeMessage, so an id
// range and a `--limit` range and a `priority` range are one sentence with
// different subjects and different numbers, which is the whole point of that
// function.
func IDRangeMessage(field RangedField) string {
	return NumericRangeMessage(field, MinID, MaxInt32)
}

// IDRangeError is the refusal of an id outside MinID..MaxInt32, in the form
// NumericRangeError produces for every other range rule and with the offending
// value echoed by the same suffix.
//
// # Why the failure class is a parameter
//
// Every other range refusal in Groadmap is an ErrValidation and exits 6. An id
// range refusal is one too on the audit surfaces, and is deliberately NOT one on
// the four comment subcommands, which publish the whole "positive integer"
// constraint on their positional id as exit code 2 misuse
// (SPEC/COMMANDS.md § Add Task Comment, § Edit Task Comment and their sprint
// counterparts, validation order step 2). That difference is a published
// contract and changing it would change an exit code, so rmp task 330 converged
// the words and left it standing.
//
// Passing the class in is what makes that possible without a second wording: the
// rule owns the sentence, the SURFACE owns the class. A caller that hand-built
// its own message to get its own exit code is exactly what this replaces.
//
// got is the value that broke the rule, as text; see offendingValue.
func IDRangeError(class error, field RangedField, got string) error {
	return fmt.Errorf("%w: %s%s", class, IDRangeMessage(field), offendingValue(got))
}
