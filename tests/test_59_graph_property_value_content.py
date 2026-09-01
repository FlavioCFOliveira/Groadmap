#!/usr/bin/env python3
"""
Test 59: the knowledge-graph property-value content rules (rmp task #298).

End-to-end backstop for SPEC/GRAPH.md, against the compiled ./bin/rmp and a real
graph store.

Every free-text value Groadmap stores is subject to two content rules -- the
Free-Text UTF-8 Encoding Constraint and the Free-Text Control-Character
Constraint (SPEC/MODELS.md). A Cypher property value written through
`rmp graph create` or `rmp graph update` was subject to NEITHER, and the graph is
the project's own memory (CLAUDE.md section 5), so what it holds is meant to be
the truth about the project.

Both halves were measured on the shipped binary before the rule existed:

  - INVALID UTF-8 WAS SILENTLY REPLACED. Writing the three bytes 61 80 62
    returned {"ok": true} with exit 0, and the value read back as 61 EF BF BD 62
    -- a real U+FFFD. The store did not hold what was written and nothing
    reported the difference. That is data corruption, not a rendering question,
    and it is what decided the rule.
  - CONTROL CHARACTERS WERE STORED VERBATIM. Their rendering is bounded TODAY --
    encoding/json escapes them and the web renders through html/template -- but
    boundedness is a property of the CONSUMER, not of the value, so it cannot be
    the guarantee.

The two rules have two REACHES, and the module pins both:

  - THE ENCODING RULE BINDS EVERY SUBCOMMAND that accepts a Cypher query. The
    engine replaces a malformed byte before its grammar runs, so the statement it
    executes is not the statement supplied -- a fact about the QUERY, indifferent
    to what the statement then does. A `graph create` stores a value never
    supplied; a `graph query` matches on a literal never supplied and reports
    success having found nothing; a `graph delete` gated by one removes nothing
    and still reports success. That last shape is why the rule is not confined to
    the writers, and its test asserts BY READ-BACK that the target survives --
    the exit code alone cannot see it, because the old behaviour also exited 0.
  - THE CONTROL-CHARACTER RULE BINDS ONLY THE TWO SUBCOMMANDS THAT WRITE
    property values. It objects to what is STORED, and a read stores nothing. The
    store can legitimately hold such a value -- everything written before this
    rule existed, and anything a computed expression produces -- so refusing a
    read that names one would leave that data unreadable rather than merely
    unwritable.

Two design facts are asserted here rather than assumed, because each decides
where half of the rule can be enforced at all:

  - A CONTROL CHARACTER REACHES THE STORE THROUGH A QUERY OF PURE ASCII. Cypher
    decodes its own escapes inside a string literal (openCypher 9, under the
    heading "Note on string literals"; that document numbers no sections),
    so a query whose text carries no control character writes a value that does.
    Every such case below asserts the query text is clean BEFORE running it, so a
    check on the query string would demonstrably pass it.
  - INVALID UTF-8 NEVER REACHES THE PARSED VALUE. The engine's lexer replaces
    every byte that decodes to no character with U+FFFD before the grammar runs,
    so a check on the parsed value would demonstrably pass every shape below.

The malformed-UTF-8 corpus is NOT written here. It is extracted from
internal/testenv/malformedutf8.go, the corpus rmp task 180 built from the items
SPEC/MODELS.md enumerates, so this module and the Go tests are bound to the same
enumeration and a shape added there is exercised here without an edit.
"""

import inspect
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CORPUS_SOURCE = os.path.join(REPO_ROOT, "internal", "testenv", "malformedutf8.go")

BACKSLASH = chr(92)

# The escape sequences a Cypher string literal decodes, written as the literal
# characters they are composed of. Built with chr(92) so this file carries no
# escape of its own that a reader could mistake for the Cypher one.
CY_ESC = BACKSLASH + "u001b"   # ESC,  U+001B
CY_RLO = BACKSLASH + "u202e"   # RIGHT-TO-LEFT OVERRIDE, U+202E
CY_BEL = BACKSLASH + "u0007"   # BEL,  U+0007
CY_BS = BACKSLASH + "b"        # BACKSPACE, U+0008

# The same characters as raw bytes in the query text.
RAW_ESC = chr(0x1B)

# A byte that begins no valid UTF-8 sequence, carried through argv by Python's
# surrogateescape convention: chr(0xDC80) is encoded by subprocess as the single
# byte 0x80. The refusal names the byte in hex, which is what proves it arrived.
RAW_CONTINUATION_BYTE = chr(0xDC80)

# The two governed wordings, which internal/utils owns and both surfaces publish.
UTF8_REASON = "the value is not valid UTF-8"
CONTROL_REASON = "control characters are not allowed"


def _decode_go_string_literal(body: str) -> bytes:
    """Decode the BODY of a Go interpreted string literal into the bytes it
    denotes, honouring the escapes the corpus actually uses.

    The corpus values are deliberately malformed byte sequences, so they cannot
    be carried as text: `chr` would produce a code point where the Go source
    means a BYTE. Decoding to bytes and re-encoding with surrogateescape is what
    delivers the exact bytes to the binary's argv.
    """
    out = bytearray()
    i = 0
    while i < len(body):
        c = body[i]
        if c != BACKSLASH:
            out.extend(c.encode("utf-8"))
            i += 1
            continue
        nxt = body[i + 1]
        if nxt == "x":
            out.append(int(body[i + 2:i + 4], 16))
            i += 4
        elif nxt == "n":
            out.append(0x0A)
            i += 2
        elif nxt == "t":
            out.append(0x09)
            i += 2
        elif nxt in (BACKSLASH, '"'):
            out.extend(nxt.encode("utf-8"))
            i += 2
        else:
            raise AssertionError(
                f"unhandled Go escape {BACKSLASH + nxt!r} in the corpus source; "
                "the extractor must be taught it rather than silently dropping the shape")
    return bytes(out)


def malformed_utf8_corpus() -> list:
    """Return [(name, value_as_argv_str, why)] read from the Go corpus.

    Reading the corpus rather than repeating it is what keeps this module bound
    to the same enumeration the Go tests use: a shape added to SPEC/MODELS.md and
    to that file is exercised here with no edit, and a shape removed stops being
    claimed here.
    """
    with open(CORPUS_SOURCE, "r", encoding="utf-8") as handle:
        source = handle.read()

    entries = []
    # Each corpus entry is a `Name:` line followed by a `Value:` line, both
    # carrying a single-line Go interpreted string literal.
    pattern = re.compile(r'Name:\s+"((?:[^"\\]|\\.)*)"\s*,\s*\n\s*Value:\s+"((?:[^"\\]|\\.)*)"\s*,')
    for match in pattern.finditer(source):
        name = _decode_go_string_literal(match.group(1)).decode("utf-8")
        raw = _decode_go_string_literal(match.group(2))
        entries.append((name, raw.decode("utf-8", "surrogateescape"), raw))
    return entries


def cypher_escape(value: str) -> str:
    """Render value as the interior of a single-quoted Cypher string literal.

    Only the backslash and the closing quote need escaping. Every other
    character -- VALID OR NOT -- passes through untouched, which is the whole
    point when the value under test is malformed.
    """
    return value.replace(BACKSLASH, BACKSLASH * 2).replace("'", BACKSLASH + "'")


class TestGraphPropertyValueContent:
    """SPEC/GRAPH.md property-value content rules, rmp task #298."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        # A realistic seed: one memory node the update cases write to, whose
        # value every refusal must leave exactly as it is.
        self.seed_body = "Sprint 38 fixes twelve recorded defects; no auditing, no sweeping."
        self.write(
            "create",
            "CREATE (n:Memory {key:'sprint-38-scope', body:'" + self.seed_body + "'})",
            expect=0)

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers -----------------------------------------------------

    def write(self, subcmd, query, expect=None):
        code, stdout, stderr = self.test.run_cmd(
            ["graph", subcmd, "-r", self.roadmap, "--query", query], check=False)
        if expect is not None:
            assert code == expect, (
                f"rmp graph {subcmd} exited {code}, want {expect}\n"
                f"  query={query!r}\n  stdout={stdout!r}\n  stderr={stderr!r}")
        return code, stdout, stderr

    def body_of(self, key):
        """Return the stored body of a Memory node, or None when the node has
        none. Reading through the binary is deliberate: the assertion is about
        what the STORE holds, not about what a refusal said it would hold."""
        result = self.test.run_cmd_json(
            ["graph", "query", "-r", self.roadmap,
             "--query", f"MATCH (n:Memory {{key:'{key}'}}) RETURN n.body"])
        rows = result.get("rows") or []
        if not rows or rows[0][0] is None:
            return None
        return rows[0][0]

    def assert_refused(self, subcmd, query, *, reason, property_name=None, code_point=None):
        code, stdout, stderr = self.write(subcmd, query, expect=6)
        assert reason in stderr, (
            f"the refusal must name the rule that was broken ({reason!r}); got {stderr!r}")
        assert stdout.strip() == "", (
            f"a refused write must print nothing to stdout; got {stdout!r}")
        if property_name is not None:
            assert f'property "{property_name}"' in stderr, (
                f"the refusal must name the offending value by its property "
                f"({property_name!r}); got {stderr!r}")
        if code_point is not None:
            assert code_point in stderr, (
                f"the refusal must name the offending code point ({code_point!r}); got {stderr!r}")
        return stderr

    # ---- the corpus extractor itself --------------------------------------

    def test_the_corpus_is_read_from_the_definition_and_is_really_malformed(self):
        corpus = malformed_utf8_corpus()
        assert len(corpus) >= 4, (
            f"the extractor found only {len(corpus)} shapes in {CORPUS_SOURCE}; "
            "it is not reading the corpus the rule is defined by")
        for name, _, raw in corpus:
            try:
                raw.decode("utf-8")
            except UnicodeDecodeError:
                continue
            raise AssertionError(
                f"corpus shape {name!r} decodes as valid UTF-8, so it proves nothing "
                "about the encoding rule; the extractor is mangling the source")

    # ---- refused: the encoding rule ---------------------------------------

    def test_every_malformed_utf8_shape_is_refused_and_writes_nothing(self):
        for name, value, _ in malformed_utf8_corpus():
            key = "malformed-" + re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-")
            stderr = self.assert_refused(
                "create",
                f"CREATE (n:Memory {{key:'{key}', body:'{cypher_escape(value)}'}})",
                reason=UTF8_REASON,
                property_name="body")
            assert "0x" in stderr, (
                f"the refusal must name the offending byte; got {stderr!r} for shape {name!r}")
            assert self.body_of(key) is None, (
                f"the refused write reached the store for shape {name!r}: "
                f"{self.body_of(key)!r}")

    def test_malformed_utf8_is_refused_on_graph_update_and_leaves_the_value_alone(self):
        for name, value, _ in malformed_utf8_corpus():
            self.assert_refused(
                "update",
                "MATCH (n:Memory {key:'sprint-38-scope'}) "
                f"SET n.body = '{cypher_escape(value)}'",
                reason=UTF8_REASON,
                property_name="body")
            assert self.body_of("sprint-38-scope") == self.seed_body, (
                f"the refused update changed the stored value (shape {name!r})")

    def test_an_invalid_byte_outside_a_written_value_is_refused_without_naming_a_property(self):
        # The documented widening of the encoding half: the engine replaces the
        # byte wherever it sits, so the statement it would run is not the one
        # supplied. The refusal stands, and it says why it names no property
        # instead of inventing one.
        stderr = self.assert_refused(
            "update",
            "MATCH (n:Memory {key:'a" + RAW_CONTINUATION_BYTE + "b'}) SET n.body = 'clean value'",
            reason=UTF8_REASON)
        assert "0x80" in stderr, (
            f"the raw byte did not survive the argv transport, so this case is not "
            f"exercising what it claims; got {stderr!r}")
        assert 'property "' not in stderr, (
            f"the refusal named a property it cannot attribute the byte to: {stderr!r}")
        assert "No property value could be attributed" in stderr, (
            f"the refusal does not explain why it names no property: {stderr!r}")

    # ---- refused: the control-character rule ------------------------------

    def test_a_raw_control_character_in_a_written_value_is_refused(self):
        self.assert_refused(
            "create",
            "CREATE (n:Memory {key:'deploy-log', body:'deploy " + RAW_ESC + "[31mFAILED'})",
            reason=CONTROL_REASON,
            property_name="body",
            code_point="U+001B")
        assert self.body_of("deploy-log") is None

    def test_escape_encoded_control_characters_are_refused_though_the_query_is_pure_ascii(self):
        # THE case that decides where the control-character half belongs. Each
        # query below is checked to be pure ASCII before it is run, so a check
        # on the query TEXT would pass every one of them, and the value it
        # writes would still carry the character.
        cases = [
            ("ansi-colour", "deploy " + CY_ESC + "[31mFAILED" + CY_ESC + "[0m", "U+001B"),
            ("trojan-source", "invoice" + CY_RLO + "gpj.exe", "U+202E"),
            ("bell", "build finished" + CY_BEL, "U+0007"),
            ("backspace", "approved" + CY_BS + CY_BS + CY_BS + "rejected", "U+0008"),
        ]
        for key, value, code_point in cases:
            query = f"CREATE (n:Memory {{key:'{key}', body:'{value}'}})"
            assert query.isascii(), (
                f"the query text for {key!r} is not pure ASCII, so this case does not "
                "show that the check reads the VALUE rather than the query")
            assert not any(ord(ch) < 0x20 or ord(ch) == 0x7F for ch in query), (
                f"the query text for {key!r} carries a control character itself")
            self.assert_refused(
                "create", query,
                reason=CONTROL_REASON, property_name="body", code_point=code_point)
            assert self.body_of(key) is None, (
                f"the refused write reached the store for {key!r}")

    def test_every_write_position_is_governed(self):
        # A list of POSITIONS, with one offending value throughout: a case that
        # stops being refused is a write position the rule no longer reaches.
        bad = CY_ESC
        cases = [
            ("create inline node map",
             "create", f"CREATE (n:Memory {{key:'pos-a', body:'x{bad}y'}})", "body"),
            ("create inline relationship map",
             "create",
             "CREATE (a:Spec {key:'pos-spec'})-[:SEE_ALSO {note:'x" + bad + "y'}]->"
             "(b:Spec {key:'pos-web'})", "note"),
            ("merge inline map",
             "create", f"MERGE (n:Memory {{key:'pos{bad}b'}})", "key"),
            ("set assignment",
             "update",
             "MATCH (n:Memory {key:'sprint-38-scope'}) SET n.body = 'x" + bad + "y'", "body"),
            ("set map merge",
             "update",
             "MATCH (n:Memory {key:'sprint-38-scope'}) SET n += {body:'x" + bad + "y'}", "body"),
            ("set whole-map replacement",
             "update",
             "MATCH (n:Memory {key:'sprint-38-scope'}) SET n = {body:'x" + bad + "y'}", "body"),
            ("foreach body",
             "update",
             "MATCH (n:Memory {key:'sprint-38-scope'}) "
             "FOREACH (i IN [1] | SET n.body = 'x" + bad + "y')", "body"),
            ("list element",
             "update",
             "MATCH (n:Memory {key:'sprint-38-scope'}) SET n.tags = ['clean', 'x" + bad + "y']",
             "tags"),
        ]
        for name, subcmd, query, property_name in cases:
            self.assert_refused(
                subcmd, query,
                reason=CONTROL_REASON, property_name=property_name, code_point="U+001B")
            assert self.body_of("sprint-38-scope") == self.seed_body, (
                f"case {name!r} disturbed the seeded value")

        # `MERGE ... ON CREATE SET` is a write position the RULE covers -- the
        # cypherguard tests exercise it directly -- but it is unreachable from
        # this CLI, because the clause-class guard rail refuses any CREATE/MERGE
        # query carrying SET under `graph create` and any SET query carrying
        # MERGE under `graph update`. Asserting that here keeps the reason on
        # record: the position is not missing from the rule, it is missing from
        # the command surface, and a later change that opened it would make this
        # assertion fail and send someone back to this comment.
        for subcmd, expected in (("create", "graph create accepts only CREATE/MERGE queries"),
                                 ("update", "graph update accepts only SET/REMOVE, index/constraint DDL, and schema-introspection queries")):
            _, _, stderr = self.write(
                subcmd,
                "MERGE (n:Memory {key:'pos-c'}) ON CREATE SET n.body = 'x" + bad + "y'",
                expect=6)
            assert expected in stderr, (
                f"the ON CREATE SET shape is no longer refused on its class by "
                f"graph {subcmd}; the content rule now needs to be reached for it: {stderr!r}")

    # ---- the refusal must not be dangerous itself -------------------------

    def test_the_refusal_carries_none_of_the_bytes_it_refuses(self):
        # The rule exists because these characters are dangerous to emit, so the
        # message that refuses them must not emit them: it names the CODE POINT.
        _, _, stderr = self.write(
            "create",
            "CREATE (n:Memory {key:'echo-check', body:'deploy " + CY_ESC + "[31mFAILED"
            + CY_RLO + "'})",
            expect=6)
        forbidden = [ch for ch in stderr
                     if (ord(ch) < 0x20 and ch not in "\t\n\r")
                     or ord(ch) == 0x7F
                     or ord(ch) in (0x200E, 0x200F, 0x202A, 0x202B, 0x202C, 0x202D,
                                    0x202E, 0x2066, 0x2067, 0x2068, 0x2069, 0xFEFF)]
        assert not forbidden, (
            "the refusal echoed the very characters it refuses: "
            f"{[hex(ord(ch)) for ch in forbidden]}")
        assert "U+001B" in stderr

    # ---- admitted: the rule must not be over-broad ------------------------

    def test_legitimate_values_are_accepted_and_stored_unchanged(self):
        cases = [
            ("spec-graph", "Especificacao do grafo de conhecimento"),
            ("spec-accents", "Especificação — acentuação e cedilha"),
            ("spec-cjk", "知識グラフのプロパティ値"),
            ("release-note", "Sprint 38 shipped \U0001F680 and was measured \U0001F4CA"),
            ("commit-body", "subject line" + BACKSLASH + "n" + BACKSLASH + "nbody paragraph"
                            + BACKSLASH + "twith a tab"),
        ]
        expected = {
            "spec-graph": "Especificacao do grafo de conhecimento",
            "spec-accents": "Especificação — acentuação e cedilha",
            "spec-cjk": "知識グラフのプロパティ値",
            "release-note": "Sprint 38 shipped \U0001F680 and was measured \U0001F4CA",
            "commit-body": "subject line\n\nbody paragraph\twith a tab",
        }
        for key, literal in cases:
            self.write("create", f"CREATE (n:Memory {{key:'{key}', body:'{literal}'}})", expect=0)
            assert self.body_of(key) == expected[key], (
                f"an accepted value must be stored unchanged: {key!r} reads back "
                f"{self.body_of(key)!r}, want {expected[key]!r}")

    def test_the_control_character_rule_does_not_reach_reads_or_deletes(self):
        # The CONTROL-CHARACTER rule governs the values a query WRITES. A read or
        # a delete that merely MATCHES on such a literal persists nothing, so it
        # stays accepted -- and a widening of that rule fails here. The ENCODING
        # rule is the other half and does reach these two subcommands; its own
        # tests below are what pin that.
        code, _, stderr = self.write(
            "query", "MATCH (n:Memory {key:'x" + CY_ESC + "y'}) RETURN n.key")
        assert code == 0, f"graph query was refused by a rule that governs writes: {stderr!r}"

        code, _, stderr = self.write(
            "delete", "MATCH (n:Memory {key:'x" + CY_ESC + "y'}) DELETE n")
        assert code == 0, f"graph delete was refused by a rule that governs writes: {stderr!r}"

        assert self.body_of("sprint-38-scope") == self.seed_body

    def test_a_value_computed_at_execution_time_is_outside_what_the_check_can_see(self):
        # The stated limit of the rule, asserted so it is a recorded fact rather
        # than a comment: the value does not exist until the engine runs the
        # statement, so Groadmap never holds it and cannot judge it. Closing this
        # means checking at the storage boundary, which is inside the engine.
        code, _, stderr = self.write(
            "update",
            "MATCH (n:Memory {key:'sprint-38-scope'}) SET n.body = toUpper(n.key)")
        assert code == 0, (
            "a computed value was refused; the rule claimed reach it does not have: "
            f"{stderr!r}")
        assert self.body_of("sprint-38-scope") == "SPRINT-38-SCOPE"

    # ---- precedence among the guard-rail rules ----------------------------

    def test_the_clause_class_objection_outranks_the_content_objection(self):
        _, _, stderr = self.write(
            "update", "CREATE (n:Memory {key:'precedence', body:'x" + CY_ESC + "y'})", expect=6)
        assert "graph update accepts only SET/REMOVE, index/constraint DDL, and schema-introspection queries" in stderr, (
            f"the class objection must be reported first; got {stderr!r}")

    def test_the_relationship_direction_objection_outranks_the_content_objection(self):
        self.write(
            "create",
            "CREATE (:Spec {key:'prec-a'})-[:SEE_ALSO]->(:Spec {key:'prec-b'})", expect=0)
        _, _, stderr = self.write(
            "update",
            "MATCH (s:Spec {key:'prec-a'})<-[e:SEE_ALSO]-(x) SET e.note = 'x" + CY_ESC + "y'",
            expect=6)
        assert "cannot write relationship" in stderr, (
            f"the relationship-direction objection must be reported first; got {stderr!r}")


    # ---- the encoding rule reaches every subcommand -----------------------

    def test_graph_delete_refuses_a_malformed_literal_and_the_target_survives(self):
        """The case that carried the extension, asserted BY READ-BACK.

        Before the rule, this exited 0 having deleted NOTHING: the engine
        replaced the byte with U+FFFD, the pattern matched no node, and the
        command reported success. An exit-code assertion cannot see that defect
        -- the old behaviour also exited 0. Only reading the store back
        distinguishes "deleted the right node" from "deleted nothing and said
        so", which is why the surviving-node assertion is the one that matters.
        """
        self.write(
            "create",
            "CREATE (n:Memory {key:'delete-target', body:'Provenance for commit cf27c57'})",
            expect=0)
        assert self.body_of("delete-target") == "Provenance for commit cf27c57", (
            "the fixture did not land, so the surviving-node assertion would be vacuous")

        shapes = [
            ("inline match key",
             "MATCH (n:Memory {key:'delete-tar" + RAW_CONTINUATION_BYTE + "get'}) DELETE n"),
            ("WHERE predicate",
             "MATCH (n:Memory) WHERE n.key = 'delete-tar" + RAW_CONTINUATION_BYTE
             + "get' DELETE n"),
            ("detach delete",
             "MATCH (n:Memory {key:'delete-tar" + RAW_CONTINUATION_BYTE
             + "get'}) DETACH DELETE n"),
        ]
        for name, query in shapes:
            stderr = self.assert_refused("delete", query, reason=UTF8_REASON)
            assert "0x80" in stderr, (
                f"the raw byte did not survive the argv transport for {name!r}, so this case is "
                f"not exercising what it claims; got {stderr!r}")
            assert "deleted nothing" in stderr, (
                f"the refusal must name the consequence for a delete; got {stderr!r}")

            # THE assertion.
            assert self.body_of("delete-target") == "Provenance for commit cf27c57", (
                f"the refused delete ({name}) removed or altered the node it named")

        # The other half of the contract: a well-formed delete still deletes,
        # proved by the node being gone. Without this the suite would pass just
        # as well if `graph delete` had stopped deleting altogether.
        self.write("delete", "MATCH (n:Memory {key:'delete-target'}) DELETE n", expect=0)
        assert self.body_of("delete-target") is None, (
            "the well-formed delete did not remove the node")

    def test_graph_query_and_search_refuse_a_malformed_literal(self):
        # Milder than the delete -- an empty result rather than a destructive
        # no-op -- but the mechanism is identical, and stating the rule by
        # command rather than by cause is what would have left this behind.
        query = ("MATCH (n:Memory {key:'sprint-38-sco" + RAW_CONTINUATION_BYTE
                 + "pe'}) RETURN n.body")
        for subcmd in ("query", "search"):
            stderr = self.assert_refused(subcmd, query, reason=UTF8_REASON)
            assert "0x80" in stderr, (
                f"the raw byte did not survive the argv transport; got {stderr!r}")
            assert "found nothing" in stderr, (
                f"the refusal must name the consequence for a read; got {stderr!r}")
            assert f"graph {subcmd}" in stderr, (
                f"the refusal must name the subcommand; got {stderr!r}")
            # A read writes no property value, so the refusal must SAY why it
            # names none rather than withholding the naming in silence.
            assert 'property "' not in stderr, (
                f"a read named a property it cannot have; got {stderr!r}")
            assert "writes no property value" in stderr, (
                f"the refusal does not explain why no property is named; got {stderr!r}")

        # The well-formed read still answers, so the rule is not simply refusing
        # everything a read sends.
        assert self.body_of("sprint-38-scope") == self.seed_body

    def test_the_encoding_rule_reaches_every_subcommand_that_takes_a_query(self):
        """The reach itself, as a partition over the whole subcommand surface.

        Naming the five subcommands here rather than three is deliberate: the
        rule is stated by CAUSE, so a subcommand that stopped being covered
        would be a hole in the rule and not a scoping decision. `create` and
        `update` are refused with a property name when the byte falls in a value
        they write; the other three cannot have one and say so.
        """
        bad = "spr" + RAW_CONTINUATION_BYTE + "int-38"
        cases = [
            ("create", f"CREATE (n:Memory {{key:'{bad}'}})"),
            ("update", f"MATCH (n:Memory {{key:'{bad}'}}) SET n.body = 'clean'"),
            ("delete", f"MATCH (n:Memory {{key:'{bad}'}}) DELETE n"),
            ("query", f"MATCH (n:Memory {{key:'{bad}'}}) RETURN n.body"),
            ("search", f"MATCH (n:Memory {{key:'{bad}'}}) RETURN n.body"),
        ]
        covered = set()
        for subcmd, query in cases:
            stderr = self.assert_refused(subcmd, query, reason=UTF8_REASON)
            assert "0x80" in stderr
            covered.add(subcmd)

        assert covered == {"create", "update", "delete", "query", "search"}, (
            f"the encoding rule was proved for {sorted(covered)} only; it binds every "
            "subcommand that accepts a Cypher query")
        assert self.body_of("sprint-38-scope") == self.seed_body, (
            "one of the refused statements reached the store")

    def test_a_control_character_in_a_read_or_delete_literal_stays_admitted(self):
        """The asymmetry, against a value the store actually holds.

        The store can hold a control character through the one write path the
        content rule cannot see -- a computed expression. This test puts one
        there that way, then reaches it with a read and removes it with a
        delete. Extending the control-character rule to those two would make the
        data unreachable, which is a loss of reach the rule never intended.
        """
        self.write("create", "CREATE (n:Memory {key:'legacy-entry'})", expect=0)
        # A computed right-hand side: outside what the content rule can see.
        self.write(
            "update",
            "MATCH (n:Memory {key:'legacy-entry'}) SET n.body = toUpper(n.key)",
            expect=0)
        assert self.body_of("legacy-entry") == "LEGACY-ENTRY"

        # A read and a delete that NAME a control character must both be
        # admitted. Neither stores anything, so neither is this rule's business.
        code, _, stderr = self.write(
            "query", "MATCH (n:Memory {key:'legacy" + CY_ESC + "entry'}) RETURN n.key")
        assert code == 0, (
            f"a read naming a control character was refused; data the store holds would be "
            f"unreadable: {stderr!r}")

        code, _, stderr = self.write(
            "delete", "MATCH (n:Memory {key:'legacy" + CY_ESC + "entry'}) DELETE n")
        assert code == 0, (
            f"a delete naming a control character was refused: {stderr!r}")

        # And the write path is still refused for the same character, so the
        # asymmetry is a boundary rather than an absence of the rule.
        self.assert_refused(
            "update",
            "MATCH (n:Memory {key:'legacy-entry'}) SET n.body = 'x" + CY_ESC + "y'",
            reason=CONTROL_REASON, property_name="body", code_point="U+001B")
        assert self.body_of("legacy-entry") == "LEGACY-ENTRY"

    def test_the_encoding_rule_is_applied_before_the_control_character_rule(self):
        # The discriminating input breaks BOTH rules, so "it was refused" proves
        # nothing -- either order refuses it, with the same exit code. What is
        # asserted is WHICH rule answered. The order is not a preference: an
        # invalid byte decodes to U+FFFD, which is not a forbidden code point, so
        # a control-character check running first would report nothing at all for
        # a value that is only malformed.
        query = ("CREATE (n:Memory {key:'both-rules', body:'deploy " + CY_ESC + "[31m"
                 + RAW_CONTINUATION_BYTE + "FAILED'})")
        stderr = self.assert_refused("create", query, reason=UTF8_REASON)
        assert CONTROL_REASON not in stderr, (
            f"the control-character rule answered for a value that is malformed UTF-8; "
            f"the encoding rule must answer first: {stderr!r}")
        assert self.body_of("both-rules") is None

    # ---- non-vacuity ------------------------------------------------------

    def test_the_readback_comparator_can_tell_a_replacement_character_apart(self):
        # The corruption assertions above rest on the read-back distinguishing
        # the supplied bytes from U+FFFD. This proves it can: a value carrying a
        # GENUINE U+FFFD is accepted -- it is well-formed UTF-8 and no control
        # character -- and reads back as exactly that, so a value that had been
        # silently replaced would have compared unequal to what was supplied.
        self.write(
            "create",
            "CREATE (n:Memory {key:'genuine-replacement', body:'placeholder � kept'})",
            expect=0)
        stored = self.body_of("genuine-replacement")
        assert stored == "placeholder � kept", (
            f"the comparator cannot round-trip U+FFFD, so it cannot detect a "
            f"replacement either; got {stored!r}")
        assert stored != "placeholder  kept", (
            "the comparator is not distinguishing U+FFFD from its absence")

    def test_the_refusal_assertions_would_fail_on_the_recorded_defect(self):
        # The recorded defect in its exact shape: the three bytes 61 80 62
        # written, {"ok": true} returned, 61 EF BF BD 62 read back. Here it must
        # be refused -- and the assertion that would have caught the OLD
        # behaviour is spelled out, so a future reader can see this module would
        # go red if the rule were removed rather than merely pass today.
        key = "recorded-defect"
        value = "a" + RAW_CONTINUATION_BYTE + "b"
        code, stdout, stderr = self.write(
            "create", f"CREATE (n:Memory {{key:'{key}', body:'{value}'}})")
        assert code == 6, (
            "the recorded defect is back: the write was accepted "
            f"(exit={code} stdout={stdout!r})")
        assert UTF8_REASON in stderr
        stored = self.body_of(key)
        assert stored is None, (
            "the refused write reached the store; before the rule this read back as "
            f"{'a' + chr(0xFFFD) + 'b'!r}, and it now reads {stored!r}")


def _run_all():
    passed = 0
    failed = 0
    failures = []
    # Classes are DISCOVERED by inspecting this module, never listed. A listed
    # tuple silently skips any suite added after it was written -- the runner
    # still exits 0 and the new class simply never runs (rmp task #303).
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
                print(f"\u2713 {label}")
            except AssertionError as exc:
                failed += 1
                failures.append((label, exc))
                print(f"\u2717 {label}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((label, exc))
                print(f"\u2717 {label} (error)")
            finally:
                instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Graph property-value content tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n\u2717 {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
