package utils

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// declaredFields is every Field constant, in declaration order. The gates below
// read it rather than the unexported publishedNames array, so a constant that
// was added without an entry — or an entry added without a constant — is a
// failure and not an invisible gap.
var declaredFields = []Field{
	FieldTaskTitle,
	FieldTaskFunctionalRequirements,
	FieldTaskTechnicalRequirements,
	FieldTaskAcceptanceCriteria,
	FieldTaskCompletionSummary,
	FieldSprintTitle,
	FieldSprintDescription,
	FieldCommentBody,
}

// declaredRangedFields is every RangedField constant, in declaration order, and
// it plays the same part for the numeric range rule that declaredFields plays
// for the free-text rules: the gate in published_field_names_test.go builds the
// names it recognises from this table, so a field brought under the rule is
// watched by adding it here and nothing else.
//
// FieldListLimit is the proof of that claim. When rmp task 318 factored the rule
// out it deliberately watched `priority` and `severity` alone, and recorded that
// bringing `--limit` under the rule later would be one line here. It was
// (rmp task 329): the constant below is the whole of the gate's extension, and
// the three commands that publish a `--limit` are now held to one sentence by
// the same two tests that hold priority and severity to theirs.
//
// The five id fields are the proof a second time over. rmp task 330 brought the
// ID RANGE rule under the same gate, and again the whole of the extension is the
// list below: the five
// constants make `task_id must be between …` — and, because the range class
// treats a flag spelling as a defect of its own, `--entity-id must be between …`
// — a failure everywhere outside the definition file.
//
// The `--entity-id` spelling is the one that proves the flag half is doing work.
// It was a must-PASS boundary case in published_field_names_test.go until this
// task, precisely because `entity-id` was not yet a subject; it is a must-FLAG
// case now, and moving it is the visible record that the class widened.
var declaredRangedFields = []RangedField{
	FieldTaskPriority,
	FieldTaskSeverity,
	FieldListLimit,
	FieldTaskID,
	FieldSprintID,
	FieldEntityID,
	FieldCommentID,
	FieldDependencyTaskID,
}

// TestRangedFieldsPublishOneUsableNameEach is to RangedField what
// TestPublishedNamesMatchTheSpecTable is to Field, minus the SPEC table: the
// range rule is published inside each command's own section of SPEC/COMMANDS.md
// rather than by a table of names, so there is no document column to compare
// against and the assertion is on the property that matters instead — every
// declared constant renders a real name, no two render the same one, and the
// zero value renders none.
//
// The names themselves are pinned where they are actually published: the
// rendered messages asserted in internal/models/error_message_dedup_test.go and
// against the binary in tests/test_55_error_string_parity.py.
func TestRangedFieldsPublishOneUsableNameEach(t *testing.T) {
	seen := make(map[string]RangedField, len(declaredRangedFields))
	for _, f := range declaredRangedFields {
		name := f.String()
		if name == "" || strings.HasPrefix(name, "RangedField(") {
			t.Errorf("RangedField(%d) renders %q, which names no field", uint8(f), name)
			continue
		}
		if first, dup := seen[name]; dup {
			t.Errorf("RangedField(%d) and RangedField(%d) both publish %q",
				uint8(first), uint8(f), name)
			continue
		}
		seen[name] = f
	}

	// The zero value must not pass for a field: a RangedField nobody assigned
	// would otherwise build a plausible-looking message about the wrong field.
	if got := RangedField(0).String(); got != "RangedField(0)" {
		t.Errorf("the zero RangedField renders %q, want %q", got, "RangedField(0)")
	}
}

// TestNumericRangeMessageAndErrorAgree pins the two halves of the range refusal
// to each other: NumericRangeMessage words the rule, NumericRangeError completes
// it with the offending value, and the complete line must contain the wording
// verbatim. A future edit that reworded one half alone would leave the sentinels
// in internal/models saying one thing and the line the user reads saying
// another, which is the split rmp task 318 removed.
func TestNumericRangeMessageAndErrorAgree(t *testing.T) {
	const (
		low  = 0
		high = 9
		got  = 99
	)
	rule := NumericRangeMessage(FieldTaskPriority, low, high)
	if want := "priority must be between 0 and 9"; rule != want {
		t.Fatalf("NumericRangeMessage = %q, want %q", rule, want)
	}

	err := NumericRangeError(errors.New(rule), got)
	if want := "validation error: priority must be between 0 and 9, got 99"; err.Error() != want {
		t.Errorf("NumericRangeError = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrValidation) {
		t.Error("NumericRangeError does not chain ErrValidation, so it would not map to exit code 6")
	}
}

// TestPublishedNamesMatchTheSpecTable reads the names out of the SPEC and
// compares them with the ones this package publishes, in order.
//
// The comparison is against the document rather than against a second list
// written here, because a list written here would be the very thing this rule
// exists to forbid: a second place where a field's name is spelled. If the
// table and the constants disagree, one of the two moved without the other.
func TestPublishedNamesMatchTheSpecTable(t *testing.T) {
	fromSpec := publishedNamesFromSpec(t)

	if len(fromSpec) != len(declaredFields) {
		t.Fatalf("SPEC/COMMANDS.md publishes %d fields %v, this package declares %d",
			len(fromSpec), fromSpec, len(declaredFields))
	}
	for i, want := range fromSpec {
		if got := declaredFields[i].String(); got != want {
			t.Errorf("field %d: SPEC publishes %q, this package publishes %q", i+1, want, got)
		}
	}
}

// publishedNamesFromSpec returns the Published field name column of the table in
// SPEC/COMMANDS.md § Published Field Names in Validation Messages, in the order
// the table lists it. It fails rather than returns nothing when the section, the
// table, or a row cannot be found, so a SPEC edit that moves the table is
// reported instead of silently turning this gate into a no-op.
func publishedNamesFromSpec(t *testing.T) []string {
	t.Helper()

	const heading = "### Published Field Names in Validation Messages"

	path := filepath.Join(repoRoot(t), filepath.FromSlash("SPEC/COMMANDS.md"))
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("reading SPEC/COMMANDS.md: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("SPEC/COMMANDS.md no longer contains the section %q", heading)
	}

	names := make([]string, 0, len(declaredFields))
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			break // the section ended
		}
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		if len(cells) < 2 {
			continue
		}
		name := strings.TrimSpace(cells[1])
		if name == "Published field name" || strings.HasPrefix(name, "---") {
			continue // header row, separator row
		}
		if !strings.HasPrefix(name, "`") || !strings.HasSuffix(name, "`") {
			t.Fatalf("the Published field name cell %q is not a code span; the table format changed", name)
		}
		names = append(names, strings.Trim(name, "`"))
	}
	if len(names) == 0 {
		t.Fatalf("no rows parsed out of the %q table; this gate is not reading what it thinks it is", heading)
	}
	return names
}

// TestTaskAndSprintTitlePublishTheSameName pins the one coincidence in the
// table. Task and Sprint each have a title and the two publish the same name,
// which is why one "title is required" sentinel can serve both entities. The
// coincidence is asserted rather than assumed: if the SPEC ever distinguishes
// them, the shared sentinel in internal/models has to be split, and this is what
// says so.
func TestTaskAndSprintTitlePublishTheSameName(t *testing.T) {
	if FieldTaskTitle == FieldSprintTitle {
		t.Error("the two titles must be distinct Field constants; the SPEC table lists them as separate fields")
	}
	if FieldTaskTitle.String() != FieldSprintTitle.String() {
		t.Errorf("task title publishes %q and sprint title publishes %q; internal/models shares one "+
			"ErrTitleRequired between them, which is only sound while the two names agree",
			FieldTaskTitle.String(), FieldSprintTitle.String())
	}
}

// TestFieldStringMarksAValueThatNamesNoField covers the zero value and an
// out-of-range conversion. Neither can arise in production — the gate in
// published_field_names_test.go refuses the conversion that would be needed —
// but if one ever did, the message must be visibly wrong rather than quietly
// plausible: an empty name or a borrowed one would read as a real refusal about
// some other field.
func TestFieldStringMarksAValueThatNamesNoField(t *testing.T) {
	for _, f := range []Field{Field(0), Field(len(publishedNames)), Field(200)} {
		name := f.String()
		if !strings.HasPrefix(name, "Field(") {
			t.Errorf("Field(%d).String() = %q, want the Field(N) form that marks it as naming no field", uint8(f), name)
		}
		for _, declared := range declaredFields {
			if name == declared.String() {
				t.Errorf("Field(%d) renders as %q, a real field name", uint8(f), name)
			}
		}
	}
}

// TestGovernedRefusalsCarryThePublishedNameAndTheirSentinel exercises every
// message constructor against every field. It pins two things at once: the name
// each message carries, and the sentinel it chains, since the sentinel is what
// SPEC/ARCHITECTURE.md maps to an exit code and a message with the right words
// and the wrong exit code is still wrong.
func TestGovernedRefusalsCarryThePublishedNameAndTheirSentinel(t *testing.T) {
	for _, f := range declaredFields {
		t.Run(f.String(), func(t *testing.T) {
			utf8Err := InvalidUTF8Error(f)
			if want := "validation error: " + f.String() + ": the value is not valid UTF-8"; utf8Err.Error() != want {
				t.Errorf("InvalidUTF8Error\n got: %q\nwant: %q", utf8Err.Error(), want)
			}
			if !errors.Is(utf8Err, ErrValidation) {
				t.Errorf("InvalidUTF8Error must wrap ErrValidation (exit 6); got %v", utf8Err)
			}

			control := ControlCharError(f)
			if want := "validation error: " + f.String() + ": control characters are not allowed"; control.Error() != want {
				t.Errorf("ControlCharError\n got: %q\nwant: %q", control.Error(), want)
			}
			if !errors.Is(control, ErrValidation) {
				t.Errorf("ControlCharError must wrap ErrValidation (exit 6); got %v", control)
			}

			large := FieldTooLargeError(f, 4096)
			if want := "field exceeds maximum size: " + f.String() + " exceeds maximum length of 4096 characters"; large.Error() != want {
				t.Errorf("FieldTooLargeError\n got: %q\nwant: %q", large.Error(), want)
			}
			if !errors.Is(large, ErrFieldTooLarge) {
				t.Errorf("FieldTooLargeError must wrap ErrFieldTooLarge (exit 6); got %v", large)
			}

			empty := FieldEmptyError(f)
			if want := "validation error: " + f.String() + " cannot be empty"; empty.Error() != want {
				t.Errorf("FieldEmptyError\n got: %q\nwant: %q", empty.Error(), want)
			}
			if !errors.Is(empty, ErrValidation) {
				t.Errorf("FieldEmptyError must wrap ErrValidation (exit 6); got %v", empty)
			}

			if want := f.String() + " is required"; RequiredFieldMessage(f) != want {
				t.Errorf("RequiredFieldMessage\n got: %q\nwant: %q", RequiredFieldMessage(f), want)
			}
		})
	}
}

// TestValidateUTF8NamesTheFieldItWasGiven closes the loop between the encoding
// rule and the name, exactly as the control-character test below does for its
// own rule: the refusal a malformed value produces must name the field the
// caller passed, and no other.
func TestValidateUTF8NamesTheFieldItWasGiven(t *testing.T) {
	for _, f := range declaredFields {
		err := ValidateUTF8("value with a lone \x80 continuation byte", f)
		if err == nil {
			t.Fatalf("%s: a lone continuation byte must be refused", f)
		}
		if want := f.String() + ": the value is not valid UTF-8"; !strings.Contains(err.Error(), want) {
			t.Errorf("%s: message\n got: %q\nwant substring: %q", f, err.Error(), want)
		}
	}
}

// TestValidateNoControlCharsNamesTheFieldItWasGiven closes the loop between the
// rule and the name: the refusal a value produces must name the field the caller
// passed, and no other.
func TestValidateNoControlCharsNamesTheFieldItWasGiven(t *testing.T) {
	for _, f := range declaredFields {
		err := ValidateNoControlChars("value with an \x1b escape", f)
		if err == nil {
			t.Fatalf("%s: an ESC must be refused", f)
		}
		if want := f.String() + ": control characters are not allowed"; !strings.Contains(err.Error(), want) {
			t.Errorf("%s: message\n got: %q\nwant substring: %q", f, err.Error(), want)
		}
	}
}

// repoRoot returns the module root. `go test` runs a package's tests with the
// working directory set to that package's own directory, so the root is two
// levels up from internal/utils. The go.mod check turns a wrong answer into a
// failure instead of turning these gates into no-ops.
func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s, so the repository root is not where these gates assume it is: %v", root, err)
	}
	return root
}

// TestIDEntityWordAgreesWithThePublishedName pins the relationship between the
// two names one id field carries, so that neither can be edited alone.
//
// An id is subject to two rules, and each publishes its own name for the same
// argument: the FORMAT rule says `invalid task ID: "X" (must be a positive
// integer)` and the ID RANGE rule says `task_id must be between 1 and
// 2147483647, got 0`. The two must be recognisably the same argument, and the
// derivation that makes them so is the one asserted here — the range name is the
// entity word with its spaces written as underscores and `_id` appended.
//
// Without this, `dependency_task_id` could drift to `dep_id` while the format
// refusal went on saying "dependency task", and a reader meeting both messages
// would have no way to tell they were about the same positional.
func TestIDEntityWordAgreesWithThePublishedName(t *testing.T) {
	ids := 0
	for _, f := range declaredRangedFields {
		word := f.IDEntity()
		if word == "" {
			continue // not an id
		}
		ids++
		want := strings.ReplaceAll(word, " ", "_") + "_id"
		if got := f.String(); got != want {
			t.Errorf("RangedField(%d) publishes the name %q and the entity word %q; "+
				"the two must be the same argument, so the name must be %q",
				uint8(f), got, word, want)
		}
	}
	if ids != 5 {
		t.Errorf("%d id fields carry an entity word, want 5; an id field was added or removed "+
			"without its entry in idEntityWords", ids)
	}
}

// TestOnlyIDFieldsCarryAnEntityWord is the other direction: a RangedField that
// is not an id must publish no entity word at all, so a caller cannot build an
// id-shaped message about `priority`.
func TestOnlyIDFieldsCarryAnEntityWord(t *testing.T) {
	for _, f := range []RangedField{FieldTaskPriority, FieldTaskSeverity, FieldListLimit, RangedField(0)} {
		if word := f.IDEntity(); word != "" {
			t.Errorf("RangedField(%d) is not an id but publishes the entity word %q", uint8(f), word)
		}
	}
}

// TestIDRangeMessageAndErrorAgree is to the id range rule what
// TestNumericRangeMessageAndErrorAgree is to the bounded-field rule: the rule is
// worded in one place, the refusal completes it with the offending value, and
// the complete line must contain the wording verbatim.
//
// It also pins the axis that made this rule worth converging: the failure CLASS
// is a parameter and the SENTENCE is not, because the four comment subcommands
// publish exit code 2 for a condition every other surface publishes as exit code
// 6 (SPEC/COMMANDS.md § Add Task Comment validation order, step 2).
func TestIDRangeMessageAndErrorAgree(t *testing.T) {
	rule := IDRangeMessage(FieldEntityID)
	if want := "entity_id must be between 1 and 2147483647"; rule != want {
		t.Fatalf("IDRangeMessage = %q, want %q", rule, want)
	}

	validation := IDRangeError(ErrValidation, FieldEntityID, "0")
	if want := "validation error: entity_id must be between 1 and 2147483647, got 0"; validation.Error() != want {
		t.Errorf("IDRangeError(ErrValidation) = %q, want %q", validation.Error(), want)
	}
	if !errors.Is(validation, ErrValidation) {
		t.Error("the supplied class must survive, or the refusal would not map to exit code 6")
	}

	misuse := IDRangeError(ErrInvalidInput, FieldCommentID, "0")
	if want := "invalid input: comment_id must be between 1 and 2147483647, got 0"; misuse.Error() != want {
		t.Errorf("IDRangeError(ErrInvalidInput) = %q, want %q", misuse.Error(), want)
	}
	if !errors.Is(misuse, ErrInvalidInput) {
		t.Error("the supplied class must survive, or the refusal would not map to exit code 2")
	}
	if errors.Is(misuse, ErrValidation) {
		t.Error("the comment subcommands publish exit code 2 for this condition, never exit code 6")
	}

	// The two classes differ; the sentence about the rule does not.
	if a, b := strings.TrimPrefix(validation.Error(), ErrValidation.Error()+": "),
		strings.TrimPrefix(misuse.Error(), ErrInvalidInput.Error()+": "); //
	strings.Replace(a, "entity_id", "", 1) != strings.Replace(b, "comment_id", "", 1) {
		t.Errorf("one rule, two sentences\n exit 6: %q\n exit 2: %q", a, b)
	}
}
