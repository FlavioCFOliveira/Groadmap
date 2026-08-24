// Groadmap - A CLI tool for managing technical roadmaps
//
// Usage: rmp [command] [subcommand] [arguments] [options]
//
// Commands:
//
//	roadmap    Manage roadmaps (alias: road)
//	task       Manage tasks (alias: t)
//	sprint     Manage sprints (alias: s)
//	backlog    Manage backlog tasks (alias: bl)
//	audit      View audit log (alias: aud)
//	stats      View roadmap statistics
//
// Global Options:
//
//	-h, --help     Show help
//	-v, --version  Show version
//
// Exit Codes:
//
//	0   Success
//	1   General error
//	2   Invalid arguments
//	3   No roadmap selected
//	4   Resource not found
//	5   Resource already exists
//	6   Invalid data
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/FlavioCFOliveira/Groadmap/internal/aihelp"
	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

const (
	version = "1.15.0"
	appName = "Groadmap"
)

// Exit codes as defined in SPEC/ARCHITECTURE.md
const (
	ExitSuccess       = 0
	ExitFailure       = 1
	ExitMisuse        = 2
	ExitNoRoadmap     = 3
	ExitNotFound      = 4
	ExitExists        = 5
	ExitInvalidData   = 6
	ExitNotExecutable = 126
	ExitCmdNotFound   = 127
	ExitSigint        = 130
)

// installSignalHandler maps SIGINT/SIGTERM to the canonical exit code 130
// defined in SPEC/ARCHITECTURE.md § Exit Codes. Without an explicit handler
// the Go runtime lets the kernel terminate the process by signal, which
// produces a platform-dependent status that is not the documented 130.
func installSignalHandler() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		os.Exit(ExitSigint)
	}()
}

func main() {
	installSignalHandler()

	if len(os.Args) < 2 {
		printHelp()
		os.Exit(ExitSuccess)
	}

	// AI_AGENT env-var discovery hint (SPEC/HELP.md
	// § AI_AGENT environment variable):
	//
	// When AI_AGENT=1 is active, the hint MUST be the first line of
	// stderr for the entire invocation. We emit it here, BEFORE
	// maybeHandleAIHelp runs, with one caveat: per SPEC the hint is
	// suppressed for the AI-help invocation forms themselves (the
	// agent is already consuming the contract, so the hint would be
	// noise). We peek at argv with the same detector the wiring uses
	// to decide whether this invocation is going to serve the contract.
	//
	// The actual write goes through aihelp.EmitHintOnce, a sync.Once-
	// guarded helper that coordinates with the error-path hint in
	// handleError. The dedup contract is "exactly one hint per
	// invocation, even when both paths fire".
	if aihelp.IsAIAgentEnvActive() && !isAIHelpInvocation(os.Args[1:]) {
		aihelp.EmitHintOnce(os.Stderr, commands.AIBannerLine)
	}

	// AI Agent Contract emission is intercepted BEFORE any other
	// global-flag handling so that --ai-help wins over --help, --version,
	// -r, and every action flag — the precedence required by
	// SPEC/COMMANDS.md § AI Help. The wiring lives in aihelp_wiring.go
	// to keep main.go small and to make the scope-extraction logic
	// independently unit-testable.
	if handled, code := maybeHandleAIHelp(os.Args[1:], os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}

	arg := os.Args[1]

	// Global flags are handled here, before any command lookup. They
	// are intentionally NOT in the command registry because their
	// effect is on the binary itself, not on any single command family.
	//
	// Because they resolve here, these six forms are NOT covered by the
	// shared positional-arity enforcement point that every registered
	// command reaches through Command.DispatchFamily. Each declares a
	// maximum of zero positional arguments (SPEC/COMMANDS.md § Positional
	// Arity by Command) and each enforces that declaration itself, through
	// refuseGlobalPositional in global_arity.go, before writing anything.
	switch arg {
	case "-h", "--help", "help":
		if err := refuseGlobalPositional(os.Args[2:]); err != nil {
			os.Exit(handleError(err))
		}
		printHelp()
		os.Exit(ExitSuccess)
	case "-v", "--version", "version":
		if err := refuseGlobalPositional(os.Args[2:]); err != nil {
			os.Exit(handleError(err))
		}
		fmt.Printf("%s version %s\n", appName, version)
		os.Exit(ExitSuccess)
	}

	// Filesystem layout migration sweep (SPEC/ARCHITECTURE.md
	// § Filesystem Layout Migration). It runs after the global-flag
	// switch above — so --help/--version/--ai-help, which exit earlier,
	// never trigger it — and before command routing, so every real
	// command observes the current ~/.roadmaps/<name>/project.db layout.
	// Per-roadmap skips and failures are non-fatal and already reported to
	// stderr inside the sweep; only an unreadable data directory is fatal,
	// surfaced here as ErrDatabase (exit 1) via the standard error path.
	if err := utils.MigrateLegacyLayout(); err != nil {
		os.Exit(handleError(err))
	}

	// Route via the command registry. The registry is the single
	// source of truth for command names, aliases, and the handler
	// associated with each command family (see
	// internal/commands/registry.go and registry_data.go).
	reg := commands.AppRegistry()
	cmd := reg.FindCommand(arg)
	if cmd == nil {
		// Dispatch failure at the top level. It is routed through the
		// same handleError path as every other error so the stderr
		// shape, the stdout silence, and the exit code all come from a
		// single place: the general help that follows the error is the
		// recovery help, written to stderr by writeFailureReport
		// (SPEC/HELP.md § Recovery help after a dispatch failure).
		os.Exit(handleError(commands.NewUnknownCommandError(arg)))
	}

	err := cmd.DispatchFamily(os.Args[2:])

	exitCode := ExitSuccess
	if err != nil {
		exitCode = handleError(err)
	}

	os.Exit(exitCode)
}

// handleError writes the failure report for err to stderr and maps it to
// the exit code SPEC/ARCHITECTURE.md § Exit Codes assigns to its class.
func handleError(err error) int {
	if err == nil {
		return ExitSuccess
	}

	writeFailureReport(os.Stderr, err)

	// Map sentinel errors to exit codes using errors.Is.
	// All errors raised by internal packages go through utils.Err* sentinels
	// with %w wrapping, so this switch is exhaustive in practice.
	switch {
	case errors.Is(err, utils.ErrUnknownCommand):
		// A dispatch failure: an unresolved command name or an
		// unresolved subcommand name. Listed first so it can never be
		// shadowed by a broader class; it deliberately does NOT wrap
		// utils.ErrInvalidInput, which would land it on 2
		// (SPEC/ARCHITECTURE.md § Sentinel Error Catalogue).
		return ExitCmdNotFound
	case errors.Is(err, utils.ErrNotFound):
		return ExitNotFound
	case errors.Is(err, utils.ErrAlreadyExists):
		return ExitExists
	case errors.Is(err, utils.ErrNoRoadmap):
		return ExitNoRoadmap
	case errors.Is(err, utils.ErrValidation),
		errors.Is(err, utils.ErrFieldTooLarge):
		return ExitInvalidData
	case errors.Is(err, utils.ErrInvalidInput),
		errors.Is(err, utils.ErrRequired):
		return ExitMisuse
	}
	return ExitFailure
}

// writeFailureReport writes the complete stderr report for a failing
// invocation, in the part order pinned by SPEC/HELP.md § Stderr part
// order:
//
//  1. the AI_AGENT=1 hint plus a blank line, when that env var is
//     active — already written at the top of main() before this runs;
//  2. the "Error: " line;
//  3. a blank line then the recovery help, on a dispatch failure only;
//  4. a blank line then the AI-agent hint, unless suppressed.
//
// Nothing here touches stdout: a failing invocation writes zero bytes to
// it (SPEC/HELP.md § Stdout silence on failure), which is why the
// recovery help goes to w rather than through the ordinary help path.
//
// The trailing AI-agent hint is suppressed in two situations:
//
//  1. When this invocation already emitted the AI Agent Contract
//     (aihelp.WasInvoked() == true). The agent is consuming the
//     contract; pointing them at it again would be recursive noise.
//
//  2. When the AI_AGENT=1 env-var path already wrote the hint at the
//     top of stderr (handled implicitly by EmitHintOnce's sync.Once:
//     the second call here becomes a no-op).
//
// Note: case (1) covers `rmp --ai-help` / `rmp ai-help` etc., where
// markInvoked() flipped the sentinel inside aihelp.Generate. For
// scope-rejection errors emitted by maybeHandleAIHelp (e.g.
// `rmp invalidcmd --ai-help`), markInvoked() is NOT called because
// Generate returns before it — so WasInvoked() stays false and the
// agent gets the hint, helping it discover the contract entry point.
func writeFailureReport(w io.Writer, err error) {
	fmt.Fprintf(w, "Error: %s\n", err.Error())

	// Part 3. Recovery help follows a dispatch failure and nothing else.
	// A missing parameter, an unknown flag, an invalid enum value, a
	// not-found, a conflict and a database failure each get the error
	// line and the hint alone: the error line already names the
	// offending flag or value, so the reader recovers by running
	// --help explicitly (SPEC/HELP.md § Recovery help after a dispatch
	// failure).
	var dispatch *commands.DispatchError
	if errors.As(err, &dispatch) {
		fmt.Fprintln(w)
		// RecoveryHelp renders the family help body for an unresolved
		// subcommand. It reports false for an unresolved top-level
		// command, whose recovery help is the global help body — owned
		// by this package, so it is written here.
		if !dispatch.RecoveryHelp(w) {
			writeGlobalHelpBody(w)
		}
	}

	// Part 4. The hint stays last on every error path.
	if aihelp.WasInvoked() {
		return
	}
	// EmitHintOnce internally writes the hint plus a trailing newline
	// pair. To get the SPEC shape "…, blank line, hint" we prepend the
	// separating blank line here. Subsequent callers in the same
	// invocation (rare — handleError runs at most once) are deduped by
	// sync.Once and produce nothing.
	fmt.Fprintln(w)
	aihelp.EmitHintOnce(w, commands.AIBannerLine)
}

// printHelp prints the main help text.
//
// The SPEC-mandated AI-agent discovery banner is prepended first
// (see SPEC/HELP.md § AI agent banner). The banner makes the
// machine-readable contract emitted by `rmp --ai-help` discoverable
// to LLM agents that first reach for the standard `--help` surface.
// The single source of the banner string lives in
// internal/commands.AIBannerLine, exposed via commands.WriteAIBanner
// so this binary cannot drift from the SPEC text.
func printHelp() {
	commands.WriteAIBanner(os.Stdout)
	writeGlobalHelpBody(os.Stdout)
}

// writeGlobalHelpBody writes the global help body WITHOUT the AI-agent
// banner, to an arbitrary writer.
//
// Two callers need it on two different streams: printHelp writes it to
// stdout under the banner for a help request the reader asked for, and
// writeFailureReport writes it to stderr as the recovery help for an
// unresolved command name. The recovery help must omit the banner:
// the banner and the trailing hint carry the same sentence, and the
// failing invocation already ends with the hint, so emitting both would
// put that sentence on stderr twice (SPEC/HELP.md § Recovery help after
// a dispatch failure).
func writeGlobalHelpBody(w io.Writer) {
	fmt.Fprintf(w, `%s - A CLI tool for managing technical roadmaps

Usage: rmp [command] [subcommand] [arguments] [options]

Commands:
%s

Choosing a task-listing command:
  rmp task list                All tasks in a roadmap, any status (filter with --status, etc.)
  rmp backlog list             Only BACKLOG tasks (subset of 'task list' with --status BACKLOG)
  rmp sprint tasks <id>        Tasks that belong to one specific sprint (any status)
  rmp sprint open-tasks <id>   Tasks in a sprint with status SPRINT/DOING/TESTING (excludes COMPLETED)
  rmp task next [num]          Top-priority tasks from the currently OPEN sprint (planning shortcut)
  rmp backlog show-next [n]    Top-priority BACKLOG tasks (sprint-planning shortcut)

I/O conventions:
  - Every command requires -r <roadmap>, except 'rmp roadmap', 'rmp web',
    the AI contract (--ai-help/ai-help), and global help/version.
  - Successful output is JSON on stdout; errors are plain text on stderr.
  - All timestamps in JSON use ISO 8601 UTC: YYYY-MM-DDTHH:mm:ss.sssZ.

Global Options:
  -h, --help       Show this help message
  -v, --version    Show version
  --ai-help        Emit the AI Agent Contract (machine-readable JSON)

Use "rmp [command] --help" for more information about a command.
`, appName, commandSummaryLines())
}

// commandSummaryLines renders the global-help command list directly from the
// command registry (the single source of truth per SPEC/ARCHITECTURE.md), so
// the list can never drift from the registered commands. Previously this block
// was hardcoded and had silently dropped the `web` command (finding #51).
func commandSummaryLines() string {
	var b strings.Builder
	cmds := commands.AppRegistry().Commands
	for i := range cmds {
		c := &cmds[i]
		name := c.Name
		if len(c.Aliases) > 0 {
			name += ", " + strings.Join(c.Aliases, ", ")
		}
		// Trim the trailing period registry summaries carry, to match the
		// established one-line help style.
		summary := strings.TrimSuffix(c.Summary, ".")
		fmt.Fprintf(&b, "  %-16s %s\n", name, summary)
	}
	return strings.TrimRight(b.String(), "\n")
}
