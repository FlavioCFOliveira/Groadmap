#!/usr/bin/env python3
"""
Test 48: the Cypher clause surface `rmp graph` is classified against.

End-to-end backstop for SPEC/GRAPH.md § Schema Introspection,
§ Per-Subcommand Validation Rules notes 6 and 7, Acceptance Criteria 23-27, and
§ Dependency Maturity Risk mitigation 5.

The guard rail decides each subcommand's accepted class from the clauses a query
contains, so the set of clauses the pinned engine accepts is part of the
integration surface even though no Go symbol expresses it. When the engine
learns a new clause family, queries the previous engine rejected as a syntax
error start EXECUTING, and each subcommand's accepted class widens with no
Groadmap code changing. That is invisible to the two checks an engine upgrade
would otherwise rely on: a diff of removed or re-signed exported symbols finds
nothing, because nothing was removed, and a re-run of the existing acceptance
criteria finds nothing, because no existing criterion names a clause that did
not previously exist.

Two families arrived exactly that way, and this suite pins both against the
compiled ./bin/rmp:

- Schema introspection (SHOW INDEXES / SHOW INDEX / SHOW CONSTRAINTS /
  SHOW CONSTRAINT, with an optional YIELD / WHERE / RETURN tail) lists the
  registered schema without altering it. It is accepted by `query` and `search`
  (exit 0, columns/rows payload) and rejected by `create`, `update` and `delete`
  (exit 6, each subcommand's own message).

- FOREACH is an updating clause with no discriminator of its own: it is
  classified by the writing clauses its body carries. That containment is an
  emergent property of the grammar, so it is asserted here rather than assumed.

The schema-MUTATING DDL forms must stay rejected everywhere, in any case and
with any spacing between the two keywords — adding an introspection class must
not loosen the DDL rejection.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


EXIT_OK = 0
EXIT_GUARD_RAIL = 6

# The four schema-introspection statement forms the engine accepts, plus the
# case and projection-tail variants the guard rail must treat identically.
SHOW_FORMS = [
    "SHOW INDEXES",
    "SHOW INDEX",
    "SHOW CONSTRAINTS",
    "SHOW CONSTRAINT",
    "show indexes",
    "sHoW cOnStRaInTs",
    "SHOW INDEXES YIELD name, type WHERE type = 'RANGE' RETURN name",
    "SHOW CONSTRAINTS YIELD name RETURN name",
]

# The per-subcommand guard-rail message each write subcommand must produce.
WRITE_SUBCOMMAND_MESSAGE = {
    "create": "graph create accepts only CREATE/MERGE queries",
    "update": "graph update accepts only SET/REMOVE queries",
    "delete": "graph delete accepts only DELETE/DETACH DELETE queries",
}


class TestGraphClauseSurface:

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        # A small, realistic knowledge-graph fixture: two specifications, one
        # depending on the other, so a FOREACH over a matched path has real
        # nodes and relationships to act on.
        self.test.run_cmd([
            "graph", "create", "-r", self.roadmap, "--query",
            "CREATE (a:Spec {key:'user-authentication', status:'draft'})"
            "-[:DEPENDS_ON]->(b:Spec {key:'credential-storage', status:'draft'})",
        ])

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers -----------------------------------------------------

    def run(self, subcmd, query, check=False):
        return self.test.run_cmd(
            ["graph", subcmd, "-r", self.roadmap, "--query", query], check=check)

    def json(self, subcmd, query):
        return self.test.run_cmd_json(
            ["graph", subcmd, "-r", self.roadmap, "--query", query])

    def prop(self, key, name):
        """Return a single property value for the Spec node with the given key."""
        result = self.json("query", f"MATCH (s:Spec {{key:'{key}'}}) RETURN s.{name} AS v")
        assert result["rows"], f"Spec {key!r} not found when reading {name!r}: {result!r}"
        return result["rows"][0][0]

    # ---- AC 23: introspection is accepted by the read subcommands ----

    def test_ac23_read_subcommands_accept_every_show_form(self):
        for subcmd in ("query", "search"):
            for query in SHOW_FORMS:
                code, stdout, stderr = self.run(subcmd, query)
                assert code == EXIT_OK, (
                    f"AC23: `graph {subcmd}` must accept the schema-introspection "
                    f"command {query!r}; exit={code} stderr={stderr!r}")
                # The payload must be a real result envelope, not merely a
                # zero exit: introspection returns columns/rows like any read.
                assert '"columns"' in stdout and '"rows"' in stdout, (
                    f"AC23: `graph {subcmd}` on {query!r} must return the "
                    f"columns/rows result shape; got {stdout!r}")

    def test_ac23_show_indexes_returns_the_documented_columns(self):
        # The engine yields the openCypher column order for SHOW INDEXES. The
        # assertion is on the columns rather than the rows because Groadmap's
        # guard rail rejects index DDL, so a roadmap graph has no index to list;
        # an empty row set with the right columns is the correct outcome.
        result = self.json("query", "SHOW INDEXES")
        assert result["columns"] == [
            "name", "state", "type", "entityType", "labelsOrTypes", "properties",
        ], f"AC23: unexpected SHOW INDEXES columns: {result['columns']!r}"
        assert result["rows"] == [], (
            f"AC23: a roadmap graph cannot hold an index (index DDL is rejected "
            f"by every subcommand), so SHOW INDEXES must list none; got {result['rows']!r}")

    def test_ac23_show_constraints_returns_the_documented_columns(self):
        result = self.json("query", "SHOW CONSTRAINTS")
        assert result["columns"] == [
            "name", "type", "entityType", "labelsOrTypes", "properties",
        ], f"AC23: unexpected SHOW CONSTRAINTS columns: {result['columns']!r}"
        assert result["rows"] == [], (
            f"AC23: constraint DDL is rejected by every subcommand, so "
            f"SHOW CONSTRAINTS must list none; got {result['rows']!r}")

    def test_ac23_projection_tail_selects_the_yielded_columns(self):
        # The YIELD / RETURN tail must actually project, not merely parse.
        result = self.json("query", "SHOW INDEXES YIELD name RETURN name")
        assert result["columns"] == ["name"], (
            f"AC23: the projection tail must reduce the result to the yielded "
            f"column; got {result['columns']!r}")

    # ---- AC 24: introspection is rejected by the write subcommands ---

    def test_ac24_write_subcommands_reject_introspection(self):
        for subcmd, message in WRITE_SUBCOMMAND_MESSAGE.items():
            for query in ("SHOW INDEXES", "SHOW CONSTRAINTS"):
                code, _stdout, stderr = self.run(subcmd, query)
                assert code == EXIT_GUARD_RAIL, (
                    f"AC24: `graph {subcmd}` must reject {query!r} with exit 6; "
                    f"exit={code} stderr={stderr!r}")
                assert message in stderr, (
                    f"AC24: `graph {subcmd}` must reject {query!r} with its own "
                    f"guard-rail message {message!r}; got {stderr!r}")

    # ---- AC 25: SHOW must be a statement, not any occurrence -------

    def test_ac25_show_inside_a_literal_is_an_ordinary_read(self):
        query = ("MATCH (s:Spec) WHERE s.key = 'SHOW INDEXES' "
                 "RETURN s.key AS k")
        code, _stdout, stderr = self.run("query", query)
        assert code == EXIT_OK, (
            f"AC25: a SHOW keyword inside a string literal must leave the query "
            f"an ordinary read; exit={code} stderr={stderr!r}")
        # It must have run as the MATCH it is — projecting the aliased column
        # and matching nothing, since no Spec carries that key — rather than as
        # an introspection command, whose columns would be the schema listing's.
        result = self.json("query", query)
        assert result["columns"] == ["k"], (
            f"AC25: the query must project its own aliased column, not a schema "
            f"listing; got {result['columns']!r}")
        assert result["rows"] == [], (
            f"AC25: no Spec carries the key 'SHOW INDEXES', so the match must be "
            f"empty; got {result['rows']!r}")

    def test_ac25_property_named_show_is_an_ordinary_write(self):
        # If the introspection matcher were not anchored at the start of the
        # statement, this creating write would be misread as read-only
        # introspection and REJECTED by `graph create` — a silently lost write.
        code, stdout, stderr = self.run(
            "create", "CREATE (p:Panel {key:'schema-panel', show:'indexes'})")
        assert code == EXIT_OK, (
            f"AC25: a node carrying a property named show is an ordinary "
            f"creating write and must be accepted; exit={code} stderr={stderr!r}")
        assert '"ok": true' in stdout, f"AC25: expected ok JSON, got {stdout!r}"

        # And it must genuinely have been written.
        result = self.json("query", "MATCH (p:Panel {key:'schema-panel'}) RETURN p.show AS v")
        assert result["rows"] and result["rows"][0][0] == "indexes", (
            f"AC25: the created Panel node must be readable back; got {result!r}")

    def test_ac25_label_named_show_is_an_ordinary_read(self):
        code, _stdout, stderr = self.run("query", "MATCH (n:Show) RETURN n")
        assert code == EXIT_OK, (
            f"AC25: matching a label named Show is an ordinary read; "
            f"exit={code} stderr={stderr!r}")

    # ---- AC 26: FOREACH is classified by its body -------------------

    def test_ac26_read_subcommands_reject_foreach(self):
        query = "MATCH (s:Spec) FOREACH (x IN [1] | SET s.reviewed = true)"
        for subcmd in ("query", "search"):
            code, _stdout, stderr = self.run(subcmd, query)
            assert code == EXIT_GUARD_RAIL, (
                f"AC26: `graph {subcmd}` must reject a FOREACH carrying a SET "
                f"body with exit 6; exit={code} stderr={stderr!r}")
            assert f"graph {subcmd} accepts only read-only queries" in stderr, (
                f"AC26: expected the read-only guard-rail message; got {stderr!r}")

        # The rejection must have prevented the write, not merely reported it.
        assert self.prop("user-authentication", "reviewed") is None, (
            "AC26: a FOREACH rejected by the guard rail must not have written "
            "anything to the graph")

    def test_ac26_update_accepts_a_foreach_that_sets(self):
        code, stdout, stderr = self.run(
            "update", "MATCH (s:Spec) FOREACH (x IN [1] | SET s.reviewed = true)")
        assert code == EXIT_OK, (
            f"AC26: `graph update` must accept a FOREACH whose body is a SET; "
            f"exit={code} stderr={stderr!r}")
        assert '"ok": true' in stdout, f"AC26: expected ok JSON, got {stdout!r}"
        # The body must have run once per row, on every matched node.
        for key in ("user-authentication", "credential-storage"):
            assert self.prop(key, "reviewed") is True, (
                f"AC26: the FOREACH body must have set reviewed on {key!r}")

    def test_ac26_create_accepts_a_foreach_that_creates(self):
        code, stdout, stderr = self.run(
            "create",
            "FOREACH (k IN ['session-management', 'password-rotation'] | "
            "CREATE (:Spec {key: k, status: 'draft'}))")
        assert code == EXIT_OK, (
            f"AC26: `graph create` must accept a FOREACH whose body is a CREATE; "
            f"exit={code} stderr={stderr!r}")
        assert '"ok": true' in stdout, f"AC26: expected ok JSON, got {stdout!r}"

        result = self.json(
            "query", "MATCH (s:Spec) RETURN s.key AS k ORDER BY s.key")
        keys = [row[0] for row in result["rows"]]
        assert keys == [
            "credential-storage", "password-rotation",
            "session-management", "user-authentication",
        ], f"AC26: the FOREACH body must have created both nodes; got {keys!r}"

    def test_ac26_cross_class_foreach_is_rejected(self):
        # A FOREACH is classified by the writing clauses of its body, so a
        # setting body is invalid under create and a creating body is invalid
        # under update.
        setting = "MATCH (s:Spec) FOREACH (x IN [1] | SET s.reviewed = true)"
        creating = "FOREACH (k IN ['audit-logging'] | CREATE (:Spec {key: k}))"
        for subcmd, query in (("create", setting), ("update", creating),
                              ("delete", setting), ("delete", creating)):
            code, _stdout, stderr = self.run(subcmd, query)
            assert code == EXIT_GUARD_RAIL, (
                f"AC26: `graph {subcmd}` must reject {query!r} with exit 6; "
                f"exit={code} stderr={stderr!r}")
            assert WRITE_SUBCOMMAND_MESSAGE[subcmd] in stderr, (
                f"AC26: expected {WRITE_SUBCOMMAND_MESSAGE[subcmd]!r}; got {stderr!r}")

    def test_ac26_nested_foreach_is_classified_by_the_innermost_body(self):
        nested = ("MATCH (s:Spec) FOREACH (a IN [[2]] | "
                  "FOREACH (b IN a | SET s.depth = b))")
        code, _stdout, stderr = self.run("query", nested)
        assert code == EXIT_GUARD_RAIL, (
            f"AC26: a nested FOREACH whose innermost body is a SET must be "
            f"rejected by `graph query`; exit={code} stderr={stderr!r}")

        code, _stdout, stderr = self.run("update", nested)
        assert code == EXIT_OK, (
            f"AC26: the same nested FOREACH must be accepted by `graph update`; "
            f"exit={code} stderr={stderr!r}")
        assert self.prop("user-authentication", "depth") == 2, (
            "AC26: the innermost FOREACH body must have run")

    def test_ac26_delete_accepts_a_foreach_that_detach_deletes(self):
        code, stdout, stderr = self.run(
            "delete",
            "MATCH p = (a:Spec {key:'user-authentication'})-[:DEPENDS_ON]->(b:Spec) "
            "FOREACH (n IN nodes(p) | DETACH DELETE n)")
        assert code == EXIT_OK, (
            f"AC26: `graph delete` must accept a FOREACH whose body is a "
            f"DETACH DELETE; exit={code} stderr={stderr!r}")
        assert '"ok": true' in stdout, f"AC26: expected ok JSON, got {stdout!r}"

        result = self.json("query", "MATCH (s:Spec) RETURN count(s) AS c")
        assert result["rows"][0][0] == 0, (
            f"AC26: both Spec nodes on the matched path must be gone; got {result!r}")

    def test_ac26_foreach_keyword_inside_a_literal_stays_a_read(self):
        query = ("MATCH (s:Spec) WHERE s.status = 'use FOREACH to fan out' "
                 "RETURN s.key AS k")
        code, _stdout, stderr = self.run("query", query)
        assert code == EXIT_OK, (
            f"AC26: a FOREACH keyword appearing only inside a literal must leave "
            f"the query read-only; exit={code} stderr={stderr!r}")

    # ---- AC 27: schema-mutating DDL stays rejected everywhere -------

    def test_ac27_ddl_is_rejected_by_every_subcommand_in_any_spelling(self):
        ddl_queries = [
            "CREATE INDEX spec_key_idx FOR (n:Spec) ON (n.key)",
            "create index spec_key_idx FOR (n:Spec) ON (n.key)",
            "CREATE   INDEX spec_key_idx FOR (n:Spec) ON (n.key)",
            "DROP INDEX spec_key_idx",
            "drop   index spec_key_idx",
            "CREATE CONSTRAINT unique_spec_key FOR (n:Spec) REQUIRE n.key IS UNIQUE",
            "Drop Constraint unique_spec_key",
        ]
        for subcmd in ("query", "search", "create", "update", "delete"):
            for query in ddl_queries:
                code, _stdout, stderr = self.run(subcmd, query)
                assert code == EXIT_GUARD_RAIL, (
                    f"AC27: `graph {subcmd}` must reject the schema-mutating DDL "
                    f"{query!r} with exit 6 whatever its case or spacing; "
                    f"exit={code} stderr={stderr!r}")
                assert "validation error" in stderr, (
                    f"AC27: expected a guard-rail validation error; got {stderr!r}")

    def test_ac27_ddl_rejection_leaves_the_schema_untouched(self):
        # The rejection must happen before execution: no index may exist after
        # attempting to create one, which SHOW INDEXES now lets us verify
        # directly rather than by inference.
        self.run("create", "CREATE INDEX spec_key_idx FOR (n:Spec) ON (n.key)")
        result = self.json("query", "SHOW INDEXES")
        assert result["rows"] == [], (
            f"AC27: a rejected CREATE INDEX must not have reached the engine; "
            f"SHOW INDEXES lists {result['rows']!r}")


def _run_all():
    instance_cls = TestGraphClauseSurface
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
    print(f"Graph clause-surface tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
