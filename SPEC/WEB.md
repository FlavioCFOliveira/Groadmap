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
  - [Roadmap Task Page](#roadmap-task-page)
  - [Roadmap Audit Log Page](#roadmap-audit-log-page)
  - [Document Title](#document-title)
  - [Shared Page-Header Partial](#shared-page-header-partial)
  - [Shared Sprint-Card Partial](#shared-sprint-card-partial)
  - [Sprint Detail Sub-Template](#sprint-detail-sub-template)
  - [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)
  - [Graph Query Bar](#graph-query-bar)
  - [Query-Bar Error Handling](#query-bar-error-handling)
  - [Graph Labels Sidebar](#graph-labels-sidebar)
  - [Graph Data Endpoint](#graph-data-endpoint)
  - [Static Assets](#static-assets)
- [Read-Only Data Flow](#read-only-data-flow)
  - [Tasks and Sprints from SQLite](#tasks-and-sprints-from-sqlite)
  - [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)
- [Frontend and Embedded Assets](#frontend-and-embedded-assets)
  - [Self-Contained Deliverable](#self-contained-deliverable)
  - [Embedded Asset Categories](#embedded-asset-categories)
  - [Frontend Rules](#frontend-rules)
  - [Markdown Rendering](#markdown-rendering)
  - [Date and Time Display](#date-and-time-display)
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
above: a statement submitted through the query bar is sent to the roadmap's graph
server, through the same client `rmp graph client` uses, and executes there.

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
the sidebar highlights whichever of these views is active, and a task's own page
counts as the Tasks view. Tabler and its assets
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
   inline member-tasks board; the full sprint detail block is
   shown only on the single Roadmap Sprint Page (see
   [Shared Sprint-Card Partial](#shared-sprint-card-partial)). It does not render
   the roadmap's task list.
3. A roadmap tasks page, served at `/roadmaps/{name}/tasks` and read from that
   roadmap's `project.db`. It presents the roadmap's tasks, of any status, as one
   paginated list: a single Tabler card holding one table, with one row per task
   and each row linking to that task's own page. A filter bar in the card's header
   narrows the list by sprint, status, type, and a search on the title and the
   `#<id>` reference, and the card's footer paginates it; the filters, the
   page, and the page size travel in the URL, and the filter state is remembered
   across visits in a cookie the server sets.
4. A roadmap sprint page that shows the details of a single sprint and the
   sprint's member tasks as a Kanban board of three fixed columns — `WAITING`,
   `DOING`, and `CLOSED` — whose cards follow the planned in-sprint execution
   order, read from that roadmap's `project.db`.
5. A roadmap task page, served at `/roadmaps/{name}/tasks/{id}` and read from
   that roadmap's `project.db`. It shows every field of one task, that task's
   comments, and a compact card naming the sprint the task belongs to, or stating
   that the task is in the backlog.
6. A roadmap audit log page, served at `/roadmaps/{name}/audit` and read from
   that roadmap's `project.db`. It presents the roadmap's full audit log — every
   audit entry of any operation and entity type — as a read-only table ordered by
   the audit entry's `performed_at` timestamp descending (most recently performed
   operation first), paginated at a fixed page size of 100 entries per page.
7. A roadmap knowledge-graph page that shows that roadmap's knowledge graph,
   read from its GoGraph store under `~/.roadmaps/<name>/graph/`, as an
   interactive node-link visualisation.

When a user selects a roadmap on the index page, the user lands on that
roadmap's sprints page (`/roadmaps/{name}`), with the **Actual** tab — the
current OPEN sprint or sprints — active by default.

Where a task is shown on these pages — as a card on the sprint page's board, or
as a row of the tasks page's list — it links to that task's own page, which
displays all of the task's fields (see [Roadmap Task Page](#roadmap-task-page)).

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
   does not expand the OPEN sprint into an inline member-tasks board. Próximos
   lists PENDING sprints ordered by ascending sprint `Order`
   (the unique execution order; the next sprint to execute, lowest `Order`,
   first); Actual lists the OPEN sprint or sprints ordered by ascending sprint
   `Order`; Concluídos lists CLOSED sprints ordered by descending sprint
   `Order` (the last/highest-`Order` closed sprint first). Each sprint shown in
   any tab is a clickable link to that sprint's own page. The sprints page does not render the roadmap's task list (see
   [Roadmap Sprints Page](#roadmap-sprints-page),
   [Roadmap Sprint Page](#roadmap-sprint-page), and
   [Shared Sprint-Card Partial](#shared-sprint-card-partial)).
7. The roadmap tasks page shows the tasks of the selected roadmap, of any
   status, as **one list** — a single Tabler card holding one table, not divided by
   status — read from that roadmap's `project.db` and using the fields and
   relationships already defined in `MODELS.md` and `DATABASE.md`. It is served at
   `/roadmaps/{name}/tasks`. Each row shows the task's `#<id>` badge, its title, its
   type, its status, its severity and priority, and its creation date; the title is
   a link to the task's own page, `/roadmaps/{name}/tasks/{id}`, which is where the
   task's full field set is shown. In the card's header, a **filter bar** — a
   `GET` form of native controls — narrows the list by sprint, status, type, and
   a search on the title and `#<id>` reference; status and type are multi-selects
   whose values combine by OR within their dimension, the criteria combine conjunctively across dimensions, and all are applied on the server. The
   list is **paginated on the server**, with the range text, a rows-per-page
   selector, and Tabler pagination in the card footer. Every filter, the page, and the page size travel
   in the URL query string (`q`, `sprint`, `status`, `type`, `page`, and `size`), so a filtered page survives a reload and can be
   shared. A request carrying none of those parameters restores the filter state
   that the last request carrying them stored in a server-set cookie, or applies the defaults
   (every status except `COMPLETED`). The page loads no project script of its
   own; only opening the status and type dropdowns needs the vendored Tabler
   script. The page is read-only: the
   form only narrows what is shown (see [Roadmap Tasks Page](#roadmap-tasks-page)
   and [Roadmap Task Page](#roadmap-task-page)).
8. When a user selects a roadmap on the index page, the user lands on that
   roadmap's sprints page (`/roadmaps/{name}`), with the **Actual** tab — the
   current OPEN sprint or sprints — active by default (see
   [Roadmap Index Page](#roadmap-index-page) and
   [Roadmap Sprints Page](#roadmap-sprints-page)).
9. The roadmap sprint page shows the details of a single sprint, the sprint's
   member tasks as a Kanban board of three fixed columns — `WAITING` holding the
   sprint's `BACKLOG` and `SPRINT` tasks, `DOING` its `DOING` and `TESTING` tasks,
   and `CLOSED` its `COMPLETED` tasks — whose cards are ordered by what each column
   is about, the `WAITING` column by the planned in-sprint execution order and the
   `DOING` and `CLOSED` columns by recency (`started_at` and `closed_at`
   descending), and whose three column counts sum to the sprint's total number of
   member tasks, and the sprint's own
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
11. Every task of a roadmap has its own read-only page, served at
   `/roadmaps/{name}/tasks/{id}` and rendered on the server. It shows all of the
   task's fields, the task's comments as a chronological timeline, and a compact
   card giving the context of the sprint the task belongs to — the sprint, its
   status, its progress, and the task's position in its planned execution order —
   or stating that the task is in the backlog. It returns HTTP `404 Not Found`
   when `{id}` is not a valid integer or is not a task of the named roadmap. Each
   card of the sprint page's board, and the title of each row of the tasks page's
   list, is a link to that page, so the pointer, touch, and the
   keyboard all follow it without any added JavaScript, and it can be opened in a
   new tab. The page only displays data: it contains no form, no edit control, and
   no submit action, and it opens no write path. The interface serves no JSON for
   a task: every value of the page is rendered on the server, escaped by
   `html/template`, with one exception: the HTML the server's Markdown renderer
   produces for each Markdown field (see [Roadmap Task Page](#roadmap-task-page)
   and [Markdown Rendering](#markdown-rendering)).
12. The roadmap knowledge-graph page shows the selected roadmap's knowledge graph
   as an interactive node-link visualisation rendered with **D3.js**, read from
   that roadmap's graph through a running `rmp graph serve`, over the same client
   the CLI uses (see
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
    `rmp graph client` reaches it: the roadmap's socket is resolved, the statement
    is sent to the `rmp graph serve` process listening there, and the result is
    read back over the protocol. The web interface opens no graph store, takes no
    advisory lock, and constructs no engine. A roadmap with no server running is
    reported as a graph that cannot be reached, HTTP `503`, rather than being read
    from disk (see
    [Security and Constraints](#security-and-constraints),
    [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
    `GRAPH.md § Server Resolution`, and
    `GRAPH.md § The Bolt Client`).
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
    roadmap task page, the roadmap audit log page, and the knowledge-graph page —
    and to the interactive components, including the
    sprint tabs, the tasks page's filter bar and task list, the sprint page's
    member-tasks board, and the interactive knowledge-graph
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
    (hamburger) menu. Wherever the sidebar is shown beside the content, the
    horizontal gap between the sidebar and the top navbar, the page header, and the
    page body is the same on every page, whether or not the page scrolls vertically
    (see [UI Framework](#ui-framework), rule 20,
    and [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
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
20. **Markdown fields render as HTML.** The long free-text fields a user authors
    as Markdown — the task `functional_requirements`, `technical_requirements`,
    `acceptance_criteria`, and `completion_summary`, the task comment `body`, the
    sprint comment `body`, and the sprint `description` — are rendered, wherever
    the interface shows them, as the HTML a single server-side Markdown renderer
    produces from them, and not as escaped plain text. The renderer is
    CommonMark-compliant, adds the GitHub Flavored Markdown extensions, footnotes,
    definition lists, and syntax highlighting of fenced code blocks, and never
    emits raw HTML from the source nor an active link to a dangerous URL. Task and
    sprint titles, and the values of the knowledge-graph detail panel, stay plain
    text (see [Markdown Rendering](#markdown-rendering)).

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
4. Reads the machine's hostname once for the document titles (see
   [Document Title](#document-title)), registers the read-only routes (see
   [Routes and Pages](#routes-and-pages)), configures the HTTP server timeouts (see
   [HTTP Server Timeouts](#http-server-timeouts)), and starts serving.
5. Takes `SIGINT` and `SIGTERM` over, and then prints to stdout the URL the
   server is listening on, so the user can open it manually if no browser is
   launched. The startup line is the single machine-readable success object
   described in `COMMANDS.md § Web Interface`. The order inside this step is
   load-bearing, and the paragraph below states why.
6. Unless `--no-open` is given, attempts to open the user's default browser at
   the served URL. A failure to launch a browser is **not** fatal: the server
   keeps running and the URL has already been printed. This launch is the only
   child process the web interface creates, and it is the only one it is
   permitted to create: everything else the interface does it does in this
   process, the graph data endpoint included (see
   [Acceptance Criteria](#acceptance-criteria), criterion 162).
7. Serves requests until the process receives an interrupt signal (`SIGINT`, for
   example `Ctrl+C`) or a termination signal (`SIGTERM`). On either signal the
   server shuts down gracefully: it stops accepting new connections, allows
   in-flight requests a brief bounded period to complete, closes any database
   handle it opened, and exits 0. It holds no graph store to close, because it
   opens none. **This holds from step 5 onwards.** A
   signal that arrives during steps 1 to 4 — the data-directory check, the schema
   migration sweep, the bind, and the route registration — reaches an invocation
   that has printed no URL and served nothing; it is an interruption and the
   process exits `130` with no graceful shutdown (see
   `ARCHITECTURE.md § Exit Codes`).

**The take-over precedes the URL, and therefore precedes the browser launch.**
Until step 5, `SIGINT` and `SIGTERM` carry the meaning they carry for every
short-lived `rmp` invocation. From step 5 they carry the shutdown of step 7. The
URL is what a caller uses to decide the server is up, so ordering the change of
meaning ahead of it is what makes a reachable URL a promise that the process stops
cleanly. It matters more here than the wording suggests: step 6 spawns a browser,
and a process spawn is far slower than anything else between the URL and the
accept loop, so a take-over placed after it would leave that whole span carrying
the wrong meaning.

**The take-over is a change of owner, not a re-registration, and the discipline is
enforced.** One package owns the disposition of these two signals for the whole
binary and never unregisters, so no interval of this process carries a meaning
nobody owns; `internal/testenv` fails the build if any production file outside
that package handles signals, or if either long-lived surface announces itself
before taking the signals over. `rmp graph serve` is on the same discipline (see
`GRAPH.md § Server Startup`), and
`ARCHITECTURE.md § Modules and Responsibilities` is canonical for the package.

The server is long-lived for the duration of the session. It is one of two `rmp`
commands whose process is expected to keep running rather than complete a single
operation and exit; the other is `rmp graph serve`, whose lifecycle is specified
in `GRAPH.md § The Dedicated Graph Server`. Each incoming request opens the data it needs read-only,
serves the response, and releases the handle; the server does not hold a roadmap
database open across requests, and it holds no graph store at any time, because it
opens none.

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

**What must fit inside the `WriteTimeout` is the sum of every bounded term a
graph data request may spend, not any one of them.** There are two: the resolution
probe that decides whether a graph server is serving the roadmap, 2500 ms, and the
backstop deadline the endpoint keeps over the statement it sent, 7.5 seconds.
Ten seconds, inside thirty. There is no third: no graph store lock is taken by
this process, because it opens no store. `GRAPH.md § Server Resolution`, rule 7, is
canonical for the backstop and for why it is the wait budget rather than the query
time budget below; this section does not restate it.

### Graph Query Time Budget

The three timeouts above bound the connection, not the work the server does for a
request. A client that sends its headers promptly, stays connected, and reads the
response as soon as it arrives satisfies all three however long the server takes
to produce that response. One route's work is driven by caller-supplied input:
the graph data endpoint (`GET /roadmaps/{name}/graph/data`) executes a Cypher
query the caller writes (see [Graph Data Endpoint](#graph-data-endpoint) and
[Graph Query Bar](#graph-query-bar)). That route MUST therefore bound its own
work with an explicit time budget.

1. **Budget: 5 seconds, and it governs every surface.** A caller's query MUST run
   under a deadline of 5 seconds. The deadline starts when execution of the query
   begins and covers that execution and the walk over the result it produces (see
   [Graph Data Endpoint](#graph-data-endpoint)).
   **The party that enforces it is the graph server**, which takes this value as
   its maximum statement timeout — clamping a client that asks for longer and
   applying the value unconditionally to a client that asks for nothing — so no
   client can raise its own above it (see `GRAPH.md § Server Options`). This
   section is canonical for the value; `GRAPH.md § Statement Time Budget` is
   canonical for what the budget does to a statement and for what a cut statement
   leaves on disk. Every surface reads one declaration, so the value cannot drift
   between them, and changing it here changes it for `rmp graph client` too.

   **The value is chosen to carry the CLI as well as this endpoint, and it is
   stated as a decision rather than as a margin over a timing.** No figure is
   published for what a statement costs: this project keeps no
   performance-measurement tests, so nothing would re-derive such a figure and
   nothing would catch it going stale
   (`BUILD.md § No Benchmarks and No Performance-Measurement Tests`). What the
   value rests on is the separation it produces, and that separation is a
   classification of query **shapes** rather than a comparison of durations:

   - **Inside the budget**: every realistic read and every realistic write against
     a real knowledge graph, including the largest this product is used against.
     Growth of the graph puts the budget under no pressure. What growth does put
     under pressure is the lock's fixed-part allowance, which
     `GRAPH.md § Lock Contention` is canonical for.
   - **Cut by the budget**: the unbounded shapes, and there are two of them — a
     whole-graph variable-length traversal (`MATCH (a)-[*1..3]->(b) RETURN count(*)`)
     and a multi-way Cartesian product. The second admits no finite budget at all
     on a graph of any size. Nothing between the two classes is close to the
     boundary.
   - **The remedy is narrowing, and it works**: the same traversal restricted to a
     label and a relationship type completes and returns its row, where the
     untargeted one is cut. That one succeeds and the other fails is the
     observable, and it is what a check asserts here.

   The value sits well below the 30-second `WriteTimeout`, so a query the budget
   cuts while it is **reading** is cancelled, and its failure is rendered, while
   the response can still be written. A query the budget cuts while it is
   **writing** is not: the engine's rollback runs past the deadline by a factor
   the statement itself sets, for which no ceiling is established, and it can
   exceed the `WriteTimeout` on its own.
   `GRAPH.md § Statement Time Budget` is canonical for that overrun, and
   `GRAPH.md § Lock Contention` states what it costs the invariant below;
   neither is restated here. The budget is additionally the quantity the graph
   store lock's bounded wait is derived from, because a waiter has to know how
   long a hold may lawfully last and the hold spans the statement (see
   `GRAPH.md § Lock Contention`). What must fit inside the `WriteTimeout` is the
   wait and the statement together rather than either of them alone;
   `GRAPH.md § Lock Contention` is canonical for that invariant, for the wait
   budget this value yields, and for the case in which the invariant does not
   hold. Changing this value changes that wait.

   **The budget binds a statement sent to a graph server too, and the server is
   the end that enforces it.** The server bounds the statement at this same value
   (see `GRAPH.md § Server Options`), and the endpoint keeps a later deadline of
   its own as a backstop against a server that answers nothing at all;
   `GRAPH.md § Server Resolution`, rule 7, is canonical for the two deadlines and
   for why they are not equal. A statement the budget cut is an execution failure
   here, HTTP `400` with `kind` `execution`, which is what rule 4 below requires
   of a budget exhaustion. The request's own context still cancels the statement
   when the client disconnects, exactly as rule 2 requires; that cancellation and
   the server's budget are two independent ends of the same statement.

   The rules below are this endpoint's own handling of the budget. What the
   budget does to a statement, and what a cut one leaves behind, is specified in
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
   and edges, in the same response shape, with nothing truncated and no ordering
   changed. The requirement is on the **response**, which is compared, and not on
   how long the request took, which is not
   (`BUILD.md § No Benchmarks and No Performance-Measurement Tests`). The budget
   is observable only to a query that would otherwise have run for longer than it.
7. **Per request, and a cancelled statement commits nothing it had not already
   committed.** Each graph data request gets its own budget; requests do not share
   one, and one request's budget is unaffected by any other request in flight. A
   statement cancelled before its transaction committed leaves the graph unchanged
   and runs no checkpoint. The budget governs statement execution only, and the
   store the statement runs against was opened by the graph server before the
   request arrived — this endpoint opens none — so cancellation neither causes nor
   undoes the recovery repair that opening performed (see
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)
   and `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`).
8. **The budget is the whole of the bound this interface adds.** This version
   bounds the work of a graph data request and adds nothing else to the web
   interface: no request rate limit and no new endpoint. That the same budget also
   binds `rmp graph client` is a property of the value, not a second bound on
   this endpoint (see rule 1 and `GRAPH.md § Statement Time Budget`).
9. **The budget bounds the work in time and not in memory, and the memory it does
   not bound is not this process's.** A statement inside its budget can still cost
   gigabytes of resident memory. That cost falls on `rmp graph serve`, which is the
   process that executes the statement, and it falls on a long-lived process with
   no exit to return it at; this endpoint sends the statement and reads the answer
   back, so it holds none of it. Nothing in this endpoint's configuration bounds
   that cost, and the endpoint introduces no bound of its own:
   `GRAPH.md § Peak Resident Memory` is canonical for what the memory is, for what
   would bound it, and for why none of those levers is applied. The store on disk
   is untouched either way (see
   `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`).

   **The property that matters is not this endpoint's any more, and saying so is
   the point of this paragraph.** While `rmp web` executed statements in its own
   process, one statement the budget cut while it was writing left that process
   holding gigabytes it did not give back, because an otherwise idle runtime
   triggers no collection, and the request itself received an empty reply when the
   `WriteTimeout` closed the connection with the statement still inside the engine
   call. A request today reaches neither state: this endpoint executes nothing, and
   its backstop fires at 7.5 seconds (`GRAPH.md § Server Resolution`, rule 7). The
   property is recorded here because it has not gone away — a **long-lived**
   process is left holding that memory — and only the process it applies to has
   changed, from this one to the graph server. No figure is published for it, here
   or in `GRAPH.md § Peak Resident Memory`
   (`BUILD.md § No Benchmarks and No Performance-Measurement Tests`).

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
   D3.js visualisation use them; the `data:` source is also what lets a Markdown
   image whose source is a `data:` URL render as an image (see
   [Markdown Rendering](#markdown-rendering), rule 9), and the Markdown feature
   changes nothing in the policy. It forbids inline and remote scripts
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
database or the knowledge graph on every request. The server already reads the
SQLite database fresh on every request, and asks the roadmap's graph server anew
for every graph request, holding no server-side data cache (see
[Read-Only Data Flow](#read-only-data-flow)). This
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
   - the roadmap sprint page (`/roadmaps/{name}/sprints/{id}`);
   - the roadmap task page (`/roadmaps/{name}/tasks/{id}`);
   - the roadmap audit log page (`/roadmaps/{name}/audit`);
   - the knowledge-graph page shell (`/roadmaps/{name}/graph`);
   - the graph data endpoint (`/roadmaps/{name}/graph/data`).

   It also covers the data-state-dependent error responses — for example a
   `404 Not Found` for a roadmap, a sprint, or a task that does not exist, and a
   `500` from
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
5. **The tasks page varies by cookie, and no response of it is reused.** A
   response of the roadmap tasks page (`/roadmaps/{name}/tasks`) depends on the
   request's `Cookie` header as well as on its URL, and the HTTP 200 response to an
   explicit request carries a `Set-Cookie` header (see
   [Roadmap Tasks Page](#roadmap-tasks-page), **Filter persistence**). Its
   responses therefore carry, besides `Cache-Control: no-store`
   (rule 1), the header `Vary: Cookie` (RFC 9110, Section 12.5.5): every response
   of the route's `GET` and `HEAD` handling carries it — to an explicit request, to
   a bare request, and a `404` alike — whether or not it sets the cookie. A `405`
   answered by the method fallback to any other method carries no `Vary`, because
   it does not depend on the cookie. The route emits no `ETag` and no
   `Last-Modified` header, evaluates no conditional request header
   (`If-None-Match`, `If-Modified-Since`), and never answers `304 Not Modified`:
   every request is answered with the full response for its own URL and its own
   cookie. `no-store` forbids every cache to store the response (RFC 9111,
   Section 5.2.2.5), so no stored response — one carrying `Set-Cookie`, or one
   produced for another cookie — can be served for a later request; `Vary: Cookie`
   additionally declares the dependency, so that no cache keys a response of the
   route on its URL alone. A `HEAD`
   response carries the headers the `GET` response to the same request carries,
   `Vary`, `Cache-Control`, and `Set-Cookie` included.

## Routes and Pages

All routes serve `GET` and `HEAD` only. Every page is server-rendered HTML
produced from embedded `html/template` templates. Page routes return HTML
(`text/html; charset=utf-8`); the graph data endpoint returns JSON.

| Route | Method | Purpose | Response |
|-------|--------|---------|----------|
| `/` | GET, HEAD | Roadmap index | HTML list of roadmaps |
| `/roadmaps/{name}` | GET, HEAD | Roadmap sprints page (landing; sprint tabs) | HTML |
| `/roadmaps/{name}/tasks` | GET, HEAD | Roadmap tasks page (one paginated task list; optional `q` and `sprint` filter parameters, optional repeatable `status` and `type` filter parameters, and optional `page` and `size` pagination parameters; a request carrying none of the six takes its filter state from the `rmp_tasks_filters` cookie or the defaults, and a request carrying any of them sets that cookie; see [Roadmap Tasks Page](#roadmap-tasks-page)) | HTML |
| `/roadmaps/{name}/tasks/{id}` | GET, HEAD | Roadmap task page (one task's fields, its comments, and its sprint context; see [Roadmap Task Page](#roadmap-task-page)) | HTML |
| `/roadmaps/{name}/sprints/{id}` | GET, HEAD | Roadmap sprint page (the sprint's details and its member-tasks board) | HTML |
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
4. `{id}`, on the task route `/roadmaps/{name}/tasks/{id}`, is a task identifier
   and follows the same discipline. It MUST be a valid integer; a non-integer
   `{id}`, or an integer `{id}` that is not the `id` of a task belonging to the
   named roadmap, is answered with HTTP `404 Not Found`. The `{name}` part is
   validated by rules 1 and 2 above before any filesystem path is built, exactly as
   on every other roadmap route, so the task page carries the same path-traversal
   guard as the other pages (see [Roadmap Task Page](#roadmap-task-page) and
   [Security and Constraints](#security-and-constraints)).
5. No route lies below `/roadmaps/{name}/tasks/{id}`. A request for a longer path
   under it — `/roadmaps/{name}/tasks/{id}/data` among them — matches no route in
   the table above and is answered with HTTP `404 Not Found`, as every path no
   route matches is. The interface serves no JSON for a task.

HTTP status mapping for page and data routes:

| Condition | HTTP status |
|-----------|-------------|
| Page or data served successfully | 200 |
| Roadmap name invalid, or roadmap not found | 404 |
| Sprint `{id}` not a valid integer, or not a sprint of the roadmap | 404 |
| Task `{id}` not a valid integer, or not a task of the roadmap | 404 |
| Audit `page` parameter out of range, non-integer, or garbage | 200 (clamped to nearest valid page; see [Roadmap Audit Log Page](#roadmap-audit-log-page)) |
| Tasks `q` search parameter absent, empty, unmatched, or undecodable | 200 (never an error; see [Roadmap Tasks Page](#roadmap-tasks-page)) |
| Tasks `sprint` filter parameter, or any occurrence of the repeatable `status` or `type` filter parameter, absent, unknown, malformed, or undecodable; a repeated or duplicated occurrence; or any parameter the page does not accept, `priority` and `severity` included | 200 (never an error; each unaccepted parameter or occurrence is ignored as though absent, and a duplicated value counts once; see [Roadmap Tasks Page](#roadmap-tasks-page)) |
| Tasks request carrying none of the six parameters, with a `rmp_tasks_filters` cookie absent, or holding unaccepted, undecodable, or foreign-roadmap parts | 200 (never an error; each unaccepted part of the cookie is ignored, and an absent cookie gives the defaults; see [Roadmap Tasks Page](#roadmap-tasks-page), **Filter persistence**) |
| Tasks `page` or `size` pagination parameter absent, non-integer, not an allowed value, or undecodable, or `page` beyond the last page | 200 (never an error; `page` falls back to 1 and `size` to 25, and a `page` beyond the last page renders the last page; see [Roadmap Tasks Page](#roadmap-tasks-page)) |
| Graph data `limit` not one of the six allowed values | 400 (`kind` `invalid_limit`; the query is not executed; see [Query-Bar Error Handling](#query-bar-error-handling)) |
| Graph data `q` carries an `EXPLAIN` or `PROFILE` prefix the engine's parser recognises, and `limit` is allowed | 400 (`kind` `plan_prefix`; the statement is not sent, and the answer is the same with no graph server listening; see [Query-Bar Error Handling](#query-bar-error-handling)) |
| Graph data query fails once running, a query cancelled for exhausting the time budget included | 400 (`kind` `execution`; see [Query-Bar Error Handling](#query-bar-error-handling)) |
| Graph data request for a roadmap with no graph server listening, or one whose server cannot be reached through a socket that answered | 503 (the graph is unavailable until a server is started; the response carries no `kind`; see [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)) |
| Graph data request for a roadmap whose derived socket path is longer than the platform's bound | 500 (no server can **ever** exist there, so the condition is permanent rather than transitory; the response carries no `kind`; see [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)) |
| Non-read HTTP method on any route | 405 |
| Unhandled internal error reading data (I/O, corrupt roadmap database) | 500 |

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
  the roadmap's full set of tasks has its own page, the task list at
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
    tab does not expand the OPEN sprint into an inline member-tasks board; the
    full sprint detail block is shown only on the single
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
    member tasks and links to no task page; a task links to its own page from its
    card on the single Roadmap Sprint Page and from its row on the tasks page's
    list (see [Roadmap Task Page](#roadmap-task-page)).

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
- **Sprint description rendered as Markdown.** Wherever a sprint's `description`
  is shown in a sprint card on this page — across all three tabs — it renders as
  the HTML the Markdown renderer produces from it, in the renderer's
  non-interactive form, because the whole card is a single link (see
  [Shared Sprint-Card Partial](#shared-sprint-card-partial), rule 5, and
  [Markdown Rendering](#markdown-rendering)). The rendered content wraps within
  the card, so no forced horizontal scrolling of the page is introduced.
- **Relationships shown.** The page surfaces, in a read-only view, the
  relationships already modelled in the data: task-to-sprint membership (including
  task order within a sprint). The presentation MUST reflect the same
  relationships defined in `DATABASE.md § Relationships`; it introduces no new
  relationship.
- **Read-only.** The page renders data only. It contains no form, button, or
  link that submits a change; there is no edit affordance of any kind. The sprint
  links navigate to read-only views and submit no change.

### Roadmap Tasks Page

- **Route:** `GET /roadmaps/{name}/tasks`
- **Content:** A read-only presentation of the named roadmap's tasks, of any
  status, read from that roadmap's `project.db` and laid out as **one list**: a
  single Tabler card holding one table, with one row per task. The list is not
  divided by status or by any other attribute; a task's status is a column of its
  row. The card's header carries the **filter bar** that narrows the list, and the
  card's footer **paginates** it. Every field a task has remains reachable from this
  page through the task's own page, to which each row links (see
  [Roadmap Task Page](#roadmap-task-page)). The page renders no board, no column
  per status, and no card per task.
- **Modelled on three Tabler examples.** The page follows the official Tabler
  examples, each for one part of the list card (see [UI Framework](#ui-framework),
  rule 8):
  - **The card as a whole** follows Tabler's cards example,
    `https://preview.tabler.io/cards.html`: one `card` made of a `card-header`, the
    table in place of a `card-body`, and a `card-footer`, the footer laid out as a
    `row align-items-center` of `col-auto` columns with the last one pushed to the
    trailing edge by `ms-auto`.
  - **The card header** follows Tabler's card-actions example,
    `https://preview.tabler.io/card-actions.html`: a leading block,
    `<div><h2 class="card-title">…</h2></div>`, holding the card title, and a
    trailing `<div class="card-actions">` holding the controls, which Tabler aligns
    to the header's trailing edge.
  - **The table and its rows** follow Tabler's task-list example,
    `https://preview.tabler.io/tasks-list.html`: a `table-responsive` container
    holding a `table table-vcenter card-table`, whose rows carry the task title,
    badges, a muted date preceded by a calendar icon, and a right-aligned
    `btn btn-sm` reading `View`.

  The page departs from the task-list example deliberately, and only in these ways:
  1. **One card, not one card per group.** The example splits its tasks into
     several cards by group; this page renders exactly one list card for all the
     tasks the filters admit.
  2. **No selection.** The table carries no selection checkbox column and no
     `table-selectable` class, because nothing on the page acts on a set of tasks.
  3. **No add-task control and no modal.** The card header carries no add-task
     button, and the page renders no modal: the page is read-only (see
     **Read-only** below), and a task is shown on its own page, never in a modal.
  4. **The columns are the roadmap's.** The example's assignee column, with its
     avatar, has no counterpart, because a task has no assignee; the columns are
     the ones **Row content** below fixes.
  6. **No per-row button.** The example ends each row with a right-aligned
     `btn btn-sm` reading `View`; this table has no such button and no actions
     column, because the task title is already a link to the task's page (see
     **Links to the task page** below).
  5. **A filter bar, pagination, and a rows-per-page selector are added**, the
     first in the card header's actions container and the other two in the card
     footer. The example has none of the three; this page needs them because a
     roadmap can hold more tasks than one screen presents (see **Filter bar** and
     **Pagination** below).
- **The card header.** The list card's header is
  `<div class="card-header flex-wrap gap-2">` and holds, in this order:
  1. the **title block**, `<div><h2 class="card-title">Task list</h2></div>`. The
     block carries **no** `card-subtitle`: the number of tasks the filters admit is
     already stated by the footer's range text (see **Pagination** below), and a
     second statement of it in the header would be a copy that can disagree with
     the first;
  2. the **actions container**, `<div class="card-actions">`, holding the filter
     bar's form and nothing else.

  Tabler's `card-header` lays its two blocks out on one line, the title block at
  the leading edge and the actions container at the trailing edge. The vendored
  `card-header` does not wrap, so the header carries the Tabler utilities
  `flex-wrap` and `gap-2`: where the viewport is too narrow for the title block and
  the form on one line, the actions container moves to a line of its own below the
  title block, still aligned to the trailing edge, and the form's controls wrap
  within it, rather than the header overflowing the card (see
  [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
  rule 9).
- **Filter bar.** The filter bar is a **`GET` form** of native Tabler form
  controls and Tabler dropdowns, placed inside the list card header's actions container (see **The card
  header** above), whose `action` is the page's own path,
  `/roadmaps/{name}/tasks`. It is the page's only form. The page header's actions
  column carries nothing on this page (see
  [Shared Page-Header Partial](#shared-page-header-partial), rule 5). The form
  carries, in this order:
  1. a **search input** —
     `<input type="search" class="form-control form-control-sm" name="q" placeholder="Search">`
     — labelled `Search`, matching the task title and the `#<id>` reference (see
     **The text search** below);
  2. a **sprint select** — `<select class="form-select form-select-sm" name="sprint">`
     — labelled `Sprint`, whose width is capped (see **Compact controls** below),
     offering `Any sprint` (value empty), `No sprint` (value `none`), and
     one option per sprint of the roadmap, whose value is the sprint's `id` and
     whose text is `Sprint #<id>` followed by a space and the sprint's `title`. The
     sprint options follow ascending sprint `Order`, the planned execution order
     `rmp sprint list` returns (see `COMMANDS.md § List Sprints`). Both the `id` and
     the `title` are shown because the `title` alone does not identify a sprint:
     `MODELS.md § Sprint` places no uniqueness constraint on it;
  3. a **status multi-select** — a Tabler dropdown labelled `Status` (see
     **Multi-select dropdowns** below) holding one checkbox per `TaskStatus` value
     (`MODELS.md § Enums`), in the order of the task state machine's flow
     (`STATE_MACHINE.md § Task State Machine`): `BACKLOG`, `SPRINT`, `DOING`,
     `TESTING`, `COMPLETED`;
  4. a **type multi-select** — a Tabler dropdown labelled `Type` (see
     **Multi-select dropdowns** below) holding one checkbox per `TaskType` value,
     in the order `MODELS.md § Enums` lists the ten values;
  5. an **Apply** control,
     `<button type="submit" class="btn btn-primary btn-sm">Apply</button>`.

  The bar carries these five controls and no other: it offers no priority filter,
  no severity filter, and no Reset control, so that the whole bar fits on the card
  title's line (see **Compact controls** below). A task's priority and severity
  remain shown in its row (see **Row content** below); they only do not narrow the
  list. Removing every filter is done by choosing `Any sprint`, unchecking every
  status and type checkbox, and emptying the search input, then applying.
  Returning to the default filter state is done by following the Reset link of
  the no-match empty state (see **Empty states** and **Filter persistence**
  below).

  **Multi-select dropdowns.** The status and the type controls are each one Tabler
  dropdown, `<div class="dropdown">`, made of two parts:
  - a **toggle button** styled as a small select,
    `<button type="button" class="form-select form-select-sm" data-bs-toggle="dropdown" data-bs-auto-close="outside" aria-describedby="<ID>">`,
    whose text states the dimension's active values (see below) and is held in a
    `<span id="<ID>">` inside the button, `<ID>` being unique in the page; and
  - a **menu**, `<div class="dropdown-menu">`, holding one checkbox per enum
    value in the order fixed above, each checkbox being
    `<input type="checkbox" class="form-check-input" name="status" value="<VALUE>">`
    (`name="type"` in the type dropdown) inside a `<label class="dropdown-item">`
    whose text is the value exactly as the enum spells it. `<VALUE>` is that same
    spelling.

  The menu carries **no** *any* checkbox: a dimension with no box checked is a
  dimension with no filter, and it admits every value. `data-bs-auto-close="outside"`
  keeps the menu open while boxes are checked and unchecked in it, and closes it
  on a click outside it. Checking or unchecking a box does not submit the form;
  the form is submitted only by activating Apply or by pressing Enter in the search
  input (see **Scripts** below). A box is
  `checked` in the served HTML exactly when its value is one of the dimension's
  active values (see **Each control shows the filter state that produced the
  list** below).

  The toggle's text is computed by the server from the dimension's active values
  and never changes after the page loads:
  - `Any status` (`Any type` for the type dropdown) when the dimension has no
    active value;
  - the value itself, exactly as the enum spells it, when it has exactly one;
  - `<n> selected`, where `<n>` is the number of active values written in ASCII
    decimal digits, when it has two or more — for example `4 selected`.

  Checking or unchecking a box before applying does not change the toggle's text:
  the text states the filter state that produced the list, not the state of the
  boxes the reader has not applied yet.

  **The toggle's text is its accessible description.** The toggle's
  `aria-describedby` names the `<span>` that holds its text, so the text —
  `Any status`, `Any type`, the one active value, or `<n> selected` — is the
  toggle's accessible description, while its visually hidden `<label>` stays its
  accessible name (see **Labels are programmatic, not visible** below). A screen
  reader therefore announces the control as its name, its role, and its applied
  state, for example "Status, button, 4 selected".

  The form **always** carries the active page size in a hidden input named `size`,
  the default `25` included, so applying the filters keeps the page size, and it
  carries no `page` field, so applying the filters always returns to page 1. The
  rule that `size` is carried only when it is not `25` governs the links the page
  generates, not the form (see **Pagination** below).

  **Compact controls.** The bar sits in the card header beside the card title, so
  its controls are Tabler's small variants: every input carries `form-control-sm`,
  the sprint select and both dropdown toggles `form-select-sm`, and Apply `btn-sm`. The sprint
  select's width is capped at `16rem` by a `max-width: 16rem` declared for a class
  of the project override stylesheet `static/style.css`, inside a media query that
  applies from Tabler's `sm` breakpoint (`576px`) up, so that a long sprint `title`
  does not widen the bar; below that breakpoint the cap does not apply, and the
  sprint select fills its row like every other control. The option text is not
  changed, and the full text of every option remains in the served HTML. The small
  sprint select, the two small dropdown toggles, the small search input, and the
  Apply button have **one rendered
  height**: where the vendored `form-select-sm` renders taller than
  `form-control-sm` and `btn-sm`, the project override stylesheet aligns it, so the
  controls of a line of the bar share one height.

  **Labels are programmatic, not visible.** The search input, the sprint select,
  and each of the two dropdown toggle buttons carries a real
  `<label>` associated with it by `for` and `id` — for a dropdown, the `id` is the
  toggle button's — naming what the control filters,
  and every such label carries Tabler's `visually-hidden` class: it is not
  displayed, and it remains the control's accessible name. What each control is
  stays visible through its own text: the search input's `placeholder` reads
  `Search`, the label's own text; the sprint select shows its first option, `Any
  sprint`, or the sprint selected, whose text names the dimension
  (`Sprint #<id> …`); and each dropdown toggle shows `Any status` or `Any type`, a
  value of the enum the label names, or `<n> selected` (see **Multi-select
  dropdowns** above), which is also the toggle's accessible description through
  `aria-describedby`. Each checkbox of a dropdown's menu is named by its own
  `<label class="dropdown-item">`, whose text is its value. The first option of the
  sprint select and the *any* text of a toggle are a **value**
  meaning *no filter on this dimension*, not the control's name, and neither they nor
  the `placeholder` replace the label: the label is present in every case. The
  search input's accessible name, `Search`, contains its visible placeholder text,
  as WCAG 2.5.3 Label in Name (Level A) requires. Because the placeholder is the
  search input's only visible label, its text has a contrast ratio of at least
  4.5:1 against the input's background in the dark theme, as WCAG 2.2 Success
  Criterion 1.4.3, Contrast (Minimum), requires of text; where the vendored
  placeholder colour falls short, `static/style.css` sets it (see
  [UI Framework](#ui-framework), rule 10). Every control is reachable and
  operable from the keyboard.

  **Each control shows the filter state that produced the list.** The **active
  filter state** of a response is the `q`, `sprint`, `status`, `type`, and `size`
  the list was produced from, whichever source supplied them — the URL, the
  filter-state cookie, or the defaults (see **Filter persistence** below). On every
  response the
  search input's `value` is the active `q`, with each byte that is not
  part of a valid UTF-8 sequence replaced by `U+FFFD` (REPLACEMENT CHARACTER); the
  sprint select marks as
  `selected` the option equal to the active `sprint`, or its first
  option — the *any* option — when there is none; and each dropdown marks as
  `checked` exactly the boxes of the dimension's active values, and no box when the
  dimension has none (see **Query parameters** below). The form is laid out on Tabler's grid, as
  `<form class="row g-2 align-items-end justify-content-end">` with each control and
  its label in a `col-12 col-sm-auto` column, so the controls are **trailing-aligned**:
  at a viewport width of `1440px` the whole bar sits on the card title's line,
  aligned to the header's trailing edge, as the controls of Tabler's card-actions
  example do. The **trailing edge** is the position Tabler's own `.card-actions`
  gives its content: the vendored rule's negative inline-end and block margins are
  kept on the list card's `card-actions`, and neither the project override
  stylesheet nor a template cancels them there. This constrains the list card's
  `card-actions` alone; the sprint board's rule for the `card-actions` of a
  collapsed column (see [Sprint Detail Sub-Template](#sprint-detail-sub-template))
  is outside it. On a narrower viewport the controls wrap onto further lines, each line
  still aligned to the trailing edge; on a phone-sized viewport each control takes a
  line of its own; and the bar never forces page-level horizontal overflow (see
  [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
  rule 9).

  **Scripts.** The page loads no project script of its own. The one script it loads
  is the vendored Tabler script, `/static/vendor/tabler/tabler.min.js`, as the
  other pages do, and opening the Status and Type dropdowns needs it: Tabler's
  dropdown behaviour is what shows and hides a dropdown's menu. Nothing else on the
  page needs a script. The filter bar is an ordinary HTML form: activating Apply,
  or pressing Enter in the search input, submits it by `GET`, and the server
  renders the filtered list; checking or unchecking a box, or choosing a sprint,
  does not submit the form by itself. The list, the range text, the pagination
  bar, the rows-per-page selector, and the state of every control are rendered by
  the server, and no script changes any of them after load. With scripting
  disabled the dropdown menus do not open, so the status and type boxes cannot be
  changed; the list still renders, and the search, the sprint select, Apply, the
  pagination bar, and the rows-per-page selector still work, and a submission
  still carries the boxes the server marked `checked`, because a checked box is
  submitted whether or not its menu is shown.
- **Query parameters.** The filters, the page, and the page size travel in the URL
  query string of `/roadmaps/{name}/tasks`, so a filtered page survives a reload,
  can be bookmarked and shared, and is restored by the browser's Back navigation.
  The parameters are exactly these six:

  | Parameter | Accepted value | Effect |
  |---|---|---|
  | `q` | any string | The search term (see **The text search** below) |
  | `sprint` | `none`, or the `id` of a sprint of the roadmap written as a canonical decimal integer | `none`: only tasks that belong to no sprint; an `id`: only the tasks of that sprint |
  | `status` | repeatable; each occurrence one of the five `TaskStatus` values, exactly as the enum spells it | Only tasks whose `status` equals one of the accepted values |
  | `type` | repeatable; each occurrence one of the ten `TaskType` values, exactly as the enum spells it | Only tasks whose `type` equals one of the accepted values |
  | `page` | a canonical decimal integer of at least `1` | The 1-based page of the list to render (see **Pagination** below) |
  | `size` | `10`, `25`, `50`, or `100` | The number of rows per page (see **Pagination** below) |

  `status` and `type` are **repeatable**: a request names several values of one
  dimension by repeating the parameter, `?status=DOING&status=TESTING`. The
  **accepted values** of a repeatable parameter are the distinct values among its
  accepted occurrences; a value that occurs more than once counts once. A
  repeatable parameter with no accepted occurrence has no accepted value, and its
  dimension is not filtered.

  A **canonical decimal integer** is one or more ASCII digits (`0` to `9`), the
  first of which is not `0`, with no sign, no whitespace, and no other character.
  The enum values are matched exactly and case-sensitively: `bug` is not `BUG`.
  Parameters not in this table are ignored. In particular, `priority` and
  `severity` are **not** parameters of this page: a request carrying either, with
  any value, lists exactly what it lists without it, and no generated link carries
  either.

  **Validation: an unacceptable value is ignored, never an error.** Every
  parameter is validated before it is used, and a value that is not accepted by the
  table above is handled as follows:
  1. **A filter parameter** — `sprint`, `status`, or `type` — whose value is not
     accepted is **ignored**: the list is rendered exactly as though that parameter
     were absent, and its control shows its *any* state — the *any* option of the
     sprint select, no box checked in a dropdown. For the repeatable `status` and
     `type`, the rule applies to **each occurrence on its own**: an occurrence
     whose value is not accepted is ignored, and the parameter's other occurrences
     are unaffected, so `?status=DOING&status=doing` filters by `DOING` alone, and
     only a parameter none of whose occurrences is accepted leaves its dimension
     unfiltered. This covers a value that
     differs from an accepted one only in case, a value carrying a sign,
     surrounding spaces, a leading zero, or any other decoration, a `sprint` that is neither `none` nor the `id` of a sprint of **this** roadmap —
     the `id` of a sprint of another roadmap, or of no sprint, included — and a
     parameter or occurrence present with an empty value, which is also the value
     the sprint select's *any* option submits.
  2. **`page`** whose value is not a canonical decimal integer — absent, empty,
     `0`, negative, fractional, decorated, or not a number — **falls back to `1`**.
     A canonical `page` greater than the last page, however large, renders **the
     last page** (see **Pagination** below).
  3. **`size`** whose value is not one of the four accepted values **falls back to
     `25`**, the default, and the rows-per-page selector marks `25` as active.
  4. **`q`** accepts every string, so no `q` is ignored for its content (see
     **The text search** below).
  5. **A parameter the server cannot decode** — one whose percent-encoding is
     malformed — is treated as absent, and the parameters that decode are applied
     unaffected. For `status` and `type`, an occurrence that cannot be decoded is
     ignored on its own, as rule 1 ignores an unaccepted one.
  6. **One value is read per non-repeatable parameter.** A URL that repeats `q`,
     `sprint`, `page`, or `size` — `?sprint=4&sprint=7` — is read as its **first**
     occurrence, and the remaining occurrences are ignored. Every occurrence of
     `status` and of `type` is read (see above). A single value that packs
     several — `?type=BUG,EPIC` — is one string that is not an accepted value, and
     rule 1 ignores that occurrence.
  7. **The parameters are independent.** An ignored parameter narrows nothing and
     leaves every accepted parameter applied, and the list depends on which values
     are present, never on the order in which the query string carries them.

  No parameter value produces an error page and none changes the route's status
  codes: the page answers HTTP 200 whatever its six parameters, or any other
  parameter, carry (see
  [Routes and Pages](#routes-and-pages)).

  **Values reach SQL only as bound parameters.** The accepted values of `sprint`,
  `status`, and `type` are passed to the task read as
  bound parameters of its prepared statement; no parameter value is ever
  concatenated or interpolated into SQL text, and an ignored value reaches no
  statement at all (see `DATABASE.md § List All`). `q`, `page`, and `size` never
  reach SQL: they are applied in memory (see **Read cost** below).
- **Filter persistence.** The page remembers the reader's filter state across
  visits in one cookie that the server sets and reads; no script reads or writes
  it, and the page uses no browser storage (`localStorage`, `sessionStorage`, or
  IndexedDB).
  - **Explicit and bare requests.** A request to `/roadmaps/{name}/tasks` is
    **explicit** when its URL query string carries at least one occurrence of a
    parameter the page accepts — `q`, `sprint`, `status`, `type`, `page`, or
    `size` — whatever that occurrence's value, empty, unaccepted, or undecodable
    included. A request that carries none of the six is **bare**, whether its
    query string is empty or carries only parameters the page does not accept,
    such as `priority`.
  - **An explicit request takes its state from the URL alone.** Its active filter
    state is the one the query parameters give, with an absent parameter meaning
    no filter on its dimension, an empty search, or the default page size `25`,
    as **Query parameters** above states; the cookie is not read. Its HTTP 200
    response carries a `Set-Cookie` header setting the cookie to the request's
    accepted `q`, `sprint`, `status`, `type`, and `size` (see **The cookie's
    value** below).
  - **A bare request takes its state from the cookie, or from the defaults.** When
    the request carries the cookie, its active filter state is the one the
    cookie's value gives; when it does not, the active filter state is the
    **defaults**. A bare request always renders page 1, and its response carries
    no `Set-Cookie` header: a bare request never rewrites the cookie.
  - **The defaults.** Status: `BACKLOG`, `SPRINT`, `DOING`, and `TESTING` — every
    `TaskStatus` value except `COMPLETED`; type: no value, so every type; sprint:
    none, so every task; `q`: empty; size: `25`; page: `1`.
  - **The cookie.** One cookie, named `rmp_tasks_filters`, shared by every roadmap:
    its state is global, not per roadmap. The server sets it with exactly these
    attributes: `Path=/`, `Max-Age=31536000` (one year), `HttpOnly`, and
    `SameSite=Lax`. It carries no `Domain` attribute, so it is a host-only cookie,
    no `Expires` attribute, and no `Secure` attribute: the server speaks plain HTTP
    on every bind address, the loopback default included (see
    [Bind Address and Port Selection](#bind-address-and-port-selection)), and a
    user agent returns a `Secure` cookie only over a secure channel (RFC 6265,
    Section 4.1.2.5), so the attribute would stop the cookie from ever being sent
    back. Cookies are not isolated by port (RFC 6265, Section 8.5), so every
    `rmp web` server a browser reaches through one host name shares the cookie.
  - **The cookie's value.** The value is a query string in the
    `application/x-www-form-urlencoded` form, holding, in this order: `q`, when
    the term is not empty after the trim, as the search input echoes it (each byte
    that is not part of a valid UTF-8 sequence replaced by `U+FFFD`); `sprint`,
    when a sprint filter is accepted; one `status` per accepted status value and
    one `type` per accepted type value, each set in its enum order (see **Filter
    bar** above); and `size`, always, `25` included — for example
    `sprint=4&status=DOING&status=TESTING&size=25`. It never carries `page`. Every
    name and value is percent-encoded, a space being written `+`, so that the value
    holds only ASCII letters and digits and the characters `-`, `.`, `_`, `~`, `%`,
    `+`, `&`, and `=`, each of which is a `cookie-octet` (RFC 6265, Section 4.1.1).
    An explicit request whose accepted state holds no filter and no term sets the
    value `size=<n>` alone, so the next bare request lists every task rather than
    the defaults.
  - **The size limit.** RFC 6265, Section 6.1, obliges a user agent to store only
    cookies of at least 4096 bytes, name, value, and attributes together. When the
    encoded value would exceed **4000 bytes** — which only a long search term can
    cause — the response carries no `Set-Cookie` header, and the cookie the browser
    already holds, if any, stays as it was. The page itself is rendered from the
    request's URL as for any explicit request.
  - **Reading the cookie validates it like a URL.** The server parses the value as
    a query string and validates every part by exactly the rules of **Query
    parameters** above, applied to the roadmap being viewed: each part that is not
    accepted is ignored on its own, and the other parts are applied. A `sprint`
    that is not `none` and not the `id` of a sprint of **this** roadmap is ignored,
    because the one cookie serves every roadmap; a `page`, and every name other
    than `q`, `sprint`, `status`, `type`, and `size`, is ignored. A cookie present
    with no accepted part gives the state with no filter, no term, and size `25`,
    not the defaults: the defaults apply only when the cookie is absent. When the
    `Cookie` request header carries the name more than once, the first occurrence
    is read. No cookie content produces an error page or a status other than the
    route's own (see [Routes and Pages](#routes-and-pages)).
  - **Which responses set it.** Only an HTTP 200 response to an explicit request
    sets the cookie, a `HEAD` request's response included: `HEAD` carries the
    headers `GET` carries for the same request, `Set-Cookie` among them. A `404`,
    a `405`, or a `500` of this route sets no cookie.
  - **Links from other pages are bare.** Every link another page generates to the
    tasks page — the sidebar's Tasks entry, and the task page's `Back to tasks`
    link (see [Roadmap Task Page](#roadmap-task-page), **The way back**) —
    carries no query parameter, so following it restores the stored filter state.
    Every link this page generates to itself is explicit (see **Links keep the
    filters** below).
  - **What the cookie is, and is not.** The cookie holds a presentation choice
    only. Its values reach the page only after the validation above and through
    the same `html/template` escaping as a URL value (see **Escaping** below), and
    they reach SQL only as bound parameters, as URL values do. The cookie grants
    nothing: it is not a session, an identity, or an authorisation, and the server
    has none of those (see [Security and Constraints](#security-and-constraints),
    rule 3). It is never written to any roadmap database or graph store, and the
    server keeps no copy of it. How the route's responses are cached with the
    cookie in play is fixed by [Cache Policy](#cache-policy), rule 5.
- **What each filter matches, and how the criteria compose.** Each filter of the
  active filter state that has a value is one criterion — a dimension with several
  active values is still one criterion — and the search term, when it is not
  empty, is one more:
  - **Sprint is membership.** `none` admits a task that has no `sprint_tasks` row;
    a sprint `id` admits a task whose `sprint_tasks` row names that sprint. A task
    belongs to at most one sprint (see `DATABASE.md § Relationships`), so the two
    forms partition the roadmap's tasks between them and the sprints.
  - **Status and type are set memberships.** A task satisfies the status criterion
    when its `status` equals **any one** of the active status values, and the type
    criterion when its `type` equals any one of the active type values, each
    compared exactly against the spelling in `MODELS.md § Enums`. Within one
    dimension the values combine by **OR**. A dimension with no active value is no
    criterion and admits every task.

  Across dimensions the criteria combine by **AND**: the list shows the tasks that
  satisfy **every** active criterion, and a request
  with no active criterion shows every task of the roadmap. The conjunction is
  total: `?q=cache&status=DOING&status=TESTING&type=BUG` shows the tasks of type
  `BUG` whose status is `DOING` or `TESTING` and
  whose title or `#<id>` reference contains `cache`, and no other task.
  Adding a criterion can only shrink the list, never grow it, and no criterion
  re-admits a task another excluded. Adding a further value to a dimension that
  already has one can only grow the list, because it widens that dimension's OR.
- **The text search.** The search answers "which task is this?" from what
  identifies a task in its row.
  - **What it matches.** A task has exactly two **searchable texts**, two things its
    row displays: the task `title`, and the task reference `#<id>`, written with its
    leading `#`. A task matches a term when **either** searchable text contains the
    term: the title contains it, **or** the reference contains it. The two are
    matched **separately** and are never joined into one string, so a term made of
    the end of the title followed by the start of the reference is contained in
    neither and does not match the task. Because the reference is matched as the literal string `#42`, both `42`
    and `#42` find task 42 under the one substring rule below, with no special case
    for either form. Every other task field is excluded: a term occurring only in a
    task's `functional_requirements` does not match it. Every rule below that
    prepares "a task's searchable text" prepares each of the two on its own.
  - **Matching rule.** Matching is **case-insensitive** and by **substring**: a
    task matches when its prepared title, or its prepared reference, contains the
    prepared term.
    Preparing the term is three steps, in this order — trim, normalise, fold — and
    preparing a task's searchable text is two — normalise, fold — because the text
    is never trimmed: a task's own leading or trailing whitespace is part of its
    text. Whitespace inside the term is significant and is matched literally. A
    term that is empty after the trim is **no term at all**, and the search is not a
    criterion.
  - **The trim rule.** Every code point carrying Unicode's **White_Space**
    property is removed from the **start** of the term and from its **end**, and
    removal stops at the first code point that does not carry the property. The set
    is named by that property and by no platform's trimming function: `U+0085`
    (NEXT LINE) carries the property and **is** removed from the ends of a term,
    and `U+FEFF` (ZERO WIDTH NO-BREAK SPACE) does not carry it and is **not**
    removed, so a term pasted with a leading byte-order mark keeps it and matches
    nothing on an ordinary roadmap. The space, the tab, the carriage return, and
    the line feed a user can type are removed.
  - **The normalisation rule.** After the trim, the term and the task's searchable
    text are both normalised to Unicode's **Normalization Form C** — NFC, the
    canonical composition of the full canonical decomposition, as UAX #15 defines
    it — so that two byte sequences that render as the same text are one text: a
    task whose `title` holds a precomposed `é` (`U+00E9`) and a task whose `title`
    holds `e` followed by a combining acute (`U+0065 U+0301`) are both found by a
    term typed in either spelling.

    **Normalisation is for comparison only: never for storage, and never for
    display.** The bytes `rmp` stores stay exactly the bytes it was given, and the
    row renders the title the roadmap actually holds.

    **NFC and not NFD.** NFD decomposes every accented letter, so under it the
    term `cafe` would be a substring of a title `Café Lisboa onboarding` and `ae`
    of `Aérea`. NFC leaves a precomposed letter precomposed, so neither term
    matches and an accented word stays one unit. The rule answers what a term
    **is**, and it does not answer whether an accent should be ignored.

    **What the rule changes, measured.** Of Unicode's 1,112,064 code points,
    exactly **1,117** produce a different searchable text under this rule than
    without it, and **not one of them is ASCII**: the canonical singletons and the
    composition exclusions — `U+0340`, `U+0341`, `U+0343`, `U+0344`, `U+0374`,
    `U+037E`, `U+0387`, the `U+0958`..`U+095F` Devanagari set, and their kind. An
    ordinary Latin roadmap is untouched.

    **Two passes, not one, and normalise before folding.** The pipeline is trim,
    then NFC, then fold, then **NFC again**. The fold can produce a sequence that
    composes where the unfolded one did not: NFC leaves `H` followed by `U+0331` as
    two code points, the fold lowers the `H`, and `h` followed by `U+0331` composes
    to `U+1E96`, so without the second pass a task titled `H̱ydro` would not be
    found by the term `ẖ`. Measured over the 1,440,384 sequences of a folding code
    point followed by a non-starter, one pass leaves the result outside NFC on
    **70** of them and two passes leave it in NFC on all of them, so a third pass
    would change nothing and is not performed. Normalising **before** the fold is
    the only order that gives a title written with `U+0130` (LATIN CAPITAL LETTER I
    WITH DOT ABOVE) and a title written as `U+0049` followed by `U+0307` — the same
    text by Unicode's own definition — one searchable text.

    The server normalises with `golang.org/x/text/unicode/norm`, the Go project's
    own implementation of UAX #15 (see `BUILD.md § External Dependencies`).
  - **The folding rule.** The normalised term and the normalised searchable text
    are folded by Unicode's **simple lowercase mapping**: the single replacement
    code point the Unicode Character Database gives a code point, applied to each
    code point on its own, with a code point that has no such mapping folding to
    itself. The fold is **unconditional** (no context changes what a code point
    folds to), **one code point in, one code point out** (it never lengthens or
    shortens the text), and **locale-independent** (a locale-sensitive case
    conversion MUST NOT be used). It is deliberately not Unicode's full Default Case
    Conversion, and the two code points on which the two differ are fixed:
    `U+0130` folds to `U+0069` alone, never to `U+0069 U+0307`; and `U+03A3`
    (GREEK CAPITAL LETTER SIGMA) folds to `U+03C3` in every position, word-final
    included, never to the final form `U+03C2`. **Nothing is rewritten after the
    mapping**: a `U+03C2` the user typed folds to itself and is not rewritten to
    `U+03C3`.
  - **One implementation.** The term and every task's searchable text are
    prepared by the server alone, through one normalisation function and one
    folding function shared by both, so the two cannot be prepared by two
    implementations of one description.
  - **No malformed term is an error.** Every string is a valid term. A term that
    matches nothing renders the no-match empty state (see **Empty states** below)
    with HTTP 200, and a term longer than any searchable text simply matches
    nothing. A term whose bytes are not valid UTF-8 has each invalid byte replaced
    by `U+FFFD` before it is prepared, and is then matched like any other term; the
    search input echoes the term with the same replacement (see **Escaping**
    below).
- **Order.** The rows appear in one deterministic order over the whole filtered
  set: descending `priority`, then ascending `created_at` for tasks of equal
  priority, then ascending `id` for tasks equal on both. The first two keys are the
  default `ListTasks` ordering (see `DATABASE.md § Main SQL Queries`, "List All");
  the `id` key makes the order total, so that a task equal to another on the first
  two keys lands on the same page on every request, and no task appears on two
  pages or on none. Filtering and searching remove rows from that order and never
  reorder the rows that remain.
- **Pagination.** The filtered list is paginated **on the server**.
  - **Page size.** `size` rows per page, one of `10`, `25`, `50`, and `100`; the
    default is `25`.
  - **Page count.** The last page is `ceil(total / size)`, where `total` is the
    number of tasks satisfying every active criterion, and there is always at least
    one page. A `page` beyond the last page renders the last page, and an
    unacceptable `page` renders page 1 (see **Query parameters** above).
  - **Rows of a page.** Page `p` shows the rows at positions `(p - 1) × size + 1`
    to `min(p × size, total)` of the order above.
  - **The card footer.** The footer is
    `<div class="card-footer">` holding one `<div class="row g-2 align-items-center">`
    whose three `col-auto` columns carry, in this order, the range text, the
    rows-per-page selector, and the pagination bar, the last column also carrying
    `ms-auto` so that the pagination bar sits at the footer's trailing edge. On a
    narrow viewport the columns wrap onto further lines rather than overflowing
    the card.
  - **Range text.** The footer's first column states
    `Showing <a> to <b> of <total> entries`, where `<a>` and `<b>` are the first
    and last positions the page shows and `<total>` is the number of tasks
    satisfying every active criterion — the filtered total, not the roadmap's.
    The text is rendered as Tabler's table-footer idiom,
    `<p class="m-0 text-secondary">`, holding each of the three numbers in its own
    `<span>`.
  - **Pagination bar.** The footer's third column carries a Tabler
    numbered pagination bar with the same shape, the same sliding window with
    ellipsis, the same **Previous** and **Next** chevrons, and the same markup as
    the audit log page's (see [Roadmap Audit Log Page](#roadmap-audit-log-page),
    **Pagination controls**, **Sliding window with ellipsis**, and **Pagination
    markup**), inside a `<nav>` whose `aria-label` is `Task list pages` (see
    [UI Framework](#ui-framework), rule 15). A list of one page still shows the bar,
    holding its single page as the active item.
  - **Rows-per-page selector.** The footer's second column carries the visible
    text `Rows per page` followed by a Tabler button group, `<div class="btn-group" role="group">` labelled by
    that text, holding four links, each carrying the Tabler classes `btn` and
    `btn-sm`, reading `10`, `25`, `50`, and `100` in that order. The link of the
    active page size also carries `active` and `aria-current="true"`.

    **The active size is not shown by colour alone.** The active link is drawn
    with a **solid fill** — for example Tabler's primary colour — that the inactive
    links do not carry, so it differs from them in fill and not in hue alone
    (WCAG 2.2 Success Criterion 1.4.1, Use of Color). The fill has a contrast ratio
    of at least 3:1 against the card footer's background and against the fill of
    an inactive link beside it (Success Criterion 1.4.11, Non-text Contrast), and
    the active link's text has a contrast ratio of at least 4.5:1 against the fill
    (Success Criterion 1.4.3, Contrast (Minimum)). The state is also exposed to
    assistive technology by `aria-current="true"`, as for the pagination bar's
    active item. Where the vendored `btn.active` styling falls short of these
    ratios, the project override stylesheet `static/style.css` sets the fill (see
    [UI Framework](#ui-framework), rule 10).
  - **Links keep the filters.** Every link the page generates to the page itself —
    each page number and chevron of the pagination bar, and each rows-per-page
    link — carries every filter of the active filter state, whether the URL, the
    cookie, or the defaults supplied it: `sprint` with its active value, and one
    `status` or `type` occurrence per active value of that repeatable parameter,
    every active value included; it carries no ignored value. A pagination link sets
    `page` and keeps `size`; a rows-per-page link sets `size` and carries no
    `page`, so changing the page size returns to page 1; submitting the filter bar
    carries no `page`, so changing a filter returns to page 1. A generated link
    carries `page` only when it is greater than `1`, `size` only when it is not
    `25`, and `q` only when the term is not empty after the trim; the `q` a link
    carries is the term as the search input echoes it, with each byte that is not
    part of a valid UTF-8 sequence replaced by `U+FFFD`, then percent-encoded; the
    order of parameters in a generated link carries no meaning. A generated link
    is always an explicit request (see **Filter persistence** above): a link that
    these rules would leave with no parameter at all — page 1, size `25`, no
    filter, and no term — carries `size=25`, so that following it reproduces the
    list it names rather than the stored state. The filter bar's form is not
    a generated link: it carries `size` on every submission, `25` included (see
    **Filter bar** above).
  - **What the total costs.** The total is the size of the filtered set the page
    already holds in memory, so it costs no query of its own, and it is correct by
    construction: the task read is bounded by the filters alone and never by a
    page, and the `rmp task list` display default — `-l, --limit <n>`, default
    `100` (see `COMMANDS.md § List Tasks`) — MUST NOT be applied to it.
- **Row content.** The table's header row, inside `<thead>`, names the columns;
  each row of `<tbody>` presents one task, in these cells and in this order:

  | Column heading | Cell content |
  |---|---|
  | `ID` | The **id badge**: a Tabler badge reading `#<id>` and carrying the classes `bg-black` and `text-white`, exactly the id badge of the sprint board's card (see [Sprint Detail Sub-Template](#sprint-detail-sub-template), **The card**). The heading cell carries the Tabler class `w-1`. |
  | `Title` | The task `title`, as a link to the task's own page (see **Links to the task page** below). A long title wraps within its cell at word boundaries; the cell carries no `text-break` and no other rule that breaks a word, and it has a minimum width of `16rem` (see **Column widths** below). |
  | `Type` | The **type badge**: a Tabler badge reading the task's `type` exactly as the `TaskType` enum spells it, coloured by the task type mapping. |
  | `Status` | The **status badge**: a Tabler badge reading the task's `status` exactly as the `TaskStatus` enum spells it, coloured by the task status mapping. |
  | `Severity` | The **severity badge**: a Tabler badge reading `S` immediately followed by the task's `severity`, coloured by the severity band mapping. |
  | `Priority` | The **priority badge**: a Tabler badge reading `P` immediately followed by the task's `priority`, coloured by the priority band mapping. |
  | `Created` | The task's `created_at`, in the display form of [Date and Time Display](#date-and-time-display), in a `<time>` element, preceded by the Tabler Icons calendar icon `<i class="ti ti-calendar me-1" aria-hidden="true"></i>`; the cell carries the Tabler class `text-secondary`, so the date is muted. |

  Every badge colour is the one the mappings of
  [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours)
  assign; the list introduces no colour and no band of its own. The `S` and `P`
  badge labels, and the order in which severity precedes priority, are those of the
  sprint board's card, so a reader meets a task's reference, severity, priority,
  and type in one form on both pages (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template), **The card**).

  The table has **no `Sprint` column and no `Actions` column**. The sprint a task
  belongs to is shown on the task's page (see
  [Roadmap Task Page](#roadmap-task-page)), and the list is narrowed to a sprint,
  or to the tasks of no sprint, by the sprint filter (see **Filter bar** above); the
  task page is opened through the title link, so the row carries no `View` link.

  **Column widths.** The table keeps its automatic layout, which shares the
  available width among the columns by their content. A class that breaks words
  anywhere, such as Tabler's `text-break`, MUST NOT be applied to the title cell:
  under the automatic layout it lets the column shrink to the width of a few
  characters, so that at viewports of `992px` and below a title is squeezed into a
  column one or two words wide. Instead, the title cell carries a class of the
  project override stylesheet `static/style.css` declaring `min-width: 16rem`. A
  title word longer than its cell is not broken: the table grows wider than the card, and it scrolls horizontally
  inside its `table-responsive` container, never the page (see
  [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
  rule 9).

  The row presents a subset of the task's fields by design. Every field of the
  `Task` model is shown on the task page the row links to (see
  [Roadmap Task Page](#roadmap-task-page)). The row does not redefine any field;
  `MODELS.md` and `DATABASE.md` remain canonical.
- **Links to the task page.** Each row carries exactly one link, the title, and
  it leads to the task's own page, `/roadmaps/{name}/tasks/{id}`. Following it is
  an ordinary navigation to a server-rendered page; the list fetches nothing when
  the link is followed and opens no write path. It is an `<a>` element with an
  `href`, so a pointer click, a touch tap, and the Enter key follow it through the
  browser's own activation behaviour with no added JavaScript, and the browser's
  own link behaviours — opening in a new tab or window, copying the address —
  apply to it. Its accessible name is its visible text, the task `title`. In the
  keyboard's tab order a row therefore contributes one stop, its title link. The
  link shows a visible focus indicator whenever it receives keyboard focus (WCAG
  2.2 Success Criterion 2.4.7, Focus Visible; see [UI Framework](#ui-framework),
  rule 21).

  **The row itself is not a link.** No `<tr>` carries an `href`, a `role`, a
  `tabindex`, or an event handler, and no row is made clickable by any other means:
  a row is not an activatable element, so a row made to look clickable would be a
  target the keyboard cannot reach.
- **Empty states.** When no task satisfies the request, the list card keeps its
  header, filter bar included, so the reader can change the filters in place; the
  table is not rendered, and a `card-body` holding Tabler's empty-state markup
  (`<div class="empty">`, with `empty-title` and `empty-subtitle`) takes its place;
  the card footer — range text, rows-per-page selector, and pagination bar — is not
  rendered.
  The page never replaces the card with a page-level empty state. Two conditions
  read differently:
  - **A roadmap with no task** shows the title
    `No tasks yet` and a subtitle stating that tasks are created with the CLI,
    `rmp task create`, and no Reset link, whatever the active filter state — from
    the URL, from the cookie, or the defaults — because no filter can make a task
    appear. Whether the roadmap holds any task is established only when the
    filtered list is empty (see **Read cost** below).
  - **A roadmap that holds at least one task, none of which satisfies the active
    criteria** — a roadmap whose tasks are all `COMPLETED`, requested under the
    defaults, among them — shows
    the title `No task matches the filters` and a subtitle inviting the reader to
    change or reset the filters, with a link carrying the Tabler class `btn` and
    reading `Reset` in the empty state's `empty-action`. Its `href` is the page's
    path carrying the defaults written explicitly — the four default status
    values, one `status` occurrence each (see **Filter persistence** above) — with
    no `sprint`, no `type`, no `q`, and no `page`, and carrying `size` only when
    the active page size is not `25`: for example
    `/roadmaps/{name}/tasks?status=BACKLOG&status=SPRINT&status=DOING&status=TESTING`.
    Following it restores the default filter state and keeps the page size, and,
    being an explicit request, writes that state to the cookie. One message covers the term and the filters
    together, because the list is their conjunction and naming one of them would
    attribute the empty result to a cause the page cannot know.
- **Escaping.** The term is the one caller-supplied string this page echoes back —
  into the search input's `value`, with each byte that is not part of a valid UTF-8
  sequence replaced by `U+FFFD`, so the page never carries an invalid byte — and it
  is escaped there by `html/template`'s
  contextual auto-escaping (see [Frontend Rules](#frontend-rules), rule 1), as is
  every task and sprint value the page renders. A term read from the filter-state
  cookie is echoed and escaped exactly as a term read from the URL (see **Filter
  persistence** above). A filter value, from the URL or from the cookie, is never
  echoed as caller-supplied text: it only decides which of the options the server
  emitted from the roadmap's own sprints is marked `selected`, which of the
  checkboxes the server emitted from an enum are marked `checked`, and which of the
  server's own texts a dropdown toggle shows — the *any* text, the enum's own
  spelling of the one active value, or the count of active values — so no
  caller-supplied string other than the term reaches the page. A term containing HTML markup
  therefore renders as visible characters and introduces no element, attribute, or
  script into the page. Generated links percent-encode every parameter value they
  carry.
- **Markup.** The page obeys the markup rules already in force and introduces no
  exception to them. Templates carry no inline `style` attribute, every class the
  page emits is defined in the vendored Tabler distribution or in the project
  override stylesheet `static/style.css`, and the page uses Tabler's own components
  — form controls, dropdowns, the card, the table, badges, the button group,
  pagination, and the empty state — without hand-rolling any of them (see
  [UI Framework](#ui-framework), rules 8 and 10). The page introduces no inline
  script and loads no project script of its own — its one script is the vendored
  Tabler script (see **Scripts** above) — and the Content-Security-Policy in
  [Security Headers](#security-headers) is unchanged. The page keeps the admin
  shell and the page header every other page uses, governed by
  [UI Framework](#ui-framework), rules 11 to 18.
- **Read-only.** The page renders data only. It offers no control that creates,
  edits, deletes, moves, or reorders a task, no selection, and no modal. Its one
  form submits by `GET` to the page itself and only narrows what the page shows;
  like every other request to the interface it writes nothing to any roadmap
  database or graph store — the one state an explicit request changes is the
  browser's filter-state cookie (see **Filter persistence** above) — and the `rmp` CLI
  remains the sole write path for every task (see
  [Security and Constraints](#security-and-constraints)). Read-only constrains
  what the page may **change**, not what it may **show**: filtering and paginating
  alter the view of the data and never the data.
- **Relationships shown.** The page surfaces **task-to-sprint membership** through
  the sprint filter alone — the table has no `Sprint` column — and introduces no
  other relationship. The parent/subtask hierarchy and the dependency edges are shown on
  the task page (see [Roadmap Task Page](#roadmap-task-page)). The presentation
  MUST reflect the relationships defined in `DATABASE.md § Relationships`; it
  introduces no new relationship.
- **Read cost.** Rendering the page performs **two** reads, and a third only in
  the case item 3 names:
  1. **one** read of the roadmap's sprints, for the sprint select's options and
     for validating the active `sprint`, whether the URL or the cookie supplied it
     (see `DATABASE.md § List Sprint Titles`);
  2. **one** read of the roadmap's tasks through the task listing, carrying one
     predicate per filtered dimension — sprint, status, and type — with one bound
     parameter per distinct active value of that dimension, and the ordering of
     **Order** above (see `DATABASE.md § List All`);
  3. **one** count of the roadmap's tasks (see `DATABASE.md § Count Roadmap
     Tasks`), issued **only** when the filtered list is empty and the task read
     carried at least one predicate, to choose between the two empty states (see
     **Empty states** above). When the task read carried no predicate, it
     returned every task of the roadmap, so the roadmap holds a task exactly when
     it returned a row, and no count is issued; a non-empty filtered list issues
     no count either.

  The page resolves no task's sprint: no row shows one, and the sprint filter is
  applied by the second read's own predicates, so the grouped sprint-resolution
  query (`DATABASE.md § Resolve the Sprint of Many Tasks (Grouped)`) is not issued
  by this page.

  The search term, the total, the page selection, and the slicing of the page's
  rows are computed in memory over the rows the second read returned: the search's
  normalisation and folding rules cannot be expressed in SQLite, so the term is
  applied after the read, and the page is selected after the term. The page reads
  no comment, because
  the list shows no comment information. The number of queries the page issues —
  two, or three for an empty filtered list — does
  not grow with the number of tasks, the number of sprints, the page size, or the
  number of active filters, and no query is issued per row. Following a row's link
  to its task page is a separate request for that one task, made only when the user
  follows it.
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
  `Sprint #<ID>` followed by the sprint's status badge in the pretitle, so the
  pretitle reads `Sprint #<ID>` and then the badge, and the `title` alone in the
  title (see [Shared Page-Header Partial](#shared-page-header-partial)); the
  roadmap name is not repeated there. The badge takes its colour from the sprint
  status mapping in
  [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours).
  The actions column carries a link back to
  the roadmap's sprints page. The page does not redefine these fields;
  `MODELS.md` remains canonical.
- **Sprint details.** The page shows the sprint's details, using the fields
  defined for the `Sprint` model in `MODELS.md § Sprint`: the page header presents
  the sprint `id`, its `title`, and its status, and the Sprint details card presents its
  description, `created_at`, `started_at`, and `closed_at` (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template)); the three timestamps,
  and the timestamps of the sprint's comments, are displayed as specified in
  [Date and Time Display](#date-and-time-display). The page does not
  show the sprint's execution `order` or its capacity (`max_tasks`).
  The page presents the sprint status clearly, through the status badge in the
  page header (the status enum and lifecycle are defined in `MODELS.md § Enums`
  and `STATE_MACHINE.md § Sprint State Machine`).
  The sprint `description` is authored as Markdown, and the page renders it as the
  HTML the Markdown renderer produces from it; the rendered content wraps within
  its card, so no forced horizontal scrolling of the page is introduced (see
  [Markdown Rendering](#markdown-rendering)). The page does not redefine these
  fields; `MODELS.md` and `DATABASE.md` remain canonical.
- **Member-tasks board.** The page presents the sprint's tasks as a Kanban board
  of three fixed columns — `WAITING`, `DOING`, and `CLOSED` — holding one card per
  task, placed between the sprint details above it and the sprint's comments below
  it. The columns group the tasks by one fixed categorisation of the task status —
  `WAITING` holds `BACKLOG` and `SPRINT`, `DOING` holds `DOING` and `TESTING`, and
  `CLOSED` holds `COMPLETED` — so the three column counts sum to the sprint's total
  number of member tasks, and each column orders its own cards: the `WAITING`
  column keeps the planned in-sprint execution order, which is the `sprint_tasks`
  order (the ordered set of task IDs the `Sprint` model exposes as `tasks`; see
  `MODELS.md § Sprint` and `DATABASE.md § Relationships`), while the `DOING` and
  `CLOSED` columns lead with the most recent — `started_at` descending and
  `closed_at` descending — because those two columns record what has happened
  rather than what is planned, with the planned order breaking their ties (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Order within a
  column**). Each card is a link to that task's own page, and the card **is** the
  link, so it is followed from the pointer, from touch, and from the keyboard
  alike (see [Roadmap Task Page](#roadmap-task-page)). Each column header
  carries, at its trailing
  edge, a chevron toggle that collapses the column to a narrow strip and expands
  it again; the toggle changes only how the board is presented. Each page load
  starts every column that holds no task collapsed and every column that holds a
  task expanded, except that a sprint with no member task starts with all three
  columns expanded (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Column collapse**).
  The board carries no control that moves a task between columns (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template)).
- **Sprint comments.** After the member-tasks board, the page shows the sprint's
  own comments in a Comments card, oldest first (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template)). The card shows the
  comments of the sprint itself, not those of its member tasks; a task's comments
  are shown on that task's own page.
- **Path parameters.** `{name}` is validated against the roadmap-name rules
  exactly as on the other roadmap routes (the path-traversal guard in
  [Routes and Pages](#routes-and-pages) and
  [Security and Constraints](#security-and-constraints)); an invalid or
  nonexistent `{name}` returns HTTP `404 Not Found`. `{id}` MUST be a valid
  integer; a non-integer `{id}`, or an integer `{id}` that is not the `id` of a
  sprint belonging to the named roadmap, returns HTTP `404 Not Found` (see the
  HTTP status mapping in [Routes and Pages](#routes-and-pages)).
- **Read-only.** The page renders data only. It contains no form, button, or
  link that submits a change; there is no edit affordance of any kind. The task
  cards are links to read-only task pages, and the only buttons the board
  carries are the column collapse toggles, which change only the board's
  presentation; neither submits anything or changes any data.

### Roadmap Task Page

- **Route:** `GET /roadmaps/{name}/tasks/{id}`
- **Content:** A read-only presentation of a single task of the named roadmap,
  rendered on the server from that roadmap's `project.db`: every field of the task,
  the task's comments, and the context of the sprint the task belongs to. The page
  is the one place in the interface that shows a task's full field set; the rows
  of the tasks page's list and the cards of the sprint page's board show a subset
  of it and link here (see [Roadmap Tasks Page](#roadmap-tasks-page), **Links to
  the task page**, and
  [Sprint Detail Sub-Template](#sprint-detail-sub-template), **The card is a link
  to the task page**). Every task has its page, whatever its status. The page does
  not redefine any field; `MODELS.md § Task`, `MODELS.md § Task Comment`, and
  `DATABASE.md` remain canonical.
- **Active view.** A task's page belongs to the roadmap's Tasks view: the
  admin-shell sidebar highlights the **Tasks** link, marked as
  [UI Framework](#ui-framework), rule 14, requires, whether or not the task
  belongs to a sprint.
- **Page header.** The page header is rendered by the shared partial (see
  [Shared Page-Header Partial](#shared-page-header-partial)): the pretitle reads
  `Task #<ID>` (the task's `id`) followed by the task's status badge, and the title
  holds the task's `title` alone, with no badge; the roadmap name is not repeated
  there. The badge's text is the task's `status` exactly as the `TaskStatus` enum
  spells it (see `MODELS.md § Enums`), and its colour is the variant the task
  status mapping assigns to that value in
  [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours).
  The header states the task's `id`, `title`, and `status`, so the page body does
  not repeat them.
- **The way back.** The page header's actions column carries exactly one link,
  back to the roadmap's tasks page at `/roadmaps/{name}/tasks`, labelled
  `Back to tasks`, in the idiom the Roadmap Sprint Page uses for its own back link
  (see [Roadmap Sprint Page](#roadmap-sprint-page) and
  [UI Framework](#ui-framework), rule 16). The link carries no query parameter, so
  it opens the list on its first page with the filter state stored in the tasks
  page's filter-state cookie, or with the default filter state when no cookie is
  stored (see [Roadmap Tasks Page](#roadmap-tasks-page), **Filter persistence**); a
  reader who reached the page from a filtered list, or from a later page of it,
  returns to that exact list with the browser's own Back navigation, because the
  list's filters, page, and page size travel in its URL (see
  [Roadmap Tasks Page](#roadmap-tasks-page)). The way to the sprint page is the
  sprint link of the **Sprint card** below, not a second link in the actions
  column, so each destination is offered once on the page (see
  [Shared Page-Header Partial](#shared-page-header-partial), rule 5). The page
  uses the page-header actions column rather than a breadcrumb because the actions
  column is the idiom the interface already uses for the way out of a record's
  page, and no page of the interface renders a breadcrumb.
- **Layout.** The page body holds one Tabler `row row-cards` of two columns:
  1. the **side column** (`col-12 col-lg-4`), which holds the **Sprint card** and,
     below it, the **Details card**;
  2. the **main column** (`col-12 col-lg-8`), which holds the four **Markdown
     field cards** and, below them, the **Comments card**.

  The side column comes first in the document. Below Tabler's `lg` breakpoint
  (`992px`) the two columns stack into one, so a reader on a narrow viewport meets
  the sprint context and the short fields before the long text. At `lg` and wider
  the side column carries Tabler's `order-lg-last` utility and stands to the right
  of the main column, which takes two thirds of the width. The page is not a
  full-height page region (see [Full-Height Page Regions](#full-height-page-regions)):
  it grows with its content, and the page scrolls vertically to reach the rest of
  it.
- **Sprint card.** A compact Tabler card whose `card-header` carries the card
  title `Sprint`. It gives the context of the sprint the task belongs to and
  nothing more: it is context, not a sprint view, so it shows no sprint
  description, no sprint timestamp, no member task, and no sprint comment.
  Membership, not status, decides which of its two forms it takes: a task whose
  status is `BACKLOG` and that is still a member of a sprint shows the sprint form
  (see `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status`).
  - **When the task belongs to a sprint**, the card body shows, in this order:
    1. **The sprint**, as one link to the sprint's own page at
       `/roadmaps/{name}/sprints/{id}` whose text is `Sprint #<id>` followed by the
       sprint's `title`, followed, outside the link, by the sprint's status badge,
       coloured by the sprint status mapping in
       [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours).
       Both the `id` and the `title` are shown because the `title` alone does not
       identify a sprint: `MODELS.md § Sprint` places no uniqueness constraint on
       the `title`, so two sprints of one roadmap may carry the same title, while
       the `id` is the primary key and is unique. A task belongs to at most one sprint (see
       `DATABASE.md § sprint_tasks Table (1:N Relationship)`), so the card names
       at most one.
    2. **The task's position** in the sprint's planned execution order, as the
       text `Position <n> of <m>`. `<m>` is the sprint's number of member tasks,
       and `<n>` is the task's 1-based rank among them in `sprint_tasks`
       `position` ascending order, the order the `WAITING` column of the sprint
       page's board keeps (see
       [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Order within a
       column**). A sprint's positions run densely from `0` to `N-1` (see
       `DATABASE.md § Position Density Within a Sprint`), so `<n>` is the task's
       stored `position` plus one: the third of eleven member tasks reads
       `Position 3 of 11`. The position is shown whatever the task's status,
       because it states the task's place in the plan, not what has happened to
       it.
    3. **The sprint's progress**, as the text `<c> of <m> tasks completed`
       followed by a progress bar. `<m>` is the sprint's number of member tasks, as
       above, and `<c>` is the number of them whose status is `COMPLETED` — the
       count the `CLOSED` column of the sprint page's board carries, under the one
       categorisation every presentation of a sprint's progress shares (see
       [Sprint Detail Sub-Template](#sprint-detail-sub-template), **The column
       counts partition the sprint**). `<m>` is at least `1`, because the task
       itself is a member. The bar is the native `<progress>` element carrying
       Tabler's `progress` and `progress-sm` classes, with the `value` attribute
       `<c>` and the `max` attribute `<m>`, and with the accessible name
       `Sprint progress`, carried by its `aria-label`. The native element is used
       because Tabler's `<div class="progress-bar">` form states its fill as an
       inline `style` width, which [UI Framework](#ui-framework), rule 10, forbids,
       while the vendored distribution styles the native element under the same
       `progress` class; the bar therefore needs no inline style and no script.
  - **When the task belongs to no sprint**, the card body shows the text
    `In the backlog: this task belongs to no sprint.` and nothing else: no link, no
    badge, no position, and no progress bar.
- **Details card.** A Tabler card whose `card-header` carries the card title
  `Details` and whose body is a Tabler datagrid, the idiom of the Sprint details
  card of the Roadmap Sprint Page (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template), rule 2). It holds
  exactly these fields, in this order, each under the label given:

  | Label | Field | Presentation |
  |---|---|---|
  | `Type` | `type` | The value as the `TaskType` enum spells it, as plain text and not as a badge |
  | `Severity` | `severity` | A badge whose text is the integer alone, coloured by the severity bands |
  | `Priority` | `priority` | A badge whose text is the integer alone, coloured by the priority bands |
  | `Parent task` | `parent_task_id` | The task reference `#<id>`, as a link to that task's page |
  | `Subtasks` | `subtask_count` | The integer, `0` included |
  | `Depends on` | `depends_on` | Each id as the task reference `#<id>`, as a link to that task's page, in the order the field lists them |
  | `Blocks` | `blocks` | Each id as the task reference `#<id>`, as a link to that task's page, in the order the field lists them |
  | `Created` | `created_at` | The display form of [Date and Time Display](#date-and-time-display) |
  | `Started` | `started_at` | As `Created` |
  | `Tested` | `tested_at` | As `Created` |
  | `Closed` | `closed_at` | As `Created` |
  | `Commit open` | `commit_open` | The hash as stored, in full, as plain text in a monospaced font |
  | `Commit close` | `commit_close` | As `Commit open` |

  `Severity` precedes `Priority` for the reason the sprint board card's badge line
  puts `S<n>` before `P<n>`: a reader meets the two values in the same order on the
  card and on the page (see [Sprint Detail Sub-Template](#sprint-detail-sub-template),
  **The card**).

  An absent value — a null `parent_task_id`, an empty `depends_on` or `blocks`
  list, an unset timestamp, an absent commit hash — is shown as the page's
  placeholder for an absent value, an em dash.

  **A commit hash is shown whole.** A hash is up to 64 characters long (see
  `MODELS.md § Task`) and is read character by character when it is compared
  against a repository, so it is never truncated and never ends in an ellipsis:
  when it is wider than its datagrid column it wraps onto further lines, breaking
  between any two characters, and every character stays visible.

  **The datagrid has two columns on a narrow viewport and one in the side
  column.** Below Tabler's `lg` breakpoint (`992px`), where the page is one
  stacked column (see **Layout** above), the datagrid lays its fields out in
  exactly **two** columns of equal width, filled row by row in the order of the
  table above, which halves the card's height against one field per row. At
  `992px` and wider, where the card sits in the `col-lg-4` side column, the
  datagrid lays its fields out in exactly **one** column. The column count is fixed
  by these two rules and not by the vendored datagrid's own default, whose minimum
  column width of `15rem` (`240px`) makes the grid wider than the side column
  between `992px` and about `1055px` and so scrolls the page horizontally. The
  rules are declared in the project override stylesheet, scoped to this card, and
  no template carries a `style` attribute for them (see
  [UI Framework](#ui-framework), rule 10). Neither the order of the fields nor the
  order of the cards changes with the column count.

  **Task references are links.** Each task reference in `Parent task`,
  `Depends on`, and `Blocks` is one `<a>` element whose text is `#<id>` and whose
  `href` is `/roadmaps/{name}/tasks/{id}`: the page of the referenced task, in the
  same roadmap, built only from the validated roadmap segment of the request path
  and the referenced task's integer `id`, never from text an author wrote. The
  link carries no `target` and no `rel`, so it opens in the same tab, and the
  browser's own link behaviours — a middle click, the context menu — open it in a
  new one. The references of one field are separate links, so each is a target of
  its own for the pointer, touch, and the keyboard. An em dash is not a link.

  The commit hashes carry no link to any code-hosting service and no copy
  control: the interface is read-only and offline, and it holds no repository URL
  from which such a link could be built.

  **No badge label on the priority and severity badges.** The badge labels `P`
  and `S` that the sprint board card's `priority` and `severity` badges carry (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template), **The card**), and
  that the tasks page's list repeats so that a task reads the same in both places
  (see [Roadmap Tasks Page](#roadmap-tasks-page), **Row content**), are not
  rendered here. Those labels exist because a card shows the
  two values with no field name beside either of them; this card names every field
  it displays, so the field's own name already stands beside each of these two
  values, and a badge label would state the same thing twice. The badge colours are
  the same either way, because the mapping keys on the value and never on the
  badge's label (see
  [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours)).
- **Markdown field cards.** The main column holds one Tabler card for each of the
  four long free-text fields, in this order, each carrying its card title in its
  `card-header`: `Functional requirements` (`functional_requirements`),
  `Technical requirements` (`technical_requirements`), `Acceptance criteria`
  (`acceptance_criteria`), and `Completion summary` (`completion_summary`). The
  four fields are authored as Markdown, and each card body presents its field as
  the HTML the server's Markdown renderer produces from it, in the renderer's
  ordinary form, inside the field's Markdown container (see
  [Markdown Rendering](#markdown-rendering)). A field whose value is empty or null
  keeps its card, which shows the em dash in place of the container. The Markdown
  container of each field card, and of every comment `body` in the Comments card,
  spans the full width of its card's body: it carries no line-length limit (see
  [Markdown Rendering](#markdown-rendering), rule 13). The rendered
  content wraps within its card, and a wide table or code block scrolls inside its
  own box, so no forced horizontal scrolling of the page is introduced.
- **Comments card.** The last card of the main column presents the task's
  comments — the task's work log — as a chronological timeline. The fields of a
  comment are defined for the `TaskComment` model in `MODELS.md § Task Comment`;
  the page does not redefine them. The card is the Comments card of the Roadmap
  Sprint Page applied to a task's comments (see
  [Sprint Detail Sub-Template](#sprint-detail-sub-template), rule 4):
  - **Card header.** A `card-header` with the card title `Comments` and a Tabler
    badge showing the number of comments, in the neutral `bg-secondary-lt`
    variant, because a comment carries no status for the colour mapping to key on
    (see
    [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
    rule 2). At the header's trailing edge, inside Tabler's card-header actions
    container `<div class="card-actions">`, the header states the order of the
    log in the secondary text colour (`text-secondary`) with exactly the words
    `Oldest first`, as the sprint page's Comments card does. The text is shown
    whether or not the task has comments; it is text, not a control.
  - **Order and completeness.** Oldest first, exactly the order `task comment-list`
    returns (`created_at` ascending, comment `id` ascending as the tie-breaker; see
    `DATABASE.md § Comments`). The timeline is a log, and the order is what makes
    it readable as one. Every comment of the task is rendered: no type filter and
    no count limit.
  - **What each entry shows.** For one comment, in order: its `type` as a badge,
    its `created_at` timestamp, its `updated_at` timestamp when that value is not
    null (marking the entry as edited), and its `body`. Both timestamps are
    displayed as specified in [Date and Time Display](#date-and-time-display).
  - **Markup.** The timeline uses Tabler's Timeline component, which the vendored
    `tabler.min.css` already provides (see
    [Embedded Asset Categories](#embedded-asset-categories)). The structure is an
    unordered list `<ul class="timeline">` whose items are
    `<li class="timeline-event">`, each containing a
    `<div class="timeline-event-icon">` holding a Tabler icon
    (`<i class="ti ti-message"></i>`) and a `<div class="card timeline-event-card">`
    whose `card-body` carries the timestamps, the type badge, and the rendered body
    in its Markdown container (see [Markdown Rendering](#markdown-rendering),
    rule 13).
  - **Type badge colour.** The comment type renders as a neutral Tabler badge,
    `bg-secondary-lt`, for every one of the seven type values. The semantic colour
    mapping covers task and sprint status, task type, priority, and severity only;
    it is not extended to comment types, and no per-type colour is introduced.
  - **Body rendered as Markdown.** A comment body is authored as Markdown, and the
    entry presents it as the HTML the Markdown renderer produces from it, wrapping
    within the card (see [Markdown Rendering](#markdown-rendering)).
  - **Empty state.** When the task has no comments, the card shows a clear
    empty-state message in place of the timeline rather than an empty list; the
    card itself is always present.
- **Rendered on the server, complete.** Every value the page shows is in the HTML
  the server sends. The page fetches nothing after it loads and loads no script of
  its own: it reads the same with scripting disabled, and it carries no inline
  script, so the Content-Security-Policy in
  [Security Headers](#security-headers) is unchanged. Every value is rendered
  through `html/template`'s contextual auto-escaping, with the single exception of
  the HTML the Markdown renderer produces for the four Markdown fields and for
  every comment `body` (see [Frontend Rules](#frontend-rules), rules 1 and 7, and
  [Security and Constraints](#security-and-constraints), rule 7). A task `title`
  containing HTML markup therefore renders as visible characters, in the page
  header and in the document title alike.
- **Read-only.** The page renders data only. It contains no form, no button, and
  no link that submits a change; there is no edit affordance of any kind, and no
  input other than the disabled checkbox of a rendered Markdown task-list item,
  which can be neither checked nor unchecked (see
  [Markdown Rendering](#markdown-rendering), rule 3). Its links — the back link,
  the sprint link, the task-reference links of the Details card, and the links of
  rendered Markdown — only navigate. Comments are
  displayed, never created, edited, or deleted from the web interface, and the
  `rmp` CLI remains the sole write path (see
  [Security and Constraints](#security-and-constraints)).
- **Read cost.** The page issues a fixed number of read queries, whatever the task
  holds:
  1. one for the task's own field set, `subtask_count`, `depends_on`, and `blocks`
     included;
  2. one for the task's comments, through
     `DATABASE.md § List Comments for One Parent`;
  3. one resolving the sprint the task belongs to, through
     `DATABASE.md § Resolve the Sprint of Many Tasks (Grouped)` over the set that
     holds the one task id;
  4. only when that resolution finds a sprint, the sprint itself and its member
     tasks in `sprint_tasks` position order (see
     `DATABASE.md § List Sprint Tasks Ordered by Position`), the reads the Roadmap
     Sprint Page issues for its own sprint.

  That is at most five queries, and three for a task that belongs to no sprint.
  The position and the progress of the **Sprint card** are computed in memory over
  the member tasks already read. No query is issued per comment or per member
  task, so the number of queries does not grow with the number of comments, of
  member tasks, or of the roadmap's tasks. The page reads the comment bodies of
  this one task and of no other (see
  [Tasks and Sprints from SQLite](#tasks-and-sprints-from-sqlite)).
- **Path parameters.** `{name}` is validated against the roadmap-name rules
  exactly as on the other roadmap routes (the path-traversal guard in
  [Routes and Pages](#routes-and-pages) and
  [Security and Constraints](#security-and-constraints)); an invalid or
  nonexistent `{name}` returns HTTP `404 Not Found`. `{id}` MUST be a valid
  integer; a non-integer `{id}`, or an integer `{id}` that is not the `id` of a
  task belonging to the named roadmap, returns HTTP `404 Not Found`. The 404 for a
  task of another roadmap is what keeps a roadmap's data reachable only through its
  own path space. No route lies below this page's path (see
  [Routes and Pages](#routes-and-pages), rule 5).
- **Methods, cache, and failure.** The route serves `GET` and `HEAD` only; any
  other method is answered HTTP `405 Method Not Allowed`, exactly as on every other
  route (see [Functional Requirements](#functional-requirements), requirement 4).
  Every response of the route, its `404` included, carries
  `Cache-Control: no-store` (see [Cache Policy](#cache-policy)). A read that fails
  for a reason other than not-found is answered HTTP `500` and is logged once, at
  `ERROR`; the response carries no detail of the failure (see
  [What Is Logged](#what-is-logged)).

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
  the `Commit`, and the `Performed At` timestamp. The `Performed At` column
  displays the entry's `performed_at` in the display form `YYYY-MM-DD HH:mm:ss`,
  in UTC as stored, specified in [Date and Time Display](#date-and-time-display).
  The page does not redefine these fields; `MODELS.md` and `DATABASE.md` remain
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

### Document Title

Every HTML page carries exactly one `<title>` element, the document title a
browser shows on its tab and in its history. This section is canonical for it. The
document title is not the page header's title column, which
[Shared Page-Header Partial](#shared-page-header-partial) governs, and not the
sidebar brand, which [UI Framework](#ui-framework), rules 11 and 13, governs and
which this section leaves unchanged.

1. **Format.** The document title is the page's segments joined by the separator
   ` - `: one space, one ASCII hyphen-minus (`U+002D`), one space. The segments
   are, in order, the roadmap name, the area, and the hostname, except on the
   task page, whose segments are the task, the roadmap name, and the hostname
   (rule 7):

   | Page | Document title |
   |---|---|
   | Roadmap Index | `Roadmaps - <hostname>` |
   | Roadmap Sprints | `<roadmap> - Sprints - <hostname>` |
   | Roadmap Tasks | `<roadmap> - Tasks - <hostname>` |
   | Roadmap Audit Log | `<roadmap> - Audit - <hostname>` |
   | Roadmap Knowledge-Graph | `<roadmap> - Knowledge graph - <hostname>` |
   | Roadmap Sprint | `<roadmap> - Sprint #<id> - <hostname>` |
   | Roadmap Task | `#<id> <title> - <roadmap> - <hostname>` |

   `<roadmap>` is the validated roadmap segment of the request path, the same
   value that selected the database and that the top navbar shows (see
   [UI Framework](#ui-framework), rule 19). `<id>` is the sprint's `id`, or on the
   task page the task's `id`, in decimal, and `<title>` is the task's `title`.
   For example, sprint 49 of the roadmap `groadmap`, served on a machine whose
   hostname is `thinkpad`, has the document title
   `groadmap - Sprint #49 - thinkpad`, and task 2 of the roadmap
   `checkout-platform`, titled `Saved card tokenisation`, served on a machine whose
   hostname is `ROG`, has `#2 Saved card tokenisation - checkout-platform - ROG`.

2. **The roadmap index names no roadmap.** `/` belongs to no roadmap, so its
   document title has no roadmap segment: its first segment is the area,
   `Roadmaps`.

3. **No product name.** The document title carries no fixed text other than the
   area of rule 1, the `#` of a task reference, and the separator. In particular it MUST NOT contain the word
   `Groadmap`.

4. **The hostname is the serving machine's, read once.** `<hostname>` is the
   hostname of the machine running `rmp web`, as the operating system reports it
   through Go's `os.Hostname`. The server reads it once, at startup, before it
   serves any request (see [Server Lifecycle](#server-lifecycle), step 4), and
   every page of the session uses that one value. It does not depend on the HTTP
   request: the `Host` header and every other request field play no part in it.

5. **An unavailable hostname drops its segment.** When `os.Hostname` returns an
   error or an empty string, every document title omits the hostname segment
   together with the separator before it: the index page's title is `Roadmaps`
   and the tasks page's is `<roadmap> - Tasks`. This is not an error: the server
   starts and serves normally, and the exit behaviour is unchanged.

6. **Values are escaped.** The document title is rendered through
   `html/template` as text, so the roadmap name, the hostname, and a task's
   `title` are escaped like every other value the interface shows.

7. **The task page leads with the task.** A task's page is the one page whose
   document title names a record's `title`, and it names it first: its first
   segment is the task reference `#<id>`, one space, and the task's `title`, so a
   browser tab too narrow for the whole document title still shows which task it
   holds. The roadmap name and the hostname follow as on every other page. The
   `title` is inserted as stored and whole: the server neither truncates nor
   shortens it, and its length is bounded only by the limit `MODELS.md § Task`
   places on a task's `title`; how much of a long document title a tab shows is
   the browser's affair. A tab, a line feed, or a carriage return a `title` may
   hold (see `MODELS.md § Task`, Free-Text Control-Character Constraint) is
   written as stored and shows as a single space, because a browser strips and
   collapses ASCII whitespace in a document title. A `title` that itself contains
   ` - ` is written as stored; the task segment is the one that begins with
   `#<id>`. The sprint page keeps its own form, which names no record `title`.

### Shared Page-Header Partial

Every page's header title column is rendered by **one** partial, so the seven pages
cannot drift into seven conventions for saying the same kind of thing. The partial
renders the `<div class="col">` of the Tabler page-header row: an optional
pretitle, an optional status badge inside the pretitle, placed after the pretitle
text, the title, and an optional lead line. A page MUST NOT hand-write a
`page-pretitle` or a `page-title` element.
The document `<title>` is a separate element, specified in
[Document Title](#document-title).

1. **The title names the view, not the roadmap.** The roadmap is named twice in
   the shell already — in the sidebar's per-roadmap section label and in the top
   navbar (see [UI Framework](#ui-framework), rule 19) — so repeating it in the
   header title would state the same fact a third time on one screen while leaving
   the view the user is actually looking at unnamed on the sprints page. The
   titles are exactly:

   | Page | Pretitle | Title |
   |---|---|---|
   | Roadmap Index | — | `Roadmaps` |
   | Roadmap Sprints | — | `Sprints` |
   | Roadmap Tasks | — | `Tasks` |
   | Roadmap Audit Log | — | `Audit` |
   | Roadmap Knowledge-Graph | — | `Knowledge graph` |
   | Roadmap Sprint | `Sprint #<ID>`, followed by the sprint's status badge | the sprint's `title` |
   | Roadmap Task | `Task #<ID>`, followed by the task's status badge | the task's `title` |

2. **The sprint page and the task page are the two hierarchical headers.** They
   are the only pages that present an individual record rather than a view of the
   roadmap, so they alone carry a pretitle: `Sprint #<ID>` on the sprint page and
   `Task #<ID>` on the task page — the roadmap name is not repeated in either. The
   record's status badge, specified in
   [Roadmap Sprint Page](#roadmap-sprint-page) and
   [Roadmap Task Page](#roadmap-task-page) respectively, sits inside the pretitle,
   immediately after that text and separated from it by white space, so the
   pretitle reads `Sprint #<ID>` or `Task #<ID>` followed by the badge. The
   record's `title` stays the header title and holds the `title` alone, with no
   badge. Each record therefore remains identifiable by both its title and its
   id. No other page's header carries a badge.

3. **The lead line belongs to the roadmap index alone.** The index page's title is
   followed by a lead line naming the directory the roadmaps are discovered under.
   No other page carries one.

4. **The actions column stays with the page.** The partial covers the title column
   only. What a page puts in its actions column is genuinely page-specific markup —
   a `<select>`, a link — and folding those into the shared partial
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
   | Roadmap Tasks | none; the page's filter bar sits in the header of its task-list card (see [Roadmap Tasks Page](#roadmap-tasks-page), **The card header** and **Filter bar**) |
   | Roadmap Audit Log | none |
   | Roadmap Knowledge-Graph | the layout dropdown (see [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)) |
   | Roadmap Sprint | a link back to the roadmap's sprints page |
   | Roadmap Task | a link back to the roadmap's tasks page, labelled `Back to tasks` (see [Roadmap Task Page](#roadmap-task-page), **The way back**) |

   The back links of the sprint page and of the task page are **not** duplicated
   navigation: each returns to the parent view of the record being shown, which is
   a relationship the sidebar's flat view list does not express.

6. **Values are escaped.** The sprint `title`, the task `title`, and any other
   data-derived value reaching the partial is rendered through `html/template` as text, exactly as it
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
   [Roadmap Sprint Page](#roadmap-sprint-page)). The card shows no member tasks.

4. **Read-only.** The card renders data only. It contains no form, button, or link
   that submits a change; its only interaction is navigating to the sprint's own
   page.

5. **Description rendered as Markdown, in the non-interactive form.** Where the
   card renders the sprint's `description`, it renders the HTML the Markdown
   renderer produces from it (see [Markdown Rendering](#markdown-rendering)), and
   it uses the renderer's **non-interactive form**
   ([Markdown Rendering](#markdown-rendering), rule 14). The whole card is one
   link (rule 3), and HTML admits no interactive element — a link or a form
   control — inside a link, so the card's rendered description carries none: a
   Markdown link renders as its text, and a task-list item's checkbox renders as a
   text marker. The card shows the whole rendered description; it neither clamps
   nor truncates it, and the content wraps within the card.

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
   - the **Sprint details card**, which carries the sprint's `description`
     (rule 6) and the **sprint metadata datagrid**. The datagrid holds exactly
     three fields, in this order: `Created` (`created_at`), `Started`
     (`started_at`), and `Closed` (`closed_at`). A `started_at` or `closed_at`
     that is unset renders a neutral placeholder, an em dash, rather than an empty
     value. A timestamp that is set is displayed in the display form of
     [Date and Time Display](#date-and-time-display). The fields are defined for
     the `Sprint` model in `MODELS.md § Sprint` and are not redefined here.

     The datagrid carries no `ID`, `Title`, `Status`, `Order`, `Capacity`, or
     `Tasks` field. The page header already presents the sprint `title`,
     `Sprint #<ID>`, and the sprint's status badge (see
     [Roadmap Sprint Page](#roadmap-sprint-page)), and the member-tasks board's
     three column counts together give the sprint's total task count, so a
     datagrid field for any of them would repeat what the page already shows. The
     sprint's execution `order` and its capacity (`max_tasks`) are not shown on
     the page. The datagrid is rendered only by this sub-template, and so only on
     the Roadmap Sprint Page;
   - the **member-tasks board**, a Kanban board of three fixed columns holding the
     sprint's tasks, one card per task (defined below). The board is the sprint's
     member-task presentation and sits between the two cards that surround it:
     directly below the Sprint details card and directly above the Comments
     card;
   - the **Comments card**, a separate card placed after the member-tasks board and
     rendered last in the sub-template (defined below).

3. **Member-tasks board.** The sprint's member tasks are presented as a Kanban
   board of three fixed columns, one card per task. The **GitLab issue board** is
   the acknowledged model for this presentation: columns that stand for states of
   the work, cards that stand for work items, a count on each column header, and
   counters at the trailing edge of a card. The model is
   structural and never acts on the data: the board's one control beyond the
   card, the column collapse toggle, changes only how the board is presented
   (see **Column collapse** and **Read-only** below).

   - **Three fixed columns, in this order.** From left to right the columns are
     `WAITING`, `DOING`, and `CLOSED`, and each holds the sprint's tasks in the
     statuses named here (the task status enum is defined in `MODELS.md § Enums`):

     | Column | Holds the sprint's tasks whose status is |
     |---|---|
     | `WAITING` | `BACKLOG` or `SPRINT` |
     | `DOING` | `DOING` or `TESTING` |
     | `CLOSED` | `COMPLETED` |

     The grouping is the categorisation `models.CalculateSprintShowResult` already
     produces — pending = `BACKLOG` + `SPRINT` (its `Summary.Pending` counter),
     in progress = `DOING` + `TESTING` (its `Summary.InProgress` counter), and
     completed = `COMPLETED` (its `Summary.Completed` counter). The board defines no
     new categorisation; it reuses that one, so every presentation of one sprint's
     task progress agrees about which tasks are waiting, which are being worked on,
     and which are done.

     Each column heading is written exactly as spelled above, in upper case, and is
     not translated.
   - **The column counts partition the sprint.** Because the grouping is that one,
     the `WAITING` column's count is the number of the sprint's member tasks in
     `BACKLOG` or `SPRINT` (`Summary.Pending`), the `DOING` column's count is the
     number in `DOING` or `TESTING` (`Summary.InProgress`), the `CLOSED` column's
     count is the number in `COMPLETED` (`Summary.Completed`), and the three counts
     sum to the sprint's total number of member tasks (`Summary.TotalTasks`). Only
     the sprint's own member tasks are counted. That identity is what makes a fourth or
     "other" column unnecessary rather than merely unwanted: the task status enum is
     closed (`MODELS.md § Enums`) and `tasks.status` is restricted by a CHECK
     constraint to exactly its five values (`DATABASE.md § tasks Table`), so every
     member task carries one of those five, each of the five is claimed by exactly
     one column, and no task of the sprint can fall outside the board.
   - **Every column is always rendered.** All three columns are present, in that
     order, whatever the sprint holds; the page never drops or hides a column, and
     neither the set of columns nor their order depends on the data. A collapsed
     column — whether the page started it collapsed or the reader collapsed it —
     stays on the board, in its place, with its heading and its count badge
     visible; only its cards, or its empty state, are hidden (see
     **Column collapse** below). In the served HTML, a column holding no task
     renders a clear, unobtrusive in-column empty state, in Tabler's empty-state
     markup, inside the column, below the column header, in place of the card list,
     with the column, its heading, and its `0` count badge still visible. Once the page's scripts have initialised, such a column starts
     collapsed when another column of the sprint holds a task, and its empty state
     is displayed when the reader expands it (see **Column collapse** below). A
     sprint with no member task is therefore shown as an empty board, with all three
     columns expanded and all three empty states displayed, rather than as an absent
     one, and the sub-template puts no page-level empty state in place of the board.
   - **Column header.** Each column header shows the column heading together with a
     Tabler badge carrying that column's task count, the way a GitLab issue board
     shows the issue count of each list. A column holding no task shows the count
     `0`. The badge is a hybrid: its **text** is the number
     of member tasks in the column, and its **colour** is the semantic colour of the
     status the column groups, taken from the task status table (see
     [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
     rule 2). No new colour and no new band is introduced here. At the header's
     trailing edge, after the heading and its badge, sits the column's collapse
     toggle (see **Column collapse** below).

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
   - **The card.** Each card presents one member task on two lines, in this
     order:
     1. the task **`title`**, leading the card as its first line and presented as
        the card's prominent main content;
     2. **one line carrying both of the card's remaining groups**: the task's four
        badges at the **leading edge** of that line, and the task's two counters at
        its **trailing edge**.

        The **badge line** leads: the task's **id badge**, **`severity` badge**,
        **`priority` badge**, and **type badge**, in that order — a task 42 of
        severity `3`, priority `5`, and type `IMPROVEMENT` shows `#42`, `S3`, `P5`,
        and `IMPROVEMENT`. The badges occupy the place the GitLab card gives its
        labels. The four badges are these:
        - the **id badge**, whose text is the task reference `#<id>` — the task's
          `id` written with its leading `#`. It carries the Tabler classes
          `bg-black` and `text-white` for every task: a black background
          (`#000000`) with white text (`#ffffff`), a contrast ratio of 21:1. Both
          classes are shipped by the vendored `tabler.min.css`, which defines
          `bg-black` as a background colour mixed from `var(--tblr-black)` and
          `text-white` as a text colour mixed from `var(--tblr-white)`, each at full
          opacity, with `--tblr-black` set to `#000` and `--tblr-white` set to
          `#fff`. The colour is fixed and value-independent: an id identifies a task
          and carries no meaning a colour could state, so no colour mapping governs
          this badge (see
          [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
          rule 2);
        - the **`severity` badge**, whose text is the badge label `S` immediately
          followed by the task's integer `severity`;
        - the **`priority` badge**, whose text is the badge label `P` immediately
          followed by the task's integer `priority`;
        - the **type badge**, whose text is the task's `type` exactly as the
          `TaskType` enum spells it (see `MODELS.md § Enums`) — for example
          `IMPROVEMENT` — and whose colour is the variant the task type table
          assigns to that value in
          [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours).

        The badge label is one letter, and no colon, no space, and no other
        separator stands between it and the digits. A task of severity `3` and
        priority `5` therefore shows `S3` and `P5`, the severity badge before the
        priority badge. The severity and priority badges are each coloured by the
        band their value falls in, using exactly the mapping in
        [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours).
        No new badge colour and no new band is introduced here.

        **The severity and priority badges each name the value they carry with a
        badge label.** The label stands for the word the card has no room to write
        out in full. Without it the card would put two bare integers side by side
        and state nowhere which one is the severity and which one is the priority.
        The label is the initial letter of the field's name — `S` for `severity`,
        `P` for `priority` — which is the fewest characters that tell the two
        fields apart. **The badge label is a label, not a value.** It changes what
        the badge reads and nothing else: the colour still follows the value alone,
        through exactly the band mapping named above, so `P5` takes the colour that
        mapping assigns to the priority `5`, and the label selects no colour,
        introduces no band, and changes no meaning. The card's accessible name
        carries neither value, and so it carries no badge label (see **The card is
        a link to the task page** below). The type badge takes no badge label,
        because it reads `IMPROVEMENT`, not a bare integer, and the leading `#` of
        the id badge is not a badge label in this sense: `#<id>` is the form in
        which this interface writes a task reference everywhere.

        The same four badges, with the same texts, badge labels, and colours, are
        the id, priority, severity, and type cells of the tasks page's list (see
        [Roadmap Tasks Page](#roadmap-tasks-page), **Row content**), so a reader
        meets a task's reference, severity, priority, and type in one form on both
        pages.

        The **counters** close the same line at its trailing edge, which is where
        the GitLab card puts its counters. They are the task's number of comments
        and its number of subtasks (`subtask_count`), **in that order: the comment
        count first, then the subtask count**. Each is rendered as an icon followed
        by its number — `ti ti-message` for the comment count and `ti ti-subtask`
        for the subtask count. Both counters are
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
     is large — the line **wraps** inside the same card: the trailing group moves
     below the leading one, and a badge of the leading group that does not fit moves
     below the badges before it, keeping the order stated above. It never overflows the card's edge, and it never makes
     the card, the column, or the page scroll horizontally (see
     [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
     rules 2 and 10). The rule is stated as behaviour because that is what a reader
     and a test can observe; the card carries no inline `style` attribute and every
     class it emits is defined either in the vendored Tabler distribution or in the
     project override stylesheet `static/style.css` (see
     [UI Framework](#ui-framework), rules 8 and 10, and **Markup** below).

     The card carries **no status badge**: the column the card sits in already
     states the task's status.

     The card shows those seven data points — the title, the id, the severity, the
     priority, the type, the comment count, and the subtask count — and no
     others: no dependency counts. It presents a subset of the task's fields by design,
     because a card is read at a glance and a column of cards is
     read as a whole; every field of the `Task` model is shown on the task page the
     card links to (see [Roadmap Task Page](#roadmap-task-page)). The card
     does not redefine any field; `MODELS.md` and `DATABASE.md` remain canonical.

     **Both counters are always rendered.** The comment count and the subtask count
     are present on every card of this board, including when either or both are `0`:
     a task with no comment shows the comment icon followed by `0`, a task with no
     subtask shows the subtask icon followed by `0`, and the trailing edge of the
     second line therefore carries both numbers on every card the board renders.

     The card carries exactly two indicators and both of them are counts, so
     rendering both always makes every card of the board the same shape and makes
     each number meaningful: a `0` states that the task has no comment, where an
     absent counter leaves the reader unable to tell "no comments" from "this card
     does not show comments".
   - **The card is a link to the task page.** Each card is a link to its task's
     own page, `/roadmaps/{name}/tasks/{id}` (see
     [Roadmap Task Page](#roadmap-task-page)), and the card itself is the link:
     one `<a>` element carrying the Tabler classes `card` and `card-link` and an
     `href` to that page, the idiom the sprint card of the Roadmap Sprints Page
     already uses (see [Shared Sprint-Card Partial](#shared-sprint-card-partial),
     rule 3). A pointer click, a touch tap, and Enter therefore all follow it through the browser's
     own activation behaviour, with no added JavaScript, and the browser's own link
     behaviours — opening in a new tab or window, copying the address — apply to
     it. The card carries no `tabindex` and no `role`: both would be redundant on
     a link, and a non-interactive element made to announce itself as a control
     MUST NOT stand in for it. The card's accessible name is
     `Open details for task #<id>: <title>`, carried by its `aria-label`, naming the
     action and identifying the task by `id` and `title`. The `title` is required in it, not
     optional: the card's visible label is the task title, and an accessible name
     that omitted it would leave the link impossible to follow by speech input,
     which is what WCAG 2.5.3 Label in Name (Level A) forbids. The card shows a
     visible focus indicator whenever it receives keyboard focus (WCAG 2.2 Success
     Criterion 2.4.7, Focus Visible); where the vendored distribution gives a
     `card-link` none, the project override stylesheet sets it on the card's
     `:focus-visible` state.

     The card can hold that contract whole, which a table row cannot. A row is not
     an activatable element and can hold no single link that wraps it, so a
     tabular presentation has to push the link down into one cell and leave the
     row itself clickable by pointer alone — two targets for one task. A card is a
     single element and can **be** the link, so pointer, touch, and keyboard
     reach the same target. No `<tr>` on this page is a link to a task or carries
     one.
   - **Following a card costs the board nothing.** Following a card is an
     ordinary navigation to the task's server-rendered page. The board fetches
     nothing when a card is followed, so it adds no query to the page's own read
     and no per-card cost.
   - **Height and scrolling.** The board is **height-limited**: it takes a definite,
     bounded height rather than growing with the number of member tasks, and each
     column scrolls **vertically and independently** inside that height when its
     cards exceed it, as a GitLab issue board's lists do. When the three columns do
     not fit the viewport's width, the **column strip** scrolls horizontally inside
     its own container, and the page itself
     never scrolls horizontally (see
     [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
     rules 2 and 10).

     The board is deliberately **not** a full-height page region. The sprint page
     is not a single-region page:
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
     column holds: the width follows the viewport, never the data. This division
     applies to the **expanded** columns. A collapsed column is a strip of fixed
     width and takes no share, and the columns that remain expanded divide the
     width it frees equally among themselves on the same terms (see **Column
     collapse** below). The served HTML renders all three columns expanded; once
     the page's scripts have initialised, the board divides its width among the
     columns that start expanded — the columns that hold a task, or all three when
     the sprint holds no member task — and each column that starts collapsed is a
     strip.

     An expanded column is never narrower than **17rem**, the width at which its
     cards stay legible. When the expanded columns at that minimum, plus any
     collapsed strip and the gaps between the columns, do not fit the viewport, the
     expanded columns keep the minimum and the column strip scrolls
     horizontally inside its own container exactly as it does when the board is
     wider than the viewport for any other reason, while `<body>` still produces no
     horizontal overflow (see **Height and scrolling** above and
     [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
     rules 2 and 10).

     The columns are separated by a **0.75rem** gap, and the body of a card carries
     **0.75rem** of padding on all four sides, in place of the `1rem` the vendored
     Tabler distribution gives a small card's body. The card's body holds running
     text inside a measure the column has already narrowed, so padding taken off
     the body is width returned to that text and height returned to the card; the
     hit target is unaffected, because what the user presses is the whole card. The
     padding is an override of a vendored component's own spacing, declared in the
     project override stylesheet (see [UI Framework](#ui-framework), rule 10); it
     changes no class the board emits and no markup. Three columns dividing the
     viewport are wide rather than narrow, and the board is read as one sprint at a
     glance, which is what makes filling the width the right shape for it.

     Every one of these lengths is expressed in `rem`, so the minimum column and the
     card scale with the reader's own text size.

     On a narrow viewport the board stays usable: each
     expanded column keeps the minimum width above, at which its cards remain
     legible, the horizontal strip scroll is reachable by a touch gesture, the cards
     and the column collapse toggles present touch-friendly hit targets, and the
     task page a card links to is usable at the same viewport (see
     [Roadmap Task Page](#roadmap-task-page)).
   - **Column collapse.** Each of the three columns can be collapsed by the
     reader and expanded again, and the page chooses each column's initial state
     from the tasks the column holds. The control is presentation only: it changes
     how the board is shown and nothing the board shows.
     - **Initial state.** Once the page's scripts have initialised, each column
       starts in the state this rule assigns:
       - when the sprint's board holds at least one member task, in any column,
         each column holding no task starts **collapsed** and each column holding
         one or more tasks starts **expanded**;
       - when the sprint holds no member task, so that every column is empty, all
         three columns start **expanded**.

       A column that starts collapsed is in exactly the state that activating its
       toggle produces (see **Collapsing** below). An empty column is started
       collapsed because it has nothing to show beside columns that do, while a
       board that is empty everywhere keeps its three empty states in view, so that
       it reads as an empty sprint rather than as three bare strips.
     - **Task count on the column.** The column element — the card carrying
       `data-role="task-board-column"` — carries the attribute
       `data-task-count="<N>"`, where `<N>` is the number of the sprint's member
       tasks in that column, written in ASCII decimal digits with no sign and no
       leading zero other than the single digit `0`. It is the same number as the
       text of the column's count badge (see **Column header** above), so the three
       values sum to the sprint's total number of member tasks. The attribute is
       the only input the initial state is derived from.
     - **Not persisted.** The collapsed or expanded state is held by the page
       alone: it is carried in no URL parameter, no cookie, and no browser storage
       (`localStorage`, `sessionStorage`, or IndexedDB). Each page load derives the
       initial state again from the tasks alone, so reloading the page, or
       navigating to it again, presents the state the rule above assigns whatever
       state the reader left the columns in.
     - **The toggle.** Each column header carries exactly one toggle, at its
       trailing (right-hand) edge, inside Tabler's card-header actions container,
       `<div class="card-actions">`. The toggle is a
       `<button type="button" class="btn-action">`, Tabler's card-header action
       button, carrying `data-role="task-board-column-toggle"` and holding one
       Tabler chevron icon: `<i class="ti ti-chevron-left" aria-hidden="true"></i>`
       while its column is expanded and `<i class="ti ti-chevron-right"
       aria-hidden="true"></i>` while it is collapsed, so the chevron points a
       different way in each state. Being a `<button>`, the toggle is activated by
       a pointer click, a touch tap, Enter, and Space alike, through the browser's
       own activation behaviour; it carries no `tabindex` and no `role`. It
       presents a touch-friendly hit target, and it shows a visible focus indicator
       whenever it receives keyboard focus (WCAG 2.2 Success Criterion 2.4.7, Focus
       Visible). The vendored distribution's `btn-action` rules remove the focus
       outline only while the focus does not match `:focus-visible`; the toggle's
       `:focus-visible` state carries a visible indicator, set in the project
       override stylesheet.
     - **Accessible state and name.** The toggle carries `aria-expanded="true"`
       while its column is expanded and `aria-expanded="false"` while it is
       collapsed, and an `aria-controls` attribute holding the `id` of its column's
       body — the element that holds the column's cards or its empty state. The
       three bodies carry the ids `sprint-board-column-waiting`,
       `sprint-board-column-doing`, and `sprint-board-column-closed`, which are
       unique within the page. The toggle's accessible name is carried by
       `aria-label` and names the action the toggle will perform and the column it
       acts on: `Collapse <HEADING> column` while the column is expanded and
       `Expand <HEADING> column` while it is collapsed, where `<HEADING>` is the
       column heading exactly as written above — for example
       `Collapse WAITING column` and `Expand WAITING column`. The chevron icon is
       decorative and is hidden from assistive technology.
     - **Collapsing.** Activating the toggle of an expanded column collapses it:
       - the column body carries the `hidden` attribute, so the column's cards, or
         its empty state, are neither displayed nor exposed to assistive
         technology;
       - the column element — the card carrying `data-role="task-board-column"` —
         carries the modifier class `task-board__column--collapsed`;
       - the column shrinks to a vertical strip **3rem** wide that keeps the board's
         height. The strip is the column's own Tabler card with its header, and it
         shows the toggle at its top, followed by the column heading and its count badge rotated 90
         degrees clockwise, so that they read from top to bottom (the vertical
         writing mode `vertical-rl`). The count badge keeps its text and its colour,
         so a collapsed column still states how many tasks it holds;
       - the toggle's `aria-expanded` becomes `false`, its `aria-label` becomes
         `Expand <HEADING> column`, and its icon becomes `ti ti-chevron-right`.
     - **Expanding.** Activating the toggle of a collapsed column restores the
       column to its expanded form: the body loses the `hidden` attribute, the
       column loses `task-board__column--collapsed`, the heading and its badge read
       horizontally again, the column takes its share of the board's width again
       (see **Height and scrolling** above), its cards, or its empty state, are shown
       unchanged and in the order the page rendered them, and the toggle
       returns to `aria-expanded="true"`, the name `Collapse <HEADING> column`, and
       the icon `ti ti-chevron-left`. Keyboard focus stays on the toggle through
       both transitions. A column that started collapsed expands in exactly this
       way, and then shows its empty state.
     - **Independence.** Each toggle acts on its own column alone, and the three
       columns collapse and expand independently, whatever state each started in.
       Every combination is allowed,
       including all three collapsed, in which case the board shows three strips,
       keeps its height, and leaves the rest of its width empty. The columns keep
       their left-to-right order in every combination.
     - **Presentation only.** Collapsing or expanding a column issues no request,
       reads nothing, and writes nothing. It changes no card, no count, no column
       order, and no card order, and it navigates nowhere. A card of a collapsed column cannot
       be reached while its column is collapsed, and is reachable again, unchanged,
       once the column is expanded.
     - **The script.** The behaviour is carried by one embedded client script,
       `static/sprint-board.js`, which the sprint page loads from `/static/` like
       every other client script (see
       [Embedded Asset Categories](#embedded-asset-categories) and
       [Frontend Rules](#frontend-rules), rules 2 and 5). No inline script and no
       inline event-handler attribute is introduced, and the
       Content-Security-Policy in [Security Headers](#security-headers) is
       unchanged. The script changes a column's state only by setting or removing
       the body's `hidden` attribute, the column's modifier class, and the toggle's
       `aria-expanded`, `aria-label`, and icon class; it writes no `style` attribute
       and no inline style property. The strip's width, its layout, and the rotated
       heading are declared in the project override stylesheet `static/style.css`
       under the modifier class (see [UI Framework](#ui-framework), rules 8 and 10).
     - **Initialisation.** When the script initialises, it performs these steps in
       this order:
       1. it removes the `hidden` attribute from every toggle (see **Without
          JavaScript** below);
       2. it reads each column element's `data-task-count`. A value that is not a
          string of one or more ASCII decimal digits cannot be read as a
          non-negative integer, and its column is left expanded;
       3. when the sum of the values read in step 2 is greater than `0`, it puts
          each column whose value reads as `0` into the collapsed state, through
          the same state change that activating that column's toggle performs
          (see **Collapsing** above); when that sum is `0`, it changes no column,
          so all three stay expanded.

       Initialisation issues no request, reads nothing but the served markup, and
       moves no keyboard focus.
     - **Without JavaScript.** The served HTML renders every column expanded and
       every toggle in its expanded state — `aria-expanded="true"`, the
       `aria-controls` reference, the name `Collapse <HEADING> column`, and the icon
       `ti ti-chevron-left` — and it renders each toggle with the `hidden`
       attribute, which the script removes from every toggle when it initialises.
       The served state is the same for every sprint, whatever its tasks; only the
       script applies the initial state. A browser that runs no script therefore
       shows the board with all three columns expanded and every card and every
       empty state visible, and shows no toggle that would do nothing when
       activated.
     - **This board only.** The collapse toggle belongs to this board. The roadmap
       tasks page carries no board, no collapse toggle, and no `data-task-count`
       attribute, and it does not load `static/sprint-board.js` (see
       [Roadmap Tasks Page](#roadmap-tasks-page)).
   - **Read cost: one grouped comment count, and nothing per card.** The card shows
     a comment count, so the page reads one. That count is read with **one grouped
     query** over the sprint's member tasks, selected by the sprint id with a
     sub-select on `sprint_tasks` rather than by a list of the member ids, so the
     query binds one parameter whatever the number of members (see
     `DATABASE.md § Count Comments for Many Parents (Grouped)`, the member tasks of
     one sprint) — never one query per card, and never a comment **body**: the card displays a number, and reading
     the text of every comment of every member task in order to display a number
     would be work the page throws away. A member task's comment text is read only
     by that task's own page, one task at a time (see
     [Roadmap Task Page](#roadmap-task-page)). When the sprint has no member task the page issues no such
     query at all, because the member-task read has already shown that there is no
     card to count for.

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
     status, or creates or edits anything. It contains no form and no write path.
     Its cards are links to read-only task pages, and it carries one kind of
     button, the column collapse toggle, which changes only the board's
     presentation (see **Column collapse** above); neither submits anything or
     changes any data. This is a deliberate divergence from the GitLab issue board
     the layout is modelled on: the inspiration is structural — columns per state,
     cards, per-column counts — and never acts on the data, and the
     `rmp` CLI remains the sole write path for every task (see
     [Security and Constraints](#security-and-constraints)).
   - **Markup.** The board introduces no exception to the markup rules already in
     force: no template carries an inline `style` attribute, and every class the
     board emits is defined either in the vendored Tabler distribution or in the
     project override stylesheet `static/style.css` (see
     [UI Framework](#ui-framework), rules 8 and 10). Where Tabler provides the
     component, the board uses Tabler's markup — Tabler cards for the task cards,
     the card-header idiom for the column headers, Tabler badges for the counts and
     for the four badges of the badge line, and Tabler's empty-state markup for an
     empty column. The vendored Tabler distribution ships no board or Kanban
     component, so the column strip's own layout, height, and scrolling rules live
     in `static/style.css`, which is the specified home for project styling no
     Tabler class covers. The column collapse toggle uses Tabler's card-header
     actions idiom (`card-actions` holding a `btn-action` button) and a Tabler
     Icons chevron, and the collapsed strip's rules live in `static/style.css`
     under the modifier class `task-board__column--collapsed` (see **Column
     collapse** above).

4. **Comments card.** The last card of the sub-template presents the sprint's own
   comments — the sprint's progression log. The fields of a comment are defined for
   the `SprintComment` model in `MODELS.md § Sprint Comment`; the sub-template does
   not redefine them.
   - **Scope.** The card shows the comments of the sprint itself. It does not show,
     aggregate, or merge in the comments of the sprint's member tasks; those are
     shown on each task's own page (see
     [Roadmap Task Page](#roadmap-task-page)).
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
     rule 2, **The discriminating test**). At the header's trailing edge, inside
     Tabler's card-header actions container `<div class="card-actions">`, the
     header states the order of the log in the secondary text colour
     (`text-secondary`) with exactly the words `Oldest first`, as the task page's
     Comments card does. The text is shown whether or not the sprint has comments;
     it is text, not a control.
   - **What each entry shows.** For one comment, in order: its `type` as a badge,
     its `created_at` timestamp, its `updated_at` timestamp when that value is not
     null (marking the entry as edited), and its `body`. Both timestamps are
     displayed as specified in [Date and Time Display](#date-and-time-display).
   - **Markup.** The card body holds Tabler's Timeline component with the same
     structure the task page's Comments card uses: `<ul class="timeline">` with
     `<li class="timeline-event">` items, each an icon
     (`<i class="ti ti-message"></i>`) in `timeline-event-icon` and a
     `card timeline-event-card` holding the entry. The type badge uses the neutral
     `bg-secondary-lt` variant for every type value, exactly as on the task page,
     and introduces no per-type colour.
   - **Body rendered as Markdown.** A comment body is authored as Markdown, and the
     card renders it as the HTML the Markdown renderer produces from it; the
     rendered content wraps within the card (see
     [Markdown Rendering](#markdown-rendering)).
   - **Empty state.** When the sprint has no comments, the card shows a clear
     empty-state message in place of the timeline, in the same idiom a column of the
     member-tasks board uses when it holds no task. The card itself is always
     present.
   - **Read-only.** The card renders data only: no form, no edit control, and no
     submit action, and no input other than the disabled checkbox of a rendered
     Markdown task-list item, which can be neither checked nor unchecked (see
     [Markdown Rendering](#markdown-rendering), rule 3).

5. **Read-only.** The sub-template renders data only. It contains no form, button,
   or link that submits a change. Its interactions are two, and neither changes
   any data: following a board card to that task's read-only page, and
   collapsing or expanding a board column with that column's toggle, which
   changes only the board's presentation.

6. **Markdown fields.** Wherever the sub-template renders the sprint's
   `description` or a sprint comment's `body`, it renders the HTML the Markdown
   renderer produces from it, as specified in
   [Markdown Rendering](#markdown-rendering).

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
- **Empty graph, and the roadmap that has none.** A roadmap whose graph is served
  but holds no elements renders successfully and shows an empty-graph state; the
  default query writes nothing, so the request changes nothing on disk. **A
  roadmap with no server running is a different case and is not an empty graph.**
  The page renders, and its data request is answered HTTP `503`, because this
  interface cannot read a graph no server is serving and will not open the store
  to find out whether one exists (see
  [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
  rule 1). The two states are worth telling apart in the page's own words, since
  the remedy for the second is to start a server and there is no remedy for the
  first.

### Graph Query Bar

The query bar is a control rendered at the top of the knowledge-graph page, above
the graph card (see [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)).
It lets the user drive the visualisation from a single editable Cypher statement
instead of a fixed full-graph read. The query bar drives, and re-renders from, the
same graph data endpoint the page already consumes; it adds no new endpoint.

**The statement is executed as written.** The endpoint does not examine what it
does, so a statement typed into the query bar may create, change, or delete graph
data and may change the graph's schema, exactly as the same statement would under
`rmp graph client`. Nothing in the page or the server prevents that, and nothing
authenticates the request (see
[Security and Constraints](#security-and-constraints)). The one statement the
endpoint refuses is one carrying an `EXPLAIN` or `PROFILE` prefix, which it
recognises through the engine's own parser and refuses without sending, because
the graph this page draws has no place for a query plan; the refusal concerns the
answer a statement asks for, not what it does, and it withdraws no write (see
[Query-Bar Error Handling](#query-bar-error-handling), rule 12).

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
   what the statement it submits does is not examined, so a `CREATE`, a `SET`, a
   `DETACH DELETE`, or a `DROP CONSTRAINT` typed into the box is executed and
   committed by the graph server, exactly as one sent by `rmp graph client` is
   (see [Graph Data Endpoint](#graph-data-endpoint),
   [Security and Constraints](#security-and-constraints), and
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)).
   The page shows no confirmation and asks for no credential before running one.

8. **Error surfacing.** When a search fails — because the limit is invalid,
   because the statement carries an `EXPLAIN` or `PROFILE` prefix, or because the
   statement fails to execute, which includes exhausting the endpoint's query time
   budget (see [Graph Query Time Budget](#graph-query-time-budget)) — the page
   shows a clear message in place and does not crash, exactly as the layout
   degradation does; every case is specified in
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

A search driven by the query bar can fail for three distinct reasons: an invalid
limit (case 1), a statement that fails to execute (case 2), and a statement
carrying an `EXPLAIN` or `PROFILE` prefix, which the endpoint refuses before it
sends anything (rule 12). The page MUST surface each clearly and in place without
crashing, consistent with the graceful layout degradation already specified (see
[Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library),
rule 5). The three are kept distinct so the user understands what to fix.

The endpoint answers each with HTTP `400 Bad Request` and a JSON body that names
the failure's class in a `kind` field, in the shape specified in
`DATA_FORMATS.md § Graph View Data`, **Error Shape**. Rules 3 to 7 below fix the
status, the order in which the three are decided, the boundary against the `500`
of an internal read error, and what the body carries.

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
   [Graph Query Time Budget](#graph-query-time-budget). A statement sent to a
   graph server that then stopped answering is an execution failure of this same
   case too, for the reason rule 10 gives. The endpoint answers HTTP
   `400 Bad Request` with `kind` `execution` in every case of this rule.

3. **In-place, non-fatal.** In every case the message is shown in place on the
   page, the page does not crash, and the failure triggers no navigation, exactly
   as the layout-degradation message does. The user can edit the statement or
   change the limit and search again. The graph already shown is left in place.

4. **One status, three kinds — and this rule is the one place the set is
   enumerated.** The endpoint's `kind` takes exactly these three values and no
   others: `invalid_limit`, `plan_prefix`, and `execution`. Every other statement
   of the set in this specification refers here rather than repeating it, so the
   count and the list cannot drift apart across sections; a value is added or
   removed here first. Every failure carries HTTP `400 Bad Request`, and the
   body's `kind` field is what distinguishes them. One status fits all three
   because in each of them the server is able to serve the route and refuses the
   request the caller made: the `limit` falls outside the closed set the endpoint
   publishes, the statement asks for a query plan the endpoint's response cannot
   carry, or the statement the caller wrote cannot be executed. RFC 9110,
   Section 15.5.1, defines `400` as
   the status for a request the server "cannot or will not process ... due to
   something that is perceived to be a client error", and RFC 9110, Section 15.5,
   puts the explanation of the error in the response representation, which is
   exactly what the `kind` and `error` fields are. Splitting the three across
   different statuses would assert a distinction HTTP does not carry, while the
   body already carries it precisely.

   A statement cancelled for exhausting the time budget carries this same `400`
   and this same `execution` kind. It is neither a `503` nor a `504`. RFC 9110,
   Section 15.6.4, defines `503` as a temporary overload or scheduled maintenance
   "which will likely be alleviated after some delay": nothing is unavailable
   here, the statement reached a graph server and ran on it, that server keeps
   serving every other request, and delay alleviates nothing, because the same
   statement over the same store exhausts the same budget again. **This endpoint
   does publish `503`, and the contrast is exactly the point**: it publishes it
   when there is no graph server to reach at all
   ([Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
   rule 1), which is a dependency an operator starts and which delay does clear.
   A statement the budget cut had its dependency, used it, and asked for more work
   than one request is given. RFC 9110, Section 15.6.5, defines `504` for a
   server "acting as a gateway or proxy" that did not receive a timely response
   "from an upstream server": this server is neither, and the engine it runs the
   statement on is in-process, not an upstream server. What is true of a budget
   exhaustion is that the caller asked this endpoint for more work than it spends
   on one request, and that the caller changes the outcome by writing a cheaper
   statement. `400` states that; the two 5xx codes state something else that is not
   the case here.

   **A request whose serialisation retry is exhausted carries this same `400` and
   this same `execution` kind, and the objection that invites is answered here
   rather than left standing.** Contention against a healthy store is not
   obviously "something that is perceived to be a client error", so the
   alternatives are weighed rather than dismissed, and each of the three states
   something that is not the case. RFC 9110, Section 15.5.10, defines `409` for a
   request that "could not be completed due to a conflict with the current state
   of the target resource", in situations "where the user might be able to resolve
   the conflict and resubmit the request": by the time the endpoint answers, the
   winning transaction has committed and the losing one has rolled back whole
   (see `GRAPH.md § Concurrency Inside the Server`), so no conflict of state
   survives for the user to resolve — and `409` is a 4xx in any case, so it does
   not answer the objection it would be reached for. `503` announces that the
   service is unavailable: the graph server was reached, ran the statement, went
   on serving every other request while it did, and would have served the same
   statement against a different node, so the announcement would be false. It is
   the same test this endpoint's own `503` passes and this condition fails —
   there, no server could be reached at all; here, one was. `503` would also
   invite a `Retry-After` this endpoint cannot compute, because nothing in the
   product knows when the contending writer will stop. `504` is refused for the reason it is refused above: this server is
   not a gateway or a proxy. What is true of an exhausted retry is what rule 6
   fixes — the failure surfaced once the statement was running — and that is the
   boundary this endpoint classifies by. The distinction the caller needs is
   carried in the body, by an `error` that names the contention (rule 11), and not
   by a status that would have to assert something else in order to carry it.

5. **The `limit` is resolved first and the prefix is examined second, so no two
   kinds can both apply.** One request can carry an invalid `limit` together with
   a prefixed statement, or together with an unexecutable one. The endpoint
   resolves the `limit` first and refuses the request there, so a request whose
   `limit` is invalid is answered `invalid_limit` whatever its statement is, a
   prefixed statement included, and its statement is neither examined for a
   prefix nor executed. Only a request whose `limit` was accepted has its
   statement examined for a prefix (rule 12), and a statement refused there as
   `plan_prefix` is never sent, so it cannot also fail to execute. There is no
   further precedence question: `execution` is reached only by a request whose
   `limit` was accepted and whose statement the engine's parser reports as
   carrying no prefix.

6. **The boundary against the internal read error is drawn at when the failure
   surfaces, not at what the failure is.** This endpoint answers an internal read
   error with `500`, exactly as every other route does (see
   [Routes and Pages](#routes-and-pages) and
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
   rule 5). What separates that `500` from the `400` of case 2 is the moment the
   failure surfaces: a failure to reach a graph server at all is answered with a
   5xx and never with this `400` — `503` when no server is listening or none can
   be reached through a socket that answered, and `500` when the roadmap's socket
   path is over the platform's bound and no server can ever listen there (see
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
   rule 1) —
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

7. **The response body.** Each failure carries a JSON body of exactly two string
   fields, `error` and `kind`, in the shape specified in
   `DATA_FORMATS.md § Graph View Data`, **Error Shape**, which is canonical for it.
   `kind` is the machine-readable class; rule 4 above enumerates its value set and
   is canonical for it. `error` is the human-readable reason the page shows in
   place. The `error` of an execution failure carries the engine's own diagnostic
   text, so the user reads for a given statement the same diagnostic the CLI prints
   for it (see `GRAPH.md § Error Handling and Exit Codes`, rule 2) and can act on
   it; the `error` of an invalid limit names the rejected value, which is what
   case 1's message requires; and the `error` of a plan-prefix refusal is the one
   fixed line rule 12 publishes. That line is `rmp`'s own text, because nothing ran
   to produce a diagnostic, and it has no CLI counterpart to read alike, because
   `rmp graph client` publishes a plan rather than refusing the prefix. Two
   execution failures are the exception to the engine's diagnostic, and in
   both of them the CLI prints its own text too, so the rule that the two surfaces
   read alike is kept rather than broken: a lost or silent server connection
   (rule 10) and an exhausted serialisation retry (rule 11) carry `rmp`'s own
   line, because no engine produced either failure and there is no engine
   diagnostic to carry. The `500` of an internal read error does not carry
   this shape: it is answered as every other route's internal read error is.

8. **A request the caller abandoned is answered, but nobody reads the answer.** A
   client that disconnects mid-statement cancels it immediately (see
   [Graph Query Time Budget](#graph-query-time-budget), rule 2). The endpoint
   treats that cancellation as an execution failure like any other and answers it
   with the same `400` and the same `execution` kind, with an `error` naming the
   cancellation rather than the budget, because the two have different causes and
   the budget must not be blamed for a caller that gave up. That answer reaches no
   one: the client that would have read it is gone. It is specified here because it
   is a further reason the `execution` kind arises beyond the two case 2 names,
   and a contract naming only those two would be incomplete on the day it is
   written. Rule 10 names the fourth and rule 11 the fifth. It is not an outcome
   a connected client can observe, so no client-side test can assert it.

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

10. **A graph server that stops answering mid-statement is an execution failure,
    and it is the one graph-server failure that is not a 5xx.** The statement
    always runs in the server rather than in this process (see
    [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
    rule 1). Two outcomes belong here: a connection lost after the statement was
    sent, and a server that is still connected but has not answered within the
    endpoint's backstop deadline, which is what a statement the budget cut
    mid-write looks like from outside (`GRAPH.md § Server Resolution`, rule 7).
    Each leaves the endpoint unable to say whether the statement committed,
    because a commit is made durable before it is acknowledged. The endpoint
    answers `400` with `kind` `execution`, and its `error` names the lost or silent
    connection rather than the budget or an engine diagnostic, because the cause is
    neither. It MUST NOT re-send the statement and MUST NOT reach the store: the
    statement may already have taken effect, and this process has no way to open a
    store in any case. This is the fourth reason the `execution` kind
    arises, after an engine failure, a budget exhaustion, and rule 8's abandoned
    request, and it changes neither the status nor the kind set rule 4 enumerates.

11. **An exhausted serialisation retry is an execution failure, and its `error`
    names the contention rather than the engine.** Two writers whose statements
    touch the same node collide inside the server.
    The losing transaction commits nothing, and the client this endpoint shares
    with the CLI re-sends the statement under the project's retry policy
    (`GRAPH.md § Concurrency Inside the Server`). Nothing of that reaches the
    page: a conflict is not surfaced on its first occurrence, and a request whose
    retry eventually succeeds is answered HTTP `200` like any other. Only when
    every attempt of the policy has collided does the request fail. It then fails
    as an execution failure, HTTP `400` with `kind` `execution` — the status rule
    4 settles — carrying as its `error` the same contention line `rmp` prints for
    the same condition (`COMMANDS.md § Client Error Cases`). That `error` is
    `rmp`'s own text rather than an engine diagnostic, because the whole reason
    the line exists is that the engine's diagnostic is what a reader cannot tell
    apart from an invalid statement, and the query bar's user faces exactly the
    decision the CLI's user faces: run the statement again, or correct it. The
    endpoint MUST NOT re-send the statement outside the retry policy and has no
    store to run it against in any case (`GRAPH.md § Server Resolution`, rule 3).
    This is the fifth reason the `execution`
    kind arises, and it changes neither the status nor the kind set rule 4
    enumerates.

12. **A statement carrying an `EXPLAIN` or `PROFILE` prefix is refused before
    anything is sent, because this endpoint's response has no place for a query
    plan.** The response carries nodes and edges and nothing else
    (`DATA_FORMATS.md § Graph View Data`), while a prefixed statement asks for a
    plan (`GRAPH.md § Query Plans: The EXPLAIN and PROFILE Prefixes`). Were one
    sent, an `EXPLAIN` would execute nothing and return no row, so it would render
    as the empty graph of rule 9, which cannot be told apart from a statement that
    matched nothing, and its plan would be discarded; a `PROFILE` would render its
    rows and discard the measurement the caller asked for. The endpoint therefore
    refuses both, and its answer names the surface that does publish a plan.

    **Recognition is the engine's, and nothing of it is restated here.** The
    endpoint hands the statement it resolved from `q` — the trimmed `q`, before any
    node `LIMIT` is appended — to the pinned engine's own statement parser, the one
    the engine itself consults to decide whether a statement carries a prefix, and
    refuses the statement when that parser reports that it does. Which spellings
    carry a prefix is fixed by the engine's grammar: case, whitespace, and a
    comment ahead of the prefix count exactly as that grammar counts them, and this
    specification states no lexical rule of its own. The parse is a question and
    nothing else. It changes nothing the endpoint sends: a statement that is sent
    is the statement the endpoint resolved, never text derived from the parse. The
    default query carries no prefix, so a request with no `q` is never refused.

    **Text the parser cannot parse is not refused here.** It takes the path it
    took before this rule existed: it is sent, and the engine's own diagnostic
    reaches the caller as an execution failure (case 2). A prefixed schema
    statement is such text, because the prefix grammar admits no schema statement
    (`GRAPH.md § Query Plans: The EXPLAIN and PROFILE Prefixes`, rule 6). A
    statement in which `EXPLAIN` or `PROFILE` appears other than as its prefix —
    as a variable, a label, a property key, inside a string literal, or inside a
    comment that follows the statement — is one the parser reports as carrying no
    prefix, and it is sent like any other.

    **The answer.** The endpoint answers HTTP `400 Bad Request` with `kind`
    `plan_prefix` and, as its `error`, exactly this line, which names neither the
    prefix nor the statement and is the same for every refused request:

    `query not run: the query bar cannot show a query plan; remove the EXPLAIN or PROFILE prefix, or run the statement with rmp graph client`

    **Nothing runs, and no server is needed.** The prefix is examined only once the
    `limit` has been accepted (rule 5), and before the roadmap's graph server is
    resolved: before the socket path is derived, before that path is checked
    against the platform's bound, and before the socket is probed. The statement
    is never sent, so it writes nothing, and neither `EXPLAIN CREATE ...` nor
    `PROFILE CREATE ...` reaches a server. The refusal is the same `400` for a
    roadmap with no graph server listening, and for one whose derived socket path
    is over the platform's bound, as for one being served: the `503` and the `500`
    of rule 6 are reached only by a request whose statement the parser reports as
    carrying no prefix.

    **The refusal withdraws no write, and it is not a guard rail.** The endpoint
    still refuses nothing on the ground of what a statement does. Every write the
    query bar could make before this rule existed it can make after it, written
    without the prefix: an `EXPLAIN` of a write executes nothing wherever it runs,
    and the engine refuses a `PROFILE` of a write wherever it runs
    (`GRAPH.md § Query Plans: The EXPLAIN and PROFILE Prefixes`, rules 1 and 4).
    The refusal is therefore no security control, and it does not narrow the
    property [Security and Constraints](#security-and-constraints), rule 3,
    states.

    **One consequence is accepted.** A `PROFILE` of a read executes, and
    `PROFILE MATCH (n) RETURN n` would render the same nodes the unprefixed
    statement renders. It is refused all the same: a caller who wrote the prefix
    asked for the measurement, and an answer that rendered the rows and silently
    dropped it is the kind of answer this rule exists to prevent. Removing the
    prefix renders the graph.

    **A statement carrying no prefix is unaffected.** It is sent exactly as it was
    sent before this rule existed, and its response is the same, byte for byte.

    **Why a kind of its own.** Each `kind` names a distinct correction. An
    `invalid_limit` is corrected by choosing another limit, and an `execution` by
    correcting the statement or running it again; neither tells the caller to
    remove a prefix, which is the only correction that clears this refusal. The
    status is the one rule 4 settles for every query-bar failure, the page
    surfaces the `error` in place as rule 3 requires, and the refusal is recorded
    as every other query-bar failure is (see [What Is Logged](#what-is-logged)).

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
- **Failure responses.** The three query-bar failures of this endpoint — an
  invalid `limit`, a statement carrying an `EXPLAIN` or `PROFILE` prefix, and a
  statement that fails to execute — are each answered with HTTP
  `400 Bad Request` and a JSON body naming the failure's class in a `kind` field,
  in the shape specified in `DATA_FORMATS.md § Graph View Data`,
  **Error Shape**. The status, the `kind` values, the order in which the three are
  decided, and the boundary against the `500` of an internal read error are
  specified in [Query-Bar Error Handling](#query-bar-error-handling); this section
  does not restate them.
- **Query parameters.** The endpoint accepts two optional URL query parameters
  that the graph page's query bar (see
  [Graph Query Bar](#graph-query-bar)) sends, and that drive which Cypher query
  runs and how many results it returns:
  - `q` — the Cypher statement to run, URL-encoded. It is executed as written,
    with one exception: a statement the engine's parser reports as carrying an
    `EXPLAIN` or `PROFILE` prefix is refused and never sent (see
    [Query-Bar Error Handling](#query-bar-error-handling), rule 12). The endpoint
    examines it for nothing else and refuses nothing on the ground of what it
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
  Cypher statement (`GRAPH.md § Maximum Query Length`) and the recognition of an
  `EXPLAIN` or `PROFILE` prefix, which asks the engine's parser whether the
  statement carries one and nothing else (see
  [Query-Bar Error Handling](#query-bar-error-handling), rule 12). It does not
  classify the statement by what it does, does not inspect the patterns it binds,
  and does not inspect the values it would write. A statement carrying `CREATE`,
  `MERGE`, `SET`, `REMOVE`, `DELETE`, `DETACH DELETE`, or any schema DDL, and no
  such prefix, is executed and committed.
- **The endpoint sends the statement to a running graph server, and has no other
  way to run one.** It resolves the roadmap's socket and sends the statement to
  whatever server answers there. It opens no store, takes no lock, and constructs
  no engine. With no server listening the request is answered HTTP `503`; the
  rule, its four states, and the status this endpoint answers with in each are
  specified in `GRAPH.md § Server Resolution` and in
  [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
  rule 1. No query parameter and no configuration selects anything here, because
  there is nothing to select between.
- **The write a caller submits is real because the server is on the transactional
  path.** A statement that creates, changes, deletes or alters the schema is
  executed and committed inside `rmp graph serve`, and is durable before the
  acknowledgement crosses back (see `GRAPH.md § Engine Constructor by Path` and
  `GRAPH.md § Durability and Checkpointing in a Long-Lived Process`). That is what
  keeps this endpoint's `200` honest: an endpoint that ran the statement against a
  graph of its own would discard the write when the request ended and still answer
  `200`. The endpoint writes no audit entry and never touches a roadmap's
  `project.db`; the only thing it writes to at all is the socket.
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
    schema listing is read from `rmp graph client`, which returns the rows.

    **Why the suppression is required, and why it costs nothing.** Appending a
    `LIMIT` to a statement that admits none bounds nothing, and has one of two
    outcomes. Usually the statement fails in the **parser**, so a form that
    `rmp graph client` runs would be unusable through this endpoint and the
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

## Read-Only Data Flow

The web interface reads the same on-disk data the CLI reads, through the same
location rules, and never writes to it. Each request opens the data, reads the
current state, and releases the handle; the freshly read state is what the user
sees, and the `Cache-Control: no-store` header on every data-derived response
(see [Cache Policy](#cache-policy)) ensures no client-side or intermediary cache
re-presents an earlier, now-stale response in its place.

### Tasks and Sprints from SQLite

1. For a roadmap sprints request, a roadmap tasks request, a roadmap sprint
   request, a roadmap task request, or a roadmap audit log request, the server resolves the roadmap's
   database at
   `~/.roadmaps/{name}/project.db` (see `ARCHITECTURE.md § Directory Structure`)
   and reads its sprints, tasks, and audit entries using the existing read queries
   defined in
   `DATABASE.md § Main SQL Queries`. The sprints page reads the roadmap's sprints
   and each sprint's total task count for its card footer, but no member tasks,
   because the page renders every sprint as a card with no member tasks on it; the
   tasks page reads the roadmap's sprints for its sprint filter and the task list
   narrowed by the page's structured filters, and applies the search term and
   selects the requested page in memory — two queries, with none per row (see
   [Roadmap Tasks Page](#roadmap-tasks-page), **Read cost**); the
   sprint page reads that sprint and its member tasks in `sprint_tasks` position
   order, which its own board then groups into the three columns and orders in
   memory — the `WAITING` column keeping the position order the read returned, the
   `DOING` and `CLOSED` columns reordered by `started_at` and `closed_at`
   descending — again with no further query per column and none per card (see
   [Sprint Detail Sub-Template](#sprint-detail-sub-template)); the task page
   reads that task, its comments, its sprint membership, and — when it belongs to
   a sprint — that sprint and its member tasks, a fixed number of queries (see
   [Roadmap Task Page](#roadmap-task-page), **Read cost**); the
   audit log page reads the
   roadmap's audit entries ordered by `performed_at` descending, one fixed-size page
   at a time (see [Roadmap Audit Log Page](#roadmap-audit-log-page) and
   `DATABASE.md § Audit Queries`).
   The web interface adds no new schema, no new table, and no new write query.
   A task's full field set and its comments are read only by that task's own page,
   one task at a time (see [Roadmap Task Page](#roadmap-task-page)). A page that
   shows many tasks therefore reads only what it displays itself: the sprint
   page's board reads a comment **count** per rendered task, in one grouped counting
   query over the sprint's member tasks, because a card shows a count and
   no comment text (see
   `DATABASE.md § Count Comments for Many Parents (Grouped)`), and the tasks page's
   list, which shows no comment information, reads no comment at all. The Roadmap
   Sprint Page additionally presents the sprint's own comment log, so it reads that sprint's
   comments in full in one further query (see `DATABASE.md § Comments`): the sprint
   page therefore issues exactly **two** comment reads — the sprint's own listing
   and the one grouped count over its member tasks — whatever the number of member
   tasks, and it reads the comment **body** of no member task. The grouped count is
   skipped entirely when the sprint has no member task, because there is no card to
   count for; the sprint's own comment listing is
   still issued, because the Comments card is always present. Every one of these is
   a read query issued server-side while the page
   is rendered, and the number of them per page does not grow with the number of
   tasks shown. The tasks page issues no sprint-resolution query: its rows show no
   sprint. The
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

1. **The endpoint reaches a graph through the graph client and through nothing
   else.** For a graph page or graph data request, the server resolves the
   roadmap's socket and sends the statement to the `rmp graph serve` process
   listening there. It opens no graph store, takes no advisory lock, constructs no
   engine, and has no second route in. `GRAPH.md § Server Resolution` is canonical
   for the resolution rule, for the four states it distinguishes, and for the
   bounded probe that decides between them; this section does not restate it and
   adds no rule of its own. What it states is the outcome this endpoint produces
   in each state, because a status code is this surface's own business:
   - **A server is answering.** The endpoint sends the statement to that server
     over the protocol. A statement that runs is answered HTTP `200` with the
     ordinary response shape.
   - **No socket exists, or the socket refuses the connection.** No server is
     listening — a socket file a killed server left behind is exactly this second
     case, and is not distinguished from an absent one. The graph cannot be
     reached, and the request is answered **HTTP `503 Service Unavailable`** (see
     [Routes and Pages](#routes-and-pages)). The endpoint does **not** open the
     store, does not create the roadmap's `graph/` directory, and leaves nothing
     behind.
   - **A socket answers but no server can be reached through it.** The request is
     answered HTTP `503`, for the same reason and with the same body.
   - **The connection is lost after the statement has been sent, or the server
     does not answer within the endpoint's backstop deadline.** The request is an
     execution failure, HTTP `400` with the `execution` kind, because the failure
     surfaced once the statement was running, which is where
     [Query-Bar Error Handling](#query-bar-error-handling), rule 6, already draws
     the boundary. No new status and no new kind is introduced.

   **The first three states are one answer, and the fourth is the only one that
   is not.** A request that never got a statement running is answered `503` with
   the opaque body every other server-side failure carries and with no `kind`,
   because `kind` belongs to the `400`s and names a fault in what the caller
   submitted; nothing the caller submitted is at fault when no server is running.
   A request whose statement was running when the failure arrived is a `400`, as
   it always was.

   **`503` is the status because the condition is a missing dependency the
   operator controls, and it is transitory.** RFC 9110, Section 15.6.4, defines
   `503` for a server "currently unable to handle the request due to a temporary
   overload or scheduled maintenance, which will likely be alleviated after some
   delay". A graph server is exactly that kind of dependency: the web interface is
   working correctly, the roadmap and its database are readable, every other route
   is served, and the one thing missing is a process the operator starts with
   `rmp graph serve`. Starting it clears the condition, with no change to this
   server and none to the request. `500` was the alternative and is refused here:
   it asserts that this server failed, which conflates a defect in the product
   with a configuration the operator has simply not set up yet, and would send an
   operator looking for a fault where there is none. The cost is accepted rather
   than hidden: this endpoint's published status set widens by one code, and
   [Routes and Pages](#routes-and-pages) is canonical for the whole set.

   **No `Retry-After` header is sent.** RFC 9110, Section 15.6.4, permits one and
   does not require it, and nothing in the product knows when an operator will
   start a server. A header carrying a guess would be worse than its absence,
   because a client that honoured it would delay by a figure with nothing behind
   it.

   **That reasoning does not reach a statement that was already running**, which
   keeps the `400` the fourth state gives it. A server that answered, ran the
   statement and then went silent is not an unavailable service: it was available,
   it did the work, and the outcome is unknown.
   [Query-Bar Error Handling](#query-bar-error-handling), rule 6, draws that
   boundary and rules 10 and 11 state why neither `503` nor `504` fits there.

   **One condition also fails before a statement runs and is deliberately not a
   `503`, and the difference is permanence.** When the socket path derived for the
   roadmap is longer than the platform allows a socket path to be, the endpoint
   refuses the request rather than probing a path nothing can be bound to. It
   answers HTTP `500` and carries no `kind`: the request never reached a
   statement, so it is not a query-bar failure, and the store is not opened. `503`
   is refused for it because no server can **ever** listen there — no delay
   alleviates it, and no operator action short of moving or renaming the roadmap
   changes it — so announcing a service that will come back would be false. This
   endpoint publishes no `--socket` flag and can receive no path, so unlike the
   command line it has no way to reach a shorter one: the graph page still
   renders, and every fetch it makes for that roadmap's data is refused, for as
   long as the derived path is what it is.
   `GRAPH.md § Socket Path Length`, rules 5 and 6, is canonical for the bound and
   for why the refusal binds this surface as it binds the ones that publish the
   flag.

   Resolution runs once per request and its outcome is not cached: a cached
   outcome would act on a server that had since stopped. It is spent before the
   statement starts.

   **The socket this endpoint resolves is always the derived one, and nothing can
   point it elsewhere.** Both `rmp graph` subcommands take a `--socket` flag; this
   endpoint takes none, accepts no request parameter carrying a path, and has no
   command line to receive one — `rmp web` serves every roadmap at once rather
   than one. A server started on a non-default socket is therefore invisible here,
   and every request for that roadmap's graph is answered `503` for as long as it
   runs there. `GRAPH.md § Serving on a Non-Default Socket` is canonical for that
   boundary and states plainly that no flag closes it.

2. **The failure the CLI reports and the failure this endpoint reports are the
   same failure, and they must be classified the same way.** With no server
   listening, `rmp graph client` exits 1 with the no-server line
   `COMMANDS.md § Graph Server Socket Error Lines` publishes, naming the socket
   path it probed. This endpoint meets that identical condition, through that
   identical client, and MUST classify it identically: the same sentinel,
   `utils.ErrGraphServer`, and the same line, which it writes to its log.

   **Where the two legitimately differ is what the caller is shown, and only
   that.** A CLI caller is the operator, so the line goes to stderr and the
   process exits 1. An HTTP caller is a browser that may not be the operator's, so
   the response body carries the opaque `internal server error` this interface
   gives every server-side failure — never the socket path, which is a filesystem
   path inside the operator's home directory (see
   [Record Content](#record-content), rule 6) — and the line itself goes to the
   server's log at `WARN`, where the operator reads it (see
   [What Is Logged](#what-is-logged)).

   **Neither the status nor the level is derived from the CLI's exit code.** An
   exit code says that an invocation failed; a status code says what kind of thing
   failed, and a log level says how much it matters to the operator of *this*
   process. The client exits 1 because its one job could not be done, while this
   server did its job and went on serving every other route — so `503` and `WARN`
   are not a softer reading of the same event but the accurate one for a different
   process. The classification behind all three is one, and only the reporting
   differs.

3. **The endpoint uses the client mechanism, not the client command.** It calls
   `internal/graphclient` directly, in its own process. It MUST NOT run
   `rmp graph client` as a child process: spawning one would put a process
   boundary, an argument-quoting layer, an exit code and a second copy of the
   output serialisation between this endpoint and the answer it owes, and it would
   make the endpoint's behaviour depend on which binary is on a path rather than
   on the code it was built from. `GRAPH.md § The Bolt Client` is canonical for
   that requirement, and `ARCHITECTURE.md § 9. internal/graphclient/ and reaching
   a graph server` for the package that realises it.

4. The endpoint sends the statement the request carries, or the default query when
   the request carries none. It does not examine what that statement does, so the
   statement may write. The one statement it does not send is one the engine's
   parser reports as carrying an `EXPLAIN` or `PROFILE` prefix, which it refuses
   before resolving a server
   ([Query-Bar Error Handling](#query-bar-error-handling), rule 12). What crosses
   is the statement the request carried, with the
   node-`LIMIT` injection already applied and nothing else changed (see
   `GRAPH.md § Server Resolution`, rule 5).

5. **The graph is therefore written when the statement is a write, and the
   writing is the server's.** The transaction commits inside `rmp graph serve` and
   is durable before the acknowledgement crosses back, and the fold of the
   write-ahead log is the server's business on its own cadence (see
   `GRAPH.md § Durability and Checkpointing in a Long-Lived Process`). This
   interface writes no audit entry, and it never writes to a roadmap's
   `project.db` outside the startup migration (see
   [Read-Only Data Flow](#read-only-data-flow)).

6. **A graph request now leaves nothing at all on disk on this process's
   account.** Opening a store runs GoGraph's recovery, which repairs an
   interrupted checkpoint before it loads anything, and that repair used to be
   reachable from a web request that wrote nothing. It is not reachable now: this
   process opens no store, so it runs no recovery, creates no `write.lock`, and
   creates no `graph/` directory. `GRAPH.md § What a Statement That Writes Nothing
   Changes on Disk` remains canonical for what a statement changes, and everything
   it lists is now done by the server rather than by a request.

7. **No lock is taken, and no request waits for one.** A dedicated graph server
   holds the graph store's advisory lock for its process lifetime, and no finite
   wait can be sized against such a hold; the endpoint never takes that lock, so
   no request waits on it and none is ever refused because of it. The lock, its
   single mode, and the one contention it still governs — between two servers —
   are specified in `GRAPH.md § Concurrency and Recovery` and
   `GRAPH.md § Lock Contention`, which are canonical and to which this section
   adds nothing.

   Two consequences are specific to the web interface and are stated here:
   - **A graph data request no longer waits for anything but the server.** It
     spends the resolution probe, and then the statement under the endpoint's
     backstop deadline. Two graph pages open on the same roadmap do not serialise
     against each other here: their statements run concurrently inside the server
     and are resolved by the store's MVCC (see
     `GRAPH.md § Concurrency Inside the Server`). A slow statement submitted
     through one query bar no longer delays another request against the same
     roadmap on this side of the socket. What a request may still meet is a
     serialisation conflict, which the client retries and surfaces only when the
     retry policy is exhausted.
   - **The write-timeout invariant is easier to satisfy than it was, and it is
     restated rather than assumed.** Everything a request may spend before its
     response must fit inside the server's 30-second `WriteTimeout` (see
     [HTTP Server Timeouts](#http-server-timeouts)). Two bounded terms remain: the
     resolution probe, 2500 ms, and the backstop deadline over the statement,
     7.5 seconds (see `GRAPH.md § Server Resolution`, rule 7). Ten seconds, inside
     thirty. The third term the old direct path contributed — a bounded wait for
     the store lock — is gone with the path, and so is the case in which a
     statement cut mid-write held this process past the write timeout: that
     statement now runs in the server, and this process stops waiting for it at
     7.5 seconds whatever the server is still doing.

8. Each request resolves, sends, reads the answer and closes its connection. The
   server holds no graph connection open across requests. A graph store that is
   corrupt or unreadable is met by `rmp graph serve` at its own startup and not by
   this interface, so a roadmap whose store cannot be opened has no server, and a
   request for its graph is answered by the no-server outcome of rule 1: HTTP
   `503`, with the reason readable in the graph server's own diagnostics rather
   than in this interface's.

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
6. **The Markdown renderer is compiled in.** The server-side Markdown renderer
   (see [Markdown Rendering](#markdown-rendering)) is built from Go modules that
   are compiled into the binary: `github.com/yuin/goldmark`,
   `github.com/yuin/goldmark-highlighting/v2`, and
   `github.com/alecthomas/chroma/v2`, together with the regular-expression module
   chroma itself requires, `dlclark/regexp2`. None of them loads anything at
   runtime: no
   lexer, style, or other data file is read from the host filesystem or fetched
   from a remote origin. The syntax-highlighting stylesheet is an embedded
   static asset like every other stylesheet (see
   [Embedded Asset Categories](#embedded-asset-categories)). The modules and the
   rules that pin them are listed in `BUILD.md § External Dependencies`.

### Embedded Asset Categories

Every asset category below is embedded into the binary with `go:embed` and served
only from the embedded asset set. This enumeration defines the complete set of
asset categories the binary must carry; no category is fetched from the network or
read from the host filesystem at runtime.

1. **HTML templates** — the `html/template` set that renders every page.
2. **Stylesheet** — all CSS, including the vendored Tabler CSS framework (the UI
   framework, see [UI Framework](#ui-framework)), the syntax-highlighting
   stylesheet of rendered Markdown code blocks (see
   [Markdown Rendering](#markdown-rendering), rule 7), and any further vendored CSS
   the interface uses (see
   [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
3. **JavaScript** — all client scripts, including the Tabler JavaScript (the UI
   framework's scripts) and the D3.js knowledge-graph visualisation library (and
   the d3-sankey plugin) and any of their dependencies, all in already-built
   (vendored) form.
4. **Web fonts** — every font the interface uses, including the Inter font in
   its upright and its italic face (see [UI Framework](#ui-framework), rule 4)
   and the Tabler Icons webfont; no font is loaded from a remote font host.
5. **Icons and images** — any icon or image the interface displays, including the
   Tabler Icons set.
6. **Favicon** — the site favicon.
7. **Any other static asset** — any further static asset the interface requires
   is embedded under the same rule; no static asset is exempt.

### Frontend Rules

1. **Server-rendered HTML.** Pages are rendered with Go's `html/template`. The
   template set is embedded into the binary at build time with `go:embed`.
   `html/template` performs contextual auto-escaping, which is the primary
   defence against injecting roadmap-derived text (task titles, sprint titles,
   graph property values) into the page (see
   [Security and Constraints](#security-and-constraints)). The one value a
   template inserts without escaping is the HTML of a Markdown field, and it comes
   only from the Markdown renderer (see rule 7).
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
6. **Authored line breaks preserved in plain multi-line free-text.** Free-text
   that the interface shows as plain text, and not as Markdown, may be multi-line:
   its author entered line breaks (newlines) in it. Where the interface renders
   such plain free-text — the property values shown in the knowledge-graph detail
   panel when a node or edge is selected, and any other multi-line value that is
   not one of the Markdown fields of rule 7 — the interface preserves the author's
   line breaks rather than collapsing them under HTML's default whitespace
   handling. The text still wraps within its container, so preserving line breaks
   introduces no forced horizontal scrolling, and the text is still emitted as the
   element's text content (never as raw HTML): a server-rendered value through
   `html/template`'s contextual auto-escaping (rule 1), and a graph detail panel
   value through the DOM `textContent` property. This rule does not govern the
   Markdown fields; rule 7 does. The
   [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page) section
   references this rule.
7. **Markdown fields render as the Markdown renderer's HTML.** The Markdown
   fields — the task `functional_requirements`, `technical_requirements`,
   `acceptance_criteria`, and `completion_summary`, the task comment `body`, the
   sprint comment `body`, and the sprint `description` — are rendered wherever the
   interface shows them as the HTML the single server-side Markdown renderer
   produces from them, never as escaped plain text and never by a Markdown parser
   in the browser. That HTML is the only markup the interface inserts without
   escaping, and it comes from that renderer alone. The renderer, the Markdown it
   accepts, the HTML it emits, and the safety properties that HTML carries are
   specified in [Markdown Rendering](#markdown-rendering); the
   [Roadmap Task Page](#roadmap-task-page),
   [Roadmap Sprints Page](#roadmap-sprints-page),
   [Roadmap Sprint Page](#roadmap-sprint-page),
   [Shared Sprint-Card Partial](#shared-sprint-card-partial), and
   [Sprint Detail Sub-Template](#sprint-detail-sub-template) sections reference
   it.
8. **Displayed timestamps take one display form.** Every date and time a page
   displays reads `YYYY-MM-DD HH:mm:ss`, in UTC as stored, while every JSON the
   interface serves keeps the canonical ISO 8601 format. The form, where it is
   produced, the machine-readable value kept beside it, and the surfaces it
   governs are specified in [Date and Time Display](#date-and-time-display).

### Markdown Rendering

The Markdown fields of rule 7 of [Frontend Rules](#frontend-rules) are authored
through the CLI as Markdown, and the web interface renders each of them as the
richest HTML it can emit safely. This section is the canonical specification of
that rendering: which fields it covers, the one renderer that performs it, the
Markdown it accepts, the HTML it emits, and the properties that make that HTML
safe to insert into a page. The stored text is never altered: rendering happens on
the way out, on every request, and the CLI's output of these fields is unchanged.

1. **The Markdown fields, and where each is rendered.** Exactly seven fields are
   rendered as Markdown, on every surface that shows them:
   - the task `functional_requirements`, `technical_requirements`,
     `acceptance_criteria`, and `completion_summary`, on the Roadmap Task Page
     (see [Roadmap Task Page](#roadmap-task-page));
   - the task comment `body`, in the Comments card of the Roadmap Task Page;
   - the sprint comment `body`, in the Comments card of the Roadmap Sprint Page
     (see [Sprint Detail Sub-Template](#sprint-detail-sub-template));
   - the sprint `description`, in the sprint card of every tab of the Roadmap
     Sprints Page (see [Shared Sprint-Card Partial](#shared-sprint-card-partial))
     and on the Roadmap Sprint Page (see
     [Sprint Detail Sub-Template](#sprint-detail-sub-template)).

   No other value is rendered as Markdown. The task `title` and the sprint `title`
   stay plain text wherever they appear, and so does every other value the
   interface shows. The property values of the knowledge-graph detail panel are
   out of this section's scope and keep the plain-text rendering of
   [Frontend Rules](#frontend-rules), rule 6.
2. **One renderer, on the server.** Markdown is rendered on the server, in Go, by
   one rendering unit whose single responsibility is to turn the stored text of
   one Markdown field into an HTML fragment. The unit is built on
   `github.com/yuin/goldmark`, a CommonMark-compliant parser and renderer. Every
   surface obtains its HTML from this one unit: the server-rendered pages insert
   its output into the page. No surface renders Markdown any other way, and the
   browser never parses Markdown.
   The rendering is deterministic: the same stored text on the same surface
   produces the same bytes. The unit holds no state between calls and keeps no
   cache of rendered output (see
   [Security and Constraints](#security-and-constraints), rule 12).
3. **The Markdown accepted.** The renderer accepts CommonMark together with these
   extensions, and no other:
   - the GitHub Flavored Markdown extensions: tables, strikethrough, autolinks
     (a bare `http://`, `https://`, or `www.` address becomes a link without angle
     brackets), and task lists, whose checkboxes render as
     `<input type="checkbox">` carrying the `disabled` attribute, and carrying
     `checked` for a checked item, so they can be neither checked nor unchecked;
     the `<li>` of a task-list item carries the fixed class `task-list-item`,
     which the renderer adds and which rule 13 styles;
   - footnotes;
   - definition lists;
   - syntax highlighting of fenced code blocks (rule 6).

   The renderer does **not** enable raw HTML output, the attribute syntax that
   lets an author attach an `id`, a `class`, or any other attribute to an element,
   or automatic heading identifiers.
4. **Plain text remains valid Markdown, and a single newline is a line break.**
   Text written without any Markdown syntax renders as paragraphs: a blank line
   separates two paragraphs. A single newline inside a paragraph renders as a line
   break (`<br>`), not as the space CommonMark's soft line break would produce,
   so every line break the author entered stays visible.
5. **Headings are demoted.** A heading in a Markdown field never outranks the
   structure of the page it appears in. A level-1 heading (`#`, or a
   Setext heading underlined with `=`) renders as `<h4>`, a level-2 heading (`##`,
   or a Setext heading underlined with `-`) renders as `<h5>`, and a heading of
   level 3 to 6 renders as `<h6>`. A rendered heading carries no `id` attribute.
6. **Fenced code blocks are highlighted by their declared language only.** A
   fenced code block whose info string begins with a language name that
   `github.com/alecthomas/chroma/v2` recognises is highlighted by chroma, through
   `github.com/yuin/goldmark-highlighting/v2`, and its tokens are marked with
   chroma's CSS classes, never with a `style` attribute. A fenced code block with no info
   string, or with a language name chroma does not recognise, and every indented
   code block, renders unhighlighted: a monospaced preformatted block whose text
   carries no token class. The renderer never guesses a block's language from its
   content, so the same block renders the same way whatever it contains.
7. **One syntax-highlighting stylesheet, dark.** The colours of highlighted
   tokens come from one embedded stylesheet, served from `/static/highlight.css`.
   It holds the CSS chroma produces, in class-based form, for its `github-dark`
   style, and its rules are not scoped by any theme selector. The interface has a
   single, fixed dark theme and offers no theme toggle (see
   [UI Framework](#ui-framework), rule 2), so no light stylesheet exists; changing
   the theme is a SPEC change to that rule and to this one. Every page that can
   render a Markdown field — the Roadmap Sprints Page, the Roadmap Sprint Page, and
   the Roadmap Task Page — links the stylesheet. The Roadmap Tasks Page renders
   no Markdown field and does not link it. Its content is the output of
   the chroma version `go.mod` pins; a stylesheet that differs from what that
   version produces for the `github-dark` style fails the test gate, so an upgrade
   of chroma cannot leave the class names in the rendered HTML and the class names
   in the stylesheet out of step.
8. **Links.** A link's destination first passes goldmark's dangerous-URL filter,
   which rejects a destination whose scheme is `javascript:`, `vbscript:`, or
   `file:`, and a `data:` destination other than a `data:image/png`,
   `data:image/gif`, `data:image/jpeg`, or `data:image/webp` URL, comparing the
   scheme without regard to case. Then:
   - a link whose destination the filter rejects renders as its link text alone,
     with no `<a>` element, so it is never an active link;
   - a link whose destination is absolute with the scheme `http` or `https`
     (compared without regard to case), or a network-path reference beginning
     with `//`, renders as `<a>` carrying `target="_blank"` and
     `rel="noopener noreferrer"`, so it opens in a new tab and the opened page
     receives neither a reference to this page nor the page's address;
   - every other link — a relative reference, a fragment, or another scheme the
     filter accepts, such as `mailto:` — renders as `<a>` with no `target` and no
     `rel`, so it opens in the same tab.

   These rules apply equally to an inline link, a reference link, an autolink, and
   the link a remote image becomes (rule 9).
9. **Images.** An image's source passes the same dangerous-URL filter. Then:
   - an image whose source is a `data:` URL the filter accepts (one of the four
     raster image types rule 8 names) renders as `<img>` with its alternative
     text, which the Content-Security-Policy's `img-src 'self' data:` already
     admits;
   - an image whose source is any other URL the filter accepts — remote or
     relative — renders **not** as an image but as a link to that URL, whose text
     is the image's alternative text or, when the alternative text is empty, the
     URL itself; the link follows rule 8 for its `target` and `rel`;
   - an image whose source the filter rejects renders as its alternative text
     alone, with no element.

   A Markdown image therefore never causes the browser to make a request: the only
   image the renderer emits is a `data:` image, which the browser decodes without
   a request.
10. **Raw HTML is never emitted.** Raw HTML in the source — a block of HTML or an
    inline tag, such as `<script>`, `<iframe>`, or `<img onerror=...>` — is omitted
    from the output; at most an HTML comment marking the omission takes its place.
    Text that is not raw HTML, including a `<` or `&` inside a code span or code
    block, is escaped. An author can therefore introduce no element and no
    attribute of their own into the page through a Markdown field.
11. **No author-controlled attributes and no inline styles.** The rendered HTML
    carries no `style` attribute and no event-handler attribute, whatever the
    source contains. A table column's alignment is expressed through the Tabler
    text-alignment classes `text-start`, `text-center`, and `text-end` rather than
    a `style` attribute. No attribute value in the output is taken from the
    source, other than a link's destination and title and an image's alternative
    text, each escaped for its attribute context and each subject to rules 8
    and 9.
12. **Footnote identifiers are unique within a page.** The footnotes of one
    Markdown field render as a list at the end of that field's HTML, with a
    reference link to each note and a back-link from each note, which are
    same-page fragment links (rule 8). Several Markdown fields share one page — a
    sprint page holds the description and every sprint comment, and a task page
    holds four task fields and every task comment — so every `id` the renderer
    emits, and every fragment that points at one, carries a prefix that
    identifies its field and differs from the prefix of every other field on the
    page: `task-<id>-<field>-` for a task field (for example
    `task-42-acceptance_criteria-`), `task-comment-<id>-` for a task comment,
    `sprint-<id>-description-` for a sprint description, and
    `sprint-comment-<id>-` for a sprint comment. The prefix is built only from
    the entity's integer `id` and the field's fixed name, never from text the
    author wrote. Each footnote link therefore resolves within its own field, and
    no two elements of a page share an `id`.
13. **The Markdown container and its styling.** Each rendered fragment is placed
    in a `<div>` carrying Tabler's `markdown` class, the class the vendored Tabler
    distribution defines for rendered Markdown content, which styles its tables,
    blockquotes, headings, lists, and code blocks consistently with the rest of the
    interface. A `<div>` is used because a fragment contains block elements, which
    a paragraph cannot hold. The project override stylesheet `static/style.css`
    (see [UI Framework](#ui-framework), rule 10) adds to that class the overflow
    handling and the typography below, and nothing else.

    Every rule of that addition is scoped to the `.markdown` container, so it
    styles rendered Markdown and no other part of the interface. Its principle is
    that rendered Markdown reads as the interface's own body text: it imposes no
    line spacing of its own, and no vertical spacing of its own other than the
    `1rem` bottom margin of a table (see **Block spacing** below). Where the vendored `.markdown`
    rules depart from the interface's base styles — `font-size:
    var(--tblr-font-size-h3)` (`1rem`) and `line-height: var(--tblr-line-height-lg)`
    (`1.7142857143`) on the container, `font-size: .8125em` on a `pre`,
    `margin-top: 2.5rem` on a top-level `h2` to `h6`, `font-size: 1rem`,
    `margin: 1.5rem 0`, and `padding: .5rem 1.5rem` on a top-level `blockquote`,
    and `margin: 3em 0` on a top-level `hr` — the addition restores the base value
    stated below, and that is the value the element's computed style carries. The
    base values are those of the vendored Tabler stylesheet's own element rules
    (its Bootstrap reboot and Tabler's element defaults), cited here as the values
    those rules resolve to. The vendored rules that remove the top margin of the
    container's first child and the bottom margin of its last child stay in
    effect. In what follows, "body text" is the size of Tabler's
    `--tblr-body-font-size` (`0.875rem`), "the secondary colour" is
    `var(--tblr-secondary)`, the colour Tabler's `text-secondary` utility gives,
    and a top-level element is a direct child of the container. The same Markdown
    therefore looks the same on every surface that shows it, in a card, on a
    page, and inside a comments timeline alike.

    - **Overflow.** The content wraps within its container, a long word or URL
      breaks rather than overflowing, and a table or a code block wider than its
      container scrolls horizontally inside its own box. Rendered Markdown
      therefore never makes the page or a card scroll horizontally.
    - **Font size and line height.** The container's `font-size` is body text, in
      place of the vendored `1rem`, and its `line-height` is
      `var(--tblr-body-line-height)` (`1.4285714286`), the line height of the
      interface's body text, in place of the vendored `1.7142857143`. A `pre` has
      the `font-size` `.85714285em` of the vendored Bootstrap reboot `pre` rule
      (`pre{display:block;margin-top:0;margin-bottom:1rem;overflow:auto;font-size:.85714285em;...}`),
      in place of the vendored `.markdown pre` `.8125em`. The vendored distribution
      also carries a later Tabler `pre` rule that sets `.92857143em` and is the one
      the cascade applies to a `pre` outside the container; the value restored
      inside the container is the reboot rule's `.85714285em`, not that one.
    - **Block spacing.** Blocks keep the interface's base bottom margins, and the
      addition sets none of its own: a `p`, `ul`, `ol`, and `dl` has the `1rem`
      of the vendored `p` and `dl,ol,ul` rules, a `pre` the `1rem` of the vendored
      Bootstrap reboot `pre` rule (the later Tabler `pre` rule sets no margin), a `blockquote` the `0 0 1rem` of the vendored `blockquote` rule,
      and a `dd` the `.5rem` of the vendored `dd` rule. A `table` has a bottom
      margin of `1rem`, the same as the other blocks, in place of the `0` the
      vendored `.markdown>table` rules resolve to. A list item carries
      no margin, so consecutive list items are separated only by the line height,
      and a paragraph inside a list item keeps the `1rem` of every paragraph.
    - **Lists.** A `ul` that is not inside another list of the container shows the
      `disc` marker, a `ul` inside one list of the container shows the `circle`
      marker, and a `ul` inside two or more lists of the container shows the
      `square` marker. A list that is not inside another list of the container has
      the `1rem` bottom margin of the vendored `dl,ol,ul` rule, and a list nested
      inside a list item of the container has the `0` bottom margin of the
      vendored `ol ol,ol ul,ul ol,ul ul` rule; both have the `0` top margin of the
      vendored `dl,ol,ul` rule. The marker and the margins are set by these rules
      and depend only on lists inside the container, never on an enclosing element,
      so a list in a comment body, which sits inside the task page's or the
      sprint page's `<ul class="timeline">`, renders the same markers and the
      same margins as the same list in any other field.
    - **Headings.** The HTML heading levels stay those of rule 5. An `h4` has a
      `font-size` of `1rem` and a `line-height` of `1.5rem`; an `h5` has a
      `font-size` of `.875rem` and a `line-height` of `1.25rem`; an `h6` has a
      `font-size` of `.875rem`, a `line-height` of `1.25rem`, and the secondary
      colour. These are the size and line-height pairs the vendored stylesheet
      gives its `h3` and `h4`, so no rendered heading is smaller than body text. A
      heading has the bottom margin of the vendored heading rule,
      `var(--tblr-spacer)` (`.5rem`). A top-level heading has the top margin of
      the vendored heading rule, `0`, in place of the vendored `.markdown`
      `2.5rem`: a heading is separated from the block before it by that block's
      own bottom margin, which is `1rem` for a paragraph, a list, a code block, a
      blockquote, and a table alike.
    - **Links.** An `<a>` inside the container has the colour
      `rgb(121, 170, 231)` — the link colour of Tabler's own dark theme, whose
      value the vendored distribution publishes as `--tblr-link-color-rgb`
      (`121,170,231`) — and is underlined, with a `text-underline-offset` of
      `.15em`. While the pointer hovers it and while it has keyboard focus
      (`:hover` and `:focus-visible`), it has the colour `rgb(148, 187, 237)` —
      the link hover colour of Tabler's own dark theme, published as
      `--tblr-link-hover-color-rgb` (`148,187,237`) — and no underline. Against the
      card background of the dark theme, Tabler's `--tblr-bg-surface`, which
      resolves in the dark theme to `--tblr-gray-800`, `oklch(26.86% 0 0deg)`
      (`#262626`), the two colours have contrast ratios of 6.28:1 and 7.63:1, both
      above the 4.5:1 that WCAG 2.2 Success Criterion 1.4.3 (Contrast (Minimum))
      requires. No link outside the
      container changes colour.
    - **Blockquotes, rules, and footnotes.** A `blockquote` has the `font-size`
      of body text, the margin `0 0 1rem` of the vendored `blockquote` rule, the
      padding `1rem 1rem 1rem` of the vendored Tabler `blockquote` rule, and the
      secondary colour. An `hr` has the margin `2rem 0` of the vendored `hr` rule.
      The footnotes list rule 12 describes, which the renderer emits as a `<div>`
      carrying the class `footnotes`, has a `font-size` of `.8125rem` and the
      secondary colour.
    - **Task lists, definition lists, and code blocks.** A task-list item — an
      `<li>` carrying the class `task-list-item` (rule 3) — shows no list marker,
      and one rule serves both forms: in the ordinary form its checkbox, and in
      the non-interactive form of rule 14 its `task-list-marker` span holding
      `[x]` or `[ ]`, takes the place the list marker would occupy. A `dd` is indented by a left margin of
      `1.5rem`. A `pre` has a `tab-size` of `4`.
    - **Emphasis.** A `strong` or `b` element has a `font-weight` of `700`. An
      italic `em` or `i` element in the Inter font is drawn from Inter's real
      italic face (see [UI Framework](#ui-framework), rule 4), never from a slant
      the browser synthesises from the upright face.
    - **Line length.** On the [Roadmap Sprint Page](#roadmap-sprint-page), the
      container of the sprint `description` and the container of every sprint
      comment `body` have a `max-width` of `80ch`, so a line of rendered Markdown
      holds at most about 80 characters: the width WCAG 2.2 Success Criterion
      1.4.8 (Visual Presentation) names, and inside the 45 to 90 characters
      Matthew Butterick's *Practical Typography* recommends. The sprint card is
      already narrow and carries no such limit. On the
      [Roadmap Task Page](#roadmap-task-page), the container of each of the four
      task Markdown fields and of every task comment `body` carries no such limit
      either: it spans the full width of its card's body.

    This styling adds to the HTML the renderer emits only the fixed task-list
    classes of rules 3 and 14, which carry no text of the author's and change no
    visible text, and it changes nothing else in rules 1 to 12, 14, and 15: the line breaks of rule 4, the heading demotion of
    rule 5, the Content-Security-Policy, and every safety property stay as they
    are.
14. **The non-interactive form, for content placed inside a link.** The sprint
    card is a single link (see
    [Shared Sprint-Card Partial](#shared-sprint-card-partial), rule 3), and HTML
    admits no interactive element — a link or a form control — inside a link.
    Where rendered Markdown is placed inside a link, the renderer is used in its
    non-interactive form, which differs from the ordinary form in these respects
    only:
    - every element rule 8 would render as `<a>` — an inline, reference, or
      autolink, a footnote reference or back-link, and the link a remote image
      becomes under rule 9 — renders as its text alone, with no `<a>` element;
    - a task-list item's checkbox renders as the text marker `[x]` for a checked
      item or `[ ]` for an unchecked one, with no `<input>` element; the marker
      is wrapped in a `<span>` carrying the fixed class `task-list-marker`, so
      that rule 13 can place it where the list marker would be. The `<li>` keeps
      the `task-list-item` class of rule 3, which the ordinary form gives it too,
      so one rule of rule 13 styles the task-list item in both forms.

    Everything else — the accepted Markdown, the line breaks, the demoted
    headings, the highlighting, the filtering, the footnote identifier prefix, and
    the container — is identical to the ordinary form. The sprint card is the only
    surface that uses the non-interactive form.
15. **The safety boundary.** The renderer's output is the only HTML the web
    interface inserts into a page without escaping: a server-rendered page inserts
    it through `html/template` as trusted HTML. No script inserts it, and no JSON
    the interface serves carries it. Every other value stays escaped on the server
    and written as text in the browser. The rendering changes nothing in the Content-Security-Policy,
    which stays exactly the value in [Security Headers](#security-headers), and it
    adds no script: highlighting is performed on the server, so the page loads no
    highlighting script.

### Date and Time Display

Every date and time the web interface displays to a reader is shown in one
display form. The rule is stated here once; each surface that displays a
timestamp references this section and does not restate it.

1. **The display form.** A displayed timestamp reads `YYYY-MM-DD HH:mm:ss`: the
   four-digit year, the two-digit month, and the two-digit day joined by hyphens;
   exactly one space (U+0020); then the two-digit hour on the 24-hour clock (`00`
   to `23`), the two-digit minute, and the two-digit second joined by colons.
   Every field is zero-padded to its width. The stored value
   `2026-09-28T08:47:32.056Z` displays as `2026-09-28 08:47:32`, and the stored
   value `2026-01-05T17:03:09.000Z` displays as `2026-01-05 17:03:09`.
2. **Derived from the stored value, in UTC, with no conversion.** The source of
   every displayed timestamp is the stored value, which is in the canonical format
   of `DATA_FORMATS.md § Dates - ISO 8601 with UTC`. The display form carries
   exactly the stored value's year, month, day, hour, minute, and second, in UTC as
   stored. The interface converts no timestamp to the server's time zone, to the
   browser's time zone, or to any other zone, and applies no locale.
3. **Seconds are truncated, never rounded.** The fractional seconds are dropped,
   not rounded: the stored value `2026-09-28T08:47:59.999Z` displays as
   `2026-09-28 08:47:59`, never as `2026-09-28 08:48:00`. Truncation therefore
   never changes the date, the hour, the minute, or the second the stored value
   carries.
4. **What the display form drops, and where.** The `T` separator, the decimal
   point and the fractional seconds, and the `Z` suffix are absent from the
   displayed text, and from nowhere else. The stored value, every JSON the
   interface serves, and the machine-readable value of rule 6 keep the canonical
   format unchanged.
5. **Where the formatting happens.**
   - **Server-rendered pages.** One Go formatting function produces the display
     form, and every server-rendered surface that displays a timestamp formats it
     through that function. No template and no handler composes the display form by
     any other means.
   - **The formatted value is text.** A server-rendered page emits the display
     form through `html/template`'s contextual auto-escaping
     ([Frontend Rules](#frontend-rules), rule 1). No script formats a timestamp:
     every surface this rule governs is rendered on the server.
6. **The stored value is kept in the markup as the machine-readable value.** Every
   displayed timestamp is the text content of a `<time>` element whose `datetime`
   attribute holds the stored value verbatim, in the canonical format: the stored
   value `2026-09-28T08:47:32.056Z` is rendered as
   `<time datetime="2026-09-28T08:47:32.056Z">2026-09-28 08:47:32</time>`. The
   canonical format is a valid global date and time string in the sense the HTML
   Living Standard defines for the `datetime` attribute of the `time` element, so
   the attribute gives assistive technology and any other machine reader the exact
   instant, including the milliseconds and the zone the display form omits. The
   `<time>` element holds the display form and nothing else: a label or a marker
   shown beside a timestamp — a datagrid field name, the edited marker of a
   comment — stays outside it. A server-rendered page sets the attribute through
   `html/template`.
7. **An unset timestamp keeps its placeholder.** A nullable timestamp that is unset
   — a task's `started_at`, `tested_at`, or `closed_at`, a sprint's `started_at` or
   `closed_at` — displays the neutral placeholder the surface already uses for it,
   an em dash, and no `<time>` element is rendered for it, because there is no
   instant to state. A comment whose `updated_at` is null shows no edited marker,
   exactly as before; this rule adds nothing in its place.
   **A stored value not in the canonical format is displayed unchanged.** When a
   stored timestamp is set but is not in the canonical format of
   `DATA_FORMATS.md § Dates - ISO 8601 with UTC`, the interface displays the stored
   text unchanged, as text escaped by `html/template`, and renders no `<time>`
   element for it, because such a value is not a valid `datetime` attribute value.
   The interface neither reformats, truncates, nor replaces it, and does not fail
   the page because of it.
8. **The surfaces this rule governs.** The rule governs every timestamp a page of
   the interface displays. They are:
   - on the [Roadmap Sprint Page](#roadmap-sprint-page), the `Created`, `Started`,
     and `Closed` fields of the sprint metadata datagrid, and the `created_at` and
     `updated_at` timestamps of each entry of the Comments card (see
     [Sprint Detail Sub-Template](#sprint-detail-sub-template), rules 2 and 4);
   - on the [Roadmap Audit Log Page](#roadmap-audit-log-page), the `Performed At`
     column;
   - on the [Roadmap Task Page](#roadmap-task-page), the `Created`, `Started`,
     `Tested`, and `Closed` fields of the Details card, and the `created_at` and
     `updated_at` timestamps of each entry of the Comments card;
   - on the [Roadmap Tasks Page](#roadmap-tasks-page), the `Created` column of the
     task list.

   The Roadmap Index Page, the sprint card of the Roadmap Sprints Page, and the
   cards of the sprint page's member-tasks board display no timestamp, and this rule
   adds none to them. A surface that comes to display a timestamp is governed by this rule.
9. **What this rule does not govern.**
   - **CLI output and every JSON endpoint.** The CLI's output and the graph data
     endpoint's response keep the canonical format of `DATA_FORMATS.md § Dates - ISO 8601 with UTC`. The display form is a
     presentation of a stored value in a page's text; it is not a timestamp
     Groadmap generates, stores, or writes to any data surface.
   - **Knowledge-graph property values.** A property value the graph detail panel
     shows is data a caller stored in the knowledge graph, displayed as delivered
     under [Frontend Rules](#frontend-rules), rule 6; a value that holds a date or a
     time is not reformatted (see `DATA_FORMATS.md § Graph Query Result`).
   - **Server log records.** The `time` attribute of a log record keeps the
     canonical format ([Logger Configuration](#logger-configuration), rule 5).
   - **Ordering.** Every ordering by a timestamp — the audit log's `performed_at`
     descending, the board columns ordered by `started_at` and `closed_at`, the
     comments' `created_at` ascending — compares the stored values, not the display
     form. Two timestamps within the same second display identically and keep the
     order their stored values give them.

### UI Framework

1. The web interface is built on **Tabler**, the admin-dashboard CSS and
   JavaScript framework (built on Bootstrap). Tabler provides the admin-shell
   layout — the navigation sidebar, the top navbar, page headers, and the cards,
   tables, badges, and buttons used across every page. The sidebar's per-roadmap
   links resolve to the roadmap's four views — Sprints at `/roadmaps/{name}` (the
   landing page), Tasks at `/roadmaps/{name}/tasks`, Audit at
   `/roadmaps/{name}/audit`, and Graph at
   `/roadmaps/{name}/graph` — and the sidebar highlights whichever of these is the
   active view; a task's own page highlights Tasks (see
   [Roadmap Task Page](#roadmap-task-page)). Tabler also provides the tabs used for
   the sprint presentation on the roadmap sprints page.
2. The interface uses Tabler's **dark theme**. Every page declares it with
   `data-bs-theme="dark"` on its `<html>` element, the attribute through which the
   vendored framework selects its theme, and the interface offers no theme toggle.
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
   [Self-Contained Deliverable](#self-contained-deliverable)). Inter is vendored
   as two variable-weight faces taken from one source, the
   `@fontsource-variable/inter` distribution of Inter (by Rasmus Andersson, under
   the SIL Open Font License 1.1): the upright face
   `static/vendor/inter/files/inter-latin-wght-normal.woff2` and the italic face
   `static/vendor/inter/files/inter-latin-wght-italic.woff2`. The vendored
   stylesheet `static/vendor/inter/inter.css` declares each face under both
   family names Tabler's font stack begins with, `Inter Var` and `Inter`, by an
   `@font-face` rule carrying `font-weight: 100 900`: the upright face with
   `font-style: normal`, and the italic face with `font-style: italic`. Italic
   text set in Inter is therefore drawn from the real italic face and is never a
   slant the browser synthesises from the upright one (see
   [Markdown Rendering](#markdown-rendering), rule 13). The licence of both faces
   is recorded in `static/vendor/LICENSES.md`, beside every other vendored asset.
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
   tables, pagination, badges, empty states, the navigation sidebar, the timeline,
   the progress bar —
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
    [Embedded Asset Categories](#embedded-asset-categories)). The one further
    source of styling is the syntax-highlighting stylesheet, which styles
    only the token classes chroma emits inside a highlighted code block of rendered
    Markdown (see [Markdown Rendering](#markdown-rendering), rule 7); no template
    uses those classes. In particular, the
    navigation sidebar's section label and the empty-state icon sizing carry no
    inline `style`. The sidebar's per-roadmap section label is Tabler's sidebar
    section title: an `<li class="nav-section-title">` holding the roadmap name,
    placed directly inside the sidebar's `<ul class="navbar-nav">` before the
    roadmap's own links, which is how Tabler's own sidebar menu renders a section
    label. No divider precedes it: Tabler's section title separates the section
    by its own top margin, so the sidebar carries no `dropdown-divider` and no
    other horizontal rule between the roadmap-index entry and the label. The vendored
    distribution styles `.navbar-nav .nav-section-title` inside a vertical navbar
    — small, uppercase, letter-spaced, and muted — and gives it the same
    horizontal padding it gives the sidebar's `nav-link` elements, so the label
    lines up with the links through the vendored rule alone. The label carries no
    Tabler spacing utility and no `subheader` class, and no project stylesheet rule
    positions it. Any presentational sizing, such as the empty-state
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
    the page wrapper an inline-start margin equal to the sidebar's width
    (`--tblr-sidebar-width`, `16rem`), the offset that clears the vertical sidebar, and a
    general sibling selector matches only elements that follow the `<aside>` at the
    same level. The templates MUST therefore place, inside `<div class="page">` and
    in this order: the sidebar `<aside>`, the top `<header>`, and then
    `<div class="page-wrapper">`, which holds the page header and the page body. A
    template MUST NOT nest the top `<header>` inside
    `<div class="page-wrapper">`, and the top navbar carries `d-print-none` as the
    Tabler shell does.

    The top `<header>` carries **no `navbar-expand-*` class**: its markup is
    `<header class="navbar d-print-none">`. The vendored distribution treats a
    horizontal navbar that expands at a breakpoint as the page's primary
    navigation: while the `<html>` element carries no `data-bs-navbar-position`
    attribute, its
    `.page:has(> [class*=navbar-expand]:not(.navbar-vertical))>.navbar-vertical`
    rule hides the vertical sidebar (`display: none`), and its companion rule sets
    `--tblr-sidebar-width` to `0px` on the top navbar and the page wrapper, which
    removes the offset described above. A `navbar-expand-*` class on the top
    `<header>` therefore hides the sidebar at every viewport width and places the
    content at the viewport's left edge. The top navbar holds no collapsible menu
    (rule 13), so it has no use for an expand breakpoint.

    The shell carries **no footer**. No page renders a `<footer>` element, so the
    page body is the last region inside `<div class="page-wrapper">` on every page.
13. **One sidebar collapse, one toggler, one brand.** Tabler's vertical navbar
    holds its collapsible menu region inside the sidebar `<aside>`, identified by
    `class="collapse navbar-collapse"` and `id="sidebar-menu"`, and gives that
    region exactly one `navbar-toggler`, also inside the `<aside>`. The top navbar
    of this interface carries no `navbar-expand-*` class (rule 12), so it has no
    collapsible menu and no toggler of its own. Where Tabler's
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
    navigation. The numbered pagination bar of the audit log page and that of the
    tasks page MUST therefore each sit inside a
    `<nav aria-label="...">` whose label names what the bar navigates. The
    `ul.pagination` list, its `li.page-item` items, and its `a.page-link` links stay
    exactly as specified in [Roadmap Audit Log Page](#roadmap-audit-log-page) and
    [Roadmap Tasks Page](#roadmap-tasks-page); the wrapper adds the landmark and the
    accessible name and changes no pagination behaviour.
16. **Page-header actions column.** Tabler's page-header component emits its
    actions column as `<div class="col-auto ms-auto d-print-none">`, its
    `d-print-none` matching the `d-print-none` the `page-header` element itself
    carries. Where a page header carries actions, the templates MUST use that
    column idiom, `d-print-none` included, with the one variation below.

    **On the two record pages the actions column wraps below the title on a
    narrow viewport.** The page header of the Roadmap Sprint Page and of the
    Roadmap Task Page — the two pages whose title is a record's `title`, rendered
    through the shared page-header partial (see
    [Shared Page-Header Partial](#shared-page-header-partial), rule 2) — emits its
    actions column as `<div class="col-12 col-sm-auto ms-auto d-print-none">`, the
    Bootstrap grid idiom for a column that is full-width below the `sm` breakpoint
    (`576px`) and sized to its content from `sm` up. Below `576px` the actions
    column therefore takes a row of its own, **below** the title column, and the
    title column takes the full width of the header, so a long record title is not
    squeezed beside the back link. From `576px` up the two columns share one row
    exactly as on every other page. The other pages keep `col-auto`.
17. **The fluid layout idiom is `layout-fluid` plus `container-xl`.** Tabler's
    full-width layout pairs `class="layout-fluid"` on `<body>` with ordinary
    `container-xl` page containers. The vendored stylesheet's
    `.layout-fluid .container,.layout-fluid [class*=" container-"],.layout-fluid [class^=container-]`
    selectors, which set `max-width: 100%`, exist for exactly that pairing and is what releases those containers to
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
    those sprints, its tasks, one of those tasks, its audit log, or its knowledge
    graph — and the name
    is what tells one roadmap's pages from another's at a glance. The sidebar's own
    per-roadmap section label collapses out of sight behind the off-canvas menu on
    a small viewport (rule 5), so the top navbar is the one region that names the
    selected roadmap at every viewport width.

    The name is rendered prominently and with vendored Tabler classes only: the
    name alone, carrying Tabler's `h3` type utility, inside the
    `navbar-nav flex-row` / `nav-item` idiom Tabler uses for the top navbar's own
    content. Because the top `<header>` carries no `navbar-expand-*` class (rule 12),
    the vendored `.navbar-nav` rule lays the list out as a column, and the
    `flex-row` utility is what keeps it a row. **No glyph precedes it.** An icon here would be the same on every
    page of every roadmap, so it would distinguish nothing, while the sidebar
    already gives each of the roadmap's views its own distinguishing glyph; a
    roadmap is identified by its name, which is what the URL, the sidebar label,
    and the document title all use (see [Document Title](#document-title)). A long name is truncated with Tabler's `text-truncate`
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
    no edit affordance anywhere, and no form that submits a change — the one form
    the interface renders, the tasks page's filter bar, submits by `GET` and only
    narrows what the page shows. Restating it in the
    one shell region that can instead identify the page's subject spends that
    region on what the user cannot act on. This mirrors the removal of the
    read-only footer band, whose whole content was the same restatement (rule 12).
20. **The sidebar-to-content gap is constant.** At every viewport width at which
    the vertical sidebar is shown beside the content — Tabler's `lg` breakpoint,
    `992px`, and wider — the horizontal distance from the right edge of the sidebar
    `<aside>` to the left edge of the content of the top navbar, of the page
    header, and of the page body is identical on every page. For each of those
    three regions the distance does not depend on whether the page scrolls
    vertically. No stylesheet rule,
    vendored or in the project override stylesheet, may shift the document, the
    page, the page wrapper, or any shell region horizontally by the width of the
    vertical scrollbar, or by any other length that depends on whether a scrollbar
    is present. The sidebar is fixed at the viewport's left edge and the top navbar
    and the page wrapper are offset by the sidebar's width (rule 12), so the gap is
    set by the shell alone; a gap that differs between pages, or between a page
    that scrolls and one that does not, is a defect and never an accepted layout
    variation. Below the `lg` breakpoint the sidebar collapses to the off-canvas
    menu (rule 5) and this rule does not apply.
21. **Keyboard focus is clearly visible on the header actions, the sidebar
    links, the tasks list, and the sprint board.** The back link of a record page's header actions column — `Back to
    tasks` on the Roadmap Task Page and the link back to the roadmap's sprints page
    on the Roadmap Sprint Page, both rendered in the page-header idiom of rule 16 —
    and every `nav-link` of the sidebar show a clearly visible focus indicator
    whenever they match `:focus-visible` (WCAG 2.2 Success Criterion 2.4.7, Focus
    Visible). The indicator is a solid outline at least `2px` thick drawn around
    the element, and its colour has a contrast ratio of at least 3:1 against every
    colour adjacent to it — the background the outline is drawn on and the
    element's own background — as WCAG 2.2 Success Criterion 1.4.11 (Non-text
    Contrast) requires of a visual indicator of state. An indicator that only
    changes a colour, or an outline whose colour is transparent or nearly so, does
    not satisfy this rule. Where the vendored distribution's focus styles fall
    short of it, the project override stylesheet sets the indicator on the
    element's `:focus-visible` state (rule 10); it is not shown for a pointer
    focus that does not match `:focus-visible`, and no template carries a `style`
    attribute for it.

    The same indicator, with the same thickness and the same 3:1 contrast against
    every colour adjacent to it, is required of every element that can take
    keyboard focus on the Roadmap Tasks Page and on the Roadmap Sprint Page's
    member-tasks board: on the tasks page, the filter bar's search input, its sprint
    select, its two dropdown toggle buttons, every checkbox of their menus, its
    Apply button, each row's title link, every link of the pagination bar, every link of the rows-per-page
    selector, and the Reset link of the no-match empty state (see [Roadmap Tasks Page](#roadmap-tasks-page)); on the board, every
    task card and every column toggle (see
    [Sprint Detail Sub-Template](#sprint-detail-sub-template)). The colours adjacent
    to an indicator are the background it is drawn on and the element's own
    background, so an indicator drawn around a filled control — the Apply button,
    the active rows-per-page link, the active pagination item — reaches 3:1 against
    that fill as well. The rule is judged in the dark theme the interface serves
    (rule 2).

### Full-Height Page Regions

One page presents a region sized to the **viewport** rather than to its own
content, so that the region's children scroll **inside** it and the page itself
does not scroll to reach them: the graph card of the knowledge-graph page (see
[Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page), **Graph card
layout**). It sits inside the `main.page-body` landmark of the admin shell (see
[UI Framework](#ui-framework), rule 18). It is the whole set: this subsection
introduces no region and no page, and states only how a region of this kind is
sized. The roadmap tasks page is not one of them: its task list is paginated, so
the list card is sized to the rows of one page and the page scrolls vertically
like any other (see [Roadmap Tasks Page](#roadmap-tasks-page)).

**The Roadmap Sprint Page's member-tasks board is deliberately not one of them.**
It is a board with per-column vertical scrolling, but its height is bounded by a definite length rather than by the space the page body
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
   top navbar, the page header, and the query bar (see
   [Graph Query Bar](#graph-query-bar)). Removing
   one of those elements reduces what the region gives up by that element's height,
   and adding one increases it — not as a follow-up correction to a value recorded
   somewhere, but because rule 1 is stated over the page body's edges and the page
   body has already moved. In particular the shell carries no footer and no page
   renders a `<footer>` element (see [UI Framework](#ui-framework), rule 12), so no
   full-height region gives up any space for one.
3. **A fixed subtraction from the viewport height does not satisfy rule 1.** The
   height of the material above a full-height region is not a constant. The page
   header's actions column and the query bar wrap as the viewport narrows, so they
   occupy more rows on a narrow viewport than on a wide one and the region begins
   lower. A height obtained by
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
   rules 2 and 6.
5. **A floor keeps the region usable on a very short viewport.** Below some
   viewport height the space the page body leaves is too small to present the
   region at all: the graph canvas would be a strip. Each full-height region therefore carries a
   **minimum height**, and when the space the page body leaves falls below that
   minimum the region takes the minimum instead. In that case, and only in that
   case, the region's bottom edge may fall below the viewport and the page may
   scroll vertically to reach it: rule 1's second edge yields to the floor, because
   a region compressed past legibility is worse than one the reader scrolls to.
   Rule 1's first edge yields with it wherever the page places an element above the
   region inside the same container — the knowledge-graph page's query bar (see
   rule 2). The page body is held to the same minimum as the region, so
   below the floor it carries that element and the floored region together, and the
   region's foot passes the page body's foot by the space the element occupies.
   Nothing in the stylesheet closes that gap: closing it would take a page-body
   floor of the region's floor plus the height of the element above it, and that is
   the fixed subtraction rule 3 forbids. The floor is the single exception to
   rule 1 — to both of its edges — and not a licence to overrun the viewport, or to
   end anywhere but where the page body ends, at ordinary viewport heights.

### Status, Priority, and Severity Badge Colours

Status, priority, and severity are presented as Tabler badges, and so is the task
type on the card of the sprint page's board and in each row of the tasks page's
list. The badges MUST use
**semantically meaningful** Tabler colour variants rather than a single fixed
colour, so the colour carries the meaning of the value at a glance. The mapping is
deterministic: a given enum value always maps to the same Tabler badge colour
variant, everywhere a badge for that value is shown. The badge colour variants are
Tabler's "light" badge utilities (the `bg-*-lt` classes), consistent with Tabler's
badge examples and its dark theme.

This subsection defines the only authoritative mapping. The badges use the
canonical enums already defined elsewhere and introduce no new enum value: the task
status enum, the task type enum, and the sprint status enum are defined in
`MODELS.md § Enums`, the meaning of each task type in `MODELS.md § Task Type`, the
task status lifecycle in `STATE_MACHINE.md § Task State Machine`, the sprint status
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

**Task type (`TaskType`) → Tabler badge colour:**

| Task type | Meaning | Badge variant |
|-----------|---------|---------------|
| `BUG` | Something in existing code not working as expected | `bg-red-lt` |
| `USER_STORY` | New feature from the end user's perspective | `bg-green-lt` |
| `TASK` | Internal work unit that is necessary but delivers no direct value | `bg-blue-lt` |
| `SUB_TASK` | Smaller step decomposed from a story or a task | `bg-azure-lt` |
| `EPIC` | Large body of work grouping related stories and tasks | `bg-purple-lt` |
| `REFACTOR` | Internal structure improved without changing external behaviour | `bg-indigo-lt` |
| `IMPROVEMENT` | Refinement of an existing working feature | `bg-teal-lt` |
| `SPIKE` | Research or prototyping that reduces a technical uncertainty | `bg-yellow-lt` |
| `DESIGN_UX` | Prototypes, wireframes, or interface flows | `bg-pink-lt` |
| `CHORE` | Maintenance that adds no feature and fixes no bug | `bg-secondary-lt` |

The meanings above summarise the descriptions of `MODELS.md § Task Type`, which
remain canonical.

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
   colour variant in the tables above. The task type table covers all ten
   `TaskType` values, so every task's type resolves to exactly one variant. The
   priority and severity bands together
   cover the whole `0`-`9` range with no gap and no overlap, so every valid integer
   value resolves to exactly one band.
2. **Applied consistently everywhere a badge is shown.** The mapping governs a
   badge's **colour**, and it is keyed on a value: a task status, a sprint status, a
   task type, a `priority`, or a `severity`. The same mapping is applied wherever a
   badge carries one of those values: the type, status, priority, and severity
   badges in each row of the tasks page's list (see
   [Roadmap Tasks Page](#roadmap-tasks-page)), the
   type, priority, and severity badges on the cards of the sprint detail
   member-tasks board (see
   [Sprint Detail Sub-Template](#sprint-detail-sub-template)), the Roadmap Task
   Page's header status badge, the priority and severity badges of its Details
   card, and the sprint status badge of its Sprint card (see
   [Roadmap Task Page](#roadmap-task-page)), the sprint cards (see
   [Shared Sprint-Card Partial](#shared-sprint-card-partial)), the Roadmap Sprint
   Page header (see [Roadmap Sprint Page](#roadmap-sprint-page)), the sprint
   tabs on the Roadmap Sprints Page (see [Roadmap Sprints Page](#roadmap-sprints-page)),
   and the per-column count badge of the sprint detail member-tasks board (see
   [Sprint Detail Sub-Template](#sprint-detail-sub-template)).
   A badge that carries one of those values uses the variant the relevant table above
   assigns to it, and no such badge uses a single fixed colour across differing
   values.

   **A badge carries a value in one of two ways.** The mapping applies to both. The
   second is a closed list of named cases, not a general licence; the test that
   decides membership of that list is stated below it:
   - **The badge's own text is the value.** Every site listed above except the sprint
     tabs and the sprint board's per-column count badges is this case: the badge reads
     `COMPLETED`, `OPEN`, `IMPROVEMENT`, `7`, or `2`, and it takes the colour the
     relevant table assigns to the value it carries. The type badge on the sprint
     board's card and in a row of the tasks page's list writes the `TaskType` value
     exactly as the enum spells it, with no badge label, and takes the variant the
     task type table assigns to it. On the sprint board's card and in a row of the
     tasks page's list the severity and priority badges write that value behind a
     one-letter badge label — `S2`, `P7` — which names the field the value belongs to
     (see [Sprint Detail Sub-Template](#sprint-detail-sub-template), **The card**,
     and [Roadmap Tasks Page](#roadmap-tasks-page), **Row content**). The badge
     label is a label and not a value: this mapping keys on the value alone and never
     on the label, so `P7` takes the colour of the priority `7`, `S2` takes
     the colour of the severity `2`, and no band, no colour variant, and no enum
     value changes because of it.
   - **The badge counts the members of a group with one status to key on.** Two
     sites are this case: the three sprint tabs on the Roadmap Sprints Page and the
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
   on — whether the group has that status directly, as a sprint tab does, or through
   its canonical status, as a sprint board column does. Where
   the counted group has no status at all, the badge stays neutral and this mapping
   does not govern it.

   Two kinds of count badge stay neutral under that test, and the rule above does
   not reach either. The **Comments card header count** on the Roadmap Sprint Page
   and on the Roadmap Task Page counts comments, and a comment carries no status of
   any kind (see [Sprint Detail Sub-Template](#sprint-detail-sub-template),
   **Comments card**, and [Roadmap Task Page](#roadmap-task-page)), so
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

   The mapping governs the colour of those four kinds of value only — task and
   sprint status, task type, `priority`, and `severity` — whether the badge writes the
   value that colours it or counts a group that has that value. It governs no other
   badge. The comment-type badge shown in the Comments card of the task page and of
   the sprint page is deliberately outside it and uses the neutral
   `bg-secondary-lt` variant for every type value (see
   [Roadmap Task Page](#roadmap-task-page)), and
   every count badge the discriminating test leaves out is outside it as well and
   stays governed by the section that defines it.

   **The id badge is outside the mapping.** The id badge that leads the badge line
   of the sprint board's card, and the first cell of each row of the tasks page's
   list, reads `#<id>` and carries the fixed Tabler classes
   `bg-black` and `text-white` — a black background with white text — for every
   task, whatever the task's type, status, priority, or severity. A task `id` is an
   identifier and not a value any table above knows, so it selects no colour, and no
   table above assigns `bg-black` to any value (see
   [Sprint Detail Sub-Template](#sprint-detail-sub-template), **The card**). The id
   badge therefore never shares its colour with the type, severity, or priority
   badge beside it, whatever their values: a badge that carries
   `bg-black` is the id badge, and a value badge that carried it would be outside
   every table above.

   **The task type is coloured on the sprint board's card and in the tasks page's
   list, and nowhere else.** The task page shows the task's `type` as plain text
   among its fields, not as a badge, and the filter bar of the Roadmap Tasks Page
   offers the ten values, like every other filter value, as plain options of a
   select control; neither carries a colour, and this mapping does not reach either
   (see [Roadmap Task Page](#roadmap-task-page) and
   [Roadmap Tasks Page](#roadmap-tasks-page), **Filter bar**).
3. **No new enum value.** The mapping introduces no status, task type, priority, or
   severity value that is not already defined in `MODELS.md` and `STATE_MACHINE.md`. Should a
   new enum value or a revised band be introduced there, this table is updated in the
   same change so that the mapping stays total.
4. **Faithful to Tabler.** The badge markup follows Tabler's badge example (a
   Tabler `badge` element carrying the `bg-*-lt` colour utility); the templates do
   not hand-roll a badge component (see [UI Framework](#ui-framework), rule 8).
5. **Every badge is readable: text contrast of at least 4.5:1 (WCAG 2.2 AA).**
   The text of every badge the interface renders has a contrast ratio of at least
   **4.5:1** against the badge's background, as WCAG 2.2 Success Criterion 1.4.3,
   Contrast (Minimum), requires of text. The rule covers every badge variant the
   interface renders, on every page: every variant the tables above assign to a
   task status, a sprint status, a task type, a priority band, or a severity band;
   the neutral `bg-secondary-lt` of the comment-type badge and of the neutral count
   badges; and the id badge's `bg-black` with `text-white`. It covers each badge
   kind that carries those variants — the status, type, priority, severity, and id
   badges, and the count badges of the sprint tabs, of the sprint board's columns,
   and of the Comments cards — on the tasks page's list and on the sprint board
   alike.
   - **The dark theme.** The ratio is judged in the dark theme, the only theme
     the interface serves (see [UI Framework](#ui-framework), rule 2). The vendored
     stylesheet selects the dark theme's colours through the CSS `color-scheme`
     property, which `data-bs-theme="dark"` sets to `dark`, and the `light-dark()`
     function, which resolves to its second argument under that scheme.
   - **The badge's background.** A `bg-*-lt` variant paints a translucent fill,
     so the background its text is read against is that fill composited over the
     surface beneath the badge. The ratio holds over each surface on which the
     interface renders badges: the card surface, `--tblr-bg-surface`, and the page
     background, `--tblr-body-bg`.
   - **How it is achieved.** Where a vendored variant falls short of the ratio,
     the project override stylesheet `static/style.css` redeclares that variant
     class's text colour, its fill, or both (see [UI Framework](#ui-framework), rule 10). Nothing else
     changes: the mapping of values to variants in the tables above is unchanged,
     every badge keeps the variant class the tables assign, the vendored Tabler
     files are unchanged, and no template carries a `style` attribute or an added
     class for it.

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
   container**, such as the task table on the roadmap tasks page (rule 9) or the
   member-tasks board on the roadmap sprint page (rule 10), is not page-level
   horizontal overflow and is permitted; the prohibition is on the page itself
   scrolling horizontally.
3. **Applies to every page.** The mobile-first, responsive requirement applies to
   every page: the roadmap index page, the roadmap sprints page (the sprint tabs),
   the roadmap tasks page (the filter bar and the task list), the roadmap sprint page, the
   roadmap task page, the roadmap audit log page (the audit table), and the
   knowledge-graph page.
4. **Usable tabular data on narrow screens.** The roadmap sprints page, the
   roadmap sprint page, and the roadmap audit log page present sprint and audit data
   that is tabular by nature — among it the sprint metadata datagrid and the audit
   table. This data MUST remain usable on narrow screens, for
   example through responsive or stacked tables or an equivalent layout that
   avoids page-level horizontal overflow, while still presenting the fields and
   relationships
   defined for those pages (see [Roadmap Sprints Page](#roadmap-sprints-page),
   [Roadmap Sprint Page](#roadmap-sprint-page), and
   [Roadmap Audit Log Page](#roadmap-audit-log-page)). The roadmap sprint page
   presents its member tasks as a board, which rule 10 governs, and the roadmap
   tasks page presents its tasks as a table, which rule 9 governs.
5. **Touch- and small-viewport-usable sprint tabs.** The three sprint tabs on the
   roadmap sprints page (Próximos, Actual, Concluídos) MUST remain usable on touch
   input and on small viewports. The tabs offer touch-friendly controls to switch
   between them without horizontal overflow (see
   [Roadmap Sprints Page](#roadmap-sprints-page)).
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
9. **Usable task list on narrow screens.** The roadmap tasks page presents its
   tasks as one table inside Tabler's `table-responsive` container (see
   [Roadmap Tasks Page](#roadmap-tasks-page)). When the table's columns do not fit
   the viewport, the table scrolls horizontally inside that container, and the page
   itself still does not scroll horizontally (rule 2); the horizontal scroll is
   reachable by a touch gesture. The page scrolls vertically like any other page,
   because the list is paginated and is not a full-height page region (see
   [Full-Height Page Regions](#full-height-page-regions)). The filter bar in the list
   card's header moves below the card title and stacks its controls one per line on
   a phone-sized viewport, and places several per line as the viewport widens; the
   card footer's range text, rows-per-page selector, and pagination bar wrap onto
   further lines likewise. Neither ever forces page-level horizontal overflow. The
   title link, every filter control, the Apply control, the
   rows-per-page links, and the pagination links each present a
   touch-friendly hit target.
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
   board MUST remain usable: each
   expanded column keeps a minimum width at which its cards stay legible, the
   horizontal strip scroll is reachable by a touch gesture, and the cards and their
   badges present touch-friendly hit targets that link to each task's read-only
   page (see [Roadmap Task Page](#roadmap-task-page)). Each column's collapse
   toggle likewise presents a touch-friendly hit target, and a collapsed column is a
   `3rem` strip (see [Sprint Detail Sub-Template](#sprint-detail-sub-template),
   **Column collapse**). The board's height is `60vh` with a floor read from the
   `--full-height-region-floor` custom property. Its expanded columns divide the
   board's width equally and grow with the viewport, never falling below a `17rem`
   minimum and separated by a `0.75rem` gap, so every
   length is viewport-relative or in `rem` and scales with the screen and with the
   reader's own text size rather than fixing the layout to one device (see
   [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Height and
   scrolling**).
11. **Usable task page on narrow screens.** The roadmap task page lays its cards
    out in two columns at Tabler's `lg` breakpoint (`992px`) and wider, and below
    it stacks them into one column: the Sprint card and the Details card first,
    then the four Markdown field cards, then the Comments card (see
    [Roadmap Task Page](#roadmap-task-page), **Layout**). At every viewport width
    the page produces no horizontal overflow (rule 2), including between `992px`
    and `1056px`, where the side column is narrowest: the Details card's datagrid
    has two columns below `992px` and one from `992px` up, a commit hash wraps
    rather than widening its column, a long title wraps in the page header, the
    header's actions column takes its own row below the title under `576px` (see
    [UI Framework](#ui-framework), rule 16), and rendered
    Markdown wraps within its card while a wide table or code block scrolls
    horizontally inside its own box (see [Markdown Rendering](#markdown-rendering),
    rule 13). The back link and the Sprint card's sprint link present
    touch-friendly hit targets.

## Server Logging

`rmp web` is one of two long-lived `rmp` processes, the other being
`rmp graph serve`. Every short-lived command reports its failure on stderr and
exits; the web server, by design, absorbs a per-request failure into an HTTP
status and keeps serving (see
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
   default. `rmp graph serve` is bound by the same rule for the same reason (see
   `GRAPH.md § Server Diagnostics on Stderr`), and the two surfaces MUST install
   **one** realisation of the format rather than each carrying a copy of it (see
   `DATA_FORMATS.md § Dates - ISO 8601 with UTC`, Scope).
6. The logger is a single package-level instance, built once at package
   initialisation. It is replaceable, so that a test can capture the records and
   assert their content rather than merely their presence.
7. The handler writes to that destination **directly**: nothing stands between a
   record and stderr. There is no queue, so no record is held back to be written
   later and none is dropped to make room for a newer one. What the arrangement
   costs is paid by the writer rather than by the server. A destination that has
   stopped being read — a pipe with no reader draining it — blocks the goroutine
   making the write, which for a per-request record is the goroutine serving that
   one request and for a startup record is startup itself. It blocks nothing
   else: every other request is served on its own goroutine, and the graceful
   shutdown of [Server Lifecycle](#server-lifecycle), step 7, stays bounded.
   `rmp graph serve` answers this question differently and MUST NOT be read
   across to this surface. Its records leave the goroutine that serves every
   statement, where a blocked write stops the server itself, so it interposes a
   bounded sink that drops records rather than block. That limit is that server's
   alone; this server declares none (`GRAPH.md § Server Diagnostics on Stderr`).

### Levels

| Level | Meaning | Examples |
|-------|---------|----------|
| `ERROR` | The server failed. The condition is answered with HTTP 500 and is a fault of the server or of the environment it cannot recover from. | A roadmap's database cannot be read; a page template fails to execute; a response body fails to encode; a roadmap's derived socket path is over the platform's bound, so no graph server can ever listen there. |
| `WARN` | The server did not fail, but an operator needs to know what happened. The condition is caused by the client or by the environment and leaves the server serving. | A query-bar request refused for an invalid limit or for an `EXPLAIN` or `PROFILE` prefix, or whose statement failed in the engine (HTTP 400); **a graph data request for a roadmap with no graph server running (HTTP 503)**; a roadmap skipped by the startup schema migration; the interface bound to a non-loopback address. |
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

**An HTTP 503 is recorded too, and at `WARN` rather than `ERROR`.** Every response
the graph data endpoint produces with HTTP status 503 MUST be accompanied by
exactly one `WARN` record naming the underlying error, so the condition is as
diagnosable as any other and no 503 is silent. The level follows the same split
the status does: a graph server that has not been started is a dependency awaiting
an operator, and by the reasoning that makes the answer a `503` rather than a
`500` (see
[Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
rule 1), nothing has failed. Recording it at `ERROR` would put a record on every
page load of a roadmap whose server is not running, which trains an operator to
ignore the level that is meant to mean something is broken. `ERROR` keeps its
original meaning and its original scope: a fault the server cannot recover from.

| Route or helper | Condition | Level | Status |
|-----------------|-----------|-------|--------|
| any roadmap-scoped route | the roadmap's existence check fails with an I/O error | `ERROR` | 500 |
| `GET /` | the roadmap list cannot be read | `ERROR` | 500 |
| `GET /roadmaps/{name}` | the sprints view cannot be loaded | `ERROR` | 500 |
| `GET /roadmaps/{name}/tasks` | the task list cannot be loaded | `ERROR` | 500 |
| `GET /roadmaps/{name}/audit` | the audit page cannot be loaded | `ERROR` | 500 |
| `GET /roadmaps/{name}/sprints/{id}` | the sprint cannot be loaded for a reason other than not-found | `ERROR` | 500 |
| `GET /roadmaps/{name}/tasks/{id}` | the task page cannot be loaded for a reason other than not-found | `ERROR` | 500 |
| `GET /roadmaps/{name}/graph/data` | the request's limit was invalid, its statement carried an `EXPLAIN` or `PROFILE` prefix, or its statement failed in the engine | `WARN` | 400 |
| `GET /roadmaps/{name}/graph/data` | no graph server is listening for the roadmap, or a server answered but could not be reached | `WARN` | 503 |
| `GET /roadmaps/{name}/graph/data` | the roadmap's derived socket path is over the platform's bound, or the graph cannot be reached for any other reason | `ERROR` | 500 |
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
| Requested bind address/port cannot be bound (port in use with explicit `--port`, host not assignable) | `utils.ErrIO` | 1 |
| Data directory `~/.roadmaps/` exists but cannot be read or created | `utils.ErrIO` | 1 |
| The listener stops accepting connections after the server has started, for a reason other than the graceful shutdown | `utils.ErrIO` | 1 |
| Server started and then stopped by `SIGINT`/`SIGTERM` (graceful shutdown) | — | 0 |

Rules:

1. A startup failure (invalid flag, unbindable address/port, unreadable data
   directory) terminates the process before it serves any request, with the
   plain-text error to stderr and the matching exit code above.
2. A bind failure is an I/O failure and maps to `utils.ErrIO` (exit code 1),
   which prints `I/O error: `. A listener is not a database and the line does not
   call it one; `ARCHITECTURE.md § Sentinel Error Catalogue` is canonical for the
   class and for its boundary against `utils.ErrDatabase`. The exit code is the
   one the CLI gives every I/O and database-class failure alike, so a consumer
   that branches on it sees nothing new. The error message names the host and port
   that could not be bound, and `COMMANDS.md § Web Interface` publishes it. The
   data-directory row above carries the same sentinel for the same reason: what
   could not be read or created is a directory.
3. The default-port fallback to an ephemeral port (see
   [Bind Address and Port Selection](#bind-address-and-port-selection)) means
   that, without an explicit `--port`, a busy default port does **not** cause a
   bind failure; the process binds an ephemeral port instead and starts normally.
4. Once the server is serving, per-request failures (roadmap not found, a graph
   that cannot be reached, read error) are handled inside the running server as HTTP status
   responses (400, 404, 405, 500, 503) and do **not** terminate the process. The process
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
   **The graph data endpoint is outside this rule**: it sends the statement the
   request carries to the roadmap's graph server, so graph data is written whenever
   that statement writes. **What this process itself writes to disk is nothing at
   all**: it opens no graph store, so it runs no recovery, creates no `write.lock`,
   and creates no directory. The writing and the folding of the write-ahead log are
   the server's (see
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)
   and `GRAPH.md § Durability and Checkpointing in a Long-Lived Process`).
3. **User-supplied Cypher is executed as written, and this is the interface's
   principal security property.** The graph page's query bar submits an editable
   Cypher statement to the graph data endpoint as the `q` parameter (see
   [Graph Query Bar](#graph-query-bar) and
   [Graph Data Endpoint](#graph-data-endpoint)). The endpoint does not classify it
   by what it does, does not refuse it on that ground, and executes it on the
   transactional path. The one statement it refuses, a statement carrying an
   `EXPLAIN` or `PROFILE` prefix, is refused for the answer it asks for rather than
   for what it does, and refusing it withdraws no write
   ([Query-Bar Error Handling](#query-bar-error-handling), rule 12). The
   consequences are stated plainly:
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
   - **A dedicated graph server changes where the statement runs and nothing about
     this rule.** When one is serving the roadmap, the endpoint sends the
     statement to it over a Unix domain socket instead of opening the store (see
     [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
     rule 1). That server authenticates nobody either, and the socket's `0600`
     mode protects nothing against `rmp web`, which runs as the same user that
     owns it. The reachable set is still whatever the bind address admits, and
     what it is granted is still write access to the knowledge graph.
4. **Filesystem permission model is unchanged.** The web interface reads through
   the existing locations and respects the existing permission model: `0700` for
   `~/.roadmaps/` and each roadmap home directory, `0600` for `project.db`, and
   `0700` for each `graph/` store (see `ARCHITECTURE.md § Directory Structure`).
   It creates no graph server socket and changes no socket's mode; it only
   connects to one that a `rmp graph serve` process has already created with mode
   `0600` (see `GRAPH.md § Socket Path and Permissions`).
   The web interface relaxes no permission, and it creates no roadmap database, no
   roadmap home directory, and no graph store directory. **A roadmap with no
   `graph/` directory is not served as an empty graph; it is answered `503`**,
   because the graph is reached only through a server and only `rmp graph serve`
   creates a store (see
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
   rule 1, and `GRAPH.md § Persistence Layout`, rule 2). This interface creates no
   artefact of any kind under `graph/`, the lock file included: it opens no store,
   so there is nothing for it to create.
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
7. **Output escaping, and the one renderer whose HTML is inserted.**
   Roadmap-derived text (task and sprint fields, task and sprint comment bodies,
   and graph node and edge labels and property values) that the server renders
   into a page is rendered through `html/template`'s contextual auto-escaping, so
   data that contains HTML control characters cannot alter page structure. The
   single exception is a Markdown field — the task `functional_requirements`,
   `technical_requirements`, `acceptance_criteria`, and `completion_summary`, the
   task and sprint comment `body`, and the sprint `description` — which is
   inserted as the HTML the Markdown renderer produces from it. That renderer is
   the only source of HTML the interface inserts unescaped; it emits no raw HTML
   from the source, no `style` or event-handler attribute, and no active link to a
   dangerous URL, and it causes no image request (see
   [Markdown Rendering](#markdown-rendering)). Data delivered as JSON instead —
   the graph data delivered to the visualisation — is encoded as JSON and never
   interpolated into HTML.

   Where a client script writes a value into the DOM, the server's auto-escaping
   does not protect the page, so the script MUST write every such value through
   `textContent` or an equivalent that cannot interpret markup, and MUST NOT use
   `innerHTML` or `insertAdjacentHTML`. This applies to every value the graph
   detail panel renders (see [Frontend Rules](#frontend-rules), rule 6). The tasks
   page's search term is echoed only by the server, through `html/template` (see
   [Roadmap Tasks Page](#roadmap-tasks-page), **Escaping**). No script
   writes a task's or a sprint's field into a page: those are rendered on the
   server (see [Roadmap Task Page](#roadmap-task-page)). A stored value can
   therefore alter page structure neither on the server-rendered path nor through a
   script, beyond the elements the Markdown renderer itself emits.
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
   page, the roadmap sprint page, the roadmap task page, the roadmap audit log
   page, the knowledge-graph page shell, the graph data
   endpoint, and the data-state-dependent error responses) carries
   `Cache-Control: no-store`, so no client-side or intermediary cache re-presents a
   state that no longer matches the database or store. The roadmap tasks page,
   whose response also depends on its filter-state cookie, additionally carries
   `Vary: Cookie` and never answers `304` ([Cache Policy](#cache-policy), rule 5).
   Embedded `/static/...`
   assets are immutable and are excluded from this rule, remaining cacheable (see
   [Cache Policy](#cache-policy)).
12. **No second source of truth.** The web interface stores nothing of its own.
   The CLI's SQLite databases and GoGraph stores remain the single source of
   truth, and the interface holds no cache, no index, and no derived copy of them.
   The one cookie it sets, the roadmap tasks page's filter-state cookie, is held by
   the browser, not by the server; it records a presentation choice, never roadmap
   data, and the server writes it to no database or store and keeps no copy of it
   (see [Roadmap Tasks Page](#roadmap-tasks-page), **Filter persistence**).
   The CLI is the sole write path for roadmap data. It is **not** the sole write path
   for a knowledge graph: the graph data endpoint sends its statement to the same
   graph server `rmp graph client` sends to, which writes into the one GoGraph
   store, so the store stays the one place a graph lives (see
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
   an inline member-tasks board, using the fields and
   relationships defined in `MODELS.md` and `DATABASE.md`. The page does **not**
   render the roadmap's task list, and it contains no form, button, or link that
   submits a change.
9. `GET /roadmaps/{name}/tasks` for an existing roadmap returns HTTP 200 and an
   HTML page that renders the roadmap's tasks, of any status, as **one list** — one
   Tabler card holding one table, with one row per task — using the fields and
   relationships defined in `MODELS.md` and `DATABASE.md`. This is a distinct endpoint
   from the sprints page. The page renders no board, no column per status, and no card
   per task, and a task's full field set is reached through the task page each row
   links to. The page's only form is its filter bar, which submits by `GET` and changes
   no data; the page contains no form, button, or link that submits a change.
   `GET /roadmaps/{name}/tasks` for a non-existent roadmap, or a request whose `{name}`
   violates the roadmap-name rules, returns HTTP 404 without touching the filesystem
   outside `~/.roadmaps/`. Acceptance Criteria 81, 84 to 88, 90 to 93, 128, 232, 233,
   and 244 define the list itself; Acceptance Criteria 100 to 107, 112 to 119, 121,
   122, 129, 152 to 155, 234 to 238, 243, and 248 define the filter bar and the search;
   Acceptance Criteria 249 to 254 define the filter-state cookie, the defaults, and
   the route's caching;
   Acceptance Criteria 82, 83, 89, 239 to 242, and 245 define the pagination;
   Acceptance Criteria 246 and 247 fix the contrast of the badges, of the search
   placeholder, and of the focus indicator; and Acceptance Criteria 94 to 99 and 221 to 231 fix the task page the
   rows link to.
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
    tabs and is not expanded into an inline member-tasks board. A tab
    with no matching sprint shows a clear empty-state message.
13. On the roadmap sprints page, every sprint card in any tab shows a header
    presenting the sprint `title` together with `Sprint #<ID>` and a status badge,
    the sprint description, and a footer with that sprint's total task count, and
    is a clickable link to that sprint's page at
    `/roadmaps/{name}/sprints/{id}`.
14. `GET /roadmaps/{name}/sprints/{id}` for a sprint of an existing roadmap returns
    HTTP 200 and an HTML page showing the details of that sprint and the sprint's
    member tasks as a three-column board whose
    `WAITING` column follows the `sprint_tasks` order (the planned
    in-sprint execution order) while its `DOING` and `CLOSED` columns lead with the
    most recently started and the most recently closed task respectively; the page
    header presents the sprint `title`
    alongside `Sprint #<ID>` and the sprint's status badge; the Sprint details card
    shows the sprint description and a metadata datagrid of exactly three fields,
    in the order `Created`, `Started`, and `Closed`, with an em dash in place of an
    unset `started_at` or `closed_at`; the datagrid carries no `ID`, `Title`,
    `Status`, `Order`, `Capacity`, or `Tasks` field, and the page shows neither the
    sprint's execution `order` nor its capacity `max_tasks`; the page contains no
    form, button, or link that submits a change. A request whose `{id}` is not a
    valid integer, or is an integer that is not a sprint of the named roadmap, returns HTTP 404, and a
    request whose `{name}` is invalid or nonexistent returns HTTP 404.
15. Following a task's link anywhere one is shown — the cards of the sprint page's
    board, and the title of each row of the tasks page's list —
    navigates to that task's page at `/roadmaps/{name}/tasks/{id}`, which displays all
    of that task's fields (`id`, `title`, `status`, `type`, `priority`, `severity`,
    `functional_requirements`, `technical_requirements`, `acceptance_criteria`,
    `completion_summary`, `parent_task_id`, `subtask_count`, `depends_on`, `blocks`,
    `created_at`, `started_at`, `tested_at`, `closed_at`, `commit_open`,
    `commit_close`) and that task's comments, in the HTML the server sends. The page is
    read-only: it contains no form, no edit control, and no submit action, and it opens
    no write path. The link is followed from the pointer, from touch, and from the
    keyboard on both pages, and the task page and the sprint tabs are usable on touch
    input and on a small phone-sized viewport (see
    [Roadmap Task Page](#roadmap-task-page)).
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
20. Serving roadmap sprints pages, roadmap tasks pages, roadmap sprint pages,
    roadmap task pages, and roadmap audit log pages
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
    page, the roadmap tasks page, the roadmap sprint page, the roadmap task page,
    the roadmap audit log page, and the knowledge-graph
    page each render without page-level horizontal scrolling — `<body>` produces no
    horizontal overflow — with readable typography and
    touch-friendly hit targets, demonstrating the mobile-first base styles. The
    horizontal scroll the tasks page's table performs inside its `table-responsive`
    container is not page-level overflow and does not violate this criterion (see
    Acceptance Criterion 128), and neither is the horizontal scroll the sprint page's
    member-tasks board performs inside its own container (see Acceptance
    Criterion 136).
28. On the roadmap sprints page, the roadmap sprint page, and the roadmap audit
    log page at a narrow viewport, the sprint and audit data remains usable without
    page-level
    horizontal overflow (for example through responsive or stacked tables or an
    equivalent layout) while still showing the fields and relationships defined for
    those pages. The roadmap sprint page presents its member tasks as a board, whose
    narrow-viewport behaviour Acceptance Criterion 136 covers, and the roadmap tasks
    page presents its tasks as a table, whose narrow-viewport behaviour Acceptance
    Criteria 128 and 129 cover.
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
32. Multi-line free-text that the interface shows as plain text renders preserving
    its source line breaks: a property value shown in the knowledge-graph detail
    panel displays the author's newlines rather than collapsing them, while the text
    still wraps without forced horizontal scrolling and is written as text, never as
    markup (see [Frontend Rules](#frontend-rules), rule 6). The seven Markdown fields
    — the task page's long free-text fields (`functional_requirements`,
    `technical_requirements`, `acceptance_criteria`, and `completion_summary`),
    every comment `body` shown in the task page's Comments card and in the sprint
    Comments card, and a sprint's `description` on the roadmap sprints page (across
    all three tabs) and on the roadmap sprint page — are not plain text: each renders
    as Markdown, and its authored single newlines render as line breaks (Acceptance
    Criterion 180).
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
    (`/roadmaps/{name}/sprints/{id}`), the roadmap task page
    (`/roadmaps/{name}/tasks/{id}`), the roadmap audit log page
    (`/roadmaps/{name}/audit`), the knowledge-graph page shell
    (`/roadmaps/{name}/graph`), the graph data endpoint
    (`/roadmaps/{name}/graph/data`), and the data-state-dependent error responses
    (for example a `404` for a missing roadmap, sprint, or task and a `500` from a
    read failure). A response for a `/static/...` asset does **not** carry
    `Cache-Control: no-store` and remains cacheable (see
    [Cache Policy](#cache-policy)).
38. On the roadmap sprints page, every sprint in every tab — Próximos, Actual, and
    Concluídos — is rendered through the single shared sprint-card partial, so all
    sprints use identical card markup. The OPEN sprint under the Actual tab is shown
    with the same card as the other sprints: it shows the header (`Sprint #<ID>`
    with a status badge), the sprint description, and the footer task count, and it
    is not expanded into an inline sprint metadata datagrid or member-tasks board
    on the sprints page. The full sprint detail block (Sprint
    details card with its metadata datagrid, member-tasks board, and Comments card) is shown
    only on the single Roadmap Sprint Page (see
    [Shared Sprint-Card Partial](#shared-sprint-card-partial) and
    [Sprint Detail Sub-Template](#sprint-detail-sub-template)).
39. **The single sprint page renders no sprint status summary line.** The
    served HTML of the Roadmap Sprint Page carries no element with
    `data-role="sprint-summary"` and no text matching the pattern
    `<pct>% - P:<p> A:<a> C:<c> - T:<t>` (for example `33% - P:8 A:29 C:18 - T:55`,
    or any text of the form `<n>% - P:`). The sprint presentation opens with the
    Sprint details card, directly below the page header (see
    [Sprint Detail Sub-Template](#sprint-detail-sub-template), rule 2).
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
    `rmp graph client` invocation against the same running server afterwards
    reports the `WebProbe` node present; a request whose `q` is
    `MATCH (n:WebProbe) DETACH DELETE n` is answered HTTP `200`, and the same
    read-back afterwards reports it gone. The store's durable state is asserted
    after the server has been stopped, because the fold is the server's and not the
    request's: `snapshot/manifest.json` exists and the `wal` file is short.
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
    message on the page, distinct from the invalid-limit message. In each of
    these two query-bar failure cases — invalid limit and execution failure — the
    message is shown in place, the page does not crash, and the failure triggers no
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
    it is shown — the status, priority, and severity badges in the rows of the tasks
    page's list and the priority and severity badges on the cards of the sprint detail
    member-tasks board, the task page (its
    header status badge, the priority and severity badges of its Details card, and
    the sprint status badge of its Sprint card), the sprint cards, the Roadmap Sprint Page header, the sprints-page
    tabs, where the colour is the variant of the status
    the tab groups while the badge text is that tab's sprint count (Acceptance
    Criterion 120), and the per-column count badge of the sprint's member-tasks board,
    where the colour is the variant of the status the column groups while the badge
    text is that column's task count (Acceptance Criterion 140) — and the mapping
    introduces no enum value beyond those defined in `MODELS.md` and
    `STATE_MACHINE.md`.
62. No template carries a presentational inline `style="..."` attribute: all styling
    is provided by vendored Tabler classes and utilities or by the project override
    stylesheet (`static/style.css`). In particular, the navigation sidebar's
    per-roadmap section label is an `<li class="nav-section-title">` placed directly
    inside the sidebar's `<ul class="navbar-nav">` and preceded by no divider (the
    sidebar contains no `dropdown-divider` and no `<hr>`), carrying neither the
    `subheader` class nor a Tabler spacing utility, rather than an inline-styled
    label; in a browser at a viewport width of `992px` or wider, the left edge of
    the label's text equals the left edge of the icons of the roadmap's sidebar
    links (both are inset by the same vendored padding), and the empty-state icon's
    sizing lives in a Tabler utility class or in `static/style.css` rather than in an
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
64. The task page renders the task's comments as a timeline in its Comments card,
    placed after the `Completion summary` card and last in the main column. For a
    task with comments, the card contains a `<ul class="timeline">` whose
    `<li class="timeline-event">` items appear oldest first, in the same order
    `rmp task comment-list` returns for that task, and every comment of the task is
    present — no type filter and no count limit — and the card header titled
    `Comments` carries a badge with the comment count (see
    [Roadmap Task Page](#roadmap-task-page)).
65. Each timeline entry shows the comment's type as a badge, its `created_at`
    timestamp, its `body` rendered as Markdown (Acceptance Criterion 180), and —
    only when `updated_at` is not null — the `updated_at` timestamp marking the
    entry as edited. A comment whose `updated_at` is null shows no edited marker.
66. The comment type badge uses the neutral `bg-secondary-lt` variant for all seven
    type values, in both the task page's Comments card and the sprint Comments card. No
    per-type colour is introduced, and the semantic mapping in
    [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours)
    is unchanged (Acceptance Criterion 61 continues to hold).
67. The page of a task with no comments renders its Comments card with a clear
    empty-state message in place of the timeline, not an empty list and not a
    missing card.
68. The Roadmap Sprint Page renders a Comments card after the member-tasks board,
    as the last card of the sprint detail sub-template. It shows the sprint's own
    comments oldest first, in the same order `rmp sprint comment-list` returns, with
    a card header titled `Comments` carrying a badge with the comment count. A sprint
    with no comments still renders the card, showing an empty-state message in place
    of the timeline (see [Sprint Detail Sub-Template](#sprint-detail-sub-template)).
69. The sprint Comments card shows only the sprint's own comments. A comment written
    against a member task appears on that task's own page and nowhere in the
    Comments card, and no aggregate of task comments is presented at sprint level.
70. Rendering a page that shows N tasks never issues one comment query per
    task: an instrumented count of comment queries is independent of N on every such
    page. On the tasks page the count is 0, because the list shows no comment
    information. On the sprint page it
    is 2, whatever N is: one listing query for that sprint's **own** comments, which
    the Comments card renders in full as a log (see `DATABASE.md § Comments`), plus
    one grouped **counting** query over the sprint's member tasks, selected by the
    sprint id, which is what gives each board card its comment number (see
    `DATABASE.md § Count Comments for Many Parents (Grouped)`). The sprint page issues no
    comment-listing query for a member task, so it reads the comment **body** of no
    task it renders. A page that renders no task issues no task-comment query of
    either kind: a sprint with no member task skips the grouped count entirely, while
    still issuing the sprint's own comment listing, because the Comments card is
    always present. A task's comment bodies are read only by that task's own page,
    one task at a time (see [Roadmap Task Page](#roadmap-task-page)).
71. The comments timeline uses only the Tabler Timeline classes already present in
    the vendored `tabler.min.css` (`timeline`, `timeline-event`,
    `timeline-event-icon`, `timeline-event-card`). The feature adds no CSS file, no
    JavaScript file, and no vendored asset, and no template carries a presentational
    inline `style` attribute for it (Acceptance Criterion 62 continues to hold).
72. Neither the task page's Comments card nor the sprint Comments card contains a form, a
    button, or a link that submits a change, and the only input either contains is
    the disabled checkbox of a rendered Markdown task-list item, which can be
    neither checked nor unchecked (see [Markdown Rendering](#markdown-rendering),
    rule 3). There is no route, no endpoint, and no client-side path through which
    the web interface can create, edit, or delete a comment; the CLI remains the
    sole write path.
73. A comment body is rendered through the Markdown renderer and no other way: a
    comment body containing raw HTML — for example `<script>`, `<iframe>`, or
    `<img onerror=...>` — introduces none of those elements and none of their
    attributes into the page, and a `<` or `&` that is not raw HTML renders as the
    character itself, on the task page and in the sprint Comments card alike. The comment can
    add to the page no element but those the renderer emits (see
    [Markdown Rendering](#markdown-rendering), rules 10 and 11, and
    [Security and Constraints](#security-and-constraints), rule 7).
74. Every page's admin shell places, inside `<div class="page">` and in this order,
    the sidebar `<aside>`, the top `<header class="navbar d-print-none">`, and
    `<div class="page-wrapper">`, which holds the page header and the page body. The
    top `<header>` is a sibling of `<div class="page-wrapper">` and
    is never nested inside it, which is the shape the vendored stylesheet's
    `.navbar-vertical~.navbar` and `.navbar-vertical~.page-wrapper` offset rules
    require. The top `<header>` carries no class beginning with `navbar-expand`,
    and in a browser at a viewport width of `992px` or wider the sidebar `<aside>`
    is displayed (its computed `display` is not `none`) and the page wrapper's
    left edge lies at the sidebar's right edge. No page renders a `<footer>` element: the page body is the last region
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
    `<div class="col-auto ms-auto d-print-none">`, except the Roadmap Sprint Page
    and the Roadmap Task Page, whose actions column is
    `<div class="col-12 col-sm-auto ms-auto d-print-none">` (Acceptance
    Criterion 230; see [UI Framework](#ui-framework), rule 16).
79. Every page carries `class="layout-fluid"` on `<body>` and uses `container-xl` for
    its shell containers: the top navbar, the page header, and the page body. No
    page container uses `container-fluid`; the only `container-fluid` in
    the shell is the one inside the sidebar `<aside>`, which is Tabler's own
    vertical-navbar markup (see [UI Framework](#ui-framework), rule 17).
80. Every page renders its page body as `<main class="page-body">`, the element
    Tabler's built admin shell uses, so each page exposes exactly one `main`
    landmark holding that page's own content. No page renders the page body as a
    `<div>` (see [UI Framework](#ui-framework), rule 18).
81. The roadmap tasks page presents its tasks as **one list and nothing else**: the
    served HTML carries exactly one list card — a `card` holding one
    `table-responsive` container whose one table carries the classes `table`,
    `table-vcenter`, and `card-table` — and one `<tbody>` row per task of the rendered
    page. The list is not divided by status or by any other attribute: tasks of all five
    `TaskStatus` values share the one table, and a task's status is a cell of its row.
    The page carries no board, no column per status, no card per task, no element with
    a class beginning `task-board`, and no second list card, whatever the roadmap's
    data contains (see [Roadmap Tasks Page](#roadmap-tasks-page)).
82. Every task of the roadmap that satisfies the request's accepted criteria
    appears in the list exactly once across its pages, and no other task appears: for
    a request whose criteria admit N tasks, the pages of the list at any page size
    together carry exactly N rows, no task is omitted, no task appears on two pages,
    and a task whose status or sprint changes is admitted or excluded accordingly on the
    next request. This holds for every N, with no upper bound: the page's task read is
    bounded by the filters alone and never by a page, and the `rmp task list` display
    default of `100` is not applied to it. For a roadmap holding more than 100 tasks,
    requested with no criterion, the range text states the roadmap's full task count and
    the pages together carry every task (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Pagination**, and
    `DATABASE.md § Main SQL Queries`, "List All").
83. The card footer states `Showing <a> to <b> of <total> entries`, where `<total>`
    is the number of tasks satisfying every accepted criterion of the request — the
    filtered total, not the roadmap's — and `<a>` and `<b>` are the positions of the
    first and last rows the page shows. For 60 matching tasks at page size `25`, page 1
    reads `Showing 1 to 25 of 60 entries` and page 3 reads
    `Showing 51 to 60 of 60 entries`; applying a filter that admits 7 of them reads
    `Showing 1 to 7 of 7 entries`. Each of the three numbers is held in its own
    `<span>` inside a `<p class="m-0 text-secondary">` (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Pagination**).
84. The rows appear in one deterministic order over the whole filtered set:
    descending `priority`, then ascending `created_at` for tasks of equal priority,
    then ascending `id` for tasks equal on both — the default `ListTasks` ordering
    (`ORDER BY priority DESC, created_at ASC`; see `DATABASE.md § Main SQL Queries`,
    "List All") with `id` as its final key. The check's data MUST include tasks of equal
    `priority` and equal `created_at`, so that an order lacking the `id` key cannot pass
    by coincidence, and MUST assert that concatenating the rows of every page in page
    order yields exactly that order. Filtering and searching remove rows from the order
    and never reorder the rows that remain.
85. Each row of the tasks page's list shows, in this order, one cell per column
    headed `ID`, `Title`, `Type`, `Status`, `Severity`, `Priority`, and `Created`,
    and no other: the id badge reading `#<id>` with the classes `bg-black`
    and `text-white`; the task `title` as a link to the task page; the type badge
    reading the task's `type` exactly as the `TaskType` enum spells it and coloured by
    the task type mapping; the status badge reading the task's `status` exactly as the
    `TaskStatus` enum spells it and coloured by the task status mapping; the severity
    badge reading
    `S` immediately followed by the task's severity; the priority badge reading `P`
    immediately followed by the task's priority, with no colon, no space, and no other
    separator between the letter and the digits; the task's `created_at` in the display
    form `YYYY-MM-DD HH:mm:ss` inside a `<time>` element whose `datetime` holds the stored
    value, preceded by `<i class="ti ti-calendar me-1" aria-hidden="true"></i>`, in a cell
    carrying `text-secondary`. The table has no `Sprint` column and no `Actions`
    column, and no row carries a `View` link. A badge reading `3`, `S 3`, `S:3`, `Sev:3`, or `Sev: 3` (or the
    corresponding `Pri:` form) does not satisfy this criterion. The severity and
    priority badges are coloured by the mapping applied to the value alone, so the badge
    label changes no badge colour (Acceptance Criterion 61 continues to hold). The row
    carries no comment count, no subtask count, no dependency count, no sprint, and no
    other field (see [Roadmap Tasks Page](#roadmap-tasks-page), **Row content**).
86. Each row carries exactly one link, the title: an `<a>` element whose `href` is
    `/roadmaps/{name}/tasks/{id}` of the row's own task and whose accessible name is
    its visible text. No row carries a `View` link or any other link, so tabbing
    through the table stops once per row, on its title. A pointer click, a touch tap,
    and Enter each follow the link, and a middle click opens the task page in a new
    tab, with no JavaScript added to make any of it work. Following the link issues no
    request from the tasks page itself and reaches no write path. The link shows a
    visible focus indicator when it receives keyboard focus, measured in a browser as a
    computed `outline` on the focused link that the unfocused link does not carry
    (Acceptance Criterion 247 fixes its contrast; see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Links to the task page**).
87. The tasks page is read-only. The table carries no selection checkbox, no
    `table-selectable` class, and no form control of any kind; the list card carries no
    add-task button; the page renders no element carrying the class `modal` and no
    element carrying `data-bs-toggle="modal"`. The filter bar's form carries the method
    `get`, and every other control on the page is a `GET` link, so no request the page
    can issue changes any data, and the `rmp` CLI remains the sole write path (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Read-only**).
88. When no task satisfies a request, the list card keeps its header and its filter
    bar, renders no table and no card footer, and shows Tabler's empty-state markup
    (`<div class="empty">`) in a `card-body`, never a page-level empty state. A roadmap
    with no task shows the empty-state title
    `No tasks yet` and no Reset link, whatever the active filter state — no
    criterion, URL filters, cookie filters, or the defaults; a roadmap holding at
    least one task, none of which satisfies the active criteria, shows the title
    `No task matches the filters` and, in the empty state's
    `empty-action`, a link carrying `btn` and reading `Reset`, whose `href` is the
    page's path carrying exactly the four default status values — `status=BACKLOG`,
    `status=SPRINT`, `status=DOING`, and `status=TESTING` — and no `sprint`, no
    `type`, no `q`, and no `page`, and with `size` only
    when the active page size is not `25`. This link is the page's only Reset control:
    the filter bar carries none. Both answer HTTP 200
    (see [Roadmap Tasks Page](#roadmap-tasks-page), **Empty states**).
89. Rendering the tasks page issues exactly two reads — one read of the roadmap's
    sprints and one read of the roadmap's tasks through the task listing — and no
    sprint-resolution query, plus exactly one count of the roadmap's tasks
    (`DATABASE.md § Count Roadmap Tasks`) when, and only when, the filtered list is
    empty and the task read carried at least one sprint, status, or type predicate.
    A request whose list holds a row issues no count, and neither does an empty list
    whose task read carried no predicate. An instrumented count of queries is the same for a roadmap of 10
    tasks and one of 300, for every page and every page size, and for any number of
    active filters; no query is issued per row, per page, or per filter, and no
    comment is read. The search term, the total, and the selection of the page's rows
    are computed in memory over the rows the task read returned (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Read cost**,
    `DATABASE.md § List Sprint Titles`, and `DATABASE.md § List All`).
90. The tasks page's markup obeys the rules already in force and introduces no
    exception: no template carries a presentational inline `style` attribute, every
    class the page emits is defined either in the vendored Tabler distribution or in
    the project override stylesheet `static/style.css` (Acceptance Criterion 62
    continues to hold), and the page uses Tabler's own components — form controls, the
    card, the table, badges, the button group, pagination, and the empty state —
    without hand-rolling any of them. The page's admin shell and page header are
    unchanged, and the page header's actions column carries nothing on this page
    (Acceptance Criteria 74 to 76 and 78 to 80 continue to hold; see
    [UI Framework](#ui-framework), rules 8 and 10).
91. The tasks page shows no task's sprint: its table has no `Sprint` heading and no
    cell holding a `Sprint #<id>` text or an em dash for a task in no sprint, for a
    roadmap whose tasks belong to several sprints and to none. Task-to-sprint
    membership reaches the page only through the sprint filter (Acceptance
    Criterion 235), and the sprint of a task is shown on that task's page (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Row content** and **Relationships
    shown**).
92. The tasks page issues no sprint-resolution query: an instrumented count of
    queries through `DATABASE.md § Resolve the Sprint of Many Tasks (Grouped)` is 0 for
    a tasks page rendering N rows, for every N, every page size, every filter, and
    however many distinct sprints the rendered tasks belong to. The sprint filter is
    applied by the task listing's own predicates (Acceptance Criterion 117). This is
    measured the same way Acceptance Criterion 70 measures the comment-query count.
93. Every task card of the Roadmap Sprint Page's member-tasks board is, in the
    served HTML, an `<a>` element with an `href` to its own task's page, and no task
    card is a `<button>`, a `<div>`, or a `<tr>`. No element carrying `role="button"`
    or `tabindex` stands in for a card or for a row of the tasks page's list, and no
    `<tr>` on any page carries an `href`, a `role`, a `tabindex`, or an event-handler
    attribute; the tasks page's rows reach the task page only through the title link of
    Acceptance Criterion 86. Each card's accessible name is
    `Open details for task #<id>: <title>`, carrying the task's `id` and its `title`.
    In particular the name contains the task title, which is the card's visible label,
    so the accessible name contains the visible label text, as WCAG 2.5.3 Label in Name
    (Level A) requires, and the card can be followed by speech input by speaking the
    title that is displayed. An accessible name carrying the `id` alone, such as
    `Open details for task #<id>`, does not satisfy this criterion. When a card receives
    keyboard focus it shows a visible focus indicator (WCAG 2.2 Success Criterion
    2.4.7, Focus Visible), measured in a browser as a computed `outline` or `box-shadow`
    on the focused card that the unfocused card does not carry. The property holds
    without any JavaScript being added: the Content-Security-Policy of Acceptance
    Criterion 33 is unchanged (see
    [Sprint Detail Sub-Template](#sprint-detail-sub-template), **The card is a link to
    the task page**, and [Roadmap Tasks Page](#roadmap-tasks-page), **Links to the task
    page**).
94. `GET /roadmaps/{name}/tasks/{id}` for a task of an existing roadmap returns
    HTTP 200 and an HTML page — for a task of each of the five statuses — whose
    served HTML, before any script runs, already carries every field Acceptance
    Criterion 15 lists and every comment of the task: the page reads the same with
    scripting disabled, and it makes no request after it loads other than for
    assets under `/static/`. `HEAD` of the same path returns HTTP 200 with the
    headers of the `GET` and no body (see
    [Roadmap Task Page](#roadmap-task-page)).
95. The task page enforces the same path-parameter discipline as every other
    roadmap route: a request whose `{name}` violates the roadmap-name rules, or
    names a roadmap that does not exist, returns HTTP 404 without touching the
    filesystem outside `~/.roadmaps/`; a non-integer `{id}` returns HTTP 404; and an
    integer `{id}` that is a task of some other roadmap, or of no roadmap, returns
    HTTP 404 rather than that task's page. The route serves `GET` and `HEAD` only
    and answers any other method with HTTP 405. Every response of the route, its
    404 included, carries `Cache-Control: no-store` (Acceptance Criterion 37
    continues to hold) and the security headers of Acceptance Criterion 33.
96. **The interface has no task modal, no task detail script, and no task JSON.**
    The served HTML of the roadmap tasks page and of the Roadmap Sprint Page
    contains no element carrying the class `modal` and no element carrying
    `data-bs-toggle="modal"`, and neither page loads a script whose purpose is to
    show a task. `GET /static/task-modal.js` returns HTTP 404, because no such asset
    is in the embedded set. `GET /roadmaps/{name}/tasks/{id}/data` for a task of an
    existing roadmap returns HTTP 404 and no JSON body, as a path no route matches,
    and so does every other path below `/roadmaps/{name}/tasks/{id}` (see
    [Routes and Pages](#routes-and-pages), rule 5).
97. Every value the task page shows is escaped by `html/template`, except the HTML
    the Markdown renderer produces for the four Markdown fields and for each
    comment `body`. A task whose `title` contains HTML markup renders that markup as
    visible characters in the page header and in the document title, and a task
    whose requirement free-text, `completion_summary`, or comment `body` contains
    raw HTML renders through the Markdown renderer's output, which carries none of
    that raw HTML, so no element, no attribute, and no script of the author's
    reaches the page. This is proven by a test that fails if a hostile value reaches
    the page as markup, covering at least a hostile task title, a hostile requirement
    field, and a hostile comment body (see
    [Roadmap Task Page](#roadmap-task-page), **Rendered on the server, complete**,
    and [Security and Constraints](#security-and-constraints), rule 7).
98. The task page loads no script of its own and carries no inline script and no
    inline event-handler attribute; every script it loads comes from `/static/`, and
    its Content-Security-Policy is exactly the value fixed in Acceptance
    Criterion 33. The page makes no request to any origin but its own (Acceptance
    Criteria 23 and 33 continue to hold).
99. When the task page's read fails for a reason other than not-found — for
    example a roadmap database that cannot be read — the route returns HTTP 500
    with no detail of the failure in the response, and the server writes exactly one
    `ERROR` record naming the underlying error (see
    [What Is Logged](#what-is-logged)). A request for a task that does not exist
    writes no record (see [What Is Not Logged](#what-is-not-logged)).
100. The roadmap tasks page's page header carries **no** actions column content: no
    search input, no filter control, and no knowledge-graph link. The graph stays
    reachable from this page through the admin-shell sidebar's Graph entry, which every
    page carries (Acceptance Criterion 16 continues to hold). The search input is
    `<input type="search" class="form-control form-control-sm" name="q" placeholder="Search">`,
    inside the filter bar of the list card's header, with a `<label>` associated with
    it by `for` and `id`, reading `Search` and carrying `visually-hidden` — the
    `placeholder` shows what the control is and does not replace that label — and it
    is reachable and operable from the keyboard (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Filter bar**).
101. A request carrying a term in `q` lists exactly the tasks whose `title` contains
    the term or whose `#<id>` reference, written with the leading `#`, contains it,
    case-insensitively and as a substring; both `42` and `#42` therefore find task 42.
    The title and the reference are matched **separately**, never as one concatenated
    string: for a task whose title ends in `Lisboa`, a term made of that title's last
    characters followed by the start of the task's reference, with or without a space
    between them, does not match that task.
    No other task field is matched: a term occurring only in a task's
    `functional_requirements`, or only in its `type`, and matching nothing in that
    task's title or reference, does not match it. Leading and trailing whitespace is
    stripped from the term by the rule Acceptance Criterion 121 fixes, and a term that
    is empty after that trim is no criterion and lists every task. The comparison
    normalises the term and the task's searchable text by the rule Acceptance
    Criterion 152 fixes and folds them by the rule Acceptance Criterion 118 fixes, on
    the server, so the verdict does not depend on the browser, on its reported locale,
    or on its Unicode version.
102. A term that matches no task, alone or together with the filters, renders the
    no-match empty state of Acceptance Criterion 88 — one message covering the term and
    the filters together — and that state is distinct from the state of a roadmap that
    holds no task at all, which shows `No tasks yet` whatever the criteria (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Empty states**).
103. The term travels in the `q` URL query parameter on `/roadmaps/{name}/tasks`.
    Submitting the filter bar puts the search input's value in `q`, and reloading the
    resulting URL renders the identical list. The search input of every response shows
    the active `q` — the one the request carried, or, for a request carrying none of
    the six parameters, the one its filter-state cookie carried — and every link the
    page generates to itself carries
    `q` while the term is not empty after the trim, and carries no `q` otherwise. The
    `q` a generated link carries is the term as echoed: for a `q` holding an invalid
    UTF-8 byte, the link carries `U+FFFD` in its place, percent-encoded as `%EF%BF%BD`,
    and never the invalid byte.
104. The tasks page's list is **rendered on the server**, and scripts only open the
    dropdowns. For any roadmap, any combination of the six parameters, and any
    filter-state cookie, the HTML the server sends already carries the final list —
    the rows, the range text, the pagination bar, the selected option of the sprint
    select, the `checked` boxes and the text of each dropdown toggle, and the empty
    state where applicable — and no script changes any of it after load: the page
    loads no project script of its own, its one script being
    `/static/vendor/tabler/tabler.min.js`. The check compares, for the same URL and
    the same cookie, the list a browser renders with scripting enabled and the one
    it renders with scripting disabled, and they are identical; with scripting
    disabled, the search, the sprint select, Apply, the pagination links, and the
    rows-per-page links still work, and submitting the form still carries the boxes
    the server marked `checked`. With scripting enabled, activating a dropdown's
    toggle opens its menu, checking a box leaves the menu open and submits nothing,
    and the form is submitted only by Apply or by Enter in the search input.
    Requesting the same URL twice, with the same cookie and the roadmap unchanged,
    returns the same list.
105. No `q` value produces an error page: a term matching nothing, a term longer
    than any searchable text, and a `q` the server cannot decode each return HTTP 200,
    the last treated as though `q` were absent. A term whose bytes are not valid UTF-8
    has each invalid byte replaced by `U+FFFD` and is then matched like any other term,
    and the search input's `value` echoes that term with the same replacement: the
    served HTML carries no invalid UTF-8 byte, and a `q` holding one invalid byte
    between two ASCII letters is echoed as those letters with one `U+FFFD` between
    them.
    Applying a term adds no database query: the page's reads remain those of
    Acceptance Criterion 89, and `q` never reaches SQL. A task's searchable text and
    the term are both prepared by the server alone (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **The text search**).
106. A term containing HTML markup renders as visible characters and introduces no
    element, attribute, or script into the page: the server escapes it through
    `html/template` where it echoes it into the search input's `value`, and no script
    writes it anywhere. Every generated link percent-encodes the parameter values it
    carries. This is proven by a test that fails if the term is written as markup
    (rule 7 of [Security and Constraints](#security-and-constraints) governs).
107. The tasks page introduces no inline script, loads no project script of its
    own, and makes no Content-Security-Policy change: the only script it loads is the
    vendored `/static/vendor/tabler/tabler.min.js`, and the policy remains exactly the value fixed in Acceptance
    Criterion 33 (Acceptance Criteria 23 and 98 continue to hold). Every class the
    filter bar, the table, and the footer emit resolves in the embedded stylesheets and
    no template carries a `style` attribute (Acceptance Criterion 62 continues to
    hold).
108. The top navbar of every roadmap-scoped page — the roadmap's sprints page, a
    sprint's own page, the tasks page, a task's own page, the audit log page, and
    the knowledge-graph page — shows the name of the roadmap in the request path, rendered prominently
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
    `Knowledge graph`, and — on a sprint's own page — that sprint's `title` alone,
    under the pretitle `Sprint #<ID>` followed by the sprint's status badge, and —
    on a task's own page — that task's `title` alone, under the pretitle
    `Task #<ID>` followed by the task's status badge; neither record page's title
    contains a badge, and no other page's header carries one.
    No header title contains the roadmap name, which the shell already states in
    the sidebar and in the top navbar. Each page's actions column carries only what
    [Shared Page-Header Partial](#shared-page-header-partial) fixes: the
    knowledge-graph page's layout dropdown, the sprint page's link back to the
    roadmap's sprints page, and the task page's link back to the roadmap's tasks
    page. The sprints, tasks, audit, and index page headers carry no actions column
    content — the tasks page's filter bar sits in its list card's header — and no
    page header links to the knowledge-graph page (Acceptance Criterion 100).
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

112. The filter bar offers **both** a status filter and a sprint filter, beside the
    type filter and the search input: four controls in the order search, sprint,
    status, type, followed by the Apply control, and nothing else — no priority
    filter, no severity filter, and no Reset control. The status dropdown's menu
    holds one checkbox named `status` per value `BACKLOG`, `SPRINT`, `DOING`,
    `TESTING`, and `COMPLETED`, in that order; the type dropdown's menu holds one
    checkbox named `type` per `TaskType` value, in the order `MODELS.md § Enums`
    lists them; neither menu holds an *any* checkbox. The form carries no control named
    `priority` or `severity`. The sprint select's
    first option carries an empty value and is the selected one whenever no sprint
    filter is active, and a dropdown checks no box whenever its dimension has no
    active value. The search input, the sprint select, and each dropdown's toggle
    button carry a `<label>` associated with it by `for`
    and `id`, naming the dimension it filters and carrying `visually-hidden` — neither
    a first option, a toggle's text, nor a `placeholder` replaces it — and every
    control, each checkbox included, is reachable and
    operable from the keyboard. Each dropdown's toggle button carries
    `aria-describedby` naming the `id` of the one `<span>` inside it that holds its
    visible text, so that, in a browser's accessibility tree, the toggle's accessible
    name is `Status` (`Type`) and its accessible description is its visible text —
    `Any status`, `Any type`, the one active value, or `<n> selected` — for example
    name `Status` and description `4 selected` under the defaults (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Filter bar** and **Multi-select
    dropdowns**).
113. Each filter narrows the list by its own dimension. A task matches the status
    filter when its `status` is **equal** to any one of the active status values, and
    the type filter when its `type` is **equal** to any one of the active type values,
    each compared exactly against the spelling in
    `MODELS.md § Enums`; it matches the sprint filter by membership (Acceptance
    Criterion 235). The page offers no priority or severity filter: a request carrying
    `priority` or `severity`, with any value — `7`, `0`, `abc`, or empty — lists exactly
    the tasks the same request lists without it, answers HTTP 200, and no link the page
    generates carries either parameter. `q`, `sprint`, `page`, and `size` each carry
    at most one value; `status` and `type` carry any number.
114. The filters combine **conjunctively**, with each other and with the search
    term: the list holds exactly the tasks satisfying every accepted criterion, and a
    request with no accepted criterion lists every task of the roadmap. A request for
    `?q=cache&status=DOING&type=BUG` lists the `DOING` tasks of type `BUG` whose
    `title` or `#<id>` reference contains `cache`, and no other task. Adding a
    criterion can only shrink the list; no criterion re-admits a task another
    excluded, and the order of the parameters in the query string changes nothing.
115. A filter parameter whose value is not accepted is **ignored**: the response is
    HTTP 200, the list is exactly the list of the same request without that parameter,
    and the parameter's control shows its *any* option. This holds, for `status` and
    `type`, for a value outside the enum or differing from one only in case; for
    `sprint`, for a value that
    is neither `none` nor the canonical decimal `id` of a sprint of this roadmap — `NONE`,
    `007`, `0`, the `id` of a sprint of another roadmap, and the `id` of no sprint among
    them; and, for every filter parameter, for a value carrying surrounding spaces, for
    a parameter present with an empty value, and for a parameter the server cannot
    decode. The other parameters are unaffected: with an ignored `type` and an accepted
    `status`, the list is narrowed by the status alone. For `status` and `type` the
    rule applies to each occurrence on its own: `?type=BUG&type=bug` is narrowed by
    `BUG` alone, and `?type=BUG&type=EPIC` by both values. A repeated `sprint`
    (`?sprint=4&sprint=7`) is read as its first occurrence, and a comma-packed value
    (`?type=BUG,EPIC`) is one string, matches no `TaskType` value, and is ignored whole
    (see [Roadmap Tasks Page](#roadmap-tasks-page), **Query parameters**).
116. The filter bar is a `<form method="get">` whose `action` is
    `/roadmaps/{name}/tasks`. It carries a hidden input named `size` holding the active
    page size on every response, the default `25` included, and no field named `page`,
    so submitting it keeps the page size and always renders page 1; submitting it at
    the default page size requests a URL carrying `size=25`. The rule that `size` is
    carried only when it is not `25` applies to generated links alone (Acceptance
    Criterion 241). Its Apply control is `<button type="submit">` carrying `btn`,
    `btn-primary`, and `btn-sm`, and the form carries no Reset control. Submitting the
    form, then reloading the resulting URL, then submitting it again with the sprint
    select on `Any sprint`, every status and type box unchecked, and an empty search
    input, renders in turn the filtered list,
    the identical filtered list, and the unfiltered list — every task, `COMPLETED`
    tasks included — at the same page size (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Filter bar**).
117. Filter values reach SQL only as **bound parameters**, whether the URL or the
    filter-state cookie supplied them. The task read carries one
    predicate per filtered dimension — sprint, status, and type — with each distinct
    active value bound as a
    parameter of the prepared statement, and no predicate and no parameter for an ignored value; it
    carries no `priority` and no `severity` predicate, whatever the request carries. The check captures the SQL text the page issues and asserts
    that no parameter value appears in it, for accepted values and for hostile ones —
    `status=DOING' OR '1'='1`, `type=BUG;DROP TABLE tasks`, `sprint=1 OR 1=1` —
    which are ignored by Acceptance Criterion 115 and reach no statement at all; after
    such requests the roadmap's tasks are intact; the same hostile values carried in
    the cookie are ignored likewise. `q`, `page`, and `size` never reach
    SQL. No filter value is echoed into the page as text: the sprint select's options
    and the dropdowns' checkboxes are the
    server's own enumeration of the roadmap's sprints or of an enum, and
    a value only decides which of them is `selected` or `checked` (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Query parameters**, and
    `DATABASE.md § List All`).
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
    `á` — and a term of `ΟΔΟΣ` finds a task titled `ΟΔΟΣ`. A term
    whose bytes are not valid UTF-8 is folded with each invalid byte replaced by
    `U+FFFD` and is then matched like any other term, being neither an error nor an
    absent term (Acceptance Criterion 105 continues to hold; see
    [Roadmap Tasks Page](#roadmap-tasks-page), **The folding rule**).
119. The server prepares the term and every task's searchable text through **one**
    normalisation function and **one** folding function, shared by both, and no
    second implementation of either exists for the search: a check that swaps the
    folding function for a substitute changes the verdict for the term and for the
    searchable text alike. No script the binary serves folds, trims, or normalises a
    term: `GET /static/task-search.js` returns HTTP 404, and no asset under `/static/`
    carries a case mapping, a whitespace table, or normalisation tables for the search
    (see [Roadmap Tasks Page](#roadmap-tasks-page), **One implementation**).
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
121. Before the term is normalised and folded, every code point carrying Unicode's
    **White_Space** property is removed from the start of the term and from its end,
    and no other code point is removed from anywhere: a code point carrying that
    property elsewhere in the term survives and is matched literally, and a term made
    only of such code points becomes the empty string and lists every task. `U+0085`
    (NEXT LINE) carries the property and **is** removed; `U+FEFF` (ZERO WIDTH NO-BREAK
    SPACE) does not carry it and is **not** removed, so a term pasted with a leading
    byte-order mark matches nothing on an ordinary roadmap. The term is trimmed,
    **then** normalised, **then** folded, in that order. The task's searchable text is
    normalised and folded but never trimmed, so a task's own leading or trailing
    whitespace is part of its text (Acceptance Criteria 101 and 152 continue to hold;
    see [Roadmap Tasks Page](#roadmap-tasks-page), **The trim rule**).
122. The tasks page ships no client copy of the search. The served HTML of the page
    references no `static/task-search.js`, the embedded asset set holds no such asset,
    and no script of the page reads the search input, rewrites the URL, or hides a row:
    filtering, searching, and paginating are performed by the server alone, on each
    request (Acceptance Criteria 104, 107, and 119 continue to hold; see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Filter bar**).
123. Every query-bar failure of `GET /roadmaps/{name}/graph/data` is answered with
    HTTP `400 Bad Request` and a JSON body of exactly two string fields, `error`
    and `kind`, and never with HTTP 200 and never with the
    `{"nodes": ..., "edges": ...}` shape. `kind` takes exactly three values, the
    set [Query-Bar Error Handling](#query-bar-error-handling), rule 4, enumerates
    and is canonical for: `invalid_limit` for a `limit` outside the six allowed
    values (Acceptance Criterion 48), `plan_prefix` for a statement carrying an
    `EXPLAIN` or `PROFILE` prefix (Acceptance Criterion 166), and `execution` for
    a statement that failed once running, which includes one cancelled for
    exhausting the 5-second time budget (Acceptance Criterion 110). One status
    serves all three and the `kind` is what distinguishes them. **A body carrying
    any other `kind` MUST fail this criterion**, and the criterion MUST assert the
    closed set rather than only the three members, because a further value is
    exactly what an endpoint that started refusing statements on the ground of
    what they do would publish. The order is fixed and testable: a request
    carrying an invalid `limit` is answered `invalid_limit` whether its statement
    is prefixed, unexecutable, or neither, because the endpoint resolves the limit
    before it examines the statement, and the statement is not executed; a
    request whose `limit` is accepted and whose statement carries a prefix is
    answered `plan_prefix`, and the statement is not sent (Acceptance
    Criterion 170). The boundary against the
    internal read error is drawn at the moment the failure surfaces: a graph that
    cannot be reached at all, because no server is listening for the roadmap or
    none could be reached through a socket that answered, is
    answered HTTP 503, while a failure surfacing once the statement is running is
    answered HTTP 400 with `kind` `execution`, a store corruption a scan discovers
    mid-statement included. The `error` of an execution failure carries the
    engine's diagnostic and the page renders it in place; the `error` of an invalid
    limit names the rejected value; and the `error` of a plan-prefix refusal is the
    fixed line rule 12 of that section publishes (see
    [Query-Bar Error Handling](#query-bar-error-handling) and
    `DATA_FORMATS.md § Graph View Data`, **Error Shape**).
124. The graph card of the knowledge-graph page, the one full-height page region,
    satisfies **both** edges of
    [Full-Height Page Regions](#full-height-page-regions), rule 1, when the page is
    rendered in a browser: the region's bottom edge coincides with the bottom edge
    of that page's `main.page-body` element, and that edge lies within the viewport,
    with the document scrolling vertically no further than the viewport height. The
    check asserts both edges, because either edge alone
    passes on a defective layout. A region that stops short of the page body's end
    leaves an unused band beneath itself while remaining comfortably within the
    viewport, so a check that asserts only the second edge accepts it. A region that
    overruns the bottom of the viewport still ends exactly where the page body ends,
    because its own overrun stretched the page body to that height, so a check that
    asserts only the first edge accepts it too. A criterion phrased as the region
    "using the available height", or as its height matching a particular length,
    establishes neither edge: every height is the available height of some layout, and
    a length is only ever correct for the page as it stood when the length was chosen.
    The roadmap tasks page carries no full-height region: its list card is sized to the
    rows of one page.
125. Acceptance Criterion 124 is checked at a set of viewport widths chosen so that
    the material above the graph card renders at more than one height — the page
    header's actions column and the query bar share rows at a wide viewport and wrap
    onto further rows as the viewport narrows — and both edges hold at every one of
    those widths. Each of those widths is exercised at a viewport tall enough that the
    floor does not bind: below the floor the region takes its minimum whatever the
    material above it measures, so a check made there would record the floor instead of
    the tracking these widths exist to vary, and what holds below the floor is
    Acceptance Criterion 127's to state. This is what a height obtained by
    subtracting a fixed length from the viewport height cannot pass: such a height
    is correct at whichever width its length was chosen for and wrong at every
    other, so a check made at a single width would accept it and leave the
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
    rule 6).
127. On a viewport short enough that the space the page body leaves falls below the
    full-height region's minimum height, the region takes that minimum rather than
    shrinking to the space available, and the vertical page scrolling that follows
    is permitted. Below the floor **neither** edge of Acceptance Criterion 124 is
    guaranteed. The second edge does not hold: the floored region is what the page
    scrolls vertically to reach. The first edge does not hold either, because the
    knowledge-graph page renders its query bar between the top of the page body and the
    graph card (see [Graph Query Bar](#graph-query-bar)), and the page body, held to
    the same minimum as the region, must then carry the bar and the floored region
    together, so the region's foot passes the page body's foot by the space the bar
    occupies. No stylesheet closes that gap: closing it would take a page-body floor of
    the region's floor plus the height of the bar, which is the fixed subtraction
    [Full-Height Page Regions](#full-height-page-regions), rule 3, forbids, and that
    height is no constant — the query bar stacks its own controls as the viewport
    narrows — so a length chosen for one viewport width is wrong at the rest. Above the
    floor, which is every viewport height at which the region is worth presenting at
    all, both edges hold: this exception is the floor case alone and weakens Acceptance
    Criterion 124 nowhere else. The check exercises both a viewport at which the floor
    binds and one at which it does not, because a check run only below the floor
    passes on a region that never tracks the page body at all, while a check run only
    above it leaves the floor free to be deleted as though it were the defect that
    Acceptance Criterion 124 describes (see
    [Full-Height Page Regions](#full-height-page-regions), rule 5).
128. The tasks page's table sits inside Tabler's `table-responsive` container: at a
    viewport too narrow for its columns, the table scrolls horizontally inside that
    container, the scroll is reachable by a touch gesture, and `<body>` produces no
    horizontal overflow at any viewport width (Acceptance Criterion 27 continues to
    hold). A long task title wraps within its cell at word boundaries, under the
    column widths Acceptance Criterion 244 fixes. The page scrolls vertically like
    any other page (see [Roadmap Tasks Page](#roadmap-tasks-page) and
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
    rule 9).
129. The list card's header wraps rather than overflowing. It carries the classes
    `card-header`, `flex-wrap`, and `gap-2`, and the filter bar's form carries
    `row g-2 align-items-end justify-content-end`, each control in a
    `col-12 col-sm-auto` column. At a viewport width of `1440px` the title block and the
    whole filter bar share one line, the filter bar's last control ending at the
    header's trailing edge; at a viewport too narrow for both, the filter bar's
    container moves to a line of its own below the title block and its controls wrap
    onto further lines, every line of controls aligned to the trailing edge; and on a
    phone-sized viewport each filter control takes a line of its own. The controls
    measured are the compact controls of Acceptance Criterion 243, whose labels are
    not displayed. The card footer's three columns wrap onto further lines likewise.
    At every viewport width measured — at least `375px`, `576px`, `992px`, and `1440px`
    — no control of the header or of the footer extends beyond the card's edge, and
    `<body>` produces no horizontal overflow (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **The card header**, and
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
    rule 9).
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
131. Each column count of the sprint's member-tasks board is the number of the
    sprint's member tasks in the statuses the column groups: the `WAITING` column's
    badge equals the number in `BACKLOG` or `SPRINT`, the `DOING` column's badge the
    number in `DOING` or `TESTING`, and the `CLOSED` column's badge the number in
    `COMPLETED` — the `Summary.Pending`, `Summary.InProgress`, and
    `Summary.Completed` counters of `models.CalculateSprintShowResult` for that
    sprint — and the three sum to the sprint's total number of member tasks
    (`Summary.TotalTasks`). The check derives the expected counts from the sprint's
    member tasks and their statuses, and asserts each badge against its own
    expected count, because a board that grouped the statuses differently could
    still show three counts whose sum is right. For a sprint of 55 member tasks of
    which 8 are in `BACKLOG` or `SPRINT`, 29 in `DOING` or `TESTING`, and 18 in
    `COMPLETED`, the three column badges read `8`, `29`, and `18`, and the board
    shows 55 cards in total.
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
133. Each card of the sprint's member-tasks board shows exactly seven data points, on
    two lines, in this order: the task `title` as the card's first line and
    prominent main content; and one line carrying, at its leading edge, the badge
    line — an id badge reading `#<id>`
    with the classes `bg-black` and `text-white`, a `severity` badge reading `S`
    immediately followed by the task's severity, a `priority` badge reading `P`
    immediately followed by the task's priority, with no colon, no space, and no
    other separator between the letter and the digits, and a type badge reading the task's `type` and
    coloured by the task type mapping, in that order (Acceptance Criteria 177 to
    179) — and, at its trailing edge, the number of comments followed by the number of
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
    to hold). A task of severity `3` and priority `5` shows `S3` and `P5`; a badge
    reading `3`, `S 3`, `S:3`, `Sev:3`, or `Sev: 3` (or the corresponding `Pri:`
    form) does not satisfy this criterion, and no `Sev:` or `Pri:` text appears on
    the card. The priority and severity badges
    take the colours the semantic mapping assigns to their values, which the badge
    label does not affect (Acceptance Criterion 61
    continues to hold). The card carries **no** status badge, because the column
    already states the status, and it shows no dependency counts.
    The task's full field set is reached through the task page the card links to
    (see [Sprint Detail Sub-Template](#sprint-detail-sub-template)
    and [Roadmap Task Page](#roadmap-task-page)).
134. Every card of the sprint's member-tasks board renders both of its counters: the
    comment count and the subtask count are present on every card, including when
    either or both are `0`, so a task with no comment and no subtask still shows the
    comment icon followed by `0` and the subtask icon followed by `0`, and both
    numbers sit at the trailing edge of the card's second line, which every card of
    the board renders. The check asserts a card whose two
    counts are both zero, because a card that has something to count renders the same
    markup whether this criterion holds or not.
135. Each card of the sprint's member-tasks board is a link to its task's page,
    and the card **is** the link: in the served HTML the card is one `<a>` element
    carrying the classes `card` and `card-link` and the `href`
    `/roadmaps/{name}/tasks/{id}` of its own task, followed by pointer, touch, and
    Enter, and carrying no `tabindex` and no `role`. Its accessible name is
    `Open details for task #<id>: <title>`, containing the task title that is the
    card's visible label; a name carrying the `id` alone does not satisfy this
    criterion. Following a card issues no request from the sprint page itself: the
    task page is an ordinary navigation, every script the sprint page loads is
    served from `/static/`, none is inline, and the Content-Security-Policy of
    Acceptance Criterion 33 is unchanged (Acceptance Criteria 93 and 96 continue to
    hold).
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
    on a narrow viewport each expanded column keeps a minimum width at which its
    cards stay legible, with touch-friendly hit targets on the cards (see
    [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Height and
    scrolling**, and
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
    rule 10).
137. The comment number on each card of the sprint's member-tasks board comes from
    **one** grouped counting query issued over the sprint's member tasks, selected by
    the sprint id with one bound parameter, never one query per card: an instrumented count of comment-counting queries
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
    a change. Every card is a link to a read-only task page (Acceptance
    Criterion 135), and every `<button>` in the board is a column collapse toggle
    carrying `data-role="task-board-column-toggle"`, one per column header, whose
    activation changes only the board's presentation (Acceptance Criteria 212 to
    218). Neither submits anything or changes any data. There is no route and no client-side
    path through which the board can write; the `rmp` CLI remains the sole write
    path.
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
    lengths is expressed in `rem`. This is a stylesheet change only: the board emits the same markup and the same
    classes, carries no inline `style` attribute, and Acceptance Criteria 27, 130,
    and 136
    continue to hold. The criterion is asserted with all three columns expanded,
    which is the state a page load presents for a sprint with no member task or
    with at least one member task in every column (Acceptance Criterion 212); with
    one or more columns collapsed,
    the expanded columns divide the width the collapsed strips leave, on the same
    terms (Acceptance Criterion 214) (see
    [Sprint Detail Sub-Template](#sprint-detail-sub-template), **Height and
    scrolling**).
140. Each per-column count badge of the sprint's member-tasks board carries the
    semantic colour of the status its column groups, while its text stays that column's
    task count (Acceptance Criterion 131 continues to hold). A column groups a set of
    statuses — two for `WAITING` and for `DOING`, and `COMPLETED` alone for `CLOSED` —
    so the badge takes the variant of the group's canonical status: `WAITING` carries
    `SPRINT`'s `bg-cyan-lt`, `DOING` carries `DOING`'s `bg-blue-lt`, and `CLOSED`
    carries `COMPLETED`'s `bg-green-lt`. A column holding no task shows the count `0`
    and keeps the colour of its status, because the colour follows the column and not
    the cards in it. The check asserts all the columns of the board together, exactly
    as Acceptance Criterion 120 asserts the three tabs together, and fails on a
    rendering that gives every column `bg-secondary-lt`. Asserting the columns together
    is necessary but not sufficient, and this criterion puts two further requirements
    on the check. The first is that each badge's class is produced by the single
    implementation of this mapping that every status badge already takes its colour
    from, rather than written into the template as a literal or resolved through a
    second mapping standing beside the first: a literal reads exactly as the mapping's
    answer on the day it is written and is then free to drift from it. The check
    establishes that by rendering the board a second time with that one implementation
    replaced by a substitute whose answer names the status it was called with; under
    the substitution a class written into the template survives unchanged and fails,
    while a template that calls the implementation renders the substitute's answer on
    every column. The second requirement is that the check pins each column to the
    status that column groups, which no assertion about the colours alone can make: a
    check establishing only that the columns' colours differ from one another passes
    unchanged on a board whose columns carry each other's statuses. The same
    substitution settles that, because a column headed by one status whose badge names
    another is visible in the rendering. The check also asserts the count badges that
    stay neutral: the Comments card header count on the Roadmap Sprint Page carries
    `bg-secondary-lt`, because it counts comments and a comment has no status to key
    on, and so does any count over a group of mixed status for which no canonical
    status is defined. The mapping introduces no new colour and no new band
    (Acceptance Criterion 61 continues to hold; see
    [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
    rule 2).

141. Every HTTP 500 the running server returns is accompanied by exactly one
    `log/slog` `ERROR` record on stderr, and no 500 is silent. The record names
    the request `method` and `path`, the `status`, and the underlying error text
    under `err` — the value the response body withholds. The response body is
    unchanged: it remains the opaque `internal server error` text, and the error
    detail never reaches the client. Stdout carries only the startup URL object;
    no log record is ever written to it. **An HTTP 503 is recorded at `WARN` and
    not at `ERROR`**, under Acceptance Criterion 165; a 503 accompanied by an
    `ERROR` record fails that criterion and this one, because it would put this
    criterion's own count of `ERROR` records over one for a request that produced
    no 500.
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
147. **A graph data request takes no graph store lock, and the criterion MUST
    assert the absence rather than a success.** While a server is running for a
    roadmap, that server holds the store's exclusive advisory lock for its whole
    process lifetime. A `GET /roadmaps/{name}/graph/data` for that roadmap is
    answered `200` throughout. **That `200` is the assertion, and it is
    sufficient**: the lock is exclusive and is held for the whole life of the
    server, so a request that contended for it would exhaust its bounded wait and
    fail rather than succeed at all. The criterion MUST NOT add an assertion on
    elapsed time, which would establish nothing the `200` has not already
    established (see `GRAPH.md § Lock Contention` and
    `BUILD.md § No Benchmarks and No Performance-Measurement Tests`).
148. **Two graph data requests against one roadmap do not serialise against each
    other, and a `rmp graph client` invocation does not serialise against either.**
    A slow statement submitted through one request does not delay a second request
    for the same roadmap on this side of the socket: both are in flight at once,
    and both are served. What may still happen inside the server is a
    serialisation conflict, which the client retries (see
    `GRAPH.md § Concurrency Inside the Server`). An implementation in which the two
    requests serialise fails this criterion, because serialising is what taking the
    store lock would produce.
149. **A graph data request for a roadmap with no server running is answered HTTP
    503 promptly, with the opaque body and exactly one `WARN` log record**, and
    the server keeps serving other requests throughout — which is half of what
    `503` asserts and MUST be checked, by serving an unrelated route successfully
    while the graph request is failing. The criterion MUST assert `503` and not
    merely a 5xx, because `500` is this endpoint's answer to a different
    condition; and it MUST assert `WARN` specifically rather than "a record", for
    the same reason and by the same split. The log record names the socket path
    that was probed; the response body does not (see
    [Record Content](#record-content), rule 6, and
    [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
    rule 2).
150. **A store left with a stale `snapshot.tmp` staging directory, or with
    `snapshot/` absent and `snapshot.bak/` carrying a manifest, is repaired by
    `rmp graph serve` at its startup and not by a request.** With a server started
    over such a store, a graph data request is served correctly and the response
    carries the committed graph. The criterion MUST also assert that a graph data
    request issued with **no** server running leaves those two states exactly as it
    found them, which is what establishes that this interface runs no recovery of
    its own (see `GRAPH.md § What a Statement That Writes Nothing Changes on Disk`
    and [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
    rule 6).
151. A schema-introspection command written with anything but a single space
    between its two keywords is answered as the engine's own parse failure: HTTP
    `400 Bad Request` with `kind` `execution` and the engine's diagnostic in
    `error`. The endpoint neither refuses it before execution nor repairs the
    spacing, and it publishes no class of its own for it. The same statement
    written with one space is answered HTTP `200` with `{"nodes": [], "edges": []}`
    (Acceptance Criterion 156), so the two spellings differ in the response, and
    the difference is the engine's routing rather than a rule of this endpoint's
    (see `GRAPH.md § What Groadmap Does Not Check`, item 7). A body carrying any
    `kind` other than `execution` MUST fail this criterion, whether that value
    lies outside the closed set of Acceptance Criterion 123 or is another member
    of it: the command carries no `EXPLAIN` or `PROFILE` prefix, so `plan_prefix`
    is as much a failure here as `invalid_limit`.
152. A term and a task's searchable text are normalised to Unicode's **Normalization
    Form C** before they are folded, and the pipeline for a term is trim, then NFC,
    then fold, then NFC, in that order. The normalisation is for
    comparison only: the `title` bytes the roadmap stores are unchanged, `rmp task
    get` returns the same bytes it returned before this rule existed, and the row
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
    and precomposed title with precomposed term each return the task. `U+0130`
    resolves as Acceptance Criterion 118 states — it folds to `U+0069` and never to
    `U+0069 U+0307` — and a title spelled `U+0049 U+0307` carries the same searchable
    text as one spelled `U+0130`.
154. Normalisation changes nothing else. It does not make one word a substring of
    another: a task titled `Café Lisboa onboarding` is **not** returned by the term
    `cafe`, and one titled `Aérea cargo terminal` is **not** returned by the term
    `ae`, because the form is NFC and an accented letter stays one code point rather
    than a base followed by a mark. Measured over the whole of Unicode, exactly
    **1,117** of the 1,112,064 code points produce a different searchable text under
    this rule than without it, and **none of them is ASCII**, so every ASCII term and
    every ASCII title selects exactly the tasks it selected before. The 1,117 are the
    canonical singletons and the composition exclusions.
155. The server normalises the term and the task's searchable text with
    `golang.org/x/text/unicode/norm`'s Normalization Form C, through one function shared
    by both (see `BUILD.md § External Dependencies`), and nothing else normalises them:
    no script the binary serves normalises a term, and the served HTML of the tasks page
    references no script that could (Acceptance Criteria 119 and 122 continue to hold;
    see [Roadmap Tasks Page](#roadmap-tasks-page), **The normalisation rule**).
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
    (Acceptance Criterion 156), and `rmp graph client` against that same server
    answers the identical statement with the rows, naming the index the caller
    declared (`GRAPH.md` Acceptance Criterion 34). The criterion MUST assert both
    halves together: the endpoint's answer is empty because its response shape
    carries nodes and edges, not because the store's schema is empty, and the CLI
    read is what establishes the difference. Asserting that the endpoint reports the
    index row MUST fail this criterion.
158. **The graph data endpoint reaches the graph only through a running server,
    and the criterion turns on a pair rather than on either status.** With
    `rmp graph serve` running for a roadmap, a `GET` of
    `/roadmaps/<roadmap>/graph/data` whose `q` carries
    `CREATE (n:WebProbe {key:'w'})` is answered HTTP `200`, and the node is then
    reported by `rmp graph client` against that same running server; the read-back
    is what establishes that the request reached the server rather than a graph of
    the endpoint's own. With the server stopped, the identical request is answered
    HTTP `503` and the roadmap's `graph/` directory is byte-identical before and
    after it. Neither half is the assertion alone: the `200` does not say which
    process ran the statement, and a `503` on its own is satisfied by an endpoint
    that answers it unconditionally. `503` MUST be asserted specifically rather
    than as a 5xx, because `500` is this endpoint's answer to a socket path over
    the platform's bound (see
    [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
    rule 1, and `GRAPH.md § Server Resolution`).
159. **A socket file with nothing behind it is answered exactly as an absent
    socket is, and is not removed.** With a socket file present at the
    roadmap's default socket path and no process listening on it, a `GET` of
    `/roadmaps/<roadmap>/graph/data` is answered HTTP `503` with no `kind`. The
    response MUST be compared against the response to the same request with no
    socket file present at all and found **equal**, because the two are one
    condition (see `GRAPH.md § Server Resolution`, rule 1). **That equality is
    what establishes that the refused connection was recognised inside the probe
    rather than waited on**, since a request that waited out the probe would be
    answered as Unreachable and would not match. The criterion MUST NOT assert on
    elapsed time (`BUILD.md § No Benchmarks and No Performance-Measurement
    Tests`). The leftover socket file is still present afterwards, because no
    caller removes one.
160. **A derived socket path over the platform's bound refuses the request with
    `500`, and the criterion turns on three answers rather than two.** The
    criterion drives one request, `GET /roadmaps/<roadmap>/graph/data`, three
    times:
    - under a `HOME` deep enough that the roadmap's derived socket path
      `~/.roadmaps/<name>/graph.sock` exceeds the platform's bound, it is answered
      HTTP `500`;
    - under a `HOME` short enough that the same roadmap's derived path is inside
      the bound, with **no** graph server running, it is answered HTTP `503`;
    - under that same short `HOME`, with `rmp graph serve` running for the
      roadmap, it is answered HTTP `200`.

    **The three together are the assertion and no one of them is.** `500` alone is
    satisfied by an endpoint that fails for any reason; `503` alone by one that
    never reaches a server; `200` alone by one that ignores the bound entirely.
    What the triple establishes is that the endpoint tells a permanent defect in
    the roadmap's layout apart from a dependency the operator has not started, and
    both apart from success — which is the whole of the distinction
    [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
    rule 1, draws. An implementation that answered `503` under the deep `HOME`
    fails this criterion even though it failed the request, because it would tell
    the operator to start a server that can never bind.

    The response carries no `kind` in either 5xx: the query-bar error shape belongs
    to the `400`s, and neither request reached a statement. The criterion MUST
    establish the bound empirically, by binding real sockets at increasing path
    lengths until one is refused, and never from a literal — the reason `GRAPH.md`
    Acceptance Criterion 67 gives holds here unchanged, because a hard-coded 107 is
    wrong on three of the five supported operating systems.

    **The deep-`HOME` half also establishes that the condition is permanent rather
    than momentary.** Under that `HOME` no server can be started for the roadmap at
    all: `rmp graph serve` refuses the same path on the same rule
    (`GRAPH.md § Socket Path Length`, rules 5 and 6). The criterion MUST assert that
    refusal alongside the request's `500`, because it is what makes `500` the right
    code rather than `503` — the second announces a service that will come back,
    and this one will not.

    It MUST also assert that `~/.roadmaps/<roadmap>/graph/` does not exist after
    the refused request, and the scope of that assertion is stated here so that it
    is not mistaken for the discriminator: this endpoint creates nothing under
    `graph/` on any path, so its absence is what every refused request leaves and
    separates nothing on its own (see
    [Security and Constraints](#security-and-constraints), rule 4). What it does
    rule out is a different defect, and a future one: an implementation that opened
    or created the store on this path (see
    [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
    rule 1, and `GRAPH.md § Socket Path Length`, rules 5 and 6).
161. **The graph page renders for a roadmap with no server, and it says which
    state it is in.** With no `rmp graph serve` running for a roadmap, a `GET` of
    `/roadmaps/<roadmap>/graph` is answered HTTP `200` — the page itself is
    server-rendered from the roadmap's database and does not depend on the graph —
    and the data request the page then makes is answered HTTP `503`. The page
    presents that as a graph it cannot reach, distinctly from the empty-graph state
    it presents for a served graph holding no elements. The criterion MUST assert
    the two states are distinguishable in the rendered page, because an interface
    that showed "no data" for both would tell an operator whose server is not
    running that their graph is empty (see
    [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)).
162. **The endpoint reaches the graph through the shared client and not through a
    child process.** The graph data endpoint's only route to a graph is a call
    into `internal/graphclient`, made inside the `rmp web` process itself, and no
    file that carries the endpoint constructs a child process at all, by any
    route. The assertion is on the source rather than on a run, because an
    endpoint that shelled out to `rmp graph client` would satisfy every
    behavioural criterion in this file — the statuses, the read-backs and the
    timings would all hold — while making the endpoint's behaviour depend on which
    binary is on a path (see
    [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
    rule 3).

    **The sweep covers the whole package, and the single exemption is identified
    by what it does.** A ban confined to the endpoint's own files would be
    defeated by a helper that spawned on the endpoint's behalf, so every
    production file under `internal/web` is scanned. Exactly one of them may
    construct a child process: the one that opens the user's default browser at
    the served URL, which [Server Lifecycle](#server-lifecycle), step 6, requires
    of `rmp web` unless `--no-open` is given. That is what makes the exemption
    identifiable without being a licence — it is held by whichever file carries
    that launch, it covers that launch, and it covers nothing else.

    **The criterion is therefore stated as failures.** It MUST fail in each of
    three distinct ways: a child process constructed in a file that carries the
    graph data endpoint, a child process constructed in any other file of the
    package, and a **second** child process constructed in the exempted file
    itself. It MUST also assert that the exempted file is still the file that
    launches the browser, rather than trusting it: an exemption held by a file
    that launches nothing is a hole left open, and a launch that reached a second
    file would widen the exemption to two, which the criterion MUST report rather
    than absorb.
163. **No web request opens a graph store, and the criterion MUST assert it over
    the store's own artefacts.** Against a roadmap whose `graph/` directory has
    been fingerprinted by name, length and content digest, and with no server
    running, each of these requests is issued and the fingerprint is compared
    afterwards and found unchanged: a graph data request with no `q`, one whose `q`
    is a read, and one whose `q` is a write. No `write.lock` appears where none
    was, no `snapshot.tmp` is removed, and no `snapshot.bak` is promoted. The
    write case is the one that matters most: an implementation that still opened
    the store would run recovery on the open and could change the directory's
    structure without changing its data, which is the effect this criterion exists
    to rule out (see
    [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
    rule 6, and [Security and Constraints](#security-and-constraints), rule 4).
164. **The `503` body says nothing about the socket and the log record says
    everything.** For a graph data request against a roadmap with no server
    running, the response body is the opaque `internal server error` every other
    server-side failure carries and contains no filesystem path, while exactly one `WARN` record
    on the server's stderr carries the no-server line naming the socket path that
    was probed — the same line `rmp graph client` writes for the same condition.
    The criterion MUST assert both halves: the body's silence is a security
    property (see [Record Content](#record-content), rule 6) and the record's
    content is what makes the condition diagnosable at all (see
    [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store),
    rule 2).
165. **The two 5xx answers are recorded at different levels, and the criterion
    turns on the pair rather than on either record.** Against one `rmp web`
    server, two graph data requests are driven and the complete stderr stream is
    compared: a request for a roadmap with no graph server running is answered
    `503` and produces exactly one `WARN` record and **zero** `ERROR` records,
    while a request for a roadmap whose derived socket path is over the
    platform's bound is answered `500` and produces exactly one `ERROR` record.
    Each record names the request `method` and `path`, the `status`, and the
    underlying error under `err`.

    **The zero is the assertion.** An implementation that recorded the 503 at
    `ERROR` would satisfy every check that merely looked for a record, and would
    reintroduce exactly what the level split exists to prevent: an `ERROR` on
    every page load of a roadmap whose server is not running, which trains an
    operator to ignore the level that means something is broken. The criterion
    MUST therefore count records by level rather than search for one, and MUST
    drive both halves against the same server, because a level is only meaningful
    against the other levels that server emits (see
    [What Is Logged](#what-is-logged) and [Levels](#levels)).
166. **Both prefixes are refused, in every spelling the engine's parser
    recognises.** Against a roadmap served by `rmp graph serve`, each of the
    following, submitted as `q`, is answered HTTP `400 Bad Request` with a body of
    exactly two fields: `kind` `plan_prefix`, and `error` exactly the line
    [Query-Bar Error Handling](#query-bar-error-handling), rule 12, publishes. The
    cases are `EXPLAIN MATCH (n) RETURN n` and `PROFILE MATCH (n) RETURN n`; each
    of the two in lower case and in mixed case; a prefix on a statement spread over
    several lines; a prefix preceded by spaces, tabs and newlines; a prefix
    preceded by a line comment, and one preceded by a block comment; a prefix on a
    statement that carries its own `LIMIT`; a prefix on a write,
    `EXPLAIN MATCH (n) DETACH DELETE n`; and a prefix on a procedure call projected
    through a `RETURN`. Each case is submitted with no `limit` and with an allowed
    one, and the answer is the same.

    **The cases come from the engine's grammar, so the criterion MUST confirm its
    premise.** Before asserting the answer, the criterion MUST confirm against the
    pinned engine's parser that each case is one the parser reports as carrying a
    prefix, and MUST fail if one is not. A grammar change at a future pin then
    surfaces as a failed premise rather than silently changing what the criterion
    asserts.
167. **A refused statement is never sent, so it writes nothing and needs no
    server.** The criterion has three halves. First, against a served roadmap,
    `EXPLAIN CREATE (n:Probe {key:'p'})` and `PROFILE CREATE (n:Probe {key:'p'})`
    are each answered `plan_prefix`, and a read-back through `rmp graph client`
    finds no `Probe` node and the graph's node and relationship counts unchanged.
    Second, with **no** `rmp graph serve` running for the roadmap,
    `EXPLAIN MATCH (n) RETURN n` and `PROFILE MATCH (n) RETURN n` are each
    answered HTTP `400` with `kind` `plan_prefix`, while `MATCH (n) RETURN n` is
    answered HTTP `503`. Third, for a roadmap whose derived socket path is over the
    platform's bound, established as Acceptance Criterion 160 establishes it, the
    same two prefixed statements are each answered HTTP `400` with `kind`
    `plan_prefix`, while `MATCH (n) RETURN n` is answered HTTP `500`.

    **The no-write half alone asserts little.** An `EXPLAIN` executes nothing and
    the engine refuses a `PROFILE` of a write wherever either runs, so an endpoint
    that sent both statements would leave the graph unchanged too. The `kind`, and
    the two pairs that set a `400` against a `503` and against a `500`, are what
    establish that the refusal is decided before any graph server is resolved.
168. **A statement carrying no prefix is answered byte for byte as before.**
    Against a served roadmap, each of the following requests is answered with the
    status and the exact response bytes that the same request produces when the
    prefix recognition is left out of the endpoint's path, driven against the same
    server holding the same graph: a request with no `q`; the default query with
    each of the six allowed `limit` values; a read carrying its own `LIMIT`;
    `SHOW INDEXES`; a standalone procedure call; a statement that fails in the
    engine, whose `error` carries the same diagnostic; and a write with no
    projection, `CREATE (n:Probe {key:'it\'s a "probe"'})`. For the write, the
    criterion MUST also read the stored `key` back through `rmp graph client` and
    find it equal to the value the literal denotes, which establishes that what was
    sent is the resolved statement and not text derived from the parse.
169. **A lookalike is not refused, and text the parser cannot parse keeps its old
    answer.** Against a served roadmap, `MATCH (explain) RETURN explain`,
    `MATCH (n) WHERE n.key = 'EXPLAIN' RETURN n`,
    ``MATCH (n:`PROFILE`) RETURN n``, `MATCH (n) RETURN n // EXPLAIN`, and
    `MATCH (n) RETURN n.explain AS profile` are each answered HTTP `200` with the
    node-and-edge shape. `EXPLAINMATCH (n) RETURN n`, `EXPLAIN` alone,
    `EXPLAIN EXPLAIN MATCH (n) RETURN n`, `EXPLAIN SHOW INDEXES`,
    `EXPLAIN CREATE INDEX probe_idx FOR (n:Probe) ON (n.key)`, and
    `EXPLAIN MATCH (n RETURN n` are each answered HTTP `400` with `kind`
    `execution` and the engine's diagnostic in `error`, and never with
    `plan_prefix`. With no graph server running, every statement of both groups is
    answered HTTP `503`, which establishes that each reached server resolution
    rather than being refused. As in Acceptance Criterion 166, the criterion MUST
    first confirm against the pinned engine's parser that each statement of the
    first group parses and carries no prefix, and that each statement of the
    second group fails to parse, and MUST fail if one does not.
170. **An invalid `limit` outranks a prefix.** A request carrying `limit=7` and
    the `q` `EXPLAIN MATCH (n) RETURN n` is answered HTTP `400` with `kind`
    `invalid_limit` and an `error` naming the rejected value, and never with
    `plan_prefix`; so is the same request carrying `PROFILE` instead, so is each
    of the two carrying the non-numeric `limit=all`, and so is each of those four
    with no graph server running for the roadmap. The control is the same prefixed
    statement with an allowed `limit`, which is answered `plan_prefix`: without it
    the criterion cannot tell a limit that outranked the prefix from a prefix that
    was never recognised (see
    [Query-Bar Error Handling](#query-bar-error-handling), rule 5).
171. **A refusal is recorded once, at `WARN`, with its kind and its line.** For a
    request refused as `plan_prefix`, the server's complete stderr stream carries
    exactly one record for that request: a `WARN` record naming the request
    `method` and `path`, whose `status` is `400`, whose `kind` is `plan_prefix`,
    and whose `err` is exactly the line
    [Query-Bar Error Handling](#query-bar-error-handling), rule 12, publishes. The
    stream carries **zero** `ERROR` records for it. The criterion MUST drive the
    request once against a served roadmap and once against a roadmap with no graph
    server running, and MUST find the same single record both times: an endpoint
    that resolved the graph server before examining the prefix would answer the
    second request with the record of a graph server that cannot be reached
    instead, and counting records rather than searching for one is what detects
    it (see [What Is Logged](#what-is-logged) and
    [Record Content](#record-content)).
172. **The page shows the refusal's line in place.** On the knowledge-graph page
    of a served roadmap, with a graph already rendered, submitting
    `EXPLAIN MATCH (n) RETURN n` through the query bar shows exactly the line
    [Query-Bar Error Handling](#query-bar-error-handling), rule 12, publishes in
    the query bar's in-place message, written as text. The graph already shown is
    left in place, the page does not crash, and the failure triggers no
    navigation. Removing the prefix and searching again renders the graph (see
    Acceptance Criterion 50 and
    [Query-Bar Error Handling](#query-bar-error-handling), rule 3).
173. **Every page's document title follows one format.** With a roadmap named
    `payments` holding sprint 7 and task 42, served on a machine whose hostname is
    `thinkpad`,
    each HTML page carries exactly one `<title>` element, whose text is exactly:
    `Roadmaps - thinkpad` for `GET /`; `payments - Sprints - thinkpad` for
    `GET /roadmaps/payments`; `payments - Tasks - thinkpad` for
    `GET /roadmaps/payments/tasks`; `payments - Audit - thinkpad` for
    `GET /roadmaps/payments/audit`; `payments - Knowledge graph - thinkpad` for
    `GET /roadmaps/payments/graph`; `payments - Sprint #7 - thinkpad` for
    `GET /roadmaps/payments/sprints/7`; and, task 42 being titled
    `Rotate the signing keys`, `#42 Rotate the signing keys - payments - thinkpad`
    for `GET /roadmaps/payments/tasks/42`. Each separator is one space, one ASCII
    hyphen-minus, and one space (see [Document Title](#document-title)).
174. **No document title names the product.** On every page of criterion 173, the
    `<title>` text contains no occurrence of `Groadmap`, in any letter case. The
    sidebar brand is unaffected: each page still renders exactly one
    `navbar-brand` element (see [UI Framework](#ui-framework), rule 13).
175. **The hostname comes from the serving machine, not the request.** Requesting
    any page of criterion 173 with a `Host` header naming another host, such as
    `Host: example.org`, yields the same `<title>` text as a request carrying the
    served host: the last segment is the hostname the machine reported at server
    startup, and nothing in the request changes it.
176. **An unavailable hostname drops its segment, not the server.** When the
    hostname lookup fails, or returns an empty string, `rmp web` starts, prints its
    URL, and serves every page with HTTP 200, and each `<title>` omits the hostname
    segment and the separator before it: `Roadmaps` for `GET /`,
    `payments - Tasks` for `GET /roadmaps/payments/tasks`, and
    `payments - Sprint #7` for `GET /roadmaps/payments/sprints/7`. No title ends
    in a separator.
177. **Every task type renders its own variant, on both pages.** In the rows of the
    roadmap tasks page's list and on the cards of the sprint's member-tasks board, the
    type badge's text is the task's `type` exactly as the `TaskType` enum spells it,
    with no badge label, and its colour is the variant the task type table assigns to
    that value: `BUG` renders `bg-red-lt`, `USER_STORY` renders `bg-green-lt`, `TASK`
    renders `bg-blue-lt`, `SUB_TASK` renders `bg-azure-lt`, `EPIC` renders
    `bg-purple-lt`, `REFACTOR` renders `bg-indigo-lt`, `IMPROVEMENT` renders
    `bg-teal-lt`, `SPIKE` renders `bg-yellow-lt`, `DESIGN_UX` renders `bg-pink-lt`, and
    `CHORE` renders `bg-secondary-lt` (see
    [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours)).
    The check MUST render a task of each of the ten types on each of the two pages and
    assert all twenty badges, because a mapping that is wrong for one type passes on
    every other. The mapping reaches no other surface: the task page shows the `type`
    as plain text and not as a badge, the comment-type badges keep the neutral
    `bg-secondary-lt` variant (Acceptance Criterion 66 continues to hold), and the type
    select of the tasks page's filter bar offers plain options with no colour.
178. **The id badge is black with white text and reads `#<id>`.** In the `ID` cell of
    each row of the tasks page's list and on the card of the sprint's member-tasks
    board, the id badge's text is the task's `id` written with its leading `#` — task 42
    reads `#42` — and the badge carries the classes `bg-black` and `text-white` for
    every task, whatever its type, status, priority, or severity. Both classes are
    defined in the vendored `tabler.min.css`, and the badge renders a `#000000`
    background with `#ffffff` text. The check MUST assert, on tasks of different types,
    severities, and priorities, that the id badge carries `bg-black` and `text-white`
    and no variant any table of the badge colour mapping assigns, because the id badge's
    colour MUST NOT vary with any value of the task. The id badge takes no colour from
    any table of the badge colour mapping.
179. **The sprint board's card leads with the title, then one badge line reading id,
    severity, priority, type.** On the card of the sprint's member-tasks board, the task
    `title` is the first line of the card; the next line opens with exactly four
    badges, in this order: the id badge, the severity badge, the priority badge, and the
    type badge. The severity badge reads `S<n>` and the priority badge reads `P<n>`, a
    one-letter badge label immediately followed by the value, and neither reads a
    `Sev:` or `Pri:` form (Acceptance Criterion 133). The check MUST assert this order,
    because a card that shows the right four badges before the title, or the priority
    badge before the severity badge, or the type badge before the id badge, satisfies
    Acceptance Criteria 177 and 178 on their own. The same four badges, with the same
    texts, labels, and colours, fill the `ID`, `Type`, `Severity`, and `Priority` cells
    of the tasks page's list, whose columns place severity before priority as the card
    does (Acceptance Criterion 85). The card's accessible name remains
    `Open details for task #<id>: <title>` (Acceptance Criterion 135 continues to
    hold), and the search of the roadmap tasks page still matches a task by its title
    and its `#<id>` reference and by nothing else, so a term matching only a task's
    `type` matches no task (Acceptance Criterion 101 continues to hold).
180. **Every Markdown field renders as Markdown, on every surface that shows it.**
    A field value holding a `**bold**` span, a bulleted list, and two lines
    separated by a single newline renders `<strong>`, `<ul>` with `<li>` items, and
    a `<br>` between the two lines, inside a `<div class="markdown">`, and the
    literal asterisks are not displayed. The check MUST cover each of the seven
    fields on each of its surfaces: the task `functional_requirements`,
    `technical_requirements`, `acceptance_criteria`, and `completion_summary` on
    the task page; a task comment `body` in the task page's Comments card;
    a sprint comment `body` in the sprint Comments card; and a sprint `description`
    in the sprint card under each of the three tabs Próximos, Actual, and
    Concluídos, and on the roadmap sprint page. A surface that still shows escaped
    text passes on every other surface, so no surface is left unchecked. A value
    written with no Markdown syntax renders as paragraphs, a blank line separating
    two of them and a single newline rendering as `<br>`. A task `title` and a
    sprint `title` containing `**bold**` still display the asterisks as text (see
    [Markdown Rendering](#markdown-rendering), rules 1 to 4).
181. **The GitHub Flavored Markdown extensions, footnotes, and definition lists
    render.** A Markdown field containing a pipe table with a header row, a
    `~~struck~~` span, a bare `https://` address and a bare `www.` address, a task
    list with one checked and one unchecked item, a footnote reference with its
    definition, and a definition list renders a `<table>` with a `<thead>` and a
    `<tbody>`, a `<del>` element, an `<a>` element for each of the two bare
    addresses, two `<input type="checkbox">` elements that both carry `disabled`
    and of which exactly the checked item's carries `checked`, a footnote reference
    link and a footnotes list, and a `<dl>` holding `<dt>` and `<dd>` elements. A
    table column aligned by `:---:` carries the class `text-center`, and no element
    of the rendered HTML carries a `style` attribute (see
    [Markdown Rendering](#markdown-rendering), rules 3 and 11).
182. **Fenced code is highlighted by its declared language, and the dark
    stylesheet exists.** A fenced code block declared as `go` renders with chroma's
    token classes on its tokens and with no `style` attribute anywhere in the
    block. A fenced block with no info string, and one declared with a language
    name chroma does not recognise, render with no token class.
    `GET /static/highlight.css` returns HTTP 200 with a CSS content type, and its
    content equals the CSS the chroma version `go.mod` pins produces, in
    class-based form, for the `github-dark` style, with no rule scoped by a theme
    selector. No other syntax-highlighting stylesheet is embedded or served: no
    light variant exists. The roadmap sprints page, the roadmap sprint page, and the
    roadmap task page each link `/static/highlight.css`, and the roadmap tasks page,
    which renders no Markdown field, does not (see
    [Markdown Rendering](#markdown-rendering), rules 6 and 7).
183. **Raw HTML is never emitted.** A Markdown field containing
    `<script>alert(1)</script>`, `<img src=x onerror=alert(1)>`, an `<iframe>`
    block, and an inline `<b onclick=alert(1)>` tag renders HTML that contains no
    `<script>`, `<img>`, `<iframe>`, or `<b>` element and no `onerror` or `onclick`
    attribute, and a `<` inside a code span renders as `&lt;`. The check MUST cover
    the server-rendered path, through a sprint `description` and a sprint comment
    `body`, and the JSON path, through a task requirement field and a task comment
    `body`, because the two paths insert the renderer's output by different
    mechanisms (see [Markdown Rendering](#markdown-rendering), rule 10).
184. **A dangerous link is never active.** A Markdown field containing
    `[a](javascript:alert(1))`, `[b](JavaScript:alert(1))`, `[c](vbscript:msgbox)`,
    `[d](file:///etc/passwd)`, and the autolink `<javascript:alert(1)>` renders the
    text of each with no `<a>` element around it, and the rendered HTML contains no
    `href` attribute at all for them; in particular no `href` is the empty string,
    which a browser would resolve to the current page (see
    [Markdown Rendering](#markdown-rendering), rule 8).
185. **An absolute `http` or `https` link opens in a new tab; any other link does
    not.** In a Markdown field, `[a](https://example.org/)`,
    `[b](HTTP://example.org/)`, `[c](//example.org/)`, and the bare address
    `https://example.org/` each render as `<a>` carrying `target="_blank"` and
    `rel="noopener noreferrer"`, while `[d](/roadmaps/groadmap/tasks)`, a link
    `[e]` whose destination is the fragment `#notes`, and
    `[f](mailto:maintainer@example.org)` each render as `<a>` carrying neither
    `target` nor `rel` (see
    [Markdown Rendering](#markdown-rendering), rule 8).
186. **A remote image becomes a link and causes no request.** In a Markdown field,
    `![deployment diagram](https://example.org/diagram.png)` renders as an `<a>`
    to that URL whose text is `deployment diagram` and which carries
    `target="_blank"` and `rel="noopener noreferrer"`, with no `<img>` element;
    `![](https://example.org/chart.png)` renders as a link whose text is the URL;
    `![favicon](/static/favicon.svg)` renders as a link with no `target`; a
    `![pixel](data:image/png;base64,...)` image renders as an `<img>` with that
    source; and `![vector](data:image/svg+xml,...)` renders as its alternative text
    alone. Opening, in a browser that records every request, the pages that show
    these fields records no request for any of the image URLs and no
    request to any origin but the server's own (see
    [Markdown Rendering](#markdown-rendering), rule 9).
187. **Headings are demoted.** In a Markdown field, `#`, `##`, `###`, and `######`
    headings render as `<h4>`, `<h5>`, `<h6>`, and `<h6>` respectively, and Setext
    headings underlined with `=` and with `-` render as `<h4>` and `<h5>`. No
    rendered Markdown contains an `<h1>`, `<h2>`, or `<h3>` element, and no
    rendered heading carries an `id` attribute (see
    [Markdown Rendering](#markdown-rendering), rule 5).
188. **The task page inserts the renderer's HTML for each Markdown field.** For a
    task with a completion summary and two comments, the served HTML of the task
    page carries, inside a `<div class="markdown">` for each, the Markdown
    renderer's output for `functional_requirements`, `technical_requirements`,
    `acceptance_criteria`, and `completion_summary` and for each comment `body`,
    each equal to the renderer's output for its stored text on this surface. For a
    task whose `completion_summary` is `null`, or for a field that is the empty
    string, the field's card shows the em dash and no `markdown` container. The
    CLI's output of the same task and comments is unchanged and carries no `_html`
    member, no JSON the interface serves carries a task's rendered Markdown, and no
    script the interface serves contains a Markdown parser (see
    [Roadmap Task Page](#roadmap-task-page) and
    [Markdown Rendering](#markdown-rendering), rule 2).
189. **The sprint card holds no interactive element.** A sprint whose `description`
    contains an inline link, a bare `https://` address, a remote image, a task list
    with one checked and one unchecked item, and a footnote renders, in its sprint
    card under its tab, with no `<a>` element other than the card itself and no
    `<input>` element: the link texts and the image's alternative text are
    displayed as text, and the task-list items read `[x]` and `[ ]`. The same
    `description` on the roadmap sprint page renders the links as `<a>` elements and
    the task-list items with disabled checkboxes. The card shows the whole rendered
    description, neither clamped nor truncated (see
    [Shared Sprint-Card Partial](#shared-sprint-card-partial), rule 5, and
    [Markdown Rendering](#markdown-rendering), rule 14).
190. **Footnote identifiers are unique within a page.** A roadmap sprint page whose
    sprint `description` and two sprint comments each define a footnote `[^1]`, and
    a roadmap task page whose four Markdown fields and two comments each define
    one, each contain no two elements with the same `id`. Every footnote reference
    and back-link points at an `id` inside the same field's rendered HTML, and every
    `id` the renderer emits starts with that field's prefix:
    `sprint-<id>-description-`, `sprint-comment-<id>-`, `task-<id>-<field>-`, or
    `task-comment-<id>-` (see
    [Markdown Rendering](#markdown-rendering), rule 12).
191. **The knowledge-graph detail panel is unchanged.** Selecting a node whose
    property value contains `**bold**`, a `# heading` line, and two lines separated
    by a single newline shows that value in the graph detail panel as those literal
    characters, with the author's line break preserved: the panel contains no
    `<strong>` and no heading element for it and no `markdown` container, and the
    value is written through `textContent` (see [Frontend Rules](#frontend-rules),
    rule 6).
192. **The Content-Security-Policy is unchanged by Markdown rendering.** The pages
    that show Markdown fields containing a highlighted code block, a
    `data:` image, and links carry exactly the Content-Security-Policy value fixed
    in Acceptance Criterion 33. The pages introduce no inline script and load no
    highlighting or Markdown script, and every script they load still comes from
    `/static/` (Acceptance Criteria 23 and 33 continue to hold; see
    [Markdown Rendering](#markdown-rendering), rule 15).
193. **Rendered Markdown never forces horizontal scrolling.** On a small phone-sized
    viewport, a Markdown field containing a table of twelve columns and a code block
    with a line of 300 characters, shown on the task page, in a sprint card, and on
    the roadmap sprint page, produces no horizontal overflow of `<body>`, of the
    task page's card, or of the sprint card: the table and the code block each
    scroll horizontally inside their own box (Acceptance Criterion 27 continues to hold;
    see [Markdown Rendering](#markdown-rendering), rule 13).
194. **The renderer is compiled into the binary.** The first `require` block of
    `go.mod` names `github.com/yuin/goldmark`,
    `github.com/yuin/goldmark-highlighting/v2`, and
    `github.com/alecthomas/chroma/v2`, and with networking disabled and only the
    `rmp` binary present on disk, the pages render every Markdown
    construct of Acceptance Criteria 180 to 187, highlighted code included, with no
    file read from the host filesystem for the purpose (see
    [Self-Contained Deliverable](#self-contained-deliverable), rule 6).
195. **Rendered Markdown keeps the body line height and the interface's base
    block spacing.** Parsing `static/style.css` as served from `/static/style.css`
    finds `line-height: var(--tblr-body-line-height)` on the `.markdown`
    container and `margin-bottom: 1rem` on the `.markdown` `table`, and finds no
    rule scoped to `.markdown` that sets a vertical margin — `margin-top`,
    `margin-bottom`, or the top or bottom component of the `margin` shorthand —
    on `p`, `pre`, or `dd`. In a browser, at the default root font size, a
    `.markdown` container on each Markdown surface has a computed `line-height`
    equal to 1.4285714286 times its font size, not the vendored 1.7142857143 times; a top-level
    `p` or `pre` that is not the container's last child has a computed
    `margin-bottom` of `16px`; a top-level `table` that is not the container's
    last child has a computed `margin-bottom` of `16px`, not the vendored `0`; an `li` that is not a task-list item has a computed `margin-top` and `margin-bottom` of `0`; and a
    `p` inside an `li` of a loose list has a computed `margin-bottom` of `16px`
    (see [Markdown Rendering](#markdown-rendering), rule 13).
196. **Lists show the same markers and margins on every surface.**
    `static/style.css` sets, in rules scoped to `.markdown`,
    `list-style-type: disc` on a `ul` inside no other list of the container,
    `circle` on a `ul` inside one list, and `square` on a `ul` inside two or more
    lists; a bottom margin of `1rem` on a list inside no other list of the
    container, and of `0` on a list nested in a list item of the container; and a
    top margin of `0` on both. A task comment `body` holding a three-level
    bulleted list renders, in the task page's Comments card, the
    computed markers `disc`, `circle`, and `square` and a computed
    `margin-bottom` of `16px` on its outer list, the same as the same Markdown in a
    task field, and a sprint comment holding it renders the same markers and
    margins on the roadmap sprint page (see
    [Markdown Rendering](#markdown-rendering), rule 13).
197. **Headings follow the stated scale and spacing, and are never smaller than
    body text.** `static/style.css` sets, under `.markdown`: on `h4`,
    `font-size: 1rem` and `line-height: 1.5rem`; on `h5`, `font-size: .875rem`
    and `line-height: 1.25rem`; on `h6`, `font-size: .875rem`,
    `line-height: 1.25rem`, and the colour `var(--tblr-secondary)`; and on a
    top-level heading, `margin-top: 0`. None of the three font sizes is below
    `.875rem`, the value of `--tblr-body-font-size`. In a browser, a top-level
    heading has a computed `margin-top` of `0`, not the vendored `2.5rem`, and a
    heading that follows a paragraph, a list, a code block, a blockquote, or a
    table is `16px` below it, the preceding block's bottom margin; it has a
    computed `margin-bottom` of
    `8px`, the vendored `var(--tblr-spacer)`; and the rendered HTML still carries
    the heading levels of Acceptance Criterion 187 (see
    [Markdown Rendering](#markdown-rendering), rule 13).
198. **Links in rendered Markdown are distinct and legible, and no other link
    changes.** `static/style.css` gives `.markdown a` the colour
    `rgb(121, 170, 231)`, `text-decoration-line: underline`, and
    `text-underline-offset: .15em`, and gives `.markdown a:hover` and
    `.markdown a:focus-visible` the colour `rgb(148, 187, 237)` and
    `text-decoration-line: none`. The contrast ratios of the two colours against
    `#262626`, computed by the WCAG 2.2 relative-luminance formula, are each at
    least 4.5:1 (6.28:1 and 7.63:1). No rule of `static/style.css` sets the colour
    of an `a` element, or changes a link-colour custom property, through a
    selector that is not scoped to `.markdown`, so the sidebar, navbar,
    page-header, and board links keep their computed colour (see
    [Markdown Rendering](#markdown-rendering), rule 13).
199. **Blockquotes, rules, and footnotes take the interface's base values.**
    `static/style.css` sets, under `.markdown`: on `blockquote`, the `font-size`
    `var(--tblr-body-font-size)`, `margin: 0 0 1rem`, `padding: 1rem 1rem 1rem`,
    and the colour `var(--tblr-secondary)`; on `hr`, `margin: 2rem 0`; and on
    `.footnotes`, `font-size: .8125rem` and the colour `var(--tblr-secondary)`. In
    a browser, a top-level `blockquote` and a top-level `hr` carry these computed
    values, not the vendored `.markdown` values `1rem` font size, `1.5rem 0`
    margin, and `.5rem 1.5rem` padding for the blockquote and `3em` margin for the
    `hr` (see [Markdown Rendering](#markdown-rendering), rule 13).
200. **Task-list items carry a fixed class and no marker, definition descriptions
    are indented, and tabs are four columns wide.** In every Markdown field, the
    `<li>` of a task-list item carries the class `task-list-item`, in the ordinary
    form and in the sprint card's non-interactive form alike; in the sprint card,
    the item's `[x]` or `[ ]` is the text content of a `<span>` carrying the class
    `task-list-marker`, and the item's visible text still reads `[x]` or `[ ]`
    followed by the item's text. No other `<li>` carries `task-list-item`, and no
    other `<span>` carries `task-list-marker`. `static/style.css` sets
    `list-style-type: none` on `.markdown .task-list-item` through a single rule
    that serves both forms, `margin-left: 1.5rem` on `.markdown dd`, and
    `tab-size: 4` on `.markdown pre`. In a browser, the left edge of the checkbox,
    or in a sprint card of the `task-list-marker` span, is at the position the
    marker of an ordinary item of the same list occupies, and an ordinary item of
    the same list keeps its marker (see
    [Markdown Rendering](#markdown-rendering), rules 3, 13, and 14).
201. **Bold is 700, and italic is the real Inter italic face.** `static/style.css`
    sets `font-weight: 700` on `.markdown strong` and `.markdown b`.
    `/static/vendor/inter/inter.css` holds four `@font-face` rules: for each of the
    family names `Inter Var` and `Inter`, one with `font-style: normal` whose source
    is `./files/inter-latin-wght-normal.woff2`, and one with `font-style: italic`
    whose source is `./files/inter-latin-wght-italic.woff2`, all four carrying
    `font-weight: 100 900`. `GET /static/vendor/inter/files/inter-latin-wght-italic.woff2`
    returns HTTP 200 and the committed file's bytes, served from the embedded
    asset set with networking disabled.
    `static/vendor/LICENSES.md` lists the italic file beside the upright one under
    the SIL Open Font License 1.1. In a browser, an `em` element of rendered
    Markdown is drawn from the italic face, which the browser reports as a loaded
    `font-style: italic` face of `Inter` (see [UI Framework](#ui-framework),
    rule 4, and [Markdown Rendering](#markdown-rendering), rule 13).
202. **The roadmap sprint page limits a Markdown line to 80 characters, and the
    roadmap task page does not.** `static/style.css` carries a rule setting
    `max-width: 80ch` whose selector matches the `.markdown` container of the
    sprint `description` and of every sprint comment `body` on the roadmap sprint
    page, and matches no `.markdown` container in a sprint card of the roadmap
    sprints page and none on the roadmap task page. On a desktop-width viewport,
    the sprint description's container on the roadmap sprint page has a computed
    `max-width` equal to 80 times the width of its `0` glyph, while a sprint card's
    container and the containers of the four Markdown fields and of every comment
    `body` on the roadmap task page have a computed `max-width` of `none`, and each
    task-page container's width equals the content width of its card's body (see
    [Markdown Rendering](#markdown-rendering), rule 13).
203. **The typography adds only the task-list classes to the rendered HTML, and
    leaves the safety rules unchanged.** Every rule `static/style.css` adds for
    rendered Markdown has a selector scoped to `.markdown`, no template or
    renderer output carries a `style` attribute for it, and the only markup the
    typography adds to the renderer's output is the `task-list-item` class and
    the `task-list-marker` span of Acceptance Criterion 200. Acceptance Criteria 180 to 194 continue to hold: the line breaks, the
    heading demotion, the Content-Security-Policy, and every safety property of
    rendered Markdown are unchanged (see
    [Markdown Rendering](#markdown-rendering), rule 13).
204. **The sprint page displays its timestamps as `YYYY-MM-DD HH:mm:ss`.** For a
    sprint whose stored `created_at` is `2026-09-28T08:47:32.056Z` and whose
    `started_at` is `2026-09-29T17:03:09.000Z`, the sprint metadata datagrid shows
    `Created` as `2026-09-28 08:47:32` and `Started` as `2026-09-29 17:03:09`. Each
    entry of the Comments card shows its `created_at`, and its `updated_at` when
    that value is not null, in the same form (see
    [Date and Time Display](#date-and-time-display), rules 1 and 8).
205. **The audit log page displays `Performed At` as `YYYY-MM-DD HH:mm:ss`.** An
    audit entry whose stored `performed_at` is `2026-09-28T08:47:32.056Z` shows
    `2026-09-28 08:47:32` in the `Performed At` column (see
    [Date and Time Display](#date-and-time-display), rules 1 and 8).
206. **The task page displays its timestamps as `YYYY-MM-DD HH:mm:ss`.**
    For a task whose stored `created_at`, `started_at`, `tested_at`, and
    `closed_at` are all set, the Details card shows each of the four in the display
    form, and each entry of the Comments card shows its `created_at`, and its
    `updated_at` when that value is not null, in the same form. The form is
    produced on the server by the one Go formatting function every server-rendered
    page uses, and no script formats a timestamp (see
    [Date and Time Display](#date-and-time-display), rule 5).
207. **No displayed timestamp carries `T`, fractional seconds, or `Z`, and none is
    rounded or converted.** On every surface of Acceptance Criteria 204 to 206, the
    text of each displayed timestamp matches the regular expression
    `^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$` — one space between
    the date and the time — and contains no `T`, no `.`, and no `Z`. A stored value of
    `2026-09-28T08:47:59.999Z` displays as `2026-09-28 08:47:59`, not
    `2026-09-28 08:48:00`, and a stored value of `2026-09-28T23:59:59.999Z`
    displays as `2026-09-28 23:59:59`. The displayed digits are the stored UTC
    digits whatever the time zone of the server process and of the browser (see
    [Date and Time Display](#date-and-time-display), rules 2 to 4).
208. **The JSON endpoint and the non-governed values keep their format.** The
    graph data endpoint's response is unchanged by the display rule. A
    knowledge-graph property value holding a date or a time is shown in the graph
    detail panel as delivered, not reformatted, and the CLI's output is unchanged:
    `rmp task get` still publishes every task timestamp in the canonical format of
    `DATA_FORMATS.md § Dates - ISO 8601 with UTC`, byte for byte as stored — for
    example `"created_at": "2026-09-28T08:47:32.056Z"` (see
    [Date and Time Display](#date-and-time-display), rule 9).
209. **Each displayed timestamp carries the stored value in a `datetime`
    attribute.** On every surface of Acceptance Criteria 204 to 206, each displayed
    timestamp is the whole text content of a `<time>` element whose `datetime`
    attribute equals the stored value verbatim: the stored value
    `2026-09-28T08:47:32.056Z` is rendered as
    `<time datetime="2026-09-28T08:47:32.056Z">2026-09-28 08:47:32</time>`. No
    label and no edited marker is inside the element (see
    [Date and Time Display](#date-and-time-display), rule 6).
210. **An unset timestamp keeps its em dash.** A sprint whose `started_at` or
    `closed_at` is unset shows an em dash in that datagrid field, and a task whose
    `started_at`, `tested_at`, or `closed_at` is unset shows an em dash for it in
    the task page's Details card; no `<time>` element is rendered for an unset timestamp. A comment
    whose `updated_at` is null shows no edited marker (see
    [Date and Time Display](#date-and-time-display), rule 7).
211. **A stored timestamp not in the canonical format is displayed unchanged.** On
    every surface of Acceptance Criteria 204 to 206, a set timestamp whose stored
    text is not in the canonical format — for example `2026-09-28 08:47` or
    `<b>yesterday</b>` — is displayed as exactly that text, as visible characters
    and never as markup, with no `<time>` element around it, and the page renders
    the rest of its content normally (see
    [Date and Time Display](#date-and-time-display), rule 7).
212. **Each sprint page load starts its board columns in the state their tasks
    assign.** For a sprint of an existing roadmap, the served HTML of
    `GET /roadmaps/{name}/sprints/{id}` renders the `WAITING`, `DOING`, and
    `CLOSED` columns of the member-tasks board expanded whatever the sprint holds:
    no column element carries the class `task-board__column--collapsed`, and no
    column body carries the `hidden` attribute. Each column element carries
    `data-task-count` equal to the text of its own count badge, and the three
    values sum to the sprint's total number of member tasks. Once the page's
    scripts have initialised:
    - for a sprint with at least one member task and at least one empty column —
      for example tasks in `WAITING` and `CLOSED` and none in `DOING` — each column
      whose `data-task-count` is `0` is collapsed, carrying exactly the state
      Acceptance Criterion 214 states for a collapse (body `hidden`, the class
      `task-board__column--collapsed`, a `3rem` strip, `aria-expanded="false"`,
      the `aria-label` `Expand <HEADING> column`, and the icon
      `ti ti-chevron-right`), and each column holding one or more tasks is
      expanded, with every one of its cards displayed and its toggle carrying
      `aria-expanded="true"`, the `aria-label` `Collapse <HEADING> column`, and the
      icon `ti ti-chevron-left`;
    - for a sprint with no member task, all three columns are expanded, each
      displays its in-column empty state, and each toggle carries
      `aria-expanded="true"`.

    Initialisation moves no keyboard focus and issues no network request. The
    check asserts both cases above, because a script that collapsed every empty
    column, or none, would pass either case alone. The state is not persisted: the
    response sets no cookie for it, the route reads no query parameter for it, and
    the page's scripts read and write no `localStorage`, `sessionStorage`, or
    IndexedDB, so a page reloaded after the reader expanded a column that started
    collapsed, or collapsed one that started expanded, presents again the state
    its tasks assign (see [Sprint Detail Sub-Template](#sprint-detail-sub-template),
    **Column collapse**).
213. **Each column header carries one accessible chevron toggle.** In the served
    HTML, each of the three column headers of the sprint's member-tasks board
    carries exactly one `<button type="button" class="btn-action">` carrying
    `data-role="task-board-column-toggle"`, placed inside a
    `<div class="card-actions">` after the column heading and its count badge, and
    carrying no `tabindex` and no `role`. Each toggle carries
    `aria-expanded="true"`; `aria-controls` equal to the `id` of its own column's
    body — `sprint-board-column-waiting`, `sprint-board-column-doing`, and
    `sprint-board-column-closed` respectively, each of which occurs exactly once in
    the page; `aria-label` `Collapse WAITING column`, `Collapse DOING column`, or
    `Collapse CLOSED column` respectively; and exactly one icon,
    `<i class="ti ti-chevron-left" aria-hidden="true"></i>`. Once the page's
    scripts have run, the toggle is visible and is activated by a pointer click, a
    touch tap, Enter, and Space; it shows a visible focus indicator when it
    receives keyboard focus, and it presents a touch-friendly hit target.
214. **Activating a toggle collapses its column.** Activating the toggle of an
    expanded column of the sprint's member-tasks board sets the `hidden` attribute
    on that column's body, so none of its cards and not its empty state is
    displayed or exposed to assistive technology; adds the class
    `task-board__column--collapsed` to the column element; shrinks the column to a
    strip `3rem` wide that keeps the board's height; shows the column heading and
    its count badge rotated 90 degrees clockwise and reading from top to bottom
    (computed `writing-mode` `vertical-rl`), with the badge's text and colour
    unchanged; and sets the toggle's `aria-expanded` to `false`, its `aria-label`
    to `Expand <HEADING> column`, and its icon to `ti ti-chevron-right`. The toggle
    stays visible and operable at the top of the strip and keeps keyboard focus.
    The columns that remain expanded divide the width the strip frees equally, each
    never narrower than `17rem`. The check measures the column widths before and
    after the collapse, because a collapse that hid the cards but kept the column's
    width would pass a check of the attributes alone.
215. **Activating the toggle again restores the column.** Activating the toggle of
    a collapsed column of the sprint's member-tasks board removes the `hidden`
    attribute from the column's body and the class `task-board__column--collapsed`
    from the column element, shows the heading and its badge horizontally again,
    returns the column to an equal share of the board's width, and shows the same
    cards, or the same empty state, in the same order as before the collapse; a
    column that started collapsed because it holds no task shows its in-column
    empty state. The toggle returns to `aria-expanded="true"`, the `aria-label`
    `Collapse <HEADING> column`, and the icon `ti ti-chevron-left`, and keeps
    keyboard focus. After a collapse and an expand of a column that started
    expanded, or an expand and a collapse of a column that started collapsed, the
    column's markup and its measured width equal those the page presented once its
    scripts had initialised.
216. **The columns toggle independently.** Activating the toggle of one column of
    the sprint's member-tasks board changes that column alone: the other two keep
    their state, their attributes, and their cards. Every combination of collapsed
    and expanded columns is reachable, including all three collapsed, in which the
    board shows three `3rem` strips in the order `WAITING`, `DOING`, `CLOSED`,
    keeps its height, and each strip's toggle still expands its own column; in
    every combination `<body>` produces no horizontal overflow (Acceptance
    Criterion 27 continues to hold). The check asserts the all-collapsed
    combination and at least one combination of collapsed and expanded columns.
217. **Collapsing is presentation only, and the board stays read-only.**
    Collapsing or expanding a column of the sprint's member-tasks board issues no
    network request, changes no card, no count badge, no column order, and no card
    order, and navigates nowhere. The behaviour is served as the embedded script
    `static/sprint-board.js`; the page carries no inline script and no inline
    event-handler attribute, no element of the board carries a `style` attribute
    before or after any toggle is activated, and the Content-Security-Policy of
    Acceptance Criterion 33 is unchanged. Acceptance Criteria 135 and 138 continue
    to hold (see [Sprint Detail Sub-Template](#sprint-detail-sub-template),
    **Column collapse** and **Read-only**).
218. **Without JavaScript the board is expanded and usable, and the tasks page has no
    board.** With scripting disabled, the sprint page shows all three columns of its
    member-tasks board expanded, with every card and every empty state visible, and
    shows no collapse toggle, because each toggle is served with the `hidden` attribute
    and only `static/sprint-board.js` removes it. The roadmap tasks page carries no
    element with `data-role="task-board-column-toggle"` and no element with the class
    `task-board__column--collapsed`, and it does not load `static/sprint-board.js`
    (Acceptance Criterion 81 continues to hold).
219. **The sidebar-to-content gap is the same on every page.** In a browser at the
    viewport widths `992px` and `1440px`, on the roadmap index page, the roadmap
    sprints page, the roadmap tasks page, the roadmap sprint page, the roadmap task
    page, the roadmap audit log page, and the knowledge-graph page, the check
    measures, for each of the top navbar, the page header, and the page body, the
    horizontal distance from the right edge of the sidebar `<aside>` to the left
    edge of that region's content. For each region the distance is identical on
    every page. The set of pages measured MUST include at least one that scrolls
    vertically and at least one that does not, because a scrollbar-dependent shift
    is visible only when the two are compared. No stylesheet served under
    `/static/...` carries a rule that offsets `:root`, `html`, `body`, the page, or
    the page wrapper horizontally by a length derived from the viewport width minus
    the document width, such as `calc(100vw - 100%)` (see
    [UI Framework](#ui-framework), rule 20).
220. **Rendered Markdown is set at body-text size.** `static/style.css` sets,
    under `.markdown`, `font-size: var(--tblr-body-font-size)` on the container and
    `font-size: .85714285em` on `pre`. In a browser, at the default root font size,
    a `.markdown` container on each Markdown surface has a computed `font-size` of
    `14px`, not the vendored `16px`, and a `pre` inside it has a computed
    `font-size` of `12px`, not the vendored `11.375px` (see
    [Markdown Rendering](#markdown-rendering), rule 13).
221. **The task page's header, active view, and way back.** For task 42 of the
    roadmap `payments`, titled `Rotate the signing keys` and in status `DOING`,
    the served HTML of `GET /roadmaps/payments/tasks/42` renders its header
    through the shared page-header partial: the pretitle reads `Task #42`
    followed by a badge whose text is `DOING` and whose class carries
    `bg-blue-lt`, and the title reads `Rotate the signing keys` alone, with no
    badge and no roadmap name. A task in each of the five statuses carries the
    badge variant the task status table assigns to that status. The sidebar's
    Tasks entry is the active one — its `<li>` carries `active` and its `<a>`
    carries `aria-current="page"` — and no other sidebar entry is. The header's
    actions column is `<div class="col-12 col-sm-auto ms-auto d-print-none">` and holds
    exactly one link, labelled `Back to tasks`, whose `href` is exactly
    `/roadmaps/payments/tasks`, with no query string; the page header carries no
    link to the sprint page and none to the knowledge-graph page (see
    [Roadmap Task Page](#roadmap-task-page), **Page header** and **The way back**,
    and [Shared Page-Header Partial](#shared-page-header-partial)).
222. **The Sprint card of a task in a sprint.** For a sprint 7 titled
    `Payments hardening`, in status `OPEN`, holding eleven member tasks of which
    four are `COMPLETED`, the page of the member task stored at `position` `2`
    carries a card whose header title is `Sprint`, and whose body shows, in this
    order: one link whose `href` is `/roadmaps/{name}/sprints/7` and whose text is
    `Sprint #7` followed by `Payments hardening`; outside that link, a badge
    reading `OPEN` whose class carries `bg-blue-lt`; the text `Position 3 of 11`;
    the text `4 of 11 tasks completed`; and a `<progress>` element carrying the
    classes `progress` and `progress-sm`, the `value` `4`, the `max` `11`, and
    `aria-label="Sprint progress"`, with no `style` attribute. The first member
    task in position order reads `Position 1 of 11`, and the last reads
    `Position 11 of 11`. A member task whose status is `BACKLOG` shows the same
    sprint form, not the backlog text, and a task of a sprint with no `COMPLETED`
    member reads `0 of <m> tasks completed` with a `value` of `0`. The card shows
    no sprint description, no sprint timestamp, no member task, and no sprint
    comment (see [Roadmap Task Page](#roadmap-task-page), **Sprint card**).
223. **The Sprint card of a task in no sprint.** For a task that belongs to no
    sprint, the card whose header title is `Sprint` shows exactly the text
    `In the backlog: this task belongs to no sprint.` and contains no `<a>`
    element, no badge, no position text, and no `<progress>` element. Moving the
    same task into a sprint with `rmp sprint add-tasks` and requesting the page
    again shows the sprint form of Acceptance Criterion 222, because the page reads
    the membership on every request (see [Roadmap Task Page](#roadmap-task-page),
    **Sprint card**, and [Cache Policy](#cache-policy)).
224. **The Details card holds the task's short fields.** The task page carries a
    card whose header title is `Details` and whose body is a Tabler datagrid
    holding exactly thirteen fields, labelled, in this order, `Type`, `Severity`,
    `Priority`, `Parent task`, `Subtasks`, `Depends on`, `Blocks`, `Created`,
    `Started`, `Tested`, `Closed`, `Commit open`, and `Commit close`. The `Type`
    value is the `TaskType` value as plain text, not inside a badge. The
    `Priority` and `Severity` values are badges whose text is the bare integer —
    `7`, not `P7`, and `2`, not `S2` — carrying the variant the priority and
    severity bands assign. For a task 42 of the roadmap `payments` whose
    `parent_task_id` is `40`, whose `depends_on` is `[12, 17]`, and whose `blocks`
    is `[51]`, `Parent task` holds exactly one `<a>` element, with the text `#40`
    and the `href` `/roadmaps/payments/tasks/40`; `Depends on` holds exactly two,
    `#12` and `#17` in that order, with the `href`s `/roadmaps/payments/tasks/12`
    and `/roadmaps/payments/tasks/17`; and `Blocks` holds exactly one, `#51`, with
    the `href` `/roadmaps/payments/tasks/51`. None of them carries a `target` or a
    `rel`, and following each one returns HTTP 200 and the referenced task's page.
    A field showing the em dash holds no `<a>` element. `Subtasks` shows `0` for a
    task with no subtask. A null `parent_task_id`, an empty `depends_on` or
    `blocks`, an unset `started_at`, `tested_at`, or `closed_at`, and an absent
    `commit_open` or `commit_close` each show an em dash, and a set commit hash is
    shown as stored, in full, in a monospaced font, with no `<a>` element: a
    64-character hash shows all 64 characters, wrapping inside its column rather
    than overflowing it, with no ellipsis and a computed `text-overflow` that is not
    `ellipsis`. The datagrid
    carries no `ID`, `Title`, or `Status` field, because the page header states
    them (see [Roadmap Task Page](#roadmap-task-page), **Details card**).
225. **The long fields and the comments fill the main column, in order.** The
    task page's page body holds one `row row-cards` of two columns. The first in
    the document carries `col-12 col-lg-4` and `order-lg-last` and holds the
    Sprint card followed by the Details card; the second carries `col-12 col-lg-8`
    and holds, in this order, the cards titled `Functional requirements`,
    `Technical requirements`, `Acceptance criteria`, and `Completion summary`,
    followed by the Comments card, which is the last card of the column. Each of
    the four field cards is present for every task, a field that is empty or null
    showing the em dash. In a browser at a viewport width of `1440px`, the side
    column's left edge lies to the right of the main column's right edge (see
    [Roadmap Task Page](#roadmap-task-page), **Layout**).
226. **The task page's read cost is fixed.** An instrumented count of the queries
    the task page issues is at most five for a task that belongs to a sprint and
    three for a task that belongs to no sprint, and it is the same for a task with
    no comment and for a task with fifty, and for a sprint of one member task and
    a sprint of fifty. The page issues exactly one comment-listing query, for this
    task's comments, and no comment query for any other task. Serving the page
    produces no audit-log entry (Acceptance Criterion 20 continues to hold; see
    [Roadmap Task Page](#roadmap-task-page), **Read cost**).
227. **The task page is usable on a phone.** In a browser at a viewport width of
    `375px`, for a task whose title is 200 characters long, whose
    `acceptance_criteria` holds a table of twelve columns, whose `depends_on`
    lists twelve tasks, and which belongs to a sprint, the page produces no
    horizontal overflow of `<body>`; the two columns stack into one, so the Sprint
    card, the Details card, the four field cards, and the Comments card appear from
    top to bottom in that order, each spanning the width of the page body; the
    title wraps within the page header; and the table scrolls horizontally inside
    its own box. The `Back to tasks` link and the Sprint card's sprint link each
    present a touch-friendly hit target (Acceptance Criterion 27 continues to hold;
    see
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
    rule 11).
228. **The task page never scrolls horizontally, and its datagrid has the
    specified column count.** For a task whose title is 200 characters long, whose
    `commit_open` is 64 characters long, and whose `depends_on` lists twelve tasks,
    the task page produces no horizontal overflow of `<body>` at the viewport
    widths `320px`, `375px`, `576px`, `768px`, `991px`, every integer width from
    `992px` to `1056px` inclusive, `1280px`, `1440px`, and `1920px`. At every
    width below `992px` the Details card's datagrid lays its fields out in exactly
    two columns of equal width, filled row by row in the order of Acceptance
    Criterion 224, and at every width from `992px` up it lays them out in exactly
    one column, measured in a browser as the number of distinct left edges among
    the datagrid's items. The order of the cards is the order Acceptance
    Criteria 225 and 227 fix at every width (see
    [Roadmap Task Page](#roadmap-task-page), **Details card**, and
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design),
    rule 11).
229. **Keyboard focus on the header back links and the sidebar links is clearly
    visible.** In a browser, moving keyboard focus with Tab onto the task page's
    `Back to tasks` link, onto the sprint page's link back to the sprints page, and
    onto each sidebar `nav-link` gives the focused element a computed `outline`
    whose style is `solid`, whose width is at least `2px`, and whose colour is not
    transparent, and that colour has a contrast ratio of at least 3:1 against the
    computed background colour of the element and against that of its container.
    The same element focused by a pointer click that does not match
    `:focus-visible` carries no such outline. No template carries a `style`
    attribute for it (see [UI Framework](#ui-framework), rule 21).
230. **The record pages' header actions wrap below the title on a narrow
    viewport.** On the Roadmap Task Page and on the Roadmap Sprint Page, the page
    header's actions column is
    `<div class="col-12 col-sm-auto ms-auto d-print-none">`. In a browser at a viewport width of `375px`, the actions
    column's top edge lies at or below the title column's bottom edge, and the
    title column's width equals the width of the header row; at `576px` and wider,
    the two columns share one row, the actions column's top edge lying above the
    title column's bottom edge. The page headers of the other pages keep
    `col-auto` (Acceptance Criterion 78; see [UI Framework](#ui-framework),
    rule 16).
231. **Both Comments cards state their order.** On the Roadmap Task Page and on the
    Roadmap Sprint Page, the Comments card's `card-header` holds, after the card
    title and its count badge, one `<div class="card-actions">` carrying the class
    `text-secondary` whose text is exactly `Oldest first`, for a record with
    comments and for a record with none. The element contains no link, no button,
    and no form control (see [Roadmap Task Page](#roadmap-task-page), **Comments
    card**, and [Sprint Detail Sub-Template](#sprint-detail-sub-template), rule 4).
232. **The list card follows the three Tabler examples it is modelled on.** In the
    served HTML of the tasks page, for a request that lists at least one task: the list
    card is a `card` whose first child is
    `<div class="card-header flex-wrap gap-2">`; that header's first child is a `<div>`
    holding exactly one `<h2 class="card-title">` reading `Task list` and no
    `card-subtitle`, and its second child is a `<div class="card-actions">` holding the
    filter bar's `<form>` and nothing else; the header is followed by a
    `table-responsive` container holding a `<table>` carrying `table`, `table-vcenter`,
    and `card-table` and not `table-selectable`, whose `<thead>` row holds the headings
    `ID`, `Title`, `Type`, `Status`, `Severity`, `Priority`, and `Created`, in that
    order and no other — no `Sprint` and no `Actions` heading — the `ID` heading
    carrying `w-1`; and the card ends with a `card-footer` holding one
    `<div class="row g-2 align-items-center">` of exactly three `col-auto` columns — the
    range text, the rows-per-page selector, and the pagination bar, in that order — the
    third also carrying `ms-auto`. The check asserts this structure element by element,
    because a list that renders the right rows in a different card structure satisfies
    every other criterion of the list (see [Roadmap Tasks Page](#roadmap-tasks-page),
    **Modelled on three Tabler examples** and **The card header**).
233. **A UX specialist validates the rendered page against the Tabler examples.** A
    UX specialist renders the tasks page in a browser, in the interface's dark theme,
    at the viewport widths `375px`, `992px`, and `1440px`, for a roadmap of at least 60
    tasks spread over every status and several sprints, and compares it with Tabler's
    examples `https://preview.tabler.io/cards.html` (the card as a whole),
    `https://preview.tabler.io/card-actions.html` (the card header), and
    `https://preview.tabler.io/tasks-list.html` (the table and its rows). The page
    passes when every difference the specialist records is one of the six deviations
    **Modelled on three Tabler examples** permits — one card rather than one per group,
    no selection, no add-task control and no modal, the roadmap's own columns, the
    added filter bar, pagination, and rows-per-page selector, and no per-row `View`
    button — and when the filter bar
    in the card header, the rows, and the card footer read as Tabler's own components at
    each of the three widths. The specialist records the result, with a screenshot at
    each width, in the task that implements the page; any other difference fails the
    criterion and is corrected in the page, not in the record (see
    [Roadmap Tasks Page](#roadmap-tasks-page)).
234. **The filter bar's controls are fixed.** The filter bar's form carries, in this
    order: `<input type="search" name="q">` labelled `Search`; `<select name="sprint">`
    labelled `Sprint`, offering `Any sprint` with an empty value, `No sprint` with the
    value `none`, and one option per sprint of the roadmap, in ascending sprint `Order`,
    whose value is the sprint's `id` and whose text is `Sprint #<id>` followed by a space
    and the sprint's `title`; the status dropdown, `<div class="dropdown">` holding
    `<button type="button" class="form-select form-select-sm" data-bs-toggle="dropdown" data-bs-auto-close="outside" aria-describedby="<ID>">`
    labelled `Status`, holding its text in `<span id="<ID>">`, and a `<div class="dropdown-menu">` of
    `<label class="dropdown-item">` elements each holding
    `<input type="checkbox" class="form-check-input" name="status" value="<VALUE>">`;
    the type dropdown, of the same markup with `name="type"`, labelled `Type`; and
    the Apply button — and no other
    control, with the checkboxes Acceptance Criterion 112 fixes. On every response
    the sprint select marks as `selected`
    exactly one option: the one equal to the active `sprint`, or its first
    option when there is none; each dropdown marks as `checked` exactly the boxes of
    its dimension's active values, and its toggle reads `Any status` (`Any type`)
    when none is checked, the value itself when one is, and `<n> selected` when
    `<n>` of two or more are; and the search input's `value` is
    the active `q`. The check covers a dimension with zero, one, two, and every value
    active, and it covers a roadmap with no sprint, whose sprint
    select offers `Any sprint` and `No sprint` only, and a roadmap with two sprints of
    the same `title`, whose two options differ by their `Sprint #<id>` text (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Filter bar**).
235. **The sprint filter selects by membership.** For a roadmap whose tasks are spread
    over two sprints and the backlog, `?sprint=none` lists exactly the tasks that belong
    to no sprint, whatever their status — a `BACKLOG` task that is a member of a sprint
    excluded; `?sprint=<id>` lists exactly the member tasks of that sprint, whatever
    their status; and the lists of `none` and of every sprint `id` together hold every
    task of the roadmap exactly once. `?sprint=<id>&status=DOING` lists exactly that
    sprint's `DOING` tasks. A `sprint` naming a sprint of another roadmap is ignored, as
    Acceptance Criterion 115 fixes (see [Roadmap Tasks Page](#roadmap-tasks-page),
    **What each filter matches, and how the criteria compose**).
236. **Every parameter is validated, and an ignored one is visible as *any*.** For each
    of the three filter parameters and for each unacceptable value Acceptance
    Criterion 115 lists, the response is HTTP 200, carries `Cache-Control: no-store`,
    lists exactly the tasks the same request lists without that parameter, shows the
    parameter's *any* state — the sprint select's first option `selected`, or no box
    of the dropdown `checked` — and carries the ignored value in no link the
    page generates. A request whose query string carries parameters not among the six —
    `priority` and `severity` among them — lists exactly what it lists without them,
    and no link the page generates carries them. No parameter value, alone or combined with
    others, produces an HTTP status other than 200 for an existing roadmap (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Query parameters**, and
    [Routes and Pages](#routes-and-pages)).
237. **Applying the filters returns to page 1 and keeps the page size.** On page 3 of
    a list at page size `10`, submitting the filter bar with a new status box checked
    requests a URL
    that carries `size=10` and no `page`, and renders page 1 of the newly filtered list
    at page size `10`. Submitting the form again from that list with the sprint select
    on `Any sprint`, every box unchecked, and an empty search input renders page 1 of
    the unfiltered list at page size `10`. The check submits the real form — its fields as the served
    HTML defines them — rather than composing the URL itself, because a form that
    carried a `page` field, or no `size` field, would fail only when submitted (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Filter bar**).
238. **The filters and the search compose on the server in every combination.** For a
    roadmap of at least 40 tasks whose values of `sprint` membership, `status`, `type`,
    and `title` are varied so that each criterion excludes a different subset, the
    check requests every one of the 16 combinations of the four criteria — sprint,
    status, type, and search term — being present or absent, each with a fixed accepted value, and asserts
    that each lists exactly the tasks satisfying every present criterion, in the order
    of Acceptance Criterion 84, with the range text stating their number. The expected
    lists are computed by the check from the task data it created, not read back from
    the page (see [Roadmap Tasks Page](#roadmap-tasks-page), **What each filter
    matches, and how the criteria compose**).
239. **The list is paginated on the server at the requested page size.** For 60 tasks
    matching a request, `size` `10`, `25`, `50`, and `100` give 6, 3, 2, and 1 pages
    respectively, and each page `p` holds exactly the rows at positions
    `(p - 1) × size + 1` to `min(p × size, 60)` of the order of Acceptance Criterion 84.
    A request with no `size` renders 25 rows per page. Only the rows of the requested
    page are in the served HTML: the rows of other pages are not present, hidden or
    otherwise (see [Roadmap Tasks Page](#roadmap-tasks-page), **Pagination**).
240. **An unacceptable `page` or `size` falls back, and a page beyond the last renders
    the last.** For a list of 3 pages, each of `page=0`, `page=-1`, `page=abc`,
    `page=1.5`, `page=02`, `page= 2`, and an empty `page` renders page 1, and each of
    `page=4`, `page=999`, and `page=99999999999999999999999` renders page 3, the range
    text and the pagination bar's active item agreeing with the page rendered. Each of
    `size=20`, `size=0`, `size=-10`, `size=abc`, `size=025`, and an empty `size` renders
    25 rows per page, with the rows-per-page selector marking `25` as active. Every one
    of these requests answers HTTP 200. A `page` beyond the last page of a filtered list
    renders the last page of that filtered list, not of the roadmap's (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Query parameters** and
    **Pagination**).
241. **Every generated link keeps the active filters.** On a filtered list, every
    link the page generates to itself — each page number and chevron of the pagination
    bar, and each rows-per-page link — carries each filter of the active filter
    state, whether the URL, the cookie, or the defaults supplied it: the active
    `sprint`, and one `status` or `type` occurrence per active value, every active
    value included — for `?status=DOING&status=TESTING&size=10`, each link carries
    both `status=DOING` and `status=TESTING`. It carries no ignored value and no
    parameter the page does not accept, `priority` and `severity` included. On a
    bare request answered from the defaults, every generated link carries the four
    default status values. A generated link that would carry no parameter at all
    carries `size=25`. The no-match empty state's Reset link carries the four
    default status values and no other filter parameter and no `q`
    (Acceptance Criterion 88). A pagination link carries `page` only when its
    target is greater than `1`, and carries `size` only when the active page size is
    not `25`; a rows-per-page link carries its own `size` only when it is not `25`, and
    carries no `page`; no generated link carries `q` when the term is empty after the
    trim. Following each such link and parsing the resulting page shows the same
    filters applied and the page and page size the link named (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Pagination**, **Links keep the
    filters**).
242. **The pagination bar and the rows-per-page selector are Tabler's.** The pagination
    bar sits inside `<nav aria-label="Task list pages">` in the card footer's third
    column and follows, for every page count from 1 to 20 and every current page, the
    sliding window with ellipsis, the chevrons, and the markup of the audit log page's
    bar (see [Roadmap Audit Log Page](#roadmap-audit-log-page), **Sliding window with
    ellipsis** and **Pagination markup**): a list of one page shows its single page as
    the active item. The rows-per-page selector sits in the footer's second column: the
    visible text `Rows per page`, then a `<div class="btn-group" role="group">` whose
    accessible name is that text, holding four links carrying `btn` and `btn-sm` and
    reading `10`, `25`, `50`, and `100` in that order, the link of the active page size
    alone carrying `active` and `aria-current="true"`. Neither appears when the request
    lists no task (Acceptance Criterion 88 continues to hold; see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Pagination**, and
    [UI Framework](#ui-framework), rule 15).
243. **The filter bar is compact and trailing-aligned.** In the served HTML of the
    tasks page, the search input carries `form-control` and `form-control-sm` and the
    attribute `placeholder="Search"`; the sprint select and the two dropdown toggle
    buttons each carry `form-select`
    and `form-select-sm`; and the Apply button carries `btn`, `btn-primary`, and
    `btn-sm`. Each of the four filter controls — the search input, the sprint
    select, and the two dropdown toggle buttons — has exactly one `<label>` whose `for` equals the control's `id`, and
    every such label carries `visually-hidden`. The sprint select carries a class for
    which `static/style.css` declares `max-width: 16rem` inside a media query whose
    condition is `min-width: 576px`, and declares no `max-width` for it outside one. The form carries
    `justify-content-end`. The list card's `card-actions` container keeps Tabler's own
    position: no rule of `static/style.css` that matches it, and no class of the
    template on it, overrides the vendored `.card-actions` margins. Rules that match
    only another card's `card-actions` — the sprint board's
    `.task-board__column--collapsed .card-actions` among them — are outside this
    check. In a browser, at a viewport width of `1440px`, for a
    roadmap whose sprint titles include one of at least 60 characters, the card title
    and every control of the bar lie on one line, the right edge of the bar's last
    control coincides with the right edge of the `card-actions` container, placed as
    in Tabler's card-actions example, and no label is displayed; at `992px` and
    `576px` each line of controls ends at that same trailing edge. At each of `375px`,
    `576px`, `992px`, and `1440px`, the sprint select, the two dropdown toggles, the
    search input, and the Apply
    button have one computed rendered height, to the pixel; at `1440px` and `576px` the
    sprint select is no wider than `16rem`, and at `375px` it is as wide as the search
    input, filling its row (Acceptance Criterion 129
    continues to hold; see [Roadmap Tasks Page](#roadmap-tasks-page), **Filter bar**,
    **Compact controls**, and **Labels are programmatic, not visible**).
244. **The title column is not squeezed.** In the served HTML of the tasks page no
    cell of the table carries `text-break`; every `Title` cell carries a class for
    which `static/style.css` declares `min-width: 16rem`. In a browser, for a roadmap
    holding a task whose title is at least 120 characters of ordinary words, at each of the viewport widths `375px`, `576px`, `992px`,
    and `1440px`: every rendered `Title` cell is at least `16rem` wide; no title is
    broken inside a word; and where the table is wider than the card, it scrolls horizontally inside its `table-responsive` container while `<body>`
    produces no horizontal overflow (Acceptance Criterion 128 continues to hold; see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Column widths**).
245. **The active page size is shown by more than colour, with sufficient
    contrast.** In the served HTML the link of the active page size alone carries
    `active` and `aria-current="true"` (Acceptance Criterion 242 continues to hold).
    In a browser, in the dark theme the interface serves, the active link's
    computed background colour is opaque and differs from that of every inactive
    link; that fill has a
    contrast ratio of at least 3:1 against the card footer's computed background and
    against an inactive link's; and the active link's text has a contrast ratio of at
    least 4.5:1 against the fill. The ratios are computed as Acceptance
    Criterion 246 defines (see [Roadmap Tasks Page](#roadmap-tasks-page),
    **Pagination**, **The active size is not shown by colour alone**).
246. **Every badge variant, and the search placeholder, has text contrast of at
    least 4.5:1 in the dark theme.** A Go test reads the two stylesheets as the
    server serves them — `/static/vendor/tabler/tabler.min.css`, then
    `/static/style.css`, the order in which every page loads them — and resolves, in
    the dark theme, every badge variant the interface renders (Status, Priority, and
    Severity Badge Colours, rule 5): every variant of the four mapping tables,
    `bg-secondary-lt`, and `bg-black` with `text-white`. For each variant the test:
    1. takes, among the declarations of `color` and `background-color` whose
       selector matches an element carrying that variant's class under
       `<html data-bs-theme="dark">`, the one that wins by the CSS cascade —
       importance, then specificity, then order of appearance;
    2. resolves every `var()` reference against the custom properties declared for
       the root element in the dark theme, by the same cascade, using the reference's
       fallback where the property is undeclared, and resolves every `calc()` of an
       opacity property the badge does not set with that property's default of `1`;
    3. resolves `light-dark(<light>, <dark>)` to `<dark>`, the `color-scheme` the
       vendored stylesheet declares for `data-bs-theme="dark"` being `dark`;
    4. evaluates `color-mix(in oklab, <colour> <p>%, transparent)` as `<colour>` with
       an alpha of `<p>/100`, and converts every `oklch()`, `oklab()`, `rgb()`, and
       hexadecimal colour to sRGB by CSS Color Module Level 4;
    5. composites the fill over each surface of rule 5 — `--tblr-bg-surface` and
       `--tblr-body-bg`, resolved the same way — and then a translucent text colour
       over the result, by source-over alpha compositing in sRGB; and
    6. computes the contrast ratio of the text colour to the composited background
       by WCAG 2.2's definitions of relative luminance and contrast ratio.

    By the same steps, the test resolves the `color` of the winning `::placeholder`
    declaration for an element carrying `form-control` and `form-control-sm` — the
    tasks page's search input — and the `background-color` of that element,
    composited over `--tblr-bg-surface`, the card surface on which the input sits,
    and computes their contrast ratio.

    The test fails when any ratio, over either surface, is below 4.5:1, and it
    fails — rather than skipping the variant — when a value cannot be resolved by
    these steps. A browser confirms the test: on the tasks page and on a sprint page
    whose board holds tasks of every status, type, priority band, and severity band,
    the computed `color` and `background-color` of one badge of every variant,
    composited as in step 5, meet 4.5:1, and on the tasks page the empty search
    input's computed `::placeholder` colour meets 4.5:1 against the input's computed
    background (see also [Roadmap Tasks Page](#roadmap-tasks-page), **Labels are
    programmatic, not visible**). Every badge still carries the variant class
    the mapping tables assign (Acceptance Criterion 61 continues to hold; see
    [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours),
    rule 5).
247. **The focus indicator reaches 3:1 on the tasks list and the sprint board.** In
    a browser, in the dark theme the interface serves, moving keyboard focus with
    Tab onto each kind of focusable element that UI Framework rule 21 names for the
    tasks page and for the sprint board — the search input, the sprint select, each
    dropdown toggle, a checkbox of an open dropdown menu, Apply, the
    no-match empty state's Reset link, a row's title link, a pagination link, the active and
    an inactive rows-per-page link, a board card, and a column toggle — gives the
    element a computed `outline` whose style is `solid`, whose width is at least
    `2px`, and whose colour has a contrast ratio of at least 3:1 against the computed
    background of the element and against that of its container, computed as
    Acceptance Criterion 246 defines. A Go test confirms, from the served
    stylesheets resolved by the steps of Acceptance Criterion 246, that the outline
    colour the winning `:focus-visible` rule declares for each of those elements
    reaches 3:1 against the backgrounds those rules resolve to. The same element
    focused by a pointer click that does not match `:focus-visible` carries no such
    outline, and no template carries a `style` attribute for it (Acceptance
    Criterion 229 continues to hold; see [UI Framework](#ui-framework), rule 21).
248. **Status and type are multi-value filters, OR within a dimension and AND
    across.** For a roadmap whose tasks cover every status and several types,
    `?status=DOING&status=TESTING` lists exactly the tasks whose status is `DOING`
    or `TESTING`; `?status=DOING&status=TESTING&type=BUG&type=EPIC` lists exactly
    those of them whose type is `BUG` or `EPIC`; and `?status=DOING&status=DOING`
    lists exactly what `?status=DOING` lists. In each response the dropdowns mark
    as `checked` exactly the boxes of the values requested, and the toggles read
    `2 selected` for two values and the value itself for one. Adding a second value
    to a dimension never removes a task from the list; adding a first value to a
    dimension never adds one (see [Roadmap Tasks Page](#roadmap-tasks-page), **Query
    parameters** and **What each filter matches, and how the criteria compose**).
249. **A bare request with no cookie applies the defaults, and Reset restores
    them.** `GET /roadmaps/{name}/tasks` with no query string and no
    `rmp_tasks_filters` cookie lists page 1 of every task whose status is not
    `COMPLETED`, at 25 rows per page, with the status boxes `BACKLOG`, `SPRINT`,
    `DOING`, and `TESTING` checked, `COMPLETED` unchecked, the status toggle
    reading `4 selected`, no type box checked and the type toggle reading
    `Any type`, the sprint select on `Any sprint`, and an empty search input; its
    response carries no `Set-Cookie` header. On a roadmap whose tasks are all
    `COMPLETED`, that request renders the no-match empty state, `No task matches
    the filters`; on a roadmap holding no task, it renders `No tasks yet` with no
    Reset link, and so does a bare request whose cookie filters by status, and an
    explicit `?status=DOING`. Following the
    no-match empty state's Reset link lists the same default list, and its response
    sets the cookie to the value
    `status=BACKLOG&status=SPRINT&status=DOING&status=TESTING&size=25` (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Filter persistence** and
    **Empty states**).
250. **An explicit request sets the cookie, with fixed attributes and value.** The
    HTTP 200 response to `GET` and to `HEAD` of
    `/roadmaps/{name}/tasks?q=cache&sprint=<id>&status=TESTING&status=DOING&type=BUG&size=50&page=2`
    carries exactly one `Set-Cookie` header, naming `rmp_tasks_filters`, whose
    attributes are exactly `Path=/`, `Max-Age=31536000`, `HttpOnly`, and
    `SameSite=Lax` — no `Domain`, no `Expires`, and no `Secure` — and whose value
    is `q=cache&sprint=<id>&status=DOING&status=TESTING&type=BUG&size=50`: the status
    values in enum order, `size` present, and no `page`. A term holding a space, a
    `;`, a `,`, or a `"` is percent-encoded so that the value holds only the
    characters **Filter persistence** lists. A request carrying only `?size=25`
    sets the value `size=25`, and a request carrying only unaccepted occurrences,
    such as `?status=doing`, sets `size=25` likewise. The same explicit request for
    a roadmap that does not exist answers `404` and sets no cookie (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Filter persistence**).
251. **A bare request restores the stored state, validated for the roadmap
    viewed.** After the explicit request of Acceptance Criterion 250, a request to
    the same roadmap's `/roadmaps/{name}/tasks` with no query string, carrying the
    cookie that request set, lists page 1 of exactly the list that request
    filtered, at 50 rows per page, with every control showing that state, and its
    response carries no `Set-Cookie` header; a request whose query string carries
    only `priority=3` is bare and renders the same. The same cookie sent to a
    second roadmap that has no sprint with that `id` applies `q`, `status`, `type`,
    and `size` and ignores `sprint`, the sprint select showing `Any sprint`. A
    cookie holding `status=doing`, `type=BUG,EPIC`, `sprint=007`, `size=20`,
    `page=3`, a malformed percent-encoding, and an accepted `status=DOING` lists
    exactly the `DOING` tasks at 25 rows per page, page 1; a cookie with no accepted
    part lists every task, `COMPLETED` tasks included, not the defaults. Every one
    of these requests answers HTTP 200 (see [Roadmap Tasks Page](#roadmap-tasks-page),
    **Filter persistence**).
252. **An oversized cookie value is not written.** With a cookie already set to
    `status=DOING&size=25`, an explicit request whose `q` makes the encoded value
    longer than 4000 bytes renders the list its URL names and carries no
    `Set-Cookie` header; a following bare request carrying the browser's cookie
    lists the `DOING` tasks, as the earlier cookie states. An explicit request whose
    encoded value is exactly 4000 bytes long sets the cookie (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Filter persistence**, **The size
    limit**).
253. **The tasks route varies by cookie and is never answered from a cache.**
    Every response of `/roadmaps/{name}/tasks` — to an explicit request, to a bare
    request with and without the cookie, and a `404` — carries `Vary: Cookie` and
    `Cache-Control: no-store`, and none carries an `ETag` or a `Last-Modified`
    header; a `POST` to the route is answered `405` with no `Vary` header.
    A request carrying `If-None-Match: *` or an `If-Modified-Since` date in
    the future is answered HTTP 200 with the full page, never `304`. For each of
    those requests, the `HEAD` response carries the same header names and values as
    the `GET` response, `Set-Cookie` included where the `GET` carries it, and no
    body (see [Cache Policy](#cache-policy), rule 5).
254. **The cookie is data, not markup, and grants nothing.** A cookie whose `q`
    decodes to `<script>alert(1)</script>` renders it as visible characters in the
    search input's `value` and introduces no element into the page; a cookie
    carrying `status=DOING' OR '1'='1` or `sprint=1 OR 1=1` has that part ignored,
    and the captured SQL text contains no cookie value. After every request of
    Acceptance Criteria 249 to 253, the roadmap's `project.db` holds the same rows
    and the same audit entries as before. The sidebar's Tasks entry and the task
    page's `Back to tasks` link carry no query string on every page (see
    [Roadmap Tasks Page](#roadmap-tasks-page), **Filter persistence**, and
    [Security and Constraints](#security-and-constraints), rules 7 and 12).

## See Also

- CLI command contract for `web` → `COMMANDS.md § Web Interface`
- Canonical timestamp format the log records share with every other Groadmap
  date → `DATA_FORMATS.md § Dates - ISO 8601 with UTC`
- The stdout-is-JSON / stderr-is-diagnostics split the log obeys →
  `ARCHITECTURE.md § Error Handling`
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
- The same statement time budget applied to a statement sent by `rmp graph client`, what a cut
  statement leaves on disk, and the exit code it reports →
  `GRAPH.md § Statement Time Budget`
- The rule this endpoint follows to decide whether a roadmap is served, and the
  server whose existence makes it necessary → `GRAPH.md § Server Resolution` and
  `GRAPH.md § The Dedicated Graph Server`
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
  page, and the task page → `MODELS.md` and `DATABASE.md`
- `TaskComment` and `SprintComment` fields, the comment type values, the comment
  read queries and their chronological ordering, and the grouped count that gives
  each sprint board card its comment number without reading a body →
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
- Task status enum and lifecycle that fix the values and the order of the tasks
  page's status filter, and the CHECK constraint that admits no other status →
  `MODELS.md § Enums`, `STATE_MACHINE.md § Task State Machine`, and
  `DATABASE.md § tasks Table`
- Default task ordering that the tasks page's list extends with `id` as its final
  key, and the listing its filters add bound predicates to →
  `DATABASE.md § Main SQL Queries` ("List All")
- The sprint read behind the tasks page's sprint filter →
  `DATABASE.md § List Sprint Titles`
- Lifecycle timestamps that order the `DOING` and `CLOSED` columns of the sprint
  page's member-tasks board, and the planned order that breaks their ties →
  `MODELS.md § Task`, `STATE_MACHINE.md § Date Tracking Fields`, and
  `DATABASE.md § List Sprint Tasks Ordered by Position`
- Task status and task type enums that fix the accepted values of the tasks
  page's filters → `MODELS.md § Enums`
- CLI filters whose meanings the tasks page's filters reuse — `-y, --type` as an
  equality →
  `COMMANDS.md § List Tasks`
- Keyboard operability of a task card and of a task row: why the sprint board's
  card is a link with an `href`, why a row of the tasks page's list is not a link
  and carries one, its title, and why no script may be added to compensate →
  [Sprint Detail Sub-Template](#sprint-detail-sub-template),
  [Roadmap Tasks Page](#roadmap-tasks-page), [Security Headers](#security-headers),
  and [Frontend Rules](#frontend-rules)
- Sprint membership and the `BACKLOG` status, which decide the form of the task
  page's Sprint card, and the density of in-sprint positions its position line
  relies on → `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status` and
  `DATABASE.md § Position Density Within a Sprint`
- Sprint membership, by which the tasks page's sprint filter narrows its list and
  which the task page shows, the `UNIQUE` constraint that limits a task to one
  sprint, and the grouped query that resolves the sprint of a set of tasks in one
  round trip, which the task page uses → `MODELS.md § Sprint`,
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
- The Go modules the Markdown renderer is built from, and the rules that pin
  them → `BUILD.md § External Dependencies` and
  `BUILD.md § Markdown Rendering Rules`
- Help skeleton for `web` → `HELP.md`
