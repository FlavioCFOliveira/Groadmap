// Package unicodenorm implements Unicode Normalization Form C, as UAX #15
// defines it: the canonical composition of the full canonical decomposition.
//
// WHY THIS IS A PACKAGE OF ITS OWN. The rule was written for the roadmap tasks
// board's search and lived inside internal/web, which was the only thing that
// needed it. It now has a second consumer that sits on the other side of an
// import edge — internal/graphkeys, which judges knowledge-graph node keys equal
// under NFC (SPEC/GRAPH.md § Node Key Uniqueness) — and internal/commands
// imports internal/web, so internal/web cannot be imported back from a leaf.
// Duplicating the rule would put two implementations of NFC in one binary, which
// is what internal/graphlock and internal/backoff were each extracted to avoid.
// So the rule moved here, unchanged, and internal/web now delegates to it: the
// board search and the key audit answer one question with one implementation.
//
// THE COMPOSITION IS GROADMAP'S OWN, AND DELIBERATELY NOT THE MODULE'S. Canonical
// decomposition, canonical ordering and the NFC_Quick_Check property come from
// golang.org/x/text/unicode/norm, which is the Go project's own implementation of
// UAX #15 and the only place that data is published for Go. Its COMPOSITION is
// not used and MUST NOT be: at the pinned version it composes a supplementary
// starter as though the starter were its low 16 bits, over 15,041 pairs. Compose
// below says the whole of it, and SPEC/BUILD.md § External Dependencies, Unicode
// Data Rules 3 states the prohibition that follows from it — norm.NFC.String,
// norm.NFC.Bytes and every part of norm.NFKC MUST NOT be called anywhere in
// Groadmap, and the one admitted use of norm.NFC is the property lookup in
// IsCompositionExcluded.
//
// NORMALISATION IS FOR COMPARISON ONLY. Nothing here rewrites what rmp stores or
// what a page renders. Every caller normalises a DERIVED value — a searchable
// text, a term, a key being compared with another key — never a value on its way
// to the database, to a card, or to the graph.
package unicodenorm

import (
	"sync"
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
	MaxCodePoint   = 0x10FFFF
	SurrogateFirst = 0xD800
	SurrogateLast  = 0xDFFF
)

// The Hangul syllables and jamo of UAX #15's algorithmic decomposition and
// composition. The 11,172 syllables are NOT tabulated on either side: a few
// lines of arithmetic give their decomposition and their composition exactly,
// so DECOMP_TABLE holds 2,061 entries rather than 13,233 and COMPOSE_TABLE 941
// rather than 12,113 (SPEC/WEB.md § Roadmap Tasks Page, What keeps the shipped
// rule equal to the server's; Acceptance Criterion 155).
const (
	HangulSBase  = 0xAC00
	HangulLBase  = 0x1100
	HangulVBase  = 0x1161
	HangulTBase  = 0x11A7
	HangulLCount = 19
	HangulVCount = 21
	HangulTCount = 28
	HangulNCount = HangulVCount * HangulTCount // 588 syllables per leading jamo
	HangulSCount = HangulLCount * HangulNCount // 11172 syllables in all
)

// Decompose returns the FULL canonical decomposition of ONE code point,
// canonically ordered: the sequence UAX #15 calls Normalization Form D of that
// code point, which is the first half of the normalisation rule.
//
// It is the ONE statement of that half in the module, and it exists as a named
// function so that the shipped DECOMP_TABLE has a function as its subject: a
// guard comparing the table with a second expression of the rule would prove
// nothing about what the server actually does. internal/web reaches it through
// searchDecompose, which is a delegation to this function and nothing else.
//
// The data is golang.org/x/text/unicode/norm's, which is the Go project's own
// implementation of UAX #15 and the only place canonical decomposition data is
// published for Go — the standard library's unicode package carries case
// mappings, categories and scripts, and no decomposition (SPEC/BUILD.md
// § External Dependencies, Unicode Data Rules 2). That module's DECOMPOSITION is
// used and its COMPOSITION is not; Compose below says why.
//
// A code point with no canonical decomposition decomposes to itself, so the
// result is never empty. Hangul is handled by norm as well as here — the sweep in
// TestTaskSearchScript_ShippedRuleIsTheServerRule holds the client's Hangul
// ARITHMETIC equal to this function for every one of the 11,172 syllables.
func Decompose(r rune) []rune {
	return []rune(norm.NFD.String(string(r)))
}

// CombiningClass returns the canonical combining class of ONE code point:
// the number UAX #15's canonical ordering sorts a run of non-starters by, and the
// number the composition below tests a character's blocking with.
//
// It is the ONE statement of the ordering data in the module, and the shipped
// CCC_TABLE is checked against THIS function, through internal/web's
// searchCombiningClass delegation, over every code point of Unicode. The data is
// again norm's, for the reason Decompose gives.
func CombiningClass(r rune) uint8 {
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	return norm.NFD.Properties(buf[:n]).CCC()
}

// Composition is the composition half of the normalisation rule: the
// primary composites, and the set of code points that can be the SECOND element
// of one.
//
// seconds exists so that a text carrying none of them can be returned untouched
// without composing anything, which is what keeps an ordinary Latin title off the
// composition path entirely. It holds the 63 second elements of the table plus
// the Hangul V and T jamo, which compose arithmetically rather than through it.
type Composition struct {
	Pairs   map[[2]rune]rune
	Seconds map[rune]bool
}

// Compositions is the module's primary-composite data, derived ONCE from the
// character data of golang.org/x/text/unicode/norm and reused thereafter.
//
// It is derived rather than stored precisely so that it MOVES when that data
// moves: a change of Unicode version — from a new module version or from the
// toolchain, since norm selects tables15.0.0.go under !go1.27 and tables17.0.0.go
// under go1.27 — changes this table, the shipped COMPOSE_TABLE stays where it
// was, and TestTaskSearchScript_ShippedRuleIsTheServerRule fails and names what
// moved. A stored Go table would leave that change unobserved (SPEC/BUILD.md
// § External Dependencies, Unicode Data Rules 5 and 6).
//
// It is built lazily rather than in an init function because a command that
// normalises nothing must not pay for the derivation, and because the ASCII fast
// path in NFC means an all-ASCII roadmap never builds it at all.
var Compositions = sync.OnceValue(BuildComposition)

// BuildComposition derives the primary composites from the canonical
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
// in this package, for the reason Compose gives.
//
// The prefix lookup handles a decomposition longer than two: U+1E14 fully
// decomposes to U+0045 U+0304 U+0300, and its pair is (U+0112, U+0300), U+0112
// being the code point whose own full decomposition is that prefix. Only
// composable code points are entered into that lookup, because a canonically
// equivalent EXCLUDED code point shares the prefix — U+212B and U+00C5 both
// decompose to U+0041 U+030A — and pairing U+01FA with the excluded U+212B rather
// than with U+00C5 would build a composite no NFC string can ever reach.
func BuildComposition() *Composition {
	decompositions := make(map[rune][]rune, 2048)
	var buf [utf8.UTFMax]byte
	for cp := rune(0); cp <= MaxCodePoint; cp++ {
		if IsSurrogate(cp) {
			continue
		}
		n := utf8.EncodeRune(buf[:], cp)
		if norm.NFD.Properties(buf[:n]).Decomposition() == nil {
			continue // no canonical decomposition, and Hangul, which is arithmetic
		}
		decompositions[cp] = Decompose(cp)
	}

	composable := make(map[rune][]rune, len(decompositions))
	byDecomposition := make(map[string]rune, len(decompositions))
	for cp, d := range decompositions {
		if len(d) < 2 || CombiningClass(d[0]) != 0 || IsCompositionExcluded(cp) {
			continue
		}
		composable[cp] = d
		byDecomposition[string(d)] = cp
	}

	c := &Composition{
		Pairs:   make(map[[2]rune]rune, len(composable)),
		Seconds: make(map[rune]bool, 128),
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
		c.Pairs[[2]rune{lead, trail}] = cp
		c.Seconds[trail] = true
	}
	for v := rune(HangulVBase); v < HangulVBase+HangulVCount; v++ {
		c.Seconds[v] = true
	}
	for t := rune(HangulTBase + 1); t < HangulTBase+HangulTCount; t++ {
		c.Seconds[t] = true
	}
	return c
}

// IsCompositionExcluded reports whether a code point carrying a canonical
// decomposition is excluded from composition, by reading its NFC_Quick_Check
// property: a code point with a canonical decomposition is NFC_QC=No exactly when
// Full_Composition_Exclusion is true of it, and NFC_QC=Yes otherwise.
//
// QuickSpanString answers that question from the property table alone — it
// reports how much of the string is already in Normalization Form C, which for a
// single code point is all of it or none of it — and composes nothing, so the
// composition defect Compose describes cannot reach it.
func IsCompositionExcluded(r rune) bool {
	s := string(r)
	return norm.NFC.QuickSpanString(s) != len(s)
}

// Compose returns the primary composite of two code points, if there is
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
func Compose(lead, trail rune) (rune, bool) {
	if l, v := lead-HangulLBase, trail-HangulVBase; l >= 0 && l < HangulLCount && v >= 0 && v < HangulVCount {
		return HangulSBase + (l*HangulVCount+v)*HangulTCount, true
	}
	if s, t := lead-HangulSBase, trail-HangulTBase; s >= 0 && s < HangulSCount && s%HangulTCount == 0 &&
		t > 0 && t < HangulTCount {
		return lead + t, true
	}
	composite, ok := Compositions().Pairs[[2]rune{lead, trail}]
	return composite, ok
}

// NFC normalises text to Unicode's Normalization Form C: the canonical
// composition of the full canonical decomposition, as UAX #15 defines it.
//
// It is the ONE statement of the normalisation rule in the module. A task's
// searchable text, a search term, and a knowledge-graph key under audit are all
// normalised through this function, so no two of them can drift apart
// (SPEC/WEB.md § Roadmap Tasks Page, One rule, and only one implementation of
// it; SPEC/GRAPH.md § Node Key Uniqueness).
//
// The decomposition and the canonical ordering come from norm; the composition is
// Compose's, for the reason that function gives.
//
// NORMALISATION IS FOR COMPARISON ONLY. Nothing here rewrites what rmp stores or
// what a page renders: the caller is a derived searchable text, a derived term,
// or a key being compared with another key — never a title on its way to the
// database or to a card, and never a key on its way to the graph.
//
// ASCII is returned untouched, which is not an optimisation with a behaviour of
// its own: no ASCII code point has a canonical decomposition, none carries a
// non-zero combining class, and none is the second element of any primary
// composite, so an ASCII string is already in Normalization Form C and no pair of
// ASCII code points composes. The fast path is what keeps an ordinary roadmap off
// this rule entirely.
func NFC(text string) string {
	if isASCII(text) {
		return text
	}
	return composeText(norm.NFD.String(text))
}

// composeText applies UAX #15's canonical composition algorithm to text
// already in Normalization Form D.
//
// A character C is composed with the last starter L before it unless it is
// BLOCKED from L, which it is when some character between the two has a combining
// class of 0 or a class not below C's own. Because the input is canonically
// ordered, the classes between L and C are non-decreasing, so the class of the
// character immediately before C is the largest of them and is the only one the
// test needs — lastClass below, carrying -1 while nothing separates C from L.
func composeText(decomposed string) string {
	composition := Compositions()
	if !hasComposableTrail(decomposed, composition) {
		return decomposed
	}

	out := make([]rune, 0, len(decomposed))
	starter, lastClass := -1, -1
	for _, r := range decomposed {
		class := int(CombiningClass(r))
		if starter >= 0 && lastClass < class {
			if composite, ok := Compose(out[starter], r); ok {
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
func hasComposableTrail(text string, composition *Composition) bool {
	for _, r := range text {
		if composition.Seconds[r] {
			return true
		}
	}
	return false
}

// isASCII reports whether text is entirely ASCII, which NFC returns
// untouched.
func isASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// IsSurrogate reports whether r is one of the 2048 surrogate code points,
// which are not scalar values.
func IsSurrogate(r rune) bool {
	return r >= SurrogateFirst && r <= SurrogateLast
}
