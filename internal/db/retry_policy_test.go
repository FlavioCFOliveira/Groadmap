// Regression fence for defect #294 at the SQLite call site.
//
// retryWithBackoff used to own a private copy of the bounded backoff: it read
// the specification's "Maximum retries: 5" as five ATTEMPTS and guarded its
// sleep with `attempt < maxRetries-1`, so it waited four times for 1500 ms and
// never reached the 1000 ms rung the specification and its own comment both
// promised. Nothing measured it, so nothing noticed.
//
// These tests measure. They do not read the constants back — the drifted loop
// would have passed that, because its constants were right all along and only
// the loop was wrong — and they do not restate the policy either: every figure
// comes from internal/backoff, so a change to the policy moves the assertions
// with it instead of leaving them behind.
package db

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
)

// lockedErr is the contention retryWithBackoff retries on: a SQLite BUSY
// result. mockSQLiteErr (connection_test.go) carries the result code that
// isLockedError classifies.
func lockedErr() error { return &mockSQLiteErr{code: sqliteBusy} }

// TestRetryWithBackoffExhaustsTheSharedPolicy is the measured proof that the
// SQLite retry loop realises the shared policy and nothing of its own.
//
// Both halves are asserted, because each on its own survives the defect:
//
//   - the operation must be attempted backoff.Attempts times, so a loop that
//     runs out of attempts early is caught even if it slept correctly;
//   - the elapsed time must cover the whole ladder, so a loop that makes every
//     attempt but skips a sleep is caught too. That is defect #294 exactly: six
//     attempts' worth of intent, four waits' worth of patience.
func TestRetryWithBackoffExhaustsTheSharedPolicy(t *testing.T) {
	t.Parallel()

	calls := 0
	start := time.Now()
	err := retryWithBackoff("transaction", func() error {
		calls++
		return lockedErr()
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("retryWithBackoff returned nil for an operation that never stopped being locked")
	}
	if calls != backoff.Attempts {
		t.Errorf("the operation was attempted %d times, want %d (one initial attempt plus five retries)",
			calls, backoff.Attempts)
	}
	if floor := backoff.Total() - backoff.Total()/10; elapsed < floor {
		t.Errorf("a permanently locked operation gave up after %v; the shared policy sleeps about %v "+
			"(SPEC/IMPLEMENTATION.md § Retry Logic). A wait near %v means this site skips the sleep "+
			"before its last attempt, which is defect #294",
			elapsed, backoff.Total(), backoff.Total()-1000*time.Millisecond)
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
	start := time.Now()
	err := retryWithBackoff("creating schema", func() error {
		calls++
		return violation
	})
	elapsed := time.Since(start)

	if !errors.Is(err, error(violation)) {
		t.Errorf("retryWithBackoff returned %v, want the constraint violation unwrapped", err)
	}
	if strings.Contains(err.Error(), "failed after") {
		t.Errorf("a non-retryable failure was reported as an exhausted retry: %q", err.Error())
	}
	if calls != 1 {
		t.Errorf("a non-retryable failure was attempted %d times, want 1", calls)
	}
	if elapsed >= backoff.FirstDelay {
		t.Errorf("a non-retryable failure took %v; it must be returned without sleeping", elapsed)
	}
	if !IsUniqueConstraintErr(err) {
		t.Error("the unwrapped constraint violation must still classify as a uniqueness collision")
	}
}

// TestRetryWithBackoffStopsAtTheFirstSuccess pins the third outcome: an
// operation that wins the lock part-way through the ladder returns then, having
// slept only the rungs it needed.
func TestRetryWithBackoffStopsAtTheFirstSuccess(t *testing.T) {
	t.Parallel()

	const succeedOn = 3

	calls := 0
	start := time.Now()
	err := retryWithBackoff("configuring database", func() error {
		calls++
		if calls < succeedOn {
			return lockedErr()
		}
		return nil
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("retryWithBackoff returned %v for an operation that succeeded on attempt %d", err, succeedOn)
	}
	if calls != succeedOn {
		t.Errorf("the operation was attempted %d times, want %d", calls, succeedOn)
	}
	if elapsed >= backoff.Total() {
		t.Errorf("a success on attempt %d took %v; the loop must stop there rather than "+
			"running the ladder out (%v)", succeedOn, elapsed, backoff.Total())
	}
}
