# CLI Help Skeleton

This file specifies the structure of the CLI help output, not its canonical text. The canonical help text is built from the command implementations in `internal/commands/*.go`; this document defines the structural contract those implementations must satisfy. Any change to the help structure (new sections, new fields, renamed labels) must be recorded here before the code is modified.

## Table of Contents

- [Purpose](#purpose)
- [Help Structure Template](#help-structure-template)
- [Command Inventory](#command-inventory)
- [Error Message Format](#error-message-format)
- [Exit Codes](#exit-codes)

## Purpose

The help system has two functions:

1. Document command usage for human operators invoking the CLI directly.
2. Provide a recoverable contract for tests and integrations that parse help output.

The text shown to the user lives next to the code that implements each command. The structure of that text is fixed by this document.

## Help Structure Template

Each command and subcommand must produce help output that follows this template:

```
usage: rmp <command> [<subcommand>] [-h | --help] [OPTIONS] [ARGUMENTS]

<Description: one or two sentences explaining what the command does.>

Options:
   -x, --xxx <type>    Description of the option. Mark required options explicitly.
   -y, --yyy <type>    Description of the option.

Arguments:
   <arg-name>          Description of the positional argument.

Examples:
   rmp <command> [...]
   rmp <command> [...]  # short alternative form
```

**Example: `rmp task create`**

```
usage: rmp task create [-h | --help] -r <name> -t <title> -fr <fr> -tr <tr> -ac <ac> [OPTIONS]

Create a new task in the specified roadmap.

Required Options:
   -r, --roadmap <name>                  Roadmap name
   -t, --title <title>                   Task title/summary
   -fr, --functional-requirements <fr>   Functional requirements
   -tr, --technical-requirements <tr>    Technical requirements
   -ac, --acceptance-criteria <ac>       Acceptance criteria

Optional Options:
   -p, --priority <n>                    Priority 0-9 (default: 0)
       --severity <n>                    Severity 0-9 (default: 0)

Output: JSON object with task ID.

Examples:
   rmp task create -r project1 -t "..." -fr "..." -tr "..." -ac "..."
```

All other commands follow this same shape. Their option lists, argument lists, and examples are documented in `COMMANDS.md` for each command family.

## Command Inventory

This table maps every help-producing command to the canonical specification for its flags, semantics, and exit-code behaviour. The help text in `internal/commands/*.go` must match the command contract in `COMMANDS.md`.

| Family | Command | Canonical Specification |
|--------|---------|-------------------------|
| Global | `rmp --help` | `COMMANDS.md § Global Commands` |
| Global | `rmp --version` | `COMMANDS.md § Global Commands` |
| Roadmap | `rmp roadmap list` | `COMMANDS.md § Roadmap Management` |
| Roadmap | `rmp roadmap create` | `COMMANDS.md § Roadmap Management` |
| Roadmap | `rmp roadmap remove` | `COMMANDS.md § Roadmap Management` |
| Task | `rmp task list` | `COMMANDS.md § Task Management` |
| Task | `rmp task create` | `COMMANDS.md § Task Management` |
| Task | `rmp task get` | `COMMANDS.md § Task Management` |
| Task | `rmp task next` | `COMMANDS.md § Task Management` |
| Task | `rmp task set-status` (alias `stat`) | `COMMANDS.md § Task Management` |
| Task | `rmp task set-priority` (alias `prio`) | `COMMANDS.md § Task Management` |
| Task | `rmp task set-severity` (alias `sev`) | `COMMANDS.md § Task Management` |
| Task | `rmp task edit` | `COMMANDS.md § Task Management` |
| Task | `rmp task remove` (alias `rm`) | `COMMANDS.md § Task Management` |
| Task | `rmp task reopen` | `COMMANDS.md § Task Management` |
| Task | `rmp task subtasks` | `COMMANDS.md § Task Management` |
| Task | `rmp task add-dep` / `remove-dep` | `COMMANDS.md § Task Management` |
| Task | `rmp task blockers` / `blocking` | `COMMANDS.md § Task Management` |
| Sprint | `rmp sprint list` / `get` / `show` / `tasks` / `open-tasks` | `COMMANDS.md § Sprint Management` |
| Sprint | `rmp sprint create` / `update` / `remove` | `COMMANDS.md § Sprint Management` |
| Sprint | `rmp sprint add-tasks` / `remove-tasks` / `move-tasks` | `COMMANDS.md § Sprint Management` |
| Sprint | `rmp sprint start` / `close` / `reopen` | `COMMANDS.md § Sprint Management` |
| Sprint | `rmp sprint stats` | `COMMANDS.md § Sprint Management` |
| Sprint | `rmp sprint reorder` / `move-to` / `swap` / `top` / `bottom` | `COMMANDS.md § Sprint Management` |
| Audit | `rmp audit list` (alias `ls`) | `COMMANDS.md § Audit Log Management` |
| Audit | `rmp audit history` (alias `hist`) | `COMMANDS.md § Audit Log Management` |
| Audit | `rmp audit stats` | `COMMANDS.md § Audit Log Management` |
| Backlog | `rmp backlog list` (alias `ls`) | `COMMANDS.md § Backlog Management` |
| Backlog | `rmp backlog show-next` | `COMMANDS.md § Backlog Management` |
| Stats | `rmp stats` | `COMMANDS.md § Statistics Command` |

## Error Message Format

When a command is invoked incorrectly, the application writes a plain-text error to stderr, followed by the help text for the invoked command or subcommand. JSON is not used for errors.

The required shape is:

```
Error: <human-readable description of the problem>

<help text for the command, exactly as it would appear under --help>
```

**Example: missing required arguments on `rmp task create`**

```
$ rmp task create -r project1
Error: Missing required options: --title, --functional-requirements, --technical-requirements, --acceptance-criteria

usage: rmp task create [-h | --help] -r <name> -t <title> -fr <fr> -tr <tr> -ac <ac> [OPTIONS]

Create a new task in the specified roadmap.

Required Options:
   -r, --roadmap <name>                  Roadmap name
   -t, --title <title>                   Task title/summary
   ...
```

The same shape applies to all input-related errors: missing parameters, unknown flags, unknown subcommands, invalid argument formats. Non-input errors (resource not found, conflict, database failure) emit only the `Error: ...` line; the help text is not appended.

## Exit Codes

The canonical exit-code catalogue is defined in `ARCHITECTURE.md § Exit Codes`. Help output must not document exit codes inline; it must reference that catalogue when needed.
