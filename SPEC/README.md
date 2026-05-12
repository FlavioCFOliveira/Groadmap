# SPEC — Groadmap Technical Specification

This directory contains the authoritative technical specification for Groadmap. The Specification First Policy applies: no implementation without a corresponding SPEC entry. See `CLAUDE.md` (project root) for the policy in full.

The SPEC is unversioned. Git is the source of truth for its evolution — recover any past state via `git log` and `git show`.

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
| `VERSION.md` | Application and schema versioning |

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
