# Implementation Plan

**Project:** Groadmap
**Last Updated:** 2026-03-18
**Status:** Active Development

---

## Overview

This document tracks all planned improvements and features for the Groadmap project. Tasks are organized by priority and dependencies.

---

## Phase 1: Performance Optimization

Based on the comprehensive performance analysis conducted by go-performance-advisor, this phase focuses on implementing critical performance improvements that can yield 60-90% performance gains.

---

### TASK-P001: Add Composite Database Indexes

**Priority:** P0 - Critical
**Dependencies:** None
**Complexity:** Low
**Estimated Time:** 1 day
**Specialists:** go-elite-developer, exhaustive-qa-engineer

**Identified Problem:**
The database schema lacks critical composite indexes for frequently queried patterns. The `ListTasks` function filters by multiple criteria (status, priority, severity) with ORDER BY clauses, causing SQLite to perform full table scans when multiple filters are combined. For datasets with 10,000+ tasks, query times can reach 150ms instead of the optimal 15ms.

**Technical Description of Need:**
Add composite indexes to `internal/db/schema.go` covering common query patterns:
- Create `idx_tasks_status_priority` for queries filtering by status and ordering by priority
- Create `idx_tasks_priority_created` for priority-based filtering with date ordering
- Create `idx_sprint_tasks_lookup` for sprint task relationship queries
- Create `idx_audit_date` for audit log date range queries
- Ensure indexes follow SQLite best practices (most selective columns first)
- Update schema version if necessary
- Test query plans with `EXPLAIN QUERY PLAN` to verify index usage

**Validation Requirements:**
- [x] `EXPLAIN QUERY PLAN` shows index usage for `ListTasks` with status filter
- [x] `EXPLAIN QUERY PLAN` shows index usage for `ListTasks` with priority filter
- [x] Query time for 10,000 tasks with filters is under 20ms (was 150ms)
- [x] Sprint task queries use `idx_sprint_tasks_lookup`
- [x] Audit date range queries use `idx_audit_date`
- [x] All existing tests pass without modification
- [x] Database file size increase is documented (~684 KB / 34% for 10,000 tasks)

**Affected Files:**
- `internal/db/schema.go` (add CREATE INDEX statements)

**Files to Create:**
- None

---

### TASK-P002: Implement Prepared Statement Caching for Dynamic Queries

**Priority:** P0 - Critical
**Dependencies:** None
**Complexity:** Medium
**Estimated Time:** 3 days
**Specialists:** go-elite-developer, go-performance-advisor

**Identified Problem:**
Multiple database functions build SQL queries using `fmt.Sprintf` with `strings.Join`, creating unique query strings for each call. This prevents SQLite from caching query plans, forcing recompilation on every execution. Functions affected include `GetTasks`, `UpdateTask`, `UpdateTaskStatus`, `UpdateTaskPriority`, and `UpdateTaskSeverity`. This causes 20-30% overhead on repeated operations.

**Technical Description of Need:**
Implement prepared statement caching for common batch operations:
- Create a query cache map for common IN clause sizes (1-100, 250, 500, 1000)
- Pre-generate placeholder strings during initialization
- Modify batch update functions to use cached queries
- Implement batch processing (chunk large ID lists into batches of 100)
- Ensure thread-safety with proper synchronization
- Add benchmarks to measure improvement
- Maintain backward compatibility with existing function signatures

**Validation Requirements:**
- [x] Benchmark shows 20-30% improvement in batch update operations
- [x] Memory allocations reduced in batch operations
- [x] Query plan cache hit rate is above 90% for repeated operations
- [x] Batch processing handles 1000+ IDs efficiently
- [x] Thread-safe implementation with concurrent access
- [x] All existing tests pass
- [x] No regression in error handling

**Affected Files:**
- `internal/db/queries.go` (modify batch query functions)
- `internal/db/connection.go` (add query cache initialization)

**Files to Create:**
- `internal/db/query_cache.go` (query caching logic)
- `internal/db/bench_test.go` (performance benchmarks)

---

### TASK-P003: Optimize Connection Pool for Concurrent Reads

**Priority:** P0 - Critical
**Dependencies:** None
**Complexity:** Low
**Estimated Time:** 1 day
**Specialists:** go-elite-developer

**Identified Problem:**
The current connection pool configuration in `internal/db/connection.go` sets `MaxOpenConns(1)`, which serializes ALL database operations including reads. While SQLite supports only one writer at a time, WAL mode allows concurrent readers. This configuration prevents leveraging WAL mode concurrency benefits, creating a sequential bottleneck for read-heavy workloads.

**Technical Description of Need:**
Optimize connection pool configuration for concurrent read access:
- Increase `MaxOpenConns` to 10 to allow concurrent readers
- Set `MaxIdleConns` to 5 to maintain warm connections
- Configure `ConnMaxLifetime` to 1 hour for connection recycling
- Configure `ConnMaxIdleTime` to 10 minutes to close unused connections
- Document the rationale for these settings
- Verify WAL mode is properly enabled
- Test concurrent read performance

**Validation Requirements:**
- [x] Concurrent read operations execute in parallel (WAL mode allows this)
- [x] Write operations remain serialized (SQLite constraint)
- [x] No "database is locked" errors under concurrent load
- [x] Connection pool metrics show expected behavior
- [x] Performance improvement for read-heavy workloads
- [x] All existing tests pass
- [x] No connection leaks detected

**Affected Files:**
- `internal/db/connection.go` (modify configureConnection function)

**Files to Create:**
- None

---

### TASK-P004: Implement Database Connection Caching

**Priority:** P1 - High
**Dependencies:** TASK-P003
**Complexity:** Medium
**Estimated Time:** 3 days
**Specialists:** go-elite-developer

**Identified Problem:**
Every CLI command opens and closes the database connection, incurring 10-50ms overhead per command. This includes connection establishment, schema validation, and file descriptor operations. For scripts with multiple operations, this overhead accumulates significantly. There is no connection reuse between commands within the same process.

**Technical Description of Need:**
Implement connection caching for the CLI process lifetime:
- Create a connection cache map keyed by roadmap name
- Implement `OpenCached()` function that returns cached connections
- Add connection health checks (ping verification)
- Implement cleanup on process exit using `sync.Once` and `atexit` pattern
- Ensure thread-safe access with RWMutex
- Handle stale/dead connection removal
- Maintain backward compatibility with existing `Open()` function

**Validation Requirements:**
- [ ] Second command reuses existing connection (verified with logging)
- [ ] Connection health check validates liveness
- [ ] Dead connections are removed from cache and recreated
- [ ] Process exit closes all cached connections
- [ ] Concurrent access to cache is thread-safe
- [ ] Memory usage remains stable (no connection leaks)
- [ ] All existing tests pass

**Affected Files:**
- `internal/db/connection.go` (add caching logic)

**Files to Create:**
- `internal/db/cache.go` (connection cache implementation)

---

### TASK-P005: Optimize scanTasks Memory Allocations

**Priority:** P1 - High
**Dependencies:** None
**Complexity:** Low
**Estimated Time:** 2 days
**Specialists:** go-elite-developer

**Identified Problem:**
The `scanTasks` function in `internal/db/queries.go` allocates memory inefficiently. The tasks slice starts as nil (capacity 0), causing reallocation on each append. Additionally, `sql.NullString` variables are redeclared for each row iteration, and interface conversions cause heap escapes. For large result sets, this creates unnecessary GC pressure.

**Technical Description of Need:**
Optimize memory allocations in scanTasks:
- Pre-allocate tasks slice with capacity of 100 (typical batch size)
- Move scan variables outside the loop for reuse
- Avoid pointer allocations for empty/null strings
- Use `strings.TrimSpace` only when necessary
- Consider using `sync.Pool` for Task objects if allocation profiling shows need
- Add memory benchmarks to verify improvement
- Document allocation patterns

**Validation Requirements:**
- [ ] Benchmark shows reduced allocations per operation
- [ ] `scanTasks` with 1000 tasks shows < 10 allocations (was N allocations)
- [ ] Memory usage remains constant for large result sets
- [ ] No regression in scan functionality
- [ ] All existing tests pass
- [ ] Profile shows improved cache locality
- [ ] No memory leaks in long-running operations

**Affected Files:**
- `internal/db/queries.go` (modify scanTasks function)

**Files to Create:**
- `internal/db/alloc_bench_test.go` (allocation benchmarks)

---

### TASK-P006: Replace Map-Based Updates with Struct-Based Approach

**Priority:** P1 - High
**Dependencies:** None
**Complexity:** Medium
**Estimated Time:** 3 days
**Specialists:** go-elite-developer

**Identified Problem:**
The `UpdateTask` function uses `map[string]interface{}` for field updates, which has several drawbacks: (1) value types require boxing to interface{}, causing allocations; (2) map iteration order is random, producing non-deterministic SQL; (3) no compile-time type safety; (4) field validation requires runtime map key checking. This pattern is inefficient and error-prone.

**Technical Description of Need:**
Replace map-based updates with a struct-based approach:
- Define `TaskUpdate` struct with pointer fields for optional updates
- Create new `UpdateTaskStruct` function using the struct
- Implement deterministic field ordering in SQL generation
- Avoid interface{} boxing by using concrete types
- Maintain field whitelist validation
- Deprecate old `UpdateTask` function (keep for backward compatibility)
- Update command handlers to use new struct-based API
- Add type-safe builder pattern if appropriate

**Validation Requirements:**
- [ ] New struct-based API produces deterministic SQL
- [ ] Memory allocations reduced compared to map approach
- [ ] All valid update scenarios work correctly
- [ ] Invalid field updates return appropriate errors
- [ ] Backward compatibility maintained for existing code
- [ ] Unit tests cover all update field combinations
- [ ] Performance benchmark shows improvement

**Affected Files:**
- `internal/db/queries.go` (add TaskUpdate struct and new function)
- `internal/commands/task.go` (update taskEdit to use new API)

**Files to Create:**
- None

---

### TASK-P007: Optimize Task Status Validation with Map Lookup

**Priority:** P2 - Medium
**Dependencies:** None
**Complexity:** Low
**Estimated Time:** 1 day
**Specialists:** go-elite-developer

**Identified Problem:**
The `IsValidTaskStatus` function iterates through a slice of valid statuses on every call (O(n) complexity). This function is called frequently during status transitions, especially in `CanTransitionTo` which validates every status change. For a fixed set of 5 statuses, a map-based lookup would be O(1) and more efficient.

**Technical Description of Need:**
Replace slice iteration with map lookup for status validation:
- Create `validStatusMap` map[string]TaskStatus with all valid statuses
- Refactor `IsValidTaskStatus` to use map lookup
- Refactor `ParseTaskStatus` to use map for parsing
- Ensure map is initialized in init() or as package variable
- Maintain existing function signatures for compatibility
- Add benchmark comparing old vs new implementation
- Document the change in code comments

**Validation Requirements:**
- [ ] Benchmark shows O(1) vs O(n) performance improvement
- [ ] All valid statuses are recognized correctly
- [ ] Invalid statuses return appropriate errors
- [ ] Status transitions work correctly
- [ ] All existing tests pass
- [ ] No memory regressions
- [ ] Thread-safe implementation

**Affected Files:**
- `internal/models/task.go` (modify status validation functions)

**Files to Create:**
- None

---

### TASK-P008: Cache JSON Encoder Instance

**Priority:** P2 - Medium
**Dependencies:** None
**Complexity:** Low
**Estimated Time:** 1 day
**Specialists:** go-elite-developer

**Identified Problem:**
The `PrintJSON` and `PrintJSONIndent` functions create a new `json.Encoder` instance on every call, including setting `SetEscapeHTML(false)` each time. Since `os.Stdout` is constant throughout the application lifetime, the encoder could be reused. This causes unnecessary allocations and configuration overhead on every JSON output operation.

**Technical Description of Need:**
Implement JSON encoder caching:
- Create package-level encoder instance using `sync.Once`
- Configure encoder once with `SetEscapeHTML(false)`
- Create getter function that returns cached encoder
- Handle `PrintJSONIndent` separately (different configuration)
- Ensure thread-safe access to shared encoder
- Add benchmark for JSON output operations
- Document thread-safety considerations

**Validation Requirements:**
- [ ] Benchmark shows reduced allocations for JSON output
- [ ] JSON output remains correct and formatted properly
- [ ] Thread-safe concurrent access to encoder
- [ ] EscapeHTML remains disabled for all outputs
- [ ] All existing tests pass
- [ ] No regression in error handling
- [ ] Memory profile shows improvement

**Affected Files:**
- `internal/utils/json.go` (add encoder caching)

**Files to Create:**
- None

---

### TASK-P009: Optimize Struct Field Alignment

**Priority:** P3 - Low
**Dependencies:** None
**Complexity:** Low
**Estimated Time:** 1 day
**Specialists:** go-elite-developer

**Identified Problem:**
The `Task` struct in `internal/models/task.go` has suboptimal field ordering, causing unnecessary padding between fields. On 64-bit systems, this results in ~104 bytes per struct when it could be ~96 bytes. While individual savings are small, this improves cache locality when processing large task lists and reduces overall memory footprint.

**Technical Description of Need:**
Reorder struct fields by size (largest first) to minimize padding:
- Analyze current struct layout with `go tool structlayout`
- Reorder fields: strings (16 bytes) first, then pointers (8 bytes), then integers (8 bytes)
- Apply same optimization to Sprint and AuditEntry structs
- Verify no breaking changes to JSON serialization
- Document the new field order in comments
- Add struct size benchmark

**Validation Requirements:**
- [ ] Struct size reduced from ~104 to ~96 bytes
- [ ] JSON serialization produces identical output
- [ ] No breaking changes to API
- [ ] All existing tests pass
- [ ] Benchmark shows improved cache locality
- [ ] Documentation updated with field order rationale

**Affected Files:**
- `internal/models/task.go` (reorder Task struct fields)
- `internal/models/sprint.go` (reorder Sprint struct fields)
- `internal/models/audit.go` (reorder AuditEntry struct fields)

**Files to Create:**
- None

---

### TASK-P010: Cache SQL Placeholder Strings

**Priority:** P3 - Low
**Dependencies:** None
**Complexity:** Low
**Estimated Time:** 1 day
**Specialists:** go-elite-developer

**Identified Problem:**
Multiple database functions build placeholder strings (e.g., "?,?,?") dynamically using loops and `strings.Join`. This creates temporary string allocations for every query with IN clauses. Since placeholder patterns are repetitive (common batch sizes), they could be pre-computed and cached.

**Technical Description of Need:**
Implement placeholder string caching:
- Pre-compute placeholder strings for sizes 1-1000 in init()
- Create `getPlaceholders(n int)` function that returns cached strings
- Use efficient string building (strings.Builder or pre-computed)
- Handle edge cases (n=0, n>1000)
- Apply to all functions building IN clauses
- Add benchmark for placeholder generation
- Document memory savings

**Validation Requirements:**
- [ ] Placeholder generation shows zero allocations for cached sizes
- [ ] All IN clause queries work correctly
- [ ] Edge cases handled (empty, large batches)
- [ ] Benchmark shows improvement
- [ ] All existing tests pass
- [ ] No regression in query functionality

**Affected Files:**
- `internal/db/queries.go` (use cached placeholders)
- `internal/db/query_cache.go` (add to existing cache file)

**Files to Create:**
- None

---

### TASK-P011: Capture Timestamps Once Per Operation

**Priority:** P3 - Low
**Dependencies:** None
**Complexity:** Low
**Estimated Time:** 1 day
**Specialists:** go-elite-developer

**Identified Problem:**
Multiple functions call `utils.NowISO8601()` (which calls `time.Now()`) multiple times within the same logical operation. For example, in task creation, one timestamp is captured for the task, and a different timestamp is captured for the audit log. This creates inconsistent timestamps and unnecessary system calls.

**Technical Description of Need:**
Capture timestamps once per operation:
- Identify all functions calling NowISO8601() multiple times
- Capture timestamp at operation start
- Use same timestamp for all records in the operation
- Apply to task creation, updates, sprint operations
- Ensure audit logs match entity timestamps
- Add test to verify timestamp consistency
- Document the pattern

**Validation Requirements:**
- [ ] Task and audit log have identical timestamps
- [ ] Sprint and its audit entries have consistent timestamps
- [ ] No regression in functionality
- [ ] All existing tests pass
- [ ] New test verifies timestamp consistency
- [ ] Code review shows pattern applied consistently

**Affected Files:**
- `internal/commands/task.go` (taskCreate, taskEdit, taskSetStatus, etc.)
- `internal/commands/sprint.go` (sprintCreate, sprint operations)

**Files to Create:**
- None

---

### TASK-P012: Optimize Sprint Tasks N+1 Query

**Priority:** P3 - Low
**Dependencies:** None
**Complexity:** Medium
**Estimated Time:** 2 days
**Specialists:** go-elite-developer

**Identified Problem:**
The `GetSprint` function performs an N+1 query pattern: it first queries the sprint, then makes a separate query to get sprint tasks. This results in two database round trips when one would suffice. For better performance, sprint and tasks should be fetched in a single query using JOINs or JSON aggregation.

**Technical Description of Need:**
Optimize sprint task retrieval to avoid N+1 queries:
- Implement single-query approach using JOIN with JSON aggregation (SQLite 3.38+)
- Alternative: Use two queries but batch task retrieval
- Consider using `json_group_array()` for task IDs
- Parse JSON result into task slice
- Maintain existing Sprint struct API
- Add benchmark comparing old vs new approach
- Document the optimization

**Validation Requirements:**
- [ ] Sprint retrieval uses single query (verified with logging)
- [ ] All sprint data (including tasks) returned correctly
- [ ] Benchmark shows reduced query count
- [ ] Performance improvement for sprint-heavy operations
- [ ] All existing tests pass
- [ ] No regression in error handling
- [ ] Documentation updated

**Affected Files:**
- `internal/db/queries.go` (optimize GetSprint function)

**Files to Create:**
- None

---

## Summary

| Task | Priority | Complexity | Time | Impact |
|------|----------|------------|------|--------|
| TASK-P001 | P0 | Low | 1 day | 90% query improvement |
| TASK-P002 | P0 | Medium | 3 days | 30% batch operation improvement |
| TASK-P003 | P0 | Low | 1 day | Concurrent reads |
| TASK-P004 | P1 | Medium | 3 days | 50% startup improvement |
| TASK-P005 | P1 | Low | 2 days | 60% allocation reduction |
| TASK-P006 | P1 | Medium | 3 days | Type safety + performance |
| TASK-P007 | P2 | Low | 1 day | O(1) validation |
| TASK-P008 | P2 | Low | 1 day | JSON encoding efficiency |
| TASK-P009 | P3 | Low | 1 day | Memory alignment |
| TASK-P010 | P3 | Low | 1 day | Placeholder caching |
| TASK-P011 | P3 | Low | 1 day | Timestamp consistency |
| TASK-P012 | P3 | Medium | 2 days | N+1 elimination |

**Total Estimated Time:** 20 days
**Expected Overall Improvement:** 60-90% performance gain

---

## Completion Checklist

- [ ] Phase 1: Performance Optimization
  - [x] TASK-P001: Add Composite Database Indexes
  - [x] TASK-P002: Implement Prepared Statement Caching
  - [x] TASK-P003: Optimize Connection Pool
  - [ ] TASK-P004: Implement Database Connection Caching
  - [ ] TASK-P005: Optimize scanTasks Memory Allocations
  - [ ] TASK-P006: Replace Map-Based Updates
  - [ ] TASK-P007: Optimize Task Status Validation
  - [ ] TASK-P008: Cache JSON Encoder Instance
  - [ ] TASK-P009: Optimize Struct Field Alignment
  - [ ] TASK-P010: Cache SQL Placeholder Strings
  - [ ] TASK-P011: Capture Timestamps Once Per Operation
  - [ ] TASK-P012: Optimize Sprint Tasks N+1 Query

---

*Document generated by task-creator skill*
*Based on performance analysis by go-performance-advisor*
