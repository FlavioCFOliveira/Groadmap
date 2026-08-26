// Package graphlock owns Groadmap's advisory lock over a roadmap's GoGraph
// store directory (~/.roadmaps/<name>/graph/).
//
// Why the lock exists at all. Opening the store is NOT a read-only operation on
// disk. GoGraph's recovery repairs an interrupted checkpoint before it loads
// anything: it removes a stale `snapshot.tmp` staging directory
// unconditionally, and, when the live `snapshot/` directory carries no manifest
// while `snapshot.bak/` does, it promotes the backup by renaming `snapshot.bak`
// to `snapshot` and making that rename durable. Both actions repair the very
// directory a writer's checkpoint publishes into, so an unlocked read could
// delete the staging directory a concurrent writer was assembling its snapshot
// in, or interleave with that writer's publish sequence.
//
// There is exactly ONE lock file, `write.lock`, and it is taken in two modes
// (SPEC/GRAPH.md § Concurrency and Recovery):
//
//   - AcquireExclusive — for a write invocation. Held across the whole open,
//     commit, checkpoint, and write-ahead-log truncation sequence. Non-blocking:
//     a writer that finds the lock held fails at once rather than waiting.
//   - AcquireShared — for a read invocation. Held across the STORE OPEN ALONE
//     and released as soon as the open returns; the query then runs against the
//     fully materialised in-memory graph with no lock held. Readers do not
//     exclude one another, and a reader that collides with a writer waits under
//     a bounded backoff before failing.
//
// The narrowness of the reader's hold is deliberate and load-bearing, not an
// oversight: every on-disk action a read performs happens inside the open, so
// holding the lock past it would protect nothing while blocking writers for as
// long as the query takes. SPEC/GRAPH.md § Concurrency and Recovery states the
// anti-widening clause that governs it — a change that makes any part of a read
// touch the store AFTER the open (a lazily loaded component, a memory-mapped
// snapshot, a handle kept open past recovery, or a re-read during iteration)
// invalidates that reasoning and REQUIRES the hold to be widened.
//
// The package exists as its own package rather than inside internal/commands
// because both graph readers need it and they sit on opposite sides of an
// import edge: internal/commands imports internal/web, so internal/web cannot
// import internal/commands back. Duplicating the primitive would put two
// implementations of the same lock file in the binary.
//
// This file owns the whole contract — the lock-file path, the file handle's
// lifetime, the error mapping, and the release closure. Only the lock and
// unlock system calls are platform-specific, because no single call provides
// them everywhere; they live in graphlock_unix.go (flock(2)) and
// graphlock_windows.go (LockFileEx / UnlockFileEx).
//
// What this file does NOT own is the bounded wait a reader performs. That is
// the project's single retry policy; it lives in internal/backoff and this
// package consumes it. It used to be restated here, in constants and prose of
// this package's own, which is how two other copies of the same policy drifted
// unnoticed (task #294).
package graphlock

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// LockFileName is the basename of the advisory lock file inside a roadmap's
// graph directory. GoGraph knows nothing about it: Groadmap creates and
// maintains it, its contents are never read or written, and only the advisory
// lock on it carries meaning (SPEC/GRAPH.md § Persistence Layout, rule 5).
const LockFileName = "write.lock"

// AcquireExclusive takes an exclusive, non-blocking advisory lock on the graph
// store for the duration of a write, and returns a closure that releases it.
//
// Two concurrent `rmp graph` writers must NOT interleave their
// open -> commit -> checkpoint -> WAL-truncate sequences: a second writer that
// loaded the graph before the first's commit would, on checkpoint, write a FULL
// snapshot of its own (stale) in-memory graph — missing the first writer's
// committed change — and then truncate the write-ahead log that still held it,
// silently losing an acknowledged write. Because the sequence that must not
// interleave is wider than a transaction, no engine-level writer exclusion
// would have covered it.
//
// Per SPEC/GRAPH.md § Lock Contention rule 1 a writer does not wait: it fails on
// the first collision, with any holder, and the failure surfaces as
// utils.ErrDatabase (exit 1) rather than corrupting the store or hanging. The
// operating system releases the lock when the holding process exits, so an
// invocation that crashes does not strand it.
func AcquireExclusive(graphDir string) (func(), error) {
	f, err := openLockFile(graphDir)
	if err != nil {
		return nil, err
	}
	if lockErr := lockExclusiveNB(f); lockErr != nil {
		// Close before returning: the handle must not leak on the contention
		// path, which is the path taken most often.
		_ = f.Close()
		return nil, fmt.Errorf("%w: graph store is busy: a concurrent write is in progress", utils.ErrDatabase)
	}
	return releaseFunc(f), nil
}

// AcquireShared takes a shared advisory lock on the graph store, waiting a
// bounded time for a conflicting writer, and returns a closure that releases it.
//
// The caller MUST hold it across the store open ALONE and release it as soon as
// the open returns — not with defer, and never across the query. See the
// package comment for why that narrowness is load-bearing.
//
// Several readers may hold the shared lock at the same time, so reads never
// serialise against one another; only a writer's exclusive hold conflicts. Per
// SPEC/GRAPH.md § Lock Contention rule 2 a read does not fail on the first
// collision and does not block indefinitely: it retries under the project's
// bounded backoff policy (internal/backoff, specified by
// SPEC/IMPLEMENTATION.md § Graph Store Concurrency rule 4, which reuses the
// SQLite policy verbatim) and then fails with utils.ErrDatabase. Callers map
// that to exit code 1 for the CLI and HTTP 500 for the web graph data endpoint.
//
// A read waits where a write fails at once because the two are not symmetrical.
// What a read waits for is a writer's hold, which spans a whole checkpoint; and
// reads are by far the more frequent operation, so failing one on the first
// collision would make ordinary reads intermittently unavailable. The wait is
// therefore sized against the WRITER's hold, whose cost grows with the live
// graph size, not against the reader's own — which is why narrowing the reader's
// hold does not narrow the wait a reader may face. Its worst case stays a small
// fraction of the web server's 30 s write timeout, and is spent before the query
// starts rather than inside the graph data endpoint's own query budget
// (SPEC/WEB.md § Graph Query Time Budget).
func AcquireShared(graphDir string) (func(), error) {
	f, err := openLockFile(graphDir)
	if err != nil {
		return nil, err
	}

	// The lock is taken NON-BLOCKING on every attempt, so the wait is
	// backoff.Retry's — bounded, observable, and identical to the wait the
	// SQLite layer performs — rather than the kernel's, which is unbounded.
	// Every failure of lockSharedNB is contention, so every one is retried.
	release, err := backoff.Retry(func() (func(), error) {
		if lockErr := lockSharedNB(f); lockErr != nil {
			return nil, lockErr
		}
		return releaseFunc(f), nil
	}, backoff.Always)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%w: graph store is busy: a concurrent write is still in progress after the bounded wait", utils.ErrDatabase)
	}
	return release, nil
}

// openLockFile opens (creating on first use) the lock file inside graphDir.
//
// It creates the file but never the directory: a caller that must not
// materialise a graph store — the web interface — relies on that, and calls
// this only for a graph directory it has already found to exist
// (SPEC/WEB.md § Security and Constraints, rule 4).
func openLockFile(graphDir string) (*os.File, error) {
	lockPath := filepath.Join(graphDir, LockFileName)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600) // #nosec G304 -- lockPath is derived from a validated roadmap name under ~/.roadmaps
	if err != nil {
		return nil, fmt.Errorf("%w: opening graph store lock: %v", utils.ErrDatabase, err)
	}
	return f, nil
}

// releaseFunc returns the closure that drops the lock and closes the handle.
// Closing alone would release the lock on both platforms; the explicit unlock
// keeps the release intentional and symmetric with the acquisition.
func releaseFunc(f *os.File) func() {
	return func() {
		_ = unlockFile(f)
		_ = f.Close()
	}
}
