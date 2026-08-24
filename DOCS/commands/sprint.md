# sprint

## Description

Sprint management within a roadmap. Sprints group tasks into time-boxed iterations with lifecycle management (PENDING → OPEN → CLOSED), and each sprint carries an append-oriented comment log recording how the sprint went.

## Synopsis

```
rmp sprint [subcommand] [arguments] [flags]
```

## Subcommands

### list

Lists sprints in the selected roadmap.

**Usage:** `rmp sprint list [OPTIONS]` or `rmp sprint ls [OPTIONS]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| | `--status` | string | - | Filter by status: PENDING, OPEN, CLOSED |

**Output:** JSON array of Sprint objects

**Examples:**
```bash
rmp sprint list -r project1
rmp sprint ls -r project1 --status OPEN
```

---

### create

Creates a new sprint (status `PENDING`) in the specified roadmap. Both `-t`/`--title` and `-d`/`--description` are mandatory; omitting either fails with exit code 2.

**Usage:** `rmp sprint create [OPTIONS]` or `rmp sprint new [OPTIONS]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-t` | `--title` | string | - | Sprint title, max 255 chars. **Required** on create |
| `-d` | `--description` | string | - | Sprint description, max 2048 chars. **Required** on create. Must state the sprint's high-level (macro) goal - see below |
| | `--order` | integer | auto | Execution order; positive integer (`> 0`), unique across sprints. When omitted, auto-assigned to `MAX(order) + 1` (first sprint is `1`) |
| | `--max-tasks` | integer | - | Capacity cap on active tasks (range 1-10000); cannot be removed once set |

**Output:** JSON object with the created sprint ID

**The `description` field** must state the high-level (macro) goal of the
development effort the sprint delivers: a new development, a fix, a refactoring,
or another kind of change. Together with the `title`, it must give a human reader
or an AI agent a clear macro idea of what the sprint's tasks are specifically
aimed at. It states the macro goal only - detailed scope, technical detail, and
acceptance conditions belong in the sprint's tasks, which specify them in full
through their functional requirements, technical requirements, and acceptance
criteria. A label such as `"Sprint 3"` or a restatement of the title does not
satisfy this contract.

**The `order` field** indicates the natural, sequential order in which sprints
must be executed. It must be a positive integer (`> 0`) and unique across all
sprints in the roadmap. A non-positive or non-integer value exits with code 6;
an order already used by another sprint exits with code 5.

**Examples:**
```bash
rmp sprint create -r project1 -t "Auth hardening" -d "Deliver session-based authentication for every write command."
rmp sprint new -r project1 -t "Ordering fixes" -d "Fix the task-ordering defects reported in v1.12."
rmp sprint create -r project1 -t "Storage refactor" -d "Refactor persistence onto a single write path." --order 3
```

**Example output:**
```json
{"id": 1}
```

---

### get

Gets detailed information about a specific sprint.

**Usage:** `rmp sprint get [OPTIONS] <id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Sprint ID |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** JSON Sprint object, including its `title`, `description`, and `order` fields

**Example:**
```bash
rmp sprint get -r project1 1
```

---

### show

Displays a comprehensive status report of a sprint, including task statistics, progress percentages, severity distribution, and criticality distribution. Ideal for sprint stand-up meetings and progress tracking.

**Usage:** `rmp sprint show [OPTIONS] <id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Sprint ID |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** JSON object with comprehensive sprint report

**Example:**
```bash
rmp sprint show -r project1 1
```

**Example output:**
```json
{
  "sprint_id": 1,
  "sprint_title": "Auth hardening",
  "sprint_description": "Deliver session-based authentication for every write command.",
  "status": "OPEN",
  "max_tasks": null,
  "capacity_pct": null,
  "current_load": 12,
  "task_order": [5, 3, 8, 1, 9, 2, 7, 4, 6, 10],
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
| `sprint_title` | string | Sprint title |
| `sprint_description` | string | Sprint description |
| `status` | string | Sprint status (PENDING, OPEN, CLOSED) |
| `max_tasks` | integer or null | Capacity cap on active tasks; `null` when uncapped |
| `capacity_pct` | float or null | `current_load / max_tasks * 100`; `null` when uncapped |
| `current_load` | integer | Count of active tasks (SPRINT, DOING, TESTING) |
| `task_order` | array of integers | Task IDs in sprint position order |
| `summary.total_tasks` | integer | Total number of tasks in sprint |
| `summary.pending` | integer | Tasks with status BACKLOG or SPRINT |
| `summary.in_progress` | integer | Tasks with status DOING or TESTING |
| `summary.completed` | integer | Tasks with status COMPLETED |
| `progress.pending_percentage` | float | Percentage of pending tasks |
| `progress.in_progress_percentage` | float | Percentage of tasks in progress |
| `progress.completed_percentage` | float | Percentage of completed tasks |
| `severity_distribution` | object | Task distribution by severity ranges |
| `criticality_distribution` | object | Task distribution by criticality levels |

---

### tasks

Lists tasks assigned to a specific sprint.

**Usage:** `rmp sprint tasks [OPTIONS] <sprint-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `sprint-id` | Yes | Sprint ID |

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-s` | `--status` | enum | - | Filter by exact task status: `BACKLOG`, `SPRINT`, `DOING`, `TESTING`, `COMPLETED`. An invalid value fails with exit code 6. Applies to `tasks` only, not `open-tasks` |
| | `--order-by-priority` | bool | false | Order by priority DESC, then sprint position ASC, instead of position only |

Lists ALL tasks in the sprint, including COMPLETED ones. Default order is by sprint position ASC. Use `--status` to restrict to one status, or `open-tasks` to list only incomplete tasks.

**Output:** JSON array of Task objects

**Examples:**
```bash
rmp sprint tasks -r project1 1
rmp sprint tasks -r project1 1 --status DOING
rmp sprint tasks -r project1 1 --order-by-priority
```

---

### open-tasks

Lists only the actively-open tasks of a sprint, i.e. tasks whose status is SPRINT, DOING, or TESTING. BACKLOG and COMPLETED tasks are excluded. Useful for stand-ups and burndown tracking.

**Usage:** `rmp sprint open-tasks [OPTIONS] <sprint-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `sprint-id` | Yes | Sprint ID |

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| | `--order-by-priority` | bool | false | Sort by priority DESC; otherwise by sprint position ASC |

**Output:** JSON array of Task objects (excludes BACKLOG and COMPLETED)

**Examples:**
```bash
rmp sprint open-tasks -r project1 1
rmp sprint open-tasks -r project1 1 --order-by-priority
```

---

### stats

Shows statistics for a sprint: per-status counts, completion percentage, the ordered task IDs, a burndown series (one entry per day on which tasks were completed), velocity (tasks/day), and elapsed days.

**Usage:** `rmp sprint stats [OPTIONS] <id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Sprint ID |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** JSON statistics object

**Example:**
```bash
rmp sprint stats -r project1 1
```

**Example output:**
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
  "task_order": [5, 3, 8, 1, 9, 2, 7, 4, 6, 10],
  "burndown": [
    {"date": "2026-06-15", "tasks_remaining": 9},
    {"date": "2026-06-16", "tasks_remaining": 7}
  ],
  "velocity": 0.0,
  "days_elapsed": 2,
  "days_remaining": null
}
```

**Output Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `sprint_id` | integer | Sprint identifier |
| `total_tasks` | integer | Total number of tasks in the sprint |
| `completed_tasks` | integer | Tasks with status COMPLETED |
| `progress_percentage` | float | Completion percentage (0.0-100.0) |
| `status_distribution` | object | Count of tasks per status |
| `task_order` | array of integers | Task IDs in sprint position order |
| `burndown` | array | One entry per day on which tasks completed; empty when none completed |
| `velocity` | float | Tasks completed per day |
| `days_elapsed` | integer or null | Days the sprint has run |
| `days_remaining` | integer or null | Always `null` (the Sprint model has no end date) |

**Notes:**

- `velocity` is `0.0` for PENDING and OPEN sprints, and for CLOSED sprints with zero completed tasks. It is only meaningful for CLOSED sprints.
- `days_elapsed` is `null` for PENDING sprints and for OPEN sprints with no `started_at`. For CLOSED sprints it spans `started_at` to `closed_at`.
- `days_remaining` is ALWAYS `null`, because the Sprint model has no end date to count down to.
- `burndown` is empty when no tasks have been completed in the sprint.

---

### start

Starts a sprint, changing its status from PENDING to OPEN.

**Usage:** `rmp sprint start [OPTIONS] <id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Sprint ID to start |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Example:**
```bash
rmp sprint start -r project1 1
```

---

### close

Closes a sprint, changing its status from OPEN to CLOSED. By default, the close is rejected (exit code 6) if any task in the sprint is still SPRINT, DOING, or TESTING. Pass `--force` to close anyway; a warning is then printed to stderr. Closing sets `closed_at` to the current timestamp.

**Usage:** `rmp sprint close [OPTIONS] <id> [--force]`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Sprint ID to close |

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| | `--force` | bool | false | Close even when the sprint has active (SPRINT/DOING/TESTING) tasks |

**Examples:**
```bash
rmp sprint close -r project1 1
rmp sprint close -r project1 1 --force
```

---

### reopen

Reopens a closed sprint, changing its status from CLOSED to OPEN.

**Usage:** `rmp sprint reopen [OPTIONS] <id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Sprint ID to reopen |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Example:**
```bash
rmp sprint reopen -r project1 1
```

---

### add-tasks

Adds tasks to a sprint. Tasks must be in BACKLOG status.

**Usage:** `rmp sprint add-tasks [OPTIONS] <sprint-id> <task-ids>` or `rmp sprint add [OPTIONS] <sprint-id> <task-ids>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `sprint-id` | Yes | Sprint ID to add tasks to |
| `task-ids` | Yes | Task IDs separated by commas (no spaces) |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Audit:** two mirrored entries per task. A `SPRINT_ADD_TASK` entry against the sprint names the task in its `related_entity_id` field, and a `TASK_STATUS_SPRINT` entry against the task names the sprint. The pair shares one `performed_at`, so `audit history SPRINT <id>` says which tasks joined and `audit history TASK <id>` says which sprint a task joined, without either reader consulting the other entity's history.

**Examples:**
```bash
rmp sprint add-tasks -r project1 1 10,11,12
rmp sprint add -r project1 2 5,6,7,8
```

---

### remove-tasks

Removes tasks from a sprint. Tasks return to BACKLOG status: the transition clears each task's lifecycle timestamps (`started_at`, `tested_at`, `closed_at`), its `completion_summary`, and its `commit_close`, and leaves `commit_open` untouched.

**Usage:** `rmp sprint remove-tasks [OPTIONS] <sprint-id> <task-ids>` or `rmp sprint rm-tasks [OPTIONS] <sprint-id> <task-ids>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `sprint-id` | Yes | Sprint ID to remove tasks from |
| `task-ids` | Yes | Task IDs separated by commas |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Audit:** two mirrored entries per task, on the same rule as `add-tasks`. A `SPRINT_REMOVE_TASK` entry against the sprint names the task, and a `TASK_STATUS_BACKLOG` entry against the task names the sprint it left. This is the only way a `TASK_STATUS_BACKLOG` entry acquires a counterpart; the one `task stat <ids> BACKLOG` writes has none, because no sprint is party to that operation.

**Examples:**
```bash
rmp sprint remove-tasks -r project1 1 10,11,12
rmp sprint rm-tasks -r project1 1 5,6
```

---

### move-tasks

Moves tasks between sprints.

**Usage:** `rmp sprint move-tasks [OPTIONS] <from-sprint> <to-sprint> <task-ids>` or `rmp sprint mv-tasks [OPTIONS] <from-sprint> <to-sprint> <task-ids>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `from-sprint` | Yes | Source sprint ID |
| `to-sprint` | Yes | Destination sprint ID |
| `task-ids` | Yes | Task IDs separated by commas |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Audit:** two entries per task, both against sprints: `SPRINT_MOVE_TASK_OUT` against the source sprint and `SPRINT_MOVE_TASK_IN` against the destination, each naming the task moved in its `related_entity_id` field. No `TASK_STATUS_*` entry is written, because a move preserves each task's status. No `SPRINT_MOVE_TASK` entry is written either: that operation is legacy (see [DOCS/commands/audit.md](audit.md)).

**Examples:**
```bash
rmp sprint move-tasks -r project1 1 2 10,11,12
rmp sprint mv-tasks -r project1 2 3 5,6,7
```

---

### update

Updates a sprint's title, description, capacity cap, or execution order. At
least one of `--title`, `--description`, `--max-tasks`, or `--order` must be
provided.

**Usage:** `rmp sprint update [OPTIONS] <id>` or `rmp sprint upd [OPTIONS] <id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Sprint ID |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |
| `-t` | `--title` | string | New title, max 255 chars (optional) |
| `-d` | `--description` | string | New description, max 2048 chars (optional). Carries the same semantics as on create: it must state the sprint's high-level (macro) goal |
| | `--order` | integer | New execution order; positive and unique (optional) |
| | `--max-tasks` | integer | New capacity cap on active tasks (optional) |

**Order immutability:** the `--order` value may only be changed while the sprint
is `PENDING` or `OPEN`. Once a sprint is `CLOSED` its order is immutable and
attempting to change it exits with code 6, preserving the historical execution
record. (Reopening a sprint returns it to `OPEN`, making its order editable
again.) A non-positive value exits with code 6; an order already used by another
sprint exits with code 5.

**Audit:** one entry per column the invocation supplies, drawn from `SPRINT_TITLE_CHANGE`, `SPRINT_DESCRIPTION_CHANGE`, `SPRINT_MAX_TASKS_CHANGE`, and `SPRINT_ORDER_CHANGE`. An update supplying two columns writes two entries. The entry records that the column was written, not its old or new value. No `SPRINT_UPDATE` entry is written: that operation is legacy (see [DOCS/commands/audit.md](audit.md)).

**Examples:**
```bash
rmp sprint update -r project1 1 -t "Auth and tracing"
rmp sprint update -r project1 1 -d "Deliver authentication and request tracing for every write command."
rmp sprint update -r project1 1 --order 2
rmp sprint upd -r project1 1 -t "Storage refactor" -d "Refactor persistence onto a single write path."
```

---

### remove

Removes a sprint permanently. Member tasks are not deleted; their status reverts to `BACKLOG`, which clears their lifecycle timestamps, their `completion_summary`, and their `commit_close`, and leaves `commit_open` untouched.

**Usage:** `rmp sprint remove [OPTIONS] <id>` or `rmp sprint rm [OPTIONS] <id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Sprint ID to remove |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Audit:** one `SPRINT_DELETE` entry, and nothing else. No per-task entry is written even though every member task reverts to `BACKLOG`: the membership rows go away with the sprint, and the sprint such an entry would have named no longer exists once the deletion commits. The sprint's earlier entries survive the deletion.

**Examples:**
```bash
rmp sprint remove -r project1 1
rmp sprint rm -r project1 2
```

---

## Task Ordering Commands

Commands for managing the execution order of tasks within a sprint. Tasks are ordered by position (0-based), where position 0 is the first task in the sprint.

### reorder

Sets the exact order of all tasks in a sprint. The order of task IDs in the argument defines the new sequence.

**Usage:** `rmp sprint reorder [OPTIONS] <sprint-id> <task-ids>` or `rmp sprint order [OPTIONS] <sprint-id> <task-ids>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `sprint-id` | Yes | Sprint ID |
| `task-ids` | Yes | Comma-separated list of ALL task IDs in desired order |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Validation:**
- All task IDs must belong to the specified sprint
- No duplicate task IDs allowed
- Must include ALL sprint tasks (partial reorder not supported)

**Examples:**
```bash
rmp sprint reorder -r project1 1 5,3,1,4,2
rmp sprint order -r project1 1 10,11,12,13,14
```

**Example output:**
```json
{
  "success": true,
  "sprint_id": 1,
  "task_order": [5, 3, 1, 4, 2]
}
```

---

### move-to

Moves a single task to a specific position within a sprint, shifting other tasks accordingly.

**Usage:** `rmp sprint move-to [OPTIONS] <sprint-id> <task-id> <position>` or `rmp sprint mvto [OPTIONS] <sprint-id> <task-id> <position>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `sprint-id` | Yes | Sprint ID |
| `task-id` | Yes | Task ID to move |
| `position` | Yes | Target position (0-based). If >= task count, moves to end |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Behavior:**
- Moving UP: Tasks between new position and current position-1 shift down by 1
- Moving DOWN: Tasks between current position+1 and new position shift up by 1
- Moving to same position: No-op
- Moving to position >= task count: Task is placed at the end

**Examples:**
```bash
rmp sprint move-to -r project1 1 5 0    # Move task 5 to position 0 (top)
rmp sprint move-to -r project1 1 5 3    # Move task 5 to position 3
rmp sprint mvto -r project1 1 10 5    # Move task 10 to position 5
```

**Example output:**
```json
{
  "success": true,
  "sprint_id": 1,
  "task_id": 5,
  "position": 0
}
```

---

### swap

Swaps the positions of two tasks within a sprint.

**Usage:** `rmp sprint swap [OPTIONS] <sprint-id> <task-id-1> <task-id-2>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `sprint-id` | Yes | Sprint ID |
| `task-id-1` | Yes | First task ID |
| `task-id-2` | Yes | Second task ID |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Validation:**
- Both tasks must belong to the same sprint
- Task IDs must be different

**Examples:**
```bash
rmp sprint swap -r project1 1 5 3    # Swap positions of tasks 5 and 3
```

**Example output:**
```json
{
  "success": true,
  "sprint_id": 1,
  "task_id_1": 5,
  "task_id_2": 3
}
```

---

### top

Moves a task to the top of the sprint (position 0).

**Usage:** `rmp sprint top [OPTIONS] <sprint-id> <task-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `sprint-id` | Yes | Sprint ID |
| `task-id` | Yes | Task ID to move |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Behavior:** Equivalent to `move-to <task-id> 0`

**Examples:**
```bash
rmp sprint top -r project1 1 5    # Move task 5 to position 0
```

---

### bottom

Moves a task to the bottom of the sprint (last position).

**Usage:** `rmp sprint bottom [OPTIONS] <sprint-id> <task-id>` or `rmp sprint btm [OPTIONS] <sprint-id> <task-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `sprint-id` | Yes | Sprint ID |
| `task-id` | Yes | Task ID to move |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Behavior:** Equivalent to `move-to <task-id> <task_count>`

**Examples:**
```bash
rmp sprint bottom -r project1 1 5    # Move task 5 to last position
rmp sprint btm -r project1 1 10    # Move task 10 to last position
```

---

## Comment Commands

Commands for a sprint's progression log: a durable, append-oriented record of how the sprint went. A sprint comment records only the progression of the work during the sprint's development — findings, decisions taken, progress, and the reason behind a change to the sprint's definition. Work carried out inside one task belongs in that task's own comments, not here (see [DOCS/commands/task.md](task.md)).

The four subcommands mirror the four task comment subcommands exactly. Two differences apply throughout:

- **The accepted type set is smaller.** A sprint comment accepts `FINDING`, `DECISION`, `PROGRESS`, and `UPDATE`. The task-only values `HYPOTHESIS`, `TEST`, and `NOTE` are rejected with exit code 6. See [Comment Type Values](#comment-type-values).
- **The id space is separate.** A comment id here identifies a sprint comment. The same number in the `task` family identifies an unrelated task comment.

Comments are accepted in every sprint status, including `CLOSED`. No comment subcommand checks or changes a sprint's status. `-y`/`--type` has no other meaning in the `sprint` family: `comment-add`, `comment-list`, and `comment-edit` are the only subcommands that accept it, and it always carries a comment type. `comment-remove` takes no `-y`/`--type` at all and rejects the flag as unknown (exit 2).

### comment-add

Adds one comment to a sprint's progression log. The comment is stored with its type, its body, and a creation timestamp; `updated_at` starts null.

**Usage:** `rmp sprint comment-add [OPTIONS] <sprint-id>` or `rmp sprint c-add [OPTIONS] <sprint-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `sprint-id` | Yes | Sprint ID the comment is attached to |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |
| `-y` | `--type` | enum | Comment type (required, no default); one of the four sprint comment types |
| `-b` | `--body` | string | Comment text, max 4096 chars. When the flag is absent, the body is read from standard input (see below) |

**Body from standard input:** when `--body` is absent, the whole of standard input is read to EOF and used as the body. On `comment-add` no other change is ever possible, so an absent `--body` always means "read standard input". Leading and trailing whitespace is trimmed before validation and before storage; interior line breaks are preserved, so a multi-line body survives intact. Two cases fail with exit code 2: standard input that is empty, whitespace only, or not connected when the body must come from it; and a `--body` whose value is empty, whitespace only, or missing (no following token, or the following token is itself a flag) — the command does not fall back to standard input in that case.

`--type` is validated, for presence and then for value, before the body is resolved, so a missing or invalid type fails immediately instead of leaving the command waiting on standard input for a body it would reject anyway.

**Output:** JSON object with the created comment ID

**Examples:**
```bash
rmp sprint comment-add -r project1 3 --type DECISION --body "Dropped the second migration from this sprint."
rmp sprint c-add -r project1 3 -y FINDING -b "The migration runs in 40 ms on a 20k-row database."
rmp sprint comment-add -r project1 3 --type PROGRESS < progress.txt
```

**Example output:**
```json
{"id": 4}
```

An absent `--type` is rejected (exit 2); a value outside the four sprint comment types is rejected (exit 6), and that includes the task-only values `HYPOTHESIS`, `TEST`, and `NOTE`. A body longer than 4096 characters, or one containing control characters, is rejected (exit 6). An unknown sprint id is rejected (exit 4).

---

### comment-list

Returns every comment of the given sprint, oldest first.

**Usage:** `rmp sprint comment-list [OPTIONS] <sprint-id>` or `rmp sprint c-ls [OPTIONS] <sprint-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `sprint-id` | Yes | Sprint ID whose comments are listed |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |
| `-y` | `--type` | enum | Optional filter: return only comments of this type. The value must be one of the four sprint comment types |

**Ordering:** `created_at` ascending, with the comment `id` ascending as the tie-breaker for comments created within the same millisecond.

**Result-set size:** unbounded. Every matching comment is returned; there is no `--limit` flag, no `--desc` flag, and no pagination.

**Output:** JSON array of Comment objects; `[]` when the sprint has no comments, or none of the requested type

**Examples:**
```bash
rmp sprint comment-list -r project1 3
rmp sprint comment-list -r project1 3 --type DECISION
rmp sprint c-ls -r project1 3 -y PROGRESS
```

**Example output:**
```json
[
  {
    "updated_at": null,
    "type": "DECISION",
    "body": "Dropped the second migration from this sprint.",
    "created_at": "2026-03-12T11:15:00.000Z",
    "id": 4,
    "sprint_id": 3
  }
]
```

Listing is a read: it writes no audit entry. An invalid `--type` value is rejected (exit 6), including a valid task-only value such as `NOTE`.

---

### comment-edit

Changes the type and/or the body of one existing sprint comment, identified by the comment's own id. At least one change is required.

**Usage:** `rmp sprint comment-edit [OPTIONS] <comment-id>` or `rmp sprint c-edit [OPTIONS] <comment-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `comment-id` | Yes | Comment ID, **not** the id of the sprint it belongs to |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |
| `-y` | `--type` | enum | New comment type; one of the four sprint comment types |
| `-b` | `--body` | string | New comment text, max 4096 chars. Read from standard input when this flag is absent **and** `--type` is absent too (see below) |

**Body from standard input:** standard input is read only when neither `--type` nor `--body` is given. When `--type` is present and `--body` is absent, only the type changes, standard input is not read, and a type-only edit therefore never blocks waiting for input. The trimming, empty-value, and missing-value rules are those of `comment-add`.

**Replacement semantics:** the edit replaces the stored body in place and stamps `updated_at` with the edit's timestamp, so a later listing shows that the comment was altered. The previous text is not retained anywhere and cannot be recovered; the audit log records that an edit happened, not what it replaced.

**No-op is not accepted.** Unlike `sprint update`, `comment-edit` requires at least one change and fails with exit code 2 when none is requested. A change is requested by a `--type` value, by a `--body` value, or by a body arriving on standard input, so the flagless form `comment-edit <comment-id> < revised.txt` is a valid edit and not a no-op.

**Output:** Empty on success (exit 0)

**Examples:**
```bash
rmp sprint comment-edit -r project1 4 --type UPDATE
rmp sprint comment-edit -r project1 4 --body "Dropped both migrations; they move to the next sprint."
rmp sprint comment-edit -r project1 4 < revised.txt
rmp sprint c-edit -r project1 4 -y PROGRESS -b "Six of the nine tasks are closed."
```

A comment id that does not exist among the sprint comments is rejected (exit 4), including an id that exists only among the task comments: the two id spaces are independent.

---

### comment-remove

Deletes one sprint comment, identified by the comment's own id. The row is removed outright; there is no soft delete and no recovery. The audit entry survives the row, so the sprint's history still records that a comment existed and was removed.

**Usage:** `rmp sprint comment-remove [OPTIONS] <comment-id>` or `rmp sprint c-rm [OPTIONS] <comment-id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `comment-id` | Yes | Comment ID, **not** the id of the sprint it belongs to |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Single-id command:** `comment-remove` takes exactly one comment id and accepts no comma-separated list. It takes no `-y`/`--type` and rejects the flag as unknown (exit 2).

**Output:** Empty on success (exit 0)

**Examples:**
```bash
rmp sprint comment-remove -r project1 4
rmp sprint c-rm -r project1 4
```

## Aliases

| Command | Alias |
|---------|-------|
| `sprint` | `s` |
| `list` | `ls` |
| `create` | `new` |
| `remove` | `rm` |
| `update` | `upd` |
| `add-tasks` | `add` |
| `remove-tasks` | `rm-tasks` |
| `move-tasks` | `mv-tasks` |
| `reorder` | `order` |
| `move-to` | `mvto` |
| `bottom` | `btm` |
| `comment-add` | `c-add` |
| `comment-list` | `c-ls` |
| `comment-edit` | `c-edit` |
| `comment-remove` | `c-rm` |

## Sprint Lifecycle

```
PENDING → OPEN → CLOSED
   ↑              ↓
   └──────────────┘ (reopen)
```

## Notes

- Sprints are created with `PENDING` status by default
- State transitions are validated (cannot close an already closed sprint)
- When removing a sprint, associated tasks return to `BACKLOG` status, which clears their lifecycle timestamps, their `completion_summary`, and their `commit_close`; `commit_open` is preserved
- When adding tasks to a sprint, the task status changes to `SPRINT`
- Task ordering commands maintain position consistency (0, 1, 2...n) automatically
- The `stats` command shows the current `task_order` array for reference
- Comments are strictly additive. They are accepted in every status, including `CLOSED`; no comment subcommand checks or changes a sprint's status, and no comment gates a transition
- `comment-add` and `comment-list` take the SPRINT's id; `comment-edit` and `comment-remove` take the COMMENT's own id
- Sprint and task comment ids are separate sequences, so `rmp sprint comment-edit 7` and `rmp task comment-edit 7` address two unrelated comments
- Comment operations are audited against the parent sprint, as `SPRINT_COMMENT_CREATE`, `SPRINT_COMMENT_UPDATE`, and `SPRINT_COMMENT_DELETE`; `comment-list` is a read and writes no audit entry
- Every command above that changes a sprint writes at least one audit entry; those whose entries name a counterpart task list them under **Audit** in the command's own section. Listing commands are reads and write none, and a rejected command writes none, because the entry is written in the same transaction as the change it records. The full operation catalogue, and the meaning of the `commit_hash` and `related_entity_id` fields an entry can carry, are in [DOCS/commands/audit.md](audit.md)

## Field Limits and Constraints

| Field | Required | Max Length / Range | Description |
|-------|----------|--------------------|-------------|
| `title` | Yes (on create) | 255 chars | Sprint title |
| `description` | Yes (on create) | 2048 chars | Sprint description: the high-level (macro) goal of the development effort the sprint delivers (macro goal only; detail belongs in the sprint's tasks) |
| `max-tasks` | No | 1-10000 | Capacity cap on active tasks; cannot be removed once set |
| `order` | No | positive integer (`> 0`), unique | Execution order across the roadmap; auto-assigned when omitted; immutable once the sprint is CLOSED |
| `type` (comment) | Yes (on `comment-add`) | one of 4 comment types | Comment classification; no default |
| `body` | Yes (on `comment-add`) | 4096 chars | Comment text; supplied through `--body` or on standard input |

### Sprint Status Values

- `PENDING` - Sprint created but not started
- `OPEN` - Sprint in progress
- `CLOSED` - Sprint finished

### Comment Type Values

Carried by `-y`/`--type` on `comment-add`, `comment-list`, and `comment-edit`. A sprint comment accepts four values:

- `FINDING` - Something discovered during the work: an observed behaviour, a measurement, a cause identified, a constraint that turned out to apply
- `DECISION` - A decision taken during the work, and the reasoning behind it
- `PROGRESS` - A statement of how the work advanced: what was done, what remains
- `UPDATE` - The reason behind a modification to the definition of the sprint: something added, updated, removed, complemented, or clarified

**Per-entity applicability.** The comment type enum is per-entity, and the two sets are not symmetric. A task comment accepts three further values — `HYPOTHESIS`, `TEST`, and `NOTE` — which are rejected on a sprint with exit code 6, because a sprint records how the sprint went and not the execution diary of its individual tasks. The reverse does not happen: every value valid on a sprint is also valid on a task. The full seven-value set is documented in [DOCS/commands/task.md](task.md).

### Comment Object Keys

Comment objects returned by `comment-list` contain:
`id`, `sprint_id`, `type`, `body`, `created_at`, `updated_at`.

`updated_at` is always present and is `null` until the comment is first edited, after which it carries the edit's timestamp. `body` preserves the author's interior line breaks as `\n` escapes in JSON. Sprint objects carry no `comments` array and no comment count: comments are read only through `comment-list` and the read-only web interface.

## Output Format

All commands follow these conventions:
- **Success**: JSON output to stdout, exit code 0. `create` and `comment-add` emit `{"id": <int>}`; read commands, `comment-list` included, emit a JSON array; mutating commands, `comment-edit` and `comment-remove` included, emit empty stdout
- **Errors**: Plain text to stderr, non-zero exit code

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error (database failure) |
| 2 | Misuse (missing required argument, bad syntax). On the comment subcommands it also covers a missing `--type` on `comment-add`, a body supplied by neither `--body` nor standard input, and a `comment-edit` that requests no change at all |
| 3 | No roadmap selected (`-r` missing) |
| 4 | Sprint or comment not found |
| 5 | `--order` value already used by another sprint (`create` / `update`) |
| 6 | Validation error: bad enum; `--max-tasks` outside 1-10000; closing while SPRINT/DOING/TESTING tasks remain without `--force`; opening while another sprint is OPEN; changing `--order` on a CLOSED sprint; a comment type outside the four sprint values; a comment body over 4096 characters or containing control characters |

The comment subcommands split the two failure kinds along a consistent line. A missing or unusable **body** is a misuse error (exit 2), because the command was invoked without the input it needs; an **oversized or control-character** body is a validation error (exit 6), because the input arrived and was rejected on its content.
