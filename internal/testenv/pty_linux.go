// This file holds the module's one way of creating a pseudo-terminal for a
// test, so every package that has to prove something about an interactive
// terminal proves it against the same construction.
//
// Why it lives here rather than in each package that needs it: two packages need
// a real terminal on a descriptor — internal/terminal, which answers whether a
// file is one, and internal/commands, which must refuse a terminal on standard
// input WITHOUT reading it (SPEC/GRAPH.md § Standard Input That Supplies No
// Query) — and their tests are in different packages. A twenty-line ioctl
// sequence copied into both drifts, and the copy that ended up weaker would be
// the one whose proof meant least.
//
// Why a real pseudo-terminal rather than an interface a test can fake: the
// property under test is what the KERNEL says about a descriptor. A fake would
// prove only that the caller consults some function, which is the part that was
// never in doubt.

package testenv

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// OpenPTY creates a pseudo-terminal pair and returns both ends. Both are real
// terminals as far as the kernel is concerned, so either can stand in for the
// interactive terminal a command finds on standard input.
//
// The caller closes both, and MUST keep the master open for as long as it uses
// the slave: closing the master hangs up the line, after which the slave is no
// longer a usable terminal.
//
// The sequence is the Linux one, which is why this file is constrained to Linux
// by its name. Opening /dev/ptmx allocates a pair and yields the master;
// TIOCSPTLCK unlocks the slave, which the kernel keeps locked until the program
// says it is ready; and TIOCGPTN reports the slave's number, which names it
// under /dev/pts. O_NOCTTY on both opens keeps the test process from acquiring
// the pair as its controlling terminal, which would outlive the test.
func OpenPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("opening /dev/ptmx: %w", err)
	}

	if err = unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("unlocking the pseudo-terminal slave: %w", err)
	}

	number, err := unix.IoctlGetUint32(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("reading the pseudo-terminal slave number: %w", err)
	}

	slaveName := fmt.Sprintf("/dev/pts/%d", number)
	// #nosec G304 -- the path is /dev/pts/<n> where n comes from the TIOCGPTN
	// ioctl on a descriptor this function opened; no external input reaches it.
	slave, err = os.OpenFile(slaveName, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("opening %s: %w", slaveName, err)
	}

	return master, slave, nil
}
