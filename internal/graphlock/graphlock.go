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
// There is exactly ONE lock file, `write.lock`, and SPEC/GRAPH.md § Concurrency
// and Recovery now gives it exactly ONE mode:
//
//   - AcquireExclusive — for every `rmp graph execute` invocation. Held across
//     the whole open, execution, commit, checkpoint, and write-ahead-log
//     truncation sequence. An invocation that finds it held WAITS, under the
//     project's bounded backoff policy, and fails only once that wait is
//     exhausted.
//
// One mode, because there is one execution path. Groadmap does not examine a
// statement, so it cannot learn from it whether the statement will write, and a
// lock mode chosen on that guess would be a shared lock held while a statement
// committed. The exclusive mode is the mode that is correct for every statement.
// The cost is stated rather than hidden: two statements against the same roadmap
// serialise even when neither of them writes.
//
// The wait is not a courtesy either. Every caller is now a possible reader, so a
// policy that failed on the first collision would make ordinary statements
// intermittently unavailable, and one of the two callers is an HTTP request
// handler for which an unbounded block would hang a GET until the server's write
// timeout fired (SPEC/GRAPH.md § Lock Contention).
//
// There is no second mode. A shared hold existed while `graph query` and
// `graph search` had a read path of their own, and it outlived them by one task
// as internal/web's only remaining caller; the web graph data endpoint now runs
// its statement on the transactional path and takes the exclusive hold across it
// (rmp task #364), so the shared mode has no caller and is gone. Reintroducing
// one would be a shared lock held while a statement committed, which is the
// lost-write corruption AcquireExclusive documents.
//
// The package exists as its own package rather than inside internal/commands
// because both graph callers need it and they sit on opposite sides of an
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
// What this file does NOT own is the bounded wait. That is the project's single
// retry policy; it lives in internal/backoff and this package consumes it. It
// used to be restated here, in constants and prose of this package's own, which
// is how two other copies of the same policy drifted unnoticed (task #294).
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

// AcquireExclusive takes the graph store's advisory lock exclusively for the
// duration of one `rmp graph execute` invocation, waiting a bounded time for a
// current holder, and returns a closure that releases it.
//
// The caller MUST hold it across the whole open -> execute -> commit ->
// checkpoint -> WAL-truncate sequence. Two invocations must NOT interleave those
// sequences: a second one that loaded the graph before the first's commit would,
// on checkpoint, write a FULL snapshot of its own (stale) in-memory graph —
// missing the first's committed change — and then truncate the write-ahead log
// that still held it, silently losing an acknowledged write. Because the
// sequence that must not interleave is wider than a transaction, no
// engine-level writer exclusion would have covered it.
//
// Per SPEC/GRAPH.md § Lock Contention rule 1 an invocation that finds the lock
// held does NOT fail on the first collision and does NOT block indefinitely: it
// retries under the project's bounded backoff policy (internal/backoff,
// specified by SPEC/IMPLEMENTATION.md § Graph Store Concurrency rule 4, which
// reuses the SQLite policy verbatim) and only then fails with utils.ErrDatabase.
// Callers map that to exit code 1 for the CLI and HTTP 500 for the web graph
// data endpoint.
//
// It used to fail on the first collision, and the asymmetry that justified it —
// a rare writer against frequent readers that waited — is gone: the read mode
// went with the five subcommands, so every caller is now a possible reader and
// failing one fast would make ordinary statements intermittently unavailable.
// The wait is sized against a holder whose critical section spans a full
// snapshot rewrite AND the execution of a statement whose cost the caller
// chooses, and its worst case stays a small fraction of the web server's 30 s
// write timeout, spent before the statement starts rather than inside the graph
// data endpoint's own query budget (SPEC/WEB.md § Graph Query Time Budget).
//
// The operating system releases the lock when the holding process exits, so an
// invocation that crashes does not strand it.
func AcquireExclusive(graphDir string) (func(), error) {
	f, err := openLockFile(graphDir)
	if err != nil {
		return nil, err
	}

	// The lock is taken NON-BLOCKING on every attempt, so the wait is
	// backoff.Retry's — bounded, observable, and identical to the wait the
	// SQLite layer performs — rather than the kernel's, which is unbounded.
	// Every failure of lockExclusiveNB is contention, so every one is retried.
	release, err := backoff.Retry(func() (func(), error) {
		if lockErr := lockExclusiveNB(f); lockErr != nil {
			return nil, lockErr
		}
		return releaseFunc(f), nil
	}, backoff.Always)
	if err != nil {
		// Close before returning: the handle must not leak on the contention
		// path.
		_ = f.Close()
		return nil, fmt.Errorf("%w: graph store is busy: another invocation still holds it after the bounded wait", utils.ErrDatabase)
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
