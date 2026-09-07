#!/usr/bin/env python3
"""
Test 60: DOCS/README completeness against the AI Agent Contract (rmp task #126).

WHY THIS IS A SECOND DIRECTION, NOT A DUPLICATE
------------------------------------------------
Four guards already hold documentation to the AI Agent Contract:

  * internal/aihelp/example_invocations_test.go -- every `rmp ...` line printed
    in SPEC/, DOCS/ or README resolves against the contract. It deliberately
    stops at NAMES: commands, subcommands and flag spellings, never values.
  * internal/aihelp/documented_status_values_test.go -- documented fenced flag
    VALUES, by declared flag type.
  * internal/models/docs_enum_coverage_test.go -- the audit operation enum
    across four surfaces.
  * internal/commands/sort_field_parity_test.go -- the `--sort` table.

Every one of those reads DOCS -> contract: it proves nothing documented is
FICTIONAL (validity). None of them reads the other way. This module reads
contract -> DOCS: it proves nothing the contract PUBLISHES is undocumented
(completeness). The two properties are independent -- a page can carry a
flag table that is 100% valid (every entry resolves) while still omitting a
flag the registry added yesterday, and the four guards above would stay green
throughout, because a validity check only ever inspects what IS written down.
rmp task #126, comment #78, drew the same line between this module and
internal/aihelp/example_invocations_test.go: that module gates EXAMPLES (running
prose asking "would this command line run?"); this module gates TABLES (a
documentation table asking "does it agree with the catalogue?"). The two
corpora do not overlap.

THE CONTRACT IS THE ORACLE
---------------------------
`rmp --ai-help` is generated at runtime from the internal command
registry (internal/commands/registry_data.go and friends), so it is the
single source of truth for what flags and exit codes a subcommand can
actually produce. DOCS/commands/*.md and README.md are maintained by hand and
are the party that can drift.

WHAT IS COMPARED, AND AT WHAT GRANULARITY
------------------------------------------
Flags are checked PER SUBCOMMAND: the contract publishes a `flags` array on
every subcommand object, and every DOCS/commands/<family>.md page documents
flags in a per-subcommand table (`### <name>` ... `| Short Flag | Long Flag |
...`), or -- for the three commands with exactly one, self-named subcommand
(`web`, `stats`, `ai-help`) -- in the single flag table the whole page carries.

Exit codes are checked PER FAMILY, not per subcommand. The contract publishes
`exit_codes` per subcommand, but every DOCS/commands/<family>.md page
publishes exactly ONE "## Exit Codes" table for the whole family (task.md,
for instance, has 19 subcommands and one such table). The natural, and only
possible, comparison is therefore the UNION of every subcommand's exit codes
in a family against that family's single table -- checked directionally, the
same as flags: every code the union contains must appear in the table.

README.md publishes a single application-wide exit-code table, checked
against the contract's top-level `exit_codes` catalogue the same way.

DECISION: DISPATCHER-LEVEL EXIT CODES (rmp task #326)
------------------------------------------------------
The exit-code leg described above is directional AND per-subcommand: it demands
of a family's DOCS table only what the UNION of that family's subcommand
`exit_codes` arrays contains. Exit code 127 (`EXIT_CMD_NOT_FOUND`) falls outside
that union by construction -- the family DISPATCHER emits it before any
subcommand resolves, so no subcommand object in the contract can publish it.
Six DOCS pages (task, sprint, backlog, audit, graph and roadmap) therefore
omitted 127 while every one of those six families really exits 127 for an
unresolved subcommand, and this module stayed green throughout: not because the
pages were complete, but because the code was invisible to the only question it
asked.

The choice this leaves -- teach the guard about dispatcher-level codes, or
declare in the guard why a DOCS table may carry a code the per-subcommand
arrays do not publish -- is settled here, in favour of the first. A DOCS
"## Exit Codes" table is a table of what the command can emit; a code the
command emits belongs in it whatever layer of the binary emits it, and a guard
that can never demand such a code is a guard the drift simply walks past.

The gap is closed WITHOUT touching the published contract. `rmp --ai-help` is
unchanged: the new leg (section 3 below) never asks the contract which commands
dispatch subcommands, it asks the BINARY, by reusing the probe
tests/test_61_family_help_dispatch_exit_code.py already performs
(`ContractProbeFixture.dispatching_commands()`, imported below). So the two
exit-code legs have two different oracles by design -- the contract for what a
resolved subcommand can emit, observed behaviour for what the dispatcher in
front of it emits -- and no module in the suite holds a second, hand-written
list of dispatching families that could drift from the binary while staying
green.

DECISION: A CODE A SUBCOMMAND EMITS THAT ITS OWN ARRAY OMITS (rmp task #336)
-----------------------------------------------------------------------------
Section 2 and the dispatcher leg just described cover two of the three ways a
DOCS "## Exit Codes" table can be incomplete. The third is this one: a
subcommand emits a code, and the `exit_codes` array the contract publishes FOR
THAT SUBCOMMAND does not carry it. `rmp roadmap create` with no name exits 2
with "Error: required parameter missing: roadmap name required", raised by
roadmapCreate itself and listed by that subcommand's own `--help`; the array
`rmp --ai-help` publishes for it is [0, 5, 6]; DOCS/commands/roadmap.md's table
lists 0, 4, 5, 6 and 127. Section 2 compared nothing here, because it demands of
a page only what the UNION of that family's arrays contains, and 2 is in no
array of the roadmap family. The dispatcher leg does not reach it either: 2 is
raised inside the subcommand, after dispatch has already succeeded.

That makes this a defect in the ORACLE, not in the comparison. Section 2 is
correct and complete GIVEN a correct contract: the day `roadmap create`'s array
carries 2 the family union becomes {0, 2, 4, 5, 6}, section 2 turns red on
roadmap.md by itself, and this module needs no edit at all. A module whose
oracle is the contract cannot detect the contract itself being wrong, however
its comparison is rewritten -- so extending section 2 is not the remedy, and is
not what is decided here.

DECISION: this module does not close the third case, and the gate that would
close it is not added here. Two reasons, in order.

First, the gate belongs on the ORACLE, and a cheap total oracle for it already
exists. It is not the obvious one -- probing every subcommand along every
refusal path it can take, which is a far larger oracle than either leg above
and the reason this case is easy to leave open. It is the per-subcommand
`--help` "Exit codes:" block: 58 of the 59 subcommands print one, it is
hand-written INDEPENDENTLY of the registry array, and holding array against
block costs one subprocess per subcommand while deriving its subject list from
the contract. Such a check would have failed on this defect the day it was
written. `rmp ai-help` is the one subcommand that prints no such block, and
giving it one is part of that work.

Second, that gate cannot be landed green today, and the obstacle is not effort.
Measured against the current tree it reports 17 code-level disagreements across
15 subcommands: fifteen whose help block lists 2 while their array omits it
(roadmap create and remove; task get, remove, reopen, prio, sev, subtasks,
add-dep, remove-dep, blockers, blocking; backlog list; audit list and history),
and two of those same fifteen whose array lists 6 while their help block omits
it (task get, task reopen). Each is a disagreement between two published
records, so each needs a ruling on WHICH record is wrong, and every ruling that
lands on the array changes the published `rmp --ai-help` contract -- an owner
decision, not a documentation fix. The measurement is recorded here so the next
reader inherits the evidence instead of re-deriving it.

What is NOT deferred is the sweep. All nine DOCS pages were checked for this
shape against three records of what their commands emit -- the contract arrays,
the per-subcommand help blocks, and the family help block -- and two pages omit
a code their commands really emit: roadmap.md omits 2 (above), and stats.md
omits 2, which `rmp stats -r <name> <surplus-token>` emits through the shared
positional-arity check in internal/commands/positional_arity.go. The other
seven pages carry every code all three of those records name.

EXEMPTION: -h/--help
---------------------
Every subcommand in the registry carries -h/--help through `helpFlag()`
(internal/commands/registry_data.go), a helper the comment there says is
"attached to every subcommand" and "handled by the dispatcher... before any
validation runs" -- it is structural, not an authored choice per subcommand.
DOCS/commands/*.md documents it on some subcommands (backlog x2, graph x5,
stats, web -- 9 of 59 today) and omits it on the rest, including whole
families (roadmap, task, sprint, audit, ai-help) that never mention it once.
That is pure inconsistency of house style around a self-evident CLI
convention, not a reader being told a real flag does not exist -- so -h/--help
is excluded from the comparison in both directions. This is the one exemption
this module takes, and it is recorded here, not by silent omission from a
result set.

NON-VACUITY
-----------
Every comparison asserts a FLOOR on how many items it actually compared
(subcommand/flag pairs, per-family exit-code entries, dispatching families and
the codes read from each of their tables, README codes), mirroring
the coverage floors in test_55_error_string_parity.py and the
minDocumented*/docsMinOperationsFloor floors in
internal/aihelp/documented_status_values_test.go and
internal/models/docs_enum_coverage_test.go. A markdown table-header rename, a
subcommand heading rewrite, or a section-boundary regression that made a
parser match nothing would otherwise leave every set comparison vacuously
empty, and therefore green.

READABLE FAILURES
------------------
Every failure names the DOCS/README file, the family and subcommand (for
flags) or family (for exit codes), and the exact flag spelling or exit code
that the contract publishes and the documentation does not -- never a bare
set-difference repr.
"""

import inspect
import json
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase, REPO_ROOT
# The dispatcher-level leg (section 3) reuses test_61's probe rather than
# repeating it. See "DECISION: DISPATCHER-LEVEL EXIT CODES" above.
from tests.test_61_family_help_dispatch_exit_code import (
    DISPATCH_FAILURE_CODE_NAME,
    MIN_DISPATCHING_COMMANDS,
    PROBE_TOKEN,
    ContractProbeFixture,
)


DOCS_COMMANDS_DIR = REPO_ROOT / "DOCS" / "commands"
README_PATH = REPO_ROOT / "README.md"

# Every command name the contract publishes today, mapped to the DOCS page
# that must document it. A contract command with no entry here is a hard
# failure (see test_every_contract_family_has_a_mapped_docs_page) rather than
# a silently-skipped family, so a tenth command landing in the registry with
# no DOCS page cannot go unnoticed by omission from this dict.
FAMILY_DOC_FILE = {
    "roadmap": "roadmap.md",
    "task": "task.md",
    "sprint": "sprint.md",
    "backlog": "backlog.md",
    "audit": "audit.md",
    "stats": "stats.md",
    "graph": "graph.md",
    "web": "web.md",
    "ai-help": "ai-help.md",
}

# The three commands whose only subcommand shares the command's own name and
# is documented directly under the page's top-level sections rather than
# behind a "### <name>" heading, because a page with exactly one subcommand
# has no second subcommand to disambiguate headings from.
SELF_NAMED_SINGLE_SUBCOMMAND_FAMILIES = {"web", "stats", "ai-help"}

# See "EXEMPTION: -h/--help" above.
EXEMPT_FLAGS = frozenset({("-h", "--help")})

RUN_TIMEOUT_SECONDS = 30

# The thinnest "## Exit Codes" table on any DOCS page today (roadmap.md) carries
# five rows, and every one of the nine carries 0. Three is a deliberately slack
# floor whose only job is to catch a table parser that matched nothing, mirroring
# MIN_CODES_PER_BLOCK in test_61.
MIN_CODES_PER_DOCS_TABLE = 3

# ---------------------------------------------------------------------------
# Extraction: the contract
# ---------------------------------------------------------------------------


def load_contract(cli_path, home_dir):
    """Return the parsed `rmp --ai-help` document from the compiled binary,
    run under this module's own HOME so the contract never depends on
    whatever roadmaps the invoking user happens to have."""
    env = os.environ.copy()
    env["HOME"] = str(home_dir)
    result = subprocess.run(
        [cli_path, "--ai-help"],
        capture_output=True,
        text=True,
        env=env,
        timeout=RUN_TIMEOUT_SECONDS,
    )
    assert result.returncode == 0, (
        f"rmp --ai-help exited {result.returncode}, expected 0; "
        f"stderr={result.stderr!r}"
    )
    return json.loads(result.stdout)


# ---------------------------------------------------------------------------
# Extraction: DOCS/commands/*.md flag tables
# ---------------------------------------------------------------------------

# Matches the header row of any flag table, regardless of the columns that
# follow it (some pages carry Type/Default/Description, others fewer).
_FLAG_TABLE_HEADER = re.compile(r"^\|\s*Short Flag\s*\|\s*Long Flag\s*\|")
_TABLE_SEPARATOR_ROW = re.compile(r"^\|[-\s|]+\|\s*$")
# The first two cells of a table row. `.*?` (not `.+?`) is required: several
# pages (sprint.md) leave the short-flag cell EMPTY rather than writing `N/A`
# or `-`, and `.+?` cannot match a zero-length cell, which silently dropped
# every such row (--status, --order, --max-tasks, --force, --order-by-priority)
# out of extraction during development of this module -- a parser bug that
# would itself have made every comparison against those rows vacuously pass.
_TABLE_ROW_FIRST_TWO_CELLS = re.compile(r"^\|\s*(.*?)\s*\|\s*(.*?)\s*\|")
# The three spellings this codebase uses for "this subcommand has no short
# flag": task.md writes `N/A`, web.md writes `-`, sprint.md leaves the cell
# empty.
_NO_SHORT_FLAG_SPELLINGS = frozenset({"N/A", "-", ""})


def parse_flag_tables(text):
    """Return the set of (short, long) pairs from every '| Short Flag | Long
    Flag | ...' table found anywhere in `text` (a page can carry more than
    one such table per subcommand, e.g. task.md create's Required/Optional
    split). `short` is None when the row spells "no short flag" in any of the
    three ways this codebase uses."""
    flags = set()
    lines = text.split("\n")
    i = 0
    while i < len(lines):
        if not _FLAG_TABLE_HEADER.match(lines[i]):
            i += 1
            continue
        i += 1  # past the header row
        if i < len(lines) and _TABLE_SEPARATOR_ROW.match(lines[i]):
            i += 1  # past the '|---|---|' separator row
        while i < len(lines) and lines[i].lstrip().startswith("|"):
            m = _TABLE_ROW_FIRST_TWO_CELLS.match(lines[i])
            if m:
                short_cell = m.group(1).strip("`")
                long_cell = m.group(2).strip("`")
                short = None if short_cell in _NO_SHORT_FLAG_SPELLINGS else short_cell
                flags.add((short, long_cell))
            i += 1
    return flags


def subcommand_sections(text, subcommand_names):
    """Split a DOCS/commands/<family>.md page into {subcommand_name: section
    text}, where a section runs from a '### <name>' heading (name matching a
    real subcommand) to the next '### ' or '## ' heading. A '### <name> (...)'
    heading (task.md's "### stat (set-status)") still matches on the bare
    name, since only the first whitespace-delimited token is read. A '###'
    or '##' heading whose name is NOT a known subcommand (task.md's "### Task
    Status Values" reference section, audit.md's "### commit_hash" field
    description) closes whatever subcommand section preceded it without
    opening a new one, so reference material is never folded into a
    subcommand's flag table by accident. '#### ' sub-subsection headings
    ("#### Flags", "#### Usage") are four hashes, not three, and are ordinary
    content inside whichever subcommand section is currently open."""
    sections = {}
    current = None
    buf = []

    def flush():
        if current is not None:
            sections[current] = sections.get(current, "") + "\n".join(buf) + "\n"

    for line in text.split("\n"):
        heading = re.match(r"^###\s+(\S+)", line)
        if heading and heading.group(1) in subcommand_names:
            flush()
            current = heading.group(1)
            buf = []
            continue
        if re.match(r"^###\s+", line) or re.match(r"^##\s+", line):
            flush()
            current = None
            buf = []
            continue
        if current is not None:
            buf.append(line)
    flush()
    return sections


def doc_flags_by_subcommand(family, subcommand_names):
    """Return {subcommand_name: set of (short, long)} documented on
    DOCS/commands/<family's page>, and {subcommand_name: bool} saying whether
    a distinct section was located for it (False for a multi-subcommand
    family means the page carries no '### <name>' heading for a real
    subcommand at all, which is reported as a problem by
    test_every_contract_flag_is_documented_per_subcommand below)."""
    doc_file = DOCS_COMMANDS_DIR / FAMILY_DOC_FILE[family]
    text = doc_file.read_text(encoding="utf-8")
    if family in SELF_NAMED_SINGLE_SUBCOMMAND_FAMILIES:
        # Exactly one subcommand, sharing the command's name; its flags are
        # documented directly under the page's own top-level sections.
        (only_name,) = subcommand_names
        return {only_name: parse_flag_tables(text)}, {only_name: True}
    sections = subcommand_sections(text, set(subcommand_names))
    flags = {name: parse_flag_tables(sections.get(name, "")) for name in subcommand_names}
    found = {name: (name in sections) for name in subcommand_names}
    return flags, found


# ---------------------------------------------------------------------------
# Extraction: "## Exit Codes" tables (DOCS/commands/*.md and README.md)
# ---------------------------------------------------------------------------

_EXIT_CODE_TABLE_HEADER = re.compile(r"^\|\s*(?:Exit )?Code\s*\|")
_EXIT_CODE_CELL = re.compile(r"^\|\s*`?(\d+)`?\s*\|")


def parse_exit_code_table(text, doc_label):
    """Return the set of integer codes listed in the single '## Exit Codes'
    table below the first '## Exit Codes' heading in `text`. Fatal (not an
    empty result) when the heading or the table header row cannot be found,
    for the same reason internal/models/docs_enum_coverage_test.go's region
    scan is fatal: a comparison run over a region this parser could not
    locate is not evidence the documentation is correct, only that this
    function stopped looking at it."""
    marker = "## Exit Codes"
    idx = text.find("\n" + marker)
    assert idx != -1, (
        f"{doc_label} carries no {marker!r} heading (searched for a line "
        f"beginning with it); the exit-code table cannot be located"
    )
    region_lines = text[idx:].split("\n")[1:]

    header_at = None
    for offset, line in enumerate(region_lines):
        if _EXIT_CODE_TABLE_HEADER.match(line):
            header_at = offset
            break
    assert header_at is not None, (
        f"{doc_label} has a {marker!r} heading but no table beginning "
        f"'| Code |' or '| Exit Code |' directly below it; the exit-code "
        f"table cannot be located"
    )

    codes = set()
    i = header_at + 1
    if i < len(region_lines) and _TABLE_SEPARATOR_ROW.match(region_lines[i]):
        i += 1
    while i < len(region_lines) and region_lines[i].lstrip().startswith("|"):
        m = _EXIT_CODE_CELL.match(region_lines[i])
        if m:
            codes.add(int(m.group(1)))
        i += 1
    return codes


def contract_exit_code_union(command):
    """The union of every subcommand's exit_codes in one contract command
    object -- the quantity a family's single DOCS Exit Codes table is
    compared against (see module docstring, "WHAT IS COMPARED")."""
    union = set()
    for sub in command["subcommands"]:
        union.update(sub.get("exit_codes", []))
    return union


# ---------------------------------------------------------------------------
# Shared fixture
# ---------------------------------------------------------------------------


class _ContractFixture:
    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.contract = load_contract(self.test.cli_path, self.test.home_dir)

    def teardown_method(self):
        self.test.teardown()


# ---------------------------------------------------------------------------
# 1. Flags: every contract subcommand flag (short and long), minus the
#    -h/--help exemption, must be documented on its DOCS page.
# ---------------------------------------------------------------------------


class TestContractFlagsAreFullyDocumented(_ContractFixture):

    def test_every_family_is_mapped_to_a_docs_page(self):
        contract_families = {cmd["name"] for cmd in self.contract["commands"]}
        unmapped = contract_families - set(FAMILY_DOC_FILE)
        assert not unmapped, (
            f"the contract publishes {sorted(unmapped)} but FAMILY_DOC_FILE in "
            f"{os.path.basename(__file__)} maps no DOCS page to it, so this "
            f"module cannot check it at all -- add an entry"
        )

    def test_every_contract_flag_is_documented_per_subcommand(self):
        problems = []
        pairs_checked = 0
        flags_checked = 0

        for cmd in self.contract["commands"]:
            family = cmd["name"]
            names = [s["name"] for s in cmd["subcommands"]]
            docs, found = doc_flags_by_subcommand(family, names)
            doc_file = FAMILY_DOC_FILE[family]

            for sub in cmd["subcommands"]:
                sub_name = sub["name"]
                pairs_checked += 1
                contract_flags = {(f["short"], f["long"]) for f in sub["flags"]}
                contract_flags -= EXEMPT_FLAGS

                if contract_flags and not found.get(sub_name, False):
                    # sorted() over (short, long) pairs cannot compare a missing
                    # short form against a present one -- the contract emits
                    # `short: null`, and None is not orderable against a string.
                    # This branch is the only place the pairs are sorted, so it
                    # crashed with a TypeError instead of reporting the missing
                    # section the moment a subcommand with a short-form-less flag
                    # went undocumented. Sorting on the long name alone is what
                    # the message reads by anyway.
                    listed = sorted(contract_flags, key=lambda pair: pair[1])
                    problems.append(
                        f"DOCS/commands/{doc_file}: no '### {sub_name}' section found, "
                        f"but the contract publishes {listed} for "
                        f"`rmp {family} {sub_name}`"
                    )
                    continue

                documented = docs.get(sub_name, set())
                flags_checked += len(contract_flags)
                missing = contract_flags - documented
                for short, long_flag in sorted(missing, key=lambda p: p[1]):
                    short_desc = short if short else "(no short form)"
                    problems.append(
                        f"DOCS/commands/{doc_file}: `rmp {family} {sub_name}` accepts "
                        f"{short_desc}/{long_flag} per the contract, but the "
                        f"'### {sub_name}' section documents no such flag"
                    )

        # Non-vacuity floors: measured against the current tree at 59
        # (family, subcommand) pairs and 123 flags checked post-exemption.
        # A parser regression that started matching nothing would collapse
        # both to 0 and pass every comparison above vacuously.
        assert pairs_checked >= 50, (
            f"only {pairs_checked} (family, subcommand) pairs were checked; "
            f"expected at least 50 -- the contract extraction likely broke"
        )
        assert flags_checked >= 100, (
            f"only {flags_checked} contract flags were checked (post -h/--help "
            f"exemption) across {pairs_checked} pairs; expected at least 100 "
            f"-- the DOCS flag-table extraction likely broke"
        )

        assert not problems, (
            f"{len(problems)} contract flag(s) are undocumented:\n  "
            + "\n  ".join(problems)
        )


# ---------------------------------------------------------------------------
# 2. Exit codes: every code any subcommand in a family can emit must appear
#    in that family's DOCS/commands/<family>.md "## Exit Codes" table.
# ---------------------------------------------------------------------------


class TestContractExitCodesAreFullyDocumentedPerFamily(_ContractFixture):

    def test_every_family_exit_code_union_is_documented(self):
        problems = []
        codes_checked = 0

        for cmd in self.contract["commands"]:
            family = cmd["name"]
            doc_file = FAMILY_DOC_FILE[family]
            text = (DOCS_COMMANDS_DIR / doc_file).read_text(encoding="utf-8")
            documented = parse_exit_code_table(text, f"DOCS/commands/{doc_file}")

            union = contract_exit_code_union(cmd)
            codes_checked += len(union)
            missing = union - documented
            for code in sorted(missing):
                problems.append(
                    f"DOCS/commands/{doc_file}: the `{family}` family's Exit "
                    f"Codes table omits {code}, which at least one `rmp "
                    f"{family}` subcommand can emit per the contract "
                    f"(union: {sorted(union)})"
                )

        # Measured at 38 across the 9 families today (roadmap 4, task 6,
        # sprint 7, backlog 3, audit 3, stats 3, graph 6, web 4, ai-help 2).
        assert codes_checked >= 30, (
            f"only {codes_checked} per-family exit codes were checked across "
            f"{len(self.contract['commands'])} families; expected at least 30 "
            f"-- the contract extraction likely broke"
        )

        assert not problems, (
            f"{len(problems)} exit code(s) are undocumented at family level:\n  "
            + "\n  ".join(problems)
        )


# ---------------------------------------------------------------------------
# 3. Dispatcher-level exit codes: a family OBSERVED to emit the dispatch-failure
#    code for an unresolved subcommand must carry that code in its DOCS table.
#    See "DECISION: DISPATCHER-LEVEL EXIT CODES" in the module docstring for
#    why this leg exists and why its oracle is the binary, not the contract.
# ---------------------------------------------------------------------------


class TestDispatcherExitCodeIsDocumentedPerFamily(ContractProbeFixture):
    """The set of families checked here is DERIVED, never transcribed: it is
    whatever `ContractProbeFixture.dispatching_commands()` observes when it
    probes every command the contract publishes with an unresolved token. A
    family that stops dispatching drops out of the requirement on its own, and
    a family that starts dispatching is picked up the day it does."""

    def test_every_dispatching_family_documents_the_dispatch_code(self):
        expected = self.dispatch_failure_code()
        # Observed by running the binary. A leaf command (`stats`, `web`,
        # `ai-help`) refuses the excess token as misuse instead of failing to
        # dispatch, so it never appears here and nothing is required of its
        # page -- an exclusion by observation, not by an exemption list.
        dispatching = list(self.dispatching_commands())
        problems = []
        tables_read = 0

        for family in dispatching:
            assert family in FAMILY_DOC_FILE, (
                f"`rmp {family} {PROBE_TOKEN}` exits {expected} "
                f"({DISPATCH_FAILURE_CODE_NAME}) but FAMILY_DOC_FILE in "
                f"{os.path.basename(__file__)} maps no DOCS page to "
                f"{family!r}, so its table cannot be checked -- add an entry"
            )
            doc_file = FAMILY_DOC_FILE[family]
            text = (DOCS_COMMANDS_DIR / doc_file).read_text(encoding="utf-8")
            documented = parse_exit_code_table(text, f"DOCS/commands/{doc_file}")
            tables_read += 1

            assert len(documented) >= MIN_CODES_PER_DOCS_TABLE, (
                f"DOCS/commands/{doc_file}: only {len(documented)} code(s) "
                f"parsed from the '## Exit Codes' table ({sorted(documented)}); "
                f"expected at least {MIN_CODES_PER_DOCS_TABLE} -- the table "
                f"parser is broken, which would make this comparison vacuous"
            )
            assert 0 in documented, (
                f"DOCS/commands/{doc_file}: the parsed '## Exit Codes' table "
                f"{sorted(documented)} omits 0, which every command page "
                f"documents -- the parser is reading the wrong table"
            )

            if expected not in documented:
                problems.append(
                    f"DOCS/commands/{doc_file}: the `{family}` family's Exit "
                    f"Codes table lists {sorted(documented)} and omits "
                    f"{expected} ({DISPATCH_FAILURE_CODE_NAME}), yet "
                    f"`rmp {family} {PROBE_TOKEN}` exits {expected}"
                )

        assert len(dispatching) >= MIN_DISPATCHING_COMMANDS, (
            f"only {len(dispatching)} command(s) ({dispatching}) were observed "
            f"exiting {expected} for an unresolved subcommand; expected at "
            f"least {MIN_DISPATCHING_COMMANDS} -- either dispatch stopped "
            f"producing {DISPATCH_FAILURE_CODE_NAME} or the probe stopped "
            f"reaching the dispatcher, and this check has gone vacuous"
        )
        assert tables_read == len(dispatching), (
            f"read {tables_read} '## Exit Codes' table(s) for "
            f"{len(dispatching)} dispatching command(s)"
        )
        assert not problems, (
            f"{len(problems)} DOCS command page(s) omit the exit code their "
            f"family actually emits for an unresolved subcommand:\n  "
            + "\n  ".join(problems)
        )


# ---------------------------------------------------------------------------
# 4. README.md's application-wide Exit Codes table vs. the contract's
#    top-level `exit_codes` catalogue.
# ---------------------------------------------------------------------------


class TestReadmeExitCodeTableMatchesContractCatalogue(_ContractFixture):

    def test_readme_documents_every_catalogue_code(self):
        text = README_PATH.read_text(encoding="utf-8")
        documented = parse_exit_code_table(text, "README.md")

        catalogue = {entry["code"] for entry in self.contract["exit_codes"]}
        assert len(catalogue) >= 8, (
            f"only {len(catalogue)} codes were read from the contract's "
            f"exit_codes catalogue; expected at least 8 -- the contract "
            f"extraction likely broke"
        )

        missing = catalogue - documented
        named = {entry["code"]: entry["name"] for entry in self.contract["exit_codes"]}
        problems = [
            f"README.md: the Exit Codes table omits {code} ({named[code]}), "
            f"which the contract's exit_codes catalogue publishes"
            for code in sorted(missing)
        ]
        assert not problems, (
            f"{len(problems)} exit code(s) from the contract catalogue are "
            f"missing from README.md's Exit Codes table:\n  "
            + "\n  ".join(problems)
        )


# ---------------------------------------------------------------------------
# Runner: classes are DISCOVERED by inspecting this module, never listed, so
# a class added later cannot go unrun the way rmp task #303 describes.
# ---------------------------------------------------------------------------


def _run_all():
    passed = 0
    failed = 0
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
    print(f"DOCS/README contract-completeness tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
