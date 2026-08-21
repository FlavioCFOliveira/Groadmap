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
