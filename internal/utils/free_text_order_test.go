package utils

import (
	"errors"
	"strings"
	"testing"
)

// The two forbidden control characters that strings.TrimSpace also removes, and
// the whole reason the sequence in SPEC/MODELS.md (Free-Text Emptiness and
// Trimming Constraint) has three steps rather than two.
const (
	verticalTab = "\v" // VT, 0x0B
	formFeed    = "\f" // FF, 0x0C
)

// Two whitespace characters outside ASCII. Neither is a forbidden control
// character, so both belong to the emptiness rule and not to the
// control-character rule -- which is what makes them the control group for the
// tests below. They are written as escapes because they are invisible, and they
// are here because the whitespace set is Go's unicode.IsSpace, wider than ASCII;
// SPEC/COMMANDS.md (Emptiness Constraint, acceptance criterion 7) names these
// two specifically.
const (
	noBreakSpace = "\u00a0" // U+00A0 NO-BREAK SPACE
	nextLine     = "\u0085" // U+0085 NEXT LINE
)

// TestRequireFreeTextJudgesControlCharactersBeforeTheTrim is the unit-level
// statement of the order, and the test the reversal must break.
//
// A value made ONLY of VT (or of FF) is the discriminating input: it is
// forbidden by the control-character rule AND it is whitespace to
// strings.TrimSpace, so the two possible orders give two different verdicts and
// nothing else can tell them apart.
//
//   - control characters first, then trim: the refusal names the CONTROL
//     CHARACTER rule. This is the specified order.
//   - trim first, then everything else: the character is gone before the check
//     runs, nothing is left, and the refusal names the EMPTINESS rule -- the
//     input is refused for the wrong reason, and the same reordering silently
//     ACCEPTS "VT + real text", which is the CWE-150 hole itself.
//
// Asserting "it was refused" would not discriminate: both orders refuse a
// VT-only value, with the same exit code. This test asserts WHICH rule refused
// it.
func TestRequireFreeTextJudgesControlCharactersBeforeTheTrim(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  error
	}{
		{"only VT", verticalTab, ControlCharError(FieldSprintTitle)},
		{"only FF", formFeed, ControlCharError(FieldSprintTitle)},
		{"VT among spaces", "  " + verticalTab + "  ", ControlCharError(FieldSprintTitle)},
		{"leading VT then text", verticalTab + "Expiry hardening", ControlCharError(FieldSprintTitle)},
		{"text then trailing FF", "Expiry hardening" + formFeed, ControlCharError(FieldSprintTitle)},

		// The other side of the boundary: whitespace the control-character rule
		// permits leaves the emptiness rule to answer.
		{"only spaces", "   ", FieldEmptyError(FieldSprintTitle)},
		{"only TAB, LF and CR", "\t\n\r ", FieldEmptyError(FieldSprintTitle)},
		{"only U+00A0", noBreakSpace, FieldEmptyError(FieldSprintTitle)},
		{"only U+0085", nextLine, FieldEmptyError(FieldSprintTitle)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RequireFreeText(tc.value, FieldSprintTitle)
			if err == nil {
				t.Fatalf("RequireFreeText(%q) returned no error", tc.value)
			}
			if err.Error() != tc.want.Error() {
				t.Errorf("RequireFreeText(%q)\n got: %v\nwant: %v", tc.value, err, tc.want)
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("refusal must wrap ErrValidation (exit 6); got %v", err)
			}
		})
	}
}

// TestRequireFreeTextReturnsTheTrimmedValue is Rule 2 at the helper level: what
// comes back is what the caller stores, and it differs from what was supplied
// exactly at the edges.
func TestRequireFreeTextReturnsTheTrimmedValue(t *testing.T) {
	const core = "Close the JWT boundary-second defect"

	for _, padded := range []string{
		"  " + core + "  ",
		"\t" + core + "\n",
		"\r\n" + core + "\r\n",
		noBreakSpace + core + nextLine,
	} {
		stored, err := RequireFreeText(padded, FieldSprintTitle)
		if err != nil {
			t.Fatalf("RequireFreeText(%q) = %v, want no error", padded, err)
		}
		if stored != core {
			t.Errorf("RequireFreeText(%q) = %q, want %q", padded, stored, core)
		}
	}
}

// TestTrimFreeTextLeavesInteriorWhitespaceAlone pins the half of Rule 2 that is
// easy to overreach: only the edges are removed. A comment body, and any other
// free-text value, keeps its line breaks and its interior runs of spaces.
func TestTrimFreeTextLeavesInteriorWhitespaceAlone(t *testing.T) {
	const body = "First line.\n\n  Indented second line.\tTabbed."

	stored, err := TrimFreeText("\n"+body+"\n", FieldCommentBody)
	if err != nil {
		t.Fatalf("TrimFreeText = %v, want no error", err)
	}
	if stored != body {
		t.Errorf("TrimFreeText collapsed interior whitespace\n got: %q\nwant: %q", stored, body)
	}
	if strings.Count(stored, "\n") != strings.Count(body, "\n") {
		t.Errorf("line breaks were not preserved: %q", stored)
	}
}

// TestTrimFreeTextDoesNotJudgeEmptiness is the completion_summary case: the one
// free-text field Rule 1 does not govern. TrimFreeText must return the empty
// string without an error, because "task stat" accepts a transition to COMPLETED
// that supplies no --summary at all, so a summary that trims away is a summary
// the caller chose not to write.
//
// It must still refuse a forbidden control character, and it must refuse it
// BEFORE the trim: a summary made only of VT is a control-character violation,
// not an absent summary.
func TestTrimFreeTextDoesNotJudgeEmptiness(t *testing.T) {
	stored, err := TrimFreeText("   ", FieldTaskCompletionSummary)
	if err != nil {
		t.Fatalf("TrimFreeText(spaces) = %v, want no error for an optional field", err)
	}
	if stored != "" {
		t.Errorf("TrimFreeText(spaces) = %q, want the empty string", stored)
	}

	if _, err := TrimFreeText(verticalTab, FieldTaskCompletionSummary); err == nil {
		t.Fatal("TrimFreeText accepted a summary made only of VT")
	} else if want := ControlCharError(FieldTaskCompletionSummary); err.Error() != want.Error() {
		t.Errorf("a VT-only summary\n got: %v\nwant: %v", err, want)
	}
}
