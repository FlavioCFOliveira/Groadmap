// Package aihelp — invocation sentinel.
//
// This file exposes a tiny package-private predicate that records
// whether the AI Agent Contract was emitted during the current process
// lifetime. The sentinel is consumed by the AI-agent-hint emitters
// added in the AI-help sprint sequence (tasks #5 and #6 in the sprint
// at the time of writing):
//
//   - Task #5 (env var AI_AGENT=1 hint): the hint is suppressed when
//     the invocation already emitted the contract.
//   - Task #6 (error-path trailing hint): same suppression.
//
// SPEC/COMMANDS.md § AI Help "Discoverability requirements" rules 2
// and 3 require that the hint be suppressed for the four contract-
// emitter invocation forms (`rmp --ai-help`, `rmp ai-help`,
// `rmp <command> --ai-help`, `rmp <command> <subcommand> --ai-help`).
// Rather than re-walk os.Args from the hint emitters (which would
// duplicate the parsing logic that lives in cmd/rmp/main.go), this
// sentinel is set once by either the runtime wiring or directly by
// Generate, and the hint emitters read it.
//
// The state is intentionally process-global (a package-level bool):
//   - It is set monotonically (false → true) and never reset, so there
//     is no read/write race that needs synchronisation: a writer that
//     sets `true` is observed by any later reader without ordering
//     guarantees beyond the program-order memory model that Go offers
//     for sequential code in main.
//   - The CLI is single-threaded for the duration of contract
//     generation — no goroutines fan-out from the contract emitter —
//     so atomic operations would add bookkeeping without buying
//     correctness.
//
// If a future caller needs to test multiple invocations in one process
// (e.g. fuzz tests, REPL embedding), `ResetForTesting` is provided to
// clear the flag between calls. It is exported only to the testing
// package via the standard "ForTesting" naming convention; production
// code MUST NOT call it.

package aihelp

// invoked records whether Generate has been called at least once in
// this process. See the package doc for the rationale behind the
// lock-free design.
var invoked bool

// markInvoked sets the invocation sentinel. Called by Generate and may
// also be called by the runtime wiring before delegating to Generate
// (e.g. when the wiring needs to suppress hints written by earlier
// stderr emitters before the JSON is produced).
//
// Idempotent: repeated calls are a no-op past the first.
func markInvoked() { invoked = true }

// WasInvoked reports whether the AI Agent Contract has been emitted
// during this process invocation. Consumed by the AI-agent-hint
// emitters added in tasks #5 and #6 of the AI-help sprint sequence to
// suppress the contract-pointer hint when the agent is already
// receiving the contract.
//
// The result is false at process start and transitions to true exactly
// once, on the first call to Generate. It never transitions back to
// false in production code.
func WasInvoked() bool { return invoked }

// ResetForTesting clears the invocation sentinel. Intended only for
// unit tests that need to verify the false→true transition multiple
// times in one process. Production code MUST NOT call this function.
func ResetForTesting() { invoked = false }
