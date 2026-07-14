#!/usr/bin/env python3
"""
Test 46: pattern predicates over parallel edges (multigraph) — regression guard
for the GoGraph v0.8.0 -> v0.8.1 Cypher correctness fix.

THE DEFECT (present in GoGraph v0.8.0, fixed in v0.8.1)
-------------------------------------------------------
Relationship-type matching inside pattern predicates (`WHERE (n)-[:TYPE]->()`,
`WHERE NOT (n)-[:TYPE]->()`) and pattern comprehensions inspected only the
FIRST stored relationship type between an ordered pair of nodes. When several
relationships of DIFFERENT types connect the same ordered pair — i.e. parallel
edges, which a multigraph allows and which this project's knowledge graph
actually contains — every non-first type was reported as ABSENT:

    (t)-[:TESTS]->(f)       stored 1st  -> predicates answered correctly
    (t)-[:COVERS]->(f)      stored 2nd  -> predicates wrongly said "no such edge"
    (t)-[:DEPENDS_ON]->(f)  stored 3rd  -> predicates wrongly said "no such edge"

The first-stored type kept behaving correctly, which is exactly why the bug
hid: only the non-first types lie. v0.8.1 tests EVERY relationship type of the
endpoint pair across the outgoing, incoming, undirected, variable-length and
comprehension paths.

WHY IT MATTERS HERE
-------------------
Groadmap's own knowledge graph IS a multigraph (SPEC/GRAPH.md): the same
ordered pair routinely carries more than one typed relationship — a test file
linked to its package by two different relationship types, for instance. The
negated pattern predicate `WHERE NOT (n)-[:TYPE]->()` is the canonical shape
used to find GAPS in the graph (a requirement with no test, a file with no
component), and `rmp web` exposes the same engine through its query bar. Under
v0.8.0 those gap audits silently reported false gaps.

WHAT THIS SUITE ASSERTS (all against the compiled ./bin/rmp)
------------------------------------------------------------
1. CONTROL — every parallel type is really stored and reported by `type(r)`.
   This assertion passes even on the broken v0.8.0, which is the point: it
   proves the fixture is sound, so the failures below are the real defect and
   not a fixture that never wrote the edges.
2. NEGATED predicate `WHERE NOT (n)-[:TYPE]->()` correct for EVERY outgoing
   type — first-stored AND non-first. Three types are used on one ordered pair,
   so a fix that only inspected the first two would still be caught.
3. POSITIVE predicate `WHERE (n)-[:TYPE]->()` correct for every type (this
   form was broken in the mirror direction: it reported no match).
4. `type(r)` over a BOUND relationship variable reports the correct type for
   parallel edges, both filtered and unfiltered.
5. INCOMING direction (`WHERE NOT (n)<-[:TYPE]-()`), which upstream lists among
   the fixed paths — plus the undirected, variable-length and pattern-
   comprehension paths.
6. The production query shape: a KG gap audit over parallel edges must name
   ONLY the genuinely incomplete file, not the complete one.

Every test also pins a genuinely ABSENT relationship type as a negative
control, so a hypothetical "fix" that made every pattern predicate match
everything would fail this suite too.

Isolation: GroadmapTestBase redirects HOME to a temporary directory, so the
scratch roadmap created here lives under that temp HOME. The real `groadmap`
roadmap is never touched. Teardown removes the roadmap explicitly via
`rmp roadmap remove` and then deletes the temp HOME.

Output shapes asserted here were confirmed against the compiled binary: a
write without RETURN emits {"ok": true}; a RETURN emits {"columns", "rows"}.
Node ids are non-deterministic and are never asserted.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


EXIT_OK = 0

# The ordered pair carrying the parallel edges, in the order the edges are
# stored. TESTS is the first-stored type: it behaved correctly even on the
# broken v0.8.0. COVERS and DEPENDS_ON are the non-first types — those are the
# ones that regressed. Three types are used deliberately: a partial fix that
# inspected only the first two would still be caught by DEPENDS_ON.
SOURCE_FILE = "internal/db/schema.go"
TEST_FILE = "internal/db/schema_test.go"
DB_PACKAGE = "internal/db"
UTILS_PACKAGE = "internal/utils"
UTILS_FILE = "internal/utils/paths.go"
DATABASE_SPEC = "SPEC/DATABASE.md"

# Test -> File: three parallel types, in stored order.
TEST_TO_FILE_TYPES = ["TESTS", "COVERS", "DEPENDS_ON"]

# File -> Package: two parallel types, in stored order. BELONGS_TO is first,
# DECLARED_IN is the non-first type the gap audit depends on.
FILE_TO_PACKAGE_TYPES = ["BELONGS_TO", "DECLARED_IN"]

# Relationship types that genuinely do not exist on the fixture's pairs.
# They are the negative control: the NOT predicate MUST match on them, and the
# positive predicate MUST NOT. Without these, a broken engine that answered
# "edge present" for everything would pass the assertions above.
ABSENT_TYPES = ["BENCHMARKS", "SUPERSEDES"]


class TestGraphParallelEdgePredicates:

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        # Dedicated scratch roadmap under the temporary HOME; the real
        # `groadmap` roadmap is never touched.
        self.roadmap = self.test.create_roadmap("kg-parallel-edge-audit")
        self._seed_knowledge_graph()

    def teardown_method(self):
        # Remove the scratch roadmap explicitly, then drop the temp HOME.
        self.test.run_cmd(["roadmap", "remove", self.roadmap], check=False)
        self.test.teardown()

    # ---- helpers -----------------------------------------------------

    def write(self, subcmd, query):
        result = self.test.run_cmd_json(["graph", subcmd, "-r", self.roadmap, "--query", query])
        assert result == {"ok": True}, (
            f"write without RETURN must emit {{'ok': true}}; {subcmd} {query!r} -> {result!r}")

    def read(self, query, subcmd="query"):
        return self.test.run_cmd_json(["graph", subcmd, "-r", self.roadmap, "--query", query])

    def col_list(self, query, col, subcmd="query"):
        """Ordered list of one column's values — the actual rows returned."""
        result = self.read(query, subcmd=subcmd)
        idx = result["columns"].index(col)
        return [row[idx] for row in result["rows"]]

    def col_set(self, query, col, subcmd="query"):
        return set(self.col_list(query, col, subcmd=subcmd))

    # ---- fixture -----------------------------------------------------

    def _seed_knowledge_graph(self):
        """A faithful slice of this project's own knowledge graph, including
        the parallel edges that the real graph contains."""
        self.write("create", f"CREATE (p:Package {{key:'{DB_PACKAGE}', layer:'persistence'}})")
        self.write("create", f"CREATE (p:Package {{key:'{UTILS_PACKAGE}', layer:'support'}})")
        self.write("create", f"CREATE (f:File {{key:'{SOURCE_FILE}', language:'go'}})")
        self.write("create", f"CREATE (f:File {{key:'{UTILS_FILE}', language:'go'}})")
        self.write("create", f"CREATE (t:Test {{key:'{TEST_FILE}', kind:'unit'}})")
        self.write("create", f"CREATE (s:Spec {{key:'{DATABASE_SPEC}', status:'approved'}})")

        # Parallel edges, ordered pair (Test) -> (File): the test file tests
        # the schema, covers it, and depends on it. Stored in this exact order;
        # TESTS is first, so only COVERS and DEPENDS_ON exercise the defect.
        for etype in TEST_TO_FILE_TYPES:
            self.write("create",
                       f"MATCH (t:Test {{key:'{TEST_FILE}'}}), (f:File {{key:'{SOURCE_FILE}'}}) "
                       f"CREATE (t)-[:{etype}]->(f)")

        # Parallel edges, ordered pair (File) -> (Package).
        for etype in FILE_TO_PACKAGE_TYPES:
            self.write("create",
                       f"MATCH (f:File {{key:'{SOURCE_FILE}'}}), (p:Package {{key:'{DB_PACKAGE}'}}) "
                       f"CREATE (f)-[:{etype}]->(p)")

        # The utils file is deliberately INCOMPLETE: it only BELONGS_TO its
        # package and has no DECLARED_IN edge. It is the one true gap that the
        # audit query in test_kg_gap_audit_over_parallel_edges must find.
        self.write("create",
                   f"MATCH (f:File {{key:'{UTILS_FILE}'}}), (p:Package {{key:'{UTILS_PACKAGE}'}}) "
                   f"CREATE (f)-[:BELONGS_TO]->(p)")

        # A plain, non-parallel edge: the control that the multigraph fix did
        # not break the ordinary single-edge case.
        self.write("create",
                   f"MATCH (f:File {{key:'{SOURCE_FILE}'}}), (s:Spec {{key:'{DATABASE_SPEC}'}}) "
                   f"CREATE (f)-[:IMPLEMENTS]->(s)")

    # ================================================================
    #  1. CONTROL — the parallel edges really are stored.
    # ================================================================

    def test_all_parallel_types_are_stored_and_reported(self):
        """Passes even on the broken v0.8.0 — that is precisely its job: it
        proves the fixture wrote every edge, so the pattern-predicate failures
        in the tests below are the engine's defect, not a missing edge."""
        types = self.col_set(
            f"MATCH (t:Test {{key:'{TEST_FILE}'}})-[r]->(f:File {{key:'{SOURCE_FILE}'}}) "
            f"RETURN type(r) AS t", "t")
        assert types == set(TEST_TO_FILE_TYPES), (
            f"the ordered pair {TEST_FILE} -> {SOURCE_FILE} must carry all "
            f"{len(TEST_TO_FILE_TYPES)} parallel types; expected "
            f"{sorted(TEST_TO_FILE_TYPES)}, store returned {sorted(types)}")

        count = self.col_list(
            f"MATCH (t:Test {{key:'{TEST_FILE}'}})-[r]->(f:File {{key:'{SOURCE_FILE}'}}) "
            f"RETURN count(r) AS c", "c")[0]
        assert count == len(TEST_TO_FILE_TYPES), (
            f"expected {len(TEST_TO_FILE_TYPES)} parallel edges on the pair, got {count}")

        pkg_types = self.col_set(
            f"MATCH (f:File {{key:'{SOURCE_FILE}'}})-[r]->(p:Package {{key:'{DB_PACKAGE}'}}) "
            f"RETURN type(r) AS t", "t")
        assert pkg_types == set(FILE_TO_PACKAGE_TYPES), (
            f"the ordered pair {SOURCE_FILE} -> {DB_PACKAGE} must carry "
            f"{sorted(FILE_TO_PACKAGE_TYPES)}, store returned {sorted(pkg_types)}")

    # ================================================================
    #  2. NEGATED pattern predicate — every outgoing type.
    # ================================================================

    def test_negated_pattern_predicate_correct_for_every_outgoing_type(self):
        """`WHERE NOT (n)-[:TYPE]->()` must be false for EVERY stored type.

        On v0.8.0 this held only for TESTS (the first-stored type); COVERS and
        DEPENDS_ON wrongly returned the node, because the evaluator inspected
        only the first relationship type of the endpoint pair.
        """
        for etype in TEST_TO_FILE_TYPES:
            rows = self.col_list(
                f"MATCH (t:Test {{key:'{TEST_FILE}'}}) WHERE NOT (t)-[:{etype}]->() "
                f"RETURN t.key AS k", "k")
            assert rows == [], (
                f"WHERE NOT (t)-[:{etype}]->() wrongly reported the edge as ABSENT: "
                f"returned {rows!r}, expected [] because {TEST_FILE} -[{etype}]-> "
                f"{SOURCE_FILE} exists (parallel-edge type-matching regression; "
                f"stored position {TEST_TO_FILE_TYPES.index(etype) + 1} of "
                f"{len(TEST_TO_FILE_TYPES)})")

        # Same, on the second parallel pair — DECLARED_IN is the non-first type.
        for etype in FILE_TO_PACKAGE_TYPES:
            rows = self.col_list(
                f"MATCH (f:File {{key:'{SOURCE_FILE}'}}) WHERE NOT (f)-[:{etype}]->() "
                f"RETURN f.key AS k", "k")
            assert rows == [], (
                f"WHERE NOT (f)-[:{etype}]->() wrongly reported the edge as ABSENT: "
                f"returned {rows!r}, expected [] because {SOURCE_FILE} -[{etype}]-> "
                f"{DB_PACKAGE} exists")

        # Negative control: a type that genuinely is not there MUST match the
        # NOT predicate. Without this, an engine that answered "present" to
        # every pattern predicate would pass the assertions above.
        for etype in ABSENT_TYPES:
            rows = self.col_list(
                f"MATCH (t:Test {{key:'{TEST_FILE}'}}) WHERE NOT (t)-[:{etype}]->() "
                f"RETURN t.key AS k", "k")
            assert rows == [TEST_FILE], (
                f"WHERE NOT (t)-[:{etype}]->() must match: no {etype} edge exists "
                f"from {TEST_FILE}; returned {rows!r}")

    # ================================================================
    #  3. POSITIVE pattern predicate — every outgoing type.
    # ================================================================

    def test_positive_pattern_predicate_correct_for_every_outgoing_type(self):
        """`WHERE (n)-[:TYPE]->()` must be true for EVERY stored type.

        The mirror of the negated form, and broken the same way on v0.8.0: the
        non-first types returned no rows at all.
        """
        for etype in TEST_TO_FILE_TYPES:
            rows = self.col_list(
                f"MATCH (t:Test {{key:'{TEST_FILE}'}}) WHERE (t)-[:{etype}]->() "
                f"RETURN t.key AS k", "k")
            assert rows == [TEST_FILE], (
                f"WHERE (t)-[:{etype}]->() failed to match an edge that EXISTS: "
                f"returned {rows!r}, expected [{TEST_FILE!r}] (parallel-edge "
                f"type-matching regression; stored position "
                f"{TEST_TO_FILE_TYPES.index(etype) + 1} of {len(TEST_TO_FILE_TYPES)})")

        for etype in FILE_TO_PACKAGE_TYPES:
            rows = self.col_list(
                f"MATCH (f:File {{key:'{SOURCE_FILE}'}}) WHERE (f)-[:{etype}]->() "
                f"RETURN f.key AS k", "k")
            assert rows == [SOURCE_FILE], (
                f"WHERE (f)-[:{etype}]->() failed to match an edge that EXISTS: "
                f"returned {rows!r}, expected [{SOURCE_FILE!r}]")

        # The ordinary, non-parallel edge must still work.
        rows = self.col_list(
            f"MATCH (f:File {{key:'{SOURCE_FILE}'}}) WHERE (f)-[:IMPLEMENTS]->() "
            f"RETURN f.key AS k", "k")
        assert rows == [SOURCE_FILE], (
            f"the single (non-parallel) IMPLEMENTS edge must still match: {rows!r}")

        # Negative control: an absent type must NOT match.
        for etype in ABSENT_TYPES:
            rows = self.col_list(
                f"MATCH (t:Test {{key:'{TEST_FILE}'}}) WHERE (t)-[:{etype}]->() "
                f"RETURN t.key AS k", "k")
            assert rows == [], (
                f"WHERE (t)-[:{etype}]->() must not match: no {etype} edge exists "
                f"from {TEST_FILE}; returned {rows!r}")

    # ================================================================
    #  4. type(r) over a BOUND relationship variable.
    # ================================================================

    def test_type_of_bound_relationship_variable_over_parallel_edges(self):
        """A bound relationship variable filtered by type must report THAT
        type — on v0.8.0 type(r) could report the wrong type for parallel
        edges, because the type lookup resolved against the pair's first
        stored relationship rather than the matched one."""
        for etype in TEST_TO_FILE_TYPES:
            result = self.read(
                f"MATCH (t:Test {{key:'{TEST_FILE}'}})-[r:{etype}]->(f:File {{key:'{SOURCE_FILE}'}}) "
                f"RETURN type(r) AS t, f.key AS k")
            rows = result["rows"]
            assert rows == [[etype, SOURCE_FILE]], (
                f"MATCH ...-[r:{etype}]->... RETURN type(r) must yield exactly "
                f"[[{etype!r}, {SOURCE_FILE!r}]]; got {rows!r}")

        # Unfiltered: the bound variable must enumerate every parallel type
        # exactly once — no type reported twice, none missing.
        types = self.col_list(
            f"MATCH (t:Test {{key:'{TEST_FILE}'}})-[r]->(f:File {{key:'{SOURCE_FILE}'}}) "
            f"RETURN type(r) AS t", "t")
        assert sorted(types) == sorted(TEST_TO_FILE_TYPES), (
            f"an unfiltered bound relationship variable must report each parallel "
            f"type exactly once; expected {sorted(TEST_TO_FILE_TYPES)}, got {sorted(types)}")

        # A type that is absent binds nothing.
        for etype in ABSENT_TYPES:
            result = self.read(
                f"MATCH (t:Test {{key:'{TEST_FILE}'}})-[r:{etype}]->(f:File) "
                f"RETURN type(r) AS t")
            assert result["rows"] == [], (
                f"no {etype} edge exists, yet the pattern bound one: {result['rows']!r}")

    # ================================================================
    #  5. INCOMING direction.
    # ================================================================

    def test_pattern_predicate_correct_for_every_incoming_type(self):
        """Upstream lists the INCOMING path among those fixed. Seen from the
        source file, the three parallel edges are incoming; the non-first ones
        must not be reported as absent."""
        for etype in TEST_TO_FILE_TYPES:
            rows = self.col_list(
                f"MATCH (f:File {{key:'{SOURCE_FILE}'}}) WHERE NOT (f)<-[:{etype}]-() "
                f"RETURN f.key AS k", "k")
            assert rows == [], (
                f"WHERE NOT (f)<-[:{etype}]-() wrongly reported the INCOMING edge as "
                f"ABSENT: returned {rows!r}, expected [] because {TEST_FILE} "
                f"-[{etype}]-> {SOURCE_FILE} exists (stored position "
                f"{TEST_TO_FILE_TYPES.index(etype) + 1} of {len(TEST_TO_FILE_TYPES)})")

            rows = self.col_list(
                f"MATCH (f:File {{key:'{SOURCE_FILE}'}}) WHERE (f)<-[:{etype}]-() "
                f"RETURN f.key AS k", "k")
            assert rows == [SOURCE_FILE], (
                f"WHERE (f)<-[:{etype}]-() failed to match an INCOMING edge that "
                f"EXISTS: returned {rows!r}, expected [{SOURCE_FILE!r}]")

        # The package sees BELONGS_TO and DECLARED_IN as incoming from the file.
        for etype in FILE_TO_PACKAGE_TYPES:
            rows = self.col_list(
                f"MATCH (p:Package {{key:'{DB_PACKAGE}'}}) WHERE NOT (p)<-[:{etype}]-() "
                f"RETURN p.key AS k", "k")
            assert rows == [], (
                f"WHERE NOT (p)<-[:{etype}]-() wrongly reported the INCOMING edge as "
                f"ABSENT: returned {rows!r}, expected [] because {SOURCE_FILE} "
                f"-[{etype}]-> {DB_PACKAGE} exists")

        # Negative control on the incoming side.
        for etype in ABSENT_TYPES:
            rows = self.col_list(
                f"MATCH (f:File {{key:'{SOURCE_FILE}'}}) WHERE NOT (f)<-[:{etype}]-() "
                f"RETURN f.key AS k", "k")
            assert rows == [SOURCE_FILE], (
                f"WHERE NOT (f)<-[:{etype}]-() must match: no incoming {etype} edge "
                f"exists on {SOURCE_FILE}; returned {rows!r}")

    # ================================================================
    #  6. Undirected, variable-length and comprehension paths.
    # ================================================================

    def test_undirected_and_variable_length_and_comprehension_paths(self):
        """The remaining paths upstream states the fix covers."""
        # Undirected: the file is connected to the test by all three types,
        # regardless of direction.
        for etype in TEST_TO_FILE_TYPES:
            rows = self.col_list(
                f"MATCH (f:File {{key:'{SOURCE_FILE}'}}) WHERE (f)-[:{etype}]-() "
                f"RETURN f.key AS k", "k")
            assert rows == [SOURCE_FILE], (
                f"undirected (f)-[:{etype}]-() failed to match an existing edge: {rows!r}")

            rows = self.col_list(
                f"MATCH (f:File {{key:'{SOURCE_FILE}'}}) WHERE NOT (f)-[:{etype}]-() "
                f"RETURN f.key AS k", "k")
            assert rows == [], (
                f"undirected NOT (f)-[:{etype}]-() wrongly reported an existing edge "
                f"as absent: {rows!r}")

        for etype in ABSENT_TYPES:
            rows = self.col_list(
                f"MATCH (f:File {{key:'{SOURCE_FILE}'}}) WHERE NOT (f)-[:{etype}]-() "
                f"RETURN f.key AS k", "k")
            assert rows == [SOURCE_FILE], (
                f"undirected NOT (f)-[:{etype}]-() must match an absent type: {rows!r}")

        # Variable-length inside a pattern predicate, over a non-first type.
        for etype in TEST_TO_FILE_TYPES:
            rows = self.col_list(
                f"MATCH (t:Test {{key:'{TEST_FILE}'}}) WHERE (t)-[:{etype}*1..2]->() "
                f"RETURN t.key AS k", "k")
            assert rows == [TEST_FILE], (
                f"variable-length (t)-[:{etype}*1..2]->() failed to match an existing "
                f"edge: {rows!r}")

        for etype in ABSENT_TYPES:
            rows = self.col_list(
                f"MATCH (t:Test {{key:'{TEST_FILE}'}}) WHERE (t)-[:{etype}*1..2]->() "
                f"RETURN t.key AS k", "k")
            assert rows == [], (
                f"variable-length (t)-[:{etype}*1..2]->() must not match an absent "
                f"type: {rows!r}")

        # Pattern comprehension over each parallel type: it must project the
        # endpoint reached through THAT type.
        for etype in TEST_TO_FILE_TYPES:
            rows = self.col_list(
                f"MATCH (t:Test {{key:'{TEST_FILE}'}}) "
                f"RETURN [(t)-[:{etype}]->(x) | x.key] AS reached", "reached")
            assert rows == [[SOURCE_FILE]], (
                f"pattern comprehension over [:{etype}] must project [{SOURCE_FILE!r}]; "
                f"got {rows!r} (stored position "
                f"{TEST_TO_FILE_TYPES.index(etype) + 1} of {len(TEST_TO_FILE_TYPES)})")

        for etype in ABSENT_TYPES:
            rows = self.col_list(
                f"MATCH (t:Test {{key:'{TEST_FILE}'}}) "
                f"RETURN [(t)-[:{etype}]->(x) | x.key] AS reached", "reached")
            assert rows == [[]], (
                f"pattern comprehension over the absent type [:{etype}] must project "
                f"an empty list; got {rows!r}")

    # ================================================================
    #  7. The production shape: a knowledge-graph gap audit.
    # ================================================================

    def test_kg_gap_audit_over_parallel_edges(self):
        """The exact query shape used to audit this project's knowledge graph:
        "which files are missing a DECLARED_IN edge?".

        internal/db/schema.go HAS one — but only as the SECOND (non-first)
        relationship on its ordered pair to internal/db. internal/utils/paths.go
        genuinely lacks it. A correct engine names exactly the incomplete file;
        v0.8.0 also named the complete one, i.e. it invented a gap that does
        not exist. This is the false-positive that made KG gap audits unsound.
        """
        gaps = self.col_list(
            "MATCH (f:File) WHERE NOT (f)-[:DECLARED_IN]->() RETURN f.key AS k", "k")
        assert gaps == [UTILS_FILE], (
            f"the DECLARED_IN gap audit must name ONLY {UTILS_FILE!r} (the file that "
            f"genuinely lacks the edge); it returned {gaps!r}. {SOURCE_FILE!r} does "
            f"have a DECLARED_IN edge — it is simply not the first-stored type on its "
            f"ordered pair, which is exactly the parallel-edge defect.")

        # The complementary audit: which files DO declare their package.
        declared = self.col_list(
            "MATCH (f:File) WHERE (f)-[:DECLARED_IN]->() RETURN f.key AS k", "k")
        assert declared == [SOURCE_FILE], (
            f"the positive audit must name exactly {SOURCE_FILE!r}; got {declared!r}")

        # A gap audit for a type nothing has: every file must be reported.
        all_files = self.col_set(
            "MATCH (f:File) WHERE NOT (f)-[:BENCHMARKS]->() RETURN f.key AS k", "k")
        assert all_files == {SOURCE_FILE, UTILS_FILE}, (
            f"no file has a BENCHMARKS edge, so the audit must report both files; "
            f"got {sorted(all_files)}")


def _run_all():
    instance_cls = TestGraphParallelEdgePredicates
    method_names = [m for m in dir(instance_cls) if m.startswith("test_")]
    passed = 0
    failed = 0
    failures = []
    for m in method_names:
        instance = instance_cls()
        instance.setup_method()
        try:
            getattr(instance, m)()
            passed += 1
            print(f"✓ {m}")
        except AssertionError as exc:
            failed += 1
            failures.append((m, exc))
            print(f"✗ {m}")
        except Exception as exc:  # noqa: BLE001
            failed += 1
            failures.append((m, exc))
            print(f"✗ {m} (error)")
        finally:
            instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Graph parallel-edge pattern-predicate tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
