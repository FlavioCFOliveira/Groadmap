# Spec Orchestrator Memory - Groadmap

## Project Context
- **Name**: Groadmap
- **Stack**: Go, SQLite
- **Source of Truth**: `SPEC/` directory
- **Data Location**: `~/.roadmaps/*.db`
- **Output Policy**: JSON for data, Plain Text for errors/help

## Architectural Decisions
- **Isolation**: Each roadmap is a separate SQLite file.
- **Security**: Directory permissions `0700`, files `0600`.
- **Integrity**: `PRAGMA foreign_keys = ON` and `WAL` mode are mandatory.
- **Audit**: All state-changing operations must be logged in the same transaction.
- **Dates**: Strict ISO 8601 UTC with milliseconds (`YYYY-MM-DDTHH:mm:ss.sssZ`).

## Specification Patterns
- **Functional Block Files**: One file per block (e.g., `DATABASE.md`, `COMMANDS.md`).
- **Standardized Error Handling**: Specific exit codes (1-6, 126, 127, 130) mapped to internal errors.

## Implementation Roadmap
- Follows the order: Utils -> Models -> DB Layer -> CLI Skeleton -> Functional Blocks.
- Defined in `SPEC/IMPLEMENTATION_PLAN.md`.
