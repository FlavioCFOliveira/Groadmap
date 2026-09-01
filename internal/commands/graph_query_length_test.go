// Package commands — regression gates for the maximum Cypher query length and
// the bounded standard-input read that enforces it (SPEC/GRAPH.md § Maximum
// Query Length, § Bounded Standard-Input Read, § Standard Input That Supplies No
// Query; acceptance criteria 40 and 41).
//
// The defect these close had two halves, and they are refused with DIFFERENT
// exit codes on purpose:
//
//   - a producer that writes too much. The query read from standard input was
//     io.ReadAll with no bound, so 256 MiB offered to `rmp graph execute` reached
//     867 MB of resident memory and 15.9 s of wall time before anything rejected
//     it. That is now a validation failure, exit code 6.
//   - a producer that writes NOTHING. The same unbounded read, given a terminal,
//     waited for a query nobody was going to type: an invocation that omitted
//     --query once hung for roughly forty minutes, printing nothing. That is now
//     a missing required parameter, exit code 2.
//
// Every test below therefore asserts the SENTINEL as well as the message, and
// several assert that the OTHER sentinel does not hold, because collapsing the
// two classes into one is the specific regression the specification forbids.
package commands

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The two refusals, spelled out as the user reads them. They are literals rather
// than values built from maxQueryBytes so that changing the maximum — or the
// wording — fails this file instead of quietly agreeing with itself.
const (
	tooLongMessage    = "validation error: query exceeds maximum length of 1048576 bytes"
	noQueryMessage    = "required parameter missing: no query supplied"
	specifiedMaxBytes = 1048576
)

// assertTooLong checks the over-long refusal completely: the validation class
// (exit code 6), the exact message, and that it is NOT the missing-parameter
// class (exit code 2).
func assertTooLong(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("a query longer than the maximum must be refused, got no error")
	}
	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf("over-long query must wrap utils.ErrValidation (exit 6), got: %v", err)
	}
	if errors.Is(err, utils.ErrRequired) {
		t.Errorf("over-long query must NOT be the missing-parameter class (exit 2): "+
			"the two exit codes are deliberately different; got: %v", err)
	}
	if err.Error() != tooLongMessage {
		t.Errorf("message = %q, want %q", err.Error(), tooLongMessage)
	}
}

// assertNoQuery checks the missing-query refusal completely: the required class
// (exit code 2), the exact message, and that it is NOT the validation class
// (exit code 6).
func assertNoQuery(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("standard input that supplies no query must be refused, got no error")
	}
	if !errors.Is(err, utils.ErrRequired) {
		t.Errorf("no query supplied must wrap utils.ErrRequired (exit 2), got: %v", err)
	}
	if errors.Is(err, utils.ErrValidation) {
		t.Errorf("no query supplied must NOT be the validation class (exit 6): "+
			"the two exit codes are deliberately different; got: %v", err)
	}
	if err.Error() != noQueryMessage {
		t.Errorf("message = %q, want %q", err.Error(), noQueryMessage)
	}
}

// TestMaxQueryBytesIsTheSpecifiedMaximum pins the constant to the number
// SPEC/GRAPH.md publishes. Everything else in this file describes behaviour AT
// the boundary; this states where the boundary is, so a change to the constant
// is a deliberate act rather than a silent one.
func TestMaxQueryBytesIsTheSpecifiedMaximum(t *testing.T) {
	if maxQueryBytes != specifiedMaxBytes {
		t.Errorf("maxQueryBytes = %d, want %d (1 MiB, SPEC/GRAPH.md § Maximum Query Length)",
			maxQueryBytes, specifiedMaxBytes)
	}
}

// TestReadQueryFlagEnforcesTheMaximum is the half of the maximum that is easy to
// forget: the cap belongs to the QUERY, not to the standard-input reader, so it
// must apply to --query too. A cap enforced at one door only would refuse in one
// place what it accepts in the other, which is not a maximum at all
// (SPEC/GRAPH.md § Maximum Query Length rule 2).
func TestReadQueryFlagEnforcesTheMaximum(t *testing.T) {
	t.Run("one byte over the maximum is refused", func(t *testing.T) {
		_, err := readQuery([]string{"--query", strings.Repeat("a", maxQueryBytes+1)})
		assertTooLong(t, err)
	})

	t.Run("exactly the maximum is accepted", func(t *testing.T) {
		value := strings.Repeat("a", maxQueryBytes)
		got, err := readQuery([]string{"--query", value})
		if err != nil {
			t.Fatalf("a query of exactly the maximum must be accepted, got: %v", err)
		}
		if len(got) != maxQueryBytes {
			t.Errorf("returned query is %d bytes, want %d", len(got), maxQueryBytes)
		}
	})

	t.Run("the refusal precedes the trim", func(t *testing.T) {
		// An over-long value made entirely of whitespace trims to nothing, so a
		// reader that trimmed first would report the missing-parameter class
		// (exit 2) for a value the maximum refuses (exit 6). The length is
		// counted over the bytes AS SUPPLIED (SPEC/GRAPH.md § Cypher Input Source
		// and Precedence, rule 5).
		_, err := readQuery([]string{"--query", strings.Repeat(" ", maxQueryBytes+1)})
		assertTooLong(t, err)
	})
}

// TestTheMaximumCountsBytesNotCharacters is the unit gate. The comment body's
// cap counts 4096 CHARACTERS; this one counts 1048576 BYTES, and the difference
// is deliberate: a body is stored text whose length its author reads back, while
// a query is an instruction that is executed and discarded, whose maximum exists
// against memory. A multi-byte query proves which unit is in force, because the
// two answers disagree about it.
func TestTheMaximumCountsBytesNotCharacters(t *testing.T) {
	// "é" is two bytes in UTF-8. This value is one CHARACTER over half the
	// maximum but one BYTE over the whole of it.
	overByBytes := strings.Repeat("é", maxQueryBytes/2+1)
	if len(overByBytes) != maxQueryBytes+2 {
		t.Fatalf("test input is %d bytes, want %d", len(overByBytes), maxQueryBytes+2)
	}
	if runes := len([]rune(overByBytes)); runes > maxQueryBytes {
		t.Fatalf("test input is %d characters, which is not under the maximum: "+
			"it would not distinguish the two units", runes)
	}

	t.Run("from the flag", func(t *testing.T) {
		_, err := readQuery([]string{"--query", overByBytes})
		assertTooLong(t, err)
	})

	t.Run("from standard input", func(t *testing.T) {
		_, err := readQueryStream(strings.NewReader(overByBytes))
		assertTooLong(t, err)
	})

	t.Run("a multi-byte query within the maximum is accepted", func(t *testing.T) {
		// Half the maximum in characters, exactly the maximum in bytes: accepted,
		// which is what makes the refusal above about bytes rather than about
		// multi-byte text.
		atMax := strings.Repeat("é", maxQueryBytes/2)
		if len(atMax) != maxQueryBytes {
			t.Fatalf("test input is %d bytes, want %d", len(atMax), maxQueryBytes)
		}
		got, err := readQueryStream(strings.NewReader(atMax))
		if err != nil {
			t.Fatalf("a multi-byte query of exactly the maximum must be accepted, got: %v", err)
		}
		if len(got) != maxQueryBytes {
			t.Errorf("returned query is %d bytes, want %d", len(got), maxQueryBytes)
		}
	})
}

// TestReadQueryStreamEnforcesTheMaximum covers the standard-input door at the
// boundary itself.
func TestReadQueryStreamEnforcesTheMaximum(t *testing.T) {
	t.Run("one byte over the maximum is refused", func(t *testing.T) {
		_, err := readQueryStream(strings.NewReader(strings.Repeat("a", maxQueryBytes+1)))
		assertTooLong(t, err)
	})

	t.Run("exactly the maximum is accepted", func(t *testing.T) {
		got, err := readQueryStream(strings.NewReader(strings.Repeat("a", maxQueryBytes)))
		if err != nil {
			t.Fatalf("a stream of exactly the maximum must be accepted, got: %v", err)
		}
		if len(got) != maxQueryBytes {
			t.Errorf("returned query is %d bytes, want %d", len(got), maxQueryBytes)
		}
	})

	t.Run("a realistic multi-kilobyte query is accepted", func(t *testing.T) {
		// The bound must refuse only what the maximum forbids. A graph bootstrap
		// script of a few hundred kilobytes is ordinary work.
		statement := "MERGE (c:Component {key:'internal/commands/graph.go'})\n"
		script := strings.Repeat(statement, 6000)
		if len(script) < 300_000 || len(script) > maxQueryBytes {
			t.Fatalf("test script is %d bytes; it must be hundreds of kilobytes and "+
				"under the maximum to prove the bound is not too tight", len(script))
		}
		got, err := readQueryStream(strings.NewReader(script))
		if err != nil {
			t.Fatalf("a %d-byte query must be accepted, got: %v", len(script), err)
		}
		if got != strings.TrimSpace(script) {
			t.Error("the accepted query must be the script as supplied, trimmed")
		}
	})
}

// countingReader supplies a fixed byte forever, up to a hard ceiling, and
// records how many bytes it was asked for.
//
// The ceiling exists so that a regression to an unbounded read FAILS this test
// instead of hanging it: an implementation that drained the stream would be
// served the whole ceiling and the assertion on served would catch it, whereas
// an endless reader would spin until the test binary's own timeout.
type countingReader struct {
	served  int
	ceiling int
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.served >= r.ceiling {
		return 0, io.EOF
	}
	n := len(p)
	if remaining := r.ceiling - r.served; n > remaining {
		n = remaining
	}
	for i := range n {
		p[i] = 'a'
	}
	r.served += n
	return n, nil
}

// TestReadQueryStreamStopsOneByteBeyondTheMaximum is the memory bound itself,
// asserted exactly rather than approximately.
//
// One byte past the maximum settles the verdict, because no later byte can bring
// the count back down; every byte read after that would be buffering at the
// writer's discretion, which is the defect. The reader below offers 64 MiB and
// the assertion is that precisely 1048577 of them were ever asked for.
func TestReadQueryStreamStopsOneByteBeyondTheMaximum(t *testing.T) {
	source := &countingReader{ceiling: 64 << 20}

	_, err := readQueryStream(source)
	assertTooLong(t, err)

	if source.served != maxQueryBytes+1 {
		t.Errorf("the reader consumed %d bytes of the %d offered, want exactly %d "+
			"(the maximum plus the one byte that settles the verdict): the "+
			"standard-input query read is no longer bounded",
			source.served, source.ceiling, maxQueryBytes+1)
	}
}

// TestReadQueryStreamDoesNotLookPastTheMaximumForWhitespace pins the ONE
// deliberate difference from the comment body's bounded read, which
// SPEC/GRAPH.md § Bounded Standard-Input Read says must not be "aligned" away.
//
// That read looks past its cap for trailing whitespace, so its verdict is
// exactly the verdict a read-to-EOF implementation would reach after trimming.
// This one does not: the maximum counts the bytes standard input SUPPLIES, so a
// stream of exactly the maximum followed by whitespace is refused even though
// trimming that whitespace would have brought it to the maximum. Making the two
// reads agree here would mean reading past the bound, which is the thing the
// bound exists to prevent.
func TestReadQueryStreamDoesNotLookPastTheMaximumForWhitespace(t *testing.T) {
	padded := strings.Repeat("a", maxQueryBytes) + "\n"
	_, err := readQueryStream(strings.NewReader(padded))
	assertTooLong(t, err)
}

// TestReadQueryStreamRefusesAStreamThatSuppliesNoQuery covers two of the three
// conditions of SPEC/GRAPH.md § Standard Input That Supplies No Query — the
// stream that is already at end of stream, and the stream that carries only
// whitespace. The third, a terminal, is refused before any read and is proved in
// graph_query_terminal_linux_test.go and end-to-end.
func TestReadQueryStreamRefusesAStreamThatSuppliesNoQuery(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "already at end of stream", input: ""},
		{name: "spaces alone", input: "     "},
		{name: "a newline alone", input: "\n"},
		{name: "mixed whitespace", input: " \t\r\n \n\t "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readQueryStream(strings.NewReader(tc.input))
			assertNoQuery(t, err)
		})
	}
}

// TestReadQueryStreamTrimsAfterTheLengthCheck covers the ordinary path: a query
// that fits comes back trimmed, with its interior untouched.
func TestReadQueryStreamTrimsAfterTheLengthCheck(t *testing.T) {
	got, err := readQueryStream(strings.NewReader("\n  MATCH (n:Component)\nRETURN n.key  \n"))
	if err != nil {
		t.Fatalf("a well-formed query on standard input must be accepted, got: %v", err)
	}
	if want := "MATCH (n:Component)\nRETURN n.key"; got != want {
		t.Errorf("query = %q, want %q", got, want)
	}
}

// TestReadQueryStdinRefusesAStreamAtEndOfStream drives the standard-input entry
// point rather than the stream reader, so it covers the terminal check's
// fall-through: os.DevNull is a character device but NOT an interactive
// terminal, so it must be READ — and then refused for being empty — rather than
// refused for being a terminal. Both refusals carry the same message and exit
// code, which is precisely why the path has to be checked with something other
// than the message.
func TestReadQueryStdinRefusesAStreamAtEndOfStream(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	defer func() { _ = devNull.Close() }()

	_, readErr := readQueryStdin(devNull)
	assertNoQuery(t, readErr)
}

// TestReadQueryStdinReadsANonTerminalStream proves the terminal check does not
// refuse what it must not: a pipe carrying a query is read normally.
func TestReadQueryStdinReadsANonTerminalStream(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating a pipe: %v", err)
	}
	defer func() { _ = read.Close() }()

	const query = "MATCH (m:Memory) RETURN m.key"
	go func() {
		defer func() { _ = write.Close() }()
		_, _ = write.WriteString(query)
	}()

	got, err := readQueryStdin(read)
	if err != nil {
		t.Fatalf("a query arriving down a pipe must be accepted, got: %v", err)
	}
	if got != query {
		t.Errorf("query = %q, want %q", got, query)
	}
}

// TestReadQueryReadsTheProcessStandardInput is the wiring gate: it proves
// readQuery falls back to os.Stdin when --query is absent, so the bound and the
// refusals proved above are the ones a real invocation reaches.
func TestReadQueryReadsTheProcessStandardInput(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating a pipe: %v", err)
	}
	defer func() { _ = read.Close() }()

	original := os.Stdin
	os.Stdin = read
	defer func() { os.Stdin = original }()

	const query = "MATCH (t:Task {key:'181'}) RETURN t.title"
	go func() {
		defer func() { _ = write.Close() }()
		_, _ = write.WriteString(query + "\n")
	}()

	got, err := readQuery(nil)
	if err != nil {
		t.Fatalf("readQuery with no --query must read standard input, got: %v", err)
	}
	if got != query {
		t.Errorf("query = %q, want %q", got, query)
	}
}
