package web

import (
	"strings"
	"unicode"

	"github.com/FlavioCFOliveira/Groadmap/internal/unicodenorm"
)

// This file is the tasks page's search text preparation: the trim, the
// normalisation and the fold, each stated ONCE, on the server. The term and every
// task's searchable text are prepared by these functions alone, so the two cannot
// be prepared by two implementations of one description, and no copy of any of
// them is shipped to the browser (SPEC/WEB.md § Roadmap Tasks Page, The text
// search, One implementation; Acceptance Criteria 119, 122 and 155).

// foldSearch applies the search's folding rule to text: Unicode's SIMPLE
// lowercase mapping, the single replacement code point the Unicode Character
// Database gives a code point, applied to each code point on its own, with a code
// point that has no such mapping folding to itself.
//
// It is the ONE implementation of that rule. A task's searchable text and a
// search term both fold through this function (SPEC/WEB.md § Roadmap Tasks Page,
// The folding rule; Acceptance Criteria 118 and 119).
//
// strings.ToLower is that rule: it is strings.Map over unicode.ToLower, which is
// the simple mapping, so the fold is unconditional (what a code point folds to
// never depends on its neighbours), one code point in and one code point out, and
// locale-independent. It is deliberately NOT Unicode's Default Case Conversion,
// which differs on exactly two code points: U+0130 folds here to U+0069 alone,
// never to U+0069 U+0307, and U+03A3 folds to U+03C3 in every position, word-final
// included, never to the final form U+03C2. Nothing is rewritten afterwards — a
// U+03C2 the user typed is already lower case and stays U+03C2, so a term of
// "οδός" keeps finding a task titled "οδός".
//
// A string holding bytes that are not valid UTF-8 is not a sequence of code
// points at all; strings.ToLower replaces each such byte with U+FFFD, which is the
// behaviour SPEC/WEB.md § Roadmap Tasks Page requires of a malformed term: folded
// like any other term, neither an error nor an absent one.
func foldSearch(text string) string {
	return strings.ToLower(text)
}

// isSearchSpace reports whether r is the whitespace the search's trim rule
// removes from the ends of a term: a code point carrying Unicode's White_Space
// property (SPEC/WEB.md § Roadmap Tasks Page, The trim rule; Acceptance
// Criterion 121).
//
// unicode.IsSpace is that property exactly: Go's unicode tables define IsSpace as
// White_Space, of whatever Unicode version the toolchain ships. The set is
// deliberately NOT the one a JavaScript platform's own trimming removes: it keeps
// U+0085 (NEXT LINE) in the set, because U+0085 carries the property, and leaves
// U+FEFF (ZERO WIDTH NO-BREAK SPACE) out of it, because U+FEFF does not.
func isSearchSpace(r rune) bool {
	return unicode.IsSpace(r)
}

// trimSearchTerm removes every leading and trailing code point carrying the
// White_Space property from a raw search term, stopping at the first code point
// that does not carry it.
//
// Whitespace INSIDE the term therefore survives, is part of the term, and is
// matched literally; a term made only of such code points becomes the empty
// string, which is no term at all and is no criterion (SPEC/WEB.md § Roadmap Tasks
// Page, The trim rule; Matching rule).
//
// The task's searchable text is NOT trimmed: the trim is the term's alone, and a
// task's own leading or trailing whitespace is part of its text.
func trimSearchTerm(raw string) string {
	return strings.TrimFunc(raw, isSearchSpace)
}

// foldSearchTerm prepares a raw search term for the matching rule: trimmed by
// trimSearchTerm, THEN normalised and folded by searchableText, the order
// SPEC/WEB.md § Roadmap Tasks Page, Matching rule fixes.
func foldSearchTerm(raw string) string {
	return searchableText(trimSearchTerm(raw))
}

// searchNFC normalises text to Unicode's Normalization Form C, through the one
// normalisation function the binary has (internal/unicodenorm, which reads
// golang.org/x/text/unicode/norm). A task's searchable text and a search term are
// both normalised through THIS function (SPEC/WEB.md § Roadmap Tasks Page, The
// normalisation rule; Acceptance Criterion 155).
func searchNFC(text string) string { return unicodenorm.NFC(text) }

// searchableText is the WHOLE preparation of a text for the search, and the one
// place its steps are stated in their order: normalise, fold, normalise again.
//
// A task's searchable text and a search term (foldSearchTerm, after its trim)
// both go through THIS function, so the corpus and the term are one rule rather
// than two implementations of one description.
//
// NORMALISE BEFORE FOLDING. The order is observable, and normalising first is the
// only order that gives a title written with U+0130 and a title written as U+0049
// followed by U+0307 — the same text by Unicode's own definition — one searchable
// text. Folding first would give U+0069 for one and U+0069 U+0307 for the other.
//
// AND NORMALISE AGAIN AFTERWARDS. The fold can produce a sequence that composes
// where the unfolded one did not: NFC leaves U+0048 U+0331 as two code points, the
// fold lowers the H, and U+0068 U+0331 composes to U+1E96. Without the second pass
// a task titled "H" followed by U+0331 would not be found by the term U+1E96. U+1E97, U+1E98,
// U+1E99 and U+01F0 behave the same way. A third pass would change nothing
// (SPEC/WEB.md § Roadmap Tasks Page, The normalisation rule; Acceptance
// Criterion 152).
//
// The result is a DERIVED form for comparison only: the bytes rmp stores are
// untouched, and the row renders the stored title itself.
func searchableText(text string) string {
	return searchNFC(searchFold(searchNFC(text)))
}

// searchFold is the folding function searchableText applies, and therefore the
// one the term and every task's searchable text are folded by. It is foldSearch in
// production; it is a variable only so that a test can substitute it and observe
// that the verdict for the term and for the searchable text changes alike, which
// is what proves there is no second folding implementation on either side
// (SPEC/WEB.md Acceptance Criterion 119). The package runs no test with
// t.Parallel, so the swap is unsynchronised.
var searchFold = foldSearch
