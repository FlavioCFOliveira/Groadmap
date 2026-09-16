#!/usr/bin/env python3
"""
Test 73: every subcommand help's `Exit codes:` block is its contract entry, code
for code, condition for condition, and line for line (rmp tasks #466 and #487;
SPEC/HELP.md § Agreement with the contract, the gate).

## The defects

The AI Agent Contract publishes, per subcommand, the exit codes it can emit and
the conditions that produce each. The plain-text help of the same subcommand had
been written by hand and drifted twice over. First the codes: 29 of the 55 helps
omitted a code their contract entry declares -- `task list` listed 0, 3 and 6
while the contract declares 0, 2, 3, 4 and 6 (task #466). Then the conditions:
with the codes aligned, 267 entries still described a code in words the contract
contradicted -- `sprint move-tasks` gave code 6 for a CLOSED destination while
the contract gave it for a task outside the source sprint (task #487). The block
is now rendered from the registry, and this gate holds every help to it.

## The gate

It reads both surfaces from the compiled binary, never from the Go printers,
because a help token can be served by a path other than the printer the
registry names:

  1. the contract, from `rmp --ai-help`: its subcommands and each `exit_codes`;
  2. the help of every subcommand other than `ai-help`, from
     `rmp <command> <subcommand> --help`, or `rmp <command> --help` for `stats`
     and `web`, which take no subcommand;
  3. a failure, naming the subcommand and the codes, on a code the help lists
     that the contract does not declare, on a code the contract declares that the
     help does not list, and on a code the help lists twice;
  4. a failure on a help with no `Exit codes:` block or more than one;
  5. `rmp ai-help --help` writes exactly what `rmp --ai-help` writes, so the one
     subcommand not compared is excluded by a checked fact;
  6. the number compared equals the number the contract publishes less one;
  7. the code comparator rejects a real block with one entry removed and with one
     entry added, and accepts it unaltered;
  8. for every code a block lists, the text of the help's entry equals the text
     of the contract's entry -- its conditions joined with single spaces, both
     normalised -- and the one entry that may carry more, the exit-code-1 entry of
     `graph client`, begins with the contract's text followed by a space and at
     least one further character, its remedies;
  9. the condition comparator rejects a real entry with a condition removed, with
     one word changed, with a condition of another code appended, and with text
     appended to an entry other than the `graph client` exit-code-1 entry, and
     accepts the entry unaltered;
 10. every block is the block rendered from the contract, line for line; the
     `graph client` exit-code-1 entry is compared to the end of its conditions,
     and every further line of it must begin with eight spaces, end in a
     character other than a space, and be at most 80 characters long;
 11. the shape comparator rejects a real block with two lines of one condition
     joined, with the second condition of a code moved onto the line that ends the
     first, and with a space appended to a line, and accepts it unaltered.

A block begins at the line `Exit codes:` and ends at the first empty line after
it. A line indented by exactly two spaces whose first field is a decimal number
followed by at least two spaces opens an entry; every other line is indented by
at least three spaces and continues the entry above. A line in neither shape
fails the gate rather than being skipped.

The rendered shape (SPEC/HELP.md, "How the block is rendered"): an entry's first
line is two spaces, the code, and spaces to column eight, then the start of its
first condition; each further condition begins on a line of its own; every line
after an entry's first begins with eight spaces; a condition's words are placed
greedily within 80 code points, a word longer than the 72 after the indent alone
on its line, and no line ends in a space.
"""

import inspect
import json
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


BLOCK_HEADER = "Exit codes:"
ENTRY_LINE = re.compile(r"^  (\d+) {2,}")
CONTINUATION_LINE = re.compile(r"^ {3,}")
AI_HELP = "ai-help"

# The indent of an entry's every line after its first, and the width of the
# column its code occupies on its first line.
PREFIX_WIDTH = 8
# The longest line the rendered shape writes, in code points.
LINE_WIDTH = 80
# The one entry whose help text may continue past the contract's conditions.
REMEDY_ENTRY = ("graph client", 1)


class GateError(AssertionError):
    """A help the gate cannot read."""


def read_block_lines(help_text):
    """The lines of the help's one `Exit codes:` block, header included, up to
    the first empty line. Raises GateError on a missing or repeated block."""
    lines = help_text.split("\n")
    headers = [i for i, line in enumerate(lines) if line == BLOCK_HEADER]
    if len(headers) != 1:
        raise GateError(f"the help carries {len(headers)} `{BLOCK_HEADER}` blocks, want exactly one")
    block = []
    for line in lines[headers[0]:]:
        if line == "":
            break
        block.append(line)
    return block


def read_entries(help_text):
    """The entries of a help's block, in order, as (code, lines): each entry's
    opening line followed by its continuation lines. Raises GateError on a block
    the gate cannot read."""
    entries = []
    for line in read_block_lines(help_text)[1:]:
        entry = ENTRY_LINE.match(line)
        if entry:
            entries.append((int(entry.group(1)), [line]))
        elif CONTINUATION_LINE.match(line):
            if not entries:
                raise GateError(f"the block opens with a continuation line: {line!r}")
            entries[-1][1].append(line)
        else:
            raise GateError(f"a line of the block is in neither shape: {line!r}")
    return entries


def read_exit_codes(help_text):
    """The codes a help's `Exit codes:` block lists, in the order listed,
    repeats kept. Raises GateError on a missing, repeated or unreadable block."""
    return [code for code, _lines in read_entries(help_text)]


def compare_codes(label, listed, declared):
    """The problems of one subcommand: extra, missing and repeated codes."""
    problems = []
    listed_set, declared_set = set(listed), set(declared)
    extra = sorted(listed_set - declared_set)
    missing = sorted(declared_set - listed_set)
    repeated = sorted({code for code in listed if listed.count(code) > 1})
    if extra:
        problems.append(f"{label}: the help lists {extra}, which the contract does not declare")
    if missing:
        problems.append(f"{label}: the contract declares {missing}, which the help does not list")
    if repeated:
        problems.append(f"{label}: the help lists {repeated} more than once")
    return problems


def normalise(text):
    """Every run of whitespace a single space, none at either end."""
    return " ".join(text.split())


def help_entry_text(lines):
    """The text of a help entry: its opening line after the code and the spaces
    that follow it, then every continuation line, joined with single spaces and
    normalised (SPEC/HELP.md, "What agreement of conditions means", rules 1 and
    3)."""
    return normalise(" ".join([ENTRY_LINE.sub("", lines[0], count=1)] + lines[1:]))


def contract_entry_text(conditions):
    """The text of a contract entry: its conditions in array order, joined with
    single spaces and normalised (rules 2 and 3)."""
    return normalise(" ".join(conditions))


def compare_conditions(label, code, help_lines, conditions, remedies_follow):
    """The problem, if any, of one entry's agreement with the contract (rule 4).
    `remedies_follow` admits text after the conditions, as the paragraph on
    remedies does for the `graph client` exit-code-1 entry and for no other."""
    got, want = help_entry_text(help_lines), contract_entry_text(conditions)
    if remedies_follow:
        agrees = got.startswith(want + " ") and len(got) > len(want) + 1
    else:
        agrees = got == want
    if agrees:
        return []
    shape = "conditions followed by remedies" if remedies_follow else "conditions"
    return [f"{label}: exit {code}: the help's entry is not the contract's {shape}\n"
            f"    help:     {got}\n    contract: {want}"]


def layout(prefix, text):
    """One condition laid out by words: greedy lines within LINE_WIDTH code
    points, the first opening with `prefix` and every other with the indent, a
    word too long for the space after the indent alone on its line."""
    lines, current, placed = [], prefix, 0
    for word in text.split():
        if placed and len(current) + 1 + len(word) > LINE_WIDTH:
            lines.append(current)
            current, placed = " " * PREFIX_WIDTH, 0
        current = current + (" " if placed else "") + word
        placed += 1
    lines.append(current if placed else prefix.rstrip())
    return lines


def render_entry(code, conditions):
    """The lines of one entry, rendered from its code and conditions."""
    lines = []
    for index, condition in enumerate(conditions):
        prefix = ("  " + str(code)).ljust(PREFIX_WIDTH) if index == 0 else " " * PREFIX_WIDTH
        lines += layout(prefix, condition)
    return lines


def render_spans(exit_codes):
    """(code, condition index, first block line, line count) for every condition
    of the rendered block, block line numbers counting the header as 0."""
    spans, at = [], 1
    for entry in exit_codes:
        for index, condition in enumerate(entry["conditions"]):
            prefix = ("  " + str(entry["code"])).ljust(PREFIX_WIDTH) if index == 0 else " " * PREFIX_WIDTH
            count = len(layout(prefix, condition))
            spans.append((entry["code"], index, at, count))
            at += count
    return spans


def compare_shape(label, help_text, exit_codes, remedy_code=None):
    """The problem, if any, of a help's block against the block rendered from
    the contract, line for line. After the conditions of `remedy_code`'s entry
    the help may carry further lines, each beginning with eight spaces, ending in
    a character other than a space, and at most LINE_WIDTH long."""
    block = read_block_lines(help_text)

    def differ(index, want):
        got = block[index] if index < len(block) else "<the block has ended>"
        return [f"{label}: line {index + 1} of the block is not the rendered line\n"
                f"    help:     {got!r}\n    rendered: {want!r}"]

    if block[0] != BLOCK_HEADER:
        return differ(0, BLOCK_HEADER)
    at = 1
    for entry in exit_codes:
        for want in render_entry(entry["code"], entry["conditions"]):
            if at >= len(block) or block[at] != want:
                return differ(at, want)
            at += 1
        if entry["code"] != remedy_code:
            continue
        while at < len(block) and not ENTRY_LINE.match(block[at]):
            line = block[at]
            if not line.startswith(" " * PREFIX_WIDTH):
                return [f"{label}: line {at + 1}, after the exit-{remedy_code} conditions, does "
                        f"not begin with {PREFIX_WIDTH} spaces: {line!r}"]
            if line.endswith(" "):
                return [f"{label}: line {at + 1}, after the exit-{remedy_code} conditions, ends in "
                        f"a space: {line!r}"]
            if len(line) > LINE_WIDTH:
                return [f"{label}: line {at + 1}, after the exit-{remedy_code} conditions, is "
                        f"{len(line)} characters long, over {LINE_WIDTH}: {line!r}"]
            at += 1
    if at < len(block):
        return [f"{label}: line {at + 1} of the block follows the end of the rendered block: "
                f"{block[at]!r}"]
    return []


def help_invocation(command, subcommand):
    if subcommand in ("", command):
        return [command, "--help"]
    return [command, subcommand, "--help"]


def run(cli_path, args, env):
    return subprocess.run([cli_path] + args, capture_output=True, text=True, env=env, timeout=30)


_GATHERED = {}


def gather(cli_path, env):
    """The contract, and every compared subcommand's help, read once per binary:
    (contract, published, [(label, subcommand entry, help result)])."""
    if cli_path in _GATHERED:
        return _GATHERED[cli_path]
    contract_run = run(cli_path, ["--ai-help"], env)
    assert contract_run.returncode == 0, f"rmp --ai-help exited {contract_run.returncode}"
    contract = json.loads(contract_run.stdout)
    published, helps = 0, []
    for command in contract["commands"]:
        for sub in command["subcommands"]:
            published += 1
            if command["name"] == AI_HELP:
                continue
            label = f"{command['name']} {sub['name']}"
            if sub["name"] in ("", command["name"]):
                label = command["name"]
            helps.append((label, sub, run(cli_path, help_invocation(command["name"], sub["name"]), env)))
    _GATHERED[cli_path] = (contract, published, helps)
    return _GATHERED[cli_path]


def collect(cli_path, env=None):
    """Compare every subcommand help's codes with its contract entry.

    Returns (compared, published, problems): how many helps were compared, how
    many subcommands the contract publishes, and every problem found.
    """
    env = dict(os.environ if env is None else env)
    _contract, published, helps = gather(cli_path, env)
    compared = 0
    problems = []
    for label, sub, result in helps:
        if result.returncode != 0:
            problems.append(f"{label}: the help invocation exited {result.returncode}")
            continue
        try:
            listed = read_exit_codes(result.stdout)
        except GateError as exc:
            problems.append(f"{label}: {exc}")
            continue
        compared += 1
        problems += compare_codes(label, listed, [entry["code"] for entry in sub["exit_codes"]])
    return compared, published, problems


class TestHelpExitCodesAgreeWithTheContract:

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.env = os.environ.copy()
        self.env["HOME"] = str(self.test.home_dir)

    def teardown_method(self):
        self.test.teardown()

    def _help_and_entry(self, command, subcommand):
        result = run(self.test.cli_path, [command, subcommand, "--help"], self.env)
        assert result.returncode == 0, f"rmp {command} {subcommand} --help exited {result.returncode}"
        contract = json.loads(run(self.test.cli_path, ["--ai-help"], self.env).stdout)
        sub = next(
            s for c in contract["commands"] if c["name"] == command
            for s in c["subcommands"] if s["name"] == subcommand
        )
        return result.stdout, sub

    def test_every_subcommand_help_lists_exactly_its_contract_codes(self):
        compared, published, problems = collect(self.test.cli_path, self.env)
        assert not problems, "\n".join(problems)
        assert compared == published - 1, (
            f"compared {compared} helps, want {published - 1}: every subcommand the contract "
            f"publishes ({published}) other than ai-help")
        # The contract publishes 56 subcommands today; a floor keeps an extraction
        # that found almost nothing from passing.
        assert compared >= 55, f"only {compared} helps were compared"

    def test_ai_help_help_is_the_contract_itself(self):
        contract = run(self.test.cli_path, ["--ai-help"], self.env)
        served = run(self.test.cli_path, [AI_HELP, "--help"], self.env)
        assert contract.returncode == 0 and served.returncode == 0, (
            f"exit codes: --ai-help={contract.returncode} ai-help --help={served.returncode}")
        assert served.stdout == contract.stdout, (
            "rmp ai-help --help does not write the contract, so ai-help has a plain-text help "
            "the gate would have to compare")

    def test_the_comparator_can_fail(self):
        result = run(self.test.cli_path, ["task", "get", "--help"], self.env)
        block_help = result.stdout
        contract = json.loads(run(self.test.cli_path, ["--ai-help"], self.env).stdout)
        declared = next(
            [entry["code"] for entry in sub["exit_codes"]]
            for command in contract["commands"] if command["name"] == "task"
            for sub in command["subcommands"] if sub["name"] == "get"
        )

        listed = read_exit_codes(block_help)
        assert compare_codes("task get", listed, declared) == [], "the unaltered block must be accepted"

        lines = block_help.split("\n")
        header = lines.index(BLOCK_HEADER)
        entry_rows = [i for i in range(header + 1, len(lines))
                      if ENTRY_LINE.match(lines[i]) and int(ENTRY_LINE.match(lines[i]).group(1)) != 0]
        assert entry_rows, "the real block has no entry other than 0 to remove"

        # One entry removed, with its continuation lines.
        start = entry_rows[0]
        end = start + 1
        while end < len(lines) and CONTINUATION_LINE.match(lines[end]):
            end += 1
        removed = "\n".join(lines[:start] + lines[end:])
        problems = compare_codes("task get", read_exit_codes(removed), declared)
        assert any("does not list" in p for p in problems), f"a removed entry was accepted: {problems}"

        # One entry added: a code the contract does not declare.
        invented = next(code for code in (5, 127, 130) if code not in declared)
        added = "\n".join(lines[:header + 1] + [f"  {invented}  Invented for the gate's own proof"]
                          + lines[header + 1:])
        problems = compare_codes("task get", read_exit_codes(added), declared)
        assert any("does not declare" in p for p in problems), f"an added entry was accepted: {problems}"

        # One entry listed twice.
        repeated = "\n".join(lines[:start + 1] + [lines[start]] + lines[start + 1:])
        problems = compare_codes("task get", read_exit_codes(repeated), declared)
        assert any("more than once" in p for p in problems), f"a repeated entry was accepted: {problems}"

        # A block the gate cannot read, and a help with no block or two.
        for broken, why in (
            ("\n".join(lines[:header + 1] + [" 9 misaligned"] + lines[header + 1:]), "a misaligned line"),
            (block_help.replace(BLOCK_HEADER, "Exit statuses:"), "no block"),
            (block_help + "\n" + BLOCK_HEADER + "\n  0  Success\n", "two blocks"),
        ):
            try:
                read_exit_codes(broken)
            except GateError:
                continue
            raise AssertionError(f"the reader accepted {why}")

    # -- items 8 and 9: the conditions ------------------------------------------

    def test_every_help_entry_agrees_with_its_contract_conditions(self):
        _contract, published, helps = gather(self.test.cli_path, self.env)
        compared = entries_compared = 0
        remedy_entry_seen = False
        problems = []
        for label, sub, result in helps:
            try:
                entries = read_entries(result.stdout)
            except GateError as exc:
                problems.append(f"{label}: {exc}")
                continue
            declared = {entry["code"]: entry["conditions"] for entry in sub["exit_codes"]}
            for code, lines in entries:
                if code not in declared:
                    continue  # a code the contract does not declare is item 3's failure
                remedies_follow = (label, code) == REMEDY_ENTRY
                remedy_entry_seen = remedy_entry_seen or remedies_follow
                problems += compare_conditions(label, code, lines, declared[code], remedies_follow)
                entries_compared += 1
            compared += 1
        assert not problems, f"{len(problems)} entr(y/ies) disagree:\n" + "\n".join(problems)
        assert compared == published - 1, (
            f"compared the conditions of {compared} helps, want {published - 1}")
        assert remedy_entry_seen, (
            f"no help carried the {REMEDY_ENTRY} entry, so the remedies rule was never applied")
        assert entries_compared >= 250, f"only {entries_compared} entries were compared"

    def test_the_condition_comparison_can_fail(self):
        help_text, sub = self._help_and_entry("task", "get")
        declared = {entry["code"]: entry["conditions"] for entry in sub["exit_codes"]}
        code, lines = next((c, l) for c, l in read_entries(help_text) if len(declared.get(c, ())) >= 2)
        conditions = declared[code]

        assert compare_conditions("task get", code, lines, conditions, False) == [], (
            "the unaltered entry must be accepted")

        mutants = {
            "a condition removed": render_entry(code, conditions[1:]),
            "one word of a condition changed": (
                [lines[0][:PREFIX_WIDTH] + "Invented" + lines[0][PREFIX_WIDTH + len(conditions[0].split()[0]):]]
                + lines[1:]),
            "a condition of another code appended": (
                lines + layout(" " * PREFIX_WIDTH, next(v for c, v in declared.items() if c != code)[0])),
            "text appended after the conditions": lines + [" " * PREFIX_WIDTH + "Then run the command again."],
        }
        for why, mutant in mutants.items():
            assert mutant != lines, f"the mutant for {why} did not change the entry"
            assert compare_conditions("task get", code, mutant, conditions, False), (
                f"an entry with {why} was accepted")

        # The addition refused above is exactly what the remedies rule admits, and
        # it admits it only where remedies follow: the refusal is the rule, not a
        # comparator that rejects any appended text.
        appended = mutants["text appended after the conditions"]
        assert compare_conditions("task get", code, appended, conditions, True) == [], (
            "text after the conditions must be accepted where remedies follow")
        assert compare_conditions("task get", code, lines, conditions, True), (
            "an entry with no remedy must be refused where remedies are required to follow")

    # -- items 10 and 11: the shape ---------------------------------------------

    def test_every_help_block_is_the_rendered_block_line_for_line(self):
        _contract, published, helps = gather(self.test.cli_path, self.env)
        compared = 0
        problems = []
        for label, sub, result in helps:
            remedy_code = REMEDY_ENTRY[1] if label == REMEDY_ENTRY[0] else None
            try:
                problems += compare_shape(label, result.stdout, sub["exit_codes"], remedy_code)
            except GateError as exc:
                problems.append(f"{label}: {exc}")
                continue
            compared += 1
        assert not problems, f"{len(problems)} block(s) are not rendered:\n" + "\n".join(problems)
        assert compared == published - 1, f"compared the shape of {compared} helps, want {published - 1}"

    def test_the_shape_comparison_can_fail(self):
        help_text, sub = self._help_and_entry("task", "get")
        exit_codes = sub["exit_codes"]
        block = read_block_lines(help_text)
        assert compare_shape("task get", help_text, exit_codes) == [], "the unaltered block must be accepted"

        spans = render_spans(exit_codes)
        wrapped = next(s for s in spans if s[3] >= 2)
        code_with_two = next(c for c, i, _a, _n in spans if i == 1)
        first = next(s for s in spans if s[0] == code_with_two and s[1] == 0)
        second = next(s for s in spans if s[0] == code_with_two and s[1] == 1)
        assert second[2] == first[2] + first[3]

        def joined(lines, at):
            return lines[:at] + [lines[at] + " " + lines[at + 1].strip()] + lines[at + 2:]

        mutants = {
            "two lines of one condition joined": joined(block, wrapped[2]),
            "the second condition moved onto the line that ends the first": joined(block, second[2] - 1),
            "a space appended to a line": block[:1] + [block[1] + " "] + block[2:],
        }
        original = "\n".join(block)
        for why, mutant in mutants.items():
            mutated = help_text.replace(original, "\n".join(mutant), 1)
            assert mutated != help_text, f"the mutant for {why} did not change the help"
            assert compare_shape("task get", mutated, exit_codes), f"a block with {why} was accepted"
        # Two of the three keep every word in place, so they still AGREE: the shape
        # is checked apart from agreement, as rule 10 requires.
        for why in ("two lines of one condition joined",
                    "the second condition moved onto the line that ends the first"):
            mutated = help_text.replace(original, "\n".join(mutants[why]), 1)
            for c, lines in read_entries(mutated):
                assert compare_conditions("task get", c, lines, next(
                    e["conditions"] for e in exit_codes if e["code"] == c), False) == [], (
                    f"a block with {why} should still agree, and did not")


def _run_all():
    passed = failed = 0
    failures = []
    classes = [obj for _n, obj in sorted(inspect.getmembers(sys.modules[__name__], inspect.isclass))
               if obj.__module__ == __name__ and _n.startswith("Test")]
    print(f"Discovered {len(classes)} test classes: "
          f"{', '.join(c.__name__ for c in classes)}")
    for cls in classes:
        for m in sorted(n for n in dir(cls) if n.startswith("test_")):
            label = f"{cls.__name__}.{m}"
            inst = cls()
            inst.setup_method()
            try:
                getattr(inst, m)()
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
                inst.teardown_method()
    print("\n" + "=" * 60)
    print(f"Help exit codes contract tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if _run_all() else 1)
