# Adversarial fidelity audit, integrity checks & hygiene

Run this on a sync, refresh, or explicit audit request. The goal is to *prove* the
graph faithfully represents the code by trying to find where it does not, then
reporting every divergence. Fidelity is measured by concrete, verifiable criteria
— never by an aspirational "looks complete".

Two halves, and **both** are required:

- **Fidelity** — does the graph match the code? (§ Fidelity criteria)
- **Integrity** — is the graph internally sound, or has it duplicated itself?
  (§ Integrity checks). This half is new-in-importance because the engine's
  pattern-`MERGE` invites duplication silently.

## Integrity checks — run these FIRST

These are cheap, they need no ground truth from the repo, and they catch the
failure mode this engine actually produces. Run them before the fidelity diff:
a duplicated node makes every fidelity count ambiguous.

```bash
kg() { rmp graph client -r "$R" "${SOCK_ARG[@]}" --query "$1"; }

# 1. Stub nodes — created by a pattern-MERGE that should have matched.
#    Symptom: the identity property is set but the descriptive ones are null.
kg "MATCH (n:Package) WHERE n.name IS NULL RETURN count(n) AS stubs"

# 2. Do the stubs carry edges? If so, queries SPLIT across the real and stub node.
kg "MATCH (n:Package) WHERE n.name IS NULL MATCH (n)-[r]-() 
    RETURN type(r) AS t, count(*) AS c ORDER BY c DESC"

# 3. Duplicated identity — per label, on the model's declared identity key.
kg "MATCH (n:L) WHERE n.p IS NOT NULL WITH n.p AS v, count(*) AS c WHERE c > 1
    RETURN count(v) AS dup_values, sum(c) AS dup_nodes"

# 4. Which values, and how badly (find the worst offender)
kg "MATCH (n:L) WITH n.p AS v, count(*) AS c WHERE c > 1 RETURN v, c ORDER BY c DESC"

# 5. Null identity — damage, or a deliberate second keying? Decide, do not assume.
kg "MATCH (n:L) WHERE n.p IS NULL RETURN count(n) AS null_identity"

# 6. Undocumented labels — drift between the graph and the dictionary
kg "CALL db.labels()"          # diff against ./knowledge-model.md
kg "CALL db.relationshipTypes()"

# 7. Provenance gaps — $PC/$PD are the names ./knowledge-model.md declares
kg "MATCH (n) WHERE n.$PC IS NULL RETURN labels(n)[0] AS l, count(*) AS c ORDER BY c DESC"

# 8. A SECOND provenance set — what a write made with another project's spelling
#    leaves behind. It exits 0, so nothing else reports it. List every property
#    key in the graph and look for commit/date-like names other than $PC/$PD:
kg "MATCH (n) UNWIND keys(n) AS k RETURN k, count(*) AS c ORDER BY k"
kg "MATCH ()-[e]->() UNWIND keys(e) AS k RETURN k, count(*) AS c ORDER BY k"
#    then count the elements carrying a suspect name <X> (e.g. gitCommit,
#    last_commit, commit_hash) that the model does not declare:
kg "MATCH (n) WHERE n.<X> IS NOT NULL RETURN labels(n)[0] AS l, count(*) AS c"
kg "MATCH ()-[e]->() WHERE e.<X> IS NOT NULL RETURN type(e) AS t, count(*) AS c"
```

A property the model gives a different meaning (for example a `Release`'s own
`commit`) is content, not a second provenance set; judge each against the
dictionary, never by name alone.

**Read a split like this as a defect, not untidiness.** Measured on one
production graph (2026-09-08): 249 of 369 `Package` nodes were null-`name` stubs carrying 234
edges — 219 `CONTAINS` out, 15 `TOUCHES` in. The named `cypher/exec` node held
**1086** `CONTAINS` edges while its 64 stubs held **63** between them, so "what
does `cypher/exec` contain?" answered 1086 or 1149 depending on which node bound.
One path, `graph/index/hash`, had **115** nodes.

**Report each integrity finding as a number with the query that produced it** — in
the audit report, which is where the number belongs. Into `./knowledge-model.md`
carry only the **query**, beside the constraint it breaks; the count itself is
graph content and stays out of the file (the *form, never content* rule in
`references/model.md`).

### Repairing a split, safely

Order matters, and every step is dry-run first.

1. **Characterise** — how many stubs, which edge types, which direction, and is
   there exactly one named node per identity value to re-home onto?
2. **Re-home the edges** onto the named node, with `MATCH … MATCH … MERGE` —
   never a pattern-`MERGE`, which is what caused this.
3. **Prove the survivors** score zero on the delete predicate:
   ```bash
   kg "MATCH (n:Package) WHERE n.name IS NULL RETURN count(n) AS will_delete"
   kg "MATCH (n:Package) WHERE n.name IS NULL AND n.importPath IS NOT NULL
       RETURN count(n) AS must_be_zero"
   ```
4. **Delete** the stubs, then **re-count** edges and state the delta as a number.
5. **Then** create the UNIQUE constraint, so the engine prevents a recurrence, and
   confirm with `SHOW CONSTRAINTS` plus a probe that a duplicate insert is rejected.

## Fidelity criteria

Check the graph against the code for each modelled tier (per
`./knowledge-model.md`; a tier the model defines but the graph does not populate
yet — the census in `references/cypher.md` says which — is a known gap to list,
not a divergence to diff):

1. **Files & directories** — every source file the model covers has exactly one
   node with the correct repo-relative path; no node points at a path that no
   longer exists; the `CONTAINS` structure mirrors the real tree.
2. **Symbols** — every top-level declaration the model covers (`Type`,
   `Function`, `Method`, `Test`, `Benchmark`, `FuzzTarget`, `Example`) exists with
   correct `pkg`/`file`/`exported`, and no phantom node remains for deleted code.
   Remember `pkg` is the **full import path**; a short path there makes every
   `MATCH` miss.
3. **Membership** — every symbol has its `CONTAINS`/`HAS_METHOD` edge, and methods
   link to the right receiver `Type`.
4. **Dependencies** — every import between packages is an edge, and every such
   edge corresponds to a real import.
5. **Provenance** — every node and edge carries the provenance pair that
   `./knowledge-model.md` declares (or is deliberately unstamped where the model
   allows it), none is stamped with a commit newer than `HEAD`, and **no element
   carries a second provenance set** under names the model does not declare.

## Method — compare graph vs code, BOTH directions

Adversarial means checking both inclusions, because either can fail:

- **Code → graph (missing):** enumerate ground truth from the repo (`git ls-files
  '*.go'`; a declaration parser; an import scan), query the graph for each, report
  what is absent.
- **Graph → code (stale/phantom):** list the graph's nodes and verify each still
  exists in the code; report anything that does not.

**A spot check is not an audit.** Parse the package's own declarations and diff
the set both ways — that method has found genuinely missing declarations *and*
proved the graph claimed nothing that does not exist. Prefer a script over
eyeballing:

```bash
# Ground truth
git ls-files '*.go' | sort > /tmp/ka_repo.txt
# Graph truth
kg "MATCH (p:Package {path:'cypher/exec'})-[:CONTAINS]->(x) RETURN x.file" \
  | python3 -c "import sys,json;[print(r[0]) for r in json.load(sys.stdin)['rows'] if r[0]]" \
  | sort -u > /tmp/ka_graph.txt
echo '--- in repo, missing from graph ---'; comm -23 /tmp/ka_repo.txt /tmp/ka_graph.txt
echo '--- in graph, gone from repo ---';    comm -13 /tmp/ka_repo.txt /tmp/ka_graph.txt
```

Two harness traps that have corrupted this exact comparison before:

- **An empty result is not evidence of absence.** Verify the query ran (a
  non-zero count somewhere) before concluding a set is empty.
- **`IFS=$'\t' read` collapses consecutive tabs**, so an empty field vanishes and
  every later field shifts left — it once wrote file paths into a `recv`
  property. Drive multi-field records from python or awk, never from `read`.

Count-level sanity first (the census queries in `references/cypher.md`), then
element-level diffs for anything that looks off.

## Report format

Report divergences grouped by tier, most material first. For each: what the graph
says, what the code says, the number, and the fix applied or proposed. Close with
the verifiable criteria that now hold and the tiers still deliberately
unpopulated. Then apply the fixes and re-stamp provenance.

State plainly what was **not** checked. An audit that implies full coverage it did
not have is worse than a narrow one that admits its scope.

## Hygiene

The graph holds **only true, validated facts** about the project. Do not pollute
it with data the user did not ask for, define, or accept. When a fallback file
read teaches you something real about the code, write it back — that is validated
fact. Speculation, TODO-style guesses, and convenience scratch data do not belong
in the graph. If a node or edge turns out to be wrong, delete it rather than leave
it to mislead a future query.
