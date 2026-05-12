# SPEC — Groadmap Technical Specification

**Version:** v2.0.0
**Date:** 2026-05-12

This directory contains the authoritative technical specification for Groadmap. The Specification First Policy applies: no implementation without a corresponding SPEC entry. See `CLAUDE.md` (project root) for the policy in full.

---

## 1. Index

| File | Functional Area |
|------|-----------------|
| `ARCHITECTURE.md` | System design, components, exit codes |
| `BUILD.md` | Build system, cross-compilation matrix, CI/CD |
| `COMMANDS.md` | CLI commands, subcommands, flags, aliases |
| `DATABASE.md` | Schema, migrations, queries, audit operations |
| `DATA_FORMATS.md` | JSON schemas, input/output formats |
| `DEPLOY.md` | Installation, distribution, release process |
| `HELP_EXAMPLES.md` | Help text, error messages, usage examples |
| `MODELS.md` | Structs, enums, domain models |
| `STATE_MACHINE.md` | State transitions, workflows |
| `VERSION.md` | Application, schema, and specification versioning |

---

## 2. Canonical Sources

To prevent drift across SPEC files, the following topics have a single authoritative source. Other SPEC files MUST link to the canonical source rather than duplicate its content.

| Topic | Canonical Source |
|-------|------------------|
| Exit codes (numeric values and sentinel names) | `ARCHITECTURE.md` — Exit Codes section |
| Enums (`TaskType`, `TaskStatus`, `SprintStatus`) | `MODELS.md` |
| State transitions | `STATE_MACHINE.md` |
| Audit operations | `DATABASE.md` — Audit Log section |
| SQL DDL (table definitions, indexes, constraints) | `DATABASE.md` |

`DATABASE.md` additionally retains `CHECK` constraints in DDL as a normative reproduction of the enums; the Go-level enum definitions remain in `MODELS.md`.

---

## 3. Global Conventions

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
- Individual roadmap databases: `~/.roadmaps/<name>.db` with permissions `0600`.

### Naming Conventions

- Database columns: `snake_case` (e.g., `created_at`, `functional_requirements`).
- Go structs and fields: `PascalCase` for exported, `camelCase` for unexported.
- CLI commands and flags: lowercase, kebab-case (e.g., `task list`, `--max-tasks`).
- Short flags: single dash, may exceed one character when an unambiguous abbreviation is more readable (e.g., `-fr` for `--functional-requirements`).

---

## 4. Change History

This section tracks changes to the SPEC documents themselves. For application releases see `VERSION.md`; for database schema migrations see `DATABASE.md` (Schema Version History).

| Date | SPEC Version | Change |
|------|--------------|--------|
| 2026-05-12 | v2.0.0 | **Audit-driven consolidation (breaking).** Closed 19 audit findings: (1) Created this README. (2) Sync VERSION.md to app v1.2.1, schema v1.6.0, SPEC v2.0.0; added Version History entries for 1.2.0 / 1.2.1 and Schema History 1.4.0 / 1.5.0 / 1.6.0. (3) Renamed obsolete field references (`description/action/expected_result` → `functional_requirements/technical_requirements/acceptance_criteria`). (4) Updated Task struct memory comment from 168 → 240 bytes (4 groups including slices). (5) **Uniformized exit codes**: `6` semantic validation, `2` syntax/parsing/required, `3` no-roadmap, `4` not-found, `5` already-exists, `1` system. Corrected ~30 occurrences across COMMANDS.md and ARCHITECTURE.md (`INVALID_STATUS_TRANSITION` 2→6 included). (6) Rejected manual `task stat <ids> SPRINT` with exit 6; propagated to COMMANDS, STATE_MACHINE, DATA_FORMATS, HELP_EXAMPLES. (7) Removed `roadmap create --force`. (8) Propagated `task remove` BACKLOG-only constraint to HELP_EXAMPLES, STATE_MACHINE, DATABASE. (9) Corrected sprint_tasks N:M → 1:N text (DDL unchanged). (10) Uniformized short flags `-fr/-tr/-ac` in HELP_EXAMPLES.md. (11) Added help blocks for 10 previously undocumented subcommands (`task reopen/subtasks/add-dep/remove-dep/blockers/blocking`, `sprint open-tasks`, `backlog list/show-next`, `stats`). (12) Consolidated audit-operation lists; added `TASK_REOPEN`. (13) Removed `linux-386` detection from DEPLOY.md. (14) Corrected `backlog list --type` enum (removed phantom `FEATURE`, added 6 missing types). (15) Rewrote state-diagram ASCII. (16) Removed non-normative "Future Considerations" section. (17) Migrated per-file Change History tables into this README. (18) Added `max_tasks: null` example in DATA_FORMATS.md. (19) **R2-R5 centralization**: ARCHITECTURE.md canonical for exit codes, MODELS.md for enums, STATE_MACHINE.md for transitions, DATABASE.md for audit operations — other SPECs link rather than duplicate. |
| 2026-05-12 | v1.0.0 | Consolidated SPEC baseline (superseded by v2.0.0 the same day). Introduced `SPEC/README.md` as index, canonical-source map, and central change history. Migrated per-file Change History tables from `COMMANDS.md`, `BUILD.md`, `DEPLOY.md` into the Historical Entries section below. |

### Historical Per-File Entries

The following entries were previously embedded in individual SPEC files and are preserved here for traceability. The originals will be removed when the corresponding files are cleaned up.

#### `COMMANDS.md`

| Date | Change | Description |
|------|--------|-------------|
| 2026-03-23 | Initial | First version of CLI Commands specification |
| 2026-03-23 | Update | Added `stats` command for roadmap statistics |
| 2026-03-24 | Update | Added `--summary` flag to `task stat` for completion summary |
| 2026-03-24 | Update | Added `task reopen` command; restricted `task remove` to BACKLOG only; enforced sequential sprint opening |
| 2026-03-24 | Update | Added `backlog` command group with `list` and `show-next` subcommands |
| 2026-03-24 | Update | Added sub-task hierarchy: `--parent` flag on `task create`, `task subtasks <id>` subcommand, parent COMPLETED guard |
| 2026-03-24 | Update | Added task dependency commands: `task add-dep`, `task remove-dep`, `task blockers`, `task blocking`; COMPLETED guard now checks dependencies |
| 2026-03-24 | Update | Added velocity, days_elapsed, days_remaining, and burndown fields to `sprint stats`; added average_velocity to `rmp stats` |
| 2026-03-24 | Update | Removed `roadmap use` subcommand; `-r <name>` / `--roadmap <name>` is now always required for roadmap-scoped commands; no default roadmap mechanism exists |

#### `BUILD.md`

| Date | Change | Description |
|------|--------|-------------|
| 2026-03-20 | Initial | Build system with all supported platforms including Raspberry Pi |

#### `DEPLOY.md`

| Date | Change | Description |
|------|--------|-------------|
| 2026-03-20 | Initial | Installation script with platform detection including Raspberry Pi |
| 2026-03-20 | Update | Added automated GitHub Release creation workflow triggered on tag push |

---

## 5. Release Process Hook

When releasing a new application version, the release process (`SPEC/VERSION.md` § Release Process) requires updating:

- `cmd/rmp/main.go` version constant
- `SPEC/VERSION.md` (current version + Version History)
- This file (`SPEC/README.md`) — SPEC version and Change History entry, if the SPEC itself was modified as part of the release

If the release does not touch the SPEC, the SPEC version does not change.
