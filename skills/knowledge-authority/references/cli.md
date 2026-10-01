# `rmp graph` — CLI contract

Everything this skill may touch on the `rmp` CLI. All other `rmp` commands are
**out of scope**: `task`, `sprint`, `backlog`, `stats`, `audit` and `roadmap`
belong to the `roadmap-manager` skill, and `web` to neither skill.

Baseline: **Groadmap v1.17.3**. The binary's own machine-readable contract is
`rmp --ai-help`; run it whenever this reference and observed behaviour disagree —
**the binary is the source of truth**. Setting `AI_AGENT=1` makes every invocation
print a one-line hint to stderr pointing at `--ai-help`; every failure appends the
same hint whatever `AI_AGENT` holds, so read the `Error:` line by pattern
(`grep '^Error:'`), never by position.

## The shape of the thing

```
rmp graph serve  -r <roadmap> [--socket <path>]
rmp graph client -r <roadmap> [-q <cypher>] [--socket <path>]
```

**Two subcommands, and the graph is reached through nothing else.** `serve` is the
only process that opens the store; `client` is the only way to run a statement.
With nothing listening, `client` fails and opens nothing — there is no fallback
path into the store.

> **The six old subcommands are gone.** `create`, `query`, `update`, `delete`,
> `search` and `execute` were subcommands of `rmp graph`; each is now an
> unresolved subcommand and exits **127**. Any script or document still invoking
> them is broken, silently in the sense that 127 is easy to mistake for a missing
> binary.

### Flags

| Flag | Applies to | Notes |
|---|---|---|
| `-r`, `--roadmap <name>` | both | **Required.** `^[a-z0-9_-]+$`, ≤ 50 chars. Missing → exit 3; unknown → exit 4. |
| `-q`, `--query <cypher>` | `client` | The statement. **When absent it is read from stdin.** Neither → exit 2. |
| `--socket <path>` | both | The path `serve` binds and `client` connects to. Default `~/.roadmaps/<name>/graph.sock` on both, so a server started without it is reached without it. An empty value → exit 2. |
| *(any flag twice)* | both | No flag is repeatable, under any spelling → exit 2. |
| `-h`, `--help` | both | Per-subcommand help. |

Resolve `roadmap` and `socket` from the project's `.rmp` — see SKILL.md
§ Configuration.

## `serve`

Opens the graph once, holds it and its **exclusive advisory store lock** for the
life of the process, and answers statements over a Unix domain socket. The
protocol is **Bolt version 5**, served by the graph engine's own server; rmp
defines no protocol of its own.

- **Long-lived.** It does not complete and exit. It runs until SIGINT (Ctrl+C) or
  SIGTERM, then drains work in flight, shuts down, **checkpoints**, releases the
  lock, removes its socket, and exits 0.
- **Startup output:** `{"socket": "<absolute path>"}`, written once when bound.
  That line is the signal that clients may proceed. Per-statement results go to
  the client that asked, never to this stdout.
- **Creating a graph is a side effect of serving one.** Against a roadmap that has
  never had a graph, `serve` creates `~/.roadmaps/<name>/graph/` (mode 0700) and
  serves it empty. A `serve` refused for any other reason creates nothing.
- **One server per roadmap.** A second `serve` on the same roadmap cannot take the
  store lock, fails **exit 1**, and leaves the first server's socket untouched.
  It does **not** queue.
- **It runs no statement of its own** and never touches the roadmap's SQLite
  `project.db`.
- **Security is the filesystem and nothing else.** The socket is mode 0600 (set
  explicitly, not left to umask) inside a 0700 directory; the server
  authenticates nobody. Anyone who can open the socket can read, write, delete
  and re-schema that graph. Startup logs two warnings — `NoAuthHandler` and no
  TLS — which are expected for a local socket, not faults.
- **No network.** A Unix domain socket only; no port is bound, on loopback or
  anywhere.
- **The web interface cannot be given a socket.** It resolves the derived path,
  so a server on a custom `--socket` leaves it unable to reach the graph — and it
  will also fail against the running server's lock. (The `web` command itself is
  outside this skill's scope.)
- **The roadmap cannot be removed while its server runs** (the removal is
  refused with exit 6), which is one more reason to stop a server you started.

### Socket path length

The path must fit the platform's `sockaddr_un`. Exceeding it is exit 1 with a
message that names the limit:

```
Error: graph server error: socket path is too long: /private/tmp/…/kgprobe.sock
is 120 bytes and this platform allows at most 103. Use --socket to name a
shorter path.
```

**Measured on macOS: 103 bytes. Measured on Linux at 1.17.3: 99 bytes for
`serve` and 107 for `client`** — the two ends need not agree, so a path that a
client accepts can still be too long for the server. Believe the message over any
remembered number.

## `client`

Sends **exactly one** Cypher statement and prints what comes back.

- **It reads and writes alike, and nothing examines the statement.** A statement
  that creates, changes, deletes or alters the schema is executed and committed.
  There is no operation-class check and no read-only subcommand.
- The statement comes from `--query`, or from **stdin** when that flag is absent.
  Prefer stdin for multi-line statements or ones with shell metacharacters.
- **It requires a server.** It resolves the socket and, with nothing listening,
  exits **1** (`no graph server is listening on <socket>`), writes nothing to
  stdout, and leaves the graph directory byte-identical. A stale socket file from
  a killed server is the same condition as no socket.
- **A retriable serialisation conflict is not an error and is not printed.** Two
  clients writing the same nodes at once is ordinary; the losing statement
  committed nothing and is re-sent under the retry policy. A failure is reported
  only once that policy or the time budget is exhausted.
- **A 5-second statement time budget** is enforced by the server. A statement
  cancelled for exceeding it **writes nothing** and exits 1 — narrow it, or split
  it. The client keeps a later deadline purely as a backstop.
- **Maximum statement length 1048576 bytes** → exit 6. Exit 6 has exactly one
  other cause: a `-r` value that breaks the roadmap-name character rule, which
  either subcommand refuses with exit 6 before it does anything else.

## Output (stdout JSON)

| Case | Shape |
|---|---|
| Produces result columns | `{"columns":[…], "rows":[[…],…]}` |
| Produces none | `{"ok": true}` |
| `EXPLAIN` prefix | adds `"plan": <plan node>`; **always** the columns shape |
| `PROFILE` prefix | adds `"profile": <plan node>`; never both plan and profile |
| Changed the graph | adds `"counters": {…}` |

Errors go to **stderr as plain text**, not JSON. Parse stdout for results; read
stderr on non-zero exit.

### The `counters` block

Present **only when the statement changed the graph**; a zero counter is left
out, and a statement that changed nothing — every read, a `MERGE` that matched, a
`DELETE` matching no row — carries **no `counters` key at all**.

`nodesCreated`, `nodesDeleted`, `relationshipsCreated`, `relationshipsDeleted`,
`propertiesWritten`, `labelsAdded`, `labelsRemoved`, `indexesAdded`,
`indexesRemoved`, `constraintsAdded`, `constraintsRemoved`.

**This block is the skill's write oracle.** `nodesCreated > 0` on a `MERGE` that
should have matched an existing node is the pattern-`MERGE` duplication trap
firing — see SKILL.md. Read it on every write.

`DROP INDEX … IF EXISTS` and `DROP CONSTRAINT … IF EXISTS` on an object that does
not exist return `{"ok":true}` with no counters (measured at 1.17.3).

### `EXPLAIN` / `PROFILE`

`EXPLAIN` runs nothing and reports the plan the engine would run; `PROFILE` runs
the statement and reports what the run measured. Plan nodes carry `operator`,
optional `detail`, `estimatedRows` and `estimatedRowsSource`; a profile adds
`rows`, `timeNs` and `dbHits` per operator.

```json
{"operator":"Project","children":[
  {"operator":"NodeByIndexSeek","detail":"seek=\"k7\"","rows":0,"timeNs":41,"dbHits":0}]}
```

The operators that decide an index question: **`NodeByLabelScan` + `Filter`** (no
usable index) versus **`NodeByIndexSeek`** (index in use).

## Schema DDL — the measured support matrix

`client` runs schema statements and listings like any other. What the engine
actually accepts was probed directly at 1.17.3; **every unsupported form fails
with exit 1**, most of them with a message naming what is not supported.

### Indexes

| Form | Result |
|---|---|
| `CREATE INDEX <n> FOR (x:L) ON (x.p)` | **supported** → `indexesAdded: 1` |
| `CREATE INDEX <n> IF NOT EXISTS FOR …` | supported; no-op returns `{"ok":true}` with no counters |
| `DROP INDEX <n>` / `DROP INDEX <n> IF EXISTS` | supported (but see the lying counter above) |
| `SHOW INDEXES` | supported → columns `name, state, type, entityType, labelsOrTypes, properties` |
| `… OPTIONS {indexType:'btree'}` | accepted → `type: btree`, but the planner used it for neither equality nor range (label scan) |
| composite `ON (x.a, x.b)` | **rejected**, exit 1 — "composite indexes (multiple properties) are not supported" |
| `CREATE TEXT\|RANGE\|POINT\|FULLTEXT\|LOOKUP INDEX …` | **parse error**, exit 1 — not in the grammar |
| relationship property `FOR ()-[r:R]-() ON (r.p)` | **parse error**, exit 1 — the DDL parser expects a node pattern |

**Every usable index is `type: hash`, node-only, single-property — therefore
equality only.** Measured: with an index on `Seed.key`, `MATCH (n:Seed {key:'k7'})` plans
`NodeByIndexSeek`, while `WHERE n.key > 'k1'` plans `NodeByLabelScan` + a filter.
**A range predicate does not use the index.**

### Constraints

| Form | Result |
|---|---|
| `CREATE CONSTRAINT <n> FOR (x:L) REQUIRE x.p IS UNIQUE` | **supported** → type `UNIQUE` |
| `CREATE CONSTRAINT <n> FOR (x:L) REQUIRE x.p IS NOT NULL` | **supported** → type `NOT_NULL` |
| `… IF NOT EXISTS` | supported; no-op returns `{"ok":true}` |
| `DROP CONSTRAINT <n> [IF EXISTS]` | supported |
| `SHOW CONSTRAINTS` | supported → columns `name, type, entityType, labelsOrTypes, properties` |
| composite `REQUIRE (x.a, x.b) IS NODE KEY` | **rejected**, exit 1 — "composite constraints (multiple properties) are not supported" |
| `ASSERT exists(x.p)` | **parse error**, exit 1 — `expected "REQUIRE"` |
| type constraint `REQUIRE x.p IS :: STRING` | **rejected**, exit 1 — "property type constraints (IS :: <TYPE>) are not supported" |

**UNIQUE is enforced.** After creating it, a duplicate insert is rejected:

```
Error: graph engine error: … constraint violation: UNIQUE constraint on
(Probe).key: value "a" already exists
```

A UNIQUE constraint also creates a **backing index** that appears in
`SHOW INDEXES` as `__uniq__<Label>.<prop>` — with **empty** `labelsOrTypes` and
`properties`, a reporting quirk. Do not mistake it for a stray index and drop it.

### Schema failure classes

- A duplicate `CREATE`, a `DROP` of something absent, an unsupported definition,
  and a `CREATE CONSTRAINT` the existing data violates all exit **1**, not 6 —
  whether an object exists is knowable only inside the graph. Write
  `IF NOT EXISTS` / `IF EXISTS` to make it a silent no-op instead.
- **`SHOW` needs exactly one space.** `SHOW  INDEXES` is a parse error (exit 1),
  and the message names `SHOW` rather than the spacing.
- **There is no `ALTER INDEX`.** Changing one is `DROP` then `CREATE` — two
  invocations, not atomic; the index is absent between them.

### One statement per invocation — a silent trap

The schema parser **stops when its grammar is satisfied and discards whatever
follows, without an error and without a notification.** Verified:

```bash
# The index IS created; the SET NEVER RUNS; exit 0 and {"ok":true}
rmp graph client -r "$R" --query \
  "CREATE INDEX trail_ix FOR (n:Probe) ON (n.name) MATCH (m:Probe) SET m.touched=true"
# -> {"ok":true,"counters":{"indexesAdded":1}}
# and afterwards: count of nodes with m.touched -> 0
```

Issue the two statements as two invocations.

## Exit codes

| Code | Meaning in the graph context |
|---|---|
| 0 | Success |
| 1 | **`serve`:** the store could not be created/opened/recovered, its lock could not be taken, or the socket could not be bound (including a path too long). **`client`:** no server listening; a Cypher parse or execution error; a statement cancelled for exceeding the 5s budget (which wrote nothing). **Either:** a server unreachable through the socket. |
| 2 | No query supplied (no `-q`, empty stdin); `--socket` with an empty value; a positional argument was given; an unrecognised flag; a flag given twice |
| 3 | No roadmap selected (`-r` missing) |
| 4 | Roadmap not found |
| 6 | Exactly two causes: **`client` only**, a statement longer than 1048576 bytes; **either**, a `-r` value that breaks the roadmap-name character rule. It no longer signals an operation-class mismatch. |
| 127 | Unknown subcommand — including every one of the six removed ones |

## Storage & side effects

- The graph lives at `~/.roadmaps/<name>/graph/` (`wal`, `snapshot/`,
  `wal.lock`, `write.lock`), created on first `serve` with mode 0700.
- The roadmap's SQLite `project.db` is **never** touched by graph commands.
- `serve` binds `~/.roadmaps/<name>/graph.sock` (mode 0600) and removes it on
  shutdown; it writes to the store's `wal` and `snapshot/` on behalf of the
  statements clients send.
- `client` opens no store, takes no lock, and creates no directory.
- Writes are durable independently of the server's lifetime: each is committed
  before its acknowledgement returns, and shutdown checkpoints. While no server
  runs the graph is **unreachable, not empty**.

## Canonical examples

```bash
R=myproject

# Start the server (long-lived — background it, wait for the socket line)
nohup rmp graph serve -r "$R" > /tmp/kg-$R.log 2>&1 &
grep -q '"socket"' /tmp/kg-$R.log   # poll until true

# Read
rmp graph client -r "$R" --query "MATCH (n:Package) RETURN n.path, n.importPath"

# Read via stdin (multi-line / special characters)
echo "MATCH (n) UNWIND labels(n) AS l RETURN l, count(*) AS c ORDER BY c DESC" \
  | rmp graph client -r "$R"

# Write, and read the counters
rmp graph client -r "$R" --query \
  "MERGE (s:Spec {path:'docs/cypher.md'}) ON CREATE SET s.title='Cypher'"

# Traversal (no special subcommand — it is just a statement)
rmp graph client -r "$R" --query \
  "MATCH (a:Package)-[:CONTAINS*1..3]->(b:Package) RETURN DISTINCT b.path"

# Schema
rmp graph client -r "$R" --query "SHOW INDEXES"
rmp graph client -r "$R" --query "CREATE INDEX spec_path IF NOT EXISTS FOR (n:Spec) ON (n.path)"

# Plan and measurement
rmp graph client -r "$R" --query "EXPLAIN MATCH (n:Spec {path:'docs/cypher.md'}) RETURN n.title"
rmp graph client -r "$R" --query "PROFILE MATCH (n:Spec {path:'docs/cypher.md'}) RETURN n.title"

# Stop the server (checkpoints, releases the lock, removes the socket)
kill -TERM "$(pgrep -f "rmp graph serve -r $R")"
```

## Cypher dialect notes (measured)

- `||` string concatenation is **not** supported (parse error). Build strings
  outside the statement, or use `UNWIND` over a literal list.
- `CALL db.labels()` and `CALL db.relationshipTypes()` work and are the cheapest
  label/edge-type inventory.
- `MATCH … MATCH …` (two separate clauses) is accepted and is the prescribed way
  to link existing nodes.
