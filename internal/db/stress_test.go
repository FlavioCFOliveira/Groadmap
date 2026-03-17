package db

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// setupStressTestDB creates a temporary database for stress testing
func setupStressTestDB(t *testing.T) (*DB, func()) {
	// Create temp data directory
	tempDataDir := filepath.Join(t.TempDir(), ".roadmaps")
	if err := os.MkdirAll(tempDataDir, 0700); err != nil {
		t.Fatalf("failed to create temp data dir: %v", err)
	}

	// Create a test database directly in temp dir
	dbPath := filepath.Join(tempDataDir, "stress-test.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Configure connection
	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		sqlDB.Close()
		t.Fatalf("failed to enable foreign keys: %v", err)
	}
	if _, err := sqlDB.Exec("PRAGMA journal_mode = WAL"); err != nil {
		sqlDB.Close()
		t.Fatalf("failed to enable WAL mode: %v", err)
	}
	if _, err := sqlDB.Exec("PRAGMA busy_timeout = 10000"); err != nil {
		sqlDB.Close()
		t.Fatalf("failed to set busy timeout: %v", err)
	}

	db := &DB{
		DB:          sqlDB,
		roadmapName: "stress-test",
	}

	// Create schema
	if err := db.CreateSchema(); err != nil {
		db.Close()
		t.Fatalf("failed to create schema: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tempDataDir)
	}

	return db, cleanup
}

// TestStressConcurrentWrites tests concurrent write operations on same database
func TestStressConcurrentWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	db, cleanup := setupStressTestDB(t)
	defer cleanup()

	const numOperations = 100
	var wg sync.WaitGroup
	var errorCount int32
	start := make(chan struct{})

	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start

			task := &models.Task{
				Priority:       id % 10,
				Severity:       id % 10,
				Status:         models.StatusBacklog,
				Description:    fmt.Sprintf("Concurrent Task %d", id),
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      time.Now().Format(time.RFC3339),
			}

			_, err := db.CreateTask(task)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
				t.Logf("Failed to create task %d: %v", id, err)
			}
		}(i)
	}

	close(start)
	wg.Wait()

	errors := int(atomic.LoadInt32(&errorCount))
	successRate := float64(numOperations-errors) / float64(numOperations) * 100
	t.Logf("Concurrent writes test: %d/%d operations succeeded (%.2f%%)",
		numOperations-errors, numOperations, successRate)

	// Requirement: 0% failures for 100 concurrent writes
	if errors > 0 {
		t.Errorf("expected 0 failures for concurrent writes, got %d (%.2f%% failure rate)",
			errors, float64(errors)/float64(numOperations)*100)
	}
}

// TestStressConcurrentReadsWrites tests concurrent reads and writes
func TestStressConcurrentReadsWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	db, cleanup := setupStressTestDB(t)
	defer cleanup()

	// Pre-populate with some data
	for i := 0; i < 10; i++ {
		task := &models.Task{
			Priority:       1,
			Severity:       1,
			Status:         models.StatusBacklog,
			Description:    fmt.Sprintf("Initial Task %d", i),
			Action:         "Test action",
			ExpectedResult: "Test result",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		_, err := db.CreateTask(task)
		if err != nil {
			t.Fatalf("failed to create initial task: %v", err)
		}
	}

	const numOperations = 100
	var wg sync.WaitGroup
	var errorCount int32
	start := make(chan struct{})

	// Writers
	for i := 0; i < numOperations/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start

			task := &models.Task{
				Priority:       id % 10,
				Severity:       id % 10,
				Status:         models.StatusBacklog,
				Description:    fmt.Sprintf("Writer Task %d", id),
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      time.Now().Format(time.RFC3339),
			}

			_, err := db.CreateTask(task)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
			}
		}(i)
	}

	// Readers
	for i := 0; i < numOperations/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start

			_, err := db.ListTasks(nil, nil, nil, nil)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
			}
		}(i)
	}

	close(start)
	wg.Wait()

	errors := int(atomic.LoadInt32(&errorCount))
	successRate := float64(numOperations-errors) / float64(numOperations) * 100
	t.Logf("Concurrent reads/writes test: %d/%d operations succeeded (%.2f%%)",
		numOperations-errors, numOperations, successRate)

	// Requirement: <1% failures for concurrent reads/writes
	failureRate := float64(errors) / float64(numOperations) * 100
	if failureRate >= 1.0 {
		t.Errorf("expected <1%% failure rate, got %.2f%% (%d failures)",
			failureRate, errors)
	}
}

// TestRetryLogging verifies that retry attempts are logged
func TestRetryLogging(t *testing.T) {
	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Re-initialize the logger with the new stderr
	utils.SetLogLevel("WARN")

	// Simulate a retryable error that eventually succeeds
	callCount := 0
	err := retryWithBackoff("test logging", func() error {
		callCount++
		if callCount < 2 {
			return fmt.Errorf("database is locked")
		}
		return nil
	})

	// Restore stderr
	w.Close()
	os.Stderr = oldStderr

	// Restore log level
	utils.SetLogLevel("INFO")

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Read captured output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify retry message was logged in structured format (JSON)
	if !strings.Contains(output, `"level":"WARN"`) {
		t.Error("expected retry logging to contain JSON level WARN")
	}
	if !strings.Contains(output, `"msg":"database operation retry"`) {
		t.Error("expected retry logging to contain structured message")
	}
	if !strings.Contains(output, `"operation":"test logging"`) {
		t.Error("expected retry logging to contain operation field")
	}
	if !strings.Contains(output, `"reason":"database locked"`) {
		t.Error("expected retry logging to mention 'database locked'")
	}
	// Check for attempt count in JSON format
	if !strings.Contains(output, `"attempt":`) {
		t.Error("expected retry logging to show attempt count")
	}
}

// TestTimeoutNotExceeded verifies that total timeout doesn't exceed 30 seconds
func TestTimeoutNotExceeded(t *testing.T) {
	start := time.Now()

	// This should fail after maxRetries with exponential backoff
	_ = retryWithBackoff("timeout test", func() error {
		return fmt.Errorf("database is locked")
	})

	elapsed := time.Since(start)

	// Calculate expected maximum time with 20 retries:
	// Retry 1: 50ms
	// Retry 2: 100ms
	// Retry 3: 200ms
	// Retry 4: 400ms
	// Retry 5-19: 500ms each (15 * 500ms = 7500ms)
	// Retry 20: fail (no delay after last failure)
	// Total: ~8750ms + overhead
	maxExpected := 10 * time.Second

	if elapsed > maxExpected {
		t.Errorf("expected timeout to not exceed %v, but took %v", maxExpected, elapsed)
	}

	// Also verify it doesn't complete too quickly (should have retries)
	minExpected := 100 * time.Millisecond
	if elapsed < minExpected {
		t.Errorf("expected at least %v for retries, but took %v", minExpected, elapsed)
	}
}
