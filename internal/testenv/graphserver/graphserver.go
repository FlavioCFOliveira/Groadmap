// Package graphserver runs a real `rmp graph serve` for the package test suites
// that must speak to one. It is test support and nothing else: no production
// code path reaches it.
//
// # Why any of this exists
//
// The graph is reachable only through a running server. `rmp graph serve` is the
// only process that opens the store and `rmp graph client` is the only subcommand
// that runs a statement (SPEC/GRAPH.md § The Dedicated Graph Server). Two package
// suites therefore need a server before they can assert anything about a graph at
// all — internal/commands, whose subject is the client, and internal/web, whose
// graph data endpoint reaches a server through internal/graphclient and answers
// HTTP 503 when none is reachable.
//
// # Why a child PROCESS rather than a server inside the test binary
//
// graphserve.Run serves until a signal arrives, so a test cannot call it and get
// control back: the whole of the production entry point — the startup order, the
// signal take-over, the drain, the shutdown checkpoint and the ordered teardown —
// is behind a call that does not return. Running it on a goroutine and stopping
// it by signalling the test binary would deliver that signal to the test process
// itself, whose own signal disposition is the subject of other tests in both
// suites.
//
// A child process gives a test the thing the production code is built around: a
// process with a lifetime, that can be signalled, and that holds the store's
// exclusive advisory lock for its whole life exactly as a real server does — so a
// test drives that arrangement rather than simulating it.
//
// # Why the child is THIS binary and not ./bin/rmp
//
// Three reasons, and the first is decisive.
//
// The validation gates run `go test ./...` BEFORE `go build` (CLAUDE.md § 6,
// rule 2, steps 3 and 4). A suite that shelled out to ./bin/rmp would therefore
// run against a binary that is absent on a clean checkout and stale on every
// other run — a verdict that depends on an artefact the step before it did not
// produce, and one that would silently test yesterday's code. Re-executing
// os.Executable() has no build ordering at all: the child is built from the same
// source as the parent, by construction.
//
// The second is the race detector. `go test -race` instruments the test binary,
// so a child that IS the test binary is instrumented too and a data race inside
// the server is reported. A separately built ./bin/rmp is not instrumented, and a
// race in the server would be invisible to the suites that drive it hardest.
//
// The third is that it needs no seam in the production code. graphserve.Run is
// called exactly as `rmp graph serve` calls it, with no test-only entry point
// beside it and nothing exported for a test's convenience.
//
// # Why it is a package of its own rather than part of internal/testenv
//
// internal/testenv has NO first-party dependencies, and that is load-bearing
// rather than incidental: the module-wide gates it hosts must compile and run
// against a tree in which the packages they audit do not, which is exactly the
// tree a broken gate would otherwise hide in (see its
// engine_constructor_gate_test.go). This package imports internal/graphserve —
// one of the packages that gate audits — so it lives beside testenv instead of
// inside it, and that property is preserved.
//
// # Who cannot use it
//
// internal/graphserve keeps a harness of its own, and must. Its tests are in
// package graphserve, so importing this package — which imports graphserve —
// would be an import cycle. Its own is also doing more: it kills a server
// outright and asserts the signal that ended it, which is the subject of those
// tests rather than a means to one.
package graphserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphserve"
	"github.com/FlavioCFOliveira/Groadmap/internal/signals"
)

// The environment a parent hands its child. They are read only by [RunChild] and
// written only by [Start]; nothing outside a test starts a server this way, which
// is why they are a private convention rather than a published contract.
const (
	// roleEnv, set to anything non-empty, turns a run of the test binary into a
	// graph server instead of a test run.
	roleEnv = "GROADMAP_TESTENV_GRAPH_SERVE"

	// graphDirEnv is the graph store directory the child serves. It must already
	// exist: graphserve.Run creates no graph directory, because creating one is
	// `rmp graph serve`'s own step and internal/commands performs it before it
	// reaches that package (SPEC/GRAPH.md § Server Startup, step 1).
	graphDirEnv = "GROADMAP_TESTENV_GRAPH_DIR"

	// socketEnv is the absolute socket path the child binds.
	socketEnv = "GROADMAP_TESTENV_GRAPH_SOCKET"

	// roadmapEnv is the roadmap name the child reports in a lock refusal. It
	// reaches nothing else: the graph directory and the socket are handed over as
	// absolute paths, exactly as the CLI hands them over once it has resolved
	// them, so the child resolves no roadmap of its own.
	roadmapEnv = "GROADMAP_TESTENV_GRAPH_ROADMAP"

	// budgetEnv carries a statement time budget for the child to serve under,
	// as a time.Duration string. It is empty for every server but the handful
	// whose SUBJECT is the budget.
	//
	// # Why this exists, and why it is not a knob on the product
	//
	// graphlock.StatementBudget is a package variable that production
	// initialises once and never reassigns: no flag, no environment variable and
	// no other user-facing control reaches it, and a `--timeout` flag was
	// considered and declined (rmp task #377). A test inside the binary moves it
	// by assignment, which is what internal/commands' setGraphStatementBudget and
	// internal/web's setGraphQueryBudget both do, and that is how the budget's
	// behaviour is provable in milliseconds instead of five real seconds a run.
	//
	// The statement now runs in the SERVER's process, which has its own copy of
	// that variable, so an assignment in the test process no longer reaches the
	// thing being timed. This variable carries the same assignment across the
	// process boundary — and it is read HERE, in a package that only test
	// binaries link, never by cmd/rmp. The property production claims for the
	// budget is therefore intact: `rmp` exposes no way to move it.
	budgetEnv = "GROADMAP_TESTENV_GRAPH_BUDGET"
)

// The three failures this package raises rather than relays. They are sentinels
// rather than messages built at the point of failure because err113 is right
// about the general case and there is no reason for a test harness to be the
// exception: a caller that wanted to tell "the child died" from "the child never
// answered" — a retry loop around Start, say — can, and the interpolated detail
// follows the sentinel rather than replacing it.
var (
	errChildExited   = errors.New("graphserver: the child server exited before it served")
	errChildSilent   = errors.New("graphserver: the child server did not answer within the start deadline")
	errChildLingered = errors.New("graphserver: the child server did not exit after SIGTERM")
)

// interruptExitCode mirrors cmd/rmp's ExitSigint: what a signal means to an
// invocation that has not taken the signals over. A child that exits with it was
// stopped before graphserve.Run reached its take-over, which for a server that
// has already announced itself would be a defect.
const interruptExitCode = 130

// StartDeadline is how long [Start] waits for the child to answer the resolution
// probe.
//
// It is generous rather than tight, and deliberately: the child has to be
// re-executed, open a store and bind, and it does all of that on a machine
// running the rest of the suite beside it. A wait that expired early would report
// a loaded machine as a server that never started.
const StartDeadline = 60 * time.Second

// StopDeadline is how long [Server.Stop] waits for a signalled child to exit.
//
// It is sized on what a shutdown can lawfully take rather than on what it usually
// takes. A statement the budget cut while it was writing holds the engine open
// inside an undo replay that takes no cancellation — measured at up to 35.6
// seconds, the largest measured and not a maximum, with no ceiling established
// (see internal/graphserve.stop) — and the store cannot close until that call
// returns. Ninety seconds clears that measurement by more than twice.
//
// What it is NOT is a bound on the shutdown, and no constant here could be one.
// A shutdown held by a peer that parked in a socket write after the drain took
// its mark has no upper bound at all: Groadmap leaves the engine's connection
// timeout at the engine's own default, so nothing arms a deadline on that write
// (SPEC/GRAPH.md § Server Options). This deadline therefore decides how long a
// TEST waits before it kills the child and reports errChildLingered, and an
// expiry is a diagnosis rather than a proof that the server was wrong to still
// be running.
const StopDeadline = 90 * time.Second

// RunChild runs the production graph server in this process when the process was
// started as a test's child server, and reports whether it did.
//
// Wire it as the first statement of a package's TestMain:
//
//	func TestMain(m *testing.M) {
//		if code, isChild := graphserver.RunChild(); isChild {
//			os.Exit(code)
//		}
//		os.Exit(runTests(m))
//	}
//
// The child runs graphserve.Run — the production entry point, not a re-assembly
// of its steps — so what a test drives is the startup sequence, the drain and the
// ordered teardown as `rmp graph serve` performs them, including the signal
// handling that entry point installs.
//
// The announcement is written to stdout as the bare socket path rather than as
// the CLI's JSON object. That output is internal/commands' and internal/graphserve
// serialises nothing (SPEC/ARCHITECTURE.md module 10), so a harness that produced
// the JSON would be a second producer of a published shape. The path is what a
// parent checks, and it checks it against the path it asked for.
func RunChild() (exitCode int, isChild bool) {
	if os.Getenv(roleEnv) == "" {
		return 0, false
	}

	// The default disposition cmd/rmp/main.go declares. Without it the child
	// would carry no handler at all until graphserve.Run takes the signals over,
	// which is a state no `rmp` invocation is ever in: a signal delivered during
	// startup would kill the child where the real command exits 130
	// (internal/signals).
	signals.Install(func(os.Signal) { os.Exit(interruptExitCode) })

	// The budget the parent asked this server to enforce, when it asked for one.
	// An unparsable or absent value leaves the production default in place, which
	// is the safe direction: a test that meant to shorten the budget and did not
	// fails on its own timing assertion rather than silently passing.
	if raw := os.Getenv(budgetEnv); raw != "" {
		budget, parseErr := time.ParseDuration(raw)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "graphserver: %s=%q is not a duration: %v\n", budgetEnv, raw, parseErr)
			return 1, true
		}
		graphlock.StatementBudget = budget
	}

	err := graphserve.Run(graphserve.Options{
		Announce: func(socket string) error {
			_, printErr := fmt.Fprintln(os.Stdout, socket)
			return printErr
		},
		RoadmapName: os.Getenv(roadmapEnv),
		GraphDir:    os.Getenv(graphDirEnv),
		SocketPath:  os.Getenv(socketEnv),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, true
	}
	return 0, true
}

// Options is everything [Start] needs.
type Options struct {
	// GraphDir is the store directory to serve. It is created at 0700 if it is
	// not there, because that creation is `rmp graph serve`'s own first step and
	// a harness that skipped it would have the server report a missing graph as a
	// lock file it could not open.
	GraphDir string

	// Socket is the absolute path to bind. It is normally the path
	// graphclient.SocketPath derives for the roadmap, so a client told nothing
	// reaches this server.
	Socket string

	// RoadmapName is the name the child reports in a lock refusal, and nothing
	// else.
	RoadmapName string

	// CaptureDir is where the child's stdout and stderr are written. It is
	// created if it is not there.
	//
	// Two servers never collide over it, and no caller has to arrange that:
	// t.TempDir returns a FRESH directory on every call, so a test that starts
	// two servers passes two directories without meaning to. A tag
	// distinguishing one capture from another would be a second answer to a
	// question the caller has already answered.
	CaptureDir string

	// StatementBudget is the budget the server enforces on a statement. The zero
	// value leaves the production default in force, which is what every server
	// but a budget test's wants. See budgetEnv for why it crosses the process
	// boundary this way and why it is not a control on the product.
	StatementBudget time.Duration
}

// Server is a handle on a running child server.
//
// It carries the paths its two output streams were captured to, because a test
// that fails while a server is running needs the server's own diagnostics to say
// why, and a child's stderr is otherwise interleaved with the parent's.
type Server struct {
	cmd    *exec.Cmd
	socket string
	stdout string
	stderr string
	waited bool
}

// Start re-executes the test binary as a graph server, waits until it answers the
// resolution probe, and returns the handle.
//
// The wait is on the PROBE — the one resolver every surface uses
// (SPEC/GRAPH.md § Server Resolution) — and never on a sleep somebody chose. A
// sleep is a guess about a machine: it passes on a fast one and leaves a loaded
// one to fail in whatever way the first statement happens to fail in.
//
// It takes no *testing.T, for the reason internal/testenv's ShortHome gives: a
// harness that owns no assertion should not own the *testing.T either. A caller
// wires the error into t.Fatalf and the handle's Stop into t.Cleanup.
func Start(opts Options) (*Server, error) {
	if err := os.MkdirAll(opts.GraphDir, 0700); err != nil {
		return nil, fmt.Errorf("graphserver: creating the graph directory %s: %w", opts.GraphDir, err)
	}
	// MkdirAll applies the process umask to the bits it is given, so the mode is
	// set again — the same two steps `rmp graph serve` performs, so a test's store
	// carries the permissions a real one carries (CLAUDE.md § 10).
	// #nosec G302 -- 0700 on a DIRECTORY is mandated by SPEC (CLAUDE.md §10: 0700 for the ~/.roadmaps tree); gosec G302 false-positives on directory permissions
	if err := os.Chmod(opts.GraphDir, 0700); err != nil {
		return nil, fmt.Errorf("graphserver: setting the graph directory permissions: %w", err)
	}
	if err := os.MkdirAll(opts.CaptureDir, 0700); err != nil {
		return nil, fmt.Errorf("graphserver: creating the capture directory %s: %w", opts.CaptureDir, err)
	}

	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("graphserver: locating the test binary to re-execute: %w", err)
	}

	s := &Server{
		socket: opts.Socket,
		stdout: filepath.Join(opts.CaptureDir, "graphserve.out"),
		stderr: filepath.Join(opts.CaptureDir, "graphserve.err"),
	}

	out, err := os.Create(s.stdout)
	if err != nil {
		return nil, fmt.Errorf("graphserver: creating %s: %w", s.stdout, err)
	}
	defer out.Close() //nolint:errcheck // the child holds its own descriptor; this one is only for the handover
	errOut, err := os.Create(s.stderr)
	if err != nil {
		return nil, fmt.Errorf("graphserver: creating %s: %w", s.stderr, err)
	}
	defer errOut.Close() //nolint:errcheck // idem

	// #nosec G204 -- the executable is this test binary's own path from os.Executable, never caller input
	s.cmd = exec.Command(self)
	s.cmd.Stdout = out
	s.cmd.Stderr = errOut
	s.cmd.Env = append(os.Environ(),
		roleEnv+"=1",
		graphDirEnv+"="+opts.GraphDir,
		socketEnv+"="+opts.Socket,
		roadmapEnv+"="+opts.RoadmapName,
	)
	if opts.StatementBudget > 0 {
		s.cmd.Env = append(s.cmd.Env, budgetEnv+"="+opts.StatementBudget.String())
	}
	if err := s.cmd.Start(); err != nil {
		return nil, fmt.Errorf("graphserver: starting the child graph server: %w", err)
	}

	if err := s.waitUntilServed(); err != nil {
		_ = s.Stop() //nolint:errcheck // already failing; the stop is to keep the run from leaking a process
		return nil, err
	}
	return s, nil
}

// Socket is the path the server bound.
func (s *Server) Socket() string { return s.socket }

// Diagnostics returns what the child wrote to stderr, for a failure message.
//
// It never fails: a harness that could not read its own capture must still let
// the test report the failure it was actually about.
func (s *Server) Diagnostics() string {
	raw, err := os.ReadFile(s.stderr) //nolint:gosec // a path this package created
	if err != nil {
		return fmt.Sprintf("(the server's stderr at %s could not be read: %v)", s.stderr, err)
	}
	return string(raw)
}

// Announced returns what the child wrote to stdout, which is the socket path it
// bound followed by a newline.
func (s *Server) Announced() string {
	raw, err := os.ReadFile(s.stdout) //nolint:gosec // a path this package created
	if err != nil {
		return ""
	}
	return string(raw)
}

// waitUntilServed blocks until the child answers the resolution probe.
func (s *Server) waitUntilServed() error {
	deadline := time.Now().Add(StartDeadline)
	for {
		state, _ := graphclient.Resolve(context.Background(), s.socket)
		if state.Served() {
			return nil
		}
		// A child that has already exited will never answer, so the wait ends at
		// once rather than at the deadline. The captured stderr is what says why.
		if s.cmd.ProcessState != nil {
			return fmt.Errorf("%w: %s: %v\nstderr:\n%s",
				errChildExited, s.socket, s.cmd.ProcessState, s.Diagnostics())
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %s within %s (state %v)\nstderr:\n%s",
				errChildSilent, s.socket, StartDeadline, state, s.Diagnostics())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Stop sends SIGTERM and waits for the child to exit.
//
// SIGTERM rather than a kill, because a kill would leave the store to be
// recovered on the next open and the socket file behind it. What a test wants
// between two of its own phases is the state a real, cleanly stopped server
// leaves: drained, its write-ahead log folded, its socket gone
// (SPEC/GRAPH.md § Server Shutdown and the Drain). A test whose SUBJECT is the
// kill drives one itself.
//
// It is idempotent, so a test may stop a server in the middle of its body and
// still register this as its cleanup.
func (s *Server) Stop() error {
	if s.waited {
		return nil
	}
	if s.cmd.Process == nil {
		s.waited = true
		return nil
	}
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("graphserver: signalling the child server: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		s.waited = true
		if err != nil {
			return fmt.Errorf("graphserver: the child server exited with %w\nstderr:\n%s", err, s.Diagnostics())
		}
		return nil
	case <-time.After(StopDeadline):
		_ = s.cmd.Process.Kill() //nolint:errcheck // already failing; the kill is to stop the run leaking a process
		<-done
		s.waited = true
		return fmt.Errorf("%w: it was killed after %s\nstderr:\n%s",
			errChildLingered, StopDeadline, s.Diagnostics())
	}
}
