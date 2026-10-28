// Package graphserve — recognising a fail-stopped engine.
//
// The engine fail-stops when a commit-time update of its secondary indexes is cut
// short by a panic: their state is then undefined, and the engine refuses every
// later statement, transaction and commit with cypher.ErrEngineFailStopped. A
// server that went on serving would answer every statement with that refusal
// while appearing healthy, so SPEC/GRAPH.md § Server Diagnostics on Stderr
// requires the server to recognise the condition from a statement's failure,
// stop checkpointing at once, shut down through its ordinary sequence without the
// shutdown checkpoint, and end with one line of its own and exit code 1.
//
// # Where the statement's failure is observed, and why there
//
// The statements run inside the engine's Bolt session layer, which reports every
// failure in exactly two places: on the wire, and in a record on the logger this
// package hands the server. The wire cannot carry the condition. The engine maps
// no protocol code to the fail-stop, so it travels as the generic
// Neo.DatabaseError.General.UnknownError with its message masked, exactly as every
// other server fault does. The record carries the error's own text under the
// "err" attribute, so the logger is the one place inside this process where the
// refusal is still distinguishable, and [failStopHandler] watches it there.
//
// A match on the text alone would let a statement whose error merely QUOTED that
// text stop the server, so a match is only a candidate: [failStopWatch.observe]
// confirms it against the engine's own state — the index manager reports its
// state undefined, which is the one condition the engine fail-stops on — before
// it acts. The text is the engine's sentinel, read from the sentinel itself
// rather than restated here.
package graphserve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// failStopLine is the whole of the server's own line for the condition, after
// the sentinel; SPEC/GRAPH.md § Error Handling and Exit Codes, rule 12, and
// SPEC/COMMANDS.md § Serve Error Cases publish it.
const failStopLine = "the graph engine fail-stopped and refuses every statement; the server has shut down. " +
	"Restart rmp graph serve to rebuild the indexes from the store."

// errFailStopped is the error a server ends with once it has recognised a
// fail-stopped engine. It carries utils.ErrGraphServer, not ErrGraphEngine: the
// statement that met the refusal is not at fault, and the action is a restart.
var errFailStopped = fmt.Errorf("%w: %s", utils.ErrGraphServer, failStopLine)

// failStopAttrKey is the attribute under which the engine's Bolt session puts a
// failed statement's error in the record it logs.
const failStopAttrKey = "err"

// failStopWatch turns the first confirmed fail-stop refusal into two effects, once:
// the fold gate refuses every later fold, and the channel serve waits on closes.
//
// Both happen on the goroutine that observed the refusal and before the record
// that reported it is written, so no fold can start after the refusal was seen,
// whatever the serve loop is doing at that moment.
type failStopWatch struct {
	// undefined is the engine's own account of whether its index state is
	// undefined; nil means defined. It is the confirmation a textual match
	// needs before it may stop the server.
	undefined func() error
	gate      *foldGate
	done      chan struct{}
	once      sync.Once
}

// newFailStopWatch returns a watch over the engine whose index state undefined
// reports, acting on gate.
func newFailStopWatch(undefined func() error, gate *foldGate) *failStopWatch {
	return &failStopWatch{undefined: undefined, gate: gate, done: make(chan struct{})}
}

// stopped is closed once the server has recognised a fail-stopped engine.
func (w *failStopWatch) stopped() <-chan struct{} { return w.done }

// recognised reports whether the server has recognised a fail-stopped engine.
func (w *failStopWatch) recognised() bool {
	select {
	case <-w.done:
		return true
	default:
		return false
	}
}

// observe examines one failed statement's error: a refusal that names the
// engine's fail-stop and is confirmed by the engine's state marks the engine
// fail-stopped. Every later call is a no-op once that has happened.
func (w *failStopWatch) observe(failure slog.Value) {
	if w.recognised() || !namesFailStop(failure) {
		return
	}
	if w.undefined() == nil {
		return
	}
	w.once.Do(func() {
		w.gate.refuseFolds()
		close(w.done)
	})
}

// namesFailStop reports whether a record's error attribute names the engine's
// fail-stop: by identity when the attribute holds the error itself, and by the
// sentinel's own text when it holds the error's rendering, which is what the
// engine's session logs.
func namesFailStop(failure slog.Value) bool {
	switch failure.Kind() {
	case slog.KindString:
		return strings.Contains(failure.String(), cypher.ErrEngineFailStopped.Error())
	case slog.KindAny:
		if err, ok := failure.Any().(error); ok {
			return errors.Is(err, cypher.ErrEngineFailStopped)
		}
	}
	return false
}

// failStopHandler is the handler of the logger the engine's Bolt server is given:
// it shows every failure record to the watch, then hands the record on unchanged.
//
// It reports itself enabled for every record at level Error, whatever the
// handler it wraps would accept, because the engine logs a failed statement at
// that level and recognising the condition must not depend on where, or whether,
// the diagnostics are written. A record the wrapped handler would not have
// accepted is examined and dropped.
type failStopHandler struct {
	inner slog.Handler
	watch *failStopWatch
}

// Enabled implements slog.Handler.
func (h *failStopHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelError || h.inner.Enabled(ctx, level)
}

// Handle implements slog.Handler, whose signature passes the record by value.
func (h *failStopHandler) Handle(ctx context.Context, r slog.Record) error { //nolint:gocritic // hugeParam: the slog.Handler interface fixes the by-value record
	if r.Level >= slog.LevelError {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == failStopAttrKey {
				h.watch.observe(a.Value.Resolve())
			}
			return true
		})
	}
	if !h.inner.Enabled(ctx, r.Level) {
		return nil
	}
	return h.inner.Handle(ctx, r)
}

// WithAttrs implements slog.Handler.
func (h *failStopHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &failStopHandler{inner: h.inner.WithAttrs(attrs), watch: h.watch}
}

// WithGroup implements slog.Handler.
func (h *failStopHandler) WithGroup(name string) slog.Handler {
	return &failStopHandler{inner: h.inner.WithGroup(name), watch: h.watch}
}

// watchedLogger returns log with every failure record it receives shown to watch.
func watchedLogger(log *slog.Logger, watch *failStopWatch) *slog.Logger {
	return slog.New(&failStopHandler{inner: log.Handler(), watch: watch})
}
