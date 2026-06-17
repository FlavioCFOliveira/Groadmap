# backlog

## Description

Inspect the tasks waiting in a roadmap's backlog. The `backlog` command groups the read-only planning views over tasks whose status is `BACKLOG`: `list` returns the full backlog (with optional filtering, sorting, and paging) and `show-next` returns the top-priority candidates to pull into the next sprint. Both subcommands require a target roadmap; there is no default or active roadmap, so `-r` / `--roadmap` must always be supplied.

## Synopsis

```
rmp backlog <subcommand> -r <roadmap> [arguments] [options]
```

## Subcommands

### list

Lists every task currently in `BACKLOG` status. It is equivalent to `rmp task list -r <roadmap> --status BACKLOG`, but with a focused option set for the planning view.

#### Usage

```
rmp backlog list -r <roadmap> [filters]
```

#### Arguments

This subcommand takes no positional arguments.

#### Flags

| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | **Required.** Target roadmap |
| `-p` | `--priority` | integer | - | Keep only tasks with priority `>= <min>`. This is a lower-bound filter, **not** a validated `0-9` value: out-of-range numbers (negative, or above 9) are accepted and simply match accordingly |
| `-y` | `--type` | string | - | Filter by task type. One of: `USER_STORY`, `TASK`, `BUG`, `SUB_TASK`, `EPIC`, `REFACTOR`, `CHORE`, `SPIKE`, `DESIGN_UX`, `IMPROVEMENT`. An invalid value fails with exit code 6 |
| - | `--sort` | string | `priority` | Sort order. One of: `priority`, `created`, `status`, `severity` |
| `-l` | `--limit` | integer | `100` | Maximum number of tasks returned (range 1-100). A non-integer value is rejected by the flag parser as misuse (exit code 2); an out-of-range value fails validation (exit code 6) |
| `-h` | `--help` | bool | false | Show command help |

#### Examples

```bash
# List the full backlog of a roadmap
rmp backlog list -r myproject

# Keep only tasks with priority 7 or higher
rmp backlog list -r myproject --priority 7

# Filter by type and sort by severity
rmp backlog list -r myproject --type BUG --sort severity

# Sort by creation date and cap the result at 50 tasks
rmp backlog list -r myproject --sort created --limit 50
```

### show-next

Returns the top `<count>` backlog tasks ordered by priority descending, then by creation date ascending for ties. It answers the sprint-planning question "what should we pull in next?".

Compared to related commands:
- `rmp task next` returns the top tasks from the OPEN sprint, not from the backlog.
- `rmp backlog list --sort priority` produces the same ordering but applies no implicit count limit, which is useful when filtering and paging are needed.

#### Usage

```
rmp backlog show-next -r <roadmap> [count]
```

#### Arguments

| Argument | Type | Default | Description |
|----------|------|---------|-------------|
| `count` | integer | `5` | Maximum number of tasks to return. The maximum is `100`; values above `100` are silently clamped to `100` |

#### Flags

| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | **Required.** Target roadmap |
| `-h` | `--help` | bool | false | Show command help |

#### Examples

```bash
# Show the top 5 backlog candidates (default count)
rmp backlog show-next -r myproject

# Show the top 10 backlog candidates
rmp backlog show-next -r myproject 10
```

## Aliases

| Command | Alias |
|---------|-------|
| `backlog` | `bl` |
| `backlog list` | `ls` |

There is no alias for `backlog show-next`.

## Notes

- Both subcommands operate exclusively on tasks whose status is `BACKLOG`.
- `-r` / `--roadmap` is mandatory on every subcommand. There is no default or active roadmap and no command to set one, so omitting `-r` is always an error (exit code 3).
- `list` applies its `--limit` after filtering and sorting; `show-next` ignores `--limit` and uses its own positional `count`.
- `--priority` on `list` is a `>=` lower-bound filter and is not validated against the `0-9` range; the `0-9` validation that `task create`/`edit` apply does not apply here.

## Output Format

Both subcommands write a JSON array of task objects to stdout. Every returned task has `status` equal to `BACKLOG`. Each task object carries the standard task keys: `id`, `title`, `status`, `type`, `functional_requirements`, `technical_requirements`, `acceptance_criteria`, `created_at`, `specialists`, `started_at`, `tested_at`, `closed_at`, `completion_summary`, `parent_task_id`, `priority`, `severity`, `subtask_count`, `depends_on`, and `blocks`. See `DOCS/commands/task.md` (Output Format) for the full key reference.

## Exit Codes

| Exit Code | Meaning |
|-----------|---------|
| 0 | Success |
| 2 | Misuse: non-integer `--limit` on `list` (rejected by the flag parser) |
| 3 | No roadmap specified (`-r` / `--roadmap` missing) |
| 6 | Validation error: bad `--type` or `--sort` value; out-of-range `--limit`; non-positive or non-numeric `count` on `show-next` |

## See Also

- `DOCS/commands/task.md` - the task object shape and the full task command set
- `DOCS/commands/sprint.md` - planning the sprints that consume the backlog
