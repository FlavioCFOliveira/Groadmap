// Package backoff owns Groadmap's bounded exponential-backoff retry policy —
// the whole of it, and the only copy of it in the code.
//
// # What the policy is
//
// One initial attempt, then retries, each preceded by the next rung of one
// delay ladder: 100 ms, 200 ms, 400 ms, 800 ms, then 1000 ms for as long as the
// ladder is walked. How FAR it is walked is the only thing an entry point
// varies:
//
//   - Retry stops after five retries — six attempts, five waits, 2500 ms of
//     sleeping in the worst case. SPEC/IMPLEMENTATION.md § Concurrency Model /
//     Retry Logic specifies exactly that, for the SQLite layer.
//   - RetryWithin keeps climbing the same ladder until a caller-supplied wait
//     budget is spent, truncating the final rung so that the sleeping totals
//     that budget exactly.
//
// Both walk one generator, so the two entry points can differ in how LONG they
// wait and never in HOW they wait. Retry is itself expressed as RetryWithin over
// the SQLite policy's total, and the two ladders are asserted to be elementwise
// identical, so the bound-parameterised entry point cannot drift away from the
// policy it generalises.
//
// # Why one caller supplies a budget of its own
//
// SPEC/IMPLEMENTATION.md § Graph Store Concurrency, "Write Contention and
// Recovery" rule 3, takes the LOOP and the LADDER from here for the graph
// store's lock and deliberately does not take the total. The two locks do not
// cover the same thing: no SQLite lock is held across a statement whose cost a
// caller chooses, because Groadmap issues every SQL statement itself, whereas
// the graph store lock is held across the statement its invocation carries. A
// hold may therefore lawfully last a whole statement budget, and a wait sized on
// 2500 ms is shorter than the hold it has to cover. The consequence is measured
// rather than feared: a lawful 4.71-second statement starved a contender that
// gave up after 2.5018 seconds (SPEC/GRAPH.md § Lock Contention).
//
// The budget itself is NOT this package's to hold. It is a property of how long
// that one lock may be held, which only internal/graphlock knows, so that
// package derives it and passes it in. This package owns the waiting and nothing
// about what is being waited for — the same division of labour that leaves
// classification to the caller.
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
// how long the ladder is climbed, because that is the decision that drifted.
//
// # Why it is a package of its own
//
// Four production call sites consume the policy — internal/db's
// retryWithBackoff, internal/commands's openWALWriter, internal/web's
// openGraphWALWriter, which joined after #294, and internal/graphlock's
// acquisition — and no one of those packages can host it for the others.
// internal/commands imports the other three; internal/web imports internal/db
// and internal/graphlock; internal/db and internal/graphlock import neither.
// Hosting the loop in any of them would point an import edge the wrong way for
// at least one caller. A leaf is therefore the only home that works, and this
// package imports nothing outside the standard library, so it can be imported
// from anywhere without forming a cycle. It is not folded into internal/utils
// because that package's charter is data handling — JSON, dates, paths, field
// validation — and a retry loop is control flow, which is the same
// separation-of-responsibilities reasoning that gave internal/graphlock its own
// package.
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
//
// It describes Retry. RetryWithin makes as many attempts as its budget pays
// for, which is why no caller of that entry point reports an attempt count.
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
//
// It is also the quantity internal/graphlock adds to its statement budget as the
// allowance for the fixed part of a hold, so this one figure sizes both waits.
func Total() time.Duration {
	var total time.Duration
	for delay := range delays() {
		total += delay
	}
	return total
}

// ladder yields the SHAPE of the policy's delay ladder, without end: 100, 200,
// 400 and 800 ms, then 1000 ms repeating, because the doubling is capped at
// maxDelay.
//
// It is unbounded on purpose. Where the ladder STOPS is the one thing the two
// entry points disagree about, so stopping is decided by the two consumers
// below, while the shape is generated exactly once. Writing a second generator
// for the budgeted walk would recreate defect #294 at one remove: two ladders
// that agree today, are compared only by eye, and drift apart later.
func ladder() iter.Seq[time.Duration] {
	return func(yield func(time.Duration) bool) {
		delay := initialDelay
		for {
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

// delays yields the wait before each retry under the SQLite policy, in order:
// 100, 200, 400, 800 and 1000 ms. It is the first maxRetries rungs of ladder,
// and it is what Total sums, so the policy's total can never describe a ladder
// Retry does not climb.
func delays() iter.Seq[time.Duration] {
	return func(yield func(time.Duration) bool) {
		remaining := maxRetries
		for delay := range ladder() {
			if remaining == 0 || !yield(delay) {
				return
			}
			remaining--
		}
	}
}

// delaysWithin yields the wait before each retry of a walk bounded by TIME
// rather than by a retry count: rungs of the same ladder until the cumulative
// sleep reaches bound, with the final rung TRUNCATED so that the sleeping totals
// exactly bound and never more.
//
// Truncating rather than dropping the overshooting rung is what makes the budget
// a promise in both directions: the caller waits the whole of what it asked for,
// and not a millisecond of it is spent after the budget is gone. A bound of zero
// or less yields nothing at all, so the operation is attempted once and never
// slept on — the same rule as "no wait follows the final attempt", reached from
// the other end.
//
// At bound == Total() this yields precisely what delays yields, since the
// SQLite policy's five rungs sum to that total exactly. That equivalence is what
// makes Retry safe to express in terms of RetryWithin, and it is asserted rather
// than argued (TestBudgetedLadderMatchesTheRetryLadder).
func delaysWithin(bound time.Duration) iter.Seq[time.Duration] {
	return func(yield func(time.Duration) bool) {
		var slept time.Duration
		for delay := range ladder() {
			remaining := bound - slept
			if remaining <= 0 {
				return
			}
			if delay > remaining {
				delay = remaining
			}
			if !yield(delay) {
				return
			}
			slept += delay
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
//
// It is RetryWithin over the SQLite policy's own total, which is the one bound
// under which the budgeted walk and the five-retry walk coincide. Expressing it
// this way leaves a single loop in the package: a separate implementation would
// be the second copy this package exists to prevent.
func Retry[T any](try func() (T, error), retryable func(error) bool) (T, error) {
	return RetryWithin(Total(), try, retryable)
}

// RetryWithin runs try under the same policy as Retry, but bounded by a wait
// budget the caller supplies instead of by the SQLite policy's retry count.
//
// The ladder is the same one, climbed the same way; only its end moves. try is
// called once immediately, then again after each rung of delaysWithin(bound),
// for as long as it keeps failing with an error retryable accepts. The three
// outcomes are Retry's three, unchanged, with one substitution: an exhausted
// call has slept exactly bound rather than exactly Total.
//
// It exists because one caller's wait must be sized against something this
// package cannot see. internal/graphlock holds the graph store's lock across a
// statement whose cost the caller chooses, so how long a hold may lawfully last
// — and therefore how long a waiter must be prepared to wait — is a fact about
// that lock, not about this policy (SPEC/GRAPH.md § Lock Contention;
// SPEC/IMPLEMENTATION.md § Graph Store Concurrency, "Write Contention and
// Recovery" rule 3). What the caller supplies is a duration, never a loop and
// never a ladder: the decision that drifted in #294 stays here.
//
// A bound of zero or less makes exactly one attempt.
func RetryWithin[T any](bound time.Duration, try func() (T, error), retryable func(error) bool) (T, error) {
	value, err := try()
	if err == nil || !retryable(err) {
		return value, err
	}

	for delay := range delaysWithin(bound) {
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
