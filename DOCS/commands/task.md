# task

## Description

Task management within a roadmap. Tasks track work with status, type, priority, severity, dependencies, detailed requirements, and an append-oriented comment log that records what was learned while the work was done. Every `task` subcommand operates on a single roadmap, which MUST be selected with the required `-r`/`--roadmap` flag.

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

**Output:** Empty on success (exit 0).

**Examples:**
```bash
rmp task edit -r project1 42 -t "Updated title" -p 8
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

**Usage:** `rmp task stat -r <roadmap> <task-ids> <new-status> [--commit-open <hash>] [--commit-close <hash>] [--summary <text>]` or `rmp task set-status ...`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-ids` | Yes | Comma-separated integer ids |
| `new-status` | Yes | Target status: BACKLOG, DOING, TESTING, COMPLETED (SPRINT is rejected) |

**Flags:**
| Short Flag | Long Flag | Type | Max Length | Description |
|------------|-----------|------|------------|-------------|
| `-r` | `--roadmap` | string | 50 | Roadmap name (required) |
| `-co` | `--commit-open` | string | 64 | Git commit hash the work starts from. **Mandatory** when the target status is `DOING`, rejected on every other target status |
| `-cc` | `--commit-close` | string | 64 | Git commit hash the work is concluded at. **Mandatory** when the target status is `COMPLETED`, rejected on every other target status |
| `-s` | `--summary` | string | 4096 | Completion summary; valid only when the target status is COMPLETED |

**Status Flow:**
```
BACKLOG --[sprint add-tasks]--> SPRINT --[stat DOING --commit-open]--> DOING --[stat TESTING]--> TESTING --[stat COMPLETED --commit-close]--> COMPLETED
COMPLETED --[reopen / stat BACKLOG]--> BACKLOG
```

**Rules:**
- `stat <ids> SPRINT` is rejected (exit 6). Use `sprint add-tasks` instead; SPRINT is only set automatically.
- Marking COMPLETED is rejected (exit 6) if any subtask or dependency is not yet COMPLETED.
- The `--summary` text is recorded as `completion_summary` and is only accepted on the TESTING -> COMPLETED transition.

**Commit Tracking Rules:**
- **`--commit-open` is mandatory on every transition into `DOING`.** Both such transitions require it: the first entry from `SPRINT`, and the re-entry from `TESTING` after testing sent the work back. On a re-entry the supplied value **replaces** the value stored before; no history of earlier values is kept.
- **`--commit-close` is mandatory on the `TESTING -> COMPLETED` transition**, the only transition into `COMPLETED`.
- **Each flag is rejected on any other target status** (exit 6, no changes made), mirroring the rule that governs `--summary`.
- **Format.** A commit hash is 7 to 64 hexadecimal characters and is stored lowercase, so `5F93B51` and `5f93b51` are the same value. Groadmap validates the format alone: it invokes no git command, reads no working directory, and does not check that the hash names a commit that exists.
- **One hash applies to the whole batch.** Every task named in `<task-ids>` receives the same value, exactly as every task receives the same `--summary`. A caller who needs different hashes issues separate commands.
- **Neither field is editable.** `task create` accepts neither flag, because a task is created in `BACKLOG`, and `task edit` cannot change either value. A wrong hash is corrected by performing the transition again where the state machine allows it.
- **`commit_open` survives a return to `BACKLOG`; `commit_close` does not.** All four routes back to `BACKLOG` — `stat BACKLOG`, `reopen`, `sprint remove-tasks`, and `sprint remove` — clear `commit_close` and leave `commit_open` untouched: reopening invalidates where the work ended, not where it began.

**Commit Flag Errors:**
| Condition | Exit Code | stderr |
|-----------|-----------|--------|
| `--commit-open` given and the target status is not `DOING` | 6 | `Error: --commit-open flag is only allowed when transitioning to DOING` |
| `--commit-close` given and the target status is not `COMPLETED` | 6 | `Error: --commit-close flag is only allowed when transitioning to COMPLETED` |
| Target status is `DOING` and `--commit-open` is absent | 6 | `Error: --commit-open is required when transitioning to DOING` |
| Target status is `COMPLETED` and `--commit-close` is absent | 6 | `Error: --commit-close is required when transitioning to COMPLETED` |
| Value is not a valid commit hash | 6 | `Error: invalid commit hash for --commit-open: "X" (expected 7 to 64 hexadecimal characters)` |
| Flag written with no value after it | 2 | `Error: --commit-open requires a value` |

In every case no task in the batch is changed: the commit flags are validated before the ids are resolved and before any write.

**Examples:**
```bash
rmp task stat -r project1 1,2,3 DOING --commit-open 5f93b51
rmp task stat -r project1 7 DOING --commit-open $(git rev-parse HEAD)
rmp task stat -r project1 7 TESTING
rmp task stat -r project1 7 COMPLETED --commit-close 2578d18 --summary "Shipped behind feature flag"
rmp task stat -r project1 7 BACKLOG
```

---

### reopen

Returns one or more tasks to `BACKLOG` and clears their lifecycle timestamps (`started_at`, `tested_at`, `closed_at`), their `completion_summary`, and their `commit_close`. `commit_open` is preserved: a reopening invalidates where the work ended, not where it began.

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

---

## Comment Commands

Commands for a task's work log: a durable, append-oriented record of what was tried, what was found, and why the work went the way it did. A task comment records exclusively the work carried out within the scope of that task. Read oldest first, the log shows how the work on the task progressed.

Three properties apply to all four subcommands:

- **Comment ids are per-family.** A comment id identifies a task comment only. The same number in the `sprint` family identifies an unrelated sprint comment, so `rmp task comment-edit 7` and `rmp sprint comment-edit 7` address different comments.
- **Comments are accepted in every task status,** including `COMPLETED`. No comment subcommand checks or changes a task's status, and `task reopen` does not touch comments.
- **`-y`/`--type` carries a different enum here.** On `list`, `create`, and `edit` it carries a task type; on the three comment subcommands that accept it — `comment-add`, `comment-list`, and `comment-edit` — it carries a comment type. The two sets are never interchangeable: a task type such as `BUG` is rejected on a comment subcommand (exit 6). See [Comment Type Values](#comment-type-values).

### comment-add

Adds one comment to a task's work log. The comment is stored with its type, its body, and a creation timestamp; `updated_at` starts null.

**Usage:** `rmp task comment-add -r <roadmap> <task-id> --type <TYPE> [--body <text>]` or `rmp task c-add ...`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-id` | Yes | Integer id of the task the comment is attached to |

**Flags:**
| Short Flag | Long Flag | Type | Max Length | Description |
|------------|-----------|------|------------|-------------|
| `-r` | `--roadmap` | string | 50 | Roadmap name (required) |
| `-y` | `--type` | enum | - | Comment type (required, no default); one of the seven task comment types |
| `-b` | `--body` | string | 4096 | Comment text. When the flag is absent, the body is read from standard input (see below) |

**Body from standard input:** when `--body` is absent, the whole of standard input is read to EOF and used as the body. On `comment-add` no other change is ever possible, so an absent `--body` always means "read standard input". Leading and trailing whitespace is trimmed before validation and before storage; interior line breaks are preserved, so a multi-line body survives intact. Two cases fail with exit code 2: standard input that is empty, whitespace only, or not connected when the body must come from it; and a `--body` whose value is empty, whitespace only, or missing (no following token, or the following token is itself a flag) — the command does not fall back to standard input in that case.

`--type` is validated, for presence and then for value, before the body is resolved, so a missing or invalid type fails immediately instead of leaving the command waiting on standard input for a body it would reject anyway.

**Output:** JSON object with the created comment ID.

**Examples:**
```bash
rmp task comment-add -r project1 42 --type FINDING --body "The parser accepts the boundary second exactly; no rounding occurs."
rmp task c-add -r project1 42 -y HYPOTHESIS -b "The truncation happens in the writer, not the reader."
rmp task comment-add -r project1 42 --type DECISION < decision.txt
```

**Example output:**
```json
{"id": 12}
```

An absent `--type` is rejected (exit 2); a value outside the seven task comment types is rejected (exit 6). A body longer than 4096 characters, or one containing control characters, is rejected (exit 6). An unknown task id is rejected (exit 4).

---

### comment-list

Returns every comment of the given task, oldest first. The listing is the task's work log, so the order is the story it tells.

**Usage:** `rmp task comment-list -r <roadmap> <task-id> [--type <TYPE>]` or `rmp task c-ls ...`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `task-id` | Yes | Integer id of the task whose comments are listed |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |
| `-y` | `--type` | enum | Optional filter: return only comments of this type. The value must be one of the seven task comment types |

**Ordering:** `created_at` ascending, with the comment `id` ascending as the tie-breaker for comments created within the same millisecond.

**Result-set size:** unbounded. Every matching comment is returned; there is no `--limit` flag, no `--desc` flag, and no pagination.

**Output:** JSON array of Comment objects; `[]` when the task has no comments, or none of the requested type.

**Examples:**
```bash
rmp task comment-list -r project1 42
rmp task comment-list -r project1 42 --type DECISION
rmp task c-ls -r project1 42 -y FINDING
```

**Example output:**
```json
[
  {
    "updated_at": null,
    "type": "FINDING",
    "body": "The parser accepts the boundary second exactly; no rounding occurs.",
    "created_at": "2026-03-12T11:15:00.000Z",
    "id": 12,
    "task_id": 42
  }
]
```

Listing is a read: it writes no audit entry. An invalid `--type` value is rejected (exit 6).

---

### comment-edit

Changes the type and/or the body of one existing task comment, identified by the comment's own id. At least one change is required.

**Usage:** `rmp task comment-edit -r <roadmap> <comment-id> [--type <TYPE>] [--body <text>]` or `rmp task c-edit ...`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `comment-id` | Yes | Integer id of the comment, **not** the id of the task it belongs to |

**Flags:**
| Short Flag | Long Flag | Type | Max Length | Description |
|------------|-----------|------|------------|-------------|
| `-r` | `--roadmap` | string | 50 | Roadmap name (required) |
| `-y` | `--type` | enum | - | New comment type; one of the seven task comment types |
| `-b` | `--body` | string | 4096 | New comment text. Read from standard input when this flag is absent **and** `--type` is absent too (see below) |

**Body from standard input:** standard input is read only when neither `--type` nor `--body` is given. When `--type` is present and `--body` is absent, only the type changes, standard input is not read, and a type-only edit therefore never blocks waiting for input. The trimming, empty-value, and missing-value rules are those of `comment-add`.

**Replacement semantics:** the edit replaces the stored body in place and stamps `updated_at` with the edit's timestamp, so a later listing shows that the comment was altered. The previous text is not retained anywhere and cannot be recovered; the audit log records that an edit happened, not what it replaced.

**No-op is not accepted.** Unlike `task edit`, which succeeds when no field is given, `comment-edit` requires at least one change and fails with exit code 2 when none is requested. A change is requested by a `--type` value, by a `--body` value, or by a body arriving on standard input, so the flagless form `comment-edit <comment-id> < revised.txt` is a valid edit and not a no-op.

**Output:** Empty on success (exit 0).

**Examples:**
```bash
rmp task comment-edit -r project1 12 --type NOTE
rmp task comment-edit -r project1 12 --body "Revised after re-reading the reader."
rmp task comment-edit -r project1 12 < revised.txt
rmp task c-edit -r project1 12 -y DECISION -b "Chose the stricter comparison."
```

A comment id that does not exist among the task comments is rejected (exit 4), including an id that exists only among the sprint comments: the two id spaces are independent.

---

### comment-remove

Deletes one task comment, identified by the comment's own id. The row is removed outright; there is no soft delete and no recovery. The audit entry survives the row, so the task's history still records that a comment existed and was removed.

**Usage:** `rmp task comment-remove -r <roadmap> <comment-id>` or `rmp task c-rm ...`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `comment-id` | Yes | Integer id of the comment, **not** the id of the task it belongs to |

**Flags:**
| Short Flag | Long Flag | Type | Description |
|------------|-----------|------|-------------|
| `-r` | `--roadmap` | string | Roadmap name (required) |

**Single-id command:** `comment-remove` takes exactly one comment id. It accepts no comma-separated list, so the batch rules that govern `task remove` do not apply. It takes no `-y`/`--type` and rejects the flag as unknown (exit 2).

**Output:** Empty on success (exit 0).

**Examples:**
```bash
rmp task comment-remove -r project1 12
rmp task c-rm -r project1 12
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
| `comment-add` | `c-add` |
| `comment-list` | `c-ls` |
| `comment-edit` | `c-edit` |
| `comment-remove` | `c-rm` |

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
- Comments are strictly additive. They are accepted in every status, including `COMPLETED`; no comment subcommand checks or changes a task's status, and no comment gates a transition. `task reopen` does not touch comments.
- `comment-add` and `comment-list` take the TASK's id; `comment-edit` and `comment-remove` take the COMMENT's own id.
- Task and sprint comment ids are separate sequences, so `rmp task comment-edit 7` and `rmp sprint comment-edit 7` address two unrelated comments.
- Comment operations are audited against the parent task, as `TASK_COMMENT_CREATE`, `TASK_COMMENT_UPDATE`, and `TASK_COMMENT_DELETE`; `comment-list` is a read and writes no audit entry.

## Field Limits and Constraints

| Field | Required | Max Length / Range | Description |
|-------|----------|--------------------|-------------|
| `roadmap` | Yes | 50 chars (regex `^[a-z0-9_-]+$`) | Target roadmap name |
| `title` | Yes (on create) | 255 chars | Task title/summary |
| `functional-requirements` | Yes (on create) | 4096 chars | Why: functional requirements |
| `technical-requirements` | Yes (on create) | 4096 chars | How: technical description |
| `acceptance-criteria` | Yes (on create) | 4096 chars | How to verify: completion criteria |
| `summary` | No | 4096 chars | Completion summary (only on COMPLETED transition) |
| `type` | No | one of 10 task types | Task type (default: TASK) |
| `priority` | No | 0-9 | Priority level (default: 0) |
| `severity` | No | 0-9 | Severity level (default: 0) |
| `type` (comment) | Yes (on `comment-add`) | one of 7 comment types | Comment classification; no default |
| `body` | Yes (on `comment-add`) | 4096 chars | Comment text; supplied through `--body` or on standard input |

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

### Comment Type Values

Carried by `-y`/`--type` on `comment-add`, `comment-list`, and `comment-edit`. A task comment accepts all seven values:

- `FINDING` - Something discovered during the work: an observed behaviour, a measurement, a cause identified, a constraint that turned out to apply
- `HYPOTHESIS` - A proposition raised to explain a problem or to guide the next step, stated before it is confirmed or refuted
- `TEST` - A test that was run and what it showed; covers both automated tests and manual verification
- `DECISION` - A decision taken during the work, and the reasoning behind it
- `PROGRESS` - A statement of how the work advanced: what was done, what remains
- `UPDATE` - The reason behind a modification to the definition of the task: something added, updated, removed, complemented, or clarified
- `NOTE` - A remark that belongs in the log but fits none of the categories above

**Per-entity applicability.** The comment type enum is per-entity, and the two sets are not symmetric. A sprint comment accepts only `FINDING`, `DECISION`, `PROGRESS`, and `UPDATE`; the three task-only values `HYPOTHESIS`, `TEST`, and `NOTE` are rejected on a sprint with exit code 6, because a sprint records how the sprint went and not the execution diary of its individual tasks. Every value valid on a sprint is also valid on a task, so no sprint comment type is rejected here.

**Two enums, one flag spelling.** `-y`/`--type` carries a task type on `list`, `create`, and `edit`, and a comment type on `comment-add`, `comment-list`, and `comment-edit`. A value from one set is rejected by the other: a task type such as `BUG` on a comment subcommand fails with exit code 6, and a comment type such as `FINDING` on `task create` fails that command's own type validation. `comment-remove` takes no `-y`/`--type` at all and rejects the flag as unknown (exit 2).

### Task Object Keys

Task objects returned by `list`, `get`, `next`, `subtasks`, `blockers`, and `blocking` contain:
`id`, `title`, `status`, `type`, `functional_requirements`, `technical_requirements`, `acceptance_criteria`, `created_at`, `started_at`, `tested_at`, `closed_at`, `completion_summary`, `commit_open`, `commit_close`, `parent_task_id`, `priority`, `severity`, `subtask_count`, `depends_on`, `blocks`.

`commit_open` and `commit_close` are `null` until the task enters `DOING` and `COMPLETED` respectively; see [stat (set-status)](#stat-set-status).

Task objects carry no `comments` array and no comment count: comments are read only through `comment-list` and the read-only web interface.

### Comment Object Keys

Comment objects returned by `comment-list` contain:
`id`, `task_id`, `type`, `body`, `created_at`, `updated_at`.

`updated_at` is always present and is `null` until the comment is first edited, after which it carries the edit's timestamp. `body` preserves the author's interior line breaks as `\n` escapes in JSON.

## Output Format

All commands follow these conventions:
- **Success:** JSON to stdout, exit code 0. `create` and `comment-add` emit `{"id": <int>}`; read commands, `comment-list` included, emit a JSON array; mutating commands (`edit`, `stat`, `prio`, `sev`, `reopen`, `remove`, `add-dep`, `remove-dep`, `comment-edit`, `comment-remove`) emit empty stdout.
- **Errors:** Plain text to stderr, non-zero exit code.
- **Dates:** ISO 8601 UTC with milliseconds, suffix `Z` (e.g. `2026-05-24T14:30:00.000Z`).

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Database failure |
| 2 | Misuse: missing required flag, bad syntax, or an invalid id argument (a `<task-id>`, `<task-ids>`, `<blocker-id>`, or `<comment-id>` that is not a positive integer is rejected by the parser before any database access). On the comment subcommands it also covers a missing `--type` on `comment-add`, a body supplied by neither `--body` nor standard input, and a `comment-edit` that requests no change at all |
| 3 | No roadmap specified (`-r` missing) |
| 4 | Task or comment not found (a syntactically valid id that does not exist) |
| 6 | Validation error: bad `--type`/`--status`/enum value, out-of-range number, oversized field, invalid state transition (including `stat SPRINT`), subtask/dependency guard, or dependency cycle. On `stat` it also covers a missing, misplaced, or malformed `--commit-open`/`--commit-close`. On the comment subcommands it covers a comment type outside the seven task values, a body over 4096 characters, and a body containing control characters |

Note the distinction: an id that is not a positive integer (for example `abc` or `0`) is an exit-code-2 syntax error, whereas a well-formed id for a task or a comment that does not exist is an exit-code-4 not-found error. An invalid `--type` or target status value is an exit-code-6 validation error.

The comment subcommands split the two failure kinds along the same line. A missing or unusable **body** is a misuse error (exit 2), because the command was invoked without the input it needs; an **oversized or control-character** body is a validation error (exit 6), because the input arrived and was rejected on its content.
