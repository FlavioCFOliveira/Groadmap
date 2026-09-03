# CLI Commands

## Table of Contents

- [Naming Conventions](#naming-conventions)
- [Command Structure](#command-structure)
- [Error Handling](#error-handling)
- [Positional Arguments](#positional-arguments)
- [Field Validation](#field-validation)
- [Global Commands](#global-commands)
- [AI Agent Contract](#ai-agent-contract)
- [Exit Codes](#exit-codes)
- [Roadmap Selection (Always Required)](#roadmap-selection-always-required)
- [Roadmap Management](#roadmap-management)
- [Task Management](#task-management)
- [Sprint Management](#sprint-management)
- [Audit Log Management](#audit-log-management)
- [Backlog Management](#backlog-management)
- [Statistics Command](#statistics-command)
- [Graph Management](#graph-management)
- [Web Interface](#web-interface)
- [Command Aliases Reference](#command-aliases-reference)

## Naming Conventions

- Commands: lowercase, kebab-case (`list`, `create`)
- Flags: double-dash for long (`--help`), single-dash for short (`-h`)
- Subcommands: clear hierarchy (`rmp roadmap list`)

## Command Structure

```
rmp [command] [subcommand] [arguments] [options]
```

## Error Handling

Errors follow typical CLI conventions (NOT JSON format):

### Default Behavior
- Error messages are written explicitly to **stderr**
- Plain text format (human-readable)
- Uses standard Unix exit codes

### Published Error Strings Are Exact

Every error string this file publishes is the **complete line the user reads on stderr**, including the `Error: ` prefix and including the sentinel text that follows it. A published string is never the message body alone, and never a paraphrase: a reader may compare a published string against captured stderr character for character, and a test may assert it verbatim.

Three consequences follow, and they hold for every table and every code block in this file:

1. **The `Error: ` prefix is part of the string.** `rmp` writes `Error: ` before every failure message, so a published string that omits it is incomplete.
2. **The sentinel text is part of the string.** Most messages carry a sentinel word or phrase between the prefix and the detail — `validation error: `, `required parameter missing: `, `resource not found: `, `invalid input: `, `field exceeds maximum size: `, `resource already exists: `, `no roadmap selected: `, `database error: `, `unknown command`. The sentinel names the failure class and determines the exit code; `ARCHITECTURE.md § Sentinel Error Catalogue` is canonical for the mapping. A message that carries no sentinel is published without one, because that is what the user sees.
3. **One string means one condition.** A row publishes the string for the single condition its own scenario names. Where one flag or field can fail in more than one way — absent versus empty, empty versus oversize — each way is a separate row with its own string and its own exit code, because the binary prints a different line for each.

**Placeholders.** A published string contains a placeholder only where the binary interpolates a value it cannot know in advance. Everything outside a placeholder is literal text, and a placeholder never stands for a fixed word: where the binary always prints the same text, that text is published. The complete set of placeholders is:

| Placeholder | Stands for |
|-------------|------------|
| `X` | An offending value, echoed as the user supplied it. Where the binary quotes it, the quotes are shown around the placeholder. |
| `N`, `M` | A number the binary computes or echoes: an id, a length, a limit, a count. `M` appears only where one string carries two distinct numbers. |
| `Y` | A second offending value, in the one message that names two. |
| `<entity>` | The word that names the entity whose id a message refuses, resolved by `§ Entity Identifier Range (All Positional Ids and --entity-id)`. |
| `<field>` | A published field name, resolved by `Published Field Names in Validation Messages` below. |
| `<flag>` | A flag name, in its kebab-case spelling and without the leading dashes, which the string supplies. |
| `<sentinel>` | The sentinel text of the failure class the surface reports, in the one published string whose sentinel varies by surface, resolved by `§ Entity Identifier Range (All Positional Ids and --entity-id)`. |
| `<detail>`, `<engine diagnostic>` | Text produced by a component other than `rmp` — the operating system, or the Cypher engine. Not specified here. |
| `<socket>` | The filesystem path of a graph server's Unix domain socket, as the invocation resolved it: the default derived from the roadmap, or the value of `--socket`. Resolved by `GRAPH.md § Socket Path and Permissions`. |
| `<ids>` | One or more ids, space-separated, in the order the user supplied them, as Go renders a slice of integers. The square brackets that surround the list in the message are literal text and are shown outside the placeholder. |
| `<absolute path of ~/.roadmaps>` | The resolved data-directory path. |

**Angle brackets are not always a placeholder.** Two messages print angle brackets literally, because the binary's own text contains them: `Error: no roadmap selected: use -r <name> or --roadmap <name>` and `Error: resource not found: no sprint is currently open. Use 'rmp sprint start <id>' to open a sprint first`. In those two lines `<name>` and `<id>` are characters the user sees, not values to substitute. Only the bracketed forms listed in the table above are placeholders.

This section states the convention once. The tables below do not restate it.

### Failing Invocations Write Nothing to Stdout

An invocation that exits with a non-zero code writes **zero bytes** to stdout. The error line, any help that accompanies it, and the AI-agent hint all go to stderr. A consumer may therefore treat a non-empty stdout as evidence that the invocation succeeded.

Help that the reader asked for is not a failure: `rmp` with no arguments, `rmp help`, `rmp --help`, `rmp -h`, `rmp <command>` with no subcommand, `rmp <command> --help`, and `rmp <command> <subcommand> --help` each exit `0` and write their help body to stdout.

### Dispatch Failures (Unresolved Command or Subcommand Names)

A **dispatch failure** is the case in which `rmp` cannot resolve a name it was given to a command or to a subcommand. There are exactly two classes, and they behave identically:

| Class | Example | Error line |
|-------|---------|-----------|
| Unresolved command | `rmp nadadisto` | `Error: unknown command: nadadisto` |
| Unresolved subcommand | `rmp task nadadisto` | `Error: unknown task subcommand: nadadisto` |

Both classes:

1. Exit with code **127** (`EXIT_CMD_NOT_FOUND`). The catalogue is in [ARCHITECTURE.md § Exit Codes](./ARCHITECTURE.md#exit-codes).
2. Write the **help for the level at which the name could not be resolved** to stderr, after the error line: the global help body for an unresolved command, and the family help body of the command that did resolve for an unresolved subcommand.
3. Write nothing to stdout.

The commands that dispatch subcommands, and for which the second class can arise, are `roadmap`, `task`, `sprint`, `backlog`, `audit`, and `graph`. The commands `stats`, `web`, and `ai-help` accept no subcommand, so no dispatch failure arises for them.

A dispatch failure is the **only** error class after which help is written. Every other error class — a missing required parameter, an unknown flag, an invalid enum value, an out-of-range value, a rejected state transition, a resource that does not exist, a name conflict, and a database failure — produces the error line and the AI-agent hint alone, with no help. The reader recovers from those by running `--help` explicitly, because the error line already names the offending flag or value.

`HELP.md § Error message format` is the canonical specification of the error output: the parts of stderr, their order, the exact error wording, and the suppression of the AI-agent banner inside the help written on an error path.

---

## Positional Arguments

A **positional argument** is a command-line token that is neither a flag nor the value of a flag. `rmp` reads positional arguments once the command and the subcommand names have been resolved, and each command uses them for the values its own block below names: an id, a comma-separated list of ids, a status, a roadmap name, a count.

This section is canonical for how many positional arguments each command accepts and for what happens when an invocation supplies more than that. It states the rule once, for the whole CLI. A command's own block may point back here, and its error table may carry the refusal alongside that command's other errors so the table stays a complete list, but no block states a rule of its own. Where a command's wording differs from the canonical line, this section names the command below.

### Declared Arity

Every command declares the **maximum number of positional arguments it accepts**. The declaration lives with that command's flag definitions, so the whole of a command's argument surface — its flags and its positional arity — is declared in one place, and every consumer of that surface reads the arity from the same declaration, including the machine-readable contract `rmp --ai-help` publishes (`DATA_FORMATS.md § AI Agent Contract`, key `positional_arguments`).

A **single shared enforcement point** compares the positional arguments an invocation supplied against the declaration of the command being invoked. The rule is not a helper that each command calls: a check every call site must remember to perform is a check some call site will not perform, and an invocation that slips past such a check is accepted while a token the user meant to matter is silently discarded. Enforcement is therefore reached by every command by construction, from the declaration alone. A command that takes no positional argument declares a maximum of zero and is enforced identically.

**Six global forms are enforced separately from that point.** `rmp help`, `rmp --help`, `rmp -h`, `rmp version`, `rmp --version`, and `rmp -v` describe the binary itself rather than any one command family. They are not entries in the command registry, and they are resolved before command lookup runs, so they exit before the shared enforcement point is ever reached. The reader must not conclude from the paragraph above that the shared point covers them: it does not, and it cannot, because nothing has been looked up when these six are answered. They obey the same contract all the same. Each of the six declares a maximum of zero positional arguments, and each refuses an excess positional argument with exit code `2` and the error line rule 1 below publishes, writing neither a help body nor a version line to stdout. This is the one place in the CLI where the rule is enforced at two points instead of one; the resolution order forces the duplication, and the two points must produce the same exit code and the same error line.

The rules are:

1. **An invocation that supplies more positional arguments than the command declares is refused.** The exit code is `2` (`EXIT_MISUSE`, `utils.ErrInvalidInput`) and the error line is `Error: invalid input: unexpected argument "X"`, where `X` is the offending token, echoed as the user supplied it.
2. **The first offending token is named, and only that one.** When several positional arguments exceed the maximum, the command names the first of them in command-line order and stops.
3. **The position of the offending token does not matter.** What is refused is whatever positional arguments remain once the command's flags and their values have been consumed, not a particular slot on the command line. An extra token written between two flags and one written at the end of the line are the same error.
4. **A comma-separated list is one positional argument.** Every command that takes a list of ids takes it as a single token, without spaces. `rmp task get -r <name> 12,13,14` supplies one positional argument and is within an arity of one; `rmp task get -r <name> 12 13 14` supplies three and is refused.
5. **A token that begins with `-` is normally a flag, not a positional argument.** An unrecognised one is refused as an unknown flag — `Error: invalid input: unknown flag: --foo` — under the same exit code `2`. Two commands refine that classification and each states its own rule: the comment subcommands treat every `-`-prefixed token as a flag, digits included (`Comment Positional Argument Contract` below, rule 2), while the two subcommands that read a Cypher statement, `graph execute` and `graph client`, treat a `-` followed by a digit or a decimal point as a numeric value rather than a flag (`GRAPH.md § Cypher Input Source and Precedence`, rule 4). A stray `-1` is therefore an excess positional argument on either of those two and an unknown flag on a comment subcommand.
6. **The refusal precedes every side effect.** It happens while the arguments are parsed: before the roadmap database is opened, before the graph store is opened, and before standard input is read. A refused invocation therefore creates nothing, changes nothing, deletes nothing, writes no audit entry, and writes zero bytes to stdout. It is refused even when it also carries a value that would fail validation on its own with exit code `6`, and even when it names a roadmap, task, sprint, or comment that does not exist, which on its own would be exit code `4`.
7. **No help follows the refusal.** An excess positional argument is not a dispatch failure, so stderr carries the error line and the AI-agent hint alone (`HELP.md § Error message format`).
8. **The rule governs the maximum only.** A required positional argument that is absent is refused by the command's own contract, with the message that command's block publishes.

An unresolved command or subcommand name is resolved before any of this and stays a dispatch failure: `rmp task nadadisto 1 2 3` exits `127` with the `task` family help, not `2`, because the name never resolved to a command whose arity could be checked (`§ Dispatch Failures (Unresolved Command or Subcommand Names)`).

**Commands that publish a different line.** Three commands already refused an excess positional argument before this rule was stated, and each keeps the wording it publishes. Two of them are published here:

| Command | Error line |
|---------|-----------|
| `rmp graph execute` | `Error: invalid input: unexpected argument "X" (graph queries use --query or stdin)` |
| `rmp ai-help` | `Error: ai-help accepts no positional arguments or flags other than --help` |

The `graph execute` line is the canonical line with a hint appended naming the two sources a Cypher query may come from; the exit code and the rest of the line are unchanged. **The hint is part of the published line and not an incidental remark**: a caller matching that line matches it in full, for the reason `§ Published Error Strings Are Exact` gives for every other error line. It stays confined to the two subcommands that read a Cypher statement, `graph execute` and `graph client`, because it names the two sources such a statement may come from and no other command has them; `graph serve` reads no statement and publishes the canonical line of rule 1 instead. `GRAPH.md § No Positional Query: A Stray Token Is Refused` is canonical for those subcommands' whole rule — the line, the classification of a `-`-prefixed token, and where the refusal lands in its order. The `ai-help` line carries no sentinel and covers an unrecognised flag as well as a positional argument; `§ AI Help` is canonical for it. The third is `rmp web`, whose line writes the offending token after a colon and without quotes; `§ Web Interface` publishes it, in that command's own error table.

### Positional Arity by Command

The table publishes the declared maximum for every command in the CLI. It is canonical for the count. Each command's own block below is canonical for what its arguments mean, which of them are required, and how each value is validated; a name in square brackets marks an optional argument.

| Command | Max | Positional arguments |
|---------|-----|----------------------|
| `rmp` with no arguments, `rmp help`, `rmp --help`, `rmp -h` | 0 | - |
| `rmp version`, `rmp --version`, `rmp -v` | 0 | - |
| `rmp --ai-help`, `rmp ai-help` | 0 | - |
| `roadmap list` | 0 | - |
| `roadmap create` | 1 | `<name>` |
| `roadmap remove` | 1 | `<name>` |
| `task list` | 0 | - |
| `task create` | 0 | - |
| `task get` | 1 | `<ids>` |
| `task next` | 1 | `[num]` |
| `task edit` | 1 | `<id>` |
| `task remove` | 1 | `<ids>` |
| `task stat` | 2 | `<ids> <new-status>` |
| `task prio` | 2 | `<ids> <priority>` |
| `task sev` | 2 | `<ids> <severity>` |
| `task reopen` | 1 | `<ids>` |
| `task subtasks` | 1 | `<id>` |
| `task add-dep` | 2 | `<task-id> <blocker-id>` |
| `task remove-dep` | 2 | `<task-id> <blocker-id>` |
| `task blockers` | 1 | `<id>` |
| `task blocking` | 1 | `<id>` |
| `task comment-add` | 1 | `<task-id>` |
| `task comment-list` | 1 | `<task-id>` |
| `task comment-edit` | 1 | `<comment-id>` |
| `task comment-remove` | 1 | `<comment-id>` |
| `sprint list` | 0 | - |
| `sprint create` | 0 | - |
| `sprint get` | 1 | `<id>` |
| `sprint show` | 1 | `<id>` |
| `sprint tasks` | 1 | `<id>` |
| `sprint open-tasks` | 1 | `<id>` |
| `sprint stats` | 1 | `<id>` |
| `sprint start` | 1 | `<id>` |
| `sprint close` | 1 | `<id>` |
| `sprint reopen` | 1 | `<id>` |
| `sprint update` | 1 | `<id>` |
| `sprint remove` | 1 | `<id>` |
| `sprint add-tasks` | 2 | `<sprint-id> <task-ids>` |
| `sprint remove-tasks` | 2 | `<sprint-id> <task-ids>` |
| `sprint move-tasks` | 3 | `<from-id> <to-id> <task-ids>` |
| `sprint reorder` | 2 | `<sprint-id> <task-ids>` |
| `sprint move-to` | 3 | `<sprint-id> <task-id> <position>` |
| `sprint swap` | 3 | `<sprint-id> <task-id-1> <task-id-2>` |
| `sprint top` | 2 | `<sprint-id> <task-id>` |
| `sprint bottom` | 2 | `<sprint-id> <task-id>` |
| `sprint comment-add` | 1 | `<sprint-id>` |
| `sprint comment-list` | 1 | `<sprint-id>` |
| `sprint comment-edit` | 1 | `<comment-id>` |
| `sprint comment-remove` | 1 | `<comment-id>` |
| `audit list` | 0 | - |
| `audit history` | 2 | `<entity-type> <entity-id>` |
| `audit stats` | 0 | - |
| `backlog list` | 0 | - |
| `backlog show-next` | 1 | `[count]` |
| `stats` | 0 | - |
| `graph execute` | 0 | - |
| `graph serve` | 0 | - |
| `graph client` | 0 | - |
| `web` | 0 | - |

Three consequences of the table are worth stating, because each is a case a reader may expect to behave differently:

- **A maximum of zero is a contract, not an absence of one.** Every listing, statistics, and creation command that takes all of its input through flags accepts no positional argument at all, and refuses the first one it is given. `stats` and `graph execute` are in this class: their whole input is `-r` and, for `graph execute`, `--query` or standard input.
- **`graph execute` takes no positional query.** A Cypher query reaches it through `--query` or through standard input and never as a positional argument, so a bare query on the command line is an excess positional argument and is refused (`GRAPH.md § No Positional Query: A Stray Token Is Refused`).
- **An arity above one is real and is not a licence for more.** `sprint move-tasks`, `sprint move-to`, and `sprint swap` each take three positional arguments; `task stat`, `task prio`, and `task sev` each take two. The rule refuses what exceeds a command's own maximum, never everything after the first argument.

### Acceptance Criteria

1. `rmp roadmap create alpha-service beta-service` exits `2`, writes `Error: invalid input: unexpected argument "beta-service"` to stderr, and writes nothing to stdout. No roadmap is created: neither `alpha-service` nor `beta-service` exists afterwards, and `~/.roadmaps/` gains no directory and no database file.
2. For every command in `§ Positional Arity by Command`, an otherwise valid invocation carrying one more positional argument than the table publishes exits `2` and writes nothing to stdout, and the same invocation carrying exactly the published maximum succeeds unchanged.
3. Every command's declared maximum equals the number `§ Positional Arity by Command` publishes for it. A test that reads the declarations and compares them against this section fails when a command declares an arity the table does not state, and when the table names a command that declares none. The comparison covers the commands the registry holds. The six global forms named in `§ Declared Arity`, and `rmp` with no arguments, are outside the registry and are therefore outside this comparison; criterion 9 checks them at their own enforcement point.
4. A refused invocation performs no work: the target roadmap's task, sprint, and comment rows are identical before and after, the `audit` table gains no entry, and the graph store's snapshot and write-ahead log are unchanged on disk.
5. An invocation carrying both an excess positional argument and a value that would otherwise fail with exit code `6`, or a roadmap that would otherwise fail with exit code `4`, exits `2`.
6. The commands that already refused an excess positional argument are unchanged: the eight comment subcommands, `graph execute`, `rmp web`, and `rmp ai-help` produce the same exit code and the same stderr line as they did before this section was written.
7. An unresolved command or subcommand name accompanied by excess positional arguments still exits `127` and still writes its recovery help, so the arity rule never converts a dispatch failure into a misuse error.
8. No invocation that stays within its declared arity changes in any way: its stdout, its stderr, and its exit code are what they were.
9. Each of the six global forms refuses a trailing token. `rmp version check` and `rmp help sprint` each exit `2` and write `Error: invalid input: unexpected argument "check"` and `Error: invalid input: unexpected argument "sprint"` to stderr, and stdout stays empty: no version line and no help body. `rmp --version check`, `rmp -v check`, `rmp --help sprint`, and `rmp -h sprint` behave identically. Each of the six invoked on its own still exits `0` and still writes what it has always written.
10. `rmp backlog show-next 5 10 -r <name>` exits `2`, writes `Error: invalid input: unexpected argument "10"` to stderr, and writes no task list to stdout, while `rmp backlog show-next 5 -r <name>` returns its five tasks unchanged.

---

## Field Validation

### Task Field Constraints

The following fields have mandatory length constraints enforced by the application:

| Field | Required | Max Length | Description |
|-------|----------|------------|-------------|
| `title` | Yes | 255 chars | Task title/summary |
| `functional_requirements` | Yes | 4096 chars | Why: functional requirements |
| `technical_requirements` | Yes | 4096 chars | How: technical description |
| `acceptance_criteria` | Yes | 4096 chars | How to verify: completion criteria |
| `completion_summary` | No | 4096 chars | Summary of work done; only accepted on `task stat` when target status is `COMPLETED` |
| `commit_open` | Conditional | 64 chars (minimum 7) | Git commit hash the task was started from; mandatory on `task stat` when the target status is `DOING`, and rejected on every other target status |
| `commit_close` | Conditional | 64 chars (minimum 7) | Git commit hash the task was concluded at; mandatory on `task stat` when the target status is `COMPLETED`, and rejected on every other target status |

The two commit fields accept hexadecimal characters only, in any letter case, and
the application stores them lowercase. `MODELS.md § Task` (Commit Hash Constraint)
is canonical for the format; `task create` and `task edit` accept neither field.

### Comment Field Constraints

The comment subcommands of the `task` and `sprint` families (`comment-add`, `comment-list`, `comment-edit`, `comment-remove`) share the following field constraints:

| Field | Required | Max Length | Description |
|-------|----------|------------|-------------|
| `type` | Yes | - | Comment classification. Mandatory, no default. Task comments accept `FINDING`, `HYPOTHESIS`, `TEST`, `DECISION`, `PROGRESS`, `UPDATE`, `NOTE`; sprint comments accept `FINDING`, `DECISION`, `PROGRESS`, `UPDATE`. See `MODELS.md § Comment Type` for the canonical list |
| `body` | Yes | 4096 chars | Comment text. Supplied through `--body` or, when that flag is absent, read from standard input under the bounded read |

A `type` value outside the set the entity accepts is rejected with exit code 6 and a message naming the valid set for that entity. The `body` is subject to the Control-Character Constraint and the UTF-8 Encoding Constraint below.

### Comment Body Input Source and Precedence

The comment `body` is supplied either through the `--body` flag or on standard input. This is the same input mechanism `graph execute` uses for `--query` (see `GRAPH.md § Cypher Input Source and Precedence`); there is no `--body-file` flag and no path argument, so the commands open no file. The rules are:

1. When `--body` is present and its value is neither empty nor whitespace only, that value is the body and standard input is **not** read.
2. When `--body` is absent **and no other change was requested**, the body is read from standard input. The read is bounded and is not a read to EOF: see **Bounded standard-input read** below. On `comment-add` no other change is ever possible, so an absent `--body` always means "read standard input". On `comment-edit` the body is read from standard input only when `--type` is also absent; when `--type` is present and `--body` is absent, only the type changes and standard input is not read, so a type-only edit never blocks waiting for input.
3. When the body must come from standard input and standard input is empty, whitespace only, or not connected, the command fails with exit code 2. The message differs by subcommand, because the two subcommands are missing different things: on `comment-add` a body is mandatory and the message is "Error: required parameter missing: no comment body supplied"; on `comment-edit` the absent body means no change was requested at all, and the message is "Error: required parameter missing: at least one of --type or --body is required".
4. When `--body` is present but its value is empty, whitespace only, or missing (no following token, or the following token is itself a flag), the command fails with exit code 2 and the message "Error: required parameter missing: no comment body supplied", in both subcommands. The command does not silently fall back to standard input in this case.
5. Leading and trailing whitespace is trimmed before validation and before storage. Interior line breaks are preserved: a comment body is expected to be multi-line.

**Bounded standard-input read.** When the body comes from standard input, the command does NOT read the stream to EOF. It reads only until the outcome is already decided, and it never retains more than the 4096-character cap while doing so:

- While the body read so far still fits within the cap, reading continues.
- The moment the body cannot fit within the cap, the verdict is fixed, because no later byte can change it. The command stops reading and fails with exit code 6 and the message "Error: field exceeds maximum size: body exceeds maximum length of 4096 characters".
- Peak memory is therefore bounded by the cap and does not grow with the amount the writer sends.

This is a security property and not an implementation detail: an oversized body is refused without ever being buffered, so a producer that writes without limit cannot drive the command's memory. A producer still writing when the command exits observes the usual broken-pipe result. The verdict the user sees is exactly the verdict a read-to-EOF implementation would reach; only the reading and the memory profile differ.

The rule is identical on all four subcommands that accept a body on standard input: `task comment-add`, `task comment-edit`, `sprint comment-add`, and `sprint comment-edit`.

**Validation order.** `--type` is validated — for presence on `comment-add`, and for value in both subcommands — before the body is resolved, so a missing or invalid type fails immediately instead of leaving the command waiting on standard input for a body it would reject anyway.

### Comment Positional Argument Contract

Each of the eight comment subcommands takes **exactly one** positional argument, and that argument is always an id:

| Subcommand | Positional argument | What the id identifies |
|------------|---------------------|------------------------|
| `task comment-add` | `<task-id>` | The task the comment is added to |
| `task comment-list` | `<task-id>` | The task whose comments are listed |
| `task comment-edit` | `<comment-id>` | The comment itself, in `task_comments` |
| `task comment-remove` | `<comment-id>` | The comment itself, in `task_comments` |
| `sprint comment-add` | `<sprint-id>` | The sprint the comment is added to |
| `sprint comment-list` | `<sprint-id>` | The sprint whose comments are listed |
| `sprint comment-edit` | `<comment-id>` | The comment itself, in `sprint_comments` |
| `sprint comment-remove` | `<comment-id>` | The comment itself, in `sprint_comments` |

A declared maximum of one is what `§ Positional Arity by Command` publishes for all eight, and the CLI-wide rule in `§ Positional Arguments` refuses a second positional argument with exit code 2 and the line `Error: invalid input: unexpected argument "X"`. This section is canonical for what the one id identifies on each subcommand, and for the four points on which these subcommands need a rule of their own:

1. The positional id is required. An invocation that supplies none fails with exit code 2 and a message naming the id the subcommand expects, as each subcommand's own block below states.
2. A leftover token that begins with `-` is a flag and not a positional argument, so it is reported as an unknown flag — `Error: invalid input: unknown flag: --foo` — and not as an unexpected argument. This holds for every `-`-prefixed token, digits included: on these subcommands `-1` is an unknown flag, unlike `graph execute`, which does not classify a negative numeric token as a flag at all and refuses a stray `-1` as an unexpected argument (`GRAPH.md § No Positional Query: A Stray Token Is Refused`, rule 1). The value of `--body` is the one exception, and it is not a leftover token at all: `--body -1` supplies the body `-1`, under rule 4 of `Comment Body Input Source and Precedence` above.
3. The refusal lands at a defined point in the subcommand's own validation order: after the positional id has been parsed, before the `--type` value is validated, and before the body is resolved. An invocation carrying an extra positional argument is therefore refused with exit code 2 even when it also carries an invalid `--type` value, which on its own would be exit code 6, and it never leaves the command waiting on standard input for a body it is going to reject.
4. The whole "positive integer" constraint on the positional id is **exit code 2** on all eight subcommands, including the range half of it. Every other surface in the CLI reports an out-of-range id as a validation failure with exit code 6 (`§ Entity Identifier Range (All Positional Ids and --entity-id)`); these eight report it as misuse, because on them a malformed positional argument is a malformed argument list. The sentence is the shared one and only the sentinel differs:

| Subcommand group | Condition | Exit Code | stderr Output |
|------------------|-----------|-----------|---------------|
| `task comment-add`, `task comment-list` | `<task-id>` is an integer outside `1`-`2147483647` | 2 | `Error: invalid input: task_id must be between 1 and 2147483647, got N` |
| `sprint comment-add`, `sprint comment-list` | `<sprint-id>` is an integer outside `1`-`2147483647` | 2 | `Error: invalid input: sprint_id must be between 1 and 2147483647, got N` |
| `task comment-edit`, `task comment-remove`, `sprint comment-edit`, `sprint comment-remove` | `<comment-id>` is an integer outside `1`-`2147483647` | 2 | `Error: invalid input: comment_id must be between 1 and 2147483647, got N` |

   A token that is not an integer at all is the format rule and keeps the wording each subcommand's own block publishes for it, also with exit code 2. A value too large for the platform's integer type is refused by the range rule with the token echoed in place of a parsed value, because there is no parsed value to echo.

What the general rule already settles for these subcommands, and what this section therefore does not restate: only the first extra token is named; the position of the extra token on the command line does not matter; and nothing happens before the refusal — standard input is not read, the roadmap database is not opened, no comment is added, changed, deleted, or listed, and stdout stays empty.

**The other command that publishes this refusal.** `graph execute` refuses a stray positional argument under the same CLI-wide rule, with the same sentinel and the same exit code 2, and `GRAPH.md § No Positional Query: A Stray Token Is Refused` is canonical for it. The two are one rule with two published lines, and they differ on exactly two points, both of them deliberate:

- The `graph execute` line appends a hint that names the two sources of a Cypher query: `Error: invalid input: unexpected argument "X" (graph queries use --query or stdin)`. That hint is contractual on `graph execute` and is correctly absent here, because a comment body comes from `--body` or standard input and never from `--query` (`§ Positional Arguments`, "Commands that publish a different line").
- The two families classify a `-`-prefixed token differently, as rule 2 above states.

Neither section may be edited as though its wording were its own invention: a change to the shared part of the line is a change to both families, and a reader who finds one of these two sections must be able to reach the other from it.

### Validation Behavior

- **Whitespace trimming:** Leading and trailing whitespace is trimmed before storage, on every free-text field; interior whitespace is never altered. Three checks run ahead of that trim, in one order on every command: the field's **length cap**, then the **UTF-8** check, then the **control-character** check. The cap is measured on the **trimmed** value, which is the value the database stores, while the two content checks see the value **as supplied**; that asymmetry is deliberate, and it is why a value of exactly the maximum length carrying surrounding whitespace is accepted while a value carrying a leading VT is still refused. `UTF-8 Encoding Constraint (All Free-Text Fields)` below states that order in full, and `Emptiness Constraint (All Required Free-Text Fields)` below states the trim and the emptiness judgement that follows it
- **Empty strings:** Treated as missing for required fields, and so is a value that is empty only once trimmed. `Emptiness Constraint (All Required Free-Text Fields)` below states the exit code and the message each command emits
- **Error format:** Plain text to stderr with descriptive message
- **Exit code:** 6 for validation errors (see `ARCHITECTURE.md` — Exit Codes for canonical mapping)

### Published Field Names in Validation Messages

Each free-text field has exactly one **published name**. That name is what a
validation message uses to identify the field, on every command that writes the
field, and it does not vary with the flag through which the value reached the
application. A caller that matches on a field name in an error message therefore
matches one spelling per field, and can tell from the message alone which field
the refusal is about.

The published name is the lowercase, underscored name of the field, which is also
the name of the database column that stores it (see `DATABASE.md`). It is neither
the flag name nor the Go struct field name that `MODELS.md` declares. Flag names
are kebab-case and carry a leading `--`, and one flag does not even repeat the
words of the field it fills: `--summary` supplies `completion_summary`.
The two spellings differ deliberately, and the difference is not an
inconsistency: a flag is what the caller types on the command line, and a
published field name is what the application calls the field it stores.

| Entity | Published field name | Flag that supplies the value | Commands that write the field |
|--------|----------------------|------------------------------|-------------------------------|
| Task | `title` | `-t, --title` | `task create`, `task edit` |
| Task | `functional_requirements` | `-fr, --functional-requirements` | `task create`, `task edit` |
| Task | `technical_requirements` | `-tr, --technical-requirements` | `task create`, `task edit` |
| Task | `acceptance_criteria` | `-ac, --acceptance-criteria` | `task create`, `task edit` |
| Task | `completion_summary` | `-s, --summary` | `task stat`, when the target status is `COMPLETED` |
| Sprint | `title` | `-t, --title` | `sprint create`, `sprint update` |
| Sprint | `description` | `-d, --description` | `sprint create`, `sprint update` |
| Task comment and sprint comment | `body` | `-b, --body`, or standard input | `task comment-add`, `task comment-edit`, `sprint comment-add`, `sprint comment-edit` |

These eight fields are the free-text fields of Groadmap, the same set the
Free-Text Control-Character Constraint governs. `MODELS.md § Task`
(Free-Text Control-Character Constraint) is canonical for the set itself.

**Messages this rule governs.** The rule applies to every validation message that
names the field whose value broke a rule, whichever command emitted it:

1. The refusal of a value that carries a forbidden control character:
   `Error: validation error: <field>: control characters are not allowed`.
2. The refusal of a value longer than the field's maximum:
   `Error: field exceeds maximum size: <field> exceeds maximum length of N characters`.
3. The refusal of an empty value for a field that requires one, which
   `Emptiness Constraint (All Required Free-Text Fields)` below states in full and
   each subcommand restates in its own section; this rule fixes the field name inside
   it and nothing else about it.
4. Every rule added later over the same fields. A later rule may state its message
   with `<field>` and leave the name to be resolved here; it MUST NOT restate the
   mapping.

In each of them, `<field>` is the published name from the table above.

**Messages this rule does not govern.** A message whose subject is a **flag**
rather than a field keeps the flag's own spelling, kebab-case with its leading
`--`. `Error: required parameter missing: --functional-requirements` names the
flag the command line did not carry, and is correct as it stands.

The criterion that separates the two cases is what the message identifies:

- The subject is the **field** when a value for it reached the application and
  that value broke a rule about its content: too long, empty after trimming, or
  carrying a forbidden code point. The message names the field by its published
  name.
- The subject is the **flag** when no value reached the application at all,
  because the flag is absent, unknown, or not accepted where it was used. The
  message names the flag.

One command emits both kinds about one field without contradicting itself:
`task create` reports the absence of `--functional-requirements` as a missing
flag, and reports a supplied value carrying a control character as a violation of
`functional_requirements`.

**One definition, not one literal per call site.** Every command MUST obtain the
published name from a single shared definition that maps each field to its
published name. No command may spell a field name inline in the message it builds.
The defect this requirement prevents is not a typo but the absence of that
definition: when each call site chooses its own literal, two of them eventually
choose differently for the same field, and one command then names one field two
ways in two of its own messages. A single definition makes a second spelling
impossible to introduce by accident rather than merely wrong. This specification
does not prescribe the definition's Go shape; it requires that exactly one exists,
that it covers the eight fields above, and that every message naming a field takes
the name from it.

**Precedence.** The message templates above are quoted to show where the field name
appears in each. This section is canonical for that name alone, and not for the rest
of a message's wording, which the subcommand's own section states. Some of those
sections quote a message in a prose form that differs from the line the application
writes, including in how the field is spelled; where such a quotation and this
section disagree about a field's name, this section is the canonical one.

**Acceptance criteria:**

1. Triggering one validation rule on one field from every command that writes that
   field produces the same field name in every resulting message, and that name is
   the one this section publishes.
2. Within a single command, every message that names a given field names it
   identically, whatever rule the value broke.
3. `task create` names `functional_requirements`, `technical_requirements`, and
   `acceptance_criteria` exactly as `task edit` does. No validation message names
   a field in kebab-case.
4. `task edit` names the field, and not the flag, when it refuses an empty value
   for a field that requires one: an empty `--functional-requirements` is refused
   as `functional_requirements`.
5. A message about a missing, unknown, or misplaced flag still names the flag,
   with its hyphens and its leading `--`. `task create` invoked without
   `--functional-requirements` still reports `--functional-requirements`.
6. Every field name in a validation message comes from the shared definition. A
   test fails when a command builds a validation message from an inline field-name
   literal, or from a name the definition does not contain.
7. The rule changes no message in which the field name already is the published
   name.

### Entity Identifier Range (All Positional Ids and `--entity-id`)

Every id the CLI accepts — a task id, a sprint id, a comment id, the dependency
positional of `task add-dep` / `task remove-dep`, and the `--entity-id` filter of
`audit list` — MUST be an integer in the inclusive range `1`-`2147483647`
(`MaxInt32`).

An id is subject to **two rules**, and they are separate. A token that is not an
integer at all breaks the **format** rule; an integer that falls outside the
range breaks the **range** rule. The two produce different messages and, on every
surface, different exit codes.

| Rule | Condition | Exit Code | stderr Output |
|------|-----------|-----------|---------------|
| Format | The token is not an integer | 2 | `Error: invalid input: invalid <entity> ID: "X" (must be a positive integer)` |
| Range | The integer is `< 1` or `> 2147483647` | 6, or 2 on the eight comment subcommands | `Error: <sentinel>: <field> must be between 1 and 2147483647, got N` |

`<entity>` is `task`, `sprint`, `comment`, `dependency task`, or `entity`, and
`<field>` is that same word with `_id` appended and any space written as an
underscore: `task_id`, `sprint_id`, `comment_id`, `dependency_task_id`,
`entity_id`. The two spellings name the same argument by construction.

**The range rule has one sentence.** It is worded by the same shared definition
that words the `priority`, `severity` and `--limit` ranges (see
`§ Published Field Names in Validation Messages` and `§ List Tasks`): the field is
named without the flag's `--` prefix, the bounds are both stated, and the
offending value is echoed after a comma. A refusal never states only the bound
that was crossed, because the two bounds are one rule and the remedy for either
is the same. In particular, `audit list --entity-id <n>` and the second
positional of `audit history` address the identical field and therefore print the
identical line.

**The failure class is a property of the surface, not of the rule.** A range
refusal is a validation failure with exit code 6 everywhere except the eight
comment subcommands, which classify the whole "positive integer" constraint on
their positional id as misuse with exit code 2 (see
`§ Comment Positional Argument Contract`). That difference is deliberate and is
the only difference between the two: the sentence is the same on both sides.

**Acceptance criteria:**

1. Every surface that refuses an out-of-range id prints one sentence, differing
   only in the field named, in the value echoed, and in the sentinel that carries
   the exit code.
2. No refusal states one bound of the range without the other.
3. The format refusal and the range refusal name the same argument.

### Control-Character Constraint (All Free-Text Fields)

All free-text fields — task `title`, `functional_requirements`,
`technical_requirements`, `acceptance_criteria`, `completion_summary`, sprint
`title` and `description`, and the comment `body` — reject control characters. An
input that contains any of the following is rejected with exit code 6 before it is
stored:

- ASCII control bytes below `0x20`, except TAB (`0x09`), LF (`0x0A`), and CR
  (`0x0D`), which are permitted.
- DEL (`0x7F`).
- Unicode bidirectional and format control code points `U+200E`, `U+200F`,
  `U+202A`-`U+202E`, `U+2066`-`U+2069`, and `U+FEFF`.

This guards against terminal escape-sequence injection (CWE-150) and Trojan Source
attacks (CVE-2021-42574). The canonical definition is the Free-Text
Control-Character Constraint in `MODELS.md § Task`. The refusal names the field by
its published name, as `Published Field Names in Validation Messages` above
requires.

### UTF-8 Encoding Constraint (All Free-Text Fields)

The free-text fields the Control-Character Constraint above lists accept only text
encoded as UTF-8. An input whose bytes are not a well-formed UTF-8 sequence is
rejected with exit code 6 before it is stored, on every command that writes the
field, and whether the value arrived as the value of a flag or on standard input.
The canonical definition is the Free-Text UTF-8 Encoding Constraint in
`MODELS.md § Task`, which states what counts as well-formed, what the rule protects,
and why the application refuses such a value instead of substituting a replacement
character for each invalid byte.

The refusal is `Error: validation error: <field>: the value is not valid UTF-8`. It
names the field by its published name, as `Published Field Names in Validation
Messages` above requires.

**Order.** Every free-text value is validated in one order, and that order does not vary
by command, by field, or by the way the value reached the application. The order is: the
field's **length cap**, then the **UTF-8 encoding** check, then the **control-character**
check, then the trim, and last the emptiness judgement that
`Emptiness Constraint (All Required Free-Text Fields)` below governs. The encoding check
runs immediately before the control-character check, and nothing falls between them,
because the control-character rule is defined over decoded code points and is only
meaningful once the bytes are known to decode.

The order holds on all seven write paths — `task create`, `task edit`, `task stat` (the
`--summary` value), `sprint create`, `sprint update`, the two `comment-add` subcommands,
and the two `comment-edit` subcommands — for all eight free-text fields, and whether the
value arrived as the value of a flag or on standard input. A value that breaks more than
one rule is refused for the earliest rule in that order, so two consequences are
universal. A value that is at once over the cap and not valid UTF-8 is refused as
`field exceeds maximum size` and never as an encoding failure. A value that is at once
over the cap and carrying a forbidden control character is refused as
`field exceeds maximum size` and never as a control-character failure. Neither refusal
depends on which command was invoked or on which field carried the value.

**Why the cap answers first.** The comment `body` is the one free-text value that can
arrive on standard input, and the command reads that stream under a bounded read: the
read fixes the length verdict the moment the content cannot fit and stops reading, which
is what keeps an oversized body from ever being buffered
(see `Comment Body Input Source and Precedence` above). That section also requires the
verdict the caller sees to be the verdict a read-to-EOF implementation would reach. A
reader cannot judge the encoding of bytes it has refused to read, so an order that put
the encoding check first would force that path either to buffer whatever a writer chose
to send or to reach a different verdict. Cap-first is therefore the only order all seven
write paths can share, and the other write paths were moved onto the comment path's
order rather than the reverse.

**The length verdict is well defined on bytes that do not decode.** Refusing an
over-long value that is also malformed UTF-8 for its length is sound, and not an accident
of the order. A field's length is counted in Unicode code points, and that count is
defined on malformed input too: each byte that decodes to no valid rune counts as one, so
the count is never lower than the count SQLite's `length()` function returns for the same
stored value, and the cap is answerable on a value whose encoding has not been
established. The trim the measurement runs on is equally safe there, because it removes
only whitespace code points and no byte that fails to decode is one, so the trim can
neither introduce nor remove an encoding failure. `MODELS.md § Task` (Free-Text UTF-8
Encoding Constraint) is canonical for both facts.

Four commands state a **Validation Order** below that restates this sequence for their
own free-text step: `task stat`, `task comment-add`, `task comment-edit`, and
`sprint comment-add`. Each of them restates this one rule and never a local variant; what
such a block adds is where the free-text step falls among that command's other checks,
which is per-command information this section does not carry. The commands that state no
Validation Order block — `task create`, `task edit`, `sprint create`, and
`sprint update` — need none, because the order above is unconditional and governs them in
full.

**Acceptance criteria:**

1. On each of the seven write paths, and for each free-text field that path writes, a
   value that is at once longer than the field's cap and not valid UTF-8 is refused with
   `Error: field exceeds maximum size: <field> exceeds maximum length of N characters`
   and never with `Error: validation error: <field>: the value is not valid UTF-8`. Both
   refusals carry exit code 6, so the exit code alone establishes nothing about the
   order: the criterion is which message reaches stderr.
2. On the same paths and fields, a value that is at once longer than the cap and carrying
   a forbidden control character is refused with the same
   `field exceeds maximum size` message and never with
   `Error: validation error: <field>: control characters are not allowed`. Here too both
   refusals exit 6 and the exit code distinguishes nothing.
3. A value of exactly the field's maximum length that carries a forbidden control
   character is refused as a control-character violation, with
   `Error: validation error: <field>: control characters are not allowed`. Reaching the
   cap is therefore never a way past the content rules: the cap answers only for a value
   that exceeds it.
4. A value of exactly the field's maximum length that is not valid UTF-8 is refused for
   its encoding, with `Error: validation error: <field>: the value is not valid UTF-8`.
5. A value within the cap that is at once not valid UTF-8 and carrying a forbidden
   control character is refused for its encoding, not for the control character.
6. The four comment subcommands reach criteria 1 to 5 on the standard-input path exactly
   as they reach them through `--body`, and an oversized body is refused there without
   the whole value being read, as `Comment Body Input Source and Precedence` above
   requires.
7. Criteria 1 to 6 are checked on every write path and every field that path writes. A
   check made on one command and one field alone would pass while another command
   disagreed, and that divergence is what these criteria exist to exclude.
8. The end-to-end suite exercises criteria 1 to 7 against the compiled binary.

### Emptiness Constraint (All Required Free-Text Fields)

A free-text field that is required to be non-empty is judged **after** the value has
been trimmed. A value made only of whitespace leaves nothing behind once leading and
trailing whitespace is removed, so it counts as absent and the command refuses it,
stores nothing, and changes no entity. The canonical definition is the Free-Text
Emptiness and Trimming Constraint in `MODELS.md § Task`, which also states the
criterion's rationale and the order it must run in; this section states the refusal
each command emits.

Seven of the eight free-text fields are required to be non-empty and are governed
here: task `title`, `functional_requirements`, `technical_requirements`, and
`acceptance_criteria`; sprint `title` and `description`; and the comment `body`. The
eighth, `completion_summary`, is optional, so no value of it is ever refused for being
empty; `task stat` accepts a transition to `COMPLETED` that carries no `--summary` at
all.

**The refusal.** Every command that writes one of the seven fields judges emptiness by
the identical criterion. The refusal differs in one place only, and that difference is
a published rule that predates this constraint:

| Field | Commands that write it | Exit code | Message on stderr |
|-------|------------------------|-----------|-------------------|
| `title`, `functional_requirements`, `technical_requirements`, `acceptance_criteria` | `task create`, `task edit` | 6 | `Error: validation error: <field> cannot be empty` |
| `title`, `description` | `sprint create`, `sprint update` | 6 | `Error: validation error: <field> cannot be empty` |
| `body` | `task comment-add`, `task comment-edit`, `sprint comment-add`, `sprint comment-edit` | 2 | `Error: required parameter missing: no comment body supplied`, except on a `comment-edit` that requested no other change, where it is `Error: required parameter missing: at least one of --type or --body is required` |

`<field>` is the field's published name, which
`Published Field Names in Validation Messages` above is canonical for; this section
does not restate that mapping. The comment `body` keeps exit code 2 and its own
wording because a body is a required parameter that may arrive on standard input, and
a body that is empty after trimming is the same condition as a body that never
arrived: `Comment Body Input Source and Precedence` above states that rule, and this
constraint leaves it exactly as it stands.

**A value that names nothing is not the same as no value at all.** Two questions are
answered in order, and only the second one belongs to this constraint:

1. **Was the flag supplied with any text at all?** On `task create` and
   `sprint create` the free-text flags are required parameters, and a required flag
   that is absent, or that carries the literal empty string, counts as not supplied.
   The command fails with exit code 2 and names the **flag**:
   `Error: required parameter missing: --title`. `Create Task` and `Create Sprint`
   below state that rule, it predates this constraint, and this constraint does not
   change it. On `task edit` and `sprint update` the same flags are optional, so this
   question does not arise there: a supplied flag is a supplied flag whatever it
   carries, and its value goes straight to the second question.
2. **Is the supplied value empty once trimmed?** A value that carries text as supplied
   has reached the application and is validated as a value. When trimming leaves
   nothing of it, the command refuses it as stated in the table above, naming the
   **field**.

A whitespace-only value falls on the second side of that boundary on every command,
including the two create commands: the caller did supply text, and the text turns out
to name nothing. `rmp sprint create -r <name> -t '   ' -d 'A real macro goal.'`
therefore fails with exit code 6 and creates no sprint, while
`rmp sprint create -r <name> -t '' -d 'A real macro goal.'` continues to fail with
exit code 2 under question 1.

**What is stored is the trimmed value.** This is a separate statement from the rule
above and is not derived from it: judging emptiness after a trim would still be
possible while storing the bytes as supplied. The application does not do that. It
removes leading and trailing whitespace before the value reaches the database, on all
eight free-text fields and on every command that writes one, `completion_summary`
included, as `Validation Behavior` above states. One consequence is required: a
field's maximum length is measured on the trimmed value, the same value the
database stores, so a value of exactly the maximum length carrying surrounding
whitespace is accepted. Interior whitespace is never altered.

**This constraint does not move the control-character check.** The encoding check and
the control-character check run on the value **as supplied**, before any trimming; the
emptiness check runs on the **trimmed** value, after it. Both facts hold at once, and
the reason the first must not be simplified into the second is that VT (`0x0B`) and FF
(`0x0C`) are forbidden control characters that the trim also removes, so trimming first
would let a leading or trailing VT or FF through with the character silently discarded
(CWE-150). `MODELS.md § Task` (Free-Text Emptiness and Trimming Constraint) states the
full sequence and is canonical for it. The visible consequence on every command is that
a value consisting solely of VT is refused as
`Error: validation error: <field>: control characters are not allowed` and never as an
empty value.

This constraint fixes where the emptiness check falls relative to the trim, and where
the trim falls relative to the encoding and control-character checks. It moves no other
check. Where a field's maximum length is checked is fixed once and for every command,
as `UTF-8 Encoding Constraint (All Free-Text Fields)` above states: the cap answers
first, ahead of both content checks. Its position relative to the emptiness judgement
changes no verdict
in either direction, because a value that trims to nothing is zero characters long, so no
input exists that both checks could answer for.

**Acceptance criteria:**

1. `rmp sprint create -r <name> -t '   ' -d 'A real macro goal.'` exits 6, writes
   `Error: validation error: title cannot be empty` to stderr, writes nothing to
   stdout, and creates no sprint.
2. `rmp sprint create -r <name> -t 'A real title' -d '   '` exits 6 with
   `Error: validation error: description cannot be empty` and creates no sprint.
3. `rmp sprint update -r <name> <id> -t '   '` and
   `rmp sprint update -r <name> <id> -d '   '` each exit 6 with the corresponding
   message, leave the stored value unchanged, and write no audit entry.
4. `rmp task create` with any one of `-t`, `-fr`, `-tr`, or `-ac` carrying a
   whitespace-only value exits 6 and names the field, not the flag, and creates no
   task. The same invocation with the flag omitted still exits 2 and names the flag.
5. `rmp task edit <id> -t '   '` exits 6 with
   `Error: validation error: title cannot be empty`, unchanged from its behaviour
   before this constraint, and the same holds for `-fr`, `-tr`, and `-ac`.
6. Each of the four comment subcommands refuses a whitespace-only body with exit code
   2, on the `--body` path and on the standard-input path alike. The wording is the
   one `Comment Body Input Source and Precedence` above already fixes for a body that
   never arrived: `Error: required parameter missing: no comment body supplied` in
   every case except a `comment-edit` whose body was to come from standard input and
   which supplies no `--type` either, where the refusal remains
   `Error: required parameter missing: at least one of --type or --body is required`.
7. A whitespace-only value is refused whichever whitespace it is made of. Spaces, TAB,
   LF, CR, and any mixture of them are refused, and so is a value made only of no-break
   spaces (`U+00A0`) or of NEL (`U+0085`): the criterion is what the trim leaves
   behind, not which whitespace character the caller supplied.
8. On every command that writes a free-text field, a value carrying surrounding
   whitespace and a non-empty core is accepted and read back trimmed, and a value of
   exactly the field's maximum length carrying surrounding whitespace is accepted.
9. A value carrying a leading or trailing VT or FF is refused as a control-character
   violation on every command that writes a free-text field, and a value consisting
   solely of VT is refused as a control-character violation rather than as an empty
   value.
10. The end-to-end suite exercises criteria 1 to 9 against the compiled binary.

### Validation Error Messages

| Scenario | Exit Code | Error Message (stderr) |
|----------|-----------|------------------------|
| Task or sprint `title` exceeds 255 characters | 6 | "Error: field exceeds maximum size: title exceeds maximum length of 255 characters" |
| A required free-text value is empty, or empty once trimmed, on `task edit` or `sprint update`, and on `task create` or `sprint create` when the flag carries text | 6 | "Error: validation error: <field> cannot be empty" |
| A required free-text flag is absent, or carries the literal empty string, on `task create` or `sprint create` | 2 | "Error: required parameter missing: --<flag>" |
| A task requirement field exceeds 4096 characters | 6 | "Error: field exceeds maximum size: <field> exceeds maximum length of 4096 characters" |
| Sprint `description` exceeds 2048 characters | 6 | "Error: field exceeds maximum size: description exceeds maximum length of 2048 characters" |
| `completion_summary` exceeds 4096 characters | 6 | "Error: field exceeds maximum size: completion_summary exceeds maximum length of 4096 characters" |
| Comment body exceeds 4096 chars | 6 | "Error: field exceeds maximum size: body exceeds maximum length of 4096 characters" |
| Free-text value is not valid UTF-8 | 6 | "Error: validation error: <field>: the value is not valid UTF-8" |
| Free-text value carries a forbidden control character | 6 | "Error: validation error: <field>: control characters are not allowed" |
| Comment body not supplied | 2 | "Error: required parameter missing: no comment body supplied" |
| Comment edit requests no change (no `--type`, no `--body`, no body on stdin) | 2 | "Error: required parameter missing: at least one of --type or --body is required" |
| Comment type missing | 2 | "Error: required parameter missing: --type" |
| Comment type invalid on a task | 6 | "Error: validation error: invalid comment type \"X\" for a task comment; valid types: FINDING, HYPOTHESIS, TEST, DECISION, PROGRESS, UPDATE, NOTE" |
| Comment type invalid on a sprint | 6 | "Error: validation error: invalid comment type \"X\" for a sprint comment; valid types: FINDING, DECISION, PROGRESS, UPDATE" |

Every length cap in this table is reported by the same message, whose shape is fixed by `Published Field Names in Validation Messages` above: the sentinel `field exceeds maximum size: `, the field's published name, and the cap that field carries. The cap differs by field; the wording does not.

### Roadmap Name Validation

All roadmap names must conform to the following validation rules:

| Rule | Value | Description |
|------|-------|-------------|
| Regex | `^[a-z0-9_-]+$` | Only lowercase letters, numbers, underscores, and hyphens |
| Maximum length | 50 characters | Ensures filesystem compatibility |
| Minimum length | 1 character | Name cannot be empty |

**Validation Error Messages:**

| Scenario | Exit Code | Error Message (stderr) |
|----------|-----------|------------------------|
| Invalid characters | 6 | "Error: Roadmap name must only contain lowercase letters, numbers, underscores, and hyphens" |
| Exceeds 50 characters | 6 | "Error: Roadmap name must not exceed 50 characters (got N)" |
| Empty name | 6 | "Error: Roadmap name is required" |
| Name starts with a hyphen | 6 | "Error: validation error: roadmap name cannot start with '-'" |
| Name is a reserved system name | 6 | "Error: validation error: \"X\": roadmap name is a reserved system name" |

Three of these five messages carry no sentinel between the `Error: ` prefix and the text: the roadmap-name checks that predate the sentinel catalogue construct their message directly, and the binary prints it as shown. The other two carry `validation error: `. All five exit 6.

---

## Global Commands

### Help

```bash
rmp --help
rmp -h
```

**Description:** Displays general help with available commands in **plain text**. This is also the default behavior when no command is provided.

A third form is the bare word: `rmp help` writes the same help body to stdout and exits `0`, identically to the two flag forms. The word `help` is not an entry in the command registry; like the flag forms, it is resolved before any command lookup, so `rmp help <command>` does not reach that command's help. The form that reaches it is `rmp <command> --help` (`HELP.md § Help structure template`).

All three forms accept no positional argument. `rmp help <command>` is therefore refused, with the exit code and the error line `§ Positional Arguments` publishes.

### Version

```bash
rmp --version
rmp -v
```

**Description:** Displays application version.

A third form is the bare word: `rmp version` writes the same single line to stdout and exits `0`, identically to the two flag forms. The binary has always accepted it, and it is a documented form of this command, on the same footing as the bare word `help` above. The word `version` is not an entry in the command registry; like the flag forms, it is resolved before any command lookup, so there is no `rmp version <subcommand>`.

All three forms accept no positional argument. An excess one is refused with the exit code and the error line `§ Positional Arguments` publishes.

### AI Help

```bash
rmp --ai-help
rmp ai-help
```

**Description:** Emits a machine-readable JSON contract that fully describes the CLI surface (commands, subcommands, flags, exit codes, output shapes, enums, examples). The output is intended to be consumed by AI agents and other automated callers without recourse to any other documentation.

**Forms:**

| Invocation | Scope of the returned contract |
|------------|--------------------------------|
| `rmp --ai-help` | Whole CLI: every command and every subcommand. |
| `rmp ai-help` | Whole CLI: identical payload to `rmp --ai-help`. |
| `rmp <command> --ai-help` | One command and all of its subcommands. |
| `rmp <command> <subcommand> --ai-help` | One subcommand only. |

**Rules:**

- The flag `--ai-help` is a global flag. It is recognised at every level of the command tree and is parsed before any other validation runs (analogous to `--help`).
- The flag `--ai-help` has no short form.
- The command `rmp ai-help` is functionally equivalent to `rmp --ai-help`. It exists so the contract is discoverable through plain command listings and shell tab-completion.
- The command `ai-help` accepts no positional arguments and no flags other than `--help`. Any other argument fails with exit code 2 and the message `Error: ai-help accepts no positional arguments or flags other than --help`, whether the offending token is a positional argument or an unrecognised flag.
- When both `--ai-help` and any other action-bearing flag or argument are present, `--ai-help` wins: the contract is emitted and no other action is performed.
- `--ai-help` and `ai-help` ignore the `-r` / `--roadmap` flag; the contract is a static description of the CLI and does not touch any roadmap database.

**Output (stdout JSON):** the contract document defined in `DATA_FORMATS.md § AI Agent Contract`. The JSON is pretty-printed with two-space indentation, UTF-8, and includes a final newline.

**Exit codes:**

| Code | Cause |
|------|-------|
| 0 | Contract emitted successfully. |
| 2 | `ai-help` invoked with unexpected positional arguments or flags; `--ai-help` used with an unknown command or subcommand name preceding it. |

An unknown command or subcommand name preceding `--ai-help` exits `2`, not the `127` specified for a dispatch failure in `§ Dispatch Failures (Unresolved Command or Subcommand Names)`, and no help follows the error. The name is a scope selector for the contract emitter rather than a name being dispatched, so an unusable selector is an invalid argument to `--ai-help`. See `ARCHITECTURE.md § Failure modes`.

**Discoverability requirements:**

1. The first line of the plain-text output of `rmp --help` and of every family-level and subcommand-level `--help` is the banner:

   ```
   AI agents: run `rmp --ai-help` for a machine-readable command contract.
   ```

   The banner is followed by one blank line, then the existing help body. The banner is **not** printed by `rmp version` / `rmp --version` / `rmp -v` (version output is parsed by scripts; extra lines would break automations) and is **not** printed by the AI contract emitters (`rmp --ai-help`, `rmp ai-help`, `rmp <command> --ai-help`, `rmp <command> <subcommand> --ai-help`), which emit JSON only.

2. Every error message emitted to stderr by the CLI ends with one blank line followed by the hint:

   ```
   AI agents: run `rmp --ai-help` for a machine-readable command contract.
   ```

   This rule applies uniformly to input errors (missing flags, unresolved subcommands), validation errors, not-found errors, conflict errors, and database errors. On a dispatch failure the hint stays last, after the help written per `§ Dispatch Failures (Unresolved Command or Subcommand Names)`, so the hint remains the final line of stderr on every error path. The hint is one line, plain text, written to stderr, and does not change the exit code. The hint is not appended when the command itself is `rmp --ai-help`, `rmp ai-help`, `rmp <command> --ai-help`, or `rmp <command> <subcommand> --ai-help` (to avoid recursion in error paths of the contract emitter). The hint is also not appended when `AI_AGENT=1` is active for this invocation; in that case the env-var hint already occupies the top of stderr and the trailing hint is suppressed to avoid duplication (see rule 3 below).

3. When the environment variable `AI_AGENT` is set to the literal value `1`, every invocation of `rmp` writes the same hint line to stderr **before** any other output, regardless of whether the invocation succeeds or fails:

   ```
   AI agents: run `rmp --ai-help` for a machine-readable command contract.
   ```

   The hint:
   - Is the **first line** written to stderr, followed by exactly one blank line, followed by any remaining stderr content (an `Error:` line on failure, otherwise nothing).
   - Is written exactly once per invocation. When `AI_AGENT=1` is active and the invocation fails, the trailing error-path hint specified in rule 2 is suppressed so the agent observes the hint exactly once.
   - Does not change stdout in any way.
   - Does not change the exit code.
   - Is suppressed for the invocations `rmp --ai-help`, `rmp ai-help`, `rmp <command> --ai-help`, and `rmp <command> <subcommand> --ai-help` (the agent is already using the contract).
   - Any value of `AI_AGENT` other than the exact string `1` (including empty, `0`, `true`, `false`, or unset) disables the hint.

   The canonical specification of ordering and deduplication is in `HELP.md § AI_AGENT environment variable`.

---

## AI Agent Contract

The structure, fields, and example payload of the JSON document returned by `rmp --ai-help` are specified in `DATA_FORMATS.md § AI Agent Contract`. The contract is generated by the CLI at runtime from its internal command registry; the registry is the single source of truth from which both the human help text and the AI contract are derived. See `ARCHITECTURE.md § AI Agent Contract Generation`.

---

## Exit Codes

Groadmap follows standard Unix exit code conventions. Success results in exit code `0`. Errors use specific codes (1-127) and are documented in detail in [ARCHITECTURE.md](./ARCHITECTURE.md#exit-codes).

---

## Roadmap Selection (Always Required)

All commands that operate on a roadmap require the `-r <name>` or `--roadmap <name>` flag explicitly.

**There is no default roadmap mechanism.** Omitting the flag always produces an error:

```
Error: no roadmap selected: use -r <name> or --roadmap <name>
```

This applies to every subcommand under `task`, `sprint`, `backlog`, `audit`, `stats`, and `graph`.

The `web` command is deliberately **not** in this list. `rmp web` operates across all roadmaps: the web interface lists every roadmap found under `~/.roadmaps/` and the user selects one in the browser, so `rmp web` does not require and does not accept the `-r` / `--roadmap` flag (see [Web Interface](#web-interface)).

```bash
# Always provide -r:
rmp task list -r myproject
rmp sprint create -r myproject -t "Sprint 1" -d "Deliver the first working persistence layer for roadmaps."
rmp stats -r myproject
```

The `-r` / `--roadmap` flag may appear anywhere among the arguments after the subcommand; the parser extracts it before processing the remaining flags.

---

## Roadmap Management

Command: `rmp roadmap` (alias: `rmp road`)

A roadmap is stored in its own home directory at `~/.roadmaps/<name>/`, with the SQLite database at `~/.roadmaps/<name>/project.db`. On every `rmp` invocation, a startup sweep automatically migrates any roadmap still in the legacy `~/.roadmaps/<name>.db` layout into the current layout before the command runs, so `roadmap list` and all other commands always observe the current layout. The sweep is specified in `ARCHITECTURE.md § Filesystem Layout Migration`.

### List Roadmaps

```bash
rmp roadmap list
rmp road ls
```

**Description:** Lists all existing roadmaps. Each roadmap is the immediate subdirectory of `~/.roadmaps/` that contains a `project.db` database.

**JSON Output:** Array of objects, each with `name` (the roadmap home directory name), `path` (the absolute path to the roadmap's `project.db`), and `size` (the size of `project.db` in bytes).
```json
[
  {"name": "project1", "path": "~/.roadmaps/project1/project.db", "size": 24576},
  {"name": "project2", "path": "~/.roadmaps/project2/project.db", "size": 8192}
]
```

### Create Roadmap

```bash
rmp roadmap create <name>
rmp road new <name>
```

**Description:** Creates a new roadmap. The command creates the roadmap home directory `~/.roadmaps/<name>/` (mode `0700`) and the SQLite database `~/.roadmaps/<name>/project.db` (mode `0600`) inside it.

`roadmap create` accepts no flags beyond `--help`. It does not provide a `--force` or overwrite option; the operation is intentionally non-destructive. To replace an existing roadmap, the caller MUST run `rmp roadmap remove <name>` first.

**Name Validation:**

| Rule | Value | Description |
|------|-------|-------------|
| Regex | `^[a-z0-9_-]+$` | Only lowercase letters, numbers, underscores, and hyphens |
| Maximum length | 50 characters | Ensures filesystem compatibility |
| Minimum length | 1 character | Name cannot be empty |

**Error Cases:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Invalid characters | 6 | "Error: Roadmap name must only contain lowercase letters, numbers, underscores, and hyphens" |
| Exceeds 50 characters | 6 | "Error: Roadmap name must not exceed 50 characters (got N)" |
| Empty name | 6 | "Error: Roadmap name is required" |
| Name starts with a hyphen | 6 | "Error: validation error: roadmap name cannot start with '-'" |
| Name is a reserved system name | 6 | "Error: validation error: \"X\": roadmap name is a reserved system name" |
| Roadmap already exists | 5 | "Error: resource already exists: roadmap \"X\" already exists" |

**Output (success):** `{"name": "project1"}`, exit code 0.

### Remove Roadmap

```bash
rmp roadmap remove <name>
rmp road rm <name>
```

**Description:** Removes a roadmap by deleting its entire home directory `~/.roadmaps/<name>/` recursively. This removes the `project.db` database, its SQLite sidecars (`project.db-wal`, `project.db-shm`), and any other per-roadmap files the directory contains.

**Output (success):** No output, exit code 0.

---

## Task Management

Command: `rmp task` (alias: `rmp t`)

### List Tasks

```bash
rmp task list --roadmap <name>
rmp task ls -r <name> [OPTIONS]
```

**Options:**
- `-s, --status <state>` - Filter by status (BACKLOG, SPRINT, DOING, TESTING, COMPLETED)
- `-p, --priority <n>` - Filter priority >= n (0-9)
- `--severity <n>` - Filter severity >= n (0-9)
- `-l, --limit <n>` - Limit number of results (default: 100)
- `-y, --type <TYPE>` - Filter by task type. See `MODELS.md` — Task Type for the canonical list of 10 valid values.
- `--created-since <date>` - Return tasks created on or after this date (RFC3339 or YYYY-MM-DD)
- `--created-until <date>` - Return tasks created on or before this date (RFC3339 or YYYY-MM-DD)
- `--sort <field>` - Sort order: `priority` (default), `created`, `status`, `severity`

**Default Ordering:** Tasks are returned ordered by `priority DESC, created_at ASC`. Higher-priority tasks appear first; equal-priority tasks are ordered by creation date (oldest first).

**Sort Field Ordering:**
| `--sort` value | ORDER BY |
|----------------|----------|
| `priority` (default) | `priority DESC, created_at ASC` |
| `created` | `created_at ASC` |
| `status` | `status ASC, priority DESC, created_at ASC` |
| `severity` | `severity DESC, priority DESC, created_at ASC` |

**Error Conditions:**
| Input | Exit Code | stderr |
|-------|-----------|--------|
| `--limit` `< 1` or `> 100` | 6 | `Error: validation error: limit must be between 1 and 100, got N` |
| Invalid `--type` value | 6 | `Error: validation error: invalid task type: "X"` |
| Invalid `--sort` value | 6 | `Error: validation error: --sort must be one of: priority, created, status, severity` |
| Invalid `--created-since` format | 6 | `Error: validation error: --created-since: invalid date format: expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01): "X"` |
| Invalid `--created-until` format | 6 | `Error: validation error: --created-until: invalid date format: expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01): "X"` |

The `--limit` refusal is worded by the same rule that words it on
`rmp backlog list` and `rmp audit list` (see `§ List Backlog Tasks` and
`§ List Audit Log`): one sentence, naming the value and not the flag, echoing
the value that was refused. The three differ in the maximum alone, because the
audit log's cap is genuinely a different number.

**JSON Output:** Array of Task objects.

### Create Task

```bash
rmp task create --roadmap <name> --title <title> --functional-requirements <fr> --technical-requirements <tr> --acceptance-criteria <ac> [OPTIONS]
rmp task new -r <name> -t <title> -fr <fr> -tr <tr> -ac <ac>
```

**Options:**
- `-t, --title <text>` - Task title (required), maximum 255 characters
- `-fr, --functional-requirements <text>` - Functional requirements (required), maximum 4096 characters
- `-tr, --technical-requirements <text>` - Technical requirements (required), maximum 4096 characters
- `-ac, --acceptance-criteria <text>` - Acceptance criteria (required), maximum 4096 characters
- `-y, --type <type>` - Task type (default: `TASK`). See `MODELS.md` — Task Type for the canonical list of 10 valid values.
- `-p, --priority <0-9>` - Priority (default: 0)
- `--severity <0-9>` - Severity (default: 0)
- `--parent <id>` - Parent task ID; creates this task as a sub-task of the given parent. The parent must exist.

**Validation Rules:**

Each required free-text field fails in three distinct ways, and the binary prints a different line for each, so each has its own row and its own exit code. The `Field` column names the field by its published name, as `Published Field Names in Validation Messages` above requires; the flag that carries it is the kebab-case name listed under **Options** above.

| Field | Condition | Exit Code | Error Message (stderr) |
|-------|-----------|-----------|------------------------|
| `title` | Flag absent, or carries the literal empty string | 2 | "Error: required parameter missing: --title" |
| `title` | Empty once trimmed | 6 | "Error: validation error: title cannot be empty" |
| `title` | Exceeds 255 characters | 6 | "Error: field exceeds maximum size: title exceeds maximum length of 255 characters" |
| `functional_requirements` | Flag absent, or carries the literal empty string | 2 | "Error: required parameter missing: --functional-requirements" |
| `functional_requirements` | Empty once trimmed | 6 | "Error: validation error: functional_requirements cannot be empty" |
| `functional_requirements` | Exceeds 4096 characters | 6 | "Error: field exceeds maximum size: functional_requirements exceeds maximum length of 4096 characters" |
| `technical_requirements` | Flag absent, or carries the literal empty string | 2 | "Error: required parameter missing: --technical-requirements" |
| `technical_requirements` | Empty once trimmed | 6 | "Error: validation error: technical_requirements cannot be empty" |
| `technical_requirements` | Exceeds 4096 characters | 6 | "Error: field exceeds maximum size: technical_requirements exceeds maximum length of 4096 characters" |
| `acceptance_criteria` | Flag absent, or carries the literal empty string | 2 | "Error: required parameter missing: --acceptance-criteria" |
| `acceptance_criteria` | Empty once trimmed | 6 | "Error: validation error: acceptance_criteria cannot be empty" |
| `acceptance_criteria` | Exceeds 4096 characters | 6 | "Error: field exceeds maximum size: acceptance_criteria exceeds maximum length of 4096 characters" |
| `type` | Not one of the 10 valid values | 6 | "Error: validation error: invalid task type: \"X\"" |
| `priority` | Outside 0-9 | 6 | "Error: validation error: priority must be between 0 and 9, got N" |
| `severity` | Outside 0-9 | 6 | "Error: validation error: severity must be between 0 and 9, got N" |
| `parent_task_id` | `--parent` names a task that does not exist | 4 | "Error: resource not found: parent task N not found" |

The two range refusals above have one wording each, whichever command applied the rule. `task create`, `task edit`, `task prio` and `task sev` all print the line shown here, with the offending value after `got`: the wording states the rule that was broken, and the rule belongs to the field rather than to the command that happened to check it. `Change Priority (prio)`, `Change Severity (sev)` and `Edit Task` below publish this same line, not one of their own.

**Empty and whitespace-only values.** `--title`, `--functional-requirements`,
`--technical-requirements`, and `--acceptance-criteria` are required parameters, and
two different failures follow from a value that carries no usable text. An invocation
that omits one of the four flags, or supplies it with the literal empty string, fails
with exit code 2 and names the **flag**:
`Error: required parameter missing: --title`. An invocation that supplies one of them
with text that is empty once trimmed — a value made only of spaces, for example —
fails with exit code 6 and names the **field**:
`Error: validation error: title cannot be empty`. No task is created in either case,
and stdout stays empty. `task create` applies exactly the criterion `task edit`
applies; `Emptiness Constraint (All Required Free-Text Fields)` under `Field
Validation` above is canonical for it, and
`Published Field Names in Validation Messages` above is canonical for the field name
inside the second message.

**Output (success):** `{"id": 42}`, exit code 0.

**Error Output:** Validation errors written to stderr with exit code 6.

**Exit Codes:** The command emits `0`, `2`, `3`, `4`, or `6`:
| Exit Code | Condition |
|-----------|-----------|
| 0 | Task created |
| 2 | A required flag is missing, or carries the literal empty string |
| 3 | Roadmap not specified |
| 4 | `--parent` points to a task that does not exist |
| 6 | Validation error (a required free-text value that is empty once trimmed, oversize field, invalid enum/range, invalid type) |

There is no exit code 5 for this command: a missing `--parent` target is a not-found condition (exit 4), not an already-exists condition.

**Audit:** One `TASK_CREATE` entry against the created task (`entity_type = TASK`,
`entity_id` = the new id), written in the same transaction as the insert.
`related_entity_id` and `commit_hash` are NULL. Creating a sub-task with `--parent`
writes no additional entry and no entry against the parent.

### Get Task(s)

```bash
rmp task get --roadmap <name> <id1,id2>
```

**Description:** Retrieves one or more tasks by ID. Multiple IDs must be comma-separated without spaces.

**Batch Operation Behavior (Fail-Fast):**

All batch operations validate ALL IDs before executing any destructive operation. This ensures atomicity and prevents partial updates.

| Scenario | Exit Code | Behavior | stderr Output |
|----------|-----------|----------|---------------|
| All IDs valid | 0 | Returns all tasks as JSON array | None |
| Some IDs do not exist | 4 | **No operation performed**, returns error | "Error: resource not found: some tasks not found" |
| All IDs do not exist | 4 | **No operation performed**, returns error | "Error: resource not found: some tasks not found" |
| An ID is not an integer | 2 | **No operation performed** | "Error: invalid input: invalid task ID: \"X\" (must be a positive integer)" |
| An ID is an integer outside `1`-`2147483647` | 6 | **No operation performed** | "Error: validation error: task_id must be between 1 and 2147483647, got N" |

The message does not name which IDs were missing, and it is the same whether one ID or every ID was missing: the command reports that the batch could not be satisfied, not which member failed. A caller that needs to know which ID is absent queries the IDs one at a time.

**Validation Order:**
1. Parse all IDs and validate format (must be positive integers)
2. Verify all IDs exist in the roadmap
3. Only after full validation succeeds, execute the operation
4. If any validation fails, exit immediately with exit code 4 (or 2 for format errors)

**Rationale:** Prevents partial state changes. If a batch update fails halfway through, the database would be in an inconsistent state. Fail-fast ensures either all operations succeed or none do.

**JSON Output:** Array of Task objects.

### Get Next Tasks (next)

```bash
rmp task next [num]
rmp t next [num]
```

**Description:** Returns the next N open tasks from the currently open sprint. Tasks are returned in the order defined by the sprint's `task_order` (set via `sprint reorder` or other ordering commands).

**The order is total, and the command publishes it as a guarantee.** Within one sprint no two tasks share a `position` — the schema enforces it (`DATABASE.md § Position Uniqueness Within a Sprint`) — and this command reads a single sprint, so ordering on `position` alone already places every task at exactly one rank. Repeating the same call over unchanged data returns the same tasks in the same sequence. `priority` does **not** order this listing and cannot promote a task above another: the planned order is the answer to "what do I do next", and a task's priority is what the plan was built from, not a second chance to override it.

**Arguments:**
- `num` (optional) - Number of tasks to return. If not provided, defaults to 1.

**JSON Output:** Array of Task objects.

**Output Examples:**

Success (tasks available):
```json
[
  {
    "id": 42,
    "title": "Implement user authentication",
    "functional_requirements": "Users must be able to authenticate securely",
    "technical_requirements": "Create login endpoint with JWT tokens",
    "acceptance_criteria": "Users can log in with valid credentials; tokens expire after 24h",
    "priority": 9,
    "severity": 9,
    "status": "SPRINT",
    "sprint_id": 5,
    "created_at": "2026-03-15T10:30:00.000Z",
    "started_at": null,
    "tested_at": null,
    "closed_at": null
  }
]
```

Success (fewer tasks than requested):
```json
[
  {
    "id": 42,
    "title": "Implement user authentication",
    "functional_requirements": "Users must be able to authenticate securely",
    "technical_requirements": "Create login endpoint with JWT tokens",
    "acceptance_criteria": "Users can log in with valid credentials; tokens expire after 24h",
    "priority": 9,
    "severity": 9,
    "status": "SPRINT",
    "sprint_id": 5,
    "created_at": "2026-03-15T10:30:00.000Z",
    "started_at": null,
    "tested_at": null,
    "closed_at": null
  },
  {
    "id": 43,
    "title": "Add input validation",
    "functional_requirements": "Prevent invalid data from entering the system",
    "technical_requirements": "Validate all user inputs using validator library",
    "acceptance_criteria": "All inputs validated; proper error messages returned",
    "priority": 8,
    "severity": 9,
    "status": "SPRINT",
    "sprint_id": 5,
    "created_at": "2026-03-15T11:00:00.000Z",
    "started_at": null,
    "tested_at": null,
    "closed_at": null
  }
]
```

Success (no open tasks in sprint):
```json
[]
```

**Error Cases:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| No sprint is currently open | 4 | "Error: resource not found: no sprint is currently open. Use 'rmp sprint start <id>' to open a sprint first" |
| Invalid num argument (non-numeric or < 1) | 6 | "Error: validation error: num must be a positive integer" |
| Roadmap not specified | 3 | "Error: no roadmap selected: use -r <name> or --roadmap <name>" |

**Behavior Notes:**
- Only returns tasks with status `SPRINT`, `DOING`, or `TESTING` (open tasks)
- Tasks are returned in the order defined by the sprint's `task_order` (set via sprint reorder or other ordering commands)
- If the requested number exceeds available open tasks, all remaining open tasks are returned
- If no open sprint exists, an error is returned indicating a sprint needs to be opened
- If the sprint has no open tasks, an empty array is returned (success, exit code 0)

### Change Status (stat)

```bash
rmp task stat -r <name> <ids> <state>
rmp task set-status -r <name> <ids> <state>

# Transitioning to DOING: --commit-open is mandatory
rmp task stat -r <name> <ids> DOING --commit-open 5f93b51
rmp task stat -r <name> <ids> DOING -co 5f93b51

# Transitioning to COMPLETED: --commit-close is mandatory
rmp task stat -r <name> <ids> COMPLETED --commit-close 2578d18
rmp task stat -r <name> <ids> COMPLETED -cc 2578d18

# With optional completion summary (only valid when transitioning to COMPLETED)
rmp task stat -r <name> <ids> COMPLETED --commit-close 2578d18 --summary "Brief description of what was done"
rmp task stat -r <name> <ids> COMPLETED -cc 2578d18 -s "Brief description of what was done"
```

**Description:** Updates the status of one or more tasks (bulk supported).

**Flags:**

| Flag | Short | Type | Description |
|------|-------|------|-------------|
| `--summary` | `-s` | string | Optional completion summary. Only accepted when target state is `COMPLETED`. Maximum 4096 characters. |
| `--commit-open` | `-co` | string | Git commit hash the task is started from. **Mandatory** when target state is `DOING`, and rejected for every other target state. 7 to 64 hexadecimal characters; stored lowercase. |
| `--commit-close` | `-cc` | string | Git commit hash the task is concluded at. **Mandatory** when target state is `COMPLETED`, and rejected for every other target state. 7 to 64 hexadecimal characters; stored lowercase. |

**Commit Tracking Behavior:**

The two commit flags are the only way a task's `commit_open` and `commit_close`
values are ever written. Groadmap never derives them: it invokes no git command,
reads no working directory, and inspects no repository. The caller supplies the
hash, and the application validates its format alone — it does not check that the
hash names a commit that exists anywhere. `MODELS.md § Task` (Commit Hash
Constraint) is canonical for the format.

- **`--commit-open` is mandatory on every transition into `DOING`.** Both such
  transitions require it: `SPRINT → DOING`, the first entry into `DOING`, and
  `TESTING → DOING`, a re-entry after testing sent the work back. On a re-entry the
  supplied value **replaces** the value stored previously; the command keeps no
  history of earlier values.
- **`--commit-close` is mandatory on the `TESTING → COMPLETED` transition,** the
  only transition into `COMPLETED`.
- **Each flag is rejected on any other target state.** `--commit-open` on a target
  other than `DOING`, and `--commit-close` on a target other than `COMPLETED`, are
  rejected with exit code 6 and no changes made. This mirrors the rule that governs
  `--summary`, which is accepted only when the target is `COMPLETED`.
- **One hash applies to the whole batch.** When several IDs are given, every task in
  the batch receives the same supplied hash, exactly as every task in a batch
  receives the same `--summary`. A caller who needs different hashes for different
  tasks issues separate commands.
- **`commit_open` survives a return to `BACKLOG`; `commit_close` does not.** See
  **Transitioning to BACKLOG** below and `STATE_MACHINE.md § Commit Tracking
  Fields`.
- **Neither field is editable.** `task create` accepts neither flag, because a task
  is created in `BACKLOG`, and `task edit` cannot change either value. A wrong hash
  is corrected by performing the transition again where the state machine allows it.

**Batch Operation Behavior (Fail-Fast):**

All batch operations validate ALL IDs and status transitions before applying any changes. This ensures atomicity - either all tasks are updated or none are.

| Scenario | Exit Code | Behavior | stderr Output |
|----------|-----------|----------|---------------|
| All IDs valid | 0 | All tasks updated | None |
| Some or all IDs do not exist | 4 | **No changes made** | "Error: resource not found: some tasks not found" |
| An ID is not an integer | 2 | **No changes made** | "Error: invalid input: invalid task ID: \"X\" (must be a positive integer)" |
| An ID is an integer outside `1`-`2147483647` | 6 | **No changes made** | "Error: validation error: task_id must be between 1 and 2147483647, got N" |
| Target state is not a recognised status | 6 | **No changes made** | "Error: validation error: invalid task status: \"X\"" |
| Invalid status transition | 6 | **No changes made** | "Error: validation error: invalid status transition from X to Y for task N" |
| Target state is `SPRINT` | 6 | **No changes made** | "Error: validation error: status SPRINT can only be set automatically via 'sprint add-tasks'" |
| `--summary` used with non-COMPLETED state | 6 | **No changes made** | "Error: validation error: --summary is only valid when transitioning to COMPLETED" |
| `--summary` exceeds 4096 characters | 6 | **No changes made** | "Error: field exceeds maximum size: completion_summary exceeds maximum length of 4096 characters" |
| `--summary` value is not valid UTF-8 | 6 | **No changes made** | "Error: validation error: completion_summary: the value is not valid UTF-8" |
| `--commit-open` used with non-DOING state | 6 | **No changes made** | "Error: --commit-open flag is only allowed when transitioning to DOING" |
| `--commit-close` used with non-COMPLETED state | 6 | **No changes made** | "Error: --commit-close flag is only allowed when transitioning to COMPLETED" |
| Target state is `DOING` and `--commit-open` is absent | 6 | **No changes made** | "Error: --commit-open is required when transitioning to DOING" |
| Target state is `COMPLETED` and `--commit-close` is absent | 6 | **No changes made** | "Error: --commit-close is required when transitioning to COMPLETED" |
| `--commit-open` value is not a valid commit hash | 6 | **No changes made** | "Error: invalid commit hash for --commit-open: \"X\" (expected 7 to 64 hexadecimal characters)" |
| `--commit-close` value is not a valid commit hash | 6 | **No changes made** | "Error: invalid commit hash for --commit-close: \"X\" (expected 7 to 64 hexadecimal characters)" |
| `--commit-open` written with no value after it | 2 | **No changes made** | "Error: --commit-open requires a value" |
| `--commit-close` written with no value after it | 2 | **No changes made** | "Error: --commit-close requires a value" |

**Validation Order:**

The order below is normative. Steps 1 to 4 need no database and MUST run before
it is opened; steps 5 to 7 read the database but write nothing; step 8 is the only
step that writes. A command rejected at any step therefore makes no change to any
task, including the other tasks of a multi-ID invocation whose IDs were valid.

1. Parse all IDs and validate format (must be positive integers)
2. Validate the target state: reject an unrecognised state, and reject the state `SPRINT`, which only `sprint add-tasks` may set
3. Validate `--summary`: reject if the target state is not `COMPLETED`; when a value is provided, validate its length, then its encoding, then its control characters
4. Validate the commit flags against the target state, in this order:
   1. Reject `--commit-open` if it is present and the target state is not `DOING`
   2. Reject `--commit-close` if it is present and the target state is not `COMPLETED`
   3. Reject if the target state is `DOING` and `--commit-open` is absent
   4. Reject if the target state is `COMPLETED` and `--commit-close` is absent
   5. Validate the format of the commit flag the target state requires, and normalise the accepted value to lowercase. The four checks above leave at most one commit flag in play, so no ordering between the two is needed here
5. Verify all IDs exist in the roadmap
6. Validate status transition for each task against state machine rules
7. When the target state is `COMPLETED`, apply the sub-task hierarchy guard and then the dependency guard (see `STATE_MACHINE.md § Sub-task Hierarchy Guard` and `STATE_MACHINE.md § Dependency Guard`)
8. Only after full validation succeeds, update all tasks and audit log, in a single transaction
9. If any validation fails, exit immediately without making changes

**Completion Summary Behavior:**
- `--summary` is optional even when transitioning to `COMPLETED`
- When provided, `completion_summary` is stored on each updated task
- When transitioning to BACKLOG, `completion_summary` is cleared to NULL
- `--summary` has no effect on non-COMPLETED transitions and is rejected with an error

**Transitioning to BACKLOG:** The target state `BACKLOG` is accepted from `SPRINT` and from `COMPLETED`, and rejected with exit code 6 from `DOING` and `TESTING`. The transition clears `started_at`, `tested_at`, `closed_at`, `completion_summary`, and `commit_close` to NULL, and **preserves `commit_open`**. The asymmetry is deliberate: the commit the work was started from remains a true historical fact after the task returns to the backlog, whereas the commit it was concluded at is invalidated by the reopening. The transition does not touch the `sprint_tasks` table: a task that was a member of a sprint stays a member, at the same `position`, while its status reads `BACKLOG`. Use `task reopen` to return a `DOING` or `TESTING` task to `BACKLOG`, and `sprint remove-tasks` to detach a task from its sprint. See `STATE_MACHINE.md § Valid Transitions`, `STATE_MACHINE.md § Commit Tracking Fields`, and `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status`.

**Output (success):** No output, exit code 0.

**Audit:** One entry per task in the batch, named for the state the task entered:

| Target state | Operation written | `commit_hash` |
|--------------|-------------------|---------------|
| `BACKLOG` | `TASK_STATUS_BACKLOG` | NULL |
| `DOING` | `TASK_STATUS_DOING` | the `--commit-open` value, normalised to lowercase |
| `TESTING` | `TASK_STATUS_TESTING` | NULL |
| `COMPLETED` | `TASK_STATUS_COMPLETED` | the `--commit-close` value, normalised to lowercase |

`SPRINT` is not reachable through this command, so `task stat` never writes
`TASK_STATUS_SPRINT`. Every entry carries `entity_type = TASK` with the task's own
id and a **NULL `related_entity_id`**, including a transition to `BACKLOG`: no second
entity is party to a `task stat` invocation, so under the governing rule
(`DATABASE.md § The Two Entities of a Relational Operation`) there is no counterpart
to name. The same `TASK_STATUS_BACKLOG` operation written by `sprint remove-tasks`
does name a sprint, because there the sprint is the counterpart. All entries of one
invocation share one `performed_at` value and are written in the same transaction as
the status updates, so a rejected command writes no entry at all.

The operation names the **destination** state, not the source: a reader learns from
the operation value alone which state the task entered, and never has to correlate
the entry with the task's current status. `task stat` writes no
`TASK_STATUS_CHANGE` entry; that operation is LEGACY (see
`DATABASE.md § audit Table`).

**Acceptance criteria:**

1. `rmp task stat -r <name> <a>,<b> TESTING` writes exactly two entries, `TASK_STATUS_TESTING` against `<a>` and against `<b>`, sharing one `performed_at`.
2. `rmp task stat -r <name> <id> DOING --commit-open 5F93B51` writes one `TASK_STATUS_DOING` entry whose `commit_hash` is `5f93b51`.
3. A batch rejected at any validation step writes zero audit entries.
4. No invocation of `task stat` writes `TASK_STATUS_CHANGE` or `TASK_STATUS_SPRINT`.

### Change Priority (prio)

```bash
rmp task prio -r <name> <ids> <priority>
rmp task set-priority -r <name> <ids> <priority>
```

**Batch Operation Behavior (Fail-Fast):**

Validates all IDs before updating any priorities. Follows same validation order as `task stat`.

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| All IDs valid | 0 | None |
| Some IDs invalid | 4 | "Error: resource not found: some tasks not found" |
| Priority out of range (0-9) | 6 | "Error: validation error: priority must be between 0 and 9, got N" |

**Output (success):** No output, exit code 0.

**Audit:** One `TASK_PRIORITY_CHANGE` entry per task in the batch, against the task,
with NULL `related_entity_id` and NULL `commit_hash`. `task edit -p <n>` writes the
same operation (see `Edit Task` below), so the priority of a task has one audit
operation whichever command changed it.

### Change Severity (sev)

```bash
rmp task sev -r <name> <ids> <severity>
rmp task set-severity -r <name> <ids> <severity>
```

**Batch Operation Behavior (Fail-Fast):**

Validates all IDs before updating any severities. Follows same validation order as `task stat`.

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| All IDs valid | 0 | None |
| Some IDs invalid | 4 | "Error: resource not found: some tasks not found" |
| Severity out of range (0-9) | 6 | "Error: validation error: severity must be between 0 and 9, got N" |

**Output (success):** No output, exit code 0.

**Audit:** One `TASK_SEVERITY_CHANGE` entry per task in the batch, against the task,
with NULL `related_entity_id` and NULL `commit_hash`. `task edit --severity <n>`
writes the same operation.

### Edit Task

```bash
rmp task edit --roadmap <name> <id> [OPTIONS]
```

**Description:** Edits an existing task's properties. Only specified fields are updated.

**Options:**
- `-t, --title <text>` - Maximum 255 characters
- `-fr, --functional-requirements <text>` - Maximum 4096 characters
- `-tr, --technical-requirements <text>` - Maximum 4096 characters
- `-ac, --acceptance-criteria <text>` - Maximum 4096 characters
- `-y, --type <type>` - Task type. See `MODELS.md` — Task Type for the canonical list of 10 valid values.
- `-p, --priority <0-9>`
- `--severity <0-9>`

**Validation Rules:**

When a field is specified, it is validated before updating:

An absent flag means "do not change this field", so no field of `task edit` is ever
missing: a supplied flag carrying the literal empty string is a supplied value, and it
is invalid. This is the one place `task edit` and `task create` differ, and it is why
`task edit -t ""` names the field and exits 6 where `task create -t ""` names the flag
and exits 2. The `Field` column below names the field by its published name, as
`Published Field Names in Validation Messages` above requires; the flag that carries it
is the kebab-case name listed under **Options** above.

| Field | Condition | Error Message (stderr) | Exit Code |
|-------|-----------|------------------------|-----------|
| `title` | Empty, or empty once trimmed | "Error: validation error: title cannot be empty" | 6 |
| `title` | Exceeds 255 characters | "Error: field exceeds maximum size: title exceeds maximum length of 255 characters" | 6 |
| `functional_requirements` | Empty, or empty once trimmed | "Error: validation error: functional_requirements cannot be empty" | 6 |
| `functional_requirements` | Exceeds 4096 characters | "Error: field exceeds maximum size: functional_requirements exceeds maximum length of 4096 characters" | 6 |
| `technical_requirements` | Empty, or empty once trimmed | "Error: validation error: technical_requirements cannot be empty" | 6 |
| `technical_requirements` | Exceeds 4096 characters | "Error: field exceeds maximum size: technical_requirements exceeds maximum length of 4096 characters" | 6 |
| `acceptance_criteria` | Empty, or empty once trimmed | "Error: validation error: acceptance_criteria cannot be empty" | 6 |
| `acceptance_criteria` | Exceeds 4096 characters | "Error: field exceeds maximum size: acceptance_criteria exceeds maximum length of 4096 characters" | 6 |
| `priority` | Integer outside 0-9 | "Error: validation error: priority must be between 0 and 9, got N" | 6 |
| `priority` | Not an integer | "Error: invalid input: invalid value for --priority: strconv.Atoi: parsing \"X\": invalid syntax" | 2 |
| `severity` | Integer outside 0-9 | "Error: validation error: severity must be between 0 and 9, got N" | 6 |
| `severity` | Not an integer | "Error: invalid input: invalid value for --severity: strconv.Atoi: parsing \"X\": invalid syntax" | 2 |
| `type` | Not one of the 10 valid values | "Error: validation error: invalid task type: \"X\"" | 6 |

**Validation Behavior:**
- **Whitespace trimming:** Leading and trailing whitespace is trimmed before validation and before storage, and the stored value is the trimmed one. The UTF-8 and control-character checks run on the value as supplied, ahead of the trim (see `Emptiness Constraint (All Required Free-Text Fields)` under `Field Validation` above)
- **Empty strings:** Setting a required field to a value that is empty, or that is empty once trimmed, fails validation with exit code 6 and names the field
- **Partial updates:** Only specified fields are validated and updated
- **Type validation:** Non-integer values for priority/severity fail with exit code 2 (malformed input)
- **No-op:** If no fields are specified, command succeeds with no changes (exit code 0)

**Output (success):** No output, exit code 0.

**Error Output:** Validation errors written to stderr with exit code 6.

**Audit:** One entry per field the invocation supplies. An invocation that supplies
N fields writes N entries; an invocation that supplies none writes none.

| Flag supplied | Operation written |
|---------------|-------------------|
| `-t, --title` | `TASK_TITLE_CHANGE` |
| `-fr, --functional-requirements` | `TASK_FUNCTIONAL_REQUIREMENTS_CHANGE` |
| `-tr, --technical-requirements` | `TASK_TECHNICAL_REQUIREMENTS_CHANGE` |
| `-ac, --acceptance-criteria` | `TASK_ACCEPTANCE_CRITERIA_CHANGE` |
| `-y, --type` | `TASK_TYPE_CHANGE` |
| `-p, --priority` | `TASK_PRIORITY_CHANGE` |
| `--severity` | `TASK_SEVERITY_CHANGE` |

Every entry carries `entity_type = TASK` with the edited task's id, a NULL
`related_entity_id`, and a NULL `commit_hash`. All entries of one invocation share
one `performed_at` value and are written in the same transaction as the `UPDATE`, so
a rejected edit writes no entry at all and a committed edit is never recorded for
only some of the fields it changed.

**The trigger is the presence of the flag, not a difference in value.** The command
compares no supplied value against the value already stored. **An invocation that
supplies a flag whose value equals the stored value still writes that field's entry**:
`rmp task edit -r <name> <id> -t "<the title it already has>"` writes a
`TASK_TITLE_CHANGE` entry. This is deliberate, and it is what makes `task edit`
consistent with the dedicated setter commands: `task prio` and `task sev` already
write their entry unconditionally, without comparing against the stored value, so
`task edit -p` behaves the same way as `task prio`. Making one path conditional and
the other unconditional would mean the same audit operation had two different
triggers depending on which command produced it. The rule also keeps the audit log a
record of the commands issued rather than of the deltas they happened to produce, and
keeps the entry count derivable from the command line alone.

**`task edit` writes no `TASK_UPDATE` entry.** That operation is LEGACY (see
`DATABASE.md § audit Table`). This removes a former inconsistency: `task prio 5` and
`task edit -p 5` now write the same operation, `TASK_PRIORITY_CHANGE`, so filtering
the audit log by that operation finds every priority change however it was made.

**Acceptance criteria:**

1. `rmp task edit -r <name> <id> -t "New" -p 3` writes exactly two entries, one `TASK_TITLE_CHANGE` and one `TASK_PRIORITY_CHANGE`, sharing one `performed_at`.
2. `rmp task edit -r <name> <id>` with no field flags writes zero entries and exits 0.
3. `rmp task edit -r <name> <id> -y BUG` writes one `TASK_TYPE_CHANGE` entry and no `TASK_UPDATE` entry.
4. An edit rejected by any validation rule writes zero entries.
5. `rmp audit list -r <name> --operation TASK_PRIORITY_CHANGE` returns the priority changes made through `task prio` and those made through `task edit -p` alike.

### Remove Task

```bash
rmp task remove -r <name> <ids>
rmp task rm -r <name> <ids>
```

**Description:** Removes one or more tasks by ID (bulk supported).

**Batch Operation Behavior (Fail-Fast):**

All batch operations validate ALL IDs before removing any tasks. This is especially critical for destructive operations to prevent accidental partial deletion.

| Scenario | Exit Code | Behavior | stderr Output |
|----------|-----------|----------|---------------|
| All IDs valid | 0 | All tasks removed | None |
| Some IDs invalid | 4 | **No tasks removed** | "Error: resource not found: some tasks not found" |
| All IDs invalid | 4 | **No tasks removed** | "Error: resource not found: some tasks not found" |
| Invalid ID format | 2 | **No tasks removed** | "Error: invalid input: invalid task ID: \"X\" (must be a positive integer)" |

**Validation Order:**
1. Parse all IDs and validate format (must be positive integers)
2. Verify all IDs exist in the roadmap
3. Only after full validation succeeds, remove all tasks in a single transaction
4. If any validation fails, exit immediately without removing any tasks

**Rationale:** Prevents accidental partial deletion. If IDs are mistyped, no data is lost.

**Output (success):** No output, exit code 0.

**Constraint:** Tasks must be in `BACKLOG` status to be removed. Attempting to delete a task in any other status returns an error.

| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Task in SPRINT, DOING, TESTING, or COMPLETED | 6 | `Error: validation error: task #N cannot be deleted — status is X, must be BACKLOG` |
| Batch with any non-BACKLOG task | 6 | Entire batch rejected, no tasks deleted |
| Task has subtasks | 6 | `Error: validation error: task #N cannot be deleted — it has M subtask(s); remove them first` |

**Audit:** One `TASK_DELETE` entry per task removed, against the task, in the same
transaction as the deletion. The entry outlives the task: the row it names is gone,
and the audit log keeps the record that it existed and was deleted. Deleting a task
deletes its `task_dependencies` rows by cascade and writes no `TASK_REMOVE_DEP`
entry for them.

---

### List Subtasks

```bash
rmp task subtasks -r <name> <id>
```

**Description:** Returns all direct subtasks of a given task, ordered by priority descending, then created_at ascending.

**Arguments:**
- `id` - Parent task ID (required, positive integer)

**JSON Output:** Array of Task objects. Empty array if the task has no subtasks.

**Error Conditions:**
| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Task not found | 4 | `Error: resource not found: task N` |
| Invalid ID format | 2 | `Error: invalid input: invalid task ID: "X" (must be a positive integer)` |

---

### Add Task Dependency

```bash
rmp task add-dep -r <name> <task-id> <dep-id>
```

**Description:** Marks `<task-id>` as depending on `<dep-id>`. The task cannot be marked COMPLETED until `<dep-id>` is COMPLETED. Circular dependencies are rejected.

**Arguments:**
- `task-id` - ID of the dependent task (required, positive integer)
- `dep-id` - ID of the task it depends on (required, positive integer)

**Constraints:**
- A task cannot depend on itself
- Circular dependency detection: if adding A→B would create a cycle (B already transitively depends on A), the operation is rejected
- Adding an already-existing dependency is a no-op (idempotent)

**JSON Output:** No stdout output on success, exit code 0.

**Error Conditions:**
| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Task not found | 4 | `Error: task #N not found: resource not found: task N` |
| Self-dependency | 6 | `Error: validation error: task cannot depend on itself` |
| Circular dependency | 6 | `Error: validation error: adding dependency would create a circular dependency between task #N and task #M` |
| Missing arguments | 2 | `Error: required parameter missing: task ID and dependency ID required` |

The first row is the one message in the file that carries its sentinel in the middle rather than directly after `Error: `: the command wraps the lookup failure in its own context, so the reader sees `task #N not found: ` first and the `resource not found: ` sentinel after it. The exit code still follows the sentinel, and it is 4.

**Audit:** Two `TASK_ADD_DEP` entries, one against each task of the pair, written in
the same transaction as the insert:

| Entry | `entity_id` | `related_entity_id` |
|-------|-------------|---------------------|
| 1 | `<task-id>` | `<dep-id>` |
| 2 | `<dep-id>` | `<task-id>` |

Both share one `performed_at`. Naming the counterpart is what makes an entry state
*which* dependency it concerns: reading either task's history shows the other task of
the pair, and two entries written by two different invocations are distinguishable.

Adding an already-existing dependency is a no-op (see Constraints above) and writes
no entry.

---

### Remove Task Dependency

```bash
rmp task remove-dep -r <name> <task-id> <dep-id>
```

**Description:** Removes the dependency of `<task-id>` on `<dep-id>`.

**Arguments:**
- `task-id` - ID of the dependent task (required, positive integer)
- `dep-id` - ID of the task it depends on (required, positive integer)

**JSON Output:** No stdout output on success, exit code 0.

**Error Conditions:**
| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Dependency not found | 4 | `Error: resource not found: dependency from task #N to task #M not found` |
| Missing arguments | 2 | `Error: required parameter missing: task ID and dependency ID required` |

**Audit:** Two `TASK_REMOVE_DEP` entries, one against each task of the pair, with the
same `entity_id` / `related_entity_id` arrangement `task add-dep` uses above, written
in the same transaction as the delete and sharing one `performed_at`.

---

### List Task Blockers

```bash
rmp task blockers -r <name> <id>
```

**Description:** Returns tasks that are blocking `<id>` — tasks that `<id>` depends on and that are NOT yet COMPLETED.

**Arguments:**
- `id` - Task ID (required, positive integer)

**JSON Output:** Array of Task objects. Empty array if no blockers.

**Error Conditions:**
| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Task not found | 4 | `Error: resource not found: task N` |
| Invalid ID format | 2 | `Error: invalid input: invalid task ID: "X" (must be a positive integer)` |

---

### List Tasks Being Blocked

```bash
rmp task blocking -r <name> <id>
```

**Description:** Returns tasks that `<id>` is blocking — tasks that depend on `<id>`.

**Arguments:**
- `id` - Task ID (required, positive integer)

**JSON Output:** Array of Task objects. Empty array if this task is not blocking anything.

**Error Conditions:**
| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Task not found | 4 | `Error: resource not found: task N` |
| Invalid ID format | 2 | `Error: invalid input: invalid task ID: "X" (must be a positive integer)` |

---

### Reopen Task

```bash
rmp task reopen -r <name> <ids>
```

**Description:** Returns one or more tasks to `BACKLOG` status, clearing all lifecycle timestamps (`started_at`, `tested_at`, `closed_at`), the `completion_summary`, and `commit_close`. It **preserves `commit_open`**, which records where the work originally started and stays true after the task returns to the backlog. Accepts comma-separated IDs for bulk operations.

**Valid source states:** `SPRINT`, `DOING`, `TESTING`, `COMPLETED` — any non-BACKLOG state.

**Effect on sprint membership:** The command removes the task's `sprint_tasks` row only when the source state is `SPRINT`, `DOING`, or `TESTING`. From the `COMPLETED` source state it leaves the row in place, so the task keeps its sprint membership and its `position` while its status reads `BACKLOG`. See `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status`.

**Batch Operation Behavior (Fail-Fast):**

All IDs are validated before any transitions are applied. If any ID is invalid, the entire batch is rejected and no tasks are modified.

| Scenario | Exit Code | Behavior | Output |
|----------|-----------|----------|--------|
| Task transitions to BACKLOG from `SPRINT`, `DOING`, or `TESTING` | 0 | Timestamps, `completion_summary`, and `commit_close` cleared; `commit_open` preserved; `sprint_tasks` row removed | No stdout |
| Task transitions to BACKLOG from `COMPLETED` | 0 | Timestamps, `completion_summary`, and `commit_close` cleared; `commit_open` preserved; `sprint_tasks` row kept | No stdout |
| Task already in BACKLOG | 0 | No change; any `sprint_tasks` row is kept | Informational message to stderr |
| Invalid task ID | 4 | **No tasks modified** | Error to stderr |

**Output (success):** No output to stdout, exit code 0.

**Audit:** One `TASK_REOPEN` entry per reopened task, against the task, with NULL
`related_entity_id` and NULL `commit_hash`. Three rules apply:

1. **`task reopen` writes `TASK_REOPEN` and nothing else.** It writes no
   `TASK_STATUS_BACKLOG` entry, even though the task ends in `BACKLOG`. The two
   operations are distinct because the commands are: `task stat <ids> BACKLOG`
   changes the status, while `task reopen` additionally clears the lifecycle
   timestamps, the completion summary, and `commit_close`.
2. **No audit entry is altered.** Clearing `commit_close` on the task MUST NOT
   change, blank, or delete any existing audit entry. The `TASK_STATUS_COMPLETED`
   entry written when the task was completed keeps its `commit_hash`, so the record
   of the commit that concluded the task survives the reopening even though the task
   no longer carries it (see `DATABASE.md § The Commit Hash of an Audit Entry`).
3. **A task already in `BACKLOG` is a no-op** (exit 0 with an informational message
   on stderr) and writes no entry.

**Acceptance criteria:**

1. `rmp task reopen -r <name> <id>` on a `COMPLETED` task writes exactly one entry, with operation `TASK_REOPEN`, and writes no `TASK_STATUS_BACKLOG` entry.
2. After the reopening, the task's earlier `TASK_STATUS_COMPLETED` entry still exists with the same `id` and the same `commit_hash`, while `tasks.commit_close` is NULL.
3. `rmp task reopen` on a task already in `BACKLOG` leaves the audit entry count unchanged.

---

### Task Comments

A task comment is a durable, typed log entry attached to a task. Task comments record exclusively the work carried out within the scope of that task: findings, hypotheses raised and tested, tests run, decisions taken, progress, the reason behind a change to the task's definition, and notes. Read oldest first, the comments show how the work on the task progressed.

The four subcommands below are flat subcommands of the `task` family, in the form `task comment-<verb>`. There is no separate `rmp comment` family, and there is no three-level `rmp task comment add` form.

Four properties apply to all four subcommands and are not repeated in full in each block:

- **Each subcommand takes exactly one positional argument.** That argument is the id, and a second or later positional argument is refused with exit code 2 rather than ignored. `Comment Positional Argument Contract` above is canonical, and it governs the `sprint` comment subcommands in the same terms.
- **Comment ids are per-family.** A comment id identifies a row in `task_comments`; the same number in the `sprint` family identifies an unrelated row in `sprint_comments`. `rmp task comment-edit 7` and `rmp sprint comment-edit 7` address different comments (see `DATABASE.md § task_comments Table`).
- **Comments are accepted in every task status,** including `COMPLETED`. No comment subcommand checks or changes a task's status, and `task reopen` does not touch comments.
- **`-y, --type` carries a different enum here than elsewhere in the `task` family.** On `task list`, `task create`, and `task edit`, `-y, --type` carries a `TaskType` value; on the four comment subcommands it carries a comment type. The flag spelling is deliberately reused, but the two enums are unrelated and never interchangeable: a `TaskType` value such as `BUG` is rejected on a comment subcommand with exit code 6, and a comment type such as `FINDING` is rejected on `task create` by that command's own type validation. Validation is therefore per subcommand, and the help and the AI Agent Contract keep the two sets apart (see `HELP.md § Comment subcommand help specifics`).

#### Add Task Comment

```bash
rmp task comment-add -r <name> <task-id> --type <TYPE> --body "<text>"
rmp task c-add -r <name> <task-id> -y <TYPE> -b "<text>"

# Body read from standard input when --body is absent
rmp task comment-add -r <name> <task-id> --type FINDING < finding.txt
```

**Description:** Adds one comment to the given task. The comment is stored with its type, its body, and a creation timestamp; `updated_at` starts null.

**Aliases:** `c-add`

**Arguments:**
- `task-id` - Task ID (required, positive integer)
- Exactly one positional argument is accepted. A second or later positional argument is refused with exit code 2 and the message `Error: invalid input: unexpected argument "X"`; the comment is not added (see `Comment Positional Argument Contract` above).

**Options:**
- `-r, --roadmap <name>` - REQUIRED. Target roadmap.
- `-y, --type <TYPE>` - REQUIRED. Comment type. One of `FINDING`, `HYPOTHESIS`, `TEST`, `DECISION`, `PROGRESS`, `UPDATE`, `NOTE`. See `MODELS.md § Comment Type` for the canonical list and the meaning of each value.
- `-b, --body <text>` - Comment text, maximum 4096 characters. When absent, the body is read from standard input under the bounded read (see `Comment Body Input Source and Precedence` above).

**Validation Rules:**

| Field | Constraint | Error Message (stderr) | Exit Code |
|-------|------------|------------------------|-----------|
| `task-id` | Integer in `1`-`2147483647` (see `Comment Positional Argument Contract`) | "Error: invalid input: invalid task ID: \"X\" (must be a positive integer)" | 2 |
| `type` | Present | "Error: required parameter missing: --type" | 2 |
| `type` | One of the seven task values | "Error: validation error: invalid comment type \"X\" for a task comment; valid types: FINDING, HYPOTHESIS, TEST, DECISION, PROGRESS, UPDATE, NOTE" | 6 |
| `body` | Supplied via `--body` or stdin | "Error: required parameter missing: no comment body supplied" | 2 |
| `body` | Max 4096 chars | "Error: field exceeds maximum size: body exceeds maximum length of 4096 characters" | 6 |
| `body` | Valid UTF-8 | "Error: validation error: body: the value is not valid UTF-8" | 6 |
| `body` | No forbidden control characters | "Error: validation error: body: control characters are not allowed" | 6 |

**Validation Order:**
1. Resolve the roadmap; a missing `-r` fails with exit code 3.
2. Parse the positional `task-id`; a non-integer or non-positive value fails with exit code 2.
3. Consume the subcommand's flags and their values; an unrecognised flag fails with exit code 2, and so does any positional argument left over after the `task-id` (see `Comment Positional Argument Contract` above).
4. Verify `--type` is present; an absent flag fails with exit code 2.
5. Validate the type value against the seven task values; an invalid value fails with exit code 6.
6. Resolve the body from `--body` or standard input; no body fails with exit code 2.
7. Verify the task exists; a missing task fails with exit code 4.
8. Validate the body's length, then its encoding, then its control characters; a violation fails with exit code 6.
9. Insert the comment and write the audit entry in one transaction.

Steps 4 and 5 both precede step 6 deliberately: a missing or invalid `--type` is reported immediately, instead of leaving the command waiting on standard input for a body it is going to reject anyway. Step 3 precedes both for the same reason: a malformed argument list is reported before the command waits on anything or opens the roadmap.

**JSON Output:** `{"id": 12}` — the id of the created comment, in the same shape `task create` uses. Exit code 0.

**Error Conditions:**

| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Task not found | 4 | `Error: resource not found: task N not found` |
| Roadmap not specified | 3 | `Error: no roadmap selected: use -r <name> or --roadmap <name>` |
| Roadmap not found | 4 | `Error: resource not found: roadmap "X"` |
| Invalid task ID format | 2 | `Error: invalid input: invalid task ID: "X" (must be a positive integer)` |
| Missing task ID | 2 | `Error: required parameter missing: task ID required` |
| Extra positional argument | 2 | `Error: invalid input: unexpected argument "X"` |
| Unknown flag | 2 | `Error: invalid input: unknown flag: --foo` |
| Database failure | 1 | `Error: database error: <detail>` |

**Audit:** Logged as `TASK_COMMENT_CREATE` against the parent task (`entity_type = TASK`, `entity_id = <task-id>`), in the same transaction as the insert. See `DATABASE.md § audit Table`.

#### List Task Comments

```bash
rmp task comment-list -r <name> <task-id> [--type <TYPE>]
rmp task c-ls -r <name> <task-id> [-y <TYPE>]
```

**Description:** Returns every comment of the given task, oldest first. The listing is the task's work log, so the order is the story it tells.

**Aliases:** `c-ls`

**Arguments:**
- `task-id` - Task ID (required, positive integer)
- Exactly one positional argument is accepted. A second or later positional argument is refused with exit code 2 and the message `Error: invalid input: unexpected argument "X"`; no listing is produced (see `Comment Positional Argument Contract` above).

**Options:**
- `-r, --roadmap <name>` - REQUIRED. Target roadmap.
- `-y, --type <TYPE>` - Optional filter. Returns only the comments whose type equals `<TYPE>`. The value MUST be one of the seven task values; any other value is rejected with exit code 6.

**Ordering:** `created_at` ascending, with the comment `id` ascending as the tie-breaker for comments created within the same millisecond.

**Result-set size:** Unbounded. Every matching comment is returned. There is no `--limit` flag, no `--desc` flag, and no pagination, matching `sprint tasks`, which also returns a complete membership listing.

**JSON Output:** Array of TaskComment objects (see `DATA_FORMATS.md § Task Comment`). An empty array `[]` when the task has no comments, or none of the requested type. Exit code 0.

**Error Conditions:**

| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Task not found | 4 | `Error: resource not found: task N not found` |
| Invalid `--type` value | 6 | `Error: validation error: invalid comment type "X" for a task comment; valid types: FINDING, HYPOTHESIS, TEST, DECISION, PROGRESS, UPDATE, NOTE` |
| Invalid task ID format | 2 | `Error: invalid input: invalid task ID: "X" (must be a positive integer)` |
| Missing task ID | 2 | `Error: required parameter missing: task ID required` |
| Extra positional argument | 2 | `Error: invalid input: unexpected argument "X"` |
| Roadmap not specified | 3 | `Error: no roadmap selected: use -r <name> or --roadmap <name>` |

**Audit:** None. Listing is a read and writes no audit entry.

#### Edit Task Comment

```bash
rmp task comment-edit -r <name> <comment-id> [--type <TYPE>] [--body "<text>"]
rmp task c-edit -r <name> <comment-id> [-y <TYPE>] [-b "<text>"]

# New body read from standard input (no --type given)
rmp task comment-edit -r <name> <comment-id> < revised.txt
```

**Description:** Changes the type and/or the body of one existing task comment, identified by the comment's own id. At least one of `--type` and `--body` is required.

**Aliases:** `c-edit`

**Arguments:**
- `comment-id` - Comment ID (required, positive integer). This is the comment's id, **not** the id of the task it belongs to.
- Exactly one positional argument is accepted. A second or later positional argument is refused with exit code 2 and the message `Error: invalid input: unexpected argument "X"`; the comment is not changed (see `Comment Positional Argument Contract` above).

**Options:**
- `-r, --roadmap <name>` - REQUIRED. Target roadmap.
- `-y, --type <TYPE>` - New comment type. One of the seven task values.
- `-b, --body <text>` - New comment text, maximum 4096 characters. When `--body` is absent and `--type` is also absent, the new body is read from standard input under the bounded read; when `--type` is present and `--body` is absent, the body is left unchanged and standard input is not read (see `Comment Body Input Source and Precedence` above).

**Replacement semantics:** The edit replaces the stored body in place and stamps `updated_at` with the edit's timestamp, so the JSON output of a later listing shows that the comment was altered. The previous text is not retained anywhere and cannot be recovered. This is a deliberate trade-off: the audit log records that an edit happened, not what it replaced.

**Validation Rules:**

| Field | Constraint | Error Message (stderr) | Exit Code |
|-------|------------|------------------------|-----------|
| `comment-id` | Integer in `1`-`2147483647` (see `Comment Positional Argument Contract`) | "Error: invalid input: invalid comment ID: \"X\" (must be a positive integer)" | 2 |
| change | At least one change requested: a `--type` value, a `--body` value, or a body on standard input | "Error: required parameter missing: at least one of --type or --body is required" | 2 |
| `type` | One of the seven task values | "Error: validation error: invalid comment type \"X\" for a task comment; valid types: FINDING, HYPOTHESIS, TEST, DECISION, PROGRESS, UPDATE, NOTE" | 6 |
| `body` | `--body` present but empty or whitespace only | "Error: required parameter missing: no comment body supplied" | 2 |
| `body` | Max 4096 chars | "Error: field exceeds maximum size: body exceeds maximum length of 4096 characters" | 6 |
| `body` | Valid UTF-8 | "Error: validation error: body: the value is not valid UTF-8" | 6 |
| `body` | No forbidden control characters | "Error: validation error: body: control characters are not allowed" | 6 |

**Validation Order:**
1. Resolve the roadmap; a missing `-r` fails with exit code 3.
2. Parse the positional `comment-id`; a non-integer or non-positive value fails with exit code 2.
3. Consume the subcommand's flags and their values; an unrecognised flag fails with exit code 2, and so does any positional argument left over after the `comment-id` (see `Comment Positional Argument Contract` above).
4. Validate the `--type` value when the flag is present; an invalid value fails with exit code 6, before standard input is considered.
5. Resolve the new body when one is being set, from `--body` or from standard input; when neither `--type` nor a body is supplied, the command fails with exit code 2.
6. Verify the comment exists in `task_comments`; a missing comment fails with exit code 4.
7. Validate the body's length, then its encoding, then its control characters; a violation fails with exit code 6.
8. Apply the update, stamp `updated_at`, and write the audit entry in one transaction.

Step 3 precedes step 5 for the same reason step 4 does: a malformed argument list is reported at once, instead of leaving the command waiting on standard input for a body it is going to reject anyway.

**No-op is not accepted.** Unlike `task edit`, which succeeds with exit code 0 when no field is given, `comment-edit` requires at least one change and fails with exit code 2 when none is requested. A change is requested by a `--type` value, by a `--body` value, or by a body arriving on standard input, so the flagless form `comment-edit <comment-id> < revised.txt` is a valid edit and not a no-op: the body on standard input is the change. Only the case where `--type` is absent, `--body` is absent, and standard input is empty, whitespace only, or not connected requests no change at all, and that is the case that fails with exit code 2 and the message "at least one of --type or --body is required". `task edit` can distinguish "no flags" from "flags to apply" without ambiguity; `comment-edit` cannot on the flags alone, because an absent `--body` with an absent `--type` is precisely the form that means "read the new body from standard input", so the decision is made after standard input has been resolved.

**Output (success):** No output, exit code 0. This follows the convention for mutating commands (`task edit`, `sprint update`).

**Error Conditions:**

| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Comment not found | 4 | `Error: resource not found: task comment N not found` |
| Roadmap not specified | 3 | `Error: no roadmap selected: use -r <name> or --roadmap <name>` |
| Invalid comment ID format | 2 | `Error: invalid input: invalid comment ID: "X" (must be a positive integer)` |
| Missing comment ID | 2 | `Error: required parameter missing: comment ID required` |
| Extra positional argument | 2 | `Error: invalid input: unexpected argument "X"` |
| Database failure | 1 | `Error: database error: <detail>` |

A comment id that exists in `sprint_comments` but not in `task_comments` is a not-found condition here (exit code 4): the two id spaces are independent.

**Audit:** Logged as `TASK_COMMENT_UPDATE` against the parent task (`entity_type = TASK`, `entity_id` = the id of the task the comment belongs to), in the same transaction as the update.

#### Remove Task Comment

```bash
rmp task comment-remove -r <name> <comment-id>
rmp task c-rm -r <name> <comment-id>
```

**Description:** Deletes one task comment, identified by the comment's own id. The row is removed outright; there is no soft delete and no recovery. The audit entry survives the row, so the task's history still records that a comment existed and was removed.

**Aliases:** `c-rm`

**Arguments:**
- `comment-id` - Comment ID (required, positive integer). This is the comment's id, **not** the id of the task it belongs to.
- Exactly one positional argument is accepted. A second or later positional argument is refused with exit code 2 and the message `Error: invalid input: unexpected argument "X"`; nothing is deleted (see `Comment Positional Argument Contract` above).

**Options:**
- `-r, --roadmap <name>` - REQUIRED. Target roadmap.

**Single-id command:** `comment-remove` takes exactly one comment id, in two senses. The id is a single value: the command accepts no comma-separated list, so the batch fail-fast rules that govern `task remove` do not apply. The id is also the only positional argument: a second positional argument is an error and never a second deletion, so the command deletes either exactly one comment or none at all.

**Output (success):** No output, exit code 0.

**Error Conditions:**

| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Comment not found | 4 | `Error: resource not found: task comment N not found` |
| Roadmap not specified | 3 | `Error: no roadmap selected: use -r <name> or --roadmap <name>` |
| Invalid comment ID format | 2 | `Error: invalid input: invalid comment ID: "X" (must be a positive integer)` |
| Missing comment ID | 2 | `Error: required parameter missing: comment ID required` |
| Extra positional argument | 2 | `Error: invalid input: unexpected argument "X"` |
| Database failure | 1 | `Error: database error: <detail>` |

**Audit:** Logged as `TASK_COMMENT_DELETE` against the parent task (`entity_type = TASK`, `entity_id` = the id of the task the comment belonged to), in the same transaction as the delete.

---

## Sprint Management

Command: `rmp sprint` (alias: `rmp s`)

### List Sprints

```bash
rmp sprint list -r <name> [--status <state>]
rmp sprint ls -r <name>
```

**JSON Output:** Array of Sprint objects.

**Options:**
- `--status <state>` - Optional filter that restricts the result to sprints whose
  status equals `<state>` (one of PENDING, OPEN, CLOSED). It selects which
  **sprints** the array contains; it does not filter the tasks of any sprint. An
  invalid value is rejected with exit code 6.

**Result Ordering:** Sprints are returned ordered by `order` ascending: the sprint
with the lowest `order` value first. That is the roadmap's planned execution
order. `order` is the field a sprint carries for exactly this purpose — the
sprint with the lowest `order` executes first — so the listing hands the caller
the sprints in the sequence in which they are planned to run. The `--order` flag of
`Create Sprint` and `Update Sprint` below is what sets the value. See
`MODELS.md § Sprint Field Constraints`.

**The ordering is total, so the result is deterministic.** A sprint's `order` is
`NOT NULL` and unique across the roadmap, enforced by the `idx_sprints_order`
unique index (see `DATABASE.md § sprints Table`); a value already used by another
sprint is rejected with exit code 5 on both `Create Sprint` and `Update Sprint`
below. No sprint can lack an `order`, and no two sprints of one roadmap can share
one, so ordering by `order` alone places every sprint at exactly one position. The
sequence is fully determined by the data, and repeating the same read over
unchanged data returns the same sequence. This specification states no tie-break
rule because no tie can occur.

**The order is a published guarantee.** It is part of this command's contract, not
an incidental property of the query that produces the result. A caller may rely on
it.

**The `--status` filter narrows the result; it never reorders it.** The filter
selects which sprints the array contains. The sprints it keeps appear in the same
relative sequence they hold in the unfiltered listing — `order` ascending, with
the excluded sprints simply absent. Filtering removes entries and changes nothing
else about the order of the entries that remain.

**Relation to the web interface.** The read-only web interface presents the same
sprints on its sprints page, and it does not present them as one sequence: it
splits them into three status tabs and orders one of those tabs in reverse.
`WEB.md § Roadmap Sprints Page` is canonical for the order of each tab and states
why that tab differs from this listing.

**Membership fields.** Every Sprint object the array contains carries its
membership resolved, exactly as `Get Sprint` below returns it for the same sprint:

- `task_count` is the sprint's real member-task count: the number of tasks that
  belong to the sprint at the moment of the read, in any status. It is never a
  placeholder, and a sprint that holds tasks never reports `0`.
- `tasks` is the list of the member tasks' **ids** — integers, not task objects.
  The listing returns ids only. A caller that needs the task records themselves
  reads `List Sprint Tasks` below.
- The two fields always agree: `task_count` equals the number of entries in
  `tasks` in every Sprint object the listing returns.
- The ids appear in ascending task-id order, the order `Get Sprint` returns them
  in. That order is not the sprint's planned in-sprint execution order; a caller
  that needs the planned order reads `List Sprint Tasks` below or the `task_order`
  field of `Sprint Statistics` below. See `MODELS.md § Sprint Field Constraints`.
- The `--status` filter does not change any of this: the sprints the filter keeps
  carry the same `task_count` and `tasks` values they carry in the unfiltered
  listing.

**A sprint with no member task** reports `task_count` `0` and `tasks` `[]` — an
empty JSON array, never `null` — exactly as `Get Sprint` reports the same sprint.
The general rule is stated in `DATA_FORMATS.md § Implementation Notes`
(Empty arrays).

**One sprint, one answer.** `sprint list`, `sprint get`, and `sprint tasks` never
disagree about the same sprint read at the same moment. The `task_count` and
`tasks` values a sprint carries in the listing are the values `sprint get` returns
for that sprint, and the ids in `tasks` are exactly the ids of the tasks
`sprint tasks` returns for that sprint when no `-s, --status` filter is applied.
The three commands present the same membership at different depths: `sprint list`
and `sprint get` publish it as the sprint's `tasks` ids and `task_count`, while
`sprint tasks` returns the member task records themselves, in the sprint's planned
order, and accepts a task-status filter that the other two do not.

**Read cost.** The listing resolves `task_count` and `tasks` for every sprint it
returns in a bounded number of queries that does not grow with the number of
sprints: it issues no query per sprint (see
`DATABASE.md § Read the Membership of Many Sprints (Grouped)`).

### Create Sprint

```bash
rmp sprint create -r <name> -t "Title" -d "Description" [--max-tasks <n>] [--order <n>]
rmp sprint new -r <name> -t "Title" -d "Description" [--max-tasks <n>] [--order <n>]
```

**Required parameters:** BOTH `-t, --title` and `-d, --description` are mandatory.
Omitting either one, or passing it the literal empty string, fails with exit code 2
and the message `Error: required parameter missing: --title` (or `--description`).
Passing one a value that carries text but is empty once trimmed is a different case
and fails with exit code 6: see **Title and Description Validation** below. Every
example below supplies both.

**Options:**
- `-t, --title <text>` - Sprint title (required), maximum 255 characters
- `-d, --description <text>` - Sprint description (required), maximum 2048
  characters. The description MUST state the high-level (macro) goal of the
  development effort the sprint delivers: a new development, a fix, a refactoring,
  or another kind of change. Together with the title, it MUST give a human reader
  or an AI agent a clear macro idea of what the sprint's tasks are specifically
  aimed at. See `MODELS.md § Sprint Field Constraints` for the canonical definition
  of the field.
- `--max-tasks <n>` - Maximum number of tasks allowed in the sprint (optional; omit
  for unlimited capacity). When provided, MUST be a positive integer in the range
  `1`-`10000`. A value `< 1` or `> 10000`, or a non-integer value, is rejected with
  exit code 6.
- `--order <n>` - Sprint execution order (optional). The natural, sequential order
  in which sprints are executed; the sprint with the lowest `--order` value runs
  first. When provided, MUST be a positive integer strictly greater than zero
  (`> 0`) and MUST NOT already be used by another sprint in the roadmap. When
  omitted, the next available value is auto-assigned as the highest existing
  `order` plus one (the first sprint in a roadmap receives `1`). See
  `MODELS.md § Sprint Field Constraints` and `DATABASE.md § Create Sprint`.

**Title and Description Validation:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Title missing, or the literal empty string | 2 | "Error: required parameter missing: --title" |
| Description missing, or the literal empty string | 2 | "Error: required parameter missing: --description" |
| Title supplied, but empty once trimmed | 6 | "Error: validation error: title cannot be empty" |
| Description supplied, but empty once trimmed | 6 | "Error: validation error: description cannot be empty" |
| Title exceeds 255 characters | 6 | "Error: field exceeds maximum size: title exceeds maximum length of 255 characters" |

No sprint is created under any of these rows, and stdout stays empty.

The sprint `title` and `description` are also subject to the Control-Character
Constraint, the UTF-8 Encoding Constraint, and the Emptiness Constraint described in
`Field Validation` above. `sprint create` judges emptiness by the same criterion
`sprint update` and `task edit` apply: after trimming, so a value made only of
whitespace names nothing and is refused. The maximum length is measured on the
trimmed value, so a title of exactly 255 characters carrying surrounding whitespace is
accepted and stored trimmed.

**Bound Validation:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| `--max-tasks` `< 1` or `> 10000` | 6 | "Error: validation error: max_tasks must be between 1 and 10000, got N" |
| `--max-tasks` non-integer | 2 | "Error: invalid input: invalid value for --max-tasks: strconv.Atoi: parsing \"X\": invalid syntax" |
| `--order` `<= 0` | 6 | "Error: validation error: --order must be a positive integer greater than zero (got N)" |
| `--order` non-integer | 6 | "Error: validation error: --order must be a positive integer greater than zero" |
| `--order` already used by another sprint | 5 | "Error: resource already exists: sprint order N is already in use" |

**Output (success):** `{"id": 1}`, exit code 0.

**Audit:** One `SPRINT_CREATE` entry against the created sprint, written in the same
transaction as the insert, including when `--order` is omitted and the order is
auto-assigned (see `DATABASE.md § Transactional Atomicity Guarantees`).
`related_entity_id` and `commit_hash` are NULL.

### Get Sprint

```bash
rmp sprint get -r <name> <id>
```

**JSON Output:** Single Sprint object, including the sprint `title` and `description` fields.

**Membership fields.** The object carries `task_count`, the sprint's real
member-task count, and `tasks`, the ids of its member tasks in ascending task-id
order. A sprint with no member task reports `0` and `[]`. These are the same two
fields, carrying the same values in the same order, that `List Sprints` above
returns for this sprint; neither command resolves membership that the other leaves
unresolved. See `MODELS.md § Sprint Field Constraints`.

### List Sprint Tasks

```bash
rmp sprint tasks -r <name> <id> [-s, --status <state>] [--order-by-priority]
```

**JSON Output:** Array of Task objects associated with the sprint, ordered by sprint position (default) or, when `--order-by-priority` is given, by priority DESC with sprint position as the tiebreaker.

**Relation to the sprint's membership fields.** Without `-s, --status`, the ids of
the task objects this command returns are exactly the ids the same sprint carries
in its `tasks` field in `List Sprints` and `Get Sprint` above, and their number is
that sprint's `task_count`. The two presentations carry the same membership at
different depths and in different orders: this command returns whole task records,
in the sprint's planned in-sprint execution order by default and by priority when
`--order-by-priority` is given, while `tasks` carries ids alone in ascending
task-id order. With `-s, --status`, this command returns a subset of the sprint's
member tasks, while `task_count` keeps counting every member task whatever its
status.

**Options:**
- `-s, --status <state>` - Optional filter that restricts the result to tasks
  whose status equals `<state>` (one of BACKLOG, SPRINT, DOING, TESTING,
  COMPLETED). Both the short form `-s` and the long form `--status` are accepted,
  consistent with the sibling list commands (`task ls`, `backlog ls`). The
  handler parses this flag and passes it to the sprint-task query, so only
  matching tasks are returned. Without it, every task in the sprint is returned
  regardless of status.
- `--order-by-priority` - Order by priority DESC, then sprint position ASC

**Exit Codes:** `0` (success), `3` (missing `-r`), `4` (sprint not found), `6` (invalid `-s, --status` value).

### List Incomplete Sprint Tasks (open-tasks)

```bash
rmp sprint open-tasks -r <name> <id> [--order-by-priority]
```

**Description:** Returns all tasks in a sprint that are not yet completed (status: SPRINT, DOING, or TESTING). Useful during stand-ups and sprint reviews to see remaining work without client-side filtering.

**Arguments:**
- `id` - Sprint identifier

**Options:**
- `--order-by-priority` - Order by priority DESC, then sprint position ASC; without it, sprint position ASC alone

**Default Ordering:** Sprint position ASC (same as `sprint tasks`).

**JSON Output:** Array of Task objects with status SPRINT, DOING, or TESTING. Returns an empty array if the sprint has no open tasks.

**Error Conditions:**
| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Sprint not found | 4 | `Error: resource not found: sprint N` |
| Missing sprint ID | 2 | `Error: required parameter missing: sprint ID required` |

### Sprint Statistics

```bash
rmp sprint stats -r <name> <id>
```

**JSON Output:**
```json
{
  "sprint_id": 1,
  "total_tasks": 10,
  "completed_tasks": 3,
  "progress_percentage": 30.0,
  "status_distribution": {
    "SPRINT": 4,
    "DOING": 2,
    "TESTING": 1,
    "COMPLETED": 3
  },
  "task_order": [5, 3, 8, 1, 9, 2, 7, 4, 6, 10],
  "velocity": 0.0,
  "days_elapsed": 5,
  "days_remaining": null,
  "burndown": [
    {"date": "2026-03-19", "tasks_remaining": 10},
    {"date": "2026-03-21", "tasks_remaining": 8},
    {"date": "2026-03-24", "tasks_remaining": 7}
  ]
}
```

**Fields:**
- `task_order` - Array of task IDs ordered by position (first to last)
- `velocity` - Tasks completed per day (CLOSED sprints only). Computed as `completed_task_count / max(1.0, (closed_at - started_at) in days)`, so the sprint duration in the denominator is floored at a minimum of 1 day and a sprint that starts and closes within the same day yields a velocity equal to its completed-task count rather than an inflated value. 0.0 for OPEN or PENDING sprints, and for CLOSED sprints with no completed tasks
- `days_elapsed` - Days since the sprint was started (OPEN sprints only). null for PENDING and CLOSED sprints, and for OPEN sprints with no started_at date
- `days_remaining` - Always null. Sprint model has no end_date field
- `burndown` - Array of daily snapshots `{date, tasks_remaining}` derived from task `closed_at` dates. Ordered by date ascending. Empty array when no tasks have been completed

**Burndown Computation:**
- Start with total_tasks as the initial remaining count on the sprint start date (or first completion date if no start date is set)
- Subtract completions per day based on task `closed_at` timestamps
- Only includes dates on which at least one task was completed

### Show Sprint Status Report

```bash
rmp sprint show -r <name> <id>
```

**Description:** Displays a comprehensive status report of a sprint, including task statistics and distribution by severity and criticality. Provides a quick overview for sprint stand-up meetings and progress tracking.

**JSON Output:**
```json
{
  "sprint_id": 5,
  "sprint_title": "Sprint 12",
  "sprint_description": "Refactor the task-ordering engine so that sprint positions stay stable across bulk moves.",
  "status": "OPEN",
  "max_tasks": 25,
  "capacity_pct": 56.0,
  "current_load": 14,
  "task_order": [12, 7, 19, 3, 21, 8, 15, 2, 9, 11, 4, 17, 6, 1, 20, 5, 18, 10, 13, 16],
  "summary": {
    "total_tasks": 20,
    "pending": 8,
    "in_progress": 6,
    "completed": 6
  },
  "progress": {
    "pending_percentage": 40.0,
    "in_progress_percentage": 30.0,
    "completed_percentage": 30.0
  },
  "severity_distribution": {
    "0-2": {"count": 2, "percentage": 10.0},
    "3-5": {"count": 5, "percentage": 25.0},
    "6-7": {"count": 8, "percentage": 40.0},
    "8-9": {"count": 5, "percentage": 25.0}
  },
  "criticality_distribution": {
    "low": {"count": 4, "percentage": 20.0},
    "medium": {"count": 8, "percentage": 40.0},
    "high": {"count": 6, "percentage": 30.0},
    "critical": {"count": 2, "percentage": 10.0}
  }
}
```

**Output Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `sprint_id` | integer | Sprint identifier |
| `sprint_title` | string | Sprint title |
| `sprint_description` | string | Sprint description |
| `status` | string | Sprint status (OPEN, CLOSED) |
| `max_tasks` | integer or null | Sprint capacity cap (maximum number of tasks the sprint may hold), or null when no cap is set |
| `capacity_pct` | float or null | Percentage of capacity used, computed from `current_load` against `max_tasks`. null when no cap is set |
| `current_load` | integer | Number of non-COMPLETED tasks counting against the sprint capacity |
| `task_order` | array of integers | Task IDs in sprint position order (first to last) |
| `summary.total_tasks` | integer | Total number of tasks in sprint |
| `summary.pending` | integer | Tasks with status BACKLOG or SPRINT |
| `summary.in_progress` | integer | Tasks with status DOING or TESTING |
| `summary.completed` | integer | Tasks with status COMPLETED |
| `progress.pending_percentage` | float | Percentage of pending tasks |
| `progress.in_progress_percentage` | float | Percentage of tasks in progress |
| `progress.completed_percentage` | float | Percentage of completed tasks |
| `severity_distribution` | object | Task distribution by severity ranges |
| `criticality_distribution` | object | Task distribution by criticality levels |

**Severity Ranges:**
- `0-2`: Low severity
- `3-5`: Medium severity
- `6-7`: High severity
- `8-9`: Critical severity

**Criticality Levels:**
- `low`: Severity 0-2
- `medium`: Severity 3-5
- `high`: Severity 6-7
- `critical`: Severity 8-9

**Error Cases:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 4 | "Sprint not found" |
| Roadmap not specified | 3 | "Error: no roadmap selected: use -r <name> or --roadmap <name>" |

### Sprint Lifecycle

```bash
rmp sprint start -r <name> <id>
rmp sprint close -r <name> <id> [--force]
rmp sprint reopen -r <name> <id>
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--force` | (`sprint close` only) Close the sprint even if tasks are still in SPRINT, DOING, or TESTING status. A warning listing the incomplete tasks is printed to stderr. |

**Active-Task Safety Check (sprint close):**

`sprint close` queries for tasks with status `SPRINT`, `DOING`, or `TESTING` in the sprint before closing. If any exist and `--force` is not provided, the command returns exit code 6 with an error listing the task IDs and statuses. With `--force`, the sprint is closed and a warning is printed to stderr.

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| No active tasks | 0 | None |
| Active tasks exist, no `--force` | 6 | "invalid input: sprint #N has M active task(s) still in progress: #ID (STATUS), ... — use --force to close anyway" |
| Active tasks exist, `--force` given | 0 | "warning: closing sprint #N with M incomplete task(s): #ID (STATUS), ..." |

**Output (success):** No output, exit code 0.

**Audit:** One entry per invocation, against the sprint, with NULL
`related_entity_id` and NULL `commit_hash`: `SPRINT_START` for `sprint start`,
`SPRINT_CLOSE` for `sprint close`, and `SPRINT_REOPEN` for `sprint reopen`. Closing
with `--force` writes the same `SPRINT_CLOSE` entry as closing without it; the flag
changes what the command permits, not what it records. None of the three commands
changes any task's status, so none writes a `TASK_STATUS_*` entry.

### Task Assignment

```bash
rmp sprint add-tasks -r <name> <sprint-id> <task-ids>
rmp sprint remove-tasks -r <name> <sprint-id> <task-ids>
rmp sprint move-tasks -r <name> <from-id> <to-id> <task-ids>
```

**Description:** Bulk assignment/removal/movement of tasks using comma-separated IDs. Alias for `add-tasks` is `add`.

**Batch Operation Behavior (Fail-Fast):**

All sprint task operations validate ALL IDs before making any changes.

| Scenario | Exit Code | Behavior | stderr Output |
|----------|-----------|----------|---------------|
| All IDs valid | 0 | All tasks assigned/removed/moved | None |
| One or more task IDs do not exist, on `add-tasks` | 4 | **No changes made** | "Error: resource not found: task(s) not found: [<ids>]" |
| One or more task IDs are not members, on `remove-tasks` | 6 | **No changes made** | "Error: validation error: task(s) not in sprint #N: [<ids>]" |
| Sprint ID does not exist, on `add-tasks` or `remove-tasks` | 4 | **No changes made** | "Error: resource not found: sprint N" |
| The source sprint ID does not exist, on `move-tasks` | 4 | **No changes made** | "Error: resource not found: from sprint N" |
| The destination sprint ID does not exist, on `move-tasks` | 4 | **No changes made** | "Error: resource not found: to sprint N" |
| A task ID is not a positive integer | 2 | **No changes made** | "Error: invalid input: invalid task ID: \"X\" (must be a positive integer)" |

Unlike the task-family batch commands, which report only that the batch could not be satisfied, these three name the offending IDs: the list is rendered as Go renders a slice of integers, space-separated inside square brackets, in the order the IDs were supplied.

`move-tasks` is the only one of the three that names two sprints, so its
unresolvable-sprint line carries the word `from` or the word `to` in front of the
id. That word is the only thing in the line that says which of the two arguments
was wrong, and it is the sole respect in which the line differs from the one
`add-tasks` and `remove-tasks` print: the classification is stated once, by the
same sentinel, in all three.

**Validation Order:**

The order below is normative and is the order the three commands apply. The two
lexical steps need no database and MUST run before it is opened; the roadmap is
opened next; every step from there to the last validation reads the database and
writes nothing; the execution step is the only one that writes. Each step names
the subcommands that perform it, and a step that names fewer than all three is
not performed by the others at all.

1. Validate the format and range of every sprint id on the command line: `<sprint-id>` for `add-tasks` and `remove-tasks`, `<from-id>` and then `<to-id>` for `move-tasks`. This reads the argument text alone; no sprint is looked up here (all three)
2. Parse all task IDs and validate their format and range (all three)
3. Open the roadmap, which refuses a roadmap that does not exist before any sprint or task is resolved (all three)
4. Verify the sprint exists: `<sprint-id>` for `add-tasks` and `remove-tasks`; `move-tasks` resolves its two sprints one at a time, source before destination, each lookup immediately followed by the CLOSED check below (all three)
5. Reject a CLOSED sprint: `add-tasks` refuses a CLOSED `<sprint-id>`, and `move-tasks` refuses a CLOSED `<from-id>` or `<to-id>`. `remove-tasks` deliberately does not, because taking tasks out of a closed sprint is the carry-over workflow (`add-tasks`, `move-tasks`)
6. Verify all task IDs exist in the roadmap (`add-tasks` only). The other two run no existence check of their own: the membership step below refuses an id the sprint does not hold, and an id that names no task cannot be held by one
7. Verify that the batch does not exceed the sprint's `max_tasks` capacity, when the sprint sets one (`add-tasks` only). This read is a fast-feedback path; the authoritative, race-free enforcement runs inside the transaction of the execution step (see `DATABASE.md § Transactional Atomicity Guarantees`)
8. Verify that every task is currently a member of the sprint it is being taken out of: `<sprint-id>` for `remove-tasks`, `<from-id>` for `move-tasks` (`remove-tasks`, `move-tasks`)
9. For `add-tasks`: nothing is verified about the sprint a task already belongs to (see Re-parenting on `add-tasks` below). This item has no failure mode; it records a check the command does not perform
10. Only after full validation succeeds, execute the operation
11. If any validation fails, exit immediately without making changes

**Re-parenting on `add-tasks`:**

`add-tasks` accepts a task that already belongs to another sprint and moves it. The
task's single membership row is re-parented onto the sprint named on the command line
and appended after that sprint's last member; the sprint the task came from no longer
holds it; and the command exits 0, exactly as it does for a task that belonged to no
sprint. Nothing is printed about the previous sprint, and the caller does not have to
run `remove-tasks` against it first.

The statement that performs this, and the repair the previous sprint's positions
receive in the same transaction, are specified in
`DATABASE.md § Add Task to Sprint with Position`.

**Automatic Status Updates:**

| Command | Task Status Change | Description |
|---------|-------------------|-------------|
| `add-tasks` | BACKLOG → SPRINT | Tasks automatically change to SPRINT status when added to sprint |
| `remove-tasks` | SPRINT, DOING, TESTING, or COMPLETED → BACKLOG | Tasks automatically return to BACKLOG when removed from sprint, whatever their status. The command also clears `started_at`, `tested_at`, `closed_at`, `completion_summary`, and `commit_close`, and preserves `commit_open` |
| `move-tasks` | (No change) | Status is preserved when moving between sprints |

**Audit:** Every one of these three commands writes one entry per **entity** it
touches, never one entry per invocation:

| Command | Entries per task | Detail |
|---------|------------------|--------|
| `add-tasks` | 2 | `SPRINT_ADD_TASK` against the sprint with `related_entity_id` = the task id, plus `TASK_STATUS_SPRINT` against the task with `related_entity_id` = the sprint id |
| `remove-tasks` | 2 | `SPRINT_REMOVE_TASK` against the sprint with `related_entity_id` = the task id, plus `TASK_STATUS_BACKLOG` against the task with `related_entity_id` = the sprint id |
| `move-tasks` | 2 | `SPRINT_MOVE_TASK_OUT` against the source sprint and `SPRINT_MOVE_TASK_IN` against the destination sprint, both with `related_entity_id` = the task id |

Four rules govern these entries:

1. **The sprint entry names the task.** Without `related_entity_id`, every
   `SPRINT_ADD_TASK` entry of a sprint reads identically and none of them says which
   task was added. Naming the task is what makes the sprint's history readable.
2. **The task entry exists so the task's own history is not silent.**
   `rmp audit history TASK <id>` shows the task entering the sprint and returning to
   the backlog, which it could not if the change were recorded against the sprint
   alone.
3. **The task entry names the sprint.** `add-tasks` and `remove-tasks` each involve
   two entities, so each of the two entries names the other one: the pair is
   mirrored, carrying transposed ids and one shared `performed_at`. Reading
   `audit history TASK <id>` therefore shows not just that the task joined or left a
   sprint but **which** sprint, without consulting the sprint's own history. This
   follows the governing rule that `related_entity_id` names the counterpart entity
   of the operation that produced the entry
   (`DATABASE.md § The Two Entities of a Relational Operation`); it is not a
   command-specific exception. The same `TASK_STATUS_BACKLOG` operation written by
   `task stat <ids> BACKLOG` carries NULL, because that invocation has no second
   entity to name.
4. **`move-tasks` writes no `TASK_STATUS_*` entry,** because it changes no task's
   status (see Automatic Status Updates above). The two sprint entries are the whole
   record of the move.

`remove-tasks` writes its `TASK_STATUS_BACKLOG` entry for every task removed,
including a task that was already in `BACKLOG` status while remaining a sprint member
(see `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status`): the entry records
the command's effect on the task, and the entry count is therefore always exactly one
per task named on the command line.

All entries of one invocation share one `performed_at` value and are written in the
same transaction as the membership and status changes (see
`DATABASE.md § Transactional Atomicity Guarantees`), so a rejected command writes no
entry at all.

**Acceptance criteria:**

1. `rmp sprint add-tasks -r <name> <s> <a>,<b>` writes exactly four entries: two `SPRINT_ADD_TASK` against `<s>` with `related_entity_id` `<a>` and `<b>`, and two `TASK_STATUS_SPRINT`, one against `<a>` and one against `<b>`, each with `related_entity_id = <s>`.
2. `rmp sprint remove-tasks -r <name> <s> <a>` writes exactly two entries: `SPRINT_REMOVE_TASK` against `<s>` with `related_entity_id = <a>`, and `TASK_STATUS_BACKLOG` against `<a>` with `related_entity_id = <s>`.
3. The entries of criteria 1 and 2 are mirrored: within one invocation, the sprint entry's `entity_id` equals the task entry's `related_entity_id`, the sprint entry's `related_entity_id` equals the task entry's `entity_id`, and both carry the same `performed_at`.
4. `rmp task stat -r <name> <a> BACKLOG` writes one `TASK_STATUS_BACKLOG` entry with `related_entity_id IS NULL`, so the same operation is distinguishable by that column from the one `sprint remove-tasks` writes.
5. `rmp sprint move-tasks -r <name> <from> <to> <a>` writes exactly two entries: `SPRINT_MOVE_TASK_OUT` against `<from>` and `SPRINT_MOVE_TASK_IN` against `<to>`, both with `related_entity_id = <a>`, and writes no entry with `entity_type = TASK`.
6. No invocation of any of the three commands writes `SPRINT_MOVE_TASK`.
7. A command rejected at any validation step writes zero entries.

**Note:** The status SPRINT is automatically managed by sprint operations. Users MUST NOT manually set status to SPRINT using `task stat`; attempts to do so are rejected with exit code 6 and the error message `"Error: validation error: status SPRINT can only be set automatically via 'sprint add-tasks'"`. Manual status transitions follow: BACKLOG → SPRINT (automatic) → DOING → TESTING → COMPLETED. `task stat <ids> BACKLOG` is also accepted from `SPRINT` and from `COMPLETED`, and it does not remove the task from its sprint: the task keeps its `sprint_tasks` row while its status reads `BACKLOG`. See `STATE_MACHINE.md § Valid Transitions` for the full set and `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status` for the membership rule.

**Output (success):** No output, exit code 0.

### Task Ordering

Commands for managing sprint task order within a sprint. Tasks are ordered by position (0-based), where position 0 is the first task in the sprint.

**Positions are unique within a sprint, and none of these commands can be told to break that.** No two member tasks of one sprint hold the same position; the schema enforces the invariant (`DATABASE.md § Position Uniqueness Within a Sprint`). None of the commands in this section takes a position for more than one task: `reorder` takes an order and derives every position from it, `swap` exchanges two positions that already exist, and `move-to`, `top` and `bottom` name one target slot and shift the other members around it. Every one of them therefore leaves the sprint holding a permutation of its positions.

**There is consequently no "position already in use" error in this section, and none of these commands repairs a collision.** A collision cannot be requested, so there is nothing for a command to reject or to repair. Should one ever reach the database it means a defect in a write path, not bad input, and it surfaces as a database failure (exit code `1`) rather than as a validation error — see `ARCHITECTURE.md § Exit Codes`. The error tables below are complete as they stand.

#### Reorder Tasks (Set Exact Order)

```bash
rmp sprint reorder -r <name> <sprint-id> <task-ids>
rmp sprint order -r <name> <sprint-id> <task-ids>
```

**Description:** Sets the exact order of all tasks in a sprint. The order of task IDs in the argument defines the new sequence.

**Arguments:**
- `sprint-id` - Sprint identifier
- `task-ids` - Comma-separated list of task IDs in the desired order (e.g., `5,3,1,4,2`)

**Validation:**
- All task IDs must belong to the specified sprint
- Duplicate task IDs are not allowed
- All sprint tasks must be included (partial reorder is not supported)

**Behavior:**
- Task at index 0 gets position 0 (first)
- Task at index 1 gets position 1 (second)
- And so on...

**JSON Output (success):** A JSON success object is written to stdout, exit code 0:

```json
{
  "success": true,
  "sprint_id": 1,
  "task_order": [5, 3, 1, 4, 2]
}
```

**Error Output:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 4 | "Sprint not found" |
| Task ID not in sprint | 6 | "Task ID N is not in sprint" |
| Duplicate task IDs | 6 | "Duplicate task ID: N" |
| Missing task IDs | 6 | "Task list incomplete: expected N tasks, got M" |
| Invalid task ID format | 2 | "Invalid task ID: X" |

#### Move Task to Position

```bash
rmp sprint move-to -r <name> <sprint-id> <task-id> <position>
rmp sprint mvto -r <name> <sprint-id> <task-id> <position>
```

**Description:** Moves a single task to a specific position, shifting other tasks accordingly.

**Arguments:**
- `sprint-id` - Sprint identifier
- `task-id` - Task to move
- `position` - Target position (0-based). Must be an integer between 0 and 2147483647 (MaxInt32) inclusive. If position >= task count, task is moved to the end. The value is a rank in the sprint's planned order, not a raw column value: it means "the *n*-th task of this sprint", counting from zero. It coincides with the `sprint_tasks.position` the command reads because a sprint's positions are dense (see `DATABASE.md § Position Density Within a Sprint`).

**Behavior:**
- Moving UP: Tasks between new position and current position-1 shift down by 1
- Moving DOWN: Tasks between current position+1 and new position shift up by 1
- Moving to same position: No-op
- Moving to position >= task count: Task is placed at the end

**JSON Output (success):** A JSON success object is written to stdout, exit code 0. The `position` field reflects the requested position:

```json
{
  "success": true,
  "sprint_id": 1,
  "task_id": 5,
  "position": 3
}
```

**Error Output:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 4 | "Sprint not found" |
| Task not in sprint | 6 | "Task N is not in sprint" |
| Invalid position | 6 | "Error: validation error: position must be an integer between 0 and 2147483647" |

#### Swap Tasks

```bash
rmp sprint swap -r <name> <sprint-id> <task-id-1> <task-id-2>
```

**Description:** Swaps the positions of two tasks within a sprint.

**Arguments:**
- `sprint-id` - Sprint identifier
- `task-id-1` - First task to swap
- `task-id-2` - Second task to swap

**Behavior:**
- Both tasks must belong to the same sprint
- Positions are exchanged between the two tasks
- No changes to other tasks

**JSON Output (success):** A JSON success object is written to stdout, exit code 0:

```json
{
  "success": true,
  "sprint_id": 1,
  "task_id_1": 5,
  "task_id_2": 3
}
```

**Error Output:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 4 | "Sprint not found" |
| Task not in sprint | 6 | "Task N is not in sprint" |
| Same task ID | 6 | "Cannot swap a task with itself" |

#### Move Task to Top/Bottom

```bash
rmp sprint top -r <name> <sprint-id> <task-id>
rmp sprint bottom -r <name> <sprint-id> <task-id>
```

**Description:** Quick commands to move a task to the beginning (top) or end (bottom) of the sprint task list.

**Arguments:**
- `sprint-id` - Sprint identifier
- `task-id` - Task to move

**Behavior:**
- `top`: Equivalent to `move-to <task-id> 0`
- `bottom`: Equivalent to `move-to <task-id> <task_count>`

**JSON Output (success):** No output, exit code 0.

**Error Output:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 4 | "Sprint not found" |
| Task not in sprint | 6 | "Task N is not in sprint" |

#### Audit of the ordering commands

The five ordering commands write one entry per invocation, against the sprint, with
NULL `related_entity_id` and NULL `commit_hash`:

| Command | Operation written |
|---------|-------------------|
| `sprint reorder` | `SPRINT_REORDER_TASKS` |
| `sprint move-to`, `sprint top`, `sprint bottom` | `SPRINT_TASK_MOVE_POSITION` |
| `sprint swap` | `SPRINT_TASK_SWAP` |

**These commands write no `TASK_STATUS_*` entry and no entry against any task.**
Ordering changes the `position` column of the `sprint_tasks` membership rows; it
changes no task's status and no task's own fields, so the sprint is the only entity
whose state changes and the sprint is the only entity the audit log records it
against. A no-op move (moving a task to the position it already holds) still writes
its entry, on the same rule that governs `task edit`: the audit log records the
command issued, not the delta it produced.

### Update Sprint

```bash
rmp sprint update -r <name> <id> [-t "New Title"] [-d "New Description"] [--max-tasks <n>] [--order <n>]
rmp sprint upd -r <name> <id> [-t "New Title"] [-d "New Description"] [--max-tasks <n>] [--order <n>]
```

**Options:**
- `-t, --title <text>` - New sprint title, maximum 255 characters
- `-d, --description <text>` - New sprint description, maximum 2048 characters. The
  new value carries exactly the same semantics as on creation: it MUST state the
  high-level (macro) goal of the development effort the sprint delivers (a new
  development, a fix, a refactoring, or another kind of change), and together with
  the title it MUST give a human reader or an AI agent a clear macro idea of what
  the sprint's tasks are specifically aimed at. See
  `MODELS.md § Sprint Field Constraints` for the canonical definition of the field.
- `--max-tasks <n>` - New capacity limit. MUST be a positive integer in the range
  `1`-`10000`. A value `< 1` or `> 10000`, or a non-integer value, is rejected with
  exit code 6.
- `--order <n>` - New sprint execution order. MUST be a positive integer strictly
  greater than zero (`> 0`) and MUST NOT already be used by another sprint. The
  order can be changed only while the sprint is `PENDING` or `OPEN`; once the
  sprint is `CLOSED`, its order is immutable and any change is rejected with exit
  code 6 (see `STATE_MACHINE.md § Sprint Order Immutability`).

At least one of `--title`, `--description`, `--max-tasks`, or `--order` is required.
This requirement counts the flags the invocation supplies, not the values they
carry: a flag supplied with an empty value is still a supplied flag, so it satisfies
the requirement and then faces the validation rules below. An invocation that
supplies none of the four is the only one this requirement rejects, with exit code 2
and this message:

`Error: required parameter missing: at least one of --title, --description, --max-tasks or --order is required`

So `rmp sprint update -r <name> <id> -t ""` never produces that message: the flag is
present, the requirement is met, and Title Validation then rejects the empty value
with exit code 6. Presence rather than value is the same criterion the audit entries
apply (see `Audit` below).

**Title Validation:**

When `--title` is provided, it is validated before updating:

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Title is empty, or empty once trimmed | 6 | "Error: validation error: title cannot be empty" |
| Title exceeds 255 characters | 6 | "Error: field exceeds maximum size: title exceeds maximum length of 255 characters" |

The sprint `title` is also subject to the Control-Character Constraint, the UTF-8
Encoding Constraint, and the Emptiness Constraint described in `Field Validation`
above.

**Description Validation:**

When `--description` is provided, it is validated before updating:

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Description is empty, or empty once trimmed | 6 | "Error: validation error: description cannot be empty" |

The sprint `description` is also subject to the Control-Character Constraint, the
UTF-8 Encoding Constraint, and the Emptiness Constraint described in
`Field Validation` above, and to the 2048-character maximum stated under `Options`
above.

These exit codes differ from `Create Sprint` on purpose: there `--title` and
`--description` are required parameters, so the literal empty string counts as the
parameter being missing (exit code 2), whereas here they are optional flags that must
carry a non-empty value when supplied, so the literal empty string is a rejected value
(exit code 6).

**That difference is confined to the literal empty string.** A value that carries text
and is empty only once trimmed — a value made only of whitespace — is a rejected value
on `sprint create` and `sprint update` alike, with exit code 6 and the same message,
because the caller did supply text and the text names nothing. See
`Emptiness Constraint (All Required Free-Text Fields)` under `Field Validation`
above.

**Bound Validation:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| `--max-tasks` `< 1` or `> 10000` | 6 | "Error: validation error: max_tasks must be between 1 and 10000, got N" |
| `--max-tasks` non-integer | 2 | "Error: invalid input: invalid value for --max-tasks: strconv.Atoi: parsing \"X\": invalid syntax" |
| `--order` `<= 0` | 6 | "Error: validation error: --order must be a positive integer greater than zero (got N)" |
| `--order` non-integer | 6 | "Error: validation error: --order must be a positive integer greater than zero" |
| `--order` on a CLOSED sprint | 6 | "Error: validation error: sprint #N order cannot be changed — sprint is CLOSED" |
| `--order` already used by another sprint | 5 | "Error: resource already exists: sprint order N is already in use" |

**Output (success):** No output, exit code 0.

**Audit:** One entry per field the invocation supplies. An invocation that supplies
N fields writes N entries. At least one field is required (see above), so every
successful invocation writes at least one entry.

| Flag supplied | Operation written |
|---------------|-------------------|
| `-t, --title` | `SPRINT_TITLE_CHANGE` |
| `-d, --description` | `SPRINT_DESCRIPTION_CHANGE` |
| `--max-tasks` | `SPRINT_MAX_TASKS_CHANGE` |
| `--order` | `SPRINT_ORDER_CHANGE` |

Every entry carries `entity_type = SPRINT` with the updated sprint's id, a NULL
`related_entity_id`, and a NULL `commit_hash`. All entries of one invocation share
one `performed_at` value and are written in the same transaction as the `UPDATE`, so
a rejected update writes no entry at all.

**The trigger is the presence of the flag, not a difference in value**, exactly as on
`task edit`: the command compares no supplied value against the stored one, so an
invocation supplying a field whose value equals the stored value still writes that
field's entry.

**`sprint update` writes no `SPRINT_UPDATE` entry.** That operation is LEGACY (see
`DATABASE.md § audit Table`).

**Acceptance criteria:**

1. `rmp sprint update -r <name> <id> -t "New" --max-tasks 12` writes exactly two entries, one `SPRINT_TITLE_CHANGE` and one `SPRINT_MAX_TASKS_CHANGE`, sharing one `performed_at`.
2. `rmp sprint update -r <name> <id> --order 3` writes exactly one `SPRINT_ORDER_CHANGE` entry.
3. An update rejected by any validation rule, including an `--order` collision (exit code 5), writes zero entries.
4. No invocation of `sprint update` writes `SPRINT_UPDATE`.
5. `rmp sprint update -r <name> <id> -t ""` exits 6 with `Error: validation error: title cannot be empty`, and `rmp sprint update -r <name> <id> -d ""` exits 6 with `Error: validation error: description cannot be empty`. Neither reports a missing parameter, because both supply a flag.
6. `rmp sprint update -r <name> <id> -t "   "` and `rmp sprint update -r <name> <id> -d "   "` produce the same two refusals as criterion 5, leave the stored value unchanged, and write zero entries.
7. `rmp sprint update -r <name> <id> -t "" -d "New description"` exits 6, changes neither field, and writes zero entries: the empty `--title` is rejected before any field is written.
8. `rmp sprint update -r <name> <id>` with none of the four flags is the only invocation that exits 2 with the at-least-one-flag message.

### Remove Sprint

```bash
rmp sprint remove -r <name> <id>
rmp sprint rm -r <name> <id>
```

**Description:** Removes a sprint and handles its associated tasks.

**Task Behavior on Sprint Removal:**

When a sprint is removed, all tasks currently associated with it are automatically returned to the backlog:

| Current Task Status | New Status | Sprint membership |
|---------------------|------------|-------------------|
| BACKLOG | BACKLOG | `sprint_tasks` row deleted |
| SPRINT | BACKLOG | `sprint_tasks` row deleted |
| DOING | BACKLOG | `sprint_tasks` row deleted |
| TESTING | BACKLOG | `sprint_tasks` row deleted |
| COMPLETED | BACKLOG | `sprint_tasks` row deleted |

A member task can already be in `BACKLOG` status before the sprint is removed (see `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status`); for such a task the status write is a no-op and only the membership row goes away.

**Process:**
1. Validate sprint ID exists
2. For each task in the sprint:
   - Set status to BACKLOG (regardless of current status)
   - Clear `started_at`, `tested_at`, `closed_at`, `completion_summary`, and `commit_close` to NULL
   - Preserve all other fields (title, requirements, priority, severity, `commit_open`, etc.)
3. Delete sprint_tasks junction table entries
4. Delete sprint from sprints table
5. Log the `SPRINT_DELETE` operation in the audit log

**Rationale:**
- Prevents data loss by preserving task content
- Tasks return to backlog for re-prioritization and re-assignment
- No automatic deletion of tasks (user must explicitly delete tasks if desired)
- Clear audit trail of the cascade operation

**Audit:** Exactly one `SPRINT_DELETE` entry, against the deleted sprint, with NULL
`related_entity_id` and NULL `commit_hash`, written in the same transaction as the
cascade (see `DATABASE.md § Transactional Atomicity Guarantees`). The command writes
no per-task entry: unlike `sprint remove-tasks`, which names one task per entry, a
deletion resets every member at once and the sprint an entry would name no longer
exists when the transaction commits. The entry outlives the sprint, so the audit log
keeps the record that the sprint existed and was deleted.

**Output (success):** No output, exit code 0.

**Error Cases:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Sprint not found | 4 | "Error: resource not found: sprint N not found" |
| Roadmap not specified | 3 | "Error: no roadmap selected: use -r <name> or --roadmap <name>" |

---

### Sprint Comments

A sprint comment is a durable, typed log entry attached to a sprint. Sprint comments record only the progression of the work during the sprint's development: findings, decisions taken, progress, and the reason behind a change to the sprint's definition. Work carried out inside one task belongs in that task's own comments, not here.

The four subcommands below are flat subcommands of the `sprint` family, in the form `sprint comment-<verb>`, and they mirror the four task comment subcommands exactly (see `COMMANDS.md § Task Comments`). Each takes exactly one positional argument, and refuses a second one with exit code 2, under the same `Comment Positional Argument Contract` above. Two differences apply throughout:

- **The accepted type set is smaller.** A sprint comment accepts `FINDING`, `DECISION`, `PROGRESS`, and `UPDATE`. The task-only values `HYPOTHESIS`, `TEST`, and `NOTE` are rejected with exit code 6.
- **The id space is separate.** A comment id here identifies a row in `sprint_comments`. The same number in the `task` family identifies an unrelated row in `task_comments`.

`-y, --type` has no other meaning in the `sprint` family: unlike the `task` family, where the same flag also carries a `TaskType` value on `list`, `create`, and `edit`, the only `sprint` subcommands that accept `-y, --type` are the four below, and the flag always means a comment type.

Comments are accepted in every sprint status, including `CLOSED`. No comment subcommand checks or changes a sprint's status.

#### Add Sprint Comment

```bash
rmp sprint comment-add -r <name> <sprint-id> --type <TYPE> --body "<text>"
rmp sprint c-add -r <name> <sprint-id> -y <TYPE> -b "<text>"

# Body read from standard input when --body is absent
rmp sprint comment-add -r <name> <sprint-id> --type DECISION < decision.txt
```

**Description:** Adds one comment to the given sprint. The comment is stored with its type, its body, and a creation timestamp; `updated_at` starts null.

**Aliases:** `c-add`

**Arguments:**
- `sprint-id` - Sprint ID (required, positive integer)
- Exactly one positional argument is accepted. A second or later positional argument is refused with exit code 2 and the message `Error: invalid input: unexpected argument "X"`; the comment is not added (see `Comment Positional Argument Contract` above).

**Options:**
- `-r, --roadmap <name>` - REQUIRED. Target roadmap.
- `-y, --type <TYPE>` - REQUIRED. Comment type. One of `FINDING`, `DECISION`, `PROGRESS`, `UPDATE`. See `MODELS.md § Comment Type` for the canonical list and the meaning of each value.
- `-b, --body <text>` - Comment text, maximum 4096 characters. When absent, the body is read from standard input under the bounded read (see `Comment Body Input Source and Precedence` above).

**Validation Rules:**

| Field | Constraint | Error Message (stderr) | Exit Code |
|-------|------------|------------------------|-----------|
| `sprint-id` | Integer in `1`-`2147483647` (see `Comment Positional Argument Contract`) | "Error: invalid input: invalid sprint ID: \"X\" (must be a positive integer)" | 2 |
| `type` | Present | "Error: required parameter missing: --type" | 2 |
| `type` | One of the four sprint values | "Error: validation error: invalid comment type \"X\" for a sprint comment; valid types: FINDING, DECISION, PROGRESS, UPDATE" | 6 |
| `body` | Supplied via `--body` or stdin | "Error: required parameter missing: no comment body supplied" | 2 |
| `body` | Max 4096 chars | "Error: field exceeds maximum size: body exceeds maximum length of 4096 characters" | 6 |
| `body` | Valid UTF-8 | "Error: validation error: body: the value is not valid UTF-8" | 6 |
| `body` | No forbidden control characters | "Error: validation error: body: control characters are not allowed" | 6 |

**Validation Order:** identical to `task comment-add`, with the sprint in place of the task: roadmap, then `sprint-id` format, then the flags — an unrecognised flag or a leftover positional argument fails here with exit code 2 — then `--type` presence, then the type value against the four sprint values, then the body, then the sprint's existence, then the body's length, then its encoding, then its control characters, then the insert and its audit entry in one transaction.

**JSON Output:** `{"id": 4}` — the id of the created comment. Exit code 0.

**Error Conditions:**

| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Sprint not found | 4 | `Error: resource not found: sprint N not found` |
| Roadmap not specified | 3 | `Error: no roadmap selected: use -r <name> or --roadmap <name>` |
| Roadmap not found | 4 | `Error: resource not found: roadmap "X"` |
| Invalid sprint ID format | 2 | `Error: invalid input: invalid sprint ID: "X" (must be a positive integer)` |
| Missing sprint ID | 2 | `Error: required parameter missing: sprint ID required` |
| Extra positional argument | 2 | `Error: invalid input: unexpected argument "X"` |
| Unknown flag | 2 | `Error: invalid input: unknown flag: --foo` |
| Database failure | 1 | `Error: database error: <detail>` |

**Audit:** Logged as `SPRINT_COMMENT_CREATE` against the parent sprint (`entity_type = SPRINT`, `entity_id = <sprint-id>`), in the same transaction as the insert. See `DATABASE.md § audit Table`.

#### List Sprint Comments

```bash
rmp sprint comment-list -r <name> <sprint-id> [--type <TYPE>]
rmp sprint c-ls -r <name> <sprint-id> [-y <TYPE>]
```

**Description:** Returns every comment of the given sprint, oldest first.

**Aliases:** `c-ls`

**Arguments:**
- `sprint-id` - Sprint ID (required, positive integer)
- Exactly one positional argument is accepted. A second or later positional argument is refused with exit code 2 and the message `Error: invalid input: unexpected argument "X"`; no listing is produced (see `Comment Positional Argument Contract` above).

**Options:**
- `-r, --roadmap <name>` - REQUIRED. Target roadmap.
- `-y, --type <TYPE>` - Optional filter. Returns only the comments whose type equals `<TYPE>`. The value MUST be one of the four sprint values; any other value, including a valid task-only value, is rejected with exit code 6.

**Ordering:** `created_at` ascending, with the comment `id` ascending as the tie-breaker.

**Result-set size:** Unbounded. There is no `--limit` flag, no `--desc` flag, and no pagination.

**JSON Output:** Array of SprintComment objects (see `DATA_FORMATS.md § Sprint Comment`). An empty array `[]` when the sprint has no comments, or none of the requested type. Exit code 0.

**Error Conditions:**

| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Sprint not found | 4 | `Error: resource not found: sprint N not found` |
| Invalid `--type` value | 6 | `Error: validation error: invalid comment type "X" for a sprint comment; valid types: FINDING, DECISION, PROGRESS, UPDATE` |
| Invalid sprint ID format | 2 | `Error: invalid input: invalid sprint ID: "X" (must be a positive integer)` |
| Missing sprint ID | 2 | `Error: required parameter missing: sprint ID required` |
| Extra positional argument | 2 | `Error: invalid input: unexpected argument "X"` |
| Roadmap not specified | 3 | `Error: no roadmap selected: use -r <name> or --roadmap <name>` |

**Audit:** None. Listing is a read and writes no audit entry.

#### Edit Sprint Comment

```bash
rmp sprint comment-edit -r <name> <comment-id> [--type <TYPE>] [--body "<text>"]
rmp sprint c-edit -r <name> <comment-id> [-y <TYPE>] [-b "<text>"]

# New body read from standard input (no --type given)
rmp sprint comment-edit -r <name> <comment-id> < revised.txt
```

**Description:** Changes the type and/or the body of one existing sprint comment, identified by the comment's own id. At least one of `--type` and `--body` is required.

**Aliases:** `c-edit`

**Arguments:**
- `comment-id` - Comment ID (required, positive integer). This is the comment's id, **not** the id of the sprint it belongs to.
- Exactly one positional argument is accepted. A second or later positional argument is refused with exit code 2 and the message `Error: invalid input: unexpected argument "X"`; the comment is not changed (see `Comment Positional Argument Contract` above).

**Options:**
- `-r, --roadmap <name>` - REQUIRED. Target roadmap.
- `-y, --type <TYPE>` - New comment type. One of the four sprint values.
- `-b, --body <text>` - New comment text, maximum 4096 characters. The standard-input rules are those of `task comment-edit`: standard input is read only when `--type` is also absent.

**Replacement semantics:** identical to `task comment-edit`. The edit replaces the stored body in place and stamps `updated_at`; the previous text is not recoverable.

**Validation Rules:**

| Field | Constraint | Error Message (stderr) | Exit Code |
|-------|------------|------------------------|-----------|
| `comment-id` | Integer in `1`-`2147483647` (see `Comment Positional Argument Contract`) | "Error: invalid input: invalid comment ID: \"X\" (must be a positive integer)" | 2 |
| change | At least one change requested: a `--type` value, a `--body` value, or a body on standard input | "Error: required parameter missing: at least one of --type or --body is required" | 2 |
| `type` | One of the four sprint values | "Error: validation error: invalid comment type \"X\" for a sprint comment; valid types: FINDING, DECISION, PROGRESS, UPDATE" | 6 |
| `body` | `--body` present but empty or whitespace only | "Error: required parameter missing: no comment body supplied" | 2 |
| `body` | Max 4096 chars | "Error: field exceeds maximum size: body exceeds maximum length of 4096 characters" | 6 |
| `body` | Valid UTF-8 | "Error: validation error: body: the value is not valid UTF-8" | 6 |
| `body` | No forbidden control characters | "Error: validation error: body: control characters are not allowed" | 6 |

**Validation Order:** identical to `task comment-edit`, resolving the comment in `sprint_comments` and validating the type against the four sprint values. An unrecognised flag or a leftover positional argument fails with exit code 2 at the same point, before the type value is validated and before standard input is read. At least one change is required, counting a body on standard input as a change; requesting none is exit code 2.

**Output (success):** No output, exit code 0.

**Error Conditions:**

| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Comment not found | 4 | `Error: resource not found: sprint comment N not found` |
| Roadmap not specified | 3 | `Error: no roadmap selected: use -r <name> or --roadmap <name>` |
| Invalid comment ID format | 2 | `Error: invalid input: invalid comment ID: "X" (must be a positive integer)` |
| Missing comment ID | 2 | `Error: required parameter missing: comment ID required` |
| Extra positional argument | 2 | `Error: invalid input: unexpected argument "X"` |
| Database failure | 1 | `Error: database error: <detail>` |

**Audit:** Logged as `SPRINT_COMMENT_UPDATE` against the parent sprint (`entity_type = SPRINT`, `entity_id` = the id of the sprint the comment belongs to), in the same transaction as the update.

#### Remove Sprint Comment

```bash
rmp sprint comment-remove -r <name> <comment-id>
rmp sprint c-rm -r <name> <comment-id>
```

**Description:** Deletes one sprint comment, identified by the comment's own id. The row is removed outright; the audit entry survives it.

**Aliases:** `c-rm`

**Arguments:**
- `comment-id` - Comment ID (required, positive integer). This is the comment's id, **not** the id of the sprint it belongs to.
- Exactly one positional argument is accepted. A second or later positional argument is refused with exit code 2 and the message `Error: invalid input: unexpected argument "X"`; nothing is deleted (see `Comment Positional Argument Contract` above).

**Options:**
- `-r, --roadmap <name>` - REQUIRED. Target roadmap.

**Single-id command:** `comment-remove` takes exactly one comment id, in the two senses `task comment-remove` states: the id accepts no comma-separated list, and it is the only positional argument the command takes, so the command deletes either exactly one comment or none at all.

**Output (success):** No output, exit code 0.

**Error Conditions:**

| Scenario | Exit Code | stderr |
|----------|-----------|--------|
| Comment not found | 4 | `Error: resource not found: sprint comment N not found` |
| Roadmap not specified | 3 | `Error: no roadmap selected: use -r <name> or --roadmap <name>` |
| Invalid comment ID format | 2 | `Error: invalid input: invalid comment ID: "X" (must be a positive integer)` |
| Missing comment ID | 2 | `Error: required parameter missing: comment ID required` |
| Extra positional argument | 2 | `Error: invalid input: unexpected argument "X"` |
| Database failure | 1 | `Error: database error: <detail>` |

**Audit:** Logged as `SPRINT_COMMENT_DELETE` against the parent sprint (`entity_type = SPRINT`, `entity_id` = the id of the sprint the comment belonged to), in the same transaction as the delete.

---

## Audit Log Management

Command: `rmp audit` (alias: `aud`)

### List Audit Log

```bash
rmp audit list -r <name> [OPTIONS]
rmp audit ls -r <name>
```

**Options:**
- `-o, --operation <name>` - Filter by audit operation name (for example
  `TASK_CREATE`, `TASK_STATUS_DOING`, `SPRINT_CLOSE`). The value MUST be one of the
  operations in the canonical catalogue (see `DATABASE.md § audit Table`); any other
  value is rejected with exit code 6, whether or not the `audit` table happens to
  hold rows carrying it. The accepted set is exactly the catalogue, **including its
  four LEGACY operations** — `TASK_STATUS_CHANGE`, `TASK_UPDATE`, `SPRINT_UPDATE`,
  and `SPRINT_MOVE_TASK`. No command writes those four, so on a roadmap whose
  entries were all written at schema 1.12.0 or later each of them matches nothing and
  the command returns `[]` with exit code 0; on an older roadmap they return the
  entries the migration left carrying them. The filter matches by equality only:
  there is no pattern search and no prefix search, so a caller who wants every status
  change issues one read per `TASK_STATUS_*` value, or reads unfiltered and selects
  from the result.
- `-e, --entity-type <type>` - Filter by entity (TASK, SPRINT). Any other value is
  rejected with exit code 6. There is no filter on `related_entity_id` and none on
  `commit_hash`: both are returned on every entry and neither is a predicate (see
  `DATABASE.md § Query Audit Entries`)
- `--entity-id <id>` - Filter by specific entity ID. MUST be a positive integer in
  the range `1`-`2147483647` (`MaxInt32`). A value `< 1` or `> 2147483647` is
  rejected with exit code 6; a non-integer value is rejected with exit code 2.
- `--since <date>` - Inclusive lower bound on `performed_at`. The value takes one
  of two forms: a full RFC3339 timestamp, including its offset and sub-second
  variants (`2026-01-01T00:00:00Z`, `2026-01-01T00:00:00.000Z`,
  `2026-01-01T00:00:00+00:00`), or a bare calendar date `YYYY-MM-DD`
  (`2026-01-01`), which denotes the **first instant of that day in UTC**. These
  are the same two forms `task list --created-since` and
  `task list --created-until` accept (see `§ List Tasks`): one acceptance rule
  governs every date-range filter the CLI publishes, so no date-range filter
  accepts a value another one refuses. A value in neither form is rejected with
  exit code 6.
- `--until <date>` - Inclusive upper bound on `performed_at`, in the same two
  forms and under the same acceptance rule as `--since`. A bare calendar date on
  this bound denotes the first instant of that day in UTC, not the last instant:
  `--until 2026-01-01` selects entries performed at exactly
  `2026-01-01T00:00:00.000Z` and nothing later in that day. To cover a whole day,
  supply the explicit timestamp `2026-01-01T23:59:59.999Z`.
- `-l, --limit <n>` - Limit the number of results. MUST be a positive integer in
  the range `1`-`500`. The maximum is the server-side cap `MaxAuditLimit` (500;
  see `DATABASE.md § Audit Result Limit`). A value `< 1` or `> 500` is rejected
  with exit code 6; a non-integer value is rejected with exit code 2.

**Bound Validation:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| `--limit` `< 1` or `> 500` | 6 | "Error: validation error: limit must be between 1 and 500, got N" |
| `--limit` non-integer | 2 | "Error: invalid input: invalid limit: X" |
| `--entity-id` `< 1` or `> 2147483647` | 6 | "Error: validation error: entity_id must be between 1 and 2147483647, got N" |
| `--entity-id` non-integer | 2 | "Error: invalid input: invalid entity ID: X" |
| `-o, --operation` not one of the catalogue operations | 6 | "Error: validation error: invalid audit operation: \"X\"" |
| `-e, --entity-type` not `TASK` or `SPRINT` | 6 | "Error: validation error: invalid entity type: \"X\"" |
| Invalid `--since` date format | 6 | "Error: validation error: --since: invalid date format: expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01): \"X\"" |
| Invalid `--until` date format | 6 | "Error: validation error: --until: invalid date format: expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01): \"X\"" |

A value out of range and a value that is not an integer at all are two conditions, not one: the first reaches the range check and is a validation failure (exit 6), while the second fails to parse and is malformed input (exit 2). The two messages differ accordingly.

**JSON Output:** Array of AuditEntry objects. Every object carries all seven keys,
including `related_entity_id` and `commit_hash`, which are `null` on the operations
that do not use them (see `DATA_FORMATS.md § Audit Entry`).

### Entity History

```bash
rmp audit history -r <name> <entity-type> <entity-id>
rmp audit hist -r <name> <entity-type> <entity-id>
```

**Description:** Shows all audit entries related to a specific task or sprint.
Equivalent to `rmp audit list -r <name> -e <entity-type> --entity-id <entity-id>`
without pagination.

**What a task's history now contains.** Because every entity an operation touches
receives its own entry, `audit history TASK <id>` shows the task entering and leaving
a sprint (`TASK_STATUS_SPRINT`, `TASK_STATUS_BACKLOG`) as well as the transitions
`task stat` performed. Each such entry names its counterpart in `related_entity_id`:
a sprint on the entries a membership change wrote, the other task of the pair on a
dependency entry, and NULL on a transition `task stat` performed, which has no
counterpart. A task's history is therefore self-contained — it says which sprint the
task joined and left without a second query. The mirrored entries —
`SPRINT_ADD_TASK` and `SPRINT_REMOVE_TASK` — belong to the sprint's history and are
reached with `audit history SPRINT <sprint-id>`. See
`DATABASE.md § The Two Entities of a Relational Operation`.

**Arguments (both positional, in this order):**
- `<entity-type>` - First positional. MUST be `TASK` or `SPRINT`. Any other value
  is rejected with exit code 6. There is no `-e` flag form for this command: a
  leading `-e` is parsed as the entity-type value and fails with
  `Error: validation error: invalid entity type: "-e"`.
- `<entity-id>` - Second positional. Entity identifier. MUST be an integer in the
  range `1`-`2147483647` (`MaxInt32`).

**Validation:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| `<entity-type>` is not TASK or SPRINT | 6 | "Error: validation error: invalid entity type: \"X\"" |
| `<entity-id>` is not an integer | 2 | "Error: invalid input: invalid entity ID: \"X\" (must be a positive integer)" |
| `<entity-id>` `< 1` or `> 2147483647` | 6 | "Error: validation error: entity_id must be between 1 and 2147483647, got N" |

A non-integer entity id is a format/misuse error (exit code 2, `EXIT_MISUSE`); an
integer that is out of the valid range is a validation error (exit code 6). Both
bounds of the range are one rule and produce one sentence, and this positional and
the `--entity-id` flag of `audit list` name the same field and print the same
line — the two commands are the same query (see
`§ Entity Identifier Range (All Positional Ids and --entity-id)`).

**JSON Output:** Array of AuditEntry objects, with the same seven keys `audit list`
returns.

### Audit Statistics

```bash
rmp audit stats -r <name> [--since <date>] [--until <date>]
```

**Description:** Returns aggregated statistics about audit log entries for the specified roadmap. Optional date filters allow narrowing the statistics to a specific time period.

**Options:**
- `--since <date>` - Inclusive lower bound on `performed_at`, in either of the two
  forms every date-range filter the CLI accepts: a full RFC3339 timestamp,
  including its offset and sub-second variants (`2026-01-01T00:00:00Z`,
  `2026-01-01T00:00:00.000Z`, `2026-01-01T00:00:00+00:00`), or a bare calendar
  date `YYYY-MM-DD` (`2026-01-01`), which denotes the **first instant of that day
  in UTC**. These are the same two forms `audit list --since/--until` and
  `task list --created-since/--created-until` accept: one acceptance rule governs
  every date-range filter the CLI publishes. A value in neither form is rejected
  with exit code 6. If omitted, includes all entries from the beginning.
- `--until <date>` - Inclusive upper bound on `performed_at`, in the same two
  forms and under the same acceptance rule. A bare calendar date on this bound
  denotes the first instant of that day in UTC, not the last instant, so
  `--until 2026-01-01` excludes every entry performed later in that day. If
  omitted, includes all entries up to now.

**Error Conditions:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Roadmap not specified | 3 | "Error: no roadmap selected: use -r <name> or --roadmap <name>" |
| Invalid `--since` date format | 6 | "Error: validation error: --since: invalid date format: expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01): \"X\"" |
| Invalid `--until` date format | 6 | "Error: validation error: --until: invalid date format: expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01): \"X\"" |
| Roadmap not found | 4 | "Error: resource not found: roadmap \"X\"" |

The command checks these conditions in the order the table lists them: a missing
`-r` is refused before any flag value is read, both date bounds are parsed before
the roadmap database is opened, and `--since` is parsed before `--until`. An
invocation that names a roadmap that does not exist and also supplies a value in
neither accepted date form therefore exits 6, not 4.

**JSON Output:**
```json
{
  "by_operation": {
    "TASK_CREATE": 15,
    "TASK_STATUS_DOING": 7,
    "TASK_STATUS_TESTING": 6,
    "TASK_STATUS_COMPLETED": 6,
    "SPRINT_ADD_TASK": 11
  },
  "by_entity_type": {
    "TASK": 36,
    "SPRINT": 18
  },
  "first_entry_at": "2026-06-03T09:15:46.656Z",
  "last_entry_at": "2026-06-03T09:15:47.522Z",
  "total_entries": 54
}
```

**Field Descriptions:**

| Field | Type | Description |
|-------|------|-------------|
| `by_operation` | map[string]int | Count of entries per operation type, keyed by the full operation name (for example `TASK_CREATE`, `TASK_STATUS_DOING`, `SPRINT_ADD_TASK`). Each operation is its own key: the five `TASK_STATUS_*` operations are never summed into one bucket, and a LEGACY operation is counted under its own name whenever entries carrying it exist. An operation with no entries has no key. See `DATABASE.md § audit Table` for the operation catalogue |
| `by_entity_type` | map[string]int | Count of entries per entity type (`TASK`, `SPRINT`) |
| `first_entry_at` | string (ISO 8601 UTC) or null | Timestamp of the oldest audit entry among the filtered entries, or null when no entries match |
| `last_entry_at` | string (ISO 8601 UTC) or null | Timestamp of the newest audit entry among the filtered entries, or null when no entries match |
| `total_entries` | int | Total number of audit log entries matching the filter criteria |

**Behavior:**
- The `--since` and `--until` filters are applied to the audit entries before aggregation; all counts and timestamps reflect only the entries that pass the filter
- The command does not echo the requested period back in the output; there is no `period` object
- `first_entry_at` and `last_entry_at` are the oldest and newest timestamps among the filtered entries, not the filter bounds
- Empty result set (no entries match the filter) returns: `{"by_operation": {}, "by_entity_type": {}, "first_entry_at": null, "last_entry_at": null, "total_entries": 0}`
- All timestamps are in ISO 8601 UTC format

---

## Backlog Management

Command: `rmp backlog` (alias: `bl`)

**Description:** Dedicated commands for managing and querying tasks in the backlog. All subcommands filter exclusively on tasks with `status == BACKLOG`.

### List Backlog Tasks

```bash
rmp backlog list -r <name> [OPTIONS]
rmp backlog ls -r <name> [OPTIONS]
```

**Description:** Returns all tasks with status `BACKLOG`, with optional filters and sorting.

**Options:**
- `-r, --roadmap <name>` - Roadmap name (required if no default)
- `-p, --priority <min>` - Filter by minimum priority value (inclusive)
- `-y, --type <type>` - Filter by task type. Valid values: `USER_STORY`, `TASK`, `BUG`, `SUB_TASK`, `EPIC`, `REFACTOR`, `CHORE`, `SPIKE`, `DESIGN_UX`, `IMPROVEMENT` (see `MODELS.md` — Task Type for the canonical enum)
- `--sort <field>` - Sort order: `priority` (default), `created`, `status`, `severity`
- `-l, --limit <n>` - Maximum number of tasks to return

**JSON Output:** Array of Task objects (same format as `rmp task list`).

**Error Conditions:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| `--limit` `< 1` or `> 100` | 6 | `Error: validation error: limit must be between 1 and 100, got N` |
| Invalid `--type` value | 6 | `Error: validation error: invalid task type: "X"` |

An invalid `--type` value is a validation error and MUST exit with code 6,
consistent with `rmp task list` (see List Tasks above). This is the canonical,
required behaviour for the command.

`--limit` carries the same bound and the same refusal as `rmp task list`,
because a backlog listing is a task listing (see `§ List Tasks`).

**Examples:**
```bash
rmp backlog list -r groadmap
rmp backlog list --priority 7 -r groadmap
rmp backlog list --type BUG -r groadmap
rmp backlog list --sort priority -r groadmap
rmp backlog ls --limit 20 -r groadmap
```

### Show Next Backlog Tasks

```bash
rmp backlog show-next [count] -r <name>
```

**Description:** Returns the top N backlog tasks ordered by priority descending (highest priority first) for sprint planning purposes. This is a convenience shortcut equivalent to `backlog list --sort priority --limit <count>`.

**Arguments:**
- `count` - Number of tasks to return (default: 5, max: 100)

**Options:**
- `-r, --roadmap <name>` - Roadmap name (required if no default)

**JSON Output:** Array of Task objects ordered by priority descending.

**Examples:**
```bash
rmp backlog show-next -r groadmap
rmp backlog show-next 5 -r groadmap
rmp backlog show-next 10 -r groadmap
```

**Error Conditions:**

| Condition | Exit Code | Message |
|-----------|-----------|---------|
| Roadmap not found | 4 | `Error: resource not found: roadmap "X"` |
| Roadmap not specified | 3 | `Error: no roadmap selected: use -r <name> or --roadmap <name>` |
| `count` is not a positive integer | 6 | `Error: validation error: count must be a positive integer` |
| Extra positional argument | 2 | `Error: invalid input: unexpected argument "X"` |

`backlog show-next` takes no `--type` and no `--sort`. It accepts one optional
positional `count` and the roadmap flags, and nothing else: a `--type` or `--sort`
written before the `count` position is read as the `count` value and refused as a
non-positive integer. A second positional argument is refused exactly as an excess
positional argument is refused everywhere else in the CLI (`§ Positional Arguments`),
with the exit code and the error line the table above publishes. `show-next` claims
no exception here: a token the command accepted in silence would be a token the
reader wrote believing it had an effect. The filtering and sorting conditions belong
to `backlog list` above, whose own table states them.

---

## Statistics Command

Command: `rmp stats`

**Description:** Provides comprehensive statistics about a roadmap, including sprint and task distribution.

### Get Roadmap Statistics

```bash
rmp stats --roadmap <name>
rmp stats -r <name>
```

**Options:**
- `-r, --roadmap <name>` - Roadmap name (required)

**JSON Output:**
```json
{
  "roadmap": "project-name",
  "sprints": {
    "current": 5,
    "total": 12,
    "completed": 9,
    "pending": 2
  },
  "tasks": {
    "backlog": 15,
    "sprint": 8,
    "doing": 5,
    "testing": 3,
    "completed": 42
  },
  "average_velocity": 2.5
}
```

**Output Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `roadmap` | string | Name of the roadmap |
| `sprints.current` | integer or null | ID of the currently open sprint, or null if no sprint is open |
| `sprints.total` | integer | Total number of sprints in the roadmap |
| `sprints.completed` | integer | Number of sprints with status CLOSED |
| `sprints.pending` | integer | Number of sprints with status PENDING (created but never started) |
| `tasks.backlog` | integer | Number of tasks with status BACKLOG |
| `tasks.sprint` | integer | Number of tasks with status SPRINT |
| `tasks.doing` | integer | Number of tasks with status DOING |
| `tasks.testing` | integer | Number of tasks with status TESTING |
| `tasks.completed` | integer | Number of tasks with status COMPLETED |
| `average_velocity` | float64 | Average tasks completed per day across the last 5 closed sprints. Each sprint's daily rate uses a sprint duration floored at a minimum of 1 day: `duration_days = max(1.0, (closed_at - started_at) in days)`. 0.0 when no qualifying closed sprints exist |

**Error Cases:**

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Roadmap not specified | 3 | "Error: no roadmap selected: use -r <name> or --roadmap <name>" |
| Roadmap not found | 4 | "Error: resource not found: roadmap \"X\"" |

**Behavior Notes:**
- The `sprints.current` field returns the ID of the sprint with status OPEN, or `null` if no sprint is currently open
- The `sprints.pending` field counts sprints with status PENDING (created but never started)
- The `sprints.completed` field counts sprints with status CLOSED
- `sprints.total` may exceed `sprints.completed` plus `sprints.pending` because the currently open sprint is counted in `sprints.current` and `sprints.total` but not in `sprints.completed` or `sprints.pending`
- The sum of all task statuses equals the total number of tasks in the roadmap
- `average_velocity` is computed from the last 5 closed sprints that have both `started_at` and `closed_at` set. Each sprint's daily completion rate divides its completed-task count by `max(1.0, (closed_at - started_at) in days)`, so a sprint that starts and closes within the same day contributes its completed-task count rather than an inflated value. Sprints with zero completed tasks contribute 0.0. Returns 0.0 when no qualifying sprints exist

---

## Graph Management

Command: `rmp graph` (no alias)

The `graph` command operates a roadmap's knowledge graph: a free-form, queryable
store of the project's elements and the relationships between them, backed by the
GoGraph engine. The design, persistence layout, and multi-layer conventions are
specified in `GRAPH.md`. This section is the CLI contract for the command.

Each roadmap owns one graph, stored under that roadmap's home directory at
`~/.roadmaps/<name>/graph/` (a directory, mode `0700`). The graph is created on
first use of the `graph` command. The graph is independent of the roadmap's
SQLite tasks and sprints data in this version.

`graph` has three subcommands. `execute` and `client` each accept any Cypher
statement the engine accepts and run it; they differ only in where the statement
runs. `serve` runs no statement of its own: it makes the graph available to the
other two.

| Subcommand | Operation | Accepts |
|------------|-----------|---------|
| `execute` | Run a Cypher statement against the roadmap's graph | Any statement the engine accepts: reads, writes, deletions, schema DDL, and schema introspection alike |
| `serve` | Serve the roadmap's graph over a Unix domain socket until stopped | No statement. It takes a roadmap and, optionally, a socket path |
| `client` | Send a Cypher statement to a running server and print its result | Any statement the engine accepts, exactly as `execute` does |

**There is no operation-class check and there are no aliases.** `rmp graph`
publishes exactly three subcommand names, `execute`, `serve` and `client`.
`create`, `query`, `update`, `delete`, and `search` are not subcommand names of
`rmp graph`: each is an unresolved subcommand and is answered as a dispatch
failure — exit code `127`, the `graph` help on stderr, nothing on stdout (see
`§ Dispatch Failures (Unresolved Command or Subcommand Names)`). They are named
here because an agent that has one of them in memory needs to be told, in the
specification, that it will not resolve.

**A running server changes where a statement executes, and nothing else.** When a
server is serving the selected roadmap, `rmp graph execute` sends its statement to
that server instead of opening the store itself, and so does the web interface's
graph data endpoint; with no server listening, both open the store directly, as
they always have. The statement, the result, the output shape and the exit code
are the same either way. `GRAPH.md § Server Resolution` fixes the rule, its four
states, and the outcome each surface reports for each state. What a caller gains
from starting a server is throughput and latency — one store open instead of one
per invocation, and concurrent sessions the store's MVCC resolves — not a
different contract.

**No flag chooses between the two paths; one flag chooses which socket is
looked at.** All three subcommands that reach a graph take `--socket <path>`, and
all three default it to the same path derived from the roadmap. The flag names the
socket the invocation resolves — it does not force a server, does not forbid one,
and does not select the direct path. Whether the statement ends up at a server is
decided by what answers on the path in force, and by nothing the caller writes.

**`rmp graph client` is not `rmp graph execute` with a socket.** It speaks to a
server and only to a server. Against a roadmap with no server listening it fails;
it never falls back to opening the store, because a subcommand that did would
report a success that says nothing about whether a server was reached (see
`GRAPH.md § The Bolt Client`).

**`execute` runs what it is given, and the caller owns what that does.** No
subcommand's contract says that a statement cannot delete. A statement's effect is
decided by its Cypher and by nothing `rmp` inspects, so the guarantee an agent
needs about a statement is a guarantee about the text it supplies.
`GRAPH.md § What Groadmap Does Not Check` enumerates the hazards that follow, each
of which reports success.

**`execute` runs what it is given, but not for as long as it takes.** The
statement runs under a time budget of **5 seconds** — the same budget, carrying
the same value, that the web graph data endpoint applies. A statement that
exhausts it is cancelled; its transaction rolls back whole, no snapshot is
written, the write-ahead log is left as the statement found it, and the command
fails with exit code 1 and the budget line this section's error table publishes.
This is a limit on what a caller may run, and it is published here for that
reason: a statement whose work takes longer than five seconds fails, however
valid its Cypher is and however healthy the store, and the remedy is to narrow it
— a label, an indexed property filter, or a `LIMIT` — or to split it into smaller
statements. `GRAPH.md § Statement Time Budget` is canonical for what a cut
statement leaves behind, and `WEB.md § Graph Query Time Budget` for the value.

### Execute Options

- `-r, --roadmap <name>` - REQUIRED. Target roadmap (see
  `COMMANDS.md § Roadmap Selection (Always Required)`).
- `-q, --query <cypher>` - The Cypher statement to run. When omitted, the
  statement is read from standard input under a bound; it is not read to EOF.
- `--socket <path>` - Path of the graph server socket this invocation resolves.
  Default `~/.roadmaps/<name>/graph.sock`, derived from the selected roadmap and
  identical to the derivation `graph serve` and `graph client` use. The flag
  changes **which socket is looked at**, never what happens next: a server
  answering there takes the statement, and a path that is absent or refuses the
  connection sends the invocation to the store under the exclusive lock, exactly
  as before the flag existed (see `GRAPH.md § Server Resolution`). Supplying the
  flag with an empty value is a missing parameter (exit code 2). It is the flag
  that lets the CLI follow a server started with `--socket`; the web graph data
  endpoint has no equivalent and cannot (see
  `GRAPH.md § Serving on a Non-Default Socket`).
- `-h, --help` - Show the subcommand help.

**Query input source and precedence.** The statement has exactly two sources,
`--query` and standard input, and omitting `--query` selects the second. Every
rule over those sources is specified in
`GRAPH.md § Cypher Input Source and Precedence`, which is canonical for it: which
source wins, the maximum query length and the bounded read that enforces it, what
happens when no statement is supplied at all, and the refusal of a statement
written as a positional argument instead of through either source. This section
does not restate those rules. It restated them once, and the copy contradicted the
original the day the original changed, which is the outcome
`README.md § 3. Canonical Sources` exists to prevent.

### Execute Output

- On success the output mirrors what the executed statement returns. When the
  statement produces result columns, the output is the `{columns, rows}` shape
  defined in `DATA_FORMATS.md § Graph Query Result`; when it produces none, the
  output is exactly `{"ok": true}`. For a data-writing statement the two cases are
  exactly "has a `RETURN` clause" and "has none". A schema-introspection command
  produces columns while carrying no `RETURN` clause, and therefore returns the
  `{columns, rows}` shape; a `CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, or
  `DROP CONSTRAINT` produces no columns and returns `{"ok": true}`. There is no
  affected-element count, because the engine reports none. Exit code 0. The shape
  is fixed in `DATA_FORMATS.md § Graph Write Result`.
- Side effect of a statement that wrote: after committing, the invocation
  produces an on-disk snapshot under `~/.roadmaps/<name>/graph/snapshot/` and
  truncates the write-ahead log, synchronously, before exit (see
  `GRAPH.md § Synchronous Checkpoint on Write`). A statement whose transaction
  appended nothing to the write-ahead log neither snapshots nor truncates. A
  snapshot failure after a durable commit does not change the success output or
  the exit code; it is reported as a diagnostic on stderr while the command still
  exits 0.
- Query notifications: the subcommand surfaces, as a plain-text diagnostic line
  per notification on stderr, exactly the advisory notifications the engine
  returns for the executed statement (for example a Cartesian-product warning on a
  disconnected multi-pattern `MATCH`). The engine alone decides which statements
  carry notifications, so a statement may produce none. Notifications do not
  change the stdout success output or the exit code, and when the engine returns
  none the subcommand writes nothing extra to stderr (see
  `GRAPH.md § Query Notifications as Diagnostics`).
- Errors: plain text to stderr, with the standard AI-agent hint.

### Execute Exit Codes

| Exit Code | Cause |
|-----------|-------|
| 0 | The statement executed successfully. |
| 1 | Cypher failed to parse or execute, or the graph store could not be opened, read, or written (`utils.ErrDatabase`). A schema statement the engine refuses is in this class, including one whose keyword spacing the engine does not route to its schema parser. See `GRAPH.md § Schema Failure Classes`. |
| 1 | The statement exhausted the 5-second statement time budget and was cancelled (`utils.ErrDatabase`). Nothing was written: the transaction rolled back, no snapshot was produced, and the write-ahead log was left unchanged. See `GRAPH.md § Statement Time Budget`. |
| 2 | No statement supplied: `--query` absent and standard input empty, whitespace only, or a terminal; or `--query` present with an empty, whitespace-only, or absent value; or `--socket` supplied with an empty value (`utils.ErrRequired`). |
| 2 | A positional argument was supplied. `graph execute` accepts none, so a bare Cypher statement on the command line, or any other token that is neither a flag nor a flag's value, is refused (`utils.ErrInvalidInput`). See `GRAPH.md § No Positional Query: A Stray Token Is Refused`. |
| 3 | No roadmap selected and none provided via `-r` (`utils.ErrNoRoadmap`). |
| 4 | Selected roadmap does not exist (`utils.ErrNotFound`). |
| 6 | The statement is longer than the maximum query length of 1 MiB (1048576 bytes), whether it arrived through `--query` or through standard input (`utils.ErrValidation`). See `GRAPH.md § Maximum Query Length`. This is the only cause of exit code 6 the command has. |
| 1 | The roadmap's socket answers, but no server could be reached through it within the resolution probe, or the connection failed for a reason other than the socket being absent or refusing (`utils.ErrDatabase`). The store was not opened and no lock was taken. See `GRAPH.md § Server Resolution`. |
| 1 | The connection to a server was lost after the statement had been sent (`utils.ErrDatabase`). Whether the statement committed is unknown, and the invocation does not retry it against the store. See `GRAPH.md § Server Resolution`, rule 4. |
| 1 | Every attempt of the retry policy lost a serialisation conflict against a server (`utils.ErrDatabase`). Nothing was written: a losing transaction commits nothing. The statement is valid and may be run again. See `GRAPH.md § Concurrency Inside the Server`. |

A socket file with nothing listening behind it is **not** in that table, because it
is not a failure: the invocation reads the refused connection as evidence that the
roadmap is not served, opens the store directly, and exits 0 (see
`GRAPH.md § Server Resolution`, rule 1).

The canonical exit-code catalogue is in `ARCHITECTURE.md § Exit Codes`; the graph
feature introduces no new codes.
`ARCHITECTURE.md § Exit Codes of the Graph Server and Client` enumerates the codes
`serve` and `client` can return.

### Execute

```bash
rmp graph execute -r <name> --query "<cypher>"
echo "<cypher>" | rmp graph execute -r <name>
```

**Description:** Runs one Cypher statement against the roadmap's knowledge graph
and returns its result. A statement that changes the graph runs inside a single
transaction and is persisted durably before the process exits — or, when a graph
server is serving the roadmap, it is sent to that server and persisted there.

**Where the statement runs is resolved, not chosen.** The invocation resolves the
socket in force — the default derived from the roadmap, or the value of
`--socket` — and sends the statement to whatever server answers there. With
nothing answering, it opens the store itself under the exclusive lock, which is
what every invocation did before a server existed, and the paragraphs below
describe that path. `GRAPH.md § Server Resolution` is canonical for the rule.
`--socket` is written only when the server was started with it; an ordinary
invocation against an ordinary server needs no flag at all.

**Reading:**

```bash
rmp graph execute -r backend-platform \
  --query "MATCH (s:Spec)-[:IMPLEMENTED_BY]->(c:Code) RETURN s.key, c.path"
```

Output (success): JSON in the shape defined in
`DATA_FORMATS.md § Graph Query Result`, for example:

```json
{
  "columns": ["s.key", "c.path"],
  "rows": [
    ["user-authentication", "internal/auth/jwt.go"]
  ]
}
```

**Writing:**

```bash
rmp graph execute -r backend-platform \
  --query "MERGE (s:Spec {key:'user-authentication'}) MERGE (c:Code {path:'internal/auth/jwt.go'}) MERGE (s)-[:IMPLEMENTED_BY]->(c)"
rmp graph execute -r backend-platform \
  --query "MATCH (s:Spec {key:'user-authentication'}) SET s.status = 'implemented'"
rmp graph execute -r backend-platform \
  --query "MATCH (d:Decision {key:'use-sessions'}) DETACH DELETE d"
```

Output (success): `{"ok": true}`, exit code 0. None of the three carries a
`RETURN` clause, so none produces result columns. Appending `RETURN` to any of
them (for example `... RETURN s`) returns the affected elements in the
`{columns, rows}` shape instead (see `DATA_FORMATS.md § Graph Write Result`).

**Traversal:**

```bash
rmp graph execute -r backend-platform \
  --query "MATCH path = (s:Spec {key:'user-authentication'})-[:DEPENDS_ON*1..3]->(d:Dependency) RETURN path"
```

**Managing the schema:**

```bash
rmp graph execute -r backend-platform \
  --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"
rmp graph execute -r backend-platform --query "SHOW INDEXES"
rmp graph execute -r backend-platform --query "DROP INDEX spec_key"
```

The `CREATE INDEX` and `DROP INDEX` invocations output `{"ok": true}` and exit 0.
The `SHOW INDEXES` invocation outputs the schema listing in the `{columns, rows}`
shape and exits 0. `GRAPH.md § Schema Management` is canonical for the schema
statements: which forms the engine accepts, how a schema object is named, why
changing an index is two invocations rather than one, and how a schema failure
surfaces.

### Execute Error Cases

| Scenario | Exit Code | stderr Output (illustrative) |
|----------|-----------|------------------------------|
| Roadmap not specified | 3 | "Error: no roadmap selected: use -r <name> or --roadmap <name>" |
| Roadmap not found | 4 | "Error: resource not found: roadmap \"X\" not found" |
| No statement supplied | 2 | "Error: required parameter missing: no query supplied" |
| `--socket` supplied with an empty value | 2 | "Error: required parameter missing: --<flag>" |
| Stray positional argument, such as a bare Cypher statement written without `--query` | 2 | "Error: invalid input: unexpected argument \"X\" (graph queries use --query or stdin)" |
| Statement above the maximum length | 6 | "Error: validation error: query exceeds maximum length of 1048576 bytes" |
| Cypher parse/execution error | 1 | "Error: database error: graph query failed: <engine diagnostic>" |
| Statement cancelled for exhausting the 5-second statement time budget | 1 | "Error: database error: graph query exceeded the 5s statement time budget; nothing was written. Narrow the statement — add a label, an indexed property filter, or a LIMIT — or split it into smaller statements." |
| Graph store open/read/write failure | 1 | "Error: database error: graph store unavailable: <detail>" |
| A server could not be reached through a socket that answered | 1 | The unreachable line of `§ Graph Server Socket Error Lines` |
| Connection to a server lost after the statement was sent | 1 | The lost-connection line of `§ Graph Server Socket Error Lines` |
| A server did not answer within the caller's backstop deadline | 1 | The unanswered line of `§ Graph Server Socket Error Lines` |
| Every attempt of the retry policy lost a serialisation conflict on a served roadmap | 1 | "Error: database error: graph write conflict: another writer committed first on every attempt within the 2.5s retry budget; nothing was written. The statement is valid — run it again, and spread concurrent writes across distinct nodes." |

The last four rows arise only against a roadmap a graph server is serving; a
socket file with nothing listening behind it produces none of them, because the
invocation reads it as evidence that the roadmap is not served and opens the
store. The conflict row is the one of the four that is not about the socket: it
reports contention inside the server, which is unreachable on the direct path
because a direct invocation runs exactly one transaction
(`GRAPH.md § Concurrency Inside the Server`).

The parse/execution row and the store-failure row end in a diagnostic the Cypher engine produces, not `rmp`. The part `rmp` fixes is everything up to and including `graph query failed: ` and `graph store unavailable: `; what follows is the engine's own text and is not specified here.

The budget row is not one of them: it carries no engine diagnostic and no placeholder, and every character of it is `rmp`'s own text, so it is compared in full. `5s` is the budget itself, rendered as a duration; it is a fixed value and not a value the binary interpolates from the invocation. The line says the three things a caller who has just lost a statement needs: what was exceeded, that nothing was written, and what to do next. `GRAPH.md § Statement Time Budget` is canonical for the behaviour it reports.

The conflict row is not one of them either, and for the same reason: it carries no engine diagnostic and no placeholder, every character of it is `rmp`'s own text, and it is compared in full. `2.5s` is the retry policy's total wait, rendered as a duration; it is a fixed value and not one the binary interpolates. The line exists because the condition it reports was otherwise indistinguishable from the parse/execution row above — both printed the same `graph query failed: ` text, and the only thing separating them was the engine's diagnostic tail, which the paragraph above deliberately declines to specify and which a caller therefore cannot lawfully match. The decision a caller must make on reading it is the opposite of the one an invalid statement calls for: run the statement again, rather than correct it. `GRAPH.md § Concurrency Inside the Server` is canonical for the behaviour it reports.

### Graph Server Socket Error Lines

Seven failure conditions belong to the socket rather than to the roadmap, the
statement, or the store, and three of this section's error tables refer here for
their exact lines instead of each publishing a copy. Every line is complete, as
`§ Published Error Strings Are Exact` requires, and every one carries
`utils.ErrDatabase` and exit code 1 (`GRAPH.md § Error Handling and Exit Codes`).
`<socket>` is the resolved socket path and `<detail>` is the operating system's
own diagnostic; the placeholder table under `§ Published Error Strings Are Exact`
declares both.

- **A live server already answers on the socket `graph serve` resolved.** The line
  is `Error: database error: a graph server is already serving <socket>`. The
  incumbent's socket is left exactly as it was found, and the incumbent keeps
  serving (`GRAPH.md § Server Startup`, step 3).
- **`graph serve` could not bind its socket.** The line is
  `Error: database error: cannot bind <socket>: <detail>`. The part `rmp` fixes is
  everything up to and including `cannot bind <socket>: `; the text after it is
  the operating system's, exactly as it is on the two bind rows of
  `§ Web Interface`.
- **`graph serve` could not take the roadmap's graph store lock within the bounded
  wait.** The line is
  `Error: database error: cannot take the graph store lock for roadmap "X": another rmp graph serve may already be running for it`.
  This is what refuses a second server against the same roadmap, and the line says
  so rather than reporting an unavailable store, because a second server is the
  overwhelmingly likely cause and the reader can act on it. It says "may": the
  lock records no holder, so the invocation reports the likely cause and does not
  assert it (`GRAPH.md § Server Startup`, step 2).
- **`graph client` found no server listening.** The line is
  `Error: database error: no graph server is listening on <socket>`. It covers
  both the socket that does not exist and the socket file a killed server left
  behind, because the two are one condition for this subcommand: there is nothing
  to send the statement to. `graph client` does not open the store
  (`GRAPH.md § The Bolt Client`).
- **A server could not be reached through a socket that answered.** The line is
  `Error: database error: graph server unreachable at <socket>: <detail>`. This is
  the `Unreachable` state of `GRAPH.md § Server Resolution`: the connection was
  accepted but the handshake did not complete inside the probe, or it failed for a
  reason other than the socket being absent or refusing. `graph execute` reports
  it and does **not** fall back to the store, because a socket that answers may
  belong to a server holding the lock.
- **The connection was lost after the statement had been sent.** The line is
  `Error: database error: the connection to the graph server at <socket> was lost; the statement's outcome is unknown`.
  It is deliberately not a claim that nothing was written: a commit is durable
  before it is acknowledged, so a connection lost between the two leaves the
  outcome genuinely unknown, and a line that said "nothing was written" would be
  false in exactly the case a caller most needs the truth
  (`GRAPH.md § Server Resolution`, rule 4).
- **The server did not answer within the caller's backstop deadline.** The line is
  `Error: database error: the graph server at <socket> did not answer within 7.5s; the statement's outcome is unknown`.
  The connection is intact here and the server is alive; it is simply not
  answering, which is what a statement the budget cut mid-write looks like from
  outside, because the engine's undo replay runs past the deadline by a factor
  nothing bounds (`GRAPH.md § Statement Time Budget`). The outcome is unknown for
  the same reason the previous line's is, and the caller does not fall back to the
  store. `7.5s` is the backstop itself rendered as a duration, a fixed value and
  not one the binary interpolates (`GRAPH.md § Server Resolution`, rule 7).

### Serve Options

- `-r, --roadmap <name>` - REQUIRED. Target roadmap (see
  `COMMANDS.md § Roadmap Selection (Always Required)`). The server serves this one
  roadmap's graph and no other.
- `--socket <path>` - Path of the Unix domain socket to bind. Default
  `~/.roadmaps/<name>/graph.sock`, derived from the selected roadmap. Supplying
  the flag with an empty value is a missing parameter (exit code 2); supplying a
  path that cannot be bound is a bind failure (exit code 1). A non-default path is
  followed by the two CLI subcommands that take the same flag, `graph execute` and
  `graph client`, and by nothing else: the web graph data endpoint has no way to
  receive it, resolves the default path, finds nothing there, and fails against
  the running server's lock for as long as it runs.
  `GRAPH.md § Serving on a Non-Default Socket` is canonical for that boundary.
- `-h, --help` - Show the subcommand help.

`rmp graph serve` accepts no positional argument; `§ Positional Arguments` is
canonical for the refusal and for the line it writes. It takes no `--query`: it
runs no statement of its own.

The socket is created with mode `0600`, set explicitly rather than left to the
process umask, and it sits inside a roadmap home directory that is `0700`. Access
control is the filesystem and there is no other: the server authenticates nobody,
so a caller that can open the socket can read, write, delete, and change the
schema of that roadmap's graph. `GRAPH.md § Socket Path and Permissions` is
canonical for the path, the mode, and the access model.

### Serve Output

- **On successful startup (stdout):** a single JSON object naming the socket the
  server bound, so the path is machine-readable even when the caller supplied
  none:

  ```json
  {"socket": "/home/user/.roadmaps/backend-platform/graph.sock"}
  ```

  The `socket` value is the absolute path actually bound, whether it came from the
  default derivation or from `--socket`. The object is pretty-printed with
  two-space indentation and a trailing newline, consistent with all other JSON
  output (see `DATA_FORMATS.md § Implementation Notes`).
- **While running:** the server answers Bolt sessions on the socket. Per-statement
  results go to the client that asked for them, not to the command's stdout.
- **Diagnostics (stderr):** structured records, and **not** the plain text this
  command's own error line uses. Every diagnostic the running server writes is a
  `log/slog` record rendered as a single line of `key=value` pairs, and every one
  of them carries a `time` attribute in the project's canonical form,
  `YYYY-MM-DDTHH:mm:ss.sssZ` — always UTC, three digits of milliseconds, an
  explicit `Z`, whatever zone the machine is set to
  (`DATA_FORMATS.md § Dates - ISO 8601 with UTC`). The records the engine
  produces are written by this handler too, so they carry the same timestamp;
  `rmp` does not merely relay them.
  `GRAPH.md § Server Diagnostics on Stderr` is canonical for the whole stream:
  what is fixed about a record, what is deliberately not, and the limit that
  makes this a diagnostic stream rather than an audit log — under sustained load
  the server drops records rather than block on a destination that has stopped
  reading them, and reports how many it dropped.
- **The two startup warnings (stderr):** one for a server running without
  transport security and one for a server running without authentication. Both
  are expected and neither is a failure; `GRAPH.md § Socket Path and Permissions`
  explains why both states are the intended ones. Both are written **before** the
  socket object appears on stdout, so a caller that reads the path and then reads
  stderr finds them there.
- **Errors (stderr):** plain text, with the standard AI-agent hint, per
  `HELP.md § Error message format`. This is the invocation's own failure line
  rather than a log record, and `§ Serve Error Cases` publishes the lines
  themselves.

### Serve Exit Codes

These are the exit codes of the `rmp graph serve` **process**. A statement a
client sends fails or succeeds inside a session and never changes them.

| Exit Code | Cause |
|-----------|-------|
| 0 | The server started, served, and was stopped by `SIGINT` or `SIGTERM`. It drained, checkpointed, released the store lock, and removed its socket. |
| 1 | The graph store could not be opened or recovered; or its exclusive lock could not be taken within the bounded wait, which is what refuses a second server against the same roadmap; or the socket could not be bound; or a live server already answers on the resolved socket (`utils.ErrDatabase`). |
| 2 | Unknown flag, or an unexpected positional argument (`utils.ErrInvalidInput`); or `--socket` supplied with an empty value (`utils.ErrRequired`). |
| 3 | No roadmap selected and none provided via `-r` (`utils.ErrNoRoadmap`). |
| 4 | Selected roadmap does not exist (`utils.ErrNotFound`). |

The canonical exit-code catalogue is in `ARCHITECTURE.md § Exit Codes`, and
`ARCHITECTURE.md § Exit Codes of the Graph Server and Client` states why a
graceful stop is 0 rather than 130. The graph server introduces no new codes.

### Serve

**Usage:** `rmp graph serve -r <name> [--socket <path>]`

**Description:** Opens the roadmap's knowledge graph once, holds it and its
advisory store lock for the life of the process, and answers Cypher statements
over a Unix domain socket until it is stopped. The protocol is Bolt version 5,
served by the engine's own server; Groadmap defines no protocol of its own.

`rmp graph serve` is long-lived. It is one of two commands whose process keeps
running rather than completing a single operation and exiting, the other being
`rmp web`. Sending `SIGINT` (`Ctrl+C`) or `SIGTERM` drains the work in flight,
shuts the server down, checkpoints, releases the lock, removes the socket, and
exits 0. `GRAPH.md § Server Shutdown and the Drain` is canonical for what the
drain guarantees and, as importantly, for what it does not.

**One server per roadmap.** The roadmap's store lock is the interlock: a second
`rmp graph serve` against the same roadmap cannot take it, fails, and leaves the
first server's socket untouched. A server on a socket some *other* roadmap's
server already owns is refused by the socket probe instead.
`GRAPH.md § Server Startup` fixes the order in which the two checks run and why
that order is the one that keeps the incumbent's socket safe.

**What the server does not do.** It runs no statement of its own, it creates no
graph directory that does not already exist, and it never reads or writes a
roadmap's `project.db`. It serves one roadmap; serving several means running
several servers, one per roadmap, each on its own socket.

### Serve Error Cases

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Roadmap not specified | 3 | "Error: no roadmap selected: use -r <name> or --roadmap <name>" |
| Roadmap not found | 4 | "Error: resource not found: roadmap \"X\" not found" |
| Unknown flag | 2 | "Error: invalid input: unknown flag: --foo" |
| Unexpected positional argument | 2 | "Error: invalid input: unexpected argument \"X\"" |
| `--socket` supplied with an empty value | 2 | "Error: required parameter missing: --<flag>" |
| Graph store lock could not be taken within the bounded wait | 1 | The lock line of `§ Graph Server Socket Error Lines` |
| A live server already answers on the resolved socket | 1 | The already-serving line of `§ Graph Server Socket Error Lines` |
| Socket could not be bound | 1 | The bind line of `§ Graph Server Socket Error Lines` |
| Graph store open/recovery failure | 1 | "Error: database error: graph store unavailable: <detail>" |

### Client Options

- `-r, --roadmap <name>` - REQUIRED. Target roadmap (see
  `COMMANDS.md § Roadmap Selection (Always Required)`). It selects the graph the
  statement runs against and, unless `--socket` overrides it, the socket the
  statement is sent to.
- `--socket <path>` - Path of the server's Unix domain socket. Default
  `~/.roadmaps/<name>/graph.sock`, the same derivation `graph serve` uses.
  Supplying the flag with an empty value is a missing parameter (exit code 2).
- `-q, --query <cypher>` - The Cypher statement to send. When omitted, the
  statement is read from standard input under a bound; it is not read to EOF.
  There is no `--statement` flag: the statement reaches `client` through exactly
  the two sources it reaches `execute` through.
- `-h, --help` - Show the subcommand help.

**Query input source and precedence.** The statement has the same two sources
`execute` has, under the same rules, and
`GRAPH.md § Cypher Input Source and Precedence` is canonical for every one of
them: which source wins, the maximum query length and the bounded read that
enforces it, what happens when no statement is supplied at all, and the refusal of
a statement written as a positional argument instead of through either source.
This section does not restate them.

### Client Output

- On success the output is byte-for-byte the output `rmp graph execute` produces
  for the same statement against the same graph: the `{columns, rows}` shape when
  the statement produces result columns, and exactly `{"ok": true}` when it
  produces none. `DATA_FORMATS.md § Graph Client Result` is canonical for the
  mapping that makes the two identical, and
  `DATA_FORMATS.md § Graph Query Result` and
  `DATA_FORMATS.md § Graph Write Result` remain canonical for the shapes
  themselves.
- Query notifications: the subcommand surfaces, as one plain-text diagnostic line
  per notification on stderr, the advisory notifications the server returns for
  the executed statement, exactly as `execute` surfaces the ones the engine
  returns to it (see `GRAPH.md § Query Notifications as Diagnostics`).
- A retriable serialisation conflict is **not** an error and is not printed. It is
  retried, and only an exhausted retry policy or an exhausted statement budget
  produces a failure (see `GRAPH.md § Concurrency Inside the Server`). Each of the
  two failures prints its own line: `§ Client Error Cases` publishes both, and
  neither is the parse/execution line.
- Errors: plain text to stderr, with the standard AI-agent hint.

### Client Exit Codes

| Exit Code | Cause |
|-----------|-------|
| 0 | The statement was sent to a server, ran, and its result was written to stdout. |
| 1 | No server is listening for the roadmap; or a server could not be reached through the socket; or the connection was lost, or went unanswered, after the statement was sent; or the statement failed to parse or execute in the engine; or it exhausted the 5-second statement time budget; or every attempt of the retry policy lost a serialisation conflict; or a value the server returned could not be mapped onto the published result shape (`utils.ErrDatabase`, see `DATA_FORMATS.md § Graph Client Result`, rule 3). |
| 2 | No statement supplied: `--query` absent and standard input empty, whitespace only, or a terminal; or `--query` present with an empty, whitespace-only, or absent value; or `--socket` supplied with an empty value (`utils.ErrRequired`). |
| 2 | A positional argument was supplied. `graph client` accepts none, exactly as `graph execute` accepts none (`utils.ErrInvalidInput`). |
| 3 | No roadmap selected and none provided via `-r` (`utils.ErrNoRoadmap`). |
| 4 | Selected roadmap does not exist (`utils.ErrNotFound`). |
| 6 | The statement is longer than the maximum query length of 1 MiB (1048576 bytes), whether it arrived through `--query` or through standard input (`utils.ErrValidation`). |

The canonical exit-code catalogue is in `ARCHITECTURE.md § Exit Codes`, and
`ARCHITECTURE.md § Exit Codes of the Graph Server and Client` states why a socket
with nothing behind it is 1 rather than 4. The graph client introduces no new
codes.

### Client

**Usage:** `rmp graph client -r <name> -q "<cypher>"`, or
`rmp graph client -r <name>` with the statement on standard input. Add
`--socket <path>` when the server was started with one.

**Description:** Sends one Cypher statement to a running graph server over its
Unix domain socket and prints the result. It reads and writes alike: the server
does not examine the statement any more than `execute` does, so a statement that
creates, changes, deletes, or alters the schema is executed and committed.

**It requires a server.** With none listening, the invocation fails; it does not
open the store. That is the whole difference between this subcommand and
`execute`, which resolves the same socket but has a second path to fall back on
(`GRAPH.md § The Bolt Client`).

**A serialisation conflict is retried, not reported.** Two clients writing to the
same nodes at the same time is an ordinary situation inside a server, and the
store detects the collision rather than preventing it. The losing statement
committed nothing, so the client re-sends it, under the project's retry policy,
and reports a failure only when that policy or the statement budget is exhausted
(`GRAPH.md § Concurrency Inside the Server`).

**When the retry policy is exhausted, the failure says so in its own words.** The
line `§ Client Error Cases` publishes for it is `rmp`'s own text from end to end:
it names the contention, states that nothing was written, and names the remedy.
That matters because the alternative was silence: an exhausted retry once printed
the same `graph query failed: ` line as an invalid statement, and the caller's
next move differs completely between the two. The remedy the line names is to run
the statement again, and to spread concurrent writes across distinct nodes, which
is what removes the collisions rather than moving the point at which they start
(`GRAPH.md § Concurrency Inside the Server`, rule 8).

**The statement runs under the same 5-second time budget** `execute` runs under.
The server is the end that enforces it, and the client keeps a later deadline of
its own purely as a backstop against a server that answers nothing, so that a
statement which committed just before the budget expired is never reported as one
that wrote nothing. A statement the budget genuinely cut fails with exit code 1
and the budget line `§ Execute Error Cases` publishes;
`GRAPH.md § Server Resolution`, rule 7, is canonical for the two deadlines and for
why they differ.

### Client Error Cases

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Roadmap not specified | 3 | "Error: no roadmap selected: use -r <name> or --roadmap <name>" |
| Roadmap not found | 4 | "Error: resource not found: roadmap \"X\" not found" |
| No statement supplied | 2 | "Error: required parameter missing: no query supplied" |
| `--socket` supplied with an empty value | 2 | "Error: required parameter missing: --<flag>" |
| Stray positional argument, such as a bare Cypher statement written without `--query` | 2 | "Error: invalid input: unexpected argument \"X\" (graph queries use --query or stdin)" |
| Statement above the maximum length | 6 | "Error: validation error: query exceeds maximum length of 1048576 bytes" |
| No server listening on the resolved socket | 1 | The no-server line of `§ Graph Server Socket Error Lines` |
| A server could not be reached through a socket that answered | 1 | The unreachable line of `§ Graph Server Socket Error Lines` |
| Connection lost after the statement was sent | 1 | The lost-connection line of `§ Graph Server Socket Error Lines` |
| The server did not answer within the backstop deadline | 1 | The unanswered line of `§ Graph Server Socket Error Lines` |
| Cypher parse/execution error reported by the server | 1 | "Error: database error: graph query failed: <engine diagnostic>" |
| Statement cancelled for exhausting the 5-second statement time budget | 1 | "Error: database error: graph query exceeded the 5s statement time budget; nothing was written. Narrow the statement — add a label, an indexed property filter, or a LIMIT — or split it into smaller statements." |
| Every attempt of the retry policy lost a serialisation conflict | 1 | "Error: database error: graph write conflict: another writer committed first on every attempt within the 2.5s retry budget; nothing was written. The statement is valid — run it again, and spread concurrent writes across distinct nodes." |

The parse/execution row carries the engine's own diagnostic after
`graph query failed: `, exactly as the same row does under
`§ Execute Error Cases`: the statement ran in the engine either way, and the
diagnostic the caller reads is the engine's in both.

The conflict row is the one that used to share it. A serialisation conflict
whose every attempt collided was reported through the parse/execution row above,
so the two conditions printed the same line and the only text separating them
was the engine's diagnostic tail — which is outside this file's contract and
which a caller therefore cannot match. It now has a line of its own, published
identically here and under `§ Execute Error Cases`, because both subcommands
reach it through the same client against the same server
(`GRAPH.md § The Bolt Client`). Every character of that line is `rmp`'s own and
it is compared in full.

---

## Web Interface

Command: `rmp web` (no alias)

The `web` command starts a browser-based view of the data the CLI manages. It runs
an HTTP server embedded in the `rmp` binary (Go standard-library `net/http`) that
serves server-rendered HTML and embedded static assets, and it reads the same
on-disk data under `~/.roadmaps/` that the CLI reads. Every page the server
renders is read-only, and the server never writes to a roadmap's `project.db`.

**The knowledge graph is the exception, and it is not a small one.** The graph
page's query bar submits caller-supplied Cypher to the graph data endpoint, which
executes it against the roadmap's graph store without examining it, so a request
to that one endpoint can create, change, and delete graph data, and can change the
graph's schema. The endpoint requires no authentication, and `rmp web` offers
none. `WEB.md § Security and Constraints` states the consequence in full, and
anyone binding a non-loopback address should read it before doing so.

The full behaviour of the running server — routes, pages, the data flow, the
interactive knowledge-graph visualisation, and the security model — is specified
in `WEB.md`. This section is the command-line contract.

`rmp web` operates across all roadmaps. The web interface lists every roadmap
found under `~/.roadmaps/` and the user drills into one from the browser, so
`rmp web` does **not** require and does **not** accept the `-r` / `--roadmap`
flag (see [Roadmap Selection (Always Required)](#roadmap-selection-always-required)).

`rmp web` has no subcommands.

```bash
rmp web
rmp web --port 9000
rmp web --host 127.0.0.1 --port 9000
rmp web --no-open
```

### Options

- `--host <address>` - Bind host. Default `127.0.0.1` (loopback only), so the
  interface is reachable only from the local machine. Exposing the
  interface on the network is the explicit opt-in `--host 0.0.0.0` (binds all
  interfaces), or any other non-loopback address. When a non-loopback host is
  bound, the server prints a warning to stderr that the interface is reachable
  from the network (see `WEB.md § Bind Address and Port Selection` and
  `WEB.md § Security and Constraints`).
- `--port <number>` - Bind port, an integer in the range 0-65535. Default `8787`.
  When `--port` is omitted and the default port `8787` is already in use, the
  server falls back to an operating-system-chosen ephemeral port so it still
  starts. When `--port` is given explicitly, there is no fallback: a port that
  cannot be bound is a bind error. `--port 0` requests an ephemeral port
  explicitly. The chosen port is reported in the served URL.
- `--no-open` - Do not launch a browser. The server still starts and prints the
  served URL. Default behaviour (without this flag) is to open the user's default
  browser at the served URL; a failed browser launch is not fatal.
- `-h, --help` - Show the command help.

`rmp web` accepts no positional arguments; `§ Positional Arity by Command` publishes
the declared maximum of zero. An unexpected positional argument and an unknown flag
are both input errors (exit code 2), and `Error Cases` below publishes the line the
command writes for each.

### Output

- **On successful startup (stdout):** a single JSON object naming the URL the
  server is listening on, so the address is machine-readable even when no browser
  is opened:

  ```json
  {"url": "http://127.0.0.1:8787"}
  ```

  The `url` reflects the actual bound host and port, including an ephemeral port
  chosen by the fallback. The object is pretty-printed with two-space indentation
  and a trailing newline, consistent with all other JSON output (see
  `DATA_FORMATS.md § Implementation Notes`).
- **While running:** the server serves HTML pages and a JSON graph data endpoint
  per `WEB.md § Routes and Pages`. Per-request responses are HTTP responses from
  the server, not stdout output of the command.
- **Errors (stderr):** plain text, with the standard AI-agent hint, per
  `HELP.md § Error message format`.

### Lifecycle

`rmp web` is long-lived: it serves until interrupted. It is one of two `rmp`
commands whose process keeps running rather than completing a single operation and
exiting; the other is `rmp graph serve` (see `§ Serve`). Sending `SIGINT`
(`Ctrl+C`) or `SIGTERM` shuts the server down gracefully and the process exits 0
(see `WEB.md § Server Lifecycle`).

### Exit Codes

These are the exit codes of the `rmp web` **process**. They are distinct from the
per-request HTTP status codes the running server returns (200, 400, 404, 405, 500),
which are specified in `WEB.md § Routes and Pages`.

| Exit Code | Cause |
|-----------|-------|
| 0 | Server started and was later stopped by `SIGINT`/`SIGTERM` (graceful shutdown). |
| 1 | Requested host/port could not be bound (port in use with an explicit `--port`, or host not assignable), or the data directory could not be read (`utils.ErrDatabase`). |
| 2 | Unknown flag or unexpected positional argument (`utils.ErrInvalidInput`). |
| 6 | `--port` value out of range 0-65535 or non-integer (`utils.ErrValidation`). |

The canonical exit-code catalogue is in `ARCHITECTURE.md § Exit Codes`; the web
interface introduces no new codes.

### Error Cases

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Explicit `--port` already in use | 1 | "Error: database error: cannot bind 127.0.0.1:8787: listen tcp 127.0.0.1:8787: bind: address already in use" |
| Host not assignable | 1 | "Error: database error: cannot bind 10.0.0.5:8787: listen tcp 10.0.0.5:8787: bind: cannot assign requested address" |
| `--port` out of range | 6 | "Error: validation error: --port must be an integer between 0 and 65535 (got 70000)" |
| `--port` not an integer | 6 | "Error: validation error: --port must be an integer between 0 and 65535 (got \"notanumber\")" |
| Unknown flag | 2 | "Error: invalid input: unknown flag: --foo" |
| Unexpected positional argument | 2 | "Error: invalid input: unexpected argument: X" |
| Data directory unreadable | 1 | "Error: reading data directory <absolute path of ~/.roadmaps>: database error" |

The two bind rows carry the operating system's own diagnostic after the address, and its wording belongs to the platform rather than to `rmp`. The part `rmp` fixes is everything up to and including `cannot bind <host>:<port>: `; the text after it is the Go standard library's `net.OpError` rendering, shown here as observed on Linux.

---

## Command Aliases Reference

| Command | Aliases |
|---------|---------|
| `ai-help` | - |
| `roadmap` | `road` |
| `task` | `t` |
| `sprint` | `s` |
| `audit` | `aud` |
| `stats` | - |
| `graph` | - |
| `web` | - |
| `list` | `ls` |
| `create` | `new` |
| `remove` (under `task`, `sprint`) | `rm` |
| `set-status` | `stat` |
| `set-priority` | `prio` |
| `set-severity` | `sev` |
| `update` | `upd` |
| `remove` (under `roadmap`) | `rm`, `delete` |
| `history` | `hist` |
| `add-tasks` | `add` |
| `remove-tasks` | `rm-tasks` |
| `move-tasks` | `mv-tasks` |
| `reorder` | `order` |
| `move-to` | `mvto` |
| `swap` | - |
| `top` | - |
| `bottom` | `btm` |
| `comment-add` (under `task`, `sprint`) | `c-add` |
| `comment-list` (under `task`, `sprint`) | `c-ls` |
| `comment-edit` (under `task`, `sprint`) | `c-edit` |
| `comment-remove` (under `task`, `sprint`) | `c-rm` |

**Note on the comment aliases:** The four comment aliases follow the family's
existing abbreviation rules: `comment` shortens to `c`, and the verb keeps the
abbreviation it already has elsewhere in the CLI (`list` → `ls`, `remove` → `rm`),
while `add` and `edit` have no shorter form anywhere and stay as they are. The
hyphen is retained, as in `remove-tasks` → `rm-tasks` and `move-tasks` →
`mv-tasks`. The same four aliases exist under both `task` and `sprint`, so the two
families read identically.

**Note on the `delete` alias:** The `delete` alias is scoped to `roadmap remove`
only. `task remove` and `sprint remove` accept the `rm` alias but NOT `delete`;
`rmp task delete` and `rmp sprint delete` are rejected with exit code `127`, as a
dispatch failure (see `§ Dispatch Failures (Unresolved Command or Subcommand
Names)`). The two lines are
`Error: unknown task subcommand: delete` and
`Error: unknown sprint subcommand: delete` respectively.

**Note on `assign` and `unassign`:** Neither name is a subcommand of the `task`
family, and neither has a reserved exit code. `rmp task assign` and
`rmp task unassign` are rejected by the same dispatch-failure path that rejects
any other unresolved name, with
`Error: unknown task subcommand: X` on stderr, where `X` is the unresolved name,
the `task` family help after it, nothing on stdout, and exit code `127` (see
`§ Dispatch Failures (Unresolved Command or Subcommand Names)` and
`ARCHITECTURE.md § Exit Codes`).
