package models

import (
	"errors"
	"io"
	"math/rand"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// bodyVerdict is the observable outcome of resolving a comment body: whether a
// body was supplied at all, the text that would be stored, and the failure the
// domain reports. It is what the streaming reader must preserve exactly.
type bodyVerdict struct {
	stored   string
	errMsg   string
	supplied bool
}

// referenceVerdict reproduces the pipeline the bounded reader replaced: read the
// WHOLE stream, treat a body that trims to nothing as absent, then apply the
// domain rules to the body AS SUPPLIED. It is the oracle every differential case
// below is compared against, so a semantic drift in the streaming reader fails
// the test rather than shipping.
func referenceVerdict(raw string) bodyVerdict {
	if NormalizeCommentBody(raw) == "" {
		return bodyVerdict{}
	}
	stored, err := ValidateCommentBody(raw)
	v := bodyVerdict{supplied: true, stored: stored}
	if err != nil {
		v.errMsg = err.Error()
		v.stored = ""
	}
	return v
}

// streamingVerdict resolves the same body through ReadCommentBody, wired exactly
// as the comment subcommands wire it: the reader produces the value, and
// ValidateCommentBody — still the sole owner of the rules — produces the verdict.
func streamingVerdict(t *testing.T, r io.Reader) bodyVerdict {
	t.Helper()
	body, err := ReadCommentBody(r)
	if err != nil {
		t.Fatalf("ReadCommentBody returned an I/O error for an in-memory reader: %v", err)
	}
	if NormalizeCommentBody(body) == "" {
		return bodyVerdict{}
	}
	stored, verr := ValidateCommentBody(body)
	v := bodyVerdict{supplied: true, stored: stored}
	if verr != nil {
		v.errMsg = verr.Error()
		v.stored = ""
	}
	return v
}

// chunkedReader hands out at most n bytes per Read, so a multi-byte sequence —
// and every scanner decision — is exercised across chunk boundaries.
type chunkedReader struct {
	data []byte
	n    int
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := c.n
	if n > len(c.data) {
		n = len(c.data)
	}
	if n > len(p) {
		n = len(p)
	}
	copy(p, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}

// differentialBodies are the inputs whose verdict must be identical under both
// implementations. They cover every branch of the scanner: leading/trailing/
// interior whitespace, the cap boundary, multi-byte and invalid UTF-8 bytes,
// each forbidden control class in each position, and the whitespace-only forms
// that count as an absent body.
func differentialBodies() map[string]string {
	const vt = "\v"      // 0x0B: forbidden, and stripped by TrimSpace
	const ff = "\f"      // 0x0C: forbidden, and stripped by TrimSpace
	const nel = "\u0085" // whitespace, permitted
	bodies := map[string]string{
		"empty":                       "",
		"single character":            "a",
		"plain sentence":              "Reviewed the audit trail and signed it off.",
		"leading whitespace":          "   \n\t Reviewed the WAL replay path",
		"trailing whitespace":         "Reviewed the WAL replay path \n\t  ",
		"surrounding whitespace":      "\n\n  Reviewed the WAL replay path  \n\n",
		"interior newlines":           "First finding\nSecond finding\n\nThird finding",
		"interior tabs":               "column\tone\tcolumn\ttwo",
		"whitespace only spaces":      "     ",
		"whitespace only mixed":       " \t\n\r ",
		"whitespace only with VT":     vt,
		"whitespace only with FF":     " " + ff + " ",
		"leading VT then content":     vt + "finding",
		"trailing FF after content":   "finding" + ff,
		"interior VT":                 "find" + vt + "ing",
		"leading NUL":                 "\x00finding",
		"interior DEL":                "find\x7fing",
		"escape sequence":             "\x1b[31mred\x1b[0m",
		"bidi override":               "before\u202eafter",
		"isolate control":             "before\u2066after",
		"byte order mark":             "\ufeffheading",
		"NEL is ordinary whitespace":  nel + "finding" + nel,
		"non breaking space trimmed":  "\u00a0finding\u00a0",
		"multi byte content":          "medição de latência: 1.2 ms",
		"cjk content":                 "監査ログの検証",
		"emoji content":               "shipped \U0001F680 and measured",
		"invalid utf8 single byte":    "a\x80b",
		"invalid utf8 lone ff":        "a\xffb",
		"overlong encoding":           "a\xc0\x80b",
		"lone surrogate":              "a\xed\xa0\x80b",
		"truncated sequence at end":   "finding\xe2\x82",
		"truncated sequence only":     "\xe2\x82",
		"cap exactly":                 strings.Repeat("a", MaxCommentBody),
		"cap exceeded by one":         strings.Repeat("a", MaxCommentBody+1),
		"cap exceeded far":            strings.Repeat("a", MaxCommentBody*3),
		"cap exactly multibyte":       strings.Repeat("é", MaxCommentBody),
		"cap exceeded multibyte":      strings.Repeat("é", MaxCommentBody+1),
		"cap exactly with padding":    "  " + strings.Repeat("a", MaxCommentBody) + "  ",
		"cap exceeded with padding":   "  " + strings.Repeat("a", MaxCommentBody+1) + "  ",
		"cap exceeded by interior ws": "a" + strings.Repeat(" ", MaxCommentBody) + "b",
		"interior ws just under cap":  "a" + strings.Repeat(" ", MaxCommentBody-3) + "b",
		"cap exceeded and control":    strings.Repeat("a", MaxCommentBody+1) + vt,
		"control then cap exceeded":   vt + strings.Repeat("a", MaxCommentBody+1),
		"cap exceeded invalid bytes":  strings.Repeat("\xff", MaxCommentBody+1),
		"trailing ws then control":    "finding   " + vt + "   ",
		"content ws content":          "one     two",
	}

	// The five malformed shapes SPEC/MODELS.md § Free-Text UTF-8 Encoding
	// Constraint enumerates, each in the four positions that decide which rule
	// answers for it. They are drawn from the module's shared corpus so this
	// oracle and the rule's own gates cannot end up testing different bytes.
	//
	// The last two shapes are the ones that matter most here: with the malformed
	// bytes BEFORE the cap is reached, a reader that stopped at the first
	// invalid byte would report an encoding failure where the whole-stream
	// pipeline reports the length one, and this comparison is what catches it.
	for _, c := range testenv.MalformedUTF8Corpus() {
		bodies["malformed "+c.Name] = c.Value
		bodies["malformed padded "+c.Name] = "  \n\t" + c.Value + " \r\n  "
		bodies["malformed then over cap "+c.Name] = "abc" + c.Value + strings.Repeat("x", MaxCommentBody)
		bodies["malformed after cap "+c.Name] = strings.Repeat("x", MaxCommentBody) + c.Value
	}
	return bodies
}

// TestReadCommentBodyMatchesWholeStreamPipeline is the differential proof that
// bounding the read changed no verdict: for every input, the streaming reader
// plus the domain rules produce the same supplied/stored/error outcome as
// reading the whole stream and applying the same rules to it.
//
// Each input is also fed through readers that deliver 1, 3 and 7 bytes per call,
// so a multi-byte rune, a forbidden code point, and the cap boundary are all
// crossed at a chunk edge.
func TestReadCommentBodyMatchesWholeStreamPipeline(t *testing.T) {
	t.Parallel()

	for name, raw := range differentialBodies() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := referenceVerdict(raw)

			if got := streamingVerdict(t, strings.NewReader(raw)); got != want {
				t.Errorf("whole-buffer read: got %+v, want %+v", got, want)
			}
			for _, size := range []int{1, 3, 7} {
				got := streamingVerdict(t, &chunkedReader{data: []byte(raw), n: size})
				if got != want {
					t.Errorf("%d-byte chunks: got %+v, want %+v", size, got, want)
				}
			}
		})
	}
}

// TestReadCommentBodyMatchesPipelineOnRandomInput is the same differential
// property over pseudo-random bodies built from the alphabet that matters here:
// ordinary text, every whitespace form, the forbidden control characters, and
// bytes that are not valid UTF-8. The seed is fixed so a failure is reproducible.
func TestReadCommentBodyMatchesPipelineOnRandomInput(t *testing.T) {
	t.Parallel()

	alphabet := []string{
		"a", "b", "Z", "9", ".", " ", "  ", "\t", "\n", "\r", "\v", "\f",
		"\x00", "\x1b", "\x7f", "\u0085", "\u00a0", "é", "監", "\U0001F680",
		"\u202e", "\ufeff", "\x80", "\xff", "\xc0\x80", "\xe2\x82",
		// The two shapes the list above did not already reach: an overlong
		// encoding of a character a filter would look for (0xC0 0xAF is `/`),
		// and a lone surrogate.
		"\xc0\xaf", "\xed\xa0\x80",
	}
	rng := rand.New(rand.NewSource(20260817)) // #nosec G404 -- deterministic test corpus, not cryptography

	for i := 0; i < 2000; i++ {
		var b strings.Builder
		for n := rng.Intn(24); n >= 0; n-- {
			b.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		raw := b.String()

		want := referenceVerdict(raw)
		if got := streamingVerdict(t, strings.NewReader(raw)); got != want {
			t.Fatalf("case %d %q: whole-buffer read got %+v, want %+v", i, raw, got, want)
		}
		if got := streamingVerdict(t, &chunkedReader{data: []byte(raw), n: 1 + rng.Intn(5)}); got != want {
			t.Fatalf("case %d %q: chunked read got %+v, want %+v", i, raw, got, want)
		}
	}
}

// hostileReader emits count bytes of fill without ever holding them, standing in
// for a writer on the other end of a pipe that keeps sending. It records how much
// was actually consumed, which is what proves the reader stops early instead of
// draining whatever it is offered.
type hostileReader struct {
	remaining int
	consumed  int
	fill      byte
}

func (h *hostileReader) Read(p []byte) (int, error) {
	if h.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > h.remaining {
		n = h.remaining
	}
	for i := 0; i < n; i++ {
		p[i] = h.fill
	}
	h.remaining -= n
	h.consumed += n
	return n, nil
}

// TestReadCommentBodyIsBounded pins the property the whole change exists for: an
// oversized body is refused after reading a bounded amount of it, and the value
// retained is bounded too — regardless of how much the writer offers.
//
// Regression test for the unbounded io.ReadAll on the standard-input body path,
// which buffered the entire stream (measured: 512 MiB of input produced 1.27 GB
// of peak RSS) before applying the 4096-character cap.
func TestReadCommentBodyIsBounded(t *testing.T) {
	t.Parallel()

	const offered = 64 << 20 // 64 MiB, ~16000x the largest acceptable body
	const bound = 1 << 20    // generous ceiling: the scanner needs ~32 KiB

	for _, tc := range []struct {
		name string
		fill byte
	}{
		{name: "printable filler", fill: 'a'},
		{name: "invalid utf8 filler", fill: 0xff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &hostileReader{remaining: offered, fill: tc.fill}

			body, err := ReadCommentBody(h)
			if err != nil {
				t.Fatalf("ReadCommentBody: unexpected error %v", err)
			}
			if h.consumed > bound {
				t.Errorf("consumed %d bytes of the %d offered; the read is not bounded", h.consumed, offered)
			}
			if len(body) > bound {
				t.Errorf("retained %d bytes; the returned value is not bounded", len(body))
			}

			// The verdict is still the domain's, and still the documented one.
			if _, verr := ValidateCommentBody(body); !errors.Is(verr, utils.ErrFieldTooLarge) {
				t.Errorf("ValidateCommentBody after a bounded read = %v, want utils.ErrFieldTooLarge", verr)
			}
		})
	}
}

// TestReadCommentBodyBoundsUnboundedWhitespace covers the one shape that cannot
// be bounded by the content alone: a body whose interior whitespace run is
// enormous. The run counts towards the cap, so the body must still be refused,
// and it must be refused without buffering the run.
func TestReadCommentBodyBoundsUnboundedWhitespace(t *testing.T) {
	t.Parallel()

	const spaces = 8 << 20
	raw := io.MultiReader(
		strings.NewReader("a"),
		&hostileReader{remaining: spaces, fill: ' '},
		strings.NewReader("b"),
	)

	body, err := ReadCommentBody(raw)
	if err != nil {
		t.Fatalf("ReadCommentBody: unexpected error %v", err)
	}
	if len(body) > 1<<20 {
		t.Errorf("retained %d bytes for a whitespace-padded body; the run was buffered", len(body))
	}
	if _, verr := ValidateCommentBody(body); !errors.Is(verr, utils.ErrFieldTooLarge) {
		t.Errorf("ValidateCommentBody = %v, want utils.ErrFieldTooLarge: interior whitespace counts towards the cap", verr)
	}
}

// errReader fails on the first read, standing in for a broken pipe or an
// unreadable descriptor.
type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// TestReadCommentBodyReportsIOFailure pins that a stream failure stays an I/O
// failure of the process (utils.ErrDatabase, exit code 1) and is never reported
// as bad user input.
func TestReadCommentBodyReportsIOFailure(t *testing.T) {
	t.Parallel()

	_, err := ReadCommentBody(errReader{err: errors.New("read /dev/stdin: is a directory")})
	if !errors.Is(err, utils.ErrDatabase) {
		t.Fatalf("ReadCommentBody error = %v, want utils.ErrDatabase", err)
	}
	if errors.Is(err, utils.ErrValidation) || errors.Is(err, utils.ErrFieldTooLarge) {
		t.Errorf("an I/O failure must not be reported as a validation failure: %v", err)
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("error %q does not carry the underlying cause", err)
	}
}
