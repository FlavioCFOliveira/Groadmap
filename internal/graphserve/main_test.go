package graphserve

import (
	"fmt"
	"os"
	"testing"
)

// TestMain doubles as the entry point of the CHILD SERVER PROCESS the durability
// tests drive, and that is the whole reason this package has one.
//
// # Why a child process at all
//
// Two of this package's requirements are about what happens to a PROCESS:
// SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process requires
// that a server killed outright lose nothing it acknowledged, and § Server
// Shutdown and the Drain requires that a signal drain rather than truncate. A
// signal is delivered to a process and SIGKILL cannot be caught at all, so
// neither can be driven against a server assembled inside the test binary: an
// in-process server can be stopped only through the same orderly path the
// production code takes, which is precisely the path the tests must NOT be
// allowed to assume.
//
// Re-executing the test binary is what gives the tests a real process to signal.
// The child runs the production entry point [Run] — not a re-assembly of its
// steps — so what the tests kill and signal is the startup sequence, the drain
// and the ordered teardown as `rmp graph serve` performs them, including the
// signal handling [Run] installs, which is itself part of what is under test.
//
// # Why it is not the built binary
//
// The end-to-end suite drives ./bin/rmp and rmp task #371 owns it. These tests
// must run under `go test ./...` with no build step before them, and under the
// race detector, where the built binary is neither instrumented nor necessarily
// present. os.Executable is the test binary itself, so the child is the same
// code the parent is testing, built the same way.
func TestMain(m *testing.M) {
	if os.Getenv(childRoleEnv) != "" {
		os.Exit(serveAsChild())
	}
	os.Exit(m.Run())
}

// The environment the parent hands the child. They are read only by
// [serveAsChild] and written only by startServerProcess, which is why they are
// unexported constants rather than a published contract: nothing outside this
// package's tests may start a server this way.
const (
	// childRoleEnv, when set to anything non-empty, turns a run of this test
	// binary into a graph server instead of a test run.
	childRoleEnv = "GROADMAP_GRAPHSERVE_TEST_SERVE"

	// childGraphDirEnv is the graph store directory the child serves. It must
	// already exist: Run creates no graph directory.
	childGraphDirEnv = "GROADMAP_GRAPHSERVE_TEST_GRAPHDIR"

	// childSocketEnv is the absolute socket path the child binds.
	childSocketEnv = "GROADMAP_GRAPHSERVE_TEST_SOCKET"
)

// childRoadmapName is the roadmap name the child reports in the lock refusal
// line. Nothing in these tests resolves a roadmap — the graph directory and the
// socket are passed as absolute paths, exactly as the CLI passes them after it
// has resolved them — so this is the name that would appear in a diagnostic and
// nothing more.
const childRoadmapName = "durability"

// serveAsChild runs the production server in this process and returns the exit
// code the CLI would use.
//
// The announcement is written to stdout as the bare socket path rather than as
// the CLI's JSON object: the OUTPUT is internal/commands' and this package
// serialises nothing (SPEC/ARCHITECTURE.md module 10), so a test that wanted the
// JSON would have to reach into a package this one does not import. The path is
// what the parent checks, and it checks it against the path it asked for.
func serveAsChild() int {
	err := Run(Options{
		Announce: func(socket string) error {
			_, printErr := fmt.Fprintln(os.Stdout, socket)
			return printErr
		},
		RoadmapName: childRoadmapName,
		GraphDir:    os.Getenv(childGraphDirEnv),
		SocketPath:  os.Getenv(childSocketEnv),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
