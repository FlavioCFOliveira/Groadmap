package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ==================== OPEN TESTS ====================

func TestOpen_ValidRoadmap(t *testing.T) {
	// Use a unique name to avoid conflicts
	roadmapName := "testopenvalid"

	db, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to open roadmap: %v", err)
	}
	defer db.Close()

	// Verify connection
	if db.DB == nil {
		t.Error("expected non-nil DB connection")
	}

	// Verify roadmap name
	if db.RoadmapName() != roadmapName {
		t.Errorf("expected roadmap name %q, got %q", roadmapName, db.RoadmapName())
	}
}

func TestOpen_InvalidRoadmapName(t *testing.T) {
	// Test with invalid name (path traversal)
	_, err := Open("../etc/passwd")
	if err == nil {
		t.Error("expected error for invalid roadmap name")
	}

	// Test with empty name
	_, err = Open("")
	if err == nil {
		t.Error("expected error for empty roadmap name")
	}

	// Test with name starting with hyphen
	_, err = Open("-r")
	if err == nil {
		t.Error("expected error for name starting with hyphen")
	}
}

func TestOpenExisting(t *testing.T) {
	// Use a unique name
	roadmapName := "testopenexisting"

	// First create the roadmap
	db, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	db.Close()

	// Now open existing
	db2, err := OpenExisting(roadmapName)
	if err != nil {
		t.Fatalf("failed to open existing roadmap: %v", err)
	}
	defer db2.Close()

	if db2.RoadmapName() != roadmapName {
		t.Errorf("expected roadmap name %q, got %q", roadmapName, db2.RoadmapName())
	}
}

func TestOpenExisting_NotFound(t *testing.T) {
	// Try to open a roadmap that doesn't exist
	_, err := OpenExisting("nonexistentroadmap12345")
	if err == nil {
		t.Error("expected error for non-existent roadmap")
	}
}

// ==================== CONNECTION CONFIG TESTS ====================

func TestConfigureConnection(t *testing.T) {
	// This is tested indirectly through Open, but we can verify
	// that the connection is properly configured
	db, err := Open("testconfig")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Verify foreign keys are enabled
	var foreignKeys int
	err = db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys)
	if err != nil {
		t.Fatalf("failed to check foreign keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("expected foreign_keys=1, got %d", foreignKeys)
	}

	// Verify journal mode is WAL
	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to check journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected journal_mode=wal, got %s", journalMode)
	}
}

// ==================== RETRY LOGIC TESTS ====================

func TestIsLockedError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"database is locked", errorString("database is locked"), true},
		{"SQLITE_BUSY", errorString("SQLITE_BUSY"), true},
		{"busy with code 5", errorString("busy 5"), true},
		{"other error", errorString("some other error"), false},
		{"connection refused", errorString("connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLockedError(tt.err)
			if result != tt.expected {
				t.Errorf("isLockedError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

// errorString is a simple error type for testing
type errorString string

func (e errorString) Error() string {
	return string(e)
}

// ==================== DB METHOD TESTS ====================

func TestDBClose(t *testing.T) {
	db, err := Open("testclose")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	err = db.Close()
	if err != nil {
		t.Errorf("failed to close database: %v", err)
	}

	// Closing again should not error (idempotent)
	err = db.Close()
	// This may or may not error depending on implementation
	// We just verify it doesn't panic
}

func TestRoadmapName(t *testing.T) {
	db, err := Open("testroadmapname")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	name := db.RoadmapName()
	if name != "testroadmapname" {
		t.Errorf("expected roadmap name 'testroadmapname', got %q", name)
	}
}

// ==================== TRANSACTION TESTS ====================

func TestWithTransaction_Success(t *testing.T) {
	db, err := Open("testtxsuccess")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Execute a simple transaction - insert into _metadata table
	err = db.WithTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT OR REPLACE INTO _metadata (key, value) VALUES (?, ?)", "test_key", "test_value")
		return err
	})

	if err != nil {
		t.Errorf("transaction failed: %v", err)
	}
}

func TestWithTransaction_RollbackOnError(t *testing.T) {
	db, err := Open("testtxrollback")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Execute a transaction that will fail
	err = db.WithTransaction(func(tx *sql.Tx) error {
		// Insert something
		_, _ = tx.Exec("INSERT INTO schema_metadata (version) VALUES (?)", 888)
		// Then return error to trigger rollback
		return errorString("intentional error")
	})

	if err == nil {
		t.Error("expected error from failed transaction")
	}
}

// ==================== SCHEMA VERSION TESTS ====================

func TestGetSchemaVersion(t *testing.T) {
	db, err := Open("testschemaversion")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Get schema version
	version, err := db.GetSchemaVersion()
	if err != nil {
		t.Fatalf("failed to get schema version: %v", err)
	}

	// Version should not be empty
	if version == "" {
		t.Error("expected non-empty schema version")
	}
}

// ==================== ENTITY HISTORY TESTS ====================

func TestGetEntityHistory(t *testing.T) {
	db, err := Open("testentityhistory")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Create a task first
	task := &models.Task{
		Priority:               1,
		Severity:               1,
		Status:                 models.StatusBacklog,
		Title:                  "Test task",
		FunctionalRequirements: "Test functional requirements",
		TechnicalRequirements:  "Test technical requirements",
		AcceptanceCriteria:     "Test acceptance criteria",
		CreatedAt:              time.Now().Format(time.RFC3339),
	}

	taskID, err := db.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create an audit entry for this task
	entry := &models.AuditEntry{
		Operation:   "TASK_CREATE",
		EntityType:  "TASK",
		EntityID:    taskID,
		PerformedAt: time.Now().Format(time.RFC3339),
	}
	_, err = db.LogAuditEntry(context.Background(), entry)
	if err != nil {
		t.Fatalf("failed to log audit entry: %v", err)
	}

	// Get entity history
	history, err := db.GetEntityHistory(context.Background(), "TASK", taskID)
	if err != nil {
		t.Fatalf("failed to get entity history: %v", err)
	}

	// Should have at least one entry (the creation)
	if len(history) == 0 {
		t.Error("expected at least one history entry for new task")
	}
}
