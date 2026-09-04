// Package graphserve — the unit tests for the parts of the shutdown sequence an
// end-to-end test cannot reach.
//
// # Why these tests exist here and not in the end-to-end suite
//
// An end-to-end test that starts a server and stops it passes whether or not the
// drain drains. It sees a socket appear, a statement answered, and exit code 0 —
// and every one of those is true of a server whose drain returns immediately and
// cuts the work in flight. That is not a hypothetical: the drain's FIRST
// formulation did exactly that, and it was refuted by measurement rather than by
// reading (rmp task #367, FINDING #264). The engine gives every connection a
// dedicated reader goroutine that reads the next message WHILE the message loop
// executes the previous one, so "blocked in a read" — which looks like the exact
// signal a drain wants — is true of a connection almost all of the time,
// including throughout a statement's execution. A drain keyed on it cut a
// statement that had been running for 1.5 seconds inside a shutdown that took 20
// milliseconds, and the client got a broken pipe rather than an answer.
//
// What follows therefore pins the MECHANISM rather than the outcome:
//
//   - that stopAccepting closes the underlying listener while Accept keeps
//     blocking, which is the whole of what makes a drain possible at all — the
//     engine fuses "stop accepting" with "cut the sessions" and this wrapper is
//     what puts them apart;
//   - that a connection is counted from accept to close, so a connection sitting
//     in a read is still live and quiescence is still false for it — the direct
//     encoding of the refutation above;
//   - that the quiescence decision is the conjunction it is documented to be, in
//     every combination rather than the one a happy path reaches;
//   - that the bounded wait ends promptly when the condition holds and gives up
//     rather than hanging when it never does;
//   - that the socket-file handling removes what is stale and leaves alone what
//     is not, including a path that is not a socket at all.
package graphserve

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/proto"
	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// listenerAt binds a Unix domain listener on a short path inside t.TempDir and
// returns the wrapper under test together with the path.
//
// The path is kept short deliberately. A Unix domain socket path is capped at 108
// bytes and a roadmap home under a deep temporary directory can exceed it (rmp
// task #367, FINDING #266); t.TempDir under the system temporary directory stays
// well inside the cap, and a test that failed on the cap would look like a defect
// in the code under test rather than in its own harness.
func listenerAt(t *testing.T) (*serverListener, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "graph.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("binding %s: %v", path, err)
	}
	wrapped := newServerListener(ln)
	t.Cleanup(func() { _ = wrapped.Close() }) //nolint:errcheck // the wrapper's Close reports no error by construction
	return wrapped, path
}

// TestServerListener_StopAcceptingBlocksAcceptInsteadOfReturning is the whole
// mechanism the drain rests on, asserted directly.
//
// The engine's accept loop exits when Accept returns an error, and its deferred
// exit cancels the accept context — from which every connection's context derives
// — before it waits for those connections. So if stopAccepting let Accept return,
// the engine would proceed straight to cutting the live sessions and there would
// be no interval in which a drain could happen.
//
// Both halves are asserted, because either alone would pass on a broken wrapper:
// that the socket really is closed (a new connection is refused), and that Accept
// nevertheless does NOT return until the listener is closed.
func TestServerListener_StopAcceptingBlocksAcceptInsteadOfReturning(t *testing.T) {
	ln, path := listenerAt(t)

	accepted := make(chan error, 1)
	go func() {
		_, err := ln.Accept()
		accepted <- err
	}()

	// stopAccepting is driven without waiting for the goroutine to have reached
	// Accept, because the wrapper must hold under BOTH orderings and the test
	// would otherwise assert only one of them: an Accept already blocked in the
	// underlying call is released by the close and then blocks on `released`,
	// while one that enters afterwards finds the underlying Accept failing at
	// once and blocks on the same channel. Either way it must not return.
	ln.stopAccepting()

	// Half one: the socket is really gone. A connection attempt must not be
	// accepted, or "stop accepting" would be a claim the wrapper does not keep.
	if conn, err := net.Dial("unix", path); err == nil {
		_ = conn.Close() //nolint:errcheck // already failing
		t.Fatal("a connection was accepted after stopAccepting; the underlying listener was not closed")
	}

	// Half two: Accept has NOT returned. It is the engine's presence inside its
	// accept call that keeps its cancellation unreached, so a return here is the
	// drain's window closing.
	select {
	case err := <-accepted:
		t.Fatalf("Accept returned %v after stopAccepting; it must block until the listener is "+
			"closed, or the engine leaves its accept loop and cuts every live session before the "+
			"drain has run", err)
	case <-time.After(200 * time.Millisecond):
	}

	// Closing releases it, which is what Shutdown and the engine's own close
	// goroutine each do.
	if err := ln.Close(); err != nil {
		t.Fatalf("Close reported %v; the wrapper's Close reports no error by construction", err)
	}
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("Accept did not return after Close; a blocked Accept must be released or the " +
			"shutdown never completes")
	}
}

// TestServerListener_CloseIsIdempotent pins what the engine requires of it:
// Serve closes this listener from its own close goroutine and Shutdown closes it
// again, and Groadmap has closed it once already by the time either runs.
func TestServerListener_CloseIsIdempotent(t *testing.T) {
	ln, _ := listenerAt(t)

	for i := 1; i <= 3; i++ {
		if err := ln.Close(); err != nil {
			t.Fatalf("Close call %d reported %v; every call must report none", i, err)
		}
	}
	// stopAccepting after Close must not panic on a channel closed twice either.
	ln.stopAccepting()
}

// TestServerListener_CountsAConnectionUntilItIsClosed is the refutation of
// FINDING #264 encoded as an assertion.
//
// The connection below sends NOTHING after connecting: it is a client sitting
// exactly where the engine's reader goroutine would leave it, blocked in a read.
// The first formulation of the drain read that state as idleness. Here it is
// counted as live, and quiescence is false for it, until the connection is
// actually closed.
func TestServerListener_CountsAConnectionUntilItIsClosed(t *testing.T) {
	ln, path := listenerAt(t)

	if got := ln.live.Load(); got != 0 {
		t.Fatalf("a fresh listener counts %d live connections, want 0", got)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			accepted <- nil
			return
		}
		accepted <- conn
	}()

	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dialling %s: %v", path, err)
	}
	server := <-accepted
	if server == nil {
		t.Fatal("Accept failed on a live listener")
	}

	if got := ln.live.Load(); got != 1 {
		t.Fatalf("an accepted connection is counted %d times, want 1", got)
	}
	// The client is connected and silent — blocked in a read, from the server's
	// point of view. This is the state the refuted formulation called idle.
	if quiescent(ln.live.Load(), 0) {
		t.Error("a connected, silent client makes the server look quiescent. That is the drain " +
			"formulation measurement refuted on rmp task #367: the engine reads ahead on a " +
			"goroutine per connection, so a connection is blocked in a read for almost all of a " +
			"statement's execution")
	}

	if err := server.Close(); err != nil {
		t.Fatalf("closing the accepted connection: %v", err)
	}
	if got := ln.live.Load(); got != 0 {
		t.Fatalf("a closed connection leaves the count at %d, want 0", got)
	}

	// Closing twice must not double-decrement: the engine closes a connection on
	// more than one path, and a count that went negative would make the drain
	// return at once for the wrong reason.
	if err := server.Close(); err == nil {
		t.Log("the second Close reported no error, which is fine; what matters is the count")
	}
	if got := ln.live.Load(); got != 0 {
		t.Fatalf("closing an accepted connection twice leaves the count at %d, want 0", got)
	}

	_ = client.Close() //nolint:errcheck // the client half is done with
}

// TestQuiescent_IsTheConjunctionItClaimsToBe drives the decision over every
// combination rather than the one a happy path reaches.
//
// Each of the two conditions alone is a state in which work is still in flight,
// and a drain that returned on either would cut it. The open-transaction half is
// not redundant with the connection half: a transaction the engine still lists is
// in flight whatever its connection is doing.
func TestQuiescent_IsTheConjunctionItClaimsToBe(t *testing.T) {
	cases := []struct {
		why          string
		live         int64
		transactions int
		want         bool
	}{
		{why: "nothing connected and nothing open", live: 0, transactions: 0, want: true},
		{why: "a connection is live", live: 1, transactions: 0, want: false},
		{why: "an explicit transaction is open", live: 0, transactions: 1, want: false},
		{why: "both", live: 3, transactions: 2, want: false},
	}
	for _, c := range cases {
		if got := quiescent(c.live, c.transactions); got != c.want {
			t.Errorf("quiescent(live=%d, transactions=%d) = %v, want %v (%s)",
				c.live, c.transactions, got, c.want, c.why)
		}
	}
}

// TestDrainUntil_ReturnsAtOnceWhenAlreadyQuiescent pins the ordinary case: a
// server with nothing attached must not spend the drain's bound waiting for
// nothing. The measured figure for it on a real server is 0.03 s.
func TestDrainUntil_ReturnsAtOnceWhenAlreadyQuiescent(t *testing.T) {
	calls := 0
	started := time.Now()
	drainUntil(func() bool {
		calls++
		return true
	})
	elapsed := time.Since(started)

	if calls != 1 {
		t.Errorf("the condition was asked %d times, want exactly 1: a condition that already holds "+
			"must not be re-checked after a delay", calls)
	}
	if elapsed >= backoff.FirstDelay {
		t.Errorf("the drain took %v, which is at least the policy's first delay (%v); a server with "+
			"nothing in flight must not wait at all", elapsed, backoff.FirstDelay)
	}
}

// TestDrainUntil_GivesUpAtTheWaitBudget pins the other end: the drain does NOT
// guarantee completion. Past its bound the remaining sessions are cut by the
// shutdown that follows, and a drain that waited for ever would turn a shutdown
// into a hang.
//
// The budget is shortened for the test through the same declaration production
// reads, so what is asserted is the relationship — the drain waits the wait
// budget — rather than a figure written out here.
func TestDrainUntil_GivesUpAtTheWaitBudget(t *testing.T) {
	previous := graphlock.StatementBudget
	t.Cleanup(func() { graphlock.StatementBudget = previous })
	graphlock.StatementBudget = 0

	var calls atomic.Int64
	started := time.Now()
	drainUntil(func() bool {
		calls.Add(1)
		return false
	})
	elapsed := time.Since(started)

	if calls.Load() < 2 {
		t.Errorf("the condition was asked %d time(s); a drain that never sees its condition hold "+
			"must retry rather than give up on the first look", calls.Load())
	}
	// The bound is the wait budget, which is now backoff.Total() alone. Allow the
	// scheduler its slack on the upper side and assert the lower bound exactly:
	// a drain that returned early would be the refuted formulation again.
	budget := graphlock.WaitBudget()
	if elapsed < budget {
		t.Errorf("the drain gave up after %v, before its %v bound; it must wait the whole of it "+
			"before the shutdown cuts what is left", elapsed, budget)
	}
	if elapsed > budget+2*time.Second {
		t.Errorf("the drain took %v against a %v bound; it must be bounded, not merely eventual",
			elapsed, budget)
	}
}

// TestRemoveStaleSocket_RemovesOnlyASocket pins the rule that keeps `--socket`
// safe: the path is caller-supplied, so a step that removed "any file at the
// path" would delete whatever the caller mistyped.
func TestRemoveStaleSocket_RemovesOnlyASocket(t *testing.T) {
	dir := t.TempDir()

	t.Run("a socket is removed", func(t *testing.T) {
		path := filepath.Join(dir, "stale.sock")
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatalf("binding %s: %v", path, err)
		}
		// Leave the file behind, exactly as a killed server does.
		unix, ok := ln.(*net.UnixListener)
		if !ok {
			t.Fatalf("a unix listener is %T", ln)
		}
		unix.SetUnlinkOnClose(false)
		if err := ln.Close(); err != nil {
			t.Fatalf("closing the listener: %v", err)
		}
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("the socket file was not left behind: %v", err)
		}

		removeStaleSocket(path)
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("the stale socket survived removeStaleSocket (%v); a relaunch after a kill "+
				"would then fail on a name that is already taken", err)
		}
	})

	t.Run("a regular file is left alone", func(t *testing.T) {
		path := filepath.Join(dir, "notes.txt")
		if err := os.WriteFile(path, []byte("a file the caller mistyped --socket onto"), 0600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		removeStaleSocket(path)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("a regular file at the socket path was removed (%v); --socket is a "+
				"caller-supplied path and this step must destroy nothing", err)
		}
	})

	t.Run("a directory is left alone", func(t *testing.T) {
		path := filepath.Join(dir, "subdir")
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatalf("creating %s: %v", path, err)
		}
		removeStaleSocket(path)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("a directory at the socket path was removed (%v)", err)
		}
	})

	t.Run("an absent path is not an error", func(t *testing.T) {
		removeStaleSocket(filepath.Join(dir, "never-existed.sock"))
	})
}

// TestRemoveSocketIfStale_LeavesALiveSocketAlone is step 6 of the shutdown
// sequence, and the property it must have is not the obvious one.
//
// Closing the listener already unlinked the socket, so in the ordinary case there
// is nothing to do. What this step must NOT do is remove a socket that is no
// longer this server's: the advisory lock was released one step earlier, so a new
// server may already have taken it and bound the same path, and an unconditional
// remove would unlink the incumbent's socket and leave a running server nobody
// can reach.
func TestRemoveSocketIfStale_LeavesALiveSocketAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.sock")
	stop := serveHandshakes(t, path)
	defer stop()

	removeSocketIfStale(path)

	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("a socket a LIVE server is answering on was removed (%v). The lock is released "+
			"before this step, so the socket may already belong to the next server, and removing "+
			"it would leave a running server nobody can reach", err)
	}
}

// TestRemoveSocketIfStale_RemovesALeftoverSocket is the other direction: a socket
// file nothing answers on is this server's leftover and goes.
func TestRemoveSocketIfStale_RemovesALeftoverSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.sock")

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("binding %s: %v", path, err)
	}
	unix, ok := ln.(*net.UnixListener)
	if !ok {
		t.Fatalf("a unix listener is %T", ln)
	}
	unix.SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatalf("closing the listener: %v", err)
	}

	removeSocketIfStale(path)
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("a leftover socket survived the shutdown's last step (%v)", err)
	}
}

// TestBind_SetsTheSocketModeExplicitly pins SPEC/GRAPH.md § Socket Path and
// Permissions, rule 3, at the one place it is enforced.
//
// The mode is set rather than left to the process umask because connecting to a
// Unix domain socket requires WRITE permission on the file: a permissive umask
// leaves the socket connectable by the user's group or by every account on the
// machine, and connecting to it is reaching the graph.
//
// The umask is deliberately not manipulated. syscall.Umask is Unix-only and this
// package is compiled for every target in SPEC/BUILD.md § Supported Build
// Targets, Windows included, so a test file that called it would break a platform
// the release matrix promises. The assertion is instead the direct one — the
// bound socket carries 0600 — and the control below records what the mode would
// be without the chmod, so a failure names the umask rather than leaving a reader
// to guess.
func TestBind_SetsTheSocketModeExplicitly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.sock")
	ln, err := bind(path)
	if err != nil {
		t.Fatalf("bind(%s) = %v", path, err)
	}
	t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // the wrapper's Close reports no error by construction

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != graphclient.SocketMode {
		t.Errorf("the bound socket carries mode %04o, want %04o. Under a permissive umask an "+
			"unset mode leaves the socket connectable by every account on the machine, and "+
			"connecting to it is reaching the graph", got, graphclient.SocketMode)
	}

	// The control: what a socket bound WITHOUT the chmod carries under this
	// process's umask. It is logged rather than asserted, because the umask is
	// the environment's and not this project's; what it buys is that a reader of
	// a failure above can tell an unset mode from a wrong one, and a reader of a
	// pass can see whether the umask alone would have produced it.
	control := filepath.Join(t.TempDir(), "control.sock")
	raw, err := net.Listen("unix", control)
	if err != nil {
		t.Fatalf("binding the control socket %s: %v", control, err)
	}
	t.Cleanup(func() { _ = raw.Close() }) //nolint:errcheck // the control listener is done with
	controlInfo, err := os.Stat(control)
	if err != nil {
		t.Fatalf("stat %s: %v", control, err)
	}
	if controlInfo.Mode().Perm() == graphclient.SocketMode {
		t.Logf("this process's umask alone produces mode %04o, so the assertion above would hold "+
			"even if bind set no mode. The chmod is still required: SPEC/GRAPH.md § Socket Path "+
			"and Permissions, rule 3, fixes the mode rather than inheriting it",
			controlInfo.Mode().Perm())
	}
}

// TestBind_ReportsThePublishedLineForAPathItCannotBind pins the bind failure's
// wording: the part rmp fixes is everything up to and including
// "cannot bind <socket>: ", and the text after it is the operating system's own
// (SPEC/COMMANDS.md § Graph Server Socket Error Lines).
func TestBind_ReportsThePublishedLineForAPathItCannotBind(t *testing.T) {
	// A path inside a directory that does not exist cannot be bound, and the
	// operating system says so in its own words.
	path := filepath.Join(t.TempDir(), "absent", "graph.sock")

	ln, err := bind(path)
	if err == nil {
		_ = ln.Close() //nolint:errcheck // already failing
		t.Fatal("binding a socket inside a directory that does not exist succeeded")
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("error = %v, want it to wrap utils.ErrDatabase (exit code 1)", err)
	}
	want := "cannot bind " + path + ": "
	if got := err.Error(); !containsSubstring(got, want) {
		t.Errorf("error = %q, want it to carry the published prefix %q", got, want)
	}
}

// TestLockRefusal_RewordsOnlyTheExhaustedWait pins both directions of the one
// place internal/graphserve rewords another package's error.
//
// internal/graphlock reports a busy store, which is the right wording for a
// short-lived invocation contending with another and the wrong one here: a server
// holds the lock for its whole process lifetime, so a second `rmp graph serve` is
// the overwhelmingly likely cause and the published line says so. It says "may",
// because the lock records no holder.
//
// The other direction matters as much: an acquisition that failed for a reason a
// second server cannot explain — a lock file that cannot be opened at all —
// arrives already classified and must be returned untouched, or the line would
// name a cause that is not there.
func TestLockRefusal_RewordsOnlyTheExhaustedWait(t *testing.T) {
	t.Run("an exhausted wait is reworded", func(t *testing.T) {
		busy := utils.ErrDatabase
		got := lockRefusal("backend-platform", busy)
		want := `cannot take the graph store lock for roadmap "backend-platform": ` +
			"another rmp graph serve may already be running for it"
		if !containsSubstring(got.Error(), want) {
			t.Errorf("error = %q, want it to carry %q", got.Error(), want)
		}
		if !errors.Is(got, utils.ErrDatabase) {
			t.Errorf("error = %v, want it to wrap utils.ErrDatabase", got)
		}
	})

	t.Run("anything else is returned untouched", func(t *testing.T) {
		other := utils.ErrValidation
		got := lockRefusal("backend-platform", other)
		if !errors.Is(got, utils.ErrValidation) {
			t.Errorf("error = %v, want the original error unchanged", got)
		}
		if containsSubstring(got.Error(), "another rmp graph serve") {
			t.Errorf("error = %q; a failure a second server cannot explain must not be reported "+
				"as one", got.Error())
		}
	})
}

// containsSubstring is strings.Contains, named so the assertions above read as
// what they are checking rather than as string mechanics.
func containsSubstring(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// serveHandshakes binds path and answers the Bolt handshake on every connection
// it accepts, which is exactly what makes internal/graphclient's probe report
// "served" (SPEC/GRAPH.md § Server Resolution: a socket that accepts a connection
// is not yet evidence of a server; completing the handshake is).
//
// The handshake's SERVER half is the engine's own proto.Negotiate — the same
// function a real server uses — so this stands in for a live server without
// standing in for the engine.
func serveHandshakes(t *testing.T, path string) (stop func()) {
	t.Helper()

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("binding %s: %v", path, err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close() //nolint:errcheck // the stand-in server has nothing to report
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _ = proto.Negotiate(ctx, c)
				// Hold the connection open until the probe closes it, so the
				// stand-in behaves like a server rather than like a peer that
				// hangs up mid-exchange.
				_, _ = io.Copy(io.Discard, c)
			}(conn)
		}
	}()

	return func() {
		_ = ln.Close() //nolint:errcheck // the stand-in listener is done with
		<-done
	}
}
