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

// probeLimit is the maximum the tests below hand to TrimFreeText and
// RequireFreeText when the cap is not the rule under test. It is deliberately
// wide enough that no probe in this file can reach it, so a verdict here is
// always the verdict of the rule the test names.
//
// It is a literal rather than one of the real maximums because those are
// declared in package models, which this package cannot import: models imports
// utils. Where the cap IS the rule under test the test states its own small
// limit, which is what makes the boundary it exercises visible on the line.
const probeLimit = 4096

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
			_, err := RequireFreeText(tc.value, FieldSprintTitle, probeLimit)
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
		stored, err := RequireFreeText(padded, FieldSprintTitle, probeLimit)
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

	stored, err := TrimFreeText("\n"+body+"\n", FieldCommentBody, probeLimit)
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
	stored, err := TrimFreeText("   ", FieldTaskCompletionSummary, probeLimit)
	if err != nil {
		t.Fatalf("TrimFreeText(spaces) = %v, want no error for an optional field", err)
	}
	if stored != "" {
		t.Errorf("TrimFreeText(spaces) = %q, want the empty string", stored)
	}

	if _, err := TrimFreeText(verticalTab, FieldTaskCompletionSummary, probeLimit); err == nil {
		t.Fatal("TrimFreeText accepted a summary made only of VT")
	} else if want := ControlCharError(FieldTaskCompletionSummary); err.Error() != want.Error() {
		t.Errorf("a VT-only summary\n got: %v\nwant: %v", err, want)
	}
}

// TestTheLengthCapAnswersBeforeEitherContentRule is the unit-level statement of
// the position rmp task 302 settles, and the test a reversal must break.
//
// The discriminating input is a value that breaks the cap AND one of the content
// rules at once. Every one of the three rules refuses with exit code 6, so the
// exit code does not separate them and neither does "it was refused": what
// separates the orders is WHICH message comes back.
//
// The cap is first because the comment body's bounded standard-input reader
// settles the length verdict without buffering the whole value, which no order
// that judged the encoding first could allow. See TrimFreeText.
func TestTheLengthCapAnswersBeforeEitherContentRule(t *testing.T) {
	const limit = 16
	over := strings.Repeat("a", limit+1)

	cases := []struct {
		name  string
		value string
	}{
		{"over the cap and carrying a BEL", over + "\a"},
		{"over the cap and carrying a VT", over + verticalTab},
		{"over the cap and carrying an ESC in the middle", over[:4] + "\x1b" + over[4:]},
		{"over the cap and not valid UTF-8", over + "\xff"},
		{"over the cap, invalid UTF-8 AND a BEL", over + "\xff\a"},
		{"over the cap only once the padding is ignored", "  " + over + "  "},
	}

	want := FieldTooLargeError(FieldSprintTitle, limit).Error()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RequireFreeText(tc.value, FieldSprintTitle, limit)
			if err == nil {
				t.Fatalf("RequireFreeText accepted %q, which is over a limit of %d", tc.value, limit)
			}
			switch err.Error() {
			case want:
				return
			case ControlCharError(FieldSprintTitle).Error():
				t.Fatalf("%s was refused as a CONTROL CHARACTER; the cap must answer first\n got: %v\nwant: %v",
					tc.name, err, want)
			case InvalidUTF8Error(FieldSprintTitle).Error():
				t.Fatalf("%s was refused for its ENCODING; the cap must answer first\n got: %v\nwant: %v",
					tc.name, err, want)
			default:
				t.Fatalf("%s\n got: %v\nwant: %v", tc.name, err, want)
			}
		})
	}
}

// TestTheCapDoesNotSwallowTheContentRules is the other side of the boundary, and
// what stops the test above from passing on an implementation that simply
// stopped applying the content rules. A value WITHIN the cap must still be
// refused for its encoding, and then for its control characters, in that order.
func TestTheCapDoesNotSwallowTheContentRules(t *testing.T) {
	const limit = 64

	for _, tc := range []struct {
		name  string
		value string
		want  error
	}{
		{"within the cap, invalid UTF-8", "Expiry\xffhardening", InvalidUTF8Error(FieldSprintTitle)},
		{"within the cap, a BEL", "Expiry\ahardening", ControlCharError(FieldSprintTitle)},
		{"within the cap, invalid UTF-8 AND a BEL", "Expiry\xff\ahardening", InvalidUTF8Error(FieldSprintTitle)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RequireFreeText(tc.value, FieldSprintTitle, limit)
			if err == nil {
				t.Fatalf("RequireFreeText accepted %q", tc.value)
			}
			if err.Error() != tc.want.Error() {
				t.Errorf("%s\n got: %v\nwant: %v", tc.name, err, tc.want)
			}
		})
	}
}

// TestTheCapMeasuresTheStoredValue pins WHAT the cap counts now that it lives
// inside the ordering: the trimmed value, which is the value stored
// (SPEC/MODELS.md, Free-Text Emptiness and Trimming Constraint, Rule 2), and
// therefore never a value the column's CHECK constraint would then reject.
func TestTheCapMeasuresTheStoredValue(t *testing.T) {
	const limit = 8
	at := strings.Repeat("a", limit)

	stored, err := RequireFreeText("   "+at+"   ", FieldSprintTitle, limit)
	if err != nil {
		t.Fatalf("a value of exactly %d characters with surrounding whitespace was refused: %v", limit, err)
	}
	if stored != at {
		t.Errorf("stored = %q, want %q", stored, at)
	}

	if _, err := RequireFreeText(at+"a", FieldSprintTitle, limit); err == nil {
		t.Fatalf("a value of %d characters was accepted against a limit of %d", limit+1, limit)
	}
}
