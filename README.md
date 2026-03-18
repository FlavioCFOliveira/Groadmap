# Groadmap

Local Roadmap Manager CLI for agentic workflows. Groadmap is a CLI tool in Go for managing technical roadmaps, using SQLite as backend.

## Features

- **Roadmap Management**: Create, list, select, and remove roadmaps
- **Task Management**: Create, edit, list tasks with status, priority, and severity
- **Sprint Management**: Organize tasks into sprints with complete lifecycle
- **Audit Trail**: Automatic logging of all operations
- **Backup/Restore**: Create and restore roadmap backups
- **Export/Import**: Export and import roadmaps in JSON format
- **Metrics**: Monitor operations and performance

## Available Commands

| Command | Description | Documentation |
|---------|-------------|---------------|
| `roadmap` | Roadmap management (create, list, select) | [DOCS/commands/roadmap.md](DOCS/commands/roadmap.md) |
| `task` | Task management | [DOCS/commands/task.md](DOCS/commands/task.md) |
| `sprint` | Sprint management | [DOCS/commands/sprint.md](DOCS/commands/sprint.md) |
| `audit` | Audit log | [DOCS/commands/audit.md](DOCS/commands/audit.md) |
| `user` | User management | [DOCS/commands/user.md](DOCS/commands/user.md) |
| `metrics` | Metrics and monitoring | [DOCS/commands/metrics.md](DOCS/commands/metrics.md) |

## Installation

### Build from Source

```bash
# Clone the repository
git clone https://github.com/FlavioCFOliveira/Groadmap.git
cd Groadmap

# Build
go build -o ./bin/ ./cmd/rmp

# Add to PATH (optional)
cp ./bin/rmp /usr/local/bin/
```

## Quick Start

```bash
# Create a new roadmap
rmp roadmap create myproject

# Select default roadmap
rmp roadmap use myproject

# Create a task
rmp task create -d "Implement feature X" -a "Develop code" -e "Feature working"

# List tasks
rmp task list

# Create a sprint
rmp sprint create -d "Sprint 1 - Setup"

# Add tasks to sprint
rmp sprint add-tasks 1 1,2,3

# Start sprint
rmp sprint start 1
```

## Project Structure

```
.
├── cmd/rmp/main.go          # CLI entry point
├── internal/
│   ├── commands/            # Subcommands (roadmap, task, sprint, audit)
│   ├── db/                  # SQLite, schema, parameterized queries
│   ├── models/              # Structs and enums
│   └── utils/               # JSON, ISO 8601 dates, paths
├── bin/                     # Build output
├── SPEC/                    # Technical specifications
└── DOCS/                    # Command documentation
```

## Conventions

- **Success output**: JSON to stdout
- **Error output**: Plain text to stderr
- **Dates**: ISO 8601 UTC
- **Roadmaps**: Stored in `~/.roadmaps/` with permissions `0700`

## Technical Documentation

See the `SPEC/` folder for detailed technical documentation:
- `SPEC/ARCHITECTURE.md` - System design
- `SPEC/COMMANDS.md` - CLI hierarchy and aliases
- `SPEC/DATABASE.md` - SQLite schema
- `SPEC/DATA_FORMATS.md` - JSON output schema
- `SPEC/MODELS.md` - Model definitions
- `SPEC/STATE_MACHINE.md` - State machines

## License

MIT License - see [LICENSE](LICENSE) for details.
