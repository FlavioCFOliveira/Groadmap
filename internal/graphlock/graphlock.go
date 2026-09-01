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
// What this file does NOT own is the LOOP or the LADDER of the bounded wait.
// Those are the project's single retry policy; they live in internal/backoff and
// this package consumes them. They used to be restated here, in constants and
// prose of this package's own, which is how two other copies of the same policy
// drifted unnoticed (task #294).
//
// What this file DOES own is the BUDGET that loop runs against, and the division
// is the whole point. How long to wait for this lock is a question about how
// long this lock may be HELD, and that is a fact about the graph store rather
// than about retrying: the hold spans a statement whose cost the caller chooses,
// so the wait has to be sized against the longest statement that is lawful. Only
// this package is in a position to know that, so DefaultStatementBudget and
// WaitBudget live here and internal/backoff is handed a duration — never a loop,
// never a ladder, never a retry count (SPEC/GRAPH.md § Lock Contention;
// SPEC/IMPLEMENTATION.md § Graph Store Concurrency, "Write Contention and
// Recovery" rule 3).
//
// The statement budget lives here for the same reason this package does. It is
// SPEC/WEB.md § Graph Query Time Budget that fixes the figure, and the web graph
// data endpoint that applies it as a deadline, but a waiter has to know how long
// a hold may lawfully last and the waiter is not necessarily the web:
// internal/web and internal/commands both import this package, and this package
// can import neither back. Leaving the figure in internal/web would point the
// import edge the wrong way for the CLI, and copying it into both would be the
// drift of #294 with a different constant.
package graphlock

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// DefaultStatementBudget is the longest a caller-supplied statement may lawfully
// run while this lock is held: the deadline SPEC/WEB.md § Graph Query Time
// Budget rule 1 fixes, 5 seconds.
//
// It is a graph-store-wide quantity rather than a web-local one, per
// SPEC/GRAPH.md § Lock Contention. The web graph data endpoint is where it is
// applied, as that endpoint's own per-request deadline, but the reason it is
// declared HERE is that it also bounds the variable part of a hold, and the
// party that has to know how long a hold may last is the party waiting for it.
// Changing this value changes the wait every caller of this lock performs.
const DefaultStatementBudget = 5 * time.Second

// StatementBudget is the budget actually in force, and the value WaitBudget
// derives from. It is a var rather than a const for exactly one reason: tests
// move it — down, so that an exhaustion test need not spend the whole wait, and
// up, so that a deliberately slow statement is not cancelled mid-test.
//
// Production never reassigns it. It is initialised from DefaultStatementBudget
// and no flag, environment variable, request parameter, or any other
// user-facing knob reaches it, so every statement the server runs is bounded by
// the 5 seconds above (SPEC/WEB.md § Graph Query Time Budget, rules 1 and 8).
var StatementBudget = DefaultStatementBudget

// WaitBudget is how long AcquireExclusive waits for a current holder before it
// gives up:
//
//	wait budget = statement budget + backoff total
//
// A finite wait can be sized against a hold only when that hold has an upper
// bound, and this is that bound, term by term. StatementBudget covers the
// VARIABLE part of a hold, the statement the invocation carries. backoff.Total
// is the allowance for the FIXED part: the store open, the write-ahead-log open,
// the engine construction, the commit, and a full snapshot checkpoint with its
// log truncation, plus scheduling. That allowance is REUSED rather than replaced
// by a figure of its own, so the project keeps one set of timing numbers, and it
// is generous rather than tight — the fixed part measures 19.2 ms on a 252-node
// store and 22.7 ms under the race detector, against an allowance three orders
// of magnitude above it (SPEC/GRAPH.md § Lock Contention).
//
// It is a function and not a constant because StatementBudget is a var: a test
// that moves the budget must move the wait with it, or the two would describe
// different policies within one process. Exporting it also lets a test assert
// the DERIVATION rather than restate the 7.5 seconds it currently yields, which
// is the mistake that put a figure of this package's own beside the shared
// policy before #294.
//
// The invariant it has to satisfy is not about this quantity alone. A request
// that exhausts the wait must still have its 500 written, and a request that
// waits and then runs its statement to the end of its budget must still have its
// response written, so what must fit inside the web server's 30 s WriteTimeout
// is StatementBudget and WaitBudget TOGETHER: 12.5 s of 30 s at the budget in
// force.
func WaitBudget() time.Duration { return StatementBudget + backoff.Total() }

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
// retries on internal/backoff's loop and ladder for as long as WaitBudget
// allows, and only then fails with utils.ErrDatabase. Callers map that to exit
// code 1 for the CLI and HTTP 500 for the web graph data endpoint.
// SPEC/IMPLEMENTATION.md § Graph Store Concurrency, "Write Contention and
// Recovery" rule 3 is the governing rule, and it is deliberately not the SQLite
// policy wholesale: the loop and the ladder come from there, the total does not.
//
// It used to fail on the first collision, and the asymmetry that justified it —
// a rare writer against frequent readers that waited — is gone: the read mode
// went with the five subcommands, so every caller is now a possible reader and
// failing one fast would make ordinary statements intermittently unavailable.
// The wait is sized against a holder whose critical section spans a full
// snapshot rewrite AND the execution of a statement whose cost the caller
// chooses, which is why it is WaitBudget and not backoff.Total. A wait of
// 2500 ms put two of this project's own constants in a two-to-one ratio in the
// holder's favour, and the consequence was measured rather than feared: a holder
// whose statement ran 4.71 seconds, lawfully and inside its own budget, starved a
// contender that gave up after 2.5018 seconds. What must fit inside the web
// server's 30 s WriteTimeout is the statement and the wait together, 12.5 s of
// 30 s; a wait that is merely a small fraction of that timeout is neither
// necessary nor sufficient, and sizing on that property alone is exactly what
// admitted a wait shorter than the hold it had to cover (SPEC/GRAPH.md § Lock
// Contention). The wait is spent before the statement starts, so it does not
// come out of the graph data endpoint's own query budget (SPEC/WEB.md § Graph
// Query Time Budget).
//
// One hold carries no budget, and that residual is stated rather than hidden.
// `rmp graph execute` runs its statement under no time budget at all, so its
// hold has no lawful maximum and no finite wait can guarantee that a contender
// is served: a CLI statement expensive enough will exhaust whatever budget the
// waiter is given, and the waiter then fails exactly as rule 2 describes, with
// utils.ErrDatabase for the CLI and HTTP 500 for the web graph data endpoint.
// That outcome is the specified one and not corruption. Giving
// `rmp graph execute` a statement budget of its own is not specified and remains
// an open question (SPEC/GRAPH.md § Lock Contention).
//
// The operating system releases the lock when the holding process exits, so an
// invocation that crashes does not strand it.
func AcquireExclusive(graphDir string) (func(), error) {
	f, err := openLockFile(graphDir)
	if err != nil {
		return nil, err
	}

	// The lock is taken NON-BLOCKING on every attempt, so the wait is
	// backoff.RetryWithin's — bounded, observable, and climbing the very ladder
	// the SQLite layer climbs — rather than the kernel's, which is unbounded.
	// Only the BUDGET is this package's, because only this package knows how
	// long a holder of this lock may lawfully keep it. Every failure of
	// lockExclusiveNB is contention, so every one is retried.
	release, err := backoff.RetryWithin(WaitBudget(), func() (func(), error) {
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
