# Query Cache Specification

## Overview

This document specifies the prepared statement caching system for the Groadmap database layer. The cache eliminates query plan recompilation overhead for frequently executed batch operations with IN clauses.

## Problem Statement

Multiple database functions build SQL queries using `fmt.Sprintf` with `strings.Join`, creating unique query strings for each call. This prevents SQLite from caching query plans, forcing recompilation on every execution.

**Affected Functions:**
- `GetTasks` - IN clause for task IDs
- `UpdateTaskStatus` - IN clause for task IDs
- `UpdateTaskPriority` - IN clause for task IDs
- `UpdateTaskSeverity` - IN clause for task IDs
- `AddTasksToSprint` - IN clause for task IDs
- `RemoveTasksFromSprint` - IN clause for task IDs

**Current Overhead:** 20-30% on repeated batch operations.

## Solution Architecture

### Cache Strategy

Pre-generate and cache query templates for common IN clause sizes to enable SQLite query plan reuse.

### Cached Sizes

The cache supports the following IN clause sizes:
- **Standard sizes:** 1-100 (individual caches)
- **Large batches:** 250, 500, 1000

Total cached templates: 103

### Data Structures

```go
// QueryCache stores pre-generated query templates for batch operations
type QueryCache struct {
    // templates maps operation name to cached queries
    // Key format: "{operation}_{size}"
    templates map[string]string

    // placeholders caches pre-generated placeholder strings
    // Index 0 = "?", Index 1 = "?,?", etc.
    placeholders []string

    // mu protects templates for thread-safe access
    mu sync.RWMutex
}

// Operation types for cache keys
const (
    OpGetTasks           = "get_tasks"
    OpUpdateTaskStatus   = "update_task_status"
    OpUpdateTaskPriority = "update_task_priority"
    OpUpdateTaskSeverity = "update_task_severity"
    OpAddTasksToSprint   = "add_tasks_to_sprint"
    OpRemoveTasksFromSprint = "remove_tasks_from_sprint"
)
```

### Cache Initialization

```go
// NewQueryCache creates and initializes a query cache with pre-generated templates
func NewQueryCache() *QueryCache {
    qc := &QueryCache{
        templates:    make(map[string]string),
        placeholders: make([]string, 1001), // 0-1000
    }

    // Pre-generate placeholder strings
    for i := 0; i <= 1000; i++ {
        qc.placeholders[i] = generatePlaceholders(i)
    }

    // Pre-generate query templates for all operations
    qc.initializeTemplates()

    return qc
}
```

### Query Template Generation

```go
// GetQuery retrieves a cached query template for the given operation and batch size
func (qc *QueryCache) GetQuery(operation string, size int) string {
    // Clamp size to nearest cached value
    cacheSize := qc.normalizeSize(size)

    key := fmt.Sprintf("%s_%d", operation, cacheSize)

    qc.mu.RLock()
    template, exists := qc.templates[key]
    qc.mu.RUnlock()

    if exists {
        return template
    }

    // Generate on-demand for non-standard sizes
    return qc.generateQuery(operation, size)
}

// normalizeSize returns the nearest cached size for a given batch size
func (qc *QueryCache) normalizeSize(size int) int {
    if size <= 0 {
        return 1
    }
    if size <= 100 {
        return size
    }
    if size <= 250 {
        return 250
    }
    if size <= 500 {
        return 500
    }
    return 1000
}
```

### Batch Processing

```go
// BatchProcessor handles chunking large ID lists into manageable batches
type BatchProcessor struct {
    batchSize int
}

// NewBatchProcessor creates a batch processor with specified chunk size
func NewBatchProcessor(batchSize int) *BatchProcessor {
    return &BatchProcessor{batchSize: batchSize}
}

// ProcessChunks splits a slice of IDs into chunks and executes fn for each
func (bp *BatchProcessor) ProcessChunks(ids []int, fn func(chunk []int) error) error {
    for i := 0; i < len(ids); i += bp.batchSize {
        end := i + bp.batchSize
        if end > len(ids) {
            end = len(ids)
        }

        if err := fn(ids[i:end]); err != nil {
            return err
        }
    }
    return nil
}
```

## Integration with DB Layer

### DB Type Extension

```go
// DB wraps sql.DB with query caching capabilities
type DB struct {
    *sql.DB
    queryCache *QueryCache
    batchProc  *BatchProcessor
}
```

### Cached Query Functions

```go
// GetTasksCached retrieves tasks by IDs using cached query templates
func (db *DB) GetTasksCached(ctx context.Context, ids []int) ([]models.Task, error) {
    var allTasks []models.Task

    err := db.batchProc.ProcessChunks(ids, func(chunk []int) error {
        query := db.queryCache.GetQuery(OpGetTasks, len(chunk))
        // Execute query with chunk...
        tasks, err := db.executeGetTasks(ctx, query, chunk)
        if err != nil {
            return err
        }
        allTasks = append(allTasks, tasks...)
        return nil
    })

    return allTasks, err
}
```

## Thread Safety

The query cache is designed for concurrent access:

1. **Template Generation:** Templates are pre-generated during initialization (no lock needed for reads)
2. **Dynamic Generation:** On-demand generation uses write lock
3. **Placeholder Access:** Pre-generated placeholders are immutable after initialization
4. **Batch Processing:** Each chunk is processed independently

## Performance Requirements

### Benchmarks

```go
// BenchmarkQueryCache benchmarks cached vs uncached queries
func BenchmarkQueryCache(b *testing.B) {
    // Benchmark GetTasks with 100 IDs
    // Expected: 20-30% improvement
}

// BenchmarkBatchProcessing benchmarks batch chunking
func BenchmarkBatchProcessing(b *testing.B) {
    // Benchmark processing 1000 IDs in batches of 100
    // Expected: Linear scaling
}
```

### Acceptance Criteria

- [ ] 20-30% improvement in batch update operations
- [ ] Memory allocations reduced in batch operations
- [ ] Query plan cache hit rate above 90% for repeated operations
- [ ] Batch processing handles 1000+ IDs efficiently
- [ ] Thread-safe implementation verified with concurrent access
- [ ] All existing tests pass without modification
- [ ] No regression in error handling

## Migration Path

1. **Phase 1:** Implement QueryCache type and tests
2. **Phase 2:** Integrate into DB type
3. **Phase 3:** Migrate one function at a time (GetTasks first)
4. **Phase 4:** Add benchmarks and verify performance gains
5. **Phase 5:** Complete migration of remaining functions

## Backward Compatibility

- Existing function signatures remain unchanged
- New cached functions use "Cached" suffix (e.g., `GetTasksCached`)
- Gradual migration allows testing at each step
- Original implementations remain available as fallback

## Files

| File | Purpose |
|------|---------|
| `internal/db/query_cache.go` | QueryCache implementation |
| `internal/db/batch.go` | BatchProcessor implementation |
| `internal/db/bench_test.go` | Performance benchmarks |

## References

- TASK-P002 in IMPLEMENTATION_PLAN.md
- SQLite Query Plan Caching: https://www.sqlite.org/queryplanner.html
