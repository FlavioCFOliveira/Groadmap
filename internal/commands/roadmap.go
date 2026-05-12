// Package commands implements CLI command handlers for Groadmap.
package commands

import (
	"fmt"
	"os"
	"path/filepath"

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
//
// Reads the data directory once and asks each DirEntry for its Info();
// on Linux/macOS the directory read already filled in the file metadata,
// so we avoid one stat(2) syscall per roadmap that os.Stat would issue.
func roadmapList() error {
	dataDir, err := utils.GetDataDir()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return utils.PrintJSON([]models.Roadmap{})
		}
		return fmt.Errorf("reading data directory: %w", err)
	}

	roadmaps := make([]models.Roadmap, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".db" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue // file may have been removed between ReadDir and Info
		}
		name := entry.Name()[:len(entry.Name())-len(".db")]
		roadmaps = append(roadmaps, models.Roadmap{
			Name: name,
			Path: filepath.Join(dataDir, entry.Name()),
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
