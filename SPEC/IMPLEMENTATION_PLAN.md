# Implementation Plan - Groadmap

This document outlines the step-by-step implementation plan for the Groadmap project, following the "Specification First" policy.

## Phase 1: Foundation and Utilities

The first phase focus on core utilities that will be used across the entire application.

### 1.1 Time Utility (`internal/utils/time.go`)
- **Objective**: Ensure all dates follow the `YYYY-MM-DDTHH:mm:ss.sssZ` format (ISO 8601 UTC with milliseconds).
- **Tasks**:
  - Implement `FormatISO8601(t time.Time) string`
  - Implement `ParseISO8601(s string) (time.Time, error)`
  - Implement `Now() string` (returns current time in formatted string)

### 1.2 Path Utility (`internal/utils/path.go`)
- **Objective**: Handle cross-platform path resolution for the `~/.roadmaps/` directory.
- **Tasks**:
  - Implement `GetDataDir() (string, error)` (returns absolute path to `~/.roadmaps/`)
  - Implement `EnsureDataDir() error` (creates directory with `0700` permissions)
  - Implement `GetRoadmapPath(name string) (string, error)` (validates name and returns `.db` path)

### 1.3 JSON Utility (`internal/utils/json.go`)
- **Objective**: Standardize JSON output to `stdout`.
- **Tasks**:
  - Implement `PrintJSON(v interface{}) error` (uses `json.NewEncoder(os.Stdout)` without pretty-print)

---

## Phase 2: Core Models (`internal/models/`)

Define the data structures and enums that represent the business domain.

### 2.1 Task Models (`internal/models/task.go`)
- **TaskStatus**: Enum with values `BACKLOG`, `SPRINT`, `DOING`, `TESTING`, `COMPLETED`.
- **Task**: Struct matching `DATABASE.md` and `DATA_FORMATS.md`.
- **Validation**: Implement methods to validate status transitions.

### 2.2 Sprint Models (`internal/models/sprint.go`)
- **SprintStatus**: Enum with values `PENDING`, `OPEN`, `CLOSED`.
- **Sprint**: Struct matching `DATABASE.md`.
- **SprintStats**: Struct for statistics output.

### 2.3 Audit Models (`internal/models/audit.go`)
- **AuditEntry**: Struct matching `DATABASE.md`.

---

## Phase 3: Database Layer (`internal/db/`)

Implement SQLite integration with security and integrity focus.

### 3.1 Connection Management (`internal/db/connection.go`)
- **Objective**: Safe database opening and closing.
- **Requirements**:
  - Enable `PRAGMA foreign_keys = ON;`
  - Enable `PRAGMA journal_mode = WAL;`
  - Handle file permissions (`0600`) for new `.db` files.

### 3.2 Schema Management (`internal/db/schema.go`)
- **Objective**: Automate table creation and versioning.
- **Tasks**:
  - Implement `CreateSchema(db *sql.DB) error` using DDL from `DATABASE.md`.
  - Initialize `_metadata` table.

### 3.3 Query Implementation (`internal/db/queries.go`)
- **Objective**: Centralize all SQL queries using prepared statements.
- **Focus**: Start with `roadmap` management queries (list, create, delete).

---

## Phase 4: CLI Entry Point (`cmd/rmp/main.go`)

Establish the command routing structure.

### 4.1 Command Routing
- Implement top-level routing for `roadmap`, `task`, `sprint`, and `audit`.
- Implement `roadmap create` and `roadmap list` as first functional commands.

### 4.2 Error Handling
- Centralize error reporting to `stderr`.
- Implement exit code mapping as defined in `ARCHITECTURE.md`.

---

## Phase 5: Functional Blocks Implementation

Implement remaining commands following the hierarchy:
1. **Roadmap Management** (CRUD)
2. **Task Management** (CRUD + Status/Priority/Severity)
3. **Sprint Management** (Lifecycle + Task Assignment)
4. **Audit Log** (Queries + Stats)
