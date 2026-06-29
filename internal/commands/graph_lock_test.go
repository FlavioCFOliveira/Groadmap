package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestAcquireGraphWriteLock_MutualExclusion is a regression gate for finding
// #39: the exclusive graph write lock must prevent two writers from holding it
// at once, and contention must surface as utils.ErrDatabase (exit 1) — never a
// silent overlap that would let a stale-snapshot checkpoint drop a committed
// write. Releasing the lock must make it acquirable again.
func TestAcquireGraphWriteLock_MutualExclusion(t *testing.T) {
	dir := t.TempDir()

	release1, err := acquireGraphWriteLock(dir)
	if err != nil {
		t.Fatalf("first lock acquisition failed: %v", err)
	}

	// A second acquisition while the first is held must fail with ErrDatabase.
	release2, err := acquireGraphWriteLock(dir)
	if err == nil {
		release2()
		release1()
		t.Fatal("second concurrent lock acquisition succeeded; expected contention error")
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("contention must surface as utils.ErrDatabase (exit 1), got: %v", err)
	}

	// After releasing the first lock, it must be acquirable again.
	release1()
	release3, err := acquireGraphWriteLock(dir)
	if err != nil {
		t.Fatalf("lock not reacquirable after release: %v", err)
	}
	release3()
}

// TestReadQuery_FlagHandling is a regression gate for findings #26-#28:
// readQuery must reject a --query with no value (or whose "value" is the next
// flag) with exit 2, must reject unknown flags with exit 2, and must accept a
// well-formed --query value. The error cases all return before stdin is read.
func TestReadQuery_FlagHandling(t *testing.T) {
	t.Run("query flag with no value", func(t *testing.T) {
		_, err := readQuery([]string{"--query"})
		if !errors.Is(err, utils.ErrRequired) {
			t.Errorf("--query with no value must be ErrRequired (exit 2), got: %v", err)
		}
	})

	t.Run("query value swallowed by following flag", func(t *testing.T) {
		_, err := readQuery([]string{"--query", "--bogus"})
		if !errors.Is(err, utils.ErrRequired) {
			t.Errorf("--query followed by a flag must be ErrRequired (exit 2), got: %v", err)
		}
	})

	t.Run("unknown flag rejected", func(t *testing.T) {
		_, err := readQuery([]string{"--bogus", "value"})
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("unknown flag must be ErrInvalidInput (exit 2), got: %v", err)
		}
	})

	t.Run("unexpected positional rejected", func(t *testing.T) {
		_, err := readQuery([]string{"MATCH (n) RETURN n"})
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("positional query must be ErrInvalidInput (exit 2), got: %v", err)
		}
	})

	t.Run("valid query accepted", func(t *testing.T) {
		q, err := readQuery([]string{"--query", "MATCH (n) RETURN n"})
		if err != nil {
			t.Fatalf("valid --query must succeed, got: %v", err)
		}
		if q != "MATCH (n) RETURN n" {
			t.Errorf("query = %q, want %q", q, "MATCH (n) RETURN n")
		}
	})
}

// TestReadQuery_NegativeNumericValue is a regression gate for finding #81:
// a --query value that begins with '-' followed by a digit or a decimal point
// is a negative numeric literal — a legitimate query value — and must NOT be
// rejected as a missing value. It is accepted verbatim and handed to the engine
// for Cypher validation (SPEC/GRAPH.md § Cypher Input Source and Precedence,
// precedence rule 4).
func TestReadQuery_NegativeNumericValue(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"negative integer literal", []string{"--query", "-1 RETURN 1"}, "-1 RETURN 1"},
		{"negative decimal literal", []string{"--query", "-0.5"}, "-0.5"},
		{"leading decimal point literal", []string{"-q", "-.5 AS x"}, "-.5 AS x"},
		{"bare dash value", []string{"--query", "-"}, "-"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := readQuery(tc.args)
			if err != nil {
				t.Fatalf("negative numeric --query value must be accepted, got: %v", err)
			}
			if q != tc.want {
				t.Errorf("query = %q, want %q", q, tc.want)
			}
		})
	}
}

// TestReadQuery_ShortFlagValueRejected is a regression gate for finding #81:
// a genuine flag immediately following --query (for example "-q" or "-x") is
// flag-like and therefore NOT swallowed as the query value; the value is
// treated as absent and the command fails with ErrRequired (exit 2).
func TestReadQuery_ShortFlagValueRejected(t *testing.T) {
	for _, tok := range []string{"-q", "-x", "--roadmap"} {
		t.Run(tok, func(t *testing.T) {
			_, err := readQuery([]string{"--query", tok})
			if !errors.Is(err, utils.ErrRequired) {
				t.Errorf("--query followed by flag %q must be ErrRequired (exit 2), got: %v", tok, err)
			}
		})
	}
}

// TestReadQuery_DefaultBranchClassification is a regression gate for finding
// #81: only genuine flags ("--…" or "-"+letter) are reported as an "unknown
// flag"; a stray "-1" positional is reported as an "unexpected argument".
// Both map to ErrInvalidInput (exit 2), but the message must not mislabel a
// numeric positional as a flag.
func TestReadQuery_DefaultBranchClassification(t *testing.T) {
	t.Run("genuine unknown flag", func(t *testing.T) {
		_, err := readQuery([]string{"-x", "value"})
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Fatalf("unknown flag must be ErrInvalidInput, got: %v", err)
		}
		if !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("a genuine flag must be reported as an unknown flag, got: %v", err)
		}
	})

	t.Run("stray numeric positional", func(t *testing.T) {
		_, err := readQuery([]string{"-1 RETURN 1"})
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Fatalf("stray positional must be ErrInvalidInput, got: %v", err)
		}
		if !strings.Contains(err.Error(), "unexpected argument") {
			t.Errorf("a numeric positional must be reported as an unexpected argument, not a flag, got: %v", err)
		}
	})
}
