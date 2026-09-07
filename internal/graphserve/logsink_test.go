// Package graphserve — the regression suite for rmp task #389: a server whose
// stderr has stopped being read must keep serving.
//
// # What was wrong
//
// The engine logs one ERROR record per serialisation conflict, from the goroutine
// serving the session. With stderr on a pipe nobody reads, ~304 of those records
// (213 bytes each) fill the operating system's 64 KiB pipe buffer, and the next
// write BLOCKS — taking the session with it. Statements stopped being answered,
// and because the engine's Serve waits for those goroutines, SIGTERM stopped
// working too.
//
// # What the tests here establish, and in what order
//
// [TestDropSink_AServerWhoseStderrIsNotReadKeepsServing] is the acceptance
// criterion, driven against a REAL server: every statement is answered while the
// destination is blocked. The unit tests below it pin the individual properties
// that make that possible — a Write that never blocks, drop-OLDEST rather than
// drop-newest, record granularity, and the report that makes the loss visible —
// because the acceptance test alone would pass on an implementation that dropped
// everything and said nothing.
package graphserve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
)

// countingWriter reports how many bytes the destination ACCEPTED, so a test can
// tell a pipe that filled from one that never did. A blocked write is visible as
// a total that stops advancing.
type countingWriter struct {
	w       io.Writer
	written atomic.Int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.written.Add(int64(n))
	return n, err
}

// blockingWriter accepts nothing until it is released. It is the deterministic
// stand-in for a full pipe in the unit tests: an os.Pipe blocks only after 64 KiB
// have gone into it, which is a lot of records to manufacture when the property
// under test is what happens once the destination stops accepting.
type blockingWriter struct {
	release chan struct{}
	mu      sync.Mutex
	written [][]byte
}

func newBlockingWriter() *blockingWriter {
	return &blockingWriter{release: make(chan struct{})}
}

func (b *blockingWriter) Write(p []byte) (int, error) {
	<-b.release
	b.mu.Lock()
	defer b.mu.Unlock()
	record := make([]byte, len(p))
	copy(record, p)
	b.written = append(b.written, record)
	return len(p), nil
}

func (b *blockingWriter) unblock() { close(b.release) }

func (b *blockingWriter) records() [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([][]byte, len(b.written))
	copy(out, b.written)
	return out
}

// TestDropSink_AServerWhoseStderrIsNotReadKeepsServing is rmp task #389's
// acceptance criterion, driven end to end against a real server assembled from
// the production bind and build.
//
// # Why the load is what it is
//
// The hazard needs BOTH an undrained destination and enough records to fill it.
// Sixteen writers on ONE node is what produces the conflict flood — MEASURED at
// 1.19 ERROR records per statement at that width — and 60 statements each gives
// roughly 1,100 records against the ~304 that fill a 64 KiB pipe, so the buffer
// is passed several times over rather than approached.
//
// # Why the assertions are what they are
//
// "Every statement succeeded" would be WRONG and flaky: a serialisation conflict
// that exhausts the retry ladder is a normal outcome under this exact load
// (measured at 0.016%, so roughly one run in six would see one) and it is not
// what this test is about. What the wedge produces is different in kind —
// graphclient.FailureUnanswered, the caller's backstop firing on a server that is
// alive and simply not answering — so that is the failure asserted against.
//
// The byte floor is what makes the test non-vacuous. Without it an
// implementation that emitted ten records would pass, having never approached
// the buffer at all.
//
// # Shown to fail without the change
//
// Mutating [stderrLogger] to return newLogger(w) — the logger writing straight
// to the destination, which is what it did before this task — makes the server
// stall and this test fail on the FailureUnanswered assertion. See the task's
// TEST comment for the numbers.
func TestDropSink_AServerWhoseStderrIsNotReadKeepsServing(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the pipe that stands in for an unread stderr: %v", err)
	}
	defer reader.Close() //nolint:errcheck // the test's assertions are about the server, not the pipe teardown

	accepted := &countingWriter{w: writer}

	// The server logs through the SAME constructor production uses, over a
	// destination nothing reads. Anything less would test a re-assembly: it is
	// stderrLogger that decides whether a diagnostic write sits on the serving
	// path, so it is stderrLogger the server has to be given.
	testLogger, sink := stderrLogger(accepted)

	socket, _, stop := startRealServerLogging(t, productionCadence(), testLogger)
	defer stop()

	if _, err := graphclient.Send(context.Background(), socket,
		"CREATE (n:Hot {key:'contended', counter:0})"); err != nil {
		t.Fatalf("seeding the contended node: %v", err)
	}

	const writers = 16
	const perWriter = 60

	// The budget is the wedge detector. Unwedged, this load takes about 1.5 s;
	// wedged, the writers park behind statements that never return and the
	// deadline is what ends the run.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var answered, conflicts, unanswered atomic.Int64
	var firstUnanswered atomic.Pointer[string]

	var wg sync.WaitGroup
	for writer := range writers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for statement := range perWriter {
				_, err := graphclient.Send(ctx, socket,
					fmt.Sprintf("MATCH (n:Hot {key:'contended'}) SET n.counter = %d",
						id*perWriter+statement))
				if err == nil {
					answered.Add(1)
					continue
				}
				var sendErr *graphclient.SendError
				if errors.As(err, &sendErr) && sendErr.Kind == graphclient.FailureConflict {
					// A conflict that exhausted the ladder IS an answer: the
					// server told the client what happened. It is a normal
					// outcome of MVCC under this load
					// (SPEC/GRAPH.md § Concurrency Inside the Server, rule 4).
					conflicts.Add(1)
					answered.Add(1)
					continue
				}
				unanswered.Add(1)
				text := err.Error()
				firstUnanswered.CompareAndSwap(nil, &text)
			}
		}(writer)
	}
	wg.Wait()

	// 1. The destination really did fill. Without this the rest is vacuous.
	const bufferFloor = 60 * 1024
	if got := accepted.written.Load(); got < bufferFloor {
		t.Fatalf("the unread destination accepted only %d bytes, want at least %d: the load did not "+
			"fill the buffer, so this run never reached the condition the test exists for. "+
			"Raise the writer count or the statements per writer", got, bufferFloor)
	}

	// 2. Every statement was answered. This is the criterion.
	if n := unanswered.Load(); n != 0 {
		first := "<none captured>"
		if p := firstUnanswered.Load(); p != nil {
			first = *p
		}
		t.Errorf("%d of %d statements went unanswered while stderr was not being read (%d were answered, "+
			"%d of them as exhausted conflicts).\nfirst failure: %s\n"+
			"A blocked diagnostic write is stalling the goroutine that serves the session, which is "+
			"rmp task #389: the logger must not put a write to stderr on the serving path",
			n, writers*perWriter, answered.Load(), conflicts.Load(), first)
	}

	// 3. The loss was DECLARED. A sink that drops silently trades a noisy
	//    failure for a quiet one, which is the whole objection to this design.
	//    Draining the destination now lets the queued records and the report
	//    through; the flush is what production runs before it exits.
	var tail bytes.Buffer
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		_, _ = io.Copy(&tail, reader)
	}()
	sink.Flush()
	_ = writer.Close() //nolint:errcheck // closing the write end is what ends the copy above
	<-drained

	if !strings.Contains(tail.String(), "graph server diagnostics were dropped") {
		t.Errorf("records were dropped and the log never said so.\n"+
			"The count must reach the operator on the stream itself: a gap that is not declared is "+
			"indistinguishable from a quiet period.\nlast bytes of the stream:\n%s",
			lastBytes(tail.String(), 600))
	}
	if !strings.Contains(tail.String(), "dropped=") {
		t.Errorf("the dropped-records report carries no count.\nlast bytes of the stream:\n%s",
			lastBytes(tail.String(), 600))
	}
}

// lastBytes renders the tail of a captured stream for a failure message.
func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// ---------------------------------------------------------------------------
// The properties that make the acceptance criterion possible
// ---------------------------------------------------------------------------

// The tests below write through fmt.Fprintf, which renders the whole string and
// calls Write exactly ONCE — the same one-call-per-record contract
// slog.TextHandler honours. That is not incidental: the sink's unit of loss is
// whatever arrives in a single Write, so a helper that split a record across two
// calls would be testing a shape the production logger never produces.

// TestDropSink_WriteNeverBlocksOnABlockedDestination is the mechanism in
// isolation: the caller returns while the destination is accepting nothing.
//
// The acceptance test above establishes the same thing through a real server,
// where a failure is measured in unanswered statements. This one establishes it
// where a failure is unambiguous — the Write either returned or it did not — so
// a regression is diagnosed rather than merely detected.
func TestDropSink_WriteNeverBlocksOnABlockedDestination(t *testing.T) {
	destination := newBlockingWriter()
	sink := newDropSink(destination)

	// One more record than the queue holds, so the last writes are the ones
	// that meet a full queue in front of a destination that accepts nothing.
	// A sink that blocked anywhere on this path would never reach the send.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range logQueueDepth + 50 {
			_, _ = fmt.Fprintf(sink, "record %d\n", i)
		}
	}()

	select {
	case <-done:
	case <-timeoutAfter(t, 30*time.Second):
		destination.unblock()
		t.Fatalf("writing %d records to a sink whose destination accepts nothing did not return: "+
			"the sink is blocking its caller, which is exactly what puts a diagnostic write back "+
			"on the goroutine serving a session (rmp task #389)", logQueueDepth+50)
	}
	destination.unblock()
}

// TestDropSink_DropsTheOldestAndKeepsTheNewest pins the direction of the loss.
//
// The direction is a real choice and the opposite one is defensible in the
// abstract, so it is pinned rather than left to the implementation: keeping the
// newest means an operator reading a log after an incident sees how it ENDED.
// Refusing the newest instead would preserve the opening of a flood — which is
// usually the same record repeated — and discard its outcome.
//
// # Why exactly one record may legitimately escape the window
//
// The destination blocks on its FIRST write and never returns, so the drain
// goroutine can remove at most ONE record from the ring before it is stuck. That
// record is in flight rather than dropped, and it is why this test asserts about
// a record in the MIDDLE of the overflow window rather than about the very first
// one: "record-0000 survived" is not evidence of drop-newest, it is evidence of
// a record that had already left.
func TestDropSink_DropsTheOldestAndKeepsTheNewest(t *testing.T) {
	destination := newBlockingWriter()
	sink := newDropSink(destination)

	const overflow = 40
	const written = logQueueDepth + overflow
	for i := range written {
		if _, err := fmt.Fprintf(sink, "record-%04d\n", i); err != nil {
			t.Fatalf("Write returned an error, which a diagnostic sink never does: %v", err)
		}
	}

	// Let the queued records through and wait for them to arrive.
	destination.unblock()
	sink.Flush()
	waitFor(t, func() bool { return len(destination.records()) >= logQueueDepth })

	survived := map[string]bool{}
	var kept []string
	for _, record := range destination.records() {
		line := strings.TrimSpace(string(record))
		if strings.HasPrefix(line, "record-") {
			survived[line] = true
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		t.Fatalf("the destination received no records at all")
	}

	// 1. The NEWEST record survived. Drop-newest loses exactly this one.
	last := fmt.Sprintf("record-%04d", written-1)
	if !survived[last] {
		t.Errorf("the newest record %q did not survive an overflow of %d: the sink is refusing new "+
			"records instead of discarding old ones, so an operator would read the beginning of a "+
			"flood and never its outcome", last, overflow)
	}

	// 2. A record from the middle of the overflow window did NOT survive.
	//    At most one record can be in flight (see the comment above), so this
	//    index is inside the dropped range whichever way that one falls. Without
	//    this half the test would pass on a sink that dropped nothing at all.
	middle := fmt.Sprintf("record-%04d", overflow/2)
	if survived[middle] {
		t.Errorf("%q survived an overflow of %d records past a queue of %d: nothing was dropped, so "+
			"this run never reached the condition the test exists for", middle, overflow, logQueueDepth)
	}

	// 3. What survived is the queue's worth plus at most the one in flight.
	if len(kept) > logQueueDepth+1 {
		t.Errorf("%d records survived a queue of depth %d: the queue is not bounded, and an "+
			"unbounded diagnostic queue is a slow memory exhaustion in place of a wedge",
			len(kept), logQueueDepth)
	}

	// 4. Delivery is in order. A log that is not an ordered account of what
	//    happened is not much of a log.
	for i := 1; i < len(kept); i++ {
		if kept[i] <= kept[i-1] {
			t.Fatalf("record %q was delivered after %q: the queue is not delivering in the order it "+
				"was written", kept[i], kept[i-1])
		}
	}
}

// TestDropSink_ReportsWhatItDropped is the owner's condition on this design: a
// count that reaches somebody. A sink that loses records silently trades a noisy
// failure mode for a quiet one, which is the whole objection to dropping at all.
//
// # What is asserted, and why not the literal number
//
// The count is checked against the INVARIANT rather than against a constant:
// every record written either arrived or was counted as dropped, so survivors
// plus reported drops must equal what was written. The literal number cannot be
// asserted, because at most one record may have left the ring before it filled
// (see [TestDropSink_DropsTheOldestAndKeepsTheNewest]) and the drop count is 39
// or 40 depending on whether the drain goroutine was scheduled — a distinction
// with no meaning. The invariant is both scheduling-independent and stronger:
// it fails on a count that is short, on a count that is inflated, and on a
// record that vanished without being counted at all.
func TestDropSink_ReportsWhatItDropped(t *testing.T) {
	destination := newBlockingWriter()
	sink := newDropSink(destination)

	const overflow = 40
	const written = logQueueDepth + overflow
	for i := range written {
		_, _ = fmt.Fprintf(sink, "record-%04d\n", i)
	}
	destination.unblock()
	sink.Flush()
	waitFor(t, func() bool { return len(destination.records()) >= logQueueDepth })

	var reports []string
	survivors := 0
	for _, record := range destination.records() {
		line := strings.TrimSpace(string(record))
		switch {
		case strings.Contains(line, "graph server diagnostics were dropped"):
			reports = append(reports, line)
		case strings.HasPrefix(line, "record-"):
			survivors++
		}
	}

	if len(reports) == 0 {
		t.Fatalf("records were dropped and nothing on the stream said so. The count must reach the " +
			"operator where the gap is, or the sink has traded a loud failure for a silent one")
	}
	if len(reports) != 1 {
		t.Errorf("got %d dropped-records reports, want exactly 1: nothing was written after the "+
			"destination reopened, so a second report means a count was carried forward and "+
			"announced twice.\nreports: %v", len(reports), reports)
	}

	match := reportedDrops.FindStringSubmatch(reports[0])
	if match == nil {
		t.Fatalf("the report carries no dropped= count, so the operator is told records were lost "+
			"but not how many.\nreport: %s", reports[0])
	}
	dropped, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("the dropped= count %q is not a number\nreport: %s", match[1], reports[0])
	}

	if survivors+dropped != written {
		t.Errorf("%d records survived and %d were reported dropped, which accounts for %d of the %d "+
			"written. Every record must be either delivered or counted: a shortfall means records "+
			"vanished unannounced, and a surplus means the count is wrong",
			survivors, dropped, survivors+dropped, written)
	}

	// The report is a record like any other: same handler, same shape, same
	// canonical timestamp. A hand-assembled line would drift from the rest and
	// would meet a reader, or a parser, as something it does not recognise.
	if !strings.HasPrefix(reports[0], "time=") || !strings.Contains(reports[0], "level=WARN") {
		t.Errorf("the report is not in the shape of the other records on this stream.\nreport: %s",
			reports[0])
	}
}

// reportedDrops extracts the count from the sink's dropped-records report.
var reportedDrops = regexp.MustCompile(`dropped=(\d+)`)

// TestDropSink_LosesNothingWhileTheQueueHasRoom is the other half of the trade,
// and the half the decision to adopt this design rested on: the mechanism must
// be invisible whenever it is not actually saving the server.
//
// # Why the load is one record short of the depth, and not more
//
// Because more would be testing the drain goroutine's SPEED rather than the
// sink's correctness. A test loop can enqueue records far faster than any real
// producer — a synthetic burst of thousands reaches six figures per second,
// where the server under the conflict flood that motivated all this produces
// about 850 — so a larger burst drops records against a perfectly healthy
// destination and would say nothing except that the test writes quickly.
//
// One short of the depth is the largest load with NO rate dependence at all:
// even if the drain goroutine has not run once, every record fits, so a drop
// here is a defect and never a timing artefact. That the real server drops
// nothing at its real rate is established by measurement against a real server
// (rmp task #389's TEST comment: zero drops, 631.8 statements per second
// against a 632.7 baseline), which is where a claim about rates belongs.
func TestDropSink_LosesNothingWhileTheQueueHasRoom(t *testing.T) {
	// Held shut for the whole enqueue, so the queue is guaranteed to be holding
	// every record rather than racing the drain to deliver them.
	destination := newBlockingWriter()
	sink := newDropSink(destination)

	const records = logQueueDepth - 1
	for i := range records {
		if _, err := fmt.Fprintf(sink, "record-%04d\n", i); err != nil {
			t.Fatalf("Write returned an error, which a diagnostic sink never does: %v", err)
		}
	}

	destination.unblock()
	sink.Flush()
	waitFor(t, func() bool { return len(destination.records()) >= records })

	var kept []string
	for _, record := range destination.records() {
		line := strings.TrimSpace(string(record))
		if strings.HasPrefix(line, "record-") {
			kept = append(kept, line)
		}
	}
	if len(kept) != records {
		t.Errorf("the destination received %d of %d records, and every one of them fitted in the "+
			"queue: the sink is dropping records it did not have to, which would make the log of a "+
			"healthy server incomplete", len(kept), records)
	}
	for i, line := range kept {
		if want := fmt.Sprintf("record-%04d", i); line != want {
			t.Fatalf("record %d is %q, want %q: the queue is not delivering in the order it was "+
				"written, so the log would no longer be an account of what happened", i, line, want)
		}
	}
	for _, record := range destination.records() {
		if strings.Contains(string(record), "graph server diagnostics were dropped") {
			t.Errorf("nothing was dropped, yet the stream carries a dropped-records report: a false " +
				"report is worse than none, because it sends an operator looking for a gap that is " +
				"not there")
		}
	}
}

// slowWriter takes a measurable time to accept a record and announces when it
// has STARTED accepting one, so a test can position itself precisely between
// "the record has left the queue" and "the record has arrived".
type slowWriter struct {
	entered chan struct{}
	once    sync.Once
	delay   time.Duration
	mu      sync.Mutex
	written []string
}

func newSlowWriter(delay time.Duration) *slowWriter {
	return &slowWriter{entered: make(chan struct{}), delay: delay}
}

func (w *slowWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	time.Sleep(w.delay)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.written = append(w.written, string(p))
	return len(p), nil
}

func (w *slowWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.written)
}

// TestDropSink_FlushWaitsForTheLastRecordToBeDelivered pins what Flush must
// mean, which is not what it is easiest to implement.
//
// An empty QUEUE is not a delivered queue. The drain goroutine removes a record
// from the ring before it writes it, so a flush that watched only the ring would
// return while the last record was still in that goroutine's hand — and at
// process exit, still in it when the process ended. The records this costs are
// exactly the ones worth keeping: the teardown's own, a store that failed to
// close or a shutdown checkpoint that failed, all written after everything else
// has finished.
//
// The test stands in the one instant where the two definitions differ. It waits
// until the destination has ENTERED the write — so the ring is empty and the
// record is in flight — and only then flushes, asserting with no polling of its
// own that the record arrived.
func TestDropSink_FlushWaitsForTheLastRecordToBeDelivered(t *testing.T) {
	destination := newSlowWriter(300 * time.Millisecond)
	sink := newDropSink(destination)

	if _, err := sink.Write([]byte("the teardown's last word\n")); err != nil {
		t.Fatalf("Write returned an error, which a diagnostic sink never does: %v", err)
	}

	select {
	case <-destination.entered:
	case <-timeoutAfter(t, 30*time.Second):
		t.Fatalf("the drain goroutine never began writing the queued record")
	}

	sink.Flush()

	if got := destination.count(); got != 1 {
		t.Errorf("Flush returned with %d of 1 records delivered. It waited for the QUEUE to empty "+
			"rather than for the record to ARRIVE, so a diagnostic written just before the process "+
			"exits would be lost to the mechanism that exists to protect the server", got)
	}
}

// TestDropSink_FlushIsBoundedWhenTheDestinationNeverAccepts pins the property
// that keeps the flush from becoming the hang it was introduced beside.
//
// The flush exists so the teardown's last diagnostics are not lost to the queue.
// An UNBOUNDED one would mean a process whose stderr has died could not exit —
// which is FINDING #410 on the task, moved to a new line.
func TestDropSink_FlushIsBoundedWhenTheDestinationNeverAccepts(t *testing.T) {
	destination := newBlockingWriter()
	defer destination.unblock()
	sink := newDropSink(destination)

	for i := range 8 {
		_, _ = fmt.Fprintf(sink, "record-%04d\n", i)
	}

	returned := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		sink.Flush()
		returned <- time.Since(start)
	}()

	select {
	case <-returned:
	case <-timeoutAfter(t, 60*time.Second):
		t.Fatalf("Flush did not return against a destination that accepts nothing. A process whose " +
			"stderr has stopped being read must still be able to exit")
	}
}

// timeoutAfter is a named deadline for a select, so a test that hangs reports
// which bound it exceeded rather than being killed by the suite's own timeout.
func timeoutAfter(t *testing.T, d time.Duration) <-chan time.Time {
	t.Helper()
	return time.After(d)
}

// waitFor polls until the condition holds, failing the test if it never does.
// The sink delivers from its own goroutine, so a test that asserted immediately
// after Flush would be asserting against a race rather than against the sink.
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("the condition never held within 30s")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestDropSink_ASignalStillStopsAServerWhoseStderrIsNotRead drives a real server
// PROCESS with a real signal, because the worst consequence of the wedge was not
// that the server stopped answering — it was that it could not be stopped.
//
// # What it is pinning
//
// The engine's Serve waits for its per-connection goroutines before it returns,
// and internal/graphserve.stop waits for Serve. With a diagnostic write on the
// serving path and a destination that has stopped accepting, those goroutines
// never come back, so that wait never ends: MEASURED at more than 90 seconds
// after SIGTERM, against 2.5 seconds with the sink in place (rmp task #389,
// FINDING #410). A supervisor meeting that has no option but SIGKILL, which
// skips the shutdown checkpoint, the socket removal and the lock release.
//
// # Why a child process
//
// A signal is delivered to a PROCESS, and the take-over that decides what SIGTERM
// means is installed by Run. An in-process server could only be stopped through
// the orderly path, which is precisely the path this test must not assume works.
// The child is this test binary re-executed, exactly as the durability tests do.
//
// # The bound and the exit code are both load-bearing
//
// The bound catches the hang. The exit code catches its consolation prize: a
// process that died some other way — cut short, or killed by its own signal
// default — is not a process that drained, checkpointed and released its lock,
// and only a clean 0 says it did. The vanished socket says the same thing from
// the other side.
//
// Shown to fail without the change: with stderrLogger returning the logger built
// over the destination directly, the process had not exited 90 seconds after
// SIGTERM and the test fails on its bound.
func TestDropSink_ASignalStillStopsAServerWhoseStderrIsNotRead(t *testing.T) {
	root := graphRoot(t)
	socket := filepath.Join(root, "graph.sock")

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary to re-execute: %v", err)
	}
	announced, err := os.Create(filepath.Join(root, "announced"))
	if err != nil {
		t.Fatalf("creating the child's stdout file: %v", err)
	}
	defer announced.Close() //nolint:errcheck // the child holds its own descriptor

	// The child's stderr is a pipe this test NEVER reads while the child runs.
	// That is the whole condition: an operator's log shipper that died, a
	// supervisor that stopped draining, a `| head` that went away.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the pipe that stands in for an unread stderr: %v", err)
	}
	defer reader.Close() //nolint:errcheck // the assertions are about the child, not the pipe teardown

	child := exec.Command(self)
	child.Stdout = announced
	child.Stderr = writer
	child.Env = append(os.Environ(),
		childRoleEnv+"=1",
		childGraphDirEnv+"="+filepath.Join(root, "graph"),
		childSocketEnv+"="+socket,
	)
	if err := child.Start(); err != nil {
		t.Fatalf("starting the child server: %v", err)
	}
	// Only the child may hold the write end, or the pipe never reports EOF.
	_ = writer.Close() //nolint:errcheck // the child inherited its own descriptor at Start
	defer func() { _ = child.Process.Kill() }()

	waitUntilServed(t, socket)

	if _, err := graphclient.Send(context.Background(), socket,
		"CREATE (n:Hot {key:'contended', counter:0})"); err != nil {
		t.Fatalf("seeding the contended node: %v", err)
	}

	// Fill the child's pipe buffer with conflict records.
	const writers = 16
	const perWriter = 200
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	var wg sync.WaitGroup
	for writer := range writers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for statement := range perWriter {
				if ctx.Err() != nil {
					return
				}
				_, _ = graphclient.Send(ctx, socket,
					fmt.Sprintf("MATCH (n:Hot {key:'contended'}) SET n.counter = %d",
						id*perWriter+statement))
			}
		}(writer)
	}
	wg.Wait()
	cancel()

	start := time.Now()
	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signalling the child: %v", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	var waitErr error
	select {
	case waitErr = <-exited:
	case <-timeoutAfter(t, 60*time.Second):
		t.Fatalf("the server had not exited 60s after SIGTERM, with its stderr still unread. " +
			"A blocked diagnostic write is holding the engine's connection goroutines, and Serve " +
			"waits for them, so the shutdown cannot complete: a supervisor's only remaining option " +
			"is SIGKILL, which skips the shutdown checkpoint, the socket removal and the lock " +
			"release (rmp task #389)")
	}
	if waitErr != nil {
		t.Errorf("the server exited %v after SIGTERM, want a clean exit 0. It stopped, but not by "+
			"draining, checkpointing and releasing its lock, which is what the 0 row of "+
			"SPEC/COMMANDS.md § Serve Exit Codes promises", waitErr)
	}
	if _, err := os.Lstat(socket); !os.IsNotExist(err) {
		t.Errorf("the socket %s still exists after a graceful stop (lstat error: %v): the shutdown "+
			"sequence did not reach its last step", socket, err)
	}
	t.Logf("stopped %s after SIGTERM with stderr unread", time.Since(start).Round(time.Millisecond))

	// Non-vacuity: the child's pipe must actually have filled, or this run never
	// reached the condition. Read it only now — the write end went with the
	// child, so the copy ends at EOF.
	var leftover bytes.Buffer
	if _, err := io.Copy(&leftover, reader); err != nil {
		t.Fatalf("reading what the child left in the pipe: %v", err)
	}
	const bufferFloor = 60 * 1024
	if leftover.Len() < bufferFloor {
		t.Errorf("the child's unread stderr held only %d bytes at exit, want at least %d: the load "+
			"never filled the pipe, so this run did not reach the condition the test exists for",
			leftover.Len(), bufferFloor)
	}
}
