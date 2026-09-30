// Package graphserve — the tests for the in-flight checkpoint and for the two
// values rmp task #370 measured rather than guessed.
//
// # What these tests are for
//
// The in-flight fold is Groadmap's own since rmp task #383: a goroutine that
// ticks on the cadence and asks the gate for a fold at every due instant, and a
// transition table that reports what each attempted fold returned. The two are
// separated in the code precisely so they can be driven apart here: the table is
// a pure function of an outcome and a state, and the goroutine is a lifetime and
// a timing.
//
// Four properties are pinned, and each has a distinct failure mode:
//
//   - The TABLE. Every row, and the sequences between rows, because the rows are
//     not independent: the state a row leaves behind is the state the next row is
//     read against, and the one row that is easy to get wrong — a repair followed
//     by another success — is a sequence and not a row.
//   - The TIMING. A due fold the gate withholds is not a fold, so the cadence
//     stays due; a fold that ran — succeeded or failed — is one, so the next is
//     due only maxAge later (SPEC/GRAPH.md § Durability and Checkpointing in a
//     Long-Lived Process, rules 9 and 10).
//   - The LIFETIME. stop must join, must be safe before start, and must be safe
//     twice, because this package must own no running goroutine once the engine's
//     server has closed its Closer, and the shutdown checkpoint must not begin
//     while an in-flight fold is still running.
//   - The SEAM's payoff. That an in-flight checkpoint actually fires and folds the
//     log, driven end to end against a real server. Nothing could test that before
//     the cadence became a parameter: at five minutes the shortest waitable
//     interval was longer than any test may run (rmp task #369, FINDING #282).
//
// # What is deliberately not mocked
//
// The store, the engine and the checkpointer. The fold function is injected in
// the table, timing and lifetime tests, which is the one substitution here, and
// it is a substitution for a FAULT or for a gate's answer rather than for a
// component: provoking a real checkpointer into failing is a filesystem
// fault-injection problem, while a function returning an error is a table.
// Production passes the gate's own method value, so the injected seam is not on
// the path that ships — and the tests that matter most here, the folds of a real
// server in inflight_fold_test.go and below, use no seam at all.
package graphserve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/store/snapshot"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
)

// Failures a test's own fold reports. They are package-level sentinels because
// err113 forbids constructing one at the call site, and because a test must be
// able to say that THIS error came back out rather than some error.
var (
	errReadOnly = errors.New("the snapshot directory is read-only")
	errNoSpace  = errors.New("no space left on device")
	// errFieldTooLong is the snapshot format's own sentinel, wrapped the way the
	// engine wraps it, so the classification is exercised by errors.Is on the
	// real value and never by text.
	errFieldTooLong = fmt.Errorf("capture: %w: property value is 2147483648 bytes, maximum 1073741824",
		snapshot.ErrFieldTooLong)
)

// step is one fold outcome fed to [checkpointHealth.observe] together with
// everything that outcome must produce: the record, if any, and the state it
// must leave behind.
//
// The state is asserted after EVERY step and not only at the end of a sequence,
// because a table whose emissions are right and whose state is wrong is a table
// that reports correctly once and then diverges — which is exactly the defect a
// sequence of outcomes exists to catch.
type step struct {
	outcome     error
	wantMessage string
	wantLastErr string
	wantLevel   slog.Level
	wantEmit    bool
	wantFailing bool
}

// success is a fold that succeeded while the state is already healthy: the row
// that must emit nothing, and the row a running server produces for its whole
// life.
func success() step {
	return step{outcome: nil, wantEmit: false, wantFailing: false, wantLastErr: ""}
}

// failure is a fold that failed with a failure the state has not reported yet,
// whether the state is healthy or already failing with a DIFFERENT text. Both are
// new facts and both are reported.
func failure(err error) step {
	return step{
		outcome:     err,
		wantEmit:    true,
		wantLevel:   slog.LevelError,
		wantMessage: checkpointFailedMessage,
		wantFailing: true,
		wantLastErr: err.Error(),
	}
}

// sameFailure is a fold that failed with the failure the state already carries:
// a condition that has not changed, which is not news.
func sameFailure(err error) step {
	return step{outcome: err, wantEmit: false, wantFailing: true, wantLastErr: err.Error()}
}

// refusedField is a fold the snapshot format refused for an over-long field,
// which is reported at EVERY attempt whatever the state.
func refusedField() step {
	return step{
		outcome:     errFieldTooLong,
		wantEmit:    true,
		wantLevel:   slog.LevelError,
		wantMessage: inFlightFieldTooLongMessage(),
		wantFailing: true,
		wantLastErr: errFieldTooLong.Error(),
	}
}

// repair is a fold that succeeded while the state is failing, which is the fact
// that ends the incident.
func repair() step {
	return step{
		outcome:     nil,
		wantEmit:    true,
		wantLevel:   slog.LevelInfo,
		wantMessage: checkpointRepairedMessage,
		wantFailing: false,
		wantLastErr: "",
	}
}

// TestCheckpointHealth_ObservesTheTransitionTableItDocuments drives every row of
// the table in [checkpointHealth.observe], and the sequences between the rows.
//
// The sequences are the point. Every row is a transition out of a state, so a row
// tested from a freshly zeroed state tests only the half of the table reachable
// from "healthy" — and the rows that matter most are reachable only from
// "failing". The case "a repair followed by another success" catches the specific
// confusion the `failing` field exists to prevent: after a repair, lastErr is the
// empty string again, which is ALSO its initial value, so an implementation that
// decided "healthy" by testing lastErr == "" would report the repair a second
// time on the very next success.
func TestCheckpointHealth_ObservesTheTransitionTableItDocuments(t *testing.T) {
	cases := []struct {
		name  string
		why   string
		steps []step
	}{
		{
			name:  "success to success is silent",
			why:   "a healthy server's stderr must stay byte-identical to what it was before the fold was Groadmap's",
			steps: []step{success(), success(), success()},
		},
		{
			name:  "healthy to failing emits ERROR once",
			why:   "the first failure is the fact the reader acts on, and the repeat is not a second fact",
			steps: []step{failure(errReadOnly), sameFailure(errReadOnly), sameFailure(errReadOnly)},
		},
		{
			name:  "failing to a different failure emits ERROR again",
			why:   "the identity of the failure changed, which is a new fact",
			steps: []step{failure(errReadOnly), failure(errNoSpace), sameFailure(errNoSpace)},
		},
		{
			name:  "failing to success emits INFO",
			why:   "a later fold succeeded, which is the fact that ends the incident the ERROR opened",
			steps: []step{failure(errReadOnly), repair()},
		},
		{
			name:  "a repair followed by another success emits nothing",
			why:   "lastErr is empty after a repair AND in the initial state; only `failing` tells them apart",
			steps: []step{failure(errReadOnly), repair(), success(), success()},
		},
		{
			name:  "an incident that recurs is reported again",
			why:   "nothing latches the first incident",
			steps: []step{failure(errReadOnly), repair(), failure(errReadOnly), sameFailure(errReadOnly)},
		},
		{
			name: "a refused field is reported at every attempt",
			why: "the log still holds the unfolded bytes, so every due instant attempts the fold " +
				"again and every attempt refuses; the report must recur with each (SPEC/GRAPH.md " +
				"§ Field Length Limits, rule 9)",
			steps: []step{refusedField(), refusedField(), refusedField()},
		},
		{
			name:  "a refused field after a general failure is still its own condition",
			why:   "the unhealable condition must never be absorbed into the general one's silence",
			steps: []step{failure(errReadOnly), refusedField(), refusedField(), repair()},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var health checkpointHealth

			for i, want := range tc.steps {
				level, message, emit := health.observe(want.outcome)

				if emit != want.wantEmit {
					t.Fatalf("step %d (outcome %v): emit = %v, want %v. %s",
						i, want.outcome, emit, want.wantEmit, tc.why)
				}
				if emit {
					if level != want.wantLevel {
						t.Errorf("step %d (outcome %v): level = %v, want %v. %s",
							i, want.outcome, level, want.wantLevel, tc.why)
					}
					if message != want.wantMessage {
						t.Errorf("step %d (outcome %v): message = %q, want %q",
							i, want.outcome, message, want.wantMessage)
					}
				}
				if health.failing != want.wantFailing {
					t.Errorf("step %d (outcome %v): state failing = %v, want %v. The state a step "+
						"leaves behind is what the next step is read against",
						i, want.outcome, health.failing, want.wantFailing)
				}
				if health.lastErr != want.wantLastErr {
					t.Errorf("step %d (outcome %v): state lastErr = %q, want %q",
						i, want.outcome, health.lastErr, want.wantLastErr)
				}
			}
		})
	}
}

// TestTickInterval_IsTheCadencesOwnOrAQuarterOfMaxAgeAboveTheFloor pins the
// derivation, including the inputs that would otherwise produce a period
// time.NewTicker refuses — which it refuses by PANICKING, so the period is also
// used, not only compared.
func TestTickInterval_IsTheCadencesOwnOrAQuarterOfMaxAgeAboveTheFloor(t *testing.T) {
	cases := []struct {
		name    string
		cadence checkpointCadence
		want    time.Duration
	}{
		{name: "the production cadence keeps its interval", cadence: productionCadence(), want: 75 * time.Second},
		{
			name:    "a stated interval is used as it is",
			cadence: checkpointCadence{maxAge: time.Second, interval: 250 * time.Millisecond},
			want:    250 * time.Millisecond,
		},
		{
			name:    "no interval takes a quarter of maxAge, as the engine derived it",
			cadence: checkpointCadence{maxAge: time.Second},
			want:    250 * time.Millisecond,
		},
		{
			name:    "a quarter below the floor takes the floor",
			cadence: checkpointCadence{maxAge: 2 * time.Millisecond},
			want:    intervalFloor,
		},
		{name: "a zero cadence takes the floor rather than zero", cadence: checkpointCadence{}, want: intervalFloor},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tickInterval(tc.cadence)
			if got != tc.want {
				t.Errorf("tickInterval(%+v) = %v, want %v", tc.cadence, got, tc.want)
			}
			ticker := time.NewTicker(got)
			ticker.Stop()
		})
	}
}

// TestProductionCadence_EnablesBothTheTriggerAndTheTicker asserts the two
// durations `rmp graph serve` ships with are both live, and ordered.
//
// A zero maxAge disables the in-flight fold outright: [build] starts none. The
// interval is the real granularity of the cadence, and an interval at or above
// maxAge doubles the effective staleness bound, because a fold becomes due at
// maxAge and is taken at the first tick after it.
func TestProductionCadence_EnablesBothTheTriggerAndTheTicker(t *testing.T) {
	cadence := productionCadence()

	if cadence.maxAge <= 0 {
		t.Errorf("productionCadence().maxAge = %v; a non-positive maxAge disables the in-flight "+
			"fold, so the server would never fold its write-ahead log while it runs "+
			"(SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process, rule 5)",
			cadence.maxAge)
	}
	if cadence.interval <= 0 {
		t.Errorf("productionCadence().interval = %v; the interval is stated rather than derived "+
			"(see checkpointCadence)", cadence.interval)
	}
	if cadence.interval >= cadence.maxAge {
		t.Errorf("productionCadence() has interval %v >= maxAge %v; the fold is due at maxAge and "+
			"taken at the first tick after it, so an interval at or above maxAge doubles the "+
			"effective staleness bound", cadence.interval, cadence.maxAge)
	}
}

// TestBuild_StartsTheInFlightFoldOnlyWhenTheCadenceIsEnabled pins the guard
// [build] puts on the in-flight fold, BOTH of its sides: a nil assertion alone
// would pass on a build that never started one at all.
//
// Under a disabled cadence no fold is ever due, so a goroutine started there
// would tick — at the floor, since tickInterval floors a zero cadence — for the
// whole life of every server a test or benchmark runs with the fold disabled,
// which is how the write-ahead log is held still under a measurement.
func TestBuild_StartsTheInFlightFoldOnlyWhenTheCadenceIsEnabled(t *testing.T) {
	if disabled := buildOverAFreshStore(t, checkpointCadence{}); disabled.inFlight != nil {
		t.Errorf("build started an in-flight fold under a disabled cadence, which ticks every %v "+
			"for the life of the server and never finds a fold due", intervalFloor)
	}

	if enabled := buildOverAFreshStore(t, productionCadence()); enabled.inFlight == nil {
		t.Errorf("build started NO in-flight fold under the production cadence, so a running " +
			"server would never fold its write-ahead log (SPEC/GRAPH.md § Durability and " +
			"Checkpointing in a Long-Lived Process, rule 5). This half is what stops the " +
			"assertion above from passing on a build that never starts one at all")
	}
}

// scriptedFold is a fold function whose answers a test writes: each call takes
// the next answer, and the last answer repeats. It counts its calls.
type scriptedFold struct {
	answers []foldAnswer
	calls   atomic.Int64
}

// foldAnswer is one answer of a [scriptedFold].
type foldAnswer struct {
	err error
	ran bool
}

func (f *scriptedFold) fold() (bool, error) {
	n := int(f.calls.Add(1)) - 1
	if n >= len(f.answers) {
		n = len(f.answers) - 1
	}
	return f.answers[n].ran, f.answers[n].err
}

// waitForCount blocks until counter reaches want, or fails.
func waitForCount(t *testing.T, counter *atomic.Int64, want int64, which string) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for counter.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("%s reached %d within 15s, want at least %d", which, counter.Load(), want)
		}
		time.Sleep(intervalFloor)
	}
}

// TestInFlightCheckpoint_AWithheldFoldKeepsTheCadenceDue is the timing rule of
// rmp task #383: a due fold the gate withholds is not a fold.
//
// The cadence below makes a fold due only an HOUR after the last one, and ticks
// every millisecond. The gate withholds the first several consultations and then
// lets one through. If a withheld consultation counted as a fold, the second
// consultation would be an hour away and the test would see exactly one. It must
// instead see a consultation at every tick until the fold runs — and then none,
// because a fold that ran IS a fold and the next is an hour away.
func TestInFlightCheckpoint_AWithheldFoldKeepsTheCadenceDue(t *testing.T) {
	const withheld = 5

	answers := make([]foldAnswer, 0, withheld+1)
	for range withheld {
		answers = append(answers, foldAnswer{ran: false})
	}
	answers = append(answers, foldAnswer{ran: true})
	script := &scriptedFold{answers: answers}

	c := newInFlightCheckpoint(script.fold,
		checkpointCadence{maxAge: time.Hour, interval: intervalFloor}, discardLogger())
	c.start()
	defer stopWithin(t, c, 5*time.Second, "stop")

	waitForCount(t, &c.attempts, 1, "the folds that ran")

	// A fold ran, so the next is due an hour from now: however many further
	// ticks pass, the gate is not consulted again.
	time.Sleep(200 * intervalFloor)
	stopWithin(t, c, 5*time.Second, "stop after the fold")

	if got := script.calls.Load(); got != withheld+1 {
		t.Errorf("the gate was consulted %d times, want %d: %d withheld consultations at "+
			"consecutive ticks, then the one that folded, then none for the hour the cadence "+
			"leaves after a fold. Fewer means a withheld fold was counted as a fold, so a write "+
			"arriving after an idle interval would wait a whole cadence longer than on a server "+
			"that had never been idle", got, withheld+1, withheld)
	}
	if got := c.due.Load(); got != withheld+1 {
		t.Errorf("due counted %d instants, want %d", got, withheld+1)
	}
	if got := c.attempts.Load(); got != 1 {
		t.Errorf("attempts counted %d folds, want 1: a withheld consultation is not an attempt", got)
	}
}

// TestInFlightCheckpoint_AFailedFoldWaitsForTheNextDueInstant is the other side
// of the rule above: a fold that ran and FAILED is a fold, so it is attempted
// again at the next due instant and not at every tick. A driver that retried at
// every tick would rewrite a whole snapshot — or re-serialise a whole graph only
// to be refused again — once per millisecond here.
func TestInFlightCheckpoint_AFailedFoldWaitsForTheNextDueInstant(t *testing.T) {
	script := &scriptedFold{answers: []foldAnswer{{ran: false, err: errReadOnly}}}

	c := newInFlightCheckpoint(script.fold,
		checkpointCadence{maxAge: time.Hour, interval: intervalFloor}, discardLogger())
	c.start()

	waitForCount(t, &c.attempts, 1, "the folds attempted")
	time.Sleep(200 * intervalFloor)
	stopWithin(t, c, 5*time.Second, "stop")

	if got := script.calls.Load(); got != 1 {
		t.Errorf("a failed fold was attempted %d times inside one cadence, want 1", got)
	}
}

// TestInFlightCheckpoint_ReportsAFailureAndThenItsRepair drives the loop, the
// table and the logger together, which is the only place the three meet.
//
// The records are asserted in full: the level, the message, the ORDER, and the
// `err` attribute carrying the fold's own error. The attribute matters on its
// own — the message says what a failed fold costs and says nothing about which
// failure happened, so a record without the attribute tells a reader that
// something broke and gives them nothing to act on.
func TestInFlightCheckpoint_ReportsAFailureAndThenItsRepair(t *testing.T) {
	script := &scriptedFold{answers: []foldAnswer{
		{err: errReadOnly}, {err: errReadOnly}, {ran: true},
	}}

	buf := &syncBuffer{}
	c := newInFlightCheckpoint(script.fold,
		checkpointCadence{maxAge: intervalFloor, interval: intervalFloor},
		slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})))

	c.start()
	waitForRecord(t, buf, checkpointRepairedMessage, "the repair")
	waitForCount(t, &c.attempts, 5, "the folds attempted")

	// Join before reading the whole stream, so what is examined below is the
	// complete output and not a prefix of it.
	stopWithin(t, c, 5*time.Second, "stop after the repair")
	out := buf.String()

	if !strings.Contains(out, `err="`+errReadOnly.Error()+`"`) {
		t.Errorf("the ERROR record carries no err attribute with the fold's error %q. Output:\n%s",
			errReadOnly, out)
	}
	failedAt := strings.Index(out, checkpointFailedMessage)
	repairedAt := strings.Index(out, checkpointRepairedMessage)
	if failedAt < 0 || repairedAt < 0 || repairedAt < failedAt {
		t.Errorf("the two records are out of order (failure at %d, repair at %d). Output:\n%s",
			failedAt, repairedAt, out)
	}
	if got := strings.Count(out, checkpointFailedMessage); got != 1 {
		t.Errorf("the failure was reported %d times, want exactly 1: two attempts failed the same "+
			"way, and a condition that has not changed is not news. Output:\n%s", got, out)
	}
	if got := strings.Count(out, checkpointRepairedMessage); got != 1 {
		t.Errorf("the repair was reported %d times, want exactly 1: every later fold succeeded "+
			"too, and a table that decided health from lastErr alone would report each. "+
			"Output:\n%s", got, out)
	}
}

// TestInFlightCheckpoint_ReportsARefusedFieldAtEveryAttempt is rmp task #383's
// requirement on the in-flight path, which the engine's own loop could not meet:
// its failure reached Groadmap as a rendered string, which errors.Is cannot
// match. Groadmap now holds the error, so the field-too-long condition is
// classified here exactly as it is at shutdown, and reported at every attempt
// (SPEC/GRAPH.md § Field Length Limits, rules 8 and 9).
func TestInFlightCheckpoint_ReportsARefusedFieldAtEveryAttempt(t *testing.T) {
	const attempts = 3

	script := &scriptedFold{answers: []foldAnswer{{err: errFieldTooLong}}}
	buf := &syncBuffer{}
	c := newInFlightCheckpoint(script.fold,
		checkpointCadence{maxAge: intervalFloor, interval: intervalFloor},
		slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})))

	c.start()
	waitForCount(t, &c.attempts, attempts, "the folds attempted")
	stopWithin(t, c, 5*time.Second, "stop")
	out := buf.String()

	reports := strings.Count(out, inFlightFieldTooLongMessage())
	if int64(reports) != c.attempts.Load() {
		t.Errorf("%d attempts were refused for an over-long field and %d reported it; every attempt "+
			"must report the condition that cannot heal. Output:\n%s", c.attempts.Load(), reports, out)
	}
	if strings.Contains(out, checkpointFailedMessage) {
		t.Errorf("the unhealable condition was reported as the general failure. Output:\n%s", out)
	}
	if !strings.Contains(out, "an in-flight graph checkpoint was refused") {
		t.Errorf("the report does not name the in-flight checkpoint, so it cannot be told apart "+
			"from the shutdown checkpoint's on the same stream. Output:\n%s", out)
	}
}

// stopWithin calls stop and fails if it has not returned inside the budget.
//
// stop JOINS the fold goroutine, so a regression in it does not return a wrong
// answer: it blocks forever. Called directly, that would hang the whole test
// binary until the framework's own timeout killed it, and the panic would name
// every goroutine in the process rather than the property that broke. Running it
// in a goroutine and racing a timer is what turns a hang into a named failure.
func stopWithin(t *testing.T, c *inFlightCheckpoint, budget time.Duration, which string) {
	t.Helper()

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		c.stop()
	}()

	select {
	case <-returned:
	case <-time.After(budget):
		t.Fatalf("%s did not return within %v. stop joins the fold goroutine, so a stop that "+
			"never returns is an in-flight fold that will outlive the server that owns it", which, budget)
	}
}

// TestInFlightCheckpoint_StopBeforeStartReturnsWithoutBlocking is the case the
// `running` flag exists for: Run closes the closer on the startup paths where
// nothing was ever served, and a stop that unconditionally waited on doneCh would
// block forever there.
func TestInFlightCheckpoint_StopBeforeStartReturnsWithoutBlocking(t *testing.T) {
	c := newInFlightCheckpoint(func() (bool, error) { return false, nil },
		checkpointCadence{maxAge: intervalFloor}, discardLogger())

	stopWithin(t, c, 5*time.Second, "stop on an in-flight fold that was never started")
}

// TestInFlightCheckpoint_StopIsIdempotent covers the way the shutdown actually
// reaches it: the engine's server closes the closer from whichever of its two
// drained exit paths gets there first, and this package's teardown helper closes
// it again. Closing an already closed channel panics, which is what this asserts
// does not happen.
func TestInFlightCheckpoint_StopIsIdempotent(t *testing.T) {
	c := newInFlightCheckpoint(func() (bool, error) { return false, nil },
		checkpointCadence{maxAge: intervalFloor}, discardLogger())
	c.start()

	stopWithin(t, c, 5*time.Second, "the first stop")
	stopWithin(t, c, 5*time.Second, "the second stop")
}

// TestInFlightCheckpoint_StopJoinsAFoldInProgress proves the join rather than
// asserting it, and it proves the property the shutdown relies on: the shutdown
// checkpoint is taken only once no in-flight fold is still running
// (SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process, rule 10).
//
// A counter alone would be vacuous: a stop that only signalled would also let the
// goroutine reach its stop channel within microseconds. What makes the difference
// observable is holding a fold IN FLIGHT across the stop. The injected fold
// blocks until this test releases it, so at the moment stop is called the
// goroutine is inside the fold and cannot reach its stop channel. A stop that
// joins CANNOT return there; a stop that only signals returns at once.
func TestInFlightCheckpoint_StopJoinsAFoldInProgress(t *testing.T) {
	const inFlight = 250 * time.Millisecond

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseFold := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseFold)

	var folds atomic.Int64
	c := newInFlightCheckpoint(func() (bool, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		folds.Add(1)
		return true, nil
	}, checkpointCadence{maxAge: intervalFloor, interval: intervalFloor}, discardLogger())

	c.start()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatalf("the in-flight fold was not attempted in 10s at a %v cadence", intervalFloor)
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		c.stop()
	}()

	select {
	case <-stopped:
		t.Fatalf("stop returned while a fold was still in progress. It must JOIN the fold " +
			"goroutine: the shutdown checkpoint that follows it would otherwise run beside an " +
			"in-flight fold, which the specification forbids")
	case <-time.After(inFlight):
	}

	releaseFold()

	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatalf("stop did not return within 10s of the in-flight fold being released")
	}

	atStop := folds.Load()
	time.Sleep(200 * intervalFloor)
	if after := folds.Load(); after != atStop {
		t.Errorf("%d further folds ran after stop returned; the fold goroutine is still alive",
			after-atStop)
	}
}

// buildOverAFreshStore opens a real store over a fresh graph directory and builds
// the server over it, returning the closer and registering the teardown.
//
// It is the store-and-build prefix of [startRealServerAt] without the socket: the
// caller above needs the [shutdownCloser] that [build] returns and nothing that
// listens, so nothing here binds, serves, or connects.
//
// The two cleanups are registered in the order the teardown of this package's
// other tests runs them. t.Cleanup is last-registered-first, so the closer's
// Close — which stops the in-flight fold and takes the shutdown checkpoint, both
// of which need the store still open — runs before the store's own close.
func buildOverAFreshStore(t *testing.T, cadence checkpointCadence) *shutdownCloser {
	t.Helper()

	graphDir := filepath.Join(t.TempDir(), "graph")
	if err := os.MkdirAll(graphDir, 0700); err != nil {
		t.Fatalf("creating %s: %v", graphDir, err)
	}

	hold, err := graphstore.Acquire(graphDir)
	if err != nil {
		t.Fatalf("taking the graph store lock: %v", err)
	}
	// Hold.Open releases the hold itself on failure, so there is nothing to
	// release on the error path below.
	st, err := hold.Open()
	if err != nil {
		t.Fatalf("opening the graph store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close() //nolint:errcheck // releases the advisory hold whatever the log reports
	})

	closer, _, err := build(st, graphDir, cadence, logger)
	t.Cleanup(func() {
		_ = closer.Close() //nolint:errcheck // this test's assertions are about the in-flight fold, not the teardown
	})
	if err != nil {
		t.Fatalf("building the server over %s: %v", graphDir, err)
	}

	return closer
}

// TestServerOptions_QuotasTransactionsAtTheConnectionCeiling pins the two
// capacity numbers the server is constructed with.
//
// The equality is the documented decision: there is exactly one principal, so a
// transaction quota BELOW the connection ceiling would refuse a BEGIN from a
// client the ceiling had already admitted.
//
// The POSITIVITY assertions are load-bearing rather than defensive, and they are
// not the same assertion twice. The engine reads MaxOpenTxPerPrincipal in three
// bands: zero takes its own default of 2048, a positive value is the quota, and a
// NEGATIVE value DISABLES enforcement entirely — newTxQuota builds a quota with no
// map and admits everything. So an edit that made this value negative would read
// as tightening a limit and would in fact remove it, and only a sign check
// catches that. MaxConnections has the same shape with a different outcome: zero
// and negative both take the engine's 1024, which is eight times what this
// package chose.
func TestServerOptions_QuotasTransactionsAtTheConnectionCeiling(t *testing.T) {
	opts := serverOptions(nil, logger)

	if opts.MaxConnections != maxConnections {
		t.Errorf("MaxConnections = %d, want %d (SPEC/GRAPH.md § Server Options)",
			opts.MaxConnections, maxConnections)
	}
	if opts.MaxConnections <= 0 {
		t.Errorf("MaxConnections = %d; the engine takes its own default of 1024 for zero AND for "+
			"any negative value, so a non-positive value here silently restores a ceiling eight "+
			"times the one this package measured", opts.MaxConnections)
	}
	if opts.MaxOpenTxPerPrincipal != opts.MaxConnections {
		t.Errorf("MaxOpenTxPerPrincipal = %d, want it EQUAL to MaxConnections (%d): there is one "+
			"principal, so a quota below the connection ceiling refuses a BEGIN from a client the "+
			"ceiling already admitted",
			opts.MaxOpenTxPerPrincipal, opts.MaxConnections)
	}
	if opts.MaxOpenTxPerPrincipal <= 0 {
		t.Errorf("MaxOpenTxPerPrincipal = %d; the engine treats a NEGATIVE value as disabling "+
			"enforcement entirely, so a value that reads as a tighter limit would in fact remove "+
			"the quota rather than tighten it", opts.MaxOpenTxPerPrincipal)
	}
}

// syncBuffer is a bytes.Buffer a test may read while the fold goroutine writes
// to it.
//
// The alternative — swapping the package's `logger` variable — is not available
// and must not be made available: it is global mutable state, two tests running in
// parallel would fight over it, and `go test -race` would report the fight rather
// than the property under test. Constructing a *slog.Logger over this and handing
// it to newInFlightCheckpoint is the injection the fold already supports.
type syncBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// discardLogger is the logger for the lifetime tests, which assert about
// goroutines rather than about records.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// waitForRecord blocks until the log stream contains message, or fails.
func waitForRecord(t *testing.T, buf *syncBuffer, message, which string) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for {
		if strings.Contains(buf.String(), message) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s was not reported within 15s. Output so far:\n%s", which, buf.String())
		}
		time.Sleep(intervalFloor)
	}
}

// TestServedServer_FoldsTheWriteAheadLogWhileItServes is what the cadence seam
// was built for, and it substitutes nothing at all.
//
// # Why it could not exist before
//
// rmp task #369 recorded it as FINDING #282's blocker: the cadence was a package
// constant of five minutes, so the shortest interval a test could wait for an
// in-flight fold was five minutes, and every assertion about the in-flight
// checkpointer would have been an assertion about a path that never ran inside a
// test. Making the cadence a parameter is what removed the blocker, and this is
// the test the removal was for.
//
// # What it asserts, and why the log's SIZE is the right observable
//
// A fold is three things a test outside the process cannot see — a capture, a
// snapshot write, and a prefix truncation — and exactly one thing it can: the
// write-ahead log gets SHORTER. Nothing else in the system shortens it. So the
// test writes until the log has demonstrably grown, then waits for it to shrink
// below that high-water mark, and a shrink is a fold that reached phase 3.
//
// The high-water mark is tracked DURING the writes rather than read once after
// them, because a fold landing between the last write and a single reading would
// leave a mark of zero and the test would then be waiting for the log to fall
// below nothing. The wait is a poll against a generous deadline rather than a
// sleep of one cadence: what is asserted is that a fold happens, not when.
func TestServedServer_FoldsTheWriteAheadLogWhileItServes(t *testing.T) {
	const (
		writes   = 40
		foldWait = 60 * time.Second
	)

	socket, graphDir, stop := startRealServerAt(t, checkpointCadence{
		maxAge:   500 * time.Millisecond,
		interval: 100 * time.Millisecond,
	})
	defer stop()

	var high int64
	for i := 0; i < writes; i++ {
		statement := "CREATE (n:Folded {seq: " + itoa(i) + ", kind: 'in-flight-checkpoint'})"
		if _, err := graphclient.Send(context.Background(), socket, statement); err != nil {
			t.Fatalf("write %d of %d failed: %v", i, writes, err)
		}
		if size := walBytesAt(graphDir); size > high {
			high = size
		}
	}

	if high == 0 {
		t.Fatalf("the write-ahead log at %s/wal never held a byte across %d acknowledged writes; "+
			"the commits reached no disk (SPEC/GRAPH.md § Durability and Checkpointing in a "+
			"Long-Lived Process, rule 1)", graphDir, writes)
	}

	deadline := time.Now().Add(foldWait)
	for {
		if size := walBytesAt(graphDir); size < high {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the write-ahead log at %s/wal is still %d bytes after %v, having peaked at "+
				"%d, on a cadence owing a fold every 500ms and looking every 100ms. An in-flight "+
				"checkpoint truncates the log prefix, so a log that never shrinks is a "+
				"checkpointer that never folded (SPEC/GRAPH.md § Durability and Checkpointing in "+
				"a Long-Lived Process, rule 6)",
				graphDir, walBytesAt(graphDir), foldWait, high)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// walBytesAt reports the size of the write-ahead log under graphDir, or zero when
// it cannot be stated.
//
// A missing log is zero and not a failure: the file is created on the first
// append, so a store that has been read and never written has none.
func walBytesAt(graphDir string) int64 {
	info, err := os.Stat(filepath.Join(graphDir, "wal"))
	if err != nil {
		return 0
	}
	return info.Size()
}
