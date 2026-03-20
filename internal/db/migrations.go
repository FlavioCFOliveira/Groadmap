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
	// Future migrations will be added here
	// Example:
	// {
	// 	Version: "1.1.0",
	// 	Name:    "Add new feature",
	// 	Apply:   migrateV1_0_0_toV1_1_0,
	// },
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
