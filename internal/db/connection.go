// Package db provides SQLite database connectivity and operations.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

const (
	// maxRetries is the maximum number of retry attempts for database operations.
	// Limited to 5 attempts as per SPEC requirements.
	maxRetries = 5
	// initialRetryDelay is the initial delay between retries (100ms).
	// Subsequent delays follow exponential backoff: 100ms, 200ms, 400ms, 800ms, 1000ms.
	initialRetryDelay = 100 * time.Millisecond
	// maxRetryDelay is the maximum delay between retries (1000ms).
	maxRetryDelay = 1000 * time.Millisecond

	// DefaultBusyTimeout is the SQLite busy timeout in milliseconds.
	// This prevents "database is locked" errors by waiting up to this duration.
	DefaultBusyTimeout = 10000 // 10 seconds

	// QueryTimeout is the default timeout for database queries.
	// Note: SQLite busy_timeout handles most locking scenarios.
	QueryTimeout = 30 * time.Second
)

// isLockedError checks if an error is a SQLite busy/locked error
func isLockedError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Check for SQLite busy/locked error codes and messages
	return strings.Contains(errStr, "database is locked") ||
		strings.Contains(errStr, "SQLITE_BUSY") ||
		strings.Contains(errStr, "busy") && strings.Contains(errStr, "5")
}

// retryWithBackoff executes a function with exponential backoff retry logic
func retryWithBackoff(operation string, fn func() error) error {
	var lastErr error
	delay := initialRetryDelay

	for attempt := 0; attempt < maxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Only retry on locked errors
		if !isLockedError(err) {
			return err
		}

		if attempt < maxRetries-1 {
			time.Sleep(delay)
			// Exponential backoff with cap
			delay *= 2
			if delay > maxRetryDelay {
				delay = maxRetryDelay
			}
		}
	}

	return fmt.Errorf("%s: failed after %d attempts: %w", operation, maxRetries, lastErr)
}

// DB wraps sql.DB with roadmap-specific operations.
type DB struct {
	*sql.DB
	roadmapName string
	queryCache  *QueryCache
	batchProc   *BatchProcessor
}

// Open opens a connection to a roadmap database.
// Creates the database file if it doesn't exist.
func Open(roadmapName string) (*DB, error) {
	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return nil, err
	}

	// Ensure data directory exists
	if err := utils.EnsureDataDir(); err != nil {
		return nil, err
	}

	// Get database path
	dbPath, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		return nil, err
	}

	// Check if file exists
	isNew := false
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		isNew = true
	}

	// Open database connection with retry logic
	var sqlDB *sql.DB
	err = retryWithBackoff("opening database", func() error {
		var openErr error
		sqlDB, openErr = sql.Open("sqlite", dbPath)
		return openErr
	})
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", roadmapName, err)
	}

	// Configure connection with retry logic
	if err := retryWithBackoff("configuring database", func() error {
		return configureConnection(sqlDB)
	}); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("configuring database: %w", err)
	}

	db := &DB{
		DB:          sqlDB,
		roadmapName: roadmapName,
		queryCache:  NewQueryCache(),
		batchProc:   NewBatchProcessor(100),
	}

	// Create schema if new database with retry logic
	if isNew {
		if err := retryWithBackoff("creating schema", func() error {
			return db.CreateSchema()
		}); err != nil {
			db.Close()
			return nil, fmt.Errorf("creating schema: %w", err)
		}

		// Set file permissions to 0600 (owner only)
		if err := os.Chmod(dbPath, utils.DBFilePerm); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting database permissions: %w", err)
		}

		// Verify permissions were set correctly (umask may have interfered)
		if err := utils.VerifyPermissions(dbPath, utils.DBFilePerm); err != nil {
			db.Close()
			return nil, fmt.Errorf("verifying database permissions: %w", err)
		}
	} else {
		// Run migrations for existing databases
		if err := retryWithBackoff("running migrations", func() error {
			return db.RunMigrations()
		}); err != nil {
			db.Close()
			return nil, fmt.Errorf("running migrations: %w", err)
		}
	}

	return db, nil
}

// OpenExisting opens an existing roadmap database.
// Returns an error if the database doesn't exist.
func OpenExisting(roadmapName string) (*DB, error) {
	exists, err := utils.RoadmapExists(roadmapName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: roadmap %q", utils.ErrNotFound, roadmapName)
	}

	return Open(roadmapName)
}

// configureConnection sets up SQLite pragmas for safety and performance.
func configureConnection(db *sql.DB) error {
	// Enable foreign key constraints (required for cascading deletes)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enabling foreign keys: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return fmt.Errorf("enabling WAL mode: %w", err)
	}

	// Set busy timeout to avoid "database is locked" errors
	// This makes SQLite wait up to DefaultBusyTimeout milliseconds before returning BUSY
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", DefaultBusyTimeout)); err != nil {
		return fmt.Errorf("setting busy timeout: %w", err)
	}

	// Configure connection pool for SQLite with WAL mode
	// SQLite only supports 1 writer at a time, so limit connections
	db.SetMaxOpenConns(2)                    // One for reads, one for writes
	db.SetMaxIdleConns(1)                    // Keep one warm connection
	db.SetConnMaxLifetime(30 * time.Minute)   // Recycle connections more frequently
	db.SetConnMaxIdleTime(10 * time.Minute)  // Close idle connections after 10 min

	return nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	if db.DB != nil {
		return db.DB.Close()
	}
	return nil
}

// RoadmapName returns the name of the connected roadmap.
func (db *DB) RoadmapName() string {
	return db.roadmapName
}

// WithTransaction executes a function within a database transaction.
// Automatically commits on success or rolls back on error.
// Uses retry logic for handling database locked errors.
func (db *DB) WithTransaction(fn func(*sql.Tx) error) error {
	return retryWithBackoff("transaction", func() error {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction: %w", err)
		}

		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
				panic(r)
			}
		}()

		if err := fn(tx); err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing transaction: %w", err)
		}

		return nil
	})
}

// ==================== CONTEXT TIMEOUT HELPERS ====================

const (
	// DefaultQueryTimeout is the default timeout for database queries (30 seconds).
	DefaultQueryTimeout = 30 * time.Second

	// QuickQueryTimeout is the timeout for simple read operations (5 seconds).
	QuickQueryTimeout = 5 * time.Second
)

// WithDefaultTimeout returns a context with the default query timeout.
func WithDefaultTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), DefaultQueryTimeout)
}

// WithQuickTimeout returns a context with the quick query timeout.
func WithQuickTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), QuickQueryTimeout)
}

// WithCustomTimeout returns a context with a custom timeout.
func WithCustomTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
