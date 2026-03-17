package db

import (
	"os"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

func TestGetBackupDir(t *testing.T) {
	backupDir, err := GetBackupDir()
	if err != nil {
		t.Fatalf("GetBackupDir failed: %v", err)
	}

	if !strings.Contains(backupDir, ".roadmaps") {
		t.Error("Backup dir should contain .roadmaps")
	}

	if !strings.Contains(backupDir, "backups") {
		t.Error("Backup dir should contain 'backups'")
	}
}

func TestEnsureBackupDir(t *testing.T) {
	// Clean up after test
	defer func() {
		backupDir, _ := GetBackupDir()
		os.RemoveAll(backupDir)
	}()

	err := EnsureBackupDir()
	if err != nil {
		t.Fatalf("EnsureBackupDir failed: %v", err)
	}

	backupDir, _ := GetBackupDir()
	info, err := os.Stat(backupDir)
	if err != nil {
		t.Fatalf("Backup dir not created: %v", err)
	}

	if !info.IsDir() {
		t.Error("Backup dir is not a directory")
	}
}

func TestBackupAndRestore(t *testing.T) {
	// Create a test roadmap
	testRoadmapName := "testbackuproadmap"
	defer func() {
		// Cleanup
		path, _ := utils.GetRoadmapPath(testRoadmapName)
		os.Remove(path)
		backupDir, _ := GetBackupDir()
		os.RemoveAll(backupDir)
	}()

	// Create a test database
	db, err := Open(testRoadmapName)
	if err != nil {
		t.Fatalf("Failed to create test roadmap: %v", err)
	}
	db.Close()

	// Create backup
	backupPath, err := BackupRoadmap(testRoadmapName)
	if err != nil {
		t.Fatalf("BackupRoadmap failed: %v", err)
	}

	// Verify backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("Backup file was not created")
	}

	// Verify backup has correct extension
	if !strings.HasSuffix(backupPath, ".db") {
		t.Errorf("Backup should have .db extension: %s", backupPath)
	}

	// Test restore
	restoreName := "testrestore"
	defer func() {
		path, _ := utils.GetRoadmapPath(restoreName)
		os.Remove(path)
	}()

	err = RestoreRoadmap(restoreName, backupPath)
	if err != nil {
		t.Fatalf("RestoreRoadmap failed: %v", err)
	}

	// Verify restored database exists
	restoredPath, _ := utils.GetRoadmapPath(restoreName)
	if _, err := os.Stat(restoredPath); os.IsNotExist(err) {
		t.Error("Restored database was not created")
	}
}

func TestListBackups(t *testing.T) {
	testRoadmapName := "testlistbackups"
	defer func() {
		// Cleanup
		path, _ := utils.GetRoadmapPath(testRoadmapName)
		os.Remove(path)
		backupDir, _ := GetBackupDir()
		os.RemoveAll(backupDir)
	}()

	// Create a test database
	db, err := Open(testRoadmapName)
	if err != nil {
		t.Fatalf("Failed to create test roadmap: %v", err)
	}
	db.Close()

	// Initially should have no backups
	backups, err := ListBackups(testRoadmapName)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("Expected 0 backups initially, got %d", len(backups))
	}

	// Create a backup
	_, err = BackupRoadmap(testRoadmapName)
	if err != nil {
		t.Fatalf("BackupRoadmap failed: %v", err)
	}

	// Should have 1 backup
	backups, err = ListBackups(testRoadmapName)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(backups) != 1 {
		t.Errorf("Expected 1 backup, got %d", len(backups))
	}

	// Verify backup info
	backup := backups[0]
	if backup.RoadmapName != testRoadmapName {
		t.Errorf("Expected roadmap name %q, got %q", testRoadmapName, backup.RoadmapName)
	}
	if backup.Size == 0 {
		t.Error("Backup size should be > 0")
	}
	if backup.Name == "" {
		t.Error("Backup name should not be empty")
	}
}

func TestBackupRotation(t *testing.T) {
	testRoadmapName := "testrotation"
	defer func() {
		// Cleanup
		path, _ := utils.GetRoadmapPath(testRoadmapName)
		os.Remove(path)
		backupDir, _ := GetBackupDir()
		os.RemoveAll(backupDir)
	}()

	// Create a test database
	db, err := Open(testRoadmapName)
	if err != nil {
		t.Fatalf("Failed to create test roadmap: %v", err)
	}
	db.Close()

	// Create more than MaxBackups backups
	for i := 0; i < MaxBackups+3; i++ {
		_, err := BackupRoadmap(testRoadmapName)
		if err != nil {
			t.Fatalf("BackupRoadmap failed on iteration %d: %v", i, err)
		}
	}

	// Should have at most MaxBackups
	backups, err := ListBackups(testRoadmapName)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(backups) > MaxBackups {
		t.Errorf("Expected at most %d backups, got %d", MaxBackups, len(backups))
	}
}

func TestRestoreNonExistentBackup(t *testing.T) {
	err := RestoreRoadmap("someroadmap", "/nonexistent/path/backup.db")
	if err == nil {
		t.Error("Should fail for non-existent backup")
	}
}

func TestBackupNonExistentRoadmap(t *testing.T) {
	_, err := BackupRoadmap("nonexistentroadmap12345")
	if err == nil {
		t.Error("Should fail for non-existent roadmap")
	}
}

func TestParseBackupTimestamp(t *testing.T) {
	tests := []struct {
		filename     string
		roadmap      string
		shouldBeZero bool
	}{
		{"myroad_20260317_120000.db", "myroad", false},
		{"test_20260101_000000.db", "test", false},
		{"invalid.db", "myroad", true},
		{"myroad_2026.db", "myroad", true},
	}

	for _, tt := range tests {
		result := parseBackupTimestamp(tt.filename, tt.roadmap)
		if tt.shouldBeZero && !result.IsZero() {
			t.Errorf("Expected zero time for %s", tt.filename)
		}
		if !tt.shouldBeZero && result.IsZero() {
			t.Errorf("Expected non-zero time for %s", tt.filename)
		}
	}
}

func TestCopyFile(t *testing.T) {
	// Create temp source file
	src, err := os.CreateTemp("", "test-src-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(src.Name())

	// Write some content
	content := "test content"
	src.WriteString(content)
	src.Close()

	// Create temp dest file
	dst, err := os.CreateTemp("", "test-dst-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	dst.Close()
	defer os.Remove(dst.Name())

	// Copy
	err = copyFile(src.Name(), dst.Name())
	if err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(dst.Name())
	if err != nil {
		t.Fatalf("Failed to read dest file: %v", err)
	}
	if string(data) != content {
		t.Errorf("Content mismatch: expected %q, got %q", content, string(data))
	}
}
