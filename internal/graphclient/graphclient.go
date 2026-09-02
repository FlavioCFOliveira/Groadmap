// Package graphclient owns reaching a roadmap's graph server: deciding whether
// one is listening, and speaking Bolt version 5 to it when one is.
//
// # What is here now, and what is not
//
// SPEC/ARCHITECTURE.md module 9 gives this package two responsibilities —
// RESOLUTION (SPEC/GRAPH.md § Server Resolution) and the CLIENT
// (SPEC/GRAPH.md § The Bolt Client) — and states that there is exactly one
// realisation of each. Only the first is here today. The server (rmp task #367)
// cannot start without it: SPEC/GRAPH.md § Server Startup, step 3, requires
// `rmp graph serve` to refuse to start when a live server already answers on the
// socket it resolved, and to probe "exactly as a resolver does". Writing that
// probe inside the server would have been a SECOND resolution rule, which is the
// arrangement module 9 exists to prevent, so the probe was written here and the
// server calls it.
//
// The statement-sending half — the RUN/PULL exchange, the retry over a
// serialisation conflict, and the mapping of protocol values onto
// SPEC/DATA_FORMATS.md § Graph Client Result — belongs to rmp task #368, together
// with the `rmp graph client` subcommand and the `--socket` flag on
// `rmp graph execute`. Nothing here anticipates it beyond leaving room for it.
//
// # Why the probe is a handshake and not a connect
//
// SPEC/GRAPH.md § Server Resolution defines the four states a socket can be in
// and fixes how each is recognised. Three of them are decided by the connection
// attempt alone; the fourth is not. A socket that accepts a connection is not yet
// evidence of a server: the accept is the kernel's, and it succeeds against a
// listener whose process is wedged, still opening its store, or not speaking Bolt
// at all. Completing the protocol handshake is what distinguishes a server from a
// listening socket, which is why the specification puts the handshake inside the
// probe and gives the whole probe a deadline.
//
// # Why the probe is bounded, and by that value
//
// The deadline is the project's backoff total, 2500 ms, reused here rather than
// replaced by a figure of this package's own so the project keeps one set of
// timing numbers. It is the allowance for a cost that is local and
// scheduling-bound rather than an I/O one: connecting to a socket in the local
// filesystem either succeeds or fails inside the kernel, and the handshake is one
// exchange with a process on the same machine. The probe is not retried, and it
// is spent before any lock is taken, so it consumes neither the statement budget
// nor the wait budget.
//
// # Why the offer is derived from the engine's own version list
//
// The handshake's client half is written here because the pinned engine exports
// only the server half (proto.Negotiate reads a client's offer and answers it).
// What is NOT written here is which versions to offer: the offer is built from
// proto.SupportedVersions, so a server that stops supporting a version this
// package would otherwise have hard-coded is still reached. A literal version in
// this file is a fact a dependency bump can falsify in silence, which is the
// hazard SPEC/GRAPH.md § Dependency Maturity Risk describes.
package graphclient

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/proto"
	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// SocketFileName is the basename of a roadmap's graph server socket inside that
// roadmap's home directory (SPEC/GRAPH.md § Socket Path and Permissions, rule 1).
//
// It sits beside project.db and the graph/ store directory, and NOT inside
// graph/: the contents of that directory belong to GoGraph, and write.lock is the
// single entry in it Groadmap owns (SPEC/GRAPH.md § Persistence Layout, rule 5).
const SocketFileName = "graph.sock"

// SocketMode is the mode a graph server's socket carries, set explicitly rather
// than left to the process umask (SPEC/GRAPH.md § Socket Path and Permissions,
// rule 3).
//
// Connecting to a Unix domain socket requires WRITE permission on the socket
// file, so a permissive umask leaves the socket connectable by the user's group
// or by every account on the machine — and connecting to it is reaching the
// graph. The roadmap home directory is 0700 and is the outer fence; this is the
// inner one, and it is the one that still holds when --socket puts the socket
// somewhere else.
const SocketMode = 0600

// ProbeDeadline bounds the whole resolution probe: the connection and the
// handshake together (SPEC/GRAPH.md § Server Resolution).
//
// It is backoff.Total(), the project's single retry policy's worst-case total,
// read rather than restated so the two cannot drift apart. It is a function for
// the same reason internal/graphlock.WaitBudget is: a test that moves the policy
// moves this with it.
func ProbeDeadline() time.Duration { return backoff.Total() }

// State is the outcome of resolving a roadmap's socket: which of the four states
// of SPEC/GRAPH.md § Server Resolution the socket in force is in.
//
// The two negatives are kept apart although every surface treats them alike,
// because the specification recognises them differently — an absent path and a
// refused connection are different observations — and a caller that must report
// which one it saw can. What a caller must NOT do is treat any other outcome as a
// negative: only these two mean "not served" (rule 2).
type State uint8

const (
	// StateNoSocket is "not served: no socket". The socket path does not exist.
	StateNoSocket State = iota + 1
	// StateNotListening is "not served: nothing listening". The connection was
	// refused, which is what a socket file left behind by a killed server
	// answers. The leftover file is neither an error nor the caller's to remove
	// (SPEC/GRAPH.md § Server Resolution, rule 1).
	StateNotListening
	// StateServed is "served". The connection was accepted and the handshake
	// completed inside the probe deadline.
	StateServed
	// StateUnreachable is "unreachable". The connection was accepted but the
	// handshake did not complete inside the probe deadline, or the attempt
	// failed for any reason other than the two negatives above: a permission the
	// caller does not have, a path that is not a socket, a peer that does not
	// speak the protocol. It is a resolution FAILURE and never a fall back
	// (SPEC/GRAPH.md § Server Resolution, rule 2).
	StateUnreachable
)

// Served reports whether s is the one state in which a statement is sent to a
// server rather than run against the store.
func (s State) Served() bool { return s == StateServed }

// NotServed reports whether s is one of the two definite negatives, which are
// the only outcomes that put a caller on the direct path.
func (s State) NotServed() bool { return s == StateNoSocket || s == StateNotListening }

// String renders a State for a diagnostic.
func (s State) String() string {
	switch s {
	case StateNoSocket:
		return "no socket"
	case StateNotListening:
		return "nothing listening"
	case StateServed:
		return "served"
	case StateUnreachable:
		return "unreachable"
	default:
		return "unknown"
	}
}

// ErrNotASocket is the observation behind a StateUnreachable on a path that
// exists and is not a socket — a file the caller mistyped --socket onto, or a
// directory. It is a definite negative about the PATH and never about the
// roadmap, which is why resolution fails on it rather than concluding that no
// server is listening.
var ErrNotASocket = errors.New("the path is not a socket")

// SocketPath returns the socket path derived from a roadmap name:
// ~/.roadmaps/<name>/graph.sock.
//
// Every surface that resolves a socket derives it this way and through this
// function, so a caller and a server that name the same roadmap name the same
// socket without either being told a path (SPEC/GRAPH.md § Socket Path and
// Permissions, rule 1). The roadmap name is validated by utils.GetRoadmapDir,
// which is what keeps a crafted name from resolving a path outside ~/.roadmaps/.
func SocketPath(roadmapName string) (string, error) {
	dir, err := utils.GetRoadmapDir(roadmapName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, SocketFileName), nil
}

// Resolve probes socketPath and reports which of the four states it is in.
//
// The whole probe — the connection and the handshake — is bounded by
// ProbeDeadline, or by ctx's own deadline where that is nearer. It is not
// retried. The returned error is the observation behind a StateUnreachable and is
// nil for every other state; it carries no classification, because what a
// resolution outcome MEANS to a caller differs by surface (an exit code in
// internal/commands, an HTTP status in internal/web, a refusal to start in
// internal/graphserve) and this package decides none of them.
//
// The probe leaves nothing behind. It removes no file: a socket a killed server
// left is the next server's business, and a caller that removed one would race a
// server that was binding it (SPEC/GRAPH.md § Server Resolution, rule 1).
func Resolve(ctx context.Context, socketPath string) (State, error) {
	ctx, cancel := context.WithTimeout(ctx, ProbeDeadline())
	defer cancel()

	// The stat comes first because it is the one observation that is the same on
	// every platform this binary is built for. Classifying a dial error by errno
	// is not: a refused connection to a Unix domain socket reports ECONNREFUSED
	// on Unix and a WSA-numbered error on Windows, and only the ABSENCE of the
	// path is reported identically. Reading the absence here leaves the dial to
	// distinguish the remaining two.
	info, err := os.Lstat(socketPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return StateNoSocket, nil
	case err != nil:
		// A path whose existence cannot even be determined — a directory the
		// caller may not traverse — is not a negative. Falling back on it would
		// send the caller at a lock a server may well be holding.
		return StateUnreachable, err
	case info.Mode()&os.ModeSocket == 0:
		return StateUnreachable, fmt.Errorf("%s: %w", socketPath, ErrNotASocket)
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		if isConnectionRefused(err) {
			return StateNotListening, nil
		}
		return StateUnreachable, err
	}
	defer conn.Close() //nolint:errcheck // the probe reads nothing it has not already got; a close error cannot change the verdict

	if err := handshake(ctx, conn); err != nil {
		return StateUnreachable, err
	}
	return StateServed, nil
}

// isConnectionRefused reports whether err is the refusal a socket file with no
// listener behind it answers with.
//
// A refusal is the ONE dial failure that is a definite negative, so it is
// recognised narrowly and everything else falls through to StateUnreachable —
// the direction in which a misclassification is safe. syscall.ECONNREFUSED is
// declared on every platform Go builds for, so this compiles everywhere; on a
// platform whose refusal arrives under a different number the outcome is
// StateUnreachable, which fails the invocation rather than sending it at a lock a
// server may be holding.
func isConnectionRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

// handshake performs the CLIENT half of the Bolt handshake on conn and reports
// whether a server answered it with a version both sides speak.
//
// The pinned engine exports proto.Negotiate, which is the SERVER half: it reads
// the 20-byte offer and writes the 4-byte answer. This is its counterpart, built
// on the same package's exported constants so the two describe one protocol.
//
// The wire format is the one proto.Negotiate documents: 4 bytes of magic followed
// by four 4-byte version slots, each laid out big-endian as
// [0x00, minor_range, minor, major], where minor_range > 0 offers the whole range
// [minor-minor_range, minor]. The answer is 4 bytes, [0x00, 0x00, minor, major],
// and an answer of four zero bytes means the server shares no version with the
// offer.
func handshake(ctx context.Context, conn net.Conn) error {
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("setting the probe deadline: %w", err)
		}
	}

	offer := versionOffer()
	if _, err := conn.Write(offer[:]); err != nil {
		return fmt.Errorf("writing the handshake: %w", err)
	}

	var answer [4]byte
	if _, err := io.ReadFull(conn, answer[:]); err != nil {
		return fmt.Errorf("reading the handshake: %w", err)
	}
	if answer == [4]byte{} {
		// A server answered, and rejected every version offered. That is an
		// exchange that completed and a handshake that did not, so it is not
		// StateServed: a caller sent to this socket could not run a statement on
		// it. It is reported rather than swallowed, because the cause is a
		// version skew between two halves of one product and the reader can act
		// on nothing else.
		return proto.ErrNoCommonVersion
	}
	return nil
}

// versionOffer builds the 20-byte handshake offer from proto.SupportedVersions,
// grouping the engine's versions by major number into at most the four slots the
// protocol allows, highest first.
//
// Deriving the offer rather than writing one is what keeps this function correct
// across a dependency bump: the versions are the engine's own, and a version it
// adds or drops moves the offer with it. proto.SupportedVersions is ordered
// highest-first and this preserves that order, so the server selects the highest
// version both sides hold.
func versionOffer() [20]byte {
	var buf [20]byte
	binary.BigEndian.PutUint32(buf[:4], proto.Magic)

	slot := 0
	for i := 0; i < len(proto.SupportedVersions) && slot < 4; i++ {
		v := proto.SupportedVersions[i]
		if slot > 0 && buf[4+(slot-1)*4+3] == v.Major {
			// Same major as the slot already written, and SupportedVersions is
			// ordered highest-first, so this version extends that slot's range
			// downwards instead of taking a slot of its own.
			prev := 4 + (slot-1)*4
			buf[prev+1] = buf[prev+2] - v.Minor
			continue
		}
		off := 4 + slot*4
		buf[off+0] = 0
		buf[off+1] = 0
		buf[off+2] = v.Minor
		buf[off+3] = v.Major
		slot++
	}
	return buf
}
