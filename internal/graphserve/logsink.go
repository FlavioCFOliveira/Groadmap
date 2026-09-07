package graphserve

import (
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
)

// logQueueDepth is how many rendered records the sink holds before it starts
// dropping, and it is a memory bound rather than a tuning knob.
//
// MEASURED, against the real server under the load that produces the flood (16
// writers on one node, 6,400 statements through internal/graphclient): at this
// depth a stderr that is being read loses NOTHING — zero drops, 631.8 statements
// per second against a 632.7 baseline with the sink written to directly — while a
// stderr that has stopped being read costs the server nothing at all, where
// today it costs it 55% of its statements. The depth is what buys the first of
// those: a reader that stalls for a moment is absorbed instead of losing records.
//
// The cost of raising it is memory and staleness, and both are real: a queued
// record is retained until it is written, so the worst case is this many records
// of heap, and a record delivered late is a record whose neighbours have already
// been overtaken by events. One conflict record is 213 bytes, so this bound is
// about 55 KiB — the same order as the operating system's own pipe buffer, which
// is the thing it stands in for.
const logQueueDepth = 256

// dropSink is the graph server's diagnostic sink: an io.Writer that NEVER blocks
// its caller, whatever the thing behind it is doing.
//
// # The defect it exists against
//
// rmp task #389. The engine logs one ERROR record per serialisation conflict
// (bolt/server/session.go, the PULL and DISCARD paths), and it logs it from the
// goroutine that is serving the session. Under concurrent writers to one node
// those records arrive fast enough to fill a 64 KiB operating-system pipe buffer
// — MEASURED at 304 records of 213 bytes — and the write that meets a full pipe
// BLOCKS. With it blocks the session, so statements stop being answered and the
// callers see the client's backstop rather than a result. Worse, the engine's
// Serve waits for those goroutines, so the server could not be stopped either:
// SIGTERM never returned it (FINDING #410 on the task).
//
// The hazard is not the conflict record and not the pipe. It is that a
// DIAGNOSTIC sink sat on the serving path: anything that stops draining stderr —
// a log shipper that dies, a supervisor that stops reading, a `| head` — stops
// the server. This type removes the sink from that path.
//
// # What it costs, stated where it is taken
//
// Records are LOST when the queue is full, and they are the newest-but-one
// rather than the newest: the queue keeps the most recent logQueueDepth records
// and discards the oldest to make room. Drop-oldest is the deliberate choice.
// The alternative, refusing the newest, preserves the beginning of an incident
// and throws away its outcome — and the outcome is what an operator reading a
// log after the fact needs, because the beginning is usually the same flood
// repeated.
//
// The loss is REPORTED rather than silent, which is the difference between a
// bounded sink and a broken one. See [dropSink.report].
//
// # Why not simply log less
//
// Because that fixes this flood and not the class. The engine's ERROR set also
// contains both of its recovered-panic sites, `commit failed` and
// `authentication failed`, so no level filter separates a normal MVCC outcome
// from a real fault; separating them means matching the engine's message text
// and its error text, which couples Groadmap to two strings no gate defends and
// which would go on passing, silently, on the day the engine rewords them.
// Bounding the sink needs no engine string and holds for every record the engine
// may ever emit.
type dropSink struct {
	// The fields are ordered so that everything holding a pointer comes first.
	// That is govet's fieldalignment rule and it is not cosmetic: the garbage
	// collector scans a struct only as far as its last pointer word, and this
	// value lives for the whole life of the process. What guards what is stated
	// per field below rather than implied by adjacency.

	// w is the real destination, written ONLY by the drain goroutine. Nothing
	// else may write to it: the goroutine's freedom to block is what buys every
	// other goroutine's freedom not to, and a second writer would both
	// interleave records and reintroduce the blocking this type removes.
	w io.Writer

	// notice renders the dropped-record report through a handler of its own,
	// over the SAME destination. It exists so the report is a real slog record
	// in the same shape as every other line, rather than a second opinion about
	// the record format assembled with string concatenation here.
	notice *slog.Logger

	// wake carries "there is work" to the drain goroutine. It has capacity 1
	// and is offered non-blockingly, so signalling costs a Write nothing when
	// the goroutine is already awake.
	wake chan struct{}

	// ring is the fixed-capacity buffer of rendered records, oldest at head. It
	// is allocated once, at construction, and never grows: a diagnostic queue
	// that grew would trade a wedge for a slow memory exhaustion, which is the
	// worse of the two because it arrives without a symptom.
	//
	// slog.TextHandler renders a whole record and calls Write ONCE with it, so
	// an element is exactly one line and a drop can never emit half of one.
	//
	// Guarded by mu.
	ring [][]byte

	// head is the index of the oldest queued record and size is how many are
	// queued. Guarded by mu.
	head int
	size int

	// dropped counts records discarded since the last report reached the
	// destination. It is reset only when that report is written, so a count is
	// never lost by being overwritten by a later one. Guarded by mu.
	dropped uint64

	// start is the drain goroutine's ignition. It is deferred to the first
	// Write so that the overwhelming majority of `rmp` invocations — every
	// short-lived command, none of which serves anything — start no goroutine
	// merely by linking this package.
	start sync.Once

	// mu guards ring, head, size, dropped and writing. It is held only for the
	// O(1) ring operations and NEVER across a write to the destination, which is
	// the whole point: a caller's Write cannot be delayed by the sink's.
	mu sync.Mutex

	// writing is true while the drain goroutine is inside a write to w, and it
	// is what makes [dropSink.Flush] mean what it says. An empty queue is NOT
	// the same as a delivered queue: the last record leaves the ring before it
	// is written, so a flush that watched only the ring would return while that
	// record was still in the goroutine's hand — and at exit, still in it when
	// the process ended. Guarded by mu.
	writing bool
}

// newDropSink returns a sink that writes to w without ever blocking its caller.
//
// The drain goroutine is not started here; see [dropSink.start].
func newDropSink(w io.Writer) *dropSink {
	s := &dropSink{
		ring: make([][]byte, logQueueDepth),
		wake: make(chan struct{}, 1),
		w:    w,
	}
	// The report goes STRAIGHT to w, not back through this sink: it is written
	// by the drain goroutine, which is already the only writer, and a report
	// that could itself be queued could itself be dropped — which is exactly
	// the silence this whole mechanism exists to avoid.
	s.notice = newLogger(w)
	return s
}

// Write queues one rendered record and returns immediately. It never blocks, it
// never fails, and it always reports the whole of p as written: a caller of a
// diagnostic sink has nothing useful to do with a short write or an error, and
// slog would only discard either.
//
// The copy is required. slog hands over its internal buffer and reuses it as
// soon as Write returns, so a queue that retained p would publish whatever the
// next record overwrote it with.
func (s *dropSink) Write(p []byte) (int, error) {
	record := make([]byte, len(p))
	copy(record, p)

	s.mu.Lock()
	if s.size == logQueueDepth {
		// Full: discard the OLDEST and take its slot, keeping the newest
		// logQueueDepth records. See the type comment for why oldest.
		s.ring[s.head] = record
		s.head = (s.head + 1) % logQueueDepth
		s.dropped++
	} else {
		s.ring[(s.head+s.size)%logQueueDepth] = record
		s.size++
	}
	s.mu.Unlock()

	s.start.Do(func() { go s.drain() })
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return len(p), nil
}

// drain is the one goroutine allowed to block on the destination. It runs for
// the life of the process, which is what it is for: there is no state to release
// and nothing to shut down, and a sink that stopped draining would be the defect
// it exists against.
func (s *dropSink) drain() {
	for {
		record, dropped, ok := s.take()
		if !ok {
			<-s.wake
			continue
		}
		// The report precedes the record it accounts for, so the gap is
		// declared exactly where it happened rather than at the end of the run.
		if dropped > 0 {
			s.report(dropped)
		}
		// A destination that cannot be written to is the condition this type
		// SURVIVES rather than reports: a diagnostic sink has no diagnostic
		// channel of its own, and the caller has already been told the write
		// succeeded because for a diagnostic that is the only useful answer.
		_, _ = s.w.Write(record)
		s.finished()
	}
}

// finished marks the end of a delivery, releasing a Flush that is waiting for it.
func (s *dropSink) finished() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writing = false
}

// take removes the oldest queued record, together with the number of records
// dropped since the last report. Both leave the sink under one acquisition of
// the lock, so a count can never be attributed to the wrong record or counted
// twice.
func (s *dropSink) take() (record []byte, dropped uint64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.size == 0 {
		return nil, 0, false
	}
	record = s.ring[s.head]
	s.ring[s.head] = nil // release the record to the collector once it is written
	s.head = (s.head + 1) % logQueueDepth
	s.size--

	dropped, s.dropped = s.dropped, 0
	s.writing = true
	return record, dropped, true
}

// report states, on the log itself, how many records were lost and why — so the
// gap an operator finds is a declared gap rather than an unexplained one.
//
// # Why the log and not somewhere else
//
// Because it is the only place with a reader. Stdout is spoken for: it carries
// the single startup object naming the socket, and SPEC/COMMANDS.md § Serve
// Output requires that a caller reading it for the path is never disturbed by a
// record. There is no metrics endpoint, and the exit code has no room for a
// count. A counter kept only in memory would be the defect rmp task #380 found
// in WithFinalCheckpoint: a number that is correct and that nobody ever reads.
//
// # Why its absence is not the same silence
//
// This record can only be written once the destination accepts writes again. It
// therefore reaches an operator in the case that matters — a sink that stalled
// and recovered, which is what a busy log shipper or a slow terminal does — and
// not in the case where the destination never recovers. In THAT case no channel
// would have delivered it: the operator has received nothing at all, and the
// missing report is not a further loss on top of the missing records. What is
// ruled out is the case the mechanism could get wrong, which is dropping records
// while the log continues to look complete.
func (s *dropSink) report(dropped uint64) {
	s.notice.Warn("graph server diagnostics were dropped: this process's stderr stopped being read, "+
		"so the records below are not continuous. The server kept serving throughout; "+
		"drain this stream to stop losing records",
		slog.Uint64("dropped", dropped))
}

// Flush waits, bounded, for the queued records to reach the destination.
//
// It exists for the end of the process and for nothing else. Without it a
// diagnostic written moments before the last return — a store that failed to
// close, a teardown that reported a failure — could still be in the queue when
// the process exits, and would be lost where today it is written synchronously.
// Losing a failure diagnostic to the mechanism that protects the server from a
// blocked stderr would be a poor trade.
//
// It is BOUNDED because the sink it is flushing may be the dead one, and an
// unbounded flush at exit is the hang of FINDING #410 moved to a new line. The
// bound is the retry policy's own total, through the package that owns waiting:
// this is a bounded wait on a condition, which is that package's shape, and the
// figure is the project's existing answer to "how long is it worth continuing to
// try something that may never succeed" rather than a new number chosen here.
//
// On a healthy sink the condition holds on the first check and nothing is slept.
func (s *dropSink) Flush() {
	// An exhausted bound leaves records unwritten, and there is deliberately
	// nothing to return: the caller is on its way out and has no channel left
	// to report a lost diagnostic on.
	_, _ = backoff.RetryWithin(backoff.Total(), func() (struct{}, error) {
		if s.empty() {
			return struct{}{}, nil
		}
		return struct{}{}, errRecordsQueued
	}, backoff.Always)
}

// empty reports whether every record has reached the destination: nothing left
// in the ring AND nothing still in the drain goroutine's hand.
func (s *dropSink) empty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size == 0 && !s.writing
}

// errRecordsQueued is Flush's "not yet" — the value that keeps its bounded wait
// climbing. It never reaches a caller.
//
// It is its own value rather than a reuse of the drain's errStillInFlight: the
// two say different things (records not yet written, against sessions not yet
// quiescent) and sharing one would make a future reader believe the shutdown
// drain and this flush wait for the same condition.
var errRecordsQueued = errors.New("graph server: diagnostic records still queued")
