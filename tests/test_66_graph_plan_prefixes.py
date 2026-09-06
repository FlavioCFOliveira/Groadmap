#!/usr/bin/env python3
"""
Test 66: end-to-end tests for the EXPLAIN and PROFILE statement prefixes
(rmp task #410).

SPEC/GRAPH.md "Query Plans: The EXPLAIN and PROFILE Prefixes",
SPEC/DATA_FORMATS.md "Graph Plan Node" and "Graph Client Result", and
SPEC/COMMANDS.md "Graph Management" are canonical for every assertion here.

## What this module proves that the Go suites cannot

`internal/graphjson` already pins the mapping's rules and `internal/commands`
already pins the envelope, both mutation-checked. Neither can prove the one
property that is the whole point of the design:

    for the same statement against the same graph, `rmp graph client` writes
    the bytes `rmp graph execute` writes

because that identity spans two PROCESSES reaching the graph two different
ways -- one opening the store in-process, the other crossing a Unix domain
socket and the Bolt protocol -- and it is asserted on the bytes on stdout,
not on a Go value. A test that could see both sides in one process would be
testing something else.

The identity holds exactly for every key whose value is a property of the
statement and the graph. It does NOT bind `timeNs`, and cannot: that key
measures the execution, and two executions measure two durations. The
comparison below therefore strips `timeNs` and asserts everything else --
which is the guarantee a caller actually relies on, since it means the
surface a statement ran through is not observable in the result.

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
import json
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from tests.base_test import GroadmapTestBase
from tests.test_65_graph_server_client_e2e import GraphServeProcess

EXIT_OK = 0

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


def _strip_timing(node):
    """Remove every `timeNs` from a decoded plan tree, in place.

    `timeNs` is the one key the execute/client identity does not bind: it
    measures the run, and the two surfaces run the statement twice.
    """
    node.pop("timeNs", None)
    for child in node.get("children", []):
        _strip_timing(child)
    return node


class PlanPrefixBase:
    """A roadmap whose graph carries SEED, plus a tracked server when needed."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        rc, out, err = self.run_cli(["graph", "execute", "-r", self.roadmap, "--query", SEED])
        assert rc == EXIT_OK, f"seeding failed: exit={rc} out={out!r} err={err!r}"
        self._servers = []

    def teardown_method(self):
        for server in self._servers:
            try:
                server.stop()
            except Exception:  # noqa: BLE001 - teardown must not mask a failure
                pass
        self.test.teardown()

    def run_cli(self, args, timeout=20.0):
        """One ./bin/rmp invocation, returning (exit_code, stdout, stderr)."""
        import subprocess

        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        result = subprocess.run(
            [self.test.cli_path] + args,
            capture_output=True, text=True, env=env, timeout=timeout,
        )
        return result.returncode, result.stdout, result.stderr

    def execute(self, query):
        """Run a statement through `graph execute` and return its parsed stdout."""
        rc, out, err = self.run_cli(["graph", "execute", "-r", self.roadmap, "--query", query])
        assert rc == EXIT_OK, f"`graph execute {query!r}` failed: exit={rc} stderr={err!r}"
        return json.loads(out)

    def start_server(self):
        server = GraphServeProcess(self.test, self.roadmap)
        self._servers.append(server)
        server.start()
        return server


class TestPlanPrefixesOnExecute(PlanPrefixBase):
    """The direct path, against the compiled binary."""

    def test_explain_publishes_a_plan_and_no_measurement(self):
        result = self.execute(f"EXPLAIN {READ}")

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
        result = self.execute(f"PROFILE {READ}")

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

    def test_explain_of_a_write_neither_claims_success_nor_runs(self):
        """The worst of the three defects: {"ok": true} over a statement that
        wrote nothing, byte-identical to a real committed write."""
        result = self.execute("EXPLAIN CREATE (n:ShouldNeverExist)")

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
        count = self.execute("MATCH (n:ShouldNeverExist) RETURN count(n)")
        assert count["rows"][0][0] == 0, (
            "EXPLAIN reports a plan and executes nothing, but the node exists: "
            f"the prefix ran the statement. Count was {count['rows'][0][0]!r}")

    def test_prefixes_are_case_insensitive(self):
        for prefix in ("explain", "ExPlAiN", "EXPLAIN"):
            result = self.execute(f"{prefix} {READ}")
            assert "plan" in result, (
                f"the {prefix!r} prefix must be recognised; the engine accepts it "
                f"case-insensitively. Got {result!r}")

    def test_unprefixed_output_is_unchanged(self):
        """The compatibility guard. The envelope departure is safe only because
        it is confined to prefixed statements."""
        read = self.execute(READ)
        assert "plan" not in read and "profile" not in read, (
            f"an unprefixed read publishes neither member. Got {read!r}")
        assert set(read) == {"columns", "rows"}, (
            f"and exactly the two keys it always published. Got {sorted(read)!r}")

        write = self.execute("CREATE (:Spec {key:'DEPLOY.md', status:'draft'})")
        assert write == {"ok": True}, (
            "an unprefixed write still publishes exactly {\"ok\": true}: the "
            f"discriminator is departed from only where a plan must be carried. Got {write!r}")


class TestPlanPrefixParityAcrossSurfaces(PlanPrefixBase):
    """The identity `execute` and `client` are required to hold, asserted on the
    bytes of two separate processes reaching the graph two different ways."""

    def test_explain_is_byte_identical_across_both_surfaces(self):
        direct_rc, direct_out, _ = self.run_cli(
            ["graph", "execute", "-r", self.roadmap, "--query", f"EXPLAIN {READ}"])
        assert direct_rc == EXIT_OK

        server = self.start_server()
        client_rc, client_out, client_err = self.run_cli(
            ["graph", "client", "-r", self.roadmap, "--socket", server.socket,
             "--query", f"EXPLAIN {READ}"])
        assert client_rc == EXIT_OK, f"client failed: exit={client_rc} stderr={client_err!r}"

        assert direct_out == client_out, (
            "`rmp graph client` must write the bytes `rmp graph execute` writes for the "
            "same statement against the same graph. An EXPLAIN carries no measured "
            "figure, so the two are identical byte for byte with nothing excused "
            "(SPEC/DATA_FORMATS.md 'Graph Client Result').\n"
            f"execute:\n{direct_out}\nclient:\n{client_out}")

    def test_profile_agrees_across_both_surfaces_except_the_clock(self):
        direct = self.execute(f"PROFILE {READ}")

        server = self.start_server()
        rc, out, err = self.run_cli(
            ["graph", "client", "-r", self.roadmap, "--socket", server.socket,
             "--query", f"PROFILE {READ}"])
        assert rc == EXIT_OK, f"client failed: exit={rc} stderr={err!r}"
        via_client = json.loads(out)

        _strip_timing(direct["profile"])
        _strip_timing(via_client["profile"])
        assert direct == via_client, (
            "a PROFILE must agree across both surfaces in every key except `timeNs`, "
            "which measures the run rather than describing the result and therefore "
            "differs between two executions. Everything else -- the operators, the "
            "structure, the ordering, the measured rows and db-hits -- is identical.\n"
            f"execute: {direct!r}\nclient:  {via_client!r}")

    def test_explain_of_a_write_is_identical_across_both_surfaces(self):
        """The envelope departure must be the same departure on both paths: two
        discriminators that disagreed would make the surface observable."""
        direct_rc, direct_out, _ = self.run_cli(
            ["graph", "execute", "-r", self.roadmap, "--query", "EXPLAIN CREATE (n:ShouldNeverExist)"])
        assert direct_rc == EXIT_OK

        server = self.start_server()
        client_rc, client_out, client_err = self.run_cli(
            ["graph", "client", "-r", self.roadmap, "--socket", server.socket,
             "--query", "EXPLAIN CREATE (n:ShouldNeverExist)"])
        assert client_rc == EXIT_OK, f"client failed: exit={client_rc} stderr={client_err!r}"

        assert direct_out == client_out, (
            "both surfaces must depart from the columns discriminator identically for a "
            f"prefixed writing statement.\nexecute:\n{direct_out}\nclient:\n{client_out}")
        assert "ok" not in json.loads(client_out), (
            "and neither may claim a write that did not happen")


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
