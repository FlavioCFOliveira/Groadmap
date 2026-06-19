# Web Interface

## Table of Contents

- [Overview](#overview)
- [Functional Requirements](#functional-requirements)
- [Command Surface](#command-surface)
- [Server Lifecycle](#server-lifecycle)
- [Startup Schema Migration](#startup-schema-migration)
- [Bind Address and Port Selection](#bind-address-and-port-selection)
- [HTTP Server Timeouts](#http-server-timeouts)
- [Security Headers](#security-headers)
- [Cache Policy](#cache-policy)
- [Routes and Pages](#routes-and-pages)
  - [Roadmap Index Page](#roadmap-index-page)
  - [Roadmap Sprints Page](#roadmap-sprints-page)
  - [Roadmap Tasks Page](#roadmap-tasks-page)
  - [Roadmap Sprint Page](#roadmap-sprint-page)
  - [Roadmap Audit Log Page](#roadmap-audit-log-page)
  - [Shared Sprint-Card Partial](#shared-sprint-card-partial)
  - [Sprint Detail Sub-Template](#sprint-detail-sub-template)
  - [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)
  - [Graph Query Bar](#graph-query-bar)
  - [Query-Bar Error Handling](#query-bar-error-handling)
  - [Graph Labels Sidebar](#graph-labels-sidebar)
  - [Graph Data Endpoint](#graph-data-endpoint)
  - [Static Assets](#static-assets)
  - [Task Detail Modal](#task-detail-modal)
- [Read-Only Data Flow](#read-only-data-flow)
  - [Tasks and Sprints from SQLite](#tasks-and-sprints-from-sqlite)
  - [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)
- [Frontend and Embedded Assets](#frontend-and-embedded-assets)
  - [Self-Contained Deliverable](#self-contained-deliverable)
  - [Embedded Asset Categories](#embedded-asset-categories)
  - [Frontend Rules](#frontend-rules)
  - [UI Framework](#ui-framework)
  - [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours)
  - [Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library)
- [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)
- [Error Handling and Exit Codes](#error-handling-and-exit-codes)
- [Security and Constraints](#security-and-constraints)
- [Acceptance Criteria](#acceptance-criteria)
- [See Also](#see-also)

## Overview

The web interface is a read-only, browser-based presentation of the data that
the `rmp` CLI manages. A user starts it with the `rmp web` command, which runs an
HTTP server embedded in the `rmp` binary, opens a local browser to it, and
navigates the roadmaps found under `~/.roadmaps/` from there.

The web interface presents data; it never changes it. The `rmp` CLI remains the
sole write path for all roadmap data, tasks, sprints, and knowledge graphs. The
web interface in this version provides no create, edit, or delete action of any
kind. It reads the same on-disk data the CLI reads, in the same locations, and
serves it as server-rendered HTML.

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
   sprint's own page. The Actual tab does not expand the OPEN sprint into an inline
   task table or per-task modals; the full sprint detail block is shown only on the
   single Roadmap Sprint Page (see
   [Shared Sprint-Card Partial](#shared-sprint-card-partial)). It does not render
   the full tasks table.
3. A roadmap tasks page, served at `/roadmaps/{name}/tasks` and read from that
   roadmap's `project.db`. It presents the full task table of the roadmap (every
   task, any status), with each task row clickable to open the read-only task
   detail modal.
4. A roadmap sprint page that shows all details of a single sprint and the
   sprint's task list in planned in-sprint execution order, read from that
   roadmap's `project.db`.
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
4. The web interface is **read-only**. It serves `GET` (and `HEAD`) requests
   only; it exposes no route that creates, edits, or deletes any roadmap, task,
   sprint, audit entry, or graph element. Any non-read HTTP method on any route
   is answered with HTTP `405 Method Not Allowed`.
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
   does not expand the OPEN sprint into an inline task table or per-task modals.
   Próximos lists PENDING sprints ordered by ascending sprint `Order` (the unique
   execution order; the next sprint to execute, lowest `Order`, first); Actual lists
   the OPEN sprint or sprints ordered by ascending sprint `Order`; Concluídos lists
   CLOSED sprints ordered by descending sprint `Order` (the last/highest-`Order`
   closed sprint first). Each sprint shown in any tab is a clickable link to that
   sprint's own page. The sprints page does not render the full tasks table (see
   [Roadmap Sprints Page](#roadmap-sprints-page),
   [Roadmap Sprint Page](#roadmap-sprint-page), and
   [Shared Sprint-Card Partial](#shared-sprint-card-partial)).
7. The roadmap tasks page shows the selected roadmap's full task table — every
   task of the roadmap, of any status — with the fields and relationships already
   defined in `MODELS.md` and `DATABASE.md`, read from that roadmap's `project.db`.
   It is served at `/roadmaps/{name}/tasks`. Each task row is clickable: selecting
   a row opens the read-only task detail modal for that task (see
   [Roadmap Tasks Page](#roadmap-tasks-page) and
   [Task Detail Modal](#task-detail-modal)).
8. When a user selects a roadmap on the index page, the user lands on that
   roadmap's sprints page (`/roadmaps/{name}`), with the **Actual** tab — the
   current OPEN sprint or sprints — active by default (see
   [Roadmap Index Page](#roadmap-index-page) and
   [Roadmap Sprints Page](#roadmap-sprints-page)).
9. The roadmap sprint page shows all details of a single sprint and the sprint's
   task list in the planned in-sprint execution order, read from that roadmap's
   `project.db`. It is served at `/roadmaps/{name}/sprints/{id}`, is read-only, and
   returns HTTP `404 Not Found` when `{id}` is not a valid integer or is not a
   sprint of the named roadmap (see [Roadmap Sprint Page](#roadmap-sprint-page)).
10. The roadmap audit log page shows the selected roadmap's full audit log — every
   audit entry of any operation and entity type — with the `AuditEntry` fields
   already defined in `MODELS.md` and `DATABASE.md`, read from that roadmap's
   `project.db`. It is served at `/roadmaps/{name}/audit`, is read-only, and
   presents the entries as a table ordered by the audit entry's `performed_at`
   timestamp descending (the most recently performed operation first). The page is
   paginated at a fixed page size of 100 entries per page, with the page selected
   by a 1-based `page` query parameter that defaults to 1 when absent and is
   clamped to the nearest valid page; an empty audit log renders successfully with
   a clear empty-state message (see
   [Roadmap Audit Log Page](#roadmap-audit-log-page)).
11. Anywhere a task is shown clickable — the tasks page's task table and the sprint
   page's task list — selecting the task opens a read-only task detail modal that
   displays all of the task's fields. The modal
   only displays data: it contains no form, no edit control, and no submit action,
   and it requires no new server endpoint and no new write path (see
   [Task Detail Modal](#task-detail-modal)).
12. The roadmap knowledge-graph page shows the selected roadmap's knowledge graph
   as an interactive node-link visualisation rendered with **D3.js**, read from
   that roadmap's GoGraph store, opened read-only exactly as the `graph query` and
   `graph search` subcommands open it. The page offers the complete set of
   "Networks"-section D3 gallery layouts — Force-directed graph,
   Disjoint force-directed graph, Mobile patent suits (the **default**), Arc diagram,
   Sankey diagram, Hierarchical edge bundling, Chord diagram, Directed chord diagram,
   and Chord dependency diagram — selectable through a dropdown, and layouts that need a
   constrained data shape degrade gracefully. The page also presents a labels
   sidebar column, inside the graph card to the left of the canvas, that lists
   every node label and every edge type in the graph with a count for each and
   lets the user highlight the matching elements without removing the rest. At the
   top of the page a query bar lets the user drive the graph from a single editable
   Cypher query, with a Search button and a node-limit dropdown; the query is
   validated as read-only before execution, reusing the graph guard-rail, and a
   writing or DDL query is rejected and not executed (see
   [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page),
   [Graph Query Bar](#graph-query-bar),
   [Graph Data Endpoint](#graph-data-endpoint),
   [Graph Labels Sidebar](#graph-labels-sidebar),
   [Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library),
   and
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)).
13. Read access to a knowledge graph through the web interface MUST NOT write to
    the store and MUST NOT trigger the synchronous checkpoint or write-ahead-log
    truncation that write subcommands perform (see
    [Security and Constraints](#security-and-constraints) and
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
    sprint tabs, the task detail modal, and the interactive knowledge-graph
    visualisation, which MUST all remain usable on touch and small-viewport devices
    (see [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
17. **Tabler admin-shell layout in the dark theme.** The interface presents a
    Tabler admin-shell layout in Tabler's dark theme across every page: a
    navigation sidebar (listing the roadmaps and, within a roadmap, that
    roadmap's Sprints, Tasks, Audit, and Graph views, resolving to
    `/roadmaps/{name}`, `/roadmaps/{name}/tasks`, `/roadmaps/{name}/audit`, and
    `/roadmaps/{name}/graph` respectively and
    highlighting the active view), a top navbar, page headers, and Tabler cards,
    tables, and badges. The interface is built on the vendored Tabler framework;
    on small viewports the navigation sidebar collapses to an off-canvas
    (hamburger) menu (see [UI Framework](#ui-framework) and
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
18. Startup failures (for example, the chosen port is already in use, the data
    directory is unreadable, or a flag value is invalid) are reported as plain
    text to stderr and map to the existing exit codes; no new exit code is
    introduced (see [Error Handling and Exit Codes](#error-handling-and-exit-codes)).

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
   corrupt), the server logs an informational message to stderr naming that
   roadmap and the reason, and continues with the remaining roadmaps. A migration
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
   continues to be opened read-only on demand by graph requests (see
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
   loopback range), the server prints a warning to stderr stating that the
   read-only interface is reachable from the network. The warning is informational
   only: it does not change the exit code and does not prevent the server from
   starting. Binding a loopback address prints no such warning.
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
route.

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
   - the roadmap sprint page (`/roadmaps/{name}/sprints/{id}`);
   - the roadmap audit log page (`/roadmaps/{name}/audit`);
   - the knowledge-graph page shell (`/roadmaps/{name}/graph`);
   - the graph data endpoint (`/roadmaps/{name}/graph/data`).

   It also covers the data-state-dependent error responses — for example a
   `404 Not Found` for a roadmap or a sprint that does not exist, and a `500` from
   a read failure — because whether such a path is found depends on the current
   database or store state, so those responses are themselves data-derived.
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
| `/roadmaps/{name}/tasks` | GET, HEAD | Roadmap tasks page (full task table) | HTML |
| `/roadmaps/{name}/sprints/{id}` | GET, HEAD | Roadmap sprint page (all sprint details and the sprint's task list) | HTML |
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

HTTP status mapping for page and data routes:

| Condition | HTTP status |
|-----------|-------------|
| Page or data served successfully | 200 |
| Roadmap name invalid, or roadmap not found | 404 |
| Sprint `{id}` not a valid integer, or not a sprint of the roadmap | 404 |
| Audit `page` parameter out of range, non-integer, or garbage | 200 (clamped to nearest valid page; see [Roadmap Audit Log Page](#roadmap-audit-log-page)) |
| Non-read HTTP method on any route | 405 |
| Unhandled internal error reading data (I/O, corrupt store) | 500 |

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
  that roadmap's `project.db`. This page does **not** render the full tasks table;
  the full task table is its own page at `/roadmaps/{name}/tasks` (see
  [Roadmap Tasks Page](#roadmap-tasks-page)).
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
  - **Actual** (the default active tab) presents the OPEN sprint or sprints —
    those in progress — ordered by ascending sprint `Order` (the unique sprint
    execution order; see `MODELS.md § Sprint`). Each OPEN sprint is shown with the
    shared sprint-card partial, the same card the other two tabs use. The Actual
    tab does not expand the OPEN sprint into an inline task table or per-task
    modals; the full sprint detail block is shown only on the single Roadmap
    Sprint Page (see [Roadmap Sprint Page](#roadmap-sprint-page)). When no sprint
    is OPEN, the Actual tab shows a clear empty-state message and no card.
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
    member tasks and opens no task detail modal; member tasks are clickable on the
    single Roadmap Sprint Page and on the tasks page (see
    [Task Detail Modal](#task-detail-modal)).
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
- **Content:** A read-only presentation of the named roadmap's full task table —
  every task of the roadmap, of any status — read from that roadmap's
  `project.db`. This is the same Tasks table presentation that the roadmap's
  landing page used to carry, now served at its own endpoint.
- **Tasks.** The page presents the tasks of the roadmap with the fields defined
  for the `Task` model in `MODELS.md § Task`: title, status, type, priority,
  severity, functional/technical/acceptance text, specialists, lifecycle
  timestamps, parent task link, subtask relationships, and dependency
  relationships (`depends_on` and `blocks`). The page does not redefine these
  fields; `MODELS.md` and `DATABASE.md` remain canonical. Each task row in the
  task table is clickable: selecting a row opens the read-only task detail modal
  for that task (see [Task Detail Modal](#task-detail-modal)).
- **Relationships shown.** The page surfaces, in a read-only view, the
  relationships already modelled in the data: task-to-sprint membership, task
  parent/subtask hierarchy, and task dependency edges. The presentation MUST
  reflect the same relationships defined in `DATABASE.md § Relationships`; it
  introduces no new relationship.
- **Path parameters.** `{name}` is validated against the roadmap-name rules
  exactly as on the other roadmap routes (the path-traversal guard in
  [Routes and Pages](#routes-and-pages) and
  [Security and Constraints](#security-and-constraints)); an invalid or
  nonexistent `{name}` returns HTTP `404 Not Found`.
- **Read-only.** The page renders data only. It contains no form, button, or
  link that submits a change; there is no edit affordance of any kind. The task
  detail modal displays a read-only view and submits no change.

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
  title and its id. The page does not redefine these fields; `MODELS.md` remains
  canonical.
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
- **Task list.** The page lists the sprint's tasks in the planned in-sprint
  execution order, which is the `sprint_tasks` order (the ordered set of task IDs
  the `Sprint` model exposes as `tasks`; see `MODELS.md § Sprint` and
  `DATABASE.md § Relationships`). Each task in the list is clickable: selecting a
  task opens the read-only task detail modal for that task (see
  [Task Detail Modal](#task-detail-modal)).
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
- **Columns.** The table shows the `AuditEntry` fields defined for the audit entry
  in `MODELS.md § Audit Entry` and `DATABASE.md § audit Table`: the entry `ID`, the
  `Operation`, the `Entity Type`, the `Entity ID`, and the `Performed At` timestamp
  (the ISO 8601 UTC timestamp). The page does not redefine these fields;
  `MODELS.md` and `DATABASE.md` remain canonical.
- **Ordering.** The entries are ordered by the audit entry's `performed_at`
  timestamp **descending**, so the most recently performed operation appears first.
  `performed_at` is the audit entry's completion timestamp. This is the same
  ordering the existing audit data access uses (`ORDER BY performed_at DESC`; see
  `DATABASE.md § Audit`); the page introduces no new ordering.
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
- **Pagination controls.** The page footer shows a read-only **numbered
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
- **"Page X of Y" indicator.** The page footer keeps the textual "Page X of Y"
  indicator alongside the numbered pagination bar. It is a read-only, accessible
  affordance that states the current page and the total page count in words; it
  reflects the same `page` value and `TotalPages` total as the numbered bar.
- **Pagination markup.** The pagination bar uses accessible Tabler pagination
  markup: a `ul.pagination` list whose items are `li.page-item` elements, with each
  link rendered as `a.page-link`. The current page item carries the `active` state,
  and a disabled **Previous** or **Next** chevron and the ellipsis item carry the
  `disabled` state. `aria` attributes mark the disabled chevrons and the
  active/current page so the bar is fully accessible. The markup contains only
  `GET` links and inert items: no form, no button, and no write path.
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
   - the **full member-tasks table** listing the sprint's tasks in planned
     in-sprint execution order (the `sprint_tasks` order; see `MODELS.md § Sprint`
     and `DATABASE.md § Relationships`), with the columns `ID`, `Title`, `Status`,
     `Type`, `Priority`, and `Severity`. The `Status`, `Priority`, and `Severity`
     cells render their values as Tabler badges coloured by the semantic mapping in
     [Status, Priority, and Severity Badge Colours](#status-priority-and-severity-badge-colours).
     Each task row is clickable and opens the
     read-only task detail modal for that task (see
     [Task Detail Modal](#task-detail-modal)). When the sprint has no tasks, the
     sub-template shows a clear empty-state message in place of the table.

3. **Sprint status summary line.** At the top of the sub-template the sub-template
   renders one indicative, complementary line that summarises the sprint's task
   completion. Its exact format is:

   `<pct>% - P:<p> A:<a> C:<c> - T:<t>`

   for example `69% - P:8 A:3 C:18 - T:55`. The components are:
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

4. **Read-only.** The sub-template renders data only. It contains no form, button,
   or link that submits a change; the only interaction is opening the read-only
   task detail modal from a task row.

5. **Authored line breaks.** Wherever the sub-template renders the sprint's
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
  new request and no new endpoint.
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
  roadmap that has no graph yet behaves the same way the read subcommands do: it
  is not an error (see `GRAPH.md § Persistence Layout`, rule 2). Because this is a
  read, the web interface MUST open the graph store read-only and MUST NOT cause a
  write or a checkpoint, so reading an empty graph through the web interface does
  not create snapshot files.

### Graph Query Bar

The query bar is a control rendered at the top of the knowledge-graph page, above
the graph card (see [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)).
It lets the user drive the visualisation from a single editable Cypher query
instead of a fixed full-graph read, while keeping the page and the server strictly
read-only. The query bar reads from, and re-renders, the same graph data the page
already consumes; it adds no new endpoint and no write path.

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
   read-only guard-rail and limit validation, the same re-render of the graph in the
   currently selected layout, and the same in-place error surfacing on failure (see
   rule 4, [Graph Data Endpoint](#graph-data-endpoint), and
   [Query-Bar Error Handling](#query-bar-error-handling)). Ctrl+Enter is an
   accelerator for the existing Search action and introduces no other behaviour.
   Plain Enter in the query box is unchanged: it inserts a newline and does not
   trigger a search, so the user can compose a multi-line query freely.

6. **Node limit applied by the endpoint.** The dropdown value is the `limit`
   parameter sent on the request. The endpoint applies it as a `LIMIT` clause only
   when the user's query does not already contain a top-level `LIMIT`, so a user
   who writes their own `LIMIT` keeps it and the dropdown value is not applied; the
   injection and precedence rule is specified in
   [Graph Data Endpoint](#graph-data-endpoint).

7. **Read-only.** The query bar submits only read-only Cypher. A query containing
   a writing clause or a DDL clause is rejected by the endpoint's read-only
   guard-rail before execution and never runs (see
   [Graph Data Endpoint](#graph-data-endpoint) and
   [Query-Bar Error Handling](#query-bar-error-handling)). The query bar offers no
   create, edit, or delete affordance; running a query through it never writes to
   the store, never checkpoints, and never truncates the write-ahead log,
   consistent with the read-only contract of the whole interface (see
   [Security and Constraints](#security-and-constraints) and
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)).

8. **Error surfacing.** When a search fails — because the query is rejected as not
   read-only, because the limit is invalid, or because the query fails to execute —
   the page shows a clear, read-only message in place and does not crash, exactly
   as the layout degradation does; the distinct cases are specified in
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

A search driven by the query bar can fail for distinct reasons, and the page MUST
surface each clearly and in place without crashing, consistent with the graceful
layout degradation already specified (see
[Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library),
rule 5). The failure modes are kept distinct so the user understands what to fix.

1. **Query rejected: not read-only.** When the submitted query contains a writing
   clause or a DDL clause, the endpoint's read-only guard-rail rejects it before
   execution (see [Graph Data Endpoint](#graph-data-endpoint)) and the query is
   never run. The page surfaces a clear message stating that the query was rejected
   because it is not read-only, distinct from an execution failure. The graph
   already shown is left in place; the rejection changes nothing in the store.

2. **Invalid limit.** When the `limit` parameter is not one of the six allowed
   values (`50`, `100`, `250`, `500`, `1000`, `3000`), the endpoint rejects the
   request as an invalid limit and does not execute the query; the page surfaces a
   clear message naming the invalid limit. Because the limit values originate from
   the page's own dropdown, this state is normally only reachable by a crafted
   request, but the endpoint rejects it rather than guessing a value.

3. **Query failed to execute.** When the submitted query is accepted as read-only
   but then fails in the engine — for example, invalid Cypher syntax — the page
   surfaces a clear message stating that the query failed to execute, distinct from
   the read-only rejection in case 1. A syntactically invalid read-only query is an
   execution failure, not a guard-rail rejection, mirroring the CLI behaviour where
   a query that passes the clause check is still rejected by the engine at execution
   time (see `GRAPH.md § Per-Subcommand Validation Rules`, note 3).

4. **In-place, read-only, non-fatal.** In every case the message is shown in place
   on the page, the page does not crash, and the failure triggers no write and no
   navigation, exactly as the layout-degradation message does. The user can edit
   the query or change the limit and search again.

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
  encoding.
- **Query parameters.** The endpoint accepts two optional URL query parameters
  that the graph page's query bar (see
  [Graph Query Bar](#graph-query-bar)) sends, and that drive which Cypher query
  runs and how many results it returns:
  - `q` — the Cypher query to run, URL-encoded. When `q` is absent or empty, the
    endpoint runs the **default query**
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
- **Read-only guard-rail (security-critical).** Before it executes any
  user-supplied `q`, the endpoint MUST validate that the query is **read-only**,
  reusing the exact guard-rail mechanism that the read subcommands `rmp graph
  query` and `rmp graph search` use (see
  `GRAPH.md § Subcommands and Guard-Rail Validation` and
  `GRAPH.md § Literal-Aware Normalization`). The validation runs on the
  **masked normalization** of the query (literal-aware masking via
  `maskCypherLiterals` followed by `cypher.QueryHasWritingClause`, plus the
  independent DDL rejection), never on the raw query string, exactly as the CLI
  guard-rail does. A query is rejected, and **not executed**, when its masked
  normalization contains any writing clause (`CREATE`, `MERGE`, `SET`, `REMOVE`,
  `DELETE`, or `DETACH DELETE`) or any DDL clause (`CREATE INDEX`, `DROP INDEX`,
  `CREATE CONSTRAINT`, or `DROP CONSTRAINT`). Because validation runs on the
  masked normalization, a write or DDL keyword that appears only inside a string
  literal, a comment, or a backtick-quoted identifier neither falsely trips the
  rejection nor falsely passes a real writing clause: a query such as
  `MATCH (m) WHERE m.title = "mentions delete and set" RETURN m` is accepted as
  read-only, while `MATCH (n) DELETE n` is rejected. The query actually executed
  against the store is the original, unmodified query; masking affects validation
  only. The endpoint runs the accepted query through the engine's read path,
  exactly as `graph query` and `graph search` do (see
  `GRAPH.md § Engine Construction and Lifecycle`); it MUST NOT run any writing
  clause, MUST NOT checkpoint or truncate the write-ahead log, MUST NOT write an
  audit entry, and follows no write path. The page and the server stay strictly
  read-only.
- **Node-limit injection.** The endpoint applies the resolved `limit` (the
  parameter value, or the default `100` when absent) by appending a top-level
  `LIMIT <n>` clause to the query, **only when the user's query does not already
  contain a top-level `LIMIT` clause**. The user's own `LIMIT` takes precedence
  and is respected as-is: when the query already has a top-level `LIMIT`, the
  endpoint injects nothing and the dropdown value is not applied. The
  presence-of-`LIMIT` check is performed on the **masked normalization** of the
  query (see `GRAPH.md § Literal-Aware Normalization`), so a `LIMIT` keyword that
  appears only inside a string literal, a comment, or a backtick-quoted identifier
  does not count as an existing top-level `LIMIT` and does not suppress injection.
  The default query has no `LIMIT`, so a request that uses the default query
  always has the resolved limit applied to it.
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

- **Where it appears.** Anywhere a task is shown clickable: the task table on the
  roadmap tasks page and the task list on the roadmap sprint page. The roadmap
  sprints page shows no clickable tasks, because every sprint there is rendered as
  a card with no member-tasks table. Selecting a task opens the modal for that
  task.
- **Fields shown.** The modal displays all of the task's fields as defined for the
  `Task` model in `MODELS.md § Task`: `id`, `title`, `status`, `type`, `priority`,
  `severity`, `functional_requirements`, `technical_requirements`,
  `acceptance_criteria`, `specialists`, `completion_summary`, `parent_task_id`,
  `subtask_count`, `depends_on`, `blocks`, `created_at`, `started_at`, `tested_at`,
  and `closed_at`. This includes the long free-text fields
  (`functional_requirements`, `technical_requirements`, `acceptance_criteria`, and
  `completion_summary`), which the modal presents formatted for readable display.
  These long free-text fields are multi-line as authored through the CLI, and the
  modal renders them preserving the author's line breaks (newlines); the text
  still wraps within the modal, so no forced horizontal scrolling is introduced
  (see [Frontend Rules](#frontend-rules), rule 6). The page does not redefine
  these fields; `MODELS.md` and `DATABASE.md` remain canonical.
- **Read-only.** The modal only displays data. It contains no form, no input, no
  edit control, and no submit action of any kind.
- **No new server endpoint and no new write path.** The modal is populated from
  read-only task data already delivered to the page that opens it: the task data
  is server-rendered into the page (as auto-escaped HTML) or carried in a JSON data
  island (JSON-encoded, not interpolated into HTML). The modal introduces no new
  server endpoint and no new write path; the CLI remains the sole write path (see
  [Security and Constraints](#security-and-constraints)).
- **Output escaping.** Roadmap-derived text shown in the modal is escaped
  consistently with [Security and Constraints](#security-and-constraints):
  `html/template` contextual auto-escaping for HTML, or JSON encoding for a data
  island. Task field values that contain HTML control characters cannot alter page
  structure.
- **Popup and touch usability.** The modal is a popup overlay (for example a
  Tabler or Bootstrap modal) rendered inside the Tabler admin shell. It MUST be
  usable on touch input and on small viewports: it fits the viewport without
  horizontal overflow, scrolls its content when the task's text is long, and
  offers touch-friendly controls to open and dismiss it (see
  [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).

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
   because the page renders every sprint as a card with no member-tasks table; the
   tasks page reads the roadmap's full task list; the audit log page reads the
   roadmap's audit entries ordered by `performed_at` descending, one fixed-size page
   at a time (see [Roadmap Audit Log Page](#roadmap-audit-log-page) and
   `DATABASE.md § Audit`). The task data the task detail modal
   displays comes from the same read queries; the modal adds no separate request.
   The web interface adds no new schema, no new table, and no new write query.
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
   it is the only place the schema is altered.
3. Each request opens the database, reads what it needs, renders the page, and
   releases the handle. Concurrency against SQLite follows the existing model in
   `IMPLEMENTATION.md § Concurrency Model`; a web read is an ordinary reader and
   does not change the CLI's write behaviour.

### Knowledge Graph from the GoGraph Store

1. For a graph page or graph data request, the server resolves the roadmap's
   graph store at `~/.roadmaps/{name}/graph/` (see `GRAPH.md § Persistence
   Layout`) and opens it through the GoGraph engine's store-backed read path, the
   same way `graph query` and `graph search` open it (see `GRAPH.md § Engine
   Construction and Lifecycle`).
2. The server runs a **read-only** Cypher query to retrieve the nodes and edges
   to visualise. The query contains no writing clause; it is the same class of
   query the read subcommands accept under the guard rail in
   `GRAPH.md § Subcommands and Guard-Rail Validation`.
3. The server MUST NOT perform any write and MUST NOT trigger the synchronous
   checkpoint or write-ahead-log truncation that write subcommands perform after
   a commit (see `GRAPH.md § Synchronous Checkpoint on Write` and
   `IMPLEMENTATION.md § Graph Store Concurrency`). A read through the web
   interface leaves the store's on-disk files unchanged, exactly as a `graph
   query` invocation does.
4. Opening the store runs GoGraph's recovery, which restores the last committed
   state from the snapshot and the write-ahead-log tail (see `GRAPH.md §
   Concurrency and Recovery`). Recovery on open is a read-path operation; it
   restores in-memory state and does not itself constitute a Groadmap write or a
   checkpoint.
5. Each request opens the store, reads, serves the result, and closes the store.
   The server does not hold the graph store open across requests, consistent with
   the short-lived-access model in `IMPLEMENTATION.md § Graph Store Concurrency`.
   A graph store that is corrupt or unreadable surfaces as an internal read error
   (HTTP 500 on the affected route); there is no automatic graph-store repair.

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
   no presentational inline styles (rule 10), and the minor markup-fidelity
   adjustments (rule 11) — are concrete applications of it.
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
   tabs and their counts or badges, and the default-active Actual tab, are preserved
   exactly as specified in [Roadmap Sprints Page](#roadmap-sprints-page).
10. **No presentational inline styles.** Templates MUST NOT carry presentational
    inline `style="..."` attributes. All styling lives in the vendored Tabler
    classes and utilities, or in the project override stylesheet (`static/style.css`),
    served from `/static/...` (see
    [Embedded Asset Categories](#embedded-asset-categories)). In particular, the
    navigation sidebar's section label and the empty-state icon sizing carry no
    inline `style`: the sidebar section separator follows Tabler's vertical-navbar
    subheader and `hr` idiom (a Tabler navbar subheader and divider, not an
    inline-styled label), and any presentational sizing such as the empty-state
    icon's dimensions lives in a Tabler utility class or in `static/style.css`. This
    keeps the markup faithful to the Tabler examples and keeps presentation out of
    the templates, consistent with the Content-Security-Policy in
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
    - **The footer** follows Tabler's footer row structure, as the Tabler footer
      example does.
    These adjustments only align the markup with the Tabler examples; they introduce
    no new page, no new content, and no write path, and the pages remain read-only.

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
the canonical criticality ranges defined in `COMMANDS.md § Show Sprint`
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
2. **Applied consistently everywhere a badge is shown.** The same mapping is applied
   wherever a status, priority, or severity badge appears: the tasks table (see
   [Roadmap Tasks Page](#roadmap-tasks-page)), the sprint detail member-tasks table
   (see [Sprint Detail Sub-Template](#sprint-detail-sub-template)), the task detail
   modal (see [Task Detail Modal](#task-detail-modal)), the sprint cards (see
   [Shared Sprint-Card Partial](#shared-sprint-card-partial)), the Roadmap Sprint
   Page header and metadata datagrid (see [Roadmap Sprint Page](#roadmap-sprint-page)
   and [Sprint Detail Sub-Template](#sprint-detail-sub-template)), and the sprint
   tabs on the Roadmap Sprints Page (see [Roadmap Sprints Page](#roadmap-sprints-page)).
   A status, priority, or severity badge anywhere in the interface uses the variant
   the relevant table above assigns to its value; no badge uses a single fixed
   colour across differing values.
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
   small-viewport-friendly: the container is fluid and fits the viewport, and the
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
   screens there is no horizontal scrolling, typography stays readable, and
   navigation and other interactive controls present touch-friendly, appropriately
   sized hit targets.
3. **Applies to every page.** The mobile-first, responsive requirement applies to
   every page: the roadmap index page, the roadmap sprints page (the sprint tabs),
   the roadmap tasks page (the full task table), the roadmap sprint page, the
   roadmap audit log page (the audit table), and the knowledge-graph page.
4. **Usable tabular data on narrow screens.** The roadmap sprints page, the
   roadmap tasks page, the roadmap sprint page, and the roadmap audit log page
   present sprint, task, and audit data
   that is tabular by nature. This data MUST remain usable on narrow screens, for
   example through responsive or stacked tables or an equivalent layout that
   avoids horizontal overflow, while still presenting the fields and relationships
   defined for those pages (see [Roadmap Sprints Page](#roadmap-sprints-page),
   [Roadmap Tasks Page](#roadmap-tasks-page),
   [Roadmap Sprint Page](#roadmap-sprint-page), and
   [Roadmap Audit Log Page](#roadmap-audit-log-page)).
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
   Its container is fluid and fits the viewport, and it supports touch gestures —
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
   responses (404, 405, 500) and do **not** terminate the process. The process
   exit code is determined by how the server itself is started and stopped.
5. Errors written to stderr by `rmp web` carry the standard AI-agent hint and
   follow the plain-text error format in `HELP.md § Error message format`.
6. If a future need arises for a dedicated web error class, it MUST be added
   following the procedure in `ARCHITECTURE.md § Adding New Error Types`. This
   version introduces none.

## Security and Constraints

1. **Loopback by default; network exposure is opt-in.** The server binds the
   loopback interface (`127.0.0.1`) by default, so the read-only interface is
   reachable only from the local machine. Exposing the interface on the network is
   the explicit opt-in `--host 0.0.0.0` (all interfaces), or any other non-loopback
   address. When a non-loopback host is bound, the server prints a warning to
   stderr that the interface is reachable from the network (see
   [Bind Address and Port Selection](#bind-address-and-port-selection)). The
   interface remains read-only regardless of bind address; exposing read access to
   the network is an explicit user choice and the user's responsibility.
2. **Read-only.** The interface never writes. It exposes no route that creates,
   edits, or deletes roadmap data, tasks, sprints, audit entries, or graph
   elements. The server accepts only `GET` and `HEAD`; every other method returns
   HTTP `405`. The graph store is opened read-only and a web read never triggers a
   checkpoint or write-ahead-log truncation (see
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)).
   SQLite reads write no rows and produce no audit-log entry.
3. **User-supplied Cypher is validated read-only before execution.** The graph
   page's query bar lets the user submit an editable Cypher query to the graph
   data endpoint as the `q` parameter (see [Graph Query Bar](#graph-query-bar) and
   [Graph Data Endpoint](#graph-data-endpoint)). The endpoint MUST validate that
   query as **read-only** before executing it, reusing the exact guard-rail the
   read subcommands `rmp graph query` and `rmp graph search` use: the literal-aware
   masked normalization (`maskCypherLiterals` then `cypher.QueryHasWritingClause`)
   plus the independent DDL rejection (see
   `GRAPH.md § Subcommands and Guard-Rail Validation` and
   `GRAPH.md § Literal-Aware Normalization`). Validation runs on the masked
   normalization, never on the raw string, so a write or DDL keyword inside a
   string literal, comment, or backtick-quoted identifier neither falsely trips nor
   falsely passes. A query containing any writing clause (`CREATE`, `MERGE`, `SET`,
   `REMOVE`, `DELETE`, `DETACH DELETE`) or any DDL clause (`CREATE`/`DROP`
   `INDEX`/`CONSTRAINT`) is rejected and **not executed**, and the page surfaces the
   rejection (see [Query-Bar Error Handling](#query-bar-error-handling)). The query
   bar opens no write path: an accepted query runs through the engine's read path
   only, and never checkpoints or truncates the write-ahead log.
4. **Filesystem permission model is unchanged.** The web interface reads through
   the existing locations and respects the existing permission model: `0700` for
   `~/.roadmaps/` and each roadmap home directory, `0600` for `project.db`, and
   `0700` for each `graph/` store (see `ARCHITECTURE.md § Directory Structure`).
   The web interface creates no new on-disk artefact for a read; it does not relax
   any permission.
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
7. **Output escaping.** Roadmap-derived text (task and sprint fields, including
   the task fields shown in the task detail modal, and graph node and edge labels
   and property values) is rendered through `html/template`'s contextual
   auto-escaping, so data that contains HTML control characters cannot alter page
   structure. Task data carried to the page as a JSON data island for the task
   detail modal, and graph data delivered as JSON to the visualisation, are encoded
   as JSON, not interpolated into HTML.
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
10. **No directory listings; bounded connection timeouts.** The static handler
   never serves a directory listing: a request for a directory under `/static/`
   returns HTTP `404` (see [Static Assets](#static-assets)). The HTTP server is
   configured with explicit ReadHeaderTimeout, WriteTimeout, and IdleTimeout values
   so a slow or idle client cannot exhaust server resources (see
   [HTTP Server Timeouts](#http-server-timeouts)).
11. **No stale data; `no-store` on data-derived responses.** Every data-derived
   response (the roadmap index page, the roadmap sprints page, the roadmap tasks
   page, the roadmap sprint page, the roadmap audit log page, the knowledge-graph
   page shell, the graph data
   endpoint, and the data-state-dependent error responses) carries
   `Cache-Control: no-store`, so no client-side or intermediary cache re-presents a
   state that no longer matches the database or store. Embedded `/static/...`
   assets are immutable and are excluded from this rule, remaining cacheable (see
   [Cache Policy](#cache-policy)).
12. **No new write path.** The web interface does not become a second source of
   truth. The CLI and its SQLite databases and GoGraph stores remain the sole
   source of truth and the sole write path; the web interface is a view over them.

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
   an inline member-tasks table or per-task modals, using the fields and
   relationships defined in `MODELS.md` and `DATABASE.md`. The page does **not**
   render the full tasks table, and it contains no form, button, or link that
   submits a change.
9. `GET /roadmaps/{name}/tasks` for an existing roadmap returns HTTP 200 and an
   HTML page that renders the roadmap's full task table — every task, of any
   status — with the fields and relationships defined in `MODELS.md` and
   `DATABASE.md`. This is a distinct endpoint from the sprints page. Each task row
   is clickable and opens a working read-only task detail modal, and the page
   contains no form, button, or link that submits a change. `GET /roadmaps/{name}/tasks`
   for a non-existent roadmap, or a request whose `{name}` violates the
   roadmap-name rules, returns HTTP 404 without touching the filesystem outside
   `~/.roadmaps/`.
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
    tabs and is not expanded into an inline task table or per-task modals. A tab
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
    `task_count`) and the sprint's task list in `sprint_tasks` order (the planned
    in-sprint execution order); the page header presents the sprint `title`
    alongside `Sprint #<ID>`, and the sprint metadata datagrid shows the sprint
    `Title` and the execution `Order` in addition to the ID, Status, Capacity,
    Tasks, Created, Started, and Closed fields; the page contains no form, button,
    or link that submits a change. A request whose `{id}` is not a valid integer, or is an
    integer that is not a sprint of the named roadmap, returns HTTP 404, and a
    request whose `{name}` is invalid or nonexistent returns HTTP 404.
15. Clicking a task anywhere it is shown clickable — the tasks page's task table
    and the sprint page's task list — opens a modal
    popup that displays all of that task's fields (`id`, `title`, `status`, `type`,
    `priority`, `severity`, `functional_requirements`, `technical_requirements`,
    `acceptance_criteria`, `specialists`, `completion_summary`, `parent_task_id`,
    `subtask_count`, `depends_on`, `blocks`, `created_at`, `started_at`,
    `tested_at`, `closed_at`). The modal is read-only: it contains no form, no edit
    control, and no submit action, and it triggers no new server request and no new
    write path. The modal and the sprint tabs are usable on touch input and on a
    small phone-sized viewport.
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
    defined in `DATA_FORMATS.md § Graph View Data`, populated from a read-only
    query against the roadmap's GoGraph store.
19. After serving any number of graph page and graph data requests for a roadmap
    that has never been written, the graph store directory contains no `snapshot/`
    subdirectory and no checkpoint has run, proving the web read path is read-only
    (see `GRAPH.md § Synchronous Checkpoint on Write`).
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
    page each render without horizontal scrolling, with readable typography and
    touch-friendly hit targets, demonstrating the mobile-first base styles.
28. On the roadmap sprints page, the roadmap tasks page, the roadmap sprint
    page, and the roadmap audit log page at a narrow viewport, the sprint, task,
    and audit data remains usable without
    horizontal overflow (for example through responsive or stacked tables or an
    equivalent layout) while still showing the fields and relationships defined for
    those pages.
29. Every HTML page the interface serves includes the responsive viewport meta
    tag, and no page loads a CSS framework or reset from a remote origin; the
    Tabler CSS framework in use is vendored and served from `/static/...`.
30. Every page renders in the Tabler admin-shell layout — a navigation sidebar
    (listing the roadmaps and, within a roadmap, that roadmap's Sprints, Tasks,
    Audit, and Graph views), a top navbar, and a page header — using Tabler cards,
    tables, and badges, and the interface renders in Tabler's dark theme.
31. On a small phone-sized viewport, the admin-shell navigation sidebar is not
    shown expanded inline; it collapses to an off-canvas (hamburger) menu that the
    user can open, so each page stays usable without horizontal overflow.
32. Multi-line free-text authored through the CLI renders preserving its source
    line breaks: the task detail modal's long free-text fields
    (`functional_requirements`, `technical_requirements`, `acceptance_criteria`,
    and `completion_summary`) and a sprint's `description` — shown in the sprint
    cards on the roadmap sprints page (across all three tabs) and on the roadmap
    sprint page — each display the author's
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
    is not expanded into an inline sprint metadata datagrid, member-tasks table, or
    per-task modals on the sprints page. The full sprint detail block (sprint status
    summary line, metadata datagrid, and member-tasks table) is shown only on the
    single Roadmap Sprint Page (see
    [Shared Sprint-Card Partial](#shared-sprint-card-partial) and
    [Sprint Detail Sub-Template](#sprint-detail-sub-template)).
39. At the top of the full sprint presentation on the single Roadmap Sprint Page, a
    sprint status summary line is shown in the
    exact format `<pct>% - P:<p> A:<a> C:<c> - T:<t>` (for example
    `69% - P:8 A:3 C:18 - T:55`), where `<pct>` is the completion percentage
    (`COMPLETED` tasks divided by total tasks, rounded to the nearest integer
    percent, and `0%` when the sprint has no tasks), `P` is the count of the
    sprint's tasks in `BACKLOG` or `SPRINT`, `A` is the count in `DOING` or
    `TESTING`, `C` is the count in `COMPLETED`, and `T` is the sprint's total task
    count; every value counts only the sprint's own member tasks. For a sprint
    with, for example, 55 member tasks of which 8 are pending, 3 in progress, and
    18 completed (with the remaining 26 in other statuses), the line reads
    `33% - P:8 A:3 C:18 - T:55` (18 of 55 completed rounds to 33%).
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
47. A query submitted through the query bar that contains a writing clause
    (`CREATE`, `MERGE`, `SET`, `REMOVE`, `DELETE`, or `DETACH DELETE`) or a DDL
    clause (`CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, or `DROP CONSTRAINT`)
    is rejected by the endpoint's read-only guard-rail **before execution** and is
    **not executed**; the store is not modified, no checkpoint runs, and no
    write-ahead-log truncation occurs, and the page surfaces a clear "query
    rejected: not read-only" message distinct from an execution failure. The
    guard-rail runs on the masked normalization of the query (literal-aware masking
    plus `cypher.QueryHasWritingClause` and the DDL rejection), so a write or DDL
    keyword that appears only inside a string literal, a comment, or a
    backtick-quoted identifier does not trip the rejection: for example
    `MATCH (m) WHERE m.title = "mentions delete and set" RETURN m` is accepted as
    read-only and executes, while `MATCH (n) DELETE n` is rejected and does not
    execute (see [Graph Data Endpoint](#graph-data-endpoint),
    [Query-Bar Error Handling](#query-bar-error-handling), and
    `GRAPH.md § Literal-Aware Normalization`).
48. The endpoint applies the node limit by appending `LIMIT <n>` only when the
    user's query does not already contain a top-level `LIMIT`: a request whose `q`
    has no top-level `LIMIT` returns at most the resolved limit's worth of results
    (the dropdown value, or `100` when `limit` is absent), while a request whose `q`
    already contains its own top-level `LIMIT` keeps that `LIMIT` and the dropdown
    value is not applied. The existing-`LIMIT` detection runs on the masked
    normalization, so a `LIMIT` keyword appearing only inside a string literal, a
    comment, or a backtick-quoted identifier does not count as an existing top-level
    `LIMIT` and does not suppress injection. A `limit` parameter that is not one of
    the six allowed values is rejected as an invalid limit and the query is not
    executed; the page surfaces a clear invalid-limit message (see
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
50. A query submitted through the query bar that is accepted as read-only but then
    fails in the engine (for example, invalid Cypher syntax) surfaces a clear
    "query failed to execute" message on the page, distinct from the "query
    rejected: not read-only" message. In every query-bar failure case — read-only
    rejection, invalid limit, or execution failure — the message is shown in place,
    the page does not crash, and the failure triggers no write and no navigation,
    consistent with the graceful layout degradation; the user can edit the query or
    change the limit and search again (see
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
    same read-only guard-rail and limit validation, re-renders the graph in the
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
59. The audit log page footer shows read-only Previous and Next navigation controls
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
    Concluídos) with their counts or badges and the default-active **Actual** tab are
    preserved exactly as specified (see [UI Framework](#ui-framework), rule 9, and
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
    it is shown — the tasks table, the sprint detail member-tasks table, the task
    detail modal, the sprint cards, the Roadmap Sprint Page header and metadata
    datagrid, and the sprints-page tabs — and the mapping introduces no enum value
    beyond those defined in `MODELS.md` and `STATE_MACHINE.md`.
62. No template carries a presentational inline `style="..."` attribute: all styling
    is provided by vendored Tabler classes and utilities or by the project override
    stylesheet (`static/style.css`). In particular, the navigation sidebar's section
    label uses Tabler's vertical-navbar subheader and `hr` divider idiom rather than
    an inline-styled label, and the empty-state icon's sizing lives in a Tabler
    utility class or in `static/style.css` rather than in an inline `style`
    attribute (see [UI Framework](#ui-framework), rule 10).
63. The templates follow Tabler's markup idioms in the minor markup-fidelity places:
    page-header rows use Tabler's `row g-2 align-items-center` gutter and alignment
    classes, the sidebar brand uses the Tabler
    `<h1 class="navbar-brand navbar-brand-autodark">` element, and the footer follows
    Tabler's footer row structure. These are markup-fidelity adjustments only: the
    read-only nature of the interface and the content shown are unchanged (see
    [UI Framework](#ui-framework), rule 11).

## See Also

- CLI command contract for `web` → `COMMANDS.md § Web Interface`
- Graph view data JSON shape → `DATA_FORMATS.md § Graph View Data`
- Graph element and property-type JSON mapping reused by the graph data endpoint
  → `DATA_FORMATS.md § Graph Query Result`
- Read-only graph access, recovery, and the checkpoint that web reads must avoid
  → `GRAPH.md § Engine Construction and Lifecycle` and
  `GRAPH.md § Synchronous Checkpoint on Write`
- Read-only guard-rail and literal-aware masked normalization reused to validate
  the query bar's user-supplied Cypher →
  `GRAPH.md § Subcommands and Guard-Rail Validation` and
  `GRAPH.md § Literal-Aware Normalization`
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
- `AuditEntry` fields, the audit read query and its `performed_at DESC` ordering,
  and the audit result-set hard cap presented on the audit log page →
  `MODELS.md § Audit Entry`, `DATABASE.md § audit Table`, `DATABASE.md § Audit`,
  and `DATABASE.md § Audit Result Limit`
- Sprint status enum and lifecycle that classify sprints into the sprints-page tabs
  → `MODELS.md § Enums` and `STATE_MACHINE.md § Sprint State Machine`
- Task and sprint status enums, the task and sprint lifecycles, and the
  `priority`/`severity` integer ranges and criticality bands that the badge colour
  mapping uses → `MODELS.md § Enums`, `MODELS.md § Task`,
  `STATE_MACHINE.md § Task State Machine`, `STATE_MACHINE.md § Sprint State Machine`,
  and `COMMANDS.md § Show Sprint`
- Embedded asset bundling, the vendored Tabler framework and D3.js (with
  d3-sankey) assets, and the self-contained-binary build verification →
  `BUILD.md § Vendored Web Assets`
- Help skeleton for `web` → `HELP.md`
