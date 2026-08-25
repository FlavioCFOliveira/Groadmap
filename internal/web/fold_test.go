package web

import (
	"fmt"
	"regexp"
	"sort"
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
const unicodeScalarValues = (unicodeMaxCodePoint + 1) - (surrogateLast - surrogateFirst + 1)

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

// decompEntry is one entry of the shipped decompositions: a code point and the
// FULL canonical decomposition it stands for. The shipped form is fixed-width, so
// that one binary search finds an entry by index; the unused slots of a shorter
// decomposition carry 0, which no decomposition element can be, and are dropped
// here.
type decompEntry struct {
	codePoint int
	runes     string
}

// classSpan is one span of the shipped combining classes: every code point in
// [start, start+length) carries the same NON-ZERO canonical combining class. A
// code point in no span carries 0, which is the default and is what both the
// canonical ordering and the composition read as "starter".
type classSpan struct {
	start  int
	length int
	class  int
}

// composeEntry is one entry of the shipped primary composites: the code point two
// code points compose to.
type composeEntry struct {
	lead      int
	trail     int
	composite int
}

// maxDecomposition is the longest full canonical decomposition in Unicode, and
// the fixed entry width of the shipped decompositions less the code point itself.
// The Hangul constants the client's arithmetic needs are fold.go's own, because
// they are Unicode's algorithm rather than anything this project decides.
const maxDecomposition = 4

// TestTaskSearchScript_ShippedRuleIsTheServerRule is the gate for Acceptance
// Criteria 119, 122 and 155, and the whole reason the client stopped consulting
// the browser's own tables to prepare a term.
//
// Preparing a term is THREE steps, and the client takes none of them from the
// JavaScript platform. It does not trim with the platform's trimming, which
// removes a DIFFERENT set from the White_Space property the trim rule fixes — it
// keeps U+0085 and removes U+FEFF. It does not fold with the platform's case
// conversion, which is Unicode's Default Case Conversion rather than the simple
// mapping, differing on U+0130 and U+03A3. And it does not normalise with the
// platform's String.prototype.normalize, which would have to agree with the
// server about COMPOSITION — the one part of the server's Unicode module that is
// wrong, and the reason the server composes from the very table it ships. All
// three platform functions read tables of whatever Unicode version the browser
// ships. The client uses the server's own whitespace set, the server's own
// normalisation data and the server's own mapping instead, shipped to it as
// SPACE_TABLE, DECOMP_TABLE, CCC_TABLE, COMPOSE_TABLE and FOLD_TABLE.
//
// Those five shipped tables are second carriers of the server's rule, and
// carriers drift unless something compares them. This is that something, and it
// is ONE check over ALL of them rather than several side by side: a check whose
// only subject was the mapping would leave the whitespace set and the
// normalisation data free to drift, and either of those drifting separates the
// two paths exactly as a drifting mapping would. They are swept together, in one
// loop over Unicode, because they are parts of one rule. It compares:
//
//   - the SHIPPED tables, extracted from the script the binary actually serves,
//     never copies of them kept in the test;
//   - against the SERVER'S OWN foldSearch, isSearchSpace, searchDecompose,
//     searchCombiningClass and composition data, never against strings.ToLower,
//     unicode.IsSpace or norm directly and never against a stored table of
//     expected results — a stored copy can be updated to match a rule that
//     changed, and would then prove nothing, so a server that changes any part of
//     the rule MUST fail here;
//   - over EVERY code point of Unicode, all unicodeScalarValues of them, applied
//     through the same binary searches the script performs, so a table that is
//     mis-ordered, overlapping, truncated or corrupt cannot pass — and the 11,172
//     Hangul syllables no table holds an entry for are swept with the rest, so the
//     arithmetic that stands in for those entries is held to the server's answer
//     rather than excused by their absence;
//   - including when a toolchain or dependency upgrade moves any of them: Go's
//     unicode tables are of the toolchain's Unicode version, and
//     golang.org/x/text/unicode/norm selects tables15.0.0.go under !go1.27 and
//     tables17.0.0.go under go1.27, so either bump changes a server function, this
//     test names what moved, and `go generate ./internal/web/` is then the fix
//     rather than the detection.
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
	shippedDecomps := scriptDecompTable(t, script)
	shippedClasses := scriptClassTable(t, script)
	shippedComposes := scriptComposeTable(t, script)

	// The structure EVERY binary search depends on: ordered, disjoint, non-empty
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
	previousEnd = 0
	for i, span := range shippedClasses {
		for _, fault := range spanFaults(span.start, span.length, previousEnd) {
			structural++
			t.Errorf("CCC_TABLE span %d %+v %s", i, span, fault)
		}
		if span.class < 1 || span.class > 254 {
			structural++
			t.Errorf("CCC_TABLE span %d carries the combining class %d, which is not a "+
				"non-zero class: 0 is the default and needs no entry", i, span.class)
		}
		previousEnd = span.start + span.length
	}
	// A decomposition table is a list of single code points rather than of spans,
	// so its invariant is the same one stated for one code point at a time: each
	// entry starts after the one before it, none is a surrogate, none is Hangul —
	// which is arithmetic on both sides and in no table — and none decomposes to
	// itself, which is the client's default and needs no entry.
	previousEnd = 0
	for i, entry := range shippedDecomps {
		for _, fault := range spanFaults(entry.codePoint, 1, previousEnd) {
			structural++
			t.Errorf("DECOMP_TABLE entry %d (U+%04X) %s", i, entry.codePoint, fault)
		}
		if syllable := entry.codePoint - hangulSBase; syllable >= 0 && syllable < hangulSCount {
			structural++
			t.Errorf("DECOMP_TABLE entry %d holds the Hangul syllable U+%04X, which UAX #15 "+
				"decomposes arithmetically on both sides", i, entry.codePoint)
		}
		if entry.runes == "" || entry.runes == string(rune(entry.codePoint)) {
			structural++
			t.Errorf("DECOMP_TABLE entry %d decomposes U+%04X to %q, which is no decomposition "+
				"at all", i, entry.codePoint, entry.runes)
		}
		if n := len([]rune(entry.runes)); n > maxDecomposition {
			structural++
			t.Errorf("DECOMP_TABLE entry %d decomposes U+%04X to %d code points; the entry is "+
				"%d wide", i, entry.codePoint, n, maxDecomposition)
		}
		previousEnd = entry.codePoint + 1
	}
	// The composites are ordered by the PAIR, which is what the client's binary
	// search compares, and no pair appears twice: two composites for one pair
	// would make the search's answer depend on where the halving landed.
	for i, entry := range shippedComposes {
		if i > 0 {
			before := shippedComposes[i-1]
			if entry.lead < before.lead || (entry.lead == before.lead && entry.trail <= before.trail) {
				structural++
				t.Errorf("COMPOSE_TABLE entry %d %+v does not follow %+v: the entries are out "+
					"of order or the pair is repeated", i, entry, before)
			}
		}
		for _, cp := range []int{entry.lead, entry.trail, entry.composite} {
			if len(spanFaults(cp, 1, 0)) > 0 {
				structural++
				t.Errorf("COMPOSE_TABLE entry %d %+v names U+%04X, which is not a scalar value",
					i, entry, cp)
			}
		}
		if syllable := entry.composite - hangulSBase; syllable >= 0 && syllable < hangulSCount {
			structural++
			t.Errorf("COMPOSE_TABLE entry %d composes the Hangul syllable U+%04X, which UAX #15 "+
				"composes arithmetically on both sides", i, entry.composite)
		}
	}
	if structural > 0 {
		t.Fatalf("a shipped table is structurally invalid (%d faults above): the script's "+
			"binary search would answer for whichever half it landed in, so the sweep below "+
			"would be noise", structural)
	}

	// Which pair reaches each code point, on each side. The Hangul syllables are
	// filled in by asking each side's own compose function, so the arithmetic that
	// stands in for their absent entries is swept with everything else.
	shippedSources := composedFrom(shippedComposes, func(lead, trail int) (int, bool) {
		return applyComposeTable(shippedComposes, lead, trail)
	})
	serverSources := composedFrom(serverComposeEntries(), func(lead, trail int) (int, bool) {
		composite, ok := searchCompose(rune(lead), rune(trail))
		return int(composite), ok
	})

	// EVERY code point, through ALL FIVE shipped tables exactly as the script uses
	// them, against the server's own functions. One sweep, every part: the rule is
	// one rule.
	swept, foldFaults, spaceFaults := 0, 0, 0
	decompFaults, classFaults, composeFaults := 0, 0, 0
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

		decomposed := string(searchDecompose(rune(cp)))
		if shippedDecomposed := applyDecompTable(shippedDecomps, cp); shippedDecomposed != decomposed {
			decompFaults++
			if decompFaults <= maxReportedMismatches {
				t.Errorf("U+%04X: the shipped tables decompose to %v, the server's "+
					"searchDecompose to %v", cp, []rune(shippedDecomposed), []rune(decomposed))
			}
		}

		if shippedClass, serverClass := applyClassTable(shippedClasses, cp),
			int(searchCombiningClass(rune(cp))); shippedClass != serverClass {
			classFaults++
			if classFaults <= maxReportedMismatches {
				t.Errorf("U+%04X: the shipped table orders it at combining class %d, the "+
					"server's searchCombiningClass at %d", cp, shippedClass, serverClass)
			}
		}

		if shippedSources[cp] != serverSources[cp] {
			composeFaults++
			if composeFaults <= maxReportedMismatches {
				t.Errorf("U+%04X: the shipped tables compose it from %v, the server from %v "+
					"([0 0] meaning no pair reaches it)", cp, shippedSources[cp], serverSources[cp])
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
	if decompFaults > maxReportedMismatches {
		t.Errorf("%d code points decompose differently on the two sides; the first %d are above",
			decompFaults, maxReportedMismatches)
	}
	if classFaults > maxReportedMismatches {
		t.Errorf("%d code points order differently on the two sides; the first %d are above",
			classFaults, maxReportedMismatches)
	}
	if composeFaults > maxReportedMismatches {
		t.Errorf("%d code points compose differently on the two sides; the first %d are above",
			composeFaults, maxReportedMismatches)
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

	canonicalDecomps := serverDecompositions()
	if len(shippedDecomps) != len(canonicalDecomps) {
		t.Fatalf("the shipped DECOMP_TABLE has %d entries, want the %d the server's "+
			"searchDecompose gives", len(shippedDecomps), len(canonicalDecomps))
	}
	for i := range canonicalDecomps {
		if shippedDecomps[i] != canonicalDecomps[i] {
			t.Errorf("DECOMP_TABLE entry %d is %+v, want %+v", i, shippedDecomps[i], canonicalDecomps[i])
		}
	}

	canonicalClasses := serverClassSpans()
	if len(shippedClasses) != len(canonicalClasses) {
		t.Fatalf("the shipped CCC_TABLE has %d spans, want the %d the server's combining "+
			"classes run-encode to", len(shippedClasses), len(canonicalClasses))
	}
	for i := range canonicalClasses {
		if shippedClasses[i] != canonicalClasses[i] {
			t.Errorf("CCC_TABLE span %d is %+v, want %+v", i, shippedClasses[i], canonicalClasses[i])
		}
	}

	canonicalComposes := serverComposeEntries()
	if len(shippedComposes) != len(canonicalComposes) {
		t.Fatalf("the shipped COMPOSE_TABLE has %d entries, want the %d primary composites the "+
			"server derives", len(shippedComposes), len(canonicalComposes))
	}
	for i := range canonicalComposes {
		if shippedComposes[i] != canonicalComposes[i] {
			t.Errorf("COMPOSE_TABLE entry %d is %+v, want %+v", i, shippedComposes[i], canonicalComposes[i])
		}
		// And the entry is REACHABLE through the client's own binary search, so a
		// table whose ordering the structural pass admitted still cannot answer
		// differently in the browser than it reads on the page.
		got, ok := applyComposeTable(shippedComposes, shippedComposes[i].lead, shippedComposes[i].trail)
		if !ok || got != shippedComposes[i].composite {
			t.Errorf("COMPOSE_TABLE entry %d %+v is not found by the search the script performs "+
				"(got %d, found=%t)", i, shippedComposes[i], got, ok)
		}
	}

	// THE COMPOSITION IS GROADMAP'S OWN, ON BOTH SIDES, AND THE WITNESSES SAY SO.
	//
	// The sweep above compares the shipped tables with the server's DATA, which a
	// server that went back to composing with golang.org/x/text would leave
	// untouched — the table is derived from that module's DECOMPOSITION, which is
	// right, and the defect is in its runtime composition, which builds its lookup
	// key as uint32(uint16(a))<<16 + uint32(uint16(b)) and so composes a
	// supplementary starter as though it were its low 16 bits. So the whole rule is
	// exercised here as well, on the three witnesses of that defect and on a
	// legitimate supplementary composite, on BOTH sides: a simplification that
	// replaced the composition step with norm.NFC.String would leave the client
	// right and the server wrong, and fails here rather than silently.
	for _, w := range []struct {
		name          string
		lead, trail   rune
		composite     rune
		shouldCompose bool
	}{
		{"the module masks U+1003C to U+003C and composes not-less-than", 0x1003C, 0x0338, 0x226E, false},
		{"the module masks U+10041 to U+0041 and composes A-acute", 0x10041, 0x0301, 0x00C1, false},
		{"the module masks U+1042B to U+042B and composes Cyrillic yeru", 0x1042B, 0x0308, 0x04F8, false},
		{"and a legitimate supplementary composite still composes", 0x11935, 0x11930, 0x11938, true},
	} {
		sequence := string(w.lead) + string(w.trail)
		want := sequence
		if w.shouldCompose {
			want = string(w.composite)
		}
		if got := searchNFC(sequence); got != want {
			t.Errorf("%s: the SERVER normalises U+%04X U+%04X to %v, want %v", w.name,
				w.lead, w.trail, []rune(got), []rune(want))
		}
		if got := clientNFC(shippedDecomps, shippedClasses, shippedComposes, sequence); got != want {
			t.Errorf("%s: the SHIPPED tables normalise U+%04X U+%04X to %v, want %v", w.name,
				w.lead, w.trail, []rune(got), []rune(want))
		}
		composite, composed := searchCompose(w.lead, w.trail)
		if composed != w.shouldCompose || (composed && composite != w.composite) {
			t.Errorf("%s: the server's searchCompose gives U+%04X (composed=%t) for U+%04X U+%04X, "+
				"want composed=%t", w.name, composite, composed, w.lead, w.trail, w.shouldCompose)
		}
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

// shippedRule is every table the binary serves the client, read out of the served
// script once and passed around together, because preparing a term uses all of
// them and no caller has a reason to hold only some.
type shippedRule struct {
	folds    []foldRun
	spaces   []spaceSpan
	decomps  []decompEntry
	classes  []classSpan
	composes []composeEntry
}

// readShippedRule extracts all five tables from the script the binary actually
// serves.
func readShippedRule(t *testing.T) *shippedRule {
	t.Helper()

	script := readEmbeddedAsset(t, "static/task-search.js")
	return &shippedRule{
		folds:    scriptFoldTable(t, script),
		spaces:   scriptSpaceTable(t, script),
		decomps:  scriptDecompTable(t, script),
		classes:  scriptClassTable(t, script),
		composes: scriptComposeTable(t, script),
	}
}

// prepare is the client's WHOLE preparation of a term, over the SHIPPED tables:
// the ends stripped by the shipped whitespace set, the result put into
// Normalization Form C from the shipped normalisation data, every code point
// folded through the shipped mapping, and Form C once more — trimTerm, toNFC, the
// fold loop and toNFC again, in the script's own order.
//
// It mirrors the script at the CODE POINT level, which is the level
// applyFoldTable mirrors foldCodePoint at: strings.TrimFunc decodes from both
// ends one code point at a time, exactly as trimTerm's two walks do. The script's
// UTF-16 surrogate arithmetic has no counterpart here because a Go string is not
// UTF-16; what it exists to guarantee — that the table is asked about the
// character the user typed and never about half of one — is what this models
// directly.
//
// Nothing here reaches for Go's own Unicode tables or for the server's functions.
// A harness that trimmed with strings.TrimSpace, folded with strings.ToLower or
// normalised with norm would be modelling the SERVER twice over and could agree
// with itself while the shipped tables said something else.
func (r *shippedRule) prepare(raw string) string {
	trimmed := strings.TrimFunc(raw, func(c rune) bool { return applySpaceTable(r.spaces, int(c)) })
	normalised := clientNFC(r.decomps, r.classes, r.composes, trimmed)
	var folded strings.Builder
	for _, c := range normalised {
		folded.WriteRune(rune(applyFoldTable(r.folds, int(c))))
	}
	return clientNFC(r.decomps, r.classes, r.composes, folded.String())
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

// ==================== THE NORMALISATION DATA, SHIPPED AND SERVED ====================

// scriptDecompTable extracts `var DECOMP_TABLE = [ cp, r1, r2, r3, r4, ... ];`
// from the served script, exactly as scriptFoldTable extracts the mapping: the
// data is read out of the asset the binary ships, so what is checked is what a
// browser would run.
//
// The trailing 0 slots of a shorter decomposition are dropped here, and a 0
// followed by a non-zero is a structural fault reported by the caller rather than
// silently repacked — a table whose padding had moved would otherwise be read as
// a different decomposition and compared as though it were intended.
func scriptDecompTable(t *testing.T, script string) []decompEntry {
	t.Helper()

	numbers := scriptTableNumbers(t, script, "DECOMP_TABLE", 1+maxDecomposition,
		"code point plus its "+strconv.Itoa(maxDecomposition)+" decomposition slots")
	entries := make([]decompEntry, 0, len(numbers)/(1+maxDecomposition))
	for i := 0; i < len(numbers); i += 1 + maxDecomposition {
		var runes []rune
		for j := 1; j <= maxDecomposition; j++ {
			if numbers[i+j] == 0 {
				break
			}
			runes = append(runes, rune(numbers[i+j]))
		}
		entries = append(entries, decompEntry{codePoint: numbers[i], runes: string(runes)})
	}
	return entries
}

// scriptClassTable extracts `var CCC_TABLE = [ start, length, class, ... ];` from
// the served script.
func scriptClassTable(t *testing.T, script string) []classSpan {
	t.Helper()

	numbers := scriptTableNumbers(t, script, "CCC_TABLE", 3, "start, length, class")
	spans := make([]classSpan, 0, len(numbers)/3)
	for i := 0; i < len(numbers); i += 3 {
		spans = append(spans, classSpan{start: numbers[i], length: numbers[i+1], class: numbers[i+2]})
	}
	return spans
}

// scriptComposeTable extracts `var COMPOSE_TABLE = [ lead, trail, composite, ... ];`
// from the served script.
func scriptComposeTable(t *testing.T, script string) []composeEntry {
	t.Helper()

	numbers := scriptTableNumbers(t, script, "COMPOSE_TABLE", 3, "lead, trail, composite")
	entries := make([]composeEntry, 0, len(numbers)/3)
	for i := 0; i < len(numbers); i += 3 {
		entries = append(entries, composeEntry{
			lead: numbers[i], trail: numbers[i+1], composite: numbers[i+2],
		})
	}
	return entries
}

// scriptTableNumbers reads one generated array literal out of the served script
// and returns its numbers, refusing a literal that is empty or that does not hold
// whole entries of the arity the client indexes it by.
//
// It is the one reader the three normalisation tables share, because the three
// differ only in their arity — which is exactly the property that lets the
// generator lay all five tables out through one emit function.
func scriptTableNumbers(t *testing.T, script, name string, arity int, shape string) []int {
	t.Helper()

	block := scriptBlock(t, script, name, "[", "]")
	text := foldTableNumber.FindAllString(block, -1)
	if len(text) == 0 {
		t.Fatalf("the script's %s is empty; the client would normalise nothing", name)
	}
	if len(text)%arity != 0 {
		t.Fatalf("the script's %s holds %d numbers, which is not whole entries of %s; "+
			"the table is truncated or corrupt", name, len(text), shape)
	}
	numbers := make([]int, len(text))
	for i, raw := range text {
		value, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("the script's %s holds the unparseable number %q: %v", name, raw, err)
		}
		numbers[i] = value
	}
	return numbers
}

// applyDecompTable decomposes one code point through the shipped table, mirroring
// static/task-search.js's decomposeCodePoint step for step: Hangul by the
// arithmetic UAX #15 fixes, everything else by binary search, and a code point no
// entry covers decomposing to itself.
//
// Mirroring the search rather than scanning linearly is deliberate, for the reason
// applyFoldTable mirrors foldCodePoint: a mis-ordered table then produces the
// wrong answer here exactly as it would in the browser.
func applyDecompTable(entries []decompEntry, cp int) string {
	if syllable := cp - hangulSBase; syllable >= 0 && syllable < hangulSCount {
		runes := []rune{
			hangulLBase + rune(syllable/hangulNCount),
			hangulVBase + rune(syllable%hangulNCount/hangulTCount),
		}
		if trailing := syllable % hangulTCount; trailing > 0 {
			runes = append(runes, hangulTBase+rune(trailing))
		}
		return string(runes)
	}
	lo, hi := 0, len(entries)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case cp < entries[mid].codePoint:
			hi = mid - 1
		case cp > entries[mid].codePoint:
			lo = mid + 1
		default:
			return entries[mid].runes
		}
	}
	return string(rune(cp))
}

// applyClassTable answers the combining class the shipped set gives one code
// point, by binary search, mirroring static/task-search.js's combiningClassOf.
func applyClassTable(spans []classSpan, cp int) int {
	lo, hi := 0, len(spans)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case cp < spans[mid].start:
			hi = mid - 1
		case cp >= spans[mid].start+spans[mid].length:
			lo = mid + 1
		default:
			return spans[mid].class
		}
	}
	return 0
}

// applyComposeTable composes two code points through the shipped table, mirroring
// static/task-search.js's composePair: Hangul arithmetically, everything else by
// binary search over the ordered PAIR.
func applyComposeTable(entries []composeEntry, lead, trail int) (int, bool) {
	if l, v := lead-hangulLBase, trail-hangulVBase; l >= 0 && l < hangulLCount &&
		v >= 0 && v < hangulVCount {
		return hangulSBase + (l*hangulVCount+v)*hangulTCount, true
	}
	if syllable, trailing := lead-hangulSBase, trail-hangulTBase; syllable >= 0 &&
		syllable < hangulSCount && syllable%hangulTCount == 0 &&
		trailing > 0 && trailing < hangulTCount {
		return lead + trailing, true
	}
	lo, hi := 0, len(entries)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case lead < entries[mid].lead || (lead == entries[mid].lead && trail < entries[mid].trail):
			hi = mid - 1
		case lead > entries[mid].lead || (lead == entries[mid].lead && trail > entries[mid].trail):
			lo = mid + 1
		default:
			return entries[mid].composite, true
		}
	}
	return 0, false
}

// serverDecompositions tabulates the SERVER's canonical decompositions over the
// whole of Unicode, by asking searchDecompose itself. It is the expectation the
// shipped table's size and shape are held to, and it is computed rather than
// stored precisely so that a change to that function moves it.
func serverDecompositions() []decompEntry {
	var entries []decompEntry
	for cp := 0; cp <= unicodeMaxCodePoint; cp++ {
		if cp >= surrogateFirst && cp <= surrogateLast {
			continue
		}
		if syllable := cp - hangulSBase; syllable >= 0 && syllable < hangulSCount {
			continue // arithmetic on both sides, and in no table
		}
		runes := searchDecompose(rune(cp))
		if len(runes) == 1 && runes[0] == rune(cp) {
			continue
		}
		entries = append(entries, decompEntry{codePoint: cp, runes: string(runes)})
	}
	return entries
}

// serverClassSpans run-encodes the SERVER's combining classes over the whole of
// Unicode, by asking searchCombiningClass itself, for the reason
// serverDecompositions asks searchDecompose.
func serverClassSpans() []classSpan {
	var spans []classSpan
	for cp := 0; cp <= unicodeMaxCodePoint; cp++ {
		if cp >= surrogateFirst && cp <= surrogateLast {
			continue
		}
		class := int(searchCombiningClass(rune(cp)))
		if class == 0 {
			continue
		}
		if n := len(spans); n > 0 && spans[n-1].class == class &&
			spans[n-1].start+spans[n-1].length == cp {
			spans[n-1].length++
			continue
		}
		spans = append(spans, classSpan{start: cp, length: 1, class: class})
	}
	return spans
}

// serverComposeEntries lists the SERVER's primary composites, in the order the
// shipped table carries them. It reads the server's own derived data rather than
// re-deriving it, so a change to how that data is built moves this expectation
// with it instead of leaving a second derivation to agree with the first by
// coincidence.
func serverComposeEntries() []composeEntry {
	pairs := searchCompositions().Pairs
	entries := make([]composeEntry, 0, len(pairs))
	for pair, composite := range pairs {
		entries = append(entries, composeEntry{
			lead: int(pair[0]), trail: int(pair[1]), composite: int(composite),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].lead != entries[j].lead {
			return entries[i].lead < entries[j].lead
		}
		return entries[i].trail < entries[j].trail
	})
	return entries
}

// composedFrom inverts a list of primary composites into "which pair composes to
// this code point", and adds the Hangul syllables by asking the compose function
// it is given — the server's searchCompose on one side, the shipped table's
// arithmetic on the other — so that the 11,172 syllables no table holds an entry
// for are swept exactly as the tabulated composites are.
//
// A code point reachable on one side and not the other, or reachable from two
// different pairs, is what the sweep that uses this reports.
func composedFrom(entries []composeEntry, compose func(lead, trail int) (int, bool)) map[int][2]int {
	sources := make(map[int][2]int, len(entries)+hangulSCount)
	for _, e := range entries {
		sources[e.composite] = [2]int{e.lead, e.trail}
	}
	for l := 0; l < hangulLCount; l++ {
		for v := 0; v < hangulVCount; v++ {
			lead, trail := hangulLBase+l, hangulVBase+v
			syllable, ok := compose(lead, trail)
			if !ok {
				continue
			}
			sources[syllable] = [2]int{lead, trail}
			for tr := 1; tr < hangulTCount; tr++ {
				composite, done := compose(syllable, hangulTBase+tr)
				if !done {
					continue
				}
				sources[composite] = [2]int{syllable, hangulTBase + tr}
			}
		}
	}
	return sources
}

// clientNFC puts text into Normalization Form C over the SHIPPED tables,
// mirroring static/task-search.js's toNFC step for step: every code point
// replaced by its full canonical decomposition, each run of non-starters put into
// canonical order by a stable sort on combining class, and the result recomposed
// against the last starter unless the character is blocked from it.
//
// Not one of the three steps consults Go's own Unicode tables or the server's
// functions: the decompositions, the classes and the composites all come from the
// tables the binary serves, so a table that drifted from the server would change
// what this returns, and a harness that reached for norm or for searchNFC would
// be modelling the server twice over and could agree with itself while the
// browser disagreed.
func clientNFC(decomps []decompEntry, classes []classSpan, composes []composeEntry, text string) string {
	decomposed := make([]rune, 0, len(text))
	for _, r := range text {
		decomposed = append(decomposed, []rune(applyDecompTable(decomps, int(r)))...)
	}
	for k := 1; k < len(decomposed); k++ {
		class := applyClassTable(classes, int(decomposed[k]))
		if class == 0 {
			continue
		}
		for j := k; j > 0; j-- {
			before := applyClassTable(classes, int(decomposed[j-1]))
			if before == 0 || before <= class {
				break
			}
			decomposed[j-1], decomposed[j] = decomposed[j], decomposed[j-1]
		}
	}
	out := make([]rune, 0, len(decomposed))
	starter, lastClass := -1, -1
	for _, current := range decomposed {
		class := applyClassTable(classes, int(current))
		if starter >= 0 && lastClass < class {
			if composite, ok := applyComposeTable(composes, int(out[starter]), int(current)); ok {
				out[starter] = rune(composite)
				continue
			}
		}
		if class == 0 {
			starter, lastClass = len(out), -1
		} else {
			lastClass = class
		}
		out = append(out, current)
	}
	return string(out)
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

	rule := readShippedRule(t)
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
			client := clientShownIDs(unnarrowed, rule, c.term)
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

// clientShownIDs is what the browser would show for a term: the term prepared
// through the SHIPPED tables — stripped by the shipped whitespace set, normalised
// from the shipped Unicode data, folded by the shipped mapping and normalised
// again — and matched as a substring against the corpus the server transformed
// into each card, or against the card's "#<id>" reference. It is the script's own
// trimTerm, toNFC, foldTerm and matchesTerm, over the script's own tables.
//
// No step is taken from Go's standard library here: a harness that trimmed with
// strings.TrimSpace would be modelling the SERVER's trim twice over and could
// agree with itself while the shipped set said something else.
func clientShownIDs(board boardState, rule *shippedRule, raw string) []int {
	term := rule.prepare(raw)

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

	created := 0
	newTask := func(title string) int {
		t.Helper()
		created++
		id, cerr := seedTask(database, &models.Task{
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
