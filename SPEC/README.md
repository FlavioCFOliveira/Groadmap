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
| Web roadmap tasks page (`/roadmaps/{name}/tasks`, full task table) | `WEB.md § Roadmap Tasks Page` |
| Web sprint page (`/roadmaps/{name}/sprints/{id}`) | `WEB.md § Roadmap Sprint Page` |
| Web shared sprint presentation sub-template (status summary line, detail block shared by sprint page and Actual tab) | `WEB.md § Shared Sprint Presentation Sub-Template` |
| Web task detail modal (read-only task popup) | `WEB.md § Task Detail Modal` |
| Web startup schema migration (automatic, no-input, before serving) | `WEB.md § Startup Schema Migration` |
| `rmp web` command syntax / flags | `COMMANDS.md § Web Interface` |
| Web graph data endpoint JSON shape | `DATA_FORMATS.md § Graph View Data` |
| Self-contained web binary (offline, no CDN, embedded asset categories) | `WEB.md § Self-Contained Deliverable` |
| Responsive / mobile-first web design | `WEB.md § Responsive and Mobile-First Design` |
| Web UI framework (Tabler admin shell, dark theme) | `WEB.md § UI Framework` |
| Web HTTP security headers (CSP, X-Frame-Options, etc.) | `WEB.md § Security Headers` |
| Web HTTP server timeouts (read-header, write, idle) | `WEB.md § HTTP Server Timeouts` |
| Vendored web assets / embedded Tabler framework and D3.js (with d3-sankey) | `BUILD.md § Vendored Web Assets` |
| Free-text control-character constraint (CWE-150 / Trojan Source) | `MODELS.md § Free-Text Control-Character Constraint` |
| Audit result-set cap (`MaxAuditLimit`) | `DATABASE.md § Audit Result Limit` |
| Migration idempotency (ALTER TABLE ADD COLUMN guard) | `DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)` |
| `graph` command syntax / subcommands | `COMMANDS.md § Graph Management` |
| Graph query result JSON / property-type mapping | `DATA_FORMATS.md § Graph Query Result` |
| Cypher input via flag or stdin | `GRAPH.md § Cypher Input Source and Precedence` |
| Graph query notifications on stderr (e.g. Cartesian-product warning) | `GRAPH.md § Query Notifications as Diagnostics` |
| Graph store concurrency / recovery | `IMPLEMENTATION.md § Graph Store Concurrency` |
| Go toolchain / external dependencies | `BUILD.md § Go Toolchain` |
| AI agent contract (CLI surface) | `COMMANDS.md § AI Help` |
| AI agent contract (JSON schema) | `DATA_FORMATS.md § AI Agent Contract` |
| AI agent contract (generation) | `ARCHITECTURE.md § AI Agent Contract Generation` |
| `AI_AGENT` env-var behaviour | `HELP.md § AI_AGENT environment variable` |
| Domain models (Task, Sprint, etc.) | `MODELS.md` |
| Memory layout / struct ordering | `MODELS.md § Memory Layout Optimization` |
| State transitions (Task) | `STATE_MACHINE.md § Task State Machine` |
| State transitions (Sprint) | `STATE_MACHINE.md § Sprint State Machine` |
| System design / modules | `ARCHITECTURE.md` |
| Data directory layout / permissions | `ARCHITECTURE.md § Directory Structure` |
| Filesystem safety (no symlink following, CWE-59) | `ARCHITECTURE.md § Security Guarantees` |
| Filesystem layout migration (per-roadmap directories) | `ARCHITECTURE.md § Filesystem Layout Migration` |
| Error handling / sentinel errors | `ARCHITECTURE.md § Error Handling` |
| Exit codes | `ARCHITECTURE.md § Exit Codes` |
| Database schema (DDL) | `DATABASE.md § DDL - Table Creation` |
| SQL queries | `DATABASE.md § Main SQL Queries` |
| Audit operations catalogue | `DATABASE.md § audit Table` |
| Concurrency (WAL, pool, retry) | `IMPLEMENTATION.md § Concurrency Model` |
| Query caching | `IMPLEMENTATION.md § Query Caching` |
| Performance practices | `IMPLEMENTATION.md § Performance Considerations` |
| Application version | `VERSION.md` |
| Schema migrations | `VERSION.md § Migrations` |
| Build / CI / lint | `BUILD.md` |
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
| `BUILD.md` | Build system, cross-compilation, CI/CD |
| `DEPLOY.md` | Installation, distribution, release process |

---

## 3. Canonical Sources

To prevent drift across SPEC files, the following topics have a single authoritative source. Other SPEC files MUST link to the canonical source rather than duplicate its content.

| Topic | Canonical Source |
|-------|------------------|
| Exit codes (numeric values and sentinel names) | `ARCHITECTURE.md § Exit Codes` |
| Sentinel errors and wrapping rules | `ARCHITECTURE.md § Error Handling` |
| Enums (`TaskType`, `TaskStatus`, `SprintStatus`) | `MODELS.md § Enums` |
| Memory layout / struct field ordering | `MODELS.md § Memory Layout Optimization` |
| Task state transitions | `STATE_MACHINE.md § Task State Machine` |
| Sprint state transitions | `STATE_MACHINE.md § Sprint State Machine` |
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
| Graph store concurrency / single-writer / recovery | `IMPLEMENTATION.md § Graph Store Concurrency` |
| Minimum Go version and external dependencies | `BUILD.md § Go Toolchain` |
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
- Individual roadmap databases: `~/.roadmaps/<name>/project.db` with permissions `0600`, created with mode `0600` from the outset (no umask-derived window). The SQLite sidecars `project.db-wal` and `project.db-shm` live alongside and use the same `0600` permissions.
- Neither the data directory nor any roadmap home directory may be a symbolic link; `rmp` refuses to follow a symlink when creating, opening, or migrating a roadmap directory (CWE-59). See `ARCHITECTURE.md § Directory Structure` and `ARCHITECTURE.md § Security Guarantees`.
- Per-roadmap knowledge graph store: `~/.roadmaps/<name>/graph/` (a directory) with permissions `0700`, created on first use of the `graph` command. See `GRAPH.md § Persistence Layout`.
- Roadmaps in the legacy `~/.roadmaps/<name>.db` layout are migrated automatically to the current layout at startup. See `ARCHITECTURE.md § Filesystem Layout Migration`.

### Naming Conventions

- Database columns: `snake_case` (e.g., `created_at`, `functional_requirements`).
- Go structs and fields: `PascalCase` for exported, `camelCase` for unexported.
- CLI commands and flags: lowercase, kebab-case (e.g., `task list`, `--max-tasks`).
- Short flags: single dash, may exceed one character when an unambiguous abbreviation is more readable (e.g., `-fr` for `--functional-requirements`).
