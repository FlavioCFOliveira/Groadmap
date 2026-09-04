#!/usr/bin/env python3
"""
Test 56: reading a relationship through an undirected or incoming pattern.

End-to-end backstop against the compiled ./bin/rmp and a real graph store.

What this module used to be. `rmp graph` refused an undirected or incoming
FIXED-LENGTH read of a bound relationship before opening the store, because
GoGraph recovered such a relationship's identity by probing the stored topology
and got it wrong on a node pair joined in BOTH directions. Twelve of the tests
below asserted that refusal and its message.

The refusal was withdrawn with the rest of the guard rail, and withdrawing it
made the underlying claim testable from the CLI for the first time since it was
written. It does not reproduce at the pinned engine: measured on GoGraph
v0.12.0 against the canonical bidirectional fixture, every one of the shapes the
specification names answers CORRECTLY (rmp task #362, FINDING). Correcting
`SPEC/GRAPH.md` section "What Groadmap Does Not Check", item 5, is rmp task
#373's, not this module's.

So this module asserts NEITHER reading. What it keeps is what is true on both:

  - the OUTGOING forms, anchored on either endpoint, which are correct whatever
    the data;
  - the published `UNION ALL` rewrite of the two outgoing legs, which recovers
    a full undirected read;
  - the shapes item 5 exempts even on its own reading -- an anonymous
    relationship, a relationship bound but never read, a projected named path,
    and a variable-length hop -- plus a node write and a bare `DELETE` through
    an undirected pattern.

EVERY fixture is a BIDIRECTIONAL pair whose two edges carry DIFFERENT types.
That is deliberate on both counts. A one-way pair resolves correctly under every
reading, so it distinguishes nothing; and a two-way pair whose edges share a
type hides the most visible symptom, because the type the engine would
substitute happens to equal the type it should report. The live groadmap
knowledge graph is in exactly that second shape today.
"""

import inspect
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestGraphRelationshipReadDirection:
    """SPEC/GRAPH.md section "What Groadmap Does Not Check", item 5."""

    SPEC_KEY = "graph-read-direction"
    TEST_KEY = "test_56_graph_read_direction.py"
    CODE_PATH = "internal/commands/graph.go"

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
                ["graph", "execute", "-r", self.roadmap, "--query", query], check=True)

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers -----------------------------------------------------

    def run(self, query, check=False):
        return self.test.run_cmd(
            ["graph", "execute", "-r", self.roadmap, "--query", query], check=check)

    def json(self, query):
        return self.test.run_cmd_json(
            ["graph", "execute", "-r", self.roadmap, "--query", query])

    def node_property(self, label, key, name):
        result = self.json(f"MATCH (n:{label} {{key:'{key}'}}) RETURN n.{name}")
        assert result["rows"], f"node {label}:{key!r} not found: {result!r}"
        return result["rows"][0][0]

    # ---- true on both readings: the outgoing forms and the exempt shapes ---

    def test_bare_delete_through_a_reverse_pattern_still_works(self):
        # This matters as much as the refusals: it is what stops the fix
        # over-reaching into the one DELETE shape that was always correct.
        code, stdout, stderr = self.run(f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:COVERS]-(v:Test) DELETE e")
        assert code == 0, (
            f"a bare DELETE through an undirected pattern must stay accepted; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")

        surviving = self.json(f"MATCH (a)-[e]->(b) WHERE a.key IN ['{self.SPEC_KEY}','{self.TEST_KEY}'] "
            f"AND b.key IN ['{self.SPEC_KEY}','{self.TEST_KEY}'] RETURN type(e)")
        types = sorted(row[0] for row in surviving["rows"])
        assert types == ["VERIFIED_BY"], (
            f"the bare DELETE must remove the COVERS edge and only that one; "
            f"surviving={types!r}")

    def test_outgoing_read_is_answered_with_the_true_type(self):
        result = self.json(f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:VERIFIED_BY]->(x:Test) "
            "RETURN type(e), startNode(e).key, endNode(e).key")
        assert result["rows"] == [["VERIFIED_BY", self.SPEC_KEY, self.TEST_KEY]], (
            f"the outgoing read must report the stored edge; got {result!r}")

    def test_outgoing_rewrite_anchored_on_the_target_reaches_the_reverse_edge(self):
        # This is what makes the refusal cost no reach: the relationship the
        # refused incoming read mislabelled as VERIFIED_BY is reported here as
        # COVERS, with the stored orientation intact.
        result = self.json(f"MATCH (x)-[e]->(s:Spec {{key:'{self.SPEC_KEY}'}}) "
            "RETURN type(e), startNode(e).key, endNode(e).key")
        assert result["rows"] == [["COVERS", self.TEST_KEY, self.SPEC_KEY]], (
            f"the target-anchored outgoing rewrite must report the reverse edge "
            f"with its true type and orientation; got {result!r}")

    def test_union_of_the_two_outgoing_legs_recovers_the_whole_undirected_read(self):
        result = self.json(f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e]->(x) "
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
        result = self.json(f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[:COVERS]-(x) RETURN x.key")
        assert result["rows"] == [[self.TEST_KEY]], (
            f"an anonymous undirected traversal must stay accepted; got {result!r}")

    def test_undirected_relationship_bound_but_never_read_is_admitted(self):
        result = self.json(f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(x) RETURN x.key")
        assert len(result["rows"]) == 3, (
            f"binding a relationship without reading it must stay accepted and "
            f"reach every neighbour; got {result!r}")

    def test_named_path_over_an_undirected_pattern_is_admitted_and_correct(self):
        # The path renderer resolves each hop through the resolver that is TOLD
        # the traversal direction, so it reports both edges truthfully.
        result = self.json(f"MATCH p=(s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(v:Test) RETURN p")
        types = sorted(
            rel["type"] for row in result["rows"] for rel in row[0]["relationships"])
        assert types == ["COVERS", "VERIFIED_BY"], (
            f"the named path must carry both edges with their true types; "
            f"got {types!r}")

    def test_variable_length_undirected_relationship_is_admitted_and_correct(self):
        result = self.json(f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e*1..1]-(v:Test) RETURN e")
        types = sorted(rel["type"] for row in result["rows"] for rel in row[0])
        assert types == ["COVERS", "VERIFIED_BY"], (
            f"a variable-length hop must carry both edges with their true "
            f"types; got {types!r}")

    def test_node_write_through_an_undirected_traversal_is_admitted(self):
        # The relationship is bound but never read, and the write targets a
        # node, resolved by identifier rather than by endpoint pair.
        code, stdout, stderr = self.run(f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})-[e]-(x) SET x.reviewed = true")
        assert code == 0, (
            f"a node write through an undirected traversal must stay accepted; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert self.node_property("Spec", self.SPEC_KEY, "reviewed") is True, (
            "the node write must genuinely have reached the Spec node")

    def test_delete_through_an_undirected_traversal_is_admitted(self):
        # DELETE resolves the edge itself rather than through the endpoint
        # columns, so it removes the right one. `graph execute` is untouched.
        code, stdout, stderr = self.run(f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:COVERS]-(v:Test) DELETE e")
        assert code == 0, (
            f"an undirected DELETE must stay accepted; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")
        survivors = self.json(f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})-[e:COVERS]->(s) RETURN type(e)")
        assert survivors["rows"] == [], (
            f"the undirected DELETE reported success but the edge survived: "
            f"{survivors!r}")
        # And it removed only that one.
        kept = self.json(f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:VERIFIED_BY]->(v) RETURN type(e)")
        assert kept["rows"] == [["VERIFIED_BY"]], (
            f"the undirected DELETE removed the wrong edge; got {kept!r}")


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
