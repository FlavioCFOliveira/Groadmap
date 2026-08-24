package models

import (
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file gates SPEC/MODELS.md § Free-Text UTF-8 Encoding Constraint on the
// comment `body` — the only free-text field of the eight with a standard-input
// source, and therefore the only one where the rule has to hold on a value the
// application never materialises in full.
//
// The differential property that governs that path is stated in
// SPEC/COMMANDS.md § Comment Body Input Source and Precedence and pinned
// generally in comment_read_test.go: "the verdict the user sees is exactly the
// verdict a read-to-EOF implementation would reach". This file pins what that
// property demands of the encoding rule specifically, which is a pair of
// prohibitions that are easy to violate in opposite directions:
//
//   - the reader must not repair invalid bytes into U+FFFD, or the check that
//     follows it has nothing left to refuse; and
//   - the reader must not refuse at the first invalid byte either, or a body
//     that is at once malformed and oversized would report an encoding failure
//     on standard input while the flag path reported its length.

// wantEncodingRefusal is the message SPEC/COMMANDS.md pins for this field, minus
// the "Error: " prefix the CLI adds when it prints the error.
const wantEncodingRefusal = "validation error: body: the value is not valid UTF-8"

// wantLengthRefusal is the message that must win when a body is BOTH oversized
// and malformed, on either input path.
const wantLengthRefusal = "field exceeds maximum size: body exceeds maximum length of 4096 characters"

// readBodyThroughEveryChunking resolves raw through ReadCommentBody as a single
// buffer and as 1-, 3- and 7-byte chunks, and returns the value the reader
// produced. It fails when the four disagree, because a multi-byte sequence split
// across a read boundary is exactly where a scanner mishandles one.
func readBodyThroughEveryChunking(t *testing.T, raw string) string {
	t.Helper()

	whole, err := ReadCommentBody(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadCommentBody(%q): unexpected I/O error %v", raw, err)
	}
	for _, size := range []int{1, 3, 7} {
		got, cerr := ReadCommentBody(&chunkedReader{data: []byte(raw), n: size})
		if cerr != nil {
			t.Fatalf("ReadCommentBody(%q) in %d-byte chunks: unexpected I/O error %v", raw, size, cerr)
		}
		if got != whole {
			t.Fatalf("%d-byte chunks produced a different value than a single buffer:\n got: %q\nwant: %q",
				size, got, whole)
		}
	}
	return whole
}

// TestCommentBodyRefusesMalformedUTF8OnBothInputPaths is the acceptance
// criterion: every malformed shape the SPEC enumerates is refused, with the
// pinned message and the exit-6 class, whether the body arrived as a flag value
// or on standard input.
func TestCommentBodyRefusesMalformedUTF8OnBothInputPaths(t *testing.T) {
	t.Parallel()

	for _, c := range testenv.MalformedUTF8Corpus() {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			// The flag path: the whole value reaches ValidateCommentBody.
			stored, err := ValidateCommentBody(c.Value)
			if err == nil {
				t.Fatalf("the flag path accepted %q and would store it.\n  %s", c.Value, c.Why)
			}
			if err.Error() != wantEncodingRefusal {
				t.Errorf("flag path message\n got: %q\nwant: %q", err.Error(), wantEncodingRefusal)
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("flag path refusal must wrap utils.ErrValidation (exit 6); got %v", err)
			}
			if stored != "" {
				t.Errorf("a refused body must yield nothing to store; got %q", stored)
			}

			// The standard-input path: the reader produces the value, and the
			// same validator produces the verdict.
			body := readBodyThroughEveryChunking(t, c.Value)
			_, serr := ValidateCommentBody(body)
			if serr == nil {
				t.Fatalf("the standard-input path accepted %q and would store it", body)
			}
			if serr.Error() != wantEncodingRefusal {
				t.Errorf("standard-input message\n got: %q\nwant: %q", serr.Error(), wantEncodingRefusal)
			}
			if !errors.Is(serr, utils.ErrValidation) {
				t.Errorf("standard-input refusal must wrap utils.ErrValidation (exit 6); got %v", serr)
			}
		})
	}
}

// TestStreamingReaderCarriesInvalidBytesUnaltered is the first of the two
// prohibitions: the reader hands on what it read.
//
// If it substituted U+FFFD for each invalid byte — which is what ranging over a
// string does by default, and what the reader would produce if it re-encoded the
// decoded rune instead of retaining the raw bytes — then the value handed to
// ValidateCommentBody would be well-formed UTF-8, the encoding rule would find
// nothing wrong with it, and the malformed body would be stored with its bytes
// silently rewritten. That is option (b) of rmp task 180, which the user
// declined.
func TestStreamingReaderCarriesInvalidBytesUnaltered(t *testing.T) {
	t.Parallel()

	for _, c := range testenv.MalformedUTF8Corpus() {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			body := readBodyThroughEveryChunking(t, c.Value)

			if want := strings.TrimSpace(c.Value); body != want {
				t.Errorf("the reader altered the bytes it carried:\n got: %q\nwant: %q", body, want)
			}
			if utf8.ValidString(body) {
				t.Errorf("the reader returned valid UTF-8 for a malformed body (%q); "+
					"the invalid bytes were repaired away and there is nothing left to refuse", body)
			}
			if strings.ContainsRune(body, utf8.RuneError) && !strings.ContainsRune(c.Value, utf8.RuneError) {
				t.Errorf("the reader introduced U+FFFD into %q; invalid bytes must be retained as supplied", body)
			}
		})
	}
}

// TestOversizedMalformedBodyReportsItsLengthOnBothPaths is the second
// prohibition, and the one that fixes the ORDER decision of rmp task 180 in
// place: the cap keeps its position, so a body that is at once oversized and
// malformed is refused for its LENGTH, never for its encoding.
//
// The invalid byte sits at offset 3, far ahead of the cap, so a reader that
// refused at the first malformed byte would report the encoding failure here
// while the flag path reported the length one — the two paths would disagree,
// which SPEC/COMMANDS.md § Comment Body Input Source and Precedence forbids.
func TestOversizedMalformedBodyReportsItsLengthOnBothPaths(t *testing.T) {
	t.Parallel()

	for _, c := range testenv.MalformedUTF8Corpus() {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			raw := "abc" + c.Value + strings.Repeat("x", MaxCommentBody)
			if utf8.RuneCountInString(strings.TrimSpace(raw)) <= MaxCommentBody {
				t.Fatalf("the fixture is not over the cap, so it cannot show which rule wins")
			}

			_, err := ValidateCommentBody(raw)
			if err == nil {
				t.Fatal("the flag path accepted an oversized body")
			}
			if err.Error() != wantLengthRefusal {
				t.Errorf("flag path message\n got: %q\nwant: %q", err.Error(), wantLengthRefusal)
			}
			if !errors.Is(err, utils.ErrFieldTooLarge) {
				t.Errorf("flag path refusal must wrap utils.ErrFieldTooLarge; got %v", err)
			}

			body := readBodyThroughEveryChunking(t, raw)
			_, serr := ValidateCommentBody(body)
			if serr == nil {
				t.Fatal("the standard-input path accepted an oversized body")
			}
			if serr.Error() != wantLengthRefusal {
				t.Errorf("the standard-input path refused an oversized body for the wrong reason.\n"+
					" got: %q\nwant: %q\n"+
					"A reader that stops at the first invalid byte reports the encoding failure here, "+
					"while the flag path reports the length one; the two paths must agree.",
					serr.Error(), wantLengthRefusal)
			}
			if !errors.Is(serr, utils.ErrFieldTooLarge) {
				t.Errorf("standard-input refusal must wrap utils.ErrFieldTooLarge; got %v", serr)
			}
		})
	}
}

// TestStreamingReaderReadsPastAnInvalidByteButStaysBounded pins the two
// prohibitions against each other, which is the only way to show neither was
// satisfied by giving up the other.
//
// The stream carries an invalid byte at offset 3 and then far more content than
// the cap allows. Reading must continue past the invalid byte — otherwise the
// verdict would be an encoding failure rather than the length one — and must
// still stop long before the writer is done, which is the bounded-read property
// audit #168 introduced and this rule may not cost.
func TestStreamingReaderReadsPastAnInvalidByteButStaysBounded(t *testing.T) {
	t.Parallel()

	const offered = 64 << 20 // 64 MiB, ~16000x the largest acceptable body
	const bound = 1 << 20    // generous ceiling: the scanner needs ~32 KiB

	h := &hostileReader{remaining: offered, fill: 'x'}
	src := io.MultiReader(strings.NewReader("abc\x80"), h)

	body, err := ReadCommentBody(src)
	if err != nil {
		t.Fatalf("ReadCommentBody: unexpected I/O error %v", err)
	}

	if _, verr := ValidateCommentBody(body); verr == nil || verr.Error() != wantLengthRefusal {
		t.Errorf("verdict\n got: %v\nwant: %q\n"+
			"the reader must not settle the verdict at the invalid byte at offset 3", verr, wantLengthRefusal)
	}
	if h.consumed == 0 {
		t.Error("the reader stopped at the invalid byte and never read the content that follows it")
	}
	if h.consumed > bound {
		t.Errorf("consumed %d bytes of the %d offered; the read is no longer bounded", h.consumed, offered)
	}
	if len(body) > bound {
		t.Errorf("retained %d bytes; the returned value is no longer bounded", len(body))
	}
}

// TestMalformedBodyBelowTheCapReachesTheEncodingRule is the counterpart of the
// two tests above: with the length rule satisfied, the encoding rule is the one
// that speaks, on both paths and at every chunk size. Without it, a reader that
// simply refused nothing would still pass the oversized cases.
func TestMalformedBodyBelowTheCapReachesTheEncodingRule(t *testing.T) {
	t.Parallel()

	for _, c := range testenv.MalformedUTF8Corpus() {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			// Comfortably under the cap, and long enough that the malformed
			// bytes are nowhere near either edge of the value.
			raw := strings.Repeat("x", 100) + c.Value + strings.Repeat("y", 100)
			if utf8.RuneCountInString(raw) > MaxCommentBody {
				t.Fatalf("the fixture is over the cap, so the length rule would win instead")
			}

			if _, err := ValidateCommentBody(raw); err == nil || err.Error() != wantEncodingRefusal {
				t.Errorf("flag path verdict\n got: %v\nwant: %q", err, wantEncodingRefusal)
			}

			body := readBodyThroughEveryChunking(t, raw)
			if _, err := ValidateCommentBody(body); err == nil || err.Error() != wantEncodingRefusal {
				t.Errorf("standard-input verdict\n got: %v\nwant: %q", err, wantEncodingRefusal)
			}
		})
	}
}

// TestWellFormedBodyIsStillAccepted is the non-vacuity guard for this whole
// file: the rule must refuse malformed bytes without refusing text. A body of
// accented Latin, CJK and emoji round-trips through both paths unchanged.
func TestWellFormedBodyIsStillAccepted(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"Reconciled batch SEPA-20260815-004 against the settlement file.",
		"medição de latência no ciclo de liquidação: 1.2 ms",
		"監査ログを検証し、差分は見つからなかった",
		"shipped \U0001F680 and measured the cut-off window",
	} {
		want := strings.TrimSpace(raw)

		stored, err := ValidateCommentBody(raw)
		if err != nil {
			t.Errorf("the flag path refused well-formed text %q: %v", raw, err)
		}
		if stored != want {
			t.Errorf("flag path stored %q, want %q", stored, want)
		}

		body := readBodyThroughEveryChunking(t, raw)
		streamStored, serr := ValidateCommentBody(body)
		if serr != nil {
			t.Errorf("the standard-input path refused well-formed text %q: %v", raw, serr)
		}
		if streamStored != want {
			t.Errorf("standard-input path stored %q, want %q", streamStored, want)
		}
	}
}
