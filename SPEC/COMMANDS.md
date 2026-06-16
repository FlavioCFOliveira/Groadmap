# CLI Commands

## Table of Contents

- [Naming Conventions](#naming-conventions)
- [Command Structure](#command-structure)
- [Error Handling](#error-handling)
- [Field Validation](#field-validation)
- [Global Commands](#global-commands)
- [AI Agent Contract](#ai-agent-contract)
- [Exit Codes](#exit-codes)
- [Roadmap Selection (Always Required)](#roadmap-selection-always-required)
- [Roadmap Management](#roadmap-management)
- [Task Management](#task-management)
- [Sprint Management](#sprint-management)
- [Audit Log Management](#audit-log-management)
- [Backlog Management](#backlog-management)
- [Statistics Command](#statistics-command)
- [Graph Management](#graph-management)
- [Web Interface](#web-interface)
- [Command Aliases Reference](#command-aliases-reference)

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
- **Exit code:** 6 for validation errors (see `ARCHITECTURE.md` — Exit Codes for canonical mapping)

### Control-Character Constraint (All Free-Text Fields)

All free-text fields — task `title`, `functional_requirements`,
`technical_requirements`, `acceptance_criteria`, `completion_summary`,
`specialists`, and sprint `description` — reject control characters. An input that
contains any of the following is rejected with exit code 6 before it is stored:

- ASCII control bytes below `0x20`, except TAB (`0x09`), LF (`0x0A`), and CR
  (`0x0D`), which are permitted.
- DEL (`0x7F`).
- Unicode bidirectional and format control code points `U+200E`, `U+200F`,
  `U+202A`-`U+202E`, `U+2066`-`U+2069`, and `U+FEFF`.

This guards against terminal escape-sequence injection (CWE-150) and Trojan Source
attacks (CVE-2021-42574). The canonical definition is in
`MODELS.md § Free-Text Control-Character Constraint`.

### Specialists List-Separator Constraint

The `specialists` field is a comma-separated list. The comma is reserved as the
list separator, so an individual specialist name MUST NOT contain a comma. An input
in which a single name contains a comma is rejected with exit code 6. The canonical
definition is in `MODELS.md § Specialists List-Separator Constraint`.

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

### AI Help

```bash
rmp --ai-help
rmp ai-help
```

**Description:** Emits a machine-readable JSON contract that fully describes the CLI surface (commands, subcommands, flags, exit codes, output shapes, enums, examples). The output is intended to be consumed by AI agents and other automated callers without recourse to any other documentation.

**Forms:**

| Invocation | Scope of the returned contract |
|------------|--------------------------------|
| `rmp --ai-help` | Whole CLI: every command and every subcommand. |
| `rmp ai-help` | Whole CLI: identical payload to `rmp --ai-help`. |
| `rmp <command> --ai-help` | One command and all of its subcommands. |
| `rmp <command> <subcommand> --ai-help` | One subcommand only. |

**Rules:**

- The flag `--ai-help` is a global flag. It is recognised at every level of the command tree and is parsed before any other validation runs (analogous to `--help`).
- The flag `--ai-help` has no short form.
- The command `rmp ai-help` is functionally equivalent to `rmp --ai-help`. It exists so the contract is discoverable through plain command listings and shell tab-completion.
- The command `ai-help` accepts no positional arguments and no flags other than `--help`. Any other argument produces an `Error: ` to stderr with exit code 2.
- When both `--ai-help` and any other action-bearing flag or argument are present, `--ai-help` wins: the contract is emitted and no other action is performed.
- `--ai-help` and `ai-help` ignore the `-r` / `--roadmap` flag; the contract is a static description of the CLI and does not touch any roadmap database.

**Output (stdout JSON):** the contract document defined in `DATA_FORMATS.md § AI Agent Contract`. The JSON is pretty-printed with two-space indentation, UTF-8, and includes a final newline.

**Exit codes:**

| Code | Cause |
|------|-------|
| 0 | Contract emitted successfully. |
| 2 | `ai-help` invoked with unexpected positional arguments or flags; `--ai-help` used with an unknown command or subcommand name preceding it. |

**Discoverability requirements:**

1. The first line of the plain-text output of `rmp --help` and of every family-level and subcommand-level `--help` is the banner:

   ```
   AI agents: run `rmp --ai-help` for a machine-readable command contract.
   ```

   The banner is followed by one blank line, then the existing help body. The banner is **not** printed by `rmp --version` / `rmp -v` (version output is parsed by scripts; extra lines would break automations) and is **not** printed by the AI contract emitters (`rmp --ai-help`, `rmp ai-help`, `rmp <command> --ai-help`, `rmp <command> <subcommand> --ai-help`), which emit JSON only.

2. Every error message emitted to stderr by the CLI ends with one blank line followed by the hint:

   ```
   AI agents: run `rmp --ai-help` for a machine-readable command contract.
   ```

   This rule applies uniformly to input errors (missing flags, unknown subcommands), validation errors, not-found errors, conflict errors, and database errors. The hint is one line, plain text, written to stderr, and does not change the exit code. The hint is not appended when the command itself is `rmp --ai-help`, `rmp ai-help`, `rmp <command> --ai-help`, or `rmp <command> <subcommand> --ai-help` (to avoid recursion in error paths of the contract emitter). The hint is also not appended when `AI_AGENT=1` is active for this invocation; in that case the env-var hint already occupies the top of stderr and the trailing hint is suppressed to avoid duplication (see rule 3 below).

3. When the environment variable `AI_AGENT` is set to the literal value `1`, every invocation of `rmp` writes the same hint line to stderr **before** any other output, regardless of whether the invocation succeeds or fails:

   ```
   AI agents: run `rmp --ai-help` for a machine-readable command contract.
   ```

   The hint:
   - Is the **first line** written to stderr, followed by exactly one blank line, followed by any remaining stderr content (an `Error:` line on failure, otherwise nothing).
   - Is written exactly once per invocation. When `AI_AGENT=1` is active and the invocation fails, the trailing error-path hint specified in rule 2 is suppressed so the agent observes the hint exactly once.
   - Does not change stdout in any way.
   - Does not change the exit code.
   - Is suppressed for the invocations `rmp --ai-help`, `rmp ai-help`, `rmp <command> --ai-help`, and `rmp <command> <subcommand> --ai-help` (the agent is already using the contract).
   - Any value of `AI_AGENT` other than the exact string `1` (including empty, `0`, `true`, `false`, or unset) disables the hint.

   The canonical specification of ordering and deduplication is in `HELP.md § AI_AGENT environment variable`.

---

## AI Agent Contract

The structure, fields, and example payload of the JSON document returned by `rmp --ai-help` are specified in `DATA_FORMATS.md § AI Agent Contract`. The contract is generated by the CLI at runtime from its internal command registry; the registry is the single source of truth from which both the human help text and the AI contract are derived. See `ARCHITECTURE.md § AI Agent Contract Generation`.

---

## Exit Codes

Groadmap follows standard Unix exit code conventions. Success results in exit code `0`. Errors use specific codes (1-127) and are documented in detail in [ARCHITECTURE.md](./ARCHITECTURE.md#exit-codes).

---

## Roadmap Selection (Always Required)

All commands that operate on a roadmap require the `-r <name>` or `--roadmap <name>` flag explicitly.

**There is no default roadmap mechanism.** Omitting the flag always produces an error:

```
Error: roadmap not specified. Use -r <name> or --roadmap <name>
```

This applies to every subcommand under `task`, `sprint`, `backlog`, `audit`, `stats`, and `graph`.

The `web` command is deliberately **not** in this list. `rmp web` operates across all roadmaps: the web interface lists every roadmap found under `~/.roadmaps/` and the user selects one in the browser, so `rmp web` does not require and does not accept the `-r` / `--roadmap` flag (see [Web Interface](#web-interface)).

```bash
# Always provide -r:
rmp task list -r myproject
rmp sprint create -r myproject -d "Sprint 1"
rmp stats -r myproject
```

The `-r` / `--roadmap` flag may appear anywhere among the arguments after the subcommand; the parser extracts it before processing the remaining flags.

---

## Roadmap Management

Command: `rmp roadmap` (alias: `rmp road`)

A roadmap is stored in its own home directory at `~/.roadmaps/<name>/`, with the SQLite database at `~/.roadmaps/<name>/project.db`. On every `rmp` invocation, a startup sweep automatically migrates any roadmap still in the legacy `~/.roadmaps/<name>.db` layout into the current layout before the command runs, so `roadmap list` and all other commands always observe the current layout. The sweep is specified in `ARCHITECTURE.md § Filesystem Layout Migration`.

### List Roadmaps

```bash
rmp roadmap list
rmp road ls
```

**Description:** Lists all existing roadmaps. Each roadmap is the immediate subdirectory of `~/.roadmaps/` that contains a `project.db` database.

**JSON Output:** Array of objects, each with `name` (the roadmap home directory name), `path` (the absolute path to the roadmap's `project.db`), and `size` (the size of `project.db` in bytes).
```json
[
  {"name": "project1", "path": "~/.roadmaps/project1/project.db", "size": 24576},
  {"name": "project2", "path": "~/.roadmaps/project2/project.db", "size": 8192}
]
```

### Create Roadmap

```bash
rmp roadmap create <name>
rmp road new <name>
```

**Description:** Creates a new roadmap. The command creates the roadmap home directory `~/.roadmaps/<name>/` (mode `0700`) and the SQLite database `~/.roadmaps/<name>/project.db` (mode `0600`) inside it.

`roadmap create` accepts no flags beyond `--help`. It does not provide a `--force` or overwrite option; the operation is intentionally non-destructive. To replace an existing roadmap, the caller MUST run `rmp roadmap remove <name>` first.

**Name Validation:**

| Rule | Value | Description |
|------|-------|-------------|
| Regex | `^[a-z0-9_-]+$` | Only lowercase letters, numbers, underscores, and hyphens |
| Maximum length | 50 characters | Ensures filesystem compatibility |
| Minimum length | 1 character | Name cannot be empty |

**Error Cases:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Invalid characters | 6 | "Error: Roadmap name must only contain lowercase letters, numbers, underscores, and hyphens" |
| Exceeds 50 characters | 6 | "Error: Roadmap name must not exceed 50 characters (got N)" |
| Roadmap already exists | 5 | "Error: Roadmap 'name' already exists. To replace it, run 'rmp roadmap remove <name>' first." |

**Output (success):** `{"name": "project1"}`, exit code 0.

### Remove Roadmap

```bash
rmp roadmap remove <name>
rmp road rm <name>
```

**Description:** Removes a roadmap by deleting its entire home directory `~/.roadmaps/<name>/` recursively. This removes the `project.db` database, its SQLite sidecars (`project.db-wal`, `project.db-shm`), and any other per-roadmap files the directory contains.

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
- `-l, --limit <n>` - Limit number of results (default: 100)
- `-y, --type <TYPE>` - Filter by task type. See `MODELS.md` — Task Type for the canonical list of 10 valid values.
- `-sp, --specialists <name>` - Filter by specialist name (partial match, case-insensitive)
- `--created-since <date>` - Return tasks created on or after this date (RFC3339 or YYYY-MM-DD)
- `--created-until <date>` - Return tasks created on or before this date (RFC3339 or YYYY-MM-DD)
- `--sort <field>` - Sort order: `priority` (default), `created`, `status`, `severity`

**Default Ordering:** Tasks are returned ordered by `priority DESC, created_at ASC`. Higher-priority tasks appear first; equal-priority tasks are ordered by creation date (oldest first).

**Sort Field Ordering:**
| `--sort` value | ORDER BY |
|----------------|----------|
| `priority` (default) | `priority DESC, created_at ASC` |
| `created` | `created_at ASC` |
| `status` | `status ASC, priority DESC, created_at ASC` |
| `severity` | `severity DESC, priority DESC, created_at ASC` |

**Error Conditions:**
| Input | Exit Code | stderr |
|-------|-----------|--------|
| Invalid `--type` value | 6 | `Error: invalid task type: "X"` |
| Invalid `--sort` value | 6 | `Error: --sort must be one of: priority, created, status, severity` |
| Invalid `--created-since` format | 6 | `Error: --created-since: invalid date "X"...` |
| Invalid `--created-until` format | 6 | `Error: --created-until: invalid date "X"...` |

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
- `-y, --type <type>` - Task type (default: `TASK`). See `MODELS.md` — Task Type for the canonical list of 10 valid values.
- `-p, --priority <0-9>` - Priority (default: 0)
- `--severity <0-9>` - Severity (default: 0)
- `-sp, --specialists <list>` - Comma-separated specialists
- `--parent <id>` - Parent task ID; creates this task as a sub-task of the given parent. The parent must exist.

**Validation Rules:**
| Field | Constraint | Error Message |
|-------|------------|---------------|
| `title` | Required, max 255 chars | "Title is required and must not exceed 255 characters" |
| `functional-requirements` | Required, max 4096 chars | "Functional requirements are required and must not exceed 4096 characters" |
| `technical-requirements` | Required, max 4096 chars | "Technical requirements are required and must not exceed 4096 characters" |
| `acceptance-criteria` | Required, max 4096 chars | "Acceptance criteria are required and must not exceed 4096 characters" |
| `type` | One of 10 valid values | "Error: invalid task type: <value>" | 6 |

**Output (success):** `{"id": 42}`, exit code 0.

**Error Output:** Validation errors written to stderr with exit code 6.

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

**Description:** Returns the next N open tasks from the currently open sprint. Tasks are returned in the order defined by the sprint's `task_order` (set via `sprint reorder` or other ordering commands). When two tasks share the same sprint position, `priority DESC` is used as a tiebreaker, ensuring higher-priority work surfaces first.

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
| No sprint is currently open | 6 | "No sprint is currently open. Use 'rmp sprint start' to open a sprint first." |
| Invalid num argument (not a positive integer) | 2 | "Invalid argument: num must be a positive integer" |
| Roadmap not specified | 3 | "Error: Roadmap not specified. Use -r <name> or --roadmap <name>" |

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
| Invalid status transition | 6 | **No changes made** | "Error: Invalid status transition from X to Y" |
| Target state is `SPRINT` | 6 | **No changes made** | "Error: status SPRINT can only be set automatically via 'sprint add-tasks'" |
| `--summary` used with non-COMPLETED state | 6 | **No changes made** | "Error: --summary flag is only allowed when transitioning to COMPLETED" |
| `--summary` exceeds 4096 characters | 6 | **No changes made** | "Error: Completion summary must not exceed 4096 characters (got N)" |

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
| Priority out of range (0-9) | 6 | "Error: Priority must be between 0 and 9" |

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
| Severity out of range (0-9) | 6 | "Error: Severity must be between 0 and 9" |

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
- `-y, --type <type>` - Task type. See `MODELS.md` — Task Type for the canonical list of 10 valid values.
- `-p, --priority <0-9>`
- `--severity <0-9>`
- `-sp, --specialists <list>`

**Validation Rules:**

When a field is specified, it is validated before updating:

| Field | Constraint | Error Message (stderr) | Exit Code |
|-------|------------|------------------------|-----------|
| `title` | Required, max 255 chars | "Error: Title is required and must not exceed 255 characters" | 6 |
| `title` | Empty string | "Error: Title cannot be empty" | 6 |
| `functional-requirements` | Required, max 4096 chars | "Error: Functional requirements are required and must not exceed 4096 characters" | 6 |
| `functional-requirements` | Empty string | "Error: Functional requirements cannot be empty" | 6 |
| `technical-requirements` | Required, max 4096 chars | "Error: Technical requirements are required and must not exceed 4096 characters" | 6 |
| `technical-requirements` | Empty string | "Error: Technical requirements cannot be empty" | 6 |
| `acceptance-criteria` | Required, max 4096 chars | "Error: Acceptance criteria are required and must not exceed 4096 characters" | 6 |
| `acceptance-criteria` | Empty string | "Error: Acceptance criteria cannot be empty" | 6 |
| `priority` | Range 0-9 | "Error: Priority must be between 0 and 9" | 6 |
| `severity` | Range 0-9 | "Error: Severity must be between 0 and 9" | 6 |
| `type` | One of 10 valid values | "Error: invalid task type: <value>" | 6 |

**Validation Behavior:**
- **Whitespace trimming:** Leading and trailing whitespace is trimmed before validation
- **Empty strings:** Setting a required field to empty string fails validation
- **Partial updates:** Only specified fields are validated and updated
- **Type validation:** Non-integer values for priority/severity fail with exit code 2 (malformed input)
- **No-op:** If no fields are specified, command succeeds with no changes (exit code 0)

**Output (success):** No output, exit code 0.

**Error Output:** Validation errors written to stderr with exit code 6.

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
| Task has subtasks | 6 | `Error: task #N cannot be deleted — it has N subtask(s); remove them first` |

---

### List Subtasks

```bash
rmp task subtasks -r <name> <id>
```

**Description:** Returns all direct subtasks of a given task, ordered by priority descending, then created_at ascending.

**Arguments:**
- `id` - Parent task ID (required, positive integer)

**JSON Output:** Array of Task objects. Empty array if the task has no subtasks.

**Error Conditions:**
| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Task not found | 4 | `Error: not found: task N` |
| Invalid ID format | 2 | `Error: invalid task ID: X` |

---

### Add Task Dependency

```bash
rmp task add-dep -r <name> <task-id> <dep-id>
```

**Description:** Marks `<task-id>` as depending on `<dep-id>`. The task cannot be marked COMPLETED until `<dep-id>` is COMPLETED. Circular dependencies are rejected.

**Arguments:**
- `task-id` - ID of the dependent task (required, positive integer)
- `dep-id` - ID of the task it depends on (required, positive integer)

**Constraints:**
- A task cannot depend on itself
- Circular dependency detection: if adding A→B would create a cycle (B already transitively depends on A), the operation is rejected
- Adding an already-existing dependency is a no-op (idempotent)

**JSON Output:** No stdout output on success, exit code 0.

**Error Conditions:**
| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Task not found | 4 | `Error: task #N not found: ...` |
| Self-dependency | 6 | `Error: task cannot depend on itself` |
| Circular dependency | 6 | `Error: adding dependency would create a circular dependency...` |
| Missing arguments | 2 | `Error: task ID and dependency ID required` |

---

### Remove Task Dependency

```bash
rmp task remove-dep -r <name> <task-id> <dep-id>
```

**Description:** Removes the dependency of `<task-id>` on `<dep-id>`.

**Arguments:**
- `task-id` - ID of the dependent task (required, positive integer)
- `dep-id` - ID of the task it depends on (required, positive integer)

**JSON Output:** No stdout output on success, exit code 0.

**Error Conditions:**
| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Dependency not found | 4 | `Error: dependency from task #N to task #N not found` |
| Missing arguments | 2 | `Error: task ID and dependency ID required` |

---

### List Task Blockers

```bash
rmp task blockers -r <name> <id>
```

**Description:** Returns tasks that are blocking `<id>` — tasks that `<id>` depends on and that are NOT yet COMPLETED.

**Arguments:**
- `id` - Task ID (required, positive integer)

**JSON Output:** Array of Task objects. Empty array if no blockers.

**Error Conditions:**
| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Task not found | 4 | `Error: not found: task N` |
| Invalid ID format | 2 | `Error: invalid task ID: X` |

---

### List Tasks Being Blocked

```bash
rmp task blocking -r <name> <id>
```

**Description:** Returns tasks that `<id>` is blocking — tasks that depend on `<id>`.

**Arguments:**
- `id` - Task ID (required, positive integer)

**JSON Output:** Array of Task objects. Empty array if this task is not blocking anything.

**Error Conditions:**
| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Task not found | 4 | `Error: not found: task N` |
| Invalid ID format | 2 | `Error: invalid task ID: X` |

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
rmp sprint create -r <name> -d "Description" [--max-tasks <n>]
rmp sprint new -r <name> -d "Description" [--max-tasks <n>]
```

**Options:**
- `-d, --description <text>` - Sprint description (required)
- `--max-tasks <n>` - Maximum number of tasks allowed in the sprint (optional; omit
  for unlimited capacity). When provided, MUST be a positive integer in the range
  `1`-`10000`. A value `< 1` or `> 10000`, or a non-integer value, is rejected with
  exit code 6.

**Bound Validation:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| `--max-tasks` `< 1` or `> 10000` | 6 | "Error: --max-tasks must be between 1 and 10000 (got N)" |
| `--max-tasks` non-integer | 6 | "Error: --max-tasks must be an integer between 1 and 10000" |

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

### List Incomplete Sprint Tasks (open-tasks)

```bash
rmp sprint open-tasks -r <name> <id> [--order-by-priority]
```

**Description:** Returns all tasks in a sprint that are not yet completed (status: SPRINT, DOING, or TESTING). Useful during stand-ups and sprint reviews to see remaining work without client-side filtering.

**Arguments:**
- `id` - Sprint identifier

**Options:**
- `--order-by-priority` - Order by priority DESC instead of sprint position

**Default Ordering:** Sprint position ASC (same as `sprint tasks`).

**JSON Output:** Array of Task objects with status SPRINT, DOING, or TESTING. Returns an empty array if the sprint has no open tasks.

**Error Conditions:**
| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Sprint not found | 4 | `Error: resource not found: sprint #N not found` |
| Missing sprint ID | 2 | `Error: required: sprint ID required` |

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
  "task_order": [5, 3, 8, 1, 9, 2, 7, 4, 6, 10],
  "velocity": 0.0,
  "days_elapsed": 5,
  "days_remaining": null,
  "burndown": [
    {"date": "2026-03-19", "tasks_remaining": 10},
    {"date": "2026-03-21", "tasks_remaining": 8},
    {"date": "2026-03-24", "tasks_remaining": 7}
  ]
}
```

**Fields:**
- `task_order` - Array of task IDs ordered by position (first to last)
- `velocity` - Tasks completed per day (CLOSED sprints only). Computed as `completed_task_count / max(1.0, (closed_at - started_at) in days)`, so the sprint duration in the denominator is floored at a minimum of 1 day and a sprint that starts and closes within the same day yields a velocity equal to its completed-task count rather than an inflated value. 0.0 for OPEN or PENDING sprints, and for CLOSED sprints with no completed tasks
- `days_elapsed` - Days since the sprint was started (OPEN sprints only). null for PENDING and CLOSED sprints, and for OPEN sprints with no started_at date
- `days_remaining` - Always null. Sprint model has no end_date field
- `burndown` - Array of daily snapshots `{date, tasks_remaining}` derived from task `closed_at` dates. Ordered by date ascending. Empty array when no tasks have been completed

**Burndown Computation:**
- Start with total_tasks as the initial remaining count on the sprint start date (or first completion date if no start date is set)
- Subtract completions per day based on task `closed_at` timestamps
- Only includes dates on which at least one task was completed

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
  "max_tasks": 25,
  "capacity_pct": 56.0,
  "current_load": 14,
  "task_order": [12, 7, 19, 3, 21, 8, 15, 2, 9, 11, 4, 17, 6, 1, 20, 5, 18, 10, 13, 16],
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
| `max_tasks` | integer or null | Sprint capacity cap (maximum number of tasks the sprint may hold), or null when no cap is set |
| `capacity_pct` | float or null | Percentage of capacity used, computed from `current_load` against `max_tasks`. null when no cap is set |
| `current_load` | integer | Number of non-COMPLETED tasks counting against the sprint capacity |
| `task_order` | array of integers | Task IDs in sprint position order (first to last) |
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
| Sprint not found | 4 | "Sprint not found" |
| Roadmap not specified | 3 | "Error: Roadmap not specified. Use -r <name> or --roadmap <name>" |

### Sprint Lifecycle

```bash
rmp sprint start -r <name> <id>
rmp sprint close -r <name> <id> [--force]
rmp sprint reopen -r <name> <id>
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--force` | (`sprint close` only) Close the sprint even if tasks are still in SPRINT, DOING, or TESTING status. A warning listing the incomplete tasks is printed to stderr. |

**Active-Task Safety Check (sprint close):**

`sprint close` queries for tasks with status `SPRINT`, `DOING`, or `TESTING` in the sprint before closing. If any exist and `--force` is not provided, the command returns exit code 6 with an error listing the task IDs and statuses. With `--force`, the sprint is closed and a warning is printed to stderr.

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

**Note:** The status SPRINT is automatically managed by sprint operations. Users MUST NOT manually set status to SPRINT using `task stat`; attempts to do so are rejected with exit code 6 and the error message `"Error: status SPRINT can only be set automatically via 'sprint add-tasks'"`. Manual status transitions follow: BACKLOG → SPRINT (automatic) → DOING → TESTING → COMPLETED (with `SPRINT → BACKLOG` and `COMPLETED → BACKLOG` also available — see `STATE_MACHINE.md` for the full set).

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

**JSON Output (success):** A JSON success object is written to stdout, exit code 0:

```json
{
  "success": true,
  "sprint_id": 1,
  "task_order": [5, 3, 1, 4, 2]
}
```

**Error Output:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 4 | "Sprint not found" |
| Task ID not in sprint | 6 | "Task ID N is not in sprint" |
| Duplicate task IDs | 6 | "Duplicate task ID: N" |
| Missing task IDs | 6 | "Task list incomplete: expected N tasks, got M" |
| Invalid task ID format | 2 | "Invalid task ID: X" |

#### Move Task to Position

```bash
rmp sprint move-to -r <name> <sprint-id> <task-id> <position>
rmp sprint mvto -r <name> <sprint-id> <task-id> <position>
```

**Description:** Moves a single task to a specific position, shifting other tasks accordingly.

**Arguments:**
- `sprint-id` - Sprint identifier
- `task-id` - Task to move
- `position` - Target position (0-based). Must be an integer between 0 and 2147483647 (MaxInt32) inclusive. If position >= task count, task is moved to the end.

**Behavior:**
- Moving UP: Tasks between new position and current position-1 shift down by 1
- Moving DOWN: Tasks between current position+1 and new position shift up by 1
- Moving to same position: No-op
- Moving to position >= task count: Task is placed at the end

**JSON Output (success):** A JSON success object is written to stdout, exit code 0. The `position` field reflects the requested position:

```json
{
  "success": true,
  "sprint_id": 1,
  "task_id": 5,
  "position": 3
}
```

**Error Output:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 4 | "Sprint not found" |
| Task not in sprint | 6 | "Task N is not in sprint" |
| Invalid position | 6 | "Position must be an integer between 0 and 2147483647" |

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

**JSON Output (success):** A JSON success object is written to stdout, exit code 0:

```json
{
  "success": true,
  "sprint_id": 1,
  "task_id_1": 5,
  "task_id_2": 3
}
```

**Error Output:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 4 | "Sprint not found" |
| Task not in sprint | 6 | "Task N is not in sprint" |
| Same task ID | 6 | "Cannot swap a task with itself" |

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
| Sprint not found | 4 | "Sprint not found" |
| Task not in sprint | 6 | "Task N is not in sprint" |

### Update Sprint

```bash
rmp sprint update -r <name> <id> [-d "New Description"] [--max-tasks <n>]
rmp sprint upd -r <name> <id> [-d "New Description"] [--max-tasks <n>]
```

**Options:**
- `-d, --description <text>` - New sprint description
- `--max-tasks <n>` - New capacity limit. MUST be a positive integer in the range
  `1`-`10000`. A value `< 1` or `> 10000`, or a non-integer value, is rejected with
  exit code 6.

At least one of `--description` or `--max-tasks` is required.

**Bound Validation:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| `--max-tasks` `< 1` or `> 10000` | 6 | "Error: --max-tasks must be between 1 and 10000 (got N)" |
| `--max-tasks` non-integer | 6 | "Error: --max-tasks must be an integer between 1 and 10000" |

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
| Roadmap not specified | 3 | "Error: Roadmap not specified. Use -r <name> or --roadmap <name>" |

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
- `--entity-id <id>` - Filter by specific entity ID. MUST be a positive integer in
  the range `1`-`2147483647` (`MaxInt32`). A value `< 1` or `> 2147483647`, or a
  non-integer value, is rejected with exit code 6.
- `--since <date>` - ISO 8601 date
- `--until <date>` - ISO 8601 date
- `-l, --limit <n>` - Limit the number of results. MUST be a positive integer in
  the range `1`-`500`. The maximum is the server-side cap `MaxAuditLimit` (500;
  see `DATABASE.md § Audit Result Limit`). A value `< 1` or `> 500`, or a
  non-integer value, is rejected with exit code 6.

**Bound Validation:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| `--limit` `< 1` or `> 500` | 6 | "Error: --limit must be between 1 and 500 (got N)" |
| `--limit` non-integer | 6 | "Error: --limit must be an integer between 1 and 500" |
| `--entity-id` `< 1` or `> 2147483647` | 6 | "Error: --entity-id must be between 1 and 2147483647 (got N)" |
| `--entity-id` non-integer | 6 | "Error: --entity-id must be an integer between 1 and 2147483647" |

**JSON Output:** Array of AuditEntry objects.

### Entity History

```bash
rmp audit history -r <name> -e <type> <id>
rmp audit hist -r <name> -e <type> <id>
```

**Description:** Shows all audit entries related to a specific task or sprint.

**Arguments:**
- `<id>` - Entity identifier. MUST be a positive integer in the range
  `1`-`2147483647` (`MaxInt32`). A value `< 1` or `> 2147483647`, or a non-integer
  value, is rejected with exit code 6.

**Bound Validation:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| `<id>` `< 1` or `> 2147483647` | 6 | "Error: entity ID must be between 1 and 2147483647 (got N)" |
| `<id>` non-integer | 6 | "Error: entity ID must be an integer between 1 and 2147483647" |

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
  "by_operation": {
    "TASK_CREATE": 15,
    "TASK_STATUS_CHANGE": 19,
    "SPRINT_ADD_TASK": 11
  },
  "by_entity_type": {
    "TASK": 36,
    "SPRINT": 18
  },
  "first_entry_at": "2026-06-03T09:15:46.656Z",
  "last_entry_at": "2026-06-03T09:15:47.522Z",
  "total_entries": 54
}
```

**Field Descriptions:**

| Field | Type | Description |
|-------|------|-------------|
| `by_operation` | map[string]int | Count of entries per operation type, keyed by the full operation name (for example `TASK_CREATE`, `TASK_STATUS_CHANGE`, `SPRINT_ADD_TASK`). See `DATABASE.md § audit Table` for the operation catalogue |
| `by_entity_type` | map[string]int | Count of entries per entity type (`TASK`, `SPRINT`) |
| `first_entry_at` | string (ISO 8601 UTC) or null | Timestamp of the oldest audit entry among the filtered entries, or null when no entries match |
| `last_entry_at` | string (ISO 8601 UTC) or null | Timestamp of the newest audit entry among the filtered entries, or null when no entries match |
| `total_entries` | int | Total number of audit log entries matching the filter criteria |

**Behavior:**
- The `--since` and `--until` filters are applied to the audit entries before aggregation; all counts and timestamps reflect only the entries that pass the filter
- The command does not echo the requested period back in the output; there is no `period` object
- `first_entry_at` and `last_entry_at` are the oldest and newest timestamps among the filtered entries, not the filter bounds
- Empty result set (no entries match the filter) returns: `{"by_operation": {}, "by_entity_type": {}, "first_entry_at": null, "last_entry_at": null, "total_entries": 0}`
- All timestamps are in ISO 8601 UTC format

---

## Backlog Management

Command: `rmp backlog` (alias: `bl`)

**Description:** Dedicated commands for managing and querying tasks in the backlog. All subcommands filter exclusively on tasks with `status == BACKLOG`.

### List Backlog Tasks

```bash
rmp backlog list -r <name> [OPTIONS]
rmp backlog ls -r <name> [OPTIONS]
```

**Description:** Returns all tasks with status `BACKLOG`, with optional filters and sorting.

**Options:**
- `-r, --roadmap <name>` - Roadmap name (required if no default)
- `-p, --priority <min>` - Filter by minimum priority value (inclusive)
- `-y, --type <type>` - Filter by task type. Valid values: `USER_STORY`, `TASK`, `BUG`, `SUB_TASK`, `EPIC`, `REFACTOR`, `CHORE`, `SPIKE`, `DESIGN_UX`, `IMPROVEMENT` (see `MODELS.md` — Task Type for the canonical enum)
- `--sort <field>` - Sort order: `priority` (default), `created`, `status`, `severity`
- `-l, --limit <n>` - Maximum number of tasks to return

**JSON Output:** Array of Task objects (same format as `rmp task list`).

**Examples:**
```bash
rmp backlog list -r groadmap
rmp backlog list --priority 7 -r groadmap
rmp backlog list --type BUG -r groadmap
rmp backlog list --sort priority -r groadmap
rmp backlog ls --limit 20 -r groadmap
```

### Show Next Backlog Tasks

```bash
rmp backlog show-next [count] -r <name>
```

**Description:** Returns the top N backlog tasks ordered by priority descending (highest priority first) for sprint planning purposes. This is a convenience shortcut equivalent to `backlog list --sort priority --limit <count>`.

**Arguments:**
- `count` - Number of tasks to return (default: 5, max: 100)

**Options:**
- `-r, --roadmap <name>` - Roadmap name (required if no default)

**JSON Output:** Array of Task objects ordered by priority descending.

**Examples:**
```bash
rmp backlog show-next -r groadmap
rmp backlog show-next 5 -r groadmap
rmp backlog show-next 10 -r groadmap
```

**Error Conditions:**

| Condition | Exit Code | Message |
|-----------|-----------|---------|
| Roadmap not found | 4 | `Error: roadmap "<name>" not found` |
| Invalid type value | 6 | `Error: invalid task type: <value>` |
| Invalid sort value | 6 | `Error: --sort must be one of: priority, created, status, severity` |
| Invalid count (show-next) | 6 | `Error: count must be a positive integer` |

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
    "completed": 9,
    "pending": 2
  },
  "tasks": {
    "backlog": 15,
    "sprint": 8,
    "doing": 5,
    "testing": 3,
    "completed": 42
  },
  "average_velocity": 2.5
}
```

**Output Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `roadmap` | string | Name of the roadmap |
| `sprints.current` | integer or null | ID of the currently open sprint, or null if no sprint is open |
| `sprints.total` | integer | Total number of sprints in the roadmap |
| `sprints.completed` | integer | Number of sprints with status CLOSED |
| `sprints.pending` | integer | Number of sprints with status PENDING (created but never started) |
| `tasks.backlog` | integer | Number of tasks with status BACKLOG |
| `tasks.sprint` | integer | Number of tasks with status SPRINT |
| `tasks.doing` | integer | Number of tasks with status DOING |
| `tasks.testing` | integer | Number of tasks with status TESTING |
| `tasks.completed` | integer | Number of tasks with status COMPLETED |
| `average_velocity` | float64 | Average tasks completed per day across the last 5 closed sprints. Each sprint's daily rate uses a sprint duration floored at a minimum of 1 day: `duration_days = max(1.0, (closed_at - started_at) in days)`. 0.0 when no qualifying closed sprints exist |

**Error Cases:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Roadmap not specified | 3 | "Error: no roadmap selected: no roadmap selected" |
| Roadmap not found | 4 | "Error: resource not found: roadmap 'name'" |

**Behavior Notes:**
- The `sprints.current` field returns the ID of the sprint with status OPEN, or `null` if no sprint is currently open
- The `sprints.pending` field counts sprints with status PENDING (created but never started)
- The `sprints.completed` field counts sprints with status CLOSED
- `sprints.total` may exceed `sprints.completed` plus `sprints.pending` because the currently open sprint is counted in `sprints.current` and `sprints.total` but not in `sprints.completed` or `sprints.pending`
- The sum of all task statuses equals the total number of tasks in the roadmap
- `average_velocity` is computed from the last 5 closed sprints that have both `started_at` and `closed_at` set. Each sprint's daily completion rate divides its completed-task count by `max(1.0, (closed_at - started_at) in days)`, so a sprint that starts and closes within the same day contributes its completed-task count rather than an inflated value. Sprints with zero completed tasks contribute 0.0. Returns 0.0 when no qualifying sprints exist

---

## Graph Management

Command: `rmp graph` (no alias)

The `graph` command operates a roadmap's knowledge graph: a free-form, queryable
store of the project's elements and the relationships between them, backed by the
GoGraph engine. The design, persistence layout, multi-layer conventions, and
guard-rail rules are specified in `GRAPH.md`. This section is the CLI contract
for the command.

Each roadmap owns one graph, stored under that roadmap's home directory at
`~/.roadmaps/<name>/graph/` (a directory, mode `0700`). The graph is created on
first use of any `graph` subcommand. The graph is independent of the roadmap's
SQLite tasks and sprints data in this version.

`graph` has five subcommands, each a guard rail that accepts only Cypher whose
operation class matches the subcommand and rejects everything else before
execution:

| Subcommand | Operation | Accepts | Rejects |
|------------|-----------|---------|---------|
| `create` | Create nodes/edges | Writing query whose only writing clauses are `CREATE` and/or `MERGE` | Read-only queries; `SET`, `REMOVE`, `DELETE`, `DETACH DELETE` |
| `query` | Read | Read-only query (`MATCH ... RETURN`, no writing clause) | Any writing clause |
| `update` | Mutate existing | Writing query whose writing clauses are `SET` and/or `REMOVE` | Read-only queries; `CREATE`, `MERGE`, `DELETE`, `DETACH DELETE` |
| `delete` | Remove | Writing query whose writing clauses are `DELETE` and/or `DETACH DELETE` | Read-only queries; `CREATE`, `MERGE`, `SET`, `REMOVE` |
| `search` | Read (traversal) | Read-only query, including variable-length paths (e.g. `-[*1..3]-`) | Any writing clause |

The canonical operation-class definitions and the full per-subcommand rules are
in `GRAPH.md § Subcommands and Guard-Rail Validation`.

### Shared Options (all graph subcommands)

- `-r, --roadmap <name>` - REQUIRED. Target roadmap (see
  `COMMANDS.md § Roadmap Selection (Always Required)`).
- `--query <cypher>` - The Cypher query to run. When omitted, the query is read
  in full from standard input.
- `-h, --help` - Show the subcommand help.

**Query input source and precedence** (specified in
`GRAPH.md § Cypher Input Source and Precedence`):

1. When `--query` is present and non-empty, its value is used and standard input
   is not read.
2. When `--query` is absent, the entire standard input is read and used as the
   query.
3. When `--query` is absent and standard input is empty or not connected, the
   command fails with exit code 2 (no query supplied).
4. When `--query` is present but empty or whitespace only, the command fails with
   exit code 2.
5. Leading and trailing whitespace is trimmed before validation and execution.

### Output

- Read subcommands (`query`, `search`) on success: JSON to stdout in the shape
  defined in `DATA_FORMATS.md § Graph Query Result` (a `columns` array and a
  `rows` array). Exit code 0.
- Write subcommands (`create`, `update`, `delete`) on success: the output
  mirrors the query's `RETURN` clause. When the query has a `RETURN` clause, the
  output is the same `{columns, rows}` shape as a read result; when it has no
  `RETURN` clause, the output is exactly `{"ok": true}`. There is no
  affected-element count, because the engine reports none. Exit code 0. The shape
  is fixed in `DATA_FORMATS.md § Graph Write Result`.
- Side effect of a successful write: after committing, a write subcommand
  produces an on-disk snapshot under `~/.roadmaps/<name>/graph/snapshot/` and
  truncates the write-ahead log, synchronously, before exit (see
  `GRAPH.md § Synchronous Checkpoint on Write`). A snapshot failure after a
  durable commit does not change the success output or the exit code; it is
  reported as a diagnostic on stderr while the command still exits 0.
- Query notifications: the subcommand surfaces, as a plain-text diagnostic line
  per notification on stderr, exactly the advisory notifications the engine
  returns for the executed query (for example a Cartesian-product warning on a
  disconnected multi-pattern `MATCH`). The surfacing is wired identically on the
  read and the write path; the engine alone decides which queries and paths carry
  notifications, so a query may produce none. Notifications do not change the
  stdout success output or the exit code, and when the engine returns none the
  subcommand writes nothing extra to stderr (see
  `GRAPH.md § Query Notifications as Diagnostics`).
- Errors: plain text to stderr, with the standard AI-agent hint.

### Exit Codes

| Exit Code | Cause |
|-----------|-------|
| 0 | Query executed successfully. |
| 1 | Cypher failed to parse or execute, or the graph store could not be opened, read, or written (`utils.ErrDatabase`). |
| 2 | No query supplied: `--query` absent and stdin empty, or `--query` empty/whitespace (`utils.ErrRequired`). |
| 3 | No roadmap selected and none provided via `-r` (`utils.ErrNoRoadmap`). |
| 4 | Selected roadmap does not exist (`utils.ErrNotFound`). |
| 6 | The query's operation class does not match the subcommand (`utils.ErrValidation`). |

The canonical exit-code catalogue is in `ARCHITECTURE.md § Exit Codes`; the graph
feature introduces no new codes.

### Create

```bash
rmp graph create -r <name> --query "<cypher>"
echo "<cypher>" | rmp graph create -r <name>
```

**Description:** Adds nodes and/or edges to the graph. Accepts only Cypher whose
writing clauses are `CREATE` and/or `MERGE`. Runs as a single transaction.

**Example:**

```bash
rmp graph create -r backend-platform \
  --query "MERGE (s:Spec {key:'user-authentication'}) MERGE (c:Code {path:'internal/auth/jwt.go'}) MERGE (s)-[:IMPLEMENTED_BY]->(c)"
```

Output (success): `{"ok": true}`, exit code 0. The query has no `RETURN` clause,
so the output is the `{"ok": true}` object. Appending `RETURN` to the query (for
example `... RETURN s`) returns the created elements in the `{columns, rows}`
shape instead (see `DATA_FORMATS.md § Graph Write Result`).

### Query

```bash
rmp graph query -r <name> --query "<cypher>"
cat query.cypher | rmp graph query -r <name>
```

**Description:** Reads from the graph and returns the result columns and rows.
Read-only: rejects any query containing a writing clause.

**Example:**

```bash
rmp graph query -r backend-platform \
  --query "MATCH (s:Spec)-[:IMPLEMENTED_BY]->(c:Code) RETURN s.key, c.path"
```

Output (success): JSON in the shape defined in
`DATA_FORMATS.md § Graph Query Result`, for example:

```json
{
  "columns": ["s.key", "c.path"],
  "rows": [
    ["user-authentication", "internal/auth/jwt.go"]
  ]
}
```

### Update

```bash
rmp graph update -r <name> --query "<cypher>"
```

**Description:** Mutates properties or labels on existing graph elements. Accepts
only Cypher whose writing clauses are `SET` and/or `REMOVE`. Runs as a single
transaction.

**Example:**

```bash
rmp graph update -r backend-platform \
  --query "MATCH (s:Spec {key:'user-authentication'}) SET s.status = 'implemented'"
```

Output (success): `{"ok": true}`, exit code 0.

### Delete

```bash
rmp graph delete -r <name> --query "<cypher>"
```

**Description:** Removes nodes and/or edges. Accepts only Cypher whose writing
clauses are `DELETE` and/or `DETACH DELETE`. Runs as a single transaction.

**Example:**

```bash
rmp graph delete -r backend-platform \
  --query "MATCH (d:Decision {key:'use-sessions'}) DETACH DELETE d"
```

Output (success): `{"ok": true}`, exit code 0.

### Search

```bash
rmp graph search -r <name> --query "<cypher>"
```

**Description:** Read-only traversal and pattern matching, including
variable-length paths. Semantically the traversal-oriented sibling of `query`;
it enforces the same read-only guard rail.

**Example:**

```bash
rmp graph search -r backend-platform \
  --query "MATCH path = (s:Spec {key:'user-authentication'})-[:DEPENDS_ON*1..3]->(d:Dependency) RETURN path"
```

Output (success): JSON in the shape defined in
`DATA_FORMATS.md § Graph Query Result`, exit code 0.

### Error Cases (all graph subcommands)

| Scenario | Exit Code | stderr Output (illustrative) |
|----------|-----------|------------------------------|
| Roadmap not specified | 3 | "Error: no roadmap selected: use -r <name> or --roadmap <name>" |
| Roadmap not found | 4 | "Error: resource not found: roadmap 'name'" |
| No query supplied | 2 | "Error: required parameter missing: --query (or pipe a query on stdin)" |
| Operation-class mismatch | 6 | "Error: graph create accepts only CREATE/MERGE queries" |
| Cypher parse/execution error | 1 | "Error: graph query failed: <engine diagnostic>" |
| Graph store open/read/write failure | 1 | "Error: graph store unavailable: <detail>" |

---

## Web Interface

Command: `rmp web` (no alias)

The `web` command starts a read-only, browser-based view of the data the CLI
manages. It runs an HTTP server embedded in the `rmp` binary (Go standard-library
`net/http`) that serves server-rendered HTML and embedded static assets, and it
reads the same on-disk data under `~/.roadmaps/` that the CLI reads. The interface
never writes; the CLI remains the sole write path. The full behaviour of the
running server — routes, pages, the read-only data flow, the interactive
knowledge-graph visualisation, and the security model — is specified in `WEB.md`.
This section is the command-line contract.

`rmp web` operates across all roadmaps. The web interface lists every roadmap
found under `~/.roadmaps/` and the user drills into one from the browser, so
`rmp web` does **not** require and does **not** accept the `-r` / `--roadmap`
flag (see [Roadmap Selection (Always Required)](#roadmap-selection-always-required)).

`rmp web` has no subcommands.

```bash
rmp web
rmp web --port 9000
rmp web --host 127.0.0.1 --port 9000
rmp web --no-open
```

### Options

- `--host <address>` - Bind host. Default `127.0.0.1` (loopback only), so the
  read-only interface is reachable only from the local machine. Exposing the
  interface on the network is the explicit opt-in `--host 0.0.0.0` (binds all
  interfaces), or any other non-loopback address. When a non-loopback host is
  bound, the server prints a warning to stderr that the interface is reachable
  from the network (see `WEB.md § Bind Address and Port Selection` and
  `WEB.md § Security and Constraints`).
- `--port <number>` - Bind port, an integer in the range 0-65535. Default `8787`.
  When `--port` is omitted and the default port `8787` is already in use, the
  server falls back to an operating-system-chosen ephemeral port so it still
  starts. When `--port` is given explicitly, there is no fallback: a port that
  cannot be bound is a bind error. `--port 0` requests an ephemeral port
  explicitly. The chosen port is reported in the served URL.
- `--no-open` - Do not launch a browser. The server still starts and prints the
  served URL. Default behaviour (without this flag) is to open the user's default
  browser at the served URL; a failed browser launch is not fatal.
- `-h, --help` - Show the command help.

`rmp web` accepts no positional arguments. An unexpected positional argument or an
unknown flag is an input error (exit code 2).

### Output

- **On successful startup (stdout):** a single JSON object naming the URL the
  server is listening on, so the address is machine-readable even when no browser
  is opened:

  ```json
  {"url": "http://127.0.0.1:8787"}
  ```

  The `url` reflects the actual bound host and port, including an ephemeral port
  chosen by the fallback. The object is pretty-printed with two-space indentation
  and a trailing newline, consistent with all other JSON output (see
  `DATA_FORMATS.md § Implementation Notes`).
- **While running:** the server serves HTML pages and a JSON graph data endpoint
  per `WEB.md § Routes and Pages`. Per-request responses are HTTP responses from
  the server, not stdout output of the command.
- **Errors (stderr):** plain text, with the standard AI-agent hint, per
  `HELP.md § Error message format`.

### Lifecycle

`rmp web` is long-lived: it serves until interrupted. It is the only `rmp` command
whose process keeps running rather than completing a single operation and exiting.
Sending `SIGINT` (`Ctrl+C`) or `SIGTERM` shuts the server down gracefully and the
process exits 0 (see `WEB.md § Server Lifecycle`).

### Exit Codes

These are the exit codes of the `rmp web` **process**. They are distinct from the
per-request HTTP status codes the running server returns (200, 404, 405, 500),
which are specified in `WEB.md § Routes and Pages`.

| Exit Code | Cause |
|-----------|-------|
| 0 | Server started and was later stopped by `SIGINT`/`SIGTERM` (graceful shutdown). |
| 1 | Requested host/port could not be bound (port in use with an explicit `--port`, or host not assignable), or the data directory could not be read (`utils.ErrDatabase`). |
| 2 | Unknown flag or unexpected positional argument (`utils.ErrInvalidInput`). |
| 6 | `--port` value out of range 0-65535 or non-integer (`utils.ErrValidation`). |

The canonical exit-code catalogue is in `ARCHITECTURE.md § Exit Codes`; the web
interface introduces no new codes.

### Error Cases

| Scenario | Exit Code | stderr Output (illustrative) |
|----------|-----------|------------------------------|
| Explicit `--port` already in use | 1 | "Error: cannot bind 127.0.0.1:8787: address already in use" |
| Host not assignable | 1 | "Error: cannot bind 10.0.0.5:8787: cannot assign requested address" |
| `--port` out of range | 6 | "Error: --port must be an integer between 0 and 65535 (got 70000)" |
| `--port` not an integer | 6 | "Error: --port must be an integer between 0 and 65535 (got \"notanumber\")" |
| Unknown flag | 2 | "Error: unknown flag: --foo" |
| Data directory unreadable | 1 | "Error: cannot read data directory ~/.roadmaps: <detail>" |

---

## Command Aliases Reference

| Command | Aliases |
|---------|---------|
| `ai-help` | - |
| `roadmap` | `road` |
| `task` | `t` |
| `sprint` | `s` |
| `audit` | `aud` |
| `stats` | - |
| `graph` | - |
| `web` | - |
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
