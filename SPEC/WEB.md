# Web Interface

## Table of Contents

- [Overview](#overview)
- [Functional Requirements](#functional-requirements)
- [Command Surface](#command-surface)
- [Server Lifecycle](#server-lifecycle)
- [Bind Address and Port Selection](#bind-address-and-port-selection)
- [HTTP Server Timeouts](#http-server-timeouts)
- [Security Headers](#security-headers)
- [Routes and Pages](#routes-and-pages)
  - [Roadmap Index Page](#roadmap-index-page)
  - [Roadmap Sprints Page](#roadmap-sprints-page)
  - [Roadmap Tasks Page](#roadmap-tasks-page)
  - [Roadmap Sprint Page](#roadmap-sprint-page)
  - [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)
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
roadmap's Sprints, Tasks, and Graph views: the Sprints link points to the
roadmap's landing page at `/roadmaps/{name}`, the Tasks link points to
`/roadmaps/{name}/tasks`, and the Graph link points to `/roadmaps/{name}/graph`;
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
   active by default, expands the OPEN ("current") sprint or sprints under Actual
   with their member tasks, and links each sprint to its own page. It does not
   render the full tasks table.
3. A roadmap tasks page, served at `/roadmaps/{name}/tasks` and read from that
   roadmap's `project.db`. It presents the full task table of the roadmap (every
   task, any status), with each task row clickable to open the read-only task
   detail modal.
4. A roadmap sprint page that shows all details of a single sprint and the
   sprint's task list in planned in-sprint execution order, read from that
   roadmap's `project.db`.
5. A roadmap knowledge-graph page that shows that roadmap's knowledge graph,
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
   under Actual, and a `CLOSED` sprint under Concluídos. Próximos lists PENDING
   sprints ordered by predicted execution order (ascending sprint `id`, next
   first); Actual lists the OPEN sprint or sprints, expanded with their member
   tasks, and shows the status of all of those tasks; Concluídos lists CLOSED
   sprints ordered by most recently closed first (`closed_at` descending, sprints
   without a `closed_at` last). Each sprint shown in any tab is a clickable link to
   that sprint's own page. The sprints page does not render the full tasks table
   (see [Roadmap Sprints Page](#roadmap-sprints-page) and
   [Roadmap Sprint Page](#roadmap-sprint-page)).
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
10. Anywhere a task is shown clickable — the tasks page's task table, the Actual
   tab's task list, and the sprint page's task list — selecting the task opens a
   read-only task detail modal that displays all of the task's fields. The modal
   only displays data: it contains no form, no edit control, and no submit action,
   and it requires no new server endpoint and no new write path (see
   [Task Detail Modal](#task-detail-modal)).
11. The roadmap knowledge-graph page shows the selected roadmap's knowledge graph
   as an interactive node-link visualisation rendered with **D3.js**, read from
   that roadmap's GoGraph store, opened read-only exactly as the `graph query` and
   `graph search` subcommands open it. The page offers the complete set of
   "Networks"-section D3 gallery layouts — Force-directed graph,
   Disjoint force-directed graph, Mobile patent suits (the **default**), Arc diagram,
   Sankey diagram, Hierarchical edge bundling, Chord diagram, Directed chord diagram,
   and Chord dependency diagram — selectable through a dropdown, and layouts that need a
   constrained data shape degrade gracefully (see
   [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page),
   [Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library),
   and
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)).
12. Read access to a knowledge graph through the web interface MUST NOT write to
    the store and MUST NOT trigger the synchronous checkpoint or write-ahead-log
    truncation that write subcommands perform (see
    [Security and Constraints](#security-and-constraints) and
    `GRAPH.md § Synchronous Checkpoint on Write`).
13. **The deliverable is fully self-contained.** The shipped `rmp` binary MUST
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
14. **No runtime network fetch.** No page references a script, stylesheet, font,
    image, or any other asset from a remote origin: no content delivery network,
    no Google Fonts or other remote font, script, or style host, and no external
    API. The interface renders and functions fully offline, with only the single
    `rmp` binary present on disk: no sidecar files and no separate assets
    directory shipped alongside it. The running server makes no outbound network
    request of its own (see
    [Self-Contained Deliverable](#self-contained-deliverable) and
    [Frontend and Embedded Assets](#frontend-and-embedded-assets)).
15. **Responsive and mobile-first.** The web interface MUST be designed
    responsive and mobile-first: base styles target small phone-sized viewports
    first and progressively enhance for larger tablet and desktop viewports
    through `min-width` media queries, and every page adapts fluidly across
    viewport sizes. This requirement applies to every page — the roadmap index,
    the roadmap sprints page, the roadmap tasks page, the roadmap sprint page, and
    the knowledge-graph page — and to the interactive components, including the
    sprint tabs, the task detail modal, and the interactive knowledge-graph
    visualisation, which MUST all remain usable on touch and small-viewport devices
    (see [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
16. **Tabler admin-shell layout in the dark theme.** The interface presents a
    Tabler admin-shell layout in Tabler's dark theme across every page: a
    navigation sidebar (listing the roadmaps and, within a roadmap, that
    roadmap's Sprints, Tasks, and Graph views, resolving to `/roadmaps/{name}`,
    `/roadmaps/{name}/tasks`, and `/roadmaps/{name}/graph` respectively and
    highlighting the active view), a top navbar, page headers, and Tabler cards,
    tables, and badges. The interface is built on the vendored Tabler framework;
    on small viewports the navigation sidebar collapses to an off-canvas
    (hamburger) menu (see [UI Framework](#ui-framework) and
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
17. Startup failures (for example, the chosen port is already in use, the data
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
2. Resolves the bind host and port (see
   [Bind Address and Port Selection](#bind-address-and-port-selection)) and binds
   a TCP listener. A bind failure (for example, the port is already in use or the
   host is not assignable) is a fatal startup error (see
   [Error Handling and Exit Codes](#error-handling-and-exit-codes)). When the
   resolved host is not a loopback address, the server prints a network-exposure
   warning to stderr (see
   [Bind Address and Port Selection](#bind-address-and-port-selection)).
3. Registers the read-only routes (see [Routes and Pages](#routes-and-pages)),
   configures the HTTP server timeouts (see
   [HTTP Server Timeouts](#http-server-timeouts)), and starts serving.
4. Prints to stdout the URL the server is listening on, so the user can open it
   manually if no browser is launched. The startup line is the single
   machine-readable success object described in `COMMANDS.md § Web Interface`.
5. Unless `--no-open` is given, attempts to open the user's default browser at
   the served URL. A failure to launch a browser is **not** fatal: the server
   keeps running and the URL has already been printed.
6. Serves requests until the process receives an interrupt signal (`SIGINT`, for
   example `Ctrl+C`) or a termination signal (`SIGTERM`). On either signal the
   server shuts down gracefully: it stops accepting new connections, allows
   in-flight requests a brief bounded period to complete, closes any graph store
   or database handle it opened, and exits 0.

The server is long-lived for the duration of the session. This is the only `rmp`
command whose process is expected to keep running rather than complete a single
operation and exit. Each incoming request opens the data it needs read-only,
serves the response, and releases the handle; the server does not hold a roadmap
database or a graph store open across requests.

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
| `/roadmaps/{name}/graph` | GET, HEAD | Roadmap knowledge-graph page (interactive visualisation) | HTML |
| `/roadmaps/{name}/graph/data` | GET, HEAD | Graph nodes and edges for the visualisation | JSON |
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
  sprint to Actual, and a `CLOSED` sprint to Concluídos.
  - **Actual** (the default active tab) lists the OPEN sprint or sprints — those
    in progress — expanded with their member tasks, and shows the state of all of
    those tasks, presenting each task with its status. The member tasks are listed
    in the planned in-sprint execution order (the `sprint_tasks` order; see
    `MODELS.md § Sprint` and `DATABASE.md § Relationships`). When no sprint is
    OPEN, the Actual tab shows a clear empty-state message and no sprint.
  - **Próximos** lists the PENDING sprints — planned but not yet started — ordered
    by predicted execution order. Because a sprint carries no planned-date field,
    the predicted execution order is the creation order: ascending sprint `id`,
    so the next sprint to start appears first. When no sprint is PENDING, the
    Próximos tab shows a clear empty-state message.
  - **Concluídos** lists the CLOSED sprints ordered by most recently closed first:
    descending `closed_at`. A CLOSED sprint that has no `closed_at` value sorts
    last. When no sprint is CLOSED, the Concluídos tab shows a clear empty-state
    message.
  - Every sprint shown in any of the three tabs is a clickable link to that
    sprint's own page at `/roadmaps/{name}/sprints/{id}` (see
    [Roadmap Sprint Page](#roadmap-sprint-page)). On the Actual tab, the member
    tasks shown for an OPEN sprint are also clickable and open the task detail
    modal (see [Task Detail Modal](#task-detail-modal)).
- **Sprint description line breaks.** Wherever a sprint's `description` text is
  shown on this page — the expanded OPEN sprint under the Actual tab, and the
  sprint cards under Próximos and Concluídos — the description renders preserving
  the author's line breaks (newlines), because the description is multi-line as
  authored through the CLI; the text still wraps, so no forced horizontal
  scrolling is introduced (see [Frontend Rules](#frontend-rules), rule 6).
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
  read from that roadmap's `project.db`.
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

### Roadmap Knowledge-Graph Page

- **Route:** `GET /roadmaps/{name}/graph`
- **Content:** An HTML page that renders the named roadmap's knowledge graph as
  an interactive node-link visualisation. The page loads the vendored D3.js
  library (and the d3-sankey plugin) from `/static/...` and fetches the graph's
  nodes and edges as JSON from the graph data endpoint
  (`/roadmaps/{name}/graph/data`).
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
  The visualisation MUST be usable without a mouse: it supports touch gestures
  (pan, pinch-to-zoom, and tap to select and inspect) and surfaces node and edge
  detail through tap or selection rather than relying on mouse hover, so the page
  is fully usable on touch devices (see
  [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
- **Empty graph.** A roadmap that has never used the `graph` command, or whose
  graph is empty, renders successfully and shows an empty-graph state. Reading a
  roadmap that has no graph yet behaves the same way the read subcommands do: it
  is not an error (see `GRAPH.md § Persistence Layout`, rule 2). Because this is a
  read, the web interface MUST open the graph store read-only and MUST NOT cause a
  write or a checkpoint, so reading an empty graph through the web interface does
  not create snapshot files.

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
- **Read path.** The endpoint reads the graph read-only, opening the store via
  recovery and running a read-only Cypher query through the engine's read path,
  exactly as `graph query` and `graph search` do (see
  `GRAPH.md § Engine Construction and Lifecycle`). It MUST NOT run any writing
  clause and MUST NOT checkpoint or truncate the write-ahead log.
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
  roadmap tasks page, the Actual tab's task list on the roadmap sprints page, and
  the task list on the roadmap sprint page. Selecting a task opens the modal for
  that task.
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
location rules, and never writes to it.

### Tasks and Sprints from SQLite

1. For a roadmap sprints request, a roadmap tasks request, or a roadmap sprint
   request, the server resolves the roadmap's database at
   `~/.roadmaps/{name}/project.db` (see `ARCHITECTURE.md § Directory Structure`)
   and reads its sprints and tasks using the existing read queries defined in
   `DATABASE.md § Main SQL Queries`. The sprints page reads the roadmap's sprints
   and, for each OPEN sprint on the Actual tab, its ordered member tasks; the tasks
   page reads the roadmap's full task list. The task data the task detail modal
   displays comes from the same read queries; the modal adds no separate request.
   The web interface adds no new schema, no new table, and no new write query.
2. The server opens the database for reading only. It MUST NOT modify rows, MUST
   NOT write an audit entry, and MUST NOT alter the schema. A web read produces no
   audit-log entry, because the audit log records changes and a read is not a
   change (see `DATABASE.md § audit Table`).
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
   the task detail modal, and a sprint's `description` wherever it is shown — the
   interface preserves the author's line breaks rather than collapsing them under
   HTML's default whitespace handling. The text still wraps within its container,
   so preserving line breaks introduces no forced horizontal scrolling, and the
   text is still rendered through `html/template`'s contextual auto-escaping (rule
   1): it is never rendered as raw HTML. This rule is the general statement of the
   behaviour; the [Task Detail Modal](#task-detail-modal),
   [Roadmap Sprints Page](#roadmap-sprints-page), and
   [Roadmap Sprint Page](#roadmap-sprint-page) sections reference it.

### UI Framework

1. The web interface is built on **Tabler**, the admin-dashboard CSS and
   JavaScript framework (built on Bootstrap). Tabler provides the admin-shell
   layout — the navigation sidebar, the top navbar, page headers, and the cards,
   tables, badges, and buttons used across every page. The sidebar's per-roadmap
   links resolve to the roadmap's three views — Sprints at `/roadmaps/{name}` (the
   landing page), Tasks at `/roadmaps/{name}/tasks`, and Graph at
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
   the roadmap tasks page (the full task table), the roadmap sprint page, and the
   knowledge-graph page.
4. **Usable tabular data on narrow screens.** The roadmap sprints page, the
   roadmap tasks page, and the roadmap sprint page present sprint and task data
   that is tabular by nature. This data MUST remain usable on narrow screens, for
   example through responsive or stacked tables or an equivalent layout that
   avoids horizontal overflow, while still presenting the fields and relationships
   defined for those pages (see [Roadmap Sprints Page](#roadmap-sprints-page),
   [Roadmap Tasks Page](#roadmap-tasks-page), and
   [Roadmap Sprint Page](#roadmap-sprint-page)).
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
3. **Filesystem permission model is unchanged.** The web interface reads through
   the existing locations and respects the existing permission model: `0700` for
   `~/.roadmaps/` and each roadmap home directory, `0600` for `project.db`, and
   `0700` for each `graph/` store (see `ARCHITECTURE.md § Directory Structure`).
   The web interface creates no new on-disk artefact for a read; it does not relax
   any permission.
4. **No arbitrary filesystem serving; path-traversal guard.** The static handler
   serves only assets from the embedded asset set, never an arbitrary host
   filesystem path. Roadmap names taken from the URL path are validated against
   the roadmap-name rules (regex `^[a-z0-9_-]+$`, maximum 50 characters) **before**
   they are used to build any filesystem path, so a crafted `{name}` cannot
   traverse outside `~/.roadmaps/`. A name that fails validation is rejected with
   HTTP `404` and never reaches the filesystem (see
   [Routes and Pages](#routes-and-pages)). This mirrors the central roadmap-name
   validation gate the CLI applies (see `ARCHITECTURE.md § Security Guarantees`).
5. **Self-contained assets, no CDN, no external calls.** Every asset a page loads
   is served from the local server's embedded assets, and the deliverable is the
   single `rmp` binary with zero external runtime dependency. No page references a
   content delivery network or any other remote origin, the interface functions
   fully offline, and the server makes no outbound network request (see
   [Self-Contained Deliverable](#self-contained-deliverable) and
   [Frontend and Embedded Assets](#frontend-and-embedded-assets)).
6. **Output escaping.** Roadmap-derived text (task and sprint fields, including
   the task fields shown in the task detail modal, and graph node and edge labels
   and property values) is rendered through `html/template`'s contextual
   auto-escaping, so data that contains HTML control characters cannot alter page
   structure. Task data carried to the page as a JSON data island for the task
   detail modal, and graph data delivered as JSON to the visualisation, are encoded
   as JSON, not interpolated into HTML.
7. **Security headers on every HTML response.** Every HTML response carries the
   Content-Security-Policy, X-Content-Type-Options (`nosniff`), X-Frame-Options
   (`DENY`), and Referrer-Policy (`same-origin`) headers specified in
   [Security Headers](#security-headers). The Content-Security-Policy restricts
   every resource to the server's own origin, consistent with the no-remote-origin
   asset model.
8. **HTML-safe JSON on the graph data endpoint.** The graph data endpoint emits
   HTML-safe JSON (`<`, `>`, and `&` serialized as Unicode escape sequences), so
   roadmap-derived graph text cannot break an HTML or script context (see
   [Graph Data Endpoint](#graph-data-endpoint)).
9. **No directory listings; bounded connection timeouts.** The static handler
   never serves a directory listing: a request for a directory under `/static/`
   returns HTTP `404` (see [Static Assets](#static-assets)). The HTTP server is
   configured with explicit ReadHeaderTimeout, WriteTimeout, and IdleTimeout values
   so a slow or idle client cannot exhaust server resources (see
   [HTTP Server Timeouts](#http-server-timeouts)).
10. **No new write path.** The web interface does not become a second source of
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
   tabs with the **Actual** tab active by default and the OPEN sprint or sprints
   expanded under Actual with their member tasks, using the fields and
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
    status: every `PENDING` sprint appears under Próximos ordered by ascending
    sprint `id`; every `OPEN` sprint appears under Actual, expanded with its member
    tasks in in-sprint order and the status of each of those tasks shown; every
    `CLOSED` sprint appears under Concluídos ordered by `closed_at` descending,
    with any CLOSED sprint lacking a `closed_at` value sorted last. A tab with no
    matching sprint shows a clear empty-state message.
13. On the roadmap sprints page, every sprint shown in any tab is a clickable link
    to that sprint's page at `/roadmaps/{name}/sprints/{id}`.
14. `GET /roadmaps/{name}/sprints/{id}` for a sprint of an existing roadmap returns
    HTTP 200 and an HTML page showing all details of that sprint (id, status,
    `title`, description, execution `order`, capacity `max_tasks`, `created_at`,
    `started_at`, `closed_at`, and
    `task_count`) and the sprint's task list in `sprint_tasks` order (the planned
    in-sprint execution order); the page contains no form, button, or link that
    submits a change. A request whose `{id}` is not a valid integer, or is an
    integer that is not a sprint of the named roadmap, returns HTTP 404, and a
    request whose `{name}` is invalid or nonexistent returns HTTP 404.
15. Clicking a task anywhere it is shown clickable — the tasks page's task table,
    the Actual tab's task list, and the sprint page's task list — opens a modal
    popup that displays all of that task's fields (`id`, `title`, `status`, `type`,
    `priority`, `severity`, `functional_requirements`, `technical_requirements`,
    `acceptance_criteria`, `specialists`, `completion_summary`, `parent_task_id`,
    `subtask_count`, `depends_on`, `blocks`, `created_at`, `started_at`,
    `tested_at`, `closed_at`). The modal is read-only: it contains no form, no edit
    control, and no submit action, and it triggers no new server request and no new
    write path. The modal and the sprint tabs are usable on touch input and on a
    small phone-sized viewport.
16. The admin-shell sidebar's per-roadmap links target the two distinct endpoints:
    the Sprints link points to `/roadmaps/{name}` (the landing page) and the Tasks
    link points to `/roadmaps/{name}/tasks`; the sidebar highlights whichever of
    the two is the active view, and the Graph link points to
    `/roadmaps/{name}/graph`.
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
20. Serving roadmap sprints pages, roadmap tasks pages, and roadmap sprint pages
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
    page, the roadmap tasks page, the roadmap sprint page, and the knowledge-graph
    page each render without horizontal scrolling, with readable typography and
    touch-friendly hit targets, demonstrating the mobile-first base styles.
28. On the roadmap sprints page, the roadmap tasks page, and the roadmap sprint
    page at a narrow viewport, the sprint and task data remains usable without
    horizontal overflow (for example through responsive or stacked tables or an
    equivalent layout) while still showing the fields and relationships defined for
    those pages.
29. Every HTML page the interface serves includes the responsive viewport meta
    tag, and no page loads a CSS framework or reset from a remote origin; the
    Tabler CSS framework in use is vendored and served from `/static/...`.
30. Every page renders in the Tabler admin-shell layout — a navigation sidebar
    (listing the roadmaps and, within a roadmap, that roadmap's Sprints, Tasks,
    and Graph views), a top navbar, and a page header — using Tabler cards,
    tables, and badges, and the interface renders in Tabler's dark theme.
31. On a small phone-sized viewport, the admin-shell navigation sidebar is not
    shown expanded inline; it collapses to an off-canvas (hamburger) menu that the
    user can open, so each page stays usable without horizontal overflow.
32. Multi-line free-text authored through the CLI renders preserving its source
    line breaks: the task detail modal's long free-text fields
    (`functional_requirements`, `technical_requirements`, `acceptance_criteria`,
    and `completion_summary`) and a sprint's `description` — shown on the roadmap
    sprints page (the Actual-tab expanded sprint and the Próximos and Concluídos
    sprint cards) and on the roadmap sprint page — each display the author's
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

## See Also

- CLI command contract for `web` → `COMMANDS.md § Web Interface`
- Graph view data JSON shape → `DATA_FORMATS.md § Graph View Data`
- Graph element and property-type JSON mapping reused by the graph data endpoint
  → `DATA_FORMATS.md § Graph Query Result`
- Read-only graph access, recovery, and the checkpoint that web reads must avoid
  → `GRAPH.md § Engine Construction and Lifecycle` and
  `GRAPH.md § Synchronous Checkpoint on Write`
- Roadmap discovery, data directory layout, and permissions →
  `ARCHITECTURE.md § Directory Structure`
- Web module responsibilities and command lifecycle →
  `ARCHITECTURE.md § Modules and Responsibilities` and
  `ARCHITECTURE.md § Command Lifecycle`
- Task and Sprint fields presented in the sprints page, the tasks page, the sprint
  page, and the task detail modal → `MODELS.md` and `DATABASE.md`
- Sprint status enum and lifecycle that classify sprints into the sprints-page tabs
  → `MODELS.md § Enums` and `STATE_MACHINE.md § Sprint State Machine`
- Embedded asset bundling, the vendored Tabler framework and D3.js (with
  d3-sankey) assets, and the self-contained-binary build verification →
  `BUILD.md § Vendored Web Assets`
- Help skeleton for `web` → `HELP.md`
