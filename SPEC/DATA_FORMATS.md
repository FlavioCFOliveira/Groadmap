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

The canonical state-machine definition (states, valid transitions, manual/automatic semantics, deletion preconditions) lives in `SPEC/STATE_MACHINE.md`. Refer to that file for the authoritative transition matrix and rules.

JSON output that includes a `status` field uses one of the five enum values defined in `MODELS.md` — Task Status (`BACKLOG`, `SPRINT`, `DOING`, `TESTING`, `COMPLETED`).

Sprint state machine (states, transitions, reopening): see `STATE_MACHINE.md § Sprint State Machine`.

---

## Data Types (JSON representation for Queries)

### Task

```json
{
  "id": 1,
  "title": "Implement JWT authentication system",
  "status": "BACKLOG",
  "type": "USER_STORY",
  "functional_requirements": "Users must be able to authenticate securely",
  "technical_requirements": "Create authentication module with JWT token support",
  "acceptance_criteria": "Functional login with 24h valid tokens; proper error handling",
  "created_at": "2026-03-12T10:00:00.000Z",
  "specialists": "go-elite-developer,security-expert",
  "started_at": null,
  "tested_at": null,
  "closed_at": null,
  "completion_summary": null,
  "parent_task_id": null,
  "priority": 9,
  "severity": 0,
  "subtask_count": 0,
  "depends_on": [],
  "blocks": []
}
```

### Sprint

Example with a capacity limit set (`max_tasks` is an integer):

```json
{
  "id": 1,
  "status": "OPEN",
  "description": "Sprint 1 - Setup and architecture",
  "tasks": [1, 2, 3, 5],
  "task_count": 4,
  "created_at": "2026-03-12T09:00:00.000Z",
  "started_at": "2026-03-12T10:00:00.000Z",
  "closed_at": null,
  "max_tasks": 10
}
```

Example with unlimited capacity (`max_tasks` is `null`):

```json
{
  "id": 2,
  "status": "PENDING",
  "description": "Sprint 2 - Open scope",
  "tasks": [],
  "task_count": 0,
  "created_at": "2026-03-13T09:00:00.000Z",
  "started_at": null,
  "closed_at": null,
  "max_tasks": null
}
```

**Note:** The `tasks` and `task_count` fields are computed at runtime from the `sprint_tasks` junction table and are not stored in the `sprints` table. The `max_tasks` field is always present in the JSON output (never omitted); it is `null` when no capacity limit is set and an integer otherwise.

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
3. **Pretty-print**: All JSON output must be human-readable with 2-space indentation (`  `) and no prefix. This applies to every command that produces JSON to stdout.
4. **UTF-8**: All strings in UTF-8
5. **Numbers**: Use JSON number format (not strings)
6. **Empty arrays**: Represent as `[]` (not `null`)
