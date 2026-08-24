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
	{
		Version: "1.8.0",
		Name:    "Add order_index column and unique index to sprints table",
		Apply:   migrateV1_7_0_toV1_8_0,
	},
	{
		Version: "1.9.0",
		Name:    "Add task_comments and sprint_comments tables for durable comment records",
		Apply:   migrateV1_8_0_toV1_9_0,
	},
	{
		Version: "1.10.0",
		Name:    "Drop the specialists column from the tasks table",
		Apply:   migrateV1_9_0_toV1_10_0,
	},
	{
		Version: "1.11.0",
		Name:    "Add commit_open and commit_close columns to tasks table",
		Apply:   migrateV1_10_0_toV1_11_0,
	},
	{
		Version: "1.12.0",
		Name:    "Add related_entity_id and commit_hash columns to audit table and reclassify the legacy TASK_STATUS_CHANGE entries",
		Apply:   migrateV1_11_0_toV1_12_0,
	},
	{
		Version: "1.13.0",
		Name:    "Make the sprint_tasks ordering index unique so a sprint's planned task order is total",
		Apply:   migrateV1_12_0_toV1_13_0,
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
// Neither `ALTER TABLE … ADD COLUMN` nor `ALTER TABLE … DROP COLUMN` is
// idempotent in SQLite: re-running the first for an existing column raises
// "duplicate column name", and re-running the second for a column already gone
// raises "no such column". Because a migration may be applied to a database that
// has already been partially or fully migrated, every ALTER step guards itself
// with this one check and reads it in the sense its own statement needs — an ADD
// runs only when the column is ABSENT, a DROP only while it is still PRESENT
// (SPEC/DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN) and
// § Migration Idempotency (ALTER TABLE DROP COLUMN)). The query is parameterized;
// table is a compile-time literal at every call site.
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

// migrateV1_7_0_toV1_8_0 adds the required order_index column to the sprints
// table and backfills every existing sprint with a deterministic, collision-free
// execution order, so pre-existing sprints satisfy the NOT NULL, > 0, and
// uniqueness invariants before the unique index is created.
//
// SQLite cannot add a bare NOT NULL column to a populated table, so the column is
// added with a temporary DEFAULT 0 and then every row is overwritten with a
// unique positive value (1, 2, 3, ...) ordered by created_at ascending, then id
// ascending as the tie-breaker. The unique index is created only after the
// backfill, once every row holds a distinct positive value.
//
// Idempotent: the ADD COLUMN is guarded by columnExists; the backfill is a
// deterministic full-table assignment that yields the same result on every run;
// and the index creation uses CREATE UNIQUE INDEX IF NOT EXISTS. Re-applying the
// migration on an already-migrated database is therefore a no-op.
func migrateV1_7_0_toV1_8_0(tx *sql.Tx) error {
	exists, err := columnExists(tx, "sprints", "order_index")
	if err != nil {
		return err
	}
	if !exists {
		// Temporary DEFAULT 0 lets the ADD COLUMN succeed against existing rows
		// under NOT NULL; the backfill below overwrites every row with a unique
		// positive value before the unique index is created. The CHECK(order_index
		// > 0) constraint is intentionally NOT included on the ALTER: SQLite
		// evaluates a column CHECK against the DEFAULT for every existing row at
		// ADD COLUMN time, so a "> 0" CHECK with DEFAULT 0 would fail on a
		// populated table. The constraint lives in the fresh-schema CREATE TABLE
		// (SPEC/DATABASE.md § sprints Table); the > 0 invariant on migrated
		// databases is upheld by the deterministic positive backfill below and by
		// application-level validation on every write.
		if _, err := tx.Exec(
			`ALTER TABLE sprints ADD COLUMN order_index INTEGER NOT NULL DEFAULT 0`,
		); err != nil {
			return fmt.Errorf("adding order_index column: %w", err)
		}
	}

	// Backfill a deterministic, unique, positive execution order across all
	// sprints, ordered by created_at ascending, then id ascending as the
	// tie-breaker. The correlated count yields 1 for the earliest row and N for
	// the latest, so the result is always a dense 1..N sequence with no
	// collisions. Running it again is harmless: it recomputes the same values.
	if _, err := tx.Exec(`
		UPDATE sprints
		SET order_index = (
			SELECT COUNT(*)
			FROM sprints AS s2
			WHERE s2.created_at < sprints.created_at
			   OR (s2.created_at = sprints.created_at AND s2.id <= sprints.id)
		)
	`); err != nil {
		return fmt.Errorf("backfilling sprint order_index: %w", err)
	}

	// Create the unique index that enforces order uniqueness across the roadmap.
	if _, err := tx.Exec(
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_sprints_order ON sprints(order_index)`,
	); err != nil {
		return fmt.Errorf("creating idx_sprints_order: %w", err)
	}

	return nil
}

// migrateV1_8_0_toV1_9_0 creates the two comment tables, task_comments and
// sprint_comments, and the one index each of them needs (SPEC/VERSION.md
// § Migration 1.8.0 → 1.9.0).
//
// The migration adds no column to any existing table, so the columnExists guard
// used by every ALTER TABLE ADD COLUMN migration does not apply here: every
// statement carries IF NOT EXISTS and is therefore inherently idempotent.
//
// There is no backfill. Comments are new data with no pre-existing source, so an
// already-populated database migrates to two empty tables and every existing
// task and sprint simply has no comments until one is written.
//
// The DDL is deliberately a copy of the statements in CreateSchema rather than a
// shared constant: a migration is a historical record of the shape the schema had
// at one version, so it must not follow a later change to the fresh-schema
// definition. TestMigratedAndFreshCommentTablesAreIdentical asserts the two are
// identical today, which is what SPEC/VERSION.md § Migration 1.8.0 → 1.9.0
// guarantees; a future change to the comment tables belongs in a new migration,
// not in this one.
func migrateV1_8_0_toV1_9_0(tx *sql.Tx) error {
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS task_comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id INTEGER NOT NULL,               -- Owning task
    type TEXT NOT NULL CHECK(type IN ('FINDING', 'HYPOTHESIS', 'TEST', 'DECISION', 'PROGRESS', 'UPDATE', 'NOTE')),
    body TEXT NOT NULL CHECK(length(body) <= 4096),  -- Comment text, max 4096 chars
    created_at TEXT NOT NULL,               -- ISO 8601 UTC, set when the comment is created
    updated_at TEXT,                        -- ISO 8601 UTC, NULL until the comment is edited
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("creating task_comments table: %w", err)
	}

	if _, err := tx.Exec(
		`CREATE INDEX IF NOT EXISTS idx_task_comments_task_created ON task_comments(task_id, created_at ASC)`,
	); err != nil {
		return fmt.Errorf("creating idx_task_comments_task_created: %w", err)
	}

	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS sprint_comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sprint_id INTEGER NOT NULL,             -- Owning sprint
    type TEXT NOT NULL CHECK(type IN ('FINDING', 'DECISION', 'PROGRESS', 'UPDATE')),
    body TEXT NOT NULL CHECK(length(body) <= 4096),  -- Comment text, max 4096 chars
    created_at TEXT NOT NULL,               -- ISO 8601 UTC, set when the comment is created
    updated_at TEXT,                        -- ISO 8601 UTC, NULL until the comment is edited
    FOREIGN KEY (sprint_id) REFERENCES sprints(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("creating sprint_comments table: %w", err)
	}

	if _, err := tx.Exec(
		`CREATE INDEX IF NOT EXISTS idx_sprint_comments_sprint_created ON sprint_comments(sprint_id, created_at ASC)`,
	); err != nil {
		return fmt.Errorf("creating idx_sprint_comments_sprint_created: %w", err)
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

// migrateV1_9_0_toV1_10_0 drops the specialists column from the tasks table
// (SPEC/VERSION.md § Migration 1.9.0 → 1.10.0).
//
// THE MIGRATION DESTROYS DATA, AND THAT IS ITS PURPOSE. Every value stored in
// tasks.specialists is discarded and is not recoverable from the database
// afterwards. There is deliberately no backfill, no archive column, and no audit
// entry recording the discarded values: the change removes a field, and removing
// a field means removing what it held.
//
// A single ALTER TABLE … DROP COLUMN is sufficient and no table rebuild is
// required, because specialists is a plain nullable TEXT column: it is not a
// primary key or part of one, carries no UNIQUE constraint, is not indexed, is
// named in no CHECK constraint and in no partial index, is used by no foreign key
// and by no generated column, and appears in no view and in no trigger. Every
// other column keeps its values, its CHECK constraint and its DEFAULT clause, and
// the table's indexes and its parent_task_id self-reference survive the drop
// (SPEC/DATABASE.md § Migration Idempotency (ALTER TABLE DROP COLUMN)).
//
// Idempotent: the DROP COLUMN is guarded by columnExists read in the opposite
// sense to the ADD COLUMN migrations — the statement runs only WHILE THE COLUMN
// IS STILL PRESENT. Without the guard a second apply raises "no such column:
// \"specialists\"" and the whole migration set fails.
//
// The audit log is NOT rewritten. Rows whose operation is TASK_ASSIGN or
// TASK_UNASSIGN are retained untouched: audit.operation carries no CHECK, so
// they need no schema accommodation, and a roadmap's recorded history stays
// complete (SPEC/DATABASE.md § audit Table).
func migrateV1_9_0_toV1_10_0(tx *sql.Tx) error {
	exists, err := columnExists(tx, "tasks", "specialists")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if _, err := tx.Exec(`ALTER TABLE tasks DROP COLUMN specialists`); err != nil {
		return fmt.Errorf("dropping specialists column: %w", err)
	}
	return nil
}

// migrateV1_10_0_toV1_11_0 adds the two commit-tracking columns, commit_open and
// commit_close, to the tasks table (SPEC/VERSION.md § Migration 1.10.0 → 1.11.0).
//
// Both are nullable TEXT columns carrying a git commit hash, and each takes the
// CHECK constraint specified in SPEC/DATABASE.md § Commit Hash Format Constraint
// WITH the column. SQLite accepts a CHECK clause on ADD COLUMN and the constraint
// is live on the migrated table immediately afterwards, so a migrated database
// and a fresh one end up with identical tasks tables;
// TestMigratedAndFreshTasksTablesAreIdentical is what stops the two copies of
// the DDL drifting apart.
//
// THERE IS NO BACKFILL, AND THERE CAN BE NONE. The two values record which commit
// a task was started from and which it was concluded at. Groadmap holds no record
// of either fact for work already done — it runs no git command and reads no
// repository — so nothing on disk could supply a truthful value for a task that
// reached DOING or COMPLETED before this migration. Every existing task therefore
// migrates with NULL in both columns and keeps them until its next transition
// into DOING or COMPLETED supplies a value. That is also why the columns are
// nullable even though the CLI makes them mandatory on those transitions: the
// mandatory rule governs the transition, not the column.
//
// SQLite does not re-validate existing rows when a column is added, and it does
// not need to here: every existing row receives NULL, and NULL satisfies both
// CHECKs.
//
// Idempotent: each ADD COLUMN is guarded INDEPENDENTLY by its own columnExists
// check, so re-running the migration is a no-op rather than a "duplicate column
// name" error, and a database that somehow carries one column and not the other
// is brought to the full shape.
//
// No index is created on either column. Neither is a filter, a sort key or a
// join key for any query in SPEC/DATABASE.md § Main SQL Queries: both are read as
// part of the Task object and are never searched, so an index would cost write
// time on every status change and buy nothing.
func migrateV1_10_0_toV1_11_0(tx *sql.Tx) error {
	for _, column := range []struct {
		name string
		ddl  string
	}{
		{
			name: "commit_open",
			ddl:  `ALTER TABLE tasks ADD COLUMN commit_open TEXT CHECK(commit_open IS NULL OR (length(commit_open) BETWEEN 7 AND 64 AND commit_open NOT GLOB '*[^0-9a-f]*'))`,
		},
		{
			name: "commit_close",
			ddl:  `ALTER TABLE tasks ADD COLUMN commit_close TEXT CHECK(commit_close IS NULL OR (length(commit_close) BETWEEN 7 AND 64 AND commit_close NOT GLOB '*[^0-9a-f]*'))`,
		},
	} {
		exists, err := columnExists(tx, "tasks", column.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := tx.Exec(column.ddl); err != nil {
			return fmt.Errorf("adding %s column: %w", column.name, err)
		}
	}
	return nil
}

// migrateV1_11_0_toV1_12_0 adds the two new columns of the audit table,
// related_entity_id and commit_hash, and reclassifies the legacy
// TASK_STATUS_CHANGE entries whose destination state the stored data determines
// beyond doubt (SPEC/VERSION.md § Migration 1.11.0 to 1.12.0).
//
// NO TABLE REBUILD. Both shape changes are new columns, and ALTER TABLE ADD
// COLUMN carries each column's CHECK with it, so nothing about an existing
// column changes: entity_type keeps its CHECK(entity_type IN ('TASK', 'SPRINT'))
// unaltered, entity_id keeps its definition, and no constraint is widened,
// narrowed, or added to a column that already exists. The migration is therefore
// two guarded ALTER TABLE statements and three UPDATE statements — no
// replacement table, no row copy, no drop, and no rename.
//
// The physical column order consequently differs between a migrated and a fresh
// database, and NOTHING MAY DEPEND ON IT. ADD COLUMN appends, so a migrated
// audit table ends `... entity_id, performed_at, related_entity_id,
// commit_hash`, while a fresh one ends `... entity_id, related_entity_id,
// commit_hash, performed_at`. The two are equivalent because every statement in
// this package names its columns explicitly and none uses SELECT * or positional
// binding. Reordering them would require the rebuild this migration refuses.
//
// NEITHER NEW COLUMN IS BACKFILLED, and there is no truthful backfill available
// for either. commit_hash records which commit a task was started from or
// concluded at, and Groadmap runs no git command and reads no repository; the
// two columns that could have supplied a value, tasks.commit_open and
// tasks.commit_close, arrived only at 1.11.0 and are NULL for every task older
// than it. related_entity_id records the counterpart entity of the operation
// that wrote the row, and a stored SPRINT_ADD_TASK row names its sprint and
// nothing else — sprint_tasks says which tasks are members NOW, which is a
// different question and cannot say which of them a given row was about.
//
// Idempotent: each ADD COLUMN is guarded INDEPENDENTLY by its own columnExists
// check, so re-running the migration is a no-op rather than a "duplicate column
// name" error, and a database that somehow carries one column and not the other
// is brought to the full shape. The three UPDATE statements are idempotent by
// construction: a row the first run rewrites no longer carries
// TASK_STATUS_CHANGE, so a second run matches nothing.
//
// No index is created on either column: neither is a filter, a sort key or a
// join key for any audit read — the read statement's predicates are operation,
// entity_type, entity_id and performed_at only — so an index would cost write
// time on every audited operation and buy nothing.
func migrateV1_11_0_toV1_12_0(tx *sql.Tx) error {
	for _, column := range []struct {
		name string
		ddl  string
	}{
		{
			name: "related_entity_id",
			ddl:  `ALTER TABLE audit ADD COLUMN related_entity_id INTEGER CHECK(related_entity_id IS NULL OR related_entity_id > 0)`,
		},
		{
			name: "commit_hash",
			ddl:  `ALTER TABLE audit ADD COLUMN commit_hash TEXT CHECK(commit_hash IS NULL OR (length(commit_hash) BETWEEN 7 AND 64 AND commit_hash NOT GLOB '*[^0-9a-f]*'))`,
		},
	} {
		exists, err := columnExists(tx, "audit", column.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := tx.Exec(column.ddl); err != nil {
			return fmt.Errorf("adding audit.%s column: %w", column.name, err)
		}
	}

	for _, step := range reclassifyStatusChangeSteps {
		if _, err := tx.Exec(step.update); err != nil {
			return fmt.Errorf("reclassifying TASK_STATUS_CHANGE entries as %s: %w", step.operation, err)
		}
	}

	return nil
}

// migrateV1_12_0_toV1_13_0 tightens idx_sprint_tasks_order into a UNIQUE index,
// so that no two member tasks of one sprint can hold the same position and the
// sprint's planned execution order is total (SPEC/VERSION.md § Migration 1.12.0
// → 1.13.0; SPEC/DATABASE.md § Position Uniqueness Within a Sprint).
//
// It follows the sequence SPEC/DATABASE.md § Introducing a Uniqueness Constraint
// over Existing Rows requires, and the reason the two steps are ordered this way
// is that CREATE UNIQUE INDEX validates every existing row at creation time:
//
//  1. Repair the existing rows FIRST, while no unique index is in force, so the
//     repair's own intermediate states cannot violate one.
//  2. Then replace the non-unique index with its unique form under the same
//     name, so the schema ends with exactly ONE index over (sprint_id, position)
//     serving both the ordering reads and the constraint.
//
// Both steps run inside the single transaction runMigration opens, which is what
// makes step 2 safe to fail: the repair is rolled back with it and
// _metadata.schema_version keeps its previous value, leaving the database exactly
// as it was. The migration NEVER deletes a sprint_tasks row to make the index
// succeed — a duplicate position is an ambiguous order, not a redundant
// membership, so both tasks are genuine members and both must survive.
//
// Idempotent: the repair assigns each row the rank it already holds on a database
// that is already dense and distinct, and the index step is DROP … IF EXISTS
// followed by CREATE … IF NOT EXISTS. Re-applying the migration is a no-op.
func migrateV1_12_0_toV1_13_0(tx *sql.Tx) error {
	// Step 1 — repair. Renumber every sprint's positions to a dense 0..N-1 run.
	//
	// The ranking is computed in a SUBQUERY that is evaluated as a unit and
	// joined to the target, NOT by a correlated count over the table being
	// written. SQLite applies an UPDATE row by row and a correlated subquery in
	// the SET clause observes rows the same statement has already rewritten, so
	// ranking by the very column being written is wrong: measured against the
	// pinned driver on the input (t10@0, t11@0, t12@1, t13@1) the correlated form
	// left t12 and t13 BOTH at 2, trading one collision for another. This is the
	// form migration 1.1.0 already uses (SPEC/DATABASE.md § Introducing a
	// Uniqueness Constraint over Existing Rows, The repair must not read its own
	// writes).
	//
	// The ordering keys are what make the repair honest. position ASC keeps the
	// order the roadmap's owner planned and task_id ASC breaks precisely the ties
	// that are the rows in violation. Ranking by added_at would also produce a
	// valid dense run while silently replacing the planned order with the order
	// the tasks happened to be added in, which is data loss dressed up as a
	// repair — so it is deliberately NOT used here, even though the 1.1.0
	// migration ranks by added_at (there, no position existed yet to preserve).
	//
	// The repair is unconditional rather than guarded by a duplicate count: a
	// database that already satisfies the constraint and is already dense
	// receives the value each row already holds, a database with gaps has them
	// closed, and a database with duplicates has them separated. A measurement of
	// one database establishes nothing about anyone else's, which is why the
	// repair always runs.
	if _, err := tx.Exec(`
		UPDATE sprint_tasks
		SET position = ranked.new_pos
		FROM (
			SELECT sprint_id, task_id,
			       ROW_NUMBER() OVER (
			           PARTITION BY sprint_id
			           ORDER BY position ASC, task_id ASC
			       ) - 1 AS new_pos
			FROM sprint_tasks
		) AS ranked
		WHERE sprint_tasks.sprint_id = ranked.sprint_id
		  AND sprint_tasks.task_id   = ranked.task_id
	`); err != nil {
		return fmt.Errorf("repairing sprint task positions: %w", err)
	}

	// Step 2 — tighten the index. idx_sprint_tasks_order already covers
	// (sprint_id, position ASC), so a separate unique index over the same pair
	// would be an exact duplicate. The existing index is dropped and recreated
	// under the same name as UNIQUE instead.
	if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_sprint_tasks_order`); err != nil {
		return fmt.Errorf("dropping idx_sprint_tasks_order: %w", err)
	}
	if _, err := tx.Exec(
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_sprint_tasks_order ON sprint_tasks(sprint_id, position ASC)`,
	); err != nil {
		return fmt.Errorf("creating unique idx_sprint_tasks_order: %w", err)
	}

	return nil
}

// reclassifyStatusChangeSteps rewrites the operation of a legacy
// TASK_STATUS_CHANGE entry to the destination-specific operation ONLY WHERE THE
// STORED DATA DETERMINES THAT DESTINATION BY EXACT EQUALITY (SPEC/VERSION.md
// § Migration 1.11.0 to 1.12.0, Reclassifying TASK_STATUS_CHANGE). The rule has
// no tolerance window, no nearest-match, and no ordering heuristic, and the
// statements are transcribed from that section rather than generated, because
// the specification's SQL is the contract.
//
// An entry is reclassified when its performed_at is exactly equal to exactly one
// of the owning task's three lifecycle timestamps. It keeps TASK_STATUS_CHANGE
// when its performed_at matches none of them (a transition to BACKLOG stamps no
// timestamp, and a reopening clears the ones that would have matched), when it
// matches more than one (two transitions recorded at the same instant leave no
// evidence of which one the entry recorded), and when the task named by
// entity_id no longer exists (a deleted task takes its timestamps with it).
//
// WHAT THE STATEMENTS DELIBERATELY DO NOT DO:
//
//   - They do not infer. Choosing the nearest timestamp, ordering the entries and
//     assigning destinations by position, or reading a task's current status to
//     guess the destination would each write a fact the database does not hold.
//   - They do not touch TASK_UPDATE or SPRINT_UPDATE, whose entries record a
//     field edit that stamps no timestamp anywhere, nor SPRINT_MOVE_TASK, whose
//     entries name one sprint and no task. The `operation = 'TASK_STATUS_CHANGE'`
//     predicate is what excludes them, and the `entity_type = 'TASK'` predicate
//     keeps a sprint-scoped entry out even if its entity_id collided with a task
//     id.
//   - They delete and renumber nothing. No DELETE, no id rewrite, no compaction:
//     afterwards the table holds exactly the entries it held before, with the
//     same id and performed_at values, and only some operation values changed.
//   - They write no audit entry of their own. A migration is not a roadmap
//     operation.
//
// The three are independent of each other and of their order: the exclusion
// clauses make their WHERE conditions mutually exclusive, so no entry can satisfy
// two of them. `t.started_at = audit.performed_at` is false when t.started_at is
// NULL, because a comparison with NULL yields NULL rather than true, so the NULL
// cases need no extra guard on the matched timestamp — only on the two excluded
// ones.
var reclassifyStatusChangeSteps = []struct {
	operation string
	update    string
}{
	{
		operation: "TASK_STATUS_DOING",
		update: `
UPDATE audit
SET operation = 'TASK_STATUS_DOING'
WHERE operation = 'TASK_STATUS_CHANGE'
  AND entity_type = 'TASK'
  AND EXISTS (
      SELECT 1 FROM tasks t
      WHERE t.id = audit.entity_id
        AND t.started_at = audit.performed_at
        AND (t.tested_at IS NULL OR t.tested_at <> audit.performed_at)
        AND (t.closed_at IS NULL OR t.closed_at <> audit.performed_at)
  )`,
	},
	{
		operation: "TASK_STATUS_TESTING",
		update: `
UPDATE audit
SET operation = 'TASK_STATUS_TESTING'
WHERE operation = 'TASK_STATUS_CHANGE'
  AND entity_type = 'TASK'
  AND EXISTS (
      SELECT 1 FROM tasks t
      WHERE t.id = audit.entity_id
        AND t.tested_at = audit.performed_at
        AND (t.started_at IS NULL OR t.started_at <> audit.performed_at)
        AND (t.closed_at IS NULL OR t.closed_at <> audit.performed_at)
  )`,
	},
	{
		operation: "TASK_STATUS_COMPLETED",
		update: `
UPDATE audit
SET operation = 'TASK_STATUS_COMPLETED'
WHERE operation = 'TASK_STATUS_CHANGE'
  AND entity_type = 'TASK'
  AND EXISTS (
      SELECT 1 FROM tasks t
      WHERE t.id = audit.entity_id
        AND t.closed_at = audit.performed_at
        AND (t.started_at IS NULL OR t.started_at <> audit.performed_at)
        AND (t.tested_at IS NULL OR t.tested_at <> audit.performed_at)
  )`,
	},
}
