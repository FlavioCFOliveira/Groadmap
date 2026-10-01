package db

import (
	"database/sql"
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// SchemaVersion is the current database schema version.
const SchemaVersion = "1.17.0"

// The DDL of each table and its indexes, one raw string literal per table.
// Without the newline that opens it, each literal is byte-identical to the block
// of its table in SPEC/DATABASE.md § DDL - Table Creation, and a test enforces
// the identity for every table: a change to a table's DDL is made in its block
// first and copied here unchanged.
const (
	// Tasks table - aligned with SPEC/DATABASE.md v1.0.0
	tasksDDL = `
CREATE TABLE IF NOT EXISTS tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- Group 1: Content fields (TEXT) - frequently accessed together
    -- Length limits are validated by the application first and enforced by the CHECK constraints
    title TEXT NOT NULL CHECK(length(title) <= 255),                    -- Task title/summary, max 255 chars
    status TEXT NOT NULL DEFAULT 'BACKLOG' CHECK(status IN ('BACKLOG', 'SPRINT', 'DOING', 'TESTING', 'COMPLETED')),
    type TEXT NOT NULL DEFAULT 'TASK' CHECK(type IN ('USER_STORY', 'TASK', 'BUG', 'SUB_TASK', 'EPIC', 'REFACTOR', 'CHORE', 'SPIKE', 'DESIGN_UX', 'IMPROVEMENT')),
    functional_requirements TEXT NOT NULL CHECK(length(functional_requirements) <= 4096),    -- Why: functional requirements, max 4096 chars
    technical_requirements TEXT NOT NULL CHECK(length(technical_requirements) <= 4096),   -- How: technical description, max 4096 chars
    acceptance_criteria TEXT NOT NULL CHECK(length(acceptance_criteria) <= 4096),      -- How to verify: completion criteria, max 4096 chars
    created_at TEXT NOT NULL,               -- ISO 8601 UTC, set on task creation

    -- Group 2: Nullable tracking fields - lifecycle timestamps
    started_at TEXT,                        -- ISO 8601 UTC, set when task moves to DOING
    tested_at TEXT,                         -- ISO 8601 UTC, set when task moves to TESTING
    closed_at TEXT,                         -- ISO 8601 UTC, set when task moves to COMPLETED
    completion_summary TEXT CHECK(completion_summary IS NULL OR length(completion_summary) <= 4096),  -- Optional summary of work done, set only on TESTING → COMPLETED
    -- Git commit hashes bracketing the work. Stored lowercase; the CHECK rejects any other case
    -- because GLOB is case-sensitive in SQLite, so it backs the application's lowercase normalisation.
    commit_open TEXT CHECK(commit_open IS NULL OR (length(commit_open) BETWEEN 7 AND 64 AND commit_open NOT GLOB '*[^0-9a-f]*')),    -- Commit the task was started from, set on every transition into DOING
    commit_close TEXT CHECK(commit_close IS NULL OR (length(commit_close) BETWEEN 7 AND 64 AND commit_close NOT GLOB '*[^0-9a-f]*')),  -- Commit the task was concluded at, set on every transition into COMPLETED
    parent_task_id INTEGER REFERENCES tasks(id),  -- NULL for top-level tasks; non-NULL links to parent task (sub-task hierarchy)

    -- Group 3: Numeric metadata fields
    priority INTEGER NOT NULL DEFAULT 0 CHECK(priority >= 0 AND priority <= 9),
    severity INTEGER NOT NULL DEFAULT 0 CHECK(severity >= 0 AND severity <= 9)
);

-- Covers: the creation-date ordering of the task listing
CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);

-- Composite indexes, each supplying one ordering of the task listing in full,
-- tie-breaker included, so the listing needs no sort step. No single-column index
-- on status or priority: each would be a leading prefix of one of these
-- (see DATABASE.md § Index Design Rationale).
-- Covers: the status filter in the default ordering, and the status ordering
CREATE INDEX IF NOT EXISTS idx_tasks_status_priority ON tasks(status, priority DESC, created_at ASC);
-- Covers: the type filter in the default ordering
CREATE INDEX IF NOT EXISTS idx_tasks_type ON tasks(type, priority DESC, created_at ASC);
-- Covers: the default ordering (matches ListTasks ORDER BY)
CREATE INDEX IF NOT EXISTS idx_tasks_priority_created ON tasks(priority DESC, created_at ASC);
-- Covers: the severity ordering
CREATE INDEX IF NOT EXISTS idx_tasks_severity_priority ON tasks(severity DESC, priority DESC, created_at ASC);

-- Covers: sub-task hierarchy lookups
CREATE INDEX IF NOT EXISTS idx_tasks_parent_task_id ON tasks(parent_task_id);
`

	// sprintsDDL is the DDL of the sprints table and its indexes.
	sprintsDDL = `
CREATE TABLE IF NOT EXISTS sprints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING', 'OPEN', 'CLOSED')),
    title TEXT NOT NULL CHECK(length(title) <= 255),  -- Sprint title, max 255 chars
    description TEXT NOT NULL,
    created_at TEXT NOT NULL,  -- ISO 8601 UTC
    started_at TEXT,           -- ISO 8601 UTC, NULL if not started
    closed_at TEXT,            -- ISO 8601 UTC, NULL if not closed
    max_tasks INTEGER,         -- NULL means unlimited capacity
    order_index INTEGER NOT NULL CHECK(order_index > 0)  -- Sprint execution order; positive integer (> 0), unique across the roadmap (see idx_sprints_order). Column named order_index because ORDER is a reserved SQL keyword.
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_sprints_status ON sprints(status);

-- At most one sprint is OPEN at a time. The commands that open a sprint refuse
-- when another is already OPEN, but that check runs before their write
-- transaction; this partial unique index rejects the second OPEN row when two
-- concurrent processes pass the check together.
CREATE UNIQUE INDEX IF NOT EXISTS idx_one_open_sprint ON sprints(status) WHERE status = 'OPEN';

-- Uniqueness of the sprint execution order across the roadmap.
-- Enforces that no two sprints share the same order_index value; an attempt to
-- insert or update a colliding value fails the constraint and is surfaced to the
-- caller as exit code 5 (see ARCHITECTURE.md § Exit Codes, ErrAlreadyExists).
CREATE UNIQUE INDEX IF NOT EXISTS idx_sprints_order ON sprints(order_index);
`

	// Sprint tasks junction table
	sprintTasksDDL = `
CREATE TABLE IF NOT EXISTS sprint_tasks (
    sprint_id INTEGER NOT NULL,
    task_id INTEGER NOT NULL UNIQUE,
    added_at TEXT NOT NULL,  -- ISO 8601 UTC
    position INTEGER NOT NULL DEFAULT 0,  -- 0-based position in sprint task order; unique within one sprint (idx_sprint_tasks_order)
    PRIMARY KEY (sprint_id, task_id),
    FOREIGN KEY (sprint_id) REFERENCES sprints(id) ON DELETE CASCADE,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

-- The implicit indexes of the PRIMARY KEY (sprint_id, task_id) and of UNIQUE(task_id)
-- are the lookup indexes: no index is declared over either column set, because it
-- would duplicate one of them exactly.

-- Unique composite index for sprint task ordering (TASK-ORDER-001)
-- Covers: sprint task listing ordered by position.
-- Enforces: no two member tasks of one sprint hold the same position, which is what
-- makes the planned execution order total. One index serves both the ordering reads
-- and the constraint, so the invariant costs no second B-tree
-- (see DATABASE.md § Position Uniqueness Within a Sprint).
CREATE UNIQUE INDEX IF NOT EXISTS idx_sprint_tasks_order ON sprint_tasks(sprint_id, position ASC);
`

	// Audit table
	auditDDL = `
CREATE TABLE IF NOT EXISTS audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation TEXT NOT NULL,
    entity_type TEXT NOT NULL CHECK(entity_type IN ('TASK', 'SPRINT')),
    entity_id INTEGER NOT NULL,
    related_entity_id INTEGER CHECK(related_entity_id IS NULL OR related_entity_id > 0),   -- Counterpart entity of the operation that produced the row; NULL when it has no counterpart
    commit_hash TEXT CHECK(commit_hash IS NULL OR (length(commit_hash) BETWEEN 7 AND 64 AND commit_hash NOT GLOB '*[^0-9a-f]*')),   -- Git commit bracketing the work; NULL on every operation but two
    performed_at TEXT NOT NULL  -- ISO 8601 UTC
);

-- Each carries performed_at DESC after its equality columns, so the read it serves
-- is returned in the audit order with no sort step.
-- Covers: the entity history (entity_type and entity_id)
CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit(entity_type, entity_id, performed_at DESC);
-- Covers: the operation filter, alone or combined with the entity-type filter
CREATE INDEX IF NOT EXISTS idx_audit_operation ON audit(operation, performed_at DESC, entity_type);

-- Covers: the unfiltered log and the date range filters. One index on performed_at,
-- not two: SQLite reads an index in either direction.
CREATE INDEX IF NOT EXISTS idx_audit_date ON audit(performed_at DESC);
`

	// Metadata table. Its rows are inserted with bound values by
	// insertMetadataTx, not by this DDL.
	metadataDDL = `
CREATE TABLE IF NOT EXISTS _metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

	// Task dependencies table
	taskDependenciesDDL = `
CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id INTEGER NOT NULL,               -- The dependent task
    depends_on_task_id INTEGER NOT NULL,    -- The task it depends on (the blocker)
    PRIMARY KEY (task_id, depends_on_task_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    FOREIGN KEY (depends_on_task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

-- No index on task_id alone: it is the leading column of the primary key.
CREATE INDEX IF NOT EXISTS idx_task_deps_depends_on ON task_dependencies(depends_on_task_id);
`

	// Task comments table - the durable, typed record of the work carried out
	// within the scope of a task (SPEC/DATABASE.md § task_comments Table).
	taskCommentsDDL = `
CREATE TABLE IF NOT EXISTS task_comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id INTEGER NOT NULL,               -- Owning task
    type TEXT NOT NULL CHECK(type IN ('FINDING', 'HYPOTHESIS', 'TEST', 'DECISION', 'PROGRESS', 'UPDATE', 'NOTE')),
    body TEXT NOT NULL CHECK(length(body) <= 4096),  -- Comment text, max 4096 chars
    created_at TEXT NOT NULL,               -- ISO 8601 UTC, set when the comment is created
    updated_at TEXT,                        -- ISO 8601 UTC, NULL until the comment is edited
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

-- Composite index for comment listing
-- Covers: the parent lookup and the chronological listing order in one index
CREATE INDEX IF NOT EXISTS idx_task_comments_task_created ON task_comments(task_id, created_at ASC);
`

	// Sprint comments table - the progression record of a sprint. The type CHECK
	// enumerates four values, not seven: HYPOTHESIS, TEST and NOTE are task-only
	// (SPEC/DATABASE.md § sprint_comments Table).
	sprintCommentsDDL = `
CREATE TABLE IF NOT EXISTS sprint_comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sprint_id INTEGER NOT NULL,             -- Owning sprint
    type TEXT NOT NULL CHECK(type IN ('FINDING', 'DECISION', 'PROGRESS', 'UPDATE')),
    body TEXT NOT NULL CHECK(length(body) <= 4096),  -- Comment text, max 4096 chars
    created_at TEXT NOT NULL,               -- ISO 8601 UTC, set when the comment is created
    updated_at TEXT,                        -- ISO 8601 UTC, NULL until the comment is edited
    FOREIGN KEY (sprint_id) REFERENCES sprints(id) ON DELETE CASCADE
);

-- Composite index for comment listing
-- Covers: the parent lookup and the chronological listing order in one index
CREATE INDEX IF NOT EXISTS idx_sprint_comments_sprint_created ON sprint_comments(sprint_id, created_at ASC);
`
)

// CreateSchema creates all database tables and indexes, and the three
// _metadata rows, in ONE transaction. This implements the DDL from
// SPEC/DATABASE.md.
//
// Either the whole schema and its metadata commit, or none of them does: a
// failure leaves no table, no index and no _metadata row behind, so no database
// can hold a partial schema or a schema without a schema_version
// (SPEC/DATABASE.md § Transactional Atomicity Guarantees, item 10). One
// transaction is also one commit, where executing each statement on its own
// committed, and synced, once per statement.
func (db *DB) CreateSchema() error {
	return db.createSchema(schemaDDL())
}

// schemaDDL returns the DDL statements of the current schema, in the order they
// must run.
func schemaDDL() []string {
	// The comment tables come last: each carries a foreign key onto a table
	// declared above it.
	return []string{
		tasksDDL, sprintsDDL, sprintTasksDDL, auditDDL, metadataDDL, taskDependenciesDDL,
		taskCommentsDDL, sprintCommentsDDL,
	}
}

// createSchema executes statements and then inserts the metadata rows, all
// inside one transaction that is committed only when every step succeeded. It
// takes the statements as a parameter so a test can make one of them fail and
// observe that nothing was left behind.
func (db *DB) createSchema(statements []string) error {
	return db.createSchemaTx(statements, false)
}

// createSchemaIfAbsent is CreateSchema for a database that may be initialised
// by another invocation at the same moment: an uninitialised, zero-byte
// project.db two commands found together (SPEC/DATABASE.md § Opening a Roadmap
// Database File). The transaction first asks whether _metadata exists and
// creates nothing when it does, so the schema is created once and _metadata
// holds one row per key. The question and the creation share one transaction:
// a concurrent creator that commits between them makes this transaction's
// first write fail as busy, and the retry policy runs it again, when the
// question finds the schema the other created.
func (db *DB) createSchemaIfAbsent() error {
	return db.createSchemaTx(schemaDDL(), true)
}

// createSchemaTx is createSchema and createSchemaIfAbsent: onlyIfAbsent makes
// the transaction create nothing when the schema is already there.
func (db *DB) createSchemaTx(statements []string, onlyIfAbsent bool) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning schema transaction: %w", err)
	}
	// A rollback after a successful commit is a no-op that returns
	// sql.ErrTxDone; on every failure path it discards the partial schema.
	defer tx.Rollback() //nolint:errcheck // rollback on failure; the original error is returned

	if onlyIfAbsent {
		var present int
		if err := tx.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = '_metadata'").Scan(&present); err != nil {
			return fmt.Errorf("checking for an existing schema: %w", err)
		}
		if present > 0 {
			return nil
		}
	}

	for _, ddl := range statements {
		if _, err := tx.Exec(ddl); err != nil {
			return fmt.Errorf("executing schema DDL: %w", err)
		}
	}

	if err := insertMetadataTx(tx); err != nil {
		return fmt.Errorf("inserting metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing schema: %w", err)
	}
	return nil
}

// insertMetadataTx inserts the initial metadata values inside tx.
func insertMetadataTx(tx *sql.Tx) error {
	now := utils.NowISO8601()

	metadata := map[string]string{
		"schema_version": SchemaVersion,
		"created_at":     now,
		"application":    "Groadmap",
	}

	for key, value := range metadata {
		_, err := tx.Exec(
			"INSERT OR REPLACE INTO _metadata (key, value) VALUES (?, ?)",
			key, value,
		)
		if err != nil {
			return fmt.Errorf("inserting metadata %s: %w", key, err)
		}
	}

	return nil
}

// GetSchemaVersion returns the current schema version from the database.
func (db *DB) GetSchemaVersion() (string, error) {
	var version string
	err := db.QueryRow("SELECT value FROM _metadata WHERE key = 'schema_version'").Scan(&version)
	if err != nil {
		return "", fmt.Errorf("getting schema version: %w", err)
	}
	return version, nil
}
