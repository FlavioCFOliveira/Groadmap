//go:build unix

// Package terminal — Unix half of the terminal check.
//
// The contract, and the reasons behind it, live in terminal.go; this file
// supplies only the system call that differs per platform. The Windows half is
// in terminal_windows.go and MUST honour the same contract: true for an
// interactive terminal alone, false for every other kind of file, and an answer
// that comes from the descriptor without reading, consuming, or blocking on the
// stream.
package terminal

import (
	"os"

	"golang.org/x/sys/unix"
)

// isTerminal asks the kernel for the window size of f with the TIOCGWINSZ
// ioctl. Only the terminal driver implements that request, so the call succeeds
// exactly when f is a terminal and fails with ENOTTY ("inappropriate ioctl for
// device") for everything else — a pipe, a regular file, a socket, /dev/null,
// /dev/zero. Nothing is done with the window size; only whether the call is
// answered matters.
//
// TIOCGWINSZ rather than the termios read that isatty(3) classically performs:
// the request constant is spelled TIOCGWINSZ on every Unix golang.org/x/sys
// supports, while the termios read is TCGETS on Linux and TIOCGETA on the BSDs
// and macOS. Choosing the uniformly named request keeps this to one file for the
// whole Unix family instead of one per libc convention, and both requests answer
// the same question — they are the same ENOTTY test on the same driver.
//
// The ioctl inspects the open file description, so it neither reads nor consumes
// a byte of the stream and cannot block.
func isTerminal(f *os.File) bool {
	_, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	return err == nil
}
