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
	"os"
	"os/signal"
	"syscall"

	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

const (
	version = "1.3.0"
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
	switch arg {
	case "-h", "--help", "help":
		printHelp()
		os.Exit(ExitSuccess)
	case "-v", "--version", "version":
		fmt.Printf("%s version %s\n", appName, version)
		os.Exit(ExitSuccess)
	}

	// Route via the command registry. The registry is the single
	// source of truth for command names, aliases, and the handler
	// associated with each command family (see
	// internal/commands/registry.go and registry_data.go).
	reg := commands.AppRegistry()
	cmd := reg.FindCommand(arg)
	if cmd == nil {
		printError("Unknown command: " + arg)
		printHelp()
		os.Exit(ExitCmdNotFound)
	}

	err := cmd.DispatchFamily(os.Args[2:])

	exitCode := ExitSuccess
	if err != nil {
		exitCode = handleError(err)
	}

	os.Exit(exitCode)
}

// handleError maps errors to appropriate exit codes.
func handleError(err error) int {
	if err == nil {
		return ExitSuccess
	}

	printError(err.Error())

	// Map sentinel errors to exit codes using errors.Is.
	// All errors raised by internal packages go through utils.Err* sentinels
	// with %w wrapping, so this switch is exhaustive in practice.
	switch {
	case errors.Is(err, utils.ErrNotFound):
		return ExitNotFound
	case errors.Is(err, utils.ErrAlreadyExists):
		return ExitExists
	case errors.Is(err, utils.ErrNoRoadmap):
		return ExitNoRoadmap
	case errors.Is(err, utils.ErrInvalidInput),
		errors.Is(err, utils.ErrValidation),
		errors.Is(err, utils.ErrFieldTooLarge):
		return ExitInvalidData
	case errors.Is(err, utils.ErrRequired):
		return ExitMisuse
	}
	return ExitFailure
}

// printError prints an error message to stderr.
func printError(msg string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
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
	fmt.Printf(`%s - A CLI tool for managing technical roadmaps

Usage: rmp [command] [subcommand] [arguments] [options]

Commands:
  roadmap, road    Create, list, and remove roadmap database files (~/.roadmaps/<name>.db)
  task, t          Manage tasks across statuses BACKLOG/SPRINT/DOING/TESTING/COMPLETED
  sprint, s        Manage sprints and their task membership/ordering
  backlog, bl      Query BACKLOG-status tasks (planning view for tasks not yet in a sprint)
  audit, aud       Query the per-roadmap audit log
  stats            Roadmap-wide statistics (sprint counts, task distribution, velocity)

Choosing a task-listing command:
  rmp task list            All tasks in a roadmap, any status (filter with --status, etc.)
  rmp backlog list         Only BACKLOG tasks (subset of 'task list' with --status BACKLOG)
  rmp sprint tasks <id>    Tasks that belong to one specific sprint (any status)
  rmp sprint open-tasks <id>   Tasks in a sprint with status SPRINT/DOING/TESTING (excludes COMPLETED)
  rmp task next [num]      Top-priority tasks from the currently OPEN sprint (planning shortcut)
  rmp backlog show-next [n]    Top-priority BACKLOG tasks (sprint-planning shortcut)

I/O conventions:
  - Every command except 'rmp roadmap' and global help requires -r <roadmap>.
  - Successful output is JSON on stdout; errors are plain text on stderr.
  - All timestamps in JSON use ISO 8601 UTC: YYYY-MM-DDTHH:mm:ss.sssZ.

Global Options:
  -h, --help       Show this help message
  -v, --version    Show version

Use "rmp [command] --help" for more information about a command.
`, appName)
}
