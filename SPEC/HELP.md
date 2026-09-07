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
  - [Audit operation entity-type classification](#audit-operation-entity-type-classification)
  - [Comment subcommand help specifics](#comment-subcommand-help-specifics)
- [Error message format](#error-message-format)
  - [Stderr part order](#stderr-part-order)
  - [Recovery help after a dispatch failure](#recovery-help-after-a-dispatch-failure)
  - [Exit code of a dispatch failure](#exit-code-of-a-dispatch-failure)
  - [Stdout silence on failure](#stdout-silence-on-failure)
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
- `rmp version`, `rmp --version`, and `rmp -v`: version output is not help.

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
| Global | `rmp help`, `rmp --help`, `rmp -h` | `COMMANDS.md § Global Commands` |
| Global | `rmp version`, `rmp --version`, `rmp -v` | `COMMANDS.md § Global Commands` |
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
| Graph | `rmp graph execute` | `COMMANDS.md § Graph Management` |
| Graph | `rmp graph serve` | `COMMANDS.md § Graph Management` |
| Graph | `rmp graph client` | `COMMANDS.md § Graph Management` |
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
   publishes it, and MUST NOT publish a subset of it. Rendering the block from the
   valid set is also the reason a coverage gate over the block cannot fail when the
   catalogue grows, which is what
   `§ Audit operation entity-type classification` rule 5 addresses.
2. **A LEGACY operation is labelled LEGACY where it is listed.** The four LEGACY
   values — `TASK_STATUS_CHANGE`, `TASK_UPDATE`, `SPRINT_UPDATE`, and
   `SPRINT_MOVE_TASK` — are accepted filter values that no command writes, so a
   reader who picks one for current activity gets an empty result. The help MUST
   render them in their own labelled group, whose form
   `§ Audit operation entity-type classification` rule 6 fixes, and MUST state in
   one sentence that no command writes them and that they
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
writes it and names the operations that replaced it. Every value of that enum also
carries a boolean `legacy` member, `true` on exactly the four LEGACY values named
above and `false` on every other value, so a consumer of the contract tests a field
instead of matching the word `LEGACY` inside a description (see
`DATA_FORMATS.md § enums map entry`). `COMMANDS.md § Audit Log Management` remains
canonical for the flags, the validation order, and the exact error text.

### Audit operation entity-type classification

Every operation in the catalogue is recorded against exactly one entity. A row
carrying the operation writes that entity's type — `TASK` or `SPRINT` — in the
audit entry's `entity_type` column, and the entry belongs to that entity's
history. Both published surfaces, the `audit` family help and the AI Agent
Contract (`rmp --ai-help`), MUST publish every operation together with the entity
type it is recorded against, so that a reader choosing an `--operation` filter
knows whose history the filter returns before running it.

Six rules govern how that classification is produced, published, and guarded.

1. **The classification is declared, never inferred from the operation's name.**
   `MODELS.md § Audit Operation` names an operation `<ENTITY>_<SUBJECT>_<OUTCOME>`,
   and for every operation a command writes today the entity in the name is also
   the entity the operation is recorded against. That agreement is a property the
   catalogue happens to have, not a rule the catalogue is held to, and an
   implementation MUST NOT turn it into one by reading the classification off the
   name.

   What separates the two is the difference between arranging a list and
   asserting a fact. Printing an operation under a heading that names `TASK`
   states that the rows carrying that operation hold `entity_type = 'TASK'`, which
   is a claim about stored data. The day one operation is recorded against the
   entity its name does not begin with — a `TASK_*` operation written against a
   sprint — an inferred claim becomes false, and it becomes false silently,
   because a prefix match has no way to notice that it now disagrees with the
   writer. A declared claim cannot fail that way: it sits beside the operation it
   describes, so an operation whose writer changes while its declaration does not
   is a contradiction that rules 4 and 5 make someone see.

2. **One declaration, read by both surfaces.** The classification lives in a
   single declaration next to the operation constants in `internal/models`, and
   both surfaces render from it. Neither surface may hold its own copy: a
   classification written twice is a classification that can disagree with itself,
   and the disagreement would appear as a plain-text help and a machine-readable
   contract that describe the same operation differently. This is the principle of
   `ARCHITECTURE.md § AI Agent Contract Generation` (Single source of truth)
   applied to a fact the command registry does not carry, the registry describing
   commands and flags rather than the operation catalogue.

   The same declaration carries the LEGACY marking that rule 2 of
   `§ Audit family help specifics` requires, for the reason rule 1 gives. Whether
   an operation is still written is also a fact about the code, and a surface that
   recovers it by searching a description string for the word `LEGACY` is
   inferring again, from text this specification requires for a reader rather than
   for a parser.

   `DATABASE.md § audit Table` remains canonical for which operations exist and
   for what each of them records; the declaration is the machine-readable form of
   the entity each operation is recorded against, and it MUST NOT contradict that
   catalogue. The declaration MUST NOT be derived from the catalogue's group
   headings either: those headings are the layout of a document, and the coverage
   gate over the catalogue region requires only that they still exist, not that
   any entry sits under the right one.

3. **The classification is total.** Every value the catalogue holds has exactly
   one declared entity type, LEGACY values included. There is no third value, no
   unknown, and no value that declines to answer: `entity_type` is `NOT NULL` on
   every row of the audit table and its `CHECK` admits exactly `TASK` and `SPRINT`
   (see `DATABASE.md § audit Table`), so an operation with no entity type would
   describe rows that cannot exist.

4. **A declaration states what the writer writes.** For every operation a command
   writes, the declared entity type MUST equal the `entity_type` of the rows that
   command produces, and the agreement MUST be established by observing such a row
   rather than by reading the operation's name.

   The four LEGACY operations have no writer left to observe. Each of their
   declarations rests instead on the recorded evidence about the rows that already
   carry it: the predicate the schema migration filters on when it reclassifies
   such rows (see `VERSION.md § Migration 1.11.0 to 1.12.0`), and, for the
   operations no migration reads, the retired writer that git history preserves. A
   LEGACY operation MUST NOT be classified from its name either. Nothing writes it
   any more, so the name is the only thing left to guess from, and guessing is
   what this section exists to prevent.

5. **The gate is over the classification, not over the operation list.** The
   `Valid operations (for --operation filter)` block is rendered from the
   catalogue itself, as rule 1 of `§ Audit family help specifics` requires, so it
   cannot omit an operation the catalogue holds. That guarantee is real, and it is
   also the limit of what a coverage gate over the block can detect: such a gate
   fails when the block names an operation the catalogue does not hold — a
   hand-written list that outlived the catalogue — and it cannot fail when the
   catalogue grows, because a newly declared value is rendered the moment it is
   declared. Adding one operation to the catalogue and running the full suite
   bears this out: the gate over the canonical catalogue in the specification
   fails, the gate over the documentation fails, and the contract gates fail,
   while the package that renders the help passes.

   A new operation therefore reaches the help by itself, but it arrives
   unclassified. Two requirements follow, and neither is a restatement or a
   simplification of the other:

   - **(a) A gate MUST fail when any value in the catalogue has no declared
     entity type.** This is the only check a new operation cannot pass by doing
     nothing, and it MUST live where the operation constants are declared, so that
     the failure reaches whoever added the value on the first test run of the
     package they changed.
   - **(b) The `Valid operations` block MUST NOT contain a catch-all group** — a
     group that collects whatever the entity-type groups did not match. A
     catch-all is what makes (a) evadable in practice: with one present, an
     unclassified operation is still printed, under a heading that asserts nothing
     about it, the block still lists every operation the command accepts, and the
     reader is told nothing about the entity whose history the new operation
     belongs to. Every group in the block MUST be labelled with exactly one entity
     type and MUST hold exactly the operations declared against that entity type.

   The catch-all in the block this rule replaces was not an oversight. It was
   there because the grouping was done by name prefix and the prefix was not
   trusted to match every name, which is the same distrust rule 1 states. Once the
   classification is declared and total, nothing can be left over, and a group for
   what is left over can only hide the failure that (a) exists to produce.

6. **How each surface publishes the classification.** In the `audit` family help,
   the `Valid operations (for --operation filter)` block is partitioned into
   labelled groups as rule 5(b) requires, and the LEGACY operations of each entity
   type form their own group, whose label names the entity type and the LEGACY
   status together. The list column of the block carries operation names and
   nothing else: an inline marker beside a name would put a token in that column
   that is not an operation the command accepts, and the block is checked on
   exactly that basis — everything it lists is a value `audit list --operation`
   takes. Both facts stay readable per operation because the label of the group
   carries them.

   In the AI Agent Contract, every value of the `AuditOperation` enum carries its
   entity type in a member of its own rather than inside its prose, and carries in
   a second member the LEGACY marking that rule 2 of this section places in the same
   declaration. Both members, their value sets, and the rule that each is present on
   every value of that enum are specified in `DATA_FORMATS.md § enums map entry`.

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

The `graph` family help and the `graph execute` help follow the same
structure template as every other family but MUST additionally make four
graph-specific behaviours explicit, because an agent cannot infer them
from the generic template. Items 5 to 10 add the behaviours the graph
server and its client introduce; items 1 to 4 continue to govern
`execute`.

1. **Query input.** State that the Cypher statement comes from the `-q` /
   `--query` flag or, when the flag is absent, from standard input, and that
   supplying neither is an error (exit code 2). `graph execute` and the
   comment subcommands of the `task` and `sprint` families are the only
   commands in the CLI that read standard input. See
   `GRAPH.md § Cypher Input Source and Precedence`.
2. **One subcommand, and what it runs.** State that `execute` runs any
   Cypher statement the engine accepts — a read, a write, a deletion, a
   schema change — and that `rmp` does not examine the statement or refuse
   it on the ground of what it does. The help MUST NOT describe any
   statement as rejected before execution on its operation class, because
   none is. See `COMMANDS.md § Graph Management`.
3. **Schema statements.** State that index and constraint DDL and the
   `SHOW INDEX(ES)` / `SHOW CONSTRAINT(S)` commands run through this same
   subcommand, and name them, rather than leaving an agent to discover them
   by trial. See `GRAPH.md § Schema Management`.
4. **The statement time budget.** The exit-codes block of this help
   enumerates the causes of each code rather than naming the code alone
   (see [Help structure template](#help-structure-template), block 8), and
   the budget is a cause of exit code 1 that a caller cannot infer from
   "a Cypher parse or execution error": the statement was valid and the
   store was healthy. The exit-code-1 line MUST therefore name it, stating
   that a statement running past the 5-second budget is cancelled and that
   nothing is written. It is the one failure whose remedy is to rewrite a
   working statement, so the help states the remedy too — narrow the
   statement, or split it — in the same terms as the published error line.
   It introduces no new exit code, so no line is added to the block. See
   `GRAPH.md § Statement Time Budget` and `COMMANDS.md § Graph Management`.
5. **Three subcommands, and what each is for.** The family help MUST list
   `execute`, `serve` and `client`, each with a verb-first description, and
   MUST make the distinction between them explicit in one sentence rather
   than leaving it to be inferred from three summaries: `execute` runs a
   statement against the roadmap's graph, `serve` makes that graph available
   over a socket until it is stopped, and `client` sends a statement to a
   running server. See `COMMANDS.md § Graph Management`.
6. **A running server is used automatically; the flag chooses the target, not
   the path.** The `execute` help MUST state that when a server is serving the
   selected roadmap the statement is sent to that server instead of opening
   the store, that this happens with no flag and no configuration, and that
   the result and the exit code are the same either way. It MUST additionally
   document `--socket <path>` for what it is: the socket the invocation
   resolves, defaulting to `~/.roadmaps/<name>/graph.sock`, written only when
   the server was started with the same flag. The help MUST NOT present it as
   a switch that forces or forbids a server, because it is neither. An agent
   told only "it is automatic" would have no way to reach a server on a
   non-default socket; an agent told only "there is a flag" would write it on
   every invocation. See `GRAPH.md § Server Resolution` and
   `GRAPH.md § Serving on a Non-Default Socket`.
7. **`client` requires a server and does not fall back.** The `client` help
   MUST state that a roadmap with no server listening is a failure (exit code
   1) and not a fall back onto the store, and MUST name the socket it
   resolves: `~/.roadmaps/<name>/graph.sock` unless `--socket` overrides it.
   This is the one place the two Cypher-running subcommands differ — they take
   the same statement sources and the same `--socket` flag, and differ only in
   whether an unanswered socket is a fallback or a failure — so it is the one
   an agent choosing between them needs. See `GRAPH.md § The Bolt Client`.
8. **`serve` is long-lived, and it is the second such command.** The `serve`
   help MUST state that the command does not complete and exit: it serves
   until `Ctrl+C` (`SIGINT`) or `SIGTERM`, then drains, checkpoints and exits
   0, exactly as `rmp web` does and unlike every other command. It MUST state
   that it prints the bound socket path as JSON on startup, and that one
   server runs per roadmap, a second against the same roadmap failing rather
   than queuing. See `GRAPH.md § The Dedicated Graph Server`.
9. **The socket's access model is stated, because it is the whole access
   model.** The `serve` help MUST state that the socket is created with mode
   `0600` inside the roadmap's `0700` home directory, that the server
   authenticates nobody, and that any caller able to open the socket can read,
   write, delete and change the schema of that roadmap's graph. Where the
   `serve` help documents `--socket`, it MUST state the consequence a user
   cannot infer: the CLI follows a non-default socket through the same flag,
   and the web interface cannot follow it at all and fails against that
   roadmap's graph for as long as the server runs
   (`GRAPH.md § Serving on a Non-Default Socket`). It is the same
   obligation item 2 of `Web command help specifics` places on `rmp web` for
   the same reason: a surface with no authentication has to say so where the
   reader is, not only in the specification. See
   `GRAPH.md § Socket Path and Permissions`.
10. **The exhausted serialisation conflict, for item 4's reason and with more
    force.** It is a second cause of exit code 1 that a caller cannot infer
    from "a Cypher parse or execution error": the statement was valid and the
    store was healthy, and the failure is contention between writers. The
    exit-code-1 line of the `execute` help and of the `client` help MUST
    therefore name it, stating that every attempt of the retry policy lost a
    serialisation conflict against a server and that nothing was written — a
    fact rather than a hope, because the conflict is detected before anything
    is applied. Both helps carry it because both subcommands have the cause:
    `client` always reaches a server, and `execute` reaches one whenever the
    roadmap is served. The prose paragraph the `client` help already carries
    about contention does not discharge this obligation, because the
    exit-codes block is where a caller goes to learn why a command exited 1.

    **It is the one cause of exit code 1 whose remedy is to run the SAME
    statement again**, and the help MUST state that, because it is what
    separates this cause from every other one in the block: a parse or
    execution error asks the caller to correct a broken statement, the
    statement time budget of item 4 asks it to rewrite a working one, and this
    cause asks it to change nothing. The help states the second half of the
    remedy too — spread concurrent writes across distinct nodes — in the same
    terms as the published error line, that being the measure which removes the
    failure rather than moving the point at which it appears.

    **The help MUST NOT write the retry budget's figure into its own text.**
    The published error line renders that figure from the retry policy instead
    of spelling it out, so that one quantity keeps one expression; a figure
    written into a help string would be a second expression of it, and would
    disagree with the policy silently the moment the policy changed. Where a
    help names the budget at all, it MUST render it from the same policy the
    error line reads. Nothing the caller decides needs the figure: the cause,
    the fact that nothing was written, and the remedy are what the block owes,
    and the error line carries the figure at the moment it is relevant. This is
    rule 2 of `Audit operation entity-type classification` applied to a
    duration, and behind it `ARCHITECTURE.md § AI Agent Contract Generation`.

    It introduces no new exit code, so no line is added to either block — the
    exit-code-1 line gains a clause, exactly as item 4's cause does. The
    command contract already states the cause: a row of its own in
    `COMMANDS.md § Execute Exit Codes`, and a clause of the single
    exit-code-1 row in `COMMANDS.md § Client Exit Codes`. What this item
    closes is therefore a divergence between the contract and the published
    help rather than an unspecified behaviour. See
    `GRAPH.md § Concurrency Inside the Server`, canonical for the mechanism and
    for the measured remedy, and `IMPLEMENTATION.md § Retry Logic`, canonical
    for the policy whose exhaustion this reports.

The `graph serve` and `graph client` helps carry an `Exit codes:` block like
every other subcommand help, listing only the codes each can emit;
`ARCHITECTURE.md § Exit Codes of the Graph Server and Client` enumerates
them, and `COMMANDS.md § Serve Exit Codes` and
`COMMANDS.md § Client Exit Codes` are the command contract for them. Neither
subcommand adds an exit code to the catalogue.

### Web command help specifics

`rmp web` is a single command with no subcommands. Its help follows the same
block order as every other command (Usage, description, Options, Output, Exit
codes, Examples), and MUST additionally make explicit three behaviours an agent
or user cannot infer from the generic template:

1. **No roadmap flag.** State that `rmp web` does **not** take `-r` /
   `--roadmap`: it lists all roadmaps and the user selects one in the browser.
   This is the one command exempt from the always-required-roadmap rule (see
   `COMMANDS.md § Roadmap Selection (Always Required)`).
2. **Pages are read-only, the graph endpoint is not, and loopback is the
   default.** State that every page the server renders is read-only and that the
   server never writes to a roadmap's `project.db`; that the graph page's query
   bar is the exception, because the statement it submits is executed as written
   and may create, change, or delete graph data, with no authentication; and that
   the server binds loopback (`127.0.0.1`) by default, so it is reachable only
   from the local machine. State that
   `--host 0.0.0.0` is the explicit opt-in to expose it on all interfaces
   (network-reachable); and that `--host`/`--port` override the bind address,
   with the default-port ephemeral fallback. See `WEB.md`.
3. **Long-lived process.** State that the command starts a server that keeps
   running until interrupted (`Ctrl+C` / `SIGINT` or `SIGTERM`), unlike every
   command except `rmp graph serve`, which completes and exits. On startup it prints the served
   URL; with `--no-open` it does not launch a browser.

The skeleton (illustrative; the canonical contract is
`COMMANDS.md § Web Interface`):

```
Usage: rmp web [options]

Start a web interface for the roadmaps under ~/.roadmaps/. The browser
lists every roadmap and lets you view its tasks, sprints, and knowledge
graph. Every page is read-only and no roadmap database is ever written.
The knowledge-graph query bar is the exception: it runs the Cypher you
type, including statements that write or delete, and it is not
authenticated. rmp web does not take -r/--roadmap.

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

When a command fails, the application writes a plain-text error to
stderr. JSON is never used for errors. A failing invocation writes
nothing to stdout at all; see `Stdout silence on failure` below.

### Error line

Every failing invocation writes exactly one error line, in this shape:

```
Error: <human-readable description of the problem>
```

The wording starts with `Error: ` (with the colon and the trailing
space). For input-related errors (missing parameters, unknown flags,
unresolved subcommands, invalid argument formats) the description names
the offending flag or value. For non-input errors (resource not found,
already exists, database failure) the description names the entity and
its id where relevant.

### Stderr part order

Stderr is assembled from up to four parts. They always appear in this
order:

| Order | Part | When it is present |
|-------|------|--------------------|
| 1 | The AI-agent hint line, followed by one blank line | Only when `AI_AGENT=1` is active for the invocation. See `AI_AGENT environment variable` below |
| 2 | The `Error: ` line | On every failing invocation |
| 3 | One blank line, then the recovery help | Only on a dispatch failure. See `Recovery help after a dispatch failure` below |
| 4 | One blank line, then the AI-agent hint line | On every failing invocation, unless a suppression rule below applies |

Parts 1 and 4 carry the same sentence, and they are never both present
in the same invocation: the deduplication rule in
`AI_AGENT environment variable` keeps the reader's count at exactly one.

### Recovery help after a dispatch failure

A **dispatch failure** is the case in which `rmp` cannot resolve a name
it was given to a command or to a subcommand. There are exactly two
classes:

1. **Unresolved command.** The first non-flag token of the invocation
   does not name a command or a command alias, as in `rmp nadadisto`.
2. **Unresolved subcommand.** The command resolves, but the next
   non-flag token does not name one of that command's subcommands or
   subcommand aliases, as in `rmp task nadadisto`. The commands that
   dispatch subcommands are `roadmap`, `task`, `sprint`, `backlog`,
   `audit`, and `graph`.

On a dispatch failure, and only on a dispatch failure, the CLI writes
the **recovery help** to stderr after the error line. The recovery help
is the help body for the level at which the name could not be resolved,
so the reader is shown the list of names that would have worked:

| Class | Recovery help written to stderr |
|-------|---------------------------------|
| Unresolved command | The global help body, that is, the body `rmp --help` prints |
| Unresolved subcommand | The family help body of the command that did resolve, that is, the body `rmp <command> --help` prints |

The recovery help omits the AI-agent banner that opens the same body on
stdout (see `AI agent banner`), together with the blank line that
follows the banner. The banner and the hint carry the same sentence, and
the invocation already ends with the hint.

No other error class appends help. A missing required parameter, an
unknown flag, an invalid enum value, an out-of-range value, a rejected
state transition, a resource that does not exist, a name conflict, and a
database failure each produce the error line and the hint alone. The
error line already names the offending flag or value in those cases, so
the reader recovers by running `--help` explicitly.

Three commands accept no subcommand, so no dispatch failure can arise
for them and the recovery help never applies: `stats`, `web`, and
`ai-help`. All three reject an unexpected positional argument as an
invalid argument, with exit code `2` and no help. Every command in the
CLI does: the rule, the declared maximum per command, and the commands
that publish their own wording for the refusal are specified in
`COMMANDS.md § Positional Arguments`.

### Error text of a dispatch failure

The two classes use the same wording, differing only in the level they
name:

| Class | Error line |
|-------|-----------|
| Unresolved command | `Error: unknown command: <name>` |
| Unresolved subcommand | `Error: unknown <command> subcommand: <name>` |

Neither line carries a sentinel prefix. In particular neither is
prefixed with `invalid input: `, because a dispatch failure is not
carried by `utils.ErrInvalidInput` and does not exit with that
sentinel's code. See `ARCHITECTURE.md § Sentinel Error Catalogue`.

### Exit code of a dispatch failure

Both dispatch-failure classes exit **127** (`EXIT_CMD_NOT_FOUND`). They
are the same failure observed at two levels of the command tree, and the
exit code does not distinguish them. The catalogue is in
`ARCHITECTURE.md § Exit Codes`.

One case is deliberately excluded. When an unresolved command or
subcommand name is used to scope the AI contract, as in
`rmp nadadisto --ai-help` or `rmp task nadadisto --ai-help`, the name is
a scope selector for `--ai-help` rather than a name being dispatched.
That failure keeps exit code **2**, and it prints no recovery help. It
is specified in `COMMANDS.md § AI Help`.

### Stdout silence on failure

An invocation that exits with a non-zero code writes **zero bytes** to
stdout. Every part of a failure report goes to stderr: the error line,
the recovery help, and the hint. A consumer may therefore treat a
non-empty stdout as evidence that the invocation succeeded.

Help that the reader asked for is not a failure, and this rule does not
restrict it. Each of the following exits `0` and writes its help body to
stdout:

- `rmp` with no arguments, and `rmp help`.
- `rmp --help` and `rmp -h`.
- `rmp <command>` with no subcommand, and `rmp <command> --help`.
- `rmp <command> <subcommand> --help`.

### Examples

A missing required parameter: error line and hint, no help, exit code 2.

```
$ rmp task create -r myproject
Error: required parameter missing: --title

AI agents: run `rmp --ai-help` for a machine-readable command contract.
```

An unresolved subcommand: error line, family help, hint, exit code 127.
Nothing reaches stdout.

```
$ rmp task nadadisto -r myproject
Error: unknown task subcommand: nadadisto

Usage: rmp task <subcommand> [options]

Subcommands:
  create, new      Create a task
  ...the remainder of the family help body...

AI agents: run `rmp --ai-help` for a machine-readable command contract.
```

An unresolved command: error line, global help, hint, exit code 127.

```
$ rmp nadadisto
Error: unknown command: nadadisto

Groadmap - A CLI tool for managing technical roadmaps

Usage: rmp [command] [subcommand] [arguments] [options]
  ...the remainder of the global help body...

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
