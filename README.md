# Groadmap

Local Roadmap Manager CLI for agentic workflows. Groadmap is a CLI tool in Go for managing technical roadmaps, using SQLite as backend.

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/FlavioCFOliveira/Groadmap/main/install.sh | bash
```

This will detect your OS and architecture, download the latest release from GitHub, and install the `rmp` binary to `/usr/local/bin`. If `rmp` is already installed, it will be updated to the latest version.

## Features

- **Roadmap Management**: Create, list, select, and remove roadmaps
- **Task Management**: Create, edit, list, and get tasks with status, priority, and severity tracking
- **Task Prioritization**: Get next tasks from open sprint ordered by sprint task order
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
| `task` | Task management (create, edit, list, get, next, status, priority, severity) | [DOCS/commands/task.md](DOCS/commands/task.md) |
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

# Get specific task(s)
rmp task get 1
rmp task get 1,2,3

# Get next task from open sprint (ordered by sprint task order)
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

## Exit Codes

| Code | Meaning | Description |
|------|---------|-------------|
| 0 | Success | Command completed successfully |
| 1 | General error | Database failure, unexpected error |
| 2 | Invalid usage | Wrong arguments, syntax error |
| 3 | No roadmap | No roadmap selected for command |
| 4 | Not found | Roadmap/task/sprint doesn't exist |
| 5 | Already exists | Duplicate name when creating |
| 6 | Invalid data | Validation failed (dates, ranges) |
| 127 | Unknown command | Unknown command or subcommand |

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

## FAQ

### Planning and Structure

**What is the active roadmap?**
```bash
rmp roadmap list
rmp roadmap use <name>
```

**What is in the backlog?**
```bash
rmp task list --status BACKLOG
```

**What sprints exist?**
```bash
rmp sprint list
rmp sprint list --status OPEN
rmp sprint list --status PENDING,CLOSED
```

**How do I create a new roadmap?**
```bash
rmp roadmap create <name>
```

**How do I remove a roadmap?**
```bash
rmp roadmap remove <name>
rmp roadmap rm <name>         # Alias
rmp roadmap delete <name>     # Alias
```
- This permanently deletes the roadmap database file
- Cannot remove the currently active roadmap

**How do I switch between roadmaps?**
```bash
rmp roadmap use <name>
```
- Sets the specified roadmap as the default for all subsequent commands

---

### Task Management

**How do I create a well-defined task?**
```bash
rmp task create -t "Title" \
  -fr "Functional requirements - Why build it?" \
  -tr "Technical requirements - How to build it?" \
  -ac "Acceptance criteria - How to verify it?" \
  --type TASK --priority 5 --severity 3
```

**What is the next task to work on?**
```bash
rmp task next              # Next task
rmp task next -n 5         # Next 5 tasks
```

**What is the status of task X?**
```bash
rmp task get <id>
rmp task get 1,2,3         # Multiple tasks (bulk)
```

**What are the high priority tasks?**
```bash
rmp task list --priority 8,9
```

**What are the critical bugs?**
```bash
rmp task list --type BUG --severity 8,9
```

**How do I edit a task?**
```bash
rmp task edit <id> -t "New title" -p 9
```

**How do I filter tasks by type?**
```bash
rmp task list --type TASK
rmp task list --type BUG
rmp task list --type USER_STORY
```

**What task types are available?**
- `TASK` - General task
- `USER_STORY` - User story
- `BUG` - Bug report
- `SUB_TASK` - Sub-task
- `EPIC` - Epic (large body of work)
- `REFACTOR` - Code refactoring
- `CHORE` - Maintenance task
- `SPIKE` - Research/exploration task
- `DESIGN_UX` - Design or UX work
- `IMPROVEMENT` - Enhancement to existing feature

**How do I combine multiple filters in task list?**
```bash
rmp task list --status BACKLOG --priority 8,9
rmp task list --type BUG --severity 9 --status SPRINT
```

**How do I remove a task?**
```bash
rmp task remove <id>
rmp task rm <id>              # Alias
```

---

### Sprint Lifecycle

**How do I create a new sprint?**
```bash
rmp sprint create -d "Sprint X - Description"
```

**How do I add tasks to a sprint?**
```bash
rmp sprint add-tasks <sprint_id> <task_ids>
rmp sprint add-tasks 1 1,2,3,4
```

**How do I define the execution order of tasks?**
```bash
rmp sprint reorder <sprint_id> <order>
rmp sprint reorder 1 3,1,2     # Task 3 first, then 1, then 2
```

**How do I adjust the position of a specific task?**
```bash
rmp sprint move-to <sprint_id> <task_id> <position>
rmp sprint swap <sprint_id> <task1> <task2>
rmp sprint top <sprint_id> <task_id>
rmp sprint bottom <sprint_id> <task_id>
```

**How do I start a sprint?**
```bash
rmp sprint start <id>
```

**How do I move tasks between sprints?**
```bash
rmp sprint move-tasks <from_sprint> <to_sprint> <task_ids>
rmp sprint move-tasks 1 2 5,6,7
```

**How do I remove tasks from a sprint?**
```bash
rmp sprint remove-tasks <sprint_id> <task_ids>
```

**How do I see the detailed status of a sprint?**
```bash
rmp sprint show <id>
```

**How do I get sprint statistics?**
```bash
rmp sprint stats <id>
```

**How do I list only the tasks in a sprint?**
```bash
rmp sprint tasks <id>
rmp sprint tasks <id> --status DOING
```

**How do I close a sprint?**
```bash
rmp sprint close <id>
```

**How do I reopen a closed sprint?**
```bash
rmp sprint reopen <id>
```

**How do I remove a sprint?**
```bash
rmp sprint remove <id>
rmp sprint rm <id>              # Alias
```
- Tasks in the sprint return to BACKLOG status
- Sprint must be CLOSED before removal

**How do I update a sprint's description?**
```bash
rmp sprint update <id> -d "New description"
rmp sprint upd <id> -d "New description"   # Alias
```

**Can I have multiple open sprints?**
No. Only one sprint can be OPEN at a time. You must close the current sprint before starting another.

---

### Work Execution

**How do I start working on a task?**
```bash
rmp task stat <id> DOING      # Automatically sets started_at
```

**How do I mark a task as ready for testing?**
```bash
rmp task stat <id> TESTING    # Automatically sets tested_at
```

**What do I do if a task fails testing?**
```bash
rmp task stat <id> DOING      # Returns to development
```

**How do I complete a task?**
```bash
rmp task stat <id> COMPLETED  # Automatically sets closed_at
```

**How do I change a task's priority?**
```bash
rmp task prio <id> <0-9>
```

**How do I change a task's severity?**
```bash
rmp task sev <id> <0-9>
```

**How do I reopen a completed task?**
```bash
rmp task stat <id> BACKLOG    # Returns to backlog, clears tracking dates
```

---

### Visibility and Reporting

**How is the sprint going?**
```bash
rmp sprint show <id>          # Complete report with distributions
```

**How many tasks are in each status?**
```bash
rmp sprint stats <id>
```

**How do I see a task's history?**
```bash
rmp audit history --entity-type TASK --id <id>
```

**What happened recently?**
```bash
rmp audit list --today
rmp audit list --limit 20
```

**How do I view all audit operations?**
```bash
rmp audit list
rmp audit list --entity-type SPRINT
```

**How do I see audit statistics?**
```bash
rmp audit stats
```

**What task statuses are available for filtering?**
- `BACKLOG` - Not in any sprint
- `SPRINT` - In a sprint but not started
- `DOING` - Currently being worked on
- `TESTING` - Ready for or in testing
- `COMPLETED` - Finished

**How do I list tasks in a specific status?**
```bash
rmp task list --status DOING
rmp task list --status TESTING,COMPLETED
rmp task list --status BACKLOG,SPRINT    # All non-active tasks
```

---

### Important Distinctions

**What is the difference between Priority and Severity?**

- **Priority (0-9)**: Product Owner perspective (business urgency)
  - Set with: `rmp task prio <id> <value>`
  - Filter: `rmp task list --priority 8,9`

- **Severity (0-9)**: Technical team perspective (technical impact)
  - Set with: `rmp task sev <id> <value>`
  - Filter: `rmp task list --severity 8,9`

**What is the difference between sprint order and priority?**

**Sprint order** is defined manually by the team and determines execution sequence:
```bash
rmp sprint reorder 1 3,1,2     # Task 3 first, then 1, then 2
rmp task next                  # Returns in defined order
```

**Priority** is a task attribute used for filtering and planning:
```bash
rmp task list --priority 9     # List high priority tasks
```

**Where is the data stored?**

Roadmaps are stored in `~/.roadmaps/` with restricted permissions (0700):
- Each roadmap is an independent SQLite file
- No external infrastructure or cloud services required
- Completely local data

**How do I get help on a specific command?**
```bash
rmp --help                       # General help
rmp task --help                  # Task command help
rmp task create --help           # Specific subcommand help
rmp sprint --help                # Sprint command help
```

**What are the shorthand aliases for commands?**
- `rmp t` = `rmp task`
- `rmp s` = `rmp sprint`
- `rmp road` = `rmp roadmap`
- `rmp aud` = `rmp audit`
- `rmp ls` = `rmp list` (for list subcommands)
- `rmp rm` = `rmp remove` (for remove subcommands)
- `rmp new` = `rmp create` (for create subcommands)

**What if I get "No roadmap selected" error?**
```bash
rmp roadmap list                 # See available roadmaps
rmp roadmap use <name>           # Select a roadmap
```

**How are dates tracked automatically?**
- `started_at` - Set when task moves to DOING
- `tested_at` - Set when task moves to TESTING
- `closed_at` - Set when task moves to COMPLETED
- These are cleared when a task returns to BACKLOG

## License

MIT License - see [LICENSE](LICENSE) for details.
