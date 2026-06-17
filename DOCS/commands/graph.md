# graph

## Description

Operate a roadmap's knowledge graph: a free-form, queryable store of the project's elements and the relationships between them, backed by the GoGraph engine. The graph turns a roadmap into a "second brain" where an AI agent records and retrieves project elements (specs, code, decisions, dependencies) and how they connect, without re-reading every source file.

Each roadmap owns one graph, stored under that roadmap's home directory at `~/.roadmaps/<name>/graph/` (a directory, mode `0700`), created on first use of any `graph` subcommand. The graph is free-form: Groadmap imposes no schema. It is independent of the roadmap's SQLite tasks and sprints data in this version.

The graph is accessed through five subcommands, each accepting a Cypher query. Each subcommand is a guard rail that accepts only Cypher whose operation class matches the subcommand and rejects everything else before execution.

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

Reads from the graph and returns the result columns and rows. Read-only: any query containing a writing clause (`CREATE`, `MERGE`, `SET`, `REMOVE`, `DELETE`, `DETACH DELETE`) is rejected by the guard rail.

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
```

---

### update

Mutates properties or labels on existing graph elements. Accepts only Cypher whose writing clauses are `SET` and/or `REMOVE`. Runs as a single transaction. `CREATE`, `MERGE`, `DELETE`, and `DETACH DELETE` are rejected by the guard rail.

**Usage:** `rmp graph update -r <roadmap> [--query <cypher>]`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-r` | `--roadmap` | string | - | Roadmap name (required) |
| `-q` | `--query` | string | - | Cypher query string. When absent, the query is read from standard input |
| `-h` | `--help` | bool | false | Show subcommand help |

**Output:** `{"ok": true}` when the query has no `RETURN` clause; `{"columns": [...], "rows": [[...], ...]}` when a `RETURN` clause is present.

**Examples:**
```bash
# Mark a spec as implemented
rmp graph update -r backend-platform \
  --query "MATCH (s:Spec {key:'user-authentication'}) SET s.status = 'implemented'"
```

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

Read-only traversal and pattern matching, including variable-length paths (for example `-[*1..3]-`). Semantically the traversal-oriented sibling of `query`; it enforces the same read-only guard rail.

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
| `create` | Create nodes/edges | Writing query whose only writing clauses are `CREATE` and/or `MERGE` | Read-only queries; `SET`, `REMOVE`, `DELETE`, `DETACH DELETE` |
| `query` | Read | Read-only query (`MATCH ... RETURN`, no writing clause) | Any writing clause |
| `update` | Mutate existing | Writing query whose writing clauses are `SET` and/or `REMOVE` | Read-only queries; `CREATE`, `MERGE`, `DELETE`, `DETACH DELETE` |
| `delete` | Remove | Writing query whose writing clauses are `DELETE` and/or `DETACH DELETE` | Read-only queries; `CREATE`, `MERGE`, `SET`, `REMOVE` |
| `search` | Read (traversal) | Read-only query, including variable-length paths (e.g. `-[*1..3]-`) | Any writing clause |

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

## Output Format

All subcommands follow these conventions:
- **Success**: JSON output to stdout, exit code 0
- **Errors**: Plain text to stderr, non-zero exit code

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Query executed successfully |
| 1 | Cypher failed to parse or execute, or the graph store could not be opened, read, or written |
| 2 | No query supplied (`--query` absent and stdin empty, or `--query` empty/whitespace) |
| 3 | No roadmap selected (`-r` missing/required) |
| 4 | Roadmap not found (the roadmap given via `-r` does not exist) |
| 6 | The query's operation class does not match the subcommand |
