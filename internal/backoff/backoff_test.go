package backoff

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// errContended stands for the contention every caller of Retry retries on: a
// SQLite busy/locked result, a held WAL directory lock, a conflicting advisory
// file lock. What it is does not matter here, only that a classifier accepts it.
var errContended = errors.New("resource is busy")

// errRejected stands for a failure that must NOT be waited on — a constraint
// violation, a syntax error, invalid input.
var errRejected = errors.New("invalid input")

// retryContended is the classifier the tests pair with errContended.
func retryContended(err error) bool { return errors.Is(err, errContended) }

// TestPolicyMatchesTheSpecification pins the ladder, the attempt count and the
// worst-case wait to the figures SPEC/IMPLEMENTATION.md § Retry Logic states:
// initial delay 100 ms, maximum delay 1000 ms, maximum retries 5, backoff
// pattern 100/200/400/800/1000 ms.
//
// It is the single place those numbers are asserted. They used to be pinned in
// internal/graphlock, next to one of three private copies of the policy, which
// is precisely why the assertion held while the other two copies drifted
// (defect #294): a test can only defend the constants it can see.
//
// The ladder is checked element by element rather than by its sum. A sum alone
// is satisfied by ladders that are not this one — five flat 500 ms waits reach
// 2500 ms too — and the shape is what the specification fixes.
func TestPolicyMatchesTheSpecification(t *testing.T) {
	t.Parallel()

	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1000 * time.Millisecond,
	}
	if got := slices.Collect(delays()); !slices.Equal(got, want) {
		t.Errorf("backoff ladder = %v, want %v (SPEC/IMPLEMENTATION.md § Retry Logic)", got, want)
	}

	// Six, not five: "Maximum retries: 5" counts retries, and a retry is an
	// attempt after the first. Reading it as five ATTEMPTS is defect #294.
	if got, want := Attempts, 6; got != want {
		t.Errorf("Attempts = %d, want %d (one initial attempt plus five retries)", got, want)
	}

	if got, want := FirstDelay, 100*time.Millisecond; got != want {
		t.Errorf("FirstDelay = %v, want %v (the ladder's first rung)", got, want)
	}

	if got, want := Total(), 2500*time.Millisecond; got != want {
		t.Errorf("Total() = %v, want %v (100+200+400+800+1000 ms)", got, want)
	}

	// Total must agree with the ladder it claims to sum, whatever the ladder
	// becomes.
	var sum time.Duration
	for _, delay := range want {
		sum += delay
	}
	if Total() != sum {
		t.Errorf("Total() = %v but the ladder sums to %v", Total(), sum)
	}
}

// TestRetryExhaustsTheWholePolicy is the measurement the four call sites are
// held to. It asserts both halves of the fix at once, because either on its own
// is satisfied by a broken loop:
//
//   - try must be called Attempts times — a loop that stops early attempts too
//     few operations, whatever it slept;
//   - the elapsed time must cover the whole ladder — a loop that makes six
//     attempts but skips a sleep gives up sooner than the specification allows,
//     which is exactly defect #294 (four waits, 1500 ms).
//
// Elapsed time is measured rather than derived from the constants: reading the
// constants back is what the drifted sites would still have passed.
func TestRetryExhaustsTheWholePolicy(t *testing.T) {
	t.Parallel()

	calls := 0
	start := time.Now()
	value, err := Retry(func() (int, error) {
		calls++
		return calls, errContended
	}, retryContended)
	elapsed := time.Since(start)

	if calls != Attempts {
		t.Errorf("try was called %d times, want %d (one initial attempt plus five retries)", calls, Attempts)
	}
	if !errors.Is(err, errContended) {
		t.Errorf("exhausted Retry returned %v, want the last error unwrapped (%v)", err, errContended)
	}
	if value != Attempts {
		t.Errorf("exhausted Retry returned value %d, want the last attempt's value %d", value, Attempts)
	}

	// A little slack below the nominal figure: time.Sleep guarantees a minimum,
	// but the comparison is against a clock read taken around the whole loop and
	// coarse timer resolution can shave a fraction off.
	if floor := Total() - Total()/10; elapsed < floor {
		t.Errorf("exhausted Retry took %v; it must sleep the whole ladder (about %v). "+
			"A wait near %v means the loop skips the sleep before its last attempt (defect #294)",
			elapsed, Total(), Total()-1000*time.Millisecond)
	}
}

// TestRetryReturnsSuccessWithoutSleeping pins the first of the three outcomes:
// an operation that succeeds is not delayed at all. A policy that slept before
// its first attempt would tax every uncontended call in the binary.
func TestRetryReturnsSuccessWithoutSleeping(t *testing.T) {
	t.Parallel()

	calls := 0
	start := time.Now()
	value, err := Retry(func() (string, error) {
		calls++
		return "opened", nil
	}, retryContended)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Retry returned %v for an operation that succeeded", err)
	}
	if value != "opened" {
		t.Errorf("Retry returned %q, want the successful attempt's value", value)
	}
	if calls != 1 {
		t.Errorf("try was called %d times for an immediate success, want 1", calls)
	}
	if elapsed >= FirstDelay {
		t.Errorf("an immediate success took %v; Retry must not sleep before the first attempt", elapsed)
	}
}

// TestRetrySucceedsOnALaterAttempt pins the middle of the ladder: a caller that
// wins the resource on the third attempt waits the first two delays and no
// more, and gets that attempt's value.
func TestRetrySucceedsOnALaterAttempt(t *testing.T) {
	t.Parallel()

	const succeedOn = 3

	calls := 0
	start := time.Now()
	value, err := Retry(func() (int, error) {
		calls++
		if calls < succeedOn {
			return 0, errContended
		}
		return calls, nil
	}, retryContended)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Retry returned %v for an operation that succeeded on attempt %d", err, succeedOn)
	}
	if value != succeedOn {
		t.Errorf("Retry returned %d, want %d", value, succeedOn)
	}
	if calls != succeedOn {
		t.Errorf("try was called %d times, want %d: Retry must stop at the first success", calls, succeedOn)
	}

	// Two sleeps precede the third attempt: 100 + 200 ms.
	const wantSlept = 300 * time.Millisecond
	if floor := wantSlept - wantSlept/10; elapsed < floor {
		t.Errorf("success on attempt %d took %v, want at least about %v (100+200 ms)", succeedOn, elapsed, wantSlept)
	}
	if elapsed >= Total() {
		t.Errorf("success on attempt %d took %v; Retry must stop sleeping once it succeeds, "+
			"not run the ladder out (%v)", succeedOn, elapsed, Total())
	}
}

// TestRetryDoesNotWaitOnARejectedError pins the third outcome and the retry
// SEMANTICS the timing fix had to leave untouched: an error the classifier
// rejects ends the loop at once, unwrapped and unslept. Waiting 2.5 s on a
// constraint violation would be a regression no timing assertion elsewhere
// would notice.
func TestRetryDoesNotWaitOnARejectedError(t *testing.T) {
	t.Parallel()

	calls := 0
	start := time.Now()
	_, err := Retry(func() (int, error) {
		calls++
		return 0, errRejected
	}, retryContended)
	elapsed := time.Since(start)

	if !errors.Is(err, errRejected) {
		t.Errorf("Retry returned %v, want the rejected error unwrapped (%v)", err, errRejected)
	}
	if calls != 1 {
		t.Errorf("try was called %d times for a non-retryable failure, want 1", calls)
	}
	if elapsed >= FirstDelay {
		t.Errorf("a non-retryable failure took %v; it must be returned without sleeping", elapsed)
	}
}

// TestRetryRejectionEndsTheLoopMidLadder guards the boundary between the two
// failure outcomes: a rejected error stops the loop wherever it appears, not
// only on the first attempt.
//
// It also pins the property callers use INSTEAD of extra API to tell exhaustion
// from rejection — the loop exhausts only on accepted errors and returns early
// only on a rejected one, so the classifier applied to the returned error
// recovers which happened. internal/db depends on exactly that to choose
// between its "failed after N attempts" diagnostic and returning the error bare.
func TestRetryRejectionEndsTheLoopMidLadder(t *testing.T) {
	t.Parallel()

	const rejectOn = 3

	calls := 0
	_, err := Retry(func() (int, error) {
		calls++
		if calls < rejectOn {
			return 0, errContended
		}
		return 0, errRejected
	}, retryContended)

	if !errors.Is(err, errRejected) {
		t.Errorf("Retry returned %v, want the rejected error that ended the loop (%v)", err, errRejected)
	}
	if calls != rejectOn {
		t.Errorf("try was called %d times, want %d: the loop must stop on the rejected error", calls, rejectOn)
	}
	if retryContended(err) {
		t.Error("the error returned after an early stop must be one the classifier REJECTS, " +
			"or callers cannot tell an early stop from an exhausted ladder")
	}
}

// TestAlwaysAcceptsEverything pins the classifier offered to callers whose only
// failure mode is contention.
func TestAlwaysAcceptsEverything(t *testing.T) {
	t.Parallel()

	for _, err := range []error{nil, errContended, errRejected, errors.New("anything at all")} {
		if !Always(err) {
			t.Errorf("Always(%v) = false, want true", err)
		}
	}
}

// TestBudgetedLadderMatchesTheRetryLadder is what makes RetryWithin a
// generalisation of Retry rather than a second policy standing beside it.
//
// Retry is expressed as RetryWithin(Total(), ...), so the SQLite layer's
// behaviour now depends on delaysWithin reproducing delays exactly at that one
// bound. It does, arithmetically — 100+200+400+800+1000 is 2500, which is
// Total, so the budget runs out precisely as the fifth rung ends and no rung is
// truncated — but arithmetic that holds today is what #294 was made of. This
// asserts it instead, element by element, so that a change to initialDelay,
// maxDelay or maxRetries which broke the coincidence would fail here rather
// than quietly lengthen or shorten every SQLite retry in the binary.
func TestBudgetedLadderMatchesTheRetryLadder(t *testing.T) {
	t.Parallel()

	retryLadder := slices.Collect(delays())
	budgeted := slices.Collect(delaysWithin(Total()))

	if !slices.Equal(budgeted, retryLadder) {
		t.Errorf("delaysWithin(Total()) = %v, want the retry ladder %v element for element. "+
			"Retry is RetryWithin over Total, so a divergence here changes the wait of every "+
			"SQLite retry in the binary (SPEC/IMPLEMENTATION.md § Retry Logic)", budgeted, retryLadder)
	}

	// Non-vacuity: the comparison must run on the real ladder, not on two empty
	// sequences that trivially agree.
	if len(retryLadder) != maxRetries {
		t.Fatalf("the retry ladder yielded %d rungs, want %d: the comparison above would be "+
			"vacuous", len(retryLadder), maxRetries)
	}
}

// TestBudgetedLadderSpendsExactlyItsBudget pins the two halves of the budgeted
// walk's contract, over bounds that fall in every interesting place relative to
// the ladder: shorter than the first rung, mid-ladder, exactly the SQLite total,
// and well past it into the flat 1000 ms tail.
//
//   - The rungs sum to exactly the bound. Not less — a walk that dropped the
//     rung that would overshoot would give up early, which is the very defect
//     the graph store lock's budget exists to prevent, reappearing one level
//     down. Not more — a walk that overshot would spend time the caller did not
//     grant it, and for the graph store lock that time comes out of the web
//     server's write timeout.
//   - Every rung except possibly the last is a rung of the shared ladder. Only
//     the final one may be truncated.
//
// A non-positive bound yields nothing, so RetryWithin attempts the operation
// once and never sleeps. That is the same rule as "no wait follows the final
// attempt", arrived at from the other end.
func TestBudgetedLadderSpendsExactlyItsBudget(t *testing.T) {
	t.Parallel()

	shape := slices.Collect(delaysWithin(20 * Total())) // enough rungs to compare against

	cases := []struct {
		name  string
		bound time.Duration
		want  time.Duration // total sleeping
	}{
		{"negative", -time.Second, 0},
		{"zero", 0, 0},
		{"shorter than the first rung", 40 * time.Millisecond, 40 * time.Millisecond},
		{"mid-ladder, truncating a rung", 250 * time.Millisecond, 250 * time.Millisecond},
		{"exactly the SQLite total", Total(), Total()},
		{"into the flat tail", Total() + 3500*time.Millisecond, Total() + 3500*time.Millisecond},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rungs := slices.Collect(delaysWithin(tc.bound))

			var sum time.Duration
			for _, rung := range rungs {
				sum += rung
			}
			if sum != tc.want {
				t.Errorf("delaysWithin(%v) sleeps %v in total (%v), want exactly %v",
					tc.bound, sum, rungs, tc.want)
			}

			// Every rung but the last must be the shared ladder's rung at that
			// position; the last may be truncated, and may only be SHORTER.
			for i, rung := range rungs {
				want := shape[i]
				if i < len(rungs)-1 && rung != want {
					t.Errorf("delaysWithin(%v) rung %d = %v, want the ladder's %v: only the FINAL "+
						"rung may be truncated", tc.bound, i, rung, want)
				}
				if rung > want {
					t.Errorf("delaysWithin(%v) rung %d = %v, longer than the ladder's %v: a rung "+
						"may be truncated, never lengthened", tc.bound, i, rung, want)
				}
			}
		})
	}
}

// TestRetryWithinHonoursItsBudgetRatherThanTheRetryCount is the measured half:
// the loop must climb delaysWithin(bound) and not the SQLite policy's five
// rungs.
//
// The budget chosen, 300 ms, is two whole rungs of the ladder and no more, so
// the two possible loops are separated by an order of magnitude in both
// observable quantities: honouring the budget makes 3 attempts in 300 ms, while
// silently running the SQLite ladder would make Attempts (6) in 2500 ms. A
// budget SMALLER than Total is used deliberately — it keeps the test cheap, and
// it fails loudly against the implementation this entry point replaced, which
// could only ever wait Total. That RetryWithin can also wait LONGER than Total
// is fenced where it matters, against the wall clock, in internal/graphlock.
func TestRetryWithinHonoursItsBudgetRatherThanTheRetryCount(t *testing.T) {
	t.Parallel()

	const budget = 300 * time.Millisecond

	calls := 0
	start := time.Now()
	value, err := RetryWithin(budget, func() (int, error) {
		calls++
		return calls, errContended
	}, retryContended)
	elapsed := time.Since(start)

	if want := 3; calls != want {
		t.Errorf("try was called %d times under a %v budget, want %d (one initial attempt plus the "+
			"two rungs that budget pays for). %d would mean the SQLite retry count was used instead "+
			"of the budget", calls, budget, want, Attempts)
	}
	if !errors.Is(err, errContended) {
		t.Errorf("exhausted RetryWithin returned %v, want the last error unwrapped (%v)", err, errContended)
	}
	if value != calls {
		t.Errorf("exhausted RetryWithin returned value %d, want the last attempt's value %d", value, calls)
	}

	if floor := budget - budget/10; elapsed < floor {
		t.Errorf("exhausted RetryWithin took %v; it must sleep its whole %v budget", elapsed, budget)
	}
	// The upper bound is what catches a loop that ignored the budget: half of
	// Total sits far above the budget and far below the ladder's own 2500 ms.
	if ceiling := Total() / 2; elapsed > ceiling {
		t.Errorf("exhausted RetryWithin took %v under a %v budget; anything approaching %v means the "+
			"budget was ignored and the SQLite ladder was climbed instead", elapsed, budget, Total())
	}
}

// TestRetryWithinKeepsTheThreeOutcomes asserts that parameterising the bound
// changed nothing else. The early-return outcomes belong to the loop, not to the
// ladder, and Retry now routes through this function, so a regression in either
// would reach every caller in the binary.
func TestRetryWithinKeepsTheThreeOutcomes(t *testing.T) {
	t.Parallel()

	// Succeeds at once: no sleeping, one attempt.
	calls := 0
	start := time.Now()
	value, err := RetryWithin(time.Hour, func() (int, error) {
		calls++
		return 7, nil
	}, retryContended)
	if err != nil || value != 7 || calls != 1 {
		t.Errorf("a successful operation returned (%d, %v) after %d attempts, want (7, nil) after 1", value, err, calls)
	}
	if elapsed := time.Since(start); elapsed >= FirstDelay {
		t.Errorf("a successful operation took %v; it must not sleep at all", elapsed)
	}

	// Fails with a rejected error: no sleeping, one attempt, error unwrapped.
	calls = 0
	start = time.Now()
	_, err = RetryWithin(time.Hour, func() (int, error) {
		calls++
		return 0, errRejected
	}, retryContended)
	if !errors.Is(err, errRejected) || calls != 1 {
		t.Errorf("a rejected error returned %v after %d attempts, want %v after 1", err, calls, errRejected)
	}
	if elapsed := time.Since(start); elapsed >= FirstDelay {
		t.Errorf("a rejected error took %v; a failure that cannot become a success must not be waited on", elapsed)
	}

	// Rejection mid-ladder ends the loop where it happens, budget or no budget.
	calls = 0
	_, err = RetryWithin(time.Hour, func() (int, error) {
		calls++
		if calls < 3 {
			return 0, errContended
		}
		return 0, errRejected
	}, retryContended)
	if !errors.Is(err, errRejected) || calls != 3 {
		t.Errorf("a rejection on the third attempt returned %v after %d attempts, want %v after 3", err, calls, errRejected)
	}
}
