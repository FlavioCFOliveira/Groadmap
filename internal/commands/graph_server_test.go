package commands

// The graph server every test in this package that runs a statement needs.
//
// `rmp graph client` sends a statement to a running server and does nothing else
// with it: there is no fallback, no one-shot form, and nothing that opens a store
// (SPEC/GRAPH.md § The Dedicated Graph Server). A test that used to call
// `runGraphExecute` and get an answer therefore now needs two things — a roadmap,
// and a server for it — and this file supplies the second.
//
// The server is a child process running the production graphserve.Run, started
// through internal/testenv/graphserver; see that package for why it is a process,
// why it is this test binary rather than ./bin/rmp, and why it lives beside
// internal/testenv rather than in it.

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv/graphserver"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// serveGraph starts a graph server for roadmap on its DERIVED socket and returns
// the function that stops it.
//
// The socket is derived rather than chosen, so a `runGraphClient` invocation that
// is told nothing but the roadmap name reaches this server — which is the
// arrangement a user gets and therefore the one the tests should exercise
// (SPEC/GRAPH.md § Socket Path and Permissions, rule 1).
//
// # How to wire it, and why the order matters
//
// Register it AFTER the roadmap's own cleanup, so that it runs BEFORE it:
//
//	defer setupTestGraphRoadmap(t, roadmap)()
//	defer serveGraph(t, roadmap)()
//
// Deferred calls run last-registered-first, so this stops the server and only
// then is the roadmap directory removed. The other order removes the store out
// from under a live server, which turns an unrelated failure into a shutdown
// checkpoint that cannot write.
//
// A stop is also registered as a t.Cleanup, as a net rather than as the
// mechanism: it costs nothing when the deferred stop has already run — Stop is
// idempotent — and it keeps a test that forgets the defer from leaking a process
// that holds a store lock for the rest of the run.
func serveGraph(t *testing.T, roadmap string) func() {
	t.Helper()

	graphDir, err := resolveGraphDir(roadmap)
	if err != nil {
		t.Fatalf("resolving the graph directory of roadmap %q: %v", roadmap, err)
	}
	socket, err := graphclient.SocketPath(roadmap)
	if err != nil {
		t.Fatalf("deriving the socket path of roadmap %q: %v", roadmap, err)
	}
	return serveGraphOn(t, roadmap, graphDir, socket)
}

// serveGraphOn starts a server over an explicit graph directory and socket, for
// the tests whose subject is one of those two paths rather than the statement.
func serveGraphOn(t *testing.T, roadmap, graphDir, socket string) func() {
	t.Helper()

	server, err := graphserver.Start(graphserver.Options{
		GraphDir:    graphDir,
		Socket:      socket,
		RoadmapName: roadmap,
		// The capture is under the test's own temporary directory rather than
		// under the roadmap home: it is read only when something has already gone
		// wrong, and a file inside the store directory would be one more thing the
		// tests that fingerprint that directory would have to account for.
		CaptureDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("starting the graph server for roadmap %q: %v", roadmap, err)
	}

	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		if stopErr := server.Stop(); stopErr != nil {
			t.Errorf("stopping the graph server for roadmap %q: %v", roadmap, stopErr)
		}
	}
	t.Cleanup(stop)
	return stop
}

// servedRoadmap creates a roadmap, starts a server for it, and returns the
// function that tears both down in the right order.
//
// It is the shape most tests want — one roadmap, one server, one deferred call —
// and it exists so that the ordering rule serveGraph documents is followed by
// construction rather than by every caller remembering it:
//
//	defer servedRoadmap(t, "graph-relread-core")()
func servedRoadmap(t *testing.T, roadmap string) func() {
	t.Helper()

	cleanupRoadmap := setupTestGraphRoadmap(t, roadmap)
	stopServer := serveGraph(t, roadmap)
	return func() {
		stopServer()
		cleanupRoadmap()
	}
}

// graphDirOf is the graph store directory of a roadmap under the home in force,
// for a test that must look at what the server wrote.
func graphDirOf(t *testing.T, roadmap string) string {
	t.Helper()

	graphDir, err := resolveGraphDir(roadmap)
	if err != nil {
		t.Fatalf("resolving the graph directory of roadmap %q: %v", roadmap, err)
	}
	return graphDir
}

// graphDirExists reports whether a roadmap has a graph store directory at all.
//
// It is what a test asserts against when the point is that a refused invocation
// created nothing: `rmp graph client` opens no store and creates no directory, so
// a directory that appeared was created by something that should not have.
func graphDirExists(t *testing.T, roadmap string) bool {
	t.Helper()

	_, err := os.Stat(graphDirOf(t, roadmap))
	return err == nil
}

// TestGraphServe_RefusesAGraphPathThatIsNotADirectory is where the condition
// internal/web's TestHandleGraphData_GraphPathIsFile used to cover now lives.
//
// A stray regular file at ~/.roadmaps/<name>/graph was once something the web
// graph data endpoint had to tolerate: it stat'ed the path, found it was not a
// directory, and answered an empty graph. That endpoint opens no store and stats
// nothing now, so the collision reaches exactly one surface — `rmp graph serve`,
// which is the only thing that creates that directory
// (SPEC/GRAPH.md § Server Startup, step 1).
//
// What it must do with it is refuse, in the class the store failures carry, and
// leave the file alone: a `serve` that removed whatever was in its way would
// destroy a caller's data on the strength of a name. Both halves are asserted,
// and the second is the one an implementation gets wrong.
//
// The refusal is driven through runGraphServe rather than through createGraphDir,
// so what is exercised is the ORDER as well as the classification: the roadmap
// and the socket path are resolved first, and this failure is what stops the
// invocation before anything is bound or locked.
func TestGraphServe_RefusesAGraphPathThatIsNotADirectory(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const roadmap = "graph-serve-stray-file"
	defer setupTestGraphRoadmap(t, roadmap)()

	graphPath := graphDirOf(t, roadmap)
	const stray = "not a directory"
	if err := os.WriteFile(graphPath, []byte(stray), 0600); err != nil {
		t.Fatalf("writing the stray file at %s: %v", graphPath, err)
	}

	var err error
	stdout, _ := captureStdStreams(t, func() {
		err = runGraphServe([]string{"-r", roadmap})
	})
	if err == nil {
		t.Fatal("rmp graph serve started against a roadmap whose graph path is a regular file; it " +
			"must refuse, because it cannot create the directory the store lives in")
	}
	if !errors.Is(err, utils.ErrGraphStore) {
		t.Errorf("err = %v, want it to wrap utils.ErrGraphStore, which is exit code 1: the failure "+
			"is the store's directory, not the roadmap and not the flags", err)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("a refused serve wrote %q to stdout; the startup object is written only by a "+
			"server that started (SPEC/COMMANDS.md § Serve Output)", stdout)
	}

	// The file is left exactly as it was found.
	raw, readErr := os.ReadFile(graphPath) //nolint:gosec // a path this test created under a temporary HOME
	if readErr != nil {
		t.Fatalf("the stray file at %s is gone after a refused serve: %v. A serve that cleared "+
			"whatever occupied the path would destroy a caller's data on the strength of a name",
			graphPath, readErr)
	}
	if string(raw) != stray {
		t.Errorf("the stray file now holds %q, want the %q it held before the refused serve",
			raw, stray)
	}
}
