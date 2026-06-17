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
- [AI agent banner](#ai-agent-banner)
- [Help structure template](#help-structure-template)
- [Family-help template](#family-help-template)
- [Subcommand-help template](#subcommand-help-template)
- [Command inventory](#command-inventory)
- [Error message format](#error-message-format)
- [AI_AGENT environment variable](#ai_agent-environment-variable)
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

A separate machine-readable surface for the second audience exists in
parallel to the plain-text help: the AI Agent Contract emitted by
`rmp --ai-help`. The contract is specified in
`COMMANDS.md § AI Help` (CLI surface) and
`DATA_FORMATS.md § AI Agent Contract` (JSON shape). The plain-text help
described in this document remains the primary surface for human
operators.

## AI agent banner

To make the machine-readable contract discoverable to LLM agents that
first reach for the plain-text help, every plain-text help printer
emits the following banner as its **first line**, followed by exactly
one blank line, followed by the existing help body:

```
AI agents: run `rmp --ai-help` for a machine-readable command contract.
```

The banner is mandatory and identical across all three help levels
(global, family, subcommand). It is the literal string above, with
backticks, with no surrounding decoration.

The banner is **not** printed when:

- The contract itself is being emitted (`rmp --ai-help`, `rmp ai-help`,
  `rmp <command> --ai-help`, `rmp <command> <subcommand> --ai-help`):
  the contract is JSON and contains no plain-text help.
- `rmp --version` / `rmp -v`: version output is not help.

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
| Global | `rmp --ai-help` / `rmp ai-help` | `COMMANDS.md § AI Help` |
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
| Graph | `rmp graph [create \| query \| update \| delete \| search]` | `COMMANDS.md § Graph Management` |
| Web | `rmp web` | `COMMANDS.md § Web Interface` |

Each subcommand in the inventory has its own dedicated help printer in
the code (e.g. `printTaskStatHelp`, `printSprintCloseHelp`,
`printBacklogShowNextHelp`). The family help additionally summarises the
subcommands and shared invariants.

### Sprint family help specifics

The `sprint` family help and the `sprint create` / `sprint update` subcommand
helps follow the same structure template as every other family but MUST
additionally make the sprint execution-order field explicit, because its rules
cannot be inferred from the generic template. Both the plain-text help and the
machine-readable AI Agent Contract (`rmp --ai-help`) MUST document the field:

1. **The `--order` flag.** State, on `sprint create` and `sprint update`, that
   `--order <n>` sets the sprint execution order; that the value must be a positive
   integer greater than zero (`> 0`); that it must be unique across the roadmap; and
   that on `sprint create` the flag is optional and the next available value is
   auto-assigned when it is omitted. See `COMMANDS.md § Create Sprint` and
   `COMMANDS.md § Update Sprint`.
2. **Order immutability after close.** State, in the family-help "Workflow / rules"
   block and on `sprint update`, that a sprint's `order` can be changed only while
   the sprint is `PENDING` or `OPEN`, and that once the sprint is `CLOSED` its
   `order` is immutable (any change is rejected with exit code 6). See
   `STATE_MACHINE.md § Sprint Order Immutability`.
3. **Collision exit code.** State that an `--order` value already used by another
   sprint is rejected with exit code 5 (resource already exists), distinct from the
   exit code 6 used for a non-positive or non-integer value.
4. **The `sprint tasks` status filter.** State, on `sprint tasks`, that in
   addition to `--order-by-priority` the subcommand accepts an optional
   `-s, --status <state>` filter that restricts the result to tasks whose status
   equals `<state>` (one of BACKLOG, SPRINT, DOING, TESTING, COMPLETED). Document
   both the short form `-s` and the long form `--status`, consistent with the
   sibling list commands (`task ls`, `backlog ls`). The handler parses the flag
   and passes it to the sprint-task query. Without the flag, every task in the
   sprint is returned regardless of status. A `<state>` value outside the valid
   set is rejected with exit code 6. See `COMMANDS.md § List Sprint Tasks`.

### Graph family help specifics

The `graph` family help and each graph subcommand help follow the same
structure template as every other family but MUST additionally make two
graph-specific behaviours explicit, because an agent cannot infer them
from the generic template:

1. **Query input.** State that the Cypher query comes from the `--query`
   flag or, when the flag is absent, from standard input, and that
   supplying neither is an error (exit code 2). This is the only command
   in the CLI that reads standard input. See
   `GRAPH.md § Cypher Input Source and Precedence`.
2. **Guard rail.** State, per subcommand, which Cypher operation class is
   accepted and that a mismatching query is rejected with exit code 6
   before execution. The family help lists the five subcommand-to-operation
   mappings; each subcommand help names its own allowed class. See
   `GRAPH.md § Subcommands and Guard-Rail Validation`.

### Web command help specifics

`rmp web` is a single command with no subcommands. Its help follows the same
block order as every other command (Usage, description, Options, Output, Exit
codes, Examples), and MUST additionally make explicit three behaviours an agent
or user cannot infer from the generic template:

1. **No roadmap flag.** State that `rmp web` does **not** take `-r` /
   `--roadmap`: it lists all roadmaps and the user selects one in the browser.
   This is the one command exempt from the always-required-roadmap rule (see
   `COMMANDS.md § Roadmap Selection (Always Required)`).
2. **Read-only, loopback by default.** State that the interface is read-only
   (the CLI remains the sole write path) and binds loopback (`127.0.0.1`) by
   default, so it is reachable only from the local machine; that
   `--host 0.0.0.0` is the explicit opt-in to expose it on all interfaces
   (network-reachable); and that `--host`/`--port` override the bind address,
   with the default-port ephemeral fallback. See `WEB.md`.
3. **Long-lived process.** State that the command starts a server that keeps
   running until interrupted (`Ctrl+C` / `SIGINT` or `SIGTERM`), unlike every
   other command, which completes and exits. On startup it prints the served
   URL; with `--no-open` it does not launch a browser.

The skeleton (illustrative; the canonical contract is
`COMMANDS.md § Web Interface`):

```
Usage: rmp web [options]

Start a read-only web interface for the roadmaps under ~/.roadmaps/.
The browser lists every roadmap and lets you view its tasks, sprints,
and knowledge graph. The web interface never writes; the rmp CLI
remains the sole write path. rmp web does not take -r/--roadmap.

Options:
  --host <address>   Bind host. Default 127.0.0.1 (loopback, local machine
                     only). Use --host 0.0.0.0 to expose on the network.
  --port <number>    Bind port 0-65535. Default 8787; falls back to an
                     ephemeral port if 8787 is in use and --port is not set.
  --no-open          Do not launch a browser; just print the served URL.
  -h, --help         Show this help message

Output (stdout JSON):
  On startup: {"url": "http://127.0.0.1:8787"} (reflects the bound host/port)

Exit codes:
  0   Server started and was stopped by Ctrl+C / SIGINT / SIGTERM
  1   Host/port could not be bound, or the data directory was unreadable
  2   Unknown flag or unexpected argument
  6   --port out of range 0-65535 or not an integer

Examples:
  rmp web
  rmp web --port 9000
  rmp web --host 0.0.0.0 --port 9000
  rmp web --no-open
```

## Error message format

When a command is invoked incorrectly, the application writes a
plain-text error to stderr. JSON is not used for errors. Help is **not**
auto-appended to the error — users invoke `--help` explicitly when they
want it. The required shape is:

```
Error: <human-readable description of the problem>

AI agents: run `rmp --ai-help` for a machine-readable command contract.
```

The wording starts with `Error: ` (with the colon and the trailing
space). For input-related errors (missing parameters, unknown flags,
unknown subcommands, invalid argument formats) the description names
the offending flag or value. For non-input errors (resource not found,
already exists, database failure) the description names the entity and
its id where relevant.

After the error line, the printer writes one blank line followed by the
AI-agent hint:

```
AI agents: run `rmp --ai-help` for a machine-readable command contract.
```

The hint:

- Is written to stderr, after the `Error: ` line, on every error path.
- Is one line of plain text, identical across every error.
- Does not change the exit code.
- Is suppressed when the failing command is itself `rmp --ai-help`,
  `rmp ai-help`, `rmp <command> --ai-help`, or
  `rmp <command> <subcommand> --ai-help`, to avoid recursive guidance
  from the contract emitter.
- Is suppressed when `AI_AGENT=1` is active for this invocation: in
  that case the env-var hint has already been emitted as the first
  line of stderr, and repeating it on the error path would duplicate
  the same message. See the deduplication rule in
  `AI_AGENT environment variable` below.

Example: missing required arguments on `rmp task create`

```
$ rmp task create -r myproject
Error: required parameter missing: --title

AI agents: run `rmp --ai-help` for a machine-readable command contract.
```

## AI_AGENT environment variable

When the environment variable `AI_AGENT` is set to the exact value `1`,
the CLI emits the AI-agent hint to stderr **before** any other output on
every invocation:

```
AI agents: run `rmp --ai-help` for a machine-readable command contract.
```

### Ordering

The env-var hint is the **first line** written to stderr. It is followed
by exactly **one blank line**. Any further stderr content (an `Error:`
line on failure paths, diagnostic output, etc.) is written after that
blank line. The ordering is the same on success and on failure:

```
AI agents: run `rmp --ai-help` for a machine-readable command contract.
<blank line>
<remaining stderr, if any>
```

On a successful invocation with `AI_AGENT=1`, the hint is the only
stderr output and stdout is unaffected.

### Deduplication

The env-var hint and the error-path hint (specified in
`Error message format` above) are textually identical. To avoid
emitting the same line twice in the same invocation, the following
deduplication rule applies:

- When `AI_AGENT=1` is active **and** the invocation fails, the
  env-var hint is emitted once at the top of stderr (per the ordering
  above) and the trailing error-path hint is **suppressed**.
- When `AI_AGENT=1` is not active and the invocation fails, only the
  trailing error-path hint is emitted (no top hint).
- When `AI_AGENT=1` is active and the invocation succeeds, only the
  env-var hint is emitted at the top of stderr (no error path runs).

The agent therefore observes the hint exactly once per invocation in
every combination of states.

### Rules

- The hint is one line, plain text, written to stderr.
- The hint is written exactly once per invocation (see deduplication
  above).
- The hint does **not** modify stdout in any way (JSON payloads remain
  byte-identical and parseable).
- The hint does **not** modify the exit code.
- The hint is suppressed when the invocation is `rmp --ai-help`,
  `rmp ai-help`, `rmp <command> --ai-help`, or
  `rmp <command> <subcommand> --ai-help` (the agent is already
  consuming the contract).
- The hint is enabled **only** when `AI_AGENT` is exactly the string
  `1`. Any other value (including empty, `0`, `true`, `false`, or
  unset) leaves the CLI silent on this axis.

The variable is read once at process start. It is a hint mechanism, not
a mode switch: no other behaviour changes when `AI_AGENT=1`.

## Exit codes

The canonical exit-code catalogue is defined in
`ARCHITECTURE.md § Exit Codes`. Each family help (and each subcommand
help) **must** include an `Exit codes:` block listing only the codes the
command can actually emit, each with a one-line cause. The agreed
philosophy is that the catalogue stays single-sourced in
`ARCHITECTURE.md`, but every help replicates the relevant subset so the
reader doesn't have to cross-reference for the failure cases that apply
to the call they're about to make.
