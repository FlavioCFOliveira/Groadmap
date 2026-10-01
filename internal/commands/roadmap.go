// Package commands implements CLI command handlers for Groadmap.
package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
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
	fmt.Fprint(helpDst(), `Usage: rmp roadmap list

Lists every roadmap under ~/.roadmaps/. Each roadmap is the immediate
subdirectory of ~/.roadmaps/ whose name satisfies every roadmap name rule and
that contains a project.db database. Any other entry, including a directory
whose name no command could select, is skipped silently.

Aliases: ls.

Arguments: (none)
Options:   -h, --help

Output (stdout JSON):
  Array of objects, one per roadmap:
    [
      { "name": "<roadmap>", "path": "/home/<user>/.roadmaps/<roadmap>/project.db", "size": <bytes> },
      ...
    ]
  An empty array is returned when no roadmaps exist; exit is still 0.

`+exitCodesBlock("roadmap", "list")+`Examples:
  rmp roadmap list
  rmp roadmap ls
`)
}

// printRoadmapCreateHelp prints help for 'rmp roadmap create'.
func printRoadmapCreateHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp roadmap create <name>

Creates the roadmap home directory ~/.roadmaps/<name>/ (mode 0700) and the
SQLite database ~/.roadmaps/<name>/project.db (mode 0600) inside it.
Initialises the SQLite schema and records the current schema version.

Aliases: new.

Arguments:
  <name>   Required. Must match ^[a-z0-9_-]+$ and be at most 50 characters.
           Reserved Windows names (CON, PRN, COM1..9, ...) are rejected.

Options: -h, --help

Output (stdout JSON):
  {"name": "<name>"}

`+exitCodesBlock("roadmap", "create")+`Examples:
  rmp roadmap create mobile-app
  rmp roadmap new payment-api
`)
}

// printRoadmapRemoveHelp prints help for 'rmp roadmap remove'.
func printRoadmapRemoveHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp roadmap remove <name>

Deletes the entire roadmap home directory ~/.roadmaps/<name>/ recursively,
including project.db, its SQLite sidecars (project.db-wal, project.db-shm),
and any other per-roadmap files it contains. This is permanent — there is no
recovery flow other than restoring from your own backup.

A roadmap whose graph server (rmp graph serve) is running is not removed: the
command refuses with exit code 6 and removes nothing. Stop the server first.

Aliases: rm, delete.

Arguments:
  <name>   Required. The roadmap to remove. Must already exist.

Options: -h, --help

Output: empty (exit 0 on success).

`+exitCodesBlock("roadmap", "remove")+`Examples:
  rmp roadmap remove mobile-app
  rmp roadmap rm payment-api
  rmp roadmap delete legacy-project
`)
}

// roadmapList lists all roadmaps.
//
// The enumeration is utils.ListRoadmapEntries, the one rule the CLI and the web
// interface share: an immediate subdirectory of ~/.roadmaps/ whose name
// satisfies every roadmap name rule and that holds a project.db. Every other
// entry — a directory whose name no command could select among them — is
// skipped silently (SPEC/COMMANDS.md § List Roadmaps; SPEC/ARCHITECTURE.md
// § Directory Structure, location rule 9).
//
// args are the tokens written after the subcommand name. This command reads
// the data directory rather than one roadmap's database, so it takes neither
// the roadmap selector nor any flag of its own, and every token it can
// legitimately receive has already been consumed: the help tokens by the
// dispatcher, and a positional argument by the shared arity point. What
// survives to here is refused rather than discarded
// (SPEC/COMMANDS.md § List Roadmaps). It taking args at all is the fix: the
// handler used to be registered as a closure that dropped them, so
// `rmp roadmap list --nosuchflag` returned the whole listing and exit 0
// (rmp task 461).
func roadmapList(args []string) error {
	if err := rejectUnknownFlags(args); err != nil {
		return err
	}

	entries, err := utils.ListRoadmapEntries()
	if err != nil {
		return err
	}

	roadmaps := make([]models.Roadmap, len(entries))
	for i := range entries {
		roadmaps[i] = models.Roadmap{Name: entries[i].Name, Path: entries[i].Path, Size: entries[i].Size}
	}
	return utils.PrintJSON(roadmaps)
}

// roadmapCreate creates a new roadmap.
func roadmapCreate(args []string) error {
	args, strays := splitPositionals(args, 1)
	if len(args) == 0 {
		return fmt.Errorf("%w: roadmap name required", utils.ErrRequired)
	}

	name := args[0]

	// Validate name
	if err := utils.ValidateRoadmapName(name); err != nil {
		return err
	}

	// A "-"-prefixed token after <name> stands in no slot. It is refused after
	// the name is validated and before the roadmap's existence is checked, the
	// one check of this command that needs the roadmap
	// (SPEC/COMMANDS.md § Positional Arguments).
	if err := rejectUnknownFlags(strays); err != nil {
		return err
	}

	// An entry at the roadmap home that is neither a directory nor a symbolic
	// link is not a roadmap, so it is not refused as one that already exists;
	// the home cannot be created there either, and the entry is left exactly as
	// it was found (SPEC/COMMANDS.md § Create Roadmap).
	home, occupied, err := utils.RoadmapHomeOccupied(name)
	if err != nil {
		return err
	}
	if occupied {
		return fmt.Errorf("%w: cannot create roadmap %q: %s is occupied and is not a directory", utils.ErrIO, name, home)
	}

	// Check if exists. This is the refusal of the sequential case; the claim
	// on the name that decides a concurrent one is CreateRoadmapDatabase's own,
	// and it refuses a loser with the same line.
	exists, err := utils.RoadmapExists(name)
	if err != nil {
		return err
	}
	if exists {
		return utils.RoadmapAlreadyExistsError(name)
	}

	// Build the database whole under a temporary name and publish it under
	// project.db with one exclusive operation, so exactly one of any number of
	// concurrent creators wins and project.db never holds a partial schema
	// (SPEC/COMMANDS.md § Create Roadmap, "Creation is atomic against
	// concurrent creators").
	if err := db.CreateRoadmapDatabase(name); err != nil {
		return err
	}

	// Return JSON with name
	return utils.PrintJSON(map[string]string{"name": name})
}

// roadmapRemove removes a roadmap.
func roadmapRemove(args []string) error {
	args, strays := splitPositionals(args, 1)
	if len(args) == 0 {
		return fmt.Errorf("%w: roadmap name required", utils.ErrRequired)
	}

	name := args[0]

	// Validate name
	if err := utils.ValidateRoadmapName(name); err != nil {
		return err
	}

	// See roadmapCreate: refused before the roadmap's existence is checked, so a
	// refused invocation removes nothing.
	if err := rejectUnknownFlags(strays); err != nil {
		return err
	}

	// Check if exists. A regular file at the roadmap home is not a roadmap, so
	// it is refused as one that does not exist and nothing is removed
	// (SPEC/COMMANDS.md § Remove Roadmap).
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

	return removeRoadmapHome(name, dir)
}

// removeRoadmapHome deletes the roadmap home directory dir, refusing while a
// graph server runs for the roadmap (SPEC/COMMANDS.md § Remove Roadmap, "A
// roadmap whose graph server is running is not removed").
//
// The decision is made on the graph store's lock and on nothing else:
//
//  1. A roadmap with no graph/ directory has no server: a running server
//     created that directory before it took its lock. Nothing is created to
//     find this out, and the directory is deleted.
//  2. Otherwise the lock on graph/write.lock is taken exclusively and without
//     waiting. Another process holding it is the refusal, exit code 6, and
//     nothing is removed.
//  3. A lock taken is held until the deletion has completed, so no server can
//     start against the store while it is deleted: one that tries cannot take
//     the lock, and one that takes it afterwards finds the roadmap gone.
//  4. Any other failure of the attempt is utils.ErrGraphStore, exit code 1, and
//     nothing is removed.
func removeRoadmapHome(name, dir string) error {
	graphDir := filepath.Join(dir, "graph")
	info, err := os.Lstat(graphDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("%w: examining graph directory %s: %v", utils.ErrGraphStore, graphDir, err)
		}
		return removeAll(name, dir)
	}
	if !info.IsDir() {
		// Not a graph store: no server can hold a lock inside it.
		return removeAll(name, dir)
	}

	release, err := graphlock.TryExclusive(graphDir)
	if err != nil {
		if errors.Is(err, graphlock.ErrHeld) {
			return fmt.Errorf("%w: cannot remove roadmap %q: a graph server is running for it; stop the server first",
				utils.ErrValidation, name)
		}
		return err
	}

	if graphlock.LockFileRemovableWhileHeld {
		defer release()
		return removeAll(name, dir)
	}

	// The lock file cannot be deleted while its handle is open on this
	// platform: everything else is deleted under the hold, and the lock file
	// and the two directories that held it after the release.
	lockPath := filepath.Join(graphDir, graphlock.LockFileName)
	if err := removeAllExcept(dir, graphDir, lockPath); err != nil {
		release()
		return fmt.Errorf("removing roadmap %q: %w", name, err)
	}
	release()
	return removeAll(name, dir)
}

// removeAll deletes dir and everything below it.
func removeAll(name, dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing roadmap %q: %w", name, err)
	}
	return nil
}

// removeAllExcept deletes every entry of home except graphDir, and every entry
// of graphDir except keep.
func removeAllExcept(home, graphDir, keep string) error {
	for _, d := range []string{home, graphDir} {
		entries, err := os.ReadDir(d)
		if err != nil {
			return err
		}
		for _, e := range entries {
			p := filepath.Join(d, e.Name())
			if p == graphDir || p == keep {
				continue
			}
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		}
	}
	return nil
}

// requireRoadmap returns the roadmap name from -r flag or current selection.
//
// The selector, like every flag, takes at most one occurrence: a second one, in
// either spelling and whether or not it names the same roadmap, is refused
// with exit code 2 (SPEC/COMMANDS.md § Repeated Flags, rule 3). This is the
// step at which every roadmap-scoped command reads the selector, so the
// refusal sits here.
func requireRoadmap(args []string) (string, []string, error) {
	// Parse flags to find -r or --roadmap
	roadmapName := ""
	remaining := []string{}
	var seen utils.FlagOccurrences

	for i := 0; i < len(args); i++ {
		if args[i] == "-r" || args[i] == "--roadmap" {
			if err := seen.Note(roadmapFlagLong, args[i]); err != nil {
				return "", nil, err
			}
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
	fmt.Fprint(helpDst(), `Usage: rmp roadmap [command] [arguments]

Aliases: road.

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
  2   Required roadmap-name argument missing
  4   Roadmap not found (remove only)
  5   Roadmap already exists (create only)
  6   Invalid roadmap name (regex or length violation)
  127 Unknown subcommand

Examples:
  rmp roadmap list
  rmp roadmap create myproject
  rmp roadmap remove myproject
`)
}
