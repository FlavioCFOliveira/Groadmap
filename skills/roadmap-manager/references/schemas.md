# JSON shapes and field semantics (binary v1.18.0)

Load this file when inspecting an unfamiliar field in `rmp` output, building a custom view, or verifying exact field names and types. Field lists below were verified against a live v1.18.0 binary.

## Task object

Returned by `task list/get/next/subtasks/blockers/blocking` (as array elements), by `sprint tasks/open-tasks`, and by `backlog list/show-next`.

| Field | Description |
|---|---|
| `id` | Numeric identifier |
| `title` | ≤ 255 chars |
| `status` | One of `BACKLOG`, `SPRINT`, `DOING`, `TESTING`, `COMPLETED` |
| `type` | One of `USER_STORY`, `TASK`, `BUG`, `SUB_TASK`, `EPIC`, `REFACTOR`, `CHORE`, `SPIKE`, `DESIGN_UX`, `IMPROVEMENT` (`FEATURE` does not exist) |
| `functional_requirements` | Why? (≤ 4096). For a `BUG`, holds the reproduction steps |
| `technical_requirements` | How? (≤ 4096) |
| `acceptance_criteria` | How to verify? (≤ 4096) |
| `priority` / `severity` | 0–9 each (0 = lowest, 9 = highest / most critical) |
| `parent_task_id` | Parent's id when created with `--parent`, else `null`. Independent of `type` — a child is not automatically `SUB_TASK` |
| `subtask_count` | Number of direct subtasks. Any non-zero value blocks `task remove` |
| `depends_on` | Array of task ids this one depends on (its blockers) |
| `blocks` | Array of task ids that depend on this one |
| `created_at` / `started_at` / `tested_at` / `closed_at` | ISO 8601 UTC timestamps; the last three are `null` until reached and are cleared on `reopen` and when the task leaves its sprint for `BACKLOG` |
| `commit_open` | Lowercase git hash supplied as `--commit-open` on `→ DOING`; `null` until then. **Never cleared** — survives `reopen`; only a new `→ DOING` replaces it |
| `commit_close` | Lowercase git hash supplied as `--commit-close` on `→ COMPLETED`; `null` until then; **cleared on `reopen`** and when the task returns to `BACKLOG` |
| `completion_summary` | Optional free text supplied on the `COMPLETED` transition; cleared on `reopen` |

> There is **no `specialists` field**. It was removed together with `task assign`/`unassign` and the `-sp` flag.

**`task edit` is a partial update.** The contract publishes defaults for its `--type`/`--priority`/`--severity` flags; they do not apply. An omitted flag leaves the field unchanged (verified at v1.18.0).

## Comment object

Returned by `task comment-list` and `sprint comment-list` (array), and referenced by id from `comment-add`'s `{"id": <int>}`.

| Field | Description |
|---|---|
| `id` | The comment's **own** id — what `comment-edit`/`comment-remove` take. Task and sprint comment ids are separate sequences |
| `task_id` / `sprint_id` | The parent entity (whichever family the comment belongs to) |
| `type` | Task: `FINDING`, `HYPOTHESIS`, `TEST`, `DECISION`, `PROGRESS`, `UPDATE`, `NOTE`. Sprint: `FINDING`, `DECISION`, `PROGRESS`, `UPDATE` only |
| `body` | ≤ 4096 chars |
| `created_at` | ISO 8601 UTC |
| `updated_at` | `null` until the first `comment-edit`, then the edit's timestamp |

Ordering is `created_at` ASC with `id` as tie-breaker — oldest first, unbounded (no `--limit`, no pagination). The previous body of an edited comment is retained nowhere.

## Sprint object

Returned by `sprint list` (array) and `sprint get` (single).

| Field | Description |
|---|---|
| `id` | Numeric identifier |
| `status` | `PENDING` \| `OPEN` \| `CLOSED` |
| `title` | ≤ 255 chars (required at creation) |
| `description` | ≤ 2048 chars (required at creation) — the sprint's macro objective |
| `max_tasks` | Capacity cap on **active** tasks (`SPRINT`/`DOING`/`TESTING`); `null` if unset. Freely raised **or lowered** (1–10000); cannot be removed once set |
| `order` | Execution order: positive integer, unique across the roadmap; auto-assigned to `max+1` when omitted; immutable once CLOSED. The JSON key is `order`, not `order_index` |
| `tasks` | Array of member task ids in sprint order (`[]` when there are none) |
| `task_count` | Convenience count |
| `created_at` / `started_at` / `closed_at` | ISO 8601 UTC timestamps (`null` until reached) |

### `sprint show` — stand-up summary (flat object, NO task list)

Keys: `sprint_id`, `sprint_title`, `sprint_description`, `status`, `max_tasks`, `capacity_pct`, `current_load`, `task_order` (array of member ids), `summary` `{total_tasks, pending, in_progress, completed}`, `progress` `{pending_percentage, in_progress_percentage, completed_percentage}`, `severity_distribution` `{ "0-2", "3-5", "6-7", "8-9" → {count, percentage} }`, `criticality_distribution` `{ low, medium, high, critical → {count, percentage} }`.

`current_load` counts only active tasks, which is what `max_tasks` caps. To list the actual member tasks use `sprint tasks` or `sprint open-tasks`.

### `sprint stats` — progress & velocity

Keys: `sprint_id`, `total_tasks`, `completed_tasks`, `progress_percentage`, `status_distribution` (map of status → count, absent statuses omitted), `task_order` (array), `burndown[]` (`{date, tasks_remaining}`), `velocity`, `days_elapsed`, `days_remaining`.

> `days_remaining` is **always `null`** — sprints have no end date. Do not derive or promise deadlines from it.

## Global `stats`

Keys: `roadmap`, `sprints` `{current, total, completed, pending}` (`current` = the OPEN sprint's id, or `null`), `tasks` `{backlog, sprint, doing, testing, completed}`, `average_velocity` (float over the last 5 closed sprints).

## Audit entry / audit stats

- `audit list` / `audit history` → array of `{id, operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at}`, ordered `performed_at` DESC.
  - `related_entity_id` names the *other* party of a two-entity operation (the sprint a task entered/left, the paired task of a dependency edge), else `null`.
  - `commit_hash` carries the `--commit-open` hash on `TASK_STATUS_DOING` rows and the `--commit-close` hash on `TASK_STATUS_COMPLETED` rows; `null` on every other operation.
- `audit stats` → `{total_entries, first_entry_at, last_entry_at, by_operation (map), by_entity_type (map)}`.

The full operation vocabulary — including the four LEGACY operations that only pre-1.12.0 rows carry — is listed in `cli.md` § Audit.

## I/O conventions

- **stdout** — successful output is JSON (may be empty `{}` or `[]`, or empty for modification commands); render as human-readable tables/bullets in the user's language.
- **stderr** — errors are plain text; surface them verbatim to the user. A failure writes **four lines**: `Error: …`, a blank, the `--ai-help` hint, a blank. With `AI_AGENT=1` the hint is printed first instead and is not repeated, so the failure writes three lines and `Error:` becomes line 3. Extract with `grep '^Error:'`, never by line position, and drop the hint line when quoting. See `cli.md` § *The AI-agent hint*.
- **Timestamps** — ISO 8601 UTC with milliseconds, suffix `Z`: `2026-05-24T14:30:00.000Z`. Date-range filters (`--since`/`--until`/`--created-since`/`--created-until`) also accept the short `YYYY-MM-DD` form.
- **Modification commands emit empty stdout** on success — do not parse; check the exit code. Measured empty at v1.18.0: `task stat`, `prio`, `sev`, `reopen`, `edit`, `remove`, `add-dep`, `remove-dep`, `comment-edit`, `comment-remove`, `sprint add-tasks`, `remove-tasks`, `move-tasks`, `update`, `start`, `close`, `reopen`, `remove`, `roadmap remove`.
- **Commands that DO print** — `roadmap create` → `{"name": …}`; `task create`, `sprint create`, `task comment-add`, `sprint comment-add` → `{"id": …}`.
- **The five sprint ORDERING commands also print**, as their contract entries declare (`object`); only the contract's `parse_modification_stdout` pitfall still says `sprint reorder` prints nothing:

  | Command | stdout on success |
  |---|---|
  | `sprint reorder` | `{"sprint_id": <int>, "success": true, "task_order": [<int>, …]}` |
  | `sprint move-to` | `{"position": <int>, "sprint_id": <int>, "success": true, "task_id": <int>}` |
  | `sprint top` | same shape as `move-to`, `position` = 0 |
  | `sprint bottom` | same shape as `move-to`, `position` = last index |
  | `sprint swap` | `{"sprint_id": <int>, "success": true, "task_id_1": <int>, "task_id_2": <int>}` |

  `success` is always `true` when the exit code is 0, so it carries no information the exit code does not — **still judge the outcome by the exit code**. `position` is 0-based.
- **stdin fallback** — `comment-add` and `comment-edit` read the body from standard input when `--body` is absent (bounded read). On `comment-edit` this happens only when `--type` is also absent; a type-only edit still reads a non-terminal stdin to check that it is empty, and refuses data there (exit 2). Neither source on `comment-add` → exit 2.
- **Charset / locale** — UTF-8, `C` locale; list separator is `,` (CSV, no spaces).

## Numeric ranges

- `--priority` / `-p`: 0–9 on create/edit/`task prio`, and as a *filter* (`task list -p`, `backlog list -p`) too: outside 0–9 → exit 6 (measured at v1.18.0, although `backlog list --help` still says any number is accepted).
- `--severity`: 0–9 (0 = lowest, 9 = most critical).
- `sprint --max-tasks`: 1–10000 (0 → exit 6). `sprint --order`: ≥ 1.
- `--limit`: task/backlog 1–100 (default 100; 0 or 101 → exit 6); audit 1–500 (default 100; 600 → exit 6).
- Positional counts: `task next [N]` default 1, `backlog show-next [N]` default 5 — both silently clamped to 100.
- Commit hashes: 7–64 hexadecimal characters, any case, stored lowercase.
- Text: title ≤ 255; sprint description ≤ 2048; `-fr`/`-tr`/`-ac`/`--summary`/comment `--body` ≤ 4096; roadmap name ≤ 50 matching `^[a-z0-9_-]+$`.

## State machines — full

```
Task lifecycle:

BACKLOG ─(sprint add-tasks)→ SPRINT ─(stat DOING --commit-open H)→ DOING ─(stat TESTING)→ TESTING ─(stat COMPLETED --commit-close H [-s …])→ COMPLETED
                                                                     ▲                        │
                                                                     └─(stat DOING --commit-open H: rework)

DOING / TESTING / COMPLETED ─(task reopen; clears started_at/tested_at/closed_at/summary/commit_close; KEEPS commit_open; stays in its sprint)→ SPRINT
SPRINT / DOING / TESTING    ─(sprint remove-tasks, or sprint remove; same clearing, KEEPS commit_open)→ BACKLOG

Rejected: BACKLOG → DOING (skips the sprint), any manual → SPRINT, any manual → BACKLOG for a sprint member,
and any move of a COMPLETED task out of its sprint (add-tasks, remove-tasks, move-tasks, sprint remove).

Sprint lifecycle:

PENDING ─(sprint start)→ OPEN ─(sprint close)→ CLOSED ─(sprint reopen)→ OPEN
                          │
                          └──(sprint close --force, only with explicit user approval)→ CLOSED
```

Every transition is recorded in the audit log with its operation, entity, `performed_at`, and — on the two commit-gated transitions — its `commit_hash`. Comments are recorded too (`*_COMMENT_CREATE/UPDATE/DELETE`, against the parent entity) but never affect either state machine.
