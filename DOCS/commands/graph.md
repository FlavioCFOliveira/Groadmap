# graph

## Description

Operate a roadmap's knowledge graph: a free-form, queryable store of the project's elements and the relationships between them, backed by the GoGraph engine. The graph turns a roadmap into a "second brain" where an AI agent records and retrieves project elements (specs, code, decisions, dependencies) and how they connect, without re-reading every source file.

Each roadmap owns one graph, stored under that roadmap's home directory at `~/.roadmaps/<name>/graph/` (a directory, mode `0700`). **The directory is created by `rmp graph serve` and by nothing else**: a server started against a roadmap that has never had a graph creates it, serves it empty, and the first statement a client sends creates the first node in it. The graph is free-form: Groadmap imposes no schema. It is independent of the roadmap's SQLite tasks and sprints data in this version.

**Using the graph begins by starting a server, and there is no other way in.** The command has two subcommands. `serve` opens the roadmap's graph once, holds it and its exclusive advisory store lock for the life of the process, and answers Cypher over a Unix domain socket until it is stopped; it runs no statement of its own. `client` sends exactly one statement to a running server and prints what comes back. It requires that server: it opens no store, falls back to nothing, and with nothing listening it fails. Groadmap does not examine a statement and refuses none for what it does.

**Only the socket is a choice.** Both subcommands take `--socket <path>` and default it identically to the path derived from the roadmap, so a server started without the flag is reached without it. The flag names which socket is bound and which socket is connected to, and nothing else — there is nothing else for it to select. One condition overrides that resolution entirely: a resolved socket path longer than the platform allows names a socket no process can create, so both subcommands refuse the invocation before they probe anything, and the path derived from the roadmap is refused on exactly the same rule as one written on the command line. See [The socket path has a length limit](#the-socket-path-has-a-length-limit).

## Synopsis

```
rmp graph serve -r <roadmap> [--socket <path>]
rmp graph client -r <roadmap> [--query <cypher>] [--socket <path>]
```

## Subcommands

### serve

Opens the roadmap's knowledge graph once, holds it and its exclusive advisory store lock for the life of the process, and answers Cypher statements over a Unix domain socket until it is stopped. The protocol is Bolt version 5, served by the graph engine's own server; Groadmap defines no protocol of its own and binds no network port, on loopback or anywhere else.

**Starting a server is how a roadmap's graph comes into being.** Against a roadmap that has never had one, `serve` creates `~/.roadmaps/<name>/graph/` with mode `0700` and serves it empty; a read against that graph returns an empty result and is not an error. A `serve` that is refused creates nothing and leaves no directory behind, because the directory is created only after the roadmap and the socket path have both been accepted. `rmp graph client` creates nothing, ever, and neither does the web interface: neither of them opens a store.

`serve` is **long-lived**. Unlike every other command except `rmp web`, it does not complete and exit: it runs until it receives `SIGINT` (`Ctrl+C`) or `SIGTERM`, then drains the work in flight, shuts the server down, checkpoints if the write-ahead log has grown, releases the lock, removes its socket, and exits `0`.

**One server per roadmap.** The roadmap's store lock is the interlock: a second `rmp graph serve` against the same roadmap cannot take it, fails with exit code `1`, and leaves the first server's socket untouched. It does not queue. A server asked to bind a socket that some *other* roadmap's server already owns is refused by the socket probe instead, and again leaves the incumbent's socket alone.

`serve` runs no statement of its own and never reads or writes a roadmap's `project.db`. It serves one roadmap; serving several means running several servers, one per roadmap, each on its own socket. It exposes exactly one database, under the engine's own default name, and a client selects nothing.

**Access control is the filesystem, and there is no other.** The socket is created with mode `0600`, set explicitly rather than left to the process umask, inside a roadmap home directory that is `0700`. The server authenticates nobody: any caller able to open the socket can read, write, delete and change the schema of that roadmap's graph. Connecting to a Unix domain socket needs write permission on the socket file, so "can open it" is the whole of the test. There is no login, no token, no session, and no transport security — the transport is a file in the local filesystem and there is no network hop to protect.

**Two warnings on stderr at startup are expected and are not failures.** The engine emits one for a server running without transport security and one for a server running without an authentication handler. Both are accurate. Both states are the intended ones, and the handler is set explicitly — the engine refuses to construct a server without one — so "no authentication" here is a declaration rather than an oversight.

**Usage:** `rmp graph serve -r <roadmap> [--socket <path>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required). The server serves this one roadmap's graph and no other |
| | `--socket` | string | `~/.roadmaps/<name>/graph.sock` | Unix domain socket to bind. A non-default path is followed by `rmp graph client` through the same flag and by nothing else: the web interface has no way to receive one. See [Serving on a non-default socket](#serving-on-a-non-default-socket). A path longer than the platform allows is refused while the path is resolved, before the bind, and the line names the path's length and the platform's limit instead of the operating system's errno; the derived default path is refused on the same rule (see [The socket path has a length limit](#the-socket-path-has-a-length-limit)) |
| `-h` | `--help` | bool | false | Show subcommand help |

`serve` takes no `--query`, because it runs no statement, and accepts no positional argument.

**Output:** a single JSON object on stdout at startup, naming the absolute path of the socket the server bound, so a caller that supplied no `--socket` still learns the path:

```json
{
  "socket": "/home/user/.roadmaps/backend-platform/graph.sock"
}
```

Per-statement results go to the client that asked for them, never to this command's stdout.

**What startup does, in order.** The order is load-bearing, and knowing it explains the failures below:

1. Resolve the roadmap and the socket path, check the path's length, and only then create the roadmap's graph directory if it has none. A roadmap that does not exist fails here, before anything is opened, created or removed. So does a resolved socket path longer than the platform allows, whether it was derived from the roadmap or supplied through `--socket` (see [The socket path has a length limit](#the-socket-path-has-a-length-limit)). The directory is created last in this step, which is what makes a refused `serve` leave nothing behind — and it is created here rather than at the store open, because the advisory lock file of step 2 lives inside it.
2. Take the graph store's exclusive advisory lock under the ordinary bounded wait. The wait exists for one holder: another `rmp graph serve` for the same roadmap that is shutting down and has not yet released it, so a restart succeeds without a pause between the two processes. An exhausted wait is what refuses a second server against the same roadmap.
3. Refuse to start if a live server already answers on the resolved socket, leaving that socket exactly as it was found. Step 2 is what makes this check sufficient rather than redundant: a server on *this* roadmap's socket already holds *this* roadmap's lock, so the lock refuses it first. What the probe catches is the case the lock cannot — a `--socket` path some other roadmap's server owns.
4. Remove a stale socket file — one a killed server left behind — now that nothing answers on it. This is what lets a relaunch after a kill succeed.
5. Bind the listener and set the socket's mode to `0600`.
6. Open the store and construct the engine.
7. Take `SIGINT` and `SIGTERM` over, flush the startup warnings to stderr, print the socket path on stdout, and serve — in that order.

**The listener is bound before the store is opened, deliberately.** Opening a large graph costs up to about a second, and a caller that resolved the roadmap during that second would find no socket, conclude the roadmap is not served, and fail — against a server that was, at that moment, starting for it. Binding first means such a caller connects and waits for the handshake instead. One narrow window remains, between taking the lock and binding the socket — a probe, an unlink and a bind, microseconds rather than the store open — in which a caller is told that nothing is listening. The failure is loud, deterministic and cleared by retrying.

**The signal take-over comes before the announcement, and that is what the announcement means.** Until step 7 runs, `SIGINT` and `SIGTERM` mean what they mean for every short-lived `rmp` invocation: the process is interrupted and exits `130`, with no drain, no checkpoint and no socket removal. From step 7 they mean the drain of [Durability, checkpoints and shutdown](#durability-checkpoints-and-shutdown). Ordering the change ahead of the announcement is what makes the announced socket a promise rather than a path: a caller that has read it is talking to a process that drains.

**Examples:**
```bash
# Serve a roadmap's graph on the default socket, until Ctrl+C
rmp graph serve -r backend-platform

# Serve on a socket of your own choosing
rmp graph serve -r backend-platform --socket /run/user/1000/backend-platform-graph.sock
```

### client

Sends exactly one Cypher statement to a running graph server over its Unix domain socket and prints the result. **This is the only way to run a statement against a roadmap's knowledge graph**: there is no flag, no fallback and no one-shot form. It reads and writes alike: the server does not examine the statement, so a statement that creates, changes, deletes or alters the schema is executed and committed.

Every class of statement runs through this one subcommand:

- a **read**, such as `MATCH ... RETURN`, including variable-length traversals like `-[*1..3]-`;
- a **write**, such as `CREATE`, `MERGE`, `SET` or `REMOVE`;
- a **deletion**, such as `DELETE` or `DETACH DELETE`;
- **schema DDL** — `CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, `DROP CONSTRAINT`;
- **schema introspection** — `SHOW INDEXES`, `SHOW INDEX`, `SHOW CONSTRAINTS`, `SHOW CONSTRAINT`, each with an optional `YIELD` / `WHERE` / `RETURN` projection tail.

**It requires a server.** `client` resolves `~/.roadmaps/<name>/graph.sock`, or the `--socket` path when one is given, and with nothing listening there it fails with exit code `1` and writes nothing to stdout. It does **not** open the store, and no subcommand does. Start a server with `rmp graph serve -r <roadmap>` and leave it running — it is long-lived, so run it in the background or in another terminal, and wait for its `{"socket": ...}` line before sending a statement. A result from this command is therefore evidence that a server ran the statement, and a failure is evidence that none was reached; neither is a defect.

**The statement runs under a 5-second time budget.** The server is the end that enforces it; the client keeps a later deadline of its own, 7.5 seconds, purely as a backstop against a server that answers nothing, so a statement that committed just before the budget expired is never reported as one that wrote nothing. A statement that exhausts the budget is cancelled, its transaction rolls back whole, nothing is written, and the command fails with exit code 1. The remedy is to narrow the statement — add a label, an indexed property filter, or a `LIMIT` — or to split it into smaller statements.

**A serialisation conflict is retried, not reported.** Two clients writing to the same nodes at the same time is an ordinary situation inside a server, and the store detects the collision rather than preventing it. The losing statement committed nothing, so the client re-sends it under the project's retry policy and reports a failure only when that policy or the statement time budget is exhausted. **An exhausted policy is reported as itself**, on a line of `rmp`'s own rather than the engine's: it names the contention, states that nothing was written, and asks for the same statement again, so a caller can tell "you hit contention, run it again" from "your statement is wrong" instead of guessing. Sustained contention on one node is the shape that produces it, and it is rare at this boundary: sixteen concurrent writers to a **single** node, driven through `rmp graph client`, exhausted the policy on **0 of 7,040** statements. That figure belongs to the delay shape the policy uses — a full-jitter draw rather than a fixed ladder — and it is what the shape was chosen for. Sixteen separate processes are the hard case: losing together, they walk a fixed ladder in lockstep, sleep the same interval and re-collide, which is why the same load under a fixed ladder exhausted on **2.81% to 3.44%** of invocations while the same contention measured inside one process exhausted on 0.18% to 0.43%. Spreading those writes across distinct nodes removes the failure rather than moving its threshold.

**A field the statement writes may be too long for the store's durable formats.** The engine refuses such a commit, nothing is written and the invocation exits 1; a field short enough to commit and too long to fold into a snapshot commits and then refuses every checkpoint of that graph. See [How long a field may be](#how-long-a-field-may-be).

**Usage:** `rmp graph client -r <roadmap> [--query <cypher>] [--socket <path>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required). It selects the graph the statement runs against and, unless `--socket` overrides it, the socket the statement is sent to |
| `-q` | `--query` | string | - | Cypher statement. When absent, the statement is read from standard input |
| | `--socket` | string | `~/.roadmaps/<name>/graph.sock` | Unix domain socket of the server, the same derivation `serve` uses. Write it only when the server was started with the same flag. A path longer than the platform allows fails the invocation with exit code 1, naming the path's length and the platform's limit rather than reporting that nothing is listening; the derived default path is refused on the same rule (see [The socket path has a length limit](#the-socket-path-has-a-length-limit)) |
| `-h` | `--help` | bool | false | Show subcommand help |

**Output:** `{"columns": [...], "rows": [[...], ...]}` when the statement produces result columns; `{"ok": true}` when it produces none. For a data statement the two cases are exactly "has a `RETURN` clause" and "has none". A schema-introspection command produces the listing and returns the `{columns, rows}` shape even though it carries no `RETURN` clause; a `CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT` or `DROP CONSTRAINT` produces no columns and returns `{"ok": true}`.

A statement that **changed** the graph adds one further member to whichever of those two shapes it produced: `counters`, naming what it changed. A statement that changed nothing carries no such member and produces exactly the bytes it produced before the member existed. See [What a statement changed](#what-a-statement-changed-the-counters-member).

**The web interface reads the same values and renders them differently.** The graph data endpoint reaches a server through the same client code, and node and relationship values cross one shared mapping, so what the engine said is what both surfaces carry. What differs is the document around those values: this subcommand publishes the two shapes above, while the graph data endpoint publishes a `{nodes, edges}` view of its own and carries no `columns`, no `rows`, no `counters` and no plan member at all. A caller that wants any of those reads them here.

**Examples:**
```bash
# Read: find which code implements each spec
rmp graph client -r backend-platform \
  --query "MATCH (s:Spec)-[:IMPLEMENTED_BY]->(c:Code) RETURN s.key, c.path"

# Read the statement from standard input
echo "MATCH (n) RETURN count(n)" | rmp graph client -r backend-platform

# Write: create a spec node linked to its implementation
rmp graph client -r backend-platform \
  --query "MERGE (s:Spec {key:'user-authentication'}) MERGE (c:Code {path:'internal/auth/jwt.go'}) MERGE (s)-[:IMPLEMENTED_BY]->(c)"

# Write and return the created node
rmp graph client -r backend-platform \
  --query "CREATE (s:Spec {key:'rate-limiting'}) RETURN s"

# Mutate an existing node
rmp graph client -r backend-platform \
  --query "MATCH (s:Spec {key:'user-authentication'}) SET s.status = 'implemented'"

# Remove a decision node and all its relationships
rmp graph client -r backend-platform \
  --query "MATCH (d:Decision {key:'use-sessions'}) DETACH DELETE d"

# Traverse a dependency chain
rmp graph client -r backend-platform \
  --query "MATCH path = (s:Spec {key:'user-authentication'})-[:DEPENDS_ON*1..3]->(d:Dependency) RETURN path"

# Introspect the registered schema
rmp graph client -r backend-platform --query "SHOW INDEXES"

# Reach a server that was started on a socket of its own
rmp graph client -r backend-platform \
  --socket /run/user/1000/backend-platform-graph.sock --query "SHOW INDEXES"
```

## Running a Graph Server

A server is not an optimisation you may add. It is the graph's only entrance: `rmp graph serve` is the one process that opens a roadmap's store, and every statement, from every surface, is sent to it. This section is what a reader needs in order to run one, reach one, and understand what it does while it runs.

### What a server buys, and what it costs

**What it buys is the store open.** Opening a graph replays the write-ahead log and reconstructs the live graph, which costs up to about a second on a large one. A server pays that once for the life of the process, and every statement after it is a message on a socket.

**It also buys concurrency, which no other configuration in the product has.** Inside a server, transactions run concurrently and the store's MVCC resolves them: readers never block and are never blocked, and two writers run at the same time rather than one waiting for the other.

**Measured**, on an 8-core / 16-thread workstation against a store of the shape a real knowledge graph has: read throughput rises with concurrency and stops rising at about **16** concurrent clients — on a point lookup, from 2,563 operations per second at one client to 17,533 at sixteen, a little under seven times; an engine-dominated full scan peaked at 8.11 times, because that work saturates the eight physical cores rather than the sixteen logical ones. Past that knee another client buys under 5% more throughput while the 99th-percentile latency grows nearly fivefold, so 16 is where useful scaling ends on that machine. The ceiling the server actually enforces is 128 concurrent connections, set well above the knee on purpose: a connection refused for hitting the ceiling is dropped without a protocol answer and is not retried by the client, so the ceiling is placed where it cannot bind rather than where it would begin to.

**What it costs is exclusivity, and a process you have to run.** A server holds the roadmap's store lock for its whole lifetime, so nothing else can open that store while it runs — which is precisely why nothing else tries, and why the resolution rule below has no second path in it. The corollary is the honest one: while no server runs for a roadmap, that roadmap's graph is **unreachable rather than empty**. A statement against it fails, and the graph page for it is unavailable. The data is untouched throughout; the way to it is simply not open.

### How a statement finds the server

Every surface that reaches a graph resolves the socket first: it connects, completes the protocol handshake, and decides from the answer. The probe carries a deadline of 2500 ms, is not retried, and is spent before the statement starts.

| State | How it is recognised | `rmp graph client` | The web graph endpoint |
|-------|----------------------|--------------------|------------------------|
| **Not served: no socket** | The socket path does not exist | Fails, exit code 1, reporting that no server is listening | HTTP `503` |
| **Not served: nothing listening** | The connection is refused, which is what a socket file left behind by a killed server answers | The same failure; the leftover file is neither a different condition nor removed | HTTP `503` |
| **Served** | The connection is accepted and the handshake completes inside the probe deadline | Sends the statement to the server | Sends the statement |
| **Unreachable** | The connection is accepted but the handshake does not complete in time, or the connection fails for any other reason — a path that is not a socket, a permission the caller does not have | Fails, exit code 1 | HTTP `503` |

Five consequences worth stating outright:

- **No caller opens the store, on any outcome.** Resolution decides whether a statement can run, not where it runs, because there is only one place it can run. A caller takes no advisory lock, creates no graph directory and runs no recovery, whether the probe succeeded or failed.
- **`--socket` names which socket is bound and which socket is connected to, and selects nothing else.** There is nothing else for it to select. It moves the meeting point of the pair, and changes nothing about what happens once a caller has looked there.
- **The two negatives are reported apart because they call for opposite actions.** "No server is listening" says to start one. "A server could not be reached" says that something is answering on that path and is not serving this roadmap's graph — a stray file, a socket belonging to another roadmap's server, a server still opening a very large store.
- **A connection lost after the statement was sent is a failure, and the statement's outcome is unknown.** A commit is durable before it is acknowledged, so a connection that dies between the two leaves nobody able to say whether the write happened. The invocation reports exactly that and does not re-run the statement, because re-running it could apply it twice. A caller that must know re-reads the graph, which is why a statement whose effect has to be confirmed is written with a `RETURN` clause or followed by a read.
- **A leftover socket file is never an error, and no caller removes one.** A killed server leaves one behind; the refusal a connection to it receives is the whole of the evidence needed to conclude that nothing is listening. Removing it is the next server's business, at step 4 of startup — a caller that removed one would race a server that was binding it.

### Serving on a non-default socket

`--socket` moves the socket off the path every surface derives. **The command line can follow it; the web interface cannot.**

`graph serve` and `graph client` both publish the flag and both default it identically, so a server on a custom path is reached by giving the same path to the client that talks to it. Nothing is lost on that side.

The web interface's graph data endpoint has no command line, `rmp web` serves every roadmap at once rather than one, and no request parameter carries a socket path. It therefore resolves the derived path, learns inside the probe deadline that nothing is listening there, and answers HTTP `503` — the same answer it gives for a roadmap nobody is serving, because from where it stands that is the same fact. **That happens on every request for that roadmap's graph, deterministically, for as long as that server runs**, and the console record names the derived socket rather than the one the server actually bound, because nothing in the product knows that a server is running elsewhere.

So `--socket` is an option that keeps the CLI and costs the web page. Use it for a server the browser is not expected to reach — a test harness, a diagnostic session, a socket that has to live on another filesystem — and start a server whose roadmap is also browsed without it.

A mistyped path fails loudly rather than quietly: `rmp graph client --socket /typo.sock` finds nothing listening there and says so, exit code 1, instead of reaching some other graph. There is no path on which a typo succeeds, because there is no second route for it to fall onto. One class of mistyped value is caught even earlier: a path longer than the platform allows cannot name a socket at all, so both subcommands refuse the invocation before the probe (see [The socket path has a length limit](#the-socket-path-has-a-length-limit)).

### The socket path has a length limit

A Unix domain socket is named by a path in the filesystem, and the operating system bounds how long that path may be: the kernel copies the path into a fixed-size field of its socket address structure, and a path that does not fit there — terminator included — can be neither bound nor connected to. Left unchecked, such a path surfaces as the kernel's own `invalid argument`, which names neither the length, nor the limit, nor anything you can act on.

**The bound belongs to the platform, so it is not one number.** It is **107 bytes on Linux and Windows** and **103 bytes on macOS, FreeBSD and OpenBSD**. The published line carries no figure of its own: it reports the length of the path it measured and the limit in force on the platform it ran on, so the same line is correct on every one of the nine supported targets.

**The length is counted in bytes, not characters.** A roadmap name carrying multi-byte UTF-8 uses more of the bound than its character count suggests.

**Every surface refuses an over-long resolved path, however the path was chosen.** The check runs on the path the invocation will actually use — a `--socket` value expanded to an absolute path, or the path derived from the roadmap — and it runs before the socket is probed, before any lock is taken and before any directory is created. `serve` and `client` each fail with exit code 1 and the line below; the web interface's graph data endpoint, which publishes no such flag, refuses its request with HTTP `500`.

```
Error: graph server error: socket path is too long: <socket> is N bytes and this platform allows at most M. Use --socket to name a shorter path.
```

`<socket>` is the resolved path, `N` its length in bytes, and `M` the limit the platform yields.

**The web interface answers `500` here and `503` for an unserved roadmap, and the split is deliberate.** An absent socket is evidence that a roadmap is not served *at this moment*, which an operator clears by starting a server; a path over the bound is evidence that no server can **ever** answer there, which no delay alleviates and no server start repairs. Announcing a service that will come back would be false.

**The default path reaches the bound without an unusual roadmap name.** The derived path is the home directory, 22 fixed bytes for `/.roadmaps/` and `/graph.sock`, and the roadmap name. A roadmap name may be 50 bytes, so a home directory of 36 bytes puts the derived path one byte past the figure Linux and Windows yield, and one of 32 bytes puts it past the figure macOS, FreeBSD and OpenBSD yield. It has been reached in practice on a three-character roadmap name under a deep home directory, at 139 bytes. This is therefore not only a `--socket` concern: the path a caller never typed crosses the bound on an ordinary installation.

**On the command line the refusal is fully recoverable without moving the roadmap**, and the line's remedy is truthful for both subcommands: `--socket` naming a path inside the bound passes the check on `serve` and on `client` alike, so a server started on a shorter path serves that roadmap normally and a client given the same path reaches it. The web interface's graph data endpoint has no such flag and no way to receive a path, so for that surface the only remedy is a shorter derived path: a shorter home directory, or a shorter roadmap name.

### Concurrency inside a server

The store's only concurrency control is MVCC, and a server is the only place in this product where that is observable.

- **Readers never block and are never blocked.** A read transaction takes no lock and pins one committed snapshot for its life.
- **Writers do not exclude one another.** Beginning a transaction acquires nothing. Two write transactions against the same roadmap run at the same time.
- **A write-write collision is detected rather than prevented.** The first updater wins; the loser's transaction fails with a retriable serialisation conflict.
- **That conflict is a normal outcome, and it is retried rather than reported.** The losing transaction committed nothing, so re-running its statement runs it against a graph that never saw it. A failure is reported only once the retry policy or the caller's own deadline is exhausted, and it is then reported as itself rather than as the engine's diagnostic: the line names the contention, states that nothing was written, and asks for the same statement again.
- **What exhausts the policy is a single hot node rather than concurrency, and that is the most useful thing a caller can be told.** Measured against a server, holding the writer count at sixteen and varying only the number of distinct nodes written, the policy was exhausted on **0.33%** of 6,000 statements against **one** node, at 474 statements per second; on **0.03%** against **four**; and on none at all against **eight or more**, with throughput rising to 3,494 statements per second at sixty-four nodes. A caller that meets this failure is therefore not being told to reduce its concurrency. It is being told that all of its writers are landing on one node — which is exactly what several agents stamping provenance on the same node looks like — and the remedy that removes the failure rather than moving its threshold is to spread those writes across distinct nodes.

An explicit `BEGIN` to `COMMIT` sequence has the same **5 seconds in total** that a single statement has, however many statements it carries, because the maximum statement timeout clamps a transaction's whole life. A session that sends nothing is closed after 60 seconds; `rmp graph client` sends one statement per invocation and never meets this, but a longer-lived Bolt client must expect to reconnect.

### Durability, checkpoints and shutdown

**Durability does not weaken because the process is long-lived.** Every operation of a transaction is appended to the write-ahead log, then a commit marker, then one synchronisation to disk, and only then is the transaction applied in memory and acknowledged. A client reading a successful commit is reading one that is already on disk, and a crash recovers all of a transaction or none of it. That holds against a kill exactly as it holds against a signal.

**A server does not checkpoint per write.** A full snapshot after every write would make every write cost the whole live graph while its neighbours waited for the pause a capture takes. It checkpoints instead on an age-based cadence while it runs — a snapshot is owed once it is five minutes old, and the loop looks every 75 seconds — and again at shutdown, so the log the next open replays is short. The cadence is provisional, chosen by analogy and left where measurement found no reason to move it; it is not part of any published contract. There is no size trigger and no operation-count trigger, so a burst of writes inside one window grows the log without limit and the next open replays all of it.

**The shutdown checkpoint is conditional, and the condition matters.** It runs when, and only when, the write-ahead log has grown since it was last folded; a shutdown that owes no fold writes nothing at all, and `snapshot/` and `wal` are left byte for byte as the server found them. The reason is not economy. A statement the time budget cut while it was writing is rolled back whole, and the rollback restores the *logical* graph but not the *physical* one: the engine keeps the interned key of every node the statement created, and a tombstone for each. A checkpoint taken afterwards serialises that residue to disk, where nothing removes it. Measured, one cut `MATCH (a),(b),(c) CREATE ()` over a store of 80 KB holding 600 nodes left the store at **134 MB** while the graph still held exactly its 600 nodes, and a later `MATCH (n) RETURN count(*)` over that store cost 1.48 s and 670 MB against 0.01 s and 21.6 MB on a clean one. A cut statement commits nothing, so it appends nothing to the log, so no fold is owed and the shutdown writes nothing — which is what keeps the residue off the disk. The in-flight cadence checkpoint carries no such condition and can still publish it.

**Shutdown drains, and the drain is Groadmap's own** because the engine's shutdown cuts sessions rather than draining them. On `SIGINT` or `SIGTERM` the server stops accepting connections, waits under a bounded timeout (7.5 seconds) for the work in flight to reach a quiescent point, cuts what the drain could not finish, shuts the Bolt server down, checkpoints and truncates the log if a fold is owed, closes the store, releases the lock, removes the socket, and exits 0.

What the drain guarantees, and what it does not:

- **Every acknowledged commit is durable.** This is the commit protocol's doing rather than the drain's, and it holds against an unexpected kill too.
- **A statement in flight is either completed and answered, or cut whole.** A cut statement's transaction rolls back entirely and leaves no partial write.
- **It does not guarantee completion.** The bound is finite; past it the remaining sessions are cut, and a cut client sees a broken connection rather than a typed failure. The store is consistent either way.
- **It does not tell a client whose connection was cut between the commit and its acknowledgement whether the statement committed.** Nothing closes that window.
- **It does not bound the shutdown.** See [Known Limitations](#known-limitations).

A signal that arrives **before** the server has announced its socket means none of this: the process is interrupted like any other, exits `130`, and drains, checkpoints and removes nothing. A socket it had already bound is left behind for the next server's step 4.

### Socket failure lines

Eight failures belong to the graph server rather than to the roadmap, the statement or anything you wrote. Each carries exit code 1. `<socket>` is the resolved socket path; `<detail>` is the operating system's own diagnostic; `N` and `M` are the two byte counts the path-length line carries.

| Condition | Subcommand | Line |
|-----------|-----------|------|
| The resolved socket path is longer than the platform allows, whether derived from the roadmap or supplied through `--socket` | `serve`, `client` | `Error: graph server error: socket path is too long: <socket> is N bytes and this platform allows at most M. Use --socket to name a shorter path.` |
| A live server already answers on the socket `serve` resolved | `serve` | `Error: graph server error: a graph server is already serving <socket>` |
| The socket could not be bound | `serve` | `Error: graph server error: cannot bind <socket>: <detail>` |
| The store lock could not be taken within the bounded wait | `serve` | `Error: graph store error: cannot take the graph store lock for roadmap "X": another rmp graph serve may already be running for it` |
| No server is listening on the resolved socket | `client` | `Error: graph server error: no graph server is listening on <socket>` |
| The socket answered but no server could be reached through it | `client` | `Error: graph server error: graph server unreachable at <socket>: <detail>` |
| The connection was lost after the statement had been sent | `client` | `Error: graph server error: the connection to the graph server at <socket> was lost; the statement's outcome is unknown` |
| The server did not answer within the caller's backstop deadline | `client` | `Error: graph server error: the graph server at <socket> did not answer within 7.5s; the statement's outcome is unknown` |

Seven of the eight carry `graph server error:`, because the server, its socket or the
connection to it is what failed. The lock line carries `graph store error:` instead,
because the failure is the store's and not the server's: `serve` never got far enough
to have a server. The two prefixes are distinct sentinels that both exit `1`.

The path-length line is the only one of the eight that both subcommands write, and the only one that refuses an invocation before the socket is probed at all. It is written for a path you supplied and for the path derived from the roadmap alike; [The socket path has a length limit](#the-socket-path-has-a-length-limit) is canonical for the bound it reports and for why no surface treats it as a roadmap that merely happens to be unserved.

The lock line says "may" deliberately: the lock records no holder, so the invocation reports the overwhelmingly likely cause without asserting it. The last two lines say the outcome is *unknown* rather than that nothing was written, because a commit is durable before it is acknowledged and a line claiming nothing was written would be false in exactly the case a caller most needs the truth.

Two further failures are the **statement's** rather than the socket's, and each holds a line of its own for the same reason the socket lines do — so that a caller can act on it without reading English. Both are wholly `rmp`'s own text and both exit 1:

```
Error: graph engine error: graph query exceeded the 5s statement time budget; nothing was written. Narrow the statement — add a label, an indexed property filter, or a LIMIT — or split it into smaller statements.
```

```
Error: graph engine error: graph write conflict: another writer committed first on every attempt within the 2.5s retry budget; nothing was written. The statement is valid — run it again, and spread concurrent writes across distinct nodes.
```

The first says to change the statement; the second says to run the same statement again. Everything else the engine refuses arrives on the general line, `Error: graph engine error: graph query failed: <engine diagnostic>`.

## The Withdrawn Subcommand Names

`execute`, `create`, `query`, `update`, `delete` and `search` were subcommands of `rmp graph`. They are not any more, and `serve` and `client` are the whole of the family. Each withdrawn name is an unresolved subcommand name and is answered as a dispatch failure: exit code `127`, the `graph` help on stderr, nothing on stdout, and the statement does not run. They are named here because an agent that has one of them in memory needs to be told that it will not resolve.

`create`, `query`, `update`, `delete` and `search` existed to enforce an operation class, and that enforcement was withdrawn; nothing distinguished the five once it was gone. `execute` is listed with them rather than apart from them, and it went for a different reason: it opened the store for a single statement, which is exactly what cannot coexist with a server that holds that store for its process lifetime. **There is no one-shot form and none is planned.**

**`client` runs what it is given, and the caller owns what that does.** No subcommand's contract says that a statement cannot delete. A statement's effect is decided by its Cypher and by nothing `rmp` inspects, so the guarantee you need about a statement is a guarantee about the text you supply. Between reading the statement and sending it, Groadmap checks its length and nothing else about its content. The same is true of the web interface's query bar.

The hazards that follow are all silent and all report success. They are enumerated in `SPEC/GRAPH.md § What Groadmap Does Not Check`, and the ones worth knowing before you type a statement are:

- A statement whose bytes are not valid UTF-8 executes, with every undecodable byte replaced by `U+FFFD`. A write stores a value that was never supplied; a match compares against a literal that was never supplied and reports success having found nothing.
- A property value carrying a control character is stored. Cypher decodes `\b`, `\f` and `\uXXXX` inside a string literal, so a statement whose own text is pure ASCII can write a real `ESC` into the store.
- A relationship property written through an incoming or undirected pattern is not reliably written, and the statement still reports success. Every relationship is writable through an outgoing pattern, which may be anchored on either endpoint: use `MATCH (s)-[e]->(v:Test {key:'…'}) SET e.last_commit = '…'` rather than the reverse spelling. See [Known Limitations](#known-limitations) for what an undirected pattern does to a `SET` that matched more than one relationship.
- Reading is unaffected: a relationship read through an incoming or undirected pattern, fixed-length or variable-length, reports its identity, its type and its stored orientation correctly. The reach of the write hazard stops at writing.
- A schema DDL statement carrying a further clause after it executes **in part**: the engine's schema parser stops when its grammar is satisfied and discards the rest without an error or a notification. Issue the two halves as two invocations.
- A schema-introspection command written with anything but a single space between its two keywords fails as a syntax error whose message names `SHOW` rather than the spacing. `SHOW  INDEXES` fails; `SHOW INDEXES` succeeds. The same is true of the four DDL forms.

## Managing the Schema

The surface is the engine's own Cypher, not a Groadmap verb. There is no `index` subcommand, no `--create` / `--drop` flags, and no vocabulary of Groadmap's own: a schema statement is written through `--query` or standard input exactly as every other statement is. The set of schema statements supported is therefore exactly the set the engine supports, and it widens or narrows with the engine rather than with a Groadmap release.

Groadmap declares no schema object of its own. No `rmp` command creates, drops, or requires an index or a constraint as a side effect of anything else it does, so every schema object in a graph is one its owner asked for. A graph that has never been given one is fully functional: an index is an optimisation you may choose, and a constraint is an integrity rule you may choose.

An index and a constraint each cover exactly one node property. The engine supports neither a composite (multi-property) form nor a form over a relationship property, and a constraint is either a uniqueness rule (`IS UNIQUE`) or a presence rule (`IS NOT NULL`).

Every statement in this section is sent to a running server, like every other statement.

**Creating an index or a constraint**

```bash
# Named index on one node property
rmp graph client -r backend-platform \
  --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"

# Unnamed: the engine derives the name from the lowercased label, the lowercased
# property and the index kind, joined by underscores. This one registers as
# spec_title_hash
rmp graph client -r backend-platform \
  --query "CREATE INDEX FOR (n:Spec) ON (n.title)"

# IF NOT EXISTS makes a create whose object already exists a silent no-op
rmp graph client -r backend-platform \
  --query "CREATE INDEX spec_key IF NOT EXISTS FOR (n:Spec) ON (n.key)"

# An index is a hash index by default; a comparison-ordered index is requested
# through the statement's OPTIONS map
rmp graph client -r backend-platform \
  --query "CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}"

# A uniqueness constraint, and a presence constraint
rmp graph client -r backend-platform \
  --query "CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE"
rmp graph client -r backend-platform \
  --query "CREATE CONSTRAINT spec_key_req IF NOT EXISTS FOR (n:Spec) REQUIRE n.key IS NOT NULL"
```

Each of these statements produces no result columns, so on success it outputs `{"ok": true}` and exits 0, carrying `"indexesAdded": 1` or `"constraintsAdded": 1` under `counters`. An `IF NOT EXISTS` form whose object is already registered changes nothing and outputs `{"ok": true}` alone.

A `CREATE INDEX` back-fills the new index from the data already in the graph. A `CREATE CONSTRAINT` validates the data already in the graph first and registers the constraint only if it passes; a uniqueness rule over a property that already holds a repeated value, or a presence rule over a property some node lacks, is refused with exit code 1 and nothing is registered. That is why the presence constraint above names `key`, which every node in these examples carries, rather than an optional property.

Index kinds are the engine's own vocabulary — `hash` by default, `btree` through `OPTIONS` — and not the index kinds of another Cypher implementation. A statement written against another implementation's vocabulary is refused by the engine.

**Dropping an index or a constraint**

```bash
# Removal is by name only, never by a label-and-property pair
rmp graph client -r backend-platform --query "DROP INDEX spec_key"
rmp graph client -r backend-platform --query "DROP CONSTRAINT spec_key_uq"

# IF EXISTS makes a drop of an absent object a silent no-op
rmp graph client -r backend-platform --query "DROP INDEX spec_key IF EXISTS"
```

A drop that removed the object outputs `{"ok": true}` carrying `"indexesRemoved": 1` or `"constraintsRemoved": 1` under `counters`. A `DROP CONSTRAINT ... IF EXISTS` that found nothing to drop changes nothing and outputs `{"ok": true}` alone.

**The figure a no-op `DROP INDEX ... IF EXISTS` reports is currently unreliable.** Over an index that does not exist, the engine reports `"indexesRemoved": 1` although nothing was removed. Only this one form is affected: a `DROP CONSTRAINT ... IF EXISTS` over an absent constraint reports no counter, and neither does a `CREATE INDEX ... IF NOT EXISTS` over an index already registered. The count comes from the engine, and the defect is tracked for repair; until it is fixed, read `indexesRemoved` after an `IF EXISTS` drop as saying that the drop ran, not as evidence that an index was there to remove. `SHOW INDEXES` remains the authoritative report of what is registered.

Because removal is by name only, a caller who did not declare a name must first learn the derived one from a listing. Declaring a name is the recommended practice and Groadmap does not enforce it: a named object is dropped by the name its author wrote, while an unnamed one is dropped by a name the engine chose, which changes if the index kind changes.

**Listing the schema**

```bash
rmp graph client -r backend-platform --query "SHOW INDEXES"
rmp graph client -r backend-platform --query "SHOW CONSTRAINTS"

# The singular aliases are the same commands
rmp graph client -r backend-platform --query "SHOW INDEX"
rmp graph client -r backend-platform --query "SHOW CONSTRAINT"

# With a projection tail
rmp graph client -r backend-platform --query "SHOW INDEXES YIELD name, type"
```

`SHOW INDEXES` and `SHOW CONSTRAINTS` are the authoritative report of what a schema object is called, and are how you learn a derived name. A listing is ordered deterministically, so two invocations against an unchanged graph produce the same rows in the same order. A uniqueness constraint also registers an index of its own, under a name the engine derives, so a listing after one shows more than the indexes you asked for.

**Altering and recreating: two invocations, and not atomic**

The engine has no statement that changes an index in place. There is no `ALTER INDEX`, no `REBUILD INDEX`, and no `CREATE OR REPLACE INDEX`, and each of the three is refused by the parser as an unrecognised statement. Changing an index — its kind, or its definition — and rebuilding one are therefore a `DROP` followed by a `CREATE`, issued as **two separate invocations**; Groadmap composes nothing on your behalf:

```bash
rmp graph client -r backend-platform --query "DROP INDEX spec_ord"
rmp graph client -r backend-platform \
  --query "CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}"
```

Altering is a drop followed by a create with a different definition; recreating is a drop followed by a create with the identical definition, and the rebuild is the back-fill the create performs.

**The pair is not atomic.** The two invocations are two processes, and nothing spans them. If the second fails — a rejected definition, a server that stopped between the two, a machine that did — the index is **dropped and not recreated**, and the graph is left with no index where it had one. Nothing in Groadmap detects that state, reports it, or repairs it; you learn of it from `SHOW INDEXES` and repair it by issuing the create again. Queries stay correct throughout, because an index is an access path and never a source of results, so what is lost is speed rather than answers.

Both halves cost time proportional to the graph: a create back-fills the index from every node carrying the label, and a drop discards that work. On a roadmap knowledge graph this is small, and it does not stay small if the graph grows.

**One statement per invocation**

An invocation carries exactly one statement, and nothing enforces that.

The engine's schema parser stops as soon as its grammar is satisfied and **discards the rest of the statement silently** — without an error, without a notification, and without any other trace. Handed

```
CREATE INDEX ix FOR (n:Spec) ON (n.key) MATCH (m) SET m.p = true
```

the engine creates the index, drops the `MATCH ... SET` on the floor, and returns success, so `graph client` prints `{"ok": true, "counters": {"indexesAdded": 1}}` and exits 0 for a statement half of which never ran — and you have no reason to check, because the command reported that it worked. The counters name the index and nothing else, because the discarded half applied nothing to count; that absence is a trace rather than a report, since a `SET` that legitimately writes nothing produces no figure either. Issue the two halves as two invocations.

A schema-introspection command carrying a further clause is refused by the engine, which names the unsupported clause rather than discarding it. And a statement that *begins* with a data-writing clause and carries schema text after it is not a schema statement at all: the engine routes it to the general Cypher grammar, which refuses it as a parse error (exit code 1).

**Schema failure classes and their exit codes**

A schema statement is refused by the **engine**, and the refusal arrives on the general parse-or-execution line with exit code **1**.

| Failure | Refused by | Exit code |
|---------|-----------|-----------|
| `CREATE INDEX` or `CREATE CONSTRAINT` whose object already exists, without `IF NOT EXISTS` | Engine | 1 |
| `DROP INDEX` or `DROP CONSTRAINT` naming an object that does not exist, without `IF EXISTS` | Engine | 1 |
| A definition the engine does not support — composite, over a relationship property, or a constraint kind it does not implement | Engine | 1 |
| `CREATE CONSTRAINT` that the data already in the graph does not satisfy | Engine | 1 |
| A `SHOW INDEX(ES)` / `SHOW CONSTRAINT(S)`, or a DDL form, whose keyword spacing routes it to the general Cypher grammar | Engine | 1 |

A duplicate create and a drop of an absent object are **engine** failures rather than validation failures, and they exit **1** rather than 6. This is stated explicitly because the exit code is the opposite of what a reader may expect: both look like input errors, and neither is one. Whether an object exists is knowable only inside the graph, which only the server has open, so the check belongs where the knowledge is. A caller who wants either to be a no-op writes `IF NOT EXISTS` or `IF EXISTS`.

```bash
# Exit 1: the engine refuses the second create, because the object exists
rmp graph client -r backend-platform --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"
rmp graph client -r backend-platform --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"
# Error: graph engine error: graph query failed: <engine diagnostic>
```

**How much of the engine's diagnostic you actually read depends on how the engine classified the failure.** A duplicate create, a drop of an object that does not exist, a spacing failure, and a uniqueness rule the data does not satisfy all arrive with the engine's own text — the name it could not add, the name it could not find, the parse position, the duplicate value — because its Bolt server classifies them as the caller's fault and forwards the message. Two are classified as the server's own fault instead, and the session replaces the message with generic internal-error text naming only the session: a definition the engine does not support, and a **presence** rule (`IS NOT NULL`) the data does not satisfy. The split between the two constraint kinds is the engine's and is not a rule you can read off the statement. The refusal, the sentinel and the exit code are unchanged either way; only the diagnostic differs, and the full text is in the server's stderr. See [Through the server, some diagnostics are replaced](#through-the-server-some-diagnostics-are-replaced).

A failed schema statement leaves the schema as it was. No partial registration exists in any of these classes: the object is either registered or it is not.

## What a Statement Changed: the `counters` Member

`{"ok": true}` says that a statement succeeded. It does not say what the statement did,
and two invocations that both print it may have created a node and matched an existing
one, or deleted a thousand relationships and deleted none. A statement that **changed**
the graph therefore publishes one further member, `counters`, naming the effects it
applied — so a caller can tell those cases apart without issuing a second statement to
read the graph back, which is the only way it could have found out before, and which
reads a graph other writers may have changed in the meantime.

```bash
rmp graph client -r backend-platform \
  --query "CREATE (:Spec {key:'rate-limiting', status:'draft'})"
```

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

A write that declares a `RETURN` clause carries the same member beside its rows:

```bash
rmp graph client -r backend-platform \
  --query "MATCH (s:Spec {key:'rate-limiting'}) SET s.status = 'implemented' RETURN s.key"
```

```json
{
  "columns": ["s.key"],
  "rows": [["rate-limiting"]],
  "counters": {
    "propertiesWritten": 1
  }
}
```

The eleven keys, in the order they are written:

| Key | Counts |
|-----|--------|
| `nodesCreated` | Nodes the statement added to the graph |
| `nodesDeleted` | Nodes the statement removed from the graph |
| `relationshipsCreated` | Relationships the statement added |
| `relationshipsDeleted` | Relationships the statement removed, including the ones a `DETACH DELETE` removed as a consequence of deleting a node rather than by naming them |
| `propertiesWritten` | Property assignments and property removals together, as one figure. See [below](#why-propertieswritten-is-one-figure) |
| `labelsAdded` | Labels attached to a node. Creating a node with a label counts the label here as well as the node under `nodesCreated` |
| `labelsRemoved` | Labels detached from a node |
| `indexesAdded` | Indexes a schema statement registered |
| `indexesRemoved` | Indexes a schema statement dropped |
| `constraintsAdded` | Constraints a schema statement registered |
| `constraintsRemoved` | Constraints a schema statement dropped |

**Nothing you already parse changes.** The member is additive and written last: `ok`,
`columns` and `rows` keep their meanings and their positions. A counter whose value is
zero is left out, so the block is never empty when it is present and an absent key
reads as zero. And a statement that changed nothing carries **no `counters` key at
all**, producing exactly the bytes it produced before the member existed — every read,
every `EXPLAIN`, which executes nothing, a `MERGE` that matched an existing element,
and a `DELETE` whose pattern matched no row:

```bash
# Matched what it would otherwise have created: no counters key
rmp graph client -r backend-platform --query "MERGE (s:Spec {key:'rate-limiting'})"
# {"ok": true}

# Matched no row: no counters key
rmp graph client -r backend-platform \
  --query "MATCH (s:Spec) WHERE s.key = 'no-such-spec' DELETE s"
# {"ok": true}
```

**The figures are a property of the statement and the graph**, not of the run that
executed it, so the same statement against the same graph reports the same figures on
every run. They are published in this subcommand's output and nowhere else: the web
graph data endpoint renders a `{nodes, edges}` document and carries no `counters`
member, so a statement whose effect has to be counted is one to run from the command
line.

### Why `propertiesWritten` is one figure

The engine counts a property **assignment** and a property **removal** separately. The
published member is their sum, and its name says so.

The constraint that decides this is the Bolt protocol every result crosses: its
statistics carry a single properties counter and no counterpart for a removal. A result
therefore arrives with the two already summed, and the protocol offers no second channel
from which the split could be recovered. Publishing a removal under a key named for an
assignment would claim an effect that did not occur, so the key is named for the sum.

Three consequences worth knowing:

- A `REMOVE` that removed one property reports `"propertiesWritten": 1`. That is
  correct, not a defect.
- Assigning `null` to a property removes it, and so counts here too.
- The figure alone cannot tell you whether a statement assigned two properties, removed
  two, or did one of each. The statement you wrote can.

Deleting an element counts no property removal, so a `DETACH DELETE` reports no property
figure at all — only the node and the relationships that went with it, here a decision
carrying one:

```bash
rmp graph client -r backend-platform \
  --query "MATCH (d:Decision {key:'use-sessions'}) DETACH DELETE d"
```

```json
{
  "ok": true,
  "counters": {
    "nodesDeleted": 1,
    "relationshipsDeleted": 1
  }
}
```

### A counter is not a promise that the write can be read back

Each figure is incremented where the engine applied the change, which is not the same as
saying the change is afterwards visible. The two relationship-write defects under
[Known Limitations](#known-limitations) both report their counters as though the write
had persisted: where their preconditions hold, `counters` is exactly as silent about the
loss as `{"ok": true}` was. What the member adds is a signal where there was none; what
it does not add is a guarantee that was never there.

`SPEC/DATA_FORMATS.md § Graph Query Counters` is canonical for the shape and the key
set, and `SPEC/GRAPH.md § Write Counters: What a Statement Changed` for the behaviour.

## Query Plans: `EXPLAIN` and `PROFILE`

A statement may ask for its plan instead of, or as well as, its answer. Both prefixes
are recognised without regard to case. The client maps the protocol encoding back onto
the engine's own plan node rather than onto JSON, so what a caller reads is the engine's
own tree rather than a second rendering of it, and one `EXPLAIN` of one statement is
identical in every byte from run to run. A `PROFILE` has one figure that cannot be, and
no implementation could make it one: `timeNs` measures the run that produced it, and two
submissions are two runs. Every other key — `rows`, `dbHits`, `rowsRemovedByFilter` and
the estimate pair included — is a property of the statement and the graph, and does not
move between runs. **A plan is a command-line result**: the web graph data endpoint
renders a `{nodes, edges}` document and carries no `plan` or `profile` member, so a
prefixed statement submitted from the query bar returns that document and no tree.

| Prefix | Does it run the statement? | What it returns | Member |
|--------|---------------------------|-----------------|--------|
| `EXPLAIN` | No. The statement is planned and nothing is executed | The statement's declared columns, **no rows**, and the plan the planner built | `plan` |
| `PROFILE` | Yes | The statement's real rows, and what each operator actually cost | `profile` |

The two members are never both present. That separation is the point of the feature:
an estimate must not be readable as a measurement. The only counts an `EXPLAIN` can
carry are `estimatedRows` and `estimatedRowsSource`, which are predictions made before
anything ran, and an operator the planner had no estimate for carries neither. A
`PROFILE` adds `rows`, `timeNs`, `dbHits` and `rowsRemovedByFilter`, which are
measured.

```bash
# Plan a read without running it
rmp graph client -r backend-platform \
  --query "EXPLAIN MATCH (s:Spec)-[:IMPLEMENTED_BY]->(c:Code) RETURN s.key"

# Run it and measure every operator
rmp graph client -r backend-platform \
  --query "PROFILE MATCH (s:Spec) WHERE s.status = 'implemented' RETURN s.key"
```

A plan is a recursive object. Each node names the `operator` that runs, the `detail` of
the physical decision it took, and the `children` it draws its rows from, in
**execution** order — a join's build side before its probe side, an apply's outer before
its inner. `operator` is the only key always present, whichever member carries the tree;
a consumer must treat every other key as optional.

| Key | In a `plan` | In a `profile` | What it is |
|-----|-------------|----------------|------------|
| `operator` | Always | Always | The operator's type name — or, in a logical plan, the whole plan line (see below) |
| `detail` | When it has one | When it has one | The physical decision: the label it scans, the index it seeks, the pattern it expands |
| `children` | When it has any | When it has any | The operators it draws its rows from, in execution order. A leaf carries no key |
| `estimatedRows` | When the planner had an estimate | The same, and the same number | The planner's prediction, made before anything ran |
| `estimatedRowsSource` | With `estimatedRows` | With `estimatedRows` | Where the estimate came from, and therefore how far it may be trusted: `exact`, `stats` or `heuristic` |
| `rows` | Never | Yes | Rows the operator emitted. Measured |
| `timeNs` | Never | Yes | Whole nanoseconds attributed to the operator. Measured |
| `dbHits` | Never | When the figure was counted | Storage record accesses charged to the operator |
| `rowsRemovedByFilter` | Never | When the operator can reject a row | Candidate rows the operator read and then discarded because a predicate said no |

`estimatedRows` is published for an `EXPLAIN` and for a `PROFILE` alike, and it is the
same number in both, because the planner predicted it before either ran. Reading it
beside a `profile`'s measured `rows` is the point: a bad estimate becomes visible in one
object.

**Four presence rules decide what a number means, and reading any of them backwards
gives a wrong answer.**

- **An absent `dbHits` means "nobody counted", never "none".** The engine distinguishes
  an operator whose storage accesses were counted and came to zero — which publishes
  `0` — from one whose accesses nobody counted at all, which publishes no key. The
  engine's own text rendering prints `?` for the second case, for the same reason. A
  reader who treats the absent key as a zero has a measurement that was never taken.
- **A `rowsRemovedByFilter` of `0` is a finding, and its absence says something else.**
  An operator that can reject rows and rejected none reports `0` deliberately: a filter
  that rejected nothing is exactly what a reader of a slow plan is looking for. The key
  is omitted only by an operator with no rejection mechanism at all. The asymmetry
  against the rule above is intended: there an absent key admits that a figure exists
  and was not counted, here it states that there is no figure to have.
- **An absent estimate is an absent estimate, not a zero**, and the pair is written as a
  pair. An operator the planner attributed no estimate to, and one whose backing
  statistic was absent or stale, both omit `estimatedRows` and `estimatedRowsSource`
  together; an estimate with no provenance is a bare number a reader cannot weigh. A
  genuine estimate of zero is published as `0`.
- **`timeNs` is inclusive of the node's children.** The figure covers everything the
  operator drew from the operators beneath it, so summing a tree's nodes double-counts
  every level; the root's figure is the one that describes the statement. It is a whole
  number of nanoseconds rather than a millisecond value, so that the one integer the
  engine already holds is the one that is published; a consumer that wants milliseconds
  divides, and the rounding is that consumer's decision.

**`dbHits` counts access-path record reads and never property reads.** This is a
deliberate divergence from Neo4j, which additionally charges one hit per property read,
and it is published because a caller comparing the two products otherwise concludes the
figure is wrong. It is not: it counts a different thing, and it counts that thing
exactly.

**An `EXPLAIN` of a writing statement publishes a logical plan, whose nodes carry
`operator` and `children` and nothing else.** A write's operators bind to an open
transaction, so there is no physical operator tree to walk outside one — and opening one
is precisely what `EXPLAIN` must not do. In such a tree `operator` is the plan line as
the engine writes it, with any detail already inside that string, so there is no
separate `detail` and no estimate pair beside it. A consumer that parses `operator` as a
bare operator type name is correct for every reading statement and wrong for this one,
which is why the case is stated here rather than left to be discovered.

**A prefixed statement always returns the `{columns, rows}` shape**, even when it
declares no column of its own — empty `columns` and `rows` arrays beside the plan,
rather than `{"ok": true}`. This is deliberate and it is the defect the feature closed:
an `EXPLAIN` of a writing statement used to print `{"ok": true}`, byte-identical to
what a real committed write reports, over a statement that had written nothing.

**Two statements are refused rather than planned.** A `PROFILE` of a **writing**
statement is refused, because profiling it would mean committing it. Neither prefix is
accepted on a **schema** statement. Both refusals exit `1`.

`SPEC/DATA_FORMATS.md § Graph Plan Node` is canonical for the shape and for when each
key is present; `SPEC/GRAPH.md § Query Plans: The EXPLAIN and PROFILE Prefixes` for the
behaviour.

## How Long a Field May Be

Groadmap checks no field length, and cannot. A statement's fields are the values its
expressions produce, so learning them means executing the statement — which is what the
engine does. Every refusal in this section is therefore the engine's, already made; what
is documented here is what you are told about it and what happens next.

**A field goes into two durable formats, and they do not bound it alike.** A committed
write is appended to the write-ahead log; a later checkpoint folds the committed state
into a snapshot. The log bounds a field by the capacity of the length prefix its frame
reserves, which is an encoding limit. The snapshot bounds one by what its own reader is
required to accept, which is an anti-exhaustion control — a reader that allocated for
any length a prefix could express would allocate gigabytes on an untrusted file. The two
are set independently, and neither is uniformly the stricter.

| Field | Write-ahead log, at commit | Snapshot, at checkpoint | Which bound binds |
|-------|----------------------------|-------------------------|-------------------|
| Node or edge label | 65535 bytes | 1 MiB | The log, by a factor of sixteen |
| Node or edge property key | 65535 bytes | 1 MiB | The log, by a factor of sixteen |
| Index or constraint identifier | 65535 bytes | 64 KiB | Neither: the engine's Cypher parser bounds it far below both |
| Property value, list element, list element count | 4294967295 bytes | 1 GiB | **The snapshot**, at a quarter of the log's bound |
| Node key | 4294967295 bytes | 1 GiB | **The snapshot**, at a quarter of the log's bound |

These figures are read from the pinned engine and are recorded here as evidence, not as
a contract. They move with the engine, and no line `rmp` prints repeats one from this
page: every published line carries the figure the engine reported for the run that
produced it.

**For a property value, write under 1 GiB, and treat the log's four-gigabyte figure as
the misleading one.** The last column is the column that matters when writing. A value
between 1 GiB and 4 GiB is inside the log's bound, so it commits — acknowledged, durable
and recoverable. It is outside the snapshot's, so from that moment every checkpoint of
that graph fails, the write-ahead log is never folded again and never reclaimed, and it
grows for as long as the value remains. A caller who read only the log's figure would
write two gigabytes believing it legal, and would be told nothing until the checkpoint.

### A field too long for the log refuses the commit

The commit is refused, nothing is written, and the invocation exits `1`.

**Nothing is written and the store stays usable.** The refused transaction consumes a
sequence number and applies nothing, so the graph holds no part of the statement — not
the elements it created before it reached the over-long field, and not the properties it
set on them. An ordinary write submitted immediately afterwards succeeds, and a
following `MATCH` counts it.

**Two field kinds are reachable from a statement, and the others are not.** The
65535-byte bound is what a node or edge **label** and a node or edge **property key**
meet, and those two are what a caller writes in a pattern. A schema identifier does not
reach it: the engine's Cypher parser bounds an index or constraint name, label and
property far below 65535 bytes at the point the statement is parsed, so a schema
statement is refused there instead, with the parser's own message and as an ordinary
engine refusal (see [Managing the Schema](#managing-the-schema)). A property value does
not reach either of its bounds from a literal, because a literal of a gigabyte does not
fit inside the 1 MiB maximum query length; a field of that size is one a statement's own
expressions produce.

**A line of its own is specified for this class, and at the pinned engine it is not
reachable.** The specification publishes

```
Error: graph engine error: graph field too long; nothing was written. Shorten the field the engine names: <engine diagnostic>
```

so that a caller can tell "shorten one of your values" from "correct your syntax"
without reading English — the same reason the statement budget and the exhausted retry
each hold a line of their own. What a caller actually reads today is the general
parse-or-execution line carrying generic internal-error text, because every statement
crosses a server and the engine's Bolt server replaces the message of this class
(see [Through the server, some diagnostics are replaced](#through-the-server-some-diagnostics-are-replaced)).
The line stays published because the class is real and the remedy is known and small;
it is published here as **not yet reachable** rather than left for a caller to wait for.
Nothing about the failure itself changes: the sentinel is the same, the exit code is 1,
nothing is written, and the store stays usable.

Two neighbouring refusals are **not** this class. A node key longer than the
log's 32-bit prefix is refused by the engine's node-key codec, and an assembled log
frame over the engine's frame ceiling — which one list property of many
individually-legal elements can reach without any one of them being over-long — is
refused by the log's framer. Both are length refusals in spirit; both arrive through the
ordinary `graph query failed: ` line.

### A field too long for the snapshot commits, and then no checkpoint succeeds

This is the other half of the condition, and it is not the first half worded
differently. The field the snapshot refuses is already **committed graph state**, so it
is not a statement to correct and re-run.

**The write succeeded.** The commit is the durability boundary and it was crossed: the
client prints its normal success output and exits `0`. What fails is a checkpoint that
runs later, inside the server, and a checkpoint failure after a durable commit never
fails the write.

**It cannot heal, and that is what separates it from every other checkpoint failure.** A
checkpoint refused because a disk filled, a permission was wrong or a write was
interrupted may succeed the next time one runs, and the next successful checkpoint
reconciles the snapshot. Here every later capture captures the same committed field, so
every later checkpoint is refused for the same reason. Nothing in the environment
changes it and no amount of waiting resolves it. **The condition is permanent until the
offending field is shortened or removed by a statement.**

**What it costs is bounded growth, not data.** The engine's guard fires while the
capture is still being assembled, before any snapshot file is written and before the
log's prefix is truncated. Every acknowledged commit therefore stays durable in the
write-ahead log, recovery still restores it in full, and the store stays open and
usable. What is lost is the truncation: the log keeps growing for as long as the field
is in the graph, and every open replays more of it, so recovery time grows with it. That
cost is real and unbounded, which is why the condition is reported rather than absorbed
— but it is not a durability failure, and a diagnostic that read as one would be worse
than none.

**The report is on the server's stderr, and the caller sees nothing.** The write that
introduced the field succeeded and was answered; the checkpoint that later refuses it
belongs to the server process, so that is where the record appears. An operator reading
a server's log is the reader this condition has.

**The diagnostic says four things**, and no exact wording for it is published, because
it accompanies a success rather than a failure and is therefore not one of the error
lines fixed character for character. What is fixed is the content: that every
acknowledged commit is still durable and recovery still restores it; that the log was
not folded, so it keeps growing and the next open replays more of it; that the condition
will persist through every later checkpoint while the field remains; and that the remedy
is to shorten or remove the offending field with a statement. It ends in the engine's
own error, which is the half that names which field is at fault.

**One of the server's two checkpoints classifies it, and the other cannot.** The
shutdown checkpoint holds the checkpoint's error, so it reports this condition rather
than the general one. The **in-flight** checkpoint of the age-based cadence does not: it
runs on the engine's own loop, and what Groadmap can observe of it is a statistics value
carrying the last failure as a rendered string rather than as an error. There is nothing
there to classify without matching the engine's wording, which is what this whole
feature is built to avoid, so that one report stays the general checkpoint-failure
record with the engine's own text inside it. The kind is legible there either way.

### Through the server, some diagnostics are replaced

Every statement crosses a Bolt connection, and the engine's server decides for each
failure whether the caller reads its diagnostic or a replacement.

**A failure the server classifies as the caller's own crosses intact.** A parse error
arrives with its position and its expected tokens; a duplicate `CREATE INDEX` arrives
naming the index; a `DROP INDEX` of an absent object arrives naming what it could not
find. These are the common cases, and nothing is lost in them.

**A failure the server classifies as its own is replaced**, with generic text naming
only the session:

```
Error: graph engine error: graph query failed: An internal error occurred. See server logs for details (session: <id>).
```

The over-long field of the section above is one such case; so are a schema definition
the engine does not support and a **presence** constraint (`REQUIRE ... IS NOT NULL`)
the existing data does not satisfy. The neighbouring **uniqueness** constraint is not:
a `REQUIRE ... IS UNIQUE` over duplicated data arrives with the engine's own text,
naming the property and the repeated value. The two kinds are refused for the same
reason and reported differently, which is the engine's classification and not something
a caller can predict from the statement. The sanitised wording is the engine's at the
pinned version and not a Groadmap contract; what is stable about it is that it names the
session and nothing else.

**It is not a different failure, and the diagnostic is not destroyed.** The sentinel and
the exit code are what they would otherwise be, nothing is written, and the store stays
usable. The engine logs the full text under that same session on the server's own
stderr, so a caller who meets one of these lines and needs to know which field, which
definition or which constraint was at fault reads the stderr of the server that answered
the statement — which is one reason to keep a server's output somewhere you can read it.

Groadmap does not close this on its own side, and there is no interception point at
which it could: the engine's server exposes no error-mapping option, and Groadmap runs
no statement of its own between the caller and the server. The one remaining lever would
be matching the replaced text, which names nothing worth matching. The remedy belongs in
the engine.

## Query Input Source and Precedence

`client` obtains its Cypher from one of two sources:

1. When `--query` is present and non-empty, its value is used and standard input is not read.
2. When `--query` is absent, the statement is read from standard input under a bound; the read is not a read to EOF (for example `cat query.cypher | rmp graph client -r backend-platform`).
3. When `--query` is absent and standard input supplies no statement — it is a terminal, it is already at end of stream, or everything it carries is whitespace — the command fails with exit code 2 (no query supplied). A terminal is refused without being read at all.
4. When `--query` is present but its value is empty, whitespace only, or absent, the command fails with exit code 2. A following token that begins with `--`, or with a single `-` followed by an ASCII letter, is the next flag and is never swallowed as the query; a `-` followed by a digit or a decimal point is a legitimate query value.
5. Leading and trailing whitespace is trimmed before the statement is sent, after the length check.

A statement longer than 1048576 bytes (1 MiB) is refused with exit code 6, whichever source carried it. That is the only cause of exit code 6 this subcommand has, and the check runs before anything is resolved or connected to.

`serve` takes no statement at all and therefore reads neither source.

`client` does not accept a **positional argument**. A bare Cypher statement written on the command line is an excess positional argument and is refused with exit code 2 and the line:

```
Error: invalid input: unexpected argument "X" (graph queries use --query or stdin)
```

The refusal precedes the socket resolution, the standard-input read, and the maximum-length check, so a refused invocation does nothing at all. `serve` accepts no positional argument either, and refuses one with exit code 2.

Supplying `--socket` with an empty value is a missing parameter on both subcommands: exit code 2, `Error: required parameter missing: --socket`.

## Modelling Conventions

The graph is free-form, but it tends toward a multi-layer model (specification, code, decisions, dependencies). These are recommendations only; Groadmap does not enforce or auto-create any schema:

- **Layer as a label.** Tag each node with a label naming its layer, such as `Spec`, `Code`, `Decision`, `Dependency`, or `Requirement`.
- **Identity as a property.** Give each node a stable identifier property (for example `key` or `path`) so you can `MERGE` on it without creating duplicates.
- **Cross-layer relationships as typed edges.** Use verb-like edge types such as `IMPLEMENTS`, `DEPENDS_ON`, `DECIDED_BY`, `REFERENCES`, or `SUPERSEDES`.
- **Properties for attributes.** Store titles, statuses, file paths, and timestamps as node or edge properties.

## Known Limitations

These are measured, currently unfixed, and reported here rather than left to be discovered. Each is a limitation of what the product does today, not a description of how it is meant to work.

- **A statement cancelled by the time budget can cost gigabytes of memory, and the server has no exit to return it at.** Every mutation a statement has applied is retained until the rollback finishes — across four accumulators, of which the undo log is only about a fifth — and the only ceiling on how many mutations a statement applies is the engine's own cap on the rows one statement may produce, which the 5-second budget is far too short to reach: given a budget long enough to reach it, the same statement costs roughly **20 GB**. Measured: `MATCH (a),(b),(c) CREATE ()` over a 600-node store of 80 KB drove the process running it to **3.3 GB** of resident memory at the 5-second budget. The figure tracks the budget rather than the size of the graph. That cost now lands on a long-lived process: measured against `rmp graph serve`, the peak was 3618-3734 MB and the server still held 1064 MB — 58 times its baseline — 130 seconds later. The connection ceiling bounds how many such statements may run at once but not what each of them costs, and when the cost cannot be served the operating system's out-of-memory killer ends the server with `SIGKILL`, which writes nothing to stdout and nothing to stderr. The store on disk is byte-identical afterwards, so this is an availability defect and not a durability one.
- **A server's shutdown is not bounded, and the undo replay is the only cause of that left.** A statement the budget cut while it was writing is inside an undo replay that takes no cancellation, and the store cannot close until that call returns. The longest such hold measured is **35.6 seconds** — the largest measured and not a maximum, since the same shape over the same store measured 34.5 seconds on an earlier run — and no ceiling has been established. A client that had stopped reading its result was a second cause until the drain began closing such a socket, which took that shutdown from 60.0 seconds to 7.5; what remains of that cause is bounded by the 60-second connection timeout rather than unbounded. `SPEC/GRAPH.md § Server Shutdown and the Drain` is canonical for which sessions the drain reaches and for what bounds each. A supervisor that escalates `SIGTERM` to `SIGKILL` after a short grace period may therefore kill the server mid-replay; every acknowledged commit is still durable and the next open replays the log, but the shutdown checkpoint is lost.
- **A `SET` on a relationship bound by a `MERGE` that matched an existing relationship is silently discarded when the ordered node pair already carries a parallel relationship.** The precondition is narrow and all three parts are required: the relationship variable must be bound by a `MERGE` clause in the same statement, that `MERGE` must have **matched** rather than created, and the same ordered pair `(source, target)` must already carry another relationship **in the same direction**. When all three hold, the statement exits 0, reports success, and writes nothing; a following read shows the previous value. Measured: with `(a)-[:OTHER]->(b)` present, `MERGE (a)-[e:T]->(b) SET e = {c:2}` over an existing `T` leaves `c` at `1` while still reporting `{"ok": true, "counters": {"propertiesWritten": 1}}` — the same output the statement produces when the write does persist, so the counters do not expose the loss either. Remove any one of the three and the write persists — an isolated pair works, a parallel edge in the **reverse** direction does not trigger it, and a plain `MATCH ... SET` writes correctly with the parallel edge present. Bind the relationship with `MATCH` rather than `MERGE` when you intend to update one that already exists, or set the properties in a second statement after a fresh `MATCH`. A `SET` on a relationship bound by `CREATE`, or by a `MERGE` that creates, is **not** affected and was repaired by the move to GoGraph v0.14.0.
- **An undirected or incoming `SET` on a relationship does not write every relationship it matched, and how many it loses depends on the data.** A write persists only where the row's left-hand node is the relationship's stored source and its right-hand node the stored target, so the same statement may write everything it matched, some of it, or none of it. Re-measured for the 1.16.0 release on a single stored `(alice)-[:MENTORS]->(bob)`: the pattern anchored with `alice` on the left writes correctly, while the same pattern written with `bob` on the left writes nothing at all, exits 0, and leaves the property at its previous value. Measured on two relationships either side of one node, `MATCH (n)-[r:R]-(m {key:'b'}) RETURN count(r)` reports 2 while the same pattern with `SET r.stamp = 'x'` writes one of them and still reports success; with both relationships pointing away from the anchored node, none is written and the report is unchanged. Nothing in the output distinguishes a complete write from a partial one, and the counters do not either — that two-relationship statement reports `"propertiesWritten": 2` over a single persisted write, and the one anchored on the wrong endpoint reports `"propertiesWritten": 1` over none. **A selective statement is the hazardous one and an unanchored sweep is safe**, because each relationship is then emitted twice and one of the two rows is correctly oriented. Write through an outgoing pattern, which can be anchored on either endpoint. `DELETE` is unaffected and removes everything it matched.

## Aliases

The `graph` command has no alias, and neither `serve` nor `client` has one.

## Notes

- The graph is created by `rmp graph serve` and by nothing else. A read against a roadmap whose graph has just been created returns an empty result and is not an error; a roadmap that has never been served has no `graph/` directory at all, and `rmp graph client` against it fails rather than creating one.
- The graph store is a directory (`~/.roadmaps/<name>/graph/`, mode `0700`), not a single file, because GoGraph persists through an on-disk snapshot plus a write-ahead log. The server's socket sits beside it, at `~/.roadmaps/<name>/graph.sock`, and not inside it: the contents of `graph/` belong to the engine, and the socket exists only while a server runs — or after one was killed.
- Graph operations never read from or write to the roadmap's SQLite `project.db`, and removing a roadmap (`rmp roadmap remove <name>`) deletes the graph along with the rest of the roadmap home directory.
- **The server holds the store's advisory lock exclusively** for its whole lifetime, and no other process takes it. There is one lock mode because there is one execution path: Groadmap cannot know before running a statement whether it will write. Callers take no lock at all — the server holds it — and concurrency between them is resolved by the store's MVCC instead.
- A statement that changes the graph runs inside a single transaction and persists durably before its result is reported. It also reports what it changed, under the `counters` member described in [What a statement changed](#what-a-statement-changed-the-counters-member); a statement that changed nothing carries no such member.
- A statement whose transaction appended nothing to the write-ahead log leaves the store's `snapshot/` directory and its `wal` file exactly as it found them. The server's cadence checkpoint is the exception, since it is not gated on the statement you just ran.
- A schema statement is the one exception to that transaction: the server runs it through the engine's transactional entry point, and the engine recognises a schema statement there and executes it outside the transaction, because a schema change is not transactional in this engine. A schema statement that succeeds has taken effect and there is nothing to roll it back into. The next checkpoint's snapshot carries the registered schema.
- The engine may attach advisory notifications to a result — a Cartesian-product warning on a disconnected multi-pattern `MATCH`, for example. Each is written to stderr as one plain-text line and changes neither the stdout output nor the exit code.
- A routing Bolt driver is not a supported client. Asked for a routing table, the engine answers with the address its listener reports, which for a Unix domain socket is a filesystem path; a driver expecting a host and a port cannot parse it. `rmp graph client` connects to the socket directly and never asks.

## Output Format

Both subcommands follow these conventions:
- **Success**: JSON output to stdout, exit code 0
- **Errors**: Plain text to stderr, non-zero exit code

`rmp graph serve` writes its single startup object to stdout and nothing else for the life of the process. Everything else it emits goes to stderr: the two engine warnings at startup, which are not failures, and every record it writes while it runs.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | For `client`: the statement was sent to a server, ran, and its result was written. For `serve`: the server started, served, and was stopped by `SIGINT` or `SIGTERM` |
| 1 | For `client`: no server is listening; a server could not be reached through the socket; the connection was lost or went unanswered after the statement was sent; the statement failed to parse or execute in the engine, a refused schema statement included; the statement exhausted the 5-second time budget; every attempt of the retry policy lost a serialisation conflict; a value the server returned could not be mapped onto the published result shape; or the statement was to come from standard input and reading that stream failed. For `serve`: the graph store could not be created, opened or recovered; its lock could not be taken within the bounded wait; the socket could not be bound; or a live server already answers on the resolved socket. For both: a resolved socket path longer than the platform allows, whether derived from the roadmap or supplied through `--socket` |
| 2 | No statement supplied (`--query` absent and stdin empty, or `--query` empty/whitespace); or `--socket` supplied with an empty value; or an unknown flag or a positional argument was supplied |
| 3 | No roadmap selected (`-r` missing/required) |
| 4 | Roadmap not found (the roadmap given via `-r` does not exist) |
| 6 | The statement is longer than the maximum length of 1048576 bytes. `serve` cannot return this code, because it takes no statement |
| 127 | Unknown subcommand — which is what each of the six withdrawn names is |

Two of these are worth stating plainly, because a reader may expect otherwise.

**A failure to reach a server is `1`, not `4`.** The roadmap exists; what is missing is a process. Exit code 4 is reserved for a roadmap that does not exist, and reporting an unserved roadmap as a missing one would send a caller to `rmp roadmap create`.

**A graceful stop of `rmp graph serve` is exit code `0`, not `130`.** `SIGINT` is an instruction to stop rather than an interruption of unfinished work, so the server drains, checkpoints and exits successfully. `rmp web`, the only other long-lived command, behaves the same way. A signal that arrives before the server has announced its socket is the ordinary interruption instead, and exits `130`.

Two of the exit-1 causes have opposite remedies, which is why each carries a line of its own. A statement that exhausted the time budget is one to change: narrow it, or split it. A statement that lost every attempt of the retry policy is one to run again unchanged, and to spread across distinct nodes if the contention persists.
