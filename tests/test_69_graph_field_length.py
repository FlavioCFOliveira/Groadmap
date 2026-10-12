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

  * A label or a property key over the log's length prefix fits comfortably
    inside the 1 MiB maximum query length, so the refusal and the intact store
    are drivable against ./bin/rmp. Acceptance criteria 68 and 69 drive them, and
    so does this module.

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

## HOW THE CLASS REACHES THE CALLER

The statement runs in a `rmp graph serve` process and its failure crosses a
protocol to reach the caller. The pinned engine's server gives the refusal a
failure code of its own -- one for an over-long label, relationship type or
property key, one for any other field a durable format cannot carry -- and
forwards the engine's diagnostic unchanged, which names the field kind and both
figures. The client selects the published field-length line by those two codes
and by nothing else (SPEC/GRAPH.md "Field Length Limits", rules 3 and 13), so the
line is `rmp`'s class, the fact that nothing was written and the remedy, ending
in the engine's diagnostic.

## What is covered here, end to end against ./bin/rmp

  * that a field one byte over the engine's maximum is REFUSED, on both field
    kinds a Cypher statement can drive into the guard, with exit code 1, nothing
    on stdout, and the published field-length line ending in the engine's
    diagnostic; and that the same two kinds at exactly the maximum are ACCEPTED
    and found by a following MATCH (criterion 68). The maximum is read from the
    refusal line itself, never written down here;
  * that a genuine syntax error still writes the parse/execution line, which is
    the direction an implementation that routed every engine failure to one line
    would fail;
  * that the refused statement leaves nothing behind -- not even the well-formed
    element it created before the over-long field -- and that the store stays
    usable afterwards (criterion 69);
  * that the healthy path stays silent (criterion 71).

## Why no length below is the bound

The limits are the ENGINE's, and a test pinned to 65535 would confirm a stale
figure instead of failing on it. The one literal length below is a PROBE grossly
over any plausible bound; every other length is derived from the maximum the
refusal reports.
"""

import json
import os
import re
import signal
import sys
import time
import inspect

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import REPO_ROOT, GroadmapTestBase


SPEC_COMMANDS = REPO_ROOT / "SPEC" / "COMMANDS.md"

# rmp's own half of the line SPEC/COMMANDS.md "Graph Management" publishes for a
# field the engine refuses, WITH the "Error: " prefix as the user reads it.
PUBLISHED_HEAD = ("Error: graph engine error: graph field too long; nothing was written. "
                  "Shorten the field the engine names: ")

# The line every other engine failure writes, a syntax error included.
PARSE_HEAD = "Error: graph engine error: graph query failed: "

# A PROBE grossly over any plausible bound: its only job is to be over it, so the
# refusal it provokes can report the maximum every other length is derived from.
PROVOKING_LENGTH = 70000

# The figure the engine's diagnostic reports as the maximum in force.
MAXIMUM = re.compile(r"maximum (\d+)")

EXIT_OK = 0
EXIT_ENGINE = 1


class _GraphFixture:
    """A temporary HOME per test, and a teardown that kills any server the test
    left running before it removes that HOME.

    It is six lines rather than an import because everything it needs is on
    GroadmapTestBase now -- the server lifecycle, the client invocations and the
    teardown that force-kills a leaked child -- and a cross-module fixture import
    would tie this module's fate to another module's refactors.
    """

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def graph(self, roadmap, query):
        """One statement against this fixture's roadmap, through the only
        subcommand that runs one.

        The statement goes in on STDIN rather than as a --query argument.
        SPEC/GRAPH.md "Cypher Input Source and Precedence" makes the two equally
        valid, and stdin has no per-argument bound to be near: Linux caps a
        single argv entry at 128 KiB (MAX_ARG_STRLEN), which a 70000-byte label
        is under today and which nothing in this module should have to depend on.
        """
        return self.test.graph_client(roadmap, stdin_text=query)

    def served(self, roadmap, *seeds):
        """A roadmap with a running server and whatever seeds the caller wants.

        The server is started BEFORE the seeds because it is what creates the
        graph store: `rmp graph serve` is the only thing that does
        (SPEC/GRAPH.md "Server Startup", step 1), and a roadmap that has never
        had a graph is served an empty one.
        """
        self.test.served_roadmap(roadmap, *seeds)
        return roadmap

    def assert_refused(self, roadmap, query, kind):
        """The statement is refused with exit 1 and the published field-length
        line, which ends in the engine's diagnostic, and it writes nothing to
        stdout. Returns that line.

        The diagnostic is required to be there and is read only for the maximum
        it reports; its wording is the engine's, which rule 3 of SPEC/GRAPH.md
        "Field Length Limits" forbids this project to match. `kind` names the
        field kind for the failure messages only.
        """
        rc, out, err = self.graph(roadmap, query)
        assert rc == EXIT_ENGINE, f"an over-long {kind} was accepted: exit {rc} out={out!r}"
        assert out == "", f"a refused statement wrote to stdout: {out!r}"
        line = err.splitlines()[0]
        assert line.startswith(PUBLISHED_HEAD), (
            f"an over-long {kind} does not write the published field-length line\n"
            f"  expected prefix: {PUBLISHED_HEAD!r}\n  captured: {line!r}"
        )
        assert line[len(PUBLISHED_HEAD):].strip(), (
            f"an over-long {kind} carries no engine diagnostic after rmp's half: {line!r}"
        )
        return line

    def maximum_of(self, line, kind):
        """The maximum the refusal line reports, read from the engine's half."""
        found = MAXIMUM.search(line[len(PUBLISHED_HEAD):])
        assert found, (
            f"the refusal of an over-long {kind} reports no maximum, so a caller could not "
            f"learn what to shorten to: {line!r}"
        )
        return int(found.group(1))


class TestFieldLengthRefusal(_GraphFixture):
    """SPEC/GRAPH.md "Field Length Limits", rules 2 to 5, and acceptance
    criteria 68 and 69, as far as a command line can still reach them: the commit
    the engine refuses for an over-long field fails, nothing is written, and the
    store stays usable.
    """

    def test_the_published_line_is_what_the_refusal_writes(self):
        """The specification publishes the field-length line, and an over-long
        field writes it: both halves, so a reword on either side fails here."""
        spec = SPEC_COMMANDS.read_text(encoding="utf-8")
        assert PUBLISHED_HEAD in spec, (
            f"SPEC/COMMANDS.md no longer publishes {PUBLISHED_HEAD!r}; this module and "
            f"tests/test_55_error_string_parity.py both need updating together"
        )
        roadmap = self.served(
            "telemetry-ingest",
            "CREATE (:Component {key:'ingest-gateway', language:'go'})",
        )
        self.assert_refused(roadmap, f"CREATE (n:`{'L' * PROVOKING_LENGTH}`)", "label")

    def test_one_byte_over_the_maximum_is_refused_and_the_maximum_is_accepted(self):
        """Criterion 68. Both field kinds a Cypher statement can drive into the
        guard -- a label and a property key -- are refused one byte over the
        maximum the refusal reports, and accepted at exactly that maximum, with
        their elements found by a following MATCH.

        (A schema identifier cannot reach this guard at all: the engine's Cypher
        parser bounds an index or constraint name far below the limit and refuses
        it there, as an ordinary engine refusal. SPEC/GRAPH.md "Field Length
        Limits", rule 11, states that, and it is why this test drives two kinds
        and not three.)
        """
        roadmap = self.served(
            "telemetry-ingest",
            "CREATE (:Component {key:'ingest-gateway', language:'go'})",
        )

        label_max = self.maximum_of(
            self.assert_refused(roadmap, f"CREATE (n:`{'L' * PROVOKING_LENGTH}`)", "label"),
            "label")
        key_max = self.maximum_of(
            self.assert_refused(
                roadmap, "CREATE (n:AlertRule {`" + "k" * PROVOKING_LENGTH + "`: 'critical'})",
                "property key"),
            "property key")

        self.assert_refused(roadmap, f"CREATE (n:`{'L' * (label_max + 1)}`)", "label")
        self.assert_refused(
            roadmap, "CREATE (n:AlertRule {`" + "k" * (key_max + 1) + "`: 'critical'})",
            "property key")

        rc, _out, err = self.graph(
            roadmap, f"CREATE (n:AtTheBound:`{'L' * label_max}` {{key:'label-at-bound'}})")
        assert rc == EXIT_OK, f"a label of exactly {label_max} bytes was refused: {err!r}"
        assert err == "", f"an accepted write wrote to stderr: {err!r}"

        rc, _out, err = self.graph(
            roadmap, "CREATE (n:KeyAtTheBound {`" + "k" * key_max + "`: 'key-at-bound'})")
        assert rc == EXIT_OK, f"a property key of exactly {key_max} bytes was refused: {err!r}"
        assert err == "", f"an accepted write wrote to stderr: {err!r}"

        rc, out, err = self.graph(roadmap, "MATCH (n:AtTheBound) RETURN count(n) AS labels")
        assert rc == EXIT_OK, f"counting the label at the bound failed: {err!r}"
        assert json.loads(out)["rows"] == [[1]], (
            f"the element carrying a {label_max}-byte label was not found: {out!r}"
        )
        rc, out, err = self.graph(roadmap, "MATCH (n:KeyAtTheBound) RETURN count(n) AS keys")
        assert rc == EXIT_OK, f"counting the property key at the bound failed: {err!r}"
        assert json.loads(out)["rows"] == [[1]], (
            f"the element carrying a {key_max}-byte property key was not found: {out!r}"
        )

    def test_a_syntax_error_still_writes_the_parse_or_execution_line(self):
        """Criterion 68's other direction. An implementation that routed every
        engine failure to the field-length line would pass every check above;
        this is the check it fails. It also requires the engine's OWN diagnostic
        to be present, which establishes that the protocol carries one for an
        ordinary failure.
        """
        roadmap = self.served(
            "telemetry-ingest",
            "CREATE (:Component {key:'ingest-gateway', language:'go'})",
        )
        rc, out, err = self.graph(roadmap, "CREATE (n:AlertRule")
        assert rc == EXIT_ENGINE, f"a malformed statement was accepted: exit {rc} out={out!r}"
        line = err.splitlines()[0]
        assert line.startswith(PARSE_HEAD), (
            f"a syntax error no longer writes the parse/execution line: {line!r}"
        )
        assert "parse" in line, (
            f"the engine's own parse diagnostic no longer crosses the protocol: {line!r}"
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
        roadmap = self.served(
            "incident-timeline",
            "CREATE (:Component {key:'pager-bridge', language:'go'})",
        )

        self.assert_refused(
            roadmap,
            "CREATE (n:PostMortem {key:'outage-2026-08-14'}) "
            "CREATE (m:`" + "L" * PROVOKING_LENGTH + "`)",
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


class TestHealthyPathStaysSilent(_GraphFixture):
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
        roadmap = self.served(
            "release-pipeline",
            "CREATE (:Component {key:'artifact-signer', language:'go'})",
        )
        rc, out, err = self.test.graph_client(
            roadmap, query="CREATE (:Component {key:'changelog-builder', language:'go'})")
        assert rc == EXIT_OK, f"an ordinary write failed: exit {rc} err={err!r}"
        assert json.loads(out)["ok"] is True, f"the write did not succeed: {out!r}"
        assert err == "", (
            f"a healthy `graph client` wrote {len(err)} bytes to stderr; the whole stream "
            f"must be empty, not merely free of one phrase: {err!r}"
        )

    def test_a_healthy_server_writes_its_two_startup_warnings_and_nothing_else(self):
        """A server that starts, serves a WRITING statement -- so a checkpoint
        really is taken, at shutdown -- and stops cleanly on SIGINT writes its
        two startup warnings and nothing further, with no checkpoint record of
        any kind among them."""
        roadmap = "release-pipeline"
        server = self.test.served_roadmap(
            roadmap, "CREATE (:Component {key:'artifact-signer', language:'go'})")

        rc, out, err = self.test.graph_client(
            roadmap, query="CREATE (:Component {key:'sbom-publisher', language:'go'})")
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
    on __module__ is belt and braces now that this module imports no fixture
    class from another test module -- it costs nothing and it would catch such an
    import being added back.
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
