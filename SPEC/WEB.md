# Web Interface

## Table of Contents

- [Overview](#overview)
- [Functional Requirements](#functional-requirements)
- [Command Surface](#command-surface)
- [Server Lifecycle](#server-lifecycle)
- [Startup Schema Migration](#startup-schema-migration)
- [Bind Address and Port Selection](#bind-address-and-port-selection)
- [HTTP Server Timeouts](#http-server-timeouts)
  - [Graph Query Time Budget](#graph-query-time-budget)
- [Security Headers](#security-headers)
- [Cache Policy](#cache-policy)
- [Routes and Pages](#routes-and-pages)
  - [Roadmap Index Page](#roadmap-index-page)
  - [Roadmap Sprints Page](#roadmap-sprints-page)
  - [Roadmap Tasks Page](#roadmap-tasks-page)
  - [Roadmap Sprint Page](#roadmap-sprint-page)
  - [Roadmap Audit Log Page](#roadmap-audit-log-page)
  - [Shared Page-Header Partial](#shared-page-header-partial)
  - [Shared Sprint-Card Partial](#shared-sprint-card-partial)
  - [Sprint Detail Sub-Template](#sprint-detail-sub-template)
  - [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)
  - [Graph Query Bar](#graph-query-bar)
  - [Query-Bar Error Handling](#query-bar-error-handling)
  - [Graph Labels Sidebar](#graph-labels-sidebar)
  - [Graph Data Endpoint](#graph-data-endpoint)
  - [Static Assets](#static-assets)
  - [Task Detail Modal](#task-detail-modal)
  - [Task Detail Endpoint](#task-detail-endpoint)
- [Read-Only Data Flow](#read-only-data-flow)
  - [Tasks and Sprints from SQLite](#tasks-and-sprints-from-sqlite)
  - [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)
- [Frontend and Embedded Assets](#frontend-and-embedded-assets)
  - [Self-Contained Deliverable](#self-contained-deliverable)
  - [Embedded Asset Categories](#embedded-asset-categories)
  - [Frontend Rules](#frontend-rules)
  - [UI Framework](#ui-framework)
  - [Full-Height Page Regions](#full-height-page-regions)
  - [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours)
  - [Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library)
- [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)
- [Server Logging](#server-logging)
  - [Logger Configuration](#logger-configuration)
  - [Levels](#levels)
  - [What Is Logged](#what-is-logged)
  - [What Is Not Logged](#what-is-not-logged)
  - [Record Content](#record-content)
  - [Log Integrity](#log-integrity)
- [Error Handling and Exit Codes](#error-handling-and-exit-codes)
- [Security and Constraints](#security-and-constraints)
- [Acceptance Criteria](#acceptance-criteria)
- [See Also](#see-also)

## Overview

The web interface is a browser-based presentation of the data that the `rmp` CLI
manages. A user starts it with the `rmp web` command, which runs an HTTP server
embedded in the `rmp` binary, opens a local browser to it, and navigates the
roadmaps found under `~/.roadmaps/` from there.

Every page the interface renders is read-only, and no roadmap's `project.db` is
ever written through a request. **The knowledge-graph query bar is the
exception.** The Cypher statement it submits is executed as written, so a request
to the graph data endpoint can create, change, and delete graph data and can
change the graph's schema. The endpoint is not authenticated and the server offers
no authentication of any kind (see
[Security and Constraints](#security-and-constraints)).

The web interface presents roadmap data and never changes it. The `rmp` CLI is
the sole write path for roadmaps, tasks, sprints, and audit entries, and the
interface provides no create, edit, or delete action over any of them. It reads
the same on-disk data the CLI reads, in the same locations, and serves it as
server-rendered HTML. The knowledge graph is outside that statement, on the terms
above: a statement submitted through the query bar reaches the graph store the way
`rmp graph execute` reaches it.

The server is built only from Go's standard library (`net/http`) and assets
embedded into the binary at build time. It requires no external runtime
dependency, no JavaScript build toolchain, no `node_modules`, and no content
delivery network. The deliverable is fully self-contained: the single `rmp`
binary embeds every component required to render and operate the interface, and
the interface renders and functions fully offline with only that binary present
on disk (see
[Self-Contained Deliverable](#self-contained-deliverable)).

The interface is built on the Tabler admin-dashboard framework and presents a
Tabler admin-shell layout in Tabler's dark theme across every page: a
navigation sidebar, a top navbar, page headers, and Tabler cards, tables, and
badges. The sidebar lists the roadmaps and, within a roadmap, links to that
roadmap's Sprints, Tasks, Audit, and Graph views: the Sprints link points to the
roadmap's landing page at `/roadmaps/{name}`, the Tasks link points to
`/roadmaps/{name}/tasks`, the Audit link points to `/roadmaps/{name}/audit`, and
the Graph link points to `/roadmaps/{name}/graph`;
the sidebar highlights whichever of these views is active. Tabler and its assets
are vendored
and served locally, never from a content delivery network or any remote origin
(see [Frontend and Embedded Assets](#frontend-and-embedded-assets) and
[UI Framework](#ui-framework)).

The interface is designed responsive and mobile-first: its base styles target
small phone-sized viewports first and progressively enhance for larger viewports,
and it adapts fluidly across viewport sizes on every page, including the
interactive knowledge-graph visualisation. On small viewports the admin-shell
navigation sidebar collapses to an off-canvas (hamburger) menu so the pages stay
usable without horizontal overflow (see
[Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).

The web interface exposes the following kinds of page for each roadmap:

1. A roadmap index that lists every roadmap found under `~/.roadmaps/`.
2. A roadmap sprints page that is the roadmap's landing page, served at
   `/roadmaps/{name}` and read from its SQLite `project.db`. It presents the
   roadmap's sprints as three tabs (Próximos, Actual, Concluídos), with **Actual**
   active by default. Every sprint in every tab — including the OPEN ("current")
   sprint or sprints under Actual — is rendered through a single shared
   sprint-card partial, so all sprints share identical card markup across the
   three tabs. Each card shows a header ("Sprint #<ID>" with a status badge), the
   sprint description, and a footer with the sprint's task count, and links to that
   sprint's own page. The Actual tab does not expand the OPEN sprint into an
   inline member-tasks board or per-task modals; the full sprint detail block is
   shown only on the single Roadmap Sprint Page (see
   [Shared Sprint-Card Partial](#shared-sprint-card-partial)). It does not render
   the roadmap's task board.
3. A roadmap tasks page, served at `/roadmaps/{name}/tasks` and read from that
   roadmap's `project.db`. It presents every task of the roadmap (any status) as a
   Kanban board of five fixed columns, one per task status, with each task shown as
   a card in the column of its status and each card clickable to open the read-only
   task detail modal. The page renders no task table.
4. A roadmap sprint page that shows all details of a single sprint and the
   sprint's member tasks as a Kanban board of three fixed columns — `WAITING`,
   `DOING`, and `CLOSED` — whose cards follow the planned in-sprint execution
   order, read from that roadmap's `project.db`.
5. A roadmap audit log page, served at `/roadmaps/{name}/audit` and read from
   that roadmap's `project.db`. It presents the roadmap's full audit log — every
   audit entry of any operation and entity type — as a read-only table ordered by
   the audit entry's `performed_at` timestamp descending (most recently performed
   operation first), paginated at a fixed page size of 100 entries per page.
6. A roadmap knowledge-graph page that shows that roadmap's knowledge graph,
   read from its GoGraph store under `~/.roadmaps/<name>/graph/`, as an
   interactive node-link visualisation.

When a user selects a roadmap on the index page, the user lands on that
roadmap's sprints page (`/roadmaps/{name}`), with the **Actual** tab — the
current OPEN sprint or sprints — active by default.

Where a task is shown clickable on these pages, selecting it opens a read-only
task detail modal that displays all of the task's fields (see
[Task Detail Modal](#task-detail-modal)).

## Functional Requirements

1. `rmp web` starts an HTTP server embedded in the `rmp` binary, built on Go's
   standard-library `net/http`, and serves the read-only web interface until the
   server is stopped (see [Server Lifecycle](#server-lifecycle)).
2. The server binds to a host and a port chosen as specified in
   [Bind Address and Port Selection](#bind-address-and-port-selection). By default
   the server binds the loopback interface (`127.0.0.1`), so the read-only
   interface is reachable only from the local machine. The bind host and port are
   overridable by flag; exposing the interface on the network is the explicit
   opt-in `--host 0.0.0.0` (or any other non-loopback address). When a non-loopback
   host is bound, the server prints a warning to stderr that the interface is
   reachable from the network.
3. `rmp web` does **not** require the `-r` / `--roadmap` flag. The web interface
   discovers all roadmaps under `~/.roadmaps/` and lets the user drill into any
   one of them from the index page. This is the one user-facing command that
   operates across all roadmaps rather than a single selected roadmap (see
   [Command Surface](#command-surface)).
4. The web interface serves `GET` (and `HEAD`) requests only. Any other HTTP
   method on any route is answered with HTTP `405 Method Not Allowed`. It exposes
   no route that creates, edits, or deletes a roadmap, a task, a sprint, or an
   audit entry. **It does expose one route that changes graph data**: the graph
   data endpoint runs the caller's Cypher, and a `GET` of it may therefore create,
   change, or delete nodes, relationships, properties, and schema objects (see
   [Graph Data Endpoint](#graph-data-endpoint) and
   [Security and Constraints](#security-and-constraints)). A `GET` that changes
   state is a departure from the safe-method semantics of RFC 9110, Section 9.2.1,
   and it is stated here rather than left to be discovered from behaviour.
5. The roadmap index page lists every roadmap discovered under `~/.roadmaps/`,
   using the same roadmap-discovery rule the CLI uses (see
   [Roadmap Index Page](#roadmap-index-page)).
6. The roadmap sprints page is the roadmap's landing page. It shows the selected
   roadmap's sprints, with the fields and relationships already defined in
   `MODELS.md` and `DATABASE.md`, read from that roadmap's `project.db`, and is
   served at `/roadmaps/{name}`. The page presents the roadmap's sprints as three
   tabs, labelled **Próximos**, **Actual**, and **Concluídos** from left to right,
   with **Actual** active by default. The interface classifies each sprint into a
   tab by its status: a `PENDING` sprint appears under Próximos, an `OPEN` sprint
   under Actual, and a `CLOSED` sprint under Concluídos. Every sprint in every tab
   is rendered through the single shared sprint-card partial, so all sprints share
   identical card markup across the three tabs; each card shows a header
   ("Sprint #<ID>" with a status badge), the sprint description, and a footer with
   that sprint's total task count, and links to the sprint's own page. The OPEN
   sprint or sprints under Actual are rendered with this same card; the Actual tab
   does not expand the OPEN sprint into an inline member-tasks board or per-task
   modals. Próximos lists PENDING sprints ordered by ascending sprint `Order`
   (the unique execution order; the next sprint to execute, lowest `Order`,
   first); Actual lists the OPEN sprint or sprints ordered by ascending sprint
   `Order`; Concluídos lists CLOSED sprints ordered by descending sprint
   `Order` (the last/highest-`Order` closed sprint first). Each sprint shown in
   any tab is a clickable link to that sprint's own page. The sprints page does not render the roadmap's task board (see
   [Roadmap Sprints Page](#roadmap-sprints-page),
   [Roadmap Sprint Page](#roadmap-sprint-page), and
   [Shared Sprint-Card Partial](#shared-sprint-card-partial)).
7. The roadmap tasks page shows every task of the selected roadmap, of any
   status, as a **Kanban board**, read from that roadmap's `project.db` and using
   the fields and relationships already defined in `MODELS.md` and `DATABASE.md`.
   It is served at `/roadmaps/{name}/tasks`. The board has exactly five fixed
   columns, one per `TaskStatus` value, in the order `BACKLOG`, `SPRINT`, `DOING`,
   `TESTING`, `COMPLETED`; every column is always present, even when empty, and
   each column header carries a badge with that column's task count. Each task is
   shown as one card in the column of its `status`, so every task appears exactly
   once and no task is omitted. Each card is clickable: selecting a card opens the
   read-only task detail modal for that task, which is where the task's full field
   set is shown. The board is read-only: it offers no drag-and-drop and no other
   control that moves a task between columns. The page renders no task table. The
   page header carries a **search input** that narrows the board to the tasks whose
   title or `#<id>` reference contains the term, and **three filter dropdowns** —
   task type, minimum priority, and minimum severity — that narrow the board by what
   a task is. The search term and the three filters combine conjunctively, the
   column counts follow the narrowed set, and each control travels in its own URL
   query parameter (`q`, `type`, `priority`, and `severity`), so requesting the page
   with those parameters renders the identical narrowed board. The board offers
   **no** status filter, because the columns already are the status (see
   [Roadmap Tasks Page](#roadmap-tasks-page) and
   [Task Detail Modal](#task-detail-modal)).
8. When a user selects a roadmap on the index page, the user lands on that
   roadmap's sprints page (`/roadmaps/{name}`), with the **Actual** tab — the
   current OPEN sprint or sprints — active by default (see
   [Roadmap Index Page](#roadmap-index-page) and
   [Roadmap Sprints Page](#roadmap-sprints-page)).
9. The roadmap sprint page shows all details of a single sprint, the sprint's
   member tasks as a Kanban board of three fixed columns — `WAITING` holding the
   sprint's `BACKLOG` and `SPRINT` tasks, `DOING` its `DOING` and `TESTING` tasks,
   and `CLOSED` its `COMPLETED` tasks — whose cards are ordered by what each column
   is about, the `WAITING` column by the planned in-sprint execution order and the
   `DOING` and `CLOSED` columns by recency (`started_at` and `closed_at`
   descending), and whose column counts are the `P`, `A`, and `C` values of the
   sprint status summary line on the same page, and the sprint's own
   comments in a Comments card, read from that roadmap's
   `project.db`. It is served at `/roadmaps/{name}/sprints/{id}`, is read-only, and
   returns HTTP `404 Not Found` when `{id}` is not a valid integer or is not a
   sprint of the named roadmap (see [Roadmap Sprint Page](#roadmap-sprint-page)).
10. The roadmap audit log page shows the selected roadmap's full audit log — every
   audit entry of any operation and entity type — with all seven `AuditEntry` fields
   already defined in `MODELS.md` and `DATABASE.md`, read from that roadmap's
   `project.db`. It is served at `/roadmaps/{name}/audit`, is read-only, and
   presents the entries as a table ordered by the audit entry's `performed_at`
   timestamp descending (the most recently performed operation first). The page is
   paginated at a fixed page size of 100 entries per page, with the page selected
   by a 1-based `page` query parameter that defaults to 1 when absent and is
   clamped to the nearest valid page; an empty audit log renders successfully with
   a clear empty-state message (see
   [Roadmap Audit Log Page](#roadmap-audit-log-page)).
11. Anywhere a task is shown clickable — the board cards of the tasks page and
   the board cards of the sprint page — selecting the task opens a read-only task
   detail modal that displays all of the task's fields and, after them, that task's
   comments as a chronological timeline. The element that opens the modal is a
   `<button>` on every such surface, and on both boards that `<button>` is the card
   itself, so the pointer, touch, Enter, and Space all
   open it without any added JavaScript. The modal
   only displays data: it contains no form, no edit control, and no submit action,
   and it opens no write path. A page renders **one** modal element, not one per
   task, and fills it on demand: opening a task fetches that task's fields and
   comments from the read-only endpoint `GET /roadmaps/{name}/tasks/{id}/data`.
   Every value that endpoint returns is written into the page as text and never as
   markup (see [Task Detail Modal](#task-detail-modal) and
   [Task Detail Endpoint](#task-detail-endpoint)).
12. The roadmap knowledge-graph page shows the selected roadmap's knowledge graph
   as an interactive node-link visualisation rendered with **D3.js**, read from
   that roadmap's GoGraph store, opened exactly as `rmp graph execute` opens it,
   and under the same exclusive lock (see
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)).
   The page offers the complete set of
   "Networks"-section D3 gallery layouts — Force-directed graph,
   Disjoint force-directed graph, Mobile patent suits (the **default**), Arc diagram,
   Sankey diagram, Hierarchical edge bundling, Chord diagram, Directed chord diagram,
   and Chord dependency diagram — selectable through a dropdown, and layouts that need a
   constrained data shape degrade gracefully. The page also presents a labels
   sidebar column, inside the graph card to the left of the canvas, that lists
   every node label and every edge type in the graph with a count for each and
   lets the user highlight the matching elements without removing the rest. At the
   top of the page a query bar lets the user drive the graph from a single editable
   Cypher statement, with a Search button and a node-limit dropdown; the statement
   is executed as written, whatever it does (see
   [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page),
   [Graph Query Bar](#graph-query-bar),
   [Graph Data Endpoint](#graph-data-endpoint),
   [Graph Labels Sidebar](#graph-labels-sidebar),
   [Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library),
   and
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)).
13. A knowledge graph is reached through the web interface exactly as
    `rmp graph execute` reaches it: the store's exclusive lock is taken before the
    open, the same transactional engine is constructed, the statement is executed,
    and the synchronous checkpoint and write-ahead-log truncation follow when that
    statement's transaction wrote. A statement that wrote nothing leaves the
    store's data untouched and may still change the store directory's structure,
    because opening the store runs the engine's recovery, which repairs an
    interrupted checkpoint (see
    [Security and Constraints](#security-and-constraints),
    [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
    `GRAPH.md § Engine Constructor by Path`,
    `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`, and
    `GRAPH.md § Synchronous Checkpoint on Write`).
14. **The deliverable is fully self-contained.** The shipped `rmp` binary MUST
   embed every component required to render and operate the web interface, with
   zero external runtime dependency. Every asset category — HTML templates, the
   stylesheet, all client JavaScript (including the D3.js knowledge-graph
   visualisation library and the d3-sankey plugin and any of their dependencies),
   web fonts, icons and images, the favicon, and any other static asset — is
   embedded into the binary at build time with `go:embed` and served only from
   the embedded asset set under the `/static/...` route. The server never reads
   an asset from the host filesystem and never serves an arbitrary host
   filesystem path (see
   [Self-Contained Deliverable](#self-contained-deliverable),
   [Embedded Asset Categories](#embedded-asset-categories), and
   [Security and Constraints](#security-and-constraints)).
15. **No runtime network fetch.** No page references a script, stylesheet, font,
    image, or any other asset from a remote origin: no content delivery network,
    no Google Fonts or other remote font, script, or style host, and no external
    API. The interface renders and functions fully offline, with only the single
    `rmp` binary present on disk: no sidecar files and no separate assets
    directory shipped alongside it. The running server makes no outbound network
    request of its own (see
    [Self-Contained Deliverable](#self-contained-deliverable) and
    [Frontend and Embedded Assets](#frontend-and-embedded-assets)).
16. **Responsive and mobile-first.** The web interface MUST be designed
    responsive and mobile-first: base styles target small phone-sized viewports
    first and progressively enhance for larger tablet and desktop viewports
    through `min-width` media queries, and every page adapts fluidly across
    viewport sizes. This requirement applies to every page — the roadmap index,
    the roadmap sprints page, the roadmap tasks page, the roadmap sprint page, the
    roadmap audit log page, and the knowledge-graph page — and to the interactive
    components, including the
    sprint tabs, the tasks page's Kanban board, the sprint page's member-tasks
    board, the task detail modal, and the interactive knowledge-graph
    visualisation, which MUST all remain usable on touch and small-viewport devices
    (see [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
17. **Tabler admin-shell layout in the dark theme.** The interface presents a
    Tabler admin-shell layout in Tabler's dark theme across every page: a
    navigation sidebar (listing the roadmaps and, within a roadmap, that
    roadmap's Sprints, Tasks, Audit, and Graph views, resolving to
    `/roadmaps/{name}`, `/roadmaps/{name}/tasks`, `/roadmaps/{name}/audit`, and
    `/roadmaps/{name}/graph` respectively and
    highlighting the active view), a top navbar naming the selected roadmap, page
    headers, and Tabler cards, tables, and badges. The interface is built on the vendored Tabler framework;
    on small viewports the navigation sidebar collapses to an off-canvas
    (hamburger) menu (see [UI Framework](#ui-framework) and
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
18. Startup failures (for example, the chosen port is already in use, the data
    directory is unreadable, or a flag value is invalid) are reported as plain
    text to stderr and map to the existing exit codes; no new exit code is
    introduced (see [Error Handling and Exit Codes](#error-handling-and-exit-codes)).
19. **The graph data endpoint bounds its own work.** The endpoint executes the
    caller-supplied Cypher query under a per-request time budget of 5 seconds,
    derived from the request context, so no single request can hold the server for
    as long as that query takes to run. The budget bounds the work the query
    causes, whereas the injected node `LIMIT` bounds only the result it returns; a
    query cancelled for exceeding the budget is surfaced as a query execution
    failure, with the message the page already shows for one, and introduces no new
    HTTP status and no new exit code (see
    [Graph Query Time Budget](#graph-query-time-budget),
    [Graph Data Endpoint](#graph-data-endpoint), and
    [Query-Bar Error Handling](#query-bar-error-handling)).

## Command Surface

`rmp web` is a single command with no subcommands. Its full CLI contract — flags,
defaults, output, and exit codes — is specified in `COMMANDS.md § Web Interface`.
This file specifies the behaviour of the running server; `COMMANDS.md` is the
canonical home for the command-line contract, and `HELP.md` is the canonical home
for the command's help skeleton.

Key contract points, repeated here only to make this file self-contained
(`COMMANDS.md § Web Interface` is canonical):

- `rmp web` has no alias.
- `rmp web` does not accept the `-r` / `--roadmap` flag. The interface lists all
  roadmaps and the user selects one in the browser. The cross-cutting
  always-required-roadmap rule in
  `COMMANDS.md § Roadmap Selection (Always Required)` lists the families it
  applies to (`task`, `sprint`, `backlog`, `audit`, `stats`, `graph`); `web` is
  deliberately not in that list.
- Flags: `--host <address>` (default `127.0.0.1`, loopback only; binding a
  non-loopback host such as `0.0.0.0` exposes the interface on the network and
  prints a warning to stderr),
  `--port <number>` (default `8787`, with the fallback behaviour in
  [Bind Address and Port Selection](#bind-address-and-port-selection)),
  `--no-open` (do not launch a browser), and `-h, --help`.

## Server Lifecycle

For an `rmp web` invocation the implementation:

1. Resolves and verifies the data directory `~/.roadmaps/` (creating it with
   `0700` if absent, consistent with the CLI). The filesystem layout migration
   sweep runs at startup before this, as on every `rmp` invocation (see
   `ARCHITECTURE.md § Filesystem Layout Migration`).
2. Migrates the SQLite schema of every existing roadmap to the current schema
   version, automatically and without user input, before binding the listener or
   serving any request (see
   [Startup Schema Migration](#startup-schema-migration)).
3. Resolves the bind host and port (see
   [Bind Address and Port Selection](#bind-address-and-port-selection)) and binds
   a TCP listener. A bind failure (for example, the port is already in use or the
   host is not assignable) is a fatal startup error (see
   [Error Handling and Exit Codes](#error-handling-and-exit-codes)). When the
   resolved host is not a loopback address, the server prints a network-exposure
   warning to stderr (see
   [Bind Address and Port Selection](#bind-address-and-port-selection)).
4. Registers the read-only routes (see [Routes and Pages](#routes-and-pages)),
   configures the HTTP server timeouts (see
   [HTTP Server Timeouts](#http-server-timeouts)), and starts serving.
5. Prints to stdout the URL the server is listening on, so the user can open it
   manually if no browser is launched. The startup line is the single
   machine-readable success object described in `COMMANDS.md § Web Interface`.
6. Unless `--no-open` is given, attempts to open the user's default browser at
   the served URL. A failure to launch a browser is **not** fatal: the server
   keeps running and the URL has already been printed.
7. Serves requests until the process receives an interrupt signal (`SIGINT`, for
   example `Ctrl+C`) or a termination signal (`SIGTERM`). On either signal the
   server shuts down gracefully: it stops accepting new connections, allows
   in-flight requests a brief bounded period to complete, closes any graph store
   or database handle it opened, and exits 0.

The server is long-lived for the duration of the session. This is the only `rmp`
command whose process is expected to keep running rather than complete a single
operation and exit. Each incoming request opens the data it needs read-only,
serves the response, and releases the handle; the server does not hold a roadmap
database or a graph store open across requests.

## Startup Schema Migration

At startup, before it binds the listener and before it serves any request,
`rmp web` ensures that every existing roadmap's SQLite schema is migrated to the
current schema version. This guarantees that the per-request read-only handlers
never query a stale-schema database. Because the per-request data loaders open
each database strictly read-only (see
[Tasks and Sprints from SQLite](#tasks-and-sprints-from-sqlite)), they never run
a schema migration themselves; the startup step is therefore where the web
interface satisfies the project-wide rule that any invocation needing the current
schema migrates to it automatically, without user input.

1. **One-time startup step.** The schema migration runs once, during startup, as
   part of the server-lifecycle step that precedes binding the listener (see
   [Server Lifecycle](#server-lifecycle)). It does not run per request.
2. **Migrates every existing roadmap.** The server discovers every roadmap under
   `~/.roadmaps/`, using the same discovery rule the index page uses (each
   immediate subdirectory of `~/.roadmaps/` that contains a `project.db`; see
   [Roadmap Index Page](#roadmap-index-page) and
   `ARCHITECTURE.md § Directory Structure`, location rule 9). For each discovered
   roadmap, the server opens that roadmap's `project.db` through the **normal
   writable open path**, which runs the schema migrations defined in
   `VERSION.md § Migrations`, and then closes the database immediately. The open
   is performed solely to run the migrations; the server holds no database open
   after this step (see [Server Lifecycle](#server-lifecycle)).
3. **Idempotent.** Opening a database through the writable path runs the schema
   migrations, which are idempotent: a database already at the current schema
   version is left unchanged, so the startup migration is a no-op for any roadmap
   that is already current and only rewrites a database whose schema is behind the
   current version (see `VERSION.md § Migrations` and
   `DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)`).
4. **Automatic, no user input.** The startup migration happens automatically. It
   requires no flag, no confirmation, and no other user input. A user who starts
   `rmp web` after upgrading the binary therefore reaches a fully usable interface
   without being asked to migrate anything first.
5. **Ordered before any read-only connection.** The startup migration is the only
   path on which the web interface writes to a roadmap database, and it completes
   before the server binds the listener and before any per-request read-only
   connection is opened. There is therefore no contention between the startup
   migration and the live read-only handlers: by the time a request is served,
   every database is already at the current schema version and is opened only
   read-only (see [Read-Only Data Flow](#read-only-data-flow)).
6. **Per-roadmap failure is best-effort and non-fatal.** If a roadmap's database
   cannot be migrated (for example, it is unreadable, locked by another writer, or
   corrupt), the server logs a `WARN` record to stderr naming that roadmap and
   the reason (see [Server Logging](#server-logging)), and continues with the
   remaining roadmaps. A migration
   failure for one roadmap does **not** prevent the server from starting and does
   **not** prevent the other roadmaps from being served. This mirrors the
   best-effort, non-fatal tone of the legacy-layout migration sweep's
   conflict-skip warning and of the network-exposure warning (see
   `ARCHITECTURE.md § Filesystem Layout Migration` and
   [Bind Address and Port Selection](#bind-address-and-port-selection)). A roadmap
   that could not be migrated remains at its on-disk schema version; a later
   request that needs a column the stale schema lacks surfaces as an internal read
   error (HTTP 500) on the affected route, exactly as any other read failure does
   (see [Routes and Pages](#routes-and-pages)).
7. **Knowledge-graph store unaffected.** This startup step migrates only the
   SQLite schema. The roadmap's GoGraph knowledge-graph store under
   `~/.roadmaps/<name>/graph/` is a separate persistence layer with its own
   on-open recovery and is not touched by the SQLite schema migration; it
   continues to be opened on demand by graph requests, through the engine's read
   path (see
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)).

## Bind Address and Port Selection

1. **Default host.** The server binds the loopback interface (`127.0.0.1`) by
   default. With the default host the read-only interface is reachable only from
   the local machine, not from any other network point.
2. **Host override.** `--host <address>` overrides the bind host. A user who wants
   to expose the interface on the network passes the explicit opt-in
   `--host 0.0.0.0` (all interfaces), or any other non-loopback address. Exposing
   the interface beyond loopback is an explicit user choice, and the security note
   in [Security and Constraints](#security-and-constraints) applies.
3. **Network-exposure warning.** When the resolved bind host is not a loopback
   address (it is neither `127.0.0.1`, nor `::1`, nor any other address in the
   loopback range), the server writes a `WARN` record to stderr stating that the
   read-only interface is reachable from the network and naming the bound host
   (see [Server Logging](#server-logging)). The warning is informational only: it
   does not change the exit code and does not prevent the server from starting.
   Binding a loopback address writes no such record.
4. **Default port.** The default port is `8787`. When `--port` is not given, the
   server attempts to bind `8787`. If `8787` is already in use, the server falls
   back to an ephemeral port chosen by the operating system (binding port `0`),
   so that `rmp web` starts successfully even when the default port is taken. The
   actual chosen port is reported in the startup line and the served URL.
5. **Explicit port.** `--port <number>` requests a specific port. When an
   explicit port is given, the server does **not** fall back to an ephemeral
   port: if the requested port cannot be bound, the command fails with a bind
   error (see [Error Handling and Exit Codes](#error-handling-and-exit-codes)),
   because the user asked for that exact port. `--port 0` explicitly requests an
   operating-system-chosen ephemeral port and always succeeds when a port is
   available.
6. **Port range.** A `--port` value MUST be an integer in the range `0`-`65535`.
   A value outside that range, or a non-integer value, is an invalid flag value
   (`utils.ErrValidation`, exit code 6).

## HTTP Server Timeouts

The embedded HTTP server MUST be configured with explicit timeouts so that a slow
or stalled client connection cannot hold server resources indefinitely. The
`net/http` server is configured with all of the following:

1. **ReadHeaderTimeout: 10 seconds.** The maximum time allowed to read a request's
   headers. This bounds slow-header (Slowloris-style) connections.
2. **WriteTimeout: 30 seconds.** The maximum time allowed for writing a response,
   measured from the end of the request header read. This bounds a slow-reading
   client that stalls the response.
3. **IdleTimeout: 120 seconds.** The maximum time a keep-alive connection is kept
   open while idle between requests. This bounds idle keep-alive connections.

These three timeouts are mandatory. They protect the read-only server from
resource exhaustion by slow or idle connections and apply uniformly to every
route. They bound the connection only. The work a handler performs once the
request has been read is bounded separately, on the one route whose work a caller
drives, by the budget specified next.

### Graph Query Time Budget

The three timeouts above bound the connection, not the work the server does for a
request. A client that sends its headers promptly, stays connected, and reads the
response as soon as it arrives satisfies all three however long the server takes
to produce that response. One route's work is driven by caller-supplied input:
the graph data endpoint (`GET /roadmaps/{name}/graph/data`) executes a Cypher
query the caller writes (see [Graph Data Endpoint](#graph-data-endpoint) and
[Graph Query Bar](#graph-query-bar)). That route MUST therefore bound its own
work with an explicit time budget.

1. **Budget: 5 seconds, and it governs both surfaces.** The graph data endpoint
   MUST execute the caller's query under a deadline of 5 seconds. The deadline
   starts when the endpoint begins executing the query and covers the endpoint's
   execution of it: the run against the engine's read path and the walk over the
   result that run produces (see [Graph Data Endpoint](#graph-data-endpoint)).
   **This value is not the endpoint's own bound.** `rmp graph execute` executes
   its statement under the same budget and the same value, so every caller of the
   graph store that is not a long-lived server is bounded by it; this section is
   canonical for the value on both surfaces, and
   `GRAPH.md § Statement Time Budget` is canonical for what the budget does to a
   CLI invocation and for what a cut statement leaves on disk. The two surfaces
   read one declaration, so the value cannot drift between them, and changing it
   here changes it for the CLI too.

   **The value is justified against real graphs, because it has to carry the CLI
   as well.** On a small store a three-way Cartesian product spent 1.32 seconds of
   server time over 252 nodes to return a single aggregate row. That store is not
   the scale at which the budget has to be generous, so the value is justified
   against the largest real knowledge graph on the development machine as well,
   44,906 nodes in 36 MB: the statement part of every realistic query and
   every realistic write measured between **0 and 554 ms** there, and between
   **6 and 870 ms** on a synthetic graph of 400,000 nodes in 122 MB. Five seconds
   therefore clears the slowest realistic statement measured by roughly **5.7x**,
   and graph growth puts the budget under no pressure — what growth does put under
   pressure is the lock's fixed-part allowance, which
   `GRAPH.md § Lock Contention` measures and bounds.

   **What the budget cuts is measured too, and it is two query shapes.** On the
   same 36 MB graph, whose store open alone costs 962 ms, an unbounded whole-graph
   traversal (`MATCH (a)-[*1..3]->(b) RETURN count(*)`) costs **10.08 s** end to
   end, and a Cartesian product over 325 million tuples costs **14.05 s**; a
   three-way Cartesian product over 9.4 billion tuples had not finished after 300
   seconds, and no finite budget admits it. Nothing else measured comes near five
   seconds. A statement of either shape is narrowed rather than waited on: the
   same traversal restricted to a label and a relationship type costs 1.52 s end
   to end, a **554 ms** statement.

   The value also sits well below the 30-second `WriteTimeout`, so a query that
   exhausts the budget is cancelled, and its failure is rendered, while the
   response can still be written. It is additionally the quantity the graph store
   lock's bounded wait is sized against, because a waiter has to know how long a
   hold may lawfully last and the hold spans the statement (see
   `GRAPH.md § Lock Contention`). What must fit inside the `WriteTimeout` is the
   wait and the statement together rather than either of them alone;
   `GRAPH.md § Lock Contention` is canonical for that invariant and for the wait
   budget this value yields. Changing this value changes that wait.

   The rules below are this endpoint's own handling of the budget. What the same
   budget does to an `rmp graph execute` invocation is specified in
   `GRAPH.md § Statement Time Budget`.
2. **Derived from the request context.** The deadline MUST be derived from the
   request's own context, so the two sources of cancellation compose rather than
   replace one another: a client that disconnects still cancels the query
   immediately, exactly as it did before the budget existed, and a client that
   stays connected can no longer hold the query running beyond the budget. A CLI
   invocation has no such second source, so on that surface the deadline is the
   only thing that cancels a statement.
3. **The budget bounds the work; the node limit bounds the result.** These are two
   different bounds, and neither substitutes for the other. The `LIMIT` clause the
   endpoint injects (see [Graph Data Endpoint](#graph-data-endpoint)) bounds the
   **result**: how many rows the query returns, and therefore how large the
   response is. It does not bound the **work** the engine performs to produce
   those rows. A query that aggregates over a Cartesian product, for example,
   scans the whole product before any limit applies: its cost grows with the size
   of the store while its response stays a few bytes long. The time budget is the
   only bound on that work.
4. **Exceeding the budget is an execution failure.** When the budget is
   exhausted, the endpoint cancels the statement and reports the request as an
   **execution failure** — case 2 of
   [Query-Bar Error Handling](#query-bar-error-handling), the same classification
   a statement that fails in the engine receives, and distinct from the invalid
   limit of case 1. The page
   surfaces the existing "query failed to execute" message in place: the page does
   not crash, the failure triggers no write and no navigation, the graph already
   shown is left as it is, and the user can edit the query, lower the node limit,
   and search again.
5. **No new status and no new error class.** The budget introduces no new HTTP
   status, no new sentinel error, and no new process exit code. A request whose
   query exceeded the budget is answered exactly as any other query execution
   failure is answered — HTTP `400 Bad Request` with `kind` `execution`, the
   status and the kind the execution-failure class already carries (see
   [Query-Bar Error Handling](#query-bar-error-handling), rules 3 and 4) — so the
   budget adds no row to the HTTP status mapping in
   [Routes and Pages](#routes-and-pages) and leaves the exit-code mapping in
   [Error Handling and Exit Codes](#error-handling-and-exit-codes) unchanged.
   Exhausting the budget never terminates the process: the server keeps serving.
6. **Ordinary queries are unaffected.** A query that completes within the budget
   is served exactly as it was served before the budget existed: the same nodes
   and edges, in the same response shape, with nothing truncated, no ordering
   changed, and no latency added. The budget is observable only to a query that
   would otherwise have run for longer than it.
7. **Per request, and a cancelled statement commits nothing it had not already
   committed.** Each graph data request gets its own budget; requests do not share
   one, and one request's budget is unaffected by any other request in flight. A
   statement cancelled before its transaction committed leaves the graph unchanged
   and runs no checkpoint. The budget governs statement execution only; the store
   is already open by the time it starts, so cancellation neither causes nor undoes
   the recovery repair that opening performed (see
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)
   and `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`).
8. **The budget is the whole of the bound this interface adds.** This version
   bounds the work of a graph data request and adds nothing else to the web
   interface: no request rate limit and no new endpoint. That the same budget also
   binds `rmp graph execute` is a property of the value, not a second bound on
   this endpoint (see rule 1 and `GRAPH.md § Statement Time Budget`).

## Security Headers

Every HTML response the server returns MUST carry the following HTTP response
headers. These headers harden the read-only interface against content injection,
clickjacking, and content-type sniffing:

| Header | Value |
|--------|-------|
| `Content-Security-Policy` | `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'` |
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Referrer-Policy` | `same-origin` |

Notes:

1. The Content-Security-Policy restricts every resource type to the server's own
   origin (`'self'`), which is consistent with the self-contained, no-remote-origin
   asset model (see [Self-Contained Deliverable](#self-contained-deliverable)). It
   allows inline styles (`style-src 'self' 'unsafe-inline'`) and `data:` image
   sources (`img-src 'self' data:`) because the vendored Tabler framework and the
   D3.js visualisation use them; it forbids inline and remote scripts
   (`script-src 'self'`), forbids the page from being framed (`frame-ancestors
   'none'`), and restricts `<base>` to the same origin (`base-uri 'self'`).
2. `X-Frame-Options: DENY` reinforces `frame-ancestors 'none'` for clients that do
   not honour the Content-Security-Policy frame directive.
3. The headers apply to every HTML response. The graph data endpoint, which returns
   JSON, is additionally subject to the HTML-safe JSON encoding required in
   [Graph Data Endpoint](#graph-data-endpoint).
4. Data-derived responses additionally carry the `Cache-Control: no-store` header
   required in [Cache Policy](#cache-policy), so the freshly read database or graph
   state is never masked by a client-side or intermediary cache.

## Cache Policy

The web interface MUST never re-present stale data. Every response whose body is
computed from current data MUST reflect the exact current state of the roadmap
database or the knowledge-graph store on every request. The server already reads
the SQLite database and the GoGraph store fresh on every request and holds no
server-side data cache (see [Read-Only Data Flow](#read-only-data-flow)). This
section closes the remaining gap: it prevents the browser or any intermediary
HTTP cache from re-presenting a previously fetched dynamic response and thereby
showing a state that no longer matches the data.

1. **`Cache-Control: no-store` on every data-derived response.** Every
   data-derived response — that is, every response whose body is computed from the
   roadmap database or the knowledge-graph store — MUST carry the HTTP response
   header `Cache-Control: no-store`. This covers:
   - the roadmap index page (`/`);
   - the roadmap sprints page (`/roadmaps/{name}`);
   - the roadmap tasks page (`/roadmaps/{name}/tasks`);
   - the task detail endpoint (`/roadmaps/{name}/tasks/{id}/data`);
   - the roadmap sprint page (`/roadmaps/{name}/sprints/{id}`);
   - the roadmap audit log page (`/roadmaps/{name}/audit`);
   - the knowledge-graph page shell (`/roadmaps/{name}/graph`);
   - the graph data endpoint (`/roadmaps/{name}/graph/data`).

   It also covers the data-state-dependent error responses — for example a
   `404 Not Found` for a roadmap or a sprint that does not exist, and a `500` from
   a read failure — because whether such a path is found depends on the current
   database or store state, so those responses are themselves data-derived. The
   `400 Bad Request` responses of the graph data endpoint (see
   [Query-Bar Error Handling](#query-bar-error-handling)) carry the header as well.
   The rule is applied per route rather than per outcome, so every response of a
   route in the list above carries `no-store` whatever its status.
2. **`no-store`, not merely `no-cache`.** `Cache-Control: no-store` is the chosen
   directive. The response MUST NOT be stored by any cache, so a reload, a
   back/forward navigation, or a re-fetch always re-reads the current database or
   store state rather than re-presenting a stored copy. This is the mechanism that
   guarantees the read-only data-flow promise — that each request opens the data,
   reads the current state, and serves it (see
   [Read-Only Data Flow](#read-only-data-flow)) — is observable to the user and is
   not masked by a client-side cache.
3. **Static assets are excluded and remain cacheable.** Embedded static assets
   under `/static/...` (the vendored Tabler CSS and JavaScript, the D3.js bundle
   and the d3-sankey plugin, the fonts, the icons and images, the favicon, and the
   local scripts and stylesheet) are not data: they are immutable assets embedded
   in the binary (see [Embedded Asset Categories](#embedded-asset-categories)).
   They are explicitly EXCLUDED from the `no-store` rule and remain cacheable by
   the client. The `no-store` requirement targets data-derived responses only.
4. **Observable counterpart of the read-only data flow.** This policy is
   consistent with, and the observable counterpart of, the existing read-only
   data-flow guarantee: each request opens the data, reads the current state, and
   releases the handle (see [Read-Only Data Flow](#read-only-data-flow)). The
   `no-store` header ensures the freshly read state is what the user actually sees,
   rather than a previously cached response.

## Routes and Pages

All routes serve `GET` and `HEAD` only. Every page is server-rendered HTML
produced from embedded `html/template` templates. Page routes return HTML
(`text/html; charset=utf-8`); the graph data endpoint returns JSON.

| Route | Method | Purpose | Response |
|-------|--------|---------|----------|
| `/` | GET, HEAD | Roadmap index | HTML list of roadmaps |
| `/roadmaps/{name}` | GET, HEAD | Roadmap sprints page (landing; sprint tabs) | HTML |
| `/roadmaps/{name}/tasks` | GET, HEAD | Roadmap tasks page (Kanban task board; optional `q` search parameter and optional `type`, `priority`, and `severity` filter parameters, see [Roadmap Tasks Page](#roadmap-tasks-page)) | HTML |
| `/roadmaps/{name}/tasks/{id}/data` | GET, HEAD | One task's fields and comments, for the task detail modal (see [Task Detail Endpoint](#task-detail-endpoint)) | JSON |
| `/roadmaps/{name}/sprints/{id}` | GET, HEAD | Roadmap sprint page (all sprint details and the sprint's member-tasks board) | HTML |
| `/roadmaps/{name}/audit` | GET, HEAD | Roadmap audit log page (full audit log, paginated; optional `page` parameter; see [Roadmap Audit Log Page](#roadmap-audit-log-page)) | HTML |
| `/roadmaps/{name}/graph` | GET, HEAD | Roadmap knowledge-graph page (interactive visualisation) | HTML |
| `/roadmaps/{name}/graph/data` | GET, HEAD | Graph nodes and edges for the visualisation (optional `q` Cypher query and `limit` node-limit parameters; see [Graph Data Endpoint](#graph-data-endpoint)) | JSON |
| `/static/...` | GET, HEAD | Embedded static assets (CSS, JS, vendored D3.js graph library) | static file |

Path-parameter rules:

1. `{name}` is a roadmap name. It MUST be validated against the same roadmap-name
   rules the CLI enforces (regex `^[a-z0-9_-]+$`, maximum 50 characters; see
   `COMMANDS.md § Create Roadmap`) before it is used to resolve any filesystem
   path. A `{name}` that fails validation is rejected with HTTP `404 Not Found`
   and is never used to build a filesystem path. This validation is the web
   interface's path-traversal guard for roadmap names (see
   [Security and Constraints](#security-and-constraints)).
2. A syntactically valid `{name}` that does not correspond to an existing roadmap
   under `~/.roadmaps/` is answered with HTTP `404 Not Found`.
3. `{id}`, on the sprint route `/roadmaps/{name}/sprints/{id}`, is a sprint
   identifier. It MUST be a valid integer. A non-integer `{id}`, or an integer
   `{id}` that is not the `id` of a sprint belonging to the named roadmap, is
   answered with HTTP `404 Not Found`. The `{name}` part of the sprint route is
   validated by rules 1 and 2 above, exactly as on the other roadmap routes.
4. `{id}`, on the task detail endpoint `/roadmaps/{name}/tasks/{id}/data`, is a
   task identifier and follows the same discipline. It MUST be a valid integer; a
   non-integer `{id}`, or an integer `{id}` that is not the `id` of a task
   belonging to the named roadmap, is answered with HTTP `404 Not Found`. The
   `{name}` part is validated by rules 1 and 2 above before any filesystem path is
   built, exactly as on every other roadmap route, so the endpoint carries the same
   path-traversal guard as the pages (see
   [Task Detail Endpoint](#task-detail-endpoint) and
   [Security and Constraints](#security-and-constraints)).

HTTP status mapping for page and data routes:

| Condition | HTTP status |
|-----------|-------------|
| Page or data served successfully | 200 |
| Roadmap name invalid, or roadmap not found | 404 |
| Sprint `{id}` not a valid integer, or not a sprint of the roadmap | 404 |
| Task `{id}` not a valid integer, or not a task of the roadmap | 404 |
| Audit `page` parameter out of range, non-integer, or garbage | 200 (clamped to nearest valid page; see [Roadmap Audit Log Page](#roadmap-audit-log-page)) |
| Tasks `q` search parameter absent, empty, unmatched, or undecodable | 200 (never an error; see [Roadmap Tasks Page](#roadmap-tasks-page)) |
| Tasks `type`, `priority`, or `severity` filter parameter absent, unknown, malformed, or undecodable | 200 (never an error; the dimension applies no filter; see [Roadmap Tasks Page](#roadmap-tasks-page)) |
| Graph data `limit` not one of the six allowed values | 400 (`kind` `invalid_limit`; the query is not executed; see [Query-Bar Error Handling](#query-bar-error-handling)) |
| Graph data query fails once running, a query cancelled for exhausting the time budget included | 400 (`kind` `execution`; see [Query-Bar Error Handling](#query-bar-error-handling)) |
| Non-read HTTP method on any route | 405 |
| Unhandled internal error reading data (I/O, corrupt store), a graph store that fails to open included | 500 |

The HTTP status codes above describe the running server's HTTP responses and are
distinct from the process exit codes in
[Error Handling and Exit Codes](#error-handling-and-exit-codes), which describe
how the `rmp web` process itself terminates.

### Roadmap Index Page

- **Route:** `GET /`
- **Content:** A list of every roadmap discovered under `~/.roadmaps/`, using the
  same discovery rule the CLI uses for `rmp roadmap list`: each immediate
  subdirectory of `~/.roadmaps/` that contains a `project.db` is one roadmap (see
  `COMMANDS.md § List Roadmaps` and `ARCHITECTURE.md § Directory Structure`,
  location rule 9). For each roadmap the page links to its sprints page (the
  landing page, `/roadmaps/{name}`) and its knowledge-graph page
  (`/roadmaps/{name}/graph`). Selecting a roadmap lands the user on that
  roadmap's sprints page.
- **Empty state.** When no roadmaps exist under `~/.roadmaps/`, the index page
  renders successfully (HTTP 200) and shows a clear empty-state message telling
  the user that no roadmaps were found and that roadmaps are created with the CLI
  (`rmp roadmap create <name>`). The absence of roadmaps is not an error for the
  web interface; the server still starts and serves the empty index.

### Roadmap Sprints Page

- **Route:** `GET /roadmaps/{name}`
- **Landing page.** This is the roadmap's landing page: selecting a roadmap on the
  index page lands the user here (see [Roadmap Index Page](#roadmap-index-page)).
- **Content:** A read-only presentation of the named roadmap's sprints, read from
  that roadmap's `project.db`. This page does **not** render the roadmap's tasks;
  the roadmap's full set of tasks has its own page, the Kanban task board at
  `/roadmaps/{name}/tasks` (see [Roadmap Tasks Page](#roadmap-tasks-page)).
- **Sprints.** The page presents the roadmap's sprints as three tabs. From left
  to right the tab labels are exactly **Próximos**, **Actual**, and
  **Concluídos**, and the **Actual** tab is the active tab by default when the
  page loads. "Current sprint selected by default" means the Actual tab — the OPEN
  sprint or sprints — is the active tab on landing. The interface classifies each
  sprint into exactly one tab by its `Sprint` status (`MODELS.md § Sprint`, status
  enum in `MODELS.md § Enums`): a `PENDING` sprint goes to Próximos, an `OPEN`
  sprint to Actual, and a `CLOSED` sprint to Concluídos. Every sprint in every tab
  is rendered through the single shared sprint-card partial (see
  [Shared Sprint-Card Partial](#shared-sprint-card-partial)), so all sprints share
  identical card markup across the three tabs. The three tabs differ only in which
  sprints they contain and in the order those sprints appear. The tab control itself
  follows Tabler's "card with tabs" example: the tab list is a single
  `<ul class="nav nav-tabs card-header-tabs" data-bs-toggle="tabs" role="tablist">`
  inside the card header, and tab activation uses Bootstrap's native tabs behaviour
  (see [UI Framework](#ui-framework), rule 9). The status badge each card shows uses
  the semantic colour mapping in
  [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours).

  **Each tab carries its own count badge.** Beside its label, each of the three tabs
  shows a Tabler badge whose **text** is the number of sprints in that tab and whose
  **colour** is the variant the sprint status mapping assigns to the status that tab
  groups: Próximos carries `bg-secondary-lt` (the `PENDING` variant), Actual carries
  `bg-blue-lt` (`OPEN`), and Concluídos carries `bg-green-lt` (`CLOSED`) (see
  [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
  rule 2). The colour states which status the tab groups, and the text states how many
  sprints the tab holds; the two are independent, so the badge of a tab that holds no
  sprint shows the count `0` and keeps the colour of its status. The Próximos colour
  is the same `bg-secondary-lt` a badge carries when nothing colours it, so that tab
  on its own cannot show whether the mapping was applied; the three tabs are read
  together, with `bg-blue-lt` on Actual and `bg-green-lt` on Concluídos beside it.

  - **Actual** (the default active tab) presents the OPEN sprint or sprints —
    those in progress — ordered by ascending sprint `Order` (the unique sprint
    execution order; see `MODELS.md § Sprint`). Each OPEN sprint is shown with the
    shared sprint-card partial, the same card the other two tabs use. The Actual
    tab does not expand the OPEN sprint into an inline member-tasks board or
    per-task modals; the full sprint detail block is shown only on the single
    Roadmap Sprint Page (see [Roadmap Sprint Page](#roadmap-sprint-page)). When no
    sprint is OPEN, the Actual tab shows a clear empty-state message and no card.
  - **Próximos** lists the PENDING sprints — planned but not yet started — ordered
    by ascending sprint `Order` (the unique sprint execution order; see
    `MODELS.md § Sprint`). The sprint with the lowest `Order`, the next sprint to
    execute, appears first. When no sprint is PENDING, the Próximos tab shows a
    clear empty-state message.
  - **Concluídos** lists the CLOSED sprints ordered by descending sprint `Order`
    (the unique sprint execution order; see `MODELS.md § Sprint`). The CLOSED
    sprint with the highest `Order`, the last in execution order, appears first.
    When no sprint is CLOSED, the Concluídos tab shows a clear empty-state message.
  - Every sprint shown in any of the three tabs is a clickable link to that
    sprint's own page at `/roadmaps/{name}/sprints/{id}` (see
    [Roadmap Sprint Page](#roadmap-sprint-page)). The sprints page itself shows no
    member tasks and opens no task detail modal; tasks are clickable on the single
    Roadmap Sprint Page and on the tasks page's board (see
    [Task Detail Modal](#task-detail-modal)).

  **Why Concluídos reverses the sequence.** `rmp sprint list` returns a roadmap's
  sprints in a single sequence, `order` ascending — the planned execution order
  (see `COMMANDS.md § List Sprints`). This page keeps that sequence on Próximos
  and Actual and deliberately reverses it on Concluídos, because the two kinds of
  tab answer different questions: Próximos and Actual look forward at work still
  to come, where the next sprint to execute is the one to read first, while
  Concluídos looks back at work already finished, where the sprint executed most
  recently is the one to read first. The reversal is a presentation choice of this
  one tab. It changes no stored data and no other surface: the CLI listing order
  is unaffected, and a reader who wants the whole roadmap in a single planned
  sequence reads `rmp sprint list`.
- **Sprint description line breaks.** Wherever a sprint's `description` text is
  shown in a sprint card on this page — across all three tabs — the description
  renders preserving the author's line breaks (newlines), because the description
  is multi-line as authored through the CLI; the text still wraps, so no forced
  horizontal scrolling is introduced (see [Frontend Rules](#frontend-rules),
  rule 6).
- **Relationships shown.** The page surfaces, in a read-only view, the
  relationships already modelled in the data: task-to-sprint membership (including
  task order within a sprint). The presentation MUST reflect the same
  relationships defined in `DATABASE.md § Relationships`; it introduces no new
  relationship.
- **Read-only.** The page renders data only. It contains no form, button, or
  link that submits a change; there is no edit affordance of any kind. The sprint
  links and the task detail modal navigate to or display read-only views and
  submit no change.

### Roadmap Tasks Page

- **Route:** `GET /roadmaps/{name}/tasks`
- **Content:** A read-only presentation of every task of the named roadmap, of
  any status, read from that roadmap's `project.db` and laid out as a **Kanban
  board**: one fixed column per task status, each column holding one card per
  task in that status. The board is the page's only task presentation; the page
  renders no task table and offers no alternative table view. Every field a task
  has remains reachable from this page through the read-only task detail modal,
  which opens when the user selects a card (see
  [Task Detail Modal](#task-detail-modal)). The Roadmap Sprint Page presents its
  member tasks as a board too, so both surfaces that show a clickable task are
  boards whose card is the modal trigger; the two boards differ in what their
  columns stand for and in what their cards show. This page has **five** columns,
  one per task status, and its card leads with the reference line; the sprint
  page's board has **three**, grouping the sprint's tasks the way the sprint status
  summary line groups them, and its card leads with the title (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template)). The five-column
  specification below governs this page alone.
- **Structural inspiration only.** The board follows the structure of a GitLab
  issue board — columns that stand for states, cards that stand for work items,
  and a task count on each column header — and deliberately departs from it in
  interaction: this board moves nothing and edits nothing (see **Read-only**
  below).
- **Columns.** The board has exactly five columns, one for each value of the
  `TaskStatus` enum (`MODELS.md § Enums`). From left to right the columns follow
  the order of the task state machine's flow (`STATE_MACHINE.md § Task State
  Machine`):

  1. `BACKLOG`
  2. `SPRINT`
  3. `DOING`
  4. `TESTING`
  5. `COMPLETED`

  The columns are fixed: all five are always present, in that order, whatever the
  roadmap's data contains, and a column holding no task is still rendered.
  Neither the set of columns nor their order depends on the data. Each column
  title is the status identifier exactly as the enum spells it, in upper case
  (`BACKLOG`, `SPRINT`, `DOING`, `TESTING`, `COMPLETED`), and is not translated.
- **Unbounded read: every task, never a page of them.** The page reads **every**
  task of the roadmap. The read carries no limit, no page size, and no truncation,
  and the board has no pagination: whatever the roadmap holds, the board shows.

  The display default that sizes `rmp task list` output — `-l, --limit <n>`,
  default `100` (see `COMMANDS.md § List Tasks`) — MUST NOT be applied to this
  read. That default exists to size the output of one command invocation, where the
  caller who wants more asks for more and can see that the listing was cut. This
  page offers no such affordance, and it does not merely list: it groups the tasks
  into five columns and prints a count on each column header as a statement of fact
  about the roadmap. Under a partial read those counts would be wrong and would
  still be presented as true, with nothing on the page to reveal that tasks were
  omitted. Reading every task is therefore what makes the counts in **Count per
  column** correct by construction, and it is a correctness requirement of this
  page rather than a performance choice (see `DATABASE.md § Main SQL Queries`,
  "List All").

  A search term and the header filters narrow what the board **shows**; neither
  narrows what the page **reads**. The read stays the full task list either way, so
  criteria applied by the server and the same criteria applied in the browser select
  from the identical set — which is what makes the two paths equivalent (see
  **Server and client produce the same board**).
- **Placement.** Each task of the roadmap appears in exactly one column: the
  column of that task's own `status`. The board omits no task and duplicates
  none, so the five column counts sum to the roadmap's total number of tasks. The
  `tasks.status` column is restricted by a CHECK constraint to exactly these five
  values (`DATABASE.md § tasks Table`), so no task can carry a status outside
  them: the board has no sixth column and no "other" column.
- **Count per column.** Each column header shows the status name together with a
  Tabler badge carrying the number of tasks in that column, the way a GitLab
  issue board shows the issue count of each list. A column holding no task shows
  the count `0`. The count always equals the number of cards that column is
  actually showing: when the header controls narrow the board, the counts narrow
  with it (see **Effect on the board** below).

  **The badge carries the colour of its column's status.** A column of this board
  is exactly one `TaskStatus` value (see **Columns** above), so the badge is a
  hybrid: its **text** is the count of tasks in the column, and its **colour** is
  the variant the task status table assigns to that column's status (see
  [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
  rule 2). The colours are the ones that table already holds; this board introduces
  no new colour and no new band, and it keys on that mapping rather than restating
  the variants here. The badge's class is produced by the single implementation of
  that mapping which every status badge on this page already takes its colour from;
  no colour variant is written into the template beside it, so this header cannot
  drift from the mapping the rest of the page obeys (Acceptance Criterion 140). The
  count itself selects no colour, so a column that holds no task shows `0` in the
  colour of its status, exactly as a tab that holds no sprint does (see
  [Roadmap Sprints Page](#roadmap-sprints-page)). The colour earns its place
  because the header is where the reader identifies the column: the mapping
  already gives each status a colour the reader meets wherever that status is
  written out, and carrying it here lets the five columns tell themselves apart by
  the same key rather than by their heading text alone.
- **Order within a column.** The cards of a column appear in a deterministic
  order: descending `priority` and, for tasks of equal priority, ascending
  `created_at`. This is the order in which the page's own read already returns the
  roadmap's tasks — the default `ListTasks` ordering,
  `ORDER BY priority DESC, created_at ASC` (see
  `DATABASE.md § Main SQL Queries`, "List All"). Grouping the tasks into columns
  preserves that relative order: the tasks of one column appear in the same
  relative order in which the read returned them, so the board introduces no
  second sort and no ordering of its own.
- **Card content.** Each card presents one task, in this order:
  1. A **reference line** at the top of the card showing the task reference
     `#<id>` (the task's `id`) and the task's `type` (the `TaskType` value; see
     `MODELS.md § Enums`). Both are rendered as muted text. The `type` carries no
     colour: the semantic colour mapping in
     [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours)
     covers task and sprint status, priority, and severity only, and is not
     extended to the task type.
  2. The task **`title`**, presented as the card's prominent main content.
  3. A **`priority` badge** and a **`severity` badge**, in that order, each
     carrying that task's integer value and coloured by the band the value falls
     in, using exactly the mapping in
     [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours).
     No new badge colour and no new band is introduced here.

     **Each badge names the value it carries with a one-letter prefix.** The
     priority badge reads `P` immediately followed by the task's `priority`, and
     the severity badge reads `S` immediately followed by the task's `severity`,
     with no space and no separator between the letter and the digits: a task of
     priority `5` and severity `3` shows `P5` and `S3`.

     The prefix is the label the card has no room to write out. Wherever else this
     interface shows these two values, a word names each of them — the task detail
     modal writes the field's name beside the value (see
     [Task Detail Modal](#task-detail-modal)) — but a card is read at a glance, and
     without the prefix it would put two bare integers side by side and state
     nowhere which one is the priority and which one is the severity. A reader
     would have to know the order by heart to tell `5` from `3`. The prefix states
     that word in the one character the card can spare for it.

     **This rule governs the card of both boards.** The card of the sprint page's
     member-tasks board renders the same pair in the same way (see
     [Sprint Detail Sub-Template](#sprint-detail-sub-template)). The rule is stated
     here once for both cards rather than twice, so that the two cannot drift apart
     on it: the two boards differ in what their columns stand for and in what else
     their cards show, and they keep **one** form for this pair.

     **The prefix is a label, not a value.** It changes what the badge reads and
     nothing else. The colour still follows the value alone, through exactly the
     band mapping named above: `P5` takes the colour that mapping assigns to the
     priority `5`, and the prefix selects no colour, introduces no band, and
     changes no meaning. The card's accessible name is unaffected as well: it is
     `Open details for task #<id>: <title>` (see **Clickable card** below), it
     carries neither value, and so it carries no prefix.

     **Only these two badges take a prefix.** A prefix earns its place only where
     no label names the value, which is true of the priority and severity badges on
     a board card and of no other badge in this interface. A status badge is never
     ambiguous, because its own text is the status name — it reads `COMPLETED`, not
     a bare integer — so it takes no prefix wherever it is shown; this card shows no
     status badge at all (see below). The priority and severity badges of the task
     detail modal take no prefix either, for the reason stated there.
  4. A **metadata footer** showing only the indicators the task actually has:
     the sprint the task belongs to, its number of subtasks (`subtask_count`), its
     number of `depends_on` entries, its number of `blocks` entries, and its number
     of comments. Each indicator is rendered with the icon or label that identifies
     what it counts, so the footer is readable without a legend.

     **The sprint indicator.** A task that belongs to a sprint shows that sprint
     on its card, the way a GitLab issue card shows the issue's milestone. The
     card identifies the sprint by its `title` together with `Sprint #<id>` (the
     `Sprint` model's `title` and `id`; see `MODELS.md § Sprint`). Both parts are
     shown because the `title` alone does not identify a sprint: `MODELS.md § Sprint`
     requires the `title` to be present and caps its length, but places no
     uniqueness constraint on it, so two sprints of one roadmap may carry the same
     title, while the `id` is the primary key and is unique. Showing both is also
     the identification idiom the rest of the interface already uses for a sprint
     (see [Shared Sprint-Card Partial](#shared-sprint-card-partial) and
     [Roadmap Sprint Page](#roadmap-sprint-page)).

     A task belongs to **at most one** sprint, so the indicator names at most one
     sprint and never a list. This is guaranteed by the schema, not by convention:
     `sprint_tasks.task_id` carries a `UNIQUE` constraint (see
     `DATABASE.md § sprint_tasks Table (1:N Relationship)` and
     `DATABASE.md § Relationships`).

     The sprint indicator is **plain text, not a link**. The whole card is a single
     `<button>` that opens the task detail modal (see **Clickable card** below), and
     a link cannot be nested inside it: a button's content model admits no
     interactive descendant, so a nested link would be invalid markup and would put
     two competing activation targets in one control, leaving pointer, touch, and
     keyboard activation ambiguous about which target the user meant. The sprint's
     own page stays one step away through the sidebar and the sprints page, so
     nothing becomes unreachable.

  The card shows **no status badge**, because the column the card sits in already
  states the task's status.

  **Absent metadata renders nothing.** An indicator whose value is absent, empty,
  or zero is not rendered at all: no dash, no placeholder, no empty slot. A task
  that belongs to no sprint shows no sprint indicator — not a dash, not "None", not
  an empty slot; a task with `subtask_count` `0`, with no `depends_on` entry, with
  no `blocks` entry, or with no comment shows no corresponding indicator. A task
  with none of the five shows no metadata footer at all.

  The card presents a subset of the task's fields by design. Every field of the
  `Task` model — including the long free-text fields, the lifecycle timestamps,
  the parent task link, and the full dependency lists — is shown in the task
  detail modal the card opens (see [Task Detail Modal](#task-detail-modal)). The
  card does not redefine any field; `MODELS.md` and `DATABASE.md` remain
  canonical.

  The absent-metadata rule above governs **this** board's card. The card of the
  sprint page's member-tasks board departs from it deliberately and always renders
  both of its counters, for the reason stated where that card is defined (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template), **The card**).
- **Clickable card.** Selecting a card opens the read-only task detail modal for
  that task (see [Task Detail Modal](#task-detail-modal)). Opening the modal
  fetches that task's fields and comments from the read-only endpoint
  `GET /roadmaps/{name}/tasks/{id}/data` and fills the page's single modal shell
  with them (see [Task Detail Endpoint](#task-detail-endpoint)). That request is
  made when the user opens the task, not while the page is rendered, so it adds no
  query to the page's own read and no per-task cost to the board. It opens no write
  path.

  The card **is** the trigger, and the trigger is a `<button type="button">`, a
  natively activatable element. A pointer click, a touch tap, and the keyboard
  (Enter and Space) therefore all open the modal through the browser's own
  activation behaviour, with no added JavaScript, so the board is fully usable
  without a pointing device. Because a `<button>` is focusable and exposes the
  button role on its own, the card carries no `tabindex` and no `role="button"`:
  both would be redundant, and neither would grant activation to an element that
  lacked it (see [Task Detail Modal](#task-detail-modal), *The trigger is a
  natively activatable element*). The card's accessible name is
  `Open details for task #<id>: <title>`, naming the action and identifying the
  task by `id` and `title`, and containing the card's own visible title text. The
  card keeps the Tabler card presentation specified under
  **Markup** above; making it a button changes the element, not the appearance.
- **Header search control.** The page header's actions column carries a **search
  input** that narrows the board. That input and the three filter dropdowns of
  **Header filter controls** below are the only controls in that column: the page
  header presents no link to the knowledge-graph page, because the admin-shell
  sidebar already lists **Graph** among the roadmap's own links on every page (see
  [UI Framework](#ui-framework), rule 1), so a header link would be a second route
  to a destination the page already offers, and removing it costs no access. The
  actions column keeps the Tabler idiom fixed in [UI Framework](#ui-framework),
  rule 16.

  The input carries a real, programmatically associated **label** naming what it
  searches. A `placeholder` MUST NOT stand in for that label: a placeholder is not
  an accessible name and disappears as soon as the user types. Where the label is
  visible, the input's accessible name contains the visible label text, by the rule
  in [Task Detail Modal](#task-detail-modal), *The trigger is a natively activatable
  element*. The control is reachable and operable from the keyboard.
- **What the search matches.** A task matches a term when the term occurs in that
  task's **searchable text**, which is the concatenation of exactly two things the
  card itself displays:
  1. the task `title`;
  2. the task reference `#<id>`, written with its leading `#`.

  Including the reference is deliberate: the card shows `#<id>` in its reference
  line, so a user reading a card can see it, and typing `42` to reach task 42 is
  the obvious gesture. Because the reference is matched as the literal string
  `#42`, both `42` and `#42` find it under the one substring rule below, with no
  special case for either form.

  Every other task field is deliberately **excluded**. The search answers "which
  task is this?" from what identifies a task on its card;
  matching an attribute answers a different question, "which tasks share this
  property?", which is the job of the type, priority, and severity filters below and
  would make one control serve two purposes with no way for the user to tell which
  one produced a hit. Keeping the two apart is what lets them compose (see **Header
  filter controls** and **How the criteria compose** below).
- **Matching rule.** Matching is **case-insensitive** and by **substring**: a task
  matches when its searchable text contains the term. Leading and trailing
  whitespace is stripped from the term before matching, and a term that is empty
  or entirely whitespace is **no term at all** — the board shows every task.
  Whitespace inside the term is significant and is matched literally.

  The paragraph above names two transformations of the term, and each of the two has
  to be stated exactly rather than only described. **The trim rule** below fixes
  which code points count as the whitespace that is stripped. Case-insensitivity
  means one specific transformation, applied to the task's searchable text and to
  the term before the two are compared, and **The folding rule** below states which
  transformation it is. Stating each exactly — rather than requiring only that
  whitespace be removed and that the viewer's locale be ignored — is what keeps the
  two paths of **Server and client produce the same board** below from disagreeing
  about a term: a description both paths satisfy while returning different terms
  fixes nothing.
- **The trim rule.** Before the term is folded, every code point carrying Unicode's
  **White_Space** property — the property Unicode's own character database
  publishes under that name — is removed from the **start** of the term and from its
  **end**. Removal stops at the first code point that does not carry the property,
  so a code point carrying it anywhere else in the term survives, is part of the
  term, and is matched literally (see **Matching rule** above). A term made only of
  such code points becomes the empty string, which is no term at all and shows every
  task.

  The set is named by that property, and deliberately **not** by either platform's
  own trimming function, because the two functions do not implement the same set and
  a rule stated as "surrounding whitespace is stripped" would therefore fix nothing:
  both platforms satisfy that description while disagreeing about which term they
  produce. The difference is observable rather than academic, and this specification
  fixes both code points on which the two disagree — they disagree in **opposite**
  directions:
  - `U+0085` (NEXT LINE) **carries** the White_Space property, so it **IS** removed
    from the ends of a term, although the JavaScript platform's own trimming keeps
    it: that platform trims the code points it classes as white space together with
    its line terminators, and `U+0085` is in neither group.
  - `U+FEFF` (ZERO WIDTH NO-BREAK SPACE) does **not** carry the White_Space
    property, so it is **NOT** removed, although the JavaScript platform's own
    trimming removes it: that platform lists this one format character in its white
    space explicitly.

  Those two are the whole of the difference at any one Unicode version: swept over
  every code point of Unicode, no third code point is removed by one trimming and
  kept by the other. Ordinary terms are therefore untouched by the distinction — the
  space, the tab, the carriage return, and the line feed a user can type are removed
  under either.

  **The cost of the choice is stated plainly rather than patched over.** A term
  pasted with a leading byte-order mark keeps that `U+FEFF`, and so matches nothing
  on an ordinary roadmap. It does so on **both** paths, which is the property this
  rule exists to protect: a term whose two paths disagree — a card on one of them
  and nothing on the other — would break **Server and client produce the same
  board** below, and that disagreement, not the empty result, is the defect. Nothing
  is stripped after the trim to compensate, for the reason **The folding rule** below
  gives for its own post-fold fixups.

  **Trim first, then normalise, then fold.** The term is trimmed, then normalised,
  then folded, in that order, and **both** paths perform those steps in that same
  order (see **The normalisation rule** and **The folding rule** below). The trim's
  place in that sequence is not observable: swept over every code point of Unicode,
  no code point carrying the White_Space property folds to anything but itself,
  none outside the property folds into it, and normalisation neither turns a code
  point carrying the property into one that does not carry it nor the reverse — the
  two code points normalisation does rewrite, `U+2000` (EN QUAD) to `U+2002` and
  `U+2001` (EM QUAD) to `U+2003`, carry the property before and after. Trimming
  therefore commutes with both later steps. Fixing the order is what keeps the
  contract from resting on that coincidence: were some code point ever to fold or
  normalise into a whitespace one, the two paths would still perform the same steps
  in the same order and would still return one term.

  The place of the **normalisation** relative to the fold is a different matter: it
  is observable, and **The normalisation rule** below states why normalising first
  is the only order that closes this defect.

  **The task's searchable text is normalised and folded, but never trimmed.** The trim applies to
  the term alone. A task's own leading or trailing whitespace is part of its text
  and is matched literally, exactly as whitespace inside a term is; the term is
  trimmed because a user reaches for the space bar around what they type, which is
  not a statement about the task.
- **The normalisation rule.** After the term is trimmed, and before either the term
  or the task's searchable text is folded, both are normalised to Unicode's
  **Normalization Form C** — NFC, the canonical composition of the full canonical
  decomposition, as UAX #15 defines it.

  The rule exists because two different byte sequences can render as the same text.
  A task whose `title` holds a precomposed `é` (`U+00E9`) and a task whose `title`
  holds `e` followed by a combining acute (`U+0065 U+0301`) carry the same title to
  every reader and two different searchable texts to a comparison of bytes, so a
  term typed in one spelling finds one of them and silently misses the other. Both
  paths miss it **together**, so this is not the disagreement **Server and client
  produce the same board** below governs; it is a second and independent way the
  search fails to find a task that visibly contains the term.

  **Normalisation is for comparison only: never for storage, and never for
  display.** The bytes `rmp` stores stay exactly the bytes it was given. A task's
  `title` is not rewritten, `rmp task get` returns what it returned before this rule
  existed, and the card renders the title the roadmap actually holds. The normalised
  text is a derived form used to decide a match, exactly as the folded corpus
  already is.

  **NFC and not NFD.** Both are canonical equivalence, and either would make the two
  spellings of `é` one text. They differ in what else they do to a **substring**
  match, which is the comparison this search performs. NFD decomposes every accented
  letter, so a task titled `Café Lisboa onboarding` would carry the searchable text
  `cafe` followed by `U+0301` and the rest, and the four ASCII letters of the term
  `cafe` would occur in it: typing `cafe` would return the task titled `Café`, and
  typing `ae` would return one titled `Aérea`. NFC leaves a precomposed letter
  precomposed, so neither term matches and an accented word stays one unit. The
  rule answers what a term **is**, and it must not quietly answer whether an accent
  should be ignored, which is a different question and one this specification does
  not answer.

  **What the rule changes, measured.** Of Unicode's 1,112,064 code points, exactly
  **1,117** produce a different searchable text under this rule than without it, and
  **not one of them is ASCII**. Those 1,117 are the canonical singletons and the
  composition exclusions — `U+0340`, `U+0341`, `U+0343`, `U+0344`, `U+0374`,
  `U+037E`, `U+0387`, the `U+0958`..`U+095F` Devanagari set, and their kind. An
  ordinary Latin roadmap is untouched, which is what makes this rule safe to apply
  to every task rather than only to the ones a user suspects.

  **Two passes, not one.** The pipeline is trim, then NFC, then fold, then **NFC
  again**. The second pass is not decoration: the fold can produce a sequence that
  composes where the unfolded one did not. Unicode has no precomposed capital for
  `H` with a line below, so NFC leaves `H` followed by `U+0331` as two code points;
  the fold then lowers the `H`, and `h` followed by `U+0331` **does** have a
  precomposed form, `U+1E96`. Without the second pass a task titled `H̱ydro` would
  carry a two-code-point searchable text while a term typed as the single character
  `ẖ` normalised to `U+1E96`, and the term would not occur in the text it plainly
  spells. `U+1E97`, `U+1E98`, `U+1E99` and `U+01F0` behave the same way. Measured
  over the 1,440,384 sequences of a folding code point followed by a non-starter,
  one pass leaves the result outside NFC on **70** of them; two passes leave it in
  NFC on **all 1,440,384**, so a third pass would change nothing and is not
  performed.

  **Normalise before folding, not after.** The order is observable: on **0** single
  code points, but on **74** of those 1,440,384 sequences, over **32** distinct
  leading code points. Normalising first is chosen because it is the only order that
  closes this defect for `U+0130` (LATIN CAPITAL LETTER I WITH DOT ABOVE), the code
  point **The folding rule** below already names. A title written with `U+0130` and a
  title written as `U+0049` followed by `U+0307` are the same text by Unicode's own
  definition, and normalising first gives both the same searchable text; folding
  first would give `U+0069` for one and `U+0069 U+0307` for the other, which are two
  different searchable texts for one title.

  **The composition step is Groadmap's own; the decomposition step is not.** The
  server obtains the canonical decomposition and the canonical ordering from
  `golang.org/x/text/unicode/norm` (see `BUILD.md § External Dependencies`), and
  performs the composition itself, from the same `COMPOSE_TABLE` it ships to the
  browser (see **One rule, and only one implementation of it** below). That module's
  own composition is **not** used, because it is wrong: at the pinned version it
  composes a supplementary starter as though the starter were its low 16 bits, so
  `U+1003C` followed by `U+0338` becomes `U+226E` (because `U+1003C` masked to 16
  bits is `U+003C`), `U+10041` followed by `U+0301` becomes `U+00C1`, and `U+1042B`
  followed by `U+0308` becomes `U+04F8`. The platform's own normalisation leaves all
  three unchanged, and so does Groadmap's. The defect spans **15,342** pairs over
  **6,232** distinct leading code points; the decomposition the server does use is
  unaffected by it.

  Composing from the table is not a private dialect of NFC. It is NFC where that
  module is right and NFC where that module is wrong: the two agree on **all
  1,112,064** single code points, and the table still composes the 33 legitimate
  supplementary composites, `U+11935` followed by `U+11930` giving `U+11938` among
  them. This is why the server takes the client's table rather than the client
  taking the server's answer — the alternative would mean generating a table that
  reproduced a truncation defect on purpose, and a client that was right while the
  server was wrong would break **Server and client produce the same board** below,
  which is the one property none of these rules may cost.

  A term whose bytes are not valid UTF-8 is normalised like any other term, after
  each invalid byte has been replaced by `U+FFFD` (see **The folding rule** below).
- **The folding rule.** The task's searchable text and the term are folded — each
  after it has been normalised (see **The normalisation rule** above) — by
  Unicode's **simple lowercase mapping**: the single replacement code point that
  the Unicode Character Database gives a code point, applied to each code point on
  its own, with a code point that has no such mapping folding to itself. Three
  properties follow from that definition, and every implementation of the rule MUST
  have all three:
  1. **Unconditional.** What a code point folds to never depends on the code points
     around it. No context — the start or the end of a word, the letters before or
     after it, the presence of another cased letter — changes the result.
  2. **One code point in, one code point out.** The fold replaces each code point
     with exactly one code point. It adds none, removes none, and reorders none, so
     folding never lengthens or shortens the text.
  3. **Locale-independent.** The fold consults no locale, so the same term and the
     same task produce the same verdict wherever the page is rendered and whatever
     locale the browser reports. A locale-sensitive case conversion MUST NOT be
     used.

  The rule is deliberately **not** Unicode's Default Case Conversion — the full
  case conversion, with its conditional rules and its multi-code-point special-case
  mappings, which is the conversion a programming language's ordinary lower-case
  function may implement. The difference is observable rather than academic, and
  this specification fixes both code points on which the two conversions disagree:
  - `U+0130` (LATIN CAPITAL LETTER I WITH DOT ABOVE) folds to `U+0069` alone, and
    never to the two code points `U+0069 U+0307` the full conversion produces for
    it.
  - `U+03A3` (GREEK CAPITAL LETTER SIGMA) folds to `U+03C3` in **every** position,
    word-final included, and never to the final form `U+03C2` the full conversion
    produces where its Final_Sigma condition holds.

  Those two are the whole of the difference at any one Unicode version: swept over
  every code point of Unicode in a range of neighbouring contexts, no third code
  point folds differently under the two conversions. Ordinary ASCII and accented
  Latin text is therefore untouched by the distinction — `A` folds to `a`, `Á` to
  `á`, letter for letter.

  **Nothing is rewritten after the mapping.** A `U+03C2` the user typed is already a
  lower-case letter, folds to itself, and MUST NOT be rewritten to `U+03C3`
  afterwards: a task titled `οδός` carries that `U+03C2` in its folded searchable
  text, so a term rewritten that way would stop finding it. The same holds for
  every other post-fold fixup, such as removing a `U+0307` the user typed. The cost
  of the simple mapping is stated plainly rather than patched over: a task whose
  title ends in a literal `ς` is not found by typing that word in capitals, because
  the capital folds to `σ`. Both paths return that same verdict, which is the
  property the rule exists to protect; whether two differently spelled forms of one
  word should match each other is a different question from case, and this rule does
  not answer it.

  A term whose bytes are not valid UTF-8 is not a sequence of code points at all:
  the server replaces each invalid byte with `U+FFFD` before folding, and the term
  is then folded and matched like any other — it matches nothing on an ordinary
  roadmap, and it is neither an error nor an absent term (see **No malformed term
  is an error** below).
- **One rule, and only one implementation of it.** A task's searchable text is
  normalised and folded **once, by the server**. The client normalises and folds
  only the term, and compares it against text the server already transformed; no
  client-side code normalises or folds a task's `title` or its reference, and no
  client-side code trims either. The two paths therefore cannot disagree about a
  task's text, because only one of them ever transforms it.

  The term is the one value both sides fold, and it is where the two could still
  drift, because each platform's own lower-case function implements whichever
  conversion that platform chose. The client therefore **MUST NOT** fold the term
  with the JavaScript platform's case conversion, locale-sensitive or not. It folds
  the term with the **server's own mapping**, which the server ships to it together
  with the script that narrows the board. The client consults no case-conversion
  table of the browser's, and the fold of a term is the server's answer on both
  paths by construction, rather than by two implementations happening to agree.

  Shipping the mapping settles a second question with the same move. A browser's
  case tables are of whatever Unicode version that browser ships, which Groadmap
  neither chooses nor can detect, so a fold that consulted them would be a fold two
  browsers could answer differently for the same term. The shipped mapping removes
  the browser from the answer entirely.

  **The term's trim is the server's by the same construction.** Preparing a term for
  comparison is three steps, and the client MUST NOT take any of them from the
  platform: it
  **MUST NOT** trim the term with the JavaScript platform's trimming function, any
  more than it may fold it with that platform's case conversion. It removes the
  term's leading and trailing whitespace by the **server's own whitespace set**,
  which the server ships to it together with the mapping and the script that narrows
  the board, so which code points a term loses at its ends is the server's answer on
  both paths by construction. Leaving that one step to the platform would be enough
  to break the equivalence on its own, and would break it quietly: the two trimmings
  agree on every code point but the two **The trim rule** above names, so every
  ordinary term would go on agreeing and hide the disagreement.

  **The term's normalisation is the server's by that same construction, and the
  prohibition extends to it.** The client **MUST NOT** call the JavaScript
  platform's own normalisation — `String.prototype.normalize` — any more than it may
  call that platform's trimming or its case conversion. It normalises the term from
  the **tables the server ships to it**, so the normalised form of a term is the
  server's answer on both paths by construction.

  Two reasons make that prohibition stricter here than for the other two steps, and
  the second is decisive. The first is the one already given for them: a browser's
  normalisation tables are of whatever Unicode version that browser ships, which
  Groadmap neither chooses nor can detect. The second is that the platform's
  normalisation and the server's would have to agree on **composition**, and
  composition is exactly where the server's own module is wrong (see **The
  normalisation rule** above). The server composes from the shipped table
  specifically so that both sides run one rule over **one set of data**, rather than
  two expressions of one description that could agree with each other by
  reproducing the same defect.

  On the server, the corpus and the term are likewise **one** rule at every step:
  the server normalises a task's searchable text and normalises a term through the
  same function and the same tables, and folds both through the same folding
  function, not through two implementations of one description, so the two cannot
  drift apart on that side either.
- **What keeps the shipped rule equal to the server's.** The binary ships the client
  the things a term's preparation is made of — the whitespace set, the case mapping,
  and the normalisation data — and **one** check covers all of them. It is one check
  and not several beside each other because they are parts of one rule: a check that
  took only the mapping as its subject would leave the whitespace set and the
  normalisation data free to drift, and either of those drifting separates the two
  paths exactly as a drifting mapping would.

  The normalisation data is **three generated tables**, shipped in
  `static/task-search.js` exactly as `FOLD_TABLE` and `SPACE_TABLE` already are:
  `DECOMP_TABLE`, the full canonical decompositions, **2,081** entries;
  `CCC_TABLE`, the canonical combining classes, **403** spans; and `COMPOSE_TABLE`,
  the primary composites, **961** entries. Together the three are the largest part
  of the script the binary serves — on the order of 60 KB, well over half of it.
  They are that small only because Hangul is **not** tabulated: UAX #15 decomposes
  and composes Hangul arithmetically, so the 11,172
  Hangul syllables are computed on both sides rather than stored. Tabulating them
  would take `DECOMP_TABLE` from 2,081 entries to 13,253 and `COMPOSE_TABLE` from
  961 to 12,133, for data that a few lines of arithmetic already give exactly.

  **That size is an order of magnitude and not a byte count, deliberately.** The
  three entry counts above are backed: the check described below reads the shipped
  tables as numbers and requires the count, and every entry, to equal what the
  server's own function derives, so an entry count stated here cannot drift from
  the artefact without the `test` gate failing. A byte count has no such backing.
  It is a property of the generator's layout — the indentation, the line width,
  and the separator its emitter writes — and the check is blind to all three,
  because it extracts the numbers and ignores the text around them. Changing any
  of the three would move a byte count stated here and leave every gate green. A
  figure a reviewer trusts and no gate checks is worse than no figure at all, so
  this section states none: whoever needs the exact size measures the artefact,
  which is its only authority.

  Each part is checked against the server's own function over **the whole of
  Unicode**: every code point, not a sample, and against that function itself, never
  against a stored copy of its expected results — such a copy can be updated to
  match a changed fold, a changed whitespace set, or changed normalisation data, and
  would then prove nothing. The check fails when a single code point folds
  differently on the two sides; it fails the same way when a single code point is
  whitespace to one side and not to the other; and it fails the same way when a
  single code point decomposes, orders, or composes differently between the shipped
  tables and the server. It fails the same way again when a toolchain upgrade or a
  dependency upgrade changes any of them, so a change of Unicode version cannot move
  one side of the rule and leave the other behind unnoticed: a server whose rule
  moved is **caught**, never followed. The check also asserts, as an absence in the
  script the binary serves, that the narrowing script calls neither a case
  conversion of the platform, nor a trimming function of the platform, nor the
  platform's own normalisation.

  **The table's composition is proven correct on every run.** The shipped
  `COMPOSE_TABLE` is not merely equal to the server's data — it is equal to the
  rule Unicode defines, which is what makes the server adopting it a correction
  rather than a divergence. Three checks establish that, and each of them
  re-establishes it whenever the `test` gate runs. Two live in
  `internal/unicodenorm`. The first requires the server's Normalization Form C to
  equal `golang.org/x/text/unicode/norm`'s over **all 1,112,064** single code
  points, which is the one domain in which that module is a valid reference: the
  truncation defect **The normalisation rule** describes needs a pair to arise, so
  over longer inputs the two forms differ by design. The second requires the
  composition exclusions the server derives to equal Full_Composition_Exclusion as
  the Unicode Character Database publishes it, over the whole of Unicode and in
  **both** directions — a false positive drops a composite Unicode composes, a
  false negative admits one Unicode excludes, and a single total would let the two
  cancel. The third is the check this section already describes: the shipped
  tables against the server's own functions, every code point and not a sample.

  **A count of inputs stood here, and it was withdrawn rather than updated.** It
  reported that a prototype driven by these tables had been checked against the
  platform's own normalisation over 69,956,194 inputs with 0 failures, broken into
  five domains. Three reasons removed it. The prototype no longer exists, so the
  run cannot be repeated. Four of the five domains are sized by the Unicode
  version, so the total moved the day the toolchain moved and nothing reported
  that it had. And re-deriving the domain sizes would have restated a proven
  result over inputs no measurement ever visited, which is worse than an obsolete
  figure and not better. This is **That size is an order of magnitude and not a
  byte count, deliberately**, above, applied to this paragraph: a figure a reviewer
  trusts and no gate checks is worse than no figure at all. What stands in its
  place is the stronger claim rather than the smaller one, because the count
  recorded that the rule had been correct once and the three checks require it to
  be correct now.

  That third check is an ordinary Go test. It runs no JavaScript and requires no
  JavaScript engine, no Node.js, no network access, and no module beyond the direct dependencies
  `BUILD.md § External Dependencies` names, so it holds within the constraints
  already fixed in that section and in `BUILD.md § Vendored Web Assets`,
  rule 2. It is the discipline the badge
  colour mapping already follows wherever a client script carries that mapping too
  (see
  [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
  rule 2).
- **Header filter controls.** Beside the search input, the page header's actions
  column carries **three filter dropdowns** (select controls) that narrow the board
  by what a task **is**, where the search narrows it by what a task is **called**:

  1. a **type filter**, offering the ten `TaskType` values (`MODELS.md § Enums`);
  2. a **minimum-priority filter**, offering the thresholds `1` to `9` over the
     task's `priority` (`MODELS.md § Task`);
  3. a **minimum-severity filter**, offering the thresholds `1` to `9` over the
     task's `severity` (`MODELS.md § Task`).

  The ten type values are enumerated in `MODELS.md § Enums` and are not restated
  here, so the type filter cannot drift from the enum. The thresholds are the
  `priority` and `severity` range of `MODELS.md § Task` without its `0` floor, for
  the reason **What each filter matches** below gives.

  These are exactly the three dimensions the CLI already filters `rmp task list` by
  — `-y, --type`, `-p, --priority`, and `--severity` (see
  `COMMANDS.md § List Tasks`) — so the page presents no less capability over the
  board than the command that lists the same data. Each dimension is also
  visible on the card the filter acts on: the card's reference line shows the task's
  `type`, and the two badges show its `priority` and its `severity` (see **Card
  content** above), so the user filters by values the board already displays, as the
  search matches text the card already displays.

  Each dropdown offers, as its **first** option, a value meaning *no filter on this
  dimension* — for example `Any type` — and that option is the selected one whenever
  the dimension carries no filter. That option is a **value**, not the control's
  name: each dropdown carries a real, programmatically associated **label** naming
  the dimension it filters, and neither a first option nor a `placeholder` stands in
  for that label, by the same rule **Header search control** above applies to the
  search input. Each control is reachable and operable from the keyboard.

  **Each dimension takes exactly one value.** A dimension carries one filter or it
  carries none; it never carries a set. `rmp task list` is single-valued on all
  three flags, a threshold is a single number by construction, and one value per
  dimension keeps a single filtering model across the three controls instead of a
  set-valued model for the categorical dimension and a scalar one for the two
  ordinal dimensions.

  **A filter value is never echoed into the page.** Unlike the term, a filter value
  is not caller-supplied text rendered back to the user: the options are the fixed
  sets enumerated above, emitted by the server from the enum and from the range, and
  all a parameter does is decide which of those options is marked as selected. A
  value that is not one of them selects the no-filter option (see **No filter value
  is an error** below), so no caller-supplied string reaches the page through
  `type`, `priority`, or `severity`, and the question that **Escaping the term**
  below answers for `q` does not arise for the filters.
- **What each filter matches.** The three dimensions do not compare the same way,
  and each keeps the meaning the CLI flag of the same name already carries (see
  `COMMANDS.md § List Tasks`), so one parameter name means one thing across the two
  surfaces:
  - **Type is an equality.** A task matches when its `type` is **equal to** the
    selected `TaskType` value. The comparison is exact against the value as
    `MODELS.md § Enums` spells it, in upper case; a differently spelled or
    differently cased value is not one of the ten and is handled by **No filter
    value is an error** below.
  - **Priority and severity are thresholds.** A task matches the priority filter
    when its `priority` is **greater than or equal to** the selected value, and the
    severity filter when its `severity` is greater than or equal to the selected
    value. This is the meaning `rmp task list` already gives `-p, --priority <n>`
    ("Filter priority >= n") and `--severity <n>` ("Filter severity >= n"), and
    `priority` and `severity` are ordinal `0`-`9` ranges rather than categories
    (`MODELS.md § Task`), so "at least" is the comparison that fits them.

    The offered thresholds start at `1` and not at `0` because a threshold of `0`
    admits every task and is therefore the unfiltered board, which already has its
    own option and its own URL form — the parameter absent (see **The URL carries
    the filters** below). Offering `0` would give one board two URLs and two
    control settings, which is what **An empty term leaves no parameter** exists to
    prevent for the term.
- **Why the board offers no status filter.** The board deliberately offers **no**
  filter over a task's `status`, and the omission follows from the layout rather
  than from an oversight:
  1. **The columns already are the status.** The board has exactly five fixed
     columns, one per `TaskStatus` value, and each task sits in the column of its
     own status (see **Columns** and **Placement** above). The narrowing a status
     filter would perform is the narrowing the layout has performed already: a user
     who wants the `DOING` tasks reads the `DOING` column, which is already
     separate, already ordered, and already counted.
  2. **Keeping the columns would make the board state something false.** A status
     filter that left the five columns in place would leave the excluded columns
     present, in order, and showing the count `0`, while the roadmap holds tasks in
     those statuses. A column count is a statement of fact about what that column
     shows (see **Count per column** above), so the board would state that the
     roadmap holds no task in a status that in fact holds many.
  3. **Dropping the columns would contradict the layout.** A status filter that
     instead dropped or hid the excluded columns would break the rule that all five
     columns are always present, in order, whatever the data contains (see
     **Columns** above), and would leave the filter and the layout disagreeing
     about how many columns a board has.
  4. **Two controls would state one fact.** With a status filter active, a card's
     status would be stated twice on one screen — by the column the card sits in
     and by the control that admitted it — and nothing would keep a reader from
     taking the two statements for two different facts.

  Status is therefore the one task attribute this page presents **structurally**,
  and the header controls filter only attributes the layout does not already
  express.
- **No filter value is an error.** A filter parameter whose value is not one the
  dimension accepts applies **no filter on that dimension**, and the board is
  rendered exactly as though that parameter were absent. This covers every way a
  value can fail to be accepted: a `type` that is not one of the ten `TaskType`
  values, including one that differs from a value only in case; a `priority` or
  `severity` that is not an integer, or is an integer outside `1` to `9` — `0`
  included, because a threshold of `0` is no filter (see **What each filter
  matches** above); a value carrying a sign, surrounding spaces, or any other
  decoration; a parameter present with an empty value; and a parameter the server
  cannot decode.

  The dimensions are independent under this rule: an unusable `type` leaves an
  accepted `priority` applied and the search term applying, and narrows nothing of
  its own. No filter value produces an error page and none changes the route's
  status codes — **No malformed term is an error** below holds for the filters
  exactly as it holds for the term, and the page answers HTTP 200 whatever the
  three parameters carry.

  **One value is read per dimension.** Because each dimension takes exactly one
  value (see **Header filter controls** above), a URL that repeats a parameter —
  `?type=BUG&type=EPIC` — is read as its **first** occurrence and the remaining
  occurrences are ignored, so a hand-written URL has one defined reading rather
  than an implementation-defined one. A single value that packs several —
  `?type=BUG,EPIC` — is not a list: it is one string, that string is not one of the
  ten `TaskType` values, and the rule above therefore ignores it. Neither form is a
  partly valid filter, so this contract needs no rule for "some values accepted,
  some not": a dimension is filtered by one accepted value, or it is not filtered.
- **Effect on the board.** A task that does not satisfy every active criterion is
  not shown. Everything the board states about itself then refers to the **shown
  set**, not to the roadmap.
  This holds continuously: as the user types a term or changes a filter, the counts,
  the empty states, and the no-match message are updated together with the cards, so
  the board is never left stating something true of a previous set of controls or of
  the unnarrowed roadmap:
  - Each column shows only its matching cards, in the order fixed by **Order within
    a column**, which the narrowing preserves.
  - **Each column's count is the number of cards that column is showing.** The
    counts follow the narrowing. A count that kept reporting the unfiltered total
    while the column displayed fewer cards would state something false about what
    the user is looking at, which is exactly what **Count per column** exists to
    prevent.
  - **"Shown" means visible to the user, not present in the document.** A card that
    is present but marked as not visible is not shown: it counts towards nothing the
    board states, and a column whose every card is in that state displays its empty
    state exactly as a column with no such card would.
  - The five columns remain present and in order. Neither searching nor filtering
    ever drops, hides, or reorders a column. The board offers no status filter, so
    no control narrows the columns themselves (see **Why the board offers no status
    filter** above).
  - A column left with no matching card shows its ordinary in-column empty state.
  - When **no** task matches, the board says so: it shows a clear message naming
    that no task matches the controls the board is currently narrowed by, alongside
    the five empty columns, rather than leaving five silently empty columns for the
    user to interpret. One message covers the term and the filters together, because
    the shown set is their conjunction and singling out one control would attribute
    the empty result to a cause the board cannot know. This is distinct from a
    roadmap that holds no task at all, which is not the result of any control and is
    covered by **Empty states**.
- **The URL carries the term.** The term travels in the URL query parameter **`q`**
  on `/roadmaps/{name}/tasks`. The name matches the role `q` already has on the
  graph data endpoint — the text the user typed into a search control (see
  [Graph Data Endpoint](#graph-data-endpoint)) — and the two are distinct routes,
  so the shared name carries one meaning per route and no ambiguity.
  - **Live typing updates the URL in place.** As the user types, the page replaces
    the current history entry so the address bar always reflects the board on
    screen. It MUST NOT push a new history entry per keystroke, which would turn
    the browser Back button into an undo key for typing.
  - **An empty term leaves no parameter.** When the term is empty or entirely
    whitespace, `q` is **removed** from the URL rather than left present and empty:
    the unfiltered board's URL is the bare page URL. "Entirely whitespace" is the
    trim rule's whitespace and no other (see **The trim rule** above), so the two
    paths agree on which terms are no term at all: a term of `U+FEFF` alone is not
    one of them, and `q` keeps it.
  - **Cold load arrives already narrowed.** When the page is requested with a `q`
    value, the **server** applies the term, and the document it sends already
    carries the narrowing in its final state: the narrowed column counts, the
    in-column empty states, and the no-match message where nothing matches. Nothing
    on the client applies the term after load. A document that arrived unnarrowed
    and was narrowed by a script afterwards is forbidden: it would flash the
    unfiltered board before narrowing it, and where scripting is unavailable it
    would leave the URL carrying a term while the board ignored it.

    The non-matching cards **may** be present in that document, provided they
    arrive already marked as not visible and count towards nothing the board
    states. Their presence is not a concession but the enabling condition for
    **Live typing** above: clearing or widening the search restores cards, and a
    card the document never carried could not be restored without going back to the
    server, which would make the narrowing instantaneous in one direction only. The
    rule forbids narrowing applied *after* load; it does not forbid cards *present*
    in the document.
- **The URL carries the filters.** Each filter travels in its own URL query
  parameter on `/roadmaps/{name}/tasks` — **`type`**, **`priority`**, and
  **`severity`** — named after the `rmp task list` flags that carry the same three
  dimensions (see `COMMANDS.md § List Tasks`). Each obeys the rules that **The URL
  carries the term** states for `q`, for the same reasons:
  - **Changing a filter updates the URL in place.** Selecting a value replaces the
    current history entry rather than pushing a new one, so the browser Back button
    leaves the board rather than stepping backwards through the control row.
    Narrowing the board is one kind of act and does not become a different kind of
    act because the user performed it with a dropdown instead of a keyboard.
  - **A dimension with no filter leaves no parameter.** While a dropdown sits on its
    no-filter option, that dimension's parameter is **removed** from the URL rather
    than left present and empty. Clearing every control — the search input and all
    three dropdowns — therefore restores the full board with its true counts, and
    leaves the bare page URL, with no parameter of any kind behind it.
  - **Cold load arrives already narrowed.** When the page is requested with any
    combination of `q`, `type`, `priority`, and `severity`, the **server** applies
    every one of them, and the document it sends already carries the narrowing in
    its final state: the narrowed column counts, the in-column empty states, the
    no-match message where nothing matches, and each control already showing the
    value that produced the board. Nothing on the client applies a filter after
    load. Non-matching cards **may** be present in that document under exactly the
    condition that **The URL carries the term** sets — they arrive already marked
    as not visible and count towards nothing the board states — which is what lets
    widening or clearing a filter restore them without a request to the server.

  The four parameters are independent of each other and of their position in the
  query string: the board depends on which values are present, never on the order
  in which the query string carries them.
- **Server and client produce the same board (the property that matters).** For any
  roadmap and any combination of a term and the three filters, the board reached by
  setting those controls on the page and the board reached by requesting the page
  URL carrying the same values in `q`, `type`, `priority`, and `severity` are the
  **same**: the same cards, in the same columns, in the same order, with the same
  column counts, and the same empty states. The two paths implement one matching
  rule per criterion and one conjunction over them, and MUST NOT diverge — that
  equivalence is what makes a narrowed board shareable and reloadable, and it is the
  property to test. For the term, one rule per criterion means the trim rule and the
  folding rule above, each with a single implementation of it, shipped from the
  server to the client (see **The trim rule**, **The folding rule**, and **One rule,
  and only one implementation of it**); for a filter it means one comparison per
  dimension (see **What each filter matches**).
- **No malformed term is an error.** Every string is a valid term. A term that
  matches nothing renders the empty board described above, with HTTP 200. A term
  longer than any searchable text simply matches nothing. A `q` the server cannot
  decode is treated as absent, and the unfiltered board is served. The search never
  produces an error page and never changes the route's status codes, and neither
  does any filter value (see **No filter value is an error** above).
- **How the criteria compose.** The search term is **one** criterion, and each
  active filter is one more. The shown set is the set of tasks satisfying **every**
  active criterion, and a board with no active criterion shows every task. The
  conjunction is total and holds in every direction: `?q=cache&type=BUG&priority=7`
  shows the `BUG` tasks of priority `7` or above whose title or `#<id>` reference
  contains `cache`, and no other task. Narrowing a criterion can only shrink the
  shown set, never grow it, and no criterion ever re-admits a task another criterion
  excluded.

  The criteria are independent: each dimension decides only its own question, none
  of them changes how another is compared, and each carries its own URL query
  parameter under the same rules as `q` — absent when inactive, applied by the
  server on a cold load, equivalent between the two paths. A further criterion added
  later composes the same way and requires no change to this contract.
- **Escaping the term.** The term is the one caller-supplied string this page
  echoes back — a filter value never is (see **Header filter controls** above) — and
  it is treated exactly as every other caller-supplied value:
  - Where the **server** renders it — into the search input's value, and into the
    no-match message — it is escaped by `html/template`'s contextual auto-escaping
    (see [Frontend Rules](#frontend-rules), rule 1).
  - Where the **script** renders it, it is written through `textContent` or an
    equivalent that cannot interpret markup, never `innerHTML` and never
    `insertAdjacentHTML`, by the same rule the task detail modal follows (see
    [Task Detail Modal](#task-detail-modal), *Client-side rendering is text-only*,
    and [Security and Constraints](#security-and-constraints), rule 7).

  A term containing HTML markup therefore renders as visible characters on both
  paths and can introduce no element, attribute, or script into the page.
- **Implementation constraints already in force.** The narrowing script — the one
  script that applies the term and the three filters alike — is embedded and served
  from `/static/...` like every other client script (see
  [Embedded Asset Categories](#embedded-asset-categories) and
  [Frontend Rules](#frontend-rules), rules 2 and 5). No inline script is
  introduced and the Content-Security-Policy in [Security Headers](#security-headers)
  is unchanged. Every class the controls emit resolves in the embedded stylesheets
  and no template carries a `style` attribute (see [UI Framework](#ui-framework),
  rules 8 and 10). The filter dropdowns introduce no component the vendored Tabler
  distribution does not already ship: the select control is the one the
  knowledge-graph page's layout dropdown already uses (see
  [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)).
- **Read-only.** The page renders data only. The board offers **no
  drag-and-drop** and no control of any other kind that moves a task between
  columns, reorders cards, changes a task's status, or creates or edits a task or
  a column. This is a deliberate and explicit divergence from the GitLab issue
  board the layout is modelled on: the inspiration is structural — columns per
  state, cards, per-column counts — and never interactive. The page contains no
  form, button, or link that submits a change, and the `rmp` CLI remains the sole
  write path for every task (see
  [Security and Constraints](#security-and-constraints)).

  Read-only constrains what the page may **change**, not what it may **show**. A
  control that only alters which of the already-read tasks the user is looking at —
  the header search of **Header search control** and the three dropdowns of
  **Header filter controls** above — changes no task, writes nothing, and is
  therefore not an exception to this rule. The distinction is
  between altering the data and altering the view of it: the first is forbidden
  here, the second is not.
- **Empty states.** A column that holds no task renders its own clear, unobtrusive
  empty state inside the column, below the column header, in place of the card
  list; the column, its title, and its `0` count badge stay visible. A roadmap
  with no task at all renders the board with all five columns present and each
  one showing that in-column empty state. The page does **not** replace the board
  with a page-level empty state, and it never drops or hides a column: the five
  columns are fixed (see **Columns** above), and an empty roadmap is shown as an
  empty board, not as an absent one.

  A roadmap that holds no task and a narrowing that matches no task are different
  conditions and read differently. The first is the state of the roadmap and shows
  the five in-column empty states alone. The second is the result of the controls
  the user set — a term, a filter, or any combination of them — so the board
  additionally says that no task matches those controls (see **Effect on the board**
  above). In both cases the five columns stay.
- **Layout and scrolling.** The five columns are presented side by side. When
  they do not fit the viewport, the **board** scrolls horizontally inside its own
  container; the page itself never scrolls horizontally, so `<body>` produces no
  horizontal overflow at any viewport width.

  The board is a **full-height page region**: its height is the space the page body
  leaves once the top navbar and the page header are placed above it, it ends where
  the page body ends, and that edge lies within the viewport, exactly as
  [Full-Height Page Regions](#full-height-page-regions) requires. The board gives up
  no space for a page footer, because the shell renders none (see
  [UI Framework](#ui-framework), rule 12). That height is the **available height**
  each column is measured against: a column scrolls vertically and independently of
  the others when its card list exceeds it, as a GitLab issue board's lists do.

  The board's own horizontal scrollbar is drawn in space reserved for it **beneath**
  the columns rather than over them, so the last card of a column stays fully
  visible while the board can still be scrolled sideways.

  On narrow viewports the board stays usable: each column keeps a minimum width at
  which its cards remain legible, the user reaches the remaining columns by
  scrolling the board horizontally (a touch-friendly gesture on a touch device),
  the cards present touch-friendly hit targets, and the task detail modal a card
  opens stays usable on the same viewport (see
  [Responsive and Mobile-First Design](#responsive-and-mobile-first-design), rule
  9, and [Task Detail Modal](#task-detail-modal)). On a viewport too short to
  present a usable board, the board takes the minimum height of
  [Full-Height Page Regions](#full-height-page-regions), rule 5.
- **Column width and card density.** How much of a task's own text the board can
  place on one line is decided by two lengths — how wide a column is, and how much
  padding the card inside it spends on its own margins — so both are fixed here
  rather than left to whatever a framework default happens to be.

  Each of the five columns is **19rem** wide and never narrower than **17rem**. All
  five carry the same width: a column stands for a state, not for a volume of work,
  so a column holding many tasks is no wider than one holding none and the board's
  shape does not change with the data. A column does not stretch to fill a viewport
  wider than the board needs either; the space beyond the five columns is left empty,
  which keeps the measure of a card's title the same at every viewport width.

  These widths are this board's own. The sprint page's member-tasks board departs
  from them deliberately: its three columns divide the width of that board equally
  and grow with the viewport, for the reason stated where that board is defined (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Height and
  scrolling**). The `17rem` minimum, the `0.75rem` gap between columns, and the
  `0.75rem` card body padding below are carried by both boards; the `19rem` column
  width is carried by this one alone.

  Inside a column, the card the user reads and activates carries **0.75rem** of
  padding on all four sides of its body, in place of the `1rem` the vendored Tabler
  distribution gives a small card's body. The card's body holds running text — the
  reference line, the title, the two badges, and the metadata footer — inside a
  measure the column has already narrowed, so padding taken off the body is width
  returned to that text and height returned to the card. The hit target is
  unaffected, because what the user presses is the whole card and not the text
  inside it (see **Card content** and **Clickable card** above). The rule is an
  override of a vendored component's own spacing, declared in the project override
  stylesheet, which is where such an override belongs (see
  [UI Framework](#ui-framework), rule 10); it changes no class the board emits and
  no markup.

  Both lengths are expressed in `rem`, so they scale with the reader's own text
  size: enlarging the browser's font enlarges the column and the card's padding with
  it, and the relation between the text and the space around it is preserved.
- **Markup.** The board obeys the markup rules already in force and introduces no
  exception to them. Templates carry no inline `style` attribute (see
  [Frontend Rules](#frontend-rules) and [UI Framework](#ui-framework), rule 10).
  Every class the board emits is defined in the embedded stylesheets — either in
  the vendored Tabler distribution or in the project override stylesheet
  `static/style.css` — and no class targets a framework component the vendored
  distribution does not ship (see [UI Framework](#ui-framework), rules 8 and 10).
  Where Tabler provides the component that does the work, the board uses Tabler's
  markup: the cards are Tabler cards, the column headers use Tabler's card-header
  idiom, the counts and the priority and severity badges are Tabler badges, and
  the in-column empty state uses Tabler's empty-state markup. The vendored Tabler
  distribution ships no board or Kanban component, so the column strip's own
  layout and scrolling rules live in `static/style.css`, which is the specified
  home for project styling no Tabler class covers (see
  [UI Framework](#ui-framework), rule 10). The page keeps the admin shell and the
  page header that every other page uses, unchanged and still governed by
  [UI Framework](#ui-framework), rules 11 to 18.
- **Relationships shown.** The page surfaces, in a read-only view, the
  relationships already modelled in the data, and each one is surfaced in a
  specific place:
  - **Task-to-sprint membership** is shown on the card, as the sprint indicator of
    the metadata footer: the card names the one sprint the task belongs to, and
    shows nothing when the task belongs to none.
  - **Task parent/subtask hierarchy** and **task dependency edges** are shown on
    the card as counts — the subtask count and the `depends_on` and `blocks`
    counts — and in full in the task detail modal, which lists the parent task and
    the dependency ids themselves (see
    [Task Detail Modal](#task-detail-modal)).

  The presentation MUST reflect the same relationships defined in
  `DATABASE.md § Relationships`; it introduces no new relationship.
- **Read cost.** Rendering the page performs **three** reads and no more:
  1. the roadmap's full task list, unbounded (see **Unbounded read** above);
  2. **one** grouped query returning the comment **count** of every task the page
     renders (see `DATABASE.md § Count Comments for Many Parents (Grouped)`);
  3. **one** grouped query that resolves the sprint of every task the page
     renders, over the whole set of rendered task ids at once (see
     `DATABASE.md § Resolve the Sprint of Many Tasks (Grouped)`).

  The page reads comment **counts**, not comment bodies. The card displays a
  number, so reading the text of every comment of every task in order to display it
  would be work the page throws away; a task's comment text is read only when a
  user opens that task's modal, one task at a time, by the task detail endpoint
  (see [Task Detail Endpoint](#task-detail-endpoint)). No page reads the comment
  text of many tasks at once.

  When the roadmap has no task, the page issues the task-list read only: neither
  the count query nor the sprint query is issued, because both take a set of
  rendered task ids and that set is empty.

  Grouping the tasks into the five columns, counting each column, and matching each
  card to its sprint are done in memory over the results already read. The board
  adds no further query — none per column and none per card — and never issues one
  query per task. The number of queries the page issues does not grow with the
  number of tasks, the number of sprints, or the number of columns. Opening a modal
  adds one request for that one task, made only on demand.

  A search term and the three filters change none of this. Applying them on a cold
  load selects from the task list the page already reads and issues no additional
  query: a filter adds no clause to that read, no second read, and no per-dimension
  query, because it is applied in memory over the rows already in hand exactly as
  the term is. Narrowing in the browser issues no request at all, because every card
  is already in the document.
- **Path parameters.** `{name}` is validated against the roadmap-name rules
  exactly as on the other roadmap routes (the path-traversal guard in
  [Routes and Pages](#routes-and-pages) and
  [Security and Constraints](#security-and-constraints)); an invalid or
  nonexistent `{name}` returns HTTP `404 Not Found`.

### Roadmap Sprint Page

- **Route:** `GET /roadmaps/{name}/sprints/{id}`
- **Content:** A read-only presentation of a single sprint of the named roadmap,
  read from that roadmap's `project.db`. The page renders the sprint through the
  sprint detail sub-template (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template)), which produces the
  full sprint detail block. This full detail block is shown only on this page; the
  Roadmap Sprints Page renders every sprint, including the OPEN sprint, as a
  compact card through the shared sprint-card partial instead (see
  [Shared Sprint-Card Partial](#shared-sprint-card-partial)).
- **Page header.** The page header presents the sprint `title` (the required
  title defined for the `Sprint` model in `MODELS.md § Sprint`) alongside the text
  `Sprint #<ID>` (the sprint's `id`), so the sprint is identifiable by both its
  title and its id. It is rendered by the shared partial, which places
  `Sprint #<ID>` in the pretitle and the `title` with its status badge in the
  title (see [Shared Page-Header Partial](#shared-page-header-partial)); the
  roadmap name is not repeated there. The actions column carries a link back to
  the roadmap's sprints page. The page does not redefine these fields;
  `MODELS.md` remains canonical.
- **Sprint status summary line.** At the top of the sprint presentation the page
  shows the sprint status summary line defined in
  [Sprint Detail Sub-Template](#sprint-detail-sub-template).
- **Sprint details.** The page shows all details of the sprint, using the fields
  defined for the `Sprint` model in `MODELS.md § Sprint`: the sprint `id`, its
  status, its `title`, its description, its execution `order` (a positive integer,
  unique across the roadmap), its capacity (`max_tasks`, which may be unset meaning
  unlimited capacity), `created_at`, `started_at`, `closed_at`, and `task_count`.
  The page presents the sprint status clearly (the status enum and lifecycle are
  defined in `MODELS.md § Enums` and `STATE_MACHINE.md § Sprint State Machine`).
  The sprint `description` is multi-line as authored through the CLI, and the page
  renders it preserving the author's line breaks (newlines); the text still wraps,
  so no forced horizontal scrolling is introduced (see
  [Frontend Rules](#frontend-rules), rule 6). The page does not redefine these
  fields; `MODELS.md` and `DATABASE.md` remain canonical.
- **Member-tasks board.** The page presents the sprint's tasks as a Kanban board
  of three fixed columns — `WAITING`, `DOING`, and `CLOSED` — holding one card per
  task, placed between the sprint details above it and the sprint's comments below
  it. The columns group the tasks by the same
  categorisation the sprint status summary line uses, so each column's count is one
  of that line's own numbers, and each column orders its own cards: the `WAITING`
  column keeps the planned in-sprint execution order, which is the `sprint_tasks`
  order (the ordered set of task IDs the `Sprint` model exposes as `tasks`; see
  `MODELS.md § Sprint` and `DATABASE.md § Relationships`), while the `DOING` and
  `CLOSED` columns lead with the most recent — `started_at` descending and
  `closed_at` descending — because those two columns record what has happened
  rather than what is planned, with the planned order breaking their ties (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Order within a
  column**). Each card is clickable: selecting a card opens
  the read-only task detail modal for that task. The card **is** the element that
  opens the modal and it is a `<button>`, so the modal opens from the pointer, from
  touch, and from the keyboard alike. The board carries no control that moves a
  task between columns (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template) and
  [Task Detail Modal](#task-detail-modal)).
- **Sprint comments.** After the member-tasks board, the page shows the sprint's
  own comments in a Comments card, oldest first (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template)). The card shows the
  comments of the sprint itself, not those of its member tasks; a task's comments
  are shown in that task's detail modal.
- **Path parameters.** `{name}` is validated against the roadmap-name rules
  exactly as on the other roadmap routes (the path-traversal guard in
  [Routes and Pages](#routes-and-pages) and
  [Security and Constraints](#security-and-constraints)); an invalid or
  nonexistent `{name}` returns HTTP `404 Not Found`. `{id}` MUST be a valid
  integer; a non-integer `{id}`, or an integer `{id}` that is not the `id` of a
  sprint belonging to the named roadmap, returns HTTP `404 Not Found` (see the
  HTTP status mapping in [Routes and Pages](#routes-and-pages)).
- **Read-only.** The page renders data only. It contains no form, button, or
  link that submits a change; there is no edit affordance of any kind.

### Roadmap Audit Log Page

- **Route:** `GET /roadmaps/{name}/audit`
- **Content:** A read-only presentation of the named roadmap's **full audit
  log** — every audit entry of any operation and any entity type — read from that
  roadmap's `project.db` (the `audit` table). The page renders the entries as a
  server-rendered HTML table. It is read-only: it shows no clickable row action, no
  modal, and no edit affordance of any kind.
- **Columns.** The table shows **every** `AuditEntry` field defined in
  `MODELS.md § Audit Entry` and `DATABASE.md § audit Table`, in this order: the entry
  `ID`, the `Operation`, the `Entity Type`, the `Entity ID`, the `Related Entity ID`,
  the `Commit`, and the `Performed At` timestamp (the ISO 8601 UTC timestamp). The
  page does not redefine these fields; `MODELS.md` and `DATABASE.md` remain
  canonical.
- **The two nullable columns are always rendered.** `Related Entity ID` and `Commit`
  are `null` on the operations that do not carry them, and the page renders a
  neutral placeholder in that cell — an em dash — rather than an empty cell, so a
  reader can tell an absent value from a rendering fault. Neither column is hidden,
  collapsed, or dropped when every entry on the visible page happens to be `null`:
  the column set is fixed and does not depend on the data.
- **Why both columns are shown.** Without `Related Entity ID`, two entries of the
  same operation against the same entity are indistinguishable on the page: every
  `SPRINT_ADD_TASK` row of a sprint reads identically and none of them says which
  task was added, and every `TASK_STATUS_SPRINT` row of a task says it joined a
  sprint without saying which one. Without `Commit`, the page cannot show the commit
  that bracketed a task's work, which is the reason the column exists. A presentation
  that omits either column fails to present the audit log.
- **`Related Entity ID` renders per entry, never inferred from the operation.** The
  column holds the counterpart entity of the operation that produced the entry, and
  is `null` when that operation has no counterpart (see
  `DATABASE.md § The Two Entities of a Relational Operation`). Whether a value is
  present does not follow from the operation name: a `TASK_STATUS_BACKLOG` entry
  written by `sprint remove-tasks` names the sprint the task left, while one written
  by `task stat` carries `null`. The page therefore renders the value each entry
  actually carries and MUST NOT derive, suppress, or substitute it based on the
  operation shown beside it.
- **`Commit` renders the stored value verbatim.** The value is 7 to 64 lowercase
  hexadecimal characters. The page does not abbreviate it, does not expand it, does
  not link it to any repository, and does not verify that it names a commit that
  exists: Groadmap contacts no repository (see `MODELS.md § Task`, Commit Hash
  Constraint). Rendering it in a monospaced face is permitted; altering the text is
  not.
- **`Operation` renders whatever value the entry carries.** The value is an opaque
  string: a stored entry can carry an operation the catalogue does not list, and the
  page MUST render it as received rather than failing, dropping the row, or
  substituting a fallback (see `DATA_FORMATS.md § Audit Entry`). This includes the
  catalogue's LEGACY operations, which appear on entries written before the
  catalogue was refined.
- **Wide-table behaviour.** The seven columns MUST NOT force the page body to scroll
  horizontally on a narrow viewport. The table scrolls inside its own container,
  consistent with the responsive rules in
  [Responsive and Mobile-First Design](#responsive-and-mobile-first-design).
- **Ordering.** The entries are ordered by the audit entry's `performed_at`
  timestamp **descending**, so the most recently performed operation appears first.
  `performed_at` is the audit entry's completion timestamp. This is the same
  ordering the existing audit data access uses (`ORDER BY performed_at DESC`; see
  `DATABASE.md § Audit Queries`); the page introduces no new ordering.
- **Pagination.** The table is paginated at a **fixed page size of 100 entries per
  page**. The page is selected by a `page` query parameter that is 1-based and
  defaults to `1` when absent. The total page count is `ceil(total_entries / 100)`,
  and there is **always at least 1 page**, even when the audit log holds zero
  entries.
- **Pagination is clamped, not strict.** The `page` parameter is **clamped** to the
  nearest valid page rather than producing an error: a `page` value below 1, a
  non-integer or otherwise unparseable `page` value, and a `page` value beyond the
  last page are each clamped to the nearest valid page (`1` or the last page). A
  clamped request renders successfully with HTTP 200; the audit page never returns
  HTTP 404 for an out-of-range or garbage `page` value. The `{name}` part is still
  validated exactly as on the other roadmap routes (an invalid or nonexistent
  `{name}` returns HTTP 404; see below).
- **Empty state.** When the roadmap's audit log is empty, the page renders
  successfully (HTTP 200) with a clear empty-state message and shows **page 1 of 1**.
  An empty audit log is not an error.
- **Pagination controls.** The audit card's footer shows a read-only **numbered
  pagination bar** in the Tabler style (the first option at
  `https://preview.tabler.io/pagination.html`), rendered in the shape
  `‹ 1 … 4 5 6 … 20 ›`. Each visible page number is a `GET` link to that page
  (`?page=N`), except the current page, which is rendered as the **active**
  (non-link, visually highlighted) item. A **Previous** chevron (`‹`) and a
  **Next** chevron (`›`) frame the numbers. The **Previous** chevron is disabled or
  absent on the first page, and the **Next** chevron is disabled or absent on the
  last page. All controls are `GET` links that change only the `page` query
  parameter: there is no form and no write path, fully consistent with the
  read-only nature of the interface.
- **Sliding window with ellipsis.** The numbered bar uses a sliding window of page
  numbers centred on the current page, and always anchors **page 1** and **page
  `TotalPages`** at the two extremities. The rules are deterministic so that
  implementation and tests agree exactly:
  1. The bar always shows page `1` and page `TotalPages`.
  2. The bar always shows a contiguous window around the current page: every page
     in the range `[current - 2, current + 2]`, clamped to `[1, TotalPages]`
     (the current page and up to two neighbours on each side).
  3. The gap between the first anchor (`1`) and the window, and the gap between the
     window and the last anchor (`TotalPages`), are each collapsed to a single
     **ellipsis** (`…`) item. The ellipsis is a non-interactive item: it is not a
     link.
  4. When such a gap is exactly one page wide, that single page number is rendered
     directly instead of an ellipsis; an ellipsis never stands in for a single
     hidden page.
  5. When the total page count is small enough that the anchors and the window
     already cover every page, every page number is shown and no ellipsis appears.
- **"Page X of Y" indicator.** The audit card's footer keeps the textual
  "Page X of Y"
  indicator alongside the numbered pagination bar. It is a read-only, accessible
  affordance that states the current page and the total page count in words; it
  reflects the same `page` value and `TotalPages` total as the numbered bar.
- **Pagination markup.** The pagination bar uses accessible Tabler pagination
  markup: a `ul.pagination` list whose items are `li.page-item` elements, with each
  link rendered as `a.page-link`. The current page item carries the `active` state,
  and a disabled **Previous** or **Next** chevron and the ellipsis item carry the
  `disabled` state. `aria` attributes mark the disabled chevrons and the
  active/current page so the bar is fully accessible, and the whole bar sits inside
  a `<nav>` element carrying a descriptive `aria-label`, the wrapper Tabler emits
  around its pagination component (see [UI Framework](#ui-framework),
  rule 15). The markup contains only `GET` links and inert items: no form, no
  button, and no write path.
- **Defense in depth: within the audit hard cap.** The data layer clamps an
  unbounded or oversized audit limit to `MaxAuditLimit` (value **500**; see
  `DATABASE.md § Audit Result Limit`). A fixed 100-entries-per-page request is
  always within that cap, so the page-size request never exceeds the hard cap.
- **Path parameters.** `{name}` is validated against the roadmap-name rules exactly
  as on the other roadmap routes (the path-traversal guard in
  [Routes and Pages](#routes-and-pages) and
  [Security and Constraints](#security-and-constraints)); an invalid or nonexistent
  `{name}` returns HTTP `404 Not Found`.
- **Read-only.** The page renders data only. It contains no form, button, or link
  that submits a change; there is no edit affordance of any kind. Reading the audit
  log writes no row and produces no new audit entry, because a read is not a change
  (see [Tasks and Sprints from SQLite](#tasks-and-sprints-from-sqlite) and
  `DATABASE.md § audit Table`).

### Shared Page-Header Partial

Every page's header title column is rendered by **one** partial, so the six pages
cannot drift into six conventions for saying the same kind of thing. The partial
renders the `<div class="col">` of the Tabler page-header row: an optional
pretitle, the title, an optional status badge inside the title, and an optional
lead line. A page MUST NOT hand-write a `page-pretitle` or a `page-title` element.

1. **The title names the view, not the roadmap.** The roadmap is named twice in
   the shell already — in the sidebar's per-roadmap section label and in the top
   navbar (see [UI Framework](#ui-framework), rule 19) — so repeating it in the
   page title would state the same fact a third time on one screen while leaving
   the view the user is actually looking at unnamed on the sprints page. The
   titles are exactly:

   | Page | Pretitle | Title |
   |---|---|---|
   | Roadmap Index | — | `Roadmaps` |
   | Roadmap Sprints | — | `Sprints` |
   | Roadmap Tasks | — | `Tasks` |
   | Roadmap Audit Log | — | `Audit` |
   | Roadmap Knowledge-Graph | — | `Knowledge graph` |
   | Roadmap Sprint | `Sprint #<ID>` | the sprint's `title`, with its status badge |

2. **The sprint page is the one hierarchical header.** It is the only page that
   presents an individual record rather than a view of the roadmap, so it alone
   carries a pretitle, and that pretitle is `Sprint #<ID>` — the roadmap name is
   not repeated in it. The sprint's `title` stays the page title and keeps the
   status badge specified in
   [Roadmap Sprint Page](#roadmap-sprint-page), so the sprint remains identifiable
   by both its title and its id.

3. **The lead line belongs to the roadmap index alone.** The index page's title is
   followed by a lead line naming the directory the roadmaps are discovered under.
   No other page carries one.

4. **The actions column stays with the page.** The partial covers the title column
   only. What a page puts in its actions column is genuinely page-specific markup —
   a search input, a `<select>`, a link — and folding those into the shared partial
   would require it to know every page that uses it. Each page therefore renders
   its own actions column, in the Tabler idiom fixed in
   [UI Framework](#ui-framework), rule 16, and the `page-header`, `container-xl`
   and `row g-2 align-items-center` wrapper likewise stays in the page.

5. **The actions column carries controls, not duplicated navigation.** A control
   that acts on the page belongs there; a link to a destination the admin-shell
   sidebar already lists on every page does not, because it is a second route to
   somewhere the page already offers and removing it costs no access. Concretely:

   | Page | Actions column |
   |---|---|
   | Roadmap Index | none |
   | Roadmap Sprints | none |
   | Roadmap Tasks | the search input and the type, priority, and severity filter dropdowns (see [Roadmap Tasks Page](#roadmap-tasks-page), **Header search control** and **Header filter controls**) |
   | Roadmap Audit Log | none |
   | Roadmap Knowledge-Graph | the layout dropdown (see [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)) |
   | Roadmap Sprint | a link back to the roadmap's sprints page |

   The sprint page's back link is **not** duplicated navigation: it returns to the
   parent record of the one being shown, which is a relationship the sidebar's flat
   view list does not express.

6. **Values are escaped.** The sprint `title` and any other data-derived value
   reaching the partial is rendered through `html/template` as text, exactly as it
   was before the partial existed.

### Shared Sprint-Card Partial

A single shared sub-template (a template "partial") renders the sprint card. All
three tabs of the Roadmap Sprints Page — Próximos, Actual, and Concluídos —
render every sprint through this same partial, so all sprints share identical
card markup across the three tabs. The card is the only sprint presentation on
the Roadmap Sprints Page; the OPEN sprint under Actual uses the same card as every
other sprint and is not expanded inline.

1. **Single source of card markup.** There is one shared partial for the sprint
   card, and every tab renders each of its sprints through it. No tab defines its
   own divergent card layout; the OPEN sprint under Actual is rendered with the
   same card as a PENDING sprint under Próximos and a CLOSED sprint under
   Concluídos.

2. **What the card renders.** For one sprint, the card renders, in order:
   - a **header** showing the sprint `title` (the sprint's required `title`; see
     `MODELS.md § Sprint`) together with (or directly under) the text
     `Sprint #<ID>` (the sprint's `id`) and a **status badge** for the sprint's
     status (the status enum is defined in `MODELS.md § Enums`), coloured by the
     semantic mapping in
     [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
     so the sprint is identifiable at a glance in the Próximos, Actual, and
     Concluídos listings;
   - the sprint **description** text;
   - a **footer** showing the sprint's total task count (the sprint's
     `task_count`; see `MODELS.md § Sprint`).

3. **Clickable link.** The whole card is a clickable link to that sprint's own
   page at `/roadmaps/{name}/sprints/{id}` (see
   [Roadmap Sprint Page](#roadmap-sprint-page)). The card shows no member tasks and
   opens no task detail modal.

4. **Read-only.** The card renders data only. It contains no form, button, or link
   that submits a change; its only interaction is navigating to the sprint's own
   page.

5. **Authored line breaks.** Where the card renders the sprint's `description`, it
   preserves the author's line breaks as specified in
   [Frontend Rules](#frontend-rules), rule 6.

### Sprint Detail Sub-Template

A sub-template (a template "partial") renders the full sprint detail block. The
single Roadmap Sprint Page renders a sprint through this sub-template. The full
detail block appears only on the Roadmap Sprint Page; the Roadmap Sprints Page
shows sprints as compact cards through the shared sprint-card partial instead (see
[Shared Sprint-Card Partial](#shared-sprint-card-partial)).

1. **Single source of detail presentation.** There is one sub-template for the
   full sprint detail block, and the Roadmap Sprint Page renders the requested
   sprint through it.

2. **What the sub-template renders.** For one sprint, the sub-template renders, in
   order:
   - the **sprint status summary line** (defined below);
   - the **sprint metadata datagrid** with the sprint's `ID`, `Title` (the
     sprint's required `title`), `Status`, `Order` (the sprint's execution
     `order`, a positive integer unique across the roadmap), `Capacity` (the
     `max_tasks` value, shown as "Unlimited" when unset), `Tasks` (the sprint's
     `task_count`), `Created` (`created_at`), `Started` (`started_at`), and
     `Closed` (`closed_at`); the fields are defined for the `Sprint` model in
     `MODELS.md § Sprint` and are not redefined here;
   - the **member-tasks board**, a Kanban board of three fixed columns holding the
     sprint's tasks, one card per task (defined below). The board is the sprint's
     member-task presentation and sits between the two cards that surround it:
     directly below the sprint metadata datagrid and directly above the Comments
     card;
   - the **Comments card**, a separate card placed after the member-tasks board and
     rendered last in the sub-template (defined below).

3. **Sprint status summary line.** At the top of the sub-template the sub-template
   renders one indicative, complementary line that summarises the sprint's task
   completion. Its exact format is:

   `<pct>% - P:<p> A:<a> C:<c> - T:<t>`

   for example `33% - P:8 A:29 C:18 - T:55`. The components are:
   - `<pct>` is the sprint **completion percentage**: the number of `COMPLETED`
     tasks divided by the total number of tasks in the sprint, expressed as a
     percentage and **rounded to the nearest integer percent**. When the sprint
     has no tasks, the completion percentage is `0%`.
   - `P` (`<p>`) is the **pending** count: the number of the sprint's tasks in the
     `BACKLOG` or `SPRINT` status.
   - `A` (`<a>`) is the **open/in-progress** count ("Abertas"): the number of the
     sprint's tasks in the `DOING` or `TESTING` status.
   - `C` (`<c>`) is the **completed** count: the number of the sprint's tasks in
     the `COMPLETED` status.
   - `T` (`<t>`) is the **total** number of tasks in the sprint.

   All five values refer only to the sprint's own member tasks; no task outside
   the sprint is counted. The status-to-category mapping (pending = `BACKLOG` +
   `SPRINT`, open/in-progress = `DOING` + `TESTING`, completed = `COMPLETED`; the
   task status enum is defined in `MODELS.md § Enums`) is exactly the
   categorisation `models.CalculateSprintShowResult` already produces (its
   `Summary.Pending`, `Summary.InProgress`, and `Summary.Completed` counters and
   its `Summary.TotalTasks`); the summary line reuses that categorisation rather
   than defining a new one.

4. **Member-tasks board.** The sprint's member tasks are presented as a Kanban
   board of three fixed columns, one card per task. The **GitLab issue board** is
   the acknowledged model for this presentation: columns that stand for states of
   the work, cards that stand for work items, a count on each column header, and
   counters at the trailing edge of a card. As on the tasks page, the model is
   structural and never interactive (see **Read-only** below).

   - **Three fixed columns, in this order.** From left to right the columns are
     `WAITING`, `DOING`, and `CLOSED`, and each holds the sprint's tasks in the
     statuses named here (the task status enum is defined in `MODELS.md § Enums`):

     | Column | Holds the sprint's tasks whose status is |
     |---|---|
     | `WAITING` | `BACKLOG` or `SPRINT` |
     | `DOING` | `DOING` or `TESTING` |
     | `CLOSED` | `COMPLETED` |

     The grouping is deliberately the **same categorisation the sprint status
     summary line already uses** — pending = `BACKLOG` + `SPRINT`, open/in-progress
     = `DOING` + `TESTING`, completed = `COMPLETED` — which is the categorisation
     `models.CalculateSprintShowResult` produces in its `Summary.Pending`,
     `Summary.InProgress`, and `Summary.Completed` counters (see rule 3 above). The
     board defines no new categorisation; it reuses that one, so the two
     presentations of one sprint cannot disagree about which tasks are waiting,
     which are being worked on, and which are done.

     Each column heading is written exactly as spelled above, in upper case, and is
     not translated.
   - **The column counts are the summary line's own numbers.** Because the grouping
     is that one, each column's count **equals** the corresponding value of the
     summary line rendered at the top of the same page: the `WAITING` column's count
     is `P`, the `DOING` column's count is `A`, the `CLOSED` column's count is `C`,
     and the three counts sum to `T`. That identity is what makes a fourth or
     "other" column unnecessary rather than merely unwanted: the task status enum is
     closed (`MODELS.md § Enums`) and `tasks.status` is restricted by a CHECK
     constraint to exactly its five values (`DATABASE.md § tasks Table`), so every
     member task carries one of those five, each of the five is claimed by exactly
     one column, and no task of the sprint can fall outside the board.
   - **Every column is always rendered.** All three columns are present, in that
     order, whatever the sprint holds; a column is never dropped or hidden, and
     neither the set of columns nor their order depends on the data. A column
     holding no task renders the in-column empty state in the idiom the tasks board
     already uses — a clear, unobtrusive empty state inside the column, below the
     column header, in place of the card list, with the column, its heading, and its
     `0` count badge still visible (see
     [Roadmap Tasks Page](#roadmap-tasks-page), **Empty states**). A sprint with no
     member task is therefore shown as an empty board rather than as an absent one,
     and the sub-template puts no page-level empty state in place of the board.
   - **Column header.** Each column header shows the column heading together with a
     Tabler badge carrying that column's task count, exactly as the tasks board's
     column header does (see [Roadmap Tasks Page](#roadmap-tasks-page), **Count per
     column**). The badge is the same hybrid it is there: its **text** is the number
     of member tasks in the column, and its **colour** is the semantic colour of the
     status the column groups, taken from the task status table (see
     [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
     rule 2). No new colour and no new band is introduced here.

     A column of this board groups a **set** of statuses rather than a single one:
     `WAITING` groups `BACKLOG` and `SPRINT`, `DOING` groups `DOING` and `TESTING`,
     and `CLOSED` holds `COMPLETED` alone (see the table above). The colour is
     therefore the one the mapping assigns to the **canonical status of the group** —
     the status a task is normally in at that stage of the sprint. `WAITING` takes
     the colour of `SPRINT`, `DOING` takes the colour of `DOING`, and `CLOSED` takes
     the colour of `COMPLETED`. A task waiting in a sprint is normally a `SPRINT`
     task: a `BACKLOG` task inside a sprint is the exceptional case, the case of a
     task returned to the backlog without leaving the sprint, so `SPRINT` is the
     status the `WAITING` column stands for. The column named `DOING` taking the
     colour of the status named `DOING` is the reading a user will expect, and any
     other choice would leave the board's own heading disagreeing with its colour.
     `CLOSED` calls for no such choice, because it holds one status and that status
     is its canonical one.
   - **Order within a column.** Each column orders its cards by the question that
     column answers, so the three columns do not share one order:

     | Column | Order | What the reader gets from it |
     |---|---|---|
     | `WAITING` | `sprint_tasks` `position` ascending | The next task to develop at the top, the last one at the bottom |
     | `DOING` | `started_at` descending | The task that entered `DOING` most recently at the top, the one that has been there longest at the bottom |
     | `CLOSED` | `closed_at` descending | The task closed most recently at the top, the one closed longest ago at the bottom |

     **Why the three columns differ.** `WAITING` holds work that has not started. It
     is a queue, and what a reader wants from a queue is the plan: `position`
     ascending is the order the user planned, and it answers "which task do I
     develop next?". `DOING` and `CLOSED` are not queues. They are records of what
     has happened, and what a reader wants from a record is recency: "what has just
     been picked up?" and "what has just been finished?". A task's place in the plan
     says nothing about when work on it began or ended, so ordering those two
     columns by the plan puts the card the reader came for somewhere in the middle
     of the column. The board therefore does hold more than one notion of order, and
     it holds it deliberately rather than arbitrarily: each column is ordered by the
     one thing that column is about.

     **`started_at` orders the whole `DOING` column, and `tested_at` orders
     nothing.** That column groups two statuses, `DOING` and `TESTING` (see the
     table of columns above), and `started_at` records entry into `DOING` for both
     of them: a task reaches `TESTING` only from `DOING`, and the task state machine
     sets `started_at` on the `SPRINT → DOING` transition (see
     `STATE_MACHINE.md § Date Tracking Fields`). One key therefore serves the whole
     column, and a `TESTING` card takes its place from when its task entered
     `DOING`, not from when it entered `TESTING`. `MODELS.md § Task` stays canonical
     for `started_at`, `tested_at`, and `closed_at`; this rule does not redefine
     them.

     **The tiebreaker is the plan.** Two cards of one column can carry the same
     ordering timestamp: `task stat` changes the status of several tasks in a single
     bulk operation (see `COMMANDS.md § Change Status (stat)`), and tasks moved
     together can carry one and the same timestamp, so equal timestamps are an
     ordinary case and not a theoretical one. When the ordering timestamp of two cards is equal, and when a
     card's ordering timestamp is absent, the cards are ordered by `sprint_tasks`
     `position` ascending. The fallback is the plan because the plan is the only
     other order the sprint defines: falling back to the task `id`, or to the order
     the rows happened to arrive in, would order the column by something the sprint
     does not mean. A card whose ordering timestamp is absent sorts **last** in its
     column, after every card that carries one, because a column ordered by recency
     has nowhere else to put a card that states no time. `MODELS.md § Task` makes
     both timestamps nullable, so this rule says what happens when one is absent
     rather than assuming one is always there.

     Together the two keys make every column's order **total and deterministic**:
     for any two cards of a column the rule states which of them is above the other,
     and two renderings of the same data produce the same board, which is what lets a
     test assert it. `position` is also the key the
     page's own read orders by, so where two member tasks of one sprint carry the
     same `position` the board keeps the relative order that read gave them; beyond
     its column key the board introduces no order of its own.

     **The ordering costs no read.** The page reads its member tasks once, in
     `sprint_tasks` position order (see `MODELS.md § Sprint`,
     `DATABASE.md § Relationships`, and
     `DATABASE.md § List Sprint Tasks Ordered by Position`), and that is still what
     the read returns. The board groups those rows into the three columns and
     reorders two of the three afterwards, in memory, over the rows already in hand:
     no second read, no query per column, and no query per card. The tiebreaker
     needs no extra data either, because the position order is the order in which
     the rows arrived, so a stable sort by the column's timestamp leaves the cards
     that timestamp does not separate in exactly the order the tiebreaker calls for.
   - **The card.** Each card presents one member task on three lines, in this
     order:
     1. the task **`title`**, leading the card — the one place this board
        deliberately differs from the tasks board's card, which leads with its
        reference line instead, because the GitLab issue board card leads with the
        title and this board follows the model it is drawn from;
     2. the task **reference** `#<id>` (the task's `id`) on its own line, rendered
        as secondary, muted text, so the card is identifiable by its id without the
        id competing with the title for the reader's attention;
     3. **one line carrying both of the card's remaining groups**: the task's two
        badges at the **leading edge** of that line, and the task's two counters at
        its **trailing edge**.

        The **`priority`** and **`severity`** lead the line, each as a Tabler badge
        carrying that task's integer value behind the one-letter prefix that names
        it — `P` for the priority and `S` for the severity, so a task of priority
        `5` and severity `3` shows `P5` and `S3` — and coloured by the band the
        value falls in, using exactly the mapping in
        [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours).
        They occupy the place the GitLab card gives its labels. No new badge colour
        and no new band is introduced here. The prefix rule itself is stated once,
        for the card of both boards, in [Roadmap Tasks Page](#roadmap-tasks-page),
        **Card content**, item 3, and is not restated here: this card renders the
        pair exactly as the tasks board's card does, and the two boards keep one
        form for it.

        The **counters** close the same line at its trailing edge, which is where
        the GitLab card puts its counters. They are the task's number of comments
        and its number of subtasks (`subtask_count`), **in that order: the comment
        count first, then the subtask count**. Each is rendered as an icon followed
        by its number — `ti ti-message` for the comment count and `ti ti-subtask`
        for the subtask count, the same two icons the tasks board's card metadata
        uses (see [Roadmap Tasks Page](#roadmap-tasks-page)). Both counters are
        always rendered, including when the number they carry is `0` (see **Both
        counters are always rendered** below).

     **Why the badges and the counters share a line.** Between them the two groups
     answer one question about the task — what this task **is**, and how much is
     **attached** to it — and a card is read at a glance, so the reader takes them
     in together rather than one after the other. Giving each group a line of its
     own also made every card taller than its content needs, and height is the
     scarce dimension here: the column a card sits in is bounded and scrolls (see
     **Height and scrolling** below), so every row of height a card does not need is
     a card the reader has to scroll to reach.

     **The line wraps rather than overflowing.** The leading group and the trailing
     group sit on one line for as long as the card is wide enough to hold both. When
     it is not — a column held at its `17rem` minimum, or a reader whose text size
     is large — the line **wraps**, placing the trailing group below the leading one
     inside the same card. It never overflows the card's edge, and it never makes
     the card, the column, or the page scroll horizontally (see
     [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
     rules 2 and 10). The rule is stated as behaviour because that is what a reader
     and a test can observe; the card carries no inline `style` attribute and every
     class it emits is defined either in the vendored Tabler distribution or in the
     project override stylesheet `static/style.css` (see
     [UI Framework](#ui-framework), rules 8 and 10, and **Markup** below).

     **The two cards differ here, and the difference is deliberate.** The tasks
     board's card keeps its **separate metadata footer** and does not fold it into
     the badge line (see [Roadmap Tasks Page](#roadmap-tasks-page), **Card
     content**, item 4). That footer lists five indicators of mixed kinds, one of
     which — the sprint the task belongs to — is text rather than a count and
     carries no bounded width, so the list cannot share a line with the badges: it
     would either push them off the line or wrap beneath them and
     spend the height the merge exists to save. This card carries exactly two
     indicators, both counts and both short, so it can. The two cards therefore
     diverge in this one line and in nothing else: the badge form and its prefixes,
     the two icons, the badge colours, the absent status badge, and the card as the
     modal trigger all stay shared. This is a stated divergence, not drift.

     **The counter order differs from the tasks board's too.** On this card the
     comment count comes first and the subtask count second. The tasks board's
     metadata footer keeps its own order, in which the subtask count precedes the
     comment count among the five indicators it lists. The two orders are stated
     separately because the two groups are separate — a pair read at the trailing
     edge of a line here, a list of five heterogeneous indicators in a block of its
     own there — and neither order is derived from the other. Each is fixed in this
     specification rather than left to the template, so that what a card shows is
     testable rather than incidental.

     The card carries **no status badge**: the column the card sits in already
     states the task's status, which is the reason the tasks board's card omits one
     as well.

     The card shows those six data points and no others: no task type and no
     dependency counts. It presents a subset of the task's fields by design,
     because a card is read at a glance and a column of cards is
     read as a whole; every field of the `Task` model is shown in the task detail
     modal the card opens (see [Task Detail Modal](#task-detail-modal)). The card
     does not redefine any field; `MODELS.md` and `DATABASE.md` remain canonical.

     **Both counters are always rendered.** The comment count and the subtask count
     are present on every card of this board, including when either or both are `0`:
     a task with no comment shows the comment icon followed by `0`, a task with no
     subtask shows the subtask icon followed by `0`, and the trailing edge of the
     third line therefore carries both numbers on every card the board renders.

     This is a deliberate departure from the tasks board's card, which renders an
     indicator only when it has something to count (see
     [Roadmap Tasks Page](#roadmap-tasks-page), **Absent metadata renders
     nothing**). The two cards differ because what they carry differs. This card
     carries exactly two indicators and both of them are counts, so rendering both
     always makes every card of the board the same shape and makes each number
     meaningful: a `0` states that the task has no comment, where an absent counter
     leaves the reader unable to tell "no comments" from "this card does not show
     comments". The tasks board's card carries five heterogeneous indicators, one of
     which — the sprint the task belongs to — is text rather than a count and has no
     zero to show, so always rendering all five is not even well defined there.
   - **The card is the trigger, and the trigger is a `<button>`.** Selecting a card
     opens the read-only task detail modal for that task, and the card itself is a
     `<button type="button">`, a natively activatable element, exactly as the tasks
     board's card is. A pointer click, a touch tap, Enter, and Space therefore all
     open the modal through the browser's own activation behaviour, with no added
     JavaScript. The card carries no `tabindex` and no `role="button"`: both would
     be redundant on a `<button>`, and a non-activatable element made to announce
     itself as a button MUST NOT be the trigger (see
     [Task Detail Modal](#task-detail-modal), *The trigger is a natively activatable
     element*). The card's accessible name is
     `Open details for task #<id>: <title>`, the same form both existing surfaces
     use. The `title` is required in it, not optional: the card's visible label is
     the task title, and an accessible name that omitted it would leave the control
     impossible to activate by speech input, which is what WCAG 2.5.3 Label in Name
     (Level A) forbids.

     The card can hold that contract whole, which a table row cannot. A row is not
     an activatable element and can hold no single control that wraps it, so a
     tabular presentation has to push the trigger down into one cell and leave the
     row itself clickable by pointer alone — two targets for one task. A card is a
     single element and can **be** the control, so pointer, touch, and keyboard
     reach the same target. No `<tr>` on this page is a modal trigger or carries
     one.
   - **Opening the modal costs the page nothing.** Opening a card fetches that
     task's fields and comments from the read-only endpoint
     `GET /roadmaps/{name}/tasks/{id}/data` and fills the page's single modal shell
     — the one modal element the page renders, not one per task — with them (see
     [Task Detail Modal](#task-detail-modal) and
     [Task Detail Endpoint](#task-detail-endpoint)). That request is made when the
     user opens a task, not while the page is rendered, so the board adds no query
     to the page's own read and no per-card cost.
   - **Height and scrolling.** The board is **height-limited**: it takes a definite,
     bounded height rather than growing with the number of member tasks, and each
     column scrolls **vertically and independently** inside that height when its
     cards exceed it, as a GitLab issue board's lists do. When the three columns do
     not fit the viewport's width, the **column strip** scrolls horizontally inside
     its own container, exactly as the tasks board's strip does, and the page itself
     never scrolls horizontally (see
     [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
     rules 2 and 10).

     The board is deliberately **not** a full-height page region, and that is where
     it departs from the tasks board. The sprint page is not a single-region page:
     it carries the Sprint details card above the board and the Comments card below
     it, and all three belong to one sprint presentation. A board sized to the space
     the page body leaves would fill the rest of the viewport on its own and push
     the Comments card below the fold for every sprint, while a board sized to its
     own content would push that card further down with every member task the sprint
     gains. The bounded height avoids both
     (see [Full-Height Page Regions](#full-height-page-regions)).

     **The height is `60vh`, with a floor read from `--full-height-region-floor`.**
     Both are declared in the project override stylesheet, and both are fixed here
     rather than left to taste, so that what the board does is testable rather than
     a matter of judgement:
     - The height is **viewport-relative** (`60vh`), so the board follows the
       screen: it presents more of a column on a tall display and less on a short
       one, at the same proportion of the screen everywhere, while leaving the rest
       of the page body to the cards above and below it.
     - The floor is the value of the **`--full-height-region-floor`** custom
       property, which is the floor the shell already holds the page body to (see
       [Full-Height Page Regions](#full-height-page-regions), rule 5). The board
       reads that property rather than restating the length, because a second copy
       of a number is a copy that can be changed on its own, and the two would then
       state different floors for the same screen. On a viewport short enough that
       `60vh` falls below it, the board takes the floor instead, so it keeps showing
       useful content rather than collapsing to a sliver of one card. The property
       MUST resolve on this page: it is declared where every page reads it, not on
       the full-height shell alone, because the sprint page is not a full-height
       page and would otherwise find nothing to read.

     **The three columns divide the width of the board.** The three columns share
     the board's width equally: all three carry the same width, and that width is an
     equal share of what the board leaves once the gaps between the columns are
     taken out, so the columns grow into whatever the viewport gives them instead of
     leaving the space beside them empty. A column stands for a state and not for a
     volume of work, so the three widths stay equal whatever number of tasks each
     column holds: the width follows the viewport, never the data.

     A column is never narrower than **17rem**, the width at which its cards stay
     legible. When three columns at that minimum, plus the gaps between them, do not
     fit the viewport, the columns keep the minimum and the column strip scrolls
     horizontally inside its own container exactly as it does when the board is
     wider than the viewport for any other reason, while `<body>` still produces no
     horizontal overflow (see **Height and scrolling** above and
     [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
     rules 2 and 10).

     The columns are separated by a **0.75rem** gap, and the body of a card carries
     **0.75rem** of padding on all four sides. Those two lengths, together with the
     `17rem` minimum above, are the lengths the tasks board's columns and cards
     already carry (see [Roadmap Tasks Page](#roadmap-tasks-page), **Column width
     and card density**). What the two boards no longer share is the column width
     itself.

     **Why the two boards differ here.** The five columns of the tasks board are
     unchanged by this rule: each is **19rem** wide, never narrower than **17rem**,
     and does not grow into a viewport wider than that board needs. Dividing a
     viewport among five columns would leave each one narrow enough to hurt the
     measure a card's title is read on, which is the length that board's fixed width
     exists to protect; three columns dividing the same viewport are wide, not
     narrow. The two boards are also read differently. The tasks board is a view of
     a whole roadmap, and its column count is fixed by the task status enum rather
     than by what is on screen, so the board has a natural width of its own and the
     space beyond it is left empty. This board has three columns and is read as one
     sprint at a glance, which is what makes filling the width the right shape for
     it. The lengths that remain shared — the `17rem` minimum, the `0.75rem` gap,
     and the `0.75rem` card body padding — stay shared, so a reader moving between
     the tasks page and a sprint page still meets one card measure and one minimum
     column.

     Every one of these lengths is expressed in `rem`, so the minimum column and the
     card scale with the reader's own text size.

     On a narrow viewport the board stays usable on the tasks board's terms: each
     column keeps the minimum width above, at which its cards remain legible, the
     horizontal strip scroll is reachable by a touch gesture, the cards present
     touch-friendly hit targets, and the task detail modal a card opens stays usable
     at the same viewport (see [Task Detail Modal](#task-detail-modal)).
   - **Read cost: one grouped comment count, and nothing per card.** The card shows
     a comment count, so the page reads one. That count is read with **one grouped
     query** over the whole set of rendered member-task ids (see
     `DATABASE.md § Count Comments for Many Parents (Grouped)`) — never one query
     per card, and never a comment **body**: the card displays a number, and reading
     the text of every comment of every member task in order to display a number
     would be work the page throws away. A member task's comment text is read only
     when the user opens that task's modal, one task at a time, through the task
     detail endpoint. When the sprint has no member task the page issues no such
     query at all, because the query takes a set of rendered task ids and that set
     is empty.

     The page therefore issues exactly **two** comment reads whatever the number of
     member tasks: the sprint's own comment listing, which the Comments card renders
     in full as a log (see `DATABASE.md § Comments`), and this one grouped count.
     Neither grows with the number of member tasks, which is the invariant that
     matters here (see
     [Tasks and Sprints from SQLite](#tasks-and-sprints-from-sqlite)).

     The **subtask count needs no read of its own**: the sprint's member-task read
     already returns each task's `subtask_count` (`MODELS.md § Task` defines it as a
     count computed with the task rather than a stored column), so the card's
     subtask number is already in hand. Grouping the member tasks into the three
     columns, ordering each column, and counting each column are done in memory over
     the rows already read, so the board adds no query per column and none per
     card.
   - **Read-only.** The board offers **no drag-and-drop** and no control of any
     other kind that moves a task between columns, reorders cards, changes a task's
     status, or creates or edits anything. It contains no form and no write path;
     its only interaction is opening the read-only task detail modal. This is the
     same deliberate divergence from the GitLab issue board the tasks page states:
     the inspiration is structural — columns per state, cards, per-column counts —
     and never interactive, and the `rmp` CLI remains the sole write path for every
     task (see [Security and Constraints](#security-and-constraints)).
   - **Markup.** The board introduces no exception to the markup rules already in
     force: no template carries an inline `style` attribute, and every class the
     board emits is defined either in the vendored Tabler distribution or in the
     project override stylesheet `static/style.css` (see
     [UI Framework](#ui-framework), rules 8 and 10). Where Tabler provides the
     component, the board uses Tabler's markup — Tabler cards for the task cards,
     the card-header idiom for the column headers, Tabler badges for the counts and
     for the priority and severity values, and Tabler's empty-state markup for an
     empty column. The vendored Tabler distribution ships no board or Kanban
     component, so the column strip's own layout, height, and scrolling rules live
     in `static/style.css`, which is the specified home for project styling no
     Tabler class covers.

5. **Comments card.** The last card of the sub-template presents the sprint's own
   comments — the sprint's progression log. The fields of a comment are defined for
   the `SprintComment` model in `MODELS.md § Sprint Comment`; the sub-template does
   not redefine them.
   - **Scope.** The card shows the comments of the sprint itself. It does not show,
     aggregate, or merge in the comments of the sprint's member tasks; those are
     reachable through each task's own detail modal (see
     [Task Detail Modal](#task-detail-modal)).
   - **Order and completeness.** Oldest first, exactly the order
     `sprint comment-list` returns (`created_at` ascending, comment `id` ascending
     as the tie-breaker). Every comment of the sprint is rendered: no type filter,
     no count limit.
   - **Card header.** A `card-header` with the card title `Comments` and a Tabler
     badge showing the number of comments, following the same header idiom the
     member-tasks board's column headers use. The idiom is shared; the colour is
     not. This badge counts comments, and a comment carries no status of any kind,
     so there is nothing for the semantic mapping to key on and the badge keeps the
     neutral `bg-secondary-lt` variant while a column badge of the board above takes
     the colour of the status its column groups (see
     [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
     rule 2, **The discriminating test**).
   - **What each entry shows.** For one comment, in order: its `type` as a badge,
     its `created_at` timestamp, its `updated_at` timestamp when that value is not
     null (marking the entry as edited), and its `body`.
   - **Markup.** The card body holds Tabler's Timeline component with the same
     structure the task detail modal uses: `<ul class="timeline">` with
     `<li class="timeline-event">` items, each an icon
     (`<i class="ti ti-message"></i>`) in `timeline-event-icon` and a
     `card timeline-event-card` holding the entry. The type badge uses the neutral
     `bg-secondary-lt` variant for every type value, exactly as in the modal, and
     introduces no per-type colour.
   - **Authored line breaks.** A comment body is multi-line as authored through the
     CLI, and the card renders it preserving the author's line breaks (see
     [Frontend Rules](#frontend-rules), rule 6).
   - **Empty state.** When the sprint has no comments, the card shows a clear
     empty-state message in place of the timeline, in the same idiom a column of the
     member-tasks board uses when it holds no task. The card itself is always
     present.
   - **Read-only.** The card renders data only: no form, no input, no edit control,
     and no submit action.

6. **Read-only.** The sub-template renders data only. It contains no form, button,
   or link that submits a change; the only interaction is opening the read-only
   task detail modal from a board card.

7. **Authored line breaks.** Wherever the sub-template renders the sprint's
   `description`, it preserves the author's line breaks as specified in
   [Frontend Rules](#frontend-rules), rule 6.

### Roadmap Knowledge-Graph Page

- **Route:** `GET /roadmaps/{name}/graph`
- **Content:** An HTML page that renders the named roadmap's knowledge graph as
  an interactive node-link visualisation. The page loads the vendored D3.js
  library (and the d3-sankey plugin) from `/static/...` and fetches the graph's
  nodes and edges as JSON from the graph data endpoint
  (`/roadmaps/{name}/graph/data`).
- **Query bar.** At the top of the page, above the graph card, a query bar lets
  the user drive the graph from a single editable Cypher query, with a Search
  button and a node-limit dropdown. On page load the query box holds the default
  query and the graph shows the full-graph view. The query bar is specified in
  [Graph Query Bar](#graph-query-bar); its failure modes are specified in
  [Query-Bar Error Handling](#query-bar-error-handling).
- **Graph card layout.** The visualisation is presented inside a Tabler card. The
  card holds two regions side by side: a labels sidebar column on the left and the
  graph canvas on the right. The labels sidebar lists the graph's node labels and
  edge types and lets the user highlight elements interactively; it is specified in
  [Graph Labels Sidebar](#graph-labels-sidebar). The labels sidebar and the
  visualisation read from the same already-fetched graph data; the sidebar adds no
  new request and no new endpoint. The card is a **full-height page region**: its
  height is the space the page body leaves once the top navbar, the page header,
  and the query bar are placed above it, it ends where the page body ends, and that
  edge lies within the viewport, exactly as
  [Full-Height Page Regions](#full-height-page-regions) requires — so the page does
  not scroll vertically to reveal the bottom of the canvas.
- **Layout selection.** The page provides a dropdown (select control) that lets
  the user choose which layout renders the graph, offering the complete set of
  layouts from the "Networks" section of the D3 gallery: Force-directed graph,
  Disjoint force-directed graph, Mobile patent suits, Arc diagram, Sankey diagram,
  Hierarchical edge bundling, Chord diagram, Directed chord diagram, and Chord
  dependency diagram. The page renders the **Mobile patent suits** layout by
  default, and changing the selection re-renders the same graph data in the chosen
  layout. Layouts that need a constrained data shape (Sankey requires a directed
  acyclic graph; Hierarchical edge bundling and the Chord variants derive a
  grouping or adjacency matrix from the graph) degrade gracefully: the option is
  always offered, and when the current graph cannot be drawn in the selected
  layout the page shows a clear, read-only in-place message instead of erroring
  (see
  [Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library)).
- **Interaction.** The visualisation supports pan and zoom and shows the
  properties of a node or an edge when the user selects it. Node and edge labels,
  types, and properties shown come directly from the graph data (see
  [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)).
  A property value that the user authored as multi-line free-text (for example a
  node's specification text or notes) is shown preserving its source line breaks
  rather than collapsing them, consistent with
  [Frontend Rules](#frontend-rules), rule 6.
  The visualisation MUST be usable without a mouse: it supports touch gestures
  (pan, pinch-to-zoom, and tap to select and inspect) and surfaces node and edge
  detail through tap or selection rather than relying on mouse hover, so the page
  is fully usable on touch devices (see
  [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
  Selecting an element in the canvas to inspect its detail works independently of
  the labels-sidebar highlight state and of the layout dropdown: a label highlight
  dims non-matching elements but does not prevent the user from selecting any
  element and opening its detail (see
  [Graph Labels Sidebar](#graph-labels-sidebar)).
- **Neighbor focus on node selection.** Selecting a node in the canvas, in
  addition to opening that node's detail panel, puts the graph into a **neighbor
  focus** state centred on the selected node. In neighbor focus the page
  emphasises the selected node, its **first-degree neighbours**, and the **edges
  incident to** the selected node; every other element — second-degree nodes and
  beyond, and every edge not incident to the selected node — is **dimmed**
  (rendered at a reduced opacity) rather than removed from the canvas. The dimming
  uses the same dim-not-remove mechanism the labels sidebar uses for its highlight
  (see [Graph Labels Sidebar](#graph-labels-sidebar), rule 4), so the full graph
  stays visible and the focused neighbourhood is seen in its surrounding context.
  The first-degree neighbourhood is **undirected** for this purpose: it includes
  every node reachable from the selected node by exactly one edge in **either
  direction** (a target of an outgoing edge or a source of an incoming edge), and
  the incident edges emphasised are the edges between the selected node and those
  neighbours, regardless of edge direction. Neighbor focus emphasises and dims
  elements only; it never adds or removes nodes or edges.
- **Clearing neighbor focus.** Neighbor focus is cleared by a single, consistent
  clear gesture: selecting the same focused node again, selecting an empty area of
  the canvas (a point on no node and no edge), or closing the node detail panel.
  Any of these gestures both closes the detail panel and clears the neighbor
  focus, so the detail panel and the neighbor-focus emphasis are opened and
  cleared together. Clearing the focus restores the **prior view**: if any labels
  sidebar entries are still active, the canvas returns to the labels-sidebar
  highlight state (see [Graph Labels Sidebar](#graph-labels-sidebar)); otherwise
  it returns to the normal, non-dimmed view. Selecting a different node while a
  node is already focused moves the focus to the newly selected node (its detail
  panel opens and its neighbourhood becomes the emphasised set) without an
  intervening clear.
- **Neighbor focus takes precedence over the labels-sidebar highlight.** While a
  node is focused, the neighbor-focus emphasis governs the canvas dimming and the
  labels-sidebar highlight is **not** applied to the canvas: an active label or
  type selection in the sidebar does not drive canvas dimming while a node is
  focused. The sidebar's selected entries may remain visually selected in the
  sidebar itself, but they take effect on the canvas only once the focus is
  cleared (see [Graph Labels Sidebar](#graph-labels-sidebar), rule 8).
- **Neighbor focus coexists with the layout dropdown and the query bar.** Changing
  the layout in the layout dropdown re-renders the same graph data; the page
  reapplies the current neighbor focus to the re-rendered layout, emphasising the
  same selected node, first-degree neighbours, and incident edges. Running a
  search from the query bar (see [Graph Query Bar](#graph-query-bar)) **clears the
  neighbor focus** together with re-rendering the new result: because the search
  fetches a new graph, any prior focus is discarded and the new result renders in
  its labels-sidebar highlight state if any entries are active, otherwise in the
  normal view. Neighbor focus is **touch-friendly**: it is driven by the same tap
  to select that opens the detail panel, and cleared by the same tap gestures,
  consistent with the page's existing touch interaction (see
  [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
- **Client-side only, read-only preserved.** Neighbor focus is computed and
  applied entirely **client-side**, in the page's JavaScript, from the graph data
  the page already holds. It adds no new server endpoint, no new server-side
  computation, and no write path, and it changes neither the graph data endpoint's
  response shape nor the read-only behaviour of the page.
- **Empty graph.** A roadmap that has never used the `graph` command, or whose
  graph is empty, renders successfully and shows an empty-graph state. Reading a
  roadmap that has no graph yet is not an error (see
  `GRAPH.md § Persistence Layout`, rule 2), and the default query writes nothing,
  so the request produces no snapshot files.

### Graph Query Bar

The query bar is a control rendered at the top of the knowledge-graph page, above
the graph card (see [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)).
It lets the user drive the visualisation from a single editable Cypher statement
instead of a fixed full-graph read. The query bar drives, and re-renders from, the
same graph data endpoint the page already consumes; it adds no new endpoint.

**The statement is executed as written.** The endpoint does not examine it, so a
statement typed into the query bar may create, change, or delete graph data and
may change the graph's schema, exactly as the same statement would under
`rmp graph execute`. Nothing in the page or the server prevents that, and nothing
authenticates the request (see
[Security and Constraints](#security-and-constraints)).

1. **One editable query drives the graph.** The page renders the graph from one
   Cypher query. This replaces the previous fixed pair of reads
   (`MATCH (n) RETURN n` for nodes and `MATCH ()-[r]->() RETURN r` for edges): a
   single query now produces both the nodes and the edges, through the
   result-to-graph extraction the graph data endpoint performs (see
   [Graph Data Endpoint](#graph-data-endpoint)).

2. **Default query.** On page load the query box is pre-filled with the
   **default query**

   `MATCH (n) OPTIONAL MATCH (n)-[r]->(m) RETURN n, r, m`

   and the user can edit it. The default query produces the same full-graph view
   the page produced before the query bar existed: all nodes, plus all
   relationships, subject to the selected node limit. The default query is the
   single source of the page's initial graph and is identical to the query the
   graph data endpoint runs when its `q` parameter is absent (see
   [Graph Data Endpoint](#graph-data-endpoint)).

3. **Controls, left to right.** The query bar presents three controls in a fixed
   left-to-right order:
   - a **query box** (a multi-line text input) that shows the current Cypher query
     and is editable; on page load it holds the default query;
   - a **Search button** to the right of the query box that re-runs the query
     currently in the box and re-renders the graph from the result;
   - a **node-limit dropdown** (a select control) to the right of the Search
     button, offering exactly the six values `50`, `100`, `250`, `500`, `1000`,
     and `3000`, with `100` selected by default.

4. **Search re-runs the query.** Selecting the Search button re-fetches the graph
   data endpoint (`GET /roadmaps/{name}/graph/data`) with the current query box
   text as the `q` parameter and the current dropdown value as the `limit`
   parameter, then re-renders the graph from the response in the currently selected
   layout. The request stays GET-only; the query text and the limit are passed as
   URL query parameters and no request body, no `POST`, and no new endpoint is
   introduced (see [Graph Data Endpoint](#graph-data-endpoint)). On page load the
   page performs this same fetch once with the default query and the default limit.

5. **Keyboard accelerator: Ctrl+Enter searches.** When the query box has focus,
   pressing Ctrl+Enter triggers the search exactly as selecting the Search button
   does: the same fetch to the graph data endpoint
   (`GET /roadmaps/{name}/graph/data`) with the current query box text as the `q`
   parameter and the current dropdown value as the `limit` parameter, the same
   limit validation, the same re-render of the graph in the currently selected
   layout, and the same in-place error surfacing on failure (see
   rule 4, [Graph Data Endpoint](#graph-data-endpoint), and
   [Query-Bar Error Handling](#query-bar-error-handling)). Ctrl+Enter is an
   accelerator for the existing Search action and introduces no other behaviour.
   Plain Enter in the query box is unchanged: it inserts a newline and does not
   trigger a search, so the user can compose a multi-line query freely.

6. **Node limit applied by the endpoint.** The dropdown value is the `limit`
   parameter sent on the request. The endpoint applies it as a `LIMIT` clause only
   when the user's query both lacks a top-level `LIMIT` of its own and is a
   statement that admits a `LIMIT` clause. A user who writes their own `LIMIT`
   keeps it and the dropdown value is not applied. A statement with no top-level
   `RETURN` admits no `LIMIT` at all — a standalone procedure call, and every write
   that projects nothing, which is what rule 7 below depends on — so the dropdown
   value does not apply to it either and the statement runs as written rather than
   failing in the parser. A schema-introspection command admits none despite
   carrying a projection, and is treated the same way. The injection, precedence,
   and suppression rules are specified in
   [Graph Data Endpoint](#graph-data-endpoint), which is canonical for them.

7. **The bar submits whatever is typed into it.** The query box offers no create,
   edit, or delete affordance of its own — there is no button that writes — but
   the statement it submits is not examined, so a `CREATE`, a `SET`, a
   `DETACH DELETE`, or a `DROP CONSTRAINT` typed into the box is executed and
   committed, and the invocation checkpoints exactly as `rmp graph execute` does
   (see [Graph Data Endpoint](#graph-data-endpoint),
   [Security and Constraints](#security-and-constraints), and
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)).
   The page shows no confirmation and asks for no credential before running one.

8. **Error surfacing.** When a search fails — because the limit is invalid, or
   because the statement fails to execute, which includes exhausting the
   endpoint's query time budget (see
   [Graph Query Time Budget](#graph-query-time-budget)) — the page shows a clear
   message in place and does not crash, exactly as the layout degradation does;
   the two cases are specified in
   [Query-Bar Error Handling](#query-bar-error-handling).

9. **Coexistence with the other graph controls.** The query bar coexists with the
   layout dropdown, the labels sidebar, and the node/edge detail panel (see
   [Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library),
   [Graph Labels Sidebar](#graph-labels-sidebar), and
   [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)). After a
   successful search the graph re-renders with the currently selected layout, and
   the labels sidebar inventory and counts recompute client-side from the new
   result (the new set of nodes' `labels` arrays and edges' `type` fields), so the
   sidebar always reflects the graph currently shown.

10. **Touch- and small-viewport-usable.** The query box, the Search button, and the
    limit dropdown are touch-friendly controls that fit a small viewport without
    forcing horizontal overflow, consistent with
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design).

### Query-Bar Error Handling

A search driven by the query bar can fail for two distinct reasons, and the page
MUST surface each clearly and in place without crashing, consistent with the
graceful layout degradation already specified (see
[Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library),
rule 5). The two are kept distinct so the user understands what to fix.

The endpoint answers both with HTTP `400 Bad Request` and a JSON body that names
the failure's class in a `kind` field, in the shape specified in
`DATA_FORMATS.md § Graph View Data`, **Error Shape**. Rules 3 to 7 below fix the
status, the order in which the two are decided, the boundary against the `500` of
an internal read error, and what the body carries.

1. **Invalid limit.** When the `limit` parameter is not one of the six allowed
   values (`50`, `100`, `250`, `500`, `1000`, `3000`), the endpoint rejects the
   request as an invalid limit and does not execute the statement; the page
   surfaces a clear message naming the invalid limit. Because the limit values
   originate from the page's own dropdown, this state is normally only reachable by
   a crafted request, but the endpoint rejects it rather than guessing a value. The
   endpoint answers HTTP `400 Bad Request` with `kind` `invalid_limit`. The
   rejection is decided before the graph store is opened, so it opens nothing,
   reads nothing, and writes nothing.

2. **The statement failed to execute.** When the submitted statement fails in the
   engine — invalid Cypher syntax, for example, or a schema statement the engine
   refuses — the page surfaces a clear message stating that the statement failed
   to execute. A statement that the endpoint cancels because it exhausted the
   endpoint's 5-second query time budget is an execution failure of this same case
   and is surfaced with this same message; the budget is specified in
   [Graph Query Time Budget](#graph-query-time-budget). The endpoint answers HTTP
   `400 Bad Request` with `kind` `execution` in every case of this rule.

3. **In-place, non-fatal.** In both cases the message is shown in place on the
   page, the page does not crash, and the failure triggers no navigation, exactly
   as the layout-degradation message does. The user can edit the statement or
   change the limit and search again. The graph already shown is left in place.

4. **One status, two kinds — and this rule is the one place the set is
   enumerated.** The endpoint's `kind` takes exactly these two values and no
   others: `invalid_limit` and `execution`. Every other statement of the set in
   this specification refers here rather than repeating it, so the count and the
   list cannot drift apart across sections; a value is added or removed here
   first. Both failures carry HTTP `400 Bad Request`, and the body's `kind` field
   is what distinguishes them. One status fits both because in each of them the
   server is able to serve the route and refuses the request the caller made: the
   `limit` falls outside the closed set the endpoint publishes, or the statement
   the caller wrote cannot be executed. RFC 9110, Section 15.5.1, defines `400` as
   the status for a request the server "cannot or will not process ... due to
   something that is perceived to be a client error", and RFC 9110, Section 15.5,
   puts the explanation of the error in the response representation, which is
   exactly what the `kind` and `error` fields are. Splitting the two across
   different statuses would assert a distinction HTTP does not carry, while the
   body already carries it precisely.

   A statement cancelled for exhausting the time budget carries this same `400`
   and this same `execution` kind. It is neither a `503` nor a `504`. RFC 9110,
   Section 15.6.4, defines `503` as a temporary overload or scheduled maintenance
   "which will likely be alleviated after some delay": this server is neither
   overloaded nor under maintenance, it keeps serving every other request, and
   delay alleviates nothing, because the same statement over the same store
   exhausts the same budget again. RFC 9110, Section 15.6.5, defines `504` for a
   server "acting as a gateway or proxy" that did not receive a timely response
   "from an upstream server": this server is neither, and the engine it runs the
   statement on is in-process, not an upstream server. What is true of a budget
   exhaustion is that the caller asked this endpoint for more work than it spends
   on one request, and that the caller changes the outcome by writing a cheaper
   statement. `400` states that; the two 5xx codes state something else that is not
   the case here.

5. **The `limit` is resolved before the statement runs, so the two kinds cannot
   both apply.** One request can carry an invalid `limit` and an unexecutable
   statement at once. The endpoint resolves the `limit` first and refuses the
   request there, so such a request is answered `invalid_limit` and the statement
   is never executed. There is no further precedence question: `execution` is
   reached only by a request whose `limit` was accepted.

6. **The boundary against the internal read error is drawn at when the failure
   surfaces, not at what the failure is.** This endpoint answers an internal read
   error with `500`, exactly as every other route does (see
   [Routes and Pages](#routes-and-pages) and
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
   rule 5). What separates that `500` from the `400` of case 2 is the moment the
   failure surfaces: a failure to open the roadmap's graph store, or to take its
   lock within the bounded wait, is an internal read error and is answered `500`,
   while a failure that surfaces once the statement is running — from the run
   itself, from the commit, or from the walk over the result it produces — is an
   execution failure and is answered `400`.

   The boundary is a rule about timing, and it is deliberately not a claim about
   what the failure is. A store corruption that a scan discovers while the
   statement is already running surfaces as an execution failure and is therefore
   reported as one, with `400` and `kind` `execution`, even though its cause is the
   store and not the statement. The endpoint classifies the engine's failures no
   further than this. Drawing the boundary at the moment of surfacing keeps it
   verifiable from outside the server, where drawing it at the cause would make the
   contract depend on which failures the engine happens to tell apart.

7. **The response body.** Each of the two failures carries a JSON body of exactly
   two string fields, `error` and `kind`, in the shape specified in
   `DATA_FORMATS.md § Graph View Data`, **Error Shape**, which is canonical for it.
   `kind` is the machine-readable class; rule 4 above enumerates its value set and
   is canonical for it. `error` is the human-readable reason the page shows in
   place. The `error` of an execution failure carries the engine's own diagnostic
   text, so the user reads for a given statement the same diagnostic the CLI prints
   for it (see `GRAPH.md § Error Handling and Exit Codes`, rule 2) and can act on
   it; the `error` of an invalid limit names the rejected value, which is what
   case 1's message requires. The `500` of an internal read error does not carry
   this shape: it is answered as every other route's internal read error is.

8. **A request the caller abandoned is answered, but nobody reads the answer.** A
   client that disconnects mid-statement cancels it immediately (see
   [Graph Query Time Budget](#graph-query-time-budget), rule 2). The endpoint
   treats that cancellation as an execution failure like any other and answers it
   with the same `400` and the same `execution` kind, with an `error` naming the
   cancellation rather than the budget, because the two have different causes and
   the budget must not be blamed for a caller that gave up. That answer reaches no
   one: the client that would have read it is gone. It is specified here because it
   is a third reason the `execution` kind arises, and a contract naming only two
   would be incomplete on the day it is written. It is not an outcome a connected
   client can observe, so no client-side test can assert it.

   **A cancelled statement may already have committed.** The endpoint runs the
   caller's statement on the transactional path, so a disconnect that arrives after
   the commit and before the response cancels nothing that matters: the change is
   durable, and the checkpoint that follows it runs to completion. A disconnect
   that arrives before the commit leaves the transaction uncommitted and the graph
   unchanged. Which of the two happened is not reported to anyone, because the
   caller is gone.

9. **A statement that returns no node and no edge is a success, not a failure.**
   The endpoint answers it HTTP `200` with `{"nodes": [], "edges": []}`. This is
   the answer to `MATCH (n:Absent) RETURN n`, which matched nothing; to
   `MATCH (n) RETURN count(n)`, which returned a number; to `SHOW INDEXES`, which
   returned tabular rows; and to `CREATE (n:Spec {key:'k'})`, which created a node
   and returned no columns at all. The four are indistinguishable in the response,
   and the page shows an empty graph for each. The endpoint publishes no failure
   class that separates them, because they did not fail: each ran, and none
   produced an element the response shape can carry.

### Graph Labels Sidebar

The labels sidebar is a column rendered inside the graph card, to the left of the
graph canvas (see [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)).
It gives the user a complete inventory of the graph's labels and edge types with
counts, and lets the user highlight the matching elements on the canvas. It is a
read-only, in-page control: it triggers no server request, no navigation, and no
write.

1. **Two sections.** The sidebar lists all labels present in the graph, organised
   into two clearly separated sections, each with a section header:
   - **Node labels.** The section header shows the title and, alongside it, the
     section total: the total number of **distinct nodes** in the current graph
     result. Below the header, the section lists one entry per distinct node label
     present in the graph (for example `Spec`, `Code`, `Memory`, `Decision`). Each
     entry shows the label name and a counter with the number of nodes that carry
     that label. A node that carries more than one label counts towards each of its
     labels, so the per-label counts may sum to more than the section total; the
     section total is the distinct-node count, not the sum of the per-label
     counts. A node that carries no label (its `labels` array is empty; see
     `DATA_FORMATS.md § Graph element mapping`, rule 2) contributes to no
     node-label entry but still counts towards the section total, because the
     section total counts distinct nodes regardless of their labels.
   - **Edge types.** The section header shows the title and, alongside it, the
     section total: the total number of **edges** in the current graph result.
     Below the header, the section lists one entry per distinct relationship type
     present in the graph (for example `IMPLEMENTS`, `DEPENDS_ON`). Each entry
     shows the type name and a counter with the number of edges of that type.
     Every edge has exactly one type, so the per-type counts sum to the section
     total.

2. **Deterministic ordering.** Within each section, the entries are sorted
   deterministically by their name (ascending, case-sensitive code-point order),
   so the sidebar renders the same order for the same graph on every request. The
   two sections are always shown in the fixed order Node labels first, then Edge
   types.

3. **Empty sections and empty graph.** Each section is handled gracefully when it
   has no entries: a graph with nodes but no labels shows an empty Node labels
   section with a clear empty-state indication, a graph with no edges shows an
   empty Edge types section with a clear empty-state indication, and an empty graph
   (no nodes and no edges) renders the sidebar with both sections empty. When a
   section has no entries, its section total renders as `0`; in an empty graph both
   section totals are `0`. An empty graph is a valid state, consistent with the
   empty-graph behaviour of the page
   (see [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)) and the
   empty graph view-data object (`DATA_FORMATS.md § Graph View Data`, rule 1). An
   empty sidebar is never an error.

4. **Highlight mode, not filter mode.** The sidebar is interactive and operates as
   a highlight control, not a filter. Selecting a node-label entry highlights every
   node that carries that label; selecting an edge-type entry highlights every edge
   of that type. Non-matching elements are **dimmed** (rendered at a reduced
   opacity) rather than removed from the canvas, so the full graph stays visible
   and the highlighted elements are seen in their surrounding context. The sidebar
   never adds or removes nodes or edges; it only changes how they are emphasised.

5. **Combinable, multi-selection union.** More than one entry can be active at the
   same time, across both sections. When several entries are active, the
   highlighted set is the **union** of their selections: an element is highlighted
   when it matches any active entry, and an element is dimmed only when it matches
   no active entry. Node-label selections and edge-type selections combine in the
   same union.

6. **Toggle and clear.** Each entry is a toggle. Selecting an inactive entry makes
   it active; selecting an active entry again toggles it off. When no entry is
   active, the canvas shows its normal, non-dimmed view: clearing all selections
   restores the normal view, with no element dimmed.

7. **Selected-state indication.** Every active entry is visually indicated as
   selected, so the user can see at a glance which labels and types are currently
   highlighted, and which entries to toggle off to clear the highlight.

8. **Coexistence with the other graph controls.** The highlight state coexists
   with the query bar, the layout dropdown, and the node/edge detail panel:
   - Changing the layout in the dropdown (see
     [Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library))
     re-renders the same graph data; the active label and type selections continue
     to apply to the re-rendered layout, highlighting the same logical elements.
   - Running a search from the query bar (see [Graph Query Bar](#graph-query-bar))
     re-fetches the graph data and re-renders the graph; the sidebar inventory and
     counts recompute from the new result, so the sidebar always reflects the graph
     currently shown.
   - Selecting a node or an edge on the canvas to open its detail (see
     [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)) works whether
     or not a highlight is active and whether or not the selected element is
     currently dimmed; the highlight state does not block element selection or the
     detail panel.
   - Selecting a node also puts the canvas into **neighbor focus** (see
     [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)), which takes
     **precedence** over this highlight: while a node is focused, the
     neighbor-focus emphasis governs the canvas dimming and the active label and
     type selections are not applied to the canvas, though they may remain visually
     selected in the sidebar. When the focus is cleared, the canvas returns to this
     highlight state if any entry is still active, otherwise to the normal,
     non-dimmed view.

9. **Touch-friendly.** Each sidebar entry is a touch-friendly hit target, and a tap
   toggles its selection, consistent with the touch-friendly graph interaction (see
   [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)). On a
   small viewport the labels sidebar adapts to the available width together with the
   graph card rather than forcing horizontal overflow.

10. **Data source: derived client-side, no new endpoint.** The label and type
    inventory and all counts are computed **client-side**, in the page's
    JavaScript, from the same graph data the page already fetches from the existing
    graph data endpoint (`GET /roadmaps/{name}/graph/data`,
    `{"nodes": [...], "edges": [...]}`; see
    [Graph Data Endpoint](#graph-data-endpoint) and
    `DATA_FORMATS.md § Graph View Data`). The node-label entries and their counts
    are derived from the `labels` arrays of the fetched nodes, and the edge-type
    entries and their counts are derived from the `type` field of the fetched
    edges. Computing the inventory client-side from the already-fetched data adds
    **no** new server endpoint, no new server-side aggregation, and no new write
    path, consistent with the read-only design of the graph page: the sidebar reads
    from whatever graph data the page currently holds and triggers no request of its
    own. When the query bar runs a search and the page re-fetches the graph data
    (see [Graph Query Bar](#graph-query-bar)), the sidebar inventory and counts
    recompute from the new response; the sidebar adds no fetch beyond the search the
    user already triggered. The graph data endpoint's response shape is unchanged by
    this feature.

11. **Section totals derived client-side.** Each section header shows an absolute
    total alongside its title: the Node labels header shows the total number of
    distinct nodes in the current graph result, and the Edge types header shows the
    total number of edges. Both totals are derived **client-side** from the same
    already-fetched graph data as the per-entry inventory (rule 10): the node total
    is the count of distinct fetched nodes (deduplicated by node `id`, as already
    returned by the endpoint; see [Graph Data Endpoint](#graph-data-endpoint),
    [Acceptance Criteria](#acceptance-criteria), criterion 49) and the edge total is
    the count of fetched edges. Because a node carrying more than one label counts
    towards each of its labels, the sum of the per-label entry counts may exceed the
    distinct-node total; the Node labels total is the distinct-node count, **not**
    the sum of the per-label counts. Every edge has exactly one type, so the Edge
    types total equals the sum of the per-type entry counts. When the query bar runs
    a search and the page re-fetches the graph data (see
    [Graph Query Bar](#graph-query-bar)), both section totals recompute from the new
    response together with the rest of the inventory, so the totals always reflect
    the graph currently shown. The totals add no new server endpoint, no new
    server-side aggregation, and no new write path.

12. **Collapse and expand control.** The sidebar has an icon control at its top
    that lets the user collapse (hide) or expand the sidebar column. The control is
    a single toggle: tapping or selecting it collapses an expanded sidebar and
    expands a collapsed one. When the sidebar is collapsed, the column contracts so
    the graph canvas takes the full width of the graph card, and only the affordance
    to expand it again (the icon control) remains visible; the label and type
    entries are hidden while collapsed. When the sidebar is expanded, it shows the
    section headers, their totals, and the entries as specified in the rules above.
    The control is touch-friendly: it is a touch-friendly hit target that toggles on
    tap, consistent with the touch-friendly sidebar entries (rule 9) and the
    touch-friendly graph interaction (see
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)). The
    control uses the page's existing Tabler-based UI, consistent with the rest of
    the graph card (see [UI Framework](#ui-framework)). The collapse and expand
    control coexists with the other graph controls (rule 8): collapsing or expanding
    the sidebar changes only the sidebar's own visibility and the canvas width, and
    does not clear the active highlight selections, change the layout, run a search,
    or open or close the detail panel; an active highlight remains active while the
    sidebar is collapsed and is shown again, still active, when the sidebar is
    expanded. The sidebar's default state is **expanded**. Persistence of the
    collapsed or expanded state across page reloads is not specified; the only
    required behaviour is that each page load starts with the sidebar expanded.

### Graph Data Endpoint

- **Route:** `GET /roadmaps/{name}/graph/data`
- **Purpose:** Feeds the node-link visualisation. The page's JavaScript fetches
  this endpoint and hands the result to the vendored D3.js library, which renders
  it in the layout selected on the page (see
  [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)).
- **Response:** JSON describing the graph's nodes and edges, in the shape
  specified in `DATA_FORMATS.md § Graph View Data`. That shape reuses the
  graph-element and property-type conventions already defined in
  `DATA_FORMATS.md § Graph Query Result` (the node and relationship object shapes
  and the property-type-to-JSON mapping) rather than inventing a new element
  encoding. A request that fails carries the error object instead, specified in
  the same file (see the next bullet).
- **Failure responses.** The two ways a request to this endpoint fails — an
  invalid `limit`, and a statement that fails to execute — are each answered with
  HTTP `400 Bad Request` and a JSON body naming the failure's class in a `kind`
  field, in the shape specified in `DATA_FORMATS.md § Graph View Data`,
  **Error Shape**. The status, the `kind` values, the order in which the two are
  decided, and the boundary against the `500` of an internal read error are
  specified in [Query-Bar Error Handling](#query-bar-error-handling); this section
  does not restate them.
- **Query parameters.** The endpoint accepts two optional URL query parameters
  that the graph page's query bar (see
  [Graph Query Bar](#graph-query-bar)) sends, and that drive which Cypher query
  runs and how many results it returns:
  - `q` — the Cypher statement to run, URL-encoded. It is executed as written:
    the endpoint does not examine it and refuses nothing on the ground of what it
    does, so a statement that writes, deletes, or changes the schema is executed
    and committed like any other. When `q` is absent or empty, the endpoint runs
    the **default query**
    `MATCH (n) OPTIONAL MATCH (n)-[r]->(m) RETURN n, r, m`, which produces the same
    full-graph view (all nodes, plus all relationships, subject to the limit) the
    endpoint produced before the query bar existed. The endpoint is therefore
    backward compatible: a request with no `q` behaves exactly as the previous
    fixed full-graph read did.
  - `limit` — the node-limit value selected in the page's limit dropdown. When
    present it MUST be one of the six allowed values `50`, `100`, `250`, `500`,
    `1000`, or `3000`; when absent the endpoint applies the default limit `100`
    (matching the dropdown default). A `limit` value that is not one of the six
    allowed values is rejected as an invalid limit (see
    [Query-Bar Error Handling](#query-bar-error-handling)); the endpoint does not
    clamp an out-of-range value to the nearest allowed value, and the query is not
    executed.
- **The statement is executed as written (security-critical).** The endpoint
  performs no validation of `q` beyond the maximum query length that binds every
  Cypher statement (`GRAPH.md § Maximum Query Length`). It does not classify the
  statement, does not inspect the patterns it binds, and does not inspect the
  values it would write. A statement carrying `CREATE`, `MERGE`, `SET`, `REMOVE`,
  `DELETE`, `DETACH DELETE`, or any schema DDL is executed and committed.
- **The endpoint therefore runs on the transactional path.** It takes the graph
  store's exclusive lock before the open, constructs the same store-backed engine
  `rmp graph execute` constructs, runs the statement inside a transaction, and
  checkpoints and truncates the write-ahead log when that transaction wrote (see
  `GRAPH.md § Engine Constructor by Path`,
  `GRAPH.md § Synchronous Checkpoint on Write`, and
  `GRAPH.md § Concurrency and Recovery`). This is what makes a write submitted
  through the query bar real: an endpoint constructed without a transactional
  store would execute the same statement against the request's own in-memory
  graph, discard it when the request ended, and still answer `200`. The endpoint
  writes no audit entry and never touches a roadmap's `project.db`; the write path
  it now has reaches the graph store and nothing else.
- **No authentication stands between a caller and this behaviour.** The server
  authenticates nothing, and this endpoint is reachable by any client that can
  reach the bound address. `§ Security and Constraints` states the consequence in
  full and is canonical for it.
- **Node-limit injection.** The endpoint applies the resolved `limit` (the
  parameter value, or the default `100` when absent) by appending a top-level
  `LIMIT <n>` clause to the query. Injection is **suppressed** in exactly the two
  cases below, and applies in every other case. The default query has no `LIMIT`
  and is an ordinary reading query, so a request that uses the default query
  always has the resolved limit applied to it.
  - **Suppression 1: the query already carries a top-level `LIMIT`.** The user's
    own `LIMIT` takes precedence and is respected as-is: the endpoint injects
    nothing and the dropdown value is not applied. The presence-of-`LIMIT` check is
    performed on the **masked normalization** of the query (see
    `GRAPH.md § Literal-Aware Normalization`), so a `LIMIT` keyword that appears
    only inside a string literal, a comment, or a backtick-quoted identifier does
    not count as an existing top-level `LIMIT` and does not suppress injection.
  - **Suppression 2: the statement admits no `LIMIT` clause.** Not every statement
    the engine accepts can carry a `LIMIT` clause, and which ones can is decided by
    the grammar rather than by a list. **A `LIMIT` attaches only to a top-level
    projection, and only a `RETURN` or a `WITH` carries one, so a statement with no
    top-level `RETURN` admits no `LIMIT`.** That is the rule. The endpoint MUST
    inject nothing into such a statement and MUST execute it as the caller wrote
    it.

    The rule is stated as a rule and not as an enumeration of forms, because an
    enumeration is answerable only for the statements someone thought of. It
    reaches, among others, a **standalone procedure call**; and every **write with
    no projection** — a `CREATE`, a `MERGE`, a `SET`, a `REMOVE`, a `DELETE` or
    `DETACH DELETE`, and a schema DDL statement — which is the class an enumeration
    left out and which the endpoint could not execute at all while it injected into
    one. It reaches a form a future engine accepts on the same terms, without that
    form having to be foreseen here.

    **The boundary is the projection and nothing else.** A `CALL` projected through
    a top-level `RETURN` — the `CALL ... YIELD ... RETURN ...` form — is an
    ordinary reading query for this rule: it admits a `LIMIT` and receives the
    injection exactly as a `MATCH ... RETURN` does. A call carrying a `YIELD` but
    no top-level `RETURN` admits none. A write projected through a top-level
    `RETURN` receives the injection; the same write without one does not. The
    decision turns on the projection, never on what the statement does.

    **One class carries a top-level projection and still admits no `LIMIT`, so the
    rule above does not reach it and it is named here: a schema-introspection
    command.** `SHOW INDEXES`, `SHOW INDEX`, `SHOW CONSTRAINTS`, and
    `SHOW CONSTRAINT`, with or without a `YIELD`, `WHERE`, or `RETURN` tail, are
    refused a `LIMIT` by the engine's schema parser on every one of those forms, so
    a tail does not make one limitable. The endpoint executes such a statement like
    any other and returns its result through the same walk, which collects no node
    and no edge from it, so the response is `{"nodes": [], "edges": []}` with HTTP
    `200` (see [Query-Bar Error Handling](#query-bar-error-handling), rule 9). A
    schema listing is read from `rmp graph execute`, which returns the rows.

    **Why the suppression is required, and why it costs nothing.** Appending a
    `LIMIT` to a statement that admits none bounds nothing, and has one of two
    outcomes. Usually the statement fails in the **parser**, so a form that
    `rmp graph execute` runs would be unusable through this endpoint and the
    endpoint would be stricter than the contract it publishes. For a schema DDL
    statement it does not fail: the engine's schema parser stops when its grammar
    is satisfied and discards the appended clause silently
    (`GRAPH.md § What Groadmap Does Not Check`, item 6), so the statement runs and
    the injection vanishes. Neither outcome is one to rely on — the first breaks
    the statement and the second leaves the endpoint depending on a documented
    hazard to save it. Suppressing costs nothing in return: the node limit bounds
    the **result**, and a statement that projects nothing returns no row and
    contributes no node and no edge to the response, so there is nothing for a
    limit to bound.
  - **Recognising a statement that admits no `LIMIT`.** Both parts of the decision
    run on the **masked normalization** of the statement, exactly as Suppression 1
    does (see `GRAPH.md § Literal-Aware Normalization`), so a `RETURN` or `SHOW`
    keyword that appears only inside a string literal, a comment, or a
    backtick-quoted identifier does not affect it: a write whose only `RETURN` sits
    inside a property value is still a write with no projection.

    The general rule is decided by the **presence** of a `RETURN`, not by a parse,
    and it errs deliberately towards judging a statement limitable. A statement
    wrongly judged limitable keeps the node cap it would otherwise escape, and
    fails in the parser only in the exotic case where the `RETURN` it carries is
    confined to a subquery and the statement has no top-level projection of its
    own. Erring the other way would silently withdraw the cap from ordinary
    queries, which is the outcome the cap exists to prevent.

    The schema-introspection class is recognised **anchored to the start of the
    statement**, so a `SHOW` appearing inside a larger statement, and an
    identifier, label, or property named `show`, do not make a statement one of
    that class. Recognition there follows the engine's own routing, which admits
    exactly one space between the two keywords
    (`GRAPH.md § What Groadmap Does Not Check`, item 7): a statement written with
    any other separator is not routed to the engine's schema parser and will fail
    there as a syntax error, so injecting into it changes nothing about its
    outcome. This suppression refuses nothing; it decides only whether a `LIMIT`
    is appended.
  - **Separator: the injected clause begins on a new line.** When the endpoint does
    inject, it MUST separate the injected `LIMIT <n>` from the query with a
    **newline**, never with a space. A query whose last line ends in a line comment
    (`MATCH (n) RETURN n //`) swallows anything appended on that same line, so a
    space-separated injection lands **inside** the comment and the limit silently
    does not apply — the endpoint then returns the whole graph and the cap it
    exists to enforce is defeated. A newline terminates the comment, so the
    injected clause is always top-level and always applies. Cypher treats the
    newline as ordinary whitespace, so every query that worked before is
    unaffected.
  - **A suppressed statement is not bounded by the node limit.** Suppression means
    no `LIMIT` is applied, so the resolved limit does not cap these statements and
    the dropdown value has no effect on them. What still bounds them is the
    per-request time budget, which applies to every statement the endpoint executes,
    injected or not (see [Graph Query Time Budget](#graph-query-time-budget)): the
    budget bounds the **work**, the node limit bounds the **result**, and only the
    second is suppressed here.
- **Per-request query time budget.** The endpoint MUST execute the query under a
  5-second deadline derived from the request context, so a query that would run
  for longer is cancelled instead of holding the server for as long as it takes to
  finish. The budget bounds the **work** the query causes; the injected `LIMIT`
  bounds only the **result** it returns, and neither substitutes for the other. A
  statement cancelled for exceeding the budget is an execution failure and is
  surfaced as one (see
  [Query-Bar Error Handling](#query-bar-error-handling), case 2). The rule,
  including the reason for the value, is specified in
  [Graph Query Time Budget](#graph-query-time-budget).
- **Result-to-graph extraction.** The endpoint builds the
  `{"nodes": [...], "edges": [...]}` response (see
  `DATA_FORMATS.md § Graph View Data`) by walking the **entire** query result and
  collecting every node (`expr.Node`) and every relationship
  (`expr.Relationship`) value that appears **anywhere** in it: in any returned
  column, and recursively inside lists, maps, and paths. The walk is exhaustive
  and recursive, so a node or relationship nested inside a returned list, map, or
  path is collected exactly as one returned directly in its own column is.
  - **Deduplication.** Nodes are deduplicated by node `id` and relationships are
    deduplicated by relationship `id`, so a node or relationship that the query
    returns more than once (for example, the same node bound by several patterns,
    or a relationship that appears both standalone and inside a path) contributes
    exactly one entry to the response.
  - **Orphan-edge dropping.** A relationship is included only when **both** its
    start node and its end node are present in the collected node set. A
    relationship whose start or end node was not collected is **dropped**; the
    endpoint never invents a synthetic endpoint node to keep an edge. This
    guarantees the `startId`/`endId` invariant of the view-data shape: every
    `startId` and `endId` in the returned `edges` references the `id` of a node
    present in the returned `nodes` array (see `DATA_FORMATS.md § Graph View Data`,
    rule 3).
  - With the default query, this extraction yields the full-graph view: `MATCH
    (n)` collects every node, and the `OPTIONAL MATCH (n)-[r]->(m)` collects every
    relationship together with both of its endpoints, so no relationship is
    dropped as an orphan.
- **HTML-safe JSON.** The endpoint MUST emit HTML-safe JSON: HTML escaping MUST be
  enabled in the JSON encoder so that the characters `<`, `>`, and `&` are
  serialized as their Unicode escape sequences (`<`, `>`, and `&`).
  This ensures that graph node and edge labels or property values containing those
  characters cannot break out of a script or HTML context if the JSON is ever
  embedded in a page, and is consistent with the output-escaping rule in
  [Security and Constraints](#security-and-constraints).

### Static Assets

- **Route:** `GET /static/...`
- **Content:** The embedded stylesheet, the embedded client scripts, and the
  vendored D3.js graph library (with the d3-sankey plugin). These are served only
  from the embedded asset set. The static handler MUST serve only embedded assets
  and MUST NOT map a
  request path to an arbitrary path on the host filesystem. A request for an asset
  that is not in the embedded set is answered with HTTP `404 Not Found` (see
  [Security and Constraints](#security-and-constraints)).
- **No directory listings.** The static handler MUST NOT serve a directory
  listing. A request for a directory path under `/static/` (for example
  `/static/` or `/static/vendor/`) is answered with HTTP `404 Not Found`, never
  with an index or a listing of the directory's contents. A request for an
  individual asset file that exists in the embedded set is served normally with
  HTTP `200 OK`. This prevents the embedded asset tree from being enumerated
  through the server.

### Task Detail Modal

The task detail modal is a popup overlay that displays the full set of fields for
one task. It is not a separate route; it is part of the pages that show clickable
tasks.

- **Where it appears.** Anywhere a task is shown clickable: the task cards on the
  roadmap tasks page's Kanban board and the task cards on the roadmap sprint page's
  member-tasks board. The roadmap sprints page shows no clickable tasks, because
  every sprint there is rendered as a card with no member tasks on it. Selecting a
  task opens the modal for that task.
- **The trigger is a natively activatable element.** Every element that opens the
  modal MUST be a `<button>`, so that a pointer click, a touch tap, Enter, and
  Space all open the modal through the browser's own activation behaviour, with no
  added JavaScript. This is the property every surface that shows a clickable task
  shares, and it holds for each surface on its own terms: the surfaces are not
  defined by reference to one another, and no surface satisfies it by copying
  another's markup.

  A `<button>` is the applicable element, not a link. A link is activatable only
  when it carries an `href`, and the modal is not a route: it has no URL to point
  at (see [Routes and Pages](#routes-and-pages)). A link also answers Enter alone,
  where a button answers Enter and Space, so choosing a button is what makes the
  same keyboard contract hold identically on every surface.

  A non-interactive element made to look interactive — a `<div>` or a `<tr>`
  carrying `role="button"` and `tabindex="0"` — MUST NOT be the trigger.
  `tabindex` grants focus and `role` announces the element as a button, but
  neither grants activation, so such an element takes focus and announces itself
  as a button that cannot be pressed. The vendored framework does not close that
  gap: it binds its modal trigger behaviour to the click event only and registers
  no key handler for a trigger, and adding one is not available to this interface,
  because the Content-Security-Policy in [Security Headers](#security-headers)
  admits script only from `/static/` and [Frontend Rules](#frontend-rules) allow
  no inline script. The trigger therefore has to be an element that is activatable
  to begin with.

  Each trigger carries an **accessible name that names the action and identifies
  the task by both its `id` and its `title`**. The name is
  `Open details for task #<id>: <title>`, so a user reaching the trigger without
  sight of the surrounding layout knows both what the control does and which task
  the modal will show.

  Where the trigger has a **visible text label**, the accessible name MUST contain
  that visible label text. This is the case on both boards, whose card carries the
  task title as its own visible text: on the Roadmap Sprint Page the title leads
  the card, and on the roadmap tasks page it is the card's prominent main content.
  Including the `title` in the name is what satisfies the rule on each of them. A
  name that omits the visible label breaks activation by speech
  input: a speech-input user says the words they can see, and a control whose
  accessible name does not contain them cannot be activated that way, even though
  it reads correctly to a screen reader. This requirement is the one stated by
  WCAG 2.5.3 Label in Name (Level A), cited here as the grounding for this rule;
  it is the reason the name carries the `title` and not the `id` alone.
- **Fields shown.** The modal displays all of the task's fields as defined for the
  `Task` model in `MODELS.md § Task`: `id`, `title`, `status`, `type`, `priority`,
  `severity`, `functional_requirements`, `technical_requirements`,
  `acceptance_criteria`, `completion_summary`, `parent_task_id`, `subtask_count`,
  `depends_on`, `blocks`, `created_at`, `started_at`, `tested_at`, `closed_at`,
  `commit_open`, and `commit_close`. The two commit hashes are short single-line
  values, and the modal presents each as plain text alongside the lifecycle
  timestamps, under the same rules as every other short field it shows. The modal
  adds no link to any code-hosting service and no copy control for them: it is
  read-only and offline, and it holds no repository URL from which such a link
  could be built.
  This includes the long free-text fields
  (`functional_requirements`, `technical_requirements`, `acceptance_criteria`, and
  `completion_summary`), which the modal presents formatted for readable display.
  These long free-text fields are multi-line as authored through the CLI, and the
  modal renders them preserving the author's line breaks (newlines); the text
  still wraps within the modal, so no forced horizontal scrolling is introduced
  (see [Frontend Rules](#frontend-rules), rule 6). The page does not redefine
  these fields; `MODELS.md` and `DATABASE.md` remain canonical. On the roadmap
  tasks page the modal is the sole place a task's full field set is shown, because
  the board card presents only the subset defined in
  [Roadmap Tasks Page](#roadmap-tasks-page).
- **No prefix on the modal's priority and severity badges.** The one-letter prefix
  the board card's `priority` and `severity` badges carry (see
  [Roadmap Tasks Page](#roadmap-tasks-page), **Card content**, item 3) belongs to
  the card and is not rendered here. That prefix exists because a card shows the two
  values with no word naming either of them; the modal names every field it
  displays, so the field's own name already stands beside each of these two values,
  and a prefix would state the same thing twice. A prefix earns its place only where
  no label names the value, which is true of the board card and of no other surface
  in this interface. The badge colours are the same either way and stay those of
  [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
  because the mapping keys on the value and never on the badge's prefix.
- **Comments timeline.** Directly after the completion-summary block, and as the
  last block of the modal body, the modal renders the task's comments as a
  chronological timeline. The fields of a comment are defined for the
  `TaskComment` model in `MODELS.md § Task Comment`; the modal does not redefine
  them.
  - **Order.** Oldest first, exactly the order `task comment-list` returns
    (`created_at` ascending, comment `id` ascending as the tie-breaker; see
    `DATABASE.md § Comments`). The timeline is a log, and the order is what makes
    it readable as one.
  - **Completeness.** Every comment of the task is rendered. The modal applies no
    type filter and no count limit.
  - **What each entry shows.** For one comment, in order: its `type` as a badge,
    its `created_at` timestamp, its `updated_at` timestamp when that value is not
    null (marking the entry as edited), and its `body`.
  - **Markup.** The timeline uses Tabler's Timeline component, which the vendored
    `tabler.min.css` already provides (see
    [Embedded Asset Categories](#embedded-asset-categories)); the feature adds no
    asset. The structure is an unordered list `<ul class="timeline">` whose items
    are `<li class="timeline-event">`, each containing a
    `<div class="timeline-event-icon">` holding a Tabler icon
    (`<i class="ti ti-message"></i>`) and a
    `<div class="card timeline-event-card">` whose `card-body` carries the
    timestamps, the type badge, and the body text.
  - **Type badge colour.** The comment type renders as a neutral Tabler badge,
    `bg-secondary-lt`, for every one of the seven type values. The semantic colour
    mapping in
    [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours)
    covers task and sprint status, priority, and severity only; it is not extended
    to comment types, and no per-type colour is introduced.
  - **Authored line breaks.** A comment body is multi-line as authored through the
    CLI. The timeline renders it preserving the author's line breaks, and the text
    wraps within the card, exactly as the long free-text fields above do (see
    [Frontend Rules](#frontend-rules), rule 6).
  - **Empty state.** When the task has no comments, the modal shows a clear
    empty-state message in place of the timeline rather than an empty list or an
    absent section.
- **Read-only.** The modal only displays data. It contains no form, no input, no
  edit control, and no submit action of any kind. This includes the comments
  timeline: comments are displayed, never created, edited, or deleted from the
  web interface.
- **One modal element, filled on demand.** A page that shows clickable tasks
  renders **one** modal element, not one per task. That element is an empty shell:
  it carries no task's data until a user opens a task. When the user opens one, the
  page's script fetches that task's data from
  `GET /roadmaps/{name}/tasks/{id}/data` (see
  [Task Detail Endpoint](#task-detail-endpoint)) and fills the shell with it. The
  document the server sends therefore carries the modal's markup once, and its size
  does not grow with the number of tasks the page shows. A user opens one task at a
  time, and a task the user never opens is never fetched.
- **Client-side rendering is text-only (security-critical).** Because the task's
  values now reach the browser as JSON rather than as server-rendered HTML, the
  server's `html/template` contextual auto-escaping no longer stands between a
  stored value and the page structure: the responsibility moves to the script that
  fills the modal. Therefore **every** value the script writes into the DOM MUST be
  written through the DOM `textContent` property, or an equivalent that cannot
  interpret markup. The script MUST NOT use `innerHTML`, MUST NOT use
  `insertAdjacentHTML`, and MUST NOT build DOM by assigning a string that embeds a
  value to any markup-parsing sink.

  This governs every caller-authored value on this path, all of which are free text
  a user wrote through the CLI: the task `title`, `functional_requirements`,
  `technical_requirements`, `acceptance_criteria`, `completion_summary`, and every
  comment `body`. A value containing HTML control characters MUST render as the
  characters themselves and MUST NOT be able to
  introduce an element, an attribute, or a script into the page. The
  control-character constraint in `MODELS.md § Task` rejects terminal and
  bidirectional control characters at write time; it does not reject HTML markup,
  so it is not a substitute for this rule.
- **Failure is visible in the modal.** The modal already depends on JavaScript,
  because the vendored framework is what opens it. When the fetch for a task's data
  fails — a network error, a non-200 response, or a body that does not parse — the
  modal MUST open and show a clear error message in place of the task's content,
  naming that the task's detail could not be loaded. It MUST NOT stay blank, MUST
  NOT close silently, and MUST NOT leave the previously opened task's data on
  display. The failure is a read failure and offers no retry that writes anything.
- **No new write path.** The endpoint the modal fetches is read-only and serves
  `GET` and `HEAD` only; the modal introduces no write path, and the CLI remains
  the sole write path (see [Task Detail Endpoint](#task-detail-endpoint) and
  [Security and Constraints](#security-and-constraints)).
- **Popup and touch usability.** The modal is a popup overlay (for example a
  Tabler or Bootstrap modal) rendered inside the Tabler admin shell. It MUST be
  usable on touch input and on small viewports: it fits the viewport without
  horizontal overflow, scrolls its content when the task's text is long, and
  offers touch-friendly controls to open and dismiss it (see
  [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).

### Task Detail Endpoint

- **Route:** `GET /roadmaps/{name}/tasks/{id}/data`
- **Purpose:** Feeds the task detail modal. The page holds one empty modal shell,
  and the page's JavaScript fetches this endpoint when the user opens a task,
  filling that shell with the returned task's fields and comments (see
  [Task Detail Modal](#task-detail-modal)).
- **Path shape.** The endpoint follows the one JSON-endpoint convention this
  interface already has: the graph page is served at `/roadmaps/{name}/graph` and
  its JSON at `/roadmaps/{name}/graph/data` (see
  [Graph Data Endpoint](#graph-data-endpoint)). The `/data` suffix is what marks a
  path as a JSON payload rather than an HTML page, which keeps the bare
  `{collection}/{id}` shape reserved for the HTML-page idiom that
  `/roadmaps/{name}/sprints/{id}` uses. `/roadmaps/{name}/tasks/{id}` is therefore
  not a route and is answered `404 Not Found`. The endpoint is scoped to a task of
  a roadmap rather than to a page, so both surfaces that show a clickable task —
  the tasks page's board and the Roadmap Sprint Page's member-tasks board — use
  this same endpoint.
- **Response:** JSON carrying the task's fields and its comments, in the shape
  specified in `DATA_FORMATS.md § Task Detail Data`. That shape **composes** the
  object shapes already defined in `DATA_FORMATS.md § Task` and
  `DATA_FORMATS.md § Task Comment` (whose fields are defined for the `Task` and
  `TaskComment` models in `MODELS.md § Task` and `MODELS.md § Task Comment`) rather
  than introducing a new object encoding, so a value carries the same field names,
  the same types, and the same null conventions here as it does in CLI output.
  `DATA_FORMATS.md` is canonical for the response shape, including the ordering of
  the `comments` array (oldest first, the order the modal's timeline presents) and
  the `[]`-not-`null` convention for a task with no comment; this file does not
  restate them.
- **Path parameters.** `{name}` and `{id}` follow the discipline in
  [Routes and Pages](#routes-and-pages), rules 1, 2, and 4: `{name}` is validated
  against the roadmap-name rules before any filesystem path is built, and an
  invalid or nonexistent `{name}`, a non-integer `{id}`, or an `{id}` that is not a
  task of the named roadmap each return HTTP `404 Not Found`. The 404 for a task of
  another roadmap is what keeps a roadmap's data reachable only through its own
  path space.
- **Methods.** `GET` and `HEAD` only. Any other method is answered HTTP
  `405 Method Not Allowed`, exactly as on every other route (see
  [Functional Requirements](#functional-requirements), requirement 4).
- **Read-only.** The endpoint reads the roadmap's `project.db` through the same
  read-only open path the pages use, writes nothing, and exposes no write path. It
  produces no audit entry, because a read is not a change (see
  [Tasks and Sprints from SQLite](#tasks-and-sprints-from-sqlite)). The `rmp` CLI
  remains the sole write path.
- **Reads.** One read for the task and one for that task's comments, for the single
  task requested. The endpoint is requested only when a user opens a modal, so it
  is not on the page-rendering path and does not reintroduce a per-task query into
  page rendering (see [Roadmap Tasks Page](#roadmap-tasks-page), **Read cost**).
- **Cache policy.** The response is data-derived and therefore carries
  `Cache-Control: no-store` from the existing header treatment, like every other
  data-derived response (see [Cache Policy](#cache-policy)).
- **Security headers and Content-Security-Policy.** The endpoint requires **no**
  change to the Content-Security-Policy. The policy specified in
  [Security Headers](#security-headers) already admits `connect-src 'self'` and
  `script-src 'self'`, which is what permits a same-origin fetch driven by a script
  served from `/static/`. The graph page already fetches its data this way, so
  runtime fetch is an established pattern of this interface and not an exception
  made for this endpoint. No inline script is introduced (see
  [Frontend Rules](#frontend-rules)).
- **Output encoding.** The response body is JSON-encoded, never HTML. Task and
  comment text is carried as JSON string values and is never interpolated into
  markup by the server. How the client renders those values is
  security-critical and is specified in [Task Detail Modal](#task-detail-modal),
  **Client-side rendering is text-only**.

## Read-Only Data Flow

The web interface reads the same on-disk data the CLI reads, through the same
location rules, and never writes to it. Each request opens the data, reads the
current state, and releases the handle; the freshly read state is what the user
sees, and the `Cache-Control: no-store` header on every data-derived response
(see [Cache Policy](#cache-policy)) ensures no client-side or intermediary cache
re-presents an earlier, now-stale response in its place.

### Tasks and Sprints from SQLite

1. For a roadmap sprints request, a roadmap tasks request, a roadmap sprint
   request, or a roadmap audit log request, the server resolves the roadmap's
   database at
   `~/.roadmaps/{name}/project.db` (see `ARCHITECTURE.md § Directory Structure`)
   and reads its sprints, tasks, and audit entries using the existing read queries
   defined in
   `DATABASE.md § Main SQL Queries`. The sprints page reads the roadmap's sprints
   and each sprint's total task count for its card footer, but no member tasks,
   because the page renders every sprint as a card with no member tasks on it; the
   tasks page reads the roadmap's full task list, which its Kanban board then
   groups into the five status columns in memory, with no further query — none per
   column and none per card (see [Roadmap Tasks Page](#roadmap-tasks-page)); the
   sprint page reads that sprint and its member tasks in `sprint_tasks` position
   order, which its own board then groups into the three columns and orders in
   memory — the `WAITING` column keeping the position order the read returned, the
   `DOING` and `CLOSED` columns reordered by `started_at` and `closed_at`
   descending — again with no further query per column and none per card (see
   [Sprint Detail Sub-Template](#sprint-detail-sub-template)); the
   audit log page reads the
   roadmap's audit entries ordered by `performed_at` descending, one fixed-size page
   at a time (see [Roadmap Audit Log Page](#roadmap-audit-log-page) and
   `DATABASE.md § Audit Queries`).
   The web interface adds no new schema, no new table, and no new write query.
   The data the task detail modal displays is **not** read while the page is
   rendered: the page carries one empty modal shell, and the task's fields and
   comments are read only when a user opens that task, by the read-only task detail
   endpoint (see [Task Detail Endpoint](#task-detail-endpoint)). A page that shows
   clickable tasks therefore reads, at render time, only what it displays itself:
   both boards read a comment **count** per rendered task, in one grouped counting
   query over the whole set of rendered task ids, because a card shows a count and
   no comment text (see
   `DATABASE.md § Count Comments for Many Parents (Grouped)`). On the tasks page
   that grouped count is the page's only comment read. The Roadmap Sprint Page
   additionally presents the sprint's own comment log, so it reads that sprint's
   comments in full in one further query (see `DATABASE.md § Comments`): the sprint
   page therefore issues exactly **two** comment reads — the sprint's own listing
   and the one grouped count over its member tasks — whatever the number of member
   tasks, and it reads the comment **body** of no member task. The grouped count is
   skipped entirely when the sprint has no member task, because it takes a set of
   rendered task ids and that set is empty; the sprint's own comment listing is
   still issued, because the Comments card is always present. Every one of these is
   a read query issued server-side while the page
   is rendered, and the number of them per page does not grow with the number of
   tasks shown. The tasks page issues one further grouped query,
   which resolves the sprint of every task it renders over the whole set of
   rendered task ids at once, so each board card can name the sprint its task
   belongs to (see `DATABASE.md § Resolve the Sprint of Many Tasks (Grouped)`).
   That query is issued once per page, never once per task and never once per
   board column, and it is skipped entirely when the page renders no task. The
   sprint page issues no sprint-resolution query at all: every card on its board
   belongs to the one sprint the page is showing, so there is nothing to resolve.
2. The server opens the database for reading only. It MUST NOT modify rows, MUST
   NOT write an audit entry, and MUST NOT alter the schema. A web read produces no
   audit-log entry, because the audit log records changes and a read is not a
   change (see `DATABASE.md § audit Table`). In particular, a per-request read
   MUST NOT run a schema migration: the read-only open path opens the database
   with SQLite `query_only` set, so it can never rewrite a stale-schema database.
   The schema is brought to the current version once, at startup, before any
   read-only connection is opened (see
   [Startup Schema Migration](#startup-schema-migration)); the startup migration
   is the only path on which the web interface writes to a roadmap database, and
   it is the only place the schema is altered. Restricting the database file's
   permissions to `0600` is not a write in this sense and is the one filesystem
   change the read-only open path may make: it alters no row, no audit entry, and
   no schema, and it only ever tightens the mode. A database whose permissions
   cannot be brought to `0600` is not served; the rule, including that refusal, is
   `ARCHITECTURE.md § Open-Time Permission Enforcement`.
3. Each request opens the database, reads what it needs, renders the page, and
   releases the handle. Concurrency against SQLite follows the existing model in
   `IMPLEMENTATION.md § Concurrency Model`; a web read is an ordinary reader and
   does not change the CLI's write behaviour.

### Knowledge Graph from the GoGraph Store

1. For a graph page or graph data request, the server resolves the roadmap's
   graph store at `~/.roadmaps/{name}/graph/` (see `GRAPH.md § Persistence
   Layout`), opens it, and runs the statement the same way `rmp graph execute`
   does (see `GRAPH.md § Engine Construction and Lifecycle`). There is one
   execution path and the server is on it, so it opens a transactional store and a
   write-ahead-log writer. Which constructor that is, is fixed by
   `GRAPH.md § Engine Constructor by Path`, and this specification does not
   restate it.
2. The server runs the statement the request carries, or the default query when
   the request carries none. It does not examine that statement, so the statement
   may write.
3. **The server therefore writes graph data when the statement it was given
   does.** The transaction commits, and the synchronous checkpoint and
   write-ahead-log truncation follow exactly as they do for a CLI invocation (see
   `GRAPH.md § Synchronous Checkpoint on Write` and
   `IMPLEMENTATION.md § Graph Store Concurrency`). When the statement's
   transaction appended nothing to the write-ahead log, the log is left byte for
   byte as the request found it and the contents of `snapshot/` are left
   unchanged. The server writes no audit entry in either case, and it never writes
   to a roadmap's `project.db` outside the startup migration (see
   [Read-Only Data Flow](#read-only-data-flow)).
4. A request that writes nothing is still **not** free of on-disk effect, and this
   specification does not claim that it is. Opening the store runs GoGraph's
   recovery, which restores the last committed state from the snapshot and the
   write-ahead-log tail, and which first repairs an interrupted checkpoint: it
   removes a stale `snapshot.tmp` staging directory, and it promotes
   `snapshot.bak` to `snapshot` when the live snapshot directory carries no
   manifest. A graph data request can therefore change the store directory's
   structure without changing its data. The exhaustive list is
   `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`, which is
   canonical and applies to this endpoint unchanged.
5. The server takes the graph store's advisory lock **exclusively** before
   opening the store, and holds it across the open, the statement, any commit, and
   any checkpoint, exactly as `rmp graph execute` does. The lock, its single mode,
   and its contention policy are specified in `GRAPH.md § Concurrency and
   Recovery`; that section is canonical and this one adds no rule of its own.
   Three consequences are specific to the web interface and are stated here:
   - A graph data request may **wait** for an in-flight `rmp graph execute`
     invocation, or for another graph data request, against the same roadmap. The
     wait is bounded (see `GRAPH.md § Lock Contention`), and it is spent before
     the statement starts and so does not consume the endpoint's query time
     budget (see [Graph Query Time Budget](#graph-query-time-budget)). It is
     sized against the longest hold this endpoint may lawfully take, one whose
     statement runs to the end of that query time budget, so the wait and the
     statement together stay well inside the server's write timeout (see
     [HTTP Server Timeouts](#http-server-timeouts)): it is the two together that
     have to fit, not the wait alone. `GRAPH.md § Lock Contention` fixes the
     sizing rule and that invariant. An `rmp graph execute` invocation is bounded
     by the same statement budget this endpoint applies (see
     `GRAPH.md § Statement Time Budget`), so a CLI hold cannot lawfully outlast
     the wait either. Two limits remain, both stated in
     `GRAPH.md § Lock Contention` rather than here: the allowance for the fixed
     part of a hold is exhausted on a large enough graph, and no finite wait can
     be sized against a long-lived server that holds the lock for its process
     lifetime. When the wait is exhausted for either reason, the request is
     answered as the next consequence describes. A request MUST NOT block
     indefinitely on the lock.
   - A request that still cannot take the lock when the bounded wait is exhausted
     is answered HTTP `500`, the status this endpoint already returns for a graph
     store that cannot be opened (see [Routes and Pages](#routes-and-pages)). It is
     logged like any other `500` (see [Server Logging](#server-logging)).
   - Serving a graph data request **does** block the CLI, and another graph data
     request, for the duration of that request. The hold now spans the statement's
     own execution, so a slow statement submitted through the query bar delays
     every other statement against the same roadmap until it finishes or its time
     budget expires. Two graph pages open on the same roadmap serialise on this
     lock. This is a consequence of the single lock mode and is stated so that it
     is met here rather than in production.
6. Each request opens the store, runs its statement, serves the result, releases
   the lock, and closes the store. The server does not hold the graph store open,
   or its lock, across requests, consistent with the short-lived-access model in
   `IMPLEMENTATION.md § Graph Store Concurrency`. A graph store that is corrupt or
   unreadable surfaces as an internal read error (HTTP 500 on the affected route);
   there is no automatic graph-store repair.

## Frontend and Embedded Assets

### Self-Contained Deliverable

The shipped deliverable is the single `rmp` binary, and that binary alone MUST be
sufficient to render and operate the web interface. This is a hard requirement,
not a convenience.

1. **Everything is embedded.** Every component required to render and operate the
   interface is embedded into the binary at build time with `go:embed`. The full
   list of asset categories is enumerated in
   [Embedded Asset Categories](#embedded-asset-categories), so "all components"
   is unambiguous: nothing the interface needs is left outside the binary.
2. **Zero external runtime dependency.** The interface requires no runtime
   dependency beyond the binary itself: no separate assets directory, no sidecar
   file, no companion package, no external service, and no JavaScript build
   toolchain (see `BUILD.md § Vendored Web Assets`).
3. **No network fetch at runtime.** No asset is fetched from the network when the
   interface runs. No page references a content delivery network, Google Fonts or
   any other remote font, script, or style host, or an external API. The running
   server makes no outbound network request of its own.
4. **Fully offline.** The interface renders and functions fully offline, with
   networking disabled and with only the single `rmp` binary present on disk.
   This property is build-verifiable (see
   [Acceptance Criteria](#acceptance-criteria) and
   `BUILD.md § Vendored Web Assets`).
5. **Served only from the embedded filesystem.** Every asset is served exclusively
   from the embedded asset set under the `/static/...` route. The server never
   reads an asset from the real filesystem, consistent with the path-traversal and
   no-arbitrary-file-serving constraint in
   [Security and Constraints](#security-and-constraints).

### Embedded Asset Categories

Every asset category below is embedded into the binary with `go:embed` and served
only from the embedded asset set. This enumeration defines the complete set of
asset categories the binary must carry; no category is fetched from the network or
read from the host filesystem at runtime.

1. **HTML templates** — the `html/template` set that renders every page.
2. **Stylesheet** — all CSS, including the vendored Tabler CSS framework (the UI
   framework, see [UI Framework](#ui-framework)) and any further vendored CSS the
   interface uses (see
   [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
3. **JavaScript** — all client scripts, including the Tabler JavaScript (the UI
   framework's scripts) and the D3.js knowledge-graph visualisation library (and
   the d3-sankey plugin) and any of their dependencies, all in already-built
   (vendored) form.
4. **Web fonts** — every font the interface uses, including the Inter font and
   the Tabler Icons webfont; no font is loaded from a remote font host.
5. **Icons and images** — any icon or image the interface displays, including the
   Tabler Icons set.
6. **Favicon** — the site favicon.
7. **Any other static asset** — any further static asset the interface requires
   is embedded under the same rule; no static asset is exempt.

### Frontend Rules

1. **Server-rendered HTML.** Pages are rendered with Go's `html/template`. The
   template set is embedded into the binary at build time with `go:embed`.
   `html/template` performs contextual auto-escaping, which is the primary
   defence against injecting roadmap-derived text (task titles, descriptions,
   graph property values) into the page (see
   [Security and Constraints](#security-and-constraints)).
2. **Embedded static assets.** Every asset category in
   [Embedded Asset Categories](#embedded-asset-categories) is embedded with
   `go:embed` and served from the `/static/...` route. There is no separate asset
   directory on disk at runtime and no asset is read from the host filesystem.
3. **No build toolchain.** The frontend uses no JavaScript build step, no
   `node_modules`, and no package manager at build time. Any JavaScript library
   the interface uses is committed to the repository in already-built form
   (vendored) and embedded directly.
4. **Responsive viewport.** Every HTML page includes the responsive viewport meta
   tag so the interface scales correctly on mobile devices (see
   [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
5. **No content delivery network and no external network calls.** No page
   references a script, stylesheet, font, or image from a remote origin. Every
   asset a page loads is served from `/static/...` on the same local server. The
   running server makes no outbound network request of its own (see
   [Self-Contained Deliverable](#self-contained-deliverable)).
6. **Authored line breaks preserved in multi-line free-text.** Free-text that a
   user authored through the CLI is multi-line: the user enters line breaks
   (newlines) in it. Where the interface renders such authored free-text — the
   task long free-text fields (`functional_requirements`,
   `technical_requirements`, `acceptance_criteria`, and `completion_summary`) in
   the task detail modal, a sprint's `description` wherever it is shown, and the
   property values shown in the knowledge-graph detail panel when a node or edge
   is selected — the interface preserves the author's line breaks rather than
   collapsing them under HTML's default whitespace handling. The text still wraps
   within its container, so preserving line breaks introduces no forced
   horizontal scrolling, and the text is still emitted as the element's text
   content (never as raw HTML): the server-rendered fields through
   `html/template`'s contextual auto-escaping (rule 1), and the graph detail panel
   values through the DOM `textContent` property. This rule is the general
   statement of the behaviour; the [Task Detail Modal](#task-detail-modal),
   [Roadmap Sprints Page](#roadmap-sprints-page),
   [Roadmap Sprint Page](#roadmap-sprint-page), and
   [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page) sections
   reference it.

### UI Framework

1. The web interface is built on **Tabler**, the admin-dashboard CSS and
   JavaScript framework (built on Bootstrap). Tabler provides the admin-shell
   layout — the navigation sidebar, the top navbar, page headers, and the cards,
   tables, badges, and buttons used across every page. The sidebar's per-roadmap
   links resolve to the roadmap's four views — Sprints at `/roadmaps/{name}` (the
   landing page), Tasks at `/roadmaps/{name}/tasks`, Audit at
   `/roadmaps/{name}/audit`, and Graph at
   `/roadmaps/{name}/graph` — and the sidebar highlights whichever of these is the
   active view. Tabler also provides the tabs used for the sprint presentation on
   the roadmap sprints page and the modal used for the task detail popup.
2. The interface uses Tabler's **dark theme**.
3. Tabler is **vendored**: its already-built distribution (the compiled Tabler
   CSS and JavaScript) is committed to the repository under the web asset set and
   embedded into the binary with `go:embed`. It is served locally from
   `/static/...`. It is never loaded from a content delivery network or any
   remote origin.
4. The fonts and icons the Tabler shell depends on are likewise vendored and
   served from `/static/...`: the **Inter** font and the **Tabler Icons** webfont
   are committed font files, embedded with `go:embed`, and loaded only from
   `/static/...`. No font is loaded from a remote font host such as Google Fonts
   (see [Embedded Asset Categories](#embedded-asset-categories) and
   [Self-Contained Deliverable](#self-contained-deliverable)).
5. Tabler is itself responsive and mobile-first; the admin-shell navigation
   sidebar collapses to an off-canvas (hamburger) menu on small viewports, so the
   pages stay usable without horizontal overflow on phones (see
   [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
6. The knowledge-graph visualisation, rendered with D3.js, is displayed inside the
   Tabler shell (see
   [Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library)).
7. The choice of Tabler, and of its dark theme, is recorded here so the SPEC is
   unambiguous about which UI framework is vendored; substituting a different UI
   framework, or changing the theme, is a SPEC change to this subsection and to
   `BUILD.md § Vendored Web Assets`, not a silent code change. No version number
   is pinned here; the vendored Tabler version lives in the committed distribution
   under git.
8. **Faithful Tabler fidelity.** Every web template faithfully follows the
   official Tabler examples, adapted only to the project domain (the read-only
   roadmap, sprint, task, audit, and graph pages). When a template needs a
   component that Tabler already provides — cards, card tabs, page headers,
   tables, pagination, badges, empty states, the navigation sidebar, the modal —
   the template starts from the closest official Tabler example and reuses its
   class and structure idioms, adapting only the data and labels to the roadmap
   domain. A template MUST NOT hand-roll a component Tabler already provides, and
   MUST NOT diverge from the Tabler example's markup structure where Tabler offers
   a direct equivalent. This fidelity rule applies to every template in the web
   asset set. The specific fidelity requirements that follow from this principle —
   card tabs (rule 9), semantic status badges (see
   [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours)),
   no presentational inline styles and no class the vendored distribution does not
   ship (rule 10), the minor markup-fidelity adjustments (rule 11), the admin-shell
   element order (rule 12), the single sidebar collapse, toggler, and brand
   (rule 13), `aria-current` on the active navigation link (rule 14), the labelled
   pagination wrapper (rule 15), the page-header actions column (rule 16), the
   fluid-layout container idiom (rule 17), the `main` page-body landmark
   (rule 18), and the top navbar's content (rule 19) — are concrete applications
   of it.
9. **Card tabs follow Tabler's "card with tabs" example.** The Roadmap Sprints
   Page tab control (the three tabs Próximos, Actual, Concluídos; see
   [Roadmap Sprints Page](#roadmap-sprints-page)) follows Tabler's "card with
   tabs" example exactly: the tab list is a single
   `<ul class="nav nav-tabs card-header-tabs" data-bs-toggle="tabs" role="tablist">`
   placed **inside** the card's `card-header`. The page MUST NOT instead put a card
   title in the header with the `nav-tabs` list in the card body; the tab list lives
   in the card header as the Tabler example shows. Tab activation uses Bootstrap's
   native tabs behaviour through the `data-bs-toggle="tabs"` attribute (Tabler is
   built on Bootstrap; see rule 1), not a hand-rolled show/hide script. Each tab's
   trigger is a Tabler `nav-link` (`<a class="nav-link" data-bs-toggle="tab">`), and
   the **Actual** tab is the one carrying the `active` state on page load. The three
   tabs and their count badges, and the default-active Actual tab, are preserved
   exactly as specified in [Roadmap Sprints Page](#roadmap-sprints-page), including
   the semantic colour each count badge carries there.
10. **No presentational inline styles, and no class the vendored Tabler
    distribution does not ship.** Templates MUST NOT carry presentational inline
    `style="..."` attributes. All styling lives in the vendored Tabler classes and
    utilities, or in the project override stylesheet (`static/style.css`), served
    from `/static/...` (see
    [Embedded Asset Categories](#embedded-asset-categories)). In particular, the
    navigation sidebar's section label and the empty-state icon sizing carry no
    inline `style`. The sidebar's per-roadmap section label is a Tabler
    `subheader` — the small uppercase letter-spaced muted label the vendored
    distribution defines — and the rule above it is a Tabler `dropdown-divider`.
    The label is aligned with the sidebar links by a Tabler spacing utility
    (`px-3`, the same 1rem horizontal padding Tabler gives a vertical-navbar
    `nav-link` at the viewport widths where the sidebar is expanded), never by a
    project stylesheet rule. Any presentational sizing, such as the empty-state
    icon's dimensions, lives in a Tabler utility class or in `static/style.css`.

    A template MUST use only class names the vendored Tabler distribution actually
    provides. `navbar-heading` and `navbar-divider` are not Tabler class names —
    the vendored distribution defines neither — and MUST NOT appear in any
    template. The project stylesheet MUST NOT carry a rule whose selector targets a
    framework class the vendored distribution does not define: such a rule does not
    override Tabler, it re-creates a component Tabler never shipped, which is the
    divergence rule 8 forbids. When a template appears to need such a rule, the
    template is wrong and is brought back to the Tabler class that already provides
    the behaviour. `static/style.css` remains the place for project-specific
    styling that no Tabler class covers.

    Keeping presentation out of the templates this way keeps the markup faithful to
    the Tabler examples and is consistent with the Content-Security-Policy in
    [Security Headers](#security-headers) (which already permits the framework's own
    `style-src 'unsafe-inline'` for Tabler, while the project's own styling stays in
    the stylesheet).
11. **Minor markup-fidelity adjustments.** The templates follow Tabler's markup
    idioms in these specific places, as markup-fidelity adjustments that change
    neither the read-only nature of the interface nor the content shown:
    - **Page-header rows** use Tabler's `row g-2 align-items-center` gutter and
      alignment classes, as the Tabler page-header example does.
    - **The sidebar brand** uses the Tabler `<h1 class="navbar-brand
      navbar-brand-autodark">` element, as the Tabler vertical-navbar example does.
    These adjustments only align the markup with the Tabler examples; they introduce
    no new page, no new content, and no write path, and the pages remain read-only.
12. **Admin-shell element order.** Tabler places the top navbar as a direct child
    of the page container: in the official page-layout examples, and in Tabler's own
    built admin shell, `<header class="navbar navbar-expand-... d-print-none">` is a
    **sibling** of `<div class="page-wrapper">` inside `<div class="page">`, never a
    descendant of it. The vendored stylesheet depends on that shape: its
    `.navbar-expand-lg.navbar-vertical~.navbar` and
    `.navbar-expand-lg.navbar-vertical~.page-wrapper` rules give the top navbar and
    the page wrapper the 15rem offset that clears the vertical sidebar, and a
    general sibling selector matches only elements that follow the `<aside>` at the
    same level. The templates MUST therefore place, inside `<div class="page">` and
    in this order: the sidebar `<aside>`, the top `<header>`, and then
    `<div class="page-wrapper">`, which holds the page header and the page body. A
    template MUST NOT nest the top `<header>` inside
    `<div class="page-wrapper">`, and the top navbar carries `d-print-none` as the
    Tabler shell does.

    The shell carries **no footer**. No page renders a `<footer>` element, so the
    page body is the last region inside `<div class="page-wrapper">` on every page.
13. **One sidebar collapse, one toggler, one brand.** Tabler's vertical navbar
    holds its collapsible menu region inside the sidebar `<aside>`, identified by
    `class="collapse navbar-collapse"` and `id="sidebar-menu"`, and gives that
    region exactly one `navbar-toggler`, also inside the `<aside>`. Where Tabler's
    top navbar carries a toggler of its own, that toggler targets the top navbar's
    own `#navbar-menu` collapse, never the sidebar's; and in Tabler's own layout
    that combines a sidebar with a top navbar, the top navbar hides its brand, so
    the shell shows one brand only. The templates MUST follow this: the sidebar
    collapse carries `class="collapse navbar-collapse"` and `id="sidebar-menu"` and
    lives inside the `<aside>`; exactly one `navbar-toggler` in the whole shell
    targets `#sidebar-menu`, and it lives inside that same `<aside>`; and the top
    navbar carries neither a second toggler for `#sidebar-menu` nor a second brand,
    so each page renders exactly one brand, the sidebar brand of rule 11. Two
    togglers driving one collapse would give a small viewport two hamburger
    controls for the same menu, and a second brand would show the product name
    twice. Tabler renders that collapse region as a `<nav>` element carrying
    `aria-label="Sidebar"`, so the menu is a navigation landmark with an accessible
    name that tells it apart from the page's other navigation; the templates MUST
    use that element and that label. The off-canvas
    (hamburger) behaviour specified in
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design) is
    unchanged, with a single control driving it.
14. **`aria-current` on the active navigation link.** Tabler marks the active entry
    of its vertical navbar with the `active` class on the `<li class="nav-item">`,
    and marks the active link of its navigation examples with `aria-current="page"`
    on the `<a class="nav-link">`. The templates MUST do both wherever they
    highlight the active view: the `<li>` carries `active` and the `<a>` inside it
    carries `aria-current="page"`. This applies to the sidebar's roadmap-index entry
    and to the active view among a roadmap's Sprints, Tasks, Audit, and Graph links
    (see rule 1), so the active view reaches assistive technology and is not
    conveyed by colour alone.
15. **Pagination is wrapped in a labelled `nav`.** Tabler emits its pagination
    component inside a `<nav>` element carrying a descriptive `aria-label`, which is
    also how Bootstrap, the framework Tabler is built on (see rule 1), specifies
    that component. The wrapper is what makes assistive technology announce the
    control as a navigation section and tell it apart from the page's other
    navigation. The audit log
    page's numbered pagination bar MUST therefore sit inside a
    `<nav aria-label="...">` whose label names what the bar navigates. The
    `ul.pagination` list, its `li.page-item` items, and its `a.page-link` links stay
    exactly as specified in [Roadmap Audit Log Page](#roadmap-audit-log-page); the
    wrapper adds the landmark and the accessible name and changes no pagination
    behaviour.
16. **Page-header actions column.** Tabler's page-header component emits its
    actions column as `<div class="col-auto ms-auto d-print-none">`, its
    `d-print-none` matching the `d-print-none` the `page-header` element itself
    carries. Where a page header carries actions, the templates MUST use that
    column idiom unchanged, `d-print-none` included.
17. **The fluid layout idiom is `layout-fluid` plus `container-xl`.** Tabler's
    full-width layout pairs `class="layout-fluid"` on `<body>` with ordinary
    `container-xl` page containers. The vendored stylesheet's
    `.layout-fluid .container,.layout-fluid [class*=" container-"],.layout-fluid [class^=container-]{max-width:100%}`
    rule exists for exactly that pairing and is what releases those containers to
    the full viewport width. A `container-fluid` page container is already full
    width on its own, which leaves the `layout-fluid` body class with nothing to act
    on and silently drops the idiom. The templates MUST therefore carry
    `layout-fluid` on `<body>` and use `container-xl` for the shell containers: the
    top navbar, the page header, and the page body. The
    `container-fluid` inside the sidebar `<aside>` is Tabler's own vertical-navbar
    markup and stays exactly as Tabler ships it.
18. **The page body is a `main` landmark.** Tabler's built admin shell renders the
    page body as `<main class="page-body">`, so the region that holds each page's
    own content is the document's `main` landmark and assistive technology can jump
    straight to it past the sidebar, the top navbar, and the page header. The
    templates MUST use that element for the page body, keeping the `page-body`
    class, which is what the vendored stylesheet styles; the element carries no
    identifier, because the one Tabler puts there exists only to anchor a skip link
    whose styling lives in Tabler's demonstration stylesheet rather than in the
    distributed one this project vendors, and this interface offers no skip link.
    The change is behaviour-neutral: `.page-body` is a class selector, so the
    element it sits on affects no layout rule. Tabler's older hand-written
    page-layout snippet still shows a `<div>` here; where a documented snippet and
    the framework's own built shell disagree, the built shell of the vendored
    distribution governs, and the templates MUST NOT be reverted to the `<div>`.
19. **The top navbar names the selected roadmap.** The shell's top navbar carries
    one thing: the name of the roadmap whose data the current page shows. Every
    page but the roadmap index is scoped to a single roadmap — its sprints, one of
    those sprints, its tasks, its audit log, or its knowledge graph — and the name
    is what tells one roadmap's pages from another's at a glance. The sidebar's own
    per-roadmap section label collapses out of sight behind the off-canvas menu on
    a small viewport (rule 5), so the top navbar is the one region that names the
    selected roadmap at every viewport width.

    The name is rendered prominently and with vendored Tabler classes only: the
    name alone, carrying Tabler's `h3` type utility, inside the
    `navbar-nav flex-row` / `nav-item` idiom Tabler uses for the top navbar's own
    content. **No glyph precedes it.** An icon here would be the same on every
    page of every roadmap, so it would distinguish nothing, while the sidebar
    already gives each of the roadmap's views its own distinguishing glyph; a
    roadmap is identified by its name, which is what the URL, the sidebar label,
    and the page title all use. A long name is truncated with Tabler's `text-truncate`
    rather than wrapped or allowed to overflow, so the navbar keeps its height and
    the page never scrolls horizontally because of it (see
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)). The
    name shown is the validated roadmap segment of the request path — the same
    value that selected the database (see [Routes and Pages](#routes-and-pages)) —
    rendered through `html/template` as text, so it is escaped exactly like every
    other value the interface shows.

    **The roadmap index page names no roadmap.** `/` lists the roadmaps and belongs
    to none of them, so its top navbar renders nothing at all: no name and no
    placeholder text. The region is simply empty, exactly as the sidebar's
    per-roadmap section is absent on that page.

    **The navbar carries no read-only indicator.** It MUST NOT carry a badge,
    label, or icon declaring the interface read-only. That the interface never
    writes is a guarantee of the server, specified in
    [Security and Constraints](#security-and-constraints) and in
    each page's own **Read-only** rule, and it is already evident on every page:
    no form, no submit control, no edit affordance anywhere. Restating it in the
    one shell region that can instead identify the page's subject spends that
    region on what the user cannot act on. This mirrors the removal of the
    read-only footer band, whose whole content was the same restatement (rule 12).

### Full-Height Page Regions

Two pages present a region sized to the **viewport** rather than to its own
content, so that the region's children scroll **inside** it and the page itself
does not scroll to reach them: the Kanban board of the roadmap tasks page (see
[Roadmap Tasks Page](#roadmap-tasks-page), **Layout and scrolling**) and the graph
card of the knowledge-graph page (see
[Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page), **Graph card
layout**). Each sits inside the `main.page-body` landmark of the admin shell (see
[UI Framework](#ui-framework), rule 18). These two are the whole set: this
subsection introduces no region and no page, and states only how a region of this
kind is sized.

**The Roadmap Sprint Page's member-tasks board is deliberately not one of them.**
It is a board with per-column vertical scrolling, like the tasks page's board, but
its height is bounded by a definite length rather than by the space the page body
leaves (see [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Height and
scrolling**). The reason is that the sprint page is not a single-region page: the
Sprint details card sits above the board and the Comments card below it, and all
three belong to one sprint presentation. A board taking the space the page body
leaves would fill the rest of the viewport on its own and push the Comments card
below the fold for every sprint, while a board sized to its own content would push
that card further down with every member task the sprint gains. A definite,
bounded height avoids both: the board shows a useful number of cards, scrolls the
rest inside its columns, and leaves the Comments card within reach. Rules 1 to 5
below therefore do not apply to that board, and a check written against them MUST
NOT be run on it.

1. **Two edges fix the height, and both MUST hold at every viewport size.**
   - **The region ends where the page body ends.** The bottom edge of the region
     coincides with the bottom edge of the page body, leaving no band of unused
     page body beneath it.
   - **That edge lies within the viewport.** The page does not scroll vertically
     to reveal the end of the region.

   Neither edge is sufficient on its own, and a check that asserts one of them
   alone passes on a defective layout. A region that stops short of the page
   body's end sits well within the viewport and satisfies the second edge while
   wasting exactly the space it failed to take. A region that overruns the bottom
   of the viewport still ends where the page body ends and satisfies the first
   edge, because the overrun is itself what stretched the page body to that
   height. Only the two together state that the region takes the space the page
   body has, and no more.
2. **No space is reserved for anything the page does not render.** What the region
   gives up at the top is what the shell and the page actually place above it: the
   top navbar and the page header on both pages, the query bar as well on the
   knowledge-graph page (see [Graph Query Bar](#graph-query-bar)), and the no-match
   message on the roadmap tasks page for as long as the board's controls match no
   task (see [Roadmap Tasks Page](#roadmap-tasks-page), **Empty states**). Removing
   one of those elements reduces what the region gives up by that element's height,
   and adding one increases it — not as a follow-up correction to a value recorded
   somewhere, but because rule 1 is stated over the page body's edges and the page
   body has already moved. In particular the shell carries no footer and no page
   renders a `<footer>` element (see [UI Framework](#ui-framework), rule 12), so no
   full-height region gives up any space for one.
3. **A fixed subtraction from the viewport height does not satisfy rule 1.** The
   height of the material above a full-height region is not a constant. The page
   header's actions column wraps as the viewport narrows — the tasks page header
   carries a search input and three filter dropdowns (see
   [Roadmap Tasks Page](#roadmap-tasks-page), **Header search control** and
   **Header filter controls**) — so the page header occupies more rows on a narrow
   viewport than on a wide one and the page body begins lower. A height obtained by
   subtracting a fixed length from the viewport height therefore matches the space
   available at one viewport width at best and misses it at every other: too tall
   where the page header is tall, which pushes the region past the fold, and too
   short where the page header is short, which leaves the unused band rule 1
   forbids. Rule 1 is stated over edges rather than over a subtracted length for
   that reason — an edge needs no value kept in step with the page.
4. **The viewport is the one the browser is showing at that moment.** A mobile
   browser with a retracting address bar has two viewport heights: the **large**
   viewport height, which measures the viewport as if the bar were retracted, and
   the **dynamic** viewport height, which measures what is visible while the bar is
   on screen. A full-height region MUST be sized against the dynamic height, so
   that rule 1's second edge holds while the bar is showing and not only once it
   has gone; sized against the large height, the end of the region sits below the
   fold for exactly as long as the bar is on screen. A browser that does not
   support the dynamic height MUST still receive a viewport-derived height rather
   than none: the two are declared together and in that order — the large height
   first, the dynamic height second — so a browser that understands only the first
   keeps it and a browser that understands both applies the second. In CSS these
   are the `vh` and `dvh` units, and that ordered pair of declarations is how the
   dynamic unit ships without a feature query. This is the vertical counterpart of
   the fluid-layout requirement in
   [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
   rules 2 and 9.
5. **A floor keeps the region usable on a very short viewport.** Below some
   viewport height the space the page body leaves is too small to present the
   region at all: a board column would show a fraction of a single card, and the
   graph canvas would be a strip. Each full-height region therefore carries a
   **minimum height**, and when the space the page body leaves falls below that
   minimum the region takes the minimum instead. In that case, and only in that
   case, the region's bottom edge may fall below the viewport and the page may
   scroll vertically to reach it: rule 1's second edge yields to the floor, because
   a region compressed past legibility is worse than one the reader scrolls to.
   Rule 1's first edge yields with it wherever the page places an element above the
   region inside the same container — the knowledge-graph page's query bar always,
   and the tasks page's no-match message for as long as the board's controls match
   no task (see rule 2). The page body is held to the same minimum as the region, so
   below the floor it carries that element and the floored region together, and the
   region's foot passes the page body's foot by the space the element occupies.
   Nothing in the stylesheet closes that gap: closing it would take a page-body
   floor of the region's floor plus the height of the element above it, and that is
   the fixed subtraction rule 3 forbids. The floor is the single exception to
   rule 1 — to both of its edges — and not a licence to overrun the viewport, or to
   end anywhere but where the page body ends, at ordinary viewport heights.

### Status, Priority, and Severity Badge Colours

Status, priority, and severity are presented as Tabler badges. The badges MUST use
**semantically meaningful** Tabler colour variants rather than a single fixed
colour, so the colour carries the meaning of the value at a glance. The mapping is
deterministic: a given enum value always maps to the same Tabler badge colour
variant, everywhere a badge for that value is shown. The badge colour variants are
Tabler's "light" badge utilities (the `bg-*-lt` classes), consistent with Tabler's
badge examples and its dark theme.

This subsection defines the only authoritative mapping. The badges use the
canonical enums already defined elsewhere and introduce no new enum value: the task
status enum and sprint status enum are defined in `MODELS.md § Enums`, the task
status lifecycle in `STATE_MACHINE.md § Task State Machine`, the sprint status
lifecycle in `STATE_MACHINE.md § Sprint State Machine`, and the `priority` and
`severity` integer ranges (`0`-`9`) in `MODELS.md § Task`. The severity bands reuse
the canonical criticality ranges defined in `COMMANDS.md § Show Sprint Status Report`
(low `0`-`2`, medium `3`-`5`, high `6`-`7`, critical `8`-`9`); this file does not
redefine them.

**Task status (`TaskStatus`) → Tabler badge colour:**

| Task status | Meaning | Badge variant |
|-------------|---------|---------------|
| `COMPLETED` | Work finished | `bg-green-lt` |
| `TESTING` | In testing / awaiting verification | `bg-yellow-lt` |
| `DOING` | In progress | `bg-blue-lt` |
| `SPRINT` | Assigned to a sprint, not yet started | `bg-cyan-lt` |
| `BACKLOG` | Neutral / not yet planned into a sprint | `bg-secondary-lt` |

**Sprint status (`SprintStatus`) → Tabler badge colour:**

| Sprint status | Meaning | Badge variant |
|---------------|---------|---------------|
| `CLOSED` | Sprint completed | `bg-green-lt` |
| `OPEN` | Sprint in progress (current) | `bg-blue-lt` |
| `PENDING` | Neutral / not yet started | `bg-secondary-lt` |

**Priority (`priority`, integer `0`-`9`) → Tabler badge colour:**

| Priority band | Range | Badge variant |
|---------------|-------|---------------|
| High | `7`-`9` | `bg-red-lt` |
| Medium | `4`-`6` | `bg-yellow-lt` |
| Low | `0`-`3` | `bg-secondary-lt` |

**Severity (`severity`, integer `0`-`9`) → Tabler badge colour:**

| Severity band | Range | Badge variant |
|---------------|-------|---------------|
| Critical | `8`-`9` | `bg-red-lt` |
| High | `6`-`7` | `bg-orange-lt` |
| Medium | `3`-`5` | `bg-yellow-lt` |
| Low | `0`-`2` | `bg-secondary-lt` |

Rules:

1. **Deterministic and total.** Every value of each enum maps to exactly one badge
   colour variant in the tables above. The priority and severity bands together
   cover the whole `0`-`9` range with no gap and no overlap, so every valid integer
   value resolves to exactly one band.
2. **Applied consistently everywhere a badge is shown.** The mapping governs a
   badge's **colour**, and it is keyed on a value: a task status, a sprint status, a
   `priority`, or a `severity`. The same mapping is applied wherever a badge carries
   one of those values: the priority and severity badges on the tasks page's board
   cards (see [Roadmap Tasks Page](#roadmap-tasks-page)), the priority and severity
   badges on the cards of the sprint detail member-tasks board (see
   [Sprint Detail Sub-Template](#sprint-detail-sub-template)), the task detail modal
   (see [Task Detail Modal](#task-detail-modal)), the sprint cards (see
   [Shared Sprint-Card Partial](#shared-sprint-card-partial)), the Roadmap Sprint
   Page header and metadata datagrid (see [Roadmap Sprint Page](#roadmap-sprint-page)
   and [Sprint Detail Sub-Template](#sprint-detail-sub-template)), the sprint
   tabs on the Roadmap Sprints Page (see [Roadmap Sprints Page](#roadmap-sprints-page)),
   and the per-column count badge of each of the two Kanban boards (see
   [Roadmap Tasks Page](#roadmap-tasks-page) and
   [Sprint Detail Sub-Template](#sprint-detail-sub-template)).
   A badge that carries one of those values uses the variant the relevant table above
   assigns to it, and no such badge uses a single fixed colour across differing
   values.

   **A badge carries a value in one of two ways.** The mapping applies to both. The
   second is a closed list of named cases, not a general licence; the test that
   decides membership of that list is stated below it:
   - **The badge's own text is the value.** Every site listed above except the sprint
     tabs and the two boards' per-column count badges is this case: the badge reads
     `COMPLETED`, `OPEN`, `7`, or `2`, and it takes the colour the relevant table
     assigns to the value it carries. On the card of
     either board the priority and severity badges write that value behind a
     one-letter prefix — `P7`, `S2` — which names the field the value belongs to,
     because a card carries no label that would (see
     [Roadmap Tasks Page](#roadmap-tasks-page), **Card content**, item 3). The prefix is a label and not a value: this mapping keys on
     the value alone and never on the prefix, so `P7` takes the colour of the
     priority `7`, and no band, no colour variant, and no enum value changes because
     of it.
   - **The badge counts the members of a group with one status to key on.** Three
     sites are this case: the three sprint tabs on the Roadmap Sprints Page, the
     per-column count badge of the Roadmap Tasks Page's Kanban board, and the
     per-column count badge of the sprint detail member-tasks board. Each such badge
     is a hybrid: the **colour** is the variant the relevant table above assigns to
     the status the counted group has, directly or through its canonical status,
     while the **text** is the number of members in the group. The count itself
     selects no colour: it is not a value this mapping knows, and it adds no fourth
     badge kind.
     - Each **sprint tab** groups the sprints of exactly one sprint status — Próximos
       the `PENDING` sprints, Actual the `OPEN` sprints, Concluídos the `CLOSED`
       sprints (see [Roadmap Sprints Page](#roadmap-sprints-page)) — so a tab has a
       sprint status even though no sprint status is written on it. Próximos therefore
       carries `bg-secondary-lt`, Actual carries `bg-blue-lt`, and Concluídos carries
       `bg-green-lt`, each showing its own count.
     - Each column of the **tasks board** is exactly one task status, because that
       board has one column per `TaskStatus` value (see
       [Roadmap Tasks Page](#roadmap-tasks-page)), so its count badge takes the
       variant the task status table above assigns to that column's status.
     - Each column of the **sprint board** groups a set of task statuses rather than a
       single one — `WAITING` groups `BACKLOG` and `SPRINT`, `DOING` groups `DOING`
       and `TESTING`, and `CLOSED` holds `COMPLETED` alone — so its count badge takes
       the variant assigned to the **canonical status of the group**, the status a
       task is normally in at that stage of the sprint: `SPRINT` for `WAITING`,
       `DOING` for `DOING`, and `COMPLETED` for `CLOSED`. Why each group's canonical
       status is the one named here is stated where that board is defined (see
       [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Column header**).

   **The discriminating test: has the counted group one status to key on?** The
   mapping colours a count badge where the group it counts has one status value to key
   on — whether the group has that status directly, as a sprint tab and a tasks board
   column do, or through its canonical status, as a sprint board column does. Where
   the counted group has no status at all, the badge stays neutral and this mapping
   does not govern it.

   Two count badges stay neutral under that test, and the rule above does not reach
   either. The **Comments card header count** on the Roadmap Sprint Page counts
   comments, and a comment carries no status of any kind (see
   [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Comments card**), so
   the tables above have nothing to key on and the badge carries the neutral
   `bg-secondary-lt`. A count over a **group of mixed status for which no canonical
   status is defined** stays neutral for the same reason: such a group has no one
   status value, and colouring it would mean choosing a colour this mapping assigns to
   nothing.

   **Read the three tab colours as a set, never one at a time.** `PENDING` maps to
   `bg-secondary-lt`, which is also the neutral variant a count badge carries when
   nothing colours it, so the Próximos tab looks the same whether the mapping colours
   it or not. Looking the same is not being correct: the Próximos badge conforms only
   when its colour comes from the sprint status table, exactly as the other two do,
   and on its own it demonstrates nothing about this rule. What separates a
   conforming rendering from a non-conforming one is that Actual carries `bg-blue-lt`
   and Concluídos carries `bg-green-lt`; a rendering that gives all three tabs
   `bg-secondary-lt` conforms on none of them.

   The same trap sits on the tasks board, where `BACKLOG` maps to `bg-secondary-lt`:
   that column's count badge renders identically whether the mapping colours it or
   not, so the board's five column badges are read together and never one at a time.

   The mapping governs the colour of those three kinds of value only — task and
   sprint status, `priority`, and `severity` — whether the badge writes the value that
   colours it or counts a group that has that value. It governs no other badge. The
   comment-type badge shown in the task detail modal and the sprint Comments card is
   deliberately outside it and uses the neutral `bg-secondary-lt` variant for every
   type value (see [Task Detail Modal](#task-detail-modal)), and every count badge the
   discriminating test leaves out is outside it as well and stays governed by the
   section that defines it.
3. **No new enum value.** The mapping introduces no status, priority, or severity
   value that is not already defined in `MODELS.md` and `STATE_MACHINE.md`. Should a
   new enum value or a revised band be introduced there, this table is updated in the
   same change so that the mapping stays total.
4. **Faithful to Tabler.** The badge markup follows Tabler's badge example (a
   Tabler `badge` element carrying the `bg-*-lt` colour utility); the templates do
   not hand-roll a badge component (see [UI Framework](#ui-framework), rule 8).

### Knowledge-Graph Visualisation Library

1. The interactive node-link visualisation uses **D3.js** (<https://d3js.org/>)
   as the graph rendering library, rendered inside the Tabler admin-shell (see
   [UI Framework](#ui-framework)).
2. D3.js is **vendored**: its already-built distribution file, together with the
   **d3-sankey** plugin used for the Sankey layout, is committed to the repository
   under the web asset set and embedded into the binary with `go:embed`. Both are
   served locally from `/static/...`. Neither is ever loaded from a content
   delivery network or any remote origin.
3. The library renders the nodes and edges returned by the graph data endpoint
   (see [Graph Data Endpoint](#graph-data-endpoint)) and provides pan, zoom, and
   selection so the user can inspect a node's or edge's labels, type, and
   properties.
4. **Selectable layouts.** The graph page can render the same graph data in any of
   the following layouts, which are the complete set of layouts in the "Networks"
   section of the D3 gallery (<https://observablehq.com/@d3/gallery>):
   - **Force-directed graph**;
   - **Disjoint force-directed graph**;
   - **Mobile patent suits** — the **default** layout;
   - **Arc diagram**;
   - **Sankey diagram**;
   - **Hierarchical edge bundling**;
   - **Chord diagram**;
   - **Directed chord diagram**;
   - **Chord dependency diagram**.

   All of these layouts are rendered with the vendored D3.js. The three Chord
   variants use D3's **d3-chord** module (`d3.chord`, `d3.ribbon`,
   `d3.ribbonArrow`), which is part of the vendored D3 bundle; no new vendored
   library is added for them. A dropdown (select control) on the graph page lets
   the user choose which layout renders the graph. The page renders the
   Mobile patent suits layout by default, and changing the dropdown selection
   re-renders the same graph data in the chosen layout.
5. **Graceful degradation for constrained layouts.** Some layouts require a
   constrained data shape: the Sankey diagram requires a directed acyclic graph,
   and Hierarchical edge bundling and the Chord variants (Chord diagram, Directed
   chord diagram, and Chord dependency diagram) derive a grouping or an adjacency
   matrix from the graph. Every layout option is always offered in the dropdown
   regardless of the current graph data. When the current graph data cannot be
   meaningfully drawn in the selected layout — for example a cyclic graph selected
   as Sankey — the page MUST degrade gracefully: it shows a clear, read-only,
   in-place message explaining that the current graph cannot be rendered in that
   layout, instead of erroring or breaking the page. The user can then select a
   different layout. This is a read-only message; it triggers no write and no
   navigation.
6. **Touch and small-viewport configuration.** D3.js supports touch gestures. The
   visualisation and its container MUST be configured to be touch- and
   small-viewport-friendly: the container is fluid and fits the viewport, ending
   within it as a full-height page region (see
   [Full-Height Page Regions](#full-height-page-regions)), and the
   visualisation supports touch pan, pinch-to-zoom, and tap to select and inspect,
   so node and edge detail can be reached without a mouse hover (see
   [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)). The
   layout dropdown is likewise touch-usable.
7. The choice of D3.js (with the d3-sankey plugin) is an implementation-level
   decision recorded here so the SPEC is unambiguous about which library is
   vendored; substituting a different vendored, locally-served, build-step-free
   graph library is a SPEC change to this subsection and to
   `BUILD.md § Vendored Web Assets`, not a silent code change. No version number is
   pinned here; the vendored D3.js and d3-sankey versions live in the committed
   distribution under git.

## Responsive and Mobile-First Design

The web interface MUST be designed responsive and mobile-first. The layout adapts
to the viewport rather than assuming a desktop window, and the small-viewport
experience is the baseline that larger viewports enhance.

1. **Mobile-first base styles.** Base styles target small phone-sized viewports
   first. Styling for larger tablet and desktop viewports is layered on top
   through `min-width` media queries, so the unqualified styles are the
   small-screen styles and wider screens progressively enhance them.
2. **Fluid layouts.** Layouts adapt fluidly across viewport sizes. On small
   screens the page produces no horizontal scrolling — `<body>` never overflows
   horizontally at any viewport width — typography stays readable, and navigation
   and other interactive controls present touch-friendly, appropriately sized hit
   targets. A component that deliberately scrolls horizontally **inside its own
   container**, such as the Kanban board on the roadmap tasks page (rule 9) or the
   member-tasks board on the roadmap sprint page (rule 10), is not page-level
   horizontal overflow and is permitted; the prohibition is on the page itself
   scrolling horizontally.
3. **Applies to every page.** The mobile-first, responsive requirement applies to
   every page: the roadmap index page, the roadmap sprints page (the sprint tabs),
   the roadmap tasks page (the Kanban task board), the roadmap sprint page, the
   roadmap audit log page (the audit table), and the knowledge-graph page.
4. **Usable tabular data on narrow screens.** The roadmap sprints page, the
   roadmap sprint page, and the roadmap audit log page present sprint and audit data
   that is tabular by nature — among it the sprint metadata datagrid and the audit
   table. This data MUST remain usable on narrow screens, for
   example through responsive or stacked tables or an equivalent layout that
   avoids page-level horizontal overflow, while still presenting the fields and
   relationships
   defined for those pages (see [Roadmap Sprints Page](#roadmap-sprints-page),
   [Roadmap Sprint Page](#roadmap-sprint-page), and
   [Roadmap Audit Log Page](#roadmap-audit-log-page)). Neither page presents its
   tasks as a table: the roadmap tasks page presents them as a Kanban board, which
   rule 9 governs, and the roadmap sprint page presents its member tasks as a board
   as well, which rule 10 governs.
5. **Touch- and small-viewport-usable sprint tabs and task modal.** The three
   sprint tabs on the roadmap sprints page (Próximos, Actual, Concluídos) and the
   task detail modal MUST remain usable on touch input and on small viewports. The
   tabs offer touch-friendly controls to switch between them without horizontal
   overflow, and the task detail modal fits the viewport, scrolls its content when
   the task's text is long, and offers touch-friendly controls to open and dismiss
   it (see [Roadmap Sprints Page](#roadmap-sprints-page),
   [Roadmap Tasks Page](#roadmap-tasks-page), and
   [Task Detail Modal](#task-detail-modal)).
6. **Touch- and mobile-usable graph visualisation.** The interactive
   knowledge-graph visualisation MUST remain usable on touch and mobile devices.
   Its container is fluid and fits the viewport, ending within it as a full-height
   page region (see [Full-Height Page Regions](#full-height-page-regions)), and it
   supports touch gestures —
   pan, pinch-to-zoom, and tap to select and inspect — so node and edge detail can
   be reached without a mouse hover (see
   [Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library)).
7. **Responsive viewport meta tag.** Every HTML page includes the responsive
   viewport meta tag, so mobile browsers scale the page to the device width rather
   than rendering it at a fixed desktop width.
8. **Vendored CSS framework (Tabler), no remote origin.** The interface uses the
   Tabler CSS framework (see [UI Framework](#ui-framework)). The framework, and
   any further CSS the interface uses, MUST be vendored and embedded with the
   stylesheet and served only from `/static/...`; no CSS is loaded from a content
   delivery network or any remote origin, consistent with
   [Self-Contained Deliverable](#self-contained-deliverable) and
   [Embedded Asset Categories](#embedded-asset-categories). Tabler is itself
   responsive and mobile-first, which keeps the mobile-first guarantee of this
   section intact; on small viewports the admin-shell navigation sidebar
   collapses to an off-canvas (hamburger) menu so the pages stay usable without
   horizontal overflow on phones.
9. **Usable Kanban board on narrow screens.** The roadmap tasks page presents its
   tasks as a board of five fixed columns side by side (see
   [Roadmap Tasks Page](#roadmap-tasks-page)). When the five columns do not fit the
   viewport, the board scrolls horizontally inside its own container, and the page
   itself still does not scroll horizontally (rule 2). Each column scrolls
   vertically and independently when its card list exceeds the available height,
   which is the height the board takes as a full-height page region — the space the
   page body leaves, measured against the viewport the browser is showing at that
   moment (see [Full-Height Page Regions](#full-height-page-regions)). On
   narrow viewports the board MUST remain usable: each column keeps a minimum width
   at which its cards stay legible, the horizontal board scroll is reachable by a
   touch gesture, and the cards and their badges present touch-friendly hit targets
   that open the read-only task detail modal (see
   [Task Detail Modal](#task-detail-modal)). The page header's search input and its
   three filter dropdowns are likewise usable on a narrow viewport: they fit the
   header's actions column without page-level horizontal overflow, wrapping within
   that column rather than forcing the page to scroll horizontally, and each
   presents a touch-friendly target.
10. **Usable member-tasks board on narrow screens.** The roadmap sprint page
   presents the sprint's member tasks as a board of three fixed columns side by
   side (see [Sprint Detail Sub-Template](#sprint-detail-sub-template)). When the
   three columns do not fit the viewport, the column strip scrolls horizontally
   inside its own container and the page itself still does not scroll horizontally
   (rule 2). Each column scrolls vertically and independently when its cards exceed
   the board's height, which is a bounded length and **not** the space the page body
   leaves, because the sprint page places the Sprint details card above the board
   and the Comments card below it (see
   [Full-Height Page Regions](#full-height-page-regions)). On narrow viewports the
   board MUST remain usable on the same terms as the tasks page's board: each column
   keeps a minimum width at which its cards stay legible, the horizontal strip
   scroll is reachable by a touch gesture, and the cards and their badges present
   touch-friendly hit targets that open the read-only task detail modal (see
   [Task Detail Modal](#task-detail-modal)). The board's height is `60vh` with a
   floor read from the `--full-height-region-floor` custom property. Its three
   columns divide the board's width equally and grow with the viewport, never
   falling below the tasks board's own `17rem` minimum and separated by that board's
   `0.75rem` gap, so every length is viewport-relative or in `rem` and scales with
   the screen and with the reader's own text size rather than fixing the layout to
   one device (see [Sprint Detail Sub-Template](#sprint-detail-sub-template),
   **Height and scrolling**).

## Server Logging

`rmp web` is the only long-lived `rmp` process. Every other command reports its
failure on stderr and exits; the web server, by design, absorbs a per-request
failure into an HTTP status and keeps serving (see
[Error Handling and Exit Codes](#error-handling-and-exit-codes), rule 4). The
response body the browser receives is deliberately opaque — `internal server
error` — so that a read failure, a corrupt graph store, or a template fault
never discloses internal detail to the client. Without a log, that detail is
discarded entirely and the operator is left with a failing page and a silent
terminal.

The server therefore writes a diagnostic log to the console. The log is the
counterpart of the opaque response: what the response withholds, the console
states explicitly.

### Logger Configuration

1. The server logs through the Go standard library's structured logger,
   `log/slog`, configured with a `slog.TextHandler`. Every record is a single
   line of `key=value` pairs, which reads directly on a terminal and needs no
   parsing tool.
2. The handler writes to **stderr**. Stdout carries only the startup success
   object defined in `COMMANDS.md § Web Interface`, so a caller that reads
   stdout for the served URL is never disturbed by a log record. No log record
   is ever written to stdout.
3. The minimum enabled level is `INFO`; `DEBUG` records are not emitted.
4. The configuration is fixed. This version adds no logging flag to `rmp web`,
   no environment variable, and no log file: the console is the only
   destination, and the surface in [Command Surface](#command-surface) is
   unchanged.
5. Every record's `time` attribute is **always UTC**, in the project's single
   canonical timestamp format `YYYY-MM-DDTHH:mm:ss.sssZ` — three digits of
   milliseconds and an explicit `Z` suffix, for example
   `2026-08-20T19:53:00.918Z`. This is the same format every Groadmap date uses
   (`DATA_FORMATS.md § Dates - ISO 8601 with UTC`), so a log record and a task's
   `created_at` can be compared directly, and a log read on a machine in one
   time zone means the same instant as the same log read anywhere else.
   `slog.TextHandler` timestamps in the **local** zone with an offset by
   default, which satisfies neither rule; the handler MUST therefore replace the
   `time` attribute with the UTC value in that format rather than accept the
   default.
6. The logger is a single package-level instance, built once at package
   initialisation. It is replaceable, so that a test can capture the records and
   assert their content rather than merely their presence.

### Levels

| Level | Meaning | Examples |
|-------|---------|----------|
| `ERROR` | The server failed. The condition is answered with HTTP 500 and is a fault of the server or of the environment it cannot recover from. | A roadmap's database cannot be read; a page template fails to execute; a response body fails to encode; the knowledge-graph store cannot be opened. |
| `WARN` | The server did not fail, but an operator needs to know what happened. The condition is caused by the client or by the environment and leaves the server serving. | A query-bar statement refused for an invalid limit or failing in the engine (HTTP 400); a roadmap skipped by the startup schema migration; the interface bound to a non-loopback address. |
| `INFO` | Enabled, but unused in this version: a successful request and a successful startup write no record. | — |

### What Is Logged

Each condition below MUST produce exactly one record.

**Startup.** These three replace the ad-hoc `warning: ...` lines the server
previously wrote to stderr with `fmt.Fprintf`. They remain non-fatal, remain on
stderr, and remain informational: none of them changes the exit code or prevents
the server from starting (see
[Startup Schema Migration](#startup-schema-migration), rule 6, and
[Bind Address and Port Selection](#bind-address-and-port-selection), item 3).

| Condition | Level | `msg` | Attributes |
|-----------|-------|-------|------------|
| The resolved bind host is not a loopback address | `WARN` | `web interface is reachable from the network` | `host`, and a `hint` naming the flag that restricts the bind |
| The roadmap list cannot be read for the startup schema migration | `WARN` | `cannot list roadmaps for startup schema migration` | `err` |
| One roadmap cannot be opened or migrated at startup | `WARN` | `startup schema migration skipped for roadmap` | `roadmap`, `err` |

A startup record has no request behind it, so it carries no `method`, `path`, or
`status`; it names its own subject instead.

**Per request.** Every response the server produces with HTTP status 500 MUST be
accompanied by exactly one `ERROR` record naming the underlying error, and every
HTTP 400 the graph data endpoint produces by exactly one `WARN` record.

| Route or helper | Condition | Level | Status |
|-----------------|-----------|-------|--------|
| any roadmap-scoped route | the roadmap's existence check fails with an I/O error | `ERROR` | 500 |
| `GET /` | the roadmap list cannot be read | `ERROR` | 500 |
| `GET /roadmaps/{name}` | the sprints view cannot be loaded | `ERROR` | 500 |
| `GET /roadmaps/{name}/tasks` | the task board cannot be loaded | `ERROR` | 500 |
| `GET /roadmaps/{name}/tasks/{id}/data` | the task detail cannot be loaded for a reason other than not-found | `ERROR` | 500 |
| `GET /roadmaps/{name}/audit` | the audit page cannot be loaded | `ERROR` | 500 |
| `GET /roadmaps/{name}/sprints/{id}` | the sprint cannot be loaded for a reason other than not-found | `ERROR` | 500 |
| `GET /roadmaps/{name}/graph/data` | the request's limit was invalid, or its statement failed in the engine | `WARN` | 400 |
| `GET /roadmaps/{name}/graph/data` | the graph cannot be read for any other reason | `ERROR` | 500 |
| HTML rendering | the page template fails to execute | `ERROR` | 500 |
| JSON rendering | the response body fails to encode | `ERROR` | 500 |

A failure that is detected by a helper and answered there — a template that
fails to execute, a body that fails to encode — is logged by that helper, so the
record exists exactly once and names the helper's own subject. A handler that has
already logged its failure does not log it again on the way out.

### What Is Not Logged

These are deliberate exclusions, not omissions.

1. **HTTP 404 and 405 are not logged.** A request for an unknown roadmap, a
   non-integer id, an id that belongs to no record of the roadmap, an unmapped
   path, or a non-read method on a known path is an ordinary outcome of
   navigation, not a failure of the server. Logging them would bury the genuine
   failures under every mistyped URL and every browser probe for an asset the
   server does not serve. The single exception is already covered above: when a
   roadmap's existence check fails with an I/O error the response is 500, not
   404, and it is logged.
2. **There is no access log.** A successful request writes no record. The log
   exists to make failures visible, not to trace traffic.
3. **The client address is not logged.** The server binds loopback by default and
   serves read-only data; recording the peer of every failing request would add a
   personal datum to the console without adding diagnostic value.
4. **Nothing is redacted.** An error text may name a filesystem path under
   `~/.roadmaps/`. That path is the diagnostic value of the record; it is written
   to the operator's own console and it never reaches the HTTP response.

### Record Content

1. Every record carries the fixed attributes `time`, `level`, and `msg`.
2. `msg` is a short, stable, lower-case phrase naming the condition. It is a
   constant: no value is interpolated into it, so every record of one condition
   groups with the others regardless of the request that produced it. What varies
   between records belongs in the attributes.
3. Every per-request record additionally carries:
   - `method` — the request method;
   - `path` — the request path;
   - `status` — the HTTP status the server returned;
   - `err` — the text of the underlying error, which is precisely the value the
     HTTP response withholds.
4. `roadmap` is carried by every record for which the roadmap name is known.
5. Route-specific attributes name the record's subject where one exists: `task`
   and `sprint` for the id-bearing routes, `page` for the audit page, `template`
   for a template failure, and `kind` for the classification of a query-bar
   failure (the same classification the response body carries; see
   [Query-Bar Error Handling](#query-bar-error-handling)).
6. This section changes no response. A 500 still returns the opaque
   `internal server error` text and a 400 from the graph data endpoint still
   returns its structured JSON error with the same `error` and `kind` fields.
   Detail is added to the console, never to the response.

### Log Integrity

A request path, a roadmap name, a Cypher query, and an error text can each carry
bytes the server did not choose. A log format that pasted those bytes in verbatim
would let a crafted request write what reads as a second, forged record — a
newline followed by `level=ERROR msg="..."` — into the operator's console, which
would make the log an untrustworthy account of what happened.

1. Every record MUST occupy exactly one line. A control character inside an
   attribute value MUST be escaped, never emitted literally.
2. `slog.TextHandler` provides this property: it quotes any value containing
   whitespace, a quotation mark, or a control character, and escapes a newline as
   the two characters `\` and `n`. A forged `level=` or `msg=` inside a value
   therefore stays inside that quoted value and cannot terminate the record.
3. The property MUST be covered by a regression test that drives a newline and a
   `level=ERROR msg=` sequence through a logged attribute and asserts that the
   emitted record is still a single line.

## Error Handling and Exit Codes

The `rmp web` process uses the existing sentinel errors and exit-code mapping in
`ARCHITECTURE.md § Error Handling` and `ARCHITECTURE.md § Exit Codes`. The web
interface introduces **no** new sentinel error and **no** new exit code.

These exit codes describe how the `rmp web` **process** terminates. They are
distinct from the per-request HTTP status codes in
[Routes and Pages](#routes-and-pages), which describe responses from the running
server.

| Condition | Sentinel | Exit code |
|-----------|----------|-----------|
| `--port` value out of range `0`-`65535`, or non-integer | `utils.ErrValidation` | 6 |
| Unknown flag, or unexpected positional argument | `utils.ErrInvalidInput` | 2 |
| Requested bind address/port cannot be bound (port in use with explicit `--port`, host not assignable) | `utils.ErrDatabase` | 1 |
| Data directory `~/.roadmaps/` exists but cannot be read or created | `utils.ErrDatabase` | 1 |
| Server started and then stopped by `SIGINT`/`SIGTERM` (graceful shutdown) | — | 0 |

Rules:

1. A startup failure (invalid flag, unbindable address/port, unreadable data
   directory) terminates the process before it serves any request, with the
   plain-text error to stderr and the matching exit code above.
2. A bind failure is treated as an I/O / system failure and maps to
   `utils.ErrDatabase` (exit code 1), consistent with how the CLI treats other
   I/O and database-class failures. The error message names the host and port
   that could not be bound.
3. The default-port fallback to an ephemeral port (see
   [Bind Address and Port Selection](#bind-address-and-port-selection)) means
   that, without an explicit `--port`, a busy default port does **not** cause a
   bind failure; the process binds an ephemeral port instead and starts normally.
4. Once the server is serving, per-request failures (roadmap not found, corrupt
   graph store, read error) are handled inside the running server as HTTP status
   responses (400, 404, 405, 500) and do **not** terminate the process. The process
   exit code is determined by how the server itself is started and stopped. The
   detail of such a failure is withheld from the response and written to the
   console instead, under the rules in [Server Logging](#server-logging).
5. Errors written to stderr by `rmp web` carry the standard AI-agent hint and
   follow the plain-text error format in `HELP.md § Error message format`.
6. If a future need arises for a dedicated web error class, it MUST be added
   following the procedure in `ARCHITECTURE.md § Adding New Error Types`. This
   version introduces none.

## Security and Constraints

1. **Loopback by default; network exposure is opt-in.** The server binds the
   loopback interface (`127.0.0.1`) by default, so the interface is reachable only
   from the local machine. Exposing the interface on the network is the explicit
   opt-in `--host 0.0.0.0` (all interfaces), or any other non-loopback address.
   When a non-loopback host is bound, the server prints a warning to stderr that
   the interface is reachable from the network (see
   [Bind Address and Port Selection](#bind-address-and-port-selection)). What is
   exposed by that choice is not read access alone; rule 3 states what else it is.
2. **Pages are read-only; one endpoint is not.** The server accepts only `GET` and
   `HEAD`; every other method returns HTTP `405`. It exposes no route that creates,
   edits, or deletes a roadmap, a task, a sprint, or an audit entry, and it writes
   no row and no audit entry to any `project.db` outside the startup migration.
   **The graph data endpoint is outside this rule**: it executes the statement the
   request carries, so it writes graph data whenever that statement does, and it
   checkpoints and truncates the write-ahead log after a transaction that wrote.
   Even a statement that writes nothing is not free of on-disk effect, because the
   engine's recovery repairs an interrupted checkpoint on open (see
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)
   and `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`).
3. **User-supplied Cypher is executed as written, and this is the interface's
   principal security property.** The graph page's query bar submits an editable
   Cypher statement to the graph data endpoint as the `q` parameter (see
   [Graph Query Bar](#graph-query-bar) and
   [Graph Data Endpoint](#graph-data-endpoint)). The endpoint does not classify it,
   does not refuse it on the ground of what it does, and executes it on the
   transactional path. The consequences are stated plainly:
   - A `GET` of that endpoint can create, change, and delete nodes, relationships,
     properties, and labels, and can create and drop indexes and constraints.
     `MATCH (n) DETACH DELETE n` submitted through the query bar empties the
     roadmap's knowledge graph, commits, and checkpoints.
   - **No authentication stands in the way.** The server has no login, no token, no
     session, and no per-route authorisation. Any client that can open the bound
     address can issue that request.
   - **The only access control is the bind address.** On the default loopback bind,
     the reachable set is the local machine's own processes. `--host 0.0.0.0`, or
     any other non-loopback address, extends that set to everything that can route
     to the host, and it is a **write** grant over every roadmap's knowledge graph,
     not a read grant. A user binding a non-loopback address is making that choice.
   - **A `GET` with side effects departs from RFC 9110, Section 9.2.1**, which
     defines `GET` as a safe method. An intermediary, a browser prefetch, a crawler,
     or a repeated history entry may therefore re-execute a destructive statement
     without the user asking again. The endpoint stays `GET`-only because the query
     bar's contract is a URL, and the departure is recorded here rather than left
     to be discovered.
   - There is no undo. The graph store has no per-statement history a caller can
     roll back to, and `rmp` offers no graph restore command.
4. **Filesystem permission model is unchanged.** The web interface reads through
   the existing locations and respects the existing permission model: `0700` for
   `~/.roadmaps/` and each roadmap home directory, `0600` for `project.db`, and
   `0700` for each `graph/` store (see `ARCHITECTURE.md § Directory Structure`).
   The web interface relaxes no permission, and it creates no roadmap database, no
   roadmap home directory, and no graph store directory: a roadmap that has no
   `graph/` directory is served as an empty graph rather than having one created
   for it. The one artefact a graph request may create outside the store's own
   contents is the lock file `write.lock`, inside a `graph/` directory that already
   exists, when no previous invocation has created it (see
   `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`).
5. **No arbitrary filesystem serving; path-traversal guard.** The static handler
   serves only assets from the embedded asset set, never an arbitrary host
   filesystem path. Roadmap names taken from the URL path are validated against
   the roadmap-name rules (regex `^[a-z0-9_-]+$`, maximum 50 characters) **before**
   they are used to build any filesystem path, so a crafted `{name}` cannot
   traverse outside `~/.roadmaps/`. A name that fails validation is rejected with
   HTTP `404` and never reaches the filesystem (see
   [Routes and Pages](#routes-and-pages)). This mirrors the central roadmap-name
   validation gate the CLI applies (see `ARCHITECTURE.md § Security Guarantees`).
6. **Self-contained assets, no CDN, no external calls.** Every asset a page loads
   is served from the local server's embedded assets, and the deliverable is the
   single `rmp` binary with zero external runtime dependency. No page references a
   content delivery network or any other remote origin, the interface functions
   fully offline, and the server makes no outbound network request (see
   [Self-Contained Deliverable](#self-contained-deliverable) and
   [Frontend and Embedded Assets](#frontend-and-embedded-assets)).
7. **Output escaping.** Roadmap-derived text (task and sprint fields, task and
   sprint comment bodies, and graph node and edge labels
   and property values) that the server renders into a page is rendered through
   `html/template`'s contextual
   auto-escaping, so data that contains HTML control characters cannot alter page
   structure. Data delivered as JSON instead — the task detail endpoint's task and
   comment data, and the graph data delivered to the visualisation — is encoded as
   JSON and never interpolated into HTML.

   Where a value reaches the browser as JSON, the server's auto-escaping no longer
   protects the page, so the client script MUST write every such value into the DOM
   through `textContent` or an equivalent that cannot interpret markup, and MUST
   NOT use `innerHTML` or `insertAdjacentHTML`. This applies to every value the
   task detail modal renders and to every value the graph detail panel renders (see
   [Task Detail Modal](#task-detail-modal), **Client-side rendering is text-only**,
   and [Frontend Rules](#frontend-rules), rule 6). A stored value can therefore
   alter neither page structure on the server-rendered path nor on the JSON path.
8. **Security headers on every HTML response.** Every HTML response carries the
   Content-Security-Policy, X-Content-Type-Options (`nosniff`), X-Frame-Options
   (`DENY`), and Referrer-Policy (`same-origin`) headers specified in
   [Security Headers](#security-headers). The Content-Security-Policy restricts
   every resource to the server's own origin, consistent with the no-remote-origin
   asset model.
9. **HTML-safe JSON on the graph data endpoint.** The graph data endpoint emits
   HTML-safe JSON (`<`, `>`, and `&` serialized as Unicode escape sequences), so
   roadmap-derived graph text cannot break an HTML or script context (see
   [Graph Data Endpoint](#graph-data-endpoint)).
10. **No directory listings; bounded connection timeouts and a bounded graph
   query.** The static handler never serves a directory listing: a request for a
   directory under `/static/` returns HTTP `404` (see
   [Static Assets](#static-assets)). The HTTP server is configured with explicit
   ReadHeaderTimeout, WriteTimeout, and IdleTimeout values so a slow or idle client
   cannot exhaust server resources (see
   [HTTP Server Timeouts](#http-server-timeouts)). Those three timeouts bound the
   connection and not the work a request causes, so the one route that executes
   caller-supplied input — the graph data endpoint — additionally bounds that work
   with a per-request query time budget of 5 seconds, after which the query is
   cancelled and the page shows the existing query-execution-failure message (see
   [Graph Query Time Budget](#graph-query-time-budget)). Without that budget a
   single `GET` could hold the server for as long as the caller's query took to
   run, because the injected node limit bounds the result and not the work.
11. **No stale data; `no-store` on data-derived responses.** Every data-derived
   response (the roadmap index page, the roadmap sprints page, the roadmap tasks
   page, the roadmap sprint page, the roadmap audit log page, the knowledge-graph
   page shell, the graph data
   endpoint, and the data-state-dependent error responses) carries
   `Cache-Control: no-store`, so no client-side or intermediary cache re-presents a
   state that no longer matches the database or store. Embedded `/static/...`
   assets are immutable and are excluded from this rule, remaining cacheable (see
   [Cache Policy](#cache-policy)).
12. **No second source of truth.** The web interface stores nothing of its own.
   The CLI's SQLite databases and GoGraph stores remain the single source of
   truth, and the interface holds no cache, no index, and no derived copy of them.
   The CLI is the sole write path for roadmap data. It is **not** the sole write path
   for a knowledge graph: the graph data endpoint writes into the same GoGraph
   store the CLI writes into, through the same engine and under the same lock, so
   the store stays the one place a graph lives (see
   [Security and Constraints](#security-and-constraints), rule 3).

## Acceptance Criteria

1. `rmp web` starts a server, prints the served URL to stdout as the success
   object defined in `COMMANDS.md § Web Interface`, and (unless `--no-open` is
   given) opens the default browser at that URL. With no flags it binds
   `127.0.0.1:8787` (loopback only) and prints no network-exposure warning.
   Passing `--host 0.0.0.0` binds all interfaces and prints a warning to stderr
   that the read-only interface is reachable from the network; the process still
   starts and the exit-related behaviour is unchanged.
2. `rmp web --no-open` starts the server and prints the URL without launching a
   browser.
3. `rmp web --port 8787` when port 8787 is already in use fails with exit code 1
   and a plain-text bind error naming the host and port (explicit port, no
   fallback).
4. `rmp web` (default port) when port 8787 is already in use starts successfully
   on an operating-system-chosen ephemeral port and reports that port in the
   served URL.
5. `rmp web --port 70000` fails with exit code 6 (port out of range), and
   `rmp web --port notanumber` fails with exit code 6 (non-integer port).
6. With at least one roadmap present, `GET /` returns HTTP 200 and an HTML page
   listing every roadmap discovered under `~/.roadmaps/`, with links to each
   roadmap's sprints page (the landing page, `/roadmaps/{name}`) and graph page.
   Selecting a roadmap lands the user on its sprints page with the **Actual** tab
   (the current OPEN sprint or sprints) active by default.
7. With no roadmaps present, `rmp web` still starts and `GET /` returns HTTP 200
   with an empty-state message; the absence of roadmaps is not an error.
8. `GET /roadmaps/{name}` for an existing roadmap returns HTTP 200 and an HTML
   page that renders the roadmap's sprints page: the roadmap's sprints as three
   tabs with the **Actual** tab active by default, and every sprint in every tab —
   including each OPEN sprint under Actual — rendered through the single shared
   sprint-card partial, so all sprints share identical card markup (a header
   showing the sprint `title` together with `Sprint #<ID>` and a status badge,
   the sprint description, and a footer task count) and each card links to the
   sprint's own page. The OPEN sprint under
   Actual is shown with the same card as the other sprints and is not expanded into
   an inline member-tasks board or per-task modals, using the fields and
   relationships defined in `MODELS.md` and `DATABASE.md`. The page does **not**
   render the roadmap's task board, and it contains no form, button, or link that
   submits a change.
9. `GET /roadmaps/{name}/tasks` for an existing roadmap returns HTTP 200 and an
   HTML page that renders every task of the roadmap, of any status, as a **Kanban
   board**, using the fields and relationships defined in `MODELS.md` and
   `DATABASE.md`. This is a distinct endpoint from the sprints page. The page
   renders **no** task table and offers no table view of the tasks: the board is
   the page's only task presentation, and a task's full field set is reached
   through the task detail modal a card opens. The page contains no form, button,
   or link that submits a change. `GET /roadmaps/{name}/tasks`
   for a non-existent roadmap, or a request whose `{name}` violates the
   roadmap-name rules, returns HTTP 404 without touching the filesystem outside
   `~/.roadmaps/`. Acceptance Criteria 81 to 92 define the board itself,
   Acceptance Criterion 93 fixes the modal trigger on every surface that shows a
   clickable task, Acceptance Criteria 94 to 99 fix the task detail endpoint that
   fills the modal, Acceptance Criteria 100 to 107 fix the header search, and
   Acceptance Criteria 112 to 117 fix the header's type, priority, and severity
   filters.
10. `GET /roadmaps/{name}` for a non-existent roadmap returns HTTP 404, and a
    request whose `{name}` violates the roadmap-name rules (for example
    `../etc`) returns HTTP 404 without touching the filesystem outside
    `~/.roadmaps/`.
11. On the roadmap sprints page, the roadmap's sprints are presented as three tabs
    whose labels, from left to right, are exactly **Próximos**, **Actual**, and
    **Concluídos**, and the **Actual** tab is the active tab by default when the
    page loads.
12. On the roadmap sprints page, sprints are classified into the tabs by their
    status and every sprint in every tab is rendered through the single shared
    sprint-card partial, so all sprints share identical card markup across the
    three tabs: every `PENDING` sprint appears under Próximos ordered by ascending
    sprint `Order` (the unique execution order; lowest `Order`, the next sprint to
    execute, first); every `OPEN` sprint appears under Actual ordered by ascending
    sprint `Order`; every `CLOSED` sprint appears under Concluídos ordered by
    descending sprint `Order` (highest `Order`, the last in execution order,
    first). The OPEN sprint under Actual is shown with the same card as the other
    tabs and is not expanded into an inline member-tasks board or per-task modals. A tab
    with no matching sprint shows a clear empty-state message.
13. On the roadmap sprints page, every sprint card in any tab shows a header
    presenting the sprint `title` together with `Sprint #<ID>` and a status badge,
    the sprint description, and a footer with that sprint's total task count, and
    is a clickable link to that sprint's page at
    `/roadmaps/{name}/sprints/{id}`.
14. `GET /roadmaps/{name}/sprints/{id}` for a sprint of an existing roadmap returns
    HTTP 200 and an HTML page showing all details of that sprint (id, status,
    `title`, description, execution `order`, capacity `max_tasks`, `created_at`,
    `started_at`, `closed_at`, and
    `task_count`) and the sprint's member tasks as a three-column board whose
    `WAITING` column follows the `sprint_tasks` order (the planned
    in-sprint execution order) while its `DOING` and `CLOSED` columns lead with the
    most recently started and the most recently closed task respectively; the page
    header presents the sprint `title`
    alongside `Sprint #<ID>`, and the sprint metadata datagrid shows the sprint
    `Title` and the execution `Order` in addition to the ID, Status, Capacity,
    Tasks, Created, Started, and Closed fields; the page contains no form, button,
    or link that submits a change. A request whose `{id}` is not a valid integer, or is an
    integer that is not a sprint of the named roadmap, returns HTTP 404, and a
    request whose `{name}` is invalid or nonexistent returns HTTP 404.
15. Clicking a task anywhere it is shown clickable — the board cards of the tasks
    page and the board cards of the sprint page — opens a modal
    popup that displays all of that task's fields (`id`, `title`, `status`, `type`,
    `priority`, `severity`, `functional_requirements`, `technical_requirements`,
    `acceptance_criteria`, `completion_summary`, `parent_task_id`, `subtask_count`,
    `depends_on`, `blocks`, `created_at`, `started_at`, `tested_at`,
    `closed_at`, `commit_open`, `commit_close`). The page carries one modal element, not one per
    task, and opening a task fetches that task's fields and comments from
    `GET /roadmaps/{name}/tasks/{id}/data` to fill it. The modal is read-only: it
    contains no form, no edit
    control, and no submit action, and it opens no
    write path. The modal opens from the pointer, from touch, and from the keyboard
    on every surface that shows a clickable task, and the modal and the sprint tabs
    are usable on touch input and on a small phone-sized viewport.
16. The admin-shell sidebar's per-roadmap links target the four distinct endpoints:
    the Sprints link points to `/roadmaps/{name}` (the landing page), the Tasks
    link points to `/roadmaps/{name}/tasks`, the Audit link points to
    `/roadmaps/{name}/audit`, and the Graph link points to
    `/roadmaps/{name}/graph`; the sidebar highlights whichever of the four is the
    active view.
17. `GET /roadmaps/{name}/graph` for an existing roadmap returns HTTP 200 and an
    HTML page that loads the vendored D3.js library (and the d3-sankey plugin) from
    `/static/...` (not from any remote origin) and renders an interactive node-link
    visualisation with pan and zoom that is usable with touch gestures (pan,
    pinch-to-zoom, tap to select and inspect) and surfaces node and edge detail
    without requiring a mouse hover. The page renders the **Mobile patent suits**
    layout by default and provides a dropdown offering the complete set of
    "Networks"-section D3 gallery layouts — Force-directed graph, Disjoint
    force-directed graph, Mobile patent suits, Arc diagram, Sankey diagram,
    Hierarchical edge bundling, Chord diagram, Directed chord diagram, and Chord
    dependency diagram. Selecting a layout in the dropdown re-renders the same graph
    data in that layout. When the current graph cannot be meaningfully drawn in the
    selected layout (for example a cyclic graph selected as Sankey), the page shows
    a clear, read-only in-place message instead of erroring, and the user can select
    a different layout; touch usability is preserved across all layouts.
18. `GET /roadmaps/{name}/graph/data` returns HTTP 200 and JSON in the shape
    defined in `DATA_FORMATS.md § Graph View Data`, populated from a statement run
    against the roadmap's GoGraph store.
19. After serving any number of graph page and graph data requests that carry no
    `q`, or a `q` that writes nothing, for a roadmap that has never been written,
    no `snapshot/` subdirectory exists and no checkpoint has run. For a roadmap
    that **has** been written, serving any number of those requests leaves the
    `wal` file byte for byte unchanged and every file under `snapshot/` unchanged,
    proving that a statement which wrote nothing neither checkpointed nor truncated
    the log (see `GRAPH.md § Synchronous Checkpoint on Write` and
    `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`).
20. Serving roadmap sprints pages, roadmap tasks pages, roadmap sprint pages, and
    roadmap audit log pages
    produces **no** new audit-log entry in the roadmap's `project.db` (a read is
    not a change).
21. A `POST`, `PUT`, `PATCH`, or `DELETE` request to any route returns HTTP 405.
22. A request for a `/static/...` path that is not in the embedded asset set
    returns HTTP 404, and no `/static/...` request can read a file outside the
    embedded asset set. A request for a directory path under `/static/` (for
    example `/static/` or `/static/vendor/`) returns HTTP 404 and never a directory
    listing, while a request for an individual embedded asset file returns HTTP 200.
23. Every page the interface serves loads all of its assets — the vendored Tabler
    CSS and JavaScript, the D3.js graph library and the d3-sankey plugin, the
    Tabler Icons webfont, the Inter font, and every other script, stylesheet, font,
    icon, image, and the favicon — only from `/static/...` on the same server; no
    page references a
    content delivery network, a remote font host (no Google Fonts), or any other
    remote origin, and the running server makes no outbound network request.
24. Sending `SIGINT` (`Ctrl+C`) or `SIGTERM` to a running `rmp web` shuts the
    server down gracefully and the process exits 0.
25. The deliverable is fully self-contained: the binary serves the interface with
    zero external runtime dependency. Every embedded asset category in
    [Embedded Asset Categories](#embedded-asset-categories) — HTML templates, the
    stylesheet, all client JavaScript including the D3.js bundle and the d3-sankey
    plugin and their dependencies, web fonts, icons and images, and the favicon —
    is embedded via
    `go:embed`, and the build produces a single self-contained binary (see
    `BUILD.md § Vendored Web Assets`).
26. The interface works with networking disabled and with only the `rmp` binary
    present on disk (no sidecar files and no separate assets directory): every
    page renders and functions fully, including the knowledge-graph visualisation,
    with no network egress.
27. On a small phone-sized viewport, the roadmap index page, the roadmap sprints
    page, the roadmap tasks page, the roadmap sprint page, the roadmap audit log
    page, and the knowledge-graph
    page each render without page-level horizontal scrolling — `<body>` produces no
    horizontal overflow — with readable typography and
    touch-friendly hit targets, demonstrating the mobile-first base styles. The
    horizontal scroll the tasks page's Kanban board performs inside its own
    container is not page-level overflow and does not violate this criterion (see
    Acceptance Criterion 88), and neither is the horizontal scroll the sprint page's
    member-tasks board performs inside its own container (see Acceptance
    Criterion 136).
28. On the roadmap sprints page, the roadmap sprint page, and the roadmap audit
    log page at a narrow viewport, the sprint and audit data remains usable without
    page-level
    horizontal overflow (for example through responsive or stacked tables or an
    equivalent layout) while still showing the fields and relationships defined for
    those pages. Neither page presents its tasks as a table: the roadmap tasks page
    presents them as a board, whose narrow-viewport behaviour Acceptance
    Criterion 88 covers, and the roadmap sprint page presents its member tasks as a
    board, whose narrow-viewport behaviour Acceptance Criterion 136 covers.
29. Every HTML page the interface serves includes the responsive viewport meta
    tag, and no page loads a CSS framework or reset from a remote origin; the
    Tabler CSS framework in use is vendored and served from `/static/...`.
30. Every page renders in the Tabler admin-shell layout — a navigation sidebar
    (listing the roadmaps and, within a roadmap, that roadmap's Sprints, Tasks,
    Audit, and Graph views), a top navbar naming the selected roadmap, and a page
    header — using Tabler cards, tables, and badges, and the interface renders in
    Tabler's dark theme.
31. On a small phone-sized viewport, the admin-shell navigation sidebar is not
    shown expanded inline; it collapses to an off-canvas (hamburger) menu that the
    user can open, so each page stays usable without horizontal overflow.
32. Multi-line free-text authored through the CLI renders preserving its source
    line breaks: the task detail modal's long free-text fields
    (`functional_requirements`, `technical_requirements`, `acceptance_criteria`,
    and `completion_summary`), every comment `body` shown in the modal's comments
    timeline and in the sprint Comments card, and a sprint's `description` — shown
    in the sprint cards on the roadmap sprints page (across all three tabs) and on
    the roadmap sprint page — each display the author's
    newlines rather than collapsing them, while the text still wraps without forced
    horizontal scrolling and remains HTML-escaped through `html/template` (never
    rendered as raw HTML).
33. Every HTML response carries the security headers: `Content-Security-Policy`
    with the value `default-src 'self'; script-src 'self'; style-src 'self'
    'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self';
    frame-ancestors 'none'; base-uri 'self'`, `X-Content-Type-Options: nosniff`,
    `X-Frame-Options: DENY`, and `Referrer-Policy: same-origin` (see
    [Security Headers](#security-headers)).
34. The embedded HTTP server is configured with `ReadHeaderTimeout` of 10 seconds,
    `WriteTimeout` of 30 seconds, and `IdleTimeout` of 120 seconds (see
    [HTTP Server Timeouts](#http-server-timeouts)).
35. `GET /roadmaps/{name}/graph/data` returns JSON in which the characters `<`,
    `>`, and `&` appearing in graph-derived strings are emitted as their Unicode
    escape sequences (`<`, `>`, `&`), proving HTML escaping is enabled
    in the JSON encoder (see [Graph Data Endpoint](#graph-data-endpoint)).
36. Binding a non-loopback host (for example `rmp web --host 0.0.0.0`) prints a
    network-exposure warning to stderr, while binding a loopback host (the default,
    or `rmp web --host 127.0.0.1`) prints no such warning.
37. Every data-derived response carries the `Cache-Control: no-store` header: the
    roadmap index page (`/`), the roadmap sprints page (`/roadmaps/{name}`), the
    roadmap tasks page (`/roadmaps/{name}/tasks`), the roadmap sprint page
    (`/roadmaps/{name}/sprints/{id}`), the roadmap audit log page
    (`/roadmaps/{name}/audit`), the knowledge-graph page shell
    (`/roadmaps/{name}/graph`), the graph data endpoint
    (`/roadmaps/{name}/graph/data`), and the data-state-dependent error responses
    (for example a `404` for a missing roadmap or sprint and a `500` from a read
    failure). A response for a `/static/...` asset does **not** carry
    `Cache-Control: no-store` and remains cacheable (see
    [Cache Policy](#cache-policy)).
38. On the roadmap sprints page, every sprint in every tab — Próximos, Actual, and
    Concluídos — is rendered through the single shared sprint-card partial, so all
    sprints use identical card markup. The OPEN sprint under the Actual tab is shown
    with the same card as the other sprints: it shows the header (`Sprint #<ID>`
    with a status badge), the sprint description, and the footer task count, and it
    is not expanded into an inline sprint metadata datagrid, member-tasks board, or
    per-task modals on the sprints page. The full sprint detail block (sprint status
    summary line, metadata datagrid, member-tasks board, and Comments card) is shown
    only on the single Roadmap Sprint Page (see
    [Shared Sprint-Card Partial](#shared-sprint-card-partial) and
    [Sprint Detail Sub-Template](#sprint-detail-sub-template)).
39. At the top of the full sprint presentation on the single Roadmap Sprint Page, a
    sprint status summary line is shown in the
    exact format `<pct>% - P:<p> A:<a> C:<c> - T:<t>` (for example
    `33% - P:8 A:29 C:18 - T:55`), where `<pct>` is the completion percentage
    (`COMPLETED` tasks divided by total tasks, rounded to the nearest integer
    percent, and `0%` when the sprint has no tasks), `P` is the count of the
    sprint's tasks in `BACKLOG` or `SPRINT`, `A` is the count in `DOING` or
    `TESTING`, `C` is the count in `COMPLETED`, and `T` is the sprint's total task
    count; every value counts only the sprint's own member tasks. `P`, `A`, and `C`
    partition the sprint's tasks and therefore always sum to `T`: the three
    categories cover all five values of the task status enum, and `tasks.status`
    admits no sixth value (`MODELS.md § Enums` and `DATABASE.md § tasks Table`). For
    a sprint with, for example, 55 member tasks of which 8 are pending, 29 are in
    progress, and 18 are completed, the line reads
    `33% - P:8 A:29 C:18 - T:55` (18 of 55 completed rounds to 33%).
40. Every sprint card under any tab of the roadmap sprints page — Próximos, Actual,
    and Concluídos — displays that sprint's total number of tasks in its footer.
41. When `rmp web` starts against a roadmap whose on-disk `project.db` is at an
    older schema version than the binary expects, the server migrates that
    roadmap's schema to the current version automatically at startup, before
    binding the listener and without any user input, so that the roadmap's sprints
    page, tasks page, and sprint page subsequently return HTTP 200 rather than an
    HTTP 500 caused by a missing column. A roadmap already at the current schema
    version is left unchanged (the startup migration is a no-op for it). Per-request
    handlers open every database read-only (SQLite `query_only`) and never run a
    migration; the startup migration is the only path on which the web interface
    writes to a roadmap database (see
    [Startup Schema Migration](#startup-schema-migration) and
    [Tasks and Sprints from SQLite](#tasks-and-sprints-from-sqlite)).
42. When a single roadmap cannot be migrated at startup (for example its database
    is unreadable, locked, or corrupt), `rmp web` logs an informational message to
    stderr naming that roadmap and still starts, serving every other roadmap; the
    failed roadmap remains at its on-disk schema, and a later request that needs a
    column its stale schema lacks surfaces as an HTTP 500 on the affected route
    (see [Startup Schema Migration](#startup-schema-migration)).
43. On the roadmap knowledge-graph page, a labels sidebar column is rendered inside
    the graph card to the left of the graph canvas. It lists, in two clearly
    separated sections, every distinct node label with a count of the nodes that
    carry it (a node with multiple labels counts towards each of its labels) and
    every distinct edge type with a count of the edges of that type, with the
    entries in each section sorted deterministically by name and the Node labels
    section shown before the Edge types section. Each section header shows an
    absolute total alongside its title: the Node labels header shows the total
    number of distinct nodes in the current graph result and the Edge types header
    shows the total number of edges. A section with no entries, and an
    empty graph (both sections empty), render gracefully with a clear empty-state
    indication and are not errors. The inventory and counts are computed
    client-side from the data already fetched from `GET /roadmaps/{name}/graph/data`
    (the `labels` arrays of the nodes and the `type` field of the edges); the
    feature adds no new server endpoint and no new write path (see
    [Graph Labels Sidebar](#graph-labels-sidebar)).
44. The labels sidebar highlights rather than filters: selecting a node-label entry
    highlights all nodes carrying that label and selecting an edge-type entry
    highlights all edges of that type, while non-matching elements are dimmed
    (reduced opacity) and remain on the canvas rather than being removed. Multiple
    entries can be active at once across both sections, and the highlighted set is
    the union of the active selections. Each entry is a toggle: selecting an active
    entry again toggles it off, every active entry is visually indicated as
    selected, and clearing all selections restores the normal non-dimmed view. The
    highlight state coexists with the layout dropdown (the active selections still
    apply after a layout change) and with the node/edge detail panel (selecting an
    element on the canvas still opens its detail, even when that element is dimmed).
    Each sidebar entry is a touch-friendly target that toggles on tap (see
    [Graph Labels Sidebar](#graph-labels-sidebar)).
45. The knowledge-graph page renders a query bar at the top of the page with three
    controls in left-to-right order: an editable query box pre-filled on page load
    with the default query `MATCH (n) OPTIONAL MATCH (n)-[r]->(m) RETURN n, r, m`, a
    Search button, and a node-limit dropdown offering exactly the six values `50`,
    `100`, `250`, `500`, `1000`, and `3000` with `100` selected by default. On page
    load the page fetches the graph data with the default query and the default
    limit and renders the full-graph view (see [Graph Query Bar](#graph-query-bar)).
46. Selecting the Search button re-fetches `GET /roadmaps/{name}/graph/data` with
    the current query box text as the `q` parameter and the current dropdown value
    as the `limit` parameter, and re-renders the graph from the response in the
    currently selected layout. The request is GET-only and carries `q` and `limit`
    as URL query parameters; no `POST`, no request body, and no new endpoint is
    used. A request to `GET /roadmaps/{name}/graph/data` with **no** `q` parameter
    runs the default query and returns the full-graph view, exactly as the endpoint
    behaved before the query bar existed (backward compatible).
47. **A statement submitted through the query bar is executed whatever it does,
    and the response status alone does not establish this criterion.** A request
    whose `q` is `CREATE (n:WebProbe {key:'p'})` is answered HTTP `200`, and a
    `rmp graph execute` invocation in a separate process afterwards reports the
    `WebProbe` node present; a request whose `q` is
    `MATCH (n:WebProbe) DETACH DELETE n` is answered HTTP `200`, and the same
    read-back afterwards reports it gone. Each of the two leaves the store
    checkpointed: `snapshot/manifest.json` exists and the `wal` file is truncated.
    Neither statement carries a top-level `RETURN`, so neither is injected into,
    which is what makes this criterion reachable at all: an endpoint that appended
    the node `LIMIT` to either would hand the engine a statement that fails in the
    parser and would answer `400` (Suppression 2 of
    [Graph Data Endpoint](#graph-data-endpoint), and Acceptance Criterion 111). An
    endpoint that refused either request, and an endpoint that answered `200` while
    storing nothing, both fail this criterion — the second is why the read-back is
    required (see [Graph Data Endpoint](#graph-data-endpoint) and
    `GRAPH.md § Engine Constructor by Path`).
48. The endpoint applies the node limit by appending `LIMIT <n>` only when the
    user's query both lacks a top-level `LIMIT` of its own and is a statement form
    that admits a `LIMIT` clause (Acceptance Criterion 111 covers the forms that do
    not): a request whose `q` has no top-level `LIMIT` returns at most the resolved
    limit's worth of results (the dropdown value, or `100` when `limit` is absent),
    while a request whose `q` already contains its own top-level `LIMIT` keeps that
    `LIMIT` and the dropdown value is not applied. The existing-`LIMIT` detection
    runs on the masked normalization, so a `LIMIT` keyword appearing only inside a
    string literal, a comment, or a backtick-quoted identifier does not count as an
    existing top-level `LIMIT` and does not suppress injection. The injected clause
    is separated from the query by a newline, never by a space, so a query whose
    last line ends in a line comment (`MATCH (n) RETURN n //`) still has the limit
    applied: the comment does not swallow the injected clause, and the endpoint
    does not return the whole graph. A `limit` parameter that is not one of the six
    allowed values is rejected
    as an invalid limit and the query is not executed; the request is answered
    HTTP `400 Bad Request` with a JSON body whose `kind` is `invalid_limit`, and
    the page surfaces a clear invalid-limit message naming the rejected value (see
    [Graph Data Endpoint](#graph-data-endpoint) and
    [Query-Bar Error Handling](#query-bar-error-handling)).
49. The endpoint builds the `{"nodes": [...], "edges": [...]}` response by walking
    the entire query result and collecting every node and every relationship that
    appears anywhere in it — in any returned column and recursively inside lists,
    maps, and paths — deduplicating nodes by node `id` and relationships by
    relationship `id`. A relationship is included only when both its start node and
    its end node are present in the collected node set; a relationship with a
    missing endpoint is dropped and no synthetic endpoint node is created, so every
    `startId` and `endId` in the returned `edges` references a node present in the
    returned `nodes` (see [Graph Data Endpoint](#graph-data-endpoint) and
    `DATA_FORMATS.md § Graph View Data`, rule 3).
50. A statement submitted through the query bar that fails in the engine (for
    example, invalid Cypher syntax) surfaces a clear "query failed to execute"
    message on the page, distinct from the invalid-limit message. In both
    query-bar failure cases — invalid limit and execution failure — the message is
    shown in place, the page does not crash, and the failure triggers no
    navigation, consistent with the graceful layout degradation; the user can edit
    the statement or change the limit and search again. Both failures are answered
    HTTP `400 Bad Request`, and the body's `kind` is what tells them apart
    (Acceptance Criterion 123; see
    [Query-Bar Error Handling](#query-bar-error-handling)).
51. The labels sidebar shows an absolute total in each section header, derived
    client-side from the same already-fetched graph data as the per-entry
    inventory: the Node labels header shows the total number of distinct nodes in
    the current graph result and the Edge types header shows the total number of
    edges. Because a node carrying multiple labels counts towards each of its
    labels, the sum of the per-label entry counts may exceed the distinct-node
    total; the Node labels total is the distinct-node count, not the sum of the
    per-label counts, while the Edge types total equals the sum of the per-type
    counts. The totals recompute on each search together with the rest of the
    inventory, and in an empty graph both totals render as `0` without error. The
    totals add no new server endpoint and no new write path (see
    [Graph Labels Sidebar](#graph-labels-sidebar)).
52. The labels sidebar has a touch-friendly icon control at its top that toggles the
    sidebar between expanded and collapsed, built with the page's existing
    Tabler-based UI. When collapsed, the sidebar column contracts so the graph
    canvas takes the full width of the graph card and only the control to expand it
    again remains visible; when expanded, the section headers, their totals, and the
    entries are shown. Toggling the control changes only the sidebar's visibility
    and the canvas width: it does not clear the active highlight selections, change
    the layout, run a search, or open or close the detail panel, and an active
    highlight remains active across a collapse and a subsequent expand. The sidebar
    starts expanded on each page load; persistence of the collapsed or expanded
    state across reloads is not required (see
    [Graph Labels Sidebar](#graph-labels-sidebar)).
53. With the query box focused, pressing Ctrl+Enter triggers the search exactly as
    selecting the Search button does: it issues the same GET request to
    `GET /roadmaps/{name}/graph/data` with the current query box text as the `q`
    parameter and the current dropdown value as the `limit` parameter, applies the
    same limit validation, re-renders the graph in the
    currently selected layout on success, and surfaces the same in-place error
    messages on failure (see criterion 46). Ctrl+Enter is a keyboard accelerator for
    the existing Search action and changes no other behaviour. Plain Enter in the
    query box does not trigger a search; it inserts a newline so the user can compose
    a multi-line query (see [Graph Query Bar](#graph-query-bar)).
54. Selecting a node in the graph canvas opens that node's detail panel and puts
    the canvas into neighbor focus: the selected node, its first-degree neighbours,
    and the edges incident to the selected node are emphasised, and every other
    element — second-degree nodes and beyond, and every edge not incident to the
    selected node — is dimmed (reduced opacity) rather than removed, using the same
    dim-not-remove mechanism as the labels-sidebar highlight. The first-degree
    neighbourhood is undirected: it includes every node connected to the selected
    node by exactly one edge in either direction (the target of an outgoing edge or
    the source of an incoming edge) together with those incident edges. Neighbor
    focus only emphasises and dims; it adds or removes no node or edge (see
    [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)).
55. Neighbor focus is cleared by selecting the focused node again, selecting an
    empty area of the canvas, or closing the node detail panel; any of these
    gestures closes the detail panel and clears the focus together. Clearing the
    focus restores the prior view: the canvas returns to the labels-sidebar
    highlight state when any label or type entry is still active, otherwise to the
    normal, non-dimmed view. Selecting a different node while one is focused moves
    the focus to the new node without an intervening clear (see
    [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)).
56. While a node is focused, neighbor focus takes precedence over the
    labels-sidebar highlight: the neighbor-focus emphasis governs the canvas
    dimming and an active label or type selection does not drive canvas dimming,
    though the sidebar entries may stay visually selected in the sidebar. Changing
    the layout in the layout dropdown reapplies the current neighbor focus to the
    re-rendered layout, emphasising the same node, neighbours, and incident edges,
    while running a search from the query bar clears the neighbor focus together
    with re-rendering the new result. Neighbor focus is driven by the same
    touch-friendly tap that opens and closes the detail panel, is computed and
    applied entirely client-side from the already-fetched graph data, and adds no
    new server endpoint and no write path, leaving the graph data endpoint's
    response shape and the page's read-only behaviour unchanged (see
    [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page) and
    [Graph Labels Sidebar](#graph-labels-sidebar)).
57. `GET /roadmaps/{name}/audit` for an existing roadmap returns HTTP 200 and an
    HTML page that renders the roadmap's full audit log as a read-only table whose
    columns are the `AuditEntry` fields defined in `MODELS.md` and `DATABASE.md`
    (`ID`, `Operation`, `Entity Type`, `Entity ID`, and `Performed At`), with the
    entries ordered by `performed_at` descending (most recently performed operation
    first). The table is read-only: it has no clickable row action, no modal, and no
    edit affordance, and the page contains no form, button, or link that submits a
    change. `GET /roadmaps/{name}/audit` for a non-existent roadmap, or a request
    whose `{name}` violates the roadmap-name rules, returns HTTP 404 without touching
    the filesystem outside `~/.roadmaps/` (see
    [Roadmap Audit Log Page](#roadmap-audit-log-page)).
58. The audit log page is paginated at a fixed page size of 100 entries per page,
    selected by a 1-based `page` query parameter that defaults to 1 when absent. The
    total page count is `ceil(total_entries / 100)` with a minimum of 1 page. A
    `page` value below 1, a non-integer or garbage `page` value, and a `page` value
    beyond the last page are each clamped to the nearest valid page (1 or the last
    page) and still return HTTP 200; the audit page never returns HTTP 404 for an
    out-of-range or unparseable `page` value. When the audit log is empty, the page
    returns HTTP 200 with a clear empty-state message and shows page 1 of 1 (see
    [Roadmap Audit Log Page](#roadmap-audit-log-page)).
59. The audit card's footer shows read-only Previous and Next navigation controls
    and a "Page X of Y" indicator, using accessible Tabler pagination markup. The
    Previous control is disabled or absent on the first page and the Next control is
    disabled or absent on the last page. The controls are `GET` links that change
    only the `page` query parameter — no form and no write path. A fixed
    100-entries-per-page request is always within the audit hard cap
    (`MaxAuditLimit` = 500; see `DATABASE.md § Audit Result Limit`), so the page-size
    request never exceeds the cap (see
    [Roadmap Audit Log Page](#roadmap-audit-log-page)).
60. The Roadmap Sprints Page tab control follows Tabler's "card with tabs" example:
    the tab list is a single
    `<ul class="nav nav-tabs card-header-tabs" data-bs-toggle="tabs" role="tablist">`
    placed inside the card header (not a card title in the header with a separate
    `nav-tabs` list in the card body), tab activation uses Bootstrap's native tabs
    behaviour via `data-bs-toggle="tabs"`, and the three tabs (Próximos, Actual,
    Concluídos) with their count badges and the default-active **Actual** tab are
    preserved exactly as specified, including the semantic colour of each tab's count
    badge (Acceptance Criterion 120; see [UI Framework](#ui-framework), rule 9, and
    [Roadmap Sprints Page](#roadmap-sprints-page)).
61. Every status, priority, and severity badge uses the semantically meaningful
    Tabler colour variant assigned to its value in
    [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
    not a single fixed colour: a `COMPLETED` task and a `CLOSED` sprint render
    `bg-green-lt`, a `DOING` task and an `OPEN` sprint render `bg-blue-lt`, a
    `TESTING` task renders `bg-yellow-lt`, a `SPRINT` task renders `bg-cyan-lt`, and a
    `BACKLOG` task and a `PENDING` sprint render `bg-secondary-lt`; a priority in
    `7`-`9` renders `bg-red-lt`, `4`-`6` renders `bg-yellow-lt`, and `0`-`3` renders
    `bg-secondary-lt`; a severity in `8`-`9` renders `bg-red-lt`, `6`-`7` renders
    `bg-orange-lt`, `3`-`5` renders `bg-yellow-lt`, and `0`-`2` renders
    `bg-secondary-lt`. The same value maps to the same colour everywhere a badge for
    it is shown — the priority and severity badges on the tasks page's board cards
    and on the cards of the sprint detail member-tasks board, the task
    detail modal, the sprint cards, the Roadmap Sprint Page header and metadata
    datagrid, the sprints-page tabs, where the colour is the variant of the status
    the tab groups while the badge text is that tab's sprint count (Acceptance
    Criterion 120), and the per-column count badge of each of the two Kanban boards,
    where the colour is the variant of the status the column groups while the badge
    text is that column's task count (Acceptance Criterion 140) — and the mapping
    introduces no enum value beyond those defined in `MODELS.md` and
    `STATE_MACHINE.md`.
62. No template carries a presentational inline `style="..."` attribute: all styling
    is provided by vendored Tabler classes and utilities or by the project override
    stylesheet (`static/style.css`). In particular, the navigation sidebar's
    per-roadmap section label is a Tabler `subheader` element preceded by a Tabler
    `dropdown-divider` rule and aligned with the sidebar links by the `px-3` spacing
    utility, rather than an inline-styled label, and the empty-state icon's sizing
    lives in a Tabler utility class or in `static/style.css` rather than in an
    inline `style` attribute. Every framework class name a template uses is present
    in the vendored `tabler.min.css`: a search of the templates for `navbar-heading`
    or for `navbar-divider` returns no match, and `static/style.css` carries no rule
    whose selector targets a framework class the vendored distribution does not
    define (see [UI Framework](#ui-framework), rule 10).
63. The templates follow Tabler's markup idioms in the minor markup-fidelity places:
    page-header rows use Tabler's `row g-2 align-items-center` gutter and alignment
    classes, and the sidebar brand uses the Tabler
    `<h1 class="navbar-brand navbar-brand-autodark">` element. These are
    markup-fidelity adjustments only: the
    read-only nature of the interface and the content shown are unchanged (see
    [UI Framework](#ui-framework), rule 11).
64. The task detail modal renders the task's comments as a timeline placed after the
    completion-summary block and last in the modal body. For a task with comments,
    the modal contains a `<ul class="timeline">` whose `<li class="timeline-event">`
    items appear oldest first, in the same order `rmp task comment-list` returns for
    that task, and every comment of the task is present — no type filter and no count
    limit (see [Task Detail Modal](#task-detail-modal)).
65. Each timeline entry shows the comment's type as a badge, its `created_at`
    timestamp, its `body` with the author's line breaks preserved, and — only when
    `updated_at` is not null — the `updated_at` timestamp marking the entry as
    edited. A comment whose `updated_at` is null shows no edited marker.
66. The comment type badge uses the neutral `bg-secondary-lt` variant for all seven
    type values, in both the task detail modal and the sprint Comments card. No
    per-type colour is introduced, and the semantic mapping in
    [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours)
    is unchanged (Acceptance Criterion 61 continues to hold).
67. A task with no comments opens a modal that shows a clear empty-state message in
    place of the timeline, not an empty list and not a missing section.
68. The Roadmap Sprint Page renders a Comments card after the member-tasks board,
    as the last card of the sprint detail sub-template. It shows the sprint's own
    comments oldest first, in the same order `rmp sprint comment-list` returns, with
    a card header titled `Comments` carrying a badge with the comment count. A sprint
    with no comments still renders the card, showing an empty-state message in place
    of the timeline (see [Sprint Detail Sub-Template](#sprint-detail-sub-template)).
69. The sprint Comments card shows only the sprint's own comments. A comment written
    against a member task appears in that task's detail modal and nowhere in the
    Comments card, and no aggregate of task comments is presented at sprint level.
70. Rendering a page that shows N clickable tasks never issues one comment query per
    task: an instrumented count of comment queries is independent of N on every such
    page. On the tasks page the count is 1 — a single grouped **counting** query for
    all N cards, and no comment-listing query at all, because the board shows counts
    and no comment text (see
    `DATABASE.md § Count Comments for Many Parents (Grouped)`). On the sprint page it
    is 2, whatever N is: one listing query for that sprint's **own** comments, which
    the Comments card renders in full as a log (see `DATABASE.md § Comments`), plus
    one grouped **counting** query over the whole set of rendered member-task ids,
    which is what gives each board card its comment number. The sprint page issues no
    comment-listing query for a member task, so it reads the comment **body** of no
    task it renders. A page that renders no task issues no task-comment query of
    either kind: a sprint with no member task skips the grouped count entirely, while
    still issuing the sprint's own comment listing, because the Comments card is
    always present. A task's comment bodies are read only when a user opens
    that task's modal, one task at a time (see
    [Task Detail Endpoint](#task-detail-endpoint)).
71. The comments timeline uses only the Tabler Timeline classes already present in
    the vendored `tabler.min.css` (`timeline`, `timeline-event`,
    `timeline-event-icon`, `timeline-event-card`). The feature adds no CSS file, no
    JavaScript file, and no vendored asset, and no template carries a presentational
    inline `style` attribute for it (Acceptance Criterion 62 continues to hold).
72. Neither the modal timeline nor the sprint Comments card contains a form, an
    input, a button, or a link that submits a change. There is no route, no
    endpoint, and no client-side path through which the web interface can create,
    edit, or delete a comment; the CLI remains the sole write path.
73. Comment text is escaped exactly as every other roadmap-derived value: a comment
    body containing HTML control characters is rendered as text and cannot alter the
    page structure, in the modal and in the Comments card alike (see
    [Security and Constraints](#security-and-constraints)).
74. Every page's admin shell places, inside `<div class="page">` and in this order,
    the sidebar `<aside>`, the top `<header class="navbar ... d-print-none">`, and
    `<div class="page-wrapper">`, which holds the page header and the page body. The
    top `<header>` is a sibling of `<div class="page-wrapper">` and
    is never nested inside it, which is the shape the vendored stylesheet's
    `.navbar-vertical~.navbar` and `.navbar-vertical~.page-wrapper` offset rules
    require. No page renders a `<footer>` element: the page body is the last region
    inside `<div class="page-wrapper">` on every page, including the knowledge-graph
    page (see [UI Framework](#ui-framework), rule 12).
75. The sidebar's collapsible region carries `class="collapse navbar-collapse"` and
    `id="sidebar-menu"`, is rendered as a `<nav>` element with `aria-label="Sidebar"`,
    and lives inside the sidebar `<aside>`. Exactly one `navbar-toggler` in the
    rendered page targets `#sidebar-menu`, and it lives inside that same `<aside>`;
    the top navbar carries neither a second toggler for `#sidebar-menu` nor a second
    brand, so each page renders exactly one `navbar-brand` element (see
    [UI Framework](#ui-framework), rules 11 and 13).
76. The active navigation entry is marked twice: its `<li class="nav-item">` carries
    the `active` class and the `<a class="nav-link">` inside it carries
    `aria-current="page"`. This holds for the sidebar's roadmap-index entry and for
    the active view among a roadmap's Sprints, Tasks, Audit, and Graph links (see
    [UI Framework](#ui-framework), rule 14).
77. The audit log page's `<ul class="pagination">` list sits inside a `<nav>` element
    carrying a descriptive `aria-label`. The pagination structure and behaviour
    specified in Acceptance Criteria 58 and 59 are unchanged (see
    [UI Framework](#ui-framework), rule 15, and
    [Roadmap Audit Log Page](#roadmap-audit-log-page)).
78. Every page header that carries actions emits its actions column as
    `<div class="col-auto ms-auto d-print-none">` (see
    [UI Framework](#ui-framework), rule 16).
79. Every page carries `class="layout-fluid"` on `<body>` and uses `container-xl` for
    its shell containers: the top navbar, the page header, and the page body. No
    page container uses `container-fluid`; the only `container-fluid` in
    the shell is the one inside the sidebar `<aside>`, which is Tabler's own
    vertical-navbar markup (see [UI Framework](#ui-framework), rule 17).
80. Every page renders its page body as `<main class="page-body">`, the element
    Tabler's built admin shell uses, so each page exposes exactly one `main`
    landmark holding that page's own content. No page renders the page body as a
    `<div>` (see [UI Framework](#ui-framework), rule 18).
81. The roadmap tasks page's Kanban board renders exactly five columns, one per
    `TaskStatus` value, ordered left to right `BACKLOG`, `SPRINT`, `DOING`,
    `TESTING`, `COMPLETED` — the order of the task state machine's flow. Each
    column title is the status identifier in upper case, untranslated. The five
    columns are fixed: all of them are rendered on every request, in that order,
    whatever the roadmap's data contains, and neither the set of columns nor their
    order varies with the data. The board renders no sixth column and no "other"
    column, because `tasks.status` is restricted to those five values by a CHECK
    constraint (see [Roadmap Tasks Page](#roadmap-tasks-page),
    `MODELS.md § Enums`, `STATE_MACHINE.md § Task State Machine`, and
    `DATABASE.md § tasks Table`).
82. Every task of the roadmap appears on the board exactly once, as one card in the
    column matching that task's `status`. No task is omitted and no task is
    duplicated: for a roadmap with N tasks, the five column counts sum to exactly N,
    and a task whose status changes appears only in the column of its new status on
    the next request. This holds for every N, with no upper bound: the page's task
    read carries no limit and no pagination, and the `rmp task list` display default
    of `100` is not applied to it. For a roadmap holding more than 100 tasks the
    board renders all of them and the column counts still sum to N, so no count the
    page prints is ever a count of a truncated result (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Unbounded read**, and
    `DATABASE.md § Main SQL Queries`, "List All").
83. Each column header shows the status name together with a Tabler badge carrying
    the number of tasks in that column. A column holding no task shows the count
    `0`, and the count of each column equals the number of cards rendered in it.
84. Within every column the cards appear in a deterministic order: descending
    `priority`, and ascending `created_at` for tasks of equal priority — the default
    `ListTasks` ordering (`ORDER BY priority DESC, created_at ASC`; see
    `DATABASE.md § Main SQL Queries`, "List All"). Grouping the tasks into columns
    preserves that relative order, so the cards of one column follow the same
    relative order the page's read returned, and the board applies no second sort of
    its own.
85. Each card of the roadmap tasks page's Kanban board shows, in order: a reference
    line carrying `#<id>` and the task's `type` as muted text with no colour applied
    to the type; the task
    `title` as the card's prominent main content; a `priority` badge reading `P`
    immediately followed by the task's priority and a `severity` badge reading `S`
    immediately followed by the task's severity, with no space and no separator
    between the letter and the digits — a task of priority `5` and severity `3`
    shows `P5` and `S3`, and a badge carrying the bare integer does not satisfy this
    criterion — each coloured by the mapping in
    [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours)
    applied to the value alone and not to the prefixed text, so the prefix changes no
    badge colour (Acceptance Criterion 61 continues to hold); and a metadata footer
    listing only
    the indicators the task actually has, among the sprint the task belongs to, its
    `subtask_count`, its number of `depends_on` entries, its number of `blocks`
    entries, and its number of comments. The card shows **no status badge**, because
    the column already states the status. An indicator whose value is absent, empty,
    or zero renders nothing at all — no dash and no placeholder — and a task with
    none of the five indicators renders no metadata footer. That absent-metadata rule
    is this board's own: the card of the sprint's member-tasks board is not governed
    by it and always renders both of its counters (Acceptance Criterion 134). The
    prefix belongs to the board card and to nothing else: the same task's `priority`
    and `severity` in the
    task detail modal render as the bare integer beside the field name that already
    names it (Acceptance Criterion 15 continues to hold), and the card's accessible
    name carries neither value and therefore carries no prefix (Acceptance Criterion
    86 continues to hold).
86. Selecting a board card opens the read-only task detail modal for that task,
    which displays that task's full field set as specified in Acceptance Criterion
    15. Opening the modal fetches that task's data from
    `GET /roadmaps/{name}/tasks/{id}/data` and reaches no write path; that request
    is made on demand, not while the page renders. The card is a
    `<button type="button">`, so it is focusable and activatable natively: a
    pointer click, a touch tap, Enter, and Space each open the modal, and no
    JavaScript is added to make that work. The card carries no `tabindex` and no
    `role="button"`, both redundant on a button, and its accessible name is
    `Open details for task #<id>: <title>`, so a card can be opened without a
    pointing device and can be named aloud by a speech-input user from the title it
    displays (see [Roadmap Tasks Page](#roadmap-tasks-page) and
    [Task Detail Modal](#task-detail-modal)).
87. The board is read-only. It offers no drag-and-drop, and no control of any other
    kind that moves a task between columns, reorders cards, changes a task's status,
    or creates or edits a task or a column. The page contains no form, button, or
    link that submits a change, and the `rmp` CLI remains the sole write path. This
    is a deliberate divergence from the GitLab issue board the layout is modelled
    on: the inspiration is structural (columns per state, cards, per-column counts)
    and never interactive (see [Roadmap Tasks Page](#roadmap-tasks-page)).
88. A column holding no task renders a clear, unobtrusive empty state inside the
    column, in place of the card list, while the column, its title, and its `0`
    count badge stay visible. A roadmap with no task at all returns HTTP 200 and
    renders the board with all five columns present, each showing that in-column
    empty state; the page never replaces the board with a page-level empty state and
    never hides or drops a column. The five columns are presented side by side, and
    when they do not fit the viewport the board scrolls horizontally inside its own
    container while the page itself does not scroll horizontally (Acceptance
    Criterion 27 continues to hold). Each column scrolls vertically and independently
    when its card list exceeds the available height, which is the board's own height
    and is fixed by Acceptance Criterion 124. On a narrow viewport each column
    keeps a minimum width at which its cards stay legible, the horizontal board
    scroll is reachable by a touch gesture, and the cards and badges present
    touch-friendly hit targets (see
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
    rule 9).
89. Rendering the tasks page for a roadmap with at least one task issues exactly
    three reads: the roadmap's full task list, one grouped query returning the
    comment **count** of every task rendered, and one grouped query that resolves
    the sprint of every task rendered. The page reads no comment **body**: an
    instrumented count of comment-listing queries for the tasks page is 0, and of
    comment-counting queries is 1, independent of the number of tasks. For a
    roadmap with no task the page issues the task-list read
    only, and neither grouped query. Grouping the tasks into the five columns,
    counting each column, and matching each card to
    its sprint are performed in memory over the results already
    read, so the board adds no further query — none per column and none per card —
    and the query count is independent of the number of tasks, sprints, and columns
    (see `DATABASE.md § Count Comments for Many Parents (Grouped)` and
    `DATABASE.md § Resolve the Sprint of Many Tasks (Grouped)`).
90. The board's markup obeys the rules already in force and introduces no exception:
    no template carries a presentational inline `style` attribute, and every class
    the board emits is defined either in the vendored Tabler distribution or in the
    project override stylesheet `static/style.css` (Acceptance Criterion 62 continues
    to hold). The board reuses Tabler's own components where Tabler provides them —
    Tabler cards for the task cards, the card-header idiom for the column headers,
    Tabler badges for the counts and for the priority and severity values, and
    Tabler's empty-state markup for an empty column — and the column strip's layout
    and scrolling rules live in `static/style.css`, because the vendored distribution
    ships no board or Kanban component. The page's admin shell and page header are
    unchanged (Acceptance Criteria 74 to 76 and 78 to 80 continue to
    hold; see [UI Framework](#ui-framework), rules 8 and 10).
91. The card of a task that belongs to a sprint shows that sprint in its metadata
    footer, identified by the sprint `title` together with `Sprint #<id>`, as plain
    text and not as a link. It names exactly one sprint and never a list, because
    `sprint_tasks.task_id` carries a `UNIQUE` constraint and a task therefore
    belongs to at most one sprint. The card of a task that belongs to no sprint
    shows no sprint indicator at all: no dash, no "None", and no empty slot. A task
    with no sprint and none of the other four indicators renders no metadata footer
    (Acceptance Criterion 85 continues to hold; see
    [Roadmap Tasks Page](#roadmap-tasks-page), `MODELS.md § Sprint`, and
    `DATABASE.md § Relationships`).
92. Resolving the sprint of the rendered tasks issues exactly one query for the
    whole set of rendered task ids, not one per task and not one per board column:
    an instrumented count of sprint-resolution queries for a tasks page rendering N
    tasks is 1, independent of N and of how many distinct sprints those tasks
    belong to. A tasks page that renders no task issues no sprint-resolution query
    at all. This is measured the same way Acceptance Criterion 70 measures the
    comment-query count (see
    `DATABASE.md § Resolve the Sprint of Many Tasks (Grouped)`).
93. On every surface that renders a clickable task, the element that opens the task
    detail modal is a `<button>` in the served HTML, activatable by pointer, touch,
    Enter, and Space. No modal trigger anywhere in the served HTML is a
    `<div>` or a `<tr>` carrying `role="button"`, and none relies on `tabindex` to
    be reachable in place of being activatable. On both boards the trigger is the
    board card itself, rendered as `<button type="button">`: on the roadmap tasks
    page and on the Roadmap Sprint Page alike. No `<tr>` in the served HTML is a
    modal trigger or carries one, on any page.
    Each trigger's accessible name is `Open details for task #<id>: <title>`,
    carrying the task's `id` and its `title`, on both surfaces. In particular the
    name contains the task title, which is the trigger's visible label on both
    boards, so the accessible name contains the visible label
    text, as WCAG 2.5.3 Label in Name (Level A) requires, and the control can be
    activated by speech input by speaking the title that is displayed. An
    accessible name carrying the `id` alone, such as `Open details for task #<id>`,
    does not satisfy this criterion. The
    property holds without any JavaScript being added: the page loads no script
    beyond those it already loads from `/static/`, and the Content-Security-Policy
    of Acceptance Criterion 33 is unchanged (see
    [Task Detail Modal](#task-detail-modal),
    [Roadmap Tasks Page](#roadmap-tasks-page), and
    [Sprint Detail Sub-Template](#sprint-detail-sub-template)).
94. `GET /roadmaps/{name}/tasks/{id}/data` for a task of an existing roadmap returns
    HTTP 200 and JSON in the shape defined in `DATA_FORMATS.md § Task Detail Data`:
    an object with exactly two members, `task` carrying that task's full field set
    and `comments` carrying that task's comments, ordered oldest first — the same
    order `rmp task comment-list` returns and the same order the modal's timeline
    shows — with every comment present, no type filter and no count limit, and `[]`
    for a task with no comment. The shape composes the `Task` and `Task Comment`
    objects `DATA_FORMATS.md` already defines and introduces no new object shape
    (see [Task Detail Endpoint](#task-detail-endpoint)).
95. The task detail endpoint enforces the same path-parameter discipline as every
    other roadmap route: a request whose `{name}` violates the roadmap-name rules,
    or names a roadmap that does not exist, returns HTTP 404 without touching the
    filesystem outside `~/.roadmaps/`; a non-integer `{id}` returns HTTP 404; and an
    integer `{id}` that is a task of some other roadmap, or of no roadmap, returns
    HTTP 404 rather than that task's data. The endpoint serves `GET` and `HEAD` only
    and answers any other method with HTTP 405. Its response carries
    `Cache-Control: no-store`, like every other data-derived response (Acceptance
    Criterion 37 continues to hold).
96. The served tasks page contains exactly **one** modal element, not one per task.
    The document therefore no longer carries any task's modal content, and its size
    does not grow with the per-task modal content: measured against the recorded
    baseline of 930,188 bytes for 100 tasks — of which 774,484 bytes, 83 percent,
    were the rendered modals — the document for the same 100 tasks is smaller by
    substantially the whole of that modal share, and the remaining size grows only
    with the cards. Opening a card fetches that task's data and fills the single
    modal with every field the modal presented before, plus that task's comments in
    the specified order: nothing the modal displayed is lost (see
    [Task Detail Modal](#task-detail-modal)).
97. Every value the modal script writes into the DOM is written as text, never as
    markup: the script uses `textContent` or an equivalent that cannot interpret
    markup, and uses neither `innerHTML` nor `insertAdjacentHTML`. A task whose
    `title`, `completion_summary`, requirement free-text, or comment `body` contains
    HTML markup renders that markup as visible characters and introduces no element,
    no attribute, and no script into the page. This is proven by a test that fails if
    the script writes such a value as markup, covering at least a hostile task title
    and a hostile comment body (see [Task Detail Modal](#task-detail-modal),
    **Client-side rendering is text-only**, and
    [Security and Constraints](#security-and-constraints), rule 7).
98. The Content-Security-Policy is unchanged by the task detail endpoint: it remains
    exactly the value fixed in Acceptance Criterion 33, whose `connect-src 'self'`
    and `script-src 'self'` already admit a same-origin fetch driven by a script
    served from `/static/`. No inline script is introduced, every script the page
    loads still comes from `/static/`, and the page makes no request to any origin
    but its own (Acceptance Criteria 23 and 33 continue to hold).
99. When the fetch for a task's data fails — a network error, a non-200 response, or
    a body that does not parse — the modal opens and shows a clear error message in
    place of the task's content, stating that the task's detail could not be loaded.
    It does not stay blank, does not close silently, and does not leave the
    previously opened task's data on display. The failure path writes nothing (see
    [Task Detail Modal](#task-detail-modal), **Failure is visible in the modal**).
100. The roadmap tasks page header carries a search input in its actions column and
    **no** knowledge-graph link. The graph stays reachable from this page through
    the admin-shell sidebar's Graph entry, which every page carries (Acceptance
    Criterion 16 continues to hold), so removing the header link removes a duplicate
    route to the graph and no access. The input has a programmatically associated
    accessible label naming what it searches — a `placeholder` does not stand in for
    that label — and is reachable and operable from the keyboard (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Header search control**).
101. Typing a term narrows the board without a page reload, and every column count
    equals the number of cards that column is then showing — the cards visible to
    the user, not the cards present in the document. A task matches when the
    term occurs, case-insensitively and as a substring, in that task's `title` or in
    its `#<id>` reference written with the leading `#`; both `42` and `#42` therefore
    find task 42. No other task field is matched: a term occurring only in a task's
    `functional_requirements`, and matching nothing in that task's title or
    reference, does not match it. Leading and trailing whitespace is stripped from
    the term by the rule Acceptance Criterion 121 fixes, and a term that is empty or entirely
    whitespace under that rule shows every task. The case-insensitive comparison
    folds the term and the task's searchable text by the rule Acceptance
    Criterion 118 fixes, over text each of them normalised by the rule Acceptance
    Criterion 152 fixes, so the same term and task yield the same verdict regardless
    of the browser's reported locale, of the browser, and of the Unicode version
    that browser's case, whitespace, and normalisation tables implement.
102. A column left with no matching card renders its ordinary in-column empty state,
    and the five columns stay present and in order — narrowing the board drops,
    hides, and reorders no column (Acceptance Criterion 81 continues to hold). When
    no task matches, the board states that no task matches the controls the board is
    narrowed by — one message covering the term and the filters together — rather
    than presenting five silently empty columns; that message is distinct from the
    state of a roadmap that holds no task at all, which shows the in-column empty
    states alone (Acceptance Criterion 88 continues to hold).
103. The term travels in the `q` URL query parameter on `/roadmaps/{name}/tasks`. As
    the user types, the page updates the URL in place, replacing the current history
    entry rather than pushing one entry per keystroke. Clearing the search restores
    every card and every unnarrowed count and **removes** `q` from the URL, leaving
    no empty parameter behind.
104. For any roadmap and any term, the board produced by typing that term into the
    search control and the board produced by requesting the page URL carrying that
    term in `q` are identical — the same cards, in the same columns, in the same
    order, with the same column counts and the same empty states — asserted by
    comparing the two. The document served for a cold load with `q` already carries
    the narrowing in its final state — the narrowed column counts, the in-column
    empty states, and the no-match message where applicable — and nothing on the
    client applies the term after load. Non-matching cards **may** be present in
    that document provided they arrive already marked as not visible and count
    towards nothing the board states; their presence is what lets clearing the
    search restore them without a request to the server, as Acceptance Criterion 103
    requires. What is forbidden is a document that arrives unnarrowed and is
    narrowed by a script after load. The identity holds for **every** term, the four
    code points included on which a platform's own normalisation of a term differs
    from the rules this specification fixes. Two of them are the case conversion's:
    a term carrying `U+0130`, and a term carrying `U+03A3` where the full
    conversion's Final_Sigma condition would hold, select the same cards on both
    paths and in every browser (Acceptance Criteria 118 and 119). Two are the
    trimming's, and they differ in opposite directions: a term whose first code
    point is `U+0085` loses it on both paths and finds what the rest of the term
    matches, and a term whose first code point is `U+FEFF` keeps it on both paths
    and finds nothing on an ordinary roadmap. None of the four is a term one path
    narrows by while the other ignores it (Acceptance Criteria 121 and 122). The
    identity extends to canonical spelling: a title written with `U+0130` and a title
    written as `U+0049` followed by `U+0307` carry **one** searchable text under
    Acceptance Criterion 152, so the board a term produces is the same whichever of
    the two spellings the roadmap happens to store, on both paths (Acceptance
    Criterion 153).
105. No `q` value produces an error page: a term matching nothing, a term longer than
    any searchable text, and a `q` the server cannot decode each return HTTP 200,
    the last treated as though `q` were absent. Applying a term adds no database
    query: the page's read remains the full task list specified in Acceptance
    Criterion 89, and narrowing in the browser issues no request at all. A task's
    searchable text is normalised and folded once by the server, never by the client,
    and never trimmed at all, so the two paths cannot disagree about a task's text;
    the term is the only value both of them transform, and both trim it with the
    server's own whitespace set, normalise it from the server's own tables, and fold
    it with the server's own mapping (Acceptance Criteria 119, 122, and 155).
106. A term containing HTML markup renders as visible characters and introduces no
    element, attribute, or script into the page: the server escapes it through
    `html/template` where it echoes it into the search input and into the no-match
    message, and the script writes it only as text, never through `innerHTML` or
    `insertAdjacentHTML`. This is proven by a test that fails if the term is written
    as markup (Acceptance Criterion 97 continues to hold for the modal, and rule 7 of
    [Security and Constraints](#security-and-constraints) governs both).
107. The search introduces no inline script and no Content-Security-Policy change:
    the narrowing script loads from `/static/` like every other client script, and
    the policy remains exactly the value fixed in Acceptance Criterion 33
    (Acceptance Criteria 23 and 98 continue to hold). Every class the control emits
    resolves in the embedded stylesheets and no template carries a `style` attribute
    (Acceptance Criterion 62 continues to hold).
108. The top navbar of every roadmap-scoped page — the roadmap's sprints page, a
    sprint's own page, the tasks board, the audit log page, and the knowledge-graph
    page — shows the name of the roadmap in the request path, rendered prominently
    with the vendored Tabler `h3` type utility and with no glyph or other element
    beside it, and a long name is truncated rather than wrapped or overflowing.
    The roadmap index page, which belongs to no roadmap, renders that region
    empty: no name and no placeholder text. No page's top navbar carries a badge,
    label, or icon declaring the interface read-only. The name is HTML-escaped
    through `html/template` like every other value, the markup uses only classes
    the vendored Tabler distribution ships, and no template carries a `style`
    attribute (Acceptance Criteria 62 and 63 continue to hold; see
    [UI Framework](#ui-framework), rule 19).
109. Every page's header title column is rendered by the shared page-header
    partial: no page hand-writes a `page-pretitle` or a `page-title` element, and
    the titles read exactly `Roadmaps`, `Sprints`, `Tasks`, `Audit`,
    `Knowledge graph`, and — on a sprint's own page — that sprint's `title` with
    its status badge, under the pretitle `Sprint #<ID>`. No page title contains the
    roadmap name, which the shell already states in the sidebar and in the top
    navbar. Each page's actions column carries only what
    [Shared Page-Header Partial](#shared-page-header-partial) fixes: the tasks
    page's search input and its three filter dropdowns, the knowledge-graph page's
    layout dropdown, and the sprint page's link back to the roadmap's sprints page.
    The sprints, audit, and index page headers carry no actions column, and no page
    header links to the knowledge-graph page — Acceptance Criterion 100 held that
    for the tasks page and now holds for every page.
110. `GET /roadmaps/{name}/graph/data` executes the caller's query under a
    5-second deadline derived from the request context. A query that would run for
    longer is cancelled when the budget is exhausted instead of running to
    completion: the request is answered as a query execution failure, and the page
    shows the same "query failed to execute" message it shows for a query that
    fails in the engine — distinct from the "query rejected: not read-only" message
    of Acceptance Criterion 47 and from the invalid-limit message of Acceptance
    Criterion 48. The request is answered HTTP `400 Bad Request` with `kind`
    `execution`, the same status and the same kind a query that fails in the engine
    receives, so no new HTTP status and no new exit code is introduced. This is
    proven with a query whose work the node limit does not bound, such as an
    aggregate over a Cartesian product (`MATCH (a),(b),(c) RETURN count(*)`), which
    returns a single row and is therefore unaffected by the injected `LIMIT`. A
    query that completes within the budget returns exactly the response it returned
    before the budget existed, with nothing truncated and no ordering changed, and
    a client that disconnects still cancels the query immediately. A cancelled
    request writes nothing: the store is unchanged, no checkpoint runs, no
    write-ahead log is truncated, and the server keeps serving later requests (see
    [Graph Query Time Budget](#graph-query-time-budget)).
111. `GET /roadmaps/{name}/graph/data` injects no node `LIMIT` into a statement
    that admits no `LIMIT` clause, and runs it instead of failing it in the parser.
    The criterion is over the **rule**, not over a list of forms, so it MUST assert
    the general case and both of the classes below rather than any single statement.

    **A statement with no top-level `RETURN` is not injected into.** A request whose
    `q` is a **standalone procedure call** executes and succeeds, and is not
    answered with the parse failure that appending a `LIMIT` to it produces. So does
    a request whose `q` is a **write with no projection**: a `CREATE`, a `SET`, a
    `DETACH DELETE`, and a schema DDL statement MUST each be asserted, because the
    write class is the one an enumeration of forms omitted, and each of the first
    three fails in the parser when injected into (Acceptance Criteria 47 and 156
    depend on this half). A `RETURN` appearing only inside a string literal, a
    comment, or a backtick-quoted identifier does not make such a statement
    limitable, and MUST be asserted not to.

    **A schema-introspection command is not injected into either**, although it does
    carry a projection: `SHOW INDEXES`, `SHOW INDEX`, `SHOW CONSTRAINTS`, or
    `SHOW CONSTRAINT`, with or without a `YIELD`, `WHERE`, or `RETURN` tail, written
    with exactly one space between its two keywords. It executes and is answered
    HTTP `200` with `{"nodes": [], "edges": []}`, because its rows carry no node and
    no edge. Recognition of this class is anchored to the start of the statement, so
    a `SHOW` nested inside a larger query does not trigger suppression.

    **The complementary half MUST be asserted too, or the criterion is satisfied by
    an endpoint that injects nothing at all.** A `CALL` projected through a
    top-level `RETURN` (`CALL ... YIELD ... RETURN ...`) admits a `LIMIT`, receives
    the injection, and returns at most the resolved limit's worth of rows; so does a
    write projected through a top-level `RETURN`. Every ordinary reading query is
    unaffected and keeps the behaviour of Acceptance Criterion 48: a query with no
    top-level `LIMIT` still receives the injection, a query with its own top-level
    `LIMIT` still keeps it, and a query whose last line ends in a line comment still
    has the injected clause applied on a new line.

    Asserting that any suppressed statement is refused MUST fail this criterion. A
    suppressed query is not bounded by the node limit; it remains bounded by the
    5-second query time budget (see Acceptance Criterion 110 and
    [Graph Query Time Budget](#graph-query-time-budget)).

112. The roadmap tasks page header carries, beside the search input, exactly three
    filter dropdowns in its actions column: a type filter offering the ten
    `TaskType` values of `MODELS.md § Enums`, a minimum-priority filter offering the
    thresholds `1` to `9`, and a minimum-severity filter offering the thresholds `1`
    to `9`. Each dropdown offers a first option meaning no filter on that dimension,
    and that option is selected whenever the dimension carries no filter. Each
    dropdown carries a programmatically associated accessible label naming the
    dimension it filters — neither that first option nor a `placeholder` stands in
    for the label — and each is reachable and operable from the keyboard
    (Acceptance Criterion 100 continues to hold for the search input). The header
    offers **no** status filter, and no control of any kind narrows, drops, or
    reorders the five columns; [Roadmap Tasks Page](#roadmap-tasks-page), **Why the
    board offers no status filter**, records the four reasons for that omission, so
    the absence is specified rather than merely unimplemented.
113. Each filter narrows the board by its own dimension and every column count
    equals the number of cards that column is then showing, as Acceptance Criterion
    101 requires of the term. A task matches the type filter when its `type` is
    **equal** to the selected value, compared exactly against the spelling in
    `MODELS.md § Enums`; it matches the priority filter when its `priority` is
    **greater than or equal to** the selected threshold, and the severity filter
    when its `severity` is greater than or equal to the selected threshold. These
    are the meanings `rmp task list` gives `-y, --type`, `-p, --priority`, and
    `--severity` (see `COMMANDS.md § List Tasks`), so the same value selects the
    same tasks on the board and on the command line. Each dimension carries at most
    one value.
114. The three filters combine **conjunctively**, with each other and with the
    search term: the board shows exactly the tasks satisfying every active control,
    and a board with no active control shows every task. A request
    for `?q=cache&type=BUG&priority=7` shows the `BUG` tasks of priority `7` or
    above whose `title` or `#<id>` reference contains `cache`, and no other task.
    Activating a further control can only shrink the shown set; no control re-admits
    a task another control excluded.
115. A `type`, `priority`, or `severity` value the dimension does not accept applies
    **no filter on that dimension** and returns HTTP 200 with the board rendered as
    though that parameter were absent — never an error page and never a changed
    status code. This holds for a `type` outside the ten `TaskType` values or
    differing from one only in case, a `priority` or `severity` that is not an
    integer or is an integer outside `1` to `9` (`0` included, a threshold of `0`
    being no filter), a value carrying a sign or surrounding spaces, a parameter
    present with an empty value, and a parameter the server cannot decode. The other
    dimensions are unaffected: with an unusable `type` and an accepted `priority`,
    the board is narrowed by the priority and by the term alone. A repeated
    parameter (`?type=BUG&type=EPIC`) is read as its first occurrence; a
    comma-packed value (`?type=BUG,EPIC`) is one string, matches no `TaskType`
    value, and is therefore ignored whole — no filter is ever partly applied.
116. Each active filter travels in its own URL query parameter on
    `/roadmaps/{name}/tasks` — `type`, `priority`, `severity` — and a dimension on
    its no-filter option leaves **no** parameter behind. Changing a dropdown updates
    the URL in place, replacing the current history entry rather than pushing a new
    one, exactly as Acceptance Criterion 103 requires of typing. For any roadmap and
    any combination of a term and the three filters, the board produced by setting
    those controls on the page and the board produced by requesting the URL carrying
    the same values are identical — the same cards, in the same columns, in the same
    order, with the same column counts and the same empty states — asserted by
    comparing the two, and the document served for such a cold load already carries
    the narrowing in its final state with each control showing the value that
    produced it (Acceptance Criterion 104 continues to hold, including its treatment
    of non-matching cards present but marked as not visible). Clearing every control
    restores the full board with its true unnarrowed counts and leaves the bare page
    URL, carrying none of the four parameters.
117. The filters add no database query: the page's read remains the full task list
    of Acceptance Criterion 89, a filter contributes no clause to it and no read of
    its own, and narrowing in the browser issues no request at all. No filter value
    is echoed into the page — the dropdown options are the server's own enumeration
    of the enum and the range, and an unaccepted value selects the no-filter option
    — so no caller-supplied string reaches the page through `type`, `priority`, or
    `severity`. The filters introduce no inline script and no
    Content-Security-Policy change: they are applied by the same `/static/` script
    that applies the term, and the policy remains exactly the value fixed in
    Acceptance Criterion 33 (Acceptance Criteria 23, 98, and 107 continue to hold).
    Every class the dropdowns emit resolves in the embedded stylesheets, the select
    control is one the vendored Tabler distribution already ships, and no template
    carries a `style` attribute (Acceptance Criterion 62 continues to hold).
118. The task's searchable text and the term are folded by Unicode's **simple
    lowercase mapping**, applied to each code point on its own: unconditional, one
    code point in and one code point out, and consulting no locale. It is **not**
    Unicode's Default Case Conversion, and the two code points where the
    conversions disagree resolve as this criterion states: `U+0130` folds to
    `U+0069` and never to `U+0069 U+0307`, and `U+03A3` folds to `U+03C3` in every
    position, word-final included, and never to `U+03C2`. Nothing is rewritten
    after the mapping: a `U+03C2` in a term stays `U+03C2`, so a term of `οδός`
    finds a task titled `οδός`, which a post-fold rewrite of `ς` to `σ` would stop
    finding. ASCII and accented Latin fold letter for letter — `A` to `a`, `Á` to
    `á` — and a term of `ΟΔΟΣ` finds a task titled `ΟΔΟΣ` on both paths. A term
    whose bytes are not valid UTF-8 is folded with each invalid byte replaced by
    `U+FFFD` and is then matched like any other term, being neither an error nor an
    absent term (Acceptance Criterion 105 continues to hold; see
    [Roadmap Tasks Page](#roadmap-tasks-page), **The folding rule**).
119. The client folds the term with the mapping the server ships to it and calls no
    case conversion of the JavaScript platform: neither `toLowerCase` nor
    `toLocaleLowerCase` appears in the narrowing script, asserted as an absence in
    the script the binary serves, the way Acceptance Criterion 97 asserts the modal
    script's markup sinks. The shipped mapping is compared against the server's own
    folding function over the whole of Unicode — every code point, not a sample —
    and against that function itself, never against a stored copy of its expected
    results; the comparison fails when one code point folds differently on the two
    sides, including when a toolchain upgrade changes a mapping. The server folds a
    task's searchable text and folds a term through that one function, not through
    two implementations of one description. The check is an ordinary Go test: it
    runs no JavaScript and requires no JavaScript engine, no Node.js, no network
    access, and no module beyond the direct dependencies
    `BUILD.md § External Dependencies` names, so that section and
    `BUILD.md § Vendored Web Assets`, rule 2, continue to hold. Because the client
    consults no case table of the browser's, the board a term produces does not
    depend on which Unicode version the browser implements, and two browsers of
    different Unicode versions produce the same board (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **One rule, and only one
    implementation of it**, and **What keeps the shipped rule equal to the
    server's**).
120. Each of the three tabs on the Roadmap Sprints Page carries a Tabler badge whose
    text is the number of sprints in that tab and whose colour is the variant the
    sprint status mapping assigns to the status that tab groups: Próximos carries
    `bg-secondary-lt` (the `PENDING` variant), Actual carries `bg-blue-lt` (`OPEN`),
    and Concluídos carries `bg-green-lt` (`CLOSED`). The three tabs therefore do not
    share one fixed colour. A tab that holds no sprint shows the count `0` and keeps
    the colour of its status, because the colour follows the tab's status and not the
    sprints in it. The check asserts all three tabs together, because only Actual and
    Concluídos can make it fail: `PENDING` maps to `bg-secondary-lt`, which is also
    the neutral colour a badge carries when nothing colours it, so the Próximos badge
    renders identically whether the mapping colours it or not, and a check that
    asserts Próximos alone passes without exercising the rule. The check fails on a
    rendering that gives all three tabs `bg-secondary-lt` (see
    [Roadmap Sprints Page](#roadmap-sprints-page) and
    [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
    rule 2).
121. Before the term is folded, every code point carrying Unicode's **White_Space**
    property is removed from the start of the term and from its end, and no other
    code point is removed from anywhere: a code point carrying that property
    elsewhere in the term survives and is matched literally, and a term made only of
    such code points becomes the empty string and shows every task. Whitespace is
    that property and **not** the set either platform's own trimming function
    removes, and the two code points where those functions disagree resolve as this
    criterion states, in opposite directions: `U+0085` (NEXT LINE) carries the
    property and **IS** removed, although the JavaScript platform's own trimming
    keeps it; `U+FEFF` (ZERO WIDTH NO-BREAK SPACE) does not carry the property and
    is **NOT** removed, although that platform's own trimming removes it — so a term
    pasted with a leading byte-order mark matches nothing on an ordinary roadmap,
    and does so on **both** paths, which is the property this criterion protects
    rather than a defect in it. Swept over every code point of Unicode, those two are the
    whole of the difference: no third code point is removed by one trimming and kept
    by the other. The term is trimmed, **then** normalised, **then** folded, in that
    order, on both paths. The task's searchable text is normalised and folded but
    never trimmed, so a task's own leading or trailing whitespace is part of its text
    (Acceptance Criteria 101, 104, and 152 continue to hold; see
    [Roadmap Tasks Page](#roadmap-tasks-page), **The trim rule**).
122. The client removes the term's leading and trailing whitespace by the whitespace
    set the server ships to it and calls no trimming function of the JavaScript
    platform: no call to `trim`, `trimStart`, `trimEnd`, or the legacy aliases
    `trimLeft` and `trimRight` appears in the narrowing script, asserted as an
    absence in the script the binary serves, the way Acceptance Criterion 119
    asserts the platform's case conversions. The shipped set is covered by the
    **same** check that criterion fixes and not by a second check beside it, and
    with the same three properties: it is compared against the server's own
    whitespace function over the whole of Unicode — every code point, not a sample —
    and against that function itself, never against a stored copy of its expected
    results; and the comparison fails when a single code point is whitespace to one
    side and not to the other, including when a toolchain upgrade changes which code
    points carry the property. The check remains an ordinary Go test: it runs no
    JavaScript and requires no JavaScript engine, no Node.js, no network access, and
    no module beyond the direct dependencies
    `BUILD.md § External Dependencies` names, so that section and
    `BUILD.md § Vendored Web Assets`, rule 2, continue to hold. Because the client
    consults no whitespace table of the browser's, the board a term produces does
    not depend on which Unicode version the browser implements, exactly as
    Acceptance Criterion 119 requires of the fold (Acceptance Criterion 121; see
    [Roadmap Tasks Page](#roadmap-tasks-page), **One rule, and only one
    implementation of it**, and **What keeps the shipped rule equal to the
    server's**).
123. Every query-bar failure of `GET /roadmaps/{name}/graph/data` is answered with
    HTTP `400 Bad Request` and a JSON body of exactly two string fields, `error`
    and `kind`, and never with HTTP 200 and never with the
    `{"nodes": ..., "edges": ...}` shape. `kind` takes exactly two values, the set
    [Query-Bar Error Handling](#query-bar-error-handling), rule 4, enumerates and
    is canonical for: `invalid_limit` for a `limit` outside the six allowed values
    (Acceptance Criterion 48), and `execution` for a statement that failed once
    running, which includes one cancelled for exhausting the 5-second time budget
    (Acceptance Criterion 110). One status serves both and the `kind` is what
    distinguishes them. **A body carrying any other `kind` MUST fail this
    criterion**, and the criterion MUST assert the closed set rather than only the
    two members, because a third value is exactly what an endpoint that started
    refusing statements again would publish. The order is fixed and testable: a
    request carrying both an invalid `limit` and an unexecutable statement is
    answered `invalid_limit`, because the endpoint resolves the limit before the
    statement runs, and the statement is not executed. The boundary against the
    internal read error is drawn at the moment the failure surfaces: a graph store
    that fails to open, or a lock that cannot be taken within the bounded wait, is
    answered HTTP 500, while a failure surfacing once the statement is running is
    answered HTTP 400 with `kind` `execution`, a store corruption a scan discovers
    mid-statement included. The `error` of an execution failure carries the
    engine's diagnostic and the page renders it in place; the `error` of an invalid
    limit names the rejected value (see
    [Query-Bar Error Handling](#query-bar-error-handling) and
    `DATA_FORMATS.md § Graph View Data`, **Error Shape**).
124. Each full-height page region — the Kanban board of the roadmap tasks page and
    the graph card of the knowledge-graph page — satisfies **both** edges of
    [Full-Height Page Regions](#full-height-page-regions), rule 1, when the page is
    rendered in a browser: the region's bottom edge coincides with the bottom edge
    of that page's `main.page-body` element, and that edge lies within the viewport,
    with the document scrolling vertically no further than the viewport height. The
    check measures both regions and asserts both edges, because either edge alone
    passes on a defective layout, and each of the two regions demonstrates one of
    those failures. A region that stops short of the page body's end leaves an
    unused band beneath itself while remaining comfortably within the viewport, so a
    check that asserts only the second edge accepts it. A region that overruns the
    bottom of the viewport still ends exactly where the page body ends, because its
    own overrun stretched the page body to that height, so a check that asserts only
    the first edge accepts it too. A criterion phrased as the region "using the
    available height", or as its height matching a particular length, establishes
    neither edge: every height is the available height of some layout, and a length
    is only ever correct for the page as it stood when the length was chosen.
125. Acceptance Criterion 124 is checked at a set of viewport widths chosen so that
    the roadmap tasks page header renders at more than one height — at a wide
    viewport its search input and three filter dropdowns share a row with the page
    title, and as the viewport narrows they wrap onto further rows (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Header search control** and
    **Header filter controls**) — and both edges hold at every one of those widths.
    Each of those widths is exercised at a viewport tall enough that the floor does
    not bind: below the floor the region takes its minimum whatever the material
    above it measures, so a check made there would record the floor instead of the
    tracking these widths exist to vary, and what holds below the floor is
    Acceptance Criterion 127's to state. This is what a height obtained by
    subtracting a fixed length from the viewport height cannot pass: such a height
    is correct at whichever width its length was chosen for and wrong at every
    other, over-reserving where the page header is short and under-reserving where
    it is tall, so a check made at a single width would accept it and leave the
    defect in place. No full-height region reserves space for a page footer, and no
    page renders a `<footer>` element (Acceptance Criterion 74 continues to hold;
    see [UI Framework](#ui-framework), rule 12, and
    [Full-Height Page Regions](#full-height-page-regions), rules 2 and 3).
126. Each full-height page region's height is declared **twice** in the stylesheet
    the binary serves, in this order: first against the large viewport height
    (`vh`), then against the dynamic viewport height (`dvh`). The check asserts both
    declarations **and** their order, and fails when either is removed or the two
    are swapped. With the dynamic declaration alone, a browser that does not
    implement the unit receives no viewport-derived height at all and the region
    collapses to its content. With the large declaration alone, or with the large
    one placed second, a mobile browser sizes the region as though the address bar
    were retracted, so the end of the region sits below the fold for as long as the
    bar is on screen and the second edge of Acceptance Criterion 124 fails on
    exactly the devices the mobile-first requirement is written for. The order is
    the whole mechanism — the later declaration wins where it is understood and is
    discarded where it is not — so asserting that both units appear, without
    asserting which comes second, does not establish it (see
    [Full-Height Page Regions](#full-height-page-regions), rule 4, and
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
    rule 9).
127. On a viewport short enough that the space the page body leaves falls below a
    full-height region's minimum height, the region takes that minimum rather than
    shrinking to the space available, and the vertical page scrolling that follows
    is permitted. Below the floor **neither** edge of Acceptance Criterion 124 is
    guaranteed. The second edge does not hold on either region: the floored region
    is what the page scrolls vertically to reach. The first edge does not hold
    either wherever the page renders an element above the region inside the same
    container, because the page body is held to the same minimum as the region and
    must then carry that element and the floored region together, so the region's
    foot passes the page body's foot by the space the element occupies. The
    knowledge-graph page always renders such an element: its query bar sits between
    the top of the page body and the graph card (see
    [Graph Query Bar](#graph-query-bar)). The roadmap tasks page renders one
    whenever its controls match no task, the no-match message then standing above
    the board (see [Roadmap Tasks Page](#roadmap-tasks-page), **Empty states**). No
    stylesheet closes that gap: closing it would take a page-body floor of the
    region's floor plus the height of the element above it, which is the fixed
    subtraction [Full-Height Page Regions](#full-height-page-regions), rule 3,
    forbids, and that height is no constant — the query bar stacks its own controls
    as the viewport narrows and the no-match message wraps — so a length chosen for
    one viewport width is wrong at the rest. Above the floor, which is every
    viewport height at which the region is worth presenting at all, both edges hold
    on both pages: this exception is the floor case alone and weakens Acceptance
    Criterion 124 nowhere else. The check exercises both a viewport at which the
    floor binds and one at which it does not, because a check run only below the
    floor passes on a region that never tracks the page body at all, while a check
    run only above it leaves the floor free to be deleted as though it were the
    defect that Acceptance Criterion 124 describes (see
    [Full-Height Page Regions](#full-height-page-regions), rule 5).
128. The Kanban board reserves space beneath its columns for its own horizontal
    scrollbar: the bottom edge of a column sits above the bottom edge of the board
    by at least the reserved amount, so the scrollbar is drawn in that space and
    never over a card, and the last card of a column stays fully visible while the
    board can still be scrolled sideways. The check fails on a board whose columns
    extend to its bottom edge, where the scrollbar overlaps the last card (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Layout and scrolling**).
129. Each of the five columns of the Kanban board is `19rem` wide and never narrower
    than `17rem`, all five carry the same width whatever number of tasks each holds,
    and none stretches to fill a viewport wider than the board needs. The body of a
    task card on that board carries `0.75rem` of padding on all four sides, which is
    strictly less than the padding the vendored Tabler distribution declares for a
    small card's body, and the project override stylesheet declares it in a rule of
    at least the specificity of Tabler's own, in the stylesheet the layout links
    last, so the override wins on the cascade with no `!important`. Both lengths are
    expressed in `rem`. This is a stylesheet change only: the board emits the same
    markup and the same classes, carries no inline `style` attribute, and
    Acceptance Criteria 27, 88, and 124 to 128 continue to hold (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Column width and card density**).
130. `GET /roadmaps/{name}/sprints/{id}` renders the sprint's member tasks as a
    Kanban board of exactly three columns, presented left to right with the headings
    `WAITING`, `DOING`, and `CLOSED`, and the served HTML carries no member-tasks
    table and no task table of any kind on this page. Each column holds exactly the
    sprint's tasks in the statuses assigned to it — `WAITING` the `BACKLOG` and
    `SPRINT` tasks, `DOING` the `DOING` and `TESTING` tasks, and `CLOSED` the
    `COMPLETED` tasks — so every member task appears on the board exactly once and
    none is omitted or duplicated. All three columns are rendered whatever the sprint
    holds: a column with no task keeps its heading and its `0` count badge and shows
    the in-column empty state in place of its card list, and a sprint with no member
    task renders the board with all three columns present, each showing that empty
    state, rather than a page-level empty state or an absent board. The Sprint
    details card above the board and the Comments card below it keep their positions
    (see [Sprint Detail Sub-Template](#sprint-detail-sub-template)).
131. Each column count equals its counterpart in the sprint status summary line
    rendered at the top of the same page: the `WAITING` column's badge equals `P`,
    the `DOING` column's badge equals `A`, the `CLOSED` column's badge equals `C`,
    and the three sum to `T` (Acceptance Criterion 39 continues to hold). The check
    compares the two renderings of one sprint against each other, rather than each
    against a number the check computes on its own, because the property under test
    is that the board and the summary line group the sprint's tasks by the **same**
    categorisation: a board that grouped the statuses differently could still show
    three counts that each looked plausible on its own. For the sprint whose summary
    line reads `33% - P:8 A:29 C:18 - T:55`, the three column badges read `8`, `29`,
    and `18`, and the board shows 55 cards in total.
132. Each column of the sprint's member-tasks board orders its cards by its own
    key. In the `WAITING` column the cards appear in the sprint's planned in-sprint
    execution order, which is the `sprint_tasks` position order the page reads
    (`position` ascending; see
    `DATABASE.md § List Sprint Tasks Ordered by Position`), so for any two tasks of
    that column the card of the task with the lower `position` appears above the
    other. In the `DOING` column the cards appear by `started_at` descending, the
    most recently started task first, and that holds for the column's `TESTING`
    cards as well: a `TESTING` card takes its place from `started_at`, never from
    `tested_at`. In the `CLOSED` column the cards appear by `closed_at` descending,
    the most recently closed task first. Where two cards of one column carry the
    same ordering timestamp, and where a card carries none, those cards are ordered
    by `sprint_tasks` `position` ascending, and a card carrying no ordering
    timestamp appears after every card of that column that carries one. Reordering
    the sprint's tasks through the CLI and reloading the page therefore reorders the
    cards of the `WAITING` column and leaves the order of the `DOING` and `CLOSED`
    columns unchanged; the check MUST assert both halves of that split, because a
    board that ordered all three columns by `position` and a board that ordered all
    three by recency each satisfy one half on its own. The check's data MUST make
    the three candidate orders differ from one another — the `position` order, the
    ordering-timestamp order, and the task `id` order — so that no assertion can
    pass on an order that merely coincides with the specified one. No ordering by
    priority, severity, title, or id is observable anywhere on the board.
133. Each card of the sprint's member-tasks board shows exactly six data points, on
    three lines, in this order: the task `title` leading the card; the reference
    `#<id>` on its own line as secondary text; and one line carrying, at its leading
    edge, a `priority` badge reading `P` immediately followed by the task's priority
    and a `severity` badge reading `S` immediately followed by the task's severity,
    and, at its trailing edge, the number of comments followed by the number of
    subtasks, each as an icon (`ti ti-message` and `ti ti-subtask` respectively)
    followed by its number. The badges and the counters share that one line, and the
    card renders no separate footer row for the counters. The check MUST assert the
    counter order, because a card showing the subtask count before the comment count
    satisfies every other clause of this criterion. On a column too narrow to hold
    the two groups side by side that line wraps inside the card instead of
    overflowing its edge, and `<body>` still produces no page-level horizontal
    overflow (Acceptance Criterion 27 continues to hold). The card carries no inline
    `style` attribute, and every class it emits is defined either in the vendored
    Tabler distribution or in `static/style.css` (Acceptance Criterion 62 continues
    to hold). A task of priority `5` and severity `3` shows `P5` and `S3`; a badge
    carrying the bare integer does not satisfy this criterion, and this card renders
    the pair exactly as the tasks board's card does (Acceptance Criterion 85
    continues to hold). The card of the roadmap tasks page's board is unchanged by
    this criterion: it keeps its separate metadata footer and that footer's own
    indicator order. The priority and severity badges
    take the colours the semantic mapping assigns to their values, which the prefix
    does not affect (Acceptance Criterion 61
    continues to hold). The card carries **no** status badge, because the column
    already states the status, and it shows no task type and no dependency counts.
    The task's full field set is reached through the task detail modal the card
    opens (see [Sprint Detail Sub-Template](#sprint-detail-sub-template)
    and [Task Detail Modal](#task-detail-modal)).
134. Every card of the sprint's member-tasks board renders both of its counters: the
    comment count and the subtask count are present on every card, including when
    either or both are `0`, so a task with no comment and no subtask still shows the
    comment icon followed by `0` and the subtask icon followed by `0`, and both
    numbers sit at the trailing edge of the card's third line, which every card of
    the board renders. The check asserts a card whose two
    counts are both zero, because a card that has something to count renders the same
    markup whether this criterion holds or not. The card of the roadmap tasks page's
    board is not governed by this criterion and keeps its own rule, under which an
    indicator whose value is absent, empty, or zero renders nothing at all
    (Acceptance Criterion 85 continues to hold).
135. Selecting a card of the sprint's member-tasks board opens the read-only task
    detail modal for that task, and the card **is** the trigger: in the served HTML
    the card is a `<button type="button">`, activatable by pointer, touch, Enter, and
    Space, carrying no `tabindex` and no `role="button"`. Its accessible name is
    `Open details for task #<id>: <title>`, containing the task title that is the
    card's visible label; a name carrying the `id` alone does not satisfy this
    criterion. Opening a card fetches that task's data from
    `GET /roadmaps/{name}/tasks/{id}/data` and fills the page's single modal shell,
    of which the page renders one and not one per task. The page loads no script
    beyond those it already loads from `/static/`, and the Content-Security-Policy of
    Acceptance Criterion 33 is unchanged (Acceptance Criteria 93 and 97 to 99
    continue to hold).
136. The sprint's member-tasks board is height-limited and scrolls per column: each
    column scrolls vertically and independently when its cards exceed the board's
    height. That height is **`60vh`** in the project override stylesheet, floored at
    the value of the **`--full-height-region-floor`** custom property, and it is
    **not** the space the page body leaves. The floor is read from that property and
    the length is not restated beside it, so the board and the page body cannot come
    to state different floors; the check fails on a stylesheet that writes the floor
    out as a literal length of its own, and it fails on a sprint page where the
    property does not resolve, because the sprint page carries no full-height shell
    to declare it. On a viewport short enough that `60vh` falls below the floor, the
    board takes the floor. The board is therefore not a full-height page region, and
    Acceptance Criteria 124 to 127 are not asserted against it: adding member tasks
    to the sprint leaves the board's height unchanged and does not push the Comments
    card further down the page. When the three columns do not fit the viewport, the
    column strip scrolls horizontally inside its own container while `<body>`
    produces no horizontal overflow (Acceptance Criterion 27 continues to hold), and
    on a narrow viewport each column keeps a minimum width at which its cards stay
    legible, with touch-friendly hit targets on the cards (see
    [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Height and
    scrolling**, and
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
    rule 10).
137. The comment number on each card of the sprint's member-tasks board comes from
    **one** grouped counting query issued over the whole set of rendered member-task
    ids, never one query per card: an instrumented count of comment-counting queries
    for a sprint page rendering N member tasks is 1, independent of N, and of
    comment-listing queries for member tasks is 0 (see
    `DATABASE.md § Count Comments for Many Parents (Grouped)` and Acceptance
    Criterion 70). A sprint with no member task issues no such query at all. The
    subtask number on a card costs no query of its own, because the sprint's
    member-task read already returns each task's `subtask_count`; grouping the tasks
    into the three columns and counting each column are performed in memory over the
    rows already read, so the board adds no query per column and none per card.
138. The sprint's member-tasks board is read-only: it offers no drag-and-drop and no
    control of any other kind that moves a task between columns, reorders cards,
    changes a task's status, or creates or edits a task, a column, or a comment. The
    served HTML contains no form, no input, and no control in the board that submits
    a change: the only button in the board is the card itself, and activating it
    opens the read-only modal. There is no route and no client-side path through
    which the board can write; the `rmp` CLI remains the sole write path.
139. The three columns of the sprint's member-tasks board divide the width of the
    board equally: all three carry the same width whatever number of tasks each
    holds, and that width grows with the viewport, so widening the viewport widens
    the three columns together and leaves no unused space beside them. No column is
    ever narrower than `17rem`; when three columns at that minimum, plus the
    `0.75rem` gaps between them, do not fit the viewport, the columns keep the
    minimum and the column strip scrolls horizontally inside its own container while
    `<body>` produces no horizontal overflow (Acceptance Criterion 27 continues to
    hold). The check measures the columns at a viewport wide enough for the equal
    division and again at one too narrow for it, because a board measured at one
    width alone passes as readily on columns that never grow as on columns that
    never stop growing. The body of a task card on that board carries `0.75rem` of
    padding on all four sides, which is strictly less than the padding the vendored
    Tabler distribution declares for a small
    card's body, and the project override stylesheet declares it in a rule of at
    least the specificity of Tabler's own, in the stylesheet the layout links last,
    so the override wins on the cascade with no `!important`. Every one of these
    lengths is expressed in `rem`. The five columns of the roadmap tasks page's board
    are unchanged: each stays `19rem` wide with a `17rem` minimum and still does not
    grow into a wider viewport (Acceptance Criterion 129 continues to hold). The two
    boards' column widths are therefore deliberately not one value, and this
    criterion no longer requires them to agree; what the two boards still share is
    the `17rem` minimum, the `0.75rem` gap, and the `0.75rem` card body padding, and
    the check compares those three across the two boards and fails when they diverge.
    This is a stylesheet change only: the board emits the same markup and the same
    classes, carries no inline `style` attribute, and Acceptance Criteria 27, 130,
    and 136
    continue to hold (see
    [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Height and
    scrolling**).
140. Each per-column count badge of the two Kanban boards carries the semantic colour
    of the status its column groups, while its text stays that column's task count
    (Acceptance Criteria 83 and 131 continue to hold). On the roadmap tasks page's
    board a column is exactly one task status, so the badge takes that status's
    variant: `BACKLOG` `bg-secondary-lt`, `SPRINT` `bg-cyan-lt`, `DOING`
    `bg-blue-lt`, `TESTING` `bg-yellow-lt`, and `COMPLETED` `bg-green-lt`. On the
    sprint's member-tasks board a column groups a set of statuses — two for `WAITING`
    and for `DOING`, and `COMPLETED` alone for `CLOSED` — so the badge takes the
    variant of the group's canonical status: `WAITING` carries `SPRINT`'s
    `bg-cyan-lt`, `DOING` carries `DOING`'s `bg-blue-lt`, and `CLOSED` carries
    `COMPLETED`'s `bg-green-lt`. A column holding no task shows the count `0` and
    keeps the colour of its status, because the colour follows the column and not the
    cards in it, and a narrowed board keeps each column's colour while its count
    follows the narrowing (Acceptance Criteria 101 and 113 continue to hold). The
    check asserts all the columns of a board together, exactly as Acceptance
    Criterion 120 asserts the three tabs together: `BACKLOG` maps to
    `bg-secondary-lt`, which is also the neutral colour a badge carries when nothing
    colours it, so that column alone renders identically whether the mapping was
    applied or not, and the check fails on a rendering that gives every column of
    either board `bg-secondary-lt`. Asserting the columns of a board together is
    necessary but not sufficient, and this criterion puts two further requirements on
    the check. The first is that each badge's class is produced by the single
    implementation of this mapping that every status badge already takes its colour
    from, rather than written into the template as a literal or resolved through a
    second mapping standing beside the first: a literal reads exactly as the
    mapping's answer on the day it is written and is then free to drift from it,
    while the mapping stated in rule 2 is the only authoritative one. The check
    establishes that by rendering both boards a second time with that one
    implementation replaced by a substitute whose answer names the status it was
    called with. Under the substitution a class written into the template survives
    unchanged and fails — on the `BACKLOG` column as well, the one column whose
    colour a literal would not change — while a template that calls the
    implementation renders the substitute's answer on every column, and no column
    header of either board still carries a real `bg-*-lt` variant. The second
    requirement is that the check pins each column to the status that column groups,
    which no assertion about the colours alone can make: a check establishing only
    that the columns' colours differ from one another passes unchanged on a board
    whose columns carry each other's statuses, where the colours stay as many and as
    distinct as they were while each sits on the wrong column. The same substitution
    settles that, because a column headed by one status whose badge names another is
    visible in the rendering. The check also asserts the two count badges that
    stay neutral, because the boundary is what keeps this rule a rule rather than a
    licence to colour any count: the Comments card header count on the Roadmap Sprint
    Page carries `bg-secondary-lt`, because it counts comments and a comment has no
    status to key on, and so does any count over a group of mixed status for which no
    canonical status is defined. The mapping introduces no new colour and no new
    band (Acceptance Criterion 61 continues to hold; see
    [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
    rule 2).

141. Every HTTP 500 the running server returns is accompanied by exactly one
    `log/slog` `ERROR` record on stderr, and no 500 is silent. The record names
    the request `method` and `path`, the `status`, and the underlying error text
    under `err` — the value the response body withholds. The response body is
    unchanged: it remains the opaque `internal server error` text, and the error
    detail never reaches the client. Stdout carries only the startup URL object;
    no log record is ever written to it.
142. An HTTP 400 from `GET /roadmaps/{name}/graph/data` — any query-bar failure,
    whatever its `kind` — is accompanied by exactly one `WARN` record carrying the
    failure `kind` and the reason under `err`, matching the `kind` and `error` the
    structured JSON response already carries. The failure still triggers no write,
    no checkpoint, and no navigation.
143. A successful request writes no log record, and neither does an HTTP 404 or an
    HTTP 405: an unknown roadmap, a non-integer or unknown id, an unmapped path,
    and a non-read method on a known path all leave the console silent. The
    exception is the I/O failure of a roadmap's existence check, which is a 500
    and is logged.
144. Every record's `time` attribute is UTC, in the canonical Groadmap format
    `YYYY-MM-DDTHH:mm:ss.sssZ` — exactly three digits of milliseconds and a `Z`
    suffix, for example `2026-08-20T19:53:00.918Z`. It is never the local-zone
    timestamp with a numeric offset that `slog.TextHandler` produces by default,
    and the timestamp is UTC whatever the machine's `TZ` setting is.
145. A record is always exactly one line. A request path, roadmap name, or error
    text containing a newline and a `level=ERROR msg="..."` sequence is emitted
    escaped inside its quoted attribute value, so a crafted request cannot forge a
    second log record on the operator's console.
146. The three startup diagnostics — the non-loopback bind, the unreadable
    roadmap list, and a roadmap skipped by the startup schema migration — are
    `WARN` records on stderr rather than ad-hoc `warning: ` lines. They remain
    non-fatal: `rmp web` still starts, still prints its URL object to stdout, and
    still exits 0 on a graceful shutdown. The non-loopback record still states
    that the interface is reachable from the network and still names the bound
    host.
147. A graph data request takes the graph store's exclusive lock before it opens
    the store, and holds it until the request's statement, its commit, and any
    checkpoint have completed. While an `rmp graph execute` invocation against the
    same roadmap holds that lock, a `GET /roadmaps/{name}/graph/data` for that
    roadmap does **not** fail on the first collision: it waits and is served once
    the invocation releases the lock (see `GRAPH.md § Lock Contention`).
148. The hold spans the statement, and this is observable in both directions: an
    `rmp graph execute` against the same roadmap, issued while a slow graph data
    request is still executing its statement, **waits** for that request rather
    than proceeding beside it, and succeeds once the request completes. Two
    concurrent graph data requests against one roadmap likewise serialise, and both
    are served. An implementation in which the two overlap fails this criterion.
149. A graph data request never blocks indefinitely on the lock. When the lock
    cannot be taken within the bounded wait, the request is answered HTTP 500
    with the opaque error body every other 500 carries, accompanied by exactly one
    `ERROR` log record, and the server keeps serving other requests throughout.
150. A graph data request against a store left with a stale `snapshot.tmp` staging
    directory, or with `snapshot/` absent and `snapshot.bak/` carrying a manifest,
    is served correctly: the response carries the committed graph, and the
    recovery repair those two states require is expected behaviour, not a defect
    (see `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`).
151. A schema-introspection command written with anything but a single space
    between its two keywords is answered as the engine's own parse failure: HTTP
    `400 Bad Request` with `kind` `execution` and the engine's diagnostic in
    `error`. The endpoint neither refuses it before execution nor repairs the
    spacing, and it publishes no class of its own for it. The same statement
    written with one space is answered HTTP `200` with `{"nodes": [], "edges": []}`
    (Acceptance Criterion 156), so the two spellings differ in the response, and
    the difference is the engine's routing rather than a rule of this endpoint's
    (see `GRAPH.md § What Groadmap Does Not Check`, item 7). A body carrying any
    `kind` other than the two of Acceptance Criterion 123 MUST fail this
    criterion.
152. A term and a task's searchable text are normalised to Unicode's **Normalization
    Form C** before they are folded, and the pipeline for a term is trim, then NFC,
    then fold, then NFC, in that order, on both paths. The normalisation is for
    comparison only: the `title` bytes the roadmap stores are unchanged, `rmp task
    get` returns the same bytes it returned before this rule existed, and the card
    renders the stored title, so no stored value and no rendered value is normalised
    (Acceptance Criterion 121 fixes the trim, 118 the fold). The second NFC pass is
    required and is proven so: over the 1,440,384 sequences of a folding code point
    followed by a non-starter, one pass leaves the result outside NFC on **70** of
    them — `H` followed by `U+0331` folds to `h` followed by `U+0331`, which
    composes to `U+1E96`, and `U+1E97`, `U+1E98`, `U+1E99` and `U+01F0` behave the
    same way — while two passes leave it in NFC on **all 1,440,384**, so a third
    pass changes nothing and is not performed. Normalising **before** folding is
    likewise required rather than incidental: the two orders differ on 0 single code
    points but on **74** of those sequences, over **32** distinct leading code
    points, and folding first would give a title spelled `U+0130` and a title
    spelled `U+0049 U+0307` two different searchable texts (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **The normalisation rule**).
153. A task whose `title` is stored decomposed and a task whose `title` is stored
    precomposed are **both** found by a term typed in **either** spelling. All four
    combinations are asserted, not a sample: decomposed title with decomposed term,
    decomposed title with precomposed term, precomposed title with decomposed term,
    and precomposed title with precomposed term each return the task. The property
    holds on the server path and on the client path alike, and the board reached by
    typing the term equals the board reached by requesting the URL carrying it in
    `q` for every one of the four, so Acceptance Criterion 104's identity survives
    normalisation rather than being weakened by it. `U+0130` resolves as Acceptance
    Criteria 104 and 118 already state — a term carrying it selects the same cards
    on both paths and in every browser, and it folds to `U+0069` and never to
    `U+0069 U+0307` — and a title spelled `U+0049 U+0307` now carries the same
    searchable text as one spelled `U+0130`.
154. Normalisation changes nothing else. It does not make one word a substring of
    another: a task titled `Café Lisboa onboarding` is **not** returned by the term
    `cafe`, and one titled `Aérea cargo terminal` is **not** returned by the term
    `ae`, because the form is NFC and an accented letter stays one code point rather
    than a base followed by a mark. Measured over the whole of Unicode, exactly
    **1,117** of the 1,112,064 code points produce a different searchable text under
    this rule than without it, and **none of them is ASCII**, so every ASCII term and
    every ASCII title selects exactly the tasks it selected before. The 1,117 are the
    canonical singletons and the composition exclusions.
155. The client normalises the term from the tables the server ships to it and calls
    the JavaScript platform's own normalisation nowhere: no call to `normalize`
    appears in the narrowing script, asserted as an absence in the script the binary
    serves, the way Acceptance Criterion 119 asserts the platform's case conversions
    and 122 its trimming functions. The shipped data is three generated tables —
    `DECOMP_TABLE` with 2,081 entries, `CCC_TABLE` with 403 spans, and
    `COMPOSE_TABLE` with 961 entries — and the 11,172 Hangul
    syllables appear in none of them, being decomposed and composed arithmetically
    per UAX #15 on both sides. All three are covered by the **same** check that
    Acceptance Criteria 119 and 122 fix and not by a further check beside it, with
    the same three properties: each is compared against the server's own function
    over the whole of Unicode — every code point, not a sample — and against that
    function itself, never against a stored copy of its expected results; and the
    comparison fails when a shipped table holds a different number of entries than
    the server's data, and when a single code point decomposes, orders, or composes
    differently on the two sides, including when a toolchain or dependency upgrade
    changes the Unicode version, so a server whose rule moved is caught rather than
    followed. The three counts above are the counts that comparison enforces. This
    criterion fixes no byte size for the tables, because no gate checks one and the
    sizes move with the generator's layout alone. The server performs the composition step itself, from that same
    `COMPOSE_TABLE`, and does **not** use the composition of
    `golang.org/x/text/unicode/norm`: at the pinned version that module composes a
    supplementary starter as though it were its low 16 bits, turning `U+1003C`
    followed by `U+0338` into `U+226E`, `U+10041` followed by `U+0301` into `U+00C1`,
    and `U+1042B` followed by `U+0308` into `U+04F8`, across 15,342 pairs over 6,232
    leading code points, while the platform's normalisation and Groadmap's leave all
    three unchanged. The table's composition agrees with that module on all 1,112,064
    single code points and still composes the 33 supplementary composites, `U+11935`
    followed by `U+11930` giving `U+11938` among them. The check remains an ordinary
    Go test, on the terms Acceptance Criteria 119 and 122 already state (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **One rule, and only one
    implementation of it**, and **What keeps the shipped rule equal to the
    server's**).
156. **A statement that produces no node and no edge is answered HTTP `200` with
    an empty graph, and this is a success rather than a failure.** Against a store
    that holds at least one index and at least one node, each of the following
    submitted as `q` is answered HTTP `200` with exactly
    `{"nodes": [], "edges": []}`: `MATCH (n:Absent) RETURN n`, which matched
    nothing; `MATCH (n) RETURN count(n)`, which returned a number; `SHOW INDEXES`,
    which returned tabular rows the response shape cannot carry; and
    `CREATE (n:Probe {key:'p'})`, which created a node and returned no columns at
    all. The last two reach this criterion only because neither is injected into:
    the `CREATE` carries no top-level `RETURN` and the `SHOW` is a
    schema-introspection command, so both are suppressed under Suppression 2 of
    [Graph Data Endpoint](#graph-data-endpoint), and an endpoint that appended the
    node `LIMIT` to either would answer `400` instead. The four responses MUST be
    compared against one another and found equal, because the endpoint publishes no
    class that separates them. The criterion also
    requires the control that keeps it narrow: `MATCH (n) OPTIONAL MATCH (n)-[r]->(m)
    RETURN n, r, m` against the same store returns HTTP `200` and a non-empty
    `nodes` array, so an empty answer is a property of the statement and not of the
    endpoint. Answering any of the four with HTTP `400`, or with a `kind` of any
    value, MUST fail this criterion (see
    [Query-Bar Error Handling](#query-bar-error-handling), rule 9).
157. **A schema listing is read from the CLI, and the endpoint says nothing false
    about it.** Against a store that holds at least one index and one constraint —
    the store of `GRAPH.md` Acceptance Criterion 32 — a request whose `q` is
    `SHOW INDEXES` is answered HTTP `200` with `{"nodes": [], "edges": []}`
    (Acceptance Criterion 156), and `rmp graph execute` against that same store
    answers the identical statement with the rows, naming the index the caller
    declared (`GRAPH.md` Acceptance Criterion 34). The criterion MUST assert both
    halves together: the endpoint's answer is empty because its response shape
    carries nodes and edges, not because the store's schema is empty, and the CLI
    read is what establishes the difference. Asserting that the endpoint reports the
    index row MUST fail this criterion.

## See Also

- CLI command contract for `web` → `COMMANDS.md § Web Interface`
- Canonical timestamp format the log records share with every other Groadmap
  date → `DATA_FORMATS.md § Dates - ISO 8601 with UTC`
- The stdout-is-JSON / stderr-is-diagnostics split the log obeys →
  `ARCHITECTURE.md § Error Handling`
- Task detail endpoint JSON shape (the task object and its comments) →
  `DATA_FORMATS.md § Task Detail Data`, composed from `DATA_FORMATS.md § Task` and
  `DATA_FORMATS.md § Task Comment`
- Graph view data JSON shape → `DATA_FORMATS.md § Graph View Data`
- Graph element and property-type JSON mapping reused by the graph data endpoint
  → `DATA_FORMATS.md § Graph Query Result`
- Graph access, recovery, and the checkpoint the endpoint runs after a statement
  that wrote → `GRAPH.md § Engine Construction and Lifecycle` and
  `GRAPH.md § Synchronous Checkpoint on Write`
- The store access lock a web graph request takes, its contention rules, and the
  exhaustive list of what a statement that writes nothing changes on disk →
  `GRAPH.md § Concurrency and Recovery`,
  `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`,
  and `GRAPH.md § Lock Contention`
- The same statement time budget applied by `rmp graph execute`, what a cut
  statement leaves on disk, and the exit code it reports →
  `GRAPH.md § Statement Time Budget`
- The literal-aware masked normalization the endpoint's `LIMIT` decisions run on
  → `GRAPH.md § Literal-Aware Normalization`
- What Groadmap does not check about a Cypher statement, on this endpoint as on
  the CLI → `GRAPH.md § What Groadmap Does Not Check`
- Roadmap discovery, data directory layout, and permissions →
  `ARCHITECTURE.md § Directory Structure`
- SQLite schema migrations the startup step runs, and their idempotency →
  `VERSION.md § Migrations` and
  `DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)`
- Web module responsibilities and command lifecycle →
  `ARCHITECTURE.md § Modules and Responsibilities` and
  `ARCHITECTURE.md § Command Lifecycle`
- Task and Sprint fields presented in the sprints page, the tasks page, the sprint
  page, and the task detail modal → `MODELS.md` and `DATABASE.md`
- `TaskComment` and `SprintComment` fields, the comment type values, the comment
  read queries and their chronological ordering, and the grouped count that gives
  each board card its comment number without reading a body →
  `MODELS.md § Task Comment`,
  `MODELS.md § Sprint Comment`, `MODELS.md § Comment Type`, and
  `DATABASE.md § Comments`
- CLI contract for writing and reading comments → `COMMANDS.md § Task Comments`
  and `COMMANDS.md § Sprint Comments`
- `AuditEntry` fields, the audit read query and its `performed_at DESC` ordering,
  and the audit result-set hard cap presented on the audit log page →
  `MODELS.md § Audit Entry`, `DATABASE.md § audit Table`,
  `DATABASE.md § Audit Queries`, and `DATABASE.md § Audit Result Limit`
- Sprint status enum and lifecycle that classify sprints into the sprints-page tabs
  → `MODELS.md § Enums` and `STATE_MACHINE.md § Sprint State Machine`
- Task status enum and lifecycle that define the tasks-page board's five fixed
  columns, their left-to-right order, and the CHECK constraint that admits no sixth
  status → `MODELS.md § Enums`, `STATE_MACHINE.md § Task State Machine`, and
  `DATABASE.md § tasks Table`
- Default task ordering that fixes the order of the cards inside each column of the
  tasks board → `DATABASE.md § Main SQL Queries` ("List All")
- Lifecycle timestamps that order the `DOING` and `CLOSED` columns of the sprint
  page's member-tasks board, and the planned order that breaks their ties →
  `MODELS.md § Task`, `STATE_MACHINE.md § Date Tracking Fields`, and
  `DATABASE.md § List Sprint Tasks Ordered by Position`
- Task type enum and the `priority` and `severity` integer ranges that fix the
  accepted values of the tasks board's header filters → `MODELS.md § Enums` and
  `MODELS.md § Task`
- CLI filters over the same three dimensions, whose meanings the board's header
  filters reuse — `-y, --type` as an equality, `-p, --priority` and `--severity` as
  thresholds → `COMMANDS.md § List Tasks`
- Keyboard operability of a clickable task: why the modal trigger must be a
  natively activatable element on every surface, and why no script may be added to
  compensate → [Task Detail Modal](#task-detail-modal),
  [Security Headers](#security-headers), and [Frontend Rules](#frontend-rules)
- Sprint membership shown on each board card, the `UNIQUE` constraint that limits a
  task to one sprint, and the grouped query that resolves the sprint of every
  rendered task in one round trip → `MODELS.md § Sprint`,
  `DATABASE.md § sprint_tasks Table (1:N Relationship)`,
  `DATABASE.md § Relationships`, and
  `DATABASE.md § Resolve the Sprint of Many Tasks (Grouped)`
- Task and sprint status enums, the task and sprint lifecycles, and the
  `priority`/`severity` integer ranges and criticality bands that the badge colour
  mapping uses → `MODELS.md § Enums`, `MODELS.md § Task`,
  `STATE_MACHINE.md § Task State Machine`, `STATE_MACHINE.md § Sprint State Machine`,
  and `COMMANDS.md § Show Sprint Status Report`
- Embedded asset bundling, the vendored Tabler framework and D3.js (with
  d3-sankey) assets, and the self-contained-binary build verification →
  `BUILD.md § Vendored Web Assets`
- Help skeleton for `web` → `HELP.md`
