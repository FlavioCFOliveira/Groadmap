# Web Interface

## Table of Contents

- [Overview](#overview)
- [Functional Requirements](#functional-requirements)
- [Command Surface](#command-surface)
- [Server Lifecycle](#server-lifecycle)
- [Bind Address and Port Selection](#bind-address-and-port-selection)
- [Routes and Pages](#routes-and-pages)
  - [Roadmap Index Page](#roadmap-index-page)
  - [Roadmap Detail Page](#roadmap-detail-page)
  - [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page)
  - [Graph Data Endpoint](#graph-data-endpoint)
  - [Static Assets](#static-assets)
- [Read-Only Data Flow](#read-only-data-flow)
  - [Tasks and Sprints from SQLite](#tasks-and-sprints-from-sqlite)
  - [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)
- [Frontend and Embedded Assets](#frontend-and-embedded-assets)
  - [Self-Contained Deliverable](#self-contained-deliverable)
  - [Embedded Asset Categories](#embedded-asset-categories)
  - [Frontend Rules](#frontend-rules)
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

The interface is designed responsive and mobile-first: its base styles target
small phone-sized viewports first and progressively enhance for larger viewports,
and it adapts fluidly across viewport sizes on every page, including the
interactive knowledge-graph visualisation (see
[Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).

The web interface exposes three kinds of page for each roadmap:

1. A roadmap index that lists every roadmap found under `~/.roadmaps/`.
2. A roadmap detail page that shows that roadmap's tasks and sprints, read from
   its SQLite `project.db`.
3. A roadmap knowledge-graph page that shows that roadmap's knowledge graph,
   read from its GoGraph store under `~/.roadmaps/<name>/graph/`, as an
   interactive node-link visualisation.

## Functional Requirements

1. `rmp web` starts an HTTP server embedded in the `rmp` binary, built on Go's
   standard-library `net/http`, and serves the read-only web interface until the
   server is stopped (see [Server Lifecycle](#server-lifecycle)).
2. The server binds to a loopback address and a port chosen as specified in
   [Bind Address and Port Selection](#bind-address-and-port-selection). The bind
   host and port are overridable by flag, but the default is loopback-only.
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
6. The roadmap detail page shows the selected roadmap's tasks and sprints, with
   the fields and relationships already defined in `MODELS.md` and `DATABASE.md`,
   read from that roadmap's `project.db` (see
   [Roadmap Detail Page](#roadmap-detail-page)).
7. The roadmap knowledge-graph page shows the selected roadmap's knowledge graph
   as an interactive node-link visualisation, read from that roadmap's GoGraph
   store, opened read-only exactly as the `graph query` and `graph search`
   subcommands open it (see
   [Roadmap Knowledge-Graph Page](#roadmap-knowledge-graph-page) and
   [Knowledge Graph from the GoGraph Store](#knowledge-graph-from-the-gograph-store)).
8. Read access to a knowledge graph through the web interface MUST NOT write to
   the store and MUST NOT trigger the synchronous checkpoint or write-ahead-log
   truncation that write subcommands perform (see
   [Security and Constraints](#security-and-constraints) and
   `GRAPH.md § Synchronous Checkpoint on Write`).
9. **The deliverable is fully self-contained.** The shipped `rmp` binary MUST
   embed every component required to render and operate the web interface, with
   zero external runtime dependency. Every asset category — HTML templates, the
   stylesheet, all client JavaScript (including the Cytoscape.js
   knowledge-graph visualisation library and any of its dependencies), web
   fonts, icons and images, the favicon, and any other static asset — is
   embedded into the binary at build time with `go:embed` and served only from
   the embedded asset set under the `/static/...` route. The server never reads
   an asset from the host filesystem and never serves an arbitrary host
   filesystem path (see
   [Self-Contained Deliverable](#self-contained-deliverable),
   [Embedded Asset Categories](#embedded-asset-categories), and
   [Security and Constraints](#security-and-constraints)).
10. **No runtime network fetch.** No page references a script, stylesheet, font,
    image, or any other asset from a remote origin: no content delivery network,
    no Google Fonts or other remote font, script, or style host, and no external
    API. The interface renders and functions fully offline, with only the single
    `rmp` binary present on disk: no sidecar files and no separate assets
    directory shipped alongside it. The running server makes no outbound network
    request of its own (see
    [Self-Contained Deliverable](#self-contained-deliverable) and
    [Frontend and Embedded Assets](#frontend-and-embedded-assets)).
11. **Responsive and mobile-first.** The web interface MUST be designed
    responsive and mobile-first: base styles target small phone-sized viewports
    first and progressively enhance for larger tablet and desktop viewports
    through `min-width` media queries, and every page adapts fluidly across
    viewport sizes. This requirement applies to all three pages — the roadmap
    index, the roadmap detail page, and the knowledge-graph page — and to the
    interactive knowledge-graph visualisation, which MUST remain usable on touch
    and small-viewport devices (see
    [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
12. Startup failures (for example, the chosen port is already in use, the data
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
- Flags: `--host <address>` (default `127.0.0.1`), `--port <number>` (default
  `8787`, with the fallback behaviour in
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
   [Error Handling and Exit Codes](#error-handling-and-exit-codes)).
3. Registers the read-only routes (see [Routes and Pages](#routes-and-pages)) and
   starts serving.
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

1. **Default host.** The server binds to the loopback interface `127.0.0.1` by
   default. With the default host the interface is reachable only from the local
   machine and is not exposed on the network.
2. **Host override.** `--host <address>` overrides the bind host. A user who
   deliberately wants to reach the interface from another machine may pass, for
   example, `--host 0.0.0.0`. The default is loopback; binding to a non-loopback
   address is an explicit user choice, and the security note in
   [Security and Constraints](#security-and-constraints) applies.
3. **Default port.** The default port is `8787`. When `--port` is not given, the
   server attempts to bind `8787`. If `8787` is already in use, the server falls
   back to an ephemeral port chosen by the operating system (binding port `0`),
   so that `rmp web` starts successfully even when the default port is taken. The
   actual chosen port is reported in the startup line and the served URL.
4. **Explicit port.** `--port <number>` requests a specific port. When an
   explicit port is given, the server does **not** fall back to an ephemeral
   port: if the requested port cannot be bound, the command fails with a bind
   error (see [Error Handling and Exit Codes](#error-handling-and-exit-codes)),
   because the user asked for that exact port. `--port 0` explicitly requests an
   operating-system-chosen ephemeral port and always succeeds when a port is
   available.
5. **Port range.** A `--port` value MUST be an integer in the range `0`-`65535`.
   A value outside that range, or a non-integer value, is an invalid flag value
   (`utils.ErrValidation`, exit code 6).

## Routes and Pages

All routes serve `GET` and `HEAD` only. Every page is server-rendered HTML
produced from embedded `html/template` templates. Page routes return HTML
(`text/html; charset=utf-8`); the graph data endpoint returns JSON.

| Route | Method | Purpose | Response |
|-------|--------|---------|----------|
| `/` | GET, HEAD | Roadmap index | HTML list of roadmaps |
| `/roadmaps/{name}` | GET, HEAD | Roadmap detail (tasks and sprints) | HTML |
| `/roadmaps/{name}/graph` | GET, HEAD | Roadmap knowledge-graph page (interactive visualisation) | HTML |
| `/roadmaps/{name}/graph/data` | GET, HEAD | Graph nodes and edges for the visualisation | JSON |
| `/static/...` | GET, HEAD | Embedded static assets (CSS, JS, vendored graph library) | static file |

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

HTTP status mapping for page and data routes:

| Condition | HTTP status |
|-----------|-------------|
| Page or data served successfully | 200 |
| Roadmap name invalid, or roadmap not found | 404 |
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
  location rule 9). For each roadmap the page links to its detail page
  (`/roadmaps/{name}`) and its knowledge-graph page (`/roadmaps/{name}/graph`).
- **Empty state.** When no roadmaps exist under `~/.roadmaps/`, the index page
  renders successfully (HTTP 200) and shows a clear empty-state message telling
  the user that no roadmaps were found and that roadmaps are created with the CLI
  (`rmp roadmap create <name>`). The absence of roadmaps is not an error for the
  web interface; the server still starts and serves the empty index.

### Roadmap Detail Page

- **Route:** `GET /roadmaps/{name}`
- **Content:** A read-only presentation of the named roadmap's tasks and sprints,
  read from that roadmap's `project.db`.
- **Tasks.** The page presents the tasks of the roadmap with the fields defined
  for the `Task` model in `MODELS.md § Task`: title, status, type, priority,
  severity, functional/technical/acceptance text, specialists, lifecycle
  timestamps, parent task link, subtask relationships, and dependency
  relationships (`depends_on` and `blocks`). The page does not redefine these
  fields; `MODELS.md` and `DATABASE.md` remain canonical.
- **Sprints.** The page presents the roadmap's sprints with the fields defined
  for the `Sprint` model in `MODELS.md § Sprint`: status, description, capacity
  (`max_tasks`), lifecycle timestamps, and the ordered set of tasks assigned to
  each sprint.
- **Relationships shown.** The page surfaces, in a read-only view, the
  relationships already modelled in the data: task-to-sprint membership
  (including task order within a sprint), task parent/subtask hierarchy, and task
  dependency edges. The presentation MUST reflect the same relationships defined
  in `DATABASE.md § Relationships`; it introduces no new relationship.
- **Read-only.** The page renders data only. It contains no form, button, or
  link that submits a change; there is no edit affordance of any kind.

### Roadmap Knowledge-Graph Page

- **Route:** `GET /roadmaps/{name}/graph`
- **Content:** An HTML page that renders the named roadmap's knowledge graph as
  an interactive node-link visualisation. The page loads the vendored graph
  library from `/static/...` and fetches the graph's nodes and edges as JSON from
  the graph data endpoint (`/roadmaps/{name}/graph/data`).
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
  this endpoint and hands the result to the vendored graph library.
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

### Static Assets

- **Route:** `GET /static/...`
- **Content:** The embedded stylesheet, the embedded client scripts, and the
  vendored JavaScript graph library. These are served only from the embedded
  asset set. The static handler MUST serve only embedded assets and MUST NOT map a
  request path to an arbitrary path on the host filesystem. A request for an asset
  that is not in the embedded set is answered with HTTP `404 Not Found` (see
  [Security and Constraints](#security-and-constraints)).

## Read-Only Data Flow

The web interface reads the same on-disk data the CLI reads, through the same
location rules, and never writes to it.

### Tasks and Sprints from SQLite

1. For a roadmap detail request, the server resolves the roadmap's database at
   `~/.roadmaps/{name}/project.db` (see `ARCHITECTURE.md § Directory Structure`)
   and reads its tasks and sprints using the existing read queries defined in
   `DATABASE.md § Main SQL Queries`. The web interface adds no new schema, no new
   table, and no new write query.
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
2. **Stylesheet** — all CSS, including any vendored CSS reset or framework (see
   [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
3. **JavaScript** — all client scripts, including the Cytoscape.js
   knowledge-graph visualisation library and any of its dependencies, all in
   already-built (vendored) form.
4. **Web fonts** — any font the interface uses; no font is loaded from a remote
   font host.
5. **Icons and images** — any icon or image the interface displays.
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

### Knowledge-Graph Visualisation Library

1. The interactive node-link visualisation uses **Cytoscape.js** as the graph
   rendering library.
2. Cytoscape.js is **vendored**: its already-built distribution file is committed
   to the repository under the web asset set and embedded into the binary with
   `go:embed`. It is served locally from `/static/...`. It is never loaded from a
   content delivery network or any remote origin.
3. The library renders the nodes and edges returned by the graph data endpoint
   (see [Graph Data Endpoint](#graph-data-endpoint)) and provides pan, zoom, and
   selection so the user can inspect a node's or edge's labels, type, and
   properties.
4. **Touch and small-viewport configuration.** Cytoscape.js supports touch
   gestures. The visualisation and its container MUST be configured to be touch-
   and small-viewport-friendly: the container is fluid and fits the viewport, and
   the visualisation supports touch pan, pinch-to-zoom, and tap to select and
   inspect, so node and edge detail can be reached without a mouse hover (see
   [Responsive and Mobile-First Design](#responsive-and-mobile-first-design)).
5. The choice of Cytoscape.js is an implementation-level decision recorded here
   so the SPEC is unambiguous about which library is vendored; substituting a
   different vendored, locally-served, build-step-free graph library is a SPEC
   change to this section and to `BUILD.md § Vendored Web Assets`, not a silent
   code change.

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
   all three pages: the roadmap index page, the roadmap detail page (tasks and
   sprints), and the knowledge-graph page.
4. **Usable tabular data on narrow screens.** The roadmap detail page presents
   task and sprint data that is tabular by nature. This data MUST remain usable on
   narrow screens, for example through responsive or stacked tables or an
   equivalent layout that avoids horizontal overflow, while still presenting the
   fields and relationships defined for the detail page (see
   [Roadmap Detail Page](#roadmap-detail-page)).
5. **Touch- and mobile-usable graph visualisation.** The interactive
   knowledge-graph visualisation MUST remain usable on touch and mobile devices.
   Its container is fluid and fits the viewport, and it supports touch gestures —
   pan, pinch-to-zoom, and tap to select and inspect — so node and edge detail can
   be reached without a mouse hover (see
   [Knowledge-Graph Visualisation Library](#knowledge-graph-visualisation-library)).
6. **Responsive viewport meta tag.** Every HTML page includes the responsive
   viewport meta tag, so mobile browsers scale the page to the device width rather
   than rendering it at a fixed desktop width.
7. **Framework-free, vendored CSS only.** The interface uses no external CSS
   framework loaded from a content delivery network or any remote origin,
   consistent with [Self-Contained Deliverable](#self-contained-deliverable). If
   any CSS framework or reset is used, it MUST be vendored and embedded with the
   stylesheet (see [Embedded Asset Categories](#embedded-asset-categories)).

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

1. **Loopback by default.** The server binds `127.0.0.1` by default, so the
   interface is reachable only from the local machine. Binding to a non-loopback
   address (for example via `--host 0.0.0.0`) is an explicit, opt-in user choice;
   the interface remains read-only regardless of bind address, but a non-loopback
   bind exposes read access to the network and is the user's responsibility.
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
6. **Output escaping.** Roadmap-derived text (task and sprint fields, graph node
   and edge labels and property values) is rendered through `html/template`'s
   contextual auto-escaping, so data that contains HTML control characters cannot
   alter page structure. Graph data delivered as JSON to the visualisation is
   encoded as JSON, not interpolated into HTML.
7. **No new write path.** The web interface does not become a second source of
   truth. The CLI and its SQLite databases and GoGraph stores remain the sole
   source of truth and the sole write path; the web interface is a view over them.

## Acceptance Criteria

1. `rmp web` starts a server, prints the served URL to stdout as the success
   object defined in `COMMANDS.md § Web Interface`, and (unless `--no-open` is
   given) opens the default browser at that URL. With no flags it binds
   `127.0.0.1:8787`.
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
   roadmap's detail and graph pages.
7. With no roadmaps present, `rmp web` still starts and `GET /` returns HTTP 200
   with an empty-state message; the absence of roadmaps is not an error.
8. `GET /roadmaps/{name}` for an existing roadmap returns HTTP 200 and an HTML
   page showing that roadmap's tasks and sprints with the fields and
   relationships defined in `MODELS.md` and `DATABASE.md`, and contains no form,
   button, or link that submits a change.
9. `GET /roadmaps/{name}` for a non-existent roadmap returns HTTP 404, and a
   request whose `{name}` violates the roadmap-name rules (for example
   `../etc`) returns HTTP 404 without touching the filesystem outside
   `~/.roadmaps/`.
10. `GET /roadmaps/{name}/graph` for an existing roadmap returns HTTP 200 and an
    HTML page that loads the vendored Cytoscape.js from `/static/...` (not from
    any remote origin) and renders an interactive node-link visualisation with pan
    and zoom that is usable with touch gestures (pan, pinch-to-zoom, tap to select
    and inspect) and surfaces node and edge detail without requiring a mouse hover.
11. `GET /roadmaps/{name}/graph/data` returns HTTP 200 and JSON in the shape
    defined in `DATA_FORMATS.md § Graph View Data`, populated from a read-only
    query against the roadmap's GoGraph store.
12. After serving any number of graph page and graph data requests for a roadmap
    that has never been written, the graph store directory contains no `snapshot/`
    subdirectory and no checkpoint has run, proving the web read path is read-only
    (see `GRAPH.md § Synchronous Checkpoint on Write`).
13. Serving roadmap detail pages produces **no** new audit-log entry in the
    roadmap's `project.db` (a read is not a change).
14. A `POST`, `PUT`, `PATCH`, or `DELETE` request to any route returns HTTP 405.
15. A request for a `/static/...` path that is not in the embedded asset set
    returns HTTP 404, and no `/static/...` request can read a file outside the
    embedded asset set.
16. Every page the interface serves loads all of its assets — scripts,
    stylesheet, graph library, fonts, icons, images, and favicon — only from
    `/static/...` on the same server; no page references a content delivery
    network, a remote font host, or any other remote origin, and the running
    server makes no outbound network request.
17. Sending `SIGINT` (`Ctrl+C`) or `SIGTERM` to a running `rmp web` shuts the
    server down gracefully and the process exits 0.
18. The deliverable is fully self-contained: the binary serves the interface with
    zero external runtime dependency. Every embedded asset category in
    [Embedded Asset Categories](#embedded-asset-categories) — HTML templates, the
    stylesheet, all client JavaScript including the Cytoscape.js bundle and its
    dependencies, web fonts, icons and images, and the favicon — is embedded via
    `go:embed`, and the build produces a single self-contained binary (see
    `BUILD.md § Vendored Web Assets`).
19. The interface works with networking disabled and with only the `rmp` binary
    present on disk (no sidecar files and no separate assets directory): every
    page renders and functions fully, including the knowledge-graph visualisation,
    with no network egress.
20. On a small phone-sized viewport, the roadmap index page, the roadmap detail
    page, and the knowledge-graph page each render without horizontal scrolling,
    with readable typography and touch-friendly hit targets, demonstrating the
    mobile-first base styles.
21. On the roadmap detail page at a narrow viewport, the task and sprint data
    remains usable without horizontal overflow (for example through responsive or
    stacked tables or an equivalent layout) while still showing the fields and
    relationships defined for the page.
22. Every HTML page the interface serves includes the responsive viewport meta
    tag, and no page loads a CSS framework or reset from a remote origin; any CSS
    framework or reset in use is vendored and served from `/static/...`.

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
- Task and Sprint fields presented in the detail page → `MODELS.md` and
  `DATABASE.md`
- Embedded asset bundling, the vendored Cytoscape.js asset, and the
  self-contained-binary build verification → `BUILD.md § Vendored Web Assets`
- Help skeleton for `web` → `HELP.md`
