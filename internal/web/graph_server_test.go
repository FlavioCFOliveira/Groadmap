package web

// The graph server every test in this package that expects the endpoint to
// ANSWER now needs.
//
// The graph data endpoint reaches a graph through internal/graphclient and
// through nothing else: it opens no store, and a roadmap nothing is serving is
// answered HTTP 503 (SPEC/WEB.md § Knowledge Graph from the GoGraph Store,
// rule 1). A test that seeds a store on disk and then submits a statement is
// therefore asserting about a graph the endpoint cannot see unless a server is
// running for it, and this file supplies the server.
//
// # Why a real server and not the scripted one
//
// graph_served_test.go scripts the server's half of the protocol with the
// engine's own proto package, and that is the right instrument for what it
// asserts: that resolution happens, that its outcome is not cached, that the
// endpoint speaks the protocol at all. It answers a FIXED exchange, so it cannot
// be the instrument for a test whose subject is what a statement returns — the
// node the endpoint reports would be the script's, and the assertion would be
// about the harness.
//
// The tests here submit real Cypher against real data and read back what the
// engine produced, so the server has to be a real one. It is a child process
// running the production graphserve.Run; see internal/testenv/graphserver for
// why it is a process, why it is this test binary rather than ./bin/rmp, and why
// it lives beside internal/testenv rather than in it.
//
// # The order the two halves must run in
//
// seedGraph opens the store directly, without taking its advisory lock, so it
// MUST run before a server is started: a server holds that lock for its whole
// lifetime, and a seed that ran beside one would be a second writer against a
// store another process has open. Every helper below therefore seeds first and
// serves second, and the server is stopped by t.Cleanup after the test's own
// deferred work.

import (
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv/graphserver"
)

// serveGraph starts a graph server for the roadmap on its DERIVED socket and
// returns the function that stops it.
//
// The socket is derived rather than chosen because the endpoint publishes no
// `--socket` flag and has nowhere to receive one: it resolves the derived path
// and only that path (SPEC/GRAPH.md § Serving on a Non-Default Socket). A test
// server on any other path would be invisible to the code under test.
func serveGraph(t *testing.T, name string) func() {
	t.Helper()
	return serveGraphAtBudget(t, name, 0)
}

// serveGraphAtBudget starts a server that enforces budget, and installs the same
// value in this process.
//
// Both halves are needed and each catches a different defect. The SERVER is what
// cuts a statement, because that is where the statement runs; this process reads
// the same declaration only to render the budget's figure into the message the
// endpoint returns (graphExecutionError). A server carrying the wrong budget cuts
// at the wrong moment, and an endpoint carrying the wrong one names a duration
// the run did not have.
//
// A zero budget leaves the production default in force on both sides, which is
// what every server but a budget test's wants.
func serveGraphAtBudget(t *testing.T, name string, budget time.Duration) func() {
	t.Helper()

	if budget > 0 {
		setGraphQueryBudget(t, budget)
	}

	socket, err := graphclient.SocketPath(name)
	if err != nil {
		t.Fatalf("deriving the socket path of roadmap %q: %v", name, err)
	}

	server, err := graphserver.Start(graphserver.Options{
		GraphDir:        webGraphDir(t, name),
		Socket:          socket,
		RoadmapName:     name,
		CaptureDir:      t.TempDir(),
		StatementBudget: budget,
	})
	if err != nil {
		t.Fatalf("starting the graph server for roadmap %q: %v", name, err)
	}

	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		if stopErr := server.Stop(); stopErr != nil {
			t.Errorf("stopping the graph server for roadmap %q: %v", name, stopErr)
		}
	}
	t.Cleanup(stop)
	return stop
}

// servedRoadmap is the shape most tests want: a roadmap with a database, a graph
// seeded with queries, and a server answering on its derived socket.
//
// It returns the roadmap name, so a call replaces the seedRoadmap + seedGraph
// pair it grew out of.
func servedRoadmap(t *testing.T, roadmap string, queries ...string) string {
	t.Helper()

	name := seedRoadmap(t, roadmap)
	if len(queries) > 0 {
		seedGraph(t, name, queries...)
	}
	serveGraph(t, name)
	return name
}

// servedGraphWithSchema is seedGraphWithSchema plus a server: the small semantic
// graph, a declared index and a declared constraint, reachable through the
// endpoint.
func servedGraphWithSchema(t *testing.T, roadmap string) string {
	t.Helper()

	name := seedGraphWithSchema(t, roadmap)
	serveGraph(t, name)
	return name
}

// servedRoadmapAtBudget is servedRoadmap with a named statement time budget, for
// the tests whose subject is the budget rather than the statement.
func servedRoadmapAtBudget(t *testing.T, roadmap string, budget time.Duration, queries ...string) string {
	t.Helper()

	name := seedRoadmap(t, roadmap)
	if len(queries) > 0 {
		seedGraph(t, name, queries...)
	}
	serveGraphAtBudget(t, name, budget)
	return name
}
