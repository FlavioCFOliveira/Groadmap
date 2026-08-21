# Knowledge Graph

## Table of Contents

- [Overview](#overview)
- [Functional Requirements](#functional-requirements)
- [Backing Engine: GoGraph](#backing-engine-gograph)
  - [Dependency](#dependency)
  - [Dependency Maturity Risk](#dependency-maturity-risk)
  - [Engine Construction and Lifecycle](#engine-construction-and-lifecycle)
  - [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)
- [Persistence Layout](#persistence-layout)
- [Multi-Layer Modelling Conventions](#multi-layer-modelling-conventions)
- [Subcommands and Guard-Rail Validation](#subcommands-and-guard-rail-validation)
  - [Operation Classes](#operation-classes)
  - [Per-Subcommand Validation Rules](#per-subcommand-validation-rules)
  - [Relationship Write Direction](#relationship-write-direction)
  - [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)
- [Query Notifications as Diagnostics](#query-notifications-as-diagnostics)
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
6. After a write subcommand (`create`, `update`, `delete`) commits its
   transaction durably, and before the process exits, the implementation MUST
   produce a self-sufficient on-disk snapshot of the committed graph state and
   truncate the write-ahead log, synchronously within the same invocation. This
   checkpoint bounds write-ahead-log growth and keeps recovery cost proportional
   to the live graph size rather than to the total history of writes (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)). Read
   subcommands (`query`, `search`) are read-only and never checkpoint.
7. A checkpoint that fails after the transaction has already committed durably
   MUST NOT fail the user-visible write. The write succeeded, the write-ahead log
   is durable, and the next successful write reconciles the snapshot; recovery
   still works from the intact write-ahead log. The command returns its normal
   success output and exit code 0, and the checkpoint failure is surfaced through
   the existing observability conventions without changing the exit code (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)).
8. The graph for a roadmap is stored under that roadmap's home directory and is
   created on first use (see [Persistence Layout](#persistence-layout)).
9. Errors are written as plain text to stderr and map to the existing exit-code
   conventions (see [Error Handling and Exit Codes](#error-handling-and-exit-codes)).
10. Every graph subcommand that executes a query (`create`, `query`, `update`,
    `delete`, `search`) surfaces, on stderr, exactly the advisory notifications
    the engine returns for that query, as one plain-text diagnostic line per
    notification. The surfacing is wired identically on the read path (`query`,
    `search`) and the write path (`create`, `update`, `delete`). Groadmap does
    not generate notifications; the engine alone decides which queries and which
    execution paths carry them, and Groadmap emits whatever it is given (which may
    be none). Notifications never change the stdout success output or the exit code
    (see [Query Notifications as Diagnostics](#query-notifications-as-diagnostics)).

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

GoGraph requires Go 1.26: its `go.mod` declares `go 1.26` with `toolchain
go1.26.5`. Adopting the graph feature therefore sets Groadmap's minor-version
floor at Go 1.26. Groadmap's own required Go version is higher than GoGraph's
minimum and is set independently of GoGraph. `BUILD.md § Go Toolchain` is the
authoritative statement of the required Go version and of the build implications.

### Dependency Maturity Risk

GoGraph is consumed at the exact tag **v0.11.0**. Because
v0.11.0 is a v0 (pre-1.0) version, it is consumable directly at the bare module path
`github.com/FlavioCFOliveira/GoGraph`, and `go.mod` pins the clean exact tag `v0.11.0`.
This exact-tag pin satisfies the pinning mitigation below directly. The pinned version
is recorded in `BUILD.md § Go Toolchain`.

As a `0.y.z` release, v0.11.0 signals under Semantic Versioning that GoGraph's public
API is not yet stable: it may change while the module matures toward `1.0.0`, and such
changes can land without a major-version bump. The following residual risks remain:

1. **Pre-1.0 API instability.** The engine constructors, result types, helper
   functions, and on-disk format named in this specification may change between
   `0.y` releases. A GoGraph upgrade can therefore alter the integration surface
   that this specification depends on.
2. **On-disk format change across pre-1.0 releases.** The store's snapshot and
   write-ahead-log format may change between `0.y` releases, which could make a graph
   written by one release unreadable by a later one. There is no graph-format
   migration mechanism in Groadmap in this version: Groadmap cannot convert a graph
   directory that the pinned engine refuses to open, so it depends entirely on the
   engine reading the format its predecessor wrote. An on-disk-format change that the
   newer engine does not read would therefore make an existing graph unreadable.

   A format change of this kind has already occurred, and the engine absorbed it. The
   snapshot's `labels.bin` component moved from format version 1 to format version 2:
   the edge record gained a slot field, so that a relationship type on parallel edges
   survives a checkpoint. Reading format version 1 is retained upstream as the
   deliberate upgrade path, so a graph directory written by the earlier release opens
   unchanged under the later one. The migration is **one-way**: because every
   successful Groadmap write checkpoints synchronously (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)), the first
   write after the upgrade rewrites `labels.bin` in place at format version 2, and the
   older release does not read format version 2. Reverting to an earlier pinned engine
   after a write is therefore not an available recovery step. Naming this component
   records the evidence behind mitigation 4 below; the internal layout of the graph
   directory remains owned by GoGraph and is not specified by Groadmap (see
   [Persistence Layout](#persistence-layout), rule 5).

3. **A widened Cypher clause surface.** The guard rail classifies a query by the
   clauses it contains, so the set of clauses the engine accepts is part of
   Groadmap's integration surface even though no Go symbol expresses it. When the
   engine learns a new clause family, queries that the previous engine rejected as
   a syntax error start executing, and each subcommand's accepted class widens
   without a line of Groadmap changing. This is invisible to the two checks an
   upgrade would otherwise rely on: a diff of removed or re-signed exported
   symbols finds nothing, because nothing was removed, and re-running the
   acceptance criteria finds nothing, because no existing criterion mentions a
   clause that did not previously exist.

   This has already happened. The engine gained the `FOREACH` updating clause and
   the `SHOW CONSTRAINTS` / `SHOW INDEXES` schema-introspection commands, and it
   extended its own `cypher/ir.IsDDL` predicate to report the latter as DDL —
   which, because `cypher.QueryHasWritingClause` returns false for anything
   `IsDDL` accepts, made a schema-introspection command classify as neither a
   write nor DDL and so pass the read-only check. The outcome is the one this
   specification now mandates (see
   [Schema Introspection](#schema-introspection)), but it was reached by omission
   rather than by decision, which is what mitigation 5 below exists to prevent.

Mitigations required by this specification:

1. Groadmap MUST pin GoGraph to an exact version in `go.mod` (a specific immutable
   reference, not a floating or branch reference), so builds are reproducible. The
   pinned exact tag is recorded in `BUILD.md § Go Toolchain`.
2. The graph feature MUST be implemented behind Groadmap's own command and
   error-handling boundary (this specification), so that an upstream API change
   is absorbed in one integration layer rather than spread across the codebase.
3. Upgrading GoGraph is a change that MUST be re-validated against the acceptance
   criteria in this file before release.
4. **An existing graph directory MUST remain readable across a GoGraph upgrade, and
   this MUST be demonstrated empirically rather than assumed.** Before an upgrade is
   released, a graph directory written under the previously pinned version MUST be
   opened with the new version and verified to read back the same content: the same
   node count, the same relationship count, and the same distribution of relationship
   types. A write MUST then be executed against that same directory and the
   verification repeated, so that the check covers both reading the old format and
   rewriting the directory in the new one. Backward compatibility MUST NOT be inferred
   from release notes alone, because Groadmap has no migration path of its own for a
   graph it can no longer open. An upgrade that fails this check MUST NOT be released.
5. **The guard rail's clause surface MUST be re-verified against the new engine,
   and every operation class this specification names MUST be pinned by a
   regression test.** Before an upgrade is released, the classes in
   [Operation Classes](#operation-classes) MUST be re-checked against the engine
   being adopted: each class MUST still be classified as specified, and any clause
   family the new engine accepts that this specification does not name MUST be
   classified deliberately — specified into an existing class or into a new one —
   rather than left to fall through the discriminators. A regression test MUST
   assert the accepted and rejected class of every subcommand for every named
   clause family, so that a later upgrade which widens the surface fails the test
   instead of passing unnoticed. Symbol-level compatibility is NOT sufficient
   evidence here: the surface can widen with no symbol change at all.

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
6. For write subcommands only, after the transaction has committed durably,
   produces a self-sufficient snapshot of the committed graph state and truncates
   the write-ahead log, synchronously, before the process exits (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)).
7. Closes the result and the store, ensuring committed writes are durable, then
   exits.

Parameter binding: when query parameters are supported, the implementation binds
them through GoGraph's parameter-binding path (`RunAny` / `RunInTxAny`, which
accept `map[string]any`, or `cypher.BindParams` followed by `Run` / `RunInTx`).

The exact Go types, function signatures, and any wrapper structs are
implementation details for `go-developer`; this specification fixes the
behaviour, not the Go API.

**Engine options.** The implementation constructs the engine with the pinned
engine's default options and MUST NOT disable a query-planner access path
without measured evidence, gathered against a graph of representative size, that
the default is worse for Groadmap's own workload. The engine ships planner
optimisations that can regress a particular query shape, and it documents them;
adopting a non-default option on the strength of an upstream note alone would be
tuning by assumption. The pinned engine's known selective-multi-label regression
is the worked example: it is reachable only through the parallel-scan tier, that
tier is gated on a live node count above a threshold of tens of thousands of
nodes, and a roadmap knowledge graph of a few hundred nodes cannot reach the gate
at all, so the option that would suppress it stays at its default and Groadmap
keeps the intra-query parallelism the default provides. The rule is that an
engine default is changed on evidence measured against this project's own graph,
never on an upstream release note.

### Synchronous Checkpoint on Write

Every successful graph write invocation produces a durable snapshot and truncates
the write-ahead log before the process exits. This step is synchronous: it runs
inside the same short-lived CLI invocation, not in a background goroutine. It
applies to the three write subcommands (`create`, `update`, `delete`) only; read
subcommands (`query`, `search`) never checkpoint.

Sequence and durability boundary:

1. The transaction commit is and remains the durability boundary. Once the write
   transaction has committed durably, the user's change is persisted in the
   write-ahead log and is guaranteed to survive recovery, independent of whether
   the checkpoint that follows succeeds.
2. After a successful commit, and before closing the store, the implementation
   writes a full snapshot of the committed graph state. The snapshot MUST be
   self-sufficient: it carries the node-identifier-to-key mapping needed to
   interpret the graph on its own, and it captures the set of deleted (tombstoned)
   nodes, so that the snapshot plus any write-ahead-log tail is enough to
   reconstruct the graph and truncating the log loses no committed data. Because
   the deletion tombstone set is part of the snapshot, a node deleted by a write
   stays deleted after the log is truncated and the store is reopened; it does not
   reappear on recovery.
3. After the self-sufficient snapshot is durable, the write-ahead log is
   truncated. Truncation bounds the log's growth: without it the log grows with
   every write for the life of the graph (see [Concurrency and Recovery](#concurrency-and-recovery)).

Failure policy:

1. A checkpoint failure that occurs **after** the transaction has already
   committed durably MUST NOT fail the user-visible write. The write has already
   succeeded.
2. In that case the command still returns its normal success output (the
   `RETURN`-mirroring shape or `{"ok": true}`) and exit code 0. A failed
   checkpoint after a durable commit is a degraded-but-correct state: the
   write-ahead log is intact, so recovery still restores the committed state, and
   the next successful write checkpoints again and reconciles the snapshot.
3. The checkpoint failure is surfaced through the existing error and
   observability conventions (a diagnostic on stderr, consistent with
   `HELP.md § Error message format`) **without** changing the exit code from 0.
   This is the one place where a non-fatal diagnostic may accompany a success
   exit code.
4. A failure that occurs **before or during** the commit (the transaction does
   not commit durably) is a normal write failure, not a checkpoint failure: the
   write did not succeed, no checkpoint is attempted, and the command fails with
   `utils.ErrDatabase` (exit code 1) per
   [Error Handling and Exit Codes](#error-handling-and-exit-codes).

Performance trade-off: a synchronous full snapshot on every write makes each
write cost proportional to the live graph size (the snapshot rewrites the
committed state), in exchange for a write-ahead log that stays bounded and a
recovery cost proportional to the live graph size rather than to the full write
history. This trade-off, and the explicit decision not to use a size-thresholded
or background checkpoint in this version, are recorded in
`IMPLEMENTATION.md § Graph Store Concurrency`.

The exact GoGraph snapshot and truncation calls, and any wrapper structs, are
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
    ├── wal               # Write-ahead log (truncated after each checkpoint)
    └── snapshot/         # On-disk snapshot, present after the first write
        ├── manifest.json   # Snapshot manifest (GoGraph-owned)
        ├── tombstones.bin  # Deleted-node tombstone set (present only when the graph has tombstoned nodes; GoGraph-owned)
        └── ...             # Snapshot data files (GoGraph-owned)
```

Rules:

1. The graph store is a **directory**, not a single file, because GoGraph
   persists through an on-disk snapshot plus a write-ahead log. The directory is
   `~/.roadmaps/<name>/graph/`.
2. The graph directory is created on first use of any `rmp graph` subcommand for
   that roadmap, including read subcommands. A read against a roadmap that has no
   graph yet creates an empty graph store and returns an empty result; it is not
   an error.
3. The `snapshot/` subdirectory (including its `manifest.json`) is produced by the
   synchronous checkpoint that runs after each successful write (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)). It is
   expected to be present after the first successful write subcommand. A graph
   that has only ever been read, or that has never been written, may contain only
   the `wal` entry and no `snapshot/` subdirectory.
4. The graph directory uses permissions `0700`, consistent with the roadmap home
   directory and the data directory (see `ARCHITECTURE.md § Directory Structure`).
5. The internal file names and on-disk format inside `graph/`, including the
   layout and contents of `snapshot/` and the format of `wal`, `manifest.json`,
   and `tombstones.bin`, are owned by GoGraph and are not specified here. The
   `tombstones.bin` component is optional: GoGraph emits it only when the graph
   has tombstoned (deleted) nodes, so a graph that has never had a node deleted
   need not contain it. Groadmap treats the directory as an opaque store managed
   through the engine; the diagram above names the `wal` entry and the `snapshot/`
   subdirectory only to document which entries are expected to appear, not their
   internal format.
6. Removing a roadmap (`rmp roadmap remove <name>`) deletes the entire roadmap
   home directory recursively, which includes `graph/`. No separate graph-removal
   command is required (see `COMMANDS.md § Remove Roadmap`).
7. The roadmap home directory layout, including the graph subdirectory, is
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
| `CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, `DROP CONSTRAINT` | Mutates the graph schema (indexes, constraints) | DDL (schema-mutating) |
| `SHOW INDEXES`, `SHOW INDEX`, `SHOW CONSTRAINTS`, `SHOW CONSTRAINT`, each with an optional `YIELD` / `WHERE` / `RETURN` projection tail | Lists the registered schema without altering it | Schema introspection (read-only) |
| `MATCH ... RETURN`, or a schema-introspection command, with no writing clause and no DDL clause | Reads and returns data | Read-only |

A query is a **writing query** when GoGraph's `cypher.QueryHasWritingClause`
reports that it contains any writing clause (`CREATE`, `MERGE`, `SET`, `REMOVE`,
`DELETE`, or `DETACH DELETE`). A query is **read-only** when it contains neither a
writing clause nor a DDL clause; a schema-introspection command is read-only.
The guard rail uses `QueryHasWritingClause` as the primary read-vs-write
discriminator, and additionally inspects which writing clauses are present to
distinguish creating, mutating, and deleting writes for the per-subcommand rules
below.

**DDL (Data Definition Language) in the guard-rail context.** DDL means a Cypher
clause that mutates the graph **schema** rather than its data: specifically
`CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, and `DROP CONSTRAINT`. These
clauses are schema-mutating and are therefore **not** read-only, even though the
two-word `CREATE INDEX` / `CREATE CONSTRAINT` forms begin with the `CREATE`
keyword and the `DROP` forms contain no data-writing clause. The guard rail MUST
detect a DDL clause independently of `QueryHasWritingClause`, because the
read-only contract of the read subcommands forbids any schema-mutating DDL, not
only data-writing (DML) clauses. A query that contains any DDL clause is treated as
schema-mutating for classification purposes. The detection of DDL clauses, like all
clause-class classification, runs on the masked normalization of the query (see
[Literal-Aware Normalization](#literal-aware-normalization)), so a DDL keyword that
appears only inside a string literal, a comment, or a backtick-quoted identifier
does not trigger DDL classification.

#### Schema Introspection

**Schema introspection is a read-only class of its own.** A schema-introspection
command — `SHOW INDEXES`, `SHOW CONSTRAINTS`, their singular aliases `SHOW INDEX`
and `SHOW CONSTRAINT`, and any of them followed by a `YIELD` / `WHERE` / `RETURN`
projection tail — lists the schema that is registered on the graph. It reads; it
creates, drops, and alters nothing. It is therefore **accepted by the read
subcommands** (`query` and `search`) and **rejected by the write subcommands**
(`create`, `update`, `delete`), each of which accepts only its own data-writing
clause class.

The guard rail MUST classify schema introspection **deliberately**, by
recognising the statement form, and MUST NOT arrive at the read-only verdict by
the absence of every other class. The distinction is not cosmetic: a verdict
reached because nothing matched cannot be reviewed and cannot be tested for
intent, and it silently absorbs whatever clause family the engine gains next.
Recognition is anchored to the start of the statement, so an identifier, a label,
or a property named `show` elsewhere in a query does not make that query an
introspection command. Like every other clause-class check it runs on the masked
normalization (see [Literal-Aware Normalization](#literal-aware-normalization)),
so a `SHOW` keyword that appears only inside a string literal, a comment, or a
backtick-quoted identifier does not trigger the classification.

**This classification is Groadmap's, and it is deliberately narrower than the
engine's.** GoGraph reports schema introspection as DDL from its own
`cypher/ir.IsDDL` predicate, which folds `SHOW` in with `CREATE INDEX` and its
siblings, and `cypher.QueryHasWritingClause` consequently reports a
schema-introspection command as **not** a writing query. Groadmap does not adopt
that grouping, because the two behave differently against the property this guard
rail protects: `CREATE INDEX` and `DROP INDEX` change the graph's schema, while
`SHOW INDEXES` only reports it. Groadmap's DDL class is therefore exactly the
four schema-**mutating** forms, and schema introspection is a separate,
read-only class. Where the engine's grouping and this specification disagree,
this specification governs what each subcommand accepts.

#### Literal-Aware Normalization

Clause-class classification MUST run on a **masked normalization** of the query,
never on the raw query string. The mask neutralizes the contents of Cypher
string literals so that a clause keyword that appears only inside a property
value can never affect classification.

Both discriminators operate on the masked query: the read-vs-write
determination (`cypher.QueryHasWritingClause`) AND the which-clauses-are-present
checks. The guard rail builds the masked string from the raw query and feeds
that masked string to BOTH discriminators. The query that is actually executed
against the store is always the **original, unmodified** query; masking affects
classification only.

Masking rules:

1. **String literals (mandatory).** Both single-quoted (`'...'`) and
   double-quoted (`"..."`) Cypher string literals are masked. Masking replaces
   the interior characters of each literal with a neutral placeholder character
   (for example, a space), while leaving the surrounding query structure intact.
   The quote delimiters and the overall positions of surrounding tokens are
   preserved so that clause detection sees the same query shape with only the
   literal contents neutralized.
2. **Backslash escape sequences.** While scanning a string literal, a backslash
   escape sequence (for example `\"`, `\'`, `\\`) does not terminate the literal:
   an escaped quote is part of the literal value, not its closing delimiter. The
   scanner honors these escapes so that a literal ends only at its true,
   unescaped closing quote.
3. **Comments and backtick identifiers (robustness).** For robustness, keyword
   text inside line comments (`// ...` to end of line), block comments
   (`/* ... */`), and backtick-quoted identifiers (`` `...` ``) MUST likewise not
   influence classification, and is masked under the same neutralization. The
   string-literal masking in rule 1 is the mandatory normative requirement; the
   comment and backtick-identifier masking is an additional robustness
   requirement applied by the same normalization.

The masked classification remains a pure clause-class check. It still does NOT
validate Cypher syntax: a syntactically invalid query that passes the masked
clause check is still passed to the engine and rejected there (see note 3 under
[Per-Subcommand Validation Rules](#per-subcommand-validation-rules) and
[Error Handling and Exit Codes](#error-handling-and-exit-codes)). Whitespace
trimming and all existing exit-code semantics (exit code 6 for an operation-class
mismatch) are unchanged.

### Per-Subcommand Validation Rules

Each subcommand accepts exactly the operation class listed below and rejects all
others. A query that does not match is rejected with `utils.ErrValidation`
(exit code 6) before execution; the graph is not opened for writing and no
change is made (see [Error Handling and Exit Codes](#error-handling-and-exit-codes)).
In the table below, every reference to a clause a query "contains" or to the
result of `QueryHasWritingClause` is evaluated on the masked normalization of the
query, not on the raw string (see
[Literal-Aware Normalization](#literal-aware-normalization)).

| Subcommand | Accepts | Rejects | Engine path |
|------------|---------|---------|-------------|
| `graph create` | A writing query whose only writing clauses are `CREATE` and/or `MERGE`. | Read-only queries; any query containing `SET`, `REMOVE`, `DELETE`, or `DETACH DELETE`; any DDL clause; any schema-introspection command. | Transactional write |
| `graph query` | A read-only query: `MATCH ... RETURN` with no writing clause and no DDL clause, or a schema-introspection command. | Any query for which `QueryHasWritingClause` is true (contains `CREATE`, `MERGE`, `SET`, `REMOVE`, `DELETE`, or `DETACH DELETE`); any query containing a DDL clause (`CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, `DROP CONSTRAINT`). | Read |
| `graph update` | A writing query whose writing clauses are `SET` and/or `REMOVE` (mutations on existing elements). | Read-only queries; queries containing `CREATE`, `MERGE`, `DELETE`, or `DETACH DELETE`; any DDL clause; any schema-introspection command. | Transactional write |
| `graph delete` | A writing query whose writing clauses are `DELETE` and/or `DETACH DELETE`. | Read-only queries; queries containing `CREATE`, `MERGE`, `SET`, or `REMOVE`; any DDL clause; any schema-introspection command. | Transactional write |
| `graph search` | A read-only query, intended for traversal and pattern matching, including variable-length paths (for example `-[*1..3]-`); a schema-introspection command is likewise accepted. | Any query for which `QueryHasWritingClause` is true; any query containing a DDL clause. | Read |

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
4. Classification ignores clause keywords that appear only inside Cypher string
   literals (mandatory), and likewise inside comments and backtick-quoted
   identifiers (robustness), because classification runs on the masked
   normalization described in
   [Literal-Aware Normalization](#literal-aware-normalization). Concretely:
   - `graph create` accepts
     `CREATE (m:Memory {body:"discusses delete, set and detach"})`, because the
     words `delete`, `set`, and `detach` appear only inside the property value
     and are masked before classification; the only real writing clause is
     `CREATE`.
   - `graph query` accepts
     `MATCH (m) WHERE m.title = "mentions delete and set" RETURN m.key`, because
     the masked query contains no writing clause and is therefore read-only.
5. **DDL is rejected by the read subcommands.** The read subcommands (`query` and
   `search`) accept only read-only queries; a schema-mutating DDL clause
   (`CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, or `DROP CONSTRAINT`) is
   **not** read-only and is rejected with `utils.ErrValidation` (exit code 6) and
   the message "graph query accepts only read-only queries". The read-only contract
   forbids schema-mutating DDL, not only data-writing (DML) clauses, because a DDL
   clause changes the graph's schema and is therefore not a read. The write
   subcommands (`create`, `update`, `delete`) likewise reject DDL: each accepts
   only its own data-writing clause class, and DDL is outside every one of those
   classes (see the Rejects column above).
6. **Schema introspection is accepted by the read subcommands and rejected by
   the write subcommands.** `graph query` and `graph search` accept a
   schema-introspection command because it reads the schema without altering it
   (see [Schema Introspection](#schema-introspection)). The write subcommands
   reject it for the same reason they reject a read-only `MATCH`: it carries none
   of the data-writing clauses they accept, so it is rejected with
   `utils.ErrValidation` (exit code 6) and that subcommand's own message.
7. **`FOREACH` is a writing clause and is classified by the clauses its body
   contains.** `FOREACH (x IN list | <updating clauses>)` runs its body once per
   list element. Its body may contain only `CREATE`, `MERGE`, `SET`, `REMOVE`,
   `DELETE`, `DETACH DELETE`, or a nested `FOREACH`, so every `FOREACH` that has
   an effect carries at least one of the six writing keywords, and the guard rail
   classifies it by those keywords: a `FOREACH` whose body sets a property is a
   mutating write valid only under `graph update`, one whose body creates is a
   creating write valid only under `graph create`, and every `FOREACH` is rejected
   by `graph query` and `graph search`. The classification is therefore correct
   without a `FOREACH` discriminator of its own, but it rests on that containment
   property rather than on the keyword `FOREACH`, so the property MUST be pinned
   by regression tests rather than left as an emergent consequence.

### Relationship Write Direction

A `SET` or `REMOVE` whose target is a **relationship variable** MUST bind that
variable with an **outgoing** relationship pattern (`-[e]->`). `graph update`
rejects a query that writes a relationship bound by an **incoming** (`<-[e]-`) or
**undirected** (`-[e]-`) pattern, with `utils.ErrValidation` (exit code 6), before
the graph store is opened. A rejected query changes nothing.

This is a **separate contract from the clause-class guard rail**, not another
operation class. The query's class is already correct — it is a mutating write
under the subcommand that accepts mutating writes — and what is refused is the
**orientation** of the pattern that binds the relationship being written. The
clause-class classification described above is unaffected, and so is the
read-only check the web graph data endpoint shares.

#### The traversal contract for SET on relationships

| Pattern binding the relationship | `SET` / `REMOVE` on that relationship | `graph update` |
|----------------------------------|----------------------------------------|----------------|
| `(a)-[e]->(b)` — outgoing | Writes every matched relationship | Accepted |
| `(a)<-[e]-(b)` — incoming | Writes nothing | Rejected, exit 6 |
| `(a)-[e]-(b)` — undirected | Writes only the relationships the traversal reaches along the stored arrow; silently skips the rest | Rejected, exit 6 |

The contract is a property of the **pattern**, not of the anchor: an incoming
pattern is rejected whether it is anchored on the relationship's source or on its
target, and an undirected pattern is rejected even when the data it happens to
match would all be written.

#### Why the reverse forms are refused rather than executed

The engine writes a relationship property by its **endpoint pair**, and it takes
that pair from the columns the expansion emitted. Those columns carry the
relationship the way the **pattern** walked it, not the way storage holds it, so
for a relationship reached against the stored arrow the pair is reversed. The
engine's **read** path corrects that orientation before it reports a
relationship, which is why `RETURN type(e)`, `startNode(e)` and `endNode(e)` are
right for the same match; its **write** path does not, so the write is addressed
to a pair that has no relationship, and the storage layer answers a write to an
absent relationship with a documented no-op. Nothing is written, no error is
raised, and the transaction still commits.

The divergence is upstream in GoGraph and cannot be corrected from this
repository. Groadmap therefore refuses the query, because the alternative — a
warning on stderr with exit 0 — still reports success for a write that did not
happen, which is the failure mode being removed. The engine's own write-effect
counters cannot be used to detect it after the fact: a reverse-leg `SET` that
wrote nothing still reports one property set, because the counter is incremented
above the layer that dropped the write.

#### Rewriting a refused query

Refusal removes no reach: **every** relationship stays writable through an
outgoing pattern, because an outgoing pattern may be anchored on either endpoint.
A relationship arriving at a node is written by anchoring the outgoing pattern on
that node rather than by reversing the arrow:

```
MATCH (other)-[e:VERIFIED_BY]->(target {key:'…'}) SET e.last_commit = '…'
```

The provenance idiom that stamps every relationship incident to a node is
therefore written as two outgoing statements, one per direction:

```
MATCH (n {key:'…'})-[e]->(other) SET e.last_commit = '…'
MATCH (other)-[e]->(n {key:'…'}) SET e.last_commit = '…'
```

Notes:

1. The check inspects the **target** of the `SET` / `REMOVE` only. A relationship
   a query merely traverses or reads is not affected: `MATCH (b)-[e]-(x) SET
   x.reviewed = true` writes a node, which the engine resolves by identifier
   rather than by endpoint pair, and is accepted. So is a relationship that
   appears only on the right-hand side of an assignment, as in
   `SET n.last_type = type(e)`.
2. The check applies to `graph update` only. `graph delete` is unaffected: it
   resolves the relationship itself rather than through the endpoint columns, and
   removes a relationship bound by a reverse traversal correctly. The read
   subcommands are unaffected: an undirected `MATCH … RETURN` is exactly the half
   of the behaviour that was always right, and it remains accepted. `graph create`
   cannot reach the condition, because the clause-class rules above already reject
   any creating query that contains `SET` or `REMOVE`.
3. A `FOREACH` body is inspected like a top-level `SET`, for the same reason the
   clause-class rules give it: `FOREACH (x IN list | SET e.k = …)` reaches the
   same write operator.
4. Detection runs on the **parsed** query rather than on the masked
   normalization, so the directions read are the directions the engine will plan.
   A relationship arrow that appears only inside a string literal or a comment is
   not pattern syntax to the parser and cannot trigger a rejection. A query the
   parser rejects is passed through to the engine unchanged, so a syntax error is
   reported as a syntax error rather than masked by a direction error.
5. Inserting a `WITH <relationship variable>` between the `MATCH` and the `SET`
   is **not** accepted as a substitute for an outgoing pattern, even though the
   projection it forces happens to repair the write in the pinned engine version.
   That repair is a consequence of projection materialisation, which the engine is
   free to elide; admitting the shape would make the guarantee depend on an
   unspecified optimisation decision and would fail open the day it changed.

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
4. When `--query` is present, its value is the token that immediately follows it.
   The command fails with `utils.ErrRequired` (exit code 2) whenever that value is
   absent. The value is absent in either of these cases:
   - There is no following token, or the following token is empty or contains only
     whitespace.
   - The following token is flag-like: it begins with `--` (a long flag), or with a
     single `-` immediately followed by an ASCII letter (a short flag). A flag-like
     token is the next flag the user supplied, not a query value, so it is never
     silently swallowed as the query.

   A following token that begins with `-` immediately followed by a digit or a
   decimal point (a negative numeric literal such as `-1` or `-0.5`) is not
   flag-like. It is a legitimate query value: the command accepts it and passes it
   to the engine like any other query, and the engine then accepts or rejects it on
   its own Cypher-validity merits.
5. Leading and trailing whitespace is trimmed from the query before the guard-rail
   check and before execution.

Standard input carries the Cypher query itself here: what the `graph` subcommands
read from it is the instruction they execute, not a value they store. Other
commands accept standard input as well, and the cross-cutting input rule is
stated in `DATA_FORMATS.md § Input`, which is the canonical statement of every
command that reads standard input: it lists the `--query` of the `graph`
subcommands together with the `--body` of the comment subcommands of the `task`
and `sprint` families.

## Query Notifications as Diagnostics

The Cypher engine may attach **advisory notifications** to a query result.
A notification is computed at parse and plan time and is available as soon as the
query has run; it is informational guidance, not an error. The classic example is
a Cartesian-product warning: a `MATCH` with two or more patterns that share no
variable forces the engine to combine every match of one pattern with every match
of the other, which can be expensive and is usually unintended.

Behaviour:

1. Every graph subcommand that executes a query (`create`, `query`, `update`,
   `delete`, and `search`) MUST, after the query has run, surface on stderr
   exactly the notifications the engine returns for that query, as a
   human-readable diagnostic line per notification. The surfacing is wired
   identically on the read path (`query`, `search`) and the write path
   (`create`, `update`, `delete`): each path emits whatever notifications the
   engine attaches to its result. Groadmap does not generate notifications and
   does not decide which queries or paths carry them; it only surfaces what the
   engine supplies, which may be none. If the engine attaches a notification to a
   given execution path, the corresponding subcommand surfaces it without further
   change.
2. Notifications are surfaced generically: the implementation emits whatever
   notifications the engine returns for the query, whatever their code, severity,
   or category. The set of notifications the engine produces may grow across
   GoGraph versions; this behaviour is not limited to any specific notification
   and does not hardcode the Cartesian-product case.
3. Each emitted diagnostic line is plain text, one line per notification, and
   includes at least the notification's severity, its stable machine-readable
   code, and its description, in a readable form. A representative line for the
   Cartesian-product warning reads:
   `INFORMATION Neo.ClientNotification.Statement.CartesianProductWarning: this query builds a cartesian product between disconnected patterns.`
4. Notifications are advisory and never change the outcome of the command. The
   stdout output is exactly the existing success output, unchanged: the
   `columns`/`rows` shape for a read or a `RETURN`-bearing write, or `{"ok": true}`
   for a write with no `RETURN` clause (see
   `DATA_FORMATS.md § Graph Query Result` and `DATA_FORMATS.md § Graph Write Result`).
   The exit code is unaffected and remains 0 on success.
5. A query that produces no notifications writes nothing extra to stderr.

This is consistent with the existing stderr-diagnostic pattern: notifications use
the same channel as the non-fatal checkpoint diagnostic on the write path (see
functional requirement 7 and
[Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)). A graph
write invocation may therefore emit, on stderr, both any query notifications and,
if a post-commit checkpoint fails, the non-fatal checkpoint diagnostic; neither
changes the success stdout output or the exit code.

The exact GoGraph notification accessor, the notification type, and its field
names are implementation details for `go-developer`; this specification fixes the
behaviour, not the Go API.

The set of notifications that exist, and which execution paths (read, write, or
both) carry them, are determined entirely by the backing engine. Groadmap's
contract is to surface, on each path, exactly what the engine returns for the
query it executed. Whether a notification appears for a given query on a given
path therefore follows the engine's behaviour, and this specification does not
promise that any particular subcommand will emit a notification for any
particular query.

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
| `graph update` writes a relationship bound by an incoming or undirected pattern (see [Relationship Write Direction](#relationship-write-direction)) | `utils.ErrValidation` | 6 |
| Cypher fails to parse or execute in the engine | `utils.ErrDatabase` | 1 |
| Graph store cannot be opened, recovered, read, or written (I/O, corruption, lock) | `utils.ErrDatabase` | 1 |
| Successful execution | — | 0 |

Rules:

1. The guard-rail rejection (operation class mismatch) is detected before the
   graph store is opened for writing. A rejected query never mutates the graph.
   The relationship-write-direction rejection is detected at the same point and
   carries the same guarantee.
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

GoGraph's store is transactional, and MVCC is its only concurrency-control
mechanism. Reads observe a consistent committed state. Independent write
transactions are not excluded from one another inside a single process: a
write-write collision is detected rather than prevented, on a first-updater-wins
basis, and the losing transaction receives a retriable serialization-conflict
error. Groadmap does not rely on that intra-process behaviour, because each
`rmp graph` invocation is a separate short-lived process that runs exactly one
transaction; that one-transaction-per-process model is why the conflict path is
not reachable today.

Groadmap does not depend on the engine to serialise its writers. It serialises
them itself, at the process level: before opening the store, a write invocation
acquires an exclusive, non-blocking advisory lock on the roadmap's graph
directory, and holds it across the whole open, commit, checkpoint, and
write-ahead-log truncation sequence. A second write invocation that finds the lock
held fails immediately rather than waiting. The operating system releases the lock
when the holding process exits, so an invocation that crashes does not strand it.
Read invocations do not take this lock.

The lock covers the full sequence, not just the transaction, because that is the
span that must not interleave: a second writer that had loaded the graph before the
first writer's commit would checkpoint a full snapshot of its own stale in-memory
graph and then truncate the write-ahead log that still held the first writer's
committed change, silently losing an acknowledged write. Because the sequence
Groadmap needs serialised is wider than a transaction, no engine-level writer
exclusion would have covered it in any case.

Durability is provided by a write-ahead log with CRC32C integrity checks plus
atomic on-disk snapshots; on opening the store, GoGraph runs recovery to restore
the last committed state from the snapshot and log.

Groadmap's usage model and expectations:

1. Each `rmp graph` invocation is a short-lived process that opens the store,
   runs one query, commits any write, checkpoints after a successful write (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)), and
   closes the store. The process does not hold the store open across invocations.
2. Because a write invocation takes that exclusive lock before opening the store,
   two concurrent `rmp graph` write invocations against the **same** roadmap
   contend for it. The implementation MUST surface a contention or lock failure as
   `utils.ErrDatabase` (exit code 1) rather than corrupting the store or hanging
   indefinitely. The checkpoint that follows a successful write runs inside the
   invocation that already holds the lock: it adds no separate lock, does not
   change the read path, and two concurrent writers still serialise. The retry and
   timeout behaviour for graph writes is specified in
   `IMPLEMENTATION.md § Graph Store Concurrency`.
3. Recovery on open restores the last committed state from the snapshot and the
   write-ahead-log tail. Because every successful write now writes a self-sufficient
   snapshot and truncates the log (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)), recovery
   genuinely exercises the snapshot path: a graph opened after a previous write is
   rebuilt from that snapshot plus any log entries written since the last
   checkpoint, rather than by replaying the entire write history. The restored
   state includes deletions: a node deleted by a previous invocation stays deleted
   after the store is reopened, because the snapshot records the tombstone set and
   recovery reconstructs it. A graph left in a consistent committed state by a
   previous invocation opens cleanly. A graph whose store is corrupt or unreadable
   surfaces as `utils.ErrDatabase` (exit code 1); there is no automatic graph-store
   repair in this first version.
4. The graph store is independent of the SQLite layer and the SQLite WAL
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
14. After a successful `rmp graph create -r <roadmap> --query "..."`, the snapshot
    manifest `~/.roadmaps/<roadmap>/graph/snapshot/manifest.json` exists, proving a
    checkpoint ran (see [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)).
15. After a successful write subcommand and its checkpoint, the write-ahead log
    `~/.roadmaps/<roadmap>/graph/wal` is truncated (small or empty), proving the
    log was bounded rather than left to grow with history.
16. After a successful write and its checkpoint, a subsequent read in a
    **separate** invocation returns the written data, proving recovery from the
    snapshot plus any log tail works across process exits.
17. When the checkpoint fails after the write transaction has already committed
    durably, the write subcommand still returns its normal success output (the
    `RETURN`-mirroring shape or `{"ok": true}`) and exit code 0, and the checkpoint
    failure is reported as a diagnostic on stderr without changing the exit code
    (see [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)).
18. `rmp graph create -r <roadmap> --query 'CREATE (m:Memory {body:"... node-delete ... MATCH...SET ..."})'`
    is accepted and creates the node, exits 0. The `delete` and `set` keywords
    appear only inside the string-literal property value and are masked before
    classification (see
    [Literal-Aware Normalization](#literal-aware-normalization)); the only
    writing clause is `CREATE`.
19. `rmp graph query -r <roadmap> --query 'MATCH (m) WHERE m.title = "mentions delete and set" RETURN m.key'`
    is accepted as read-only and exits 0. The `delete` and `set` keywords appear
    only inside the string-literal value and are masked, so the masked query
    contains no writing clause.
20. `rmp graph query -r <roadmap> --query "MATCH (a:Spec), (b:Code) RETURN a.key, b.path"`
    runs a disconnected multi-pattern `MATCH` and surfaces a Cartesian-product
    notification on stderr (a plain-text line carrying at least the severity, the
    stable code, and the description). The stdout JSON is exactly the normal
    `columns`/`rows` result, unchanged by the notification, and the exit code is 0
    (see [Query Notifications as Diagnostics](#query-notifications-as-diagnostics)).
21. `rmp graph query -r <roadmap> --query "MATCH (s:Spec) RETURN s.key"`, a query
    that produces no notifications, writes nothing extra to stderr: stderr is empty
    on success, while stdout carries the normal result and the exit code is 0.
22. Notification surfacing is wired on both the read path and the write path,
    and each path surfaces exactly the notifications the engine returns for the
    query it executed:
    - Read path: a read subcommand (`query` or `search`) running a query for
      which the engine returns a notification emits the corresponding diagnostic
      line on stderr; its stdout success output and exit code 0 remain unchanged.
    - Write path: a write subcommand (`create`, `update`, or `delete`) surfaces
      exactly the notifications the engine returns for the committed query, which
      may be none. When the engine returns no notification for the write path,
      the subcommand emits no notification line; in all cases its stdout JSON
      success output and exit code 0 remain unchanged.
    This holds with the pinned engine, where the write path returns no
    notification, and remains correct without further change if a future engine
    attaches notifications to the write path.
23. `rmp graph query -r <roadmap> --query "SHOW INDEXES"` is accepted as a
    schema-introspection command and exits 0, returning the engine's schema
    listing in the normal `columns`/`rows` shape. The same holds for
    `SHOW CONSTRAINTS`, for the singular `SHOW INDEX` / `SHOW CONSTRAINT`
    aliases, for any of them under `graph search`, and for a form carrying a
    `YIELD` / `WHERE` / `RETURN` projection tail such as
    `SHOW INDEXES YIELD name RETURN name`.
24. `rmp graph create`, `rmp graph update`, and `rmp graph delete` each reject
    `SHOW INDEXES` with exit code 6 and that subcommand's own guard-rail message,
    because a schema-introspection command carries none of the data-writing
    clauses those subcommands accept.
25. A `SHOW` keyword that appears only inside a string literal, a comment, or a
    backtick-quoted identifier does not make a query a schema-introspection
    command, and an identifier, label, or property named `show` elsewhere in a
    query does not either: for example
    `rmp graph query --query 'MATCH (n) WHERE n.title = "SHOW INDEXES" RETURN n.key'`
    is accepted as an ordinary read, and
    `rmp graph create --query 'CREATE (n:Panel {show:"indexes"})'` is accepted as
    an ordinary creating write.
26. `rmp graph query --query "MATCH (n:Spec) FOREACH (x IN [1] | SET n.seen = true)"`
    is rejected with exit code 6, and so is the same query under `graph search`,
    because `FOREACH` is classified by the writing clauses its body contains. The
    same query is accepted by `graph update` and rejected by `graph create` and
    `graph delete`; a `FOREACH` whose body creates is accepted by `graph create`
    and rejected by `graph update` and `graph delete`; and a nested `FOREACH` is
    classified by the innermost body's writing clauses in the same way.
27. The four schema-mutating DDL forms remain rejected by every subcommand with
    exit code 6 regardless of keyword case and of the amount of whitespace
    between the two keywords: `create index`, `CREATE   INDEX`, `Drop Constraint`
    and their siblings are each rejected exactly as the canonical spelling is.
28. For one relationship `(s:Spec)-[:VERIFIED_BY]->(v:Test)`, an outgoing
    `SET` writes and reads back from **either** endpoint: both
    `MATCH (s:Spec {key:'…'})-[e:VERIFIED_BY]->(v) SET e.last_commit = '…'` and
    `MATCH (other)-[e:VERIFIED_BY]->(v:Test {key:'…'}) SET e.last_commit = '…'`
    exit 0, and a subsequent read reports the value on that relationship. This
    is what makes the rejections below cost no reach (see
    [Relationship Write Direction](#relationship-write-direction)).
29. The same `SET` written through a reverse pattern is rejected with exit code 6
    and writes nothing: `MATCH (v:Test {key:'…'})-[e]-(x) SET e.last_commit = '…'`
    and `MATCH (v:Test {key:'…'})<-[e:VERIFIED_BY]-(s) SET e.last_commit = '…'`
    each fail, a read-back reports the property absent, and the error message
    names the relationship variable, the direction that would have been skipped,
    and the outgoing rewrite. An undirected pattern is rejected even when
    anchored on the relationship's source, where every matched relationship would
    in fact have been written.
30. The rejection does not spread beyond `graph update`'s relationship writes:
    `MATCH (v:Test {key:'…'})-[e]-(x) SET x.reviewed = true` is accepted (the
    write targets a node), `MATCH (v:Test {key:'…'})-[e]-(x) DELETE e` is
    accepted by `graph delete` and removes the relationship, and
    `MATCH (v:Test {key:'…'})-[e]-(x) RETURN type(e)` is accepted by
    `graph query` and reports the incoming relationship.

## See Also

- CLI command contract for `graph` → `COMMANDS.md § Graph Management`
- Graph query result JSON and property-type mapping → `DATA_FORMATS.md § Graph Query Result`
- Standard input as a Cypher source → `DATA_FORMATS.md § Input`
- GoGraph integration, directory layout, error handling → `ARCHITECTURE.md`
- Go 1.26 toolchain bump and the GoGraph dependency → `BUILD.md § Go Toolchain`
- Writer serialisation, recovery, write contention, and the synchronous checkpoint trade-off → `IMPLEMENTATION.md § Graph Store Concurrency`
- Help skeleton and AI-help entry for `graph` → `HELP.md`
