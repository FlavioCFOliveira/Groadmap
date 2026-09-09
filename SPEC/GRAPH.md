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
- [What Groadmap Does Not Check](#what-groadmap-does-not-check)
- [Literal-Aware Normalization](#literal-aware-normalization)
- [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)
  - [No Positional Query: A Stray Token Is Refused](#no-positional-query-a-stray-token-is-refused)
  - [Maximum Query Length](#maximum-query-length)
  - [Bounded Standard-Input Read](#bounded-standard-input-read)
  - [Standard Input That Supplies No Query](#standard-input-that-supplies-no-query)
- [Schema Management](#schema-management)
  - [Accepted Schema Statements](#accepted-schema-statements)
  - [Schema Object Names](#schema-object-names)
  - [Altering and Recreating an Index](#altering-and-recreating-an-index)
  - [Schema Failure Classes](#schema-failure-classes)
  - [Recovered Schema on Every Surface](#recovered-schema-on-every-surface)
- [Query Notifications as Diagnostics](#query-notifications-as-diagnostics)
- [Query Plans: The EXPLAIN and PROFILE Prefixes](#query-plans-the-explain-and-profile-prefixes)
- [Write Counters: What a Statement Changed](#write-counters-what-a-statement-changed)
- [Field Length Limits](#field-length-limits)
- [Error Handling and Exit Codes](#error-handling-and-exit-codes)
- [The Dedicated Graph Server](#the-dedicated-graph-server)
  - [Socket Path and Permissions](#socket-path-and-permissions)
  - [Socket Path Length](#socket-path-length)
  - [Server Startup](#server-startup)
  - [Server Shutdown and the Drain](#server-shutdown-and-the-drain)
  - [Server Options](#server-options)
  - [Server Diagnostics on Stderr](#server-diagnostics-on-stderr)
  - [Concurrency Inside the Server](#concurrency-inside-the-server)
  - [Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process)
  - [Server Resolution](#server-resolution)
  - [The Bolt Client](#the-bolt-client)
  - [Serving on a Non-Default Socket](#serving-on-a-non-default-socket)
- [Concurrency and Recovery](#concurrency-and-recovery)
  - [What a Statement That Writes Nothing Changes on Disk](#what-a-statement-that-writes-nothing-changes-on-disk)
  - [Statement Time Budget](#statement-time-budget)
  - [Peak Resident Memory](#peak-resident-memory)
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

The graph is accessed through the `rmp graph` command and its two subcommands.
`serve` opens one roadmap's graph once and serves it over a Unix domain socket for
as long as the process runs, and `client` sends a statement to a running server and
prints what comes back; both are specified in
[The Dedicated Graph Server](#the-dedicated-graph-server). The graph is backed by
the external GoGraph module, which provides a labelled property graph, a Cypher
engine, durable on-disk persistence, and the Bolt version 5 server `serve` runs.

**The graph is reached through a running server and through nothing else.**
`rmp graph serve` is the only process that ever opens a roadmap's graph store, and
every statement runs inside it. `rmp graph client` sends a statement to that server,
and the web graph data endpoint sends its statement through the same client, over
the same socket, speaking the same protocol (see
`WEB.md § Knowledge Graph from the GoGraph Store`). Neither of those two surfaces
opens the store, neither takes its advisory lock, and neither has a second way in.
With no server listening for the selected roadmap, each reports that the graph is
unavailable rather than reaching the store itself. The rule is stated once, in
[Server Resolution](#server-resolution).

**`rmp graph client` runs whatever Cypher it is given.** Groadmap does not classify
a statement, does not route it by the clauses it contains, and does not refuse it on
the ground of what it would read, write, or delete. A read, a write, a deletion, and
a schema change all reach the engine through the same statement surface and the one
execution path. What Groadmap still refuses is stated in
[Error Handling and Exit Codes](#error-handling-and-exit-codes), and what it
deliberately does not examine is stated in
[What Groadmap Does Not Check](#what-groadmap-does-not-check).

**The web graph data endpoint is the second surface onto the same graph, and it
behaves the same way.** It sends the statement it is given to the same server,
through the same client, so everything this file says about a statement holds of one
submitted through the web query bar (see `WEB.md § Graph Data Endpoint`). The one
thing that surface does with a statement which the CLI does not is decide whether to
append a node `LIMIT` to it, which is a decision about the response's size and
refuses nothing (see
[Literal-Aware Normalization](#literal-aware-normalization)).

## Functional Requirements

1. `rmp graph` provides two subcommands: `serve`, which opens the roadmap's graph
   and serves it over a Unix domain socket for as long as the process runs; and
   `client`, which sends a Cypher statement to a running server. `client` accepts
   any Cypher statement the engine accepts and runs it; there is no per-statement
   operation-class check, and there is no third subcommand.
2. `graph client` and `graph serve` each require a target roadmap, selected with
   the shared `-r` / `--roadmap` flag (see
   `COMMANDS.md § Roadmap Selection (Always Required)`).
3. `client` reads its Cypher from the `-q` / `--query` flag, or from standard
   input when the flag is absent, and never from a positional argument: a query
   written bare on the command line is refused (see
   [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)).
4. The output mirrors what the executed statement returns. A statement that
   produces result columns returns those columns and rows as JSON to stdout, in
   the shape defined in `DATA_FORMATS.md § Graph Query Result`; a statement that
   produces none returns `{"ok": true}` (see
   `DATA_FORMATS.md § Graph Write Result`). A statement that changed the graph
   adds one member to whichever of those two shapes it produced, `counters`,
   naming what it changed; a statement that changed nothing adds none and is
   unchanged in every byte (see
   [Write Counters: What a Statement Changed](#write-counters-what-a-statement-changed)
   and `DATA_FORMATS.md § Graph Query Counters`). A statement
   written with an `EXPLAIN` or `PROFILE` prefix is the single exception to that
   discriminator: it always returns the columns-and-rows shape, carrying the
   captured plan, and never `{"ok": true}` (see
   [Query Plans: The EXPLAIN and PROFILE Prefixes](#query-plans-the-explain-and-profile-prefixes)).
5. Every statement runs inside a single transaction on the transactional
   execution path, and a statement that changes the graph is durable before its
   result is acknowledged (see
   [Engine Constructor by Path](#engine-constructor-by-path) and
   [Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process)).
6. The server MUST fold the write-ahead log into a self-sufficient on-disk
   snapshot and truncate the log, on a cadence while it runs and again at
   shutdown when the log has grown since it was last folded. This checkpoint
   bounds write-ahead-log growth and keeps recovery cost proportional to the live
   graph size rather than to the total history of writes. The condition under
   which a fold is owed is stated in
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write) and the
   server's application of it in
   [Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process).
   A transaction that appended nothing to the write-ahead log owes no fold; what
   such a statement does change on disk is specified in
   [What a Statement That Writes Nothing Changes on Disk](#what-a-statement-that-writes-nothing-changes-on-disk).
7. A checkpoint that fails after the transaction has already committed durably
   MUST NOT fail the user-visible write. The write succeeded, the write-ahead log
   is durable, and the next successful checkpoint reconciles the snapshot;
   recovery still works from the intact write-ahead log. The statement returns its
   normal success result, the caller's exit code is unchanged, and the checkpoint
   failure is surfaced through the server's own diagnostics (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write) and
   [Server Diagnostics on Stderr](#server-diagnostics-on-stderr)).
8. The graph for a roadmap is stored under that roadmap's home directory and is
   created by `rmp graph serve` on first use (see
   [Persistence Layout](#persistence-layout) and
   [Server Startup](#server-startup), step 1).
9. Errors are written as plain text to stderr and map to the existing exit-code
   conventions (see [Error Handling and Exit Codes](#error-handling-and-exit-codes)).
10. `rmp graph client` surfaces, on stderr, exactly the advisory notifications
    the engine returns for the statement it ran, as one plain-text diagnostic line
    per notification. Groadmap does not generate notifications; the engine alone
    decides which statements carry them, and Groadmap emits whatever it is given
    (which may be none). Notifications never change the stdout success output or
    the exit code (see
    [Query Notifications as Diagnostics](#query-notifications-as-diagnostics)).
11. `rmp graph client` is also the surface through which the knowledge graph's
    schema is managed. It accepts the schema-mutating DDL statements
    `CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT` and `DROP CONSTRAINT`, and
    the schema-introspection commands `SHOW INDEX(ES)` and `SHOW CONSTRAINT(S)`,
    because it accepts every statement the engine accepts. What each statement
    does, how a schema object is named, why changing an index is two invocations
    rather than one, and how a schema failure surfaces are specified in
    [Schema Management](#schema-management).
12. A statement runs under a time budget, enforced by the server that executes it
    and carrying one value for every surface. A statement that exhausts it is
    cancelled, its transaction rolls back whole, no checkpoint runs, and the
    caller fails with `utils.ErrGraphEngine` (exit code 1); no new exit code is
    introduced. What the budget does to a statement is specified in
    [Statement Time Budget](#statement-time-budget), the bound the server applies
    in [Server Options](#server-options), and
    `WEB.md § Graph Query Time Budget` is canonical for the value.
13. `rmp graph serve` opens one roadmap's graph store, holds it for the life of
    the process, and serves it over a Unix domain socket using the engine's own
    Bolt version 5 server. The socket and its permissions, the startup and
    shutdown sequences, the options the server is given, and what the server
    guarantees are specified in
    [The Dedicated Graph Server](#the-dedicated-graph-server).
14. `rmp graph client` sends one statement to a running server over that socket
    and prints the result. It has no second path: a roadmap with no server
    listening is a failure for this subcommand and never a fall back onto the
    store (see [The Bolt Client](#the-bolt-client)).
15. `rmp graph client` and the web graph data endpoint resolve the socket in force
    before they send anything — the path derived from the roadmap, or, on
    `graph client`, the value of its `--socket` flag. Against a served roadmap the
    statement goes to the server; with no server listening neither surface reaches
    the store, and each reports that the graph is unavailable. Neither ever takes
    the store's advisory lock. The rule, its four states, and the outcome each
    surface reports for each state are specified once in
    [Server Resolution](#server-resolution).
16. A statement produces the same result whichever of the two surfaces carried
    it: each publishes the same values for the same elements, through the same
    mapping, and which surface carried the statement is not observable in what the
    engine answered. The documents that carry those values differ by design —
    `rmp graph client` publishes the columns-and-rows object and the web graph
    data endpoint publishes a graph view of nodes and edges — so what is common
    between the two surfaces is the values and never the response body (see
    [Acceptance Criteria](#acceptance-criteria), criterion 78). The one value that
    may legitimately differ between two runs of the same statement is a query
    plan's measured duration, which describes the execution rather than the result
    and which two executions measure independently;
    `DATA_FORMATS.md § Graph Client Result`, rule 5, is canonical for that
    boundary.
17. `rmp graph client` publishes the query plan the engine
    captured for a statement written with an `EXPLAIN` or a `PROFILE` prefix,
    rather than discarding it. The plan appears under `plan` for an `EXPLAIN` and
    under `profile` for a `PROFILE`, never under both, and a statement carrying
    neither prefix produces the output it produced before, byte for byte. What
    each prefix does, which statements each admits, and the rules under which the
    plan's figures are published are specified in
    [Query Plans: The EXPLAIN and PROFILE Prefixes](#query-plans-the-explain-and-profile-prefixes);
    the JSON is fixed in `DATA_FORMATS.md § Graph Plan Node`.

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

GoGraph requires Go 1.26: its `go.mod` declares `go 1.26`. Adopting the graph
feature therefore sets Groadmap's minor-version floor at Go 1.26. Groadmap's own
required Go version is higher than GoGraph's minimum and is set independently of
GoGraph. `BUILD.md § Go Toolchain` is the authoritative statement of the required
Go version and of the build implications.

### Dependency Maturity Risk

GoGraph is consumed at the exact tag **v0.14.1**. Because
v0.14.1 is a v0 (pre-1.0) version, it is consumable directly at the bare module path
`github.com/FlavioCFOliveira/GoGraph`, and `go.mod` pins the clean exact tag `v0.14.1`.
This exact-tag pin satisfies the pinning mitigation below directly. The pinned version
is recorded in `BUILD.md § External Dependencies`, which is the table that carries it;
`BUILD.md § Go Toolchain` records the Go minor-version floor GoGraph imposes, which is
a different fact about the same dependency.

As a `0.y.z` release, v0.14.1 signals under Semantic Versioning that GoGraph's public
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

3. **A widened Cypher statement surface.** `rmp graph client` runs whatever the
   engine accepts, so the set of statements the engine accepts is Groadmap's
   Cypher surface, and it widens with the engine rather than with a Groadmap
   release. Groadmap neither publishes that set nor bounds it. The widening is
   invisible to the two checks an upgrade would otherwise rely on: a diff of
   removed or re-signed exported symbols finds nothing, because nothing was
   removed, and re-running the acceptance criteria finds nothing, because no
   existing criterion mentions a statement form that did not previously exist.

   One place in the product still reads that surface, and it is the one the
   widening can break. The web graph data endpoint injects a node `LIMIT` into the
   statement it is given unless the statement admits no `LIMIT` clause, which it
   decides from the statement's own grammar: a statement with no top-level `RETURN`
   carries no projection for a `LIMIT` to attach to (see
   `WEB.md § Graph Data Endpoint`, Suppression 2). That rule is general, so a new
   statement form is covered by it without having to be foreseen. What the widening
   can still introduce is a form that **does** carry a top-level projection and yet
   admits no `LIMIT` — a new sibling of the schema-introspection class, which is the
   one class the general rule does not reach and which the endpoint therefore
   recognises by name. Such a form is injected into and then fails in the parser — a
   statement `rmp graph client` runs becoming unusable through the endpoint, with a
   diagnostic that names the injected clause rather than the cause.

Mitigations required by this specification:

1. Groadmap MUST pin GoGraph to an exact version in `go.mod` (a specific immutable
   reference, not a floating or branch reference), so builds are reproducible. The
   pinned exact tag is recorded in `BUILD.md § External Dependencies`.
2. The graph feature MUST be implemented behind Groadmap's own command and
   error-handling boundary (this specification), so that an upstream API change
   is absorbed in one integration layer rather than spread across the codebase.
3. Upgrading GoGraph is a change that MUST be re-validated against the acceptance
   criteria in this file before release.

   **The re-validation MUST cover the relationship behaviours recorded in
   [What Groadmap Does Not Check](#what-groadmap-does-not-check), and MUST cover
   each of them in both directions.** Those items state measured properties of the
   pinned engine rather than properties of Cypher, and items 4 and 8 each assert a
   boundary that an upstream fix moves: an assertion that only checks the losing
   side is satisfied by an engine in which the working side has regressed to match
   it. For item 4: that a relationship property write through a pattern that walks
   against the stored arrow is still dropped, **and** that a `DELETE` over the
   same pattern still removes every relationship it matched. For item 8: that a
   write over a relationship variable bound by a `CREATE` or `MERGE` clause is
   still dropped, **and** that `ON CREATE SET` and `ON MATCH SET` still persist.
   For item 5: the reads it names are still resolved correctly. Item 4's loss is
   decided by the data rather than by the statement, so its fixture MUST fix the
   stored orientation of every relationship it measures and read each one back;
   a statement's shape does not tell the assertion what to expect.
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

5. **Which statements admit no `LIMIT` clause MUST be re-verified against the new
   engine.** Before an upgrade is released, the rule in
   `WEB.md § Graph Data Endpoint`, Suppression 2, MUST be re-checked against the
   engine being adopted, in both of its halves. The general half: a `LIMIT` MUST
   still attach only to a top-level projection, so that a statement with no
   top-level `RETURN` still admits none — a grammar in which a `LIMIT` attaches
   anywhere else invalidates the rule rather than one of its cases. The named half:
   the schema-introspection class MUST still be the only form that carries a
   projection and admits no `LIMIT`, and any new form of that kind the engine
   accepts MUST be added there deliberately rather than left to fail in the parser
   once the endpoint injects into it. A regression test MUST assert, form by form,
   which statements the endpoint injects into and which it leaves alone, and MUST
   cover the injecting half as well: a test that only checks suppression is
   satisfied by an endpoint that injects nothing at all. Symbol-level compatibility
   is NOT sufficient evidence here: the surface can widen with no symbol change at
   all.

### Engine Construction and Lifecycle

`rmp graph serve` is the only process that performs this sequence, and it performs
it once, at startup, rather than once per statement. On starting, the
implementation:

1. Resolves the graph directory for the selected roadmap (see [Persistence Layout](#persistence-layout)).
2. Takes the store's advisory lock, exclusively, before opening the store, and
   holds it until step 6 has completed (see
   [Concurrency and Recovery](#concurrency-and-recovery)).
3. Opens the GoGraph store rooted at that directory, recovering any committed
   state from the snapshot and write-ahead log.
4. Constructs the Cypher engine that will run the statement: it wraps the
   recovered graph and a write-ahead-log writer in a transactional store and
   constructs a store-backed engine over that store. There is one such
   construction and it is fixed by
   [Engine Constructor by Path](#engine-constructor-by-path).
5. Runs the statement through the engine's transactional path
   (`RunInTx` / `RunInTxAny`), so that a change it makes is committed atomically.
   It then iterates the result (`Columns`, then `Next` / `Record` until exhausted,
   checking `Err`), serialises it to JSON, and writes it to stdout.
6. After a transaction that appended to the write-ahead log has committed durably,
   produces a self-sufficient snapshot of the committed graph state and truncates
   the write-ahead log, synchronously, before the process exits (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)).
7. Closes the result and the store, ensuring committed writes are durable, then
   exits.

**There is one execution path, because nothing decides between two.** Groadmap
does not examine a statement to learn whether it reads or writes, so it cannot
choose an execution path from the statement. Choosing the transactional path for
every statement is the only choice that is correct for every statement: a writing
statement run on an engine constructed without a transactional store executes
against the recovered in-memory graph, commits nothing, writes nothing to the
write-ahead log, and still reports success, so the write is lost silently and the
caller has no reason to look again. The reverse cost — a statement that changes
nothing running inside a transaction and holding a write-ahead-log writer it never
uses — is paid in resources rather than in correctness, and it is the cost this
specification accepts.

Parameter binding: when query parameters are supported, the implementation binds
them through GoGraph's parameter-binding path (`RunInTxAny`, which accepts
`map[string]any`, or `cypher.BindParams` followed by `RunInTx`).

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
instead. The table covers every Cypher engine Groadmap constructs.

| Path | Surface | GoGraph constructor | Transactional store and write-ahead-log writer |
|------|---------|---------------------|------------------------------------------------|
| Transactional | `graph serve` | `cypher.NewEngineWithStoreAndRecovery`, over a transactional store, given the whole recovery result the store open returned | Both are opened: the write-ahead-log writer over `wal`, and the transactional store over the recovered graph and that writer |

There is **one** path and **one** surface on it. `rmp graph serve` is the only
process that opens a roadmap's graph store, so it is the only process that
constructs an engine at all (see
[The Dedicated Graph Server](#the-dedicated-graph-server)).

**The construction is literally one, in `internal/graphstore`, and the server
reaches it rather than repeating it.** That package owns the graph store's whole
lifecycle — the exclusive advisory hold, the recovery open, the write-ahead-log
writer, the transactional store, the engine over them, and the checkpoint — and
the server is on this path by calling it. A second construction anywhere in the
product is a second path, whatever constructor it names.

**The single row is what the withdrawal of the direct path leaves, and the rule it
carries is stronger than the one it replaces.** The table once carried a row for
`graph execute` and a row for the web graph data endpoint, because each opened the
store for itself. Neither does now: `rmp graph client` and the web graph data
endpoint reach the graph through the Bolt client alone
([Server Resolution](#server-resolution)), and a client constructs no engine. So
the requirement that every surface construct the same engine has become the
narrower and more easily checked requirement that **exactly one** construction
exists in the whole of production source, on one path, in one package. What differs
inside the server is how long the sequence is held open and when the checkpoint
runs, not what is constructed (see
[Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process)).

Groadmap constructs an engine through no other constructor. The pinned engine
also exposes `NewEngine`, `NewEngineWithOptions`, `NewEngineWithRegistry`,
`NewEngineWithStore`, `NewEngineWithStoreAndConstraints`, and
`NewEngineWithStoreAndSchema`; Groadmap uses none of the six, and adopting one is
a change to this table before it is a change to the code.

**Why the one path must be the transactional one.** The server executes
caller-supplied Cypher and does not examine it, so a statement that arrives — from
`rmp graph client` or from the web page's query bar alike — may write, delete, or
change the schema (see `WEB.md § Graph Data Endpoint`). A server constructed
without a transactional store would run such a statement against its own in-memory
graph, acknowledge the commit, and lose it when the process ended. The write would
be reported as done and would not exist. Both surfaces read their answer from that
one engine, so the constructor this table fixes is what makes either surface's
answer true.

**Why the constructor takes the whole recovery result rather than the schema
alone.** The declined `NewEngineWithStoreAndSchema` re-registers the same
constraints and the same index definitions, under the same declared names, and
answers every statement identically. What it does not carry is the snapshot's
index payloads, so an engine built through it **rebuilds every index by a full
scan of the recovered graph** each time the store is opened. The constructor this
table gives loads each index from the payload the snapshot already holds, wherever
recovery certifies that safe, and falls back to the same rebuild where it does
not.

**The reason that difference matters is what a rebuild is amortised over.**
"Each time the store is opened" now means once per server, because
`rmp graph serve` is the only process that opens one, so a full-scan rebuild is
paid at startup and then spread over every statement that server answers. That is
a far better position than the one this choice was originally made in, when a
store was opened once per command and the rebuild was amortised over nothing at
all. The constructor is kept: a rebuild the operator waits for at startup is
still a cost, it grows with the graph, and nothing about the change makes the
declined constructor preferable. What the change removes is the argument's
urgency, not its direction.

**No speed-up is claimed at present scale, and measurement finds none.** The
project's own knowledge graph holds a few hundred nodes in about a megabyte, and
at that size a full-scan rebuild is cheap. It is not cheap because the indexes
would go unused. The engine admits a seek only for a label whose population
reaches a floor the engine owns — one floor, gating every seek plan it has, over
hash and comparison-ordered indexes alike — and several of this graph's labels
are above the floor the pinned engine applies, so a plan over today's graph can
select an index. What is absent is a measurable gain, not the opportunity for
one. Measured against this project's own graph, on a label of a few hundred nodes
over repeated invocations, the same query with an index and without one are
indistinguishable within the run-to-run spread. Those figures were measured when
a command opened the store for itself, so the process start and the store open
dominated whatever the plan chose; against a running server neither cost is paid
per statement, and the measurement has not been repeated in that arrangement.
The constructor chosen here is therefore about what happens as a graph grows, and
this specification does not assert that it makes any statement measurably faster
today. Should a future change seek a
measured improvement, the rule below applies to it: a path moves on evidence
gathered against this project's own graph, never on an upstream recommendation,
and the table is amended first.

**The floor's value is deliberately not restated here, and no conclusion above
rests on it.** It is an engine-internal constant, set from the engine's own
measurements rather than from any property of index seeks in general, and the
engine has already lowered it by more than an order of magnitude between two
releases this project pinned in succession. Restating the number would give this
specification a fact that a dependency bump can falsify in silence: no exported
symbol changes, so a symbol diff reports nothing, and no acceptance criterion in
this file names the constant, so re-running the criteria reports nothing either.
That is the hazard [Dependency Maturity Risk](#dependency-maturity-risk)
describes for the statement surface, in a second guise. Naming the floor as an
engine-owned threshold, and resting the paragraph on a measurement rather than on
the threshold's value, is what keeps the paragraph true across the next bump.
[Engine Construction and Lifecycle](#engine-construction-and-lifecycle) states
the parallel-scan gate's threshold in the same shape, as an order of magnitude
rather than a constant, for the same reason.

**The recovery result handed to the constructor MUST be the one that opened this
store.** It is the result of the completed store open for this roadmap's graph
directory, in the same invocation or the same request, and it is passed whole
rather than as extracted fields — which is the reason the engine offers it in that
shape: a handoff the caller must remember to perform is one a caller eventually
forgets, and forgetting this one is silent, costing correctness nothing and
rebuilding everything. A result from any other open would describe a different
graph, and neither the engine nor the store can detect the substitution, because
the mismatch is in the caller's wiring and not on disk.

**The engine is given the schema the store open recovered, and it may not be
given less.** Opening the store returns the graph together with the index and
constraint definitions committed to it. A constructor that takes the graph alone
discards those definitions, and an engine built that way reports an empty schema
whatever the store holds: `SHOW INDEXES` answers with no rows, and `DROP INDEX`
fails as though the index had never been created. Passing the recovered schema is
not an optimisation; it is what makes the engine report the schema the store
actually holds.

**The constructor this table gives is the one the engine names as recommended for
opening a persisted store, and the alternative it replaces is not merely less
informative.** An engine opened over a store that holds durable constraints
without being given them does not report an empty constraint set: it re-registers
what it finds in the store under **synthesised** names, so a constraint the caller
created as `spec_key_uq` is reported under a name the caller never chose and
cannot use in a `DROP CONSTRAINT`. Secondary indexes are not re-registered at
all. The engine emits a warning at construction saying so and naming the
constructor that avoids it. Groadmap uses that constructor, so the names it
reports are the names the caller declared.

**Moving a surface off this path is a change to this table first.** It requires
the same kind of measured evidence that
[Engine Construction and Lifecycle](#engine-construction-and-lifecycle) demands
before an engine option is changed, and it requires an answer to the question this
section settles: what runs the statements that write, and how a lost write is
prevented.

### Synchronous Checkpoint on Write

**This section fixes what a checkpoint is: the condition under which one is
owed, what the snapshot it writes MUST contain, and what a failed one means.**
`rmp graph serve` is the only process that runs one, because it is the only
process that opens the store
([Engine Constructor by Path](#engine-constructor-by-path)); when it runs one, and
how often, is stated in
[Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process),
which applies this section's condition rather than declaring a second one. A
checkpoint is synchronous with respect to the sequence that owes it: it runs on
the engine's own commit serialiser or inside the shutdown sequence, never as a
background goroutine racing either.

**What decides whether a checkpoint is owed is the write-ahead log, not the
statement.** Groadmap does not examine a statement to learn whether it writes, so
it cannot decide in advance. The transaction runs, and a fold is owed only when
that transaction appended to the write-ahead log. A statement that appended
nothing owes none, and neither the snapshot directory nor the log is touched on
its account (see
[What a Statement That Writes Nothing Changes on Disk](#what-a-statement-that-writes-nothing-changes-on-disk)).
Folding unconditionally would rewrite a full snapshot of the whole graph after
every statement, including one that only counted nodes, which is a cost
proportional to the graph paid for no change at all.

Sequence and durability boundary:

1. The transaction commit is and remains the durability boundary. Once the write
   transaction has committed durably, the user's change is persisted in the
   write-ahead log and is guaranteed to survive recovery, independent of whether
   the checkpoint that follows succeeds.
2. After a successful commit, and before closing the store, the implementation
   writes a full snapshot of the committed graph state. The snapshot MUST be
   self-sufficient: it carries the node-identifier-to-key mapping needed to
   interpret the graph on its own, it captures the set of deleted (tombstoned)
   nodes, and it captures the **registered schema** — the definitions of every
   index and every constraint the graph carries — so that the snapshot plus any
   write-ahead-log tail is enough to reconstruct the graph and truncating the log
   loses no committed data. Because the deletion tombstone set is part of the
   snapshot, a node deleted by a write stays deleted after the log is truncated
   and the store is reopened; it does not reappear on recovery. Because the schema
   definitions are part of the snapshot, an index or a constraint created by a
   write is still registered after the log is truncated and the store is reopened;
   it does not vanish.

   **The schema clause of this requirement is load-bearing and MUST NOT be read as
   a restatement.** An index or constraint definition is committed data that lives
   in the write-ahead log until a snapshot carries it. A snapshot that omits it,
   followed by the truncation in step 3, destroys it: the next invocation opens a
   graph whose schema is empty, `SHOW INDEXES` reports nothing, a `DROP` of the
   object fails as though it had never existed, and — the worst of the three — a
   `UNIQUE` constraint stops being enforced while the data it was declared to
   protect is still there. One checkpoint is enough to do this, and a server takes
   one at shutdown whenever the log has grown. A snapshot that carries the graph but not
   its schema is therefore not self-sufficient in the sense this requirement uses,
   and the implementation MUST write the schema-carrying form (see
   [Schema Management](#schema-management)).

   **The schema the snapshot carries is the one the engine holds registered at
   the moment of the checkpoint**, obtained from the engine that just executed
   the statement. It MUST NOT be a set Groadmap accumulates, remembers, or
   reconstructs on its own: the engine is the only party that knows what is
   registered after a statement has run, and a second record kept beside it would
   be a copy free to disagree with it. The checkpoint therefore runs with access
   to that engine, which is a consequence of this requirement rather than an
   independent design choice.
3. After the self-sufficient snapshot is durable, the write-ahead log is
   truncated. Truncation bounds the log's growth: without it the log grows with
   every write for the life of the graph (see [Concurrency and Recovery](#concurrency-and-recovery)).

Failure policy:

1. A checkpoint failure that occurs **after** the transaction has already
   committed durably MUST NOT fail the user-visible write. The write has already
   succeeded.
2. In that case the caller still receives its normal success result (the
   `RETURN`-mirroring shape or `{"ok": true}`) and exit code 0. A failed
   checkpoint after a durable commit is a degraded-but-correct state: the
   write-ahead log is intact, so recovery still restores the committed state, and
   the next successful checkpoint reconciles the snapshot. **One
   cause of checkpoint failure does not reconcile, and is a condition of its
   own**: a field the snapshot format cannot carry is committed graph state, so
   every later checkpoint refuses for the same reason until that field is removed
   (see [Field Length Limits](#field-length-limits), rules 6 to 9).
3. The checkpoint failure is surfaced on the server's own stderr, as a record of
   the kind [Server Diagnostics on Stderr](#server-diagnostics-on-stderr)
   governs, **without** changing the result the caller reads or the exit code it
   returns. The caller that ran the statement is not told: its write succeeded,
   and the condition is the operator's to act on. This is the one place where a
   diagnostic accompanies an acknowledged success.
4. A failure that occurs **before or during** the commit (the transaction does
   not commit durably) is a normal write failure, not a checkpoint failure: the
   write did not succeed, no checkpoint is attempted, and the caller fails with
   `utils.ErrGraphEngine` (exit code 1) per
   [Error Handling and Exit Codes](#error-handling-and-exit-codes).

Performance trade-off: a full snapshot makes each fold cost proportional to the
live graph size (the snapshot rewrites the committed state), in exchange for a
write-ahead log that stays bounded and a recovery cost proportional to the live
graph size rather than to the full write history. That cost is what makes the
server fold on a cadence rather than after every committed write, which is the
division
[Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process)
fixes. The trade-off is recorded in
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
├── graph.sock            # Graph server socket, mode 0600 (present only while a server runs)
└── graph/                # Knowledge graph store (GoGraph)
    ├── write.lock        # Groadmap's store access lock (see Concurrency and Recovery)
    ├── wal               # Write-ahead log (truncated after each checkpoint)
    └── snapshot/         # On-disk snapshot, present after the first write
        ├── manifest.json   # Snapshot manifest (GoGraph-owned)
        ├── tombstones.bin  # Deleted-node tombstone set (present only when the graph has tombstoned nodes; GoGraph-owned)
        ├── constraints.bin # Declared constraint definitions (present only when the graph carries declared schema; GoGraph-owned)
        ├── indexdefs.bin   # Declared index definitions (present only when the graph carries declared schema; GoGraph-owned)
        └── ...             # Snapshot data files (GoGraph-owned)
```

Rules:

1. The graph store is a **directory**, not a single file, because GoGraph
   persists through an on-disk snapshot plus a write-ahead log. The directory is
   `~/.roadmaps/<name>/graph/`.
2. **The graph directory is created by `rmp graph serve`, and by nothing else.**
   A server started for a roadmap that has no graph yet creates the directory,
   with mode `0700`, and opens an empty store in it (see
   [Server Startup](#server-startup), step 1). `rmp graph client` and the web
   graph data endpoint never create it, because neither opens a store at all (see
   [Server Resolution](#server-resolution) and `WEB.md § Security and
   Constraints`, rule 4). A read run against a roadmap whose graph has just been
   created returns an empty result; it is not an error.
3. The `snapshot/` subdirectory (including its `manifest.json`) is produced by the
   synchronous checkpoint that follows a transaction that wrote (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)). It is
   expected to be present after the first statement that changed the graph. A
   graph against which no statement has ever written has no `snapshot/`
   subdirectory and holds a `wal` file that recovery finds empty, because the
   snapshot is produced by the checkpoint and the checkpoint has never run. Such a
   directory holds `write.lock`, and it holds that only once a statement has
   actually opened the store.
4. The graph directory uses permissions `0700`, consistent with the roadmap home
   directory and the data directory (see `ARCHITECTURE.md § Directory Structure`).
5. The internal file names and on-disk format inside `graph/`, including the
   layout and contents of `snapshot/` and the format of `wal`, `manifest.json`,
   `tombstones.bin`, `constraints.bin`, and `indexdefs.bin`, are owned by GoGraph
   and are not specified here. The one exception is `write.lock`, which GoGraph
   knows nothing about: Groadmap creates and maintains it, and it is specified in
   [Concurrency and Recovery](#concurrency-and-recovery). Its contents are never
   read or written; only the advisory lock on it carries meaning.

   Three of the snapshot's components are **optional**, and each is emitted only
   when the graph has something for it to hold: `tombstones.bin` when the graph
   has tombstoned (deleted) nodes, `constraints.bin` when constraints are
   declared over it, and `indexdefs.bin` when indexes are. A graph that has never
   had a node deleted, and one over which no schema has ever been declared, need
   contain none of them.

   The last two are named here rather than left to the diagram's ellipsis because
   they are what makes a guarantee stated elsewhere true. They carry the declared
   schema across the write-ahead-log truncation that follows every checkpoint, and
   so they are the on-disk form of the requirement in
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write) that a
   snapshot be self-sufficient, and of the criterion that a schema change survives
   a reopen. A snapshot written without them, over a graph that carries schema,
   loses that schema the moment the log is truncated.

   **Their arrival is not a format change, and no migration is implied.** Both
   entries are additive: they are appended to the manifest's file list without
   changing the manifest version, exactly as `tombstones.bin` was. A store written
   before either existed is read unchanged, and a reader that predates one of them
   ignores the unrecognised file name rather than failing. Nothing about them
   affects [Dependency Maturity Risk](#dependency-maturity-risk), risk 2, which
   concerns a format change that a newer engine does not read.

   Apart from `write.lock`, Groadmap treats the directory as an opaque store
   managed through the engine. The diagram above names an entry only to document
   that it is expected to appear, and never to specify its internal format; it is
   not an exhaustive listing of what the engine may place there, and an entry it
   does not name is not thereby forbidden.
6. Removing a roadmap (`rmp roadmap remove <name>`) deletes the entire roadmap
   home directory recursively, which includes `graph/`. No separate graph-removal
   command is required (see `COMMANDS.md § Remove Roadmap`).
7. The roadmap home directory layout, including the graph subdirectory, is
   described in `ARCHITECTURE.md § Directory Structure`. This file is the
   canonical source for the `graph/` subdirectory.
8. **`graph.sock` is the graph server's socket, and it is not part of the
   store.** It lives in the roadmap home directory rather than inside `graph/`,
   because `graph/` is GoGraph's directory and `write.lock` is the one entry in it
   Groadmap owns. It carries no data: it is a rendezvous point, it exists only
   while a server is running or has been killed without removing it, and deleting
   it while no server is running loses nothing.
   [Socket Path and Permissions](#socket-path-and-permissions) is canonical for
   its path, its mode, and what a leftover one means. A roadmap that has never
   been served has no such entry, and a roadmap served on a non-default path has
   it wherever `--socket` put it.

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

1. **Groadmap places no uniqueness constraint on a graph, and one placed by the
   caller would not enforce this invariant anyway.** Groadmap emits no constraint
   DDL of its own on any code path, so no `rmp` command puts a constraint on a
   graph as a side effect of anything else it does. A caller may declare one
   deliberately, because `graph client` runs `CREATE CONSTRAINT` like any other
   statement (see
   [Schema Management](#schema-management)); that is the caller's own instrument,
   which Groadmap neither issues, requires, nor assumes. It would not make this
   convention enforced, because the two judge sameness differently: this section's
   invariant is judged on the **NFC** form, while the engine's `UNIQUE`
   enforcement compares a string property's **raw bytes**, with no normalisation
   of any kind. A `UNIQUE` constraint on `key` therefore refuses a byte-identical
   repetition and admits the very pair this convention calls a violation — a
   precomposed key beside its decomposed twin. Declaring one narrows the gap; it
   does not close it, and the audit below remains the way a violation is found.
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

**Step 1 — read every key, with `rmp graph client`.** This statement reads and
changes nothing:

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

## What Groadmap Does Not Check

`rmp graph client` hands the statement to the engine. Between reading the
statement and running it, Groadmap checks its length and nothing else about its
content: it does not parse it, does not classify it, does not inspect the patterns
it binds, and does not inspect the values it would write. The web graph data
endpoint is bound by this section identically, because it runs the statement it is
given on the same path (see `WEB.md § Graph Data Endpoint`).

This section enumerates what follows. Every item but one is a hazard: a real
outcome of a real statement, silent, reporting success. They are stated here so
that a caller meets them in the specification rather than in the store.

Item 5 is the exception and states the opposite of a hazard: a direction in which
the engine is correct. It is here because it is the neighbour of item 4 and would
otherwise be inferred from it — a reader told that a relationship property write
may be dropped when the pattern walks against the stored arrow has every reason
to assume the read is unreliable in the same way, and that assumption is false.
Item 5 is a measured property of the pinned engine, not a property of Cypher, so
it is re-measured whenever the pin moves (see
[Dependency Maturity Risk](#dependency-maturity-risk), mitigation 3).

Items 4 and 8 are two members of one family: a relationship property write
dropped in silence, by two independent causes that share an outcome. They belong
side by side and are numbered apart because these item numbers are cited from
elsewhere in this specification and are not renumbered. Each of the two names the
other, and neither one's workarounds are the other's.

1. **A statement runs whatever it says.** There is no subcommand whose contract is
   "this cannot delete". A statement that deletes reaches the engine the same way
   one that counts does, so the protection against deleting through a command
   believed to be read-only is the caller's own care with the text it supplies.

2. **A statement whose bytes are not valid UTF-8 executes.** The engine decodes
   the statement to characters before its grammar runs and replaces every byte
   that decodes to no character with `U+FFFD` (REPLACEMENT CHARACTER). The
   statement the engine executes is therefore not the statement the caller wrote,
   and no later point can recover the byte the caller supplied. A write stores a
   value that was never supplied; a match compares against a literal that was never
   supplied, so a row that should have matched does not and the command reports
   success having found nothing; and a deletion gated by such a literal removes
   nothing and still reports success.

3. **A property value carrying a control character is stored.** Cypher decodes
   escape sequences inside a string literal — among them `\b` (backspace), `\f`
   (form feed), and `\uXXXX`, a code point written as four hexadecimal digits — so
   a statement whose own text is pure ASCII can write a value that carries a real
   control character. `SET n.body = 'red\u001b[31m'` writes an `ESC` (`U+001B`)
   into the store, and every later surface that renders that value renders the
   control character with it. The free-text control-character constraint that
   governs task and sprint fields (`MODELS.md § Task`) does not reach
   knowledge-graph property values.

4. **A relationship property write persists only where the pattern walked the
   relationship the way storage holds it, and where it does not the statement
   still reports success.** The engine writes a relationship property by its
   endpoint pair, and it takes that pair from the columns the expansion emitted.
   Those columns carry the relationship the way the **pattern** walked it, not the
   way storage holds it. A `SET e.k = …`, a `SET e = {…}`, a `SET e += {…}` and a
   `REMOVE e.k` therefore persist, for each relationship the statement matched,
   only where the node bound at the pattern's **left** position is that
   relationship's stored source and the node bound at its **right** position is
   its stored target. Every other matched relationship is addressed as the
   reversed pair and lost. No error is raised, no notification is attached, and
   the transaction still commits.

   **The write is not refused; it is misfiled.** The engine holds relationship
   properties in two stores, and the reversed pair defeats both — but only one of
   them by declining to act. The per-pair store answers a write against a pair
   that carries no relationship with a documented no-op. The by-handle store does
   not test the pair at all: it records the property under the relationship's
   correct handle in a bucket keyed by the reversed pair, and a read keys on the
   pair the same way, so the value lands in a bucket no read consults. The write
   happens and nothing can observe it.

   **How much of a statement's write survives is decided by the data, not by the
   statement.** The same statement over the same schema may write every
   relationship it matched, some of them, or none, according to how those
   relationships happen to be oriented in storage. An undirected pattern anchored
   on one node writes both of two relationships that point at the anchor, writes
   one of a pair with one pointing each way, and writes neither of two that point
   away from the anchor — reporting `{"ok": true}` for a statement that changed
   nothing. Moving the anchor to the pattern's other side writes the other
   relationship instead. An **incoming** pattern is the extreme case of the rule
   rather than a separate one: every relationship it binds is bound against its
   stored arrow, so it loses the whole of its write, and
   `MATCH (v:Test {key:'…'})<-[e]-(s) SET e.last_commit = '…'` writes nothing.

   **The selective statement is the hazardous one and the sweeping statement is
   safe**, which inverts the order a caller would triage in. An undirected pattern
   with neither endpoint pinned writes **every** relationship it matches: with
   nothing to prune the rows, the expansion emits each relationship twice, once
   per direction, and one of the two rows is oriented the way storage holds it.
   Any filter that narrows the match to one row per relationship — an inline key,
   a `WHERE`, a bound second endpoint — can leave the reversed row as the
   survivor.

   **The engine's own write-effect counters do not reveal it.** A `SET` that wrote
   nothing still reports one property set per matched row, because the counter is
   incremented above the layer that dropped the write, so its number is the same
   number the statement reports when every write lands. The counter for removals
   is the one that reports what landed rather than what was attempted, and it is
   not a detector either: a `REMOVE` whose only write was dropped reports zero,
   and so does a `REMOVE` of a property that was genuinely absent, so the number
   means something only to a caller who already knows how many relationships
   carried the property. Groadmap surfaces no counter on any path.

   **`DELETE` is unaffected, and not by accident.** A `DELETE` over an incoming or
   undirected pattern removes every relationship it matched — through an anchor,
   over parallel relationships, on a node pair joined both ways, and under a
   predicate over `startNode(e)`. The engine's delete operator detects the
   reversed pair and retries against the stored orientation; the property-write
   operators were never given that step. The divergence is confined to property
   writes, and that boundary is the part of this item an engine upgrade is most
   likely to move (see [Dependency Maturity Risk](#dependency-maturity-risk),
   mitigation 3).

   **The reach is unaffected, and two forms write whatever they match.** Every
   relationship is writable through an **outgoing** pattern, because an outgoing
   pattern may be anchored on either endpoint, so
   `MATCH (s)-[e]->(v:Test {key:'…'}) SET e.last_commit = '…'` writes what the
   reverse form did not. A statement that must write without knowing the stored
   direction may instead project the relationship before writing it — either
   across a `WITH`,
   `MATCH (a {key:'…'})-[e]-(b {key:'…'}) WITH e SET e.last_commit = '…'`, or
   inside a `FOREACH` over the collected relationships. Both are measured to write
   every relationship they match, whichever way the pattern walked it, because the
   projected value carries storage's own endpoints. Neither workaround extends to
   item 8, whose binding comes from a write clause rather than from a match.

5. **A relationship read through an incoming or undirected fixed-length pattern
   is reported correctly, and this is measured rather than assumed.** The reach of
   item 4 stops at writing. Reading a bound relationship resolves its identity, its
   type and its stored orientation whichever way the pattern walked it, including
   on the shape that is hardest for an engine to get right: a node pair joined in
   **both** directions, where an implementation that inferred the relationship from
   the endpoint pair alone would find one in the emitted order and report the
   forward leg twice. Measured at the pinned engine version, on such a pair, every
   one of the following is correct — a projection over an undirected pattern
   reports each relationship once; `startNode(e)` and `endNode(e)` under an
   incoming pattern report what storage holds; a `WHERE` predicate over the
   relationship selects the relationship the traversal bound; a `SET` whose
   right-hand side reads the relationship persists the true value; and a `DELETE`
   gated by such a predicate removes the relationship the predicate names and
   leaves its sibling in place. The same holds for a **variable-length**
   relationship (`-[e*1..2]-`, and equally `-[e*1..1]-`), for a projected **named
   path** (`MATCH p=(a {key:'…'})-[e]-(b) RETURN p`), and for a bare `DELETE e`.

   The line between this item and item 4 is the line between **reading** a
   relationship and **addressing** it. A right-hand side or a `WHERE` that reads a
   bound relationship sees the orientation storage holds; a write whose target is
   that same relationship is governed by item 4 regardless. A statement may
   therefore select exactly the relationship its author meant and write nothing to
   it: in
   `MATCH (n)-[e]-(m {key:'b'}) WHERE startNode(e).key = 'b' SET e.stamp = 'x'`,
   the predicate is evaluated against the stored orientation and binds the one
   relationship it names, correctly, and the `SET` behind it is dropped.

   This is a statement about GoGraph at the pinned tag and about nothing else.
   Groadmap does not verify it per statement, cannot repair it if a later engine
   regresses, and does not refuse the shape: acceptance criterion 38's fourth
   bullet is the assertion that would fail if it stopped holding, and mitigation 3
   of [Dependency Maturity Risk](#dependency-maturity-risk) is what makes that
   assertion run before a new engine is adopted.

   Reading through an outgoing pattern is correct whatever the data and whatever
   the engine, because nothing about the stored orientation has to be recovered.
   That form therefore remains the one to reach for where a statement must hold
   independently of the pin, and both directions are read in one statement as the
   union of the two outgoing legs:

   ```
   MATCH (a {key:'…'})-[e]->(x) RETURN type(e) AS t, x.key AS k
   UNION ALL
   MATCH (x)-[e]->(a {key:'…'}) RETURN type(e) AS t, x.key AS k
   ```

6. **A schema statement carrying a further clause after it executes in part.** The
   engine's schema parser stops as soon as its grammar is satisfied and discards
   the rest of the statement without an error, without a notification, and without
   any other trace. Handed
   `CREATE INDEX spec_key FOR (n:Spec) ON (n.key) MATCH (m) SET m.reviewed = true`,
   the engine creates the index, drops the `MATCH ... SET` on the floor, and
   returns success, so `rmp graph client` prints `{"ok": true}` and exits 0 for a
   statement half of which never ran. This is a property of the engine's schema
   parser and applies to the four schema-mutating DDL statements; a
   schema-introspection command carrying a further clause is refused by the engine
   itself, which names the unsupported clause and discards nothing.

7. **A schema-introspection command written with anything but a single space
   between its two keywords fails as a syntax error.** The engine decides whether
   to route a statement to its schema-introspection parser by testing it against
   the literal prefixes `SHOW CONSTRAINT` and `SHOW INDEX`, each carrying exactly
   one space; it trims leading whitespace and leading comments before that test, so
   the separator between the two keywords is the only spacing that matters. A
   statement that misses those prefixes by its spacing is routed to the general
   Cypher grammar, which has no `SHOW` production and rejects it with a diagnostic
   that reports `SHOW` as unexpected and lists the clause keywords it did expect.
   Nothing in that message points at the spacing, so it reads as though schema
   introspection were unsupported, while the identical statement with a single
   space returns its result set. `SHOW  INDEXES` fails; `SHOW INDEXES` succeeds.
   The same is true of the four DDL forms: `CREATE   INDEX ...` is refused by the
   general grammar rather than routed to the schema parser, and
   `CREATE INDEX ...` is not.

8. **A relationship property write whose target was bound by a `CREATE` or a
   `MERGE` clause in the same statement does not persist at all, and the statement
   reports success.** This is the second member of item 4's family and an
   independent defect with the same outcome: there the relationship is identified
   correctly and its endpoint pair is reversed; here the endpoint pair is right
   and the identity is not. A relationship variable bound by a write clause
   carries an identifier synthesised from its two endpoints rather than the stable
   handle that names the relationship, so a write addressed by that identifier
   names no relationship at all. Nothing is written, no error is raised, no
   notification is attached, and the transaction still commits.

   **It is the binding's origin that decides this, and nothing else.** Whether the
   clause is `CREATE` or `MERGE` makes no difference; whether the relationship is
   new or already existed makes no difference; and a relationship bound by a
   `MATCH` still persists its write across an intervening `CREATE` or `MERGE`
   clause, so a write clause standing between the binding and the `SET` is not
   what does the damage.
   `MATCH (a:Spec {key:'…'}), (b:Test {key:'…'}) CREATE (a)-[e:VERIFIED_BY]->(b) SET e.last_commit = '…'`
   loses its write with no `MERGE` anywhere in it.

   **Every property-write form over such a binding is lost**: `SET e.k = …`,
   `SET e = {…}`, `SET e += {…}` and `REMOVE e.k`; each hop of a multi-hop `MERGE`
   pattern; and the write however it is reached, whether directly, across a `WITH`
   or inside a `FOREACH`. Item 4's two workarounds are precisely this item's
   defect: projecting the relationship first does not rescue a binding whose
   identity was wrong before the projection.

   **`SET e = {…}` is the most deceptive of them**, because a `RETURN` in the same
   statement echoes back the value the statement did not write, while a later
   invocation reads the property absent. The scalar form reports the absence in
   both places, so the shape that looks most confirmed is the one that persisted
   least.

   **`DELETE e` is unaffected**: the identifier is good enough to destroy the
   relationship and not good enough to write one of its properties. Nor is
   anything about the binding wrong to read — `type(e)`, `startNode(e)` and
   `endNode(e)` all report what storage holds — so nothing observable about it
   warns the caller.

   **The idiomatic forms are sound, and that is what makes this avoidable.**
   `ON CREATE SET` and `ON MATCH SET` both persist — on a relationship the `MERGE`
   created, on one it matched, and on every hop of a multi-hop pattern — as do
   inline pattern properties, `MERGE (a)-[e:VERIFIED_BY {last_commit:'…'}]->(b)`,
   and re-binding the relationship with a `MATCH` after the write clause. A
   statement that stamps a property on a relationship it is creating should be
   written in one of those forms rather than with a trailing bare `SET`.

**The divergences in items 4 and 8 are upstream in GoGraph and cannot be
corrected from this repository.** Both are measured properties of the engine at
the pinned tag, as item 5 is. Groadmap holds no position from which to repair
either: the write is dropped below the engine's own accounting, so what reaches
Groadmap is a committed transaction reporting the property set, which is
indistinguishable from the same statement having written. Detecting either would
require reading the relationship back and comparing, which is the caller's
statement to write and not Groadmap's to insert. Recognising the statement shapes
instead is what the paragraph below rules out, and for item 4 it would be unsound
as well as forbidden: how much of a write survives is decided by the data, so a
rule that refused the shapes which can lose a write would refuse the many
statements of that shape which write every relationship they match.

**None of the items above is a reason for Groadmap to inspect a statement.** A
check for any one of them would introduce the coupling this specification does not
carry: Groadmap would hold an opinion about which Cypher the engine ought to run,
that opinion would be narrower or wider than the engine's own on the day the
engine changed, and the caller would be refused a statement the engine would have
executed or admitted one it would not. The statement surface belongs to
the engine (see [Dependency Maturity Risk](#dependency-maturity-risk), risk 3).
What this specification owes instead is that these outcomes are written down.

## Literal-Aware Normalization

A decision taken about a Cypher statement by inspecting its text MUST run on a
**masked normalization** of that statement, never on the raw string. The mask
neutralizes the contents of Cypher string literals, comments, and backtick-quoted
identifiers, so that a keyword appearing only inside a property value can never
affect the decision.

One such decision exists in the product, and this section is canonical for the
normalization it runs on: the web graph data endpoint decides whether to inject a
node `LIMIT` into the statement it was given, which requires it to know whether the
statement already carries a top-level `LIMIT` and whether it is a form that admits
one at all (see `WEB.md § Graph Data Endpoint`). `rmp graph client` takes no such
decision and performs no masking: it checks the statement's length and runs it.

Masking rules:

1. **String literals (mandatory).** Both single-quoted (`'...'`) and
   double-quoted (`"..."`) Cypher string literals are masked. Masking replaces
   the interior characters of each literal with a neutral placeholder character
   (for example, a space), while leaving the surrounding statement structure
   intact. The quote delimiters and the overall positions of surrounding tokens
   are preserved so that the decision sees the same statement shape with only the
   literal contents neutralized.
2. **Backslash escape sequences.** While scanning a string literal, a backslash
   escape sequence (for example `\"`, `\'`, `\\`) does not terminate the literal:
   an escaped quote is part of the literal value, not its closing delimiter. The
   scanner honors these escapes so that a literal ends only at its true,
   unescaped closing quote.
3. **Comments and backtick identifiers (robustness).** For robustness, keyword
   text inside line comments (`// ...` to end of line), block comments
   (`/* ... */`), and backtick-quoted identifiers (`` `...` ``) MUST likewise not
   influence the decision, and is masked under the same neutralization. The
   string-literal masking in rule 1 is the mandatory normative requirement; the
   comment and backtick-identifier masking is an additional robustness
   requirement applied by the same normalization.

The statement that is actually executed against the store is always the
**original, unmodified** statement, with only the endpoint's own `LIMIT` clause
appended where it injects one. Masking affects the decision and never the text
that runs.

The masked normalization validates no Cypher syntax and refuses nothing. A
statement is not rejected on anything the mask reveals; the mask exists so that a
`LIMIT` written inside a string literal is not mistaken for the statement's own.

## Cypher Input Source and Precedence

`rmp graph client` obtains its Cypher from one of two sources:

1. The `-q` / `--query "<cypher>"` flag.
2. Standard input, read under a bound, when the `--query` flag is absent. This
   allows piping a statement, for example
   `cat statement.cypher | rmp graph client -r myproject`.

Whichever source carries it, a query is subject to the maximum length stated in
[Maximum Query Length](#maximum-query-length) below.

**These two sources are the only ones, and this section is canonical for
them.** A statement reaches `graph client` through the `--query` flag or through
standard input and through nothing else: there is no `--statement` flag, and a
statement written as a positional argument is refused. Where the statement is sent
once it has been read is fixed by [The Bolt Client](#the-bolt-client).

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
5. Leading and trailing whitespace is trimmed from the query before execution.
   The trim happens **after** the length check, which counts the bytes as
   supplied (see [Maximum Query Length](#maximum-query-length)).

Standard input carries the Cypher query itself here: what `graph client` reads
from it is the instruction it runs, not a value it stores. Other
commands accept standard input as well, and the cross-cutting input rule is
stated in `DATA_FORMATS.md § Input`, which is the canonical statement of every
command that reads standard input: it lists the `--query` of `graph client`
together with the `--body` of the comment subcommands of the `task` and `sprint`
families.

### No Positional Query: A Stray Token Is Refused

The two sources above are the only two. `graph client` accepts **no positional
argument at all**: it declares a maximum of zero, which is what
`COMMANDS.md § Positional Arity by Command` publishes for it. A Cypher query
written bare on the command line is therefore not a third source. It is an excess
positional argument, and the subcommand refuses it.

`graph client` declares the same maximum of zero and refuses a positional
argument the same way, with the same line, for the same reason: it takes its
statement from the same two sources and from no third one. `graph serve` declares
a maximum of zero as well, and refuses an excess positional argument under the
CLI-wide wording of `COMMANDS.md § Positional Arguments`, rule 1, rather than the
line below, because it takes no Cypher statement at all and the hint names two
sources it does not have. Everything the rules below say of `graph client` holds
of `graph client` word for word.

The rules are:

1. **Which tokens are positional arguments at all.** This must be settled before
   a token can be called unexpected, because it decides which of two errors a
   `-`-prefixed token draws. Rule 4 of the precedence rules above is canonical
   for the classification: a
   token is flag-like when it begins with `--`, or with a single `-` immediately
   followed by an ASCII letter. Every other token is a positional argument,
   including a `-` followed by a digit or a decimal point (`-1`, `-0.5`) and a
   bare `-`. A flag-like token that `graph client` does not define is refused as
   an unknown flag, under the CLI-wide wording `COMMANDS.md § Positional Arguments`
   rule 5 publishes; every other stray token is refused by rule 2 below. This is
   the one point on which `graph client` and the comment subcommands classify
   the same token differently, and each states its own rule: on a comment
   subcommand a stray `-1` is an unknown flag
   (`COMMANDS.md § Comment Positional Argument Contract`, rule 2).
2. **The refusal.** An invocation that supplies a positional argument is refused
   with `utils.ErrInvalidInput` (exit code 2) and this line on stderr:

   ```
   Error: invalid input: unexpected argument "X" (graph queries use --query or stdin)
   ```

   `X` is the offending token, quoted and echoed exactly as the user supplied it.
3. **Only the first offending token is named.** The tokens are examined left to
   right and the first positional argument ends the invocation, so
   `rmp graph client -r <roadmap> --query "<cypher>" alpha beta` names `alpha`
   and never mentions `beta`.
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
     [Maximum Query Length](#maximum-query-length).

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
   **Cypher query**, which only `graph client` has; the comment subcommands,
   whose body has two sources of its own, publish the canonical line without it,
   and a hint naming `--query` would be false on them. An edit to either family
   must therefore keep the shared part of the line shared and keep this hint
   confined to `graph client`.

### Maximum Query Length

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
3. **The length check runs first.** It precedes the opening of the graph store
   and the engine. An over-long query is never parsed and never executed; nothing
   in the graph changes and stdout stays empty.
4. **Why 1 MiB and not something tighter.** One MiB is roughly a million
   characters, which is generous even for a graph bootstrap script carrying
   hundreds of `MERGE` statements, while the harm measured against the unbounded
   read this replaces needed 256 MiB of input to reach 867 MB of resident memory
   and 15.9 seconds of wall time. A maximum that someone reaches while doing ordinary work is a
   maximum that gets widened later, and widening a published limit is worse than
   choosing it well once. A 64 KiB cap was considered and declined for exactly
   that reason.

### Bounded Standard-Input Read

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
for 256 MiB offered to a graph subcommand, the time going into the engine's parse
attempt over a 256 MB "query" that was never going to be accepted.

A producer still writing when the command exits observes the usual broken-pipe
result. The bound is a promise about what `rmp` consumes and retains, not about
what the producer manages to write: the operating system's pipe buffer holds
bytes the command never reads, so a writer may push somewhat more than 1048577
bytes before the pipe breaks.

**A read that fails is a failure of the stream, not of the statement.** The two
ordinary outcomes of a bounded read — a stream shorter than the bound, with or
without content — are not failures and are not reported as such. Anything else
is: the command stops, exits 1, and prints
`Error: I/O error: reading query from stdin: <detail>`, where `<detail>` is the
operating system's own text. `COMMANDS.md § Client Error Cases` publishes the
line, and [Error Handling and Exit Codes](#error-handling-and-exit-codes)
classifies it. The sentinel names the stream because the stream is what failed:
no store has been opened, no server has been contacted, and there is no statement
yet to correct. A directory redirected onto standard input is the plainest way to
reach it.

One difference from the comment body's bounded read is deliberate and must not be
"aligned" away. That read looks past its cap for trailing whitespace, so that the
verdict it reaches is exactly the verdict a read-to-EOF implementation would
reach after trimming. This read does not: the maximum counts the bytes standard
input supplies, so a stream of 1048576 bytes of Cypher followed by trailing
whitespace is refused even though trimming that whitespace would have brought it
to the maximum. The simpler rule is the right one here because a query's length
is not a value anybody reads back, and a producer that pads a megabyte of Cypher
with more whitespace is not a case worth reading further for.

### Standard Input That Supplies No Query

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
cheaper one to trigger: it needs no hostile input and consumes no memory. An
interactive terminal is not a source `graph client` ever expects a statement
from, because the two documented ways to supply one are the flag and a pipe or a
redirection.

The exit code is 2 and not the 6 that an over-long query carries, and the two
MUST NOT be collapsed into one class. Supplying no query at all is a missing
required parameter, which is exit code 2 across the CLI; supplying a query the
command refuses to accept is a validation failure, which is exit code 6. The
comment body reaches the same two verdicts for the same two conditions
(`COMMANDS.md § Comment Body Input Source and Precedence`, rule 3 for the missing
body and the bounded read for the over-long one).

## Schema Management

`rmp graph client` is how a knowledge graph's schema — its indexes and its
constraints — is managed. This section is canonical for that: which statements the
engine accepts, what each of them does, how a schema object is named, why changing
an index is two invocations rather than one, and how a schema failure reaches the
caller.

**The surface is the engine's own Cypher, not a Groadmap verb.** A schema
statement is written through `--query` or standard input exactly as every other
graph statement is (see
[Cypher Input Source and Precedence](#cypher-input-source-and-precedence)):

```bash
rmp graph client -r <roadmap> --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"
rmp graph client -r <roadmap> --query "SHOW INDEXES"
rmp graph client -r <roadmap> --query "DROP INDEX spec_key"
```

Groadmap adds no `index` subcommand, no `--create` / `--drop` flags, and no
vocabulary of its own. The consequence is deliberate and is the reason the choice
was made: the set of schema statements Groadmap supports is exactly the set the
pinned engine supports, so it widens or narrows with the engine rather than with
a Groadmap release, and Groadmap never has to decide what a schema statement
means. What Groadmap owns is what happens when the engine refuses one.

**Groadmap declares no schema object of its own.** No `rmp` command creates,
drops, or requires an index or a constraint as a side effect of anything else it
does, and no Groadmap code path emits schema DDL. Every schema object in a
knowledge graph is one its owner asked for, through this subcommand (see
[Constraints](#constraints), rule 1). A graph that has never been given one is
fully functional: indexes are an optimisation the caller may choose, and
constraints are an integrity rule the caller may choose.

### Accepted Schema Statements

| Statement | What it does | Success output |
|-----------|--------------|----------------|
| `CREATE INDEX [name] [IF NOT EXISTS] FOR (n:Label) ON (n.property) [OPTIONS {...}]` | Registers an index on one node property and back-fills it from the data already in the graph | `{"ok": true}` |
| `DROP INDEX <name> [IF EXISTS]` | Removes the index carrying that name | `{"ok": true}` |
| `CREATE CONSTRAINT [name] [IF NOT EXISTS] FOR (n:Label) REQUIRE n.property IS UNIQUE` (or `IS NOT NULL`) | Validates the data already in the graph and, only if it passes, registers the constraint | `{"ok": true}` |
| `DROP CONSTRAINT <name> [IF EXISTS]` | Removes the constraint carrying that name | `{"ok": true}` |
| `SHOW INDEX(ES)` and `SHOW CONSTRAINT(S)`, each with an optional `YIELD` / `WHERE` / `RETURN` projection tail | Lists the registered schema, altering nothing | `{columns, rows}` |

Rules:

1. **The accepted statement surface belongs to the engine.** The table above
   describes what the pinned engine accepts; it is not a grammar Groadmap
   defines, and Groadmap MUST NOT rewrite, complete, or normalise a schema
   statement before executing it. A statement outside the engine's grammar is
   refused by the engine (see
   [Schema Failure Classes](#schema-failure-classes)).
2. **An index and a constraint each cover exactly one node property.** The engine
   supports neither a composite (multi-property) form nor a form over a
   relationship property, and refuses both. A constraint is either a uniqueness
   rule or a presence rule; the engine supports no other kind.
3. **Index kinds are the engine's own vocabulary.** An index is a hash index by
   default, and a comparison-ordered index is requested through the statement's
   `OPTIONS` map. These are not the index kinds of any other Cypher
   implementation, and a statement written against another implementation's
   vocabulary is refused by the engine.
4. **A schema statement runs on the one execution path, and the engine runs it
   outside the transaction.** The server holds the store's exclusive lock, has the
   transactional store and the write-ahead-log writer already open, and runs the
   statement through the engine's transactional entry point, exactly as it does
   for every other statement (see
   [Engine Constructor by Path](#engine-constructor-by-path)). The engine itself
   recognises a schema statement there and executes it outside the transaction it
   would otherwise open, because a schema change is not transactional in this
   engine. Groadmap MUST NOT attempt to make one transactional, MUST NOT wrap it
   in a transaction of its own, and MUST NOT report it as one: a schema statement
   that succeeds has taken effect, and there is nothing to roll back it into.
5. **A successful schema-mutating statement owes a fold.** It appends to the
   write-ahead log, so the condition of
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write) is met and
   the server folds on its cadence or at shutdown. The snapshot that fold writes
   MUST carry the registered schema, for the reason that section gives: without it
   the truncation that follows destroys the definition the statement created.
6. **The output shape follows the columns the statement produces.** A
   schema-mutating statement produces none and returns `{"ok": true}`; a
   schema-introspection command produces the listing and returns the
   `{columns, rows}` shape (see `DATA_FORMATS.md § Graph Write Result`).
7. **Every other rule that binds a graph statement binds a schema statement.**
   Roadmap selection, the refusal of a positional argument, the maximum query
   length, and the bounded standard-input read all apply unchanged.

### Schema Object Names

1. **A declared name is used verbatim.** `CREATE INDEX spec_key FOR (n:Spec) ON
   (n.key)` registers the index under `spec_key`, with nothing appended and
   nothing folded.
2. **An omitted name is derived by the engine, and the derived name is not the
   one a reader would guess.** For an index the derived name is the lowercased
   label, the lowercased property, and the index kind, joined by underscores: an
   unnamed hash index on `Spec.title` is registered as `spec_title_hash`. For a
   constraint the same shape is used with the kind spelled as the rule it
   enforces. The derivation is the engine's, not Groadmap's.
3. **Removal is by name only.** `DROP INDEX` and `DROP CONSTRAINT` take a name,
   never a label-and-property pair, so a caller who did not declare a name must
   first learn the derived one.
4. **`SHOW INDEXES` and `SHOW CONSTRAINTS` are the authoritative report of what a
   schema object is called**, and are the way a caller learns a derived name. A
   schema listing is ordered deterministically, so two invocations against an
   unchanged graph produce the same rows in the same order.
5. **Declaring a name is the recommended practice, and Groadmap does not enforce
   it.** A named object is dropped by the name its author wrote; an unnamed one is
   dropped by a name the engine chose, which changes if the index kind changes.
   This is a recommendation, in the same sense as
   [Multi-Layer Modelling Conventions](#multi-layer-modelling-conventions): no
   `rmp` command requires a name or rejects a statement for omitting one.

### Altering and Recreating an Index

**The engine has no statement that changes an index in place.** There is no
`ALTER INDEX`, no `REBUILD INDEX`, and no `CREATE OR REPLACE INDEX`; each of the
three is refused by the parser as an unrecognised statement. Changing an existing
index — its kind, or its definition — and rebuilding one are therefore not single
statements, and Groadmap composes nothing on the caller's behalf. Both are the
caller issuing two statements in two invocations:

```bash
rmp graph client -r <roadmap> --query "DROP INDEX spec_ord"
rmp graph client -r <roadmap> --query "CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}"
```

Altering is a drop followed by a create with a different definition; recreating
is a drop followed by a create with the identical definition, and the rebuild is
the back-fill the create performs.

**The pair is not atomic, and the consequence MUST be stated to the caller rather
than discovered.** The two invocations are two processes, each taking and
releasing the store's exclusive lock, and nothing spans them. If the second fails
— a rejected definition, a lock it cannot take, a machine that stops between the
two — the index is **dropped and not recreated**, and the graph is left with no
index where it had one. Nothing in Groadmap detects that state, reports it, or
repairs it; the caller learns of it from `SHOW INDEXES`. Queries stay correct
throughout, because an index is an access path and never a source of results, so
what is lost is speed rather than answers.

**Groadmap MUST NOT offer an atomic alter, and MUST NOT simulate one.** The
engine runs a schema change outside any transaction, so a Groadmap-side wrapper
could not roll the drop back; it could only re-issue the create, which is not the
same guarantee and would report success for a repair that may itself fail. A
composed operation that is atomic in name only is worse than two statements the
caller can see.

**Both halves cost time proportional to the graph.** A create back-fills the
index from every node carrying the label, and a drop discards that work. On a
roadmap knowledge graph this is small, and the point of stating it is that it
does not stay small if the graph grows.

### Schema Failure Classes

| Failure | Refused by | Sentinel | Exit code |
|---------|-----------|----------|-----------|
| `CREATE INDEX` or `CREATE CONSTRAINT` whose object already exists, without `IF NOT EXISTS` | The engine | `utils.ErrGraphEngine` | 1 |
| `DROP INDEX` or `DROP CONSTRAINT` naming an object that does not exist, without `IF EXISTS` | The engine | `utils.ErrGraphEngine` | 1 |
| A definition the engine does not support — composite, over a relationship property, or a constraint kind it does not implement | The engine | `utils.ErrGraphEngine` | 1 |
| `CREATE CONSTRAINT` that the data already in the graph does not satisfy | The engine, having validated the data and registered nothing | `utils.ErrGraphEngine` | 1 |
| A schema statement whose keyword spacing the engine does not route to its schema parser | The engine's general Cypher grammar, as a parse error | `utils.ErrGraphEngine` | 1 |

Rules:

1. **Every schema failure is the engine's, and every one of them exits 1.**
   Groadmap refuses no schema statement of its own, so there is no second class to
   tell apart by exit code. An engine refusal carries the engine's diagnostic text
   after the wording Groadmap fixes (see
   [Error Handling and Exit Codes](#error-handling-and-exit-codes), rule 2). The
   engine's diagnostic text is not specified here, for the reason
   `COMMANDS.md § Graph Management` gives for every engine diagnostic: it belongs
   to the engine and changes with it.
2. **A duplicate create and a drop of an absent object are engine failures, not
   validation failures, and they exit 1 rather than 6.** They are stated
   explicitly because the exit code is the opposite of what a reader may expect:
   both look like input errors, and neither is one. Groadmap cannot know whether
   an object exists without opening the store, so the check belongs where the
   knowledge is. A caller that wants either to be a no-op writes `IF NOT EXISTS`
   or `IF EXISTS`, which the engine accepts and which makes the statement succeed
   silently.
3. **A `CREATE CONSTRAINT` refused by the existing data MUST NOT surface as an
   unexplained engine error.** The engine validates the graph's current data
   before registering a constraint and refuses the statement when the data does
   not satisfy it: a uniqueness rule over a property that already holds a repeated
   value, or a presence rule over a property some node already lacks. Nothing is
   registered and nothing is changed. Groadmap's obligation is to surface the
   engine's diagnostic intact, so that the caller learns which rule failed and on
   which property, rather than only that the command exited 1.
4. **A failed schema statement leaves the schema as it was.** No partial
   registration exists in any of the classes above; the object is either
   registered or it is not.
5. **A misleading diagnostic is a failure class of its own, and the caller meets
   it here.** The last row of the table is the spacing hazard of
   [What Groadmap Does Not Check](#what-groadmap-does-not-check), item 7, seen from
   the exit-code side: the message names `SHOW` or the clause keyword as
   unexpected and never names the separator, so a caller reading it has no route
   from the diagnostic to the cause. Nothing in Groadmap improves that message.

### Recovered Schema on Every Surface

**Every surface reports the schema the store actually holds.** `rmp graph client`
and the web graph data endpoint each answer a schema-introspection command from
the definitions the store open recovered, under the names the caller declared. The
two agree because they construct the same engine from the same recovery result,
which is what [Engine Constructor by Path](#engine-constructor-by-path) requires.

**An engine that is not given the recovered schema reports an empty one, and
reports it as the truth.** `SHOW INDEXES` answers with zero rows whatever the
store holds — not an error, and not a partial answer — and `DROP INDEX` fails as
though the index had never been created. That is why the constructor is given the
recovery result whole, and why a surface may not be moved to a constructor that
takes less.

**The web graph data endpoint reports a schema listing as no graph.** Its response
carries nodes and edges, and a schema-introspection command returns tabular rows
and neither a node nor an edge, so the walk over the result collects nothing and
the endpoint answers `{"nodes": [], "edges": []}` with HTTP `200`. That answer is
indistinguishable from a statement that genuinely matched nothing, and it is the
answer every statement that returns no graph element gets from this endpoint — a
`MATCH (n) RETURN count(n)` as much as a `SHOW INDEXES` (see
`WEB.md § Graph Data Endpoint`). A schema listing is obtained from
`rmp graph client`, which returns the rows.

## Query Notifications as Diagnostics

The Cypher engine may attach **advisory notifications** to a query result.
A notification is computed at parse and plan time and is available as soon as the
query has run; it is informational guidance, not an error. The classic example is
a Cartesian-product warning: a `MATCH` with two or more patterns that share no
variable forces the engine to combine every match of one pattern with every match
of the other, which can be expensive and is usually unintended.

Behaviour:

1. `rmp graph client` MUST, after the statement has run, surface on stderr
   exactly the notifications the engine returns for that statement, as a
   human-readable diagnostic line per notification. Groadmap does not generate
   notifications and does not decide which statements carry them; it only surfaces
   what the engine supplies, which may be none.
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
   stdout output is exactly the existing success output for that statement,
   unchanged, whichever of the published shapes it is: the `columns`/`rows` shape
   for a statement that produces columns, `{"ok": true}` for one that produces
   none, and the plan-bearing `columns`/`rows` shape for one written with an
   `EXPLAIN` or `PROFILE` prefix (see `DATA_FORMATS.md § Graph Query Result`,
   `DATA_FORMATS.md § Graph Write Result`, and
   [Query Plans: The EXPLAIN and PROFILE Prefixes](#query-plans-the-explain-and-profile-prefixes)).
   The exit code is unaffected and remains 0 on success.
5. A query that produces no notifications writes nothing extra to stderr.

This is consistent with the existing stderr-diagnostic pattern: notifications use
the same channel as the non-fatal checkpoint diagnostic (see functional
requirement 7 and
[Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)). One
invocation may therefore emit, on stderr, both any query notifications and, if a
post-commit checkpoint fails, the non-fatal checkpoint diagnostic; neither changes
the success stdout output or the exit code.

The exact GoGraph notification accessor, the notification type, and its field
names are implementation details for `go-developer`; this specification fixes the
behaviour, not the Go API.

The set of notifications that exist is determined entirely by the backing engine.
Groadmap's contract is to surface exactly what the engine returns for the
statement it executed. Whether a notification appears for a given statement
therefore follows the engine's behaviour, and this specification does not promise
that any particular statement will produce one.

## Query Plans: The EXPLAIN and PROFILE Prefixes

A Cypher statement may be written with an `EXPLAIN` or a `PROFILE` prefix. The
engine then captures the query plan it built and carries it beside the ordinary
result, and `rmp graph client` publishes it. A statement written with neither
prefix is unaffected in every respect, and the shape it produces does not change
by one byte.

The two prefixes answer two different questions, and the difference between them
is the whole reason both exist:

| Prefix | What it does | What it returns |
|--------|--------------|-----------------|
| `EXPLAIN` | Plans the statement and executes **nothing** | The statement's own column signature, zero rows, and the plan the engine would have run, carrying the planner's **estimates** |
| `PROFILE` | Executes the statement | The statement's real rows, and the same plan carrying what each operator actually **cost** |

### What each prefix does

1. **`EXPLAIN` executes nothing, and that is a safety property before it is a
   diagnostic one.** `EXPLAIN MATCH (n:Spec) DETACH DELETE n` deletes no node,
   writes no relationship, and appends nothing to the write-ahead log. A caller
   may reach for the prefix to inspect a statement it is unsure of, which is
   exactly the case in which executing the statement would be the worst possible
   answer.
2. **`PROFILE` executes the statement in full**, because a measurement of a run
   that did not happen is not a measurement. Its rows are the statement's own
   rows, unchanged and complete: a caller that prefixed a statement with
   `PROFILE` still asked for its answer.
3. **Both prefixes are recognised without regard to case.** `EXPLAIN` and
   `explain` select the same behaviour, and so does any other mixture of cases;
   the same holds for `PROFILE`. The prefix is part of the engine's grammar
   rather than a token `rmp` scans for, and `rmp` does not inspect the statement
   here any more than it does anywhere else (see
   [What Groadmap Does Not Check](#what-groadmap-does-not-check)).
4. **`PROFILE` refuses a statement that writes, and at the pinned engine the
   caller is not told why.** Measuring a write would mean performing it, and the
   engine refuses the statement rather than perform a write a caller asked only
   to have measured. The invocation fails with `utils.ErrGraphEngine` and exit
   code 1, through the same parse-and-execution failure class every other engine
   refusal uses, and no new exit code is introduced (see
   [Error Handling and Exit Codes](#error-handling-and-exit-codes)). What
   arrives in place of the engine's diagnostic is generic internal-error text
   naming only the session. Every statement runs inside `rmp graph serve` and
   every result crosses a Bolt connection, and the engine's Bolt server
   classifies this refusal as a **server** fault: its failure-code mapping
   carries no case for it — the refusal carries no sentinel and none of the
   categorised message forms the mapping recognises — so the mapping falls back
   to its generic database-error code, and the session then replaces the message
   of every failure so classified. The diagnostic itself is not destroyed. The
   engine writes it in full, under the same session, as a record of the kind
   [Server Diagnostics on Stderr](#server-diagnostics-on-stderr), rule 1,
   governs, and that record does name the remedy — use `EXPLAIN` for the
   statement's plan, or run the statement with no prefix to execute it. It is
   therefore readable by whoever can read the server's stderr, and unreadable by
   the caller who ran the statement.

   **This is the same substitution, for the same reason, that
   [Field Length Limits](#field-length-limits), rule 13, records for an
   over-long field, and it is above Groadmap's reach for the same reason.**
   There is no interception point to close it at: the engine's server exposes no
   error-mapping option, and Groadmap runs no statement of its own between the
   caller and the server. The one remaining lever would be matching the
   substituted text, which names no statement, no prefix and no remedy, so it
   would yield nothing worth publishing even before it broke silently at the
   next version bump. **The remedy belongs in the engine**: a case in its Bolt
   failure-code mapping that resolves this refusal to a client-error code, after
   which the engine's own message reaches the caller intact, exactly as a parse
   diagnostic already does (rule 6). **What is unaffected**: the sentinel is
   `utils.ErrGraphEngine` and the exit code is 1, the statement is still refused
   and still writes nothing, and no condition moves between sentinels. Only the
   message the caller reads is less informative than the refusal deserves.
5. **A writing statement's plan is a logical plan, and it is published as one.**
   A write's operators bind to an open transaction, so there is no physical
   operator tree to walk outside one, and opening one is precisely what `EXPLAIN`
   must not do. `EXPLAIN` therefore captures the logical plan for a writing
   statement and the physical plan for every other. The consequence is visible in
   the published JSON and is specified with the shape rather than hidden: a
   logical plan node names its operator as the plan line reads, with any detail
   already inside that name, and carries neither a separate detail nor an
   estimate (see `DATA_FORMATS.md § Graph Plan Node`).
6. **Neither prefix is accepted on a schema statement.** `CREATE INDEX`,
   `DROP INDEX`, `CREATE CONSTRAINT`, `DROP CONSTRAINT`, `SHOW INDEXES` and
   `SHOW CONSTRAINTS` are not statements the prefix grammar admits, and a
   prefixed one fails to parse. The invocation reports the engine's parse
   diagnostic with `utils.ErrGraphEngine` and exit code 1, exactly as any other
   statement the engine will not parse does (see
   [Schema Failure Classes](#schema-failure-classes)).

### What the client publishes

7. **The plan is published under one of two keys, and never under both.** An
   `EXPLAIN` publishes `plan`; a `PROFILE` publishes `profile`. The split is not
   a stylistic choice and must not be collapsed into one key with a mode flag
   beside it: it mirrors the engine's own two accessors and the two fields the
   Bolt protocol carries for the same purpose, and its effect is that **no reader
   can mistake an estimate for a measurement**. A figure under `plan` is what the
   planner predicted before anything ran. A figure under `profile` is what a run
   cost. A single key would have made the two indistinguishable at exactly the
   moment a reader is deciding whether to trust a number.
8. **A statement carrying either prefix always returns the columns-and-rows
   envelope, and never `{"ok": true}`.** This is the one point at which the
   prefixes depart from the discriminator every other statement obeys. That
   discriminator is whether the statement produces result columns (see
   `DATA_FORMATS.md § Graph Write Result`), and a writing statement that carries
   no `RETURN` clause produces none — so an `EXPLAIN` of one would otherwise
   publish `{"ok": true}`, which is
   the exact object a real, committed write publishes. A caller reading it would
   be told that a statement succeeded in doing something, when the statement did
   nothing at all and the plan it was asked for had been discarded. A prefixed
   statement that declares no result columns therefore publishes an empty
   `columns` array, an empty `rows` array, and its plan.

   **The departure costs nothing in compatibility, and the reason is recorded
   here so that it is not reasoned through again.** The output of a prefixed
   statement has no earlier contract to break: this specification is the only
   place a shape for one is fixed, and the prefixes are admitted by the engine
   the project pins rather than by anything a caller could have relied on
   before. What a consumer may have built against is the output of an
   *unprefixed* statement, and rule 9 leaves that untouched. So the discriminator
   is departed from exactly where no consumer exists, and honoured everywhere one
   might.
9. **A statement carrying neither prefix keeps its current output byte for
   byte.** Neither key appears, and no other member is added, removed, or
   reordered. An existing consumer of `rmp graph client` parses exactly what it
   parsed before and requires no change. This is the
   guarantee rule 8's departure is bounded by, and it is not a courtesy: it is
   what makes the departure safe.
10. **The published plan is the engine's own plan, rendered rather than
    re-derived.** The fidelity that `DATA_FORMATS.md § Graph Client Result`
    requires is not weakened by the plan and is not re-established for it by
    inspection: the plan a client receives over the protocol is mapped back onto
    the engine's own plan representation, exactly as a value crossing the
    protocol is mapped back onto the engine's value model, and one serialisation
    then produces the JSON. Every key the mapping decides therefore carries what
    the engine reported under it, and nothing is recomputed from the rendered
    text.

    The exception is a `profile` tree's `timeNs`, and it is an exception no
    implementation could remove. That key measures the execution that produced
    it, so two runs of one statement measure two durations and both figures are
    correct. An `EXPLAIN` publishes no `timeNs` at all and is identical in every
    byte across two runs, as is any statement carrying neither prefix.
    `DATA_FORMATS.md § Graph Client Result`, rule 5, is canonical for the
    boundary and for what a caller may rely on across it.

    Nothing about the protocol the plan crossed is observable in the published
    JSON. A caller parses the shape this section fixes and learns nothing from it
    about the transport, which is the same opacity the published values are
    under.

### The four honesty rules a reader must not get wrong

The plan's numbers are published under rules that exist because the alternative
in each case is a figure a reader would act on and should not. Each rule is
stated here because it is a property of the diagnostic, not of the encoding;
`DATA_FORMATS.md § Graph Plan Node` fixes which key each rule governs.

11. **A figure nobody counted is omitted, never published as zero.** The engine
    distinguishes an operator whose storage accesses were counted and came to
    zero from an operator whose accesses nobody counted at all, and the published
    JSON keeps that distinction by omitting the key in the second case. A zero
    would be a measurement claim, and the second case has no measurement to
    claim. The engine's own text rendering prints `?` for the same state, for the
    same reason.
12. **A rejection count of zero is published, and its absence means something
    else.** An operator that has a rejection mechanism and rejected nothing
    reports zero, deliberately: a filter that rejected nothing is precisely the
    finding a reader of a slow plan is looking for. Only an operator with no
    rejection mechanism at all omits the key. The asymmetry against rule 11 is
    intended — there, an absent key admits that a figure exists and was not
    counted; here, an absent key states that there is no figure to have.
13. **An operator's time is inclusive of its children.** The figure is the
    wall-clock time attributed to the operator including everything it drew from
    the operators beneath it. A reader who sums the times of a plan's nodes
    double-counts every level of the tree; the root's figure is the one that
    describes the statement. It is published as a whole number of nanoseconds
    rather than as a millisecond value, so that what is published is the integer
    the engine already holds, and the fidelity rule 10 states holds by
    construction rather than by a formatter's conversion (see
    `DATA_FORMATS.md § Graph Plan Node`, rule 12).
14. **The storage-access count is a count of access-path record reads and never
    of property reads.** This is a deliberate divergence from Neo4j, which
    additionally charges one hit per property read, and it is published because a
    caller comparing the two products otherwise concludes that the figure is
    wrong. It is not: it counts a different thing, and it counts that thing
    exactly.

### What is unchanged by a prefix

15. **Notifications are surfaced for a prefixed statement exactly as for any
    other**, and this is one of the things `EXPLAIN` is for: the
    Cartesian-product warning for a disconnected multi-pattern `MATCH` reaches
    stderr without the statement ever running. Notifications remain advisory and
    change neither the stdout output nor the exit code (see
    [Query Notifications as Diagnostics](#query-notifications-as-diagnostics)).
16. **The statement time budget applies unchanged.** A `PROFILE` executes, so it
    can exhaust the budget and be cut like any other statement; an `EXPLAIN`
    plans only, and the budget bounds the planning (see
    [Statement Time Budget](#statement-time-budget)).
17. **An `EXPLAIN` changes nothing on disk beyond what any statement that writes
    nothing changes.** It appends nothing to the write-ahead log, so no
    checkpoint runs and no snapshot is written, which is the rule functional
    requirement 6 already states for every such statement (see
    [What a Statement That Writes Nothing Changes on Disk](#what-a-statement-that-writes-nothing-changes-on-disk)).
18. **The prefix introduces no flag, no subcommand, no sentinel error, and no
    exit code.** It is part of the statement text a caller already supplies
    through `--query` or standard input, and it reaches the engine through the
    path every statement reaches it through.

## Write Counters: What a Statement Changed

A statement that changed the graph reports what it changed. The engine keeps,
for each statement it executes, a set of counters recording the write effects
that were actually applied — nodes and relationships created and deleted,
properties written, labels added and removed, indexes and constraints added and
dropped — and `rmp graph client` publishes them beside the statement's result.
`DATA_FORMATS.md § Graph Query Counters` is canonical for the
published shape, the key set and the omission rules; this section fixes the
behaviour.

**The reason the member exists is that `{"ok": true}` answers a different
question.** It says a statement succeeded, which is worth knowing and is not the
thing a caller writing to a graph most needs to know. Two invocations that both
print it may have created a node and matched an existing one, or deleted a
thousand relationships and deleted none. The counters separate those cases
without the caller having to issue a second, reading statement to find out —
which is the only way it could have found out before, and which reads a graph
that other writers may have changed in the meantime.

Behaviour:

1. **The counters are read after the result has been fully drained and before
   the transaction is committed.** They are accumulated by the write path as the
   statement's operators run, so they are final only once every operator has been
   driven to exhaustion; and the commit is what releases the result, so a read
   after it is a read of something that no longer exists. This is the same window
   in which the query plan and the advisory notifications are taken, and for the
   same two reasons (see
   [Query Notifications as Diagnostics](#query-notifications-as-diagnostics) and
   [Query Plans: The EXPLAIN and PROFILE Prefixes](#query-plans-the-explain-and-profile-prefixes)).
2. **Both surfaces publish them, and publish the same object.** `rmp graph
   execute` reads them from the engine it opened, or from the server it resolved;
   `rmp graph client` reads them from the server it was pointed at. The identity
   `DATA_FORMATS.md § Graph Client Result` requires binds them with no exception:
   they describe the statement and the graph, not the duration of the run, which
   is the one thing that section exempts.
3. **A statement that failed or was rolled back publishes no counters, because
   it publishes nothing at all.** A failing invocation writes nothing to stdout
   (see `DATA_FORMATS.md § Fundamental Principle`), so the question of what its
   counters said does not arise. This holds for every failure class alike: a
   statement the engine refused, one the time budget cut, one whose commit
   failed, and one that lost every attempt of the retry policy against a server.
   The counters live with the statement's own write path, so an abandoned
   statement's counts are discarded rather than carried into the next one.
4. **A statement that changed nothing publishes no counters either, and its
   output is unchanged in every byte.** This covers every read, every `EXPLAIN`
   — which executes nothing — and every write whose effects came to nothing,
   such as a `MERGE` that matched an existing element or a `DELETE` whose
   pattern matched no row. It is what makes the member additive rather than a
   change to the published contract: a consumer that never issues a writing
   statement never sees a byte it did not see before.
5. **The counters count effects, not observability, and this specification
   claims nothing about the second.** A counter is incremented at the point the
   write path applies the change. It is not a statement that the change can
   afterwards be read back, and it must not be read as one:
   [What Groadmap Does Not Check](#what-groadmap-does-not-check) enumerates the
   ways in which a statement that the engine accepted and executed can leave the
   graph other than as its author intended, and publishing a count does not
   remove any of them. What the counters add is a signal where there was none;
   what they do not add is a guarantee that was never there.

### The one figure the protocol carries folded

6. **`propertiesWritten` is one member covering two engine counters, on both
   paths, and the constraint that decides it is the protocol's.** The engine
   counts a property assignment and a property removal separately, following
   openCypher, which names `+properties` and `-properties` as two distinct side
   effects. The Bolt protocol that carries a served result does not: its
   statistics vocabulary has a single properties counter and no counterpart for
   a removal. A result that reached a caller through `rmp graph client` therefore
   arrives with the two already summed, and no second channel exists anywhere in
   the protocol from which the split could be recovered.
7. **The published key is therefore named for the sum, and the fold is not a
   choice this specification can avoid.** Every result crosses the protocol, so
   the split is not available to any caller. Three alternatives were considered
   while a second path still existed, and each failed a requirement this feature
   is under; they are recorded because the reasoning still bears on the name:
   - Publishing the split wherever it happened to be available and the sum
     elsewhere would make one statement publish different key sets at different
     times, which is precisely what `DATA_FORMATS.md § Graph Client Result`
     forbids.
   - Publishing the sum under a key named for an assignment — a
     `propertiesSet` carrying a removal — asserts an effect that did not occur.
     This specification already refuses that trade for a plan's figures, where a
     count nobody took is omitted rather than published as a zero
     ([Query Plans: The EXPLAIN and PROFILE Prefixes](#query-plans-the-explain-and-profile-prefixes),
     rule 11), and the principle does not weaken because the number here happens
     to be convenient.
   - Omitting the property counter entirely leaves a statement whose only effect
     was on properties — every `SET` and every `REMOVE` that touches no label —
     with a change to report and no member to report it in. Its block would be
     present, because the statement changed something, and empty, which
     `DATA_FORMATS.md § Graph Query Counters`, rule 2, does not allow.
8. **What the fold costs, stated plainly, and what it does not.** A caller
   cannot tell, from `propertiesWritten` alone, whether a statement assigned two
   properties, removed two, or did one of each. That is a real loss and it is
   published rather than hidden. What it does not cost is the member's purpose:
   distinguishing a `MERGE` that created from one that matched, a `DELETE` that
   removed nothing from one that removed a thousand, and a schema statement that
   registered an index from one that found it already present, all rest on the
   node, relationship and schema counters, and every one of those crosses the
   protocol faithfully. A caller that must know which of the two property
   effects occurred knows it from the statement it wrote.
9. **The remedy is upstream and is not available here.** Restoring the split
   would require the protocol's statistics map to carry a properties-removed
   figure alongside the one it has — an extension to the wire vocabulary, not a
   change to how Groadmap reads it. Until such a figure exists on the wire, the
   fold is the only arrangement that keeps every requirement in this section and
   in `DATA_FORMATS.md § Graph Client Result` true at the same time: every
   statement crosses the protocol, so there is no route by which the split could
   reach a caller. Should the figure appear, `propertiesWritten` may be replaced
   by the two separate members; nothing in this specification is written so as to
   make that harder than it needs to be.
10. **The counters and a query plan never appear together, and neither surface
    has to arrange it.** An `EXPLAIN` executes nothing, so it has no applied
    effect to count. A `PROFILE` executes, but the engine refuses a `PROFILE` of
    a writing statement rather than perform a write it was asked only to measure
    (see
    [Query Plans: The EXPLAIN and PROFILE Prefixes](#query-plans-the-explain-and-profile-prefixes),
    rules 1 and 4), so the only statement a `profile` tree can describe is one
    that changed nothing. Both exclusions are the engine's, and this
    specification records them rather than imposing them.

## Field Length Limits

A statement writes labels, property keys and property values into two durable
formats, and each format bounds how long a field it will carry. The bounds are
the engine's. This specification does not set them, does not raise them and does
not lower them; what it fixes is what the caller is told when a field exceeds
one, and what Groadmap does next.

**The two formats do not bound a field alike, and that is why this section has
two halves rather than one rule applied twice.** A committed write goes into the
write-ahead log; a checkpoint later folds the committed state into a snapshot
(see [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)). The
log bounds a field by the capacity of the length prefix its frame reserves, which
is an encoding limit. The snapshot bounds one by what its own reader is required
to accept, which is an anti-exhaustion control — a reader that allocated for any
length a prefix could express would allocate gigabytes on an untrusted file. The
two are set independently and neither is uniformly the stricter: for a label and
for a property key the log's bound is by far the tighter and fires first, at
commit, so the snapshot's can never be reached; for a property value and for a
node key the snapshot's is the tighter by a factor of four, so a field can be
short enough to commit and too long to fold. Such a field is durable, correct,
recoverable and unfoldable at the same time. Rules 6 to 9 exist for exactly that
field, and they are not a wording variant of rules 2 to 5.

**The figures are read from the pinned GoGraph version and are recorded here as
evidence, not as the rule.** They move with the engine, a version bump may move
any of them, and no line Groadmap publishes repeats one from this page: every
published line carries the figure the engine reported for the run that produced
it (see [Dependency Maturity Risk](#dependency-maturity-risk)).

| Field | Write-ahead log, at commit | Snapshot, at checkpoint | Which bound binds |
|-------|----------------------------|-------------------------|-------------------|
| Node or edge label | 65535 bytes | 1 MiB | The log, by a factor of sixteen |
| Node or edge property key | 65535 bytes | 1 MiB | The log, by a factor of sixteen |
| Index or constraint identifier | 65535 bytes | 64 KiB | Neither: the engine's Cypher parser bounds it far below both |
| Property value, list element, list element count | 4294967295 bytes | 1 GiB | **The snapshot**, at a quarter of the log's bound |
| Node key | 4294967295 bytes, enforced by the node-key codec | 1 GiB | **The snapshot**, at a quarter of the log's bound |
| Labels or properties on one edge-handle record | — | 1 Mi | The snapshot, at a ceiling no graph the engine produces approaches |

**The binding bound for a property value is 1 GiB, and a caller who reads only
the log's figure is misled.** A 2 GiB value is inside the log's bound, so it
commits, and it is acknowledged, durable and recoverable. It is outside the
snapshot's, so from that moment every checkpoint of that graph fails, the
write-ahead log is never folded again and never reclaimed, and it grows for as
long as the value remains. The last column of the table above is therefore the
column that matters when writing, and 1 GiB is the number to write under.

Behaviour:

1. **Groadmap checks no field length, and MUST NOT.** It cannot: a statement's
   fields are the values its expressions produce, so learning them means
   executing the statement, which is what the engine does. A pre-check would have
   to reimplement the engine's evaluator to guess at a bound the engine owns, and
   it would be wrong in the direction that matters — refusing writes the engine
   accepts. This is the same reasoning that puts the write-ahead log, and not the
   statement's text, in charge of whether a checkpoint runs
   ([Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)). Every
   rule below describes what Groadmap does with a refusal the engine has already
   made.
2. **A commit the engine refuses for an over-long field publishes a line of its
   own.** It carries `utils.ErrGraphEngine` and exit code 1, as an ordinary parse
   or execution failure does, and it is nonetheless not that failure's line. The
   two conditions were otherwise separated only by the engine's diagnostic tail,
   which [Error Handling and Exit Codes](#error-handling-and-exit-codes), rule 2,
   deliberately declines to specify and which a caller therefore cannot lawfully
   match: a caller reading `graph query failed: ` had to parse English to learn
   whether to correct the statement's syntax or to shorten one of its values.
   This is the same defect, and the same remedy, as the statement time budget and
   the exhausted serialisation retry, which each hold a line of their own for the
   same reason (rules 6 and 7 of that section).
3. **What a caller may rely on to recognise the class is `rmp`'s own text, and
   inside the binary the engine's sentinel.** Externally, the fixed prefix of the
   published line is the whole of the contract: `COMMANDS.md § Graph Management`
   publishes the exact line. Internally, the condition is recognised with
   `errors.Is` against `store/txn.ErrFieldTooLong`, which the engine wraps around
   every write-ahead-log length refusal. It MUST NOT be recognised by matching
   the engine's message text: that text is the engine's to reword, a match on it
   fails silently at the next version bump, and the whole point of a sentinel is
   that it survives the wording.
4. **The published line ends in the engine's diagnostic, and the resulting echo
   is deliberate.** The engine formats every such refusal with the field kind and
   both figures — the length the field occupies and the maximum in force — and
   that is the most useful part of the line, because it names which of the
   statement's fields is at fault and by how much. It follows `rmp`'s own text
   unchanged, untrimmed and **last**, which is where every other published line
   carrying an engine or operating-system diagnostic puts it, and which is what
   lets a test assert the whole of `rmp`'s half and none of the engine's. The two
   halves therefore both say the field is too long, and that repetition MUST NOT
   be tidied away by editing, trimming or re-deriving the engine's half: doing so
   would put `rmp` back to parsing a diagnostic that rule 3 forbids it to match.
   `rmp`'s half accordingly names no field kind of its own — it defers to the
   engine's, which is the only one that knows.
5. **Nothing is written, the store stays usable, and the exit code is
   unchanged.** The refused transaction consumes a sequence number and applies
   nothing, so the graph holds no part of the statement — not the elements it
   created before the over-long field, and not the properties it set on them.
   Measured, an ordinary write submitted immediately after such a refusal
   returned `{"ok": true}` and a following `MATCH` counted it. The refusal adds
   no exit code and moves no condition between sentinels
   ([Constraints](#constraints), rule 5).
6. **A checkpoint the engine refuses for an over-long field is a condition of its
   own, and what separates it from every other checkpoint failure is that it
   cannot heal.** A checkpoint that fails because a disk is full, a permission is
   wrong or a write is interrupted may succeed the next time it runs, and both
   the existing rule and the existing diagnostic are built on that expectation:
   the write succeeded, the log is intact, and the next successful checkpoint
   reconciles the snapshot
   ([Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write), failure
   policy;
   [Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process),
   rule 7). Here that expectation is false. The field the snapshot refuses is
   committed graph state; every later capture captures the same state, so every
   later checkpoint refuses for the same reason. **The condition is permanent
   until the offending field is removed or shortened.** Nothing in the
   environment changes it, no amount of waiting resolves it, and a diagnostic
   that told the operator the next checkpoint would reconcile the snapshot would
   be telling them to wait for something that will not happen. That is why this
   is a condition of its own and not the general one worded better.
7. **What the failure costs is bounded growth lost, not data.** The engine's
   guard fires while the capture is still being assembled, before any snapshot
   file is written and before the write-ahead log's prefix is truncated. Every
   acknowledged commit therefore remains durable in the log, recovery still
   restores it in full, and the store stays open and usable. What is lost is the
   truncation: the log keeps growing for as long as the offending field is in the
   graph, and every open replays more of it, so recovery time grows with it. That
   is a real and unbounded cost, and it is the reason the condition must be
   reported rather than absorbed — but it is not a durability failure, and a
   diagnostic that read as one would be worse than none.
8. **Every surface that holds the checkpoint error MUST classify it; the one that
   does not hold it MUST NOT pretend to.** The synchronous checkpoint of a
   short-lived invocation and the graph server's shutdown checkpoint both return
   an error to Groadmap, so both MUST recognise
   `store/snapshot.ErrFieldTooLong` with `errors.Is` and report this condition
   rather than the general one. The graph server's in-flight checkpoint does not:
   it runs on the engine's own cadence loop
   ([Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process),
   rules 5 and 9), and what Groadmap can observe of it is a statistics value
   carrying the last failure as a **rendered string**, not as an error. On that
   path `errors.Is` has nothing to match, and matching the string is what rule 3
   forbids, so the in-flight report stays the general one — with the engine's own
   text inside it, which is where an operator reads the kind. This is a limit of
   what the engine exposes; it is stated as one rather than closed by a text
   match, and it is the boundary an implementation MUST observe rather than work
   around.
9. **The report says what is safe, what did not happen, that it will not happen
   again, and what to remove.** Wherever rule 8 requires the classification, the
   diagnostic MUST carry four things: that every acknowledged commit is still
   durable and recovery still restores it; that the log was not folded, and that
   the log therefore grows and the next open replays more of it; that the
   condition will persist through every later checkpoint while the field remains;
   and that the remedy is to shorten or remove the offending field with a
   statement. The last two are what make this report different from every other
   checkpoint diagnostic, all of which describe a condition the operator waits
   out or repairs in the environment. They are also what makes the report worth
   emitting more than once: on the short-lived surfaces the diagnostic
   accompanies **every subsequent write**, because every subsequent write
   checkpoints and every checkpoint refuses, and a line that recurred on every
   write saying only that a checkpoint had failed would train an operator to
   ignore the one message that names an unbounded, permanent cost. **No literal
   for these diagnostics is published in this specification or in `COMMANDS.md`.**
   They accompany a successful invocation — exit code 0 for the short-lived
   surfaces
   ([Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write), failure
   policy, rule 2) and a log record for the server
   ([Server Diagnostics on Stderr](#server-diagnostics-on-stderr)) — so neither
   is an error line and neither belongs in the error tables that
   `COMMANDS.md § Published Error Strings Are Exact` governs. What is fixed is
   the content above, not the wording.
10. **Two neighbouring refusals are outside this class, and an implementation
    MUST NOT fold them in.** A node key longer than the write-ahead log's
    unsigned 32-bit prefix is refused by the engine's node-key codec, which does
    not wrap `store/txn.ErrFieldTooLong`; it reaches the caller through the
    ordinary parse-or-execution line of
    [Error Handling and Exit Codes](#error-handling-and-exit-codes), rule 2. An
    assembled write-ahead-log frame over the engine's frame ceiling — which a
    single list property of many individually-legal elements can reach without
    any one of them being over-long — is refused by the log's framer under a
    sentinel of its own, and reaches the caller the same way. Both are genuine
    length refusals in spirit; neither is this class, because the class is
    defined by the sentinel a caller matches and not by the shape of the
    complaint.
11. **A schema identifier does not reach the write-ahead log's bound.** The
    engine's Cypher parser bounds an index or constraint name, label and property
    far below 65535 bytes at the point the statement is parsed, so a schema
    statement meets that bound first and is refused there, with its own message
    and as an ordinary engine refusal (see
    [Schema Failure Classes](#schema-failure-classes)). The field kinds a Cypher
    statement can drive into rule 2's refusal are therefore a node or edge label
    and a node or edge property key — the two a caller writes in a pattern.
12. **What is checkable end to end, and what is not.** The 65535-byte bound on a
    label and on a property key is reachable from a statement that fits inside
    the maximum query length, so rule 2's refusal, rule 5's intact store and the
    published line are all drivable against the compiled binary, and
    [Acceptance Criteria](#acceptance-criteria) 68 and 69 drive them. The bounds
    that govern a property value are not, in either format: a literal of a
    gigabyte does not fit inside the maximum query length, and a regression test
    cannot materialise a field of that size at all — one that tried would measure
    the machine rather than the product. Rules 6 to 9 are therefore not drivable
    end to end either, since the only way to a refused checkpoint is a field of
    that size. Their coverage lives in two places instead, and this specification
    says where rather than implying an end-to-end check exists. The engine's own
    suite covers each guard at the boundary of its own constant, which is where
    that coverage belongs, because the constants are the engine's. Groadmap's
    coverage is of the classification alone, driven with a fabricated error that
    wraps the engine's sentinel: what Groadmap owns on this path is the decision
    of which line or which diagnostic to publish, and that decision is testable
    without a field of any particular size. Criterion 70 fixes that division and
    forbids a criterion that attempts the field itself.
13. **The published line is not reachable at the pinned engine, and this rule
    states that plainly rather than implying otherwise.** Every statement now runs
    inside `rmp graph serve` and every result crosses a Bolt connection, so there
    is no path on which a caller reads this refusal as itself. The engine's Bolt
    server classifies the refusal as a **server** fault, because its failure-code
    mapping carries no case for `store/txn.ErrFieldTooLong` and falls back to its
    generic database-error code; the session then replaces the message of every
    failure so classified with generic internal-error text naming only the
    session. So neither the sentinel, nor a code that separates this condition
    from any other, nor the field kind crosses the connection, and what a caller
    reads is the ordinary parse-or-execution line of
    [Error Handling and Exit Codes](#error-handling-and-exit-codes), rule 2,
    carrying that generic text where the engine's diagnostic would be. That is
    precisely the defect rule 2 of this section exists to remove, and the
    withdrawal of the direct path has left it standing everywhere rather than on
    one path of two. The diagnostic itself is not destroyed: the engine logs it in
    full, under the same session, as a record of the kind
    [Server Diagnostics on Stderr](#server-diagnostics-on-stderr), rule 1,
    governs — so it is readable by whoever can read the server's stderr, and
    unreadable by the caller who ran the statement.

    **The line stays published, and it is published as not yet reachable.** It is
    specified in `COMMANDS.md § Client Error Cases`, and its scenario says that it
    is the line a caller reads once the engine distinguishes the condition — not
    the line a caller reads today. Keeping it costs nothing and removing it would
    cost the contract: the class is real, the remedy is known and is small, and a
    line withdrawn now would have to be re-specified, re-agreed and re-tested the
    moment the engine gains the case. What MUST NOT happen is a specification that
    publishes the line without saying it is unreachable, because a caller told to
    match it would wait for something that never arrives.

    **What is unaffected.** The sentinel is `utils.ErrGraphEngine` and the exit
    code is 1; nothing is written and the store stays usable (rule 5); no
    condition moves between sentinels and no exit code is added. Only the message
    the caller reads differs from the message this section specifies.

    **Groadmap MUST NOT close this on its own side.** There is no interception
    point to close it at: the engine's server exposes no error-mapping option, and
    Groadmap runs no statement of its own between the caller and the server. The
    one remaining lever would be matching the sanitised text, which rule 3 forbids
    and which would yield nothing worth publishing in any case — that text names
    no field, no kind and no figure. **The remedy belongs in the engine**: a case
    for `store/txn.ErrFieldTooLong` in its Bolt failure-code mapping, resolving to
    a client-error code, exactly as the per-transaction operation cap is already
    mapped there and its message reaches the client intact. Once a code
    distinguishes the condition, the client reaches the published line through the
    code-matching it already uses lawfully for the conflict and the budget
    classes, this rule's exception ends, and the line becomes reachable at both
    surfaces at once.

    **No identity is broken by this, which is the one thing the withdrawal
    improved here.** While two paths existed, one statement produced one of two
    different stderr lines according to whether a server happened to be running —
    which the caller did not choose and could not see — and that departure had to
    be recorded against the identity
    [Functional Requirements](#functional-requirements), rule 16, and
    `DATA_FORMATS.md § Graph Client Result` fix. With one path there is one line,
    the same one for every caller of every surface, so the identity holds without
    qualification and the departure is retired. What remains is not an
    inconsistency but a deficiency: the line every caller reads is less
    informative than the line this section requires.

## Error Handling and Exit Codes

Graph subcommands use the exit-code mapping defined in
`ARCHITECTURE.md § Error Handling` and `ARCHITECTURE.md § Exit Codes`. They
introduce no exit code of their own.

They do carry three sentinels of their own, `utils.ErrGraphEngine`,
`utils.ErrGraphStore` and `utils.ErrGraphServer`, all three mapping to exit
code 1. `ARCHITECTURE.md § Sentinel Error Catalogue` is canonical for what each
one means and for why the three are kept apart; the table below assigns every
condition a graph subcommand can reach to a sentinel, and every graph failure
among them to one of those three. No graph failure carries `utils.ErrDatabase`:
the only database a roadmap has is its `project.db`, which no graph operation
reads or writes ([Constraints](#constraints), rule 2).

One row of the table is not a graph failure at all. A read of standard input that
fails carries `utils.ErrIO`, because what failed is the stream the statement was
to arrive on and not the graph: neither the store nor a server has been touched at
that point, and the statement does not yet exist. It is listed here because
`graph client` can reach it, and a table that claims to assign every condition a
graph subcommand can reach would be incomplete without it.

| Condition | Sentinel | Exit code |
|-----------|----------|-----------|
| No roadmap selected and none provided via `-r` | `utils.ErrNoRoadmap` | 3 |
| Selected roadmap does not exist | `utils.ErrNotFound` | 4 |
| No query supplied: `--query` absent and standard input empty, whitespace only, or a terminal; or `--query` present with an empty, whitespace-only, or absent value (see [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)) | `utils.ErrRequired` | 2 |
| `graph client` receives a positional argument, a bare Cypher query included; it accepts none (see [No Positional Query: A Stray Token Is Refused](#no-positional-query-a-stray-token-is-refused)) | `utils.ErrInvalidInput` | 2 |
| Query longer than the maximum query length of 1 MiB, from either source (see [Maximum Query Length](#maximum-query-length)) | `utils.ErrValidation` | 6 |
| The query was to come from standard input and the read of the stream itself failed (see [Bounded Standard-Input Read](#bounded-standard-input-read)) | `utils.ErrIO` | 1 |
| Cypher fails to parse or execute in the engine, a schema statement included (see [Schema Failure Classes](#schema-failure-classes)) | `utils.ErrGraphEngine` | 1 |
| A label or property key the statement writes is longer than the write-ahead log's length prefix allows, and the engine refuses the commit (see [Field Length Limits](#field-length-limits)) | `utils.ErrGraphEngine` | 1 |
| The statement exhausts the statement time budget and is cancelled (see [Statement Time Budget](#statement-time-budget)) | `utils.ErrGraphEngine` | 1 |
| Every attempt of the client's retry policy loses a serialisation conflict against a graph server (see [Concurrency Inside the Server](#concurrency-inside-the-server), rule 9) | `utils.ErrGraphEngine` | 1 |
| `graph serve` cannot open, recover, read, or write the graph store, or cannot create its directory (I/O or corruption) | `utils.ErrGraphStore` | 1 |
| `graph serve` finds the graph store's exclusive lock still held when the bounded wait is exhausted, so another server is likely already running for the roadmap (see [Lock Contention](#lock-contention), rule 3) | `utils.ErrGraphStore` | 1 |
| The roadmap's socket answers but no server can be reached through it, or the connection fails for a reason other than the socket being absent or refusing (see [Server Resolution](#server-resolution)) | `utils.ErrGraphServer` | 1 |
| The connection to a server is lost after the statement has been sent (see [Server Resolution](#server-resolution), rule 4) | `utils.ErrGraphServer` | 1 |
| A server does not answer within the caller's backstop deadline (see [Server Resolution](#server-resolution), rule 7) | `utils.ErrGraphServer` | 1 |
| `graph client` finds no server listening for the selected roadmap (see [The Bolt Client](#the-bolt-client)) | `utils.ErrGraphServer` | 1 |
| A resolved socket path is longer than the platform's bound, whether derived from the roadmap or supplied through `--socket` (see [Socket Path Length](#socket-path-length)) | `utils.ErrGraphServer` | 1 |
| `graph serve` cannot bind its socket, or a live server already answers on the resolved socket (see [Server Startup](#server-startup)) | `utils.ErrGraphServer` | 1 |
| Successful execution, and a server stopped by `SIGINT` or `SIGTERM` after a graceful shutdown | — | 0 |

Rules:

1. **The maximum query length is the only condition on which Groadmap refuses a
   statement's content, and it is the only cause of exit code 6 in this file.**
   Exit code 6 remains the CLI's validation class and is reached from other
   commands for their own reasons (see `ARCHITECTURE.md § Exit Codes`); within the
   graph feature the over-long query is its single cause. The three refusals that
   precede the engine are all decided before the graph store is opened: the
   stray-positional refusal (exit code 2), the missing-query refusal (exit
   code 2) and the maximum-length refusal (exit code 6), all three stated in
   [Cypher Input Source and Precedence](#cypher-input-source-and-precedence). A
   statement refused by any of the three never reaches the engine and changes
   nothing. The stray-positional refusal is settled while the arguments are still
   being read, so it precedes the maximum-length refusal always; against the
   missing-query refusal it does not, because a `--query` whose value is absent
   is settled in the same left-to-right pass and the earlier token wins (see
   [No Positional Query: A Stray Token Is Refused](#no-positional-query-a-stray-token-is-refused),
   rule 4).
2. A Cypher parse or execution failure reported by the engine is wrapped as
   `utils.ErrGraphEngine` (exit code 1). The sentinel names the engine because
   the engine is what refused the statement, and because the action it calls for
   is to correct the statement rather than to touch anything else. The message
   carries a fixed prefix and then the engine's diagnostic text;
   `COMMANDS.md § Graph Management` publishes the exact line, and the engine's
   half of it is not specified there or here.
3. Errors are written as plain text to stderr and carry the standard AI-agent
   hint (see `HELP.md § Error message format`).
4. The graph feature introduces no new exit codes, and MUST NOT. Its three
   sentinels all map to exit code 1, the code the graph subsystem returned for
   every one of these conditions before the sentinels existed, so a consumer that
   branches on an exit code sees no change. A further sentinel, if one is ever
   needed, MUST be added following the procedure in
   `ARCHITECTURE.md § Adding New Error Types`, and MUST map to a code the
   catalogue already publishes. The dedicated graph server and its client add no
   code either: every failure either of them can produce is carried by a sentinel
   this table names, and
   `ARCHITECTURE.md § Exit Codes of the Graph Server and Client` enumerates the
   codes each subcommand can return.
5. **A statement that the engine executes, and that does the wrong thing quietly,
   carries exit code 0 and no diagnostic.** Every hazard listed in
   [What Groadmap Does Not Check](#what-groadmap-does-not-check) reaches the
   caller as a success, because that is what the engine reports. No exit code in
   the table above distinguishes them, and none is added to: an exit code that
   claimed to would require the inspection this specification does not perform.
6. **A statement the time budget cuts fails in the same class and publishes a
   line of its own.** It carries `utils.ErrGraphEngine` and exit code 1, as rule
   2's engine failures do: the statement had reached the engine and was running
   there when the budget cut it, and the action the caller must take is on the
   statement. It introduces no new exit code (see
   [Constraints](#constraints), rule 5). Its message is not
   rule 2's message: the whole of it is `rmp`'s own text, it names the budget that
   was exceeded, it states that nothing was written, and it says what to do about
   it. `COMMANDS.md § Graph Management` publishes the exact line, and
   [Statement Time Budget](#statement-time-budget) states the behaviour behind it.
7. **An exhausted serialisation retry fails in the same class and publishes a
   line of its own too.** It carries `utils.ErrGraphEngine` and exit code 1, as
   rule 2's engine failures and rule 6's budget exhaustion do, and for the same
   reason: the statement reached the engine and the engine's transaction manager
   refused every attempt of it. It carries the engine's sentinel even though the
   statement is valid, because what failed is this execution of it and the action
   the caller must take is still on the statement — run it again. It introduces
   no new exit code. Its message is neither rule 2's nor rule 6's. The whole of it is `rmp`'s own text;
   it names the contention rather than the statement, it states that nothing was
   written, and it names the remedy — run the statement again, and spread
   concurrent writes across distinct nodes. Every statement is now executed by a
   server, so this condition is reachable for any statement rather than for a
   subset of them; it was unreachable while a caller could run its own single
   transaction in its own process (see
   [Concurrency and Recovery](#concurrency-and-recovery)).
   `COMMANDS.md § Graph Management` publishes the exact line, and
   [Concurrency Inside the Server](#concurrency-inside-the-server) states the
   behaviour behind it.

8. **An over-long field fails in the same class and publishes a line of its own
   too, and it is the last of the four that do.** It carries
   `utils.ErrGraphEngine` and exit code 1, as rule 2's engine failures, rule 6's
   budget exhaustion and rule 7's exhausted retry do: the statement reached the
   engine, and the engine refused to make it durable. It introduces no new exit
   code. Its message is none of the other three's. Unlike rule 6's and rule 7's,
   it is **not** wholly `rmp`'s own text: `rmp` writes the class, the statement
   that nothing was written and the action to take, and then ends the line with
   the engine's diagnostic, because that diagnostic names which field is at fault
   and by how much and no text `rmp` could write would know that. What `rmp`'s
   half supplies is the class — the thing rule 2's line could not distinguish. `COMMANDS.md § Graph Management` publishes the exact line, and
   [Field Length Limits](#field-length-limits) states the behaviour behind it,
   including the second half of the condition that reaches the caller as a
   diagnostic on a successful invocation rather than as an error at all, and
   including the fact that at the pinned engine no caller reads this line: the
   engine's Bolt server replaces its message, so the condition arrives through
   rule 2's line instead ([Field Length Limits](#field-length-limits), rule 13).

## The Dedicated Graph Server

`rmp graph serve` turns a roadmap's knowledge graph into a service. It opens that
roadmap's store once, holds it for the life of the process, and answers Cypher
statements over a Unix domain socket until it is told to stop. `rmp graph client`
is its counterpart: a client that sends one statement to a running server and
prints what comes back. The command-line contract for both — every flag, its
default, and its failure — is `COMMANDS.md § Graph Management`. This section is
canonical for what the server is, what it guarantees, and what it does not.

**The protocol is Bolt version 5, and the server is the engine's own.** Groadmap
defines no protocol. It builds the same engine every other surface builds (see
[Engine Constructor by Path](#engine-constructor-by-path)), hands that engine to
GoGraph's Bolt server, and gives the server a listener. Sessions, explicit
transactions, statement timeouts, transaction quotas, and result streaming are the
engine's rather than Groadmap's, and this specification fixes only the values
Groadmap chooses and the behaviour Groadmap adds around them.

**The transport is a Unix domain socket and nothing else.** The server binds no
network port, on loopback or anywhere else, and no flag exists to make it. Access
control is therefore the filesystem's, and it is the whole of the access control
there is (see [Socket Path and Permissions](#socket-path-and-permissions)).

**The server is the only process that opens the store, and that is what the lock
now means.** A server holds the store's exclusive advisory lock for its whole
process lifetime, which is a hold no finite wait can be sized against. Nothing else
takes that lock: `rmp graph client` and the web graph data endpoint reach the graph
by resolving the roadmap's socket and sending the statement to the server, and
neither has a second way in. With no server listening, each reports that the graph
is unavailable rather than opening the store. That rule is stated once, in
[Server Resolution](#server-resolution), and both surfaces follow it rather than
restating it. The lock therefore has one remaining role, and it is not a queue: it
admits one server per roadmap (see [Lock Contention](#lock-contention)).

**A roadmap's graph comes into being when a server first opens it.** `rmp graph
serve` creates the roadmap's graph directory when there is none, so starting a
server is both the first step and the only step in bringing a graph into existence
(see [Server Startup](#server-startup), step 1, and
[Persistence Layout](#persistence-layout), rule 2). An operator's first use of the
graph for a roadmap is therefore `rmp graph serve -r <name>`, followed by
`rmp graph client -r <name> --query "<cypher>"` against the server it started.

### Socket Path and Permissions

The socket for roadmap `<name>` is `~/.roadmaps/<name>/graph.sock`. It sits in the
roadmap's home directory, beside `project.db` and the `graph/` store directory,
and not inside `graph/`: the contents of that directory belong to GoGraph, and
`write.lock` is the single entry in it Groadmap owns (see
[Persistence Layout](#persistence-layout), rule 5).

1. **The default path is derived from the roadmap and from nothing else.** Every
   surface that resolves a socket derives it the same way, so a caller and a
   server that name the same roadmap name the same socket without either being
   told a path.
2. **The `--socket` flag overrides that derivation on the two subcommands that
   publish it**, `graph serve` and `graph client`. The web graph
   data endpoint publishes no such flag and has nowhere to receive one, so it
   resolves the derived path and only the derived path. What follows from that
   asymmetry is stated in
   [Serving on a Non-Default Socket](#serving-on-a-non-default-socket).

   **The flag names a socket; it does not name a path through the product.** It
   changes which socket an invocation looks at, and nothing about what happens
   once it has looked: a server answering there takes the statement, and a path
   that is absent or refuses sends the caller to the store under the exclusive
   lock. There is no flag that demands a server, and none that forbids one (see
   [Server Resolution](#server-resolution)).
3. **The socket carries mode `0600`, set explicitly.** It MUST NOT be left at
   whatever the process umask happens to yield. Connecting to a Unix domain
   socket requires **write** permission on the socket file, so a permissive umask
   leaves the socket connectable by the user's group, or by every account on the
   machine, and connecting to it is reaching the graph. Setting the mode
   explicitly removes the dependency on the umask altogether, and it is set before
   the server answers its first connection.
4. **The filesystem is the access control, and the roadmap home is the outer
   fence.** The roadmap home directory is `0700` (see
   `ARCHITECTURE.md § Directory Structure`), so a socket at the default path is
   already unreachable by another user whatever its own mode says. Rule 3 is the
   inner fence, and it is the one that still holds when `--socket` puts the socket
   somewhere else.
5. **The server authenticates nobody, and says so rather than omitting it.** The
   Bolt authentication handler admits every connection. It is set explicitly,
   because the engine refuses to construct a server with no handler at all, so
   "no authentication" here is a declaration and never an oversight. A caller that
   can open the socket can read, write, delete, and change the schema of that
   roadmap's graph. This is the trust model the web graph data endpoint already
   has (see `WEB.md § Security and Constraints`), reached through a second door.
   The declaration is announced as well as written down here: the engine emits a
   warning for it at construction, which rule 6 covers together with the warning
   for the absent transport security.
6. **The connection is not encrypted, and the engine warns about both of these.**
   No transport security is configured, because the transport is a socket in the
   local filesystem and there is no network hop to protect. The engine emits a
   warning at construction for the absent transport security and a second for the
   permissive authentication handler. Both are expected, both are correct, and
   neither is a failure. Both are structured `log/slog` records rather than
   plain-text lines, and both reach stderr **before** the socket is announced on
   stdout, so a caller that waits for the announcement and then reads stderr finds
   them there. [Server Diagnostics on Stderr](#server-diagnostics-on-stderr) is
   canonical for the form those records take and for that ordering.
7. **The socket file belongs to the server and does not outlive it.** A server
   that stops removes it. A socket file left behind by a process that was killed
   is a **stale** socket: nothing is listening on it, the next server replaces it,
   and every resolver reads it as evidence that the roadmap is not served (see
   [Server Resolution](#server-resolution)).

### Socket Path Length

A Unix domain socket is named by a path in the filesystem, and the operating
system bounds how long that path may be. The bound belongs to the platform and
not to Groadmap: the kernel copies the path into the fixed-size `sun_path` field
of its socket address structure, and a path that does not fit there — terminator
included — can be neither bound nor connected to. A path over the bound therefore
names a socket that no process can create and no caller can reach, and it stays
that way for as long as the path stays what it is. Left unchecked, it surfaces as
the kernel's own `invalid argument`, which names neither the length, nor the
limit, nor anything the reader can act on.

**The bound is derived from the platform, never declared here.** It is the
capacity of the `sun_path` field, less one byte for the terminator that must have
somewhere to go. That expression is what this specification fixes; the number it
yields is not, because the number is not the same everywhere. Across the
operating systems the project's targets span (`BUILD.md § Primary Platforms`) the
same expression yields **107 bytes on Linux and Windows** and **103 bytes on
macOS, FreeBSD and OpenBSD** — two figures over nine targets, from one
expression. An implementation that hard-codes 107 is correct on two of the five
operating systems and silently wrong on the other three, where it would accept
four paths the platform cannot bind and hand the caller back the very errno this
section exists to replace. The figures above are what the derivation produces;
they are evidence for it, and they are not a substitute for it.

Behaviour:

1. **The check runs on the resolved path, not on what the caller typed.** It is
   applied after `~/.roadmaps/<name>/graph.sock` has been derived from the
   roadmap, and after a `--socket` value has been expanded to the absolute path
   the invocation will actually use. The kernel measures the resolved string, so
   a check that measured anything earlier would be measuring something else. The
   derived default path is therefore checked exactly as a supplied one is: the
   bound is a property of the path, not of how the path was chosen.
2. **The length is counted in bytes.** `sun_path` holds bytes, and a roadmap name
   may carry multi-byte UTF-8, so a path of a hundred characters can be well over
   the bound. A check that counted runes would pass paths the kernel refuses, and
   would do so only for callers whose roadmap names are not ASCII — the worst way
   for a bound to be wrong, because it would look correct everywhere it was
   tested.
3. **The check runs before the socket is used.** `rmp graph serve` performs it
   while it resolves the path, before it takes the store lock, probes the path,
   removes a stale file, or binds anything (see
   [Server Startup](#server-startup), step 1). A caller performs it before it
   probes. The purpose is to report the cause instead of the errno, and a check
   placed after the attempt would be too late to replace anything.
4. **The published line names the actual length, the derived limit, and the
   remedy.** It reports the resolved path once, the number of bytes that path
   occupies, and the number of bytes this platform allows, and it names
   `--socket` as the way to put the socket somewhere shorter. Both numbers are
   values the binary interpolates: the limit is the derived figure for the
   platform the binary is running on, so the same published line is correct on
   all nine targets. `COMMANDS.md § Graph Server Socket Error Lines` publishes
   the exact line.
5. **An over-long resolved path fails the invocation, on every surface, however
   the path was chosen.** The two subcommands that publish `--socket` —
   `graph serve` and `graph client` — each exit 1 with the line above, and the web
   graph data endpoint refuses the request with HTTP `500`; each of them does so
   both for a path the caller supplied and for the derived default path. The
   refusal is decided before the probe rather than by it, which is the one respect
   in which this condition departs from the ordinary resolution sequence.

   **On the web surface the two are reported apart, and the status is what
   separates them.** An absent socket is evidence that no server is listening *at
   this moment*: a missing dependency the operator controls, which starting a
   server clears, and the endpoint answers `503` for it. A path over the bound is
   evidence that no server can **ever** listen there: it is a permanent defect in
   the roadmap's layout, no delay and no operator action short of moving the
   roadmap alleviates it, and announcing a temporarily unavailable service would
   be false. The endpoint therefore answers `500` for it, and
   `WEB.md § Knowledge Graph from the GoGraph Store` is canonical for both
   statuses. The bound is a property of the path, and rule 1 has already fixed
   that a derived path and a supplied one are measured on the same rule.
6. **The rule is uniform because every surface genuinely needs the socket.** A
   roadmap whose derived socket path cannot be bound has a real and permanent
   defect in its layout: the path is what it is, no server can be started for that
   roadmap, and nothing the caller
   does at the moment of the call changes it. Every surface that reaches a graph
   now reaches it through a server on that socket, so a path no socket can occupy
   is fatal to every one of them on its own terms. The refusal reports that fact
   at whichever surface the operator meets first, at the first point of contact,
   where the published line already names the remedy.

   **What the bound still costs is stated rather than softened.** A home directory
   deep enough to push the derived path over the bound makes that roadmap's graph
   unreachable, because no server can be started for it. That is an ordinary
   installation and not a hypothetical: rule 8 records the bound being reached in
   practice on a three-character roadmap name. On the command line the refusal is
   recoverable without moving the roadmap, and the published line's remedy is
   truthful for both subcommands: `--socket` naming a path inside the bound is
   checked and passes, so a server can be started there and a client can be pointed
   at it. The web graph data endpoint has no such flag and no way to receive one,
   so for that surface the only remedy is a shorter derived path: a shorter home
   directory, or a shorter roadmap name. That asymmetry between the command line
   and the web interface is the same boundary, drawn for the same reason, that
   [Serving on a Non-Default Socket](#serving-on-a-non-default-socket) already
   states.
7. **The failure class and the exit code are unchanged; only the message
   differs.** The refusal carries `utils.ErrGraphServer` and exit code 1 — the
   same sentinel and the same code the unqualified bind failure already carried.
   This section adds no exit code, moves no condition between sentinels, and
   changes no classification (see
   [Error Handling and Exit Codes](#error-handling-and-exit-codes)). What it
   changes is what the reader is told: a length, a limit and a remedy, in place of
   a diagnostic that named the path twice and the cause not at all.
8. **The derived path reaches the bound without an unusual roadmap name.** The
   default path is the home directory, 22 fixed bytes for `/.roadmaps/` and
   `/graph.sock`, and the roadmap name. A roadmap name may be 50 bytes, so a home
   directory of 36 bytes puts the derived path one byte over the 107-byte figure
   Linux and Windows yield, and one of 32 bytes puts it over the 103-byte figure
   macOS, FreeBSD and OpenBSD yield. It has been reached in practice on a three-character roadmap name under
   a deep home directory, at 139 bytes — 32 over the limit in force there. The
   bound is a live constraint on ordinary installations, and not a limit only a
   deliberately long `--socket` can find.

### Server Startup

`rmp graph serve` performs this sequence in this order. The order is load-bearing:
each step is what makes a later one safe.

1. **Resolve the roadmap and the socket path, check the path's length, and then
   create the roadmap's graph directory if it has none.** A roadmap that does not
   exist fails here, before anything is opened, created, or removed. So does a
   resolved socket path longer than the platform's bound, whether it was derived
   from the roadmap or supplied through `--socket` (see
   [Socket Path Length](#socket-path-length)). Both refusals precede the lock, the
   probe, the unlink and the bind, so **a server that cannot start touches
   nothing**: the directory is created only once both refusals have been passed,
   which is why it is the last action of this step and not the first.

   **This is where a roadmap's graph comes into being, and `rmp graph serve` is the
   only thing that creates one.** The directory is `~/.roadmaps/<name>/graph/`,
   created with mode `0700` (see [Persistence Layout](#persistence-layout),
   rule 2). It has to be created here rather than at the store open of step 6,
   because the advisory lock file step 2 takes lives inside it: a server that left
   the creation to the open would report a missing graph as a lock file it could
   not open, which names the wrong thing entirely. A server started against a
   roadmap that has never had a graph therefore serves an empty one, and the first
   statement a client sends creates the first node in it.
2. **Take the graph store's exclusive advisory lock under the bounded wait**
   [Lock Contention](#lock-contention) specifies. The wait exists for one holder:
   another `rmp graph serve` for the same roadmap that is shutting down and has not
   yet released it. A server that waits rather than failing on the first collision
   is what lets a restart succeed without a pause between the two processes. When
   that wait is exhausted the server does not start, which is what admits one
   server per roadmap.
3. **Refuse to start when a live server already answers on the resolved socket.**
   The server probes the path exactly as a resolver does, under the bounded probe
   [Server Resolution](#server-resolution) fixes. A live answer means another
   server owns that socket: `rmp graph serve` fails and MUST leave the socket file
   exactly as it found it. A path that does not exist, and a path that refuses the
   connection, each carry no live server.

   **Step 2 is what makes this check sufficient, and it is why the order is this
   way round.** A server answering on *this* roadmap's default socket holds *this*
   roadmap's lock, so it would already have stopped step 2 from completing; the
   probe is therefore not the interlock for the ordinary case, and a second
   `rmp graph serve` against the same roadmap is refused by the lock without the
   incumbent's socket being touched. What the probe catches is the case the lock
   cannot: a `--socket` path some other roadmap's server owns. The two interlocks
   are different, and neither is relied on to do the other's work.
4. **Replace a stale socket file.** Once step 3 has established that nothing
   answers there, any file at the path is removed. This is what lets a relaunch
   after a kill succeed instead of failing on a name that is already taken.
5. **Bind the listener and set the socket's mode to `0600`.**
6. **Open the store and construct the engine**, through the one lifecycle
   `internal/graphstore` owns, so the server is on the same single path as the
   other two surfaces (see
   [Engine Constructor by Path](#engine-constructor-by-path)). An engine
   constructed over a graph with no write-ahead log behind it accepts every write,
   acknowledges every commit, warns about nothing, and loses all of it when the
   process ends; the constructor that table fixes is the one that does not.
7. **Take `SIGINT` and `SIGTERM` over, flush the startup diagnostics to stderr,
   announce the socket on stdout, and serve — in that order.** The announcement is
   a single JSON object naming the socket the server bound, so a caller that
   supplied no `--socket` still learns the path (see
   `COMMANDS.md § Serve Output`). The flush is what puts the two warnings of rules
   5 and 6 of [Socket Path and Permissions](#socket-path-and-permissions) on
   stderr **before** that announcement, so a caller that reads stdout first and
   stderr second finds them there rather than finding nothing (see
   [Server Diagnostics on Stderr](#server-diagnostics-on-stderr)). The order inside
   this step is load-bearing in the same way as the order of the steps around it,
   and the paragraph below states why the take-over is first of the three.

**The listener is bound before the store is opened, deliberately.** Opening the
store costs up to about a second on a large graph, and a caller that resolved the
roadmap during that second would find no socket, conclude that no server is
listening, and fail — against a server that was, at that moment, starting for it.
Binding first spends that second with the socket already accepting: a caller that
arrives during it connects, waits for the handshake, and is served. When the store
open fails instead, the server closes the listener and exits, and the waiting
caller sees the connection dropped and fails as
[Server Resolution](#server-resolution) requires.

**One window remains, and it is stated rather than hidden.** Between step 2 and
step 5 the lock is held and no socket answers. A caller that resolves inside it is
told that no server is listening, which is true of that instant and false a
moment later. The window is a probe, an unlink, and a bind — microseconds, not the
store open — and the failure is immediate, deterministic, and cleared by retrying.
It is the reason the window is kept as narrow as it is rather than being allowed
to span the store open.

**A second, narrower case belongs to the interval the bind covers.** A caller that
connects between step 5 and step 7 waits for the handshake while the store opens,
and its probe deadline is 2500 ms (see
[Server Resolution](#server-resolution)). The store open is measured at 955 ms on
a 36 MB graph and 1784 ms on a 122 MB one, so the deadline covers the graphs
measured; on one large enough that it does not, the caller's probe expires and the
roadmap resolves as **Unreachable**. The two failures are worth telling apart, and
the resolution rule does tell them apart: a caller in the first window is told that
nothing is listening, and one in this window is told that something answered and
could not be reached, which is the more accurate report of a server that is still
opening its store.

**The signal take-over precedes the announcement, and that is what the
announcement means.** Until step 7 runs, `SIGINT` and `SIGTERM` carry the meaning
they carry for every short-lived `rmp` invocation: the process is interrupted and
exits `130` (see `ARCHITECTURE.md § Exit Codes`). From step 7 they carry the drain
of [Server Shutdown and the Drain](#server-shutdown-and-the-drain). Ordering the
change of meaning ahead of the announcement is what makes the announced socket a
promise rather than a path: a caller that has read it is talking to a process that
drains. Announcing first would publish a server that could still be stopped the
wrong way, for as long as the take-over took.

**The take-over is a change of owner, not a re-registration, and the discipline is
enforced.** One package owns the disposition of these two signals for the whole
binary, registers for them once at the start of the process, and never
unregisters; a surface that wants the drain replaces the action taken on delivery
rather than registering for the signal itself. There is therefore no instant in
which a delivery is unowned — an instant in which a signal would kill the process
outright, or be delivered to nothing at all, instead of being drained or reported.
`internal/testenv` enforces both halves: that no production file outside that one
package handles signals, and that in each long-lived surface the take-over
precedes the announcement. `rmp web` is on the same discipline (see
`WEB.md § Server Lifecycle`), and
`ARCHITECTURE.md § Modules and Responsibilities` is canonical for the package.

### Server Shutdown and the Drain

`rmp graph serve` stops on `SIGINT` or `SIGTERM`, and it stops gracefully. The
guarantee begins at the announcement.

**Where the guarantee begins, stated rather than left to be discovered.** The
server takes the two signals over at step 7 of
[Server Startup](#server-startup), immediately before it announces its socket. A
signal that arrives earlier — during the lock acquisition, the live-server probe,
the stale-socket removal, the bind, or the store open of steps 2 to 6 — reaches an
invocation that has served nothing, acknowledged nothing, and owes no drain. It is
an interruption and it is treated as one: the process exits `130` (see
`ARCHITECTURE.md § Exit Codes`) with no drain, no shutdown checkpoint, and no
socket removal — and so, if the listener had already been bound, it leaves behind
a socket file, which the next `rmp graph serve` finds dead and removes at step 4.
That interval is a few milliseconds on a small graph, and it is dominated by the
store open, which costs up to about a second on a large one
([Server Startup](#server-startup)). A supervisor sizing a grace period is sizing
it against the drain, and the drain begins at the announcement; a supervisor that
stops a server by process identifier without waiting for the announcement must
expect `130` rather than a graceful stop.

**Taking the signals over earlier is deliberately not specified.** Arming the
drain before the store is opened would hold an early signal until the accept loop
was reached to receive it, so the process would appear to ignore `SIGTERM` for the
whole of the store open — up to about a second, and longer on a graph larger than
any measured. Trading a prompt, correct `130` for an unbounded silence is a
different specified behaviour, not a correction of this one.

**The drain is Groadmap's, because the engine has none.** The engine's own
shutdown cuts sessions rather than draining them: measured, it returned
immediately against an idle authenticated session and left that session's client
with a broken connection. Groadmap therefore drains before it calls that shutdown
at all.

The sequence:

1. Stop accepting new connections.
2. Wait, under a bounded timeout, for the statements and explicit transactions
   already in flight to reach a quiescent point.
3. Cut what the drain could not finish, and shut the Bolt server down. The
   engine's shutdown cancels every connection's context, which is the whole of
   the cut it can perform; a session parked in a socket write does not observe a
   cancellation, so Groadmap closes that socket itself.
4. Checkpoint and truncate the write-ahead log, if the log has grown since it was
   last folded (see
   [Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process),
   rule 4).
5. Close the store and release the exclusive advisory lock.
6. Ensure the socket file is gone.
7. Exit 0.

**The queued diagnostics outlive every step of that sequence, deliberately.** The
records this teardown writes — a store that failed to close, a shutdown
checkpoint that failed — are the last account an operator has of what happened,
and the sink that keeps a blocked stderr off the serving path is a queue, so
those records could otherwise still be waiting in it when the process returned.
They are delivered after every step above and before the process exits, under a
bound of their own (see
[Server Diagnostics on Stderr](#server-diagnostics-on-stderr)).

**The drain's bound is the graph store's wait budget** — the statement budget plus
the backoff total, 7.5 seconds at the values in force (see
[Lock Contention](#lock-contention)) — reused rather than replaced by a figure of
its own, so the project keeps one set of timing numbers. It is the right quantity
because it is the one a waiter is already required to survive: the longest lawful
hold of a statement that is a read or that runs to completion.

**Step 3 reaches a session that no cancellation reaches, and it is a cut rather
than a bound.** The engine holds its serve call behind every session goroutine
and cannot tell one it is still working for from one that is stopped, so a peer
that has stopped reading fills the socket buffer, parks a goroutine in a socket
write, and holds the whole shutdown for as long as that write takes to fail.
Groadmap separates the two cases by observation rather than by a deadline: a
connection that had one socket write outstanding when the drain began and has
that same write outstanding when the drain ends has been blocked for the whole
drain, which is longer than the longest lawful statement, and its socket is
closed. A session inside an engine call is not in a socket write at all, so it is
never selected and is still waited for without limit. Measured against a peer
that had stopped reading, the shutdown returned at 60.0 seconds without the cut
and at 7.5 seconds with it, while the same cut against a server inside an undo
replay moved nothing — 32.9 seconds against 34.3, inside the spread of the replay
itself.

**The cut releases a goroutine and abandons nothing, which is what makes it safe
rather than merely fast.** Every step after it still runs, in the same order and
through the same code: the engine's own drain completes, so steps 4 and 5 take
the shutdown checkpoint and close the store on the ordinary path. That is why
step 3 closes a socket instead of putting a deadline on the wait. A deadline
cannot see why the wait is long, so the only deadline prompt enough to release a
blocked peer would also abandon a session inside an undo replay — and the engine
tears the durability stack down only from a fully drained exit, so abandoning
that session leaves the store to be closed while a statement is still unwinding.
Nothing acknowledged would be lost either way, but that is a race this shutdown
does not have and a deadline is the only way to introduce it.

**The shutdown says when it has cut, because the engine's account of the same
event reads as something else.** A connection closed at step 3 produces a write
error from the engine's end, and on its own that line reads as a network fault a
client caused rather than as a choice the server made, which is the difference
between an operator investigating and an operator moving on. The shutdown
therefore writes its own record at `WARN`, carrying the number of connections it
closed under a `connections` attribute — one record however many were closed, and
no record at all when nothing was cut. Its absence therefore says that nothing
was cut, and not that the shutdown was clean: a shutdown held open by an undo
replay selects no connection and writes no record either. Its presence is the
informative half, and what it means is that the shutdown closed something. It is
a `log/slog` record like every other the server writes, through the same handler
and the same sink, and
[Server Diagnostics on Stderr](#server-diagnostics-on-stderr) governs it
unchanged. It reports a count and not an identity because there is no identity to
report: a connection over a Unix domain socket carries no client address, and the
engine's own records of these same connections carry an empty one.

**What the drain guarantees:**

1. **Every acknowledged commit is durable.** This is not the drain's doing and the
   specification does not claim it is: the commit protocol makes a transaction's
   log frames durable before the acknowledgement is written, so the guarantee
   holds against an unexpected kill exactly as it holds against a signal (see
   [Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process)).
   The drain adds nothing to it.
2. **A statement in flight when the signal arrives either completes and is
   answered, or is cut whole.** A cut statement's transaction is rolled back
   entirely: it leaves no partial write and no torn state on disk, exactly as a
   statement the time budget cuts does (see
   [Statement Time Budget](#statement-time-budget)).
3. **A statement that completes during the drain is answered before the server
   stops.** That is the whole of what the drain buys over the engine's own
   shutdown, and it is worth buying: without it a client that had just committed
   would be told nothing about a change that is already on disk.

**What the drain does not guarantee, stated so that it is not read as more:**

1. **It does not guarantee completion.** The bound is finite, and past it the
   remaining sessions are cut. A cut session's client sees a broken connection
   rather than a typed failure, and cannot distinguish that from a crash. It does
   not have to: the store is consistent either way.
2. **It does not tell a client whose connection was cut between the commit and its
   acknowledgement whether the statement committed.** That window exists in every
   protocol that acknowledges a durable commit over a connection, and nothing here
   closes it. A caller that must know re-reads the graph, which is why a statement
   whose effect the caller has to confirm is written with a `RETURN` clause or
   followed by a read.
3. **It does not bound the shutdown.** A statement the deadline cut while it was
   writing is inside an undo replay the engine takes no cancellation for, and the
   store cannot close until that call has returned. Shutdown therefore lasts as
   long as that replay lasts whatever the drain's bound says, and the longest such
   hold measured is 35.6 seconds, with no ceiling established (see
   [Statement Time Budget](#statement-time-budget)). That replay is the only cause
   of it left with no ceiling. A peer that stopped reading before the signal was a
   second cause until step 3 began closing its socket, which took the same
   shutdown from 60.0 seconds to 7.5. What remains of that cause is bounded rather
   than removed, and the criterion that decides it is whether the peer's write is
   already parked when the mark is taken. A write that parks **after** the mark is
   not selected, because its counter has moved — the false-positive discipline
   working rather than failing — and a session parked in a socket write never
   reaches the row loop where the shutdown's cancellation would be observed. It is
   released when the per-message write deadline expires, so the connection timeout
   is its ceiling: 60 seconds at the values in force, measured end to end at
   60.035 seconds (see [Server Options](#server-options)). Both a peer that stops
   reading during the drain and one that reads just enough that each individual
   write completes and the next one parks arrive at that case. A peer that never
   parks does not: every write completes, so the row loop keeps turning, observes
   the cancellation at the next row, and ends the stream after one more record's
   write.

### Server Options

The engine's server takes a set of options. Groadmap fixes the ones below and
leaves every other at the engine's own default. A value the engine owns is not
restated here: restating it would give this specification a fact a dependency bump
can falsify in silence, which is the hazard
[Dependency Maturity Risk](#dependency-maturity-risk) describes.

One of the options Groadmap fixes is the logger the engine's server reports
through, and it is specified in
[Server Diagnostics on Stderr](#server-diagnostics-on-stderr) rather than in the
list below, because what it settles is the shape of published output rather than
a bound on a session.

**The statement bound is the graph store's, and it is the declaration the other
two surfaces already read.** The server's default statement timeout is the
statement budget of [Statement Time Budget](#statement-time-budget), and its
maximum statement timeout is that same value, so a client cannot raise its own
statement timeout above the bound this specification fixes. One declaration
governs the server and both the surfaces that reach it, and changing it changes
all of them together.

**Capping the maximum has a consequence on explicit transactions, and it is stated
rather than left to be discovered.** The engine clamps an explicit transaction's
total life by that same maximum. A `BEGIN` to `COMMIT` sequence therefore has the
same 5 seconds in total that a single statement has, however many statements it
carries. That is the price of having one bound rather than two that can disagree;
a caller with more work than fits splits it across transactions.

**The connection timeout MUST sit well above the statement bound, and Groadmap
MUST set it rather than inherit it.** The engine documents that timeout as the
silent gap between messages, but it reaches both directions of the socket and it
reaches them while a statement is running. On the read side it is armed as a read
deadline that stays in force while the message loop is busy executing the previous
statement. A statement that runs longer than it destroys its own connection
mid-flight, whatever the statement's own budget says. Measured, the cut tracks the
connection timeout exactly and ignores a statement timeout four times its size.
That mechanism is why the value is fixed here and not left to the engine. The
engine's own default for this option is neither restated here nor relied on: it is
a value the engine owns, and the rule at the head of this section governs it. The
bound below is Groadmap's own, and it holds whatever that default is.

Groadmap therefore sets the connection timeout to **twelve times the statement
budget**, which is 60 seconds at the budget in force. The multiple is derived
rather than picked. A statement the deadline cuts while it is writing holds the
engine call open for the budget multiplied by a factor the statement itself sets,
measured from 1.005x to 7.13x, and the longest such hold measured at the budget in
force is 35.6 seconds (see [Statement Time Budget](#statement-time-budget)). Sixty
seconds clears that by 1.7x, and it clears the longest statement the server
actually permits, 5 seconds, by twelve. Because the value is a multiple of the
budget rather than a constant of its own, it moves with the budget and the two
cannot drift apart.

**The residual, because no multiple removes it.** Nothing measured establishes a
ceiling on that factor, so no finite connection timeout guarantees that a cut
write is answered rather than disconnected. A client whose write is cut may lose
its connection instead of receiving a typed failure. It is the same unbounded
quantity [Lock Contention](#lock-contention) once recorded of a statement holding
the store lock, arriving now at the connection instead.

**Idleness is bounded by the same value.** The connection timeout is also what
bounds a session that sends nothing, so a client that holds a session open without
using it loses it after 60 seconds and must reconnect. `rmp graph client` sends
one statement per invocation and never meets this; a longer-lived client is
expected to reconnect.

**A write is bounded by the same value, and that is the arming a shutdown
inherits.** The connection timeout is armed afresh before every response the
server writes, every record of a streamed result included, so a client that asks
for a large result and then stops reading it holds one write open until the
deadline trips and loses its connection 60 seconds later. Until that socket is
closed the write holds the session and the session holds the shutdown, because
the engine's serve call waits for every session goroutine: measured end to end, a
server whose peer had stopped reading took 60.035 seconds to stop, and the whole
of that was this deadline.
[Server Shutdown and the Drain](#server-shutdown-and-the-drain), step 3, closes
that socket sooner, and sets out which sessions it reaches. Because the deadline
is armed afresh before each response rather than once per session, a peer that
goes on consuming — so that every write completes and none parks — never trips it
at all, and needs nothing to trip it: a session whose writes complete reaches the
next row of the result it is streaming, and that is where a shutdown's
cancellation is observed and the stream ends. Only a session parked in a write
inherits this value as its bound.

**The inbound message bound MUST NOT sit below the maximum query length.** A
statement the engine accepts is a statement the server must accept, so
the server's limits on an inbound message and on a decoded payload MUST leave room
for a statement of the maximum query length (see
[Maximum Query Length](#maximum-query-length)) together with the protocol framing
around it. The engine's defaults already do; the requirement is stated so that
lowering either is recognised for what it would be — a narrowing of the statement
surface, not a tuning change.

**The connection and transaction quotas bound a count, not a cost, and in a server
that distinction is the whole of the risk.** The engine caps concurrent
connections, in-flight statements per connection, and open transactions per
principal. Every one of those is a count. What a count does not bound is what one
statement costs: measured against this server over a store of 80 KB holding 600
nodes, one statement the deadline cuts while it is writing costs between 3618 and
3734 MB of resident memory, and given a budget long enough to reach the engine's
own row cap the same statement reaches roughly 20 GB.
[Peak Resident Memory](#peak-resident-memory) measures that cost, states the four
accumulators it is made of, and states what does and does not bound it.

**A quota is therefore not a lever over peak resident memory, and multiplying the
two factors is not a derivation this product supports.** A quota reaches the count
and nothing available here reaches the cost, so the ceiling multiplied by a single
statement's cost would be the natural upper bound to publish — and measurement
refutes it: concurrent cut writes do not multiply, and concurrent heavy reads
multiply only to a plateau (see
[Peak Resident Memory](#peak-resident-memory)). What survives that refutation is
the conclusion rather than the arithmetic: **no setting available here both
preserves throughput and bounds peak resident memory**, because a quota bounds how
many statements may run at once and nothing available here bounds what one of them
costs.

The values of those quotas are set on measurement of the server under load rather
than fixed here, because a quota is a capacity decision and this document has no
measurement of a running server to make it from. What is fixed here is what a
quota means: it bounds the count and not the cost, it is **not** a lever over peak
resident memory, and it is set deliberately rather than left at a default chosen
for a different workload.

**One roadmap, one graph, one database.** A server serves the graph of the single
roadmap it was started for. It exposes exactly one database, under the engine's
own default name, and Groadmap does not override that name. A client selects
nothing: `rmp graph client` sends no database selection, and the statement it
sends runs against the only graph the server has.

**A routing driver is not a supported client, and the reason is the address the
server advertises.** Asked for a routing table, the engine's server answers with
the address its listener reports, which for a Unix domain socket is a filesystem
path. A client that connects to the socket directly never asks, so
`rmp graph client` never sees it and nothing about it affects this product. A
driver that speaks the routing form of the protocol would receive a path where it
expects a host and a port and would fail to parse it. Groadmap neither supports
such a driver nor works around the address: the transport is a socket, a socket
has no host and no port, and a routing table over one describes nothing.

### Server Diagnostics on Stderr

The server's stderr carries two different kinds of line, and only one of them is
the plain text the rest of this product writes.

1. **Structured records.** Everything the running server reports is a `log/slog`
   record rendered by a `slog.TextHandler` Groadmap configures: one record is one
   line of `key=value` pairs, which reads on a terminal and needs no parsing
   tool. The two startup warnings of
   [Socket Path and Permissions](#socket-path-and-permissions), rules 5 and 6,
   are records of this kind, and so is everything the engine reports while a
   session is being served. It is the same handler type, configured the same way,
   that `rmp web` uses for the same purpose (`WEB.md § Logger Configuration`); the
   two long-lived surfaces MUST NOT answer this question differently.
2. **The invocation's own error line.** A failure that ends the process is
   reported in the project's plain-text error form — the `Error: ` prefix, the
   sentinel, and the AI-agent hint — exactly as a short-lived `rmp` invocation
   reports one (`HELP.md § Error message format`, and
   `COMMANDS.md § Serve Error Cases` for the lines themselves). It is not a log
   record.

Beyond those two, nothing else reaches stderr while the server runs, and no
record ever reaches stdout: stdout carries the socket announcement and nothing
else, so a caller that reads it for the path is never disturbed by a diagnostic
(`COMMANDS.md § Serve Output`). The subcommand's help, which `-h` writes and
which no serving invocation writes at all, is the usual exception every command
has.

**The engine's records are this product's lines, and that is what puts them
inside the rules below.** It is tempting to read them as the engine's output,
relayed by `rmp` and therefore not `rmp`'s to answer for. That reading is false.
The engine supplies a message and its attributes; the handler supplies everything
else on the line, because `log/slog` builds the `time` attribute inside the
handler rather than at the call site, and the handler is Groadmap's. `rmp` writes
those lines. A scope that exempted them would have to be drawn around output
whose **message** `rmp` wrote, and that same scope would exempt the web server's
records, which carry database and engine error text inside them and which
`WEB.md § Logger Configuration`, rule 5, already binds.

**Every record's timestamp is the project's canonical one, and that is a
contract.** The `time` attribute is always UTC, in the single format
`YYYY-MM-DDTHH:mm:ss.sssZ` — exactly three digits of milliseconds and an explicit
`Z` — for example `2026-09-03T15:28:27.652Z`. It is UTC whatever zone the
machine is set to, so a server log line and a task's `created_at` are directly
comparable and a log read in one time zone means the same instant as the same log
read anywhere else. `slog.TextHandler` stamps records in the **local** zone with
a numeric offset by default, which is neither UTC nor `Z`-suffixed, so the
handler MUST replace that attribute rather than accept what the standard library
produces. `DATA_FORMATS.md § Dates - ISO 8601 with UTC` is canonical for the
format and for the scope of the rule; what this section adds is that the rule
reaches every record the handler renders, the engine's included. Measured on a
machine set one hour ahead of UTC, every one of the 205 records a server emitted
under load carried the `Z` form, the two startup warnings among them; the same
records without the replacement carry `+01:00`.

**Publishing the timestamp is deliberately not publishing the line.** What is
fixed here is the timestamp and the one-record-one-line property, and nothing
further. The order of the attributes after `time`, the rendering of the level,
and the quoting of a value that contains whitespace or a control character are
`slog.TextHandler`'s, and they belong to the Go standard library rather than to
this project; fixing them here would give this specification a fact a toolchain
upgrade could falsify in silence, which is the hazard
[Dependency Maturity Risk](#dependency-maturity-risk) states for the engine and
which applies unchanged to the standard library. The one-line property is fixed
because two other rules rest on it: a record is dropped whole or not at all, and
a value carrying a newline cannot forge a second record on an operator's console
(`WEB.md § Log Integrity` is canonical for the second).

**The minimum enabled level is `INFO`, which is a load decision rather than a
default accepted.** `DEBUG` records are not emitted. Measured, the engine emits
two `DEBUG` records per connection — one when it is accepted and one when it is
closed — and `rmp graph client` opens one connection per statement, so at `DEBUG`
an idle, uncontended server writes of the order of ninety kilobytes of log per
five hundred statements, where at `INFO` those same five hundred statements
produce two records in total. The level is what keeps a server that is doing
nothing wrong quiet, and it is the same level `rmp web` enables
(`WEB.md § Logger Configuration`, rule 3).

**The two startup warnings reach stderr before the socket reaches stdout, and
that ordering is a guarantee.** The engine emits them when the Bolt server is
constructed, after the store is opened at step 6 of
[Server Startup](#server-startup) and before step 7; the queued records are then
flushed at step 7, ahead of the announcement. A caller that waits
for the announcement on stdout and then reads stderr therefore finds both
warnings there. The guarantee has to be stated because the sink below is
otherwise non-blocking and queued: without that flush the first complete line
reaches a line-oriented reader **after** the announcement — measured at 5.1 ms,
which is nothing to a person and everything to a program — and a caller reading
the two streams in that order sees no warnings and concludes the server issued
none. Those are the warnings that say every client is admitted without
credentials and that Bolt credentials travel in cleartext, so a deployment gated
on their presence would proceed on their absence. The flush is the one place the sink's
non-blocking property is deliberately suspended, and it is safe there for a
reason that does not hold later: at that instant no session exists, so nothing it
waits on is a statement being served.

**The stream is not a complete record of what happened, and it MUST NOT be read
as an audit log.** The sink between the handler and the file descriptor is
bounded and non-blocking. It holds a fixed number of rendered records, and when
the destination stops accepting writes and that queue fills, records are
**dropped** — oldest first — while the server goes on serving at full speed. This
is a limit, and it is stated as one rather than dressed as a guarantee. It is
deliberate for a reason that outranks completeness: a diagnostic channel MUST NOT
be able to stop the server. An unbounded sink puts a diagnostic write back on the
serving path, where a stderr that stops being read — a log shipper that dies, a
supervisor that stops reading, a `| head` — blocks the goroutine serving a
session, and through it the shutdown, because the engine's serve call waits for
those goroutines. Measured against an undrained stderr under concurrent writers
to one node, such a server answered fewer than half the statements it was sent
and did not return from `SIGTERM` at all; bounded, it answers all of them at full
speed and stops when it is told to. Oldest-first is deliberate too: the newest
records survive, because an operator reading a log after the fact needs the
outcome of an incident, and its beginning is usually the same flood repeated.

**The loss is declared rather than silent.** Once the destination accepts writes
again, the sink writes one record stating how many records were dropped since it
last said so, so a gap in the stream is an announced gap rather than an
unexplained one. Two consequences follow and neither is a defect. A destination
that never recovers receives no report either — but it has received nothing at
all, so a missing report is not a further loss on top of the missing records. And
a record is dropped whole: the handler renders a record complete and writes it
once, so a drop can never emit half a line. What survives is delivered in the
order it was written — the queue drops records but never reorders them — because
a log that is not an ordered account of what happened is not much of a log.

**The queued records are delivered before the process exits.** Whatever path the
process leaves by, the queue is emptied last — after the teardown of
[Server Shutdown and the Drain](#server-shutdown-and-the-drain) has written its
own final records, such as a store that failed to close or a shutdown checkpoint
that failed. That wait is **bounded**, by the retry policy's own total rather
than by a figure invented for it (`IMPLEMENTATION.md § Retry Logic`), because the
sink being emptied may be the dead one and an unbounded wait at exit would
reintroduce on the last line the hang the bound above exists to remove.

### Concurrency Inside the Server

The store's only concurrency control is MVCC, and the server is the first place in
this product where that is observable.

Until now every statement ran in a process of its own that ran exactly one
transaction, so no two of Groadmap's transactions ever overlapped and the engine's
conflict path was unreachable. A server runs many transactions concurrently in one
process. What that means is fixed by the engine and is not Groadmap's to choose:

1. **Readers never block and are never blocked.** A read transaction takes no lock
   and pins one committed snapshot for its life.
2. **Writers do not exclude one another.** Beginning a transaction acquires
   nothing: no writer serialisation, no visibility barrier, and no lock of any
   kind. Two write transactions against the same roadmap run at the same time.
3. **A write-write collision is detected rather than prevented.** The first
   updater wins, and the loser's transaction fails with a retriable serialisation
   conflict, reported as `Neo.TransientError.Transaction.Outdated`.
4. **That conflict is a normal outcome and MUST be retried.** It is not a fault,
   it does not indicate a defect in the statement, and it MUST NOT be surfaced to
   the caller on its first occurrence. Every client this product ships retries it,
   under the loop of the project's single retry policy and under that policy's
   **full-jitter** delay shape rather than its fixed ladder (see
   `IMPLEMENTATION.md § Retry Logic`, canonical for both shapes and for the
   measurements that choose between them), and reports a failure only when that
   policy or the caller's own deadline is exhausted. **The policy stays single.**
   The shape is a second entry point of the one package that owns retrying,
   selected by the caller; it is never a constant moved out of that package for
   this caller's benefit, and never a loop the client keeps privately.
5. **A retry is safe because the conflict is detected before anything is
   applied.** The losing transaction committed nothing, so re-running its
   statement runs it against a graph that never saw it. A statement that is not
   idempotent is therefore no less safe to retry here than it was to run once.
6. **Two things still serialise inside the engine, and neither is a writer lock.**
   The application of a committed transaction to the in-memory graph runs in
   transaction-sequence order after that transaction is durable, and a checkpoint
   quiesces writers for the instant in which it captures the graph. Neither is
   observable to a client as exclusion. Both are stated so that "MVCC is the only
   concurrency control" is not read as "nothing is ordered".
7. **The delay before a retry is load shedding, and not a wait for the winner to
   commit.** Rule 3 invites the opposite reading — the first updater has already
   committed, so the loser has nothing left to wait for and should re-send at
   once — and that reading is wrong by a wide margin rather than by a little.
   Measured against a real server under identical load, a client that re-sent
   immediately failed the great majority of its statements where the delaying
   client failed a fraction of one percent. A loser that waits removes itself
   from the contending set; a loser that re-sends at once keeps that set
   saturated, so the conflict rate rises with the load the retries themselves
   offer. `IMPLEMENTATION.md § Retry Logic` carries the figures and the shape
   they produced.
8. **The failure this retry exists against is a property of a single hot node
   rather than of concurrency, and that is the most useful thing a caller can be
   told.** Writers spread across distinct nodes barely collide; writers
   converging on one node collide steadily however few of them there are.
   Measured against a real server, holding the writer count at sixteen and
   varying only the number of distinct nodes written: on **one** node the retry
   policy was exhausted on 0.33% of 6,000 statements, at 474 statements per
   second; on **four** nodes, on 0.03%; on **eight or more**, on none at all,
   with throughput rising to 3,494 statements per second at sixty-four nodes. A
   caller that meets this failure is therefore not being told to reduce its
   concurrency. It is being told that all of its writers are landing on one
   node — which is the shape this project produces itself when several agents
   stamp provenance on the same node — and the remedy that removes the failure,
   rather than moving the threshold at which it appears, is to spread those
   writes across distinct nodes.
9. **An exhausted retry is reported as itself, and is not left to be inferred
   from the engine's diagnostic.** A caller that has spent the whole retry budget
   losing conflicts has exactly one decision to make — run the statement again,
   or correct it — and the two courses are opposite: treating contention as a bad
   statement stops work that was right, and treating a bad statement as
   contention re-runs a write that may not be idempotent. The failure therefore
   carries a line of its own, whose text is `rmp`'s own from end to end: it names
   the contention, states that nothing was written, and says what to do about it.
   `COMMANDS.md § Graph Management` publishes the exact line, and
   [Error Handling and Exit Codes](#error-handling-and-exit-codes), rule 7,
   states the class it carries.

### Durability and Checkpointing in a Long-Lived Process

Durability does not weaken because the process is long-lived: a commit is durable
before it is acknowledged, and that is what makes an acknowledgement mean
something.

1. **The commit protocol is the engine's and is unchanged.** Every operation of a
   transaction is appended to the write-ahead log, then a commit marker, then one
   synchronisation to disk, and only then is the transaction applied in memory.
   The protocol's acknowledgement is written after that call returns, so a client
   reading a successful commit is reading one that is already on disk, and a crash
   recovers all of a transaction or none of it.
2. **The engine MUST be the one with a write-ahead log behind it.** An engine
   constructed over a bare in-memory graph accepts every write, acknowledges every
   commit, warns about nothing, and loses all of it when the process ends. The
   constructor that avoids this is fixed by
   [Engine Constructor by Path](#engine-constructor-by-path), and the server is on
   that one path like every other surface.
3. **The server checkpoints; it does not checkpoint per write.** The rule for a
   short-lived invocation — checkpoint synchronously after any transaction that
   appended to the log (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)) — exists
   because such an invocation has no later opportunity: it is about to exit. A
   server has later opportunities, and a full snapshot after every committed write
   would make every write cost the whole live graph while its neighbours waited
   for the quiesce that capture takes.
4. **The server MUST checkpoint at shutdown when, and only when, the write-ahead
   log has grown since it was last folded**, after the drain and before it
   releases the lock, so that the log the next open replays is short and the
   snapshot on disk is current. The condition is the same one
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write) applies to
   a short-lived invocation, and it MUST be the same realisation of that condition
   rather than a second one beside it: one comparison and one mark, so that the
   two cannot drift. A shutdown that owes no fold writes nothing at all —
   `snapshot/` and `wal` are left byte for byte as the server found them — which
   is what makes the guarantee in rule 8 below hold at the surface a long-lived
   process exposes.
5. **The server MUST also checkpoint while it runs**, because the write-ahead log
   otherwise grows for the whole process lifetime and the cost of recovering from
   a kill grows with it. That checkpoint MUST be driven through the engine's
   commit serialiser, so what it captures is a real transaction boundary and not a
   graph caught mid-commit.
6. **The cadence of that checkpoint is set on measurement, and no value for it is
   fixed here.** Its cost is proportional to the live graph size — measured from
   19.7 ms on a 1.3 MB store to 964 ms on a 122 MB one (see
   [Lock Contention](#lock-contention)) — and its benefit is proportional to how
   fast the log is growing, which is a property of the workload. Neither quantity
   is knowable from this document. What is fixed here is that a cadence exists and
   that it is bounded by something other than the process's lifetime.
7. **A checkpoint failure does not fail a write that has already committed
   durably.** The write succeeded, the
   log is intact, the next successful checkpoint reconciles the snapshot, and the
   failure is a diagnostic rather than a failed statement (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)). The
   reconciliation is what an over-long field does not get: it is committed state,
   so it refuses every later checkpoint too, and the server reports it as its own
   condition on the one checkpoint path whose error it holds (see
   [Field Length Limits](#field-length-limits), rules 6 to 9).
8. **An unconditional checkpoint is not merely a wasted write; it publishes a
   permanent residue, and that is why rule 4's condition is a requirement rather
   than an optimisation.** A statement the deadline cuts while it is writing is
   rolled back whole, and the rollback restores the **logical** graph but not the
   **physical** one: the engine's key mapper keeps the interned key of every node
   the statement created, and the tombstone set keeps a tombstone for each. A
   checkpoint taken afterwards serialises that residue to disk, where nothing
   removes it and where every later reader pays for it. Measured against a server,
   one cut `MATCH (a),(b),(c) CREATE ()` over a store of 80 KB holding 600 nodes
   left the store at **134 MB** while the graph still held exactly its 600 nodes,
   and a subsequent `MATCH (n) RETURN count(*)` over that store then cost 1.48 s
   and 670 MB against 0.01 s and 21.6 MB on a clean one. A later ordinary write
   rewrites the same residue, so it does not decay. The control isolates it to the
   write path: one cut **read** over the same store left it at 80 KB. Rule 4's
   condition is what keeps the residue off the disk: a cut statement commits
   nothing, so it appends nothing to the write-ahead log, so no fold is owed and
   the shutdown writes nothing at all
   ([Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)).
9. **The in-flight checkpoint of rule 5 is not conditioned, and the residue of
   rule 8 is still reachable through it.** It folds on the cadence rule 6 fixes,
   driven by the engine's own loop rather than by Groadmap, so a server that
   outlives its first fold can publish that residue without ever being shut down.
   Conditioning it would mean Groadmap driving that fold itself, which is new
   behaviour inside the server process rather than a value, and this version does
   not specify it. What rule 4 closes is the shutdown window; what bounds the
   remaining one is the cadence, and nothing else does.

### Server Resolution

**This section is the single statement of how a caller reaches a roadmap's graph
server, and every surface that reaches a graph follows it.** `rmp graph client`
follows it, and so does the web graph data endpoint (see
`WEB.md § Knowledge Graph from the GoGraph Store`). Neither states a rule of its
own; each states only the outcome its own surface reports for each of the four
states below.

**There is one way in, and this section is it.** `rmp graph serve` is the only
process that opens a roadmap's graph store
([Engine Constructor by Path](#engine-constructor-by-path)), so the only question a
caller has is whether a server is there. Three of the four answers below are
failures, and none of them is a fall back onto the store: no caller has a second
path to fall back to. That is what the rule buys — a server's hold on the exclusive
advisory lock lasts as long as its process does, and no finite wait can be sized
against a hold with no upper bound (see [Lock Contention](#lock-contention)), so a
caller that reached the store directly against a served roadmap would wait its
whole wait budget and then fail, on every invocation, for as long as the server
ran. Removing the second path removes that outcome rather than merely avoiding it.

**The resolution is a probe, and the probe is bounded.** The caller resolves the
socket **in force** — the path derived from the roadmap, or the value of
`--socket` where the surface publishes that flag (see
[Socket Path and Permissions](#socket-path-and-permissions), rule 2) — connects to
it, and completes the protocol handshake. The whole probe carries a deadline of the project's backoff total,
2500 ms (see `IMPLEMENTATION.md § Retry Logic`), reused here as the allowance for
a cost that is local and scheduling-bound rather than an I/O one: connecting to a
socket in the local filesystem either succeeds or fails inside the kernel, and the
handshake is one exchange with a process on the same machine. The probe is not
retried. It is spent before the statement starts.

The four states, and what each surface does with each:

In the table below, "the socket" is always the socket in force for that
invocation or request, never the derived path specifically.

| State | How the caller recognises it | `rmp graph client` | The web graph data endpoint |
|-------|------------------------------|--------------------|-----------------------------|
| **Not served: no socket** | The socket path does not exist | Fail with `utils.ErrGraphServer`, exit code 1, reporting that no server is listening | The graph is unavailable: HTTP `503` |
| **Not served: nothing listening** | The connection is refused, which is what a socket file left behind by a killed server answers | As above, and the leftover file is neither a different condition nor removed | As above. HTTP `503` |
| **Served** | The connection is accepted and the handshake completes inside the probe deadline | Send the statement to the server. Exit code 0 on success | Send the statement to the server. HTTP `200` on success |
| **Unreachable** | The connection is accepted but the handshake does not complete inside the probe deadline, or the connection fails for any reason other than the two above | Fail with `utils.ErrGraphServer`, exit code 1 | The graph is unavailable: HTTP `503` |

Rules:

1. **A leftover socket file is not a condition of its own, and no caller removes
   one.** A server that was killed leaves its socket behind, and the refusal a
   connection to it receives is the whole of the evidence needed to conclude that
   nothing is listening. For every surface that is the same fact as an absent
   socket and carries the same outcome, so the two are not distinguished in what
   the caller reports. The caller does **not** remove the file: removing one is
   the next server's business, and a caller that removed one would race a server
   that was binding it.
2. **Every answer but a completed handshake is a failure.** The two definite
   negatives above mean "no server is listening"; everything else — a handshake
   that does not complete, a permission the caller does not have, a path that is
   not a socket — means "a server could not be reached". The two are reported
   apart because they call for different actions: the first says to start a
   server, the second says that something is answering on that path and is not
   serving this roadmap's graph.
   `COMMANDS.md § Graph Server Socket Error Lines` publishes the exact line each
   of these failures writes.
3. **No caller opens the store, on any outcome.** Resolution decides whether a
   statement can run, not where it runs, because there is only one place it can
   run. A caller takes no advisory lock, creates no graph directory, and runs no
   recovery, whether the probe succeeded or failed.
4. **A connection lost once the statement has been sent is a failure, and the
   caller MUST NOT try to resolve it by running the statement again.** The
   statement may already have committed on the server: the commit is durable
   before it is acknowledged, so a connection that dies between the two leaves the
   caller unable to tell whether it happened. A second run would apply it twice.
   The
   invocation therefore fails and reports what it can honestly report — that the
   connection to the server was lost and the statement's outcome is unknown.
   `rmp graph client` fails with `utils.ErrGraphServer` and exit code 1. The web
   graph data endpoint answers HTTP `400` with the `execution` kind, because the
   failure surfaced once the statement was running, which is where
   `WEB.md § Query-Bar Error Handling` already draws that boundary.
5. **The statement crosses unchanged.** The caller sends the statement it was
   given. The node-`LIMIT` injection the web endpoint performs is applied before
   the statement is sent (see `WEB.md § Graph Data Endpoint`). Resolution decides
   whether a statement runs and changes nothing about what runs.
6. **The result is the same result on both surfaces, and the documents that
   carry it are not.** A statement produces the same values for the same elements
   whichever surface submitted it, and which surface that was is not observable in
   what the engine answered. The two then render those values differently, by
   design: the CLI publishes the columns-and-rows object and the endpoint a graph
   view of nodes and edges. `DATA_FORMATS.md § Graph Client Result` is canonical
   for the mapping that makes the first half true of a result carried over the
   protocol.
7. **The statement budget is the server's, and the caller keeps a backstop of its
   own.** The server bounds the statement at the statement budget (see
   [Server Options](#server-options)), and the caller keeps a deadline so that a
   server which answers nothing at all cannot hold it for ever. **The caller's
   deadline is the wait budget, statement budget plus backoff total, and never the
   statement budget itself.** The reason is a false report the equal values would
   produce: a statement that commits a few milliseconds before the budget expires
   has its acknowledgement in flight when a caller-side deadline of exactly the
   budget fires, and the caller would then print the budget line — which states
   that nothing was written — over a write that had succeeded. Giving the caller
   the later deadline makes the server's typed failure the one that arrives, so
   the budget line is printed only when the engine really did cut the statement
   and really did roll it back. The caller's own deadline is a backstop, and when
   it is what fires the failure is reported as an unanswered server rather than as
   a budget exhaustion: the connection is intact, the server is alive, and the
   statement's outcome is unknown. That is what a statement the budget cut
   **mid-write** looks like from outside, because the engine's undo replay runs
   past the deadline by a factor nothing bounds (see
   [Statement Time Budget](#statement-time-budget)). Both bounds are derived from
   one declaration, so they cannot disagree about the value.

   A statement genuinely cut by the budget therefore reports the same class of
   failure at either surface: exit code 1 for `rmp graph client`, and an execution
   failure with HTTP `400` for the web graph data endpoint.
8. **A retriable serialisation conflict is retried, and is not surfaced on its
   first occurrence.** It is a normal outcome (see
   [Concurrency Inside the Server](#concurrency-inside-the-server)), and a caller
   that surfaced it at once would report a defect where the store reported
   ordinary concurrency. The retry runs under the loop of the project's single
   retry policy and under that policy's full-jitter delay shape (see
   `IMPLEMENTATION.md § Retry Logic`) and is bounded additionally by the caller's
   own deadline, whichever ends first. Two outcomes follow, and each is reported
   as itself: a caller whose **retry policy** is exhausted — every attempt having
   collided — fails with a line of its own that names the contention, states that
   nothing was written, and says what to do about it, because at that point the
   conflict is the outcome and a caller unable to tell it from an invalid
   statement would have nothing to act on; `COMMANDS.md § Graph Management`
   publishes that line. A caller whose **deadline** expires first fails as that
   deadline requires, under rule 7.

   **The retry gives up with most of the caller's deadline unspent, and that is
   deliberate rather than an oversight.** The retry policy's total is 2500 ms and
   the caller's deadline is the wait budget, 7.5 seconds, so an exhausted retry
   reports its failure with roughly five seconds of that deadline still in hand.
   The headroom is not the retry's to spend: it belongs to the statement, and it
   exists so that a statement the server is still lawfully executing — for up to
   the whole statement budget, and past it while the engine undoes a cut write —
   is not cut short by the caller's own backstop, which is the whole of rule 7's
   reasoning. Spending it on retries instead would make the wait budget's
   published derivation false and would put a conflict within a second and a half
   of being reported as a server that did not answer and a statement whose
   outcome is unknown, which for a conflict whose loser provably committed
   nothing would be false rather than merely cautious. The measurement confirms
   the choice rather than only asserting it: lengthening the retry was measured
   against reshaping it and is dominated on every axis (see
   `IMPLEMENTATION.md § Retry Logic`), so the headroom would buy nothing the
   shape has not already bought inside 2500 ms.
9. **Resolution runs once per invocation and once per request, and its outcome is
   not cached.** A short-lived invocation has nothing to cache the outcome for, and
   a web request that cached one would act on a server that had since stopped.
10. **The path resolved is the path in force, and the surfaces differ only in
    whether a caller can set it.** `graph client` and `graph serve` each take
    `--socket` and each default it identically; the web graph data endpoint takes
    none and resolves the derived path. Everything else in this section is the
    same on both surfaces, and the asymmetry has exactly one consequence, stated
    in [Serving on a Non-Default Socket](#serving-on-a-non-default-socket).
11. **No surface has a second path, and that is now the whole of the rule.** The
    subcommand that once opened the store when nothing answered has been
    withdrawn, and the web graph data endpoint that once did the same reaches the
    graph through the same client as `rmp graph client`. What used to be a choice
    between two paths is a single question with one affirmative answer, so
    "resolution" here means finding the server and nothing more; see
    [The Bolt Client](#the-bolt-client).
12. **A resolved path longer than the platform's socket-path bound is settled
    before the probe, and it is settled the same way on every surface.** A path
    that cannot be bound cannot be listened on, so probing it can tell a caller
    nothing it does not already know. It is **not** one of the two definite
    negatives the first two states describe: those report that no server is
    listening now, while a path over the bound reports that none can ever listen
    there, and the second fact is not served by the answer the first one gets.
    The resolution therefore fails before the probe — on `rmp graph serve` and
    `rmp graph client` alike, and the web graph data endpoint refuses the request
    — whether the path was derived from the roadmap or supplied through
    `--socket`. [Socket Path Length](#socket-path-length) is canonical for the
    bound, for the line each of these failures publishes, and for why the rule is
    uniform.

### The Bolt Client

`rmp graph client` and the client half of [Server Resolution](#server-resolution)
are one implementation rather than two, and the web graph data endpoint is on that
same one. The subcommand is a thin wrapper over the shared client: the same
connection, the same statement, the same retry, and the same mapping of protocol
values onto JSON. A second implementation would be a second set of answers to
every question this section settles.

**"The same client" means the same code, not the same command.** The web graph
data endpoint reaches the graph by calling the shared client directly, in its own
process (see `ARCHITECTURE.md § 9. internal/graphclient/ and reaching a graph
server`). It MUST NOT reach it by running `rmp graph client` as a child process.
Spawning one would put a process boundary, an argument-quoting layer, an exit code
and a second copy of the output serialisation between the endpoint and the answer
it is required to give, and it would make the endpoint's behaviour depend on the
binary found on a path rather than on the code it was built from. What both
surfaces share is the mechanism; what each keeps is its own way of reporting a
failure, which is a CLI exit code on one and an HTTP status on the other.

What the subcommand adds over that shared client is what a command-line surface
adds. It reads the statement from `-q` / `--query` or from standard input under
the rules [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)
already fixes, it serialises the result to stdout, it writes diagnostics to
stderr, and it chooses an exit code.

What it deliberately does not do is fall back. A roadmap with no server listening
is a failure for this subcommand and not a signal to open the store: its contract
is that it speaks to a server, and no surface may open a store the server owns.
Since the withdrawal of the direct path this is no longer a property that
distinguishes the subcommand from a sibling — there is no sibling — but it remains
the property a caller must be able to rely on: a result from this subcommand is
evidence that a server was reached.

### Serving on a Non-Default Socket

`--socket` moves the socket off the path every surface derives. **The command line
can follow it and the web interface cannot, and that boundary is the whole of what
this section is about.**

1. **The CLI follows, through the flag.** `graph serve` and `graph client` both
   publish `--socket` and both default it identically, so a server started on a
   non-default path is reached by giving the same path to the subcommand that
   talks to it. Nothing is lost on this side: a statement sent that way runs on the
   server, returns the same result, and carries the same exit code as one sent to a
   server on the derived path.
2. **The web graph data endpoint cannot follow, and this is not an oversight.** It
   is an HTTP handler; it has no command line, `rmp web` serves every roadmap at
   once rather than one, and no request parameter carries a socket path. It
   therefore resolves the derived path, finds nothing there, concludes that no
   server is listening, and reports the graph as unavailable — HTTP `503`, on every
   request for that roadmap's graph, for as long as that server runs on a path the
   endpoint cannot name. The status is honest about the state: a server is running
   and the endpoint cannot reach it, which the operator clears by restarting that
   server on the derived path.
3. **The residual is named rather than removed, and its shape has changed.** While
   a direct path existed, a server on a non-default socket left the web page
   waiting the whole wait budget on a lock it could never take, and answering `500`
   at the end of it. Neither the wait nor that status survives: the endpoint learns
   inside the probe deadline that nothing is listening and answers `503` at once.
   What remains is that the page cannot be served for that roadmap.
   Closing that would take a mechanism the caller does not supply — the server
   advertising its bound path somewhere every surface reads — and this
   specification does not introduce one.
4. **`--socket` is therefore specified as what it is: an option that keeps the CLI
   and costs the web page.** It exists for a server the browser is not expected to
   reach: a test harness, a diagnostic session, a socket that has to live on a
   different filesystem. A server whose roadmap is also browsed is started without
   it.
5. The refusal the web request meets in that state is the absent socket's, and it
   reports the graph as unavailable with HTTP `503`. It does not report that a
   server is running on another path, because nothing in the product knows that:
   the request probed a path the server never bound.
6. **A mistyped `--socket` fails rather than succeeding quietly.** Every mistyped
   path that could lawfully name a socket resolves to "no server is listening",
   and the invocation fails with the line
   `COMMANDS.md § Graph Server Socket Error Lines` publishes for it, naming the
   path it actually probed. No reading sends the invocation somewhere else
   instead, because no surface has a second way to a graph (see
   [Server Resolution](#server-resolution), rule 11). One class of mistyped value
   is caught earlier still: a path longer than the platform's socket-path bound
   cannot name a socket at all, so it is refused before the probe rather than
   probed (see [Socket Path Length](#socket-path-length), rule 5).

## Concurrency and Recovery

GoGraph's store is transactional, and MVCC is its only concurrency-control
mechanism. Reads observe a consistent committed state. Independent write
transactions are not excluded from one another inside a single process: a
write-write collision is detected rather than prevented, on a first-updater-wins
basis, and the losing transaction receives a retriable serialization-conflict
error. Groadmap relies on that intra-process behaviour, because every statement now
runs inside `rmp graph serve`, which runs many transactions concurrently in one
process. A conflict is therefore an ordinary outcome rather than an unreachable
one, and every client is obliged to retry it. What a conflict means, and the retry
it obliges, are specified in
[Concurrency Inside the Server](#concurrency-inside-the-server).

Groadmap does not depend on the engine to serialise access to the store between
processes. It serialises it itself, at the process level, through a single advisory
lock file that Groadmap maintains in the roadmap's graph directory, `write.lock`
(see [Persistence Layout](#persistence-layout)). **`rmp graph serve` is the only
process that takes it.** It takes the lock **exclusively** before it opens the
store and holds it until the process stops, so the lock's whole remaining purpose
is to admit one server per roadmap. No caller takes it, because no caller opens a
store (see [Server Resolution](#server-resolution)).

**There is one lock mode, because there is one holder.** A mode that distinguished
readers from writers would have to be taken per statement by a party that knows
what the statement does, and Groadmap does not examine a statement. Held once per
process by the only process that opens the store, the exclusive mode is the mode
that is correct. Serialisation *between statements* is no longer this lock's
business at all: inside the server it is the store's MVCC that resolves concurrent
transactions (see
[Concurrency Inside the Server](#concurrency-inside-the-server)).

The operating system releases the lock when the holding process exits, so an
invocation that crashes does not strand it. The lock file itself is created by
whichever invocation first needs it, and is never removed.

The exclusive lock covers the whole sequence, not just the transaction, because
that is the span that must not interleave: a second writer that had loaded the
graph before the first writer's commit would checkpoint a full snapshot of its own
stale in-memory graph and then truncate the write-ahead log that still held the
first writer's committed change, silently losing an acknowledged write. Because
the sequence Groadmap needs serialised is wider than a transaction, no
engine-level writer exclusion would have covered it in any case.

Opening the store is itself not a read-only operation on disk, which is a second
reason the lock is taken before the open rather than around the transaction alone.
Opening it runs GoGraph's recovery step, and recovery repairs an interrupted
checkpoint before it loads anything: it removes a stale staging directory
`snapshot.tmp` unconditionally, and, when the live `snapshot/` directory carries no
manifest while `snapshot.bak/` does, it promotes the backup by renaming
`snapshot.bak` to `snapshot` and making that rename durable. Both actions repair
the very directory a checkpoint publishes into.

Durability is provided by a write-ahead log with CRC32C integrity checks plus
atomic on-disk snapshots; on opening the store, GoGraph runs recovery to restore
the last committed state from the snapshot and log.

### What a Statement That Writes Nothing Changes on Disk

A statement whose transaction appended nothing to the write-ahead log changes
exactly what the recovery repair above changes, and nothing else. It is not
distinguished in advance — the transaction runs, appends nothing, and commits —
and it is the write-ahead log's own state that decides whether the fold of
[Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write) is owed at all.
Such a statement, whether it arrived through `rmp graph client` or through a web
graph request:

1. Adds, alters, and removes no node, relationship, property, label, index, or
   constraint, because it wrote none.
2. MUST NOT checkpoint and MUST NOT write a snapshot. The contents of `snapshot/`
   are left exactly as the statement found them.
3. MUST NOT truncate the write-ahead log. The log is left byte for byte as the
   statement found it, so a statement that wrote nothing never shortens the
   history a subsequent recovery replays.
4. MAY remove a stale `snapshot.tmp` staging directory, and MAY promote
   `snapshot.bak` to `snapshot`, as the recovery repair above describes.
5. Creates nothing. The lock file `write.lock` and the graph directory that holds
   it are both created by `rmp graph serve` before it opens the store (see
   [Server Startup](#server-startup), step 1, and
   [Persistence Layout](#persistence-layout), rule 2), so by the time any statement
   can be submitted at all, both already exist. No statement creates either, and no
   caller creates anything: a caller opens no store.

The **content** of the graph is therefore never changed by a statement that writes
nothing. What such a statement can change is the store directory's structure, and
only by completing a repair that the store open would otherwise complete instead.
Since the store is opened once per server rather than once per statement, item 4's
repair is reachable by a statement only in the sense that the server's own open
performed it before any statement ran.

### Statement Time Budget

**Every statement runs under a deadline, the server enforces it, and every
surface gets the same one.** The graph server bounds a statement at 5 seconds (see
[Server Options](#server-options)), so a statement sent by `rmp graph client` and
one sent by the web graph data endpoint are cut at the same point by the same
party. `WEB.md § Graph Query Time Budget` fixes that value, states the evidence for
it, and is canonical for it. This section is canonical for what the budget does to
a statement and for what a cut statement leaves behind. The value is one
declaration that the server and both surfaces read, so there is no second constant
to drift from the first.

The budget bounds the **variable** part of what an outgoing server's drain must
wait for — the statement, whose cost the caller chooses — for a read and for a
statement that runs to completion. It does not bound a statement the deadline cuts
while that statement is writing: the measurements at the end of this section show
that such a hold has no known upper bound. It does not bound the fixed part of a
shutdown either, which [Lock Contention](#lock-contention) accounts for separately,
and it bounds nothing at all about what the statement costs in memory, which
[Peak Resident Memory](#peak-resident-memory) measures. That section states what
the wait derived from this budget covers, what it does not, and the residual
limits that survive.

1. **The deadline covers the execution of the statement and the walk over its
   result.** It starts when the server begins executing the statement. It does not
   cover the caller's resolution probe, the server's own store open, the recovery
   repair that open performed, or a checkpoint. Two things can end a statement
   early and they are not the same: this deadline, which cancels it inside the
   server and rolls it back, and the caller's own backstop, which abandons the
   answer without stopping the statement (see
   [Server Resolution](#server-resolution), rule 7). The web graph data endpoint
   has a third, a request whose client disconnects, which a CLI invocation does
   not (see `WEB.md § Graph Query Time Budget`, rule 2).
2. **A cut statement rolls back whole.** There is no partial write to reconcile
   and no torn state on disk. Measured: a writing statement over a Cartesian
   product, cut two seconds into a run that would otherwise have made 4.4 million
   writes, left **zero** of its nodes behind when the store was closed, reopened
   from disk, and the survivors counted.
3. **No checkpoint runs, and nothing on disk is rewritten.** A cut statement
   committed no change, so it never checkpoints and never truncates the
   write-ahead log. `snapshot/` and `wal` are left exactly as the statement found
   them, which is what
   [What a Statement That Writes Nothing Changes on Disk](#what-a-statement-that-writes-nothing-changes-on-disk)
   requires of every statement that commits nothing. The recovery repair the open
   already performed is neither undone nor repeated.
4. **The caller fails with `utils.ErrGraphEngine` (exit code 1).** The
   budget introduces no new exit code, and may not:
   [Constraints](#constraints), rule 5, forbids it. It carries the engine's
   sentinel rather than the store's because the statement was running in the
   engine when it was cut, and because what the caller must act on is the
   statement. The exact line the user reads is published in
   `COMMANDS.md § Graph Management`.
5. **The cancellation arrives through the result iteration.** The engine's
   statement call returns no error; the result's own error does, as
   `context.DeadlineExceeded`. An implementation that classified only the call's
   error would report a cut statement as an ordinary query failure and tell the
   user nothing about the budget. The classification is made once, inside the
   server, and reaches both surfaces as the same typed failure over the protocol,
   which is why one classification serves both.
6. **The budget is a limit on what a user may run, and it is published as one.**
   A statement whose work takes longer than five seconds fails, however valid its
   Cypher is and however healthy the store. The remedy is to narrow the statement,
   and narrowing is effective rather than merely available. Measured on a
   44,906-node graph whose store open alone costs 962 ms, an untargeted
   whole-graph `MATCH (a)-[*1..3]->(b) RETURN count(*)`
   costs 10.08 s end to end, while the targeted
   `MATCH (a:Class)-[:DEPENDS_ON*1..3]->(b) RETURN count(*)` costs 1.52 s end to
   end: a statement of 554 ms against one of roughly nine seconds. The published
   error line names that remedy for the same reason.

**On a cut read the deadline is honoured promptly, which is what makes it a real
bound rather than a nominal one.** Measured against the largest real knowledge
graph on the development machine, 44,906 nodes in 36 MB, the engine returned
between 1.6 ms and 4.5 ms after the deadline on read statements that otherwise
run for minutes, including a three-way Cartesian product over 9.4 billion tuples
that had not finished after 300 seconds. A cut read is honoured at **1.000x** its
deadline at every budget measured: 1, 2, 4 and 5 seconds.

**A statement cut while it is writing does not return promptly, and its overrun
is a property of the statement rather than a constant.** The forward pass is cut
at the deadline exactly as a read's is. The transaction is then rolled back whole
(rule 2), and the rollback undoes every mutation the statement had already
applied, one inverse write per mutation. The excess over the deadline is
therefore proportional to the number of rows the statement managed to write
inside its budget — and a **cheaper** write per row is worse rather than better,
because more rows fit inside the same budget. Measured over a 600-node store at a
2-second budget, timing the whole hold and varying only the writing clause of
`MATCH (a),(b),(c) ...`:

| Writing clause | Hold, as a multiple of the budget |
|----------------|----------------------------------:|
| a clause whose `WHERE` matches no row, so nothing is written | 1.005x |
| `REMOVE a.nosuch`, removing a property no node carries | 1.007x |
| `SET a.touched = 1` | 1.06x |
| `CREATE (:P {k:a.i})` | 2.09x |
| `CREATE (a)-[:R]->(b)` | 2.36x |
| `CREATE (:P)` | 3.80x |
| `CREATE ()` | 6.08x |

**At the budget in force the longest hold measured is 35.6 seconds.** The last
shape above, `MATCH (a),(b),(c) CREATE ()`, over the same 600-node store, timed
from the moment the lock is taken to the moment it is released:

| Statement budget | Hold | Multiple of the budget |
|-----------------:|-----:|-----------------------:|
| 3 s | 19.9 s | 6.6x |
| 4 s | 27.9 s | 7.0x |
| 5 s, the budget in force | 35.6 s | 7.1x |

**That figure is the largest measured and not a maximum, and the way it moved is
itself the evidence of which.** The same shape at the same budget over the same
store has been measured at 34.5 seconds and, later, at 35.6 seconds. How many rows
a statement fits inside its budget varies from run to run, so the hold varies with
it, and a later measurement that exceeds the number published here does not
contradict this section — it is the paragraph below, observed once more.

**Nothing measured establishes a ceiling.** The ladder is monotone in how cheap
the writing clause is, `CREATE ()` is merely the cheapest clause that was tried,
and the corpus of shapes is not exhaustive. This specification therefore
publishes a measured range and no upper bound, and
[Lock Contention](#lock-contention) states what having no upper bound costs the
wait that must cover such a hold.

**The overrun is neither a fixed cost nor a function of the graph's size.** It is
not part of the fixed part of a hold: it scales with the budget and with what the
statement wrote, and a statement that writes nothing does not carry it — a
write-routed statement whose `WHERE` matches no row is honoured at 1.005x, so it
is not the routing to the write path that costs the time. It does not grow with
the store either: the same `CREATE ()` at a 1-second budget over seeds of 300,
600, 1200 and 2400 nodes held the lock 5.23, 5.15, 5.21 and 5.11 seconds. What
governs it is write throughput, which is a different quantity from the
per-megabyte cost of the fixed part that [Lock Contention](#lock-contention)
measures.

**The overrun cannot be cancelled once it has begun, and that is a limitation of
the engine rather than a choice Groadmap makes.** The engine cuts the forward
drain at the deadline, checking the context once per row, which is why a cut read
returns at 1.000x. The rollback then runs inside the same engine call, before
that call returns, and the undo replay takes no context at all: it observes no
deadline and no cancellation. There is therefore no point at which the invocation
can interrupt it, because the invocation is still inside the engine call, and the
deadline — the one mechanism this specification gives a statement — has already
done everything it can do. Bounding this hold at its source means threading a
deadline into that undo replay in the engine, which would return the write path
to the 1.000x the read path already achieves.

### Peak Resident Memory

**One statement can cost gigabytes of resident memory, on every surface, and
nothing in Groadmap's own configuration bounds it.** This section is canonical
for what a statement costs in memory, for what that memory is made of, for what
happens when the cost cannot be served, and for the different exposure of the
three surfaces. It is a different quantity from the lock hold
[Lock Contention](#lock-contention) measures, and the two do not order
statements the same way.

**The cost tracks the budget, not the graph.** Measured over a store of 80 KB
holding 600 nodes, `MATCH (a),(b),(c) CREATE ()` — the cheapest writing clause
tried, and therefore the one that fits the most rows inside a budget:

| Statement budget | Peak resident memory |
|-----------------:|---------------------:|
| 1 s | 754 MB |
| 3 s | 1966 MB |
| 5 s, the budget in force | 3293 MB |

The same statement at a 3-second budget over seeds of 150, 600 and 2400 nodes
(40, 80 and 248 KB on disk) costs 1892, 1900 and 1907 MB. So the memory is
linear in how many rows the statement managed to apply, which the budget
decides, and it is flat in the size of the graph — the same relation
[Statement Time Budget](#statement-time-budget) measures for the hold. A
statement that applies no row costs nothing measurable: a write-routed statement
whose `WHERE` matches no row runs the identical Cartesian product for its whole
budget and holds at 18 MB, and so does a read that materialises nothing.

**Four accumulators hold that memory, and the undo log is the minority of it.**
[Statement Time Budget](#statement-time-budget) states that a cut write retains
every mutation it has applied so that the rollback can undo it. That is true,
and it is not where most of the memory goes. Heap-profiled at the deadline with
a forced collection, on the statement above:

| Retained accumulator | Share |
|----------------------|------:|
| the transactional store's in-memory write-ahead-log operation buffer | 39% |
| the applied in-memory graph state | 38% |
| the undo log | 20% |
| everything else | 3% |

Across four statement shapes the undo log ranges from 18% to 34% and the Cypher
engine's index buffer reaches 19%. The attribution decides what a repair can
reach, which is why it is published rather than summarised: threading a deadline
into the undo replay — the repair
[Statement Time Budget](#statement-time-budget) names for the **hold** — would
not touch four fifths of this memory, because four fifths of it is spent before
the replay begins.

**A pure read costs as much and carries none of the hold defect, so the two
defects are distinct.** `MATCH (a),(b),(c) RETURN a` materialises 6.48 million
node values inside its budget, costs 3143 MB, has no undo log, no rollback and
no overrun, and is honoured at 1.000x its deadline. Ordering the same shapes by
hold and by memory gives two ladders that are close to inverted:

| Statement (`MATCH (a),(b),(c) ...`) | Hold, as a multiple of the 5 s budget | Peak resident memory |
|-------------------------------------|--------------------------------------:|---------------------:|
| `RETURN a`, a read | 1.00x | 3143 MB |
| `SET a.touched = 1` | 1.06x | 1452 MB |
| `CREATE (a)-[:R]->(b)` | 2.47x | 147 MB |
| `CREATE ()` | 7.13x | 3293 MB |

The relationship write is the cheapest in memory of the four, at 147 MB, and it
is the one with the highest cost per row: it applies only 47,000 edges inside
the budget, where `CREATE ()` applies millions. An expensive per-row write is
exactly what stops a statement applying enough rows to consume memory, so what
makes a shape hold the lock is what keeps it cheap in memory. `SET` is the
mirror image — nearly invisible on the availability axis at 1.06x, and the
second-heaviest write shape measured. **The memory defect is therefore not the
availability defect seen from another side.** The two overlap and neither
contains the other, and a repair aimed at one does not settle the other.

**There is a ceiling, it is the engine's, and it is not a useful bound.** The
engine applies a default cap on the number of rows one statement may produce.
Its value is the engine's and is not restated here (see
[Dependency Maturity Risk](#dependency-maturity-risk)); what is stated is what
it does. Given a budget long enough to reach it — 600 seconds, confined to a
memory cgroup so the host was never at risk — `MATCH (a),(b),(c) CREATE ()` over
the same 600-node store was cut by that cap rather than by the deadline, at
20,000,002 mutations and **20,419 MB**, holding the lock 470 seconds;
`... SET a.touched = 1` was cut at the same mutation count and 13,662 MB. So one
statement is bounded at roughly **20 GB** rather than unbounded. That is two
thirds of the memory of the machine the figure was taken on, it moves with a
constant this specification does not own, and it is a published limit rather
than a useful one.

**A write with no `RETURN` clause is charged half the rows it applies.** At an
explicit cap of N such a write applies **2N+2** mutations, while the same write
with a `RETURN` applies **N+1** — measured exactly, at four caps and on four
shapes, with memory scaling alongside the mutation count. A write submitted
without a `RETURN` is a first-class form on this surface, which
[Acceptance Criteria](#acceptance-criteria), criterion 1, requires, so the
shorter of the two forms is the one that costs twice the cap it is given. It is
the engine's behaviour, nothing in Groadmap changes it, and it is recorded
upstream.

**When the cost cannot be served the process is killed by the operating
system.** The kernel's out-of-memory killer delivers `SIGKILL`: the process
exits 137, writes nothing to stdout and nothing to stderr, and the Go runtime is
never given the chance to report. There is no Go out-of-memory panic and no
graceful failure, and Groadmap cannot produce one: on the Linux configuration
measured, with the kernel's default overcommit setting, the allocation succeeds
as virtual memory and the kill arrives on a page fault, inside no code path this
product can reach. A caller sees a process that vanished.

**What that kill leaves on disk is the good half, and it is structural rather
than lucky.** After every kill measured — during the forward pass, during the
undo replay, and against a server with eight statements in flight — `wal` and
every file under `snapshot/` were byte-identical to what the statement found,
and reopening the store replayed every committed write with zero survivors of
the killed one. Nothing durable is written before the commit: the write-ahead
log's operation list is buffered in memory, which is the 39% share above, and
reaches disk only when the result closes. A kill at any point before that commit
is therefore indistinguishable from the statement never having run, so
[Statement Time Budget](#statement-time-budget), rule 3, holds of a statement
that is **killed** exactly as it holds of one that is merely cut. **This is an
availability defect and not a durability one**, and the distinction is worth
stating because the two call for different remedies.

**The cost is borne by one process now, and the measurements say which.** The
same statement, at the same budget, over the same 600-node store, was measured
against three surfaces while two of them still executed statements in their own
process. Only the third still does:

| | `rmp graph serve` | `rmp web`, before the withdrawal | `rmp graph execute`, withdrawn |
|---|---:|---:|---:|
| baseline resident memory | 18 MB | 23 MB | not applicable |
| peak resident memory | 3618-3734 MB | 3088 MB | 2974-3293 MB |
| resident 130 s later | 1064 MB | **3088 MB, none of it returned** | 0, the process exited |
| the store on disk afterwards | unchanged | unchanged | unchanged |
| what the caller received | the unanswered-server line at 7.5 s, exit 1 | an empty reply after 39.5 s | `utils.ErrGraphEngine`, exit 1 |

**Only the first column measures a surface that can be run, and the other two
are retained as the baselines that figures elsewhere rest on.** A statement
cannot inflate an `rmp web` process, because `rmp web` executes none: it sends
the statement to the server and reads the answer back, so the whole of this cost
falls on `rmp graph serve`. The second and third columns are therefore evidence
and not specification: each was measured on a surface that executes no statement
today, and each is kept because what it measures is still relied on. The
`rmp web` column's 3088 MB — none of it returned in the 130 seconds
observed, because an otherwise idle process triggers no collection — is the figure
`WEB.md § Graph Query Time Budget`, rule 9, quotes for what an unbounded statement
costs a long-lived process. The `rmp graph execute` column is the only measurement
of the one property no long-lived surface has: a short-lived invocation returns
the memory to the operating system by exiting. `rmp graph serve`
released most of its own after roughly 73 seconds and settled at a floor of
1064 MB — **58 times its baseline** — where it stayed for the remainder of the
observation, and it is now the only process where any of this is paid. The web
request in the middle column failed at 39.5 seconds with an empty reply, which
is the 30-second `WriteTimeout` closing the connection while the statement was
still inside the engine call; a web request today does not reach that state,
because the caller's backstop fires at 7.5 seconds
([Server Resolution](#server-resolution), rule 7). The store on disk was
unchanged on all three, and on the server that is a requirement rather than an
accident: an unconditional shutdown checkpoint left the same store at 134 MB,
which is why
[Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process),
rules 4 and 8, conditions the fold.

**Concurrency does not multiply the cost.** One server against N clients sending
the same statement at once, in a memory cgroup:

| Concurrent clients | a cut write, `CREATE ()` | a cut read, `RETURN a` |
|-------------------:|-------------------------:|-----------------------:|
| 1 | 3618 MB | 3143 MB |
| 2 | 5515 MB | not measured |
| 4 | 5102 MB | 10,348 MB |
| 8 | not measured | 16,061 MB |
| 16 | not measured | 16,136 MB |

Writes stop multiplying almost at once — four concurrent cut writes cost less
than two — and reads multiply to a plateau of roughly 16,100 MB that sixteen
clients do not exceed. The plateau is repeatable across runs; its cause is
**not** established and this specification offers none. What follows from the
table is negative and it is the point of publishing it: peak resident memory in
a server is not a connection ceiling multiplied by one statement's cost, and no
arithmetic of that shape may be published here as though it had been measured.

**Three quantities do move peak resident memory. None of them is applied, and
declining each is a decision rather than an oversight.**

1. **The engine's cap on the rows a statement may produce.** It cuts during the
   forward pass, so none of the four accumulators grows past it, and it bounds
   the read path in the same proportion as the write path. Lowering it lowers
   peak resident memory proportionally on every shape measured: at a cap of
   10,000 every write shape measured peaks at 80 MB or less and finishes in
   under 3 seconds. It is the only one of the three that bounds the write path
   by a count of rows rather than by elapsed time, so what it bounds does not
   vary with the speed of the machine or with what else is running on it.
   **Declined, and the grounds for declining it have narrowed to one.** While
   two paths existed, the first ground was coherence: a cap the server carried and
   the direct path did not would have made the same statement pass or fail
   according to whether a server happened to be running, invisibly to whoever
   wrote it. That ground is gone — every statement runs in a server, so a cap set
   there applies to every statement uniformly and nothing about it depends on what
   is running. What remains is the second ground, and it is the one this decision
   now rests on entirely: a cap publishes a ceiling on the rows any statement may
   return, and `MATCH (n) RETURN n` over the largest real knowledge graph on the
   development machine needs 44,906 of them. A cap set above that bounds nothing
   an operator meets, and one set below it refuses an ordinary read.
2. **The Go runtime's soft memory limit.** It is a real lever on the read path:
   eight concurrent served reads fall from 16,149 MB to 3429 MB with it set to
   1 GiB. **Declined**, on three measured costs. On the write path it buys
   memory with availability — the same cut write's hold rises from 35.6 to
   172.8 seconds, 4.9 times — which makes the defect
   [Lock Contention](#lock-contention) records worse rather than repairing it.
   It is a soft limit and behaves like one: below the live set the collector
   runs continuously and the process still exceeds the limit it was given by
   37% to 167%. And the engine derives two of its own byte budgets from it, so
   setting it silently narrows both: at the pinned version the engine-wide
   result-byte ceiling becomes half of it and the server's inbound decode bound
   an eighth. Those derivations are the engine's and are not restated as values
   here; what the decision rests on is that lowering the limit narrows what a
   caller may run and receive without announcing it. At 1 GiB the inbound bound
   is still far above the maximum query length, so the rule
   [Server Options](#server-options) states is not breached — but the margin it
   protects is consumed silently, and a narrowing of the statement surface is
   not a thing this product does by side effect.
3. **The statement budget itself**, which peak resident memory is linear in, as
   the first table above shows. It is fixed at 5 seconds for reasons that have
   nothing to do with memory, it is one declaration read by all three surfaces
   (see `WEB.md § Graph Query Time Budget`), and lowering it to bound memory
   would narrow every statement every caller may run on every surface at once.
   It is not moved for this.

**What that leaves, stated as the finding it is.** No configuration available to
Groadmap both preserves throughput and bounds peak resident memory. One
statement can still reach 3.3 GB at the budget in force and roughly 20 GB given
time, on every surface, and the two long-lived surfaces do not return it
promptly. The bound has to come from the engine, exactly as the bound on the
hold does (see [Statement Time Budget](#statement-time-budget)), and it has to
be a bound on the **work** a statement performs rather than on the result it
returns: the engine's byte budgets are its only memory-shaped guard and they
measure the materialised result alone, so a write with no `RETURN` produces rows
whose estimated size is zero and passes them untouched. Measured, a 1 MiB
result-byte ceiling cut `MATCH (a),(b),(c) RETURN a.i` in 10 milliseconds at
17.6 MB and did not cut `MATCH (a),(b),(c) CREATE ()` at all, which ran to its
deadline at 2116 MB. Groadmap does not bound this from its own side, and this
specification does not claim it can.

### Lock Contention

**One process takes this lock, so one thing can contend for it: another server.**
`rmp graph serve` takes the graph store's exclusive advisory lock before it opens
the store and holds it until the process stops
([Server Startup](#server-startup), step 2). No caller takes it, because no caller
opens a store ([Server Resolution](#server-resolution)). The whole of the
contention this section governs is therefore between two `rmp graph serve`
processes for the same roadmap.

1. A server that finds the exclusive lock held **waits**, under the bounded
   exponential-backoff policy specified in
   `IMPLEMENTATION.md § Graph Store Concurrency`. It MUST NOT block indefinitely
   and MUST NOT fail on the first collision. The wait carries a budget of its own,
   derived from the statement budget rather than from the fixed total the SQLite
   layer waits; the derivation, the 7.5 seconds it yields at the statement budget
   in force, and the hold it does not cover are stated below.
2. If the lock is still unavailable when that bounded wait is exhausted, the
   server does not start: it fails with `utils.ErrGraphStore` (exit code 1). That
   is the interlock which admits one server per roadmap.
3. **The line an exhausted wait prints MUST NOT claim to know which process holds
   the lock, because nothing records one.** The lock is advisory and carries no
   owner, and the server that failed to take it cannot find out who has it. It
   names the likely holder and does not assert it: another `rmp graph serve` is
   now the only thing that can be holding this lock, so the line says that one
   **may** already be running for the roadmap, which is a cause the reader can act
   on without being told a fact the product does not have.
   `COMMANDS.md § Graph Server Socket Error Lines` publishes the exact line.

**Waiting rather than failing fast is the policy because a restart is the ordinary
case.** A server stopped and immediately started again would otherwise fail on the
outgoing process's tail — its final checkpoint and its store close — and an
operator restarting a server has no way to know how long to pause between the two
commands. The wait removes the pause. It is not a queue for statements: statements
do not take this lock, and concurrency between them is resolved inside the server
by the store's MVCC (see
[Concurrency Inside the Server](#concurrency-inside-the-server)).

**The hold spans the process, so nothing about it is a wait a statement can
shorten.** A server holds the lock across its whole open, service and shutdown
sequence. That hold has no upper bound and is not meant to have one: it lasts as
long as the operator runs the server. A finite wait can never cover it, which is
exactly why no surface but the server is allowed to take the lock at all —
withdrawing the direct path is what removed the class of failure a wait could not
have fixed.

**The wait budget is unchanged in value, and its derivation now rests on the
shutdown tail rather than on a statement.**

```
wait budget = statement budget + backoff total
```

- **Statement budget** is the deadline `WEB.md § Graph Query Time Budget` fixes
  for a caller-supplied statement, 5 seconds. It bounds a statement that is a read
  or that runs to completion, and it is therefore the bound on what an outgoing
  server's drain may lawfully wait for before it cuts (see
  [Server Shutdown and the Drain](#server-shutdown-and-the-drain)).
- **Backoff total** is the worst-case total wait of the project's single retry
  policy, 2500 ms (see `IMPLEMENTATION.md § Retry Logic`), reused here as the
  allowance for the **fixed** part of the tail: the final checkpoint with its log
  truncation, the store close, and scheduling. It is reused rather than replaced
  by a figure of its own so that the project keeps one set of timing numbers. How
  much of the quantity it actually covers depends on the size of the graph, and is
  stated next.

At the 5-second statement budget in force, the wait budget is therefore
**7.5 seconds**.

**The allowance for the fixed part is sized against a quantity that grows with
the graph, and the allowance does not grow with it.** The fixed part is linear in
the store's size on disk, at a rate that depends on the shape of the data.
Measured by phase instrumentation, forcing a write so that the checkpoint
executes:

| Graph | Size on disk | Fixed part |
|-------|-------------:|-----------:|
| this project's own knowledge graph, 701 nodes | 1.3 MB | 50.5 ms |
| a real knowledge graph, 20,665 nodes | 7.1 MB | 268 ms |
| a real knowledge graph, 14,532 nodes | 11 MB | 367 ms |
| the largest real knowledge graph on the development machine, 44,906 nodes | 36 MB | 1286 ms |
| a synthetic graph of uniformly simple nodes, 400,000 nodes | 122 MB | 2784 ms |

The four real knowledge graphs cluster between **33 and 39 ms per megabyte**. The
synthetic graph is markedly cheaper per byte, at 23 ms per megabyte, because its
nodes are simpler and more uniform than a real graph's; there is therefore no
single rate, and the real graphs are the ones the rate must be read from.

**The margin is stated on the largest real graph, where it needs no
extrapolation, and the point at which it runs out is stated too.** At 36 MB the
fixed part is 1286 ms on the ordinary path and 955 ms with no checkpoint behind
it, against the 2500 ms allowance. That is a margin of **1.9x and 2.6x** — a
margin, and not an order of magnitude. The allowance is **exhausted at roughly
70 MB** for graphs shaped like the four real ones measured, and only at roughly
110 MB for the simpler synthetic shape. Past that point a restart against a large
graph can fail rather than wait, and Groadmap does not measure a graph's size, does
not warn on it, and does not refuse to open a store above it. This is a known limit
of the sizing rule, stated here rather than hidden. The consequence is now a failed
start that an operator retries, rather than a statement that fails: the class of
caller the old limit starved no longer exists.

**One residual remains, and it is the sum of the two terms rather than either of
them.** The wait budget is derived as the drain's bound plus the fixed tail, but
an incoming server does not begin waiting at the end of the outgoing server's
drain — it begins waiting whenever the operator starts it, which may be before the
outgoing server has received its signal at all. An incoming server that arrives at
the start of a drain that then runs its own full budget waits that budget and the
fixed tail after it, which is more than the wait budget covers, and it fails while
the outgoing server is doing nothing it is not entitled to do. The outcome is
rule 2's: `utils.ErrGraphStore`, exit code 1, cleared by starting the server
again once the previous one has exited. It is recorded here rather than sized
away, because sizing it away means lengthening a wait whose only beneficiary is a
restart.

Groadmap's usage model and expectations:

1. `rmp graph serve` is a long-lived process. It opens the roadmap's store once,
   holds the exclusive lock for its process lifetime, serves statements over its
   socket, folds the write-ahead log on a cadence and at shutdown, and closes both
   the store and the lock when it stops. Every other `rmp` invocation, graph or
   not, is short-lived and takes no graph store lock at all.
2. Two `rmp graph serve` processes against the **same** roadmap contend for this
   lock, and exactly one of them starts. The implementation MUST surface an
   exhausted wait as `utils.ErrGraphStore` (exit code 1) rather than corrupting
   the store or hanging indefinitely. A second server for the same roadmap on a
   different `--socket` is refused by this lock and not by the socket probe, which
   is why the order of [Server Startup](#server-startup) puts the lock first.
3. Servers for **different** roadmaps never contend, since each roadmap has its
   own graph directory and its own lock file. `rmp graph client` invocations and
   web graph requests never contend with anything on this lock, in either
   direction, because they never take it: two clients against one server are
   resolved inside that server by MVCC, and a client against a roadmap with no
   server is a failure rather than a wait.
4. Recovery on open restores the last committed state from the snapshot and the
   write-ahead-log tail, and it runs once per server rather than once per
   statement. Because the server folds the log on a cadence and at shutdown,
   recovery genuinely exercises the snapshot path: a graph opened after a previous
   server ran is rebuilt from that snapshot plus any log entries written since the
   last fold, rather than by replaying the entire write history. The restored state
   includes deletions: a node deleted by a previous server stays deleted after the
   store is reopened, because the snapshot records the tombstone set and recovery
   reconstructs it. A graph left in a consistent committed state by a previous
   server opens cleanly. A graph whose store is corrupt or unreadable surfaces as
   `utils.ErrGraphStore` (exit code 1) at server startup; there is no automatic
   graph-store repair in this first version.
5. The graph store is independent of the SQLite layer and the SQLite WAL model
   described in `IMPLEMENTATION.md § Concurrency Model`; the two persistence
   mechanisms do not share connections, locks, or transactions.

## Constraints

1. The graph is free-form. Groadmap MUST NOT impose, validate, or auto-create a
   node/edge schema. The conventions in
   [Multi-Layer Modelling Conventions](#multi-layer-modelling-conventions) are
   recommendations only. A caller may declare indexes and constraints of its own
   through `graph client` (see [Schema Management](#schema-management)). That does
   not weaken this constraint: Groadmap declares no schema object, requires none,
   creates none implicitly, and assumes nothing about what any graph's schema
   holds. Every schema object in a knowledge graph is one its owner asked for.
2. The graph is independent of the SQLite tasks/sprints data in this version.
   Graph operations MUST NOT read from or write to `project.db`, and roadmap data
   operations MUST NOT read from or write to `graph/`.
3. Node identifiers are `string` and edge weights are `float64`, as fixed by
   GoGraph's parameterisation. Groadmap does not override these.
4. Graph operations require the `-r` / `--roadmap` flag, identical to `task` and
   `sprint` operations.
5. The graph feature MUST NOT introduce a new exit code. It carries three
   sentinels of its own — `utils.ErrGraphEngine`, `utils.ErrGraphStore` and
   `utils.ErrGraphServer` — and all three map to exit code 1, which is the code
   every condition they cover already returned. A sentinel exists to name, in the
   line the caller reads, which part of the graph subsystem failed and therefore
   which action to take; it never changes the code the process exits with (see
   [Error Handling and Exit Codes](#error-handling-and-exit-codes), rule 4, and
   `ARCHITECTURE.md § Sentinel Error Catalogue`).
6. GoGraph is pinned to an exact version in `go.mod` (see
   [Dependency Maturity Risk](#dependency-maturity-risk)).

## Acceptance Criteria

**Every criterion below that runs a Cypher statement runs it through
`rmp graph client` against an `rmp graph serve` started for that roadmap, because
that is the only way a statement reaches a graph.** Where a criterion turns on the
server being absent, it says so. Where it turns on the store's state before or
after a statement, the server is stopped before the store is inspected, so that
what is compared is a store no process holds open.

1. `rmp graph client -r <roadmap> --query "CREATE (s:Spec {key:'user-authentication'})"`
   creates the node, persists it, prints `{"ok": true}` (the statement has no
   `RETURN` clause), and exits 0. The same statement with `... RETURN s` appended
   instead returns the created node in the `columns`/`rows` shape
   (see `DATA_FORMATS.md § Graph Write Result`).
2. `rmp graph client -r <roadmap> --query "MATCH (s:Spec) RETURN s.key"` returns
   the previously created node's `key` as JSON in the shape defined in
   `DATA_FORMATS.md § Graph Query Result`, and exits 0.
3. A statement is read back correctly in a **separate** invocation, proving the
   graph persisted to `~/.roadmaps/<roadmap>/graph/` across process exits.
4. **One subcommand runs every class of statement, and the criterion MUST assert
   all four together.** Against one roadmap and in this order, each exiting 0:
   `CREATE (n:Spec {key:'k'})`, then
   `MATCH (n:Spec {key:'k'}) SET n.status = 'implemented'`, then
   `CREATE INDEX spec_key FOR (n:Spec) ON (n.key)`, then
   `MATCH (n:Spec {key:'k'}) DETACH DELETE n`. Each is submitted to
   `rmp graph client`, and a read-back after each confirms the effect. An
   implementation that refused any one of them on the ground of what it does fails
   this criterion.
5. **`serve` and `client` are the only subcommand names `rmp graph` resolves.**
   Each of `rmp graph execute`, `rmp graph create`, `rmp graph query`,
   `rmp graph update`, `rmp graph delete`, and `rmp graph search`, invoked with an
   otherwise valid `-r` and `--query`, is an unresolved subcommand name: it exits
   `127`, writes zero bytes to stdout, and writes the dispatch-failure error and
   the `graph` help to stderr (see
   `COMMANDS.md § Dispatch Failures (Unresolved Command or Subcommand Names)`).
   `execute` is named first because it is the one of the six that used to resolve,
   so it is the one an agent is likeliest to still have in memory; it takes no
   alias and no deprecation path.
   The criterion MUST assert the exit code **and** that the statement did not
   run — the graph is byte-identical afterwards — because an alias that quietly
   executed would otherwise pass an exit-code-only check on the success path.
6. `echo "MATCH (n) RETURN count(n)" | rmp graph client -r <roadmap>` reads the
   statement from standard input and returns the count, exits 0.
7. `rmp graph client -r <roadmap>` with no `--query` and no piped standard input
   fails with exit code 2 (no query supplied).
8. `rmp graph client -r <roadmap> --query "MATCH p=(a)-[*1..3]-(b) RETURN p"`
   executes a variable-length traversal and returns results, exits 0.
9. `rmp graph client -r missing-roadmap --query "MATCH (n) RETURN n"` against a
   non-existent roadmap fails with exit code 4.
10. A syntactically invalid Cypher statement fails at execution with exit code 1
    and a plain-text engine diagnostic on stderr. The exit code is what
    distinguishes an engine failure from the two refusals Groadmap owns: 1, and
    not the 2 of a missing query or the 6 of an over-long one.
11. `graph serve` and `graph client` are represented in the AI Agent Contract
    emitted by `rmp graph --ai-help` and `rmp --ai-help`, with the same fields as
    every other subcommand, and no entry remains for any of the six removed names,
    `execute` among them (see `DATA_FORMATS.md § AI Agent Contract`). The
    criterion MUST assert the absence as well as the presence, because a contract
    that still advertised `execute` would send an agent at a subcommand that exits
    `127`.
12. The graph directory `~/.roadmaps/<roadmap>/graph/` is created with `0700`
    permissions on first graph use.
13. After a successful `rmp graph client -r <roadmap> --query "CREATE ..."`, the
    snapshot manifest `~/.roadmaps/<roadmap>/graph/snapshot/manifest.json` exists,
    proving a checkpoint ran (see
    [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)).
14. After a statement that wrote, and its checkpoint, the write-ahead log
    `~/.roadmaps/<roadmap>/graph/wal` is truncated (small or empty), proving the
    log was bounded rather than left to grow with history.
15. After a statement that wrote and its checkpoint, a subsequent read in a
    **separate** invocation returns the written data, proving recovery from the
    snapshot plus any log tail works across process exits.
16. When the checkpoint fails after the transaction has already committed
    durably, the invocation still returns its normal success output (the
    `RETURN`-mirroring shape or `{"ok": true}`) and exit code 0, and the checkpoint
    failure is reported as a diagnostic on stderr without changing the exit code
    (see [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)).
17. **A statement that writes nothing leaves the store's data untouched, and the
    criterion MUST be run against a store whose write-ahead log is not empty.**
    After `rmp graph client -r <roadmap> --query "MATCH (n) RETURN count(n)"`, the
    `wal` file is byte for byte identical to what it was before, and every file
    under `snapshot/` is unchanged, proving that a transaction which appended
    nothing neither checkpointed nor truncated the log. An implementation that
    checkpointed unconditionally fails this criterion, and it fails it in the way
    that matters: it would rewrite a full snapshot on every statement (see
    [What a Statement That Writes Nothing Changes on Disk](#what-a-statement-that-writes-nothing-changes-on-disk)).
18. A statement completes an interrupted checkpoint, and this is expected
    behaviour rather than a defect. With a stale `snapshot.tmp` staging directory
    present, an invocation removes it. With `snapshot/` absent while
    `snapshot.bak/` carries a manifest, an invocation promotes the backup to
    `snapshot/`. In both cases the statement still returns the correct result and
    exits 0.
19. Two concurrent invocations against the same roadmap serialise on the exclusive
    lock, and neither fails on the first collision: the second **waits** and then
    succeeds once the first releases the lock. This holds whether or not either
    statement writes, which is what fixes the single lock mode; an implementation
    that let two non-writing statements overlap fails this criterion.
20. **A server that cannot take the lock within the bounded wait fails rather
    than hanging, and no caller ever meets that wait.** A second
    `rmp graph serve` against a roadmap an incumbent is already serving exits 1
    with a plain-text diagnostic on stderr, and does not block indefinitely. The
    criterion MUST also assert the other half: against that same incumbent, no
    `rmp graph client` invocation and no web graph data request produces a
    lock-related failure at all, because neither takes the lock (see
    [Lock Contention](#lock-contention), rules 2 and 3).
21. `rmp graph client -r <roadmap> --query "MATCH (a:Spec), (b:Code) RETURN a.key, b.path"`
    runs a disconnected multi-pattern `MATCH` and surfaces a Cartesian-product
    notification on stderr (a plain-text line carrying at least the severity, the
    stable code, and the description). The stdout JSON is exactly the normal
    `columns`/`rows` result, unchanged by the notification, and the exit code is 0
    (see [Query Notifications as Diagnostics](#query-notifications-as-diagnostics)).
22. `rmp graph client -r <roadmap> --query "MATCH (s:Spec) RETURN s.key"`, a
    statement that produces no notifications, writes nothing extra to stderr:
    stderr is empty on success, while stdout carries the normal result and the exit
    code is 0.
23. A statement longer than the maximum is refused, and the read that refuses it
    is bounded. A producer that offers `rmp graph client -r <roadmap>` far more
    than 1 MiB on standard input, with `--query` absent, sees the command exit 6
    with `Error: validation error: query exceeds maximum length of 1048576 bytes`
    on stderr while it is still writing: the pipe breaks after the producer has
    managed to send only a small fraction of what it offered, which is what
    bounds the command's peak memory. Stdout is empty and the graph is unchanged.
    The refusal is the length check's own, not the engine's: the exit code is 6
    and not the 1 an engine parse failure carries, and the message is the one
    above rather than an engine diagnostic. A legitimate statement of several
    hundred kilobytes, supplied the same way, still executes normally and exits 0,
    so the bound refuses only what the maximum forbids. Lowering the maximum below
    what ordinary work needs, or restoring a read that drains whatever it is
    offered, MUST fail this criterion (see
    [Maximum Query Length](#maximum-query-length) and
    [Bounded Standard-Input Read](#bounded-standard-input-read)).
24. An invocation that supplies no statement fails at once instead of blocking.
    `rmp graph client -r <roadmap>` with `--query` absent fails with exit code 2
    and `Error: required parameter missing: no query supplied` on stderr in each
    of the three cases the rule names: standard input at end of stream, standard
    input carrying only whitespace, and standard input connected to a terminal.
    The terminal case is the one that regressed into a hang, and it is asserted
    on wall-clock time: the process exits without waiting for input, rather than
    sitting there until something kills it. Criterion 7 fixes the exit code for
    the first case; this criterion fixes the message, all three cases, and the
    requirement that none of them waits (see
    [Standard Input That Supplies No Query](#standard-input-that-supplies-no-query)).
25. `graph client` refuses a positional argument.
    `rmp graph client -r <roadmap> --query "<cypher>" stray`
    exits 2, writes zero bytes to stdout, and writes to stderr the line
    `Error: invalid input: unexpected argument "stray" (graph queries use --query or stdin)`.
    The criterion MUST compare the whole line, the parenthetical included.
26. The classification of a `-`-prefixed token is asserted in both directions. A
    stray `-1` and a stray bare `-` each exit 2 and are reported as an
    **unexpected argument**, while a stray `--foo` exits 2 and is reported as an
    **unknown flag**. Of several stray tokens only the first is named: an
    invocation carrying `alpha beta` names `alpha`, and its stderr does not
    contain `beta`.
27. The refusal precedes every other check the subcommand performs, and roadmap
    selection precedes the refusal. Measured against the built binary:
    `rmp graph client stray -r <a roadmap that does not exist> --query "<cypher>"`
    exits 2 and not 4; the same invocation with no `-r` and no roadmap selected
    exits 3; and `rmp graph client -r <roadmap> stray` with a producer still
    writing to standard input exits 2 at once, reads nothing, and leaves the
    producer to observe a broken pipe. In every case stdout is empty, stderr
    carries the error line and the AI-agent hint and no help body, and the
    roadmap's `graph/` directory — its snapshot directory and its write-ahead
    log — is byte-identical before and after.
28. The rule is one rule across the two families that publish it. The line
    `graph client` emits is the line `COMMANDS.md § Positional Arguments`
    publishes for the whole CLI with this family's hint appended, and the line the
    comment subcommands emit is that same line without a hint
    (`COMMANDS.md § Comment Positional Argument Contract`). A test that asserts one
    family's wording MUST cite the other's, so that a change to either is made
    deliberately rather than by copying.
29. The specification and the implementation name the same engine constructor for
    every path. A regression test enumerates every Cypher engine the
    implementation constructs to serve a `graph` subcommand or a web graph
    request, and fails if any of them is constructed through a constructor other
    than the one [Engine Constructor by Path](#engine-constructor-by-path) gives
    for that path, or if an engine is constructed on a path that table does not
    list. This is what stops the table and the code from drifting apart again.
30. **A write submitted to the web graph data endpoint persists, and the response
    status alone does not establish this criterion.** A `GET` of
    `/roadmaps/<roadmap>/graph/data` whose `q` parameter carries
    `CREATE (n:WebProbe {key:'p'})` is answered HTTP `200`, and a subsequent
    `rmp graph client` in a separate process reports the `WebProbe` node present.
    The read-back is what the criterion turns on: an endpoint constructed without a
    transactional store answers the identical request with the identical `200` and
    stores nothing, so the status is exactly what the defect returns (see
    [Engine Constructor by Path](#engine-constructor-by-path) and
    `WEB.md § Graph Data Endpoint`).
31. The key-uniqueness convention is stated and its violation is detectable. On a
    graph seeded with two nodes whose `key` values are equal under NFC and
    different in bytes — a precomposed `U+00C9` against the decomposed
    `U+0045 U+0301`, for example — each node's stored `key` is byte-for-byte the
    value supplied, `MATCH` with either spelling binds exactly that one node and
    never both, and the byte-wise duplicate audit reports the two as separate
    single-count rows. The two-step audit of
    [Auditing the convention](#auditing-the-convention) reports the pair: step 1
    runs under `rmp graph client` and exits 0, and step 2 groups its rows by NFC
    form and names the group holding both nodes. The same audit reports nothing on
    a graph whose keys are all distinct under NFC.
32. `graph client` runs the schema statements, and each returns the shape the
    specification gives it.
    `rmp graph client -r <roadmap> --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"`
    prints `{"ok": true}` and exits 0.
    `rmp graph client -r <roadmap> --query "SHOW INDEXES"` exits 0 and returns the
    listing in the `{columns, rows}` shape — not `{"ok": true}`, although the
    statement carries no `RETURN` clause — with a row whose name is `spec_key`.
    `rmp graph client -r <roadmap> --query "DROP INDEX spec_key"` prints
    `{"ok": true}`, exits 0, and a subsequent `SHOW INDEXES` no longer reports the
    row. The same three hold for
    `CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE`,
    `SHOW CONSTRAINTS`, and `DROP CONSTRAINT spec_key_uq` (see
    [Schema Management](#schema-management)).
33. A schema change survives the checkpoint and a reopen, and this is the
    criterion the destroyed-schema defect fails. Because a statement that writes
    checkpoints, the `CREATE INDEX` of criterion 32 truncates the write-ahead log
    and rewrites the snapshot before the process exits. In a **separate**
    invocation afterwards, `rmp graph client -r <roadmap> --query "SHOW INDEXES"`
    still reports `spec_key`. Asserting this inside the creating invocation does
    **not** establish the criterion and MUST NOT be the only assertion: an
    implementation whose snapshot carries no schema at all passes that check and
    loses the index at the process boundary. For a constraint the criterion MUST
    additionally assert **enforcement** after the reopen, because a constraint
    that is merely listed is not a constraint that is applied: with
    `spec_key_uq` declared over `Spec.key` and a node already carrying
    `'user-authentication'`, a later `rmp graph client` creating a second node
    with that same key fails, and a read-back reports one such node and not two.
    Executed against an implementation whose checkpoint dropped the constraint,
    that second create exits 0 reporting `{"ok": true}` and the duplicate is
    stored, which is the silent integrity loss this criterion exists to catch (see
    [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)).
34. Every surface reports the schema the store holds, and the surface that cannot
    report it says nothing false about it. Against the store of criterion 32,
    `rmp graph client -r <roadmap> --query "SHOW INDEXES"` reports the row named
    `spec_key` and succeeds. The exit code alone does **not** establish this
    criterion and MUST NOT be the only assertion: an engine constructed without
    the recovered schema answers the identical statement with **zero rows** and
    exits 0, so success is exactly what the defect returns. The rows MUST be
    compared, and the name reported MUST be the one the caller declared rather
    than a name synthesised by the engine. The same statement submitted to the web
    graph data endpoint is answered HTTP `200` with `{"nodes": [], "edges": []}`,
    because the endpoint's response carries nodes and edges and a schema listing is
    neither; it MUST NOT be asserted to report the `spec_key` row (see
    [Recovered Schema on Every Surface](#recovered-schema-on-every-surface)).
35. A declared name is used verbatim and an omitted one is derived, and the drop
    accepts only the name the object actually carries.
    `CREATE INDEX spec_key FOR (n:Spec) ON (n.key)` is reported by `SHOW INDEXES`
    as exactly `spec_key`, with nothing appended.
    `CREATE INDEX FOR (n:Spec) ON (n.title)`, which declares no name, is reported
    as `spec_title_hash`. `DROP INDEX spec_title_hash` then succeeds and exits 0,
    while `DROP INDEX spec_title` fails with exit code 1 and leaves the index in
    place, which is what fixes the derived name as the only one a drop accepts. An
    unnamed constraint is likewise reported under a derived name and dropped by it
    (see [Schema Object Names](#schema-object-names)).
36. Altering an index is two invocations, and the state between them is the
    specified one rather than a defect.
    `rmp graph client --query "DROP INDEX spec_ord"` followed by
    `rmp graph client --query "CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}"`
    each exit 0, and `SHOW INDEXES` afterwards reports `spec_ord` with the changed
    kind. Between the two invocations `SHOW INDEXES` reports the index **absent**,
    and a statement over `Spec.ord` still returns the correct rows, which is what
    establishes that the intermediate state costs speed and not answers. When the
    second invocation fails — a definition the engine refuses, for example — the
    index stays absent, `SHOW INDEXES` reports it absent, and no `rmp` command
    reports the situation or repairs it (see
    [Altering and Recreating an Index](#altering-and-recreating-an-index)).
37. The schema failure classes carry the exit codes this specification gives them,
    and every one of them is 1. Against the store of criterion 32: a second
    `CREATE INDEX spec_key FOR (n:Spec) ON (n.key)` fails with exit code **1** and
    not 6; the same statement written `CREATE INDEX IF NOT EXISTS spec_key ...`
    exits 0 and prints `{"ok": true}`; `DROP INDEX no_such_index` fails with exit
    code **1** and not 6, while `DROP INDEX no_such_index IF EXISTS` exits 0; a
    composite index and an index over a relationship property each fail with exit
    code 1; `CREATE CONSTRAINT ... REQUIRE n.key IS UNIQUE` over a property that
    already holds a repeated value fails with exit code 1, registers nothing, and
    is absent from a subsequent `SHOW CONSTRAINTS`; and
    `CREATE   INDEX spec_key FOR (n:Spec) ON (n.key)`, whose keyword spacing the
    engine does not route to its schema parser, fails with exit code **1**
    carrying a parse diagnostic, creates no index, and leaves the graph's node and
    relationship counts as they were (see
    [Schema Failure Classes](#schema-failure-classes)).
38. **The outcomes this specification declines to check are asserted as the
    specified behaviour, so that a check cannot be reintroduced without a
    deliberate change to this file.** Each of the following exits 0, and the
    criterion MUST assert the observable outcome and not only the exit code:
    - a statement whose raw bytes are not valid UTF-8 executes, and a node created
      by it is stored carrying `U+FFFD` in place of the byte supplied;
    - a `SET` whose right-hand side is a pure-ASCII string literal carrying the
      four-hex-digit Cypher escape for `U+001B` stores a value whose first code
      point after the literal's leading text is a real `U+001B`, read back through
      a subsequent statement;
    - `MATCH (v:Test {key:'…'})<-[e]-(s) SET e.last_commit = 'x'` reports success
      while a read-back through an outgoing pattern reports `last_commit` absent;
    - against a node pair joined in both directions with a **different**
      relationship type each way — the fixture the criterion requires, because a
      pair whose two legs share a type cannot tell a correctly resolved read from
      one that reported the other leg — every one of the following resolves
      correctly, and each MUST be asserted on the rows and not on the exit code:
      `MATCH (s:Spec {key:'…'})-[e]-(x) RETURN type(e)` reports each incident
      relationship's type exactly once, the same multiset the `UNION ALL` of the
      two outgoing legs reports; the incoming spelling
      `MATCH (s:Spec {key:'…'})<-[e]-(x:Test) RETURN type(e), startNode(e).key, endNode(e).key`
      reports the reverse leg's type with the orientation storage holds; a `WHERE`
      over `type(e)` selects the leg it names and returns no row for its sibling; a
      `SET` deriving its value from `type(e)` persists the type the traversal
      bound; and a `DELETE` gated by such a predicate removes that relationship and
      leaves the other in place. The undirected and the incoming spelling are each
      asserted with the far endpoint bound by key and bound by label alone. This
      bullet is the one whose subject is not a hazard (see item 5), and it is
      asserted for the mirror-image reason: it is what fails if the engine stops
      resolving these reads correctly, and equally what fails if a refusal of the
      shape is reintroduced;
    - `CREATE INDEX spec_key FOR (n:Spec) ON (n.key) MATCH (m) SET m.reviewed = true`
      creates the index, prints `{"ok": true}`, and leaves `m.reviewed` absent.
    An implementation that refused any of the five fails this criterion. It is
    stated in this direction — asserting the outcome rather than the absence of a
    check — because an absence cannot be tested and an outcome can (see
    [What Groadmap Does Not Check](#what-groadmap-does-not-check)).
39. **A statement that exhausts the time budget is cut, and the criterion MUST
    assert what it left behind and not only the exit code.** An
    `rmp graph client` whose statement cannot finish inside the budget — an
    unbounded whole-graph traversal, or a multi-way Cartesian product over a graph
    large enough, the two shapes measurement shows the budget cuts — exits **1**,
    writes zero bytes to stdout, and writes to stderr the budget line
    `COMMANDS.md § Graph Management` publishes, which is `rmp`'s own text
    throughout and not an engine diagnostic. Where the statement was a **read**,
    the invocation is asserted on wall-clock time to return shortly after the
    budget rather than running to completion; a cut **write** carries no such
    upper assertion, because its return time is not bounded by the budget (see
    [Statement Time Budget](#statement-time-budget)). Where the cut statement was
    a **write**, the criterion MUST also
    assert that nothing survived: in a **separate** invocation afterwards the
    graph holds none of the elements that statement was creating, `wal` is byte
    for byte what it was before, and every file under `snapshot/` is unchanged,
    proving the transaction rolled back whole and that no checkpoint ran. An
    implementation that classified the engine call's error and not the result's
    passes an exit-code-only check while reporting an ordinary query failure,
    which is the outcome this criterion exists to catch (see
    [Statement Time Budget](#statement-time-budget)).
40. **The budget is one value read by both surfaces, and an ordinary statement
    does not notice it.** A regression test asserts that the deadline
    `rmp graph client` applies is the same declaration the web graph data
    endpoint applies, so the CLI carries no second constant of its own, and
    changing that one declaration changes the wait budget of
    [Lock Contention](#lock-contention) with it. Every other graph criterion in
    this file still passes unchanged: a statement that completes inside the budget
    returns exactly the result its own Cypher produces, with nothing truncated, no
    ordering changed, and no latency added. The budget is observable only to a
    statement that would otherwise have run for longer than it.
41. **A server serves.** `rmp graph serve -r <roadmap>` creates
    `~/.roadmaps/<roadmap>/graph.sock` with mode `0600`, writes the socket path to
    stdout as JSON, and answers a statement sent through
    `rmp graph client -r <roadmap> --query "MATCH (n) RETURN count(n)"` with the
    same JSON `rmp graph client` returns for that statement against the same
    graph. The mode is asserted, not assumed: a socket created at whatever the
    umask yields passes every functional check in this criterion.
42. **A write through the client is durable across the server's own lifetime.**
    `rmp graph client -r <roadmap> --query "CREATE (s:Spec {key:'k'})"` prints
    `{"ok": true}` and exits 0, a subsequent read through the client reports the
    node, and after the server is stopped an `rmp graph client` in a separate
    process still reports it. The last assertion is the one that matters: an
    engine constructed without a write-ahead log behind it passes the first two
    and loses the node at exit (see
    [Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process)).
43. **A second server against the same roadmap is refused and disturbs nothing.**
    With one server running, a second `rmp graph serve -r <roadmap>` exits 1,
    writes zero bytes to stdout, and leaves the first server's socket in place:
    the socket file is still there, still carries mode `0600`, and the first
    server still answers a statement afterwards. The criterion MUST assert the
    incumbent still answers, because a refusal that had already unlinked the
    socket passes an exit-code-only check.
44. **A server relaunches over a stale socket, and loses nothing the killed one
    had acknowledged.** After a known number of commits acknowledged through the
    client, the server is killed outright. The socket file is still present.
    `rmp graph serve -r <roadmap>` against the same roadmap starts, replaces that
    file, and answers; every one of those commits is present in the graph.
45. **Exactly one process opens the store, and the criterion MUST assert the
    negative half.** With a server running for a roadmap,
    `rmp graph client -r <roadmap> --query "CREATE (n:Probe {key:'p'})"` exits 0
    and the node is visible to a second `rmp graph client` against that same
    server. With the server stopped, the identical invocation exits 1 and writes
    zero bytes to stdout, and the roadmap's `graph/` directory is byte-identical
    before and after it — no file created, none modified, no lock file appearing
    where none was. The pair is the assertion: the success alone says nothing
    about which process ran the statement, and the failure alone is satisfied by
    an implementation that fails for any reason at all.
46. **A stale socket is reported as no server listening, is reported promptly, and
    is not removed.** With a socket file present at the derived path and nothing
    listening, `rmp graph client -r <roadmap> --query "MATCH (n) RETURN count(n)"`
    exits 1 and writes the no-server line naming that path. It does so promptly:
    the criterion is asserted on wall-clock time, because a leftover socket must be
    recognised inside the probe rather than waited on. The leftover socket file is
    still present afterwards, because no caller removes one (see
    [Server Resolution](#server-resolution), rule 1).
47. **`graph client` does not fall back.** Against a roadmap with no server
    listening, `rmp graph client -r <roadmap> --query "MATCH (n) RETURN n"` exits
    1, writes zero bytes to stdout, and writes a diagnostic naming the socket path
    it tried. The graph directory is byte-identical afterwards, which is what
    establishes that the subcommand opened no store (see
    [The Bolt Client](#the-bolt-client)).
48. **A serialisation conflict is retried and is not surfaced on its first
    occurrence.** Two clients driven concurrently against the server, each running
    a write transaction over the same nodes, both ultimately succeed and both exit
    0. The criterion MUST also assert the graph afterwards: both writes are
    present. An implementation that surfaced
    `Neo.TransientError.Transaction.Outdated` to the caller instead of retrying
    fails this criterion, and so does one that reported success while discarding
    the losing statement (see
    [Concurrency Inside the Server](#concurrency-inside-the-server) and
    [Server Resolution](#server-resolution), rule 8).
49. **The web graph data endpoint reaches the graph through the client and
    through nothing else, and the criterion turns on a pair.** With a server
    running, a `GET` of `/roadmaps/<roadmap>/graph/data` whose `q` parameter
    carries `CREATE (n:WebProbe {key:'w'})` is answered HTTP `200`, and the node is
    visible through `rmp graph client` against that same server — which establishes
    that the request reached the server rather than a store of its own. With the
    server stopped, the identical request is answered HTTP `503`, the response
    carries no `kind`, and the roadmap's `graph/` directory is byte-identical
    before and after the request. Neither half is the assertion on its own: the
    `200` alone does not say which process ran the statement, and the `503` alone
    is satisfied by an endpoint that answers it unconditionally. The criterion MUST
    assert `503` specifically rather than "a 5xx", because `500` is this endpoint's
    answer to a different condition — a socket path over the platform's bound — and
    the two are deliberately kept apart (see
    `WEB.md § Knowledge Graph from the GoGraph Store`).
50. **The socket does not outlive the server.** After `SIGINT`, `rmp graph serve`
    exits 0 and `~/.roadmaps/<roadmap>/graph.sock` is gone. The store is left in a
    state a subsequent `rmp graph serve` opens cleanly, and the write-ahead log
    is short, proving the shutdown checkpoint ran (see
    [Server Shutdown and the Drain](#server-shutdown-and-the-drain)).
51. **`--socket` lets the CLI follow a server off the derived path, and the
    criterion MUST assert both halves of the boundary.** With
    `rmp graph serve -r <roadmap> --socket <path>` running on a path that is not
    the derived one:
    - `rmp graph client -r <roadmap> --socket <path> --query "CREATE (n:Probe {key:'p'})"`
      exits 0, and the node is visible to a second
      `rmp graph client -r <roadmap> --socket <path>` against that same running
      server;
    - the same `rmp graph client` **without** `--socket` finds nothing on the
      derived path and fails with exit code 1, promptly rather than after any
      wait, leaving the graph unchanged;
    - a `GET` of `/roadmaps/<roadmap>/graph/data` is answered HTTP `503` for the
      same reason, and no request can be made to answer otherwise, because the
      endpoint publishes no socket parameter.
    The second and third bullets are the criterion's point: they fix the residual
    as real and as confined to the surface that cannot carry the flag (see
    [Serving on a Non-Default Socket](#serving-on-a-non-default-socket)).
52. **An empty `--socket` value is refused on every subcommand that publishes the
    flag, and refused before anything is opened.** Each of
    `rmp graph client` and `rmp graph serve`, invoked with
    `--socket ""` and otherwise valid arguments, exits 2 and writes zero bytes to
    stdout. No store is opened, no socket is created or removed, and the roadmap's
    `graph/` directory is byte-identical before and after.
53. **A cut write served by a server leaves the store byte-identical, and the
    criterion MUST compare content rather than size.** A server serves exactly one
    statement that the deadline cuts while it is writing, and is then stopped. Every
    file under the roadmap's `graph/` directory is compared by name, by length and
    by content digest against a fingerprint taken before that statement ran: the
    set of files, their lengths and their digests are all unchanged, and the graph
    still holds exactly the nodes it held. Size alone is the weak reading — a fold
    whose residue happened to be small would pass it while still rewriting the
    snapshot — and the property that actually holds is the stronger one: a shutdown
    that owes no fold writes nothing (see
    [Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process),
    rules 4 and 8). Content rather than modification time, because a fold renames a
    directory into place and timestamps move even for identical bytes. The criterion
    MUST also assert that the statement was **cut** rather than refused — it failed
    no sooner than the deadline it was given, and it did not succeed — because a
    statement that applied nothing exercises none of what this criterion is for.
54. **Every writer against one hot node ultimately succeeds, and the criterion
    MUST drive the shape that used to fail rather than a milder one.** Sixteen
    concurrent clients, each updating the **same** node through
    `rmp graph client` against one running server, all succeed: every invocation
    exits 0, and the node afterwards carries a value one of them wrote. The
    criterion MUST assert the exit code of **every** invocation rather than of a
    sample, because the failure it exists against is a fraction of one percent
    and a sampled assertion would step over it. It MUST also be shown to fail
    when the retry is put back on the fixed ladder, since a criterion that passes
    under both shapes establishes nothing about either (see
    [Concurrency Inside the Server](#concurrency-inside-the-server), rule 4, and
    `IMPLEMENTATION.md § Retry Logic`).
55. **An exhausted retry prints the published contention line, in full.** Driven
    against a server that answers every statement with the serialisation
    conflict, `rmp graph client` exits 1 once the retry policy's total is spent,
    writes zero bytes to stdout, and writes on stderr exactly the line
    `COMMANDS.md § Client Error Cases` publishes, compared character for
    character. The criterion MUST compare the whole line rather than a prefix,
    because the distinction it exists to establish — contention as against an
    invalid statement — is carried entirely by text `rmp` chooses, and it MUST
    assert that the caller does **not** read the `graph query failed: ` line an
    invalid statement produces, which is the confusion this line was published to
    end (see
    [Concurrency Inside the Server](#concurrency-inside-the-server), rule 9).
56. **Every record the server writes to stderr carries the project's canonical
    timestamp, and the criterion MUST include records the engine produced.** With
    the machine's local zone set to a non-zero offset, a server is started,
    exercised and stopped; every record it emitted carries a `time` attribute of
    the form `YYYY-MM-DDTHH:mm:ss.sssZ`, and the two startup warnings of
    [Socket Path and Permissions](#socket-path-and-permissions), rules 5 and 6,
    are among the records checked. The criterion MUST be driven under a non-UTC
    local zone, because under UTC a handler that replaces nothing at all passes
    it; and it MUST assert the **instant** as well as the shape, because a
    handler that reformatted the local reading instead of converting it would
    satisfy the shape while naming an instant hours away (see
    [Server Diagnostics on Stderr](#server-diagnostics-on-stderr)).
57. **The two startup warnings are on stderr before the socket is on stdout.** A
    caller that starts a server, waits for the announcement object on stdout, and
    only then reads stderr finds both warnings there: the one for the absent
    authentication and the one for the absent transport security. The criterion
    MUST read stderr the way a caller reads it, a complete line at a time, because
    the defect it exists against left bytes on the stream but no complete line at
    the moment of the announcement — so a check for readable bytes would have
    passed while a line-oriented reader saw nothing (see
    [Server Startup](#server-startup), step 7).
58. **A server whose stderr has stopped being read keeps serving, still stops on a
    signal, and accounts for what it lost.** Driven with nothing draining its
    stderr and with enough records written to overflow the queue, the server
    answers every statement it is sent, and `SIGINT` or `SIGTERM` still stops it.
    Once the destination accepts writes again, the criterion MUST assert the
    invariant rather than a literal count: every record written either arrived
    whole or was counted in a dropped-record report, and the records that arrived
    are in the order they were written. A literal count would assert a scheduling
    accident; the invariant fails on a count that is short, on one that is
    inflated, and on a record that vanished without being counted at all (see
    [Server Diagnostics on Stderr](#server-diagnostics-on-stderr)).
59. **A prefixed statement returns its plan, an unprefixed one is untouched, and
    the criterion MUST assert all three parts together.** Against one roadmap:
    `EXPLAIN MATCH (n:Spec) RETURN n.key` returns the statement's declared
    columns, an empty `rows` array, and a `plan` object with no `profile` key,
    and creates, changes and deletes nothing; `PROFILE MATCH (n:Spec) RETURN n.key`
    returns the same columns, the statement's real rows, and a `profile` object
    with no `plan` key; and the same statement with no prefix returns bytes
    identical to those it returned before the prefixes were published, carrying
    neither key. The third part is the one that cannot be dropped: it is the
    guarantee an existing consumer depends on, and it is the part a change to the
    result envelope breaks silently (see
    [Query Plans: The EXPLAIN and PROFILE Prefixes](#query-plans-the-explain-and-profile-prefixes),
    rules 7 to 9).
60. **Two runs of one prefixed statement publish the same bytes, and the
    criterion MUST compare them — everything except the clock.** With a server
    serving the roadmap and one graph unchanged between them, `rmp graph client`
    is invoked twice for one `EXPLAIN` statement and the two stdout streams are
    byte for byte the same, plan included, and the criterion MUST compare complete
    stdout: the identity is over every figure in the tree and over the order the
    members are written in, and a check that merely finds a plan in both streams
    passes on two trees that disagree about both. For a `PROFILE` statement the
    criterion MUST compare complete stdout **with every `timeNs` excluded**, and
    MUST NOT compare the durations themselves. That key measures the execution
    rather than the result, two invocations are two executions, and a criterion
    that compared their clocks would fail whenever two correct measurements
    disagreed — a flaky test asserting a requirement the specification does not
    make. It MUST still assert that `timeNs` is **present** in both streams
    wherever the shape requires it, so that excluding the value does not quietly
    excuse a missing key (see `DATA_FORMATS.md § Graph Client Result`, rule 5).
61. **A write publishes its counters, a read publishes none, and the criterion
    MUST assert both halves.** Against one roadmap:
    `CREATE (:Widget {serial:'A-1', batch:7})` returns `{"ok": true}` carrying a
    `counters` object of exactly `nodesCreated` 1, `propertiesWritten` 2 and
    `labelsAdded` 1 — three members and no fourth, so the criterion fails on a
    zero that was published rather than omitted; and `MATCH (w:Widget) RETURN
    w.serial` returns its `columns` and `rows` with **no `counters` key at
    all**, in bytes identical to those it returned before the member was
    published. The second half is the one that cannot be dropped: it is the
    guarantee every existing consumer depends on, and it is the part an
    over-eager implementation breaks silently by publishing an empty object
    (see [Write Counters: What a Statement Changed](#write-counters-what-a-statement-changed)
    and `DATA_FORMATS.md § Graph Query Counters`).
62. **A write that applied nothing is distinguishable from one that applied
    something, and that is what the member is for.** Re-running a `MERGE` that
    matches the element it matched before returns `{"ok": true}` with no
    `counters` key, while its first run returned one; a `DELETE` whose pattern
    matches no row does the same. The criterion MUST compare the two runs of the
    **same statement** rather than two different statements, because it is the
    difference between them that a caller reads, and MUST also assert that a
    `DETACH DELETE` of a connected node reports `nodesDeleted` and
    `relationshipsDeleted` and no property figure — a deletion counts no property
    removal.
63. **`propertiesWritten` is the sum of both property effects, and the criterion
    MUST drive a statement that produces both.** With a server serving the
    roadmap, a writing statement that assigns one property and removes another in
    a single pass publishes a `propertiesWritten` equal to the two effects
    together, not to the assignment alone. The criterion MUST use such a statement
    rather than a pure `SET`, because a pure `SET` passes on an implementation
    that publishes the assignment the protocol names and drops the removal it
    cannot name. Nothing is excluded from the assertion: unlike the plan of
    criterion 60, this object carries no clock, because every member of it
    describes what the statement did to the graph rather than how long the run
    took (`DATA_FORMATS.md § Graph Client Result`, rule 6).

64. **A path one byte over the bound is refused and a path exactly at the bound
    serves, and the criterion MUST assert both halves.** Against one roadmap,
    `rmp graph serve --socket <path>` is invoked twice: once with a path whose
    length is exactly the platform's bound, which binds the socket, announces it
    on stdout, answers a statement sent to it, and stops cleanly on `SIGINT`; and
    once with a path one byte longer, which exits 1, writes zero bytes to stdout,
    and leaves no file at that path. The criterion MUST construct both lengths
    from the bound it measured rather than from a literal, and it MUST keep the
    at-the-bound half: a check of the refusal alone passes on an implementation
    that is one byte too strict, and such an implementation refuses a path the
    kernel accepts on every platform at once.
65. **The refusal names the length and the limit, and the criterion MUST assert
    both numbers separately.** The line the over-long invocation writes to stderr
    reports the resolved path, the number of bytes that path occupies, and the
    number of bytes the platform allows, and the invocation exits 1. The criterion
    MUST assert that the length reported is the length of the path it supplied
    **and** that the limit reported is the bound it measured, as two distinct
    checks, because a message that printed the limit in both places — or the
    path's length in both — would satisfy a check that merely found two numbers.
    It MUST also assert that the operating system's own text is absent: an
    `invalid argument` in that line is the defect the criterion exists against.
66. **The derived default path is validated on the same rule, and every surface
    refuses it.** Against a roadmap whose derived path
    `~/.roadmaps/<name>/graph.sock` is longer than the bound, `rmp graph serve`
    invoked with **no `--socket` flag at all** exits 1 with the same line, naming
    the derived path. In that same state `rmp graph client`, invoked with no
    `--socket` flag either, exits 1 with that same line as well, writes nothing to
    stdout and does not open the store; and a web graph data request for the same
    roadmap is refused with HTTP `500`, which
    `WEB.md § Acceptance Criteria`, criterion 160, is canonical for. The criterion
    MUST assert all three, because the value of the rule is that it binds every
    surface alike: an implementation that checked only the surface which binds the
    socket would pass a check of the server alone while leaving a caller to
    resolve a path no socket can occupy and hand back the kernel's own errno. It
    MUST also assert the recovery, in the same state and against the same roadmap:
    `rmp graph serve --socket <path>` with a path inside the bound starts a
    server, and `rmp graph client --socket <path>` pointed at it returns the
    statement's result and exits 0. Without that half the criterion is satisfied by
    an implementation that refuses the roadmap's graph unconditionally, which is
    not what the rule says (see [Socket Path Length](#socket-path-length), rules 5
    and 6).
67. **The limit is derived from the platform, and the criterion MUST be capable of
    failing a hard-coded one.** The criterion MUST establish the bound
    empirically — by binding real sockets at increasing path lengths until one is
    refused — and MUST compare that measured figure against the figure the refusal
    line reports. A criterion that asserted the literal 107 would confirm the
    defect on the three operating systems whose bound is 103, and would confirm it
    silently, because it would then agree with an implementation that is wrong
    everywhere the criterion does not run. The measured comparison fails against
    any hard-coded value on at least one supported platform, and against a correct
    derivation on none.

68. **An over-long field is distinguishable from an invalid statement, and the
    criterion MUST assert the distinction in both directions.** Against one
    roadmap, a statement writing a label one byte over the engine's limit, and a
    statement writing a property key one byte over it, each exit 1 and write a
    line carrying the field-length prefix `COMMANDS.md § Graph Management`
    publishes; the same two statements at exactly the limit exit 0 and their
    elements are found by a following `MATCH`. The criterion MUST derive both
    lengths from the maximum the refusal line itself reports rather than from a
    literal, for the reason criterion 67 gives: the limit is the engine's, a
    version bump may move it, and a criterion pinned to a literal would confirm a
    stale figure instead of failing on it. It MUST also assert that a statement
    with a genuine syntax error still writes the parse-or-execution line, because
    an implementation that routed every engine failure to the new line would
    otherwise pass a check that only looked for the new one.
69. **The refused statement leaves nothing behind, and the criterion MUST assert
    the partial-write half.** The statement it refuses MUST both create a
    well-formed element and write the over-long field, in that order and in one
    pass. After the refusal, a `MATCH` for that element returns no row; an
    ordinary write then returns `{"ok": true}` and a following `MATCH` counts it.
    All three MUST be asserted. A criterion that checked only the write that
    follows would pass on an implementation that committed the statement's
    well-formed prefix and refused only its tail, which is the failure this
    criterion exists against.
70. **The property-value bound and the checkpoint condition are covered without a
    field of that size, and the criterion MUST NOT attempt one.** A field of a
    gigabyte cannot be carried by a statement inside the maximum query length and
    cannot be materialised by a regression test at all, so no criterion may try:
    one that did would measure the machine. What is asserted instead is the
    classification, driven with a fabricated error that wraps the engine's
    snapshot sentinel, and what it asserts is the content
    [Field Length Limits](#field-length-limits), rule 9, requires — that the
    diagnostic states the commits are durable, that the log was not folded, that
    the condition persists through every later checkpoint while the field
    remains, and what to remove. A criterion that asserted only that some
    checkpoint diagnostic was emitted would pass on the general one, which is the
    defect: the general one tells an operator to wait for a reconciliation that
    will never come.
71. **The healthy path stays silent, and the criterion MUST assert the whole
    stream rather than search it for one phrase.** A `graph client` that writes
    an ordinary element exits 0 and writes **zero bytes** to stderr, for a
    statement the engine raises no notification for — the criterion MUST choose
    such a statement, because a notification is the engine's to raise and shares
    that stream
    ([Query Notifications as Diagnostics](#query-notifications-as-diagnostics),
    rule 5). A graph server that starts, serves a writing statement and stops
    cleanly on `SIGINT` writes its two startup warnings and nothing further, with
    no checkpoint record of any kind among them. The criterion MUST compare the
    complete stream in both halves, because an implementation that emitted the
    new diagnostic speculatively — on every checkpoint, or on every checkpoint
    that returned any error at all — would satisfy a check that merely searched
    for the absence of one phrase, while making rule 9 meaningless by announcing
    a permanent condition that does not hold.
72. **A roadmap with no graph is brought into being by starting a server, and the
    criterion MUST drive the whole first-use path rather than the creation
    alone.** Against a roadmap whose home directory holds no `graph/` subdirectory,
    `rmp graph serve -r <roadmap>` starts, announces its socket, and creates
    `~/.roadmaps/<roadmap>/graph/` with mode `0700`. With that server running,
    `rmp graph client -r <roadmap> --query "MATCH (n) RETURN count(n)"` exits 0 and
    returns a count of zero — a read against a graph that has just been created is
    not an error — and a following
    `rmp graph client -r <roadmap> --query "CREATE (s:Spec {key:'first'})"` exits 0.
    After the server is stopped, a second `rmp graph serve` for the same roadmap
    starts over the store the first one left, and a client against it finds the
    node. The criterion MUST assert every step: an implementation that created the
    directory but refused to serve an empty graph, and one that served the empty
    graph but lost the first write, both pass a check that stops at the mode bits
    (see [Server Startup](#server-startup), step 1, and
    [Persistence Layout](#persistence-layout), rule 2).
73. **A server that cannot start creates nothing, and the criterion MUST use a
    refusal that would otherwise have reached the creation.** Against a roadmap
    that does not exist, and again against an existing roadmap whose resolved
    socket path is over the platform's bound, `rmp graph serve` exits non-zero and
    `~/.roadmaps/<roadmap>/graph/` does not exist afterwards. The second half is
    the one that matters: the roadmap is real, so only the ordering of
    [Server Startup](#server-startup), step 1 — both refusals before the creation —
    keeps the directory from being made for a server that then refuses to run. An
    implementation that created the directory first and refused afterwards would
    leave a graph store behind for a roadmap that can never be served.
74. **`rmp graph` publishes two subcommands and the help says so.** `rmp graph`
    with no subcommand, and `rmp graph --help`, each list exactly `serve` and
    `client`, name neither `execute` nor any of the five names withdrawn before it,
    and are byte-identical to one another. The criterion MUST compare the complete
    listing rather than search it for the absence of one word, because a help text
    that still described a statement-running subcommand would pass a check that
    only looked for the string `execute` (see `HELP.md § Graph family help
    specifics`).
75. **No process but `rmp graph serve` opens a graph store, and this is asserted
    against the source rather than against a run.** The whole of production source
    contains exactly one sequence that acquires the graph store's advisory lock and
    opens the store — the one `internal/graphstore` owns — and the only production
    package that reaches it is `internal/graphserve`. `internal/commands` and
    `internal/web` reach a graph only through `internal/graphclient`. A runtime
    criterion cannot establish this, because a surface that opened a store only on
    a path no test drove would pass every runtime check; `internal/testenv` is
    where the project already enforces claims of this shape (see
    [Engine Constructor by Path](#engine-constructor-by-path) and
    `ARCHITECTURE.md § Modules and Responsibilities`).
76. **The web interface never spawns `rmp graph client`, and the criterion is
    structural for the same reason as criterion 75.** The graph data endpoint's
    only route to a graph is a call into `internal/graphclient`, made inside the
    `rmp web` process rather than in a child process running the client command.
    An implementation that shelled out to the binary would satisfy every
    behavioural criterion in this file and every one in `WEB.md`, which is exactly
    why the assertion is on the source (see [The Bolt Client](#the-bolt-client)).
    The scope of that source assertion belongs to the file that owns the web
    package: `WEB.md § Acceptance Criteria`, criterion 162, fixes which files the
    sweep covers, the one exemption it makes for the browser launch that
    `WEB.md § Server Lifecycle`, step 6, requires of `rmp web`, and the three
    distinct ways the sweep MUST fail. It is stated once, in that file, so that
    two statements of it cannot drift apart.
77. **The store lock's exhausted wait is reachable only from `rmp graph serve`,
    and the criterion MUST assert the reachable half and the unreachable half
    together.** With a server running for a roadmap, a second
    `rmp graph serve -r <roadmap> --socket <a different path inside the bound>`
    exits 1 after the bounded wait and writes the store-lock line of
    `COMMANDS.md § Graph Server Socket Error Lines`, which names another
    `rmp graph serve` as the likely holder. Against the same running server, no
    `rmp graph client` invocation and no web graph data request produces any
    lock-related failure at all: each either succeeds or fails on the socket, never
    on the lock. The second half is what establishes that callers no longer take
    this lock; without it the criterion is satisfied by an implementation in which
    both still contend (see [Lock Contention](#lock-contention), rules 2 and 3).
78. **A statement's result is the same at both surfaces, and the criterion MUST
    compare them rather than check each.** Against one running server and one
    graph, the same statement submitted through `rmp graph client` and through the
    web graph data endpoint yields the same values for the same elements. The
    comparison is of the graph content each reports, not of the two response
    bodies, because the two surfaces publish different shapes by design — the CLI
    publishes the `columns`/`rows` object and the endpoint publishes nodes and
    edges (see `DATA_FORMATS.md § Graph Client Result` and
    `DATA_FORMATS.md § Graph View Data`). A criterion that compared the bodies
    would fail on a correct implementation, and one that checked only that each
    answered would pass on two independent engines.


## See Also

- CLI command contract for `graph` → `COMMANDS.md § Graph Management`
- Graph query result JSON and property-type mapping → `DATA_FORMATS.md § Graph Query Result`
- Query plan JSON for a statement written with an `EXPLAIN` or `PROFILE` prefix → `DATA_FORMATS.md § Graph Plan Node`
- The JSON shape of the write counters, their key set, and the rules that omit a zero and omit the block → `DATA_FORMATS.md § Graph Query Counters`
- Standard input as a Cypher source → `DATA_FORMATS.md § Input`
- The sibling standard-input rule for the comment body, whose cap counts characters rather than bytes → `COMMANDS.md § Comment Body Input Source and Precedence`
- GoGraph integration, directory layout, error handling → `ARCHITECTURE.md`
- The required Go version, and the minor-version floor the GoGraph dependency contributes to it → `BUILD.md § Go Toolchain`
- Store serialisation, recovery, server-to-server lock contention, and the checkpoint trade-off → `IMPLEMENTATION.md § Graph Store Concurrency`
- The value of the statement time budget, the evidence for it, and the web graph data endpoint's own handling of a statement it cuts → `WEB.md § Graph Query Time Budget`
- Graph statements submitted through the web interface, the client it reaches them with, and what it answers when no server is running → `WEB.md § Knowledge Graph from the GoGraph Store`
- Help skeleton and AI-help entry for `graph` → `HELP.md`
- CLI contract for `graph serve` and `graph client`, every flag and every failure → `COMMANDS.md § Graph Management`
- The exit codes each of the two subcommands can return, and the packages that implement them → `ARCHITECTURE.md § Exit Codes of the Graph Server and Client`
- The shape of the client's stdout, and the mapping that makes the values it publishes exactly the engine's → `DATA_FORMATS.md § Graph Client Result`
- Where the web endpoint's HTTP answer differs from the client's exit code for the same condition → `WEB.md § Knowledge Graph from the GoGraph Store`
- The timestamp every log record carries, and which output the UTC rule binds → `DATA_FORMATS.md § Dates - ISO 8601 with UTC`
- The sibling long-lived surface's logger, on the same rule and the same handler → `WEB.md § Logger Configuration`
