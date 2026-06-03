# web

## Description

Start a read-only, browser-based view of the data the CLI manages. `rmp web` runs an HTTP server embedded in the `rmp` binary (Go standard-library `net/http`) that serves server-rendered HTML and embedded static assets, reading the same on-disk data under `~/.roadmaps/` that the CLI reads. The interface only presents data; it never changes it. The `rmp` CLI remains the sole write path.

The deliverable is fully self-contained: every asset required to render and operate the interface (HTML templates, the stylesheet, all client JavaScript including the vendored D3.js knowledge-graph library and the d3-sankey plugin, and the favicon) is embedded into the binary with `go:embed`. The interface renders and functions fully offline, references no content delivery network or any other remote origin, and the running server makes no outbound network request. The interface is responsive and mobile-first.

`rmp web` operates across all roadmaps: it lists every roadmap found under `~/.roadmaps/` and you drill into one from the browser. It is the one command that is exempt from the always-required-roadmap rule, so it does **not** accept the `-r` / `--roadmap` flag. It has no subcommands.

Unlike every other command, `rmp web` is long-lived: it keeps serving until interrupted. Sending `SIGINT` (`Ctrl+C`) or `SIGTERM` shuts the server down gracefully and the process exits 0.

## Synopsis

```
rmp web [options]
```

## Options

| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| - | `--host` | string | `0.0.0.0` | Bind host. The default binds all interfaces, which exposes the read-only interface on the network. Restricting the interface to the local machine is the explicit opt-in `--host 127.0.0.1` (loopback only) |
| - | `--port` | integer | `8787` | Bind port (0-65535). When `--port` is omitted and `8787` is in use, the server falls back to an OS-chosen ephemeral port so it still starts. With an explicit `--port` there is no fallback. `--port 0` requests an ephemeral port |
| - | `--no-open` | bool | false | Do not launch a browser; still start the server and print the served URL |
| `-h` | `--help` | bool | false | Show command help |

`rmp web` accepts no positional arguments. An unknown flag or an unexpected positional argument is an input error (exit code 2).

## Output

On successful startup the served URL is printed to stdout as a single JSON object, so the address is machine-readable even when no browser is opened:

```json
{
  "url": "http://0.0.0.0:8787"
}
```

The `url` reflects the actual bound host and port, including an ephemeral port chosen by the fallback. While running, the server serves HTML pages and a JSON graph-data endpoint; those are HTTP responses, not stdout output of the command.

## Routes and Pages

All routes serve `GET` and `HEAD` only. Any other HTTP method on any route returns HTTP `405`.

| Route | Purpose | Response |
|-------|---------|----------|
| `/` | Roadmap index: every roadmap under `~/.roadmaps/`, with links to each roadmap's sprints landing page and graph page (empty-state message when none) | HTML |
| `/roadmaps/{name}` | Roadmap sprints page and landing page: that roadmap's sprints in three tabs (Próximos / Actual / Concluídos, Actual default), the OPEN sprint expanded with its tasks; each sprint links to its own page. Selecting a roadmap on the index lands here | HTML |
| `/roadmaps/{name}/tasks` | Roadmap tasks page: that roadmap's full task table (every task, any status); clicking a task opens a read-only modal with all task fields | HTML |
| `/roadmaps/{name}/sprints/{id}` | Dedicated sprint page: all sprint details and the task list in planned execution order; each task opens the task detail modal | HTML |
| `/roadmaps/{name}/graph` | Interactive knowledge-graph visualisation (D3.js; selectable Networks-section layouts via a dropdown, default Mobile patent suits; pan/zoom, touch, tap-to-inspect) | HTML |
| `/roadmaps/{name}/graph/data` | The graph's nodes and edges for the visualisation | JSON |
| `/static/...` | Embedded static assets (CSS, JS, vendored Tabler framework and D3.js + d3-sankey, fonts) | static file |

`{name}` is validated against the roadmap-name rules (regex `^[a-z0-9_-]+$`, max 50 characters) before it is used to build any filesystem path; a name that fails validation, or a roadmap that does not exist, returns HTTP `404`. A request for a `/static/...` asset that is not embedded returns HTTP `404`. These HTTP statuses are distinct from the process exit codes below.

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
# Start on the default host (all interfaces) and port (opens the browser)
rmp web

# Start without launching a browser; just print the served URL
rmp web --no-open

# Start on a specific port
rmp web --port 9000

# Restrict the read-only interface to the local machine (loopback opt-in)
rmp web --host 127.0.0.1 --port 9000
```

## Read-Only and Security

- **Read-only.** The interface exposes no route that creates, edits, or deletes any roadmap, task, sprint, audit entry, or graph element. Serving a page writes no rows and no audit-log entry. The graph store is opened read-only and a web read never triggers a checkpoint or write-ahead-log truncation.
- **Exposed to the network by default.** The server binds all interfaces (`0.0.0.0`) by default, so the read-only interface is reachable from every network point of the machine. Restricting it to the local machine via `--host 127.0.0.1` (loopback only) is the explicit opt-in.
- **Path-traversal guard.** Roadmap names from the URL are validated before any filesystem path is built, so a crafted name cannot traverse outside `~/.roadmaps/`.
- **Tabler dark-theme UI.** The interface is built on the vendored Tabler admin-dashboard framework in its dark theme (navigation sidebar that collapses to a hamburger menu on small viewports, top navbar, page headers, Tabler cards/tables/badges).
- **Self-contained.** Every asset (HTML, CSS, JavaScript, the vendored Tabler framework and D3.js with the d3-sankey plugin, the Tabler Icons webfont, and the Inter font) is served from the binary's embedded set under `/static/`; no page references a CDN, a remote font host, or any other remote origin, and the server makes no outbound request.

## See Also

- `SPEC/WEB.md` - full behaviour of the running server (routes, read-only data flow, self-contained delivery, mobile-first design, security)
- `SPEC/COMMANDS.md` (Web Interface) - the command-line contract
- `DOCS/commands/graph.md` - the knowledge graph the graph page visualises
