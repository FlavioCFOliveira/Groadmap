#!/usr/bin/env python3
"""
Test 63: one refusal for an invalid roadmap name, whatever command was typed
(rmp task #325), measured against the compiled ./bin/rmp.

The defect. `internal/commands/openGraphStore` re-applied `utils.ErrValidation`
to the error `utils.GetRoadmapDir` had already classified, so the `graph` family
alone printed the class twice:

    $ rmp graph <statement subcommand> -r CON --query "MATCH (n) RETURN n"
    Error: validation error: validation error: "CON": roadmap name is a reserved system name
    $ rmp task list -r CON
    Error: validation error: "CON": roadmap name is a reserved system name

The exit code was right (6) and the sentence after the prefix was right; only
the classification was stated twice. SPEC/COMMANDS.md § "Published Error Strings
Are Exact" publishes ONE sentinel between the `Error: ` prefix and the detail,
and § "Roadmap Name Validation" publishes these five refusals with the sentinel
present on two of them and absent on the other three -- so the same wrap also
INVENTED a `validation error: ` prefix in front of the three that must not carry
one. Three of the five refusals the `graph` family printed were therefore words
the SPEC does not contain.

Why this module exists next to test_55. That module drives every string
SPEC/COMMANDS.md publishes and compares it character for character against the
binary, which is what caught the 121 divergences it was written for. It reaches
the roadmap-name refusals through `roadmap create`, once each, because one
driver per published string is all a coverage gate needs. The defect above lived
in a DIFFERENT family reaching the SAME string, so a per-string gate could not
see it: `roadmap create con` was correct throughout. What was missing was a
per-FAMILY comparison, and that is what this module adds.

What is asserted, for each of four invalid names:

  * `roadmap create <name>` supplies the reference line, because that is the
    invocation SPEC/COMMANDS.md § Roadmap Name Validation documents and the one
    test_55 already pins to the published string. Nothing here is a second copy
    of a published literal.
  * every other entry point -- the five SQLite-backed families and BOTH `graph`
    subcommands -- must print that line BYTE FOR BYTE and exit with the same
    code.
  * the classification `validation error` appears in each family's line exactly
    as often as it appears in the reference, so a family may neither double it
    nor add one where the SPEC publishes none.
  * stdout is empty (SPEC/HELP.md § Stdout silence on failure).

Non-vacuity. The four names are asserted to produce four DISTINCT reference
lines, at least one carrying the classification and at least one not, and every
invocation is required to exit 6 -- so a run in which every command happened to
fail for some unrelated shared reason cannot pass.

The graph half of the table is now TWO subcommands, not five. `rmp graph
execute` is withdrawn along with the four operation-class subcommands that
preceded it; the graph is reached only through a running `rmp graph serve`,
spoken to by `rmp graph client` (SPEC/GRAPH.md § The Dedicated Graph Server).
Shrinking the table does not weaken what it proves, and it is worth saying
exactly why:

  * The defect was never about how MANY graph subcommands existed. It was about
    ONE code path -- the graph family's own roadmap resolution -- wrapping an
    already-classified error a second time. Two subcommands reach that path, and
    both are driven, so the path is covered exactly as it was when five reached
    it. Five drivers of one function were five copies of one assertion.
  * The two are not interchangeable. `graph client` resolves the roadmap AFTER
    reading the statement, and `graph serve` resolves it with no statement in
    play at all and goes on to create a directory, bind a socket and take a lock
    if it is not refused. They enter the shared path from opposite sides, which
    is a stronger pair than any two of the five retired subcommands were.
  * The table cannot silently shrink further. test_every_graph_subcommand_is_
    driven reads the subcommand list out of the binary's own machine-readable
    contract (`rmp --ai-help`) and requires the drivers here to cover it, so a
    third subcommand added later fails this module until it is driven too, and a
    subcommand deleted without its driver being removed fails it as well.

Nothing is written by any invocation: the name is invalid, so the refusal
precedes the graph directory being created, any socket being bound and any store
being opened, and the throwaway HOME this module runs under holds no roadmap at
all.
"""

import inspect
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


# SPEC/ARCHITECTURE.md § Exit Codes: a rejected value is 6.
EXIT_VALIDATION = 6

# The classification whose repetition is the defect. It is written once here and
# every expectation counts occurrences of it; no expectation spells out a whole
# refusal line, which all come from the binary itself.
CLASSIFICATION = "validation error"

# The bound on one invocation. Every command here is expected to be refused for
# its roadmap name and to exit at once. `graph serve` is why the bound is
# written down: a regression that resolved the name after binding the socket
# would start a long-lived server, and this turns that into a failed test rather
# than a hung suite.
INVOCATION_TIMEOUT_SECONDS = 30.0

# The four names, one per rule utils.ValidateRoadmapName enforces that a `-r`
# value can actually reach. The fifth rule -- the empty name -- is refused
# earlier as "no roadmap selected" (exit 3) on every family, so it never reaches
# the name validator through `-r` and is not a case here.
#
# Two of these produce a line carrying the classification and two produce a line
# the SPEC publishes without one, which is what makes the corpus cover both
# halves of the defect: the wrap doubled the prefix on the first pair and
# invented one on the second.
INVALID_NAMES = [
    # Reserved on Windows; checked before the charset rule, so the uppercase
    # form reaches the reserved branch rather than the regex branch.
    ("reserved system name", "CON"),
    # A leading hyphen is refused so a name can never be read as a flag.
    ("leading hyphen", "-payments"),
    # '#' is outside ^[a-z0-9_-]+$.
    ("characters outside the charset", "payments#gateway"),
    # One character past the 50-character maximum.
    ("longer than the maximum", "n" * 51),
]

# The invocation that supplies the reference line for each name.
REFERENCE = ("roadmap create", lambda name: ["roadmap", "create", name])

# A statement that would succeed against a served roadmap, so that `graph
# client` is refused for the NAME and for nothing about the Cypher. It never
# reaches an engine here -- the name is refused first -- but a statement the
# engine would reject would make the case ambiguous the day the ordering moved.
GRAPH_STATEMENT = "MATCH (s:Spec) RETURN s.key"

# Every other entry point that resolves a roadmap by name. The five
# SQLite-backed families reach utils.GetRoadmapDir through db.OpenExisting; the
# two graph subcommands reach it through commands.resolveGraphDir, which is the
# successor of the openGraphStore the defect lived in.
#
# The two graph entries enter that shared resolution from opposite sides:
# `client` resolves the name after it has read the statement and before it
# touches the socket, and `serve` resolves it with no statement at all and
# before it creates the graph directory, binds a socket or takes the store lock.
ENTRY_POINTS = [
    ("task list", lambda name: ["task", "list", "-r", name]),
    ("sprint list", lambda name: ["sprint", "list", "-r", name]),
    ("backlog list", lambda name: ["backlog", "list", "-r", name]),
    ("audit list", lambda name: ["audit", "list", "-r", name]),
    ("stats", lambda name: ["stats", "-r", name]),
    ("graph client", lambda name: [
        "graph", "client", "-r", name, "--query", GRAPH_STATEMENT]),
    ("graph serve", lambda name: ["graph", "serve", "-r", name]),
]


def error_line(stderr):
    """The refusal itself: the first non-blank line of stderr.

    SPEC/HELP.md § Stderr part order puts the error line first and separates the
    parts with blank lines, so this is the whole line the user reads as the
    verdict, prefix and sentinel included.
    """
    for line in stderr.splitlines():
        if line.strip():
            return line
    return ""


class TestRoadmapNameRefusalParity:
    """Every family must surface the roadmap-name refusal unchanged."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def run(self, args):
        """One invocation, with standard input supplied and already at end of
        stream, and under a time bound.

        run_cli rather than run_cmd: it never raises on a non-zero exit -- every
        invocation here is expected to fail -- and it carries the timeout that
        keeps a regression in `graph serve` from hanging the suite. The empty
        stdin closes the other hazard: a `graph client` that reached its
        standard-input read would find end of stream and fail, instead of
        waiting for a statement nobody is going to type.
        """
        return self.test.run_cli(args, stdin_text="", timeout=INVOCATION_TIMEOUT_SECONDS)

    def published_graph_subcommands(self):
        """The graph subcommands the BINARY publishes, read out of its own
        machine-readable command contract.

        This is what keeps the shrunken table honest. The set is not written
        down here, so it cannot fall out of step with the CLI: a subcommand
        added later is reported by --ai-help and fails
        test_every_graph_subcommand_is_driven until a driver for it is added.
        """
        code, stdout, stderr = self.run(["--ai-help"])
        assert code == 0, f"`rmp --ai-help` exits {code}; stderr={stderr!r}"
        contract = json.loads(stdout)
        graph = [c for c in contract["commands"] if c["name"] == "graph"]
        assert len(graph) == 1, (
            f"the contract must publish exactly one `graph` command; got {len(graph)}")
        return {sub["name"] for sub in graph[0]["subcommands"]}

    def reference_for(self, name):
        """Drive the documented invocation and return (line, exit code)."""
        label, build = REFERENCE
        code, stdout, stderr = self.run(build(name))
        line = error_line(stderr)
        assert code == EXIT_VALIDATION, (
            f"{label} {name!r}: exit={code}, want {EXIT_VALIDATION}; stderr={stderr!r}")
        assert stdout == "", f"{label} {name!r}: stdout must stay empty, got {stdout!r}"
        assert line.startswith("Error: "), (
            f"{label} {name!r}: the reference is not an error line: {line!r}")
        return line, code

    def test_every_family_renders_the_same_refusal(self):
        for note, name in INVALID_NAMES:
            want_line, want_code = self.reference_for(name)
            for label, build in ENTRY_POINTS:
                code, stdout, stderr = self.run(build(name))
                line = error_line(stderr)
                assert line == want_line, (
                    f"{label} renders the {note} refusal differently from "
                    f"`{REFERENCE[0]}`\n"
                    f"     got: {line!r}\n"
                    f"    want: {want_line!r}\n"
                    f"    note: a family must not restate a classification the "
                    f"error already carries (rmp task #325)")
                assert code == want_code, (
                    f"{label}: exit={code} for the {note}, want {want_code} "
                    f"(the code `{REFERENCE[0]}` returns); stderr={stderr!r}")
                assert stdout == "", (
                    f"{label}: stdout must stay empty on a refusal, got {stdout!r}")

    def test_classification_is_stated_once(self):
        for note, name in INVALID_NAMES:
            want_line, _ = self.reference_for(name)
            want_count = want_line.count(CLASSIFICATION)
            assert want_count <= 1, (
                f"`{REFERENCE[0]}` already states {CLASSIFICATION!r} {want_count} "
                f"times for the {note}: {want_line!r}")
            for label, build in ENTRY_POINTS:
                _code, _stdout, stderr = self.run(build(name))
                line = error_line(stderr)
                got = line.count(CLASSIFICATION)
                assert got == want_count, (
                    f"{label} states {CLASSIFICATION!r} {got} time(s) for the {note}, "
                    f"`{REFERENCE[0]}` states it {want_count} time(s)\n"
                    f"    line: {line!r}")

    def test_the_corpus_covers_both_shapes(self):
        """Non-vacuity: four distinct refusals, both with and without the
        sentinel, so neither half of the defect can pass unobserved."""
        lines = []
        for _note, name in INVALID_NAMES:
            line, _code = self.reference_for(name)
            lines.append(line)

        assert len(set(lines)) == len(lines), (
            f"the four names must produce four distinct refusals; got {lines!r}")
        with_sentinel = [line for line in lines if CLASSIFICATION in line]
        without_sentinel = [line for line in lines if CLASSIFICATION not in line]
        assert with_sentinel, (
            f"no case carries {CLASSIFICATION!r}, so doubling it could not be "
            f"observed; got {lines!r}")
        assert without_sentinel, (
            f"every case carries {CLASSIFICATION!r}, so inventing one where the "
            f"SPEC publishes none could not be observed; got {lines!r}")

    def test_every_graph_subcommand_is_driven(self):
        """The graph half of the table must cover the whole family.

        The bug report named two of the five subcommands that then existed; the
        table drove all five, because the site they shared was reached by all
        five. Two subcommands reach that site now -- `serve` and `client` -- and
        the requirement is unchanged in substance: whatever the family consists
        of, every member of it is driven here.

        The expected set is READ FROM THE BINARY rather than written down, which
        is what stops this from becoming a comparison of one literal against
        another. A third subcommand added later appears in --ai-help and fails
        this assertion until a driver is added to ENTRY_POINTS; a subcommand
        removed without its driver fails it in the other direction.
        """
        driven = {label.split(" ", 1)[1] for label, _build in ENTRY_POINTS
                  if label.startswith("graph ")}
        published = self.published_graph_subcommands()
        assert driven == published, (
            f"the graph subcommands driven here are {sorted(driven)}, and the binary "
            f"publishes {sorted(published)}. Every subcommand that resolves a roadmap "
            f"by name must be driven, because the roadmap-name refusal is emitted from "
            f"a path they share and a family member that stops sharing it is exactly "
            f"the defect this module exists for.")

    def test_the_graph_subcommands_are_refused_before_they_touch_the_filesystem(self):
        """The refusal precedes creating a graph directory and binding a socket.

        This is what makes it safe to drive `graph serve` with an invalid name
        at all, and it is a property worth asserting rather than assuming:
        `serve` is the one command in the CLI that CREATES a roadmap graph, so a
        name check that ran after the creation would leave a store behind for a
        roadmap that can never exist.
        """
        roadmaps_dir = self.test.home_dir / ".roadmaps"
        before = sorted(os.listdir(roadmaps_dir)) if roadmaps_dir.exists() else []

        for _note, name in INVALID_NAMES:
            for label, build in ENTRY_POINTS:
                if not label.startswith("graph "):
                    continue
                code, _stdout, stderr = self.run(build(name))
                assert code == EXIT_VALIDATION, (
                    f"{label} {name!r}: exit={code}, want {EXIT_VALIDATION}; stderr={stderr!r}")

        after = sorted(os.listdir(roadmaps_dir)) if roadmaps_dir.exists() else []
        assert after == before, (
            f"a refused graph invocation changed the set of roadmap homes: "
            f"{before!r} then {after!r}")


def _run_all():
    passed = 0
    failed = 0
    failures = []
    # Classes are DISCOVERED by inspecting this module, never listed, so a
    # suite added later cannot be silently skipped.
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
                print(f"PASS {label}")
            except Exception as exc:  # noqa: BLE001 - the harness reports every failure
                failed += 1
                failures.append((label, exc))
                print(f"FAIL {label}: {exc}")
            finally:
                instance.teardown_method()

    print(f"\n{passed} passed, {failed} failed")
    if failures:
        for label, exc in failures:
            print(f"\n--- {label} ---\n{exc}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(_run_all())
