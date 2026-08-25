#!/usr/bin/env python3
"""
Test 63: one refusal for an invalid roadmap name, whatever family was typed
(rmp task #325), measured against the compiled ./bin/rmp.

The defect. `internal/commands/openGraphStore` re-applied `utils.ErrValidation`
to the error `utils.GetRoadmapDir` had already classified, so the `graph` family
alone printed the class twice:

    $ rmp graph query -r CON --query "MATCH (n) RETURN n"
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
  * every other entry point -- the four SQLite-backed families and all five
    `graph` subcommands -- must print that line BYTE FOR BYTE and exit with the
    same code.
  * the classification `validation error` appears in each family's line exactly
    as often as it appears in the reference, so a family may neither double it
    nor add one where the SPEC publishes none.
  * stdout is empty (SPEC/HELP.md § Stdout silence on failure).

Non-vacuity. The four names are asserted to produce four DISTINCT reference
lines, at least one carrying the classification and at least one not, and every
invocation is required to exit 6 -- so a run in which every command happened to
fail for some unrelated shared reason cannot pass.

All five `graph` subcommands are driven, not just the two the bug report named.
`create`, `update` and `delete` classify the query before they resolve the
roadmap, so a read query makes them refuse earlier and never reach the site;
each is therefore given a query of its OWN operation class, which is what lets
it reach the roadmap-name check. Nothing is written by any of them: the name is
invalid, so the refusal precedes the graph store being opened, and the throwaway
HOME this module runs under holds no roadmap at all.
"""

import inspect
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

# Every other entry point that resolves a roadmap by name. The four
# SQLite-backed families reach utils.GetRoadmapDir through db.OpenExisting; the
# five graph subcommands reach it through commands.openGraphStore, which is
# where the defect lived. Each graph query is of the subcommand's own operation
# class, so the guard rail passes it through to the roadmap-name check.
ENTRY_POINTS = [
    ("task list", lambda name: ["task", "list", "-r", name]),
    ("sprint list", lambda name: ["sprint", "list", "-r", name]),
    ("backlog list", lambda name: ["backlog", "list", "-r", name]),
    ("audit list", lambda name: ["audit", "list", "-r", name]),
    ("stats", lambda name: ["stats", "-r", name]),
    ("graph query", lambda name: [
        "graph", "query", "-r", name, "--query", "MATCH (s:Spec) RETURN s.key"]),
    ("graph search", lambda name: [
        "graph", "search", "-r", name, "--query", "MATCH p=(a:Spec)-[*1..3]-(b:Spec) RETURN p"]),
    ("graph create", lambda name: [
        "graph", "create", "-r", name, "--query", "CREATE (:Spec {key:'payment-capture'})"]),
    ("graph update", lambda name: [
        "graph", "update", "-r", name, "--query",
        "MATCH (s:Spec {key:'payment-capture'}) SET s.status = 'ready'"]),
    ("graph delete", lambda name: [
        "graph", "delete", "-r", name, "--query",
        "MATCH (s:Spec {key:'refund-flow'}) DETACH DELETE s"]),
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
        return self.test.run_cmd(args, check=False)

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

    def test_all_five_graph_subcommands_are_driven(self):
        """The bug report named `query` and `search`; the site they share is
        reached by all five, and this module must keep covering all five."""
        driven = {label for label, _build in ENTRY_POINTS if label.startswith("graph ")}
        expected = {"graph query", "graph search", "graph create", "graph update",
                    "graph delete"}
        assert driven == expected, (
            f"the graph subcommands driven here are {sorted(driven)}, want "
            f"{sorted(expected)}")


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
