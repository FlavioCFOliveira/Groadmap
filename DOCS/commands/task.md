# task

## Description

Task management within a roadmap. Tasks track work with status, type, priority, severity, specialists, dependencies, and detailed requirements. Every `task` subcommand operates on a single roadmap, which MUST be selected with the required `-r`/`--roadmap` flag.

## Synopsis

```
rmp task [subcommand] [arguments] [flags]
```

The `task` command has the alias `t`.

## Subcommands

### list

Lists tasks in the selected roadmap (any status). All filters compose with AND.

**Usage:** `rmp task list -r <roadmap> [filters]` or `rmp task ls -r <roadmap> [filters]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-s` | `--status` | enum | - | Filter by exact status: BACKLOG, SPRINT, DOING, TESTING, COMPLETED |
| `-p` | `--priority` | int | - | Filter: keep tasks with priority `>= min`. Lower-bound filter, not a validated `0-9` value: out-of-range numbers are accepted and simply match accordingly |
| N/A | `--severity` | int | - | Filter: keep tasks with severity `>= min`. Lower-bound filter, not a validated `0-9` value |
| `-y` | `--type` | enum | - | Filter by task type (one of the 10 task types) |
| `-sp` | `--specialists` | string | - | Filter by specialists (case-insensitive substring) |
| N/A | `--created-since` | date | - | Include tasks created on/after this date (RFC3339 or YYYY-MM-DD) |
| N/A | `--created-until` | date | - | Include tasks created on/before this date (RFC3339 or YYYY-MM-DD) |
| N/A | `--sort` | enum | `priority` | Sort field: priority, created, status, severity |
| `-l` | `--limit` | int | `100` | Maximum results (1-100) |

**Sort fields:**
- `priority` - by priority descending (default)
- `created` - by created_at ascending
- `status` - by status (state-machine order)
- `severity` - by severity descending

**Output:** JSON array of Task objects.

**Examples:**
```bash
rmp task list -r project1
rmp task list -r project1 --status BACKLOG --priority 5 --sort priority
rmp task list -r project1 --created-since 2026-01-01 --type BUG
rmp task ls -r project1 -p 5 -l 20
```

---

### create

Creates a new task. The task lands in `BACKLOG` status.

**Usage:** `rmp task create -r <roadmap> -t <title> -fr <FR> -tr <TR> -ac <AC> [options]` or `rmp task new ...`

**Required Flags:**
| Short Flag | Long Flag | Type | Max Length | Description |
|------------|-----------|------|------------|-------------|
| `-r` | `--roadmap` | string | 50 | Roadmap name (required) |
| `-t` | `--title` | string | 255 | Task title |
| `-fr` | `--functional-requirements` | string | 4096 | Functional requirements (Why?) |
| `-tr` | `--technical-requirements` | string | 4096 | Technical requirements (How?) |
| `-ac` | `--acceptance-criteria` | string | 4096 | Acceptance criteria (How to verify?) |

**Optional Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-y` | `--type` | enum | `TASK` | Task type (one of the 10 task types) |
| `-p` | `--priority` | int | `0` | Priority 0-9 (0 lowest, 9 highest) |
| N/A | `--severity` | int | `0` | Severity 0-9 (0 lowest, 9 highest) |
| `-sp` | `--specialists` | string | - | Comma-separated specialists (max 500 chars) |
| N/A | `--parent` | int | - | Parent task ID; creates this task as a SUB_TASK of the given parent and bumps the parent's `subtask_count` (create only) |

**Output:** JSON object with the created task ID.

**Examples:**
```bash
rmp task create -r project1 -t "Fix login bug" -fr "User can login" -tr "Update auth" -ac "Login works"
rmp task create -r project1 -t "Add metrics" --type CHORE -p 3 -fr "Track usage" -tr "Add counters" -ac "Metrics emitted"
rmp task create -r project1 -t "Validate input" --parent 42 -fr "Reject bad input" -tr "Add guards" -ac "Invalid input rejected"
```

**Example output:**
```json
{"id": 42}
```

---

### get

Gets one or more tasks by id. Fails fast on any unknown id.

**Usage:** `rmp task get -r <roadmap> <task-ids>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-ids` | Yes | Comma-separated integer ids (no spaces) |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** JSON array of Task objects.

**Examples:**
```bash
rmp task get -r project1 42
rmp task get -r project1 1,3,5
```

---

### next

Retrieves the next N incomplete tasks from the currently OPEN sprint. Tasks are returned in the sprint's position order (set via sprint reorder commands), allowing the team to define execution sequence independent of priority/severity.

**Usage:** `rmp task next -r <roadmap> [num]`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `num` | No | Maximum tasks to return (default: 1, clamped to 100) |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** JSON array of Task objects (statuses SPRINT/DOING/TESTING).

**Examples:**
```bash
rmp task next -r project1        # Returns 1 task
rmp task next -r project1 5      # Returns up to 5 tasks
```

**Error Cases:**
- Returns an error (exit 4) if no sprint is currently OPEN.
- Returns an empty array if the sprint has no incomplete tasks.

---

### edit

Edits one or more fields of an existing task. Only specified fields are updated, and at least one field option must be provided. Status is NOT editable here (use `stat` or `reopen`).

**Usage:** `rmp task edit -r <roadmap> <task-id> [options]`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-id` | Yes | Integer id of the task to edit |

**Flags:**
| Short Flag | Long Flag | Type | Max Length / Range | Description |
|------------|-----------|------|--------------------|-------------|
| `-r` | `--roadmap` | string | 50 | Roadmap name (required) |
| `-t` | `--title` | string | 255 | New title |
| `-fr` | `--functional-requirements` | string | 4096 | New functional requirements |
| `-tr` | `--technical-requirements` | string | 4096 | New technical requirements |
| `-ac` | `--acceptance-criteria` | string | 4096 | New acceptance criteria |
| `-y` | `--type` | enum | - | New task type (one of the 10 task types) |
| `-p` | `--priority` | int | 0-9 | New priority |
| N/A | `--severity` | int | 0-9 | New severity |
| `-sp` | `--specialists` | string | 500 | New specialists (comma-separated) |

**Output:** Empty on success (exit 0).

**Examples:**
```bash
rmp task edit -r project1 42 -t "Updated title" -p 8
rmp task edit -r project1 1 --specialists "go-developer"
```

---

### remove

Removes one or more tasks permanently. All target tasks MUST be in `BACKLOG` status and free of active subtasks. This action cannot be undone.

**Usage:** `rmp task remove -r <roadmap> <task-ids>` or `rmp task rm -r <roadmap> <task-ids>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-ids` | Yes | Comma-separated integer ids |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** Empty on success (exit 0).

**Examples:**
```bash
rmp task remove -r project1 42
rmp task rm -r project1 1,2,3
```

A task that is not in `BACKLOG` is rejected (exit 6).

---

### stat (set-status)

Changes the status of one or more tasks (manual transitions). Rejected transitions return exit 6.

**Usage:** `rmp task stat -r <roadmap> <task-ids> <new-status> [--summary <text>]` or `rmp task set-status ...`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-ids` | Yes | Comma-separated integer ids |
| `new-status` | Yes | Target status: BACKLOG, DOING, TESTING, COMPLETED (SPRINT is rejected) |

**Flags:**
| Short Flag | Long Flag | Type | Max Length | Description |
|------------|-----------|------|------------|-------------|
| `-r` | `--roadmap` | string | 50 | Roadmap name (required) |
| `-s` | `--summary` | string | 4096 | Completion summary; valid only when the target status is COMPLETED |

**Status Flow:**
```
BACKLOG --[sprint add-tasks]--> SPRINT --[stat DOING]--> DOING --[stat TESTING]--> TESTING --[stat COMPLETED]--> COMPLETED
COMPLETED --[reopen / stat BACKLOG]--> BACKLOG
```

**Rules:**
- `stat <ids> SPRINT` is rejected (exit 6). Use `sprint add-tasks` instead; SPRINT is only set automatically.
- Marking COMPLETED is rejected (exit 6) if any subtask or dependency is not yet COMPLETED.
- The `--summary` text is recorded as `completion_summary` and is only accepted on the TESTING -> COMPLETED transition.

**Examples:**
```bash
rmp task stat -r project1 1,2,3 DOING
rmp task stat -r project1 7 COMPLETED --summary "Shipped behind feature flag"
```

---

### reopen

Returns one or more tasks to `BACKLOG` and clears their lifecycle timestamps (`started_at`, `tested_at`, `closed_at`) and `completion_summary`.

**Usage:** `rmp task reopen -r <roadmap> <task-ids>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-ids` | Yes | Comma-separated integer ids |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** Empty on success (exit 0).

**Examples:**
```bash
rmp task reopen -r project1 7
rmp task reopen -r project1 1,3,5
```

---

### prio (set-priority)

Sets the priority of one or more tasks to the same value.

**Usage:** `rmp task prio -r <roadmap> <task-ids> <priority>` or `rmp task set-priority ...`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-ids` | Yes | Comma-separated integer ids |
| `priority` | Yes | Integer 0-9 (0 lowest, 9 highest) |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Priority Scale:**
- 0 = lowest urgency
- 9 = maximum urgency (Product Owner perspective)

**Examples:**
```bash
rmp task prio -r project1 42 9
rmp task set-priority -r project1 1,2,3 5
```

---

### sev (set-severity)

Sets the severity of one or more tasks to the same value.

**Usage:** `rmp task sev -r <roadmap> <task-ids> <severity>` or `rmp task set-severity ...`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-ids` | Yes | Comma-separated integer ids |
| `severity` | Yes | Integer 0-9 (0 lowest, 9 highest) |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Severity Scale:**
- 0 = minimal impact
- 9 = critical impact (Dev Team perspective)

**Examples:**
```bash
rmp task sev -r project1 5 9
rmp task set-severity -r project1 1,2,3 9
```

---

### assign

Adds a specialist to the task's specialists list. The operation is idempotent: assigning an already-present specialist succeeds (exit 0) and emits an informational note to stderr.

**Usage:** `rmp task assign -r <roadmap> <task-id> <specialist>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-id` | Yes | Integer task id |
| `specialist` | Yes | Free-form specialist label |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** Empty on success (exit 0).

**Examples:**
```bash
rmp task assign -r project1 7 go-developer
```

---

### unassign

Removes a specialist from the task's specialists list. The operation is idempotent: removing an absent specialist still succeeds (exit 0).

**Usage:** `rmp task unassign -r <roadmap> <task-id> <specialist>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-id` | Yes | Integer task id |
| `specialist` | Yes | Specialist label to remove |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** Empty on success (exit 0).

**Examples:**
```bash
rmp task unassign -r project1 7 go-developer
```

---

### subtasks

Lists the direct subtasks of a task (one level only; no grand-children).

**Usage:** `rmp task subtasks -r <roadmap> <task-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-id` | Yes | Integer id of the parent task |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** JSON array of Task objects.

**Examples:**
```bash
rmp task subtasks -r project1 5
```

---

### add-dep

Records that a task depends on another task (the blocker, which must complete first). Self-edges and dependency cycles are rejected (exit 6). The operation is idempotent.

**Usage:** `rmp task add-dep -r <roadmap> <task-id> <blocker-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-id` | Yes | Integer id of the dependent task |
| `blocker-id` | Yes | Integer id of the task that must complete first |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** Empty on success (exit 0).

**Examples:**
```bash
rmp task add-dep -r project1 10 7   # task 10 depends on task 7
```

---

### remove-dep

Removes the dependency edge previously created by `add-dep`.

**Usage:** `rmp task remove-dep -r <roadmap> <task-id> <blocker-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-id` | Yes | Integer id of the dependent task |
| `blocker-id` | Yes | Integer id of the task that was a blocker |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** Empty on success (exit 0).

**Examples:**
```bash
rmp task remove-dep -r project1 10 7
```

---

### blockers

Lists the tasks that a given task depends on and that are not yet COMPLETED (its incomplete dependencies).

**Usage:** `rmp task blockers -r <roadmap> <task-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-id` | Yes | Integer task id |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** JSON array of Task objects (incomplete dependencies).

**Examples:**
```bash
rmp task blockers -r project1 10
```

---

### blocking

Lists the tasks that depend on a given task (the reverse of `blockers`).

**Usage:** `rmp task blocking -r <roadmap> <task-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-id` | Yes | Integer task id |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** JSON array of Task objects (downstream dependents).

**Examples:**
```bash
rmp task blocking -r project1 7
```

## Aliases

| Command | Alias |
|---------|-------|
| `task` | `t` |
| `list` | `ls` |
| `create` | `new` |
| `remove` | `rm` |
| `stat` | `set-status` |
| `prio` | `set-priority` |
| `sev` | `set-severity` |

The only alias for `task remove` is `rm`. The `delete` alias exists for `roadmap remove`, not for `task remove`.

## Notes

- The `-r`/`--roadmap` flag is REQUIRED on every `task` subcommand. There is no default or active roadmap.
- Tasks are created with `BACKLOG` status by default.
- Status transitions are validated according to the state machine (see SPEC/STATE_MACHINE.md).
- `SPRINT` status is set automatically by `sprint add-tasks`; it cannot be set manually via `stat`.
- When transitioning to `DOING`, the `started_at` field is set automatically.
- When transitioning to `TESTING`, the `tested_at` field is set automatically.
- When transitioning to `COMPLETED`, the `closed_at` field is set automatically; an optional `--summary` records `completion_summary`.
- When reopening to `BACKLOG` (via `reopen` or `stat BACKLOG` from COMPLETED), `started_at`, `tested_at`, `closed_at`, and `completion_summary` are cleared.
- Marking a task COMPLETED is rejected if any of its subtasks or dependencies is not yet COMPLETED.

## Field Limits and Constraints

| Field | Required | Max Length / Range | Description |
|-------|----------|--------------------|-------------|
| `roadmap` | Yes | 50 chars (regex `^[a-z0-9_-]+$`) | Target roadmap name |
| `title` | Yes (on create) | 255 chars | Task title/summary |
| `functional-requirements` | Yes (on create) | 4096 chars | Why: functional requirements |
| `technical-requirements` | Yes (on create) | 4096 chars | How: technical description |
| `acceptance-criteria` | Yes (on create) | 4096 chars | How to verify: completion criteria |
| `specialists` | No | 500 chars | Comma-separated specialist tags |
| `summary` | No | 4096 chars | Completion summary (only on COMPLETED transition) |
| `type` | No | one of 10 task types | Task type (default: TASK) |
| `priority` | No | 0-9 | Priority level (default: 0) |
| `severity` | No | 0-9 | Severity level (default: 0) |

### Task Status Values

- `BACKLOG` - Task in backlog, not assigned to a sprint
- `SPRINT` - Task assigned to a sprint (set automatically by `sprint add-tasks`)
- `DOING` - Task in progress
- `TESTING` - Task being tested
- `COMPLETED` - Task finished

### Task Type Values

- `USER_STORY` - New feature from the end user's perspective (who/what/why)
- `TASK` - Internal work unit, necessary but not directly user-facing (default)
- `BUG` - Report of something not working as expected in existing code
- `SUB_TASK` - Decomposition of a Story or Task into smaller steps
- `EPIC` - Large body of work grouping multiple Stories and Tasks; spans multiple sprints
- `REFACTOR` - Improvement of internal code structure without changing behaviour
- `CHORE` - Necessary maintenance that does not add features or fix bugs
- `SPIKE` - Research or prototyping task to reduce technical uncertainty
- `DESIGN_UX` - Prototypes, wireframes, or interface flows
- `IMPROVEMENT` - Refinement of an existing working feature

### Task Object Keys

Task objects returned by `list`, `get`, `next`, `subtasks`, `blockers`, and `blocking` contain:
`id`, `title`, `status`, `type`, `functional_requirements`, `technical_requirements`, `acceptance_criteria`, `created_at`, `specialists`, `started_at`, `tested_at`, `closed_at`, `completion_summary`, `parent_task_id`, `priority`, `severity`, `subtask_count`, `depends_on`, `blocks`.

## Output Format

All commands follow these conventions:
- **Success:** JSON to stdout, exit code 0. `create` emits `{"id": <int>}`; read commands emit a JSON array; mutating commands (`edit`, `stat`, `prio`, `sev`, `reopen`, `remove`, `assign`, `unassign`, `add-dep`, `remove-dep`) emit empty stdout.
- **Errors:** Plain text to stderr, non-zero exit code.
- **Dates:** ISO 8601 UTC with milliseconds, suffix `Z` (e.g. `2026-05-24T14:30:00.000Z`).

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Misuse: missing required flag, bad syntax, or an invalid id argument (a `<task-id>`, `<task-ids>`, or `<blocker-id>` that is not a positive integer is rejected by the parser before any database access) |
| 3 | No roadmap specified (`-r` missing) |
| 4 | Task not found (a syntactically valid id that does not exist) |
| 6 | Validation error: bad `--type`/`--status`/enum value, out-of-range number, oversized field, invalid state transition (including `stat SPRINT`), subtask/dependency guard, or dependency cycle |

Note the distinction: an id that is not a positive integer (for example `abc` or `0`) is an exit-code-2 syntax error, whereas a well-formed id for a task that does not exist is an exit-code-4 not-found error. An invalid `--type` or target status value is an exit-code-6 validation error.
