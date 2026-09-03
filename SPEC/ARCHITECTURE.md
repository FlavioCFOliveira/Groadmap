# System Architecture

## Table of Contents

- [High-Level Overview](#high-level-overview)
- [Directory Structure](#directory-structure)
- [Security Guarantees](#security-guarantees)
  - [Open-Time Permission Enforcement](#open-time-permission-enforcement)
- [Source Code Structure](#source-code-structure)
- [Modules and Responsibilities](#modules-and-responsibilities)
- [Command Lifecycle](#command-lifecycle)
- [Filesystem Layout Migration](#filesystem-layout-migration)
- [Error Handling](#error-handling)
  - [Error Reuse Policy (Mandatory)](#error-reuse-policy-mandatory)
- [Exit Codes](#exit-codes)
  - [Exit Code Standards](#exit-code-standards)
  - [Exit Codes of the Graph Server and Client](#exit-codes-of-the-graph-server-and-client)
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
│   ├── graph.sock            # Graph server socket (present only while rmp graph serve runs)
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
6. The roadmap's SQLite database lives inside the roadmap home directory at `~/.roadmaps/<name>/project.db` with `0600` permissions, applied and verified every time `rmp` opens the database and not only when it creates it (see `ARCHITECTURE.md § Open-Time Permission Enforcement`). Its SQLite sidecars (`project.db-wal`, `project.db-shm`) live alongside it.
7. A roadmap home directory holds the SQLite database and its sidecars, and, once the knowledge graph is used, the `graph/` subdirectory. While a dedicated graph server is running for the roadmap it also holds that server's socket, `graph.sock` (see rule 11). The directory is the designated location for per-roadmap artefacts; additional file types may be added without changing this layout.
8. The knowledge graph for a roadmap is stored in the subdirectory `~/.roadmaps/<name>/graph/` (mode `0700`), created on first use of `rmp graph execute`. It is a directory because the GoGraph backing store persists through an on-disk snapshot plus a write-ahead log; after the first statement that wrote, the directory also contains a `snapshot/` subdirectory, produced by the synchronous checkpoint that follows a transaction that wrote (see `GRAPH.md § Synchronous Checkpoint on Write`). The directory also holds `write.lock`, the advisory lock file Groadmap itself maintains to serialise access to the store (see `GRAPH.md § Concurrency and Recovery`). Apart from that lock file, the internal layout is owned by GoGraph and is opaque to Groadmap. The graph store is the canonical subject of `GRAPH.md`; see `GRAPH.md § Persistence Layout`.
9. Roadmap enumeration considers the immediate **subdirectories** of `~/.roadmaps/` (one directory per roadmap), not files at the top level. A roadmap is identified by the presence of `project.db`; neither the optional `graph/` subdirectory nor the optional `graph.sock` socket constitutes a roadmap on its own.
10. **No symbolic links for the data directory or a roadmap home directory.** Neither the data directory `~/.roadmaps/` nor any roadmap home directory `~/.roadmaps/<name>/` may be a symbolic link. When creating, opening, or migrating a roadmap directory, `rmp` MUST refuse to follow a symbolic link: if `~/.roadmaps/` is a symlink, or if the resolved `~/.roadmaps/<name>/` path is a symlink (rather than a real directory), the operation fails with an error (`utils.ErrDatabase`, exit code 1) instead of following the link. This prevents an attacker from redirecting `project.db` writes outside the data directory and prevents `rmp` from applying its `0700`/`0600` permission changes to a directory or file outside `~/.roadmaps/` reached through a link (CWE-59, link following). The startup layout-migration sweep applies the same rule: a `.db`-named top-level symbolic link is never a migration candidate and is left untouched (see `ARCHITECTURE.md § Filesystem Layout Migration`, Edge Cases).
11. The dedicated graph server started by `rmp graph serve` binds a Unix domain socket at `~/.roadmaps/<name>/graph.sock` with mode `0600`, unless `--socket` names another path. The socket is not part of the graph store and carries no data: it exists while a server runs, is removed when that server stops, and a copy left behind by a killed server is stale and is replaced by the next one. Because the roadmap home directory is `0700`, a socket at the default path is unreachable by another user whatever its own mode is; the socket's own mode is the fence that still holds when `--socket` places it elsewhere. `GRAPH.md § Socket Path and Permissions` is canonical for the path, the mode, and the access model.

## Security Guarantees

Groadmap implements several security layers to protect user data and ensure system stability:

### 1. Data Isolation and Privacy
- **Restricted Permissions**: The data directory `~/.roadmaps` and every per-roadmap home directory `~/.roadmaps/<name>/` are created with `0700` permissions, and individual `project.db` files are created with `0600` permissions **from the outset** (the file is created with mode `0600`, not created under the process umask and chmod-ed afterwards, so there is no window in which the database is more permissive than `0600`). The SQLite sidecars `project.db-wal` and `project.db-shm` are held to the same `0600` permissions as `project.db`, because they can hold the same data pages. These permissions are not a creation-time convention that later runs inherit: `0700` on the two directories and `0600` on `project.db` are re-applied and re-verified **every time `rmp` opens a roadmap database**, and after every layout migration. A `project.db` that cannot be brought to `0600` fails the command. The complete rule — the order of operations, the failure mode, the treatment of the sidecars, and the read-only open path — is `ARCHITECTURE.md § Open-Time Permission Enforcement`, which is the canonical statement of the permission model for the roadmap database.
- **Filesystem Safety — No Symlink Following (CWE-59)**: Neither `~/.roadmaps/` nor any roadmap home directory `~/.roadmaps/<name>/` may be a symbolic link. When creating, opening, or migrating a roadmap directory, `rmp` MUST refuse to follow a symbolic link and fail with an error rather than following it. This prevents redirection of `project.db` writes to a path outside the data directory and prevents `rmp`'s `0700`/`0600` permission changes from being applied to an external directory or file reached through a link. The rule is stated in `ARCHITECTURE.md § Directory Structure`, location rule 10, and the layout-migration sweep enforces the same rule for `.db`-named top-level symlinks (see `ARCHITECTURE.md § Filesystem Layout Migration`, Edge Cases).
- **Input Validation**: Roadmap names are strictly validated using the regex `^[a-z0-9_-]+$` with a maximum length of **50 characters** to prevent path traversal attacks and ensure filesystem compatibility. This validation MUST be applied as a central gate for all commands that accept a roadmap name (via `-r` or `--roadmap`).
- **Length Validation Error**: When a roadmap name exceeds 50 characters, the error message is: "Error: Roadmap name must not exceed 50 characters (got N)"

#### Open-Time Permission Enforcement

The `0600` restriction on `~/.roadmaps/<name>/project.db` is an **open-time**
guarantee, not a creation-time one. A database can arrive with a wider mode
through entirely ordinary means: restored from an archive that carried no
permission bits, copied from a filesystem that does not record them,
synchronized without preserving them, written by an older binary under a
permissive umask, or moved in from the legacy layout. Every command that opens a
roadmap database therefore re-establishes the restriction before it uses the
database, in the same way the `0700` on the directories is already re-applied and
re-verified on every open. This section is the canonical statement of that rule;
`DATABASE.md § Physical Location and Naming` refers to it.

**A. Directories: unchanged.** Before any database file is touched, `rmp`
re-applies `0700` to the data directory `~/.roadmaps/` and to the roadmap home
directory `~/.roadmaps/<name>/`, and verifies both. A directory that cannot be
brought to `0700` fails the command. This behaviour is unchanged, and it MUST NOT
be relaxed in order to align the directories with the file rule: the directory is
the boundary that stops another user from reaching any file inside the roadmap
home at all, so it stays re-applied and verified on every open.

**B. The database file: read the mode, repair it, read it again.** After the
directory permissions are applied and verified, and before any database
connection is established, `rmp` reads the permission bits of
`~/.roadmaps/<name>/project.db` when that file already exists, and then:

1. If the mode is exactly `0600`, `rmp` proceeds and changes nothing. No mode
   change is attempted, so a database that the invoking user can read but does not
   own opens normally as long as it is already restricted.
2. If the mode differs, `rmp` changes it to `0600` and reads the mode again.
3. If the mode is `0600` after the change, `rmp` proceeds.
4. If the change fails, or the mode still differs after the change, `rmp` fails
   the command with the failure mode in **C. Failure mode** below.

A database being created is unaffected by this sequence: it is created with mode
`0600` from the outset, as stated in **Restricted Permissions** above, so there is
no window in which it is more permissive.

Two ordering constraints are part of the rule, and neither is incidental:

- The enclosing directories are brought to `0700` **before** the file's mode is
  read, so between reading that mode and repairing it no other user can traverse
  into the roadmap home directory to act on the file.
- The file's mode is settled **before** the database connection is established.
  SQLite derives the mode of the write-ahead log and shared-memory sidecars from
  the mode of the database file it is opening. The process umask does not narrow
  that mode: the driver this binary links re-applies the requested mode to a
  sidecar it has just created while that file is still empty, which undoes the
  umask. A session that begins with `project.db` at `0600` therefore produces
  sidecars at `0600`, and the mode is never widened, so the guarantee this
  ordering constraint rests on is unaffected. Connecting first and restricting
  afterwards would let the engine stamp a wider mode onto files it creates in
  between.

**C. Failure mode.** A database that cannot be brought to `0600` is a database
whose contents are readable by other users of the machine. `rmp` refuses to
operate on it rather than warning and continuing.

- **Error message.** The failure produces the standard error shape specified in
  `HELP.md § Error message format`, and the error wraps the `utils.ErrDatabase`
  sentinel, as every I/O failure does (see
  `ARCHITECTURE.md § Error Reuse Policy (Mandatory)`). The line written to stderr
  is:

  ```
  Error: database error: cannot secure <path> to 0600: <detail>
  ```

  `<path>` is the absolute path of the database file. `<detail>` is the
  underlying failure, in one of two forms: the operating system's error text when
  the mode change itself failed, or `expected 0600, got <mode>` when the mode
  change reported success but the file is still not `0600`, which happens on a
  filesystem that does not record POSIX permission bits. A complete example:

  ```
  Error: database error: cannot secure /home/user/.roadmaps/project1/project.db to 0600: chmod /home/user/.roadmaps/project1/project.db: operation not permitted
  ```

  The `database error: ` prefix is the rendering of the wrapped sentinel and is
  part of the message the user sees. The AI-agent hint follows on its own line,
  exactly as on every other error path.

- **Exit code.** `1` (`EXIT_FAILURE`), which is the code `utils.ErrDatabase` maps
  to in `ARCHITECTURE.md § Exit Codes`. No new sentinel and no new exit code are
  introduced for this failure.

- **What the command MUST NOT have done.** When a command fails this way, it has
  performed no work on the database:
  1. No database connection has been established and no SQL statement has run:
     no schema creation, no migration, no query, no row change, and no audit
     entry. In particular, an invocation that appends a comment to a task or a
     sprint has not appended it, and an invocation that lists comments has read
     none.
  2. Nothing has been written to stdout. The invocation produces no JSON success
     object; its only output is the error on stderr.
  3. The database file's contents, name, and location are unchanged. The file is
     never deleted, renamed, replaced, or truncated in order to satisfy the
     guarantee.
  4. The file's mode is never left wider than it was found. The only mode change
     `rmp` attempts on it is the restriction to `0600`.

  The directory permissions described in **A. Directories** may already have been
  re-applied, because that step precedes the file check. That is the intended
  order: it restricts the enclosing directories and never touches the user's data.

**D. Sidecars: restricted on every open, never fatal.** When `project.db-wal` or
`project.db-shm` is present, `rmp` restricts it to `0600` on every open. A
sidecar that cannot be restricted does **not** fail the command, and no warning
is emitted. The asymmetry with `project.db` is deliberate, and it is not an
oversight to be "unified" later:

- SQLite owns the sidecars' lifetime. The engine creates, removes, and recreates
  them at moments `rmp` does not control: a checkpoint, or the closing of the
  last connection, can remove the write-ahead log between the moment `rmp`
  observes the file and the moment it changes the mode. Treating that as fatal
  would turn a benign race that has already resolved itself into an intermittent
  command failure.
- The case the restriction covers is narrow, because the guarantee on
  `project.db` already carries the sidecars. SQLite creates a sidecar with the
  permission bits of the database file it belongs to, narrowed by the process
  umask, so once step **B** has settled `project.db` at `0600` before the
  connection is established, every sidecar created in that session is `0600`
  without any further action. What the explicit restriction catches is a sidecar
  left behind by an earlier session or by another tool, created when the database
  file was still more permissive.
- The sidecars are transient state that never leaves the roadmap home directory.
  `project.db` is the durable artefact that gets backed up, copied to another
  machine, or handed to someone else, and its mode travels with it — which is
  precisely how a database arrives more permissive than `0600` in the first
  place. A sidecar exists only while a connection is open or after a crash,
  inside a directory that step **A** has just verified is `0700`, and a directory
  without its execute bit cannot be traversed by another user.

The sidecar restriction is therefore defence in depth for the case where the
`0700` directory guarantee is itself defeated: applied whenever it can be, and
skipped silently when it cannot.

**E. The read-only open path.** The web interface opens each roadmap database
strictly read-only, with SQLite `query_only` set (see
`WEB.md § Read-Only Data Flow`). A read-only opener may be able to read a file it
does not own, and therefore may be unable to change its mode. That path applies
the same rule, with the sequence in step **B** followed exactly as written:

- It reads the mode first and attempts no change when the file is already `0600`.
  This is what keeps a legitimate read working for a correctly restricted
  database the caller does not own: an unconditional mode change would fail there
  and refusing would protect nothing, because the file is already private.
- When the mode does deviate, the read-only path repairs it if it can and refuses
  the open if it cannot. A reader that lacks the ownership needed to restrict a
  database it can read is, by construction, reading a database that other users
  of the machine can also read, and the web interface is the one surface on which
  those contents leave the invoking user's terminal.
- Restricting the mode is the only filesystem effect the read-only path may have.
  It creates no file and no directory, it never widens a mode, and it changes
  neither the contents nor the schema of the database.
  `WEB.md § Security and Constraints` states that the web interface relaxes no
  permission and creates no roadmap database, roadmap home directory, or graph
  store directory for a read; restricting a file that is more permissive than
  `0600` is consistent with both statements. This bullet is about the roadmap
  database only: the graph store has its own rules, and what a graph statement
  that writes nothing may change on disk is
  `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`.
- The read-only path does not create, modify, or verify directories. The
  directory rule in step **A** is enforced by the writable open path and by the
  web server's startup sequence, which verifies `0700` on `~/.roadmaps/` before
  serving anything and re-applies `0700` to each roadmap home directory it opens
  during the startup schema migration (see `WEB.md § Server Lifecycle` and
  `WEB.md § Startup Schema Migration`).
- A refusal on the read-only path does not stop the server. It is a read failure
  on the affected route and surfaces as HTTP `500`, exactly as any other read
  failure does (see `WEB.md § Routes and Pages`). This matters for a roadmap whose
  startup schema migration was skipped, which is non-fatal and per-roadmap (see
  `WEB.md § Startup Schema Migration`): the per-request check is what prevents a
  database the startup step refused from being served through a path that never
  checked it. If a command-line invocation ever opens a database through the
  read-only path, the refusal is the failure mode in **C. Failure mode**,
  unchanged.

**F. Verifiable behaviour.** The rule is observable from outside the process, and
the following statements MUST hold:

1. Given `~/.roadmaps/<name>/project.db` at mode `0666`, owned by the invoking
   user, any command that opens that roadmap succeeds and leaves the file at
   `0600`. This is the case the rule exists for: without it, the command succeeds
   and leaves the file at `0666`.
2. Given a database whose mode the invoking user cannot change to `0600`, the
   command fails with exit code `1`, writes the message in **C. Failure mode** to
   stderr, writes nothing to stdout, and leaves both the database's contents and
   its mode as they were.
3. Given a database already at `0600`, the command opens it with no mode change
   attempted, whether the invoking user owns the file or not.
4. After any successful open, `~/.roadmaps/` and `~/.roadmaps/<name>/` are `0700`,
   whatever mode they had before it.
5. A `project.db-wal` or `project.db-shm` that cannot be restricted changes
   neither the exit code nor the output of the command.

### 2. Binary Hardening
- **ASLR Support**: The binary is compiled as a Position Independent Executable (PIE) to leverage Address Space Layout Randomization (standard in modern Go).
- **Stack Protection**: Go's runtime provides built-in stack management and bounds checking to protect against stack buffer overflows.
- **Static Analysis**: Usage of `go vet`, `staticcheck`, and race detection during development to identify potential vulnerabilities and race conditions.

### 3. Robustness and Reliability
- **CLI Robustness**: The argument parser is designed to handle extremely large inputs and malicious characters without crashing or panicking.
- **No SQL Injection**: All database interactions use parameterized queries via SQLite's prepared statements. Bulk ID parameters are converted to integers before query construction.
- **Foreign Key Enforcement**: `PRAGMA foreign_keys = ON;` must be enabled on every database connection to ensure referential integrity and trigger cascading deletes. Because it is connection-scoped, it is carried in the DSN rather than executed against an already-open connection, so it cannot depend on which pooled connection services a query; see `IMPLEMENTATION.md § Database Connections`.
- **Inert Database Paths**: The DSN is a `file:` URI with the database path percent-encoded, so no character in the path can redirect the open to another file or introduce a connection parameter. The roadmap name is validated, but the home directory the path is rooted in is not; see `IMPLEMENTATION.md § DSN Construction`.
- **Bulk Operation Limits**: Commands handling bulk task IDs (e.g., `rmp task get`) must batch operations into sets of 500 or fewer to stay safely within SQLite's `SQLITE_LIMIT_VARIABLE_NUMBER`.
- **Transactional Integrity**: All database modifications (CREATE, UPDATE, DELETE, status change) MUST be wrapped in an explicit SQL transaction. **Every** audit log entry the operation owes MUST be written within the same transaction to ensure atomicity and consistency. Several operations owe more than one entry — one per entity touched, or one per field changed — and the requirement covers all of them together: an operation that commits its change while writing only some of its entries, or that writes an entry for a change that was rolled back, violates this guarantee. `DATABASE.md § Transactional Atomicity Guarantees` enumerates the multi-entry operations and what each must contain.
- **Audit Immutability**: The `audit` table is append-only. No command updates an audit row and no command deletes one, so the record of an operation survives every later change to the entity it concerned — including `task reopen`, which clears a task's `commit_close` while leaving the audit entry that recorded the commit intact. The only statement that removes audit rows is the maintenance delete-by-age statement in `DATABASE.md § Clear Audit (Maintenance)`, which no CLI command issues. A migration may rewrite an entry's `operation` to a more precise value, and may do nothing else to it: it may not delete an entry, renumber one, or alter its `entity_type`, `entity_id`, or `performed_at` (see `VERSION.md § Migrations`).
- **XSS Prevention — Escaping at Render Time, Not Sanitizing at Input Time**: Roadmap text is stored exactly as the user entered it. `rmp` strips no HTML tag, removes no attribute, and rewrites no character on the way in. The defence is contextual escaping at the point of rendering: every page the web interface serves is produced by Go's `html/template`, which escapes each value according to the context it lands in (HTML text, attribute, script, URL), and data delivered to the browser as JSON is encoded as JSON rather than interpolated into markup. This is the correct defence, and the specified one. Escaping at render time protects each output context with the rules of that context and leaves the stored record faithful to what the user wrote, whereas sanitizing at input time would corrupt the record — a task description or a comment body that legitimately contains `<`, `>`, or an HTML fragment is data, not markup — while still not making any single output context safe. The rendering rules are specified in `WEB.md § Security and Constraints` (output escaping) and `WEB.md § Frontend Rules`.

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
│   │   ├── comment.go     # Comment subcommands of the task and sprint families
│   │   ├── graph.go       # Graph subcommands (GoGraph integration)
│   │   └── web.go         # web command (starts the embedded HTTP server)
│   ├── graphstore/        # The graph store's lifecycle: open, checkpoint, close
│   │   └── graphstore.go  # The ONE open/checkpoint sequence; every surface calls it
│   ├── graphclient/       # Reaching a roadmap's graph server: resolution + Bolt v5 client
│   │   └── graphclient.go # The ONE resolution rule and the ONE client; every surface calls it
│   ├── graphserve/        # The graph server's lifecycle: listener, options, drain, shutdown
│   ├── signals/           # The ONE registration for SIGINT and SIGTERM; every surface takes the action over
│   ├── web/               # Embedded HTTP server (net/http)
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
│   │   ├── comment.go     # TaskComment and SprintComment structs, CommentType enum
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
    ├── GRAPH.md
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
- Implements the `graph` command and its three subcommands, `execute`, `serve`
  and `client`.
- Integrates the external module `github.com/FlavioCFOliveira/GoGraph`, which
  supplies the labelled property graph, the Cypher engine, and the durable
  directory-based store. The integration boundary is contained in this one
  package so that an upstream API change is absorbed in a single place.
- Owns the `--query`/stdin input handling, the JSON serialisation of results,
  and the mapping of engine failures onto Groadmap's sentinel errors. It owns no
  validation of the statement's content beyond its length: the statement is
  handed to the engine as written, whatever it does (see
  `GRAPH.md § What Groadmap Does Not Check`).
- It does **not** own the graph store's lifecycle. Opening the store, holding its
  lock, building the engine and taking the checkpoint belong to
  `internal/graphstore` (module 8 below); this package calls it and then does
  what only a CLI does — reads the statement, serialises the result, prints the
  diagnostics, and chooses the exit code.
- It owns neither of the two things the graph server introduced. Deciding whether
  a roadmap is served, and speaking the protocol to a server that is, belong to
  `internal/graphclient` (module 9); running a server belongs to
  `internal/graphserve` (module 10). This package calls both, exactly as it calls
  `internal/graphstore`, and keeps the CLI's own half: the flags, the statement's
  source, the JSON on stdout, the diagnostics on stderr, and the exit code.
- The behaviour is specified in `GRAPH.md`; the CLI contract is in
  `COMMANDS.md § Graph Management`; the result JSON is in
  `DATA_FORMATS.md § Graph Query Result`.

**External dependency note.** GoGraph requires Go 1.26 and is consumed at the exact
tag **v0.12.0**. Because v0.12.0 is a v0 (pre-1.0) version,
it is consumable directly at the bare module path and `go.mod` pins the clean exact tag
`v0.12.0`. GoGraph MUST be pinned to an exact version in `go.mod`. The risk analysis and
required mitigations are in `GRAPH.md § Dependency Maturity Risk`; the toolchain and
pinning requirements are in `BUILD.md § Go Toolchain`.

### 7. internal/web/ and the embedded HTTP server

- Implements the web interface started by `rmp web`. The command entry point is
  `internal/commands/web.go`; the server itself lives in `internal/web/`.
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
  knowledge graph from each roadmap's `graph/` store. Every per-request handler
  opens a roadmap database **read-only**: it performs no write to it and writes no
  audit entry.
- **The graph store is opened the way the CLI opens it, and that includes
  writing.** The graph data endpoint executes the caller's Cypher without
  examining it, so it takes the store's exclusive lock, constructs the same
  transactional engine `graph execute` constructs, and checkpoints when the
  transaction it ran wrote (see `GRAPH.md § Engine Constructor by Path` and
  `GRAPH.md § Concurrency and Recovery`). A request to that endpoint can therefore
  create, change, and delete graph data, and can change the graph's schema, with
  no authentication. What a statement that writes nothing leaves untouched is the
  exhaustive rule `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`.
- Performs one writing step, at startup only: before binding the listener it opens
  each existing roadmap's `project.db` through the normal writable open path to run
  the SQLite schema migrations (idempotent; automatic; no user input), then closes
  it. This guarantees the per-request read-only handlers never query a stale-schema
  database. The startup migration is the only path on which `rmp web` writes to a
  roadmap database, and it precedes any read-only connection (see
  `WEB.md § Startup Schema Migration` and `VERSION.md § Migrations`).
- Validates roadmap names taken from the URL path against the central
  roadmap-name rules before using them to resolve any filesystem path, so a
  crafted path cannot traverse outside `~/.roadmaps/` (see Security Guarantees).
- The behaviour is specified in `WEB.md`; the CLI contract is in
  `COMMANDS.md § Web Interface`; the graph data JSON shape is in
  `DATA_FORMATS.md § Graph View Data`; the embedded-asset bundling is in
  `BUILD.md § Vendored Web Assets`.


### 8. internal/graphstore/ and the graph store's lifecycle

- Owns the lifecycle of a roadmap's GoGraph store, and only that: taking the
  directory's exclusive advisory lock, opening the store through recovery, opening
  the write-ahead-log writer, wrapping both in the transactional store,
  constructing the Cypher engine over it, taking the synchronous checkpoint, and
  releasing everything in the one safe order. The behaviour is specified in
  `GRAPH.md § Concurrency and Recovery`, `GRAPH.md § Engine Constructor by Path`
  and `GRAPH.md § Synchronous Checkpoint on Write`, all of which remain canonical.
- **There is exactly one realisation of that sequence, and every surface calls
  it.** `graph execute` and the web graph data endpoint are both on it, and the
  dedicated graph server will be. `GRAPH.md § Engine Constructor by Path` states
  the single-construction rule and `internal/testenv` enforces it: one engine
  construction and one snapshot write in the whole of production source.
- The package exists as its own package rather than inside either caller because
  `internal/commands` imports `internal/web`, so the dependency cannot run the
  other way, and because a store's lifecycle is neither a CLI concern nor an HTTP
  one. It is the same reasoning that gave `internal/graphlock` and
  `internal/backoff` packages of their own.
- **What it deliberately does not own.** It does not create the graph directory:
  the CLI creates it, and the web interface is forbidden to (`WEB.md § Security
  and Constraints`). And it does not execute statements, drain results, or
  classify failures — those differ by surface (a CLI exit code, an HTTP status, a
  Bolt session), and a boundary drawn around them would fit two callers and not
  the third.

### 9. internal/graphclient/ and reaching a graph server

- Owns two things and only those two: deciding whether a roadmap is currently
  served, and speaking Bolt version 5 to the server when it is. The behaviour of
  both is specified in `GRAPH.md § Server Resolution` and
  `GRAPH.md § The Bolt Client`, which remain canonical.
- **There is exactly one realisation of each, and every surface calls it.**
  `rmp graph client` is a thin command-line wrapper over this package;
  `rmp graph execute` and the web graph data endpoint call it to resolve, and
  then either call it to send the statement or call `internal/graphstore` to open
  the store. A second client, or a second resolution rule, would be a second set
  of answers to the questions `GRAPH.md § Server Resolution` settles, and the two
  copies would diverge silently: a resolution that treated an unanswered probe as
  "not served" still runs, and still returns a result, right up to the moment a
  server is holding the lock it then waits on.
- The package exists as its own package for the same reason `internal/graphstore`
  does: `internal/commands` imports `internal/web`, so the dependency cannot run
  the other way, and reaching a server is neither a CLI concern nor an HTTP one.
- It builds its client on the pinned engine's exported protocol primitives — the
  handshake, the request encoding and decoding, the chunked framing, and the
  PackStream codec — rather than on a third-party driver. It maps the values it
  decodes onto the JSON representations `DATA_FORMATS.md § Graph Client Result`
  fixes, so a result is the same whichever path carried it.
- **What it deliberately does not own.** It does not open the graph store, does
  not take the advisory lock, and does not decide what a failure means to a
  caller: a resolution outcome becomes an exit code in `internal/commands` and an
  HTTP status in `internal/web`, and those two answers differ.

### 10. internal/graphserve/ and the dedicated graph server

- Owns the lifecycle of the server `rmp graph serve` runs: the Unix domain socket
  listener, the options the engine's Bolt server is constructed with, the drain
  that precedes shutdown, and the ordered teardown. The behaviour is specified in
  `GRAPH.md § The Dedicated Graph Server`, which remains canonical.
- It constructs no engine of its own. It calls `internal/graphstore` for the
  store, the lock, the engine, and the checkpoint, exactly as the other surfaces
  do, so the single construction `GRAPH.md § Engine Constructor by Path` fixes
  stays single.
- It uses the engine's own Bolt server rather than a protocol of Groadmap's. The
  engine's convenience entry point that both listens and serves is unusable here
  because it binds a network address, so this package builds the `unix` listener
  itself and hands it to the serve call. The serve call closes that listener
  itself, so this package must not treat itself as its owner.
- The drain is this package's own work, because the engine has none: its shutdown
  cancels connection contexts rather than waiting for in-flight statements. See
  `GRAPH.md § Server Shutdown and the Drain`.
- **What it deliberately does not own.** It does not read the statement, does not
  serialise a result, and does not choose an exit code; and it is not a client of
  itself — a caller that wants to reach a server uses `internal/graphclient`.

### 11. internal/signals/ and the process's signal disposition

- Owns what `SIGINT` and `SIGTERM` mean to the `rmp` process, for every command.
  It registers for the two signals **once**, at the start of the process, and
  never unregisters. What a caller changes is the action taken on delivery, not
  the registration.
- **No delivery is ever unowned.** A short-lived invocation leaves the default
  action in place and is interrupted, exiting `130` (see
  [Exit Codes](#exit-codes)). A long-lived surface — `rmp graph serve` and
  `rmp web`, the only two — takes the action over for the length of its service
  and hands it back for its teardown. Because the registration is never torn down
  and rebuilt, there is no interval in which the process would be killed outright
  by a signal it is supposed to handle, and none in which a signal already queued
  would be delivered to nothing.
- **A surface takes the action over before it announces itself.**
  `rmp graph serve` takes over before it prints its socket
  (`GRAPH.md § Server Startup`, step 7), and `rmp web` before it prints its URL
  and therefore before it launches a browser (`WEB.md § Server Lifecycle`,
  step 5). An announcement is what a caller uses to decide the server is up, so
  ordering the take-over ahead of it is what makes a server that is up a server
  that stops gracefully.
- **Both properties are enforced rather than described.** `internal/testenv`
  fails the build if any production file outside this package handles signals
  itself, if this package's single registration is torn down or duplicated, if
  this package stops handling signals at all, or if either long-lived surface
  announces itself before taking the action over.
- **What it deliberately does not own.** It chooses no exit code — the action a
  caller installs does that — and it knows nothing about drains, stores, sockets,
  or HTTP.

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

Most commands complete a single operation and exit. Two do not, and their handlers do not return after step 5.

`rmp web` starts the embedded HTTP server and serves requests until it receives an interrupt or termination signal, then shuts down gracefully and exits 0. Each request the server handles opens the data it needs, renders the response, and releases the handle; the server holds no roadmap database or graph store open across requests. A roadmap database is always opened read-only. The graph store is opened the way `graph execute` opens it — but only when the roadmap is not being served, because a graph data request resolves the roadmap's socket first and sends its statement to a running server instead (see `GRAPH.md § Server Resolution`); when it does open the store, a request to that endpoint may write to it. The `web` lifecycle is specified in `WEB.md § Server Lifecycle`.

`rmp graph serve` opens one roadmap's graph store, holds it and its advisory lock for the life of the process, and answers Cypher statements over a Unix domain socket until it receives an interrupt or termination signal. It then drains the work in flight, shuts the protocol server down, checkpoints, releases the lock, removes the socket, and exits 0. Unlike `rmp web`, it holds the store open across requests: that is the point of it. The lifecycle is specified in `GRAPH.md § The Dedicated Graph Server`.

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

A failing invocation writes **nothing to stdout**. The error line, the AI-agent hint, and any help that accompanies the error all go to stderr.

Help follows an error in exactly one case: a **dispatch failure**, meaning the CLI cannot resolve the name it was given to a command (`rmp nadadisto`) or to a subcommand of a command (`rmp task nadadisto`). In that case the help for the level at which the name could not be resolved follows the error on stderr. No other error class appends help; a missing parameter, an unknown flag, an invalid value, a resource that does not exist, or a database failure each produce the error line and the hint alone. `HELP.md § Error message format` is the canonical specification of the error output: which parts appear, in which order, and on which stream.

**Example - General error:**
```
$ rmp task get -r project1 999
Error: Task with ID 999 not found in roadmap 'project1'
```

**Example - Missing parameter (no help is appended):**
```
$ rmp task create -r project1
Error: required parameter missing: --title

AI agents: run `rmp --ai-help` for a machine-readable command contract.
```

**Example - Dispatch failure (family help follows the error, exit code 127):**
```
$ rmp task nadadisto -r project1
Error: unknown task subcommand: nadadisto

Usage: rmp task <subcommand> [options]
...the remainder of the family help body...

AI agents: run `rmp --ai-help` for a machine-readable command contract.
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
| `utils.ErrUnknownCommand` | 127 | Dispatch failure: the name given does not resolve to a command or to a subcommand of a command |

A dispatch failure MUST be carried by `utils.ErrUnknownCommand` and MUST NOT be wrapped in `utils.ErrInvalidInput`. The two are distinct classes: `utils.ErrInvalidInput` covers a malformed flag or argument supplied to a command that was resolved, and exits `2`; `utils.ErrUnknownCommand` covers a command or subcommand name that could not be resolved at all, and exits `127`. Wrapping the second in the first is what makes an unresolved subcommand exit `2` instead of `127`, and it also prefixes the message with `invalid input: `, which misreports the class to the reader.

Three failure conditions are classified in ways the one-line descriptions above
do not settle on their own, so the specification fixes them here.

1. **A date value that does not parse** is carried by `utils.ErrValidation` and
   exits `6`. This holds for every date-range filter flag the CLI publishes:
   `task list --created-since` and `--created-until`, and `audit list` and
   `audit stats` `--since` and `--until`. The value is neither out of an allowed
   range nor an invalid enum member, which are the two cases the table names, so
   without this rule a reader could reasonably classify it as
   `utils.ErrInvalidInput` and exit `2` instead.
2. **A state transition the state machine rejects** is carried by
   `utils.ErrValidation` and exits `6`. The status the caller asked for is a
   valid enum member; what the CLI refuses is the move from the task's current
   status to it. `STATE_MACHINE.md § Valid Transitions` defines the permitted
   moves; an unknown status string is a separate failure, also
   `utils.ErrValidation`, because the value itself is then invalid.
3. **A multi-identifier operation in which any identifier does not exist** is
   carried by `utils.ErrNotFound` and exits `4` as a whole. The rule is the same
   whether none of the identifiers exists or only some do, and the operation is
   refused in its entirety rather than applied to the identifiers that do exist.
   The per-command error tables in `COMMANDS.md` publish the exact string and
   restate that no change is made.

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
| `127` | `EXIT_CMD_NOT_FOUND` | Command not found | Dispatch failure: an unresolved command name or an unresolved subcommand name |
| `130` | `EXIT_SIGINT` | Interrupted by Ctrl+C | SIGINT received |

### Exit Codes of the Graph Server and Client

`rmp graph serve` and `rmp graph client` introduce no new exit code and no new sentinel error. Every failure either can produce is carried by a sentinel the catalogue above already names, and this section enumerates which codes each subcommand can return so that the enumeration exists in one place. `COMMANDS.md § Graph Management` is canonical for the command-line contract, and `GRAPH.md § The Dedicated Graph Server` for the behaviour behind each row.

`rmp graph execute` is not enumerated here, because the graph server changed which failures it can reach without changing its exit codes: it gained the same `--socket` flag the two subcommands below carry, and with it three failures of the server path — a socket that answers but yields no reachable server, a connection lost or unanswered after the statement was sent, and a serialisation conflict every attempt of the retry policy collided on — all three `utils.ErrDatabase` and exit code 1, and an empty `--socket` value, `utils.ErrRequired` and exit code 2. Every one of them lands on a code that subcommand already returned. `COMMANDS.md § Execute Exit Codes` is canonical for its full set.

`rmp graph serve`:

| Exit Code | Sentinel | Cause |
|-----------|----------|-------|
| `0` | — | The server started, served, and was stopped by `SIGINT` or `SIGTERM`. The drain completed or its bound expired; in both cases every acknowledged commit is durable. |
| `1` | `utils.ErrDatabase` | The store could not be opened or recovered; or its exclusive advisory lock could not be taken within the bounded wait, which is what refuses a second server against the same roadmap; or the socket could not be bound; or a live server already answers on the resolved socket. |
| `2` | `utils.ErrInvalidInput` | An unknown flag, or a positional argument: the subcommand accepts none. |
| `2` | `utils.ErrRequired` | `--socket` was supplied with an empty value. |
| `3` | `utils.ErrNoRoadmap` | No roadmap selected and none provided via `-r`. |
| `4` | `utils.ErrNotFound` | The selected roadmap does not exist. |

`rmp graph client`:

| Exit Code | Sentinel | Cause |
|-----------|----------|-------|
| `0` | — | The statement was sent to a server, ran, and its result was written to stdout. |
| `1` | `utils.ErrDatabase` | No server is listening for the roadmap; or a server could not be reached through the socket; or the connection was lost, or went unanswered, after the statement was sent; or the statement failed to parse or execute in the engine; or it exhausted the statement time budget; or every attempt of its retry policy lost a serialisation conflict; or a value the server returned could not be mapped onto the published result shape. |
| `2` | `utils.ErrRequired` | No statement supplied, or `--socket` supplied with an empty value. |
| `2` | `utils.ErrInvalidInput` | An unknown flag, or a positional argument: the subcommand accepts none. |
| `3` | `utils.ErrNoRoadmap` | No roadmap selected and none provided via `-r`. |
| `4` | `utils.ErrNotFound` | The selected roadmap does not exist. |
| `6` | `utils.ErrValidation` | The statement is longer than the maximum query length. |

Two remarks, because each is a place a reader could reasonably expect a different code:

1. **A failure to reach a server is `1`, not `4`.** `utils.ErrNotFound` is the class of a roadmap, task, or sprint that does not exist. A socket with nothing behind it is a dependency that is unavailable, which is the class `utils.ErrDatabase` already carries for a graph store that cannot be opened, and treating it as `4` would make a shell script that branches on `4` act on the wrong condition.
2. **A graceful stop is `0`, not `130`, and the reading begins when the server takes the signals over.** The catalogue reserves `130` for an interruption, and `rmp graph serve` interprets `SIGINT` as an instruction to stop rather than as an interruption of unfinished work: it drains, checkpoints, and exits successfully. This matches `rmp web`, which is the only other long-lived command, and the two are stated the same way for the same reason. Both take the signals over immediately before they announce themselves (see the `internal/signals` entry under [Modules and Responsibilities](#modules-and-responsibilities)), so the `0` row of each table is conditioned on a server that started and served, exactly as it is worded. A signal that arrives during startup — before the socket or the URL is announced — reaches an invocation that has served nothing and owes no drain, and it is still an interruption: the process exits `130`. `GRAPH.md § Server Shutdown and the Drain` and `WEB.md § Server Lifecycle` state that boundary where a reader sizing a supervisor's grace period will meet it.

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
rmp task create -t "New task"   # Exits 3: no roadmap given, so set -e stops the script
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
- A `stdout_on_success.schema` that enumerates the keys of a returned
  object — the object itself, or the element object of a returned
  array — MUST name exactly the keys the Go struct behind it marshals
  to JSON, no key more and no key fewer, because the enumeration is a
  second copy of a shape the struct already fixes. The obligation binds
  the enumerating values alone: one that only names the returned object
  without listing its keys publishes no such copy and is not required to
  start listing them. `HELP.md § Audit family help specifics` rule 4
  places the parallel obligation on the plain-text `audit` help.

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
preceding it, the CLI emits exit code 2 with the standard error format,
and no help follows the error. This is a deliberate exception to the
exit code `127` specified for a dispatch failure in
`HELP.md § Exit code of a dispatch failure`: the name preceding
`--ai-help` is a scope selector for the contract emitter, not a name
being dispatched, so an unusable selector is an invalid argument to
`--ai-help` rather than a command that could not be found.

## See Also

- Memory Layout Optimization → `MODELS.md § Memory Layout Optimization`
- Concurrency, Caching, Performance → `IMPLEMENTATION.md`
- Database schema and queries → `DATABASE.md`
- AI Agent Contract schema → `DATA_FORMATS.md § AI Agent Contract`
- AI Agent Contract CLI surface → `COMMANDS.md § AI Help`
- The dedicated graph server, its socket, its options, and the rule that decides whether a roadmap is served → `GRAPH.md § The Dedicated Graph Server`
- The resolution rule every surface follows before it opens a graph store → `GRAPH.md § Server Resolution`
