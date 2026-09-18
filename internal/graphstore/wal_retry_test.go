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
//
// What they assert is the ATTEMPT COUNT, counted at the walRetryable seam, and
// never a duration: SPEC/BUILD.md § No Benchmarks and No Performance-Measurement Tests
// names a count of attempts as an admissible observable and forbids the clock.
// The ladder this site climbs is proven once, against an injected delay source,
// by internal/backoff's TestRetryExhaustsTheWholePolicy; that this site owns no
// ladder of its own is proven by internal/testenv's
// TestOnlyTheBackoffPackageWaits. What remains for this file is that the call
// site reaches the whole of the policy, and an attempt count says that exactly.
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

// countClassifiedFailures replaces the retry policy's classifier with a counting
// wrapper around the real one for the duration of one test, and returns the
// counter.
//
// What it counts is the number of failed opens the policy CLASSIFIED, and that
// determines the attempt count exactly, because the loop asks the classifier once
// after every attempt that failed and never after one that succeeded:
//
//   - an exhausted open classifies once per attempt, so the count IS the attempt
//     count;
//   - an open that succeeds classifies once per attempt before the last, so the
//     attempt count is the count plus one. Zero therefore means the very first
//     attempt succeeded and nothing was ever waited for.
//
// It wraps rather than replaces, so the verdict stays production's: every failure
// is retryable, exactly as backoff.Always says. The tests that use it do not call
// t.Parallel, because the seam is a package-level variable.
func countClassifiedFailures(t *testing.T) *int {
	t.Helper()

	previous := walRetryable
	t.Cleanup(func() { walRetryable = previous })

	classified := 0
	walRetryable = func(err error) bool {
		classified++
		return previous(err)
	}
	return &classified
}

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

// TestOpenWALExhaustsTheSharedPolicy is the proof that the graph WAL opener
// realises the shared policy and nothing of its own: under contention it makes
// every attempt the policy allows, and stops there.
//
// The count is taken at the seam rather than derived from the constants, because
// the constants were never the defect: they said 100 ms, 1000 ms and 5 all along
// while the loop they fed gave up an attempt early.
func TestOpenWALExhaustsTheSharedPolicy(t *testing.T) {
	classified := countClassifiedFailures(t)
	path := heldWALPath(t)

	writer, err := openWAL(path)
	if err == nil {
		_ = writer.Close()
		t.Fatal("openWAL returned a writer for a path whose lock was held throughout")
	}
	// Every attempt failed, so the classification count IS the attempt count.
	if *classified != backoff.Attempts {
		t.Errorf("a contended graph WAL open was attempted %d times, want %d — one initial attempt "+
			"plus every retry the shared policy allows (SPEC/IMPLEMENTATION.md § Retry Logic). "+
			"Fewer means this site stops short of the policy, which is defect #294",
			*classified, backoff.Attempts)
	}
}

// TestOpenWALExhaustionSurfacesAsErrGraphStore pins the error contract the
// timing fix had to leave untouched: an exhausted wait is a database-class
// failure (exit code 1), carrying the diagnostic the CLI prints, with the
// underlying WAL error still named.
func TestOpenWALExhaustionSurfacesAsErrGraphStore(t *testing.T) {
	_, err := openWAL(heldWALPath(t))
	if err == nil {
		t.Fatal("openWAL returned a writer for a path whose lock was held throughout")
	}
	if !errors.Is(err, utils.ErrGraphStore) {
		t.Errorf("an exhausted wait must surface as utils.ErrGraphStore (exit 1), got: %v", err)
	}
	if want := "graph store unavailable"; !strings.Contains(err.Error(), want) {
		t.Errorf("exhaustion message = %q, want it to contain %q", err.Error(), want)
	}
	if want := wal.ErrWALLocked.Error(); !strings.Contains(err.Error(), want) {
		t.Errorf("exhaustion message = %q, want it to name the underlying cause %q", err.Error(), want)
	}
}

// TestOpenWALSucceedsWithoutWaiting pins the uncontended path: an available WAL
// is opened on the FIRST attempt, so no rung of the ladder is reached at all.
// Every ordinary graph write takes this path, so a policy that waited before its
// first attempt would tax all of them — and one attempt is the whole of the
// proof that it did not, because the loop waits only between attempts.
func TestOpenWALSucceedsWithoutWaiting(t *testing.T) {
	classified := countClassifiedFailures(t)
	path := filepath.Join(t.TempDir(), "graph.wal")

	writer, err := openWAL(path)
	if err != nil {
		t.Fatalf("openWAL failed on an uncontended path: %v", err)
	}
	defer func() { _ = writer.Close() }()

	if *classified != 0 {
		t.Errorf("an uncontended open produced %d classified failure(s), want none: the first "+
			"attempt succeeded, so nothing was classified and no rung of the ladder was reached",
			*classified)
	}
}

// TestOpenWALSucceedsOnceTheHolderReleases pins the middle of the ladder under
// real contention: a writer released partway through the wait is picked up on a
// later attempt rather than after the whole ladder, and rather than not at all.
//
// "A later attempt, not the last" is the attempt count, recovered from the
// classified failures: strictly more than one,
// because the first found the lock held, and strictly fewer than the policy
// allows, because the loop returns at its first success instead of running the
// ladder out.
func TestOpenWALSucceedsOnceTheHolderReleases(t *testing.T) {
	classified := countClassifiedFailures(t)

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

	writer, err := openWAL(path)
	if err != nil {
		t.Fatalf("openWAL never acquired a WAL released after 50ms: %v", err)
	}
	defer func() { _ = writer.Close() }()

	// It ended in a success, so the attempt count is the classified failures plus
	// one.
	attempts := *classified + 1
	if attempts < 2 {
		t.Errorf("acquiring a WAL that was held at first took %d attempt(s); the first one found "+
			"the lock held, so the open that succeeded cannot be it", attempts)
	}
	if attempts >= backoff.Attempts {
		t.Errorf("acquiring a WAL released early took %d attempts, want fewer than the %d the "+
			"policy allows: the loop must return at its first success rather than running the "+
			"ladder out", attempts, backoff.Attempts)
	}
}
