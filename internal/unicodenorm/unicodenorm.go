// Package unicodenorm holds Groadmap's Unicode Normalization Form C, as UAX #15
// defines it: the canonical composition of the full canonical decomposition. It
// holds the server's normalisation, and it derives the data and states the
// algorithm the roadmap tasks board ships to the browser so that the browser
// normalises a term as the server does.
//
// WHY THIS IS A PACKAGE OF ITS OWN. The rule was written for the roadmap tasks
// board's search and lived inside internal/web, which was the only thing that
// needed it. It now has a second consumer that sits on the other side of an
// import edge — internal/graphkeys, which judges knowledge-graph node keys equal
// under NFC (SPEC/GRAPH.md § Node Key Uniqueness) — and internal/commands
// imports internal/web, so internal/web cannot be imported back from a leaf.
// Duplicating the rule would put two server normalisations in one binary, which
// is what internal/graphlock and internal/backoff were each extracted to avoid.
// So the rule moved here and internal/web delegates to it: the board search and
// the key audit answer one question with one implementation.
//
// THE SERVER NORMALISES WITH THE MODULE. NFC returns the Normalization Form C of
// golang.org/x/text/unicode/norm, the Go project's own implementation of UAX #15,
// which supplies the decomposition, the canonical ordering and the composition
// alike (SPEC/BUILD.md § External Dependencies, Unicode Data Rules 3).
//
// THE BROWSER CANNOT CALL THE MODULE, SO THIS PACKAGE ALSO STATES WHAT IT SHIPS.
// Decompose, CombiningClass and Compositions derive, from the module's character
// data, the data of the three tables internal/web ships in task-search.js —
// DECOMP_TABLE, CCC_TABLE and COMPOSE_TABLE — and internal/web's guard test holds
// each shipped table equal to that derivation over every code point. clientNFC is
// the Go statement of the algorithm the script runs over those tables, and the
// tests of this package hold it equal to the module's Normalization Form C over
// every single code point and over every two-code-point sequence in which the
// second can interact with the first (SPEC/WEB.md § Roadmap Tasks Page, What
// keeps the shipped rule equal to the server's).
//
// clientNFC IS NOT ON ANY SEARCH OR KEY PATH, AND IT IS UNEXPORTED SO THAT IT
// CANNOT BE PUT ON ONE. Its only use is as the subject of those tests, which have
// to run the shipped rule somewhere, and no Go test can run the script itself.
// A server that normalised through it would answer with the browser's copy of the
// rule and leave the module answering nothing any check compares.
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
// composition. The 11,172 syllables are NOT tabulated in what the browser is
// shipped: a few lines of arithmetic give their decomposition and their
// composition exactly, so DECOMP_TABLE holds 2,081 entries rather than 13,253 and
// COMPOSE_TABLE 961 rather than 12,133 (SPEC/WEB.md § Roadmap Tasks Page, What
// keeps the shipped rule equal to the server's; Acceptance Criterion 155).
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
// code point.
//
// It is the derivation of the data the shipped DECOMP_TABLE carries, and it
// exists as a named function so that the table has a function as its subject:
// internal/web's guard test (TestTaskSearchScript_ShippedRuleIsTheServerRule)
// holds the shipped table equal to this function, through the searchDecompose
// delegation, over every code point of Unicode. The server's own normalisation
// does not call it — NFC decomposes inside the module.
//
// The data is golang.org/x/text/unicode/norm's, which is the Go project's own
// implementation of UAX #15 and the only place canonical decomposition data is
// published for Go — the standard library's unicode package carries case
// mappings, categories and scripts, and no decomposition (SPEC/BUILD.md
// § External Dependencies, Unicode Data Rules 2).
//
// A code point with no canonical decomposition decomposes to itself, so the
// result is never empty. Hangul is decomposed by norm here and arithmetically by
// the browser, and the guard sweep holds the browser's ARITHMETIC equal to this
// function for every one of the 11,172 syllables.
func Decompose(r rune) []rune {
	return []rune(norm.NFD.String(string(r)))
}

// CombiningClass returns the canonical combining class of ONE code point:
// the number UAX #15's canonical ordering sorts a run of non-starters by, and the
// number the composition tests a character's blocking with.
//
// It is the derivation of the data the shipped CCC_TABLE carries, and the guard
// test holds that table equal to THIS function, through internal/web's
// searchCombiningClass delegation, over every code point of Unicode. The data is
// again norm's, for the reason Decompose gives.
func CombiningClass(r rune) uint8 {
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	return norm.NFD.Properties(buf[:n]).CCC()
}

// hasDecompositionMapping reports whether the module gives r a canonical
// decomposition mapping of its own. A Hangul syllable carries none here, because
// the module and the browser alike decompose it arithmetically.
func hasDecompositionMapping(r rune) bool {
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	return norm.NFD.Properties(buf[:n]).Decomposition() != nil
}

// Composition is the data the composition step of the browser's copy of the
// rule reads: the primary composites, and the set of code points that can be the
// SECOND element of one.
//
// Seconds holds the second elements of the table plus the Hangul V and T jamo,
// which compose arithmetically rather than through it. It is the set a code point
// has to begin with, after its full decomposition, for a code point before it to
// compose with it, which is how the pair test of this package chooses the
// sequences it sweeps.
type Composition struct {
	Pairs   map[[2]rune]rune
	Seconds map[rune]bool
}

// Compositions is the primary-composite data, derived ONCE from the character
// data of golang.org/x/text/unicode/norm and reused thereafter.
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
// normalises nothing must not pay for the derivation. The server's normalisation
// never builds it at all: NFC composes inside the module, so only the checks that
// hold the shipped rule equal to the server's read it.
var Compositions = sync.OnceValue(BuildComposition)

// BuildComposition derives the primary composites from the canonical
// decompositions, which is what a primary composite IS: a code point whose
// canonical decomposition is two characters, the first of them a starter, that
// Unicode does not exclude from composition.
//
// The derivation reads two things from the module and nothing else: the
// decompositions, and the Full_Composition_Exclusion character property, which
// IsCompositionExcluded reads through the one query SPEC/BUILD.md § External
// Dependencies, Unicode Data Rules 3, names for it. It never asks the module to
// compose, so the table it builds is a statement of the pairs that is independent
// of the module's own composition, and the tests that hold the browser's copy of
// the rule equal to that composition compare two things rather than one.
//
// The exclusions cannot be derived from the decompositions themselves, because a
// script exclusion such as U+0958 and a post-composition-version exclusion such
// as U+2ADC decompose exactly as an ordinary composite does; that is why the
// property is read at all.
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
	for cp := rune(0); cp <= MaxCodePoint; cp++ {
		if IsSurrogate(cp) || !hasDecompositionMapping(cp) {
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
// decomposition is excluded from composition — that is, whether Unicode's
// Full_Composition_Exclusion property is true of it. A code point carrying a
// canonical decomposition is NFC_QC=No exactly when that property holds, so the
// question is equivalently "is this code point already in Normalization Form C?",
// and IsNormalString is the module's answer to it.
//
// IT IS DELIBERATELY NOT norm.NFC.QuickSpanString(s) != len(s), WHICH IS A
// DIFFERENT QUESTION. QuickSpanString reports how much of a string is
// *quick-checked* to be in Normalization Form C, so for a single code point it
// answers NFC_QC != Yes — that is, No **or Maybe**. NFC_QC=Maybe is carried by
// every code point that can be the SECOND element of a primary composite, and
// such a code point is not excluded from composition; it is the reason the
// composition table has any entries at all.
//
// Under Unicode 15.0.0 the two questions happened to have the same answer,
// because no code point then carried both a canonical decomposition and
// NFC_QC=Maybe. Unicode 16.0.0 introduced twelve that do — U+113C5, U+113C7 and
// U+113C8 (Tulu-Tigalari), U+16121 to U+16128 (Gurung Khema) and U+16D68 (Kirat
// Rai) — and the quick-check form silently reported all twelve as excluded. That
// dropped their composites from the table and left the browser's copy of the
// rule returning the decomposition of a code point that composes, which is not
// Normalization Form C. TestIsCompositionExcluded_IsFullCompositionExclusion
// holds this function to the property as the Unicode Character Database
// publishes it, in both directions.
//
// IsNormalString returns a property of its argument, never a transformed string,
// and this function runs only in the one-time derivation of the composition
// table, never on a search (SPEC/BUILD.md § External Dependencies, Unicode Data
// Rules 3).
func IsCompositionExcluded(r rune) bool {
	return !norm.NFC.IsNormalString(string(r))
}

// Compose returns the primary composite of two code points, if there is
// one: the composition step of the browser's copy of the rule, over the data the
// shipped COMPOSE_TABLE carries.
//
// It states what task-search.js's composePair does: a leading jamo composes with
// a vowel jamo, and the syllable that makes with a trailing jamo, by UAX #15's
// arithmetic, and every other pair is looked up in Compositions. The server's
// normalisation does not call it — NFC composes inside the module — and
// internal/web's guard test holds the shipped table's answer equal to this
// function's for every code point a pair can reach.
func Compose(lead, trail rune) (rune, bool) {
	return composeFrom(Compositions(), lead, trail)
}

// composeFrom is Compose over composition data the caller already holds, so
// that the browser's algorithm, which composes once per code point, reads the
// derived data once per normaliser rather than once per pair.
func composeFrom(composition *Composition, lead, trail rune) (rune, bool) {
	if l, v := lead-HangulLBase, trail-HangulVBase; l >= 0 && l < HangulLCount && v >= 0 && v < HangulVCount {
		return HangulSBase + (l*HangulVCount+v)*HangulTCount, true
	}
	if s, t := lead-HangulSBase, trail-HangulTBase; s >= 0 && s < HangulSCount && s%HangulTCount == 0 &&
		t > 0 && t < HangulTCount {
		return lead + t, true
	}
	composite, ok := composition.Pairs[[2]rune{lead, trail}]
	return composite, ok
}

// NFC normalises text to Unicode's Normalization Form C: the canonical
// composition of the full canonical decomposition, as UAX #15 defines it. It is
// the server's normalisation, and its result is golang.org/x/text/unicode/norm's.
//
// A task's searchable text, a search term, and a knowledge-graph key under audit
// are all normalised through this function, so no two of them can drift apart
// (SPEC/WEB.md § Roadmap Tasks Page, One rule, and only one implementation of
// it; SPEC/GRAPH.md § Node Key Uniqueness; SPEC/BUILD.md § External
// Dependencies, Unicode Data Rules 3).
//
// NORMALISATION IS FOR COMPARISON ONLY. Nothing here rewrites what rmp stores or
// what a page renders: the caller is a derived searchable text, a derived term,
// or a key being compared with another key — never a title on its way to the
// database or to a card, and never a key on its way to the graph.
//
// ASCII is returned untouched, which is not a behaviour of its own: no ASCII
// code point has a canonical decomposition, none carries a non-zero combining
// class, and none is the second element of any primary composite, so an ASCII
// string is already in Normalization Form C and the module would return it
// unchanged. The fast path keeps an ordinary roadmap from consulting the module's
// tables at all.
func NFC(text string) string {
	if isASCII(text) {
		return text
	}
	return norm.NFC.String(text)
}

// clientTables is the data the shipped DECOMP_TABLE and CCC_TABLE carry, laid
// out so that the Go statement of the browser's algorithm reads it by index. It
// is built from Decompose and CombiningClass — the functions internal/web's guard
// test holds those shipped tables equal to — so it carries no data of its own.
// The data COMPOSE_TABLE carries is Compositions, which Compose reads.
type clientTables struct {
	// decomposition holds, for every code point, 0 when the code point
	// decomposes to itself — a Hangul syllable included, which is decomposed
	// arithmetically — and otherwise one more than the index of its full
	// decomposition in decompositions.
	decomposition  []int32
	decompositions [][]rune
	// classes holds the canonical combining class of every code point.
	classes []uint8
}

// loadClientTables derives clientTables once per process, on the first call of
// the browser's algorithm. The server never calls that algorithm, so a server
// never builds them.
var loadClientTables = sync.OnceValue(buildClientTables)

// buildClientTables derives clientTables over the whole of Unicode.
func buildClientTables() *clientTables {
	t := &clientTables{
		decomposition:  make([]int32, MaxCodePoint+1),
		decompositions: make([][]rune, 0, 2048),
		classes:        make([]uint8, MaxCodePoint+1),
	}
	var entries int32
	for cp := rune(0); cp <= MaxCodePoint; cp++ {
		if IsSurrogate(cp) {
			continue
		}
		t.classes[cp] = CombiningClass(cp)
		if !hasDecompositionMapping(cp) {
			continue
		}
		t.decompositions = append(t.decompositions, Decompose(cp))
		entries++
		t.decomposition[cp] = entries
	}
	return t
}

// clientNormaliser is the Go statement of the algorithm task-search.js's toNFC
// runs to put a term into Normalization Form C, over the data of the three
// tables the binary ships to it (SPEC/BUILD.md § External Dependencies, Unicode
// Data Rules 3).
//
// It follows the script step for step: every code point is replaced by its full
// canonical decomposition, a Hangul syllable arithmetically and every other code
// point from the DECOMP_TABLE data; each run of non-starters is put into
// canonical order by a STABLE insertion sort on the CCC_TABLE data, so two marks
// of one class keep the order they were written in; and the sequence is
// recomposed as Compose composes. A character C composes with the last starter L
// before it unless it is BLOCKED from L, which it is when some character between
// the two has class 0 or a class not below C's own; because the sequence is
// canonically ordered, the class of the character immediately before C is the
// largest of those between, so it is the only one the test reads — lastClass
// below, holding -1 while nothing separates C from L.
//
// The script walks a JavaScript string by code point; this walks UTF-8, and a
// byte that is not valid UTF-8 is read as U+FFFD, which is what the server's
// term preparation turns such a byte into (SPEC/WEB.md § Roadmap Tasks Page, The
// folding rule).
//
// Its buffers are reused from one call to the next, so one value serves one
// goroutine at a time.
type clientNormaliser struct {
	tables      *clientTables
	composition *Composition
	decomposed  []rune
	composed    []rune
}

// newClientNormaliser returns a normaliser over the derived data.
func newClientNormaliser() *clientNormaliser {
	return &clientNormaliser{
		tables:      loadClientTables(),
		composition: Compositions(),
		decomposed:  make([]rune, 0, 32),
		composed:    make([]rune, 0, 32),
	}
}

// appendNFC appends the Normalization Form C of src, as the browser computes
// it, to dst and returns the extended slice. It allocates nothing once its
// buffers have grown to the longest decomposition it has met.
func (c *clientNormaliser) appendNFC(dst, src []byte) []byte {
	decomposed := c.decomposed[:0]
	for len(src) > 0 {
		r, size := utf8.DecodeRune(src)
		src = src[size:]
		decomposed = c.appendDecomposition(decomposed, r)
	}
	c.decomposed = decomposed

	classes := c.tables.classes
	for k := 1; k < len(decomposed); k++ {
		class := classes[decomposed[k]]
		if class == 0 {
			continue
		}
		for j := k; j > 0; j-- {
			before := classes[decomposed[j-1]]
			if before == 0 || before <= class {
				break
			}
			decomposed[j-1], decomposed[j] = decomposed[j], decomposed[j-1]
		}
	}

	composed := c.composed[:0]
	starter, lastClass := -1, -1
	for _, r := range decomposed {
		class := int(classes[r])
		if starter >= 0 && lastClass < class {
			if composite, ok := composeFrom(c.composition, composed[starter], r); ok {
				composed[starter] = composite
				continue
			}
		}
		if class == 0 {
			starter, lastClass = len(composed), -1
		} else {
			lastClass = class
		}
		composed = append(composed, r)
	}
	c.composed = composed

	for _, r := range composed {
		dst = utf8.AppendRune(dst, r)
	}
	return dst
}

// appendDecomposition appends the full canonical decomposition of one code
// point to out, as task-search.js's decomposeCodePoint does.
func (c *clientNormaliser) appendDecomposition(out []rune, r rune) []rune {
	if s := r - HangulSBase; s >= 0 && s < HangulSCount {
		out = append(out, HangulLBase+s/HangulNCount, HangulVBase+(s%HangulNCount)/HangulTCount)
		if t := s % HangulTCount; t > 0 {
			out = append(out, HangulTBase+t)
		}
		return out
	}
	if i := c.tables.decomposition[r]; i > 0 {
		return append(out, c.tables.decompositions[i-1]...)
	}
	return append(out, r)
}

// clientNFC returns the Normalization Form C of text as the browser computes
// it. It is the convenience form of clientNormaliser.appendNFC for a single
// value; the subject of this package's tests, and nothing else.
func clientNFC(text string) string {
	return string(newClientNormaliser().appendNFC(nil, []byte(text)))
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
