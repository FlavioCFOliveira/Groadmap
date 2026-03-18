// Package commands implements CLI command handlers for Groadmap.
package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// HandleBackup handles backup-related commands.
func HandleBackup(args []string) error {
	if len(args) == 0 {
		printBackupHelp()
		return nil
	}

	subcommand := args[0]

	// Check for help
	if subcommand == "-h" || subcommand == "--help" || subcommand == "help" {
		printBackupHelp()
		return nil
	}

	switch subcommand {
	case "backup":
		return backupCreate(args[1:])
	case "restore":
		return backupRestore(args[1:])
	case "list-backups":
		return backupList(args[1:])
	default:
		return fmt.Errorf("unknown backup subcommand: %s", subcommand)
	}
}

// backupCreate creates a backup of a roadmap.
func backupCreate(args []string) error {
	// Parse arguments
	if len(args) == 0 {
		return fmt.Errorf("%w: roadmap name required", utils.ErrRequired)
	}

	roadmapName := args[0]

	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return err
	}

	// Check if roadmap exists
	exists, err := utils.RoadmapExists(roadmapName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: roadmap %q", utils.ErrNotFound, roadmapName)
	}

	// Create backup
	backupPath, err := db.Backup(roadmapName)
	if err != nil {
		return err
	}

	// Get backup info
	info, err := os.Stat(backupPath)
	if err != nil {
		return fmt.Errorf("getting backup info: %w", err)
	}

	// Return JSON result
	return utils.PrintJSON(map[string]interface{}{
		"roadmap": roadmapName,
		"backup":  filepath.Base(backupPath),
		"path":    backupPath,
		"size":    info.Size(),
	})
}

// backupRestore restores a roadmap from a backup file.
func backupRestore(args []string) error {
	// Parse arguments
	if len(args) < 2 {
		return fmt.Errorf("%w: roadmap name and backup file required", utils.ErrRequired)
	}

	roadmapName := args[0]
	backupPath := args[1]

	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return err
	}

	// Check if roadmap already exists
	exists, err := utils.RoadmapExists(roadmapName)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: roadmap %q already exists, remove it first or use a different name", utils.ErrAlreadyExists, roadmapName)
	}

	// If backup path is not absolute, try to resolve it
	if !filepath.IsAbs(backupPath) {
		// Check if it's just a filename in the backups directory
		dataDir, err := utils.GetDataDir()
		if err == nil {
			backupDir := filepath.Join(dataDir, "backups")
			fullPath := filepath.Join(backupDir, backupPath)
			if _, err := os.Stat(fullPath); err == nil {
				backupPath = fullPath
			}
		}
	}

	// Restore from backup
	if err := db.Restore(backupPath, roadmapName); err != nil {
		return err
	}

	// Return JSON result
	return utils.PrintJSON(map[string]string{
		"roadmap": roadmapName,
		"source":  backupPath,
	})
}

// backupList lists available backups for a roadmap.
func backupList(args []string) error {
	// Parse arguments
	if len(args) == 0 {
		return fmt.Errorf("%w: roadmap name required", utils.ErrRequired)
	}

	roadmapName := args[0]

	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return err
	}

	// Get list of backups
	backups, err := db.ListBackups(roadmapName)
	if err != nil {
		return err
	}

	// Return JSON result
	return utils.PrintJSON(backups)
}

// printBackupHelp prints backup command help.
func printBackupHelp() {
	fmt.Print(`Usage: rmp roadmap backup [command] [arguments]

Commands:
  backup <name>              Create a backup of a roadmap
  restore <name> <backup>    Restore roadmap from backup
  list-backups <name>        List available backups for a roadmap

Options:
  -h, --help                 Show this help message

Examples:
  rmp roadmap backup myproject
  rmp roadmap restore myproject myproject_20260115_143022.db
  rmp roadmap list-backups myproject

Backup files are stored in ~/.roadmaps/backups/
Backups are automatically rotated (last 10 are kept).
`)
}
