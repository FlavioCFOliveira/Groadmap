# graph

## Description

Operate a roadmap's knowledge graph: a free-form, queryable store of the project's elements and the relationships between them, backed by the GoGraph engine. The graph turns a roadmap into a "second brain" where an AI agent records and retrieves project elements (specs, code, decisions, dependencies) and how they connect, without re-reading every source file.

Each roadmap owns one graph, stored under that roadmap's home directory at `~/.roadmaps/<name>/graph/` (a directory, mode `0700`), created on first use of any `graph` subcommand. The graph is free-form: Groadmap imposes no schema. It is independent of the roadmap's SQLite tasks and sprints data in this version.

The graph is accessed through five subcommands, each accepting a Cypher query. Each subcommand is a guard rail that accepts only Cypher whose operation class matches the subcommand and rejects everything else before execution. Four subcommands accept one operation class each; `update` accepts three, because it is also the subcommand through which the graph's schema — its indexes and its constraints — is managed.

## Synopsis

```
rmp graph <subcommand> -r <roadmap> [--query <cypher>]
```

## Subcommands

### create

Adds nodes and/or edges to the graph. Accepts only Cypher whose writing clauses are `CREATE` and/or `MERGE`. Runs as a single transaction. Read-only queries and any query containing `SET`, `REMOVE`, `DELETE`, or `DETACH DELETE` are rejected by the guard rail.

**Usage:** `rmp graph create -r <roadmap> [--query <cypher>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-q` | `--query` | string | - | Cypher query string. When absent, the query is read from standard input |
| `-h` | `--help` | bool | false | Show subcommand help |

**Output:** `{"ok": true}` when the query has no `RETURN` clause; `{"columns": [...], "rows": [[...], ...]}` when a `RETURN` clause is present.

**Examples:**
```bash
# Create a spec node linked to its implementation
rmp graph create -r backend-platform \
  --query "MERGE (s:Spec {key:'user-authentication'}) MERGE (c:Code {path:'internal/auth/jwt.go'}) MERGE (s)-[:IMPLEMENTED_BY]->(c)"

# Create a node and return it
rmp graph create -r backend-platform \
  --query "CREATE (s:Spec {key:'rate-limiting'}) RETURN s"
```

---

### query

Reads from the graph and returns the result columns and rows. Read-only: any query containing a writing clause (`CREATE`, `MERGE`, `SET`, `REMOVE`, `DELETE`, `DETACH DELETE`) is rejected by the guard rail, as is any schema-mutating DDL clause (`CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, `DROP CONSTRAINT`).

Schema introspection is accepted: `SHOW INDEXES` and `SHOW CONSTRAINTS`, their singular aliases `SHOW INDEX` and `SHOW CONSTRAINT`, and any of them followed by a `YIELD` / `WHERE` / `RETURN` projection. These list the registered schema without altering it, so they are read-only. The same commands are accepted by `search`, for the same reason, and by `update`, which owns the schema; `create` and `delete` reject them, because they carry none of the data-writing clauses those two subcommands accept.

Write the command with **exactly one space between its two keywords**. The engine recognises `SHOW INDEXES` only in that spelling, so `SHOW  INDEXES` with two spaces, with a tab, or with a line break is rejected by the guard rail with exit code 6 and a message naming the spacing, before the query reaches the engine. Whitespace and comments *before* the statement are fine, and so is any amount of whitespace *after* the target keyword: `SHOW INDEXES   YIELD name` is accepted.

**Usage:** `rmp graph query -r <roadmap> [--query <cypher>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-q` | `--query` | string | - | Cypher query string. When absent, the query is read from standard input |
| `-h` | `--help` | bool | false | Show subcommand help |

**Output:** `{"columns": [...], "rows": [[...], ...]}`

**Examples:**
```bash
# Find which code implements each spec
rmp graph query -r backend-platform \
  --query "MATCH (s:Spec)-[:IMPLEMENTED_BY]->(c:Code) RETURN s.key, c.path"

# Read the query from standard input
echo "MATCH (n) RETURN count(n)" | rmp graph query -r backend-platform

# Introspect the registered schema
rmp graph query -r backend-platform --query "SHOW INDEXES"
```

---

### update

Mutates properties or labels on existing graph elements, and is also the subcommand through which the graph's schema is managed. It accepts three kinds of statement:

- a **data mutation** whose writing clauses are `SET` and/or `REMOVE`, which runs as a single transaction;
- **schema DDL** — `CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, `DROP CONSTRAINT` — which the engine runs outside that transaction, because a schema change is not transactional in this engine;
- **schema introspection** — `SHOW INDEXES`, `SHOW INDEX`, `SHOW CONSTRAINTS`, `SHOW CONSTRAINT`, each with an optional `YIELD` / `WHERE` / `RETURN` projection tail — which reports the registered schema and changes nothing.

`CREATE`, `MERGE`, `DELETE`, `DETACH DELETE`, and every read-only query that is not a schema-introspection command are rejected by the guard rail, with exit code 6 and the message:

```
Error: validation error: graph update accepts only SET/REMOVE, index/constraint DDL, and schema-introspection queries
```

**Usage:** `rmp graph update -r <roadmap> [--query <cypher>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-q` | `--query` | string | - | Cypher query string. When absent, the query is read from standard input |
| `-h` | `--help` | bool | false | Show subcommand help |

**Output:** the output mirrors what the executed statement returns. `{"columns": [...], "rows": [[...], ...]}` when the statement produces result columns; `{"ok": true}` when it produces none. For a data mutation the two cases are exactly "has a `RETURN` clause" and "has none". A `CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, or `DROP CONSTRAINT` produces no columns and returns `{"ok": true}`; a schema-introspection command produces the listing and returns the `{columns, rows}` shape even though it carries no `RETURN` clause.

**Examples:**
```bash
# Mark a spec as implemented
rmp graph update -r backend-platform \
  --query "MATCH (s:Spec {key:'user-authentication'}) SET s.status = 'implemented'"
```

#### Managing the schema

The surface is the engine's own Cypher, not a Groadmap verb. There is no `index` subcommand, no `--create` / `--drop` flags, and no vocabulary of Groadmap's own: a schema statement is written through `--query` or standard input exactly as every other graph statement is. The set of schema statements supported is therefore exactly the set the engine supports, and it widens or narrows with the engine rather than with a Groadmap release.

Groadmap declares no schema object of its own. No `rmp` command creates, drops, or requires an index or a constraint as a side effect of anything else it does, so every schema object in a graph is one its owner asked for. A graph that has never been given one is fully functional: an index is an optimisation you may choose, and a constraint is an integrity rule you may choose.

An index and a constraint each cover exactly one node property. The engine supports neither a composite (multi-property) form nor a form over a relationship property, and a constraint is either a uniqueness rule (`IS UNIQUE`) or a presence rule (`IS NOT NULL`).

**Creating an index or a constraint**

```bash
# Named index on one node property
rmp graph update -r backend-platform \
  --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"

# Unnamed: the engine derives the name from the lowercased label, the lowercased
# property and the index kind, joined by underscores. This one registers as
# spec_title_hash
rmp graph update -r backend-platform \
  --query "CREATE INDEX FOR (n:Spec) ON (n.title)"

# IF NOT EXISTS makes a create whose object already exists a silent no-op
rmp graph update -r backend-platform \
  --query "CREATE INDEX spec_key IF NOT EXISTS FOR (n:Spec) ON (n.key)"

# An index is a hash index by default; a comparison-ordered index is requested
# through the statement's OPTIONS map
rmp graph update -r backend-platform \
  --query "CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}"

# A uniqueness constraint, and a presence constraint
rmp graph update -r backend-platform \
  --query "CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE"
rmp graph update -r backend-platform \
  --query "CREATE CONSTRAINT spec_title_req IF NOT EXISTS FOR (n:Spec) REQUIRE n.title IS NOT NULL"
```

Each of these statements produces no result columns, so on success it outputs `{"ok": true}` and exits 0.

A `CREATE INDEX` back-fills the new index from the data already in the graph. A `CREATE CONSTRAINT` validates the data already in the graph first and registers the constraint only if it passes; a uniqueness rule over a property that already holds a repeated value, or a presence rule over a property some node lacks, is refused with exit code 1 and nothing is registered.

Index kinds are the engine's own vocabulary — `hash` by default, `btree` through `OPTIONS` — and not the index kinds of another Cypher implementation. A statement written against another implementation's vocabulary is refused by the engine.

**Dropping an index or a constraint**

```bash
# Removal is by name only, never by a label-and-property pair
rmp graph update -r backend-platform --query "DROP INDEX spec_key"
rmp graph update -r backend-platform --query "DROP CONSTRAINT spec_key_uq"

# IF EXISTS makes a drop of an absent object a silent no-op
rmp graph update -r backend-platform --query "DROP INDEX spec_key IF EXISTS"
```

Because removal is by name only, a caller who did not declare a name must first learn the derived one from a listing. Declaring a name is the recommended practice and Groadmap does not enforce it: a named object is dropped by the name its author wrote, while an unnamed one is dropped by a name the engine chose, which changes if the index kind changes.

**Listing the schema**

```bash
rmp graph update -r backend-platform --query "SHOW INDEXES"
rmp graph update -r backend-platform --query "SHOW CONSTRAINTS"

# The singular aliases are the same commands
rmp graph update -r backend-platform --query "SHOW INDEX"
rmp graph update -r backend-platform --query "SHOW CONSTRAINT"

# With a projection tail
rmp graph update -r backend-platform --query "SHOW INDEXES YIELD name, type"
```

`SHOW INDEXES` and `SHOW CONSTRAINTS` are the authoritative report of what a schema object is called, and are how you learn a derived name. A listing is ordered deterministically, so two invocations against an unchanged graph produce the same rows in the same order. The same commands run identically under `rmp graph query` and `rmp graph search`; all three report the schema the store actually holds.

**Altering and recreating: two invocations, and not atomic**

The engine has no statement that changes an index in place. There is no `ALTER INDEX`, no `REBUILD INDEX`, and no `CREATE OR REPLACE INDEX`, and each of the three is refused by the parser as an unrecognised statement. Changing an index — its kind, or its definition — and rebuilding one are therefore a `DROP` followed by a `CREATE`, issued as **two separate invocations**; Groadmap composes nothing on your behalf:

```bash
rmp graph update -r backend-platform --query "DROP INDEX spec_ord"
rmp graph update -r backend-platform \
  --query "CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}"
```

Altering is a drop followed by a create with a different definition; recreating is a drop followed by a create with the identical definition, and the rebuild is the back-fill the create performs.

**The pair is not atomic.** The two invocations are two processes, each taking and releasing the store's exclusive lock, and nothing spans them. If the second fails — a rejected definition, a lock it cannot take, a machine that stops between the two — the index is **dropped and not recreated**, and the graph is left with no index where it had one. Nothing in Groadmap detects that state, reports it, or repairs it; you learn of it from `SHOW INDEXES` and repair it by issuing the create again. Queries stay correct throughout, because an index is an access path and never a source of results, so what is lost is speed rather than answers.

Both halves cost time proportional to the graph: a create back-fills the index from every node carrying the label, and a drop discards that work. On a roadmap knowledge graph this is small, and it does not stay small if the graph grows.

**One statement per invocation**

An invocation carries exactly one schema statement. A DDL statement carrying a further clause after it is rejected with exit code 6, before the graph store is opened, with a message naming the trailing text.

The refusal is Groadmap's own, and it is load-bearing. The engine's schema parser stops as soon as its grammar is satisfied and **discards the rest of the statement silently** — without an error, without a notification, and without any other trace. Handed

```
CREATE INDEX ix FOR (n:Spec) ON (n.key) MATCH (m) SET m.p = true
```

the engine would create the index, drop the `MATCH ... SET` on the floor, and return success, so `graph update` would print `{"ok": true}` and exit 0 for a statement half of which never ran — and you would have no reason to check, because the command reported that it worked. Groadmap refuses such a statement instead. Issue the two halves as two invocations.

The rule belongs to the DDL class alone. A schema-introspection command carrying a further clause is already refused by the engine, which names the unsupported clause rather than discarding it. And a statement that *begins* with a data-writing clause and carries schema text after it is not a schema statement at all: the engine routes it to the general Cypher grammar, which refuses it as a parse error (exit code 1).

**Schema failure classes and their exit codes**

Two different parties refuse a schema statement, and the exit code is what tells them apart. A **guard-rail** refusal carries Groadmap's own message, happens before the graph store is opened, and exits **6**. An **engine** refusal carries the engine's own diagnostic text after the wording Groadmap fixes, and exits **1**.

| Failure | Refused by | Exit code |
|---------|-----------|-----------|
| Schema DDL issued under `create`, `query`, `delete`, or `search` | Guard rail | 6 |
| A `SHOW INDEX(ES)` / `SHOW CONSTRAINT(S)` whose keyword spacing the engine does not accept, under `query`, `search`, or `update` | Guard rail | 6 |
| A DDL statement carrying a further clause after it | Guard rail | 6 |
| `CREATE INDEX` or `CREATE CONSTRAINT` whose object already exists, without `IF NOT EXISTS` | Engine | 1 |
| `DROP INDEX` or `DROP CONSTRAINT` naming an object that does not exist, without `IF EXISTS` | Engine | 1 |
| A definition the engine does not support — composite, over a relationship property, or a constraint kind it does not implement | Engine | 1 |
| `CREATE CONSTRAINT` that the data already in the graph does not satisfy | Engine | 1 |

A duplicate create and a drop of an absent object are **engine** failures rather than validation failures, and they exit **1** rather than 6. This is stated explicitly because the exit code is the opposite of what a reader may expect: both look like input errors, and neither is one. Groadmap cannot know whether an object exists without opening the store, so the check belongs where the knowledge is. A caller who wants either to be a no-op writes `IF NOT EXISTS` or `IF EXISTS`.

```bash
# Exit 1: the engine refuses the second create, because the object exists
rmp graph update -r backend-platform --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"
rmp graph update -r backend-platform --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"
# Error: database error: graph update failed: <engine diagnostic>

# Exit 6: the guard rail refuses the class, before the store is opened
rmp graph query -r backend-platform --query "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"
# Error: validation error: graph query accepts only read-only queries
```

A failed schema statement leaves the schema as it was. No partial registration exists in any of these classes: the object is either registered or it is not.

---

### delete

Removes nodes and/or edges. Accepts only Cypher whose writing clauses are `DELETE` and/or `DETACH DELETE`. Runs as a single transaction. `CREATE`, `MERGE`, `SET`, and `REMOVE` are rejected by the guard rail.

**Usage:** `rmp graph delete -r <roadmap> [--query <cypher>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-q` | `--query` | string | - | Cypher query string. When absent, the query is read from standard input |
| `-h` | `--help` | bool | false | Show subcommand help |

**Output:** `{"ok": true}` when the query has no `RETURN` clause; `{"columns": [...], "rows": [[...], ...]}` when a `RETURN` clause is present.

**Examples:**
```bash
# Remove a decision node and all its relationships
rmp graph delete -r backend-platform \
  --query "MATCH (d:Decision {key:'use-sessions'}) DETACH DELETE d"
```

---

### search

Read-only traversal and pattern matching, including variable-length paths (for example `-[*1..3]-`). Semantically the traversal-oriented sibling of `query`; it enforces the same read-only guard rail, and so accepts schema introspection on the same terms.

**Usage:** `rmp graph search -r <roadmap> [--query <cypher>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-q` | `--query` | string | - | Cypher query string. When absent, the query is read from standard input |
| `-h` | `--help` | bool | false | Show subcommand help |

**Output:** `{"columns": [...], "rows": [[...], ...]}`

**Examples:**
```bash
# Variable-length traversal across dependency chains
rmp graph search -r backend-platform \
  --query "MATCH path = (s:Spec {key:'user-authentication'})-[:DEPENDS_ON*1..3]->(d:Dependency) RETURN path"
```

## Guard Rail (Operation Classes)

Each subcommand accepts only Cypher whose operation class matches it; everything else is rejected with exit code 6 before the query executes, so a rejected query never mutates the graph.

| Subcommand | Operation | Accepts | Rejects |
|------------|-----------|---------|---------|
| `create` | Create nodes/edges | Writing query whose only writing clauses are `CREATE` and/or `MERGE` | Read-only queries; `SET`, `REMOVE`, `DELETE`, `DETACH DELETE`; DDL; schema introspection |
| `query` | Read | Read-only query (`MATCH ... RETURN`, no writing clause), or a schema-introspection command | Any writing clause; any DDL clause |
| `update` | Mutate existing, and manage the schema | Writing query whose writing clauses are `SET` and/or `REMOVE`; schema DDL (`CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, `DROP CONSTRAINT`); a schema-introspection command | Read-only queries other than a schema-introspection command; `CREATE`, `MERGE`, `DELETE`, `DETACH DELETE` |
| `delete` | Remove | Writing query whose writing clauses are `DELETE` and/or `DETACH DELETE` | Read-only queries; `CREATE`, `MERGE`, `SET`, `REMOVE`; DDL; schema introspection |
| `search` | Read (traversal) | Read-only query, including variable-length paths (e.g. `-[*1..3]-`), or a schema-introspection command | Any writing clause; any DDL clause |

Two clause families are worth naming explicitly:

- **Schema introspection** (`SHOW INDEXES`, `SHOW INDEX`, `SHOW CONSTRAINTS`, `SHOW CONSTRAINT`, each with an optional `YIELD` / `WHERE` / `RETURN` tail) lists the registered schema without altering it, so it is read-only. It is accepted by `query` and `search` as the read it is, and by `update` because that subcommand owns the schema, so listing the schema and changing it are reached the same way; `create` and `delete` reject it, each accepting only its own data-writing clause class. It is distinct from **DDL** (`CREATE INDEX`, `DROP INDEX`, `CREATE CONSTRAINT`, `DROP CONSTRAINT`), which mutates the schema and is accepted by `update` alone and rejected by the other four.
  Only the single-space spelling of an introspection command is accepted, because it is the only one the engine parses; any other separator between the two keywords is rejected with exit code 6 under `query`, `search`, and `update` — the three subcommands that accept the class. The DDL forms, by contrast, are matched at any spacing, so on the four subcommands that refuse the class `CREATE   INDEX` is refused exactly as `CREATE INDEX` is. The difference is deliberate: the DDL match exists to refuse on those four, so a wider match only refuses more, while the introspection match exists to accept, so a wider match would accept statements the engine then rejects.
  The one place the wide DDL match is visible is `update`, which accepts the class: a badly spaced `CREATE   INDEX ...` passes the guard rail and is then refused by the engine's general grammar with exit code 1 and a parse diagnostic that names the wrong problem. Nothing is created and the graph is unchanged; the cost is the misleading message. The match stays wide because narrowing it would reopen a hole on the other four, which must refuse DDL at any spacing.
- **`FOREACH`** is a writing clause, classified by the clauses its body contains. `FOREACH (x IN list | SET ...)` is a mutating write valid only under `update`; `FOREACH (x IN list | CREATE ...)` is a creating write valid only under `create`; every `FOREACH` is rejected by `query` and `search`.

## Query Input Source and Precedence

Each subcommand obtains its Cypher from one of two sources:

1. When `--query` is present and non-empty, its value is used and standard input is not read.
2. When `--query` is absent, the entire standard input is read and used as the query (for example `cat query.cypher | rmp graph query -r backend-platform`).
3. When `--query` is absent and standard input is empty or not connected, the command fails with exit code 2 (no query supplied).
4. When `--query` is present but empty or whitespace only, the command fails with exit code 2.
5. Leading and trailing whitespace is trimmed before validation and execution.

## Modelling Conventions

The graph is free-form, but it tends toward a multi-layer model (specification, code, decisions, dependencies). These are recommendations only; Groadmap does not enforce or auto-create any schema:

- **Layer as a label.** Tag each node with a label naming its layer, such as `Spec`, `Code`, `Decision`, `Dependency`, or `Requirement`.
- **Identity as a property.** Give each node a stable identifier property (for example `key` or `path`) so you can `MERGE` on it without creating duplicates.
- **Cross-layer relationships as typed edges.** Use verb-like edge types such as `IMPLEMENTS`, `DEPENDS_ON`, `DECIDED_BY`, `REFERENCES`, or `SUPERSEDES`.
- **Properties for attributes.** Store titles, statuses, file paths, and timestamps as node or edge properties.

## Aliases

The `graph` command has no alias, and its subcommands have no aliases.

## Notes

- The graph is created on first use of any subcommand, including read subcommands; a read against a roadmap with no graph yet returns an empty result and is not an error.
- The graph store is a directory (`~/.roadmaps/<name>/graph/`, mode `0700`), not a single file, because GoGraph persists through an on-disk snapshot plus a write-ahead log.
- Graph operations never read from or write to the roadmap's SQLite `project.db`, and removing a roadmap (`rmp roadmap remove <name>`) deletes the graph along with the rest of the roadmap home directory.
- Write subcommands run inside a single transaction and persist durably before the process exits. The engine reports no affected-element count, so write results carry no such field.
- A schema statement under `graph update` is the one exception to that transaction: the subcommand still takes the store's exclusive lock and runs the statement through the engine's transactional entry point, but the engine recognises a schema statement there and executes it outside the transaction, because a schema change is not transactional in this engine. A schema statement that succeeds has taken effect and there is nothing to roll it back into. It checkpoints like any other successful write, and the snapshot carries the registered schema.

## Output Format

All subcommands follow these conventions:
- **Success**: JSON output to stdout, exit code 0
- **Errors**: Plain text to stderr, non-zero exit code

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Query executed successfully |
| 1 | Cypher failed to parse or execute, or the graph store could not be opened, read, or written; or the engine refused a schema statement run under `update` (see [Managing the schema](#managing-the-schema)) |
| 2 | No query supplied (`--query` absent and stdin empty, or `--query` empty/whitespace) |
| 3 | No roadmap selected (`-r` missing/required) |
| 4 | Roadmap not found (the roadmap given via `-r` does not exist) |
| 6 | The query's operation class does not match the subcommand; or `query`, `search`, or `update` received a schema-introspection command written with anything other than exactly one space between its two keywords; or `update` received a DDL statement carrying a further clause after it |
| 127 | Unknown subcommand |
