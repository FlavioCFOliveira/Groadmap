# Database Schema

## Overview

Each roadmap is stored in an individual SQLite file. The schema is designed to be simple, efficient, and normalized.

## Naming Conventions

- **Tables**: snake_case, plural (`tasks`, `sprints`)
- **Columns**: snake_case (`created_at`, `expected_result`)
- **Primary keys**: `INTEGER PRIMARY KEY AUTOINCREMENT`
- **Indexes**: prefix `idx_` followed by table and column name

## SQLite File Structure

```
+----------------------------------------+
|           tasks                        |
|  - id (PK, AUTOINCREMENT)              |
|  - priority (INTEGER 0-9)              |
|  - severity (INTEGER 0-9)              |
|  - status (TEXT)                       |
|  - description (TEXT)                  |
|  - specialists (TEXT)                  |
|  - action (TEXT)                       |
|  - expected_result (TEXT)              |
|  - created_at (TEXT ISO8601)           |
|  - completed_at (TEXT ISO8601, NULL) |
+----------------------------------------+
|           sprints                      |
|  - id (PK, AUTOINCREMENT)              |
|  - status (TEXT)                       |
|  - description (TEXT)                  |
|  - created_at (TEXT ISO8601)           |
|  - started_at (TEXT ISO8601, NULL)     |
|  - closed_at (TEXT ISO8601, NULL)    |
+----------------------------------------+
|           sprint_tasks                 |
|  - sprint_id (FK → sprints.id)         |
|  - task_id (FK → tasks.id)             |
|  - added_at (TEXT ISO8601)             |
|  - Composite PK (sprint_id, task_id)   |
+----------------------------------------+
|           audit                        |
|  - id (PK, AUTOINCREMENT)              |
|  - operation (TEXT)                    |
|  - entity_type (TEXT)                  |
|  - entity_id (INTEGER)                 |
|  - performed_at (TEXT ISO8601)         |
+----------------------------------------+
|           _metadata                     |
|  - key (TEXT PK)                       |
|  - value (TEXT)                        |
+----------------------------------------+
```

---

## DDL - Table Creation

### `tasks` Table

```sql
CREATE TABLE IF NOT EXISTS tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    priority INTEGER NOT NULL DEFAULT 0 CHECK(priority >= 0 AND priority <= 9),
    severity INTEGER NOT NULL DEFAULT 0 CHECK(severity >= 0 AND severity <= 9),
    status TEXT NOT NULL DEFAULT 'BACKLOG' CHECK(status IN ('BACKLOG', 'SPRINT', 'DOING', 'TESTING', 'COMPLETED')),
    description TEXT NOT NULL,
    specialists TEXT,
    action TEXT NOT NULL,
    expected_result TEXT NOT NULL,
    created_at TEXT NOT NULL,  -- ISO 8601 UTC
    completed_at TEXT          -- ISO 8601 UTC, NULL if not complete
);

-- Indexes for frequent queries
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority);
CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);
```

### `sprints` Table

```sql
CREATE TABLE IF NOT EXISTS sprints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING', 'OPEN', 'CLOSED')),
    description TEXT NOT NULL,
    created_at TEXT NOT NULL,  -- ISO 8601 UTC
    started_at TEXT,           -- ISO 8601 UTC, NULL if not started
    closed_at TEXT             -- ISO 8601 UTC, NULL if not closed
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_sprints_status ON sprints(status);
CREATE INDEX IF NOT EXISTS idx_sprints_created_at ON sprints(created_at);
```

### `sprint_tasks` Table (N:M Relationship)

Junction table for many-to-many relationship between sprints and tasks.

```sql
CREATE TABLE IF NOT EXISTS sprint_tasks (
    sprint_id INTEGER NOT NULL,
    task_id INTEGER NOT NULL UNIQUE,
    added_at TEXT NOT NULL,  -- ISO 8601 UTC
    PRIMARY KEY (sprint_id, task_id),
    FOREIGN KEY (sprint_id) REFERENCES sprints(id) ON DELETE CASCADE,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_sprint_tasks_task_id ON sprint_tasks(task_id);
```

### `audit` Table

Logs all operations that change task or sprint state, enabling complete audit history.

```sql
CREATE TABLE IF NOT EXISTS audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id INTEGER NOT NULL,
    performed_at TEXT NOT NULL  -- ISO 8601 UTC
);

-- Indexes for efficient lookup
CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_operation ON audit(operation);
CREATE INDEX IF NOT EXISTS idx_audit_performed_at ON audit(performed_at);
```

**Fields:**
- `operation`: Operation type (e.g., `TASK_STATUS_CHANGE`, `SPRINT_START`). Values validated by application.
- `entity_type`: `'TASK'` or `'SPRINT'`. Values validated by application.
- `entity_id`: Affected entity ID
- `performed_at`: Operation timestamp

**Valid values (validated by application):**

**Tasks:**
- `TASK_CREATE` - New task created
- `TASK_DELETE` - Task deleted
- `TASK_STATUS_CHANGE` - Status change (BACKLOG → SPRINT → DOING → TESTING → COMPLETED)
- `TASK_PRIORITY_CHANGE` - Priority change (0-9)
- `TASK_SEVERITY_CHANGE` - Severity change (0-9)
- `TASK_UPDATE` - Generic update (description, action, expected_result, specialists)

**Sprints:**
- `SPRINT_CREATE` - New sprint created
- `SPRINT_DELETE` - Sprint deleted
- `SPRINT_START` - Sprint started (PENDING → OPEN)
- `SPRINT_CLOSE` - Sprint closed (OPEN → CLOSED)
- `SPRINT_REOPEN` - Sprint reopened (CLOSED → OPEN)
- `SPRINT_UPDATE` - Sprint description updated
- `SPRINT_ADD_TASK` - Task added to sprint
- `SPRINT_REMOVE_TASK` - Task removed from sprint
- `SPRINT_MOVE_TASK` - Task moved between sprints

**Note:** Read operations (GET, STATS, LIST_TASKS) are NOT logged to audit as they do not modify state.

**Entities:**
- `entity_type`: TASK, SPRINT

### `_metadata` Table

Stores roadmap metadata and schema version.

```sql
CREATE TABLE IF NOT EXISTS _metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Insert schema version on creation
INSERT INTO _metadata (key, value) VALUES
    ('schema_version', '1.0.0'),
    ('created_at', '2026-03-12T14:30:00.000Z'),
    ('application', 'Groadmap');
```

---

## Main SQL Queries

### Tasks

#### Insert Task

```sql
INSERT INTO tasks (priority, severity, description, specialists, action, expected_result, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?);
```

#### List All

```sql
SELECT * FROM tasks ORDER BY priority DESC, created_at ASC;
```

#### List by Status

```sql
SELECT * FROM tasks WHERE status = ? ORDER BY priority DESC;
```

#### List by Sprint

```sql
SELECT t.* FROM tasks t
INNER JOIN sprint_tasks st ON t.id = st.task_id
WHERE st.sprint_id = ? ORDER BY t.priority DESC;
```

#### List Sprint Tasks Ordered (Priority → Severity)

Returns all tasks in a sprint ordered by priority (descending) and severity (descending).

```sql
SELECT t.* FROM tasks t
INNER JOIN sprint_tasks st ON t.id = st.task_id
WHERE st.sprint_id = ?
ORDER BY t.priority DESC, t.severity DESC;
```

**Ordering priority:**
1. `priority` DESC (highest first: 9 → 0)
2. `severity` DESC (highest first: 9 → 0)

**Use case:** Sprint planning and execution view - tasks with highest urgency AND technical impact appear first.

#### Update Status

```sql
UPDATE tasks
SET status = ?, completed_at = CASE WHEN ? = 'COMPLETED' THEN ? ELSE completed_at END
WHERE id IN (?, ?, ...);
```

#### Update Priority

```sql
UPDATE tasks SET priority = ? WHERE id IN (?, ?, ...);
```

#### Associate to Sprint

```sql
-- Insert into junction table
INSERT INTO sprint_tasks (sprint_id, task_id, added_at) VALUES (?, ?, ?);

-- Update task status
UPDATE tasks SET status = 'SPRINT' WHERE id IN (?, ?, ...);
```

#### Remove from Sprint

```sql
-- Remove from junction table
DELETE FROM sprint_tasks WHERE task_id IN (?, ?, ...);

-- Update task status
UPDATE tasks SET status = 'BACKLOG' WHERE id IN (?, ?, ...);
```

#### Clear All Tasks from Sprint

```sql
-- Remove all sprint relationships
DELETE FROM sprint_tasks WHERE sprint_id = ?;

-- Update task status
UPDATE tasks SET status = 'BACKLOG' WHERE id IN (
    SELECT task_id FROM sprint_tasks WHERE sprint_id = ?
);
```

#### Delete Task

```sql
DELETE FROM tasks WHERE id = ?;
```

### Sprints

#### Create Sprint

```sql
INSERT INTO sprints (description, created_at) VALUES (?, ?);
```

#### Add Tasks to Sprint

```sql
-- Insert into junction table
INSERT INTO sprint_tasks (sprint_id, task_id, added_at) VALUES (?, ?, ?);

-- Update associated tasks
UPDATE tasks SET status = 'SPRINT' WHERE id IN (?, ?, ...);
```

#### Start Sprint

```sql
UPDATE sprints SET status = 'OPEN', started_at = ? WHERE id = ?;
```

#### Close Sprint

```sql
UPDATE sprints SET status = 'CLOSED', closed_at = ? WHERE id = ?;
```

#### Delete Sprint

```sql
-- Tasks are automatically disassociated via ON DELETE CASCADE
-- in sprint_tasks table

-- Remove sprint (and relationships in sprint_tasks)
DELETE FROM sprints WHERE id = ?;

-- Optional: reset task status to BACKLOG
-- Note: in implementation, do this before deleting sprint
UPDATE tasks SET status = 'BACKLOG' WHERE id IN (
    SELECT task_id FROM sprint_tasks WHERE sprint_id = ?
);

-- Then remove relationships
DELETE FROM sprint_tasks WHERE sprint_id = ?;

-- Finally remove sprint
DELETE FROM sprints WHERE id = ?;
```

### Audit

#### Log Operation

```sql
INSERT INTO audit (operation, entity_type, entity_id, performed_at)
VALUES (?, ?, ?, ?);
```

**Examples by operation:**

```sql
-- Create task
INSERT INTO audit (operation, entity_type, entity_id, performed_at)
VALUES ('TASK_CREATE', 'TASK', 42, '2026-03-12T15:00:00.000Z');

-- Change task status
INSERT INTO audit (operation, entity_type, entity_id, performed_at)
VALUES ('TASK_STATUS_CHANGE', 'TASK', 42, '2026-03-12T15:30:00.000Z');

-- Change task priority
INSERT INTO audit (operation, entity_type, entity_id, performed_at)
VALUES ('TASK_PRIORITY_CHANGE', 'TASK', 42, '2026-03-12T15:45:00.000Z');

-- Change task severity
INSERT INTO audit (operation, entity_type, entity_id, performed_at)
VALUES ('TASK_SEVERITY_CHANGE', 'TASK', 42, '2026-03-12T16:00:00.000Z');

-- Start sprint
INSERT INTO audit (operation, entity_type, entity_id, performed_at)
VALUES ('SPRINT_START', 'SPRINT', 1, '2026-03-12T16:00:00.000Z');

-- Add task to sprint
INSERT INTO audit (operation, entity_type, entity_id, performed_at)
VALUES ('SPRINT_ADD_TASK', 'SPRINT', 1, '2026-03-12T16:30:00.000Z');

-- Remove task from sprint
INSERT INTO audit (operation, entity_type, entity_id, performed_at)
VALUES ('SPRINT_REMOVE_TASK', 'SPRINT', 1, '2026-03-12T16:45:00.000Z');
```

#### Query Entity History

```sql
-- Complete history of a task
SELECT * FROM audit
WHERE entity_type = 'TASK' AND entity_id = ?
ORDER BY performed_at DESC;

-- Complete history of a sprint
SELECT * FROM audit
WHERE entity_type = 'SPRINT' AND entity_id = ?
ORDER BY performed_at DESC;

-- All status change operations
SELECT * FROM audit
WHERE operation LIKE '%STATUS_CHANGE%'
ORDER BY performed_at DESC;

-- Last N operations
SELECT * FROM audit
ORDER BY performed_at DESC
LIMIT ?;
```

#### Query Audit Log with Filters

```sql
-- List all audit entries (most recent first)
SELECT * FROM audit
ORDER BY performed_at DESC
LIMIT ? OFFSET ?;

-- Filter by operation type
SELECT * FROM audit
WHERE operation = ?
ORDER BY performed_at DESC
LIMIT ?;

-- Filter by entity type
SELECT * FROM audit
WHERE entity_type = ?
ORDER BY performed_at DESC
LIMIT ?;

-- Filter by entity ID
SELECT * FROM audit
WHERE entity_type = ? AND entity_id = ?
ORDER BY performed_at DESC
LIMIT ?;

-- Filter by date range (ISO 8601 UTC)
SELECT * FROM audit
WHERE performed_at >= ? AND performed_at <= ?
ORDER BY performed_at DESC
LIMIT ?;

-- Combined filters
SELECT * FROM audit
WHERE entity_type = ?
  AND operation = ?
  AND performed_at >= ?
  AND performed_at <= ?
ORDER BY performed_at DESC
LIMIT ? OFFSET ?;
```

#### Audit Statistics

```sql
-- Total entries count
SELECT COUNT(*) as total_entries FROM audit;

-- Count by operation type
SELECT operation, COUNT(*) as count
FROM audit
GROUP BY operation
ORDER BY count DESC;

-- Count by entity type
SELECT entity_type, COUNT(*) as count
FROM audit
GROUP BY entity_type;

-- Statistics for specific period
SELECT
    COUNT(*) as total_entries,
    COUNT(CASE WHEN entity_type = 'TASK' THEN 1 END) as task_entries,
    COUNT(CASE WHEN entity_type = 'SPRINT' THEN 1 END) as sprint_entries,
    MIN(performed_at) as first_entry,
    MAX(performed_at) as last_entry
FROM audit
WHERE performed_at >= ? AND performed_at <= ?;

-- Count by operation for specific period
SELECT operation, COUNT(*) as count
FROM audit
WHERE performed_at >= ? AND performed_at <= ?
GROUP BY operation
ORDER BY count DESC;
```

#### Clear Audit (Maintenance)

```sql
-- Remove old records (e.g., > 90 days)
DELETE FROM audit WHERE performed_at < ?;
```

---

## Audit Queries

### List All Audit Entries

```sql
SELECT * FROM audit
ORDER BY performed_at DESC
LIMIT ? OFFSET ?;
```

### Filter by Operation Type

```sql
SELECT * FROM audit
WHERE operation = ?
ORDER BY performed_at DESC
LIMIT ?;
```

### Filter by Entity Type

```sql
SELECT * FROM audit
WHERE entity_type = ?
ORDER BY performed_at DESC
LIMIT ?;
```

### Filter by Entity ID

```sql
-- Complete history of a specific entity
SELECT * FROM audit
WHERE entity_type = ? AND entity_id = ?
ORDER BY performed_at DESC;

-- Or using separate columns
SELECT * FROM audit
WHERE entity_type = 'TASK' AND entity_id = 42
ORDER BY performed_at DESC;
```

### Filter by Date Range

```sql
-- Since a specific date
SELECT * FROM audit
WHERE performed_at >= ?
ORDER BY performed_at DESC
LIMIT ?;

-- Until a specific date
SELECT * FROM audit
WHERE performed_at <= ?
ORDER BY performed_at DESC
LIMIT ?;

-- Date range (between two dates)
SELECT * FROM audit
WHERE performed_at >= ? AND performed_at <= ?
ORDER BY performed_at DESC
LIMIT ?;
```

### Combined Filters

```sql
-- By operation and entity type
SELECT * FROM audit
WHERE operation = ? AND entity_type = ?
ORDER BY performed_at DESC
LIMIT ?;

-- By entity type, operation, and date range
SELECT * FROM audit
WHERE entity_type = ?
  AND operation = ?
  AND performed_at >= ?
  AND performed_at <= ?
ORDER BY performed_at DESC
LIMIT ? OFFSET ?;
```

### Get Total Count (for pagination)

```sql
-- Total entries with optional filters
SELECT COUNT(*) FROM audit;

-- With filters
SELECT COUNT(*) FROM audit
WHERE entity_type = ? AND operation = ?;
```

### Audit Statistics

```sql
-- Total entries in period
SELECT COUNT(*) FROM audit
WHERE performed_at >= ? AND performed_at <= ?;

-- By operation type
SELECT operation, COUNT(*) as count
FROM audit
WHERE performed_at >= ? AND performed_at <= ?
GROUP BY operation
ORDER BY count DESC;

-- By entity type
SELECT entity_type, COUNT(*) as count
FROM audit
WHERE performed_at >= ? AND performed_at <= ?
GROUP BY entity_type
ORDER BY count DESC;

-- First and last entry dates
SELECT MIN(performed_at) as first_entry, MAX(performed_at) as last_entry
FROM audit;

-- Combined statistics (all in one query for application processing)
SELECT
  operation,
  entity_type,
  COUNT(*) as count
FROM audit
WHERE performed_at >= ? AND performed_at <= ?
GROUP BY operation, entity_type
ORDER BY count DESC;
```

---

## Relationships

```
+-------------+           +-----------------+           +-------------+
|   sprints   |           |  sprint_tasks   |           |    tasks    |
|     id      | 1      N  |  sprint_id (FK) | N      1  |     id      |
|   (PK)      |-----------|  task_id (FK)   |-----------|   (PK)      |
|             |           |  (Composite PK) |           |             |
+-------------+           +-----------------+           +-------------+
```

**Integrity rules:**
- A task may not be in any sprint (no record in `sprint_tasks`)
- A task can only be in one sprint at a time (composite PK constraint)
- When deleting sprint, relationships in `sprint_tasks` are removed (`ON DELETE CASCADE`)
- Tasks are never automatically deleted, only disassociated

---

## Data Constraints

### Tasks

| Column | Type | Constraints |
|--------|------|-------------|
| id | INTEGER | PK, AUTOINCREMENT |
| priority | INTEGER | NOT NULL, DEFAULT 0, CHECK 0-9 |
| severity | INTEGER | NOT NULL, DEFAULT 0, CHECK 0-9 |
| status | TEXT | NOT NULL, DEFAULT 'BACKLOG', CHECK enum values |
| description | TEXT | NOT NULL |
| action | TEXT | NOT NULL |
| expected_result | TEXT | NOT NULL |
| created_at | TEXT | NOT NULL, ISO 8601 format |
| completed_at | TEXT | NULLABLE, ISO 8601 format |

### Sprints

| Column | Type | Constraints |
|--------|------|-------------|
| id | INTEGER | PK, AUTOINCREMENT |
| status | TEXT | NOT NULL, DEFAULT 'PENDING', CHECK enum values |
| description | TEXT | NOT NULL |
| created_at | TEXT | NOT NULL, ISO 8601 format |
| started_at | TEXT | NULLABLE, ISO 8601 format |
| closed_at | TEXT | NULLABLE, ISO 8601 format |

### Sprint_Tasks

| Column | Type | Constraints |
|--------|------|-------------|
| sprint_id | INTEGER | NOT NULL, FK → sprints.id, ON DELETE CASCADE, part of PK |
| task_id | INTEGER | NOT NULL, FK → tasks.id, ON DELETE CASCADE, part of PK |
| added_at | TEXT | NOT NULL, ISO 8601 format |

**Note:** Composite primary key `(sprint_id, task_id)`. A task can only be in one sprint at a time.

### Audit

| Column | Type | Constraints |
|--------|------|-------------|
| id | INTEGER | PK, AUTOINCREMENT |
| operation | TEXT | NOT NULL |
| entity_type | TEXT | NOT NULL |
| entity_id | INTEGER | NOT NULL |
| performed_at | TEXT | NOT NULL, ISO 8601 format |

**Valid values (validated by application):**
- `operation`: TASK_CREATE, TASK_UPDATE, TASK_DELETE, TASK_STATUS_CHANGE, TASK_PRIORITY_CHANGE, TASK_SEVERITY_CHANGE, SPRINT_CREATE, SPRINT_UPDATE, SPRINT_DELETE, SPRINT_START, SPRINT_CLOSE, SPRINT_REOPEN, SPRINT_ADD_TASK, SPRINT_REMOVE_TASK, SPRINT_MOVE_TASK
- `entity_type`: TASK, SPRINT

---

## SQLite Validation

To verify if a file is valid SQLite:

```sql
-- Validation query
SELECT name FROM sqlite_master WHERE type='table' AND name='_metadata';
```

Or check magic bytes: SQLite files start with `"SQLite format 3\x00"`

---

## Migrations

The `_metadata` table enables future schema versioning:

```sql
-- Check current version
SELECT value FROM _metadata WHERE key = 'schema_version';

-- Update version after migration
UPDATE _metadata SET value = '1.1.0' WHERE key = 'schema_version';
```
