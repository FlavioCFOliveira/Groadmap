# Groadmap

Local Roadmap Manager CLI for agentic workflows. Groadmap is a CLI tool in Go for managing technical roadmaps, using SQLite as backend.

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/FlavioCFOliveira/Groadmap/main/install.sh | bash
```

This will detect your OS and architecture, download the latest release from GitHub, and install the `rmp` binary to `/usr/local/bin`. If `rmp` is already installed, it will be updated to the latest version.

## Features

- **Roadmap Management**: Create, list, select, and remove roadmaps
- **Task Management**: Create, edit, list tasks with status, priority, and severity tracking
- **Task Prioritization**: Get next tasks from open sprint ordered by severity and priority
- **Sprint Management**: Organize tasks into sprints with complete lifecycle (PENDING, OPEN, CLOSED)
- **Sprint Reporting**: Comprehensive sprint reports with progress and distribution metrics
- **Task Ordering**: Reorder, move-to-position, swap, top, and bottom commands for sprint task management
- **Audit Trail**: Automatic logging of all operations for traceability
- **State Machine**: Validated task and sprint status transitions with automatic date tracking
- **Bulk Operations**: Support for multiple task IDs in single commands

## Available Commands

| Command | Description | Documentation |
|---------|-------------|---------------|
| `roadmap` | Roadmap management (create, list, select, remove) | [DOCS/commands/roadmap.md](DOCS/commands/roadmap.md) |
| `task` | Task management (create, edit, list, status, priority, severity, next) | [DOCS/commands/task.md](DOCS/commands/task.md) |
| `sprint` | Sprint management with lifecycle control, reporting, and task ordering | [DOCS/commands/sprint.md](DOCS/commands/sprint.md) |
| `audit` | Audit log and entity history | [DOCS/commands/audit.md](DOCS/commands/audit.md) |

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

### Claude Code Skills

Install the `roadmap-coordinator` skill to enable task coordination via Claude Code:

**Project-level (current directory only):**
```bash
cp -r .claude/skills/roadmap-coordinator ~/.claude/skills/
```

**User-level (all projects):**
```bash
cp -r .claude/skills/roadmap-coordinator ~/.claude/skills/
```

**Global (system-wide):**
```bash
sudo cp -r .claude/skills/roadmap-coordinator /usr/local/share/claude/skills/
```

Or install directly from GitHub:
```bash
mkdir -p ~/.claude/skills/
curl -fsSL https://raw.githubusercontent.com/FlavioCFOliveira/Groadmap/main/.claude/skills/roadmap-coordinator/roadmap-coordinator.md -o ~/.claude/skills/roadmap-coordinator/roadmap-coordinator.md
```

## Quick Start

```bash
# Create a new roadmap
rmp roadmap create myproject

# Select default roadmap
rmp roadmap use myproject

# Create a task
rmp task create -t "Implement feature X" \
  -fr "User can perform X action" \
  -tr "Develop code using Go" \
  -ac "Feature working in production"

# List tasks
rmp task list

# Get next task from open sprint (ordered by severity/priority)
rmp task next

# Create a sprint
rmp sprint create -d "Sprint 1 - Setup"

# Add tasks to sprint
rmp sprint add-tasks 1 1,2,3

# Start sprint
rmp sprint start 1

# Reorder tasks in sprint
rmp sprint reorder 1 3,1,2

# Show sprint report
rmp sprint show 1
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
- `SPEC/ARCHITECTURE.md` - System design and architecture
- `SPEC/BUILD.md` - Build system, CI/CD workflows, and cross-compilation
- `SPEC/COMMANDS.md` - CLI hierarchy and aliases
- `SPEC/DATABASE.md` - SQLite schema and migrations
- `SPEC/DATA_FORMATS.md` - JSON output schema
- `SPEC/DEPLOY.md` - Installation, deployment, and platform detection
- `SPEC/MODELS.md` - Model definitions
- `SPEC/STATE_MACHINE.md` - State machines
- `SPEC/VERSION.md` - Version management strategy

## License

MIT License - see [LICENSE](LICENSE) for details.
