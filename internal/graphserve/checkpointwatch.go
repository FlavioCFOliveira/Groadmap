// Package graphserve — the in-flight checkpoint.
//
// A server folds the write-ahead log into the snapshot at two moments: at
// shutdown, which [shutdownCloser.Close] performs and reports, and while it
// runs, at the instants a cadence makes a fold due. This file is the second.
//
// It began as a WATCH. The in-flight fold used to be the engine's own loop, which
// folded on its own timer and exposed its last failure only as a rendered string
// in checkpoint.Stats().LastError, so all Groadmap could do was poll that level
// and report what it saw (rmp task #369, DECISION #281; rmp task #370). The loop
// also folded unconditionally, rewriting the whole snapshot at every tick of an
// idle server (rmp task #383). Groadmap now drives the fold itself, so it holds
// the fold's error as it holds the shutdown checkpoint's, and it asks for a fold
// only when the gate finds the log grown (SPEC/GRAPH.md § Durability and
// Checkpointing in a Long-Lived Process, rules 5, 9 and 10).
//
// SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process, rule 7,
// fixes the SHAPE of the report: a checkpoint failure after commits that are
// already durable is a diagnostic and not a failed statement. Nothing here
// changes an exit code, fails a write, or stops the server.
package graphserve

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
)

// intervalFloor is the shortest tick the in-flight fold will use.
//
// It matters only for a cadence that carries a maxAge and no interval, which
// production never builds: the interval is then derived as a quarter of maxAge,
// exactly as the engine's own constructor derived it, and one millisecond is the
// same floor that constructor applied, so a quarter of a very short maxAge can
// never reach zero — which time.NewTicker does not accept.
const intervalFloor = time.Millisecond

// tickInterval is the period the in-flight fold ticks at for a given cadence: the
// cadence's own interval, or a quarter of its maxAge when it states none.
func tickInterval(cadence checkpointCadence) time.Duration {
	interval := cadence.interval
	if interval <= 0 {
		interval = cadence.maxAge / 4
	}
	if interval < intervalFloor {
		return intervalFloor
	}
	return interval
}

// checkpointHealth is the in-flight fold's reporting state between attempts, and
// it is separated from the goroutine so that the DECISION can be read, and
// driven, on its own: a transition table is a thing to test directly, and a
// ticker is not.
//
// It is owned by the fold goroutine alone and carries no synchronisation of its
// own. [inFlightCheckpoint.stop] joins that goroutine before returning, so a
// caller that has stopped it may read this without a race.
type checkpointHealth struct {
	// lastErr is the error text this state was last reported for. It is the
	// identity of the failure, not merely a flag: a DIFFERENT failure while
	// already failing is a new fact and is reported again.
	lastErr string
	// failing is whether the last attempt failed. It is not derivable from
	// lastErr alone — a repair sets lastErr back to the empty string, which is
	// also its initial value, and the two must not be confused.
	failing bool
}

// observe folds the outcome of one ATTEMPTED fold into the health state and
// reports the record to emit, if any. A due instant at which the gate withheld the
// fold is not an attempt and is never passed here: it neither succeeded nor
// failed.
//
// The whole of the policy is this table, and it is written out because the reason
// for each row differs:
//
//	previous | outcome                        | emitted
//	---------+--------------------------------+-------------------------------------
//	healthy  | success                        | nothing
//	any      | refused for an over-long field | slog.LevelError, EVERY attempt
//	healthy  | any other failure              | slog.LevelError, once
//	failing  | the SAME failure text          | nothing — a condition that has not
//	         |                                | changed is not news
//	failing  | a DIFFERENT failure text       | slog.LevelError again — a different
//	         |                                | failure is a new fact
//	failing  | success                        | slog.LevelInfo — a later fold
//	         |                                | repaired it
//
// Three properties of the table are load-bearing rather than incidental:
//
//   - The healthy path emits NOTHING, ever. stderr for a successful
//     `rmp graph serve` is the two warnings the engine emits at construction and
//     nothing else, and it stays byte-identical: the end-to-end suite asserts on
//     that stream and this feature must not be visible in it.
//   - A field the snapshot format refuses is reported at EVERY attempt, with the
//     wording graphstore shares with the shutdown checkpoint. It is the one
//     failure that cannot heal: the log still holds the unfolded bytes, so every
//     due instant attempts the fold again and every attempt refuses, and a record
//     that recurred saying only that a checkpoint had failed would train an
//     operator to ignore the one message that names an unbounded, permanent cost
//     (SPEC/GRAPH.md § Field Length Limits, rules 8 and 9). It is recognised with
//     errors.Is on the error this package now holds, never by matching text.
//   - A repair is reported at INFO and not at ERROR, because it is the fact that
//     ends the incident. A reader who saw the ERROR needs to know the fold is
//     happening again; a reader who did not see it needs nothing, and INFO is
//     where a fact that is not a problem belongs.
func (h *checkpointHealth) observe(err error) (level slog.Level, message string, emit bool) {
	switch {
	case err == nil:
		if !h.failing {
			return 0, "", false
		}
		h.failing = false
		h.lastErr = ""
		return slog.LevelInfo, checkpointRepairedMessage, true

	case graphstore.CheckpointRefusedFieldTooLong(err):
		h.failing = true
		h.lastErr = err.Error()
		return slog.LevelError, inFlightFieldTooLongMessage(), true

	case h.failing && err.Error() == h.lastErr:
		return 0, "", false

	default:
		h.failing = true
		h.lastErr = err.Error()
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

// inFlightFieldTooLongMessage is the in-flight fold's report of a field the
// snapshot format refused. The wording after the subject is graphstore's, the
// same the shutdown checkpoint uses, and the subject names THIS checkpoint so the
// two reports on the one stderr stream stay distinguishable.
func inFlightFieldTooLongMessage() string {
	return graphstore.FieldTooLongCheckpointDiagnostic("an in-flight graph checkpoint")
}

// inFlightCheckpoint is the server's in-flight fold: a goroutine that ticks on
// the cadence and, at every instant the cadence makes a fold due, asks the gate
// to fold.
//
// # What it decides and what it does not
//
// It decides only WHEN a fold is due, and it decides that exactly as the engine's
// own loop did: at each tick, a fold is due once maxAge has passed since the last
// fold, and the last fold starts at the zero time, so the first tick is due. The
// gate decides WHETHER the due fold runs (see [foldGate]): it reads the
// write-ahead log's durable offset once and folds only when the log has grown
// since it was last folded. That read is taken at due instants and at no other
// time, and it costs nothing that grows with the graph.
//
// A due fold the gate withholds is not a fold, so the last-fold time does not
// move and the cadence stays due: the gate is consulted again at the next tick.
// An attempted fold — one that ran, whether it succeeded or failed — moves it, so
// a failure is attempted again at the next due instant and not at every tick
// (SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process, rules 9
// and 10; § Synchronous Checkpoint on Write, failure policy, rule 2).
//
// # Why the fold arrives as a function
//
// fold is injected rather than taken as a [foldGate] so that the timing, the
// reporting and the lifetime are drivable without a store — and, for the failure
// rows, without provoking a real checkpointer into failing, which is a
// filesystem fault-injection problem. Production passes the gate's own method
// value, so nothing is substituted on the path that ships.
//
// # The goroutine leak boundary
//
// This is one, and it is the reason stop both closes and JOINS. The fold is owned
// by [shutdownCloser], which the engine's server closes after its drain; once
// Close has returned, this package must own no running goroutine at all, and the
// shutdown checkpoint must not begin while an in-flight fold is still running.
// stop is idempotent because Close is: the engine may reach it from either of its
// two drained exit paths, and Run closes it directly on the startup paths where
// nothing was ever served.
type inFlightCheckpoint struct {
	fold   func() (bool, error)
	log    *slog.Logger
	stopCh chan struct{}
	doneCh chan struct{}
	health checkpointHealth
	every  time.Duration
	maxAge time.Duration
	// due counts the instants at which the cadence found a fold due and the gate
	// was consulted, and attempts counts the folds that ran. They are the
	// observable a test waits on — "at least two due instants have passed" — so a
	// test waits for the event rather than for a duration it guessed.
	due      atomic.Int64
	attempts atomic.Int64
	// running records that the goroutine was actually launched, so stop does not
	// wait on a done channel nothing will ever close. It is atomic because start
	// runs on the goroutine that assembles the server and stop runs on whichever
	// of the engine's exit paths reaches the closer first.
	running   atomic.Bool
	startOnce sync.Once
	stopOnce  sync.Once
}

// newInFlightCheckpoint builds the in-flight fold over fold, ticking on the
// cadence and reporting through log. It starts nothing; see start.
func newInFlightCheckpoint(fold func() (bool, error), cadence checkpointCadence, log *slog.Logger) *inFlightCheckpoint {
	return &inFlightCheckpoint{
		fold:   fold,
		log:    log,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		every:  tickInterval(cadence),
		maxAge: cadence.maxAge,
	}
}

// start launches the fold goroutine, once however many times it is called.
func (c *inFlightCheckpoint) start() {
	c.startOnce.Do(func() {
		c.running.Store(true)
		go c.run()
	})
}

// stop ends the fold goroutine and waits for it to return, including for a fold
// it is in the middle of.
//
// It is idempotent, and it JOINS rather than merely signalling: an in-flight fold
// that outlived the call would run beside the shutdown checkpoint, which the
// specification forbids, and would read a checkpointer being torn down beneath
// it. Calling it on a fold that was never started is safe and returns at once.
func (c *inFlightCheckpoint) stop() {
	c.stopOnce.Do(func() { close(c.stopCh) })
	if c.running.Load() {
		<-c.doneCh
	}
}

// run is the cadence loop.
//
// The ticker is the only clock. A tick before maxAge has passed since the last
// fold does nothing at all; a due tick consults the gate once.
func (c *inFlightCheckpoint) run() {
	defer close(c.doneCh)

	ticker := time.NewTicker(c.every)
	defer ticker.Stop()

	var lastFold time.Time
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			if time.Since(lastFold) < c.maxAge {
				continue
			}
			if c.attempt() {
				lastFold = time.Now()
			}
		}
	}
}

// attempt consults the gate at one due instant, reports the outcome of a fold
// that ran, and reports whether one did.
//
// The error text is taken from the ERROR and not from the health state, because
// the state's copy is cleared on the repair transition: the two are the same
// string on every row that reports an error, and reading the error keeps the
// attribute and the decision derived from one value.
func (c *inFlightCheckpoint) attempt() bool {
	c.due.Add(1)
	ran, err := c.fold()
	if !ran && err == nil {
		return false
	}
	c.attempts.Add(1)

	level, message, emit := c.health.observe(err)
	if !emit {
		return true
	}
	if level == slog.LevelError {
		c.log.Error(message, slog.String("err", err.Error()))
		return true
	}
	c.log.Info(message)
	return true
}
