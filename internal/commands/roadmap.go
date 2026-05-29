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

Lists every roadmap under ~/.roadmaps/. Each roadmap is the immediate
subdirectory of ~/.roadmaps/ that contains a project.db database.

Arguments: (none)
Options:   -h, --help

Output (stdout JSON):
  Array of objects, one per roadmap:
    [
      { "name": "<roadmap>", "path": "/home/<user>/.roadmaps/<roadmap>/project.db", "size": <bytes> },
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

Creates the roadmap home directory ~/.roadmaps/<name>/ (mode 0700) and the
SQLite database ~/.roadmaps/<name>/project.db (mode 0600) inside it.
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

Deletes the entire roadmap home directory ~/.roadmaps/<name>/ recursively,
including project.db, its SQLite sidecars (project.db-wal, project.db-shm),
and any other per-roadmap files it contains. This is permanent — there is no
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
// Under the current layout each roadmap is an immediate subdirectory of
// ~/.roadmaps/ that contains a project.db database. The data directory is read
// once; for every candidate subdirectory we stat its project.db to obtain the
// reported size. A subdirectory without a project.db (or one that disappears
// between ReadDir and Stat) is silently skipped — it is not a roadmap.
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
		if !entry.IsDir() {
			continue
		}
		dbPath := filepath.Join(dataDir, entry.Name(), utils.DBFileName)
		info, err := os.Stat(dbPath)
		if err != nil || info.IsDir() {
			continue // not a roadmap home directory
		}
		roadmaps = append(roadmaps, models.Roadmap{
			Name: entry.Name(),
			Path: dbPath,
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

	// Remove the entire roadmap home directory recursively. This naturally
	// covers project.db, its SQLite sidecars (project.db-wal/-shm), and any
	// other per-roadmap files the directory holds.
	dir, err := utils.GetRoadmapDir(name)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing roadmap %q: %w", name, err)
	}

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
Each roadmap lives in its own home directory ~/.roadmaps/<name>/ (mode 0700),
with the SQLite database at ~/.roadmaps/<name>/project.db (mode 0600).

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
