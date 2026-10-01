# The graph model & `./knowledge-model.md`

## The modelling paradigm

The graph is a **Label-Property Graph (LPG)**: nodes carry one or more *labels*
and a map of *properties*; directed, typed *predicates* (edges) connect them and
may also carry properties. Keep node identity small and stable; put descriptive
data in properties.

## `./knowledge-model.md` is the canonical shape

Each project keeps a `./knowledge-model.md` at its root. It is the contract
between this skill and the graph, **written exclusively by this skill** (SKILL.md
§ Scope), and it carries **four** things and nothing besides:

1. **The label dictionary** — every node label, what it *means*, and its properties.
2. **The predicate dictionary** — every edge type, its endpoints, and what the
   relationship *asserts*.
3. **The constraints** — the model's integrity rules, the DDL that declares each,
   and which of them this engine can express.
4. **The recommended indexes** — which indexes serve the way the graph is
   actually queried, and how each recommendation is proven.

Every one of the four is a *definition*. None of them is a fact about the data:
the data is in the graph.

**Read `./knowledge-model.md` at the start of any non-trivial task** so your
statements match the real shape. Three hard rules:

1. **The model must always conform to the live graph** — it is the best possible
   description of the graph's actual structure. When you introduce a new label,
   predicate or property, update the model in the *same* change.
2. **Form, never content.** The file holds **only the definitions of the graph's
   labels and predicates** — the vocabulary, its shape, its constraints, its index
   recommendations. **The information itself lives only in the graph, never in the
   file.** The test that settles every case: **if a fact in the file would go stale
   when the graph changes without the vocabulary changing, it does not belong in
   the file.** So: no node listings, no node or edge counts, no census, no
   component, feature or package inventory, no example rows presented as project
   facts, no measured selectivity, no violation count — nothing a sync would have
   to re-synchronise. Every one of those is one statement away, so the entry names
   **the query that answers it** instead of the answer. Where a candidate is
   genuinely ambiguous, keep it out of the file and name the query.
3. **A definition may precede its data.** The vocabulary may define a label or
   predicate the graph does not populate yet; defining one is form, and costs the
   graph nothing. Do **not** track *which* definitions are populated — that is
   content, and the census in `references/cypher.md` answers it exactly.

### The dictionaries exist so an LLM can know the shape objectively

This is their purpose: a reader must be able to compose a correct statement
**without** exploring the graph first. So each entry states meaning, not just
name.

**Label entries** carry: the label, one sentence of what it represents, its
**identity property (and that property's type)**, and its remaining properties
with their meaning. Property *type* is part of the contract, not formatting —
`MERGE (t:Task {id: 2494})` and `MERGE (t:Task {id: '2494'})` bind **different
nodes**, which is exactly how duplicate stubs appear.

**Predicate entries** carry: the type, its endpoint labels and direction, and what
it asserts. Where one predicate is reused across different endpoint pairs, give it
one row per pair — `CONTAINS` meaning "directory nesting" between two `Package`
nodes is a different assertion from `CONTAINS` meaning "declares this symbol".

Record **divergence honestly**. Where live nodes disagree with the documented
shape, say so on the entry: what the divergence *is*, how to reach the odd ones
(e.g. `coalesce(l.name, l.id)`), and the query that measures it — never the number
that query returned. A dictionary that describes an ideal the data does not meet
sends every future query to the wrong property.

At minimum the model must be able to represent: files and directories; code
symbols (functions, types, methods) with definition/membership predicates;
dependencies (imports, and calls where derivable); and the architectural level —
packages, components, features, specs, tests.

## Provenance convention

Every node and edge carries the commit at which it was last confirmed and that
commit's ISO date, under **property names the project's `./knowledge-model.md`
declares** — the model is canonical, and this skill carries no default spelling
(one project may declare `gitCommit`/`gitDate`, another `last_commit`/
`last_commit_date`). Stamp them on every write
(see `references/cypher.md`). When bootstrapping a model, declare the pair in its
conventions and ask the user to approve the names with the rest of the shape. A
stamp asserts re-verification at that commit; reconciling a node with the model
is not re-verification and does not advance it.

## Constraints — what the engine can enforce

**The engine supports exactly two kinds, on a single node property:**

| Kind | DDL | Reported type |
|---|---|---|
| Uniqueness | `CREATE CONSTRAINT <n> FOR (x:L) REQUIRE x.p IS UNIQUE` | `UNIQUE` |
| Presence | `CREATE CONSTRAINT <n> FOR (x:L) REQUIRE x.p IS NOT NULL` | `NOT_NULL` |

Composite `NODE KEY`, `ASSERT exists(...)` and type constraints (`IS :: STRING`)
are all rejected with exit 1. **UNIQUE is genuinely enforced** — a
violating write is rejected — which makes it the strongest defence available
against the pattern-`MERGE` duplication trap.

### How to record a constraint in the model

Every label with an identity property earns a **declared UNIQUE constraint** on
it: that is what "identity" means, and the engine can hold the line. State, per
constraint:

| Field | Why |
|---|---|
| Label + property + kind | the rule itself |
| The DDL that creates it | so it is applicable, not aspirational |
| **`SHOW CONSTRAINTS`** as the way to read whether the engine holds it | its status today is live state, not form: the query belongs in the file, the answer does not |
| For one the data cannot yet satisfy: the **integrity query** that measures the gap | so the gap is one statement away, with no stale number in the file |

**Never silently omit a constraint because the data breaks it.** `CREATE
CONSTRAINT` fails (exit 1) against violating data, so a dirty key cannot be
enforced today — but it is still the model's rule. Record the rule, and beside it
the integrity query that measures the gap; the count that query returns stays in
the graph. A constraint absent from the model is a rule nobody knows about; a
constraint with its measuring query beside it is a repair waiting to be scheduled.

Check before declaring (see `references/audit.md` for the full integrity sweep):

```bash
kg "MATCH (n:L) WHERE n.p IS NOT NULL WITH n.p AS v, count(*) AS c WHERE c > 1
    RETURN count(v) AS dup_values, sum(c) AS dup_nodes"
kg "MATCH (n:L) WHERE n.p IS NULL RETURN count(n) AS null_identity"
```

A null identity is not automatically damage — it can be a modelling choice (a
label that keys on `ref` instead of `id` for some nodes). Decide which it is, and
say so on the entry, before proposing `IS NOT NULL`.

## Recommended indexes — derived from usage, proven by EXPLAIN

**The engine's index is single-property, node-only, and hash — therefore
equality-only.** A range predicate (`>`, `<`) ignores it and falls back to a label
scan. A `btree` index can be declared (`OPTIONS {indexType:'btree'}`), but
measured at rmp 1.17.3 the planner used it for neither an equality nor a range
lookup, so recommend the default hash index. So an index is worth recommending exactly where the skill issues an
**equality lookup on one property**, which is what every identity lookup in
`references/cypher.md` is.

### The method — three steps, in this order

1. **Take the query shapes from how the graph is actually used** — the identity
   lookups in `references/cypher.md`, not hypothetical ones. An index that serves
   no statement this skill issues is waste.
2. **Rank by label size × selectivity**, both measured:

   ```bash
   kg "MATCH (n:L) WHERE n.p IS NOT NULL
       RETURN count(n) AS tot, count(DISTINCT n.p) AS dv"
   ```

   A large label pays most: an index turns a scan of every node of that label
   into a seek. High selectivity means the seek returns few rows. A label of 80
   nodes does not need an index however selective it is — the scan is already
   cheap.
3. **Prove it with `EXPLAIN`**, before and after. `NodeByLabelScan` + `Filter`
   → no usable index; `NodeByIndexSeek` → in use. Never assert a benefit that a
   plan has not shown.

**What lands in `./knowledge-model.md` is the recommendation, not the
measurement:** the label, the property, the DDL that creates the index, and the
plan operator that proves it — plus the two queries above, so anyone can
re-measure. The node counts and selectivities themselves are graph content and go
stale on the next sync — the *form, never content* rule above.

### Worked measurement (a mid-sized Go codebase, 2026-09-08 — 15447 nodes, 19507 edges, 0 indexes)

Worked here, in this skill, to show the method in numbers. These figures measure
one graph at one moment: read them as an example of ranking, and never copy them
into a project's model file.

| Label | Property | Nodes | Distinct | Selectivity | Verdict |
|---|---|---:|---:|---:|---|
| `Test` | `name` | 4796 | 4719 | 98.4% | **index — highest value** |
| `Method` | `name` | 3666 | 1633 | 44.5% | **index** — 2.2 rows/seek beats 3666 scanned |
| `Function` | `name` | 3145 | 2907 | 92.4% | **index** |
| `Type` | `name` | 1075 | 980 | 91.2% | **index** |
| `Commit` | `hash` | 748 | 741 | 99.1% | index |
| `Task` | `id` | 499 | 494 | 99.0% | index |
| `Package` | `path` | 369 | 122 | 33.1% | index — **but see below** |
| `Benchmark` | `name` | 190 | 190 | 100% | skip — label too small to pay |
| `Feature` | `name` | 87 | 87 | 100% | skip |
| `Spec` | `path` | 83 | 83 | 100% | skip |
| `Component` | `name` | 79 | 79 | 100% | skip |
| `DSTScenario` | `id` | 57 | 57 | 100% | skip |

**`Package.path`'s 33.1% is an artefact, not a property of the model.** 249 of the
369 nodes are null-`name` stubs produced by the pattern-`MERGE` trap; on the 122
real packages the key is unique. Diagnose a low selectivity before designing
around it — here the answer is to repair the data and declare the UNIQUE
constraint, not to reject the index.

**A composite lookup cannot be indexed.** `Function {name, pkg}` is an equality
match on two properties, and composite indexes are unsupported — so index the
**more selective single property** (`name`) and let the engine filter the rest.

## Bootstrap — when `./knowledge-model.md` does NOT exist

Only for a project with no model yet. Do **not** silently invent a model.

1. **Survey exhaustively.** Walk the project's structure, components, entry
   points, specs and tests until you understand it as a whole.
2. **Propose a shape.** Draft the labels, predicates and properties that fit what
   you found, with their identity keys and types, plus the constraints and the
   indexes those keys imply.
3. **Stop for approval — mandatory.** Present the proposed model and **wait**. Do
   not materialise it or populate the graph until the user approves. Only then
   write the file, populate, and create the schema.

## Evolving the model

When a sync or a fallback reveals a structure the model does not capture:

- **A genuinely new kind of thing** (new label, predicate or property) → add it to
  the dictionary with its meaning, identity key and type; if it has an identity
  key, add the matching UNIQUE constraint entry and consider the index by the
  method above; then populate it.
- **Something that merely fills a definition the graph had not populated** →
  populate it; the definition already covers it, so the file does not change.
- **A label the graph has but the model does not** → this is drift, and it is
  common. Count it (`CALL db.labels()` against the dictionary) and either document
  it or delete it — never leave it unaccounted.

Never let the model and the graph drift apart: a statement that fails because the
model described something the graph lacks (or vice-versa) is a fidelity defect —
fix it, then answer.
