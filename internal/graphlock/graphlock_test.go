// Contract tests for the graph store's advisory lock
// (SPEC/GRAPH.md § Concurrency and Recovery; § Lock Contention; Acceptance
// Criteria 19 and 20).
//
// SPEC/GRAPH.md gives the store ONE lock mode, exclusive, with ONE contention
// policy, the bounded wait. AcquireExclusive used to fail on the first
// collision, and the asymmetry that justified it — a rare writer against
// frequent readers that waited — went with the five graph subcommands: every
// caller is a possible reader now, so failing one fast would make ordinary
// statements intermittently unavailable.
//
// A shared mode existed beside it and is gone. Its last caller was the web graph
// data endpoint, which took a shared hold across the store open alone because it
// could not write; that endpoint now runs its statement on the transactional
// path and holds the exclusive lock across the whole sequence (rmp task #364).
// The shared-mode tests went with the mode: a test for a function nothing calls
// asserts the behaviour of dead code.
//
// Every property below is one half of a pair that a platform port, or a
// refactor, is liable to break in isolation:
//
//   - exclusive excludes exclusive, and WAITS, bounded, before it fails;
//   - the wait ENDS, with utils.ErrDatabase, rather than blocking indefinitely;
//   - the wait is sized against the longest LAWFUL hold rather than against the
//     SQLite policy's total, so a holder that stays inside its own statement
//     budget cannot starve a waiter;
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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// busyExclusiveMessage is the diagnostic AcquireExclusive must produce on every
// platform once its bounded wait is exhausted (SPEC/GRAPH.md § Lock Contention
// rule 2). It names no operation class, because the holder may not have been
// writing: one mode carries one message, and it says only that the store is held.
const busyExclusiveMessage = "graph store is busy: another invocation still holds it after the bounded wait"

// firstRung is the ladder's first delay, used by the assertions that an
// acquisition did NOT wait. It comes from the shared policy, so no test in this
// package names a figure of the policy's own; the figures themselves are
// asserted once, in internal/backoff's TestPolicyMatchesTheSpecification.
const firstRung = backoff.FirstDelay

// budgetlessHold is the statement budget the three exhaustion tests below run
// under, and zero is the honest value rather than merely the cheap one: the
// holder they contend with is a bare lock hold with no statement behind it, so
// the variable part of the hold their wait must cover really is nothing, and the
// wait collapses to the fixed-cost allowance alone.
//
// It is a test-time cost control and not a weakening. What matters about the
// value in force is fenced independently of it: the derivation by
// TestWaitBudgetIsDerivedFromTheStatementBudget, and the fact that the wait
// genuinely tracks the budget — on the wall clock, against a lawful hold — by
// TestAcquireExclusive_WaitsTheDerivedBudgetNotTheSQLiteTotal. At the production
// budget each of these acquisitions would spend 7.5 s instead of 2.5 s and prove
// nothing extra.
const budgetlessHold time.Duration = 0

// setStatementBudget installs a statement budget for the duration of one test
// and restores the previous value when the test ends. Production never
// reassigns StatementBudget; this is the only writer, and t.Cleanup makes nested
// use safe because cleanups unwind LIFO.
func setStatementBudget(t *testing.T, d time.Duration) {
	t.Helper()
	previous := StatementBudget
	t.Cleanup(func() { StatementBudget = previous })
	StatementBudget = d
}

// TestWaitBudgetIsDerivedFromTheStatementBudget pins the DERIVATION rather than
// the 7.5 seconds it currently yields, which is the only form of the assertion
// that survives a change to either term.
//
// This test used to say something else, and what it said was wrong twice over.
// It asserted that the lock's wait WAS backoff.Total, and that the wait stayed
// under a tenth of the web server's 30 s write timeout. Both were satisfied,
// comfortably, by the sizing that let a lawful 4.71-second holder starve a
// contender which gave up after 2.5018 seconds:
//
//   - equality with backoff.Total is exactly the defect. The SQLite total is the
//     allowance for the FIXED part of a hold; a hold here also spans a statement
//     whose cost the caller chooses, and SPEC/IMPLEMENTATION.md § Graph Store
//     Concurrency, "Write Contention and Recovery" rule 3 takes the loop and the
//     ladder from that policy while deliberately not taking its total;
//   - "a small fraction of the write timeout" is neither necessary nor
//     sufficient. It says nothing about whether the wait outlasts the hold it
//     has to cover, and what must fit inside that timeout is the statement and
//     the wait TOGETHER — asserted where both terms are visible, in
//     internal/web's TestGraphQueryBudget_StatementAndWaitFitTheWriteTimeout.
//
// What is asserted instead is the sizing rule SPEC/GRAPH.md § Lock Contention
// states — wait budget = statement budget + backoff total — and the property
// that rule exists to deliver: the wait STRICTLY outlasts the longest lawful
// statement, with the fixed-cost allowance as the margin.
func TestWaitBudgetIsDerivedFromTheStatementBudget(t *testing.T) {
	assert := func(t *testing.T, when string) {
		t.Helper()
		if got, want := WaitBudget(), StatementBudget+backoff.Total(); got != want {
			t.Errorf("%s: WaitBudget() = %v, want the statement budget %v plus the backoff total %v "+
				"= %v (SPEC/GRAPH.md § Lock Contention)", when, got, StatementBudget, backoff.Total(), want)
		}
		if WaitBudget() <= StatementBudget {
			t.Errorf("%s: the wait budget is %v and a statement may lawfully hold the lock for %v, "+
				"so a holder inside its own budget outlasts the waiter and starves it. The wait must "+
				"strictly exceed the longest lawful hold (SPEC/GRAPH.md § Lock Contention)",
				when, WaitBudget(), StatementBudget)
		}
	}

	assert(t, "at the production budget")

	// And it must TRACK the budget rather than have been computed once: the two
	// would otherwise describe different policies inside one process the moment
	// a caller moved the budget.
	setStatementBudget(t, 3*time.Second)
	assert(t, "after the statement budget moved")
	if got, want := WaitBudget(), 3*time.Second+backoff.Total(); got != want {
		t.Errorf("WaitBudget() = %v after the statement budget was set to 3s, want %v: the wait must "+
			"be derived on each call, not frozen at initialisation", got, want)
	}
}

// TestAcquireExclusive_WaitsTheDerivedBudgetNotTheSQLiteTotal is the regression
// fence for the defect itself, measured on the wall clock rather than computed
// from the constants.
//
// The arithmetic above cannot catch this one. A build that derived WaitBudget
// correctly and then went on calling backoff.Retry — the fixed 2500 ms — would
// satisfy every assertion in this file except this one, and would starve exactly
// the waiter the budget exists to protect. So the acquisition is timed against a
// held lock and required to have waited the DERIVED budget.
//
// The statement budget is set to the fixed-cost allowance itself, which puts the
// two candidate sizings a clean factor of two apart on the clock: 5 s if the
// budget is honoured, 2.5 s if the SQLite total is used instead. No scheduling
// noise closes a gap that wide, and the cost is one acquisition.
func TestAcquireExclusive_WaitsTheDerivedBudgetNotTheSQLiteTotal(t *testing.T) {
	dir := t.TempDir()

	setStatementBudget(t, backoff.Total())

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
		r, acqErr := AcquireExclusive(dir)
		done <- attempt{release: r, err: acqErr, elapsed: time.Since(start)}
	}()

	select {
	case got := <-done:
		if got.err == nil {
			got.release()
			t.Fatal("contended acquisition succeeded; the lock is not exclusive")
		}
		if !errors.Is(got.err, utils.ErrDatabase) {
			t.Errorf("contention must surface as utils.ErrDatabase (exit 1), got: %v", got.err)
		}
		// The regression, named: a wait sized on the SQLite total alone would
		// have given up here, while a statement running the whole budget in force
		// would still have been holding the lock.
		//
		// The threshold clears backoff.Total by a tenth rather than testing
		// against it exactly, because an exhausted 2500 ms ladder lands a few
		// milliseconds PAST 2500 ms — the sleeps guarantee a minimum and the
		// clock is read around the whole loop. Measured, the reverted
		// implementation returns at 2.5029 s, which a strict comparison against
		// 2.5 s would let through. The derived budget in force here is twice the
		// total, so the two sizings stay unambiguously apart.
		if outlasted := backoff.Total() + backoff.Total()/10; got.elapsed < outlasted {
			t.Errorf("the contended acquisition gave up after %v, no meaningfully longer than the "+
				"SQLite policy's total of %v, while the statement budget in force is %v and the "+
				"derived wait is %v. The wait is sized on backoff.Total again, so a holder that stays "+
				"inside its own budget starves the waiter (SPEC/GRAPH.md § Lock Contention)",
				got.elapsed, backoff.Total(), StatementBudget, WaitBudget())
		}
		// A little slack below the nominal figure: time.Sleep guarantees a
		// minimum, but the comparison is against a clock read taken around the
		// whole loop, and coarse timer resolution can shave a fraction off.
		if floor := WaitBudget() - WaitBudget()/10; got.elapsed < floor {
			t.Errorf("the contended acquisition waited %v, less than the derived budget of %v",
				got.elapsed, WaitBudget())
		}
	case <-time.After(4 * WaitBudget()):
		t.Fatalf("contended acquisition still blocked after %v; the wait must be BOUNDED "+
			"(SPEC/GRAPH.md § Lock Contention rule 2)", 4*WaitBudget())
	}
}

// TestAcquireExclusive_MutualExclusion is a regression gate for finding #39:
// the exclusive graph store lock must prevent two invocations from holding it at
// once, and an exhausted wait must surface as utils.ErrDatabase (exit 1) — never
// a silent overlap that would let a stale-snapshot checkpoint drop a committed
// write. Releasing the lock must make it acquirable again.
func TestAcquireExclusive_MutualExclusion(t *testing.T) {
	dir := t.TempDir()

	setStatementBudget(t, budgetlessHold)

	release1, err := AcquireExclusive(dir)
	if err != nil {
		t.Fatalf("first lock acquisition failed: %v", err)
	}

	// A second acquisition while the first is held must not succeed, and must
	// fail only after the bounded wait rather than on the first collision.
	start := time.Now()
	release2, err := AcquireExclusive(dir)
	elapsed := time.Since(start)
	if err == nil {
		release2()
		release1()
		t.Fatal("second concurrent lock acquisition succeeded; expected contention error")
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("contention must surface as utils.ErrDatabase (exit 1), got: %v", err)
	}
	if !strings.Contains(err.Error(), busyExclusiveMessage) {
		t.Errorf("contention message = %q, want it to contain %q", err.Error(), busyExclusiveMessage)
	}
	if elapsed < firstRung {
		t.Errorf("the second acquisition failed after %v, which is less than the ladder's first "+
			"delay of %v: it failed on the first collision instead of waiting "+
			"(SPEC/GRAPH.md § Lock Contention rule 1)", elapsed, firstRung)
	}

	// After releasing the first lock, it must be acquirable again.
	release1()
	release3, err := AcquireExclusive(dir)
	if err != nil {
		t.Fatalf("lock not reacquirable after release: %v", err)
	}
	release3()
}

// TestAcquireExclusive_ContentionWaitsThenFails covers SPEC/GRAPH.md acceptance
// criterion 20 for the exclusive mode, and pins BOTH halves of the contention
// policy, because each half on its own is satisfied by a broken implementation:
//
//   - the contended acquisition must take AT LEAST the bounded wait, or it is
//     failing on the first collision and every statement against a busy roadmap
//     becomes intermittently unavailable;
//   - it must RETURN, and with utils.ErrDatabase, or an invocation hangs — and
//     one of the two callers of this lock is an HTTP request handler.
//
// The lower bound is what would have caught the pre-collapse behaviour, in
// which a second acquisition failed at once; the upper bound is what catches a
// port that drops LOCK_NB or LOCKFILE_FAIL_IMMEDIATELY, since either turns the
// bounded Go-side wait into an unbounded kernel block that no assertion on the
// returned error could ever see.
//
// The contended call is made on a separate goroutine so that a blocking
// implementation fails this test with a clear diagnostic rather than
// deadlocking until the whole test binary times out.
func TestAcquireExclusive_ContentionWaitsThenFails(t *testing.T) {
	dir := t.TempDir()

	setStatementBudget(t, budgetlessHold)

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

	// Generously larger than the bounded wait: this ceiling only distinguishes
	// "returned" from "blocked forever".
	ceiling := 4 * WaitBudget()
	select {
	case got := <-done:
		if got.err == nil {
			got.release()
			t.Fatal("contended acquisition succeeded; the lock is not exclusive")
		}
		if !errors.Is(got.err, utils.ErrDatabase) {
			t.Errorf("contention must surface as utils.ErrDatabase (exit 1), got: %v", got.err)
		}
		if !strings.Contains(got.err.Error(), busyExclusiveMessage) {
			t.Errorf("contention message = %q, want it to contain %q", got.err.Error(), busyExclusiveMessage)
		}
		// A little slack below the nominal figure: time.Sleep guarantees a
		// minimum, but the comparison is against a clock read taken around the
		// whole loop, and coarse timer resolution can shave a fraction off.
		if floor := WaitBudget() - WaitBudget()/10; got.elapsed < floor {
			t.Errorf("the contended acquisition gave up after %v; it must wait the derived budget "+
				"(about %v) before failing (SPEC/GRAPH.md § Lock Contention rule 1)", got.elapsed, WaitBudget())
		}
	case <-time.After(ceiling):
		t.Fatalf("contended acquisition still blocked after %v; the wait must be BOUNDED and end in "+
			"a failure, never an indefinite block (SPEC/GRAPH.md § Lock Contention rule 2)", ceiling)
	}
}

// descriptorProbe opens the lock file, records the descriptor value the runtime
// handed out, and closes it again. Where descriptors are allocated
// lowest-free-first — flock(2)'s platforms, which is where this lock's Unix half
// lives — the value climbs by exactly one for every descriptor the process is
// still holding, which makes it a cheap open-handle counter.
//
// It opens the same file, with the same flags, that openLockFile opens, so it
// costs nothing that the code under test does not already cost.
func descriptorProbe(t *testing.T, dir string) uintptr {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, LockFileName), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatalf("probing the descriptor table: %v", err)
	}
	fd := f.Fd()
	if closeErr := f.Close(); closeErr != nil {
		t.Fatalf("closing the probe handle: %v", closeErr)
	}
	return fd
}

// TestAcquireExclusive_ContentionDoesNotLeakHandles guards the failure path's
// cleanup: AcquireExclusive opens the lock file before it tries to lock it, so a
// contended attempt that returned without closing that handle would leak one
// descriptor per attempt.
//
// The check is indirect by necessity — Go exposes no portable open-descriptor
// count. It used to work by exhaustion, running 2048 contended attempts until a
// leak ran the process out of descriptors. That stopped being affordable when
// AcquireExclusive started WAITING on contention: each attempt now costs the
// full bounded wait, so the old loop would have taken over an hour.
//
// What replaces it is a descriptor-number probe, and its own non-vacuity is
// established inside the test rather than assumed: the probe is first shown to
// MOVE when handles really are held, and to come back when they are closed. A
// platform on which that demonstration fails reports so and the leak assertion
// is not silently reduced to decoration.
func TestAcquireExclusive_ContentionDoesNotLeakHandles(t *testing.T) {
	dir := t.TempDir()

	setStatementBudget(t, budgetlessHold)

	release, err := AcquireExclusive(dir)
	if err != nil {
		t.Fatalf("first lock acquisition failed: %v", err)
	}
	defer release()

	baseline := descriptorProbe(t, dir)

	// Non-vacuity: the probe must be able to see held handles at all. Without
	// this, a platform whose descriptor values are not allocated lowest-free
	// would make the assertion below pass unconditionally.
	const held = 4
	handles := make([]*os.File, 0, held)
	for i := range held {
		f, openErr := os.OpenFile(filepath.Join(dir, LockFileName), os.O_CREATE|os.O_RDWR, 0600)
		if openErr != nil {
			t.Fatalf("opening decoy handle %d: %v", i, openErr)
		}
		handles = append(handles, f)
	}
	withHandles := descriptorProbe(t, dir)
	for _, f := range handles {
		_ = f.Close()
	}
	if withHandles <= baseline {
		t.Fatalf("the descriptor probe read %d with %d extra handles open and %d with none, so it "+
			"cannot see a held handle on this platform. The leak assertion below would pass "+
			"whatever the contention path did, and must not be reported as if it had checked "+
			"anything", withHandles, held, baseline)
	}
	if restored := descriptorProbe(t, dir); restored != baseline {
		t.Fatalf("the descriptor probe read %d after the decoy handles were closed and %d before "+
			"they were opened; the probe is not stable, so a difference it reports below would not "+
			"mean a leak", restored, baseline)
	}

	// The path under test. Each attempt costs the bounded wait, so the count is
	// small; the probe reports a leak of ONE, so a small count is enough.
	const attempts = 3
	for i := range attempts {
		r, attemptErr := AcquireExclusive(dir)
		if attemptErr == nil {
			r()
			t.Fatalf("attempt %d acquired a held lock; the lock is not exclusive", i)
		}
		if !strings.Contains(attemptErr.Error(), busyExclusiveMessage) {
			t.Fatalf("attempt %d failed for the wrong reason: %v", i, attemptErr)
		}
	}

	if after := descriptorProbe(t, dir); after != baseline {
		t.Errorf("the descriptor probe read %d after %d contended acquisitions and %d before them: "+
			"the contention path is leaking about %d handle(s), one per attempt. AcquireExclusive "+
			"opens the lock file before it locks it and MUST close it when the bounded wait is "+
			"exhausted", after, attempts, baseline, int(after)-int(baseline))
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
