package unicodenorm_test

import (
	"bytes"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"

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
// replaced. A reference has to come from outside the thing measured
// (SPEC/BUILD.md § External Dependencies, Unicode Data Rules 3).
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
//   - a FALSE POSITIVE drops a composite from the table the browser is shipped,
//     so the browser's copy of the rule returns the decomposition of a code point
//     that composes and stops being NFC. That is what happened;
//   - a FALSE NEGATIVE admits a composite Unicode excludes, so the browser's copy
//     composes text into a form no NFC string may contain — the mirror fault,
//     and no less wrong.
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
			"it is not; its composite is dropped from the shipped table and the browser's copy of "+
			"the rule will leave it decomposed",
			cp)
	}
	if nFalsePositive > 0 {
		t.Errorf("%d code points are excluded by this package and not by Unicode (%d named above)",
			nFalsePositive, len(falsePositives))
	}
	for _, cp := range falseNegatives {
		t.Errorf("U+%04X: IsCompositionExcluded says NOT excluded, Full_Composition_Exclusion says "+
			"it is; the browser's copy of the rule would compose text into a form no NFC string "+
			"may contain", cp)
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

// TestClientNFC_AgreesWithTheModuleOnEverySingleCodePoint is the first of the
// two checks that hold the browser's copy of the normalisation rule equal to the
// server's: over every single code point of Unicode, the Go statement of the
// algorithm task-search.js runs returns what golang.org/x/text/unicode/norm
// returns (SPEC/WEB.md § Roadmap Tasks Page, What keeps the shipped rule equal to
// the server's, check 1).
//
// It is a stronger statement than counting exclusions, because it measures the
// OUTPUT of the rule rather than one input to it. A miscounted exclusion, a
// missing prefix entry, a wrong combining class, a broken blocking test in the
// composition — each of them changes a result here, and none of them need change
// the exclusion count.
//
// THE SUBJECT IS THE BROWSER'S COPY, NOT NFC. NFC returns the module's own
// answer, so comparing it with the module would compare the module with itself.
// The client's algorithm runs over the data the three shipped tables carry, which
// internal/web's guard test holds equal to the shipped tables, so this sweep and
// that one together say that the script answers as the server does.
func TestClientNFC_AgreesWithTheModuleOnEverySingleCodePoint(t *testing.T) {
	swept, faults := 0, 0
	named := make([]rune, 0, maxReportedCodePoints)

	for cp := rune(0); cp <= unicodenorm.MaxCodePoint; cp++ {
		if unicodenorm.IsSurrogate(cp) {
			continue
		}
		swept++
		s := string(cp)
		client, reference := unicodenorm.ClientNFC(s), norm.NFC.String(s)
		if client == reference {
			continue
		}
		faults++
		if len(named) >= maxReportedCodePoints {
			continue
		}
		named = append(named, cp)
		t.Errorf("U+%04X: the browser's copy of the rule normalises to %X, the module to %X",
			cp, []rune(client), []rune(reference))
	}

	if swept != 1112064 {
		t.Errorf("swept %d scalar values, want 1112064; the sweep is not the whole of Unicode",
			swept)
	}
	if faults > 0 {
		t.Errorf("%d of the %d single code points normalise differently in the browser's copy "+
			"of the rule and in the server's (%d named above)", faults, swept, len(named))
	}
}

// interactingSeconds returns, in ascending order, every code point that can
// interact with a code point written before it: a code point whose full
// canonical decomposition begins with a code point that carries a non-zero
// canonical combining class, or with one that is the second element of a
// canonical composition, the Hangul vowel and trailing-consonant jamo included.
//
// Every other code point begins with a starter that nothing composes with, so no
// code point before it can change it or be changed by it, and under UAX #15 a
// pair ending in one normalises to the concatenation of the two single code
// points TestClientNFC_AgreesWithTheModuleOnEverySingleCodePoint covers
// (SPEC/WEB.md § Roadmap Tasks Page, What keeps the shipped rule equal to the
// server's, check 2).
func interactingSeconds() []rune {
	seconds := unicodenorm.Compositions().Seconds
	interacting := make([]rune, 0, 1100)
	for cp := rune(0); cp <= unicodenorm.MaxCodePoint; cp++ {
		if unicodenorm.IsSurrogate(cp) {
			continue
		}
		first := unicodenorm.Decompose(cp)[0]
		if unicodenorm.CombiningClass(first) != 0 || seconds[first] {
			interacting = append(interacting, cp)
		}
	}
	return interacting
}

// pairFault is one two-code-point sequence the two expressions of the rule
// normalise differently.
type pairFault struct {
	lead, trail       rune
	client, reference []byte
}

// pairSweep is what one worker of the pair sweep observed.
type pairSweep struct {
	faults      []pairFault
	firsts      int
	pairs       int
	faultsFound int
}

// pairSweepBlock is how many leading code points a worker claims at a time. It
// is small enough that the processors share the dense and the sparse parts of
// Unicode evenly, and large enough that claiming costs nothing measurable.
const pairSweepBlock = 1 << 12

// TestClientNFC_AgreesWithTheModuleOnEveryInteractingPair is the second of the
// two checks: over every sequence of two code points whose first is any code
// point of Unicode and whose second is one interactingSeconds returns, the Go
// statement of the browser's algorithm returns what the module returns
// (SPEC/WEB.md § Roadmap Tasks Page, What keeps the shipped rule equal to the
// server's, check 2; Acceptance Criterion 155).
//
// THE SWEEP IS WHOLE ON EVERY RUN. The domain is the product of 1,112,064 leading
// code points and about a thousand trailing ones — over a billion pairs — and
// every one of them is normalised by both expressions and compared, under the
// race detector as much as without it. Nothing is sampled and nothing is
// skipped: a sample would leave the pairs it did not draw unproven, and the claim
// is about all of them. The cost is paid by dividing the sweep across every
// processor the run is given, which the specification requires, and by keeping
// both expressions free of allocation inside the loop.
//
// Sequences of three or more code points are not enumerated. They are covered
// because both expressions follow UAX #15's algorithm over data the checks hold
// equal, which is the limit of the proof the specification states.
func TestClientNFC_AgreesWithTheModuleOnEveryInteractingPair(t *testing.T) {
	seconds := interactingSeconds()
	requireInteractingSetShape(t, seconds)

	encodedSeconds := make([][]byte, len(seconds))
	for i, r := range seconds {
		encodedSeconds[i] = utf8.AppendRune(nil, r)
	}

	// The client's tables are derived before the workers start, so the one-time
	// derivation is not raced for by every worker at once.
	_ = unicodenorm.ClientNFC("é") // derives the client's tables; the value is irrelevant

	workers := runtime.GOMAXPROCS(0)
	sweeps := make([]pairSweep, workers)
	var claimed atomic.Int32
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(sweep *pairSweep) {
			defer wg.Done()
			sweepPairs(&claimed, encodedSeconds, sweep)
		}(&sweeps[w])
	}
	wg.Wait()

	firsts, pairs, faults := 0, 0, 0
	named := 0
	for i := range sweeps {
		firsts += sweeps[i].firsts
		pairs += sweeps[i].pairs
		faults += sweeps[i].faultsFound
		for _, f := range sweeps[i].faults {
			if named >= maxReportedCodePoints {
				break
			}
			named++
			t.Errorf("U+%04X U+%04X: the browser's copy of the rule normalises to %X, the module to %X",
				f.lead, f.trail, []rune(string(f.client)), []rune(string(f.reference)))
		}
	}

	if firsts != 1112064 {
		t.Errorf("the sweep led with %d scalar values, want 1112064; it is not the whole of Unicode",
			firsts)
	}
	if want := 1112064 * len(seconds); pairs != want {
		t.Errorf("the sweep compared %d pairs, want %d (1112064 leading code points times %d "+
			"trailing ones)", pairs, want, len(seconds))
	}
	if faults > 0 {
		t.Errorf("%d of the %d pairs normalise differently in the browser's copy of the rule and "+
			"in the server's (%d named above)", faults, pairs, named)
	}
	t.Logf("compared %d pairs (%d leading code points, %d trailing ones) across %d workers",
		pairs, firsts, len(seconds), workers)
}

// sweepPairs claims blocks of leading code points until none is left, and
// compares every pair each block leads. It is one worker, and it owns every
// buffer it writes.
func sweepPairs(claimed *atomic.Int32, encodedSeconds [][]byte, sweep *pairSweep) {
	client := unicodenorm.NewClientNormaliser()
	input := make([]byte, 0, 2*utf8.UTFMax)
	var clientOut, referenceOut []byte
	firsts, pairs := 0, 0
	defer func() { sweep.firsts, sweep.pairs = firsts, pairs }()

	for {
		start := claimed.Add(pairSweepBlock) - pairSweepBlock
		if start > unicodenorm.MaxCodePoint {
			return
		}
		end := min(start+pairSweepBlock-1, unicodenorm.MaxCodePoint)
		for lead := start; lead <= end; lead++ {
			if unicodenorm.IsSurrogate(lead) {
				continue
			}
			firsts++
			input = utf8.AppendRune(input[:0], lead)
			leadLen := len(input)
			for _, trail := range encodedSeconds {
				input = append(input[:leadLen], trail...)
				referenceOut = norm.NFC.Append(referenceOut[:0], input...)
				clientOut = client.AppendNFC(clientOut[:0], input)
				pairs++
				if bytes.Equal(clientOut, referenceOut) {
					continue
				}
				sweep.faultsFound++
				if len(sweep.faults) < maxReportedCodePoints {
					r, _ := utf8.DecodeRune(trail)
					sweep.faults = append(sweep.faults, pairFault{
						lead:      lead,
						trail:     r,
						client:    bytes.Clone(clientOut),
						reference: bytes.Clone(referenceOut),
					})
				}
			}
		}
	}
}

// requireInteractingSetShape stops the pair sweep before it starts when the set
// of trailing code points is not the set the specification describes, because a
// sweep over the wrong set proves nothing about the pairs it left out.
//
// The members named are one of each kind the definition admits: a combining mark;
// a code point with no class of its own whose decomposition begins with a mark;
// a starter that is the second element of a composition, in the Basic
// Multilingual Plane and beyond it; and a Hangul vowel and trailing jamo. The
// non-members are a letter, a Hangul syllable and a leading jamo, each a starter
// nothing composes with. The size is not pinned, because it moves with the
// Unicode version, but it is held to the order of magnitude the specification
// states.
func requireInteractingSetShape(t *testing.T, seconds []rune) {
	t.Helper()

	in := make(map[rune]bool, len(seconds))
	for _, r := range seconds {
		in[r] = true
	}
	for _, member := range []struct {
		r    rune
		kind string
	}{
		{0x0301, "a combining mark"},
		{0x0338, "a combining overlay"},
		{0x0F73, "a code point of class 0 whose decomposition begins with a mark"},
		{0x0CD5, "a starter that is the second element of a composition"},
		{0x11930, "a supplementary starter that is the second element of a composition"},
		{0x1161, "a Hangul vowel jamo"},
		{0x11A8, "a Hangul trailing jamo"},
	} {
		if !in[member.r] {
			t.Errorf("U+%04X, %s, is missing from the trailing code points", member.r, member.kind)
		}
	}
	for _, outsider := range []struct {
		r    rune
		kind string
	}{
		{'a', "a letter"},
		{0xAC00, "a Hangul syllable"},
		{0x1100, "a Hangul leading jamo"},
	} {
		if in[outsider.r] {
			t.Errorf("U+%04X, %s, is among the trailing code points; nothing before it can "+
				"interact with it", outsider.r, outsider.kind)
		}
	}
	if len(seconds) < 500 || len(seconds) > 5000 {
		t.Errorf("the trailing set holds %d code points; the specification describes about a "+
			"thousand", len(seconds))
	}
	if t.Failed() {
		t.FailNow()
	}
}

// TestClientNFC_ComposesTheQuickCheckMaybeCodePoints is the named regression for
// the defect that TestIsCompositionExcluded_IsFullCompositionExclusion
// generalises.
//
// These twelve carry BOTH a canonical decomposition and NFC_QC=Maybe, a
// combination Unicode 15.0.0 had no instance of and Unicode 16.0.0 introduced.
// Under `norm.NFC.QuickSpanString(s) != len(s)` every one of them was read as a
// composition exclusion, its composite never entered the composition table, and
// the rule built on that table returned its decomposition. Each MUST normalise to
// itself: it is a composite Unicode composes, not one Unicode excludes.
//
// The subject is the browser's copy of the rule, because the table is what the
// defect damaged and the browser's copy is what reads it; NFC is asserted beside
// it so that the twelve are held to one answer on both sides. It is stated as
// behaviour rather than as a property lookup alone, so that it keeps testing the
// text a search compares however the predicate underneath is next rewritten.
func TestClientNFC_ComposesTheQuickCheckMaybeCodePoints(t *testing.T) {
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
		if got := unicodenorm.ClientNFC(string(cp)); got != string(cp) {
			t.Errorf("U+%04X: the browser's copy of the rule returns %X, want the code point "+
				"itself; it is a composite Unicode composes", cp, []rune(got))
		}
		if got := unicodenorm.NFC(string(cp)); got != string(cp) {
			t.Errorf("U+%04X: NFC returns %X, want the code point itself; it is a composite "+
				"Unicode composes", cp, []rune(got))
		}
	}
}
