# Knowledge Graph

## Table of Contents

- [Overview](#overview)
- [Functional Requirements](#functional-requirements)
- [Backing Engine: GoGraph](#backing-engine-gograph)
  - [Dependency](#dependency)
  - [Dependency Maturity Risk](#dependency-maturity-risk)
  - [Engine Construction and Lifecycle](#engine-construction-and-lifecycle)
- [Persistence Layout](#persistence-layout)
- [Multi-Layer Modelling Conventions](#multi-layer-modelling-conventions)
- [Subcommands and Guard-Rail Validation](#subcommands-and-guard-rail-validation)
  - [Operation Classes](#operation-classes)
  - [Per-Subcommand Validation Rules](#per-subcommand-validation-rules)
  - [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)
- [Error Handling and Exit Codes](#error-handling-and-exit-codes)
- [Concurrency and Recovery](#concurrency-and-recovery)
- [Constraints](#constraints)
- [Acceptance Criteria](#acceptance-criteria)
- [See Also](#see-also)

## Overview

The knowledge graph turns a roadmap into a queryable "second brain": a single
place where an AI agent records and retrieves the project's elements and the
relationships between them, so the agent can answer questions about the project
without re-reading every source file.

Each roadmap owns one knowledge graph. The graph is a free-form knowledge space.
Groadmap does not impose a fixed schema on it: the agent decides what nodes,
edges, labels, and properties to model. The graph is independent of the
roadmap's SQLite tasks and sprints data in this first version; the two stores
are not linked, and graph operations never read or write the `project.db`
database.

The graph is accessed through the `rmp graph` command and its five subcommands,
which accept Cypher and return results as JSON. The graph is backed by the
external GoGraph module, which provides a labelled property graph, a Cypher
engine, and durable on-disk persistence.

## Functional Requirements

1. `rmp graph` provides five subcommands: `create`, `query`, `update`,
   `delete`, and `search`. Each subcommand accepts a Cypher query and validates
   that the query matches the subcommand's operation class before executing it
   (see [Subcommands and Guard-Rail Validation](#subcommands-and-guard-rail-validation)).
2. Every graph subcommand requires a target roadmap, selected with the shared
   `-r` / `--roadmap` flag (see `COMMANDS.md § Roadmap Selection (Always Required)`).
3. Each subcommand reads its Cypher from the `--query` flag, or from standard
   input when the flag is absent (see [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)).
4. Read subcommands (`query`, `search`) return their result columns and rows as
   JSON to stdout, in the shape defined in `DATA_FORMATS.md § Graph Query Result`.
5. Write subcommands (`create`, `update`, `delete`) execute inside a single
   transaction and persist the change durably before the process exits. Their
   output mirrors the query's `RETURN` clause: a query with a `RETURN` clause
   returns the same `columns`/`rows` shape as a read result, and a query without
   a `RETURN` clause returns `{"ok": true}` (see
   `DATA_FORMATS.md § Graph Write Result`). The engine reports no
   affected-element count, so the write result carries no such field.
6. The graph for a roadmap is stored under that roadmap's home directory and is
   created on first use (see [Persistence Layout](#persistence-layout)).
7. Errors are written as plain text to stderr and map to the existing exit-code
   conventions (see [Error Handling and Exit Codes](#error-handling-and-exit-codes)).

## Backing Engine: GoGraph

### Dependency

The graph is backed by the external module GoGraph, consumed at the canonical
module path `github.com/FlavioCFOliveira/GoGraph`. GoGraph provides:

- A labelled property graph (LPG) parameterised as node identifier type `string`
  and edge weight type `float64`.
- A Cypher engine in the `cypher` package that parses and executes Cypher
  against the graph.
- A durable, directory-based store combining a write-ahead log, atomic on-disk
  snapshots, and recovery on open (see [Concurrency and Recovery](#concurrency-and-recovery)).

GoGraph requires Go 1.26 (toolchain 1.26.3). Adopting the graph feature
therefore raises Groadmap's minimum Go version from 1.25 to 1.26. The build
implications are specified in `BUILD.md § Go Toolchain`.

### Dependency Maturity Risk

At the time this specification was written, GoGraph is published as the
pre-release `v2.0.0-rc2`. A pre-release dependency is not API-stable: its public
surface (engine constructors, result iteration, helper functions, on-disk format)
may change before a stable `v2.0.0` tag. This carries concrete risks for
Groadmap:

1. **API drift.** The engine and result types named in this specification may be
   renamed or change signature between release candidates.
2. **On-disk format drift.** The store's snapshot and write-ahead-log format may
   change, which could make a graph written by one release candidate unreadable
   by a later one. There is no graph-format migration mechanism in this first
   version.
3. **Correctness maturity.** A release candidate may contain defects in Cypher
   parsing, execution, or recovery that a stable release would not.

Mitigations required by this specification:

1. Groadmap MUST pin GoGraph to an exact version in `go.mod` (a specific tagged
   version, not a floating reference), so builds are reproducible. The exact
   version is recorded in `BUILD.md § Go Toolchain`.
2. The graph feature MUST be implemented behind Groadmap's own command and
   error-handling boundary (this specification), so that an upstream API change
   is absorbed in one integration layer rather than spread across the codebase.
3. Upgrading GoGraph is a change that MUST be re-validated against the acceptance
   criteria in this file before release.

### Engine Construction and Lifecycle

The CLI is a short-lived process. For each `rmp graph` invocation the
implementation:

1. Resolves the graph directory for the selected roadmap (see [Persistence Layout](#persistence-layout)).
2. Opens the GoGraph store rooted at that directory, recovering any committed
   state from the snapshot and write-ahead log.
3. Constructs a persistent Cypher engine over that store (GoGraph exposes
   `cypher.NewEngineWithStore` for a store-backed engine; the in-memory
   `NewEngine`, `NewEngineWithOptions`, and `NewEngineWithRegistry` constructors
   are not used for persisted graphs).
4. Runs the validated query:
   - Read subcommands (`query`, `search`) run through the engine's read path
     (`Run` / `RunAny`).
   - Write subcommands (`create`, `update`, `delete`) run through the engine's
     transactional path (`RunInTx` / `RunInTxAny`) so the change is committed
     atomically.
5. Iterates the result for read subcommands (`Columns`, then `Next` / `Record`
   until exhausted, checking `Err`), serialises it to JSON, and writes it to
   stdout.
6. Closes the result and the store, ensuring committed writes are durable, then
   exits.

Parameter binding: when query parameters are supported, the implementation binds
them through GoGraph's parameter-binding path (`RunAny` / `RunInTxAny`, which
accept `map[string]any`, or `cypher.BindParams` followed by `Run` / `RunInTx`).

The exact Go types, function signatures, and any wrapper structs are
implementation details for `go-developer`; this specification fixes the
behaviour, not the Go API.

## Persistence Layout

Each roadmap's knowledge graph is stored in a dedicated subdirectory of that
roadmap's home directory:

```
~/.roadmaps/<name>/
├── project.db            # SQLite database (tasks, sprints, audit)
├── project.db-wal        # SQLite sidecar (when present)
├── project.db-shm        # SQLite sidecar (when present)
└── graph/                # Knowledge graph store (GoGraph)
    └── ...               # GoGraph snapshot and write-ahead-log files
```

Rules:

1. The graph store is a **directory**, not a single file, because GoGraph
   persists through an on-disk snapshot plus a write-ahead log. The directory is
   `~/.roadmaps/<name>/graph/`.
2. The graph directory is created on first use of any `rmp graph` subcommand for
   that roadmap, including read subcommands. A read against a roadmap that has no
   graph yet creates an empty graph store and returns an empty result; it is not
   an error.
3. The graph directory uses permissions `0700`, consistent with the roadmap home
   directory and the data directory (see `ARCHITECTURE.md § Directory Structure`).
4. The internal file names and on-disk format inside `graph/` are owned by
   GoGraph and are not specified here. Groadmap treats the directory as an opaque
   store managed through the engine.
5. Removing a roadmap (`rmp roadmap remove <name>`) deletes the entire roadmap
   home directory recursively, which includes `graph/`. No separate graph-removal
   command is required (see `COMMANDS.md § Remove Roadmap`).
6. The roadmap home directory layout, including the graph subdirectory, is
   described in `ARCHITECTURE.md § Directory Structure`. This file is the
   canonical source for the `graph/` subdirectory.

## Multi-Layer Modelling Conventions

The graph "will always tend to be a multi-layer graph": the project is captured
across several conceptual layers (for example, specification, code, and
decisions), with relationships within and across layers. GoGraph's labelled
property graph expresses layers through **node and edge labels** and **typed
properties**, not through separate stores.

This section provides **conventions and recommendations only**. Groadmap does
not enforce a schema, does not reject queries that ignore these conventions, and
does not create any nodes or labels on the agent's behalf. The agent is free to
model the graph however it chooses.

Recommended conventions:

1. **Layer as a label.** Tag each node with a label that names its layer, such
   as `Spec`, `Code`, `Decision`, `Dependency`, or `Requirement`. A node may
   carry more than one label.
2. **Identity as a property.** Give each node a stable, human-meaningful
   identifier property (for example, `key` or `name`) so the agent can `MERGE`
   on it without creating duplicates.
3. **Cross-layer relationships as typed edges.** Use edge types that read as
   verbs, such as `IMPLEMENTS`, `DEPENDS_ON`, `DECIDED_BY`, `REFERENCES`, or
   `SUPERSEDES`, to connect nodes within and across layers.
4. **Properties for attributes.** Store attributes (titles, statuses, file
   paths, timestamps) as node or edge properties using the value types GoGraph
   supports (see `DATA_FORMATS.md § Graph Query Result`).

Example layers and relationships (illustrative, not mandatory):

- A `Spec` node `MERGE`d on `key: "user-authentication"` linked by `IMPLEMENTED_BY`
  to a `Code` node `MERGE`d on `path: "internal/auth/jwt.go"`.
- A `Decision` node recording why JWT was chosen, linked by `MOTIVATES` to the
  `Spec` node and by `SUPERSEDES` to an earlier `Decision`.
- A `Dependency` node for an external library linked by `REQUIRED_BY` to the
  `Code` node that imports it.

## Subcommands and Guard-Rail Validation

The `graph` command exposes five semantic subcommands. Each subcommand is a
guard rail: it accepts only Cypher whose operation class matches that
subcommand, and it rejects everything else **before** executing the query. The
guard rail prevents an agent from, for example, deleting data through a command
it believes is read-only.

### Operation Classes

The guard rail classifies a query by the Cypher clauses it contains:

| Clause | Operation | Class |
|--------|-----------|-------|
| `CREATE`, `MERGE` | Adds nodes or edges | Write (creating) |
| `SET`, `REMOVE` | Mutates properties or labels on existing elements | Write (mutating) |
| `DELETE`, `DETACH DELETE` | Removes nodes or edges | Write (deleting) |
| `MATCH ... RETURN` only, with no writing clause | Reads and returns data | Read-only |

A query is a **writing query** when GoGraph's `cypher.QueryHasWritingClause`
reports that it contains any writing clause (`CREATE`, `MERGE`, `SET`, `REMOVE`,
`DELETE`, or `DETACH DELETE`). A query is **read-only** when it contains no
writing clause.
The guard rail uses `QueryHasWritingClause` as the primary read-vs-write
discriminator, and additionally inspects which writing clauses are present to
distinguish creating, mutating, and deleting writes for the per-subcommand rules
below.

### Per-Subcommand Validation Rules

Each subcommand accepts exactly the operation class listed below and rejects all
others. A query that does not match is rejected with `utils.ErrValidation`
(exit code 6) before execution; the graph is not opened for writing and no
change is made (see [Error Handling and Exit Codes](#error-handling-and-exit-codes)).

| Subcommand | Accepts | Rejects | Engine path |
|------------|---------|---------|-------------|
| `graph create` | A writing query whose only writing clauses are `CREATE` and/or `MERGE`. | Read-only queries; any query containing `SET`, `REMOVE`, `DELETE`, or `DETACH DELETE`. | Transactional write |
| `graph query` | A read-only query (`MATCH ... RETURN`, no writing clause). | Any query for which `QueryHasWritingClause` is true (contains `CREATE`, `MERGE`, `SET`, `REMOVE`, `DELETE`, or `DETACH DELETE`). | Read |
| `graph update` | A writing query whose writing clauses are `SET` and/or `REMOVE` (mutations on existing elements). | Read-only queries; queries containing `CREATE`, `MERGE`, `DELETE`, or `DETACH DELETE`. | Transactional write |
| `graph delete` | A writing query whose writing clauses are `DELETE` and/or `DETACH DELETE`. | Read-only queries; queries containing `CREATE`, `MERGE`, `SET`, or `REMOVE`. | Transactional write |
| `graph search` | A read-only query, intended for traversal and pattern matching, including variable-length paths (for example `-[*1..3]-`). | Any query for which `QueryHasWritingClause` is true. | Read |

Notes:

1. `graph query` and `graph search` enforce the **same** guard rail (read-only).
   They are distinct subcommands so the agent's intent is explicit and so the
   help and AI contract can describe `search` as the richer traversal-oriented
   read. The guard rail does not attempt to forbid simple matches under `search`
   or rich traversals under `query`; both accept any read-only Cypher.
2. A `MATCH` clause that only locates elements to write (for example,
   `MATCH (n:Spec {key:"x"}) SET n.status = "done"`) is classified by its
   **writing** clause, not by the presence of `MATCH`. The example is a mutating
   write and is valid only under `graph update`.
3. The guard rail is purely a clause-class check. It does not validate Cypher
   syntax; a syntactically invalid query that passes the clause check is rejected
   by the engine at execution time and surfaces as an engine error (see
   [Error Handling and Exit Codes](#error-handling-and-exit-codes)).

### Cypher Input Source and Precedence

Each graph subcommand obtains its Cypher from one of two sources:

1. The `--query "<cypher>"` flag.
2. Standard input, read in full, when the `--query` flag is absent. This allows
   piping a query, for example `cat query.cypher | rmp graph query -r myproject`.

Precedence and rules:

1. When `--query` is present and non-empty, its value is used and standard input
   is not read.
2. When `--query` is absent, the entire contents of standard input are read and
   used as the query.
3. When `--query` is absent and standard input is empty (or not connected), the
   command fails with `utils.ErrRequired` (exit code 2): no query was supplied.
4. When `--query` is present but its value is empty or whitespace only, the
   command fails with `utils.ErrRequired` (exit code 2).
5. Leading and trailing whitespace is trimmed from the query before the guard-rail
   check and before execution.

This is the one place in Groadmap where a command reads from standard input. The
cross-cutting input rule is stated in `DATA_FORMATS.md § Input`.

## Error Handling and Exit Codes

Graph subcommands use the existing sentinel errors and exit-code mapping defined
in `ARCHITECTURE.md § Error Handling` and `ARCHITECTURE.md § Exit Codes`. No new
sentinel is introduced for the graph feature.

| Condition | Sentinel | Exit code |
|-----------|----------|-----------|
| No roadmap selected and none provided via `-r` | `utils.ErrNoRoadmap` | 3 |
| Selected roadmap does not exist | `utils.ErrNotFound` | 4 |
| No query supplied (flag absent and stdin empty, or flag empty/whitespace) | `utils.ErrRequired` | 2 |
| Query's operation class does not match the subcommand | `utils.ErrValidation` | 6 |
| Cypher fails to parse or execute in the engine | `utils.ErrDatabase` | 1 |
| Graph store cannot be opened, recovered, read, or written (I/O, corruption, lock) | `utils.ErrDatabase` | 1 |
| Successful execution | — | 0 |

Rules:

1. The guard-rail rejection (operation class mismatch) is detected before the
   graph store is opened for writing. A rejected query never mutates the graph.
2. A Cypher parse or execution failure reported by the engine is wrapped as
   `utils.ErrDatabase` (exit code 1), consistent with treating the graph store as
   a database-class dependency. The error message names the subcommand and
   includes the engine's diagnostic text.
3. Errors are written as plain text to stderr and carry the standard AI-agent
   hint (see `HELP.md § Error message format`).
4. The graph feature introduces no new exit codes. If a future need arises for a
   dedicated graph error class, it MUST be added following the procedure in
   `ARCHITECTURE.md § Adding New Error Types`.

## Concurrency and Recovery

GoGraph's store uses a single-writer transactional model: writes are serialised
through one writer, while reads observe a consistent committed state. Durability
is provided by a write-ahead log with CRC32C integrity checks plus atomic on-disk
snapshots; on opening the store, GoGraph runs recovery to restore the last
committed state from the snapshot and log.

Groadmap's usage model and expectations:

1. Each `rmp graph` invocation is a short-lived process that opens the store,
   runs one query, commits any write, and closes the store. The process does not
   hold the store open across invocations.
2. Because the store is single-writer, two concurrent `rmp graph` write
   invocations against the **same** roadmap may contend for the writer. The
   implementation MUST surface a contention or lock failure as `utils.ErrDatabase`
   (exit code 1) rather than corrupting the store or hanging indefinitely. The
   retry and timeout behaviour for graph writes is specified in
   `IMPLEMENTATION.md § Graph Store Concurrency`.
3. Recovery on open is expected to be transparent: a graph left in a consistent
   committed state by a previous invocation opens cleanly. A graph whose store is
   corrupt or unreadable surfaces as `utils.ErrDatabase` (exit code 1); there is
   no automatic graph-store repair in this first version.
4. The graph store is independent of the SQLite connection cache and SQLite WAL
   model described in `IMPLEMENTATION.md § Concurrency Model`; the two persistence
   mechanisms do not share connections, locks, or transactions.

## Constraints

1. The graph is free-form. Groadmap MUST NOT impose, validate, or auto-create a
   node/edge schema. The conventions in
   [Multi-Layer Modelling Conventions](#multi-layer-modelling-conventions) are
   recommendations only.
2. The graph is independent of the SQLite tasks/sprints data in this version.
   Graph operations MUST NOT read from or write to `project.db`, and roadmap data
   operations MUST NOT read from or write to `graph/`.
3. Node identifiers are `string` and edge weights are `float64`, as fixed by
   GoGraph's parameterisation. Groadmap does not override these.
4. Graph operations require the `-r` / `--roadmap` flag, identical to `task` and
   `sprint` operations.
5. The graph feature MUST NOT introduce a new sentinel error or exit code in this
   version (see [Error Handling and Exit Codes](#error-handling-and-exit-codes)).
6. GoGraph is pinned to an exact version in `go.mod` (see
   [Dependency Maturity Risk](#dependency-maturity-risk)).

## Acceptance Criteria

1. `rmp graph create -r <roadmap> --query "CREATE (s:Spec {key:'user-authentication'})"`
   creates the node, persists it, prints `{"ok": true}` (the query has no `RETURN`
   clause), and exits 0. The same query with `... RETURN s` appended instead
   returns the created node in the `columns`/`rows` shape
   (see `DATA_FORMATS.md § Graph Write Result`).
2. `rmp graph query -r <roadmap> --query "MATCH (s:Spec) RETURN s.key"` returns
   the previously created node's `key` as JSON in the shape defined in
   `DATA_FORMATS.md § Graph Query Result`, and exits 0.
3. A query is read back correctly in a **separate** invocation, proving the graph
   persisted to `~/.roadmaps/<roadmap>/graph/` across process exits.
4. `rmp graph query --query "CREATE (n:Spec)"` is rejected with exit code 6 and a
   plain-text error on stderr, and creates nothing (guard-rail enforcement).
5. `rmp graph delete --query "MATCH (s:Spec) RETURN s"` is rejected with exit
   code 6 (a read-only query under a delete subcommand).
6. `rmp graph update --query "CREATE (n:Spec)"` is rejected with exit code 6 (a
   creating query under an update subcommand).
7. `echo "MATCH (n) RETURN count(n)" | rmp graph query -r <roadmap>` reads the
   query from stdin and returns the count, exits 0.
8. `rmp graph query -r <roadmap>` with no `--query` and no piped stdin fails with
   exit code 2 (no query supplied).
9. `rmp graph search -r <roadmap> --query "MATCH p=(a)-[*1..3]-(b) RETURN p"`
   executes a variable-length traversal and returns results, exits 0.
10. `rmp graph query -r missing-roadmap --query "MATCH (n) RETURN n"` against a
    non-existent roadmap fails with exit code 4.
11. A syntactically invalid Cypher query that passes the guard-rail clause check
    fails at execution with exit code 1 and a plain-text engine diagnostic on
    stderr.
12. Each graph subcommand is represented in the AI Agent Contract emitted by
    `rmp graph --ai-help` and `rmp --ai-help`, with the same fields as every
    other subcommand (see `DATA_FORMATS.md § AI Agent Contract`).
13. The graph directory `~/.roadmaps/<roadmap>/graph/` is created with `0700`
    permissions on first graph use.

## See Also

- CLI command contract for `graph` → `COMMANDS.md § Graph Management`
- Graph query result JSON and property-type mapping → `DATA_FORMATS.md § Graph Query Result`
- Standard input as a Cypher source → `DATA_FORMATS.md § Input`
- GoGraph integration, directory layout, error handling → `ARCHITECTURE.md`
- Go 1.26 toolchain bump and the GoGraph dependency → `BUILD.md § Go Toolchain`
- Single-writer store, recovery, and write contention → `IMPLEMENTATION.md § Graph Store Concurrency`
- Help skeleton and AI-help entry for `graph` → `HELP.md`
