# Concurrency Strategy

**Date:** 2026-03-18
**Version:** 1.0.0
**Applies to:** Groadmap CLI v1.0.0+

---

## Overview

Groadmap uses SQLite as its database backend. This document describes the concurrency model and safe patterns for concurrent access to the database.

---

## SQLite Concurrency Model

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

Groadmap uses a connection pool optimized for concurrent read access:

```go
db.SetMaxOpenConns(10)        // Allow concurrent readers (WAL mode supports this)
db.SetMaxIdleConns(5)         // Keep warm connections for frequent access
db.SetConnMaxLifetime(time.Hour)   // Recycle connections after 1 hour
db.SetConnMaxIdleTime(10 * time.Minute) // Close idle connections after 10 min
```

**Rationale**:
- **MaxOpenConns(10)**: WAL mode allows multiple concurrent readers. While SQLite only supports one writer at a time, reads can proceed in parallel.
- **MaxIdleConns(5)**: Maintains warm connections to avoid connection establishment overhead for read-heavy workloads.
- **ConnMaxLifetime(1 hour)**: Periodically recycles connections to prevent resource leaks and handle potential SQLite memory growth.
- **ConnMaxIdleTime(10 min)**: Closes unused connections to free resources while keeping connections alive during active use periods.

**Note**: Write operations remain serialized at the SQLite level regardless of connection pool size.

### Busy Timeout

A busy timeout is configured to prevent immediate failures when the database is locked:

```sql
PRAGMA busy_timeout = 10000;  -- 10 seconds
```

This makes SQLite wait up to 10 seconds before returning a BUSY error.

---

## Retry Logic

### Exponential Backoff

Groadmap implements exponential backoff retry logic for database operations:

- **Initial delay**: 100ms
- **Maximum delay**: 1000ms
- **Maximum retries**: 5
- **Backoff pattern**: 100ms, 200ms, 400ms, 800ms, 1000ms

### When to Retry

Only retry on SQLite busy/locked errors:

- `database is locked`
- `SQLITE_BUSY`
- `busy` with error code 5

### When NOT to Retry

Do not retry on:
- Schema errors
- Constraint violations
- Syntax errors
- Not found errors
- Invalid input errors

---

## Context and Timeouts

### Context Propagation

All database operations accept a `context.Context` parameter:

```go
func (db *DB) CreateTask(ctx context.Context, task *models.Task) (int, error)
func (db *DB) GetTask(ctx context.Context, id int) (*models.Task, error)
```

### Timeout Values

- **DefaultQueryTimeout**: 30 seconds for normal operations
- **QuickQueryTimeout**: 5 seconds for simple read operations

### Cancellation

Operations respect context cancellation. If a context is cancelled, the database operation will be interrupted.

---

## Safe Concurrent Patterns

### Pattern 1: Multiple Readers

Multiple goroutines can safely read from the database simultaneously:

```go
var wg sync.WaitGroup
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        tasks, _, _ := db.ListTasks(ctx, nil, nil, nil, nil, nil, nil, 1, 100)
        // Process tasks...
    }()
}
wg.Wait()
```

### Pattern 2: Single Writer

Only one goroutine should write at a time. Use a mutex if needed:

```go
var writeMutex sync.Mutex

func safeWrite(db *DB, task *models.Task) (int, error) {
    writeMutex.Lock()
    defer writeMutex.Unlock()
    return db.CreateTask(ctx, task)
}
```

### Pattern 3: Read-While-Writing

Readers can safely read while a writer is active (WAL mode):

```go
// Writer
go func() {
    db.CreateTask(ctx, task)
}()

// Readers (can run concurrently)
go func() {
    db.ListTasks(ctx, nil, nil, nil, nil, nil, nil, 1, 100)
}()
```

### Pattern 4: Transaction Boundaries

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

---

## Race Condition Testing

### Running Race Tests

Run tests with the race detector:

```bash
go test -race ./internal/db/...
```

### Race Test Coverage

The following scenarios are tested:

1. **Concurrent Task Creation**: Multiple goroutines creating tasks
2. **Concurrent Task Reads**: Reading tasks while they are being created
3. **Concurrent Task Updates**: Updating different tasks simultaneously
4. **Concurrent Sprint Creation**: Multiple goroutines creating sprints
5. **Concurrent Sprint Task Operations**: Adding/removing tasks from sprints
6. **Concurrent Audit Logging**: Multiple goroutines logging audit entries
7. **Concurrent Audit Reads**: Reading audit entries while logging
8. **High Concurrency Stress**: Mixed operations under high load

### Expected Behavior

- **No data races**: The race detector should not find any data races
- **Some locking errors**: Under high load, SQLite may return "database is locked" errors
- **Retry success**: The retry logic should eventually succeed
- **Data integrity**: All data should be consistent after concurrent operations

---

## Anti-Patterns to Avoid

### Anti-Pattern 1: Multiple Writers Without Coordination

```go
// BAD: Multiple uncoordinated writers
for i := 0; i < 100; i++ {
    go func() {
        db.CreateTask(ctx, task)  // May fail with "database is locked"
    }()
}
```

### Anti-Pattern 2: Long-Running Transactions

```go
// BAD: Long-running transaction
db.WithTransaction(func(tx *sql.Tx) error {
    time.Sleep(10 * time.Second)  // Holds lock for too long
    // ...
})
```

### Anti-Pattern 3: Ignoring Context Cancellation

```go
// BAD: Ignoring context
tasks, _ := db.ListTasks(nil, nil, nil, nil, nil, nil, nil, 1, 100)
// Should pass context for proper timeout/cancellation handling
```

---

## Best Practices

1. **Always use context**: Pass context to all database operations
2. **Handle timeouts**: Set appropriate timeouts for operations
3. **Use transactions**: Group related operations in transactions
4. **Expect retries**: SQLite may require retries under load
5. **Limit concurrency**: Don't create too many concurrent writers
6. **Monitor errors**: Log and monitor "database is locked" errors
7. **Test with race detector**: Regularly run tests with `-race` flag

---

## Related Files

- `internal/db/connection.go`: Connection configuration
- `internal/db/race_test.go`: Race condition tests
- `.github/workflows/ci.yml`: CI pipeline with race detection

---

## References

- [SQLite WAL Mode](https://sqlite.org/wal.html)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
- [database/sql Package](https://pkg.go.dev/database/sql)
