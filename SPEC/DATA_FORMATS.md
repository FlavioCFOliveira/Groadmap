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
- Input-related errors (missing parameters, wrong types, unknown commands or subcommands) additionally show the **specific help of the command or subcommand** that was invoked
- Uses standard Unix exit codes for script integration
- This rule governs **command output**. It does not govern the HTTP responses of
  the running `web` server, which are not command stdout or stderr (see the
  server-startup bullet above). The graph data endpoint answers a rejected or
  failed query with a JSON error object, specified in
  [Graph View Data](#graph-view-data), **Error Shape**.

### Input

**Application inputs are via CLI parameters. Exactly two flag values may also
arrive on standard input: the Cypher query of the `graph` subcommands, and the
comment body of the comment subcommands of the `task` and `sprint` families.**

- No JSON input
- No configuration files
- No interactive input
- **Standard input:** used as an alternative source for exactly two flag values,
  and by no other command:
  - the `--query` Cypher string of the `graph` subcommands (see
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
- Standard input: the full stdin contents are read as the Cypher query when
  `rmp graph <subcommand>` is invoked without `--query`, and as the comment body
  when a comment subcommand is invoked without `--body`.

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
  "specialists": "go-elite-developer,security-expert",
  "started_at": null,
  "tested_at": null,
  "closed_at": null,
  "completion_summary": null,
  "parent_task_id": null,
  "priority": 9,
  "severity": 0,
  "subtask_count": 0,
  "depends_on": [],
  "blocks": []
}
```

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

```json
{
  "id": 1,
  "operation": "TASK_STATUS_CHANGE",
  "entity_type": "TASK",
  "entity_id": 42,
  "performed_at": "2026-03-12T15:30:00.000Z"
}
```

A comment operation is recorded against the parent entity, never against the comment: `TASK_COMMENT_CREATE` carries `entity_type: "TASK"` and the owning task's id in `entity_id`. See `DATABASE.md § audit Table`.

---

## Graph Query Result

The read graph subcommands (`rmp graph query` and `rmp graph search`) return the
result of a Cypher query as a single JSON object to stdout. The shape exposes the
result's columns and its rows, mirroring the GoGraph engine result, which exposes
the ordered column names (`Columns()`) and an iterable sequence of records.

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

Field reference:

| Field | Type | Description |
|-------|------|-------------|
| `columns` | array of string | The ordered return-column names of the query (the engine's `Columns()`). One entry per returned expression, in the order the query declares them. |
| `rows` | array of array | One inner array per record, in the order the engine yields records. Each inner array has exactly `columns.length` cells, positionally aligned with `columns`. |

Rules:

1. `columns` and `rows` are always present. A query that matches nothing returns
   its declared `columns` and an empty `rows` array (`[]`), never `null`.
2. A query that returns no columns (for example a write run through a read path,
   which the guard rail forbids) is not a valid read result; read subcommands
   always declare at least one return column.
3. Each row cell is a JSON value produced by the property-type mapping below.
4. The result is pretty-printed with two-space indentation and a trailing
   newline, consistent with all other JSON output (see
   [Implementation Notes](#implementation-notes)).

### Property-Type Mapping

GoGraph property values carry Go types. Each maps to JSON as follows:

| GoGraph value type | JSON representation | Notes |
|--------------------|---------------------|-------|
| `string` | JSON string | UTF-8, as-is. |
| `int64` | JSON number (integer) | Emitted without a decimal point. JSON numbers are IEEE-754 doubles in many consumers; values outside the safe integer range (beyond ±2^53) may lose precision on the consumer side. The CLI emits the exact integer; precision loss, if any, is the consumer's concern. |
| `float64` | JSON number | Emitted in the standard Go float format. `NaN`, positive infinity, and negative infinity are not valid JSON numbers; when the engine produces any of them, they are emitted as JSON `null`. |
| `bool` | JSON boolean | `true` / `false`. |
| `time.Time` | JSON string | ISO 8601 UTC with milliseconds and a `Z` suffix, identical to every other timestamp in Groadmap (see [Dates - ISO 8601 with UTC](#dates---iso-8601-with-utc)). |
| `[]byte` | JSON string | Base64-standard-encoded (RFC 4648) so arbitrary bytes survive JSON transport. |
| absent / null property | JSON `null` | A returned expression that has no value is `null`. |

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

## Graph Write Result

The write graph subcommands (`rmp graph create`, `rmp graph update`,
`rmp graph delete`) mirror their query's `RETURN` clause. The output shape
depends on whether the query returns anything:

1. **With a `RETURN` clause:** the output is the standard read-result shape
   defined in [Graph Query Result](#graph-query-result) — a `columns` array and a
   `rows` array — populated with the elements the query returns. For example, a
   `CREATE ... RETURN n` query returns the created node in the `{columns, rows}`
   shape.
2. **Without a `RETURN` clause:** the output is exactly:

```json
{"ok": true}
```

The GoGraph engine exposes only the result's columns and an iterable record
sequence; it reports no mutation or affected-element counter. There is therefore
**no** count field in the write result, and the CLI does not attempt to compute
one. The `{"ok": true}` object is the success signal for a write query that
returns no data.

Field reference (no-`RETURN` case):

| Field | Type | Description |
|-------|------|-------------|
| `ok` | boolean | Always `true`. Confirms the write transaction committed successfully. |

Examples:

A write query without `RETURN`:

```json
{"ok": true}
```

A write query that ends with `RETURN n` (same shape as a read result):

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
  ]
}
```

---

## Graph View Data

The web interface's graph data endpoint (`GET /roadmaps/{name}/graph/data`, see
`WEB.md § Graph Data Endpoint`) returns a roadmap's knowledge graph as a single
JSON object describing its nodes and edges, shaped for an interactive node-link
visualisation. The endpoint reads the graph **read-only**, the same way
`rmp graph query`/`search` do (see `GRAPH.md § Engine Construction and
Lifecycle`); it never writes and never checkpoints.

The endpoint accepts two optional URL query parameters, `q` (the Cypher query to
run, URL-encoded) and `limit` (the node-limit value), that the graph page's query
bar sends. When `q` is absent or empty, the endpoint runs the default query
`MATCH (n) OPTIONAL MATCH (n)-[r]->(m) RETURN n, r, m`, which yields the same
full-graph view a request with no parameters always produced (backward
compatible). User-supplied `q` is validated as **read-only** before execution
(reusing the graph guard-rail) and the resolved `limit` is applied as a `LIMIT`
clause only when the query both lacks a top-level `LIMIT` of its own and is a
statement form that admits a `LIMIT` clause. The full parameter contract, the
read-only guard-rail, the limit-injection and suppression rules, and the
failure modes are specified in `WEB.md § Graph Data Endpoint` and
`WEB.md § Query-Bar Error Handling`; this section specifies the response shapes —
the successful one below, which is identical regardless of which query produced
it, and the error one in [Error Shape](#error-shape) — and not the behaviour that
selects between them.

This is the canonical specification of the graph view-data shape. It **reuses**
the graph-element and property-type conventions already defined in
[Graph Query Result](#graph-query-result); it does not introduce a new element
encoding.

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
   graph returns `{"nodes": [], "edges": []}` (empty arrays, never `null`). A
   roadmap that has never used the `graph` command is treated as an empty graph and
   returns this empty object; it is not an error (see
   `GRAPH.md § Persistence Layout`, rule 2). A response that is not successful
   carries neither field: it carries the object in [Error Shape](#error-shape)
   below, or, for an internal read error, no JSON at all.
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

A request the graph data endpoint refuses, and a query that fails, are answered
with this object in place of the node-and-edge object above. The endpoint returns
it for each of the three query-bar failures, always with HTTP `400 Bad Request`.
The status, the failure classes, and the rules that select between them are
specified in `WEB.md § Query-Bar Error Handling`, which is canonical for them; this
section is canonical for the shape.

```json
{
  "error": "query rejected: not read-only",
  "kind": "not_read_only"
}
```

Field reference:

| Field | Type | Description |
|-------|------|-------------|
| `error` | string | The human-readable reason. The graph page shows it in place as its failure message. |
| `kind` | string | The machine-readable failure class: `not_read_only`, `invalid_limit`, or `execution`. |

Rules:

1. Both fields are always present and both are always strings. The object carries
   these two fields and no others, and it carries neither `nodes` nor `edges`.
2. `kind` takes exactly three values, one per failure class in
   `WEB.md § Query-Bar Error Handling`: `not_read_only` for a query the read-only
   guard-rail rejected before execution, `invalid_limit` for a `limit` that is not
   one of the six allowed values, and `execution` for a query that was accepted as
   read-only and then failed once running. A query cancelled for exhausting the
   endpoint's query time budget is an execution failure and carries `execution`;
   the budget adds no fourth value (see `WEB.md § Graph Query Time Budget`).
3. `error` is written to be read by a person and is not parsed. For an execution
   failure it carries the engine's own diagnostic text, so a given query produces
   the same diagnostic here as it produces on the CLI (see
   `GRAPH.md § Error Handling and Exit Codes`, rule 2). For an invalid limit it
   names the rejected value.
4. The object is serialized exactly as every other response of this endpoint is:
   HTML-safe, so `<`, `>`, and `&` are escaped (see `WEB.md § Graph Data Endpoint`),
   pretty-printed with two-space indentation, and terminated by a newline (see
   [Implementation Notes](#implementation-notes)).
5. This is the endpoint's error contract for the three query-bar failures only. An
   internal read error — a graph store that cannot be opened, for example — is
   answered HTTP `500` as on every other route of the web interface and does not
   carry this shape (see `WEB.md § Query-Bar Error Handling`, rule 7).

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
    "specialists": "go-elite-developer,security-expert",
    "started_at": "2026-03-12T10:30:00.000Z",
    "tested_at": null,
    "closed_at": null,
    "completion_summary": null,
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
   `technical_requirements`, `acceptance_criteria`, and `completion_summary`) and
   the lifecycle timestamps.
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
    {"value": "SPRINT",    "description": "Task is assigned to a sprint. Set automatically; do not set manually."},
    {"value": "DOING",     "description": "Task is being worked on."},
    {"value": "TESTING",   "description": "Task is in testing phase."},
    {"value": "COMPLETED", "description": "Task is complete."}
  ],
  "state_machine_reference": "STATE_MACHINE.md § Task State Machine"
}
```

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
| `required` | boolean | yes | True when the flag must be supplied; false otherwise. |
| `default` | any or null | yes | Default value when the flag is omitted; `null` when there is no default. |
| `enum` | string or null | yes | Name of the enum (key into the top-level `enums` map) when `type` is `enum`; otherwise `null`. |
| `range` | object or absent | no | `{min, max}` when the flag is a bounded integer. |
| `max_length` | integer or absent | no | Maximum string length when applicable. |
| `min_length` | integer or absent | no | Minimum string length when applicable. |
| `description` | string | yes | One-sentence description of the flag's purpose. |
| `mutually_exclusive_with` | array of string or absent | no | Long flag names that cannot be combined with this one. |
| `stdin_fallback` | boolean or absent | no | `true` when the flag's value is read from standard input if the flag is omitted. Present and `true` on the `graph` subcommands' `--query` flag and on the `--body` flag of the `comment-add` and `comment-edit` subcommands of the `task` and `sprint` families. When `stdin_fallback` is `true`, `required` is `false` (the value may come from stdin instead), but the value is mandatory from one source or the other; supplying neither is an error. The flag's own `description` states any condition under which the fallback does not apply: on `comment-edit` the body is read from stdin only when `--type` is absent as well, so a type-only edit does not wait for input. See `GRAPH.md § Cypher Input Source and Precedence` and `COMMANDS.md § Comment Body Input Source and Precedence`. |

### Field reference: subcommand-level fields

| Field | Type | Description |
|-------|------|-------------|
| `usage` | string | One-line usage signature. |
| `reads_stdin` | boolean or absent | `true` when the subcommand reads standard input as an input source: the `graph` subcommands, and the `comment-add` and `comment-edit` subcommands of the `task` and `sprint` families. Absent or `false` for every other subcommand, which ignores stdin. |
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
