# CLI Commands

## Naming Conventions

- Commands: lowercase, kebab-case (`list`, `create`)
- Flags: double-dash for long (`--help`), single-dash for short (`-h`)
- Subcommands: clear hierarchy (`rmp roadmap list`)

## Command Structure

```
rmp [command] [subcommand] [arguments] [options]
```

## Error Handling

Errors follow typical CLI conventions (NOT JSON format):

### Default Behavior
- Error messages are written explicitly to **stderr**
- Plain text format (human-readable)
- Uses standard Unix exit codes

### Input-Related Errors
When errors are related to inputs (misuse of commands or subcommands), the **specific help for that command or subcommand** is displayed after the error.

---

## Field Validation

### Task Field Constraints

The following fields have mandatory length constraints enforced by the application:

| Field | Required | Max Length | Description |
|-------|----------|------------|-------------|
| `title` | Yes | 255 chars | Task title/summary |
| `functional_requirements` | Yes | 4096 chars | Why: functional requirements |
| `technical_requirements` | Yes | 4096 chars | How: technical description |
| `acceptance_criteria` | Yes | 4096 chars | How to verify: completion criteria |

### Validation Behavior

- **Whitespace trimming:** Leading and trailing whitespace is trimmed before validation
- **Empty strings:** Treated as missing for required fields
- **Error format:** Plain text to stderr with descriptive message
- **Exit code:** 1 for validation errors

### Validation Error Messages

| Scenario | Error Message (stderr) |
|----------|------------------------|
| Title exceeds 255 chars | "Error: Title must not exceed 255 characters (got N)" |
| Title is empty | "Error: Title is required" |
| Requirements exceed 4096 chars | "Error: {Field} must not exceed 4096 characters (got N)" |
| Requirements are empty | "Error: {Field} is required" |

---

## Global Commands

### Help

```bash
rmp --help
rmp -h
```

**Description:** Displays general help with available commands in **plain text**. This is also the default behavior when no command is provided.

### Version

```bash
rmp --version
rmp -v
```

**Description:** Displays application version.

---

## Exit Codes

Groadmap follows standard Unix exit code conventions. Success results in exit code `0`. Errors use specific codes (1-127) and are documented in detail in [ARCHITECTURE.md](./ARCHITECTURE.md#exit-codes).

---

## Roadmap Management

Command: `rmp roadmap` (alias: `rmp road`)

### List Roadmaps

```bash
rmp roadmap list
rmp road ls
```

**Description:** Lists all existing roadmaps.

**JSON Output:**
```json
[
  {"name": "project1", "path": "~/.roadmaps/project1.db", "size": 24576},
  {"name": "project2", "path": "~/.roadmaps/project2.db", "size": 8192}
]
```

### Create Roadmap

```bash
rmp roadmap create <name>
rmp road new <name>
```

**Output (success):** `{"name": "project1"}`, exit code 0.

### Remove Roadmap

```bash
rmp roadmap remove <name>
rmp road rm <name>
```

**Output (success):** No output, exit code 0.

### Select Roadmap (Default)

```bash
rmp roadmap use <name>
rmp road use <name>
```

**Description:** Sets the default roadmap for subsequent commands.

**Output (success):** No output, exit code 0.

---

## Task Management

Command: `rmp task` (alias: `rmp t`)

### List Tasks

```bash
rmp task list --roadmap <name>
rmp task ls -r <name> [OPTIONS]
```

**Options:**
- `-s, --status <state>` - Filter by status (BACKLOG, SPRINT, DOING, TESTING, COMPLETED)
- `-p, --priority <n>` - Filter priority >= n (0-9)
- `--severity <n>` - Filter severity >= n (0-9)
- `-l, --limit <n>` - Limit number of results

**JSON Output:** Array of Task objects.

### Create Task

```bash
rmp task create --roadmap <name> --title <title> --functional-requirements <fr> --technical-requirements <tr> --acceptance-criteria <ac> [OPTIONS]
rmp task new -r <name> -t <title> -fr <fr> -tr <tr> -ac <ac>
```

**Options:**
- `-t, --title <text>` - Task title (required), maximum 255 characters
- `-fr, --functional-requirements <text>` - Functional requirements (required), maximum 4096 characters
- `-tr, --technical-requirements <text>` - Technical requirements (required), maximum 4096 characters
- `-ac, --acceptance-criteria <text>` - Acceptance criteria (required), maximum 4096 characters
- `-p, --priority <0-9>` - Priority (default: 0)
- `--severity <0-9>` - Severity (default: 0)
- `-sp, --specialists <list>` - Comma-separated specialists

**Validation Rules:**
| Field | Constraint | Error Message |
|-------|------------|---------------|
| `title` | Required, max 255 chars | "Title is required and must not exceed 255 characters" |
| `functional-requirements` | Required, max 4096 chars | "Functional requirements are required and must not exceed 4096 characters" |
| `technical-requirements` | Required, max 4096 chars | "Technical requirements are required and must not exceed 4096 characters" |
| `acceptance-criteria` | Required, max 4096 chars | "Acceptance criteria are required and must not exceed 4096 characters" |

**Output (success):** `{"id": 42}`, exit code 0.

**Error Output:** Validation errors written to stderr with exit code 1.

### Get Task(s)

```bash
rmp task get --roadmap <name> <id1,id2>
```

**Description:** Retrieves one or more tasks by ID. Multiple IDs must be comma-separated without spaces.

**JSON Output:** Array of Task objects.

### Get Next Tasks (next)

```bash
rmp task next [num]
rmp t next [num]
```

**Description:** Returns the next N open tasks from the currently open sprint. Tasks are ordered by severity (descending) and then by priority (descending), returning the most critical and highest priority tasks first.

**Arguments:**
- `num` (optional) - Number of tasks to return. If not provided, defaults to 1.

**JSON Output:** Array of Task objects.

**Output Examples:**

Success (tasks available):
```json
[
  {
    "id": 42,
    "title": "Implement user authentication",
    "functional_requirements": "Users must be able to authenticate securely",
    "technical_requirements": "Create login endpoint with JWT tokens",
    "acceptance_criteria": "Users can log in with valid credentials; tokens expire after 24h",
    "priority": 9,
    "severity": 9,
    "status": "SPRINT",
    "specialists": "backend,security",
    "sprint_id": 5,
    "created_at": "2026-03-15T10:30:00.000Z",
    "started_at": null,
    "tested_at": null,
    "closed_at": null
  }
]
```

Success (fewer tasks than requested):
```json
[
  {
    "id": 42,
    "title": "Implement user authentication",
    "functional_requirements": "Users must be able to authenticate securely",
    "technical_requirements": "Create login endpoint with JWT tokens",
    "acceptance_criteria": "Users can log in with valid credentials; tokens expire after 24h",
    "priority": 9,
    "severity": 9,
    "status": "SPRINT",
    "specialists": "backend,security",
    "sprint_id": 5,
    "created_at": "2026-03-15T10:30:00.000Z",
    "started_at": null,
    "tested_at": null,
    "closed_at": null
  },
  {
    "id": 43,
    "title": "Add input validation",
    "functional_requirements": "Prevent invalid data from entering the system",
    "technical_requirements": "Validate all user inputs using validator library",
    "acceptance_criteria": "All inputs validated; proper error messages returned",
    "priority": 8,
    "severity": 9,
    "status": "SPRINT",
    "specialists": "backend",
    "sprint_id": 5,
    "created_at": "2026-03-15T11:00:00.000Z",
    "started_at": null,
    "tested_at": null,
    "closed_at": null
  }
]
```

Success (no open tasks in sprint):
```json
[]
```

**Error Cases:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| No sprint is currently open | 1 | "No sprint is currently open. Use 'rmp sprint start' to open a sprint first." |
| Invalid num argument (not a positive integer) | 1 | "Invalid argument: num must be a positive integer" |
| Roadmap not specified and no default set | 1 | "Roadmap not specified. Use -r flag or set a default with 'rmp roadmap use'" |

**Behavior Notes:**
- Only returns tasks with status `SPRINT`, `DOING`, or `TESTING` (open tasks)
- Tasks are ordered first by severity (highest first), then by priority (highest first)
- If the requested number exceeds available open tasks, all remaining open tasks are returned
- If no open sprint exists, an error is returned indicating a sprint needs to be opened
- If the sprint has no open tasks, an empty array is returned (success, exit code 0)

### Change Status (stat)

```bash
rmp task stat -r <name> <ids> <state>
rmp task set-status -r <name> <ids> <state>
```

**Description:** Updates the status of one or more tasks (bulk supported).

**Output (success):** No output, exit code 0.

### Change Priority (prio)

```bash
rmp task prio -r <name> <ids> <priority>
rmp task set-priority -r <name> <ids> <priority>
```

**Output (success):** No output, exit code 0.

### Change Severity (sev)

```bash
rmp task sev -r <name> <ids> <severity>
rmp task set-severity -r <name> <ids> <severity>
```

**Output (success):** No output, exit code 0.

### Edit Task

```bash
rmp task edit --roadmap <name> <id> [OPTIONS]
```

**Description:** Edits an existing task's properties. Only specified fields are updated.

**Options:**
- `-t, --title <text>` - Maximum 255 characters
- `-fr, --functional-requirements <text>` - Maximum 4096 characters
- `-tr, --technical-requirements <text>` - Maximum 4096 characters
- `-ac, --acceptance-criteria <text>` - Maximum 4096 characters
- `-p, --priority <0-9>`
- `--severity <0-9>`
- `-sp, --specialists <list>`

**Validation Rules:**
When specified, the following fields must meet length constraints:
| Field | Constraint | Error Message |
|-------|------------|---------------|
| `title` | Max 255 chars | "Title must not exceed 255 characters" |
| `functional-requirements` | Max 4096 chars | "Functional requirements must not exceed 4096 characters" |
| `technical-requirements` | Max 4096 chars | "Technical requirements must not exceed 4096 characters" |
| `acceptance-criteria` | Max 4096 chars | "Acceptance criteria must not exceed 4096 characters" |

**Output (success):** No output, exit code 0.

**Error Output:** Validation errors written to stderr with exit code 1.

### Remove Task

```bash
rmp task remove -r <name> <ids>
rmp task rm -r <name> <ids>
```

**Output (success):** No output, exit code 0.

---

## Sprint Management

Command: `rmp sprint` (alias: `rmp s`)

### List Sprints

```bash
rmp sprint list -r <name> [--status <state>]
rmp sprint ls -r <name>
```

**JSON Output:** Array of Sprint objects.

### Create Sprint

```bash
rmp sprint create -r <name> -d "Description"
rmp sprint new -r <name> -d "Description"
```

**Output (success):** `{"id": 1}`, exit code 0.

### Get Sprint

```bash
rmp sprint get -r <name> <id>
```

**JSON Output:** Single Sprint object.

### List Sprint Tasks

```bash
rmp sprint tasks -r <name> <id> [--status <state>]
```

**JSON Output:** Array of Task objects associated with the sprint.

### Sprint Statistics

```bash
rmp sprint stats -r <name> <id>
```

**JSON Output:**
```json
{
  "sprint_id": 1,
  "total_tasks": 10,
  "completed_tasks": 3,
  "progress_percentage": 30.0,
  "status_distribution": {
    "SPRINT": 4,
    "DOING": 2,
    "TESTING": 1,
    "COMPLETED": 3
  }
}
```

### Show Sprint Status Report

```bash
rmp sprint show -r <name> <id>
```

**Description:** Displays a comprehensive status report of a sprint, including task statistics and distribution by severity and criticality. Provides a quick overview for sprint stand-up meetings and progress tracking.

**JSON Output:**
```json
{
  "sprint_id": 5,
  "sprint_description": "Sprint 12 - March 2026",
  "status": "OPEN",
  "summary": {
    "total_tasks": 20,
    "pending": 8,
    "in_progress": 6,
    "completed": 6
  },
  "progress": {
    "pending_percentage": 40.0,
    "in_progress_percentage": 30.0,
    "completed_percentage": 30.0
  },
  "severity_distribution": {
    "0-2": {"count": 2, "percentage": 10.0},
    "3-5": {"count": 5, "percentage": 25.0},
    "6-7": {"count": 8, "percentage": 40.0},
    "8-9": {"count": 5, "percentage": 25.0}
  },
  "criticality_distribution": {
    "low": {"count": 4, "percentage": 20.0},
    "medium": {"count": 8, "percentage": 40.0},
    "high": {"count": 6, "percentage": 30.0},
    "critical": {"count": 2, "percentage": 10.0}
  }
}
```

**Output Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `sprint_id` | integer | Sprint identifier |
| `sprint_description` | string | Sprint description/name |
| `status` | string | Sprint status (OPEN, CLOSED) |
| `summary.total_tasks` | integer | Total number of tasks in sprint |
| `summary.pending` | integer | Tasks with status BACKLOG or SPRINT |
| `summary.in_progress` | integer | Tasks with status DOING or TESTING |
| `summary.completed` | integer | Tasks with status COMPLETED |
| `progress.pending_percentage` | float | Percentage of pending tasks |
| `progress.in_progress_percentage` | float | Percentage of tasks in progress |
| `progress.completed_percentage` | float | Percentage of completed tasks |
| `severity_distribution` | object | Task distribution by severity ranges |
| `criticality_distribution` | object | Task distribution by criticality levels |

**Severity Ranges:**
- `0-2`: Low severity
- `3-5`: Medium severity
- `6-7`: High severity
- `8-9`: Critical severity

**Criticality Levels:**
- `low`: Severity 0-2
- `medium`: Severity 3-5
- `high`: Severity 6-7
- `critical`: Severity 8-9

**Error Cases:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 1 | "Sprint not found" |
| Roadmap not specified and no default set | 1 | "Roadmap not specified. Use -r flag or set a default with 'rmp roadmap use'" |

### Sprint Lifecycle

```bash
rmp sprint start -r <name> <id>
rmp sprint close -r <name> <id>
rmp sprint reopen -r <name> <id>
```

**Output (success):** No output, exit code 0.

### Task Assignment

```bash
rmp sprint add-tasks -r <name> <sprint-id> <task-ids>
rmp sprint remove-tasks -r <name> <sprint-id> <task-ids>
rmp sprint move-tasks -r <name> <from-id> <to-id> <task-ids>
```

**Description:** Bulk assignment/removal/movement of tasks using comma-separated IDs. Alias for `add-tasks` is `add`.

**Output (success):** No output, exit code 0.

### Update Sprint

```bash
rmp sprint update -r <name> <id> -d "New Description"
rmp sprint upd -r <name> <id> -d "New Description"
```

**Output (success):** No output, exit code 0.

### Remove Sprint

```bash
rmp sprint remove -r <name> <id>
rmp sprint rm -r <name> <id>
```

**Output (success):** No output, exit code 0.

---

## Audit Log Management

Command: `rmp audit` (alias: `aud`)

### List Audit Log

```bash
rmp audit list -r <name> [OPTIONS]
rmp audit ls -r <name>
```

**Options:**
- `-o, --operation <type>` - Filter by operation (CREATE, UPDATE, etc.)
- `-e, --entity-type <type>` - Filter by entity (TASK, SPRINT, ROADMAP)
- `--entity-id <id>` - Filter by specific entity ID
- `--since <date>` - ISO 8601 date
- `--until <date>` - ISO 8601 date
- `-l, --limit <n>` - Limit results

**JSON Output:** Array of AuditEntry objects.

### Entity History

```bash
rmp audit history -r <name> -e <type> <id>
rmp audit hist -r <name> -e <type> <id>
```

**Description:** Shows all audit entries related to a specific task or sprint.

**JSON Output:** Array of AuditEntry objects.

### Audit Statistics

```bash
rmp audit stats -r <name> [--since <date>] [--until <date>]
```

**JSON Output:** Summary of operations performed in the period.

---

## Command Aliases Reference

| Command | Aliases |
|---------|---------|
| `roadmap` | `road` |
| `task` | `t` |
| `sprint` | `s` |
| `audit` | `aud` |
| `list` | `ls` |
| `create` | `new` |
| `remove` | `rm`, `delete` |
| `set-status` | `stat` |
| `set-priority` | `prio` |
| `set-severity` | `sev` |
| `update` | `upd` |
| `history` | `hist` |
| `add-tasks` | `add` |
| `remove-tasks` | `rm-tasks` |
| `move-tasks` | `mv-tasks` |
