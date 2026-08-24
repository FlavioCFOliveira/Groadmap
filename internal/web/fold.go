package web

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// The whole of Unicode, as every rule in this file is stated over and as the
// tables generated from them sweep it.
//
// Surrogates are not scalar values: they carry no case mapping, no White_Space
// property, no canonical decomposition and no combining class, so no shipped
// entry may cover one and every sweep skips them.
const (
	unicodeMaxCodePoint = 0x10FFFF
	surrogateFirst      = 0xD800
	surrogateLast       = 0xDFFF
)

//go:generate go run searchtables_gen.go

// foldSearch applies the board search's folding rule to text: Unicode's SIMPLE
// lowercase mapping, the single replacement code point the Unicode Character
// Database gives a code point, applied to each code point on its own, with a code
// point that has no such mapping folding to itself.
//
// It is the ONE implementation of that rule on the server. A task's searchable
// text (taskView.SearchText) and a search term (foldSearchTerm) both fold through
// this function rather than through two implementations of one description, so
// the corpus and the term cannot drift apart (SPEC/WEB.md § Roadmap Tasks Page,
// One rule, and only one implementation of it; Acceptance Criteria 118 and 119).
//
// strings.ToLower is that rule: it is strings.Map over unicode.ToLower, which is
// the simple mapping, so the fold is unconditional (what a code point folds to
// never depends on its neighbours), one code point in and one code point out, and
// locale-independent. It is deliberately NOT Unicode's Default Case Conversion,
// which a platform's ordinary lower-case function may implement instead and which
// differs on exactly two code points: U+0130 folds here to U+0069 alone, never to
// U+0069 U+0307, and U+03A3 folds to U+03C3 in every position, word-final
// included, never to the final form U+03C2. Nothing is rewritten afterwards — a
// U+03C2 the user typed is already lower case and stays U+03C2, so a term of
// "οδός" keeps finding a task titled "οδός".
//
// A string holding bytes that are not valid UTF-8 is not a sequence of code
// points at all; strings.ToLower replaces each such byte with U+FFFD, which is
// the behaviour SPEC/WEB.md § Roadmap Tasks Page requires of a malformed term:
// folded like any other term, neither an error nor an absent one.
//
// The client folds a term through the SAME mapping, shipped to it as the
// run-encoded FOLD_TABLE in static/task-search.js, which searchtables_gen.go
// generates from this rule and which TestTaskSearchScript_ShippedRuleIsTheServerRule
// checks against this function over the whole of Unicode.
func foldSearch(text string) string {
	return strings.ToLower(text)
}

// isSearchSpace reports whether r is the whitespace the board search's trim rule
// removes from the ends of a term: a code point carrying Unicode's White_Space
// property, the property Unicode's own character database publishes under that
// name (SPEC/WEB.md § Roadmap Tasks Page, The trim rule; Acceptance Criteria 121
// and 122).
//
// It is the ONE statement of that set on the server, and it exists as a named
// function for the same reason foldSearch does: the set has to have a single
// subject that the term's trim uses and that the shipped set is checked against.
// A guard comparing the shipped set with unicode.IsSpace directly would pass
// while the server trimmed by some other set entirely, which is the vacuity this
// function removes — trimSearchTerm below trims by THIS function, so a change
// here is a change to the server's rule and fails
// TestTaskSearchScript_ShippedRuleIsTheServerRule.
//
// unicode.IsSpace is that property exactly: Go's unicode tables define IsSpace as
// White_Space, of whatever Unicode version the toolchain ships. The set is
// deliberately NOT the one the JavaScript platform's own trimming removes, which
// is a different set: it keeps U+0085 (NEXT LINE), which carries the property, and
// removes U+FEFF (ZERO WIDTH NO-BREAK SPACE), which does not. Those two are the
// whole of the difference at any one Unicode version, and the client trims by the
// set THIS function defines rather than by the platform's, so the two paths return
// one term for either of them.
func isSearchSpace(r rune) bool {
	return unicode.IsSpace(r)
}

// trimSearchTerm removes every leading and trailing code point carrying the
// White_Space property from a raw search term, stopping at the first code point
// that does not carry it.
//
// Whitespace INSIDE the term therefore survives, is part of the term, and is
// matched literally; a term made only of such code points becomes the empty
// string, which is no term at all and matches every task (SPEC/WEB.md § Roadmap
// Tasks Page, The trim rule; Matching rule).
//
// It is strings.TrimFunc over isSearchSpace rather than strings.TrimSpace so that
// the set the server trims by is the one function the guard compares the shipped
// set against. The two are the same trimming — strings.TrimSpace is documented as
// the TrimFunc(s, unicode.IsSpace) special case, and TestSearchTrim_IsTheWhiteSpaceProperty
// holds them equal over the whole of Unicode — so nothing about the server's
// behaviour changes; what changes is that the rule now has a name to be checked
// against.
//
// The task's searchable text is NOT trimmed: the trim is the term's alone, and a
// task's own leading or trailing whitespace is part of its text.
func trimSearchTerm(raw string) string {
	return strings.TrimFunc(raw, isSearchSpace)
}

// foldSearchTerm prepares a raw search term for the matching rule: trimmed by
// trimSearchTerm, THEN normalised and folded by searchableText.
//
// The order is fixed, and the client performs the same steps in the same order.
// The TRIM's place in it is not observable under the Unicode version in force —
// no code point carrying White_Space folds to anything but itself, none outside
// the property folds into it, and normalisation neither gives the property to a
// code point that lacked it nor takes it from one that had it, the two code
// points it does rewrite (U+2000 to U+2002 and U+2001 to U+2003) carrying it
// before and after — so the trim commutes with both later steps. Fixing it is
// what keeps the contract from resting on that coincidence.
//
// The place of the NORMALISATION relative to the fold is a different matter: it
// IS observable, and searchableText says why normalising first is the only order
// that closes the defect this rule exists for (SPEC/WEB.md § Roadmap Tasks Page,
// Trim first, then normalise, then fold).
func foldSearchTerm(raw string) string {
	return searchableText(trimSearchTerm(raw))
}

// ==================== THE NORMALISATION RULE ====================

// The Hangul syllables and jamo of UAX #15's algorithmic decomposition and
// composition. The 11,172 syllables are NOT tabulated on either side: a few
// lines of arithmetic give their decomposition and their composition exactly,
// so DECOMP_TABLE holds 2,061 entries rather than 13,233 and COMPOSE_TABLE 941
// rather than 12,113 (SPEC/WEB.md § Roadmap Tasks Page, What keeps the shipped
// rule equal to the server's; Acceptance Criterion 155).
const (
	hangulSBase  = 0xAC00
	hangulLBase  = 0x1100
	hangulVBase  = 0x1161
	hangulTBase  = 0x11A7
	hangulLCount = 19
	hangulVCount = 21
	hangulTCount = 28
	hangulNCount = hangulVCount * hangulTCount // 588 syllables per leading jamo
	hangulSCount = hangulLCount * hangulNCount // 11172 syllables in all
)

// searchDecompose returns the FULL canonical decomposition of ONE code point,
// canonically ordered: the sequence UAX #15 calls Normalization Form D of that
// code point, which is the first half of the normalisation rule.
//
// It is the ONE statement of that half on the server, and it exists as a named
// function for the reason isSearchSpace exists as one: the shipped DECOMP_TABLE
// has to have a server function as its subject, or the guard would compare the
// table with a second expression of the rule and prove nothing about what the
// server does.
//
// The data is golang.org/x/text/unicode/norm's, which is the Go project's own
// implementation of UAX #15 and the only place canonical decomposition data is
// published for Go — the standard library's unicode package carries case
// mappings, categories and scripts, and no decomposition (SPEC/BUILD.md
// § External Dependencies, Unicode Data Rules 2). That module's DECOMPOSITION is
// used and its COMPOSITION is not; searchCompose below says why.
//
// A code point with no canonical decomposition decomposes to itself, so the
// result is never empty. Hangul is handled by norm as well as here — the sweep in
// TestTaskSearchScript_ShippedRuleIsTheServerRule holds the client's Hangul
// ARITHMETIC equal to this function for every one of the 11,172 syllables.
func searchDecompose(r rune) []rune {
	return []rune(norm.NFD.String(string(r)))
}

// searchCombiningClass returns the canonical combining class of ONE code point:
// the number UAX #15's canonical ordering sorts a run of non-starters by, and the
// number the composition below tests a character's blocking with.
//
// It is the ONE statement of the ordering data on the server, and the shipped
// CCC_TABLE is checked against THIS function over every code point of Unicode.
// The data is again norm's, for the reason searchDecompose gives.
func searchCombiningClass(r rune) uint8 {
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	return norm.NFD.Properties(buf[:n]).CCC()
}

// searchComposition is the composition half of the normalisation rule: the
// primary composites, and the set of code points that can be the SECOND element
// of one.
//
// seconds exists so that a text carrying none of them can be returned untouched
// without composing anything, which is what keeps an ordinary Latin title off the
// composition path entirely. It holds the 63 second elements of the table plus
// the Hangul V and T jamo, which compose arithmetically rather than through it.
type searchComposition struct {
	pairs   map[[2]rune]rune
	seconds map[rune]bool
}

// searchCompositions is the server's primary-composite data, derived ONCE from
// the character data of golang.org/x/text/unicode/norm and reused thereafter.
//
// It is derived rather than stored precisely so that it MOVES when that data
// moves: a change of Unicode version — from a new module version or from the
// toolchain, since norm selects tables15.0.0.go under !go1.27 and tables17.0.0.go
// under go1.27 — changes this table, the shipped COMPOSE_TABLE stays where it
// was, and TestTaskSearchScript_ShippedRuleIsTheServerRule fails and names what
// moved. A stored Go table would leave that change unobserved (SPEC/BUILD.md
// § External Dependencies, Unicode Data Rules 5 and 6).
//
// It is built lazily rather than in an init function because `rmp task list` must
// not pay for a rule only `rmp web` applies, and because the ASCII fast path in
// searchNFC means an all-ASCII roadmap never builds it at all.
var searchCompositions = sync.OnceValue(buildSearchComposition)

// buildSearchComposition derives the primary composites from the canonical
// decompositions, which is what a primary composite IS: a code point whose
// canonical decomposition is two characters, the first of them a starter, that
// Unicode does not exclude from composition.
//
// The exclusion is read from the NFC_Quick_Check character property, which is the
// published form of Full_Composition_Exclusion and is DATA rather than the
// composing transform: composition exclusions cannot be derived from the
// decompositions themselves, because a script exclusion such as U+0958 and a
// post-composition-version exclusion such as U+2ADC decompose exactly as an
// ordinary composite does. norm.NFC.String, norm.NFC.Bytes, norm.NFKC and every
// other composing entry point of that module are NOT called here or anywhere else
// in this package, for the reason searchCompose gives.
//
// The prefix lookup handles a decomposition longer than two: U+1E14 fully
// decomposes to U+0045 U+0304 U+0300, and its pair is (U+0112, U+0300), U+0112
// being the code point whose own full decomposition is that prefix. Only
// composable code points are entered into that lookup, because a canonically
// equivalent EXCLUDED code point shares the prefix — U+212B and U+00C5 both
// decompose to U+0041 U+030A — and pairing U+01FA with the excluded U+212B rather
// than with U+00C5 would build a composite no NFC string can ever reach.
func buildSearchComposition() *searchComposition {
	decompositions := make(map[rune][]rune, 2048)
	var buf [utf8.UTFMax]byte
	for cp := rune(0); cp <= unicodeMaxCodePoint; cp++ {
		if isSurrogateRune(cp) {
			continue
		}
		n := utf8.EncodeRune(buf[:], cp)
		if norm.NFD.Properties(buf[:n]).Decomposition() == nil {
			continue // no canonical decomposition, and Hangul, which is arithmetic
		}
		decompositions[cp] = searchDecompose(cp)
	}

	composable := make(map[rune][]rune, len(decompositions))
	byDecomposition := make(map[string]rune, len(decompositions))
	for cp, d := range decompositions {
		if len(d) < 2 || searchCombiningClass(d[0]) != 0 || isCompositionExcluded(cp) {
			continue
		}
		composable[cp] = d
		byDecomposition[string(d)] = cp
	}

	c := &searchComposition{
		pairs:   make(map[[2]rune]rune, len(composable)),
		seconds: make(map[rune]bool, 128),
	}
	for cp, d := range composable {
		lead := d[0]
		if len(d) > 2 {
			prefix, ok := byDecomposition[string(d[:len(d)-1])]
			if !ok {
				continue // no composable prefix: no pair can reach this code point
			}
			lead = prefix
		}
		trail := d[len(d)-1]
		c.pairs[[2]rune{lead, trail}] = cp
		c.seconds[trail] = true
	}
	for v := rune(hangulVBase); v < hangulVBase+hangulVCount; v++ {
		c.seconds[v] = true
	}
	for t := rune(hangulTBase + 1); t < hangulTBase+hangulTCount; t++ {
		c.seconds[t] = true
	}
	return c
}

// isCompositionExcluded reports whether a code point carrying a canonical
// decomposition is excluded from composition, by reading its NFC_Quick_Check
// property: a code point with a canonical decomposition is NFC_QC=No exactly when
// Full_Composition_Exclusion is true of it, and NFC_QC=Yes otherwise.
//
// QuickSpanString answers that question from the property table alone — it
// reports how much of the string is already in Normalization Form C, which for a
// single code point is all of it or none of it — and composes nothing, so the
// composition defect searchCompose describes cannot reach it.
func isCompositionExcluded(r rune) bool {
	s := string(r)
	return norm.NFC.QuickSpanString(s) != len(s)
}

// searchCompose returns the primary composite of two code points, if there is
// one: the second half of the normalisation rule.
//
// THE COMPOSITION IS GROADMAP'S OWN, AND DELIBERATELY NOT THE MODULE'S. At the
// pinned version golang.org/x/text/unicode/norm composes a SUPPLEMENTARY starter
// as though the starter were its low 16 bits — its combine() builds the lookup
// key as uint32(uint16(a))<<16 + uint32(uint16(b)) — so norm.NFC.String turns
// U+1003C followed by U+0338 into U+226E (U+1003C masked to 16 bits is U+003C,
// and less-than plus U+0338 is not-less-than), U+10041 followed by U+0301 into
// U+00C1, and U+1042B followed by U+0308 into U+04F8. Measured over every
// supplementary starter against each of the 63 code points a composition can
// consume, the defect spans 15,041 pairs over 6,021 distinct leading code points.
// The platform's own normalisation leaves all three witnesses unchanged, and so
// does this function.
//
// Composing from the derived table is not a private dialect of NFC: it is NFC
// where that module is right and NFC where that module is wrong. The two agree on
// all 1,112,064 single code points, and the table still composes the 13
// legitimate supplementary composites, U+11935 followed by U+11930 giving
// U+11938 among them (SPEC/BUILD.md § External Dependencies, Unicode Data
// Rules 3; SPEC/WEB.md § Roadmap Tasks Page, The normalisation rule).
//
// Hangul is arithmetic rather than tabulated, on this side exactly as on the
// client's.
func searchCompose(lead, trail rune) (rune, bool) {
	if l, v := lead-hangulLBase, trail-hangulVBase; l >= 0 && l < hangulLCount && v >= 0 && v < hangulVCount {
		return hangulSBase + (l*hangulVCount+v)*hangulTCount, true
	}
	if s, t := lead-hangulSBase, trail-hangulTBase; s >= 0 && s < hangulSCount && s%hangulTCount == 0 &&
		t > 0 && t < hangulTCount {
		return lead + t, true
	}
	composite, ok := searchCompositions().pairs[[2]rune{lead, trail}]
	return composite, ok
}

// searchNFC normalises text to Unicode's Normalization Form C: the canonical
// composition of the full canonical decomposition, as UAX #15 defines it.
//
// It is the ONE statement of the normalisation rule on the server. A task's
// searchable text and a search term are both normalised through this function,
// so the corpus and the term cannot drift apart on this side (SPEC/WEB.md
// § Roadmap Tasks Page, One rule, and only one implementation of it).
//
// The decomposition and the canonical ordering come from norm; the composition is
// searchCompose's, for the reason that function gives.
//
// NORMALISATION IS FOR COMPARISON ONLY. Nothing here rewrites what rmp stores or
// what a page renders: the caller is the derived searchable text and the derived
// term, never a title on its way to the database or to a card.
//
// ASCII is returned untouched, which is not an optimisation with a behaviour of
// its own: no ASCII code point has a canonical decomposition, none carries a
// non-zero combining class, and none is the second element of any primary
// composite, so an ASCII string is already in Normalization Form C and no pair of
// ASCII code points composes. The fast path is what keeps an ordinary roadmap off
// this rule entirely.
func searchNFC(text string) string {
	if isSearchASCII(text) {
		return text
	}
	return composeSearchText(norm.NFD.String(text))
}

// composeSearchText applies UAX #15's canonical composition algorithm to text
// already in Normalization Form D.
//
// A character C is composed with the last starter L before it unless it is
// BLOCKED from L, which it is when some character between the two has a combining
// class of 0 or a class not below C's own. Because the input is canonically
// ordered, the classes between L and C are non-decreasing, so the class of the
// character immediately before C is the largest of them and is the only one the
// test needs — lastClass below, carrying -1 while nothing separates C from L.
func composeSearchText(decomposed string) string {
	composition := searchCompositions()
	if !hasComposableTrail(decomposed, composition) {
		return decomposed
	}

	out := make([]rune, 0, len(decomposed))
	starter, lastClass := -1, -1
	for _, r := range decomposed {
		class := int(searchCombiningClass(r))
		if starter >= 0 && lastClass < class {
			if composite, ok := searchCompose(out[starter], r); ok {
				out[starter] = composite
				continue
			}
		}
		if class == 0 {
			starter, lastClass = len(out), -1
		} else {
			lastClass = class
		}
		out = append(out, r)
	}
	return string(out)
}

// hasComposableTrail reports whether text carries any code point that could be
// the second element of a composition. A text carrying none composes to itself,
// so it is returned as it arrived and allocates nothing.
func hasComposableTrail(text string, composition *searchComposition) bool {
	for _, r := range text {
		if composition.seconds[r] {
			return true
		}
	}
	return false
}

// isSearchASCII reports whether text is entirely ASCII, which searchNFC returns
// untouched.
func isSearchASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// isSurrogateRune reports whether r is one of the 2048 surrogate code points,
// which are not scalar values.
func isSurrogateRune(r rune) bool {
	return r >= surrogateFirst && r <= surrogateLast
}

// searchableText is the WHOLE preparation of a text for the board search, and the
// one place its four steps are stated in their order: normalise, fold, normalise
// again.
//
// A task's searchable text (taskView.SearchText) and a search term
// (foldSearchTerm, after its trim) both go through THIS function, so the corpus
// and the term are one rule rather than two implementations of one description.
//
// NORMALISE BEFORE FOLDING. The order is observable, and normalising first is the
// only order that closes the defect this rule exists for: the canonical
// decomposition of U+0130 is U+0049 U+0307, so a title written with U+0130 and a
// title written as U+0049 followed by U+0307 are the same text by Unicode's own
// definition, and normalising first gives both one searchable text. Folding first
// would give U+0069 for one and U+0069 U+0307 for the other.
//
// AND NORMALISE AGAIN AFTERWARDS. The second pass is not decoration: the fold can
// produce a sequence that composes where the unfolded one did not. Unicode has no
// precomposed capital for H with a line below, so the first pass leaves U+0048
// U+0331 as two code points; the fold lowers the H, and U+0068 U+0331 DOES have a
// precomposed form, U+1E96. Without the second pass a task titled "H̱ydro" would
// carry a two-code-point searchable text while a term typed as the single
// character U+1E96 stayed one, and the term would not occur in the text it plainly
// spells. U+1E97, U+1E98, U+1E99 and U+01F0 behave the same way. A third pass
// would change nothing (SPEC/WEB.md § Roadmap Tasks Page, The normalisation rule;
// Acceptance Criterion 152).
func searchableText(text string) string {
	return searchNFC(foldSearch(searchNFC(text)))
}
