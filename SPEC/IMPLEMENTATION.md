# Implementation Specification

This file contains the implementation strategies that support the contracts defined in `ARCHITECTURE.md` (system design), `DATABASE.md` (schema and queries), and `MODELS.md` (domain models). It covers concurrency, caching, and performance. Any change to these areas must be reflected here before implementation, so that the strategy stays in sync with the contracts it supports.

## Table of Contents

- [Database Connections](#database-connections)
  - [Entry Point](#entry-point)
  - [DSN Construction](#dsn-construction)
  - [Where Each PRAGMA Is Applied](#where-each-pragma-is-applied)
  - [Read-Only Connections](#read-only-connections)
- [Concurrency Model](#concurrency-model)
  - [WAL Mode](#wal-mode)
  - [Connection Pooling](#connection-pooling)
  - [Busy Timeout](#busy-timeout)
  - [Retry Logic](#retry-logic)
  - [Safe Concurrent Patterns](#safe-concurrent-patterns)
  - [Anti-Patterns to Avoid](#anti-patterns-to-avoid)
  - [Race Condition Testing](#race-condition-testing)
- [Query Caching](#query-caching)
- [Graph Store Concurrency](#graph-store-concurrency)
- [Performance Considerations](#performance-considerations)
- [See Also](#see-also)

## Database Connections

Every roadmap database is opened by `internal/db`, and only by `internal/db`. This
section is the contract for how a connection is established: the entry point, the
form of the DSN, and which settings travel in the DSN rather than being executed
against an already-open connection. `BUILD.md § SQLite Driver Rules` governs the
driver version itself.

### Entry Point

Databases MUST be opened with `sqlite.NewConnector(dsn)` followed by
`sql.OpenDB(connector)`, never with `sql.Open("sqlite", dsn)`.

`NewConnector` returns a `driver.Connector` backed by the same driver the package
registers as `"sqlite"`, so a connection it opens is identical to one `sql.Open`
would have opened. What differs is when a bad DSN is reported. `sql.Open` only
looks up the registered driver; because `modernc.org/sqlite` deliberately does not
implement `driver.DriverContext`, the DSN is not examined there at all, and a
defect in it surfaces later, from whichever query first forces the pool to dial.
`NewConnector` checks what can be checked without touching the filesystem — that
the query string parses, and that the `vfs` parameters do not conflict — so the
error is attributed to opening the database, which is where it belongs. Values
that require an open database, such as an out-of-range PRAGMA value, are still
reported when the connection is made.

Neither function connects, so the connection-pool settings below keep their
meaning and are applied to the returned `*sql.DB` exactly as before.

### DSN Construction

The DSN MUST be a SQLite `file:` URI with the database path percent-encoded. It
MUST NOT be built by concatenating the path with a `?` and a query string.

The reason is that the driver splits a DSN at the first `?` and, when the string
is not prefixed with `file:`, treats everything before it as the filename and
everything after it as driver parameters. A path containing `?` therefore opens a
different file from the one intended and feeds its own tail to the parameter
parser — which, since the driver gained validated shorthand keys, can disable
foreign-key enforcement or downgrade `synchronous` on a connection the application
believes it configured itself. Percent-encoding the path removes the possibility:
no character in a path can terminate the path component or introduce a parameter.

The construction follows <https://www.sqlite.org/uri.html>:

1. Convert every `\` to `/`. This matters only on Windows.
2. Prepend `/` when the path begins with a drive letter, so a Windows path becomes
   `/C:/Users/...`.
3. Percent-encode the result and emit it as the path component of a `file:` URI.

The driver passes `SQLITE_OPEN_URI` and, for a `file:`-prefixed DSN, hands the
whole string to SQLite rather than truncating it, so SQLite decodes the path
itself. Query parameters SQLite does not recognise are passed through to the VFS
and ignored, so the driver's own parameters are inert to it.

### Where Each PRAGMA Is Applied

A PRAGMA is connection-scoped or database-level, and that determines where it is
set. Setting a connection-scoped PRAGMA with a one-shot `Exec` is a defect: it
configures whichever single pooled connection services that call and leaves the
others on the SQLite defaults, so referential integrity or lock-waiting silently
depends on which connection a query lands on.

| PRAGMA | Scope | Applied | Value |
|--------|-------|---------|-------|
| `busy_timeout` | Connection | DSN, `_busy_timeout` | `10000` |
| `foreign_keys` | Connection | DSN, `_foreign_keys` | `1` |
| `query_only` | Connection | DSN, `_query_only` | `1`, read-only opens only |
| `journal_mode` | Database | One `Exec` after opening | `WAL` |

Connection-scoped PRAGMAs MUST be carried in the DSN using the driver's validated
shorthand keys, not the verbatim `_pragma=name(value)` form. `_pragma` values are
executed as written and are not validated, and they are the one parameter class
that can still fail partway through a DSN, leaving the settings ahead of the
failure already applied. The shorthand keys are validated against a fixed accepted
set before any parameter is applied, and are applied in an order the driver fixes
— `_busy_timeout` first, `_query_only` last — independent of the order they are
written in.

Only the primary key names are used. Each of these keys has an alias (`_fk`,
`_timeout`), and when a key and its alias both appear the alias wins; supplying
both is therefore a trap and is forbidden.

`journal_mode` is database-level: WAL is recorded in the file header, survives
reopening, and applies to every connection, so it is set once with a single `Exec`
after the database is opened and MUST NOT be carried in the DSN.

### Read-Only Connections

The web interface opens databases read-only. Such a connection carries
`_query_only=1` in addition to the connection-scoped PRAGMAs above, so the SQLite
engine itself rejects every write — schema change, row mutation, and audit insert
alike — rather than relying on the calling code to refrain from writing. A
read-only open also runs no migrations, since DDL is a write. `journal_mode` is
not set on these connections: it is a write, and the database is already in WAL
mode from creation. `WEB.md § Read-Only Data Flow` states the requirement this
serves.

## Concurrency Model

Groadmap uses SQLite as its database backend with a carefully designed concurrency model for safe concurrent access.

### WAL Mode

Groadmap enables SQLite's Write-Ahead Logging (WAL) mode for better concurrency:

```sql
PRAGMA journal_mode = WAL;
```

WAL mode provides:
- **Readers don't block writers**: Multiple readers can access the database while a writer is active
- **Writers don't block readers**: Readers see a consistent snapshot of the database
- **Better performance**: Especially for read-heavy workloads

It is set once per database rather than per connection; see
[Where Each PRAGMA Is Applied](#where-each-pragma-is-applied).

### Connection Pooling

Groadmap is a single-user CLI tool, so the connection pool is sized for low
resource usage and predictable behaviour rather than high read concurrency.

```go
db.SetMaxOpenConns(2)                    // One for reads, one for writes
db.SetMaxIdleConns(1)                    // Keep one warm connection
db.SetConnMaxLifetime(30 * time.Minute)  // Recycle connections every 30 min
db.SetConnMaxIdleTime(10 * time.Minute)  // Close idle connections after 10 min
```

**Rationale**:
- **MaxOpenConns(2)**: SQLite serialises writes; a CLI process rarely benefits
  from more than one reader plus one writer in flight.
- **MaxIdleConns(1)**: A single warm connection avoids re-handshake on the
  next command without holding extra file descriptors.
- **ConnMaxLifetime(30 min)**: Bounds the maximum age of a pooled connection
  so long-running CLI sessions do not accumulate stale state.
- **ConnMaxIdleTime(10 min)**: Releases unused connections to free resources.

**Note**: Write operations remain serialised at the SQLite level regardless of
pool size. WAL mode is enabled so readers do not block writers and vice versa.

### Busy Timeout

A busy timeout is configured to prevent immediate failures when the database is locked:

```sql
PRAGMA busy_timeout = 10000;  -- 10 seconds
```

It is connection-scoped and therefore carried in the DSN, so that it holds on
every pooled connection and not only on the one that would have serviced a
one-shot `Exec`; see [Where Each PRAGMA Is Applied](#where-each-pragma-is-applied).

### Retry Logic

Groadmap implements exponential backoff retry logic for database operations:

- **Initial delay**: 100ms
- **Maximum delay**: 1000ms
- **Maximum retries**: 5 — retries only; the first attempt is not a retry
- **Maximum attempts**: 6 — one initial attempt plus at most five retries
- **Backoff pattern**: 100ms, 200ms, 400ms, 800ms, 1000ms — one wait before each retry
- **Maximum total wait**: 2500ms — 100 + 200 + 400 + 800 + 1000

**Attempts and Retries:**

"Maximum retries" counts the attempts made after the first one, so the number of
times the operation itself runs is one greater: at most six. This section states
both figures rather than the retry count alone, because an attempt count left to
the reader to derive is a figure that two implementations of the same policy can
disagree on without either of them contradicting the text.

**Wait Ordering:**

The implementation waits before each retry, and never after an attempt it does
not retry:

1. The first attempt runs immediately, with no preceding wait.
2. Each retry is preceded by the next delay of the backoff pattern: 100ms before
   the first retry, then 200ms, 400ms, 800ms and 1000ms before the second, third,
   fourth and fifth.
3. An attempt that succeeds returns its result immediately; no further wait and
   no further attempt happen.
4. An attempt that fails with an error the Retry Conditions below exclude returns
   that error immediately, without waiting.
5. No wait follows the sixth attempt. An operation whose every attempt fails with
   a retryable error waits 2500ms in total and then fails; it does not wait again
   after its final failure.

**Retry Conditions:**
- Only retry on SQLite busy/locked errors (`database is locked`, `SQLITE_BUSY`)
- Do not retry on schema errors, constraint violations, syntax errors, or invalid input errors

### Safe Concurrent Patterns

**Pattern 1: Multiple Readers**
Multiple goroutines can safely read from the database simultaneously.

**Pattern 2: Single Writer**
Only one goroutine should write at a time. Use a mutex if needed:

```go
var writeMutex sync.Mutex

func safeWrite(db *DB, task *models.Task) (int, error) {
    writeMutex.Lock()
    defer writeMutex.Unlock()
    return db.CreateTask(ctx, task)
}
```

**Pattern 3: Read-While-Writing**
Readers can safely read while a writer is active (WAL mode).

**Pattern 4: Transaction Boundaries**
Use transactions for atomic operations:

```go
db.WithTransaction(func(tx *sql.Tx) error {
    // Multiple operations within a transaction
    _, err := tx.Exec("INSERT INTO tasks ...")
    if err != nil {
        return err
    }
    _, err = tx.Exec("INSERT INTO audit ...")
    return err
})
```

### Anti-Patterns to Avoid

- **Multiple Writers Without Coordination**: Multiple uncoordinated writers may fail with "database is locked"
- **Long-Running Transactions**: Holding locks for too long blocks other operations
- **Ignoring Context Cancellation**: Always pass context for proper timeout/cancellation handling

### Race Condition Testing

Run tests with the race detector:

```bash
go test -race ./internal/db/...
```

**Test Coverage:**
- Concurrent task creation and reads
- Concurrent task updates
- Concurrent sprint operations
- Concurrent audit logging
- High concurrency stress testing

## Query Caching

The database layer implements prepared statement caching to eliminate query plan recompilation overhead for frequently executed batch operations with IN clauses.

### Problem Statement

Multiple database functions build SQL queries using `fmt.Sprintf` with `strings.Join`, creating unique query strings for each call. This prevents SQLite from caching query plans, forcing recompilation on every execution.

**Affected Operations:**
- `GetTasks` - IN clause for task IDs
- `UpdateTaskStatus` - IN clause for task IDs
- `UpdateTaskPriority` - IN clause for task IDs
- `UpdateTaskSeverity` - IN clause for task IDs
- `AddTasksToSprint` - IN clause for task IDs
- `RemoveTasksFromSprint` - IN clause for task IDs

**Current Overhead:** 20-30% on repeated batch operations.

### Cache Strategy

Pre-generate and cache query templates for common IN clause sizes to enable SQLite query plan reuse.

**Cached Sizes:**
- **Standard sizes:** 1-100 (individual caches)
- **Large batches:** 250, 500, 1000

Total cached templates: 103

### Data Structures

```go
// QueryCache stores pre-generated query templates for batch operations
type QueryCache struct {
    templates    map[string]string
    placeholders []string
    mu           sync.RWMutex
}

// Operation types for cache keys
const (
    OpGetTasks              = "get_tasks"
    OpUpdateTaskStatus      = "update_task_status"
    OpUpdateTaskPriority    = "update_task_priority"
    OpUpdateTaskSeverity    = "update_task_severity"
    OpAddTasksToSprint      = "add_tasks_to_sprint"
    OpRemoveTasksFromSprint = "remove_tasks_from_sprint"
)
```

### Batch Processing

```go
// BatchProcessor handles chunking large ID lists into manageable batches
type BatchProcessor struct {
    batchSize int
}

// ProcessChunks splits a slice of IDs into chunks and executes fn for each
func (bp *BatchProcessor) ProcessChunks(ids []int, fn func(chunk []int) error) error
```

### Performance Requirements

- 20-30% improvement in batch update operations
- Query plan cache hit rate above 90% for repeated operations
- Batch processing handles 1000+ IDs efficiently
- Thread-safe implementation verified with concurrent access

## Graph Store Concurrency

The knowledge graph is backed by the GoGraph store, which is a separate
persistence mechanism from SQLite. This section specifies how Groadmap uses that
store at runtime. The feature itself is specified in `GRAPH.md`.

### Transactional Model and Writer Serialisation

GoGraph's store is transactional, and MVCC is its only concurrency-control
mechanism. Reads observe a consistent committed snapshot. Independent write
transactions are not excluded from one another inside a single process: a
write-write collision is detected rather than prevented, on a first-updater-wins
basis, and the losing transaction receives a retriable serialization-conflict
error. Groadmap does not rely on that intra-process behaviour, because each
`rmp graph execute` invocation and each web graph request runs exactly one
transaction; that one-transaction-per-invocation model is why the conflict path is
not reachable today. Groadmap likewise uses none of the engine's MVCC-specific
entry points and issues no `MERGE` of its own, so the engine's concurrency
semantics are not observable through the product as it stands.

Groadmap does not depend on the engine to serialise access to the store. It
serialises it itself, at the process level, on a lock file that Groadmap maintains
in the roadmap's graph directory (`write.lock`). Every invocation and every web
graph request takes that lock **exclusively** before the store is opened and holds
it until after any checkpoint. There is one mode, because Groadmap does not examine
a statement and so cannot know before running one whether it will write. The
operating system releases the lock when the holding process exits, so a crashed
invocation does not strand it. This is the lock referred to throughout
[Write Contention and Recovery](#write-contention-and-recovery); the contract it
implements is specified in `GRAPH.md § Concurrency and Recovery`, which is
canonical.

The lock deliberately spans the whole open, execution, commit, checkpoint, and
write-ahead-log truncation sequence rather than the transaction alone. That is the
span that must not interleave: a second writer that had loaded the graph before the
first writer's commit would checkpoint a full snapshot of its own stale in-memory
graph and then truncate the write-ahead log that still held the first writer's
committed change, silently losing an acknowledged write. Because the sequence is
wider than a transaction, no engine-level writer exclusion would have covered it in
any case.

Durability comes from a write-ahead log (with CRC32C integrity checks) plus atomic
on-disk snapshots; opening the store runs recovery to restore the last committed
state from the snapshot and log.

### One Realisation of the Sequence

The lock, the open, the engine construction and the checkpoint described above are
implemented **once**, in `internal/graphstore`, and every surface reaches that
sequence by calling it rather than by repeating it. This is an implementation
requirement and not an incidental fact of the current layout.

The reason is the failure mode a second copy produces. Every step above is silent
when it is wrong: a lock taken after the open rather than before it still runs, a
write-ahead-log writer closed after its lock is released still returns nil, and a
snapshot written without the registered schema is indistinguishable from a correct
one until the next open finds the schema gone. Two copies of such a sequence do not
fail loudly when they diverge; they diverge and keep passing. The project has
already had two, in `internal/commands` and `internal/web`, and the only thing
holding them together was a static gate over the one divergence anybody had thought
to fence.

`GRAPH.md § Engine Constructor by Path` states the rule in its canonical form —
one construction, in that package, reached by every surface — and `internal/testenv`
enforces both it and the matching rule for the snapshot write. A surface that needs
the store behaves differently (a longer hold, a different checkpoint cadence) varies
how it *uses* the sequence; it does not acquire a copy of it.

### Process Model

1. The `rmp` CLI is a short-lived process. Each `rmp graph execute` invocation
   opens the roadmap's graph store, runs exactly one statement, commits,
   checkpoints when that transaction wrote (see
   [Synchronous Checkpoint on Write](#synchronous-checkpoint-on-write)), closes the
   store, and exits. The store is **not** held open across invocations, and it
   shares no connections, locks, or transactions with the SQLite layer. The two
   persistence mechanisms are fully independent. A web graph request follows the
   same sequence within the request.
2. Every invocation takes the store lock exclusively, opens the store, runs the
   statement through the engine's transactional path so that a change it makes is
   committed atomically, checkpoints synchronously if that transaction appended to
   the write-ahead log, and releases the lock only after that. Opening the store is
   not a read-only operation on disk even for a statement that writes nothing:
   recovery repairs an interrupted checkpoint on open, which is why the lock is
   taken before the open (see
   `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`).

### Write Contention and Recovery

1. Because every invocation acquires Groadmap's graph store lock exclusively
   before opening the store, two concurrent statements against the **same**
   roadmap contend for that lock, whatever those statements do, and so do a CLI
   invocation and a web graph request in either direction. The losing invocation
   MUST wait a bounded time and then fail; it MUST never hang indefinitely and
   MUST never corrupt the store.
2. The contention/lock failure surfaces as `utils.ErrDatabase` (exit code 1),
   consistent with treating the graph store as a database-class dependency. For a
   web graph request, it surfaces as an internal read error (HTTP 500), the status
   that endpoint already returns for a graph store that cannot be opened.
3. Every caller waits, and waits a bounded time. It retries the lock under the
   **loop and the delay ladder** of the project's single retry policy, the one
   specified for SQLite in [Retry Logic](#retry-logic): the first attempt is
   immediate, each retry is preceded by the next delay of that ladder, and no
   wait follows an attempt that is not retried. That section states the ladder
   and the wait ordering, and this rule does not restate them, so the two cannot
   diverge. What the graph store lock does **not** take from that section is its
   total. This lock has a **wait budget of its own**, the statement budget plus
   the backoff total, so the loop keeps retrying until that budget is exhausted
   rather than stopping after the five retries the SQLite policy makes.
   `GRAPH.md § Lock Contention` is canonical for that sizing rule, for the figure
   it yields, and for the measurements behind it, and this rule does not restate
   those either. The SQLite total is not reused because the two locks do not
   cover the same thing: no SQLite lock is held across a statement whose cost a
   caller chooses, since Groadmap issues every SQL statement itself, while the
   graph store lock is held across the statement its invocation carries. A wait
   sized against the SQLite total is therefore shorter than the hold it has to
   cover, and it starves the waiter. The caller retries only on lock/contention
   conditions and never on parse or execution errors. When the bounded wait is
   exhausted the invocation fails as rule 2 describes. The contract is a bounded
   wait and then failure, never an unbounded block: a caller that blocked without
   a bound would let a long statement hang a web request until the server's write
   timeout fired (see `WEB.md § HTTP Server Timeouts`). The wait is spent before
   the statement starts, so it does not consume the graph data endpoint's own
   query time budget (see `WEB.md § Graph Query Time Budget`); what has to fit
   inside that timeout is the wait and the statement together, which
   `GRAPH.md § Lock Contention` states. The reasoning behind the single policy is
   there too.
4. **A holder whose statement is a read, or runs to completion, is bounded, and
   the wait rests on that.** The variable part of a hold is the statement, and
   both surfaces run their statement under one statement time budget: the web
   graph data endpoint and `rmp graph execute` alike.
   `WEB.md § Graph Query Time Budget` is canonical for the value and this rule
   does not restate it; `GRAPH.md § Statement Time Budget` is canonical for what a
   cut statement leaves behind and for how long a cut statement holds the lock.
   Such a hold therefore has a lawful maximum, which is the precondition rule 3's
   derivation rests on, and a waiter contending with one is served.

   Three limits survive that, and none is fixed by bounding the statement.
   `GRAPH.md § Lock Contention` states all three and is canonical for them, with
   the measurements behind them:

   - The allowance rule 3's wait budget reserves for the **fixed** part of a hold
     is a constant, while the quantity it covers grows linearly with the store's
     size on disk. On a large enough graph the allowance is exhausted and the
     no-starvation guarantee lapses, with no statement cost involved at all.
     Nothing in the implementation measures a graph's size or enforces that bound.
   - A statement the budget cuts while it is **writing** overruns its deadline by
     a factor the statement itself sets, with no ceiling established, so its hold
     has no known upper bound and the wait does not cover one. A waiter can fail
     against a holder that is inside every published budget, and one such
     statement exceeds the web server's write timeout on its own.
   - A finite wait can cover only a hold that has an upper bound, and a server
     that holds the lock for its process lifetime has none, so no finite wait can
     be derived from one. The wait-based policy bounds a read and a statement that
     runs to completion; it bounds neither of the two holders above.
5. Recovery on open is expected to be transparent for a consistently committed
   store. A corrupt or unreadable store surfaces as `utils.ErrDatabase` (exit code
   1); there is no automatic graph-store repair in this version.

### Synchronous Checkpoint on Write

After a transaction that appended to the write-ahead log commits durably, the
implementation produces a self-sufficient on-disk snapshot of the committed graph
state and truncates the write-ahead log, synchronously within the same short-lived
invocation, before closing the store. A transaction that appended nothing never
checkpoints. The feature-level behaviour is specified in
`GRAPH.md § Synchronous Checkpoint on Write`; this section records the runtime
implications.

1. **Checkpoint ordering.** The checkpoint runs inside the invocation that already
   holds the graph store lock. It runs after the transaction commit and acquires no
   separate lock. Two concurrent invocations against
   the same roadmap still serialise on that one lock exactly as specified in
   [Write Contention and Recovery](#write-contention-and-recovery); the checkpoint
   does not introduce a new contention point beyond the statement itself. Holding the
   lock until the checkpoint completes is what makes the sequence safe, as described
   in [Transactional Model and Writer Serialisation](#transactional-model-and-writer-serialisation).
2. **Durability boundary.** The transaction commit is the durability boundary. The
   committed change survives recovery from the write-ahead log regardless of the
   checkpoint outcome. The snapshot is self-sufficient (it carries the
   node-identifier-to-key mapping) so that truncating the log after the snapshot
   loses no committed data and recovery can rebuild from the snapshot plus any log
   tail alone.
3. **Write-ahead-log truncation.** After the self-sufficient snapshot is durable,
   the write-ahead log is truncated. Without truncation the log would grow with
   every write for the life of the graph, and every invocation (read or write)
   would replay the full write history on open, degrading open latency in
   proportion to total history. Truncation bounds log size and keeps recovery cost
   proportional to the live graph size.
4. **Failure policy.** A checkpoint failure that occurs after the commit is
   already durable MUST NOT fail the user-visible write. The command returns its
   normal success output and exit code 0; the checkpoint failure is reported as a
   diagnostic on stderr (per `HELP.md § Error message format`) without changing the
   exit code. This is a degraded-but-correct state: the intact write-ahead log
   still recovers the committed state, and the next successful write checkpoints
   again and reconciles the snapshot. A failure before or during the commit is an
   ordinary write failure (`utils.ErrDatabase`, exit code 1), not a checkpoint
   failure, and no checkpoint is attempted.
5. **Performance trade-off.** A synchronous full snapshot on every write makes each
   write cost proportional to the live graph size, because the snapshot rewrites
   the committed state. The deliberate trade is bounded write-ahead-log growth and
   a recovery cost proportional to live graph size rather than to total write
   history. This version intentionally does **not** use a size-thresholded or
   background checkpoint; a thresholded checkpoint that snapshots only after the
   log crosses a size bound is a possible future optimisation and is out of scope
   here.

### Statements Against a Contended Store

An invocation observes the last committed state, reconstructed from the snapshot
plus the write-ahead-log tail, and that state is fixed at the moment its own store
open returned.

An invocation is **not** independent of another that holds the store. Groadmap
adds no second lock: every caller takes the one graph store lock exclusively,
before the store is opened, and holds it until after any checkpoint (see
[Transactional Model and Writer Serialisation](#transactional-model-and-writer-serialisation)).
A caller therefore waits, for a bounded time, on whichever invocation holds the
store when it tries to open it. This is not an optimisation the implementation may
skip: opening the store runs recovery, recovery repairs an interrupted checkpoint
on disk, and an unlocked open could delete or race the staging directory a
concurrent checkpoint is publishing from. The full contract, including what a
statement that writes nothing does and does not change on disk, is
`GRAPH.md § Concurrency and Recovery`.

Two consequences follow from the single mode, and both are stated rather than
discovered. A long-running statement blocks every other statement against the same
roadmap for as long as it runs, because the hold spans the execution and not only
the open. And two statements that each write nothing still serialise, where a
shared mode would have let them overlap; nothing about a statement is known before
it runs, so there is no mode to choose between.

## Performance Considerations

1. **Lazy loading**: SQLite connections only opened when needed.
2. **Prepared statements**: Pre-compiled SQLite queries for repeated operations.
3. **WAL Mode**: Use `PRAGMA journal_mode=WAL;` to improve concurrency for read/write operations.
4. **Foreign Keys**: Explicitly enable `PRAGMA foreign_keys=ON;` on every connection to enforce constraints and cascading actions.
5. **Bulk Operations**: Encapsulate multiple updates in a single transaction. Batch ID lists larger than 500 to avoid SQLite variable limits.
6. **Streaming Output**: Use `json.Encoder` for large result sets (e.g., `audit list`) to stream JSON directly to `stdout` instead of buffering.
7. **Concurrency**: Leverage Go's concurrency for independent read operations, but ensure writes are strictly sequential per roadmap file.

## See Also

- System design and module boundaries → `ARCHITECTURE.md`
- Schema, queries, and indexes → `DATABASE.md`
- Memory Layout Optimization → `MODELS.md § Memory Layout Optimization`
- Knowledge graph feature, persistence, and what Groadmap does not check about a
  statement → `GRAPH.md`
