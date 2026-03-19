# Data Formats

## Fundamental Principle

### Output (Responses)

**JSON output is reserved for query operations and record creation.**

- **Query operations (JSON)**: `list`, `ls`, `get`, `next`, `tasks`, `stats`, `show`, `history`, `hist`.
- **Creation operations (JSON)**: `create`, `new`. These commands return a JSON object containing the ID of the newly created record (e.g., `{"id": 42}`).
- **Other database modifications (No output)**: Commands that update, delete, or change the state of entities (status, priority, etc.) respond with **no content** on success, signaling completion via exit code `0`.
- **Help commands (Plain text)**: When no command is provided, or when using `-h` and `--help` flags, the application displays information in **plain text**, following traditional CLI application formats (not JSON).

**Error responses follow typical CLI behavior (NOT JSON):**
- Errors are written as explicit human-readable messages to stderr
- Input-related errors (missing parameters, wrong types, unknown commands or subcommands) additionally show the **specific help of the command or subcommand** that was invoked
- Uses standard Unix exit codes for script integration

### Input

**All application inputs are via CLI parameters, without exceptions.**

- No JSON input
- No stdin input
- No configuration files
- No interactive input

**Accepted formats:**
- Positional parameters: `rmp task create <name>`
- Short flags: `-r <name>`, `-p 5`
- Long flags: `--roadmap <name>`, `--priority 5`
- Comma-separated lists: `1,2,3`

---

## Response Structure

### Query Response (Success)

Success responses for query operations are the direct result object or array, without any wrapper:

```json
{ /* command-specific payload directly */ }
```

**Examples:**
- `rmp task list` returns an array of Task objects directly: `[{...}, {...}]`
- `rmp sprint stats` returns the stats object directly: `{"sprint_id": 1, ...}`

### Creation Response (Success)

Commands that create new records producing a JSON object with the ID.
- **Exit Code**: `0`
- **Stdout**: `{"id": 42}` (or `{"name": "project1"}` for roadmaps)

### Modification Response (Success)

Commands that alter the database state without creating new records (update, delete, status change, etc.) produce **no output** on success.
- **Exit Code**: `0`
- **Stdout**: Empty

### Help Response

Help commands display human-readable text to stdout.

### Error Response

Error responses follow typical CLI conventions (NOT JSON).

---

## Exit Codes

Groadmap returns standard Unix exit codes for integration with shell scripts and CI/CD pipelines.

Refer to the authoritative exit code documentation and error mapping in [ARCHITECTURE.md](./ARCHITECTURE.md#exit-codes).

---

## Dates - ISO 8601 with UTC

### Exact Format

```
YYYY-MM-DDTHH:mm:ss.sssZ
```

### Rules

1. **Always UTC**: All dates are converted to UTC
2. **With milliseconds**: 3 digits after the dot
3. **Z suffix**: Explicit UTC indicator
4. **T separator**: Between date and time

---

## Task Status State Machine

The following table defines the valid transitions between task statuses.

| From \ To | BACKLOG | SPRINT | DOING | TESTING | COMPLETED |
|-----------|:-------:|:------:|:-----:|:-------:|:---------:|
| **BACKLOG** | - | Yes | No | No | No |
| **SPRINT** | Yes | - | Yes | No | No |
| **DOING** | No | Yes | - | Yes | No |
| **TESTING** | No | No | Yes | - | Yes |
| **COMPLETED** | Yes | No | No | No | - |

**Key Rules:**
1. **Adding to Sprint**: Tasks move from `BACKLOG` to `SPRINT` (typically via `rmp sprint add`).
2. **Starting Work**: Tasks move from `SPRINT` to `DOING` (starting development).
3. **Internal Iteration**: `TESTING -> DOING` allows returning to development if tests fail.
4. **Pausing**: `DOING -> SPRINT` allows pausing work on a task within the same sprint.
5. **Removal/Return**: `SPRINT -> BACKLOG` allows removing a task from a sprint.
6. **Completion**: Only `TESTING` tasks can be marked as `COMPLETED`.
7. **Reopening**: A `COMPLETED` task can be moved back to `BACKLOG` for major changes or bug fixes.

---

## Sprint Status Flow

Sprints follow a linear progression with reopening capability.

```
PENDING → OPEN → CLOSED
            ↑      │
            └──────┘ (reopen)
```

1. **PENDING**: Initial state upon creation.
2. **OPEN**: Active sprint (started via `rmp sprint start`).
3. **CLOSED**: Completed sprint (closed via `rmp sprint close`).
4. **REOPEN**: Moving from `CLOSED` back to `OPEN`.

---

## Data Types (JSON representation for Queries)

### Task

```json
{
  "id": 1,
  "title": "Implement JWT authentication system",
  "status": "BACKLOG",
  "functional_requirements": "Users must be able to authenticate securely",
  "technical_requirements": "Create authentication module with JWT token support",
  "acceptance_criteria": "Functional login with 24h valid tokens; proper error handling",
  "created_at": "2026-03-12T10:00:00.000Z",
  "specialists": "go-elite-developer,security-expert",
  "started_at": null,
  "tested_at": null,
  "closed_at": null,
  "priority": 9,
  "severity": 0
}
```

### Sprint

```json
{
  "id": 1,
  "status": "OPEN",
  "description": "Sprint 1 - Setup and architecture",
  "tasks": [1, 2, 3, 5],
  "task_count": 4,
  "created_at": "2026-03-12T09:00:00.000Z",
  "started_at": "2026-03-12T10:00:00.000Z",
  "closed_at": null
}
```

**Note:** The `tasks` and `task_count` fields are computed at runtime from the `sprint_tasks` junction table and are not stored in the `sprints` table.

### Audit Entry

```json
{
  "id": 1,
  "operation": "TASK_STATUS_CHANGE",
  "entity_type": "TASK",
  "entity_id": 42,
  "performed_at": "2026-03-12T15:30:00.000Z"
}
```

---

## Implementation Notes

1. **No extra fields**: Do not include extra fields in JSON responses
2. **Consistent order**: Maintain field order as defined in examples
3. **No pretty-print by default**: Compact JSON for efficient parsing
4. **UTF-8**: All strings in UTF-8
5. **Numbers**: Use JSON number format (not strings)
6. **Empty arrays**: Represent as `[]` (not `null`)
