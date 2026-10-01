# `rmp` CLI — full reference (binary v1.17.3)

Load this file when invoking a command not covered by the cheat-sheet in `SKILL.md`, or when you need exact flag semantics. The binary's own `rmp --ai-help` emits the canonical machine-readable JSON contract — prefer it whenever in doubt about a flag, an enum, or an exit code.

Replace `<rdm>` with the target roadmap name. All commands return JSON on stdout unless noted as "empty (exit 0)". Two commands are **out of scope** and intentionally omitted from this reference: `rmp graph`, which belongs to the `knowledge-authority` skill, and the `web` command, which this skill never starts.

## Table of contents

1. [AI contract](#ai-contract)
2. [Roadmap](#roadmap)
3. [Task](#task)
4. [Comments (task and sprint)](#comments-task-and-sprint)
5. [Sprint](#sprint)
6. [Backlog](#backlog)
7. [Audit](#audit)
8. [Stats](#stats)
9. [Aliases](#aliases)
10. [The 15 pitfalls](#the-15-pitfalls)
11. [Removed since v1.13.x](#removed-since-v113x)

---

## AI contract

```bash
rmp --ai-help                                 # full machine-readable JSON contract (stdout)
rmp ai-help                                   # identical — subcommand form
```

The contract exposes `schema_version` (2.0.0), `tool`, `conventions`, `exit_codes`, `enums` (8), `global_flags` (3), `commands` (9), `common_workflows` (8), and `pitfalls` (20). Of those, one command (`graph`), one workflow (`build_knowledge_graph`) and five pitfalls (`graph_*`) concern the knowledge graph and are **out of scope** for this skill — they belong to `knowledge-authority`. The `web` command is out of scope too.

### The AI-agent hint, and how to read stderr

The hint line is ``AI agents usage: run `rmp --ai-help` for a machine-readable command contract.`` Its placement was measured at v1.17.3:

| `AI_AGENT` | stderr on failure | stderr on success |
|---|---|---|
| unset, or any value other than `1` | `Error: …` · blank · hint · blank | *empty* (0 bytes) |
| `1` | hint · blank · `Error: …` · blank | hint · blank |

So the hint is appended on **every failure regardless of `AI_AGENT`**, and `AI_AGENT=1` additionally prints it *before* every invocation — which pushes the `Error:` line to line 3. The contract documents only the `enable_value: "1"` half of this.

**Claude Code sets `AI_AGENT` itself** (measured `claude-code_2-1-287_agent`), so the hint is always present on errors. Extract the error by pattern, never by position:

```bash
rmp <cmd> … 2>&1 >/dev/null | grep '^Error:'    # correct
rmp <cmd> … 2>&1 | tail -1                      # WRONG — a blank line
rmp <cmd> … 2>&1 | head -1                      # WRONG under AI_AGENT=1 — the hint
```

### Divergences between the contract and the binary (measured v1.17.3)

**The binary wins.** Three contract statements are contradicted by it:

1. **`sprint reorder` DOES print JSON.** The `parse_modification_stdout` pitfall names `sprint reorder` as an example of a command that prints nothing. In fact it, `move-to`, `swap`, `top` and `bottom` each return `{"success": true, "sprint_id": …, …}` — which their own subcommand entries correctly declare as `object`.
2. **`task edit`'s published defaults are misleading.** It advertises `--type TASK`, `--priority 0`, `--severity 0`, but an omitted flag leaves the field unchanged — verified: a title-only edit left `BUG` / priority 1 / severity 8 intact. `task edit` is a genuine partial update.
3. **`conventions.ai_agent_env_var` describes only half of the hint.** It gives `enable_value: "1"`; the binary appends the hint on every failure whatever `AI_AGENT` holds — see the table above.

One `--help` text is also contradicted: `backlog list --help` says `-p`/`--priority` takes any number, but the binary refuses a value outside 0–9 with exit 6, as the contract's `range` says.

---

## Roadmap

```bash
rmp roadmap list                              # Array of {name, path, size}
rmp roadmap create <name>                     # {"name": "<name>"}        exit 5 if it exists
rmp roadmap remove <name>                     # empty (exit 0)            exit 4 if missing — DESTRUCTIVE, confirm first
```

Names match `^[a-z0-9_-]+$`, ≤ 50 chars, and are not a reserved Windows device name such as `con` or `com1` (violation → exit 6). Each roadmap lives in `~/.roadmaps/<name>/` (dir mode 0700) with the SQLite database at `~/.roadmaps/<name>/project.db` (mode 0600). `roadmap remove` deletes the whole directory recursively, WAL sidecars and the knowledge graph included.

**A roadmap whose knowledge-graph server is running is not removed:** `roadmap remove` refuses with exit 6 and removes nothing. The server belongs to `knowledge-authority`; ask the user to stop it first.

**`roadmap` commands do not take `-r` — the name is positional.** Passing it anyway fails with an error that names the wrong cause: `rmp roadmap remove -r myproject` → exit **6**, `Error: validation error: roadmap name cannot start with '-'`, because `-r` is consumed as the positional name. `roadmap remove` is non-interactive and prompts for nothing, so confirm with the user *before* invoking it.

---

## Task

### List & filter (filters compose with AND)

```bash
rmp task list -r <rdm>
rmp task list -r <rdm> -s BACKLOG|SPRINT|DOING|TESTING|COMPLETED
rmp task list -r <rdm> -p <0-9>               # priority >= N (outside 0-9 → exit 6)
rmp task list -r <rdm> --severity <0-9>       # severity >= N
rmp task list -r <rdm> -y <TYPE>              # by task type
rmp task list -r <rdm> --created-since <date> --created-until <date>   # ISO 8601 or YYYY-MM-DD
rmp task list -r <rdm> --sort priority|created|status|severity         # default: priority
rmp task list -r <rdm> -l <1-100>             # default 100; outside 1-100 → exit 6
```

`--sort priority` and `severity` are DESC; `created` is ASC; `status` follows state-machine order.

### Get / next / relationships

```bash
rmp task get <id> -r <rdm>                    # array with one task object
rmp task get <id1>,<id2>,<id3> -r <rdm>       # CSV, NO spaces; fail-fast on any unknown id (exit 4)
rmp task next -r <rdm> [N]                    # next N incomplete tasks of the OPEN sprint   exit 4 if no OPEN sprint
rmp task subtasks <id> -r <rdm>               # direct subtasks (one level, no grandchildren)
rmp task blockers <id> -r <rdm>               # incomplete dependencies blocking <id>
rmp task blocking <id> -r <rdm>               # tasks that depend on <id> (reverse of blockers)
```

`task next` returns tasks in **sprint position ascending**, not priority order, and **default N = 1** (clamped to 100). It does **not** exclude tasks with incomplete blockers — filter them yourself with `task blockers`.

There is **no `sprint next`** (exit 127) — the OPEN-sprint "what next" query is `task next`.

### Create (all four content fields required; lands in BACKLOG)

```bash
rmp task create -r <rdm> \
  -t   "Title"                                \  # max 255
  -fr  "Why?  Functional requirements"        \  # max 4096  (for a BUG: include reproduction steps here)
  -tr  "How?  Technical requirements"         \  # max 4096
  -ac  "How to verify?  Acceptance criteria"  \  # max 4096
  [-y  USER_STORY|TASK|BUG|SUB_TASK|EPIC|REFACTOR|CHORE|SPIKE|DESIGN_UX|IMPROVEMENT] \  # default TASK
  [-p 0-9] [--severity 0-9]                    \  # each default 0
  [--parent <parent-id>]                          # create only — attaches as a subtask
# Output: {"id": <int>}
```

`--parent` sets `parent_task_id` and bumps the parent's `subtask_count`. It does **not** set `-y SUB_TASK` — pass the type explicitly if you want it.

> **Text-field parsing trap — a value must never start with `- `.** The CLI reads the leading hyphen as the next flag and aborts with `Error: required parameter missing: <flag> requires a value` (exit 2). Re-verified at v1.17.3 on `-t`/`--title`, `-fr`, `-tr`, `-ac` and sprint `-d`/`--description`, in both short and long forms; quoting does not help, because the check happens after the shell has passed the argument. `--summary` and comment `--body` tolerate it, but **use `*` for every bullet uniformly** so the rule needs no exceptions. Markdown otherwise round-trips byte-exact: `**bold**`, `` `code` ``, `*` lists, `###` headings and blank lines are all stored and returned unchanged. Content style is governed by the Writing rules in `SKILL.md`.

### Edit (status NOT editable here — use `task stat`)

```bash
rmp task edit <id> -r <rdm> [-t ...] [-fr ...] [-tr ...] [-ac ...] [-y ...] [-p N] [--severity N]
```

Each supplied field writes its own audit row (`TASK_TITLE_CHANGE`, `TASK_PRIORITY_CHANGE`, …). An invocation with no option succeeds as a no-op (exit 0).

**A partial update, despite what the contract says.** `rmp --ai-help` publishes defaults for `task edit` (`--type TASK`, `--priority 0`, `--severity 0`); they do **not** apply. An omitted flag leaves the field untouched — verified at v1.17.3: editing only `-t` on a `BUG` with priority 1 and severity 8 left all three unchanged. Never pass a field "to keep it", and never read those defaults as a reset. Pair every content edit with an `UPDATE` comment saying why the definition changed.

### Status & lifecycle — the two commit-gated transitions

```bash
rmp task stat <ids> DOING     -r <rdm> --commit-open  <hash>   # -co; MANDATORY here, rejected elsewhere (from SPRINT, or from TESTING for rework)
rmp task stat <ids> TESTING   -r <rdm>                         # neither commit flag accepted
rmp task stat <ids> COMPLETED -r <rdm> --commit-close <hash> [-s "..."]   # -cc MANDATORY; -s optional, max 4096
rmp task reopen <ids> -r <rdm>                # DOING/TESTING/COMPLETED → SPRINT in its own sprint; clears started_at/tested_at/closed_at/summary/commit_close
rmp task prio <ids> <0-9> -r <rdm>            # set priority for one or many
rmp task sev  <ids> <0-9> -r <rdm>            # set severity for one or many
```

- Commit hashes are 7–64 hexadecimal characters, accepted in any case and **stored lowercase**. A malformed hash → exit 6. `rmp` runs no git command and reads no repository: the caller supplies the hash.
- One hash applies to every id of a multi-id invocation.
- `--commit-open` on any target other than `DOING`, or `--commit-close` on any target other than `COMPLETED`, → exit 6.
- `reopen` **preserves `commit_open`**; a later `stat DOING --commit-open <hash>` replaces it. No command ever clears it.
- `reopen` keeps the task in its sprint and at its position. Ids already in `SPRINT` or `BACKLOG` are skipped with a note on stderr; a task whose sprint is `CLOSED` is refused (exit 6) — `sprint reopen` first.
- `SPRINT` is rejected on `task stat` (exit 6) — only `sprint add-tasks` and `task reopen` set it. `BACKLOG` is rejected on `task stat` for every sprint member, `COMPLETED` included (exit 6). `BACKLOG → DOING` is rejected too: a task must be in a sprint first.
- Completing is gated on all subtasks and dependencies being `COMPLETED` (exit 6 otherwise).

### Dependencies (self-edges and cycles rejected with exit 6)

```bash
rmp task add-dep    <id> <dep-id> -r <rdm>    # <id> depends on <dep-id>
rmp task remove-dep <id> <dep-id> -r <rdm>    # exit 4 if the edge does not exist
```

Each writes two audit rows, one against each task of the pair, each naming the other in `related_entity_id`.

### Remove — BACKLOG only, no subtasks — DESTRUCTIVE, confirm first

```bash
rmp task remove <ids> -r <rdm>
```

Rejected (exit 6) if any id is not `BACKLOG`, or if any id still has subtasks — of **any** status, `COMPLETED` included (measured at v1.17.3). A sprint member leaves its sprint with `sprint remove-tasks` first; a `COMPLETED` member must be returned to `SPRINT` with `task reopen` before that.

**Task object keys:** `id, title, status, type, functional_requirements, technical_requirements, acceptance_criteria, created_at, started_at, tested_at, closed_at, completion_summary, commit_open, commit_close, parent_task_id, priority, severity, subtask_count, depends_on, blocks`.

---

## Comments (task and sprint)

An append-oriented, typed, chronological log per task and per sprint. The two families are identical in shape and differ only in their accepted type set.

```bash
# Task comments — types: FINDING, HYPOTHESIS, TEST, DECISION, PROGRESS, UPDATE, NOTE
rmp task comment-add    <task-id>    -r <rdm> --type <TYPE> [--body "..."]     # {"id": <int>}
rmp task comment-list   <task-id>    -r <rdm> [--type <TYPE>]                  # array, oldest first
rmp task comment-edit   <comment-id> -r <rdm> [--type <TYPE>] [--body "..."]   # empty (exit 0)
rmp task comment-remove <comment-id> -r <rdm>                                  # empty (exit 0) — IRREVERSIBLE

# Sprint comments — types: FINDING, DECISION, PROGRESS, UPDATE only
rmp sprint comment-add    <sprint-id>  -r <rdm> --type <TYPE> [--body "..."]
rmp sprint comment-list   <sprint-id>  -r <rdm> [--type <TYPE>]
rmp sprint comment-edit   <comment-id> -r <rdm> [--type <TYPE>] [--body "..."]
rmp sprint comment-remove <comment-id> -r <rdm>
```

Semantics that matter:

- **`-y`/`--type` here means a COMMENT type**, not the `TaskType` the same spelling carries on `task list/create/edit`. Passing `BUG` → exit 6.
- **`HYPOTHESIS`, `TEST` and `NOTE` are task-only.** On a sprint comment → exit 6. A sprint comment records how the *sprint* went, not the diary of one task.
- **`--type` is required on `comment-add`**, optional as a filter on `comment-list`, and optional on `comment-edit`.
- **`--body` falls back to stdin.** Omit it and the body is read from standard input under a bounded read (max 4096 chars). Neither source → exit 2. On `comment-edit`, stdin is read only when **both** `--type` and `--body` are absent, so a type-only edit never blocks on input.
- **`comment-edit` requires a type or a body** — from `--type`, `--body`, or stdin. With neither flag and empty stdin it fails with exit 2; unlike `task edit`, it does not succeed as a no-op. With neither flag it **waits on stdin**, so never run it bare from a terminal.
- **The positional id on `comment-edit`/`comment-remove` is the COMMENT's own id**, returned by `comment-add`, not the parent task's or sprint's id. Task and sprint comment ids are separate sequences.
- **`comment-remove` accepts exactly one id** — no CSV.
- **Comments are accepted in every status**, `COMPLETED` and `CLOSED` included, and never change or gate a status.
- **`comment-list` is unbounded** — no `--limit`, no `--desc`, no pagination. Ordered `created_at` ASC, comment id as tie-breaker.
- **Editing is lossy, deleting is final.** The previous body is retained nowhere; the audit row records only that an edit or delete happened, and never the comment's own id.

**Comment object keys:** `id, task_id` (or `sprint_id`), `type, body, created_at, updated_at` (`null` until first edit).

### Which type, and when

The types are not interchangeable labels — each marks a different kind of fact, and picking the right one is what makes the log answerable later. `SKILL.md` § *The work log* holds the full discipline; this is the lookup table.

| Type | Task | Sprint | Written the moment… |
|---|:-:|:-:|---|
| `HYPOTHESIS` | ✓ | — | a proposition is about to be tested, **before** the result is known |
| `TEST` | ✓ | — | a test, benchmark or verification was run — the command, and what it showed |
| `FINDING` | ✓ | ✓ | something was discovered, measured, or a hypothesis was refuted |
| `DECISION` | ✓ | ✓ | a choice was taken — with its reasoning **and the options rejected** |
| `PROGRESS` | ✓ | ✓ | the work advanced materially — a part delivered, a blocker cleared |
| `UPDATE` | ✓ | ✓ | the definition changed — always alongside `task edit` / `sprint update`, saying **why** |
| `NOTE` | ✓ | — | something worth keeping fits none of the above |

Reading recipes:

```bash
rmp task comment-list <id> -r <rdm>                    # full history, oldest first — read before resuming or closing
rmp task comment-list <id> -r <rdm> --type DECISION    # the "why": answer history questions from here
rmp task comment-list <id> -r <rdm> --type TEST        # the evidence gathered
rmp task comment-list <id> -r <rdm> --type UPDATE      # why the definition changed, before editing it again
rmp sprint comment-list <sid> -r <rdm>                 # the sprint narrative — the PDS draws on this
```

If the log does not answer the question, say it was not recorded. Never infer a reason and present it as the record.

---

## Sprint

### List & inspect

```bash
rmp sprint list -r <rdm>                            # all sprints
rmp sprint list -r <rdm> --status PENDING|OPEN|CLOSED
rmp sprint get  <id> -r <rdm>                       # raw sprint object
rmp sprint show <id> -r <rdm>                       # stand-up summary (NO task list — only task_order IDs)
rmp sprint stats <id> -r <rdm>                      # burndown, velocity, progress, days_elapsed
                                                    # NOTE: days_remaining is ALWAYS null (no end_date)
```

### Create / update / remove

```bash
rmp sprint create -r <rdm> -t "title" -d "description" [--max-tasks N] [--order N]
#   -t (title, max 255) AND -d (description, max 2048) are BOTH REQUIRED.
#   Together they must state the sprint's macro objective.
rmp sprint update <id> -r <rdm> [-t "..."] [-d "..."] [--max-tasks N] [--order N]
rmp sprint remove <id> -r <rdm>                     # SPRINT/DOING/TESTING members → BACKLOG; sprint deleted — DESTRUCTIVE, confirm first
                                                    # refused (exit 6) if the sprint holds any COMPLETED task
```

`--max-tasks` range 1–10000. It caps **active** tasks (`SPRINT`/`DOING`/`TESTING`), and is **freely raised or lowered** — lowering below the current load is accepted and simply blocks further adds (`adding N task(s) would exceed sprint #S capacity (L/M tasks active)`, exit 6). It cannot be *removed* once set: 0 is out of range (exit 6).

`--order` is a positive integer, unique across the roadmap (duplicate → exit 5), auto-assigned to `max+1` when omitted, mutable while `PENDING`/`OPEN`, immutable once `CLOSED` (exit 6). In JSON the field is named `order`.

### Lifecycle

```bash
rmp sprint start  <id> -r <rdm>                     # PENDING → OPEN (sets started_at)
rmp sprint close  <id> -r <rdm> [--force]           # OPEN → CLOSED; --force bypasses the active-task check (confirm first)
rmp sprint reopen <id> -r <rdm>                     # CLOSED → OPEN; clears closed_at; started_at preserved
```

At most one sprint may be OPEN at a time — `start` and `reopen` are both rejected (exit 6) while another is OPEN. `close` without `--force` is rejected (exit 6) if any task is SPRINT/DOING/TESTING.

### Membership & ordering

```bash
rmp sprint tasks      <id> -r <rdm> [-s STATUS] [--order-by-priority]   # all members (incl. COMPLETED)
rmp sprint open-tasks <id> -r <rdm> [--order-by-priority]               # SPRINT/DOING/TESTING only
rmp sprint add-tasks    <sid> <ids> -r <rdm>        # atomic BACKLOG → SPRINT; an active task of another sprint keeps its status; rejected if CLOSED or over capacity
rmp sprint remove-tasks <sid> <ids> -r <rdm>        # remove members → BACKLOG (clears timestamps, summary, commit_close; keeps commit_open)
rmp sprint move-tasks   <from> <to> <ids> -r <rdm>  # move between sprints; task status preserved
rmp sprint reorder      <sid> <ids> -r <rdm>        # FULL CSV of ALL members in desired order
rmp sprint move-to      <sid> <task> <pos> -r <rdm> # move one task to a 0-based position
rmp sprint swap         <sid> <t1> <t2> -r <rdm>    # swap positions of two tasks
rmp sprint top          <sid> <task> -r <rdm>       # move to position 0
rmp sprint bottom       <sid> <task> -r <rdm>       # move to last position
```

**A `COMPLETED` task stays in the sprint it was completed in:** `add-tasks`, `remove-tasks` and `move-tasks` each reject it with exit 6.

Default ordering everywhere is **sprint position ascending**; `--order-by-priority` re-sorts by priority DESC (priority is 0 lowest … 9 highest). Position — not priority — is what `task next` follows.

**The five ordering commands print JSON on success**, unlike every other mutation: `reorder` → `{"sprint_id", "success", "task_order"}`; `move-to`/`top`/`bottom` → `{"position", "sprint_id", "success", "task_id"}`; `swap` → `{"sprint_id", "success", "task_id_1", "task_id_2"}`. `add-tasks`, `remove-tasks` and `move-tasks` print nothing. The contract's subcommand entries declare exactly this; only its `parse_modification_stdout` pitfall still claims `sprint reorder` prints nothing — see [Divergences](#divergences-between-the-contract-and-the-binary-measured-v1173). Judge success by the exit code regardless.

**Sprint object keys:** `id, status, title, description, created_at, started_at, closed_at, max_tasks, tasks (array of int, may be null), task_count, order`.

---

## Backlog

Planning view over `BACKLOG`-status tasks only. Never a source of executable work.

```bash
rmp backlog list -r <rdm>                           # all BACKLOG tasks
rmp backlog list -r <rdm> -p <min>                  # priority >= min, 0-9 (outside → exit 6, whatever --help says)
rmp backlog list -r <rdm> -y <TYPE>
rmp backlog list -r <rdm> --sort priority|created|status|severity     # default: priority
rmp backlog list -r <rdm> -l <1-100>                # default 100
rmp backlog show-next [N] -r <rdm>                  # top-N by priority DESC, created_at ASC for ties
                                                    # default 5; > 100 silently clamped
```

Both subcommands return an array of task objects with `status == BACKLOG` (same shape as `task`).

---

## Audit

```bash
rmp audit list -r <rdm>
rmp audit list -r <rdm> -o <OPERATION>              # filter by operation
rmp audit list -r <rdm> -e TASK|SPRINT              # filter by entity type
rmp audit list -r <rdm> --entity-id <n>             # filter by specific entity
rmp audit list -r <rdm> --since <date> --until <date>   # ISO 8601 or YYYY-MM-DD
rmp audit list -r <rdm> -l <1-500>                  # default 100 (> 500 → exit 6)

rmp audit history TASK   <id> -r <rdm>              # full history for a task (newest first)
rmp audit history SPRINT <id> -r <rdm>              # full history for a sprint
rmp audit stats    -r <rdm> [--since <date>] [--until <date>]
```

**Valid `-o` operations (current set).** Filters compose with AND; entries are newest first.

- **Task lifecycle:** `TASK_CREATE`, `TASK_DELETE`, `TASK_REOPEN`, `TASK_SPRINT_CHANGE` (a task taken by `sprint add-tasks` from another sprint)
- **Task status (one per target status — there is no single "status change" op):** `TASK_STATUS_BACKLOG`, `TASK_STATUS_SPRINT`, `TASK_STATUS_DOING`, `TASK_STATUS_TESTING`, `TASK_STATUS_COMPLETED`
- **Task field edits (one per field):** `TASK_TITLE_CHANGE`, `TASK_TYPE_CHANGE`, `TASK_FUNCTIONAL_REQUIREMENTS_CHANGE`, `TASK_TECHNICAL_REQUIREMENTS_CHANGE`, `TASK_ACCEPTANCE_CRITERIA_CHANGE`, `TASK_PRIORITY_CHANGE`, `TASK_SEVERITY_CHANGE`
- **Task dependencies:** `TASK_ADD_DEP`, `TASK_REMOVE_DEP`
- **Task comments:** `TASK_COMMENT_CREATE`, `TASK_COMMENT_UPDATE`, `TASK_COMMENT_DELETE`
- **Sprint lifecycle:** `SPRINT_CREATE`, `SPRINT_DELETE`, `SPRINT_START`, `SPRINT_CLOSE`, `SPRINT_REOPEN`
- **Sprint field edits:** `SPRINT_TITLE_CHANGE`, `SPRINT_DESCRIPTION_CHANGE`, `SPRINT_MAX_TASKS_CHANGE`, `SPRINT_ORDER_CHANGE`
- **Sprint membership & ordering:** `SPRINT_ADD_TASK`, `SPRINT_REMOVE_TASK`, `SPRINT_MOVE_TASK_OUT`, `SPRINT_MOVE_TASK_IN`, `SPRINT_REORDER_TASKS`, `SPRINT_TASK_MOVE_POSITION`, `SPRINT_TASK_SWAP`
- **Sprint comments:** `SPRINT_COMMENT_CREATE`, `SPRINT_COMMENT_UPDATE`, `SPRINT_COMMENT_DELETE`
- **LEGACY — accepted as a filter, but only pre-1.12.0 rows carry them:** `TASK_STATUS_CHANGE`, `TASK_UPDATE`, `SPRINT_UPDATE`, `SPRINT_MOVE_TASK`. Filtering on these against a modern roadmap returns `[]` with exit 0 — a silent empty result, not an error. Use the split operations above.

Notable row semantics:

- `TASK_STATUS_DOING` carries the `--commit-open` hash in `commit_hash`; `TASK_STATUS_COMPLETED` carries `--commit-close`. All other rows have `commit_hash: null`.
- `TASK_STATUS_BACKLOG` from `sprint remove-tasks` names the departed sprint in `related_entity_id`; from `task stat BACKLOG` it is `null`.
- `task reopen` writes `TASK_REOPEN` **alone** — no `TASK_STATUS_SPRINT` row.
- `TASK_ADD_DEP`/`TASK_REMOVE_DEP` write two rows, one per task of the pair, each naming the other.
- Comment rows are logged against the **parent** task/sprint; the comment's own id is never recorded.

`list`/`history` → array of `{id, operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at}` (performed_at DESC). `stats` → `{total_entries, first_entry_at, last_entry_at, by_operation, by_entity_type}`.

---

## Stats

```bash
rmp stats -r <rdm>
# {
#   "roadmap": "<name>",
#   "sprints":  {current, total, completed, pending},
#   "tasks":    {backlog, sprint, doing, testing, completed},
#   "average_velocity": <float over the last 5 closed sprints>
# }
```

---

## Aliases

Canonical name first, as the contract declares it — note that for the status/priority/severity setters the **short form is the canonical one** and the long form is the alias, the opposite of what reads naturally.

| Canonical | Alias(es) |
|---|---|
| `roadmap` | `road` |
| `task` | `t` |
| `sprint` | `s` |
| `backlog` | `bl` |
| `audit` | `aud` |
| `list` | `ls` |
| `create` | `new` |
| `remove` | `rm` (also `delete` on `roadmap`) |
| `update` | `upd` |
| `stat` | `set-status` |
| `prio` | `set-priority` |
| `sev` | `set-severity` |
| `add-tasks` | `add` |
| `remove-tasks` | `rm-tasks` |
| `move-tasks` | `mv-tasks` |
| `reorder` | `order` |
| `move-to` | `mvto` |
| `bottom` | `btm` |
| `history` | `hist` |
| `comment-add` | `c-add` |
| `comment-list` | `c-ls` |
| `comment-edit` | `c-edit` |
| `comment-remove` | `c-rm` |

Short flags worth knowing: `-r` = `--roadmap`, `-fr` = `--functional-requirements`, `-tr` = `--technical-requirements`, `-ac` = `--acceptance-criteria`, `-co` = `--commit-open`, `-cc` = `--commit-close`, `-s` = `--summary` (on `task stat`) or `--status` (on the list commands), `-y` = `--type`, `-b` = `--body`, `-p` = `--priority`, `-l` = `--limit`, `-o` = `--operation`, `-e` = `--entity-type`, `-t` = `--title`, `-d` = `--description`. **`--severity` and `--sort` have no short form.** (`-q` = `--query` exists but belongs only to `rmp graph`, which is out of scope here.)

---

## The 15 pitfalls

The anti-patterns documented in `rmp --ai-help`. The contract publishes 20; the **5 `graph_*` pitfalls are omitted here as out of scope** (they belong to `knowledge-authority`), leaving the 15 that concern tasks, sprints and the backlog. The top 7 are inlined in `SKILL.md`; here is the full set.

| Pitfall | Wrong | Correct |
|---|---|---|
| **Roadmap has no numeric ID** — always use the name | `rmp task list -r 42` | `rmp task list -r myproject` |
| **Never set SPRINT status manually** — use `sprint add-tasks` | `rmp task stat -r rdm 42 SPRINT` | `rmp sprint add-tasks -r rdm 7 42` |
| **`task remove` on a non-BACKLOG task** — `sprint remove-tasks` first; a COMPLETED task needs `task reopen` before that (and clear any subtasks) | `rmp task remove -r rdm 42` (SPRINT task) | `rmp sprint remove-tasks -r rdm 7 42` → `rmp task remove -r rdm 42` |
| **`sprint add-tasks` on a CLOSED sprint** — create or use a PENDING/OPEN sprint | `rmp sprint add-tasks -r rdm 3 42,43` (CLOSED) | `rmp sprint create -r rdm -t "S8" -d "..."` → `rmp sprint add-tasks -r rdm 8 42,43` |
| **`task next` without an OPEN sprint** — plan/start a sprint first | `rmp task next -r rdm` (no OPEN sprint) | `rmp sprint start -r rdm 7` → `rmp task next -r rdm` |
| **Missing commit hash on a gated transition** — `--commit-open` on DOING and `--commit-close` on COMPLETED are mandatory, and rejected on every other transition | `rmp task stat -r rdm 42 DOING` | `rmp task stat -r rdm 42 DOING --commit-open $(git rev-parse HEAD)` |
| **Completing a task with open blockers** — `task next` does not filter them out for you | `rmp task stat -r rdm 42 COMPLETED --commit-close <h>` (has blockers) | `rmp task blockers -r rdm 42` → complete them → complete 42 |
| **`--summary` on a non-COMPLETED transition** | `rmp task stat -r rdm 42 DOING -co <h> -s "..."` | `rmp task stat -r rdm 42 COMPLETED -cc <h> -s "..."` |
| **Partial `sprint reorder`** — supply ALL member IDs | `rmp sprint reorder -r rdm 7 42,43` (omits others) | `rmp sprint tasks -r rdm 7` → pass every ID in order |
| **Non-ISO date input** | `--since 24/05/2026` | `--since 2026-05-24` |
| **Assuming partial batch success** — it is all-or-nothing | `rmp task stat -r rdm 42,99999,43 COMPLETED -cc <h>` | `rmp task get -r rdm 42,43` first, then stat |
| **Invalid roadmap name** | `rmp roadmap create My Project!` | `rmp roadmap create my-project` |
| **Parsing modification-command stdout** — it is empty | `result=$(rmp task stat -r rdm 42 DOING -co <h>); echo "$result"` | `rmp task stat -r rdm 42 DOING -co <h> && echo "ok"` |
| **Task-only comment type on a sprint** — `HYPOTHESIS`/`TEST`/`NOTE` are rejected there | `rmp sprint comment-add -r rdm 7 --type HYPOTHESIS -b "..."` | `rmp sprint comment-add -r rdm 7 --type FINDING -b "..."` |

Two further comment-specific traps, documented in the contract under the same heading:

- **Not every comment subcommand prints JSON.** Only `comment-add` (`{"id": …}`) and `comment-list` (array) do; `comment-edit` and `comment-remove` print nothing — read the result back with `comment-list`.
- **The id on `comment-edit`/`comment-remove` is the comment's own id**, not the parent task's or sprint's.

### Two further traps, not in the contract

Found empirically, reproduced at v1.17.3, and absent from `rmp --ai-help`:

| Pitfall | Wrong | Correct |
|---|---|---|
| **A text field whose value starts with `- `** is read as the next flag → exit 2 `requires a value`. Hits `-t`/`--title`, `-fr`, `-tr`, `-ac`, sprint `-d`/`--description`, short and long forms alike; quoting does not help. (`--summary` and comment `--body` tolerate it.) | `rmp task create … -fr "- observed: X"` | `rmp task create … -fr "* observed: X"` |
| **Reading the error off the wrong stderr line.** A failure writes four lines and the `Error:` line is first only when `AI_AGENT` is not `1` — see [The AI-agent hint](#the-ai-agent-hint-and-how-to-read-stderr). | `rmp task get -r rdm 9999 2>&1 \| tail -1` | `rmp task get -r rdm 9999 2>&1 >/dev/null \| grep '^Error:'` |

Always echo the exit code after a create or edit: a bare `\| tail -2` swallows the error line and makes a failed create look like a success.

---

## Removed since v1.13.x

Do not invoke these — they no longer exist: the two subcommands fail with exit 127 (unknown subcommand), the flag with exit 2 (unknown flag):

| Gone | Replacement |
|---|---|
| `task assign <id> <specialist>` | none — the concept was removed |
| `task unassign <id> <specialist>` | none |
| `-sp` / `--specialists` on `task list`, `task create`, `task edit` | none; the `specialists` field is gone from the task object |
| `TASK_ASSIGN` / `TASK_UNASSIGN` audit operations | none |

Ownership of a task is now expressed in the task's content and its comment log, not in a dedicated field.

Separately, the `rmp graph …` surface was **rebuilt at v1.17.0**, so any graph command an older document prescribes may no longer exist. It makes no difference here: that surface is out of scope for this skill and belongs to `knowledge-authority`. Never invoke a `rmp graph …` command from this skill — hand the request over instead.
