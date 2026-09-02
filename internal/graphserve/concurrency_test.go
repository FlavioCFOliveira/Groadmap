// Package graphserve — the concurrency acceptance test, driven against a REAL
// server rather than against a scripted one.
//
// # Why this test lives here
//
// It is rmp task #368's acceptance criterion: "A Neo.TransientError.Transaction.Outdated
// failure is retried, not surfaced as an error. A test drives two overlapping
// writers and asserts both statements ultimately succeed."
//
// Everything the retry does under a KNOWN conflict is pinned deterministically in
// internal/graphclient, against a server whose answers the test writes: that the
// statement is re-sent, that the conflict never reaches the caller, that an
// exhausted policy reports the engine's own diagnostic, and that nothing else is
// retried. What a scripted server cannot establish is that a conflict the REAL
// engine produces is the one the client recognises — the code is matched as a
// string, because the failure crosses the protocol as a code and the typed error
// the engine classified stays on the other side of the wire — so this test runs
// two writers against a real store and requires both to succeed.
//
// It is in this package rather than in internal/graphclient because a real server
// is assembled here, from the same bind and build the production startup sequence
// uses. It deliberately does NOT call Run: that function takes over the process's
// signal handling, which a test binary must not have done to it.
//
// # Why the end state is asserted and not only the two exit codes
//
// "Both succeeded" alone would pass on a run in which the two writers never
// overlapped. The two writers therefore set DIFFERENT properties on the SAME
// node, and both properties must be present at the end. Under first-updater-wins
// the loser's transaction commits nothing, so a client that did not retry would
// either fail — which the success assertion catches — or leave one property
// missing, which this catches.
package graphserve

import (
	"context"
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

	closer, srv, err := build(st, graphDir, cadence)
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
	drain(srv, ln)
	_ = srv.Shutdown(context.Background()) //nolint:errcheck // the test's assertions are about the statements, not the teardown
	_ = ln.Close()                         //nolint:errcheck // the wrapper's Close reports no error by construction
	<-serveErr
	_ = closer.Close() //nolint:errcheck // the server closed it already; this is belt and braces
	_ = st.Close()     //nolint:errcheck // releases the advisory hold whatever the log reports
}

// TestServer_TwoOverlappingWritersBothSucceed is the acceptance criterion.
//
// MVCC is the store's sole concurrency control at the pinned engine: writers
// overlap, and a write-write collision is DETECTED rather than prevented, with
// the loser's transaction failing as Neo.TransientError.Transaction.Outdated
// (SPEC/GRAPH.md § Concurrency Inside the Server, rule 3). That is a normal
// outcome of concurrent writes and not a fault, so the client retries it and the
// caller never sees it (rule 4).
//
// Two writers hammer the SAME node with many statements each, so the collision is
// as likely as the test can make it, and every statement must ultimately succeed.
func TestServer_TwoOverlappingWritersBothSucceed(t *testing.T) {
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

	type failure struct {
		writer string
		round  int
		err    error
	}
	var (
		mu       sync.Mutex
		failures []failure
	)

	writer := func(name, property string) {
		for round := 1; round <= rounds; round++ {
			statement := "MATCH (c:Counter {key:'contended'}) SET c." + property + " = " +
				itoa(round) + " RETURN c." + property
			if _, err := graphclient.Send(context.Background(), socket, statement); err != nil {
				mu.Lock()
				failures = append(failures, failure{writer: name, round: round, err: err})
				mu.Unlock()
				return
			}
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); writer("alpha", "alpha") }()
	go func() { defer wg.Done(); writer("beta", "beta") }()
	wg.Wait()

	for _, f := range failures {
		t.Errorf("writer %s failed at round %d with %v. A serialisation conflict is a NORMAL "+
			"outcome of concurrent writes inside a server — MVCC detects the collision rather than "+
			"preventing it — and the client must retry it rather than surface it "+
			"(SPEC/GRAPH.md § Concurrency Inside the Server, rule 4)", f.writer, f.round, f.err)
	}

	// The end state. "Both succeeded" alone would pass on a run in which the two
	// writers never overlapped; requiring BOTH properties to be present is what
	// catches a retry that reported success without re-running the statement,
	// because under first-updater-wins the loser's transaction commits nothing.
	result, err := graphclient.Send(context.Background(), socket,
		"MATCH (c:Counter {key:'contended'}) RETURN c.alpha, c.beta")
	if err != nil {
		t.Fatalf("reading the contended node back: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("the contended node came back in %d rows, want 1", len(result.Rows))
	}
	for i, column := range result.Columns {
		if result.Rows[0][i] == expr.Null {
			t.Errorf("%s is null after %d rounds of writes; one writer's work never landed",
				column, rounds)
		}
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
