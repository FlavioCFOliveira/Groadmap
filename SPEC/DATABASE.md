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
|  - title (TEXT)                        |
|  - status (TEXT)                       |
|  - type (TEXT)                         |
|  - functional_requirements (TEXT)      |
|  - technical_requirements (TEXT)       |
|  - acceptance_criteria (TEXT)          |
|  - created_at (TEXT ISO8601)           |
|  - specialists (TEXT, NULL)            |
|  - started_at (TEXT ISO8601, NULL)     |
|  - tested_at (TEXT ISO8601, NULL)      |
|  - closed_at (TEXT ISO8601, NULL)      |
|  - priority (INTEGER 0-9)              |
|  - severity (INTEGER 0-9)              |
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
    -- Primary key
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- Group 1: Content fields (TEXT) - frequently accessed together
    -- Length constraints enforced by application (255 chars for title, 4096 for requirements/criteria)
    title TEXT NOT NULL CHECK(length(title) <= 255),                    -- Task title/summary, max 255 chars
    status TEXT NOT NULL DEFAULT 'BACKLOG' CHECK(status IN ('BACKLOG', 'SPRINT', 'DOING', 'TESTING', 'COMPLETED')),
    type TEXT NOT NULL DEFAULT 'TASK' CHECK(type IN ('USER_STORY', 'TASK', 'BUG', 'SUB_TASK', 'EPIC', 'REFACTOR', 'CHORE', 'SPIKE', 'DESIGN_UX', 'IMPROVEMENT')),
    functional_requirements TEXT NOT NULL CHECK(length(functional_requirements) <= 4096),    -- Why: functional requirements, max 4096 chars
    technical_requirements TEXT NOT NULL CHECK(length(technical_requirements) <= 4096),   -- How: technical description, max 4096 chars
    acceptance_criteria TEXT NOT NULL CHECK(length(acceptance_criteria) <= 4096),      -- How to verify: completion criteria, max 4096 chars
    created_at TEXT NOT NULL,               -- ISO 8601 UTC, set on task creation

    -- Group 2: Nullable tracking fields - lifecycle timestamps
    specialists TEXT,                       -- Comma-separated specialists (nullable)
    started_at TEXT,                        -- ISO 8601 UTC, set when task moves to DOING
    tested_at TEXT,                         -- ISO 8601 UTC, set when task moves to TESTING
    closed_at TEXT,                         -- ISO 8601 UTC, set when task moves to COMPLETED

    -- Group 3: Numeric metadata fields
    priority INTEGER NOT NULL DEFAULT 0 CHECK(priority >= 0 AND priority <= 9),
    severity INTEGER NOT NULL DEFAULT 0 CHECK(severity >= 0 AND severity <= 9)
);

-- Indexes for frequent queries
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_type ON tasks(type);
CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority);
CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);

-- Composite indexes for multi-criteria queries (TASK-P001)
-- Covers: ListTasks with status filter + priority ordering
CREATE INDEX IF NOT EXISTS idx_tasks_status_priority ON tasks(status, priority DESC);
-- Covers: Priority filtering with date ordering (matches ListTasks ORDER BY)
CREATE INDEX IF NOT EXISTS idx_tasks_priority_created ON tasks(priority DESC, created_at ASC);
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

-- Composite index for sprint task lookups (TASK-P001)
-- Covers: GetSprintTasks and sprint-task relationship queries
CREATE INDEX IF NOT EXISTS idx_sprint_tasks_lookup ON sprint_tasks(sprint_id, task_id);
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

-- Composite index for audit date range queries (TASK-P001)
-- Covers: GetAuditEntries with date range filters
CREATE INDEX IF NOT EXISTS idx_audit_date ON audit(performed_at DESC);
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
- `TASK_TYPE_CHANGE` - Type change (USER_STORY, TASK, BUG, SUB_TASK, EPIC, REFACTOR, CHORE, SPIKE, DESIGN_UX, IMPROVEMENT)
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
    ('schema_version', '1.1.0'),
    ('created_at', '2026-03-12T14:30:00.000Z'),
    ('application', 'Groadmap');
```

---

## Main SQL Queries

### Tasks

#### Insert Task

```sql
INSERT INTO tasks (title, status, type, functional_requirements, technical_requirements, acceptance_criteria, created_at, specialists, priority, severity)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);  -- created_at set by application (ISO 8601 UTC)
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

Date tracking fields are automatically managed by the application based on state transitions:

```sql
-- When transitioning to DOING: set started_at
UPDATE tasks
SET status = 'DOING', started_at = ?
WHERE id = ?;

-- When transitioning to TESTING: set tested_at
UPDATE tasks
SET status = 'TESTING', tested_at = ?
WHERE id = ?;

-- When transitioning to COMPLETED: set closed_at
UPDATE tasks
SET status = 'COMPLETED', closed_at = ?
WHERE id = ?;

-- When reopening (COMPLETED → BACKLOG): clear tracking dates
UPDATE tasks
SET status = 'BACKLOG', started_at = NULL, tested_at = NULL, closed_at = NULL
WHERE id = ?;

-- Generic status update without date tracking changes
UPDATE tasks
SET status = ?
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

| Column | Type | Constraints | Group |
|--------|------|-------------|-------|
| id | INTEGER | PK, AUTOINCREMENT | Key |
| title | TEXT | NOT NULL, CHECK length <= 255 chars, task title/summary | Content |
| status | TEXT | NOT NULL, DEFAULT 'BACKLOG', CHECK enum values | Content |
| type | TEXT | NOT NULL, DEFAULT 'TASK', CHECK enum values | Content |
| functional_requirements | TEXT | NOT NULL, CHECK length <= 4096 chars, answers "Why?" | Content |
| technical_requirements | TEXT | NOT NULL, CHECK length <= 4096 chars, answers "How?" | Content |
| acceptance_criteria | TEXT | NOT NULL, CHECK length <= 4096 chars, answers "How to verify?" | Content |
| created_at | TEXT | NOT NULL, ISO 8601 format | Content |
| specialists | TEXT | NULLABLE, comma-separated | Tracking |
| started_at | TEXT | NULLABLE, ISO 8601 format | Tracking |
| tested_at | TEXT | NULLABLE, ISO 8601 format | Tracking |
| closed_at | TEXT | NULLABLE, ISO 8601 format | Tracking |
| priority | INTEGER | NOT NULL, DEFAULT 0, CHECK 0-9 | Metadata |
| severity | INTEGER | NOT NULL, DEFAULT 0, CHECK 0-9 | Metadata |

**Field Grouping Rationale:**

Fields are organized to match the optimized Go struct layout:
- **Content fields**: Frequently accessed together for display purposes (96 bytes)
- **Tracking fields**: Nullable lifecycle timestamps, often queried together (32 bytes)
- **Metadata fields**: Small integers used for filtering and sorting (24 bytes)

**Memory Layout Optimization:**

On 64-bit systems, the Task struct occupies exactly 168 bytes with zero padding:
- String fields: 16 bytes each (ptr + len), 8-byte aligned
- Pointer fields: 8 bytes each, 8-byte aligned
- Integer fields: 8 bytes each, 8-byte aligned

Total: 168 bytes (7×16 + 4×8 + 3×8 = 112 + 32 + 24)

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
- `operation`: TASK_CREATE, TASK_UPDATE, TASK_DELETE, TASK_STATUS_CHANGE, TASK_TYPE_CHANGE, TASK_PRIORITY_CHANGE, TASK_SEVERITY_CHANGE, SPRINT_CREATE, SPRINT_UPDATE, SPRINT_DELETE, SPRINT_START, SPRINT_CLOSE, SPRINT_REOPEN, SPRINT_ADD_TASK, SPRINT_REMOVE_TASK, SPRINT_MOVE_TASK
- `entity_type`: TASK, SPRINT

---

## Performance Optimization

### Composite Indexes

The following composite indexes are designed to optimize frequently executed query patterns identified during performance analysis (TASK-P001):

| Index Name | Table | Columns | Purpose |
|------------|-------|---------|---------|
| `idx_tasks_status_priority` | tasks | (status, priority DESC) | Optimizes ListTasks with status filter and priority ordering |
| `idx_tasks_priority_created` | tasks | (priority DESC, created_at) | Optimizes priority filtering with date-based ordering |
| `idx_sprint_tasks_lookup` | sprint_tasks | (sprint_id, task_id) | Optimizes sprint task relationship lookups |
| `idx_audit_date` | audit | (performed_at DESC) | Optimizes audit log date range queries |

### Index Design Rationale

**idx_tasks_status_priority:**
- Query pattern: `WHERE status = ? ORDER BY priority DESC`
- Without index: Full table scan + sort operation
- With index: Index scan only, no sort needed
- Expected improvement: 90% query time reduction for filtered listings

**idx_tasks_priority_created:**
- Query pattern: `WHERE priority >= ? ORDER BY created_at`
- Supports priority-based filtering with chronological ordering
- Expected improvement: 80% query time reduction for priority filters

**idx_sprint_tasks_lookup:**
- Query pattern: `WHERE sprint_id = ?` in sprint_tasks table
- Optimizes GetSprintTasks and sprint membership checks
- Expected improvement: 70% query time reduction for sprint operations

**idx_audit_date:**
- Query pattern: `WHERE performed_at >= ? AND performed_at <= ?`
- Essential for audit log pagination and date range filtering
- Expected improvement: 85% query time reduction for date range queries

### Verification

To verify index usage:

```sql
-- Check if query uses index
EXPLAIN QUERY PLAN SELECT * FROM tasks WHERE status = 'BACKLOG' ORDER BY priority DESC;
-- Expected: USING INDEX idx_tasks_status_priority
```

---

## Field Length Validation

The following length constraints are enforced at the database level using CHECK constraints:

| Field | Maximum Length | Constraint |
|-------|----------------|------------|
| `title` | 255 characters | `CHECK(length(title) <= 255)` |
| `functional_requirements` | 4096 characters | `CHECK(length(functional_requirements) <= 4096)` |
| `technical_requirements` | 4096 characters | `CHECK(length(technical_requirements) <= 4096)` |
| `acceptance_criteria` | 4096 characters | `CHECK(length(acceptance_criteria) <= 4096)` |

**Application-Level Validation:**
- Validate inputs BEFORE database insertion to provide clear error messages
- Trim whitespace before length checking
- Return specific error messages indicating which field exceeded the limit

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

### Schema Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0.0 | 2026-03-12 | Initial schema |
| 1.1.0 | 2026-03-19 | Added CHECK constraints for field length validation (title: 255 chars, requirements/criteria: 4096 chars) |
| 1.2.0 | 2026-03-19 | Added `type` column to tasks table with enum constraint for task classification |

### Migration Commands

```sql
-- Check current version
SELECT value FROM _metadata WHERE key = 'schema_version';

-- Update version after migration
UPDATE _metadata SET value = '1.1.0' WHERE key = 'schema_version';
```

### Migrating from 1.0.0 to 1.1.0

To add length constraints to an existing database:

```sql
-- Note: SQLite has limited ALTER TABLE support.
-- For existing databases, recreate the table with constraints:

BEGIN TRANSACTION;

-- Create new table with constraints
CREATE TABLE tasks_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL CHECK(length(title) <= 255),
    status TEXT NOT NULL DEFAULT 'BACKLOG' CHECK(status IN ('BACKLOG', 'SPRINT', 'DOING', 'TESTING', 'COMPLETED')),
    functional_requirements TEXT NOT NULL CHECK(length(functional_requirements) <= 4096),
    technical_requirements TEXT NOT NULL CHECK(length(technical_requirements) <= 4096),
    acceptance_criteria TEXT NOT NULL CHECK(length(acceptance_criteria) <= 4096),
    created_at TEXT NOT NULL,
    specialists TEXT,
    started_at TEXT,
    tested_at TEXT,
    closed_at TEXT,
    priority INTEGER NOT NULL DEFAULT 0 CHECK(priority >= 0 AND priority <= 9),
    severity INTEGER NOT NULL DEFAULT 0 CHECK(severity >= 0 AND severity <= 9)
);

-- Copy data
INSERT INTO tasks_new SELECT * FROM tasks;

-- Replace tables
DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;

-- Recreate indexes
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_type ON tasks(type);
CREATE INDEX idx_tasks_priority ON tasks(priority);
CREATE INDEX idx_tasks_created_at ON tasks(created_at);
CREATE INDEX idx_tasks_status_priority ON tasks(status, priority DESC);
CREATE INDEX idx_tasks_priority_created ON tasks(priority DESC, created_at ASC);

-- Update version
UPDATE _metadata SET value = '1.1.0' WHERE key = 'schema_version';

COMMIT;
```

### Migrating from 1.1.0 to 1.2.0

To add the `type` column to an existing database:

```sql
-- Note: SQLite has limited ALTER TABLE support.
-- For existing databases, recreate the table with the new column:

BEGIN TRANSACTION;

-- Create new table with type column
CREATE TABLE tasks_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL CHECK(length(title) <= 255),
    status TEXT NOT NULL DEFAULT 'BACKLOG' CHECK(status IN ('BACKLOG', 'SPRINT', 'DOING', 'TESTING', 'COMPLETED')),
    type TEXT NOT NULL DEFAULT 'TASK' CHECK(type IN ('USER_STORY', 'TASK', 'BUG', 'SUB_TASK', 'EPIC', 'REFACTOR', 'CHORE', 'SPIKE', 'DESIGN_UX', 'IMPROVEMENT')),
    functional_requirements TEXT NOT NULL CHECK(length(functional_requirements) <= 4096),
    technical_requirements TEXT NOT NULL CHECK(length(technical_requirements) <= 4096),
    acceptance_criteria TEXT NOT NULL CHECK(length(acceptance_criteria) <= 4096),
    created_at TEXT NOT NULL,
    specialists TEXT,
    started_at TEXT,
    tested_at TEXT,
    closed_at TEXT,
    priority INTEGER NOT NULL DEFAULT 0 CHECK(priority >= 0 AND priority <= 9),
    severity INTEGER NOT NULL DEFAULT 0 CHECK(severity >= 0 AND severity <= 9)
);

-- Copy data (set default type 'TASK' for existing tasks)
INSERT INTO tasks_new (
    id, title, status, type, functional_requirements, technical_requirements,
    acceptance_criteria, created_at, specialists, started_at, tested_at,
    closed_at, priority, severity
)
SELECT
    id, title, status, 'TASK', functional_requirements, technical_requirements,
    acceptance_criteria, created_at, specialists, started_at, tested_at,
    closed_at, priority, severity
FROM tasks;

-- Replace tables
DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;

-- Recreate indexes
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_type ON tasks(type);
CREATE INDEX idx_tasks_priority ON tasks(priority);
CREATE INDEX idx_tasks_created_at ON tasks(created_at);
CREATE INDEX idx_tasks_status_priority ON tasks(status, priority DESC);
CREATE INDEX idx_tasks_priority_created ON tasks(priority DESC, created_at ASC);

-- Update version
UPDATE _metadata SET value = '1.2.0' WHERE key = 'schema_version';

COMMIT;
```
