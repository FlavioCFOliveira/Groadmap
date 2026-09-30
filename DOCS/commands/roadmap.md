# roadmap

## Description

Roadmap management — the top-level containers for tasks and sprints. Each roadmap lives in its own directory `~/.roadmaps/<name>/`, which holds the roadmap's SQLite database at `~/.roadmaps/<name>/project.db`.

## Synopsis

```
rmp roadmap [subcommand] [arguments] [flags]
```

## Subcommands

### list

Lists all existing roadmaps.

**Usage:** `rmp roadmap list` or `rmp road ls`

**Output:** JSON array of roadmap objects

**Example:**
```bash
rmp roadmap list
rmp road ls
```

**Example output:**
```json
[
  {"name": "project1", "path": "~/.roadmaps/project1/project.db", "size": 24576},
  {"name": "project2", "path": "~/.roadmaps/project2/project.db", "size": 8192}
]
```

---

### create

Creates a new roadmap.

**Usage:** `rmp roadmap create <name>` or `rmp road new <name>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Roadmap name. Must match the regex `^[a-z0-9_-]+$`, be at most 50 characters, and not be a reserved Windows name (CON, PRN, COM1..9, ...). |

**Name rules:** the length is counted in characters (Unicode code points), never in bytes. A name that breaks several rules is refused with the line of the first rule it breaks, in this order: empty, longer than 50 characters, starts with a hyphen, reserved name, character outside `^[a-z0-9_-]+$`. A 51-character name that holds a space is therefore refused with `Error: Roadmap name must not exceed 50 characters (got 51)`.

**An occupied path:** when `~/.roadmaps/<name>` exists but is neither a directory nor a symbolic link, such as a regular file, it is not a roadmap, so the command does not report that the roadmap already exists; it cannot create the roadmap home there either. It fails with exit code 1 and `Error: I/O error: cannot create roadmap "X": <path> is occupied and is not a directory`, `<path>` being the absolute path of that entry. Nothing is created, changed, or removed.

**Output:** JSON object with the created roadmap name

**Examples:**
```bash
rmp roadmap create myproject
rmp road new myproject
```

**Example output:**
```json
{"name": "myproject"}
```

---

### remove

Removes a roadmap permanently, deleting its entire `~/.roadmaps/<name>/` directory (the database and every file the roadmap owns). This action cannot be undone.

**Usage:** `rmp roadmap remove <name>` or `rmp road rm <name>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Name of the roadmap to remove |

A name under which no roadmap exists is refused with exit code 4 and `Error: resource not found: roadmap "X" not found`, and nothing is removed. That includes a regular file at `~/.roadmaps/<name>`: it is not a roadmap, and `remove` removes only a roadmap.

**Examples:**
```bash
rmp roadmap remove myproject
rmp road rm oldproject
```

---

## Aliases

| Command | Alias |
|---------|-------|
| `roadmap` | `road` |
| `list` | `ls` |
| `create` | `new` |
| `remove` | `rm`, `delete` |

## Notes

- Each roadmap lives in its own home directory `~/.roadmaps/<name>/`, which holds the SQLite database `~/.roadmaps/<name>/project.db` (plus its `-wal`/`-shm` sidecars). This directory is the roadmap's home for all of its files, including future per-roadmap artefacts.
- The `~/.roadmaps/` directory and each `~/.roadmaps/<name>/` directory have permissions `0700` (owner only); `project.db` has permissions `0600`.
- A roadmap `<name>` exists only when `~/.roadmaps/<name>/` is a directory that holds `project.db`. No entry of that name, a directory without `project.db`, and an entry that is neither a directory nor a symbolic link (such as a regular file) are each a roadmap that does not exist: every command that selects it with `-r`/`--roadmap` refuses it with exit code 4, `roadmap remove` refuses it with exit code 4, and the web interface answers it with HTTP 404. A symbolic link at that path is never followed: the command fails with exit code 1 instead.
- The name given to `-r`/`--roadmap` on every `task`, `sprint`, `backlog`, `audit`, `graph` and `stats` subcommand is judged by the same name rules as `roadmap create`, and a name that breaks one is refused with exit code 6 and that rule's line, instead of the roadmap-not-found line.
- Legacy roadmaps stored in the old `~/.roadmaps/<name>.db` layout are migrated automatically to `~/.roadmaps/<name>/project.db` on the next `rmp` invocation, without data loss.

## Output Format

All commands follow these conventions:
- **Success**: JSON output to stdout, exit code 0
- **Errors**: Plain text to stderr, non-zero exit code

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | `create` only: `~/.roadmaps/<name>` is occupied by an entry that is not a directory, such as a regular file |
| 2 | Required roadmap-name argument missing; or, on `list`, an unrecognised flag or a positional argument (`list` takes none) |
| 4 | Roadmap not found (`remove` only), including a regular file or a directory without `project.db` at `~/.roadmaps/<name>` |
| 5 | Roadmap already exists (`create` only) |
| 6 | Invalid roadmap name (regex, length in characters, leading hyphen, or reserved word); the line of the first rule the name breaks is printed |
| 127 | Unknown subcommand |
