package web

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The whole of Unicode, which is what the guard below sweeps.
//
// unicodeScalarValues is not a magic number: it is 0x110000 code points less the
// 2048 surrogates, which are not scalar values, carry no case mapping, and can
// therefore appear in no run. Stating it as a constant and counting the sweep
// against it is what makes "every code point, not a sample" checkable rather than
// merely claimed (SPEC/WEB.md Acceptance Criterion 119).
const (
	unicodeMaxCodePoint = 0x10FFFF
	surrogateFirst      = 0xD800
	surrogateLast       = 0xDFFF
	unicodeScalarValues = (unicodeMaxCodePoint + 1) - (surrogateLast - surrogateFirst + 1)
)

// maxReportedMismatches caps the failure output: a rule that moved wholesale
// would otherwise print a million lines and say less than a dozen would.
const maxReportedMismatches = 12

// foldRun is one span of the shipped mapping: every code point in
// [start, start+length) folds to itself plus delta.
type foldRun struct {
	start  int
	length int
	delta  int
}

// spaceSpan is one span of the shipped whitespace set: every code point in
// [start, start+length) carries Unicode's White_Space property, and one in no
// span does not. There is no third number — membership is the whole question a
// trim asks, so the pair is the entire entry.
type spaceSpan struct {
	start  int
	length int
}

// TestTaskSearchScript_ShippedRuleIsTheServerRule is the gate for Acceptance
// Criteria 119 and 122, and the whole reason the client stopped consulting the
// browser's own tables to normalise a term.
//
// Normalising a term is TWO steps, and the client takes neither from the
// JavaScript platform. It no longer trims with the platform's trimming, which
// removes a DIFFERENT set from the White_Space property the trim rule fixes — it
// keeps U+0085 and removes U+FEFF — and it no longer folds with the platform's
// case conversion, which is Unicode's Default Case Conversion rather than the
// simple mapping, differing on U+0130 and U+03A3. Both platform functions read
// tables of whatever Unicode version the browser ships. The client uses the
// server's own whitespace set and the server's own mapping instead, shipped to it
// as SPACE_TABLE and FOLD_TABLE.
//
// Those two shipped tables are second carriers of the server's rule, and carriers
// drift unless something compares them. This is that something, and it is ONE
// check over BOTH of them rather than two checks side by side: a check whose only
// subject was the mapping would leave the whitespace set free to drift, and a set
// that drifts separates the two paths exactly as a drifting mapping would. The
// two are swept together, in one loop over Unicode, because they are two halves
// of one rule. It compares:
//
//   - the SHIPPED tables, extracted from the script the binary actually serves,
//     never copies of them kept in the test;
//   - against the SERVER'S OWN foldSearch and isSearchSpace, never against
//     strings.ToLower or unicode.IsSpace directly and never against a stored
//     table of expected results — a stored copy can be updated to match a rule
//     that changed, and would then prove nothing, so a server that changes either
//     half of the rule MUST fail here;
//   - over EVERY code point of Unicode, all unicodeScalarValues of them, applied
//     through the same binary searches the script performs, so a table that is
//     mis-ordered, overlapping, truncated or corrupt cannot pass;
//   - including when a toolchain upgrade moves a mapping or changes which code
//     points carry White_Space: Go's unicode tables are of the toolchain's Unicode
//     version, so a bump changes foldSearch or isSearchSpace, this test names what
//     moved, and `go generate ./internal/web/` is then the fix rather than the
//     detection.
func TestTaskSearchScript_ShippedRuleIsTheServerRule(t *testing.T) {
	script := readEmbeddedAsset(t, "static/task-search.js")

	// AC 119, the absence: the served script calls no case conversion of the
	// platform at all. Asserted on the RAW asset, comments included, the way
	// Acceptance Criterion 97 asserts the modal script's markup sinks — a
	// comment that still named one would mean the file had drifted back.
	for _, conversion := range []string{"toLowerCase", "toLocaleLowerCase"} {
		if strings.Contains(script, conversion) {
			t.Errorf("the served script names %s; the client must fold the term with the "+
				"server's shipped mapping and consult no case table of the browser's", conversion)
		}
	}

	// AC 122, the same absence for the other half: the script calls none of the
	// platform's five trimming functions, the legacy aliases included. A bare
	// substring would be useless here — the script's OWN trimTerm carries those
	// four letters — so each name is matched as an ACCESS: after a dot or inside
	// a subscript's quotes, and bounded on the right so that trim does not match
	// trimStart. Reverting the script to the platform's trimming fails this.
	for _, trimming := range []string{"trim", "trimStart", "trimEnd", "trimLeft", "trimRight"} {
		access := regexp.MustCompile(`[.\["']\s*` + trimming + `\b`)
		if access.MatchString(script) {
			t.Errorf("the served script calls the platform's %s; the client must strip the "+
				"term's ends with the server's shipped whitespace set, which keeps U+0085 "+
				"and U+FEFF on the server's side of the answer", trimming)
		}
	}

	// AC 118, the third absence: NOTHING is rewritten after the mapping. A
	// U+03C2 the user typed is already lower case and stays U+03C2, so a term of
	// "οδός" keeps finding a task titled "οδός" — a post-fold rewrite of U+03C2
	// to U+03C3, or any other fixup, would lose it. The table checks below cannot
	// see such a rewrite, because a rewrite lives in the script rather than in
	// the tables, so it is asserted here as an absence: the narrowing script
	// performs no string replacement, no Unicode normalisation, and no
	// locale-aware comparison of any kind. Nothing is stripped after the trim to
	// compensate for U+FEFF either, for the same reason.
	for _, rewrite := range []string{".replace(", ".replaceAll(", ".normalize(", "Intl.", "localeCompare"} {
		if strings.Contains(script, rewrite) {
			t.Errorf("the served script uses %s; nothing may be rewritten after the mapping, "+
				"and no locale may enter the fold", rewrite)
		}
	}

	shippedFolds := scriptFoldTable(t, script)
	shippedSpaces := scriptSpaceTable(t, script)

	// The structure BOTH binary searches depend on: ordered, disjoint, non-empty
	// spans that stay inside Unicode and never cover a surrogate.
	//
	// A fault here is counted on its own rather than read off t.Failed(), which
	// an absence above would already have set: only a malformed TABLE makes the
	// sweep below meaningless, and saying so about an unrelated failure would
	// misdirect whoever reads the output.
	structural, previousEnd := 0, 0
	for i, run := range shippedFolds {
		for _, fault := range spanFaults(run.start, run.length, previousEnd) {
			structural++
			t.Errorf("FOLD_TABLE run %d %+v %s", i, run, fault)
		}
		if run.delta == 0 {
			structural++
			t.Errorf("FOLD_TABLE run %d folds its span to itself, which needs no entry: %+v", i, run)
		}
		previousEnd = run.start + run.length
	}
	previousEnd = 0
	for i, span := range shippedSpaces {
		for _, fault := range spanFaults(span.start, span.length, previousEnd) {
			structural++
			t.Errorf("SPACE_TABLE span %d %+v %s", i, span, fault)
		}
		previousEnd = span.start + span.length
	}
	if structural > 0 {
		t.Fatalf("a shipped table is structurally invalid (%d faults above): the script's "+
			"binary search would answer for whichever half it landed in, so the sweep below "+
			"would be noise", structural)
	}

	// EVERY code point, through BOTH shipped tables exactly as the script uses
	// them, against the server's own two functions. One sweep, both halves: the
	// rule is one rule.
	swept, foldFaults, spaceFaults := 0, 0, 0
	for cp := 0; cp <= unicodeMaxCodePoint; cp++ {
		if cp >= surrogateFirst && cp <= surrogateLast {
			continue
		}
		swept++

		got, want := string(rune(applyFoldTable(shippedFolds, cp))), foldSearch(string(rune(cp)))
		if got != want {
			foldFaults++
			if foldFaults <= maxReportedMismatches {
				t.Errorf("U+%04X: the shipped table folds to %q, the server's foldSearch to %q",
					cp, got, want)
			}
		}

		shipped, server := applySpaceTable(shippedSpaces, cp), isSearchSpace(rune(cp))
		if shipped != server {
			spaceFaults++
			if spaceFaults <= maxReportedMismatches {
				t.Errorf("U+%04X: the shipped set calls it whitespace=%t, the server's "+
					"isSearchSpace says %t", cp, shipped, server)
			}
		}
	}
	if foldFaults > maxReportedMismatches {
		t.Errorf("%d code points fold differently on the two sides; the first %d are above",
			foldFaults, maxReportedMismatches)
	}
	if spaceFaults > maxReportedMismatches {
		t.Errorf("%d code points are whitespace to one side and not the other; the first %d "+
			"are above", spaceFaults, maxReportedMismatches)
	}
	if swept != unicodeScalarValues {
		t.Errorf("the sweep covered %d code points, want the whole of Unicode: %d",
			swept, unicodeScalarValues)
	}

	// The tables' SIZE and shape, derived from the server rather than stored: the
	// run encoding of each rule is canonical, so a table with an entry too few, an
	// entry too many, or a truncated tail fails here even where the sweep above
	// happened to agree.
	canonicalFolds := serverFoldRuns()
	if len(shippedFolds) != len(canonicalFolds) {
		t.Fatalf("the shipped FOLD_TABLE has %d runs, want the %d the server's fold "+
			"run-encodes to", len(shippedFolds), len(canonicalFolds))
	}
	for i := range canonicalFolds {
		if shippedFolds[i] != canonicalFolds[i] {
			t.Errorf("FOLD_TABLE run %d is %+v, want %+v", i, shippedFolds[i], canonicalFolds[i])
		}
	}
	if got, want := foldRunsCoverage(shippedFolds), foldRunsCoverage(canonicalFolds); got != want {
		t.Errorf("the shipped FOLD_TABLE maps %d code points, want %d", got, want)
	}

	canonicalSpaces := serverSpaceSpans()
	if len(shippedSpaces) != len(canonicalSpaces) {
		t.Fatalf("the shipped SPACE_TABLE has %d spans, want the %d the server's whitespace "+
			"set run-encodes to", len(shippedSpaces), len(canonicalSpaces))
	}
	for i := range canonicalSpaces {
		if shippedSpaces[i] != canonicalSpaces[i] {
			t.Errorf("SPACE_TABLE span %d is %+v, want %+v", i, shippedSpaces[i], canonicalSpaces[i])
		}
	}
	if got, want := spaceSpansCoverage(shippedSpaces), spaceSpansCoverage(canonicalSpaces); got != want {
		t.Errorf("the shipped SPACE_TABLE holds %d code points, want %d", got, want)
	}
}

// TestSearchTrim_IsTheWhiteSpaceProperty holds the server's own trim to the set
// SPEC/WEB.md names, and to the behaviour it had before the rule was given a name
// to be checked against.
//
// trimSearchTerm is strings.TrimFunc over isSearchSpace rather than
// strings.TrimSpace, so that the guard above has a server function as its
// subject. That refactor MUST NOT have moved a single code point: the two are
// compared here at BOTH ends, over the whole of Unicode. The two divergent code
// points are then asserted by name, because they are the whole reason the rule is
// stated as a property rather than as "surrounding whitespace" (Acceptance
// Criterion 121).
func TestSearchTrim_IsTheWhiteSpaceProperty(t *testing.T) {
	moved := 0
	for cp := 0; cp <= unicodeMaxCodePoint; cp++ {
		if cp >= surrogateFirst && cp <= surrogateLast {
			continue
		}
		padded := string(rune(cp)) + "settlement" + string(rune(cp))
		got, want := trimSearchTerm(padded), strings.TrimSpace(padded)
		if got == want {
			continue
		}
		moved++
		if moved <= maxReportedMismatches {
			t.Errorf("U+%04X: trimSearchTerm gives %q, strings.TrimSpace %q; naming the rule "+
				"must not have changed it", cp, got, want)
		}
	}
	if moved > maxReportedMismatches {
		t.Errorf("%d code points trim differently; the first %d are above",
			moved, maxReportedMismatches)
	}

	// U+0085 carries the property; U+FEFF does not. The JavaScript platform's own
	// trimming answers the opposite on both, which is the disagreement the shipped
	// set removes.
	if !isSearchSpace('\u0085') {
		t.Errorf("U+0085 (NEXT LINE) carries White_Space and MUST be trimmed from a term's ends")
	}
	if isSearchSpace('\ufeff') {
		t.Errorf("U+FEFF (ZERO WIDTH NO-BREAK SPACE) does not carry White_Space and MUST NOT " +
			"be trimmed, so a term pasted with a byte-order mark matches nothing on BOTH paths")
	}

	// And the rule as a whole, on terms: the ends go, the interior stays, and the
	// order is trim-then-fold.
	for _, c := range []struct{ raw, want string }{
		{"\u0085Passkey\u0085", "passkey"},
		{"\ufeffPasskey", "\ufeffpasskey"},
		{"Passkey\ufeff", "passkey\ufeff"},
		{"Pass\u0085key", "pass\u0085key"},
		{"\u0085", ""},
		{"\ufeff", "\ufeff"},
		{"\u00a0\u2003Passkey\u3000", "passkey"},
		{" \t\r\nPasskey \v\f", "passkey"},
	} {
		if got := foldSearchTerm(c.raw); got != c.want {
			t.Errorf("foldSearchTerm(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// spanFaults returns what is structurally wrong with one span of a shipped table,
// which is the same list for both tables: the invariants the script's two binary
// searches depend on. previousEnd is where the span before it ended, 0 for the
// first.
func spanFaults(start, length, previousEnd int) []string {
	var faults []string
	if length < 1 {
		faults = append(faults, fmt.Sprintf("spans %d code points, want at least 1", length))
	}
	if start < 0 || start+length > unicodeMaxCodePoint+1 {
		faults = append(faults, "leaves Unicode")
	}
	if start < surrogateLast+1 && start+length > surrogateFirst {
		faults = append(faults, "covers surrogates, which are not scalar values")
	}
	if start < previousEnd {
		faults = append(faults, fmt.Sprintf("starts before U+%04X, where the entry before it "+
			"ended: the entries are out of order or overlap", previousEnd))
	}
	return faults
}

// applyFoldTable folds one code point through the shipped table by binary search,
// mirroring static/task-search.js's foldCodePoint step for step. Mirroring it
// rather than scanning linearly is deliberate: a mis-ordered table then produces
// the wrong answer here exactly as it would in the browser.
func applyFoldTable(runs []foldRun, cp int) int {
	lo, hi := 0, len(runs)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case cp < runs[mid].start:
			hi = mid - 1
		case cp >= runs[mid].start+runs[mid].length:
			lo = mid + 1
		default:
			return cp + runs[mid].delta
		}
	}
	return cp
}

// applySpaceTable answers whether the shipped set holds one code point, by
// binary search, mirroring static/task-search.js's isSpaceCodePoint step for
// step, for the same reason applyFoldTable mirrors foldCodePoint.
func applySpaceTable(spans []spaceSpan, cp int) bool {
	lo, hi := 0, len(spans)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case cp < spans[mid].start:
			hi = mid - 1
		case cp >= spans[mid].start+spans[mid].length:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

// clientTrimAndFold is the client's whole normalisation of a term, over the
// SHIPPED tables: the ends stripped by the shipped whitespace set, THEN every
// code point folded through the shipped mapping — trimTerm followed by the fold
// loop of foldTerm, in the script's own order.
//
// It mirrors the script at the CODE POINT level, which is the level applyFoldTable
// mirrors foldCodePoint at: strings.TrimFunc decodes from both ends one code point
// at a time, exactly as trimTerm's two walks do. The script's UTF-16 surrogate
// arithmetic has no counterpart here because a Go string is not UTF-16; what it
// exists to guarantee — that the table is asked about the character the user
// typed and never about half of one — is what this models directly.
func clientTrimAndFold(folds []foldRun, spaces []spaceSpan, raw string) string {
	trimmed := strings.TrimFunc(raw, func(r rune) bool { return applySpaceTable(spaces, int(r)) })
	var folded strings.Builder
	for _, r := range trimmed {
		folded.WriteRune(rune(applyFoldTable(folds, int(r))))
	}
	return folded.String()
}

// serverFoldRuns run-encodes the SERVER's folding rule over the whole of Unicode,
// by asking foldSearch itself. It is the expectation the shipped table's size and
// shape are held to, and it is computed rather than stored precisely so that a
// change to foldSearch moves it.
func serverFoldRuns() []foldRun {
	var runs []foldRun
	for cp := 0; cp <= unicodeMaxCodePoint; cp++ {
		if cp >= surrogateFirst && cp <= surrogateLast {
			continue
		}
		folded := []rune(foldSearch(string(rune(cp))))
		if len(folded) != 1 || int(folded[0]) == cp {
			continue
		}
		delta := int(folded[0]) - cp
		if n := len(runs); n > 0 && runs[n-1].delta == delta && runs[n-1].start+runs[n-1].length == cp {
			runs[n-1].length++
			continue
		}
		runs = append(runs, foldRun{start: cp, length: 1, delta: delta})
	}
	return runs
}

// serverSpaceSpans run-encodes the SERVER's whitespace set over the whole of
// Unicode, by asking isSearchSpace itself. It is the expectation the shipped set's
// size and shape are held to, and it is computed rather than stored precisely so
// that a change to isSearchSpace moves it.
func serverSpaceSpans() []spaceSpan {
	var spans []spaceSpan
	for cp := 0; cp <= unicodeMaxCodePoint; cp++ {
		if cp >= surrogateFirst && cp <= surrogateLast {
			continue
		}
		if !isSearchSpace(rune(cp)) {
			continue
		}
		if n := len(spans); n > 0 && spans[n-1].start+spans[n-1].length == cp {
			spans[n-1].length++
			continue
		}
		spans = append(spans, spaceSpan{start: cp, length: 1})
	}
	return spans
}

// spaceSpansCoverage totals the code points a span list holds.
func spaceSpansCoverage(spans []spaceSpan) int {
	total := 0
	for _, span := range spans {
		total += span.length
	}
	return total
}

// foldRunsCoverage totals the code points a run list maps.
func foldRunsCoverage(runs []foldRun) int {
	total := 0
	for _, run := range runs {
		total += run.length
	}
	return total
}

// foldTableNumber matches one signed integer of the table literal.
var foldTableNumber = regexp.MustCompile(`-?\d+`)

// scriptFoldTable extracts `var FOLD_TABLE = [ start, length, delta, ... ];` from
// the served script, the way scriptArrayTable extracts the badge tables from the
// modal script: the table is read out of the asset the binary ships, so what is
// checked is what a browser would run.
func scriptFoldTable(t *testing.T, script string) []foldRun {
	t.Helper()

	block := scriptBlock(t, script, "FOLD_TABLE", "[", "]")
	numbers := foldTableNumber.FindAllString(block, -1)
	if len(numbers) == 0 {
		t.Fatalf("the script's FOLD_TABLE is empty; the client would fold nothing")
	}
	if len(numbers)%3 != 0 {
		t.Fatalf("the script's FOLD_TABLE holds %d numbers, which is not whole triples of "+
			"start, length, delta; the table is truncated or corrupt", len(numbers))
	}
	runs := make([]foldRun, 0, len(numbers)/3)
	for i := 0; i < len(numbers); i += 3 {
		values := [3]int{}
		for j := range values {
			value, err := strconv.Atoi(numbers[i+j])
			if err != nil {
				t.Fatalf("the script's FOLD_TABLE holds the unparseable number %q: %v",
					numbers[i+j], err)
			}
			values[j] = value
		}
		runs = append(runs, foldRun{start: values[0], length: values[1], delta: values[2]})
	}
	return runs
}

// scriptSpaceTable extracts `var SPACE_TABLE = [ start, length, ... ];` from the
// served script, exactly as scriptFoldTable extracts the mapping: the set is read
// out of the asset the binary ships, so what is checked is what a browser would
// run.
func scriptSpaceTable(t *testing.T, script string) []spaceSpan {
	t.Helper()

	block := scriptBlock(t, script, "SPACE_TABLE", "[", "]")
	numbers := foldTableNumber.FindAllString(block, -1)
	if len(numbers) == 0 {
		t.Fatalf("the script's SPACE_TABLE is empty; the client would trim nothing, and a term " +
			"typed with a leading space would match nothing")
	}
	if len(numbers)%2 != 0 {
		t.Fatalf("the script's SPACE_TABLE holds %d numbers, which is not whole pairs of "+
			"start, length; the set is truncated or corrupt", len(numbers))
	}
	spans := make([]spaceSpan, 0, len(numbers)/2)
	for i := 0; i < len(numbers); i += 2 {
		values := [2]int{}
		for j := range values {
			value, err := strconv.Atoi(numbers[i+j])
			if err != nil {
				t.Fatalf("the script's SPACE_TABLE holds the unparseable number %q: %v",
					numbers[i+j], err)
			}
			values[j] = value
		}
		spans = append(spans, spaceSpan{start: values[0], length: values[1]})
	}
	return spans
}

// ==================== THE FOUR DIVERGENT CODE POINTS ====================

// Titles seeded for the divergence fixture. They carry the code points on which
// the JavaScript platform's own normalisation of a term differs from the two
// rules this specification fixes — two in the fold, two in the trim — plus the
// controls that keep a term which narrows nothing from passing unnoticed:
//
//   - greekUpperTitle carries a WORD-FINAL U+03A3. The folding rule folds it to
//     U+03C3 in every position; Unicode's Default Case Conversion, which a
//     platform's own lower-case function may implement, folds it to U+03C2 here.
//   - greekFinalTitle carries a literal U+03C2 the author typed. It is already
//     lower case and folds to itself, which is why nothing may be rewritten after
//     the mapping: a client that rewrote U+03C2 to U+03C3 in a term would stop
//     finding this task.
//   - dottedCapitalTitle carries U+0130. The folding rule folds it to U+0069
//     alone; the full conversion produces the TWO code points U+0069 U+0307.
//   - desereteTitle carries astral code points, which a term must be folded by
//     CODE POINT to match: walking a term by UTF-16 code unit would fold each
//     surrogate half, no run covers a surrogate, and the term would be left
//     unfolded.
//   - trimProbeTitle is the target of the TRIM's two divergent code points. Its
//     word appears in no other title, so a term of U+0085 + the word must find
//     exactly this card — U+0085 carries White_Space and IS stripped, though the
//     platform's own trimming keeps it — and a term of U+FEFF + the word must
//     find nothing at all, on both paths, because U+FEFF does not carry the
//     property and is NOT stripped, though the platform's own trimming removes
//     it (Acceptance Criterion 121).
const (
	greekUpperTitle    = "ΟΔΟΣ ΠΛΗΡΩΜΩΝ: επανασχεδιασμός δρομολόγησης"
	greekFinalTitle    = "Χαρτογράφηση οδός πληρωμών ανά πάροχο"
	dottedCapitalTitle = "İSTANBUL nightly settlement reconciliation"
	plainLatinTitle    = "Istanbul acquirer report parser"
	deseretTitle       = "𐐀𐐁 Deseret glossary pilot"
	controlTitle       = "Rotate the payment gateway signing keys"
	trimProbeTitle     = "Passkey enrolment fallback for the merchant console"
)

// TestTaskSearch_DivergentCodePointsSelectTheSameCardsOnBothPaths is the gate for
// Acceptance Criteria 118 and 121, and for the identity Acceptance Criterion 104
// requires of EVERY term, the FOUR divergent code points included.
//
// The two paths are compared as the two paths, not as one: the server's board is
// the page it renders for ?q=<term>, and the client's board is computed by
// normalising the term through the TABLES EXTRACTED FROM THE SERVED SCRIPT —
// stripped by the shipped whitespace set, then folded by the shipped mapping —
// and matching it against the corpus the server folded into the cards, which is
// exactly what the browser does.
//
// Both halves of the normalisation are exercised, because both diverge from the
// platform's, and in opposite directions: U+0085 is stripped here and kept by the
// platform's trimming, U+FEFF is kept here and stripped by it.
func TestTaskSearch_DivergentCodePointsSelectTheSameCardsOnBothPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedFoldFixture(t, "multilingual-settlement")
	mux := buildMux()

	script := readEmbeddedAsset(t, "static/task-search.js")
	shippedFolds := scriptFoldTable(t, script)
	shippedSpaces := scriptSpaceTable(t, script)
	unnarrowed, _ := servedBoard(t, mux, f.name, clientControls{})

	for _, c := range []struct {
		name string
		term string
		want []int
	}{
		{
			// The defect itself: typing the word in capitals found nothing,
			// while opening the URL that carries it found the card.
			name: "word-final capital sigma folds to U+03C3",
			term: "ΟΔΟΣ",
			want: []int{f.greekUpper},
		},
		{
			name: "the same word already in lower case",
			term: "οδοσ",
			want: []int{f.greekUpper},
		},
		{
			// The post-fold fixup this rule forbids: rewriting U+03C2 to
			// U+03C3 after folding would lose this card.
			name: "a literal final sigma stays a final sigma",
			term: "οδός",
			want: []int{f.greekFinal},
		},
		{
			name: "a phrase of capitals, spaces and all",
			term: "ΟΔΟΣ ΠΛΗΡΩΜΩΝ",
			want: []int{f.greekUpper},
		},
		{
			// U+0130 folds to U+0069 ALONE. The full conversion's
			// U+0069 U+0307 would match neither title.
			name: "dotted capital I folds to a bare i",
			term: "İSTANBUL",
			want: []int{f.dotted, f.plain},
		},
		{
			name: "and the plain spelling finds the same two",
			term: "istanbul",
			want: []int{f.dotted, f.plain},
		},
		{
			// Folded by code point: a UTF-16 code-unit walk would leave this
			// term unfolded and match nothing.
			name: "an astral pair folds as one character",
			term: "𐐀𐐁",
			want: []int{f.deseret},
		},
		{
			name: "an unrelated ASCII term still narrows normally",
			term: "SIGNING",
			want: []int{f.control},
		},
		{
			// THE TRIM, first direction. U+0085 carries White_Space, so it is
			// stripped and the rest of the term matches. The platform's own
			// trimming keeps it, which is the defect: typing this found
			// nothing while the URL carrying it found the card.
			name: "a leading NEXT LINE is stripped",
			term: "\u0085passkey",
			want: []int{f.trimProbe},
		},
		{
			name: "and a trailing one, and both at once",
			term: "\u0085Passkey\u0085",
			want: []int{f.trimProbe},
		},
		{
			// THE TRIM, the other direction. U+FEFF does not carry the
			// property, so it survives and the term matches nothing — on BOTH
			// paths, which is the property being protected rather than a
			// defect. The platform's own trimming would remove it and show
			// the card.
			name: "a leading byte-order mark is kept, and matches nothing",
			term: "\ufeffpasskey",
			want: []int{},
		},
		{
			name: "a trailing byte-order mark likewise",
			term: "passkey\ufeff",
			want: []int{},
		},
		{
			// The trim is the ENDS only: whitespace inside a term is part of
			// it and is matched literally.
			name: "a NEXT LINE inside the term survives and matches nothing",
			term: "pass\u0085key",
			want: []int{},
		},
		{
			// A term made only of the property is no term at all.
			name: "a term of NEXT LINE alone shows every task",
			term: "\u0085",
			want: f.all(),
		},
		{
			// And one made only of U+FEFF is a term, and finds nothing.
			name: "a term of the byte-order mark alone is a term",
			term: "\ufeff",
			want: []int{},
		},
		{
			// The code points both sides have always agreed on keep working,
			// so the two above are a difference and not the whole rule.
			name: "ordinary whitespace of every kind is stripped",
			term: " \t\r\n\u00a0\u2003PASSKEY\u3000\v\f ",
			want: []int{f.trimProbe},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			server, _ := servedBoard(t, mux, f.name, clientControls{Term: c.term})
			if got := shownBoardIDs(server); !equalIDSets(got, c.want) {
				t.Errorf("the SERVER shows %v for %q, want %v", got, c.term, c.want)
			}
			client := clientShownIDs(unnarrowed, shippedFolds, shippedSpaces, c.term)
			if !equalIDSets(client, c.want) {
				t.Errorf("the CLIENT shows %v for %q, want %v", client, c.term, c.want)
			}
			if got := shownBoardIDs(server); !equalIDSets(got, client) {
				t.Errorf("the two paths disagree on %q: the server shows %v, the browser %v",
					c.term, got, client)
			}
		})
	}
}

// clientShownIDs is what the browser would show for a term: the term normalised
// through the SHIPPED tables — stripped by the shipped whitespace set, then
// folded by the shipped mapping — and matched as a substring against the corpus
// the server folded into each card, or against the card's "#<id>" reference. It
// is the script's own trimTerm, foldTerm and matchesTerm, over the script's own
// tables.
//
// Neither step is taken from Go's standard library here: a harness that trimmed
// with strings.TrimSpace would be modelling the SERVER's trim twice over and
// could agree with itself while the shipped set said something else.
func clientShownIDs(board boardState, folds []foldRun, spaces []spaceSpan, raw string) []int {
	term := clientTrimAndFold(folds, spaces, raw)

	shown := []int{}
	for _, column := range board.columns {
		for _, card := range column.cards {
			if term == "" ||
				strings.Contains(card.search, term) ||
				strings.Contains("#"+strconv.Itoa(card.id), term) {
				shown = append(shown, card.id)
			}
		}
	}
	return shown
}

// shownBoardIDs lists every id a served board is showing, across its columns.
func shownBoardIDs(board boardState) []int {
	shown := []int{}
	for _, column := range board.columns {
		for _, card := range column.cards {
			if card.shown {
				shown = append(shown, card.id)
			}
		}
	}
	return shown
}

// equalIDSets compares two id lists as sets of the same size, so a column ordering
// difference is not mistaken for a matching difference.
func equalIDSets(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[int]int, len(got))
	for _, id := range got {
		seen[id]++
	}
	for _, id := range want {
		seen[id]--
		if seen[id] < 0 {
			return false
		}
	}
	return true
}

// foldFixture names the tasks seedFoldFixture created, so the expectations above
// bind to ids rather than to insertion order.
type foldFixture struct {
	name       string
	greekUpper int
	greekFinal int
	dotted     int
	plain      int
	deseret    int
	control    int
	trimProbe  int
}

// all lists every id the fixture seeded, which is what a term that is no term at
// all must show.
func (f foldFixture) all() []int {
	return []int{f.greekUpper, f.greekFinal, f.dotted, f.plain, f.deseret, f.control, f.trimProbe}
}

// seedFoldFixture builds a roadmap whose titles carry the code points on which a
// platform's own case conversion differs from the folding rule, plus an ordinary
// ASCII control so a term that narrows nothing at all cannot pass unnoticed.
func seedFoldFixture(t *testing.T, name string) foldFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	created := 0
	newTask := func(title string) int {
		t.Helper()
		created++
		id, cerr := database.CreateTask(ctx, &models.Task{
			Title:                  title,
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			Priority:               5,
			Severity:               3,
			FunctionalRequirements: "Operators must find this task by typing its title into the board search.",
			TechnicalRequirements:  "The title is folded once by the server into the card's search corpus.",
			AcceptanceCriteria:     "The term selects the same cards typed as it does carried in the URL.",
			CreatedAt:              fmt.Sprintf("2026-03-%02dT09:00:00Z", created),
		})
		if cerr != nil {
			t.Fatalf("creating task %q: %v", title, cerr)
		}
		return id
	}

	return foldFixture{
		name:       name,
		greekUpper: newTask(greekUpperTitle),
		greekFinal: newTask(greekFinalTitle),
		dotted:     newTask(dottedCapitalTitle),
		plain:      newTask(plainLatinTitle),
		deseret:    newTask(deseretTitle),
		control:    newTask(controlTitle),
		trimProbe:  newTask(trimProbeTitle),
	}
}
