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

## WHAT THIS MODULE LOST WHEN `rmp graph execute` WAS WITHDRAWN

The statement now runs in a `rmp graph serve` process and its failure crosses a
protocol to reach the caller. **The published field-length line does not cross
it, and neither do the two figures the line hands over to.**

MEASURED against the pinned engine, through a running server: a 70000-byte label
comes back as

    Error: graph engine error: graph query failed: An internal error occurred. See server logs for details (session: <id>).

while an ordinary parse failure and an ordinary execution failure both cross with
their full text -- `cypher: parse: parse error at 1:16, expected one of {...}` and
`exec: DropIndex "nope": index: no index by that name: "nope"`. The engine's Bolt
server classifies this refusal as an internal error and replaces its message.

Two consequences, both stated rather than worked around:

  1. **The published line has no producer.** SPEC/GRAPH.md "Field Length Limits",
     rules 2 to 5, and SPEC/COMMANDS.md still publish it; nothing writes it while
     the only route to a graph is a server that replaces the message.
     tests/test_55_error_string_parity.py carries the exemption that records this,
     and internal/commands/graph_fieldlength_test.go carries the measurement.
     Recovering the class by matching the engine's replacement text is exactly
     what rule 3 forbids.

  2. **The MAXIMUM cannot be derived here any more.** This module used to provoke
     one refusal, read the engine's own maximum out of the line, and drive
     exactly that maximum and exactly one byte more -- which is the derivation
     criterion 68 requires by name. The line that carried the figure is gone from
     this surface.

## What is still covered here, and what moved

Still here, end to end against ./bin/rmp:

  * that a field grossly over the engine's bound is REFUSED, on both field kinds
    a Cypher statement can drive into the guard, with exit code 1 and nothing on
    stdout;
  * that a shorter field of the same kinds is ACCEPTED and found, so the refusal
    discriminates by LENGTH rather than merely disliking long fields;
  * that the refused statement leaves nothing behind -- not even the well-formed
    element it created before the over-long field -- and that the store stays
    usable afterwards (criterion 69);
  * that a genuine syntax error still writes the parse/execution line, which is
    the direction an implementation that routed every engine failure to one line
    would fail;
  * that the healthy path stays silent (criterion 71).

Moved, because it is no longer observable from a command line:

  * the exact bound, and the boundary either side of it. Both are measured by
    internal/commands' TestGraphClient_FieldTooLongAgainstTheRealEngine, which
    opens the store IN PROCESS to provoke the refusal -- the one place the
    engine's sentinel and its diagnostic both survive -- reads the maximum from
    the engine's own message, and then drives `rmp graph client` at exactly that
    maximum and exactly one byte more against a running server. That test also
    asserts, live, that the engine still wraps store/txn.ErrFieldTooLong, which
    no fabricated case can check.

Not covered anywhere, and said plainly: the published field-length line itself,
and the field kind and figures its tail carries, are no longer produced by any
surface a user can reach. A caller who writes an over-long label is told that the
statement failed and is told nothing about which field or by how much.

## Why the two lengths below are not the bound

The limits are the ENGINE's, and a test pinned to 65535 would confirm a stale
figure instead of failing on it. Neither length below is written down as the
bound: they are PROBES, one grossly over any plausible bound and one comfortably
under, and what they bracket is that the refusal is a function of length. The
bound itself is measured, not assumed, by the Go test named above.
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
#
# Nothing produces it any more (see the module docstring). It is kept here for
# the two assertions below that are ABOUT its absence: that the specification
# still publishes it, and that no invocation writes it. If a future engine stops
# replacing the diagnostic, the second of those fails and this module is the
# place that says so.
PUBLISHED_HEAD = ("Error: graph engine error: graph field too long; nothing was written. "
                  "Shorten the field the engine names: ")

# The line every engine failure now writes, this class included.
PARSE_HEAD = "Error: graph engine error: graph query failed: "

# Two PROBES, and neither is the bound. The first is grossly over any plausible
# bound and its only job is to be over it; the second is comfortably under the
# smallest bound the engine has ever had for these two field kinds and its only
# job is to be under it. What they bracket is that the refusal discriminates by
# length. See the module docstring for where the bound itself is measured.
PROVOKING_LENGTH = 70000
ACCEPTED_LENGTH = 60000

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
        """The statement is refused with exit 1 and the ordinary
        parse-or-execution line, and it writes nothing to stdout.

        `kind` names the field kind for the failure message only. It is NOT
        asserted against the output, and the reason is the whole of what this
        module lost: the engine's diagnostic naming the field is replaced by the
        server before it reaches this process.
        """
        rc, out, err = self.graph(roadmap, query)
        assert rc == EXIT_ENGINE, (
            f"a {kind} of {PROVOKING_LENGTH} bytes was accepted: exit {rc} out={out!r}"
        )
        assert out == "", f"a refused statement wrote to stdout: {out!r}"
        line = err.splitlines()[0]
        assert line.startswith(PARSE_HEAD), (
            f"an over-long {kind} does not write the parse/execution line\n"
            f"  expected prefix: {PARSE_HEAD!r}\n  captured: {line!r}"
        )
        return line


class TestFieldLengthRefusal(_GraphFixture):
    """SPEC/GRAPH.md "Field Length Limits", rules 2 to 5, and acceptance
    criteria 68 and 69, as far as a command line can still reach them: the commit
    the engine refuses for an over-long field fails, nothing is written, and the
    store stays usable.
    """

    def test_the_published_line_is_still_published(self):
        """The specification still publishes the field-length line, and no
        invocation produces it.

        Both halves are asserted, and the pair is the point. The line is a
        published contract SPEC/COMMANDS.md carries, so a reword must fail
        somewhere; and nothing writes it, because the engine's Bolt server
        replaces the diagnostic that would have selected it. An implementation
        that started producing it again would fail the second half, which is the
        signal that this module's whole account of what it lost has changed.
        """
        spec = SPEC_COMMANDS.read_text(encoding="utf-8")
        assert PUBLISHED_HEAD in spec, (
            f"SPEC/COMMANDS.md no longer publishes {PUBLISHED_HEAD!r}; this module and "
            f"tests/test_55_error_string_parity.py both need updating together"
        )

        roadmap = self.served(
            "telemetry-ingest",
            "CREATE (:Component {key:'ingest-gateway', language:'go'})",
        )
        _rc, _out, err = self.graph(roadmap, f"CREATE (n:`{'L' * PROVOKING_LENGTH}`)")
        assert PUBLISHED_HEAD not in err, (
            f"an over-long label now writes the published field-length line:\n{err!r}\n"
            f"That is an IMPROVEMENT and not a failure -- it means the engine's "
            f"diagnostic reaches the caller again -- but this module, "
            f"tests/test_55_error_string_parity.py's exemption for the same line, and "
            f"internal/commands/graph_fieldlength_test.go all describe a surface on "
            f"which it does not, and all three must be brought back into line."
        )

    def test_a_field_over_the_engines_bound_is_refused_on_both_field_kinds(self):
        """Criterion 68, the refusing half. Both field kinds a Cypher statement
        can drive into the log's guard -- a label and a property key -- are
        refused.

        (A schema identifier cannot reach this guard at all: the engine's Cypher
        parser bounds an index or constraint name far below the log's prefix and
        refuses it there, as an ordinary engine refusal. SPEC/GRAPH.md
        "Field Length Limits", rule 11, states that, and it is why this test
        drives two kinds and not three.)
        """
        roadmap = self.served(
            "telemetry-ingest",
            "CREATE (:Component {key:'ingest-gateway', language:'go'})",
        )

        self.assert_refused(
            roadmap, f"CREATE (n:`{'L' * PROVOKING_LENGTH}`)", "label",
        )
        self.assert_refused(
            roadmap,
            "CREATE (n:AlertRule {`" + "k" * PROVOKING_LENGTH + "`: 'critical'})",
            "property key",
        )

    def test_a_shorter_field_of_the_same_kinds_is_accepted_and_found(self):
        """Criterion 68, the accepting half. The refusal is a function of LENGTH
        and not of "a long field": the same two kinds, at a length comfortably
        inside the engine's bound, are committed and a following MATCH finds the
        elements they created.

        It used to drive exactly the maximum, one byte under the refusal above,
        because the maximum could be read out of the refusal's own message. It
        cannot be now (see the module docstring); the boundary at that precision
        is measured by internal/commands' TestGraphClient_FieldTooLongAgainstTheRealEngine
        instead, and what remains here is the bracket.
        """
        roadmap = self.served(
            "telemetry-ingest",
            "CREATE (:Component {key:'ingest-gateway', language:'go'})",
        )

        rc, _out, err = self.graph(
            roadmap,
            f"CREATE (n:UnderTheBound:`{'L' * ACCEPTED_LENGTH}` {{key:'label-under-bound'}})")
        assert rc == EXIT_OK, f"a label of {ACCEPTED_LENGTH} bytes was refused: {err!r}"
        assert err == "", f"an accepted write wrote to stderr: {err!r}"

        rc, _out, err = self.graph(
            roadmap,
            "CREATE (n:KeyUnderTheBound {`" + "k" * ACCEPTED_LENGTH + "`: 'key-under-bound'})")
        assert rc == EXIT_OK, f"a property key of {ACCEPTED_LENGTH} bytes was refused: {err!r}"
        assert err == "", f"an accepted write wrote to stderr: {err!r}"

        rc, out, err = self.graph(
            roadmap, "MATCH (n:UnderTheBound) RETURN count(n) AS labels")
        assert rc == EXIT_OK, f"counting the under-the-bound label failed: {err!r}"
        assert json.loads(out)["rows"] == [[1]], (
            f"the element carrying a {ACCEPTED_LENGTH}-byte label was not found: {out!r}"
        )

        rc, out, err = self.graph(
            roadmap, "MATCH (n:KeyUnderTheBound) RETURN count(n) AS keys")
        assert rc == EXIT_OK, f"counting the under-the-bound property key failed: {err!r}"
        assert json.loads(out)["rows"] == [[1]], (
            f"the element carrying a {ACCEPTED_LENGTH}-byte property key was not found: {out!r}"
        )

    def test_a_syntax_error_still_writes_the_parse_or_execution_line(self):
        """Criterion 68's other direction. An implementation that routed every
        engine failure to a field-length line would pass every check above; this
        is the check it fails.

        It also carries the non-vacuity control for the module docstring's
        measurement: the engine's OWN diagnostic is required to be present, which
        establishes that the protocol carries one in general and that the
        field-length refusal's replacement message is a property of that class
        rather than of every failure.
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
            f"the engine's own parse diagnostic no longer crosses the protocol: {line!r}. "
            f"If EVERY engine failure is now replaced by the server, this module's account "
            f"of what it lost is stale and the loss is no longer specific to the "
            f"field-length class."
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
