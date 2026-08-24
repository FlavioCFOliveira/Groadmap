package testenv

// This file holds the module's one corpus of values built to an exact number of
// Unicode code points, so every layer that proves a free-text length cap proves
// it against the same probes.
//
// Why it lives here rather than in each package that needs it: the caps it
// exercises are spread across three layers whose tests are in three packages —
// the unit itself in internal/utils, the field validators in internal/models,
// and every command that writes a field in internal/commands — plus the CHECK
// constraints in internal/db. Four copies of "255 CJK characters" would
// eventually disagree about what the boundary is, and the layer with the
// weakest copy would be the one whose proof meant least. Nothing here depends
// on another package of this module, which is the property this package's own
// gate relies on.
//
// The defect the corpus exists for (rmp task 296): the task and sprint caps
// measured BYTES with len() while their message, SPEC/MODELS.md and
// CHECK(length(<column>) <= N) all named CHARACTERS, so a title of 102 CJK
// characters was refused for exceeding "255 characters". An ASCII-only test
// suite cannot see that, because for ASCII the two counts are the same number.

// LengthProbeScript is one writing system, together with a builder that produces
// a value of an exact number of code points in it.
type LengthProbeScript struct {
	// Repeat returns a value of exactly n code points. The value carries no
	// whitespace at either edge, so a cap measured on the stored (trimmed) value
	// and a cap measured on the value as supplied count the same thing: a test
	// built on it cannot pass by accident on a command that trims.
	Repeat func(n int) string

	// Name identifies the script for a subtest name and a failure message.
	Name string

	// BytesPerRune is what one code point of this script occupies in UTF-8. It is
	// what makes each case prove something different: at 1 the byte count and the
	// character count agree, so a byte-counting cap passes and the case is only a
	// control; at 2, 3 and 4 they diverge, and the cap's unit is decided.
	BytesPerRune int
}

// LengthProbeScripts returns the four scripts every length-boundary proof draws
// on, spanning all four UTF-8 encoded widths:
//
//  1. ASCII, one byte per code point — the control, the only script under which
//     a byte-counting cap behaves correctly;
//  2. accented Latin, two bytes — the script this project's own roadmap is
//     written in;
//  3. CJK, three bytes — the defect exactly as it was reported;
//  4. emoji beyond the Basic Multilingual Plane, four bytes — one code point
//     that the three plausible wrong units would count as four (bytes), two
//     (UTF-16 code units) or, for a longer emoji sequence, one grapheme.
//
// Every entry satisfies two properties that a caller's own tests assert rather
// than assume, because they are what make a proof built on this corpus
// non-vacuous:
//
//  1. Repeat(n) is exactly n code points, so a value can be placed exactly at a
//     cap and exactly one over it; and
//  2. len(Repeat(n)) is n*BytesPerRune, so the byte count and the character
//     count actually differ wherever BytesPerRune is above 1 — a corpus whose
//     "CJK" probe had drifted to ASCII would still pass a cap test while proving
//     nothing at all.
func LengthProbeScripts() []LengthProbeScript {
	return []LengthProbeScript{
		{
			Name:         "ascii",
			BytesPerRune: 1,
			Repeat:       repeatToRunes("refuse-the-token-whose-expiry-is-the-current-second-"),
		},
		{
			Name:         "accented latin",
			BytesPerRune: 2,
			Repeat:       repeatToRunes("çãõáéíóúâêôàüñ"),
		},
		{
			Name:         "cjk",
			BytesPerRune: 3,
			Repeat:       repeatToRunes("資料庫遷移驗證任務標題"),
		},
		{
			Name:         "emoji beyond the BMP",
			BytesPerRune: 4,
			Repeat:       repeatToRunes("\U0001F680\U0001F5C4\U0001F9EA\U0001F9F1\U0001F52D"),
		},
	}
}

// repeatToRunes builds a repeater that returns exactly n code points of seed,
// repeating the seed as often as needed and cutting on a code-point boundary —
// never in the middle of a multi-byte sequence, which would produce a value the
// encoding rule refuses and a length test that proved the wrong refusal.
func repeatToRunes(seed string) func(n int) string {
	runes := []rune(seed)
	return func(n int) string {
		if n <= 0 {
			return ""
		}
		out := make([]rune, 0, n)
		for len(out) < n {
			if remaining := n - len(out); remaining < len(runes) {
				out = append(out, runes[:remaining]...)
				break
			}
			out = append(out, runes...)
		}
		return string(out)
	}
}
