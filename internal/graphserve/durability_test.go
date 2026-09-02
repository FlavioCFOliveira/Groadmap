// Package graphserve — the durability regression tests: what a killed server
// loses, and what a signalled one saves.
//
// # What these two tests are for
//
// SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process makes two
// promises about a server that runs indefinitely, and neither is observable from
// a test that starts a server and stops it politely:
//
//   - every acknowledged commit is on disk BEFORE the client is told it
//     succeeded, so a server killed outright loses nothing it acknowledged;
//   - a signal DRAINS rather than truncates, and the shutdown folds the
//     write-ahead log into the snapshot before it releases the lock.
//
// Both need a real operating-system process, because both are about what happens
// when one dies: SIGKILL cannot be caught, and a signal handler is a property of
// a process rather than of a goroutine. main_test.go says how the child process
// is obtained and why it runs the production entry point rather than a
// re-assembly of its steps.
//
// # What each test would pass over, if it were written more weakly
//
// An engine constructed over a bare in-memory graph accepts every write,
// acknowledges every commit, warns about nothing, and loses all of it when the
// process ends — and a CLEAN shutdown hides even that, because the shutdown
// checkpoint snapshots the in-memory graph the engine did mutate. So the
// only observation that separates a write-ahead-log-backed server from one
// without a log is a KILL, and that is why the first test kills rather than
// signals. Symmetrically, a shutdown that skipped its checkpoint loses no
// committed data at all — the log still holds it — so the only observation that
// separates a shutdown that checkpointed from one that did not is the state of
// the log and the snapshot on disk afterwards, and that is why the second test
// asserts those rather than only the answer the client got.
//
// # What the "durable before acknowledged" assertion here does and does not prove
//
// After each acknowledged commit the parent reads the write-ahead log's size from
// outside the server's process and requires it to have grown. That proves the
// transaction's frames left the writer's user-space buffer and reached the FILE
// before the acknowledgement crossed the socket — which is exactly what a
// removed commit synchronisation breaks, because the frames then sit in a 64 KiB
// buffer instead. It does NOT by itself prove the fsync: a written-but-unsynced
// page is still visible to another process, so this observation cannot tell a
// synchronised write from an unsynchronised one. The fsync ORDERING was
// established separately, by tracing the server's system calls against the
// built binary, and is recorded on rmp task #369. What this file adds to that is
// the part a trace cannot give: that the bytes survive the death of the process
// that wrote them.
//
// The limit that follows from this is stated rather than left to be discovered:
// removing the whole commit synchronisation is caught here, because the frames
// then never leave the buffer; removing ONLY the fsync and keeping the flush
// would not be, because a written-but-unsynced page survives a process kill just
// as a synced one does. Nothing short of losing the machine distinguishes those
// two, and no test in this repository does.
package graphserve

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/packstream"
	"github.com/FlavioCFOliveira/GoGraph/bolt/proto"
	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
)

// serverStartDeadline bounds the wait for a child server to answer the
// resolution probe. It is generous rather than tight: the child is the test
// binary re-executed, it opens a store through recovery, and under the race
// detector both cost several times what they cost otherwise. A tight bound here
// would produce a flake that reads as a durability defect.
const serverStartDeadline = 60 * time.Second

// shutdownDeadline bounds the wait for a signalled child to exit. It has to
// clear the drain's own bound with room to spare, and the drain is bounded by
// the graph store's wait budget while the shutdown behind it is not bounded at
// all (SPEC/GRAPH.md § Server Shutdown and the Drain).
const shutdownDeadline = 90 * time.Second

// serverProcess is one child process running the production server.
type serverProcess struct {
	cmd      *exec.Cmd
	stdout   string
	stderr   string
	socket   string
	graphDir string
	waited   bool
}

// graphRoot creates a directory to hold one test's graph store and socket, and
// returns it.
//
// os.MkdirTemp rather than t.TempDir, and deliberately. A Unix domain socket path
// is capped at 108 bytes, t.TempDir names its directory after the TEST, and a
// descriptive test name pushes the socket past the cap — where the bind fails
// with "invalid argument" and reads as a defect in the code under test rather
// than in its own harness (rmp task #368, FINDING #275). A short fixed prefix
// keeps the whole path well inside the cap whatever the test is called.
func graphRoot(t *testing.T) string {
	t.Helper()

	root, err := os.MkdirTemp("", "rmpd")
	if err != nil {
		t.Fatalf("creating the test's graph root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	if err := os.MkdirAll(filepath.Join(root, "graph"), 0700); err != nil {
		t.Fatalf("creating the graph directory: %v", err)
	}
	return root
}

// startServerProcess re-executes the test binary as a graph server over root's
// graph directory and socket, waits until it answers, and returns the handle.
//
// tag distinguishes one server's captured output from another's when a test runs
// more than one against the same root, which the relaunch cases do.
func startServerProcess(t *testing.T, root, tag string) *serverProcess {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary to re-execute: %v", err)
	}

	p := &serverProcess{
		stdout:   filepath.Join(root, tag+".out"),
		stderr:   filepath.Join(root, tag+".err"),
		socket:   filepath.Join(root, "graph.sock"),
		graphDir: filepath.Join(root, "graph"),
	}

	out, err := os.Create(p.stdout)
	if err != nil {
		t.Fatalf("creating %s: %v", p.stdout, err)
	}
	defer out.Close() //nolint:errcheck // the child holds its own descriptor; this one is only for the handover
	errOut, err := os.Create(p.stderr)
	if err != nil {
		t.Fatalf("creating %s: %v", p.stderr, err)
	}
	defer errOut.Close() //nolint:errcheck // idem

	p.cmd = exec.Command(self)
	p.cmd.Stdout = out
	p.cmd.Stderr = errOut
	p.cmd.Env = append(os.Environ(),
		childRoleEnv+"=1",
		childGraphDirEnv+"="+p.graphDir,
		childSocketEnv+"="+p.socket,
	)
	if err := p.cmd.Start(); err != nil {
		t.Fatalf("starting the child server: %v", err)
	}
	t.Cleanup(func() { p.abandon() })

	p.waitUntilServed(t)
	p.requireAnnounced(t)
	return p
}

// waitUntilServed blocks until the child answers the resolution probe, through
// the one resolver every surface uses rather than through a sleep somebody chose.
func (p *serverProcess) waitUntilServed(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(serverStartDeadline)
	for {
		state, _ := graphclient.Resolve(context.Background(), p.socket)
		if state.Served() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child server at %s did not answer within %s (state %v).\nstderr:\n%s",
				p.socket, serverStartDeadline, state, p.capturedStderr(t))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// requireAnnounced asserts that the child announced the socket it was told to
// bind, which is what establishes that it reached step 7 of the startup sequence
// rather than being answered by something else on that path.
func (p *serverProcess) requireAnnounced(t *testing.T) {
	t.Helper()

	announced := bytes.TrimSpace(readFile(t, p.stdout))
	if string(announced) != p.socket {
		t.Fatalf("the child announced %q on stdout, want the socket it was asked to bind, %q",
			announced, p.socket)
	}
}

// signalAndWait sends sig and requires the child to exit 0 within the shutdown
// deadline, which is the exit code SPEC/COMMANDS.md § Serve Exit Codes fixes for
// a server stopped by a signal.
func (p *serverProcess) signalAndWait(t *testing.T, sig os.Signal) {
	t.Helper()

	if err := p.cmd.Process.Signal(sig); err != nil {
		t.Fatalf("sending %v to the child server: %v", sig, err)
	}
	if err := p.waitWithin(shutdownDeadline); err != nil {
		t.Fatalf("the child server did not exit 0 after %v: %v\nstderr:\n%s",
			sig, err, p.capturedStderr(t))
	}
}

// killAndWait kills the child outright and asserts that SIGKILL is what ended
// it — not an orderly exit that happened to race the kill, which would make
// everything after it a test of a clean shutdown wearing a kill's name.
func (p *serverProcess) killAndWait(t *testing.T) {
	t.Helper()

	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatalf("killing the child server: %v", err)
	}
	err := p.waitWithin(shutdownDeadline)

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the killed child server reported %v, want an exit error carrying SIGKILL. "+
			"A server that exited cleanly here was never killed, and every assertion below "+
			"would be about an orderly shutdown instead", err)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("the child server ended as %v, want termination by SIGKILL", exitErr)
	}
}

// waitWithin waits for the child, bounded, so a server that never returns fails
// the test with its own message instead of the package-wide test timeout.
func (p *serverProcess) waitWithin(d time.Duration) error {
	if p.waited {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		p.waited = true
		return err
	case <-time.After(d):
		_ = p.cmd.Process.Kill() //nolint:errcheck // already failing; the kill is to stop the run leaking a process
		<-done
		p.waited = true
		return fmt.Errorf("the child server was still running after %s", d) //nolint:err113 // a test's own diagnostic
	}
}

// abandon stops a child a failed test left behind, so no server outlives the run
// holding the store lock.
func (p *serverProcess) abandon() {
	if p.waited || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Kill() //nolint:errcheck // best effort cleanup
	_ = p.cmd.Wait()         //nolint:errcheck // idem
	p.waited = true
}

// capturedStderr returns whatever the child wrote to stderr, for a failure
// message. A child that refused to start explains itself there and nowhere else.
func (p *serverProcess) capturedStderr(t *testing.T) string {
	t.Helper()
	return string(readFile(t, p.stderr))
}

// walPath is the write-ahead log inside this server's graph directory.
func (p *serverProcess) walPath() string { return filepath.Join(p.graphDir, "wal") }

// readFile reads a whole file, failing the test rather than the caller.
func readFile(t *testing.T, path string) []byte {
	t.Helper()

	b, err := os.ReadFile(path) //nolint:gosec // a path this test built inside its own temporary directory
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return b
}

// walSize is the write-ahead log's size on disk, read from OUTSIDE the server's
// process. That is the point of it: it observes what the file holds, not what the
// server believes it wrote.
func walSize(t *testing.T, path string) int64 {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Size()
}

// mustSend runs statement against the server at socket and requires it to
// succeed, through the one client every surface uses.
func mustSend(t *testing.T, socket, statement string) *graphclient.Result {
	t.Helper()

	result, err := graphclient.Send(context.Background(), socket, statement)
	if err != nil {
		t.Fatalf("running %q against %s: %v", statement, socket, err)
	}
	return result
}

// stringColumn reads one column of a result as strings, failing on any value the
// statement was not written to produce.
func stringColumn(t *testing.T, result *graphclient.Result) []string {
	t.Helper()

	values := make([]string, 0, len(result.Rows))
	for i, row := range result.Rows {
		if len(row) != 1 {
			t.Fatalf("row %d carries %d columns, want exactly 1", i, len(row))
		}
		s, ok := row[0].(expr.StringValue)
		if !ok {
			t.Fatalf("row %d carries %T, want a string", i, row[0])
		}
		values = append(values, string(s))
	}
	sort.Strings(values)
	return values
}

// commitKeys are the keys the acknowledged commits carry, in the order they are
// written. They are the test's own record of what the server said it had done.
func commitKeys(n int) []string {
	keys := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		keys = append(keys, fmt.Sprintf("commit-%02d", i))
	}
	return keys
}

// TestKilledServer_LosesNoCommitItAcknowledged is the acceptance criterion: a
// known number of commits are acknowledged through the client, the server is
// killed outright, it is relaunched, and every one of them is present.
//
// Three things are asserted along the way that the criterion does not name, and
// each of them is what stops a weaker reading of it from passing:
//
//   - the write-ahead log GREW on disk after every acknowledged commit, observed
//     from outside the server's process. Without this the test would pass on a
//     server that buffered every frame and was saved by the relaunch only because
//     the kill happened to be graceful, which it is not; see the package comment
//     for what this observation does and does not establish.
//   - SIGKILL is what ended the process. A server that exited cleanly here was
//     never killed, and everything after it would be a test of an orderly
//     shutdown.
//   - the socket file is STILL PRESENT after the kill, which is the state
//     SPEC/GRAPH.md § Server Resolution rule 1 calls a stale socket and which the
//     relaunch has to clear for itself (§ Server Startup, step 4). A relaunch
//     over a path nothing had left behind would not exercise it.
func TestKilledServer_LosesNoCommitItAcknowledged(t *testing.T) {
	root := graphRoot(t)
	server := startServerProcess(t, root, "first")

	// The store is new, so nothing has been appended and nothing folded. If this
	// is ever non-zero the growth assertions below are measuring the wrong thing.
	if size := walSize(t, server.walPath()); size != 0 {
		t.Fatalf("the write-ahead log of a freshly opened store is %d bytes, want 0", size)
	}

	const commits = 12
	keys := commitKeys(commits)

	previous := int64(0)
	for _, key := range keys {
		mustSend(t, server.socket, fmt.Sprintf("CREATE (c:Commit {key:'%s'})", key))

		size := walSize(t, server.walPath())
		if size <= previous {
			t.Fatalf("the write-ahead log holds %d bytes after the commit of %q was "+
				"ACKNOWLEDGED, and held %d before it. An acknowledged commit must already be "+
				"on disk when the client is told it succeeded (SPEC/GRAPH.md § Durability and "+
				"Checkpointing in a Long-Lived Process, rule 1); a log that did not grow means "+
				"the frames are still in the writer's buffer. A log that SHRANK would mean a "+
				"checkpoint folded it mid-test, which its cadence makes impossible inside this "+
				"test's runtime and which would need this assertion rewritten rather than relaxed",
				size, key, previous)
		}
		previous = size
	}

	beforeKill := readFile(t, server.walPath())
	server.killAndWait(t)

	if after := readFile(t, server.walPath()); !bytes.Equal(after, beforeKill) {
		t.Errorf("the write-ahead log changed across the kill: %d bytes before, %d after. "+
			"Nothing may write to it once the process holding it is gone",
			len(beforeKill), len(after))
	}
	if _, err := os.Lstat(server.socket); err != nil {
		t.Fatalf("the killed server's socket file is gone (%v), so the relaunch below would "+
			"not exercise the stale-socket path SPEC/GRAPH.md § Server Startup step 4 exists for", err)
	}

	relaunched := startServerProcess(t, root, "second")

	present := stringColumn(t, mustSend(t, relaunched.socket, "MATCH (c:Commit) RETURN c.key"))
	if len(present) != commits {
		t.Errorf("the relaunched server holds %d of the %d commits the killed one acknowledged: %v",
			len(present), commits, present)
	}
	for i, want := range keys {
		if i >= len(present) || present[i] != want {
			t.Errorf("commit %q was acknowledged and is not present after the relaunch; the "+
				"graph holds %v", want, present)
			break
		}
	}

	relaunched.signalAndWait(t, syscall.SIGINT)
}

// TestSignalledServer_DrainsAnOpenTransaction is the second acceptance
// criterion: SIGINT arrives while an explicit transaction is open, and the
// shutdown drains it instead of cutting it.
//
// # Why an EXPLICIT transaction, and why the raw exchange below
//
// Quiescence is a conjunction — no connection is live, and no explicit
// transaction is open — and the two halves are reached by different clients. The
// unit tests drive the decision directly and internal/graphclient drives the
// first half every time it sends a statement, because a statement in flight is a
// live connection. Nothing drives the SECOND half, because the shared client
// sends autocommit statements and the engine's registry lists explicit
// transactions only. So this test opens one, which means BEGIN and COMMIT, which
// the shared client does not send.
//
// The exchange below is therefore a test fixture and not a second client: it
// exists to put the server into a state the shared client cannot put it into.
// Everything a real client does — resolution, the retry, the value mapping — is
// still the shared one's, and this test uses it for exactly that everywhere else.
//
// # What the test is watching for
//
// The signal is sent while the transaction is open, and the COMMIT is sent only
// after the socket file has gone — which is step 1 of the shutdown sequence and
// therefore proof that the shutdown is under way and inside its drain. On a
// server whose drain returned immediately, the engine's own shutdown would have
// cancelled every connection's context by then and this COMMIT would meet a
// broken connection. That is not hypothetical: it is what the drain's first
// formulation did, measured (rmp task #367, FINDING #264), and it is what this
// test reports when the drain is removed — "sending COMMIT: broken pipe".
//
// The on-disk assertions afterwards are the other half. A shutdown that skipped
// its checkpoint would answer this COMMIT exactly as well, lose no committed
// data, and leave a log the next open replays in full and a snapshot older than
// the graph — so the answer alone cannot establish that steps 4 and 5 ran.
//
// # Which half of quiescence this reaches, measured rather than assumed
//
// Quiescence is a conjunction, and this test holds the drain open through its
// FIRST half only: the client's connection is still live throughout, so a drain
// reduced to "no connection is live" passes this test unchanged. That was
// measured, not reasoned — the reduction was applied and the test stayed green —
// and the second half is pinned instead by
// TestQuiescent_IsTheConjunctionItClaimsToBe, which the same reduction fails.
//
// The second half is not reachable from out here, and the engine's own teardown
// order says why it nevertheless has to be there. A connection's handler cancels
// the connection context, CLOSES THE SOCKET — which is where this package's
// counter is decremented — then joins the reader goroutine, and only then rolls
// back the open explicit transaction and unregisters it. So there is a real
// window in which the connection count has reached zero while a transaction is
// still open and still rolling back, and its width is a goroutine join plus an
// undo. A drain keyed on the connection count alone would conclude quiescence
// inside it. Reaching that window on purpose would mean timing a test against
// that join, which is a flake rather than an assertion.
func TestSignalledServer_DrainsAnOpenTransaction(t *testing.T) {
	root := graphRoot(t)
	server := startServerProcess(t, root, "first")

	session := dialBolt(t, server.socket)
	defer session.close()

	session.requireSuccess(t, "BEGIN", &proto.Begin{Extra: map[string]packstream.Value{}})
	session.requireSuccess(t, "RUN", &proto.Run{
		Query: "CREATE (t:Drained {key:'drained-by-the-shutdown'}) RETURN t.key",
	})
	session.requireSuccess(t, "PULL", &proto.Pull{N: -1, QID: -1})

	if err := server.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("sending SIGINT to the child server: %v", err)
	}
	waitUntilSocketGone(t, server.socket)

	session.requireSuccess(t, "COMMIT", &proto.Commit{})

	// Read the log before the process exits, so what is compared afterwards is
	// what the shutdown folded rather than what it found.
	walBeforeShutdown := walSize(t, server.walPath())
	if walBeforeShutdown == 0 {
		t.Fatalf("the write-ahead log is empty after a COMMIT the server acknowledged; the "+
			"commit reached no disk (SPEC/GRAPH.md § Durability and Checkpointing in a "+
			"Long-Lived Process, rule 1). Server stderr:\n%s", server.capturedStderr(t))
	}

	session.close()
	if err := server.waitWithin(shutdownDeadline); err != nil {
		t.Fatalf("the child server did not exit 0 after SIGINT: %v\nstderr:\n%s",
			err, server.capturedStderr(t))
	}

	// Step 6: the socket does not outlive the server.
	if _, err := os.Lstat(server.socket); !os.IsNotExist(err) {
		t.Errorf("the socket file is still at %s after a clean shutdown (%v)", server.socket, err)
	}

	// Step 4: the log was folded into the snapshot and truncated. This is the
	// assertion a shutdown that skipped its checkpoint fails, and the only one
	// that can distinguish it: no committed data is lost either way.
	if after := walSize(t, server.walPath()); after != 0 {
		t.Errorf("the write-ahead log holds %d bytes after the shutdown and held %d before it. "+
			"The shutdown MUST checkpoint and truncate (SPEC/GRAPH.md § Server Shutdown and "+
			"the Drain, step 4), so that the log the next open replays is short and the "+
			"snapshot on disk is current", after, walBeforeShutdown)
	}
	manifest := filepath.Join(server.graphDir, "snapshot", "manifest.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Errorf("no snapshot manifest at %s after the shutdown (%v), so the truncation above "+
			"folded the log into nothing", manifest, err)
	}

	// And the commit the drain saved is in the graph a fresh process opens.
	relaunched := startServerProcess(t, root, "second")
	present := stringColumn(t, mustSend(t, relaunched.socket, "MATCH (t:Drained) RETURN t.key"))
	if len(present) != 1 || present[0] != "drained-by-the-shutdown" {
		t.Errorf("the transaction the shutdown drained is not in the graph afterwards; it holds %v",
			present)
	}
	relaunched.signalAndWait(t, syscall.SIGINT)
}

// waitUntilSocketGone blocks until the socket file has been unlinked, which is
// step 1 of the shutdown sequence and the earliest moment from outside the server
// at which the drain is known to be running.
func waitUntilSocketGone(t *testing.T, socket string) {
	t.Helper()

	deadline := time.Now().Add(shutdownDeadline)
	for {
		if _, err := os.Lstat(socket); os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the socket at %s was still present %s after SIGINT, so the shutdown "+
				"never reached step 1 and the drain is not what this test would be measuring",
				socket, shutdownDeadline)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// boltSession is the raw Bolt exchange this file needs and internal/graphclient
// does not provide: BEGIN and COMMIT.
//
// It is a fixture, not a client. See TestSignalledServer_DrainsAnOpenTransaction
// for why one exists at all, and note what it does NOT do: it offers a single
// protocol version rather than deriving an offer, because the server it talks to
// is the one this test started a moment ago and internal/graphclient has already
// completed a full handshake against it.
type boltSession struct {
	conn   net.Conn
	reader *proto.ChunkedReader
	writer *proto.ChunkedWriter
}

// dialBolt connects, negotiates, and authenticates, leaving the session READY.
func dialBolt(t *testing.T, socket string) *boltSession {
	t.Helper()

	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatalf("connecting to %s: %v", socket, err)
	}

	var offer [20]byte
	binary.BigEndian.PutUint32(offer[:4], proto.Magic)
	highest := proto.SupportedVersions[0]
	offer[6] = highest.Minor
	offer[7] = highest.Major
	if _, err := conn.Write(offer[:]); err != nil {
		t.Fatalf("writing the handshake to %s: %v", socket, err)
	}
	var answer [4]byte
	if _, err := io.ReadFull(conn, answer[:]); err != nil {
		t.Fatalf("reading the handshake from %s: %v", socket, err)
	}
	if answer == [4]byte{} {
		t.Fatalf("the server at %s rejected Bolt %d.%d, the highest version it declares support for",
			socket, highest.Major, highest.Minor)
	}

	s := &boltSession{
		conn:   conn,
		reader: proto.NewChunkedReader(conn),
		writer: proto.NewChunkedWriter(conn),
	}
	t.Cleanup(s.close)

	s.requireSuccess(t, "HELLO", &proto.Hello{Extra: map[string]packstream.Value{
		"user_agent": "rmp-graphserve-durability-test/1.0",
	}})
	// Bolt 5.1 split authentication out of HELLO, and the answer above is at
	// least that: the server is the one this package builds and it negotiates the
	// engine's own highest version.
	s.requireSuccess(t, "LOGON", &proto.Logon{Auth: map[string]packstream.Value{
		"scheme": "none",
	}})
	return s
}

// requireSuccess sends one request and requires a SUCCESS, streaming past any
// records the server sends first.
//
// The failure message names the step, because what separates a drained shutdown
// from a truncated one is precisely WHICH step stopped being answered.
func (s *boltSession) requireSuccess(t *testing.T, label string, request any) {
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

	for {
		msg, err := s.reader.ReadMessage()
		if err != nil {
			t.Fatalf("reading the answer to %s: %v", label, err)
		}
		response, err := proto.DecodeResponse(packstream.NewDecoder(bytes.NewReader(msg)))
		if err != nil {
			t.Fatalf("decoding the answer to %s: %v", label, err)
		}
		switch answer := response.(type) {
		case *proto.Record:
			continue
		case *proto.Success:
			return
		case *proto.Failure:
			t.Fatalf("%s was answered %s: %s", label, answer.Code, answer.Message)
		default:
			t.Fatalf("%s was answered %T, want a success", label, response)
		}
	}
}

// close drops the connection. It is idempotent, so a deferred close beside the
// explicit one a test makes is harmless.
func (s *boltSession) close() {
	if s.conn == nil {
		return
	}
	_ = s.conn.Close() //nolint:errcheck // the exchange is over either way
	s.conn = nil
}
