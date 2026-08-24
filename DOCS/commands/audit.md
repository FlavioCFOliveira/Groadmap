# audit

## Description

View the audit log and the history of a single entity. Every command that changes
a task or a sprint writes one or more audit entries, so the log is a complete
record of what happened, in what order, and to what.

The log is append-only. No command updates or deletes an entry, and an entry
outlives the row it describes: the `TASK_CREATE` entry of a deleted task, and the
`TASK_COMMENT_CREATE` entry of a deleted comment, both survive the deletion.

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

The canonical catalogue is `SPEC/DATABASE.md` § `audit` Table, and it holds 43
values: 39 that a command writes today, and 4 marked LEGACY that no command writes
any more but that `--operation` still accepts so that historical entries stay
reachable by name (see Legacy Operations below). Each operation names what
happened, so a reader learns the outcome from the operation alone.

Two of the seven fields of an entry carry the detail the operation name cannot:
`commit_hash` and `related_entity_id`. The entries below say which operations set
them; Audit Entry Fields, further down, says what the values mean.

**Task Operations:**
- `TASK_CREATE` - Task created via `task create`
- `TASK_DELETE` - Task deleted via `task remove`, which is allowed only while the task is in BACKLOG. The entry outlives the task it names

**Task Status Operations:** the five destination states, plus the reopen
transition. One entry per task named in the command, so a transition applied to
three tasks writes three entries. Written by `task stat` unless noted.
- `TASK_STATUS_BACKLOG` - Task entered BACKLOG. Written by `task stat <ids> BACKLOG` and by `sprint remove-tasks`; only the second names a sprint in `related_entity_id`
- `TASK_STATUS_SPRINT` - Task entered SPRINT. Written by `sprint add-tasks` alone, because `task stat` rejects the SPRINT target, so every entry names its sprint in `related_entity_id`
- `TASK_STATUS_DOING` - Task entered DOING. The entry records the mandatory `--commit-open` hash in `commit_hash`
- `TASK_STATUS_TESTING` - Task entered TESTING
- `TASK_STATUS_COMPLETED` - Task entered COMPLETED. The entry records the mandatory `--commit-close` hash in `commit_hash`
- `TASK_REOPEN` - Task returned to BACKLOG via `task reopen`. This is the only entry that command writes; it writes no `TASK_STATUS_BACKLOG` entry

**Task Field Operations:** one entry per field the invocation supplies, so an edit
that supplies three fields writes three entries. The entry records that the field
was written, never its old or new value, and it is written whether or not the new
value differs from the stored one.
- `TASK_TITLE_CHANGE` - `title` supplied to `task edit`
- `TASK_TYPE_CHANGE` - `type` supplied to `task edit`
- `TASK_FUNCTIONAL_REQUIREMENTS_CHANGE` - `functional_requirements` supplied to `task edit`
- `TASK_TECHNICAL_REQUIREMENTS_CHANGE` - `technical_requirements` supplied to `task edit`
- `TASK_ACCEPTANCE_CRITERIA_CHANGE` - `acceptance_criteria` supplied to `task edit`
- `TASK_PRIORITY_CHANGE` - Priority changed via `task prio` or `task edit -p`
- `TASK_SEVERITY_CHANGE` - Severity changed via `task sev` or `task edit --severity`

**Task Dependency Operations:** two entries per invocation, one against each task
of the pair, each naming the other task in `related_entity_id`.
- `TASK_ADD_DEP` - Dependency edge added between tasks via `task add-dep`
- `TASK_REMOVE_DEP` - Dependency edge removed between tasks via `task remove-dep`

**Comment Operations:** recorded against the PARENT entity; the comment's own id
never appears in the log.
- `TASK_COMMENT_CREATE` - Comment added to a task
- `TASK_COMMENT_UPDATE` - Task comment edited
- `TASK_COMMENT_DELETE` - Task comment deleted
- `SPRINT_COMMENT_CREATE` - Comment added to a sprint
- `SPRINT_COMMENT_UPDATE` - Sprint comment edited
- `SPRINT_COMMENT_DELETE` - Sprint comment deleted

**Sprint Operations:**
- `SPRINT_CREATE` - Sprint created
- `SPRINT_DELETE` - Sprint deleted. This is the only entry `sprint remove` writes, even though every member task reverts to BACKLOG
- `SPRINT_START` - Sprint started (PENDING to OPEN)
- `SPRINT_CLOSE` - Sprint closed (OPEN to CLOSED)
- `SPRINT_REOPEN` - Sprint reopened (CLOSED to OPEN)

**Sprint Field Operations:** one entry per column the invocation supplies, on the
same rule as the task field operations above.
- `SPRINT_TITLE_CHANGE` - `title` supplied to `sprint update`
- `SPRINT_DESCRIPTION_CHANGE` - `description` supplied to `sprint update`
- `SPRINT_MAX_TASKS_CHANGE` - `max_tasks` supplied to `sprint update`
- `SPRINT_ORDER_CHANGE` - `order_index` supplied to `sprint update`

**Sprint Membership Operations:** one entry per task, against the sprint, naming
the task in `related_entity_id`. Adding and removing also write a mirrored entry
against the task itself; moving does not, because a move changes no task's status.
- `SPRINT_ADD_TASK` - Task added to a sprint via `sprint add-tasks`; mirrored by `TASK_STATUS_SPRINT`
- `SPRINT_REMOVE_TASK` - Task removed from a sprint via `sprint remove-tasks`; mirrored by `TASK_STATUS_BACKLOG`
- `SPRINT_MOVE_TASK_OUT` - Task left a sprint in a `sprint move-tasks`; written against the source sprint
- `SPRINT_MOVE_TASK_IN` - Task entered a sprint in a `sprint move-tasks`; written against the destination sprint

**Sprint Task Ordering Operations:** written against the sprint, with both
`related_entity_id` and `commit_hash` NULL. Ordering changes the position of a
membership row rather than a task, so no entry is written against any task.
- `SPRINT_REORDER_TASKS` - Full order of a sprint's tasks set via `sprint reorder`
- `SPRINT_TASK_MOVE_POSITION` - Single task moved to a position via `sprint move-to`, `sprint top`, or `sprint bottom`
- `SPRINT_TASK_SWAP` - Two tasks swapped positions via `sprint swap`

**Legacy Operations:** the four values no command writes any more. `--operation`
accepts each of them so that entries recorded before schema 1.12.0 remain
reachable by name; on a roadmap created at schema 1.12.0 or later there are no
such entries and each filter returns an empty array. A roadmap migrated from an
earlier schema can still hold them. See Legacy Operations below for what each one
replaced and why it was not removed.
- `TASK_STATUS_CHANGE` - Replaced by the five `TASK_STATUS_*` operations
- `TASK_UPDATE` - Replaced by the per-field task operations
- `SPRINT_UPDATE` - Replaced by the per-field sprint operations
- `SPRINT_MOVE_TASK` - Replaced by `SPRINT_MOVE_TASK_OUT` and `SPRINT_MOVE_TASK_IN`

**Output:** JSON array of audit-entry objects, newest first (`performed_at` DESC).
Each object has the seven keys described in Audit Entry Fields below.

**Examples:**
```bash
rmp audit list -r project1
rmp audit ls -r project1 -o TASK_STATUS_DOING
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

**Output:** JSON array of audit entries for the entity, newest first, with the same seven keys `list` returns.

`history` selects on `entity_type` and `entity_id` alone. It therefore returns the
entries the entity *is the subject of*, not the entries that merely *name* it in
`related_entity_id`. To find the entries that name a task as the counterpart of
somebody else's operation, use `list` and filter the result on `related_entity_id`
yourself; there is no flag for it.

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

`by_operation` counts only the operations present in the window, so an operation
that never occurred is absent from the map rather than present with a count of
zero.

**Examples:**
```bash
rmp audit stats -r project1
rmp audit stats -r project1 --since 2026-03-01T00:00:00.000Z
```

## Audit Entry Fields

Both `list` and `history` return objects with exactly these seven keys, and no
others. There is no field naming the *type* of the related entity: `entity_type`
describes the subject of the entry, and the counterpart's type is implied by the
operation.

| Key | Type | Description |
|-----|------|-------------|
| `id` | int | Monotonically increasing entry id. Never reused and never renumbered |
| `operation` | string | One of the 43 operation values above |
| `entity_type` | string | `TASK` or `SPRINT`: the type of the entity whose history this entry belongs to |
| `entity_id` | int | The id of that entity. It may name an entity that has since been deleted |
| `performed_at` | string | ISO 8601 UTC timestamp, millisecond precision |
| `commit_hash` | string or null | The git commit bracketing a task's development work; `null` on every operation but two (see below) |
| `related_entity_id` | int or null | The counterpart entity of the operation; `null` when the operation has no counterpart (see below) |

### commit_hash

`commit_hash` records the git commit that brackets a task's development work. It
is written on exactly two operations, and is `null` on the other 41:

| Operation | Value recorded |
|-----------|----------------|
| `TASK_STATUS_DOING` | the commit the work started from, supplied as `--commit-open` on the transition into DOING |
| `TASK_STATUS_COMPLETED` | the commit that concluded the task, supplied as `--commit-close` on the transition into COMPLETED |

Both flags are mandatory on the transitions that write them, so from schema 1.12.0
onward every `TASK_STATUS_DOING` and `TASK_STATUS_COMPLETED` entry carries a
non-null hash. The value is the one supplied on the command line, normalised to
lowercase and validated as 7 to 64 hexadecimal characters; `rmp` runs no git
command and does not check that the commit exists.

Because entries are never rewritten, the audit log keeps commit history the task
itself no longer carries. `task reopen` clears `commit_close` on the task but
leaves the `TASK_STATUS_COMPLETED` entry and its `commit_hash` untouched, so the
commit that once concluded the task remains on the record. Re-entering DOING or
re-completing the task adds a further entry with the new hash rather than
replacing the earlier one.

No other operation records a commit, including `TASK_REOPEN`, and a task's stored
`commit_open`/`commit_close` values are never copied onto an unrelated entry.

### related_entity_id

`entity_type` and `entity_id` name the entity whose history the entry belongs to.
`related_entity_id` names the **counterpart entity of the operation that produced
the entry**, and is `null` when the operation has no counterpart. That one rule
decides the value everywhere.

Without it, two entries of the same operation would be indistinguishable: every
`SPRINT_ADD_TASK` entry of a sprint would read identically and none would say
which task was added.

The field is non-null in exactly these eight cases, and null everywhere else:

| Operation | Written by | `entity_type` / `entity_id` | `related_entity_id` names |
|-----------|------------|-----------------------------|---------------------------|
| `SPRINT_ADD_TASK` | `sprint add-tasks` | `SPRINT` / the sprint the task joined | the task added |
| `TASK_STATUS_SPRINT` | `sprint add-tasks` | `TASK` / the task added | the sprint it entered |
| `SPRINT_REMOVE_TASK` | `sprint remove-tasks` | `SPRINT` / the sprint the task left | the task removed |
| `TASK_STATUS_BACKLOG` | `sprint remove-tasks` | `TASK` / the task removed | the sprint it left |
| `SPRINT_MOVE_TASK_OUT` | `sprint move-tasks` | `SPRINT` / the source sprint | the task moved |
| `SPRINT_MOVE_TASK_IN` | `sprint move-tasks` | `SPRINT` / the destination sprint | the task moved |
| `TASK_ADD_DEP` | `task add-dep` | `TASK` / one task of the pair | the other task of the pair |
| `TASK_REMOVE_DEP` | `task remove-dep` | `TASK` / one task of the pair | the other task of the pair |

**The counterpart is usually of the other entity type.** In the first six rows the
subject is a sprint and the counterpart a task, or the reverse. Only the two
dependency operations name a counterpart of the same type, because a dependency
relates two tasks.

**A membership change writes two mirrored entries.** Adding task 42 to sprint 1
writes a `SPRINT_ADD_TASK` entry with `entity_id` 1 and `related_entity_id` 42,
and a `TASK_STATUS_SPRINT` entry with the two ids transposed and the same
`performed_at`. Each side is therefore complete on its own:
`audit history SPRINT 1` says which task joined, and `audit history TASK 42` says
which sprint it joined, with neither reader consulting the other entity's history.
`sprint remove-tasks` writes the same mirrored pair as `SPRINT_REMOVE_TASK` and
`TASK_STATUS_BACKLOG`.

**One operation, two writing commands, one rule.** `TASK_STATUS_BACKLOG` is
written both by `sprint remove-tasks` and by `task stat <ids> BACKLOG`, and it
carries a counterpart only from the first. This is the rule applied consistently
rather than an exception: a removal from a sprint has that sprint as its
counterpart, whereas `task stat` changes a task's status with no second entity
party to the operation. A `null` therefore always means "this operation had no
counterpart", never "it had one that was not recorded".

`TASK_STATUS_SPRINT` has only one writing command, so every entry carrying it
names a sprint.

**`sprint move-tasks` writes no `TASK_STATUS_*` entry at all,** because moving a
task between sprints preserves its status. The two sprint entries are the whole
record of the move.

**`sprint remove` writes one `SPRINT_DELETE` entry and no per-task entry,** even
though the member tasks revert to BACKLOG. The membership rows go away with the
sprint, and the sprint a per-task entry would have named no longer exists once the
deletion commits.

## Legacy Operations

Four values in the catalogue are marked LEGACY. No command has written any of them
since schema 1.12.0 split each into the more precise operations that replaced it:

| Legacy value | Replaced by | Why the replacement is better |
|--------------|-------------|-------------------------------|
| `TASK_STATUS_CHANGE` | `TASK_STATUS_BACKLOG`, `TASK_STATUS_SPRINT`, `TASK_STATUS_DOING`, `TASK_STATUS_TESTING`, `TASK_STATUS_COMPLETED` | The entry names the destination state, so a reader learns the outcome without reconstructing the task's timeline |
| `TASK_UPDATE` | `TASK_TITLE_CHANGE`, `TASK_TYPE_CHANGE`, `TASK_FUNCTIONAL_REQUIREMENTS_CHANGE`, `TASK_TECHNICAL_REQUIREMENTS_CHANGE`, `TASK_ACCEPTANCE_CRITERIA_CHANGE` | One entry per field, so the log says which field an edit touched |
| `SPRINT_UPDATE` | `SPRINT_TITLE_CHANGE`, `SPRINT_DESCRIPTION_CHANGE`, `SPRINT_MAX_TASKS_CHANGE`, `SPRINT_ORDER_CHANGE` | The same, for sprints |
| `SPRINT_MOVE_TASK` | `SPRINT_MOVE_TASK_OUT`, `SPRINT_MOVE_TASK_IN` | One entry against each sprint, each naming the task moved, so both sprints record the move |

**They remain in the catalogue because entries still carry them.** A value cannot
be retired while stored entries use it: the migration described below deliberately
reclassifies only what the stored data proves, so entries carrying all four values
survive in any roadmap that existed before schema 1.12.0.

`audit list --operation` therefore accepts all four, and `audit stats` counts them
in `by_operation`, so an old entry stays reachable by name. What no longer happens
is a new entry: from 1.12.0 onward `task edit` writes per-field entries and never
`TASK_UPDATE`, `sprint update` writes per-column entries and never `SPRINT_UPDATE`,
`task stat` writes per-destination entries and never `TASK_STATUS_CHANGE`, and
`sprint move-tasks` writes the OUT/IN pair and never `SPRINT_MOVE_TASK`.

On a roadmap created at schema 1.12.0 or later, filtering on any of the four
returns an empty array.

## Schema Migration 1.11.0 to 1.12.0

The migration that introduced `related_entity_id` and `commit_hash` runs
automatically the next time `rmp` opens an older roadmap. No user action is
required, and it is safe to run repeatedly. `SPEC/VERSION.md`
§ Migration 1.11.0 to 1.12.0 is the normative description; what follows is what a
user of an existing roadmap will observe.

**No entry is added, removed, or renumbered.** The audit table afterwards holds
exactly the entries it held before, with the same ids and the same `performed_at`
values. Only some `operation` values change, and the migration writes no audit
entry of its own.

**Neither new column is backfilled.** Every pre-existing entry keeps `null` in
both, because no truthful value is available for either. `rmp` holds no record of
which commit bracketed work already done: it runs no git command, and the task
columns that could have supplied one did not exist before 1.11.0. Nor is the
counterpart recoverable: a stored `SPRINT_ADD_TASK` entry names its sprint and
nothing else, and the sprint's *current* membership answers a different question,
saying nothing about which entry concerned which task and nothing at all about
tasks since removed. Inferring a value would fabricate a fact, so the migration
does not attempt it.

**Only `TASK_STATUS_CHANGE` is reclassified, and only where the stored data
settles the destination beyond doubt.** An entry is rewritten when its
`performed_at` is *exactly equal* to exactly one of the owning task's three
lifecycle timestamps:

| Timestamp matched | Entry becomes |
|-------------------|---------------|
| `started_at` | `TASK_STATUS_DOING` |
| `tested_at` | `TASK_STATUS_TESTING` |
| `closed_at` | `TASK_STATUS_COMPLETED` |

There is no tolerance window, no nearest match, and no ordering heuristic. An
entry keeps `TASK_STATUS_CHANGE` when its timestamp matches none of the three (a
transition into BACKLOG stamps no timestamp, and reopening a task clears the ones
that would have matched), when it matches more than one (two transitions recorded
at the same instant leave no evidence of which the entry recorded), or when the
task it names has since been deleted and took its timestamps with it.

**What the migration deliberately does not reclassify:**

- **`TASK_UPDATE` and `SPRINT_UPDATE` entries are never touched.** A generic field
  edit stamps no timestamp anywhere, so which field the entry recorded is simply
  not knowable from the stored data. The alternative is to guess, and a guessed
  entry is worse than an imprecise one.
- **`SPRINT_MOVE_TASK` entries are never touched.** Such an entry names one sprint
  and no task, so neither the task that moved nor the sprint it came from can be
  recovered, and the OUT/IN pair that replaced it cannot be reconstructed.
- **Nothing is inferred.** Choosing the nearest timestamp, assigning destinations
  by position in the log, or reading a task's current status to guess an entry's
  destination are all excluded. Each would write a fact the database does not hold.

The consequence is that all four legacy values survive in a migrated roadmap,
which is why the catalogue keeps them.

## Aliases

| Command | Alias |
|---------|-------|
| `audit` | `aud` |
| `list` | `ls` |
| `history` | `hist` |

## Notes

- The log covers tasks and sprints only. Every creation, field change, status transition, membership change, dependency change, comment change, and deletion of a task or a sprint is logged automatically. Roadmap-level commands (`roadmap create`, `roadmap remove`) and knowledge-graph commands write no entry, and neither do reads: `task list`, `sprint tasks`, `task comment-list`, `sprint comment-list` and the other listing commands
- A rejected command writes no entry at all, because the entry is written in the same transaction as the change it records
- The log is stored in the `audit` table of the SQLite database, one database per roadmap
- The `web` interface presents the same log read-only at `/roadmaps/{name}/audit`; serving a page writes no entry

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
