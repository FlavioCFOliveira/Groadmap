# Changelog

All notable changes to **Groadmap** (`rmp`) are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.9.0] - 2026-06-03

Redesigns the read-only `rmp web` interface and restructures its navigation. The
web UI is rebuilt on a shared Tabler-based layout with a D3.js knowledge-graph
viewer (replacing the previous Cytoscape.js renderer), and the roadmap view is
split into a dedicated Sprints landing page and a separate Tasks page served from
a new `/roadmaps/{name}/tasks` endpoint. Authored line breaks in sprint and task
free-text are now preserved verbatim. Every change is additive or
presentation-only: the `rmp` CLI remains the sole write path, the read-only
contract is unchanged, and there are no CLI, JSON output, exit-code, or database
schema changes. Under Semantic Versioning 2.0.0 this is a **MINOR** release, fully
backward compatible with `v1.8.2`. The database schema version is unchanged at
`1.6.0`.

### Added

- **Dedicated sprint page** — a new read-only page at
  `/roadmaps/{name}/sprints/{id}` presents a single sprint and its member tasks.
  The id is validated in the handler; an unknown or non-integer id returns HTTP
  `404`. Specified in `SPEC/WEB.md` (Routes and Pages).
- **Separate Tasks endpoint** — the roadmap view is split into two pages. The
  landing page (`/roadmaps/{name}`) is now the Sprints page (three sprint tabs,
  the current/`OPEN` sprint expanded by default), and a new
  `/roadmaps/{name}/tasks` endpoint serves the full task list with a per-row
  read-only detail modal. The sidebar links Sprints to `/roadmaps/{name}` and
  Tasks to `/roadmaps/{name}/tasks`. Both endpoints answer `GET`/`HEAD` only and
  keep the existing roadmap-name 404 path guard.

### Changed

- **Read-only web UI redesigned on a shared Tabler layout** — `index`, the
  roadmap pages, and the graph page now extend a single shared base layout
  (HTML skeleton, Tabler-based navbar and shell, favicon, vendored CSS and
  fonts), removing the per-page duplicated boilerplate. The vendored asset set is
  updated accordingly: the Cytoscape.js bundle is removed and a new vendor bundle
  (D3 with d3-sankey, the Inter web font, the Tabler UI framework and Tabler
  Icons, and the favicon) is embedded with `go:embed`, each bundle's licence
  recorded in `vendor/LICENSES.md`. The interface remains fully self-contained
  and renders offline; the server makes no outbound request.
- **Knowledge-graph viewer rebuilt on D3.js** — the graph renderer is
  reimplemented in D3.js, replacing Cytoscape.js. A force-directed layout is the
  default, with the D3 "Networks" gallery layouts (including the d3-sankey flow
  layout) selectable from a dropdown. Graph data is fetched once and re-rendered
  in memory on layout change, with no re-fetch (`SPEC/WEB.md` FR7, AC10).
- **Authored line breaks preserved in sprint and task free-text** — sprint
  descriptions and task long-text now retain their authored newlines instead of
  collapsing them under HTML's default whitespace handling. A shared `pre-wrap`
  stylesheet rule (`white-space: pre-wrap; word-break: break-word;
  overflow-wrap: anywhere`) covers both the task detail modal and sprint
  descriptions across the Sprints page and the sprint detail page. Output remains
  `html/template`-escaped; no raw HTML is introduced.
- **Project governance** — `CLAUDE.md` gains a "Core Working Principles" section
  (Ask-Never-Assume, Never-Guess, Measure-to-Decide, Production-Grade-by-Default,
  Self-Contained Development, and the Specify -> Implement -> Test -> Document
  workflow), reinforced across the Decision Matrix and Anti-Patterns sections.
  Documentation and process only; no code or SPEC impact.

### Tests

- Web E2E suite extended for the redesigned UI: the shared layout chrome, the new
  sprint page (valid id and 404 paths), the D3 graph assets, the Sprints/Tasks
  split, the Tasks page with HTML escaping, the new sidebar links, and the
  verbatim survival of multi-line sprint and task free-text in the served HTML.
- New Go unit tests cover the shared layout rendering and the sprint page
  (`internal/web/layout_test.go`, `internal/web/sprint_test.go`); `web_test.go`
  is updated for the D3 asset set and the new routes.
- All validation gates pass against the freshly built binary (fmt / vet / test
  under the race detector / build / lint clean).

[1.9.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.8.2...v1.9.0

## [1.8.2] - 2026-06-03

Corrects a silent result-truncation defect in large multi-task fetches and
activates a query-caching optimisation that was specified but dormant. Both
changes are backward-compatible bug fixes: existing commands now return the
complete data they were always meant to return, and a mandated internal
optimisation finally runs. No new commands, flags, JSON output fields, exit
codes, or database schema changes, so this is a **PATCH** release fully
compatible with `v1.8.1`. The database schema version is unchanged at `1.6.0`.

### Fixed

- **Large task fetches and batch updates no longer truncate to the first 1000
  ids** — multi-id task operations built a single `WHERE id IN (...)` clause and
  passed the id count through `normalizeSize`, which caps at 1000. Result sets
  with more than 1000 matching tasks (for example `task list` over a large
  roadmap) were silently truncated to the first 1000 rows, and batch status,
  priority, and sprint-membership updates could miss tasks beyond the cap. Every
  multi-id path now sorts a copy of the id set and chunks it through the
  `BatchProcessor` (`ProcessChunks` / `ProcessChunksWithResult`), so results are
  complete and each generated statement stays within SQLite's per-statement
  variable limit. The caller's slice is never mutated, and chunks are processed
  in deterministic id order.
- **Query cache reconciled with the real schema and activated** — the
  `QueryCache` templates referenced a fictional schema (columns that never
  existed in the `tasks` table), so the cached query plans could not be used and
  the optimisation mandated by `SPEC/IMPLEMENTATION.md` (§ Query Caching) was
  effectively dead. The templates are now generated from a single
  `buildTemplates` source of truth shared by the pre-generation and on-demand
  paths, byte-identical in semantics to the real production queries (full task
  projection with dependency columns, subtask count, and the `ORDER BY t.id`
  tail). The cache and batch path are now genuinely active, so repeated batch
  operations reuse prepared query plans instead of rebuilding them.

### Performance

- **Repeated batch task operations reuse cached query plans** — with the query
  cache reconciled and activated (see Fixed), `GetTasks` and the batch
  status/priority/sprint-membership updates now fetch a cached, chunk-sized
  template per operation instead of formatting a fresh SQL string on every call.
  The optimisation is internal and changes no observable output; it reduces
  per-call query construction on hot batch paths.

### Removed

- **Dead code in `internal/db` and `internal/commands`** — removed ten unused
  `internal/db` functions (parent/subtask, task-dependency, and max-position
  helpers superseded by current code paths) and the unused `HandleBacklog` and
  `HandleGraph` command wrappers (command dispatch already routes through the
  central registry). No behaviour change; these paths were unreachable.

### Tests

- **Nine dormant E2E suites revived and a dormancy guard added** — nine
  `tests/test_*.py` files existed on disk but were never registered in
  `tests/run_tests.py`, so they never ran and gave a false sense of coverage.
  All nine are now registered, and a new `assert_no_dormant_modules()` guard
  fails the run fast if any `tests/test_*.py` is left unregistered, preventing
  the gap from silently returning. The registered E2E suite grows from 27 to 37
  modules; all 37 pass (100 %) against the freshly built binary.
- **New `task list --created-since` / `--until` coverage** — added
  `tests/test_38_task_list_date_filters.py` exercising the date-range filters on
  `task list`. Two stale tests targeting non-existent features were removed.
- **Web-server coverage is now measurable** — the web server now tears down
  gracefully on `SIGTERM`, so coverage counters flush on shutdown. New
  `internal/web` unit tests (`data_test.go`, `handlers_test.go`,
  `server_test.go`) and new `internal/db` tests (`batch_test.go`,
  `query_cache_test.go`) accompany the fixes above.

### Tooling

- **New coverage targets** — `make cover` reports unit-test coverage, and
  `make cover-full` builds an instrumented binary, drives it through the E2E
  suite, and merges the result with unit coverage to report the real exercised
  command surface. Merged coverage for this release is **83.9 %**.

[1.8.2]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.8.1...v1.8.2

## [1.8.1] - 2026-06-03

Fixes a rendering defect in the `rmp web` knowledge-graph page introduced with
the web interface in `v1.8.0`. The page rendered the empty-state overlay on top
of a populated knowledge graph, hiding the graph from view. This is a
backward-compatible bug fix that restores the behaviour already specified in
`SPEC/WEB.md` (§ Empty graph). No new features, and no API, exit-code, or schema
changes, so this is a PATCH release fully compatible with `v1.8.0`.

### Fixed

- **`rmp web` graph page now renders the knowledge graph** — the
  `/roadmaps/{name}/graph` page no longer paints the empty-state overlay over a
  populated graph. Root cause: the `.graph-empty { display: flex }` class rule
  outranked the user-agent `[hidden] { display: none }` rule on specificity, so
  the `hidden` empty-state overlay (`position: absolute; inset: 0;` with an
  opaque background) was always painted on top of the Cytoscape canvas, hiding
  the graph that had in fact initialised correctly underneath. The fix adds a
  global `[hidden] { display: none !important; }` reset to
  `internal/web/static/style.css`, so the `hidden` attribute always wins over
  component `display` rules. The empty-graph state now appears only when the
  graph is genuinely empty, as specified in `SPEC/WEB.md` (§ Empty graph).

### Tests

- Added the regression test `TestEmbeddedCSS_HiddenAttributeWins` in
  `internal/web/web_test.go`, which asserts the embedded `style.css` carries the
  global `[hidden] { display: none !important }` rule so the defect cannot
  silently return (no browser required).
- E2E: 24/24 pass (100 % success rate) against the freshly built binary.

[1.8.1]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.8.0...v1.8.1

## [1.8.0] - 2026-06-03

Adds the read-only `rmp web` interface and aligns the CLI exit-code mapping with
the canonical `SPEC/ARCHITECTURE.md` contract. Both changes are additive or
corrective: the web interface is a new command and the exit-code change only
affects error paths the SPEC already defined. No existing JSON output schema
changes, so this remains backward compatible with `v1.7.0`.

### Added

- **`rmp web` command** — a read-only, self-contained, mobile-first web
  interface for browsing every roadmap under `~/.roadmaps/`, its tasks and
  sprints, and an interactive knowledge-graph visualisation. Specified in
  `SPEC/WEB.md`.
  - Serves server-rendered HTML and a JSON graph-data endpoint over the Go
    standard-library `net/http`; routes answer `GET`/`HEAD` only (any other
    method returns HTTP `405`).
  - Routes: roadmap index (`/`), roadmap detail (`/roadmaps/{name}`),
    knowledge-graph page (`/roadmaps/{name}/graph`), graph data
    (`/roadmaps/{name}/graph/data`), and embedded static assets (`/static/...`).
    Roadmap names from the URL are validated before any path is built; an
    invalid or unknown name returns HTTP `404`.
  - **Self-contained** — every asset (HTML templates, stylesheet, all client
    JavaScript including the vendored Cytoscape.js graph library, favicon) is
    embedded with `go:embed`; the interface renders fully offline and the
    server makes no outbound request.
  - **Read-only** — exposes no route that creates, edits, or deletes data; the
    graph store is opened read-only and a web read never triggers a checkpoint
    or write-ahead-log truncation. The `rmp` CLI remains the sole write path.
  - **Loopback by default** — binds `127.0.0.1:8787`; a non-loopback bind
    (`--host 0.0.0.0`) is an explicit opt-in. When `--port` is omitted and the
    default port is in use, the server falls back to an OS-chosen ephemeral
    port so it still starts.
  - Flags: `--host`, `--port`, `--no-open`, and `-h`/`--help`. The process is
    long-lived: `SIGINT`/`SIGTERM` shut it down gracefully (exit 0). It is the
    one command exempt from the always-required-roadmap rule and accepts no
    `-r`/`--roadmap` flag and no subcommands.

### Fixed

- **CLI exit-code mapping aligned with `SPEC/ARCHITECTURE.md`** —
  `ErrInvalidInput` now maps to exit `2` (misuse: unknown flags and
  subcommands, malformed or non-numeric IDs), while value, range, enum, date,
  state-transition and business-rule validations are reclassified to
  `ErrValidation` so they remain exit `6` (invalid data). This resolves the
  first item under the `v1.7.0` "Known Issues" (the `ErrInvalidInput`
  exit-code divergence).

### Tests

- E2E: 24/24 pass (100 % success rate) against the freshly built binary,
  including the new `tests/test_35_web_interface.py` suite covering the server,
  every route and method, the read-only guarantee, path-traversal validation,
  and graceful shutdown.
- Go unit tests green across all packages (fmt / vet / test / build / lint
  clean), including the new `internal/web` package tests.

[1.8.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.7.0...v1.8.0

## [1.7.0] - 2026-06-02

Feature release. Introduces the `rmp graph` command family: a per-roadmap
knowledge graph backed by the external GoGraph engine, accessed through Cypher
and exposed via five guard-railed subcommands (`create`, `query`, `update`,
`delete`, `search`). Each write is made durable through a synchronous
checkpoint that snapshots the committed state and truncates the write-ahead log
before the process exits. The graph is an additive capability: the existing CLI
surface, exit codes, and all JSON output schemas remain fully backward
compatible with `v1.6.0`, so this is a MINOR release under SemVer.

### Added

- **`rmp graph` command family** — a per-roadmap knowledge graph for recording
  and querying the project's elements and their relationships. Specified in
  `SPEC/GRAPH.md`. Five subcommands, each accepting Cypher via `--query` or
  standard input:
  - `graph create` — execute write Cypher that adds nodes and edges.
  - `graph query` — execute read Cypher and return `columns`/`rows` as JSON.
  - `graph update` — execute write Cypher that modifies existing elements.
  - `graph delete` — execute write Cypher that removes elements; deletions are
    durable tombstones that survive store reopen.
  - `graph search` — execute read Cypher tailored to lookup/traversal queries.
- **Guard-rail validation** — every subcommand validates that the supplied
  Cypher matches its operation class (read vs. write) before execution, so a
  read subcommand cannot mutate the graph and a write subcommand cannot be used
  to bypass the intended access pattern.
- **Cypher input precedence** — each subcommand reads its query from the
  `--query` flag when present, otherwise from standard input, enabling both
  inline invocation and piped/heredoc usage.
- **Synchronous checkpoint on write** — after a write subcommand commits its
  transaction durably, and before the process exits, the implementation
  produces a self-sufficient on-disk snapshot of the committed graph state and
  truncates the write-ahead log within the same invocation. This bounds WAL
  growth and keeps recovery cost proportional to the live graph size rather
  than to the total history of writes. Read subcommands never checkpoint.
- **Per-roadmap graph store** — each roadmap owns one graph, persisted under
  `~/.roadmaps/<name>/graph/`, independent of the roadmap's `project.db`. Graph
  operations never read or write the SQLite database.
- **Multigraph support** — parallel edges between the same pair of nodes are
  supported, allowing multiple distinct relationships to coexist.

### Changed

- **Go toolchain directive**: `go.mod` raised from `go 1.26.2` to `go 1.26.4`.
  CI and release workflows derive the Go version from `go.mod` via
  `go-version-file`, so no workflow edit was required.
- **`SPEC/VERSION.md` `Current Version` table**: corrected the stale
  Application entry (was `v1.2.1`) to reflect the real state
  (Application `v1.7.0`, Database Schema `v1.6.0`), and updated the illustrative
  `const version` snippet to match.

### Dependencies

- **GoGraph** added as a direct dependency, pinned at the exact tag `v0.1.0`
  (a pre-1.0 release consumed directly via `go get`, with no pseudo-version).
  GoGraph provides the labelled property graph, the Cypher engine, the durable
  on-disk store, durable node tombstones (deletes survive reopen), and
  multigraph parallel edges that back the `rmp graph` command.

### Tests

- Go unit tests: 6 packages, all green (fmt / vet / test / build / lint clean).
- E2E: 23/23 pass (100 % success rate) against the freshly built `v1.7.0`
  binary on the Go 1.26.4 toolchain.
- Two new E2E suites added for the graph command:
  - `tests/test_33_graph_checkpoint.py` — verifies the synchronous
    snapshot-and-WAL-truncate checkpoint contract on every write.
  - `tests/test_34_graph_realistic_usage.py` — exercises 219 graph calls in a
    realistic modelling scenario, including multigraph parallel edges.

### Known Issues

The two SPEC-vs-code divergences carried forward from prior releases remain
open and are unaffected by this release:

1. **Exit-code mapping for `ErrInvalidInput`** — `SPEC/ARCHITECTURE.md`
   documents exit code `2`; the implementation uses `ExitInvalidData = 6`.
2. **`audit stats` JSON keys** — `SPEC/COMMANDS.md` documents one set of keys;
   the implementation emits a different (stable) set.

Both are tracked as `spec` / `tech-debt` GitHub issues and will be resolved by
a `specification-manager` pass in a future release.

[1.7.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.6.0...v1.7.0

## [1.6.0] - 2026-06-01

Maintenance release. Updates the Go toolchain directive to `1.26.2`, upgrades
all GitHub Actions to their latest patch releases, and bumps transitive Go
module dependencies. Removes the stale Windows binary references from the
release workflow body (Windows builds were dropped in v1.5.0 when GoGraph's
`syscall.Kill` dependency made cross-compilation to Windows infeasible). No
new features, no breaking changes; the public CLI surface, exit codes, and
all JSON output schemas remain fully backward compatible with `v1.5.0`.

### Changed

- **Go toolchain directive**: `go.mod` raised from `go 1.26` to `go 1.26.2`.
- **GitHub Actions — `actions/checkout`**: v6 → v6.0.2.
- **GitHub Actions — `actions/setup-go`**: v6.3.0 → v6.4.0.
- **GitHub Actions — `actions/upload-artifact`**: v7 → v7.0.1.
- **GitHub Actions — `actions/download-artifact`**: v7/v8 → v8.0.1
  (unified to a single version across `ci.yml` and `release.yml`).
- **GitHub Actions — `golangci/golangci-lint-action`**: v8 → v9.2.1.
- **GitHub Actions — `codecov/codecov-action`**: v5.5.3 → v6.0.1.
- **GitHub Actions — `softprops/action-gh-release`**: v2.6.1 → v3.0.0.
- **Go module `google/go-cmp`**: v0.6.0 → v0.7.0 (indirect).
- **Go module `golang.org/x/text`**: v0.22.0 → v0.37.0 (indirect).
- **Go module `modernc.org/cc/v4`**: v4.28.2 → v4.28.4 (indirect).
- **Go module `modernc.org/ccgo/v4`**: v4.34.2 → v4.34.4 (indirect).
- **Release workflow body**: stale Windows binary download links removed
  from the `dev-release` step; Windows targets were already dropped in
  a prior commit due to GoGraph's `syscall.Kill` dependency.

### Tests

- Go unit tests: 6 packages, all green (fmt / vet / test / build / lint clean).
- E2E: 21/21 pass (100 % success rate).
- `gosec` not installed on the release host; security gate skipped and noted
  per project policy. No security-relevant code changes in this release.

### Known Issues

The two SPEC-vs-code divergences flagged in earlier releases remain open and
are unchanged by this release:

- `SPEC/ARCHITECTURE.md` documents `ErrInvalidInput` mapping to exit code `2`;
  the implementation maps `ErrInvalidInput`, `ErrValidation` and
  `ErrFieldTooLarge` to `ExitInvalidData = 6`.
- `SPEC/COMMANDS.md` `audit stats` JSON keys differ from the implementation
  (`by_operation` / `by_entity_type` / `first_entry_at` / `last_entry_at` /
  `total_entries`, no `period` object). Implementation behaviour is stable.

No E2E tests cover `rmp graph` subcommands. Coverage will be added in a
follow-up release.

[1.6.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.5.0...v1.6.0

## [1.5.0] - 2026-06-01

Minor release. Introduces the **`rmp graph` command**: a Cypher-queryable
knowledge graph per roadmap, backed by the GoGraph engine and persisted under
`~/.roadmaps/<name>/graph/`. Adds the **per-roadmap directory layout** with
automatic legacy migration, moving each roadmap's SQLite database from a flat
`~/.roadmaps/<name>.db` file to `~/.roadmaps/<name>/project.db` inside its own
home directory. The AI Agent Contract gains two new registry fields
(`stdin_fallback`, `reads_stdin`), a new `build_knowledge_graph` workflow, and
two new pitfall entries. No breaking changes; the public CLI surface, exit codes
and existing JSON output schemas remain backward compatible with `v1.4.0`.

### Added

- **`rmp graph` command** (`internal/commands/graph.go`,
  `internal/commands/registry_graph.go`): five subcommands backed by the
  GoGraph Cypher engine:
  - `graph create` — executes `CREATE`/`MERGE` queries to add nodes or edges.
  - `graph query` — executes read-only `MATCH ... RETURN` queries.
  - `graph update` — executes `SET`/`REMOVE` queries to mutate existing elements.
  - `graph delete` — executes `DELETE`/`DETACH DELETE` queries to remove elements.
  - `graph search` — executes read-only traversal queries (variable-length paths).
  Each subcommand is a guard rail: it rejects a Cypher query whose operation class
  does not match the subcommand, exiting with code `6` before touching the graph.
  The `--query` flag falls back to reading from standard input when absent.
  The graph store is rooted at `~/.roadmaps/<name>/graph/` (mode `0700`),
  created on first use, and is durable via GoGraph's WAL.
- **Per-roadmap directory layout** (`internal/utils/path.go`): each roadmap is
  now stored under `~/.roadmaps/<name>/` (mode `0700`) with the SQLite database
  at `~/.roadmaps/<name>/project.db` (mode `0600`).
- **Automatic legacy migration** (`internal/utils/migrate.go`):
  `MigrateLegacyLayout` runs at startup and atomically renames any flat
  `~/.roadmaps/<name>.db` file into `~/.roadmaps/<name>/project.db`. The
  migration is idempotent, skips symbolic links and invalid names, and handles
  WAL/SHM sidecars best-effort. An existing `project.db` is never overwritten.
- E2E test `test_32_layout_migration.py`: end-to-end coverage of the migration
  (data preservation, permissions, idempotent re-run, conflict resolution,
  symlink security guard).
- Go unit tests in `internal/utils/migrate_test.go`: happy-path, idempotent
  no-op, conflict, empty home directory recovery, invalid-name skip, and symlink
  guard scenarios.
- AI contract field `stdin_fallback` on `FlagEntry`: projected from registry's
  `Flag.StdinFallback`; omitted when false.
- AI contract field `reads_stdin` on `SubcommandEntry`: projected from registry's
  `Subcommand.ReadsStdin`; omitted when false.
- AI contract workflow `build_knowledge_graph`: a four-step guide for populating
  and querying a roadmap's knowledge graph with Cypher.
- AI contract pitfall `graph_guard_rail_mismatch`: documents the exit-6 guard
  rail and shows the correct subcommand for each operation class.
- AI contract pitfall `graph_missing_query`: documents the stdin-fallback
  behaviour and the failure mode when neither `--query` nor stdin is supplied.
- `SPEC/GRAPH.md` (new): complete specification for the graph command —
  persistence layout, guard-rail rules, Cypher input source precedence, output
  schemas, exit codes, and security model.

### Changed

- Storage layout: roadmap databases moved from `~/.roadmaps/<name>.db` to
  `~/.roadmaps/<name>/project.db`. Existing databases are migrated automatically
  on the first run of any command (other than `--help`, `--version`,
  `--ai-help`). No manual action is required.
- `go.mod`: GoGraph (`github.com/FlavioCFOliveira/GoGraph`) promoted from
  indirect to direct dependency at `v0.0.0-20260601121207-03162239610a`; `go`
  directive raised to `1.26`.
- AI contract tool description updated to mention the Cypher-queryable knowledge
  graph capability (`cmd/rmp/aihelp_wiring.go`).
- `internal/commands/registry.go`: `Flag` gains `StdinFallback bool`;
  `Subcommand` gains `ReadsStdin bool`.
- `internal/commands/registry_data.go`: `graph` family registered in the
  declarative command registry.
- `internal/commands/roadmap.go`: `list` output now reports
  `<name>/project.db` paths; `remove` deletes the whole `<name>/` home
  directory.
- `internal/db/connection.go`: `Open` ensures the roadmap home directory before
  opening `project.db`.
- SPEC updated: `ARCHITECTURE.md`, `COMMANDS.md`, `DATA_FORMATS.md`, `BUILD.md`,
  `HELP.md`, `IMPLEMENTATION.md`, `README.md`, `DATABASE.md`, `DEPLOY.md`,
  `VERSION.md`.

### Tests

- E2E: 21/21 pass (`test_32_layout_migration.py` added; no E2E coverage for
  `rmp graph` subcommands yet — tracked as a follow-up).
- Go unit tests: 6 packages, all green (fmt/vet/test/build/lint clean).

### Known Issues

The two SPEC-vs-code divergences flagged in earlier releases remain open and are
unchanged by this release:

- `SPEC/ARCHITECTURE.md` documents `ErrInvalidInput` mapping to exit code `2`;
  the implementation maps `ErrInvalidInput`, `ErrValidation` and
  `ErrFieldTooLarge` to `ExitInvalidData = 6`.
- `SPEC/COMMANDS.md` `audit stats` JSON keys differ from the implementation
  (`by_operation` / `by_entity_type` / `first_entry_at` / `last_entry_at` /
  `total_entries`, no `period` object). Implementation behaviour is stable.

No E2E tests cover `rmp graph` subcommands in this release. Coverage will be
added in a follow-up release.

[1.5.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.4.0...v1.5.0

## [1.4.0] - 2026-05-25

Minor release. Introduces the **AI Agent Contract**: a machine-readable JSON
description of every command, flag, exit code, JSON output shape, common
workflow and known pitfall, exposed via `rmp --ai-help` and `rmp ai-help`.
The release adds proactive discovery hints (a banner on every `--help` page,
a stderr hint on every error, and an opt-in `AI_AGENT=1` environment-variable
hint) so that AI agents that invoke `rmp` always find the contract entry
point. Internally, command dispatch is now driven by a declarative command
registry, eliminating the long-standing handcrafted switch chains. The
`sprint` description length cap is raised from 500 to 2048 characters. No
breaking changes; the public CLI surface, exit codes and existing JSON
output schemas remain backward compatible with `v1.3.0`.

### Added

- **AI Agent Contract** (`internal/aihelp`): a complete machine-readable
  contract describing every command family, subcommand, flag, alias,
  enum, exit code, JSON output shape, plus canonical `common_workflows`
  and `pitfalls`. The contract is generated from the same registry that
  drives runtime dispatch, so it cannot drift from the binary.
- `rmp --ai-help` global flag and `rmp ai-help` command emit the contract
  to stdout. The flag takes precedence over `--help`, `--version`, `-r`
  and every action flag. Scoping is supported: `rmp task --ai-help`,
  `rmp sprint create --ai-help` and equivalents return the relevant
  contract slice.
- AI-agent discovery banner prepended to every `--help` page (main help
  and every family/subcommand help), pointing agents at `rmp --ai-help`.
- AI-agent stderr hint emitted on the error path
  (`Error: ...` followed by a hint pointing at `rmp --ai-help`).
- Opt-in `AI_AGENT=1` environment-variable mode: when active, the
  discovery hint is the first line written to stderr for the entire
  invocation, with a `sync.Once`-guarded dedup so it appears exactly
  once even when both the env-var path and the error path are involved.
  The hint is intentionally suppressed when the invocation itself is
  serving the contract.
- Declarative **command registry** (`internal/commands/registry*.go`):
  command families, subcommands, aliases, and handlers are now declared
  as data. `cmd/rmp/main.go` dispatches through the registry and the
  AI Agent Contract is generated from the same source.
- E2E test `test_30_aihelp_contract.py`: exhaustive coverage of the
  contract surface (579 lines) including precedence, scoping,
  exit codes, JSON schema invariants and discoverability.
- E2E test `test_31_sprint_description_limit.py`: exhaustive coverage
  of the new sprint description length boundary (178 lines).
- Go unit-test suites for the AI Agent Contract generator, registry,
  banner, hint emission and the `--ai-help` wiring layer.
- `DOCS/commands/ai-help.md`: complete reference page for the AI Agent
  Contract feature.
- README section surfacing `--ai-help` for human discovery.

### Changed

- `sprint` description maximum length raised from **500** to **2048**
  characters (`internal/models/consts.go`, SPEC/DATABASE.md,
  SPEC/MODELS.md). Existing rows are unaffected; only the validator
  upper bound changes. No schema migration required.
- Command family dispatch (`task`, `sprint`, `roadmap`, `audit`,
  `backlog`) now flows through the declarative registry rather than
  per-family switch statements. Public CLI behaviour is unchanged.
- The AI-agent stderr hint replaces the previous silent error path: every
  error message is now followed by `AI agents: run `+"`rmp --ai-help`"+`
  for a machine-readable command contract.` (suppressed when the
  contract itself is being emitted).

### Documentation

- New SPEC pages: SPEC/COMMANDS.md (AI Help section), SPEC/HELP.md
  (banner, error hint, AI_AGENT env var), SPEC/ARCHITECTURE.md
  (contract subsystem), SPEC/DATA_FORMATS.md (contract JSON schema).
- README and `DOCS/commands/` updated to surface the new feature.

### Known Issues

The two SPEC-vs-code divergences flagged in v1.3.0 remain open and are
unchanged by this release:

- `SPEC/ARCHITECTURE.md` documents `ErrInvalidInput` mapping to exit
  code `2`; the implementation maps `ErrInvalidInput`, `ErrValidation`
  and `ErrFieldTooLarge` to `ExitInvalidData = 6`.
- `SPEC/COMMANDS.md` `audit stats` JSON keys differ from the
  implementation (`by_operation` / `by_entity_type` /
  `first_entry_at` / `last_entry_at` / `total_entries`, no `period`
  object). Implementation behaviour is stable and adopted by tooling.

[1.4.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.3.0...v1.4.0

## [1.3.0] - 2026-05-13

Minor release. Adds GNU-style `--flag=value` parsing across every command and
introduces per-subcommand `--help` for the `roadmap`, `task`, `sprint`, `audit`
and `backlog` families. Several internal performance and refactoring passes
reduce SQL round-trips and standardise data-access patterns. Test coverage
grows substantially (E2E and Go unit tests) and the SPEC has been reorganised
for single-responsibility files. No breaking changes; the public CLI surface,
exit codes and JSON output schemas remain backward compatible with `v1.2.1`.

### Added

- GNU-style `--flag=value` syntax is now accepted across all commands, in
  addition to the existing space-separated form.
- Per-subcommand `--help` for every command in the `roadmap`, `task`,
  `sprint`, `audit` and `backlog` families, backed by a shared `hasHelpFlag`
  helper.
- Family help texts now enumerate valid enum values (status, priority, type),
  document previously-omitted flags, distinguish similar listing commands,
  describe their JSON outputs, document conditional flags and workflow rules,
  and list exit codes per command family.
- Go unit tests for `sprint open-tasks`.
- E2E coverage for backlog `list` and `show-next`, task dependency workflows,
  subtask and dependency completion guards, command-alias surface, the
  `4096`-byte field-length boundary, exit codes `127` (unknown command) and
  `130` (SIGINT), `reopen` lifecycle-field clearing, subprocess-level
  parallel `rmp` invocations, JSON schema shape assertions, and
  timing-realistic burndown and velocity scenarios.

### Changed

- The Go source uses the `any` alias instead of `interface{}` across the
  whole project for readability.
- `GetAuditEntries` now accepts an `AuditFilter` struct rather than a long
  positional argument list.
- Position and swap mutations are now executed through `WithTransaction`,
  unifying transactional boundaries for ordering operations.
- Inline status string literals in SQL fragments have been replaced with
  model-backed constants.
- Audit-row inserts have been consolidated into a single `LogAuditTx` helper.
- The `release-notes/` directory and this `CHANGELOG.md` are introduced for
  the first time; future releases will follow the same layout.

### Fixed

- Help texts no longer contain factual errors; wording is standardised across
  families and aligned with the actual runtime behaviour (exit codes,
  required vs optional flags, conditional flags).
- `isLockedError` now uses a structured `errors.As` check instead of
  string matching.
- Update statements emit fields in a deterministic sorted order to make
  generated SQL stable.
- Roadmap-name validation errors are now wrapped in `ErrValidation`.
- Code and SPEC are aligned across the twelve audit findings raised during
  the SPEC v2.0.0 consolidation.
- Lint: replaced the deprecated `reflect.Ptr` with `reflect.Pointer` in
  `flags.go`.

### Performance

- `GetAuditStats` collapsed into a single `GROUP BY` query.
- `GetAuditEntries` query construction uses `strings.Builder` to avoid
  intermediate string allocations.
- Task dependencies are now resolved with `group_concat` to eliminate the
  per-task N+1 query.
- `hasTransitiveDependency` uses a recursive Common Table Expression instead
  of an application-side breadth-first search.
- Subtask and dependency completion guards run as bulk queries inside
  `task_mutate`.
- `AddTasksToSprint` uses a single multi-row `INSERT`.
- `roadmapList` uses `entry.Info()` to skip a per-file `stat` syscall.
- `ValidateIDString` uses `strconv.Atoi` for ID parsing.

### Refactored

- `db.ConnectionCache` and the related `atexit` hooks were removed; nothing
  consumed them and the lifecycle was a source of confusion.
- The retry wrapper around `sql.Open` was dropped (the driver already
  retries lazy connections on first use).
- `ValidateNumericRange` helper added in `internal/utils` for bounded
  integer parsing.
- `ParseCommaSeparatedIDs` helper extracted in `internal/utils`.
- The unused string-match fallback in `handleError` was removed.
- `task_mutate` now reuses the cached `db.Placeholders` lookup.

### Tests

- E2E weak assertions of the form `!= 0` have been replaced with explicit
  exit-code and error-message checks (ten call-sites).
- Python test artefacts (`__pycache__`, `*.pyc`) are now git-ignored.
- New `perfsprint` lint compliance for the `sprint open-tasks` Go tests.

### Documentation

- The `SPEC/` directory has been reorganised into single-responsibility
  files optimised for LLM navigation, with versioning and change-history
  removed in favour of git as the source of truth.
- `SPEC/HELP.md` was refreshed to match the new per-subcommand help
  structure.
- The `.claude/` agent and skill references were updated after the
  project-local skill cleanup.

### Known Issues

Two SPEC-vs-code divergences remain open and are tracked as follow-up
issues so a future `specification-manager` pass can decide how to reconcile
them. Neither divergence affects runtime behaviour:

- `SPEC/ARCHITECTURE.md` documents `ErrInvalidInput` mapping to exit code
  `2`; the implementation in `cmd/rmp/main.go` maps `ErrInvalidInput`,
  `ErrValidation` and `ErrFieldTooLarge` to `ExitInvalidData = 6` and the
  help texts reflect the implementation.
- `SPEC/COMMANDS.md` documents the `audit stats` JSON keys
  `operations_count`, `entity_type_count`, `first_entry`, `last_entry`
  and a `period.{since,until}` block; the implementation emits
  `by_operation`, `by_entity_type`, `first_entry_at`, `last_entry_at`,
  `total_entries` with no `period` object.

[1.3.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.2.1...v1.3.0
