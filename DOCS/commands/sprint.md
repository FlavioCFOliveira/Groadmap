# sprint

## Description

Sprint management within a roadmap. Sprints group tasks into time-boxed iterations with lifecycle management (PENDING → OPEN → CLOSED).

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
| `-d` | `--description` | string | - | Sprint description, max 2048 chars. **Required** on create |
| | `--order` | integer | auto | Execution order; positive integer (`> 0`), unique across sprints. When omitted, auto-assigned to `MAX(order) + 1` (first sprint is `1`) |
| | `--max-tasks` | integer | - | Capacity cap on active tasks (range 1-10000); cannot be removed once set |

**Output:** JSON object with the created sprint ID

**The `order` field** indicates the natural, sequential order in which sprints
must be executed. It must be a positive integer (`> 0`) and unique across all
sprints in the roadmap. A non-positive or non-integer value exits with code 6;
an order already used by another sprint exits with code 5.

**Examples:**
```bash
rmp sprint create -r project1 -t "Initial Setup" -d "Sprint 1 - Initial Setup"
rmp sprint new -r project1 -t "Features" -d "Sprint 2 - Features"
rmp sprint create -r project1 -t "Hardening" -d "Sprint 3 - Hardening" --order 3
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
  "sprint_title": "Initial Setup",
  "sprint_description": "Sprint 1 - Initial Setup",
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

**Examples:**
```bash
rmp sprint add-tasks -r project1 1 10,11,12
rmp sprint add -r project1 2 5,6,7,8
```

---

### remove-tasks

Removes tasks from a sprint. Tasks return to BACKLOG status.

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
| `-d` | `--description` | string | New description, max 2048 chars (optional) |
| | `--order` | integer | New execution order; positive and unique (optional) |
| | `--max-tasks` | integer | New capacity cap on active tasks (optional) |

**Order immutability:** the `--order` value may only be changed while the sprint
is `PENDING` or `OPEN`. Once a sprint is `CLOSED` its order is immutable and
attempting to change it exits with code 6, preserving the historical execution
record. (Reopening a sprint returns it to `OPEN`, making its order editable
again.) A non-positive value exits with code 6; an order already used by another
sprint exits with code 5.

**Examples:**
```bash
rmp sprint update -r project1 1 -t "Setup and Config"
rmp sprint update -r project1 1 -d "Sprint 1 - Setup and Config"
rmp sprint update -r project1 1 --order 2
rmp sprint upd -r project1 1 -t "Updated title" -d "Updated description"
```

---

### remove

Removes a sprint permanently. Member tasks are not deleted; their status reverts to `BACKLOG`.

**Usage:** `rmp sprint remove [OPTIONS] <id>` or `rmp sprint rm [OPTIONS] <id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Sprint ID to remove |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

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

## Sprint Lifecycle

```
PENDING → OPEN → CLOSED
   ↑              ↓
   └──────────────┘ (reopen)
```

## Notes

- Sprints are created with `PENDING` status by default
- State transitions are validated (cannot close an already closed sprint)
- When removing a sprint, associated tasks return to `BACKLOG` status
- When adding tasks to a sprint, the task status changes to `SPRINT`
- Task ordering commands maintain position consistency (0, 1, 2...n) automatically
- The `stats` command shows the current `task_order` array for reference

## Field Limits and Constraints

| Field | Required | Max Length / Range | Description |
|-------|----------|--------------------|-------------|
| `title` | Yes (on create) | 255 chars | Sprint title |
| `description` | Yes (on create) | 2048 chars | Sprint description |
| `max-tasks` | No | 1-10000 | Capacity cap on active tasks; cannot be removed once set |
| `order` | No | positive integer (`> 0`), unique | Execution order across the roadmap; auto-assigned when omitted; immutable once the sprint is CLOSED |

### Sprint Status Values

- `PENDING` - Sprint created but not started
- `OPEN` - Sprint in progress
- `CLOSED` - Sprint finished

## Output Format

All commands follow these conventions:
- **Success**: JSON output to stdout, exit code 0
- **Errors**: Plain text to stderr, non-zero exit code

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error (database failure) |
| 2 | Misuse (missing required argument, bad syntax) |
| 3 | No roadmap selected (`-r` missing) |
| 4 | Sprint not found |
| 5 | `--order` value already used by another sprint (`create` / `update`) |
| 6 | Validation error: bad enum; `--max-tasks` outside 1-10000; closing while SPRINT/DOING/TESTING tasks remain without `--force`; opening while another sprint is OPEN; changing `--order` on a CLOSED sprint |
