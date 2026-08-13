package commands

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// busyMessage is the contention diagnostic that acquireGraphWriteLock must
// produce on every platform. SPEC/GRAPH.md § Concurrency and Recovery rule 2
// and SPEC/IMPLEMENTATION.md § Graph Store Concurrency require a second writer
// to fail with this outcome rather than wait, so the text is part of the
// contract that the Unix and Windows lock primitives share.
const busyMessage = "graph store is busy: a concurrent write is in progress"

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

	if !strings.Contains(err.Error(), busyMessage) {
		t.Errorf("contention message = %q, want it to contain %q", err.Error(), busyMessage)
	}

	// After releasing the first lock, it must be acquirable again.
	release1()
	release3, err := acquireGraphWriteLock(dir)
	if err != nil {
		t.Fatalf("lock not reacquirable after release: %v", err)
	}
	release3()
}

// TestAcquireGraphWriteLock_ContentionFailsFast pins the half of the write-lock
// contract that a port is most likely to get wrong: the lock is NON-BLOCKING,
// so a second writer fails immediately instead of waiting for the first to
// finish. flock(2) only behaves that way because of LOCK_NB, and LockFileEx
// only because of LOCKFILE_FAIL_IMMEDIATELY — both are opt-in, and dropping
// either one turns a fast, well-diagnosed failure into a hang that no
// assertion on the returned error would ever catch.
//
// The contended call is made on a separate goroutine so that a blocking
// implementation fails this test with a clear diagnostic rather than
// deadlocking until the whole test binary times out.
func TestAcquireGraphWriteLock_ContentionFailsFast(t *testing.T) {
	dir := t.TempDir()

	release, err := acquireGraphWriteLock(dir)
	if err != nil {
		t.Fatalf("first lock acquisition failed: %v", err)
	}
	defer release()

	type attempt struct {
		release func()
		err     error
	}
	done := make(chan attempt, 1)
	go func() {
		r, err := acquireGraphWriteLock(dir)
		done <- attempt{release: r, err: err}
	}()

	// The bound is generous: the point is to distinguish "returned promptly"
	// from "waited for the holder", not to measure the syscall.
	const bound = 30 * time.Second
	select {
	case got := <-done:
		if got.err == nil {
			got.release()
			t.Fatal("contended acquisition succeeded; the lock is not exclusive")
		}
		if !errors.Is(got.err, utils.ErrDatabase) {
			t.Errorf("contention must surface as utils.ErrDatabase (exit 1), got: %v", got.err)
		}
		if !strings.Contains(got.err.Error(), busyMessage) {
			t.Errorf("contention message = %q, want it to contain %q", got.err.Error(), busyMessage)
		}
	case <-time.After(bound):
		t.Fatalf("contended acquisition still blocked after %s; the lock must fail immediately, "+
			"never wait (SPEC/GRAPH.md § Concurrency and Recovery rule 2)", bound)
	}
}

// TestAcquireGraphWriteLock_ContentionDoesNotLeakHandles guards the failure
// path's cleanup: acquireGraphWriteLock opens the lock file before it tries to
// lock it, so a contended attempt that returned without closing that handle
// would leak one descriptor per attempt.
//
// The check is indirect by necessity — Go exposes no portable open-descriptor
// count — so it works by exhaustion: with a leak, a process whose descriptor
// limit is the common 1024 stops being able to open the lock file part-way
// through, and the reported error changes from the contention diagnostic to an
// open failure. On a host with a very high limit the loop cannot prove the
// absence of a leak, but it still costs almost nothing and it fails loudly
// wherever the limit is ordinary.
func TestAcquireGraphWriteLock_ContentionDoesNotLeakHandles(t *testing.T) {
	dir := t.TempDir()

	release, err := acquireGraphWriteLock(dir)
	if err != nil {
		t.Fatalf("first lock acquisition failed: %v", err)
	}
	defer release()

	const attempts = 2048
	for i := range attempts {
		r, err := acquireGraphWriteLock(dir)
		if err == nil {
			r()
			t.Fatalf("attempt %d acquired a held lock; the lock is not exclusive", i)
		}
		if !strings.Contains(err.Error(), busyMessage) {
			t.Fatalf("attempt %d failed for the wrong reason (descriptor leak on the contention path?): %v", i, err)
		}
	}
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
