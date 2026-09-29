// Package unicodenorm holds Groadmap's Unicode Normalization Form C, as UAX #15
// defines it: the canonical composition of the full canonical decomposition.
//
// WHY THIS IS A PACKAGE OF ITS OWN. The rule has two consumers on opposite sides
// of an import edge: the roadmap tasks page's search in internal/web
// (SPEC/WEB.md § Roadmap Tasks Page, The normalisation rule) and the
// knowledge-graph key audit in internal/graphkeys, which judges two node keys the
// same key when their NFC forms are equal (SPEC/GRAPH.md § Node Key Uniqueness).
// internal/commands imports internal/web, so internal/web cannot be imported back
// from a leaf. Duplicating the rule would put two normalisations in one binary,
// which is what internal/graphlock and internal/backoff were each extracted to
// avoid, so both consumers normalise through NFC here.
//
// THE SERVER NORMALISES WITH THE MODULE, AND NOTHING ELSE NORMALISES. NFC returns
// the Normalization Form C of golang.org/x/text/unicode/norm, the Go project's own
// implementation of UAX #15. Groadmap derives no normalisation data of its own
// from the module, ships none to the browser, and keeps no second statement of
// the algorithm (SPEC/BUILD.md § External Dependencies, Unicode Data Rules 3).
//
// NORMALISATION IS FOR COMPARISON ONLY. Nothing here rewrites what rmp stores or
// what a page renders. Every caller normalises a DERIVED value — a searchable
// text, a term, a key being compared with another key — never a value on its way
// to the database, to a page, or to the graph.
package unicodenorm

import (
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// NFC normalises text to Unicode's Normalization Form C: the canonical
// composition of the full canonical decomposition, as UAX #15 defines it. Its
// result is golang.org/x/text/unicode/norm's.
//
// A task's searchable text, a search term, and a knowledge-graph key under audit
// are all normalised through this function, so no two of them can drift apart
// (SPEC/WEB.md § Roadmap Tasks Page, One implementation; SPEC/GRAPH.md § Node Key
// Uniqueness; SPEC/BUILD.md § External Dependencies, Unicode Data Rules 3).
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
