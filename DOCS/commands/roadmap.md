# roadmap

## Description

Roadmap management - the top-level containers for tasks and sprints. Each roadmap is stored as an independent SQLite database in `~/.roadmaps/`.

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
  {"name": "project1", "path": "~/.roadmaps/project1.db", "size": 24576},
  {"name": "project2", "path": "~/.roadmaps/project2.db", "size": 8192}
]
```

---

### create

Creates a new roadmap.

**Usage:** `rmp roadmap create <name>` or `rmp road new <name>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Roadmap name (alphanumeric, hyphens, underscores) |

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

Removes a roadmap permanently. This action cannot be undone.

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

### use

Selects a roadmap as the default for subsequent commands. Avoids repeating the `--roadmap` flag in every command.

**Usage:** `rmp roadmap use <name>` or `rmp road use <name>`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `name` | Yes | Name of the roadmap to select |

**Examples:**
```bash
rmp roadmap use myproject
rmp road use myproject
```

## Aliases

| Command | Alias |
|---------|-------|
| `roadmap` | `road` |
| `list` | `ls` |
| `create` | `new` |
| `remove` | `rm`, `delete` |

## Notes

- Roadmaps are stored in `~/.roadmaps/<name>.db`
- The `~/.roadmaps/` directory has permissions `0700` (owner only)
- The `.current` file in `~/.roadmaps/` stores the roadmap selected by `use`

## Output Format

All commands follow these conventions:
- **Success**: JSON output to stdout, exit code 0
- **Errors**: Plain text to stderr, non-zero exit code

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | No roadmap selected |
| 4 | Resource not found |
| 5 | Resource already exists |
