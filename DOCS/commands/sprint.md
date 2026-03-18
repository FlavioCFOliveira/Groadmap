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
| `-r` | `--roadmap` | string | - | Roadmap name (required if no default set) |
| `-s` | `--status` | string | - | Filter by status: PENDING, OPEN, CLOSED |

**Output:** JSON array of Sprint objects

**Examples:**
```bash
rmp sprint list -r project1
rmp sprint ls -r project1 -s OPEN
```

---

### create

Creates a new sprint in the specified roadmap.

**Usage:** `rmp sprint create [OPTIONS]` or `rmp sprint new [OPTIONS]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-d` | `--description` | string | - | Sprint description (required) |

**Output:** JSON object with the created sprint ID

**Examples:**
```bash
rmp sprint create -r project1 -d "Sprint 1 - Initial Setup"
rmp sprint new -r project1 -d "Sprint 2 - Features"
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

**Output:** JSON Sprint object

**Example:**
```bash
rmp sprint get -r project1 1
```

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
| `-s` | `--status` | string | - | Filter by task status |

**Output:** JSON array of Task objects

**Examples:**
```bash
rmp sprint tasks -r project1 1
rmp sprint tasks -r project1 1 -s DOING
```

---

### stats

Shows statistics for a sprint including task counts by status.

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
  }
}
```

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

Closes a sprint, changing its status from OPEN to CLOSED.

**Usage:** `rmp sprint close [OPTIONS] <id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Sprint ID to close |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Example:**
```bash
rmp sprint close -r project1 1
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

Updates a sprint's description.

**Usage:** `rmp sprint update [OPTIONS] <id>` or `rmp sprint upd [OPTIONS] <id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Sprint ID |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |
| `-d` | `--description` | string | New description (required) |

**Examples:**
```bash
rmp sprint update -r project1 1 -d "Sprint 1 - Setup and Config"
rmp sprint upd -r project1 1 -d "Updated description"
```

---

### remove

Removes a sprint permanently. Tasks in the sprint are not deleted.

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
