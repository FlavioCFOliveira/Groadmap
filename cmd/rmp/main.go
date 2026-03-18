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
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

const (
	version = "1.0.0"
	appName = "Groadmap"
)

// Global logger
var logger *slog.Logger

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
	// Configure logger with default level (INFO)
	setupLogger(slog.LevelInfo)

	if len(os.Args) < 2 {
		printHelp()
		os.Exit(ExitSuccess)
	}

	// Check for global flags
	arg := os.Args[1]

	// Handle verbose flag before other processing
	for i, a := range os.Args {
		if a == "--verbose" || a == "-verbose" {
			setupLogger(slog.LevelDebug)
			// Remove the flag from args
			os.Args = append(os.Args[:i], os.Args[i+1:]...)
			break
		}
	}

	// Re-check args after potentially removing --verbose
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(ExitSuccess)
	}

	arg = os.Args[1]
	switch arg {
	case "-h", "--help", "help":
		printHelp()
		os.Exit(ExitSuccess)
	case "-v", "--version", "version":
		fmt.Printf("%s version %s\n", appName, version)
		os.Exit(ExitSuccess)
	}

	// Route to appropriate command handler
	var err error
	exitCode := ExitSuccess

	switch arg {
	case "roadmap", "road":
		logger.Debug("handling roadmap command", "command", arg)
		err = commands.HandleRoadmap(os.Args[2:])
	case "task", "t":
		logger.Debug("handling task command", "command", arg)
		err = commands.HandleTask(os.Args[2:])
	case "sprint", "s":
		logger.Debug("handling sprint command", "command", arg)
		err = commands.HandleSprint(os.Args[2:])
	case "audit", "aud":
		logger.Debug("handling audit command", "command", arg)
		err = commands.HandleAudit(os.Args[2:])
	case "completion":
		logger.Debug("handling completion command")
		err = commands.HandleCompletion(os.Args[2:])
	default:
		logger.Error("unknown command", "command", arg)
		printError(fmt.Sprintf("Unknown command: %s", arg))
		printHelp()
		os.Exit(ExitCmdNotFound)
	}

	if err != nil {
		exitCode = handleError(err)
	}

	os.Exit(exitCode)
}

// setupLogger configures the global structured logger.
func setupLogger(level slog.Level) {
	opts := &slog.HandlerOptions{
		Level: level,
	}
	handler := slog.NewJSONHandler(os.Stderr, opts)
	logger = slog.New(handler)
	slog.SetDefault(logger)
}

// handleError maps errors to appropriate exit codes.
func handleError(err error) int {
	if err == nil {
		return ExitSuccess
	}

	msg := err.Error()
	printError(msg)

	// Log error with structured fields
	logger.Error("command failed",
		"error", msg,
		"error_type", fmt.Sprintf("%T", err),
	)

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
  completion       Generate shell completion scripts

Global Options:
  -h, --help       Show this help message
  -v, --version    Show version
  --verbose        Enable verbose (debug) logging

Use "rmp [command] --help" for more information about a command.
`, appName)
}

// getDefaultRoadmap returns the currently selected default roadmap.
func getDefaultRoadmap() (string, error) {
	dataDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}

	currentFile := filepath.Join(dataDir, ".roadmaps", ".current")
	data, err := os.ReadFile(currentFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no roadmap selected")
		}
		return "", fmt.Errorf("reading current roadmap: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

// setDefaultRoadmap sets the default roadmap.
func setDefaultRoadmap(name string) error {
	dataDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	roadmapsDir := filepath.Join(dataDir, ".roadmaps")
	if err := os.MkdirAll(roadmapsDir, 0700); err != nil {
		return fmt.Errorf("creating roadmaps directory: %w", err)
	}

	currentFile := filepath.Join(roadmapsDir, ".current")
	if err := os.WriteFile(currentFile, []byte(name), 0600); err != nil {
		return fmt.Errorf("writing current roadmap: %w", err)
	}

	return nil
}
