# Connection Cache Specification

## Overview

This document specifies the connection caching system for the Groadmap CLI. The cache eliminates connection establishment overhead (10-50ms per command) by reusing database connections within the same process lifetime.

## Problem Statement

Every CLI command opens and closes the database connection, incurring:
- **Connection establishment**: 5-20ms
- **Schema validation**: 2-10ms
- **File descriptor operations**: 3-10ms
- **Total overhead**: 10-50ms per command

For scripts with multiple operations, this overhead accumulates significantly.

## Solution Architecture

### Cache Design

A process-level connection cache that:
- Keys connections by roadmap name
- Returns existing connections when available
- Validates connection health before reuse
- Cleans up on process exit

### Data Structures

```go
// ConnectionCache manages database connections for the process lifetime.
// It eliminates connection establishment overhead for repeated operations
type ConnectionCache struct {
    // connections maps roadmap names to cached database connections
    connections map[string]*CachedConnection

    // mu protects connections for thread-safe access
    mu sync.RWMutex

    // once ensures cleanup runs only once on process exit
    cleanupOnce sync.Once
}

// CachedConnection wraps a database connection with metadata
type CachedConnection struct {
    db        *DB
    roadmapName string
    createdAt time.Time
    lastUsed  time.Time
    useCount  int
}
```

### Cache Operations

```go
// OpenCached returns a cached connection for the roadmap, or creates a new one.
// If a cached connection exists and is healthy, it returns the cached connection.
// Otherwise, it creates a new connection and caches it.
func (cc *ConnectionCache) OpenCached(roadmapName string) (*DB, error)

// Get retrieves a cached connection without creating a new one.
// Returns nil if no cached connection exists.
func (cc *ConnectionCache) Get(roadmapName string) *DB

// Store adds a connection to the cache.
func (cc *ConnectionCache) Store(roadmapName string, db *DB)

// Remove deletes a connection from the cache.
func (cc *ConnectionCache) Remove(roadmapName string)

// HealthCheck verifies a connection is still alive.
func (cc *ConnectionCache) HealthCheck(db *DB) error

// CloseAll closes all cached connections.
// Should be called on process exit.
func (cc *ConnectionCache) CloseAll() error
```

### Global Cache Instance

```go
// globalCache is the process-level connection cache
var globalCache = NewConnectionCache()

// OpenCached is a convenience function that uses the global cache
func OpenCached(roadmapName string) (*DB, error) {
    return globalCache.OpenCached(roadmapName)
}

// CloseAllCached closes all cached connections
func CloseAllCached() error {
    return globalCache.CloseAll()
}
```

### Process Exit Cleanup

```go
// init registers cleanup on process exit
func init() {
    // Register cleanup function to run on exit
    atexit.Register(func() {
        globalCache.CloseAll()
    })
}
```

## Implementation Details

### Connection Health Check

```go
// isHealthy checks if a database connection is still alive
func isHealthy(db *DB) bool {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    err := db.PingContext(ctx)
    return err == nil
}
```

### Stale Connection Handling

```go
// getOrCreate returns a cached connection or creates a new one
func (cc *ConnectionCache) getOrCreate(roadmapName string) (*DB, error) {
    cc.mu.RLock()
    cached, exists := cc.connections[roadmapName]
    cc.mu.RUnlock()

    if exists {
        // Check if connection is still healthy
        if isHealthy(cached.db) {
            cached.lastUsed = time.Now()
            cached.useCount++
            return cached.db, nil
        }

        // Connection is dead, remove it
        cc.Remove(roadmapName)
        cached.db.Close()
    }

    // Create new connection
    db, err := Open(roadmapName)
    if err != nil {
        return nil, err
    }

    // Cache the new connection
    cc.Store(roadmapName, db)
    return db, nil
}
```

## Thread Safety

The cache is designed for concurrent access:

1. **Read operations**: Use RLock for multiple concurrent readers
2. **Write operations**: Use Lock for exclusive access
3. **Connection access**: Each goroutine gets its own connection reference
4. **Cleanup**: Uses sync.Once to ensure single execution

## Backward Compatibility

- Existing `Open()` function remains unchanged
- New `OpenCached()` provides caching behavior
- Gradual migration possible
- Original implementations remain available as fallback

## Performance Requirements

### Benchmarks

```go
// BenchmarkConnectionCache benchmarks cached vs uncached opens
func BenchmarkConnectionCache(b *testing.B) {
    // Benchmark OpenCached (should be ~50x faster after first call)
    // Benchmark Open (baseline)
}
```

### Acceptance Criteria

- [ ] Second command reuses existing connection (verified with logging)
- [ ] Connection health check validates liveness
- [ ] Dead connections are removed from cache and recreated
- [ ] Process exit closes all cached connections
- [ ] Concurrent access to cache is thread-safe
- [ ] Memory usage remains stable (no connection leaks)
- [ ] All existing tests pass

## Files

| File | Purpose |
|------|---------|
| `internal/db/cache.go` | ConnectionCache implementation |
| `internal/db/cache_test.go` | Cache tests and benchmarks |

## References

- TASK-P004 in IMPLEMENTATION_PLAN.md
- SPEC/CONCURRENCY.md for connection management
