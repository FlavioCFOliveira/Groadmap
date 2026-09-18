// Regression fence for defect #294 at the SQLite call site.
//
// retryWithBackoff used to own a private copy of the bounded backoff: it read
// the specification's "Maximum retries: 5" as five ATTEMPTS and guarded its
// sleep with `attempt < maxRetries-1`, so it waited four times for 1500 ms and
// never reached the 1000 ms rung the specification and its own comment both
// promised. Nothing noticed, because the three copies of the loop were only ever
// compared by eye.
//
// What these tests assert is the ATTEMPT COUNT, which is the quantity the defect
// got wrong at this call site and the quantity the operation this package hands
// the policy can count for itself. They do not read the constants back — the
// drifted loop would have passed that, because its constants were right all
// along and only the loop was wrong — and they do not read a clock either
// (SPEC/BUILD.md § No Benchmarks and No Performance-Measurement Tests). The
// ladder this site climbs is the shared one, asserted once against the injected
// delay source in internal/backoff's TestRetryExhaustsTheWholePolicy, and that
// this site owns no ladder of its own is asserted by internal/testenv's
// TestOnlyTheBackoffPackageWaits. Every figure named here comes from
// internal/backoff, so a change to the policy moves the assertions with it
// instead of leaving them behind.
package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
)

// lockedErr is the contention retryWithBackoff retries on: a SQLite BUSY
// result. mockSQLiteErr (connection_test.go) carries the result code that
// isLockedError classifies.
func lockedErr() error { return &mockSQLiteErr{code: sqliteBusy} }

// TestRetryWithBackoffExhaustsTheSharedPolicy is the proof that the SQLite retry
// loop realises the shared policy and nothing of its own.
//
// The operation must be attempted backoff.Attempts times: a loop that runs out
// of attempts early gives up sooner than the specification allows, and a loop
// that makes five attempts where six are promised is defect #294 at this site
// exactly. The count is exact and comes from the shared declaration, so it moves
// with the policy rather than restating it.
func TestRetryWithBackoffExhaustsTheSharedPolicy(t *testing.T) {
	t.Parallel()

	calls := 0
	err := retryWithBackoff("transaction", func() error {
		calls++
		return lockedErr()
	})

	if err == nil {
		t.Fatal("retryWithBackoff returned nil for an operation that never stopped being locked")
	}
	if calls != backoff.Attempts {
		t.Errorf("the operation was attempted %d times, want %d (one initial attempt plus five "+
			"retries, SPEC/IMPLEMENTATION.md § Retry Logic). Fewer means this site stops short of "+
			"the shared policy, which is defect #294", calls, backoff.Attempts)
	}
}

// TestRetryWithBackoffReportsTheAttemptsItMade pins the diagnostic to the
// policy. The message used to say "failed after 5 attempts" while the loop was
// making five attempts and four waits; both numbers moved, and the message must
// move with them rather than becoming a second, stale statement of the policy.
func TestRetryWithBackoffReportsTheAttemptsItMade(t *testing.T) {
	t.Parallel()

	err := retryWithBackoff("running migrations", lockedErr)
	if err == nil {
		t.Fatal("retryWithBackoff returned nil for an operation that never stopped being locked")
	}

	want := "running migrations: failed after 6 attempts"
	if got := err.Error(); !strings.Contains(got, want) {
		t.Errorf("exhaustion message = %q, want it to contain %q", got, want)
	}

	// The cause must survive: callers classify the wrapped SQLite error.
	var coded sqliteCoded
	if !errors.As(err, &coded) {
		t.Error("the exhaustion error must wrap the last SQLite error, or callers lose the result code")
	}
}

// TestRetryWithBackoffDoesNotWaitOnANonRetryableError pins the retry SEMANTICS
// the timing fix had to leave untouched. Only busy/locked failures are waited
// on (SPEC/IMPLEMENTATION.md § Retry Logic, Retry Conditions); a constraint
// violation, a schema error or bad input must come back at once and unwrapped,
// so callers such as IsUniqueConstraintErr still see it and the user is not
// made to wait 2.5 s for a rejection that will never change.
func TestRetryWithBackoffDoesNotWaitOnANonRetryableError(t *testing.T) {
	t.Parallel()

	violation := &mockSQLiteErr{code: sqliteConstraintUniqueViolat}

	calls := 0
	err := retryWithBackoff("creating schema", func() error {
		calls++
		return violation
	})

	if !errors.Is(err, error(violation)) {
		t.Errorf("retryWithBackoff returned %v, want the constraint violation unwrapped", err)
	}
	if strings.Contains(err.Error(), "failed after") {
		t.Errorf("a non-retryable failure was reported as an exhausted retry: %q", err.Error())
	}
	if calls != 1 {
		t.Errorf("a non-retryable failure was attempted %d times, want 1: one attempt is the whole "+
			"of the proof that it was not waited on, because the loop waits only between attempts",
			calls)
	}
	if !IsUniqueConstraintErr(err) {
		t.Error("the unwrapped constraint violation must still classify as a uniqueness collision")
	}
}

// TestRetryWithBackoffStopsAtTheFirstSuccess pins the third outcome: an
// operation that wins the lock part-way through the ladder returns then, having
// climbed only the rungs it needed.
func TestRetryWithBackoffStopsAtTheFirstSuccess(t *testing.T) {
	t.Parallel()

	const succeedOn = 3

	calls := 0
	err := retryWithBackoff("configuring database", func() error {
		calls++
		if calls < succeedOn {
			return lockedErr()
		}
		return nil
	})

	if err != nil {
		t.Errorf("retryWithBackoff returned %v for an operation that succeeded on attempt %d", err, succeedOn)
	}
	if calls != succeedOn {
		t.Errorf("the operation was attempted %d times, want %d: the loop must stop at the first "+
			"success rather than running the ladder out to %d attempts",
			calls, succeedOn, backoff.Attempts)
	}
}
