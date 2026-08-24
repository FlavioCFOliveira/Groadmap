# Application Version Specification

The SPEC itself is not versioned (see `SPEC/README.md` and `CLAUDE.md` § Versioning Policy). Git tags are the canonical record of past application and schema versions.

## Versioning Strategy

### Application Version

The application version is defined in `cmd/rmp/main.go`:

```go
const version = "X.Y.Z"
```

This version is:
- Compiled into the binary at build time
- Displayed via `rmp version`, `rmp --version`, and `rmp -v`, which are three equivalent forms of the same request (`COMMANDS.md § Version`)
- Used for release artefact naming (e.g., `rmp-v1.2.1-linux-amd64.tar.gz`)

### Database Schema Version

The database schema version is managed separately via `internal/db/schema.go`:

```go
const SchemaVersion = "X.Y.Z"
```

- Used for database migrations
- Stored in database `_metadata` table
- Independent from application version

## Semantic Versioning

Groadmap follows Semantic Versioning (SemVer):

```
vMAJOR.MINOR.PATCH

Example: v1.2.1
```

### Version Components

- **MAJOR**: Incompatible API changes or major architectural changes
- **MINOR**: New functionality, backward compatible
- **PATCH**: Bug fixes, backward compatible

## Version Independence

The application version and schema version are independent:

- Application version follows release cadence and SemVer
- Schema version increments only when a migration is added in `internal/db/migrations.go`
- Schema updates can happen without application version bumps and vice versa

## Migrations

This section covers **database schema** migrations, which alter the contents and structure of a roadmap's SQLite database. They are distinct from the **filesystem layout** migration, which relocates a roadmap's database within the data directory and is specified in `ARCHITECTURE.md § Filesystem Layout Migration`. The two mechanisms are independent: a schema migration runs when a specific database is opened; the layout migration runs once at startup against the data directory.

The `_metadata` table records the active schema version. Migration steps and their descriptions live in `internal/db/migrations.go`; the migration history is recoverable via `git log internal/db/migrations.go`.

### Current Schema Version

`SchemaVersion = "1.13.0"` (defined in `internal/db/schema.go`).

### Migration Commands

```sql
-- Check current version
SELECT value FROM _metadata WHERE key = 'schema_version';

-- Update version after migration
UPDATE _metadata SET value = '1.3.0' WHERE key = 'schema_version';
```

### Migration 1.1.0 → 1.2.0

```sql
-- Enforce at most one OPEN sprint at a time
CREATE UNIQUE INDEX IF NOT EXISTS idx_one_open_sprint ON sprints(status) WHERE status = 'OPEN';

-- Update schema version
UPDATE _metadata SET value = '1.2.0' WHERE key = 'schema_version';
```

### Migration 1.2.0 → 1.3.0

```sql
-- Add completion_summary column to existing databases
ALTER TABLE tasks ADD COLUMN completion_summary TEXT CHECK(completion_summary IS NULL OR length(completion_summary) <= 4096);

-- Update schema version
UPDATE _metadata SET value = '1.3.0' WHERE key = 'schema_version';
```

### Migration 1.6.0 → 1.7.0

Adds the required `title` column to the `sprints` table and backfills every
existing sprint with a deterministic title derived from its identifier, so that
pre-existing sprints satisfy the `NOT NULL` constraint after the column is added.

```sql
-- Add the title column only when it does not already exist (see
-- DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)). When absent, run:
ALTER TABLE sprints ADD COLUMN title TEXT NOT NULL DEFAULT '' CHECK(length(title) <= 255);

-- Backfill each existing sprint with the literal title 'Sprint ' || id
-- (for example, sprint 5 becomes "Sprint 5")
UPDATE sprints SET title = 'Sprint ' || id;

-- Update schema version
UPDATE _metadata SET value = '1.7.0' WHERE key = 'schema_version';
```

This migration is idempotent: the `ADD COLUMN` step is guarded by the
column-existence check specified in `DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)`, so
re-running the migration set against an already-migrated database is a no-op
rather than an error. Fresh databases created at schema version 1.7.0 receive the
`title TEXT NOT NULL` column directly from the `sprints` CREATE TABLE statement
and require no backfill.

### Migration 1.7.0 → 1.8.0

Adds the required `order_index` column to the `sprints` table and backfills every
existing sprint with a deterministic, collision-free execution order, so that
pre-existing sprints satisfy the `NOT NULL`, `> 0`, and uniqueness invariants
after the column and its unique index are added. The backfill assigns
`1, 2, 3, ...` in `created_at` ascending order, with `id` ascending as the
tie-breaker, so the resulting order is deterministic and reproducible.

The column is added with a temporary default of `0` so that the `ADD COLUMN`
succeeds against existing rows under the `NOT NULL` constraint; the backfill then
overwrites every row with a unique positive value before the unique index is
created. (SQLite cannot add a `NOT NULL` column without a default to a table that
already has rows.)

The `ADD COLUMN` statement deliberately carries **no** column-level
`CHECK(order_index > 0)`. SQLite evaluates a column-level `CHECK` against the
column `DEFAULT` for every existing row at `ADD COLUMN` time, so pairing
`DEFAULT 0` with `CHECK(order_index > 0)` would fail with "CHECK constraint
failed" on any populated table. A retrofitted column-level `CHECK` is accepted
only when the column `DEFAULT` satisfies it — as it does for the `title` column
added by the `1.6.0` → `1.7.0` migration above, whose `DEFAULT ''` satisfies
`CHECK(length(title) <= 255)` — and the sentinel `DEFAULT 0` required here does
not satisfy `> 0`. The `CHECK(order_index > 0)` therefore exists **only** on
freshly created databases, where it is part of the `sprints` `CREATE TABLE`
definition (see `DATABASE.md § sprints Table`). On migrated databases the column carries no
column-level `CHECK`; the positive (`> 0`) invariant on those databases is
upheld by the positive deterministic backfill below, the `idx_sprints_order`
unique index, and application-level model validation on every write.

```sql
-- Add the order_index column only when it does not already exist (see
-- DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)). When absent, run:
-- No column-level CHECK is added here: SQLite evaluates a retrofitted CHECK
-- against the column DEFAULT for every existing row, so CHECK(order_index > 0)
-- would fail against the DEFAULT 0 used to satisfy NOT NULL on existing rows.
ALTER TABLE sprints ADD COLUMN order_index INTEGER NOT NULL DEFAULT 0;

-- Backfill a deterministic, unique, positive execution order across all sprints,
-- ordered by created_at ascending, then id ascending as the tie-breaker.
UPDATE sprints
SET order_index = (
    SELECT COUNT(*)
    FROM sprints AS s2
    WHERE s2.created_at < sprints.created_at
       OR (s2.created_at = sprints.created_at AND s2.id <= sprints.id)
);

-- Create the unique index that enforces order uniqueness across the roadmap.
CREATE UNIQUE INDEX IF NOT EXISTS idx_sprints_order ON sprints(order_index);

-- Update schema version
UPDATE _metadata SET value = '1.8.0' WHERE key = 'schema_version';
```

This migration is idempotent: the `ADD COLUMN` step is guarded by the
column-existence check specified in `DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)`, the
backfill is a deterministic full-table assignment that yields the same result on
every run, and the index creation uses `IF NOT EXISTS`. Re-running the migration
set against an already-migrated database is therefore a no-op. Fresh databases
created at schema version 1.8.0 receive the `order_index` column and the
`idx_sprints_order` unique index directly from the `sprints` schema definition
and require no backfill.

### Migration 1.8.0 → 1.9.0

Adds the two comment tables, `task_comments` and `sprint_comments`, and the one
index each of them needs. The migration adds no column to any existing table, so
the `ALTER TABLE ADD COLUMN` guard specified in
`DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)` does not apply here. The migration is
inherently idempotent: every `CREATE` statement carries `IF NOT EXISTS`, and the
closing `UPDATE _metadata` writes the same literal on every run.

There is no backfill. Comments are new data with no pre-existing source, so an
already-populated database migrates to two empty tables, and every existing task
and sprint simply has no comments until one is written.

```sql
-- Task comments. The CHECK enumerates exactly the seven types a task comment
-- accepts (see DATABASE.md § task_comments Table).
CREATE TABLE IF NOT EXISTS task_comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id INTEGER NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('FINDING', 'HYPOTHESIS', 'TEST', 'DECISION', 'PROGRESS', 'UPDATE', 'NOTE')),
    body TEXT NOT NULL CHECK(length(body) <= 4096),
    created_at TEXT NOT NULL,
    updated_at TEXT,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_comments_task_created ON task_comments(task_id, created_at ASC);

-- Sprint comments. The CHECK enumerates exactly the four types a sprint comment
-- accepts (see DATABASE.md § sprint_comments Table).
CREATE TABLE IF NOT EXISTS sprint_comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sprint_id INTEGER NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('FINDING', 'DECISION', 'PROGRESS', 'UPDATE')),
    body TEXT NOT NULL CHECK(length(body) <= 4096),
    created_at TEXT NOT NULL,
    updated_at TEXT,
    FOREIGN KEY (sprint_id) REFERENCES sprints(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_sprint_comments_sprint_created ON sprint_comments(sprint_id, created_at ASC);

-- Update schema version
UPDATE _metadata SET value = '1.9.0' WHERE key = 'schema_version';
```

Unlike the `1.8.0` migration, this one creates whole tables rather than
retrofitting a column onto a populated one, so the `CHECK` constraints stated
above are present on migrated databases exactly as they are on fresh ones: the
`CREATE TABLE` statement is the same statement in both cases, and there are no
existing rows for a new constraint to be evaluated against. Migrated and freshly
created databases therefore end up with identical comment tables, and the
application-level validation described in
`COMMANDS.md § Comment Field Constraints` is a first line of defence rather than
the only one.

The two tables reference `tasks(id)` and `sprints(id)` with `ON DELETE CASCADE`,
which SQLite enforces only when `foreign_keys` is on. That PRAGMA is connection
scoped and is carried in the DSN of every connection the application opens (see
`IMPLEMENTATION.md § Where Each PRAGMA Is Applied`), so deleting a task or a
sprint deletes its comments.

### Migration 1.9.0 → 1.10.0

Drops the `specialists` column from the `tasks` table. The task model carries no
such field, so the column holds data no part of the application reads or writes.

**This migration destroys data, and that is its purpose.** Every value stored in
`tasks.specialists` is discarded and is not recoverable from the database
afterwards. There is no backfill, no archive column, and no audit entry recording
the discarded values: the migration removes a field, and removing it means removing
what it held. A roadmap whose tasks carried a value therefore loses it, which is the
intended outcome of the change and not a side effect of it.

The `DROP COLUMN` step is guarded by the column-existence check specified in
`DATABASE.md § Migration Idempotency (ALTER TABLE DROP COLUMN)`, which is the
`ADD COLUMN` guard applied with the opposite sense: the statement runs only while
the column is still present. Re-running the migration set against an
already-migrated database is therefore a no-op rather than a "no such column"
error. Fresh databases created at schema version 1.10.0 never have the column,
because it is absent from the `tasks` `CREATE TABLE` statement (see
`DATABASE.md § tasks Table`), and so need no drop.

```sql
-- Drop the specialists column only while it still exists (see
-- DATABASE.md § Migration Idempotency (ALTER TABLE DROP COLUMN)). When present, run:
ALTER TABLE tasks DROP COLUMN specialists;

-- Update schema version
UPDATE _metadata SET value = '1.10.0' WHERE key = 'schema_version';
```

The single `ALTER TABLE ... DROP COLUMN` statement is sufficient and no table
rebuild is required, because `specialists` is a plain nullable `TEXT` column: it is
not a primary key or part of one, carries no `UNIQUE` constraint, is not indexed, is
named in no `CHECK` constraint and in no partial index, is used by no foreign key
and by no generated column, and appears in no view and in no trigger. The other
columns of `tasks` keep their values, their `CHECK` constraints, and their
`DEFAULT` clauses; the table's indexes and its `parent_task_id` self-reference
survive the drop unchanged.

**The audit log is not rewritten.** The `audit` table keeps every row it already
holds, including rows whose `operation` is `TASK_ASSIGN` or `TASK_UNASSIGN`. Those
two operations are not in the valid set the application writes and accepts, and no
command reaches them by name, but the migration deletes no audit row: a roadmap's
recorded history stays complete. The `operation` column carries no `CHECK`, so the
retained rows need no schema accommodation (see `DATABASE.md § audit Table`).

### Migration 1.10.0 → 1.11.0

Adds the two commit-tracking columns, `commit_open` and `commit_close`, to the
`tasks` table. Both are nullable `TEXT` columns carrying a git commit hash in the
format specified in `DATABASE.md § Commit Hash Format Constraint`, and each takes
that section's `CHECK` constraint with the column.

**There is no backfill, and there can be none.** The two values record which commit
a task was started from and which commit it was concluded at. Groadmap holds no
record of either fact for work already done: it runs no git command and reads no
repository, so nothing in the database or on disk could supply a truthful value for
a task that reached `DOING` or `COMPLETED` before this migration. Every existing
task therefore migrates with NULL in both columns, and it keeps them until its next
transition into `DOING` or into `COMPLETED` supplies a value. A task that is already
`COMPLETED` when the migration runs keeps a NULL `commit_close` permanently unless
it is reopened and completed again.

Consequently the two columns are nullable even though the CLI makes them mandatory
on the transitions that write them. The mandatory rule governs the transition, not
the column: it guarantees that no task can enter `DOING` or `COMPLETED` from now on
without a hash, and it says nothing about tasks that reached those states earlier.
Making either column `NOT NULL` would require inventing a value for that history,
which is exactly what this migration refuses to do.

Both `ADD COLUMN` steps are guarded by the column-existence check specified in
`DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)`, so re-running the
migration set against an already-migrated database is a no-op rather than a
"duplicate column name" error. The two guards are independent: a database that
somehow carries one column and not the other is brought to the full shape.

The `CHECK` constraint travels with each `ADD COLUMN` statement, so a migrated
database enforces the commit-hash format exactly as a fresh one does. SQLite does
not re-validate existing rows when a column is added, and it does not need to here:
every existing row receives NULL in the new column, and NULL satisfies the
constraint. Fresh databases created at schema version 1.11.0 receive both columns,
with the same constraints, directly from the `tasks` `CREATE TABLE` statement (see
`DATABASE.md § tasks Table`).

```sql
-- Add each column only when it does not already exist (see
-- DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)). When absent, run:
ALTER TABLE tasks ADD COLUMN commit_open TEXT CHECK(commit_open IS NULL OR (length(commit_open) BETWEEN 7 AND 64 AND commit_open NOT GLOB '*[^0-9a-f]*'));
ALTER TABLE tasks ADD COLUMN commit_close TEXT CHECK(commit_close IS NULL OR (length(commit_close) BETWEEN 7 AND 64 AND commit_close NOT GLOB '*[^0-9a-f]*'));

-- Update schema version
UPDATE _metadata SET value = '1.11.0' WHERE key = 'schema_version';
```

No index is created on either column. Neither is a filter, a sort key, or a join
key for any query in `DATABASE.md § Main SQL Queries`: both are read as part of the
`Task` object and are never searched. An index would cost write time on every status
change and buy nothing.

### Migration 1.11.0 to 1.12.0

Adds the two new columns of the `audit` table, `related_entity_id` and
`commit_hash`, and reclassifies the legacy `TASK_STATUS_CHANGE` entries whose
destination state the stored data determines beyond doubt.

**This migration needs no table rebuild.** Both changes to the table's shape are new
columns, and `ALTER TABLE ... ADD COLUMN` carries each column's `CHECK` constraint
with it. Nothing about an existing column changes: `entity_type` keeps its
`CHECK(entity_type IN ('TASK', 'SPRINT'))` unaltered, `entity_id` keeps its
definition unaltered, and no constraint is widened, narrowed, or added to a column
that already exists. The migration is therefore two guarded `ALTER TABLE` statements
and a set of `UPDATE` statements — no replacement table, no row copy, no drop, and no
rename. The rebuild procedure that altering an existing column's constraint would
require is deliberately not used here (see
`DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)`).

Both `ADD COLUMN` steps are guarded by the column-existence check specified in
`DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)`, so re-running the
migration set against an already-migrated database is a no-op rather than a
"duplicate column name" error. The two guards are independent: a database that
somehow carries one column and not the other is brought to the full shape. SQLite
does not re-validate existing rows when a column is added, and it does not need to:
every existing row receives NULL in each new column, and NULL satisfies both
constraints. Fresh databases created at schema version 1.12.0 receive both columns,
with the same constraints, directly from the `audit` `CREATE TABLE` statement (see
`DATABASE.md § audit Table`).

```sql
-- Add each column only when it does not already exist (see
-- DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)). When absent, run:
ALTER TABLE audit ADD COLUMN related_entity_id INTEGER CHECK(related_entity_id IS NULL OR related_entity_id > 0);
ALTER TABLE audit ADD COLUMN commit_hash TEXT CHECK(commit_hash IS NULL OR (length(commit_hash) BETWEEN 7 AND 64 AND commit_hash NOT GLOB '*[^0-9a-f]*'));
```

No index is created on either column, for the reason stated in
`DATABASE.md § audit Table`: neither is a predicate of any audit read.

**The physical column order differs between a migrated and a fresh database, and
nothing may depend on it.** `ADD COLUMN` appends, so a migrated `audit` table ends
`... entity_id, performed_at, related_entity_id, commit_hash`, while a table created
fresh from the `CREATE TABLE` statement ends
`... entity_id, related_entity_id, commit_hash, performed_at`. The two are
equivalent, because every statement in this specification names its columns
explicitly and none uses `SELECT *` or positional binding (see
`DATABASE.md § Query Audit Entries`). No migration reorders the columns to make the
two layouts identical: a reorder would require a table rebuild, and it would buy
nothing that naming the columns does not already guarantee.

#### Neither new column is backfilled

Every pre-existing audit entry keeps NULL in both new columns. This is not a
shortcut, and there is no truthful backfill available for either:

- **`commit_hash`** records which commit a task was started from or concluded at.
  Groadmap holds no record of either fact for work already done: it runs no git
  command and reads no repository. The two columns that could have supplied a value,
  `tasks.commit_open` and `tasks.commit_close`, were themselves introduced only in
  1.11.0 and are NULL for every task that existed before it, so they carry nothing to
  copy. Copying a task's current commit values onto a historical entry would also be
  wrong in principle: the entry records a moment, and the task's current value is the
  result of every transition since.
- **`related_entity_id`** records the counterpart entity of the operation that wrote
  the entry. A stored `SPRINT_ADD_TASK` entry names its sprint and nothing else, so
  the task it refers to is not recoverable. The `sprint_tasks` table shows which
  tasks are members of that sprint **now**, which is a different question: it cannot
  say which of them a given entry was about, it says nothing about tasks since
  removed, and a sprint with N entries and N members offers no correspondence between
  the two sets. Inferring a value from it would fabricate a fact, so the migration
  does not attempt it.

  The counterpart entries that would name a sprint, `TASK_STATUS_SPRINT` and the
  `sprint remove-tasks` form of `TASK_STATUS_BACKLOG`, raise no backfill question at
  all: no version of Groadmap before 1.12.0 wrote either operation, and the
  reclassification below never produces one, so no stored entry carries either value
  when the migration runs.

#### Reclassifying `TASK_STATUS_CHANGE`

The migration rewrites the `operation` of a `TASK_STATUS_CHANGE` entry to the
destination-specific operation **only when the stored data determines that
destination by exact equality**. The rule has no tolerance window, no nearest-match,
and no ordering heuristic:

An entry is reclassified when its `performed_at` is **exactly equal** to exactly one
of the owning task's three lifecycle timestamps:

| Timestamp matched | New operation |
|---|---|
| `tasks.started_at` | `TASK_STATUS_DOING` |
| `tasks.tested_at` | `TASK_STATUS_TESTING` |
| `tasks.closed_at` | `TASK_STATUS_COMPLETED` |

An entry is **not** reclassified, and keeps the value `TASK_STATUS_CHANGE`, in each
of these cases:

1. Its `performed_at` matches none of the three timestamps. This covers a transition
   to `BACKLOG`, which stamps no timestamp, and a task later reopened, which clears
   the timestamps that would have matched.
2. Its `performed_at` matches more than one of the three. Two transitions recorded at
   the same instant leave no evidence of which one the entry recorded.
3. The task named by `entity_id` no longer exists. A deleted task takes its
   timestamps with it.

```sql
-- Each statement rewrites only the entries whose destination is unambiguous: the
-- timestamp it matches must be equal, and neither of the other two may also equal it.
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
  );

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
  );

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
  );
```

The three statements are independent of each other and of their order: the exclusion
clauses make their `WHERE` conditions mutually exclusive, so no entry can satisfy two
of them. They are also idempotent: an entry the first run rewrites no longer carries
`TASK_STATUS_CHANGE`, so a second run matches nothing.

`t.started_at = audit.performed_at` is false when `t.started_at` is NULL, because a
comparison with NULL yields NULL rather than true. The NULL cases therefore need no
extra guard on the matched timestamp, only on the two excluded ones.

#### What the migration must not do

1. **It must not infer.** Only exact timestamp equality reclassifies an entry.
   Choosing the nearest timestamp, ordering the entries and assigning destinations by
   position, or reading a task's current status to guess the destination are all
   forbidden: each would write a fact the database does not hold.
2. **It must not reclassify `TASK_UPDATE` or `SPRINT_UPDATE`.** A field edit stamps
   no timestamp anywhere, so which field an entry recorded is unknowable. No entry
   carrying either operation is touched.
3. **It must not reclassify `SPRINT_MOVE_TASK`.** Such an entry names one sprint and
   no task, so neither the pair of sprints involved nor the task that moved is
   recoverable.
4. **It must not delete or renumber any entry.** No `DELETE`, no `id` rewrite, and no
   compaction. The `audit` table after the migration holds exactly the entries it
   held before, with the same `id` values and the same `performed_at` values; only
   some `operation` values change.
5. **It must not write an audit entry of its own.** A migration is not a roadmap
   operation.

#### Consequence: three operations survive as LEGACY

Because reclassification is deliberately incomplete, entries carrying
`TASK_STATUS_CHANGE` remain in migrated databases, and entries carrying
`TASK_UPDATE`, `SPRINT_UPDATE`, and `SPRINT_MOVE_TASK` remain untouched. All four
values therefore stay in the published operation catalogue, marked LEGACY: they are
accepted as `audit list --operation` filter values so the entries stay reachable by
name, and no command writes any of them from 1.12.0 onward (see
`DATABASE.md § audit Table`).

```sql
-- Update schema version
UPDATE _metadata SET value = '1.12.0' WHERE key = 'schema_version';
```

#### Acceptance criteria

1. Applying the migration to a database at 1.11.0 leaves `SELECT COUNT(*) FROM audit` unchanged, and leaves `SELECT MIN(id), MAX(id) FROM audit` unchanged.
2. Every entry whose `operation` was not `TASK_STATUS_CHANGE` before the migration carries the same `operation` after it.
3. `SELECT COUNT(*) FROM audit WHERE commit_hash IS NOT NULL OR related_entity_id IS NOT NULL` returns 0 immediately after the migration.
4. An entry whose `performed_at` equals the owning task's `started_at` and equals neither `tested_at` nor `closed_at` carries `TASK_STATUS_DOING` after the migration, and the equivalent holds for `tested_at` and `closed_at`.
5. An entry whose `performed_at` equals two of the owning task's timestamps still carries `TASK_STATUS_CHANGE` after the migration.
6. An entry whose `entity_id` names no existing task still carries `TASK_STATUS_CHANGE` after the migration.
7. An entry whose `performed_at` matches none of the three timestamps still carries `TASK_STATUS_CHANGE` after the migration.
8. Running the migration set twice against the same database produces the same result as running it once, and raises no error.
9. After the migration, `SELECT value FROM _metadata WHERE key = 'schema_version'` returns `1.12.0`.
10. After the migration, an `INSERT` into `audit` with `related_entity_id = 0` fails, and one with `commit_hash = 'ABC1234'` fails.

### Migration 1.12.0 → 1.13.0

Makes the `sprint_tasks` ordering index unique, so that no two member tasks of one
sprint can hold the same `position` and the sprint's planned execution order is total.
The invariant, the reasons it is required, and the rules every write path must follow
to preserve it are specified in
`DATABASE.md § sprint_tasks Table (1:N Relationship)` (Position Uniqueness Within a
Sprint). The general rules this migration follows are specified in
`DATABASE.md § Introducing a Uniqueness Constraint over Existing Rows`.

The migration adds no column, so the `ALTER TABLE ADD COLUMN` guard specified in
`DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)` does not apply, and it
rebuilds no table, because the constraint is carried by an index rather than by a
table-level `UNIQUE` clause.

#### The existing index is tightened, not duplicated

`idx_sprint_tasks_order` already covers `(sprint_id, position ASC)`. A separate unique
index over the same pair would be an exact duplicate, so the migration drops the
existing index and recreates it under the same name as `UNIQUE`. The schema ends with
one index over those columns, serving both the ordering reads and the constraint (see
`DATABASE.md § Index Design Rationale`).

#### Existing rows are repaired before the index is created

The repair runs first, while no unique index is in force, so its intermediate states
cannot violate one. It renumbers every sprint's positions to a dense `0..N-1` run
ordered by `position` ascending with `task_id` ascending as the tie-breaker, which
preserves the planned order wherever that order is unambiguous and settles it
deterministically wherever it is not. The ranking is computed in a subquery rather
than by a correlated count over the table being written, because a correlated count
would observe rows the same statement had already moved and could leave duplicates
behind; see `DATABASE.md § Introducing a Uniqueness Constraint over Existing Rows`
(The repair must not read its own writes).

The repair is not conditional on finding a violation. A database that already
satisfies the constraint and whose positions are already dense receives the value each
row already holds, so the statement is a no-op for it; a database with gaps has them
closed; a database with duplicates has them separated.

```sql
-- 1. Repair: renumber each sprint's positions to a dense 0..N-1 run, preserving the
--    planned order and breaking ties by task_id. Runs before the unique index exists.
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

-- 2. Replace the non-unique ordering index with its unique form, under the same name.
DROP INDEX IF EXISTS idx_sprint_tasks_order;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sprint_tasks_order ON sprint_tasks(sprint_id, position ASC);

-- Update schema version
UPDATE _metadata SET value = '1.13.0' WHERE key = 'schema_version';
```

#### What the migration must not do

It MUST NOT delete, truncate, or otherwise discard a `sprint_tasks` row in order to
make the index succeed. A duplicate position is an ambiguous order, not a redundant
membership: both tasks are genuine members of the sprint and both must survive. If the
index cannot be created even after the repair, the migration fails and the whole
transaction is rolled back, leaving the database exactly as it was and
`_metadata.schema_version` at `1.12.0`. The failure surface — exit code `1`, empty
stdout, and the message shape on stderr — is specified in
`DATABASE.md § Introducing a Uniqueness Constraint over Existing Rows` (The failure
surface).

It MUST NOT rank the repair by `added_at`. That would produce a valid dense run while
silently replacing each sprint's planned order with the order its tasks happened to be
added in.

#### Acceptance criteria

1. Applying the migration to a database at 1.12.0 leaves `SELECT COUNT(*) FROM sprint_tasks` unchanged, and leaves the set of `(sprint_id, task_id)` pairs unchanged.
2. After the migration, `SELECT COUNT(*) FROM (SELECT sprint_id, position FROM sprint_tasks GROUP BY sprint_id, position HAVING COUNT(*) > 1)` returns 0.
3. After the migration, every sprint's positions are a dense `0..N-1` run: for every `sprint_id`, `MIN(position)` is 0 and `MAX(position)` is `COUNT(*) - 1`.
4. For a sprint whose positions were already distinct before the migration, the relative order of its tasks by `position` is the same after the migration as before it.
5. For a sprint holding two tasks at the same position before the migration, the task with the lower `task_id` holds the lower position after it.
6. `PRAGMA index_list('sprint_tasks')` reports `idx_sprint_tasks_order` with `unique = 1`, and reports no second index over `(sprint_id, position)`.
7. After the migration, an `INSERT` or `UPDATE` that would give two tasks of one sprint the same `position` fails with a `UNIQUE` constraint error; the same position in two different sprints is accepted.
8. Running the migration set twice against the same database produces the same result as running it once, and raises no error.
9. A database whose `sprint_tasks` rows already satisfy the constraint and are already dense is byte-for-byte unchanged in that table by the repair step.
10. After the migration, `SELECT value FROM _metadata WHERE key = 'schema_version'` returns `1.13.0`.
11. A fresh database created at 1.13.0 receives the unique `idx_sprint_tasks_order` directly from the `sprint_tasks` schema definition and requires no repair.
12. `sprint reorder`, `sprint move-to`, `sprint top`, `sprint bottom` and `sprint swap` all succeed against a migrated database, including on a full reversal of a sprint's order and on a swap of two adjacent tasks.

## Release Process

1. Run the pre-release vulnerability check on the tree being released and act on its result before going further (see Pre-Release Vulnerability Check)
2. Bump the version constant in `cmd/rmp/main.go`
3. Update `CHANGELOG.md` and add the release notes file `release-notes/v<version>-<date>.md`
4. Commit the changes
5. Create the annotated git tag: `git tag -a v<version> -m "Release v<version>"`
6. Push `main` and the tag: `git push origin main && git push origin v<version>`
7. On the tag push, the `.github/workflows/release.yml` workflow builds the binaries and publishes the GitHub release

Past releases are discoverable via `git tag --list` and `git log v<previous>..v<current>` — no Version History table is kept here.

### Pre-Release Vulnerability Check

This check is a required step of every release. It is deliberately step 1,
because a vulnerability found here changes `go.mod` and `SPEC/BUILD.md`, and
those changes belong in the release commit rather than in a follow-up.

Run it from the repository root, against the exact tree being released:

```bash
govulncheck ./...
```

The report separates vulnerabilities whose code is actually called from those
merely present in the module graph. The release engineer treats the two
differently:

| What the report shows | What the release engineer does |
|-----------------------|--------------------------------|
| Nothing (`No vulnerabilities found`) | Continue to step 2 |
| A standard-library vulnerability that **is called** | Stop. Raise the Go floor to the release that fixes it, as `BUILD.md § Go Toolchain` requires: set the `go` directive in `go.mod`, update `BUILD.md § Go Toolchain` to name the new floor and the advisories behind it, re-run `govulncheck ./...`, and continue only once it reports nothing. The release MUST NOT be published on the old floor |
| A vulnerability outside the standard library that **is called** | Stop. Remediating it means changing a dependency, and the pins are governed by `BUILD.md § External Dependencies`, which forbids floating them casually. Refer the decision to the project owner, and do not publish the release while it is open |
| A vulnerability that is reported but **not called** | Continue; it does not block the release. Record it in the release notes so the judgement is visible to whoever reads them |

`govulncheck` exits 0 when it finds nothing and non-zero when it finds
vulnerabilities, so a scripted release can test the exit status. The
called-versus-not-called distinction, however, is read from the report itself,
not from the exit status.

**This check is not a validation gate.** The gate set is the six gates in
`BUILD.md § Validation Gates`, enforced identically in all three places, and
`govulncheck` is not one of them: neither `make check` nor either workflow runs
it. Making it a gate would turn a pipeline red because an advisory was published
between two commits, with nothing in the repository having changed. The
obligation belongs to the release procedure instead, where a person reads the
report and decides.

**`govulncheck` is not pinned to a version**, unlike the tools behind the `lint`
and `security` gates. Those are pinned so that a gate returns the same verdict
everywhere it runs. This check is the opposite case: its purpose is to reflect
the vulnerability database as it stands at release time, so a run that reported
nothing before may legitimately report something now. Install it with:

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
```

**Why this step exists.** A release was prepared, passed `make check`, passed the
full end-to-end suite, and was tagged — and its binaries would have shipped
carrying four standard-library vulnerabilities reachable from Groadmap's own
code, two of them on the `rmp web` request path. Every gate was green, because no
gate inspects published advisories. Nothing in the procedure required anyone to
look, so nobody did. This step is what requires someone to look.
