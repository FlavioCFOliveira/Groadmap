// Package db provides SQLite database connectivity and operations.
package db

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

const (
	// BackupDirName is the subdirectory for backups.
	BackupDirName = "backups"

	// MaxBackups is the maximum number of backups to keep per roadmap.
	MaxBackups = 10

	// BackupTimeFormat is the timestamp format for backup files.
	BackupTimeFormat = "20060102_150405"
)

// BackupInfo contains metadata about a backup.
type BackupInfo struct {
	Name      string    `json:"name"`
	Roadmap   string    `json:"roadmap"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size"`
	Path      string    `json:"path"`
}

// Backup creates a backup of the specified roadmap.
// Returns the path to the created backup file.
func Backup(roadmapName string) (string, error) {
	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return "", err
	}

	// Check if roadmap exists
	exists, err := utils.RoadmapExists(roadmapName)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("%w: roadmap %q", utils.ErrNotFound, roadmapName)
	}

	// Get source database path
	sourcePath, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		return "", err
	}

	// Ensure backup directory exists
	backupDir, err := getBackupDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(backupDir, utils.DataDirPerm); err != nil {
		return "", fmt.Errorf("creating backup directory: %w", err)
	}

	// Generate backup filename with timestamp
	timestamp := time.Now().UTC().Format(BackupTimeFormat)
	backupName := fmt.Sprintf("%s_%s.db", roadmapName, timestamp)
	backupPath := filepath.Join(backupDir, backupName)

	// Create backup using file copy (SQLite backup API requires CGO)
	// For the modernc.org/sqlite driver, we use file copy which is safe
	// when the database is not being written to
	if err := copyDatabaseFile(sourcePath, backupPath); err != nil {
		return "", fmt.Errorf("creating backup: %w", err)
	}

	// Set permissions on backup file
	if err := os.Chmod(backupPath, utils.DBFilePerm); err != nil {
		return "", fmt.Errorf("setting backup permissions: %w", err)
	}

	// Rotate old backups
	if err := rotateBackups(roadmapName); err != nil {
		// Log but don't fail - backup was created successfully
		// This is a non-critical error
		_ = err
	}

	return backupPath, nil
}

// Restore restores a roadmap from a backup file.
// If targetName is empty, the original roadmap name is used.
func Restore(backupPath string, targetName string) error {
	// Validate backup path
	if backupPath == "" {
		return fmt.Errorf("%w: backup path required", utils.ErrRequired)
	}

	// Check if backup file exists
	info, err := os.Stat(backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: backup file %q", utils.ErrNotFound, backupPath)
		}
		return fmt.Errorf("checking backup file: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("backup path is a directory, not a file: %s", backupPath)
	}

	// Validate backup integrity (basic check - file size > 0)
	if info.Size() == 0 {
		return fmt.Errorf("backup file is empty: %s", backupPath)
	}

	// Determine target roadmap name
	if targetName == "" {
		// Extract roadmap name from backup filename
		// Format: roadmapname_20060102_150405.db
		base := filepath.Base(backupPath)
		base = strings.TrimSuffix(base, ".db")
		parts := strings.Split(base, "_")
		if len(parts) >= 3 {
			// Join all parts except the last two (date and time)
			targetName = strings.Join(parts[:len(parts)-2], "_")
		} else {
			return fmt.Errorf("cannot extract roadmap name from backup filename: %s", backupPath)
		}
	}

	// Validate target name
	if err := utils.ValidateRoadmapName(targetName); err != nil {
		return err
	}

	// Ensure data directory exists
	if err := utils.EnsureDataDir(); err != nil {
		return err
	}

	// Get target database path
	targetPath, err := utils.GetRoadmapPath(targetName)
	if err != nil {
		return err
	}

	// Check if target already exists
	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("%w: roadmap %q already exists, remove it first or use a different name", utils.ErrAlreadyExists, targetName)
	}

	// Copy backup to target location
	if err := copyDatabaseFile(backupPath, targetPath); err != nil {
		return fmt.Errorf("restoring backup: %w", err)
	}

	// Set permissions on restored database
	if err := os.Chmod(targetPath, utils.DBFilePerm); err != nil {
		return fmt.Errorf("setting database permissions: %w", err)
	}

	// Verify permissions
	if err := utils.VerifyPermissions(targetPath, utils.DBFilePerm); err != nil {
		return fmt.Errorf("verifying database permissions: %w", err)
	}

	return nil
}

// ListBackups returns a list of available backups for a roadmap.
func ListBackups(roadmapName string) ([]BackupInfo, error) {
	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return nil, err
	}

	backupDir, err := getBackupDir()
	if err != nil {
		return nil, err
	}

	// Check if backup directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return []BackupInfo{}, nil
	}

	// Read backup directory
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, fmt.Errorf("reading backup directory: %w", err)
	}

	var backups []BackupInfo
	prefix := roadmapName + "_"

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".db") {
			continue
		}

		// Check if this backup belongs to the specified roadmap
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Parse timestamp from filename
		createdAt := parseBackupTimestamp(name)

		backups = append(backups, BackupInfo{
			Name:      name,
			Roadmap:   roadmapName,
			CreatedAt: createdAt,
			Size:      info.Size(),
			Path:      filepath.Join(backupDir, name),
		})
	}

	// Sort by creation time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// getBackupDir returns the path to the backup directory.
func getBackupDir() (string, error) {
	dataDir, err := utils.GetDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, BackupDirName), nil
}

// copyDatabaseFile copies a database file safely.
// Uses a temporary file and rename for atomicity.
func copyDatabaseFile(source, destination string) error {
	// Open source file
	srcFile, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("opening source database: %w", err)
	}
	defer srcFile.Close()

	// Create temporary file in the same directory as destination
	dir := filepath.Dir(destination)
	tempFile, err := os.CreateTemp(dir, ".backup_*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary file: %w", err)
	}
	tempPath := tempFile.Name()

	// Ensure cleanup on error
	cleanup := func() {
		tempFile.Close()
		os.Remove(tempPath)
	}

	// Copy data
	if _, err := io.Copy(tempFile, srcFile); err != nil {
		cleanup()
		return fmt.Errorf("copying database: %w", err)
	}

	// Close files
	if err := tempFile.Close(); err != nil {
		cleanup()
		return fmt.Errorf("closing temporary file: %w", err)
	}
	srcFile.Close()

	// Atomic rename
	if err := os.Rename(tempPath, destination); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("renaming backup file: %w", err)
	}

	return nil
}

// rotateBackups removes old backups, keeping only the most recent MaxBackups.
func rotateBackups(roadmapName string) error {
	backups, err := ListBackups(roadmapName)
	if err != nil {
		return err
	}

	if len(backups) <= MaxBackups {
		return nil
	}

	// Remove oldest backups
	for i := MaxBackups; i < len(backups); i++ {
		if err := os.Remove(backups[i].Path); err != nil {
			// Log but continue - don't fail rotation
			continue
		}
	}

	return nil
}

// parseBackupTimestamp extracts the timestamp from a backup filename.
// Expected format: roadmapname_20060102_150405.db
func parseBackupTimestamp(filename string) time.Time {
	// Remove .db extension
	base := strings.TrimSuffix(filename, ".db")

	// Split by underscore
	parts := strings.Split(base, "_")
	if len(parts) < 3 {
		return time.Time{}
	}

	// Last two parts should be date and time
	dateStr := parts[len(parts)-2]
	timeStr := parts[len(parts)-1]

	// Parse timestamp
	timestampStr := dateStr + "_" + timeStr
	t, err := time.Parse(BackupTimeFormat, timestampStr)
	if err != nil {
		return time.Time{}
	}

	return t
}
