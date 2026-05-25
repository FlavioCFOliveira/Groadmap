package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// HandleStats handles the stats command.
func HandleStats(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		// Route through invokeHelpPrinter so the SPEC-mandated AI-agent
		// banner (SPEC/HELP.md § AI agent banner) is prepended uniformly.
		// `stats` is a leaf command and bypasses DispatchFamily's help
		// path, so the banner wrapping has to happen here explicitly.
		invokeHelpPrinter(printStatsHelp)
		return nil
	}

	roadmapName, _, err := requireRoadmap(args)
	if err != nil {
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
	fmt.Print(`Usage: rmp stats [options]

Description:
  Provides comprehensive statistics about a roadmap, including sprint and task distribution,
  and average velocity across the last 5 closed sprints.

Options:
  -r, --roadmap <name>    	REQUIRED. Target roadmap.
  -h, --help             	Show this help message

JSON Output:
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
  3   No roadmap specified (-r missing)
  4   Roadmap not found

Examples:
  rmp stats -r myproject
`)
}
