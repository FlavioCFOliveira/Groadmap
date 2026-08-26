// Package backoff owns Groadmap's bounded exponential-backoff retry policy —
// the whole of it, and the only copy of it in the code.
//
// # What the policy is
//
// One initial attempt, then at most five retries, sleeping 100 ms, 200 ms,
// 400 ms, 800 ms and 1000 ms before them: six attempts, five waits, 2500 ms of
// sleeping in the worst case. SPEC/IMPLEMENTATION.md § Concurrency Model /
// Retry Logic specifies it for the SQLite layer, and § Graph Store Concurrency
// rule 4 reuses it verbatim for the graph store's shared read lock.
//
// # Why the LOOP lives here, and not merely the numbers
//
// Task #294. Three subsystems ran this policy and each wrote the loop out for
// itself. Two of them — internal/db's retryWithBackoff and internal/commands's
// openWALWriter — read the "5" as a count of ATTEMPTS and skipped the sleep
// before the last one, so they waited four times for 1500 ms while the
// specification, their own comments, and the third site all promised five waits
// for 2500 ms. The 1000 ms delay was never reached by either.
//
// Nothing caught it because the three loops were only ever compared by eye. A
// shared set of CONSTANTS would not have caught it either: the constants agreed
// all along, and the divergence was in the loop that consumed them. So the loop
// is what this package exports. A caller supplies the operation and the
// definition of a retryable failure; it does not get to say how many times or
// how long, because that is the decision that drifted.
//
// # Why it is a package of its own
//
// The three callers sit at three depths of the import graph and none can host
// it for the others: internal/commands imports both internal/db and
// internal/graphlock, while those two import neither. A leaf is therefore the
// only home that works, and this package imports nothing outside the standard
// library, so it can be imported from anywhere without forming a cycle. It is
// not folded into internal/utils because that package's charter is data
// handling — JSON, dates, paths, field validation — and a retry loop is control
// flow, which is the same separation-of-responsibilities reasoning that gave
// internal/graphlock its own package.
package backoff

import (
	"iter"
	"time"
)

const (
	// initialDelay is the wait before the first retry.
	initialDelay = 100 * time.Millisecond

	// maxDelay caps the doubling, so the ladder flattens rather than growing
	// without bound.
	maxDelay = 1000 * time.Millisecond

	// maxRetries counts RETRIES, not attempts, and the distinction is the whole
	// of defect #294. SPEC/IMPLEMENTATION.md § Retry Logic specifies "Maximum
	// retries: 5" and lists five delays; a retry is by definition an attempt
	// after the first, so the policy makes one initial attempt plus five
	// retries — see Attempts — and sleeps exactly once before each retry. It
	// never sleeps after the final attempt, which would buy nothing but delay
	// the failure.
	maxRetries = 5
)

// Attempts is the number of times a retried operation is tried in the worst
// case: one initial attempt plus maxRetries retries. It is derived rather than
// written out so it cannot disagree with the policy, and callers use it to
// report how many attempts were made.
const Attempts = 1 + maxRetries

// FirstDelay is the wait before the first retry, and so the shortest time this
// policy can delay anything by. It is exported for the assertion that an
// operation did NOT wait: comparing against it keeps that check tied to the
// policy, where naming 100 ms would make the check one more copy of it.
const FirstDelay = initialDelay

// Total returns the worst-case time an exhausted Retry spends sleeping: the sum
// of every delay in the ladder, 2500 ms under the current policy.
//
// It sums the ladder rather than stating a figure, so a change to the policy
// moves this value, every caller's diagnostics, and every test assertion
// together instead of leaving a stale number behind — which is how the four
// waits of #294 went on claiming to be five.
func Total() time.Duration {
	var total time.Duration
	for delay := range delays() {
		total += delay
	}
	return total
}

// delays yields the wait before each retry, in order: 100, 200, 400, 800 and
// 1000 ms. It is the single definition of the ladder — Retry sleeps it and
// Total sums it, so the two can never describe different policies.
func delays() iter.Seq[time.Duration] {
	return func(yield func(time.Duration) bool) {
		delay := initialDelay
		for range maxRetries {
			if !yield(delay) {
				return
			}
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}
}

// Retry runs try under the policy and returns its first success.
//
// try is called once and then again after each delay in the ladder, for as long
// as it keeps failing with an error retryable accepts — at most Attempts calls
// in all, with a sleep before each retry and none after the last attempt.
//
// Three outcomes, and only three:
//
//   - try succeeds on some attempt: that attempt's value is returned at once,
//     with a nil error, and no further sleeping happens;
//   - try fails with an error retryable REJECTS: that error is returned at once
//     and unwrapped, without sleeping. A constraint violation or a syntax error
//     must not be waited on;
//   - every attempt fails with an error retryable ACCEPTS: the LAST error is
//     returned, unwrapped, after Total of sleeping.
//
// Retry does not wrap, so callers keep their own diagnostics. They also keep
// the ability to tell the last two outcomes apart without extra API: the loop
// stops early only on an error retryable rejects and exhausts only on errors it
// accepts, so asking retryable about the returned error recovers exactly which
// happened.
//
// Classification is the caller's because only the caller knows what contention
// looks like in its subsystem — a SQLite busy/locked result code, a held WAL
// directory lock, a conflicting advisory file lock. Retry owns the timing and
// nothing else.
func Retry[T any](try func() (T, error), retryable func(error) bool) (T, error) {
	value, err := try()
	if err == nil || !retryable(err) {
		return value, err
	}

	for delay := range delays() {
		time.Sleep(delay)

		value, err = try()
		if err == nil || !retryable(err) {
			return value, err
		}
	}

	return value, err
}

// Always accepts every error as retryable. It is the classifier for a caller
// whose operation has exactly one failure mode, contention — opening a
// lock-guarded resource, say — so that such a caller states that fact
// explicitly rather than passing a nil predicate whose meaning has to be
// guessed.
func Always(error) bool { return true }
