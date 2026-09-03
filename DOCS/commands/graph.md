# graph

## Description

Operate a roadmap's knowledge graph: a free-form, queryable store of the project's elements and the relationships between them, backed by the GoGraph engine. The graph turns a roadmap into a "second brain" where an AI agent records and retrieves project elements (specs, code, decisions, dependencies) and how they connect, without re-reading every source file.

Each roadmap owns one graph, stored under that roadmap's home directory at `~/.roadmaps/<name>/graph/` (a directory, mode `0700`), created on first use of the `graph` command. The graph is free-form: Groadmap imposes no schema. It is independent of the roadmap's SQLite tasks and sprints data in this version.

The graph is reached through three subcommands. `execute` accepts any Cypher statement the engine accepts and runs it against the roadmap's graph; `serve` runs no statement of its own and instead holds that graph open, answering statements over a Unix domain socket until it is stopped; `client` sends a statement to a running server and prints what comes back. Groadmap does not examine a statement and refuses none on the ground of what it does.

**A running server is used automatically, and only the socket is a choice.** When a server is serving the selected roadmap, `execute` sends its statement to that server instead of opening the store, with no flag and no configuration; with nothing listening it opens the store directly, as it always did. The statement, the result, the output shape and the exit code are the same either way. `client` resolves the same socket and has no second path: with nothing listening it fails. All three subcommands take `--socket <path>` and default it identically; the flag names which socket is looked at and neither forces a server nor forbids one.

## Synopsis

```
rmp graph execute -r <roadmap> [--query <cypher>] [--socket <path>]
rmp graph serve -r <roadmap> [--socket <path>]
rmp graph client -r <roadmap> [--query <cypher>] [--socket <path>]
```

## Subcommands

### execute

Runs exactly one Cypher statement against the roadmap's knowledge graph and prints what it returns. A statement that changes the graph runs inside a single transaction and is persisted durably before the process exits — or, when a graph server is serving the roadmap, it is sent to that server and persisted there.

Every class of statement runs through this one subcommand:

- a **read**, such as `MATCH ... RETURN`, including variable-length traversals like `-[*1..3]-`;
- a **write**, such as `CREATE`, `MERGE`, `SET` or `REMOVE`;
- a **deletion**, such as `DELETE` or `DETACH DELETE`;
- **schema DDL** — `CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, `DROP CONSTRAINT`;
- **schema introspection** — `SHOW INDEXES`, `SHOW INDEX`, `SHOW CONSTRAINTS`, `SHOW CONSTRAINT`, each with an optional `YIELD` / `WHERE` / `RETURN` projection tail.

**Where the statement runs is resolved, not chosen.** The invocation resolves the socket in force — the path derived from the roadmap, or the value of `--socket` — before it opens anything. A server answering there takes the statement and the store is never opened locally; with nothing answering, the store is opened directly under its exclusive advisory lock, which is what every invocation did before a server existed. See [Running a Graph Server](#running-a-graph-server).

**The statement runs under a 5-second time budget** on both paths. A statement that exhausts it is cancelled, its transaction rolls back whole, nothing is written, and the command fails with exit code 1. The remedy is to narrow the statement — add a label, an indexed property filter, or a `LIMIT` — or to split it into smaller statements.

**Usage:** `rmp graph execute -r <roadmap> [--query <cypher>] [--socket <path>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-q` | `--query` | string | - | Cypher statement. When absent, the statement is read from standard input |
| | `--socket` | string | `~/.roadmaps/<name>/graph.sock` | Socket this invocation resolves. A server answering there takes the statement; an absent socket, or one that refuses the connection, sends the invocation to the store under the exclusive lock. Write it only when the server was started with the same flag |
| `-h` | `--help` | bool | false | Show subcommand help |

**Output:** `{"columns": [...], "rows": [[...], ...]}` when the statement produces result columns; `{"ok": true}` when it produces none. For a data statement the two cases are exactly "has a `RETURN` clause" and "has none". A schema-introspection command produces the listing and returns the `{columns, rows}` shape even though it carries no `RETURN` clause; a `CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT` or `DROP CONSTRAINT` produces no columns and returns `{"ok": true}`. The bytes are the same whichever path carried the statement, so a script may parse one shape and change nothing when a server is started or stopped.

**Examples:**
```bash
# Read: find which code implements each spec
rmp graph execute -r backend-platform \
  --query "MATCH (s:Spec)-[:IMPLEMENTED_BY]->(c:Code) RETURN s.key, c.path"

# Read the statement from standard input
echo "MATCH (n) RETURN count(n)" | rmp graph execute -r backend-platform

# Write: create a spec node linked to its implementation
rmp graph execute -r backend-platform \
  --query "MERGE (s:Spec {key:'user-authentication'}) MERGE (c:Code {path:'internal/auth/jwt.go'}) MERGE (s)-[:IMPLEMENTED_BY]->(c)"

# Write and return the created node
rmp graph execute -r backend-platform \
  --query "CREATE (s:Spec {key:'rate-limiting'}) RETURN s"

# Mutate an existing node
rmp graph execute -r backend-platform \
  --query "MATCH (s:Spec {key:'user-authentication'}) SET s.status = 'implemented'"

# Remove a decision node and all its relationships
rmp graph execute -r backend-platform \
  --query "MATCH (d:Decision {key:'use-sessions'}) DETACH DELETE d"

# Traverse a dependency chain
rmp graph execute -r backend-platform \
  --query "MATCH path = (s:Spec {key:'user-authentication'})-[:DEPENDS_ON*1..3]->(d:Dependency) RETURN path"

# Introspect the registered schema
rmp graph execute -r backend-platform --query "SHOW INDEXES"

# Reach a server that was started on a socket of its own
rmp graph execute -r backend-platform \
  --socket /run/user/1000/backend-platform-graph.sock --query "SHOW INDEXES"
```

### serve

Opens the roadmap's knowledge graph once, holds it and its exclusive advisory store lock for the life of the process, and answers Cypher statements over a Unix domain socket until it is stopped. The protocol is Bolt version 5, served by the graph engine's own server; Groadmap defines no protocol of its own and binds no network port, on loopback or anywhere else.

`serve` is **long-lived**. Unlike every other command except `rmp web`, it does not complete and exit: it runs until it receives `SIGINT` (`Ctrl+C`) or `SIGTERM`, then drains the work in flight, shuts the server down, checkpoints, releases the lock, removes its socket, and exits `0`.

**One server per roadmap.** The roadmap's store lock is the interlock: a second `rmp graph serve` against the same roadmap cannot take it, fails with exit code `1`, and leaves the first server's socket untouched. It does not queue. A server asked to bind a socket that some *other* roadmap's server already owns is refused by the socket probe instead, and again leaves the incumbent's socket alone.

`serve` runs no statement of its own, creates no graph directory that does not already exist, and never reads or writes a roadmap's `project.db`. It serves one roadmap; serving several means running several servers, one per roadmap, each on its own socket. It exposes exactly one database, under the engine's own default name, and a client selects nothing.

**Access control is the filesystem, and there is no other.** The socket is created with mode `0600`, set explicitly rather than left to the process umask, inside a roadmap home directory that is `0700`. The server authenticates nobody: any caller able to open the socket can read, write, delete and change the schema of that roadmap's graph. Connecting to a Unix domain socket needs write permission on the socket file, so "can open it" is the whole of the test. There is no login, no token, no session, and no transport security — the transport is a file in the local filesystem and there is no network hop to protect.

**Two warnings on stderr at startup are expected and are not failures.** The engine emits one for a server running without transport security and one for a server running without an authentication handler. Both are accurate. Both states are the intended ones, and the handler is set explicitly — the engine refuses to construct a server without one — so "no authentication" here is a declaration rather than an oversight.

**Usage:** `rmp graph serve -r <roadmap> [--socket <path>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required). The server serves this one roadmap's graph and no other |
| | `--socket` | string | `~/.roadmaps/<name>/graph.sock` | Unix domain socket to bind. A non-default path is followed by the CLI through the same flag and by nothing else: the web interface has no way to receive one. See [Serving on a non-default socket](#serving-on-a-non-default-socket) |
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

1. Resolve the roadmap and the socket path. A roadmap that does not exist fails here, before anything is opened, created or removed.
2. Take the graph store's exclusive advisory lock under the ordinary bounded wait. A server starting while a short-lived `rmp graph execute` holds the lock waits for it rather than failing at once. This is what refuses a second server against the same roadmap.
3. Refuse to start if a live server already answers on the resolved socket, leaving that socket exactly as it was found.
4. Remove a stale socket file — one a killed server left behind — now that nothing answers on it. This is what lets a relaunch after a kill succeed.
5. Bind the listener and set the socket's mode to `0600`.
6. Open the store and construct the engine.
7. Serve, and print the socket path on stdout.

**The listener is bound before the store is opened, deliberately.** Opening a large graph costs up to about a second, and a caller that resolved the roadmap during that second would find no socket, conclude the roadmap is not served, and walk into a lock this process is already holding. Binding first means such a caller connects and waits for the handshake instead. One narrow window remains, between taking the lock and binding the socket — a probe, an unlink and a bind, microseconds rather than the store open — in which a caller takes the direct path, waits out its budget and fails. The failure is loud, deterministic and cleared by retrying.

**Examples:**
```bash
# Serve a roadmap's graph on the default socket, until Ctrl+C
rmp graph serve -r backend-platform

# Serve on a socket of your own choosing
rmp graph serve -r backend-platform --socket /run/user/1000/backend-platform-graph.sock
```

### client

Sends exactly one Cypher statement to a running graph server over its Unix domain socket and prints the result. It reads and writes alike: the server does not examine the statement any more than `execute` does, so a statement that creates, changes, deletes or alters the schema is executed and committed. Every statement class listed under `execute` is accepted here unchanged.

**It requires a server.** `client` resolves `~/.roadmaps/<name>/graph.sock`, or the `--socket` path when one is given, and with nothing listening there it fails with exit code `1`. It does **not** open the store. That is the whole difference between this subcommand and `execute`, which resolves the same socket and has a second path to fall back on: a subcommand that quietly became `execute` when no server answered would report a success that says nothing about whether a server was reached.

**The output is `execute`'s output.** For the same statement against the same graph the bytes on stdout are byte for byte what `rmp graph execute` writes, so a caller may parse one shape and change nothing when a server is started or stopped.

**A serialisation conflict is retried, not reported.** Two clients writing to the same nodes at the same time is an ordinary situation inside a server, and the store detects the collision rather than preventing it. The losing statement committed nothing, so the client re-sends it under the project's retry policy and reports a failure only when that policy or the statement time budget is exhausted. Under sustained contention on one node the ladder can still run out; see [Known Limitations](#known-limitations).

**The statement runs under the same 5-second time budget** `execute` runs under. The server is the end that enforces it; the client keeps a later deadline of its own, 7.5 seconds, purely as a backstop against a server that answers nothing, so a statement that committed just before the budget expired is never reported as one that wrote nothing.

**Usage:** `rmp graph client -r <roadmap> [--query <cypher>] [--socket <path>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required). It selects the graph the statement runs against and, unless `--socket` overrides it, the socket the statement is sent to |
| `-q` | `--query` | string | - | Cypher statement. When absent, the statement is read from standard input |
| | `--socket` | string | `~/.roadmaps/<name>/graph.sock` | Unix domain socket of the server, the same derivation `serve` uses. Write it when the server was started with the same flag |
| `-h` | `--help` | bool | false | Show subcommand help |

**Output:** `{"columns": [...], "rows": [[...], ...]}` when the statement produces result columns; `{"ok": true}` when it produces none — the same shapes, and the same bytes, `execute` writes.

**Examples:**
```bash
# Read through a running server
rmp graph client -r backend-platform \
  --query "MATCH (s:Spec)-[:IMPLEMENTED_BY]->(c:Code) RETURN s.key, c.path"

# Write through a running server
rmp graph client -r backend-platform \
  --query "MERGE (s:Spec {key:'rate-limiting'}) RETURN s"

# Read the statement from standard input
echo "MATCH (n) RETURN count(n)" | rmp graph client -r backend-platform

# Reach a server started on a socket of its own
rmp graph client -r backend-platform \
  --socket /run/user/1000/backend-platform-graph.sock --query "SHOW INDEXES"
```

## Running a Graph Server

A server is optional. Everything the graph can do is reachable without one, and starting one changes throughput and latency rather than the contract: the same statements, the same output, the same exit codes.

### What a server buys, and what it costs

**What it buys is the store open.** Without a server, every `rmp graph execute` opens the store, replays the write-ahead log, runs one statement, checkpoints if it wrote, and closes. With a server, that cost is paid once for the life of the process and every statement after it is a message on a socket.

**It also buys concurrency, which no other configuration in the product has.** Without a server, two invocations against the same roadmap serialise on the store's exclusive advisory lock, even when neither of them writes, because Groadmap cannot know before running a statement whether it will write. Inside a server, transactions run concurrently and the store's MVCC resolves them: readers never block and are never blocked, and two writers run at the same time rather than one waiting for the other.

**Measured**, on an 8-core / 16-thread workstation against a store of the shape a real knowledge graph has: read throughput rises with concurrency to roughly seven to eight times the single-client rate and stops rising at about **16** concurrent clients. Past that knee another client buys under 5% more throughput while the 99th-percentile latency grows nearly fivefold, so 16 is where useful scaling ends on that machine. The ceiling the server actually enforces is 128 concurrent connections, set well above the knee on purpose: a connection refused for hitting the ceiling is dropped without a protocol answer and is not retried by the client, so the ceiling is placed where it cannot bind rather than where it would begin to.

**What it costs is exclusivity.** A server holds the roadmap's store lock for its whole lifetime. Nothing else can open that store directly while it runs, which is precisely why the resolution rule below exists.

### How a statement finds the server

Every surface that reaches a graph resolves the socket first: it connects, completes the protocol handshake, and decides on the answer. The probe carries a deadline of 2500 ms, is not retried, and is spent before any lock is taken.

| State | How it is recognised | `rmp graph execute` | `rmp graph client` | The web graph endpoint |
|-------|----------------------|---------------------|--------------------|------------------------|
| **Not served: no socket** | The socket path does not exist | Opens the store directly under the exclusive lock | Fails, exit code 1 | Opens the store directly |
| **Not served: nothing listening** | The connection is refused, which is what a socket file left behind by a killed server answers | Opens the store directly; the leftover file is neither an error nor removed | Fails, exit code 1 | Opens the store directly |
| **Served** | The connection is accepted and the handshake completes inside the probe deadline | Sends the statement to the server; takes no lock and opens no store | Sends the statement | Sends the statement |
| **Unreachable** | The connection is accepted but the handshake does not complete in time, or the connection fails for any other reason | Fails, exit code 1. It does **not** fall back to the store | Fails, exit code 1 | HTTP `500` |

Four consequences worth stating outright:

- **No flag selects a path.** `--socket` names which socket is looked at. It does not force a server, does not forbid one, and does not select the store.
- **A caller takes exactly one path, never both.** Resolution happens before any lock is taken, and a caller that reached a server and then failed does not retry against the store.
- **A connection lost after the statement was sent is a failure, and the statement's outcome is unknown.** A commit is durable before it is acknowledged, so a connection that dies between the two leaves nobody able to say whether the write happened. The invocation reports exactly that and does not re-run the statement, because re-running it could apply it twice. A caller that must know re-reads the graph, which is why a statement whose effect has to be confirmed is written with a `RETURN` clause or followed by a read.
- **A leftover socket file is never an error.** A killed server leaves one behind; the refusal a connection to it receives is the whole of the evidence needed to conclude that nothing is listening. The next `rmp graph serve` replaces it.

### Serving on a non-default socket

`--socket` moves the socket off the path every surface derives. **The command line can follow it; the web interface cannot.**

`graph serve`, `graph client` and `graph execute` all publish the flag and all default it identically, so a server on a custom path is reached by giving the same path to the subcommand that talks to it. Nothing is lost on that side.

The web interface's graph data endpoint has no command line, `rmp web` serves every roadmap at once rather than one, and no request parameter carries a socket path. It therefore resolves the derived path, finds nothing there, concludes the roadmap is not served, and takes the direct path — where it meets the lock the server is holding for its lifetime, waits out the bounded wait, and answers HTTP `500`. **That happens on every request for that roadmap's graph, deterministically, for as long as that server runs.** The message reports an unavailable store rather than a server on another path, because nothing in the product knows that a server is running elsewhere: the lock records no holder, and the request probed a path the server never bound.

So `--socket` is an option that keeps the CLI and costs the web page. Use it for a server the browser is not expected to reach — a test harness, a diagnostic session, a socket that has to live on another filesystem — and start a server whose roadmap is also browsed without it.

A mistyped path has the same shape with a quieter symptom: a path nothing answers on reads as "not served", so `rmp graph execute --socket /typo.sock` goes to the store rather than to the server it meant. Against an unserved roadmap it succeeds there and says nothing; against a roadmap whose server is running on the default socket it meets that server's lock and fails with `Error: database error: graph store is busy: another invocation still holds it after the bounded wait`.

### Concurrency inside a server

The store's only concurrency control is MVCC, and a server is the first place in this product where that is observable.

- **Readers never block and are never blocked.** A read transaction takes no lock and pins one committed snapshot for its life.
- **Writers do not exclude one another.** Beginning a transaction acquires nothing. Two write transactions against the same roadmap run at the same time.
- **A write-write collision is detected rather than prevented.** The first updater wins; the loser's transaction fails with a retriable serialisation conflict.
- **That conflict is a normal outcome, and it is retried rather than reported.** The losing transaction committed nothing, so re-running its statement runs it against a graph that never saw it. A failure is reported only once the retry policy or the caller's own deadline is exhausted. Sustained contention on one node can exhaust it; see [Known Limitations](#known-limitations).

An explicit `BEGIN` to `COMMIT` sequence has the same **5 seconds in total** that a single statement has, however many statements it carries, because the maximum statement timeout clamps a transaction's whole life. A session that sends nothing is closed after 60 seconds; `rmp graph client` sends one statement per invocation and never meets this, but a longer-lived Bolt client must expect to reconnect.

### Durability, checkpoints and shutdown

**Durability does not weaken because the process is long-lived.** Every operation of a transaction is appended to the write-ahead log, then a commit marker, then one synchronisation to disk, and only then is the transaction applied in memory and acknowledged. A client reading a successful commit is reading one that is already on disk, and a crash recovers all of a transaction or none of it. That holds against a kill exactly as it holds against a signal.

**A server does not checkpoint per write.** A short-lived `rmp graph execute` checkpoints after any transaction that appended to the log because it is about to exit and has no later opportunity; a server has later opportunities, and a full snapshot after every write would make every write cost the whole live graph. It checkpoints instead on an age-based cadence while it runs — a snapshot is owed once it is five minutes old, and the loop looks every 75 seconds — and again at shutdown, after the drain and before the lock is released, so the log the next open replays is short. The cadence is provisional, chosen by analogy and left where measurement found no reason to move it; it is not part of any published contract. There is no size trigger and no operation-count trigger, so a burst of writes inside one window grows the log without limit and the next open replays all of it.

**Shutdown drains, and the drain is Groadmap's own** because the engine's shutdown cuts sessions rather than draining them. On `SIGINT` or `SIGTERM` the server stops accepting connections, waits under a bounded timeout (7.5 seconds) for the work in flight to reach a quiescent point, shuts the Bolt server down, checkpoints, truncates the log, closes the store, releases the lock, removes the socket, and exits 0.

What the drain guarantees, and what it does not:

- **Every acknowledged commit is durable.** This is the commit protocol's doing rather than the drain's, and it holds against an unexpected kill too.
- **A statement in flight is either completed and answered, or cut whole.** A cut statement's transaction rolls back entirely and leaves no partial write.
- **It does not guarantee completion.** The bound is finite; past it the remaining sessions are cut, and a cut client sees a broken connection rather than a typed failure. The store is consistent either way.
- **It does not tell a client whose connection was cut between the commit and its acknowledgement whether the statement committed.** Nothing closes that window.
- **It does not bound the shutdown.** See [Known Limitations](#known-limitations).

### Socket failure lines

Seven failures belong to the socket rather than to the roadmap, the statement or the store. Each carries exit code 1. `<socket>` is the resolved socket path; `<detail>` is the operating system's own diagnostic.

| Condition | Subcommand | Line |
|-----------|-----------|------|
| A live server already answers on the socket `serve` resolved | `serve` | `Error: database error: a graph server is already serving <socket>` |
| The socket could not be bound | `serve` | `Error: database error: cannot bind <socket>: <detail>` |
| The store lock could not be taken within the bounded wait | `serve` | `Error: database error: cannot take the graph store lock for roadmap "X": another rmp graph serve may already be running for it` |
| No server is listening on the resolved socket | `client` | `Error: database error: no graph server is listening on <socket>` |
| The socket answered but no server could be reached through it | `execute`, `client` | `Error: database error: graph server unreachable at <socket>: <detail>` |
| The connection was lost after the statement had been sent | `execute`, `client` | `Error: database error: the connection to the graph server at <socket> was lost; the statement's outcome is unknown` |
| The server did not answer within the caller's backstop deadline | `execute`, `client` | `Error: database error: the graph server at <socket> did not answer within 7.5s; the statement's outcome is unknown` |

The lock line says "may" deliberately: the lock records no holder, so the invocation reports the overwhelmingly likely cause without asserting it. The last two lines say the outcome is *unknown* rather than that nothing was written, because a commit is durable before it is acknowledged and a line claiming nothing was written would be false in exactly the case a caller most needs the truth.

## The Withdrawn Subcommand Names

`create`, `query`, `update`, `delete` and `search` were subcommands of `rmp graph`. They are not any more, and `execute`, `serve` and `client` are the whole of the family. Each withdrawn name is an unresolved subcommand name and is answered as a dispatch failure: exit code `127`, the `graph` help on stderr, nothing on stdout. They are named here because an agent that has one of them in memory needs to be told that it will not resolve.

They existed to enforce an operation class, and that enforcement has been withdrawn. Nothing distinguished the five once it was gone, so they were replaced rather than kept as five names for one behaviour.

**`execute` runs what it is given, and the caller owns what that does.** No subcommand's contract says that a statement cannot delete. A statement's effect is decided by its Cypher and by nothing `rmp` inspects, so the guarantee you need about a statement is a guarantee about the text you supply. Between reading the statement and running it, Groadmap checks its length and nothing else about its content. The same is true of `client`, and of the web interface's query bar.

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

Every statement in this section runs through `client` as well, unchanged, when a server is what you are talking to.

**Creating an index or a constraint**

```bash
# Named index on one node property
rmp graph execute -r backend-platform \
  --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"

# Unnamed: the engine derives the name from the lowercased label, the lowercased
# property and the index kind, joined by underscores. This one registers as
# spec_title_hash
rmp graph execute -r backend-platform \
  --query "CREATE INDEX FOR (n:Spec) ON (n.title)"

# IF NOT EXISTS makes a create whose object already exists a silent no-op
rmp graph execute -r backend-platform \
  --query "CREATE INDEX spec_key IF NOT EXISTS FOR (n:Spec) ON (n.key)"

# An index is a hash index by default; a comparison-ordered index is requested
# through the statement's OPTIONS map
rmp graph execute -r backend-platform \
  --query "CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}"

# A uniqueness constraint, and a presence constraint
rmp graph execute -r backend-platform \
  --query "CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE"
rmp graph execute -r backend-platform \
  --query "CREATE CONSTRAINT spec_title_req IF NOT EXISTS FOR (n:Spec) REQUIRE n.title IS NOT NULL"
```

Each of these statements produces no result columns, so on success it outputs `{"ok": true}` and exits 0.

A `CREATE INDEX` back-fills the new index from the data already in the graph. A `CREATE CONSTRAINT` validates the data already in the graph first and registers the constraint only if it passes; a uniqueness rule over a property that already holds a repeated value, or a presence rule over a property some node lacks, is refused with exit code 1 and nothing is registered.

Index kinds are the engine's own vocabulary — `hash` by default, `btree` through `OPTIONS` — and not the index kinds of another Cypher implementation. A statement written against another implementation's vocabulary is refused by the engine.

**Dropping an index or a constraint**

```bash
# Removal is by name only, never by a label-and-property pair
rmp graph execute -r backend-platform --query "DROP INDEX spec_key"
rmp graph execute -r backend-platform --query "DROP CONSTRAINT spec_key_uq"

# IF EXISTS makes a drop of an absent object a silent no-op
rmp graph execute -r backend-platform --query "DROP INDEX spec_key IF EXISTS"
```

Because removal is by name only, a caller who did not declare a name must first learn the derived one from a listing. Declaring a name is the recommended practice and Groadmap does not enforce it: a named object is dropped by the name its author wrote, while an unnamed one is dropped by a name the engine chose, which changes if the index kind changes.

**Listing the schema**

```bash
rmp graph execute -r backend-platform --query "SHOW INDEXES"
rmp graph execute -r backend-platform --query "SHOW CONSTRAINTS"

# The singular aliases are the same commands
rmp graph execute -r backend-platform --query "SHOW INDEX"
rmp graph execute -r backend-platform --query "SHOW CONSTRAINT"

# With a projection tail
rmp graph execute -r backend-platform --query "SHOW INDEXES YIELD name, type"
```

`SHOW INDEXES` and `SHOW CONSTRAINTS` are the authoritative report of what a schema object is called, and are how you learn a derived name. A listing is ordered deterministically, so two invocations against an unchanged graph produce the same rows in the same order.

**Altering and recreating: two invocations, and not atomic**

The engine has no statement that changes an index in place. There is no `ALTER INDEX`, no `REBUILD INDEX`, and no `CREATE OR REPLACE INDEX`, and each of the three is refused by the parser as an unrecognised statement. Changing an index — its kind, or its definition — and rebuilding one are therefore a `DROP` followed by a `CREATE`, issued as **two separate invocations**; Groadmap composes nothing on your behalf:

```bash
rmp graph execute -r backend-platform --query "DROP INDEX spec_ord"
rmp graph execute -r backend-platform \
  --query "CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}"
```

Altering is a drop followed by a create with a different definition; recreating is a drop followed by a create with the identical definition, and the rebuild is the back-fill the create performs.

**The pair is not atomic.** The two invocations are two processes, and nothing spans them. If the second fails — a rejected definition, a lock it cannot take, a machine that stops between the two — the index is **dropped and not recreated**, and the graph is left with no index where it had one. Nothing in Groadmap detects that state, reports it, or repairs it; you learn of it from `SHOW INDEXES` and repair it by issuing the create again. Queries stay correct throughout, because an index is an access path and never a source of results, so what is lost is speed rather than answers.

Both halves cost time proportional to the graph: a create back-fills the index from every node carrying the label, and a drop discards that work. On a roadmap knowledge graph this is small, and it does not stay small if the graph grows.

**One statement per invocation**

An invocation carries exactly one statement, and nothing enforces that.

The engine's schema parser stops as soon as its grammar is satisfied and **discards the rest of the statement silently** — without an error, without a notification, and without any other trace. Handed

```
CREATE INDEX ix FOR (n:Spec) ON (n.key) MATCH (m) SET m.p = true
```

the engine creates the index, drops the `MATCH ... SET` on the floor, and returns success, so `graph execute` prints `{"ok": true}` and exits 0 for a statement half of which never ran — and you have no reason to check, because the command reported that it worked. Issue the two halves as two invocations.

A schema-introspection command carrying a further clause is refused by the engine, which names the unsupported clause rather than discarding it. And a statement that *begins* with a data-writing clause and carries schema text after it is not a schema statement at all: the engine routes it to the general Cypher grammar, which refuses it as a parse error (exit code 1).

**Schema failure classes and their exit codes**

A schema statement is refused by the **engine**, and the refusal carries the engine's own diagnostic text after the wording Groadmap fixes, with exit code **1**.

| Failure | Refused by | Exit code |
|---------|-----------|-----------|
| `CREATE INDEX` or `CREATE CONSTRAINT` whose object already exists, without `IF NOT EXISTS` | Engine | 1 |
| `DROP INDEX` or `DROP CONSTRAINT` naming an object that does not exist, without `IF EXISTS` | Engine | 1 |
| A definition the engine does not support — composite, over a relationship property, or a constraint kind it does not implement | Engine | 1 |
| `CREATE CONSTRAINT` that the data already in the graph does not satisfy | Engine | 1 |
| A `SHOW INDEX(ES)` / `SHOW CONSTRAINT(S)`, or a DDL form, whose keyword spacing routes it to the general Cypher grammar | Engine | 1 |

A duplicate create and a drop of an absent object are **engine** failures rather than validation failures, and they exit **1** rather than 6. This is stated explicitly because the exit code is the opposite of what a reader may expect: both look like input errors, and neither is one. Groadmap cannot know whether an object exists without opening the store, so the check belongs where the knowledge is. A caller who wants either to be a no-op writes `IF NOT EXISTS` or `IF EXISTS`.

```bash
# Exit 1: the engine refuses the second create, because the object exists
rmp graph execute -r backend-platform --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"
rmp graph execute -r backend-platform --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"
# Error: database error: graph query failed: <engine diagnostic>
```

A failed schema statement leaves the schema as it was. No partial registration exists in any of these classes: the object is either registered or it is not.

## Query Input Source and Precedence

`execute` and `client` obtain their Cypher from one of two sources, under identical rules:

1. When `--query` is present and non-empty, its value is used and standard input is not read.
2. When `--query` is absent, the statement is read from standard input under a bound; the read is not a read to EOF (for example `cat query.cypher | rmp graph execute -r backend-platform`).
3. When `--query` is absent and standard input supplies no statement — it is a terminal, it is already at end of stream, or everything it carries is whitespace — the command fails with exit code 2 (no query supplied). A terminal is refused without being read at all.
4. When `--query` is present but its value is empty, whitespace only, or absent, the command fails with exit code 2. A following token that begins with `--`, or with a single `-` followed by an ASCII letter, is the next flag and is never swallowed as the query; a `-` followed by a digit or a decimal point is a legitimate query value.
5. Leading and trailing whitespace is trimmed before execution, after the length check.

A statement longer than 1048576 bytes (1 MiB) is refused with exit code 6, whichever source carried it. That is the only cause of exit code 6 these subcommands have.

`serve` takes no statement at all and therefore reads neither source.

Neither `execute` nor `client` accepts a **positional argument**. A bare Cypher statement written on the command line is an excess positional argument and is refused with exit code 2 and the line:

```
Error: invalid input: unexpected argument "X" (graph queries use --query or stdin)
```

The refusal precedes the socket resolution, the store open, the standard-input read, and the maximum-length check, so a refused invocation does nothing at all. `serve` accepts no positional argument either, and refuses one with exit code 2.

Supplying `--socket` with an empty value is a missing parameter on all three subcommands: exit code 2, `Error: required parameter missing: --socket`.

## Modelling Conventions

The graph is free-form, but it tends toward a multi-layer model (specification, code, decisions, dependencies). These are recommendations only; Groadmap does not enforce or auto-create any schema:

- **Layer as a label.** Tag each node with a label naming its layer, such as `Spec`, `Code`, `Decision`, `Dependency`, or `Requirement`.
- **Identity as a property.** Give each node a stable identifier property (for example `key` or `path`) so you can `MERGE` on it without creating duplicates.
- **Cross-layer relationships as typed edges.** Use verb-like edge types such as `IMPLEMENTS`, `DEPENDS_ON`, `DECIDED_BY`, `REFERENCES`, or `SUPERSEDES`.
- **Properties for attributes.** Store titles, statuses, file paths, and timestamps as node or edge properties.

## Known Limitations

These are measured, currently unfixed, and reported here rather than left to be discovered. Each is a limitation of what the product does today, not a description of how it is meant to work.

- **A statement cancelled by the time budget can cost gigabytes of memory.** Every mutation a statement has applied is retained until the rollback finishes — across four accumulators, of which the undo log is only about a fifth — and nothing bounds how many mutations a statement applies before the budget cuts it. Measured: `MATCH (a),(b),(c) CREATE ()` over a 600-node store of 1.3 MB drove a single `rmp graph execute` process to **3.3 GB** of resident memory at the 5-second budget. The figure tracks the budget rather than the size of the graph. A short-lived invocation returns that memory to the operating system by exiting; `rmp graph serve` and `rmp web` have no exit to return it at, and the connection ceiling bounds how many such statements may run at once but not what each of them costs.
- **A server's shutdown is not bounded.** A statement the budget cut while it was writing is inside an undo replay that takes no cancellation, and the store cannot close until that call returns. The longest such hold measured is **35.6 seconds** — the largest measured and not a maximum, since the same shape over the same store measured 34.5 seconds on an earlier run — and no ceiling has been established. A supervisor that escalates `SIGTERM` to `SIGKILL` after a short grace period may therefore kill the server mid-replay; every acknowledged commit is still durable and the next open replays the log, but the shutdown checkpoint is lost.
- **A `SET` on a relationship bound by `CREATE` or `MERGE` in the same statement is silently discarded.** `CREATE (a)-[e:R]->(b) SET e.stamp = 'x' RETURN e.stamp` creates the relationship, returns `null`, writes no property, and exits 0; `MERGE` behaves the same, whether it creates the relationship or matches one that already existed. Binding origin is the only thing that matters: a `WITH` or a `FOREACH` between the two clauses does not rescue the write, and `SET e = {...}` is worse still, because its `RETURN` echoes the value it did not write. The same shape on a **node** is correct. Use `ON CREATE SET` or `ON MATCH SET`, or inline the properties in the pattern, or set them in a second statement after a fresh `MATCH`.
- **An undirected or incoming `SET` on a relationship does not write every relationship it matched, and how many it loses depends on the data.** A write persists only where the row's left-hand node is the relationship's stored source and its right-hand node the stored target, so the same statement may write everything it matched, some of it, or none of it. Measured on two relationships either side of one node, `MATCH (n)-[r:R]-(m {key:'b'}) RETURN count(r)` reports 2 while the same pattern with `SET r.stamp = 'x'` writes one of them and still reports `{"ok": true}`; with both relationships pointing away from the anchored node, none is written and the report is unchanged. Nothing in the output distinguishes a complete write from a partial one. **A selective statement is the hazardous one and an unanchored sweep is safe**, because each relationship is then emitted twice and one of the two rows is correctly oriented. Write through an outgoing pattern, which can be anchored on either endpoint. `DELETE` is unaffected and removes everything it matched.
- **Sustained write contention on one node surfaces the raw conflict to the caller.** Under 16 concurrent writers to a **single** node, about **1%** of statements exhaust the client's retry ladder and fail with the engine's own transient-conflict diagnostic. The statement was correct and the store healthy, and nothing in the message distinguishes "you hit contention, run it again" from "your statement is wrong" — so a caller that re-runs it must first be sure the statement is safe to run twice. Several agents stamping provenance on one shared node is exactly the shape that produces this.

## Aliases

The `graph` command has no alias, and neither `execute`, `serve` nor `client` has one.

## Notes

- The graph is created on first use of `rmp graph execute`, including by a statement that only reads; a read against a roadmap with no graph yet returns an empty result and is not an error. `rmp graph serve` creates no graph directory that does not already exist.
- The graph store is a directory (`~/.roadmaps/<name>/graph/`, mode `0700`), not a single file, because GoGraph persists through an on-disk snapshot plus a write-ahead log. The server's socket sits beside it, at `~/.roadmaps/<name>/graph.sock`, and not inside it: the contents of `graph/` belong to the engine.
- Graph operations never read from or write to the roadmap's SQLite `project.db`, and removing a roadmap (`rmp roadmap remove <name>`) deletes the graph along with the rest of the roadmap home directory.
- **On the direct path**, every invocation takes the store's advisory lock **exclusively**, and holds it across the whole open, execution, commit, checkpoint and write-ahead-log truncation sequence. There is one lock mode because there is one execution path: Groadmap cannot know before running a statement whether it will write. Two invocations against the same roadmap therefore serialise even when neither of them writes; an invocation that finds the lock held waits, under a bounded backoff, and fails with exit code 1 only once that wait is exhausted. **On the served path no lock is taken at all** — the server holds it — and concurrency is resolved by the store's MVCC instead.
- A statement that changes the graph runs inside a single transaction and persists durably before its result is reported. The engine reports no affected-element count, so write results carry no such field.
- A statement whose transaction appended nothing to the write-ahead log neither snapshots nor truncates the log; the store's `snapshot/` directory and its `wal` file are left exactly as the statement found them. A server on its cadence is the exception, since its checkpoint is not gated on the statement you just ran.
- A schema statement is the one exception to that transaction: the invocation still takes the exclusive lock and runs the statement through the engine's transactional entry point, but the engine recognises a schema statement there and executes it outside the transaction, because a schema change is not transactional in this engine. A schema statement that succeeds has taken effect and there is nothing to roll it back into. It checkpoints like any other successful write, and the snapshot carries the registered schema.
- The engine may attach advisory notifications to a result — a Cartesian-product warning on a disconnected multi-pattern `MATCH`, for example. Each is written to stderr as one plain-text line and changes neither the stdout output nor the exit code. `client` surfaces the notifications the server returns in exactly the same way.
- A routing Bolt driver is not a supported client. Asked for a routing table, the engine answers with the address its listener reports, which for a Unix domain socket is a filesystem path; a driver expecting a host and a port cannot parse it. `rmp graph client` connects to the socket directly and never asks.

## Output Format

All three subcommands follow these conventions:
- **Success**: JSON output to stdout, exit code 0
- **Errors**: Plain text to stderr, non-zero exit code

`rmp graph serve` writes its single startup object to stdout and nothing else for the life of the process; the two engine warnings it prints at startup go to stderr and are not failures.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | The statement executed successfully. For `serve`: the server started, served, and was stopped by `SIGINT` or `SIGTERM` |
| 1 | Cypher failed to parse or execute, the engine refused a schema statement, the statement exhausted the 5-second time budget, or the graph store could not be opened, read, or written. Also every socket failure: for `client`, no server listening; for `execute` and `client`, a socket that answers but yields no reachable server, and a connection lost or unanswered after the statement was sent; for `serve`, a lock it could not take, a socket it could not bind, and a live server already answering there |
| 2 | No statement supplied (`--query` absent and stdin empty, or `--query` empty/whitespace); or `--socket` supplied with an empty value; or an unknown flag or a positional argument was supplied |
| 3 | No roadmap selected (`-r` missing/required) |
| 4 | Roadmap not found (the roadmap given via `-r` does not exist) |
| 6 | The statement is longer than the maximum length of 1048576 bytes. `serve` cannot return this code, because it takes no statement |
| 127 | Unknown subcommand |

A socket file with nothing listening behind it is not a failure for `execute`: the refused connection is read as evidence that the roadmap is not served, the store is opened directly, and the invocation exits 0. For `client` the same state is exit code 1, because it has no second path.

A graceful stop of `rmp graph serve` is exit code `0` and not `130`: `SIGINT` is an instruction to stop rather than an interruption of unfinished work, and the server drains, checkpoints and exits successfully. `rmp web`, the only other long-lived command, behaves the same way.
