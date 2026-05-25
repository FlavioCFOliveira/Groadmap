package aihelp

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"
)

// The hint text is intentionally arbitrary in these tests: the
// production callers pass commands.AIBannerLine, but importing that
// package here would create a cycle (commands → aihelp). The unit
// tests verify the dedup mechanics; SPEC-text correctness is verified
// by the cmd/rmp integration tests (which CAN see both packages).
const testHint = "AI agents: hint line"

// resetHintBetween is a tiny helper to make the test body read as
// "given a clean Once, when..." instead of repeating the reset call
// inline. Production code never calls ResetHintForTesting; the test
// suite is the only legitimate caller.
func resetHintBetween(t *testing.T) {
	t.Helper()
	ResetHintForTesting()
}

func TestEmitHintOnce_WritesExactlyOnce(t *testing.T) {
	resetHintBetween(t)
	var buf bytes.Buffer

	EmitHintOnce(&buf, testHint)
	first := buf.String()
	if !strings.Contains(first, testHint) {
		t.Fatalf("first call did not write hint: %q", first)
	}

	// Second call must be a no-op: the buffer length cannot grow.
	EmitHintOnce(&buf, testHint)
	if buf.String() != first {
		t.Errorf("second call wrote extra bytes: first=%q after-second=%q", first, buf.String())
	}

	// Third call (sanity): still nothing.
	EmitHintOnce(&buf, testHint)
	if buf.String() != first {
		t.Errorf("third call wrote extra bytes: %q", buf.String())
	}
}

func TestEmitHintOnce_OutputShape(t *testing.T) {
	resetHintBetween(t)
	var buf bytes.Buffer
	EmitHintOnce(&buf, testHint)
	// SPEC mandates the hint line is followed by exactly one blank
	// line (so the consumer can rely on a stable separator before any
	// other stderr content the run produces).
	want := testHint + "\n\n"
	if got := buf.String(); got != want {
		t.Errorf("EmitHintOnce produced %q, want %q", got, want)
	}
}

func TestEmitHintOnce_NilWriter(t *testing.T) {
	resetHintBetween(t)
	// Passing a nil writer must not panic and must not consume the
	// sync.Once budget — a subsequent call with a real writer should
	// still emit the hint.
	EmitHintOnce(nil, testHint)
	var buf bytes.Buffer
	EmitHintOnce(&buf, testHint)
	if !strings.Contains(buf.String(), testHint) {
		t.Errorf("real writer after nil writer received no hint: %q", buf.String())
	}
}

func TestEmitHintOnce_Concurrent(t *testing.T) {
	resetHintBetween(t)
	var buf bytes.Buffer
	var mu sync.Mutex // serialise buffer writes, NOT the Once.
	const goroutines = 32

	writer := writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.Write(p)
	})

	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			EmitHintOnce(writer, testHint)
		}()
	}
	close(start)
	wg.Wait()

	// Despite N goroutines racing, only one emission must reach the
	// buffer. The hint text appears exactly once.
	if got := strings.Count(buf.String(), testHint); got != 1 {
		t.Errorf("hint appeared %d times under concurrent emission, want 1; buf=%q", got, buf.String())
	}
}

func TestResetHintForTesting_AllowsReEmission(t *testing.T) {
	resetHintBetween(t)
	var buf bytes.Buffer

	EmitHintOnce(&buf, testHint)
	EmitHintOnce(&buf, testHint)
	if strings.Count(buf.String(), testHint) != 1 {
		t.Fatalf("baseline: expected 1 emission, got %d", strings.Count(buf.String(), testHint))
	}

	ResetHintForTesting()
	EmitHintOnce(&buf, testHint)
	if got := strings.Count(buf.String(), testHint); got != 2 {
		t.Errorf("after reset: expected 2 total emissions, got %d", got)
	}
}

func TestIsAIAgentEnvActive(t *testing.T) {
	// Per SPEC/HELP.md § AI_AGENT environment variable, ONLY the exact
	// string "1" activates the hint. Every other value (including the
	// commonly-truthy "true"/"yes" and any whitespace variants) leaves
	// the CLI silent. This table guards against well-meaning future
	// drift toward a more permissive parser.
	cases := []struct {
		name string
		val  string
		want bool
		set  bool // false = unset the variable
	}{
		{"unset", "", false, false},
		{"empty-string-set", "", false, true},
		{"exact-1", "1", true, true},
		{"zero", "0", false, true},
		{"true-lower", "true", false, true},
		{"true-upper", "TRUE", false, true},
		{"yes", "yes", false, true},
		{"no", "no", false, true},
		{"false", "false", false, true},
		{"leading-space", " 1", false, true},
		{"trailing-space", "1 ", false, true},
		{"two-digit", "11", false, true},
		{"leading-zero", "01", false, true},
		{"non-digit", "on", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv("AI_AGENT", tc.val)
			} else {
				// t.Setenv handles cleanup of preexisting values too:
				// set to a sentinel, then unset, restoring at teardown.
				t.Setenv("AI_AGENT", "preexisting")
				if err := unsetAIAgent(); err != nil {
					t.Fatalf("failed to unset env: %v", err)
				}
			}
			if got := IsAIAgentEnvActive(); got != tc.want {
				t.Errorf("IsAIAgentEnvActive() with AI_AGENT=%q set=%v = %v, want %v",
					tc.val, tc.set, got, tc.want)
			}
		})
	}
}

func TestHintWasEmitted(t *testing.T) {
	resetHintBetween(t)
	// HintWasEmitted itself consumes the Once on first call (see
	// godoc). So after a clean reset:
	//   - HintWasEmitted() returns false (and triggers the Once).
	//   - A subsequent EmitHintOnce is a no-op.
	if HintWasEmitted() {
		t.Error("freshly reset Once should report false on first probe")
	}
	var buf bytes.Buffer
	EmitHintOnce(&buf, testHint)
	if buf.Len() != 0 {
		t.Errorf("EmitHintOnce after HintWasEmitted probe wrote bytes: %q", buf.String())
	}
}

// ---- helpers ----

// writerFunc adapts a function to io.Writer for table-driven tests
// that need a custom Write hook without standing up a full type.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// unsetAIAgent clears the AI_AGENT environment variable. t.Setenv
// does not expose an "unset" mode (its design intent is to set-and-
// restore), so the few test cases that need the "unset" state use
// this helper in tandem with a prior t.Setenv call: the t.Setenv
// arranges restoration of the original value at test teardown, and
// this call clears the variable for the duration of the test body.
func unsetAIAgent() error { return os.Unsetenv("AI_AGENT") }
