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
  - [`task_comments` Table](#task_comments-table)
  - [`sprint_comments` Table](#sprint_comments-table)
  - [`_metadata` Table](#_metadata-table)
- [Main SQL Queries](#main-sql-queries)
  - [Tasks](#tasks)
  - [Sprints](#sprints)
  - [Audit](#audit)
  - [Comments](#comments)
- [Relationships](#relationships)
  - [Transactional Atomicity Guarantees](#transactional-atomicity-guarantees)
- [Data Constraints](#data-constraints)
- [Performance Optimization](#performance-optimization)
- [Field Length Validation](#field-length-validation)
- [SQLite Validation](#sqlite-validation)
- [Migration Idempotency (ALTER TABLE ADD COLUMN)](#migration-idempotency-alter-table-add-column)
- [Migration Idempotency (ALTER TABLE DROP COLUMN)](#migration-idempotency-alter-table-drop-column)
- [Audit Result Limit](#audit-result-limit)
- [See Also](#see-also)

## Overview

Each roadmap is stored in an individual SQLite database. The schema is designed to be simple, efficient, and normalized.

### Physical Location and Naming

- Each roadmap has its own home directory at `~/.roadmaps/<name>/`, where `<name>` is the roadmap name. The home directory uses `0700` permissions, owner-only.
- The SQLite database is named `project.db` and lives inside that directory at `~/.roadmaps/<name>/project.db` with `0600` permissions.
- SQLite sidecars (`project.db-wal`, `project.db-shm`) live alongside the database in the same directory. Because they can contain the same data pages as the main database, they are held to the same owner-only `0600` permission as `project.db`.
- The permission model for these files and directories — when `0600` and `0700` are applied, when they are verified, what happens when `project.db` cannot be restricted to `0600`, how the sidecars are treated, and what the read-only open path does — is specified in one place only: `ARCHITECTURE.md § Open-Time Permission Enforcement`. That section is canonical; the values repeated above are a summary and must not be restated here in more detail or amended independently of it.
- The data directory layout and the automatic migration from the legacy `~/.roadmaps/<name>.db` layout are specified in `ARCHITECTURE.md § Directory Structure` and `ARCHITECTURE.md § Filesystem Layout Migration`.

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
|           task_comments                |
|  - id (PK, AUTOINCREMENT)              |
|  - task_id (FK → tasks.id)             |
|  - type (TEXT)                         |
|  - body (TEXT)                         |
|  - created_at (TEXT ISO8601)           |
|  - updated_at (TEXT ISO8601, NULL)     |
+----------------------------------------+
|           sprint_comments              |
|  - id (PK, AUTOINCREMENT)              |
|  - sprint_id (FK → sprints.id)         |
|  - type (TEXT)                         |
|  - body (TEXT)                         |
|  - created_at (TEXT ISO8601)           |
|  - updated_at (TEXT ISO8601, NULL)     |
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
    entity_type TEXT NOT NULL CHECK(entity_type IN ('TASK', 'SPRINT')),
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
- `entity_type`: `'TASK'` or `'SPRINT'`. Values validated by application and enforced by the column `CHECK`.
- `entity_id`: Affected entity ID
- `performed_at`: Operation timestamp

**Valid values (validated by application):** This section is the canonical catalogue of audit operations. All other SPEC files referencing audit operations MUST link here rather than re-listing.

**Tasks:**
- `TASK_CREATE` - New task created
- `TASK_DELETE` - Task deleted (only allowed while in BACKLOG; see Delete Task precondition)
- `TASK_STATUS_CHANGE` - Every status change made by `task stat`, whatever the source and target state, including `SPRINT` → `BACKLOG` and `COMPLETED` → `BACKLOG`. Status changes made as a side effect of a sprint operation are logged against the sprint instead, as `SPRINT_ADD_TASK` / `SPRINT_REMOVE_TASK` / `SPRINT_DELETE`
- `TASK_PRIORITY_CHANGE` - Priority change (0-9) via `task priority`
- `TASK_SEVERITY_CHANGE` - Severity change (0-9) via `task severity`
- `TASK_UPDATE` - Generic update via `task edit` (title, type, functional_requirements, technical_requirements, acceptance_criteria). A type change made through `task edit` is recorded here, not under a dedicated operation.
- `TASK_REOPEN` - Task returned to BACKLOG via `task reopen`; lifecycle timestamps and completion_summary cleared. The sprint_tasks row is removed only when the source state is SPRINT, DOING, or TESTING; from COMPLETED the row is kept
- `TASK_ADD_DEP` - Dependency added (logged against both task_id and depends_on_task_id)
- `TASK_REMOVE_DEP` - Dependency removed (logged against both task_id and depends_on_task_id)
- `TASK_COMMENT_CREATE` - Comment added to a task via `task comment-add` (logged against the parent task)
- `TASK_COMMENT_UPDATE` - Comment edited via `task comment-edit` (logged against the parent task)
- `TASK_COMMENT_DELETE` - Comment deleted via `task comment-remove` (logged against the parent task)

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
- `SPRINT_COMMENT_CREATE` - Comment added to a sprint via `sprint comment-add` (logged against the parent sprint)
- `SPRINT_COMMENT_UPDATE` - Comment edited via `sprint comment-edit` (logged against the parent sprint)
- `SPRINT_COMMENT_DELETE` - Comment deleted via `sprint comment-remove` (logged against the parent sprint)

**Note:** Read operations (GET, STATS, LIST_TASKS) are NOT logged to audit as they do not modify state.

**A stored row may carry an operation the catalogue does not list.** The
`operation` column carries no `CHECK` constraint (see the DDL above), so the
catalogue is enforced by the application when it writes a row and by nothing at all
on the rows already stored. `TASK_ASSIGN` and `TASK_UNASSIGN` are the two values a
Groadmap `audit` table can hold that the catalogue does not list. Three rules govern
them, and they apply together:

1. **They are not in the valid set.** The application writes neither value, and the
   audit read surface accepts neither as an `--operation` filter value: a filter
   naming one of them is rejected as an invalid operation with exit code 6, exactly
   as any other name outside the catalogue is (see
   `COMMANDS.md § List Audit Log`). Neither operation is reachable by name.
2. **The rows carrying them are retained.** No migration deletes them and no read
   path hides them. They continue to appear in an unfiltered `rmp audit list`, in
   `rmp audit history` for the task they were logged against, in the audit
   statistics, and on the read-only web interface's audit log page (see
   `WEB.md § Roadmap Audit Log Page`), so a roadmap's recorded history stays
   complete.
3. **A reader MUST tolerate them.** Any consumer of an audit entry — the CLI's own
   output, the web interface, or an AI agent reading the JSON — MUST treat the
   `operation` value as an opaque string and MUST NOT assume it is one of the
   catalogue's values (see `DATA_FORMATS.md § Audit Entry`).

**Comment operations are recorded against the parent entity.** The six comment operations write `entity_type = 'TASK'` with `entity_id` set to the owning task's id, or `entity_type = 'SPRINT'` with `entity_id` set to the owning sprint's id. They never write the comment's own id and never introduce a new `entity_type` value. Two consequences follow, both intended:

1. The `entity_type` value set stays exactly `TASK` and `SPRINT`. No `COMMENT` entity type is introduced. The set is closed by the table definition itself: `entity_type` carries `CHECK(entity_type IN ('TASK', 'SPRINT'))`, so SQLite rejects a row naming any other entity type whatever the calling code intends. Those two values are validated by the application on every write, and they are also the only two the audit read surface accepts, because `audit history` requires its first positional argument to be `TASK` or `SPRINT` and rejects anything else with exit code 6 (see `COMMANDS.md § Audit Log Management`). Introducing a third value would therefore mean changing the table definition as well as widening a command contract and the catalogue above, all for no gain: a comment operation is always an operation on the task or the sprint that owns the comment, so the parent entity is the correct subject of the entry.
2. The audit trail of a comment survives the comment. `task comment-remove` deletes the row from `task_comments`, but the `TASK_COMMENT_DELETE` entry remains in the audit log against the parent task, so the history of the task still records that a comment was written, edited, and removed. The comment's text is not recoverable from the audit log; the audit log records that the operations happened, not what they contained.

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

### `task_comments` Table

Stores the comments attached to a task. A comment is a durable, typed log entry that records the work carried out within the scope of that task: findings, hypotheses raised and tested, tests run, decisions taken, progress, the reason behind a change to the task's definition, and free-form notes. The relationship is one-task-to-many-comments: a task has many comments, and each comment belongs to exactly one task.

```sql
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
```

**Fields:**
- `task_id`: The owning task. Deleting the task deletes its comments (`ON DELETE CASCADE`).
- `type`: The comment classification. The `CHECK` enumerates exactly the seven values a task comment accepts; the Go-level enum is defined in `MODELS.md § Comment Type`.
- `body`: The comment text, maximum 4096 characters, subject to the Free-Text Control-Character Constraint defined in `MODELS.md § Task`.
- `created_at`: Creation timestamp, never modified afterwards.
- `updated_at`: Last edit timestamp. `NULL` while the comment has never been edited; set on every edit, so a reader can tell that the stored text is no longer the text originally written.

**Comment ids are per-table.** `task_comments.id` and `sprint_comments.id` are independent `AUTOINCREMENT` sequences, so the value `7` may exist in both tables and address two unrelated comments. A comment id is only ever meaningful together with the family that owns it (`rmp task comment-edit 7` and `rmp sprint comment-edit 7` address different rows; see `COMMANDS.md § Edit Task Comment` and `COMMANDS.md § Edit Sprint Comment`).

**No authorship.** A comment records no author. The table has no author column, consistent with the `audit` table, which records no actor either.

### `sprint_comments` Table

Stores the comments attached to a sprint. A sprint comment records only the progression of the work during the sprint's development: findings, decisions taken, progress, and the reason behind a change to the sprint's definition. The relationship is one-sprint-to-many-comments: a sprint has many comments, and each comment belongs to exactly one sprint.

```sql
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
```

**Fields:** identical in meaning to the `task_comments` fields above, with `sprint_id` in place of `task_id`. The `type` `CHECK` enumerates exactly the four values a sprint comment accepts: a sprint comment records the progression of the sprint, so the task-only values `HYPOTHESIS`, `TEST`, and `NOTE` are not accepted at either the database or the application level.

**Two tables, deliberately.** Comments are stored in two separate tables rather than one polymorphic table. Each table has a single `NOT NULL` foreign key, a single `ON DELETE CASCADE` rule, and its own `CHECK` enumerating that entity's valid types. There is no nullable parent column and no exclusivity `CHECK`, so each table is readable and verifiable on its own. The accepted cost is duplication: two sets of queries, two models, and two sets of tests.

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
    ('schema_version', '1.10.0'),
    ('created_at', '2026-03-20T00:00:00.000Z'),
    ('application', 'Groadmap');
```

---

## Main SQL Queries

### Tasks

#### Insert Task

```sql
INSERT INTO tasks (title, status, type, functional_requirements, technical_requirements, acceptance_criteria, created_at, priority, severity)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);  -- created_at set by application (ISO 8601 UTC)
```

#### List All

Returns every task, each row carrying the complete `Task` object: the task's stored columns, its computed subtask count, and its two dependency sets.

```sql
SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements,
       t.acceptance_criteria, t.created_at, t.started_at, t.tested_at,
       t.closed_at, t.completion_summary, t.parent_task_id,
       t.priority, t.severity,
       (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count,
       (SELECT COALESCE(group_concat(d), '') FROM (
           SELECT depends_on_task_id AS d FROM task_dependencies
           WHERE task_id = t.id ORDER BY depends_on_task_id
       )) AS depends_on_csv,
       (SELECT COALESCE(group_concat(b), '') FROM (
           SELECT task_id AS b FROM task_dependencies
           WHERE depends_on_task_id = t.id ORDER BY task_id
       )) AS blocks_csv
FROM tasks t WHERE 1=1
ORDER BY t.priority DESC, t.created_at ASC;
```

**What the statement returns:**
- The task's **stored columns**, named one by one rather than through `*`, so the column order the application scans is fixed by the statement itself and not by the table's current shape.
- **`subtask_count`**, the number of direct subtasks, produced by the correlated subquery above. It is not a stored column — `MODELS.md § Task` defines it as computed — and this statement is where its value comes from, so the caller needs no second query and no per-task query to obtain it.
- **`depends_on_csv`** and **`blocks_csv`**, the task's two dependency sets, each a comma-separated list of task ids in ascending id order (fixed by the inner `ORDER BY`) and the empty string when the set is empty (`COALESCE`). The application parses them into the task's `depends_on` and `blocks` values, which keeps the listing free of one dependency query per task.

**One statement, several shapes.** The listing is assembled rather than fixed. It opens `WHERE 1=1` so that each optional predicate can be appended as a further `AND` — the status filter of `List by Status` below, and the priority, severity, type, and creation-date filters of the same listing — and it carries one of four orderings: `t.priority DESC, t.created_at ASC` (the default, shown above), `t.created_at ASC`, `t.status ASC, t.priority DESC, t.created_at ASC`, or `t.severity DESC, t.priority DESC, t.created_at ASC`. `COMMANDS.md § List Tasks` is canonical for which caller selects which. Every filter value is a bound parameter; none is concatenated into the SQL, and a value used in a `LIKE` predicate has its wildcards escaped first.

**Result-set size:** The listing itself imposes no bound: it carries no `OFFSET`, and it carries a `LIMIT ?` only when the caller asks for one. Any bound on the number of rows a caller receives is therefore the caller's, not this query's.

**The web tasks page reads this listing unbounded.** The read-only web interface's
Kanban task board reads every task of the roadmap through this statement, with no
`LIMIT` applied and no pagination (see `WEB.md § Roadmap Tasks Page`). The display
default that sizes `rmp task list` output — `-l, --limit <n>`, default `100` (see
`COMMANDS.md § List Tasks`) — MUST NOT be applied to this read. That default exists
to size the output of one command invocation, where a caller who wants more asks for
more and can see that the listing was cut. The board has no such affordance: it
groups the tasks it reads into five columns and presents a count on each column
header as a statement of fact about the roadmap. A partial read would therefore not
merely show fewer cards, it would publish wrong counts as true ones, with nothing on
the page to reveal that anything was omitted. Reading every row is what makes those
counts correct by construction.

#### List by Status

The status filter is one predicate on the listing above, not a statement of its own:

```sql
SELECT ...  -- the select list of List All above, unchanged
FROM tasks t WHERE 1=1
  AND t.status = ?
ORDER BY t.priority DESC, t.created_at ASC;
```

**Ordering.** `priority DESC` is not the whole ordering. `created_at ASC` breaks its ties, exactly as in the unfiltered listing, so two tasks of equal priority are returned oldest first rather than in whichever order SQLite is free to produce. A caller that asks for a different sort gets one of the other three orderings named in `List All` above; the status predicate does not change which orderings are available.

**The status value is bound, never concatenated,** like every other filter value of this listing.

#### List by Sprint

A sprint's membership is read in two shapes, and the choice is the caller's.

**The ids alone**, for a caller that needs the membership and not the tasks:

```sql
SELECT task_id FROM sprint_tasks WHERE sprint_id = ? ORDER BY task_id;
```

This statement reads `sprint_tasks` only and joins nothing: the answer is a set of ids, so no task row is fetched to produce it. It orders by `task_id` ascending, which is not the sprint's planned order — the planned order is `sprint_tasks.position`, and a caller that needs it reads one of the two listings below.

**The tasks themselves**, through the sprint listing documented in the two sections that follow — `List Sprint Tasks Ordered (Priority → Position)` and `List Sprint Tasks Ordered by Position`. Those two are **one** statement under two `ORDER BY` forms, with the same select list, the same join, and the same optional `AND t.status = ?` predicate; they are not two different reads of the schema.

#### List Sprint Tasks Ordered (Priority → Position)

Returns all tasks in a sprint ordered by priority (descending), with the sprint position breaking ties. It is the statement of `List Sprint Tasks Ordered by Position` below under its other `ORDER BY`: the select list, the join, the `WHERE` clause, and the optional status predicate are identical, and only the ordering differs.

```sql
SELECT ...  -- the select list of List Sprint Tasks Ordered by Position below, unchanged
FROM tasks t
INNER JOIN sprint_tasks st ON t.id = st.task_id
WHERE st.sprint_id = ?
ORDER BY t.priority DESC, st.position ASC;
```

**Ordering priority:**
1. `priority` DESC (highest first: 9 → 0)
2. `position` ASC (the sprint's planned order) as the tie-breaker

**Severity does not order this listing.** Two tasks of equal priority are returned in the sprint's planned order, not by severity: `severity` appears in the `ORDER BY` of no sprint listing. The one ordering that leads with severity is a whole-roadmap task listing (`t.severity DESC, t.priority DESC, t.created_at ASC`; see `List All` above), which reads no `sprint_tasks` row and is not this statement.

**Use case:** Sprint execution view for a caller that wants the most urgent work first — and, within one priority, the order the user planned.

#### List Sprint Tasks Ordered by Position

Returns all tasks in a sprint ordered by their position in the sprint task list, each row carrying the complete `Task` object — the same three groups of values `List All` above returns, from the same select list: the task's stored columns, its computed subtask count, and its two dependency sets.

```sql
SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements,
       t.acceptance_criteria, t.created_at, t.started_at, t.tested_at,
       t.closed_at, t.completion_summary, t.parent_task_id,
       t.priority, t.severity,
       (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count,
       (SELECT COALESCE(group_concat(d), '') FROM (
           SELECT depends_on_task_id AS d FROM task_dependencies
           WHERE task_id = t.id ORDER BY depends_on_task_id
       )) AS depends_on_csv,
       (SELECT COALESCE(group_concat(b), '') FROM (
           SELECT task_id AS b FROM task_dependencies
           WHERE depends_on_task_id = t.id ORDER BY task_id
       )) AS blocks_csv
FROM tasks t
INNER JOIN sprint_tasks st ON t.id = st.task_id
WHERE st.sprint_id = ?
ORDER BY st.position ASC;
```

**What the statement returns:**
- The task's **stored columns**, named one by one rather than through `t.*`, so the column order the application scans is fixed by the statement itself and not by the table's current shape.
- **`subtask_count`**, the number of direct subtasks, produced by the correlated subquery above. It is not a stored column — `MODELS.md § Task` defines it as computed — and this statement is where its value comes from on this path, so the caller needs no second query and no per-task query to obtain it.
- **`depends_on_csv`** and **`blocks_csv`**, the task's two dependency sets, each a comma-separated list of task ids in ascending id order (fixed by the inner `ORDER BY`) and the empty string when the set is empty (`COALESCE`). The application parses them into the task's `depends_on` and `blocks` values, which is what keeps the listing free of one dependency query per task.

**`st.position` orders the rows and is not selected.** The caller receives the tasks already in the planned order; the position value itself is not part of the `Task` object (`MODELS.md § Task`).

**Optional status filter:** a caller that wants a single status adds `AND t.status = ?` to the `WHERE` clause. The select list and the ordering are unchanged by it.

**The other ordering of this statement** — `t.priority DESC, st.position ASC` — is documented as `List Sprint Tasks Ordered (Priority → Position)` above. Only the `ORDER BY` differs between the two.

**Ordering priority:**
1. `position` ASC (lowest first: 0, 1, 2...)

**Use case:** Sprint task sequence view - tasks appear in the order defined by the user for sprint execution. The read-only web interface's sprint page reads this statement to fill its member-tasks board: the board groups the rows into its three columns in memory and then orders the `DOING` column by `started_at` descending and the `CLOSED` column by `closed_at` descending, so the position order this statement returns is what the `WAITING` column keeps and what breaks ties in the other two. The board shows each task's subtask count on its card without any further query. The per-column ordering is presentation and belongs to `WEB.md § Sprint Detail Sub-Template`, which states it in full; this section owns the statement, not the board.

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

-- Returning a task to BACKLOG: clear the tracking dates and the completion
-- summary. The same statement serves `task stat <ids> BACKLOG` (accepted from
-- SPRINT and COMPLETED only) and `task reopen` (accepted from any non-BACKLOG
-- state). Neither command writes to sprint_tasks; see
-- STATE_MACHINE.md § Sprint Membership and the BACKLOG Status.
UPDATE tasks
SET status = 'BACKLOG', started_at = NULL, tested_at = NULL, closed_at = NULL, completion_summary = NULL
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
-- Remove from junction table, scoped to the named sprint
DELETE FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?;

-- Reset the task, whatever its status was inside the sprint
UPDATE tasks
SET status = 'BACKLOG', started_at = NULL, tested_at = NULL, closed_at = NULL, completion_summary = NULL
WHERE id = ?;
```

#### Clear All Tasks from Sprint

The status reset MUST run before the membership rows are deleted; once the
`sprint_tasks` rows are gone the subquery selects nothing.

```sql
-- Reset every member task, whatever its status was inside the sprint
UPDATE tasks
SET status = 'BACKLOG', started_at = NULL, tested_at = NULL, closed_at = NULL, completion_summary = NULL
WHERE id IN (
    SELECT task_id FROM sprint_tasks WHERE sprint_id = ?
);

-- Then remove all sprint relationships
DELETE FROM sprint_tasks WHERE sprint_id = ?;
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

-- Reset every member task, whatever its status was inside the sprint
-- Note: in implementation, do this before deleting sprint
UPDATE tasks
SET status = 'BACKLOG', started_at = NULL, tested_at = NULL, closed_at = NULL, completion_summary = NULL
WHERE id IN (
    SELECT task_id FROM sprint_tasks WHERE sprint_id = ?
);

-- Then remove relationships
DELETE FROM sprint_tasks WHERE sprint_id = ?;

-- Finally remove sprint
DELETE FROM sprints WHERE id = ?;
```

#### Resolve the Sprint of Many Tasks (Grouped)

Returns the sprint each task of a given set belongs to, in one round trip, so that a caller can walk the result once and index it by task without re-sorting.

```sql
-- Sprint membership of several tasks at once. The IN list is built from the same
-- number of placeholders as ids, never by string concatenation.
SELECT st.task_id, s.id, s.title
FROM sprint_tasks st
INNER JOIN sprints s ON s.id = st.sprint_id
WHERE st.task_id IN (?, ?, ...)
ORDER BY st.task_id ASC;
```

**At most one row per task.** `sprint_tasks.task_id` carries a `UNIQUE` constraint, so a task belongs to at most one sprint at any time (see the `sprint_tasks` Table section above and Relationships below). The query therefore returns at most one row per task id, and the caller needs no de-duplication step.

**No row for a task without a sprint.** A task that belongs to no sprint has no `sprint_tasks` row, so the result simply carries no entry for that task id. The absence of an entry is the answer: the query returns neither a `NULL` row nor a placeholder for it. The inner join excludes nothing else, because `sprint_id` is `NOT NULL` and carries a foreign key to `sprints(id)`, so every `sprint_tasks` row matches exactly one sprint.

**Empty id set.** When the id set is empty, the application skips the query entirely instead of issuing a statement with an empty `IN` list. Every grouped read in this file that takes a set of ids follows this rule.

**Ordering.** `task_id` ascending. The order makes the result walkable in one pass against a caller-side set of task ids; it carries no other meaning, and no tie-breaker is needed because at most one row exists per task id.

**Index.** The query needs no new index. `WHERE st.task_id IN (...)` is served by `idx_sprint_tasks_task_id`, the single-column index the `sprint_tasks` DDL already declares on `task_id`, and by the implicit unique index SQLite creates for that column's `UNIQUE` constraint. The join resolves `sprints` by its primary key. See Performance Optimization below.

**Use case:** the read-only web interface renders the roadmap's tasks as a Kanban board and shows on each card the sprint that task belongs to, so it MUST resolve the sprint of every rendered task with this single grouped query rather than one query per task or one query per board column (see `WEB.md § Roadmap Tasks Page`).

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

#### Query Audit Entries

Every audit read is **one** assembled statement, not a family of statements. It names the five columns of an audit entry, appends only the predicates the caller supplied, and always orders and bounds the result:

```sql
SELECT id, operation, entity_type, entity_id, performed_at
FROM audit WHERE 1=1
  AND operation = ?          -- optional
  AND entity_type = ?        -- optional
  AND entity_id = ?          -- optional
  AND performed_at >= ?      -- optional (ISO 8601 UTC)
  AND performed_at <= ?      -- optional (ISO 8601 UTC)
ORDER BY performed_at DESC
LIMIT ?                      -- always present, always clamped
OFFSET ?;                    -- only when the caller asks to skip rows
```

**The five columns are named, never `*`.** They are the whole of `AuditEntry` (`MODELS.md § Audit Entry`), and naming them fixes the result-set order the application scans rather than leaving it to the table's current shape.

**The predicates are optional and compose.** The statement opens `WHERE 1=1` so that each predicate the caller supplied can be appended as a further `AND`; a caller that supplies none reads the whole log. Every value is a bound parameter and none is concatenated into the SQL.

**`operation` is matched by equality only.** This table carries no `LIKE` predicate on any column, so there is no pattern search over operation names: a caller that wants a family of operations either issues one read per operation value or filters the entries it received.

**The ordering is not a caller choice.** It is always `performed_at DESC` — the most recently performed operation first — because the audit log is read as a history.

**The `LIMIT` is always present and always clamped** to `MaxAuditLimit`, whatever the caller asked for, including a caller that asked for none (see `Audit Result Limit` below). An entity history is bounded by the same clamp: the history of one task or sprint is the newest `MaxAuditLimit` entries of that entity, not every row ever written for it.

#### Query Audit Log with Filters

Each documented "filter" is a caller's choice of predicates on the statement above, not a statement of its own. The four shapes the application uses:

| Caller | Predicates supplied | Notes |
|---|---|---|
| Full audit log, newest first | none | the read-only web audit log page, which pages through it with `OFFSET` (see `WEB.md § Roadmap Audit Log Page`) |
| Entity history | `entity_type = ?` and `entity_id = ?` | the history of one task or one sprint, newest first, bounded by the clamp above |
| Operation filter | `operation = ?` | one operation value, by equality |
| Date range | `performed_at >= ?` and `performed_at <= ?` | ISO 8601 UTC bounds, either or both |

The shapes combine: supplying an entity, an operation, and a date range at once appends all four predicates to the one statement, in the order shown above, and changes neither the select list, the ordering, nor the clamp.

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

### Comments

Every statement below exists in a `task_comments` form and a `sprint_comments` form, with the single exception noted under `Count Comments for Many Parents (Grouped)`. The two forms are identical apart from the table name and the parent-key column (`task_id` / `sprint_id`); only the task form is written out where the sprint form adds nothing.

#### Insert Comment

```sql
-- Task comment. created_at set by the application (ISO 8601 UTC); updated_at
-- starts NULL and is written only by a later edit.
INSERT INTO task_comments (task_id, type, body, created_at)
VALUES (?, ?, ?, ?);

-- Sprint comment
INSERT INTO sprint_comments (sprint_id, type, body, created_at)
VALUES (?, ?, ?, ?);
```

The insert and its audit entry (`TASK_COMMENT_CREATE` / `SPRINT_COMMENT_CREATE`, written against the parent entity) MUST run in the same transaction.

#### List Comments for One Parent

```sql
-- All comments of one task, oldest first
SELECT id, task_id, type, body, created_at, updated_at
FROM task_comments
WHERE task_id = ?
ORDER BY created_at ASC, id ASC;

-- Optional type filter
SELECT id, task_id, type, body, created_at, updated_at
FROM task_comments
WHERE task_id = ? AND type = ?
ORDER BY created_at ASC, id ASC;
```

**Ordering:** `created_at` ascending, with `id` ascending as the tie-breaker. The listing is a log, so the oldest entry comes first. The tie-breaker is required because two comments created within the same millisecond share a `created_at` value, and without it their relative order would be undefined. The result set is unbounded: the query carries no `LIMIT` and no `OFFSET` (see `COMMANDS.md § List Task Comments`).

#### Get One Comment by Id

```sql
SELECT id, task_id, type, body, created_at, updated_at
FROM task_comments
WHERE id = ?;
```

**Use case:** the application resolves the comment before editing or removing it, so that a missing id is reported as a not-found condition (exit code 4) before any write.

#### Update Comment

```sql
-- Edit body and type
UPDATE task_comments SET type = ?, body = ?, updated_at = ? WHERE id = ?;

-- Edit body only
UPDATE task_comments SET body = ?, updated_at = ? WHERE id = ?;

-- Edit type only
UPDATE task_comments SET type = ?, updated_at = ? WHERE id = ?;
```

`updated_at` is written on every edit, whichever columns the edit touches. The edit replaces the stored `body` in place: the previous text is not retained anywhere and is not recoverable. The update and its audit entry (`TASK_COMMENT_UPDATE` / `SPRINT_COMMENT_UPDATE`) MUST run in the same transaction.

#### Delete Comment

```sql
DELETE FROM task_comments WHERE id = ?;
```

The row is removed outright; there is no soft delete and no tombstone. The delete and its audit entry (`TASK_COMMENT_DELETE` / `SPRINT_COMMENT_DELETE`) MUST run in the same transaction.

#### Count Comments for Many Parents (Grouped)

Returns how many comments each task of a given set has, in one round trip, without reading any comment body.

```sql
-- Comment counts for several tasks at once. The IN list is built from the same
-- number of placeholders as ids, never by string concatenation.
SELECT task_id, COUNT(*) AS comment_count
FROM task_comments
WHERE task_id IN (?, ?, ...)
GROUP BY task_id
ORDER BY task_id ASC;
```

**No row for a task without comments.** A task with no comment produces no group, so the result carries no entry for that task id and the caller reads its count as zero. The query never returns a row whose `comment_count` is `0`.

**Empty id set.** When the id set is empty, the application skips the query entirely instead of issuing a statement with an empty `IN` list, as every grouped read that takes a set of ids does.

**Index.** Served by `idx_task_comments_task_created`, whose leading column is `task_id`; the aggregate needs no further index and reads no `body` value. See Performance Optimization below.

**Use case:** the two boards of the read-only web interface — the roadmap tasks page's Kanban task board and the sprint page's member-tasks board (see `WEB.md § Roadmap Tasks Page` and `WEB.md § Sprint Detail Sub-Template`) — each show a comment count on a card but no comment text, because the card's modal loads a task's comments on demand from its own endpoint (see `WEB.md § Task Detail Endpoint`). Neither board therefore ever reads a comment body in order to display a number, and no read anywhere loads the comment text of several tasks at once: a task's comments are read one task at a time, through the single-parent listing above.

This statement has no `sprint_comments` form: the Roadmap Sprint Page presents one sprint's comment log in full through the single-parent listing, and no surface counts the comments of several sprints at once.

---

## Relationships

```
+-------------+           +-----------------+           +-------------+
|   sprints   |           |  sprint_tasks   |           |    tasks    |
|     id      | 1      N  |  sprint_id (FK) | N      1  |     id      |
|   (PK)      |-----------|  task_id (FK)   |-----------|   (PK)      |
|             |           |  (Composite PK) |           |             |
+------+------+           +-----------------+           +------+------+
       | 1                                                     | 1
       |                                                       |
       | N                                                     | N
+------+-------------+                              +----------+---------+
|  sprint_comments   |                              |   task_comments    |
|  sprint_id (FK)    |                              |   task_id (FK)     |
|  id (PK)           |                              |   id (PK)          |
+--------------------+                              +--------------------+
```

**Integrity rules:**
- A task may not be in any sprint (no record in `sprint_tasks`)
- A task can only be in one sprint at a time (`UNIQUE` constraint on `sprint_tasks.task_id`)
- When deleting sprint, relationships in `sprint_tasks` are removed (`ON DELETE CASCADE`)
- Tasks are never automatically deleted, only disassociated
- A task may have no comments (no record in `task_comments`); a sprint may have no comments (no record in `sprint_comments`)
- Every comment belongs to exactly one parent: `task_comments.task_id` and `sprint_comments.sprint_id` are `NOT NULL`, so a comment can never exist without its parent
- Deleting a task deletes that task's comments, and deleting a sprint deletes that sprint's comments (`ON DELETE CASCADE`). Removing a task from a sprint deletes only the `sprint_tasks` row; it deletes no comment, because a task's comments belong to the task and not to the sprint
- Comments are never moved between parents. Moving a task between sprints (`sprint move-tasks`) re-parents the `sprint_tasks` row only; the task keeps its own comments and neither sprint's comments change

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
   writing the audit entry MUST occur in the same transaction. No committed state
   shows a task still marked `SPRINT`, `DOING`, or `TESTING` after its
   `sprint_tasks` row is gone. The converse combination is legitimate and is not a
   partial commit: a task in `BACKLOG` status can hold a live `sprint_tasks` row,
   because `task stat <ids> BACKLOG` changes the status without touching
   membership (see `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status`).
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
7. **Writing a comment (`AddTaskComment`, `AddSprintComment`, `UpdateTaskComment`,
   `UpdateSprintComment`, `DeleteTaskComment`, `DeleteSprintComment`).** The
   `INSERT`, `UPDATE`, or `DELETE` on the comment table and the matching audit
   entry against the parent entity MUST occur in the same transaction. A committed
   comment change can never exist without its audit record, and an audit record can
   never exist for a change that was rolled back.

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
| entity_type | TEXT | NOT NULL, CHECK enum values: TASK, SPRINT |
| entity_id | INTEGER | NOT NULL |
| performed_at | TEXT | NOT NULL, ISO 8601 format |

**Valid values (validated by application):**
- `operation`: See the canonical catalogue in the `audit` Table section above (Tasks + Sprints).
- `entity_type`: TASK, SPRINT

### Task_Comments

| Column | Type | Constraints |
|--------|------|-------------|
| id | INTEGER | PK, AUTOINCREMENT; unique within `task_comments` only |
| task_id | INTEGER | NOT NULL, FK → tasks.id, ON DELETE CASCADE |
| type | TEXT | NOT NULL, CHECK enum values: FINDING, HYPOTHESIS, TEST, DECISION, PROGRESS, UPDATE, NOTE |
| body | TEXT | NOT NULL, CHECK length <= 4096 chars, non-empty (application) |
| created_at | TEXT | NOT NULL, ISO 8601 format |
| updated_at | TEXT | NULLABLE, ISO 8601 format; NULL until the comment is first edited |

**Note:** The `type` value is mandatory: there is no default and no fallback value. An `INSERT` or `UPDATE` carrying a value outside the seven listed above fails the `CHECK`; the application rejects it first, with exit code 6 (see `COMMANDS.md § Add Task Comment`).

### Sprint_Comments

| Column | Type | Constraints |
|--------|------|-------------|
| id | INTEGER | PK, AUTOINCREMENT; unique within `sprint_comments` only |
| sprint_id | INTEGER | NOT NULL, FK → sprints.id, ON DELETE CASCADE |
| type | TEXT | NOT NULL, CHECK enum values: FINDING, DECISION, PROGRESS, UPDATE |
| body | TEXT | NOT NULL, CHECK length <= 4096 chars, non-empty (application) |
| created_at | TEXT | NOT NULL, ISO 8601 format |
| updated_at | TEXT | NULLABLE, ISO 8601 format; NULL until the comment is first edited |

**Note:** The four accepted values are a subset of the seven a task comment accepts. `HYPOTHESIS`, `TEST`, and `NOTE` are rejected on a sprint comment with exit code 6 (see `COMMANDS.md § Add Sprint Comment`).

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
| `idx_task_comments_task_created` | task_comments | (task_id, created_at ASC) | Optimizes the comment listing of one task, and the grouped comment count of many tasks |
| `idx_sprint_comments_sprint_created` | sprint_comments | (sprint_id, created_at ASC) | Optimizes the comment listing of one sprint |

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

**idx_task_comments_task_created and idx_sprint_comments_sprint_created:**
- Query pattern: `WHERE task_id = ? ORDER BY created_at ASC` (and the `sprint_id` equivalent)
- The leading column serves the parent lookup and the trailing column serves the listing order, so one index covers both and no sort step is needed
- The same index serves the grouped `WHERE task_id IN (...) GROUP BY task_id` count the web interface's task board uses to show a comment count per card without reading any body
- A single index per table is sufficient: every comment listing filters on the parent key, so no query ever scans a comment table without it, and no listing is ordered by any other column

**Grouped sprint resolution needs no new index.** The grouped query that resolves the sprint of many tasks at once (see `Resolve the Sprint of Many Tasks (Grouped)` above) filters with `WHERE sprint_tasks.task_id IN (...)` and joins `sprints` by primary key. The `task_id` lookup is already served by `idx_sprint_tasks_task_id`, the single-column index the `sprint_tasks` DDL declares, and by the implicit unique index SQLite creates for the `UNIQUE` constraint on that column. No index is added for this query, and `idx_sprint_tasks_lookup` (leading column `sprint_id`) is not the index that serves it.

### Verification

Index usage is verified by planning **the statement the application issues**, taken from the production query builder itself, with the bind arguments production passes:

```sql
-- The SQL comes from the builder (for example the task listing of
-- `List All` above, assembled with a status filter); it is never retyped here.
EXPLAIN QUERY PLAN <the production statement>;
-- Expected of the plan:
--   * it names the index the section above creates for that statement
--     (for the status-filtered task listing: idx_tasks_status_priority);
--   * it contains no `SCAN <target table>`, which would mean the index is
--     not doing its job even if the plan mentions it elsewhere, such as in
--     a subquery;
--   * for a comment listing, it contains no `TEMP B-TREE`, because the
--     index must supply the `created_at` order rather than the engine
--     sorting the result afterwards.
```

The bind arguments are required even to plan the statement, so a check passes the same values production passes.

**A hand-written lookalike proves nothing, and this is why the statement is taken from the builder rather than retyped.** SQLite plans a statement from its select list, its predicates, its ordering and its limit; a lookalike differs from the real statement in each of those, so it can be served by a different index — or by none — than the statement it stands in for. A check written that way certifies a query the application never issues, and it keeps passing while the real statement drifts away from its index. Taking the SQL from the builder is what makes that drift fail the check instead of hiding behind it.

This verification is automated, not left to hand-running: `internal/db/index_test.go` plans the production statements of the task listing, the audit listing, the sprint membership lookup, and the comment listings, and asserts the three expectations above for each. The production builders are separated from execution precisely so a check can obtain that SQL.

---

## Field Length Validation

The following length constraints are enforced at the database level using CHECK constraints:

| Field | Maximum Length | Constraint |
|-------|----------------|------------|
| `tasks.title` | 255 characters | `CHECK(length(title) <= 255)` |
| `tasks.functional_requirements` | 4096 characters | `CHECK(length(functional_requirements) <= 4096)` |
| `tasks.technical_requirements` | 4096 characters | `CHECK(length(technical_requirements) <= 4096)` |
| `tasks.acceptance_criteria` | 4096 characters | `CHECK(length(acceptance_criteria) <= 4096)` |
| `tasks.completion_summary` | 4096 characters | `CHECK(completion_summary IS NULL OR length(completion_summary) <= 4096)` |
| `sprints.title` | 255 characters | `CHECK(length(title) <= 255)` |
| `task_comments.body` | 4096 characters | `CHECK(length(body) <= 4096)` |
| `sprint_comments.body` | 4096 characters | `CHECK(length(body) <= 4096)` |

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

## Migration Idempotency (ALTER TABLE DROP COLUMN)

SQLite's `ALTER TABLE ... DROP COLUMN` is not idempotent either: re-running it for a
column that is already gone raises a "no such column" error. Every migration that
drops a column MUST therefore guard the `DROP COLUMN` with the same
**column-existence check** the section above specifies, applied with the opposite
sense — the `ALTER TABLE ... DROP COLUMN` runs only while the column is still
present:

```sql
-- Drop the column only while it still exists
SELECT COUNT(*) FROM pragma_table_info('tasks') WHERE name = 'specialists';
-- If the count is 1, run:
ALTER TABLE tasks DROP COLUMN specialists;
```

Any statement in this specification or in `VERSION.md § Migrations` that claims a
migration is idempotent MUST be backed by this guard for every `DROP COLUMN` step; a
bare `ALTER TABLE ... DROP COLUMN` without the guard is not idempotent and is not
permitted.

**Availability.** `ALTER TABLE ... DROP COLUMN` requires SQLite 3.35.0 or later. The
pinned pure-Go driver (see `BUILD.md § External Dependencies`) embeds a later SQLite
release than that on every platform the project builds for, so the statement is
always available and no table-rebuild fallback is specified.

**What the drop preserves.** Dropping a column rewrites the table definition and
leaves the rest of the table intact: every remaining column keeps its values, its
`CHECK` constraint, and its `DEFAULT` clause; the table's indexes and foreign keys
survive; and `AUTOINCREMENT` continues from the value it had reached. Only the
dropped column and the values in it go.

**What the drop discards.** The values the dropped column held are destroyed and are
not recoverable from the database afterwards. A migration that drops a column
therefore loses data by definition, and it is permitted only where losing that data
is the purpose of the change rather than a side effect of it.

**When SQLite refuses to drop a column.** `DROP COLUMN` is rejected when the column
is a `PRIMARY KEY` or part of one, carries a `UNIQUE` constraint, is indexed, is
named in a table-level `CHECK` constraint or in a partial index's `WHERE` clause, is
used by a foreign key or by a generated column, or appears in a view or a trigger. A
column that any of these describes has to be removed by rebuilding the table
instead. Every column this specification drops MUST be free of all of them, which is
what makes the single guarded statement above sufficient.

**The column this rule governs.** `tasks.specialists` is dropped by the
`1.9.0` → `1.10.0` migration; `VERSION.md § Migrations` is canonical for that
migration's statements and carries them in full. The column is a plain nullable
`TEXT` column with no index, no `CHECK`, no `UNIQUE`, no foreign key, and no view or
trigger referring to it, so the guarded single statement removes it. The values it
held are discarded, which is the purpose of that migration and not a side effect of
it.

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

- Database file permissions (`0600`, when enforced, failure mode) → `ARCHITECTURE.md § Open-Time Permission Enforcement`
- Query caching strategy → `IMPLEMENTATION.md § Query Caching`
- Schema migrations and version history → `VERSION.md § Migrations`
- Concurrency model (WAL, pool, retry) → `IMPLEMENTATION.md § Concurrency Model`
