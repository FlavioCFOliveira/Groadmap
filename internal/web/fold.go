package web

import "strings"

//go:generate go run foldtable_gen.go

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
// run-encoded FOLD_TABLE in static/task-search.js, which foldtable_gen.go
// generates from this rule and which TestTaskSearchScript_FoldTableIsTheServerFold
// checks against this function over the whole of Unicode.
func foldSearch(text string) string {
	return strings.ToLower(text)
}

// foldSearchTerm normalises a raw search term into the form the matching rule
// compares with: surrounding whitespace stripped, then folded by foldSearch.
//
// A term that is empty or entirely whitespace folds to the empty string, which is
// no term at all and matches every task. Whitespace INSIDE the term survives and
// is matched literally (SPEC/WEB.md § Roadmap Tasks Page, Matching rule).
func foldSearchTerm(raw string) string {
	return foldSearch(strings.TrimSpace(raw))
}
