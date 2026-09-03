// Package graphserve — the shutdown's behaviour when a session is parked on a
// socket whose peer has stopped reading (rmp task #396).
//
// # What these two tests divide between them
//
// [TestServerListener_CutsOnlyTheWriteThatSpannedTheDrain] pins the SELECTOR, in
// isolation and deterministically: which connection the shutdown closes, and
// which three it must leave alone. It needs no engine and no server.
//
// [TestSignalledServer_StopsWhenAPeerHasStoppedReading] pins the OUTCOME against
// a real server process and a real signal: a peer that stopped reading no longer
// holds the shutdown for the engine's write deadline, and the shutdown that
// results is a whole one — the checkpoint is still taken, the socket still goes,
// and the exit code is still 0.
//
// # Why the cause is a peer and not a sink
//
// rmp task #389 took the diagnostic sink off the serving path, so a blocked
// stderr can no longer park a session goroutine and a test written around one
// would be testing a cause that has been removed. The cause used here is the one
// that remains and that no option removes: a client that asks for a large result
// and stops reading it.
package graphserve

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/packstream"
	"github.com/FlavioCFOliveira/GoGraph/bolt/proto"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
)

// blockedPeerRows is how many nodes the wedging statement returns. It has to
// produce more than one socket send buffer — 208 KiB is the Linux default — so
// that the server's write blocks with the peer still connected and still not
// reading. Measured: the server parks in a socket write 22 ms after the PULL at
// this size (rmp task #396, TEST #417).
const blockedPeerRows = 4000

// blockedPeerFloor is the least the wedging client must find waiting in its
// socket when the server has gone. It is the non-vacuity check: a run that
// delivered less than a buffer's worth never filled the buffer, so the server
// never blocked and the test would be asserting nothing. It is deliberately below
// the 208 KiB default so a machine tuned lower does not read as a defect.
const blockedPeerFloor = 128 * 1024

// blockedPeerShutdownBound is how long the signalled server may take to exit.
//
// It sits between the two measured outcomes and touches neither. With the cut it
// is the drain's own bound plus a teardown — 7.5 s and change. Without it the
// server holds until the engine's per-message write deadline expires, which is
// ConnTimeout, 60 s at connTimeoutMultiple times the statement budget: MEASURED
// at 60.035 s (rmp task #396, FINDING #416). Thirty seconds clears the first by
// four times and is only half the second.
const blockedPeerShutdownBound = 30 * time.Second

// TestServerListener_CutsOnlyTheWriteThatSpannedTheDrain pins the whole of the
// selector, in both directions, without an engine anywhere near it.
//
// The claim under test is NOT "a connection that is writing is cut". It is "a
// connection on which ONE write has been outstanding for the whole of the drain
// is cut", and the difference between those two is what keeps a healthy client
// from losing an answer it was microseconds away from receiving. The four cases
// below are the four states a connection can be in when the drain ends, and three
// of them are ways the weaker reading would be wrong.
//
// Each case drives the sequence counter deterministically rather than by timing:
// a write to a peer that is not reading parks with the counter odd, and the same
// peer reading is what completes it. Nothing here sleeps.
func TestServerListener_CutsOnlyTheWriteThatSpannedTheDrain(t *testing.T) {
	cases := []struct {
		// before runs ahead of markWrites and between runs after it, so a case
		// can put the connection into one state when the mark is taken and
		// another when the cut is made. A nil stage is a stage that case does not
		// need.
		before  func(w *parkedWriter, t *testing.T)
		between func(w *parkedWriter, t *testing.T)
		name    string
		why     string
		wantCut int
	}{
		{
			name:    "idle between writes",
			wantCut: 0,
			why: "a connection with no write outstanding is where a session inside an engine " +
				"call is, and that is the session the shutdown must go on waiting for",
		},
		{
			name:    "the write outstanding at the mark completed before the cut",
			before:  (*parkedWriter).park,
			between: (*parkedWriter).release,
			wantCut: 0,
			why: "a write that COMPLETES is a healthy write however long it took, and cutting " +
				"its connection would take from a reading client an answer it had just received",
		},
		{
			name:    "a different write is outstanding at the cut",
			before:  (*parkedWriter).park,
			between: (*parkedWriter).releaseAndParkAnother,
			wantCut: 0,
			why: "this is the case a boolean cannot see. The connection is writing at the mark " +
				"and writing at the cut, and it is nevertheless making progress: only a " +
				"counter that identifies WHICH write can tell that apart from a parked one",
		},
		{
			name:    "a whole write began and finished after the mark",
			between: (*parkedWriter).parkAndRelease,
			wantCut: 0,
			why: "this is the state of the client rmp task #369's drain test holds open: the " +
				"mark is taken while the connection is idle, the answer to its COMMIT is " +
				"written during the drain, and the client reads it. An even mark can never " +
				"select, which is WHY that fence stays green rather than an assumption that it does",
		},
		{
			name:    "one write outstanding throughout",
			before:  (*parkedWriter).park,
			wantCut: 1,
			why: "a write outstanding for longer than the drain — which is longer than the " +
				"longest lawful statement — is a write the peer is never going to take",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ln, server, peer := acceptOne(t)
			writer := &parkedWriter{conn: server, peer: peer}
			t.Cleanup(writer.stop)

			if testCase.before != nil {
				testCase.before(writer, t)
			}
			ln.markWrites()
			if testCase.between != nil {
				testCase.between(writer, t)
			}
			cut := ln.cutBlockedWrites()

			if cut != testCase.wantCut {
				t.Errorf("cutBlockedWrites closed %d connections, want %d: %s",
					cut, testCase.wantCut, testCase.why)
			}
			if testCase.wantCut > 0 {
				writer.requireTheParkedWriteWasReleased(t)
			}
		})
	}
}

// parkedWriter drives one connection's write side into and out of the parked
// state, so a case can say WHEN a write is outstanding rather than hoping.
//
// The mechanism is the socket's own send buffer: a write larger than it parks
// against a peer that is not reading, and the same peer reading is the only thing
// that completes it. That is the production condition exactly — a client that
// asked for a result and stopped taking it — reduced to two bytes and a read.
type parkedWriter struct {
	conn   *drainConn
	peer   net.Conn
	failed chan error
}

// parkedWriterBlob is comfortably larger than any socket send buffer a Linux
// default sets, so one write of it cannot complete unread.
const parkedWriterBlob = 8 << 20

// park starts a write that cannot complete and returns once it is outstanding.
func (w *parkedWriter) park(t *testing.T) {
	t.Helper()

	before := w.conn.writeSeq.Load()
	w.failed = make(chan error, 1)
	go func() {
		_, err := w.conn.Write(make([]byte, parkedWriterBlob))
		w.failed <- err
	}()
	waitFor(t, func() bool {
		now := w.conn.writeSeq.Load()
		return now > before && now%2 == 1
	})
}

// release lets the peer take everything, so the parked write completes.
func (w *parkedWriter) release(t *testing.T) {
	t.Helper()

	before := w.conn.writeSeq.Load()
	go func() {
		buf := make([]byte, 1<<20)
		for {
			if _, err := w.peer.Read(buf); err != nil {
				return
			}
		}
	}()
	waitFor(t, func() bool {
		now := w.conn.writeSeq.Load()
		return now > before && now%2 == 0
	})
	select {
	case err := <-w.failed:
		if err != nil {
			t.Fatalf("the write the peer drained failed anyway: %v", err)
		}
	case <-timeoutAfter(t, 30*time.Second):
		t.Fatalf("the write did not return although the counter says it completed")
	}
	w.failed = nil
}

// releaseAndParkAnother completes the write outstanding at the mark and parks a
// SECOND one, so the connection is writing at the cut and is not the same write.
func (w *parkedWriter) releaseAndParkAnother(t *testing.T) {
	t.Helper()

	w.release(t)
	// The peer's reader above is still draining, so a second blob has to be big
	// enough to outrun it rather than merely bigger than the buffer. Stopping the
	// reader is what makes the second write park, and closing the peer would
	// close the connection this case is about.
	w.stopPeerReading(t)
	w.park(t)
}

// parkAndRelease drives one COMPLETE write, from a connection that was idle at
// the mark. It is the shape of a server answering a statement during the drain.
func (w *parkedWriter) parkAndRelease(t *testing.T) {
	t.Helper()

	w.park(t)
	w.release(t)
}

// stopPeerReading arms a read deadline in the past, so the peer's draining
// goroutine fails its next read and stops taking bytes, without the connection
// being closed.
func (w *parkedWriter) stopPeerReading(t *testing.T) {
	t.Helper()

	if err := w.peer.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("stopping the peer from reading: %v", err)
	}
}

// requireTheParkedWriteWasReleased is the other half of a cut: closing the socket
// has to RELEASE the goroutine, which is the whole reason the shutdown does it.
func (w *parkedWriter) requireTheParkedWriteWasReleased(t *testing.T) {
	t.Helper()

	select {
	case err := <-w.failed:
		if err == nil {
			t.Errorf("the parked write returned no error, so it completed on its own and this " +
				"run never held the connection the cut is supposed to release")
		}
		w.failed = nil
	case <-timeoutAfter(t, 30*time.Second):
		t.Errorf("the parked write had not returned 30s after the cut: closing the socket did " +
			"not release the goroutine, and releasing it is the entire repair")
	}
}

// stop unwinds whatever is still outstanding, so a failing case cannot leave a
// goroutine parked on a socket for the rest of the run.
func (w *parkedWriter) stop() {
	_ = w.peer.Close() //nolint:errcheck // releasing the write; the assertions are above
	_ = w.conn.Close() //nolint:errcheck // idem
	if w.failed != nil {
		<-w.failed
	}
}

// acceptOne binds a listener, dials it, and returns the wrapper, the server's end
// and the peer's end of the one connection.
func acceptOne(t *testing.T) (*serverListener, *drainConn, net.Conn) {
	t.Helper()

	root := graphRoot(t)
	raw, err := net.Listen("unix", filepath.Join(root, "sel.sock"))
	if err != nil {
		t.Fatalf("binding the selector's listener: %v", err)
	}
	ln := newServerListener(raw)
	t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // the wrapper's Close reports no error by construction

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			accepted <- nil
			return
		}
		accepted <- conn
	}()

	peer, err := net.Dial("unix", raw.Addr().String())
	if err != nil {
		t.Fatalf("dialling the selector's listener: %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() }) //nolint:errcheck // the assertions are about the cut, not the teardown

	conn := <-accepted
	if conn == nil {
		t.Fatalf("the selector's listener accepted nothing")
	}
	server, ok := conn.(*drainConn)
	if !ok {
		t.Fatalf("Accept returned %T, want a *drainConn: the listener no longer wraps what it "+
			"accepts, and the shutdown has nothing left to observe", conn)
	}
	return ln, server, peer
}

// TestSignalledServer_StopsWhenAPeerHasStoppedReading is the acceptance test: a
// real server process, a real client that asks for a large result and stops
// reading it, a real SIGTERM.
//
// # What holds the shutdown, and why the drain does not help
//
// The engine's Serve returns only when every connection goroutine has, and
// internal/graphserve.stop waits for Serve. A session parked in a socket write
// comes back when the engine's per-message write deadline expires and not
// before — ConnTimeout, 60 s here — so the whole shutdown waits that long. The
// drain cannot shorten it: a live connection is not quiescent, so the drain
// spends its entire budget and then cuts, and the engine's own cut is a context
// cancellation, which does not release a goroutine parked in a write.
//
// # What is asserted beyond the bound
//
// Three things, and each of them is a way a bounded exit could still be the wrong
// one. The exit code, because a process that died another way did not drain. The
// vanished socket, because step 6 is the last one. And the FOLDED write-ahead log
// beside a snapshot manifest, because that is step 4 — the shutdown checkpoint —
// and the point of cutting rather than abandoning is that every later step still
// runs. A repair that bounded the wait by giving up on it would pass the first
// two and fail the third.
//
// Shown to fail without the change: with ln.cutBlockedWrites() removed from stop,
// the child had not exited 30 s after SIGTERM and the test fails on its bound;
// the same run measured 60.0 s to exit unaided.
func TestSignalledServer_StopsWhenAPeerHasStoppedReading(t *testing.T) {
	root := graphRoot(t)
	socket := filepath.Join(root, "graph.sock")
	graphDir := filepath.Join(root, "graph")

	diagnostics := filepath.Join(root, "diagnostics")
	child := startBlockedPeerServer(t, root, socket, graphDir)

	waitUntilServed(t, socket)
	seedBlockedPeerRows(t, socket, blockedPeerRows)

	// One session asks for every row and never reads a byte of the answer. Its
	// connection stays open throughout: a client that had gone away would be a
	// closed socket, which releases the write and is not the condition.
	wedged := dialBolt(t, socket)
	wedged.sendWithoutReading(t, "RUN", &proto.Run{
		Query:      "MATCH (n:Row) RETURN n",
		Parameters: map[string]packstream.Value{},
		Extra:      map[string]packstream.Value{},
	})
	wedged.sendWithoutReading(t, "PULL", &proto.Pull{N: -1, QID: -1})

	// Give the server time to fill the send buffer and park. The measured figure
	// is 22 ms; this is two orders of magnitude of headroom, and the byte floor
	// below is what actually establishes that the condition was reached.
	time.Sleep(2 * time.Second)

	start := time.Now()
	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signalling the server: %v", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	var waitErr error
	select {
	case waitErr = <-exited:
	case <-timeoutAfter(t, blockedPeerShutdownBound):
		t.Fatalf("the server had not exited %s after SIGTERM, with one client parked on a "+
			"result it stopped reading. The engine's Serve waits for that connection's "+
			"goroutine and it does not come back until the write deadline expires "+
			"(ConnTimeout, %s), so the shutdown cannot complete inside a supervisor's grace "+
			"period and its only remaining option is SIGKILL — which skips the shutdown "+
			"checkpoint, the socket removal and the lock release (rmp task #396)",
			blockedPeerShutdownBound, connTimeoutMultiple*5*time.Second)
	}
	elapsed := time.Since(start)

	if waitErr != nil {
		t.Errorf("the server exited %v after SIGTERM, want a clean exit 0. It stopped, but not "+
			"by draining, checkpointing and releasing its lock, which is what the 0 row of "+
			"SPEC/COMMANDS.md § Serve Exit Codes promises", waitErr)
	}
	if _, err := os.Lstat(socket); !os.IsNotExist(err) {
		t.Errorf("the socket %s still exists after a graceful stop (lstat error: %v): the "+
			"shutdown sequence did not reach its last step", socket, err)
	}

	// Step 4 still ran. Cutting a blocked socket releases a goroutine; it must
	// not skip the checkpoint that folds the write-ahead log into the snapshot,
	// which is the difference between this repair and one that bounded the wait
	// by abandoning it.
	if size := walSize(t, filepath.Join(graphDir, "wal")); size != 0 {
		t.Errorf("the write-ahead log holds %d bytes after the shutdown, want 0: the shutdown "+
			"checkpoint did not fold it into the snapshot, so the cut abandoned step 4 of "+
			"SPEC/GRAPH.md § Server Shutdown and the Drain instead of releasing a socket", size)
	}
	manifest := filepath.Join(graphDir, "snapshot", "manifest.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Errorf("no snapshot manifest at %s after the shutdown (%v): the log was truncated "+
			"without a snapshot to recover from, or no checkpoint was taken at all", manifest, err)
	}

	// Non-vacuity: the wedging client must find a full socket buffer waiting for
	// it. A run that delivered less than that never blocked the server, so it
	// never reached the condition this test exists for.
	pending := drainWedgedPeer(t, wedged)
	if pending < blockedPeerFloor {
		t.Errorf("the client that stopped reading found only %d bytes waiting when the server "+
			"had gone, want at least %d: the send buffer never filled, so no session was ever "+
			"parked in a write and this run asserted nothing", pending, blockedPeerFloor)
	}
	// The shutdown says what it did. Without this the only trace is the engine's
	// own write error, which reads as a network fault rather than as a choice the
	// server made.
	requireShutdownCutRecord(t, diagnostics, 1)

	t.Logf("stopped %s after SIGTERM with one peer parked on %d unread bytes",
		elapsed.Round(time.Millisecond), pending)
}

// cutRecordMessage is the substring that identifies the shutdown's own record.
// It is the message and not the whole line, because everything around the message
// — the attribute order, the level's rendering, the quoting — is
// slog.TextHandler's and belongs to the standard library rather than to this
// project (SPEC/GRAPH.md § Server Diagnostics on Stderr).
const cutRecordMessage = "shutdown closed connections whose peer had stopped reading"

// canonicalTimestamp is the one format § Server Diagnostics on Stderr fixes for
// every record: UTC, exactly three digits of milliseconds, an explicit Z.
var canonicalTimestamp = regexp.MustCompile(
	`(?m)^time=\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z `)

// requireShutdownCutRecord asserts that the server published exactly one record
// for the connections its shutdown closed, in the shape the specification fixes.
//
// Four things are checked and each is a way the record could be present and still
// be wrong: that it exists at all; that it is ONE line, because a record that
// spanned two could not be dropped whole and could forge a second record on a
// console; that it carries the COUNT, because "some connections" is not a report;
// and that its timestamp is the project's canonical UTC form rather than
// slog.TextHandler's local-zone default.
func requireShutdownCutRecord(t *testing.T, diagnostics string, wantConnections int) {
	t.Helper()

	written, err := os.ReadFile(diagnostics) //nolint:gosec // the path is this test's own temporary directory
	if err != nil {
		t.Fatalf("reading the server's diagnostics at %s: %v", diagnostics, err)
	}

	var matched []string
	for _, line := range strings.Split(string(written), "\n") {
		if strings.Contains(line, cutRecordMessage) {
			matched = append(matched, line)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("the server's stderr carries %d records for the connections its shutdown "+
			"closed, want exactly 1. Without one, the only trace of a connection the SERVER "+
			"chose to close is the engine's own `bolt: write error`, which reads as a network "+
			"fault a client caused. Stderr:\n%s", len(matched), written)
	}

	record := matched[0]
	if !strings.HasPrefix(record, "time=") {
		t.Errorf("the record does not begin a line, so it is not one record on one line "+
			"(SPEC/GRAPH.md § Server Diagnostics on Stderr): %q", record)
	}
	if !canonicalTimestamp.MatchString(record) {
		t.Errorf("the record's timestamp is not the canonical UTC form the specification "+
			"fixes — YYYY-MM-DDTHH:mm:ss.sssZ, three digits and an explicit Z: %q", record)
	}
	if !strings.Contains(record, "level=WARN") {
		t.Errorf("the record is not at WARN, so a server left at the default level would "+
			"not carry it where it matters: %q", record)
	}
	t.Logf("the shutdown reported: %s", record)

	want := "connections=" + strconv.Itoa(wantConnections)
	if !strings.Contains(record, want) {
		t.Errorf("the record does not carry %s, so it says that something was closed and not "+
			"WHAT: %q", want, record)
	}
}

// startBlockedPeerServer re-executes the test binary as a graph server, exactly
// as the durability tests do, and returns the running child.
//
// It is written here rather than reusing startServerProcess because that helper
// waits for the child on the test's cleanup and this test waits for it itself,
// on a bound that is the whole assertion.
func startBlockedPeerServer(t *testing.T, root, socket, graphDir string) *exec.Cmd {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary to re-execute: %v", err)
	}
	announced, err := os.Create(filepath.Join(root, "announced"))
	if err != nil {
		t.Fatalf("creating the child's stdout file: %v", err)
	}
	t.Cleanup(func() { _ = announced.Close() }) //nolint:errcheck // the child holds its own descriptor
	diagnostics, err := os.Create(filepath.Join(root, "diagnostics"))
	if err != nil {
		t.Fatalf("creating the child's stderr file: %v", err)
	}
	t.Cleanup(func() { _ = diagnostics.Close() }) //nolint:errcheck // idem

	child := exec.Command(self)
	child.Stdout = announced
	child.Stderr = diagnostics
	child.Env = append(os.Environ(),
		childRoleEnv+"=1",
		childGraphDirEnv+"="+graphDir,
		childSocketEnv+"="+socket,
	)
	if err := child.Start(); err != nil {
		t.Fatalf("starting the child server: %v", err)
	}
	t.Cleanup(func() { _ = child.Process.Kill() }) //nolint:errcheck // a child that already exited cannot be killed
	return child
}

// seedBlockedPeerRows writes the nodes the wedging statement returns, through the
// one client every surface uses.
func seedBlockedPeerRows(t *testing.T, socket string, rows int) {
	t.Helper()

	const batch = 500
	for lo := 0; lo < rows; lo += batch {
		hi := min(lo+batch, rows) - 1
		// The property is long enough that the result outgrows a socket send
		// buffer at blockedPeerRows without needing a graph big enough to make
		// the seed itself slow.
		statement := fmt.Sprintf("UNWIND range(%d, %d) AS i CREATE (:Row {seq: i, "+
			"blob: '%s'})", lo, hi, string(bytes.Repeat([]byte("payload."), 10)))
		if _, err := graphclient.Send(context.Background(), socket, statement); err != nil {
			t.Fatalf("seeding %q: %v", statement, err)
		}
	}
}

// sendWithoutReading sends one request and reads NOTHING back, which is the whole
// of what makes this client a wedge rather than a client.
func (s *boltSession) sendWithoutReading(t *testing.T, label string, request any) {
	t.Helper()

	var buf bytes.Buffer
	enc := packstream.NewEncoder(&buf)
	if err := proto.EncodeRequest(enc, request); err != nil {
		t.Fatalf("encoding %s: %v", label, err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("encoding %s: %v", label, err)
	}
	if err := s.writer.WriteMessage(buf.Bytes()); err != nil {
		t.Fatalf("sending %s: %v", label, err)
	}
}

// drainWedgedPeer reads everything the server left in the wedging client's socket
// and reports how many bytes there were.
func drainWedgedPeer(t *testing.T, s *boltSession) int {
	t.Helper()

	if err := s.conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatalf("arming the wedging client's read deadline: %v", err)
	}
	var sink discardCounter
	// The server has gone, so the copy ends at EOF or at the reset the close
	// produced; either is the end of what was queued and neither is a failure of
	// this test's subject.
	_, _ = io.Copy(&sink, s.conn) //nolint:errcheck // the count is the assertion, not the error
	return sink.n
}

// discardCounter counts what is written to it and keeps none of it. The wedging
// client's backlog is a socket buffer's worth of encoded records and only its
// SIZE is evidence.
type discardCounter struct {
	n  int
	mu sync.Mutex
}

func (d *discardCounter) Write(p []byte) (int, error) {
	d.mu.Lock()
	d.n += len(p)
	d.mu.Unlock()
	return len(p), nil
}
