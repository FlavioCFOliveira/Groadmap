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
| Web board search text rules (trim by White_Space, Unicode NFC normalisation, simple lowercase fold, and the three tables shipped to the browser) | `WEB.md § Roadmap Tasks Page` |
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
| Web status / priority / severity badge colours (semantic Tabler `bg-*-lt` mapping, including the count badges of the sprints-page tabs and of both Kanban boards' columns) | `WEB.md § Status, Priority, and Severity Badge Colours` |
| Web HTTP security headers (CSP, X-Frame-Options, etc.) | `WEB.md § Security Headers` |
| Web HTTP server timeouts (read-header, write, idle) and the graph data endpoint's query time budget | `WEB.md § HTTP Server Timeouts` |
| Vendored web assets / embedded Tabler framework and D3.js (with d3-sankey) | `BUILD.md § Vendored Web Assets` |
| Free-text control-character constraint (CWE-150 / Trojan Source) | `MODELS.md § Task` (Free-Text Control-Character Constraint) |
| Free-text UTF-8 encoding constraint (only valid UTF-8 is accepted and stored) | `MODELS.md § Task` (Free-Text UTF-8 Encoding Constraint) |
| The two free-text content rules applied to Cypher and to knowledge-graph property values (which `rmp graph` subcommand each rule binds, and why the two reaches differ) | `GRAPH.md § Cypher Query and Property Value Content Rules` |
| Published field name in a validation error message (one name per field, underscored; how it differs from the flag name) | `COMMANDS.md § Published Field Names in Validation Messages` |
| Task commit-hash format (7-64 hexadecimal characters, lowercase on storage, no git invocation) | `MODELS.md § Task` (Commit Hash Constraint) |
| Task commit-hash `CHECK` constraints and why `GLOB` is case-sensitive | `DATABASE.md § Commit Hash Format Constraint` |
| Task commit tracking (`commit_open` / `commit_close`: when each is written, when each is cleared, the asymmetry on reopening) | `STATE_MACHINE.md § Commit Tracking Fields` |
| `task stat` commit flags (`--commit-open` / `--commit-close`), their validation order and errors | `COMMANDS.md § Change Status (stat)` |
| Comment types (the one enum, and the per-entity valid subsets) | `MODELS.md § Comment Type` |
| Task comment / sprint comment models and field constraints | `MODELS.md § Task Comment` and `MODELS.md § Sprint Comment` |
| Comment subcommand syntax / flags (`comment-add`, `comment-list`, `comment-edit`, `comment-remove`) | `COMMANDS.md § Task Comments` and `COMMANDS.md § Sprint Comments` |
| Comment body input via flag or stdin | `COMMANDS.md § Comment Body Input Source and Precedence` |
| Positional argument count of any command, and the refusal of an invocation that supplies more than the command declares | `COMMANDS.md § Positional Arguments` |
| Comment positional argument count, and what the one id identifies on each comment subcommand | `COMMANDS.md § Comment Positional Argument Contract` |
| Comment JSON shape | `DATA_FORMATS.md § Task Comment` and `DATA_FORMATS.md § Sprint Comment` |
| Comment tables, DDL, and cascade rules | `DATABASE.md § task_comments Table` and `DATABASE.md § sprint_comments Table` |
| Web comment presentation (task modal timeline, sprint Comments card) | `WEB.md § Task Detail Modal` and `WEB.md § Sprint Detail Sub-Template` |
| Sprint `description` semantics (must state the sprint's high-level goal) | `MODELS.md § Sprint Field Constraints` |
| Sprint membership fields (`tasks` as ids, `task_count`, what an empty sprint reports, which reads populate them) | `MODELS.md § Sprint Field Constraints` and `COMMANDS.md § List Sprints` |
| Sprint membership read cost (one grouped read for the whole listing, no query per sprint) | `DATABASE.md § Read the Membership of Many Sprints (Grouped)` |
| Sprint listing order (`sprint list` returns sprints by `order` ascending, the planned execution order) and how the web sprint tabs relate to it | `COMMANDS.md § List Sprints` and `WEB.md § Roadmap Sprints Page` |
| In-sprint task order (`sprint_tasks.position` is unique within a sprint, why the order must be total, and what every write path must do to preserve it) | `DATABASE.md § Position Uniqueness Within a Sprint` |
| In-sprint position density (a sprint holds exactly `0` to `N-1`, why the schema cannot enforce it, every write path that touches `position` and whether it preserves or repairs the run) | `DATABASE.md § Position Density Within a Sprint` |
| Adding a `UNIQUE` index to a populated table (repair before create, why a repair must not read its own writes, the failure surface) | `DATABASE.md § Introducing a Uniqueness Constraint over Existing Rows` |
| Audit result-set cap (`MaxAuditLimit`) | `DATABASE.md § Audit Result Limit` |
| Migration idempotency (ALTER TABLE ADD COLUMN guard) | `DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)` |
| Migration idempotency (ALTER TABLE DROP COLUMN guard, what a drop preserves and discards) | `DATABASE.md § Migration Idempotency (ALTER TABLE DROP COLUMN)` |
| `graph` command syntax / subcommands | `COMMANDS.md § Graph Management` |
| Graph query result JSON / property-type mapping | `DATA_FORMATS.md § Graph Query Result` |
| Cypher input via flag or stdin | `GRAPH.md § Cypher Input Source and Precedence` |
| Maximum Cypher query length (1 MiB, counted in bytes) and the exit code for exceeding it | `GRAPH.md § Maximum Query Length` |
| A stray positional argument on a `graph` subcommand (the five accept none), the exact line it publishes, and where the refusal lands in the subcommand's order | `GRAPH.md § No Positional Query: A Stray Token Is Refused` |
| Bounded standard-input read of a Cypher query, and the refusal of an empty, whitespace-only, or terminal standard input | `GRAPH.md § Bounded Standard-Input Read` and `GRAPH.md § Standard Input That Supplies No Query` |
| Keyword spacing the guard rail requires in a `SHOW INDEX(ES)` / `SHOW CONSTRAINT(S)` command, and why the DDL class stays whitespace-tolerant | `GRAPH.md § Keyword Spacing in a Schema-Introspection Command` |
| Which Cypher engine constructor each graph path uses (read vs transactional write) and why a read opens no store | `GRAPH.md § Engine Constructor by Path` |
| Graph query notifications on stderr (e.g. Cartesian-product warning) | `GRAPH.md § Query Notifications as Diagnostics` |
| Graph store concurrency / recovery | `IMPLEMENTATION.md § Graph Store Concurrency` |
| Graph store access lock (shared for reads, exclusive for writes), and what happens on contention | `GRAPH.md § Concurrency and Recovery` and `GRAPH.md § Lock Contention` |
| What a graph read does and does not change on disk (the recovery repair performed on open) | `GRAPH.md § What a Read Changes on Disk` |
| Go toolchain / external dependencies | `BUILD.md § Go Toolchain` |
| Dependency version pins (the four direct modules — GoGraph, `golang.org/x/sys`, `golang.org/x/text`, `modernc.org/sqlite` — and the exact `modernc.org/libc` / `modernc.org/memory` versions the driver requires) | `BUILD.md § External Dependencies` |
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
| Error output shape (the parts of stderr and the order they appear in) | `HELP.md § Error message format` |
| Dispatch failure (an unresolved command or subcommand name): exit code `127`, the help written after the error, the excluded `--ai-help` scope case | `HELP.md § Error message format` and `COMMANDS.md § Dispatch Failures (Unresolved Command or Subcommand Names)` |
| Which error classes append help, and which do not | `HELP.md § Recovery help after a dispatch failure` |
| Stdout silence on a failing invocation, and the help invocations that exit `0` | `HELP.md § Stdout silence on failure` and `COMMANDS.md § Failing Invocations Write Nothing to Stdout` |
| Database schema (DDL) | `DATABASE.md § DDL - Table Creation` |
| SQL queries | `DATABASE.md § Main SQL Queries` |
| Audit operations catalogue (the canonical list, including the LEGACY values) | `DATABASE.md § audit Table` |
| Audit operation descriptions on `rmp --ai-help` (that each is its catalogue entry verbatim, the two alterations the transcription makes, and what editing a catalogue entry therefore costs) | `DATABASE.md § The Catalogue Entry Is Also the Published Contract Description` and `DATA_FORMATS.md § enums map entry` |
| Audit: how many entries an operation writes and what each says | `DATABASE.md § One Row per Thing That Happened` |
| Audit entry `related_entity_id` (which operations write it, and what the counterpart is) | `DATABASE.md § The Two Entities of a Relational Operation` |
| Audit entry `commit_hash` (which operations write it, and why a reopening does not clear it) | `DATABASE.md § The Commit Hash of an Audit Entry` |
| Audit entry model, enums, and struct layout | `MODELS.md § Audit Entry`, `MODELS.md § Audit Operation`, `MODELS.md § Entity Type` |
| Audit entry JSON shape (seven keys, two nullable) | `DATA_FORMATS.md § Audit Entry` |
| Audit log append-only guarantee | `ARCHITECTURE.md § Security Guarantees` |
| Audit help surfaces (operation list, LEGACY marking, output keys) | `HELP.md § Audit family help specifics` |
| Audit operation entity type (which entity each operation is recorded against, why it is declared rather than read off the operation's name, and the gate that fails on an unclassified operation) | `HELP.md § Audit operation entity-type classification` |
| Audit operation `entity_type` and `legacy` members of the AI Agent Contract enum | `DATA_FORMATS.md § enums map entry` |
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
| Error output shape (stderr parts and their order, which error classes append help, stdout silence on failure) | `HELP.md § Error message format` |
| Filesystem permission model (`0700` directories, `0600` database, when enforced, failure mode) | `ARCHITECTURE.md § Open-Time Permission Enforcement` |
| Enums (`TaskType`, `TaskStatus`, `SprintStatus`, `CommentType`) | `MODELS.md § Enums` |
| Comment type per-entity valid subsets (task: 7 values, sprint: 4 values) | `MODELS.md § Comment Type` |
| Published field names in validation messages (field to published name, and when a message names the flag instead) | `COMMANDS.md § Published Field Names in Validation Messages` |
| Which commands read standard input at all (exactly two flag values, and no other command) | `DATA_FORMATS.md § Input` |
| Comment body input source and precedence (`--body` or stdin) | `COMMANDS.md § Comment Body Input Source and Precedence` |
| Cypher query input source, maximum query length, the bounded standard-input read, and the refusal of a positional argument on any `graph` subcommand | `GRAPH.md § Cypher Input Source and Precedence` |
| Cypher query and knowledge-graph property value content (the UTF-8 encoding rule on every graph subcommand, the control-character rule on the two that write, their order, and the limits of both) | `GRAPH.md § Cypher Query and Property Value Content Rules` |
| Declared positional arity per command, and the refusal of an excess positional argument (exit code 2, the published line, no side effect) | `COMMANDS.md § Positional Arguments` |
| Comment positional arguments (exactly one id per subcommand, and what that id identifies) | `COMMANDS.md § Comment Positional Argument Contract` |
| Memory layout / struct field ordering | `MODELS.md § Memory Layout Optimization` |
| Task state transitions | `STATE_MACHINE.md § Task State Machine` |
| Task commit-hash format (character set, length bounds, lowercase normalisation) | `MODELS.md § Task` (Commit Hash Constraint) |
| Free-text field encoding (only valid UTF-8 accepted and stored, on the flag path and the standard-input path alike) | `MODELS.md § Task` (Free-Text UTF-8 Encoding Constraint) |
| Task commit tracking field rules (when `commit_open` and `commit_close` are written, preserved, and cleared) | `STATE_MACHINE.md § Commit Tracking Fields` |
| Sprint state transitions | `STATE_MACHINE.md § Sprint State Machine` |
| Sprint membership versus task status | `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status` |
| Audit operations catalogue | `DATABASE.md § audit Table` |
| Audit operation description text, on the catalogue and on the `rmp --ai-help` contract alike | `DATABASE.md § audit Table` (the entry itself), with the coupling and its cost stated in `DATABASE.md § The Catalogue Entry Is Also the Published Contract Description` |
| Audit operation entity-type classification and LEGACY marking (the single declaration both published help surfaces render from) | `HELP.md § Audit operation entity-type classification` |
| SQL DDL (table definitions, indexes, constraints) | `DATABASE.md` |
| In-sprint task order and the uniqueness of `sprint_tasks.position` | `DATABASE.md § Position Uniqueness Within a Sprint` |
| In-sprint position density, and the compaction every removal owes | `DATABASE.md § Position Density Within a Sprint` and `DATABASE.md § Compact Sprint Positions` |
| Introducing a uniqueness constraint over rows that already exist | `DATABASE.md § Introducing a Uniqueness Constraint over Existing Rows` |
| Schema migrations | `VERSION.md § Migrations` |
| Concurrency model (WAL, pool, retry) | `IMPLEMENTATION.md § Concurrency Model` |
| Caching strategies (query, connection) | `IMPLEMENTATION.md` |
| Knowledge graph feature, persistence layout, guard rails, multi-layer conventions | `GRAPH.md` |
| Read-only web interface (server behaviour, routes, pages, security model) | `WEB.md` |
| Graph store directory (`graph/` subdir) | `GRAPH.md § Persistence Layout` (layout referenced from `ARCHITECTURE.md § Directory Structure`) |
| Graph query result JSON and property-type mapping | `DATA_FORMATS.md § Graph Query Result` |
| Web graph view-data JSON shape | `DATA_FORMATS.md § Graph View Data` |
| Board search text preparation (the trim, normalisation, and folding rules; the single implementation of each; the tables shipped to the browser) | `WEB.md § Roadmap Tasks Page` |
| Web UI framework (Tabler admin shell, dark theme) | `WEB.md § UI Framework` |
| Vendored web assets / embedded Tabler framework and D3.js (with d3-sankey) | `BUILD.md § Vendored Web Assets` |
| Graph store concurrency / writer serialisation / reader locking / recovery | `IMPLEMENTATION.md § Graph Store Concurrency` (contract in `GRAPH.md § Concurrency and Recovery`) |
| Graph store lock file (`write.lock`) | `GRAPH.md § Concurrency and Recovery` (layout in `GRAPH.md § Persistence Layout`) |
| Cypher engine constructor per path (`cypher.NewEngine` on the read path, `cypher.NewEngineWithStore` on the write path) | `GRAPH.md § Engine Constructor by Path` |
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
