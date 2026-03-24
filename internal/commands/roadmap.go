// Package commands implements CLI command handlers for Groadmap.
package commands

import (
	"fmt"
	"os"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// HandleRoadmap handles roadmap commands.
func HandleRoadmap(args []string) error {
	if len(args) == 0 {
		printRoadmapHelp()
		return nil
	}

	subcommand := args[0]

	// Check for help
	if subcommand == "-h" || subcommand == "--help" || subcommand == "help" {
		printRoadmapHelp()
		return nil
	}

	switch subcommand {
	case "list", "ls":
		return roadmapList()
	case "create", "new":
		return roadmapCreate(args[1:])
	case "remove", "rm", "delete":
		return roadmapRemove(args[1:])
	default:
		return fmt.Errorf("%w: unknown roadmap subcommand: %s", utils.ErrInvalidInput, subcommand)
	}
}

// roadmapList lists all roadmaps.
func roadmapList() error {
	names, err := utils.ListRoadmaps()
	if err != nil {
		return err
	}

	var roadmaps []models.Roadmap
	for _, name := range names {
		path, _ := utils.GetRoadmapPath(name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		roadmaps = append(roadmaps, models.Roadmap{
			Name: name,
			Path: path,
			Size: info.Size(),
		})
	}

	return utils.PrintJSON(roadmaps)
}

// roadmapCreate creates a new roadmap.
func roadmapCreate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: roadmap name required", utils.ErrRequired)
	}

	name := args[0]

	// Validate name
	if err := utils.ValidateRoadmapName(name); err != nil {
		return err
	}

	// Check if exists
	exists, err := utils.RoadmapExists(name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: roadmap %q already exists", utils.ErrAlreadyExists, name)
	}

	// Create database (this also creates schema)
	database, err := db.Open(name)
	if err != nil {
		return err
	}
	defer database.Close()

	// Return JSON with name
	return utils.PrintJSON(map[string]string{"name": name})
}

// roadmapRemove removes a roadmap.
func roadmapRemove(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: roadmap name required", utils.ErrRequired)
	}

	name := args[0]

	// Validate name
	if err := utils.ValidateRoadmapName(name); err != nil {
		return err
	}

	// Check if exists
	exists, err := utils.RoadmapExists(name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: roadmap %q not found", utils.ErrNotFound, name)
	}

	// Get path and delete
	path, err := utils.GetRoadmapPath(name)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("removing roadmap: %w", err)
	}

	// Remove SQLite WAL files if present; errors are intentionally ignored
	// because these files may not exist and their absence is not an error.
	_ = os.Remove(path + "-shm")
	_ = os.Remove(path + "-wal")

	return nil
}

// requireRoadmap returns the roadmap name from -r flag or current selection.
func requireRoadmap(args []string) (string, []string, error) {
	// Parse flags to find -r or --roadmap
	roadmapName := ""
	remaining := []string{}

	for i := 0; i < len(args); i++ {
		if args[i] == "-r" || args[i] == "--roadmap" {
			if i+1 < len(args) {
				roadmapName = args[i+1]
				i++ // Skip the value
			}
		} else {
			remaining = append(remaining, args[i])
		}
	}

	if roadmapName == "" {
		return "", nil, fmt.Errorf("%w: use -r <name> or --roadmap <name>", utils.ErrNoRoadmap)
	}

	return roadmapName, remaining, nil
}

// printRoadmapHelp prints roadmap command help.
func printRoadmapHelp() {
	fmt.Print(`Usage: rmp roadmap [command] [arguments]

Commands:
  list, ls                   List all roadmaps
  create, new <name>         Create a new roadmap
  remove, rm <name>          Remove a roadmap

Options:
  -h, --help                 Show this help message

Examples:
  rmp roadmap list
  rmp roadmap create myproject
`)
}
