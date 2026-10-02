# Cypher recipes

Copy-ready patterns for this graph. Every one goes through **one**
`rmp graph client` invocation against a **running server** (SKILL.md § The server
lifecycle). The labels, predicates and properties used here are defined in the
project's `./knowledge-model.md` — treat that file as the authoritative **shape**,
and the graph, never the file, as the authoritative **content**.

Throughout: `R` is the roadmap and `SOCK_ARG` the optional socket flag, both
resolved from `.rmp`:

```bash
R=$(rmp_conf roadmap); S=$(rmp_conf socket)
SOCK_ARG=(); [ -n "$S" ] && SOCK_ARG=(--socket "$S")
kg() { rmp graph client -r "$R" "${SOCK_ARG[@]}" --query "$1"; }   # one statement
```

Read shapes: `{"columns":[…],"rows":[…]}`. Writes without `RETURN`:
`{"ok":true}`, plus `counters` when something changed.

## Provenance — get the names and the values first

The provenance **property names** are the project's: read them from its
`./knowledge-model.md` before the first write (SKILL.md § Provenance). The recipes
below write them as `$PC` (commit) and `$PD` (date):

```bash
PC=<commit property from ./knowledge-model.md>   # e.g. gitCommit
PD=<date property from ./knowledge-model.md>     # e.g. gitDate
COMMIT=$(git rev-parse HEAD)            # full 40-char hash
GDATE=$(git show -s --format=%cs HEAD)  # e.g. 2026-09-08
```

## Writing — the upsert, in ONE statement

The two-step `create`-then-`update` dance is **obsolete**: it existed only because
a guard-rail rejected `SET`, and that guard-rail is gone.

```bash
kg "MERGE (f:Function {name:'ProcessMessage', pkg:'$IMPORTPATH'})
    ON CREATE SET f.file='cmd/worker/process.go', f.exported=true,
                  f.$PC='$COMMIT', f.$PD='$GDATE'
    ON MATCH  SET f.file='cmd/worker/process.go', f.exported=true,
                  f.$PC='$COMMIT', f.$PD='$GDATE'"
```

**Keep the `MERGE` map to identity properties only.** `MERGE` matches the whole
pattern, so a mutable property inside the map (say `file`) makes the pattern miss
when it changes and creates a **duplicate**. Identity goes in the map; everything
else goes in `SET`.

`pkg` is the **full import path**, `file` is **repo-relative**:

```
pkg  = example.com/acme/worker/internal/metrics/prometheus
file = internal/metrics/prometheus/prometheus.go
```

Writing the short path into `pkg` makes every later `MATCH` miss — and a `MATCH`
that binds nothing returns success.

## Writing — edges, and the trap that governs them

**`MERGE` over a pattern re-creates every node in it unless the whole pattern
already matches.** Two forms, and only two:

```bash
# (a) A node AND its first edge, both genuinely new — safe as one pattern:
kg "MERGE (p:Package {path:'cmd/worker'})-[:CONTAINS]->(f:Function {name:'Run', pkg:'$IP'})"

# (b) An edge between nodes that ALREADY EXIST — two MATCHes, then MERGE the
#     relationship ALONE. This is the form to reach for by default.
kg "MATCH (p:Package {path:'cmd/worker'})
    MATCH (f:Feature {name:'Worker orchestration'})
    MERGE (p)-[:IMPLEMENTS]->(f)"
```

**Never** pattern-`MERGE` over nodes that exist, and never issue two
pattern-`MERGE`s that share a node — each rebuilds the node it should have bound.
That is how one production graph acquired 249 stub `Package` nodes.

**Why the recipe has this shape — the relationship half of the trap.** Merging
the whole pattern `(a:L {…})-[:R]->(b:L2 {…})` over nodes that already exist
creates the relationship **and two fresh copies of its endpoints**, measured on
rmp 1.18.0 as `nodesCreated: 2` with both keys now held twice. That is why the
endpoints are `MATCH`ed and only the relationship is `MERGE`d.

Stamp edge provenance by **re-binding the relationship with an outgoing `MATCH`**,
never through the `MERGE`'s own binding (`ON MATCH SET`, or a `SET e` after the
`MERGE`):

```bash
kg "MATCH (p:Package {path:'cmd/worker'})
    MATCH (f:Feature {name:'Worker orchestration'})
    MERGE (p)-[:IMPLEMENTS]->(f)
    WITH DISTINCT p, f
    MATCH (p)-[e:IMPLEMENTS]->(f)
    SET e.$PC='$COMMIT', e.$PD='$GDATE'"
```

Both choices are load-bearing; do not "simplify" either away (SKILL.md § Silent
write hazards). A `MERGE` that matched, on a pair already joined by more than one
relationship in that direction — `PART_OF` beside `TESTS` is enough — binds a
relationship of the pair that may be another one, and its `SET` lands there,
`{"ok":true}`. And the re-binding `MATCH` is **outgoing**, from the stored source:
a property **removal** through an undirected or incoming pattern is dropped with
`{"ok":true}` and no `counters`. Verify an edge write with `e.<prop>` or
`properties(e)`, never with `RETURN e`, whose projected map can carry another
relationship's properties.

### Verify every write — the two oracles

1. **Read the `counters` block.** `nodesCreated > 0` on a `MERGE` you expected to
   match is the duplication trap firing. No `counters` key at all means nothing
   changed — which for a `MERGE` that matched is correct, and for a stamp that
   was supposed to write properties is a **silent failure**.
2. **Read the data back.** A `MATCH` binding nothing is not an error.

```bash
kg "MATCH (f:Function {name:'ProcessMessage', pkg:'$IP'})
    RETURN count(f) AS n, f.$PC AS stamped"   # n must be 1
```

## Writing — remove

```bash
# One edge
kg "MATCH (:Package {path:'old'})-[e:DEPENDS_ON]->(:Package {path:'gone'}) DELETE e"

# A node and all its edges
kg "MATCH (n:Spec {path:'docs/removed.md'}) DETACH DELETE n"

# Clear a property without deleting the element
kg "MATCH (t:Task {id:42}) REMOVE t.status"
```

**Dry-run every delete.** Nothing refuses a `DETACH DELETE` — there is no
read-only subcommand to hide behind. Run the same predicate as a `count(*)`
first, **and** prove the nodes that must survive score 0:

```bash
kg "MATCH (n:Package) WHERE n.name IS NULL RETURN count(n) AS will_delete"
kg "MATCH (n:Package) WHERE n.name IS NULL AND n.importPath IS NOT NULL
    RETURN count(n) AS must_be_zero"
```

## Reading — orientation

```bash
kg "CALL db.labels()"                      # cheapest label inventory
kg "CALL db.relationshipTypes()"           # cheapest predicate inventory

# Census with counts (a node may carry several labels, so UNWIND them)
kg "MATCH (n) UNWIND labels(n) AS l RETURN l, count(*) AS c ORDER BY c DESC"
kg "MATCH ()-[e]->() RETURN type(e) AS edge, count(*) AS c ORDER BY c DESC"
kg "MATCH (n) RETURN count(n) AS nodes"
kg "MATCH ()-[r]->() RETURN count(r) AS rels"
```

## Reading — answer the common questions

```bash
# Where is a symbol defined? (returns the file → build a file:line ref)
kg "MATCH (f:Function {name:'ExecuteQuery'}) RETURN f.pkg, f.file, f.exported"

# Which methods does a type expose?
kg "MATCH (t:Type {name:'Engine'})-[:HAS_METHOD]->(m:Method)
    RETURN m.name, m.file ORDER BY m.name"

# What does a package contain? (bind the NAMED node — see the integrity checks)
kg "MATCH (p:Package {path:'cypher/exec'})-[:CONTAINS]->(x)
    RETURN labels(x)[0] AS kind, count(*) AS c ORDER BY c DESC"

# What implements a feature, where is it specified, what tests it?
kg "MATCH (p:Package)-[:IMPLEMENTS]->(f:Feature {name:'WAL & Recovery'})
    OPTIONAL MATCH (f)-[:SPECIFIED_IN]->(s:Spec)
    RETURN p.path, collect(DISTINCT s.path) AS specs"

# Which tests cover a package?
kg "MATCH (p:Package {path:'store/wal'})-[:CONTAINS]->(t:Test)
    RETURN t.name, t.file ORDER BY t.name"

# A task's provenance: what commit closed it, what did it touch?
kg "MATCH (t:Task {id:2777})-[:IMPLEMENTED_IN]->(c:Commit)
    RETURN c.hash, c.message"
```

## Reading — traversal and blast radius

**There is no `search` subcommand any more** — a variable-length path is just a
statement:

```bash
# Directory nesting, transitively
kg "MATCH (a:Package {path:'cypher'})-[:CONTAINS*1..3]->(b:Package)
    RETURN DISTINCT b.path"

# Impact: what reaches a node within N hops
kg "MATCH (x)-[*1..3]->(target:Component {name:'mvcc.Horizon'})
    RETURN DISTINCT labels(x)[0] AS kind, coalesce(x.name, x.path, x.id) AS who"
```

Keep the hop bound small and the anchor selective: the **5-second statement
budget** is enforced by the server, and an unbounded traversal over 15k nodes
will hit it. A cancelled statement writes nothing.

## Schema — constraints and indexes

One statement per invocation: **a clause after schema DDL is silently
discarded**. Support is narrow — see `references/cli.md` for the measured matrix.

```bash
# Listings
kg "SHOW INDEXES"        # exactly one space; SHOW  INDEXES is a parse error
kg "SHOW CONSTRAINTS"

# Index: single-property, node-only, hash -> EQUALITY ONLY
kg "CREATE INDEX spec_path IF NOT EXISTS FOR (n:Spec) ON (n.path)"
kg "DROP INDEX spec_path IF EXISTS"      # ignore indexesRemoved; confirm with SHOW

# Constraint: IS UNIQUE and IS NOT NULL are the only two kinds
kg "CREATE CONSTRAINT spec_path_uniq IF NOT EXISTS FOR (n:Spec) REQUIRE n.path IS UNIQUE"
kg "CREATE CONSTRAINT task_id_present IF NOT EXISTS FOR (n:Task) REQUIRE n.id IS NOT NULL"
kg "DROP CONSTRAINT spec_path_uniq IF EXISTS"
```

**Check the data before declaring a constraint** — `CREATE CONSTRAINT` fails
(exit 1) on data that already violates it:

```bash
# Duplicates on a candidate identity key
kg "MATCH (n:Spec) WHERE n.path IS NOT NULL
    WITH n.path AS v, count(*) AS c WHERE c > 1
    RETURN count(v) AS dup_values, sum(c) AS dup_nodes"

# Null identities
kg "MATCH (n:Spec) WHERE n.path IS NULL RETURN count(n) AS null_identity"
```

### Prove an index decision, never assert it

```bash
kg "EXPLAIN MATCH (n:Spec {path:'docs/cypher.md'}) RETURN n.title"
```

`NodeByLabelScan` + `Filter` means no usable index; `NodeByIndexSeek` means it is
used. A range predicate (`>`, `<`) will show a label scan **even with an index**,
because the index is a hash. When a claim needs measuring rather than predicting,
use `PROFILE` and read `rows`, `timeNs`, `dbHits`.

## Sync after a commit — outline

For each path in `git diff --name-only <prev>..HEAD`:

1. Determine what changed (added / modified / deleted file; added/removed symbols;
   changed imports). Read the file only if the graph lacks what you need.
2. **Deleted file** → `DETACH DELETE` the symbol nodes declared in it and any
   node representing the file itself; remove now-dangling edges. Dry-run first.
3. **Added/modified** → one-statement upsert per node (identity in the `MERGE`
   map, the rest in `SET`), then link edges with `MATCH … MATCH … MERGE`.
4. Bump provenance on the touched `Package`/`Feature` nodes even when only their
   files changed, so "last confirmed" stays truthful.
5. If a new label, predicate or property appeared, update `./knowledge-model.md`
   in the same change — the dictionary, and the constraints/indexes sections if the
   new element has an identity key (`references/model.md`). **A change of
   vocabulary is the only thing that touches that file:** the nodes, edges, counts
   and provenance this sync wrote are graph content, and are never written into it.

Close the sync with the **adversarial audit** in `references/audit.md`, not a spot
check: parse the package's own declarations and diff the set **both ways** against
`MATCH (p:Package {path:…})-[:CONTAINS]->(x)`. Confirm the touched elements now
carry the new commit stamp (`$PC`).

## Dialect notes (measured)

- `||` string concatenation is **not** supported. Build strings in the shell, or
  `UNWIND` a literal list.
- `MATCH … MATCH …` as two clauses is accepted, and is the prescribed way to bind
  two existing nodes.
- `coalesce()` is available and useful where a label's identity key has drifted
  (e.g. `coalesce(l.name, l.id)`).
