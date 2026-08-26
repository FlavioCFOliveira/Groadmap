package web

import (
	"strings"
	"unicode"

	"github.com/FlavioCFOliveira/Groadmap/internal/unicodenorm"
)

// The whole of Unicode, as every rule in this file is stated over and as the
// tables generated from them sweep it.
//
// Surrogates are not scalar values: they carry no case mapping, no White_Space
// property, no canonical decomposition and no combining class, so no shipped
// entry may cover one and every sweep skips them.
const (
	unicodeMaxCodePoint = unicodenorm.MaxCodePoint
	surrogateFirst      = unicodenorm.SurrogateFirst
	surrogateLast       = unicodenorm.SurrogateLast
)

// The Hangul syllables and jamo of UAX #15's algorithmic decomposition and
// composition, which no shipped table holds a single entry for: a few lines of
// arithmetic give their decomposition and their composition exactly, so
// DECOMP_TABLE holds 2,061 entries rather than 13,233 and COMPOSE_TABLE 941
// rather than 12,113 (SPEC/WEB.md § Roadmap Tasks Page, What keeps the shipped
// rule equal to the server's; Acceptance Criterion 155).
const (
	hangulSBase  = unicodenorm.HangulSBase
	hangulLBase  = unicodenorm.HangulLBase
	hangulVBase  = unicodenorm.HangulVBase
	hangulTBase  = unicodenorm.HangulTBase
	hangulLCount = unicodenorm.HangulLCount
	hangulVCount = unicodenorm.HangulVCount
	hangulTCount = unicodenorm.HangulTCount
	hangulNCount = unicodenorm.HangulNCount
	hangulSCount = unicodenorm.HangulSCount
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

// The normalisation rule itself lives in internal/unicodenorm. It moved there
// when it gained a second consumer on the other side of an import edge — the
// knowledge-graph key audit in internal/graphkeys, which judges two keys the same
// key when their NFC forms are equal (SPEC/GRAPH.md § Node Key Uniqueness). Since
// internal/commands imports this package, a leaf cannot import it back, and a
// second copy of NFC in one binary is what internal/graphlock and internal/backoff
// were each extracted to prevent.
//
// WHAT REMAINS HERE ARE DELEGATIONS, AND THAT IS THE POINT. Each name below is
// still the server's subject for the rule it names, so the guard that holds the
// SHIPPED client tables equal to the server's rule
// (TestTaskSearchScript_ShippedRuleIsTheServerRule) still compares the tables
// against these functions, and still fails if the Unicode data underneath them
// moves. What changed is where the body lives, not which function the board
// search calls or which function the guard measures. Adding a rule of this
// package's own here — rather than delegating — would put a second answer to one
// question back into the binary.

// searchDecompose returns the FULL canonical decomposition of ONE code point,
// canonically ordered: Normalization Form D of that code point.
func searchDecompose(r rune) []rune { return unicodenorm.Decompose(r) }

// searchCombiningClass returns the canonical combining class of ONE code point.
func searchCombiningClass(r rune) uint8 { return unicodenorm.CombiningClass(r) }

// searchCompositions is the primary-composite data, derived once per process.
func searchCompositions() *unicodenorm.Composition { return unicodenorm.Compositions() }

// buildSearchComposition derives the primary composites from the Unicode
// character data. It is the one-time work searchCompositions memoises, exposed
// so the derivation can be benchmarked on its own.
func buildSearchComposition() *unicodenorm.Composition { return unicodenorm.BuildComposition() }

// searchCompose returns the primary composite of two code points, if there is
// one. It is Groadmap's own composition, deliberately not the module's; the
// reason is in unicodenorm.Compose.
func searchCompose(lead, trail rune) (rune, bool) { return unicodenorm.Compose(lead, trail) }

// searchNFC normalises text to Unicode's Normalization Form C.
//
// It is the server's subject for the normalisation rule: a task's searchable text
// and a search term are both normalised through THIS function, so the corpus and
// the term cannot drift apart (SPEC/WEB.md § Roadmap Tasks Page, One rule, and
// only one implementation of it).
func searchNFC(text string) string { return unicodenorm.NFC(text) }

// isSurrogateRune reports whether r is one of the 2048 surrogate code points,
// which are not scalar values.
func isSurrogateRune(r rune) bool { return unicodenorm.IsSurrogate(r) }

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
