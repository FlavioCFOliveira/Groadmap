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
| `/roadmaps/{name}` | Roadmap sprints page and landing page: that roadmap's sprints in three tabs (Próximos / Actual / Concluídos, Actual default), every sprint rendered through the same sprint card and linking to its own page. Selecting a roadmap on the index lands here | HTML |
| `/roadmaps/{name}/tasks` | Roadmap tasks page: that roadmap's full task table (every task, any status); clicking a task opens a read-only modal with all task fields and that task's comments timeline | HTML |
| `/roadmaps/{name}/sprints/{id}` | Dedicated sprint page: all sprint details, the task list in planned execution order, and the sprint's own Comments card; each task opens the task detail modal | HTML |
| `/roadmaps/{name}/audit` | Roadmap audit log page: that roadmap's full audit log (columns ID, Operation, Entity Type, Entity ID, Performed At), ordered by Performed At descending (most recent first), paginated at 100 entries per page via the `page` query parameter (1-based, default 1; out-of-range or non-numeric values are clamped to the nearest valid page) with Previous/Next controls and a "Page X of Y" indicator | HTML |
| `/roadmaps/{name}/graph` | Interactive knowledge-graph visualisation (D3.js; selectable Networks-section layouts via a dropdown, default Mobile patent suits; pan/zoom, touch, tap-to-inspect) | HTML |
| `/roadmaps/{name}/graph/data` | The graph's nodes and edges for the visualisation | JSON |
| `/static/...` | Embedded static assets (CSS, JS, vendored Tabler framework and D3.js + d3-sankey, fonts) | static file |

`{name}` is validated against the roadmap-name rules (regex `^[a-z0-9_-]+$`, max 50 characters) before it is used to build any filesystem path; a name that fails validation, or a roadmap that does not exist, returns HTTP `404`. A request for a `/static/...` asset that is not embedded returns HTTP `404`. These HTTP statuses are distinct from the process exit codes below.

## Comment Surfaces

Comments recorded through `rmp task comment-add` and `rmp sprint comment-add` are surfaced on two read-only places in the interface. Both only display data: neither creates, edits, nor deletes a comment, and the CLI remains the sole write path.

### Task comments: the detail modal timeline

Anywhere a task is clickable — the roadmap tasks page's task table and the sprint page's task list — the read-only task detail modal renders that task's comments as a chronological timeline, placed after the task's fields and last in the modal body.

- **Order and completeness.** Oldest first, exactly the order `rmp task comment-list` returns (`created_at` ascending, comment `id` ascending as the tie-breaker). Every comment of the task is rendered: no type filter and no count limit.
- **What each entry shows.** The comment's `type` as a badge, its `created_at` timestamp, an edited marker carrying the `updated_at` timestamp when that value is not null, and the `body`.
- **Type badge colour.** Neutral for every one of the seven task comment types. The semantic colour mapping used for status, priority, and severity is not extended to comment types.
- **Line breaks.** A comment body is multi-line as authored through the CLI, and the timeline preserves the author's line breaks; the text wraps inside the card, so no horizontal scrolling is introduced.
- **Empty state.** A task with no comments shows a plain message in place of the timeline, not an empty list and not a missing section.

### Sprint comments: the Comments card

The dedicated sprint page (`/roadmaps/{name}/sprints/{id}`) shows the sprint's own comments in a Comments card, placed after the member-tasks card and rendered last on the page.

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
- **Tabler dark-theme UI.** The interface is built on the vendored Tabler admin-dashboard framework in its dark theme (navigation sidebar that collapses to a hamburger menu on small viewports, top navbar, page headers, Tabler cards/tables/badges). The top navbar names the roadmap the current page belongs to, so the page's subject is stated at the top of the viewport even where the sidebar has collapsed behind the hamburger menu; the roadmap index page, which belongs to no roadmap, leaves that region empty. Each page header is rendered by one shared template and its title names the view rather than the roadmap - Sprints, Tasks, Audit, Knowledge graph - so the roadmap is stated once in the sidebar and once in the navbar, and never a third time. A sprint's own page is the exception: it shows that sprint's title with its status badge, under the pretitle `Sprint #<id>`. A page header carries an actions control only where one acts on the page (the tasks board's search box, the graph page's layout dropdown) or returns to the parent record (the sprint page's back link); it never repeats a link the sidebar already lists. Task and sprint status, priority, and severity render as colour-coded Tabler badges (for example completed work in green, in-progress in blue, high priority or critical severity in red), so state is scannable at a glance.
- **Audit log as a history tree.** The audit page draws its entries as a git-style history: one lane per path, one point per operation. A sprint's lane branches off the roadmap line and merges back where the sprint closes; it carries the sprint's own operations and those of the tasks that belong to it, while work in no sprint rides a backlog lane. The lane a task's operation sits on is the sprint that task belongs to **now**: the audit log records which sprint an operation touched and never which task was added or moved, so a task later moved between sprints shows its earlier operations on its current sprint's lane. The page states this where the tree is shown. The entry table remains below the tree, with a `Path` column, so a reader whose browser did not run the drawing script still sees every entry and what it belongs to.
- **Self-contained.** Every asset (HTML, CSS, JavaScript, the vendored Tabler framework and D3.js with the d3-sankey plugin, the gitgraph.js history library, the Tabler Icons webfont, and the Inter font) is served from the binary's embedded set under `/static/`; no page references a CDN, a remote font host, or any other remote origin, and the server makes no outbound request.

## See Also

- `SPEC/WEB.md` - full behaviour of the running server (routes, read-only data flow, self-contained delivery, mobile-first design, security)
- `SPEC/COMMANDS.md` (Web Interface) - the command-line contract
- `DOCS/commands/graph.md` - the knowledge graph the graph page visualises
- `DOCS/commands/task.md` and `DOCS/commands/sprint.md` - the comment subcommands that write the logs these pages display
