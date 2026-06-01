# Changelog

All notable changes to **Groadmap** (`rmp`) are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
