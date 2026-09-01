//go:build unix

// Package graphlock — Unix half of the graph store lock primitive.
//
// The lock contract, the lock-file path, the handle lifetime and the error
// mapping all live in graphlock.go, and the bounded wait BOTH modes perform is
// the project-wide policy in internal/backoff; this file supplies only the
// system calls that differ per platform. The Windows half is in
// graphlock_windows.go and MUST honour the same contract: an exclusive mode and
// a shared mode that are mutually exclusive with each other, shared holders
// that do not exclude one another, and BOTH modes failing immediately on
// contention rather than waiting — the wait happens in Go, under
// internal/backoff, and never in a blocking system call.
package graphlock

import (
	"os"
	"syscall"
)

// lockExclusiveNB takes an exclusive, non-blocking advisory lock on the whole
// file with flock(2). LOCK_NB is what makes contention fail immediately
// (EWOULDBLOCK) instead of blocking the caller, as SPEC/GRAPH.md § Lock
// Contention rule 1 requires of a writer. The lock is held by the open file
// description, so the kernel drops it when the process exits, however it exits.
func lockExclusiveNB(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// unlockFile releases the lock lockExclusiveNB took.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
