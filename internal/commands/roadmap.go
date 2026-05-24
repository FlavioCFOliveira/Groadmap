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

// HandleRoadmap handles roadmap commands via the central registry.
// See HandleTask for the rationale; the dispatch lives in
// Command.DispatchFamily.
func HandleRoadmap(args []string) error {
	return dispatchFamily("roadmap", args)
}

// printRoadmapListHelp prints help for 'rmp roadmap list'.
func printRoadmapListHelp() {
	fmt.Print(`Usage: rmp roadmap list

Lists every roadmap database file found under ~/.roadmaps/.

Arguments: (none)
Options:   -h, --help

Output (stdout JSON):
  Array of objects, one per roadmap:
    [
      { "name": "<roadmap>", "path": "/home/<user>/.roadmaps/<roadmap>.db", "size": <bytes> },
      ...
    ]
  An empty array is returned when no roadmaps exist; exit is still 0.

Exit codes:
  0   Success

Examples:
  rmp roadmap list
  rmp roadmap ls
`)
}

// printRoadmapCreateHelp prints help for 'rmp roadmap create'.
func printRoadmapCreateHelp() {
	fmt.Print(`Usage: rmp roadmap create <name>

Creates a new roadmap database at ~/.roadmaps/<name>.db (mode 0600).
Initialises the SQLite schema and records the current schema version.

Arguments:
  <name>   Required. Must match ^[a-z0-9_-]+$ and be at most 50 characters.
           Reserved Windows names (CON, PRN, COM1..9, ...) are rejected.

Options: -h, --help

Output (stdout JSON):
  {"name": "<name>"}

Exit codes:
  0   Success
  5   A roadmap with that name already exists
  6   Invalid roadmap name (bad regex match, length, or reserved word)

Examples:
  rmp roadmap create mobile-app
  rmp roadmap new payment-api
`)
}

// printRoadmapRemoveHelp prints help for 'rmp roadmap remove'.
func printRoadmapRemoveHelp() {
	fmt.Print(`Usage: rmp roadmap remove <name>

Deletes the roadmap database file ~/.roadmaps/<name>.db AND its SQLite
sidecar files (-wal, -shm) if present. This is permanent — there is no
recovery flow other than restoring from your own backup.

Aliases: rm, delete.

Arguments:
  <name>   Required. The roadmap to remove. Must already exist.

Options: -h, --help

Output: empty (exit 0 on success).

Exit codes:
  0   Success
  4   Roadmap not found
  6   Invalid roadmap name

Examples:
  rmp roadmap remove mobile-app
  rmp roadmap rm payment-api
  rmp roadmap delete legacy-project
`)
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

Roadmap names must match the regex ^[a-z0-9_-]+$ and not exceed 50 characters.
Each roadmap is stored as a SQLite database in ~/.roadmaps/<name>.db (mode 0600).

Commands:
  list, ls                       List all roadmaps
  create, new <name>             Create a new roadmap
  remove, rm, delete <name>      Remove a roadmap (irreversible)

Options:
  -h, --help                     Show this help message

Output (stdout JSON):
  list      Array of objects { "name", "path", "size" }
  create    {"name": "<name>"}
  remove    Empty (exit 0 on success)

Exit codes:
  0   Success
  4   Roadmap not found (remove only)
  5   Roadmap already exists (create only)
  6   Invalid roadmap name (regex or length violation)

Examples:
  rmp roadmap list
  rmp roadmap create myproject
  rmp roadmap remove myproject
`)
}
