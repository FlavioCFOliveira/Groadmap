package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// HandleBackup handles backup commands.
func HandleBackup(args []string) error {
	if len(args) == 0 {
		printBackupHelp()
		return nil
	}

	subcommand := args[0]

	switch subcommand {
	case "create":
		return backupCreate(args[1:])
	case "restore":
		return backupRestore(args[1:])
	case "list":
		return backupList(args[1:])
	default:
		return fmt.Errorf("unknown backup subcommand: %s", subcommand)
	}
}

// backupCreate creates a backup of a roadmap.
func backupCreate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("roadmap name required")
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
		return fmt.Errorf("roadmap %q not found", roadmapName)
	}

	// Create backup
	backupPath, err := db.BackupRoadmap(roadmapName)
	if err != nil {
		return err
	}

	fmt.Printf("Backup created: %s\n", backupPath)
	return nil
}

// backupRestore restores a roadmap from a backup.
func backupRestore(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("roadmap name and backup file required")
	}

	roadmapName := args[0]
	backupPath := args[1]

	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return err
	}

	// Resolve backup path if relative
	if !filepath.IsAbs(backupPath) {
		// Check if it's just a filename in the backups directory
		backupDir, err := db.GetBackupDir()
		if err != nil {
			return err
		}
		fullPath := filepath.Join(backupDir, backupPath)
		if _, err := os.Stat(fullPath); err == nil {
			backupPath = fullPath
		}
	}

	// Restore backup
	if err := db.RestoreRoadmap(roadmapName, backupPath); err != nil {
		return err
	}

	fmt.Printf("Roadmap %q restored from %s\n", roadmapName, backupPath)
	return nil
}

// backupList lists backups for a roadmap.
func backupList(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("roadmap name required")
	}

	roadmapName := args[0]

	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return err
	}

	// Get backups
	backups, err := db.ListBackups(roadmapName)
	if err != nil {
		return err
	}

	if len(backups) == 0 {
		fmt.Printf("No backups found for roadmap %q\n", roadmapName)
		return nil
	}

	fmt.Printf("Backups for roadmap %q:\n", roadmapName)
	fmt.Printf("%-30s %15s %20s\n", "Name", "Size", "Created At")
	fmt.Println(string(make([]byte, 70)))

	for _, backup := range backups {
		size := formatSize(backup.Size)
		fmt.Printf("%-30s %15s %20s\n",
			backup.Name,
			size,
			backup.CreatedAt.Format("2006-01-02 15:04:05"),
		)
	}

	return nil
}

// formatSize formats a file size for human-readable output.
func formatSize(size int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case size >= GB:
		return fmt.Sprintf("%.2f GB", float64(size)/GB)
	case size >= MB:
		return fmt.Sprintf("%.2f MB", float64(size)/MB)
	case size >= KB:
		return fmt.Sprintf("%.2f KB", float64(size)/KB)
	default:
		return fmt.Sprintf("%d B", size)
	}
}

// printBackupHelp prints help for backup commands.
func printBackupHelp() {
	fmt.Println("Usage: rmp roadmap backup [command] [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  create <name>              Create a backup of a roadmap")
	fmt.Println("  restore <name> <file>    Restore a roadmap from backup")
	fmt.Println("  list <name>              List backups for a roadmap")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  rmp roadmap backup create myroadmap")
	fmt.Println("  rmp roadmap backup restore myroadmap myroadmap_20260317_120000.db")
	fmt.Println("  rmp roadmap backup list myroadmap")
}
