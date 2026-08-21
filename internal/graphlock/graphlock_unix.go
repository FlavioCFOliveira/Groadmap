//go:build unix

// Package graphlock — Unix half of the graph store lock primitive.
//
// The lock contract, the lock-file path, the handle lifetime, the retry policy,
// and the error mapping all live in graphlock.go; this file supplies only the
// system calls that differ per platform. The Windows half is in
// graphlock_windows.go and MUST honour the same contract: an exclusive mode and
// a shared mode that are mutually exclusive with each other, shared holders
// that do not exclude one another, and BOTH modes failing immediately on
// contention rather than waiting — the bounded wait a reader performs is the
// retry loop in graphlock.go, not a blocking system call.
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

// lockSharedNB takes a shared, non-blocking advisory lock on the whole file
// with flock(2). LOCK_SH conflicts with LOCK_EX but not with another LOCK_SH,
// which is exactly the reader/writer exclusion the graph store needs: readers
// run concurrently with one another and only a writer's exclusive hold shuts
// them out.
//
// LOCK_NB is not optional here either, even though a reader is allowed to wait:
// the wait must be the bounded retry loop in AcquireShared, so that it can end
// in a diagnosed failure. Dropping LOCK_NB would turn it into an unbounded
// kernel block, which SPEC/GRAPH.md § Lock Contention rule 2 forbids because one
// of the two readers is an HTTP request handler.
func lockSharedNB(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
}

// unlockFile releases the lock taken in either mode.
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
