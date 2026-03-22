# audit

## Description

View audit log and entity history. All changes to tasks and sprints are automatically logged for traceability.

## Synopsis

```
rmp audit [subcommand] [arguments] [flags]
```

## Subcommands

### list

Lists audit log entries with optional filters.

**Usage:** `rmp audit list [OPTIONS]` or `rmp audit ls [OPTIONS]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-o` | `--operation` | string | - | Filter by operation type |
| `-e` | `--entity-type` | string | - | Filter by entity type: TASK, SPRINT |
| N/A | `--entity-id` | int | - | Filter by specific entity ID |
| N/A | `--since` | string | - | Include entries from this date (ISO 8601) |
| N/A | `--until` | string | - | Include entries until this date (ISO 8601) |
| `-l` | `--limit` | int | 100 | Limit number of results |

**Operation Types:**

**Task Operations:**
- `TASK_CREATE` - Task created
- `TASK_UPDATE` - Task updated
- `TASK_DELETE` - Task deleted
- `TASK_STATUS_CHANGE` - Task status changed
- `TASK_PRIORITY_CHANGE` - Task priority changed
- `TASK_SEVERITY_CHANGE` - Task severity changed
- `TASK_TYPE_CHANGE` - Task type changed

**Sprint Operations:**
- `SPRINT_CREATE` - Sprint created
- `SPRINT_UPDATE` - Sprint updated
- `SPRINT_DELETE` - Sprint deleted
- `SPRINT_START` - Sprint started
- `SPRINT_CLOSE` - Sprint closed
- `SPRINT_REOPEN` - Sprint reopened
- `SPRINT_ADD_TASK` - Task added to sprint
- `SPRINT_REMOVE_TASK` - Task removed from sprint
- `SPRINT_MOVE_TASK` - Task moved between sprints

**Sprint Task Ordering Operations:**
- `SPRINT_REORDER_TASKS` - Tasks reordered in sprint
- `SPRINT_TASK_MOVE_POSITION` - Task moved to specific position
- `SPRINT_TASK_SWAP` - Tasks swapped positions

**Output:** JSON array of audit entries

**Examples:**
```bash
rmp audit list -r project1
rmp audit ls -r project1 -o TASK_STATUS_CHANGE
rmp audit ls -r project1 -e TASK --since 2026-03-01T00:00:00.000Z
```

---

### history

Shows complete history for a specific entity (task or sprint).

**Usage:** `rmp audit history [OPTIONS] <type> <id>` or `rmp audit hist [OPTIONS] <type> <id>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `type` | Yes | Entity type (see Entity Types below) |
| `id` | Yes | Entity ID |

**Entity Types:**
- `TASK` - Tasks in the roadmap
- `SPRINT` - Sprints in the roadmap

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Output:** JSON array of audit entries for the entity

**Examples:**
```bash
rmp audit history -r project1 -e TASK 42
rmp audit hist -r project1 -e SPRINT 1
```

---

### stats

Shows audit statistics including operation counts and trends.

**Usage:** `rmp audit stats [OPTIONS]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| N/A | `--since` | string | - | Include entries from this date (ISO 8601) |
| N/A | `--until` | string | - | Include entries until this date (ISO 8601) |

**Output:** JSON statistics object

**Examples:**
```bash
rmp audit stats -r project1
rmp audit stats -r project1 --since 2026-03-01T00:00:00.000Z
```

## Aliases

| Command | Alias |
|---------|-------|
| `audit` | `aud` |
| `list` | `ls` |
| `history` | `hist` |

## Notes

- All create, update, and delete operations are automatically logged
- The audit log is stored in the `audit` table of the SQLite database
- Each audit entry includes: operation, entity type, entity ID, timestamp
- History allows tracking all changes made to a specific task or sprint

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
