# stats

## Description

Report roadmap-wide statistics for a single roadmap. `rmp stats` returns a JSON object summarising sprint counts (current, total, completed, pending), the distribution of tasks across their statuses, and the average velocity computed over the last five closed sprints. It is a read-only command: it queries the roadmap and writes a summary to stdout without changing any data.

A target roadmap is mandatory. There is no default or active roadmap, so `-r` / `--roadmap` must always be supplied.

## Synopsis

```
rmp stats -r <roadmap> [options]
```

## Subcommands

`rmp stats` is a top-level command with no subcommands.

#### Arguments

This command takes no positional arguments.

#### Flags

| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | **Required.** Target roadmap |
| `-h` | `--help` | bool | false | Show command help |

#### Examples

```bash
# Show statistics for a roadmap
rmp stats -r myproject
```

## Aliases

`rmp stats` has no alias.

## Notes

- `-r` / `--roadmap` is mandatory. There is no default or active roadmap and no command to set one, so omitting `-r` is always an error (exit code 3).
- `average_velocity` is computed across the last five closed sprints.
- The command is read-only and does not modify any roadmap, sprint, or task.

## Output Format

On success, `rmp stats` writes a single JSON object to stdout:

```json
{
  "roadmap": "project-name",
  "sprints": {
    "current": 5,
    "total": 12,
    "completed": 10,
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

| Field | Type | Description |
|-------|------|-------------|
| `roadmap` | string | Name of the roadmap the statistics describe |
| `sprints.current` | integer | Identifier or count of the current sprint |
| `sprints.total` | integer | Total number of sprints in the roadmap |
| `sprints.completed` | integer | Number of completed sprints |
| `sprints.pending` | integer | Number of pending (not yet completed) sprints |
| `tasks.backlog` | integer | Tasks in `BACKLOG` status |
| `tasks.sprint` | integer | Tasks assigned to a sprint but not yet started |
| `tasks.doing` | integer | Tasks in progress |
| `tasks.testing` | integer | Tasks in testing |
| `tasks.completed` | integer | Completed tasks |
| `average_velocity` | number | Average velocity across the last five closed sprints |

## Exit Codes

| Exit Code | Meaning |
|-----------|---------|
| 0 | Success |
| 3 | No roadmap specified (`-r` / `--roadmap` missing) |
| 4 | Roadmap not found |

## See Also

- `DOCS/commands/sprint.md` - the sprints whose counts and velocity this command aggregates
- `DOCS/commands/task.md` - the tasks whose status distribution this command reports
