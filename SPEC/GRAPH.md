# Knowledge Graph

## Table of Contents

- [Overview](#overview)
- [Functional Requirements](#functional-requirements)
- [Backing Engine: GoGraph](#backing-engine-gograph)
  - [Dependency](#dependency)
  - [Dependency Maturity Risk](#dependency-maturity-risk)
  - [Engine Construction and Lifecycle](#engine-construction-and-lifecycle)
  - [Engine Constructor by Path](#engine-constructor-by-path)
  - [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)
- [Persistence Layout](#persistence-layout)
- [Multi-Layer Modelling Conventions](#multi-layer-modelling-conventions)
  - [Node Key Uniqueness](#node-key-uniqueness)
- [Subcommands and Guard-Rail Validation](#subcommands-and-guard-rail-validation)
  - [Operation Classes](#operation-classes)
  - [Per-Subcommand Validation Rules](#per-subcommand-validation-rules)
  - [Relationship Write Direction](#relationship-write-direction)
  - [Relationship Read Direction](#relationship-read-direction)
  - [Cypher Query and Property Value Content Rules](#cypher-query-and-property-value-content-rules)
  - [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)
- [Query Notifications as Diagnostics](#query-notifications-as-diagnostics)
- [Error Handling and Exit Codes](#error-handling-and-exit-codes)
- [Concurrency and Recovery](#concurrency-and-recovery)
  - [What a Read Changes on Disk](#what-a-read-changes-on-disk)
  - [Lock Contention](#lock-contention)
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
   input when the flag is absent, and never from a positional argument: a query
   written bare on the command line is refused (see
   [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)).
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
   subcommands (`query`, `search`) never checkpoint and never truncate the
   write-ahead log. What a read does change on disk, which is limited to the
   repair of an interrupted checkpoint that opening the store performs, is
   specified in [What a Read Changes on Disk](#what-a-read-changes-on-disk).
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
2. Takes the store's advisory lock, before opening the store: exclusively for a
   write subcommand, in shared mode for a read subcommand (see
   [Concurrency and Recovery](#concurrency-and-recovery)).
3. Opens the GoGraph store rooted at that directory, recovering any committed
   state from the snapshot and write-ahead log. **For a read subcommand, the
   shared lock is released as soon as this step returns**; every step below runs
   with no lock held. A write subcommand keeps the exclusive lock until step 7
   has completed.
4. Constructs the Cypher engine that will run the query. The two paths construct
   it differently. A **write** subcommand wraps the recovered graph and a
   write-ahead-log writer in a transactional store and constructs a store-backed
   engine over that store. A **read** subcommand constructs the engine directly
   over the graph the previous step recovered, and opens neither a transactional
   store nor a write-ahead-log writer. Which constructor each path uses, and why
   a read needs no store, are stated once, in
   [Engine Constructor by Path](#engine-constructor-by-path).
5. Runs the validated query:
   - Read subcommands (`query`, `search`) run through the engine's read path
     (`Run` / `RunAny`).
   - Write subcommands (`create`, `update`, `delete`) run through the engine's
     transactional path (`RunInTx` / `RunInTxAny`) so the change is committed
     atomically.
6. Iterates the result for read subcommands (`Columns`, then `Next` / `Record`
   until exhausted, checking `Err`), serialises it to JSON, and writes it to
   stdout.
7. For write subcommands only, after the transaction has committed durably,
   produces a self-sufficient snapshot of the committed graph state and truncates
   the write-ahead log, synchronously, before the process exits (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)).
8. Closes the result and the store, ensuring committed writes are durable, then
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

### Engine Constructor by Path

**This section is the single authoritative statement of which GoGraph engine
constructor Groadmap uses on each path.** No other section of this specification,
and no other SPEC file, states it independently; every one of them refers here
instead. The table covers every Cypher engine Groadmap constructs in order to
serve a `graph` subcommand or a web graph request.

| Path | Surface | GoGraph constructor | Transactional store and write-ahead-log writer |
|------|---------|---------------------|------------------------------------------------|
| Read | `graph query` and `graph search`, including the schema-introspection commands they accept | `cypher.NewEngine`, over the graph the store open recovered | Neither is opened |
| Read | The web graph page and the web graph data endpoint (see `WEB.md § Knowledge Graph from the GoGraph Store`) | `cypher.NewEngine`, over the graph the store open recovered | Neither is opened |
| Transactional write | `graph create`, `graph update`, and `graph delete` | `cypher.NewEngineWithStore`, over a transactional store | Both are opened: the write-ahead-log writer over `wal`, and the transactional store over the recovered graph and that writer |

The two path names are the ones the **Engine path** column of
[Per-Subcommand Validation Rules](#per-subcommand-validation-rules) uses. Three
surfaces run on those two paths: the read path serves both the CLI read
subcommands and the web interface, and the two are not distinguished here because
they construct the same engine.

Groadmap constructs an engine through no other constructor. The pinned engine
also exposes `NewEngineWithOptions`, `NewEngineWithRegistry`,
`NewEngineWithStoreAndConstraints`, and `NewEngineWithStoreAndSchema`; Groadmap
uses none of the four, and adopting one is a change to this table before it is a
change to the code.

**Why a read needs no store.** Opening the store — step 3 of
[Engine Construction and Lifecycle](#engine-construction-and-lifecycle) — runs
GoGraph's recovery, and recovery returns a graph that already carries the last
committed state, replayed from the snapshot and from the write-ahead-log tail. A
store-backed engine over that same state would observe the same graph and produce
the same results, so a transactional store gives a read nothing further to read.
It does cost something: constructing one requires opening a write-ahead-log
writer, which is a write-side resource, and a read would then hold that writer
for the whole of a query that commits nothing. The cost falls hardest on the web
interface, which drives the read path on every graph page and every graph data
request and which must stay strictly read-only. Constructing the plain engine is
also what keeps the read path's on-disk guarantees simple to state and to test:
once the open returns, the query runs against an in-memory graph and touches no
file in the store (see [Concurrency and Recovery](#concurrency-and-recovery) and
[What a Read Changes on Disk](#what-a-read-changes-on-disk)).

**The asymmetry between the two paths is deliberate, not an omission.** A reader
who notices that the write path is store-backed while the read path is not MUST
NOT align the two on that ground alone: making a read store-backed would hand a
read subcommand, and the web server, a write-ahead-log writer neither has a use
for, in exchange for no observable difference in any result. Moving a path to a
different constructor means amending this table first, and it requires the same
kind of measured evidence that
[Engine Construction and Lifecycle](#engine-construction-and-lifecycle) demands
before an engine option is changed. The one condition that would force the change
is a read that must observe state the recovery does not return, and no such read
exists.

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
    ├── write.lock        # Groadmap's store access lock (see Concurrency and Recovery)
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
   that has only ever been read has no `snapshot/` subdirectory and no `wal` file:
   a read creates neither, because the write-ahead log is created by the write
   path and the snapshot by the checkpoint. Such a directory holds only
   `write.lock`, and it holds that only once a read has actually opened the store.
4. The graph directory uses permissions `0700`, consistent with the roadmap home
   directory and the data directory (see `ARCHITECTURE.md § Directory Structure`).
5. The internal file names and on-disk format inside `graph/`, including the
   layout and contents of `snapshot/` and the format of `wal`, `manifest.json`,
   and `tombstones.bin`, are owned by GoGraph and are not specified here. The one
   exception is `write.lock`, which GoGraph knows nothing about: Groadmap creates
   and maintains it, and it is specified in
   [Concurrency and Recovery](#concurrency-and-recovery). Its contents are never
   read or written; only the advisory lock on it carries meaning. The
   `tombstones.bin` component is optional: GoGraph emits it only when the graph
   has tombstoned (deleted) nodes, so a graph that has never had a node deleted
   need not contain it. Apart from `write.lock`, Groadmap treats the directory as an
   opaque store managed through the engine; the diagram above names the
   `write.lock` and `wal` entries and the `snapshot/` subdirectory only to
   document which entries are expected to appear, not their internal format, and
   it is not an exhaustive listing of what the engine may place there.
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

### Node Key Uniqueness

Convention 2 above recommends giving each node a stable identifier property. The
project's own knowledge graph uses `key` for that purpose, and `knowledge-model.md`
at the repository root states that every node carries one. This subsection is
canonical for what the uniqueness of that property means, for which comparison
decides that two keys are the same, and for who is responsible for holding the
property true.

**The invariant.** Within one knowledge graph, no two nodes carry the same `key`,
so that `MATCH (n {key:'...'})` written without a label binds at most one node.
Two keys are **the same key** when their **Unicode Normalization Form C** forms
are equal — NFC, the canonical composition of the full canonical decomposition, as
UAX #15 defines it. The comparison is defined on string values, which is what the
identifier convention gives `key`.

**Groadmap does not enforce this invariant. It is a convention the caller
honours.** No `rmp` command rejects, rewrites, deduplicates, or reports a second
node carrying a key that is already in use — neither under the NFC comparison
above nor under byte equality. Three properties of the product produce that
outcome together, and every one of them would have to change for enforcement to
exist:

1. **The graph carries no uniqueness constraint, and no `rmp` command can declare
   one.** GoGraph supports `CREATE CONSTRAINT ... IS UNIQUE`, but constraint DDL
   belongs to the DDL class in [Operation Classes](#operation-classes), and all
   five subcommands reject that class (see the Rejects column of
   [Per-Subcommand Validation Rules](#per-subcommand-validation-rules)). Groadmap
   emits no constraint DDL of its own on any code path either, so nothing in the
   product ever places a uniqueness constraint on a graph.
2. **A node's identity in the store is not its `key`.** GoGraph identifies a node
   by an internal `uint64`, which `DATA_FORMATS.md § Graph Query Result` describes
   as ephemeral and explicitly not a stable business key. Two nodes carrying the
   same `key` are two identities to the store, and nothing in the engine relates
   one to the other.
3. **Keys are compared only inside the caller's own Cypher.** A pattern such as
   `MATCH (n:Spec {key:'...'})` compares the literal against the stored value as
   strings, byte for byte. The engine performs no normalisation of its own and
   offers the caller none: `normalize` is not in GoGraph's function registry, and a
   query that calls it is refused as an unknown function.

**Normalisation is for comparison only.** The `key` a node carries is exactly the
bytes the caller supplied. Groadmap does not normalise a key on the way in, does
not store a normalised form beside it, and does not normalise it on the way out;
what `rmp` stores and renders is the caller's own text. NFC decides only whether
two keys count as the same key when the convention is being judged. This is the
same rule the board search already applies to task text
(`WEB.md § Roadmap Tasks Page`), and it is stated the same way here so that the
product does not hold two answers to one question.

#### What the convention means in practice

The consequence of enforcing nothing and comparing on NFC is that the two halves
of the rule reach different things, and a reader has to be able to predict which:

1. **`MATCH (n {key:'...'})` matches by bytes, not by NFC.** A caller who writes
   one spelling of a key reaches only the node whose stored key is byte-for-byte
   that spelling. NFC is the specification's comparison for judging the
   convention; it is not the engine's comparison for binding a pattern, and no
   part of the product makes it so.
2. **Two spellings of one key under NFC are therefore two nodes**, indistinguishable
   wherever `rmp` renders them, since each renders the text the caller supplied and
   the two render identically. Either spelling binds exactly one of the two, and
   neither binds both.
3. **That is a caller error, and the product does not prevent it.** What the
   product owes instead is that the error can be **found**: the condition is
   detectable after the fact by the audit below. The state this specification
   closes is not that the invariant can be broken — under a convention it always
   can — but that breaking it used to leave no trace.
4. **The byte-wise duplicate audit does not report this condition, by
   construction.** `MATCH (n) WHERE n.key IS NOT NULL RETURN n.key, count(*)`
   groups on the stored bytes, so two spellings of one key are two groups of one,
   and the audit reports no duplicate while rendering its two rows identically.
   That query remains correct for what it checks — nodes that repeat a key
   byte-for-byte — and the audit below is its companion, not its replacement.

Today every key in the project's own graph is a repository-relative path, a Go
package path, or an ASCII slug, and NFC is the identity on ASCII text, so the
condition is latent rather than present. It becomes reachable the first time a key
carries a character outside ASCII.

#### Auditing the convention

Because the invariant is a convention, the specification owes a way to detect a
violation rather than a rule that prevents one. **The audit runs in two steps, and
the second is outside the engine**: GoGraph's function registry holds no
normalising function, so no single Cypher query can group keys by their NFC form.
A query that claimed to would not run.

**Step 1 — read every key, with `rmp graph query`.** This query is read-only and
is accepted by both read subcommands:

```cypher
MATCH (n) WHERE n.key IS NOT NULL
RETURN id(n) AS id, labels(n) AS labels, n.key AS key
ORDER BY key, id
```

**Step 2 — group the returned keys by their NFC form.** Report every group that
holds more than one distinct byte sequence. Each such group is one violation of
the invariant, and the `id` and `labels` columns of its rows identify the nodes
that carry it. An audit that finds no such group has found no violation.

Notes:

1. Step 1 returns **every** keyed node, not a candidate subset. The audit is
   therefore incapable of missing a violation by narrowing to the wrong candidates,
   which is the failure a cleverer query would risk. The cost is the full key list,
   which the graph of a single roadmap returns in one response.
2. `ORDER BY key, id` orders by the stored bytes and not by the NFC form, so it
   does **not** bring the members of a violating group together. It is there to make
   the response deterministic for a given graph, so that the audit's output can be
   compared across runs.
3. `id` is GoGraph's internal identifier and is **ephemeral**, exactly as
   `DATA_FORMATS.md § Graph Query Result` requires. The audit uses it to tell the
   nodes of one group apart within a single response, and it MUST NOT be recorded
   as the way to reach a node afterwards.
4. The audit reads; it changes no node and no key. Resolving a violation it reports
   is the caller's decision, because only the caller knows which of the two
   spellings the artefact is meant to carry.

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
| `SHOW INDEXES`, `SHOW INDEX`, `SHOW CONSTRAINTS`, `SHOW CONSTRAINT`, each written with exactly one space between the two keywords (see [Keyword Spacing in a Schema-Introspection Command](#keyword-spacing-in-a-schema-introspection-command)) and each with an optional `YIELD` / `WHERE` / `RETURN` projection tail | Lists the registered schema without altering it | Schema introspection (read-only) |
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
backtick-quoted identifier does not trigger the classification. Recognition is
also exact about the spacing between the two keywords, for reasons given in
[Keyword Spacing in a Schema-Introspection Command](#keyword-spacing-in-a-schema-introspection-command).

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

#### Keyword Spacing in a Schema-Introspection Command

**The guard rail recognises a schema-introspection command only when exactly one
space separates `SHOW` from the keyword that follows it.** `SHOW INDEXES` is a
schema-introspection command. The same statement written with two spaces, with a
tab, with a line break, or with a comment between the two keywords is not, and
neither is any other spelling that places anything except a single space there.

The rule governs that one separator and nothing else about the statement's
spacing. Whitespace and comments **before** `SHOW` are accepted, and so is any
amount of whitespace **after** the target keyword, including before a `YIELD` /
`WHERE` / `RETURN` projection tail. `SHOW INDEXES  YIELD name` is a
schema-introspection command; `SHOW  INDEXES YIELD name` is not. Like every other
clause-class check, this one runs on the masked normalization of the query and
never on the raw string (see
[Literal-Aware Normalization](#literal-aware-normalization)); masking neutralizes
a comment to spaces, which is why a comment between the two keywords reads as
spacing rather than as an absent separator.

**The rule is the engine's, not Groadmap's.** GoGraph decides
whether to route a statement to its schema-introspection parser by testing that
statement against the literal prefixes `SHOW CONSTRAINT` and `SHOW INDEX`, each
carrying exactly one space. It trims leading whitespace and leading comments
before applying that test, which is exactly why the separator between the two
keywords is the only spacing that matters. A statement that misses those prefixes
by its spacing never reaches the introspection parser: the engine routes it to
the general Cypher grammar, which has no `SHOW` production and rejects it as a
syntax error.

**The guard rail MUST classify by what the engine accepts, and MUST NOT apply a
broader grammar of its own.** A classification wider than the engine's admits a
statement the engine then refuses, and the user receives a diagnostic that names
the wrong problem: the engine reports `SHOW` as unexpected and lists the clause
keywords it did expect, none of which is `SHOW`, so the message reads as though
schema introspection were unsupported — while the identical statement with a
single space returns its result set. Nothing in that message points at the
spacing, so the user cannot reach the real cause from what is printed. The fault
in that outcome is the classification, not the engine's diagnostic; the
diagnostic is only where the fault becomes visible.

**A statement rejected under this rule is rejected by the guard rail, not by the
engine.** A `SHOW INDEX(ES)` or `SHOW CONSTRAINT(S)` statement whose keyword
spacing the engine does not accept is rejected with `utils.ErrValidation` (exit
code 6) and the guard rail's own message, before the query is handed to the
engine. The message MUST name the cause: that the statement was read as a
schema-introspection command, that exactly one space is required between the two
keywords, and what the accepted spelling is. It MUST NOT be the engine's parse
diagnostic, and this rejection MUST NOT surface with the exit code 1 that an
engine parse failure carries (see
[Error Handling and Exit Codes](#error-handling-and-exit-codes)). Exit code 1 for
a genuine engine parse failure is unchanged; a statement rejected here never
reaches the parser.

**Rejecting is the specified behaviour; normalizing the spacing is not.**
Groadmap MUST NOT rewrite the separator and execute the repaired statement.
Normalizing would silently alter a query the user wrote, and it would make
Groadmap the party that decides which Cypher the engine ought to have accepted.
The accepted statement surface belongs to the engine (see
[Dependency Maturity Risk](#dependency-maturity-risk), risk 3). The user is told
what is wrong and rewrites the statement.

**The exactness applies to this class alone and MUST NOT be carried over to the
DDL class.** DDL matching stays tolerant of arbitrary whitespace between `CREATE`
or `DROP` and `INDEX` or `CONSTRAINT`, and the asymmetry between the two is
deliberate. The two classes are matched for opposite purposes, so being wider
than the engine has opposite consequences for each. The guard rail matches DDL in
order to **refuse** it: a match wider than the engine's can only refuse more than
is strictly necessary, which is safe, whereas a narrower one would let a
schema-mutating statement past the check that exists to stop it. The guard rail
matches schema introspection in order to **admit** it: a match wider than the
engine's admits statements the engine then refuses, which is the misdiagnosis
described above. A reader who notices that the two matchers treat whitespace
differently MUST NOT align them on that ground: narrowing the DDL matcher would
reopen a guard-rail hole, and the difference is intentional.

**The rule holds identically on every surface that accepts this class.**
`graph query`, `graph search`, and the read-only web graph data endpoint apply one
shared classification, so a statement rejected under this rule on one of them is
rejected on all three. On the CLI the rejection is `utils.ErrValidation` and exit
code 6, as above. The web graph data endpoint rejects the query before executing
it and answers HTTP `400 Bad Request` with the failure class
`invalid_keyword_spacing` and a message that likewise names the keyword spacing.
That class is its own: the endpoint MUST NOT report the query as not read-only,
because a `SHOW` statement is read-only whatever its spacing, and the spelling is
the whole of the objection. `WEB.md § Query-Bar Error Handling` is canonical for
the endpoint's failure classes and for the precedence between them, and
`DATA_FORMATS.md § Graph View Data`, **Error Shape**, is canonical for the
response body. Because the rejection precedes execution, the endpoint's own
decision about injecting a node `LIMIT` into the query is never reached for such
a statement.

**The write subcommands are unaffected by this rule.** `graph create`,
`graph update`, and `graph delete` reject a `SHOW` statement on its operation
class, with that subcommand's own message, whatever its spacing — the objection
that it carries none of the data-writing clauses they accept holds for the
well-formed spelling too (see
[Per-Subcommand Validation Rules](#per-subcommand-validation-rules), note 6).

**This rule MUST be re-verified on every GoGraph upgrade.** It states a property
of the pinned engine, so it falls under the clause-surface re-verification that
[Dependency Maturity Risk](#dependency-maturity-risk) mitigation 5 requires before
an upgrade is released. If a later engine recognises other spacing, this section
and the classification MUST be updated together, so that the guard rail and the
engine do not disagree about the same input again.

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
[Literal-Aware Normalization](#literal-aware-normalization)). The **Engine path**
column names which of the two execution paths the subcommand runs on; the engine
constructor each of those paths uses is fixed by
[Engine Constructor by Path](#engine-constructor-by-path).

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
8. **A schema-introspection command is accepted only in the spelling the engine
   routes to its introspection parser.** `graph query` and `graph search` accept
   the class only when exactly one space separates `SHOW` from the target
   keyword. A `SHOW INDEX(ES)` or `SHOW CONSTRAINT(S)` statement written with any
   other separator is rejected with `utils.ErrValidation` (exit code 6) and a
   message naming the keyword spacing, before the query reaches the engine,
   rather than admitted and left to fail at the parser (see
   [Keyword Spacing in a Schema-Introspection Command](#keyword-spacing-in-a-schema-introspection-command)).
   This changes nothing for the write subcommands, which reject the statement on
   its class under note 6 whatever its spacing.

### Relationship Write Direction

Two rules govern the **direction** of the pattern that binds a relationship
variable: this one, which governs writing that relationship, and
[Relationship Read Direction](#relationship-read-direction), which governs
reading it. Both rest on the same **direction doctrine**: an outgoing pattern
(`-[e]->`) is correct against every graph, while an incoming (`<-[e]-`) or
undirected (`-[e]-`) pattern is correct only against some graphs. Whether a
reverse leg behaves is decided by the data the traversal meets — by which
relationships the two endpoints happen to carry — and not by the query, so the
query text alone never tells the agent whether the result it is about to get is
right. A guarantee that holds only for the data seen so far is not a guarantee,
so Groadmap refuses the reverse forms in both rules rather than leaving the
outcome to the shape of the graph.

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
engine's **read** path tries to correct that orientation before it reports a
relationship, by probing the stored topology for the pair, and the probe decides
correctly whenever the two endpoints are joined in one direction only; its
**write** path does not correct the orientation at all, so the write is addressed
to a pair that has no relationship, and the storage layer answers a write to an
absent relationship with a documented no-op. Nothing is written, no error is
raised, and the transaction still commits. Where that read-side probe cannot
decide — a node pair joined in both directions — the read is wrong in a way of
its own, which is what [Relationship Read Direction](#relationship-read-direction)
refuses.

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
   a query merely traverses is not affected by **this** rule: `MATCH (b)-[e]-(x)
   SET x.reviewed = true` writes a node, which the engine resolves by identifier
   rather than by endpoint pair, and is accepted. A relationship that appears on
   the right-hand side of an assignment, as in `SET n.last_type = type(e)`, is
   outside this rule as well, but it is an expression use and is therefore
   governed by [Relationship Read Direction](#relationship-read-direction):
   that form is accepted when `e` is bound by an outgoing pattern and refused
   when `e` is bound by an incoming or undirected one.
2. The check applies to `graph update` only. A bare `DELETE e` is unaffected: the
   delete resolves the relationship itself rather than through the endpoint
   columns, and removes a relationship bound by a reverse traversal correctly.
   The other four subcommands are outside **this** rule, but none of them is
   therefore unconstrained: reading a relationship bound by a reverse pattern has
   a defect of its own and a refusal of its own, in
   [Relationship Read Direction](#relationship-read-direction), which binds all
   five subcommands. `graph delete` is among them: what that rule exempts is the
   `DELETE` clause, so a bare `DELETE e` stays accepted while a `WHERE` predicate
   over the same relationship is refused. `graph create` cannot reach the
   condition of **this** rule, because the clause-class rules above already
   reject any creating query that contains `SET` or `REMOVE`; it is nonetheless
   bound by the read rule, which does not depend on `SET` or `REMOVE` being
   present.
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

### Relationship Read Direction

A relationship variable bound by a fixed-length **incoming** (`<-[e]-`) or
**undirected** (`-[e]-`) pattern MUST NOT be used in an **expression**. All five
graph subcommands — `graph query`, `graph search`, `graph create`, `graph update`
and `graph delete` — reject a query that uses one with `utils.ErrValidation`
(exit code 6), before the graph store is opened. A rejected query returns nothing
and changes nothing.

The rule binds every subcommand because a misresolved relationship value is
harmful wherever it is read, and the subcommand carrying the expression decides
only what the harm looks like: `graph query` and `graph search` deliver the wrong
value to the caller, `graph update` persists it, `graph create` derives new graph
content from it, and a `graph delete` whose predicate reads it deletes nothing
while reporting success. What this rule exempts is the `DELETE` **clause**, not
the `graph delete` **command**; note 3 below draws that line and gives the
measurement behind it.

This rule is the read-side half of the direction doctrine stated at the head of
[Relationship Write Direction](#relationship-write-direction). Like the write
rule, it is a **separate contract from the clause-class guard rail**, not another
operation class: the query's class is already correct, and what is refused is the
**orientation** of the pattern that binds the relationship being read. The
clause-class classification is unaffected.

**Used in an expression** means any of the following, anywhere in the query:

- projected by `RETURN` or `WITH`, including by a `RETURN *` star projection,
  which projects every bound variable and therefore projects the relationship;
- passed to a function, as in `type(e)`, `startNode(e)`, `endNode(e)`,
  `properties(e)`, or `keys(e)`;
- read as a property, as in `e.key`;
- used in a `WHERE` predicate;
- used in `ORDER BY`, `SKIP`, or `LIMIT`;
- used on the right-hand side of a `SET`, as in `SET n.last_type = type(e)`.

#### The traversal contract for reading a bound relationship

| Pattern binding the relationship | Expression use of that relationship | Every `graph` subcommand |
|----------------------------------|-------------------------------------|--------------------------|
| `(a)-[e]->(b)` — outgoing | Reports the stored type, endpoints, and properties of every matched relationship | Accepted |
| `(a)<-[e]-(b)` — incoming | Reports the true relationship only while the two endpoints are joined in one direction; where they are joined in both, reports the opposite relationship's type and the pattern's own orientation | Rejected, exit 6 |
| `(a)-[e]-(b)` — undirected | Reports the forward leg correctly; where the two endpoints are joined in both directions, reports the forward relationship a second time in place of the reverse one | Rejected, exit 6 |

As with the write contract, this contract is a property of the **pattern**, not
of the anchor and not of the data: an incoming or undirected pattern is refused
whether it is anchored on the relationship's source or on its target, and it is
refused even against a graph whose node pairs are today all joined in one
direction only, where the read would in fact have been correct.

#### Why the corrupted reads are refused rather than corrected

The engine resolves a bound relationship's identity from the **endpoint pair**
the expansion emitted, exactly as the write path does, and that pair carries the
relationship the way the **pattern** walked it rather than the way storage holds
it. To recover the stored orientation, the read path probes the topology: when
the pair carries no relationship in the emitted order but carries one in the
opposite order, the engine inverts the pair and reports the relationship it finds
there. That probe decides correctly only while the two endpoints are joined in
**one** direction. Where they are joined in both, the emitted order already
carries a relationship of its own, so the engine finds one there, inverts
nothing, and resolves the reverse leg of the traversal as though it were the
forward one.

The consequences are all silent, and all of them report success:

- A projection over an undirected pattern reports the forward relationship
  twice, once per leg, and never reports the reverse relationship at all.
- `startNode(e)` and `endNode(e)` under an incoming pattern report the pattern's
  orientation, which is the exact reverse of what storage holds.
- A `WHERE` predicate over the relationship is evaluated against the wrong
  relationship, so a row that should have matched is discarded inside the engine.
  The result is short by that row, with exit code 0 and no notification.
- A `SET` whose right-hand side reads the relationship persists the wrong value
  and exits 0, so the wrong value outlives the query.

Groadmap cannot repair any of this after the fact, and correction inside Groadmap
was investigated before refusal was chosen. For `type(e)`, `startNode(e).key`,
and their siblings, what reaches Groadmap is a bare scalar with no relationship
identity attached to it, so there is nothing left to correct against. The `WHERE`
case is worse: the row is dropped inside the engine before any result reaches
Groadmap, so the missing row cannot be detected, let alone restored.

The divergence is upstream in GoGraph and cannot be corrected from this
repository. Groadmap therefore refuses the query, for the same reason the write
rule refuses its own: the alternative — a diagnostic on stderr with exit 0 —
still reports success for an answer that is wrong, which is the failure mode
being removed.

#### Rewriting a refused read

Refusal removes no reach: **every** relationship stays readable through an
outgoing pattern, because an outgoing pattern may be anchored on either endpoint.
The error message offers the three rewrites below, and each of them reports the
relationship's true stored type and orientation.

Anchor the outgoing pattern on the relationship's source to read the
relationships leaving a node:

```
MATCH (a {key:'…'})-[e]->(x) RETURN type(e)
```

Anchor it on the relationship's target to read the relationships arriving at a
node, rather than reversing the arrow:

```
MATCH (x)-[e]->(a {key:'…'}) RETURN type(e)
```

Read both directions in one query as the union of the two outgoing legs, which is
the rewrite for an undirected pattern:

```
MATCH (a {key:'…'})-[e]->(x) RETURN type(e) AS t, x.key AS k
UNION ALL
MATCH (x)-[e]->(a {key:'…'}) RETURN type(e) AS t, x.key AS k
```

Notes:

1. The rule inspects **uses of the relationship variable**. A pattern that binds
   no variable is unaffected, because no relationship value is built for it:
   `MATCH (a {key:'…'})-[:COVERS]-(b) RETURN b.key` is an ordinary read and is
   accepted.
2. A **variable-length** relationship (`-[e*1..2]-`, and equally `-[e*1..1]-`)
   and a projected **named path** (`MATCH p=(a {key:'…'})-[e]-(b) RETURN p`) are
   accepted through an incoming or undirected pattern. Neither is resolved by the
   endpoint-pair probe described above: the engine is told which way each hop was
   walked instead of inferring it, so both report the true stored type and
   orientation, including on a node pair joined in both directions. A direct
   expression use of a fixed-length relationship variable is the only shape the
   probe resolves incorrectly, and it is the only shape this rule refuses.
3. The exemption is of the `DELETE` **clause**, not of the `graph delete`
   **command**. A bare `DELETE e` names the relationship as a delete **target**
   rather than as a value: the engine resolves that relationship itself rather
   than through the endpoint columns, so
   `MATCH (a {key:'…'})-[e]-(b) DELETE e` remains accepted and removes the right
   relationship. The moment a predicate over the relationship decides **which**
   relationships the statement deletes, that predicate is an ordinary expression
   use, and `graph delete` refuses it exactly as the other subcommands do:
   `MATCH (a {key:'…'})-[e]-(b) WHERE type(e) = 'COVERS' DELETE e` is rejected
   with exit code 6. It has to be. Executed rather than refused, the engine
   evaluates the predicate against the misresolved type, discards the row inside
   the engine, and the destructive statement exits 0 reporting `{"ok": true}`
   having removed nothing at all. That is the sharpest failure in this family:
   the caller asked for a deletion, was told it succeeded, and has no reason to
   look again.
4. A relationship variable that appears only as the **target** of a `SET` or
   `REMOVE` is not an expression use. That shape belongs to
   [Relationship Write Direction](#relationship-write-direction), which continues
   to own it and to refuse it when the binding pattern is incoming or undirected.
5. A `WITH *` that only carries the binding forward is accepted; a later
   expression use of the variable it carried is refused like any other.
6. Detection runs on the **parsed** query rather than on the masked
   normalization, exactly as the write rule's detection does. The directions read
   are therefore the directions the engine will plan; a relationship arrow that
   appears only inside a string literal or a comment is not pattern syntax to the
   parser and cannot trigger a rejection; and a query the parser rejects is passed
   through to the engine unchanged, so a syntax error is reported as a syntax
   error rather than masked by a direction error.
7. A `FOREACH` body is inspected like a top-level clause, for the same reason the
   write rule gives: `FOREACH (x IN list | SET n.last_type = type(e))` reaches
   the same expression.

### Cypher Query and Property Value Content Rules

Two content rules govern what a Cypher query may **contain** and what a query may
**write** into a knowledge-graph property value. They are the graph's instances of
the two rules every other free-text value in Groadmap obeys — the Free-Text UTF-8
Encoding Constraint and the Free-Text Control-Character Constraint, both defined
in `MODELS.md § Task`, which stays canonical for what each rule forbids. This
section is canonical for how the two apply to the graph.

Both rules refuse with `utils.ErrValidation` (exit code 6), before the graph store
is opened. A refused query returns nothing and changes nothing.

The two rules **do not have the same reach**, because they object to different
things. The encoding rule objects to a query the engine would silently rewrite,
which is a fact about the statement. The control-character rule objects to a value
that would be stored, and only a write stores one.

| Rule | Decided on | `graph create` | `graph update` | `graph delete` | `graph query` | `graph search` |
|------|------------|----------------|----------------|----------------|---------------|----------------|
| Free-Text UTF-8 Encoding Constraint | The raw bytes of the query, before the parse | Binds | Binds | Binds | Binds | Binds |
| Free-Text Control-Character Constraint | The property values the query will write | Binds | Binds | Does not bind | Does not bind | Does not bind |

Like [Relationship Write Direction](#relationship-write-direction) and
[Relationship Read Direction](#relationship-read-direction), these rules are a
**separate contract from the clause-class guard rail**, not another operation
class. What they refuse is the query's **content**; the clause-class
classification is unaffected.

**Precedence.** Both rules are applied after the clause-class guard rail and after
both relationship-direction rules, and still before the graph store is opened. The
rules that precede them decide what the query **is** — its operation class, and the
orientation of the patterns it binds — while these decide what the query, or a
value it carries, **contains**, which matters only once the statement is otherwise
one that the subcommand would run.

#### Why the encoding rule binds every subcommand

The rule is decided on the raw query bytes, before the parse, because the parse
destroys the evidence: the engine decodes the query to characters before its
grammar runs, and replaces every byte that decodes to no character with `U+FFFD`
(REPLACEMENT CHARACTER). No later point can see the byte the caller supplied.

That substitution is a fact about the **statement**, not about storage, and it is
indifferent to what the statement then does. The subcommand carrying the byte
decides only what the damage looks like:

- `graph create` and `graph update` store a value that was never supplied, and
  report success.
- `graph query` and `graph search` compare against a literal that was never
  supplied, so a row that should have matched does not, and the command reports
  success having found nothing.
- `graph delete` gated by such a literal matches nothing, removes nothing, and
  still reports success.

The third is the worst of the three, and it is why the rule is keyed on the
**cause** rather than on the command. A destructive statement that reports success
having removed nothing is the failure shape the caller has no reason to check —
the same judgement already recorded for `graph delete` in
[Relationship Read Direction](#relationship-read-direction), note 3. Stated by
command, one cause would have become three rules, and two of the three would never
have been written.

Because the rule is decided on the raw bytes, a query that carries an invalid byte
**anywhere** is refused: in a label, in a match pattern, in a property key, or in a
comment, and not only in a value the query writes. That widening is intended and is
not a false positive. The engine replaces those bytes just the same, so the
statement it would execute is not the statement the caller wrote.

#### Why the control-character rule does not extend to the reads

The control-character rule objects to what is **stored**, and a read stores
nothing: a control character in a read literal is compared against what the graph
already holds.

The store can legitimately hold a value that carries a control character, from two
ordinary sources: every value written before this rule existed, and any value a
computed expression produces, which the rule cannot see (see
[What the rules do not reach](#what-the-rules-do-not-reach) below). Refusing a read
or a delete that named such a value would leave that data **unreadable** rather
than merely unwritable, which is a loss of reach the rule never intended.
`graph delete` is on the same side: it removes elements, it stores no value, and a
predicate naming a control character is how an operator reaches the entry that
carries one.

#### Where each rule is decided

The control-character rule applies to the value the engine **will write**, never to
the query text. Cypher decodes escape sequences inside a string literal — among
them `\b` (backspace), `\f` (form feed), and `\uXXXX`, a code point written as four
hexadecimal digits — so a query whose own text is pure ASCII can write a value that
carries a real control character, and a scan of the query string would admit it.
`SET n.body = 'a\u001b[31mred'` is such a query: its text carries no control
character, and the value it writes carries a real `ESC`. The escape sequences are
those of `Cypher Query Language Reference, Version 9`, the openCypher 9 reference
document, which states them under the heading "Note on string literals"; that
document numbers no sections, so it is cited by heading and not by number.

The encoding rule applies to the raw query bytes, for the reason given above. The
two rules therefore read two different objects, and each reads the only object in
which what it objects to can be seen.

#### The order of the two rules

Where both rules apply, the application applies the **encoding rule first**. This is
the order `MODELS.md § Task` (Free-Text UTF-8 Encoding Constraint) fixes for the
pair, and it is not a preference: an invalid byte decodes to `U+FFFD`, which is not
a forbidden control character, so the control-character rule would report as
acceptable a value that the encoding rule refuses.

#### What the rules do not reach

Both rules reach **literal** values only. A right-hand side that the statement
computes at execution time is outside both, because the value does not exist until
the statement runs and Groadmap never holds it. This is a **limit**, stated here
rather than left to be discovered:

- A function result, as in `SET n.last_type = type(e)` or `SET n.name = toUpper(x)`.
  In the first example the relationship must be bound by an outgoing pattern; a
  reverse binding is refused by
  [Relationship Read Direction](#relationship-read-direction), which runs before
  these rules.
- Another element's property, as in `SET n.name = other.key`.
- A parameter reference, as in `SET n.name = $value`.

A value of any of those shapes is written unchecked, exactly as it was before these
rules existed. Closing the limit means checking at the storage boundary, which is
inside the engine and not in this repository.

One computed shape **is** covered, and needs no treatment of its own: the
concatenation of string literals, as in `SET n.name = 'a' + 'b'`. Both rules are
closed under concatenation — two values free of forbidden code points concatenate to
one, and two well-formed UTF-8 strings concatenate to one — so checking each literal
operand decides the result. A list of string literals is covered element by element
for the same kind of reason: each element is stored as a value in its own right.

#### What a refusal names

A refusal identifies what is wrong without ever echoing the offending bytes.
Printing them would emit into the terminal exactly the characters the
control-character rule exists to keep out of it, and for the encoding rule the value
the caller supplied is no longer recoverable from the parsed query in any case. Each
refusal therefore names the offending byte or code point in a written form, and
reproduces none of the text around it.

- A **control-character** refusal names the **property key** the value is assigned
  to, and the first forbidden **code point**, written in the `U+001B` form. It also
  states that the query text alone does not show the character, because Cypher
  decodes escapes inside a string literal.
- An **encoding** refusal names the offending **byte** and its **offset** in the
  query, and states the consequence for the subcommand at hand: a stored value that
  differs from the supplied one, a match against a literal that was never supplied,
  or a deletion that removes nothing while reporting success.
- An encoding refusal names the **property key** as well, where the byte falls
  inside a value that the query writes. Where no property can be named, the message
  says so, in terms that are true for the subcommand at hand: a subcommand that
  writes no property value has none to name, and the message states instead that
  the byte corrupts the literal the query matches on.

Notes:

1. Both rules run before the graph store is opened, so a refusal is the guard's own
   and not the engine's. The exit code distinguishes the two: 6, and not the 1 an
   engine failure carries.
2. Neither rule uses the masked normalization that the clause-class guard rail
   classifies on (see [Literal-Aware Normalization](#literal-aware-normalization)).
   The encoding rule reads the raw query string, and the control-character rule
   reads the values of the parsed query. Masking is a device for deciding a query's
   operation class, and it answers neither of the questions these rules ask.
3. A query the parser rejects is still refused by the encoding rule, which needs no
   parse. It draws no control-character refusal: the values that rule inspects do
   not exist for an unparseable query, so the syntax error is left to the engine to
   report, exactly as the two direction rules leave it. An encoding refusal for such
   a query names no property, because none can be attributed.

### Cypher Input Source and Precedence

Each graph subcommand obtains its Cypher from one of two sources:

1. The `--query "<cypher>"` flag.
2. Standard input, read under a bound, when the `--query` flag is absent. This
   allows piping a query, for example
   `cat query.cypher | rmp graph query -r myproject`.

Whichever source carries it, a query is subject to the maximum length stated in
[Maximum Query Length](#maximum-query-length) below.

Precedence and rules:

1. When `--query` is present and non-empty, its value is used and standard input
   is not read.
2. When `--query` is absent, the query is read from standard input. The read is
   bounded and is **not** a read to EOF: see
   [Bounded Standard-Input Read](#bounded-standard-input-read) below.
3. When `--query` is absent and standard input supplies no query, the command
   fails with `utils.ErrRequired` (exit code 2) and the message
   `Error: required parameter missing: no query supplied`. Standard input supplies
   no query in each of these cases:
   - Standard input is a terminal, meaning an interactive character device.
   - Standard input is already at end of stream: it is closed, or it is connected
     to a source that carries nothing, such as `/dev/null`.
   - Everything standard input carries is whitespace, so nothing remains after
     the trim of rule 5.

   A terminal is refused **without being read at all**, so an invocation that
   forgot the flag fails at once instead of waiting for a query nobody is going
   to type. The other two cases are decided as soon as the stream ends (see
   [Standard Input That Supplies No Query](#standard-input-that-supplies-no-query)).
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
   check and before execution. The trim happens **after** the length check, which
   counts the bytes as supplied (see
   [Maximum Query Length](#maximum-query-length)).

Standard input carries the Cypher query itself here: what the `graph` subcommands
read from it is the instruction they execute, not a value they store. Other
commands accept standard input as well, and the cross-cutting input rule is
stated in `DATA_FORMATS.md § Input`, which is the canonical statement of every
command that reads standard input: it lists the `--query` of the `graph`
subcommands together with the `--body` of the comment subcommands of the `task`
and `sprint` families.

#### No Positional Query: A Stray Token Is Refused

The two sources above are the only two. A `graph` subcommand accepts **no
positional argument at all**: each of the five declares a maximum of zero, which
is what `COMMANDS.md § Positional Arity by Command` publishes for `graph create`,
`graph query`, `graph update`, `graph delete`, and `graph search`. A Cypher query
written bare on the command line is therefore not a third source. It is an excess
positional argument, and the subcommand refuses it.

The rules are:

1. **Which tokens are positional arguments at all.** This must be settled before
   a token can be called unexpected, because it decides which of two errors a
   `-`-prefixed token draws. Rule 4 of the precedence rules above is canonical
   for the classification: a
   token is flag-like when it begins with `--`, or with a single `-` immediately
   followed by an ASCII letter. Every other token is a positional argument,
   including a `-` followed by a digit or a decimal point (`-1`, `-0.5`) and a
   bare `-`. A flag-like token that no `graph` subcommand defines is refused as an
   unknown flag, under the CLI-wide wording `COMMANDS.md § Positional Arguments`
   rule 5 publishes; every other stray token is refused by rule 2 below. This is
   the one point on which the `graph` family and the comment subcommands classify
   the same token differently, and each family states its own rule: on a comment
   subcommand a stray `-1` is an unknown flag
   (`COMMANDS.md § Comment Positional Argument Contract`, rule 2).
2. **The refusal.** An invocation that supplies a positional argument is refused
   with `utils.ErrInvalidInput` (exit code 2) and this line on stderr:

   ```
   Error: invalid input: unexpected argument "X" (graph queries use --query or stdin)
   ```

   `X` is the offending token, quoted and echoed exactly as the user supplied it.
   All five subcommands emit this line, and they emit it identically: they share
   one argument-parsing rule, so the family has one wording and not five.
3. **Only the first offending token is named.** The tokens are examined left to
   right and the first positional argument ends the invocation, so
   `rmp graph query -r <roadmap> --query "<cypher>" alpha beta` names `alpha` and
   never mentions `beta`.
4. **The position of the offending token does not matter; the order of the
   tokens decides which refusal is reached.** A stray token written before the
   flags is refused exactly as one written after them. When an invocation carries
   both a stray token and a `--query` whose value is absent (rule 4 of the
   precedence rules above), the left-to-right examination settles it: whichever
   comes first is the error reported. Both carry exit code 2.
5. **Where the refusal lands in the subcommand's order.** Roadmap selection runs
   first, so an invocation that names no roadmap and has none selected fails with
   `utils.ErrNoRoadmap` (exit code 3) even when it also carries a stray token.
   Everything else runs after the refusal. The stray token is refused:
   - **before the graph store is opened**, so an invocation naming a roadmap that
     does not exist exits 2 and not the 4 that roadmap would otherwise draw;
   - **before standard input is read**, so a subcommand that was given no
     `--query` never blocks on, and never consumes, a stream a producer is still
     writing to;
   - **before the maximum-length check**, so an over-long query offered alongside
     a stray token exits 2 and not the 6 of
     [Maximum Query Length](#maximum-query-length);
   - **before the guard rail classifies anything**, so a query of the wrong
     operation class supplied alongside a stray token exits 2 and not 6, and
     before the two content rules of
     [Cypher Query and Property Value Content Rules](#cypher-query-and-property-value-content-rules).

   A refused invocation therefore does nothing: it opens no store, creates,
   changes and deletes nothing, leaves the snapshot directory and the
   write-ahead log untouched on disk, and writes zero bytes to stdout. An excess
   positional argument is not a dispatch failure, so no help follows it: stderr
   carries the error line and the AI-agent hint alone
   (`HELP.md § Error message format`).
6. **The parenthetical is part of the published line, not an incidental hint.**
   ` (graph queries use --query or stdin)` is appended to the canonical CLI-wide
   line, and a caller that matches the line matches it in full, hint included.
   The reason is the one `COMMANDS.md § Published Error Strings Are Exact` gives
   for every other error line: a reader must not have to work out which half of a
   line is normative. Its absence on the comment subcommands is not a divergence
   between two copies of one wording. The hint names the two sources of a
   **Cypher query**, which only these five subcommands have; the comment
   subcommands, whose body has two sources of its own, publish the canonical line
   without it, and a hint naming `--query` would be false on them. An edit to
   either family must therefore keep the shared part of the line shared and keep
   this hint confined to the `graph` family.

#### Maximum Query Length

A Cypher query MUST NOT exceed **1 MiB, which is 1048576 bytes**. A query longer
than that is refused with `utils.ErrValidation` (exit code 6) and the message:

```
Error: validation error: query exceeds maximum length of 1048576 bytes
```

The rules are:

1. **The maximum counts bytes, not characters.** This is a real difference from
   the comment body's cap, which counts 4096 **characters**
   (`COMMANDS.md § Comment Body Input Source and Precedence`), and the two units
   are each correct for what they measure. A comment body is stored text whose
   length is a property the user reads back, so it is counted in the units the
   user wrote it in. A query is an instruction that is executed and discarded,
   never stored, and the harm this maximum exists against is memory, which is
   counted in bytes. A 1 MiB query written in multi-byte characters therefore
   carries fewer than 1048576 characters, and that is the intended reading.
2. **The maximum applies to both sources.** A query is refused at the same length
   whether it arrived through `--query` or through standard input, so the same
   text never passes at one door and fails at the other. The count is taken over
   the bytes as supplied, before the trim of rule 5 above.
3. **The length check runs first.** It precedes the guard-rail classification,
   the literal masking that classification depends on
   ([Literal-Aware Normalization](#literal-aware-normalization)), the opening of
   the graph store, and the engine. An over-long query is never masked, never
   classified, never parsed, and never executed; nothing in the graph changes and
   stdout stays empty.
4. **Why 1 MiB and not something tighter.** One MiB is roughly a million
   characters, which is generous even for a graph bootstrap script carrying
   hundreds of `MERGE` statements, while the harm measured against the unbounded
   read this replaces needed 256 MiB of input to reach 867 MB of resident memory
   and 15.9 seconds of wall time. A maximum that someone reaches while doing ordinary work is a
   maximum that gets widened later, and widening a published limit is worse than
   choosing it well once. A 64 KiB cap was considered and declined for exactly
   that reason.

#### Bounded Standard-Input Read

When the query comes from standard input, the command does **not** read the
stream to EOF. It consumes at most one byte beyond the maximum — 1048577 bytes —
because that one byte already settles the verdict:

- While what has arrived still fits within the maximum, reading continues.
- The moment one byte more than the maximum has arrived, the verdict is fixed,
  because no later byte can bring the count back down. The command stops reading
  and fails with exit code 6 and the message given above.
- Peak memory is therefore bounded by the maximum and does not grow with the
  amount the writer sends.

This is a security property and not an implementation detail: an over-long query
is refused without ever being buffered, so a producer that writes without limit
cannot drive the command's memory. The measured behaviour of the unbounded read
this replaces was 867 MB of peak resident memory and 15.9 seconds of wall time
for 256 MiB offered to `rmp graph query`, the time going into the guard rail's
masking pass and the engine's parse attempt, both run over a 256 MB "query" that
was never going to be accepted.

A producer still writing when the command exits observes the usual broken-pipe
result. The bound is a promise about what `rmp` consumes and retains, not about
what the producer manages to write: the operating system's pipe buffer holds
bytes the command never reads, so a writer may push somewhat more than 1048577
bytes before the pipe breaks.

One difference from the comment body's bounded read is deliberate and must not be
"aligned" away. That read looks past its cap for trailing whitespace, so that the
verdict it reaches is exactly the verdict a read-to-EOF implementation would
reach after trimming. This read does not: the maximum counts the bytes standard
input supplies, so a stream of 1048576 bytes of Cypher followed by trailing
whitespace is refused even though trimming that whitespace would have brought it
to the maximum. The simpler rule is the right one here because a query's length
is not a value anybody reads back, and a producer that pads a megabyte of Cypher
with more whitespace is not a case worth reading further for.

#### Standard Input That Supplies No Query

Rule 3 above refuses an empty, whitespace-only, or terminal standard input with
exit code 2. For the terminal, the refusal comes **before any read**, and that
is part of the contract rather than a remark about how fast the check happens to
be: the command MUST NOT wait for input on a terminal, and MUST fail with exit
code 2 instead.

The failure this closes was observed rather than imagined. An invocation that
omitted `--query`, with a terminal on standard input, printed nothing and never
returned; it was terminated after roughly forty minutes. Nothing on the command
line looks wrong, no diagnostic appears, and any automated caller — a script, a
CI step, an agent — blocks indefinitely. This half of the unbounded read is the
cheaper one to trigger: it needs no hostile input and consumes no memory. An interactive terminal is not a source a `graph` subcommand
ever expects a query from, because the two documented ways to supply one are the
flag and a pipe or a redirection.

The exit code is 2 and not the 6 that an over-long query carries, and the two
MUST NOT be collapsed into one class. Supplying no query at all is a missing
required parameter, which is exit code 2 across the CLI; supplying a query the
command refuses to accept is a validation failure, which is exit code 6. The
comment body reaches the same two verdicts for the same two conditions
(`COMMANDS.md § Comment Body Input Source and Precedence`, rule 3 for the missing
body and the bounded read for the over-long one).

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
| No query supplied: `--query` absent and standard input empty, whitespace only, or a terminal; or `--query` present with an empty, whitespace-only, or absent value (see [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)) | `utils.ErrRequired` | 2 |
| Any graph subcommand — `graph create`, `graph query`, `graph update`, `graph delete` or `graph search` — receives a positional argument, a bare Cypher query included; the five accept none (see [No Positional Query: A Stray Token Is Refused](#no-positional-query-a-stray-token-is-refused)) | `utils.ErrInvalidInput` | 2 |
| Query longer than the maximum query length of 1 MiB, from either source (see [Maximum Query Length](#maximum-query-length)) | `utils.ErrValidation` | 6 |
| Query's operation class does not match the subcommand | `utils.ErrValidation` | 6 |
| `graph update` writes a relationship bound by an incoming or undirected pattern (see [Relationship Write Direction](#relationship-write-direction)) | `utils.ErrValidation` | 6 |
| Any graph subcommand — `graph query`, `graph search`, `graph create`, `graph update` or `graph delete` — uses a relationship variable bound by an incoming or undirected pattern in an expression; a bare `DELETE e` is not an expression use and stays accepted (see [Relationship Read Direction](#relationship-read-direction)) | `utils.ErrValidation` | 6 |
| `graph query` or `graph search` receives a `SHOW INDEX(ES)` / `SHOW CONSTRAINT(S)` statement whose keyword spacing the engine does not accept (see [Keyword Spacing in a Schema-Introspection Command](#keyword-spacing-in-a-schema-introspection-command)) | `utils.ErrValidation` | 6 |
| Any graph subcommand — `graph create`, `graph query`, `graph update`, `graph delete` or `graph search` — receives a query whose raw bytes are not valid UTF-8 (see [Cypher Query and Property Value Content Rules](#cypher-query-and-property-value-content-rules)) | `utils.ErrValidation` | 6 |
| `graph create` or `graph update` would write a property value carrying a forbidden control character; the other three subcommands write no property value and are not bound by this rule (see [Cypher Query and Property Value Content Rules](#cypher-query-and-property-value-content-rules)) | `utils.ErrValidation` | 6 |
| Cypher fails to parse or execute in the engine | `utils.ErrDatabase` | 1 |
| Graph store cannot be opened, recovered, read, or written (I/O, corruption, lock) | `utils.ErrDatabase` | 1 |
| Successful execution | — | 0 |

Rules:

1. The guard-rail rejection (operation class mismatch) is detected before the
   graph store is opened for writing. A rejected query never mutates the graph.
   The relationship-write-direction rejection, the relationship-read-direction
   rejection, the introspection keyword-spacing rejection, and the two content
   rejections of
   [Cypher Query and Property Value Content Rules](#cypher-query-and-property-value-content-rules)
   are detected at the same point and carry the same guarantee; none of those
   statements is ever handed to the engine. The three refusals
   that belong to the subcommand's arguments and to the query's source are
   detected earlier still, before the guard rail classifies anything: the
   stray-positional refusal (exit code 2), the missing-query refusal (exit
   code 2) and the maximum-length refusal (exit code 6), all three stated in
   [Cypher Input Source and Precedence](#cypher-input-source-and-precedence).
   The stray-positional refusal is settled while the arguments are still being
   read, so it precedes the maximum-length refusal always; against the
   missing-query refusal it does not, because a `--query` whose value is absent
   is settled in the same left-to-right pass and the earlier token wins (see
   [No Positional Query: A Stray Token Is Refused](#no-positional-query-a-stray-token-is-refused),
   rule 4).
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

Groadmap does not depend on the engine to serialise access to the store. It
serialises it itself, at the process level, through a single advisory lock file
that Groadmap maintains in the roadmap's graph directory, `write.lock` (see
[Persistence Layout](#persistence-layout)). Every invocation that opens the store
takes that lock before opening it, in one of two modes:

- A **write** invocation takes the lock **exclusively** and holds it across the
  whole open, commit, checkpoint, and write-ahead-log truncation sequence.
- A **read** invocation takes the lock in **shared** mode and holds it across the
  **store open alone**: it acquires the lock before opening the store and
  releases it as soon as the open returns. The query then runs with no lock
  held.

The two modes are mutually exclusive. While a writer holds the lock exclusively,
no reader can hold it; while one or more readers hold it in shared mode, no
writer can. Several readers may hold the shared lock at the same time, so reads
never serialise against one another. The operating system releases the lock when
the holding process exits, so an invocation that crashes does not strand it. The
lock file itself is created by whichever invocation first needs it, reader or
writer, and is never removed.

The exclusive lock covers the writer's full sequence, not just the transaction,
because that is the span that must not interleave: a second writer that had
loaded the graph before the first writer's commit would checkpoint a full
snapshot of its own stale in-memory graph and then truncate the write-ahead log
that still held the first writer's committed change, silently losing an
acknowledged write. Because the sequence Groadmap needs serialised is wider than
a transaction, no engine-level writer exclusion would have covered it in any
case.

A read takes the shared lock because **opening the store is not a read-only
operation on disk**. Opening it runs GoGraph's recovery step, and recovery
repairs an interrupted checkpoint before it loads anything: it removes a stale
staging directory `snapshot.tmp` unconditionally, and, when the live `snapshot/`
directory carries no manifest while `snapshot.bak/` does, it promotes the backup
by renaming `snapshot.bak` to `snapshot` and making that rename durable. Both
actions repair the very directory a writer's checkpoint publishes into. Without
the shared lock, a read could delete the staging directory a concurrent writer
was assembling its snapshot in, or interleave with that writer's own publish
sequence.

**The store open is also the whole of what a read needs the lock for, which is
why the lock is released the moment the open returns.** Every on-disk action a
read performs happens inside the open: the staging-directory removal and the
backup promotion are both part of the recovery step, and the snapshot components
and the write-ahead log are read there and closed there. The open returns a graph
that is fully materialised in memory, including the state replayed from the
write-ahead-log tail; the query, the traversal, and the serialisation of the
result that follow read that in-memory graph and touch no file in the store.
Holding the lock past the open would therefore protect nothing that is not
already protected, while blocking writers for as long as the query takes.

This is a deliberate, load-bearing narrowness, not an oversight. A future change
that makes any part of a read touch the store **after** the open — a lazily
loaded component, a memory-mapped snapshot, a handle kept open past recovery, or
a re-read during iteration — invalidates the reasoning above, and the hold MUST
then be widened to cover the new access. Widening it in the absence of such a
change is a regression: it reintroduces contention that buys no safety.

Durability is provided by a write-ahead log with CRC32C integrity checks plus
atomic on-disk snapshots; on opening the store, GoGraph runs recovery to restore
the last committed state from the snapshot and log.

### What a Read Changes on Disk

A read changes exactly what the recovery repair above changes, and nothing else.
Every change below happens **inside the store open**, while the shared lock is
held; after the open returns, a read touches no file in the store at all. A read
invocation, whether it is a CLI read subcommand or a web request:

1. MUST NOT run a write transaction, and MUST NOT add, alter, or remove any node,
   relationship, property, label, index, or constraint.
2. MUST NOT checkpoint and MUST NOT write a snapshot. The contents of `snapshot/`
   are left exactly as the read found them.
3. MUST NOT truncate or otherwise write to the write-ahead log. The log is opened
   for reading only and is left byte for byte as the read found it, so a read
   never shortens the history a subsequent recovery replays.
4. MAY remove a stale `snapshot.tmp` staging directory, and MAY promote
   `snapshot.bak` to `snapshot`, as the recovery repair above describes.
5. Creates the lock file `write.lock` when it does not already exist.
6. Creates the graph directory itself when, and only when, the invocation is a
   CLI read subcommand (see [Persistence Layout](#persistence-layout), rule 2).
   The web interface never creates the graph directory: a roadmap with no graph
   directory is an empty graph and is served as such (see `WEB.md § Knowledge
   Graph from the GoGraph Store`).

The **content** of the graph is therefore never changed by a read. What a read can
change is the store directory's structure, and only by completing a repair that
the next invocation to open the store would otherwise complete instead.

### Lock Contention

The two lock modes handle contention differently, because their callers differ.

1. A **write** invocation takes the exclusive lock without waiting. A writer that
   finds the lock held, by another writer or by a reader, fails immediately with
   `utils.ErrDatabase` (exit code 1) rather than waiting or corrupting the store.
   Because a reader holds the lock only across the store open, the window in
   which a read can make a concurrent write fail is the duration of that open,
   and is **not** proportional to how long the read's query runs. A long-running
   query cannot fail a concurrent write.
2. A **read** invocation MUST NOT block indefinitely and MUST NOT fail on the
   first collision. It waits for the shared lock under the bounded
   exponential-backoff policy specified in
   `IMPLEMENTATION.md § Graph Store Concurrency`. If the lock is still unavailable
   when that bounded wait is exhausted, the read fails: with `utils.ErrDatabase`
   (exit code 1) for a CLI read subcommand, and as an internal read error
   (HTTP 500) for the web graph data endpoint, which is the status that endpoint
   already returns for a graph store that cannot be opened (see
   `WEB.md § Routes and Pages`).

A read waits where a write fails at once because the two are not symmetrical, and
the asymmetry survives the narrow reader hold. What a read waits for is a
**writer's** hold, and that hold is unchanged: it still spans the whole open,
commit, checkpoint, and write-ahead-log truncation sequence, including a full
snapshot rewrite whose cost grows with the live graph size. The reader's wait is
therefore sized against the writer's critical section, not against its own, which
is why narrowing the reader's hold does not narrow the wait it may face. Reads
are also by far the more frequent operation, so failing a read on the first
collision would make ordinary reads intermittently unavailable.

The wait is bounded, and never unbounded, because one of the two readers is an
HTTP request handler: an unbounded block would let a long write hang a `GET`
until the server's write timeout fired (see `WEB.md § HTTP Server Timeouts`). A
bounded wait keeps the worst case well inside that timeout, and it does not
consume the endpoint's query time budget, because the wait ends before the query
starts (see `WEB.md § Graph Query Time Budget`).

Groadmap's usage model and expectations:

1. Each `rmp graph` invocation is a short-lived process that opens the store,
   runs one query, commits any write, checkpoints after a successful write (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)), and
   closes the store. The process does not hold the store open across invocations.
2. Because a write invocation takes the lock exclusively before opening the store,
   two concurrent `rmp graph` write invocations against the **same** roadmap
   contend for it. The implementation MUST surface a contention or lock failure as
   `utils.ErrDatabase` (exit code 1) rather than corrupting the store or hanging
   indefinitely. The checkpoint that follows a successful write runs inside the
   invocation that already holds the lock: it adds no separate lock and two
   concurrent writers still serialise. The retry and timeout behaviour for graph
   writes is specified in `IMPLEMENTATION.md § Graph Store Concurrency`.
3. Because a read invocation takes the same lock in shared mode, a read and a
   write against the **same** roadmap also contend, in both directions: a read
   waits for an in-flight write to finish, and a write that finds a read inside
   its store open fails fast. This is deliberate. A read that opened the store
   while a writer was publishing its checkpoint could delete or race the writer's
   staging directory, because opening the store performs the recovery repair
   described above. The contention is confined to the open on the reader's side:
   once a read has loaded the graph it holds no lock, so it neither waits nor
   blocks a writer for the time its query takes. Reads against **different**
   roadmaps never contend, since each roadmap has its own graph directory and its
   own lock file. The contention rules for each mode are in
   [Lock Contention](#lock-contention).
4. Recovery on open restores the last committed state from the snapshot and the
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
5. The graph store is independent of the SQLite layer and the SQLite WAL
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
    This whitespace tolerance belongs to the DDL class alone and MUST NOT be
    carried over to the schema-introspection class, which is matched exactly
    (see criterion 39 and
    [Keyword Spacing in a Schema-Introspection Command](#keyword-spacing-in-a-schema-introspection-command)).
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
    write targets a node, and the relationship variable is bound but never read),
    and `MATCH (v:Test {key:'…'})-[e]-(x) DELETE e` is accepted by `graph delete`
    and removes the relationship. Reading the relationship through that same
    undirected pattern is refused, but by the separate
    [Relationship Read Direction](#relationship-read-direction) rule rather than
    by this one: `MATCH (v:Test {key:'…'})-[e]-(x) RETURN type(e)` fails with exit
    code 6 under `graph query`, and the type is read instead through an outgoing
    pattern anchored on the node the relationship arrives at,
    `MATCH (x)-[e]->(v:Test {key:'…'}) RETURN type(e)` (see criteria 42 and 43).
31. A read leaves the graph's data untouched on disk. After
    `rmp graph query -r <roadmap> --query "MATCH (n) RETURN count(n)"` runs
    against a store whose write-ahead log is **not** empty, the `wal` file is
    byte for byte identical to what it was before the read, and every file under
    `snapshot/` is unchanged, proving the read neither checkpointed nor truncated
    the log (see [What a Read Changes on Disk](#what-a-read-changes-on-disk)).
32. A read completes an interrupted checkpoint, and this is expected behaviour
    rather than a defect. With a stale `snapshot.tmp` staging directory present, a
    read removes it. With `snapshot/` absent while `snapshot.bak/` carries a
    manifest, a read promotes the backup to `snapshot/`. In both cases the read
    still returns the correct result and exits 0.
33. A write and a read against the same roadmap exclude each other while the read
    is opening the store. While a write holds the exclusive lock, a concurrent
    read does **not** fail on the first collision: it waits and then succeeds once
    the writer releases the lock.
34. A read holds the shared lock across the store open only, and this is
    observable: a read whose query runs for a long time does **not** fail a
    concurrent `rmp graph create` issued after that read has opened the store. The
    write succeeds and exits 0 while the read is still executing its query. This
    criterion is what prevents the hold from being silently widened back to the
    whole read.
35. Nothing in the store is read after the open. With the graph directory removed
    from disk immediately after a read has opened the store, the read still
    returns the complete and correct result, including any state that came from
    the write-ahead-log tail rather than the snapshot. This is the property the
    narrow lock hold depends on (see
    [Concurrency and Recovery](#concurrency-and-recovery)); if it ever stops
    holding, the hold MUST be widened.
36. A read that cannot take the shared lock within the bounded wait fails rather
    than hanging: a CLI read subcommand exits 1 with a plain-text diagnostic on
    stderr, and the web graph data endpoint answers HTTP 500. Neither blocks
    indefinitely (see [Lock Contention](#lock-contention)).
37. Two concurrent read invocations against the same roadmap both succeed and
    neither waits on the other, because the lock they take is shared.
38. The specification and the implementation name the same engine constructor for
    every path. A regression test enumerates every Cypher engine the
    implementation constructs to serve a `graph` subcommand or a web graph
    request, and fails if any of them is constructed through a constructor other
    than the one [Engine Constructor by Path](#engine-constructor-by-path) gives
    for that path, or if an engine is constructed on a path that table does not
    list. This is what stops the table and the code from drifting apart again.
39. A schema-introspection command is accepted with exactly one space between its
    two keywords and rejected by the guard rail with any other spacing.
    `rmp graph query -r <roadmap> --query "SHOW INDEXES"` exits 0 and returns the
    schema listing (criterion 23), while the same command with two spaces, with a
    tab, or with a line break in place of that single space fails with exit code 6
    and a plain-text message that names the keyword spacing as the cause. The
    rejected statement is never executed: the message is the guard rail's own, not
    the engine's `unexpected "SHOW"` parse diagnostic, and the exit code is not the
    1 that an engine parse failure carries. This holds for all four target keywords
    (`INDEXES`, `INDEX`, `CONSTRAINTS`, `CONSTRAINT`), in any keyword case, under
    both `graph query` and `graph search`, and on the web graph data endpoint,
    which rejects such a query before executing it, answers HTTP `400 Bad Request`
    with `kind` `invalid_keyword_spacing`, and does not report it as a query that
    is not read-only (`WEB.md` Acceptance Criterion 151). Widening the introspection classification to
    tolerate other spacing again MUST fail this criterion (see
    [Keyword Spacing in a Schema-Introspection Command](#keyword-spacing-in-a-schema-introspection-command)).
40. A query longer than the maximum is refused, and the read that refuses it is
    bounded. A producer that offers `rmp graph query -r <roadmap>` far more than
    1 MiB on standard input, with `--query` absent, sees the command exit 6 with
    `Error: validation error: query exceeds maximum length of 1048576 bytes` on
    stderr while it is still writing: the pipe breaks after the producer has
    managed to send only a small fraction of what it offered, which is what
    bounds the command's peak memory. Stdout is empty and the graph is unchanged.
    The refusal is the length check's own, not the engine's: the exit code is 6
    and not the 1 an engine parse failure carries, and the message is the one
    above rather than an engine diagnostic. A legitimate query of several hundred
    kilobytes, supplied the same way, still executes normally and exits 0, so the
    bound refuses only what the maximum forbids. Lowering the maximum below what
    ordinary work needs, or restoring a read that drains whatever it is offered,
    MUST fail this criterion (see
    [Maximum Query Length](#maximum-query-length) and
    [Bounded Standard-Input Read](#bounded-standard-input-read)).
41. An invocation that supplies no query fails at once instead of blocking.
    `rmp graph query -r <roadmap>` with `--query` absent fails with exit code 2
    and `Error: required parameter missing: no query supplied` on stderr in each
    of the three cases the rule names: standard input at end of stream, standard
    input carrying only whitespace, and standard input connected to a terminal.
    The terminal case is the one that regressed into a hang, and it is asserted
    on wall-clock time: the process exits without waiting for input, rather than
    sitting there until something kills it. Criterion 8 fixes the exit code for
    the first case; this criterion fixes the message, all three cases, and the
    requirement that none of them waits (see
    [Standard Input That Supplies No Query](#standard-input-that-supplies-no-query)).
42. Reading a relationship through an **outgoing** pattern is correct whatever the
    data, which is what makes the refusals below cost no reach. For a node pair
    joined in **both** directions — `(s:Spec)-[:VERIFIED_BY]->(v:Test)` and
    `(v:Test)-[:COVERS]->(s:Spec)` —
    `MATCH (s:Spec {key:'…'})-[e]->(x) RETURN type(e)` reports `VERIFIED_BY` and
    nothing else, `MATCH (x)-[e]->(s:Spec {key:'…'}) RETURN type(e)` reports
    `COVERS` and nothing else, and the union of the two legs,
    `MATCH (s:Spec {key:'…'})-[e]->(x) RETURN type(e) AS t, x.key AS k UNION ALL MATCH (x)-[e]->(s:Spec {key:'…'}) RETURN type(e) AS t, x.key AS k`,
    reports both, each with its own endpoint. Each of the three exits 0 (see
    [Relationship Read Direction](#relationship-read-direction)).
43. The same reads written through a reverse pattern are rejected with exit code 6
    and return nothing: `MATCH (s:Spec {key:'…'})-[e]-(x) RETURN type(e)` and
    `MATCH (s:Spec {key:'…'})<-[e]-(x) RETURN startNode(e).key, endNode(e).key`
    each fail under `graph query`, stdout is empty, and the error message names
    the relationship variable, the pattern direction that bound it, and the
    outgoing rewrite. The refusal is the guard's own, not the engine's: the exit
    code is 6 and not the 1 an engine failure carries, and the graph store is
    never opened. An undirected pattern is rejected even against a graph whose
    node pairs are all joined in one direction only, where the read would in fact
    have been correct.
44. The rule reaches **every** expression use of the bound variable, not only
    `type(e)`. Under `graph query` and under `graph search` alike, each of
    `RETURN e`, `RETURN *`, `RETURN properties(e)`, `RETURN e.key`,
    `WHERE type(e) = 'COVERS'`, and `ORDER BY type(e)` is rejected with exit code
    6 when `e` is bound by an incoming or undirected pattern, and accepted when
    the same query binds `e` by an outgoing pattern. The `WHERE` case is the one
    that loses a row rather than corrupting a visible value, so it MUST be refused
    rather than executed: against the two-way pair of criterion 42,
    `MATCH (s:Spec {key:'…'})-[e]-(x) WHERE type(e) = 'COVERS' RETURN e` matches
    no row at all, although the `COVERS` relationship exists and an outgoing read
    reports it.
45. `graph update` refuses the same use on the right-hand side of a `SET`, and
    nothing is written: `MATCH (s:Spec {key:'…'})<-[e]-(v) SET v.last_type = type(e)`
    fails with exit code 6, and a subsequent read reports `v.last_type` absent.
    Executed instead of refused, that query exits 0 and persists the **forward**
    relationship's type on the node, so the refusal is what keeps a wrong value
    off disk.
46. The read rejection does not spread further.
    `MATCH (s:Spec {key:'…'})-[:COVERS]-(x) RETURN x.key` is accepted, because the
    pattern binds no relationship variable and no relationship value is built;
    `MATCH (s:Spec {key:'…'})-[e]-(x) DELETE e` is accepted by `graph delete` and
    removes the relationship; `MATCH (s:Spec {key:'…'})-[e]-(x) SET x.reviewed = true`
    remains accepted by `graph update`, because the relationship variable is bound
    but never read; `MATCH p=(s:Spec {key:'…'})-[e]-(x) RETURN p` and
    `MATCH (s:Spec {key:'…'})-[e*1..1]-(x) RETURN e` are accepted and each reports
    the two legs with their own types and stored orientations; and
    `MATCH (s:Spec {key:'…'})-[e]-(x) WITH * RETURN x.key` is accepted, because
    carrying the binding forward is not a use of it.
47. A `graph delete` whose predicate reads the relationship is refused, and the
    refusal is what leaves the relationships intact. Against the two-way pair of
    criterion 42,
    `MATCH (s:Spec {key:'…'})-[e]-(x) WHERE type(e) = 'COVERS' DELETE e` fails
    with exit code 6 under `graph delete`, and the same statement written with an
    incoming pattern fails likewise. The exit code alone does **not** establish
    this criterion and MUST NOT be the only assertion: a read-back through
    outgoing patterns MUST report **both** relationships of the pair still
    present, because an implementation that accepted the statement would leave
    the same two in place, and an exit-code-only check could not tell the two
    apart. Executed rather than refused, that statement exits 0 reporting
    `{"ok": true}` and removes nothing at all: the engine resolves the reverse
    leg from the forward pair, evaluates the predicate against the wrong type,
    and discards the row. This criterion fixes the exemption of note 3 as an
    exemption of the `DELETE` **clause**; criterion 46 fixes its other half, that
    a bare `DELETE e` through the same pattern stays accepted and removes the
    right relationship.
48. `graph create` is bound by the rule as well, so the rule's coverage does not
    depend on which subcommand carries the expression.
    `MATCH (s:Spec {key:'…'})-[e]-(x) CREATE (n:Probe {t: type(e)})` is rejected
    with exit code 6 under `graph create`, the same statement written with an
    incoming pattern is rejected likewise, and the `MERGE` spelling of either is
    rejected as well. Nothing is created in any of these cases: a read-back
    reports no `Probe` node. The refusal is the guard's own and not the engine's,
    which the exit code distinguishes — 6, not the 1 an engine failure carries —
    and the graph store is never opened.

49. A query whose raw bytes are not valid UTF-8 is refused by every graph
    subcommand, and the write subcommands store nothing.
    `rmp graph create -r <roadmap> --query "CREATE (m:Memory {key:'sprint-38-sco<0x80>pe'})"`
    fails with exit code 6, stdout is empty, and a read-back reports no such node.
    `rmp graph update` fails likewise on
    `MATCH (m:Memory {key:'…'}) SET m.body = 'commit cf27c57<0x80>'`, and a
    read-back reports `m.body` unchanged. Executed rather than refused, each of
    those exits 0 reporting success while the store holds `U+FFFD` in place of the
    byte supplied, so the refusal is what keeps the stored value equal to the
    supplied one (see
    [Cypher Query and Property Value Content Rules](#cypher-query-and-property-value-content-rules)).
50. `graph create` and `graph update` refuse a written property value that carries
    a forbidden control character, even when the query text is pure ASCII.
    `rmp graph update -r <roadmap> --query "MATCH (m:Memory {key:'…'}) SET m.body = 'red\u001b[31m'"`
    fails with exit code 6, and a read-back reports `m.body` unchanged. The
    criterion MUST assert, before running the query, that the query text itself
    carries no control character: that is what establishes that a check on the
    query string could not have caught this case, because the character reaches
    the value only through the escape sequence Cypher decodes. The refusal names
    the property `body` and the code point `U+001B`.
51. The encoding rule binds the read subcommands, and refusing costs no reach.
    Against a stored node whose `key` is `sprint-38-scope`,
    `rmp graph query -r <roadmap> --query "MATCH (m:Memory {key:'sprint-38-sco<0x80>pe'}) RETURN m.body"`
    fails with exit code 6 and prints nothing on stdout, and `rmp graph search`
    fails likewise. A query carrying the same byte in a label, in a property key,
    or in a Cypher comment is refused as well, which is the intended widening. The
    same queries with the byte removed match the node and exit 0. Executed rather
    than refused, each of them exits 0 having found nothing, because the engine
    matched on a literal that was never supplied.
52. `graph delete` is bound by the encoding rule, and the exit code alone does
    **not** establish this criterion and MUST NOT be the only assertion. With a
    node whose `key` is `delete-target` and whose `body` holds a known value,
    `rmp graph delete -r <roadmap> --query "MATCH (m:Memory {key:'delete-tar<0x80>get'}) DELETE m"`
    fails with exit code 6, and a read-back MUST report that node still present
    with its `body` unchanged; the same holds for the `WHERE`-predicate spelling
    and for the `DETACH DELETE` spelling. Executed rather than refused, that
    statement exits 0 reporting `{"ok": true}` having removed nothing at all,
    which is the failure the caller has no reason to check. The criterion MUST
    also delete the same node through a well-formed query and read back its
    absence, without which it would pass equally well if `graph delete` had
    stopped deleting altogether. The refusal names the consequence for a delete:
    that the statement would have reported success having deleted nothing.
53. The encoding rule is applied first. A value that is at once not valid UTF-8
    and carrying a forbidden control character is refused as an **encoding**
    failure — the message names the byte and its offset and carries the wording
    `the value is not valid UTF-8` — and never as a control-character failure.
    This ordering is load-bearing rather than cosmetic: an invalid byte decodes to
    `U+FFFD`, which is not a forbidden control character, so the
    control-character rule alone would report the value acceptable.
54. The control-character rule does **not** extend to the subcommands that write
    no property value. `rmp graph query`, `rmp graph search`, and
    `rmp graph delete -r <roadmap> --query "MATCH (m:Memory {key:'legacy\u001b entry'}) DELETE m"`
    each name a forbidden control character in a match literal, and each is
    **accepted** and exits 0. In the same sequence, `graph update` still refuses
    that character on the right-hand side of a `SET`, so the asymmetry is a
    boundary of the rule and not an absence of it. Refusing the reads and the
    delete would leave a value the store legitimately holds unreadable, and beyond
    the reach of a delete, which is the loss of reach the rule never intended.
55. The stated limit is measured rather than assumed. A property value that the
    statement computes at execution time is written without inspection:
    `rmp graph update -r <roadmap> --query "MATCH (m:Memory {key:'…'}) SET m.body = toUpper(m.key)"`
    is accepted and exits 0, because the value does not exist until the engine
    runs the statement and Groadmap never holds it. Concatenated string literals
    are covered rather than exempt: `SET m.body = 'red' + '\u001b[31m'` is refused
    with exit code 6, and so is a list of string literals one of whose elements
    carries the character.
56. No refusal echoes the offending bytes, and each names what it can. A
    control-character refusal names the property key and the code point in the
    `U+001B` form, and stderr carries neither the character itself nor the value.
    An encoding refusal names the byte and its offset; it names the property key
    where the byte falls inside a value the query writes, and where no property
    can be named — which is always the case for `graph query`, `graph search`, and
    `graph delete`, and also for a query the parser rejects — the message says so
    in terms true for that subcommand instead of withholding the naming in
    silence. Every one of these refusals is the guard's own and not the engine's,
    which the exit code distinguishes: 6, and not the 1 an engine failure carries.
57. All five subcommands refuse a positional argument, with one wording. For each
    of `create`, `query`, `update`, `delete`, and `search`,
    `rmp graph <subcommand> -r <roadmap> --query "<a query of that subcommand's class>" stray`
    exits 2, writes zero bytes to stdout, and writes to stderr the line
    `Error: invalid input: unexpected argument "stray" (graph queries use --query or stdin)`.
    The criterion MUST compare the whole line, the parenthetical included, and
    MUST compare the five lines against each other: a wording that drifts on one
    subcommand is the failure this criterion exists to catch.
58. The classification of a `-`-prefixed token is asserted in both directions. On
    each of the five subcommands, a stray `-1` and a stray bare `-` each exit 2
    and are reported as an **unexpected argument**, while a stray `--foo` exits 2
    and is reported as an **unknown flag**. Of several stray tokens only the first
    is named: an invocation carrying `alpha beta` names `alpha`, and its stderr
    does not contain `beta`.
59. The refusal precedes every other check the subcommand performs, and roadmap
    selection precedes the refusal. Measured against the built binary:
    `rmp graph query stray -r <a roadmap that does not exist> --query "<cypher>"`
    exits 2 and not 4; the same invocation with no `-r` and no roadmap selected
    exits 3; a stray token supplied with a query of the wrong operation class
    exits 2 and not 6; and `rmp graph query -r <roadmap> stray` with a producer
    still writing to standard input exits 2 at once, reads nothing, and leaves the
    producer to observe a broken pipe. In every case stdout is empty, stderr
    carries the error line and the AI-agent hint and no help body, and the
    roadmap's `graph/` directory — its snapshot directory and its write-ahead
    log — is byte-identical before and after.
60. The rule is one rule across the two families that publish it. The line the
    `graph` subcommands emit is the line
    `COMMANDS.md § Positional Arguments` publishes for the whole CLI with this
    family's hint appended, and the line the comment subcommands emit is that same
    line without a hint (`COMMANDS.md § Comment Positional Argument Contract`). A
    test that asserts one family's wording MUST cite the other's, so that a change
    to either is made deliberately rather than by copying.

61. The key-uniqueness convention is stated and its violation is detectable. On a
    graph seeded with two nodes whose `key` values are equal under NFC and
    different in bytes — a precomposed `U+00C9` against the decomposed
    `U+0045 U+0301`, for example — each node's stored `key` is byte-for-byte the
    value supplied, `MATCH` with either spelling binds exactly that one node and
    never both, and the byte-wise duplicate audit reports the two as separate
    single-count rows. The two-step audit of
    [Auditing the convention](#auditing-the-convention) reports the pair: step 1
    runs under `rmp graph query` and exits 0, and step 2 groups its rows by NFC
    form and names the group holding both nodes. The same audit reports nothing on
    a graph whose keys are all distinct under NFC.

## See Also

- CLI command contract for `graph` → `COMMANDS.md § Graph Management`
- Graph query result JSON and property-type mapping → `DATA_FORMATS.md § Graph Query Result`
- Standard input as a Cypher source → `DATA_FORMATS.md § Input`
- The sibling standard-input rule for the comment body, whose cap counts characters rather than bytes → `COMMANDS.md § Comment Body Input Source and Precedence`
- GoGraph integration, directory layout, error handling → `ARCHITECTURE.md`
- The required Go version, and the minor-version floor the GoGraph dependency contributes to it → `BUILD.md § Go Toolchain`
- Writer serialisation, reader locking, recovery, lock contention, and the synchronous checkpoint trade-off → `IMPLEMENTATION.md § Graph Store Concurrency`
- Graph reads through the web interface, and the HTTP consequences of the read lock → `WEB.md § Knowledge Graph from the GoGraph Store`
- Help skeleton and AI-help entry for `graph` → `HELP.md`
