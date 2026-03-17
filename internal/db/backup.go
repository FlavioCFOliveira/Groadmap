// Package db provides SQLite database connectivity and operations.
package db

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
	_ "modernc.org/sqlite"
)

// BackupInfo represents information about a backup file.
type BackupInfo struct {
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
	RoadmapName string    `json:"roadmap_name"`
}

// MaxBackups is the maximum number of backups to keep per roadmap.
const MaxBackups = 10

// GetBackupDir returns the backup directory path.
func GetBackupDir() (string, error) {
	dataDir, err := utils.GetDataDir()
	if err != nil {
		return "", err
	}

	backupDir := filepath.Join(dataDir, "backups")
	return backupDir, nil
}

// EnsureBackupDir creates the backup directory if it doesn't exist.
func EnsureBackupDir() error {
	backupDir, err := GetBackupDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(backupDir, utils.DataDirPerm); err != nil {
		return fmt.Errorf("creating backup directory: %w", err)
	}

	// Ensure permissions
	if err := os.Chmod(backupDir, utils.DataDirPerm); err != nil {
		return fmt.Errorf("setting backup directory permissions: %w", err)
	}

	return nil
}

// BackupRoadmap creates a backup of a roadmap database.
// Returns the path to the created backup file.
func BackupRoadmap(roadmapName string) (string, error) {
	// Ensure backup directory exists
	if err := EnsureBackupDir(); err != nil {
		return "", err
	}

	// Get source database path
	sourcePath, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		return "", err
	}

	// Check if source exists
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		return "", fmt.Errorf("roadmap %q not found", roadmapName)
	}

	// Generate backup filename with timestamp
	timestamp := time.Now().UTC().Format("20060102_150405")
	backupFilename := fmt.Sprintf("%s_%s.db", roadmapName, timestamp)

	backupDir, err := GetBackupDir()
	if err != nil {
		return "", err
	}
	backupPath := filepath.Join(backupDir, backupFilename)

	// Create backup using SQLite backup API (file copy for now)
	if err := copyFile(sourcePath, backupPath); err != nil {
		return "", fmt.Errorf("creating backup: %w", err)
	}

	// Set permissions on backup file
	if err := os.Chmod(backupPath, utils.DBFilePerm); err != nil {
		return "", fmt.Errorf("setting backup permissions: %w", err)
	}

	// Rotate old backups
	if err := rotateBackups(roadmapName); err != nil {
		// Log but don't fail - backup was created successfully
		fmt.Fprintf(os.Stderr, "Warning: failed to rotate backups: %v\n", err)
	}

	return backupPath, nil
}

// RestoreRoadmap restores a roadmap from a backup file.
func RestoreRoadmap(roadmapName string, backupPath string) error {
	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return err
	}

	// Check if backup file exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", backupPath)
	}

	// Verify backup is a valid SQLite database
	if err := verifyBackup(backupPath); err != nil {
		return fmt.Errorf("backup verification failed: %w", err)
	}

	// Get target database path
	targetPath, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		return err
	}

	// Check if target exists (warn but allow overwrite)
	if _, err := os.Stat(targetPath); err == nil {
		// Target exists, create a safety backup
		timestamp := time.Now().UTC().Format("20060102_150405")
		safetyPath := targetPath + "." + timestamp + ".safety"
		if err := copyFile(targetPath, safetyPath); err != nil {
			return fmt.Errorf("creating safety backup: %w", err)
		}
		// Remove safety backup after successful restore
		defer os.Remove(safetyPath)
	}

	// Restore by copying backup to target
	if err := copyFile(backupPath, targetPath); err != nil {
		return fmt.Errorf("restoring backup: %w", err)
	}

	// Set permissions
	if err := os.Chmod(targetPath, utils.DBFilePerm); err != nil {
		return fmt.Errorf("setting restored database permissions: %w", err)
	}

	return nil
}

// ListBackups returns a list of available backups for a roadmap.
func ListBackups(roadmapName string) ([]BackupInfo, error) {
	backupDir, err := GetBackupDir()
	if err != nil {
		return nil, err
	}

	// Check if backup directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return []BackupInfo{}, nil
	}

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
		// Check if this is a backup for the specified roadmap
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".db") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Parse timestamp from filename
		createdAt := parseBackupTimestamp(name, roadmapName)

		backups = append(backups, BackupInfo{
			Name:        name,
			Size:        info.Size(),
			CreatedAt:   createdAt,
			RoadmapName: roadmapName,
		})
	}

	// Sort by creation time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// parseBackupTimestamp extracts the timestamp from a backup filename.
func parseBackupTimestamp(filename, roadmapName string) time.Time {
	// Remove roadmap name prefix and .db suffix
	timestampStr := strings.TrimPrefix(filename, roadmapName+"_")
	timestampStr = strings.TrimSuffix(timestampStr, ".db")

	// Parse timestamp
	t, err := time.Parse("20060102_150405", timestampStr)
	if err != nil {
		// Return zero time if parsing fails
		return time.Time{}
	}

	return t
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

	backupDir, err := GetBackupDir()
	if err != nil {
		return err
	}

	// Remove oldest backups
	for i := MaxBackups; i < len(backups); i++ {
		backupPath := filepath.Join(backupDir, backups[i].Name)
		if err := os.Remove(backupPath); err != nil {
			// Log but continue
			fmt.Fprintf(os.Stderr, "Warning: failed to remove old backup %s: %v\n", backupPath, err)
		}
	}

	return nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Create destination file with restricted permissions
	destFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, utils.DBFilePerm)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	return destFile.Close()
}

// verifyBackup verifies that a backup file is a valid SQLite database.
func verifyBackup(path string) error {
	// Open the database directly to verify it's valid
	sqlDB, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("invalid database file: %w", err)
	}
	defer sqlDB.Close()

	// Test connection
	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	// Try a simple query to verify integrity
	var result string
	err = sqlDB.QueryRow("PRAGMA integrity_check").Scan(&result)
	if err != nil {
		return fmt.Errorf("database integrity check failed: %w", err)
	}

	if result != "ok" {
		return fmt.Errorf("database integrity check returned: %s", result)
	}

	return nil
}
