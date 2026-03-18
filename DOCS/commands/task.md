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
| `-d` | `--description` | string | Task description |
| `-a` | `--action` | string | Technical action to perform |
| `-e` | `--expected-result` | string | Expected outcome |

**Optional Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-p` | `--priority` | int | 0 | Priority 0-9 |
| N/A | `--severity` | int | 0 | Severity 0-9 |
| `-sp` | `--specialists` | string | - | Comma-separated list of specialists |

**Output:** JSON object with the created task ID

**Examples:**
```bash
rmp task create -r project1 -d "Fix login bug" -a "Debug auth" -e "Login works"
rmp task new -r project1 -d "Update docs" -a "Write README" -e "Docs complete" -p 5
```

**Example output:**
```json
{"id": 42}
```

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
| `-d` | `--description` | string | New description |
| `-a` | `--action` | string | New action |
| `-e` | `--expected-result` | string | New expected result |
| `-p` | `--priority` | int | New priority (0-9) |
| N/A | `--severity` | int | New severity (0-9) |
| `-sp` | `--specialists` | string | New specialists |

**Examples:**
```bash
rmp task edit -r project1 42 -d "New description" -p 7
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
- Status transitions are validated (cannot go from `COMPLETED` to `BACKLOG` directly)
- When a task is marked as `COMPLETED`, the `completed_at` field is automatically populated
- The `-r`/`--roadmap` flag can be omitted if a default roadmap has been set with `rmp roadmap use`
