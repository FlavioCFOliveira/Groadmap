# CLI Help Skeleton

This file specifies the **structure** of the CLI help output. The canonical
text of every help message lives next to the code that implements each
command (`internal/commands/*.go`, `internal/commands/*_help.go`,
`cmd/rmp/main.go`). This document defines the structural contract those
implementations must satisfy. A change to the help structure (new
sections, new labels, dropped fields) is recorded here before the code
is modified.

## Table of Contents

- [Audience and intent](#audience-and-intent)
- [Help levels](#help-levels)
- [Help structure template](#help-structure-template)
- [Family-help template](#family-help-template)
- [Subcommand-help template](#subcommand-help-template)
- [Command inventory](#command-inventory)
- [Error message format](#error-message-format)
- [Exit codes](#exit-codes)

## Audience and intent

The help system has two readers in mind:

1. A **human operator** invoking the CLI from a shell.
2. An **LLM agent** composing invocations on behalf of a user.

To serve both, every help text must enumerate the things that cannot be
guessed from a placeholder: valid enum values, default values for
optional flags, the JSON output shape, and the failure modes that map to
each exit code.

## Help levels

Three levels of help text exist, each at a different granularity:

| Level | Trigger | Purpose |
|-------|---------|---------|
| Global | `rmp --help`, `rmp -h`, `rmp help` | Cross-cutting orientation: which command family handles what, choosing between similar listing commands, I/O conventions. |
| Family | `rmp <family> --help` (and `rmp <family>` with no subcommand) | Enumerates the subcommands of a family, shared options, valid enum values, status workflow, output shapes, exit codes, families-wide examples. |
| Subcommand | `rmp <family> <subcommand> --help` | Focused contract for a single subcommand: usage line, required vs optional, the JSON it returns, the exit codes it can emit, two or three worked examples. |

Every family handler routes `--help` (and `-h` and the literal word
`help`) anywhere in its argument list to the matching printer **before**
any other parsing runs, so subcommand help is reachable even when the
required `-r` flag is missing.

## Help structure template

Every family or subcommand help follows the same block order, in this
order:

1. `Usage: rmp ...` — one line, with positional argument names spelled
   semantically (`<task-ids>`, `<sprint-id>`, `<new-status>`, not
   `<id>`).
2. **Description / context** — one paragraph for family helps, one
   sentence for subcommands.
3. **Valid values** *(when applicable)* — explicit enumeration of any
   enum values the command accepts (statuses, types, operations,
   entity-types). Numeric ranges (priority/severity 0-9) and date
   formats are listed here too.
4. **Workflow / rules** *(family helps only)* — the state-machine
   diagram, capacity rules, and rejection conditions enforced by the
   runtime. Tells the reader *when* a command will fail before they try.
5. **Commands** *(family helps only)* — one line per subcommand with
   its aliases and a description that starts with a verb.
6. **Options** — split into named sections when the command set is
   heterogeneous (`Options (shared)`, `Options (list)`, `Options
   (create / edit)`, etc.). Each line: short and long form, `<type>`
   placeholder, required-or-optional + default, and a one-sentence
   description.
7. **Output (stdout JSON)** — one block per subcommand for family
   helps, one fenced block for subcommand helps. Mutating subcommands
   declare "empty (exit 0)" explicitly.
8. **Exit codes** — every code the command can emit, each with a
   one-line cause. Exit code 0 is included so the reader sees the
   success case without inference.
9. **Examples** — two to four worked examples that cover the common
   paths (filter, mutate, error-recovery).

## Family-help template

```
Usage: rmp <family> [command] [arguments] [options]

<one-paragraph description>

Valid <enum> values (for <flag>):
  VALUE1, VALUE2, ...

Numeric ranges:
  --foo, --bar     0-9 (0 = ..., 9 = ...)

<Family> workflow:
  STATE_A --[verb]--> STATE_B --[verb]--> STATE_C
  Rules enforced:
    - <invariant>
    - <invariant>

Commands:
  list, ls [OPTIONS]              <verb-first description>
  ...

Options (shared):
  -r, --roadmap <name>            REQUIRED. Target roadmap.
  -h, --help                      Show this help message

Options (<subcommand-or-group>):
  ...

Output (stdout JSON):
  list, get, ...        <shape sketch>
  create                {"id": <int>}
  mutating commands     Empty (exit 0 on success).
  Object key list:       <comma-separated key list>

Exit codes:
  0   Success
  ...

Examples:
  rmp <family> <sub> -r <roadmap> [...]
```

## Subcommand-help template

```
Usage: rmp <family> <subcommand> -r <roadmap> <positional-args> [options]

<one-sentence description, optionally a comparison paragraph for
commands easily confused with siblings (e.g. task list vs sprint
tasks vs backlog list)>

Aliases: <list>   (omitted when there is no alias)

Required:
  -r, --roadmap <name>            Target roadmap
  <positional>                    <type + role>

Optional:
  <flag>                          <description, with default and range>

Output (stdout JSON):
  <one-line or fenced JSON block describing the shape>

Exit codes:
  0   Success
  <code>   <cause>

Examples:
  rmp <family> <subcommand> -r <roadmap> ...
```

The Required vs Optional split is required: an LLM should not need to
re-read a description paragraph to discover whether a flag is mandatory.

## Command inventory

This table maps every help-producing command to the canonical
specification for its flags, semantics, and exit-code behaviour. The
help text must match the command contract in `COMMANDS.md`.

| Family | Command | Canonical specification |
|--------|---------|-------------------------|
| Global | `rmp --help` | `COMMANDS.md § Global Commands` |
| Global | `rmp --version` | `COMMANDS.md § Global Commands` |
| Roadmap | `rmp roadmap [list \| create \| remove]` | `COMMANDS.md § Roadmap Management` |
| Task | `rmp task [list \| create \| get \| next \| edit \| remove \| stat \| reopen \| prio \| sev \| assign \| unassign \| subtasks \| add-dep \| remove-dep \| blockers \| blocking]` | `COMMANDS.md § Task Management` |
| Sprint | `rmp sprint [list \| create \| get \| show \| update \| remove]` | `COMMANDS.md § Sprint Management` |
| Sprint | `rmp sprint [start \| close \| reopen]` | `COMMANDS.md § Sprint Lifecycle` |
| Sprint | `rmp sprint [tasks \| open-tasks \| stats]` | `COMMANDS.md § Sprint Management` |
| Sprint | `rmp sprint [add-tasks \| remove-tasks \| move-tasks]` | `COMMANDS.md § Sprint Task Assignment` |
| Sprint | `rmp sprint [reorder \| move-to \| swap \| top \| bottom]` | `COMMANDS.md § Sprint Task Ordering` |
| Audit | `rmp audit [list \| history \| stats]` | `COMMANDS.md § Audit Log Management` |
| Backlog | `rmp backlog [list \| show-next]` | `COMMANDS.md § Backlog Management` |
| Stats | `rmp stats` | `COMMANDS.md § Statistics Command` |

Each subcommand in the inventory has its own dedicated help printer in
the code (e.g. `printTaskStatHelp`, `printSprintCloseHelp`,
`printBacklogShowNextHelp`). The family help additionally summarises the
subcommands and shared invariants.

## Error message format

When a command is invoked incorrectly, the application writes a
plain-text error to stderr. JSON is not used for errors. Help is **not**
auto-appended to the error — users invoke `--help` explicitly when they
want it. The required shape is:

```
Error: <human-readable description of the problem>
```

The wording starts with `Error: ` (with the colon and the trailing
space). For input-related errors (missing parameters, unknown flags,
unknown subcommands, invalid argument formats) the description names
the offending flag or value. For non-input errors (resource not found,
already exists, database failure) the description names the entity and
its id where relevant.

Example: missing required arguments on `rmp task create`

```
$ rmp task create -r myproject
Error: required parameter missing: --title
```

## Exit codes

The canonical exit-code catalogue is defined in
`ARCHITECTURE.md § Exit Codes`. Each family help (and each subcommand
help) **must** include an `Exit codes:` block listing only the codes the
command can actually emit, each with a one-line cause. The agreed
philosophy is that the catalogue stays single-sourced in
`ARCHITECTURE.md`, but every help replicates the relevant subset so the
reader doesn't have to cross-reference for the failure cases that apply
to the call they're about to make.
