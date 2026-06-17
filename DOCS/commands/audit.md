# audit

## Description

View audit log and entity history. All changes to tasks and sprints are automatically logged for traceability.

## Synopsis

```
rmp audit <subcommand> -r <roadmap> [arguments] [flags]
```

The `-r`/`--roadmap` flag is required for every audit subcommand; there is no default or active roadmap.

## Subcommands

### list

Lists audit log entries with optional filters.

**Usage:** `rmp audit list -r <roadmap> [filters]` or `rmp audit ls -r <roadmap> [filters]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-o` | `--operation` | string | - | Filter by operation type (see Operation Types below) |
| `-e` | `--entity-type` | string | - | Filter by entity type: TASK, SPRINT |
| N/A | `--entity-id` | int | - | Filter by specific entity numeric id (positive integer, range 1-2147483647). A non-integer value is rejected by the flag parser as misuse (exit code 2); an out-of-range value fails validation (exit code 6) |
| N/A | `--since` | string | - | Lower bound on `performed_at`, inclusive (ISO 8601 UTC; RFC 3339 variants accepted) |
| N/A | `--until` | string | - | Upper bound on `performed_at`, inclusive (ISO 8601 UTC; RFC 3339 variants accepted) |
| `-l` | `--limit` | int | 100 | Maximum rows returned (range 1-500). A non-integer value is rejected as misuse (exit code 2); an out-of-range value fails validation (exit code 6) |

**Operation Types:**

**Task Operations:**
- `TASK_CREATE` - Task created
- `TASK_UPDATE` - Task updated
- `TASK_DELETE` - Task deleted
- `TASK_STATUS_CHANGE` - Task status changed
- `TASK_PRIORITY_CHANGE` - Task priority changed
- `TASK_SEVERITY_CHANGE` - Task severity changed
- `TASK_REOPEN` - Task reopened to BACKLOG
- `TASK_ASSIGN` - Specialist assigned to a task
- `TASK_UNASSIGN` - Specialist removed from a task
- `TASK_ADD_DEP` - Dependency edge added between tasks
- `TASK_REMOVE_DEP` - Dependency edge removed between tasks

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

**Output:** JSON array of audit-entry objects, newest first (`performed_at` DESC). Each object has keys `id`, `operation`, `entity_type`, `entity_id`, and `performed_at`.

**Examples:**
```bash
rmp audit list -r project1
rmp audit ls -r project1 -o TASK_STATUS_CHANGE
rmp audit ls -r project1 -e TASK --since 2026-03-01T00:00:00.000Z
rmp audit list -r project1 --since 2026-01-01 --until 2026-01-31 -l 500
```

---

### history

Shows complete history for a specific entity (task or sprint).

**Usage:** `rmp audit history -r <roadmap> <type> <id>` or `rmp audit hist -r <roadmap> <type> <id>`

Equivalent to `rmp audit list -r <roadmap> -e <type> --entity-id <id>` without pagination. The entity type and id are **positional arguments**, not flags; there is no `-e` flag on `history`.

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `type` | Yes | Entity type: TASK or SPRINT (see Entity Types below) |
| `id` | Yes | Entity ID (integer) |

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
rmp audit history -r project1 TASK 42
rmp audit hist -r project1 SPRINT 1
```

---

### stats

Shows audit statistics including operation counts and trends.

**Usage:** `rmp audit stats -r <roadmap> [--since <date>] [--until <date>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|------------|------|--------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| N/A | `--since` | string | - | Aggregation window start (ISO 8601 UTC; RFC 3339 variants accepted) |
| N/A | `--until` | string | - | Aggregation window end (ISO 8601 UTC; RFC 3339 variants accepted) |

**Output:** A single `AuditStats` JSON object with keys `total_entries`, `first_entry_at`, `last_entry_at`, `by_operation` (map of operation to count), and `by_entity_type` (map of entity type to count). On an empty result set (no matching entries), `first_entry_at` and `last_entry_at` are `null`.

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
| 1 | General error (database failure) |
| 2 | Misuse: non-integer `--limit` or `--entity-id` on `list`, or a non-integer positional `<entity-id>` on `history` (rejected by the parser) |
| 3 | No roadmap selected (`-r` missing/required) |
| 4 | Roadmap not found |
| 6 | Validation error: invalid operation, entity-type, or date format; `--limit` out of range 1-500; `--entity-id` out of range 1-2147483647 |
