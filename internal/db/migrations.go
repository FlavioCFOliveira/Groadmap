package db

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// MigrationFunc is a function that performs a schema migration.
type MigrationFunc func(*sql.Tx) error

// Migration represents a database schema migration.
type Migration struct {
	Apply   MigrationFunc
	Version string
	Name    string
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
	{
		Version: "1.5.0",
		Name:    "Add parent_task_id column and index to tasks table",
		Apply:   migrateV1_4_0_toV1_5_0,
	},
	{
		Version: "1.6.0",
		Name:    "Add task_dependencies table for blocking relationships",
		Apply:   migrateV1_5_0_toV1_6_0,
	},
	{
		Version: "1.7.0",
		Name:    "Add title column to sprints table",
		Apply:   migrateV1_6_0_toV1_7_0,
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
// Returns true if targetVersion is strictly greater than currentVersion.
func shouldApplyMigration(currentVersion, targetVersion string) bool {
	return compareVersions(currentVersion, targetVersion) < 0
}

// compareVersions compares two dotted version strings (e.g. "1.9.0", "1.10.0")
// numerically, component by component. It returns -1 if a < b, 0 if equal, and
// +1 if a > b. A lexicographic string comparison is WRONG for versions because
// it orders "1.10.0" before "1.9.0" ("1" < "9"), which would skip migrations
// once any version component reaches two digits. Missing trailing components
// are treated as 0 (so "1.5" == "1.5.0"); non-numeric components compare as 0.
func compareVersions(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var na, nb int
		if i < len(pa) {
			na, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			nb, _ = strconv.Atoi(pb[i])
		}
		if na < nb {
			return -1
		}
		if na > nb {
			return 1
		}
	}
	return 0
}

// runMigration executes a single migration in a transaction.
func (db *DB) runMigration(migration Migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback, migration error already captured

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

// columnExists reports whether table already has a column named column.
//
// SQLite's `ALTER TABLE … ADD COLUMN` is not idempotent: re-running it for an
// existing column raises "duplicate column name". Because a migration may be
// applied to a database that has already been partially or fully migrated, every
// ADD COLUMN step guards itself with this check and skips the ALTER when the
// column is already present (SPEC/DATABASE.md § Migration Idempotency). The query
// is parameterized; table is a compile-time literal at every call site.
func columnExists(tx *sql.Tx, table, column string) (bool, error) {
	var count int
	query := fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?", table) // #nosec G201 -- table is a constant literal at every call site; column value is parameterized
	if err := tx.QueryRow(query, column).Scan(&count); err != nil {
		return false, fmt.Errorf("checking column %s.%s: %w", table, column, err)
	}
	return count > 0, nil
}

// migrateV1_0_0_toV1_1_0 adds the position column to sprint_tasks table.
// It initializes existing tasks with sequential positions based on their order.
//
// Idempotent: the ADD COLUMN is guarded by columnExists, so re-applying the
// migration on a database that already has the column is a no-op (not an error).
func migrateV1_0_0_toV1_1_0(tx *sql.Tx) error {
	// Add position column with DEFAULT 0 only when it does not already exist.
	exists, err := columnExists(tx, "sprint_tasks", "position")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := tx.Exec(
			`ALTER TABLE sprint_tasks ADD COLUMN position INTEGER NOT NULL DEFAULT 0`,
		); err != nil {
			return fmt.Errorf("adding position column: %w", err)
		}
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
// Idempotent: the ADD COLUMN is guarded by columnExists, so re-applying the
// migration on a database that already has the column is a no-op (not an error).
func migrateV1_3_0_toV1_4_0(tx *sql.Tx) error {
	exists, err := columnExists(tx, "sprints", "max_tasks")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := tx.Exec(`ALTER TABLE sprints ADD COLUMN max_tasks INTEGER`); err != nil {
		return fmt.Errorf("adding max_tasks column: %w", err)
	}
	return nil
}

// migrateV1_4_0_toV1_5_0 adds the parent_task_id column and its index to the tasks table.
// The column is optional (NULL by default) and enables sub-task hierarchy.
// Idempotent: the ADD COLUMN is guarded by columnExists and the index uses
// CREATE INDEX IF NOT EXISTS, so re-applying the migration is a no-op.
func migrateV1_4_0_toV1_5_0(tx *sql.Tx) error {
	exists, err := columnExists(tx, "tasks", "parent_task_id")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := tx.Exec(
			`ALTER TABLE tasks ADD COLUMN parent_task_id INTEGER REFERENCES tasks(id)`,
		); err != nil {
			return fmt.Errorf("adding parent_task_id column: %w", err)
		}
	}

	if _, err := tx.Exec(
		`CREATE INDEX IF NOT EXISTS idx_tasks_parent_task_id ON tasks(parent_task_id)`,
	); err != nil {
		return fmt.Errorf("creating idx_tasks_parent_task_id: %w", err)
	}

	return nil
}

// migrateV1_5_0_toV1_6_0 creates the task_dependencies table for blocking relationships.
// The migration is idempotent: CREATE TABLE IF NOT EXISTS is a no-op if the table already exists.
func migrateV1_5_0_toV1_6_0(tx *sql.Tx) error {
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id INTEGER NOT NULL,
    depends_on_task_id INTEGER NOT NULL,
    PRIMARY KEY (task_id, depends_on_task_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    FOREIGN KEY (depends_on_task_id) REFERENCES tasks(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("creating task_dependencies table: %w", err)
	}

	if _, err := tx.Exec(
		`CREATE INDEX IF NOT EXISTS idx_task_deps_task_id ON task_dependencies(task_id)`,
	); err != nil {
		return fmt.Errorf("creating idx_task_deps_task_id: %w", err)
	}

	if _, err := tx.Exec(
		`CREATE INDEX IF NOT EXISTS idx_task_deps_depends_on ON task_dependencies(depends_on_task_id)`,
	); err != nil {
		return fmt.Errorf("creating idx_task_deps_depends_on: %w", err)
	}

	return nil
}

// migrateV1_6_0_toV1_7_0 adds the required title column to the sprints table and
// backfills existing rows with a deterministic title derived from the sprint id.
//
// SQLite cannot add a bare NOT NULL column to a populated table, so the column is
// added with DEFAULT ” and then every backfilled row receives the literal title
// 'Sprint ' || id. The backfill is restricted to rows whose title is still the
// empty-string default, so re-running the migration never clobbers a real title.
// Idempotent: the ADD COLUMN is guarded by columnExists, so re-applying the
// migration on a database that already has the column is a no-op (not an error).
func migrateV1_6_0_toV1_7_0(tx *sql.Tx) error {
	exists, err := columnExists(tx, "sprints", "title")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := tx.Exec(
			`ALTER TABLE sprints ADD COLUMN title TEXT NOT NULL DEFAULT '' CHECK(length(title) <= 255)`,
		); err != nil {
			return fmt.Errorf("adding title column: %w", err)
		}
	}

	// Backfill only rows still holding the empty-string default so a second
	// apply does not overwrite titles set after the first migration.
	if _, err := tx.Exec(
		`UPDATE sprints SET title = 'Sprint ' || id WHERE title = ''`,
	); err != nil {
		return fmt.Errorf("backfilling sprint titles: %w", err)
	}

	return nil
}

// migrateV1_2_0_toV1_3_0 adds the completion_summary column to the tasks table.
// The column is optional (NULL by default) and capped at 4096 characters.
// Idempotent: the ADD COLUMN is guarded by columnExists, so re-applying the
// migration on a database that already has the column is a no-op (not an error).
func migrateV1_2_0_toV1_3_0(tx *sql.Tx) error {
	exists, err := columnExists(tx, "tasks", "completion_summary")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := tx.Exec(
		`ALTER TABLE tasks ADD COLUMN completion_summary TEXT CHECK(completion_summary IS NULL OR length(completion_summary) <= 4096)`,
	); err != nil {
		return fmt.Errorf("adding completion_summary column: %w", err)
	}
	return nil
}
