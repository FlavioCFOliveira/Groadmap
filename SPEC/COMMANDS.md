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
rmp task create --roadmap <name> --description <desc> --action <a> --expected-result <e> [OPTIONS]
rmp task new -r <name> -d <desc> -a <a> -e <e>
```

**Options:**
- `-p, --priority <0-9>` - Priority (default: 0)
- `--severity <0-9>` - Severity (default: 0)
- `-sp, --specialists <list>` - Comma-separated specialists

**Output (success):** `{"id": 42}`, exit code 0.

### Get Task(s)

```bash
rmp task get --roadmap <name> <id1,id2>
```

**Description:** Retrieves one or more tasks by ID. Multiple IDs must be comma-separated without spaces.

**JSON Output:** Array of Task objects.

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
- `-d, --description <text>`
- `-a, --action <text>`
- `-e, --expected-result <text>`
- `-p, --priority <0-9>`
- `--severity <0-9>`
- `-sp, --specialists <list>`

**Output (success):** No output, exit code 0.

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
