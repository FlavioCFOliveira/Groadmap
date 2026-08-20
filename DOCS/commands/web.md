# web

## Description

Start a read-only, browser-based view of the data the CLI manages. `rmp web` runs an HTTP server embedded in the `rmp` binary (Go standard-library `net/http`) that serves server-rendered HTML and embedded static assets, reading the same on-disk data under `~/.roadmaps/` that the CLI reads. The interface only presents data; it never changes it. The `rmp` CLI remains the sole write path.

The deliverable is fully self-contained: every asset required to render and operate the interface (HTML templates, the stylesheet, all client JavaScript including the vendored D3.js knowledge-graph library and the d3-sankey plugin, and the favicon) is embedded into the binary with `go:embed`. The interface renders and functions fully offline, references no content delivery network or any other remote origin, and the running server makes no outbound network request. The interface is responsive and mobile-first.

`rmp web` operates across all roadmaps: it lists every roadmap found under `~/.roadmaps/` and you drill into one from the browser. It is the one command that is exempt from the always-required-roadmap rule, so it does **not** accept the `-r` / `--roadmap` flag. It has no subcommands.

By default the server binds the loopback interface (`127.0.0.1`), so the read-only interface is reachable only from the local machine. Exposing it on the network is the explicit opt-in `--host 0.0.0.0` (all interfaces), which also prints a network-exposure warning to stderr at startup.

Unlike every other command, `rmp web` is long-lived: it keeps serving until interrupted. Sending `SIGINT` (`Ctrl+C`) or `SIGTERM` shuts the server down gracefully and the process exits 0.

## Synopsis

```
rmp web [options]
```

## Options

| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| - | `--host` | string | `127.0.0.1` | Bind host. The default binds loopback only, so the read-only interface is reachable only from the local machine. Exposing it on the network is the explicit opt-in `--host 0.0.0.0` (all interfaces), which also prints a network-exposure warning to stderr |
| - | `--port` | integer | `8787` | Bind port (0-65535). When `--port` is omitted and `8787` is in use, the server falls back to an OS-chosen ephemeral port so it still starts. With an explicit `--port` there is no fallback. `--port 0` requests an ephemeral port |
| - | `--no-open` | bool | false | Do not launch a browser; still start the server and print the served URL |
| `-h` | `--help` | bool | false | Show command help |

`rmp web` accepts no positional arguments. An unknown flag or an unexpected positional argument is an input error (exit code 2).

## Output

On successful startup the served URL is printed to stdout as a single JSON object, so the address is machine-readable even when no browser is opened:

```json
{
  "url": "http://127.0.0.1:8787"
}
```

The `url` reflects the actual bound host and port, including an ephemeral port chosen by the fallback. While running, the server serves HTML pages and a JSON graph-data endpoint; those are HTTP responses, not stdout output of the command.

## Routes and Pages

All routes serve `GET` and `HEAD` only. Any other HTTP method on any route returns HTTP `405`.

| Route | Purpose | Response |
|-------|---------|----------|
| `/` | Roadmap index: every roadmap under `~/.roadmaps/`, with links to each roadmap's sprints landing page and graph page (empty-state message when none) | HTML |
| `/roadmaps/{name}` | Roadmap sprints page and landing page: that roadmap's sprints in three tabs (Próximos / Actual / Concluídos, Actual default), each tab carrying a count badge in the colour of the sprint status it groups; every sprint rendered through the same sprint card and linking to its own page. Selecting a roadmap on the index lands here | HTML |
| `/roadmaps/{name}/tasks` | Roadmap tasks board: every task of that roadmap, of any status, laid out as a Kanban board of five fixed status columns with a count badge on each; narrowed by the header search and the three header filters through the `q`, `type`, `priority` and `severity` query parameters; clicking a card opens a read-only modal with all task fields and that task's comments timeline. See [The Tasks Board](#the-tasks-board) | HTML |
| `/roadmaps/{name}/sprints/{id}` | Dedicated sprint page: all sprint details, the sprint's member tasks as a three-column board in planned execution order, and the sprint's own Comments card; each task card opens the task detail modal. See [The Sprint Board](#the-sprint-board) | HTML |
| `/roadmaps/{name}/audit` | Roadmap audit log page: that roadmap's full audit log (columns ID, Operation, Entity Type, Entity ID, Performed At), ordered by Performed At descending (most recent first), paginated at 100 entries per page via the `page` query parameter (1-based, default 1; out-of-range or non-numeric values are clamped to the nearest valid page) with Previous/Next controls and a "Page X of Y" indicator | HTML |
| `/roadmaps/{name}/graph` | Interactive knowledge-graph visualisation (D3.js; selectable Networks-section layouts via a dropdown, default Mobile patent suits; pan/zoom, touch, tap-to-inspect), driven by the editable Cypher query bar above the graph card. See [The Knowledge-Graph Query Bar and Data Endpoint](#the-knowledge-graph-query-bar-and-data-endpoint) | HTML |
| `/roadmaps/{name}/graph/data` | The graph's nodes and edges for the visualisation, produced by the read-only Cypher query in the `q` parameter (the full-graph default query when absent) under the endpoint's 5-second query time budget; a rejected, invalid or failed request answers HTTP `400` with an `error`/`kind` JSON body | JSON |
| `/static/...` | Embedded static assets (CSS, JS, vendored Tabler framework and D3.js + d3-sankey, fonts) | static file |

`{name}` is validated against the roadmap-name rules (regex `^[a-z0-9_-]+$`, max 50 characters) before it is used to build any filesystem path; a name that fails validation, or a roadmap that does not exist, returns HTTP `404`. A request for a `/static/...` asset that is not embedded returns HTTP `404`. These HTTP statuses are distinct from the process exit codes below.

## The Tasks Board

`/roadmaps/{name}/tasks` presents every task of the roadmap as a read-only **Kanban board**. The board is the page's only task presentation: the page renders no task table and offers no alternative table view. Its structure follows a GitLab issue board — columns that stand for states, cards that stand for work items, and a task count on each column header — and departs from it in interaction, because this board moves nothing and edits nothing.

### Columns

The board has exactly five columns, one for each task status, ordered left to right by the flow of the task state machine:

1. `BACKLOG`
2. `SPRINT`
3. `DOING`
4. `TESTING`
5. `COMPLETED`

The columns are fixed. All five are always present, in that order, whatever the roadmap's data contains, and a column holding no task is still rendered with its own in-column empty state. Neither the set of columns nor their order depends on the data, and each column title is the status identifier exactly as the enum spells it, in upper case.

Each column header carries a Tabler badge with the number of tasks that column is showing, coloured by the status the column stands for, so the badge states a count in text and a status in colour; a column with no card shows the count `0`. Every task appears in exactly one column, the column of its own `status`, so on an unnarrowed board the five counts sum to the roadmap's total number of tasks.

Within a column the cards appear in descending `priority` and, for tasks of equal priority, ascending `created_at` — the same order `rmp task list` returns by default. The board introduces no second sort.

### Every task, never a page of them

The page reads **every** task of the roadmap. The read carries no limit, no page size and no truncation, and the board has no pagination: whatever the roadmap holds, the board shows. There is no page window, no `page` parameter and no page clamping on this route.

The `-l, --limit` default that sizes `rmp task list` output is deliberately not applied here. That default sizes the output of one command invocation, where a caller who wants more asks for more and can see that the listing was cut. The board offers no such affordance and it does not merely list: it counts each column and prints that count as a statement of fact about the roadmap. Under a partial read those counts would be wrong and would still be presented as true. Reading every task is therefore a correctness requirement of this page, not a performance choice.

(The audit log page, `/roadmaps/{name}/audit`, is a different page and **is** paginated; its own `page` parameter and clamping rules are in the route table above.)

### Cards

Each card presents one task, in this order:

1. A **reference line** with the task reference `#<id>` and the task's `type`, both in muted text. The type carries no colour.
2. The task **`title`**, as the card's prominent main content.
3. A **`priority` badge** and a **`severity` badge**, each naming the value it carries with a one-letter prefix — `P` for the priority and `S` for the severity, so a task of priority `5` and severity `3` shows `P5` and `S3` — and coloured by the band that value falls in. The prefix is a label, not part of the value: the colour still follows the number alone.
4. A **metadata footer** showing only the indicators the task actually has: the sprint it belongs to (identified by the sprint's `title` together with `Sprint #<id>`, as plain text rather than a link), its `specialists`, its number of subtasks, its number of `depends_on` entries, its number of `blocks` entries, and its number of comments.

An indicator whose value is absent, empty or zero is not rendered at all: no dash, no placeholder, no empty slot. A task with none of the six shows no metadata footer. The card shows **no status badge**, because the column it sits in already states the task's status.

The whole card is a `<button>`: a pointer click, a touch tap, and the keyboard (Enter and Space) all open the read-only task detail modal for that task, which carries every field of the `Task` model and that task's comments timeline. The modal's data is fetched when the user opens the task, so it adds no query to the page's own read.

### Header controls

The page header's actions column carries four controls, and no others — a search box and three filter dropdowns:

| Control | Kind | Query parameter | Matching |
|---------|------|-----------------|----------|
| Search tasks | text input | `q` | Case-insensitive substring over the task's `title` and its `#<id>` reference |
| Type | dropdown (`Any type` plus the ten `TaskType` values) | `type` | **Equality**: the task's `type` is equal to the selected value |
| Min priority | dropdown (`Any priority` plus `1` to `9`) | `priority` | **Threshold**: the task's `priority` is `>= n` |
| Min severity | dropdown (`Any severity` plus `1` to `9`) | `severity` | **Threshold**: the task's `severity` is `>= n` |

Each control carries a real, programmatically associated label naming what it acts on, and each is reachable and operable from the keyboard. A dropdown's first option is a value meaning *no filter on this dimension*, not the control's name.

- **The three filters are the three dimensions `rmp task list` already filters by** — `-y, --type`, `-p, --priority` and `--severity` — and each keeps the meaning the flag of the same name carries, so one parameter name means one thing across the two surfaces. The type comparison is exact against the enum's own upper-case spelling; the thresholds start at `1` and not at `0`, because a threshold of `0` admits every task and is therefore the unfiltered board, which already has its own option and its own URL form.
- **What the search matches** is only what identifies a task on its card: the `title` and the reference written with its leading `#`, so both `42` and `#42` find task 42. `specialists` and every other field are excluded, because matching an attribute is the job of the three filters.
- **The criteria combine conjunctively.** A task is shown when it satisfies *every* active criterion, and a board with no active criterion shows every task. `?q=cache&type=BUG&priority=7` shows the `BUG` tasks of priority `7` or above whose title or `#<id>` reference contains `cache`, and no other task. Narrowing a criterion can only shrink the shown set, never grow it.
- **There is deliberately no status filter.** The five columns already are the status, so a status filter would perform narrowing the layout has performed already — and it could not do so without either leaving excluded columns present and stating a false count of `0`, or dropping columns and contradicting the rule that all five are always present.

### A narrowed board is a shareable URL

Every control is a URL query parameter, so the address bar always describes the board on screen and that address reproduces it.

- **Setting a control updates the URL in place.** Typing in the search box or selecting a filter value replaces the current history entry rather than pushing a new one, so the browser Back button leaves the board instead of stepping backwards through the control row.
- **An inactive control leaves no parameter.** While the search box is empty or a dropdown sits on its no-filter option, that parameter is removed from the URL rather than left present and empty. Clearing every control restores the full board with its true counts and leaves the bare page URL.
- **A cold load arrives already narrowed.** When the page is requested with any combination of `q`, `type`, `priority` and `severity`, the server applies all of them and the document it sends already carries the narrowing in its final state: the narrowed column counts, the in-column empty states, the no-match message where nothing matches, and each control already showing the value that produced the board. For any roadmap and any combination of values, the board reached by setting the controls on the page and the board reached by opening that URL cold are the same board — the same cards, in the same columns, in the same order, with the same counts.
- **Order and repetition.** The four parameters are independent of each other and of their position in the query string. A repeated parameter is read as its first occurrence.

### Nothing a control carries is an error

An unknown or malformed value applies **no filter on that dimension** and the board is rendered exactly as though that parameter were absent. This covers a `type` that is not one of the ten values (including one that differs only in case), a `priority` or `severity` that is not an integer or falls outside `1` to `9`, a value carrying a sign or surrounding spaces, a parameter present with an empty value, and a parameter the server cannot decode. The dimensions are independent under this rule: an unusable `type` leaves an accepted `priority` applied.

Every string is likewise a valid search term: a term that matches nothing renders an empty board, and a `q` the server cannot decode is treated as absent. No value of any of the four parameters produces an error page or changes the route's status codes; the page answers HTTP `200` whatever they carry.

### What narrowing does to the board

A task that does not satisfy every active criterion is not shown, and everything the board states then refers to the shown set rather than to the roadmap. As the user types or changes a filter, the cards, the counts, the empty states and the no-match message are updated together:

- Each column shows only its matching cards, in the same order.
- **Each column's count is the number of cards that column is showing**, so the counts narrow with the board.
- The five columns remain present and in order. Neither searching nor filtering ever drops, hides or reorders a column.
- A column left with no matching card shows its ordinary in-column empty state.
- When no task matches at all, the board says so with a clear message beside the five empty columns, rather than leaving the user to interpret silence. A roadmap that holds no task is a different condition and reads differently: it shows the five in-column empty states alone, because it is the state of the roadmap and not the result of any control.

### Read-only, like every other page

The board offers no drag-and-drop and no control of any other kind that moves a task between columns, reorders cards, changes a task's status, or creates or edits a task or a column. The divergence from the GitLab issue board it is modelled on is deliberate: the inspiration is structural, never interactive. The header search and the three filters change only which of the already-read tasks the user is looking at — they write nothing, and are therefore not an exception to the read-only rule.

### Layout and read cost

The five columns are presented side by side. When they do not fit the viewport the board scrolls horizontally inside its own container, so the page itself never scrolls horizontally, and each column scrolls vertically and independently when its card list exceeds the available height. On narrow viewports each column keeps a minimum width at which its cards stay legible.

Every column is `19rem` wide and never narrower than `17rem`, and no column grows or shrinks away from that width: a column stands for a state and not for a volume of work, so all five are the same width whatever number of tasks each holds, and the space a very wide viewport leaves beyond the board stays empty rather than being shared out among them. Inside a column, a card's body carries `0.75rem` of padding on all four sides instead of the `1rem` the UI framework gives a small card, because that padding is measure taken from the card's own text — its reference line, its title, its badges and its metadata footer — on a width the column has already narrowed. What the user presses is the whole card, so the hit target does not shrink with the padding. Both lengths are expressed in `rem` and therefore follow the reader's own text size.

Rendering the page performs three reads and no more: the unbounded read of every task of the roadmap, one grouped query for the comment count of every rendered task, and one grouped query resolving the sprint of every rendered task. The board issues no query per column and none per card, so the number of queries does not grow with the number of tasks. A search term and the three filters add nothing to this: on a cold load they are applied in memory over the rows already read, and narrowing in the browser issues no request at all, because every card is already in the document.

## The Sprint Board

`/roadmaps/{name}/sprints/{id}` presents the sprint's member tasks as a read-only **three-column board**, placed between the sprint's details card and its Comments card. It follows the same GitLab issue board model as the tasks board, and departs from it in the same way: the board moves nothing and edits nothing. The page renders no task table.

### Three columns, grouped by what the work is doing

The board has exactly three columns, presented left to right:

| Column | Holds the sprint's tasks whose status is |
|--------|------------------------------------------|
| `WAITING` | `BACKLOG` or `SPRINT` |
| `DOING` | `DOING` or `TESTING` |
| `CLOSED` | `COMPLETED` |

This is the same grouping the sprint status summary line at the top of the page already uses — pending, open, completed — rather than a second categorisation invented for the board. That is what makes the two agree by construction: the `WAITING` count is the summary line's `P`, the `DOING` count is its `A`, the `CLOSED` count is its `C`, and the three sum to its `T`. A task status enum with five closed values and three columns claiming all five means no member task can fall outside the board, so there is no fourth column and no "other" column.

A `BACKLOG` task can be a sprint member — `rmp task stat <id> BACKLOG` returns a task to the backlog without removing it from the sprint — which is why `WAITING` groups `BACKLOG` with `SPRINT` rather than showing `SPRINT` alone.

Each column header carries a Tabler badge with that column's task count, coloured by the canonical status of the group it holds: `WAITING` takes the colour of `SPRINT`, `DOING` that of `DOING`, and `CLOSED` that of `COMPLETED`. The canonical status is the one a task is normally in at that stage — a task waiting in a sprint is normally `SPRINT`, and `BACKLOG` there is the exceptional case.

All three columns are always present, in that order, whatever the sprint holds. A column with no task keeps its heading and its `0` count badge and shows its own in-column empty state, so a sprint with no member tasks renders an empty board rather than no board.

### Order within a column

Each column is ordered by the question that column answers:

| Column | Order | What you read at the top |
|--------|-------|--------------------------|
| `WAITING` | Planned in-sprint execution order (`position` ascending) | The next task to develop |
| `DOING` | `started_at` descending | The task that entered `DOING` most recently |
| `CLOSED` | `closed_at` descending | The task closed most recently |

`WAITING` is a queue of work not yet started, so the plan is what you want from it: it is the order `rmp sprint tasks` returns and `rmp sprint reorder` sets, and reordering the sprint through the CLI reorders that column. `DOING` and `CLOSED` are not queues but records of what has happened, so they lead with the most recent and a CLI reorder does not touch them. The board therefore holds more than one notion of order, deliberately: each column is ordered by the one thing that column is about.

A task in `TESTING` sits in the `DOING` column and is ordered by `started_at` as well — when it entered `DOING`, not when it entered `TESTING` — so one key orders the whole column.

Where two cards carry the same ordering timestamp, which is ordinary because a bulk `rmp task stat` stamps a batch alike, they fall back to the planned order; a card with no ordering timestamp sorts last in its column. The fallback is the plan because that is the only other order the sprint defines.

### Cards

Each card presents one member task on three lines:

1. The task **`title`**, leading the card.
2. The task **reference** `#<id>` on its own line, in muted text.
3. One line carrying both remaining groups: the **`priority` badge** and the **`severity` badge** at the leading edge, prefixed `P` and `S` and coloured by band exactly as on the tasks board, and the two **counters** at the trailing edge — the number of comments first, then the number of subtasks, each an icon followed by its number.

The badges and the counters share a line because they hold one kind of information — what the task is, and how much is attached to it — and because height is the scarce dimension in a column that is bounded and scrolls. Where the card is too narrow to hold both groups, the line wraps inside the card rather than overflowing it, so the card, its column and the page never scroll horizontally.

Both counters are always rendered, including when either or both are `0`, so a zero is a statement rather than a silence. This is where the sprint card departs from the tasks board card, which renders only the indicators a task has and keeps them in a footer of their own: the sprint card carries exactly two counters and can put them on the badge line, while the tasks board card carries six indicators of mixed kinds, two of them text with no zero to show, which cannot share that line. The counter order differs for the same reason — comments before subtasks here, subtasks before comments in the tasks board's footer. The card shows no status badge, because the column it sits in already states the status, and it shows no type, specialists or dependency counts: those are in the task detail modal the card opens.

The whole card is a `<button>`, so a pointer click, a touch tap, and the keyboard (Enter and Space) all open that task's read-only detail modal.

### Layout and read cost

The board takes a bounded height of `60vh`, never falling below the floor the interface uses for its full-height regions, and each column scrolls vertically and independently within it. It is deliberately not sized to the space the page body leaves: the page carries the sprint's details above the board and its Comments card below, and a board that grew with the sprint's task count would push those comments further away with every task added. When the three columns do not fit the viewport, the column strip scrolls horizontally inside its own container and the page itself never scrolls horizontally.

The three columns divide the width of the board equally and grow with the viewport, down to a floor of `17rem` below which the strip scrolls horizontally. This is where the two boards part: the tasks board's five columns keep a fixed `19rem`, because five columns divided across a viewport would each be narrow enough to hurt the card's measure, and that board is a view of a whole roadmap whose column count the status enum fixes. The minimum column width, the gap between columns and the card's body padding stay shared by both boards.

The page performs two comment reads whatever the number of member tasks: the sprint's own comment log, which the Comments card renders in full, and one grouped query for the comment count of every rendered card. Neither grows with the number of member tasks, and the board issues no query per column and none per card. The subtask counter costs no read of its own, because the sprint's member-task read already carries it.

## The Knowledge-Graph Query Bar and Data Endpoint

The knowledge-graph page renders its visualisation from a single editable Cypher query. Above the graph card sit three controls, left to right: a multi-line **query box** pre-filled on load with the default query `MATCH (n) OPTIONAL MATCH (n)-[r]->(m) RETURN n, r, m`, a **Search** button that re-runs the query in the box (Ctrl+Enter in the query box does the same), and a **node-limit dropdown** offering exactly `50`, `100`, `250`, `500`, `1000` and `3000`, with `100` selected by default.

Searching re-fetches `GET /roadmaps/{name}/graph/data` with the query box text as `q` and the dropdown value as `limit`, then re-renders the graph in the currently selected layout. Before it executes any query the endpoint validates that the query is read-only, using the same guard rail as `rmp graph query` and `rmp graph search`: a query whose masked normalization contains a writing clause (`CREATE`, `MERGE`, `SET`, `REMOVE`, `DELETE`, `DETACH DELETE`) or a DDL clause (`CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, `DROP CONSTRAINT`) is rejected and never runs.

### Query time budget: 5 seconds

The server's connection timeouts bound the connection, not the work a handler does. A client that sends its headers promptly, stays connected and reads the response as soon as it arrives satisfies all of them however long the server takes. This endpoint is the one route whose work a caller drives, because the caller writes the query, so it bounds its own work with an explicit budget:

- **The endpoint executes the caller's query under a deadline of 5 seconds.** The deadline covers the endpoint's execution of the query: the run against the engine's read path and the walk over the result that run produces. A query that would run for longer is cancelled.
- **The deadline is derived from the request's own context**, so the two sources of cancellation compose: a client that disconnects still cancels the query immediately, and a client that stays connected can no longer hold a query running beyond the budget.
- **The budget bounds the work; the node limit bounds only the result.** These are two different bounds, and neither substitutes for the other. The injected `LIMIT` clause bounds how many rows the query returns, and therefore how large the response is — it does not bound the work the engine performs to produce those rows. A query that aggregates over a Cartesian product, for instance, scans the whole product before any limit applies: its cost grows with the size of the store while its response stays a few bytes long. The time budget is the only bound on that work.
- **Exhausting the budget is a query execution failure**, surfaced with the page's existing "query failed to execute" message, in place. The page does not crash, the failure triggers no write and no navigation, the graph already shown is left as it is, and the user can edit the query, lower the node limit and search again.
- **No new status and no new error class.** A request whose query exceeded the budget is answered exactly as any other execution failure — HTTP `400` with `kind` `execution`. Exhausting the budget never terminates the process; the server keeps serving.
- **The budget is per request**, and cancelling a query changes nothing on disk: the store is opened read-only, so an abandoned query writes no data, runs no checkpoint and truncates no write-ahead log. A query that completes within the budget is served exactly as it was before the budget existed, with nothing truncated and no latency added.

### Statement forms that admit no `LIMIT`

The endpoint applies the resolved limit by appending a top-level `LIMIT <n>` clause on a **new line** (never after a space, which a trailing line comment would swallow). Injection is suppressed in exactly two cases:

1. **The query already carries a top-level `LIMIT`.** The user's own clause takes precedence and the dropdown value is not applied.
2. **The query is a statement form that admits no `LIMIT` clause.** Appending one to such a statement bounds nothing — it makes the statement fail in the **parser** — so a read form the guard rail accepts, and that `rmp graph query` runs, would be unusable through this endpoint. Two forms are affected, and each runs exactly as the caller wrote it:
   - **A schema-introspection command**: `SHOW INDEXES`, `SHOW INDEX`, `SHOW CONSTRAINTS` and `SHOW CONSTRAINT`, including a command carrying a `YIELD`, `WHERE` or `RETURN` tail. No form of the command admits a `LIMIT`, so a tail does not make one injectable.
   - **A standalone procedure call**: a statement whose first clause is `CALL` and that has no top-level `RETURN`. A `CALL ... YIELD ... RETURN ...` is projected and is an ordinary reading query for this rule, so the resolved limit is injected into it exactly as into a `MATCH ... RETURN`.

Both forms are recognised on the masked normalization of the query and anchored to the start of the statement, so a keyword appearing only inside a string literal, a comment or a backtick-quoted identifier does not affect the decision, and a `CALL` inside a larger query does not make the statement standalone. Suppression changes no operation class: both forms are read-only before the rule and after it, and the guard rail still decides alone whether they execute. A suppressed query is not capped by the node limit; what still bounds it is the time budget above.

### Failure responses

The endpoint answers each of its three failure classes with HTTP `400 Bad Request` and a JSON body of exactly two string fields, `error` and `kind`:

| `kind` | Raised when | `error` carries |
|--------|-------------|-----------------|
| `not_read_only` | The read-only guard rail rejected the query before execution, because it contains a writing clause or a DDL clause | The reason the query was rejected as not read-only |
| `invalid_limit` | The `limit` parameter is not one of the six allowed values. The endpoint rejects it rather than clamping it to the nearest allowed value, and the query is not executed | The rejected value |
| `execution` | The query was accepted as read-only but failed in the engine — invalid Cypher syntax, for example — or was cancelled for exhausting the 5-second budget, or was cancelled because the client disconnected | The engine's own diagnostic text, so the user reads the same diagnostic the CLI prints for that query. A query cancelled because the client disconnected names the cancellation rather than the budget, so the budget is never blamed for a caller that gave up |

- **One status, three kinds.** In each case the server can serve the route and refuses the request the caller made, which is what HTTP `400` states; the `kind` field is what distinguishes the three. A budget exhaustion is neither a `503` (the server is not overloaded and delay alleviates nothing, since the same query exhausts the same budget again) nor a `504` (the server is no gateway or proxy, and the engine is in-process).
- **Precedence.** One request can be wrong in more than one way. The `limit` is resolved before the guard rail runs, so a request carrying both an invalid `limit` and a query that is not read-only is answered `invalid_limit`. Under either order the request is rejected before the query runs and before the graph store is opened, so neither reads nor writes anything.
- **The boundary against `500`.** An internal read error — a failure to open the roadmap's graph store — is answered `500`, as on every other route, and does not carry this body shape. What separates it from the `400` of an execution failure is *when* the failure surfaces: a failure that surfaces once the query is already running, from the run itself or from the walk over its result, is a query execution failure.

In every case the message is shown in place on the page, the page does not crash, and the failure triggers no write and no navigation.

## Comment Surfaces

Comments recorded through `rmp task comment-add` and `rmp sprint comment-add` are surfaced on two read-only places in the interface. Both only display data: neither creates, edits, nor deletes a comment, and the CLI remains the sole write path.

### Task comments: the detail modal timeline

Anywhere a task is clickable — the cards of the roadmap tasks board and the sprint page's task list — the read-only task detail modal renders that task's comments as a chronological timeline, placed after the task's fields and last in the modal body.

- **Order and completeness.** Oldest first, exactly the order `rmp task comment-list` returns (`created_at` ascending, comment `id` ascending as the tie-breaker). Every comment of the task is rendered: no type filter and no count limit.
- **What each entry shows.** The comment's `type` as a badge, its `created_at` timestamp, an edited marker carrying the `updated_at` timestamp when that value is not null, and the `body`.
- **Type badge colour.** Neutral for every one of the seven task comment types. The semantic colour mapping used for status, priority, and severity is not extended to comment types.
- **Line breaks.** A comment body is multi-line as authored through the CLI, and the timeline preserves the author's line breaks; the text wraps inside the card, so no horizontal scrolling is introduced.
- **Empty state.** A task with no comments shows a plain message in place of the timeline, not an empty list and not a missing section.

### Sprint comments: the Comments card

The dedicated sprint page (`/roadmaps/{name}/sprints/{id}`) shows the sprint's own comments in a Comments card, placed after the member-tasks board and rendered last on the page.

- **Scope.** The card shows the comments of the sprint itself. It does not show, aggregate, or merge in the comments of the sprint's member tasks; those are reachable through each task's own detail modal.
- **Order and completeness.** Oldest first, exactly the order `rmp sprint comment-list` returns. Every comment of the sprint is rendered: no type filter and no count limit.
- **Card header.** The card title `Comments` with a badge showing the number of comments.
- **What each entry shows.** The same four elements as the task timeline: the `type` badge, `created_at`, the edited marker when `updated_at` is not null, and the `body`. The badge is neutral for every one of the four sprint comment types.
- **Empty state.** A sprint with no comments shows an empty-state message in place of the timeline. The card itself is always present.

The comments of every task rendered on a page are loaded in a single grouped query over the whole set of rendered task ids, never one query per task, so the number of comment queries per page does not grow with the number of tasks. The surfaces add no server endpoint: no route returns comments on their own, and comments are not embedded in any JSON the CLI emits for a task or a sprint.

## Exit Codes

These are the exit codes of the `rmp web` **process** (distinct from the per-request HTTP statuses above).

| Exit Code | Meaning |
|-----------|---------|
| 0 | Server started and was later stopped by `SIGINT` / `SIGTERM` (graceful shutdown) |
| 1 | Requested host/port could not be bound (explicit `--port` in use, or host not assignable), or the data directory could not be read |
| 2 | Unknown flag or unexpected positional argument |
| 6 | `--port` value out of range 0-65535 or not an integer |

## Examples

```bash
# Start on the default host (loopback, local machine only) and port (opens the browser)
rmp web

# Start without launching a browser; just print the served URL
rmp web --no-open

# Start on a specific port
rmp web --port 9000

# Expose the read-only interface on the network (all interfaces; prints a warning)
rmp web --host 0.0.0.0 --port 9000
```

## Read-Only and Security

- **Read-only.** The interface exposes no route that creates, edits, or deletes any roadmap, task, sprint, comment, audit entry, or graph element. Comments are displayed, never created, edited, or deleted from the web interface. Serving a page writes no rows and no audit-log entry. The graph store is opened read-only and a web read never triggers a checkpoint or write-ahead-log truncation.
- **Loopback by default.** The server binds the loopback interface (`127.0.0.1`) by default, so the read-only interface is reachable only from the local machine. Exposing it on the network via `--host 0.0.0.0` (all interfaces, or any other non-loopback address) is the explicit opt-in; doing so prints a network-exposure warning to stderr at startup.
- **Path-traversal guard.** Roadmap names from the URL are validated before any filesystem path is built, so a crafted name cannot traverse outside `~/.roadmaps/`.
- **Tabler dark-theme UI.** The interface is built on the vendored Tabler admin-dashboard framework in its dark theme (navigation sidebar that collapses to a hamburger menu on small viewports, top navbar, page headers, Tabler cards/tables/badges). The top navbar names the roadmap the current page belongs to, so the page's subject is stated at the top of the viewport even where the sidebar has collapsed behind the hamburger menu; the roadmap index page, which belongs to no roadmap, leaves that region empty. Each page header is rendered by one shared template and its title names the view rather than the roadmap - Sprints, Tasks, Audit, Knowledge graph - so the roadmap is stated once in the sidebar and once in the navbar, and never a third time. A sprint's own page is the exception: it shows that sprint's title with its status badge, under the pretitle `Sprint #<id>`. A page header carries an actions control only where one acts on the page (the tasks board's search box and its three filter dropdowns, the graph page's layout dropdown) or returns to the parent record (the sprint page's back link); it never repeats a link the sidebar already lists. Task and sprint status, priority, and severity render as colour-coded Tabler badges (for example completed work in green, in-progress in blue, high priority or critical severity in red), so state is scannable at a glance.
- **Self-contained.** Every asset (HTML, CSS, JavaScript, the vendored Tabler framework and D3.js with the d3-sankey plugin, the Tabler Icons webfont, and the Inter font) is served from the binary's embedded set under `/static/`; no page references a CDN, a remote font host, or any other remote origin, and the server makes no outbound request.

## See Also

- `SPEC/WEB.md` - full behaviour of the running server (routes, read-only data flow, self-contained delivery, mobile-first design, security)
- `SPEC/COMMANDS.md` (Web Interface) - the command-line contract
- `DOCS/commands/graph.md` - the knowledge graph the graph page visualises
- `DOCS/commands/task.md` and `DOCS/commands/sprint.md` - the comment subcommands that write the logs these pages display
