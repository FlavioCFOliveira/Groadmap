// Package graphserve — the concurrency acceptance test, driven against a REAL
// server rather than against a scripted one.
//
// # Why this test lives here
//
// It is rmp task #368's acceptance criterion, as rmp task #421 narrowed it. #368
// asked for a test that "drives two overlapping writers and asserts both
// statements ultimately succeed"; the contract does not promise that, and on a
// contended CI runner the original assertion failed for the one reason it was
// always going to. What the contract does promise, and what this file asserts,
// is stated on the test itself.
//
// Everything the retry does under a KNOWN conflict is pinned deterministically in
// internal/graphclient, against a server whose answers the test writes: that the
// statement is re-sent, that the conflict never reaches the caller, that an
// exhausted policy reports the engine's own diagnostic, and that nothing else is
// retried. What a scripted server cannot establish is that a conflict the REAL
// engine produces is the one the client recognises — the code is matched as a
// string, because the failure crosses the protocol as a code and the typed error
// the engine classified stays on the other side of the wire — so this test runs
// two writers against a real store and requires every one of their statements to
// land on one of the two outcomes the contract publishes.
//
// It is in this package rather than in internal/graphclient because a real server
// is assembled here, from the same bind and build the production startup sequence
// uses. It deliberately does NOT call Run: that function takes over the process's
// signal handling, which a test binary must not have done to it.
package graphserve

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/server"
	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
)

// startRealServer assembles a server over a fresh store on a short socket path,
// exactly as the startup sequence does, and returns the socket together with the
// teardown.
//
// The teardown mirrors the order SPEC/GRAPH.md § Server Shutdown and the Drain
// fixes — stop accepting, drain, shut down, release the hold — so a test that
// finished with work in flight tears down the way production does rather than by
// abandoning goroutines.
//
// It takes a testing.TB rather than a *testing.T because the benchmarks of rmp
// task #370 drive the same real server this test does. Measuring a re-assembly of
// the startup sequence would measure the harness; measuring THIS one measures
// what `rmp graph serve` runs.
//
// The cadence is a parameter for the reason [checkpointCadence] gives. A test
// passes [productionCadence] so it runs what production runs; a benchmark that is
// measuring the write-ahead log's growth passes a disabled cadence so no fold can
// truncate the log underneath the measurement.
func startRealServer(tb testing.TB, cadence checkpointCadence) (socket string, stop func()) {
	tb.Helper()
	socket, _, stop = startRealServerAt(tb, cadence)
	return socket, stop
}

// startRealServerAt is [startRealServer] and also reports the graph directory it
// served.
//
// The directory is what a benchmark measuring the write-ahead log needs — the log
// is graphDir/wal and its growth per write is the quantity the checkpoint cadence
// decision is made from — and it is deliberately NOT folded into
// startRealServer's own return: exactly one caller wants it, and every other
// would carry a second return value it discards.
func startRealServerAt(tb testing.TB, cadence checkpointCadence) (socket, graphDir string, stop func()) {
	tb.Helper()
	return startRealServerLogging(tb, cadence, logger)
}

// startRealServerLogging is [startRealServerAt] with the server's diagnostic
// logger supplied by the caller.
//
// The logger is INJECTED rather than swapped into the package variable, for the
// reason [syncBuffer] states: the variable is global mutable state and a test
// that reassigned it would be testing its own bookkeeping as much as the server.
// build already takes the logger for exactly this reason, and this is the seam
// that reaches it — which is what lets rmp task #389's test give a real server a
// destination that has stopped being read.
func startRealServerLogging(tb testing.TB, cadence checkpointCadence, log *slog.Logger) (socket, graphDir string, stop func()) {
	tb.Helper()

	root := tb.TempDir()
	graphDir = filepath.Join(root, "graph")
	if err := os.MkdirAll(graphDir, 0700); err != nil {
		tb.Fatalf("creating %s: %v", graphDir, err)
	}

	hold, err := graphstore.Acquire(graphDir)
	if err != nil {
		tb.Fatalf("taking the graph store lock: %v", err)
	}
	st, err := hold.Open()
	if err != nil {
		tb.Fatalf("opening the graph store: %v", err)
	}

	closer, srv, err := build(st, graphDir, cadence, log)
	if err != nil {
		tb.Fatalf("building the server: %v", err)
	}

	socket = filepath.Join(root, "graph.sock")
	ln, err := bind(socket)
	if err != nil {
		tb.Fatalf("binding %s: %v", socket, err)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(context.Background(), ln) }()

	// Wait for the socket to answer before returning, so a caller's first
	// statement is not racing the accept loop.
	waitUntilServed(tb, socket)

	return socket, graphDir, func() {
		teardown(tb, srv, ln, st, closer, serveErr)
	}
}

// waitUntilServed blocks until the socket answers the resolution probe, using the
// one resolver every surface uses rather than a sleep somebody chose.
func waitUntilServed(tb testing.TB, socket string) {
	tb.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for {
		state, _ := graphclient.Resolve(context.Background(), socket)
		if state.Served() {
			return
		}
		if time.Now().After(deadline) {
			tb.Fatalf("the server at %s did not answer within 15s (state %v)", socket, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// teardown runs the shutdown sequence's steps in the order production runs them.
func teardown(tb testing.TB, srv *server.Server, ln *serverListener, st *graphstore.Store,
	closer *shutdownCloser, serveErr <-chan error) {
	tb.Helper()

	ln.stopAccepting()
	ln.markWrites()
	drain(srv, ln)
	ln.cutBlockedWrites()
	_ = srv.Shutdown(context.Background()) //nolint:errcheck // the test's assertions are about the statements, not the teardown
	_ = ln.Close()                         //nolint:errcheck // the wrapper's Close reports no error by construction
	<-serveErr
	_ = closer.Close() //nolint:errcheck // the server closed it already; this is belt and braces
	_ = st.Close()     //nolint:errcheck // releases the advisory hold whatever the log reports
}

// TestServer_TwoOverlappingWritersEitherCommitOrReportTheConflict is the
// acceptance criterion, and its name is the whole of what it guarantees: every
// statement two overlapping writers send either COMMITS, or is refused as the
// serialisation conflict the contract publishes a line for. Nothing else is
// permitted, and the node's end state must match, exactly, what the commits
// claimed.
//
// # What it guarantees, clause by clause
//
//  1. No statement is lost. Every statement resolves to one of exactly two
//     outcomes: a commit, or a *graphclient.SendError of kind FailureConflict. A
//     parse failure, a statement failure, a connection lost mid-statement, a
//     server that did not answer, and a value that could not be mapped all fail
//     the test, because none of them is something concurrent writing may produce.
//  2. A commit committed. Each statement writes its round number and reads it
//     back in the same breath, and the value it returns must be that round.
//  3. A refusal wrote nothing, and no commit was undone. When both writers have
//     finished, each property holds EXACTLY the last round its writer reported
//     committing, and holds null when it reported none. A round refused as a
//     conflict that had in fact written would leave a higher number there; a
//     commit the retry only claimed would leave a lower one.
//
// # Why "both succeed" is not among them
//
// It was, and it asserted more than the contract offers. MVCC is the store's sole
// concurrency control at the pinned engine: writers overlap, and a write-write
// collision is DETECTED rather than prevented, with the loser's transaction
// failing as Neo.TransientError.Transaction.Outdated (SPEC/GRAPH.md § Concurrency
// Inside the Server, rule 3). The client retries that inside a 2.5s budget and
// the caller never sees it (rule 4) — but a statement that loses on EVERY attempt
// within the budget is a published outcome, with a line of its own in
// SPEC/COMMANDS.md § Graph Management and a measured rate:
// SPEC/IMPLEMENTATION.md § Retry Logic, canonical for the measurements of the
// policy's shapes, records the full-jitter shape this client walks exhausted on
// 0.07-0.22% of statements from sixty-four writers updating a single node.
//
// This test converges on one node the same way and does it with TWO, where the
// rate is far lower — measured for #421, 820 runs of this test on CPUs saturated
// by busy loops (GOMAXPROCS 1 and 2, -race, with and without -cover, one set of
// runs with synchronous disk writes competing) exhausted the budget on none of
// their 49,200 statements — but it is not zero, and the difference between
// "rare" and "never" is the whole defect. Requiring all sixty statements to win
// was requiring the engine to do better than it promises, and the CI runner that
// failed this test refused writer alpha's round 7 as a conflict lost on every
// attempt (rmp task #421).
//
// # Why this is not "anything goes"
//
// FailureConflict is minted on exactly one engine code, so an error that arrives
// under it has established the thing a scripted server cannot: that the code the
// REAL engine reports is the code the client matches. A conflict reported under
// any other code would arrive under another kind and fail clause 1. Nor is the
// collision hypothetical: measured for #421, each of 160 instrumented runs of
// this test had its retry absorb at least six conflicts — 467 across 1,800
// statements on an idle machine, 2,088 across 6,000 on saturated CPUs — so a
// client that stopped recognising the engine's code fails this test on every
// run, not on an unlucky one. What load changes is not whether the two writers
// collide but whether one statement can lose every attempt inside the budget.
func TestServer_TwoOverlappingWritersEitherCommitOrReportTheConflict(t *testing.T) {
	socket, stop := startRealServer(t, productionCadence())
	defer stop()

	if _, err := graphclient.Send(context.Background(), socket,
		"CREATE (c:Counter {key:'contended'})"); err != nil {
		t.Fatalf("seeding the contended node: %v", err)
	}

	// Each round is one statement per writer against the same node. Enough
	// rounds that an overlap is near-certain, few enough that the test stays
	// quick: the statements are small and the store is empty apart from one node.
	const rounds = 30

	alpha := make([]roundOutcome, rounds)
	beta := make([]roundOutcome, rounds)

	// Neither writer stops at its first refusal. A refused statement wrote
	// nothing and the next round is a fresh one, so stopping would hide every
	// later round's outcome behind the first that lost — and would make the end
	// state depend on where the writer happened to give up.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); runWriter(socket, "alpha", alpha) }()
	go func() { defer wg.Done(); runWriter(socket, "beta", beta) }()
	wg.Wait()

	alphaRounds := checkRounds(t, "alpha", alpha)
	betaRounds := checkRounds(t, "beta", beta)
	t.Logf("of %d rounds each: alpha committed %d and lost %d to the retry budget; "+
		"beta committed %d and lost %d", rounds,
		alphaRounds.committed, alphaRounds.refused, betaRounds.committed, betaRounds.refused)

	// The end state, which is where a refusal's claim that nothing was written is
	// checked against the graph rather than taken on trust. A writer runs its
	// rounds in order and each commit overwrites the one before it, so the value
	// left behind is determined: the last round that writer reported committing.
	result, err := graphclient.Send(context.Background(), socket,
		"MATCH (c:Counter {key:'contended'}) RETURN c.alpha, c.beta")
	if err != nil {
		t.Fatalf("reading the contended node back: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("the contended node came back in %d rows, want 1", len(result.Rows))
	}
	checkProperty(t, result, "c.alpha", alphaRounds.lastCommitted)
	checkProperty(t, result, "c.beta", betaRounds.lastCommitted)
}

// roundOutcome is what one round of one writer produced: the error its statement
// failed with, or the value the statement read back after writing it. A failed
// round sets err; a round that committed leaves err nil and sets value to what
// came back, which is nil when the result did not have the shape the statement
// asked for — a case checkRounds reports rather than skips.
type roundOutcome struct {
	err   error
	value expr.Value
}

// runWriter sets property to the round number once per round, reading the value
// back in the same statement, and records every round's outcome into the slot
// that round owns.
//
// It writes into a pre-sized slice rather than appending, so the two writers need
// no lock between them and a round's index is its round number less one.
func runWriter(socket, property string, into []roundOutcome) {
	for round := 1; round <= len(into); round++ {
		statement := "MATCH (c:Counter {key:'contended'}) SET c." + property + " = " +
			itoa(round) + " RETURN c." + property
		result, err := graphclient.Send(context.Background(), socket, statement)
		if err != nil {
			into[round-1] = roundOutcome{err: err}
			continue
		}
		// A shape other than the one row of one column the statement asks for
		// leaves the value nil, which checkRounds reports as the commit not
		// having returned what it wrote.
		var value expr.Value
		if len(result.Rows) == 1 && len(result.Rows[0]) == 1 {
			value = result.Rows[0][0]
		}
		into[round-1] = roundOutcome{value: value}
	}
}

// writerRounds is the verdict on one writer's rounds.
type writerRounds struct {
	// lastCommitted is the highest round that committed, and zero when none did.
	// It is what the node must hold at the end, because a writer runs its rounds
	// in order and every commit overwrites the one before it.
	lastCommitted int
	committed     int
	refused       int
}

// checkRounds holds one writer's rounds to the two outcomes the contract permits,
// and reports what they were.
func checkRounds(t *testing.T, name string, outcomes []roundOutcome) writerRounds {
	t.Helper()

	var rounds writerRounds
	for i, outcome := range outcomes {
		round := i + 1
		switch {
		case outcome.err == nil:
			rounds.lastCommitted = round
			rounds.committed++
			if got, ok := outcome.value.(expr.IntegerValue); !ok || int(got) != round {
				t.Errorf("writer %s round %d reported a commit and read back %v, want the "+
					"integer %d: a statement that reports success must have applied the "+
					"value it wrote", name, round, outcome.value, round)
			}
		case isWriteConflict(outcome.err):
			rounds.refused++
		default:
			t.Errorf("writer %s round %d failed with %v. Two writers against one node produce "+
				"exactly two outcomes — a commit, or a serialisation conflict lost on every "+
				"attempt of the retry policy — and this is neither (SPEC/GRAPH.md § Concurrency "+
				"Inside the Server, rules 3, 4 and 9)", name, round, outcome.err)
		}
	}
	return rounds
}

// isWriteConflict reports whether err is the one failure concurrency may
// legitimately produce here: a serialisation conflict that lost on every attempt
// of the retry policy, which SPEC/COMMANDS.md § Graph Management publishes a line
// of its own for.
//
// The kind carries the whole check. internal/graphclient mints FailureConflict on
// exactly one engine code, so an error arriving under it has established that the
// code the real engine reported is the code the client matches — and a conflict
// under any other code would arrive under another kind, which checkRounds
// rejects. Re-stating the code here would put a second copy of it in the tree
// without adding a thing the kind does not already say.
func isWriteConflict(err error) bool {
	var sendErr *graphclient.SendError
	return errors.As(err, &sendErr) && sendErr.Kind == graphclient.FailureConflict
}

// checkProperty asserts that column holds the integer want, or null when want is
// zero, which is how "that writer committed no round at all" is spelled.
func checkProperty(t *testing.T, result *graphclient.Result, column string, want int) {
	t.Helper()

	index := -1
	for i, name := range result.Columns {
		if name == column {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatalf("the read-back returned columns %v, which do not include %s",
			result.Columns, column)
	}

	got := result.Rows[0][index]
	if want == 0 {
		if !expr.IsNull(got) {
			t.Errorf("%s is %v, but every one of that writer's rounds was refused as a write "+
				"conflict, and a refused statement writes nothing (SPEC/GRAPH.md § Concurrency "+
				"Inside the Server, rule 5)", column, got)
		}
		return
	}
	if n, ok := got.(expr.IntegerValue); !ok || int(n) != want {
		t.Errorf("%s is %v, want the integer %d — the last round that writer reported "+
			"committing. A lower value means a commit was reported over a statement that never "+
			"ran; a higher one means a round refused as a write conflict wrote anyway "+
			"(SPEC/GRAPH.md § Concurrency Inside the Server, rule 5)", column, got, want)
	}
}

// itoa renders a small non-negative integer without pulling strconv into a file
// that needs nothing else from it.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
