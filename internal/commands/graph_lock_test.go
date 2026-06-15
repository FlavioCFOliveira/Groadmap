package commands

import (
	"errors"
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
