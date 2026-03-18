# Technical Specification - Groadmap

## Overview

This directory contains the complete technical specification for Groadmap - a CLI tool for managing technical roadmaps in agentic workflows.

## Specification Structure

| File | Description |
|------|-------------|
| [ARCHITECTURE.md](./ARCHITECTURE.md) | System architecture, file structure, and design philosophy |
| [COMMANDS.md](./COMMANDS.md) | CLI commands, subcommands, arguments, and options |
| [DATABASE.md](./DATABASE.md) | SQLite schema, tables, indexes, and relationships |
| [DATA_FORMATS.md](./DATA_FORMATS.md) | JSON output formats, ISO 8601 date conventions, and data types |
| [HELP_EXAMPLES.md](./HELP_EXAMPLES.md) | Help output format examples for all commands and error messages |
| [MODELS.md](./MODELS.md) | Go structures and enums mapping |

## Technology Stack

- **Language**: Go (exclusively)
- **Database**: SQLite (individual `.db` files)
- **Input**: CLI arguments and options only (no JSON, no stdin, no config files)
- **Output Format**: JSON for queries/creation, Plain Text for errors and help
- **Dates**: ISO 8601 with UTC

## Design Principles

1. **Performance**: Fast execution, minimal overhead
2. **Resources**: Efficient usage, only what is necessary
3. **Security**: Strict validation, protection against invalid data
4. **Consistency**: Consistent JSON for queries, always UTC dates

## Specification Versioning

- Current version: 1.1.0 (Migrated to Go)
- Date: 2026-03-16
