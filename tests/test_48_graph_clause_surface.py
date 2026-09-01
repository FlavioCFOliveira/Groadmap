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
  (exit 0, columns/rows payload) as the read it is, accepted by `update` because
  that subcommand owns the graph's schema, and rejected by `create` and `delete`
  (exit 6, each subcommand's own message).

- FOREACH is an updating clause with no discriminator of its own: it is
  classified by the writing clauses its body carries. That containment is an
  emergent property of the grammar, so it is asserted here rather than assumed.

The schema-MUTATING DDL forms must stay rejected by `query`, `search`, `create`
and `delete`, in any case and with any spacing between the two keywords — the
introspection class and the schema subcommand must not loosen that rejection.
`graph update` is the one subcommand that accepts the DDL class, by decision
(SPEC/GRAPH.md § Per-Subcommand Validation Rules note 5, § Schema Management,
Acceptance Criteria 24 and 27), and its acceptance is asserted here beside the
four refusals so that neither can drift without failing a test.

A third family belongs here for the same reason: the relationship-write-
direction check (SPEC/GRAPH.md § Relationship Write Direction, Acceptance
Criteria 28-30, rmp task #193) is, like the two above, a rule about the
pinned GoGraph engine's own behaviour rather than about Groadmap's clause
grammar. `graph update` accepted a SET or REMOVE whose relationship target
was bound by an incoming or undirected pattern, and reported unqualified
success while GoGraph's write path silently dropped the change — the
engine's own write-effect counters cannot detect the drop, so there is no
post-hoc signal to test and the shape is refused before the store is opened.
The check also treats a FOREACH body exactly as the clause-class rule above
does — `FOREACH (x IN list | SET e.k = …)` is inspected like a top-level
SET, for the same containment reason. `TestGraphRelationshipWriteDirection`
below pins that a reverse-bound SET/REMOVE is refused (exit 6) and writes
nothing, that the outgoing form and node writes reached through a reverse
traversal keep working, and that the refusal message names the offending
direction and the outgoing rewrite.
"""

import inspect
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

# The per-subcommand guard-rail message each write subcommand produces when a
# query's operation class is outside every class that subcommand accepts.
# `graph update` names three classes because it accepts three.
WRITE_SUBCOMMAND_MESSAGE = {
    "create": "graph create accepts only CREATE/MERGE queries",
    "update": ("graph update accepts only SET/REMOVE, index/constraint DDL, "
               "and schema-introspection queries"),
    "delete": "graph delete accepts only DELETE/DETACH DELETE queries",
}

# The write subcommands that reject the two SCHEMA classes -- introspection and
# schema-mutating DDL -- on their operation class. `graph update` is absent
# because it accepts both: it is the subcommand through which the graph's schema
# is managed (SPEC/GRAPH.md § Schema Management).
NON_SCHEMA_WRITE_SUBCOMMANDS = ("create", "delete")

# The subcommands that reject schema-mutating DDL. `graph update` is the single
# exception, and Acceptance Criterion 27 puts it outside the criterion in as many
# words: "graph update is outside this criterion, and deliberately so".
DDL_REJECTING_SUBCOMMANDS = ("query", "search", "create", "delete")


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
        # row set is empty because this suite's fixture declares no schema --
        # Groadmap creates no index of its own accord, so a graph has exactly
        # the schema its owner asked for (SPEC/GRAPH.md § Schema Management).
        # Indexes that ARE created are exercised in
        # tests/test_64_graph_schema_management.py.
        result = self.json("query", "SHOW INDEXES")
        assert result["columns"] == [
            "name", "state", "type", "entityType", "labelsOrTypes", "properties",
        ], f"AC23: unexpected SHOW INDEXES columns: {result['columns']!r}"
        assert result["rows"] == [], (
            f"AC23: this fixture declares no index, so SHOW INDEXES must list "
            f"none; got {result['rows']!r}")

    def test_ac23_show_constraints_returns_the_documented_columns(self):
        result = self.json("query", "SHOW CONSTRAINTS")
        assert result["columns"] == [
            "name", "type", "entityType", "labelsOrTypes", "properties",
        ], f"AC23: unexpected SHOW CONSTRAINTS columns: {result['columns']!r}"
        assert result["rows"] == [], (
            f"AC23: this fixture declares no constraint, so SHOW CONSTRAINTS "
            f"must list none; got {result['rows']!r}")

    def test_ac23_projection_tail_selects_the_yielded_columns(self):
        # The YIELD / RETURN tail must actually project, not merely parse.
        result = self.json("query", "SHOW INDEXES YIELD name RETURN name")
        assert result["columns"] == ["name"], (
            f"AC23: the projection tail must reduce the result to the yielded "
            f"column; got {result['columns']!r}")

    # ---- AC 24: introspection is rejected by the write subcommands ---

    def test_ac24_create_and_delete_reject_introspection_and_update_accepts_it(self):
        """AC24 requires the two rejections and the one acceptance TOGETHER.

        Asserting them in one test is what makes widening the class on `create`
        or `delete` -- or narrowing it on `update` -- fail, which two separate
        tests would not: a change that moved the class from one group to the
        other could be made to pass by editing whichever test it broke.
        """
        for subcmd in NON_SCHEMA_WRITE_SUBCOMMANDS:
            message = WRITE_SUBCOMMAND_MESSAGE[subcmd]
            for query in ("SHOW INDEXES", "SHOW CONSTRAINTS"):
                code, _stdout, stderr = self.run(subcmd, query)
                assert code == EXIT_GUARD_RAIL, (
                    f"AC24: `graph {subcmd}` must reject {query!r} with exit 6; "
                    f"exit={code} stderr={stderr!r}")
                assert message in stderr, (
                    f"AC24: `graph {subcmd}` must reject {query!r} with its own "
                    f"guard-rail message {message!r}; got {stderr!r}")

        # `graph update` accepts the class and returns the listing. The exit code
        # alone would not establish it: the assertion is on the payload, so a
        # subcommand that accepted the statement and produced nothing would fail.
        for query in ("SHOW INDEXES", "SHOW CONSTRAINTS"):
            code, stdout, stderr = self.run("update", query)
            assert code == EXIT_OK, (
                f"AC24: `graph update` must ACCEPT {query!r} -- it is the "
                f"subcommand through which the schema is managed; "
                f"exit={code} stderr={stderr!r}")
            assert '"columns"' in stdout and '"rows"' in stdout, (
                f"AC24: `graph update` on {query!r} must return the schema "
                f"listing in the columns/rows shape, not {{\"ok\": true}}; "
                f"got {stdout!r}")

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
        for subcmd in DDL_REJECTING_SUBCOMMANDS:
            for query in ddl_queries:
                code, _stdout, stderr = self.run(subcmd, query)
                assert code == EXIT_GUARD_RAIL, (
                    f"AC27: `graph {subcmd}` must reject the schema-mutating DDL "
                    f"{query!r} with exit 6 whatever its case or spacing; "
                    f"exit={code} stderr={stderr!r}")
                assert "validation error" in stderr, (
                    f"AC27: expected a guard-rail validation error; got {stderr!r}")

        # `graph update` is outside AC27 by name, because it accepts the class
        # (AC62 and AC69). Asserting that here rather than leaving it implicit
        # is what stops the exclusion being read as an oversight: every one of
        # these spellings is ADMITTED by the guard rail there, and each then
        # either runs or is refused by the ENGINE with exit 1 -- never with the
        # guard rail's exit 6.
        for query in ddl_queries:
            code, _stdout, stderr = self.run("update", query)
            assert code != EXIT_GUARD_RAIL, (
                f"AC27/AC62: `graph update` accepts the DDL class, so {query!r} "
                f"must not be refused by the guard rail; exit={code} "
                f"stderr={stderr!r}")

    def test_ac27_ddl_rejection_leaves_the_schema_untouched(self):
        # The rejection must happen before execution: no index may exist after
        # attempting to create one, which SHOW INDEXES now lets us verify
        # directly rather than by inference.
        self.run("create", "CREATE INDEX spec_key_idx FOR (n:Spec) ON (n.key)")
        result = self.json("query", "SHOW INDEXES")
        assert result["rows"] == [], (
            f"AC27: a rejected CREATE INDEX must not have reached the engine; "
            f"SHOW INDEXES lists {result['rows']!r}")


    # ---- Guard-rail bypass: DDL keywords spelled with non-ASCII letters ----

    def test_spoofed_ddl_keywords_are_rejected_by_every_subcommand(self):
        """A DDL keyword whose letters include a non-ASCII code point that
        Unicode UPPERCASING maps onto ASCII must still be classified as DDL.

        The engine decides DDL on strings.ToUpper, which maps U+0131 (dotless i)
        onto 'I' and U+017F (long s) onto 'S'; a case-insensitive regexp folds
        instead, and did not see those spellings. A security audit proved the
        divergence end to end against this binary: `graph query` accepted
        "CREATE \u0131NDEX ..." with exit 0 and the engine executed it through
        its DDL executor, and the DROP form reached the DropIndex executor.
        """
        spoofed = [
            "CREATE \u0131NDEX spec_key_idx FOR (n:Spec) ON (n.key)",
            "CREATE \u0131NDEX IF NOT EXISTS spec_key_idx FOR (n:Spec) ON (n.key)",
            "DROP \u0131NDEX spec_key_idx",
            "drop \u0131ndex spec_key_idx",
            "CREATE CONSTRA\u0131NT unique_spec_key FOR (n:Spec) REQUIRE n.key IS UNIQUE",
            "DROP CONSTRA\u0131NT unique_spec_key",
            "CREATE CON\u017fTRAINT unique_spec_key FOR (n:Spec) REQUIRE n.key IS UNIQUE",
        ]
        for subcmd in DDL_REJECTING_SUBCOMMANDS:
            for query in spoofed:
                code, _stdout, stderr = self.run(subcmd, query)
                assert code == EXIT_GUARD_RAIL, (
                    f"`graph {subcmd}` must reject the spoofed DDL {query!r} with "
                    f"exit 6: the engine executes it as schema DDL; "
                    f"exit={code} stderr={stderr!r}")
                assert "validation error" in stderr, (
                    f"expected a guard-rail validation error; got {stderr!r}")

        # The bypass being guarded against was never "the statement runs"; it was
        # "the guard rail and the engine disagree about what the statement IS".
        # On `graph update` that disagreement would be just as real: the
        # statement would be refused as a data-writing CREATE while the engine
        # executed it as schema DDL. So what must hold there is that the guard
        # rail CLASSIFIES these as DDL, which is what admitting them under the
        # schema subcommand means -- a missed classification surfaces as the
        # class refusal, exit 6.
        for query in spoofed:
            code, _stdout, stderr = self.run("update", query)
            assert code != EXIT_GUARD_RAIL, (
                f"`graph update` refused the spoofed DDL {query!r} with exit 6, "
                f"so the guard rail did not see the schema statement the engine "
                f"sees; stderr={stderr!r}")

    def test_spoofed_ddl_never_reaches_the_engine(self):
        """The rejection happens before execution: no index is created."""
        code, stdout, _stderr = self.run(
            "query", "CREATE \u0131NDEX spec_key_idx FOR (n:Spec) ON (n.key)")
        assert code == EXIT_GUARD_RAIL, (
            f"spoofed CREATE INDEX on the read path must be refused; "
            f"exit={code} stdout={stdout!r}")
        result = self.json("query", "SHOW INDEXES")
        assert result["rows"] == [], (
            f"a refused spoofed CREATE INDEX must not have reached the engine; "
            f"SHOW INDEXES lists {result['rows']!r}")

    def test_unicode_identifiers_are_still_ordinary_reads(self):
        """The stricter classification must not reject legitimate non-ASCII text."""
        reads = [
            "MATCH (n:Yaz\u0131l\u0131m) RETURN n",
            "MATCH (s:Spec) WHERE s.key = 'CREATE \u0131NDEX x' RETURN s.key",
            "MATCH (s:Spec) RETURN s.key // CREATE \u0131NDEX x",
        ]
        for query in reads:
            code, _stdout, stderr = self.run("query", query)
            assert code == EXIT_OK, (
                f"an ordinary read must stay admissible: {query!r}; "
                f"exit={code} stderr={stderr!r}")


# ---- Schema-introspection keyword spacing (SPEC/GRAPH.md § Keyword Spacing
# in a Schema-Introspection Command, Acceptance Criterion 39, rmp task #275) --

# The four target keywords the schema-introspection class covers. Both
# singulars appear beside both plurals because the guard rail spells the two
# plurals with different patterns -- INDEX(ES)? against CONSTRAINTS? -- so a
# regression in either spelling would drop one form while the other kept
# working.
INTROSPECT_KEYWORDS = ("INDEXES", "INDEX", "CONSTRAINTS", "CONSTRAINT")

# Every separator that is not exactly one space, each of which the engine's
# prefix test misses and its general grammar then refuses.
MISSPACED_SEPARATORS = (
    ("two spaces", "  "),
    ("a tab", "\t"),
    ("a line break", "\n"),
    ("four spaces", "    "),
)

# The four fragments the guard rail's own refusal must carry: the class it read
# the statement as, the rule, the name of the rule, and the accepted spelling
# (appended per case, since it varies with the keyword).
SPACING_MESSAGE_FRAGMENTS = (
    "validation error",
    "schema-introspection command",
    "exactly one space",
    "keyword spacing",
)

# Fragments the refusal must NEVER carry. The first three are the engine's own
# parse diagnostic -- the misdiagnosis this criterion exists to remove, which
# lists every clause keyword EXCEPT SHOW and so reads as though schema
# introspection were unsupported. The fourth is the false classification: a SHOW
# statement reads the schema and writes nothing whatever its spacing.
FORBIDDEN_MESSAGE_FRAGMENTS = (
    "cypher: parse",
    'unexpected "SHOW"',
    "database error",
    "not read-only",
)


class TestGraphIntrospectionKeywordSpacing:
    """SPEC/GRAPH.md § Keyword Spacing in a Schema-Introspection Command.

    End-to-end half of Acceptance Criterion 39, against the compiled ./bin/rmp.

    The defect (rmp task #275): `rmp graph query --query "SHOW  INDEXES"` --
    two spaces -- exited 1 with

        Error: database error: graph query failed: cypher: parse: unexpected
        "SHOW" at 1:0, expected one of {CALL, YIELD, CREATE, DELETE, ...}

    a list naming every clause keyword EXCEPT SHOW, while the identical
    statement with one space returned its result set. The guard rail admitted
    the statement because its introspection matcher tolerated arbitrary
    whitespace between the two keywords, whereas GoGraph routes a statement to
    its introspection parser by testing it against the literal prefixes
    "SHOW INDEX" and "SHOW CONSTRAINT", each carrying exactly one space. The two
    disagreed about the same input, and the user was handed a diagnostic that
    named the wrong problem and offered no route to the real cause.

    Why this belongs in THIS module rather than in a module of its own: the rule
    narrows the very class Acceptance Criteria 23-25 establish here. It is the
    same integration surface -- what the pinned engine accepts -- and the
    boundary being asserted is the boundary between AC23 (one space: accepted,
    executes, returns columns/rows) and AC39 (any other separator: refused by
    the guard rail before the engine sees it).

    The claim is a PAIRING, and each test asserts both halves against the same
    keyword, because either half alone is satisfiable by a broken binary: a
    build that had dropped schema introspection altogether would pass every
    rejection assertion, and the pre-fix build passed every acceptance one.
    """

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        # A real store, so the accepted spelling genuinely executes against a
        # graph rather than being accepted for the want of one.
        self.test.run_cmd([
            "graph", "create", "-r", self.roadmap, "--query",
            "CREATE (:Spec {key:'schema-introspection-spacing', status:'draft'})",
        ])

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers -----------------------------------------------------

    def run(self, subcmd, query):
        return self.test.run_cmd(
            ["graph", subcmd, "-r", self.roadmap, "--query", query], check=False)

    def assert_guard_rail_refusal(self, subcmd, query, accepted):
        """Assert the guard rail itself refused query, with its own message."""
        code, stdout, stderr = self.run(subcmd, query)
        assert code == EXIT_GUARD_RAIL, (
            f"AC39: `graph {subcmd}` must refuse {query!r} with exit "
            f"{EXIT_GUARD_RAIL}; exit={code} stdout={stdout!r} stderr={stderr!r}")
        # Exit 1 is the engine's (SPEC/GRAPH.md AC11) and is NOT this failure:
        # a statement refused here never reaches the parser.
        assert code != 1, (
            f"AC39: {query!r} must not carry the exit code of an engine parse "
            f"failure; the guard rail refuses it first")
        for fragment in SPACING_MESSAGE_FRAGMENTS + (accepted,):
            assert fragment in stderr, (
                f"AC39: the refusal of {query!r} must name {fragment!r}; "
                f"got {stderr!r}")
        for fragment in FORBIDDEN_MESSAGE_FRAGMENTS:
            assert fragment not in stderr, (
                f"AC39: the refusal of {query!r} must never carry {fragment!r} "
                f"-- that is the engine's misdiagnosis this criterion removes; "
                f"got {stderr!r}")
        # Refused before execution, so the command produced no result.
        assert stdout.strip() == "", (
            f"AC39: a refused query must write nothing to stdout; got {stdout!r}")

    def assert_accepted_and_executed(self, subcmd, query):
        """Assert query was accepted AND actually ran, returning a result set."""
        code, stdout, stderr = self.run(subcmd, query)
        assert code == EXIT_OK, (
            f"AC39: `graph {subcmd}` must accept the single-space spelling "
            f"{query!r}; exit={code} stderr={stderr!r}")
        assert '"columns"' in stdout and '"rows"' in stdout, (
            f"AC39: `graph {subcmd}` on {query!r} must return the columns/rows "
            f"result shape, so the acceptance is not merely a zero exit; "
            f"got {stdout!r}")

    # ---- AC 39: one space accepted, every other separator refused ----

    def test_ac39_one_space_accepted_every_other_separator_refused(self):
        """The core criterion, for all four keywords under both read
        subcommands: exactly one space executes, and two spaces, a tab and a
        line break are each refused with exit 6 and the guard rail's message.

        Both halves are asserted against the SAME keyword and the SAME
        subcommand, so the pair differs in the separator and in nothing else.
        """
        for subcmd in ("query", "search"):
            for keyword in INTROSPECT_KEYWORDS:
                accepted = f"SHOW {keyword}"
                self.assert_accepted_and_executed(subcmd, accepted)
                for _label, sep in MISSPACED_SEPARATORS:
                    self.assert_guard_rail_refusal(
                        subcmd, f"SHOW{sep}{keyword}", accepted)

    def test_ac39_refusal_folds_keyword_case(self):
        """The rule folds case exactly as the engine's prefix test does, so a
        lowercase or mixed-case statement is refused for its spacing rather than
        admitted by a case accident -- and the message names the CANONICAL
        spelling whatever case was typed, because it is built from the keywords
        and never echoed back from the query.
        """
        cases = (
            ("show  indexes", "SHOW INDEXES"),
            ("Show\tIndex", "SHOW INDEX"),
            ("sHoW\ncOnStRaInTs", "SHOW CONSTRAINTS"),
            ("SHOW    constraint", "SHOW CONSTRAINT"),
        )
        for subcmd in ("query", "search"):
            for query, accepted in cases:
                self.assert_guard_rail_refusal(subcmd, query, accepted)

    def test_ac39_only_the_keyword_separator_is_strict(self):
        """The rule governs the separator between the two keywords and nothing
        else about the statement's spacing.

        The engine trims leading whitespace and leading comments before its
        prefix test, and its SHOW parser is itself whitespace-tolerant once
        reached, so whitespace BEFORE the statement and whitespace AFTER the
        target keyword are both accepted. Refusing more than that separator
        would be Groadmap inventing a grammar of its own, which the
        specification forbids -- and would break spellings that work today.
        """
        for query in (
            "   SHOW INDEXES",
            "\n\t  SHOW CONSTRAINTS",
            "// which indexes exist?\nSHOW INDEXES",
            "/* schema check */ SHOW CONSTRAINTS",
            "SHOW INDEXES   YIELD name",
            "SHOW INDEXES YIELD name, type WHERE type = 'RANGE' RETURN name",
        ):
            for subcmd in ("query", "search"):
                self.assert_accepted_and_executed(subcmd, query)

    def test_ac39_a_comment_between_the_keywords_is_spacing(self):
        """Classification runs on the masked normalization, which neutralizes a
        comment to spaces, so a comment BETWEEN the two keywords reads as
        spacing rather than as an absent separator -- and spacing is what the
        engine refuses.
        """
        for subcmd in ("query", "search"):
            self.assert_guard_rail_refusal(
                subcmd, "SHOW /* which ones? */ INDEXES", "SHOW INDEXES")

    def test_ac39_create_and_delete_still_object_on_class(self):
        """The rule changed nothing for `graph create` and `graph delete`.

        Their objection to a SHOW statement is that it carries none of the
        data-writing clauses they accept, that objection is decided first, and
        it holds at every spacing (AC24). Implementing the spacing rule in the
        shared code path would have replaced each subcommand's own contract
        message with a spacing complaint that says nothing about why
        `graph create` refused a SHOW.

        `graph update` is not in this list, because it ACCEPTS the class and so
        has no class objection to make; it is bound by the spacing rule instead,
        which the test below asserts (AC39 names `graph update` alongside
        `graph query` and `graph search`).
        """
        for subcmd in NON_SCHEMA_WRITE_SUBCOMMANDS:
            message = WRITE_SUBCOMMAND_MESSAGE[subcmd]
            for query in ("SHOW INDEXES", "SHOW  INDEXES", "SHOW\tCONSTRAINT",
                          "SHOW\nINDEX"):
                code, _stdout, stderr = self.run(subcmd, query)
                assert code == EXIT_GUARD_RAIL, (
                    f"AC39: `graph {subcmd}` must reject {query!r} with exit "
                    f"{EXIT_GUARD_RAIL}; exit={code} stderr={stderr!r}")
                assert message in stderr, (
                    f"AC39: `graph {subcmd}` must reject {query!r} on its "
                    f"operation class with {message!r}, at any spacing; "
                    f"got {stderr!r}")
                assert "keyword spacing" not in stderr, (
                    f"AC39: `graph {subcmd}` must not answer a class objection "
                    f"with a spacing complaint; got {stderr!r}")

    def test_ac39_update_is_bound_by_the_spacing_rule(self):
        """`graph update` accepts the schema-introspection class, so the
        well-spaced spelling runs under it and the badly spaced one must be
        refused by the GUARD RAIL with the spacing named -- not admitted and
        left to die in the engine's parser under a diagnostic that lists every
        clause keyword except SHOW.
        """
        for keyword in ("INDEXES", "INDEX", "CONSTRAINTS", "CONSTRAINT"):
            accepted = "SHOW " + keyword
            code, stdout, stderr = self.run("update", accepted)
            assert code == EXIT_OK, (
                f"AC39: `graph update` must accept {accepted!r}; "
                f"exit={code} stderr={stderr!r}")
            assert '"columns"' in stdout, (
                f"AC39: {accepted!r} must return the listing under "
                f"`graph update`; got {stdout!r}")

            for sep in ("  ", "\t", "\n"):
                query = "SHOW" + sep + keyword
                code, stdout, stderr = self.run("update", query)
                assert code == EXIT_GUARD_RAIL, (
                    f"AC39: `graph update` must refuse {query!r} with exit "
                    f"{EXIT_GUARD_RAIL}, not leave it to the engine; "
                    f"exit={code} stderr={stderr!r}")
                assert "keyword spacing" in stderr and accepted in stderr, (
                    f"AC39: the refusal must name the keyword spacing and the "
                    f"accepted spelling {accepted!r}; got {stderr!r}")
                assert "cypher: parse" not in stderr, (
                    f"AC39: the user must not be shown the engine's parse "
                    f"diagnostic; got {stderr!r}")
                assert stdout.strip() == "", (
                    f"AC39: a refused query must produce no output; "
                    f"got {stdout!r}")

    def test_ac39_non_introspection_show_still_reaches_the_engine(self):
        """The refusal is confined to statements that ARE schema-introspection
        commands under some spacing.

        A near miss on the keyword and a SHOW family the engine does not
        implement must keep reaching the engine, which already names the real
        problem for them (SPEC/GRAPH.md § Per-Subcommand Validation Rules note
        3). A guard rail that started answering for those too would be inventing
        a grammar, and would hide the engine's correct diagnostic behind an
        incorrect one.
        """
        for query in ("SHOW  INDEXER", "SHOW  DATABASES", "SHOW  CONSTRAINTX"):
            for subcmd in ("query", "search"):
                code, _stdout, stderr = self.run(subcmd, query)
                assert code == 1, (
                    f"AC39: {query!r} is not a schema-introspection command "
                    f"under any spacing, so it must reach the engine and carry "
                    f"its exit code 1; exit={code} stderr={stderr!r}")
                assert "keyword spacing" not in stderr, (
                    f"AC39: the guard rail must not claim a spacing problem for "
                    f"{query!r}; got {stderr!r}")

    def test_ac39_refused_statement_never_reaches_the_engine(self):
        """The refusal precedes execution and precedes the store open.

        Asserted by observation rather than by inference: a misspaced SHOW
        CONSTRAINTS is refused, and the store it would have read is afterwards
        still readable and unchanged -- the seeded Spec node is still there and
        the schema is still empty.
        """
        self.assert_guard_rail_refusal(
            "query", "SHOW  CONSTRAINTS", "SHOW CONSTRAINTS")

        result = self.test.run_cmd_json([
            "graph", "query", "-r", self.roadmap, "--query",
            "MATCH (s:Spec) RETURN s.key AS k"])
        assert [row[0] for row in result["rows"]] == [
            "schema-introspection-spacing"], (
            f"AC39: a refused statement must leave the store untouched; "
            f"got {result!r}")

        result = self.test.run_cmd_json([
            "graph", "query", "-r", self.roadmap, "--query", "SHOW CONSTRAINTS"])
        assert result["rows"] == [], (
            f"AC39: the refused statement must not have reached the engine; "
            f"SHOW CONSTRAINTS lists {result['rows']!r}")


# ---- Relationship Write Direction (SPEC/GRAPH.md § Relationship Write
# Direction, Acceptance Criteria 28-30, rmp task #193) ---------------------

# `graph update` cannot write relationship "e" — the exact phrase every
# refusal in this section carries, whatever direction bound it.
REFUSAL_PREFIX = 'graph update cannot write relationship "e"'
OUTGOING_REWRITE_RECIPE = "anchor the outgoing pattern on that node instead of reversing the arrow"

# The read-direction rule (SPEC/GRAPH.md § Relationship Read Direction, rmp task
# #288) carries its own phrase, and the subcommand name varies because the rule
# covers `query`, `search` and `update`'s right-hand side alike. Matching on the
# verb keeps the two rules' refusals distinguishable: a test that expects a READ
# refusal must not pass on a WRITE refusal that happened to fire first.
READ_REFUSAL_PREFIX = 'cannot read relationship "e"' 


class TestGraphRelationshipWriteDirection:
    """SPEC/GRAPH.md § Relationship Write Direction fixes rmp task #193:
    `graph update` accepted a SET/REMOVE whose relationship target was bound
    by an incoming or undirected pattern, and reported `{"ok": true}` while
    GoGraph's write path silently dropped the change (its own write-effect
    counters cannot see the drop, so there is no post-hoc signal — the shape
    is refused before the graph store is opened at all).

    The fixture mirrors the one the Go regression suite
    (internal/commands/graph_relwrite_test.go) already pins at the unit
    level: a Spec node for this very rule and a Test node for the Go file
    that verifies it, joined by a single VERIFIED_BY edge running
    spec -> test, which is the literal example SPEC/GRAPH.md's Acceptance
    Criteria 28-30 use. What is new here is proving the same contract
    end-to-end against the compiled binary and a real graph store: every
    refused shape is confirmed to write NOTHING by reading the property back,
    and every shape that must keep working (the outgoing form from both
    endpoints, a node write reached through a reverse traversal, a
    relationship read only on the right-hand side of an assignment, and
    `graph delete`/`graph query` through an undirected pattern) is confirmed
    to keep working by reading its result back too — so a fix that refused
    too much, as well as one that refused too little, would fail this suite.
    """

    SPEC_KEY = "graph-write-direction"
    TEST_KEY = "graph_relwrite_test.go"

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        self.test.run_cmd([
            "graph", "create", "-r", self.roadmap, "--query",
            f"CREATE (:Spec {{key:'{self.SPEC_KEY}'}}), (:Test {{key:'{self.TEST_KEY}'}})",
        ])
        self.test.run_cmd([
            "graph", "create", "-r", self.roadmap, "--query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}}), (v:Test {{key:'{self.TEST_KEY}'}}) "
            "MERGE (s)-[:VERIFIED_BY]->(v)",
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

    def edge_property(self, name):
        """The value of `name` on the seeded VERIFIED_BY edge, read through
        the OUTGOING pattern that matches how it is actually stored — never
        through the undirected pattern the write side refuses, since the
        point of the read-back is to observe storage independently of it."""
        result = self.json(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:VERIFIED_BY]->(v) RETURN e.{name}",
        )
        assert result["rows"], f"the seeded edge itself is missing: {result!r}"
        return result["rows"][0][0]

    def node_property(self, label, key, name):
        result = self.json(
            "query", f"MATCH (n:{label} {{key:'{key}'}}) RETURN n.{name}")
        assert result["rows"], f"node {label}:{key!r} not found: {result!r}"
        return result["rows"][0][0]

    # ---- AC 29 / the recorded reproduction: reverse SET is refused ---

    def test_undirected_set_from_the_target_is_refused_and_writes_nothing(self):
        # The recorded reproduction, restated on this fixture: an undirected
        # pattern anchored on the relationship's TARGET.
        code, stdout, stderr = self.run(
            "update",
            f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})-[e]-(x) "
            "SET e.last_commit = '4f5ba9b'")
        assert code == 6, (
            f"AC29: an undirected SET on a relationship must be refused with "
            f"exit 6; exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert REFUSAL_PREFIX in stderr and "undirected pattern" in stderr, (
            f"AC29: expected the undirected-pattern refusal; got {stderr!r}")
        assert self.edge_property("last_commit") is None, (
            "AC29: a refused undirected SET must not have reached storage")

    def test_undirected_set_from_the_source_is_refused_although_it_would_have_written(self):
        # AC29's sharper case: anchored on the SOURCE, every row this
        # traversal matches runs forwards, so the write would in fact have
        # landed — and it is refused all the same, because whether an
        # undirected pattern writes depends on the data it meets, not on the
        # query, and that cannot be the guarantee.
        code, stdout, stderr = self.run(
            "update",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e]-(x) "
            "SET e.last_commit = '2c9f4b1'")
        assert code == 6, (
            f"AC29: an undirected SET anchored on the source must still be "
            f"refused; exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert "undirected pattern" in stderr, (
            f"AC29: expected the undirected-pattern refusal; got {stderr!r}")
        assert self.edge_property("last_commit") is None, (
            "AC29: the refusal must hold even though this traversal's rows "
            "would all have run forwards")

    def test_incoming_set_is_refused_from_the_target_anchor(self):
        code, stdout, stderr = self.run(
            "update",
            f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})<-[e:VERIFIED_BY]-(s) "
            "SET e.last_commit = 'a83e716'")
        assert code == 6, (
            f"AC29: an incoming SET anchored on the target must be refused; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert REFUSAL_PREFIX in stderr and "incoming pattern" in stderr, (
            f"AC29: expected the incoming-pattern refusal; got {stderr!r}")
        assert self.edge_property("last_commit") is None, (
            "AC29: a refused incoming SET must not have reached storage")

    def test_incoming_set_is_refused_from_the_source_anchor_too(self):
        # The trigger is the pattern's DIRECTION, not which endpoint anchors
        # it: this traversal starts at the SOURCE node and still uses the
        # `<-[e]-` arrow, so it is refused exactly like the target-anchored
        # form above.
        code, stdout, stderr = self.run(
            "update",
            f"MATCH (x)<-[e:VERIFIED_BY]-(s:Spec {{key:'{self.SPEC_KEY}'}}) "
            "SET e.last_commit = 'f01d922'")
        assert code == 6, (
            f"AC29: an incoming SET anchored on the source must be refused "
            f"too; exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert "incoming pattern" in stderr, (
            f"AC29: expected the incoming-pattern refusal; got {stderr!r}")
        assert self.edge_property("last_commit") is None, (
            "AC29: a refused incoming SET must not have reached storage "
            "regardless of which endpoint anchors the traversal")

    # ---- REMOVE is affected exactly as SET is -------------------------

    def test_remove_through_an_undirected_pattern_is_refused_and_removes_nothing(self):
        # Seed a real property through the one form that writes, so a
        # refused REMOVE has something it could wrongly have deleted.
        self.run(
            "update",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:VERIFIED_BY]->(v) "
            "SET e.last_commit = 'b6d40ce'", check=True)

        code, stdout, stderr = self.run(
            "update",
            f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})-[e]-(x) "
            "REMOVE e.last_commit")
        assert code == 6, (
            f"REMOVE through an undirected pattern must be refused exactly "
            f"as SET is; exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert REFUSAL_PREFIX in stderr and "undirected pattern" in stderr, (
            f"expected the undirected-pattern refusal on REMOVE; got {stderr!r}")
        assert self.edge_property("last_commit") == "b6d40ce", (
            "a refused REMOVE must leave the previously-set property intact")

    def test_remove_through_an_incoming_pattern_is_refused_and_removes_nothing(self):
        self.run(
            "update",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:VERIFIED_BY]->(v) "
            "SET e.last_commit = '9a2eb54'", check=True)

        code, stdout, stderr = self.run(
            "update",
            f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})<-[e:VERIFIED_BY]-(s) "
            "REMOVE e.last_commit")
        assert code == 6, (
            f"REMOVE through an incoming pattern must be refused exactly as "
            f"SET is; exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert REFUSAL_PREFIX in stderr and "incoming pattern" in stderr, (
            f"expected the incoming-pattern refusal on REMOVE; got {stderr!r}")
        assert self.edge_property("last_commit") == "9a2eb54", (
            "a refused REMOVE must leave the previously-set property intact")

    # ---- AC 30 / DELETE is NOT affected --------------------------------

    def test_delete_through_an_undirected_pattern_still_succeeds(self):
        # DELETE resolves the relationship itself rather than through the
        # endpoint columns the write path mishandles, so it must keep working
        # through the very shape SET and REMOVE just had refused.
        code, stdout, stderr = self.run(
            "delete", f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})-[e]-(x) DELETE e")
        assert code == 0, (
            f"AC30: DELETE through an undirected pattern must still succeed; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert '"ok": true' in stdout, f"AC30: expected ok JSON, got {stdout!r}"

        result = self.json(
            "query",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:VERIFIED_BY]->(v) RETURN e")
        assert result["rows"] == [], (
            f"AC30: the undirected DELETE must have removed the edge; "
            f"got {result!r}")
        # Only the relationship is gone; DELETE (not DETACH DELETE) leaves
        # both nodes standing.
        assert self.node_property("Spec", self.SPEC_KEY, "key") == self.SPEC_KEY
        assert self.node_property("Test", self.TEST_KEY, "key") == self.TEST_KEY

    # ---- AC 28: the outgoing form keeps writing from either endpoint --

    def test_outgoing_set_writes_and_reads_back_from_either_endpoint(self):
        code, stdout, stderr = self.run(
            "update",
            f"MATCH (s:Spec {{key:'{self.SPEC_KEY}'}})-[e:VERIFIED_BY]->(v) "
            "SET e.from_source = 'c77e410'")
        assert code == 0, (
            f"AC28: an outgoing SET anchored on the source must be accepted; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert self.edge_property("from_source") == "c77e410", (
            "AC28: the property set from the source anchor must read back")

        # The documented repair for an incoming edge: keep the arrow outgoing
        # and anchor on the node the edge arrives at instead.
        code, stdout, stderr = self.run(
            "update",
            f"MATCH (other)-[e:VERIFIED_BY]->(v:Test {{key:'{self.TEST_KEY}'}}) "
            "SET e.from_target = '15af8bc'")
        assert code == 0, (
            f"AC28: an outgoing SET anchored on the target must be accepted; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert self.edge_property("from_target") == "15af8bc", (
            "AC28: the property set from the target anchor must read back")

    # ---- AC 30: the rejection does not spread beyond the relationship
    #      write target ---------------------------------------------------

    def test_node_write_reached_through_an_undirected_traversal_is_accepted(self):
        # The SET target is x (a node), resolved by identifier rather than by
        # endpoint pair, so this must be admitted even though e is bound by
        # the very undirected pattern SET/REMOVE on e refuse.
        code, stdout, stderr = self.run(
            "update",
            f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})-[e]-(x) "
            "SET x.reviewed = true")
        assert code == 0, (
            f"AC30: a node write reached through an undirected traversal "
            f"must be accepted; exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert '"ok": true' in stdout, f"AC30: expected ok JSON, got {stdout!r}"
        assert self.node_property("Spec", self.SPEC_KEY, "reviewed") is True, (
            "AC30: the node write must genuinely have reached the source node")

    def test_relationship_read_through_an_undirected_traversal_is_refused(self):
        # This test asserted the opposite until rmp task #288. The undirected
        # READ was the escape hatch that made the write refusal cost no reach,
        # and it was measured returning the WRONG relationship on a node pair
        # carrying edges both ways. It is now refused by the read-direction
        # rule, and the reach it used to provide comes from the outgoing
        # rewrite asserted below instead.
        code, stdout, stderr = self.run(
            "query", f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})-[e]-(x) RETURN type(e), x.key")
        assert code == 6, (
            f"an undirected READ of a relationship value must be refused; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert READ_REFUSAL_PREFIX in stderr and "undirected pattern" in stderr, (
            f"expected the undirected read refusal; got {stderr!r}")

        # The rewrite the refusal names reaches the very same edge.
        result = self.json(
            "query", f"MATCH (x)-[e]->(v:Test {{key:'{self.TEST_KEY}'}}) RETURN type(e), x.key")
        assert result["rows"] == [["VERIFIED_BY", self.SPEC_KEY]], (
            f"the outgoing rewrite must report the true relationship and the "
            f"source node it reaches; got {result!r}")

    def test_relationship_bound_only_as_a_read_reference_is_refused(self):
        # `e` is bound by an INCOMING pattern and appears only on the RIGHT-HAND
        # SIDE of a node write. The write-direction rule inspects the write
        # TARGET alone, so it admitted this shape — and rmp task #288 measured
        # the consequence: the misresolved type is PERSISTED to disk while the
        # command reports success. The read-direction rule owns the right-hand
        # side, and refuses it.
        code, stdout, stderr = self.run(
            "update",
            f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})<-[e:VERIFIED_BY]-(x) "
            "SET x.last_edge_type = type(e)")
        assert code == 6, (
            f"a relationship VALUE read on the right-hand side of a node SET "
            f"must be refused when it is incoming-bound; "
            f"exit={code} stdout={stdout!r} stderr={stderr!r}")
        assert READ_REFUSAL_PREFIX in stderr, (
            f"the refusal must come from the read rule; got {stderr!r}")
        assert self.node_property("Spec", self.SPEC_KEY, "last_edge_type") is None, (
            "the refused write must not have reached storage")

    # ---- the refusal names the direction and the outgoing rewrite -----

    def test_refusal_message_names_the_variable_direction_and_rewrite(self):
        cases = [
            (
                f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})-[e]-(x) "
                "SET e.last_commit = '7be3aa0'",
                "undirected pattern",
            ),
            (
                f"MATCH (v:Test {{key:'{self.TEST_KEY}'}})<-[e:VERIFIED_BY]-(s) "
                "SET e.last_commit = '7be3aa0'",
                "incoming pattern",
            ),
        ]
        for query, want_direction in cases:
            code, stdout, stderr = self.run("update", query)
            assert code == 6, (
                f"expected a refusal for {query!r}; "
                f"exit={code} stdout={stdout!r} stderr={stderr!r}")
            assert REFUSAL_PREFIX in stderr, (
                f"the message must name the relationship variable; got {stderr!r}")
            assert want_direction in stderr, (
                f"the message must name the offending direction ({want_direction!r}); "
                f"got {stderr!r}")
            assert OUTGOING_REWRITE_RECIPE in stderr, (
                f"the message must give the outgoing-anchor rewrite recipe; "
                f"got {stderr!r}")
            assert "MATCH (source)-[e]->(target)" in stderr, (
                f"the message must show the outgoing rewrite shape; got {stderr!r}")


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
    print(f"Graph clause-surface tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
