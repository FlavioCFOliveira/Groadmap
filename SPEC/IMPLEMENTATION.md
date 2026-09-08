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

**Groadmap has one retry policy, and one package owns the whole of it.** One
loop, one worst-case total wait, and a classifier the calling site supplies. A
caller never writes the loop, the sleep, or the attempt count out for itself:
three subsystems once did, two of them read the retry count as an attempt count,
and both waited four times where the policy promised five. The loop, and not the
constants, is therefore what is shared.

**What the policy does not have is a single delay shape.** It publishes two, and
each caller selects one. The two conditions this project retries are not the same
condition, and the delay that is right for one is measurably wrong for the other:

| Shape | Delay before each retry | Retried under it |
|-------|-------------------------|------------------|
| **The fixed ladder** | The next rung of the ladder below, taken in order | A SQLite busy or locked error; a wait for the graph store's exclusive advisory lock |
| **Full jitter** | A duration drawn uniformly at random between zero and a ceiling, the ceiling doubling from 5ms and then held at 250ms | A retriable serialisation conflict returned by a graph server |

The two shapes share everything else: the loop, the wait ordering, the rule that
a caller supplies the classifier and nothing more, and the **maximum total wait
of 2500ms**. A shape is an entry point of the one package that owns retrying. It
is never a constant moved out of that package for one caller's benefit, and never
a private loop written beside it — a second loop is the defect that produced the
single-policy rule in the first place.

**The fixed ladder:**

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
not retry. The ordering governs both shapes. Rules 1, 3 and 4 hold verbatim
under either; rules 2 and 5 are written with the fixed ladder's values, and under
full jitter the delay before each retry is the draw described below and the last
attempt is the twentieth rather than the sixth:

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

These conditions are the classifier of the SQLite caller. Every other caller
supplies its own and takes nothing else from this one: the graph store lock
retries on lock contention alone (see
[Write Contention and Recovery](#write-contention-and-recovery), rule 3), and the
graph client retries on the serialisation conflict alone
(`GRAPH.md § Concurrency Inside the Server`).

**Full jitter:**

- **Delay before each retry**: a duration drawn uniformly at random from the
  interval that runs from zero to the ceiling then in force. Zero is a possible
  draw and the ceiling is a possible draw.
- **Ceiling**: 5ms before the first retry, doubling before each subsequent one —
  5, 10, 20, 40, 80, 160ms — and then held at 250ms for every retry after that.
  The ceiling grows monotonically; the delay does not, because each one is drawn
  independently, so a later delay may be shorter than an earlier one.
- **Maximum attempts**: 20 — one initial attempt plus at most nineteen retries.
  The cap is load-bearing rather than decorative: a draw may be near zero, so the
  total wait alone does not bound how many times the loop turns.
- **Maximum total wait**: 2500ms, the same total the fixed ladder spends. The
  loop stops as soon as that total is spent, so the shape changes how the waiting
  is distributed and never how long a caller can be made to wait.

**Why a second shape exists, stated as the measurement that produced it rather
than as a preference.** A first-updater-wins serialisation conflict invites the
reading that no delay is needed at all — the winner has already committed, so the
loser should succeed on its next attempt. Measured against a real server, that
reading is not merely suboptimal, it is a congestion collapse: retrying
immediately, six attempts, failed **79.9%** of statements where the fixed ladder,
under the identical load in the same experiment, failed **0.15%**. A loser that
waits removes itself from the contending set; a loser that retries at once keeps
that set saturated. **The delay is load shedding, and the conflict rate is a
function of the offered load the retries themselves create.**

Once the delay is understood as load shedding, the shape follows from what sheds
load best inside a fixed budget. Measured head to head under identical load on
one server, with sixteen and then sixty-four concurrent writers all updating a
single node:

| Shape, all inside 2500ms | Exhausted, 16 writers | Exhausted, 64 writers | Worst observed wait | Attempts per statement |
|--------------------------|----------------------|-----------------------|---------------------|------------------------|
| The fixed ladder | 0.08-0.30% | 0.86-1.46% | 2.5s | 1.07-1.33 |
| Full jitter, ceiling 5 to 250ms | 0.000% (0 in 18,000) | 0.07-0.22% | 1.6-2.5s | 2.19-3.48 |

Full jitter removes the failure entirely at sixteen writers, cuts it by between
four and thirteen times at sixty-four, holds a worst case **shorter** than the
fixed ladder's rather than longer, halves the 99th-percentile wait at sixty-four
writers, and raises throughput by 15-45%. What it costs is server work: about
2.6 times the attempts per statement under contention, and nothing at all when
there is no contention, because an uncontended statement never reaches a retry
under either shape.

**Two shapes that were measured and rejected, recorded so that they are not
measured again.** Jitter with a ceiling that does not grow is adequate at sixteen
writers (0.03-0.08%) and collapses at sixty-four (8.7-9.0%): a fixed cap of a few
tens of milliseconds cannot shed enough load. A ceiling that grows but stops at
100ms is **worse than the fixed ladder** at sixty-four writers (1.21-1.48%). The
ceiling has to grow and it has to reach a few hundred milliseconds; the cap, and
not the randomisation alone, is what sheds the load.

**Lengthening the total instead of reshaping it was measured and is dominated.**
Walking the fixed ladder for 6 seconds rather than 2500ms buys one decimal order
of magnitude for five extra seconds of worst case, which is less than full jitter
buys for none. It also collides with a published derivation: the graph store's
wait budget is the statement budget plus this policy's total
(`GRAPH.md § Lock Contention`), and a caller that waited 6 seconds on a conflict
would sit within 1.5 seconds of the deadline at which its failure is reported as
a server that did not answer and a statement whose outcome is unknown — which,
for a conflict whose loser provably committed nothing, would be false.

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
error. Every statement now runs inside `rmp graph serve`, which runs many
transactions concurrently in one process, so the conflict path is reachable for
any statement and a client retries a serialisation conflict rather than surfacing
it. It was unreachable only while a caller could run its own single transaction in
a process of its own, which no caller does now.
`GRAPH.md § Concurrency Inside the Server` is canonical for that. The retry runs
under the loop of [Retry Logic](#retry-logic) like every other retry in this
project, and under that policy's **full-jitter** delay shape rather than its fixed
ladder, because a conflict is a contention failure whose rate is a function of the
load the retries themselves offer; that section is canonical for both shapes and
for the measurements that separate them.

Groadmap does not depend on the engine to serialise access to the store between
processes. It serialises it itself, at the process level, on a lock file that
Groadmap maintains in the roadmap's graph directory (`write.lock`).
**`rmp graph serve` is the only process that takes it**: it takes the lock
**exclusively** before it opens the store and holds it for its process lifetime.
No caller takes it, because no caller opens a store
(`GRAPH.md § Server Resolution`). There is one mode, because there is one holder,
and the lock's remaining purpose is to admit one server per roadmap. The operating
system releases the lock when the holding process exits, so a crashed server does
not strand it. This is the lock referred to throughout
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
`rmp graph serve` is that case rather than an exception to it: it holds the
sequence open for its process lifetime and checkpoints on its own cadence
(`GRAPH.md § Durability and Checkpointing in a Long-Lived Process`), through the
same one realisation.

The same reasoning applies a second time to the two things the graph server
introduced. Deciding whether a roadmap is served, and speaking the protocol to a
server that is, are implemented **once**, in `internal/graphclient`, and reached by
`rmp graph client` and by `internal/web` alike — the second by calling the package
in its own process, never by spawning the subcommand
(`GRAPH.md § The Bolt Client`). A second resolution rule would be a second set of
answers to the questions `GRAPH.md § Server Resolution` settles, and the two copies
would diverge silently.
`ARCHITECTURE.md § 9. internal/graphclient/ and reaching a graph server` records
the boundary.

### Process Model

1. The `rmp` CLI is a short-lived process, with one exception: `rmp graph serve`
   holds the store, its engine, and its lock for the life of the process
   (`GRAPH.md § The Dedicated Graph Server`). It opens the store once, at startup,
   and closes it at shutdown. No other process opens a graph store: an
   `rmp graph client` invocation resolves the roadmap's socket, sends one
   statement, reads the answer and exits, and a web graph request does the same
   within the request. The graph store shares no connections, locks, or
   transactions with the SQLite layer; the two persistence mechanisms are fully
   independent.
2. The server takes the store lock exclusively, opens the store, and thereafter
   runs each statement it is sent through the engine's transactional path so that
   a change it makes is committed atomically. It folds the write-ahead log on its
   own cadence and at shutdown, and releases the lock last. Opening the store is
   not a read-only operation on disk: recovery repairs an interrupted checkpoint on
   open, which is why the lock is taken before the open — and, since the open now
   happens once per server rather than once per statement, that repair happens once
   per server too (see
   `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`).

### Write Contention and Recovery

1. Because `rmp graph serve` acquires Groadmap's graph store lock exclusively
   before opening the store, and holds it for its process lifetime, two servers
   started against the **same** roadmap contend for that lock and exactly one of
   them runs. No statement contends for it, because no caller takes it. The losing
   server MUST wait a bounded time and then fail; it MUST never hang indefinitely
   and MUST never corrupt the store.
2. The contention/lock failure surfaces as `utils.ErrGraphStore` (exit code 1),
   the sentinel that names the store whose lock could not be taken. No web graph
   request can reach this failure, because no request takes the lock; a request
   whose roadmap has no server running is answered HTTP 503 for the different
   reason that the graph is unavailable until a server is started
   (`WEB.md § Knowledge Graph from the GoGraph Store`).
3. A server that finds the lock held waits, and waits a bounded time. It retries the lock under the
   **loop and the fixed-ladder delay shape** of the project's single retry
   policy, the one specified for SQLite in [Retry Logic](#retry-logic): the
   first attempt is immediate, each retry is preceded by the next delay of that
   ladder, and no wait follows an attempt that is not retried. That section
   states the ladder and the wait ordering, and this rule does not restate them,
   so the two cannot diverge. What the graph store lock does **not** take from
   that section is its total. This lock has a **wait budget of its own**, the
   statement budget plus the backoff total, so the loop keeps retrying until that
   budget is exhausted rather than stopping after the five retries the SQLite
   policy makes.
   `GRAPH.md § Lock Contention` is canonical for that sizing rule, for the figure
   it yields, and for the measurements behind it, and this rule does not restate
   those either. The SQLite total is not reused because the two locks do not
   cover the same thing: no SQLite lock is held across a statement whose cost a
   caller chooses, since Groadmap issues every SQL statement itself, while the
   graph store lock is held across an outgoing server's whole drain and shutdown.
   A wait sized against the SQLite total is therefore shorter than the tail it has
   to cover. The server retries only on lock/contention conditions and never on
   parse or execution errors. When the bounded wait is exhausted the server does
   not start, as rule 2 describes. The contract is a bounded wait and then
   failure, never an unbounded block. Nothing in a web request is exposed to this
   wait, because no request takes the lock (see rule 2); what a request spends is
   fixed by `WEB.md § HTTP Server Timeouts` and consists of the resolution probe
   and the backstop deadline alone.
4. **What the wait must cover is an outgoing server's tail, and that tail is
   bounded by the statement budget plus the fixed part.** The only contention left
   on this lock is between two `rmp graph serve` processes for the same roadmap:
   an incoming one waits for an outgoing one to finish draining, fold its
   write-ahead log and close the store. The drain is bounded by the statement time
   budget, `WEB.md § Graph Query Time Budget` being canonical for the value, and
   the fold and close are the fixed part.
   `GRAPH.md § Statement Time Budget` is canonical for what a cut statement leaves
   behind.

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
   - A finite wait can cover only a hold that has an upper bound, and
     `rmp graph serve` holds the lock for its process lifetime, which has none, so
     no finite wait can be derived from it. That limit is not reached by bounding
     the statement and is not meant to be: no caller takes this lock at all, so no
     caller waits on a running server (`GRAPH.md § Server Resolution`). What is
     still exposed to it is a second server started against a roadmap the first is
     already serving, which is refused rather than queued, and that refusal is the
     interlock rather than a defect.
5. Recovery on open is expected to be transparent for a consistently committed
   store. A corrupt or unreadable store surfaces as `utils.ErrGraphStore` (exit
   code 1); there is no automatic graph-store repair in this version.

### Synchronous Checkpoint on Write

After a transaction that appended to the write-ahead log commits durably, a fold
of that log into a self-sufficient on-disk snapshot is owed. `rmp graph serve` is
the only process that runs one, because it is the only process that opens a store;
it folds on a cadence while it runs and again at shutdown when the log has grown.
A transaction that appended nothing owes no fold. The feature-level behaviour is
specified in `GRAPH.md § Synchronous Checkpoint on Write` and
`GRAPH.md § Durability and Checkpointing in a Long-Lived Process`; this section
records the runtime implications.

1. **Checkpoint ordering.** A fold runs inside the process that already holds the
   graph store lock, and acquires no separate lock. The in-flight fold is driven
   through the engine's own commit serialiser, so what it captures is a real
   transaction boundary rather than a graph caught mid-commit; the shutdown fold
   runs after the drain, when no statement is in flight. Holding the lock across
   the whole sequence is what makes it safe, as described in
   [Transactional Model and Writer Serialisation](#transactional-model-and-writer-serialisation).
2. **Durability boundary.** The transaction commit is the durability boundary, and
   it precedes the acknowledgement the client reads. The committed change survives
   recovery from the write-ahead log regardless of the fold's outcome. The snapshot
   is self-sufficient — it carries the node-identifier-to-key mapping, the
   tombstone set and the registered schema — so that truncating the log after the
   snapshot loses no committed data and recovery can rebuild from the snapshot plus
   any log tail alone.
3. **Write-ahead-log truncation.** After the self-sufficient snapshot is durable,
   the write-ahead log is truncated. Without truncation the log would grow for the
   whole life of the server, and the next server to open the store would replay
   that history, degrading open latency in proportion to it. Truncation bounds log
   size and keeps recovery cost proportional to the live graph size.
4. **Failure policy.** A fold that fails after the commit is already durable MUST
   NOT fail the write the client was acknowledged for. The client's result and exit
   code are unchanged; the failure is reported on the server's own stderr without
   reaching the caller. This is a degraded-but-correct state: the intact
   write-ahead log still recovers the committed state, and the next successful fold
   reconciles the snapshot. A failure before or during the commit is an ordinary
   write failure (`utils.ErrGraphEngine`, exit code 1 at the caller), not a fold
   failure, and no fold is attempted.
5. **Performance trade-off.** A full snapshot makes each fold cost proportional to
   the live graph size, because the snapshot rewrites the committed state. That
   cost is why a long-lived server folds on a cadence rather than after every
   committed write: doing the latter would make every write cost the whole live
   graph while its neighbours waited for the quiesce the capture takes. The
   cadence's value is set on measurement and is not fixed in this specification
   (`GRAPH.md § Durability and Checkpointing in a Long-Lived Process`, rule 6).

### Statements Against a Contended Store

A statement observes the last committed state as the store's MVCC presents it,
inside the one server process that holds the store open.

**Statements no longer contend on the graph store lock, because no caller takes
it.** That lock is taken once by `rmp graph serve` and held for its process
lifetime, so it serialises servers rather than statements (see
[Transactional Model and Writer Serialisation](#transactional-model-and-writer-serialisation)).
Concurrency between statements is resolved inside the server by the store's MVCC:
readers never block, writers do not exclude one another, and a write-write
collision is detected rather than prevented, on a first-updater-wins basis. The
client retries the loser. `GRAPH.md § Concurrency Inside the Server` is canonical
for that, and `GRAPH.md § Concurrency and Recovery` for the lock's contract.

Two consequences follow, and both are stated rather than discovered. A
long-running statement no longer blocks every other statement against the same
roadmap: two statements against one server run at the same time. And a caller that
meets a serialisation conflict on every attempt is meeting contention over one hot
node rather than concurrency in general, which is what makes spreading writes
across distinct nodes the remedy rather than reducing the writer count.

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
