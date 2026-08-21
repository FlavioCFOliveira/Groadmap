# SPEC — Groadmap Technical Specification

This directory contains the authoritative technical specification for Groadmap. The Specification First Policy applies: no implementation without a corresponding SPEC entry. See `CLAUDE.md` (project root) for the policy in full.

The SPEC is unversioned. Git is the source of truth for its evolution — recover any past state via `git log` and `git show`.

---

## 1. Quick-Find Map

| Looking for... | File |
|----------------|------|
| CLI command syntax / flags | `COMMANDS.md` |
| JSON input/output formats | `DATA_FORMATS.md` |
| Help text structure | `HELP.md` |
| Knowledge graph feature (design, persistence, guard rails) | `GRAPH.md` |
| Read-only web interface (`rmp web`, server, pages, graph viz) | `WEB.md` |
| Web roadmap sprints page / landing (`/roadmaps/{name}`, sprint tabs Próximos / Actual / Concluídos) | `WEB.md § Roadmap Sprints Page` |
| Web roadmap tasks page (`/roadmaps/{name}/tasks`, Kanban task board, header search and type / priority / severity filters) | `WEB.md § Roadmap Tasks Page` |
| Web sprint page (`/roadmaps/{name}/sprints/{id}`) | `WEB.md § Roadmap Sprint Page` |
| Web shared sprint-card partial (header, description, task-count footer; used by all three sprints-page tabs) | `WEB.md § Shared Sprint-Card Partial` |
| Web sprint detail sub-template (status summary line, metadata datagrid, member-tasks board; single sprint page only) | `WEB.md § Sprint Detail Sub-Template` |
| Web task detail modal (read-only task popup) | `WEB.md § Task Detail Modal` |
| Web graph labels sidebar (node-label / edge-type inventory, counts, section totals, highlight, collapse/expand) | `WEB.md § Graph Labels Sidebar` |
| Web graph query bar (editable Cypher query box, Search button, node-limit dropdown) | `WEB.md § Graph Query Bar` |
| Web graph query-bar error handling (rejected vs failed vs invalid limit) | `WEB.md § Query-Bar Error Handling` |
| Web graph data endpoint `q` / `limit` parameters, read-only guard-rail, limit injection, node/edge extraction | `WEB.md § Graph Data Endpoint` |
| Web startup schema migration (automatic, no-input, before serving) | `WEB.md § Startup Schema Migration` |
| `rmp web` command syntax / flags | `COMMANDS.md § Web Interface` |
| Web graph data endpoint JSON shape | `DATA_FORMATS.md § Graph View Data` |
| Self-contained web binary (offline, no CDN, embedded asset categories) | `WEB.md § Self-Contained Deliverable` |
| Responsive / mobile-first web design | `WEB.md § Responsive and Mobile-First Design` |
| Web UI framework (Tabler admin shell, dark theme, Tabler-fidelity rules, card tabs) | `WEB.md § UI Framework` |
| Web status / priority / severity badge colours (semantic Tabler `bg-*-lt` mapping) | `WEB.md § Status, Priority, and Severity Badge Colours` |
| Web HTTP security headers (CSP, X-Frame-Options, etc.) | `WEB.md § Security Headers` |
| Web HTTP server timeouts (read-header, write, idle) and the graph data endpoint's query time budget | `WEB.md § HTTP Server Timeouts` |
| Vendored web assets / embedded Tabler framework and D3.js (with d3-sankey) | `BUILD.md § Vendored Web Assets` |
| Free-text control-character constraint (CWE-150 / Trojan Source) | `MODELS.md § Task` (Free-Text Control-Character Constraint) |
| Task commit-hash format (7-64 hexadecimal characters, lowercase on storage, no git invocation) | `MODELS.md § Task` (Commit Hash Constraint) |
| Task commit-hash `CHECK` constraints and why `GLOB` is case-sensitive | `DATABASE.md § Commit Hash Format Constraint` |
| Task commit tracking (`commit_open` / `commit_close`: when each is written, when each is cleared, the asymmetry on reopening) | `STATE_MACHINE.md § Commit Tracking Fields` |
| `task stat` commit flags (`--commit-open` / `--commit-close`), their validation order and errors | `COMMANDS.md § Change Status (stat)` |
| Comment types (the one enum, and the per-entity valid subsets) | `MODELS.md § Comment Type` |
| Task comment / sprint comment models and field constraints | `MODELS.md § Task Comment` and `MODELS.md § Sprint Comment` |
| Comment subcommand syntax / flags (`comment-add`, `comment-list`, `comment-edit`, `comment-remove`) | `COMMANDS.md § Task Comments` and `COMMANDS.md § Sprint Comments` |
| Comment body input via flag or stdin | `COMMANDS.md § Comment Body Input Source and Precedence` |
| Comment positional argument count, and the refusal of a stray extra argument | `COMMANDS.md § Comment Positional Argument Contract` |
| Comment JSON shape | `DATA_FORMATS.md § Task Comment` and `DATA_FORMATS.md § Sprint Comment` |
| Comment tables, DDL, and cascade rules | `DATABASE.md § task_comments Table` and `DATABASE.md § sprint_comments Table` |
| Web comment presentation (task modal timeline, sprint Comments card) | `WEB.md § Task Detail Modal` and `WEB.md § Sprint Detail Sub-Template` |
| Sprint `description` semantics (must state the sprint's high-level goal) | `MODELS.md § Sprint Field Constraints` |
| Sprint membership fields (`tasks` as ids, `task_count`, what an empty sprint reports, which reads populate them) | `MODELS.md § Sprint Field Constraints` and `COMMANDS.md § List Sprints` |
| Sprint membership read cost (one grouped read for the whole listing, no query per sprint) | `DATABASE.md § Read the Membership of Many Sprints (Grouped)` |
| Sprint listing order (`sprint list` returns sprints by `order` ascending, the planned execution order) and how the web sprint tabs relate to it | `COMMANDS.md § List Sprints` and `WEB.md § Roadmap Sprints Page` |
| Audit result-set cap (`MaxAuditLimit`) | `DATABASE.md § Audit Result Limit` |
| Migration idempotency (ALTER TABLE ADD COLUMN guard) | `DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)` |
| Migration idempotency (ALTER TABLE DROP COLUMN guard, what a drop preserves and discards) | `DATABASE.md § Migration Idempotency (ALTER TABLE DROP COLUMN)` |
| `graph` command syntax / subcommands | `COMMANDS.md § Graph Management` |
| Graph query result JSON / property-type mapping | `DATA_FORMATS.md § Graph Query Result` |
| Cypher input via flag or stdin | `GRAPH.md § Cypher Input Source and Precedence` |
| Graph query notifications on stderr (e.g. Cartesian-product warning) | `GRAPH.md § Query Notifications as Diagnostics` |
| Graph store concurrency / recovery | `IMPLEMENTATION.md § Graph Store Concurrency` |
| Graph store access lock (shared for reads, exclusive for writes), and what happens on contention | `GRAPH.md § Concurrency and Recovery` and `GRAPH.md § Lock Contention` |
| What a graph read does and does not change on disk (the recovery repair performed on open) | `GRAPH.md § What a Read Changes on Disk` |
| Go toolchain / external dependencies | `BUILD.md § Go Toolchain` |
| Dependency version pins (GoGraph, `modernc.org/sqlite`, and the exact `modernc.org/libc` / `modernc.org/memory` versions the driver requires) | `BUILD.md § External Dependencies` |
| AI agent contract (CLI surface) | `COMMANDS.md § AI Help` |
| AI agent contract (JSON schema) | `DATA_FORMATS.md § AI Agent Contract` |
| AI agent contract (generation) | `ARCHITECTURE.md § AI Agent Contract Generation` |
| `AI_AGENT` env-var behaviour | `HELP.md § AI_AGENT environment variable` |
| Domain models (Task, Sprint, etc.) | `MODELS.md` |
| Memory layout / struct ordering | `MODELS.md § Memory Layout Optimization` |
| State transitions (Task) | `STATE_MACHINE.md § Task State Machine` |
| State transitions (Sprint) | `STATE_MACHINE.md § Sprint State Machine` |
| Sprint membership versus task status (a `BACKLOG` task that is still a sprint member) | `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status` |
| System design / modules | `ARCHITECTURE.md` |
| Data directory layout | `ARCHITECTURE.md § Directory Structure` |
| File and directory permissions (`0700`, `0600`, enforcement, failure mode) | `ARCHITECTURE.md § Open-Time Permission Enforcement` |
| Filesystem safety (no symlink following, CWE-59) | `ARCHITECTURE.md § Security Guarantees` |
| Filesystem layout migration (per-roadmap directories) | `ARCHITECTURE.md § Filesystem Layout Migration` |
| Error handling / sentinel errors | `ARCHITECTURE.md § Error Handling` |
| Exit codes | `ARCHITECTURE.md § Exit Codes` |
| Database schema (DDL) | `DATABASE.md § DDL - Table Creation` |
| SQL queries | `DATABASE.md § Main SQL Queries` |
| Audit operations catalogue (the canonical list, including the LEGACY values) | `DATABASE.md § audit Table` |
| Audit: how many entries an operation writes and what each says | `DATABASE.md § One Row per Thing That Happened` |
| Audit entry `related_entity_id` (which operations write it, and what the counterpart is) | `DATABASE.md § The Two Entities of a Relational Operation` |
| Audit entry `commit_hash` (which operations write it, and why a reopening does not clear it) | `DATABASE.md § The Commit Hash of an Audit Entry` |
| Audit entry model, enums, and struct layout | `MODELS.md § Audit Entry`, `MODELS.md § Audit Operation`, `MODELS.md § Entity Type` |
| Audit entry JSON shape (seven keys, two nullable) | `DATA_FORMATS.md § Audit Entry` |
| Audit log append-only guarantee | `ARCHITECTURE.md § Security Guarantees` |
| Audit help surfaces (operation list, LEGACY marking, output keys) | `HELP.md § Audit family help specifics` |
| Concurrency (WAL, pool, retry) | `IMPLEMENTATION.md § Concurrency Model` |
| Query caching | `IMPLEMENTATION.md § Query Caching` |
| Performance practices | `IMPLEMENTATION.md § Performance Considerations` |
| Application version | `VERSION.md` |
| Schema migrations | `VERSION.md § Migrations` |
| Build / CI / lint | `BUILD.md` |
| Validation gates (the six gates, and their enforcement locally, in CI, and at release) | `BUILD.md § Validation Gates` |
| Security scan (`gosec`, accepted findings, scope exclusion) | `BUILD.md § Security Scan: gosec` |
| Installation / release | `DEPLOY.md` |

---

## 2. Index

| File | Functional Area |
|------|-----------------|
| `COMMANDS.md` | CLI commands, subcommands, flags, aliases |
| `DATA_FORMATS.md` | JSON schemas, input/output formats |
| `HELP.md` | CLI help skeleton and structure |
| `GRAPH.md` | Knowledge graph feature: GoGraph integration, persistence, multi-layer conventions, guard-rail validation |
| `WEB.md` | Read-only web interface: `rmp web` server, server-rendered pages, interactive knowledge-graph visualisation, embedded assets |
| `MODELS.md` | Structs, enums, memory layout |
| `STATE_MACHINE.md` | Task and Sprint state transitions |
| `ARCHITECTURE.md` | System design, modules, error handling, exit codes |
| `DATABASE.md` | Schema, queries, constraints, indexes |
| `IMPLEMENTATION.md` | Concurrency, caching, performance strategies |
| `VERSION.md` | Application and schema versioning, migrations |
| `BUILD.md` | Build system, cross-compilation, validation gates, CI/CD |
| `DEPLOY.md` | Installation, distribution, release process |

---

## 3. Canonical Sources

To prevent drift across SPEC files, the following topics have a single authoritative source. Other SPEC files MUST link to the canonical source rather than duplicate its content.

| Topic | Canonical Source |
|-------|------------------|
| Exit codes (numeric values and sentinel names) | `ARCHITECTURE.md § Exit Codes` |
| Sentinel errors and wrapping rules | `ARCHITECTURE.md § Error Handling` |
| Filesystem permission model (`0700` directories, `0600` database, when enforced, failure mode) | `ARCHITECTURE.md § Open-Time Permission Enforcement` |
| Enums (`TaskType`, `TaskStatus`, `SprintStatus`, `CommentType`) | `MODELS.md § Enums` |
| Comment type per-entity valid subsets (task: 7 values, sprint: 4 values) | `MODELS.md § Comment Type` |
| Comment body input source and precedence (`--body` or stdin) | `COMMANDS.md § Comment Body Input Source and Precedence` |
| Comment positional arguments (exactly one id per subcommand; an extra one is exit code 2) | `COMMANDS.md § Comment Positional Argument Contract` |
| Memory layout / struct field ordering | `MODELS.md § Memory Layout Optimization` |
| Task state transitions | `STATE_MACHINE.md § Task State Machine` |
| Task commit-hash format (character set, length bounds, lowercase normalisation) | `MODELS.md § Task` (Commit Hash Constraint) |
| Task commit tracking field rules (when `commit_open` and `commit_close` are written, preserved, and cleared) | `STATE_MACHINE.md § Commit Tracking Fields` |
| Sprint state transitions | `STATE_MACHINE.md § Sprint State Machine` |
| Sprint membership versus task status | `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status` |
| Audit operations catalogue | `DATABASE.md § audit Table` |
| SQL DDL (table definitions, indexes, constraints) | `DATABASE.md` |
| Schema migrations | `VERSION.md § Migrations` |
| Concurrency model (WAL, pool, retry) | `IMPLEMENTATION.md § Concurrency Model` |
| Caching strategies (query, connection) | `IMPLEMENTATION.md` |
| Knowledge graph feature, persistence layout, guard rails, multi-layer conventions | `GRAPH.md` |
| Read-only web interface (server behaviour, routes, pages, security model) | `WEB.md` |
| Graph store directory (`graph/` subdir) | `GRAPH.md § Persistence Layout` (layout referenced from `ARCHITECTURE.md § Directory Structure`) |
| Graph query result JSON and property-type mapping | `DATA_FORMATS.md § Graph Query Result` |
| Web graph view-data JSON shape | `DATA_FORMATS.md § Graph View Data` |
| Web UI framework (Tabler admin shell, dark theme) | `WEB.md § UI Framework` |
| Vendored web assets / embedded Tabler framework and D3.js (with d3-sankey) | `BUILD.md § Vendored Web Assets` |
| Graph store concurrency / writer serialisation / reader locking / recovery | `IMPLEMENTATION.md § Graph Store Concurrency` (contract in `GRAPH.md § Concurrency and Recovery`) |
| Graph store lock file (`write.lock`) | `GRAPH.md § Concurrency and Recovery` (layout in `GRAPH.md § Persistence Layout`) |
| Minimum Go version and external dependencies | `BUILD.md § Go Toolchain` |
| Validation gate set and where it is enforced (local, CI, release) | `BUILD.md § Validation Gates` |
| Help text canonical | code in `internal/commands/*.go` (structure in `HELP.md`) |
| AI agent contract JSON schema | `DATA_FORMATS.md § AI Agent Contract` |
| AI agent contract generation rules | `ARCHITECTURE.md § AI Agent Contract Generation` |

`DATABASE.md` additionally retains `CHECK` constraints in DDL as a normative reproduction of the enums; the Go-level enum definitions remain in `MODELS.md`.

---

## 4. Global Conventions

### Dates and Timestamps

- All dates and timestamps use ISO 8601 with UTC timezone.
- Format example: `2026-05-12T14:30:00Z`.
- This applies to: database columns, JSON output, audit log entries, version metadata.

### Process Output

- Successful command output: JSON to stdout.
- Error messages, help text, usage hints: plain text to stderr.
- Exit code conveys outcome class (canonical list in `ARCHITECTURE.md`).

### Filesystem

- Roadmap data directory: `~/.roadmaps/` with permissions `0700`.
- Per-roadmap home directory: `~/.roadmaps/<name>/` with permissions `0700`. The directory name is the roadmap name and is the container for all files the application uses for that roadmap.
- Individual roadmap databases: `~/.roadmaps/<name>/project.db` with permissions `0600`, created with mode `0600` from the outset (no umask-derived window) and re-applied and re-verified every time `rmp` opens the database. A database that cannot be brought to `0600` fails the command. The SQLite sidecars `project.db-wal` and `project.db-shm` live alongside and use the same `0600` permissions, restricted opportunistically rather than as a hard guarantee. See `ARCHITECTURE.md § Open-Time Permission Enforcement`.
- Neither the data directory nor any roadmap home directory may be a symbolic link; `rmp` refuses to follow a symlink when creating, opening, or migrating a roadmap directory (CWE-59). See `ARCHITECTURE.md § Directory Structure` and `ARCHITECTURE.md § Security Guarantees`.
- Per-roadmap knowledge graph store: `~/.roadmaps/<name>/graph/` (a directory) with permissions `0700`, created on first use of the `graph` command. See `GRAPH.md § Persistence Layout`.
- Roadmaps in the legacy `~/.roadmaps/<name>.db` layout are migrated automatically to the current layout at startup. See `ARCHITECTURE.md § Filesystem Layout Migration`.

### Naming Conventions

- Database columns: `snake_case` (e.g., `created_at`, `functional_requirements`).
- Go structs and fields: `PascalCase` for exported, `camelCase` for unexported.
- CLI commands and flags: lowercase, kebab-case (e.g., `task list`, `--max-tasks`).
- Short flags: single dash, may exceed one character when an unambiguous abbreviation is more readable (e.g., `-fr` for `--functional-requirements`).
