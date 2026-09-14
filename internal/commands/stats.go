package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// HandleStats handles the stats command.
func HandleStats(args []string) error {
	// A help token anywhere in the argument list is served before any other
	// parsing runs, so the help is reachable even when -r is missing
	// (SPEC/HELP.md § Help levels). `stats` is a leaf command: DispatchFamily
	// hands it the arguments untouched and never runs the hasHelpFlag
	// short-circuit it runs for a family's subcommands, so this handler
	// applies that same predicate to the same span itself. That is what makes
	// `rmp stats -r <name> --help` indistinguishable from
	// `rmp task list -r <name> --help` (SPEC/COMMANDS.md § Roadmap Statistics).
	//
	// The check used to read args[0] alone. A help token written after the
	// selector then survived requireRoadmap and reached rejectUnknownFlags
	// below, which refused it as an unknown flag with exit 2 (rmp task 474).
	//
	// Route through invokeHelpPrinter so the SPEC-mandated AI-agent banner
	// (SPEC/HELP.md § AI agent banner) is prepended uniformly.
	if hasHelpFlag(args) {
		invokeHelpPrinter(printStatsHelp)
		return nil
	}

	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	// `stats` declares no flag of its own beyond the roadmap selector, which
	// requireRoadmap has just consumed, and no positional argument, which the
	// shared arity point refused before this handler ran. A "-"-prefixed token
	// left in remaining therefore names nothing this command accepts, and is
	// refused with the CLI-wide line rather than discarded
	// (SPEC/COMMANDS.md § Roadmap Statistics). Discarding it is what this
	// command used to do: `rmp stats -r <name> --nosuchflag` returned the full
	// report and exit 0, alone with `roadmap list` among every command of the
	// CLI (rmp task 461).
	//
	// The refusal sits after requireRoadmap and before the database is opened,
	// which is the order every other roadmap-scoped command checks these in.
	if err := rejectUnknownFlags(remaining); err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Get roadmap statistics
	stats, err := database.GetRoadmapStats(ctx, roadmapName)
	if err != nil {
		return err
	}

	return utils.PrintJSON(stats)
}

// printStatsHelp prints the help text for the stats command.
func printStatsHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp stats [options]

Provides comprehensive statistics about a roadmap, including sprint and
task distribution, and average velocity across the last 5 closed sprints.

Options:
  -r, --roadmap <name>    REQUIRED. Target roadmap.
  -h, --help              Show this help message

Output (stdout JSON):
  {
    "roadmap": "project-name",
    "sprints": {
      "current": 5,
      "total": 12,
      "completed": 10,
      "pending": 2
    },
    "tasks": {
      "backlog": 15,
      "sprint": 8,
      "doing": 5,
      "testing": 3,
      "completed": 42
    },
    "average_velocity": 2.5
  }

Exit codes:
  0   Success
  2   Unrecognised flag, or a positional argument (this command takes none)
  3   No roadmap specified (-r missing)
  4   Roadmap not found

Examples:
  rmp stats -r myproject
`)
}
