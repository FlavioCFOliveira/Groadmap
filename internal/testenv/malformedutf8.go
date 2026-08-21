package testenv

// This file holds the module's one corpus of byte sequences that are not valid
// UTF-8, so every package that proves SPEC/MODELS.md § Free-Text UTF-8 Encoding
// Constraint proves it against the same inputs.
//
// Why it lives here rather than in each package that needs it: the constraint
// governs eight fields reached through three layers — the rule itself in
// internal/utils, the comment body's whole-value and streaming paths in
// internal/models, and every command that writes a field in internal/commands —
// and each layer's tests are in a different package. A corpus copied into three
// places drifts, and the layer that ended up with the weakest copy would be the
// one whose proof meant least. Nothing here depends on another package of this
// module, so the property this package's own gate relies on is preserved.

// MalformedUTF8 is one input that the encoding constraint must refuse, together
// with the reason it is malformed.
//
// Value is deliberately ordinary text with the malformed bytes embedded in it,
// not the bytes alone: what the constraint has to catch is a realistic value
// that carries a few bad bytes, and a value made of nothing else would also
// exercise the empty/whitespace paths that have refusals of their own.
type MalformedUTF8 struct {
	// Name identifies the shape for a subtest name and a failure message.
	Name string
	// Value is the input to supply, malformed bytes included.
	Value string
	// Why records which item of the SPEC's list of malformed shapes this is,
	// and where such a sequence comes from in practice.
	Why string
}

// MalformedUTF8Corpus returns one input for each malformed shape
// SPEC/MODELS.md § Free-Text UTF-8 Encoding Constraint enumerates, in the order
// the SPEC lists them.
//
// Every entry satisfies two properties that the caller's own tests assert
// rather than assume, because they are what make a proof built on this corpus
// non-vacuous:
//
//  1. the value is not valid UTF-8, so the encoding rule has something to
//     refuse; and
//  2. the value carries no forbidden control character, so the encoding rule is
//     the ONLY rule that can refuse it — a corpus that tripped the
//     control-character rule as well would pass an encoding test while proving
//     nothing about the encoding rule.
//
// The returned slice is freshly built on each call, so a caller may reorder or
// extend its copy without affecting anyone else's.
func MalformedUTF8Corpus() []MalformedUTF8 {
	return []MalformedUTF8{
		{
			Name:  "lone continuation byte",
			Value: "Ledger batch SEPA-20260815-004 \x80 failed to reconcile",
			Why: "SPEC item 1: a continuation byte (0x80-0xBF) that no lead byte " +
				"introduces. This is what a value cut at a byte offset rather than a " +
				"rune boundary leaves behind.",
		},
		{
			Name:  "lone 0xFF",
			Value: "Settlement window closed at 17:00\xff before the last file arrived",
			Why: "SPEC item 2: a byte that never occurs in valid UTF-8 at all. This is " +
				"what arrives when a latin-1 or binary payload is handed over as if it " +
				"were text.",
		},
		{
			Name:  "overlong encoding",
			Value: "Traversal probe in the upload path: ..\xc0\xaf..\xc0\xafetc/passwd",
			Why: "SPEC item 3: `/` (U+002F) written as the two bytes 0xC0 0xAF instead " +
				"of its shortest form. Overlong forms are the classic way a filter that " +
				"matches on the shortest form is bypassed, which is why refusing them is " +
				"the only safe reading of the rule.",
		},
		{
			Name:  "lone surrogate",
			Value: "Imported from a UTF-16 feed carrying an unpaired \xed\xa0\x80 surrogate",
			Why: "SPEC item 4: a surrogate code point, U+D800-U+DFFF, written as three " +
				"bytes. Surrogates are not characters and have no UTF-8 encoding; they " +
				"reach a value through a careless transcoding from UTF-16.",
		},
		{
			Name:  "truncated sequence at the end of the input",
			Value: "Reconciliation summary for the medi\xc3",
			Why: "SPEC item 5: a sequence a lead byte begins and the input ends before " +
				"completing — here the two-byte encoding of `ç` with its continuation " +
				"byte lost. On the streaming path this is the shape that distinguishes " +
				"the input ending from a read ending, which is not malformed.",
		},
	}
}
