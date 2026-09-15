#!/usr/bin/env python3
"""
Test 73: every subcommand help lists exactly the exit codes of its contract
entry (rmp task #466; SPEC/HELP.md § Agreement with the contract, the gate).

## The defect

The AI Agent Contract publishes, per subcommand, the exit codes it can emit. The
plain-text help of the same subcommand had been written by hand and drifted: 29
of the 55 helps omitted a code their contract entry declares -- `task list`
listed 0, 3 and 6 while the contract declares 0, 2, 3, 4 and 6 -- so a reader of
the help could not know that an unknown flag exits 2 or that a missing roadmap
exits 4.

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
  7. the comparator rejects a real block with one entry removed and with one
     entry added, and accepts it unaltered.

A block begins at the line `Exit codes:` and ends at the first empty line after
it. A line indented by exactly two spaces whose first field is a decimal number
followed by at least two spaces opens an entry; every other line is indented by
at least three spaces and continues the entry above. A line in neither shape
fails the gate rather than being skipped.
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


class GateError(AssertionError):
    """A help the gate cannot read."""


def read_exit_codes(help_text):
    """The codes a help's `Exit codes:` block lists, in the order listed,
    repeats kept. Raises GateError on a missing, repeated or unreadable block."""
    lines = help_text.split("\n")
    headers = [i for i, line in enumerate(lines) if line == BLOCK_HEADER]
    if len(headers) != 1:
        raise GateError(f"the help carries {len(headers)} `{BLOCK_HEADER}` blocks, want exactly one")
    codes = []
    for line in lines[headers[0] + 1:]:
        if line == "":
            break
        entry = ENTRY_LINE.match(line)
        if entry:
            codes.append(int(entry.group(1)))
        elif CONTINUATION_LINE.match(line):
            if not codes:
                raise GateError(f"the block opens with a continuation line: {line!r}")
        else:
            raise GateError(f"a line of the block is in neither shape: {line!r}")
    return codes


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


def help_invocation(command, subcommand):
    if subcommand in ("", command):
        return [command, "--help"]
    return [command, subcommand, "--help"]


def run(cli_path, args, env):
    return subprocess.run([cli_path] + args, capture_output=True, text=True, env=env, timeout=30)


def collect(cli_path, env=None):
    """Compare every subcommand help with its contract entry.

    Returns (compared, published, problems): how many helps were compared, how
    many subcommands the contract publishes, and every problem found.
    """
    env = dict(os.environ if env is None else env)
    contract_run = run(cli_path, ["--ai-help"], env)
    assert contract_run.returncode == 0, f"rmp --ai-help exited {contract_run.returncode}"
    contract = json.loads(contract_run.stdout)

    compared = published = 0
    problems = []
    for command in contract["commands"]:
        for sub in command["subcommands"]:
            published += 1
            if command["name"] == AI_HELP:
                continue
            label = f"{command['name']} {sub['name']}"
            result = run(cli_path, help_invocation(command["name"], sub["name"]), env)
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
