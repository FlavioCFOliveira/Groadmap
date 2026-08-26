package aihelp

import (
	"bytes"
	"testing"
)

// internal/aihelp — regression suite for the trailing hint emitter.
//
// SPEC/HELP.md § Stderr part order defines part 4 as "One blank line,
// then the AI-agent hint line", present "unless a suppression rule
// below applies". The two words that matter are "One blank line,
// then": the separator and the hint are one part, so a suppression
// rule that removes the hint must remove the separator with it.
//
// The defect these tests exist to prevent had the separator written by
// the CALLER, immediately before the emitter. The emitter was then a
// no-op under the deduplication rule (AI_AGENT=1 already put the hint
// at the top of stderr), so stderr ended on a blank line that
// introduced nothing — a part 4 that was half present, which the
// contract has no way to describe.
//
// The fix moves the separator inside the same sync.Once as the hint.
// TestEmitTrailingHintOnce_SuppressedWritesNothingAtAll is the test
// that fails if it ever moves back out.

// TestEmitTrailingHintOnce_OutputShape pins the bytes of the trailing
// form when nothing has consumed the Once yet: blank line, hint line,
// blank line.
func TestEmitTrailingHintOnce_OutputShape(t *testing.T) {
	resetHintBetween(t)
	var buf bytes.Buffer
	EmitTrailingHintOnce(&buf, testHint)
	want := "\n" + testHint + "\n\n"
	if got := buf.String(); got != want {
		t.Errorf("EmitTrailingHintOnce produced %q, want %q", got, want)
	}
}

// TestEmitTrailingHintOnce_SuppressedWritesNothingAtAll is the guard on
// the defect itself.
//
// The scenario is the deduplication rule of SPEC/HELP.md § AI_AGENT
// environment variable: the leading form has already emitted the hint
// at the top of stderr, so the trailing form must be suppressed. It
// must then write ZERO bytes — not the separating blank line either.
//
// The assertion is on the byte count rather than on a substring,
// because the orphan this test exists to catch is not the hint text
// but the single "\n" that used to precede it.
func TestEmitTrailingHintOnce_SuppressedWritesNothingAtAll(t *testing.T) {
	resetHintBetween(t)

	// The leading form runs first, exactly as main() does under
	// AI_AGENT=1, and consumes the process's single emission.
	var leading bytes.Buffer
	EmitHintOnce(&leading, testHint)
	if leading.String() != testHint+"\n\n" {
		t.Fatalf("precondition failed: leading form wrote %q", leading.String())
	}

	// The error path now reaches the trailing form. It is suppressed.
	var trailing bytes.Buffer
	EmitTrailingHintOnce(&trailing, testHint)
	if trailing.Len() != 0 {
		t.Errorf("suppressed trailing hint wrote %d bytes (%q), want zero; the separating "+
			"blank line must be governed by the same condition as the hint it introduces "+
			"(SPEC/HELP.md § Stderr part order)", trailing.Len(), trailing.String())
	}
}

// TestEmitTrailingHintOnce_SuppressesTheLeadingForm covers the shared
// Once in the other direction. It cannot arise in today's CLI (main()
// runs the leading form first, at process entry), but the guard is
// what keeps "at most one hint per process" a property of the package
// rather than of the call order in main().
func TestEmitTrailingHintOnce_SuppressesTheLeadingForm(t *testing.T) {
	resetHintBetween(t)

	var trailing bytes.Buffer
	EmitTrailingHintOnce(&trailing, testHint)
	if trailing.Len() == 0 {
		t.Fatal("precondition failed: the first trailing emission wrote nothing")
	}

	var leading bytes.Buffer
	EmitHintOnce(&leading, testHint)
	if leading.Len() != 0 {
		t.Errorf("leading form wrote %q after the trailing form had already emitted; the two "+
			"emitters share one sync.Once and must dedup against each other", leading.String())
	}
}

// TestEmitTrailingHintOnce_RepeatedCallsStayNoOps mirrors the
// exactly-once guarantee the leading form already carries.
func TestEmitTrailingHintOnce_RepeatedCallsStayNoOps(t *testing.T) {
	resetHintBetween(t)
	var buf bytes.Buffer

	EmitTrailingHintOnce(&buf, testHint)
	first := buf.String()

	EmitTrailingHintOnce(&buf, testHint)
	EmitTrailingHintOnce(&buf, testHint)
	if got := buf.String(); got != first {
		t.Errorf("repeated calls grew the buffer: first=%q now=%q", first, got)
	}
}

// TestEmitTrailingHintOnce_NilWriterKeepsTheOnceUnspent pins the
// nil-writer carve-out for the trailing form.
//
// The nil check sits BEFORE the Once deliberately: a caller with
// nowhere to write must not spend the process's single emission and
// silence the caller that does have a writer. Losing this would turn a
// nil writer anywhere into a silently missing hint everywhere.
func TestEmitTrailingHintOnce_NilWriterKeepsTheOnceUnspent(t *testing.T) {
	resetHintBetween(t)

	EmitTrailingHintOnce(nil, testHint)

	var buf bytes.Buffer
	EmitTrailingHintOnce(&buf, testHint)
	if got, want := buf.String(), "\n"+testHint+"\n\n"; got != want {
		t.Errorf("after a nil-writer call the real writer got %q, want %q", got, want)
	}
}
