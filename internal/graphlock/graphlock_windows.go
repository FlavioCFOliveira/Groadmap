//go:build windows

// Package graphlock — Windows half of the graph store lock primitive.
//
// The lock contract, the lock-file path, the handle lifetime and the error
// mapping all live in graphlock.go, and the bounded wait a reader performs is
// the project-wide policy in internal/backoff; this file supplies only the
// system calls that differ per platform. The Unix half is in graphlock_unix.go
// and honours the same contract: an exclusive mode and a shared mode that are
// mutually exclusive with each other, shared holders that do not exclude one
// another, and BOTH modes failing immediately on contention rather than waiting
// — a reader waits in AcquireShared, not in a blocking system call.
//
// Windows has no flock(2). The equivalent is a byte-range lock taken with
// LockFileEx, which distinguishes a shared (read) lock from an exclusive
// (write) one per file handle: a second handle on the same file — in this
// process or another — collides with an exclusive holder, while two shared
// holders coexist. That is the mutual exclusion the graph store needs.
package graphlock

import (
	"os"

	"golang.org/x/sys/windows"
)

const (
	// lockRegionLow and lockRegionHigh are the low and high 32 bits of the
	// locked byte count. LockFileEx locks a byte range rather than a file, so
	// locking 0xFFFFFFFFFFFFFFFF bytes from offset 0 covers the whole file and
	// any length it could ever take. Ranges may be locked beyond end-of-file,
	// so this works on the empty lock file Groadmap creates.
	lockRegionLow  uint32 = 0xFFFFFFFF
	lockRegionHigh uint32 = 0xFFFFFFFF

	// lockReserved is the LockFileEx/UnlockFileEx "reserved" parameter, which
	// the API requires to be zero.
	lockReserved uint32 = 0
)

// lockExclusiveNB takes an exclusive, non-blocking lock on the whole file with
// LockFileEx.
//
// LOCKFILE_EXCLUSIVE_LOCK requests a write lock rather than the default shared
// read lock. LOCKFILE_FAIL_IMMEDIATELY is not optional: LockFileEx blocks until
// the lock can be taken unless it is set, so omitting it would turn the fast
// failure that SPEC/GRAPH.md § Lock Contention rule 1 requires into a hang. With
// it, a contended call returns ERROR_LOCK_VIOLATION at once and
// AcquireExclusive reports utils.ErrDatabase (exit 1).
//
// The overlapped structure supplies the range's starting offset; a zero value
// means offset 0, so the lock starts at the beginning of the file. The lock is
// held by the file handle, so Windows releases it when the handle is closed —
// including when the process exits without closing it.
func lockExclusiveNB(f *os.File) error {
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		lockReserved,
		lockRegionLow,
		lockRegionHigh,
		new(windows.Overlapped),
	)
}

// lockSharedNB takes a shared, non-blocking lock on the whole file with
// LockFileEx. Omitting LOCKFILE_EXCLUSIVE_LOCK is what makes the lock shared:
// it conflicts with an exclusive holder but not with another shared one, which
// is the reader/writer exclusion the graph store needs.
//
// LOCKFILE_FAIL_IMMEDIATELY is not optional here either, even though a reader is
// allowed to wait: the wait must be the bounded one AcquireShared performs, so
// that it can end in a diagnosed failure rather than an unbounded block that
// SPEC/GRAPH.md § Lock Contention rule 2 forbids.
func lockSharedNB(f *os.File) error {
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_FAIL_IMMEDIATELY,
		lockReserved,
		lockRegionLow,
		lockRegionHigh,
		new(windows.Overlapped),
	)
}

// unlockFile releases the lock taken in either mode. The byte range must match
// the locked range exactly, so it repeats the same offset and length.
func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(
		windows.Handle(f.Fd()),
		lockReserved,
		lockRegionLow,
		lockRegionHigh,
		new(windows.Overlapped),
	)
}
