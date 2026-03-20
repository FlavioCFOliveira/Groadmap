# System Architecture

## High-Level Overview

Groadmap is a CLI application distributed as a single binary executable. The architecture follows principles of simplicity, performance, and data isolation.

```
+-------------------------------------+
|           CLI Interface             |
|         (Go, argument parsing)      |
+------------------+------------------+
                   |
+------------------v------------------+
|         Command Router            |
|   (roadmap | task | sprint)       |
+------------------+------------------+
                   |
+------------------v------------------+
|         Business Logic              |
|   (validation, business rules)    |
+------------------+------------------+
                   |
+------------------v------------------+
|         SQLite Layer                |
|   (queries, transactions, schema)   |
+------------------+------------------+
                   |
+------------------v------------------+
|         Filesystem                  |
|   (~/.roadmaps/*.db)                |
+-------------------------------------+
```

## Directory Structure

```
~/.roadmaps/              # User data directory
├── project1.db          # Individual roadmap (SQLite)
├── project2.db
└── ...
```

### Location Rules

1. The `.roadmaps` directory is located in the **user home directory**
2. Directory name: exactly `.roadmaps` (dot prefix, lowercase)
3. Permissions: Restricted to the owner (`0700` or `drwx------` on POSIX) to ensure data privacy
4. Each `.db` file represents an independent roadmap
5. Only files with `.db` extension are considered

## Security Guarantees

Groadmap implements several security layers to protect user data and ensure system stability:

### 1. Data Isolation and Privacy
- **Restricted Permissions**: The data directory `~/.roadmaps` is created with `0700` permissions, and individual `.db` files should be created with `0600` permissions, preventing other users on the system from reading or modifying roadmap data.
- **Input Validation**: Roadmap names are strictly validated using the regex `^[a-z0-9_-]+$` to prevent path traversal attacks. This validation MUST be applied as a central gate for all commands that accept a roadmap name (via `-r`, `--roadmap`, or `roadmap use`).

### 2. Binary Hardening
- **ASLR Support**: The binary is compiled as a Position Independent Executable (PIE) to leverage Address Space Layout Randomization (standard in modern Go).
- **Stack Protection**: Go's runtime provides built-in stack management and bounds checking to protect against stack buffer overflows.
- **Static Analysis**: Usage of `go vet`, `staticcheck`, and race detection during development to identify potential vulnerabilities and race conditions.

### 3. Robustness and Reliability
- **CLI Robustness**: The argument parser is designed to handle extremely large inputs and malicious characters without crashing or panicking.
- **No SQL Injection**: All database interactions use parameterized queries via SQLite's prepared statements. Bulk ID parameters are converted to integers before query construction.
- **Foreign Key Enforcement**: `PRAGMA foreign_keys = ON;` must be enabled on every database connection to ensure referential integrity and trigger cascading deletes.
- **Bulk Operation Limits**: Commands handling bulk task IDs (e.g., `rmp task get`) must batch operations into sets of 500 or fewer to stay safely within SQLite's `SQLITE_LIMIT_VARIABLE_NUMBER`.
- **Transactional Integrity**: All database modifications (CREATE, UPDATE, DELETE, STATUS_CHANGE) MUST be wrapped in an explicit SQL transaction. The audit log entry for the operation MUST be written within the same transaction to ensure atomicity and consistency.
- **XSS Prevention**: User inputs that might be rendered in other contexts are sanitized to remove HTML tags and dangerous attributes.

## Source Code Structure

```
Groadmap/
├── go.mod                 # Go module definition
├── go.sum                 # Go dependencies checksum
├── cmd/
│   └── rmp/
│       └── main.go        # Entry point, CLI parsing
├── internal/
│   ├── commands/
│   │   ├── roadmap.go     # Roadmap subcommands
│   │   ├── task.go        # Task subcommands
│   │   └── sprint.go      # Sprint subcommands
│   ├── db/
│   │   ├── connection.go  # SQLite connection management
│   │   ├── schema.go      # DDL, structure creation
│   │   └── queries.go     # Parameterized SQL queries
│   ├── models/
│   │   ├── task.go        # Task structs, enums
│   │   ├── sprint.go      # Sprint structs, enums
│   │   └── roadmap.go     # Roadmap structures
│   └── utils/
│       ├── json.go        # JSON serialization
│       ├── time.go        # ISO 8601 date handling
│       └── path.go        # Cross-platform path resolution
└── SPEC/                  # Technical specification
    ├── ARCHITECTURE.md
    ├── COMMANDS.md
    ├── DATABASE.md
    ├── DATA_FORMATS.md
    ├── DEPLOY.md
    ├── HELP_EXAMPLES.md
    ├── MODELS.md
    ├── STATE_MACHINE.md
    └── VERSION.md
```

## Memory Layout Optimization

### Struct Field Ordering

All Go structs are organized to minimize memory padding and maximize cache locality. Fields are grouped by size and access patterns:

**Task Struct (152 bytes, zero padding on 64-bit):**
```
Group 1: Content fields (strings) - 96 bytes
  Title, Status, FunctionalRequirements, TechnicalRequirements,
  AcceptanceCriteria, CreatedAt

Group 2: Tracking fields (pointers) - 32 bytes
  Specialists, StartedAt, TestedAt, ClosedAt

Group 3: Metadata fields (integers) - 24 bytes
  ID, Priority, Severity
```

**Rationale:**
- String fields (16 bytes each) are grouped for cache locality when displaying task content
- Pointer fields (8 bytes each) are grouped as they're often accessed together during lifecycle transitions
- Integer fields (8 bytes each) are grouped as they're frequently used for filtering/sorting

**Field Sizes on 64-bit Systems:**
- `string`: 16 bytes (pointer + length), 8-byte aligned
- `*T` (pointer): 8 bytes, 8-byte aligned
- `int`: 8 bytes, 8-byte aligned

### Cache Line Considerations

The Task struct (152 bytes) spans approximately 2-3 cache lines (64 bytes each). Field grouping ensures that commonly accessed fields together (e.g., all content fields) fit within the same cache lines, reducing cache misses during task display operations.

## Modules and Responsibilities

### 1. cmd/rmp/main.go
- Parse command-line arguments
- Route to appropriate handlers
- Top-level error handling
- Consistent JSON output

### 2. internal/commands/
Each package implements:
- Argument validation
- Specific business logic
- Data layer calls
- Response formatting

### 3. internal/db/
- **connection.go**: Connection management, safe open/close
- **schema.go**: Structure creation/updates
- **queries.go**: Parameterized SQL, injection prevention

### 4. internal/models/
- Go struct definitions
- Enums for states (TaskStatus, SprintStatus)
- JSON serialization/deserialization tags

### 5. internal/utils/
- **json.go**: Consistent JSON output wrapper
- **time.go**: UTC conversion, ISO 8601 formatting
- **path.go**: Cross-platform path resolution

## Command Lifecycle

```
1. CLI Input → Parse arguments
2. Validation → Verify syntax and values
3. Routing → Determine handler
4. Execution → Business logic + DB
5. Formatting → Structure result
6. Output → JSON to stdout
```

## Error Handling

### Error Categories

| Category | Example | Response |
|-----------|---------|----------|
| Invalid input | Missing parameter | Plain text error + command help |
| Resource not found | Roadmap not found | Plain text error to stderr |
| Conflict | Duplicate name | Plain text error to stderr |
| SQLite | Query error | Plain text error to stderr |
| System | No permissions | Plain text error to stderr |

### Error Format

**All errors are output as plain text to stderr (NOT JSON).**

Errors follow typical CLI conventions:
- Error messages are written explicitly to **stderr**
- Plain text format (human-readable)
- Uses standard Unix exit codes

**Input-related errors** (missing parameters, invalid arguments, unknown commands/subcommands) additionally display the **specific help for the command or subcommand** that was invoked.

**Example - General error:**
```
$ rmp task get -r project1 999
Error: Task with ID 999 not found in roadmap 'project1'
```

**Example - Input error (shows help):**
```
$ rmp task create -r project1
Error: Missing required parameters: --description, --action, --expected-result

Usage: rmp task create [OPTIONS]
...
```

## Exit Codes

Groadmap follows standard Unix/Linux exit code conventions. Success output is JSON; errors are plain text to stderr. The exit code indicates success or failure type for shell scripting and CI/CD integration.

### Exit Code Standards

| Exit Code | Name | Description | When Used |
|-----------|------|-------------|-----------|
| `0` | `EXIT_SUCCESS` | Command completed successfully | All successful operations |
| `1` | `EXIT_FAILURE` | General error | Unexpected errors, database failures |
| `2` | `EXIT_MISUSE` | Misuse of command | Invalid arguments, syntax errors |
| `3` | `EXIT_NO_ROADMAP` | No roadmap selected | Commands requiring roadmap when none selected |
| `4` | `EXIT_NOT_FOUND` | Resource not found | Roadmap/task/sprint not found |
| `5` | `EXIT_EXISTS` | Resource already exists | Duplicate roadmap/task names |
| `6` | `EXIT_INVALID_DATA` | Invalid input data | Validation failures (dates, ranges) |
| `126` | `EXIT_NOT_EXECUTABLE` | Command not executable | Permission issues |
| `127` | `EXIT_CMD_NOT_FOUND` | Command not found | Unknown command/subcommand |
| `130` | `EXIT_SIGINT` | Interrupted by Ctrl+C | SIGINT received |

### Error Code Mapping

Internal error codes map to exit codes as follows:

| Error Code | Exit Code | Meaning |
|------------|-----------|---------|
| `INVALID_INPUT` | 2 | Bad command syntax or missing arguments |
| `INVALID_DATE` | 6 | Date format or range validation failed |
| `INVALID_DATE_RANGE` | 6 | Date range validation failed |
| `INVALID_PRIORITY` | 6 | Priority out of range (0-9) |
| `INVALID_SEVERITY` | 6 | Severity out of range (0-9) |
| `INVALID_STATUS_TRANSITION` | 2 | Invalid task status transition |
| `ROADMAP_NOT_FOUND` | 4 | Specified roadmap does not exist |
| `ROADMAP_EXISTS` | 5 | Roadmap name already in use |
| `TASK_NOT_FOUND` | 4 | Task ID does not exist |
| `TASKS_NOT_FOUND` | 4 | None of the provided task IDs exist |
| `SOME_TASKS_NOT_FOUND` | 4 | Some of the provided task IDs don't exist |
| `SPRINT_NOT_FOUND` | 4 | Sprint ID does not exist |
| `NO_ROADMAP` | 3 | No roadmap selected and none specified |
| `DB_ERROR` | 1 | Database operation failed |
| `SYSTEM_ERROR` | 1 | Internal system error |
| `UNKNOWN_SUBCOMMAND` | 2 | Invalid subcommand specified |
| `UNKNOWN_COMMAND` | 127 | Unknown command or subcommand |
| `UPDATE_FAILED` | 1 | Failed to update resource |
| `DELETE_FAILED` | 1 | Failed to delete resource |

### Usage in Shell Scripts

```bash
# Check if command succeeded
if rmp task list -r myproject > /dev/null 2>&1; then
    echo "Tasks listed successfully"
fi

# Handle specific errors
rmp roadmap create newproject
case $? in
    0) echo "Created successfully" ;;
    5) echo "Roadmap already exists" ;;
    *) echo "Failed with error code $?" ;;
esac

# Exit on any error (strict mode)
set -e
rmp roadmap use myproject    # Exits 4 if not found
rmp task add -d "New task"   # Exits 3 if no roadmap
```

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

Groadmap uses a connection pool optimized for concurrent read access:

```go
db.SetMaxOpenConns(10)        // Allow concurrent readers (WAL mode supports this)
db.SetMaxIdleConns(5)         // Keep warm connections for frequent access
db.SetConnMaxLifetime(time.Hour)   // Recycle connections after 1 hour
db.SetConnMaxIdleTime(10 * time.Minute) // Close idle connections after 10 min
```

**Rationale**:
- **MaxOpenConns(10)**: WAL mode allows multiple concurrent readers
- **MaxIdleConns(5)**: Maintains warm connections to avoid connection establishment overhead
- **ConnMaxLifetime(1 hour)**: Periodically recycles connections to prevent resource leaks
- **ConnMaxIdleTime(10 min)**: Closes unused connections to free resources

**Note**: Write operations remain serialized at the SQLite level regardless of connection pool size.

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

---

## Performance Considerations

1. **Lazy loading**: SQLite connections only opened when needed.
2. **Prepared statements**: Pre-compiled SQLite queries for repeated operations.
3. **WAL Mode**: Use `PRAGMA journal_mode=WAL;` to improve concurrency for read/write operations.
4. **Foreign Keys**: Explicitly enable `PRAGMA foreign_keys=ON;` on every connection to enforce constraints and cascading actions.
5. **Bulk Operations**: Encapsulate multiple updates in a single transaction. Batch ID lists larger than 500 to avoid SQLite variable limits.
6. **Streaming Output**: Use `json.Encoder` for large result sets (e.g., `audit list`) to stream JSON directly to `stdout` instead of buffering.
7. **Concurrency**: Leverage Go's concurrency for independent read operations, but ensure writes are strictly sequential per roadmap file.
