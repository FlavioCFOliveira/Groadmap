# Changelog

All notable changes to **Groadmap** (`rmp`) are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
