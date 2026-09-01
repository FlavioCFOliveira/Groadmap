#!/usr/bin/env python3
"""
Test 64: schema management through `rmp graph execute`.

End-to-end backstop for SPEC/GRAPH.md "Schema Management" and Acceptance
Criteria 62 to 69, driven against the compiled ./bin/rmp.

`rmp graph execute` is the subcommand through which a knowledge graph's schema --
its indexes and its constraints -- is managed. Three things about that make an
end-to-end suite the only place several of these criteria can be established at
all, rather than a duplicate of the Go tests:

- A schema definition must survive the PROCESS boundary. Every successful write
  checkpoints: it rewrites the snapshot and truncates the write-ahead log the
  CREATE INDEX event was living in. An implementation whose snapshot carries no
  schema passes every assertion made inside the creating invocation and loses
  the index the moment the process exits, so the assertion that matters is the
  one made by a LATER invocation (AC63).

- Several criteria turn on the EXIT CODE, and two of them carry the code a
  reader does not expect: a duplicate create and a drop of an object that does
  not exist are engine failures and exit 1, not the 6 a guard-rail refusal
  carries (AC68). A badly spaced DDL statement likewise exits 1, because the
  guard rail admits it deliberately and the engine refuses it (AC69). Exit codes
  are a property of the binary, not of a function.

- Four surfaces answer a schema-introspection command -- `graph execute`,
  `graph execute`, `graph execute`, and the read-only web graph data endpoint --
  and they must agree. The exit code alone establishes nothing there: a read
  path constructed without the recovered schema answers the identical query with
  ZERO ROWS and exits 0, so the rows are what is compared (AC64).

One limitation of that last comparison is recorded here rather than hidden. The
web graph data endpoint's response shape is `{"nodes": [...], "edges": [...]}`
and carries no tabular rows at all (SPEC/DATA_FORMATS.md "Graph View Data"), so
a schema listing requested through it comes back as the empty graph. The row
comparison is therefore made across the three CLI surfaces, and the endpoint is
asserted to answer the same statement successfully against the same store with
the shape its own contract gives it. AC64's wording asks the endpoint to
"report the row named spec_key", which its published response shape cannot do;
that disagreement between AC64 and DATA_FORMATS.md is left for the specification
to settle rather than resolved by an assertion invented here.
"""

import http.client
import inspect
import json
import os
import subprocess
import sys
import tempfile
import time
import urllib.parse

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


EXIT_OK = 0
EXIT_ENGINE = 1
EXIT_GUARD_RAIL = 6

# A realistic knowledge-graph fixture: three specifications of a backend
# platform, one depending on another, each carrying the properties the indexes
# below are declared over.
SEED_QUERY = (
    "CREATE (a:Spec {key:'user-authentication', title:'User authentication', ord:1})"
    "-[:DEPENDS_ON]->"
    "(b:Spec {key:'credential-storage', title:'Credential storage', ord:2}) "
    "CREATE (c:Spec {key:'session-management', title:'Session management', ord:3})"
)


class SchemaTestBase:
    """Shared fixture: a roadmap whose graph carries the seed above."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        self.test.run_cmd(["graph", "execute", "-r", self.roadmap, "--query", SEED_QUERY])

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers -----------------------------------------------------

    def run(self, query):
        """One `rmp graph execute` invocation: its own process, every time."""
        return self.test.run_cmd(
            ["graph", "execute", "-r", self.roadmap, "--query", query], check=False)

    def ok(self, query):
        """Run a statement that must succeed, and return its parsed stdout."""
        code, stdout, stderr = self.run(query)
        assert code == EXIT_OK, (
            f"`graph execute --query {query!r}` must succeed; "
            f"exit={code} stderr={stderr!r}")
        return json.loads(stdout)

    def schema_rows(self, statement):
        """The full row set a SHOW statement reports."""
        result = self.ok(statement)
        assert "columns" in result and "rows" in result, (
            f"{statement!r} must return the columns/rows listing shape, not "
            f"{result!r}")
        return result["rows"]

    def schema_names(self, statement="SHOW INDEXES"):
        """The `name` column of every row a SHOW statement reports."""
        result = self.ok(statement)
        name_col = result["columns"].index("name")
        return [row[name_col] for row in result["rows"]]

    def node_count(self):
        return self.ok("MATCH (n) RETURN count(n)")["rows"][0][0]

    def edge_count(self):
        return self.ok("MATCH ()-[r]->() RETURN count(r)")["rows"][0][0]


class TestGraphSchemaStatements(SchemaTestBase):
    """AC62, AC63, AC65, AC66: the statements, their durability, their names."""

    # ---- AC62: each statement returns the shape the specification gives it ---

    def test_ac62_index_lifecycle_across_separate_invocations(self):
        result = self.ok("CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
        assert result == {"ok": True}, (
            f"AC62: a schema-mutating statement produces no result columns and "
            f"returns {{'ok': true}}; got {result!r}")

        # A LATER, SEPARATE process invocation -- every self.run is its own
        # process -- must still see it. This is the assertion the destroyed-
        # schema defect fails (AC63): the create above checkpointed and
        # truncated the write-ahead log before the process exited.
        listing = self.ok("SHOW INDEXES")
        assert set(listing.keys()) == {"columns", "rows"}, (
            f"AC62: SHOW INDEXES returns the columns/rows shape even though it "
            f"carries no RETURN clause, not {{'ok': true}}; got {listing!r}")
        assert listing["columns"] == [
            "name", "state", "type", "entityType", "labelsOrTypes", "properties",
        ], f"AC62: unexpected SHOW INDEXES columns: {listing['columns']!r}"
        assert [row[0] for row in listing["rows"]] == ["spec_key"], (
            f"AC62/AC63: SHOW INDEXES must report the index created in an earlier "
            f"invocation; got {listing['rows']!r}")

        result = self.ok("DROP INDEX spec_key")
        assert result == {"ok": True}, f"AC62: DROP INDEX returns ok; got {result!r}"
        assert self.schema_names() == [], (
            "AC62: a dropped index must be gone from a subsequent SHOW INDEXES")

    def test_ac62_constraint_lifecycle_across_separate_invocations(self):
        result = self.ok("CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE")
        assert result == {"ok": True}, f"AC62: got {result!r}"

        listing = self.ok("SHOW CONSTRAINTS")
        assert listing["columns"] == [
            "name", "type", "entityType", "labelsOrTypes", "properties",
        ], f"AC62: unexpected SHOW CONSTRAINTS columns: {listing['columns']!r}"
        assert [row[0] for row in listing["rows"]] == ["spec_key_uq"], (
            f"AC62/AC63: SHOW CONSTRAINTS must report the constraint created in "
            f"an earlier invocation; got {listing['rows']!r}")

        assert self.ok("DROP CONSTRAINT spec_key_uq") == {"ok": True}
        assert self.schema_names("SHOW CONSTRAINTS") == [], (
            "AC62: a dropped constraint must be gone from a subsequent listing")

    # ---- AC63: the schema survives the checkpoint, and a constraint is
    #            still ENFORCED afterwards ---------------------------------

    def test_ac63_constraint_is_still_enforced_after_the_process_boundary(self):
        """A constraint that is merely LISTED is not a constraint that is
        APPLIED, so the criterion asks for the write to be refused.

        Executed against an implementation whose checkpoint dropped the
        constraint, the duplicate create below exits 0 reporting {"ok": true}
        and the duplicate is stored -- the silent integrity loss this test
        exists to catch.
        """
        self.ok("CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE")

        code, stdout, stderr = self.run("CREATE (:Spec {key:'user-authentication'})")
        assert code == EXIT_ENGINE, (
            f"AC63: with spec_key_uq declared over Spec.key and a node already "
            f"carrying 'user-authentication', a second create of that key must "
            f"fail; exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert "constraint" in stderr.lower(), (
            f"AC63: the refusal must name the constraint that failed; got {stderr!r}")

        # And the read-back reports one such node, not two.
        count = self.ok("MATCH (n:Spec {key:'user-authentication'}) RETURN count(n)")["rows"][0][0]
        assert count == 1, (
            f"AC63: the duplicate must not have been stored; the graph holds "
            f"{count} nodes with that key")

    def test_ac63_index_survives_a_later_unrelated_write_and_its_checkpoint(self):
        """Every successful write checkpoints, not only the one that declared
        the schema, so an ordinary write later in the graph's life must not
        destroy a definition that was already there.
        """
        self.ok("CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
        self.ok("CREATE (:Spec {key:'audit-logging', title:'Audit logging', ord:4})")
        self.ok("MATCH (n:Spec {key:'audit-logging'}) SET n.status = 'draft'")
        self.ok("MATCH (n:Spec {key:'audit-logging'}) DETACH DELETE n")

        assert self.schema_names() == ["spec_key"], (
            "AC63: an ordinary create, update and delete each checkpoint, and "
            "none of them may destroy a schema definition already registered")

    # ---- AC65: names ---------------------------------------------------

    def test_ac65_declared_name_is_verbatim_and_derived_name_is_the_only_drop_key(self):
        self.ok("CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
        assert self.schema_names() == ["spec_key"], (
            "AC65: a declared name is used verbatim, with nothing appended")

        self.ok("CREATE INDEX FOR (n:Spec) ON (n.title)")
        assert sorted(self.schema_names()) == ["spec_key", "spec_title_hash"], (
            f"AC65: an omitted name is derived as <label>_<property>_<kind>; "
            f"got {self.schema_names()!r}")

        # The derived name is the ONLY name a drop accepts. Dropping by the name
        # a reader would guess fails, and leaves the index in place.
        code, _stdout, stderr = self.run("DROP INDEX spec_title")
        assert code == EXIT_ENGINE, (
            f"AC65: DROP INDEX by a name no object carries must fail with exit "
            f"{EXIT_ENGINE}; exit={code} stderr={stderr!r}")
        assert "spec_title_hash" in self.schema_names(), (
            "AC65: a failed drop must leave the index in place")

        assert self.ok("DROP INDEX spec_title_hash") == {"ok": True}
        assert self.schema_names() == ["spec_key"], (
            "AC65: the derived name is what drops the unnamed index")

    def test_ac65_unnamed_constraint_is_derived_and_dropped_by_the_derived_name(self):
        self.ok("CREATE CONSTRAINT FOR (n:Spec) REQUIRE n.title IS NOT NULL")
        names = self.schema_names("SHOW CONSTRAINTS")
        assert len(names) == 1, f"AC65: expected one constraint; got {names!r}"
        derived = names[0]
        assert derived != "" and "title" in derived, (
            f"AC65: the derived constraint name must be built from the label and "
            f"property; got {derived!r}")
        assert self.ok(f"DROP CONSTRAINT {derived}") == {"ok": True}
        assert self.schema_names("SHOW CONSTRAINTS") == [], (
            "AC65: the derived name is what drops the unnamed constraint")

    # ---- AC66: altering an index is two invocations ---------------------

    def test_ac66_altering_an_index_is_two_invocations_with_a_visible_gap(self):
        self.ok("CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord)")
        rows = self.schema_rows("SHOW INDEXES")
        kind_col = self.ok("SHOW INDEXES")["columns"].index("type")
        assert rows[0][kind_col] == "hash", (
            f"AC66: an index is a hash index by default; got {rows[0]!r}")

        assert self.ok("DROP INDEX spec_ord") == {"ok": True}

        # BETWEEN the two invocations the index is absent, and a query over the
        # property it covered still returns the correct rows -- which is what
        # establishes that the intermediate state costs speed and not answers.
        assert self.schema_names() == [], (
            "AC66: between the drop and the create, SHOW INDEXES must report the "
            "index absent")
        ordered = self.ok("MATCH (s:Spec) WHERE s.ord >= 2 RETURN s.key AS k ORDER BY s.ord")
        assert [row[0] for row in ordered["rows"]] == [
            "credential-storage", "session-management"], (
            f"AC66: a query over the uncovered property must still return the "
            f"correct rows; got {ordered['rows']!r}")

        self.ok("CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}")
        listing = self.ok("SHOW INDEXES")
        kind_col = listing["columns"].index("type")
        assert [row[0] for row in listing["rows"]] == ["spec_ord"], (
            f"AC66: the recreated index must be reported; got {listing['rows']!r}")
        assert listing["rows"][0][kind_col] == "btree", (
            f"AC66: the alter must have changed the index kind; got "
            f"{listing['rows'][0]!r}")

    def test_ac66_a_failed_second_invocation_leaves_the_index_absent(self):
        """Nothing in Groadmap detects, reports, or repairs the gap: the caller
        learns of it from SHOW INDEXES, which is what this asserts.
        """
        self.ok("CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord)")
        assert self.ok("DROP INDEX spec_ord") == {"ok": True}

        # A definition the engine refuses: composite indexes are out of scope.
        code, _stdout, stderr = self.run("CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord, n.key)")
        assert code == EXIT_ENGINE, (
            f"AC66: a composite definition is refused by the engine with exit "
            f"{EXIT_ENGINE}; exit={code} stderr={stderr!r}")

        assert self.schema_names() == [], (
            "AC66: when the second invocation fails the index stays absent, and "
            "no rmp command reports the situation or repairs it")


class TestGraphSchemaFailureClasses(SchemaTestBase):
    """AC67, AC68, AC69: what is refused, by whom, and with which exit code."""

    # ---- AC67: one statement per invocation ----------------------------

    def test_a_trailing_clause_after_ddl_executes_in_part_and_reports_success(self):
        """SPEC/GRAPH.md acceptance criterion 38, last bullet, and
        section "What Groadmap Does Not Check", item 6.

        This test asserted the opposite until sprint 41: Groadmap refused a DDL
        statement carrying a further clause, with exit 6 and a message naming
        the trailing text. That refusal was WITHDRAWN, and what it protected
        against is now specified as a hazard rather than prevented.

        The exit code alone establishes nothing here either -- it is 0 in both
        readings -- so what is asserted is the split outcome: the index IS
        created, the trailing clause is NOT run, and the caller is told the
        statement succeeded.
        """
        mixed = ("CREATE INDEX spec_key FOR (n:Spec) ON (n.key) "
                 "MATCH (m:Spec) SET m.reviewed = true")
        code, stdout, stderr = self.run(mixed)
        assert code == EXIT_OK, (
            f"the statement must execute rather than be refused; exit={code} "
            f"stderr={stderr!r}")
        assert '"ok": true' in stdout, (
            f"the caller is told the whole statement succeeded; got {stdout!r}")

        assert self.schema_names() == ["spec_key"], (
            f"the schema half of the statement must have run; "
            f"SHOW INDEXES reports {self.schema_names()!r}")
        reviewed = self.ok("MATCH (m:Spec) WHERE m.reviewed IS NOT NULL RETURN count(m)")
        assert reviewed["rows"][0][0] == 0, (
            f"the MATCH ... SET half must have been discarded by the engine's schema "
            f"parser, leaving `reviewed` absent on every node; got {reviewed['rows']!r}. "
            f"This is the hazard: half the statement never ran and nothing said so")

    def test_ac67_a_property_named_after_a_clause_keyword_is_accepted(self):
        """The opposite direction, and the half a keyword scan fails.

        A check that refused both would be worse than the defect, because it
        would deny the caller an index the engine would have created.
        """
        assert self.ok("CREATE INDEX spec_set FOR (n:Spec) ON (n.set)") == {"ok": True}
        assert self.schema_names() == ["spec_set"], (
            "AC67: an index on a property named after a clause keyword must be "
            "created and reported")

        # And the same for the other clause keywords a scan would look for, so
        # the acceptance is not an accident of the word `set`.
        for prop in ("match", "delete", "remove", "merge", "create"):
            assert self.ok(f"CREATE INDEX spec_{prop} FOR (n:Spec) ON (n.{prop})") == {"ok": True}
        assert sorted(self.schema_names()) == sorted(
            ["spec_set", "spec_match", "spec_delete", "spec_remove", "spec_merge",
             "spec_create"]), (
            f"AC67: every property named after a clause keyword must be "
            f"indexable; got {self.schema_names()!r}")

    def test_the_partial_execution_reaches_all_four_ddl_forms(self):
        """The hazard is a property of the engine's schema parser, so it holds
        for every DDL form the parser routes: two creates and two drops, each
        of which runs and each of which discards the clause after it."""
        self.ok("CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
        self.ok("CREATE CONSTRAINT spec_title_nn FOR (n:Spec) REQUIRE n.title IS NOT NULL")

        mixed = [
            "CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) MATCH (m:Spec) SET m.reviewed = true",
            "DROP INDEX spec_key MATCH (m:Spec) SET m.reviewed = true",
            "CREATE CONSTRAINT spec_ord_uq FOR (n:Spec) REQUIRE n.ord IS UNIQUE "
            "MATCH (m:Spec) SET m.reviewed = true",
            "DROP CONSTRAINT spec_title_nn MATCH (m:Spec) SET m.reviewed = true",
        ]
        for query in mixed:
            code, stdout, stderr = self.run(query)
            assert code == EXIT_OK, (
                f"{query!r} must execute; exit={code} stderr={stderr!r}")
            assert '"ok": true' in stdout, f"got stdout {stdout!r}"

        # The DDL half of each ran: spec_key was dropped, spec_ord created,
        # spec_title_nn dropped, spec_ord_uq created.
        # spec_key dropped, spec_ord created, and the UNIQUE constraint's own
        # backing index registered alongside it.
        assert sorted(self.schema_names()) == ["__uniq__Spec.ord", "spec_ord"], (
            f"each statement's DDL half must have run; SHOW INDEXES reports "
            f"{sorted(self.schema_names())!r}")
        assert self.schema_names("SHOW CONSTRAINTS") == ["spec_ord_uq"], (
            f"the DROP CONSTRAINT and the CREATE CONSTRAINT must both have run; "
            f"SHOW CONSTRAINTS reports {self.schema_names('SHOW CONSTRAINTS')!r}")

        # And not one of the four trailing clauses ran.
        reviewed = self.ok("MATCH (m:Spec) WHERE m.reviewed IS NOT NULL RETURN count(m)")
        assert reviewed["rows"][0][0] == 0, (
            "every trailing clause must have been discarded; one of the four ran")

    # ---- AC68: the failure classes and their exit codes -----------------

    def test_ac68_duplicate_create_and_drop_of_absent_are_engine_failures(self):
        """The two that look like input errors and are not.

        Groadmap cannot know whether an object exists without opening the store,
        so the check belongs where the knowledge is: both exit 1, not 6.
        """
        self.ok("CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")

        code, _stdout, stderr = self.run("CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
        assert code == EXIT_ENGINE, (
            f"AC68: a duplicate CREATE INDEX exits {EXIT_ENGINE}, not "
            f"{EXIT_GUARD_RAIL}; exit={code} stderr={stderr!r}")
        assert "database error" in stderr, (
            f"AC68: it is an engine failure, so it carries the database-error "
            f"class rather than the guard rail's validation error; got {stderr!r}")

        assert self.ok("CREATE INDEX IF NOT EXISTS spec_key FOR (n:Spec) ON (n.key)") == {"ok": True}

        code, _stdout, stderr = self.run("DROP INDEX no_such_index")
        assert code == EXIT_ENGINE, (
            f"AC68: DROP INDEX of an absent object exits {EXIT_ENGINE}, not "
            f"{EXIT_GUARD_RAIL}; exit={code} stderr={stderr!r}")
        assert self.ok("DROP INDEX no_such_index IF EXISTS") == {"ok": True}

        # The store is unchanged by the two failures and the two no-ops.
        assert self.schema_names() == ["spec_key"], (
            f"AC68: got {self.schema_names()!r}")

    def test_ac68_unsupported_definitions_are_engine_failures(self):
        for query in (
            "CREATE INDEX spec_ck FOR (n:Spec) ON (n.key, n.title)",
            "CREATE INDEX rel_since FOR ()-[e:DEPENDS_ON]-() ON (e.since)",
            "CREATE CONSTRAINT spec_nk FOR (n:Spec) REQUIRE (n.key, n.title) IS UNIQUE",
        ):
            code, stdout, stderr = self.run(query)
            assert code == EXIT_ENGINE, (
                f"AC68: {query!r} is a definition the engine does not support and "
                f"must exit {EXIT_ENGINE}; exit={code} stderr={stderr!r}")
            assert stdout.strip() == "", f"AC68: got stdout {stdout!r}"
        assert self.schema_names() == [] and self.schema_names("SHOW CONSTRAINTS") == [], (
            "AC68: a refused definition registers nothing")

    def test_ac68_a_constraint_the_data_does_not_satisfy_registers_nothing(self):
        """The failure class `graph execute` did not previously have.

        The engine validates the graph's current data before registering a
        constraint. Groadmap's obligation is to surface that diagnostic intact,
        so the caller learns WHICH rule failed and on WHICH property.
        """
        self.ok("CREATE (:Spec {key:'user-authentication', ord:9})")

        code, stdout, stderr = self.run("CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE")
        assert code == EXIT_ENGINE, (
            f"AC68: a constraint the data does not satisfy exits {EXIT_ENGINE}; "
            f"exit={code} stderr={stderr!r}")
        assert stdout.strip() == "", f"AC68: got stdout {stdout!r}"
        assert "UNIQUE" in stderr and "key" in stderr, (
            f"AC68: the engine's diagnostic must reach the caller intact, naming "
            f"the rule and the property; got {stderr!r}")
        assert self.schema_names("SHOW CONSTRAINTS") == [], (
            "AC68: nothing is registered when the validation fails")

        # Presence rules fail the same way, on a property some node lacks.
        code, _stdout, stderr = self.run("CREATE CONSTRAINT spec_status_nn FOR (n:Spec) REQUIRE n.status IS NOT NULL")
        assert code == EXIT_ENGINE, (
            f"AC68: a presence rule over a property some node lacks exits "
            f"{EXIT_ENGINE}; exit={code} stderr={stderr!r}")
        assert self.schema_names("SHOW CONSTRAINTS") == [], (
            "AC68: nothing is registered when the validation fails")

    def test_the_retired_subcommand_names_are_the_other_exit_code(self):
        """What used to distinguish the engine's 1 from the guard rail's 6.

        This test drove five operation-class mismatches, one per subcommand, and
        required each to exit 6 with a validation-error message. There is no
        operation class and there are no five subcommands, so the contrast the
        criterion is about has moved: the exit code a caller now meets for
        naming one of the retired subcommands is 127, the dispatch failure, and
        it is worth asserting beside the engine's 1 for the same reason the old
        pair was -- a reader who sees only one of them learns nothing.
        """
        for name in ("create", "query", "update", "delete", "search"):
            code, stdout, stderr = self.test.run_cmd(
                ["graph", name, "-r", self.roadmap, "--query",
                 "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"], check=False)
            assert code == 127, (
                f"`rmp graph {name}` exits 127 as an unresolved subcommand; "
                f"exit={code} stderr={stderr!r}")
            assert "validation error" not in stderr, (
                f"a dispatch failure is not a validation failure; got {stderr!r}")
            assert stdout.strip() == "", f"got stdout {stdout!r}"

        assert self.schema_names() == [] and self.node_count() == 3, (
            "an unresolved subcommand never reaches the graph store, so nothing changed")

    # ---- AC69: DDL the engine will not route to its schema parser -------

    def test_ac69_badly_spaced_ddl_is_refused_by_the_engine(self):
        """SPEC/GRAPH.md section "What Groadmap Does Not Check", item 7.

        The engine routes a statement to its schema parser by testing it against
        literal prefixes carrying exactly one space. A statement that misses
        those prefixes by its spacing goes to the general Cypher grammar, which
        has no such production and rejects it with a diagnostic that names the
        keyword rather than the spacing.

        Groadmap used to admit these statements through a deliberately wide DDL
        matcher and let the engine refuse them, which produced the same outcome
        by a different route. It now inspects nothing at all, so the outcome is
        the engine's alone. The cost is a misleading diagnostic and exit 1 --
        not a schema change that slipped through, which is what the assertions
        after the exit code establish.
        """
        before_nodes, before_edges = self.node_count(), self.edge_count()

        spellings = [
            "CREATE   INDEX spec_key FOR (n:Spec) ON (n.key)",
            "CREATE\tINDEX spec_key FOR (n:Spec) ON (n.key)",
            "CREATE\nINDEX spec_key FOR (n:Spec) ON (n.key)",
            "CREATE /* which one? */ INDEX spec_key FOR (n:Spec) ON (n.key)",
            "DROP   INDEX spec_key",
            "DROP\tINDEX spec_key",
            "CREATE   CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE",
            "DROP  CONSTRAINT spec_key_uq",
        ]
        for query in spellings:
            code, stdout, stderr = self.run(query)
            assert code == EXIT_ENGINE, (
                f"AC69: {query!r} must fail with exit {EXIT_ENGINE}, not "
                f"{EXIT_GUARD_RAIL}: Groadmap inspects nothing and the engine "
                f"refuses it; exit={code} stderr={stderr!r}")
            assert "validation error" not in stderr, (
                f"AC69: the refusal must be the engine's, not a validation "
                f"message of Groadmap's; got {stderr!r}")
            assert stdout.strip() == "", (
                f"AC69: {query!r} must produce no stdout; got {stdout!r}")

        assert self.schema_names() == [] and self.schema_names("SHOW CONSTRAINTS") == [], (
            "AC69: none of those statements may have registered a schema object")
        assert (self.node_count(), self.edge_count()) == (before_nodes, before_edges), (
            "AC69: the graph's node and relationship counts must be what they were")


class TestGraphSchemaOnEverySurface(SchemaTestBase):
    """AC64: every surface that can report the schema reports the same schema.

    The exit code establishes nothing here. An engine constructed WITHOUT the
    recovered schema answers the identical query with zero rows and exits 0, so
    success is exactly what the defect returns -- the rows are compared instead.

    Two surfaces remain: `rmp graph execute` and the web graph data endpoint.
    """

    def setup_method(self):
        super().setup_method()
        self._procs = []
        self.ok("CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
        self.ok("CREATE CONSTRAINT spec_title_nn FOR (n:Spec) REQUIRE n.title IS NOT NULL")

    def teardown_method(self):
        for proc in self._procs:
            try:
                proc.terminate()
                proc.wait(timeout=5)
            except Exception:  # noqa: BLE001
                proc.kill()
        super().teardown_method()

    # ---- helpers -----------------------------------------------------

    def _start_web(self):
        """Launch `rmp web` against this test's HOME and return its port."""
        out = tempfile.TemporaryFile(mode="w+")
        err = tempfile.TemporaryFile(mode="w+")
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        proc = subprocess.Popen(
            [self.test.cli_path, "web", "--no-open", "--host", "127.0.0.1", "--port", "0"],
            stdout=out, stderr=err, text=True, env=env)
        self._procs.append(proc)

        deadline = time.time() + 15.0
        while time.time() < deadline:
            out.seek(0)
            body = out.read()
            if "\"url\"" in body:
                try:
                    url = json.loads(body)["url"]
                except json.JSONDecodeError:
                    time.sleep(0.05)
                    continue
                return int(url.rsplit(":", 1)[1])
            if proc.poll() is not None:
                err.seek(0)
                raise AssertionError(f"rmp web exited early: {err.read()!r}")
            time.sleep(0.05)
        raise AssertionError("rmp web did not print a startup URL within 15s")

    def _graph_data(self, port, query):
        path = (f"/roadmaps/{self.roadmap}/graph/data?"
                + urllib.parse.urlencode({"q": query}))
        conn = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
        try:
            conn.request("GET", path)
            resp = conn.getresponse()
            return resp.status, resp.read().decode("utf-8", "replace")
        finally:
            conn.close()

    # ---- the comparison ----------------------------------------------

    def test_the_cli_reports_the_schema_the_store_holds_across_the_process_boundary(self):
        """SPEC/GRAPH.md section "Recovered Schema on Every Surface".

        This compared THREE CLI surfaces -- `graph update`, `graph query` and
        `graph search` -- against each other. There is one CLI surface now, so
        the comparison that replaces it is against a SECOND OPENING of the
        store: each read below is its own process, and the schema it reports is
        the schema recovery reconstructed from disk rather than anything an
        earlier invocation left in memory.

        The exit code establishes nothing here either. An engine constructed
        WITHOUT the recovered schema answers the identical statement with zero
        rows and exits 0, so success is exactly what the defect returns; the
        ROWS are compared, and a declared name is what they must carry.
        """
        for statement, declared in (("SHOW INDEXES", "spec_key"),
                                    ("SHOW CONSTRAINTS", "spec_title_nn")):
            first = self.ok(statement)
            second = self.ok(statement)

            names = [row[0] for row in first["rows"]]
            assert names == [declared], (
                f"`graph execute --query {statement!r}` reported {names!r}, which is "
                f"not the single declared name [{declared!r}]. Zero rows and exit 0 is "
                f"exactly what a surface constructed without the recovered schema "
                f"returns, so an empty listing would make this comparison vacuous")
            assert (first["columns"], first["rows"]) == (second["columns"], second["rows"]), (
                f"two separate invocations disagree about {statement!r}: "
                f"{first!r} then {second!r}")

    def test_the_name_reported_is_the_one_the_caller_declared(self):
        """Not a name synthesised by the engine: an object created under a
        declared name must be reported under it, which is what makes the
        comparison above a statement about the recovered DEFINITIONS rather
        than about a listing rebuilt from the data.
        """
        assert self.schema_names("SHOW INDEXES") == ["spec_key"], (
            "`graph execute` must report the declared index name")
        assert self.schema_names("SHOW CONSTRAINTS") == ["spec_title_nn"], (
            "`graph execute` must report the declared constraint name")

    def test_ac64_the_web_endpoint_answers_an_empty_graph_and_the_cli_the_rows(self):
        """The fourth surface, and why it is asserted differently.

        The graph data endpoint's response shape is {"nodes", "edges"} and
        carries no tabular rows (SPEC/DATA_FORMATS.md "Graph View Data"), so it
        has nowhere to put a schema listing. It EXECUTES the statement like any
        other and answers HTTP 200 with {"nodes": [], "edges": []}, because the
        rows the statement returns carry no node and no edge (SPEC/WEB.md AC156
        and AC157, canonical for the endpoint's half of AC64).

        The empty answer is only defensible ALONGSIDE the CLI read, and the two
        are asserted together for that reason: the endpoint's answer is empty
        because of its response shape, not because the store's schema is empty,
        and the three CLI assertions above are what establish the difference.
        Asserting that the endpoint reports the index row MUST fail this test.

        This test has asserted the opposite twice before. The endpoint answered
        HTTP 200 with an empty graph and nothing beside it (the defect of rmp
        task #344, an empty graph reporting success with no way to tell it from
        a query that matched nothing); then it refused the class outright with
        kind schema_introspection; and the guard rail is now withdrawn (rmp task
        #364), so the empty graph is back -- this time as the specified answer,
        with the CLI read named as where the listing is obtained.
        """
        port = self._start_web()

        for statement in ("SHOW INDEXES", "SHOW INDEX",
                          "SHOW CONSTRAINTS", "SHOW CONSTRAINT",
                          "SHOW INDEXES YIELD name RETURN name"):
            status, body = self._graph_data(port, statement)
            assert status == 200, (
                f"AC64/AC157: the endpoint executes {statement!r} and answers "
                f"200; got {status} {body!r}")
            assert json.loads(body) == {"nodes": [], "edges": []}, (
                f"AC64/AC157: {statement!r} returns tabular rows the response "
                f"shape cannot carry, so the answer is the empty graph; "
                f"got {body!r}")

        # The same class at a spacing the ENGINE does not route to its schema
        # parser. The endpoint holds no opinion about the spacing and hands the
        # statement to the engine, which fails it: HTTP 400, kind execution, the
        # engine's own diagnostic (SPEC/WEB.md AC151).
        status, body = self._graph_data(port, "SHOW  INDEXES")
        assert status == 400, (
            f"AC151: a badly spaced SHOW is not routed to the engine's schema "
            f"parser and fails there; got {status} {body!r}")
        err = json.loads(body)
        assert err.get("kind") == "execution", (
            f"AC151: the failure is the engine's, so the kind is execution; "
            f"got {err!r}")
        assert set(err) == {"error", "kind"}, (
            f"AC151: the failure body carries neither nodes nor edges; got {err!r}")
        assert "cypher:" in err["error"], (
            f"AC151: the message must be the engine's own diagnostic; got "
            f"{err['error']!r}")

        # The other half of AC157, without which the empty answers above are
        # consistent with a store that simply has no schema: the CLI answers the
        # identical statement with the rows.
        assert self.schema_names("SHOW INDEXES") == ["spec_key"], (
            "AC157: `rmp graph execute` must report the declared index, which "
            "is what makes the endpoint's empty answer a property of its "
            "response shape rather than of the store")

        # The control: the same endpoint, the same store, an ordinary read. It
        # returns the seeded graph, which is what makes the empty answers above
        # a fact about a CLASS of statement rather than about an endpoint that
        # answers everything empty or a store that is empty.
        status, body = self._graph_data(port, "MATCH (n:Spec) RETURN n")
        assert status == 200, f"AC64: got {status} {body!r}"
        nodes = json.loads(body)["nodes"]
        assert len(nodes) == 3, (
            f"AC64: the endpoint reads the same store the schema was declared "
            f"on, which holds three Spec nodes; got {len(nodes)}")

        # And the schema is still there afterwards: a statement that wrote
        # nothing neither checkpointed nor truncated, so nothing was lost.
        assert self.schema_names("SHOW INDEXES") == ["spec_key"], (
            "AC157: a statement that writes nothing changes nothing")
        assert self.schema_names("SHOW CONSTRAINTS") == ["spec_title_nn"], (
            "AC157: the declared constraint survives too")


def _run_all():
    """Discover and run every Test* class defined in this module.

    Enumerating the module's own namespace rather than naming the classes in a
    fixed list means a class added later cannot silently fail to run.
    """
    passed = failed = 0
    failures = []
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
    print(f"Graph schema-management tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
