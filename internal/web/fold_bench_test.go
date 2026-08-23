package web

import "testing"

// benchSink keeps the compiler from eliminating the work being measured.
var benchSink string

// BenchmarkSearchableText measures the board search's whole text preparation on
// the path a page render actually takes it: once per card, on the card's title.
//
// The ASCII cases are the ones that matter for an ordinary roadmap, and they are
// what the fast path in searchNFC exists for: an ASCII string is already in
// Normalization Form C, no pair of ASCII code points composes, and the rule
// therefore reduces to the fold it was before normalisation existed.
//
// The accented cases pay for the rule. They decompose, recompose, fold and
// recompose again, and the cost is stated here rather than assumed, so that a
// later change to the pipeline can be compared against it rather than argued
// about. The one-time derivation of the composition data is measured separately
// and excluded from these, because it happens once per process.
func BenchmarkSearchableText(b *testing.B) {
	for _, c := range []struct{ name, title string }{
		{"ascii-lower", "rotate the acquirer signing certificates"},
		{"ascii-mixed", "Rotate the Acquirer Signing Certificates"},
		{"latin-precomposed", precomposedTitle},
		{"latin-decomposed", decomposedTitle},
		{"greek", greekUpperTitle},
	} {
		b.Run(c.name, func(b *testing.B) {
			benchSink = searchableText(c.title) // derive the composition data before timing
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchSink = searchableText(c.title)
			}
		})
	}
}

// BenchmarkSearchCompositionDerivation measures the ONE-TIME derivation of the
// primary composites from the Unicode character data, which searchCompositions
// performs on first use and never again.
//
// It is derived rather than stored so that a change of Unicode version moves it
// and the guard test catches the drift; this is what that choice costs, paid once
// per process and only by a process that normalises something outside ASCII.
func BenchmarkSearchCompositionDerivation(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchSink = string(rune(len(buildSearchComposition().pairs)))
	}
}
