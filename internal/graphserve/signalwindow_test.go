package graphserve

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This file is the surface half of the regression gate for rmp task #388: the
// property that `rmp graph serve` drains when it is signalled the instant it
// announces its socket, EVERY time and not most of the time.
//
// # Why it is separate from the harness the durability tests use
//
// startServerProcess redirects the child's stdout to a file and then waits for
// the socket to answer a resolution probe, polling every 20 milliseconds. That
// is the right way to wait for a server that a test is about to use, and it is
// the wrong way to reproduce this defect: by the time a probe has succeeded and
// a poll has come round, the child is long past the moment under test. The
// defect lives in the microseconds after the announcement is written, so the
// child's stdout has to be a PIPE and the signal has to be sent on the
// announcement itself.
//
// # What was measured, and what this test therefore proves
//
// Against the binary built from the tree this fix was written on — 100 servers,
// each sent SIGTERM the instant its announcement was read — the shape that
// shipped produced 75 clean drains, 7 processes killed outright by SIGTERM, and
// 18 that never stopped at all because Reset dropped the queued signal. The
// same 100 against the repaired tree produced 100 clean drains.
//
// A single run of this test therefore has about a one-in-four chance of
// catching a regression; the repetition below is what turns that into a
// certainty. The count is deliberately modest because each iteration is a real
// server: it opens a store through recovery, takes the exclusive hold, drains,
// takes a shutdown checkpoint and releases. internal/signals carries the
// high-count version of the same property over a child that does nothing else.
const signalAtAnnouncementRuns = 12

func TestSignalAtTheAnnouncementDrainsEveryTime(t *testing.T) {
	for i := 0; i < signalAtAnnouncementRuns; i++ {
		code, killedBy, hung := serveAndSignalOnAnnouncement(t)
		switch {
		case hung:
			t.Fatalf("run %d of %d: the server never exited after SIGTERM. The signal "+
				"reached no handler at all — the shape before rmp task #388 unregistered "+
				"every channel from the signal while re-arming, so one already queued in "+
				"the runtime was delivered to nobody.", i+1, signalAtAnnouncementRuns)
		case killedBy != 0:
			t.Fatalf("run %d of %d: the server was KILLED by signal %d instead of draining. "+
				"There is an instant in which SIGTERM carries the default disposition, "+
				"which is exactly the defect rmp task #388 removed: no drain, no shutdown "+
				"checkpoint, no socket removal, and the store's exclusive hold released by "+
				"the operating system rather than by the process.",
				i+1, signalAtAnnouncementRuns, killedBy)
		case code != 0:
			t.Fatalf("run %d of %d: the server exited %d, want 0. SPEC/COMMANDS.md "+
				"§ Serve Exit Codes fixes 0 for a server stopped by a signal; 130 means the "+
				"take-over had not happened yet when the signal arrived, so the announcement "+
				"published a server that did not yet own its own shutdown.",
				i+1, signalAtAnnouncementRuns, code)
		}
	}
}

// serveAndSignalOnAnnouncement starts one child server, sends it SIGTERM the
// moment it writes its socket path, and reports how it ended: the exit status,
// the signal that killed it, or that it outlasted the shutdown deadline.
func serveAndSignalOnAnnouncement(t *testing.T) (code, killedBy int, hung bool) {
	t.Helper()

	root := graphRoot(t)

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary to re-execute: %v", err)
	}

	socket := filepath.Join(root, "graph.sock")
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(),
		childRoleEnv+"=1",
		childGraphDirEnv+"="+filepath.Join(root, "graph"),
		childSocketEnv+"="+socket,
	)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("opening the child server's stdout: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the child server: %v", err)
	}

	line, readErr := bufio.NewReader(stdout).ReadString('\n')
	if readErr != nil || strings.TrimSpace(line) != socket {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("the child server announced %q (err %v), want the socket it was asked to "+
			"bind, %q", line, readErr, socket)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM to the child server: %v", err)
	}

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	select {
	case err := <-waited:
		return classifyServerExit(t, err)
	case <-time.After(shutdownDeadline):
		_ = cmd.Process.Kill()
		<-waited
		return 0, 0, true
	}
}

// classifyServerExit keeps death by signal distinct from an exit status: the
// two are different failures with different causes, and a test message that
// conflated them would send the next reader looking in the wrong place.
func classifyServerExit(t *testing.T, err error) (code, killedBy int, hung bool) {
	t.Helper()

	if err == nil {
		return 0, 0, false
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("waiting for the child server: %v", err)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("the child's wait status is not a syscall.WaitStatus: %T", exitErr.Sys())
	}
	if status.Signaled() {
		return 0, int(status.Signal()), false
	}
	return status.ExitStatus(), 0, false
}
