# graph

## Description

Operate a roadmap's knowledge graph: a free-form, queryable store of the project's elements and the relationships between them, backed by the GoGraph engine. The graph turns a roadmap into a "second brain" where an AI agent records and retrieves project elements (specs, code, decisions, dependencies) and how they connect, without re-reading every source file.

Each roadmap owns one graph, stored under that roadmap's home directory at `~/.roadmaps/<name>/graph/` (a directory, mode `0700`), created on first use of the `graph` command. The graph is free-form: Groadmap imposes no schema. It is independent of the roadmap's SQLite tasks and sprints data in this version.

The graph is reached through two subcommands. `execute` accepts any Cypher statement the engine accepts and runs it against the roadmap's graph; `serve` runs no statement of its own and instead holds that graph open, answering statements over a Unix domain socket until it is stopped. Groadmap does not examine a statement and refuses none on the ground of what it does.

## Synopsis

```
rmp graph execute -r <roadmap> [--query <cypher>]
rmp graph serve -r <roadmap> [--socket <path>]
```

## Subcommands

### execute

Runs exactly one Cypher statement against the roadmap's knowledge graph and prints what it returns. A statement that changes the graph runs inside a single transaction and is persisted durably before the process exits.

Every class of statement runs through this one subcommand:

- a **read**, such as `MATCH ... RETURN`, including variable-length traversals like `-[*1..3]-`;
- a **write**, such as `CREATE`, `MERGE`, `SET` or `REMOVE`;
- a **deletion**, such as `DELETE` or `DETACH DELETE`;
- **schema DDL** — `CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, `DROP CONSTRAINT`;
- **schema introspection** — `SHOW INDEXES`, `SHOW INDEX`, `SHOW CONSTRAINTS`, `SHOW CONSTRAINT`, each with an optional `YIELD` / `WHERE` / `RETURN` projection tail.

**Usage:** `rmp graph execute -r <roadmap> [--query <cypher>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-q` | `--query` | string | - | Cypher statement. When absent, the statement is read from standard input |
| `-h` | `--help` | bool | false | Show subcommand help |

**Output:** `{"columns": [...], "rows": [[...], ...]}` when the statement produces result columns; `{"ok": true}` when it produces none. For a data statement the two cases are exactly "has a `RETURN` clause" and "has none". A schema-introspection command produces the listing and returns the `{columns, rows}` shape even though it carries no `RETURN` clause; a `CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT` or `DROP CONSTRAINT` produces no columns and returns `{"ok": true}`.

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
```

### serve

Opens the roadmap's knowledge graph once, holds it and its exclusive advisory store lock for the life of the process, and answers Cypher statements over a Unix domain socket until it is stopped. The protocol is Bolt version 5, served by the graph engine's own server; Groadmap defines no protocol of its own.

`serve` is **long-lived**. Unlike every other command except `rmp web`, it does not complete and exit: it runs until it receives `SIGINT` (`Ctrl+C`) or `SIGTERM`, then drains the work in flight, shuts the server down, checkpoints, releases the lock, removes its socket, and exits `0`.

**One server per roadmap.** The roadmap's store lock is the interlock: a second `rmp graph serve` against the same roadmap cannot take it, fails with exit code `1`, and leaves the first server's socket untouched. It does not queue.

`serve` runs no statement of its own, creates no graph directory that does not already exist, and never reads or writes a roadmap's `project.db`. It serves one roadmap; serving several means running several servers, one per roadmap, each on its own socket.

**Access control is the filesystem, and there is no other.** The socket is created with mode `0600`, set explicitly rather than left to the process umask, inside a roadmap home directory that is `0700`. The server authenticates nobody: any caller able to open the socket can read, write, delete and change the schema of that roadmap's graph. Two warnings from the engine are expected on stderr at startup and are not failures — one for a server running without transport security, one for a server running without authentication.

**Usage:** `rmp graph serve -r <roadmap> [--socket <path>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| | `--socket` | string | `~/.roadmaps/<name>/graph.sock` | Unix domain socket to bind. A non-default path is followed by the CLI through the same flag and by nothing else: the web interface has no way to receive one |
| `-h` | `--help` | bool | false | Show subcommand help |

**Output:** a single JSON object on stdout at startup, naming the absolute path of the socket the server bound, so a caller that supplied no `--socket` still learns the path: `{"socket": "/home/user/.roadmaps/backend-platform/graph.sock"}`. Per-statement results go to the client that asked for them, never to this command's stdout.

**Examples:**
```bash
# Serve a roadmap's graph on the default socket, until Ctrl+C
rmp graph serve -r backend-platform

# Serve on a socket of your own choosing
rmp graph serve -r backend-platform --socket /run/user/1000/backend-platform-graph.sock
```

## The Withdrawn Subcommand Names

`create`, `query`, `update`, `delete` and `search` were subcommands of `rmp graph`. They are not any more, and `execute` and `serve` are the whole of the family. Each is an unresolved subcommand name and is answered as a dispatch failure: exit code `127`, the `graph` help on stderr, nothing on stdout. They are named here because an agent that has one of them in memory needs to be told that it will not resolve.

They existed to enforce an operation class, and that enforcement has been withdrawn. Nothing distinguished the five once it was gone, so they were replaced rather than kept as five names for one behaviour.

**`execute` runs what it is given, and the caller owns what that does.** No subcommand's contract says that a statement cannot delete. A statement's effect is decided by its Cypher and by nothing `rmp` inspects, so the guarantee you need about a statement is a guarantee about the text you supply. Between reading the statement and running it, Groadmap checks its length and nothing else about its content.

The hazards that follow are all silent and all report success. They are enumerated in `SPEC/GRAPH.md § What Groadmap Does Not Check`, and the ones worth knowing before you type a statement are:

- A statement whose bytes are not valid UTF-8 executes, with every undecodable byte replaced by `U+FFFD`. A write stores a value that was never supplied; a match compares against a literal that was never supplied and reports success having found nothing.
- A property value carrying a control character is stored. Cypher decodes `\b`, `\f` and `\uXXXX` inside a string literal, so a statement whose own text is pure ASCII can write a real `ESC` into the store.
- A relationship written through an incoming or undirected pattern is **not written**, and the statement still reports success. Every relationship is writable through an outgoing pattern, which may be anchored on either endpoint: use `MATCH (s)-[e]->(v:Test {key:'…'}) SET e.last_commit = '…'` rather than the reverse spelling.
- A schema DDL statement carrying a further clause after it executes **in part**: the engine's schema parser stops when its grammar is satisfied and discards the rest without an error or a notification. Issue the two halves as two invocations.
- A schema-introspection command written with anything but a single space between its two keywords fails as a syntax error whose message names `SHOW` rather than the spacing. `SHOW  INDEXES` fails; `SHOW INDEXES` succeeds. The same is true of the four DDL forms.

## Managing the Schema

The surface is the engine's own Cypher, not a Groadmap verb. There is no `index` subcommand, no `--create` / `--drop` flags, and no vocabulary of Groadmap's own: a schema statement is written through `--query` or standard input exactly as every other statement is. The set of schema statements supported is therefore exactly the set the engine supports, and it widens or narrows with the engine rather than with a Groadmap release.

Groadmap declares no schema object of its own. No `rmp` command creates, drops, or requires an index or a constraint as a side effect of anything else it does, so every schema object in a graph is one its owner asked for. A graph that has never been given one is fully functional: an index is an optimisation you may choose, and a constraint is an integrity rule you may choose.

An index and a constraint each cover exactly one node property. The engine supports neither a composite (multi-property) form nor a form over a relationship property, and a constraint is either a uniqueness rule (`IS UNIQUE`) or a presence rule (`IS NOT NULL`).

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

**The pair is not atomic.** The two invocations are two processes, each taking and releasing the store's exclusive lock, and nothing spans them. If the second fails — a rejected definition, a lock it cannot take, a machine that stops between the two — the index is **dropped and not recreated**, and the graph is left with no index where it had one. Nothing in Groadmap detects that state, reports it, or repairs it; you learn of it from `SHOW INDEXES` and repair it by issuing the create again. Queries stay correct throughout, because an index is an access path and never a source of results, so what is lost is speed rather than answers.

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

`execute` obtains its Cypher from one of two sources:

1. When `--query` is present and non-empty, its value is used and standard input is not read.
2. When `--query` is absent, the statement is read from standard input under a bound; the read is not a read to EOF (for example `cat query.cypher | rmp graph execute -r backend-platform`).
3. When `--query` is absent and standard input supplies no statement — it is a terminal, it is already at end of stream, or everything it carries is whitespace — the command fails with exit code 2 (no query supplied). A terminal is refused without being read at all.
4. When `--query` is present but its value is empty, whitespace only, or absent, the command fails with exit code 2. A following token that begins with `--`, or with a single `-` followed by an ASCII letter, is the next flag and is never swallowed as the query; a `-` followed by a digit or a decimal point is a legitimate query value.
5. Leading and trailing whitespace is trimmed before execution, after the length check.

A statement longer than 1048576 bytes (1 MiB) is refused with exit code 6, whichever source carried it. That is the only cause of exit code 6 this command has.

`execute` accepts **no positional argument**. A bare Cypher statement written on the command line is an excess positional argument and is refused with exit code 2 and the line:

```
Error: invalid input: unexpected argument "X" (graph queries use --query or stdin)
```

The refusal precedes the store open, the standard-input read, and the maximum-length check, so a refused invocation does nothing at all.

## Modelling Conventions

The graph is free-form, but it tends toward a multi-layer model (specification, code, decisions, dependencies). These are recommendations only; Groadmap does not enforce or auto-create any schema:

- **Layer as a label.** Tag each node with a label naming its layer, such as `Spec`, `Code`, `Decision`, `Dependency`, or `Requirement`.
- **Identity as a property.** Give each node a stable identifier property (for example `key` or `path`) so you can `MERGE` on it without creating duplicates.
- **Cross-layer relationships as typed edges.** Use verb-like edge types such as `IMPLEMENTS`, `DEPENDS_ON`, `DECIDED_BY`, `REFERENCES`, or `SUPERSEDES`.
- **Properties for attributes.** Store titles, statuses, file paths, and timestamps as node or edge properties.

## Aliases

The `graph` command has no alias, and `execute` has no alias.

## Notes

- The graph is created on first use of `rmp graph execute`, including by a statement that only reads; a read against a roadmap with no graph yet returns an empty result and is not an error.
- The graph store is a directory (`~/.roadmaps/<name>/graph/`, mode `0700`), not a single file, because GoGraph persists through an on-disk snapshot plus a write-ahead log.
- Graph operations never read from or write to the roadmap's SQLite `project.db`, and removing a roadmap (`rmp roadmap remove <name>`) deletes the graph along with the rest of the roadmap home directory.
- Every invocation takes the store's advisory lock **exclusively**, and holds it across the whole open, execution, commit, checkpoint and write-ahead-log truncation sequence. There is one lock mode because there is one execution path: Groadmap cannot know before running a statement whether it will write. Two invocations against the same roadmap therefore serialise even when neither of them writes; an invocation that finds the lock held waits, under a bounded backoff, and fails with exit code 1 only once that wait is exhausted.
- A statement that changes the graph runs inside a single transaction and persists durably before the process exits. The engine reports no affected-element count, so write results carry no such field.
- A statement whose transaction appended nothing to the write-ahead log neither snapshots nor truncates the log; the store's `snapshot/` directory and its `wal` file are left exactly as the statement found them.
- A schema statement is the one exception to that transaction: the invocation still takes the exclusive lock and runs the statement through the engine's transactional entry point, but the engine recognises a schema statement there and executes it outside the transaction, because a schema change is not transactional in this engine. A schema statement that succeeds has taken effect and there is nothing to roll it back into. It checkpoints like any other successful write, and the snapshot carries the registered schema.
- The engine may attach advisory notifications to a result — a Cartesian-product warning on a disconnected multi-pattern `MATCH`, for example. Each is written to stderr as one plain-text line and changes neither the stdout output nor the exit code.

## Output Format

`execute` follows these conventions:
- **Success**: JSON output to stdout, exit code 0
- **Errors**: Plain text to stderr, non-zero exit code

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | The statement executed successfully |
| 1 | Cypher failed to parse or execute, the engine refused a schema statement, or the graph store could not be opened, read, or written |
| 2 | No statement supplied (`--query` absent and stdin empty, or `--query` empty/whitespace); or a positional argument was supplied |
| 3 | No roadmap selected (`-r` missing/required) |
| 4 | Roadmap not found (the roadmap given via `-r` does not exist) |
| 6 | The statement is longer than the maximum length of 1048576 bytes |
| 127 | Unknown subcommand |
