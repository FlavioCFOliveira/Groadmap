// Regression fence for defect #294 at the graph write-ahead-log call site.
//
// The site moved here with the store open sequence it belongs to (rmp task
// #375); it was internal/commands's openWALWriter, and internal/web held a
// second copy of the same function. Both are gone, and this is the one that
// remains.
//
// openWAL used to own a private copy of the bounded backoff, taken from
// constants named walRetryInitial/walRetryMax/walRetryAttempts whose comment
// claimed to "mirror the SQLite bounded exponential-backoff specified in
// IMPLEMENTATION.md § Concurrency Model". It did not mirror it: the loop read
// the "5" as attempts and guarded its sleep with `attempt < walRetryAttempts-1`,
// so a contended graph write gave up after four waits (1500 ms) instead of five
// (2500 ms), and the 1000 ms rung was never reached.
//
// The contention here is real rather than simulated: a WAL writer is held open
// on the path, so wal.Open returns ErrWALLocked on every attempt, which is
// precisely the failure the retry exists for. Every figure comes from
// internal/backoff, so these assertions follow the policy instead of restating
// it.
package graphstore

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/store/wal"

	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// heldWALPath returns a WAL path whose directory lock is already held for the
// duration of the test, so every further wal.Open on it fails with
// ErrWALLocked.
func heldWALPath(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "graph.wal")
	holder, err := wal.Open(path)
	if err != nil {
		t.Fatalf("opening the WAL writer that holds the lock: %v", err)
	}
	t.Cleanup(func() { _ = holder.Close() })

	// Guard the premise: if a second open ever stopped conflicting, the tests
	// below would measure an uncontended open and pass without proving anything.
	second, err := wal.Open(path)
	if err == nil {
		_ = second.Close()
		t.Fatal("a second wal.Open on a held path succeeded; these tests need genuine contention")
	}
	return path
}

// TestOpenWALExhaustsTheSharedPolicy is the measured proof that the graph
// WAL opener realises the shared policy and nothing of its own.
//
// It measures elapsed time under real contention rather than reading the
// constants back, because the constants were never the defect: they said 100 ms,
// 1000 ms and 5 all along while the loop they fed waited four times.
func TestOpenWALExhaustsTheSharedPolicy(t *testing.T) {
	t.Parallel()

	path := heldWALPath(t)

	start := time.Now()
	writer, err := openWAL(path)
	elapsed := time.Since(start)

	if err == nil {
		_ = writer.Close()
		t.Fatal("openWAL returned a writer for a path whose lock was held throughout")
	}
	if floor := backoff.Total() - backoff.Total()/10; elapsed < floor {
		t.Errorf("a contended graph WAL open gave up after %v; the shared policy sleeps about %v "+
			"(SPEC/IMPLEMENTATION.md § Retry Logic). A wait near %v means this site skips the sleep "+
			"before its last attempt, which is defect #294",
			elapsed, backoff.Total(), backoff.Total()-1000*time.Millisecond)
	}
}

// TestOpenWALExhaustionSurfacesAsErrDatabase pins the error contract the
// timing fix had to leave untouched: an exhausted wait is a database-class
// failure (exit code 1), carrying the diagnostic the CLI prints, with the
// underlying WAL error still named.
func TestOpenWALExhaustionSurfacesAsErrDatabase(t *testing.T) {
	t.Parallel()

	_, err := openWAL(heldWALPath(t))
	if err == nil {
		t.Fatal("openWAL returned a writer for a path whose lock was held throughout")
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("an exhausted wait must surface as utils.ErrDatabase (exit 1), got: %v", err)
	}
	if want := "graph store unavailable"; !strings.Contains(err.Error(), want) {
		t.Errorf("exhaustion message = %q, want it to contain %q", err.Error(), want)
	}
	if want := wal.ErrWALLocked.Error(); !strings.Contains(err.Error(), want) {
		t.Errorf("exhaustion message = %q, want it to name the underlying cause %q", err.Error(), want)
	}
}

// TestOpenWALSucceedsWithoutWaiting pins the uncontended path: an
// available WAL is opened on the first attempt, with no part of the ladder
// slept. Every ordinary graph write takes this path, so a policy that slept
// before its first attempt would tax all of them.
func TestOpenWALSucceedsWithoutWaiting(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "graph.wal")

	start := time.Now()
	writer, err := openWAL(path)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("openWAL failed on an uncontended path: %v", err)
	}
	defer func() { _ = writer.Close() }()

	if elapsed >= backoff.FirstDelay {
		t.Errorf("an uncontended open took %v; the policy must not sleep before its first attempt", elapsed)
	}
}

// TestOpenWALSucceedsOnceTheHolderReleases pins the middle of the ladder
// under real contention: a writer released partway through the wait is picked
// up on a later attempt rather than after the whole ladder, and rather than not
// at all.
func TestOpenWALSucceedsOnceTheHolderReleases(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "graph.wal")
	holder, err := wal.Open(path)
	if err != nil {
		t.Fatalf("opening the WAL writer that holds the lock: %v", err)
	}

	// Released inside the first rung of the ladder, so the retry must pick it up
	// on its second or third attempt.
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = holder.Close()
	}()

	start := time.Now()
	writer, err := openWAL(path)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("openWAL never acquired a WAL released after 50ms: %v", err)
	}
	defer func() { _ = writer.Close() }()

	if elapsed >= backoff.Total() {
		t.Errorf("acquiring a WAL released after 50ms took %v; the loop must return at its first "+
			"success rather than running the ladder out (%v)", elapsed, backoff.Total())
	}
}
