package web

import (
	"strings"
	"unicode"
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

// foldSearchTerm normalises a raw search term into the form the matching rule
// compares with: trimmed by trimSearchTerm, THEN folded by foldSearch.
//
// The order is fixed, and the client performs the same two steps in the same
// order. It is not observable under the Unicode version in force — no code point
// carrying White_Space folds to anything but itself, and none outside the property
// folds into it, so the two steps commute — but fixing it is what keeps the
// contract from resting on that coincidence (SPEC/WEB.md § Roadmap Tasks Page,
// Trim first, then fold).
func foldSearchTerm(raw string) string {
	return foldSearch(trimSearchTerm(raw))
}
