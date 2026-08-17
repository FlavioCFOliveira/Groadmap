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
- Displayed via `rmp --version`
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
