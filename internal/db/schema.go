package db

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// SchemaVersion is the current database schema version.
const SchemaVersion = "1.1.0"

// CreateSchema creates all database tables and indexes.
// This implements the DDL from SPEC/DATABASE.md.
func (db *DB) CreateSchema() error {
	// Tasks table - aligned with SPEC/DATABASE.md v1.0.0
	tasksDDL := `
CREATE TABLE IF NOT EXISTS tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- Group 1: Content fields (TEXT) - frequently accessed together
    title TEXT NOT NULL CHECK(length(title) <= 255),
    status TEXT NOT NULL DEFAULT 'BACKLOG' CHECK(status IN ('BACKLOG', 'SPRINT', 'DOING', 'TESTING', 'COMPLETED')),
    type TEXT NOT NULL DEFAULT 'TASK' CHECK(type IN ('USER_STORY', 'TASK', 'BUG', 'SUB_TASK', 'EPIC', 'REFACTOR', 'CHORE', 'SPIKE', 'DESIGN_UX', 'IMPROVEMENT')),
    functional_requirements TEXT NOT NULL CHECK(length(functional_requirements) <= 4096),
    technical_requirements TEXT NOT NULL CHECK(length(technical_requirements) <= 4096),
    acceptance_criteria TEXT NOT NULL CHECK(length(acceptance_criteria) <= 4096),
    created_at TEXT NOT NULL,

    -- Group 2: Nullable tracking fields - lifecycle timestamps
    specialists TEXT,
    started_at TEXT,
    tested_at TEXT,
    closed_at TEXT,

    -- Group 3: Numeric metadata fields
    priority INTEGER NOT NULL DEFAULT 0 CHECK(priority >= 0 AND priority <= 9),
    severity INTEGER NOT NULL DEFAULT 0 CHECK(severity >= 0 AND severity <= 9)
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_type ON tasks(type);
CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority);
CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);

-- Composite indexes for multi-criteria queries (TASK-P001)
CREATE INDEX IF NOT EXISTS idx_tasks_status_priority ON tasks(status, priority DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_priority_created ON tasks(priority DESC, created_at ASC);
`

	// Sprints table
	sprintsDDL := `
CREATE TABLE IF NOT EXISTS sprints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING', 'OPEN', 'CLOSED')),
    description TEXT NOT NULL,
    created_at TEXT NOT NULL,
    started_at TEXT,
    closed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_sprints_status ON sprints(status);
CREATE INDEX IF NOT EXISTS idx_sprints_created_at ON sprints(created_at);
`

	// Sprint tasks junction table
	sprintTasksDDL := `
CREATE TABLE IF NOT EXISTS sprint_tasks (
    sprint_id INTEGER NOT NULL,
    task_id INTEGER NOT NULL UNIQUE,
    added_at TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,  -- 0-based position in sprint task order
    PRIMARY KEY (sprint_id, task_id),
    FOREIGN KEY (sprint_id) REFERENCES sprints(id) ON DELETE CASCADE,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_sprint_tasks_task_id ON sprint_tasks(task_id);

-- Composite index for sprint task lookups (TASK-P001)
CREATE INDEX IF NOT EXISTS idx_sprint_tasks_lookup ON sprint_tasks(sprint_id, task_id);

-- Composite index for sprint task ordering (TASK-P001)
CREATE INDEX IF NOT EXISTS idx_sprint_tasks_order ON sprint_tasks(sprint_id, position ASC);
`

	// Audit table
	auditDDL := `
CREATE TABLE IF NOT EXISTS audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation TEXT NOT NULL,
    entity_type TEXT NOT NULL CHECK(entity_type IN ('TASK', 'SPRINT')),
    entity_id INTEGER NOT NULL,
    performed_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_operation ON audit(operation);
CREATE INDEX IF NOT EXISTS idx_audit_performed_at ON audit(performed_at);

-- Composite index for audit date range queries (TASK-P001)
CREATE INDEX IF NOT EXISTS idx_audit_date ON audit(performed_at DESC);
`

	// Metadata table
	metadataDDL := `
CREATE TABLE IF NOT EXISTS _metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

	// Execute all DDL statements
	statements := []string{tasksDDL, sprintsDDL, sprintTasksDDL, auditDDL, metadataDDL}
	for _, ddl := range statements {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("executing schema DDL: %w", err)
		}
	}

	// Insert metadata
	if err := db.insertMetadata(); err != nil {
		return fmt.Errorf("inserting metadata: %w", err)
	}

	return nil
}

// insertMetadata inserts the initial metadata values.
func (db *DB) insertMetadata() error {
	now := utils.NowISO8601()

	metadata := map[string]string{
		"schema_version": SchemaVersion,
		"created_at":     now,
		"application":    "Groadmap",
	}

	for key, value := range metadata {
		_, err := db.Exec(
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
