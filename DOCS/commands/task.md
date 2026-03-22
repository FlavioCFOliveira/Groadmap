# task

## Description

Task management within a roadmap. Tasks track work with status, priority, severity, and detailed descriptions.

## Synopsis

```
rmp task [subcommand] [arguments] [flags]
```

## Subcommands

### list

Lists tasks in the selected roadmap.

**Usage:** `rmp task list [OPTIONS]` or `rmp task ls [OPTIONS]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required if no default set) |
| `-s` | `--status` | string | - | Filter by status: BACKLOG, SPRINT, DOING, TESTING, COMPLETED |
| `-p` | `--priority` | int | - | Filter by minimum priority (0-9) |
| N/A | `--severity` | int | - | Filter by minimum severity (0-9) |
| `-l` | `--limit` | int | - | Limit number of results |

**Output:** JSON array of Task objects

**Examples:**
```bash
rmp task list -r project1
rmp task ls -r project1 -s DOING
rmp task ls -r project1 -p 5 -l 20
```

---

### create

Creates a new task in the specified roadmap.

**Usage:** `rmp task create [OPTIONS]` or `rmp task new [OPTIONS]`

**Required Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name |
| `-t` | `--title` | string | Task title/summary |
| `-fr` | `--functional-requirements` | string | Functional requirements (Why?) |
| `-tr` | `--technical-requirements` | string | Technical requirements (How?) |
| `-ac` | `--acceptance-criteria` | string | Acceptance criteria (How to verify?) |

**Optional Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-p` | `--priority` | int | 0 | Priority 0-9 |
| N/A | `--severity` | int | 0 | Severity 0-9 |
| `-sp` | `--specialists` | string | - | Comma-separated list of specialists |

**Output:** JSON object with the created task ID

**Examples:**
```bash
rmp task create -r project1 -t "Fix login bug" -fr "User can login" -tr "Update auth" -ac "Login works"
rmp task new -r project1 -t "Update docs" -fr "Docs needed" -tr "Write README" -ac "Docs complete" -p 5
```

**Example output:**
```json
{"id": 42}
```

---

### next

Retrieves the next N open tasks from the currently open sprint. Tasks are returned in the order defined by the sprint's `task_order` (set via sprint reorder commands), allowing the team to define execution sequence independent of priority/severity.

**Usage:** `rmp task next [num]`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `num` | No | Number of tasks to return (default: 1, max: 100) |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** JSON array of Task objects

**Examples:**
```bash
rmp task next -r project1        # Returns 1 task
rmp task next -r project1 5      # Returns up to 5 tasks
```

**Example output:**
```json
[
  {
    "id": 42,
    "title": "Implement user authentication",
    "functional_requirements": "Users must be able to authenticate securely",
    "technical_requirements": "Create login endpoint with JWT tokens",
    "acceptance_criteria": "Users can log in with valid credentials",
    "priority": 9,
    "severity": 9,
    "status": "SPRINT",
    "specialists": "backend,security",
    "created_at": "2026-03-15T10:30:00.000Z",
    "started_at": null,
    "tested_at": null,
    "closed_at": null
  }
]
```

**Error Cases:**
- Returns error if no sprint is currently open
- Returns empty array if sprint has no open tasks

---

### get

Gets detailed information about one or more tasks.

**Usage:** `rmp task get [OPTIONS] <ids>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `ids` | Yes | Task IDs separated by commas (no spaces) |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** JSON array of Task objects

**Examples:**
```bash
rmp task get -r project1 42
rmp task get -r project1 1,2,3,10
```

---

### set-status (stat)

Changes the status of one or more tasks.

**Usage:** `rmp task set-status [OPTIONS] <ids> <state>` or `rmp task stat [OPTIONS] <ids> <state>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `ids` | Yes | Task IDs separated by commas |
| `state` | Yes | New status: BACKLOG, SPRINT, DOING, TESTING, COMPLETED |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Status Flow:**
```
BACKLOG ↔ SPRINT ↔ DOING ↔ TESTING → COMPLETED
```

**Examples:**
```bash
rmp task set-status -r project1 42 DOING
rmp task stat -r project1 1,2,3 COMPLETED
```

---

### set-priority (prio)

Changes the priority of one or more tasks.

**Usage:** `rmp task set-priority [OPTIONS] <ids> <priority>` or `rmp task prio [OPTIONS] <ids> <priority>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `ids` | Yes | Task IDs separated by commas |
| `priority` | Yes | Priority value 0-9 |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Priority Scale:**
- 0 = low urgency
- 9 = maximum urgency (Product Owner perspective)

**Examples:**
```bash
rmp task set-priority -r project1 42 9
rmp task prio -r project1 1,2,3 5
```

---

### set-severity (sev)

Changes the severity of one or more tasks.

**Usage:** `rmp task set-severity [OPTIONS] <ids> <severity>` or `rmp task sev [OPTIONS] <ids> <severity>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `ids` | Yes | Task IDs separated by commas |
| `severity` | Yes | Severity value 0-9 |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Severity Scale:**
- 0 = minimal impact
- 9 = critical impact (Dev Team perspective)

**Examples:**
```bash
rmp task set-severity -r project1 42 5
rmp task sev -r project1 1,2,3 9
```

---

### edit

Edits an existing task's properties. Only specified fields are updated.

**Usage:** `rmp task edit [OPTIONS] <id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Task ID to edit |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |
| `-t` | `--title` | string | New title |
| `-fr` | `--functional-requirements` | string | New functional requirements |
| `-tr` | `--technical-requirements` | string | New technical requirements |
| `-ac` | `--acceptance-criteria` | string | New acceptance criteria |
| `-p` | `--priority` | int | New priority (0-9) |
| N/A | `--severity` | int | New severity (0-9) |
| `-sp` | `--specialists` | string | New specialists |

**Examples:**
```bash
rmp task edit -r project1 42 -t "New title" -p 7
rmp task edit -r project1 1 --specialists "go-developer"
```

---

### remove

Removes one or more tasks permanently. This action cannot be undone.

**Usage:** `rmp task remove [OPTIONS] <ids>` or `rmp task rm [OPTIONS] <ids>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `ids` | Yes | Task IDs separated by commas |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Examples:**
```bash
rmp task remove -r project1 42
rmp task rm -r project1 1,2,3
```

## Aliases

| Command | Alias |
|---------|-------|
| `task` | `t` |
| `list` | `ls` |
| `create` | `new` |
| `remove` | `rm` |
| `set-status` | `stat` |
| `set-priority` | `prio` |
| `set-severity` | `sev` |

## Notes

- Tasks are created with `BACKLOG` status by default
- Status transitions are validated according to the state machine (see SPEC/STATE_MACHINE.md)
- When transitioning to `DOING`, the `started_at` field is automatically set
- When transitioning to `TESTING`, the `tested_at` field is automatically set
- When transitioning to `COMPLETED`, the `closed_at` field is automatically set
- When reopening to `BACKLOG`, all tracking dates are cleared
- The `-r`/`--roadmap` flag can be omitted if a default roadmap has been set with `rmp roadmap use`

## Field Limits and Constraints

| Field | Required | Max Length | Description |
|-------|----------|------------|-------------|
| `title` | Yes | 255 chars | Task title/summary |
| `functional-requirements` | Yes | 4096 chars | Why: functional requirements |
| `technical-requirements` | Yes | 4096 chars | How: technical description |
| `acceptance-criteria` | Yes | 4096 chars | How to verify: completion criteria |
| `specialists` | No | 500 chars | Comma-separated specialist tags |
| `priority` | No | 0-9 | Priority level (default: 0) |
| `severity` | No | 0-9 | Severity level (default: 0) |

### Task Status Values

- `BACKLOG` - Task not yet in a sprint
- `SPRINT` - Task assigned to sprint
- `DOING` - Task in progress
- `TESTING` - Task being tested
- `COMPLETED` - Task finished

### Task Type Values

- `USER_STORY` - User story
- `TASK` - General task (default)
- `BUG` - Bug fix
- `SUB_TASK` - Sub-task
- `EPIC` - Epic (collection of tasks)
- `REFACTOR` - Code refactoring
- `CHORE` - Maintenance task
- `SPIKE` - Research/exploration
- `DESIGN_UX` - Design/UX work
- `IMPROVEMENT` - Enhancement

## Output Format

All commands follow these conventions:
- **Success**: JSON output to stdout, exit code 0
- **Errors**: Plain text to stderr, non-zero exit code

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | No roadmap selected |
| 4 | Resource not found |
| 5 | Resource already exists |
| 6 | Invalid data |
