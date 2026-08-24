//go:build windows

// Package terminal — Windows half of the terminal check.
//
// The contract, and the reasons behind it, live in terminal.go; this file
// supplies only the system call that differs per platform. The Unix half is in
// terminal_unix.go and honours the same contract: true for an interactive
// terminal alone, false for every other kind of file, and an answer that comes
// from the descriptor without reading, consuming, or blocking on the stream.
//
// Windows has no ioctl and no ENOTTY. The equivalent question is asked of the
// console subsystem, which owns the handles a console window hands to a process.
package terminal

import (
	"os"

	"golang.org/x/sys/windows"
)

// isTerminal asks the console subsystem for the mode of f's handle with
// GetConsoleMode. The call succeeds only for a handle that refers to a console
// input buffer or screen buffer — what a console window gives a process for
// standard input — and fails with ERROR_INVALID_HANDLE for a handle that refers
// to anything else: a pipe, a regular file, or the NUL device that os.DevNull
// names on this platform. Nothing is done with the mode; only whether the call
// is answered matters.
//
// Like its Unix counterpart the call interrogates the handle alone, so it
// neither reads nor consumes a byte of the stream and cannot block.
func isTerminal(f *os.File) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(f.Fd()), &mode) == nil
}
