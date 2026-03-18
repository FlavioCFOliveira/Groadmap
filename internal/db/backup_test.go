// Package db provides SQLite database connectivity and operations.
package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== BACKUP TESTS ====================

func TestBackup_Success(t *testing.T) {
	// Create a test roadmap
	roadmapName := "testbackup" + time.Now().Format("150405")

	// Create the roadmap
	db, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	db.Close()

	// Cleanup after test
	defer func() {
		utils.GetRoadmapPath(roadmapName)
		if path, err := utils.GetRoadmapPath(roadmapName); err == nil {
			os.Remove(path)
		}
		// Clean up backups
		if backupDir, err := getBackupDir(); err == nil {
			entries, _ := os.ReadDir(backupDir)
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), roadmapName+"_") {
					os.Remove(filepath.Join(backupDir, entry.Name()))
				}
			}
		}
	}()

	// Create backup
	backupPath, err := Backup(roadmapName)
	if err != nil {
		t.Fatalf("failed to create backup: %v", err)
	}

	// Verify backup file exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Errorf("backup file was not created: %s", backupPath)
	}

	// Verify backup filename format
	base := filepath.Base(backupPath)
	if !strings.HasPrefix(base, roadmapName+"_") {
		t.Errorf("backup filename should start with roadmap name: %s", base)
	}
	if !strings.HasSuffix(base, ".db") {
		t.Errorf("backup filename should have .db extension: %s", base)
	}
}

func TestBackup_RoadmapNotFound(t *testing.T) {
	// Try to backup non-existent roadmap
	_, err := Backup("nonexistentroadmap12345")
	if err == nil {
		t.Error("expected error for non-existent roadmap")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestBackup_InvalidName(t *testing.T) {
	// Try to backup with invalid name
	_, err := Backup("../etc/passwd")
	if err == nil {
		t.Error("expected error for invalid roadmap name")
	}
}

func TestRestore_Success(t *testing.T) {
	// Create a test roadmap
	roadmapName := "testrestore" + time.Now().Format("150405")
	targetName := roadmapName + "_restored"

	// Create the roadmap
	db, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	db.Close()

	// Cleanup after test
	defer func() {
		// Clean up original
		if path, err := utils.GetRoadmapPath(roadmapName); err == nil {
			os.Remove(path)
		}
		// Clean up restored
		if path, err := utils.GetRoadmapPath(targetName); err == nil {
			os.Remove(path)
		}
		// Clean up backups
		if backupDir, err := getBackupDir(); err == nil {
			entries, _ := os.ReadDir(backupDir)
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), roadmapName+"_") {
					os.Remove(filepath.Join(backupDir, entry.Name()))
				}
			}
		}
	}()

	// Create backup
	backupPath, err := Backup(roadmapName)
	if err != nil {
		t.Fatalf("failed to create backup: %v", err)
	}

	// Remove original roadmap
	if path, err := utils.GetRoadmapPath(roadmapName); err == nil {
		os.Remove(path)
	}

	// Restore from backup with new name
	err = Restore(backupPath, targetName)
	if err != nil {
		t.Fatalf("failed to restore backup: %v", err)
	}

	// Verify restored roadmap exists
	exists, err := utils.RoadmapExists(targetName)
	if err != nil {
		t.Fatalf("failed to check restored roadmap: %v", err)
	}
	if !exists {
		t.Error("restored roadmap does not exist")
	}
}

func TestRestore_BackupNotFound(t *testing.T) {
	// Try to restore from non-existent backup
	err := Restore("/nonexistent/path/backup.db", "target")
	if err == nil {
		t.Error("expected error for non-existent backup")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestRestore_TargetExists(t *testing.T) {
	// Create a test roadmap
	roadmapName := "testrestoreexists" + time.Now().Format("150405")

	// Create the roadmap
	db, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	db.Close()

	// Cleanup after test
	defer func() {
		if path, err := utils.GetRoadmapPath(roadmapName); err == nil {
			os.Remove(path)
		}
		if backupDir, err := getBackupDir(); err == nil {
			entries, _ := os.ReadDir(backupDir)
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), roadmapName+"_") {
					os.Remove(filepath.Join(backupDir, entry.Name()))
				}
			}
		}
	}()

	// Create backup
	backupPath, err := Backup(roadmapName)
	if err != nil {
		t.Fatalf("failed to create backup: %v", err)
	}

	// Try to restore with same name (should fail)
	err = Restore(backupPath, roadmapName)
	if err == nil {
		t.Error("expected error when target already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestListBackups(t *testing.T) {
	// Create a test roadmap
	roadmapName := "testlistbackups" + time.Now().Format("150405")

	// Create the roadmap
	db, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	db.Close()

	// Cleanup after test
	defer func() {
		if path, err := utils.GetRoadmapPath(roadmapName); err == nil {
			os.Remove(path)
		}
		if backupDir, err := getBackupDir(); err == nil {
			entries, _ := os.ReadDir(backupDir)
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), roadmapName+"_") {
					os.Remove(filepath.Join(backupDir, entry.Name()))
				}
			}
		}
	}()

	// Initially should have no backups
	backups, err := ListBackups(roadmapName)
	if err != nil {
		t.Fatalf("failed to list backups: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("expected 0 backups initially, got %d", len(backups))
	}

	// Create a backup
	_, err = Backup(roadmapName)
	if err != nil {
		t.Fatalf("failed to create backup: %v", err)
	}

	// Should have 1 backup
	backups, err = ListBackups(roadmapName)
	if err != nil {
		t.Fatalf("failed to list backups: %v", err)
	}
	if len(backups) != 1 {
		t.Errorf("expected 1 backup, got %d", len(backups))
	}

	// Verify backup info
	if backups[0].Roadmap != roadmapName {
		t.Errorf("expected roadmap name %q, got %q", roadmapName, backups[0].Roadmap)
	}
	if backups[0].Size == 0 {
		t.Error("expected non-zero backup size")
	}
}

func TestBackupRotation(t *testing.T) {
	// Create a test roadmap
	roadmapName := "testrotation" + time.Now().Format("150405")

	// Create the roadmap
	db, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	db.Close()

	// Cleanup after test
	defer func() {
		if path, err := utils.GetRoadmapPath(roadmapName); err == nil {
			os.Remove(path)
		}
		if backupDir, err := getBackupDir(); err == nil {
			entries, _ := os.ReadDir(backupDir)
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), roadmapName+"_") {
					os.Remove(filepath.Join(backupDir, entry.Name()))
				}
			}
		}
	}()

	// Create more than MaxBackups backups
	for i := 0; i < MaxBackups+3; i++ {
		// Small delay to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
		_, err := Backup(roadmapName)
		if err != nil {
			t.Fatalf("failed to create backup %d: %v", i, err)
		}
	}

	// Should have at most MaxBackups
	backups, err := ListBackups(roadmapName)
	if err != nil {
		t.Fatalf("failed to list backups: %v", err)
	}
	if len(backups) > MaxBackups {
		t.Errorf("expected at most %d backups, got %d", MaxBackups, len(backups))
	}
}

func TestParseBackupTimestamp(t *testing.T) {
	tests := []struct {
		filename string
		valid    bool
	}{
		{"myproject_20260115_143022.db", true},
		{"myproject_20260115_143022", true},         // no extension
		{"myproject_20260115.db", false},            // missing time
		{"myproject.db", false},                     // no timestamp
		{"myproject_2026_01_15_14_30_22.db", false}, // wrong format
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := parseBackupTimestamp(tt.filename)
			if tt.valid && result.IsZero() {
				t.Errorf("expected valid timestamp for %s", tt.filename)
			}
			if !tt.valid && !result.IsZero() {
				t.Errorf("expected zero timestamp for %s", tt.filename)
			}
		})
	}
}

func TestGetBackupDir(t *testing.T) {
	backupDir, err := getBackupDir()
	if err != nil {
		t.Fatalf("failed to get backup dir: %v", err)
	}

	// Should contain "backups" in path
	if !strings.Contains(backupDir, "backups") {
		t.Errorf("backup dir should contain 'backups': %s", backupDir)
	}
}
