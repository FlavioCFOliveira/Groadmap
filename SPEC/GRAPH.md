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
- [Error Handling and Exit Codes](#error-handling-and-exit-codes)
- [The Dedicated Graph Server](#the-dedicated-graph-server)
  - [Socket Path and Permissions](#socket-path-and-permissions)
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

The graph is accessed through the `rmp graph` command and its three subcommands.
`execute` accepts Cypher and returns results as JSON. `serve` opens one roadmap's
graph once and serves it over a Unix domain socket for as long as the process
runs, and `client` sends a statement to a running server and prints what comes
back; both are specified in
[The Dedicated Graph Server](#the-dedicated-graph-server). The graph is backed by
the external GoGraph module, which provides a labelled property graph, a Cypher
engine, durable on-disk persistence, and the Bolt version 5 server `serve` runs.

**A running server changes where a statement executes and nothing else.** Against
a roadmap a server is serving, `rmp graph execute` and the web graph data endpoint
send their statement to that server instead of opening the store themselves; with
no server listening they open the store directly, exactly as they always have. The
rule that decides between the two is stated once, in
[Server Resolution](#server-resolution). A statement's result, its output shape,
and its exit code are the same either way.

**`rmp graph execute` runs whatever Cypher it is given, and so does
`rmp graph client`.** Groadmap does not classify a statement, does not route it by
the clauses it contains, and does not refuse it on the ground of what it would
read, write, or delete. A read, a write, a deletion, and a schema change all reach
the engine through the same statement surface and the one execution path. What Groadmap still refuses is stated in
[Error Handling and Exit Codes](#error-handling-and-exit-codes), and what it
deliberately does not examine is stated in
[What Groadmap Does Not Check](#what-groadmap-does-not-check).

**The web graph data endpoint is the second surface onto the same graph, and it
behaves the same way.** It executes the statement it is given, on the same
execution path and under the same store lock, so everything this file says about a
statement holds of one submitted through the web query bar (see
`WEB.md § Graph Data Endpoint`). The one thing that surface does with a statement
which the CLI does not is decide whether to append a node `LIMIT` to it, which is
a decision about the response's size and refuses nothing (see
[Literal-Aware Normalization](#literal-aware-normalization)).

## Functional Requirements

1. `rmp graph` provides three subcommands: `execute`, which runs a Cypher
   statement against the roadmap's graph; `serve`, which serves that graph over a
   Unix domain socket for as long as the process runs; and `client`, which sends a
   statement to a running server. `execute` and `client` each accept any Cypher
   statement the engine accepts and execute it; there is no per-statement
   operation-class check on either, and no fourth subcommand.
2. `graph execute` requires a target roadmap, selected with the shared
   `-r` / `--roadmap` flag (see `COMMANDS.md § Roadmap Selection (Always Required)`).
3. `execute` reads its Cypher from the `-q` / `--query` flag, or from standard
   input when the flag is absent, and never from a positional argument: a query
   written bare on the command line is refused (see
   [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)).
4. The output mirrors what the executed statement returns. A statement that
   produces result columns returns those columns and rows as JSON to stdout, in
   the shape defined in `DATA_FORMATS.md § Graph Query Result`; a statement that
   produces none returns `{"ok": true}` (see
   `DATA_FORMATS.md § Graph Write Result`). The engine reports no
   affected-element count, so the result carries no such field.
5. Every statement runs inside a single transaction on the transactional
   execution path, and a statement that changes the graph persists that change
   durably before the process exits (see
   [Engine Constructor by Path](#engine-constructor-by-path)).
6. After a statement whose transaction committed a change, and before the process
   exits, the implementation MUST produce a self-sufficient on-disk snapshot of
   the committed graph state and truncate the write-ahead log, synchronously
   within the same invocation. This checkpoint bounds write-ahead-log growth and
   keeps recovery cost proportional to the live graph size rather than to the
   total history of writes (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)). A
   statement whose transaction appended nothing to the write-ahead log never
   checkpoints and never truncates the log; what such a statement does change on
   disk is specified in
   [What a Statement That Writes Nothing Changes on Disk](#what-a-statement-that-writes-nothing-changes-on-disk).
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
10. `rmp graph execute` surfaces, on stderr, exactly the advisory notifications
    the engine returns for the statement it ran, as one plain-text diagnostic line
    per notification. Groadmap does not generate notifications; the engine alone
    decides which statements carry them, and Groadmap emits whatever it is given
    (which may be none). Notifications never change the stdout success output or
    the exit code (see
    [Query Notifications as Diagnostics](#query-notifications-as-diagnostics)).
11. `rmp graph execute` is also the surface through which the knowledge graph's
    schema is managed. It accepts the schema-mutating DDL statements
    `CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT` and `DROP CONSTRAINT`, and
    the schema-introspection commands `SHOW INDEX(ES)` and `SHOW CONSTRAINT(S)`,
    because it accepts every statement the engine accepts. What each statement
    does, how a schema object is named, why changing an index is two invocations
    rather than one, and how a schema failure surfaces are specified in
    [Schema Management](#schema-management).
12. `rmp graph execute` executes its statement under a time budget: the same
    budget, carrying the same value, that the web graph data endpoint applies. A
    statement that exhausts it is cancelled, its transaction rolls back whole, no
    checkpoint runs, and the invocation fails with `utils.ErrDatabase` (exit code
    1); no new sentinel error and no new exit code is introduced. What the budget
    does to an invocation is specified in
    [Statement Time Budget](#statement-time-budget), and
    `WEB.md § Graph Query Time Budget` is canonical for the value.
13. `rmp graph serve` opens one roadmap's graph store, holds it for the life of
    the process, and serves it over a Unix domain socket using the engine's own
    Bolt version 5 server. The socket and its permissions, the startup and
    shutdown sequences, the options the server is given, and what the server
    guarantees are specified in
    [The Dedicated Graph Server](#the-dedicated-graph-server).
14. `rmp graph client` sends one statement to a running server over that socket
    and prints the result in the shapes `graph execute` prints. It has no direct
    path: a roadmap with no server listening is a failure for this subcommand and
    never a fall back onto the store (see [The Bolt Client](#the-bolt-client)).
15. `rmp graph execute` and the web graph data endpoint resolve the socket in
    force before they open anything — the path derived from the roadmap, or, on
    `graph execute`, the value of its `--socket` flag. Against a served roadmap the statement goes
    to the server and neither surface takes the store's advisory lock; with no
    server listening both open the store directly under that lock, exactly as they
    do today. The rule, its four states, and the outcome each surface reports for
    each state are specified once in [Server Resolution](#server-resolution).
16. A statement executed through a server produces the same result, the same
    output shape, and the same exit code it produces on the direct path. Which
    path carried a statement is not observable in its result.

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

GoGraph is consumed at the exact tag **v0.12.0**. Because
v0.12.0 is a v0 (pre-1.0) version, it is consumable directly at the bare module path
`github.com/FlavioCFOliveira/GoGraph`, and `go.mod` pins the clean exact tag `v0.12.0`.
This exact-tag pin satisfies the pinning mitigation below directly. The pinned version
is recorded in `BUILD.md § Go Toolchain`.

As a `0.y.z` release, v0.12.0 signals under Semantic Versioning that GoGraph's public
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

3. **A widened Cypher statement surface.** `rmp graph execute` runs whatever the
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
   statement `rmp graph execute` runs becoming unusable through the endpoint, with a
   diagnostic that names the injected clause rather than the cause.

Mitigations required by this specification:

1. Groadmap MUST pin GoGraph to an exact version in `go.mod` (a specific immutable
   reference, not a floating or branch reference), so builds are reproducible. The
   pinned exact tag is recorded in `BUILD.md § Go Toolchain`.
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

The CLI is a short-lived process. For each `rmp graph execute` invocation the
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
instead. The table covers every Cypher engine Groadmap constructs in order to
serve a `graph` subcommand or a web graph request.

| Path | Surface | GoGraph constructor | Transactional store and write-ahead-log writer |
|------|---------|---------------------|------------------------------------------------|
| Transactional | `graph execute` | `cypher.NewEngineWithStoreAndRecovery`, over a transactional store, given the whole recovery result the store open returned | Both are opened: the write-ahead-log writer over `wal`, and the transactional store over the recovered graph and that writer |
| Transactional | The web graph page and the web graph data endpoint (see `WEB.md § Knowledge Graph from the GoGraph Store`) | `cypher.NewEngineWithStoreAndRecovery`, over a transactional store, given the whole recovery result the store open returned | Both are opened: the write-ahead-log writer over `wal`, and the transactional store over the recovered graph and that writer |

There is **one** path, and both surfaces run on it. The two rows name the two
surfaces rather than two constructions: they construct the same engine, in the
same way, from the same recovery result.

**The construction is literally one, in `internal/graphstore`, and every surface
reaches it.** That package owns the graph store's whole lifecycle — the exclusive
advisory hold, the recovery open, the write-ahead-log writer, the transactional
store, the engine over them, and the synchronous checkpoint — and a surface is on
this path by calling it, not by repeating it. A second construction anywhere in
the product is a second path, whatever constructor it names.

`rmp graph serve` reaches that same one construction and builds nothing of its
own. What differs there is how long the sequence is held open and when the
checkpoint runs, not what is constructed, which is why the server needs no
constructor of its own and takes no row of its own in the table above (see
[The Dedicated Graph Server](#the-dedicated-graph-server) and
[Durability and Checkpointing in a Long-Lived Process](#durability-and-checkpointing-in-a-long-lived-process)).

Groadmap constructs an engine through no other constructor. The pinned engine
also exposes `NewEngine`, `NewEngineWithOptions`, `NewEngineWithRegistry`,
`NewEngineWithStore`, `NewEngineWithStoreAndConstraints`, and
`NewEngineWithStoreAndSchema`; Groadmap uses none of the six, and adopting one is
a change to this table before it is a change to the code.

**Why the web surface is on the transactional path too.** The web graph data
endpoint executes caller-supplied Cypher, and it does not examine that Cypher any
more than `graph execute` does, so a statement submitted through the page's query
bar may write, delete, or change the schema (see `WEB.md § Graph Data Endpoint`).
An endpoint constructed without a transactional store would run such a statement
against the request's own in-memory graph, discard it when the request ended, and
answer `200`. The write would be reported as done and would not exist. Putting the
endpoint on the same path as the CLI is what makes the endpoint's answer true.

**Why the constructor takes the whole recovery result rather than the schema
alone.** The declined `NewEngineWithStoreAndSchema` re-registers the same
constraints and the same index definitions, under the same declared names, and
answers every statement identically. What it does not carry is the snapshot's
index payloads, so an engine built through it **rebuilds every index by a full
scan of the recovered graph** each time the store is opened. The constructor this
table gives loads each index from the payload the snapshot already holds, wherever
recovery certifies that safe, and falls back to the same rebuild where it does
not.

**The reason that difference matters here is what `rmp` is, not how large its
graph is.** `rmp` opens the store, runs one statement, and exits, so "each time
the store is opened" means **once per command**, not once per process lifetime as
it would in a long-running server. The rebuild is not amortised over anything.
That is a structural property of the product and it holds at any graph size,
which is why the choice is made on it rather than on a measurement.

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
indistinguishable within the run-to-run spread: `rmp` opens the store, runs one
statement, and exits, so at this size the process start and the store open
dominate whatever the plan chooses. The constructor chosen here is therefore
about what happens as a graph grows, and this specification does not assert that
it makes any command measurably faster today. Should a future change seek a
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

An invocation whose transaction committed a change produces a durable snapshot
and truncates the write-ahead log before the process exits. This step is
synchronous: it runs inside the same short-lived invocation, not in a background
goroutine.

**What decides whether the checkpoint runs is the write-ahead log, not the
statement.** Groadmap does not examine a statement to learn whether it writes, so
it cannot decide in advance. The transaction runs, and the checkpoint follows only
when that transaction appended to the write-ahead log. A statement that appended
nothing never checkpoints and never truncates: the snapshot directory and the log
are left exactly as it found them (see
[What a Statement That Writes Nothing Changes on Disk](#what-a-statement-that-writes-nothing-changes-on-disk)).
Checkpointing unconditionally would rewrite a full snapshot of the whole graph
after every statement, including one that only counted nodes, which is a cost
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
   protect is still there. One successful write is enough to do this, because
   every successful write checkpoints. A snapshot that carries the graph but not
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
2. The graph directory is created on first use of `rmp graph execute` for that
   roadmap, whatever the statement does. A statement that only reads, run against
   a roadmap that has no graph yet, creates an empty graph store and returns an
   empty result; it is not an error.
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
   deliberately, because `graph execute` runs `CREATE CONSTRAINT` like any other
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

**Step 1 — read every key, with `rmp graph execute`.** This statement reads and
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

`rmp graph execute` hands the statement to the engine. Between reading the
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
   returns success, so `rmp graph execute` prints `{"ok": true}` and exits 0 for a
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
one at all (see `WEB.md § Graph Data Endpoint`). `rmp graph execute` takes no such
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

`rmp graph execute` obtains its Cypher from one of two sources:

1. The `-q` / `--query "<cypher>"` flag.
2. Standard input, read under a bound, when the `--query` flag is absent. This
   allows piping a statement, for example
   `cat statement.cypher | rmp graph execute -r myproject`.

Whichever source carries it, a query is subject to the maximum length stated in
[Maximum Query Length](#maximum-query-length) below.

**This section binds `rmp graph client` identically, and is canonical for it
too.** The two subcommands take the statement from the same two sources, in the
same order, under the same bounded read, the same maximum length, the same refusal
of a missing statement, and the same refusal of a positional argument; every rule
below is written of `graph execute` and holds of `graph client` word for word.
There is no `--statement` flag on either. The only thing the two do differently
with a statement is where they send it (see
[The Bolt Client](#the-bolt-client)).

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

Standard input carries the Cypher query itself here: what `graph execute` reads
from it is the instruction it runs, not a value it stores. Other
commands accept standard input as well, and the cross-cutting input rule is
stated in `DATA_FORMATS.md § Input`, which is the canonical statement of every
command that reads standard input: it lists the `--query` of `graph execute`
together with the `--body` of the comment subcommands of the `task` and `sprint`
families.

### No Positional Query: A Stray Token Is Refused

The two sources above are the only two. `graph execute` accepts **no positional
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
sources it does not have. Everything the rules below say of `graph execute` holds
of `graph client` word for word.

The rules are:

1. **Which tokens are positional arguments at all.** This must be settled before
   a token can be called unexpected, because it decides which of two errors a
   `-`-prefixed token draws. Rule 4 of the precedence rules above is canonical
   for the classification: a
   token is flag-like when it begins with `--`, or with a single `-` immediately
   followed by an ASCII letter. Every other token is a positional argument,
   including a `-` followed by a digit or a decimal point (`-1`, `-0.5`) and a
   bare `-`. A flag-like token that `graph execute` does not define is refused as
   an unknown flag, under the CLI-wide wording `COMMANDS.md § Positional Arguments`
   rule 5 publishes; every other stray token is refused by rule 2 below. This is
   the one point on which `graph execute` and the comment subcommands classify
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
   `rmp graph execute -r <roadmap> --query "<cypher>" alpha beta` names `alpha`
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
   **Cypher query**, which only `graph execute` has; the comment subcommands,
   whose body has two sources of its own, publish the canonical line without it,
   and a hint naming `--query` would be false on them. An edit to either family
   must therefore keep the shared part of the line shared and keep this hint
   confined to `graph execute`.

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
interactive terminal is not a source `graph execute` ever expects a statement
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

`rmp graph execute` is how a knowledge graph's schema — its indexes and its
constraints — is managed. This section is canonical for that: which statements the
engine accepts, what each of them does, how a schema object is named, why changing
an index is two invocations rather than one, and how a schema failure reaches the
caller.

**The surface is the engine's own Cypher, not a Groadmap verb.** A schema
statement is written through `--query` or standard input exactly as every other
graph statement is (see
[Cypher Input Source and Precedence](#cypher-input-source-and-precedence)):

```bash
rmp graph execute -r <roadmap> --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"
rmp graph execute -r <roadmap> --query "SHOW INDEXES"
rmp graph execute -r <roadmap> --query "DROP INDEX spec_key"
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
   outside the transaction.** `graph execute` takes the store's exclusive lock,
   opens the transactional store and the write-ahead-log writer, and runs the
   statement through the engine's transactional entry point, exactly as it does
   for every other statement (see
   [Engine Constructor by Path](#engine-constructor-by-path)). The engine itself
   recognises a schema statement there and executes it outside the transaction it
   would otherwise open, because a schema change is not transactional in this
   engine. Groadmap MUST NOT attempt to make one transactional, MUST NOT wrap it
   in a transaction of its own, and MUST NOT report it as one: a schema statement
   that succeeds has taken effect, and there is nothing to roll back it into.
5. **A successful schema-mutating statement checkpoints.** The synchronous
   snapshot and write-ahead-log truncation of
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write) run after
   it, and the snapshot MUST carry the registered schema, for the reason that
   section gives: without it the truncation that follows destroys the definition
   the statement just created.
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
rmp graph execute -r <roadmap> --query "DROP INDEX spec_ord"
rmp graph execute -r <roadmap> --query "CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}"
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
| `CREATE INDEX` or `CREATE CONSTRAINT` whose object already exists, without `IF NOT EXISTS` | The engine | `utils.ErrDatabase` | 1 |
| `DROP INDEX` or `DROP CONSTRAINT` naming an object that does not exist, without `IF EXISTS` | The engine | `utils.ErrDatabase` | 1 |
| A definition the engine does not support — composite, over a relationship property, or a constraint kind it does not implement | The engine | `utils.ErrDatabase` | 1 |
| `CREATE CONSTRAINT` that the data already in the graph does not satisfy | The engine, having validated the data and registered nothing | `utils.ErrDatabase` | 1 |
| A schema statement whose keyword spacing the engine does not route to its schema parser | The engine's general Cypher grammar, as a parse error | `utils.ErrDatabase` | 1 |

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

**Every surface reports the schema the store actually holds.** `rmp graph execute`
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
`rmp graph execute`, which returns the rows.

## Query Notifications as Diagnostics

The Cypher engine may attach **advisory notifications** to a query result.
A notification is computed at parse and plan time and is available as soon as the
query has run; it is informational guidance, not an error. The classic example is
a Cartesian-product warning: a `MATCH` with two or more patterns that share no
variable forces the engine to combine every match of one pattern with every match
of the other, which can be expensive and is usually unintended.

Behaviour:

1. `rmp graph execute` MUST, after the statement has run, surface on stderr
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
   stdout output is exactly the existing success output, unchanged: the
   `columns`/`rows` shape for a statement that produces columns, or
   `{"ok": true}` for one that produces none (see
   `DATA_FORMATS.md § Graph Query Result` and `DATA_FORMATS.md § Graph Write Result`).
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

## Error Handling and Exit Codes

Graph subcommands use the existing sentinel errors and exit-code mapping defined
in `ARCHITECTURE.md § Error Handling` and `ARCHITECTURE.md § Exit Codes`. No new
sentinel is introduced for the graph feature.

| Condition | Sentinel | Exit code |
|-----------|----------|-----------|
| No roadmap selected and none provided via `-r` | `utils.ErrNoRoadmap` | 3 |
| Selected roadmap does not exist | `utils.ErrNotFound` | 4 |
| No query supplied: `--query` absent and standard input empty, whitespace only, or a terminal; or `--query` present with an empty, whitespace-only, or absent value (see [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)) | `utils.ErrRequired` | 2 |
| `graph execute` receives a positional argument, a bare Cypher query included; it accepts none (see [No Positional Query: A Stray Token Is Refused](#no-positional-query-a-stray-token-is-refused)) | `utils.ErrInvalidInput` | 2 |
| Query longer than the maximum query length of 1 MiB, from either source (see [Maximum Query Length](#maximum-query-length)) | `utils.ErrValidation` | 6 |
| Cypher fails to parse or execute in the engine, a schema statement included (see [Schema Failure Classes](#schema-failure-classes)) | `utils.ErrDatabase` | 1 |
| The statement exhausts the statement time budget and is cancelled (see [Statement Time Budget](#statement-time-budget)) | `utils.ErrDatabase` | 1 |
| Every attempt of the client's retry policy loses a serialisation conflict against a graph server (see [Concurrency Inside the Server](#concurrency-inside-the-server), rule 9) | `utils.ErrDatabase` | 1 |
| Graph store cannot be opened, recovered, read, or written (I/O, corruption, lock) | `utils.ErrDatabase` | 1 |
| The roadmap's socket answers but no server can be reached through it, or the connection fails for a reason other than the socket being absent or refusing (see [Server Resolution](#server-resolution)) | `utils.ErrDatabase` | 1 |
| The connection to a server is lost after the statement has been sent (see [Server Resolution](#server-resolution), rule 4) | `utils.ErrDatabase` | 1 |
| A server does not answer within the caller's backstop deadline (see [Server Resolution](#server-resolution), rule 7) | `utils.ErrDatabase` | 1 |
| `graph client` finds no server listening for the selected roadmap (see [The Bolt Client](#the-bolt-client)) | `utils.ErrDatabase` | 1 |
| `graph serve` cannot bind its socket, or a live server already answers on the resolved socket (see [Server Startup](#server-startup)) | `utils.ErrDatabase` | 1 |
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
   `utils.ErrDatabase` (exit code 1), consistent with treating the graph store as
   a database-class dependency. The message carries a fixed prefix and then the
   engine's diagnostic text; `COMMANDS.md § Graph Management` publishes the exact
   line, and the engine's half of it is not specified there or here.
3. Errors are written as plain text to stderr and carry the standard AI-agent
   hint (see `HELP.md § Error message format`).
4. The graph feature introduces no new exit codes. If a future need arises for a
   dedicated graph error class, it MUST be added following the procedure in
   `ARCHITECTURE.md § Adding New Error Types`. The dedicated graph server and its
   client introduce none either: every failure either of them can produce is
   carried by a sentinel this table already names, and
   `ARCHITECTURE.md § Exit Codes of the Graph Server and Client` enumerates the
   codes each subcommand can return.
5. **A statement that the engine executes, and that does the wrong thing quietly,
   carries exit code 0 and no diagnostic.** Every hazard listed in
   [What Groadmap Does Not Check](#what-groadmap-does-not-check) reaches the
   caller as a success, because that is what the engine reports. No exit code in
   the table above distinguishes them, and none is added to: an exit code that
   claimed to would require the inspection this specification does not perform.
6. **A statement the time budget cuts fails in the same class and publishes a
   line of its own.** It carries `utils.ErrDatabase` and exit code 1, as rule 2's
   engine failures do, because the graph feature introduces no new sentinel and
   no new exit code (see [Constraints](#constraints), rule 5). Its message is not
   rule 2's message: the whole of it is `rmp`'s own text, it names the budget that
   was exceeded, it states that nothing was written, and it says what to do about
   it. `COMMANDS.md § Graph Management` publishes the exact line, and
   [Statement Time Budget](#statement-time-budget) states the behaviour behind it.
7. **An exhausted serialisation retry fails in the same class and publishes a
   line of its own too.** It carries `utils.ErrDatabase` and exit code 1, as rule
   2's engine failures and rule 6's budget exhaustion do, and for the same
   reason: the graph feature introduces no new sentinel and no new exit code. Its
   message is neither rule 2's nor rule 6's. The whole of it is `rmp`'s own text;
   it names the contention rather than the statement, it states that nothing was
   written, and it names the remedy — run the statement again, and spread
   concurrent writes across distinct nodes. It is reachable only for a statement
   a graph server executed, because on the direct path one invocation runs one
   transaction and the conflict path is unreachable there (see
   [Concurrency and Recovery](#concurrency-and-recovery)).
   `COMMANDS.md § Graph Management` publishes the exact line, and
   [Concurrency Inside the Server](#concurrency-inside-the-server) states the
   behaviour behind it.

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

**The server changes what the store's advisory lock means, and it changes how the
two surfaces that existed before it reach the graph.** A server holds that lock
exclusively for its whole process lifetime, which is a hold no finite wait can be
sized against. `rmp graph execute` and the web graph data endpoint therefore
resolve the roadmap's socket before they open anything: against a served roadmap
they send the statement to the server and never take the lock, and with no server
listening they open the store directly under the lock exactly as they do today.
That rule is stated once, in [Server Resolution](#server-resolution), and both
surfaces follow it rather than restating it.

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
2. **The `--socket` flag overrides that derivation on the three subcommands that
   publish it**, `graph serve`, `graph client` and `graph execute`. The web graph
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

### Server Startup

`rmp graph serve` performs this sequence in this order. The order is load-bearing:
each step is what makes a later one safe.

1. **Resolve the roadmap and the socket path.** A roadmap that does not exist
   fails here, before anything is opened, created, or removed.
2. **Take the graph store's exclusive advisory lock under the bounded wait**
   [Lock Contention](#lock-contention) specifies. The wait is the ordinary one: a
   server starting while a short-lived `rmp graph execute` invocation holds the
   lock waits for it rather than failing on the first collision. When that wait is
   exhausted the server does not start.
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
roadmap during that second would find no socket, conclude the roadmap is not
served, and take the direct path into a lock this process is already holding.
Binding first spends that second with the socket already accepting: a caller that
arrives during it connects, waits for the handshake, and is served. When the store
open fails instead, the server closes the listener and exits, and the waiting
caller sees the connection dropped and fails as
[Server Resolution](#server-resolution) requires rather than falling back.

**One window remains, and it is stated rather than hidden.** Between step 2 and
step 5 the lock is held and no socket answers. A caller that resolves inside it
takes the direct path, waits the whole wait budget, and fails. The window is a
probe, an unlink, and a bind — microseconds, not the store open — and the failure
is loud, deterministic, and cleared by retrying.
[Lock Contention](#lock-contention) records it as one of the three cases that put
a caller back on the wait.

**A second, narrower case belongs to the interval the bind covers, and it is a
failure rather than a fall back.** A caller that connects between step 5 and step
7 waits for the handshake while the store opens, and its probe deadline is
2500 ms (see [Server Resolution](#server-resolution)). The store open is measured
at 955 ms on a 36 MB graph and 1784 ms on a 122 MB one, so the deadline covers the
graphs measured; on one large enough that it does not, the caller's probe expires,
the roadmap resolves as **Unreachable**, and the invocation fails rather than
taking the direct path. That is the correct outcome and not a defect of the
resolution rule: the socket answered, so a server is starting there, and a caller
that fell back would take the direct path into a lock this process is about to
hold for its lifetime.

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
3. Shut the Bolt server down. Whatever is still in flight at that moment is cut,
   which is what the engine's shutdown does.
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
   [Statement Time Budget](#statement-time-budget)).

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
statement timeout above the bound `rmp graph execute` and the web graph data
endpoint obey. One declaration governs all three surfaces, and changing it changes
all three together.

**Capping the maximum has a consequence on explicit transactions, and it is stated
rather than left to be discovered.** The engine clamps an explicit transaction's
total life by that same maximum. A `BEGIN` to `COMMIT` sequence therefore has the
same 5 seconds in total that a single statement has, however many statements it
carries. That is the price of having one bound rather than two that can disagree;
a caller with more work than fits splits it across transactions.

**The connection timeout MUST sit well above the statement bound, and this is the
one option whose default is actively wrong here.** The engine documents that
timeout as the silent gap between messages, but it is armed as a read deadline on
the socket while the message loop is busy executing the previous statement. A
statement that runs longer than it destroys its own connection mid-flight,
whatever the statement's own budget says. Measured, the cut tracks the connection
timeout exactly and ignores a statement timeout four times its size. The engine's
default for the connection timeout equals its default statement timeout, so a
server left at both defaults is one whose slowest permitted statement is
guaranteed to die as a transport error rather than as a typed failure.

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
quantity [Lock Contention](#lock-contention) records on the direct path, arriving
at a second surface.

**Idleness is bounded by the same value.** The connection timeout is also what
bounds a session that sends nothing, so a client that holds a session open without
using it loses it after 60 seconds and must reconnect. `rmp graph client` sends
one statement per invocation and never meets this; a longer-lived client is
expected to reconnect.

**The inbound message bound MUST NOT sit below the maximum query length.** A
statement `rmp graph execute` accepts is a statement the server must accept, so
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
   durably**, exactly as it does not on the direct path. The write succeeded, the
   log is intact, the next successful checkpoint reconciles the snapshot, and the
   failure is a diagnostic rather than a failed statement (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)).
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
   write path: one cut **read** over the same store left it at 80 KB. The same
   statement submitted through `rmp graph execute` or through the web interface
   leaves the store unchanged, because both already fold under the condition
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write) states;
   rule 4 is what brings the server onto the same footing.
9. **The in-flight checkpoint of rule 5 is not conditioned, and the residue of
   rule 8 is still reachable through it.** It folds on the cadence rule 6 fixes,
   driven by the engine's own loop rather than by Groadmap, so a server that
   outlives its first fold can publish that residue without ever being shut down.
   Conditioning it would mean Groadmap driving that fold itself, which is new
   behaviour inside the server process rather than a value, and this version does
   not specify it. What rule 4 closes is the shutdown window; what bounds the
   remaining one is the cadence, and nothing else does.

### Server Resolution

**This section is the single statement of how a caller decides whether a roadmap
is served, and every surface that reaches a graph follows it.**
`rmp graph execute` follows it, and so does the web graph data endpoint (see
`WEB.md § Knowledge Graph from the GoGraph Store`). Neither states a rule of its
own; each states only the outcome its own surface reports for each of the four
states below.

The rule exists because a server's hold on the exclusive advisory lock lasts as
long as the process does, and no finite wait can be sized against a hold with no
upper bound (see [Lock Contention](#lock-contention)). A caller that reached the
store directly against a served roadmap would wait its whole wait budget and then
fail — not intermittently and not under load, but on every invocation, for as long
as the server ran. Resolving the socket first is what stops a running server from
disabling the two surfaces that existed before it.

**The resolution is a probe, and the probe is bounded.** The caller resolves the
socket **in force** — the path derived from the roadmap, or the value of
`--socket` where the surface publishes that flag (see
[Socket Path and Permissions](#socket-path-and-permissions), rule 2) — connects to
it, and completes the protocol handshake. The whole probe carries a deadline of the project's backoff total,
2500 ms (see `IMPLEMENTATION.md § Retry Logic`), reused here as the allowance for
a cost that is local and scheduling-bound rather than an I/O one: connecting to a
socket in the local filesystem either succeeds or fails inside the kernel, and the
handshake is one exchange with a process on the same machine. The probe is not
retried. It is spent before the statement starts and before any lock is taken, so
it consumes neither the statement budget nor the wait budget.

The four states, and what each surface does with each:

In the table below, "the socket" is always the socket in force for that
invocation or request, never the derived path specifically.

| State | How the caller recognises it | `rmp graph execute` | The web graph data endpoint |
|-------|------------------------------|---------------------|-----------------------------|
| **Not served: no socket** | The socket path does not exist | Open the store directly under the exclusive lock, exactly as it did before this section existed. Exit code 0 on success | Open the store directly under the exclusive lock, exactly as before. HTTP `200` on success |
| **Not served: nothing listening** | The connection is refused, which is what a socket file left behind by a killed server answers | As above, and the leftover file is neither an error nor removed. Exit code 0 on success | As above. HTTP `200` on success |
| **Served** | The connection is accepted and the handshake completes inside the probe deadline | Send the statement to the server. Do not take the exclusive lock and do not open the store. Exit code 0 on success | Send the statement to the server. Do not take the exclusive lock and do not open the store. HTTP `200` on success |
| **Unreachable** | The connection is accepted but the handshake does not complete inside the probe deadline, or the connection fails for any reason other than the two above | Fail with `utils.ErrDatabase`, exit code 1 | Fail as an internal read error, HTTP `500` |

Rules:

1. **A leftover socket file is not an error and never fails an invocation.** A
   server that was killed leaves its socket behind, and the refusal a connection
   to it receives is the whole of the evidence needed to conclude that nothing is
   listening. The caller proceeds on the direct path and does **not** remove the
   file: removing one is the next server's business, and a caller that removed one
   would race a server that was binding it.
2. **A probe that does not answer is not a fallback.** Only the two definite
   negatives above mean "not served". Everything else — a handshake that does not
   complete, a permission the caller does not have, a path that is not a socket —
   is a resolution failure and fails the invocation. Falling back on any of those
   would send the caller at a lock a server may well be holding for its process
   lifetime, which is the outcome this section exists to prevent.
   `COMMANDS.md § Graph Server Socket Error Lines` publishes the exact line each of
   these failures writes.
3. **Resolution is decided before any lock is taken and before any store is
   opened**, so a caller takes exactly one of the two paths and never both. A
   caller that resolved a server and then failed does not retry on the direct
   path.
4. **A connection lost once the statement has been sent is a failure, and the
   caller MUST NOT then reach the store directly.** The statement may already have
   committed on the server: the commit is durable before it is acknowledged, so a
   connection that dies between the two leaves the caller unable to tell whether
   it happened. Re-running the statement would run it a second time, against a
   store a server may still be holding. The invocation therefore fails and reports
   what it can honestly report — that the connection to the server was lost and
   the statement's outcome is unknown. `rmp graph execute` fails with
   `utils.ErrDatabase` and exit code 1. The web graph data endpoint answers HTTP
   `400` with the `execution` kind, because the failure surfaced once the
   statement was running, which is where `WEB.md § Query-Bar Error Handling`
   already draws that boundary.
5. **The statement crosses unchanged.** The caller sends the statement it was
   given. The node-`LIMIT` injection the web endpoint performs is applied before
   the statement is sent, exactly as it is applied before a direct execution (see
   `WEB.md § Graph Data Endpoint`). Resolution decides where a statement runs and
   changes nothing about what runs.
6. **The result is the same result on both paths.** A statement produces the same
   output shape, the same values, and the same exit code whichever path carried
   it; which path that was is not observable in the result.
   `DATA_FORMATS.md § Graph Client Result` is canonical for the mapping that makes
   this true of a result carried over the protocol.
7. **The statement budget binds both paths, and on the served path the server's
   end of it fires first, deliberately.** The server bounds the statement at the
   statement budget (see [Server Options](#server-options)), and the caller keeps
   a deadline of its own so that a server which answers nothing at all cannot hold
   it for ever. **The caller's deadline is the wait budget, statement budget plus
   backoff total, and never the statement budget itself.** The reason is a false
   report the equal values would produce: a statement that commits a few
   milliseconds before the budget expires has its acknowledgement in flight when a
   caller-side deadline of exactly the budget fires, and the caller would then
   print the budget line — which states that nothing was written — over a write
   that had succeeded. Giving the caller the later deadline makes the server's
   typed failure the one that arrives, so the budget line is printed only when the
   engine really did cut the statement and really did roll it back. The caller's
   own deadline is a backstop, and when it is what fires the failure is reported
   as an unanswered server rather than as a budget exhaustion: the connection is
   intact, the server is alive, and the statement's outcome is unknown. That is
   what a statement the budget cut **mid-write** looks like from outside, because
   the engine's undo replay runs past the deadline by a factor nothing bounds (see
   [Statement Time Budget](#statement-time-budget)) — the same unbounded quantity
   [Lock Contention](#lock-contention) records on the direct path, reaching a
   third surface. The caller does not fall back to the store, for rule 4's reason.
   Both bounds are derived from one declaration, so they cannot disagree about the
   value.

   A statement genuinely cut by the budget therefore reports the same class of
   failure on either path: exit code 1 for `rmp graph execute` and for
   `rmp graph client`, and an execution failure with HTTP `400` for the web graph
   data endpoint.
8. **A retriable serialisation conflict is retried on the served path, and is not
   surfaced on its first occurrence.** It is a normal outcome (see
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
    whether a caller can set it.** `graph execute`, `graph client` and
    `graph serve` each take `--socket` and each default it identically; the web
    graph data endpoint takes none and resolves the derived path. Everything else
    in this section is the same on every surface, and the asymmetry has exactly
    one consequence, stated in
    [Serving on a Non-Default Socket](#serving-on-a-non-default-socket).
11. **`rmp graph client` resolves the same socket but has no second path.** For
    that subcommand the first two states are failures rather than fallbacks; see
    [The Bolt Client](#the-bolt-client).

### The Bolt Client

`rmp graph client` and the client half of [Server Resolution](#server-resolution)
are one implementation rather than two. The subcommand is a thin wrapper over the
same client `rmp graph execute` and the web graph data endpoint use when a roadmap
is served: the same connection, the same statement, the same retry, and the same
mapping of protocol values onto JSON. A second implementation would be a second
set of answers to every question this section settles.

What the subcommand adds over that shared client is what a command-line surface
adds. It reads the statement from `-q` / `--query` or from standard input under
the rules [Cypher Input Source and Precedence](#cypher-input-source-and-precedence)
already fixes, it serialises the result to stdout, it writes diagnostics to
stderr, and it chooses an exit code.

What it deliberately does not do is fall back. A roadmap with no server listening
is a failure for this subcommand and not a signal to open the store: its contract
is that it speaks to a server, and a subcommand that quietly became
`graph execute` when no server answered would report a success that says nothing
about whether a server was reached. The two subcommands ask different questions
and keep different answers.

### Serving on a Non-Default Socket

`--socket` moves the socket off the path every surface derives. **The command line
can follow it and the web interface cannot, and that boundary is the whole of what
this section is about.**

1. **The CLI follows, through the flag.** `graph serve`, `graph client` and
   `graph execute` all publish `--socket` and all default it identically, so a
   server started on a non-default path is reached by giving the same path to the
   subcommand that talks to it. Nothing is lost on this side: a statement sent
   that way runs on the server, returns the same result, and carries the same exit
   code as one sent to a server on the derived path.
2. **The web graph data endpoint cannot follow, and this is not an oversight.** It
   is an HTTP handler; it has no command line, `rmp web` serves every roadmap at
   once rather than one, and no request parameter carries a socket path. It
   therefore resolves the derived path, finds nothing there, concludes the roadmap
   is not served, and takes the direct path — where it meets the lock the server
   is holding for its process lifetime, waits the whole wait budget, and answers
   HTTP `500`. That happens deterministically, on every request for that roadmap's
   graph, for as long as that server runs.
3. **The residual is therefore halved and named, rather than removed.** Before
   `graph execute` carried the flag, serving on a non-default socket disabled
   **both** surfaces that existed; it now disables one. The half that remains is
   the half no flag closes, because the surface that cannot follow has nowhere to
   put a flag. Closing it would take a mechanism the caller does not supply — the
   server advertising its bound path somewhere every surface reads — and this
   specification does not introduce one.
4. **`--socket` is therefore specified as what it is: an option that keeps the CLI
   and costs the web page.** It exists for a server the browser is not expected to
   reach: a test harness, a diagnostic session, a socket that has to live on a
   different filesystem. A server whose roadmap is also browsed is started without
   it.
5. The refusal the web request meets in that state is the lock's, and it reports an
   internal read error. It does not report that a server is running on another
   path, because nothing in the product knows that: the lock records no holder, and
   the request probed a path the server never bound.
6. Nothing about this changes what a `--socket` value must be. A path that cannot
   be bound fails `graph serve` at startup, and a path nothing answers on is read
   by `graph execute` as "not served" — so a mistyped `--socket` sends that
   invocation to the store rather than to the server it meant, silently and
   successfully. That is rule 2 of [Server Resolution](#server-resolution) applied
   to a caller-supplied path, and it is stated here because a typo is a likelier
   cause of it than a deliberate choice.

## Concurrency and Recovery

GoGraph's store is transactional, and MVCC is its only concurrency-control
mechanism. Reads observe a consistent committed state. Independent write
transactions are not excluded from one another inside a single process: a
write-write collision is detected rather than prevented, on a first-updater-wins
basis, and the losing transaction receives a retriable serialization-conflict
error. On the **direct** path Groadmap does not rely on that intra-process behaviour,
because each `rmp graph execute` invocation is a separate short-lived process that
runs exactly one transaction, and each web graph request runs exactly one; that
one-transaction-per-invocation model is why the conflict path is not reachable
there. It is reachable inside `rmp graph serve`, which runs many transactions
concurrently in one process. What a conflict means there, and the retry it
obliges of every client, are specified in
[Concurrency Inside the Server](#concurrency-inside-the-server).

Groadmap does not depend on the engine to serialise access to the store. It
serialises it itself, at the process level, through a single advisory lock file
that Groadmap maintains in the roadmap's graph directory, `write.lock` (see
[Persistence Layout](#persistence-layout)). Every invocation and every web graph
request that opens the store takes that lock **exclusively** before opening it, and
holds it across the whole open, execution, commit, checkpoint, and write-ahead-log
truncation sequence. A caller that reached a running server instead opens no store
and takes no lock (see [Server Resolution](#server-resolution)), and a server
takes the lock once and holds it until the process stops.

**There is one lock mode, because there is one execution path.** Groadmap does not
examine a statement, so it cannot know before running one whether it will write,
and a lock mode chosen on a guess would be a shared lock held while a statement
committed. The exclusive lock is the mode that is correct for every statement. The
cost is stated rather than hidden: two statements against the same roadmap
serialise even when neither of them writes, where a shared mode would have let
them overlap.

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
and it is the write-ahead log's own state that decides whether the checkpoint of
[Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write) runs at all.
Such a statement, whether it arrived through `rmp graph execute` or through a web
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
5. Creates the lock file `write.lock` when it does not already exist.
6. Creates the graph directory itself when, and only when, the statement arrived
   through `rmp graph execute` (see [Persistence Layout](#persistence-layout),
   rule 2). The web interface never creates the graph directory: a roadmap with no
   graph directory is an empty graph and is served as such (see `WEB.md § Knowledge
   Graph from the GoGraph Store`).

The **content** of the graph is therefore never changed by a statement that writes
nothing. What such a statement can change is the store directory's structure, and
only by completing a repair that the next invocation to open the store would
otherwise complete instead.

### Statement Time Budget

**Every statement runs under a deadline, and both surfaces use the same one.**
`rmp graph execute` executes its statement under the time budget the web graph
data endpoint applies, and under the same value: 5 seconds.
`WEB.md § Graph Query Time Budget` fixes that value, states the evidence for it,
and is canonical for it on both surfaces. This section is canonical for what the
budget does to an `rmp graph execute` invocation and for what a cut statement
leaves behind. The value is one declaration that both surfaces read, so there is
no second constant to drift from the first.

The budget bounds the **variable** part of a hold on the graph store lock — the
statement, whose cost the caller chooses — for a read and for a statement that
runs to completion. It does not bound a statement the deadline cuts while that
statement is writing: the measurements at the end of this section show that such
a hold has no known upper bound. It does not bound the fixed part either, which
[Lock Contention](#lock-contention) accounts for separately, and it bounds nothing
at all about what the statement costs in memory, which
[Peak Resident Memory](#peak-resident-memory) measures. That section states
what the wait derived from this budget covers, what it does not, and the residual
limits that survive.

1. **The deadline covers the execution of the statement and the walk over its
   result.** It starts when the invocation begins executing the statement. It
   does not cover taking the lock, opening the store, the recovery repair the
   open performs, or the checkpoint. The deadline is the invocation's own and is
   the only thing that cancels its statement: unlike the web graph data endpoint,
   a CLI invocation has no request whose client can disconnect (see
   `WEB.md § Graph Query Time Budget`, rule 2).
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
4. **The invocation fails with `utils.ErrDatabase` (exit code 1).** The budget
   introduces no new sentinel error and no new exit code, and may not:
   [Constraints](#constraints), rule 5, forbids both. The exact line the user
   reads is published in `COMMANDS.md § Graph Management`.
5. **The cancellation arrives through the result iteration.** The engine's
   statement call returns no error; the result's own error does, as
   `context.DeadlineExceeded`. An implementation that classified only the call's
   error would report a cut statement as an ordinary query failure and tell the
   user nothing about the budget. This is the same arrival point the web graph
   data endpoint already classifies, which is why one classification serves both
   surfaces.
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

**The three surfaces are exposed differently, and the difference is the point.**
The same statement, at the same budget, over the same 600-node store:

| | `rmp graph execute` | `rmp web` | `rmp graph serve` |
|---|---:|---:|---:|
| baseline resident memory | not applicable | 23 MB | 18 MB |
| peak resident memory | 2974-3293 MB | 3088 MB | 3618-3734 MB |
| resident 130 s later | 0, the process exited | **3088 MB, none of it returned** | 1064 MB |
| the store on disk afterwards | unchanged | unchanged | unchanged |
| what the caller received | `utils.ErrDatabase`, exit 1 | an empty reply after 39.5 s | the unanswered-server line at 7.5 s, exit 1 |

A short-lived invocation returns the memory to the operating system by exiting;
a long-lived one has no exit to return it at. `rmp web` returned none of its
3088 MB in the 130 seconds observed, because an otherwise idle process triggers
no collection. `rmp graph serve` released most of its own after roughly 73
seconds and settled at a floor of 1064 MB — **58 times its baseline** — where it
stayed for the remainder of the observation. The web request itself failed at
39.5 seconds with an empty reply, which is the 30-second `WriteTimeout` closing
the connection while the statement was still inside the engine call: the case
`WEB.md § Graph Query Time Budget` and [Lock Contention](#lock-contention) both
predict, now observed end to end. The store on disk is unchanged on all three
surfaces, and on the server that is a requirement rather than an accident: an
unconditional shutdown checkpoint left the same store at 134 MB, which is why
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
   **Declined**, and on coherence rather than on cost. A cap that the server
   carried and the direct path did not would make the same statement pass or
   fail depending on whether a server happened to be running, because a served
   roadmap routes `rmp graph execute` through that server automatically, with no
   flag and with byte-identical output (see
   [Server Resolution](#server-resolution)): a statement that works today would
   begin failing the moment somebody started a server, invisibly to whoever
   wrote it, and a limit the caller cannot see and did not choose is worse than
   one met honestly. A single cap applied to every surface alike avoids that
   incoherence and carries a cost of its own, which is why it is declined too:
   it publishes a ceiling on the rows any statement may return, and
   `MATCH (n) RETURN n` over the largest real knowledge graph on the development
   machine needs 44,906 of them.
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

One lock mode carries one contention policy.

1. An invocation that finds the exclusive lock held **waits**, under the bounded
   exponential-backoff policy specified in
   `IMPLEMENTATION.md § Graph Store Concurrency`. It MUST NOT block indefinitely
   and MUST NOT fail on the first collision. The wait carries a budget of its
   own, derived from the statement budget rather than from the fixed total the
   SQLite layer waits; the derivation, the 7.5 seconds it yields at the statement
   budget in force, and the holds it does not cover are stated below.
2. If the lock is still unavailable when that bounded wait is exhausted, the
   invocation fails: with `utils.ErrDatabase` (exit code 1) for
   `rmp graph execute`, and as an internal read error (HTTP 500) for the web graph
   data endpoint, which is the status that endpoint already returns for a graph
   store that cannot be opened (see `WEB.md § Routes and Pages`).

**Waiting rather than failing fast is the policy because every caller is now a
possible reader.** A policy that failed on the first collision would make ordinary
statements intermittently unavailable, and one of the two callers is an HTTP
request handler, for which an unbounded block would let a long-running statement
hang a `GET` until the server's write timeout fired (see
`WEB.md § HTTP Server Timeouts`). The wait does not consume the endpoint's query
time budget, because the wait ends before the statement starts (see
`WEB.md § Graph Query Time Budget`).

**The hold spans the statement, so the statement is the variable part of it.**
The hold covers the whole open, execution, commit, checkpoint, and
write-ahead-log truncation sequence, including a full snapshot rewrite whose cost
grows with the live graph size, and the execution of a statement whose cost the
caller chooses. An expensive statement therefore delays every other statement
against the same roadmap for as long as it runs, which a shared reader hold did
not. Nothing is taken out of the hold to shorten it: the sequence that must not
interleave is the one [Concurrency and Recovery](#concurrency-and-recovery)
fixes, and it is the wait that is sized to the hold, never the hold that is
trimmed to the wait.

**The wait is derived from the statement budget, which bounds some holds and not
all of them.** A finite wait can cover a hold only when that hold has an upper
bound. The statement budget is that bound for the variable part of the holds that
have one — a read, and a statement that runs to completion — and the wait budget
is derived from it:

```
wait budget = statement budget + backoff total
```

- **Statement budget** is the deadline `WEB.md § Graph Query Time Budget` fixes
  for a caller-supplied statement, 5 seconds. It bounds the variable part of a
  hold whose statement is a read or runs to completion. It does **not** bound the
  variable part of a hold whose statement the deadline cuts while it is writing,
  and the third residual below states what that costs. It is a graph-store-wide
  quantity rather than a web-local one, because the party that must wait for a
  hold has to know how long a hold may lawfully last, and the waiter is not
  necessarily the web.
- **Backoff total** is the worst-case total wait of the project's single retry
  policy, 2500 ms (see `IMPLEMENTATION.md § Retry Logic`), reused here as the
  allowance for the **fixed** part of a hold: the store open, the write-ahead-log
  open, the engine construction, the commit, and a full snapshot checkpoint with
  its log truncation, plus scheduling. It is reused rather than replaced by a
  figure of its own so that the project keeps one set of timing numbers. How much
  of the quantity it actually covers depends on the size of the graph, and is
  stated next.

At the 5-second statement budget in force, the wait budget is therefore
**7.5 seconds**.

**The allowance for the fixed part is sized against a quantity that grows with
the graph, and the allowance does not grow with it.** The fixed part of a hold is
linear in the store's size on disk, at a rate that depends on the shape of the
data. Measured by phase instrumentation, forcing a write so that the checkpoint
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

A cut statement does not checkpoint, so on that path the fixed part is the store
open alone. The rollback a cut **write** performs is not part of the fixed part:
it is proportional to what that statement had already written, and therefore
belongs to the statement, which is where
[Statement Time Budget](#statement-time-budget) measures it.

**The margin is stated on the largest real graph, where it needs no
extrapolation, and the point at which it runs out is stated too.** At 36 MB the
fixed part is 1286 ms on the ordinary path and 955 ms on the cut path, which is
the store open with no checkpoint behind it, against the 2500 ms allowance. That
is a margin of **1.9x and 2.6x** — a margin, and not an order of magnitude. The
allowance is **exhausted at roughly 70 MB** for graphs shaped like the four real
ones measured, and only at roughly 110 MB for the simpler synthetic shape.

**Beyond that point the no-starvation guarantee lapses, and nothing in the
product detects it.** A wait shorter than the fixed part of the hold it must
cover cannot serve its waiter, so on a graph past roughly 70 MB a waiter can fail
against a holder that is behaving lawfully and is inside every budget — with no
statement cost involved at all. Groadmap does not measure a graph's size, does
not warn on it, and does not refuse to open a store above it. This is a known
limit of the sizing rule, stated here rather than hidden. Raising the allowance
is not specified here because it lengthens the wait for every caller, including
the web graph data endpoint, whose wait and statement must still fit inside the
server's write timeout together.

**A wait shorter than the hold it must cover starves the waiter.** A wait sized
against the SQLite policy's fixed total would put two of this project's own
constants in a two-to-one ratio in the holder's favour: a hold may lawfully last
the whole 5-second statement budget, while the waiter would give up after
2500 ms. The consequence is measured, not theoretical. With the race detector
absent and the statement budget untouched, a holder whose statement ran 4.71
seconds, lawfully and inside its own budget, starved a contending invocation,
which failed after 2.5018 seconds. On a small store the statement dominates the
hold it belongs to: 98.0% of a 951.86 ms hold without the race detector, and
99.84% of a 14.26-second hold under it. On a large one the balance inverts — at
36 MB the open alone costs 955 ms while the statement part of every realistic
query measures between 0 and 554 ms — which is why the two parts of the wait
budget are sized separately and why the fixed part carries the limit above.

**The invariant is that everything a request spends before its response fits
inside the server's write timeout together.** A request that exhausts the wait
must still have its `500` written, and a request that waits and then runs its
statement to the end of its budget must still have its response written. The
quantity to compare against the 30-second `WriteTimeout` (see
`WEB.md § HTTP Server Timeouts`) is therefore the sum of every bounded term a
request may spend, and which terms those are depends on the path. On the **direct**
path there are three: the resolution probe that decides whether the roadmap is
served, 2500 ms (see [Server Resolution](#server-resolution)); the wait budget,
7.5 seconds; and the statement budget, 5 seconds. Two and a half plus 7.5 plus 5
is 15 seconds, inside 30. On the **served** path there are two: the same probe,
and the caller's backstop deadline over the statement, which is the wait budget's
7.5 seconds (see [Server Resolution](#server-resolution), rule 7). No lock is
taken at all, so 2.5 plus 7.5 is 10 seconds, also inside 30. It is the sum that
must fit. A wait that is merely a small fraction of the write timeout is
neither necessary nor sufficient, and sizing on that property alone is what admits
a wait shorter than the hold it has to cover.

**That invariant does not hold today on the cut-write path, and it is the
statement term alone that breaks it.** The sum above is valid only where the
statement term is bounded by the statement budget, and it is not bounded on a
statement the deadline cuts while it is writing. The longest such hold measured,
**35.6 seconds** (see [Statement Time Budget](#statement-time-budget)), exceeds
the 30-second `WriteTimeout` on its own, before any wait is added to it, and the
web graph data endpoint runs its statement through the same engine call under the
same budget as the CLI. No value of the wait budget repairs this, because the
term that exceeds the timeout is not the wait. It is stated here rather than
papered over, and the residual below states the same fact from the waiter's
side.

**A statement cut mid-write has no known upper bound on its hold, so the wait
does not cover one.** The statement budget binds both surfaces — the web graph
data endpoint and `rmp graph execute` execute their statements under the same
5-second deadline (see [Statement Time Budget](#statement-time-budget)) — and a
hold whose statement is a read, or a statement that runs to completion, therefore
has a lawful maximum. The wait above is derived from that maximum, and a waiter
contending with such a hold is served. A statement the deadline cuts while it is
writing has no such maximum. Its hold is the budget multiplied by a factor the
statement itself sets, measured from 1.005x to 7.13x with no ceiling established,
so no finite wait is derived from it: the 7.5-second wait is exhausted against a
holder that is behaving lawfully and is inside every budget this specification
publishes, and the waiter fails while the holder is doing nothing it is not
entitled to do. Groadmap does not bound that hold from its own side. The overrun
runs inside the engine call and takes no cancellation (see
[Statement Time Budget](#statement-time-budget)), and refusing in advance the
statement shapes that provoke it would require examining the statement, which
Groadmap does not do (see [Concurrency and Recovery](#concurrency-and-recovery)
and [What Groadmap Does Not Check](#what-groadmap-does-not-check)). The bound has
to come from the engine. This residual stands beside the fixed-part allowance
above, which lapses on a graph past roughly 70 MB whatever the statement does,
and the server below.

**A server holder is outside the wait-based policy, and the resolution rule is
what keeps it outside.** A finite wait can cover only a hold that has an upper
bound, and a hold that lasts a process lifetime has none, so no wait derived from
a statement budget covers one. `rmp graph serve` is a holder of exactly that
shape: it opens a roadmap's store once and holds the exclusive lock for as long as
the process runs (see
[The Dedicated Graph Server](#the-dedicated-graph-server)). The policy is not
extended to cover it, because it cannot be. What happens instead is that no
ordinary caller waits on it at all: `rmp graph execute` and the web graph data
endpoint resolve the roadmap's socket before they take the lock, and against a
served roadmap they send the statement to the server and never take it (see
[Server Resolution](#server-resolution)). Mutual exclusion moves from
per-statement to per-process, and contention inside the served process is resolved
by the store's MVCC rather than by this lock (see
[Concurrency Inside the Server](#concurrency-inside-the-server)).

**The lock therefore carries two roles, and only the first of them is a wait.**
Between short-lived holders it is the bounded wait this section specifies,
unchanged. Against a server it is a **startup interlock**: it admits one server
per roadmap, because a second `rmp graph serve` against the same roadmap cannot
take it and does not start. That is one mechanism used for two purposes, and the
difference is worth naming — the first is a queue, the second is a refusal.

**Three residual cases put a caller back on the wait, and each is bounded and
loud.** A caller reaches the lock against a server only when resolution sent it
there, and resolution sends it there only in these:

1. **The server's startup window.** The lock is taken before the socket is bound,
   so a caller that resolves inside that interval finds no socket and takes the
   direct path. The interval is a probe, an unlink, and a bind — not the store
   open, which is deliberately spent with the socket already accepting (see
   [Server Startup](#server-startup)). The caller waits the wait budget, fails,
   and succeeds on a retry.
2. **The server's shutdown window.** The socket goes away when the listener
   closes, which happens before the final checkpoint and before the lock is
   released (see [Server Shutdown and the Drain](#server-shutdown-and-the-drain)).
   A caller that resolves between the two reads no socket, takes the direct path,
   and waits for the checkpoint and the store close — which are the fixed part of
   a hold, the quantity this section already reserves an allowance for. Inside
   that allowance the caller waits and then succeeds; past it, the allowance's own
   limit above applies, exactly as it does to any other holder.
3. **A server on a non-default socket, for the one surface that cannot be told
   about it.** `rmp graph execute` follows such a server through its own
   `--socket` flag and never reaches the lock. The web graph data endpoint has no
   flag to follow it with, so it resolves the derived path, finds nothing, takes
   the direct path, and fails for as long as that server runs (see
   [Serving on a Non-Default Socket](#serving-on-a-non-default-socket)).

In each case the outcome is rule 2's: `utils.ErrDatabase` and exit code 1 for
`rmp graph execute`, HTTP 500 for the web graph data endpoint. That is the outcome
this section has always specified for an exhausted wait, and the server adds no
new one.

Groadmap's usage model and expectations:

1. Each `rmp graph execute` invocation is a short-lived process that opens the
   store, runs one statement, commits, checkpoints if that transaction wrote (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)), and
   closes the store. The process does not hold the store open across invocations.
2. Two concurrent invocations against the **same** roadmap contend for the lock,
   whatever their statements do. The implementation MUST surface an exhausted wait
   as `utils.ErrDatabase` (exit code 1) rather than corrupting the store or
   hanging indefinitely. The checkpoint runs inside the invocation that already
   holds the lock: it adds no separate lock, and two concurrent invocations still
   serialise.
3. A web graph request contends with `rmp graph execute` in both directions, and
   with another web graph request, on the same lock and under the same policy.
   Statements against **different** roadmaps never contend, since each roadmap has
   its own graph directory and its own lock file.
4. Recovery on open restores the last committed state from the snapshot and the
   write-ahead-log tail. Because every write checkpoints, recovery genuinely
   exercises the snapshot path: a graph opened after a previous write is rebuilt
   from that snapshot plus any log entries written since the last checkpoint,
   rather than by replaying the entire write history. The restored state includes
   deletions: a node deleted by a previous invocation stays deleted after the
   store is reopened, because the snapshot records the tombstone set and recovery
   reconstructs it. A graph left in a consistent committed state by a previous
   invocation opens cleanly. A graph whose store is corrupt or unreadable surfaces
   as `utils.ErrDatabase` (exit code 1); there is no automatic graph-store repair
   in this first version.
5. The graph store is independent of the SQLite layer and the SQLite WAL
   model described in `IMPLEMENTATION.md § Concurrency Model`; the two persistence
   mechanisms do not share connections, locks, or transactions.
6. `rmp graph serve` is the one exception to rule 1's short-lived model, and the
   only holder of the lock that is not short-lived. It opens the store once, holds
   the lock for its process lifetime, and closes both when it stops. While it
   runs, rules 2 and 3 do not describe what an `rmp graph execute` invocation or a
   web graph request does against that roadmap: neither contends with the server,
   because neither opens the store (see
   [Server Resolution](#server-resolution)). The three windows in which one of
   them still reaches the lock are the three named above.

## Constraints

1. The graph is free-form. Groadmap MUST NOT impose, validate, or auto-create a
   node/edge schema. The conventions in
   [Multi-Layer Modelling Conventions](#multi-layer-modelling-conventions) are
   recommendations only. A caller may declare indexes and constraints of its own
   through `graph execute` (see [Schema Management](#schema-management)). That does
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
5. The graph feature MUST NOT introduce a new sentinel error or exit code in this
   version (see [Error Handling and Exit Codes](#error-handling-and-exit-codes)).
6. GoGraph is pinned to an exact version in `go.mod` (see
   [Dependency Maturity Risk](#dependency-maturity-risk)).

## Acceptance Criteria

1. `rmp graph execute -r <roadmap> --query "CREATE (s:Spec {key:'user-authentication'})"`
   creates the node, persists it, prints `{"ok": true}` (the statement has no
   `RETURN` clause), and exits 0. The same statement with `... RETURN s` appended
   instead returns the created node in the `columns`/`rows` shape
   (see `DATA_FORMATS.md § Graph Write Result`).
2. `rmp graph execute -r <roadmap> --query "MATCH (s:Spec) RETURN s.key"` returns
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
   `rmp graph execute`, and a read-back after each confirms the effect. An
   implementation that refused any one of them on the ground of what it does fails
   this criterion.
5. **`execute`, `serve`, and `client` are the only subcommand names `rmp graph`
   resolves.** Each of `rmp graph create`, `rmp graph query`, `rmp graph update`,
   `rmp graph delete`, and `rmp graph search`, invoked with an otherwise valid
   `-r` and `--query`, is an unresolved subcommand name: it exits `127`, writes
   zero bytes to stdout, and writes the dispatch-failure error and the `graph`
   help to stderr (see
   `COMMANDS.md § Dispatch Failures (Unresolved Command or Subcommand Names)`).
   The criterion MUST assert the exit code **and** that the statement did not
   run — the graph is byte-identical afterwards — because an alias that quietly
   executed would otherwise pass an exit-code-only check on the success path.
6. `echo "MATCH (n) RETURN count(n)" | rmp graph execute -r <roadmap>` reads the
   statement from standard input and returns the count, exits 0.
7. `rmp graph execute -r <roadmap>` with no `--query` and no piped standard input
   fails with exit code 2 (no query supplied).
8. `rmp graph execute -r <roadmap> --query "MATCH p=(a)-[*1..3]-(b) RETURN p"`
   executes a variable-length traversal and returns results, exits 0.
9. `rmp graph execute -r missing-roadmap --query "MATCH (n) RETURN n"` against a
   non-existent roadmap fails with exit code 4.
10. A syntactically invalid Cypher statement fails at execution with exit code 1
    and a plain-text engine diagnostic on stderr. The exit code is what
    distinguishes an engine failure from the two refusals Groadmap owns: 1, and
    not the 2 of a missing query or the 6 of an over-long one.
11. `graph execute` is represented in the AI Agent Contract emitted by
    `rmp graph --ai-help` and `rmp --ai-help`, with the same fields as every
    other subcommand, and no entry remains for any of the five removed names (see
    `DATA_FORMATS.md § AI Agent Contract`).
12. The graph directory `~/.roadmaps/<roadmap>/graph/` is created with `0700`
    permissions on first graph use.
13. After a successful `rmp graph execute -r <roadmap> --query "CREATE ..."`, the
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
    After `rmp graph execute -r <roadmap> --query "MATCH (n) RETURN count(n)"`, the
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
20. An invocation that cannot take the lock within the bounded wait fails rather
    than hanging: `rmp graph execute` exits 1 with a plain-text diagnostic on
    stderr, and the web graph data endpoint answers HTTP 500. Neither blocks
    indefinitely (see [Lock Contention](#lock-contention)).
21. `rmp graph execute -r <roadmap> --query "MATCH (a:Spec), (b:Code) RETURN a.key, b.path"`
    runs a disconnected multi-pattern `MATCH` and surfaces a Cartesian-product
    notification on stderr (a plain-text line carrying at least the severity, the
    stable code, and the description). The stdout JSON is exactly the normal
    `columns`/`rows` result, unchanged by the notification, and the exit code is 0
    (see [Query Notifications as Diagnostics](#query-notifications-as-diagnostics)).
22. `rmp graph execute -r <roadmap> --query "MATCH (s:Spec) RETURN s.key"`, a
    statement that produces no notifications, writes nothing extra to stderr:
    stderr is empty on success, while stdout carries the normal result and the exit
    code is 0.
23. A statement longer than the maximum is refused, and the read that refuses it
    is bounded. A producer that offers `rmp graph execute -r <roadmap>` far more
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
    `rmp graph execute -r <roadmap>` with `--query` absent fails with exit code 2
    and `Error: required parameter missing: no query supplied` on stderr in each
    of the three cases the rule names: standard input at end of stream, standard
    input carrying only whitespace, and standard input connected to a terminal.
    The terminal case is the one that regressed into a hang, and it is asserted
    on wall-clock time: the process exits without waiting for input, rather than
    sitting there until something kills it. Criterion 7 fixes the exit code for
    the first case; this criterion fixes the message, all three cases, and the
    requirement that none of them waits (see
    [Standard Input That Supplies No Query](#standard-input-that-supplies-no-query)).
25. `graph execute` refuses a positional argument.
    `rmp graph execute -r <roadmap> --query "<cypher>" stray`
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
    `rmp graph execute stray -r <a roadmap that does not exist> --query "<cypher>"`
    exits 2 and not 4; the same invocation with no `-r` and no roadmap selected
    exits 3; and `rmp graph execute -r <roadmap> stray` with a producer still
    writing to standard input exits 2 at once, reads nothing, and leaves the
    producer to observe a broken pipe. In every case stdout is empty, stderr
    carries the error line and the AI-agent hint and no help body, and the
    roadmap's `graph/` directory — its snapshot directory and its write-ahead
    log — is byte-identical before and after.
28. The rule is one rule across the two families that publish it. The line
    `graph execute` emits is the line `COMMANDS.md § Positional Arguments`
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
    `rmp graph execute` in a separate process reports the `WebProbe` node present.
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
    runs under `rmp graph execute` and exits 0, and step 2 groups its rows by NFC
    form and names the group holding both nodes. The same audit reports nothing on
    a graph whose keys are all distinct under NFC.
32. `graph execute` runs the schema statements, and each returns the shape the
    specification gives it.
    `rmp graph execute -r <roadmap> --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"`
    prints `{"ok": true}` and exits 0.
    `rmp graph execute -r <roadmap> --query "SHOW INDEXES"` exits 0 and returns the
    listing in the `{columns, rows}` shape — not `{"ok": true}`, although the
    statement carries no `RETURN` clause — with a row whose name is `spec_key`.
    `rmp graph execute -r <roadmap> --query "DROP INDEX spec_key"` prints
    `{"ok": true}`, exits 0, and a subsequent `SHOW INDEXES` no longer reports the
    row. The same three hold for
    `CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE`,
    `SHOW CONSTRAINTS`, and `DROP CONSTRAINT spec_key_uq` (see
    [Schema Management](#schema-management)).
33. A schema change survives the checkpoint and a reopen, and this is the
    criterion the destroyed-schema defect fails. Because a statement that writes
    checkpoints, the `CREATE INDEX` of criterion 32 truncates the write-ahead log
    and rewrites the snapshot before the process exits. In a **separate**
    invocation afterwards, `rmp graph execute -r <roadmap> --query "SHOW INDEXES"`
    still reports `spec_key`. Asserting this inside the creating invocation does
    **not** establish the criterion and MUST NOT be the only assertion: an
    implementation whose snapshot carries no schema at all passes that check and
    loses the index at the process boundary. For a constraint the criterion MUST
    additionally assert **enforcement** after the reopen, because a constraint
    that is merely listed is not a constraint that is applied: with
    `spec_key_uq` declared over `Spec.key` and a node already carrying
    `'user-authentication'`, a later `rmp graph execute` creating a second node
    with that same key fails, and a read-back reports one such node and not two.
    Executed against an implementation whose checkpoint dropped the constraint,
    that second create exits 0 reporting `{"ok": true}` and the duplicate is
    stored, which is the silent integrity loss this criterion exists to catch (see
    [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)).
34. Every surface reports the schema the store holds, and the surface that cannot
    report it says nothing false about it. Against the store of criterion 32,
    `rmp graph execute -r <roadmap> --query "SHOW INDEXES"` reports the row named
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
    `rmp graph execute --query "DROP INDEX spec_ord"` followed by
    `rmp graph execute --query "CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}"`
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
    `rmp graph execute` whose statement cannot finish inside the budget — an
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
    `rmp graph execute` applies is the same declaration the web graph data
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
    same JSON `rmp graph execute` returns for that statement against the same
    graph. The mode is asserted, not assumed: a socket created at whatever the
    umask yields passes every functional check in this criterion.
42. **A write through the client is durable across the server's own lifetime.**
    `rmp graph client -r <roadmap> --query "CREATE (s:Spec {key:'k'})"` prints
    `{"ok": true}` and exits 0, a subsequent read through the client reports the
    node, and after the server is stopped an `rmp graph execute` in a separate
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
45. **A served roadmap routes `rmp graph execute` through the server, and the
    criterion MUST assert that rather than the exit code.** With a server running,
    `rmp graph execute -r <roadmap> --query "CREATE (n:Probe {key:'p'})"` exits 0,
    and the node is visible to `rmp graph client` against the same running server
    — which it could not be if the invocation had opened the store itself, because
    the server holds the exclusive lock and an invocation that took the direct
    path would have failed. The same statement against the same roadmap with no
    server running also exits 0, on the direct path.
46. **A stale socket does not fail an invocation and does not make it wait.** With
    a socket file present and nothing listening,
    `rmp graph execute -r <roadmap> --query "MATCH (n) RETURN count(n)"` returns
    its result and exits 0, and it does so promptly: the criterion is asserted on
    wall-clock time, because the failure it exists against is one that waits the
    whole wait budget and then fails. The leftover socket file is still present
    afterwards (see [Server Resolution](#server-resolution), rule 1).
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
49. **The web graph data endpoint follows the same resolution rule.** With a
    server running, a `GET` of `/roadmaps/<roadmap>/graph/data` whose `q`
    parameter carries `CREATE (n:WebProbe {key:'w'})` is answered HTTP `200`, and
    the node is visible through `rmp graph client` against that same server. With
    the server stopped, the identical request is answered HTTP `200` on the direct
    path. Neither answer is established by the status alone: the read-back is what
    separates a request that reached the server from one that failed on the lock
    (see `WEB.md § Knowledge Graph from the GoGraph Store`).
50. **The socket does not outlive the server.** After `SIGINT`, `rmp graph serve`
    exits 0 and `~/.roadmaps/<roadmap>/graph.sock` is gone. The store is left in a
    state a subsequent `rmp graph execute` opens cleanly, and the write-ahead log
    is short, proving the shutdown checkpoint ran (see
    [Server Shutdown and the Drain](#server-shutdown-and-the-drain)).
51. **`--socket` lets the CLI follow a server off the derived path, and the
    criterion MUST assert both halves of the boundary.** With
    `rmp graph serve -r <roadmap> --socket <path>` running on a path that is not
    the derived one:
    - `rmp graph execute -r <roadmap> --socket <path> --query "CREATE (n:Probe {key:'p'})"`
      exits 0, and the node is visible to
      `rmp graph client -r <roadmap> --socket <path>` against that same running
      server — which establishes that the statement reached the server rather than
      the store, because the server holds the exclusive lock and a direct
      invocation would have failed on it;
    - the same `rmp graph execute` **without** `--socket` finds nothing on the
      derived path, takes the direct path, and fails with exit code 1 after the
      bounded wait, leaving the graph unchanged;
    - a `GET` of `/roadmaps/<roadmap>/graph/data` is answered HTTP `500` for the
      same reason, and no request can be made to answer otherwise, because the
      endpoint publishes no socket parameter.
    The second and third bullets are the criterion's point: they fix the residual
    as real and as confined to the surface that cannot carry the flag (see
    [Serving on a Non-Default Socket](#serving-on-a-non-default-socket)).
52. **An empty `--socket` value is refused on every subcommand that publishes the
    flag, and refused before anything is opened.** Each of
    `rmp graph execute`, `rmp graph client` and `rmp graph serve`, invoked with
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

## See Also

- CLI command contract for `graph` → `COMMANDS.md § Graph Management`
- Graph query result JSON and property-type mapping → `DATA_FORMATS.md § Graph Query Result`
- Standard input as a Cypher source → `DATA_FORMATS.md § Input`
- The sibling standard-input rule for the comment body, whose cap counts characters rather than bytes → `COMMANDS.md § Comment Body Input Source and Precedence`
- GoGraph integration, directory layout, error handling → `ARCHITECTURE.md`
- The required Go version, and the minor-version floor the GoGraph dependency contributes to it → `BUILD.md § Go Toolchain`
- Store serialisation, recovery, lock contention, and the synchronous checkpoint trade-off → `IMPLEMENTATION.md § Graph Store Concurrency`
- The value of the statement time budget, the evidence for it, and the web graph data endpoint's own handling of a statement it cuts → `WEB.md § Graph Query Time Budget`
- Graph statements executed through the web interface, and the HTTP consequences of the store lock → `WEB.md § Knowledge Graph from the GoGraph Store`
- Help skeleton and AI-help entry for `graph` → `HELP.md`
- CLI contract for `graph serve` and `graph client`, every flag and every failure → `COMMANDS.md § Graph Management`
- The exit codes each of the two subcommands can return, and the packages that implement them → `ARCHITECTURE.md § Exit Codes of the Graph Server and Client`
- The shape of the client's stdout, and the mapping that makes it identical to `graph execute`'s → `DATA_FORMATS.md § Graph Client Result`
- How the web graph data endpoint resolves a running server → `WEB.md § Knowledge Graph from the GoGraph Store`
- The timestamp every log record carries, and which output the UTC rule binds → `DATA_FORMATS.md § Dates - ISO 8601 with UTC`
- The sibling long-lived surface's logger, on the same rule and the same handler → `WEB.md § Logger Configuration`
