# Changelog

All notable changes to **Groadmap** (`rmp`) are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.12.0] - 2026-06-17

A combined release that pairs read-only `rmp web` interface enhancements with a
full review of the command-line help surface and the machine-readable AI Agent
Contract. The web interface gains a graph query bar, a clickable labels sidebar,
neighbour focus, unified sprint cards, and now surfaces each sprint's title and
execution order. In parallel, every plain-text help printer was revised for
correctness and completeness, the `rmp --ai-help` contract was normalised, and a
set of documented-but-divergent exit codes was corrected so that the runtime now
matches the published contract. The `GoGraph` dependency is bumped to `v0.4.0`.

Under Semantic Versioning 2.0.0 this is a **MINOR** release: it adds
backward-compatible functionality (web features, richer help, contract
completeness) and corrects exit codes to match the already-documented contract.
No JSON success schema is removed or renamed, and the database schema version is
unchanged. The corrected exit codes are aligned to the values the contract and
help already promised, so they restore — rather than break — the documented
behaviour.

### Added

- **Web — graph query bar, labels sidebar, neighbour focus.** The read-only
  `rmp web` graph view gains an interactive query bar, a labels sidebar with
  click-to-highlight, and neighbour-focus navigation, plus unified sprint cards
  for a consistent layout. Specified in `SPEC/WEB.md`.
- **Web — sprint title and execution order surfaced.** The read-only UI now
  displays each sprint's title and its execution order, so the served pages
  reflect the same ordering the CLI uses.
- **`sprint tasks` — `-s` short alias for `--status`.** The `sprint tasks`
  command now accepts `-s` as a short alias for `--status`, matching the
  documented contract.

### Changed

- **Plain-text help revised across all commands.** Every plain-text help printer
  was reviewed and corrected for accuracy, formatting, and completeness, so the
  on-screen help now faithfully describes the runtime behaviour.
- **AI Agent Contract normalised and completed.** `rmp --ai-help` was reworked
  for internal consistency: nested single-action commands are represented
  uniformly; empty arrays are emitted as `[]` rather than `null`; min-only ranges
  no longer carry a misleading `max: 0`; `--max-tasks` advertises its `1-10000`
  range; the `roadmap_flag` web exemption is documented; `sprint tasks` exposes
  `-s`/`--status`; and failure examples are included for commands with non-zero
  exit codes.
- **SPEC reconciled with verified runtime behaviour.** The help-related
  specifications (`SPEC/HELP.md`, `SPEC/COMMANDS.md`, and related files) were
  reconciled with the behaviour confirmed empirically against the binary.

### Fixed

- **Invalid `--type` on list commands now exits 6.** Passing an invalid
  `--type` to `backlog list` and `task list` now exits with code 6 (invalid
  data) instead of 1, matching the documented exit-code contract.
- **Invalid `--status` on list commands now exits 6.** Passing an invalid
  `--status` to `task list`, `sprint list`, `sprint tasks`, and `task stat` now
  exits with code 6 instead of 1.
- **`audit stats` emits `null` for empty-set timestamps.** On an empty result
  set, the first/last timestamps are now emitted as JSON `null` rather than an
  empty string `""`.
- **`sprint bottom` on a missing sprint now exits 4.** Targeting a non-existent
  sprint with `sprint bottom` now exits with code 4 (not found) instead of 6.

### Dependencies

- **`GoGraph` bumped to `v0.4.0`** and module dependencies refreshed
  (`go.mod`, `go.sum`).

### Internal

- **New and extended test coverage.** Adds the help-content unit suite
  (`internal/commands/help_content_test.go`) and the help/exit-code end-to-end
  contract suite (`tests/test_44_help_and_exitcode_contract.py`), and extends the
  AI-contract E2E suite (`tests/test_30_aihelp_contract.py`) to lock in the
  revised help text and contract invariants.

[1.12.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.11.0...v1.12.0

## [1.11.0] - 2026-06-16

A reliability release for the read-only `rmp web` server. It makes `rmp web`
**auto-migrate every served roadmap's SQLite schema once at startup**, before the
HTTP listener binds, so that the web interface can no longer return HTTP 500 when
it is the first command run against a roadmap whose on-disk schema predates a
binary schema bump. The migration runs through the writable `db.Open` path
(idempotent `RunMigrations`, a no-op when the schema is already current) and never
touches the per-request data path, which remains strictly read-only
(`OpenReadOnly` with `query_only`), preserving the read-only invariant (finding
#43). Under Semantic Versioning 2.0.0 this is a **MINOR** release: the change is
additive and fully backward compatible with `v1.10.0`. No `rmp` command, flag,
JSON output, exit code, or on-disk format is altered. The database schema version
is unchanged at `1.8.0`; this release adds no migration of its own and only
applies the already-defined migrations earlier in the `rmp web` lifecycle.

### Added

- **Startup schema migration for `rmp web`** — `serve()` now runs a new
  `migrateRoadmapsAtStartup()` step after `EnsureDataDir` and before the listener
  binds. It enumerates every roadmap via `utils.ListRoadmaps()`, opens each one
  through the writable `db.Open` path (which runs the idempotent
  `RunMigrations`, a no-op when the schema is already current), then closes it.
  A per-roadmap list, open, or migration failure is logged to stderr and is
  **non-fatal**: the server still starts and serves the remaining roadmaps.
  Specified in `SPEC/WEB.md` (§ Startup Schema Migration, Server Lifecycle step 2,
  Acceptance Criteria 41/42) and `SPEC/ARCHITECTURE.md` (§ internal/web).

### Fixed

- **`rmp web` sprints page returned HTTP 500 on a stale schema** — when `rmp web`
  was the first command run after a binary schema bump on a roadmap whose
  `project.db` predated schema `1.7.0`/`1.8.0`, the sprints page
  (`GET /roadmaps/{name}`) failed with HTTP 500 because the read-only query
  referenced columns absent from the stale file (`sprints.title`,
  `sprints.order_index`). The per-request loaders open the database read-only
  (`OpenReadOnly`, `query_only`) and therefore cannot migrate it. Migrating once
  at startup, before any read-only connection is opened, closes this gap and makes
  migration automatic and input-free while keeping every per-request connection
  strictly read-only — no write, no audit row, and no schema change occurs on a
  read (finding #43 preserved).

### Tests

- New regression suite `internal/web/startup_migration_test.go` (6 tests) covering:
  stale-to-current schema migration at startup, the sprints page recovering from
  HTTP 500 to 200, the non-fatal behaviour when one roadmap is broken,
  multi-roadmap migration, idempotency against an already-current schema, and the
  read-only invariant (no audit row and no `schema_version` change across `GET`
  requests).
- The full battery is green this release cycle: `go test -count=1 ./...`
  (7 packages PASS), `gofmt -l .` clean, `go vet ./...` 0 issues,
  `golangci-lint run ./...` 0 issues, `go build` succeeds (`rmp --version` reports
  `1.11.0`), and `python3 tests/run_tests.py` reports **42/42** (100 %).

### Known Issues

One SPEC-vs-code divergence remains open and is unaffected by this release. It does
not affect runtime behaviour and is tracked as a `spec` / `tech-debt` follow-up for
a future `specification-manager` pass:

- `SPEC/COMMANDS.md` documents the `audit stats` JSON keys `operations_count`,
  `entity_type_count`, `first_entry`, `last_entry` and a `period.{since,until}`
  block; the implementation emits `by_operation`, `by_entity_type`,
  `first_entry_at`, `last_entry_at`, `total_entries` with no `period` object.

[1.11.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.10.0...v1.11.0

## [1.10.0] - 2026-06-16

A feature and reliability release. It introduces a required, human-readable
**sprint title** and a unique **sprint execution-order** field; sharpens the
read-only `rmp web` interface (never-stale responses, a shared sprint
presentation, preserved free-text line breaks, and a hardened HTTP server);
surfaces GoGraph query notifications as diagnostics; and lands a 32-finding
reliability/spec-conformance audit and a 23-finding security audit, each with a
permanent regression gate. The knowledge-graph engine GoGraph is upgraded
`v0.2.0` → `v0.3.2`. Under Semantic Versioning 2.0.0 this is a **MINOR** release:
all changes are additive or corrective and remain backward compatible with
`v1.9.1`, with the single migration-bearing exception that `sprint create` now
requires `--title` (see Changed and Upgrade Notes). The database schema advances
to `1.8.0` through two automatic, idempotent migrations; existing installations
upgrade transparently on first run.

### Added

- **Required sprint title** — sprints now carry a human-readable title in
  addition to their description, so a sprint is identifiable at a glance in
  listings and stand-ups. `sprint create` requires `-t/--title` (max 255
  characters); `sprint update` accepts an optional `-t/--title`. The schema adds
  a `title TEXT NOT NULL CHECK(length(title) <= 255)` column (migration to schema
  `1.7.0`, backfilling existing sprints as `Sprint <id>`). `sprint show` output
  gains a `sprint_title` field. Specified across `SPEC/MODELS.md`,
  `SPEC/DATABASE.md`, `SPEC/COMMANDS.md`, `SPEC/DATA_FORMATS.md`, and
  `SPEC/VERSION.md`.
- **Unique sprint execution-order field** — every sprint carries a unique,
  positive execution order. `sprint create` accepts an optional `--order` (auto-
  assigned to `MAX+1` when omitted); `sprint update` may edit it while the sprint
  is `PENDING` or `OPEN` and rejects edits on a `CLOSED` sprint (exit 6). The
  schema adds `order_index INTEGER NOT NULL CHECK(order_index > 0)` plus a unique
  index (migration to schema `1.8.0`, deterministically backfilling `1..N` by
  creation order). Order collisions map to exit 5. `sprint get`/`list` expose the
  `order` field. Specified across `SPEC/MODELS.md`, `SPEC/DATABASE.md`,
  `SPEC/STATE_MACHINE.md`, `SPEC/COMMANDS.md`, `SPEC/HELP.md`,
  `SPEC/DATA_FORMATS.md`, `SPEC/WEB.md`, and `SPEC/VERSION.md`.
- **Graph query notifications as diagnostics** — `rmp graph query`/`search` now
  print each GoGraph query notification (for example the Neo4j-compatible
  `CartesianProductWarning` for a disconnected multi-pattern `MATCH`) as a
  plain-text line to stderr in the form `<Severity> <Code>: <Description>`. The
  stdout success JSON and exit codes are unchanged, so JSON-parsing consumers are
  unaffected. Specified in `SPEC/GRAPH.md`.

### Changed

- **`sprint create` now requires `--title`** — a previously non-existent flag is
  now mandatory on `sprint create`. Scripts that create sprints must pass
  `-t/--title`; calls without it fail with exit 6
  (`required parameter missing: --title`). This is the only backward-incompatible
  command-line change in the release; see Upgrade Notes.
- **Unified sprint presentation in `rmp web`** — a shared `{{sprintDetail}}`
  sub-template renders the full sprint block (completion summary, metadata
  datagrid, and member-task table) identically on the sprint page and the Actual
  tab of the sprints page, so the two views can no longer diverge. The CLI
  `sprint show` report and the web summary now share
  `CategorizeTaskStatus`/`CalculateSprintSummary`/`CompletionPercentage`, so their
  figures are guaranteed consistent. Specified in `SPEC/WEB.md`.
- **GoGraph upgraded `v0.2.0` → `v0.3.2`** — the engine behind `rmp graph` and the
  read-only graph page of `rmp web` adopts upstream robustness, security, Cypher,
  and durability hardening. The release line is API-additive over `v0.2.0`; no
  consumed exported identifier was removed or renamed. v0.3.2 specifically fixes a
  recovery panic present in v0.3.0/v0.3.1 when reopening a `v0.2.0`-written store,
  which is why the pin targets v0.3.2. The indirect `golang.org/x/sys` hash is
  refreshed and the toolchain directive is `go1.26.4`. Specified across
  `SPEC/BUILD.md`, `SPEC/GRAPH.md`, and `SPEC/ARCHITECTURE.md`.

### Fixed

#### Reliability and SPEC conformance (audit findings #39–#63)

- **Concurrent graph-write data loss (#39, CRITICAL)** — `rmp graph` writes now
  take an exclusive, non-blocking file lock for the whole write (open → commit →
  checkpoint → WAL-truncate). Two concurrent writers could otherwise interleave so
  one writer's snapshot checkpoint overwrote the other's committed-but-unseen
  write and then truncated the WAL that still held it, silently losing an
  acknowledged write. Contention now surfaces as exit 1.
- **Per-connection PRAGMAs (#41) and version comparison (#42)** — `foreign_keys`
  and `busy_timeout` are now carried in the SQLite DSN so every pooled connection
  applies them (a one-shot `Exec` had left the second pooled connection with
  `foreign_keys=OFF`, silently disabling `ON DELETE CASCADE`). Migration gating now
  compares versions numerically, so `1.9.0` versus `1.10.0` orders correctly and
  migrations are no longer skipped once a component reaches two digits.
- **Read-only web database access (#43)** — `rmp web` opens roadmap databases with
  `query_only` and without running migrations, so a mere page view can never
  rewrite a stale-schema `project.db`.
- **Task-command correctness (#44–#46, #48)** — `task get`, `task priority`, and
  `task severity` fail fast with exit 4 on unknown IDs (no phantom audit rows,
  no partial mutation in a mixed batch); out-of-range priority/severity returns
  exit 6; a no-field `task edit` is a successful no-op.
- **Sprint task management (#40, #47, #49, #50)** — `sprint remove-tasks` is scoped
  to the named sprint (membership-checked, exit 6 otherwise), resets reverted tasks
  to `BACKLOG` clearing their lifecycle timestamps and completion summary, and
  compacts remaining positions to a contiguous sequence; `move-to` clamps an
  out-of-range position to the end.
- **State-machine and empty-list contracts (#53, #55)** — the `DOING → SPRINT`
  manual transition is forbidden (SPRINT is set exclusively by
  `sprint add-tasks`); empty sprint and audit lists marshal to `[]`, never `null`.
- **SPEC-verbatim messages and exit codes (#54, #58, #59, #60)** — required-
  parameter and roadmap-name error messages match the SPEC canonical text exactly;
  an int64-overflowing all-digit ID is a range failure (exit 6); the data
  directory's `0700` permission is re-applied and verified on every layout
  migration.
- **Global help generated from the registry (#51)** — `rmp --help` builds its
  command list from the single command registry instead of a hardcoded block that
  had silently dropped the `web` command.
- **Atomic sprint add/move-task audit (#65–#67)** — `AddTasksToSprint` and
  `MoveTasksBetweenSprints` write their audit rows inside the same transaction as
  the membership change; sprint capacity is enforced inside the transaction,
  closing a TOCTOU window. `DeleteSprint` and `RemoveTasksFromSprint` run their
  multi-statement mutations in a single transaction so task status and membership
  can never diverge.
- **Idempotent migrations (#68)** — `ALTER TABLE ADD COLUMN` migrations are guarded
  by a `pragma_table_info` column-existence check.
- **Audit catalogue and help cleanup** — the unused `TASK_TYPE_CHANGE` audit
  operation is removed (a task type change is recorded as `TASK_UPDATE`); the
  `audit list --operation` help, SPEC, and code now agree.

### Security (audit findings #64–#87)

- **`rmp web` server hardening (#69–#71, #73, #76)** — added `WriteTimeout` (30 s)
  and `IdleTimeout` (120 s) to mitigate slowloris-style denial of service; `/static/`
  directory requests return 404 (no embedded-tree disclosure); a strict security-
  header set (Content-Security-Policy, `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, `Referrer-Policy: same-origin`) is applied to every
  response; `/graph/data` emits HTML-safe JSON; the default bind host is now
  `127.0.0.1`, and a non-loopback bind prints a network-exposure warning to stderr.
- **Never serve stale data** — `Cache-Control: no-store` is set on every data-
  derived response (all dynamic pages, the `/graph/data` endpoint, and data-state-
  dependent error responses) at the single outermost middleware choke point, so no
  client or intermediary cache can re-present a database/store state that has since
  changed. Immutable `/static/` assets stay cacheable.
- **Symlink following refused (#72, #74, #75)** — the data directory, roadmap
  directories, and the legacy-layout migration are guarded by an `Lstat` symlink
  check, so a pre-placed symlink can no longer redirect a `project.db` write or a
  `0700` `chmod` outside `~/.roadmaps`.
- **Bounded results and secure file permissions (#64, #77, #78)** — `GetAuditEntries`
  hard-caps its result set; a new `project.db` is created with
  `O_CREATE|O_EXCL` mode `0600` before `sql.Open`, eliminating the umask-derived
  world-readable window, and the `-wal`/`-shm` sidecars are `chmod`-ed to `0600`.
- **Input validation hardening (#82–#87)** — CLI free-text inputs reject ASCII
  control characters (except TAB/LF/CR), DEL, and Unicode bidirectional/format code
  points, blocking terminal-escape injection and Trojan Source (CVE-2021-42574);
  specialist names containing the list-separator comma are rejected; audit IDs,
  `--entity-id`, `--limit`, sprint `--max-tasks`, and sprint `move-to` positions are
  bounded.
- **Read-only graph guard-rail rejects DDL (#79, #80)** — the `query`/`search`
  guard-rail rejects `CREATE`/`DROP INDEX|CONSTRAINT` using a case- and whitespace-
  insensitive matcher on the literal-masked query string (GoGraph's own check was
  case/whitespace-sensitive and bypassable). DDL keywords inside string literals are
  not misclassified, so legitimate read queries and the knowledge-graph memory store
  still pass.

### Documentation

- `CLAUDE.md` gained "Separation of Responsibilities", prioritization, and
  "Knowledge Graph as Memory" working principles (internal, contributor-facing).
- `SPEC/IMPLEMENTATION.md` removed an unimplemented "Connection Caching" section
  so the specification reflects the real code (#63); four further SPEC-vs-code
  contradictions were reconciled (#56, #61, #62).
- `DOCS/commands/sprint.md` and `DOCS/commands/audit.md` synced with the new
  `--title`/`--order` flags and the trimmed audit-operation catalogue.

### Tests

- New E2E suites: `tests/test_40_graph_notifications.py`,
  `tests/test_41_graph_concurrency_input.py`,
  `tests/test_42_security_audit.py` (8 standing defense assertions plus 15 finding
  regression probes for #64–#87), and `tests/test_43_sprint_order_field.py`.
- The full battery is green: `go test -count=1 ./...` (7 packages PASS),
  `gofmt -l .` clean, `go vet ./...` 0 issues, `golangci-lint run ./...` 0 issues,
  `go build` succeeds, and `python3 tests/run_tests.py` reports **42/42** (100 %).

### Known Issues

One SPEC-vs-code divergence remains open and is unaffected by this release. It does
not affect runtime behaviour and is tracked as a `spec` / `tech-debt` follow-up for
a future `specification-manager` pass:

- `SPEC/COMMANDS.md` documents the `audit stats` JSON keys `operations_count`,
  `entity_type_count`, `first_entry`, `last_entry` and a `period.{since,until}`
  block; the implementation emits `by_operation`, `by_entity_type`,
  `first_entry_at`, `last_entry_at`, `total_entries` with no `period` object.

[1.10.0]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.9.1...v1.10.0

## [1.9.1] - 2026-06-05

Hardens the `rmp graph` knowledge-graph store by upgrading its backing engine,
GoGraph, from `v0.1.0` to `v0.2.0`. The upgrade is a drop-in dependency change:
no `rmp` command, flag, JSON output, exit code, on-disk graph format, or database
schema is altered, and no `rmp` source change is required. A v0.2.0 usage
evaluation confirmed that the existing consumers (`internal/commands/graph.go`
and `internal/web/data.go`) are source-compatible, and the only behavioural
change that reaches Groadmap strengthens existing error handling. Under Semantic
Versioning 2.0.0 this is a **PATCH** release, fully backward compatible with
`v1.9.0`. The database schema version is unchanged at `1.6.0`, so existing
installations require no migration.

### Changed

- **`rmp graph` store hardened via GoGraph `v0.1.0` → `v0.2.0`** — the
  knowledge-graph store that backs the `graph` command family is upgraded to
  GoGraph `v0.2.0`, a reliability, ACID, and durability hardening release. The
  consumed surface (`store/recovery`, `store/wal`, `store/txn`, `store/snapshot`,
  `cypher`, `graph/lpg`, `graph/csr`) is unchanged at the API level; the
  exact-tag pin in `go.mod` moves to `v0.2.0` and the indirect
  `golang.org/x/exp` hash is refreshed. Specified across `SPEC/BUILD.md`,
  `SPEC/GRAPH.md`, and `SPEC/ARCHITECTURE.md`, all reconciled to the `v0.2.0`
  pin.

### Fixed

- **Fail-stop on genuine graph-store corruption** — `recovery.Open`, used by
  both `rmp graph` and the read-only `rmp web` graph page, now returns a clean
  error on genuine write-ahead-log corruption (CRC mismatch or unsupported
  record version) instead of the `v0.1.0` behaviour of swallowing it and risking
  further appends onto a damaged store. A benign crash-truncated WAL tail still
  recovers cleanly, so the change only tightens the corruption path and leaves
  the normal open path unaffected. Inherited crash-durability ordering fixes from
  GoGraph also apply: the snapshot writer `fsync`s its staging directory before
  the publish rename, autocommit writes are made durable before they become
  visible, and the snapshot manifest now records the directed/multigraph shape so
  a simple graph cannot silently become a multigraph after a reopen.

### Security

- **Two Go standard-library vulnerabilities resolved** — the GoGraph `v0.2.0`
  upgrade pulls in the `go1.26.4` toolchain, which resolves **GO-2026-5039**
  (`net/textproto`) and **GO-2026-5037** (`crypto/x509`), both reachable through
  the dependency. `govulncheck ./...` was run against the upgraded module and
  reports **"No vulnerabilities found."**

### Documentation

- **Regression Prevention principle** — `CLAUDE.md` gains a "Regression
  Prevention" working principle. This is an internal, contributor-facing
  governance change only, with no code, CLI, or runtime impact.

### Known Issues

One SPEC-vs-code divergence remains open and is unaffected by this release. It
does not affect runtime behaviour and is tracked as a `spec` / `tech-debt`
follow-up for a future `specification-manager` pass:

- `SPEC/COMMANDS.md` documents the `audit stats` JSON keys `operations_count`,
  `entity_type_count`, `first_entry`, `last_entry` and a `period.{since,until}`
  block; the implementation emits `by_operation`, `by_entity_type`,
  `first_entry_at`, `last_entry_at`, `total_entries` with no `period` object.

[1.9.1]: https://github.com/FlavioCFOliveira/Groadmap/compare/v1.9.0...v1.9.1

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
