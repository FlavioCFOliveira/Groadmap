package utils

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// This file gates the rule itself: SPEC/MODELS.md § Free-Text UTF-8 Encoding
// Constraint, as ValidateUTF8 implements it, and the order it stands in relative
// to the control-character rule, as ValidateFreeText welds it.
//
// The defect behind rmp task 180: the control-character rule decodes a value
// rune by rune, and an invalid byte decodes to U+FFFD, which is not a forbidden
// code point. Invalid UTF-8 therefore passed every free-text validator and was
// written verbatim into a TEXT column, while the JSON encoder that produces
// command output replaced each such byte with U+FFFD — so what was stored, what
// was printed and what was supplied were three different strings.

// TestMalformedCorpusIsolatesTheEncodingRule proves the shared corpus is worth
// building a proof on, before any proof is built on it.
//
// Both halves matter. A corpus entry that were valid UTF-8 would make every
// test below pass for nothing. An entry that ALSO carried a forbidden control
// character would be refused either way, so a test asserting the encoding
// message would be measuring the order of two rules rather than the rule under
// test — and would keep passing if ValidateUTF8 stopped working entirely.
func TestMalformedCorpusIsolatesTheEncodingRule(t *testing.T) {
	corpus := testenv.MalformedUTF8Corpus()
	if len(corpus) != 5 {
		t.Fatalf("the corpus holds %d entries; SPEC/MODELS.md enumerates 5 malformed shapes", len(corpus))
	}

	for _, c := range corpus {
		t.Run(c.Name, func(t *testing.T) {
			if utf8.ValidString(c.Value) {
				t.Errorf("%q is valid UTF-8, so it proves nothing about the encoding rule.\n  %s", c.Value, c.Why)
			}
			if err := ValidateNoControlChars(c.Value, FieldTaskTitle); err != nil {
				t.Errorf("%q also breaks the control-character rule (%v), so a refusal of it "+
					"would not show the encoding rule ran", c.Value, err)
			}
		})
	}
}

// TestValidateUTF8RefusesEveryMalformedShape is the rule, over every shape the
// SPEC enumerates and every field it governs.
func TestValidateUTF8RefusesEveryMalformedShape(t *testing.T) {
	for _, c := range testenv.MalformedUTF8Corpus() {
		for _, f := range declaredFields {
			t.Run(c.Name+"/"+f.String(), func(t *testing.T) {
				err := ValidateUTF8(c.Value, f)
				if err == nil {
					t.Fatalf("ValidateUTF8(%q) = nil, want a refusal.\n  %s", c.Value, c.Why)
				}
				if !errors.Is(err, ErrValidation) {
					t.Errorf("refusal must wrap ErrValidation (exit 6); got %v", err)
				}
				if want := "validation error: " + f.String() + ": the value is not valid UTF-8"; err.Error() != want {
					t.Errorf("message\n got: %q\nwant: %q", err.Error(), want)
				}
			})
		}
	}
}

// TestValidateUTF8AcceptsLegitimateText is the other side of the rule: the
// constraint refuses malformed BYTES, never unfamiliar characters. A rule that
// turned out to reject accented Latin, CJK or emoji would be a regression of a
// different kind, and one this project has explicit round-trip coverage for
// (tests/test_16_boundary_unicode.py).
func TestValidateUTF8AcceptsLegitimateText(t *testing.T) {
	for _, value := range []string{
		"",
		"Reconcile the settlement ledger before the cut-off",
		"medição de latência: 1.2 ms",
		"監査ログの検証",
		"shipped \U0001F680 and measured",
		"Ω≈ç√∫˜µ≤≥÷",
		"line one\nline two\tcolumn\r\n",
		strings.Repeat("é", 4096),
		"�", // the replacement character itself is a perfectly valid code point
	} {
		if err := ValidateUTF8(value, FieldTaskTitle); err != nil {
			t.Errorf("ValidateUTF8(%q) = %v, want nil", value, err)
		}
	}
}

// TestValidateFreeTextChecksEncodingBeforeControlCharacters is the ORDER, which
// the user fixed as a decision on rmp task 180 and SPEC/MODELS.md § Free-Text
// UTF-8 Encoding Constraint records under "Order": the encoding check runs
// immediately before the control-character check, everywhere.
//
// A value that breaks both rules is the only input that can tell the two orders
// apart, so that is what is fed in. The assertion is on the message, not merely
// on failure: both refusals carry ErrValidation and exit code 6, so an exit code
// cannot distinguish them.
func TestValidateFreeTextChecksEncodingBeforeControlCharacters(t *testing.T) {
	for _, c := range testenv.MalformedUTF8Corpus() {
		t.Run(c.Name, func(t *testing.T) {
			both := c.Value + " \x1b[31m"

			err := ValidateFreeText(both, FieldCommentBody)
			if err == nil {
				t.Fatal("a value that breaks both rules must be refused")
			}
			if want := "validation error: body: the value is not valid UTF-8"; err.Error() != want {
				t.Errorf("the encoding check must run first.\n got: %q\nwant: %q", err.Error(), want)
			}
		})
	}
}

// TestValidateFreeTextStillAppliesTheControlCharacterRule keeps the test above
// honest. Welding the two rules into one call could have dropped the second one
// silently, and every encoding test would still pass. So: a value that is
// perfectly well-formed UTF-8 and carries a forbidden code point must still be
// refused, with the control-character message.
func TestValidateFreeTextStillAppliesTheControlCharacterRule(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"ESC", "Ledger batch \x1b[31mSEPA-20260815-004\x1b[0m reversed"},
		{"NUL", "Ledger batch SEPA-20260815-004\x00 reversed"},
		{"DEL", "Ledger batch SEPA-20260815-004\x7f reversed"},
		{"vertical tab", "Ledger batch\x0bSEPA-20260815-004"},
		{"bidi override", "Ledger posting reversed \u202egnitsop regdeL"}, // U+202E RIGHT-TO-LEFT OVERRIDE
		{"byte order mark", "\ufeffLedger batch SEPA-20260815-004"},       // U+FEFF, invisible
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !utf8.ValidString(tc.value) {
				t.Fatalf("%q is not valid UTF-8, so it cannot show the control-character rule ran", tc.value)
			}
			err := ValidateFreeText(tc.value, FieldCommentBody)
			if err == nil {
				t.Fatal("a forbidden control character must still be refused")
			}
			if want := "validation error: body: control characters are not allowed"; err.Error() != want {
				t.Errorf("message\n got: %q\nwant: %q", err.Error(), want)
			}
		})
	}
}

// TestValidateFreeTextAcceptsWhatBothRulesAccept closes the set: a value that
// breaks neither rule must pass, so the pairing cannot be satisfied by a
// validator that simply refuses everything.
func TestValidateFreeTextAcceptsWhatBothRulesAccept(t *testing.T) {
	for _, value := range []string{
		"Reconcile the settlement ledger before the cut-off",
		"medição de latência: 1.2 ms\nsegunda linha\tcoluna",
		"監査ログの検証 \U0001F680",
	} {
		if err := ValidateFreeText(value, FieldSprintDescription); err != nil {
			t.Errorf("ValidateFreeText(%q) = %v, want nil", value, err)
		}
	}
}

// TestTrimmingCannotChangeTheEncodingVerdict pins the fact ValidateFreeText's
// documentation leans on, and that the whole-value and streaming comment-body
// paths both rely on: strings.TrimSpace removes only runes for which
// unicode.IsSpace is true, and no invalid byte decodes to one — an invalid byte
// decodes to U+FFFD, which is not whitespace. Trimming therefore neither
// introduces nor removes an encoding failure, which is why it does not matter
// whether the rule is applied before or after the trim.
//
// It is asserted rather than assumed because the two comment-body paths hand
// ValidateCommentBody DIFFERENT values — the raw stream on one side, a trimmed
// proxy on the other — and their agreement on this rule rests on exactly this.
func TestTrimmingCannotChangeTheEncodingVerdict(t *testing.T) {
	corpus := testenv.MalformedUTF8Corpus()
	values := make([]string, 0, 5*len(corpus)+4)
	for _, c := range corpus {
		values = append(values,
			c.Value,
			"  \t\n"+c.Value,
			c.Value+" \r\n\t ",
			"\v\f "+c.Value+" \f\v",
			" "+c.Value+"",
		)
	}
	values = append(values,
		"Reconcile the settlement ledger",
		"  Reconcile the settlement ledger  ",
		"",
		"   ",
	)

	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if utf8.ValidString(value) != utf8.ValidString(trimmed) {
			t.Errorf("trimming changed the encoding verdict of %q -> %q", value, trimmed)
		}
	}
}
