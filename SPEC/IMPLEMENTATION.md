# Implementation Specification

This file contains the implementation strategies that support the contracts defined in `ARCHITECTURE.md` (system design), `DATABASE.md` (schema and queries), and `MODELS.md` (domain models). It covers concurrency, caching, and performance. Any change to these areas must be reflected here before implementation, so that the strategy stays in sync with the contracts it supports.

## Table of Contents

- [Concurrency Model](#concurrency-model)
  - [WAL Mode](#wal-mode)
  - [Connection Pooling](#connection-pooling)
  - [Busy Timeout](#busy-timeout)
  - [Retry Logic](#retry-logic)
  - [Safe Concurrent Patterns](#safe-concurrent-patterns)
  - [Anti-Patterns to Avoid](#anti-patterns-to-avoid)
  - [Race Condition Testing](#race-condition-testing)
- [Query Caching](#query-caching)
- [Connection Caching](#connection-caching)
- [Graph Store Concurrency](#graph-store-concurrency)
- [Performance Considerations](#performance-considerations)
- [See Also](#see-also)

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

### Retry Logic

Groadmap implements exponential backoff retry logic for database operations:

- **Initial delay**: 100ms
- **Maximum delay**: 1000ms
- **Maximum retries**: 5
- **Backoff pattern**: 100ms, 200ms, 400ms, 800ms, 1000ms

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

## Connection Caching

The database layer implements connection caching to eliminate connection establishment overhead (10-50ms per command) by reusing database connections within the same process lifetime.

### Problem Statement

Every CLI command opens and closes the database connection, incurring:
- **Connection establishment:** 5-20ms
- **Schema validation:** 2-10ms
- **File descriptor operations:** 3-10ms
- **Total overhead:** 10-50ms per command

### Cache Design

A process-level connection cache that:
- Keys connections by roadmap name
- Returns existing connections when available
- Validates connection health before reuse
- Cleans up on process exit

### Data Structures

```go
// ConnectionCache manages database connections for the process lifetime
type ConnectionCache struct {
    connections map[string]*CachedConnection
    mu          sync.RWMutex
    cleanupOnce sync.Once
}

// CachedConnection wraps a database connection with metadata
type CachedConnection struct {
    db          *DB
    roadmapName string
    createdAt   time.Time
    lastUsed    time.Time
    useCount    int
}
```

### Cache Operations

```go
// OpenCached returns a cached connection for the roadmap, or creates a new one
func (cc *ConnectionCache) OpenCached(roadmapName string) (*DB, error)

// Get retrieves a cached connection without creating a new one
func (cc *ConnectionCache) Get(roadmapName string) *DB

// Store adds a connection to the cache
func (cc *ConnectionCache) Store(roadmapName string, db *DB)

// Remove deletes a connection from the cache
func (cc *ConnectionCache) Remove(roadmapName string)

// HealthCheck verifies a connection is still alive
func (cc *ConnectionCache) HealthCheck(db *DB) error

// CloseAll closes all cached connections
func (cc *ConnectionCache) CloseAll() error
```

### Global Cache Instance

```go
// globalCache is the process-level connection cache
var globalCache = NewConnectionCache()

// OpenCached is a convenience function that uses the global cache
func OpenCached(roadmapName string) (*DB, error)

// CloseAllCached closes all cached connections
func CloseAllCached() error
```

### Performance Requirements

- Second command reuses existing connection
- Connection health check validates liveness
- Dead connections are removed from cache and recreated
- Process exit closes all cached connections
- Concurrent access to cache is thread-safe
- Memory usage remains stable (no connection leaks)

## Graph Store Concurrency

The knowledge graph is backed by the GoGraph store, which is a separate
persistence mechanism from SQLite. This section specifies how Groadmap uses that
store at runtime. The feature itself is specified in `GRAPH.md`.

### Single-Writer Transactional Model

GoGraph's store is single-writer and transactional: writes are serialised through
one writer while reads observe a consistent committed snapshot. Durability comes
from a write-ahead log (with CRC32C integrity checks) plus atomic on-disk
snapshots; opening the store runs recovery to restore the last committed state
from the snapshot and log.

### Process Model

1. The `rmp` CLI is a short-lived process. Each `rmp graph` invocation opens the
   roadmap's graph store, runs exactly one query, commits any write, closes the
   store, and exits. The store is **not** held open across invocations and is
   **not** part of the SQLite connection cache (see
   [Connection Caching](#connection-caching)). The two persistence mechanisms
   share no connections, locks, or transactions.
2. Read subcommands open the store, run the query through the engine's read path,
   stream the result to stdout, and close. Write subcommands run the query through
   the engine's transactional path so the change is committed atomically.

### Write Contention and Recovery

1. Because the store is single-writer, two concurrent `rmp graph` write
   invocations against the **same** roadmap may contend for the writer. The losing
   invocation MUST fail fast rather than hang indefinitely or corrupt the store.
2. The contention/lock failure surfaces as `utils.ErrDatabase` (exit code 1),
   consistent with treating the graph store as a database-class dependency.
3. A bounded retry on a graph-store lock uses the **same** bounded
   exponential-backoff policy specified for SQLite in
   [Concurrency Model](#concurrency-model) (a small bounded number of attempts,
   exponential backoff, retrying only on lock/contention conditions and never on
   parse or validation errors). The contract is fail-fast with a bounded wait,
   not an unbounded block.
4. Recovery on open is expected to be transparent for a consistently committed
   store. A corrupt or unreadable store surfaces as `utils.ErrDatabase` (exit code
   1); there is no automatic graph-store repair in this version.

### Reads During Writes

A read invocation observes the last committed snapshot. It does not block on, and
is not blocked by, an in-flight writer in a different process, subject to the
store's own consistency guarantees. Groadmap does not add a separate read lock.

## Performance Considerations

1. **Lazy loading**: SQLite connections only opened when needed.
2. **Prepared statements**: Pre-compiled SQLite queries for repeated operations.
3. **WAL Mode**: Use `PRAGMA journal_mode=WAL;` to improve concurrency for read/write operations.
4. **Foreign Keys**: Explicitly enable `PRAGMA foreign_keys=ON;` on every connection to enforce constraints and cascading actions.
5. **Bulk Operations**: Encapsulate multiple updates in a single transaction. Batch ID lists larger than 500 to avoid SQLite variable limits.
6. **Streaming Output**: Use `json.Encoder` for large result sets (e.g., `audit list`) to stream JSON directly to `stdout` instead of buffering.
7. **Concurrency**: Leverage Go's concurrency for independent read operations, but ensure writes are strictly sequential per roadmap file.

## See Also

- `ARCHITECTURE.md` § System design and module boundaries
- `DATABASE.md` § Schema, queries, and indexes
- `MODELS.md` § Memory Layout Optimization
- `GRAPH.md` § Knowledge graph feature, persistence, and guard rails
