#!/usr/bin/env python3
"""
Test 69: the engine's field-length refusals, end to end (rmp task #413).

A statement writes labels, property keys and property values into TWO durable
formats, and each bounds how long a field it will carry: the write-ahead log
bounds one at commit, the snapshot bounds one at checkpoint. The bounds are the
engine's; what SPEC/GRAPH.md "Field Length Limits" fixes is what the caller is
told when a field exceeds one, and what Groadmap does next.

This module drives the half that is reachable from the command surface, which is
the COMMIT half and only the commit half:

  * A label or a property key over the log's 65535-byte length prefix fits
    comfortably inside the 1 MiB maximum query length, so the refusal, the intact
    store and the published line are all drivable against ./bin/rmp. Acceptance
    criteria 68 and 69 drive them, and so does this module.

  * The bounds that govern a property VALUE are not drivable in either format: a
    literal of a gigabyte does not fit inside a statement, and a regression test
    cannot materialise a field of that size at all -- one that tried would measure
    the machine rather than the product. Acceptance criterion 70 says so
    explicitly and forbids a criterion that attempts the field itself, so nothing
    here attempts it. That half's coverage is the classification alone, driven
    with fabricated errors that wrap the engine's own sentinels, and it lives in
    the Go suites: internal/graphstore (the two predicates and the diagnostic's
    required content), internal/commands, internal/web and internal/graphserve
    (each surface's choice between the two conditions). This module states that
    division rather than implying an end-to-end check exists where there is none.

## Why nothing here is written down as a literal length

The limits are the ENGINE's. A version bump may move any of them, and a test
pinned to 65535 would confirm a stale figure instead of failing on it. So every
length below is DERIVED, at run time, from the maximum the engine's own refusal
reports: the module provokes one refusal with a label grossly over any plausible
bound, reads the maximum out of the line, and drives exactly that maximum (which
must be accepted) and exactly one byte more (which must be refused). Criterion 68
requires that derivation by name.

## The two directions, and why both are here

Reporting the new line for the new condition is the easy half. The half an
implementation gets wrong is NOT reporting it for the conditions beside it: an
implementation that routed every engine failure to the field-length line would
pass every check that only looked for the new line. So a genuine syntax error is
driven too, and is required to keep the parse/execution line.
"""

import json
import os
import re
import signal
import subprocess
import sys
import time
import inspect

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import REPO_ROOT
# The graph server harness is test_65's, and it is imported rather than copied:
# it spawns the real process, drains both pipes off the main thread so a full
# pipe buffer cannot wedge the child, waits for the startup object, and
# force-kills anything a failing test leaves behind. A second copy here would be
# a second thing to keep correct, and the one assertion this module needs it for
# -- that a healthy server's stderr carries the two startup warnings and nothing
# else -- depends on exactly the drain discipline that harness already has.
from tests.test_65_graph_server_client_e2e import GraphServerTestBase


SPEC_COMMANDS = REPO_ROOT / "SPEC" / "COMMANDS.md"

# rmp's own half of the line SPEC/COMMANDS.md "Graph Management" publishes for a
# field the engine refuses, WITH the "Error: " prefix as the user reads it. The
# engine's diagnostic ends the line and is not specified there, which is why this
# is a prefix and not the whole string.
#
# test_55_error_string_parity.py owns the character-for-character parity between
# this text and the file (it extracts the corpus from the tables and drives the
# binary against it). What this module adds is the assertion below that the text
# is still IN the file, so a reword of the published row fails here too rather
# than leaving this module quietly asserting a string the specification no longer
# publishes.
PUBLISHED_HEAD = ("Error: graph engine error: graph field too long; nothing was written. "
                  "Shorten the field the engine names: ")

# The line a Cypher failure that is NOT this class still writes.
PARSE_HEAD = "Error: graph engine error: graph query failed: "

# The engine reports both figures in its own diagnostic: what the field occupies
# and what the maximum is. The maximum is what every length in this module is
# derived from.
MAXIMUM_PATTERN = re.compile(r"maximum (\d+)")

# A label grossly over any plausible bound, used ONCE to provoke the refusal the
# real figures are read out of. It is deliberately not near the bound: its only
# job is to be over it.
PROVOKING_LENGTH = 70000

EXIT_OK = 0
EXIT_ENGINE = 1


class TestFieldLengthRefusal(GraphServerTestBase):
    """SPEC/GRAPH.md "Field Length Limits", rules 2 to 5, and acceptance
    criteria 68 and 69: the commit the engine refuses for an over-long field
    publishes a line of its own, nothing is written, and the store stays usable.
    """

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def graph(self, roadmap, query):
        """One `graph execute` against this fixture's HOME.

        The statement goes in on STDIN rather than as a --query argument.
        SPEC/GRAPH.md "Cypher Input Source and Precedence" makes the two equally
        valid, and stdin has no per-argument bound to be near: Linux caps a
        single argv entry at 128 KiB (MAX_ARG_STRLEN), which a 70000-byte label
        is under today and which nothing in this module should have to depend on.
        """
        return self.run_cli(["graph", "execute", "-r", roadmap], stdin_text=query)

    def derive_maximum(self, roadmap):
        """Provoke one refusal and read the engine's own maximum out of it.

        Returns the maximum, in bytes. Fails loudly -- rather than falling back
        to a literal -- if the refusal does not report one, because a refusal
        that names no maximum leaves a caller unable to learn what to shorten to,
        which is the whole content of the tail the published line hands over to.
        """
        rc, out, err = self.graph(roadmap, f"CREATE (n:`{'L' * PROVOKING_LENGTH}`)")
        assert rc == EXIT_ENGINE, (
            f"a label of {PROVOKING_LENGTH} bytes was not refused (exit {rc}); the engine's "
            f"bound has moved beyond what this module provokes, and the lengths below "
            f"cannot be derived. out={out!r} err={err!r}"
        )
        line = err.splitlines()[0]
        assert line.startswith(PUBLISHED_HEAD), (
            f"the provoking refusal does not write the published line: {line!r}"
        )
        match = MAXIMUM_PATTERN.search(line)
        assert match, f"the refusal reports no maximum, so nothing can be derived: {line!r}"
        maximum = int(match.group(1))
        assert maximum > 0, f"the reported maximum is not a length: {line!r}"
        return maximum

    def assert_refused(self, roadmap, query, kind):
        """The statement is refused with exit 1 and the published line, and the
        engine's diagnostic ends that line naming `kind` and both figures."""
        rc, out, err = self.graph(roadmap, query)
        assert rc == EXIT_ENGINE, (
            f"a {kind} over the engine's bound was accepted: exit {rc} out={out!r}"
        )
        assert out == "", f"a refused statement wrote to stdout: {out!r}"
        line = err.splitlines()[0]
        assert line.startswith(PUBLISHED_HEAD), (
            f"an over-long {kind} does not write the published field-length line\n"
            f"  published prefix: {PUBLISHED_HEAD!r}\n  captured: {line!r}"
        )
        tail = line[len(PUBLISHED_HEAD):]
        assert tail, f"the binary printed the head and no engine diagnostic: {line!r}"
        assert kind in tail, (
            f"the engine's diagnostic does not name the field kind the caller must "
            f"shorten (expected {kind!r}): {tail!r}"
        )
        return line

    # ------------------------------------------------------------------
    # Criterion 68: the distinction, asserted in both directions
    # ------------------------------------------------------------------

    def test_the_published_line_is_still_published(self):
        """A cheap guard on this module's own premise: the head it asserts
        against the binary is the head SPEC/COMMANDS.md publishes. The
        character-for-character parity belongs to test_55; this only fails fast
        when the row is reworded, instead of asserting a string the file no
        longer carries."""
        spec = SPEC_COMMANDS.read_text(encoding="utf-8")
        assert PUBLISHED_HEAD in spec, (
            f"SPEC/COMMANDS.md no longer publishes {PUBLISHED_HEAD!r}; this module and "
            f"test_55_error_string_parity.py both need updating together"
        )

    def test_one_byte_over_the_bound_is_refused_on_both_field_kinds(self):
        """Criterion 68, the refusing half. Both field kinds a Cypher statement
        can drive into the log's guard -- a label and a property key -- are
        refused one byte over the maximum the engine itself reports.

        (A schema identifier cannot reach this guard at all: the engine's Cypher
        parser bounds an index or constraint name far below 65535 bytes and
        refuses it there, as an ordinary engine refusal. SPEC/GRAPH.md
        "Field Length Limits", rule 11, states that, and it is why this test
        drives two kinds and not three.)
        """
        roadmap = self.seeded_roadmap(
            "telemetry-ingest",
            "CREATE (:Component {key:'ingest-gateway', language:'go'})",
        )
        maximum = self.derive_maximum(roadmap)

        self.assert_refused(
            roadmap, f"CREATE (n:`{'L' * (maximum + 1)}`)", "label",
        )
        self.assert_refused(
            roadmap,
            "CREATE (n:AlertRule {`" + "k" * (maximum + 1) + "`: 'critical'})",
            "property key",
        )

    def test_exactly_at_the_bound_is_accepted_and_found(self):
        """Criterion 68, the accepting half. The fence is on the BOUND and not
        merely on "a long field": one byte less than the refusal above is
        committed, and a following MATCH finds the element it created."""
        roadmap = self.seeded_roadmap(
            "telemetry-ingest",
            "CREATE (:Component {key:'ingest-gateway', language:'go'})",
        )
        maximum = self.derive_maximum(roadmap)

        rc, out, err = self.graph(
            roadmap, f"CREATE (n:AtTheBound:`{'L' * maximum}` {{key:'label-at-bound'}})")
        assert rc == EXIT_OK, f"a label of exactly {maximum} bytes was refused: {err!r}"
        assert err == "", f"an accepted write wrote to stderr: {err!r}"

        rc, out, err = self.graph(
            roadmap,
            "CREATE (n:KeyAtTheBound {`" + "k" * maximum + "`: 'key-at-bound'})")
        assert rc == EXIT_OK, f"a property key of exactly {maximum} bytes was refused: {err!r}"
        assert err == "", f"an accepted write wrote to stderr: {err!r}"

        rc, out, err = self.graph(
            roadmap,
            "MATCH (n:AtTheBound) RETURN count(n) AS labels")
        assert rc == EXIT_OK, f"counting the at-the-bound label failed: {err!r}"
        assert json.loads(out)["rows"] == [[1]], (
            f"the element carrying a {maximum}-byte label was not found: {out!r}"
        )

        rc, out, err = self.graph(
            roadmap, "MATCH (n:KeyAtTheBound) RETURN count(n) AS keys")
        assert rc == EXIT_OK, f"counting the at-the-bound property key failed: {err!r}"
        assert json.loads(out)["rows"] == [[1]], (
            f"the element carrying a {maximum}-byte property key was not found: {out!r}"
        )

    def test_a_syntax_error_still_writes_the_parse_or_execution_line(self):
        """Criterion 68's other direction. An implementation that routed every
        engine failure to the new line would pass every check above; this is the
        check it fails."""
        roadmap = self.seeded_roadmap(
            "telemetry-ingest",
            "CREATE (:Component {key:'ingest-gateway', language:'go'})",
        )
        rc, out, err = self.graph(roadmap, "CREATE (n:AlertRule")
        assert rc == EXIT_ENGINE, f"a malformed statement was accepted: exit {rc} out={out!r}"
        line = err.splitlines()[0]
        assert line.startswith(PARSE_HEAD), (
            f"a syntax error no longer writes the parse/execution line: {line!r}"
        )
        assert "graph field too long" not in line, (
            f"a syntax error was reported as a field-length refusal: {line!r}"
        )

    # ------------------------------------------------------------------
    # Criterion 69: nothing survives the refusal
    # ------------------------------------------------------------------

    def test_the_refusal_leaves_nothing_behind_and_the_store_stays_usable(self):
        """Criterion 69. The refused statement creates a well-formed element
        FIRST and writes the over-long field second, in one pass, so an
        implementation that committed the statement's well-formed prefix and
        refused only its tail is caught here rather than passing on the write
        that follows.

        All three halves are asserted: the element is gone, the next ordinary
        write succeeds, and a following MATCH counts it.
        """
        roadmap = self.seeded_roadmap(
            "incident-timeline",
            "CREATE (:Component {key:'pager-bridge', language:'go'})",
        )
        maximum = self.derive_maximum(roadmap)

        self.assert_refused(
            roadmap,
            "CREATE (n:PostMortem {key:'outage-2026-08-14'}) "
            "CREATE (m:`" + "L" * (maximum + 1) + "`)",
            "label",
        )

        rc, out, err = self.graph(
            roadmap, "MATCH (n:PostMortem) RETURN count(n) AS survivors")
        assert rc == EXIT_OK, f"counting after the refusal failed: {err!r}"
        assert json.loads(out)["rows"] == [[0]], (
            f"the refused statement committed its well-formed prefix: {out!r}"
        )

        rc, out, err = self.graph(
            roadmap, "CREATE (n:PostMortem {key:'outage-2026-08-15'})")
        assert rc == EXIT_OK, f"an ordinary write after the refusal failed: {err!r}"
        assert json.loads(out) == {
            "ok": True,
            "counters": {"nodesCreated": 1, "propertiesWritten": 1, "labelsAdded": 1},
        }, f"the write after the refusal did not report success: {out!r}"

        rc, out, err = self.graph(
            roadmap, "MATCH (n:PostMortem) RETURN n.key AS key")
        assert rc == EXIT_OK, f"counting after the ordinary write failed: {err!r}"
        assert json.loads(out)["rows"] == [["outage-2026-08-15"]], (
            f"the store did not keep the write that followed the refusal: {out!r}"
        )


class TestHealthyPathStaysSilent(GraphServerTestBase):
    """Acceptance criterion 71: the classification must not become a diagnostic
    the healthy path emits.

    Both halves compare the COMPLETE stream rather than searching it for the
    absence of one phrase. An implementation that emitted the new diagnostic
    speculatively -- on every checkpoint, or on every checkpoint that returned
    any error at all -- would satisfy a search and would make
    SPEC/GRAPH.md "Field Length Limits", rule 9, meaningless by announcing a
    permanent condition that does not hold.
    """

    def test_an_ordinary_write_writes_nothing_at_all_to_stderr(self):
        """The statement is chosen for raising no notification of its own: a
        notification is the engine's to raise and shares this stream
        (SPEC/GRAPH.md "Query Notifications as Diagnostics", rule 5), so a
        statement that raised one would make the assertion about the engine
        rather than about the checkpoint."""
        roadmap = self.seeded_roadmap(
            "release-pipeline",
            "CREATE (:Component {key:'artifact-signer', language:'go'})",
        )
        rc, out, err = self.run_cli(
            ["graph", "execute", "-r", roadmap, "--query",
             "CREATE (:Component {key:'changelog-builder', language:'go'})"])
        assert rc == EXIT_OK, f"an ordinary write failed: exit {rc} err={err!r}"
        assert err == "", (
            f"a healthy `graph execute` wrote {len(err)} bytes to stderr; the whole stream "
            f"must be empty, not merely free of one phrase: {err!r}"
        )

    def test_a_healthy_server_writes_its_two_startup_warnings_and_nothing_else(self):
        """A server that starts, serves a WRITING statement -- so a checkpoint
        really is taken, in flight and again at shutdown -- and stops cleanly on
        SIGINT writes its two startup warnings and nothing further, with no
        checkpoint record of any kind among them."""
        roadmap = self.seeded_roadmap(
            "release-pipeline",
            "CREATE (:Component {key:'artifact-signer', language:'go'})",
        )
        server = self.start_server(roadmap)

        rc, out, err = self.run_cli(
            ["graph", "client", "-r", roadmap, "--query",
             "CREATE (:Component {key:'sbom-publisher', language:'go'})"])
        assert rc == EXIT_OK, f"the served write failed: exit {rc} err={err!r}"
        assert json.loads(out)["ok"] is True, f"the served write did not succeed: {out!r}"

        assert server.stop(signal.SIGINT) == EXIT_OK
        # The drain runs on its own thread, so the exit does not mean the last
        # line has been collected. Closing that window is what makes a
        # whole-stream comparison deterministic instead of a race.
        server.drain_stderr_to_eof()

        lines = [line for line in server.stderr_text().splitlines() if line.strip()]
        assert len(lines) == 2, (
            f"a healthy server's stderr must carry exactly the two engine startup "
            f"warnings; got {len(lines)} lines:\n" + "\n".join(lines)
        )
        assert re.search(r"(?i)no authentication", lines[0]), (
            f"the first line is not the no-authentication warning: {lines[0]!r}"
        )
        assert re.search(r"(?i)no tls", lines[1]), (
            f"the second line is not the no-TLS warning: {lines[1]!r}"
        )
        for line in lines:
            assert "checkpoint" not in line.lower(), (
                f"a healthy server announced a checkpoint: {line!r}"
            )


def _run_all():
    """Discover and run every Test* class defined in this module.

    Enumerating the module's own namespace rather than naming the classes in a
    fixed list means a class added later cannot silently fail to run. The filter
    on __module__ matters here more than usual: this module imports a fixture
    base from test_65, and every Test* class that import drags in would
    otherwise be re-run under this module's name.
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
            started = time.monotonic()
            try:
                getattr(instance, m)()
                passed += 1
                print(f"OK   {label} ({time.monotonic() - started:.2f}s)")
            except AssertionError as exc:
                failed += 1
                failures.append((label, exc))
                print(f"FAIL {label} ({time.monotonic() - started:.2f}s)")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((label, exc))
                print(f"ERROR {label} ({time.monotonic() - started:.2f}s)")
            finally:
                instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Graph field-length tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\nFAIL {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
