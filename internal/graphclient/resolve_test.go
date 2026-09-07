// Package graphclient — the resolution half's tests.
//
// SPEC/GRAPH.md § Server Resolution defines four states and fixes how each is
// recognised, and this file drives all four against real sockets rather than
// against a stub, because three of the four are decided by the OPERATING SYSTEM's
// answer to a connection attempt and a stub would be asserting the stub.
//
// The state that matters most here is the third one, and it is the one an
// end-to-end test reaches only by accident: a socket file a killed server left
// behind. It exists on disk, it is a socket, and nothing is listening on it. Rule
// 1 says the refusal it answers with is the whole of the evidence needed to
// conclude that a roadmap is not served, that the file is neither an error nor
// the caller's to remove, and that the caller proceeds on the direct path. Every
// one of those is asserted below, including the removal that must NOT happen: a
// caller that removed a leftover socket would race a server that was binding it.
package graphclient

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/proto"
	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
)

// socketPathIn returns a short socket path inside t.TempDir.
//
// Short deliberately: a Unix domain socket path is capped at 108 bytes and a
// roadmap home under a deep temporary directory can exceed it (rmp task #367,
// FINDING #266). A harness that tripped that cap would look like a defect in the
// code under test.
func socketPathIn(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "graph.sock")
}

// leaveStaleSocket binds path and then closes the listener WITHOUT unlinking, so
// the file survives with nothing behind it — exactly the state a killed server
// leaves (measured on rmp task #367: `kill -9` exits 137 and graph.sock is still
// present).
func leaveStaleSocket(t *testing.T, path string) {
	t.Helper()

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
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("the socket file was not left behind: %v", err)
	}
}

// listenWithoutAnswering binds path and accepts connections, but writes nothing
// on them. It is a listener whose process is wedged, still opening its store, or
// not speaking the protocol at all — the case the specification gives the probe a
// handshake for, because the accept alone is the kernel's and succeeds anyway.
func listenWithoutAnswering(t *testing.T, path string) (stop func()) {
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
				// Read and discard for ever; answer nothing.
				_, _ = io.Copy(io.Discard, c)
				_ = c.Close() //nolint:errcheck // the stand-in has nothing to report
			}(conn)
		}
	}()
	return func() {
		_ = ln.Close() //nolint:errcheck // the stand-in listener is done with
		<-done
	}
}

// answerHandshake binds path and completes the Bolt handshake on every connection
// it accepts, using the engine's own server half. That is what distinguishes a
// server from a listening socket.
func answerHandshake(t *testing.T, path string) (stop func()) {
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
				defer c.Close() //nolint:errcheck // the stand-in has nothing to report
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, negErr := proto.Negotiate(ctx, c); negErr != nil {
					return
				}
				_, _ = io.Copy(io.Discard, c)
			}(conn)
		}
	}()
	return func() {
		_ = ln.Close() //nolint:errcheck // the stand-in listener is done with
		<-done
	}
}

// TestResolve_NoSocket is the first definite negative: the path does not exist,
// so no server can be listening on it and the caller takes the direct path.
func TestResolve_NoSocket(t *testing.T) {
	path := socketPathIn(t)

	state, err := Resolve(context.Background(), path)
	if err != nil {
		t.Fatalf("Resolve reported %v for an absent path; an absent socket is a definite negative "+
			"and carries no error", err)
	}
	if state != StateNoSocket {
		t.Errorf("state = %v, want %v", state, StateNoSocket)
	}
	if !state.NotServed() {
		t.Error("an absent socket must be one of the two states that put a caller on the direct path")
	}
	if state.Served() {
		t.Error("an absent socket must not be read as served")
	}
}

// TestResolve_StaleSocketIsNotServedAndIsNotRemoved is rule 1, in both of its
// halves.
//
// A server that was killed leaves its socket behind. The refusal a connection to
// it receives is the whole of the evidence needed to conclude that nothing is
// listening — and the caller does NOT remove the file, because removing one is
// the next server's business and a caller that removed one would race a server
// that was binding it.
func TestResolve_StaleSocketIsNotServedAndIsNotRemoved(t *testing.T) {
	path := socketPathIn(t)
	leaveStaleSocket(t, path)

	state, err := Resolve(context.Background(), path)
	if err != nil {
		t.Fatalf("Resolve reported %v for a leftover socket; rule 1 says a leftover file is not an "+
			"error and never fails an invocation", err)
	}
	if state != StateNotListening {
		t.Errorf("state = %v, want %v", state, StateNotListening)
	}
	if !state.NotServed() {
		t.Error("a refused connection must be one of the two states that put a caller on the direct path")
	}

	if _, statErr := os.Lstat(path); statErr != nil {
		t.Errorf("the leftover socket was removed by the probe (%v). Rule 1 forbids it: removing "+
			"one is the next server's business, and a caller that removed one would race a server "+
			"that was binding it", statErr)
	}
}

// TestResolve_ServedRequiresTheHandshake pins why the probe is a handshake and
// not a connect, by driving both sides of the distinction against listeners that
// are identical as far as the kernel is concerned.
func TestResolve_ServedRequiresTheHandshake(t *testing.T) {
	t.Run("a listener that completes the handshake is served", func(t *testing.T) {
		path := socketPathIn(t)
		stop := answerHandshake(t, path)
		defer stop()

		state, err := Resolve(context.Background(), path)
		if err != nil {
			t.Fatalf("Resolve reported %v against a listener that answers the handshake", err)
		}
		if state != StateServed {
			t.Errorf("state = %v, want %v", state, StateServed)
		}
		if !state.Served() {
			t.Error("the one state in which a statement is sent to a server must report Served")
		}
	})

	t.Run("a listener that accepts and answers nothing is unreachable", func(t *testing.T) {
		// This is the case the accept alone cannot tell apart: the connection
		// succeeds inside the kernel against a listener whose process is wedged,
		// still opening its store, or not speaking the protocol at all.
		//
		// It costs the whole probe deadline, and the deadline is not shortened
		// for it: ProbeDeadline is backoff.Total() and the project has one retry
		// policy, so a knob to shorten it here would be a second opinion about
		// that policy. 2.5 seconds spent once is the price of driving the state
		// the specification gives the handshake for.
		path := socketPathIn(t)
		stop := listenWithoutAnswering(t, path)
		defer stop()

		started := time.Now()
		state, err := Resolve(context.Background(), path)
		elapsed := time.Since(started)

		if state != StateUnreachable {
			t.Errorf("state = %v, want %v: a socket that accepts a connection is not yet evidence "+
				"of a server", state, StateUnreachable)
		}
		if err == nil {
			t.Error("an unreachable socket must carry the observation behind it; a caller has " +
				"nothing else to report")
		}
		if state.NotServed() {
			t.Error("an unreachable socket must NOT be read as a definite negative. Rule 2: " +
				"falling back on it would send the caller at a lock a server may well be holding")
		}
		if elapsed > ProbeDeadline()+2*time.Second {
			t.Errorf("the probe took %v against a %v deadline; it is bounded and is not retried",
				elapsed, ProbeDeadline())
		}
	})
}

// TestResolve_APathThatIsNotASocketIsUnreachable is the third way rule 2 is
// reached: a file the caller mistyped `--socket` onto, or a directory.
//
// It is a definite negative about the PATH and never about the roadmap, which is
// why resolution fails on it rather than concluding that no server is listening —
// the roadmap may well be served on the socket the caller meant to name.
func TestResolve_APathThatIsNotASocketIsUnreachable(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name string
		make func(t *testing.T) string
	}{
		{
			name: "a regular file",
			make: func(t *testing.T) string {
				t.Helper()
				path := filepath.Join(dir, "notes.txt")
				if err := os.WriteFile(path, []byte("not a socket"), 0600); err != nil {
					t.Fatalf("writing %s: %v", path, err)
				}
				return path
			},
		},
		{
			name: "a directory",
			make: func(t *testing.T) string {
				t.Helper()
				path := filepath.Join(dir, "subdir")
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatalf("creating %s: %v", path, err)
				}
				return path
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := c.make(t)

			state, err := Resolve(context.Background(), path)
			if state != StateUnreachable {
				t.Errorf("state = %v, want %v", state, StateUnreachable)
			}
			if !errors.Is(err, ErrNotASocket) {
				t.Errorf("error = %v, want it to carry ErrNotASocket", err)
			}
			if state.NotServed() {
				t.Error("a path that is not a socket must not put the caller on the direct path")
			}
		})
	}
}

// TestResolve_LeavesNothingBehind pins the last clause of rule 1 in the state a
// reader is least likely to consider: the probe against a LIVE server must not
// disturb the socket either.
func TestResolve_LeavesNothingBehind(t *testing.T) {
	path := socketPathIn(t)
	stop := answerHandshake(t, path)
	defer stop()

	before, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if _, err := Resolve(context.Background(), path); err != nil {
		t.Fatalf("Resolve against a live server: %v", err)
	}
	after, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("the probe removed a live server's socket: %v", err)
	}
	if before.Mode() != after.Mode() {
		t.Errorf("the probe changed the socket's mode from %v to %v", before.Mode(), after.Mode())
	}
}

// TestResolve_RespectsACallerDeadlineNearerThanTheProbeBudget pins that the whole
// probe is bounded "by ProbeDeadline, or by ctx's own deadline where that is
// nearer".
//
// The web graph data endpoint is the caller that brings one: its context is the
// request's.
func TestResolve_RespectsACallerDeadlineNearerThanTheProbeBudget(t *testing.T) {
	path := socketPathIn(t)
	stop := listenWithoutAnswering(t, path)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	started := time.Now()
	state, err := Resolve(ctx, path)
	elapsed := time.Since(started)

	if state != StateUnreachable {
		t.Errorf("state = %v, want %v", state, StateUnreachable)
	}
	if err == nil {
		t.Error("a probe cut by the caller's deadline must carry the observation behind it")
	}
	if elapsed >= ProbeDeadline() {
		t.Errorf("the probe took %v, which reached its own %v budget; a nearer caller deadline "+
			"must end it first", elapsed, ProbeDeadline())
	}
}

// TestProbeDeadline_IsTheProjectsBackoffTotal pins that the figure is READ rather
// than restated, which is what stops the project's timing numbers drifting apart.
func TestProbeDeadline_IsTheProjectsBackoffTotal(t *testing.T) {
	if got, want := ProbeDeadline(), backoff.Total(); got != want {
		t.Errorf("ProbeDeadline() = %v, want backoff.Total() = %v: SPEC/GRAPH.md § Server "+
			"Resolution gives the probe the project's backoff total and no figure of its own",
			got, want)
	}
}

// TestSocketPath_DerivesTheRoadmapPathAndValidatesTheName pins the derivation
// every surface shares, and the validation that keeps a crafted roadmap name from
// resolving a path outside ~/.roadmaps/.
func TestSocketPath_DerivesTheRoadmapPathAndValidatesTheName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := SocketPath("backend-platform")
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	want := filepath.Join(home, ".roadmaps", "backend-platform", SocketFileName)
	if got != want {
		t.Errorf("SocketPath = %q, want %q", got, want)
	}

	for _, name := range []string{"../escape", "a/b", ""} {
		if path, err := SocketPath(name); err == nil {
			t.Errorf("SocketPath(%q) = %q with no error; the roadmap name must be validated, or a "+
				"crafted one resolves a path outside ~/.roadmaps/", name, path)
		}
	}
}

// TestSocketMode_IsOwnerOnly pins the inner fence of SPEC/GRAPH.md § Socket Path
// and Permissions, rule 3.
//
// Connecting to a Unix domain socket requires WRITE permission on the file, so
// any bit outside the owner's makes the socket connectable by another account —
// and connecting to it is reaching the graph. The roadmap home's 0700 is the
// outer fence; this is the one that still holds when --socket puts the socket
// somewhere else.
func TestSocketMode_IsOwnerOnly(t *testing.T) {
	if SocketMode != 0600 {
		t.Errorf("SocketMode = %04o, want 0600", SocketMode)
	}
	if SocketMode&0077 != 0 {
		t.Errorf("SocketMode = %04o grants a bit outside the owner's; connecting to a socket needs "+
			"write permission on it, and connecting is reaching the graph", SocketMode)
	}
}

// TestVersionOffer_IsDerivedFromTheEnginesOwnList pins the property that keeps
// the handshake correct across a dependency bump: the offer is BUILT from
// proto.SupportedVersions rather than written, so a version the engine adds or
// drops moves the offer with it.
//
// A literal version in the offer is a fact a dependency bump can falsify in
// silence, which is the hazard SPEC/GRAPH.md § Dependency Maturity Risk describes.
func TestVersionOffer_IsDerivedFromTheEnginesOwnList(t *testing.T) {
	offer := versionOffer()

	if got := uint32(offer[0])<<24 | uint32(offer[1])<<16 | uint32(offer[2])<<8 | uint32(offer[3]); got != proto.Magic {
		t.Fatalf("the offer's preamble is %#08x, want the protocol magic %#08x", got, proto.Magic)
	}

	// Every major the engine supports must appear in a slot, and the highest
	// minor of each must be the slot's minor, so the server selects the highest
	// version both sides hold.
	highest := make(map[uint8]uint8)
	lowest := make(map[uint8]uint8)
	for _, v := range proto.SupportedVersions {
		if seen, ok := highest[v.Major]; !ok || v.Minor > seen {
			highest[v.Major] = v.Minor
		}
		if seen, ok := lowest[v.Major]; !ok || v.Minor < seen {
			lowest[v.Major] = v.Minor
		}
	}

	offered := make(map[uint8]struct{ minor, minorRange uint8 })
	for slot := 0; slot < 4; slot++ {
		off := 4 + slot*4
		major := offer[off+3]
		if major == 0 {
			continue
		}
		offered[major] = struct{ minor, minorRange uint8 }{minor: offer[off+2], minorRange: offer[off+1]}
	}

	for major, wantMinor := range highest {
		got, ok := offered[major]
		if !ok {
			t.Errorf("the engine supports major %d and the offer does not carry it", major)
			continue
		}
		if got.minor != wantMinor {
			t.Errorf("major %d is offered at minor %d, want the engine's highest, %d",
				major, got.minor, wantMinor)
		}
		if got.minor-got.minorRange != lowest[major] {
			t.Errorf("major %d is offered over the range [%d, %d], want it to reach the engine's "+
				"lowest, %d", major, got.minor-got.minorRange, got.minor, lowest[major])
		}
	}

	// The offer's own server half must accept it. proto.Negotiate is the
	// authority on the wire format, so answering our own offer with it is the
	// strongest check there is that the two describe one protocol.
	client, server := net.Pipe()
	defer client.Close() //nolint:errcheck // the pipe is done with
	defer server.Close() //nolint:errcheck // idem

	negotiated := make(chan proto.Version, 1)
	go func() {
		v, err := proto.Negotiate(context.Background(), server)
		if err != nil {
			negotiated <- proto.Version{}
			return
		}
		negotiated <- v
	}()

	if _, err := client.Write(offer[:]); err != nil {
		t.Fatalf("writing the offer: %v", err)
	}
	var answer [4]byte
	if _, err := io.ReadFull(client, answer[:]); err != nil {
		t.Fatalf("reading the answer: %v", err)
	}

	got := <-negotiated
	if got == (proto.Version{}) {
		t.Fatal("the engine's own Negotiate rejected the offer this package builds; the two halves " +
			"of one product no longer describe one protocol")
	}
	if want := proto.SupportedVersions[0]; got != want {
		t.Errorf("the negotiated version is %v, want the engine's highest, %v: the offer preserves "+
			"SupportedVersions' highest-first order so the server selects the highest both sides hold",
			got, want)
	}
}
