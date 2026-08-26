#!/usr/bin/env python3
"""
Test 56: the relationship-READ direction contract (rmp task #288).

End-to-end backstop for SPEC/GRAPH.md § Relationship Read Direction, against the
compiled ./bin/rmp and a real graph store.

GoGraph recovers a bound relationship's type and endpoints by probing the stored
topology rather than by being told which way the traversal walked. The probe is
right whenever a node pair carries an edge one way only. On a pair carrying edges
BOTH ways it is wrong: the reverse leg of an incoming or undirected traversal is
hydrated from the FORWARD pair, so the read reports another relationship's type
and the reversed orientation.

The consequences reach past a mislabelled column, which is why the rule is keyed
on any expression use rather than on projection alone:

  - `RETURN type(e)` names the wrong relationship, and the reverse edge's own
    type never appears at all.
  - `WHERE type(e) = '...'` is evaluated against the corrupted value, so a row
    that genuinely matches is silently DROPPED and nothing reaches the caller.
  - `SET n.p = type(e)` PERSISTS the corrupted value while reporting success.

Correcting the value in Groadmap was measured first and is impossible: for
`type(e)` and `startNode(e).key` the consumer receives a bare string carrying no
relationship identity at all. Groadmap therefore REFUSES the read, before the
graph store is opened, rather than answering it with a mislabelled edge.

EVERY fixture in this module is a BIDIRECTIONAL pair whose two edges carry
DIFFERENT types. That is deliberate on both counts. A one-way pair resolves
correctly with or without the fix, so it cannot tell the two apart; and a
two-way pair whose edges share a type hides the most visible symptom, because
the forward type the engine substitutes happens to equal the reverse type it
should have reported. The live groadmap knowledge graph is in exactly that
second shape today, which is why the defect went unnoticed there.

The suite pins both halves of the contract: every shape that must be refused
writes nothing and answers nothing, and every shape that must keep working —
outgoing from either endpoint, the UNION-ALL rewrite, an anonymous relationship,
a named path, a variable-length hop, a node write, and `graph delete` — is
confirmed by reading its result back. A fix that refused too much would fail
this suite exactly as one that refused too little.
"""

import inspect
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


# The read rule's own phrase. The subcommand name varies — the rule covers
# `query`, `search` and `update`'s right-hand side — so the verb is what
# distinguishes it from the write rule's "cannot write relationship".
READ_REFUSAL = 'cannot read relationship "e"'
WRITE_REFUSAL = "cannot write relationship"


class TestGraphRelationshipReadDirection:
    """SPEC/GRAPH.md § Relationship Read Direction, rmp task #288."""

    SPEC_KEY = "graph-read-direction"
    TEST_KEY = "test_56_graph_read_direction.py"
    CODE_PATH = "internal/cypherguard/relread.go"

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        for query in [
            f"CREATE (:Spec {{key:'{self.SPEC_KEY}'}}), (:Test {{key:'{self.TEST_KEY}'}}), "
            f"(:Code {{key:'{self.CODE_PATH}'}})",
            # The two-way pair, with DIFFERENT types in the two directions.
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}}), (v:Test {{key:'{self.TEST_KEY}'}}) "
            "MERGE (s)-[:VERIFIED_BY]->(v)",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}}), (v:Test {{key:'{self.TEST_KEY}'}}) "
            "MERGE (v)-[:COVERS]->(s)",
            # A one-way pair, so a test can show the same traversal shape
            # resolving correctly when only one direction exists.
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}}), (c:Code {{key:'{self.CODE_PATH}'}}) "
            "MERGE (s)-[:IMPLEMENTED_BY]->(c)",
        ]:
            self.test.run_cmd(
                ["graph", "create", "-r", self.roadmap, "--query", query], check=True)

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers -----------------------------------------------------

    def run(self, subcmd, query, check=False):
        return self.test.run_cmd(
            ["graph", subcmd, "-r", self.roadmap, "--query", query], check=check)

    def json(self, subcmd, query):
        return self.test.run_cmd_json(
            ["graph", subcmd, "-r", self.roadmap, "--query", query])

    def assert_refused(self, subcmd, query, want_direction=None):
        code, stdout, stderr = self.run(subcmd, query)
        assert code == 6, (
            f"expected the read refusal (exit 6) for {query!r}; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert READ_REFUSAL in stderr, (
            f"the message must name the relationship variable and the READ rule; "
            f"got {stderr!r}")
        if want_direction:
            assert want_direction in stderr, (
                f"the message must name the offending direction "
                f"({want_direction!r}); got {stderr!r}")
        assert stdout.strip() == "", (
            f"a refused read must write nothing to stdout; got {stdout!r}")
        return stderr

    def node_property(self, label, key, name):
        result = self.json(
            "query", f"MATCH (n:{label} {{key:'{key}'}}) RETURN n.{name}")
        assert result["rows"], f"node {label}:{key!r} not found: {result!r}"
        return result["rows"][0][0]

    # ---- refused: the recorded reproduction -------------------------------

    def test_undirected_read_of_the_type_is_refused(self):
        self.assert_refused(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(x) RETURN type(e), x.key",
            "undirected pattern")

    def test_incoming_read_of_the_type_is_refused(self):
        self.assert_refused(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})<-[e]-(x) RETURN type(e)",
            "incoming pattern")

    def test_incoming_read_narrowed_by_a_type_filter_is_refused(self):
        # The recorded symptom in its sharpest form: the filter SELECTS the
        # right edge — matching is not the broken part — and the engine then
        # reports it under the OTHER edge's type. A type filter must not be
        # read as making the traversal safe.
        self.assert_refused(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})<-[e:COVERS]-(x) RETURN type(e)",
            "incoming pattern")

    def test_undirected_read_of_the_endpoints_is_refused(self):
        self.assert_refused(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(x) "
            "RETURN startNode(e).key, endNode(e).key",
            "undirected pattern")

    def test_undirected_read_used_only_by_a_where_predicate_is_refused(self):
        # Nothing reaches the caller to look wrong here: the engine drops the
        # matching row against the corrupted type. A rule that guarded
        # projections alone would leave this in place.
        self.assert_refused(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(x) "
            "WHERE type(e) = 'COVERS' RETURN x.key",
            "undirected pattern")

    def test_star_projection_is_refused(self):
        # RETURN * projects the relationship without naming it.
        self.assert_refused(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(x) RETURN *",
            "undirected pattern")

    def test_graph_search_is_refused_on_the_same_shape(self):
        self.assert_refused(
            "search",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(x) RETURN type(e)",
            "undirected pattern")

    # ---- refused: the value must not reach the disk -----------------------

    def test_node_write_deriving_its_value_from_an_incoming_read_is_refused_and_writes_nothing(self):
        # The consequence that carried the decision. Before the fix this exited
        # 0, reported {"ok": true}, and PERSISTED "VERIFIED_BY" — the forward
        # edge's type — where the truth is "COVERS". The assertion is on
        # storage, not on the exit code: a refusal that still wrote the property
        # would pass an exit-code-only test.
        code, stdout, stderr = self.run(
            "update",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})<-[e]-(v:Test) "
            "SET v.last_edge_type = type(e)")
        assert code == 6, (
            f"a node write whose VALUE comes from an incoming-bound relationship "
            f"must be refused; exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert READ_REFUSAL in stderr, (
            f"the refusal must come from the READ rule, which owns the "
            f"right-hand side; got {stderr!r}")
        stored = self.json(
            "query",
            f"MATCH (v:Test {{key:'{self.TEST_KEY}'}}) RETURN v.last_edge_type")
        assert stored["rows"] == [[None]], (
            f"the refused write reached storage: {stored!r} "
            f"(before the fix this persisted 'VERIFIED_BY', the FORWARD edge's "
            f"type, where the truth is 'COVERS')")

    # ---- the DELETE exemption is of the CLAUSE, not of the command --------

    def test_predicate_gated_delete_is_refused_and_removes_nothing(self):
        # The sharpest consequence in this family, and the reason the exemption
        # is drawn by clause. A bare `DELETE e` is correct; the moment a
        # predicate over type(e) decides WHICH edges are deleted, the engine
        # evaluates the corrupted type, drops the row, and a DESTRUCTIVE
        # statement exits 0 reporting {"ok": true} having removed nothing. The
        # caller has no reason to check.
        for pattern, direction in [
            (f"(s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(v:Test)", "undirected pattern"),
            (f"(s:Spec {{key:'{self.SPEC_KEY}'}})<-[e]-(v:Test)", "incoming pattern"),
        ]:
            code, stdout, stderr = self.run(
                "delete", f"MATCH {pattern} WHERE type(e) = 'COVERS' DELETE e")
            assert code == 6, (
                f"a DELETE gated by a predicate over a misresolved relationship "
                f"must be refused; pattern={pattern!r} exit={code} "
                f"stdout={stdout!r} stderr={stderr!r}")
            assert READ_REFUSAL in stderr and direction in stderr, (
                f"expected the {direction} read refusal; got {stderr!r}")

        # Storage is the real assertion: BOTH edges must still be there.
        surviving = self.json(
            "query",
            f"MATCH (a)-[e]->(b) WHERE a.key IN ['{self.SPEC_KEY}','{self.TEST_KEY}'] "
            f"AND b.key IN ['{self.SPEC_KEY}','{self.TEST_KEY}'] RETURN type(e)")
        types = sorted(row[0] for row in surviving["rows"])
        assert types == ["COVERS", "VERIFIED_BY"], (
            f"a refused DELETE must leave both edges in place; surviving={types!r}")

    def test_bare_delete_through_a_reverse_pattern_still_works(self):
        # This matters as much as the refusals: it is what stops the fix
        # over-reaching into the one DELETE shape that was always correct.
        code, stdout, stderr = self.run(
            "delete",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:COVERS]-(v:Test) DELETE e")
        assert code == 0, (
            f"a bare DELETE through an undirected pattern must stay accepted; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")

        surviving = self.json(
            "query",
            f"MATCH (a)-[e]->(b) WHERE a.key IN ['{self.SPEC_KEY}','{self.TEST_KEY}'] "
            f"AND b.key IN ['{self.SPEC_KEY}','{self.TEST_KEY}'] RETURN type(e)")
        types = sorted(row[0] for row in surviving["rows"])
        assert types == ["VERIFIED_BY"], (
            f"the bare DELETE must remove the COVERS edge and only that one; "
            f"surviving={types!r}")

    # ---- the refusal names the rewrites -----------------------------------

    def test_refusal_names_the_variable_the_direction_and_both_rewrites(self):
        stderr = self.assert_refused(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(x) RETURN type(e)",
            "undirected pattern")
        for want in [
            # the source-anchored rewrite
            "MATCH (source)-[e]->(target)",
            # the target-anchored rewrite, for the edges arriving AT a node
            "MATCH (other)-[e]->(target {key:'...'})",
            # the union of the two outgoing legs, for a full undirected read
            "UNION ALL",
        ]:
            assert want in stderr, (
                f"the refusal must offer the rewrite {want!r}; got {stderr!r}")

    # ---- admitted: the shapes that resolve correctly ----------------------

    def test_outgoing_read_is_answered_with_the_true_type(self):
        result = self.json(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:VERIFIED_BY]->(x:Test) "
            "RETURN type(e), startNode(e).key, endNode(e).key")
        assert result["rows"] == [["VERIFIED_BY", self.SPEC_KEY, self.TEST_KEY]], (
            f"the outgoing read must report the stored edge; got {result!r}")

    def test_outgoing_rewrite_anchored_on_the_target_reaches_the_reverse_edge(self):
        # This is what makes the refusal cost no reach: the relationship the
        # refused incoming read mislabelled as VERIFIED_BY is reported here as
        # COVERS, with the stored orientation intact.
        result = self.json(
            "query",
            f"MATCH (x)-[e]->(s:Spec {{key:'{self.SPEC_KEY}'}}) "
            "RETURN type(e), startNode(e).key, endNode(e).key")
        assert result["rows"] == [["COVERS", self.TEST_KEY, self.SPEC_KEY]], (
            f"the target-anchored outgoing rewrite must report the reverse edge "
            f"with its true type and orientation; got {result!r}")

    def test_union_of_the_two_outgoing_legs_recovers_the_whole_undirected_read(self):
        result = self.json(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e]->(x) "
            "RETURN type(e) AS t, x.key AS k UNION ALL "
            f"MATCH (x)-[e]->(s:Spec {{key:'{self.SPEC_KEY}'}}) "
            "RETURN type(e) AS t, x.key AS k")
        got = sorted(tuple(row) for row in result["rows"])
        expected = sorted([
            ("VERIFIED_BY", self.TEST_KEY),
            ("IMPLEMENTED_BY", self.CODE_PATH),
            ("COVERS", self.TEST_KEY),
        ])
        assert got == expected, (
            f"the union rewrite must recover every edge the undirected read "
            f"reached, each with its true type; got {got!r} want {expected!r}")

    def test_anonymous_undirected_relationship_is_admitted(self):
        # No variable is bound, so no relationship value is ever built.
        result = self.json(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[:COVERS]-(x) RETURN x.key")
        assert result["rows"] == [[self.TEST_KEY]], (
            f"an anonymous undirected traversal must stay accepted; got {result!r}")

    def test_undirected_relationship_bound_but_never_read_is_admitted(self):
        result = self.json(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(x) RETURN x.key")
        assert len(result["rows"]) == 3, (
            f"binding a relationship without reading it must stay accepted and "
            f"reach every neighbour; got {result!r}")

    def test_named_path_over_an_undirected_pattern_is_admitted_and_correct(self):
        # The path renderer resolves each hop through the resolver that is TOLD
        # the traversal direction, so it reports both edges truthfully.
        result = self.json(
            "query",
            f"MATCH p=(s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(v:Test) RETURN p")
        types = sorted(
            rel["type"] for row in result["rows"] for rel in row[0]["relationships"])
        assert types == ["COVERS", "VERIFIED_BY"], (
            f"the named path must carry both edges with their true types; "
            f"got {types!r}")

    def test_variable_length_undirected_relationship_is_admitted_and_correct(self):
        result = self.json(
            "search",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e*1..1]-(v:Test) RETURN e")
        types = sorted(rel["type"] for row in result["rows"] for rel in row[0])
        assert types == ["COVERS", "VERIFIED_BY"], (
            f"a variable-length hop must carry both edges with their true "
            f"types; got {types!r}")

    def test_node_write_through_an_undirected_traversal_is_admitted(self):
        # The relationship is bound but never read, and the write targets a
        # node, resolved by identifier rather than by endpoint pair.
        code, stdout, stderr = self.run(
            "update",
            f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})-[e]-(x) SET x.reviewed = true")
        assert code == 0, (
            f"a node write through an undirected traversal must stay accepted; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert self.node_property("Spec", self.SPEC_KEY, "reviewed") is True, (
            "the node write must genuinely have reached the Spec node")

    def test_delete_through_an_undirected_traversal_is_admitted(self):
        # DELETE resolves the edge itself rather than through the endpoint
        # columns, so it removes the right one. `graph delete` is untouched.
        code, stdout, stderr = self.run(
            "delete",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:COVERS]-(v:Test) DELETE e")
        assert code == 0, (
            f"an undirected DELETE must stay accepted; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")
        survivors = self.json(
            "query",
            f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})-[e:COVERS]->(s) RETURN type(e)")
        assert survivors["rows"] == [], (
            f"the undirected DELETE reported success but the edge survived: "
            f"{survivors!r}")
        # And it removed only that one.
        kept = self.json(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:VERIFIED_BY]->(v) RETURN type(e)")
        assert kept["rows"] == [["VERIFIED_BY"]], (
            f"the undirected DELETE removed the wrong edge; got {kept!r}")

    def test_relationship_write_refusal_still_owns_the_set_target(self):
        # The two rules stay separate: a SET whose TARGET is the relationship is
        # refused by the WRITE rule, with its own message, not by this one.
        code, stdout, stderr = self.run(
            "update",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(v:Test) "
            "SET e.last_commit = '0d86dbc'")
        assert code == 6, (
            f"a relationship SET target must still be refused; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert WRITE_REFUSAL in stderr, (
            f"the write rule must own the SET target; got {stderr!r}")


def _run_all():
    passed = 0
    failed = 0
    failures = []
    # Classes are DISCOVERED by inspecting this module, never listed. A listed
    # tuple silently skips any suite added after it was written — the runner
    # still exits 0 and the new class simply never runs (rmp task #303). The
    # count is printed so a class that stops being discovered is visible in the
    # output rather than inferred from a total that quietly shrank.
    classes = [
        obj for _name, obj in sorted(inspect.getmembers(sys.modules[__name__], inspect.isclass))
        if obj.__module__ == __name__ and _name.startswith("Test")
    ]
    print(f"Discovered {len(classes)} test classes: "
          f"{', '.join(cls.__name__ for cls in classes)}")
    for cls in classes:
        for m in sorted(name for name in dir(cls) if name.startswith("test_")):
            label = f"{cls.__name__}.{m}"
            instance = cls()
            instance.setup_method()
            try:
                getattr(instance, m)()
                passed += 1
                print(f"✓ {label}")
            except AssertionError as exc:
                failed += 1
                failures.append((label, exc))
                print(f"✗ {label}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((label, exc))
                print(f"✗ {label} (error)")
            finally:
                instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Graph read-direction tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
