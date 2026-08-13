//go:build unix

// Package commands — Unix half of the graph write-lock primitive.
//
// The lock contract, the lock-file path, the handle lifetime, and the error
// mapping all live in acquireGraphWriteLock (graph.go); this file supplies
// only the two system calls that differ per platform. The Windows half is in
// graph_lock_windows.go and MUST honour the same contract: exclusive, and
// failing immediately on contention instead of waiting.
package commands

import (
	"os"
	"syscall"
)

// lockGraphWriteFile takes an exclusive, non-blocking advisory lock on the
// whole file with flock(2). LOCK_NB is what makes contention fail immediately
// (EWOULDBLOCK) instead of blocking the caller, as SPEC/GRAPH.md § Concurrency
// and Recovery requires. The lock is held by the open file description, so the
// kernel drops it when the process exits, however it exits.
func lockGraphWriteFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// unlockGraphWriteFile releases the lock taken by lockGraphWriteFile.
func unlockGraphWriteFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
