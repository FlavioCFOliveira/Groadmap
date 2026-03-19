package db

import (
	"database/sql"
	"fmt"
)

// MigrationFunc is a function that performs a schema migration.
type MigrationFunc func(*sql.Tx) error

// Migration represents a database schema migration.
type Migration struct {
	Version string
	Name    string
	Apply   MigrationFunc
}

// migrations is a list of all available migrations, ordered by version.
// Each migration must be idempotent and safe to run multiple times.
var migrations = []Migration{
	{
		Version: "1.1.0",
		Name:    "Align tasks table with SPEC/DATABASE.md",
		Apply:   migrateV1_0_0_toV1_1_0,
	},
}

// RunMigrations executes all pending migrations in a transaction.
// It checks the current schema version and applies migrations in order.
func (db *DB) RunMigrations() error {
	currentVersion, err := db.GetSchemaVersion()
	if err != nil {
		// If _metadata table doesn't exist, this is a fresh database
		// Schema will be created fresh by CreateSchema
		return nil
	}

	for _, migration := range migrations {
		if shouldApplyMigration(currentVersion, migration.Version) {
			if err := db.runMigration(migration); err != nil {
				return fmt.Errorf("migration %s failed: %w", migration.Version, err)
			}
		}
	}

	return nil
}

// shouldApplyMigration determines if a migration should be applied.
// Returns true if targetVersion is greater than currentVersion.
func shouldApplyMigration(currentVersion, targetVersion string) bool {
	// Simple string comparison works for semantic versioning
	// where versions are ordered lexicographically (e.g., "1.0.0" < "1.1.0")
	return currentVersion < targetVersion
}

// runMigration executes a single migration in a transaction.
func (db *DB) runMigration(migration Migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Apply the migration
	if err := migration.Apply(tx); err != nil {
		return fmt.Errorf("applying migration: %w", err)
	}

	// Update schema version
	if _, err := tx.Exec(
		"UPDATE _metadata SET value = ? WHERE key = 'schema_version'",
		migration.Version,
	); err != nil {
		return fmt.Errorf("updating schema version: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migration: %w", err)
	}

	return nil
}

// migrateV1_0_0_toV1_1_0 migrates from schema v1.0.0 to v1.1.0.
// This migration aligns the tasks table with SPEC/DATABASE.md:
// - Renames description -> title
// - Renames action -> functional_requirements
// - Renames expected_result -> technical_requirements
// - Adds acceptance_criteria
// - Renames completed_at -> closed_at
// - Adds started_at and tested_at
func migrateV1_0_0_toV1_1_0(tx *sql.Tx) error {
	// Check if we're on old schema by checking for 'description' column
	var hasOldSchema bool
	err := tx.QueryRow(
		"SELECT 1 FROM pragma_table_info('tasks') WHERE name = 'description'",
	).Scan(&hasOldSchema)
	if err != nil {
		// Column doesn't exist, assume new schema
		hasOldSchema = false
	}

	if !hasOldSchema {
		// Already on new schema, nothing to do
		return nil
	}

	// Step 1: Create new table with updated schema
	_, err = tx.Exec(`
		CREATE TABLE tasks_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'BACKLOG' CHECK(status IN ('BACKLOG', 'SPRINT', 'DOING', 'TESTING', 'COMPLETED')),
			functional_requirements TEXT NOT NULL,
			technical_requirements TEXT NOT NULL,
			acceptance_criteria TEXT NOT NULL,
			created_at TEXT NOT NULL,
			specialists TEXT,
			started_at TEXT,
			tested_at TEXT,
			closed_at TEXT,
			priority INTEGER NOT NULL DEFAULT 0 CHECK(priority >= 0 AND priority <= 9),
			severity INTEGER NOT NULL DEFAULT 0 CHECK(severity >= 0 AND severity <= 9)
		)
	`)
	if err != nil {
		return fmt.Errorf("creating new tasks table: %w", err)
	}

	// Step 2: Migrate data from old table to new table
	// Map old fields to new fields:
	// - description -> title
	// - action -> functional_requirements
	// - expected_result -> technical_requirements
	// - completed_at -> closed_at (when status is COMPLETED)
	// - acceptance_criteria defaults to "Migrated from legacy schema"
	// - started_at and tested_at are NULL (will be tracked going forward)
	_, err = tx.Exec(`
		INSERT INTO tasks_new (
			id, title, status, functional_requirements, technical_requirements,
			acceptance_criteria, created_at, specialists,
			started_at, tested_at, closed_at,
			priority, severity
		)
		SELECT
			id,
			description AS title,
			status,
			action AS functional_requirements,
			expected_result AS technical_requirements,
			'Migrated from legacy schema' AS acceptance_criteria,
			created_at,
			specialists,
			NULL AS started_at,
			NULL AS tested_at,
			completed_at AS closed_at,
			priority,
			severity
		FROM tasks
	`)
	if err != nil {
		return fmt.Errorf("migrating task data: %w", err)
	}

	// Step 3: Drop old table
	_, err = tx.Exec("DROP TABLE tasks")
	if err != nil {
		return fmt.Errorf("dropping old tasks table: %w", err)
	}

	// Step 4: Rename new table to old name
	_, err = tx.Exec("ALTER TABLE tasks_new RENAME TO tasks")
	if err != nil {
		return fmt.Errorf("renaming new tasks table: %w", err)
	}

	// Step 5: Recreate indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)",
		"CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority)",
		"CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at)",
		"CREATE INDEX IF NOT EXISTS idx_tasks_status_priority ON tasks(status, priority DESC)",
		"CREATE INDEX IF NOT EXISTS idx_tasks_priority_created ON tasks(priority DESC, created_at ASC)",
	}

	for _, idx := range indexes {
		_, err = tx.Exec(idx)
		if err != nil {
			return fmt.Errorf("recreating index: %w", err)
		}
	}

	return nil
}
