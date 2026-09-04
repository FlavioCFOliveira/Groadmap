// Package graphserve — the in-flight checkpoint watch.
//
// A short-lived `rmp graph execute` invocation takes its checkpoint itself and
// prints the failure, and the server's SHUTDOWN checkpoint is reported by
// [shutdownCloser.Close]. Between those two, the in-flight checkpointer runs on a
// cadence nobody watches: its failures are recorded in checkpoint.Stats().LastError
// and, until this file, nothing in Groadmap ever read that field. A server could
// therefore run for days folding nothing, growing a write-ahead log the next open
// has to replay in full, and say nothing at all. rmp task #369 recorded that as
// DECISION #281 and left it to rmp task #370, which is this.
//
// SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process, rule 7,
// is what fixes the SHAPE of the report: a checkpoint failure after commits that
// are already durable is a diagnostic and not a failed statement. Nothing here
// changes an exit code, fails a write, or stops the server.
package graphserve

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/store/checkpoint"
)

// watchPeriodFloor is the shortest poll period the watch will use.
//
// It exists for the benchmark cadences alone. [checkpointCadence] may carry an
// interval of a few hundred milliseconds so that a fold is reachable inside a
// benchmark's lifetime, and halving a short enough interval reaches zero — which
// time.NewTicker does not accept — or a sub-millisecond period, which would spend
// a core spinning on Stats() to observe a checkpointer that fires far less often
// than that. One millisecond is the same floor the engine's own constructor
// applies when it derives Interval from MaxAge (checkpoint.New), so the two agree
// on the shortest cadence either will run.
const watchPeriodFloor = time.Millisecond

// watchPeriod is the poll period for a given cadence, and it is DERIVED rather
// than picked.
//
// Stats().LastError is a LEVEL and not a latch: every completed attempt records
// its own outcome through checkpoint.setErr, and a success records the empty
// string, so a failure is visible for exactly one attempt cycle and is then
// erased by the next attempt that succeeds. A sampler of a state that persists
// for one cycle needs at least TWO samples per cycle to be certain of catching
// it — one sample per cycle can land, every time, in the window the next attempt
// has already overwritten. The cycle here is the checkpointer's own interval,
// because that is the granularity at which it decides whether to fire, so the
// period is half of it.
//
// The residual, because sampling does not remove it: a failure, a repair, and the
// SAME failure again, all inside one period, are indistinguishable from a failure
// that simply persisted, and the watch stays silent through it. checkpoint.Stats
// carries a monotone success counter (Checkpoints) that would disambiguate it,
// and it is deliberately not used: what the reader acts on is whether folding is
// currently broken and with what error, which the level answers, and "it failed
// again with the same message" is not a further action.
func watchPeriod(cadence checkpointCadence) time.Duration {
	period := cadence.interval / 2
	if period < watchPeriodFloor {
		return watchPeriodFloor
	}
	return period
}

// checkpointHealth is the watch's state between samples, and it is separated from
// the goroutine so that the DECISION can be read, and driven, on its own: a
// transition table is a thing to test directly, and a ticker is not.
//
// It is owned by the polling goroutine alone and carries no synchronisation of
// its own. [checkpointWatch.stop] joins that goroutine before returning, so a
// caller that has stopped the watch may read this without a race.
type checkpointHealth struct {
	// lastErr is the error text this state was last reported for. It is the
	// identity of the failure, not merely a flag: a DIFFERENT failure while
	// already failing is a new fact and is reported again.
	lastErr string
	// failing is whether the last sample reported a failure. It is not derivable
	// from lastErr alone — a repair sets lastErr back to the empty string, which
	// is also its initial value, and the two must not be confused.
	failing bool
}

// observe folds one sample of the checkpointer's statistics into the health state
// and reports the record to emit, if any.
//
// The whole of the policy is this table, and it is written out because the reason
// for each row differs:
//
//	previous | sample                     | emitted
//	---------+----------------------------+-------------------------------------
//	healthy  | LastError empty            | nothing
//	healthy  | LastError non-empty        | slog.LevelError, once
//	failing  | the SAME LastError         | nothing — a level that has not
//	         |                            | changed is not news
//	failing  | a DIFFERENT non-empty text | slog.LevelError again — a different
//	         |                            | failure is a new fact
//	failing  | LastError empty            | slog.LevelInfo — a later checkpoint
//	         |                            | repaired it
//
// Two properties of the table are load-bearing rather than incidental:
//
//   - The healthy path emits NOTHING, ever. stderr for a successful
//     `rmp graph serve` is the two warnings the engine emits at construction and
//     nothing else, and it stays byte-identical: the end-to-end suite asserts on
//     that stream and this feature must not be visible in it.
//   - A repair is reported at INFO and not at ERROR, because it is the fact that
//     ends the incident. A reader who saw the ERROR needs to know the fold is
//     happening again; a reader who did not see it needs nothing, and INFO is
//     where a fact that is not a problem belongs.
func (h *checkpointHealth) observe(s checkpoint.Stats) (level slog.Level, message string, emit bool) {
	switch {
	case s.LastError == "":
		if !h.failing {
			return 0, "", false
		}
		h.failing = false
		h.lastErr = ""
		return slog.LevelInfo, checkpointRepairedMessage, true

	case h.failing && s.LastError == h.lastErr:
		return 0, "", false

	default:
		h.failing = true
		h.lastErr = s.LastError
		return slog.LevelError, checkpointFailedMessage, true
	}
}

// checkpointFailedMessage is what an in-flight checkpoint failure reads as, and
// every clause of it is something the reader can act on.
//
// It says what is NOT at risk first, because that is the question a durability
// diagnostic raises and leaving it unanswered would make a diagnostic read as
// data loss: the commit protocol made every acknowledged commit durable in the
// write-ahead log before it acknowledged it, and recovery replays that log
// (SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process, rules 1
// and 7). It then says what DID not happen — the fold — and what that costs,
// which is a log that keeps growing and an open that replays more of it. Those
// are the two quantities that grow while the condition lasts, and they are the
// reason the message exists at all.
const checkpointFailedMessage = "an in-flight graph checkpoint failed; every acknowledged commit is " +
	"still durable in the write-ahead log and the next open recovers it, but the log was not folded " +
	"into the snapshot, so it keeps growing and the next open replays more of it"

// checkpointRepairedMessage closes the incident the message above opened. It
// names the same two quantities, so a reader matching the pair does not have to
// infer that "succeeded" undoes "failed".
const checkpointRepairedMessage = "a later in-flight graph checkpoint succeeded; the write-ahead log " +
	"is being folded into the snapshot again"

// checkpointWatch polls a checkpointer's statistics and reports what
// [checkpointHealth.observe] decides.
//
// # Why the statistics arrive as a function
//
// stats is injected rather than taken as a *checkpoint.Checkpointer so that the
// decision and the loop are drivable without a real checkpointer — and, more to
// the point, without waiting for one to fire. A checkpointer that has to be
// PROVOKED into failing is a fault-injection problem in the filesystem; a
// function that returns a checkpoint.Stats value is a table. The production wiring
// passes cp.Stats, which is the method value of the real checkpointer, so nothing
// is faked on the path that ships.
//
// # The goroutine leak boundary
//
// This is one, and it is the reason stop both closes and JOINS. The watch is
// owned by [shutdownCloser], which the engine's server closes after its drain;
// once Close has returned, this package must own no running goroutine at all, or
// `go test -race` sees a goroutine still calling Stats() on a checkpointer the
// composed store is closing underneath it. stop is idempotent because Close is:
// the engine may reach it from either of its two drained exit paths, and Run
// closes it directly on the startup paths where nothing was ever served.
type checkpointWatch struct {
	stats  func() checkpoint.Stats
	log    *slog.Logger
	stopCh chan struct{}
	doneCh chan struct{}
	health checkpointHealth
	every  time.Duration
	// running records that the goroutine was actually launched, so stop does not
	// wait on a done channel nothing will ever close. It is atomic because start
	// runs on the goroutine that assembles the server and stop runs on whichever
	// of the engine's exit paths reaches the closer first.
	running   atomic.Bool
	startOnce sync.Once
	stopOnce  sync.Once
}

// newCheckpointWatch builds a watch over stats, polling every the given period
// and reporting through log. It starts nothing; see start.
func newCheckpointWatch(stats func() checkpoint.Stats, every time.Duration, log *slog.Logger) *checkpointWatch {
	return &checkpointWatch{
		stats:  stats,
		log:    log,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		every:  every,
	}
}

// start launches the polling goroutine, once however many times it is called.
func (w *checkpointWatch) start() {
	w.startOnce.Do(func() {
		w.running.Store(true)
		go w.run()
	})
}

// stop ends the polling goroutine and waits for it to return.
//
// It is idempotent, and it JOINS rather than merely signalling: a watch that had
// been asked to stop but was still sampling would outlive the server that owns
// it and would read a checkpointer being torn down beneath it. Calling it on a
// watch that was never started is safe and returns at once.
func (w *checkpointWatch) stop() {
	w.stopOnce.Do(func() { close(w.stopCh) })
	if w.running.Load() {
		<-w.doneCh
	}
}

// run is the poll loop.
//
// The ticker is the only clock: nothing here reacts to a checkpoint, because the
// checkpointer offers no notification and Stats() is the whole of its
// observability surface. A tick that finds nothing changed emits nothing, which
// is what keeps a healthy server's stderr exactly as quiet as it was before this
// existed.
func (w *checkpointWatch) run() {
	defer close(w.doneCh)

	ticker := time.NewTicker(w.every)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.sample()
		}
	}
}

// sample takes one reading and emits whatever the transition table decides.
//
// The error text is taken from the SAMPLE and not from the health state, because
// the state's copy is cleared on the repair transition: the two are the same
// string on every row that reports an error, and reading the sample keeps the
// attribute and the decision derived from one value.
func (w *checkpointWatch) sample() {
	s := w.stats()
	level, message, emit := w.health.observe(s)
	if !emit {
		return
	}
	if level == slog.LevelError {
		w.log.Error(message, slog.String("err", s.LastError))
		return
	}
	w.log.Info(message)
}
