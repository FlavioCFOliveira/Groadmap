# Data Formats

## Fundamental Principle

### Output (Responses)

**JSON output is reserved for query operations and record creation.**

- **Query operations (JSON)**: `list`, `ls`, `get`, `next`, `tasks`, `stats`, `show`, `history`, `hist`, `comment-list`, `c-ls`.
- **Server startup (JSON)**: `web` prints a single JSON object naming the served URL on successful startup (e.g. `{"url": "http://127.0.0.1:8787"}`), then keeps running; see `COMMANDS.md § Web Interface`. While running, the server returns HTTP responses (HTML pages and its JSON endpoints, the graph data endpoint and the task detail endpoint), which are not command stdout output.
- **Creation operations (JSON)**: `create`, `new`, `comment-add`, `c-add`. These commands return a JSON object containing the ID of the newly created record (e.g., `{"id": 42}`).
- **Other database modifications (No output)**: Commands that update, delete, or change the state of entities (status, priority, etc.) respond with **no content** on success, signaling completion via exit code `0`.
- **Help commands (Plain text)**: When no command is provided, or when using `-h` and `--help` flags, the application displays information in **plain text**, following traditional CLI application formats (not JSON).

**Error responses follow typical CLI behavior (NOT JSON):**
- Errors are written as explicit human-readable messages to stderr
- A failing invocation writes **nothing to stdout**; the error text and everything accompanying it go to stderr
- Help follows an error only on a **dispatch failure**, meaning an unresolved command name or an unresolved subcommand name, in which case the help for the level at which the name could not be resolved is written to stderr after the error. No other error class appends help. See `HELP.md § Error message format` and `COMMANDS.md § Dispatch Failures (Unresolved Command or Subcommand Names)`
- Uses standard Unix exit codes for script integration
- This rule governs **command output**. It does not govern the HTTP responses of
  the running `web` server, which are not command stdout or stderr (see the
  server-startup bullet above). The graph data endpoint answers a rejected or
  failed query with a JSON error object, specified in
  [Graph View Data](#graph-view-data), **Error Shape**.

### Input

**Application inputs are via CLI parameters. Exactly two flag values may also
arrive on standard input: the Cypher statement of `graph execute`, and the
comment body of the comment subcommands of the `task` and `sprint` families.**

- No JSON input
- No configuration files
- No interactive input
- **Standard input:** used as an alternative source for exactly two flag values,
  and by no other command:
  - the `--query` Cypher string of `graph execute` (see
    `GRAPH.md § Cypher Input Source and Precedence`);
  - the `--body` comment text of `comment-add` and `comment-edit` under `task`
    and `sprint` (see
    `COMMANDS.md § Comment Body Input Source and Precedence`).

  Every other command ignores standard input.

**Accepted formats:**
- Positional parameters: `rmp task create <name>`
- Short flags: `-r <name>`, `-p 5`
- Long flags: `--roadmap <name>`, `--priority 5`
- Comma-separated lists: `1,2,3`
- Standard input: the alternative source for the two flag values named above —
  the Cypher query when `rmp graph <subcommand>` is invoked without `--query`,
  and the comment body when a comment subcommand is invoked without `--body`.
  Neither is read to EOF: each read is bounded, and the bound, the unit it counts
  in, and what each refusal costs belong to that value's own canonical section,
  which this file does not restate. Those sections are
  `GRAPH.md § Cypher Input Source and Precedence` for the query and
  `COMMANDS.md § Comment Body Input Source and Precedence` for the comment body.

---

## Response Structure

### Query Response (Success)

Success responses for query operations are the direct result object or array, without any wrapper:

```json
{ /* command-specific payload directly */ }
```

**Examples:**
- `rmp task list` returns an array of Task objects directly: `[{...}, {...}]`
- `rmp sprint stats` returns the stats object directly: `{"sprint_id": 1, ...}`

### Creation Response (Success)

Commands that create new records producing a JSON object with the ID.
- **Exit Code**: `0`
- **Stdout**: `{"id": 42}` (or `{"name": "project1"}` for roadmaps)

### Modification Response (Success)

Commands that alter the database state without creating new records (update, delete, status change, etc.) produce **no output** on success.
- **Exit Code**: `0`
- **Stdout**: Empty

### Help Response

Help commands display human-readable text to stdout.

### Error Response

Error responses follow typical CLI conventions (NOT JSON). This covers command
output only; the `web` server's HTTP error responses are separate, and the graph
data endpoint's JSON error object is specified in
[Graph View Data](#graph-view-data), **Error Shape**.

---

## Exit Codes

Groadmap returns standard Unix exit codes for integration with shell scripts and CI/CD pipelines.

Refer to the authoritative exit code documentation and error mapping in [ARCHITECTURE.md](./ARCHITECTURE.md#exit-codes).

---

## Dates - ISO 8601 with UTC

### Exact Format

```
YYYY-MM-DDTHH:mm:ss.sssZ
```

### Rules

1. **Always UTC**: All dates are converted to UTC
2. **With milliseconds**: 3 digits after the dot
3. **Z suffix**: Explicit UTC indicator
4. **T separator**: Between date and time

### Scope

The format binds **every timestamp Groadmap generates**, wherever the product
writes it — not only the ones that reach a JSON object on stdout. It governs a
roadmap database's stored timestamps, an audit entry's `timestamp`, those same
values rendered into JSON output, and the `time` attribute of every diagnostic
record the two long-lived surfaces write to stderr: `rmp web` (see
`WEB.md § Logger Configuration`, rule 5) and `rmp graph serve` (see
`GRAPH.md § Server Diagnostics on Stderr`).

It does **not** govern a temporal value that is data a caller stored in the
knowledge graph. Those are the caller's values, of six distinct types, and each
is rendered in the ISO 8601 form of its own type; see
[Graph Query Result](#graph-query-result), **Temporal values**, which states that
boundary in full.

**A record whose message came from a dependency is inside this rule rather than
outside it.** The graph server's stderr carries records the graph engine
produces, and it is tempting to read those as output the product merely relays
and is therefore not answerable for. That reading fails on the point that
matters: the engine supplies the message and its attributes, and Groadmap's own
handler supplies the timestamp, because `log/slog` builds the `time` attribute
inside the handler rather than at the call site. Groadmap generates those
timestamps, so this rule binds them. The narrower reading — that the rule covers
only output whose **message** the product wrote — would also exempt the web
server's records, which carry database and engine error text inside them and
which `WEB.md` already requires to be UTC.

**One realisation of the format, not the rule restated in each place.** Every
surface that stamps a timestamp MUST use the project's single implementation of
this format rather than expressing it again locally
(`ARCHITECTURE.md § Modules and Responsibilities` names the module that owns date
handling). Two expressions of one format is how two surfaces come to answer one
question differently: one of them is corrected and the other is not, and nothing
between them notices. A surface that expresses the rule locally satisfies it for
itself and for nothing else, which is the failure this requirement exists
against.

---

## Task Status State Machine

The canonical state-machine definition (states, valid transitions, manual/automatic semantics, deletion preconditions) lives in `SPEC/STATE_MACHINE.md`. Refer to that file for the authoritative transition matrix and rules.

JSON output that includes a `status` field uses one of the five enum values defined in `MODELS.md` — Task Status (`BACKLOG`, `SPRINT`, `DOING`, `TESTING`, `COMPLETED`).

Sprint state machine (states, transitions, reopening): see `STATE_MACHINE.md § Sprint State Machine`.

---

## Data Types (JSON representation for Queries)

### Task

```json
{
  "id": 1,
  "title": "Implement JWT authentication system",
  "status": "BACKLOG",
  "type": "USER_STORY",
  "functional_requirements": "Users must be able to authenticate securely",
  "technical_requirements": "Create authentication module with JWT token support",
  "acceptance_criteria": "Functional login with 24h valid tokens; proper error handling",
  "created_at": "2026-03-12T10:00:00.000Z",
  "started_at": null,
  "tested_at": null,
  "closed_at": null,
  "completion_summary": null,
  "commit_open": null,
  "commit_close": null,
  "parent_task_id": null,
  "priority": 9,
  "severity": 0,
  "subtask_count": 0,
  "depends_on": [],
  "blocks": []
}
```

**`commit_open` and `commit_close`.** Both are JSON strings when set and `null`
when not, following the same null convention as the lifecycle timestamps beside
them. A set value is always a lowercase hexadecimal string of 7 to 64 characters:
the CLI accepts any letter case on input and stores the normalised form, so a
consumer of this JSON never sees an uppercase hash and never needs to fold case
before comparing two values. `commit_open` is `null` until the task first enters
`DOING`, and survives every later return to `BACKLOG`. `commit_close` is `null`
until the task enters `COMPLETED`, and returns to `null` on every return to
`BACKLOG`. A task can therefore carry a non-null `commit_open` with a null
`commit_close` in any status, while the reverse combination — a null `commit_open`
with a non-null `commit_close` — can only arise on a task that reached `COMPLETED`
before the columns existed. `MODELS.md § Task` is canonical for the format and
`STATE_MACHINE.md § Commit Tracking Fields` for when each value is written and
cleared.

### Sprint

Example with a capacity limit set (`max_tasks` is an integer):

```json
{
  "id": 1,
  "status": "OPEN",
  "title": "Sprint 1",
  "description": "Establish the foundations of the roadmap persistence layer so that roadmaps can be created and stored reliably.",
  "tasks": [1, 2, 3, 5],
  "task_count": 4,
  "created_at": "2026-03-12T09:00:00.000Z",
  "started_at": "2026-03-12T10:00:00.000Z",
  "closed_at": null,
  "max_tasks": 10,
  "order": 1
}
```

Example with unlimited capacity (`max_tasks` is `null`):

```json
{
  "id": 2,
  "status": "PENDING",
  "title": "Sprint 2",
  "description": "Harden the CLI against untrusted input so that no free-text field can carry terminal-escape or bidirectional control characters.",
  "tasks": [],
  "task_count": 0,
  "created_at": "2026-03-13T09:00:00.000Z",
  "started_at": null,
  "closed_at": null,
  "max_tasks": null,
  "order": 2
}
```

**Note:** The `tasks` and `task_count` fields are computed at runtime from the `sprint_tasks` junction table and are not stored in the `sprints` table. The `max_tasks` field is always present in the JSON output (never omitted); it is `null` when no capacity limit is set and an integer otherwise. The `order` field is always present: it is a positive integer (`> 0`), unique across the roadmap, and is stored in the `order_index` column (the JSON name is `order` because `ORDER` is a reserved SQL keyword). See `MODELS.md § Sprint Field Constraints`.

**Membership in both reads:** every read that produces a Sprint object populates `tasks` and `task_count` — the single object of `rmp sprint get` and every object of the `rmp sprint list` array alike. A Sprint object therefore never shows `tasks` as `null`, and never shows `task_count` as `0` for a sprint that holds tasks. `tasks` carries task **ids** as integers, in ascending id order, and never task objects; `task_count` always equals the number of entries in `tasks`. A sprint with no member task shows `task_count` `0` and `tasks` `[]`, as the second example above does, under the empty-array rule in `Implementation Notes` below. See `COMMANDS.md § List Sprints`.

### Task Comment

One entry of a task's work log, as returned by `rmp task comment-list`. The fields are defined in `MODELS.md § Task Comment`.

A comment that has never been edited (`updated_at` is `null`):

```json
{
  "id": 12,
  "task_id": 42,
  "type": "FINDING",
  "body": "The JWT middleware rejects tokens whose exp claim is exactly the current second. time.Now().After(exp) is false at equality, so the boundary second is accepted by the parser and refused by the handler.",
  "created_at": "2026-03-12T11:15:00.000Z",
  "updated_at": null
}
```

A comment that has been edited (`updated_at` carries the edit timestamp):

```json
{
  "id": 13,
  "task_id": 42,
  "type": "DECISION",
  "body": "Token expiry is compared with !time.Now().Before(exp), so the boundary second expires. Rejected the alternative of widening the clock skew allowance, because it hides the boundary instead of defining it.",
  "created_at": "2026-03-12T11:40:00.000Z",
  "updated_at": "2026-03-12T14:05:00.000Z"
}
```

**Notes:**
- `updated_at` is always present in the JSON output; it is `null` while the comment has never been edited and an ISO 8601 UTC timestamp afterwards.
- `type` is always present and never null; it is one of the seven values a task comment accepts.
- `body` preserves the author's interior line breaks as `\n` escapes in JSON. Leading and trailing whitespace is trimmed before storage.
- `rmp task comment-list` returns an array of these objects, oldest first, and `[]` when the task has no comments.

### Sprint Comment

One entry of a sprint's progression log, as returned by `rmp sprint comment-list`. The fields are defined in `MODELS.md § Sprint Comment`. The shape is identical to the task comment shape, with `sprint_id` in place of `task_id`.

```json
{
  "id": 4,
  "sprint_id": 7,
  "type": "PROGRESS",
  "body": "Authentication tasks 42 and 43 are closed; the rate-limit task 47 is still blocked on the shared Redis client decision, so the sprint carries one open dependency into its second week.",
  "created_at": "2026-03-18T09:00:00.000Z",
  "updated_at": null
}
```

**Notes:**
- `type` is one of the four values a sprint comment accepts: `FINDING`, `DECISION`, `PROGRESS`, `UPDATE`.
- `rmp sprint comment-list` returns an array of these objects, oldest first, and `[]` when the sprint has no comments.

**Comments are not embedded in task or sprint output.** The `Task` and `Sprint` JSON objects above carry no `comments` array and no `comment_count` field. `task get`, `task list`, `sprint get`, and `sprint show` return exactly the keys shown in their own examples. Comments are read only through `comment-list` and the read-only web interface.

### Audit Entry

Every audit entry carries the same seven keys. Two of them are nullable and are
present with the value `null` on the entries that do not carry them; a key is never
omitted.

```json
{
  "id": 1,
  "operation": "TASK_STATUS_DOING",
  "entity_type": "TASK",
  "entity_id": 42,
  "related_entity_id": null,
  "commit_hash": "5f93b51",
  "performed_at": "2026-03-12T15:30:00.000Z"
}
```

An entry that carries neither of the two nullable values, which is the common case:

```json
{
  "id": 2,
  "operation": "TASK_STATUS_TESTING",
  "entity_type": "TASK",
  "entity_id": 42,
  "related_entity_id": null,
  "commit_hash": null,
  "performed_at": "2026-03-12T15:40:00.000Z"
}
```

An entry for a relational operation, which names its counterpart:

```json
{
  "id": 3,
  "operation": "SPRINT_ADD_TASK",
  "entity_type": "SPRINT",
  "entity_id": 7,
  "related_entity_id": 42,
  "commit_hash": null,
  "performed_at": "2026-03-12T16:30:00.000Z"
}
```

**Field notes:**

| Key | Type | Notes |
|-----|------|-------|
| `id` | integer | Primary key of the entry. |
| `operation` | string | What happened. Opaque to the reader; see below. |
| `entity_type` | string | `"TASK"` or `"SPRINT"` — the entity whose history the entry belongs to. |
| `entity_id` | integer | Id of that entity. Always positive. |
| `related_entity_id` | integer or null | The counterpart entity of the operation that produced the entry, or `null` when that operation has no counterpart. `DATABASE.md § The Two Entities of a Relational Operation` is canonical. |
| `commit_hash` | string or null | The git commit bracketing a task's development work, 7 to 64 lowercase hexadecimal characters, or `null`. Non-null on `TASK_STATUS_DOING` and `TASK_STATUS_COMPLETED` only; `DATABASE.md § The Commit Hash of an Audit Entry` is canonical. |
| `performed_at` | string | ISO 8601 UTC. Shared by every entry a single command wrote. |

**Both nullable keys are always present.** A consumer reads `related_entity_id` and
`commit_hash` on every entry and finds either a value or `null`. Neither key is
omitted for the operations that do not use it, so an agent can rely on the key set
being identical across entries and needs no per-operation knowledge to parse one.

**`related_entity_id` is what distinguishes two entries of the same operation.**
Two `SPRINT_ADD_TASK` entries against the same sprint differ only in `id`,
`related_entity_id`, and possibly `performed_at`. A consumer that renders an audit
log MUST show `related_entity_id`, because without it those entries are
indistinguishable to a reader.

**The same operation value may carry it or not, and `null` is meaningful.** Whether
the key holds a value depends on the operation that produced the entry, not on the
operation name alone: a `TASK_STATUS_BACKLOG` entry written by `sprint remove-tasks`
names the sprint the task left, while one written by `task stat` carries `null`
because no sprint was party to that operation. A consumer MUST therefore read the key
per entry and MUST NOT infer its presence from the operation name. A `null` means the
operation had no counterpart; it never means a counterpart existed and went
unrecorded.

A comment operation is recorded against the parent entity, never against the comment: `TASK_COMMENT_CREATE` carries `entity_type: "TASK"` and the owning task's id in `entity_id`. See `DATABASE.md § audit Table`.

**`operation` is an opaque string to the reader.** The canonical catalogue of
operation values is `DATABASE.md § audit Table`, and it is enforced by the
application on write; the `operation` column itself carries no `CHECK`, so a stored
entry can carry a value the catalogue does not list. `TASK_ASSIGN` and
`TASK_UNASSIGN` are the two such values a Groadmap `audit` table can hold: they are
not in the valid set, so no command writes them and no `--operation` filter accepts
them by name, while the rows already carrying them are retained and are returned by
every unfiltered audit read. A consumer of this object — including an AI agent
reading the JSON — MUST therefore treat `operation` as an opaque string, MUST render
whatever value it receives, and MUST NOT fail, drop the entry, or substitute a
fallback when the value is not one it recognises.

The catalogue also publishes four LEGACY operations — `TASK_STATUS_CHANGE`,
`TASK_UPDATE`, `SPRINT_UPDATE`, and `SPRINT_MOVE_TASK`. Unlike `TASK_ASSIGN` and
`TASK_UNASSIGN`, these are in the valid set and are accepted as `--operation` filter
values, but no command writes them: they appear only on entries written before the
catalogue was refined. A consumer treats them exactly like any other value it
receives.

**Acceptance criteria:**

1. Every entry `rmp audit list` and `rmp audit history` emit carries all seven keys, including `related_entity_id` and `commit_hash`, whatever the operation.
2. An entry with no counterpart emits `"related_entity_id": null`, never `0` and never an omitted key.
3. An entry with no commit emits `"commit_hash": null`, never `""` and never an omitted key.
4. A `SPRINT_ADD_TASK` entry emits the added task's id in `related_entity_id`, and the `TASK_STATUS_SPRINT` entry written alongside it emits the sprint's id in `related_entity_id`; the two entries carry transposed ids and the same `performed_at`.
5. A `TASK_STATUS_BACKLOG` entry written by `sprint remove-tasks` emits the sprint's id, and one written by `task stat` emits `null`.
6. A `TASK_STATUS_DOING` entry emits the `--commit-open` value, normalised to lowercase, in `commit_hash`.

---

## Graph Query Result

`rmp graph execute` returns the result of a Cypher statement that produces result
columns as a single JSON object to stdout, and `rmp graph client` returns the same
object for the same statement (see [Graph Client Result](#graph-client-result)). The shape exposes the result's columns
and its rows, mirroring the GoGraph engine result, which exposes the ordered
column names (`Columns()`) and an iterable sequence of records. A statement that
produces no columns returns the shape in [Graph Write Result](#graph-write-result)
instead, unless it was written with an `EXPLAIN` or `PROFILE` prefix, which
always returns this shape.

A statement written with one of those two prefixes adds exactly one member to
this object: `plan` for an `EXPLAIN`, `profile` for a `PROFILE`. Both are
optional, at most one is ever present, and each holds the recursive object
[Graph Plan Node](#graph-plan-node) defines. A statement written with neither
prefix carries neither key, and its object is unchanged in every byte.

A statement that **changed the graph** adds one further optional member,
`counters`, naming what it changed;
[Graph Query Counters](#graph-query-counters) is canonical for it. A statement
that changed nothing carries no such key, so a read — which is what this shape
answers in the ordinary case — is unchanged in every byte. The member never
appears beside `plan` or `profile`, for the reason that section's rule 5 gives.

This is the canonical specification of the graph read-result shape. The command
contract that references it is `COMMANDS.md § Graph Management`; the feature
design is in `GRAPH.md`.

### Shape

```json
{
  "columns": ["s.key", "c.path"],
  "rows": [
    ["user-authentication", "internal/auth/jwt.go"],
    ["payment-processing", "internal/payments/stripe.go"]
  ]
}
```

The same statement written with an `EXPLAIN` prefix, which executes nothing and
so returns the declared columns with no rows:

```json
{
  "columns": ["s.key", "c.path"],
  "rows": [],
  "plan": {
    "operator": "Project",
    "detail": "s.key, c.path",
    "estimatedRows": 12,
    "estimatedRowsSource": "stats"
  }
}
```

Field reference:

| Field | Type | Description |
|-------|------|-------------|
| `columns` | array of string | The ordered return-column names of the query (the engine's `Columns()`). One entry per returned expression, in the order the query declares them. |
| `rows` | array of array | One inner array per record, in the order the engine yields records. Each inner array has exactly `columns.length` cells, positionally aligned with `columns`. |
| `plan` | object, omitted unless present | The plan the engine built for a statement written with an `EXPLAIN` prefix, as [Graph Plan Node](#graph-plan-node) defines it. Its figures are the planner's **estimates**: the statement was not executed. |
| `profile` | object, omitted unless present | The plan the engine ran for a statement written with a `PROFILE` prefix, as [Graph Plan Node](#graph-plan-node) defines it. Its figures are **measurements** of that run. |
| `counters` | object, omitted unless present | What the statement changed in the graph, as [Graph Query Counters](#graph-query-counters) defines it. Present only when the statement changed something, and written after `rows`. |

Rules:

1. `columns` and `rows` are always present. A query that matches nothing returns
   its declared `columns` and an empty `rows` array (`[]`), never `null`.
2. A statement that returns no columns does not produce this shape at all; it
   produces the `{"ok": true}` object of
   [Graph Write Result](#graph-write-result). This shape is the answer to a
   statement that declares at least one result column. The one exception is a
   statement written with an `EXPLAIN` or `PROFILE` prefix, which produces this
   shape whether or not it declares a column: rule 6 below.
3. Each row cell is a JSON value produced by the property-type mapping below.
4. The result is pretty-printed with two-space indentation and a trailing
   newline, consistent with all other JSON output (see
   [Implementation Notes](#implementation-notes)).
5. `plan` and `profile` are mutually exclusive and both are optional. At most one
   appears in any object, and neither appears for a statement written with no
   prefix. A consumer decides which kind of figure it is holding by **which key
   carried the tree**, and by nothing inside the tree: an estimate and a
   measurement are otherwise written identically, and the two keys are what keep
   them apart (see
   `GRAPH.md § Query Plans: The EXPLAIN and PROFILE Prefixes`, rule 7).
6. A prefixed statement that declares no result columns publishes `columns` and
   `rows` as empty arrays alongside its plan, rather than the `{"ok": true}`
   object. `EXPLAIN CREATE (n:Spec {key:'auth'})` is the case that arises: it
   executes nothing, so `{"ok": true}` would report a success identical to the
   one a committed `CREATE` reports.
7. A statement that both declares result columns and changed the graph — a
   `CREATE ... RETURN`, a `SET ... RETURN` — publishes `counters` after `rows`,
   under the rules of [Graph Query Counters](#graph-query-counters). The member
   is additive: it never displaces `columns` or `rows`, and a statement that
   changed nothing does not carry it.

### Property-Type Mapping

GoGraph property values are typed. Each type maps to JSON as follows:

| GoGraph value type | JSON representation | Notes |
|--------------------|---------------------|-------|
| `string` | JSON string | UTF-8, as-is. |
| `int64` | JSON number (integer) | Emitted without a decimal point. JSON numbers are IEEE-754 doubles in many consumers; values outside the safe integer range (beyond ±2^53) may lose precision on the consumer side. The CLI emits the exact integer; precision loss, if any, is the consumer's concern. |
| `float64` | JSON number | Emitted in the standard Go float format. `NaN`, positive infinity, and negative infinity are not valid JSON numbers; when the engine produces any of them, they are emitted as JSON `null`. |
| `bool` | JSON boolean | `true` / `false`. |
| A temporal value | JSON string | One of six kinds, each written in the ISO 8601 form of **its own type** rather than as an instant in UTC. The six do not share one shape, and none of them is the timestamp format of [Dates - ISO 8601 with UTC](#dates---iso-8601-with-utc). See **Temporal values** below. |
| `[]byte` | JSON string | Base64-standard-encoded (RFC 4648) so arbitrary bytes survive JSON transport. |
| absent / null property | JSON `null` | A returned expression that has no value is `null`. |

**Temporal values.** The engine's value model has six temporal kinds, and each is
written in the ISO 8601 form of its own type. These are the renderings:

| Temporal kind | Rendering | Examples |
|---------------|-----------|----------|
| Date | The calendar date alone. | `2026-03-05` |
| Time | The time of day, followed by the offset the value itself carries. | `14:23:47+02:00`, `14:23:47.12+02:00`, `14:23:47Z` |
| Local time | The time of day, with no offset. | `14:23:47`, `14:23:47.12` |
| Date and time | Converted to UTC first, then written as the date, `T`, the time, and a `Z` suffix. | `2026-03-05T12:23:47.123456789Z`, `2026-03-05T14:23:47.12Z`, `2026-03-05T14:23:47Z` |
| Local date and time | The date, `T`, and the time, with no offset. | `2026-03-05T14:23:47`, `2026-03-05T14:23:47.12` |
| Duration | The ISO 8601 duration form. | `P1Y2M3DT4H5M6S`, `PT0S` |

**Why the mapping is per kind rather than one shape.** A graph temporal is a value
the caller's own statement produced, of one of six distinct types. It is not a
Groadmap-generated instant, and the six do not share an instant's shape: writing
them all as an instant in UTC is impossible for a duration, which is not an
instant at all, and false for the three kinds that carry no offset. Rendering each
kind in the ISO 8601 form of its own type is also what the query language's own
string conversion of a temporal produces, so the published format follows the
language rather than departing from it.

Four consequences bind a consumer, and none may be assumed away:

1. **The fractional second is variable-width, and it is often absent.** Every
   kind that can carry one — a time, a local time, a date and time, and a local
   date and time — writes it with between zero and nine digits: trailing zeros are
   trimmed, and a whole second is written with no fraction and no dot at all. A
   date and time of `2026-03-05T14:23:47.120Z` is published as
   `2026-03-05T14:23:47.12Z`; the same value with a zero fraction is published as
   `2026-03-05T14:23:47Z`; and one with nanosecond precision keeps all nine
   digits. Nothing rounds and nothing pads: the width follows the value. A
   consumer MUST parse the fraction as optional and of variable length, and MUST
   NOT expect the three digits of
   [Dates - ISO 8601 with UTC](#dates---iso-8601-with-utc). The variable width is
   a hazard rather than a convenience — a consumer written against a fixed `.sss`
   field parses the three-digit case and breaks on every other — and it is
   published plainly here for that reason.
2. **Three of the six kinds carry no offset, and none is written for them.** A
   date, a local time, and a local date and time are zoneless by definition;
   appending `Z` to any of them would assert an offset the value does not hold.
3. **A time keeps its own offset; a date and time does not.** A date and time is
   converted to UTC before it is written, so a value at `+02:00` is published with
   a `Z` and a shifted clock reading. A time is written with the offset it
   carries, so a value at `+02:00` is published with `+02:00` and an unshifted
   clock reading. The mapping is not uniform across those two kinds, and a
   consumer that reads an offset MUST read the one it is given rather than assume
   UTC.
4. **A duration is not an instant** and has no timestamp form at all. It is
   written in the ISO 8601 duration form, which no timestamp parser accepts.

**This is not the format Groadmap uses for its own timestamps.**
[Dates - ISO 8601 with UTC](#dates---iso-8601-with-utc) fixes the shape of every
timestamp Groadmap generates — a task's `created_at`, an audit entry's
`timestamp` — and that shape is a fixed-width instant in UTC. A graph temporal is
a caller's value carried through the engine and published as the kind it is. The
two formats coincide only for a date and time whose fractional second happens to
have exactly three significant digits.

The renderings above govern every surface that publishes a graph value: the
`{columns, rows}` shape of this section, the identical shape
[Graph Client Result](#graph-client-result) requires of `rmp graph client`, and
the node and edge properties of [Graph View Data](#graph-view-data).

### Graph element mapping

A returned value that is itself a graph element (rather than a scalar property)
is serialised as a JSON object using the fixed shapes below. The same mapping
applies recursively to properties, list elements, and map values.

| GoGraph value | JSON representation |
|---------------|---------------------|
| Node | `{"id": <int>, "labels": [<string>, ...], "properties": {<object>}}` |
| Relationship (edge) | `{"id": <int>, "type": "<string>", "startId": <int>, "endId": <int>, "properties": {<object>}}` |
| Path | `{"nodes": [<node>, ...], "relationships": [<relationship>, ...]}` |
| List | JSON array of mapped values. |
| Map | JSON object whose values are mapped values. |

Rules:

1. `properties` is a JSON object whose values follow the scalar property-type
   mapping above, applied recursively (a property may itself be a list or map).
2. A node's `labels` array preserves the order GoGraph reports and may be empty
   (`[]`) when the node carries no labels.
3. Within a single result, a relationship's `startId` and `endId` reference the
   `id` of nodes that appear in the same result or path. The identifiers exist
   so that nodes and relationships in one result or path can be correlated.
4. `id`, `startId`, and `endId` are GoGraph's internal storage identifiers
   (`uint64`). They are emitted as JSON numbers and carry the same `>2^53`
   precision caveat noted for `int64` above. These identifiers are **ephemeral**:
   they are not stable business keys, are not guaranteed to remain constant
   across invocations, and MUST NOT be persisted or used as long-lived
   references. Agents must rely on node and edge properties (for example `key` or
   `name`) for stable identity, following the conventions in
   `GRAPH.md § Multi-Layer Modelling Conventions`.

### One Realisation of the Mapping

The two sections above are canonical for **what** the mapping produces, and this
section changes none of it. It fixes the question they do not answer: **how many
times the mapping may be written**. The answer is once.

**One realisation of the mapping, not the mapping restated per surface.** Every
surface that turns an engine value into published JSON MUST use the project's
single implementation of [Property-Type Mapping](#property-type-mapping), and of
the Node and Relationship rows of
[Graph element mapping](#graph-element-mapping), rather than expressing either
again locally. Three surfaces are bound by the rule: `rmp graph execute`,
`rmp graph client` (see [Graph Client Result](#graph-client-result)), and the web
interface's graph data endpoint (see [Graph View Data](#graph-view-data)).
`ARCHITECTURE.md § Modules and Responsibilities` is canonical for the package
that holds the realisation and for why it is a package rather than a function
inside one of its callers.

Two expressions of one mapping is how two surfaces come to answer one question
differently: one of them is corrected and the other is not, and nothing between
them notices. Every side a test normally watches stays quiet while it happens.
Both copies keep compiling, because neither calls the other; both keep passing,
because each is exercised against itself where it is exercised at all; and the
divergence becomes visible only to a reader holding a CLI row and a web node's
properties side by side. A surface that expresses the mapping locally satisfies
this specification for itself and for nothing else, which is the failure this
requirement exists against.

**The rule is enforced rather than described.** `internal/testenv` fails the
build if a second realisation of either mapping appears anywhere in production
source, in the way it already fails the build for a second engine construction
and a second snapshot write (`ARCHITECTURE.md § Modules and Responsibilities`,
module 8). A static gate is the instrument this class of rule takes in this
project, and it is the right instrument here rather than a test that compares two
implementations and asserts they agree: once the second copy is gone there is
nothing left to compare, and what remains to be prevented is a third copy
appearing later.

**What each surface still owns.** The rule binds the mapping from a value to its
JSON. It does not bind the document that JSON is placed in, and the two documents
are not the same one.

| Surface | The document it publishes | What it takes from the single realisation |
|---------|---------------------------|-------------------------------------------|
| `rmp graph execute` and `rmp graph client` | The `{columns, rows}` object of [Graph Query Result](#graph-query-result) | Every top-level result cell, of whatever kind, and everything nested inside one |
| The graph data endpoint | The node-and-edge object of [Graph View Data](#graph-view-data) | The Node and Relationship shapes, and the `properties` object inside each |

**Only the CLI publishes a path, so the Path row is not shared.** The graph data
endpoint publishes no path object at all: a path in a result is decomposed into
the nodes and relationships it contains, each collected once and placed in the
node and edge arrays (see [Graph View Data](#graph-view-data), rule 3). The Path
rendering of [Graph element mapping](#graph-element-mapping) therefore already
has one realisation, because one surface produces it, and it stays with the
surface that does.

**A property value is never a graph element, and sharing the mapping does not
make it one.** Two independent grounds establish it, and the conclusion needs
only one of them. The storage boundary: the store's property representation has
no encoding for a node, a relationship, a path, or a map, and the conversion back
from it cannot construct one, so a property read back is never one of the four
whatever a statement attempted to write. And measurement: a statement that
assigns a node, a relationship, or a path to a property leaves that key absent
from the entity when a later process reads it.

**Do not read that as a uniform refusal.** How the attempt is turned away depends
on the form of the statement rather than on the value: on some write paths the
engine raises `InvalidPropertyType`, and on others the statement is accepted,
reports success, and stores nothing. Which paths do which is engine behaviour of
the class `GRAPH.md § What Groadmap Does Not Check` exists to catalogue; this
section neither settles it nor rests on it, because the conclusion above holds on
every path either way. What this section settles is that conclusion alone: the
element rows of the shared realisation are reached from a top-level result cell
and from nowhere else.

Giving a surface that only ever maps property bags a realisation which also
carries those rows therefore widens neither what that surface can publish nor any
JSON a request can produce. It changes exactly one thing, and only in a case no
input can construct: an element found where a property belongs is rendered as the
object this specification requires, instead of as whatever a surface that never
expected one fell back to.

**What preserves the byte identity.**
[Graph Client Result](#graph-client-result) requires the bytes `rmp graph client`
writes to be the bytes `rmp graph execute` writes for the same statement — bar
the one measured duration named in that section's rule 5, which is a property of
the execution rather than of the mapping — and that identity holds by
construction rather than by inspection: a result that
crossed the protocol is mapped back onto the engine's value model rather than
onto JSON, so both paths run one serialiser over one representation. The single
realisation gives the third surface the same standing. What the graph data
endpoint must match is not a whole document — it publishes a different one — but
every value and every element object inside it, and under this rule those match
because one piece of code produced them, not because two pieces were compared and
found to agree.

## Graph Plan Node

A plan node is the recursive object that the `plan` and `profile` members of
[Graph Query Result](#graph-query-result) carry. It is one operator of the query
plan the engine built, together with the operators that feed it. The same shape
serves both members: what differs between an `EXPLAIN` and a `PROFILE` is which
keys are present, never what a present key means.

This is the canonical specification of the plan-node shape. The behaviour that
produces it — what each prefix does, which statements each admits, and why the
two members are two rather than one — is in
`GRAPH.md § Query Plans: The EXPLAIN and PROFILE Prefixes`; the command contract
is `COMMANDS.md § Graph Management`.

### Shape

A `profile` tree, which carries every key the shape defines:

```json
{
  "operator": "Project",
  "detail": "s.key",
  "estimatedRows": 12,
  "estimatedRowsSource": "stats",
  "rows": 9,
  "timeNs": 1482310,
  "children": [
    {
      "operator": "Filter",
      "detail": "s.status = 'implemented'",
      "estimatedRows": 12,
      "estimatedRowsSource": "heuristic",
      "rows": 9,
      "timeNs": 1104986,
      "rowsRemovedByFilter": 35,
      "children": [
        {
          "operator": "NodeByLabelScan",
          "detail": "s:Spec",
          "estimatedRows": 44,
          "estimatedRowsSource": "exact",
          "rows": 44,
          "timeNs": 815402,
          "dbHits": 44
        }
      ]
    }
  ]
}
```

### Field reference

| Field | Type | Presence | Description |
|-------|------|----------|-------------|
| `operator` | string | Always | The operator's type name, for example `NodeByLabelScan` or `HashJoin`. It is the name of the operator that actually runs, taken from the operator itself rather than reconstructed from the planner's decisions. |
| `detail` | string | Omitted when empty | The physical decision the operator took, worth reading beside its name: the label it scans, the index it seeks, the pattern it expands. An operator with nothing to add carries no key. |
| `children` | array of plan node | Omitted when empty | The operators this one draws its rows from, in **execution** order. For an asymmetric operator that is the order that explains the cost: a join's build side before its probe side, an apply's outer before its inner. A leaf carries no key. |
| `estimatedRows` | integer | Present only with `estimatedRowsSource` | The planner's predicted row count for this operator. It is a prediction made before anything ran, and it is the only figure an `EXPLAIN` has. |
| `estimatedRowsSource` | string | Present only with `estimatedRows` | Where the estimate came from, and therefore how far it may be trusted. Exactly one of `exact`, `stats`, `heuristic`. |
| `rows` | integer | `profile` only | The number of rows the operator emitted. Measured. |
| `timeNs` | integer | `profile` only | The wall-clock time attributed to the operator, in whole nanoseconds. Measured, and **inclusive of the operator's children**. |
| `dbHits` | integer | `profile` only, and omitted when the figure was not counted | The number of storage record accesses charged to the operator. |
| `rowsRemovedByFilter` | integer | `profile` only, and omitted when the operator has no rejection mechanism | The number of candidate rows the operator read and then discarded because a predicate said no. |

### Rules

1. **`operator` is always present and is never empty.** Every other key may be
   absent, and a consumer must treat every other key as optional.
2. **No key beyond the nine above appears.** The engine's plan node carries
   information this shape does not publish, and the protocol carries fields of
   its own; neither is passed through. A consumer may rely on the key set being
   closed, and an addition to it is a change to this section.
3. **`children` is ordered and the order is meaningful.** It is execution order,
   not an arbitrary traversal, and a consumer that reorders it destroys the one
   thing the order was carrying.
4. **`estimatedRows` and `estimatedRowsSource` appear together or not at all.**
   An estimate with no provenance is a bare number a reader cannot weigh, and a
   provenance with no number says nothing; the pair is written as a pair.
5. **An absent estimate is an absent estimate, not a zero.** An operator the
   planner attributed no estimate to, and an operator whose backing statistic was
   absent or stale, both omit the pair. Publishing a number for either would
   fabricate one. A genuine estimate of zero is published as `0`.
6. **`estimatedRows` is published for an `EXPLAIN` as well as for a `PROFILE`,
   and it is the same number in both.** It is what the planner predicted, and the
   planner predicted it before either statement ran. Placing it beside the
   measured `rows` of a `profile` tree is the point: the two are readable against
   each other in one object, which is what makes a bad estimate visible.
7. **`rows`, `timeNs`, `dbHits` and `rowsRemovedByFilter` never appear in a
   `plan` tree.** An `EXPLAIN` executes nothing, so it measured nothing; a `rows`
   of `0` on an operator that never ran would read as an operator that produced
   no rows. The four keys are the measured figures, and only a `profile` tree has
   any.
8. **`dbHits` is omitted when the figure was never counted, and is never
   published as `0` in its place.** The engine distinguishes an operator whose
   accesses were counted and came to zero — which publishes `0` — from an
   operator whose accesses nobody counted, which publishes no key. A reader must
   read an absent `dbHits` as "not counted" and never as "none". This is the
   distinction the engine's own text rendering draws by printing `?`, and
   collapsing it here would give a caller a measurement that was never taken.
9. **`dbHits` counts access-path record reads and never property reads.** This
   diverges from Neo4j, which additionally charges one hit per property read, and
   the divergence is published because a caller comparing figures across the two
   products otherwise concludes this one is wrong. It is not: it counts a
   different quantity, and it counts that quantity exactly. A present `dbHits` is
   in some cases counted by the operator and in others derived from the rows it
   emitted, under a contract that asserts one record read per row; the published
   key does not say which, and a consumer must not infer it. The distinction the
   key does carry is the one rule 8 fixes — present against absent — and that is
   the distinction a reader acts on.
10. **`rowsRemovedByFilter` is published as `0` when the operator rejected
    nothing, and is omitted only when the operator has no rejection mechanism at
    all.** The asymmetry against rule 8 is deliberate and must not be
    "harmonised". There, an absent key admits a figure exists and was not
    counted. Here, an absent key states there is no figure to have — and a
    present `0` is a finding in its own right, because a filter that rejected
    nothing is exactly what a reader of a slow plan wants to see.
11. **`timeNs` is inclusive of the node's children.** An operator's figure covers
    everything it drew from the operators beneath it. Summing the `timeNs` of a
    tree's nodes double-counts every level; the root's figure is the one that
    describes the statement.
12. **`timeNs` publishes the engine's whole-nanosecond figure and derives
    nothing from it.** The engine measures a duration in nanoseconds and the
    protocol carries that same integer, so both surfaces that publish this key
    already hold one identical whole number, and publishing it unchanged makes
    the byte identity
    [Graph Client Result](#graph-client-result) requires **exact by
    construction**. A millisecond value would have been friendlier to read and
    would have put that guarantee at the mercy of two floating-point formatters
    agreeing on a last digit — a guarantee the specification would then be
    asserting rather than holding. A consumer that wants milliseconds divides;
    the division is a consumer's rounding decision, and it is not one this
    format takes on its behalf.
13. **A logical plan node carries `operator` and `children` and nothing else.**
    A writing statement has no physical operator tree outside a transaction, so
    `EXPLAIN` captures its logical plan instead
    (`GRAPH.md § Query Plans: The EXPLAIN and PROFILE Prefixes`, rule 5). In such
    a tree `operator` is the plan line as the engine writes it, with any detail
    already inside that string, and neither `detail` nor the estimate pair is
    present. A consumer that parses `operator` as a bare operator type name is
    correct for every reading statement and wrong for this one, which is why the
    case is published here rather than left to be discovered.
14. **The tree has one realisation, shared by `rmp graph execute` and
    `rmp graph client`.** A plan that crossed the protocol is mapped back onto
    the engine's own plan representation and then serialised by the same code
    that serialises a plan captured in process — the same construction, and for
    the same reason, that
    [One Realisation of the Mapping](#one-realisation-of-the-mapping) applies to
    values. The byte identity
    [Graph Client Result](#graph-client-result) requires is therefore a property
    of the code rather than an assertion policed by comparison. Mapping the
    protocol's own plan encoding straight to JSON would create a second
    realisation of this section, free to drift from the first.

## Graph Write Result

`rmp graph execute` mirrors what the executed statement returns, and
`rmp graph client` mirrors it identically (see
[Graph Client Result](#graph-client-result)). The discriminator is whether the
statement produces **result columns**:

1. **The statement produces result columns:** the output is the standard
   read-result shape defined in [Graph Query Result](#graph-query-result) — a
   `columns` array and a `rows` array — populated with the elements the statement
   returns. For example, a `CREATE ... RETURN n` query returns the created node in
   the `{columns, rows}` shape.
2. **The statement produces no result columns:** the output is exactly:

```json
{"ok": true}
```

`{"ok": true}` is the success signal for a statement that returns no data, and
it is the whole of the object for a statement that changed nothing. A statement
that **did** change something adds one member, `counters`, naming what it
changed:

```json
{
  "ok": true,
  "counters": {
    "nodesCreated": 1,
    "propertiesWritten": 2,
    "labelsAdded": 1
  }
}
```

[Graph Query Counters](#graph-query-counters) is canonical for that member: its
key set, the rule that omits a zero, and the rule that omits the whole block for
a statement that changed nothing. The member is **additive**, so `ok` keeps its
meaning, its value and its position, and a consumer that reads `ok` and ignores
members it does not know is unaffected.

**Where this specification writes `{"ok": true}` for a statement that changed
the graph, it names the constant part of the object.** The two-member form above
is what such a statement actually publishes; the shorthand is used throughout
this file, `GRAPH.md` and `COMMANDS.md` wherever the point being made is the
shape rather than the counters, and it is not a second, counter-free shape. A
statement that changed nothing publishes the shorthand literally.

**Why the discriminator is the columns and not the `RETURN` clause.** For every
data-writing statement the two coincide exactly: a `CREATE`, `MERGE`, `SET`,
`REMOVE`, `DELETE`, or `DETACH DELETE` statement produces columns when, and only
when, it carries a `RETURN` clause. They part company on the schema statements
(see `GRAPH.md § Schema Management`). A schema-introspection command —
`SHOW INDEXES` and its siblings — produces columns while carrying no `RETURN`
clause, so it returns the `{columns, rows}` shape. A schema-mutating statement —
`CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, `DROP CONSTRAINT` — produces no
columns and returns `{"ok": true}`.

**A statement written with an `EXPLAIN` or `PROFILE` prefix never produces this
shape.** It is the one statement class the discriminator above does not govern:
whatever it declares, it returns the shape of
[Graph Query Result](#graph-query-result) carrying its plan, with `columns` and
`rows` empty where it declares no column. The reason is that `{"ok": true}` is a
claim, not a placeholder — it says a statement succeeded in committing what it
was asked to commit — and an `EXPLAIN` commits nothing and was not asked to.
Publishing it here would make `EXPLAIN CREATE (n:Spec)` indistinguishable on
stdout from the `CREATE` that ran, which is the confusion the prefix exists to
prevent. The exception costs nothing in compatibility, because the output of a
prefixed statement has no earlier contract to break while an unprefixed
statement's output is unchanged byte for byte;
`GRAPH.md § Query Plans: The EXPLAIN and PROFILE Prefixes`, rules 8 and 9, is
canonical for that reasoning and this section does not restate it.

Field reference (no-columns case):

| Field | Type | Description |
|-------|------|-------------|
| `ok` | boolean | Always `true`. Confirms the statement succeeded: for a data-writing query, that its transaction committed; for a schema-mutating statement, that the schema change was applied. |
| `counters` | object, omitted unless present | What the statement changed in the graph, as [Graph Query Counters](#graph-query-counters) defines it. Present only when the statement changed something, and written after `ok`. |

Examples:

A write query without `RETURN` whose transaction applied nothing — a `MERGE`
that matched an existing element, a `DELETE` whose pattern matched no row:

```json
{"ok": true}
```

The same query when it did change the graph, here a `MERGE` that created a
labelled node with one property:

```json
{
  "ok": true,
  "counters": {
    "nodesCreated": 1,
    "propertiesWritten": 1,
    "labelsAdded": 1
  }
}
```

A schema-mutating statement such as `CREATE INDEX`, which registers one index:

```json
{
  "ok": true,
  "counters": {
    "indexesAdded": 1
  }
}
```

A write query that ends with `RETURN n` (same shape as a read result, with the
same counters member added):

```json
{
  "columns": ["n"],
  "rows": [
    [
      {
        "id": 17,
        "labels": ["Spec"],
        "properties": {"key": "user-authentication"}
      }
    ]
  ],
  "counters": {
    "nodesCreated": 1,
    "propertiesWritten": 1,
    "labelsAdded": 1
  }
}
```

---

## Graph Query Counters

A statement that changed the graph publishes, beside its result, a `counters`
object naming what it changed. `rmp graph execute` and `rmp graph client` both
publish it, and they publish the same object for the same statement against the
same graph (see [Graph Client Result](#graph-client-result)).

The member is **additive**. It is added to the two published result shapes and
removes nothing from either: `ok` keeps its meaning and its position in
[Graph Write Result](#graph-write-result), and `columns` and `rows` keep theirs
in [Graph Query Result](#graph-query-result). A statement that changed nothing
carries no `counters` key at all and produces exactly the bytes it produced
before this member existed. An existing consumer is therefore unaffected in
either case: it parses what it parsed before, and reads one further member on
the statements that now have something more to report.

This is the canonical specification of the counters' shape and key set. The
behaviour that produces them — when they are read, what a failed or rolled-back
statement publishes, and the protocol constraint that fixes the property key —
is in `GRAPH.md § Write Counters: What a Statement Changed`; the command
contract is `COMMANDS.md § Graph Management`.

### Shape of the counters object

A write that declares no result column, added to the object of
[Graph Write Result](#graph-write-result):

```json
{
  "ok": true,
  "counters": {
    "nodesCreated": 1,
    "propertiesWritten": 2,
    "labelsAdded": 1
  }
}
```

A write that declares one, added to the object of
[Graph Query Result](#graph-query-result):

```json
{
  "columns": ["w.serial"],
  "rows": [["W-1"]],
  "counters": {
    "propertiesWritten": 1
  }
}
```

Field reference. Every member is a JSON number, every member is omitted when its
value is zero, and the table's order is the order the members are written in:

| Field | Description |
|-------|-------------|
| `nodesCreated` | Nodes the statement added to the graph. |
| `nodesDeleted` | Nodes the statement removed from the graph. |
| `relationshipsCreated` | Relationships the statement added. |
| `relationshipsDeleted` | Relationships the statement removed, including the ones a `DETACH DELETE` removed as a consequence of deleting a node rather than by naming them. |
| `propertiesWritten` | Property assignments and property removals together, as one figure. It is one figure rather than two for the reason rule 4 gives, and its name is chosen to say so. Two consequences a caller should know: assigning `null` to a property removes it, and therefore counts here; and deleting an element does not count the properties that went with it, so a `DETACH DELETE` reports no property figure at all. |
| `labelsAdded` | Labels the statement attached to a node. Creating a node with a label counts the label here as well as the node under `nodesCreated`. |
| `labelsRemoved` | Labels the statement detached from a node. |
| `indexesAdded` | Indexes a schema statement registered. |
| `indexesRemoved` | Indexes a schema statement dropped. |
| `constraintsAdded` | Constraints a schema statement registered. |
| `constraintsRemoved` | Constraints a schema statement dropped. |

Rules:

1. **The `counters` member is present if and only if the statement changed
   something.** The engine reports whether a statement contained updates at all,
   and the member is published exactly when it did. Three classes therefore
   carry no `counters` key, and each produces exactly the bytes it produced
   before this member existed: a statement that only reads; a statement that
   writes but applied nothing, such as a `MERGE` that matched an existing
   element or a `DELETE` whose pattern matched no row; and any statement written
   with an `EXPLAIN` prefix, which executes nothing at all.
2. **Within the block, a counter whose value is zero is omitted.** The block is
   consequently never empty when it is present, and a caller reads an absent key
   as zero. This is the opposite of the rule [Graph Plan Node](#graph-plan-node)
   states for an absent estimate, and the asymmetry is intended: there, an
   absent key admits that a figure exists and was not measured, so publishing a
   zero would fabricate a measurement. Here every counter is counted for every
   statement that reaches the block, so an absent key states that the effect it
   names did not happen and there is no second state for a zero to hide. What
   omission buys is that the figures which matter are not buried under ten
   zeroes.
3. **The member is additive to both published result shapes, and is written
   last in each.** It may appear beside `ok` in
   [Graph Write Result](#graph-write-result) and beside `columns` and `rows` in
   [Graph Query Result](#graph-query-result). No existing member changes its
   meaning, its value, or its position: `ok` still says that the statement
   succeeded in committing what it was asked to commit. This is the same bound
   `GRAPH.md § Query Plans: The EXPLAIN and PROFILE Prefixes`, rule 9, places on
   the plan members — what changes is the output of a statement that has
   something new to report, and the output of every other statement is left
   untouched.
4. **`propertiesWritten` carries property assignments and property removals as
   one figure, and is named for what it carries.** The engine counts the two
   separately, and this specification publishes them as one. The reason is the
   protocol a served result crosses: it carries a single property counter and
   has no second channel for a removal, so a result read through
   `rmp graph client` can distinguish eleven of the engine's twelve counters and
   never the twelfth. Publishing the split on the direct path alone would break
   the identity [Graph Client Result](#graph-client-result) requires; publishing
   a removal under a key named for an assignment would state an effect that did
   not happen. Folding on both paths and naming the key for the sum is the only
   arrangement under which every requirement stated here holds at once, and it
   costs the member's purpose nothing: telling a `MERGE` that created from one
   that matched, and a `DELETE` that removed nothing from one that removed a
   thousand, rests on the node and relationship counters, and those cross the
   protocol faithfully. `GRAPH.md § Write Counters: What a Statement Changed` is
   canonical for the constraint and for the upstream change that would let the
   split be restored.
5. **`counters` and the plan members are mutually exclusive, and that is a
   property of the engine rather than a rule imposed here.** A consumer may rely
   on never seeing `counters` beside `plan` or `profile`, and the two ways it
   could have happened are both closed upstream. An `EXPLAIN` executes nothing,
   so it has no applied effect to count and rule 1 omits the block. A `PROFILE`
   does execute, but the engine refuses a `PROFILE` of a writing statement
   outright rather than perform a write a caller asked only to have measured, so
   the only statement a `profile` tree can describe is one that changed nothing.
   `GRAPH.md § Query Plans: The EXPLAIN and PROFILE Prefixes`, rules 1 and 4, is
   canonical for what each prefix does and for that refusal.
6. **The counters fall inside the identity
   [Graph Client Result](#graph-client-result) requires, and take no exception
   from it.** They describe the statement and the graph, not the duration of the
   run that executed it, which is what the one documented exception — a
   `profile` tree's `timeNs` — is about. Two correct executions of one statement
   against one graph change the graph the same way, so the two surfaces publish
   the same `counters` object, key for key and value for value, or one of them
   is wrong.
7. **Every value is a count of an effect actually applied, never of one
   attempted.** A `MERGE` that matched counts nothing, because it never reached
   a creation. A `REMOVE` of a property the element does not carry counts
   nothing, and so does the removal of a label it does not bear. Values are
   therefore non-negative, and a counter is incremented at the point the write
   is applied rather than at the point it is requested.

---

## Graph Client Result

`rmp graph client` writes the result of the statement it sent to a running graph
server as JSON to stdout. **The shape is not a new one: it is exactly the shape
`rmp graph execute` writes for the same statement against the same graph**, and
this section exists to fix that identity and the mapping that makes it hold, not
to describe a second format.

1. A statement that produces result columns returns the `{columns, rows}` shape of
   [Graph Query Result](#graph-query-result).
2. A statement that produces none returns the object of
   [Graph Write Result](#graph-write-result).
3. A statement written with an `EXPLAIN` or `PROFILE` prefix returns the shape of
   [Graph Query Result](#graph-query-result) carrying its `plan` or `profile`
   member, whether or not it declares a result column.
4. All three are pretty-printed with two-space indentation and a trailing newline,
   consistent with all other JSON output (see
   [Implementation Notes](#implementation-notes)). A statement that changed the
   graph adds, to whichever of the first two shapes it produced, the `counters`
   member of [Graph Query Counters](#graph-query-counters); rule 6 below is
   canonical for its standing against the identity.

**The identity is a requirement, not an observation.** For any statement and any
graph, the bytes `rmp graph client` writes to stdout are the bytes
`rmp graph execute` writes for that statement against that graph, with the single
exception rule 5 below states. The same requirement binds `rmp graph execute`
itself when it reaches a running server rather than the store, which it does
whenever one is listening (see `GRAPH.md § Server Resolution`): the surface a
statement was executed through is not observable in the JSON. A caller may
therefore parse one shape and change nothing when a server is started or stopped.

**The identity governs the success output and the exit code, and it does not
reach the error line.** What it fixes is the bytes a statement writes to stdout
and the code it exits with; the plain-text diagnostic a *failing* statement
writes to stderr is outside it, and is fixed per condition by the error tables of
`COMMANDS.md` rather than here. The distinction is not academic at the pinned
engine: exactly one condition — a field the engine refuses as too long for its
durable format — currently prints a different stderr line on each path, because
the protocol carries no code that distinguishes it and the server replaces its
message. `GRAPH.md § Field Length Limits`, rule 13, is canonical for that
departure, for the remedy that ends it, and for what stays identical meanwhile:
the sentinel, the exit code, and the fact that nothing was written. No other
condition diverges, and nothing above is weakened for a statement that succeeds.

5. **The identity binds every value that is a property of the statement and the
   graph. It does not bind `timeNs`, and no implementation could make it.** That
   key is a wall-clock measurement of the execution that produced it (see
   [Graph Plan Node](#graph-plan-node), rules 11 and 12). `rmp graph execute` and
   `rmp graph client` are two executions, so they measure two durations, and two
   correct measurements of two runs are not obliged to agree. A figure that
   differs between them is not a defect in either: it is the key doing what it
   exists to do.

   **What a caller may rely on is therefore everything but the clock.** For any
   statement, the two surfaces publish the same key set, the same structure, the
   same member order, the same plan-tree shape, and the same value under every
   key other than `timeNs` — including `rows`, `dbHits`, `rowsRemovedByFilter`,
   the estimate pair, and every member of `counters`, each of which describes the
   statement and the graph rather than the run's duration. A consumer comparing
   the two surfaces compares everything except the clock, and a statement
   carrying neither prefix, or carrying `EXPLAIN`, has no `timeNs` at all and is
   therefore identical in every byte.

   **This is narrower than the guarantee stated in the paragraph above, and it is
   narrow on purpose.** A wider claim would be one the specification could not
   hold: a test written against it verbatim would compare two clocks and fail
   whenever they disagreed, which is a flaky test asserting a false requirement
   rather than a real one going unchecked. What is genuinely identical is the
   **mapping** — one realisation over one representation, as the paragraphs below
   establish — and the mapping is exactly what governs every key the exception
   does not name.

6. **The `counters` member is inside the identity, without exception, and the
   one figure the protocol cannot carry twice is folded on BOTH paths so that it
   stays inside.** The counters describe what the statement did to the graph, so
   two correct executions of one statement against one graph must report the
   same object; nothing about them measures the run, which is the only ground
   rule 5's exception stands on. The protocol, however, carries a single property
   counter and has no second channel for a property removal, so a served result
   can distinguish eleven of the engine's twelve counters and never the twelfth.
   The published shape resolves that by folding the two property figures into one
   member, `propertiesWritten`, on the direct path as well as the served one —
   which is a property of the shared mapping and not a divergence between the
   surfaces. Publishing the split where it happens to be available would make the
   direct path publish a key the served path cannot, turning a protocol limit
   into a second exception to this section's central requirement; and publishing
   a removal under a key named for an assignment would state an effect that did
   not happen. [Graph Query Counters](#graph-query-counters), rule 4, is
   canonical for the choice, and
   `GRAPH.md § Write Counters: What a Statement Changed` for the constraint
   behind it.

**The counters cross the protocol as summary metadata, and land the same way the
plan does.** They arrive in the same terminal success metadata that already
carries the notifications and the plan — the statistics map a driver turns into
its result summary — rather than as rows, and the client inverts that encoding
onto **the engine's own counter model, not onto JSON**. The step from there to
the published object is then the one realisation both surfaces run. Three
consequences follow, and each is a requirement:

1. **A key the protocol's statistics encoding adds is not added to the JSON.**
   The protocol names its counters in its own spelling and carries an extra
   boolean saying that the statement contained updates; the published object
   names the eleven keys of [Graph Query Counters](#graph-query-counters) and no
   others. The boolean is not published, because the presence of the block
   already carries it.
2. **An absent counter is an absent counter on both paths.** The protocol omits
   a counter whose value is zero, and the client MUST carry that omission
   through rather than read the missing key as `0` and publish it. Publishing it
   would break the byte identity in the same stroke, since the direct path omits
   it too.
3. **A statement that changed nothing carries no statistics at all**, and the
   client publishes no `counters` key for it, exactly as the direct path
   publishes none. The protocol omits the whole map in that case, so the two
   paths agree without either having to test for the empty object.

**Why the identity holds, and where the work is.** A result that crossed the
protocol arrives in the protocol's own encoding rather than as the engine's
values, so something has to map it back. What it is mapped back onto is **the
engine's value model, not JSON**: the client inverts the protocol encoding and
hands its caller the same values an in-process engine would have handed it. The
step from those values to the published JSON is then the one realisation every
surface shares (see
[One Realisation of the Mapping](#one-realisation-of-the-mapping)), run unchanged
over one representation — which is what makes the identity above a property of the
code rather than a coincidence that has to be policed. Mapping straight to JSON
here instead would have created another copy of both mappings, free to drift from
the one that already exists, and the identity would then be an assertion rather
than a consequence.

The mapping this section fixes is therefore the protocol's encoding onto the
values [Property-Type Mapping](#property-type-mapping) and
[Graph element mapping](#graph-element-mapping) already govern. Those two sections
state the JSON once; the client's obligation is to land on them:

| Value carried over the protocol | JSON it MUST produce |
|---------------------------------|----------------------|
| A string, an integer, a floating-point number, a boolean, or a null | The representation [Property-Type Mapping](#property-type-mapping) gives it, including that representation's treatment of a non-finite floating-point value as JSON `null` |
| A byte string | A base64-standard-encoded JSON string, as [Property-Type Mapping](#property-type-mapping) requires |
| A temporal value | The string [Property-Type Mapping](#property-type-mapping) gives that temporal kind, under **Temporal values** — which is per kind, is not an instant in UTC, and has no fixed-width fractional second |
| A node | `{"id": <int>, "labels": [<string>, ...], "properties": {<object>}}` |
| A relationship | `{"id": <int>, "type": "<string>", "startId": <int>, "endId": <int>, "properties": {<object>}}` |
| A path | `{"nodes": [<node>, ...], "relationships": [<relationship>, ...]}` |
| A list or a dictionary | A JSON array or object whose members are mapped by these same rules, recursively |

Rules:

1. **`id`, `startId`, and `endId` carry the same identifiers, and the same
   caveat.** They are the engine's internal storage identifiers, emitted as JSON
   numbers, ephemeral, and never to be persisted or used as long-lived references
   (see [Graph element mapping](#graph-element-mapping), rule 4). A protocol node
   may carry a second, string-shaped element identifier alongside the numeric one;
   this shape does not publish it, because publishing it would make a result
   depend on which path carried it.
2. **A key the protocol's encoding adds is not added to the JSON.** The mapping is
   defined by the table above and by the two sections it points at, and nothing
   else appears. A field the protocol carries which those sections do not name is
   dropped rather than passed through.
3. **A value the mapping cannot represent is a failure of the statement, not a
   silently different result.** The client does not substitute a placeholder for a
   value it could not map; it fails with `utils.ErrGraphServer` and exit code 1, so
   that a caller never reads a result that is quietly not the one the graph holds.

**The query plan crosses the protocol the same way, and lands the same way.** A
statement written with an `EXPLAIN` or `PROFILE` prefix comes back with its plan
in the protocol's own summary metadata — one field for a plan and a second for a
profile, which is where a Bolt driver already looks for them — rather than as
rows. The client inverts that encoding onto **the engine's plan representation,
not onto JSON**, and the step from there to the published object is the one
[Graph Plan Node](#graph-plan-node) fixes, run by the same code the direct path
runs. Three consequences follow, and each is a requirement:

1. **Which member the object carries is decided by which metadata field carried
   the tree**, so a plan reported over the protocol cannot arrive under the key a
   measurement belongs to, or the reverse.
2. **A key the protocol's plan encoding adds is not added to the JSON**, exactly
   as rule 2 above requires of a value. The protocol nests some of the plan's
   figures inside its own argument map and names them in its own spelling; the
   published object names the nine keys of [Graph Plan Node](#graph-plan-node)
   and no others.
3. **An absent figure stays absent.** The protocol omits a storage-access count
   nobody measured rather than sending a zero, and the client MUST carry that
   omission through instead of reading the missing field as `0`. A client that
   defaults it would publish a measurement the graph never took, and would break
   the byte identity against the direct path in the same stroke (see
   [Graph Plan Node](#graph-plan-node), rule 8).

---

## Graph View Data

The web interface's graph data endpoint (`GET /roadmaps/{name}/graph/data`, see
`WEB.md § Graph Data Endpoint`) returns a roadmap's knowledge graph as a single
JSON object describing its nodes and edges, shaped for an interactive node-link
visualisation. The endpoint runs the statement it is given exactly as
`rmp graph execute` runs it (see `GRAPH.md § Engine Construction and Lifecycle`),
so a statement that writes is committed and checkpointed like any other.

The endpoint accepts two optional URL query parameters, `q` (the Cypher statement
to run, URL-encoded) and `limit` (the node-limit value), that the graph page's
query bar sends. When `q` is absent or empty, the endpoint runs the default query
`MATCH (n) OPTIONAL MATCH (n)-[r]->(m) RETURN n, r, m`, which yields the same
full-graph view a request with no parameters always produced (backward
compatible). A user-supplied `q` is executed as written, and the resolved `limit`
is applied as a `LIMIT` clause only when the statement both lacks a top-level
`LIMIT` of its own and is a form that admits a `LIMIT` clause. The full parameter
contract, the limit-injection and suppression rules, and the failure modes are
specified in `WEB.md § Graph Data Endpoint` and
`WEB.md § Query-Bar Error Handling`; this section specifies the response shapes —
the successful one below, which is identical regardless of which statement
produced it, and the error one in [Error Shape](#error-shape) — and not the
behaviour that selects between them.

This is the canonical specification of the graph view-data shape. It **reuses**
the graph-element and property-type conventions already defined in
[Graph Query Result](#graph-query-result); it does not introduce a new element
encoding. The reuse binds the code as well as the page: the endpoint calls the one
realisation of that mapping rather than holding a copy of it (see
[One Realisation of the Mapping](#one-realisation-of-the-mapping)).

### Shape

```json
{
  "nodes": [
    {"id": 17, "labels": ["Spec"], "properties": {"key": "user-authentication"}},
    {"id": 18, "labels": ["Code"], "properties": {"path": "internal/auth/jwt.go"}}
  ],
  "edges": [
    {"id": 42, "type": "IMPLEMENTED_BY", "startId": 17, "endId": 18, "properties": {}}
  ]
}
```

Field reference:

| Field | Type | Description |
|-------|------|-------------|
| `nodes` | array of object | One object per node in the graph, using the Node shape from [Graph element mapping](#graph-element-mapping): `{"id", "labels", "properties"}`. |
| `edges` | array of object | One object per relationship in the graph, using the Relationship shape from [Graph element mapping](#graph-element-mapping): `{"id", "type", "startId", "endId", "properties"}`. |

Rules:

1. `nodes` and `edges` are always present **in a successful response**. An empty
   graph returns `{"nodes": [], "edges": []}` (empty arrays, never `null`), and so
   does any statement whose result carries no node and no edge — a count, a schema
   listing, or a write with no `RETURN` clause among them (see
   `WEB.md § Query-Bar Error Handling`, rule 9). A roadmap that has never used the
   `graph` command is treated as an empty graph and returns this empty object; it
   is not an error (see `GRAPH.md § Persistence Layout`, rule 2). A response that
   is not successful carries neither field: it carries the object in
   [Error Shape](#error-shape) below, or, for an internal read error, no JSON at
   all.
2. Each node object follows the Node mapping and each edge object follows the
   Relationship mapping in [Graph element mapping](#graph-element-mapping),
   including the `properties` object, whose values follow the
   [Property-Type Mapping](#property-type-mapping) recursively.
3. Every `startId` and `endId` in `edges` references the `id` of a node present
   in the same `nodes` array, so the visualisation can resolve every edge's
   endpoints from the one response. The endpoint builds the response by collecting
   every node and relationship that appears anywhere in the query result
   (recursively, deduplicated by `id`) and then **dropping** any relationship whose
   start or end node was not collected, rather than inventing a synthetic endpoint;
   this drop is what guarantees this invariant for an arbitrary user-supplied query
   (see `WEB.md § Graph Data Endpoint`).
4. `id`, `startId`, and `endId` are GoGraph's internal storage identifiers
   (`uint64`), **ephemeral** and not stable business keys, exactly as defined in
   [Graph element mapping](#graph-element-mapping) rule 4. They are used only to
   correlate nodes and edges **within this response** for rendering; they MUST
   NOT be persisted or treated as long-lived references. Stable identity comes
   from node and edge properties (for example `key` or `name`), per
   `GRAPH.md § Multi-Layer Modelling Conventions`.
5. The result is pretty-printed with two-space indentation and a trailing
   newline, consistent with all other JSON output (see
   [Implementation Notes](#implementation-notes)).
6. This response is the sole data source for the graph page's labels sidebar
   (`WEB.md § Graph Labels Sidebar`): the page derives the node-label inventory and
   counts from the nodes' `labels` arrays and the edge-type inventory and counts
   from the edges' `type` field, client-side, from this same response. That feature
   consumes this shape and does not change it; no field is added here for it.

### Error Shape

A request the graph data endpoint refuses, and a statement that fails, are
answered with this object in place of the node-and-edge object above. The endpoint
returns it for each of the two query-bar failures, always with HTTP
`400 Bad Request`. The status, the failure classes, and the rule that selects
between them are specified in `WEB.md § Query-Bar Error Handling`, which is
canonical for them; this section is canonical for the shape.

```json
{
  "error": "invalid limit: 7",
  "kind": "invalid_limit"
}
```

Field reference:

| Field | Type | Description |
|-------|------|-------------|
| `error` | string | The human-readable reason. The graph page shows it in place as its failure message. |
| `kind` | string | The machine-readable failure class. `WEB.md § Query-Bar Error Handling`, rule 4, enumerates the value set and is canonical for it; this file does not repeat it. |

Rules:

1. Both fields are always present and both are always strings. The object carries
   these two fields and no others, and it carries neither `nodes` nor `edges`.
2. `kind` carries one value per failure class, drawn from the closed set
   `WEB.md § Query-Bar Error Handling`, rule 4, publishes; that rule is canonical
   for which values exist and how many, and this file deliberately does not carry
   a second copy of the list, so the two cannot disagree. What each value means is
   fixed there too: an invalid `limit`, and a statement that failed once running. A
   statement cancelled for exhausting the endpoint's query time budget is an
   execution failure and carries the execution value; the budget adds no value of
   its own (see `WEB.md § Graph Query Time Budget`).
3. `error` is written to be read by a person and is not parsed. For an execution
   failure it carries the engine's own diagnostic text, so a given statement
   produces the same diagnostic here as it produces on the CLI (see
   `GRAPH.md § Error Handling and Exit Codes`, rule 2). For an invalid limit it
   names the rejected value.
4. The object is serialized exactly as every other response of this endpoint is:
   HTML-safe, so `<`, `>`, and `&` are escaped (see `WEB.md § Graph Data Endpoint`),
   pretty-printed with two-space indentation, and terminated by a newline (see
   [Implementation Notes](#implementation-notes)).
5. This is the endpoint's error contract for the two query-bar failures only. An
   internal read error — a graph store that cannot be opened, for example — is
   answered HTTP `500` as on every other route of the web interface and does not
   carry this shape (see `WEB.md § Query-Bar Error Handling`, rule 6).

---

## Task Detail Data

The web interface's task detail endpoint (`GET /roadmaps/{name}/tasks/{id}/data`,
see `WEB.md § Task Detail Endpoint`) returns one task's full field set together
with that task's comments, as a single JSON object. The read-only task detail
modal fetches it when a user opens a task, and fills the page's single modal
element with the result (see `WEB.md § Task Detail Modal`). The endpoint reads the
roadmap's `project.db` **read-only**: it writes nothing, alters no schema, and
produces no audit entry.

This is the canonical specification of the task detail response shape. It
**composes** the two object shapes this file already defines and introduces no new
field definitions of its own: the task object is the [Task](#task) shape and each
comment is the [Task Comment](#task-comment) shape. A value therefore carries the
same field name, the same type, and the same null convention here as it does in
the corresponding CLI output.

### Shape

```json
{
  "task": {
    "id": 42,
    "title": "Implement JWT authentication system",
    "status": "DOING",
    "type": "USER_STORY",
    "functional_requirements": "Users must be able to authenticate securely",
    "technical_requirements": "Create authentication module with JWT token support",
    "acceptance_criteria": "Functional login with 24h valid tokens; proper error handling",
    "created_at": "2026-03-12T10:00:00.000Z",
    "started_at": "2026-03-12T10:30:00.000Z",
    "tested_at": null,
    "closed_at": null,
    "completion_summary": null,
    "commit_open": "5f93b51",
    "commit_close": null,
    "parent_task_id": null,
    "priority": 9,
    "severity": 0,
    "subtask_count": 0,
    "depends_on": [],
    "blocks": []
  },
  "comments": [
    {
      "id": 12,
      "task_id": 42,
      "type": "FINDING",
      "body": "The JWT middleware rejects tokens whose exp claim is exactly the current second.",
      "created_at": "2026-03-12T11:15:00.000Z",
      "updated_at": null
    },
    {
      "id": 13,
      "task_id": 42,
      "type": "DECISION",
      "body": "Token expiry is compared with !time.Now().Before(exp), so the boundary second expires.",
      "created_at": "2026-03-12T11:40:00.000Z",
      "updated_at": "2026-03-12T14:05:00.000Z"
    }
  ]
}
```

**Notes:**

1. The object carries exactly two members, `task` and `comments`. No other
   top-level member is added.
2. `task` is one [Task](#task) object, whose fields are defined for the `Task`
   model in `MODELS.md § Task`. Every field the task detail modal displays is
   present, including the long free-text fields (`functional_requirements`,
   `technical_requirements`, `acceptance_criteria`, and `completion_summary`), the
   lifecycle timestamps, and the two commit hashes (`commit_open` and
   `commit_close`).
3. `comments` is an array of [Task Comment](#task-comment) objects, whose fields
   are defined for the `TaskComment` model in `MODELS.md § Task Comment`.
4. **Order.** The `comments` array is ordered **oldest first**: `created_at`
   ascending, with the comment `id` ascending as the tie-breaker. This is exactly
   the order `rmp task comment-list` returns for the same task (see
   `DATABASE.md § Comments`), and exactly the order the modal's timeline presents,
   so one ordering rule serves the CLI and the web interface alike.
5. **Completeness.** Every comment of the task is present. The endpoint applies no
   type filter, no count limit, and no pagination.
6. **A task with no comment yields `[]`, never `null`**, consistent with the
   empty-array rule in [Implementation Notes](#implementation-notes).
7. Free-text values preserve the author's interior line breaks as `\n` escapes in
   JSON, exactly as they do in CLI output.
8. The response is JSON-encoded and is never interpolated into HTML by the server.
   Because these values reach the browser as data rather than as server-rendered
   markup, the client that renders them MUST write every value into the DOM as
   text and never as markup; that requirement is specified in
   `WEB.md § Task Detail Modal`.

---

## Implementation Notes

1. **No extra fields**: Do not include extra fields in JSON responses
2. **Consistent order**: Maintain field order as defined in examples
3. **Pretty-print**: All JSON output must be human-readable with 2-space indentation (`  `) and no prefix. This applies to every command that produces JSON to stdout.
4. **UTF-8**: All strings in UTF-8
5. **Numbers**: Use JSON number format (not strings)
6. **Empty arrays**: Represent as `[]` (not `null`)

---

## AI Agent Contract

The CLI exposes a machine-readable description of its entire command
surface, intended for AI agents and other automated callers. The
contract is emitted by `rmp --ai-help` and the equivalent forms
documented in `COMMANDS.md § AI Help`. This section is the canonical
specification of the JSON payload.

### Design principles

The contract is designed to be **self-contained, exhaustive, and
sufficient for an AI agent to operate the CLI without consulting any
other document**. Concretely:

1. The contract is fully self-describing. It declares its own schema
   version, the tool identity, and the binary version that produced it.
2. The contract is deterministic. Repeated invocations against the same
   binary version return byte-identical output (modulo the `generated_at`
   field, which is omitted from the contract for that reason).
3. The contract is exhaustive. Every command, every subcommand, every
   flag, every enum value, every exit code that the binary can emit is
   represented.
4. The contract is derived from the same internal command registry that
   feeds the plain-text help. The contract and the plain-text help can
   never disagree. See `ARCHITECTURE.md § AI Agent Contract Generation`.

### Top-level shape

```json
{
  "schema_version": "1.0.0",
  "tool": {
    "name": "rmp",
    "display_name": "Groadmap",
    "binary_version": "1.3.0",
    "description": "CLI for managing technical roadmaps in SQLite."
  },
  "conventions": { ... },
  "exit_codes": [ ... ],
  "enums": { ... },
  "global_flags": [ ... ],
  "commands": [ ... ],
  "common_workflows": [ ... ],
  "pitfalls": [ ... ]
}
```

### Field reference

| Field | Type | Description |
|-------|------|-------------|
| `schema_version` | string | Semantic version of the contract schema itself. Bumped only when the structure of the contract changes. Independent of the binary version. |
| `tool.name` | string | Canonical binary name (`rmp`). |
| `tool.display_name` | string | Human-readable product name (`Groadmap`). |
| `tool.binary_version` | string | Bare semver string of the `rmp` binary that produced this contract (e.g. `"1.3.0"`). This is the value extracted from the application version constant, NOT the formatted output of `rmp --version` (which is plain text such as `Groadmap version 1.3.0`). The contract MUST strip the `Groadmap version ` prefix and emit only the semver. |
| `tool.description` | string | One-sentence summary of what the tool does. |
| `conventions` | object | Cross-cutting invariants the agent must observe. See below. |
| `exit_codes` | array of object | Catalogue of every exit code the binary can emit. |
| `enums` | object | Map of enum name to enum definition. Mirrors `MODELS.md § Enums`. |
| `global_flags` | array of object | Flags accepted at the top level (e.g. `--help`, `--version`, `--ai-help`). |
| `commands` | array of object | One entry per top-level command family (`roadmap`, `task`, `sprint`, `audit`, `backlog`, `stats`, `graph`). |
| `common_workflows` | array of object | Canonical end-to-end command sequences an agent is expected to perform. See `common_workflows` below. |
| `pitfalls` | array of object | Known mistakes agents make against this CLI, each paired with the correct alternative. See `pitfalls` below. |

#### `conventions` object

```json
{
  "stdout_on_success": "json",
  "stderr_on_error": "plain_text",
  "json_indent": 2,
  "charset": "utf-8",
  "locale": "C",
  "datetime_format": "ISO 8601 UTC with milliseconds, suffix Z",
  "datetime_example": "2026-05-24T14:30:00.000Z",
  "roadmap_flag": {
    "short": "-r",
    "long": "--roadmap",
    "required_for": "every command except roadmap list/create/remove, web, and the help/version/ai-help commands"
  },
  "list_separator": ",",
  "ai_agent_env_var": {
    "name": "AI_AGENT",
    "enable_value": "1",
    "effect": "Emits a one-line hint to stderr on every invocation pointing to --ai-help."
  }
}
```

#### `exit_codes` array entry

```json
{
  "code": 4,
  "name": "EXIT_NOT_FOUND",
  "meaning": "Resource not found.",
  "sentinel": "utils.ErrNotFound"
}
```

The contract reproduces, in full, the table in
`ARCHITECTURE.md § Exit Codes`. The `sentinel` field is omitted for exit
codes that are not produced by wrapping a sentinel error (e.g. `0`,
`130`).

#### `enums` map entry

Key: enum name (e.g. `TaskStatus`, `TaskType`, `SprintStatus`).

**Comment types are exposed as two keys, not one.** `MODELS.md § Comment Type`
defines a single `CommentType` enum and two valid subsets of it, one per entity.
A `flags[].enum` value is a single key into this map, so the contract carries the
two subsets as two separate entries: `TaskCommentType`, with the seven values a
task comment accepts, and `SprintCommentType`, with the four values a sprint
comment accepts. The `--type` flag of a `task` comment subcommand names
`TaskCommentType`; the `--type` flag of a `sprint` comment subcommand names
`SprintCommentType`. There is no `CommentType` key carrying all seven values for
both families, so an agent reading the contract cannot offer a sprint a type that
a sprint rejects (see `HELP.md § Comment subcommand help specifics`).

```json
"TaskStatus": {
  "values": [
    {"value": "BACKLOG",   "description": "Task is in backlog, not assigned to a sprint."},
    {"value": "SPRINT",    "description": "Task is assigned to a sprint. Set automatically by `sprint add-tasks`; cannot be set manually via `task stat`."},
    {"value": "DOING",     "description": "Task is being worked on."},
    {"value": "TESTING",   "description": "Task is in testing phase."},
    {"value": "COMPLETED", "description": "Task is complete."}
  ],
  "state_machine_reference": "STATE_MACHINE.md § Task State Machine"
}
```

**The audit enums are published in full.** `AuditOperation` carries every value in
`ValidAuditOperations` — the canonical catalogue of `DATABASE.md § audit Table` — and
`AuditEntityType` carries `TASK` and `SPRINT`. Five rules apply to
`AuditOperation` specifically:

1. **No value is omitted.** `audit list --operation` accepts exactly the values in
   this enum, so a value missing from the contract is a filter an agent cannot
   discover. This includes the four LEGACY values.
2. **A LEGACY value says so in its own `description`.** The description MUST state
   that no command writes the value and that it exists so the entries already
   carrying it stay filterable, and it MUST name the operations that replaced it. An
   agent that reads only the value list would otherwise choose a LEGACY operation
   when composing a filter for current activity and get an empty result with no
   explanation.
3. **Every value names the entity it is recorded against.** Each element of
   `AuditOperation.values` carries an `entity_type` member holding `TASK` or
   `SPRINT`: the value an audit entry's own `entity_type` field holds on a row
   carrying that operation (see `§ Audit Entry`). The member is present on every
   value of this enum, and is never `null` and never empty. Without it an agent
   composing an `audit list` filter cannot tell whose history an operation belongs
   to, and the only thing left to infer it from is the operation's name;
   `HELP.md § Audit operation entity-type classification` states why that
   inference is not permitted and what both published surfaces read instead.

   The entity type cannot travel inside `description`. That string carries the
   catalogue entry's own text from `DATABASE.md § audit Table`, which rule 5 below
   alters in two mechanical ways and no others, so the string has no room for a fact
   the catalogue does not state; and prose is not a member a consumer can read a
   value from without parsing it.

4. **Every value states whether a command still writes it.** Each element of
   `AuditOperation.values` carries a `legacy` member holding a boolean: `true` on
   the four LEGACY values rule 2 governs, `false` on every other value. The member
   is present on every value of this enum, and is never `null`. The member and the
   `description` state the same fact and MUST agree: a value carrying `true` is a
   value whose description says no command writes it.

   Both forms are required because they serve different consumers, and the prose
   form alone does not serve the second one. Rule 2's sentence explains to a reader
   why the operation returns nothing and which operations to filter instead.
   `--ai-help` exists to be read by machine, and recovering the LEGACY status from
   that sentence makes a consumer depend on wording this specification is free to
   change, so the same fact travels separately in a member the consumer can test.
   An agent composing a filter over the operations still in use reads one field
   rather than searching a string.

   `legacy` and `entity_type` come from the same single declaration, the one
   `HELP.md § Audit operation entity-type classification` rule 2 requires and which
   carries both facts. The contract derives neither of them from the value's name
   and neither of them from its `description`.

5. **Every description is the catalogue entry's own text.** A value's `description`
   is not composed here. It is transcribed from that operation's entry in the
   canonical catalogue of `DATABASE.md § audit Table`, verbatim, backticks
   included, and the transcription alters the entry in exactly two ways:

   a. A full stop is appended when the catalogue entry does not already end in one.
      A catalogue entry is a list item and most end without terminal punctuation,
      while a `description` on the contract is a sentence.
   b. The six comment operations — `TASK_COMMENT_CREATE`, `TASK_COMMENT_UPDATE`,
      `TASK_COMMENT_DELETE`, `SPRINT_COMMENT_CREATE`, `SPRINT_COMMENT_UPDATE`, and
      `SPRINT_COMMENT_DELETE` — additionally receive one fixed closing sentence,
      the same sentence on each, stating that the audit entry names the parent
      entity and that the comment's own id is never recorded. This is the fact an
      operation name hides: `TASK_COMMENT_DELETE` reads as an operation on a
      comment, and the entry is recorded against the task.

   Nothing else is reworded, shortened, expanded, or reordered. The two surfaces
   are tied byte for byte in both directions: rewriting a description on the
   contract without making the same change to the catalogue fails the test suite,
   and so does editing the catalogue alone. What this costs the writer of the
   catalogue is stated at the catalogue itself (`DATABASE.md § The Catalogue Entry
   Is Also the Published Contract Description`).

   A single source for the text is the point of the rule. The catalogue and the
   contract describe the same operations for two different readers, and a second
   wording maintained independently of the first would be free to drift from it
   without either surface looking wrong on its own. The text is nonetheless copied
   rather than read from the markdown at run time, because the binary must describe
   itself with no repository present; the copy is pinned by test rather than
   avoided.

   Rule 2's content requirement therefore lands on the catalogue entry. A LEGACY
   value's `description` states what that rule demands because the entry it is
   transcribed from states it, and it cannot be satisfied by wording introduced on
   the contract alone.

```json
"AuditOperation": {
  "values": [
    {"value": "TASK_STATUS_DOING",     "entity_type": "TASK",   "legacy": false, "description": "Task entered `DOING` via `task stat`, one row per task. The row carries the `commit_hash` supplied as `--commit-open`."},
    {"value": "SPRINT_ADD_TASK",       "entity_type": "SPRINT", "legacy": false, "description": "Task added to a sprint via `sprint add-tasks`; one row per task, against the sprint, naming the task in `related_entity_id`."},
    {"value": "TASK_COMMENT_CREATE",   "entity_type": "TASK",   "legacy": false, "description": "Comment added to a task via `task comment-add` (logged against the parent task). The audit entry names the parent entity; the comment's own id is never recorded."},
    {"value": "TASK_STATUS_CHANGE",    "entity_type": "TASK",   "legacy": true,  "description": "LEGACY. The single status-change operation the five `TASK_STATUS_*` operations above replace. It survives on rows the 1.11.0 to 1.12.0 migration could not reclassify (see `VERSION.md § Migration 1.11.0 to 1.12.0`)."}
  ]
}
```

Every `description` above is the operation's catalogue entry from
`DATABASE.md § audit Table`, and rule 5 is visible in the rows: each has gained the
full stop its catalogue entry lacks, and `TASK_COMMENT_CREATE`, being one of the six
comment operations, has gained the closing sentence about the parent entity as well.
Nothing else separates these strings from the catalogue's.

**`entity_type` and `legacy` appear only where they apply.** Each is a member of an
`enums[].values[]` element, not a member every such element carries: each is present
on every value of `AuditOperation` and absent from the values of every other enum.
This follows the convention the contract already uses for members that do not
apply to an entry, where `commands[].flags[]` omits `range`, `min_length`, and
`max_length` rather than publishing them as `null`. Absent is the right form here
rather than `null`: a `TaskStatus` value is not recorded against an entity at all
and no `TaskStatus` value is LEGACY, so keys whose values would be `null` on every
enum but this one would suggest that the contract has a general notion of an enum
value's entity and a general notion of an enum value's LEGACY status, and it has
neither.

Adding these members widens a published contract, which is a deliberate change and
not a detail. A consumer that reads the members it knows is unaffected; a consumer
that enumerates members sees exactly two new keys, both on the values of exactly
one enum.

**An enum carries a reference member only when it has a state machine.** An enum
definition has exactly two members. `values` is carried by every enum.
`state_machine_reference` is carried by `TaskStatus` and `SprintStatus`, and by no
other enum: `AuditOperation`, `AuditEntityType`, `TaskCommentType`,
`SprintCommentType`, `TaskSort`, and `TaskType` each carry `values` alone.
`state_machine_reference` is also the only reference member this contract defines,
so an enum that carries no state-machine reference carries no reference of any kind.
The two lists above are exhaustive on purpose. A member that is absent is
indistinguishable, to the consumer reading it, from a member that was never
specified, so a consumer cannot detect a reference this contract failed to publish;
naming every enum on both sides of the rule is what lets that consumer stop looking.

The asymmetry is deliberate, and `AuditOperation` is not an exception to a rule the
other seven enums follow: six enums carry `values` alone and two carry a reference,
and what separates them is the state machine, not the enum's subject matter. A
reference is published when the values cannot be used correctly without the text it
points at. The values of `TaskStatus` and `SprintStatus` are the states of a
machine, and which value may follow which is not in the value list: an agent holding
the list still cannot tell whether it may move a task from `BACKLOG` to `TESTING`,
which is not a legal transition. `STATE_MACHINE.md § Valid Transitions`, inside the
section this enum's `state_machine_reference` names, is where that answer is.
`AuditOperation` has no such gap. Its values are the arguments
`audit list --operation` accepts, and each value already carries what an agent
needs in order to choose one: a `description`, which rule 5 above transcribes from
the catalogue entry verbatim, an `entity_type`, and a `legacy` flag. A reference to
`DATABASE.md § audit Table` would point the consumer at text this contract already
carries value by value, wrapped in schema material — the DDL, the column list, the
index rationale — that a consumer filtering audit entries has no use for at run
time.

**The asymmetry is not to be closed by adding a reference member.** Giving
`AuditOperation`, or any other enum, a second kind of reference widens a published
contract: every consumer that enumerates the members of an enum definition sees a
new key. That is a change to be decided on its own terms, with a stated consumer
that needs the referenced text at run time, and not a gap to be filled because two
enums out of eight look different from the rest. This specification names the
catalogue for its own reader — `The audit enums are published in full` above does
exactly that, and so does rule 5 — and naming it in this document is not a statement
that the contract carries the same reference as data.

**Every published value carries a description.** Each element of `values` MUST
carry a `description` that is not empty after trimming whitespace. The rule
applies to every enum the contract publishes, not only to the one shown above,
and it holds however many values the enum has. A value published without a
description is a value an agent can see but cannot interpret, and the agent gets
no signal that anything is missing: it cannot tell what the value means, or how
it differs from the value beside it. The contract is the only description of the
CLI such an agent has, so the description travels with the value rather than
being left to the value's name.

#### `global_flags` array entry

Same shape as `commands[].flags[]` (see below). Global flags include at
least `--help` / `-h`, `--version` / `-v`, and `--ai-help`.

#### `commands` array entry

```json
{
  "name": "task",
  "aliases": ["t"],
  "summary": "Manage tasks within a roadmap.",
  "description": "Long-form description covering when to use this family, how it relates to sprints and the backlog, and any cross-cutting rules.",
  "prerequisites": [
    "An existing roadmap selected via -r/--roadmap."
  ],
  "subcommands": [ ... ]
}
```

#### Single-action commands (no subcommands)

Some commands have no subcommands: `ai-help`, `stats`, and `web`. These
commands MUST use the SAME nested shape as every other command. The
contract MUST NOT flatten a single-action command's flags, usage, exit
codes, or other subcommand-level fields onto the top-level command
object. Instead, the command object carries a `subcommands` array with
exactly ONE element that describes the single action, using the
`subcommands` array entry shape defined below.

For a single-action command, the one-element `subcommands` entry repeats
the command's own `name` (for example, the `web` command object contains
one subcommand also named `web`). This guarantees that an agent can
traverse every command uniformly through `commands[].subcommands[]`
without special-casing the commands that happen to have a single action.

#### Empty-array serialization

Whenever a contract field of array type has no elements, it MUST
serialize as an empty JSON array `[]`, never as `null`. This applies to
every array-typed field, including `subcommands`, `aliases`,
`prerequisites`, `positional_arguments`, `mutual_exclusion_groups`, and
`examples`. This is the contract-level statement of the general rule in
`Implementation Notes` (Empty arrays). Examples in this specification
MUST NOT show `null` in place of an empty array.

#### `subcommands` array entry

```json
{
  "name": "create",
  "aliases": ["new"],
  "summary": "Create a new task in the roadmap.",
  "description": "Long-form description.",
  "usage": "rmp task create -r <roadmap> --title <string> --type <TaskType> --priority <0-9> --functional-requirements <string> --technical-requirements <string> --acceptance-criteria <string> [options]",
  "positional_arguments": [],
  "flags": [
    {
      "long": "--title",
      "short": null,
      "type": "string",
      "required": true,
      "default": null,
      "enum": null,
      "max_length": 255,
      "min_length": 1,
      "description": "Task title."
    },
    {
      "long": "--type",
      "short": null,
      "type": "enum",
      "required": true,
      "default": null,
      "enum": "TaskType",
      "description": "Task type. See enums.TaskType for the value list."
    },
    {
      "long": "--priority",
      "short": "-p",
      "type": "integer",
      "required": true,
      "default": null,
      "range": {"min": 0, "max": 9},
      "description": "Priority, 0 (lowest) to 9 (highest)."
    }
  ],
  "mutual_exclusion_groups": [],
  "stdout_on_success": {
    "kind": "object",
    "schema": {"id": "integer"},
    "example": {"id": 42}
  },
  "side_effects": {
    "database": "INSERT into tasks and audit; wrapped in one transaction.",
    "filesystem": "None.",
    "network": "None."
  },
  "idempotent": false,
  "exit_codes": [0, 2, 3, 4, 6],
  "prerequisites": [
    "An existing roadmap selected via -r/--roadmap."
  ],
  "examples": [
    {
      "title": "Create a user story with priority 9",
      "cmd": "rmp task create -r myproject --title \"Login flow\" --type USER_STORY --priority 9 --functional-requirements \"User can log in\" --technical-requirements \"JWT tokens\" --acceptance-criteria \"Login succeeds with valid creds\"",
      "stdout": "{\"id\": 42}",
      "stderr": "",
      "exit": 0
    },
    {
      "title": "Missing required flag",
      "cmd": "rmp task create -r myproject",
      "stdout": "",
      "stderr": "Error: required parameter missing: --title\n\nAI agents: run `rmp --ai-help` for a machine-readable command contract.",
      "exit": 2
    }
  ]
}
```

### Field reference: flag entry

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `long` | string | yes | Long flag including the `--` prefix. |
| `short` | string or null | yes | Short flag including the `-` prefix, or `null` when no short form exists. |
| `type` | string | yes | One of `string`, `integer`, `boolean`, `enum`, `list:string`, `list:integer`, `date`. |
| `required` | boolean | yes | True when the flag must be supplied on every invocation of the subcommand; false otherwise. A flag that is mandatory only for some values of a positional argument carries `required: false`, and its `description` states the condition under which it becomes mandatory. The `--commit-open` and `--commit-close` flags of `task stat` are the case in point: each is mandatory for one target status and rejected for every other, so neither can be marked required for the subcommand as a whole. |
| `default` | any or null | yes | Default value when the flag is omitted; `null` when there is no default. |
| `enum` | string or null | yes | Name of the enum (key into the top-level `enums` map) when `type` is `enum`; otherwise `null`. |
| `range` | object or absent | no | `{min, max}` when the flag is a bounded integer. |
| `max_length` | integer or absent | no | Maximum string length when applicable. |
| `min_length` | integer or absent | no | Minimum string length when applicable. |
| `description` | string | yes | One-sentence description of the flag's purpose. |
| `mutually_exclusive_with` | array of string or absent | no | Long flag names that cannot be combined with this one. |
| `stdin_fallback` | boolean or absent | no | `true` when the flag's value is read from standard input if the flag is omitted. Present and `true` on the `--query` flag of `graph execute` and on the `--body` flag of the `comment-add` and `comment-edit` subcommands of the `task` and `sprint` families. When `stdin_fallback` is `true`, `required` is `false` (the value may come from stdin instead), but the value is mandatory from one source or the other; supplying neither is an error. The flag's own `description` states any condition under which the fallback does not apply: on `comment-edit` the body is read from stdin only when `--type` is absent as well, so a type-only edit does not wait for input. See `GRAPH.md § Cypher Input Source and Precedence` and `COMMANDS.md § Comment Body Input Source and Precedence`. |

### Field reference: subcommand-level fields

| Field | Type | Description |
|-------|------|-------------|
| `usage` | string | One-line usage signature. |
| `reads_stdin` | boolean or absent | `true` when the subcommand reads standard input as an input source: `graph execute`, and the `comment-add` and `comment-edit` subcommands of the `task` and `sprint` families. Absent or `false` for every other subcommand, which ignores stdin. |
| `positional_arguments` | array of object | Each entry: `{name, type, required, description}`. |
| `mutual_exclusion_groups` | array of array of string | Each inner array is a set of long flag names of which at most one may be supplied. |
| `stdout_on_success.kind` | string | One of `object`, `array`, `empty`. `empty` is used by mutating commands that return no body. |
| `stdout_on_success.schema` | object or null | Field-name to type map for `object`; element-type for `array`; `null` for `empty`. |
| `stdout_on_success.example` | any or null | A canonical example payload; `null` for `empty`. |
| `side_effects.database` | string | Plain-language description of DB writes; `"Read-only."` when none. |
| `side_effects.filesystem` | string | Plain-language description of FS writes; `"None."` when none. |
| `side_effects.network` | string | Always `"None."` for Groadmap; field kept for forward compatibility. |
| `idempotent` | boolean | True when repeated invocations with the same arguments produce the same end state. |
| `exit_codes` | array of integer | Exit codes the subcommand can emit, in ascending order. Always includes `0`. |
| `prerequisites` | array of string | Preconditions the agent must ensure before invoking (e.g. roadmap exists, sprint is open). |
| `examples` | array of object | Each entry: `{title, cmd, stdout, stderr, exit}`. Must contain at least one success example, and at least one failure example for every subcommand that has a failure mode (i.e. whose `exit_codes` include a non-zero code). A subcommand whose only exit code is 0 (e.g. `roadmap list`) is exempt from the failure-example requirement. |

### `common_workflows` array entry

Each entry documents one end-to-end sequence of `rmp` invocations that an
agent is expected to perform. The list is curated, not generated: it
captures the small number of recipes that account for the majority of
agent traffic against this CLI. Every command string referenced in a
workflow MUST resolve to a real command or subcommand documented in the
same contract under `commands`.

```json
{
  "name": "bootstrap_new_project",
  "description": "Create a fresh roadmap, open its first sprint, and seed the sprint with backlog items. Use when an agent is asked to set up tracking for a project that has no existing roadmap database.",
  "prerequisites": [
    "No roadmap with the target name exists yet (verify with `rmp roadmap list`)."
  ],
  "steps": [
    {
      "command": "rmp roadmap create <name>",
      "purpose": "Create the roadmap home directory ~/.roadmaps/<name>/ and its SQLite database project.db, and register the roadmap."
    },
    {
      "command": "rmp task create -r <name> --title <t> --type TASK --priority <p> --functional-requirements <fr> --technical-requirements <tr> --acceptance-criteria <ac>",
      "purpose": "Populate the backlog with one task per work item. Repeat once per task. Each invocation returns the new task ID on stdout."
    },
    {
      "command": "rmp sprint create -r <name> -t <title> -d <description> [--max-tasks <n>] [--order <n>]",
      "purpose": "Create the first sprint in PENDING state. Returns the new sprint ID on stdout."
    },
    {
      "command": "rmp sprint add-tasks -r <name> <sprint-id> <task-id-1,task-id-2,...>",
      "purpose": "Move selected backlog tasks into the sprint. Tasks transition BACKLOG to SPRINT automatically."
    },
    {
      "command": "rmp sprint start -r <name> <sprint-id>",
      "purpose": "Transition the sprint from PENDING to OPEN so `rmp task next` will return its tasks."
    }
  ],
  "expected_outcome": "One roadmap exists, one sprint is in OPEN state, and that sprint contains the selected tasks in SPRINT status."
}
```

The full `common_workflows` array MUST contain at least the following
entries. Each follows the shape shown above.

| `name` | Purpose |
|--------|---------|
| `bootstrap_new_project` | Create a roadmap, seed the backlog, open the first sprint, and start it. |
| `plan_next_sprint` | From an existing roadmap with a populated backlog, choose the next batch of work and open a new sprint for it. |
| `close_active_sprint_and_open_next` | Mark the current OPEN sprint as CLOSED, handle any unfinished tasks, and promote the next PENDING sprint. |
| `reprioritise_backlog` | Inspect the backlog, change priorities on selected tasks, and verify the resulting order. |
| `move_task_between_sprints` | Transfer one or more tasks from one sprint to another without altering their status. |
| `complete_task_with_summary` | Walk a task from SPRINT through DOING and TESTING to COMPLETED, recording a completion summary. |

#### Field reference: `common_workflows` entry

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Short stable identifier in `snake_case`. Used by agents to refer to the workflow. |
| `description` | string | yes | One or two sentences explaining what the workflow does and when to use it. |
| `prerequisites` | array of string | yes | Preconditions that must hold before step 1 runs. Empty array when the workflow has no preconditions. |
| `steps` | array of object | yes | Ordered list of steps. Each step has `command` and `purpose`. The array MUST contain at least one step. |
| `steps[].command` | string | yes | The exact `rmp` invocation, with placeholder tokens (e.g. `<name>`, `<sprint-id>`) for caller-supplied values. The base command and subcommand MUST exist in this contract. |
| `steps[].purpose` | string | yes | One sentence stating why this step is necessary in the sequence. |
| `expected_outcome` | string | yes | One sentence describing the end state once the final step succeeds. |

### `pitfalls` array entry

Each entry documents a mistake that an agent driving this CLI is likely
to make, the correct alternative, and a pointer back to the relevant
command or concept already specified in the contract. The list is
curated against observed and anticipated failure modes; it is not
generated from the command registry.

```json
{
  "id": "manual_sprint_status",
  "description": "Manually setting a task's status to SPRINT via `task stat` is rejected. The SPRINT status is owned by sprint operations and is set atomically when a task is added to a sprint.",
  "wrong_example": "rmp task stat -r myproject 42 SPRINT",
  "correct_example": "rmp sprint add-tasks -r myproject 7 42",
  "reference": "sprint add-tasks; see also enums.TaskStatus and the SPRINT entry."
}
```

The full `pitfalls` array MUST contain at least the following entries.
Each follows the shape shown above.

| `id` | What the agent gets wrong |
|------|---------------------------|
| `roadmap_identified_by_name` | Treating the roadmap as having a numeric ID. Roadmaps are identified by `name` only; every non-`roadmap` command needs `-r <name>` / `--roadmap <name>`. |
| `manual_sprint_status` | Attempting `task stat <id> SPRINT`. SPRINT is set only by `sprint add-tasks`. |
| `delete_non_backlog_task` | Calling `task remove` on a task that is not in `BACKLOG`. Move the task back to `BACKLOG` first (via `sprint remove-tasks` or `task reopen`). |
| `add_tasks_to_closed_sprint` | Calling `sprint add-tasks` against a sprint in `CLOSED` state. Use a `PENDING` or `OPEN` sprint, or create a new one. |
| `next_without_open_sprint` | Calling `rmp task next` while no sprint is in `OPEN` state. Open a sprint with `sprint start` first. |
| `complete_with_open_dependencies` | Transitioning a task to `COMPLETED` while it has incomplete subtasks or declared dependencies. Complete the blockers first or remove the dependency. |
| `summary_on_non_completed_transition` | Passing `--summary` on any transition other than `→ COMPLETED`. The flag is accepted only for that one transition. |
| `missing_commit_hash_on_transition` | Running `task stat <ids> DOING` without `--commit-open`, or `task stat <ids> COMPLETED` without `--commit-close`. Each flag is mandatory on its own transition and rejected on every other one. The agent must supply the hash itself; `rmp` never reads a git repository to obtain it. |
| `partial_reorder` | Passing only a subset of a sprint's task IDs to `sprint reorder`. The command requires the complete ordered set; partial reorders are rejected. |
| `non_iso_date_input` | Supplying dates in a non-ISO 8601 format to filter flags such as `--since` / `--until` / `--created-since` / `--created-until`. The contract's `conventions.datetime_format` is the authoritative input format; `YYYY-MM-DD` is also accepted by date-range filters. |
| `assume_partial_batch_success` | Assuming a batch operation may partially succeed. All batch operations are fail-fast: either every ID is valid and the operation runs, or no change is made. |
| `invalid_roadmap_name` | Creating a roadmap with characters outside `^[a-z0-9_-]+$` or longer than 50 characters. Validate the name client-side before issuing `roadmap create`. |
| `parse_modification_stdout` | Parsing stdout after a modification command (status change, priority change, reorder, delete). Such commands deliberately return empty stdout on success; rely on the exit code. |

#### Field reference: `pitfalls` entry

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | yes | Stable `snake_case` identifier. Used by agents to refer to the pitfall. |
| `description` | string | yes | One or two sentences explaining the mistake and why the CLI rejects it. |
| `wrong_example` | string | yes | A concrete `rmp` invocation (or short shell snippet) that triggers the pitfall. |
| `correct_example` | string | yes | A concrete `rmp` invocation that achieves the user's actual intent. |
| `reference` | string | yes | The command, enum, or convention in this contract that governs the rule (e.g. `sprint add-tasks`, `enums.TaskStatus`, `conventions.datetime_format`). |

### Scope filtering

When invoked with a scope narrower than the whole CLI, the contract is
filtered as follows:

- `rmp <command> --ai-help`: the `commands` array contains exactly one
  entry, that command, with all its subcommands. `enums`, `exit_codes`,
  `conventions`, `global_flags`, `schema_version`, and `tool` remain
  unchanged.
- `rmp <command> <subcommand> --ai-help`: the `commands` array contains
  exactly one entry, that command, whose `subcommands` array contains
  exactly one entry, that subcommand. All other top-level fields remain
  unchanged.

The filtering rule guarantees that any contract slice is still
self-contained: an agent receiving a subcommand-scoped contract still
has the enums it references and the exit-code catalogue it relies on.
