// Contract tests for the graph store's advisory lock
// (SPEC/GRAPH.md § Concurrency and Recovery; § Lock Contention; Acceptance
// Criteria 33, 36, 37).
//
// The lock has two modes over one file, and every property below is one half of
// a pair that a platform port, or a refactor, is liable to break in isolation:
//
//   - exclusive excludes exclusive, and fails immediately rather than waiting;
//   - shared does NOT exclude shared, so reads never serialise on one another;
//   - shared and exclusive exclude each other, in both directions;
//   - a shared acquisition WAITS for a writer, bounded, and then fails;
//   - the contended acquisition path does not leak a file descriptor.
//
// The tests use two distinct file descriptors on the same lock file from the
// same process. That is a faithful stand-in for two processes: flock(2) treats
// separate open file descriptions independently even inside one process, and
// LockFileEx locks per handle, so the exclusion observed here is the exclusion
// two `rmp` invocations observe.
package graphlock

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// busyWriteMessage is the contention diagnostic AcquireExclusive must produce on
// every platform. SPEC/GRAPH.md § Lock Contention rule 1 and
// SPEC/IMPLEMENTATION.md § Graph Store Concurrency require a second writer to
// fail with this outcome rather than wait, so the text is part of the contract
// that the Unix and Windows lock primitives share.
const busyWriteMessage = "graph store is busy: a concurrent write is in progress"

// busyReadMessage is the diagnostic AcquireShared must produce once its bounded
// wait is exhausted (SPEC/GRAPH.md § Lock Contention rule 2).
const busyReadMessage = "graph store is busy: a concurrent write is still in progress after the bounded wait"

// boundedWait is the worst-case total sleep AcquireShared performs: one initial
// attempt plus retryMaxRetries retries at 100+200+400+800+1000 ms. It is
// derived from the constants rather than written out, so a change to the policy
// moves the assertions with it instead of silently invalidating them.
var boundedWait = func() time.Duration {
	var total time.Duration
	delay := retryInitialDelay
	for range retryMaxRetries {
		total += delay
		delay *= 2
		if delay > retryMaxDelay {
			delay = retryMaxDelay
		}
	}
	return total
}()

// TestBoundedWaitMatchesTheSpecifiedPolicy pins the reader's wait budget to the
// figure the SPEC and the web timeout reasoning depend on. SPEC/IMPLEMENTATION.md
// § Graph Store Concurrency rule 4 specifies initial delay 100 ms doubling to a
// maximum of 1000 ms with at most 5 retries, and SPEC/WEB.md § Knowledge Graph
// from the GoGraph Store rule 5 relies on the resulting worst case staying well
// inside the server's 30 s write timeout. A silent change to any of the three
// constants would move that worst case without any other test noticing.
func TestBoundedWaitMatchesTheSpecifiedPolicy(t *testing.T) {
	if got, want := retryInitialDelay, 100*time.Millisecond; got != want {
		t.Errorf("initial delay = %v, want %v", got, want)
	}
	if got, want := retryMaxDelay, 1000*time.Millisecond; got != want {
		t.Errorf("maximum delay = %v, want %v", got, want)
	}
	if got, want := retryMaxRetries, 5; got != want {
		t.Errorf("maximum retries = %d, want %d", got, want)
	}
	if got, want := boundedWait, 2500*time.Millisecond; got != want {
		t.Errorf("worst-case bounded wait = %v, want %v (100+200+400+800+1000 ms)", got, want)
	}
}

// TestAcquireExclusive_MutualExclusion is a regression gate for finding #39:
// the exclusive graph store lock must prevent two writers from holding it at
// once, and contention must surface as utils.ErrDatabase (exit 1) — never a
// silent overlap that would let a stale-snapshot checkpoint drop a committed
// write. Releasing the lock must make it acquirable again.
func TestAcquireExclusive_MutualExclusion(t *testing.T) {
	dir := t.TempDir()

	release1, err := AcquireExclusive(dir)
	if err != nil {
		t.Fatalf("first lock acquisition failed: %v", err)
	}

	// A second acquisition while the first is held must fail with ErrDatabase.
	release2, err := AcquireExclusive(dir)
	if err == nil {
		release2()
		release1()
		t.Fatal("second concurrent lock acquisition succeeded; expected contention error")
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("contention must surface as utils.ErrDatabase (exit 1), got: %v", err)
	}
	if !strings.Contains(err.Error(), busyWriteMessage) {
		t.Errorf("contention message = %q, want it to contain %q", err.Error(), busyWriteMessage)
	}

	// After releasing the first lock, it must be acquirable again.
	release1()
	release3, err := AcquireExclusive(dir)
	if err != nil {
		t.Fatalf("lock not reacquirable after release: %v", err)
	}
	release3()
}

// TestAcquireExclusive_ContentionFailsFast pins the half of the write-lock
// contract that a port is most likely to get wrong: the exclusive lock is
// NON-BLOCKING, so a second writer fails immediately instead of waiting for the
// first to finish. flock(2) only behaves that way because of LOCK_NB, and
// LockFileEx only because of LOCKFILE_FAIL_IMMEDIATELY — both are opt-in, and
// dropping either one turns a fast, well-diagnosed failure into a hang that no
// assertion on the returned error would ever catch.
//
// It also guards against the writer inheriting the reader's retry loop: a
// writer that waited would take at least the bounded wait to fail, so the
// elapsed-time bound below fails in that case too.
//
// The contended call is made on a separate goroutine so that a blocking
// implementation fails this test with a clear diagnostic rather than
// deadlocking until the whole test binary times out.
func TestAcquireExclusive_ContentionFailsFast(t *testing.T) {
	dir := t.TempDir()

	release, err := AcquireExclusive(dir)
	if err != nil {
		t.Fatalf("first lock acquisition failed: %v", err)
	}
	defer release()

	type attempt struct {
		release func()
		err     error
		elapsed time.Duration
	}
	done := make(chan attempt, 1)
	go func() {
		start := time.Now()
		r, err := AcquireExclusive(dir)
		done <- attempt{release: r, err: err, elapsed: time.Since(start)}
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
		if !strings.Contains(got.err.Error(), busyWriteMessage) {
			t.Errorf("contention message = %q, want it to contain %q", got.err.Error(), busyWriteMessage)
		}
		if got.elapsed >= boundedWait {
			t.Errorf("contended write waited %v; a writer must fail on the FIRST collision, "+
				"never retry like a reader (SPEC/GRAPH.md § Lock Contention rule 1)", got.elapsed)
		}
	case <-time.After(bound):
		t.Fatalf("contended acquisition still blocked after %s; the write lock must fail immediately, "+
			"never wait (SPEC/GRAPH.md § Lock Contention rule 1)", bound)
	}
}

// TestAcquireExclusive_ContentionDoesNotLeakHandles guards the failure path's
// cleanup: AcquireExclusive opens the lock file before it tries to lock it, so a
// contended attempt that returned without closing that handle would leak one
// descriptor per attempt.
//
// The check is indirect by necessity — Go exposes no portable open-descriptor
// count — so it works by exhaustion: with a leak, a process whose descriptor
// limit is the common 1024 stops being able to open the lock file part-way
// through, and the reported error changes from the contention diagnostic to an
// open failure. On a host with a very high limit the loop cannot prove the
// absence of a leak, but it still costs almost nothing and it fails loudly
// wherever the limit is ordinary.
func TestAcquireExclusive_ContentionDoesNotLeakHandles(t *testing.T) {
	dir := t.TempDir()

	release, err := AcquireExclusive(dir)
	if err != nil {
		t.Fatalf("first lock acquisition failed: %v", err)
	}
	defer release()

	const attempts = 2048
	for i := range attempts {
		r, err := AcquireExclusive(dir)
		if err == nil {
			r()
			t.Fatalf("attempt %d acquired a held lock; the lock is not exclusive", i)
		}
		if !strings.Contains(err.Error(), busyWriteMessage) {
			t.Fatalf("attempt %d failed for the wrong reason (descriptor leak on the contention path?): %v", i, err)
		}
	}
}

// TestAcquireShared_ReadersDoNotExcludeReaders covers SPEC/GRAPH.md acceptance
// criterion 37: several readers may hold the lock at the same time, so reads
// never serialise on one another. A shared mode implemented as an exclusive one
// — the easiest way to get this wrong, since the exclusive primitive already
// existed — would make the second acquisition wait the full bounded wait and
// then fail, so the elapsed-time bound is asserted as well as the success.
func TestAcquireShared_ReadersDoNotExcludeReaders(t *testing.T) {
	dir := t.TempDir()

	release1, err := AcquireShared(dir)
	if err != nil {
		t.Fatalf("first shared acquisition failed: %v", err)
	}
	defer release1()

	start := time.Now()
	release2, err := AcquireShared(dir)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("second shared acquisition failed; readers must not exclude one another: %v", err)
	}
	defer release2()

	if elapsed >= retryInitialDelay {
		t.Errorf("second shared acquisition took %v; it must succeed at once, not after a retry, "+
			"which would mean the shared mode is really exclusive", elapsed)
	}

	// A third one, to make it clear the coexistence is not limited to a pair.
	release3, err := AcquireShared(dir)
	if err != nil {
		t.Fatalf("third shared acquisition failed: %v", err)
	}
	release3()
}

// TestAcquireShared_ExcludedByAWriter covers the reader half of the mutual
// exclusion: while a writer holds the lock exclusively, no reader can take it.
// This is the property that makes the read path safe at all — an unlocked read
// runs GoGraph's recovery repair, which removes snapshot.tmp and can promote
// snapshot.bak, over the very directory the writer's checkpoint is publishing
// into.
//
// The reader must not fail on the first collision (SPEC/GRAPH.md § Lock
// Contention rule 2): it must still be trying after the first retry delay.
func TestAcquireShared_ExcludedByAWriter(t *testing.T) {
	dir := t.TempDir()

	releaseWriter, err := AcquireExclusive(dir)
	if err != nil {
		t.Fatalf("writer lock acquisition failed: %v", err)
	}

	type attempt struct {
		release func()
		err     error
	}
	done := make(chan attempt, 1)
	go func() {
		r, err := AcquireShared(dir)
		done <- attempt{release: r, err: err}
	}()

	// Still retrying after the first delay proves the reader did not fail fast.
	select {
	case got := <-done:
		if got.err == nil {
			got.release()
			releaseWriter()
			t.Fatal("a reader took the shared lock while a writer held it exclusively; " +
				"an unlocked read can race a writer's checkpoint")
		}
		releaseWriter()
		t.Fatalf("the reader gave up after the first collision; it must retry under the "+
			"bounded backoff (SPEC/GRAPH.md § Lock Contention rule 2), got: %v", got.err)
	case <-time.After(retryInitialDelay + retryInitialDelay/2):
	}

	releaseWriter()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("the reader must succeed once the writer releases the lock, got: %v", got.err)
		}
		got.release()
	case <-time.After(boundedWait):
		t.Fatalf("the reader did not acquire the shared lock within %v of the writer releasing it", boundedWait)
	}
}

// TestAcquireExclusive_ExcludedByAReader covers the other direction of the same
// exclusion, and it is the direction a shared-mode port is most likely to drop:
// a shared lock that was in fact a no-op would pass every reader-side test
// above and still let a writer's checkpoint run underneath a reader's recovery
// repair. A writer that collides with a reader fails fast, exactly as it does
// against another writer (SPEC/GRAPH.md § Lock Contention rule 1).
func TestAcquireExclusive_ExcludedByAReader(t *testing.T) {
	dir := t.TempDir()

	releaseReader, err := AcquireShared(dir)
	if err != nil {
		t.Fatalf("shared acquisition failed: %v", err)
	}

	start := time.Now()
	release, err := AcquireExclusive(dir)
	elapsed := time.Since(start)
	if err == nil {
		release()
		releaseReader()
		t.Fatal("a writer took the exclusive lock while a reader held the shared lock; " +
			"the shared mode is not excluding anything")
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("contention must surface as utils.ErrDatabase (exit 1), got: %v", err)
	}
	if elapsed >= boundedWait {
		t.Errorf("the writer waited %v for the reader; a writer must fail on the first collision", elapsed)
	}

	releaseReader()
	release, err = AcquireExclusive(dir)
	if err != nil {
		t.Fatalf("the writer must succeed once the reader releases the lock, got: %v", err)
	}
	release()
}

// TestAcquireShared_BoundedWaitThenFails covers SPEC/GRAPH.md acceptance
// criterion 36 and SPEC/WEB.md rule 149: a reader that never gets the lock must
// fail rather than hang. Both halves are asserted, because each on its own is
// satisfied by a broken implementation:
//
//   - it must take AT LEAST the bounded wait, or the reader is failing fast and
//     ordinary reads would be intermittently unavailable beside a writer;
//   - it must RETURN, and with utils.ErrDatabase, or a web request would block
//     until the server's write timeout fired.
func TestAcquireShared_BoundedWaitThenFails(t *testing.T) {
	dir := t.TempDir()

	releaseWriter, err := AcquireExclusive(dir)
	if err != nil {
		t.Fatalf("writer lock acquisition failed: %v", err)
	}
	defer releaseWriter()

	type attempt struct {
		release func()
		err     error
		elapsed time.Duration
	}
	done := make(chan attempt, 1)
	go func() {
		start := time.Now()
		r, err := AcquireShared(dir)
		done <- attempt{release: r, err: err, elapsed: time.Since(start)}
	}()

	// Generously larger than the bounded wait: this bound only distinguishes
	// "returned" from "blocked forever".
	ceiling := 4 * boundedWait
	select {
	case got := <-done:
		if got.err == nil {
			got.release()
			t.Fatal("the reader took the shared lock while a writer held it exclusively")
		}
		if !errors.Is(got.err, utils.ErrDatabase) {
			t.Errorf("an exhausted wait must surface as utils.ErrDatabase (exit 1 / HTTP 500), got: %v", got.err)
		}
		if !strings.Contains(got.err.Error(), busyReadMessage) {
			t.Errorf("exhausted-wait message = %q, want it to contain %q", got.err.Error(), busyReadMessage)
		}
		// A little slack below the nominal figure: time.Sleep guarantees a
		// minimum, but the comparison is against a clock read taken around the
		// whole loop, and coarse timer resolution can shave a fraction off.
		if floor := boundedWait - boundedWait/10; got.elapsed < floor {
			t.Errorf("the reader gave up after %v; it must wait the bounded backoff (about %v) "+
				"before failing (SPEC/GRAPH.md § Lock Contention rule 2)", got.elapsed, boundedWait)
		}
	case <-time.After(ceiling):
		t.Fatalf("the reader was still blocked after %v; the wait must be BOUNDED and end in a "+
			"failure, never an indefinite block (SPEC/GRAPH.md § Lock Contention rule 2)", ceiling)
	}
}

// TestLockFileName pins the lock file's name. It is the one entry inside the
// graph directory that Groadmap owns rather than GoGraph, it is named in
// SPEC/GRAPH.md § Persistence Layout, and both the CLI and the web server must
// agree on it or they would lock different files and exclude nothing.
func TestLockFileName(t *testing.T) {
	if LockFileName != "write.lock" {
		t.Errorf("LockFileName = %q, want %q (SPEC/GRAPH.md § Persistence Layout)", LockFileName, "write.lock")
	}
}
