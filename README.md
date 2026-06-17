# Groadmap

Local Roadmap Manager CLI for agentic workflows. Groadmap is a CLI tool in Go for managing technical roadmaps, using SQLite as backend.

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/FlavioCFOliveira/Groadmap/main/install.sh | bash
```

This will detect your OS and architecture, download the latest release from GitHub, and install the `rmp` binary to `/usr/local/bin`. If `rmp` is already installed, it will be updated to the latest version.

## Features

- **Roadmap Management**: Create, list, and remove roadmaps
- **Task Management**: Create, edit, list, and get tasks with status, priority, and severity tracking
- **Task Prioritization**: Get next tasks from the open sprint, ordered by sprint task order
- **Sub-tasks and Dependencies**: Decompose tasks into sub-tasks and declare blocking dependencies between tasks
- **Sprint Management**: Organize tasks into sprints with a complete lifecycle (PENDING, OPEN, CLOSED) and a unique execution order
- **Sprint Reporting**: Comprehensive sprint reports with progress and distribution metrics
- **Task Ordering**: Reorder, move-to-position, swap, top, and bottom commands for sprint task management
- **Backlog and Statistics**: Backlog planning views and roadmap-wide statistics with velocity
- **Audit Trail**: Automatic logging of all operations for traceability
- **State Machine**: Validated task and sprint status transitions with automatic date tracking
- **Bulk Operations**: Support for multiple task IDs in single commands
- **Knowledge Graph**: Per-roadmap queryable graph (nodes, edges, Cypher) for capturing project elements and their relationships
- **Web Interface**: Read-only, self-contained, mobile-first browser view of all roadmaps, their tasks and sprints, and an interactive knowledge-graph visualisation, built on the Tabler admin-shell UI in a dark theme (`rmp web`)

## Roadmap Selection (Always Required)

Every command operates on a single roadmap, selected with the `-r`/`--roadmap` flag. **There is no default or active roadmap.** Omitting the flag on a command that needs it fails with exit code 3 (`Error: no roadmap selected: use -r <name> or --roadmap <name>`).

The only commands that do **not** take `-r` are:

- `rmp roadmap list`, `rmp roadmap create`, `rmp roadmap remove` (they operate on the roadmap set itself)
- `rmp web` (it lists every roadmap and you pick one in the browser)
- `rmp ai-help` / `rmp --ai-help`, `rmp --help`, `rmp --version`

## Available Commands

| Command | Description | Documentation |
|---------|-------------|---------------|
| `roadmap` | Roadmap management (create, list, remove) | [DOCS/commands/roadmap.md](DOCS/commands/roadmap.md) |
| `task` | Task management (create, edit, list, get, next, status, priority, severity, dependencies) | [DOCS/commands/task.md](DOCS/commands/task.md) |
| `sprint` | Sprint management with lifecycle control, reporting, and task ordering | [DOCS/commands/sprint.md](DOCS/commands/sprint.md) |
| `backlog` | Backlog planning views (list and show-next) | [DOCS/commands/backlog.md](DOCS/commands/backlog.md) |
| `stats` | Roadmap-wide statistics and velocity | [DOCS/commands/stats.md](DOCS/commands/stats.md) |
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

### Claude Code Skill

Agentic workflows drive Groadmap through the `roadmap-manager` skill, which plans and audits roadmaps, sprints, and tasks entirely through the `rmp` CLI (its SQLite database is the single source of truth). This skill is provided by your global Claude Code configuration; it is not bundled inside this repository, so there is nothing to copy from here. When the skill is available, refer to the project's roadmap simply by asking about tasks, sprints, the backlog, or what to work on next, and the skill issues the appropriate `rmp` commands.

## Quick Start

```bash
# Create a new roadmap
rmp roadmap create myproject

# List roadmaps
rmp roadmap list

# Create a task (every task/sprint command requires -r <roadmap>)
rmp task create -r myproject \
  -t "Implement feature X" \
  -fr "User can perform X action" \
  -tr "Develop code using Go" \
  -ac "Feature working in production"

# List tasks
rmp task list -r myproject

# Get specific task(s)
rmp task get -r myproject 1
rmp task get -r myproject 1,2,3

# Get next task from the open sprint (ordered by sprint task order)
rmp task next -r myproject

# Create a sprint
rmp sprint create -r myproject -t "Setup" -d "Sprint 1 - Setup"

# Add tasks to the sprint
rmp sprint add-tasks -r myproject 1 1,2,3

# Start the sprint
rmp sprint start -r myproject 1

# Reorder tasks in the sprint
rmp sprint reorder -r myproject 1 3,1,2

# Show the sprint report
rmp sprint show -r myproject 1

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
│   ├── commands/            # Subcommands (roadmap, task, sprint, backlog, audit, graph, stats)
│   ├── db/                  # SQLite, schema, parameterized queries
│   ├── models/              # Structs and enums
│   ├── web/                 # Read-only embedded web interface
│   └── utils/               # JSON, ISO 8601 dates, paths
├── bin/                     # Build output
├── SPEC/                    # Technical specifications
└── DOCS/                    # Command documentation
```

## Conventions

- **Roadmap selection**: every command (except `roadmap list/create/remove`, `web`, and `ai-help`) requires `-r <roadmap>`; there is no default roadmap
- **Success output**: JSON to stdout
- **Error output**: Plain text to stderr
- **Dates**: ISO 8601 UTC (with milliseconds, suffix `Z`)
- **List arguments**: comma-separated, no spaces (e.g. `1,2,3`)
- **Roadmaps**: Each roadmap is a directory `~/.roadmaps/<name>/` (permissions `0700`) holding its SQLite database `project.db` (permissions `0600`)
- **Knowledge graph**: Each roadmap may hold a graph store under `~/.roadmaps/<name>/graph/` (a directory, permissions `0700`), created on first use of the `graph` command

## Exit Codes

| Code | Meaning | Description |
|------|---------|-------------|
| 0 | Success | Command completed successfully |
| 1 | General error | Database failure, unexpected error |
| 2 | Invalid usage | Wrong arguments, syntax error |
| 3 | No roadmap | No roadmap provided via `-r` for a command that requires it |
| 4 | Not found | Roadmap/task/sprint doesn't exist |
| 5 | Already exists | Duplicate name or duplicate sprint order |
| 6 | Invalid data | Validation failed (dates, ranges) |
| 127 | Unknown command | Unknown command or subcommand |

## Technical Documentation

See the `SPEC/` folder for detailed technical documentation:
- `SPEC/ARCHITECTURE.md` - System design and architecture
- `SPEC/BUILD.md` - Build system, CI/CD workflows, and cross-compilation
- `SPEC/COMMANDS.md` - CLI hierarchy and aliases
- `SPEC/DATABASE.md` - SQLite schema and migrations
- `SPEC/DATA_FORMATS.md` - JSON output schema and the AI Agent Contract
- `SPEC/DEPLOY.md` - Installation, deployment, and platform detection
- `SPEC/GRAPH.md` - Knowledge graph feature: GoGraph integration, persistence, guard rails
- `SPEC/HELP.md` - Help skeleton and error message format
- `SPEC/IMPLEMENTATION.md` - Concurrency, caching, and performance strategies
- `SPEC/MODELS.md` - Model definitions
- `SPEC/STATE_MACHINE.md` - State machines
- `SPEC/VERSION.md` - Version management strategy
- `SPEC/WEB.md` - Read-only web interface and knowledge-graph visualisation

## FAQ

### Planning and Structure

**What roadmaps exist?**
```bash
rmp roadmap list               # List all roadmaps
```

**How do I create and remove roadmaps?**
```bash
rmp roadmap create <name>      # Create a new roadmap
rmp roadmap remove <name>      # Permanently delete a roadmap and its entire home directory
```

**How do I target a roadmap?**

Pass `-r <name>` (or `--roadmap <name>`) to every command that operates on tasks, sprints, the backlog, the audit log, statistics, or the graph. There is no default or active roadmap to set.

**What is the overall state of the roadmap?**
```bash
rmp stats -r <name>            # Sprint counts, task distribution by status, average velocity
```

**What is in the backlog?**
```bash
rmp backlog list -r <name>               # All backlog tasks, sorted by priority
rmp backlog show-next -r <name> 10       # Top 10 backlog tasks for sprint planning
rmp backlog list -r <name> --type BUG    # Filter by type
rmp backlog list -r <name> --priority 7  # Filter by minimum priority
```

**What sprints exist?**
```bash
rmp sprint list -r <name>
rmp sprint list -r <name> --status OPEN
rmp sprint list -r <name> --status PENDING,CLOSED
```

---

### Task Management

**How do I create a well-defined task?**
```bash
rmp task create -r <name> \
  -t "Title" \
  -fr "Functional requirements - Why build it?" \
  -tr "Technical requirements - How to build it?" \
  -ac "Acceptance criteria - How to verify it?" \
  --type USER_STORY --priority 7 --severity 3 \
  --specialists "go-developer,exhaustive-qa-engineer"
```

**What task types are available?**
- `USER_STORY` - New feature from the user's perspective
- `TASK` - Internal work unit (setup, configuration); the default
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
rmp task get -r <name> 5
rmp task get -r <name> 1,2,3             # Bulk fetch
```

**How do I filter and search tasks?**
```bash
rmp task list -r <name> --status BACKLOG
rmp task list -r <name> --status DOING,TESTING
rmp task list -r <name> --type BUG --severity 8,9
rmp task list -r <name> --priority 7 --status SPRINT
rmp task list -r <name> --specialists "go-developer"
rmp task list -r <name> --created-since 2026-03-01
rmp task list -r <name> --sort created --limit 50
```

**What is the next task to work on?**
```bash
rmp task next -r <name>                  # Next task from the open sprint (by sprint order)
rmp task next -r <name> 5                # Next 5 tasks
```

**How do I edit a task?**
```bash
rmp task edit -r <name> <id> -t "New title" --priority 9 --type BUG
```

**How do I delete a task?**
```bash
rmp task remove -r <name> <id>
rmp task rm -r <name> 1,2,3              # Bulk delete (tasks must be in BACKLOG)
```
- A task must be in `BACKLOG` status and have no sub-tasks.

---

### Sub-task Hierarchy

**How do I break a task into sub-tasks?**
```bash
rmp task create -r <name> -t "Write unit tests" \
  -fr "..." -tr "..." -ac "..." \
  --type SUB_TASK --parent 42
```
- The parent task's `subtask_count` is updated automatically.

**How do I list the sub-tasks of a task?**
```bash
rmp task subtasks -r <name> 42           # Direct sub-tasks of task 42, ordered by priority
```

---

### Task Dependencies

**How do I declare that a task depends on another?**
```bash
rmp task add-dep -r <name> 10 5          # Task 10 depends on task 5 (task 5 must complete first)
rmp task remove-dep -r <name> 10 5       # Remove that dependency
```
- Circular dependencies are rejected.

**How do I see what is blocking a task?**
```bash
rmp task blockers -r <name> 10           # Tasks that task 10 depends on and are not yet COMPLETED
```

**How do I see what a task is blocking?**
```bash
rmp task blocking -r <name> 5            # Tasks that depend on task 5 and are waiting for it
```

---

### Sprint Lifecycle

**How do I create a sprint?**
```bash
rmp sprint create -r <name> -t "Auth module" -d "Sprint 5 - Auth module"
rmp sprint create -r <name> -t "Auth module" -d "Sprint 5 - Auth module" --max-tasks 8   # Cap at 8 active tasks
rmp sprint create -r <name> -t "Auth module" -d "Sprint 5 - Auth module" --order 5        # Set the execution order
```
- `--order` is a positive integer, unique across the roadmap; the sprint with the lowest order executes first. When omitted, the next available value is auto-assigned.

**How do I update a sprint?**
```bash
rmp sprint update -r <name> <id> -t "New title"
rmp sprint update -r <name> <id> -d "New description"
rmp sprint update -r <name> <id> --max-tasks 10        # Update the capacity limit
rmp sprint update -r <name> <id> --order 2             # Change the execution order (PENDING/OPEN only)
rmp sprint upd -r <name> <id> -t "New title" -d "New description" --max-tasks 10
```

**How do I add tasks to a sprint?**
```bash
rmp sprint add-tasks -r <name> 1 5,8,12,15
```
- Tasks move from `BACKLOG` to `SPRINT` automatically.
- Rejected if the sprint is at `max_tasks` capacity.

**How do I define the execution order of tasks within a sprint?**
```bash
rmp sprint reorder -r <name> 1 3,1,2        # Task 3 first, then 1, then 2
rmp sprint move-to -r <name> 1 8 0          # Move task 8 to position 0 (top)
rmp sprint top -r <name> 1 8                # Same as above, shorthand
rmp sprint bottom -r <name> 1 8             # Move task 8 to the last position
rmp sprint swap -r <name> 1 3 7             # Swap the positions of tasks 3 and 7
```

**How do I start a sprint?**
```bash
rmp sprint start -r <name> <id>
```
- Only one sprint can be `OPEN` at a time.

**How do I move tasks between sprints?**
```bash
rmp sprint move-tasks -r <name> 1 2 5,6,7   # Move tasks 5, 6, 7 from sprint 1 to sprint 2
```

**How do I remove tasks from a sprint?**
```bash
rmp sprint remove-tasks -r <name> 1 5,6     # Tasks return to BACKLOG
```

**How do I close a sprint?**
```bash
rmp sprint close -r <name> <id>
rmp sprint close -r <name> <id> --force     # Bypass the active-task check
```

**How do I reopen a closed sprint?**
```bash
rmp sprint reopen -r <name> <id>
```

**How do I remove a sprint?**
```bash
rmp sprint remove -r <name> <id>
rmp sprint rm -r <name> <id>
```
- The sprint must be `CLOSED`. Tasks return to `BACKLOG`.

**Can I have multiple open sprints?**
No. Only one sprint can be `OPEN` at a time. Close the current sprint before starting another.

---

### Work Execution

**How do I start working on a task?**
```bash
rmp task stat -r <name> <id> DOING          # Sets started_at automatically
```

**How do I mark a task as ready for testing?**
```bash
rmp task stat -r <name> <id> TESTING        # Sets tested_at automatically
```

**What do I do if a task fails testing?**
```bash
rmp task stat -r <name> <id> DOING          # Return to development
```

**How do I complete a task?**
```bash
rmp task stat -r <name> <id> COMPLETED
rmp task stat -r <name> <id> COMPLETED --summary "Implemented OAuth2 with PKCE flow"
```
- `--summary` is optional (max 4096 chars) and only valid on the `TESTING → COMPLETED` transition.
- `closed_at` is set automatically.

**How do I bulk-change task status?**
```bash
rmp task stat -r <name> 1,2,3 TESTING
```

**How do I reopen a completed task?**
```bash
rmp task reopen -r <name> <id>              # Returns to BACKLOG, clears all lifecycle timestamps
rmp task reopen -r <name> 1,2,3             # Bulk reopen
```

**How do I change priority or severity?**
```bash
rmp task prio -r <name> <id> 9              # Priority 0-9
rmp task sev -r <name> <id> 8               # Severity 0-9
rmp task prio -r <name> 1,2,3 7             # Bulk update
```

**How do I assign or remove specialists?**
```bash
rmp task assign -r <name> <id> "go-developer"
rmp task unassign -r <name> <id> "go-developer"
```

---

### Visibility and Reporting

**How is the sprint going? (comprehensive view)**
```bash
rmp sprint show -r <name> <id>
```
Returns: status, task summary (pending/in-progress/completed), progress percentages, severity distribution, criticality distribution, task order, current load, and capacity percentage.

**How many tasks are in each status? (metrics and velocity)**
```bash
rmp sprint stats -r <name> <id>
```
Returns: total tasks, completed tasks, progress percentage, status distribution, task order, velocity (tasks/day, CLOSED sprints only), days elapsed, and burndown series.

**What tasks are still open in the sprint?**
```bash
rmp sprint open-tasks -r <name> <id>                    # SPRINT + DOING + TESTING only
rmp sprint open-tasks -r <name> <id> --order-by-priority
```

**How do I list all tasks in a sprint?**
```bash
rmp sprint tasks -r <name> <id>
rmp sprint tasks -r <name> <id> --order-by-priority
```

**How do I see a task's full audit history?**
```bash
rmp audit history -r <name> TASK <id>
```

**What happened recently?**
```bash
rmp audit list -r <name> --limit 20
rmp audit list -r <name> --since 2026-03-20
rmp audit list -r <name> --since 2026-03-01 --until 2026-03-31
rmp audit list -r <name> --entity-type SPRINT
```

**How do I see audit statistics?**
```bash
rmp audit stats -r <name>
rmp audit stats -r <name> --since 2026-03-01 --until 2026-03-31
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

A read-only, browser-based view of everything the CLI manages. It starts an HTTP server embedded in the `rmp` binary that lists every roadmap under `~/.roadmaps/`. Selecting a roadmap lands you on its sprints page with the current sprint selected; from there a separate page shows the roadmap's full task list, another shows the roadmap's full audit log (paginated, most recent first), and another shows an interactive visualisation of its knowledge graph. It only presents data; the CLI remains the sole write path.

```bash
# Start on the default host (loopback) and port and open the browser
rmp web

# Start without launching a browser; just print the served URL
rmp web --no-open

# Choose a port, or expose the interface on the network (explicit opt-in)
rmp web --port 9000
rmp web --host 0.0.0.0 --port 9000
```

On startup the served URL is printed as JSON (`{"url": "http://127.0.0.1:8787"}`); the `url` reflects the actual bound host and port. When `--port` is omitted and `8787` is in use, the server falls back to an OS-chosen ephemeral port so it still starts.

**What makes it different from every other command?**

- **Read-only.** No route creates, edits, or deletes anything; serving a page writes no rows, no audit-log entry, and never checkpoints the graph store. Only `GET`/`HEAD` are accepted (any other method returns HTTP 405).
- **No `-r` flag.** It is the one command exempt from the always-required-roadmap rule; it lists all roadmaps and you pick one in the browser.
- **Long-lived.** It keeps serving until interrupted; `Ctrl+C` (`SIGINT`) or `SIGTERM` shuts it down gracefully (exit 0).
- **Tabler dark-theme UI.** The interface is built on the vendored Tabler admin-dashboard framework in its dark theme: a navigation sidebar (which collapses to a hamburger menu on small viewports), a top navbar, page headers, and Tabler cards, tables, and badges.
- **Self-contained and offline.** Every asset (HTML, CSS, JavaScript, the vendored Tabler framework and D3.js graph library with the d3-sankey plugin, the Tabler Icons webfont, and the Inter font) is embedded in the binary via `go:embed` and served only from `/static/`; no page references a CDN, a remote font host, or any other remote origin, and the server makes no outbound request.
- **Loopback by default.** It binds the loopback interface (`127.0.0.1`), so the read-only interface is reachable only from the local machine. Exposing it on the network is the explicit opt-in via `--host 0.0.0.0` (or any other non-loopback address), which also prints a network-exposure warning to stderr. Roadmap names from the URL are validated before any filesystem path is built (path-traversal guard).
- **Responsive, mobile-first.** Every page, including the graph visualisation, adapts to small touch viewports.

See [DOCS/commands/web.md](DOCS/commands/web.md) for the full route list and exit codes.

---

### Important Distinctions

**What is the difference between Priority and Severity?**

- **Priority (0-9)**: business urgency, set by the Product Owner.
  - `rmp task prio -r <name> <id> 9` / filter: `--priority 8,9`
- **Severity (0-9)**: technical impact, set by the engineering team.
  - `rmp task sev -r <name> <id> 8` / filter: `--severity 8,9`

Both scales run 0 (lowest) to 9 (highest). Use them independently.

**What is the difference between `sprint show` and `sprint stats`?**

- `sprint show` — operational view: task composition, severity/criticality distributions, capacity load.
- `sprint stats` — metric view: velocity, burndown, days elapsed, progress percentage.

**What is the difference between sprint order and task priority?**

**Sprint task order** controls the execution sequence within a sprint — the order `rmp task next` returns tasks:
```bash
rmp sprint reorder -r <name> 1 5,3,1,2
rmp task next -r <name>                   # Returns task 5 first
```

**Priority** is a planning attribute for filtering and backlog grooming:
```bash
rmp backlog show-next -r <name> 5         # Top 5 by priority for sprint planning
rmp task list -r <name> --priority 8,9
```

(Distinct from both is a sprint's own `--order`, which sequences the sprints themselves across the roadmap.)

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
- `rmp bl` = `rmp backlog`
- `rmp aud` = `rmp audit`
- `rmp new` = `rmp create` (subcommand alias, e.g. `rmp task new`)
- `rmp ls` = `rmp list` (subcommand alias, e.g. `rmp task ls`)
- `rmp rm` = `rmp remove` (subcommand alias, e.g. `rmp task rm`)
- `rmp upd` = `rmp update` (subcommand alias, e.g. `rmp sprint upd`)

**What if I get the "No roadmap selected" error?**
```bash
rmp roadmap list               # See which roadmaps exist
rmp <command> -r <name> ...    # Pass -r explicitly; there is no default roadmap
```

**How are timestamps tracked automatically?**
- `started_at` — set when a task moves to `DOING`
- `tested_at` — set when a task moves to `TESTING`
- `closed_at` — set when a task moves to `COMPLETED`
- All three are cleared when a task is reopened to `BACKLOG`

## License

MIT License - see [LICENSE](LICENSE) for details.
