# System Architecture

## Table of Contents

- [High-Level Overview](#high-level-overview)
- [Directory Structure](#directory-structure)
- [Security Guarantees](#security-guarantees)
- [Source Code Structure](#source-code-structure)
- [Modules and Responsibilities](#modules-and-responsibilities)
- [Command Lifecycle](#command-lifecycle)
- [Filesystem Layout Migration](#filesystem-layout-migration)
- [Error Handling](#error-handling)
  - [Error Reuse Policy (Mandatory)](#error-reuse-policy-mandatory)
- [Exit Codes](#exit-codes)
  - [Exit Code Standards](#exit-code-standards)
- [AI Agent Contract Generation](#ai-agent-contract-generation)
- [See Also](#see-also)

## High-Level Overview

Groadmap is a CLI application distributed as a single binary executable. The architecture follows principles of simplicity, performance, and data isolation.

```
+-------------------------------------+
|           CLI Interface             |
|         (Go, argument parsing)      |
+------------------+------------------+
                   |
+------------------v------------------+
|         Command Router            |
|  (roadmap | task | sprint | graph)|
+------------------+------------------+
                   |
+------------------v------------------+
|         Business Logic              |
|   (validation, business rules)    |
+--------+-------------------+--------+
         |                   |
+--------v--------+   +------v-----------+
|  SQLite Layer   |   |  Graph Layer     |
| (queries, tx,   |   |  (GoGraph Cypher |
|  schema)        |   |   engine, store) |
+--------+--------+   +------+-----------+
         |                   |
+--------v-------------------v--------+
|         Filesystem                  |
| ~/.roadmaps/<name>/project.db       |
| ~/.roadmaps/<name>/graph/           |
+-------------------------------------+
```

The SQLite layer and the graph layer are independent persistence mechanisms
under the same roadmap home directory. They do not share connections,
transactions, or locks. The graph layer is specified in `GRAPH.md`.

## Directory Structure

```
~/.roadmaps/                  # User data directory
├── project1/                 # Roadmap home directory
│   ├── project.db            # Individual roadmap (SQLite)
│   ├── project.db-wal        # SQLite write-ahead log sidecar (when present)
│   ├── project.db-shm        # SQLite shared-memory sidecar (when present)
│   └── graph/                # Knowledge graph store (GoGraph), when present
├── project2/
│   └── project.db
└── ...
```

### Location Rules

1. The `.roadmaps` directory is located in the **user home directory**.
2. Directory name: exactly `.roadmaps` (dot prefix, lowercase).
3. Permissions: the data directory is restricted to the owner (`0700` or `drwx------` on POSIX) to ensure data privacy.
4. Each roadmap has its own **home directory** at `~/.roadmaps/<name>/`. The directory name is the roadmap name. This directory is the container for every file the `rmp` application uses for that roadmap.
5. Each roadmap home directory is created if absent, is owned by the user only, and uses the same `0700` permissions as the data directory; its permissions are verified on access.
6. The roadmap's SQLite database lives inside the roadmap home directory at `~/.roadmaps/<name>/project.db` with `0600` permissions. Its SQLite sidecars (`project.db-wal`, `project.db-shm`) live alongside it.
7. A roadmap home directory holds the SQLite database and its sidecars, and, once the knowledge graph is used, the `graph/` subdirectory. The directory is the designated location for per-roadmap artefacts; additional file types may be added without changing this layout.
8. The knowledge graph for a roadmap is stored in the subdirectory `~/.roadmaps/<name>/graph/` (mode `0700`), created on first use of any `rmp graph` subcommand. It is a directory because the GoGraph backing store persists through an on-disk snapshot plus a write-ahead log; after the first successful write subcommand the directory also contains a `snapshot/` subdirectory, produced by the synchronous checkpoint that runs after each write (see `GRAPH.md § Synchronous Checkpoint on Write`). Its internal layout is owned by GoGraph and is opaque to Groadmap. The graph store is the canonical subject of `GRAPH.md`; see `GRAPH.md § Persistence Layout`.
9. Roadmap enumeration considers the immediate **subdirectories** of `~/.roadmaps/` (one directory per roadmap), not files at the top level. A roadmap is identified by the presence of `project.db`; the optional `graph/` subdirectory does not by itself constitute a roadmap.

## Security Guarantees

Groadmap implements several security layers to protect user data and ensure system stability:

### 1. Data Isolation and Privacy
- **Restricted Permissions**: The data directory `~/.roadmaps` and every per-roadmap home directory `~/.roadmaps/<name>/` are created with `0700` permissions, and individual `project.db` files are created with `0600` permissions, preventing other users on the system from reading or modifying roadmap data. Permissions are (re)verified to `0700` for directories and `0600` for the database after every layout migration.
- **Input Validation**: Roadmap names are strictly validated using the regex `^[a-z0-9_-]+$` with a maximum length of **50 characters** to prevent path traversal attacks and ensure filesystem compatibility. This validation MUST be applied as a central gate for all commands that accept a roadmap name (via `-r` or `--roadmap`).
- **Length Validation Error**: When a roadmap name exceeds 50 characters, the error message is: "Error: Roadmap name must not exceed 50 characters (got N)"

### 2. Binary Hardening
- **ASLR Support**: The binary is compiled as a Position Independent Executable (PIE) to leverage Address Space Layout Randomization (standard in modern Go).
- **Stack Protection**: Go's runtime provides built-in stack management and bounds checking to protect against stack buffer overflows.
- **Static Analysis**: Usage of `go vet`, `staticcheck`, and race detection during development to identify potential vulnerabilities and race conditions.

### 3. Robustness and Reliability
- **CLI Robustness**: The argument parser is designed to handle extremely large inputs and malicious characters without crashing or panicking.
- **No SQL Injection**: All database interactions use parameterized queries via SQLite's prepared statements. Bulk ID parameters are converted to integers before query construction.
- **Foreign Key Enforcement**: `PRAGMA foreign_keys = ON;` must be enabled on every database connection to ensure referential integrity and trigger cascading deletes.
- **Bulk Operation Limits**: Commands handling bulk task IDs (e.g., `rmp task get`) must batch operations into sets of 500 or fewer to stay safely within SQLite's `SQLITE_LIMIT_VARIABLE_NUMBER`.
- **Transactional Integrity**: All database modifications (CREATE, UPDATE, DELETE, STATUS_CHANGE) MUST be wrapped in an explicit SQL transaction. The audit log entry for the operation MUST be written within the same transaction to ensure atomicity and consistency.
- **XSS Prevention**: User inputs that might be rendered in other contexts are sanitized to remove HTML tags and dangerous attributes.

## Source Code Structure

```
Groadmap/
├── go.mod                 # Go module definition
├── go.sum                 # Go dependencies checksum
├── cmd/
│   └── rmp/
│       └── main.go        # Entry point, CLI parsing
├── internal/
│   ├── commands/
│   │   ├── roadmap.go     # Roadmap subcommands
│   │   ├── task.go        # Task subcommands
│   │   ├── sprint.go      # Sprint subcommands
│   │   ├── graph.go       # Graph subcommands (GoGraph integration)
│   │   └── web.go         # web command (starts the embedded HTTP server)
│   ├── web/               # Embedded read-only HTTP server (net/http)
│   │   ├── server.go      # Server construction, routes, graceful shutdown
│   │   ├── handlers.go    # Read-only route handlers (index, sprints, tasks, sprint, graph, data)
│   │   ├── templates/     # Embedded html/template files (go:embed)
│   │   └── static/        # Embedded CSS/JS (vendored Tabler framework, D3.js + d3-sankey), fonts (Inter, Tabler Icons) (go:embed)
│   ├── db/
│   │   ├── connection.go  # SQLite connection management
│   │   ├── schema.go      # DDL, structure creation
│   │   ├── migrations.go  # Database schema migrations
│   │   ├── queries.go     # Parameterized SQL queries
│   │   └── query_cache.go # Query template caching
│   ├── models/
│   │   ├── task.go        # Task structs, enums
│   │   ├── sprint.go      # Sprint structs, enums
│   │   ├── roadmap.go     # Roadmap structures
│   │   ├── audit.go       # Audit log structures
│   │   └── consts.go      # Constants (limits, defaults)
│   └── utils/
│       ├── json.go        # JSON serialization
│       ├── time.go        # ISO 8601 date handling
│       └── path.go        # Cross-platform path resolution
└── SPEC/                  # Technical specification
    ├── ARCHITECTURE.md
    ├── BUILD.md
    ├── COMMANDS.md
    ├── DATABASE.md
    ├── DATA_FORMATS.md
    ├── DEPLOY.md
    ├── HELP.md
    ├── IMPLEMENTATION.md
    ├── MODELS.md
    ├── README.md
    ├── STATE_MACHINE.md
    ├── VERSION.md
    └── WEB.md
```

## Modules and Responsibilities

### 1. cmd/rmp/main.go
- Parse command-line arguments
- Route to appropriate handlers
- Top-level error handling
- Consistent JSON output

### 2. internal/commands/
Each package implements:
- Argument validation
- Specific business logic
- Data layer calls
- Response formatting

### 3. internal/db/
- **connection.go**: Connection management, safe open/close
- **schema.go**: Structure creation/updates
- **queries.go**: Parameterized SQL, injection prevention

### 4. internal/models/
- Go struct definitions
- Enums for states (TaskStatus, SprintStatus)
- JSON serialization/deserialization tags

### 5. internal/utils/
- **json.go**: Consistent JSON output wrapper
- **time.go**: UTC conversion, ISO 8601 formatting
- **path.go**: Cross-platform path resolution. Resolves the roadmap home
  directory and, for the graph feature, the per-roadmap `graph/` subdirectory.

### 6. internal/commands/graph.go and the GoGraph dependency
- Implements the `graph` command and its five subcommands.
- Integrates the external module `github.com/FlavioCFOliveira/GoGraph`, which
  supplies the labelled property graph, the Cypher engine, and the durable
  directory-based store. The integration boundary is contained in this one
  package so that an upstream API change is absorbed in a single place.
- Owns the guard-rail validation that maps each subcommand to an allowed Cypher
  operation class, the `--query`/stdin input handling, the JSON serialisation of
  results, and the mapping of engine failures onto Groadmap's sentinel errors.
- The behaviour is specified in `GRAPH.md`; the CLI contract is in
  `COMMANDS.md § Graph Management`; the result JSON is in
  `DATA_FORMATS.md § Graph Query Result`.

**External dependency note.** GoGraph requires Go 1.26 and is consumed at the exact
tag **v0.3.2**. Because v0.3.2 is a v0 (pre-1.0) version,
it is consumable directly at the bare module path and `go.mod` pins the clean exact tag
`v0.3.2`. GoGraph MUST be pinned to an exact version in `go.mod`. The risk analysis and
required mitigations are in `GRAPH.md § Dependency Maturity Risk`; the toolchain and
pinning requirements are in `BUILD.md § Go Toolchain`.

### 7. internal/web/ and the embedded HTTP server

- Implements the read-only web interface started by `rmp web`. The command entry
  point is `internal/commands/web.go`; the server itself lives in
  `internal/web/`.
- Built on Go's standard-library `net/http` only. It introduces no third-party
  web framework and no external runtime dependency.
- Serves server-rendered HTML produced from `html/template`, presented in the
  vendored Tabler admin-shell layout (dark theme), plus the vendored Tabler CSS
  and JavaScript framework, the Inter font and the Tabler Icons webfont, client
  scripts, and the vendored D3.js graph library (and d3-sankey). The templates and static
  assets are embedded into the binary with `go:embed`; the server serves only
  those embedded assets and never an arbitrary host filesystem path. The UI
  framework and asset set are specified in `WEB.md § UI Framework` and
  `BUILD.md § Vendored Web Assets`.
- Reads the same on-disk data the CLI reads: tasks and sprints from each
  roadmap's `project.db` (via the existing read queries in `DATABASE.md`) and the
  knowledge graph from each roadmap's `graph/` store (via the GoGraph engine's
  read path, exactly as `graph query`/`search` open it). It performs **no** write
  and triggers **no** graph checkpoint.
- Validates roadmap names taken from the URL path against the central
  roadmap-name rules before using them to resolve any filesystem path, so a
  crafted path cannot traverse outside `~/.roadmaps/` (see Security Guarantees).
- The behaviour is specified in `WEB.md`; the CLI contract is in
  `COMMANDS.md § Web Interface`; the graph data JSON shape is in
  `DATA_FORMATS.md § Graph View Data`; the embedded-asset bundling is in
  `BUILD.md § Vendored Web Assets`.

## Command Lifecycle

```
1. CLI Input → Parse arguments
2. Startup → Filesystem layout migration sweep (see Filesystem Layout Migration)
3. Validation → Verify syntax and values
4. Routing → Determine handler
5. Execution → Business logic + DB
6. Formatting → Structure result
7. Output → JSON to stdout
```

The startup sweep runs before routing on every `rmp` invocation, so all handlers (including `roadmap list` and `roadmap open`) observe the current filesystem layout.

Most commands complete a single operation and exit. The one exception is `rmp web`, whose handler does not return after step 5: it starts the embedded HTTP server and serves read-only requests until it receives an interrupt or termination signal, then shuts down gracefully and exits 0. Each request the server handles opens the data it needs read-only, renders the response, and releases the handle; the server holds no roadmap database or graph store open across requests. The `web` lifecycle is specified in `WEB.md § Server Lifecycle`.

## Filesystem Layout Migration

This section specifies the automatic migration of roadmaps from the **legacy** filesystem layout to the **current** layout. This is a filesystem-and-directory migration; it is distinct from the SQLite **schema** migration mechanism, which alters the contents of a database and is specified in `VERSION.md § Migrations`. The two mechanisms are independent and run at different times: the layout migration runs once at startup against the data directory; a schema migration runs when a specific database is opened.

### Layout Transition

| Layout | Database path |
|--------|---------------|
| Legacy | `~/.roadmaps/<name>.db` (plus sidecars `<name>.db-wal`, `<name>.db-shm`) |
| Current | `~/.roadmaps/<name>/project.db` (plus sidecars `project.db-wal`, `project.db-shm`) |

The roadmap name moves from being the database file basename to being the roadmap home directory name.

### When the Migration Runs

1. A migration sweep runs at the **startup of every `rmp` invocation**, before command routing.
2. The sweep performs a single read of the `~/.roadmaps/` directory to detect legacy roadmaps, identified as the immediate top-level entries that are **regular files** whose name ends in `.db` (the `-wal` and `-shm` sidecars are not counted as roadmaps; they are handled as part of their database's migration). A top-level entry whose name ends in `.db` but which is **not a regular file** — a symbolic link, a directory, or any other special file — is **not** a legacy roadmap candidate and is left untouched (see Edge Cases).
3. The sweep migrates **all** detected legacy roadmaps in one pass.
4. The sweep is idempotent and cheap when there is nothing to migrate: when no top-level `.db` files exist, the single directory read finds no candidates and the sweep is a no-op.
5. After the sweep completes, the rest of the command proceeds normally. Every command therefore observes the current layout.

### How a Single Roadmap Is Migrated

For each detected legacy database `~/.roadmaps/<name>.db` (a top-level **regular file** whose name ends in `.db`; non-regular entries are excluded at detection time per When the Migration Runs):

1. Validate `<name>` (the basename without the `.db` extension) against the roadmap name rules (see `COMMANDS.md § Create Roadmap`: regex `^[a-z0-9_-]+$`, maximum 50 characters, and any reserved-name rules). If the name is invalid, the entry is **not a valid roadmap**: skip it and leave it untouched (see Edge Cases).
2. Check for a conflict on the **destination database file**: if `~/.roadmaps/<name>/project.db` already exists, the current layout wins. Skip the migration for that name, leave the legacy `~/.roadmaps/<name>.db` and its sidecars untouched, surface a non-fatal warning on stderr, and continue with the remaining roadmaps (see Edge Cases). The conflict is keyed on the existence of `project.db`, not on the existence of the `~/.roadmaps/<name>/` directory: an existing directory without `project.db` is not a conflict and is handled by the next step.
3. Ensure the roadmap home directory `~/.roadmaps/<name>/` exists with `0700` permissions, before moving any file into it. If the directory does not exist, create it. If it already exists (for example, because an earlier run was interrupted after creating the directory but before the rename completed), reuse it and (re)apply and verify `0700` permissions on it.
4. **Move** (atomic rename within the same filesystem) the legacy files into the home directory:
   - `~/.roadmaps/<name>.db` → `~/.roadmaps/<name>/project.db`
   - `~/.roadmaps/<name>.db-wal` → `~/.roadmaps/<name>/project.db-wal` (only if the sidecar is present)
   - `~/.roadmaps/<name>.db-shm` → `~/.roadmaps/<name>/project.db-shm` (only if the sidecar is present)
5. No copies are left behind. After a successful migration the legacy top-level files for that roadmap no longer exist.
6. Verify permissions after the move: `0700` on `~/.roadmaps/<name>/` and `0600` on `~/.roadmaps/<name>/project.db`, consistent with the security model.

The move uses an atomic rename. The database content is never copied, so a roadmap's data is never duplicated and cannot be partially written; at every instant the database exists exactly once on disk.

The conflict check in step 2 is a mandatory safety guard. An atomic rename **silently overwrites** an existing destination file. The check that `~/.roadmaps/<name>/project.db` is absent must therefore pass before the rename in step 4 is attempted; this is precisely why the conflict is keyed on `project.db`. When `project.db` already exists, step 2 skips the roadmap and the rename in step 4 is never reached, so existing data is never overwritten.

### Edge Cases

| Case | Behaviour |
|------|-----------|
| **Conflict — current database already exists.** Both `~/.roadmaps/<name>.db` (legacy) and `~/.roadmaps/<name>/project.db` (current) exist. | The **current layout wins**. The migration for that name is **skipped**: the legacy `~/.roadmaps/<name>.db` and its sidecars are **left untouched** and are not moved, deleted, or overwritten. No existing data is destroyed. The conflict is keyed on the existence of `project.db`, not on the existence of the `~/.roadmaps/<name>/` directory. This skip is surfaced to the user as a non-fatal warning on stderr; the invocation continues and other roadmaps are still migrated. |
| **Existing `project.db`-less home directory — not a conflict (idempotent recovery).** `~/.roadmaps/<name>/` already exists but does **not** contain `project.db` (for example, an earlier run was interrupted after creating the directory but before the rename completed). | This is **not a conflict**. The migration **proceeds**: the existing directory is **reused**, its `0700` permissions are (re)applied and verified, and the legacy `~/.roadmaps/<name>.db` (plus any present sidecars) is moved into it as `project.db`. This makes the sweep idempotent across an interrupted earlier run. No warning is emitted; the migration completes normally. |
| **Invalid legacy name.** `<name>` violates the roadmap name rules (regex, length, or reserved name). | The entry is not a valid roadmap. It is **skipped and left untouched**; it is never moved and never deleted. A non-fatal warning may be surfaced on stderr. |
| **Non-regular top-level entry.** A top-level entry whose name ends in `.db` is **not a regular file**: a symbolic link (dangling, or pointing to a file or a directory), a directory, or any other special file (for example, a `.db`-named directory, or `escape.db` that is a symlink to a path outside the data directory). | The entry is **not a legacy roadmap candidate**. It is **skipped silently and left completely untouched**: it is never renamed, moved, chmod-ed, or deleted, and no roadmap home directory is created for it. No warning is emitted; like a `.db`-named directory, it is simply not a roadmap. The sweep never follows a symbolic link, so it can never move, change permissions on, or delete anything reached through the link. This preserves the security guarantee that the migration only ever affects paths strictly inside `~/.roadmaps/<name>/` and never mutates anything outside the data directory. |
| **Missing sidecars.** A `-wal` and/or `-shm` sidecar is absent. | Not an error. Only the files that are present are moved. |
| **Single-roadmap failure.** Moving one roadmap fails (for example, a rename across filesystems, or a permissions error). | The failure is contained to that one roadmap. Because the move is an atomic rename, a failed move leaves the original legacy files intact (no partial state). The sweep skips that roadmap, surfaces a non-fatal warning on stderr, and continues with the remaining roadmaps. The CLI invocation is not aborted destructively. |

### Error Handling and Exit Codes

1. A skipped roadmap (conflict, invalid name, or contained failure) does not change the invocation's exit code on its own; the sweep records a non-fatal warning to stderr and the requested command runs. A non-regular top-level entry (for example, a symbolic link or a directory) is not a candidate at all rather than a skipped roadmap: it does not change the exit code, and it is skipped silently with no warning (see Edge Cases).
2. A failure that prevents the sweep from reading the data directory at all (for example, `~/.roadmaps/` exists but is not readable) is an I/O failure and maps to `utils.ErrDatabase` (exit code `1`), consistent with `ARCHITECTURE.md § Error Handling`.
3. The migration never deletes a file it did not first successfully move. Legacy files are removed only as the source side of an atomic rename; they are never unlinked independently.

## Error Handling

### Error Categories

| Category | Example | Response |
|-----------|---------|----------|
| Invalid input | Missing parameter | Plain text error + command help |
| Resource not found | Roadmap not found | Plain text error to stderr |
| Conflict | Duplicate name | Plain text error to stderr |
| SQLite | Query error | Plain text error to stderr |
| System | No permissions | Plain text error to stderr |

### Error Format

**All errors are output as plain text to stderr (NOT JSON).**

Errors follow typical CLI conventions:
- Error messages are written explicitly to **stderr**
- Plain text format (human-readable)
- Uses standard Unix exit codes

**Input-related errors** (missing parameters, invalid arguments, unknown commands/subcommands) additionally display the **specific help for the command or subcommand** that was invoked.

**Example - General error:**
```
$ rmp task get -r project1 999
Error: Task with ID 999 not found in roadmap 'project1'
```

**Example - Input error (shows help):**
```
$ rmp task create -r project1
Error: Missing required parameters: --functional-requirements, --technical-requirements, --acceptance-criteria

Usage: rmp task create [OPTIONS]
...
```

### Error Reuse Policy (Mandatory)

All errors produced anywhere in the codebase MUST originate from the sentinel errors defined in `internal/utils/errors.go`. This is a hard requirement with no exceptions.

#### Sentinel Error Catalogue

The canonical set of sentinel errors is defined exclusively in `internal/utils/errors.go`:

| Sentinel | Mapped Exit Code | When to Use |
|----------|-----------------|-------------|
| `utils.ErrNotFound` | 4 | Any resource lookup that returns no rows |
| `utils.ErrAlreadyExists` | 5 | Unique constraint violation (name/ID conflict) |
| `utils.ErrInvalidInput` | 2 | Malformed argument, unknown flag, bad syntax |
| `utils.ErrRequired` | 2 | Required parameter is absent or empty |
| `utils.ErrNoRoadmap` | 3 | No roadmap selected and none provided via `-r` |
| `utils.ErrDatabase` | 1 | Any SQLite or I/O failure |
| `utils.ErrValidation` | 6 | Value out of allowed range or invalid enum value |
| `utils.ErrFieldTooLarge` | 6 | String field exceeds its maximum character limit |

#### Wrapping Rules

1. **Always use `%w`**: Every `fmt.Errorf` call that produces or re-wraps an error MUST use the `%w` verb to preserve the error chain for `errors.Is()` inspection.

   ```go
   // Correct
   return fmt.Errorf("opening roadmap %q: %w", name, utils.ErrNotFound)

   // Forbidden - breaks error chain
   return fmt.Errorf("opening roadmap %q: %v", name, utils.ErrNotFound)
   return errors.New("roadmap not found")
   ```

2. **Include context**: The wrapping message must identify the operation and relevant entity (roadmap name, task ID, etc.) to aid debugging.

3. **Never construct ad-hoc sentinel errors inline**: Strings like `errors.New("not found")` in command handlers are forbidden. Always wrap the corresponding sentinel from `utils`.

#### Propagation Rules

Each layer of the stack has a designated wrapping responsibility:

| Layer | Source Error | Must Wrap As |
|-------|-------------|-------------|
| `internal/db/` | `sql.ErrNoRows` | `utils.ErrNotFound` |
| `internal/db/` | SQLite constraint violation | `utils.ErrAlreadyExists` |
| `internal/db/` | Any other `database/sql` error | `utils.ErrDatabase` |
| `internal/commands/` | Field length exceeded | `utils.ErrFieldTooLarge` |
| `internal/commands/` | Missing required flag | `utils.ErrRequired` |
| `internal/commands/` | Invalid flag value / enum | `utils.ErrValidation` or `utils.ErrInvalidInput` |
| `internal/commands/` | No `-r` flag provided | `utils.ErrNoRoadmap` |
| `cmd/rmp/main.go` | Any unwrapped error | Maps via `errors.Is()` to exit code; falls back to exit 1 |

#### Adding New Error Types

When a new error category is genuinely needed:

1. Add the new sentinel variable to `internal/utils/errors.go` only — never inline.
2. Add the corresponding `IsXxx()` helper function in the same file.
3. Add a new exit-code mapping in `cmd/rmp/main.go` in the `handleError()` function.
4. Update the sentinel catalogue table above in this specification.

No new sentinel may be introduced without all four steps being completed in the same commit.

#### Compliance Verification

Static analysis must enforce these rules. The following patterns are forbidden and must be caught by code review or `go vet` custom checks:

```go
// Forbidden patterns
errors.New("...")           // in internal/commands/ or internal/db/
fmt.Errorf("...", err)      // missing %w — breaks errors.Is()
fmt.Errorf("...")           // no error wrapped at all in a return-error path
```

## Exit Codes

Groadmap follows standard Unix/Linux exit code conventions. Success output is JSON; errors are plain text to stderr. The exit code indicates success or failure type for shell scripting and CI/CD integration.

### Exit Code Standards

| Exit Code | Name | Description | When Used |
|-----------|------|-------------|-----------|
| `0` | `EXIT_SUCCESS` | Command completed successfully | All successful operations |
| `1` | `EXIT_FAILURE` | General error | Unexpected errors, database failures |
| `2` | `EXIT_MISUSE` | Misuse of command | Invalid arguments, syntax errors |
| `3` | `EXIT_NO_ROADMAP` | No roadmap selected | Commands requiring roadmap when none selected |
| `4` | `EXIT_NOT_FOUND` | Resource not found | Roadmap/task/sprint not found |
| `5` | `EXIT_EXISTS` | Resource already exists | Duplicate roadmap/task names |
| `6` | `EXIT_INVALID_DATA` | Invalid input data | Validation failures (dates, ranges) |
| `126` | `EXIT_NOT_EXECUTABLE` | Command not executable | Permission issues |
| `127` | `EXIT_CMD_NOT_FOUND` | Command not found | Unknown command/subcommand |
| `130` | `EXIT_SIGINT` | Interrupted by Ctrl+C | SIGINT received |

### Error Code Mapping

Internal error codes map to exit codes as follows:

| Error Code | Exit Code | Meaning |
|------------|-----------|---------|
| `INVALID_INPUT` | 2 | Bad command syntax or missing arguments |
| `INVALID_DATE` | 6 | Date format or range validation failed |
| `INVALID_DATE_RANGE` | 6 | Date range validation failed |
| `INVALID_PRIORITY` | 6 | Priority out of range (0-9) |
| `INVALID_SEVERITY` | 6 | Severity out of range (0-9) |
| `INVALID_STATUS_TRANSITION` | 6 | Invalid task status transition (state-machine validation) |
| `ROADMAP_NOT_FOUND` | 4 | Specified roadmap does not exist |
| `ROADMAP_EXISTS` | 5 | Roadmap name already in use |
| `TASK_NOT_FOUND` | 4 | Task ID does not exist |
| `TASKS_NOT_FOUND` | 4 | None of the provided task IDs exist |
| `SOME_TASKS_NOT_FOUND` | 4 | Some of the provided task IDs don't exist |
| `SPRINT_NOT_FOUND` | 4 | Sprint ID does not exist |
| `NO_ROADMAP` | 3 | No roadmap selected and none specified |
| `DB_ERROR` | 1 | Database operation failed |
| `SYSTEM_ERROR` | 1 | Internal system error |
| `UNKNOWN_SUBCOMMAND` | 2 | Invalid subcommand specified |
| `UNKNOWN_COMMAND` | 127 | Unknown command or subcommand |
| `UPDATE_FAILED` | 1 | Failed to update resource |
| `DELETE_FAILED` | 1 | Failed to delete resource |

### Usage in Shell Scripts

```bash
# Check if command succeeded
if rmp task list -r myproject > /dev/null 2>&1; then
    echo "Tasks listed successfully"
fi

# Handle specific errors
rmp roadmap create newproject
case $? in
    0) echo "Created successfully" ;;
    5) echo "Roadmap already exists" ;;
    *) echo "Failed with error code $?" ;;
esac

# Exit on any error (strict mode)
set -e
rmp task add -r myproject -d "New task"   # Exits 3 if no roadmap specified
```

## AI Agent Contract Generation

The CLI exposes a machine-readable description of its surface to AI
agents via `rmp --ai-help` (see `COMMANDS.md § AI Help` and
`DATA_FORMATS.md § AI Agent Contract`). To keep that contract and the
plain-text help in lock-step, both surfaces MUST be generated from the
same single source of truth at runtime.

### Single source of truth

A central command registry inside the binary describes every command,
every subcommand, every flag (long, short, type, required, default,
enum, range, length bounds, mutual exclusion), every positional
argument, every exit code, every success-output shape, every side
effect, every prerequisite, and at least one success and one failure
example per subcommand.

Two derivations are taken from this registry:

1. The plain-text help printers (`internal/commands/*_help.go`) format
   selected fields per the templates in `HELP.md`.
2. The AI contract emitter serialises the registry to JSON per the
   schema in `DATA_FORMATS.md § AI Agent Contract`.

### Non-duplication rules

- No `--help` printer may invent flag descriptions, defaults, or exit
  codes that are not in the registry. If the help needs to surface
  information, the registry is the place to add it.
- No `--ai-help` serialiser may invent or omit fields relative to the
  registry. Filtering by scope (whole CLI / command / subcommand) is
  the only transformation permitted.
- A change to a command's surface (new flag, renamed alias, new exit
  code, changed default) is one edit in the registry and is reflected
  automatically by both surfaces.

### Determinism

The JSON contract is deterministic: two invocations of `rmp --ai-help`
against the same binary version produce byte-identical output. The
contract does not include a timestamp, a process identifier, or any
locale-dependent string.

### Failure modes

The contract emitter is in-process and reads no external state. The
only runtime errors it can surface are I/O errors writing to stdout,
which map to exit code 1 via the standard error-handling path. When
`--ai-help` is combined with an unknown command or subcommand name
preceding it, the CLI emits exit code 2 with the standard error format.

## See Also

- Memory Layout Optimization → `MODELS.md § Memory Layout Optimization`
- Concurrency, Caching, Performance → `IMPLEMENTATION.md`
- Database schema and queries → `DATABASE.md`
- AI Agent Contract schema → `DATA_FORMATS.md § AI Agent Contract`
- AI Agent Contract CLI surface → `COMMANDS.md § AI Help`
