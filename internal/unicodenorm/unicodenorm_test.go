package unicodenorm_test

import (
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/FlavioCFOliveira/Groadmap/internal/unicodenorm"
)

// maxReportedCodePoints caps how many offending code points a failure names, so
// that a whole-of-Unicode sweep that goes wrong reports a diagnosis rather than a
// megabyte of output. Every failure also reports the total, so a cap can never
// hide the size of the fault.
const maxReportedCodePoints = 12

// fullCompositionExclusion is Unicode's Full_Composition_Exclusion property: the
// code points that carry a canonical decomposition and are nevertheless excluded
// from composition, so that Normalization Form C leaves them decomposed.
//
// IT IS TRANSCRIBED FROM THE UNICODE CHARACTER DATABASE, AND FROM NOWHERE ELSE.
// Source: https://www.unicode.org/Public/17.0.0/ucd/DerivedNormalizationProps.txt
// (Unicode 17.0.0, file dated 2025-08-01), every line whose property field is
// `Full_Composition_Exclusion`, run-length encoded here into inclusive ranges:
// 1,120 code points in 73 ranges. The same 1,120 code points are the property's
// value in Unicode 15.0.0, so the set below is not merely the current version's
// answer — it has not moved across the version change this test was written for.
//
// WHY A TRANSCRIBED LIST AND NOT A DERIVATION. The property has four sources
// (UAX #15, Full_Composition_Exclusion): the Script Specifics and Post
// Composition Version lists of `CompositionExclusions.txt`, which UAX #15 itself
// states cannot be derived; the singleton decompositions; and the non-starter
// decompositions. The last two are derivable in principle, but not from this
// module: golang.org/x/text/unicode/norm publishes the FULL, recursive canonical
// decomposition, so a singleton such as U+212B (whose one-step mapping is U+00C5)
// is indistinguishable here from an ordinary two-character composite. Deriving
// them would therefore require transcribing UnicodeData.txt instead, which is
// larger and no more authoritative.
//
// The alternative — asking the module — is what this test exists to check, so it
// cannot be the reference: `!norm.NFC.IsNormalString(s)` is the implementation
// under test, and `norm.NFC.QuickSpanString(s) != len(s)` is the defect it
// replaced. A reference has to come from outside the thing measured.
//
// The list needs no network at test time and MUST NOT acquire one: a test that
// fetched unicode.org would fail offline and would silently follow a property
// that moved instead of reporting it. When a future Unicode version does move the
// property, this test fails, names the code points, and updating this list from
// the URL above is then the deliberate act — exactly as regenerating the shipped
// search tables is the deliberate act when the Unicode version moves them
// (SPEC/BUILD.md § External Dependencies, Unicode Data Rules 6).
var fullCompositionExclusion = [][2]rune{
	{0x0340, 0x0341}, {0x0343, 0x0344}, {0x0374, 0x0374}, {0x037E, 0x037E}, {0x0387, 0x0387},
	{0x0958, 0x095F}, {0x09DC, 0x09DD}, {0x09DF, 0x09DF}, {0x0A33, 0x0A33}, {0x0A36, 0x0A36},
	{0x0A59, 0x0A5B}, {0x0A5E, 0x0A5E}, {0x0B5C, 0x0B5D}, {0x0F43, 0x0F43}, {0x0F4D, 0x0F4D},
	{0x0F52, 0x0F52}, {0x0F57, 0x0F57}, {0x0F5C, 0x0F5C}, {0x0F69, 0x0F69}, {0x0F73, 0x0F73},
	{0x0F75, 0x0F76}, {0x0F78, 0x0F78}, {0x0F81, 0x0F81}, {0x0F93, 0x0F93}, {0x0F9D, 0x0F9D},
	{0x0FA2, 0x0FA2}, {0x0FA7, 0x0FA7}, {0x0FAC, 0x0FAC}, {0x0FB9, 0x0FB9}, {0x1F71, 0x1F71},
	{0x1F73, 0x1F73}, {0x1F75, 0x1F75}, {0x1F77, 0x1F77}, {0x1F79, 0x1F79}, {0x1F7B, 0x1F7B},
	{0x1F7D, 0x1F7D}, {0x1FBB, 0x1FBB}, {0x1FBE, 0x1FBE}, {0x1FC9, 0x1FC9}, {0x1FCB, 0x1FCB},
	{0x1FD3, 0x1FD3}, {0x1FDB, 0x1FDB}, {0x1FE3, 0x1FE3}, {0x1FEB, 0x1FEB}, {0x1FEE, 0x1FEF},
	{0x1FF9, 0x1FF9}, {0x1FFB, 0x1FFB}, {0x1FFD, 0x1FFD}, {0x2000, 0x2001}, {0x2126, 0x2126},
	{0x212A, 0x212B}, {0x2329, 0x232A}, {0x2ADC, 0x2ADC}, {0xF900, 0xFA0D}, {0xFA10, 0xFA10},
	{0xFA12, 0xFA12}, {0xFA15, 0xFA1E}, {0xFA20, 0xFA20}, {0xFA22, 0xFA22}, {0xFA25, 0xFA26},
	{0xFA2A, 0xFA6D}, {0xFA70, 0xFAD9}, {0xFB1D, 0xFB1D}, {0xFB1F, 0xFB1F}, {0xFB2A, 0xFB36},
	{0xFB38, 0xFB3C}, {0xFB3E, 0xFB3E}, {0xFB40, 0xFB41}, {0xFB43, 0xFB44}, {0xFB46, 0xFB4E},
	{0x1D15E, 0x1D164}, {0x1D1BB, 0x1D1C0}, {0x2F800, 0x2FA1D},
}

// excludedCodePoints expands the transcribed ranges into the set the sweep
// compares against.
func excludedCodePoints() map[rune]bool {
	excluded := make(map[rune]bool, 1120)
	for _, r := range fullCompositionExclusion {
		for cp := r[0]; cp <= r[1]; cp++ {
			excluded[cp] = true
		}
	}
	return excluded
}

// TestIsCompositionExcluded_IsFullCompositionExclusion holds the composition
// exclusion IsCompositionExcluded reports to the property Unicode publishes,
// over every code point of Unicode and in BOTH directions.
//
// Both directions are required, and a count on its own would not give them. The
// defect this test was written for — reading NFC_QC != Yes, which is `No` OR
// `Maybe`, where the property is `No` alone — produced twelve FALSE POSITIVES
// under Unicode 17.0.0 and no false negatives, and the two errors could equally
// well have cancelled in a total. Each direction is therefore counted and named
// separately:
//
//   - a FALSE POSITIVE drops a composite from the table, so NFC returns the
//     decomposition of a code point that composes and Groadmap's NFC stops being
//     NFC. That is what happened;
//   - a FALSE NEGATIVE admits a composite Unicode excludes, so NFC composes text
//     into a form no NFC string may contain — the mirror fault, and no less
//     wrong.
//
// The reference is transcribed UCD data, never the module: see
// fullCompositionExclusion for why a derivation is not available here and why
// asking norm would make the test measure itself.
func TestIsCompositionExcluded_IsFullCompositionExclusion(t *testing.T) {
	excluded := excludedCodePoints()
	if len(excluded) != 1120 {
		t.Fatalf("the transcribed Full_Composition_Exclusion set holds %d code points, want 1120; "+
			"the ranges in this file are wrong and the sweep below would be measured against them",
			len(excluded))
	}

	falsePositives := make([]rune, 0, maxReportedCodePoints)
	falseNegatives := make([]rune, 0, maxReportedCodePoints)
	nFalsePositive, nFalseNegative := 0, 0

	for cp := rune(0); cp <= unicodenorm.MaxCodePoint; cp++ {
		if unicodenorm.IsSurrogate(cp) {
			continue // not a scalar value: it carries no property
		}
		got, want := unicodenorm.IsCompositionExcluded(cp), excluded[cp]
		switch {
		case got && !want:
			nFalsePositive++
			if len(falsePositives) < maxReportedCodePoints {
				falsePositives = append(falsePositives, cp)
			}
		case !got && want:
			nFalseNegative++
			if len(falseNegatives) < maxReportedCodePoints {
				falseNegatives = append(falseNegatives, cp)
			}
		}
	}

	for _, cp := range falsePositives {
		t.Errorf("U+%04X: IsCompositionExcluded says EXCLUDED, Full_Composition_Exclusion says "+
			"it is not; its composite is dropped from the table and NFC will leave it decomposed",
			cp)
	}
	if nFalsePositive > 0 {
		t.Errorf("%d code points are excluded by this package and not by Unicode (%d named above)",
			nFalsePositive, len(falsePositives))
	}
	for _, cp := range falseNegatives {
		t.Errorf("U+%04X: IsCompositionExcluded says NOT excluded, Full_Composition_Exclusion says "+
			"it is; NFC would compose text into a form no NFC string may contain", cp)
	}
	if nFalseNegative > 0 {
		t.Errorf("%d code points are excluded by Unicode and not by this package (%d named above)",
			nFalseNegative, len(falseNegatives))
	}
	if nFalsePositive > 0 || nFalseNegative > 0 {
		t.Errorf("the composition-exclusion property moved. If the Go toolchain or " +
			"golang.org/x/text changed the Unicode version this package reads (norm selects " +
			"tables15.0.0.go under !go1.27 and tables17.0.0.go under go1.27), re-transcribe " +
			"fullCompositionExclusion from DerivedNormalizationProps.txt of that version and " +
			"regenerate the shipped tables with `go generate ./internal/web/`. Do NOT relax " +
			"this test to make it pass")
	}
}

// TestNFC_AgreesWithTheModuleOnEverySingleCodePoint asserts the claim the whole
// of this package rests on, directly: NFC IS Normalization Form C.
//
// It is a stronger statement than counting exclusions, because it measures the
// OUTPUT of the rule rather than one input to it. A miscounted exclusion, a
// missing prefix entry, a wrong combining class, a broken blocking test in
// composeText — each of them changes a result here, and none of them need change
// the exclusion count.
//
// THE MODULE IS THE REFERENCE HERE, AND ONLY OVER SINGLE CODE POINTS. Compose
// explains that golang.org/x/text/unicode/norm composes a supplementary starter
// as though it were its low 16 bits, so over longer inputs the two forms differ
// BY DESIGN and the module would be the wrong reference. Over a single code point
// the defect cannot arise — the input carries no pair for it to key on — so the
// two must agree on all 1,112,064 scalar values, and that is exactly the claim
// SPEC/BUILD.md § External Dependencies, Unicode Data Rules 3 publishes.
//
// This is the one call to norm.NFC.String in Groadmap. Rule 3 forbids it in the
// rule; here it is the yardstick the rule is held against, never a value any
// caller receives, and the prohibition would be unmeasurable without it.
func TestNFC_AgreesWithTheModuleOnEverySingleCodePoint(t *testing.T) {
	swept, faults := 0, 0
	named := make([]rune, 0, maxReportedCodePoints)

	for cp := rune(0); cp <= unicodenorm.MaxCodePoint; cp++ {
		if unicodenorm.IsSurrogate(cp) {
			continue
		}
		swept++
		s := string(cp)
		ours, reference := unicodenorm.NFC(s), norm.NFC.String(s)
		if ours == reference {
			continue
		}
		faults++
		if len(named) >= maxReportedCodePoints {
			continue
		}
		named = append(named, cp)
		t.Errorf("U+%04X: this package normalises to %X, Normalization Form C is %X",
			cp, []rune(ours), []rune(reference))
	}

	if swept != 1112064 {
		t.Errorf("swept %d scalar values, want 1112064; the sweep is not the whole of Unicode",
			swept)
	}
	if faults > 0 {
		t.Errorf("%d of the %d single code points are not returned in Normalization Form C "+
			"(%d named above). This package's NFC is not NFC", faults, swept, len(named))
	}
}

// TestNFC_ComposesTheQuickCheckMaybeCodePoints is the named regression for the
// defect that TestIsCompositionExcluded_IsFullCompositionExclusion generalises.
//
// These twelve carry BOTH a canonical decomposition and NFC_QC=Maybe, a
// combination Unicode 15.0.0 had no instance of and Unicode 16.0.0 introduced.
// Under `norm.NFC.QuickSpanString(s) != len(s)` every one of them was read as a
// composition exclusion, its composite never entered the table, and NFC returned
// its decomposition. Each MUST normalise to itself: it is a composite Unicode
// composes, not one Unicode excludes.
//
// It is deliberately stated as behaviour of NFC and not as a property lookup, so
// that it keeps testing the thing that mattered — the text a search compares —
// however the predicate underneath is next rewritten.
func TestNFC_ComposesTheQuickCheckMaybeCodePoints(t *testing.T) {
	for _, cp := range []rune{
		0x113C5, 0x113C7, 0x113C8, // Tulu-Tigalari vowel signs AI, OO, AU
		0x16121, 0x16122, 0x16123, 0x16124, // Gurung Khema vowel signs U, UU, E, EE
		0x16125, 0x16126, 0x16127, 0x16128, // Gurung Khema vowel signs AI, O, OO, AU
		0x16D68, // Kirat Rai vowel sign AI
	} {
		if unicodenorm.IsCompositionExcluded(cp) {
			t.Errorf("U+%04X: reported as excluded from composition; it is NFC_QC=Maybe, which "+
				"is not NFC_QC=No, and Full_Composition_Exclusion is false of it", cp)
		}
		if got := unicodenorm.NFC(string(cp)); got != string(cp) {
			t.Errorf("U+%04X: NFC returns %X, want the code point itself; it is a composite "+
				"Unicode composes", cp, []rune(got))
		}
	}
}
