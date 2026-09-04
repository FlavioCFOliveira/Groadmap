package signals

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This file is the regression gate for the defect rmp task #388 fixed: a
// long-lived process killed by the very signal it was meant to drain on.
//
// # Why it needs child processes
//
// The property is about an INTERVAL between two registrations, and about what
// the operating system does to a process during it. Neither is observable from
// inside the process: a test that sends itself a signal observes only the
// handler that happened to be installed, and the fatal outcome — death by
// SIGTERM under the default disposition — ends the process that would have
// reported it. The exit status of a child is the only place that outcome is
// legible, so the test binary re-executes itself and reads it.
//
// # The three tests, and what each one can and cannot fail on
//
//  1. TestTheUnownedIntervalIsFatalAndThisHarnessSeesIt runs a child built the
//     way both surfaces were built before the fix — main's handler, then
//     signal.Reset followed by signal.Notify — with the interval between the
//     two WIDENED to a fixed sleep, and with the announcement moved inside it
//     so that the parent signals precisely when the child is unprotected. A
//     signal arriving there must kill the child outright. This is the control:
//     it proves the harness observes exactly the failure the fix removes, so
//     that test 2 passing means something. Widening and announcing from inside
//     make it deterministic; at its natural width the same child dies about one
//     run in twenty and, more often, has the signal dropped instead, which is a
//     control that sometimes says nothing and sometimes says something else.
//
//  2. TestATakeOverIsNeverUnowned runs the same shape through this package,
//     with the same widened delay in the same place, and requires exit 0.
//     Together with test 1 the pair isolates the change: same timing, same
//     signal, opposite outcome, and the only difference is that one of them
//     unregisters and the other does not.
//
//  3. TestASignalAtTheAnnouncementAlwaysReachesTheOwner runs many children at
//     NATURAL timing — signalled the instant the announcement is read, which is
//     what a supervisor does — and requires every one of them to drain. It is
//     the test that would have caught the defect: at HEAD the same shape fails
//     about a quarter of the time, either killed or with the signal dropped
//     entirely.
//
// # What test 3 asserts, and why it cannot flake in the other direction
//
// It asserts only that nothing goes wrong. A child that is killed, that exits
// 130, that hangs, or that reports it never saw the signal is a failure; there
// is no assertion that a failure MUST occur, so a machine on which the timing
// never lands badly does not turn the test red. That direction is covered by
// test 1, which does not depend on timing at all.

const (
	// childRoleEnv turns a run of this test binary into one of the child
	// programs below instead of a test run.
	childRoleEnv = "GROADMAP_SIGNALS_TEST_ROLE"

	// announcement is what a child prints when it is ready to be signalled. It
	// stands for `rmp graph serve`'s socket line and `rmp web`'s URL: the
	// moment a caller learns the process exists.
	announcement = "ARMED"

	// defaultActionExitCode mirrors cmd/rmp's ExitSigint. A child that exits
	// with it was stopped by the default disposition rather than by the owner,
	// which for a surface that has announced itself is a failure — a smaller
	// one than being killed, and still wrong.
	defaultActionExitCode = 130

	// noSignalExitCode is what a child reports when it waited and nothing
	// arrived, which separates a dropped signal from a mishandled one.
	noSignalExitCode = 3

	// widenedInterval is how long the control child stays inside the interval
	// the fix removes. It is orders of magnitude wider than the real one — 41
	// microseconds at the median on the machine this was measured on — so that
	// the control cannot miss.
	widenedInterval = 50 * time.Millisecond

	// childPatience bounds how long a child waits for the signal the parent
	// always sends. Reaching it means the signal was delivered to nothing.
	childPatience = 30 * time.Second

	// parentPatience bounds how long the parent waits for a signalled child to
	// exit. A child that outlasts it is recorded as hung, which is one of the
	// two failures at HEAD.
	parentPatience = 30 * time.Second
)

// raceExitSleep switches off ThreadSanitizer's exit delay in the CHILDREN.
//
// A race-instrumented binary sleeps one second in its main goroutine before
// exiting, so that a race in a goroutine still running at that point still gets
// reported. Multiplied by the number of children this file starts it dominates
// the run: 150 children cost 153 seconds under -race and 0.4 seconds without
// it, which is not a cost a package-level test may impose on `go test -race
// ./...`. With the delay switched off the same 150 cost about two seconds.
//
// What it gives up, stated rather than assumed: a race that only materialises
// AFTER a child's main goroutine has returned is not reported. The children are
// three-statement programs, and the concurrency this package actually has — the
// dispatcher goroutine racing an owner swap — is exercised fully instrumented
// by the in-process tests in signals_test.go, in the parent, where no delay is
// touched. Races during a child's run are reported exactly as before; only the
// window after it has finished is given up.
//
// exec.Cmd keeps the LAST occurrence of a duplicated environment key, so this
// overrides a GORACE the developer has set, and is inert for a build that is
// not race-instrumented.
const raceExitSleep = "GORACE=atexit_sleep_ms=0"

func TestMain(m *testing.M) {
	switch os.Getenv(childRoleEnv) {
	case "":
		os.Exit(m.Run())
	case "takeover":
		os.Exit(childTakeOver(0))
	case "takeover-widened":
		os.Exit(childTakeOver(widenedInterval))
	case "reset-widened":
		os.Exit(childResetWidened(widenedInterval))
	default:
		fmt.Fprintf(os.Stderr, "unknown child role %q\n", os.Getenv(childRoleEnv))
		os.Exit(2)
	}
}

// childTakeOver is the shape both surfaces now have: the process installs the
// default action, takes the signals over, and only then announces itself.
//
// delay sits between the announcement and the receive, standing for everything
// a surface does after announcing and before it is ready to act on a signal —
// `rmp web`'s browser launch, `rmp graph serve`'s entry into its accept loop.
// The signal must survive it, which is what the buffered owner channel is for.
func childTakeOver(delay time.Duration) int {
	Install(func(os.Signal) { os.Exit(defaultActionExitCode) })

	sigCh, release := TakeOver()
	defer release()

	fmt.Println(announcement)

	if delay > 0 {
		time.Sleep(delay)
	}

	select {
	case <-sigCh:
		return 0
	case <-time.After(childPatience):
		return noSignalExitCode
	}
}

// childResetWidened reproduces, deliberately, the code this package replaced:
// cmd/rmp/main.go's handler and then the three lines that stood at
// internal/graphserve/graphserve.go:979-981 and internal/web/server.go:200-202.
//
// Two departures from what shipped, both of them about OBSERVING the interval
// rather than changing it:
//
//   - a sleep between Reset and Notify widens it. Reset has already restored
//     the default disposition when the sleep begins, so a signal arriving
//     during it meets exactly the disposition it met at the natural width of
//     41 microseconds; only the odds of arriving there change.
//   - the announcement is written from INSIDE the interval, so the parent
//     signals a child that is provably unprotected. At HEAD the announcement
//     came first, which is what made the failure a race: a signal that arrived
//     before Reset was queued, and then dropped by Reset's unregistration, and
//     the child hung instead of dying. Both outcomes are the same defect and
//     neither is a drain; the control picks the one that is deterministic.
//
// This is the module's one deliberate use of signal.Reset, and it is here so
// that the gate in internal/testenv — which forbids os/signal outside this
// package's production file — is guarding something a test can still
// demonstrate is worth guarding.
func childResetWidened(gap time.Duration) int {
	mainCh := make(chan os.Signal, 1)
	signal.Notify(mainCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-mainCh
		os.Exit(defaultActionExitCode)
	}()

	signal.Reset(syscall.SIGINT, syscall.SIGTERM)

	fmt.Println(announcement)
	time.Sleep(gap)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
		return 0
	case <-time.After(childPatience):
		return noSignalExitCode
	}
}

// outcome is how one child ended.
type outcome struct {
	code     int  // the exit status, when the child exited on its own
	killedBy int  // the signal number, when the operating system ended it
	hung     bool // the child outlasted parentPatience and was killed by the parent
}

func (o outcome) String() string {
	switch {
	case o.hung:
		return "HUNG: the signal reached no handler and the process never stopped"
	case o.killedBy != 0:
		return fmt.Sprintf("KILLED by signal %d: the default disposition was in force", o.killedBy)
	case o.code == defaultActionExitCode:
		return "exit 130: the default action ran, so the surface did not own the signal"
	case o.code == noSignalExitCode:
		return "exit 3: the child waited and no signal ever arrived"
	default:
		return fmt.Sprintf("exit %d", o.code)
	}
}

func (o outcome) drained() bool { return !o.hung && o.killedBy == 0 && o.code == 0 }

// runChild starts one child in the given role, waits for its announcement,
// sends it SIGTERM at once, and reports how it ended.
func runChild(t *testing.T, role string) outcome {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary to re-execute: %v", err)
	}

	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), childRoleEnv+"="+role, raceExitSleep)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("opening the child's stdout: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the child: %v", err)
	}

	line, readErr := bufio.NewReader(stdout).ReadString('\n')
	if readErr != nil || strings.TrimSpace(line) != announcement {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("the %s child announced %q (err %v), want %q", role, line, readErr, announcement)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM to the %s child: %v", role, err)
	}

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	select {
	case err := <-waited:
		return classify(t, err)
	case <-time.After(parentPatience):
		_ = cmd.Process.Kill()
		<-waited
		return outcome{hung: true}
	}
}

// classify turns what exec reports into the three outcomes that matter here,
// keeping death by signal distinct from an exit status: they are different
// failures with different causes and the test messages must not conflate them.
func classify(t *testing.T, err error) outcome {
	t.Helper()

	if err == nil {
		return outcome{code: 0}
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("waiting for the child: %v", err)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("the child's wait status is not a syscall.WaitStatus: %T", exitErr.Sys())
	}
	if status.Signaled() {
		return outcome{killedBy: int(status.Signal())}
	}
	return outcome{code: status.ExitStatus()}
}

func TestTheUnownedIntervalIsFatalAndThisHarnessSeesIt(t *testing.T) {
	got := runChild(t, "reset-widened")

	if got.killedBy != int(syscall.SIGTERM) {
		t.Fatalf("the control child ended as %s; it must be killed by SIGTERM.\n"+
			"The control reproduces the shape that shipped before rmp task #388: "+
			"signal.Reset restores the DEFAULT disposition, so a signal arriving "+
			"before the following signal.Notify terminates the process. If this "+
			"child now survives, the harness can no longer observe the failure "+
			"the next test claims does not happen, and that test has become "+
			"vacuous.", got)
	}
}

func TestATakeOverIsNeverUnowned(t *testing.T) {
	got := runChild(t, "takeover-widened")

	if !got.drained() {
		t.Fatalf("a child that took the signals over before announcing ended as %s, want exit 0.\n"+
			"Same timing and same signal as the control above, which is killed; "+
			"the difference is that this one never unregisters, so there is no "+
			"instant with the default disposition.", got)
	}
}

func TestASignalAtTheAnnouncementAlwaysReachesTheOwner(t *testing.T) {
	// The count is what makes "fixed" distinguishable from "less frequent".
	// The shape this replaces mishandled a signal at the announcement about a
	// quarter of the time, so a run of this length that finds nothing puts the
	// residual rate far below anything the fifteen-run reproduction could see.
	const runs = 150

	for i := 0; i < runs; i++ {
		if got := runChild(t, "takeover"); !got.drained() {
			t.Fatalf("run %d of %d ended as %s, want exit 0.\n"+
				"A signal delivered the instant a surface announces itself must "+
				"reach the surface, every time: that is what rmp task #388 fixed "+
				"and what SPEC/GRAPH.md § Server Shutdown and the Drain assumes.",
				i+1, runs, got)
		}
	}
}
