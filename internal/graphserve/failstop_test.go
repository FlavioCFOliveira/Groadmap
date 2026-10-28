// Package graphserve — the regression fences for a fail-stopped engine (rmp task
// #635) and for the engine's close at shutdown (rmp task #636).
//
// Both need the engine in hand: the fail-stop is injected through the engine's
// own index manager, and the engine's background goroutine is observed through
// its graph. A child process cannot be reached that way, so these tests assemble
// the server in-process from the same steps Run performs — lock, bind, open,
// build — and run the production serve over it. Only the signal channel differs:
// it is the test's own, because a test binary must not have the process's signal
// handling taken over.
package graphserve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// publishedFailStopLine is the line SPEC/GRAPH.md § Error Handling and Exit
// Codes, rule 12, publishes, written out rather than derived from the code it
// checks.
const publishedFailStopLine = "Error: graph server error: the graph engine fail-stopped and refuses every " +
	"statement; the server has shut down. Restart rmp graph serve to rebuild the indexes from the store."

// inProcessServer is a server assembled in this process and served by the
// production serve.
type inProcessServer struct {
	st       *graphstore.Store
	closer   *shutdownCloser
	sigCh    chan os.Signal
	done     chan error
	graphDir string
	socket   string
}

// serveInProcess performs Run's startup steps 2, 5 and 6 over a fresh store and
// starts the production serve, with log as the server's logger and no in-flight
// cadence, so the only fold a test can observe is the shutdown's.
func serveInProcess(t *testing.T, log *slog.Logger) *inProcessServer {
	t.Helper()

	root := graphRoot(t)
	s := &inProcessServer{
		graphDir: filepath.Join(root, "graph"),
		socket:   filepath.Join(root, "graph.sock"),
		sigCh:    make(chan os.Signal, 1),
		done:     make(chan error, 1),
	}
	hold, err := graphstore.Acquire(s.graphDir)
	if err != nil {
		t.Fatalf("taking the graph store lock: %v", err)
	}
	ln, err := bind(s.socket)
	if err != nil {
		hold.Release()
		t.Fatalf("binding %s: %v", s.socket, err)
	}
	s.st, err = hold.Open()
	if err != nil {
		_ = ln.Close() //nolint:errcheck // already failing
		t.Fatalf("opening the graph store: %v", err)
	}
	closer, srv, err := build(s.st, s.graphDir, checkpointCadence{}, log, nil)
	if err != nil {
		t.Fatalf("building the server: %v", err)
	}
	s.closer = closer
	go func() { s.done <- serve(srv, ln, s.st, s.socket, s.sigCh, closer.failStop.stopped()) }()
	waitUntilServed(t, s.socket)
	return s
}

// wait returns what serve returned, failing the test if it has not returned
// within the shutdown deadline.
func (s *inProcessServer) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-s.done:
		return err
	case <-time.After(shutdownDeadline):
		t.Fatalf("the server did not shut down within %s", shutdownDeadline)
		return nil
	}
}

// requireReleased asserts what every graceful stop leaves: the socket gone and
// the store's lock free for the next server.
func (s *inProcessServer) requireReleased(t *testing.T) {
	t.Helper()

	if _, err := os.Lstat(s.socket); !os.IsNotExist(err) {
		t.Errorf("the stopped server left %s behind (lstat: %v)", s.socket, err)
	}
	hold, err := graphstore.Acquire(s.graphDir)
	if err != nil {
		t.Fatalf("the stopped server still holds the store lock: %v", err)
	}
	hold.Release()
}

// TestServe_AFailStoppedEngineShutsTheServerDownWithoutAFold is the regression
// fence for rmp task #635. Once a statement is refused with the engine's
// fail-stop error, the server shuts down through its ordinary sequence without
// the shutdown checkpoint, ends with the published line under
// utils.ErrGraphServer (exit code 1), releases the lock and removes the socket;
// every acknowledged commit stays durable in the log and the next open serves it
// (SPEC/GRAPH.md § Server Diagnostics on Stderr; § Server Shutdown and the Drain).
//
// The server's logger accepts no record at all, so the test also proves the
// recognition does not depend on where, or whether, diagnostics are written.
func TestServe_AFailStoppedEngineShutsTheServerDownWithoutAFold(t *testing.T) {
	silent := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 100}))
	s := serveInProcess(t, silent)

	mustSend(t, s.socket, "CREATE (:Task {key: 'fail-stop-seed'})")
	manifest := filepath.Join(s.graphDir, "snapshot", "manifest.json")
	if _, err := os.Stat(manifest); !os.IsNotExist(err) {
		t.Fatalf("the store already holds a snapshot before the shutdown (stat: %v); the test could not "+
			"tell a skipped shutdown fold from one that ran", err)
	}

	s.st.Graph().IndexManager().MarkUndefined("injected by " + t.Name())
	if _, err := graphclient.Send(context.Background(), s.socket, "MATCH (n:Task) RETURN count(n)"); err == nil {
		t.Fatal("a statement against a fail-stopped engine succeeded; the injection did not take")
	}

	err := s.wait(t)
	if !errors.Is(err, utils.ErrGraphServer) {
		t.Fatalf("the fail-stopped server ended with %v, want an error carrying utils.ErrGraphServer (exit 1)", err)
	}
	if got := "Error: " + err.Error(); got != publishedFailStopLine {
		t.Errorf("the fail-stopped server's line is\n  %q\nwant\n  %q", got, publishedFailStopLine)
	}
	s.requireReleased(t)

	// Step 4 was skipped: the log had grown, so an ordinary shutdown would have
	// folded it into a snapshot.
	if _, err := os.Stat(manifest); !os.IsNotExist(err) {
		t.Errorf("the fail-stopped server's shutdown wrote a snapshot (stat: %v); it must take no fold, "+
			"because a fold would serialise indexes whose state is undefined", err)
	}

	// Nothing acknowledged was lost, and the next open rebuilt the indexes.
	reopened, err := graphstore.Open(s.graphDir)
	if err != nil {
		t.Fatalf("reopening the store after a fail-stop: %v", err)
	}
	defer reopened.Close() //nolint:errcheck // a read-only check; the close releases the lock
	result, err := reopened.Engine().RunInTx(context.Background(),
		"MATCH (n:Task {key: 'fail-stop-seed'}) RETURN count(n) AS c", nil)
	if err != nil {
		t.Fatalf("querying the reopened store: %v", err)
	}
	var count any
	for result.Next() {
		count = result.Record()["c"]
	}
	if err := result.Close(); err != nil {
		t.Fatalf("closing the reopened store's result: %v", err)
	}
	if fmt.Sprint(count) != "1" {
		t.Errorf("the reopened store holds %v nodes the acknowledged commit created, want 1", count)
	}
}

// TestFailStopWatch_ActsOnlyOnAConfirmedRefusal pins the two halves of the
// recognition: a record must name the fail-stop, and the engine's state must
// confirm it. A statement whose error merely quoted the sentinel must not stop a
// healthy server, and a failure that names something else must not stop a
// fail-stopped one before its own refusal arrives (SPEC/GRAPH.md § Server
// Diagnostics on Stderr, rule 1).
func TestFailStopWatch_ActsOnlyOnAConfirmedRefusal(t *testing.T) {
	refusal := cypher.ErrEngineFailStopped.Error() + ": index: index state undefined"
	undefinedErr := errors.New("index state undefined")
	cases := []struct {
		name      string
		value     slog.Value
		undefined error
		want      bool
	}{
		{"the refusal over a fail-stopped engine", slog.StringValue(refusal), undefinedErr, true},
		{"the refusal as an error value", slog.AnyValue(cypher.ErrEngineFailStopped), undefinedErr, true},
		{"the sentinel quoted while the engine is healthy", slog.StringValue("syntax error near '" + refusal + "'"), nil, false},
		{"another failure while the engine is fail-stopped", slog.StringValue("cypher: parse: unexpected token"), undefinedErr, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gate := &foldGate{}
			watch := newFailStopWatch(func() error { return tc.undefined }, gate)
			log := watchedLogger(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 100})), watch)
			log.Error("bolt: query execution failed", slog.String("session", "s1"), slog.Attr{Key: "err", Value: tc.value})
			if got := watch.recognised(); got != tc.want {
				t.Fatalf("recognised = %v, want %v", got, tc.want)
			}
			if got := gate.failStopped.Load(); got != tc.want {
				t.Fatalf("the gate refuses folds = %v, want %v", got, tc.want)
			}
			// A second refusal changes nothing and does not close the channel twice.
			log.Error("bolt: query execution failed", slog.Attr{Key: "err", Value: tc.value})
		})
	}
}

// TestFoldGate_RefusesEveryFoldOnceTheEngineFailStopped pins that the refusal is
// the gate's, so neither checkpoint path can fold past it: a fold owed before
// the refusal is not taken after it.
func TestFoldGate_RefusesEveryFoldOnceTheEngineFailStopped(t *testing.T) {
	closer := buildOverAFreshStore(t, checkpointCadence{})
	if _, err := closer.gate.st.Engine().RunInTx(context.Background(), "CREATE (:Task {key: 'owed'})", nil); err != nil {
		t.Fatalf("seeding a write: %v", err)
	}
	closer.gate.refuseFolds()
	ran, err := closer.gate.foldIfOwed()
	if ran || err != nil {
		t.Fatalf("a fold owed by a write ran after the engine fail-stopped (ran=%v, err=%v)", ran, err)
	}
}

// TestServe_LeavesNoGoGraphGoroutineBehind is the regression fence for rmp task
// #636: the shutdown closes the engine, which stops the graph's background
// reclamation goroutine and waits for it, so no goroutine of the engine outlives
// the server (SPEC/GRAPH.md § Engine Construction and Lifecycle, step 7; § Server
// Shutdown and the Drain, step 5).
//
// The count of the engine's goroutines is compared before and after. That
// comparison alone could pass a server that never closed the engine, because the
// reclamation goroutine also exits on its own once idle, so the test then asks
// the engine to reclaim again: a closed graph starts no reclamation goroutine, and
// an open one does.
func TestServe_LeavesNoGoGraphGoroutineBehind(t *testing.T) {
	before := goGraphGoroutines()
	s := serveInProcess(t, discardLogger())

	mustSend(t, s.socket, "UNWIND range(1, 3000) AS i CREATE (:Task {i: i})")
	mustSend(t, s.socket, "MATCH (n:Task) SET n.j = n.i")
	mustSend(t, s.socket, "MATCH (n:Task) SET n.j = n.i + 1")
	if s.st.Graph().VacuumStats().Starts == 0 {
		t.Fatal("the engine never started its reclamation goroutine while serving; the test would not be " +
			"about a goroutine that exists")
	}

	s.sigCh <- syscall.SIGTERM
	if err := s.wait(t); err != nil {
		t.Fatalf("the signalled server ended with %v, want nil", err)
	}
	s.requireReleased(t)

	deadline := time.Now().Add(5 * time.Second)
	for goGraphGoroutines() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := goGraphGoroutines(); after > before {
		t.Errorf("%d goroutine(s) of the engine outlived the server (%d before it started):\n%s",
			after-before, before, goGraphStacks())
	}

	stats := s.st.Graph().VacuumStats()
	// The write fails, because the log is closed, and its rollback asks the graph
	// to reclaim what it wrote.
	if result, err := s.st.Engine().RunInTx(context.Background(),
		"UNWIND range(1, 5000) AS i CREATE (:Probe {i: i})", nil); err == nil {
		_ = result.Close() //nolint:errcheck // the close's failure is the expected outcome; the check is below
	}
	if got := s.st.Graph().VacuumStats(); got.Starts != stats.Starts || got.Running {
		t.Errorf("after the shutdown the engine started its reclamation goroutine again (starts %d -> %d, "+
			"running %v): the shutdown did not close the engine", stats.Starts, got.Starts, got.Running)
	}
}

// goGraphPackage is the import path prefix that identifies a goroutine of the
// engine in a stack dump.
const goGraphPackage = "github.com/FlavioCFOliveira/GoGraph/"

// goGraphGoroutines counts the goroutines with a frame of the engine on their
// stack.
func goGraphGoroutines() int {
	return len(goGraphStackList())
}

// goGraphStacks returns the stack dumps of the goroutines with a frame of the
// engine, separated by blank lines.
func goGraphStacks() string {
	return strings.Join(goGraphStackList(), "\n\n")
}

// goGraphStackList returns one stack dump per goroutine with a frame of the
// engine. The dump separates goroutines by a blank line, and a goroutine's own
// text never contains one.
func goGraphStackList() []string {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	var out []string
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, goGraphPackage) {
			out = append(out, g)
		}
	}
	return out
}
