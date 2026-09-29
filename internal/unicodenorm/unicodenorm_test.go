package unicodenorm_test

import (
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/FlavioCFOliveira/Groadmap/internal/unicodenorm"
)

// TestNFC_IsTheModulesNormalizationFormC pins NFC to golang.org/x/text/unicode/norm's
// Normalization Form C, the one normalisation the tasks page's search and the
// graph key audit share (SPEC/BUILD.md § External Dependencies, Unicode Data
// Rules 3; SPEC/WEB.md Acceptance Criterion 155).
//
// The witnesses cover the four shapes the rule has to get right: a string that is
// already NFC and is ASCII (the fast path), a decomposed Latin letter that
// composes, marks in non-canonical order that canonical ordering sorts before
// composing, and a composition exclusion that NFC leaves decomposed.
func TestNFC_IsTheModulesNormalizationFormC(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"ascii is returned untouched", "Reconcile settlement window 42", "Reconcile settlement window 42"},
		{"decomposed e acute composes", "Cafe\u0301 Lisboa", "Café Lisboa"},
		{"precomposed stays precomposed", "Café Lisboa", "Café Lisboa"},
		{"marks are canonically ordered then composed", "he\u0302\u0323", "hệ"},
		{"a composition exclusion stays decomposed", "\u0958", "\u0915\u093C"},
		{"U+0130 is already NFC", "İstanbul", "İstanbul"},
		{"I followed by combining dot composes to U+0130", "I\u0307stanbul", "İstanbul"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unicodenorm.NFC(tc.in)
			if got != tc.want {
				t.Errorf("NFC(%+q) = %+q, want %+q", tc.in, got, tc.want)
			}
			if module := norm.NFC.String(tc.in); got != module {
				t.Errorf("NFC(%+q) = %+q, but the module's Normalization Form C is %+q", tc.in, got, module)
			}
		})
	}
}
