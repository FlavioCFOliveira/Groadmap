# Database Schema

## Table of Contents

- [Overview](#overview)
- [Naming Conventions](#naming-conventions)
- [SQLite File Structure](#sqlite-file-structure)
- [DDL - Table Creation](#ddl---table-creation)
  - [`tasks` Table](#tasks-table)
  - [`sprints` Table](#sprints-table)
  - [`sprint_tasks` Table (1:N Relationship)](#sprint_tasks-table-1n-relationship)
    - [Position Uniqueness Within a Sprint](#position-uniqueness-within-a-sprint)
    - [Position Density Within a Sprint](#position-density-within-a-sprint)
  - [`audit` Table](#audit-table)
    - [One Row per Thing That Happened](#one-row-per-thing-that-happened)
    - [The Two Entities of a Relational Operation](#the-two-entities-of-a-relational-operation)
    - [The Commit Hash of an Audit Entry](#the-commit-hash-of-an-audit-entry)
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
- [Commit Hash Format Constraint](#commit-hash-format-constraint)
- [SQLite Validation](#sqlite-validation)
- [Migration Idempotency (ALTER TABLE ADD COLUMN)](#migration-idempotency-alter-table-add-column)
- [Migration Idempotency (ALTER TABLE DROP COLUMN)](#migration-idempotency-alter-table-drop-column)
- [Introducing a Uniqueness Constraint over Existing Rows](#introducing-a-uniqueness-constraint-over-existing-rows)
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
|  - commit_open (TEXT hex 7-64, NULL)   |
|  - commit_close (TEXT hex 7-64, NULL)  |
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
|  - position (INTEGER, UNIQUE/sprint)   |
|  - Composite PK (sprint_id, task_id)   |
+----------------------------------------+
|           audit                        |
|  - id (PK, AUTOINCREMENT)              |
|  - operation (TEXT)                    |
|  - entity_type (TEXT)                  |
|  - entity_id (INTEGER)                 |
|  - related_entity_id (INTEGER, NULL)   |
|  - commit_hash (TEXT hex 7-64, NULL)   |
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
    -- Git commit hashes bracketing the work. Stored lowercase; the CHECK rejects any other case
    -- because GLOB is case-sensitive in SQLite, so it backs the application's lowercase normalisation.
    commit_open TEXT CHECK(commit_open IS NULL OR (length(commit_open) BETWEEN 7 AND 64 AND commit_open NOT GLOB '*[^0-9a-f]*')),    -- Commit the task was started from, set on every transition into DOING
    commit_close TEXT CHECK(commit_close IS NULL OR (length(commit_close) BETWEEN 7 AND 64 AND commit_close NOT GLOB '*[^0-9a-f]*')),  -- Commit the task was concluded at, set on every transition into COMPLETED
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

Junction table linking sprints to their tasks. The relationship is one-sprint-to-many-tasks: a sprint contains many tasks, but each task belongs to at most one sprint at any given time. This 1:N constraint is enforced at the schema level by the `UNIQUE` constraint on `task_id`. The table also stores the sprint's planned execution order in `position`, and that order is total: no two member tasks of one sprint may hold the same `position`, and the schema enforces it (see `Position Uniqueness Within a Sprint` below). The order is also dense: the members of a sprint hold exactly the values `0` to `N-1`, so a member's stored `position` is its rank in that order. The schema cannot enforce density and the write paths uphold it instead (see `Position Density Within a Sprint` below).

```sql
CREATE TABLE IF NOT EXISTS sprint_tasks (
    sprint_id INTEGER NOT NULL,
    task_id INTEGER NOT NULL UNIQUE,
    added_at TEXT NOT NULL,  -- ISO 8601 UTC
    position INTEGER NOT NULL DEFAULT 0,  -- 0-based position in sprint task order; unique within one sprint (idx_sprint_tasks_order)
    PRIMARY KEY (sprint_id, task_id),
    FOREIGN KEY (sprint_id) REFERENCES sprints(id) ON DELETE CASCADE,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_sprint_tasks_task_id ON sprint_tasks(task_id);

-- Composite index for sprint task lookups (TASK-P001)
-- Covers: GetSprintTasks and sprint-task relationship queries
CREATE INDEX IF NOT EXISTS idx_sprint_tasks_lookup ON sprint_tasks(sprint_id, task_id);

-- Unique composite index for sprint task ordering (TASK-ORDER-001)
-- Covers: Sprint task listing ordered by position
-- Enforces: no two member tasks of one sprint hold the same position, which is what
-- makes the planned execution order total (see Position Uniqueness Within a Sprint below)
CREATE UNIQUE INDEX IF NOT EXISTS idx_sprint_tasks_order ON sprint_tasks(sprint_id, position ASC);
```

#### Position Uniqueness Within a Sprint

**The invariant.** Within one sprint, no two member tasks hold the same `position`.
The pair `(sprint_id, position)` is unique across the whole table; `position` on its
own is not, so two tasks in two different sprints may both sit at position 0.

**The invariant is enforced by the schema, not by the write paths.** The unique
index `idx_sprint_tasks_order` declared above is the enforcement point. It is the
same index that serves every listing ordered by position, so making it unique adds
the constraint without adding an index: the read it already covers is unchanged, and
no second B-tree is created for the constraint alone (see `Index Design Rationale`
below).

**Why the invariant is required.** The in-sprint execution order *is*
`sprint_tasks.position`. Three published contracts read that order and none of them
can state a deterministic result unless it is total:

1. `List Sprint Tasks Ordered by Position` above orders on `st.position ASC` alone.
   With duplicate positions the statement has a tie it does not break, so two
   executions over unchanged data may return different sequences.
2. The read-only web interface's sprint board depends on the order twice over: it is
   the `WAITING` column's own order, and it is the tiebreaker of the two columns
   ordered by recency (`WEB.md § Sprint Detail Sub-Template`). A board whose column
   order is not total renders in an order nobody chose.
3. `Get Next Tasks` (`COMMANDS.md § Get Next Tasks (next)`) hands an agent the next
   piece of work to do by reading this order.

**The invariant is not left to the write paths to uphold, and the reason is the
failure mode.** An order that is total only because every insert remembers to supply
a position is a promise held by the code and by nothing else: a write path that omits
`position` gets `DEFAULT 0`, collides with the sprint's first task, and nothing
reports it — the sprint simply starts rendering in an order nobody chose, in the CLI
and on the web board alike. A silent ambiguity in stored data is worse than a failed
write, because a failed write names its cause at the moment it happens. The
constraint puts the invariant where every write path is subject to it whether or not
its author knew the invariant existed.

**No user input can name a colliding position.** Positions are always computed by
the application, never supplied by the caller:

| Command | How the position is determined |
|---------|-------------------------------|
| `sprint add-tasks` | Appended after the sprint's current `MAX(position)`, one per task |
| `sprint move-tasks` | Appended after the destination sprint's current `MAX(position)` |
| `sprint reorder` | A complete permutation of the sprint's members; index `i` in the argument becomes position `i` |
| `sprint move-to`, `sprint top`, `sprint bottom` | The caller names a target slot, and the other members shift around it, so the result is a permutation |
| `sprint swap` | The two members exchange the positions they already hold |

Every one of these produces a permutation of the sprint's positions or an append
beyond the current maximum. None of them can be handed a duplicate: `sprint reorder`
rejects a repeated task id and an incomplete list before it writes anything, and no
other command accepts a position for more than one task. **The constraint therefore
never surfaces as a caller-facing rejection.** A violation reaching the database
means a defect in a write path, not bad input, so it is reported as a database
failure (`utils.ErrDatabase`, exit code `1`) and not as a validation error. Every
check that establishes a caller's request is a permutation runs inside the same
transaction as the write it guards, so no interleaving of concurrent commands can
turn a valid request into a colliding one.

**Every write path must reach its result without a transient collision.** SQLite
checks a unique index per row as each row is written; it has no deferred constraint
check. A statement sequence that produces a legal final state can still fail partway
through if an intermediate state holds two equal positions in one sprint. Any
operation that permutes existing positions MUST therefore either

- assign in an order that provably never re-uses a position still held by another
  row of the same sprint (renumbering a sprint downwards to a dense `0..N-1` run has
  this property, because the value assigned to the *i*-th row in ascending order is
  never greater than the position that row already held), or
- **park first**: move every row it will touch to a value range disjoint from the
  one it will assign — negative positions serve, since `position = -1 - position`
  maps distinct non-negative values to distinct negative ones — and then assign the
  final positions. Parked values never escape the transaction, so no reader observes
  one.

The `Reorder Sprint Tasks (Set Exact Order)`, `Move Task to Position`, `Swap Tasks`
and `Move Task to Top/Bottom` statements below all permute existing positions and
all use the parking form for this reason. `Add Task to Sprint with Position` and
`Add Tasks to Sprint` append beyond `MAX(position)` and need no parking.

#### Position Density Within a Sprint

**The invariant.** The positions held by the member tasks of one sprint form a dense
run from zero: a sprint with *N* member tasks holds exactly the values `0, 1, ...,
N-1`, one value per task. No value inside that range is missing, and no value outside
it is held. A sprint with no member task holds no position and satisfies the invariant
trivially.

Density and the uniqueness stated above are two different properties, and together
they say something neither says alone: **the `position` stored for a member task is
that task's rank in the sprint's planned order.** Uniqueness makes the order total.
Density makes the stored number and the rank the same number.

**The schema cannot enforce density, and this is not an omission.** Uniqueness is
declared in the DDL because SQLite has a constraint form for it. Density has no such
form, for three reasons that together exhaust the options:

1. A `CHECK` constraint is evaluated over the single row being written, and its
   expression may not contain a subquery, so it cannot read the other rows of the same
   sprint. Density is a property of the whole set of rows sharing a `sprint_id` — how
   many there are and which values they hold — and no expression confined to one row
   can state it.
2. `UNIQUE` is the only constraint SQLite offers that ranges over more than one row,
   and all it can state is that two values differ. It cannot state that a collection
   of values is a complete run from zero.
3. A trigger cannot stand in for them. SQLite implements `FOR EACH ROW` triggers
   only, so a trigger would judge each row of a multi-row renumbering on its own, and
   every operation that reaches a dense final state passes through intermediate states
   that are not dense — the parked negative range described above is the clearest
   case. A trigger enforcing density would reject the correct write paths.

**Density is therefore upheld by the write paths and proved by tests, and by nothing
else.** The DDL says only that positions do not collide; it says nothing about their
being a complete run, and a reader who takes the stored data to be dense because the
schema declares it is mistaken. The whole weight of the guarantee rests on the
enumeration below, which is why a new write path that touches `position` is not
finished until it appears there with its effect on the run stated.

**Why the invariant is required.** It is not a tidiness rule. Three published
behaviours are wrong without it:

1. **The caller names a rank, and the column stores a value.** `sprint move-to` takes
   a zero-based target index as its third argument (`COMMANDS.md § Move Task to
   Position`), and `sprint bottom` derives its target from the sprint's member count.
   The stored `position` is never handed back to a caller: `List Sprint Tasks Ordered
   by Position` below orders on it without selecting it, and it is not a field of the
   `Task` object (`MODELS.md § Task`). The index the caller names and the value the
   column holds are the same number only while the run is dense.
2. **`Move Task to Position` decides whether it has work to do by comparing the moved
   task's stored position against the target rank.** Over a run with a gap those are
   two different quantities, so a real move can be read as no move at all. In a sprint
   holding the positions `0, 1, 3, 4`, the member stored at position `3` sits at rank
   2; a request to move it to index 3 compares 3 against 3, changes nothing, and still
   reports success. The task the caller asked to move stays where it was, and nothing
   in the output says so.
3. **`sprint bottom` derives its target from the sprint's member count** and hands it
   to that same comparison, so the sprint above answers `sprint bottom` on that member
   by doing nothing at all, while reporting the position it did not move the task to.

**Every write path that touches `position`.** Each one either leaves a dense run by
construction or repairs the run before its transaction commits. There is no third
category, and the table below is the complete list of the application's write paths.
The other writers of the column are the two schema migrations that renumber every
sprint to a dense `0..N-1` run, both following the sequence in `Introducing a
Uniqueness Constraint over Existing Rows` below. The first runs that repair before it
creates the unique index, and so makes the order total
(`VERSION.md § Migration 1.12.0 → 1.13.0`). The second drops that index, runs the same
repair, and recreates the index, and so closes the gaps that the removals had already
committed before they compacted (`VERSION.md § Migration 1.13.0 → 1.14.0`). Each of
them establishes the invariant over rows that already exist; neither maintains it, and
each runs once.

| Write path | What it does to `position` | Effect on the run |
|------------|---------------------------|-------------------|
| `sprint add-tasks`, for a task that belonged to no sprint | Appends one value per task after the sprint's current `MAX(position)` (`Add Tasks to Sprint` below) | **Preserves.** Over a dense run `MAX(position)` is `N-1`, so the appended values continue it |
| `sprint add-tasks`, for a task that already belonged to another sprint | The `ON CONFLICT(task_id)` clause re-parents the task's single row, which leaves the other sprint's run | **Leaves a gap**, in the sprint the task left — a sprint the command does not name. That sprint MUST be compacted in the same transaction |
| `sprint move-tasks`, destination sprint | Appends after the destination's current `MAX(position)`, preserving the relative order of the moved tasks | **Preserves**, for the same reason as `sprint add-tasks` |
| `sprint move-tasks`, source sprint | Re-parents the moved rows away from it | **Leaves a gap.** The source sprint MUST be compacted in the same transaction |
| `sprint reorder` | Parks the whole sprint and writes `0..N-1` (`Reorder Sprint Tasks (Set Exact Order)` below) | **Repairs.** The result is dense whatever the run was before |
| `sprint move-to`, `sprint top`, `sprint bottom` | Park the whole sprint and write `0..N-1` (`Move Task to Position` below) | **Repairs the run, but reads it first.** The written run is dense, yet these three decide what to write from the run they find, so over a sparse run they can decide to do nothing at all (reason 2 above). Repairing afterwards is not a substitute for the removals compacting |
| `sprint swap` | Exchanges the two values the two named tasks already hold (`Swap Tasks` below) | **Preserves.** It opens no gap and closes none |
| `sprint remove-tasks` | Deletes the named membership rows, then compacts (`Remove from Sprint` below) | **Repairs** |
| `sprint remove` | Deletes every membership row of the sprint, then the sprint itself (`Clear All Tasks from Sprint` below) | **Trivially satisfied**: neither member nor sprint remains |
| `task reopen`, from `SPRINT`, `DOING` or `TESTING` | Deletes the task's membership row (`STATE_MACHINE.md § Valid Transitions`) | **Leaves a gap.** The sprint MUST be compacted in the same transaction |
| `task reopen`, from `COMPLETED` | Keeps the membership row and the position it holds | **Preserves** |
| `task remove`, on a `BACKLOG` task that is still a sprint member | Deletes the task row; `ON DELETE CASCADE` on `task_id` takes the membership row with it | **Leaves a gap.** The sprint MUST be compacted in the same transaction |
| `task stat <ids> BACKLOG` | Does not touch the `sprint_tasks` table (`STATE_MACHINE.md § Sprint Membership and the BACKLOG Status`) | **Preserves** |

**Every path that can leave a gap is a removal, and every removal owes the same
repair.** Four entries above take a row out of a sprint's run: the re-parenting form
of `sprint add-tasks`, the source side of `sprint move-tasks`, `task reopen` from the
three sprint-associated states, and the cascade behind `task remove`. Each MUST
compact the sprint it took the row out of, inside the same transaction as the removal,
so that no committed state holds a gap and no reader ever observes one. The obligation
follows the row, not the command: only the source side of `sprint move-tasks` names the
sprint it must repair. The other three repair a sprint the caller's arguments do not
mention at all, so each of them must first read which sprint the row it is removing
belonged to.

**The obligation is not suspended for a `CLOSED` sprint.** A removal that reaches a
closed sprint has already changed that sprint's membership; leaving the survivors with
a gap preserves nothing, it only leaves the sprint in a state the invariant forbids.
The compaction renumbers the surviving members and never reorders them, so the
sprint's recorded plan is unchanged.

**The repair: `CompactSprintPositionsTx`.** This is the routine that makes the
invariant true again, and it is the only one. It renumbers a sprint's surviving
members to `0..N-1` in their current order, inside a transaction the caller has
already opened. Its two statements are `Compact Sprint Positions` below.

Four properties define it:

- **It changes values, never the sequence.** The read that feeds it is the sprint's
  own order, so the *i*-th member of that order receives position *i*. A compaction
  can never reorder a sprint; a caller that wants a different order calls `sprint
  reorder`.
- **It needs no parking step**, unlike every permuting statement described under
  `Position Uniqueness Within a Sprint` above. It renumbers downwards over an
  ascending read, and the *i*-th member in ascending order holds a position of at
  least *i*, because the *i* members ranked before it hold *i* distinct smaller
  non-negative values. Every member not yet written therefore still holds a value
  strictly greater than *i*, and every member already written holds one strictly
  below *i*, so no assignment can land on a value another row of the same sprint still
  holds.
- **It runs inside the caller's transaction**, so the removal and its repair commit
  together. A separate transaction would leave a window in which the removal is
  durable and the gap is visible.
- **It is idempotent.** Run against a sprint that is already dense, it assigns every
  member the value it already holds. A write path may therefore call it
  unconditionally rather than first testing whether a gap exists.

**What a test must show.** The guarantee lives in the write paths, so the tests are
where it is verified:

- For every entry of the table above, the sprint's positions read back as a dense
  `0..N-1` run once the operation's transaction has committed.
- The removal of a task from the **middle** of a sprint leaves a dense run, not only
  the removal of the last one. A test that removes the last member proves nothing:
  removing the last member of a dense run leaves a dense run whether or not anything
  compacted it.
- `Move Task to Position` reaches the correct order over a run that starts at zero and
  has no gap, for a move up, a move down, a move to the first slot, a move to the last
  slot, and a target beyond the member count.
- A regression test fails if any write path leaves a sprint with a gap. A gap planted
  by hand is a legitimate fixture only for the repair itself: it proves
  `CompactSprintPositionsTx` closes the gap, and it must never be used to argue that a
  sparse run is an acceptable resting state.

### `audit` Table

Logs all operations that change task or sprint state, enabling complete audit history.

```sql
CREATE TABLE IF NOT EXISTS audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation TEXT NOT NULL,
    entity_type TEXT NOT NULL CHECK(entity_type IN ('TASK', 'SPRINT')),
    entity_id INTEGER NOT NULL,
    related_entity_id INTEGER CHECK(related_entity_id IS NULL OR related_entity_id > 0),   -- Counterpart entity of the operation that produced the row; NULL when it has no counterpart
    commit_hash TEXT CHECK(commit_hash IS NULL OR (length(commit_hash) BETWEEN 7 AND 64 AND commit_hash NOT GLOB '*[^0-9a-f]*')),   -- Git commit bracketing the work; NULL on every operation but two
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
- `operation`: Operation type (for example `TASK_STATUS_DOING`, `SPRINT_START`). Values validated by application.
- `entity_type`: `'TASK'` or `'SPRINT'`. Values validated by application and enforced by the column `CHECK`.
- `entity_id`: Identifier of the entity whose history the row belongs to. Always a positive task id or sprint id.
- `related_entity_id`: Identifier of the counterpart entity of the operation that produced the row, or NULL when that operation has no counterpart. See `The Two Entities of a Relational Operation` below.
- `commit_hash`: Git commit hash bracketing a task's development work, or NULL. See `The Commit Hash of an Audit Entry` below.
- `performed_at`: Operation timestamp

**No index is created on `related_entity_id` or on `commit_hash`.** Neither column
is a filter, a sort key, or a join key for any statement in `Main SQL Queries`
below: the audit read statement's predicates are `operation`, `entity_type`,
`entity_id`, and `performed_at` only, and both new columns are read as part of the
returned entry rather than searched. An index on either would cost write time on
every audited operation and buy nothing.

**Valid values (validated by application):** This section is the canonical catalogue of audit operations. All other SPEC files referencing audit operations MUST link here rather than re-listing.

**Tasks:**
- `TASK_CREATE` - New task created via `task create`
- `TASK_DELETE` - Task deleted via `task remove` (only allowed while in BACKLOG; see Delete Task precondition)
- `TASK_STATUS_BACKLOG` - Task entered `BACKLOG`. Written by `task stat <ids> BACKLOG` and by `sprint remove-tasks`, one row per task in either case. From `sprint remove-tasks` the row names the sprint the task left in `related_entity_id`; from `task stat` no sprint is party to the operation and `related_entity_id` is NULL
- `TASK_STATUS_SPRINT` - Task entered `SPRINT`. Written by `sprint add-tasks` only, one row per task, naming the sprint the task entered in `related_entity_id`; `task stat` cannot set `SPRINT`, so no other command writes this operation and every row of it names a sprint
- `TASK_STATUS_DOING` - Task entered `DOING` via `task stat`, one row per task. The row carries the `commit_hash` supplied as `--commit-open`
- `TASK_STATUS_TESTING` - Task entered `TESTING` via `task stat`, one row per task
- `TASK_STATUS_COMPLETED` - Task entered `COMPLETED` via `task stat`, one row per task. The row carries the `commit_hash` supplied as `--commit-close`
- `TASK_REOPEN` - Task returned to BACKLOG via `task reopen`; lifecycle timestamps, completion_summary, and commit_close cleared, commit_open preserved. The sprint_tasks row is removed only when the source state is SPRINT, DOING, or TESTING; from COMPLETED the row is kept. `task reopen` writes this operation alone and writes no `TASK_STATUS_BACKLOG` row
- `TASK_TITLE_CHANGE` - `title` supplied to `task edit`
- `TASK_TYPE_CHANGE` - `type` supplied to `task edit`
- `TASK_FUNCTIONAL_REQUIREMENTS_CHANGE` - `functional_requirements` supplied to `task edit`
- `TASK_TECHNICAL_REQUIREMENTS_CHANGE` - `technical_requirements` supplied to `task edit`
- `TASK_ACCEPTANCE_CRITERIA_CHANGE` - `acceptance_criteria` supplied to `task edit`
- `TASK_PRIORITY_CHANGE` - Priority change (0-9) via `task prio` or via `task edit`
- `TASK_SEVERITY_CHANGE` - Severity change (0-9) via `task sev` or via `task edit`
- `TASK_ADD_DEP` - Dependency added via `task add-dep`; two rows are written, one against each task of the pair, and each row names the other task in `related_entity_id`
- `TASK_REMOVE_DEP` - Dependency removed via `task remove-dep`; two rows are written, one against each task of the pair, and each row names the other task in `related_entity_id`
- `TASK_COMMENT_CREATE` - Comment added to a task via `task comment-add` (logged against the parent task)
- `TASK_COMMENT_UPDATE` - Comment edited via `task comment-edit` (logged against the parent task)
- `TASK_COMMENT_DELETE` - Comment deleted via `task comment-remove` (logged against the parent task)

**Sprints:**
- `SPRINT_CREATE` - New sprint created
- `SPRINT_DELETE` - Sprint deleted
- `SPRINT_START` - Sprint started (PENDING to OPEN)
- `SPRINT_CLOSE` - Sprint closed (OPEN to CLOSED)
- `SPRINT_REOPEN` - Sprint reopened (CLOSED to OPEN)
- `SPRINT_TITLE_CHANGE` - `title` supplied to `sprint update`
- `SPRINT_DESCRIPTION_CHANGE` - `description` supplied to `sprint update`
- `SPRINT_MAX_TASKS_CHANGE` - `max_tasks` supplied to `sprint update`
- `SPRINT_ORDER_CHANGE` - `order_index` supplied to `sprint update`
- `SPRINT_ADD_TASK` - Task added to a sprint via `sprint add-tasks`; one row per task, against the sprint, naming the task in `related_entity_id`
- `SPRINT_REMOVE_TASK` - Task removed from a sprint via `sprint remove-tasks`; one row per task, against the sprint, naming the task in `related_entity_id`
- `SPRINT_MOVE_TASK_OUT` - Task moved out of the source sprint via `sprint move-tasks`; one row per task, against the source sprint, naming the task in `related_entity_id`
- `SPRINT_MOVE_TASK_IN` - Task moved into the destination sprint via `sprint move-tasks`; one row per task, against the destination sprint, naming the task in `related_entity_id`
- `SPRINT_REORDER_TASKS` - Sprint tasks reordered (set exact order)
- `SPRINT_TASK_MOVE_POSITION` - Single task moved to specific position
- `SPRINT_TASK_SWAP` - Two tasks swapped positions
- `SPRINT_COMMENT_CREATE` - Comment added to a sprint via `sprint comment-add` (logged against the parent sprint)
- `SPRINT_COMMENT_UPDATE` - Comment edited via `sprint comment-edit` (logged against the parent sprint)
- `SPRINT_COMMENT_DELETE` - Comment deleted via `sprint comment-remove` (logged against the parent sprint)

**Legacy (readable, never written):**

The four operations below are never written by any command. They exist only on rows
written before the operation catalogue was refined, and they stay in the catalogue so
that those rows remain reachable by name: each is accepted as an
`audit list --operation` filter value and each is counted under its own key by
`audit stats`. An implementation MUST NOT write any of them, and MUST NOT remove them
from the valid set.

- `TASK_STATUS_CHANGE` - LEGACY. The single status-change operation the five `TASK_STATUS_*` operations above replace. It survives on rows the 1.11.0 to 1.12.0 migration could not reclassify (see `VERSION.md § Migration 1.11.0 to 1.12.0`)
- `TASK_UPDATE` - LEGACY. The generic `task edit` operation the five per-field `TASK_*_CHANGE` operations above replace. The migration reclassifies no row carrying it, because a field edit leaves no trace of which field it touched
- `SPRINT_UPDATE` - LEGACY. The generic `sprint update` operation the four per-field `SPRINT_*_CHANGE` operations above replace. The migration reclassifies no row carrying it, for the same reason
- `SPRINT_MOVE_TASK` - LEGACY. The single move operation `SPRINT_MOVE_TASK_OUT` and `SPRINT_MOVE_TASK_IN` replace. The migration reclassifies no row carrying it, because such a row names neither the task that moved nor the sprint it came from

**Note:** Read operations (GET, STATS, LIST_TASKS) are NOT logged to audit as they do not modify state.

#### The Catalogue Entry Is Also the Published Contract Description

Each entry above is read by two consumers, not one. A human reads it here. An AI
agent reads the same words on the machine-readable contract, because the
`AuditOperation` enum that `rmp --ai-help` publishes carries the catalogue entry's
own text as the `description` of every value (see
`DATA_FORMATS.md § enums map entry`). This section is the single source of that
text. The contract holds a transcription of it and never a second wording.

**Editing an entry above edits the published contract.** This is the consequence a
writer has to know before touching the list, and it is the reason the coupling is
stated here rather than only where the contract is specified. The change does not
stay in this document: it alters what every agent reading `rmp --ai-help` is told
the operation means. A test re-derives every description from this section and
compares it byte for byte against the transcription the binary ships, so an edit
made here and nowhere else fails the test suite instead of shipping a
contradiction between the two surfaces.

**The transcription is mechanical.** `DATA_FORMATS.md § enums map entry` states the
derivation the contract applies to an entry's text, and that derivation is the only
licence to alter it. Nothing is reworded, shortened, or expanded on the way.

**An entry is read once with this document around it and once without it.** A
reference that resolves only here — the name of another section, or a word such as
"above" that depends on where the entry sits in this list — travels to the contract
intact and points at nothing for the agent reading it there. Four entries do this
today, and they remain correct catalogue entries: each carries its operative fact
in full, and what fails to travel is the pointer beside the fact rather than the
fact itself. A writer adding or editing an entry should know that wording which
stands on its own serves both readers, while wording that leans on this page serves
only one.

**Acceptance criteria:**

1. Every description published on the `AuditOperation` enum of `rmp --ai-help` is
   the text of the corresponding entry above, altered only by the derivation
   `DATA_FORMATS.md § enums map entry` states.
2. Editing the text of an entry above, without updating the transcription the
   binary ships, fails the test suite.
3. No operation's contract description is written independently of the entry above.
   There is no second wording to keep in step.

#### One Row per Thing That Happened

The catalogue above rests on two rules that together decide how many rows an
operation writes and what each of them says. Both are requirements on every command
that writes to the `audit` table.

1. **An operation names its destination, not merely its kind.** A status change
   writes the operation of the state the task entered, so a reader learns the
   outcome from the operation value alone and never has to correlate the row with
   the task's current state. The same rule governs field edits: `task edit` and
   `sprint update` write the operation of the field, not a generic update
   operation.
2. **Every entity an operation touches gets a row of its own.** An operation that
   changes two entities writes one row against each, so neither entity's history is
   silent about it. `sprint add-tasks`, `sprint remove-tasks`, `task add-dep`, and
   `task remove-dep` each write rows against both sides; `sprint move-tasks` writes
   one row against the source sprint and one against the destination sprint.

**Acceptance criteria:**

1. `rmp audit history TASK <id>` on a task that has passed through the full lifecycle returns rows whose `operation` values name the states entered, and returns no `TASK_STATUS_CHANGE` row for any transition performed at schema 1.12.0 or later.
2. `rmp task stat -r <name> <a>,<b> TESTING` writes exactly two audit rows, one `TASK_STATUS_TESTING` row against `<a>` and one against `<b>`.
3. `rmp task prio -r <name> <id> 5` and `rmp task edit -r <name> <id> -p 5` both write a `TASK_PRIORITY_CHANGE` row; neither writes `TASK_UPDATE`.
4. No command writes any of the four LEGACY operations: after any sequence of `rmp` invocations against a database created at schema 1.12.0, `SELECT COUNT(*) FROM audit WHERE operation IN ('TASK_STATUS_CHANGE', 'TASK_UPDATE', 'SPRINT_UPDATE', 'SPRINT_MOVE_TASK')` returns 0.
5. Each of the four LEGACY operations is accepted as an `audit list --operation` value and exits 0.

#### The Two Entities of a Relational Operation

**The governing rule.** `entity_type` and `entity_id` name **the entity whose
history the row belongs to**. `related_entity_id` names **the counterpart entity of
the operation that produced the row**, and is NULL when that operation has no
counterpart. That single rule decides the column's value everywhere; the table below
applies it and adds nothing to it.

The rule is what makes two rows of the same operation distinguishable. Without it,
every `SPRINT_ADD_TASK` row of a sprint reads identically and none of them says which
task was added, and every `TASK_STATUS_SPRINT` row of a task says the task joined a
sprint without saying which one.

`related_entity_id` is non-NULL exactly in the eight cases below, and NULL for every
other combination of operation and producing command in the catalogue:

| Operation | Written by | `entity_type` / `entity_id` | `related_entity_id` |
|---|---|---|---|
| `SPRINT_ADD_TASK` | `sprint add-tasks` | `SPRINT` / the sprint the task was added to | the task added |
| `TASK_STATUS_SPRINT` | `sprint add-tasks` | `TASK` / the task added | the sprint the task entered |
| `SPRINT_REMOVE_TASK` | `sprint remove-tasks` | `SPRINT` / the sprint the task was removed from | the task removed |
| `TASK_STATUS_BACKLOG` | `sprint remove-tasks` | `TASK` / the task removed | the sprint the task left |
| `SPRINT_MOVE_TASK_OUT` | `sprint move-tasks` | `SPRINT` / the source sprint | the task moved |
| `SPRINT_MOVE_TASK_IN` | `sprint move-tasks` | `SPRINT` / the destination sprint | the task moved |
| `TASK_ADD_DEP` | `task add-dep` | `TASK` / one task of the pair | the other task of the pair |
| `TASK_REMOVE_DEP` | `task remove-dep` | `TASK` / one task of the pair | the other task of the pair |

**The two rows of a membership change mirror each other.** Adding task 42 to sprint 1
writes a `SPRINT_ADD_TASK` row with `entity_id = 1, related_entity_id = 42` and a
`TASK_STATUS_SPRINT` row with `entity_id = 42, related_entity_id = 1`. The two carry
transposed ids and one shared `performed_at`, so each side of the operation is
complete on its own: `audit history SPRINT 1` says which task was added, and
`audit history TASK 42` says which sprint the task entered. Neither reader has to
consult the other entity's history to learn the counterpart. `sprint remove-tasks`
writes the same mirrored pair with `SPRINT_REMOVE_TASK` and `TASK_STATUS_BACKLOG`.

**One operation value, two producing commands, one rule.**
`TASK_STATUS_BACKLOG` is written by `task stat <ids> BACKLOG` and by
`sprint remove-tasks`, and it carries a `related_entity_id` only from the second.
This is the governing rule applied consistently, not a per-command exception: a
removal from a sprint has the sprint as its counterpart, while `task stat` changes a
task's status with no second entity party to the operation, so there is no
counterpart to name. The field therefore means one thing everywhere — the
counterpart, when the operation has one — and a reader never has to know which
command wrote a row in order to interpret the column. A NULL says "this operation had
no counterpart", never "this operation had one and it was not recorded".

`TASK_STATUS_SPRINT` has only one producing command, `sprint add-tasks`, so every row
carrying that operation names a sprint. No version of Groadmap has ever written a
`TASK_STATUS_SPRINT` row, and the `1.11.0` to `1.12.0` migration never produces one
(it reclassifies only to `TASK_STATUS_DOING`, `TASK_STATUS_TESTING`, and
`TASK_STATUS_COMPLETED`), so the invariant holds for migrated databases as well as
fresh ones.

**A dependency writes two rows and each states its own direction.**
`task add-dep <task-id> <dep-id>` writes one row against `<task-id>` naming
`<dep-id>`, and one row against `<dep-id>` naming `<task-id>`. `task remove-dep`
writes the same pair. Reading either task's history therefore shows which dependency
the entry concerns, and the two rows of one invocation are distinguished from the two
rows of any other invocation.

**`sprint move-tasks` writes no `TASK_STATUS_*` row at all,** because moving a task
between sprints preserves its status (see `COMMANDS.md § Task Assignment`). The two
sprint rows are the whole record of the move.

**`sprint remove` writes one `SPRINT_DELETE` row and no per-task row.** The
membership rows and the member tasks' statuses are reset by the cascade the deletion
performs, and the sprint the rows would name no longer exists after the transaction
commits. `SPRINT_DELETE` therefore carries NULL in `related_entity_id`, exactly as
every other operation outside the table above does.

**Acceptance criteria:**

1. After `rmp sprint add-tasks -r <name> <sprint-id> <a>,<b>`, the audit table holds two `SPRINT_ADD_TASK` rows against `<sprint-id>` whose `related_entity_id` values are `<a>` and `<b>`, and two `TASK_STATUS_SPRINT` rows, one against `<a>` and one against `<b>`, **both with `related_entity_id = <sprint-id>`**.
2. After `rmp sprint remove-tasks -r <name> <sprint-id> <a>`, the audit table holds one `SPRINT_REMOVE_TASK` row against `<sprint-id>` with `related_entity_id = <a>`, and one `TASK_STATUS_BACKLOG` row against `<a>` with `related_entity_id = <sprint-id>`.
3. After `rmp task stat -r <name> <a> BACKLOG`, the audit table holds one `TASK_STATUS_BACKLOG` row against `<a>` with `related_entity_id IS NULL`, because no sprint is party to that operation.
4. Every row written by a membership change has a mirror: for each `SPRINT_ADD_TASK` row there is a `TASK_STATUS_SPRINT` row with the two ids transposed and the same `performed_at`, and for each `SPRINT_REMOVE_TASK` row there is a `TASK_STATUS_BACKLOG` row with the two ids transposed and the same `performed_at`.
5. After `rmp sprint move-tasks -r <name> <from> <to> <a>`, the audit table holds one `SPRINT_MOVE_TASK_OUT` row against `<from>` and one `SPRINT_MOVE_TASK_IN` row against `<to>`, both with `related_entity_id = <a>`, and no `TASK_STATUS_*` row for `<a>`.
6. After `rmp task add-dep -r <name> <a> <b>`, `rmp audit history TASK <a>` shows a `TASK_ADD_DEP` row with `related_entity_id = <b>`, and `rmp audit history TASK <b>` shows a `TASK_ADD_DEP` row with `related_entity_id = <a>`.
7. `SELECT COUNT(*) FROM audit WHERE related_entity_id IS NOT NULL AND operation NOT IN ('SPRINT_ADD_TASK', 'TASK_STATUS_SPRINT', 'SPRINT_REMOVE_TASK', 'TASK_STATUS_BACKLOG', 'SPRINT_MOVE_TASK_OUT', 'SPRINT_MOVE_TASK_IN', 'TASK_ADD_DEP', 'TASK_REMOVE_DEP')` returns 0 on a database written only at schema 1.12.0 or later.
8. `SELECT COUNT(*) FROM audit WHERE operation = 'TASK_STATUS_SPRINT' AND related_entity_id IS NULL` returns 0 on **any** database, migrated or fresh, because `sprint add-tasks` is the operation's only writer and the migration never produces it.
9. An `INSERT` with `related_entity_id = 0` or a negative `related_entity_id` fails the `related_entity_id` `CHECK`; `related_entity_id IS NULL` is accepted.

#### The Commit Hash of an Audit Entry

`commit_hash` records the git commit that brackets a task's development work. It is
written on exactly two operations and is NULL on every other operation in the
catalogue:

| Operation | Value written |
|---|---|
| `TASK_STATUS_DOING` | the commit the development work started from — the value supplied as `--commit-open` |
| `TASK_STATUS_COMPLETED` | the commit that concluded the task — the value supplied as `--commit-close` |

The value is the same one the transition writes to `tasks.commit_open` or
`tasks.commit_close`, already normalised to lowercase, and it takes the same format
constraint (see `Commit Hash Format Constraint` below). No new CLI flag exists: the
value already reaches the transition, and the audit write takes it from there. Both
flags are mandatory on the transitions that write them (see
`COMMANDS.md § Change Status (stat)`), so a `TASK_STATUS_DOING` or
`TASK_STATUS_COMPLETED` row written at schema 1.12.0 or later always carries a
non-NULL `commit_hash`.

**No command writes `commit_hash` on any other operation,** including `TASK_REOPEN`,
which clears `tasks.commit_close` on the task. An implementation MUST NOT copy a
task's stored commit values onto an unrelated audit row.

**The audit row is immutable.** No command updates or deletes an audit row, and
`task reopen` is the case that matters: it clears `tasks.commit_close` on the task
but MUST NOT alter, delete, or blank any audit row. The `TASK_STATUS_COMPLETED` row
written before the reopening keeps its `commit_hash`, so the historical record of the
commit that once concluded the task survives a reopening even though the task itself
no longer carries it. Re-completing the task later adds a second
`TASK_STATUS_COMPLETED` row with the new hash rather than replacing the first. The
same holds for a re-entry into `DOING`, which replaces `tasks.commit_open` on the
task and adds a second `TASK_STATUS_DOING` row without touching the first.

**Acceptance criteria:**

1. After `rmp task stat -r <name> <id> DOING --commit-open <hash>`, the newest audit row for that task has `operation = 'TASK_STATUS_DOING'` and `commit_hash` equal to `<hash>` in lowercase, whatever case `<hash>` was supplied in.
2. After `rmp task stat -r <name> <id> COMPLETED --commit-close <hash>`, the newest audit row for that task has `operation = 'TASK_STATUS_COMPLETED'` and `commit_hash` equal to `<hash>` in lowercase.
3. `SELECT COUNT(*) FROM audit WHERE commit_hash IS NOT NULL AND operation NOT IN ('TASK_STATUS_DOING', 'TASK_STATUS_COMPLETED')` returns 0.
4. After `rmp task reopen -r <name> <id>` on a task that had been completed, the earlier `TASK_STATUS_COMPLETED` row still exists, still carries the same `id`, and still carries its `commit_hash`, while `tasks.commit_close` for that task is NULL.
5. Completing the same task a second time leaves two `TASK_STATUS_COMPLETED` rows for it, carrying the two different hashes.
6. An `INSERT` whose `commit_hash` is not NULL and is outside 7 to 64 lowercase hexadecimal characters fails the `commit_hash` `CHECK`.

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

**Legacy is not the same as uncatalogued.** The four LEGACY operations above are in
the valid set and are reachable by name; `TASK_ASSIGN` and `TASK_UNASSIGN` are not in
the valid set and are not reachable by name. Both kinds of row are retained and both
are returned by an unfiltered read. The only difference is whether an `--operation`
filter accepts the name.

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

-- Insert metadata on creation. The two values that vary are supplied by the
-- application, in the placeholder notation used throughout Main SQL Queries below.
INSERT INTO _metadata (key, value) VALUES
    ('schema_version', ?),
    ('created_at', ?),
    ('application', 'Groadmap');
```

**The stored `schema_version` is the `SchemaVersion` constant**, which is defined in
`internal/db/schema.go` and named in `VERSION.md § Current Schema Version`. No literal
version is written here, because a literal would be falsified by the next migration: a
database records the schema version of the binary that created it, and every migration
applied afterwards advances the stored value (`VERSION.md § Migrations`). `created_at`
receives the creation instant in ISO 8601 UTC, and `application` is always the literal
`Groadmap`.

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
       t.closed_at, t.completion_summary, t.commit_open, t.commit_close,
       t.parent_task_id, t.priority, t.severity,
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
       t.closed_at, t.completion_summary, t.commit_open, t.commit_close,
       t.parent_task_id, t.priority, t.severity,
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

**This is the only statement in the application that inserts a `sprint_tasks` row.**
Its batch form is stated in full under `Add Tasks to Sprint` below; the two sections
describe one statement, not two, and every `sprint_tasks` row the application creates
comes from it. Any other insert would be a new write path and would have to be
specified here before it is written.

```sql
-- Get max position for the sprint
SELECT COALESCE(MAX(position), -1) + 1 AS next_position
FROM sprint_tasks
WHERE sprint_id = ?;

-- Insert into junction table with calculated position
INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position)
VALUES (?, ?, ?, ?)
ON CONFLICT(task_id) DO UPDATE SET
    sprint_id = excluded.sprint_id,
    added_at  = excluded.added_at,
    position  = excluded.position;

-- Update task status
UPDATE tasks SET status = 'SPRINT' WHERE id IN (?, ?, ...);
```

**`position` is never omitted from the column list.** The column carries
`DEFAULT 0`, so an insert that leaves it out places the task at position 0 and
collides with whichever task already holds it. The unique index rejects such a row
(see `Position Uniqueness Within a Sprint` above),
which turns a silently ambiguous sprint order into a failed write.

**The `ON CONFLICT` clause re-parents rather than duplicates.** `task_id` is unique
across the table, so adding a task that already belongs to some sprint updates the
one row it already has: the task moves to the named sprint and takes the appended
position. This is what keeps a task in exactly one sprint without the caller having
to remove it from the previous one first.

**Re-parenting is a removal as far as the previous sprint is concerned.** The row
leaves that sprint's run and opens a hole in it, even though the command names only
the destination. The previous sprint MUST therefore be compacted in the same
transaction (`Compact Sprint Positions` below), one compaction per distinct sprint the
batch drew a task out of. See `Position Density Within a Sprint` above.

**Use case:** New tasks are added to the end of the sprint task list (highest position).

#### Update Status

Date tracking fields are automatically managed by the application based on state transitions:

```sql
-- When transitioning to DOING: set started_at and commit_open. The caller always
-- supplies commit_open on this transition, already normalised to lowercase, so the
-- statement never writes NULL to that column here. A second entry into DOING (from
-- TESTING) runs the same statement and overwrites the earlier value.
UPDATE tasks
SET status = 'DOING', started_at = ?, commit_open = ?
WHERE id = ?;

-- When transitioning to TESTING: set tested_at. Neither commit column changes.
UPDATE tasks
SET status = 'TESTING', tested_at = ?
WHERE id = ?;

-- When transitioning to COMPLETED: set closed_at, completion_summary, and
-- commit_close. completion_summary is optional and becomes NULL when --summary is
-- absent; commit_close is mandatory and is always a value, never NULL.
UPDATE tasks
SET status = 'COMPLETED', closed_at = ?, completion_summary = ?, commit_close = ?
WHERE id = ?;

-- Returning a task to BACKLOG: clear the tracking dates, the completion summary,
-- and commit_close, and PRESERVE commit_open. The same statement serves
-- `task stat <ids> BACKLOG` (accepted from SPRINT and COMPLETED only) and
-- `task reopen` (accepted from any non-BACKLOG state). commit_open is deliberately
-- absent from the SET list: the commit the work started from stays a true
-- historical fact, while the commit it was concluded at is invalidated by the
-- reopening. Neither command writes to sprint_tasks; see
-- STATE_MACHINE.md § Sprint Membership and the BACKLOG Status.
UPDATE tasks
SET status = 'BACKLOG', started_at = NULL, tested_at = NULL, closed_at = NULL,
    completion_summary = NULL, commit_close = NULL
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

#### Remove from Sprint

```sql
-- Remove from junction table, scoped to the named sprint
DELETE FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?;

-- Reset the task, whatever its status was inside the sprint. commit_close is
-- cleared with the tracking dates; commit_open is preserved (see Update Status above).
UPDATE tasks
SET status = 'BACKLOG', started_at = NULL, tested_at = NULL, closed_at = NULL,
    completion_summary = NULL, commit_close = NULL
WHERE id = ?;

-- Then close the gap the DELETE opened: Compact Sprint Positions below, in this
-- same transaction.
```

**The removal is not complete until the sprint is compacted.** Deleting a member from
the middle of a sprint leaves the survivors holding a run with a hole in it, and a
hole makes a stored position stop meaning the rank the caller names (see `Position
Density Within a Sprint` above). The compaction runs in the same transaction as the
`DELETE`, so no committed state and no reader ever sees the hole.

#### Clear All Tasks from Sprint

The status reset MUST run before the membership rows are deleted; once the
`sprint_tasks` rows are gone the subquery selects nothing.

```sql
-- Reset every member task, whatever its status was inside the sprint. commit_close
-- is cleared with the tracking dates; commit_open is preserved (see Update Status above).
UPDATE tasks
SET status = 'BACKLOG', started_at = NULL, tested_at = NULL, closed_at = NULL,
    completion_summary = NULL, commit_close = NULL
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

**Over a dense run this is the member count minus one.** Appending at `MAX(position) + 1`
therefore continues the run rather than jumping past a hole, which is why every appending
write path preserves density without doing anything about it (see `Position Density
Within a Sprint` above).

#### Compact Sprint Positions

Renumbers a sprint's members to a dense `0..N-1` run in their current order, closing any
gap a removal opened. Every write path that takes a membership row out of a sprint runs
it, in the same transaction as the removal (see `Position Density Within a Sprint` above,
which owns the invariant, the list of write paths that owe this repair, and the four
properties of the routine).

```sql
-- 1. Read the sprint's surviving members in their current order.
SELECT task_id, position FROM sprint_tasks WHERE sprint_id = ? ORDER BY position ASC, task_id ASC;

-- 2. Assign the dense run. For the member at index i of that sequence:
UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?;
```

**This sequence needs no parking step**, and it is the only position-writing sequence in
this document that does not. It renumbers downwards over an ascending read, so the value
it assigns to a member is never greater than the value that member already holds, and no
assignment can land on a value another row of the same sprint still holds. The argument in
full is under `Position Density Within a Sprint` above; the parking rule it is the
exception to is under `Position Uniqueness Within a Sprint`.

#### Reorder Sprint Tasks (Set Exact Order)

Updates positions for all tasks in a sprint based on a provided ordered list of task IDs.

```sql
-- Transaction:
-- 1. Park every member of the sprint in the negative range, so that no value the
--    assignment below writes is still held by another row of the same sprint.
UPDATE sprint_tasks SET position = -1 - position WHERE sprint_id = ?;

-- 2. Assign the final positions. For each task ID in the ordered list at index i:
UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?;
```

**The parking step is required, not an optimisation.** A reorder is a permutation, so
assigning the final positions directly makes the first task of the new order claim a
position another task still holds, and the unique index rejects the statement (see
`Position Uniqueness Within a Sprint` above). The
negative range works because `-1 - position` maps distinct non-negative positions to
distinct negative ones, so the parked state satisfies the constraint too, and the
whole sequence runs in one transaction, so no reader ever observes a negative
position.

**Validation:** All task IDs in the ordered list must belong to the sprint, the list
must contain no duplicate, and it must name every member of the sprint. The
membership, duplicate and completeness checks MUST all run inside the same
transaction as the two statements above. Checking completeness in an earlier, separate
read leaves a window in which another process adds a task to the sprint: the list is
then complete when it is read and partial when it is applied, and a partial
assignment leaves the omitted tasks holding positions the reorder also assigns.

#### Move Task to Position

Moves a single task to a specific position, updating positions of other tasks accordingly.

```sql
-- Transaction:
-- 1. Get current position of the task
SELECT position FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?;

-- 2. Park every member of the sprint in the negative range.
UPDATE sprint_tasks SET position = -1 - position WHERE sprint_id = ?;

-- 3. Assign the final positions: the members in their previous order, with the moved
--    task lifted out and re-inserted at the target slot. For each task at index i of
--    that sequence:
UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?;
```

**A range shift cannot express this move.** The shift form —
`UPDATE ... SET position = position + 1 WHERE position >= ? AND position < ?` — walks
a contiguous run of rows and moves each onto the value its neighbour still holds, so
the unique index rejects it on the first row, in both directions. Parking the sprint
and then writing the resulting permutation produces the same final state and never
presents a duplicate.

**Validation:** The target position must be an integer between 0 and 2147483647 (MaxInt32) inclusive. A value less than 0 or greater than 2147483647 is rejected as a validation error.

**Behavior:**
- Moving to position 0 places the task at the beginning
- Moving to a position >= task count places the task at the end
- Positions of other tasks are automatically adjusted to maintain continuity

**The target is a rank, and the statement compares it against a stored value.** The
caller names a zero-based index into the sprint's planned order; step 1 above reads
the value the column holds. The two are the same number only while the sprint's run is
dense, and the comparison that decides this statement has nothing to do — the move to
the position the task already occupies — is a comparison between them. Over a run with
a hole that comparison can call a real move a no-op, so this statement depends on
`Position Density Within a Sprint` above and does not establish it for itself.

#### Swap Tasks

Swaps positions between two tasks in the same sprint.

```sql
-- Transaction:
-- 1. Get positions of both tasks
SELECT task_id, position FROM sprint_tasks WHERE sprint_id = ? AND task_id IN (?, ?);

-- 2. Park the first task, so the second can take the position it is vacating.
UPDATE sprint_tasks SET position = -1 - position WHERE sprint_id = ? AND task_id = ?;

-- 3. Move the second task into the first task's old position.
UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?;

-- 4. Move the first task out of the parked range into the second task's old position.
UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?;
```

**Only one row needs parking here.** A swap touches exactly two rows, and once the
first has left its position the second can take it, so the three statements above are
the cheapest form that never presents a duplicate. Writing the two positions directly
fails on the first statement, because the position it assigns is the one the other
task still holds.

#### Move Task to Top/Bottom

```sql
-- Move to top (position 0)
-- Transaction: same logic as Move Task to Position with target position 0

-- Move to bottom (last position)
-- Get current max position, then use Move Task to Position logic
```

Both reuse `Move Task to Position` above in full, parking step included.

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

The batch form of `Add Task to Sprint with Position` above, and the only statement
that inserts a `sprint_tasks` row. One multi-row `INSERT` covers the whole batch, so
adding *n* tasks costs one round trip rather than *n*.

```sql
-- Get max position for the sprint
SELECT COALESCE(MAX(position), -1) AS max_position FROM sprint_tasks WHERE sprint_id = ?;

-- Insert into junction table with incremental positions, one row group per task
INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position)
VALUES (?, ?, ?, ?), (?, ?, ?, ?), ...
ON CONFLICT(task_id) DO UPDATE SET
    sprint_id = excluded.sprint_id,
    added_at  = excluded.added_at,
    position  = excluded.position;

-- Update task status
UPDATE tasks SET status = 'SPRINT' WHERE id IN (?, ?, ...);
```

**Note:** Tasks are added with positions starting from max_position + 1, ensuring they appear at the end of the sprint task list. An empty sprint yields -1, so its first task takes position 0.

**The batch needs no parking.** Every position it writes is strictly greater than the
sprint's current maximum, and the values within one batch are consecutive, so no
assigned value is ever held by another row at the moment it is written.

**The batch preserves the destination's density and can break another sprint's.** The
appended values continue the destination's run, because over a dense run
`MAX(position)` is the member count minus one. Any task in the batch that already
belonged to a different sprint is re-parented out of that sprint by the `ON CONFLICT`
clause, which leaves a hole there; every such sprint MUST be compacted in this same
transaction (`Compact Sprint Positions` above).

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

-- Reset every member task, whatever its status was inside the sprint. commit_close
-- is cleared with the tracking dates; commit_open is preserved (see Update Status above).
-- Note: in implementation, do this before deleting sprint
UPDATE tasks
SET status = 'BACKLOG', started_at = NULL, tested_at = NULL, closed_at = NULL,
    completion_summary = NULL, commit_close = NULL
WHERE id IN (
    SELECT task_id FROM sprint_tasks WHERE sprint_id = ?
);

-- Then remove relationships
DELETE FROM sprint_tasks WHERE sprint_id = ?;

-- Finally remove sprint
DELETE FROM sprints WHERE id = ?;
```

#### Read the Membership of Many Sprints (Grouped)

Returns the member task ids of each sprint of a given set, in one round trip, so that a caller can walk the result once and index it by sprint.

```sql
-- Membership of several sprints at once. The IN list is built from the same
-- number of placeholders as ids, never by string concatenation.
SELECT sprint_id, task_id
FROM sprint_tasks
WHERE sprint_id IN (?, ?, ...)
ORDER BY sprint_id ASC, task_id ASC;
```

**What it answers.** A `Sprint` object carries two fields that are not columns of `sprints` and are computed on every read: `tasks`, the ids of its member tasks, and `task_count`, how many there are (`MODELS.md § Sprint`). The sprint listing returns both fields populated for every sprint it returns (`COMMANDS.md § List Sprints`), and this statement is where their values come from.

**Bounded query count.** The listing costs a bounded number of queries that does not grow with the number of sprints: one read of `sprints` for the sprint rows themselves, then this **one** grouped read over the ids those rows carry. The listing issues no query per sprint and no query per returned id. This is the same shape, adopted for the same reason, as `Count Comments for Many Parents (Grouped)` and `Resolve the Sprint of Many Tasks (Grouped)` below: a listing must not pay one round trip per row it returns.

**Counting is not a second query.** `task_count` is the number of ids this statement returns for that sprint. No `COUNT(*)` statement is issued to obtain it, so the count and the id list are two readings of one result and can never disagree.

**No row for a sprint without tasks.** A sprint that holds no task has no `sprint_tasks` row, so the result carries no entry for that sprint id. The absence of an entry is the answer: the caller reads that sprint's membership as the empty set and reports `tasks` as `[]` and `task_count` as `0` — never `null`, and never a placeholder row (see `DATA_FORMATS.md § Implementation Notes`, Empty arrays).

**Reads no task row.** The statement reads `sprint_tasks` alone and joins nothing. The answer is a set of ids per sprint, so no `tasks` row is fetched to produce it, exactly as in the ids-alone read of `List by Sprint` above.

**Membership is not status.** The statement applies no predicate on task status, because membership is a `sprint_tasks` row and status is a `tasks` column. A member task in `BACKLOG` status is therefore included, and both computed fields count it (see `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status`).

**Empty id set.** When the id set is empty, the application skips the query entirely instead of issuing a statement with an empty `IN` list, as every grouped read that takes a set of ids does.

**Ordering.** `sprint_id` ascending groups the rows of one sprint together, so the result is walkable in a single pass; `task_id` ascending fixes the order of the ids inside each sprint, and that is the order the `tasks` field publishes (`MODELS.md § Sprint Field Constraints`). The ordering is fixed by the statement, so it is a property of the read and not an accident of how the rows happen to be stored. Neither column is the sprint's planned execution order: that order is `sprint_tasks.position`, read through the sprint task listings in `List by Sprint` above.

**Index.** Served by `idx_sprint_tasks_lookup`, whose columns are exactly `(sprint_id, task_id)`: the leading column serves the `IN` lookup and the pair serves the ordering, so the statement needs no sort step and reads no table row, and the query plan reports a covering index search. The composite primary key of `sprint_tasks` covers the same two columns in the same order. No index is added for this query. See Performance Optimization below.

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

Every audit write is **one** statement, and it always names all six writable
columns. `related_entity_id` and `commit_hash` are bound to NULL on the operations
that do not carry them rather than being omitted from the statement, so one prepared
statement serves every operation and no operation can silently acquire a value
because a shorter statement was reused.

```sql
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES (?, ?, ?, ?, ?, ?);
```

**Examples by operation:**

```sql
-- Create task
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('TASK_CREATE', 'TASK', 42, NULL, NULL, '2026-03-12T15:00:00.000Z');

-- Task entered DOING; the row carries the commit the work started from
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('TASK_STATUS_DOING', 'TASK', 42, NULL, '5f93b51', '2026-03-12T15:30:00.000Z');

-- Task entered TESTING
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('TASK_STATUS_TESTING', 'TASK', 42, NULL, NULL, '2026-03-12T15:40:00.000Z');

-- Task entered COMPLETED; the row carries the commit that concluded it
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('TASK_STATUS_COMPLETED', 'TASK', 42, NULL, '2578d18', '2026-03-12T15:50:00.000Z');

-- Change task priority (task prio, or task edit -p)
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('TASK_PRIORITY_CHANGE', 'TASK', 42, NULL, NULL, '2026-03-12T15:45:00.000Z');

-- Change task title through task edit
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('TASK_TITLE_CHANGE', 'TASK', 42, NULL, NULL, '2026-03-12T15:46:00.000Z');

-- Add a dependency: two rows, one per task, each naming the other
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('TASK_ADD_DEP', 'TASK', 42, 43, NULL, '2026-03-12T15:55:00.000Z');
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('TASK_ADD_DEP', 'TASK', 43, 42, NULL, '2026-03-12T15:55:00.000Z');

-- Start sprint
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('SPRINT_START', 'SPRINT', 1, NULL, NULL, '2026-03-12T16:00:00.000Z');

-- Add task 42 to sprint 1: two mirrored rows, one per entity. The ids are
-- transposed and the timestamp is shared, so each side names its counterpart.
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('SPRINT_ADD_TASK', 'SPRINT', 1, 42, NULL, '2026-03-12T16:30:00.000Z');
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('TASK_STATUS_SPRINT', 'TASK', 42, 1, NULL, '2026-03-12T16:30:00.000Z');

-- Remove task 42 from sprint 1: the same mirrored pair
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('SPRINT_REMOVE_TASK', 'SPRINT', 1, 42, NULL, '2026-03-12T16:35:00.000Z');
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('TASK_STATUS_BACKLOG', 'TASK', 42, 1, NULL, '2026-03-12T16:35:00.000Z');

-- The same status operation from `task stat`, where no sprint is party to it
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('TASK_STATUS_BACKLOG', 'TASK', 42, NULL, NULL, '2026-03-12T16:36:00.000Z');

-- Move task 42 from sprint 1 to sprint 2: one row per sprint, both naming the task
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('SPRINT_MOVE_TASK_OUT', 'SPRINT', 1, 42, NULL, '2026-03-12T16:45:00.000Z');
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('SPRINT_MOVE_TASK_IN', 'SPRINT', 2, 42, NULL, '2026-03-12T16:45:00.000Z');

-- Change a sprint field through sprint update
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('SPRINT_MAX_TASKS_CHANGE', 'SPRINT', 1, NULL, NULL, '2026-03-12T16:50:00.000Z');

-- Reorder sprint tasks
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('SPRINT_REORDER_TASKS', 'SPRINT', 1, NULL, NULL, '2026-03-12T17:00:00.000Z');

-- Move task to position
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('SPRINT_TASK_MOVE_POSITION', 'SPRINT', 1, NULL, NULL, '2026-03-12T17:15:00.000Z');

-- Swap tasks
INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
VALUES ('SPRINT_TASK_SWAP', 'SPRINT', 1, NULL, NULL, '2026-03-12T17:30:00.000Z');
```

**The rows of one command share one timestamp.** When a command writes several audit
rows — a per-task row and a per-sprint row, the two rows of a dependency, or the N
rows of an N-field edit — every row it writes carries the same `performed_at` value,
because they record one operation performed at one moment inside one transaction. A
reader can therefore group the rows of a single invocation by `(performed_at)` and
`id` order.

#### Query Audit Entries

Every audit read is **one** assembled statement, not a family of statements. It names the seven columns of an audit entry, appends only the predicates the caller supplied, and always orders and bounds the result:

```sql
SELECT id, operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at
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

**The seven columns are named, never `*`.** They are the whole of `AuditEntry` (`MODELS.md § Audit Entry`), and naming them fixes the result-set order the application scans rather than leaving it to the table's current shape.

**`related_entity_id` and `commit_hash` are read, never filtered.** Both are in the select list because a caller reading an entry needs them, and neither appears among the predicates below: the audit read surface offers no filter on either column (see `COMMANDS.md § List Audit Log`). This is why neither column is indexed.

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
- No two tasks of one sprint share a `position`, so the sprint's planned execution order is total (unique index `idx_sprint_tasks_order` over `(sprint_id, position)`)
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
   writing both audit entries per task — the `SPRINT_REMOVE_TASK` entry against the
   sprint and the `TASK_STATUS_BACKLOG` entry against the task — MUST occur in the
   same transaction. No committed state
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
   `sprint_tasks` rows, updating those tasks' status to `SPRINT`, and writing both
   audit entries per task — the `SPRINT_ADD_TASK` entry against the sprint and the
   `TASK_STATUS_SPRINT` entry against the task — MUST all occur in the same
   transaction. A committed membership change can never exist without its audit
   record, and neither of the two entries can exist without the other.
5. **Moving tasks between sprints (`MoveTasksBetweenSprints`).** The source-sprint
   membership check, the re-parenting of the `sprint_tasks` rows, and writing the
   `SPRINT_MOVE_TASK_OUT` and `SPRINT_MOVE_TASK_IN` audit entries (one pair per
   task) MUST all occur in the same transaction. A committed move can never exist
   without its audit record, and the database never shows the source sprint's entry
   without the destination sprint's.
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

8. **Changing a task's status (`UpdateTaskStatus`).** The `UPDATE` on every task in
   the batch and the matching `TASK_STATUS_<TARGET>` audit entries (one per task,
   each carrying the batch's `commit_hash` when the target is `DOING` or
   `COMPLETED`) MUST occur in the same transaction. A committed status change can
   never exist without its audit record, and an audit record naming a commit can
   never exist for a transition that was rolled back.
9. **Editing fields (`UpdateTask`, `UpdateSprint`).** The single `UPDATE` that
   applies every supplied field and the N per-field audit entries MUST occur in the
   same transaction. Either the whole edit and all N entries commit, or none of them
   does; a committed edit whose audit record names only some of the fields it
   changed is forbidden.

These guarantees extend the general transactional-integrity requirement in
`ARCHITECTURE.md § Security Guarantees` (every modification, including its audit
entries, is wrapped in one transaction) to these specific operations. Where an
operation writes more than one audit entry, "its audit entry" means all of them: the
requirement is satisfied only when every entry the operation owes is written inside
the same transaction as the change itself.

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
| completion_summary | TEXT | NULLABLE, CHECK length <= 4096 chars | Tracking |
| commit_open | TEXT | NULLABLE, CHECK 7-64 lowercase hexadecimal characters | Tracking |
| commit_close | TEXT | NULLABLE, CHECK 7-64 lowercase hexadecimal characters | Tracking |
| parent_task_id | INTEGER | NULLABLE, REFERENCES tasks(id) | Tracking |
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
| position | INTEGER | NOT NULL, DEFAULT 0, position in sprint task order (0-based), unique within one sprint (`idx_sprint_tasks_order`) |

**Note:** Composite primary key `(sprint_id, task_id)` combined with the `UNIQUE` constraint on `task_id` enforces the 1:N relationship — a task can only belong to one sprint at a time. The `position` field enables sprint task ordering, with 0 being the first position.

**Note:** `position` is unique per sprint, not globally: the unique index is over the pair `(sprint_id, position)`, so two tasks in two different sprints may both hold position 0. See `DATABASE.md § Position Uniqueness Within a Sprint` for the invariant and what every write path must do to preserve it.

**Note:** the positions of one sprint are also dense — a sprint with `N` members holds exactly `0` to `N-1` — so a member's stored `position` is its rank in the sprint's planned order. No column constraint states this and none can; the write paths uphold it and tests prove it. See `DATABASE.md § Position Density Within a Sprint`.

### Audit

| Column | Type | Constraints |
|--------|------|-------------|
| id | INTEGER | PK, AUTOINCREMENT |
| operation | TEXT | NOT NULL |
| entity_type | TEXT | NOT NULL, CHECK enum values: TASK, SPRINT |
| entity_id | INTEGER | NOT NULL |
| related_entity_id | INTEGER | NULLABLE, CHECK NULL or `> 0`; non-NULL only where the producing operation has a counterpart |
| commit_hash | TEXT | NULLABLE, CHECK 7-64 lowercase hexadecimal characters; non-NULL on two operations only |
| performed_at | TEXT | NOT NULL, ISO 8601 format |

**Valid values (validated by application):**
- `operation`: See the canonical catalogue in the `audit` Table section above (Tasks + Sprints + Legacy).
- `entity_type`: TASK, SPRINT
- `related_entity_id`: See `The Two Entities of a Relational Operation` in the `audit` Table section above for the eight operation-and-command combinations that write it.
- `commit_hash`: See `The Commit Hash of an Audit Entry` in the `audit` Table section above for the two operations that write it.

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
| `idx_sprint_tasks_lookup` | sprint_tasks | (sprint_id, task_id) | Optimizes sprint task relationship lookups, and the grouped membership read of many sprints |
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
- The same index serves the grouped `WHERE sprint_id IN (...) ORDER BY sprint_id ASC, task_id ASC` read that resolves the `tasks` and `task_count` of every sprint the sprint listing returns (see `Read the Membership of Many Sprints (Grouped)` above): the leading column serves the lookup and the pair serves the ordering, so that read needs no sort step and touches no table row
- Expected improvement: 70% query time reduction for sprint operations

**idx_sprint_tasks_order:**
- Query pattern: `WHERE sprint_id = ? ORDER BY position ASC` in the `sprint_tasks` table
- Serves every listing of a sprint's tasks in the planned execution order, and supplies the `position` tiebreaker of the priority-ordered listing, so neither needs a sort step
- **The index is `UNIQUE`, and that is deliberate.** The constraint that no two tasks of one sprint share a position is enforced by this index rather than by a second one declared for the purpose. A separate unique index over `(sprint_id, position)` would be an exact duplicate of this one: same table, same columns, same order. Making the existing index unique adds the constraint at no storage cost and leaves the reads it already serves unchanged
- Also serves `SELECT MAX(position) WHERE sprint_id = ?`, the read that computes where an appended task goes

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
| `tasks.commit_open` | 64 characters (minimum 7) | `CHECK(commit_open IS NULL OR (length(commit_open) BETWEEN 7 AND 64 AND commit_open NOT GLOB '*[^0-9a-f]*'))` |
| `tasks.commit_close` | 64 characters (minimum 7) | `CHECK(commit_close IS NULL OR (length(commit_close) BETWEEN 7 AND 64 AND commit_close NOT GLOB '*[^0-9a-f]*'))` |
| `audit.commit_hash` | 64 characters (minimum 7) | `CHECK(commit_hash IS NULL OR (length(commit_hash) BETWEEN 7 AND 64 AND commit_hash NOT GLOB '*[^0-9a-f]*'))` |
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

## Commit Hash Format Constraint

Three columns in this schema store a git commit hash: `tasks.commit_open`,
`tasks.commit_close`, and `audit.commit_hash`. All three carry the same format
constraint, which is not a length constraint alone, so it is stated here once and in
full. The `CHECK` on each column is the database-level backstop for the rule the
application enforces first (see `MODELS.md § Task`, Commit Hash Constraint).

The three `CHECK` constraints are identical apart from the column name. Any future
column that stores a commit hash MUST take this same constraint rather than a
variant of it.

Each `CHECK` has three parts and all three must hold together:

1. `<column> IS NULL` — the column is nullable and NULL is always valid. A task
   that has never entered `DOING` has a NULL `commit_open`; a task that has never
   entered `COMPLETED`, or that has since returned to `BACKLOG`, has a NULL
   `commit_close`; and an audit row for any operation other than
   `TASK_STATUS_DOING` and `TASK_STATUS_COMPLETED` has a NULL `commit_hash`.
2. `length(<column>) BETWEEN 7 AND 64` — the value is at least 7 and at most 64
   characters, inclusive. Values of any length outside that range are rejected,
   including the empty string.
3. `<column> NOT GLOB '*[^0-9a-f]*'` — the value contains no character outside
   `0`-`9` and `a`-`f`. The pattern matches any value that contains at least one
   character outside that set, so its negation asserts that every character is in
   it.

**The third part is case-sensitive, and deliberately so.** SQLite's `GLOB`
operator compares case-sensitively, unlike `LIKE`. The pattern therefore rejects
`5F93B51` as firmly as it rejects `5f93b5g`, which makes the constraint a genuine
backstop for the application's lowercase normalisation rather than a restatement
of the hexadecimal alphabet alone. A value that reaches the database in any case
other than lowercase means the normalisation step was skipped, and the constraint
fails the write instead of storing an unnormalised value.

Because the constraint rejects every non-hexadecimal character, it also rejects
leading and trailing whitespace. The application does not trim these values, so a
padded value is invalid at both layers.

**One value, written to two columns.** A transition into `DOING` writes the same
normalised value to `tasks.commit_open` and to the `commit_hash` of the
`TASK_STATUS_DOING` audit row, and a transition into `COMPLETED` writes the same
normalised value to `tasks.commit_close` and to the `commit_hash` of the
`TASK_STATUS_COMPLETED` audit row. The normalisation happens once, before either
write, so the two columns can never disagree about the case or the content of the
hash. The two writes are in the same transaction (see
`Transactional Atomicity Guarantees` above).

**The two columns diverge over time, and that is intended.** `tasks.commit_close` is
cleared when the task returns to `BACKLOG`, and `tasks.commit_open` is overwritten on
a re-entry into `DOING`. No audit row is ever cleared or overwritten, so the audit
table keeps the value the task no longer holds. A NULL `tasks.commit_close` beside a
`TASK_STATUS_COMPLETED` audit row carrying a hash is therefore a correct state, not
an inconsistency.

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

**A `CHECK` constraint travels with the added column.** SQLite accepts a `CHECK` in
an `ADD COLUMN` clause, and the constraint applies to the new column from that point
on. It is not re-validated against the rows already stored, and it does not need to
be: every existing row receives NULL in the new column, and every constraint this
specification attaches to an added column accepts NULL.

**No migration in this specification rebuilds a table.** Widening or adding a
constraint on a column that already exists is not something `ALTER TABLE` can do in
SQLite; it requires creating a replacement table, copying every row into it, dropping
the original, and renaming. No migration specified here needs that, because every
constraint change this schema has made so far has been attached to a column being
added at the same time. `ALTER TABLE ... ADD COLUMN` and the guarded
`ALTER TABLE ... DROP COLUMN` below are therefore the only two schema-altering
statements the migration set uses, and both are single statements with a
column-existence guard. A future change that must alter an existing column's
constraint would need the rebuild procedure, and it would have to be specified here
before it is written.

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

## Introducing a Uniqueness Constraint over Existing Rows

A migration that adds a `UNIQUE` index to a table that already holds rows differs
from every other migration in this specification: it can fail on data, not only on
schema. `CREATE UNIQUE INDEX` validates every existing row at creation time, so a
database that already contains a duplicate cannot receive the index at all. This
section states what such a migration MUST do. It applies to any future uniqueness
constraint as well as to the one over `(sprint_id, position)`.

**No table rebuild is required.** Every uniqueness constraint in this schema is a
`CREATE UNIQUE INDEX` statement, never a table-level `UNIQUE` clause, so adding one
never triggers the rebuild procedure described in
`Migration Idempotency (ALTER TABLE ADD COLUMN)` above. `idx_one_open_sprint`,
`idx_sprints_order` and `idx_sprint_tasks_order` are all declared this way.

### The required sequence

A migration that adds a uniqueness constraint MUST perform these steps, in this
order, inside the migration's single transaction:

1. **Repair the existing rows** so that they satisfy the constraint. The repair runs
   **before** the index exists, which is what makes it possible at all: with no
   unique index in force, the intermediate states of the repair cannot violate one.
2. **Create the index.** `CREATE UNIQUE INDEX IF NOT EXISTS` for a new index; for an
   existing non-unique index that is being tightened, `DROP INDEX IF EXISTS` followed
   by `CREATE UNIQUE INDEX IF NOT EXISTS` under the same name, so that the schema
   ends with exactly one index over those columns.
3. **Fail closed if the index still cannot be created.** The migration MUST NOT
   attempt to delete, truncate, or otherwise discard rows in order to make the index
   succeed.

Running steps 1 and 2 in one transaction is what makes step 3 safe: if the index creation
fails, the whole migration is rolled back, the repair is undone with it, and the
recorded `schema_version` is not advanced. The database is left exactly as it was.

### The repair must not read its own writes

A repair that renumbers a column MUST derive each row's new value from a source the
`UPDATE` does not modify. SQLite applies an `UPDATE` row by row, and a correlated
subquery in the `SET` clause that reads the table being updated observes rows that
the same statement has already rewritten. Ranking rows by the very column being
written is therefore **wrong**: rows that have already moved are no longer counted
where they were, later rows are ranked too low, and the statement can leave
duplicates behind even though its logic is correct on paper.

The sound form ranks in a subquery that is computed as a unit and joined to the
target, which is the form the `1.0.0` → `1.1.0` migration already uses:

```sql
-- Renumber each sprint's positions to a dense 0..N-1 run, preserving the order the
-- rows already had. ROW_NUMBER() is computed over the whole table before any row is
-- written, so no row is ranked against a value this statement has already changed.
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
  AND sprint_tasks.task_id   = ranked.task_id;
```

**The ordering keys are what make the repair honest.** `position ASC` keeps the order
the roadmap's owner planned, and `task_id ASC` breaks the ties that are precisely the
rows in violation. Ranking by a different column — `added_at`, for instance — would
also produce a valid dense run, but it would silently replace the planned order with
the order the tasks happened to be added in, which is a data loss dressed up as a
repair.

**The repair is idempotent and safe on a conforming database.** On a sprint whose
positions are already a dense `0..N-1` run it assigns every row the value it already
holds. On a sprint with gaps it closes them without changing the relative order. On a
sprint with duplicates it separates them, putting the lower task id first. Running it
twice yields the same result as running it once.

### Verify before writing the migration

The data that a migration will run against MUST be measured before the migration is
written, not assumed. For a uniqueness constraint the check is a direct count of the
groups that violate it:

```sql
SELECT COUNT(*) FROM (
    SELECT sprint_id, position
    FROM sprint_tasks
    GROUP BY sprint_id, position
    HAVING COUNT(*) > 1
);
```

A count of zero establishes that the constraint holds for the database measured; it
establishes nothing about anyone else's. That is why the repair in step 1 is
mandatory rather than conditional on the measurement: a migration that fails on
another person's database is worse than the gap it closes, and the measurement can
only ever cover the databases within reach.

### The failure surface

If the index creation fails despite the repair, the failure is reported through the
ordinary migration path. The behaviour is:

| Property | Value |
|----------|-------|
| Exit code | `1` (`EXIT_FAILURE`; the migration error carries no sentinel and falls through to the general failure code, consistent with `utils.ErrDatabase`) |
| stdout | Empty. A failing invocation writes nothing to stdout (`COMMANDS.md § Failing Invocations Write Nothing to Stdout`) |
| stderr | `Error: running migrations: migration <version> failed: applying migration: <step>: <SQLite error>`, followed by a blank line and the standard AI-agent hint (`HELP.md § Error message format`) |
| Database | Unchanged. The transaction is rolled back, the repair is undone with it, and `_metadata.schema_version` keeps its previous value |

**The command that triggered the migration does not run.** Migrations are applied
when the database is opened, so a migration failure aborts the invocation that opened
it, and every later invocation against that roadmap fails the same way until the
cause is removed. This is the intended behaviour — a roadmap whose schema could not
be brought up to date must not be served by a binary that expects the newer schema —
and it is also the reason the repair exists: a migration that merely refuses leaves
the roadmap unusable, while a migration that repairs leaves it correct.

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
