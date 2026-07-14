// Package db provides SQLite database connectivity and operations.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
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

// SQLite result codes. See https://www.sqlite.org/rescode.html.
//
// The constraint codes are EXTENDED codes: SQLITE_CONSTRAINT (19) is the
// primary code shared by every constraint kind, so it cannot tell a uniqueness
// collision apart from a CHECK (275) or NOT NULL (1299) violation. Only the
// extended code can.
const (
	sqliteBusy                   = 5
	sqliteLocked                 = 6
	sqliteConstraintPrimaryKey   = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
	sqliteConstraintUniqueViolat = 2067 // SQLITE_CONSTRAINT_UNIQUE
)

// IsUniqueConstraintErr reports whether err is a SQLite UNIQUE or PRIMARY-KEY
// constraint violation, and only those. Callers translate it into
// ErrAlreadyExists (exit code 5), so it must not answer true for a CHECK, NOT
// NULL or FOREIGN KEY violation: those are different failures and must not be
// reported to the user as an "already in use" collision.
//
// The check is on the extended result code, not the primary one. Masking down
// to the primary code (19) makes every constraint kind look like a uniqueness
// collision, which is exactly the bug this guards against.
func IsUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	var coded sqliteCoded
	if errors.As(err, &coded) {
		code := coded.Code()
		return code == sqliteConstraintUniqueViolat || code == sqliteConstraintPrimaryKey
	}
	return false
}

// sqliteCoded is satisfied by modernc.org/sqlite's *sqlite.Error.
// Using an interface keeps the check structural and testable without
// importing the driver here.
type sqliteCoded interface {
	Code() int
}

// isLockedError checks if an error is a SQLite busy/locked error by
// inspecting the structured result code rather than matching strings.
func isLockedError(err error) bool {
	if err == nil {
		return false
	}
	var coded sqliteCoded
	if errors.As(err, &coded) {
		c := coded.Code() & 0xFF // primary result code (low 8 bits)
		return c == sqliteBusy || c == sqliteLocked
	}
	return false
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
	queryCache  *QueryCache
	batchProc   *BatchProcessor
	roadmapName string
}

// Placeholders returns a comma-separated string of n SQL "?" placeholders,
// pulled from the connection's pre-generated cache when n is in range.
// Use from command handlers to build IN (...) clauses without re-allocating
// a []string + strings.Join on each call.
func (db *DB) Placeholders(n int) string {
	return db.queryCache.GetPlaceholders(n)
}

// Open opens a connection to a roadmap database.
// Creates the database file if it doesn't exist.
func Open(roadmapName string) (*DB, error) {
	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return nil, err
	}

	// Ensure the roadmap home directory (~/.roadmaps/<name>/) exists with
	// 0700 permissions before opening project.db inside it. This also
	// ensures the parent data directory exists and is private.
	if err := utils.EnsureRoadmapDir(roadmapName); err != nil {
		return nil, err
	}

	// Get database path (~/.roadmaps/<name>/project.db)
	dbPath, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		return nil, err
	}

	// Check if file exists
	isNew := false
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		isNew = true
	}

	// For a NEW database, pre-create the file with 0600 BEFORE sql.Open touches
	// it, so the descriptor never exists at umask-derived (potentially
	// world-readable) permissions (finding #77, SPEC/DATABASE.md § file
	// permissions). O_EXCL guarantees we are the creator; if a concurrent
	// process created it first we fall back to treating it as existing. The
	// post-schema os.Chmod + VerifyPermissions below remain as belt-and-braces.
	if isNew {
		// #nosec G304 -- dbPath is the internal per-roadmap path ~/.roadmaps/<name>/project.db; <name> is validated by ValidateRoadmapName upstream (no traversal), and this O_EXCL pre-create at 0600 is the fix for security finding #77
		f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, utils.DBFilePerm)
		if err != nil {
			if os.IsExist(err) {
				// Lost the race: another process created it. Treat as existing.
				isNew = false
			} else {
				return nil, fmt.Errorf("pre-creating database %s: %w", roadmapName, err)
			}
		} else {
			// Close the descriptor immediately; sql.Open reopens the path. The
			// file now exists at 0600 with no umask window.
			if cerr := f.Close(); cerr != nil {
				return nil, fmt.Errorf("closing pre-created database %s: %w", roadmapName, cerr)
			}
		}
	}

	// sql.Open is documented as not actually establishing a connection — it
	// only validates the driver — so wrapping it in retryWithBackoff was
	// dead weight. Any failure here is immediate and not retryable.
	//
	// foreign_keys and busy_timeout are CONNECTION-scoped PRAGMAs: a one-shot
	// db.Exec only configures whichever single pooled connection services it,
	// leaving the second pooled connection (SetMaxOpenConns(2)) on the SQLite
	// defaults foreign_keys=OFF / busy_timeout=0 — so ON DELETE CASCADE would
	// silently not fire and locked-database waits would return BUSY immediately.
	// Carrying them in the DSN makes modernc.org/sqlite apply them on EVERY new
	// connection. See SPEC/IMPLEMENTATION.md (foreign_keys on every connection;
	// busy_timeout) and SPEC/DATABASE.md (CASCADE integrity).
	sqlDB, err := sql.Open("sqlite", dsnWithPragmas(dbPath))
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", roadmapName, err)
	}

	// Configure connection with retry logic
	if err := retryWithBackoff("configuring database", func() error {
		return configureConnection(sqlDB)
	}); err != nil {
		sqlDB.Close() // #nosec G104 -- cleanup call in error path, original error returned
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
			db.Close() // #nosec G104 -- cleanup call in error path, original error returned
			return nil, fmt.Errorf("creating schema: %w", err)
		}

		// Set file permissions to 0600 (owner only)
		if err := os.Chmod(dbPath, utils.DBFilePerm); err != nil {
			db.Close() // #nosec G104 -- cleanup call in error path, original error returned
			return nil, fmt.Errorf("setting database permissions: %w", err)
		}

		// Verify permissions were set correctly (umask may have interfered)
		if err := utils.VerifyPermissions(dbPath, utils.DBFilePerm); err != nil {
			db.Close() // #nosec G104 -- cleanup call in error path, original error returned
			return nil, fmt.Errorf("verifying database permissions: %w", err)
		}
	} else {
		// Run migrations for existing databases
		if err := retryWithBackoff("running migrations", func() error {
			return db.RunMigrations()
		}); err != nil {
			db.Close() // #nosec G104 -- cleanup call in error path, original error returned
			return nil, fmt.Errorf("running migrations: %w", err)
		}
	}

	// Tighten the WAL/SHM sidecars to 0600 (finding #78, SPEC/DATABASE.md §
	// file permissions). WAL mode is enabled in configureConnection and the
	// schema-creation/migration writes above create project.db-wal /
	// project.db-shm; SQLite creates these with the process umask, so they can
	// be world-readable despite holding the same data pages as project.db.
	// Best-effort: a sidecar may legitimately be absent (e.g. checkpointed
	// away), so a missing file or chmod error is ignored.
	for _, suffix := range []string{"-wal", "-shm"} {
		p := dbPath + suffix
		if _, err := os.Stat(p); err == nil {
			_ = os.Chmod(p, utils.DBFilePerm) // #nosec G104 -- best-effort hardening; absence/failure is non-fatal
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

// OpenReadOnly opens an existing roadmap database for strictly read-only
// access. Unlike Open/OpenExisting it does NOT run schema migrations (no DDL)
// and opens every connection with query_only(true), so the SQLite engine
// rejects any write — schema change, row mutation, or audit insert. This is
// required by the web interface, which MUST NOT modify rows, write an audit
// entry, or alter the schema (SPEC/WEB.md § Read-Only Data Flow / Security and
// Constraints). Previously the web pages opened via OpenExisting -> Open, which
// ran RunMigrations and could rewrite a stale-schema database on a mere read
// (finding #43). journal_mode is intentionally not set (it is a write blocked
// by query_only; the database is already WAL from creation).
func OpenReadOnly(roadmapName string) (*DB, error) {
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return nil, err
	}

	exists, err := utils.RoadmapExists(roadmapName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: roadmap %q", utils.ErrNotFound, roadmapName)
	}

	dbPath, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("%s?_pragma=query_only(true)&_pragma=foreign_keys(1)&_pragma=busy_timeout(%d)", dbPath, DefaultBusyTimeout)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", roadmapName, err)
	}

	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	return &DB{
		DB:          sqlDB,
		roadmapName: roadmapName,
		queryCache:  NewQueryCache(),
		batchProc:   NewBatchProcessor(100),
	}, nil
}

// dsnWithPragmas builds a modernc.org/sqlite DSN that applies the
// connection-scoped PRAGMAs (foreign_keys, busy_timeout) on EVERY pooled
// connection. Unlike a one-shot db.Exec("PRAGMA ..."), the driver replays
// these on each new connection it opens, so the safety/integrity guarantees
// hold regardless of which pooled connection services a given query.
// journal_mode=WAL is intentionally NOT set here: it is a persistent,
// database-level setting (stored in the file header) and is configured once
// in configureConnection.
func dsnWithPragmas(dbPath string) string {
	return fmt.Sprintf("%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(%d)", dbPath, DefaultBusyTimeout)
}

// configureConnection sets up the persistent, database-level SQLite settings
// and the connection pool. Connection-scoped PRAGMAs (foreign_keys,
// busy_timeout) are NOT set here — they are carried in the DSN (see
// dsnWithPragmas) so they apply to every pooled connection, not just the one
// that happens to service this call.
func configureConnection(db *sql.DB) error {
	// Enable WAL mode for better concurrency. WAL is a persistent database-level
	// setting recorded in the file header, so a single Exec suffices for the
	// lifetime of the database (it survives reopen and applies to all connections).
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return fmt.Errorf("enabling WAL mode: %w", err)
	}

	// Configure connection pool for SQLite with WAL mode
	// SQLite only supports 1 writer at a time, so limit connections
	db.SetMaxOpenConns(2)                   // One for reads, one for writes
	db.SetMaxIdleConns(1)                   // Keep one warm connection
	db.SetConnMaxLifetime(30 * time.Minute) // Recycle connections more frequently
	db.SetConnMaxIdleTime(10 * time.Minute) // Close idle connections after 10 min

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
				// #nosec G104 -- rollback during panic recovery; the original panic is re-raised below and takes precedence over any rollback error
				tx.Rollback() //nolint:errcheck // rollback in panic recovery, original panic takes precedence
				panic(r)
			}
		}()

		if err := fn(tx); err != nil {
			// #nosec G104 -- rollback after a failed operation; the original error is returned to the caller and takes precedence over any rollback error
			tx.Rollback() //nolint:errcheck // rollback after error, original error is returned to caller
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
