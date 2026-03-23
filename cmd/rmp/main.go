// Groadmap - A CLI tool for managing technical roadmaps
//
// Usage: rmp [command] [subcommand] [arguments] [options]
//
// Commands:
//
//	roadmap    Manage roadmaps (alias: road)
//	task       Manage tasks (alias: t)
//	sprint     Manage sprints (alias: s)
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
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

const (
	version = "1.1.0"
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

func main() {
	if len(os.Args) < 2 {
		printHelp()
		db.RunExitHandlers()
		os.Exit(ExitSuccess)
	}

	arg := os.Args[1]

	switch arg {
	case "-h", "--help", "help":
		printHelp()
		db.RunExitHandlers()
		os.Exit(ExitSuccess)
	case "-v", "--version", "version":
		fmt.Printf("%s version %s\n", appName, version)
		db.RunExitHandlers()
		os.Exit(ExitSuccess)
	}

	// Route to appropriate command handler
	var err error
	exitCode := ExitSuccess

	switch arg {
	case "roadmap", "road":
		err = commands.HandleRoadmap(os.Args[2:])
	case "task", "t":
		err = commands.HandleTask(os.Args[2:])
	case "sprint", "s":
		err = commands.HandleSprint(os.Args[2:])
	case "audit", "aud":
		err = commands.HandleAudit(os.Args[2:])
	case "stats":
		err = commands.HandleStats(os.Args[2:])
	default:
		printError(fmt.Sprintf("Unknown command: %s", arg))
		printHelp()
		db.RunExitHandlers()
		os.Exit(ExitCmdNotFound)
	}

	if err != nil {
		exitCode = handleError(err)
	}

	db.RunExitHandlers()
	os.Exit(exitCode)
}

// handleError maps errors to appropriate exit codes.
func handleError(err error) int {
	if err == nil {
		return ExitSuccess
	}

	msg := err.Error()
	printError(msg)

	// Map sentinel errors to exit codes using errors.Is
	switch {
	case errors.Is(err, utils.ErrNotFound):
		return ExitNotFound
	case errors.Is(err, utils.ErrAlreadyExists):
		return ExitExists
	case errors.Is(err, utils.ErrNoRoadmap):
		return ExitNoRoadmap
	case errors.Is(err, utils.ErrInvalidInput), errors.Is(err, utils.ErrValidation):
		return ExitInvalidData
	case errors.Is(err, utils.ErrRequired):
		return ExitMisuse
	}

	// Fallback to string matching for wrapped errors not yet using sentinel errors
	switch {
	case strings.Contains(msg, "not found"):
		return ExitNotFound
	case strings.Contains(msg, "already exists"):
		return ExitExists
	case strings.Contains(msg, "invalid"):
		return ExitInvalidData
	case strings.Contains(msg, "required"):
		return ExitMisuse
	case strings.Contains(msg, "no roadmap"):
		return ExitNoRoadmap
	default:
		return ExitFailure
	}
}

// printError prints an error message to stderr.
func printError(msg string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
}

// printHelp prints the main help text.
func printHelp() {
	fmt.Printf(`%s - A CLI tool for managing technical roadmaps

Usage: rmp [command] [subcommand] [arguments] [options]

Commands:
  roadmap, road    Manage roadmaps
  task, t          Manage tasks
  sprint, s        Manage sprints
  audit, aud       View audit log
  stats            View roadmap statistics

Global Options:
  -h, --help       Show this help message
  -v, --version    Show version

Use "rmp [command] --help" for more information about a command.
`, appName)
}
