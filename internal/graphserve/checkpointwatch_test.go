// Package graphserve — the tests for the in-flight checkpoint watch and for the
// two values rmp task #370 measured rather than guessed.
//
// # What these tests are for
//
// The watch exists because an in-flight checkpoint failure was unobservable:
// checkpoint.Stats().LastError is a LEVEL that the next completed attempt
// overwrites, so a failure that a later success repaired cannot be found after
// the fact, and nothing in Groadmap read the field at all (rmp task #369,
// DECISION #281). What replaces that silence is a transition table and a polling
// goroutine, and the two are separated in the code precisely so they can be
// driven apart here: the table is a pure function of a sample and a state, and
// the goroutine is a lifetime.
//
// Three properties are pinned, and each has a distinct failure mode:
//
//   - The TABLE. Every row, and the sequences between rows, because the rows are
//     not independent: the state a row leaves behind is the state the next row is
//     read against, and the one row that is easy to get wrong — a repair followed
//     by another healthy sample — is a sequence and not a row.
//   - The LIFETIME. stop must join, must be safe before start, and must be safe
//     twice, because this package must own no running goroutine once the engine's
//     server has closed its Closer. A watch that outlived the server would sample
//     a checkpointer the composed store is tearing down underneath it, and `go
//     test -race` is where that surfaces.
//   - The SEAM's payoff. That an in-flight checkpoint actually fires and folds the
//     log, driven end to end against a real server. Nothing could test that before
//     the cadence became a parameter: at five minutes the shortest waitable
//     interval was longer than any test may run (rmp task #369, FINDING #282).
//
// # What is deliberately not mocked
//
// The store, the engine and the checkpointer. The watch's stats function is
// injected, which is the one substitution here, and it is a substitution for a
// FAULT rather than for a component: provoking a real checkpointer into failing is
// a filesystem fault-injection problem, while a function returning a
// checkpoint.Stats value is a table. Production passes cp.Stats, the real
// checkpointer's own method value, so the injected seam is not on the path that
// ships — and the test that matters most here, the fold below, uses no seam at
// all.
package graphserve

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/store/checkpoint"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
)

// step is one sample fed to [checkpointHealth.observe] together with everything
// that sample must produce: the record, if any, and the state it must leave
// behind.
//
// The state is asserted after EVERY step and not only at the end of a sequence,
// because a table whose emissions are right and whose state is wrong is a table
// that reports correctly once and then diverges — which is exactly the defect a
// sequence of samples exists to catch.
type step struct {
	sample      string
	wantMessage string
	wantLastErr string
	wantLevel   slog.Level
	wantEmit    bool
	wantFailing bool
}

// healthySample is a sample reporting no failure while the state is already
// healthy: the row that must emit nothing, and the row a running server produces
// for its whole life.
func healthySample() step {
	return step{sample: "", wantEmit: false, wantFailing: false, wantLastErr: ""}
}

// failureSample is a sample reporting a failure the state has not reported yet,
// whether the state is healthy or already failing with a DIFFERENT text. Both are
// new facts and both are reported.
func failureSample(text string) step {
	return step{
		sample:      text,
		wantEmit:    true,
		wantLevel:   slog.LevelError,
		wantMessage: checkpointFailedMessage,
		wantFailing: true,
		wantLastErr: text,
	}
}

// sameFailureSample is a sample repeating the failure the state already carries:
// a level that has not changed, which is not news.
func sameFailureSample(text string) step {
	return step{sample: text, wantEmit: false, wantFailing: true, wantLastErr: text}
}

// repairSample is the empty LastError arriving while the state is failing, which
// is the fact that ends the incident.
func repairSample() step {
	return step{
		sample:      "",
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
// from "healthy" — and the two rows that matter most are reachable only from
// "failing". The last case is the one that catches the specific confusion the
// `failing` field exists to prevent: after a repair, lastErr is the empty string
// again, which is ALSO its initial value, so an implementation that decided
// "healthy" by testing lastErr == "" would report the repair a second time on the
// very next healthy sample.
func TestCheckpointHealth_ObservesTheTransitionTableItDocuments(t *testing.T) {
	const (
		readOnly = "the snapshot directory is read-only"
		noSpace  = "no space left on device"
	)

	cases := []struct {
		name  string
		why   string
		steps []step
	}{
		{
			name:  "healthy to healthy is silent",
			why:   "a healthy server's stderr must stay byte-identical to what it was before the watch existed",
			steps: []step{healthySample(), healthySample(), healthySample()},
		},
		{
			name:  "healthy to failing emits ERROR once",
			why:   "the first failure is the fact the reader acts on, and the repeat is not a second fact",
			steps: []step{failureSample(readOnly), sameFailureSample(readOnly), sameFailureSample(readOnly)},
		},
		{
			name:  "failing to the same failure is silent",
			why:   "a level that has not changed is not news, and the poller resamples it every period",
			steps: []step{failureSample(noSpace), sameFailureSample(noSpace)},
		},
		{
			name:  "failing to a different failure emits ERROR again",
			why:   "the identity of the failure changed, which is a new fact even though the level did not",
			steps: []step{failureSample(readOnly), failureSample(noSpace), sameFailureSample(noSpace)},
		},
		{
			name:  "failing to healthy emits INFO",
			why:   "a later fold succeeded, which is the fact that ends the incident the ERROR opened",
			steps: []step{failureSample(readOnly), repairSample()},
		},
		{
			name:  "a repair followed by another healthy sample emits nothing",
			why:   "lastErr is empty after a repair AND in the initial state; only `failing` tells them apart",
			steps: []step{failureSample(readOnly), repairSample(), healthySample(), healthySample()},
		},
		{
			name:  "healthy failing healthy failing emits ERROR INFO ERROR",
			why:   "an incident that recurs is reported again; nothing latches the first one",
			steps: []step{failureSample(readOnly), repairSample(), failureSample(readOnly), sameFailureSample(readOnly)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var health checkpointHealth

			for i, want := range tc.steps {
				level, message, emit := health.observe(checkpoint.Stats{LastError: want.sample})

				if emit != want.wantEmit {
					t.Fatalf("step %d (sample %q): emit = %v, want %v. %s",
						i, want.sample, emit, want.wantEmit, tc.why)
				}
				if emit {
					if level != want.wantLevel {
						t.Errorf("step %d (sample %q): level = %v, want %v. %s",
							i, want.sample, level, want.wantLevel, tc.why)
					}
					if message != want.wantMessage {
						t.Errorf("step %d (sample %q): message = %q, want %q",
							i, want.sample, message, want.wantMessage)
					}
				}
				if health.failing != want.wantFailing {
					t.Errorf("step %d (sample %q): state failing = %v, want %v. The state a step "+
						"leaves behind is what the next step is read against",
						i, want.sample, health.failing, want.wantFailing)
				}
				if health.lastErr != want.wantLastErr {
					t.Errorf("step %d (sample %q): state lastErr = %q, want %q",
						i, want.sample, health.lastErr, want.wantLastErr)
				}
			}
		})
	}
}

// TestWatchPeriod_IsHalfTheIntervalAboveTheFloorAndTheFloorBelowIt pins the
// derivation, including the two inputs that would otherwise produce a period
// time.NewTicker refuses.
//
// Half is not a taste. Stats().LastError persists for exactly one attempt cycle
// and is then overwritten by the next attempt, so one sample per cycle can land
// every time in the window the next attempt has already cleared; two samples per
// cycle is the least that is certain to catch a state that lasts one.
//
// The zero case is the one with teeth. A disabled cadence carries a zero interval
// — see [checkpointCadence] — and time.NewTicker PANICS on a non-positive period,
// so a derivation that returned zero would turn a configuration this package
// supports into a crash. The floor is asserted by constructing the ticker as well
// as by comparing the number, because the number alone would still be right on a
// day somebody changed what the ticker accepts.
func TestWatchPeriod_IsHalfTheIntervalAboveTheFloorAndTheFloorBelowIt(t *testing.T) {
	cases := []struct {
		name    string
		cadence checkpointCadence
		want    time.Duration
	}{
		{
			name:    "the production cadence halves",
			cadence: productionCadence(),
			want:    37500 * time.Millisecond,
		},
		{
			name:    "a benchmark cadence halves",
			cadence: checkpointCadence{maxAge: time.Second, interval: 250 * time.Millisecond},
			want:    125 * time.Millisecond,
		},
		{
			name:    "twice the floor halves exactly onto it",
			cadence: checkpointCadence{maxAge: 8 * time.Millisecond, interval: 2 * time.Millisecond},
			want:    watchPeriodFloor,
		},
		{
			name:    "a sub-2ms interval takes the floor rather than half",
			cadence: checkpointCadence{maxAge: 4 * time.Millisecond, interval: time.Millisecond},
			want:    watchPeriodFloor,
		},
		{
			name:    "a zero interval takes the floor rather than zero",
			cadence: checkpointCadence{},
			want:    watchPeriodFloor,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := watchPeriod(tc.cadence)
			if got != tc.want {
				t.Errorf("watchPeriod(%+v) = %v, want %v", tc.cadence, got, tc.want)
			}
			if got <= 0 {
				t.Fatalf("watchPeriod(%+v) = %v, which time.NewTicker refuses", tc.cadence, got)
			}
			// The refusal is a panic, so the only assertion that proves the
			// period is usable is to use it.
			ticker := time.NewTicker(got)
			ticker.Stop()
		})
	}
}

// TestProductionCadence_EnablesBothTheTriggerAndTheTicker asserts the two
// durations `rmp graph serve` ships with are both live.
//
// Zero has a different meaning in each field and both meanings disable something.
// A zero maxAge disables age-based triggering outright. A zero interval is
// quieter and worse: checkpoint.New derives Interval from MaxAge only when
// Interval is zero AND MaxAge is positive, and the loop builds no ticker at all
// for a non-positive interval — so a production cadence that lost its interval
// would keep a MaxAge nothing ever checks, and the in-flight fold would silently
// never happen. Both are asserted, and so is the ordering that makes the pair a
// cadence rather than two unrelated numbers.
func TestProductionCadence_EnablesBothTheTriggerAndTheTicker(t *testing.T) {
	cadence := productionCadence()

	if cadence.maxAge <= 0 {
		t.Errorf("productionCadence().maxAge = %v; a non-positive maxAge disables age-based "+
			"triggering, so the server would never fold its write-ahead log in flight "+
			"(SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process, rule 6)",
			cadence.maxAge)
	}
	if cadence.interval <= 0 {
		t.Errorf("productionCadence().interval = %v; the engine's loop builds NO TICKER for a "+
			"non-positive interval, so the maxAge above would never be checked and no in-flight "+
			"fold would ever happen", cadence.interval)
	}
	if cadence.interval >= cadence.maxAge {
		t.Errorf("productionCadence() has interval %v >= maxAge %v; the fold is owed at maxAge and "+
			"taken at the first tick after it, so an interval at or above maxAge doubles the "+
			"effective staleness bound", cadence.interval, cadence.maxAge)
	}
}

// TestBuild_StartsNoWatchWhenTheCadenceIsDisabled pins the guard [build] puts on
// the watch, which property mutation 5 of rmp task #370's matrix showed NO test
// caught: a build that started the watch unconditionally passed the whole suite.
//
// # Why the property is worth a test rather than left to the reader
//
// The guard is not tidiness. Under a disabled cadence the engine builds neither
// ticker nor trigger, so there is no in-flight attempt to observe and the watch
// would sample a level nothing ever writes. Worse, [watchPeriod] FLOORS the poll
// period at watchPeriodFloor, so the watch a missing guard would start under a
// zero cadence is the FASTEST one this package can build: a Stats() call every
// millisecond. Every benchmark here runs with a disabled cadence — that is how
// the write-ahead log's growth is measured without a fold truncating it
// underneath the measurement — so a watch started there would poll a thousand
// times a second for the whole of every run, adding noise to the very numbers
// [productionCadence]'s comment cites.
//
// # Why the enabled half is in the same test
//
// The nil assertion alone is VACUOUS against a build that started no watch under
// ANY cadence: it would pass, and the watch would be dead code nothing noticed.
// What is being pinned is a GUARD, so both of its sides are asserted here, and
// the second half is what makes the first mean "not started BECAUSE the cadence
// is disabled" rather than "never started at all".
func TestBuild_StartsNoWatchWhenTheCadenceIsDisabled(t *testing.T) {
	if disabled := buildOverAFreshStore(t, checkpointCadence{}); disabled.watch != nil {
		t.Errorf("build started a checkpoint watch under a disabled cadence. Nothing is in "+
			"flight to observe under one, and watchPeriod floors the period at %v, so the watch "+
			"samples a checkpointer that never fires once every millisecond for the life of the "+
			"server — which, under the benchmarks of this package, runs for the whole of the "+
			"measurement", watchPeriodFloor)
	}

	if enabled := buildOverAFreshStore(t, productionCadence()); enabled.watch == nil {
		t.Errorf("build started NO checkpoint watch under the production cadence, so an " +
			"in-flight checkpoint failure is unobservable again (rmp task #369, DECISION #281). " +
			"This half is what stops the assertion above from passing on a build that never " +
			"starts a watch at all")
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
// Close — which stops the watch and takes the shutdown checkpoint, both of which
// need the store still open — runs before the store's own close.
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
		_ = closer.Close() //nolint:errcheck // this test's assertions are about the watch, not the teardown
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

// syncBuffer is a bytes.Buffer a test may read while the polling goroutine writes
// to it.
//
// The alternative — swapping the package's `logger` variable — is not available
// and must not be made available: it is global mutable state, two tests running in
// parallel would fight over it, and `go test -race` would report the fight rather
// than the property under test. Constructing a *slog.Logger over this and handing
// it to newCheckpointWatch is the injection the watch already supports.
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

// stopWithin calls stop and fails if it has not returned inside the budget.
//
// stop JOINS the polling goroutine, so a regression in it does not return a wrong
// answer: it blocks forever. Called directly, that would hang the whole test
// binary until the framework's own timeout killed it, and the panic would name
// every goroutine in the process rather than the property that broke. Running it
// in a goroutine and racing a timer is what turns a hang into a named failure.
func stopWithin(t *testing.T, w *checkpointWatch, budget time.Duration, which string) {
	t.Helper()

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		w.stop()
	}()

	select {
	case <-returned:
	case <-time.After(budget):
		t.Fatalf("%s did not return within %v. stop joins the polling goroutine, so a stop that "+
			"never returns is a watch that will outlive the server that owns it", which, budget)
	}
}

// TestCheckpointWatch_StopBeforeStartReturnsWithoutBlocking is the case the
// `running` flag exists for.
//
// [build] starts a watch only when the cadence is enabled, and [shutdownCloser]
// stops whatever watch it holds; Run also closes the closer on the startup paths
// where nothing was ever served. A stop that unconditionally waited on doneCh
// would therefore block forever on a watch nothing had launched, and it would do
// it inside the server's shutdown.
func TestCheckpointWatch_StopBeforeStartReturnsWithoutBlocking(t *testing.T) {
	w := newCheckpointWatch(
		func() checkpoint.Stats { return checkpoint.Stats{} },
		watchPeriodFloor, discardLogger())

	stopWithin(t, w, 5*time.Second, "stop on a watch that was never started")
}

// TestCheckpointWatch_StopIsIdempotent covers the way the shutdown actually
// reaches it.
//
// [shutdownCloser.Close] is closed by the engine's server from whichever of its
// two drained exit paths gets there first, and Run closes it directly on the
// startup failure paths. The once-guard on Close makes the second call cheap, but
// the teardown helper in this package's own tests closes it again as belt and
// braces, so a second stop is a path that is actually taken. Closing an already
// closed channel panics, which is what this asserts does not happen.
func TestCheckpointWatch_StopIsIdempotent(t *testing.T) {
	w := newCheckpointWatch(
		func() checkpoint.Stats { return checkpoint.Stats{} },
		watchPeriodFloor, discardLogger())
	w.start()

	stopWithin(t, w, 5*time.Second, "the first stop")
	stopWithin(t, w, 5*time.Second, "the second stop")
}

// TestCheckpointWatch_StopJoinsThePollingGoroutine proves the join rather than
// asserting it.
//
// This is the goroutine-leak boundary of the package: once the engine's server
// has closed its Closer, nothing here may still be running, or a surviving
// sampler calls Stats() on a checkpointer the composed store is closing beneath
// it.
//
// # Why a counter alone does not prove it, which cost this test a rewrite
//
// The obvious form — count the samples, stop, wait, require the count not to have
// moved — is VACUOUS against the mutation it exists to catch. A stop that only
// signalled would also let the goroutine reach its stop channel within
// microseconds, so by the time the test read the counter the goroutine had
// already exited and the count was stable either way. The test passed with the
// join deleted, which is the definition of not testing it.
//
// What makes the difference observable is holding a sample IN FLIGHT across the
// stop. The injected stats function blocks until this test releases it, so at the
// moment stop is called the goroutine is inside Stats() and cannot reach its stop
// channel. A stop that joins CANNOT return there; a stop that only signals returns
// at once, and that is the assertion below. The counter then closes the second
// half: once stop has returned, and with the sample released and the loop free to
// run at its full period, no further sample may complete.
func TestCheckpointWatch_StopJoinsThePollingGoroutine(t *testing.T) {
	const (
		period       = watchPeriodFloor
		inFlight     = 250 * time.Millisecond
		settleMargin = 200
	)

	// entered reports that the goroutine is inside the sample; release is what
	// lets that sample finish. The send is non-blocking and the channel is
	// buffered, so a sample after the release costs nothing.
	entered := make(chan struct{}, 1)
	release := make(chan struct{})

	// Released on every exit path, including a failure: a test that gave up while
	// the sample was blocked would otherwise leave the polling goroutine parked
	// there for the rest of the binary's life.
	var releaseOnce sync.Once
	releaseSample := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseSample)

	var samples atomic.Int64
	w := newCheckpointWatch(func() checkpoint.Stats {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		samples.Add(1)
		return checkpoint.Stats{}
	}, period, discardLogger())

	w.start()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatalf("the watch took no sample in 10s at a %v period; it is not polling at all", period)
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		w.stop()
	}()

	// The sample is still running. stop must be waiting for it.
	select {
	case <-stopped:
		t.Fatalf("stop returned while a sample was still in flight. It must JOIN the polling " +
			"goroutine and not merely signal it: a sampler that outlives the server's Closer " +
			"reads a checkpointer the composed store is tearing down beneath it")
	case <-time.After(inFlight):
	}

	releaseSample()

	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatalf("stop did not return within 10s of the in-flight sample being released")
	}

	atStop := samples.Load()
	time.Sleep(settleMargin * period)

	if after := samples.Load(); after != atStop {
		t.Errorf("the watch took %d further samples in the %v after stop returned; the polling "+
			"goroutine is still alive", after-atStop, settleMargin*period)
	}
}

// TestCheckpointWatch_ReportsAFailureAndThenItsRepair drives the loop, the table
// and the logger together, which is the only place the three meet.
//
// The records are asserted in full: the level, the message, the ORDER, and the
// `err` attribute carrying the sample's own text. The attribute matters on its
// own — the message says what a failed fold costs and says nothing about which
// failure happened, so a record without the attribute tells a reader that
// something broke and gives them nothing to act on.
//
// The error text is read from the SAMPLE rather than from the health state, in
// the code, because the state's copy is cleared on the repair transition; this is
// what would catch a regression that read the state instead and emitted an empty
// attribute.
func TestCheckpointWatch_ReportsAFailureAndThenItsRepair(t *testing.T) {
	const failure = "the snapshot directory is read-only"

	var sampled atomic.Pointer[string]
	broken := failure
	sampled.Store(&broken)

	buf := &syncBuffer{}
	w := newCheckpointWatch(
		func() checkpoint.Stats { return checkpoint.Stats{LastError: *sampled.Load()} },
		watchPeriodFloor,
		slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})))

	w.start()
	defer w.stop()

	waitForRecord(t, buf, checkpointFailedMessage, "the failure")

	repaired := ""
	sampled.Store(&repaired)

	waitForRecord(t, buf, checkpointRepairedMessage, "the repair")

	// Join before reading the whole stream, so what is examined below is the
	// complete output and not a prefix of it.
	stopWithin(t, w, 5*time.Second, "stop after the repair")
	out := buf.String()

	if !strings.Contains(out, `err="`+failure+`"`) {
		t.Errorf("the ERROR record carries no err attribute with the sample's text %q. Output:\n%s",
			failure, out)
	}
	if !strings.Contains(out, "level=ERROR") {
		t.Errorf("the failure was not reported at ERROR. Output:\n%s", out)
	}
	if !strings.Contains(out, "level=INFO") {
		t.Errorf("the repair was not reported at INFO. A repair is the fact that ends the "+
			"incident, and INFO is where a fact that is not a problem belongs. Output:\n%s", out)
	}

	failedAt := strings.Index(out, checkpointFailedMessage)
	repairedAt := strings.Index(out, checkpointRepairedMessage)
	if failedAt < 0 || repairedAt < 0 || repairedAt < failedAt {
		t.Errorf("the two records are out of order (failure at %d, repair at %d). Output:\n%s",
			failedAt, repairedAt, out)
	}

	// And nothing else. The healthy samples before the repair transition and
	// every sample after it must emit nothing at all.
	if got := strings.Count(out, checkpointFailedMessage); got != 1 {
		t.Errorf("the failure was reported %d times, want exactly 1: a level that has not changed "+
			"is not news, and the poller resamples it every %v. Output:\n%s",
			got, watchPeriodFloor, out)
	}
	if got := strings.Count(out, checkpointRepairedMessage); got != 1 {
		t.Errorf("the repair was reported %d times, want exactly 1. lastErr is empty after a "+
			"repair AND in the initial state, so a table that decided health from it alone would "+
			"report the repair again on the next healthy sample. Output:\n%s", got, out)
	}
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
		time.Sleep(watchPeriodFloor)
	}
}

// TestServedServer_FoldsTheWriteAheadLogWhileItServes is what the cadence seam
// was built for, and it is the one test here that substitutes nothing at all.
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
