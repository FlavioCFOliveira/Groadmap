package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestTimeoutConstants(t *testing.T) {
	// Verify timeout constants are reasonable
	if DefaultBusyTimeout <= 0 {
		t.Errorf("DefaultBusyTimeout should be positive, got %d", DefaultBusyTimeout)
	}

	if DefaultBusyTimeout < 5000 {
		t.Errorf("DefaultBusyTimeout should be at least 5 seconds, got %d ms", DefaultBusyTimeout)
	}

	if QueryTimeout <= 0 {
		t.Errorf("QueryTimeout should be positive, got %v", QueryTimeout)
	}

	if QueryTimeout < 10*time.Second {
		t.Errorf("QueryTimeout should be at least 10 seconds, got %v", QueryTimeout)
	}
}

func TestBusyTimeoutConfiguration(t *testing.T) {
	// Use a file-based test database since PRAGMA busy_timeout
	// doesn't persist correctly on :memory: databases
	testDBPath := filepath.Join(os.TempDir(), "test_busy_timeout.db")
	defer os.Remove(testDBPath)
	defer os.Remove(testDBPath + "-shm")
	defer os.Remove(testDBPath + "-wal")

	// Open database
	sqlDB, err := sql.Open("sqlite", testDBPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer sqlDB.Close()

	// Configure connection (this sets busy_timeout)
	if err := configureConnection(sqlDB); err != nil {
		t.Fatalf("failed to configure connection: %v", err)
	}

	// Query the current busy_timeout setting
	var timeout int
	err = sqlDB.QueryRow("PRAGMA busy_timeout").Scan(&timeout)
	if err != nil {
		t.Fatalf("failed to query busy_timeout: %v", err)
	}

	if timeout != DefaultBusyTimeout {
		t.Errorf("busy_timeout = %d, want %d", timeout, DefaultBusyTimeout)
	}
}
