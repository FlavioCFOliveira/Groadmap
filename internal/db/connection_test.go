package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
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

// TestOpenReadOnly_NoMigrationNoWrites is a regression gate for finding #43:
// the web interface opens the database read-only, so OpenReadOnly must NOT run
// schema migrations (no DDL on a stale-schema DB) and must reject every write
// via query_only, while still serving reads.
func TestOpenReadOnly_NoMigrationNoWrites(t *testing.T) {
	roadmapName := "testreadonly"

	// Ensure a clean slate: this test deliberately downgrades the schema
	// version, so a leftover database from a prior run must not survive.
	if dir, derr := utils.GetRoadmapDir(roadmapName); derr == nil {
		os.RemoveAll(dir) // #nosec G104 -- best-effort pre-test cleanup
		defer os.RemoveAll(dir)
	}

	// Create the database at the current schema, then simulate a stale schema
	// by forcing an older recorded version.
	rw, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	if _, err := rw.Exec("UPDATE _metadata SET value = '1.0.0' WHERE key = 'schema_version'"); err != nil {
		rw.Close()
		t.Fatalf("forcing stale schema version: %v", err)
	}
	rw.Close()

	// Open read-only: this MUST NOT migrate the database back up.
	ro, err := OpenReadOnly(roadmapName)
	if err != nil {
		t.Fatalf("OpenReadOnly failed: %v", err)
	}
	defer ro.Close()

	var version string
	if err := ro.QueryRow("SELECT value FROM _metadata WHERE key = 'schema_version'").Scan(&version); err != nil {
		t.Fatalf("reading schema version: %v", err)
	}
	if version != "1.0.0" {
		t.Errorf("OpenReadOnly migrated the schema (version is now %q); a read must not write DDL", version)
	}

	// Reads must still work.
	var n int
	if err := ro.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&n); err != nil {
		t.Errorf("read query failed under OpenReadOnly: %v", err)
	}

	// Writes must be rejected by query_only.
	if _, err := ro.Exec("INSERT INTO _metadata (key, value) VALUES ('probe', 'x')"); err == nil {
		t.Error("OpenReadOnly must reject writes (query_only), but the INSERT succeeded")
	}
}

// ==================== FILE PERMISSION TESTS ====================

// TestOpenCreatesDBWith0600 verifies a freshly created project.db has exactly
// 0600 permissions, with no umask-derived window in which it was more permissive
// (finding #77, SPEC/DATABASE.md § file permissions). HOME is redirected to a
// temp dir so the test is hermetic.
func TestOpenCreatesDBWith0600(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	roadmapName := "permcheck"

	db, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	dbPath, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		t.Fatalf("GetRoadmapPath: %v", err)
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat project.db: %v", err)
	}
	if perm := info.Mode().Perm(); perm != os.FileMode(utils.DBFilePerm) {
		t.Errorf("project.db permissions = %o, want %o", perm, utils.DBFilePerm)
	}
}

// TestOpenChmodsWALSidecars verifies the WAL/SHM sidecars are tightened to 0600
// after the database is opened and written (finding #78, SPEC/DATABASE.md § file
// permissions). The WAL is created by the schema-creation write under WAL mode.
func TestOpenChmodsWALSidecars(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	roadmapName := "walperm"

	db, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Force a write so the WAL sidecar definitely exists, then reopen so Open's
	// post-setup chmod runs over the present sidecars.
	if _, err := db.Exec("INSERT INTO _metadata (key, value) VALUES ('probe', 'x')"); err != nil {
		db.Close()
		t.Fatalf("probe write: %v", err)
	}
	db.Close()

	db2, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()

	dbPath, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		t.Fatalf("GetRoadmapPath: %v", err)
	}

	for _, suffix := range []string{"-wal", "-shm"} {
		p := dbPath + suffix
		info, statErr := os.Stat(p)
		if statErr != nil {
			// A checkpointed-away sidecar may legitimately be absent.
			continue
		}
		if perm := info.Mode().Perm(); perm != os.FileMode(utils.DBFilePerm) {
			t.Errorf("%s permissions = %o, want %o", p, perm, utils.DBFilePerm)
		}
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

// TestPerConnectionPragmas is a regression gate for finding #41: the
// connection-scoped PRAGMAs foreign_keys and busy_timeout must be set on EVERY
// pooled connection, not just whichever single connection a one-shot
// db.Exec("PRAGMA ...") happened to use. With SetMaxOpenConns(2), a second
// connection materialized later previously inherited the SQLite defaults
// (foreign_keys=OFF, busy_timeout=0), silently disabling ON DELETE CASCADE and
// turning lock waits into immediate BUSY errors.
func TestPerConnectionPragmas(t *testing.T) {
	db, err := Open("testperconnpragma")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Pin two distinct pooled connections simultaneously. db.Conn reserves a
	// connection from the pool, so holding c1 forces c2 onto a second physical
	// connection (the one that previously missed the one-shot PRAGMAs).
	c1, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquiring connection 1: %v", err)
	}
	defer c1.Close()
	c2, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("acquiring connection 2: %v", err)
	}
	defer c2.Close()

	for i, c := range []*sql.Conn{c1, c2} {
		var fk, bt int
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatalf("conn %d: reading foreign_keys: %v", i+1, err)
		}
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&bt); err != nil {
			t.Fatalf("conn %d: reading busy_timeout: %v", i+1, err)
		}
		if fk != 1 {
			t.Errorf("conn %d: foreign_keys=%d, want 1", i+1, fk)
		}
		if bt != DefaultBusyTimeout {
			t.Errorf("conn %d: busy_timeout=%d, want %d", i+1, bt, DefaultBusyTimeout)
		}
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
		{"SQLITE_BUSY (5)", &mockSQLiteErr{code: 5}, true},
		{"SQLITE_LOCKED (6)", &mockSQLiteErr{code: 6}, true},
		{"extended busy code (261)", &mockSQLiteErr{code: 261}, true}, // SQLITE_BUSY_RECOVERY: high byte 1, low byte 5
		{"unrelated SQLite code", &mockSQLiteErr{code: 14}, false},    // SQLITE_CANTOPEN
		{"plain string error", errorString("database is locked"), false},
		{"other error", errorString("some other error"), false},
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

// mockSQLiteErr satisfies the sqliteCoded interface used by isLockedError.
type mockSQLiteErr struct {
	code int
}

func (e *mockSQLiteErr) Code() int     { return e.code }
func (e *mockSQLiteErr) Error() string { return fmt.Sprintf("sqlite error %d", e.code) }

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
	_ = db.Close()
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
