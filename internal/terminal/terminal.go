// Package terminal answers one question about an already-open file: is it an
// interactive terminal, meaning a device a person types into?
//
// Why Groadmap needs to ask. A command that falls back to standard input when a
// flag is absent MUST refuse a terminal WITHOUT reading it. Reading it is the
// defect: the process waits for a query nobody is going to type, prints nothing,
// and never returns. That failure was observed rather than imagined — an
// invocation of `rmp graph query` that omitted --query, with a terminal on
// standard input, hung for roughly forty minutes before it was killed
// (SPEC/GRAPH.md § Standard Input That Supplies No Query). The refusal is part
// of the contract, not a remark about how fast a check happens to be, so the
// answer must come from the descriptor and never from the stream.
//
// Why not os.File.Stat and os.ModeCharDevice, which needs no system call of its
// own: that bit says "character device", and a terminal is only one kind. Both
// os.DevNull and /dev/zero are character devices too, so the bit cannot tell an
// interactive terminal from either. Answering with it would make
// `rmp graph query < /dev/zero` report "no query supplied" (exit code 2) about a
// source that does supply bytes — a megabyte of them per millisecond, which the
// maximum query length refuses with exit code 6. The two verdicts are
// deliberately distinct (SPEC/GRAPH.md § Standard Input That Supplies No Query),
// so the check that chooses between them has to be exact.
//
// The implementation is one system call per platform, in terminal_unix.go and
// terminal_windows.go. Both interrogate the descriptor alone: no byte is read,
// nothing is consumed from the stream, and neither call can block.
package terminal

import "os"

// IsTerminal reports whether f is an interactive terminal.
//
// It returns false for every other kind of file — a pipe, a regular file, a
// socket, and a non-interactive character device such as os.DevNull — and for a
// nil or already-closed f, whose descriptor no system call can answer for. False
// is the safe answer in all of those cases: it lets the caller read the stream,
// which for a non-terminal ends at end of stream instead of waiting forever.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return isTerminal(f)
}
