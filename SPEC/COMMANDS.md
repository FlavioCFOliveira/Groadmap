# CLI Commands

## Change History

| Date | Change | Description |
|------|--------|-------------|
| 2026-03-23 | Initial | First version of CLI Commands specification |
| 2026-03-23 | Update | Added `stats` command for roadmap statistics |
| 2026-03-24 | Update | Added `--summary` flag to `task stat` for completion summary |
| 2026-03-24 | Update | Added `task reopen` command; restricted `task remove` to BACKLOG only; enforced sequential sprint opening |

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
| `completion_summary` | No | 4096 chars | Summary of work done; only accepted on `task stat` when target status is `COMPLETED` |

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

### Roadmap Name Validation

All roadmap names must conform to the following validation rules:

| Rule | Value | Description |
|------|-------|-------------|
| Regex | `^[a-z0-9_-]+$` | Only lowercase letters, numbers, underscores, and hyphens |
| Maximum length | 50 characters | Ensures filesystem compatibility |
| Minimum length | 1 character | Name cannot be empty |

**Validation Error Messages:**

| Scenario | Error Message (stderr) |
|----------|------------------------|
| Invalid characters | "Error: Roadmap name must only contain lowercase letters, numbers, underscores, and hyphens" |
| Exceeds 50 characters | "Error: Roadmap name must not exceed 50 characters (got N)" |
| Empty name | "Error: Roadmap name is required" |

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

**Name Validation:**

| Rule | Value | Description |
|------|-------|-------------|
| Regex | `^[a-z0-9_-]+$` | Only lowercase letters, numbers, underscores, and hyphens |
| Maximum length | 50 characters | Ensures filesystem compatibility |
| Minimum length | 1 character | Name cannot be empty |

**Error Cases:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Invalid characters | 2 | "Error: Roadmap name must only contain lowercase letters, numbers, underscores, and hyphens" |
| Exceeds 50 characters | 2 | "Error: Roadmap name must not exceed 50 characters (got N)" |
| Roadmap already exists | 5 | "Error: Roadmap 'name' already exists" |

**Output (success):** `{"name": "project1"}`, exit code 0.

### Remove Roadmap

```bash
rmp roadmap remove <name>
rmp road rm <name>
```

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
- `-y, --type <type>` - Task type (default: TASK). Valid values: `USER_STORY`, `TASK`, `BUG`, `SUB_TASK`, `EPIC`, `REFACTOR`, `CHORE`, `SPIKE`, `DESIGN_UX`, `IMPROVEMENT`
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
| `type` | One of 10 valid values | "Error: invalid task type: <value>" | 6 |

**Output (success):** `{"id": 42}`, exit code 0.

**Error Output:** Validation errors written to stderr with exit code 1.

### Get Task(s)

```bash
rmp task get --roadmap <name> <id1,id2>
```

**Description:** Retrieves one or more tasks by ID. Multiple IDs must be comma-separated without spaces.

**Batch Operation Behavior (Fail-Fast):**

All batch operations validate ALL IDs before executing any destructive operation. This ensures atomicity and prevents partial updates.

| Scenario | Exit Code | Behavior | stderr Output |
|----------|-----------|----------|---------------|
| All IDs valid | 0 | Returns all tasks as JSON array | None |
| Some IDs invalid | 4 | **No operation performed**, returns error | "Error: Task ID N not found" (first invalid only) |
| All IDs invalid | 4 | **No operation performed**, returns error | "Error: Tasks not found: N,M,..." |
| Invalid ID format | 2 | **No operation performed** | "Error: Invalid task ID format: X" |

**Validation Order:**
1. Parse all IDs and validate format (must be positive integers)
2. Verify all IDs exist in the roadmap
3. Only after full validation succeeds, execute the operation
4. If any validation fails, exit immediately with exit code 4 (or 2 for format errors)

**Rationale:** Prevents partial state changes. If a batch update fails halfway through, the database would be in an inconsistent state. Fail-fast ensures either all operations succeed or none do.

**JSON Output:** Array of Task objects.

### Get Next Tasks (next)

```bash
rmp task next [num]
rmp t next [num]
```

**Description:** Returns the next N open tasks from the currently open sprint. Tasks are returned in the order defined by the sprint's `task_order` (set via `sprint reorder` or other ordering commands), allowing the team to define execution sequence independent of priority/severity.

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
| Roadmap not specified | 1 | "Error: Roadmap not specified. Use -r <name> or --roadmap <name>" |

**Behavior Notes:**
- Only returns tasks with status `SPRINT`, `DOING`, or `TESTING` (open tasks)
- Tasks are returned in the order defined by the sprint's `task_order` (set via sprint reorder or other ordering commands)
- If the requested number exceeds available open tasks, all remaining open tasks are returned
- If no open sprint exists, an error is returned indicating a sprint needs to be opened
- If the sprint has no open tasks, an empty array is returned (success, exit code 0)

### Change Status (stat)

```bash
rmp task stat -r <name> <ids> <state>
rmp task set-status -r <name> <ids> <state>

# With optional completion summary (only valid when transitioning to COMPLETED)
rmp task stat -r <name> <ids> COMPLETED --summary "Brief description of what was done"
rmp task stat -r <name> <ids> COMPLETED -s "Brief description of what was done"
```

**Description:** Updates the status of one or more tasks (bulk supported).

**Flags:**

| Flag | Short | Type | Description |
|------|-------|------|-------------|
| `--summary` | `-s` | string | Optional completion summary. Only accepted when target state is `COMPLETED`. Maximum 4096 characters. |

**Batch Operation Behavior (Fail-Fast):**

All batch operations validate ALL IDs and status transitions before applying any changes. This ensures atomicity - either all tasks are updated or none are.

| Scenario | Exit Code | Behavior | stderr Output |
|----------|-----------|----------|---------------|
| All IDs valid | 0 | All tasks updated | None |
| Some IDs invalid | 4 | **No changes made** | "Error: Task ID N not found" |
| All IDs invalid | 4 | **No changes made** | "Error: Tasks not found: N,M,..." |
| Invalid ID format | 2 | **No changes made** | "Error: Invalid task ID format: X" |
| Invalid status transition | 2 | **No changes made** | "Error: Invalid status transition from X to Y" |
| `--summary` used with non-COMPLETED state | 2 | **No changes made** | "Error: --summary flag is only allowed when transitioning to COMPLETED" |
| `--summary` exceeds 4096 characters | 2 | **No changes made** | "Error: Completion summary must not exceed 4096 characters (got N)" |

**Validation Order:**
1. Parse all IDs and validate format (must be positive integers)
2. Validate `--summary` flag: reject if target state is not `COMPLETED`; validate length if provided
3. Verify all IDs exist in the roadmap
4. Validate status transition for each task against state machine rules
5. Only after full validation succeeds, update all tasks and audit log
6. If any validation fails, exit immediately without making changes

**Completion Summary Behavior:**
- `--summary` is optional even when transitioning to `COMPLETED`
- When provided, `completion_summary` is stored on each updated task
- When transitioning COMPLETED → BACKLOG (reopen), `completion_summary` is cleared to NULL
- `--summary` has no effect on non-COMPLETED transitions and is rejected with an error

**Output (success):** No output, exit code 0.

### Change Priority (prio)

```bash
rmp task prio -r <name> <ids> <priority>
rmp task set-priority -r <name> <ids> <priority>
```

**Batch Operation Behavior (Fail-Fast):**

Validates all IDs before updating any priorities. Follows same validation order as `task stat`.

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| All IDs valid | 0 | None |
| Some IDs invalid | 4 | "Error: Task ID N not found" |
| Priority out of range (0-9) | 2 | "Error: Priority must be between 0 and 9" |

**Output (success):** No output, exit code 0.

### Change Severity (sev)

```bash
rmp task sev -r <name> <ids> <severity>
rmp task set-severity -r <name> <ids> <severity>
```

**Batch Operation Behavior (Fail-Fast):**

Validates all IDs before updating any severities. Follows same validation order as `task stat`.

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| All IDs valid | 0 | None |
| Some IDs invalid | 4 | "Error: Task ID N not found" |
| Severity out of range (0-9) | 2 | "Error: Severity must be between 0 and 9" |

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
- `-y, --type <type>` - Task type. Valid values: `USER_STORY`, `TASK`, `BUG`, `SUB_TASK`, `EPIC`, `REFACTOR`, `CHORE`, `SPIKE`, `DESIGN_UX`, `IMPROVEMENT`
- `-p, --priority <0-9>`
- `--severity <0-9>`
- `-sp, --specialists <list>`

**Validation Rules:**

When a field is specified, it is validated before updating:

| Field | Constraint | Error Message (stderr) | Exit Code |
|-------|------------|------------------------|-----------|
| `title` | Required, max 255 chars | "Error: Title is required and must not exceed 255 characters" | 1 |
| `title` | Empty string | "Error: Title cannot be empty" | 1 |
| `functional-requirements` | Required, max 4096 chars | "Error: Functional requirements are required and must not exceed 4096 characters" | 1 |
| `functional-requirements` | Empty string | "Error: Functional requirements cannot be empty" | 1 |
| `technical-requirements` | Required, max 4096 chars | "Error: Technical requirements are required and must not exceed 4096 characters" | 1 |
| `technical-requirements` | Empty string | "Error: Technical requirements cannot be empty" | 1 |
| `acceptance-criteria` | Required, max 4096 chars | "Error: Acceptance criteria are required and must not exceed 4096 characters" | 1 |
| `acceptance-criteria` | Empty string | "Error: Acceptance criteria cannot be empty" | 1 |
| `priority` | Range 0-9 | "Error: Priority must be between 0 and 9" | 1 |
| `severity` | Range 0-9 | "Error: Severity must be between 0 and 9" | 1 |
| `type` | One of 10 valid values | "Error: invalid task type: <value>" | 6 |

**Validation Behavior:**
- **Whitespace trimming:** Leading and trailing whitespace is trimmed before validation
- **Empty strings:** Setting a required field to empty string fails validation
- **Partial updates:** Only specified fields are validated and updated
- **Type validation:** Non-integer values for priority/severity fail with exit code 1
- **No-op:** If no fields are specified, command succeeds with no changes (exit code 0)

**Output (success):** No output, exit code 0.

**Error Output:** Validation errors written to stderr with exit code 1.

### Remove Task

```bash
rmp task remove -r <name> <ids>
rmp task rm -r <name> <ids>
```

**Description:** Removes one or more tasks by ID (bulk supported).

**Batch Operation Behavior (Fail-Fast):**

All batch operations validate ALL IDs before removing any tasks. This is especially critical for destructive operations to prevent accidental partial deletion.

| Scenario | Exit Code | Behavior | stderr Output |
|----------|-----------|----------|---------------|
| All IDs valid | 0 | All tasks removed | None |
| Some IDs invalid | 4 | **No tasks removed** | "Error: Task ID N not found" |
| All IDs invalid | 4 | **No tasks removed** | "Error: Tasks not found: N,M,..." |
| Invalid ID format | 2 | **No tasks removed** | "Error: Invalid task ID format: X" |

**Validation Order:**
1. Parse all IDs and validate format (must be positive integers)
2. Verify all IDs exist in the roadmap
3. Only after full validation succeeds, remove all tasks in a single transaction
4. If any validation fails, exit immediately without removing any tasks

**Rationale:** Prevents accidental partial deletion. If IDs are mistyped, no data is lost.

**Output (success):** No output, exit code 0.

**Constraint:** Tasks must be in `BACKLOG` status to be removed. Attempting to delete a task in any other status returns an error.

| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Task in SPRINT, DOING, TESTING, or COMPLETED | 6 | `Error: task #N cannot be deleted — status is X, must be BACKLOG` |
| Batch with any non-BACKLOG task | 6 | Entire batch rejected, no tasks deleted |

---

### Reopen Task

```bash
rmp task reopen -r <name> <ids>
```

**Description:** Returns one or more tasks to `BACKLOG` status, clearing all lifecycle timestamps (`started_at`, `tested_at`, `closed_at`). Also removes the task from its sprint association (`sprint_tasks`). Accepts comma-separated IDs for bulk operations.

**Valid source states:** `SPRINT`, `DOING`, `TESTING`, `COMPLETED` — any non-BACKLOG state.

**Batch Operation Behavior (Fail-Fast):**

All IDs are validated before any transitions are applied. If any ID is invalid, the entire batch is rejected and no tasks are modified.

| Scenario | Exit Code | Behavior | Output |
|----------|-----------|----------|--------|
| Task transitions to BACKLOG | 0 | Timestamps cleared; sprint_tasks row removed if applicable | No stdout |
| Task already in BACKLOG | 0 | No change | Informational message to stderr |
| Invalid task ID | 4 | **No tasks modified** | Error to stderr |

**Output (success):** No output to stdout, exit code 0.

**Audit:** Each reopened task is logged individually with operation `TASK_REOPEN`.

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
rmp sprint tasks -r <name> <id> [--status <state>] [--order-by-priority]
```

**JSON Output:** Array of Task objects associated with the sprint, ordered by position (default) or by priority if flag specified.

**Options:**
- `-s, --status <state>` - Filter by status
- `--order-by-priority` - Order by priority DESC, severity DESC (legacy ordering)

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
  },
  "task_order": [5, 3, 8, 1, 9, 2, 7, 4, 6, 10]
}
```

**Fields:**
- `task_order` - Array of task IDs ordered by position (first to last)

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
| Roadmap not specified | 1 | "Error: Roadmap not specified. Use -r <name> or --roadmap <name>" |

### Sprint Lifecycle

```bash
rmp sprint start -r <name> <id>
rmp sprint close -r <name> <id> [--force]
rmp sprint reopen -r <name> <id>
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--force` | (`sprint close` only) Close the sprint even if tasks are still in DOING or TESTING status. A warning listing the incomplete tasks is printed to stderr. |

**Active-Task Safety Check (sprint close):**

`sprint close` queries for tasks with status `DOING` or `TESTING` in the sprint before closing. If any exist and `--force` is not provided, the command returns exit code 2 with an error listing the task IDs and statuses. With `--force`, the sprint is closed and a warning is printed to stderr.

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| No active tasks | 0 | None |
| Active tasks exist, no `--force` | 6 | "invalid input: sprint #N has M active task(s) still in progress: #ID (STATUS), ... — use --force to close anyway" |
| Active tasks exist, `--force` given | 0 | "warning: closing sprint #N with M incomplete task(s): #ID (STATUS), ..." |

**Output (success):** No output, exit code 0.

### Task Assignment

```bash
rmp sprint add-tasks -r <name> <sprint-id> <task-ids>
rmp sprint remove-tasks -r <name> <sprint-id> <task-ids>
rmp sprint move-tasks -r <name> <from-id> <to-id> <task-ids>
```

**Description:** Bulk assignment/removal/movement of tasks using comma-separated IDs. Alias for `add-tasks` is `add`.

**Batch Operation Behavior (Fail-Fast):**

All sprint task operations validate ALL IDs before making any changes.

| Scenario | Exit Code | Behavior | stderr Output |
|----------|-----------|----------|---------------|
| All IDs valid | 0 | All tasks assigned/removed/moved | None |
| Some task IDs invalid | 4 | **No changes made** | "Error: Task ID N not found" |
| Sprint ID invalid | 4 | **No changes made** | "Error: Sprint ID N not found" |
| Invalid ID format | 2 | **No changes made** | "Error: Invalid ID format: X" |

**Validation Order:**
1. Validate sprint ID exists
2. Parse all task IDs and validate format
3. Verify all task IDs exist in the roadmap
4. For `add-tasks`: verify tasks are not already in another sprint
5. For `remove-tasks`/`move-tasks`: verify tasks are currently in the specified sprint
6. Only after full validation succeeds, execute the operation
7. If any validation fails, exit immediately without making changes

**Automatic Status Updates:**

| Command | Task Status Change | Description |
|---------|-------------------|-------------|
| `add-tasks` | BACKLOG → SPRINT | Tasks automatically change to SPRINT status when added to sprint |
| `remove-tasks` | SPRINT → BACKLOG | Tasks automatically return to BACKLOG when removed from sprint |
| `move-tasks` | (No change) | Status is preserved when moving between sprints |

**Note:** The status SPRINT is automatically managed by sprint operations. Users should NOT manually set status to SPRINT using `task stat`. Manual status transitions should follow: BACKLOG → DOING → TESTING → COMPLETED.

**Output (success):** No output, exit code 0.

### Task Ordering

Commands for managing sprint task order within a sprint. Tasks are ordered by position (0-based), where position 0 is the first task in the sprint.

#### Reorder Tasks (Set Exact Order)

```bash
rmp sprint reorder -r <name> <sprint-id> <task-ids>
rmp sprint order -r <name> <sprint-id> <task-ids>
```

**Description:** Sets the exact order of all tasks in a sprint. The order of task IDs in the argument defines the new sequence.

**Arguments:**
- `sprint-id` - Sprint identifier
- `task-ids` - Comma-separated list of task IDs in the desired order (e.g., `5,3,1,4,2`)

**Validation:**
- All task IDs must belong to the specified sprint
- Duplicate task IDs are not allowed
- All sprint tasks must be included (partial reorder is not supported)

**Behavior:**
- Task at index 0 gets position 0 (first)
- Task at index 1 gets position 1 (second)
- And so on...

**JSON Output (success):** No output, exit code 0.

**Error Output:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 1 | "Sprint not found" |
| Task ID not in sprint | 1 | "Task ID N is not in sprint" |
| Duplicate task IDs | 1 | "Duplicate task ID: N" |
| Missing task IDs | 1 | "Task list incomplete: expected N tasks, got M" |
| Invalid task ID format | 1 | "Invalid task ID: X" |

#### Move Task to Position

```bash
rmp sprint move-to -r <name> <sprint-id> <task-id> <position>
rmp sprint mvto -r <name> <sprint-id> <task-id> <position>
```

**Description:** Moves a single task to a specific position, shifting other tasks accordingly.

**Arguments:**
- `sprint-id` - Sprint identifier
- `task-id` - Task to move
- `position` - Target position (0-based). If position >= task count, task is moved to the end.

**Behavior:**
- Moving UP: Tasks between new position and current position-1 shift down by 1
- Moving DOWN: Tasks between current position+1 and new position shift up by 1
- Moving to same position: No-op
- Moving to position >= task count: Task is placed at the end

**JSON Output (success):** No output, exit code 0.

**Error Output:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 1 | "Sprint not found" |
| Task not in sprint | 1 | "Task N is not in sprint" |
| Invalid position | 1 | "Position must be a non-negative integer" |

#### Swap Tasks

```bash
rmp sprint swap -r <name> <sprint-id> <task-id-1> <task-id-2>
```

**Description:** Swaps the positions of two tasks within a sprint.

**Arguments:**
- `sprint-id` - Sprint identifier
- `task-id-1` - First task to swap
- `task-id-2` - Second task to swap

**Behavior:**
- Both tasks must belong to the same sprint
- Positions are exchanged between the two tasks
- No changes to other tasks

**JSON Output (success):** No output, exit code 0.

**Error Output:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 1 | "Sprint not found" |
| Task not in sprint | 1 | "Task N is not in sprint" |
| Same task ID | 1 | "Cannot swap a task with itself" |

#### Move Task to Top/Bottom

```bash
rmp sprint top -r <name> <sprint-id> <task-id>
rmp sprint bottom -r <name> <sprint-id> <task-id>
```

**Description:** Quick commands to move a task to the beginning (top) or end (bottom) of the sprint task list.

**Arguments:**
- `sprint-id` - Sprint identifier
- `task-id` - Task to move

**Behavior:**
- `top`: Equivalent to `move-to <task-id> 0`
- `bottom`: Equivalent to `move-to <task-id> <task_count>`

**JSON Output (success):** No output, exit code 0.

**Error Output:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 1 | "Sprint not found" |
| Task not in sprint | 1 | "Task N is not in sprint" |

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

**Description:** Removes a sprint and handles its associated tasks.

**Task Behavior on Sprint Removal:**

When a sprint is removed, all tasks currently associated with it are automatically returned to the backlog:

| Current Task Status | New Status | sprint_id |
|---------------------|------------|-----------|
| SPRINT | BACKLOG | NULL |
| DOING | BACKLOG | NULL |
| TESTING | BACKLOG | NULL |
| COMPLETED | BACKLOG | NULL |

**Process:**
1. Validate sprint ID exists
2. For each task in the sprint:
   - Set status to BACKLOG (regardless of current status)
   - Clear sprint_id (set to NULL)
   - Preserve all other fields (title, requirements, priority, severity, etc.)
3. Delete sprint_tasks junction table entries
4. Delete sprint from sprints table
5. Log SPRINT_DELETE operation in audit log with cascade info

**Rationale:**
- Prevents data loss by preserving task content
- Tasks return to backlog for re-prioritization and re-assignment
- No automatic deletion of tasks (user must explicitly delete tasks if desired)
- Clear audit trail of the cascade operation

**Output (success):** No output, exit code 0.

**Error Cases:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 4 | "Error: Sprint ID N not found" |
| Roadmap not specified | 1 | "Error: Roadmap not specified. Use -r <name> or --roadmap <name>" |

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

**Description:** Returns aggregated statistics about audit log entries for the specified roadmap. Optional date filters allow narrowing the statistics to a specific time period.

**Options:**
- `--since <date>` - ISO 8601 date (inclusive). If omitted, includes all entries from the beginning.
- `--until <date>` - ISO 8601 date (inclusive). If omitted, includes all entries up to now.

**JSON Output:**
```json
{
  "total_entries": 156,
  "period": {
    "since": "2026-01-01T00:00:00.000Z",
    "until": "2026-03-23T23:59:59.000Z"
  },
  "first_entry": "2026-01-15T10:30:00.000Z",
  "last_entry": "2026-03-23T14:45:00.000Z",
  "operations_count": {
    "CREATE": 45,
    "UPDATE": 67,
    "DELETE": 12,
    "STATUS_CHANGE": 32
  },
  "entity_type_count": {
    "TASK": 120,
    "SPRINT": 25,
    "ROADMAP": 11
  }
}
```

**Field Descriptions:**

| Field | Type | Description |
|-------|------|-------------|
| `total_entries` | int | Total number of audit log entries matching the filter criteria |
| `period.since` | string (ISO 8601) | Start date of the statistics period (from `--since` or first entry) |
| `period.until` | string (ISO 8601) | End date of the statistics period (from `--until` or last entry) |
| `first_entry` | string (ISO 8601) | Timestamp of the first audit entry in the period |
| `last_entry` | string (ISO 8601) | Timestamp of the last audit entry in the period |
| `operations_count` | map[string]int | Count of entries per operation type (CREATE, UPDATE, DELETE, STATUS_CHANGE, etc.) |
| `entity_type_count` | map[string]int | Count of entries per entity type (TASK, SPRINT, ROADMAP) |

**Behavior:**
- When no `--since` is specified, `period.since` equals `first_entry`
- When no `--until` is specified, `period.until` equals `last_entry`
- Empty audit log returns: `{"total_entries": 0, "period": {"since": null, "until": null}, "first_entry": null, "last_entry": null, "operations_count": {}, "entity_type_count": {}}`
- All timestamps are in ISO 8601 UTC format

**Output Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `total_entries` | int | Total number of audit entries matching the filter |
| `period.since` | string | ISO 8601 UTC timestamp of filter start (or first entry if not specified) |
| `period.until` | string | ISO 8601 UTC timestamp of filter end (or last entry if not specified) |
| `first_entry` | string | ISO 8601 UTC timestamp of the oldest audit entry in results |
| `last_entry` | string | ISO 8601 UTC timestamp of the newest audit entry in results |
| `operations_count` | map[string]int | Count of entries grouped by operation type |
| `entity_type_count` | map[string]int | Count of entries grouped by entity type |

---

## Statistics Command

Command: `rmp stats`

**Description:** Provides comprehensive statistics about a roadmap, including sprint and task distribution.

### Get Roadmap Statistics

```bash
rmp stats --roadmap <name>
rmp stats -r <name>
```

**Options:**
- `-r, --roadmap <name>` - Roadmap name (required)

**JSON Output:**
```json
{
  "roadmap": "project-name",
  "sprints": {
    "current": 5,
    "total": 12,
    "completed": 10,
    "pending": 2
  },
  "tasks": {
    "backlog": 15,
    "sprint": 8,
    "doing": 5,
    "testing": 3,
    "completed": 42
  }
}
```

**Output Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `roadmap` | string | Name of the roadmap |
| `sprints.current` | integer or null | ID of the currently open sprint, or null if no sprint is open |
| `sprints.total` | integer | Total number of sprints in the roadmap |
| `sprints.completed` | integer | Number of sprints with status CLOSED |
| `sprints.pending` | integer | Number of sprints with status OPEN (typically 0 or 1) |
| `tasks.backlog` | integer | Number of tasks with status BACKLOG |
| `tasks.sprint` | integer | Number of tasks with status SPRINT |
| `tasks.doing` | integer | Number of tasks with status DOING |
| `tasks.testing` | integer | Number of tasks with status TESTING |
| `tasks.completed` | integer | Number of tasks with status COMPLETED |

**Error Cases:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Roadmap not specified | 3 | "Error: no roadmap selected: no roadmap selected" |
| Roadmap not found | 4 | "Error: resource not found: roadmap 'name'" |

**Behavior Notes:**
- The `sprints.current` field returns the ID of the sprint with status OPEN, or `null` if no sprint is currently open
- The `sprints.pending` field reflects sprints with status OPEN (normally only one sprint can be open at a time)
- The sum of all task statuses equals the total number of tasks in the roadmap

---

## Command Aliases Reference

| Command | Aliases |
|---------|---------|
| `roadmap` | `road` |
| `task` | `t` |
| `sprint` | `s` |
| `audit` | `aud` |
| `stats` | - |
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
| `reorder` | `order` |
| `move-to` | `mvto` |
| `swap` | - |
| `top` | - |
| `bottom` | `btm` |
