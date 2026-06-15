#!/usr/bin/env python3
"""
Test 39: literal-aware guard rail for `rmp graph` (rmp task #36, BUG).

End-to-end backstop for SPEC/GRAPH.md § Literal-Aware Normalization and
Acceptance Criteria 18 and 19. The guard rail classifies a Cypher query by
the clauses it contains, but it MUST run that classification on a masked
normalization of the query so that a clause keyword appearing only inside a
string literal (or comment / backtick identifier) cannot change the class.

Covered against the compiled ./bin/rmp:
- AC 18: `graph create` whose property value contains "node-delete" and a
  "MATCH...SET" phrase is accepted (exit 0); the node is then queryable.
- AC 19: `graph query` whose WHERE string value contains "delete and set"
  is accepted as read-only (exit 0).
- Regression for both originally reproduced exit-6 symptoms.
- A genuine cross-class query is still rejected with exit 6, proving the mask
  neutralizes only literal contents, not real clauses.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


EXIT_OK = 0
EXIT_GUARD_RAIL = 6


class TestGraphGuardRailLiterals:

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers -----------------------------------------------------

    def run(self, args, check=True):
        return self.test.run_cmd(["graph", *args, "-r", self.roadmap], check=check)

    def query_json(self, query, subcmd="query"):
        return self.test.run_cmd_json(["graph", subcmd, "-r", self.roadmap, "--query", query])

    # ---- AC 18: create with keyword-bearing literal ------------------

    def test_ac18_create_with_delete_and_set_in_literal(self):
        # The literal mentions node-delete and a MATCH...SET phrase; the only
        # real writing clause is CREATE. Must be accepted (exit 0).
        query = (
            'CREATE (m:Memory {key:"audit-note", '
            'body:"discusses node-delete and a MATCH (n) SET n.x flow"})'
        )
        code, stdout, stderr = self.run(["create", "--query", query], check=False)
        assert code == EXIT_OK, (
            f"AC18: create with keywords only inside a literal must be accepted; "
            f"exit={code} stderr={stderr!r}"
        )
        assert '"ok": true' in stdout, f"AC18: expected ok JSON, got {stdout!r}"

        # The created node must be queryable, with its literal body intact.
        result = self.query_json('MATCH (m:Memory {key:"audit-note"}) RETURN m.body')
        assert result["rows"], f"AC18: created node not found on read-back: {result!r}"
        body = result["rows"][0][0]
        assert "node-delete" in body and "SET n.x" in body, (
            f"AC18: literal body was altered: {body!r}"
        )

    # ---- AC 19: read-only query with keyword-bearing literal ---------

    def test_ac19_query_with_delete_and_set_in_literal(self):
        # Seed a node so the read returns a row.
        self.run(["create", "--query",
                  'CREATE (m:Memory {key:"k1", title:"mentions delete and set"})'])
        # The WHERE string value contains "delete and set"; masked, the query
        # has no writing clause and is read-only. Must be accepted (exit 0).
        query = 'MATCH (m) WHERE m.title = "mentions delete and set" RETURN m.key'
        code, stdout, stderr = self.run(["query", "--query", query], check=False)
        assert code == EXIT_OK, (
            f"AC19: read-only query with keywords only inside a literal must be "
            f"accepted; exit={code} stderr={stderr!r}"
        )
        assert '"k1"' in stdout, f"AC19: expected matched row, got {stdout!r}"

    # ---- Genuine cross-class queries are still rejected --------------

    def test_genuine_create_in_query_still_rejected(self):
        # A real CREATE under `query` must still be rejected with exit 6:
        # the mask neutralizes literal contents only, not real clauses.
        self.test.assert_exit_code(
            ["graph", "query", "-r", self.roadmap,
             "--query", 'CREATE (n:Spec {key:"x"})'],
            EXIT_GUARD_RAIL,
        )

    def test_genuine_detach_delete_in_create_still_rejected(self):
        self.test.assert_exit_code(
            ["graph", "create", "-r", self.roadmap,
             "--query", "MATCH (n) DETACH DELETE n"],
            EXIT_GUARD_RAIL,
        )

    def test_genuine_set_in_query_still_rejected(self):
        self.test.assert_exit_code(
            ["graph", "query", "-r", self.roadmap,
             "--query", "MATCH (n:Spec {key:'x'}) SET n.status = 'done'"],
            EXIT_GUARD_RAIL,
        )


def _run_all():
    instance_cls = TestGraphGuardRailLiterals
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
    print(f"Graph guard-rail literal tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
