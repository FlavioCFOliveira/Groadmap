# Application Version Specification

## Current Version

| Component | Version | File |
|-----------|---------|------|
| Application | v1.11.0 | `cmd/rmp/main.go` |
| Database Schema | v1.8.0 | `internal/db/schema.go` |

The SPEC itself is not versioned (see `SPEC/README.md` and `CLAUDE.md` § Versioning Policy). Git tags are the canonical record of past application and schema versions.

## Versioning Strategy

### Application Version

The application version is defined in `cmd/rmp/main.go`:

```go
const version = "1.11.0"
```

This version is:
- Compiled into the binary at build time
- Displayed via `rmp --version`
- Used for release artefact naming (e.g., `rmp-v1.2.1-linux-amd64.tar.gz`)

### Database Schema Version

The database schema version is managed separately via `internal/db/schema.go`:

```go
const SchemaVersion = "1.8.0"
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

`SchemaVersion = "1.8.0"` (defined in `internal/db/schema.go`).

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
-- DATABASE.md § Migration Idempotency). When absent, run:
ALTER TABLE sprints ADD COLUMN title TEXT NOT NULL DEFAULT '' CHECK(length(title) <= 255);

-- Backfill each existing sprint with the literal title 'Sprint ' || id
-- (for example, sprint 5 becomes "Sprint 5")
UPDATE sprints SET title = 'Sprint ' || id;

-- Update schema version
UPDATE _metadata SET value = '1.7.0' WHERE key = 'schema_version';
```

This migration is idempotent: the `ADD COLUMN` step is guarded by the
column-existence check specified in `DATABASE.md § Migration Idempotency`, so
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
failed" on any populated table. More generally, SQLite cannot retrofit a
column-level `CHECK` onto an existing table through `ALTER TABLE ADD COLUMN` at
all. The `CHECK(order_index > 0)` therefore exists **only** on freshly created
databases, where it is part of the `sprints` `CREATE TABLE` definition (see
`DATABASE.md § sprints Table`). On migrated databases the column carries no
column-level `CHECK`; the positive (`> 0`) invariant on those databases is
upheld by the positive deterministic backfill below, the `idx_sprints_order`
unique index, and application-level model validation on every write.

```sql
-- Add the order_index column only when it does not already exist (see
-- DATABASE.md § Migration Idempotency). When absent, run:
-- No column-level CHECK is added here: SQLite cannot add a CHECK via
-- ALTER TABLE ADD COLUMN, and a CHECK(order_index > 0) would fail against the
-- DEFAULT 0 used to satisfy NOT NULL on existing rows.
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
column-existence check specified in `DATABASE.md § Migration Idempotency`, the
backfill is a deterministic full-table assignment that yields the same result on
every run, and the index creation uses `IF NOT EXISTS`. Re-running the migration
set against an already-migrated database is therefore a no-op. Fresh databases
created at schema version 1.8.0 receive the `order_index` column and the
`idx_sprints_order` unique index directly from the `sprints` schema definition
and require no backfill.

## Release Process

1. Update the version constant in `cmd/rmp/main.go`
2. Update the `Current Version` table in this file
3. Create the git tag: `git tag -a v<version> -m "Release v<version>"`
4. Push the tag: `git push origin v<version>`
5. The release workflow builds binaries and uploads artefacts

Past releases are discoverable via `git tag --list` and `git log v<previous>..v<current>` — no Version History table is kept here.
