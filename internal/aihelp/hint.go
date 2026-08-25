// Package aihelp — AI-agent discovery hint emitter (env-var + error path).
//
// This file implements the small, coordinated piece of plumbing that
// emits the SPEC-mandated "AI agents: ..." hint line to stderr in two
// situations:
//
//  1. AI_AGENT environment variable path (SPEC/HELP.md
//     § AI_AGENT environment variable): when AI_AGENT is exactly the
//     string "1", the hint is the FIRST line of stderr for the entire
//     invocation, followed by exactly one blank line, followed by any
//     other stderr content the run produces (errors, warnings).
//
//  2. Error-path trailing hint (SPEC/HELP.md § Error message format):
//     every "Error: ..." line written to stderr is followed by one
//     blank line and the hint, unless a suppression rule removes
//     them. The blank line and the hint are ONE unit: SPEC/HELP.md
//     § Stderr part order lists them together as part 4 ("One blank
//     line, then the AI-agent hint line"), so a suppressed hint takes
//     its separator with it.
//
// The two situations share text but must coordinate so that, in the
// invocation where BOTH would fire (AI_AGENT=1 + failure), the hint is
// emitted EXACTLY ONCE — at the top of stderr, per SPEC deduplication
// rule "When AI_AGENT=1 is active and the invocation fails, the
// env-var hint is emitted once at the top of stderr ... and the
// trailing error-path hint is suppressed".
//
// Design:
//
//   - A package-level sync.Once guards both emitters. After the first
//     successful Write, every subsequent call is a no-op. This is the
//     deduplication primitive: the wiring sites call an emitter
//     unconditionally (within their own SPEC gates), and the second
//     caller naturally elides its output.
//
//   - There are two emitters because the hint appears in two
//     positions, and each owns the separator its position needs.
//     EmitHintOnce writes the hint at the top of stderr (part 1 of
//     SPEC/HELP.md § Stderr part order), where nothing precedes it
//     and no separator is wanted. EmitTrailingHintOnce writes one
//     blank line and then the hint (part 4), where the blank line
//     separates it from the error line or the recovery help above.
//
//     The separator belongs to the emitter and not to the caller,
//     because it must be governed by the same condition as the hint.
//     A caller that wrote its own separator before calling would
//     still write it when the Once had already been consumed,
//     leaving stderr ending on a blank line that separates nothing.
//
//   - The Once is reset between tests via ResetHintForTesting (used in
//     the same way as ResetForTesting clears the invocation sentinel).
//
//   - The hint text is intentionally NOT re-declared here. The single
//     source of truth lives in internal/commands.AIBannerLine — see
//     SPEC/HELP.md, which requires the env-var line, the error-path
//     trailer, and the --help banner to be byte-identical. Re-typing
//     the literal here would create three places to drift. Instead the
//     caller passes the hint string in, and the wiring in cmd/rmp
//     supplies commands.AIBannerLine.
//
//     (We could also import internal/commands directly from this
//     package, but that would invert the existing dependency arrow —
//     commands → aihelp is the established direction — and trip the
//     import-cycle check. Passing the string is the cheapest way to
//     keep the dependency graph clean.)
//
//   - The AI_AGENT environment variable parser (IsAIAgentEnvActive)
//     lives here too because the env-var path and the dedup primitive
//     are read in lock-step by main(), and putting both in one file
//     keeps the SPEC-to-code mapping obvious. The function reads
//     os.Getenv directly so the caller does not have to thread the
//     environment through; tests that need to override the value use
//     t.Setenv (the standard Go pattern since 1.17).
//
// Concurrency:
//
//   - sync.Once provides the happens-before relationship needed when
//     the env-var path (called at program entry) and the error-path
//     (called from handleError near program exit) race on the same
//     writer. They don't race in today's strictly sequential CLI, but
//     using Once future-proofs the helper against any background work
//     that might fan out from a command handler.

package aihelp

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// hintOnce guards both emitters so the hint is written at most once
// per process. The zero value is ready to use.
//
// It is the SINGLE condition that governs the hint AND the blank line
// that separates it from whatever precedes it, because both are
// written inside the closure it guards.
var hintOnce sync.Once

// EmitHintOnce writes hintText followed by a newline and a blank line
// (a single "\n" after the hint, then one more "\n") to w, but only
// the first time an emitter in this package is called in the current
// process. Subsequent calls — to this function or to
// EmitTrailingHintOnce — are no-ops regardless of which goroutine
// invokes them.
//
// This is the leading form, part 1 of SPEC/HELP.md § Stderr part
// order: the env-var hint that opens stderr under AI_AGENT=1. It
// takes no separator because nothing precedes it.
//
// The output shape is exactly two lines (the hint line itself plus
// the trailing blank line):
//
//	AI agents: run `rmp --ai-help` for a machine-readable command contract.
//	<blank line>
//
// The blank line is intentional: SPEC/HELP.md § AI_AGENT environment
// variable mandates one blank line between the hint and any
// subsequent stderr content (Error: lines, warnings).
func EmitHintOnce(w io.Writer, hintText string) {
	emitHintOnce(w, "", hintText)
}

// EmitTrailingHintOnce writes one blank line, then hintText, then a
// newline and a blank line, to w — but only the first time an emitter
// in this package is called in the current process. Subsequent calls
// — to this function or to EmitHintOnce — write NOTHING AT ALL, the
// separating blank line included.
//
// This is the trailing form, part 4 of SPEC/HELP.md § Stderr part
// order: the hint that closes stderr on a failing invocation, after
// the "Error: " line and after the recovery help when there is one.
// The leading blank line separates it from that preceding content.
//
// That separator is written here, inside the same sync.Once as the
// hint, rather than by the caller. SPEC part 4 is one unit that is
// present or absent as a whole, and only a shared guard can deliver
// that: a caller that prepended the blank line itself would emit it
// even when the deduplication rule elides the hint, leaving stderr
// ending on a blank line the contract does not account for.
//
// The output shape is exactly three lines:
//
//	<blank line>
//	AI agents: run `rmp --ai-help` for a machine-readable command contract.
//	<blank line>
//
// The trailing blank line matches the leading form's, so an agent
// splitting stderr sees the same two-line hint block in both
// positions; terminal users also get a clean newline before the
// shell prompt.
func EmitTrailingHintOnce(w io.Writer, hintText string) {
	emitHintOnce(w, "\n", hintText)
}

// emitHintOnce is the one guarded write both exported emitters share.
//
// lead is the text that precedes the hint line: empty for the leading
// form, a single newline (that is, the separating blank line) for the
// trailing form. Keeping lead inside the closure is the entire point
// of this helper — it makes the separator and the hint one indivisible
// unit under one condition.
//
// A nil writer is rejected before the Once is consulted, so a caller
// with nowhere to write cannot silently spend the process's single
// emission and mute a later caller that does have a writer.
//
// Write errors are silently ignored: stderr is best-effort. A failure
// here cannot be reported anywhere (we are already the diagnostic
// channel) and aborting the process for a broken-pipe stderr would
// turn a cosmetic problem into a behavioural one.
func emitHintOnce(w io.Writer, lead, hintText string) {
	if w == nil {
		return
	}
	hintOnce.Do(func() {
		// Single Fprintf to minimise the chance of interleaving with
		// concurrent stderr writers; the cost over separate calls is
		// one allocation in exchange for atomicity.
		fmt.Fprintf(w, "%s%s\n\n", lead, hintText)
	})
}

// ResetHintForTesting resets the shared emitter sentinel so the next
// call to either emitter writes again. Intended only for unit tests that need to verify
// multiple emission cycles within one process. Production code MUST
// NOT call this function.
//
// The reset replaces the sync.Once value rather than mutating it,
// because sync.Once exposes no Reset method (the type is intentionally
// one-shot). Replacing the variable is safe in tests that run
// sequentially (the standard Go test runner serialises tests in a
// single package unless t.Parallel is called).
func ResetHintForTesting() { hintOnce = sync.Once{} }

// HintWasEmitted reports whether an emitter has performed the
// process's single write. Useful for tests that want to verify the dedup gate
// without re-reading the writer.
//
// Implementation note: sync.Once does not expose a "done" predicate
// publicly, so we infer state by calling Do with a recording closure.
// We mutate a local bool BEFORE the recording closure could be
// invoked, and the closure flips it to false if and only if it runs
// (meaning the Once had NOT been triggered yet). The behaviour is
// observably equivalent to a hypothetical `once.Done()` accessor and
// has the same cost (one atomic load on the fast path).
//
// NOTE: this DOES trigger the Once if it had not been triggered yet,
// which is acceptable for a testing helper (the test must call
// ResetHintForTesting before subsequent assertions) but means
// production code must not call HintWasEmitted as a passive probe.
func HintWasEmitted() bool {
	emitted := true
	hintOnce.Do(func() { emitted = false })
	return emitted
}

// IsAIAgentEnvActive reports whether the AI_AGENT environment variable
// is set to the exact value that activates the env-var hint.
//
// Per SPEC/HELP.md § AI_AGENT environment variable, the hint is
// enabled ONLY when AI_AGENT is the literal string "1". Every other
// value — empty, "0", "true", "false", "yes", "1 " (trailing space),
// "01" — leaves the CLI silent. This is intentionally stricter than
// a typical truthy parser: the SPEC favours zero ambiguity over
// developer convenience because the value is consumed by automated
// agent runners that can always set the exact value.
//
// The function reads os.Getenv directly. Tests override the value
// with t.Setenv("AI_AGENT", "..."), which is automatically restored
// at test teardown.
func IsAIAgentEnvActive() bool {
	return os.Getenv("AI_AGENT") == "1"
}
