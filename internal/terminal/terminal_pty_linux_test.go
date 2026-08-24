package terminal

import (
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// TestIsTerminalAcceptsAPseudoTerminal is the other half of
// TestIsTerminalRejectsEveryNonTerminal: without it, an implementation that
// returned false unconditionally would pass every other test in this package,
// and the refusal that depends on this answer would never fire.
//
// Both ends of the pseudo-terminal are checked. The slave is the faithful one —
// it is what a shell hands a process as standard input — while the master is the
// end a test can create, and the master's answer matters too because a Go test
// that only ever holds a master would otherwise prove nothing.
//
// The file is constrained to Linux by its name, because creating a pseudo-
// terminal without a C library is a platform-specific sequence and the helper
// this uses implements the Linux one. That is not a hole in the coverage: the
// end-to-end suite drives the compiled binary with a pseudo-terminal on standard
// input on whatever platform it runs, and asserts the refusal by wall-clock time.
func TestIsTerminalAcceptsAPseudoTerminal(t *testing.T) {
	master, slave, err := testenv.OpenPTY()
	if err != nil {
		t.Fatalf("opening a pseudo-terminal: %v", err)
	}
	defer func() { _ = slave.Close() }()
	defer func() { _ = master.Close() }()

	if !IsTerminal(slave) {
		t.Error("IsTerminal(pseudo-terminal slave) = false, want true: the slave " +
			"is exactly what a shell puts on a process's standard input")
	}
	if !IsTerminal(master) {
		t.Error("IsTerminal(pseudo-terminal master) = false, want true")
	}
}
