package models

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// fuzzForbiddenCodePoint reports whether r is one of the code points the
// Free-Text Control-Character Constraint forbids, written from the SPEC's list
// (SPEC/COMMANDS.md § Control-Character Constraint (All Free-Text Fields)) rather
// than through utils.IsForbiddenControlChar, which is what it judges.
func fuzzForbiddenCodePoint(r rune) bool {
	switch {
	case r == 0x09 || r == 0x0A || r == 0x0D:
		return false
	case r < 0x20 || r == 0x7F:
		return true
	case r == 0x200E || r == 0x200F:
		return true
	case r >= 0x202A && r <= 0x202E:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0xFEFF:
		return true
	}
	return false
}

// fuzzExpectedBodyVerdict is the verdict SPEC/COMMANDS.md § Comment Body Input
// Source and Precedence and § UTF-8 Encoding Constraint (All Free-Text Fields)
// assign to a body as supplied, whichever input path carried it: the length cap
// on the trimmed value, then the encoding rule, then the control-character rule,
// both on the value as supplied, then the emptiness judgement on the trimmed
// value. A body that is empty after the trim and breaks no content rule is an
// absent body (exit code 2), which bodyVerdict records as supplied == false.
func fuzzExpectedBodyVerdict(raw string) bodyVerdict {
	trimmed := strings.TrimSpace(raw)
	switch {
	case utf8.RuneCountInString(trimmed) > MaxCommentBody:
		return bodyVerdict{supplied: true,
			errMsg: "field exceeds maximum size: body exceeds maximum length of 4096 characters"}
	case !utf8.ValidString(raw):
		return bodyVerdict{supplied: true, errMsg: "validation error: body: the value is not valid UTF-8"}
	case strings.ContainsFunc(raw, fuzzForbiddenCodePoint):
		return bodyVerdict{supplied: true, errMsg: "validation error: body: control characters are not allowed"}
	case trimmed == "":
		return bodyVerdict{}
	default:
		return bodyVerdict{supplied: true, stored: trimmed}
	}
}

// FuzzCommentBody drives the comment body rules with arbitrary bodies, through
// both input paths the four body-taking comment subcommands have: the `--body`
// value as supplied, and standard input through the bounded reader.
//
// Invariants (SPEC/COMMANDS.md § Comment Body Input Source and Precedence,
// § Control-Character Constraint and § UTF-8 Encoding Constraint (All Free-Text
// Fields)):
//
//  1. The flag path reaches the verdict fuzzExpectedBodyVerdict states: over the
//     4096-character cap, not valid UTF-8, a forbidden control character, absent,
//     or accepted — in that order, with the published message.
//  2. An accepted body is stored as its trimmed form: valid UTF-8, 1 to 4096
//     characters, and free of forbidden code points.
//  3. Every refusal of a supplied body carries an exit-code-6 class, and an empty
//     body the domain is handed directly is ErrValidation too.
//  4. The standard-input path reaches the identical verdict, whether the stream
//     arrives whole or one, three, or 4096 bytes at a time.
func FuzzCommentBody(f *testing.F) {
	for _, seed := range differentialBodies() {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		raw := string(input)
		want := fuzzExpectedBodyVerdict(raw)

		if got := resolveBody(raw); got != want {
			t.Fatalf("--body path: body %q resolved to %+v, want %+v", truncateForReport(raw), got, want)
		}

		stored, err := ValidateCommentBody(raw)
		if err == nil {
			if !utf8.ValidString(stored) || utf8.RuneCountInString(stored) > MaxCommentBody ||
				stored == "" || strings.ContainsFunc(stored, fuzzForbiddenCodePoint) ||
				stored != strings.TrimSpace(raw) {
				t.Fatalf("ValidateCommentBody(%q) accepted and stored %q, which breaks a published rule",
					truncateForReport(raw), truncateForReport(stored))
			}
		} else if !errors.Is(err, utils.ErrValidation) && !errors.Is(err, utils.ErrFieldTooLarge) {
			t.Fatalf("ValidateCommentBody(%q) = %v, which carries no exit-code-6 class",
				truncateForReport(raw), err)
		}

		for _, size := range []int{0, 1, 3, commentBodyReadChunk} {
			var got bodyVerdict
			if size == 0 {
				got = streamingVerdict(t, bytes.NewReader(input))
			} else {
				got = streamingVerdict(t, &chunkedReader{data: bytes.Clone(input), n: size})
			}
			if got != want {
				t.Fatalf("standard-input path (chunk %d): body %q resolved to %+v, want %+v",
					size, truncateForReport(raw), got, want)
			}
		}
	})
}

// truncateForReport shortens a body for a failure message; a 4097-character body
// quoted in full buries the verdict it was quoted to explain.
func truncateForReport(s string) string {
	const keep = 120
	if len(s) <= keep {
		return s
	}
	return s[:keep] + "...(truncated)"
}
