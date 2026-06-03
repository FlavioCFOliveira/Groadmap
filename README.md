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
- **Knowledge Graph**: Per-roadmap queryable graph (nodes, edges, Cypher) for capturing project elements and their relationships
- **Web Interface**: Read-only, self-contained, mobile-first browser view of all roadmaps, their tasks and sprints, and an interactive knowledge-graph visualisation, built on the Tabler admin-shell UI in a dark theme (`rmp web`)

## Available Commands

| Command | Description | Documentation |
|---------|-------------|---------------|
| `roadmap` | Roadmap management (create, list, select, remove) | [DOCS/commands/roadmap.md](DOCS/commands/roadmap.md) |
| `task` | Task management (create, edit, list, get, next, status, priority, severity) | [DOCS/commands/task.md](DOCS/commands/task.md) |
| `sprint` | Sprint management with lifecycle control, reporting, and task ordering | [DOCS/commands/sprint.md](DOCS/commands/sprint.md) |
| `audit` | Audit log and entity history | [DOCS/commands/audit.md](DOCS/commands/audit.md) |
| `graph` | Knowledge graph management (create, query, update, delete, search via Cypher) | [DOCS/commands/graph.md](DOCS/commands/graph.md) |
| `web` | Read-only, self-contained web interface for all roadmaps and their knowledge graphs | [DOCS/commands/web.md](DOCS/commands/web.md) |
| `ai-help` | Emit the AI Agent Contract (machine-readable JSON for automated callers) | [DOCS/commands/ai-help.md](DOCS/commands/ai-help.md) |

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

# Record project knowledge in the roadmap's graph
rmp graph create -r myproject \
  --query "MERGE (s:Spec {key:'user-authentication'}) MERGE (c:Code {path:'internal/auth/jwt.go'}) MERGE (s)-[:IMPLEMENTED_BY]->(c)"

# Query the graph
rmp graph query -r myproject \
  --query "MATCH (s:Spec)-[:IMPLEMENTED_BY]->(c:Code) RETURN s.key, c.path"
```

## Project Structure

```
.
├── cmd/rmp/main.go          # CLI entry point
├── internal/
│   ├── commands/            # Subcommands (roadmap, task, sprint, audit, graph)
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
- **Roadmaps**: Each roadmap is a directory `~/.roadmaps/<name>/` (permissions `0700`) holding its SQLite database `project.db` (permissions `0600`)
- **Knowledge graph**: Each roadmap may hold a graph store under `~/.roadmaps/<name>/graph/` (a directory, permissions `0700`), created on first use of the `graph` command

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
- `SPEC/GRAPH.md` - Knowledge graph feature: GoGraph integration, persistence, guard rails
- `SPEC/MODELS.md` - Model definitions
- `SPEC/STATE_MACHINE.md` - State machines
- `SPEC/VERSION.md` - Version management strategy

## FAQ

### Planning and Structure

**What is the active roadmap?**
```bash
rmp roadmap list               # List all roadmaps
rmp roadmap use <name>         # Set active roadmap
```

**How do I create and switch roadmaps?**
```bash
rmp roadmap create <name>      # Create new roadmap
rmp roadmap use <name>         # Set as active for all subsequent commands
rmp roadmap remove <name>      # Permanently delete (cannot remove active roadmap)
```

**What is the overall state of the roadmap?**
```bash
rmp stats                      # Sprint counts, task distribution by status, average velocity
rmp stats -r <name>            # Specific roadmap
```

**What is in the backlog?**
```bash
rmp backlog list               # All backlog tasks, sorted by priority
rmp backlog show-next 10       # Top 10 backlog tasks for sprint planning
rmp backlog list --type BUG    # Filter by type
rmp backlog list --priority 7  # Filter by minimum priority
```

**What sprints exist?**
```bash
rmp sprint list
rmp sprint list --status OPEN
rmp sprint list --status PENDING,CLOSED
```

---

### Task Management

**How do I create a well-defined task?**
```bash
rmp task create \
  -t "Title" \
  -fr "Functional requirements - Why build it?" \
  -tr "Technical requirements - How to build it?" \
  -ac "Acceptance criteria - How to verify it?" \
  --type USER_STORY --priority 7 --severity 3 \
  --specialists "go-elite-developer,qa-engineer"
```

**What task types are available?**
- `USER_STORY` - New feature from the user's perspective
- `TASK` - Internal work unit (setup, configuration)
- `BUG` - Something not working as expected
- `SUB_TASK` - Decomposition of a story or task
- `EPIC` - Large body of work spanning multiple sprints
- `REFACTOR` - Structural improvement without behaviour change
- `CHORE` - Maintenance (update dependencies, cleanup)
- `SPIKE` - Research or prototype to reduce uncertainty
- `DESIGN_UX` - Wireframes, prototypes, interface flows
- `IMPROVEMENT` - Refinement of an existing working feature

**How do I get a task or multiple tasks?**
```bash
rmp task get 5
rmp task get 1,2,3             # Bulk fetch
```

**How do I filter and search tasks?**
```bash
rmp task list --status BACKLOG
rmp task list --status DOING,TESTING
rmp task list --type BUG --severity 8,9
rmp task list --priority 7 --status SPRINT
rmp task list --specialists "go-elite-developer"
rmp task list --created-since 2026-03-01
rmp task list --sort created --limit 50
```

**What is the next task to work on?**
```bash
rmp task next                  # Next task from active sprint (by sprint order)
rmp task next 5                # Next 5 tasks
```

**How do I edit a task?**
```bash
rmp task edit <id> -t "New title" --priority 9 --type BUG
```

**How do I delete a task?**
```bash
rmp task remove <id>
rmp task rm 1,2,3              # Bulk delete (tasks must be in BACKLOG)
```
- Task must be in `BACKLOG` status and have no subtasks.

---

### Sub-task Hierarchy

**How do I break a task into subtasks?**
```bash
rmp task create -t "Write unit tests" \
  -fr "..." -tr "..." -ac "..." \
  --type SUB_TASK --parent 42
```
- The parent task's `subtask_count` is updated automatically.

**How do I list the subtasks of a task?**
```bash
rmp task subtasks 42           # Direct subtasks of task 42, ordered by priority
```

---

### Task Dependencies

**How do I declare that a task depends on another?**
```bash
rmp task add-dep 10 5          # Task 10 depends on task 5 (task 5 must complete first)
rmp task remove-dep 10 5       # Remove that dependency
```
- Circular dependencies are rejected.

**How do I see what is blocking a task?**
```bash
rmp task blockers 10           # Tasks that task 10 depends on and are not yet COMPLETED
```

**How do I see what a task is blocking?**
```bash
rmp task blocking 5            # Tasks that depend on task 5 and are waiting for it
```

---

### Sprint Lifecycle

**How do I create a sprint with capacity control?**
```bash
rmp sprint create -d "Sprint 5 - Auth module"
rmp sprint create -d "Sprint 5 - Auth module" --max-tasks 8   # Cap at 8 tasks
```

**How do I update a sprint?**
```bash
rmp sprint update <id> -d "New description"
rmp sprint update <id> --max-tasks 10        # Update capacity limit
rmp sprint upd <id> -d "New description" --max-tasks 10
```

**How do I add tasks to a sprint?**
```bash
rmp sprint add-tasks 1 5,8,12,15
```
- Tasks move from `BACKLOG` to `SPRINT` automatically.
- Rejected if the sprint is at `max_tasks` capacity.

**How do I define the execution order of tasks?**
```bash
rmp sprint reorder 1 3,1,2        # Task 3 first, then 1, then 2
rmp sprint move-to 1 8 0          # Move task 8 to position 0 (top)
rmp sprint top 1 8                # Same as above, shorthand
rmp sprint bottom 1 8             # Move task 8 to last position
rmp sprint swap 1 3 7             # Swap positions of tasks 3 and 7
```

**How do I start a sprint?**
```bash
rmp sprint start <id>
```
- Only one sprint can be `OPEN` at a time.

**How do I move tasks between sprints?**
```bash
rmp sprint move-tasks 1 2 5,6,7   # Move tasks 5, 6, 7 from sprint 1 to sprint 2
```

**How do I remove tasks from a sprint?**
```bash
rmp sprint remove-tasks 1 5,6     # Tasks return to BACKLOG
```

**How do I close a sprint?**
```bash
rmp sprint close <id>
rmp sprint close <id> --force     # Bypass active-task check
```

**How do I reopen a closed sprint?**
```bash
rmp sprint reopen <id>
```

**How do I remove a sprint?**
```bash
rmp sprint remove <id>
rmp sprint rm <id>
```
- Sprint must be `CLOSED`. Tasks return to `BACKLOG`.

**Can I have multiple open sprints?**
No. Only one sprint can be `OPEN` at a time. Close the current sprint before starting another.

---

### Work Execution

**How do I start working on a task?**
```bash
rmp task stat <id> DOING          # Sets started_at automatically
```

**How do I mark a task as ready for testing?**
```bash
rmp task stat <id> TESTING        # Sets tested_at automatically
```

**What do I do if a task fails testing?**
```bash
rmp task stat <id> DOING          # Return to development
```

**How do I complete a task?**
```bash
rmp task stat <id> COMPLETED
rmp task stat <id> COMPLETED --summary "Implemented OAuth2 with PKCE flow"
```
- `--summary` is optional (max 4096 chars) and only valid on the `TESTING → COMPLETED` transition.
- `closed_at` is set automatically.

**How do I bulk-change task status?**
```bash
rmp task stat 1,2,3 TESTING
```

**How do I reopen a completed task?**
```bash
rmp task reopen <id>              # Returns to BACKLOG, clears all lifecycle timestamps
rmp task reopen 1,2,3             # Bulk reopen
```

**How do I change priority or severity?**
```bash
rmp task prio <id> 9              # Priority 0-9
rmp task sev <id> 8               # Severity 0-9
rmp task prio 1,2,3 7             # Bulk update
```

**How do I assign or remove specialists?**
```bash
rmp task assign <id> "go-elite-developer"
rmp task unassign <id> "go-elite-developer"
```

---

### Visibility and Reporting

**How is the sprint going? (comprehensive view)**
```bash
rmp sprint show <id>
```
Returns: status, task summary (pending/in-progress/completed), progress percentages, severity distribution, criticality distribution, task order, current load, and capacity percentage.

**How many tasks are in each status? (metrics and velocity)**
```bash
rmp sprint stats <id>
```
Returns: total tasks, completed tasks, progress percentage, status distribution, task order, velocity (tasks/day, CLOSED sprints only), days elapsed, and burndown series.

**What tasks are still open in the sprint?**
```bash
rmp sprint open-tasks <id>                    # SPRINT + DOING + TESTING only
rmp sprint open-tasks <id> --order-by-priority
```

**How do I list all tasks in a sprint?**
```bash
rmp sprint tasks <id>
rmp sprint tasks <id> --status DOING
rmp sprint tasks <id> --order-by-priority
```

**How do I see a task's full audit history?**
```bash
rmp audit history --entity-type TASK --entity-id <id>
```

**What happened recently?**
```bash
rmp audit list --today
rmp audit list --limit 20
rmp audit list --since 2026-03-20
rmp audit list --entity-type SPRINT
```

**How do I see audit statistics?**
```bash
rmp audit stats
rmp audit stats --since 2026-03-01 --until 2026-03-31
```

---

### Knowledge Graph

**What is the knowledge graph?**

Each roadmap owns one free-form, queryable graph backed by GoGraph. It captures the project's elements (specs, code, decisions, dependencies) and the relationships between them, so an AI agent can answer questions about the project without re-reading every source file. The graph is independent of the SQLite tasks/sprints data and is accessed through Cypher.

**How do I record knowledge in the graph?**
```bash
rmp graph create -r myproject \
  --query "MERGE (s:Spec {key:'user-authentication'}) MERGE (c:Code {path:'internal/auth/jwt.go'}) MERGE (s)-[:IMPLEMENTED_BY]->(c)"
```

**How do I read or traverse the graph?**
```bash
rmp graph query -r myproject \
  --query "MATCH (s:Spec)-[:IMPLEMENTED_BY]->(c:Code) RETURN s.key, c.path"
rmp graph search -r myproject \
  --query "MATCH path = (s:Spec {key:'user-authentication'})-[:DEPENDS_ON*1..3]->(d:Dependency) RETURN path"
```

**How do I update or delete graph elements?**
```bash
rmp graph update -r myproject \
  --query "MATCH (s:Spec {key:'user-authentication'}) SET s.status = 'implemented'"
rmp graph delete -r myproject \
  --query "MATCH (d:Decision {key:'use-sessions'}) DETACH DELETE d"
```

**What are the five graph subcommands?**

Each subcommand is a guard rail that accepts only Cypher whose operation class matches it, rejecting everything else (exit code 6) before execution:
- `create` — add nodes/edges (`CREATE` / `MERGE`)
- `query` — read (`MATCH ... RETURN`, read-only)
- `update` — mutate existing elements (`SET` / `REMOVE`)
- `delete` — remove nodes/edges (`DELETE` / `DETACH DELETE`)
- `search` — read-only traversal, including variable-length paths (e.g. `-[*1..3]-`)

**Can I pipe a query instead of using `--query`?**
```bash
echo "MATCH (n) RETURN count(n)" | rmp graph query -r myproject
cat query.cypher | rmp graph search -r myproject
```
When `--query` is absent, the entire standard input is read as the query. See [DOCS/commands/graph.md](DOCS/commands/graph.md) for full details.

**Where is the graph stored?**

Under the roadmap's home directory at `~/.roadmaps/<name>/graph/` (a directory, permissions `0700`), created on first use of any `graph` subcommand. Removing the roadmap deletes its graph along with the rest of the home directory.

---

### Web Interface

**What is `rmp web`?**

A read-only, browser-based view of everything the CLI manages. It starts an HTTP server embedded in the `rmp` binary that lists every roadmap under `~/.roadmaps/` and lets you drill into a roadmap's tasks and sprints and an interactive visualisation of its knowledge graph. It only presents data; the CLI remains the sole write path.

```bash
# Start on the default host (all interfaces) and port and open the browser
rmp web

# Start without launching a browser; just print the served URL
rmp web --no-open

# Choose a port, or restrict to the local machine (loopback opt-in)
rmp web --port 9000
rmp web --host 127.0.0.1 --port 9000
```

On startup the served URL is printed as JSON (`{"url": "http://0.0.0.0:8787"}`); the `url` reflects the actual bound host and port. When `--port` is omitted and `8787` is in use, the server falls back to an OS-chosen ephemeral port so it still starts.

**What makes it different from every other command?**

- **Read-only.** No route creates, edits, or deletes anything; serving a page writes no rows, no audit-log entry, and never checkpoints the graph store. Only `GET`/`HEAD` are accepted (any other method returns HTTP 405).
- **No `-r` flag.** It is the one command exempt from the always-required-roadmap rule; it lists all roadmaps and you pick one in the browser.
- **Long-lived.** It keeps serving until interrupted; `Ctrl+C` (`SIGINT`) or `SIGTERM` shuts it down gracefully (exit 0).
- **Tabler dark-theme UI.** The interface is built on the vendored Tabler admin-dashboard framework in its dark theme: a navigation sidebar (which collapses to a hamburger menu on small viewports), a top navbar, page headers, and Tabler cards, tables, and badges.
- **Self-contained and offline.** Every asset (HTML, CSS, JavaScript, the vendored Tabler framework and D3.js graph library with the d3-sankey plugin, the Tabler Icons webfont, and the Inter font) is embedded in the binary via `go:embed` and served only from `/static/`; no page references a CDN, a remote font host, or any other remote origin, and the server makes no outbound request.
- **Exposed to the network by default.** It binds all interfaces (`0.0.0.0`), so the read-only interface is reachable from every network point of the machine; restricting it to the local machine via `--host 127.0.0.1` is the explicit opt-in. Roadmap names from the URL are validated before any filesystem path is built (path-traversal guard).
- **Responsive, mobile-first.** Every page, including the graph visualisation, adapts to small touch viewports.

See [DOCS/commands/web.md](DOCS/commands/web.md) for the full route list and exit codes.

---

### Important Distinctions

**What is the difference between Priority and Severity?**

- **Priority (0-9)**: business urgency, set by the Product Owner.
  - `rmp task prio <id> 9` / filter: `--priority 8,9`
- **Severity (0-9)**: technical impact, set by the engineering team.
  - `rmp task sev <id> 8` / filter: `--severity 8,9`

Both scales run 0 (lowest) to 9 (highest). Use them independently.

**What is the difference between `sprint show` and `sprint stats`?**

- `sprint show` — operational view: task composition, severity/criticality distributions, capacity load.
- `sprint stats` — metric view: velocity, burndown, days elapsed, progress percentage.

**What is the difference between sprint order and task priority?**

**Sprint order** controls execution sequence within a sprint — the order `rmp task next` returns tasks:
```bash
rmp sprint reorder 1 5,3,1,2
rmp task next                   # Returns task 5 first
```

**Priority** is a planning attribute for filtering and backlog grooming:
```bash
rmp backlog show-next 5         # Top 5 by priority for sprint planning
rmp task list --priority 8,9
```

**Where is the data stored?**

In `~/.roadmaps/` with permissions `0700`. Each roadmap is an independent directory `~/.roadmaps/<name>/` holding its own SQLite database at `project.db`. Legacy single-file roadmaps are migrated automatically on first use. No external services or cloud required.

**How do I get help on any command?**
```bash
rmp --help
rmp task --help
rmp task create --help
rmp sprint --help
rmp sprint show --help
```

---

### For AI Agents

Groadmap exposes a machine-readable contract that fully describes the CLI surface in a single JSON document. AI agents and automated callers should fetch this contract once and use it as the canonical reference instead of scraping `--help` output.

**Fetch the whole contract:**
```bash
rmp --ai-help        # global flag form
rmp ai-help          # equivalent top-level command
```

**Scope to one command or subcommand:**
```bash
rmp task --ai-help              # one command and all its subcommands
rmp task create --ai-help       # a single subcommand
```

The contract is pretty-printed JSON on stdout (2-space indent, trailing newline, UTF-8) with `schema_version`, `tool`, `conventions`, `exit_codes`, `enums`, `global_flags`, `commands`, `common_workflows`, and `pitfalls`. The JSON shape is specified in `SPEC/DATA_FORMATS.md § AI Agent Contract`. `--ai-help` wins over any other action flag: combining it with `task create` flags emits the contract and persists nothing.

**Discoverability surfaces for agents that did not start at the contract:**

- Every `--help` output is prepended with `AI agents: run \`rmp --ai-help\` for a machine-readable command contract.` as its first line.
- Every `Error: ...` line on stderr is followed by the same hint after a blank line.
- Setting `AI_AGENT=1` in the environment prepends the same hint as the first line of stderr on every invocation. Only the literal string `1` enables this; other values (including `true`, `yes`, `0`) are silent. When `AI_AGENT=1` is active and an error occurs, the hint is emitted exactly once at the top.

**What are the shorthand aliases?**
- `rmp t` = `rmp task`
- `rmp s` = `rmp sprint`
- `rmp road` = `rmp roadmap`
- `rmp aud` = `rmp audit`
- `rmp new` = `rmp create`
- `rmp ls` = `rmp list`
- `rmp rm` = `rmp remove`
- `rmp upd` = `rmp update`

**What if I get "No roadmap selected" error?**
```bash
rmp roadmap list
rmp roadmap use <name>
```

**How are timestamps tracked automatically?**
- `started_at` — set when task moves to `DOING`
- `tested_at` — set when task moves to `TESTING`
- `closed_at` — set when task moves to `COMPLETED`
- All three are cleared when a task is reopened to `BACKLOG`

## License

MIT License - see [LICENSE](LICENSE) for details.
