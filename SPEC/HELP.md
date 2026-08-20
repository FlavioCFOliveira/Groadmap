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
  - [Task family help specifics](#task-family-help-specifics)
  - [Sprint family help specifics](#sprint-family-help-specifics)
  - [Audit family help specifics](#audit-family-help-specifics)
  - [Comment subcommand help specifics](#comment-subcommand-help-specifics)
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
| Task | `rmp task [list \| create \| get \| next \| edit \| remove \| stat \| reopen \| prio \| sev \| subtasks \| add-dep \| remove-dep \| blockers \| blocking]` | `COMMANDS.md § Task Management` |
| Task | `rmp task [comment-add \| comment-list \| comment-edit \| comment-remove]` | `COMMANDS.md § Task Comments` |
| Sprint | `rmp sprint [list \| create \| get \| show \| update \| remove]` | `COMMANDS.md § Sprint Management` |
| Sprint | `rmp sprint [comment-add \| comment-list \| comment-edit \| comment-remove]` | `COMMANDS.md § Sprint Comments` |
| Sprint | `rmp sprint [start \| close \| reopen]` | `COMMANDS.md § Sprint Lifecycle` |
| Sprint | `rmp sprint [tasks \| open-tasks \| stats]` | `COMMANDS.md § Sprint Management` |
| Sprint | `rmp sprint [add-tasks \| remove-tasks \| move-tasks]` | `COMMANDS.md § Task Assignment` |
| Sprint | `rmp sprint [reorder \| move-to \| swap \| top \| bottom]` | `COMMANDS.md § Task Ordering` |
| Audit | `rmp audit [list \| history \| stats]` | `COMMANDS.md § Audit Log Management` |
| Backlog | `rmp backlog [list \| show-next]` | `COMMANDS.md § Backlog Management` |
| Stats | `rmp stats` | `COMMANDS.md § Statistics Command` |
| Graph | `rmp graph [create \| query \| update \| delete \| search]` | `COMMANDS.md § Graph Management` |
| Web | `rmp web` | `COMMANDS.md § Web Interface` |

Each subcommand in the inventory has its own dedicated help printer in
the code (e.g. `printTaskStatHelp`, `printSprintCloseHelp`,
`printBacklogShowNextHelp`). The family help additionally summarises the
subcommands and shared invariants.

### Task family help specifics

The `task stat` subcommand help follows the same structure template as every other
subcommand but MUST additionally make the rules below explicit, because the generic
Required / Optional split cannot express them. Both the plain-text help and the
machine-readable AI Agent Contract (`rmp --ai-help`) MUST document them:

1. **Two flags are conditionally mandatory.** `-co, --commit-open` is mandatory when
   the target status is `DOING`, and `-cc, --commit-close` is mandatory when the
   target status is `COMPLETED`. Neither is mandatory for the subcommand as a whole,
   so neither belongs under a bare `Required:` heading. The help MUST list both under
   `Optional:` and state the condition in the flag's own description, in the form
   "required when <new-status> is DOING" and "required when <new-status> is
   COMPLETED". A reader must be able to tell, without running the command, that
   `rmp task stat -r x 7 DOING` on its own fails.
2. **Each flag is rejected outside its own transition.** The help MUST state that
   `--commit-open` is accepted only for the target status `DOING` and
   `--commit-close` only for `COMPLETED`, alongside the existing statement of the
   same rule for `--summary` and `COMPLETED`.
3. **The value format.** The help MUST state that the value is a git commit hash of
   7 to 64 hexadecimal characters, accepted in any letter case and stored lowercase.
4. **Groadmap reads no repository.** The help MUST state that the caller supplies
   the hash and that `rmp` runs no git command and inspects no working directory.
   This is the point an AI agent is most likely to get wrong, because the natural
   assumption is that a tool storing a commit hash can discover it; the contract's
   `pitfalls` array carries the same warning (see
   `DATA_FORMATS.md § pitfalls array entry`).
5. **What a return to `BACKLOG` does to each field.** The `task stat` and
   `task reopen` helps both describe the clearing behaviour of a return to
   `BACKLOG`. Both MUST name `commit_close` among the cleared fields and MUST state
   that `commit_open` is preserved, because the asymmetry contradicts the pattern
   every other tracking field follows and a reader who assumes symmetry would be
   wrong. `STATE_MACHINE.md § Commit Tracking Fields` is canonical for the rule.

6. **Where the hash is recorded.** The help MUST state that the supplied hash is
   written both to the task and to the audit entry for the transition, and that the
   audit entry keeps it permanently: reopening the task clears `commit_close` on the
   task but changes no audit entry. An agent that has to recover which commit
   concluded a task that was later reopened must know to look in the audit log, and
   it cannot infer that from the flag description alone.
7. **Which operation the transition records.** The help MUST state that a status
   change is recorded under an operation named for the state entered
   (`TASK_STATUS_BACKLOG`, `TASK_STATUS_SPRINT`, `TASK_STATUS_DOING`,
   `TASK_STATUS_TESTING`, `TASK_STATUS_COMPLETED`), so a reader composing an
   `audit list --operation` filter picks the right value without guessing.

`COMMANDS.md § Change Status (stat)` remains canonical for the flags, the
validation order, and the exact error text.

### Sprint family help specifics

The `sprint` family help and the `sprint create` / `sprint update` subcommand
helps follow the same structure template as every other family but MUST
additionally make the sprint-specific rules below explicit, because they cannot
be inferred from the generic template. Both the plain-text help and the
machine-readable AI Agent Contract (`rmp --ai-help`) MUST document them:

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
5. **The `--description` flag semantics.** The help for `-d, --description` MUST
   state what the field is *for*, not only its length cap: a caller must not be
   able to read the help and conclude that any free text will do. The help MUST
   state that the description carries the high-level (macro) goal of the
   development effort the sprint delivers, and that title and description together
   convey what the sprint's tasks aim at. The canonical definition of the field is
   `MODELS.md § Sprint Field Constraints`; the help text below is the wording that
   states it to the caller.

   On `sprint create` the flag is mandatory, so it appears in the `Required:` block
   and its help text reads exactly:

   ```
     -d, --description <text>        Sprint description (max 2048 chars). REQUIRED
                                     on create. It must state the high-level (macro)
                                     goal of the development effort the sprint
                                     delivers: a new development, a fix, a
                                     refactoring, or another kind of change.
                                     Together with the title, it must give a human
                                     or an AI agent a clear macro idea of what the
                                     sprint's tasks are specifically aimed at.
   ```

   On `sprint update` the flag is optional, so it appears in the `Optional:` block
   and its help text reads exactly the same text with the requiredness sentence
   adapted, because the flag documents a NEW description with the same semantics:

   ```
     -d, --description <text>        New sprint description (max 2048 chars). It
                                     must state the high-level (macro) goal of the
                                     development effort the sprint delivers: a new
                                     development, a fix, a refactoring, or another
                                     kind of change. Together with the title, it
                                     must give a human or an AI agent a clear macro
                                     idea of what the sprint's tasks are
                                     specifically aimed at.
   ```

   In the AI Agent Contract, the `flags[]` entry for `--description` on both
   `sprint create` and `sprint update` MUST carry the same semantics in its
   `description` string. See `COMMANDS.md § Create Sprint` and
   `COMMANDS.md § Update Sprint`.

### Audit family help specifics

The `audit` family help and the `audit list` subcommand help follow the same
structure template as every other family but MUST additionally make the rules below
explicit. Both the plain-text help and the machine-readable AI Agent Contract
(`rmp --ai-help`) MUST document them:

1. **The `Valid operations (for --operation filter)` block lists every operation in
   the catalogue.** The block is rendered from the valid set itself rather than
   maintained by hand, so an operation the command accepts can never be missing from
   the help. `DATABASE.md § audit Table` is canonical for the catalogue; the help
   publishes it, and MUST NOT publish a subset of it.
2. **A LEGACY operation is labelled LEGACY where it is listed.** The four LEGACY
   values — `TASK_STATUS_CHANGE`, `TASK_UPDATE`, `SPRINT_UPDATE`, and
   `SPRINT_MOVE_TASK` — are accepted filter values that no command writes, so a
   reader who picks one for current activity gets an empty result. The help MUST
   render them in a separate labelled group, or with an inline `LEGACY` marker on
   each, and MUST state in one sentence that no command writes them and that they
   exist so the older entries carrying them stay filterable. Listing them
   indistinguishably from the operations in use is the defect this rule prevents.
3. **The status operations name their destination.** The help MUST make it visible
   that the five `TASK_STATUS_*` operations are one per task state, so a reader
   filtering for "tasks that started" chooses `TASK_STATUS_DOING` rather than
   scanning a generic status-change value.
4. **The two nullable output keys.** The `Output (stdout JSON)` block of the `audit`
   family help MUST name all seven keys of an audit entry in its object key list,
   `related_entity_id` and `commit_hash` included, and MUST state that both are
   `null` on the operations that do not carry them and are never omitted. An agent
   that does not know a key can be `null` will treat its absence of a value as an
   error.
5. **What `related_entity_id` means.** The help MUST state that it names the
   counterpart entity of the operation that produced the entry — the task a
   `SPRINT_ADD_TASK` entry added, the sprint a `TASK_STATUS_SPRINT` entry names, the
   other task of a dependency pair — and that it is `null` when the operation has no
   counterpart. Without that sentence the key reads as a duplicate of `entity_id`.
   The help MUST NOT suggest that the key's presence follows from the operation name
   alone: `TASK_STATUS_BACKLOG` carries a sprint id from `sprint remove-tasks` and
   `null` from `task stat`.
6. **Valid entity types.** The `-e, --entity-type` flag and the `audit history`
   positional argument both accept exactly `TASK` and `SPRINT`. The help MUST
   enumerate both values on both surfaces.
7. **There is no filter on the two new columns.** The help MUST NOT imply that
   `related_entity_id` or `commit_hash` can be filtered; the accepted filters are
   `--operation`, `--entity-type`, `--entity-id`, `--since`, `--until`, and
   `--limit`.

In the AI Agent Contract, the `AuditOperation` enum carries every value with a
non-empty `description`, and each LEGACY value's description states that no command
writes it and names the operations that replaced it (see
`DATA_FORMATS.md § enums map entry`). `COMMANDS.md § Audit Log Management` remains
canonical for the flags, the validation order, and the exact error text.

### Comment subcommand help specifics

The eight comment subcommands — `comment-add`, `comment-list`, `comment-edit`,
and `comment-remove`, under both `task` and `sprint` — follow the same structure
template as every other subcommand but MUST additionally make three behaviours
explicit, because a reader cannot infer them from the generic template:

1. **The valid type set differs per family, and `--type` already means something
   else in the `task` family.** The `-y, --type` flag has the same name in both
   families and a different valid set in each. Each subcommand help MUST list the
   values its own family accepts: the seven task values (`FINDING`,
   `HYPOTHESIS`, `TEST`, `DECISION`, `PROGRESS`, `UPDATE`, `NOTE`) in the `task`
   family, the four sprint values (`FINDING`, `DECISION`, `PROGRESS`, `UPDATE`)
   in the `sprint` family. It MUST NOT show the task set on a sprint subcommand.

   In the `task` family, `-y, --type` is also the flag that carries the ten
   `TaskType` values on `task list`, `task create`, and `task edit`. The two
   enums therefore share one flag spelling inside one family, so the family help
   MUST carry two distinct "Valid values" blocks, each naming the subcommands its
   block governs — one for the task types accepted by `list`, `create`, and
   `edit`, one for the comment types accepted by the four comment subcommands —
   and MUST NOT merge them into a single list of seventeen values. The `sprint`
   family help carries one comment-type block, `--type` having no other meaning
   there.

   The same separation MUST hold in the machine-readable AI Agent Contract
   (`rmp --ai-help`), where the two comment-type sets are two enum keys,
   `TaskCommentType` and `SprintCommentType`: the enum an agent reaches from a
   `sprint` comment subcommand's `--type` flag carries the four sprint values
   only, and the enum reached from a `task` comment subcommand's `--type` flag
   carries the seven task values, so the two sets are never conflated into a
   single seven-value enum shared by both families, and neither is conflated with
   `TaskType`. See `MODELS.md § Comment Type` and
   `DATA_FORMATS.md § enums map entry`.
2. **Body input.** State, on `comment-add` and `comment-edit`, that the comment
   body comes from `-b, --body` or, when that flag is absent, from standard
   input, and that supplying neither is an error (exit code 2). On
   `comment-edit`, state additionally that standard input is read only when
   `--type` is absent as well, so a type-only edit does not wait for input. See
   `COMMANDS.md § Comment Body Input Source and Precedence`.
3. **Which id the command takes.** State, on `comment-edit` and
   `comment-remove`, that the positional argument is the comment's own id and not
   the id of the task or sprint it belongs to, and that task comment ids and
   sprint comment ids are separate sequences. `comment-add` and `comment-list`
   take the parent's id instead, and their help MUST name the argument
   accordingly (`<task-id>` / `<sprint-id>`).

### Graph family help specifics

The `graph` family help and each graph subcommand help follow the same
structure template as every other family but MUST additionally make two
graph-specific behaviours explicit, because an agent cannot infer them
from the generic template:

1. **Query input.** State that the Cypher query comes from the `--query`
   flag or, when the flag is absent, from standard input, and that
   supplying neither is an error (exit code 2). The `graph` subcommands and
   the comment subcommands of the `task` and `sprint` families are the only
   commands in the CLI that read standard input. See
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
