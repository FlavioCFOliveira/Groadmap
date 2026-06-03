// cmd/rmp/aihelp_wiring.go — AI Agent Contract emission wiring.
//
// This file is the SOLE production path for emitting the AI Agent
// Contract. It is invoked from main() before any command dispatch
// happens, so the contract emission is unaffected by missing -r,
// invalid action flags, or any other downstream parsing concerns.
//
// SPEC references:
//   - SPEC/COMMANDS.md § AI Help — invocation forms, precedence rules,
//     exit codes.
//   - SPEC/ARCHITECTURE.md § AI Agent Contract Generation — registry
//     single-source-of-truth invariant.
//
// Design contract:
//
//   - The four invocation forms required by SPEC are intercepted by
//     a single early-pass scan over argv. The scan runs BEFORE the
//     registry's FindCommand lookup so that every action handler is
//     bypassed (acceptance criterion #5: "combining --ai-help with
//     mutating flags does NOT perform the mutation").
//
//   - Scope is determined by the position of the --ai-help token (or
//     by the presence of the literal `ai-help` token at argv[1]):
//
//       rmp --ai-help                        → ScopeAll
//       rmp ai-help                          → ScopeAll
//       rmp <cmd> --ai-help                  → ScopeCommand(cmd)
//       rmp <cmd> <sub> --ai-help            → ScopeSubcommand(cmd, sub)
//
//   - Unknown command/subcommand names PRECEDING --ai-help produce
//     ErrInvalidInput, which the standard error mapping in main.go
//     surfaces as exit code 2 — matching SPEC/COMMANDS.md § AI Help
//     "Exit codes".
//
//   - `--help` appearing alongside --ai-help is ignored: the contract
//     wins per SPEC rule "When both --ai-help and any other action-
//     bearing flag or argument are present, --ai-help wins".
//
//   - The `ai-help` top-level command accepts only `--help` / `-h` /
//     `help` beyond its own name; in that case the contract is emitted
//     anyway (the contract IS the documentation; printing a human
//     help block when the agent explicitly asked for the contract
//     would defeat the purpose of the command). Any OTHER positional
//     or flag argument after `ai-help` is rejected with exit code 2.

package main

import (
	"fmt"
	"io"

	"github.com/FlavioCFOliveira/Groadmap/internal/aihelp"
	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
)

// isAIHelpInvocation reports whether args (the program's argv minus
// the binary name) is going to be handled by the AI-help wiring.
// Wrapper around detectAIHelpInvocation so the main() entry can decide
// — BEFORE calling maybeHandleAIHelp — whether to skip the AI_AGENT
// env-var hint emission. The SPEC requires the hint to be suppressed
// for any of the four AI-help invocation forms (the agent is already
// consuming the contract).
//
// The function deliberately ignores the boolean's "scope" value: at
// this point we only need to know that the wiring WILL take over;
// the scope (whole CLI vs. one command) does not change the
// suppression decision.
func isAIHelpInvocation(args []string) bool {
	_, ok := detectAIHelpInvocation(args)
	return ok
}

// writeAIHelpError emits a SPEC-shaped error from the AI-help wiring
// layer. It mirrors cmd/rmp/main.go's printError but writes to an
// arbitrary io.Writer (the wiring path is unit-tested with a buffer
// instead of os.Stderr).
//
// The trailing AI-agent hint is appended exactly like printError
// does, with the same suppression rules:
//
//   - If aihelp.WasInvoked() is true, the contract has already been
//     delivered and the hint is suppressed (no recursion).
//   - The dedup with the env-var hint is implicit: aihelp.EmitHintOnce
//     uses a single sync.Once for the whole process, so if the env-var
//     path already fired, this call is a no-op.
//
// In practice WasInvoked() is always false here because the only
// caller is the scope-rejection branch of maybeHandleAIHelp, which
// runs BEFORE Generate flips the sentinel. Checking the sentinel is
// still the right thing to do: it keeps the helper drop-in safe if
// future error sites move below Generate.
func writeAIHelpError(w io.Writer, msg string) {
	fmt.Fprintln(w, "Error: "+msg)
	if aihelp.WasInvoked() {
		return
	}
	fmt.Fprintln(w)
	aihelp.EmitHintOnce(w, commands.AIBannerLine)
}

// aiHelpFlagToken is the literal flag string the early-pass scan
// looks for. Declared as a constant so the test suite can reference
// it without re-typing the literal.
const aiHelpFlagToken = "--ai-help"

// aiHelpCommandToken is the top-level command name the early-pass
// scan treats as equivalent to bare `--ai-help`.
const aiHelpCommandToken = "ai-help"

// aiHelpRejectScopeName is a reserved scope.Command value used by
// detectAIHelpInvocation to signal "ai-help with unexpected trailing
// arguments" to maybeHandleAIHelp. The value is intentionally outside
// the legal command-name regex so it cannot ever collide with a real
// registry entry. The string itself never reaches stderr; the
// translator in maybeHandleAIHelp swaps it for a SPEC-accurate error
// message before printing.
const aiHelpRejectScopeName = "__ai_help_unexpected_args__"

// maybeHandleAIHelp inspects argv (without the program name) and
// returns (true, exitCode) when it has fully handled the invocation,
// or (false, _) when control should fall through to the normal
// dispatch path.
//
// The caller in main() does:
//
//	if handled, code := maybeHandleAIHelp(os.Args[1:], os.Stdout, os.Stderr); handled {
//	    os.Exit(code)
//	}
//
// The stdout/stderr writers are taken as parameters (instead of
// hard-coding os.Stdout / os.Stderr) so the unit tests can assert on
// the exact bytes produced without subprocess execution.
func maybeHandleAIHelp(args []string, stdout, stderr io.Writer) (handled bool, exitCode int) {
	scope, ok := detectAIHelpInvocation(args)
	if !ok {
		return false, 0
	}

	// Sentinel-scope path: the detection layer signals "ai-help with
	// unexpected positional/flag arguments" by stuffing a reserved
	// command name into scope.Command. We translate that here into a
	// SPEC-accurate error message instead of leaking the sentinel
	// through Generate's "unknown command" branch.
	if scope.Command == aiHelpRejectScopeName {
		writeAIHelpError(stderr, "ai-help accepts no positional arguments or flags other than --help")
		return true, ExitMisuse
	}

	// Build the ContractInfo from the binary identity constants. The
	// values are the canonical source: the version constant in this
	// package is what `rmp --version` already reports, so the contract
	// and the version subcommand are in lock-step by construction.
	info := aihelp.ContractInfo{
		ToolName:      "rmp",
		DisplayName:   appName,
		BinaryVersion: version,
		Description:   "CLI for managing technical roadmaps with SQLite-backed task/sprint tracking and a Cypher-queryable knowledge graph per roadmap.",
	}

	payload, err := aihelp.Generate(scope, info)
	if err != nil {
		// Generate returns ErrInvalidInput when scope filtering hits an
		// unknown command or subcommand. Per SPEC/COMMANDS.md § AI Help
		// "Exit codes" the correct code is 2 (misuse), which is also what
		// the standard handleError path in main.go now produces for
		// ErrInvalidInput. We surface the error directly here (rather than
		// routing through handleError) because the AI-help error shape is
		// distinct — writeAIHelpError emits the contract-discovery hint —
		// and returning ExitMisuse explicitly keeps this path independent
		// of the central mapping.
		writeAIHelpError(stderr, err.Error())
		return true, ExitMisuse
	}

	if _, err := stdout.Write(payload); err != nil {
		// stdout write errors are rare in practice (pipe closed,
		// disk full) but worth surfacing as a generic failure so the
		// caller knows the contract did not reach the consumer.
		writeAIHelpError(stderr, "write contract to stdout: "+err.Error())
		return true, ExitFailure
	}
	return true, ExitSuccess
}

// detectAIHelpInvocation implements the SPEC's invocation-form
// detection rules. Returns (scope, true) when args triggers contract
// emission, or (Scope{}, false) when no AI-help token is present.
//
// The function never reaches into any global state; it is a pure
// function of args, which makes it trivially unit-testable.
func detectAIHelpInvocation(args []string) (aihelp.Scope, bool) {
	if len(args) == 0 {
		return aihelp.Scope{}, false
	}

	// Form 1: bare top-level command `rmp ai-help [...]`.
	//
	// Per SPEC: "The command `ai-help` accepts no positional arguments
	// and no flags other than `--help`. Any other argument produces an
	// `Error: ` to stderr with exit code 2."
	//
	// We tolerate help-tokens (`--help`, `-h`, `help`) trailing the
	// command name (treated as a no-op — the contract IS the help)
	// and reject everything else, including a duplicated `--ai-help`
	// flag which is harmless but confusing. Note: the rejection is
	// performed by emitting an explicit error scope via the registry-
	// resolution path below (we just route into ScopeAll first and let
	// the trailing-arg validator decide).
	if args[0] == aiHelpCommandToken {
		// Reject any non-help trailing argument.
		if !aiHelpTrailingArgsAreOnlyHelp(args[1:]) {
			// Signal "unexpected args" to maybeHandleAIHelp by stuffing
			// the reserved sentinel into scope.Command. The translator
			// in the wiring layer swaps it for the SPEC-mandated error
			// message before any output reaches the user.
			return aihelp.ScopeCommand(aiHelpRejectScopeName), true
		}
		return aihelp.ScopeAll(), true
	}

	// Form 2..4: --ai-help flag appearing somewhere in args.
	pos := indexOf(args, aiHelpFlagToken)
	if pos < 0 {
		return aihelp.Scope{}, false
	}

	// The scope is determined by the tokens that PRECEDE --ai-help.
	// Anything AFTER it is part of the suppressed action-flag tail
	// and intentionally ignored — this is how acceptance criterion #5
	// ("no mutation when --ai-help is present") is satisfied.
	preceding := args[:pos]

	// preceding == nothing → whole-CLI scope.
	if len(preceding) == 0 {
		return aihelp.ScopeAll(), true
	}

	reg := commands.AppRegistry()

	// preceding[0] is the command name (canonical or alias). It must
	// resolve through the registry. If it does not, the contract
	// generator will produce a ScopeCommand error → exit 2.
	cmdToken := preceding[0]

	// preceding[0] may itself be the top-level `ai-help` command name.
	// In that case we treat it the same as form 1 (whole-CLI scope).
	if cmdToken == aiHelpCommandToken {
		return aihelp.ScopeAll(), true
	}

	// preceding[0] may also be a help/version token. Per SPEC,
	// --ai-help wins over --help, so we proceed with ScopeAll instead
	// of routing into the help printer. We do NOT treat --version as
	// a contract-emitting form (the SPEC does not specify a "--version
	// --ai-help" form), but to honour the "ai-help wins" precedence
	// we resolve it as whole-CLI scope rather than failing.
	if isHelpOrVersionToken(cmdToken) {
		return aihelp.ScopeAll(), true
	}

	cmd := reg.FindCommand(cmdToken)
	if cmd == nil {
		// Surface the unknown command via Generate so the error
		// message format and the exit code (2) are the same as for an
		// unknown subcommand. We use ScopeCommand to capture the
		// raw token in the error.
		return aihelp.ScopeCommand(cmdToken), true
	}

	// Resolve subcommand, if any. preceding[1] is treated as a
	// subcommand candidate iff it is present and is NOT itself a flag
	// (a flag would mean the user wrote `rmp <cmd> -x --ai-help`,
	// which is the ScopeCommand form).
	if len(preceding) >= 2 && !isFlagToken(preceding[1]) {
		subToken := preceding[1]
		// Help tokens at the subcommand position fold into the
		// command-scope form (precedence rule).
		if isHelpToken(subToken) {
			return aihelp.ScopeCommand(cmd.Name), true
		}
		// We canonicalise the subcommand name via the registry so the
		// error message (when the subcommand is unknown) uses the
		// canonical command name and surfaces the raw subcommand token
		// — both via Generate's ErrInvalidInput message.
		return aihelp.ScopeSubcommand(cmd.Name, subToken), true
	}
	return aihelp.ScopeCommand(cmd.Name), true
}

// indexOf returns the index of needle in haystack, or -1 if absent.
// Kept small and explicit instead of pulling in slices.Index so the
// generated binary's import graph stays minimal.
func indexOf(haystack []string, needle string) int {
	for i, v := range haystack {
		if v == needle {
			return i
		}
	}
	return -1
}

// isFlagToken reports whether arg looks like a flag (starts with `-`
// and is at least two bytes). This is the same heuristic the rest of
// the parser uses to distinguish flags from positionals.
func isFlagToken(arg string) bool {
	return len(arg) >= 2 && arg[0] == '-'
}

// isHelpToken mirrors commands.isHelpToken (which is unexported in
// that package). The duplicate is intentional: the dependency
// direction is cmd/rmp → internal/commands, and we do not want to
// add an exported helper to internal/commands solely for this
// wiring file.
func isHelpToken(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

// isHelpOrVersionToken reports whether arg is one of the global
// help/version tokens recognised before any command dispatch. Used
// to handle the precedence rule that --ai-help wins over --help and
// (by extension) over the global --version token at the same
// position.
func isHelpOrVersionToken(arg string) bool {
	if isHelpToken(arg) {
		return true
	}
	return arg == "-v" || arg == "--version" || arg == "version"
}

// aiHelpTrailingArgsAreOnlyHelp reports whether every token in args
// is a help token. Used to validate the `rmp ai-help [...]` form,
// where the SPEC permits `--help` as the only additional flag.
func aiHelpTrailingArgsAreOnlyHelp(args []string) bool {
	for _, a := range args {
		if !isHelpToken(a) {
			return false
		}
	}
	return true
}
