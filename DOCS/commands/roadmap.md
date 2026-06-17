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
- Legacy roadmaps stored in the old `~/.roadmaps/<name>.db` layout are migrated automatically to `~/.roadmaps/<name>/project.db` on the next `rmp` invocation, without data loss.

## Output Format

All commands follow these conventions:
- **Success**: JSON output to stdout, exit code 0
- **Errors**: Plain text to stderr, non-zero exit code

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 4 | Roadmap not found (`remove` only) |
| 5 | Roadmap already exists (`create` only) |
| 6 | Invalid roadmap name (regex, length, or reserved word) |
