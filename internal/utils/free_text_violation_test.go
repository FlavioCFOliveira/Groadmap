package utils

// Gates for the field-agnostic form of the two free-text content rules, added
// by rmp task 298 so the graph write path could apply the identical pair to
// Cypher property values without a second copy of it.
//
// The risk this file exists against is drift. FreeTextViolation.Reason and the
// two message constructors publish the same wording, and InspectFreeText and
// ValidateFreeText reach the same verdict in the same order; if either pair ever
// disagreed, one policy would be realised twice — the defect rmp task 294
// removed from this package and the one task 298 must not reintroduce.

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// controlCharValue carries a forbidden control character and is otherwise
// well-formed UTF-8, so it exercises the control-character rule alone.
const controlCharValue = "deploy \x1b[31mFAILED\x1b[0m"

// TestInspectFreeTextAgreesWithValidateFreeText holds the two entry points to
// the same pair of rules against each other, over every field and over every
// input shape the rules distinguish.
//
// ValidateFreeText is the one with a Field and a message; InspectFreeText is the
// one the graph write path uses, which has neither. They must reach the same
// verdict on every value, or a Cypher property value would be held to a
// different standard than a task title.
func TestInspectFreeTextAgreesWithValidateFreeText(t *testing.T) {
	type freeTextCase struct {
		name  string
		value string
		want  FreeTextViolation
	}
	corpus := testenv.MalformedUTF8Corpus()
	cases := make([]freeTextCase, 0, 8+len(corpus))
	cases = append(cases,
		freeTextCase{"plain text", "Sprint 38 correctness sweep", FreeTextValid},
		freeTextCase{"permitted whitespace controls", "line one\nline two\ttabbed\r\n", FreeTextValid},
		freeTextCase{"non-ASCII text", "especificação — 知識グラフ 🚀", FreeTextValid},
		freeTextCase{"empty", "", FreeTextValid},
		freeTextCase{"ESC", controlCharValue, FreeTextControlChars},
		freeTextCase{"bidi override", "invoice\u202egpj.exe", FreeTextControlChars},
		freeTextCase{"DEL", "batch\x7fclosed", FreeTextControlChars},
		freeTextCase{"vertical tab, which is also whitespace", "\v", FreeTextControlChars},
	)
	for _, c := range corpus {
		cases = append(cases, freeTextCase{"malformed UTF-8: " + c.Name, c.Value, FreeTextInvalidUTF8})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InspectFreeText(tc.value); got != tc.want {
				t.Fatalf("InspectFreeText(%q) = %v, want %v", tc.value, got, tc.want)
			}
			for _, f := range declaredFields {
				err := ValidateFreeText(tc.value, f)
				switch tc.want {
				case FreeTextValid:
					if err != nil {
						t.Errorf("field %s: InspectFreeText accepts %q but ValidateFreeText returns %v", f, tc.value, err)
					}
				case FreeTextInvalidUTF8:
					if want := InvalidUTF8Error(f); err == nil || err.Error() != want.Error() {
						t.Errorf("field %s: ValidateFreeText(%q) = %v, want %v", f, tc.value, err, want)
					}
				case FreeTextControlChars:
					if want := ControlCharError(f); err == nil || err.Error() != want.Error() {
						t.Errorf("field %s: ValidateFreeText(%q) = %v, want %v", f, tc.value, err, want)
					}
				}
			}
		})
	}
}

// TestInspectFreeTextKeepsTheSpecifiedOrder is the discriminating case for the
// order, and the one a reversal must break.
//
// A value that is BOTH malformed UTF-8 AND carrying a forbidden control
// character is refused by either rule, so "it was refused" proves nothing about
// which ran first. This asserts WHICH rule answered — and the order matters for
// the reason ValidateFreeText documents: an invalid byte decodes to U+FFFD,
// which is not a forbidden code point, so a control-character check that ran
// first would report nothing for a value that is only malformed.
func TestInspectFreeTextKeepsTheSpecifiedOrder(t *testing.T) {
	for _, c := range testenv.MalformedUTF8Corpus() {
		both := c.Value + controlCharValue
		if got := InspectFreeText(both); got != FreeTextInvalidUTF8 {
			t.Errorf("a value that breaks both rules reported %v; the encoding rule must answer first (%s)", got, c.Name)
		}
	}
}

// TestFreeTextViolationReasonIsWhatTheConstructorsPublish is the anti-drift gate
// between the two ways a refusal reaches a user.
//
// The graph write path names its offending value by a property key and appends
// Reason(); every other surface names a Field and calls the constructor. This
// asserts the two carry the identical sentence, by deriving the expected text
// from the constructor's own output rather than by repeating it here — a
// repetition would be the very duplication the gate exists to prevent.
func TestFreeTextViolationReasonIsWhatTheConstructorsPublish(t *testing.T) {
	cases := []struct {
		violation FreeTextViolation
		build     func(Field) error
	}{
		{FreeTextInvalidUTF8, InvalidUTF8Error},
		{FreeTextControlChars, ControlCharError},
	}

	for _, tc := range cases {
		reason := tc.violation.Reason()
		if reason == "" {
			t.Fatalf("%v carries no wording, so a caller without a Field has nothing to publish", tc.violation)
		}
		for _, f := range declaredFields {
			err := tc.build(f)
			// The constructor's message is "validation error: <field>: <reason>",
			// so the reason is exactly its tail. Deriving the expectation this
			// way means a reworded constructor fails here instead of silently
			// leaving the graph publishing the old sentence.
			want := f.String() + ": " + reason
			if !strings.HasSuffix(err.Error(), want) {
				t.Errorf("%v: constructor publishes %q, which does not end in %q", tc.violation, err.Error(), want)
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("%v: the refusal must carry ErrValidation (exit 6), got %v", tc.violation, err)
			}
		}
	}
}

// TestFreeTextValidCarriesNoReason keeps the zero value honest: a violation that
// was never assigned names no rule, so a caller that publishes Reason() without
// first checking cannot emit a plausible-looking sentence about nothing.
func TestFreeTextValidCarriesNoReason(t *testing.T) {
	var unset FreeTextViolation
	if unset != FreeTextValid {
		t.Fatalf("the zero FreeTextViolation is %v, want FreeTextValid", unset)
	}
	if got := FreeTextValid.Reason(); got != "" {
		t.Errorf("FreeTextValid.Reason() = %q, want the empty string", got)
	}
	// A value outside the declared set must not read as one of the rules
	// either; the bounds check is what stops it from panicking or inventing one.
	if got := FreeTextViolation(200).Reason(); got != "" {
		t.Errorf("an out-of-range violation reported %q, want the empty string", got)
	}
}

// TestValidateNoControlCharsStillJudgesTheControlRuleAlone guards the split that
// made InspectFreeText possible: ValidateNoControlChars and ValidateUTF8 are the
// two rules applied SEPARATELY, and a caller that wants one must not silently
// get the other. The streaming comment-body reader depends on exactly that.
func TestValidateNoControlCharsStillJudgesTheControlRuleAlone(t *testing.T) {
	for _, c := range testenv.MalformedUTF8Corpus() {
		if err := ValidateNoControlChars(c.Value, FieldCommentBody); err != nil {
			t.Errorf("ValidateNoControlChars refused %q, but malformed UTF-8 is the encoding rule's business: %v", c.Name, err)
		}
		if err := ValidateUTF8(c.Value, FieldCommentBody); err == nil {
			t.Errorf("ValidateUTF8 admitted %q", c.Name)
		}
	}
	if err := ValidateUTF8(controlCharValue, FieldCommentBody); err != nil {
		t.Errorf("ValidateUTF8 refused a well-formed value carrying a control character: %v", err)
	}
	if err := ValidateNoControlChars(controlCharValue, FieldCommentBody); err == nil {
		t.Error("ValidateNoControlChars admitted a value carrying ESC")
	}
}
