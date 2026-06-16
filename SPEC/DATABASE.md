# Database Schema

## Table of Contents

- [Overview](#overview)
- [Naming Conventions](#naming-conventions)
- [SQLite File Structure](#sqlite-file-structure)
- [DDL - Table Creation](#ddl---table-creation)
  - [`tasks` Table](#tasks-table)
  - [`sprints` Table](#sprints-table)
  - [`sprint_tasks` Table (1:N Relationship)](#sprint_tasks-table-1n-relationship)
  - [`audit` Table](#audit-table)
  - [`task_dependencies` Table](#task_dependencies-table)
  - [`_metadata` Table](#_metadata-table)
- [Main SQL Queries](#main-sql-queries)
  - [Tasks](#tasks)
  - [Sprints](#sprints)
  - [Audit](#audit)
- [Relationships](#relationships)
  - [Transactional Atomicity Guarantees](#transactional-atomicity-guarantees)
- [Data Constraints](#data-constraints)
- [Performance Optimization](#performance-optimization)
- [Field Length Validation](#field-length-validation)
- [SQLite Validation](#sqlite-validation)
- [Migration Idempotency (ALTER TABLE ADD COLUMN)](#migration-idempotency-alter-table-add-column)
- [Audit Result Limit](#audit-result-limit)
- [See Also](#see-also)

## Overview

Each roadmap is stored in an individual SQLite database. The schema is designed to be simple, efficient, and normalized.

### Physical Location and Naming

- Each roadmap has its own home directory at `~/.roadmaps/<name>/`, where `<name>` is the roadmap name. The home directory uses `0700` permissions, owner-only.
- The SQLite database is named `project.db` and lives inside that directory at `~/.roadmaps/<name>/project.db` with `0600` permissions. The `project.db` file MUST be created with `0600` permissions from the outset, not created with the process umask and chmod-ed afterwards: there must be no window in which the file is more permissive than `0600`.
- SQLite sidecars (`project.db-wal`, `project.db-shm`) live alongside the database in the same directory and MUST also use `0600` permissions, identical to `project.db`. Because the sidecars can contain the same data pages as the main database, they are held to the same owner-only permission as `project.db`.
- The data directory layout, its permission model, and the automatic migration from the legacy `~/.roadmaps/<name>.db` layout are specified in `ARCHITECTURE.md § Directory Structure` and `ARCHITECTURE.md § Filesystem Layout Migration`.

## Naming Conventions

- **Tables**: snake_case, plural (`tasks`, `sprints`)
- **Columns**: snake_case (`created_at`, `acceptance_criteria`)
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
|  - completion_summary (TEXT, NULL)     |
|  - parent_task_id (INTEGER FK, NULL)   |
|  - priority (INTEGER 0-9)              |
|  - severity (INTEGER 0-9)              |
+----------------------------------------+
|           sprints                      |
|  - id (PK, AUTOINCREMENT)              |
|  - status (TEXT)                       |
|  - title (TEXT)                        |
|  - description (TEXT)                  |
|  - created_at (TEXT ISO8601)           |
|  - started_at (TEXT ISO8601, NULL)     |
|  - closed_at (TEXT ISO8601, NULL)      |
|  - max_tasks (INTEGER, NULL)           |
|  - order_index (INTEGER, UNIQUE, >0)   |
+----------------------------------------+
|           sprint_tasks                 |
|  - sprint_id (FK → sprints.id)         |
|  - task_id (FK → tasks.id)             |
|  - added_at (TEXT ISO8601)             |
|  - position (INTEGER)                  |
|  - Composite PK (sprint_id, task_id)   |
+----------------------------------------+
|           audit                        |
|  - id (PK, AUTOINCREMENT)              |
|  - operation (TEXT)                    |
|  - entity_type (TEXT)                  |
|  - entity_id (INTEGER)                 |
|  - performed_at (TEXT ISO8601)         |
+----------------------------------------+
|           task_dependencies            |
|  - task_id (FK → tasks.id)            |
|  - depends_on_task_id (FK → tasks.id) |
|  - Composite PK (task_id, dep_id)     |
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
    completion_summary TEXT CHECK(completion_summary IS NULL OR length(completion_summary) <= 4096),  -- Optional summary of work done, set only on TESTING → COMPLETED
    parent_task_id INTEGER REFERENCES tasks(id),  -- NULL for top-level tasks; non-NULL links to parent task (sub-task hierarchy)

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
-- Covers: sub-task hierarchy lookups (GetSubTasks)
CREATE INDEX IF NOT EXISTS idx_tasks_parent_task_id ON tasks(parent_task_id);
```

### `sprints` Table

```sql
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
CREATE INDEX IF NOT EXISTS idx_sprints_created_at ON sprints(created_at);

-- Uniqueness of the sprint execution order across the roadmap.
-- Enforces that no two sprints share the same order_index value; an attempt to
-- insert or update a colliding value fails the constraint and is surfaced to the
-- caller as exit code 5 (see ARCHITECTURE.md § Exit Codes, ErrAlreadyExists).
CREATE UNIQUE INDEX IF NOT EXISTS idx_sprints_order ON sprints(order_index);
```

### `sprint_tasks` Table (1:N Relationship)

Junction table linking sprints to their tasks. The relationship is one-sprint-to-many-tasks: a sprint contains many tasks, but each task belongs to at most one sprint at any given time. This 1:N constraint is enforced at the schema level by the `UNIQUE` constraint on `task_id`. The table also stores ordering information (`position`) for sprint task priority.

```sql
CREATE TABLE IF NOT EXISTS sprint_tasks (
    sprint_id INTEGER NOT NULL,
    task_id INTEGER NOT NULL UNIQUE,
    added_at TEXT NOT NULL,  -- ISO 8601 UTC
    position INTEGER NOT NULL DEFAULT 0,  -- 0-based position in sprint task order
    PRIMARY KEY (sprint_id, task_id),
    FOREIGN KEY (sprint_id) REFERENCES sprints(id) ON DELETE CASCADE,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_sprint_tasks_task_id ON sprint_tasks(task_id);

-- Composite index for sprint task lookups (TASK-P001)
-- Covers: GetSprintTasks and sprint-task relationship queries
CREATE INDEX IF NOT EXISTS idx_sprint_tasks_lookup ON sprint_tasks(sprint_id, task_id);

-- Index for sprint task ordering (TASK-ORDER-001)
-- Covers: Sprint task listing ordered by position
CREATE INDEX IF NOT EXISTS idx_sprint_tasks_order ON sprint_tasks(sprint_id, position ASC);
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

**Valid values (validated by application):** This section is the canonical catalogue of audit operations. All other SPEC files referencing audit operations MUST link here rather than re-listing.

**Tasks:**
- `TASK_CREATE` - New task created
- `TASK_DELETE` - Task deleted (only allowed while in BACKLOG; see Delete Task precondition)
- `TASK_STATUS_CHANGE` - Status change (BACKLOG ↔ DOING ↔ TESTING → COMPLETED, plus COMPLETED → BACKLOG; SPRINT transitions are logged as `SPRINT_ADD_TASK` / `SPRINT_REMOVE_TASK`)
- `TASK_PRIORITY_CHANGE` - Priority change (0-9) via `task priority`
- `TASK_SEVERITY_CHANGE` - Severity change (0-9) via `task severity`
- `TASK_UPDATE` - Generic update via `task edit` (title, type, functional_requirements, technical_requirements, acceptance_criteria, specialists). A type change made through `task edit` is recorded here, not under a dedicated operation.
- `TASK_REOPEN` - Task returned to BACKLOG via `task reopen`; lifecycle timestamps cleared and sprint_tasks row removed
- `TASK_ADD_DEP` - Dependency added (logged against both task_id and depends_on_task_id)
- `TASK_REMOVE_DEP` - Dependency removed (logged against both task_id and depends_on_task_id)

**Sprints:**
- `SPRINT_CREATE` - New sprint created
- `SPRINT_DELETE` - Sprint deleted
- `SPRINT_START` - Sprint started (PENDING → OPEN)
- `SPRINT_CLOSE` - Sprint closed (OPEN → CLOSED)
- `SPRINT_REOPEN` - Sprint reopened (CLOSED → OPEN)
- `SPRINT_UPDATE` - Sprint title, description, capacity, or execution order updated via `sprint update`
- `SPRINT_ADD_TASK` - Task added to sprint
- `SPRINT_REMOVE_TASK` - Task removed from sprint
- `SPRINT_MOVE_TASK` - Task moved between sprints
- `SPRINT_REORDER_TASKS` - Sprint tasks reordered (set exact order)
- `SPRINT_TASK_MOVE_POSITION` - Single task moved to specific position
- `SPRINT_TASK_SWAP` - Two tasks swapped positions

**Note:** Read operations (GET, STATS, LIST_TASKS) are NOT logged to audit as they do not modify state.

**Entities:**
- `entity_type`: TASK, SPRINT

### `task_dependencies` Table

Junction table encoding blocking relationships between tasks.

```sql
CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id INTEGER NOT NULL,               -- The dependent task
    depends_on_task_id INTEGER NOT NULL,    -- The task it depends on (the blocker)
    PRIMARY KEY (task_id, depends_on_task_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    FOREIGN KEY (depends_on_task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_deps_task_id ON task_dependencies(task_id);
CREATE INDEX IF NOT EXISTS idx_task_deps_depends_on ON task_dependencies(depends_on_task_id);
```

**Semantics:** A row `(A, B)` means "task A depends on task B". Task A cannot be marked COMPLETED until task B is COMPLETED. Circular dependencies are rejected by the application using BFS traversal of existing dependencies.

---

### `_metadata` Table

Stores roadmap metadata and schema version.

```sql
CREATE TABLE IF NOT EXISTS _metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Insert schema version on creation
INSERT INTO _metadata (key, value) VALUES
    ('schema_version', '1.8.0'),
    ('created_at', '2026-03-20T00:00:00.000Z'),
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

#### List Sprint Tasks Ordered by Position

Returns all tasks in a sprint ordered by their position in the sprint task list.

```sql
SELECT t.*, st.position FROM tasks t
INNER JOIN sprint_tasks st ON t.id = st.task_id
WHERE st.sprint_id = ?
ORDER BY st.position ASC;
```

**Ordering priority:**
1. `position` ASC (lowest first: 0, 1, 2...)

**Use case:** Sprint task sequence view - tasks appear in the order defined by the user for sprint execution.

#### Add Task to Sprint with Position

```sql
-- Get max position for the sprint
SELECT COALESCE(MAX(position), -1) + 1 AS next_position
FROM sprint_tasks
WHERE sprint_id = ?;

-- Insert into junction table with calculated position
INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position)
VALUES (?, ?, ?, ?);

-- Update task status
UPDATE tasks SET status = 'SPRINT' WHERE id IN (?, ?, ...);
```

**Use case:** New tasks are added to the end of the sprint task list (highest position).

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

#### Get Max Position in Sprint

Returns the highest current position value for a sprint. Used when adding tasks.

```sql
SELECT COALESCE(MAX(position), -1) AS max_position
FROM sprint_tasks
WHERE sprint_id = ?;
```

**Note:** Returns -1 if sprint has no tasks, meaning first task gets position 0.

#### Reorder Sprint Tasks (Set Exact Order)

Updates positions for all tasks in a sprint based on a provided ordered list of task IDs.

```sql
-- Transaction: Update positions for each task
-- For each task ID in the ordered list at index i:
UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?;
```

**Validation:** All task IDs in the ordered list must belong to the sprint.

#### Move Task to Position

Moves a single task to a specific position, updating positions of other tasks accordingly.

```sql
-- Transaction:
-- 1. Get current position of the task
SELECT position FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?;

-- 2. If moving UP (new_position < current_position):
--    Shift tasks between new_position and current_position-1 down by 1
UPDATE sprint_tasks
SET position = position + 1
WHERE sprint_id = ?
  AND position >= ?
  AND position < ?;

-- 3. If moving DOWN (new_position > current_position):
--    Shift tasks between current_position+1 and new_position up by 1
UPDATE sprint_tasks
SET position = position - 1
WHERE sprint_id = ?
  AND position > ?
  AND position <= ?;

-- 4. Update the moved task to the new position
UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?;
```

**Validation:** The target position must be an integer between 0 and 2147483647 (MaxInt32) inclusive. A value less than 0 or greater than 2147483647 is rejected as a validation error.

**Behavior:**
- Moving to position 0 places the task at the beginning
- Moving to a position >= task count places the task at the end
- Positions of other tasks are automatically adjusted to maintain continuity

#### Swap Tasks

Swaps positions between two tasks in the same sprint.

```sql
-- Transaction:
-- 1. Get positions of both tasks
SELECT task_id, position FROM sprint_tasks WHERE sprint_id = ? AND task_id IN (?, ?);

-- 2. Swap positions
UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?;
UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?;
```

#### Move Task to Top/Bottom

```sql
-- Move to top (position 0)
-- Transaction: same logic as Move Task to Position with target position 0

-- Move to bottom (last position)
-- Get current max position, then use Move Task to Position logic
```

#### Delete Task

```sql
DELETE FROM tasks WHERE id = ?;
```

**Application-level precondition:** Before executing this statement, the application MUST verify that the task's `status` is `BACKLOG` and that the task has no subtasks (no other tasks with `parent_task_id = <id>`). Tasks in `SPRINT`, `DOING`, `TESTING`, or `COMPLETED` status are not deletable; the operation returns exit code 6. See `STATE_MACHINE.md` — Task Deletion Precondition. The SQLite DDL does not enforce this rule via `CHECK` or trigger.

### Sprints

#### Create Sprint

```sql
-- When the caller does not supply an explicit order, compute the next available
-- value as MAX(order_index) + 1 (the first sprint in an empty roadmap gets 1).
SELECT COALESCE(MAX(order_index), 0) + 1 AS next_order FROM sprints;

-- Insert the new sprint with its execution order.
INSERT INTO sprints (title, description, created_at, order_index) VALUES (?, ?, ?, ?);
```

**Order assignment:** When the caller supplies `--order`, that value is used and
validated for the `> 0` and uniqueness invariants; a colliding value fails the
`idx_sprints_order` unique index and is surfaced as exit code 5. When the caller
omits `--order`, the value `MAX(order_index) + 1` is used, which is always unique
and `> 0`. The `SELECT next_order` and the `INSERT` MUST run inside the same
transaction so that two concurrent creations cannot compute the same
`next_order` value; the unique index is the final backstop if they do.

#### Update Sprint Order

```sql
-- Allowed only while the sprint status is PENDING or OPEN. A colliding value
-- fails idx_sprints_order and is surfaced as exit code 5.
UPDATE sprints SET order_index = ? WHERE id = ?;
```

**Application-level precondition:** Before executing this statement, the
application MUST verify that the sprint's `status` is not `CLOSED`. A sprint in
`CLOSED` status has an immutable `order_index`; an attempt to change it is
rejected with exit code 6 (see `STATE_MACHINE.md § Sprint Order Immutability`).
The new value MUST be a positive integer (`> 0`); a value `<= 0` is rejected with
exit code 6 before the statement runs.

#### Add Tasks to Sprint

```sql
-- Get max position for the sprint
SELECT COALESCE(MAX(position), -1) AS max_position FROM sprint_tasks WHERE sprint_id = ?;

-- Insert into junction table with incremental positions
INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position) VALUES (?, ?, ?, ?);

-- Update task status
UPDATE tasks SET status = 'SPRINT' WHERE id IN (?, ?, ...);
```

**Note:** Tasks are added with positions starting from max_position + 1, ensuring they appear at the end of the sprint task list.

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

-- Reorder sprint tasks
INSERT INTO audit (operation, entity_type, entity_id, performed_at)
VALUES ('SPRINT_REORDER_TASKS', 'SPRINT', 1, '2026-03-12T17:00:00.000Z');

-- Move task to position
INSERT INTO audit (operation, entity_type, entity_id, performed_at)
VALUES ('SPRINT_TASK_MOVE_POSITION', 'SPRINT', 1, '2026-03-12T17:15:00.000Z');

-- Swap tasks
INSERT INTO audit (operation, entity_type, entity_id, performed_at)
VALUES ('SPRINT_TASK_SWAP', 'SPRINT', 1, '2026-03-12T17:30:00.000Z');
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

### Transactional Atomicity Guarantees

The following multi-statement operations MUST run inside a single SQL transaction
so that the database never reaches a state where `tasks.status` and the
`sprint_tasks` membership diverge:

1. **Sprint deletion (`DeleteSprint`).** Resetting the member tasks' status to
   `BACKLOG`, deleting the `sprint_tasks` rows, deleting the `sprints` row, and
   writing the `SPRINT_DELETE` audit entry MUST all occur in the same transaction.
   Either every step commits or none does. A partial commit that left tasks marked
   `SPRINT` while their sprint or their `sprint_tasks` rows were gone is forbidden.
2. **Removing tasks from a sprint (`RemoveTasksFromSprint`).** Deleting the
   affected `sprint_tasks` rows, resetting those tasks' status to `BACKLOG`, and
   writing the audit entry MUST occur in the same transaction. `tasks.status` and
   `sprint_tasks` membership are always consistent at every committed state.
3. **Sprint capacity enforcement (`max_tasks`).** When `max_tasks` is set, the
   capacity check (current member count against `max_tasks`) and the insertion of
   the new `sprint_tasks` rows MUST occur **inside the same transaction** as a
   single atomic operation. The check and the insert MUST NOT be separated by a
   time-of-check-to-time-of-use (TOCTOU) window in which a concurrent writer could
   add tasks between the count and the insert and thereby exceed the cap. The
   capacity is enforced atomically within the insert transaction, so the committed
   member count can never exceed `max_tasks`.
4. **Adding tasks to a sprint (`AddTasksToSprint`).** Inserting the
   `sprint_tasks` rows, updating those tasks' status to `SPRINT`, and writing
   the `SPRINT_ADD_TASK` audit entries (one per task) MUST all occur in the same
   transaction. A committed membership change can never exist without its audit
   record.
5. **Moving tasks between sprints (`MoveTasksBetweenSprints`).** The source-sprint
   membership check, the re-parenting of the `sprint_tasks` rows, and writing the
   `SPRINT_MOVE_TASK` audit entries (one per task) MUST all occur in the same
   transaction. A committed move can never exist without its audit record.
6. **Creating a sprint with an auto-assigned order (`CreateSprint`).** When the
   caller omits `--order`, computing `MAX(order_index) + 1`, inserting the
   `sprints` row with that value, and writing the `SPRINT_CREATE` audit entry MUST
   all occur in the same transaction, so two concurrent creations cannot read the
   same `MAX` and then both insert it. The `idx_sprints_order` unique index is the
   final backstop: if a race still produces a collision, the second insert fails
   the constraint and the whole transaction rolls back, surfaced as exit code 5.

These guarantees extend the general transactional-integrity requirement in
`ARCHITECTURE.md § Security Guarantees` (every modification, including its audit
entry, is wrapped in one transaction) to these specific sprint operations.

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

Fields are organized to match the optimized Go struct layout (Content, Tracking, Metadata groups). The byte-level layout, struct sizes, and cache-line considerations are documented in `MODELS.md § Memory Layout Optimization`.

### Sprints

| Column | Type | Constraints |
|--------|------|-------------|
| id | INTEGER | PK, AUTOINCREMENT |
| status | TEXT | NOT NULL, DEFAULT 'PENDING', CHECK enum values |
| title | TEXT | NOT NULL, CHECK length <= 255 chars, sprint title |
| description | TEXT | NOT NULL |
| created_at | TEXT | NOT NULL, ISO 8601 format |
| started_at | TEXT | NULLABLE, ISO 8601 format |
| closed_at | TEXT | NULLABLE, ISO 8601 format |
| max_tasks | INTEGER | NULLABLE, NULL means unlimited capacity |
| order_index | INTEGER | NOT NULL, CHECK(order_index > 0), UNIQUE across the roadmap via `idx_sprints_order`; sprint execution order. Column named `order_index` because `ORDER` is a reserved SQL keyword |

### Sprint_Tasks

| Column | Type | Constraints |
|--------|------|-------------|
| sprint_id | INTEGER | NOT NULL, FK → sprints.id, ON DELETE CASCADE, part of PK |
| task_id | INTEGER | NOT NULL, FK → tasks.id, ON DELETE CASCADE, part of PK |
| added_at | TEXT | NOT NULL, ISO 8601 format |
| position | INTEGER | NOT NULL, DEFAULT 0, position in sprint task order (0-based) |

**Note:** Composite primary key `(sprint_id, task_id)` combined with the `UNIQUE` constraint on `task_id` enforces the 1:N relationship — a task can only belong to one sprint at a time. The `position` field enables sprint task ordering, with 0 being the first position.

### Audit

| Column | Type | Constraints |
|--------|------|-------------|
| id | INTEGER | PK, AUTOINCREMENT |
| operation | TEXT | NOT NULL |
| entity_type | TEXT | NOT NULL |
| entity_id | INTEGER | NOT NULL |
| performed_at | TEXT | NOT NULL, ISO 8601 format |

**Valid values (validated by application):**
- `operation`: See the canonical catalogue in the `audit` Table section above (Tasks + Sprints).
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
| `tasks.title` | 255 characters | `CHECK(length(title) <= 255)` |
| `tasks.functional_requirements` | 4096 characters | `CHECK(length(functional_requirements) <= 4096)` |
| `tasks.technical_requirements` | 4096 characters | `CHECK(length(technical_requirements) <= 4096)` |
| `tasks.acceptance_criteria` | 4096 characters | `CHECK(length(acceptance_criteria) <= 4096)` |
| `sprints.title` | 255 characters | `CHECK(length(title) <= 255)` |

**Application-Level Validation Only:**

The following fields have a maximum length enforced at the application layer but **not** at the database level (the column has no CHECK constraint). The application MUST reject inputs that exceed the limit before insert/update.

| Table.Field | Maximum Length | Enforcement |
|-------------|----------------|-------------|
| `sprints.description` | 2048 characters | Application validation (`models.MaxSprintDescription`) |

**Application-Level Validation Rules:**
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

## Migration Idempotency (ALTER TABLE ADD COLUMN)

SQLite's `ALTER TABLE ... ADD COLUMN` is not itself idempotent: re-running it for
a column that already exists raises a "duplicate column name" error. Because a
migration may be applied to a database that has already been partially or fully
migrated, every migration that adds a column MUST guard the `ADD COLUMN` with a
**column-existence check** before executing it. The check queries
`pragma_table_info(<table>)` for the target column name and performs the
`ALTER TABLE ... ADD COLUMN` only when the column is absent:

```sql
-- Add the column only when it does not already exist
SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name = 'completion_summary';
-- If the count is 0, run:
ALTER TABLE tasks ADD COLUMN completion_summary TEXT;
```

This makes each such migration idempotent: applying it to a database that already
has the column is a no-op rather than an error, so re-running the migration set is
safe. Any statement in this specification or in `VERSION.md § Migrations` that
claims a migration is idempotent MUST be backed by this column-existence guard for
every `ADD COLUMN` step; a bare `ALTER TABLE ... ADD COLUMN` without the guard is
not idempotent and is not permitted. The schema-migration mechanism and its version
history are specified in `VERSION.md § Migrations`.

---

## Audit Result Limit

The number of audit entries a single query may return is bounded by a server-side
hard cap, `MaxAuditLimit`, defined in `internal/models/consts.go` with the value
**500**. This cap applies to the audit-entry result sets produced by
`audit list` and the other audit read paths:

1. The `audit list --limit <n>` flag MUST be a positive integer in the range
   `1`-`MaxAuditLimit` (1-500). A value below 1 or above `MaxAuditLimit`, or a
   non-integer value, is rejected with exit code 6 (see
   `COMMANDS.md § List Audit Log`). The audit-list query is never issued with an
   unbounded or larger-than-`MaxAuditLimit` `LIMIT`.
2. `MaxAuditLimit` is the single source of truth for the audit result-set cap;
   `COMMANDS.md` references this value rather than restating it independently.

---

## See Also

- Query caching strategy → `IMPLEMENTATION.md § Query Caching`
- Schema migrations and version history → `VERSION.md § Migrations`
- Concurrency model (WAL, pool, retry) → `IMPLEMENTATION.md § Concurrency Model`
