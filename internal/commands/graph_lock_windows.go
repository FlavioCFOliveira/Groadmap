//go:build windows

// Package commands — Windows half of the graph write-lock primitive.
//
// The lock contract, the lock-file path, the handle lifetime, and the error
// mapping all live in acquireGraphWriteLock (graph.go); this file supplies
// only the two system calls that differ per platform. The Unix half is in
// graph_lock_unix.go and honours the same contract: exclusive, and failing
// immediately on contention instead of waiting.
//
// Windows has no flock(2). The equivalent is a byte-range lock taken with
// LockFileEx, which is exclusive per file handle: a second handle on the same
// file — in this process or another — collides with the first, which is the
// mutual exclusion the graph write path needs.
package commands

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

// lockGraphWriteFile takes an exclusive, non-blocking lock on the whole file
// with LockFileEx.
//
// LOCKFILE_EXCLUSIVE_LOCK requests a write lock rather than the default shared
// read lock. LOCKFILE_FAIL_IMMEDIATELY is not optional: LockFileEx blocks until
// the lock can be taken unless it is set, so omitting it would turn the fast
// failure that SPEC/GRAPH.md § Concurrency and Recovery requires into a hang.
// With it, a contended call returns ERROR_LOCK_VIOLATION at once and
// acquireGraphWriteLock reports utils.ErrDatabase (exit 1).
//
// The overlapped structure supplies the range's starting offset; a zero value
// means offset 0, so the lock starts at the beginning of the file. The lock is
// held by the file handle, so Windows releases it when the handle is closed —
// including when the process exits without closing it.
func lockGraphWriteFile(f *os.File) error {
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		lockReserved,
		lockRegionLow,
		lockRegionHigh,
		new(windows.Overlapped),
	)
}

// unlockGraphWriteFile releases the lock taken by lockGraphWriteFile. The byte
// range must match the locked range exactly, so it repeats the same offset and
// length.
func unlockGraphWriteFile(f *os.File) error {
	return windows.UnlockFileEx(
		windows.Handle(f.Fd()),
		lockReserved,
		lockRegionLow,
		lockRegionHigh,
		new(windows.Overlapped),
	)
}
