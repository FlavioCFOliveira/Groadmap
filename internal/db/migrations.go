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
// NOTE: v1.0.0 is the initial schema version, no migrations needed.
var migrations = []Migration{
	{
		Version: "1.1.0",
		Name:    "Add sprint_tasks position column",
		Apply:   migrateV1_0_0_toV1_1_0,
	},
	{
		Version: "1.2.0",
		Name:    "Add partial unique index to enforce at most one OPEN sprint",
		Apply:   migrateV1_1_0_toV1_2_0,
	},
	{
		Version: "1.3.0",
		Name:    "Add completion_summary column to tasks table",
		Apply:   migrateV1_2_0_toV1_3_0,
	},
	{
		Version: "1.4.0",
		Name:    "Add max_tasks column to sprints table",
		Apply:   migrateV1_3_0_toV1_4_0,
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

// migrateV1_0_0_toV1_1_0 adds the position column to sprint_tasks table.
// It initializes existing tasks with sequential positions based on their order.
func migrateV1_0_0_toV1_1_0(tx *sql.Tx) error {
	// Add position column with DEFAULT 0
	if _, err := tx.Exec(
		`ALTER TABLE sprint_tasks ADD COLUMN position INTEGER NOT NULL DEFAULT 0`,
	); err != nil {
		return fmt.Errorf("adding position column: %w", err)
	}

	// Add index for sprint task ordering
	if _, err := tx.Exec(
		`CREATE INDEX IF NOT EXISTS idx_sprint_tasks_order ON sprint_tasks(sprint_id, position ASC)`,
	); err != nil {
		return fmt.Errorf("creating idx_sprint_tasks_order: %w", err)
	}

	// Initialize positions for existing sprint tasks
	// Assign sequential positions (0, 1, 2...) based on added_at order within each sprint
	if _, err := tx.Exec(`
		UPDATE sprint_tasks
		SET position = new_pos
		FROM (
			SELECT
				sprint_id,
				task_id,
				ROW_NUMBER() OVER (PARTITION BY sprint_id ORDER BY added_at ASC) - 1 AS new_pos
			FROM sprint_tasks
		) AS ordered
		WHERE sprint_tasks.sprint_id = ordered.sprint_id
		  AND sprint_tasks.task_id = ordered.task_id
	`); err != nil {
		return fmt.Errorf("initializing task positions: %w", err)
	}

	return nil
}

// migrateV1_1_0_toV1_2_0 adds a partial unique index that enforces at most one OPEN sprint.
// This prevents TOCTOU races between concurrent processes starting sprints simultaneously.
func migrateV1_1_0_toV1_2_0(tx *sql.Tx) error {
	_, err := tx.Exec(
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_one_open_sprint ON sprints(status) WHERE status = 'OPEN'`,
	)
	if err != nil {
		return fmt.Errorf("creating idx_one_open_sprint: %w", err)
	}
	return nil
}

// migrateV1_3_0_toV1_4_0 adds the max_tasks column to the sprints table.
// The column is optional (NULL by default) and enables sprint capacity management.
// The migration is idempotent: ALTER TABLE … ADD COLUMN is a no-op when the column already exists.
func migrateV1_3_0_toV1_4_0(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE sprints ADD COLUMN max_tasks INTEGER`)
	if err != nil {
		return fmt.Errorf("adding max_tasks column: %w", err)
	}
	return nil
}

// migrateV1_2_0_toV1_3_0 adds the completion_summary column to the tasks table.
// The column is optional (NULL by default) and capped at 4096 characters.
// The migration is idempotent: ALTER TABLE … ADD COLUMN is a no-op when the column already exists in SQLite.
func migrateV1_2_0_toV1_3_0(tx *sql.Tx) error {
	_, err := tx.Exec(
		`ALTER TABLE tasks ADD COLUMN completion_summary TEXT CHECK(completion_summary IS NULL OR length(completion_summary) <= 4096)`,
	)
	if err != nil {
		return fmt.Errorf("adding completion_summary column: %w", err)
	}
	return nil
}
