#!/usr/bin/env python3
"""
Test 66: end-to-end tests for the EXPLAIN and PROFILE statement prefixes
(rmp task #410).

SPEC/GRAPH.md "Query Plans: The EXPLAIN and PROFILE Prefixes",
SPEC/DATA_FORMATS.md "Graph Plan Node" and "Graph Client Result", and
SPEC/COMMANDS.md "Graph Management" are canonical for every assertion here.

## What this module is for

The prefixes, and the write counters beside them, asserted END TO END: real
processes, a real `rmp graph serve`, a real Unix domain socket, and the bytes
`rmp graph client` writes to stdout. `internal/graphjson` already pins the
mapping's rules and `internal/commands` already pins the envelope, both
mutation-checked, and neither runs the binary or crosses the Bolt protocol.
What this module adds is that the plan a statement carries, and the counters a
write reports, survive that crossing intact -- the plan tree's keys and shape,
the measured figures a PROFILE carries and an EXPLAIN must not, the
case-insensitivity of the prefixes, the unprefixed envelope left exactly as it
was, and, hardest of all, that an EXPLAIN of a writing statement does not run
the write, asserted against the graph rather than against the output.

## What this module used to be for as well, and no longer is

Its stated reason to exist was a second property: that for the same statement
against the same graph, `rmp graph client` wrote the bytes `rmp graph execute`
wrote. That identity spanned two surfaces reaching the graph two different ways
-- one opening the store in-process, the other crossing a socket -- and it was
worth asserting on stdout because a Go test could not see both sides.

`rmp graph execute` is withdrawn: `rmp graph <anything but serve|client>` exits
127, and the graph is reachable only through a running server. There is one
surface, so there is no identity left to assert, and every comparison of the
two is RETIRED rather than rewritten -- a comparison of a surface with itself
asserts nothing. The retirements are recorded at the point where the code was:
see the block where `TestPlanPrefixParityAcrossSurfaces` stood, and the
docstring of `TestWriteCounters`, which keeps everything that class asserted
ABOUT the objects and drops only the second side of each comparison.

## The three defects this guards

The prefixes were parsed by the engine and their plans discarded, so:

  1. `EXPLAIN` printed `{"columns": [...], "rows": []}` -- indistinguishable
     from a query that matched nothing.
  2. `PROFILE` printed rows and discarded the measurement.
  3. `EXPLAIN` of a WRITING statement printed `{"ok": true}` -- byte-identical
     to a real committed write, over a statement that wrote nothing.

The third is asserted hardest, and against the graph itself: the test reads
the node count back to prove the statement did not run.
"""

import inspect
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from tests.base_test import GroadmapTestBase

# Three specs, two of which pass the predicate the tests filter on. Two-of-three
# is deliberate: it makes `rowsRemovedByFilter` a number that could not have been
# derived from the emitted row count, so a figure that merely echoed the rows
# would be visible as 2 rather than 1.
SEED = (
    "CREATE (:Spec {key:'GRAPH.md', status:'implemented'}), "
    "(:Spec {key:'BUILD.md', status:'implemented'}), "
    "(:Spec {key:'WEB.md', status:'draft'})"
)

READ = "MATCH (s:Spec) WHERE s.status = 'implemented' RETURN s.key"


class PlanPrefixBase:
    """A roadmap whose graph carries SEED, served for the whole of each test.

    The server is started BEFORE the seed, and it has to be. `rmp graph client`
    is the only way to run a statement and it needs something listening for
    every one, while starting a server is itself what creates the graph store
    (SPEC/COMMANDS.md "Serve"). The old fixture seeded through `rmp graph
    execute` and started a server over what that left behind; the ordering
    described a subcommand that no longer exists, and the chicken-and-egg it
    worked around has dissolved with it.
    """

    # A realistic roadmap for a graph of specifications: each test gets its own
    # temporary HOME, so one name serves them all.
    ROADMAP = "documentation-index"

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.ROADMAP
        self.server = self.test.served_roadmap(self.roadmap, SEED)

    def teardown_method(self):
        # GroadmapTestBase.teardown() kills the server before it removes the
        # temporary HOME the server is serving out of.
        self.test.teardown()

    def statement(self, query):
        """Run one statement through `rmp graph client` against the running
        server, and return its parsed stdout. Fails loudly, with the
        invocation's own streams, when the statement did not succeed.
        """
        return self.test.graph_ok(self.roadmap, query=query)


class TestPlanPrefixes(PlanPrefixBase):
    """The prefixes themselves, through `rmp graph client` against a running
    server -- which is the only path to a graph there is.
    """

    def test_explain_publishes_a_plan_and_no_measurement(self):
        result = self.statement(f"EXPLAIN {READ}")

        assert "plan" in result, (
            "an EXPLAIN must publish a `plan` member; without it its output is "
            f"indistinguishable from a query that matched nothing. Got {result!r}")
        assert "profile" not in result, (
            "an EXPLAIN must not publish a `profile` member: at most one of the two "
            f"ever appears, which is what keeps an estimate from reading as a "
            f"measurement. Got {result!r}")
        assert result["plan"].get("operator"), (
            f"every plan node carries a non-empty `operator`. Got {result['plan']!r}")

        # An EXPLAIN executed nothing, so it measured nothing.
        def assert_unmeasured(node, path="plan"):
            for key in ("rows", "timeNs", "dbHits", "rowsRemovedByFilter"):
                assert key not in node, (
                    f"a plan tree carries no measured key, but {path} has {key!r}: an "
                    "EXPLAIN never ran, so a figure here would be invented "
                    "(SPEC/DATA_FORMATS.md 'Graph Plan Node' rule 7)")
            for i, child in enumerate(node.get("children", [])):
                assert_unmeasured(child, f"{path}.children[{i}]")

        assert_unmeasured(result["plan"])

    def test_profile_publishes_the_measurement(self):
        result = self.statement(f"PROFILE {READ}")

        assert "profile" in result and "plan" not in result, (
            f"a PROFILE publishes `profile` and never `plan`. Got {result!r}")
        assert len(result["rows"]) == 2, (
            f"PROFILE runs the statement and returns its real rows. Got {result['rows']!r}")

        def find(node, key):
            if key in node:
                return node[key]
            for child in node.get("children", []):
                found = find(child, key)
                if found is not None:
                    return found
            return None

        assert find(result["profile"], "rows") is not None, (
            "a profile tree carries the measured `rows`")
        assert find(result["profile"], "timeNs") is not None, (
            "a profile tree carries the measured `timeNs`")

        removed = find(result["profile"], "rowsRemovedByFilter")
        if removed is not None:
            assert removed == 1, (
                "the predicate rejected one of the three seeded specs, so "
                f"rowsRemovedByFilter is 1 and not an echo of the 2 emitted rows. Got {removed!r}")

    def test_two_executions_of_one_statement_agree_where_the_clock_cannot_reach(self):
        """Acceptance Criterion 60: DETERMINISM, which no per-field assertion sees.

        The retired TestPlanPrefixParityAcrossSurfaces compared two SURFACES and
        was rightly deleted with the second of them. This compares two
        EXECUTIONS through the one client, which is a different property, and it
        is the one SPEC/DATA_FORMATS.md "Graph Client Result" rule 5 actually
        states: a statement carrying EXPLAIN is identical in every byte between
        runs, and a PROFILE is identical everywhere the clock does not reach.

        It catches what the detailed assertions in this class cannot. Each of
        those checks one field at a time, so output whose plan children arrive
        in a different order on each run, or whose object keys are emitted
        straight from a map, satisfies every one of them and is still
        non-deterministic. A consumer diffing two runs would see churn that
        means nothing, and a real change would hide in it.
        """
        first_rc, first_out, first_err = self.test.graph_client(
            self.roadmap, query=f"EXPLAIN {READ}")
        second_rc, second_out, second_err = self.test.graph_client(
            self.roadmap, query=f"EXPLAIN {READ}")
        assert first_rc == 0 and second_rc == 0, (
            f"both runs must succeed: {first_rc}/{first_err!r}, {second_rc}/{second_err!r}")
        assert first_out == second_out, (
            "two EXPLAINs of one statement against an unchanged graph must be identical "
            "byte for byte -- an EXPLAIN runs nothing, so there is no measurement to "
            f"differ and nothing else may.\nfirst:  {first_out!r}\nsecond: {second_out!r}")

        # A PROFILE ran, so exactly one thing may differ: the clock. Everything
        # else it publishes is a property of the graph and the plan, and two runs
        # over an unchanged graph must agree on all of it.
        def without_timing(node, path="profile"):
            """The node with every `timeNs` removed at every depth, and the paths
            where one was found -- so an absent measurement fails loudly instead
            of making the comparison vacuously true."""
            found = []
            if "timeNs" in node:
                found.append(path)
            stripped = {k: v for k, v in node.items() if k not in ("timeNs", "children")}
            children = []
            for i, child in enumerate(node.get("children", [])):
                child_stripped, child_found = without_timing(child, f"{path}.children[{i}]")
                children.append(child_stripped)
                found.extend(child_found)
            if "children" in node:
                stripped["children"] = children
            return stripped, found

        first = self.statement(f"PROFILE {READ}")
        second = self.statement(f"PROFILE {READ}")
        first_profile, first_timed = without_timing(first["profile"])
        second_profile, second_timed = without_timing(second["profile"])

        assert first_timed and second_timed, (
            "a PROFILE measured the run it published, so `timeNs` must be PRESENT in "
            "both -- without this the comparison below would pass over two outputs "
            f"that simply carry no measurement. first={first_timed!r} second={second_timed!r}")
        assert first_timed == second_timed, (
            "the two runs measured the same plan, so a `timeNs` must appear at the same "
            f"places in both. first={first_timed!r} second={second_timed!r}")
        assert first_profile == second_profile, (
            "two PROFILEs of one statement against an unchanged graph must agree "
            "everywhere the clock does not reach: the rows, the dbHits, the operators "
            "and the shape of the tree are properties of the graph and the plan, not of "
            f"when it ran.\nfirst:  {first_profile!r}\nsecond: {second_profile!r}")
        assert first["rows"] == second["rows"], (
            f"the same read over an unchanged graph returns the same rows. "
            f"first={first['rows']!r} second={second['rows']!r}")

    def test_explain_of_a_write_neither_claims_success_nor_runs(self):
        """The worst of the three defects: {"ok": true} over a statement that
        wrote nothing, byte-identical to a real committed write."""
        result = self.statement("EXPLAIN CREATE (n:ShouldNeverExist)")

        assert "ok" not in result, (
            "an EXPLAIN of a writing statement must not publish {\"ok\": true}: that is "
            "the shape a real committed write publishes, and the statement wrote "
            f"nothing (SPEC/GRAPH.md 'Query Plans' rule 8). Got {result!r}")
        assert "plan" in result, f"it must publish its plan instead. Got {result!r}"
        assert result["columns"] == [], (
            "a prefixed statement declaring no column publishes an empty `columns` "
            f"array, never null. Got {result.get('columns')!r}")
        assert result["rows"] == [], (
            f"and an empty `rows` array. Got {result.get('rows')!r}")

        # The decisive assertion, made against the graph rather than the output.
        count = self.statement("MATCH (n:ShouldNeverExist) RETURN count(n)")
        assert count["rows"][0][0] == 0, (
            "EXPLAIN reports a plan and executes nothing, but the node exists: "
            f"the prefix ran the statement. Count was {count['rows'][0][0]!r}")

    def test_prefixes_are_case_insensitive(self):
        for prefix in ("explain", "ExPlAiN", "EXPLAIN"):
            result = self.statement(f"{prefix} {READ}")
            assert "plan" in result, (
                f"the {prefix!r} prefix must be recognised; the engine accepts it "
                f"case-insensitively. Got {result!r}")

    def test_unprefixed_output_is_unchanged(self):
        """The compatibility guard. The envelope departure is safe only because
        it is confined to prefixed statements."""
        read = self.statement(READ)
        assert "plan" not in read and "profile" not in read, (
            f"an unprefixed read publishes neither member. Got {read!r}")
        assert set(read) == {"columns", "rows"}, (
            f"and exactly the two keys it always published. Got {sorted(read)!r}")

        write = self.statement("CREATE (:Spec {key:'DEPLOY.md', status:'draft'})")
        assert "plan" not in write and "profile" not in write, (
            f"an unprefixed write publishes neither member either. Got {write!r}")
        # `ok` keeps its meaning, its value and its position, and the one member
        # additive to it is the counters of what the write applied
        # (SPEC/DATA_FORMATS.md "Graph Write Result"). The key set is asserted
        # in full, as it was before that member existed, so a THIRD key still
        # fails here.
        assert write == {
            "ok": True,
            "counters": {"nodesCreated": 1, "propertiesWritten": 2, "labelsAdded": 1},
        }, (
            "an unprefixed write still publishes {\"ok\": true}, now beside the "
            "counters of the node it created: the discriminator is departed from "
            f"only where a plan must be carried. Got {write!r}")


# --------------------------------------------------------------------------
# RETIRED: TestPlanPrefixParityAcrossSurfaces
#
# All three of its cases compared the stdout of `rmp graph execute` with the
# stdout of `rmp graph client` for one statement: an EXPLAIN of a read, a
# PROFILE of a read with `timeNs` stripped, and an EXPLAIN of a write. The
# second side of every one of those comparisons no longer exists -- `rmp graph
# execute` is withdrawn, and `rmp graph <anything but serve|client>` exits 127
# -- and a comparison of one surface with itself asserts nothing at all. They
# are deleted rather than reduced to a single-sided check, because the single
# side is already asserted, in more detail than the comparison ever was:
#
#   test_explain_is_byte_identical_across_both_surfaces
#       The EXPLAIN itself is covered by
#       TestPlanPrefixes.test_explain_publishes_a_plan_and_no_measurement,
#       which asserts the plan member, the absence of the profile member, the
#       non-empty operator, and the absence of every measured key at every
#       depth of the tree -- where the comparison only required the two
#       surfaces to agree, whatever they printed.
#
#   test_profile_agrees_across_both_surfaces_except_the_clock
#       Covered by TestPlanPrefixes.test_profile_publishes_the_measurement,
#       which asserts the profile member, the absence of the plan member, the
#       real rows, and the measured rows/timeNs/rowsRemovedByFilter figures.
#       `_strip_timing` went with the comparison: it existed only to excuse the
#       one key two runs cannot share.
#
#   test_explain_of_a_write_is_identical_across_both_surfaces
#       Covered by
#       TestPlanPrefixes.test_explain_of_a_write_neither_claims_success_nor_runs,
#       which is the stronger case of the two: besides refusing the {"ok": true}
#       shape it reads the graph back and proves the write did not happen.
# --------------------------------------------------------------------------


class TestWriteCounters(PlanPrefixBase):
    """What a write reports it changed, asserted on the object `rmp graph
    client` publishes (SPEC/DATA_FORMATS.md "Graph Client Result", rule 6;
    SPEC/GRAPH.md "Write Counters: What a Statement Changed").

    This class used to be TestWriteCounterParityAcrossSurfaces, and each of its
    three cases ran the same statement through `rmp graph execute` and through
    `rmp graph client` and compared the two stdouts byte for byte. That second
    side is gone with the subcommand, and the comparison with it: there is one
    surface, and comparing it with itself would assert nothing.

    NOTHING ELSE WAS DROPPED. Every case already asserted the object itself
    beside the comparison -- the complete key set and the exact counters -- and
    that is what remains, unchanged and still asserted in full, so a stray
    third member or a wrong figure fails here exactly as it did before.
    """

    # A gauge carrying both a property to reassign and a property to remove, so
    # that one statement exercises BOTH engine property counters.
    GAUGE = "CREATE (:Gauge {serial:'G-1', reading:1, spare:'x'})"
    # The mixed statement. A check over a pure SET would not reach the fold: the
    # Bolt protocol carries one property counter and no counterpart for a
    # removal, so an assignment and a removal have to arrive as ONE figure.
    MIXED = "MATCH (g:Gauge {serial:'G-1'}) SET g.reading = 12 REMOVE g.spare"

    def test_a_write_publishes_the_counters_of_what_it_created(self):
        create = "CREATE (:Widget {serial:'A-1', batch:7})"

        result = self.statement(create)

        assert result == {
            "ok": True,
            "counters": {"nodesCreated": 1, "propertiesWritten": 2, "labelsAdded": 1},
        }, (
            "a write publishes {\"ok\": true} beside the counters of exactly what "
            "it applied -- one labelled node carrying two properties -- and "
            f"nothing else. Got {result!r}")

    def test_a_mixed_property_write_folds_both_counters_into_one_figure(self):
        self.statement(self.GAUGE)

        result = self.statement(self.MIXED)

        assert result == {"ok": True, "counters": {"propertiesWritten": 2}}, (
            "the assignment and the removal are ONE figure of 2, published under "
            "a key named for the sum: the protocol carries one property counter "
            "and no counterpart for a removal, so an implementation that did not "
            "fold would report 1 here "
            '(SPEC/GRAPH.md "Write Counters: What a Statement Changed", rules 6 '
            f"to 9). Got {result!r}")

        # Non-vacuity: the statement really did both halves. A MATCH that
        # matched nothing would publish no counters at all, and the assertion
        # above would then be about a statement that did not run.
        gauge = self.statement(
            "MATCH (g:Gauge {serial:'G-1'}) RETURN g.reading, g.spare")
        assert gauge == {
            "columns": ["g.reading", "g.spare"], "rows": [[12, None]],
        }, (
            "the reassignment must be on the node and the removed property must "
            f"be gone from it. Got {gauge!r}")

    def test_a_read_carries_no_counters(self):
        """The compatibility half: a read is unchanged in every byte, and it was
        the bytes an existing consumer parsed."""
        result = self.statement(READ)

        assert set(result) == {"columns", "rows"}, (
            "a statement that changed nothing carries no `counters` key at all. "
            f"Got {result!r}")
        assert result == {
            "columns": ["s.key"], "rows": [["GRAPH.md"], ["BUILD.md"]],
        }, f"and it returns the seeded specs the predicate passes. Got {result!r}"


def _run_all():
    """Discover and run every Test* class defined in this module."""
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
    print(f"Graph plan-prefix tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if _run_all() else 1)
