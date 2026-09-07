#!/usr/bin/env python3
"""
Test 59: what `rmp graph execute` stores when the statement carries bytes or
escapes Groadmap does not inspect.

End-to-end backstop for SPEC/GRAPH.md acceptance criterion 38, bullets 1, 2 and
5, against the compiled ./bin/rmp and a real graph store.

WHAT THIS MODULE USED TO BE. Groadmap applied two free-text content rules to a
Cypher property value -- the UTF-8 Encoding Constraint and the Control-Character
Constraint (SPEC/MODELS.md) -- and refused a statement that broke either, with
exit code 6, before the store was opened. Twenty-two of the tests below asserted
those refusals, their precedence against the other guard-rail rules, and the
subcommands each rule reached.

Both rules were WITHDRAWN. `rmp graph execute` checks a statement's LENGTH and
nothing else about its content (SPEC/GRAPH.md section "What Groadmap Does Not
Check"), so what has to be asserted now is not a refusal but an OUTCOME -- and
the specification says so in as many words: the criterion is stated "asserting
the outcome rather than the absence of a check", because an absence cannot be
tested and an outcome can.

The three outcomes, each of which exits 0 and reports success:

  - INVALID UTF-8 IS SILENTLY REPLACED. The engine decodes the statement to
    characters before its grammar runs and replaces every byte that decodes to no
    character with U+FFFD. Writing the three bytes 61 80 62 returns {"ok": true}
    with exit 0 and the value reads back as 61 EF BF BD 62 -- a real U+FFFD. The
    store does not hold what was written and nothing reports the difference.
  - A CONTROL CHARACTER REACHES THE STORE THROUGH A QUERY OF PURE ASCII. Cypher
    decodes its own escapes inside a string literal (openCypher 9, under the
    heading "Note on string literals"; that document numbers no sections), so a
    statement whose text carries no control character writes a value that does.
    Each case asserts the query text is clean BEFORE running it, so a check on
    the query string would demonstrably have passed it.
  - A SCHEMA STATEMENT CARRYING A FURTHER CLAUSE EXECUTES IN PART. The engine's
    schema parser stops when its grammar is satisfied and discards the rest
    without an error or a notification, so the index is created, {"ok": true} is
    printed, and the trailing clause never runs.

Every assertion is made by READ-BACK, never by exit code: the exit code is 0 in
each case and would be 0 for an implementation that did nothing at all.

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
# byte 0x80.
RAW_CONTINUATION_BYTE = chr(0xDC80)

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
    """SPEC/GRAPH.md acceptance criterion 38, bullets 1, 2 and 5."""

    SEED_KEY = "sprint-41-scope"

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        # A realistic seed the update cases write to.
        self.seed_body = "Sprint 41 replaces five graph subcommands with one."
        self.run_query(
            "CREATE (n:Memory {key:'" + self.SEED_KEY + "', body:'" + self.seed_body + "'})",
            expect=0)

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers -----------------------------------------------------

    def run_query(self, query, expect=None):
        code, stdout, stderr = self.test.run_cmd(
            ["graph", "execute", "-r", self.roadmap, "--query", query], check=False)
        if expect is not None:
            assert code == expect, (
                f"rmp graph execute exited {code}, want {expect}\n"
                f"  query={query!r}\n  stdout={stdout!r}\n  stderr={stderr!r}")
        return code, stdout, stderr

    def body_of(self, key):
        """Return the stored body of a Memory node, or None when it has none.

        Reading through the binary is deliberate: the assertion is about what a
        LATER invocation sees, which is what makes a silent replacement a defect
        rather than a rendering question.
        """
        result = self.test.run_cmd_json(
            ["graph", "execute", "-r", self.roadmap,
             "--query", "MATCH (n:Memory {key:'" + key + "'}) RETURN n.body"])
        if not result["rows"]:
            return None
        return result["rows"][0][0]

    # ---- bullet 1: invalid UTF-8 executes and is replaced -----------------

    def test_the_corpus_is_read_from_the_definition_and_is_really_malformed(self):
        corpus = malformed_utf8_corpus()
        assert len(corpus) >= 5, (
            f"the malformed-UTF-8 corpus extractor found {len(corpus)} shapes in "
            f"{CORPUS_SOURCE}; the file's shape has changed and every case below "
            f"would be running on an empty list")
        for name, _argv_value, raw in corpus:
            try:
                raw.decode("utf-8")
            except UnicodeDecodeError:
                continue
            raise AssertionError(
                f"corpus shape {name!r} decodes as valid UTF-8, so it cannot "
                f"demonstrate the replacement this module asserts")

    def test_every_malformed_shape_executes_and_stores_the_replacement_character(self):
        for index, (name, argv_value, raw) in enumerate(malformed_utf8_corpus()):
            key = "malformed-{}".format(index)
            literal = cypher_escape("start" + argv_value + "end")
            code, stdout, stderr = self.run_query(
                "CREATE (n:Memory {key:'" + key + "', body:'" + literal + "'})")
            assert code == 0, (
                f"shape {name!r} was REFUSED (exit {code}); the encoding rule was "
                f"withdrawn and no statement is refused for its content "
                f"(SPEC/GRAPH.md section \"What Groadmap Does Not Check\", item 2); "
                f"stderr={stderr!r}")
            assert '"ok": true' in stdout, (
                f"shape {name!r} did not report success; stdout={stdout!r}")

            stored = self.body_of(key)
            assert stored is not None, f"shape {name!r} stored no node at all"
            assert stored.startswith("start") and stored.endswith("end"), (
                f"shape {name!r}: the well-formed text around the malformed bytes "
                f"must survive intact; stored={stored!r}")
            middle = stored[len("start"):-len("end")]
            assert "\ufffd" in middle, (
                f"shape {name!r}: the store must hold U+FFFD where the supplied "
                f"bytes were, and holds {middle!r} instead. The bytes supplied were "
                f"{raw!r}; nothing recovers them, which is the hazard the "
                f"specification records")
            assert argv_value not in stored, (
                f"shape {name!r}: the store holds the supplied bytes verbatim, so "
                f"the replacement this module asserts did not happen")

    def test_the_readback_comparator_can_tell_a_replacement_character_apart(self):
        # Non-vacuity for the assertion above: a value with no U+FFFD in it must
        # read back with none, or "contains U+FFFD" would be true of everything.
        self.run_query(
            "CREATE (n:Memory {key:'clean-value', body:'startend'})", expect=0)
        assert self.body_of("clean-value") == "startend"
        assert "\ufffd" not in self.body_of("clean-value")

    def test_a_malformed_literal_in_a_match_matches_nothing_and_still_reports_success(self):
        # The other face of the same hazard: a READ compares against a literal
        # that was never supplied, finds nothing, and exits 0.
        _name, argv_value, _raw = malformed_utf8_corpus()[0]
        literal = cypher_escape(self.seed_body[:10] + argv_value)
        result = self.test.run_cmd_json(
            ["graph", "execute", "-r", self.roadmap,
             "--query", "MATCH (n:Memory {body:'" + literal + "'}) RETURN n.key"])
        assert result["rows"] == [], (
            f"the malformed literal matched something: {result!r}")
        # And the seeded node is still there, so the empty answer is the
        # literal's doing and not the node's absence.
        assert self.body_of(self.SEED_KEY) == self.seed_body

    # ---- bullet 2: an ASCII statement writes a real control character -----

    def test_escape_encoded_control_characters_are_stored_as_real_code_points(self):
        for label, escape, code_point in (
            ("ESC", CY_ESC, "\u001b"),
            ("RIGHT-TO-LEFT OVERRIDE", CY_RLO, "\u202e"),
            ("BEL", CY_BEL, "\u0007"),
            ("BACKSPACE", CY_BS, "\u0008"),
        ):
            key = "escape-" + label.split()[0].lower()
            query = ("CREATE (n:Memory {key:'" + key + "', body:'red" + escape + "[31m'})")
            assert query.isascii(), (
                f"the {label} case must be driven by a PURE ASCII statement, or it "
                f"does not show that a clean query writes a control character: {query!r}")
            assert code_point not in query, (
                f"the {label} case must not carry the code point in the query text")

            self.run_query(query, expect=0)
            stored = self.body_of(key)
            assert stored is not None, f"the {label} case stored no node"
            assert stored == "red" + code_point + "[31m", (
                f"the {label} case must store a real {code_point!r}; "
                f"stored={stored!r}. Cypher decodes the escape inside the string "
                f"literal, so a check on the query text could never have seen it")

    def test_a_raw_control_character_in_the_query_text_is_stored_too(self):
        query = "CREATE (n:Memory {key:'raw-esc', body:'red" + RAW_ESC + "[31m'})"
        self.run_query(query, expect=0)
        assert self.body_of("raw-esc") == "red" + RAW_ESC + "[31m"

    def test_a_value_computed_at_execution_time_is_stored_as_computed(self):
        self.run_query(
            "MATCH (n:Memory {key:'" + self.SEED_KEY + "'}) SET n.body = toUpper(n.key)",
            expect=0)
        assert self.body_of(self.SEED_KEY) == self.SEED_KEY.upper()

    # ---- bullet 5: a schema statement with a trailing clause --------------

    def test_a_trailing_clause_after_a_schema_statement_is_discarded_silently(self):
        code, stdout, stderr = self.run_query(
            "CREATE INDEX memory_key FOR (n:Memory) ON (n.key) "
            "MATCH (m:Memory) SET m.reviewed = true")
        assert code == 0, (
            f"the statement must execute rather than be refused (exit {code}, "
            f"stderr={stderr!r}); the trailing-clause refusal was withdrawn")
        assert '"ok": true' in stdout, f"stdout={stdout!r}"

        listing = self.test.run_cmd_json(
            ["graph", "execute", "-r", self.roadmap, "--query", "SHOW INDEXES"])
        names = {row[listing["columns"].index("name")] for row in listing["rows"]}
        assert "memory_key" in names, (
            f"the index half of the statement did not run; SHOW INDEXES reports {names!r}")

        reviewed = self.test.run_cmd_json(
            ["graph", "execute", "-r", self.roadmap,
             "--query", "MATCH (n:Memory {key:'" + self.SEED_KEY + "'}) RETURN n.reviewed"])
        assert reviewed["rows"] == [[None]], (
            f"the MATCH ... SET half must have been discarded, leaving `reviewed` "
            f"absent; got {reviewed!r}. The engine reported success for a statement "
            f"half of which never ran, and nothing warned")


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
