#!/usr/bin/env python3
"""
Test 61: every command help lists the exit code that command really emits for
an unresolved subcommand (rmp task #299).

THE DEFECT THIS GUARDS
----------------------
SPEC/HELP.md section "Exit codes" requires each family help to carry an
`Exit codes:` block "listing only the codes the command can actually emit".
Five family helps -- task, sprint, backlog, audit and graph -- listed 0..6 and
stopped there, while each of them exits 127 for a subcommand name it cannot
resolve (SPEC/ARCHITECTURE.md section "Exit Code Standards", `EXIT_CMD_NOT_FOUND`).
The omission predates the dispatch-failure cluster (rmp tasks #274, #251, #273):
before it the same five exited 2 for an unresolved subcommand and did not list 2
either, so only the identity of the unlisted code changed. `roadmap` was the one
family that actively CLAIMED "2   Unknown subcommand", so the cluster had to
correct it there; filling the other five in was left as this task.

WHY THIS MODULE IS NOT A DUPLICATE OF WHAT ALREADY EXISTS
---------------------------------------------------------
Two guards already cover the BEHAVIOUR half and neither covers the help text:

  * cmd/rmp/dispatch_failure_test.go -- in-process, pins the whole dispatch
    contract (exit 127 at both levels, empty stdout, recovery help, banner
    suppression, part order, wording) across the six dispatching families.
    It never reads an `Exit codes:` block.
  * tests/test_27_exit_code_extremes.py -- binary-level, pins 127 end to end
    for the line in main() the in-process suite cannot reach.

And one guard covers a neighbouring documentation direction:

  * tests/test_60_docs_readme_contract_completeness.py (rmp task #126) -- proves
    nothing the AI contract publishes goes undocumented in DOCS/commands/*.md
    and README.md. Its exit-code leg compares the UNION of a family's
    per-subcommand `exit_codes` against that family's DOCS table. 127 is not in
    that union: it is emitted by the family DISPATCHER, before any subcommand is
    resolved, so no subcommand's `exit_codes` carries it and no directional
    contract->DOCS check can ever demand it. Nor does #126 read plain-text
    `--help` output at all; its corpora are the markdown pages.

What no guard held is the join between the two: that the code a command really
emits appears in the block that command prints when asked for help. That is the
single property this module adds.

DERIVED, NOT TRANSCRIBED
------------------------
Nothing here hardcodes the number 127, nor the list of five families. Both are
read from `rmp --ai-help`, which is generated at run time from the internal
command registry (internal/commands/registry*.go) and is therefore the same
source the dispatcher itself is built from:

  * the dispatch-failure code comes from the contract's top-level `exit_codes`
    catalogue entry named `EXIT_CMD_NOT_FOUND`;
  * the command list is every command the contract publishes -- no family/leaf
    classification is applied. Each command is PROBED with an unresolved token
    and the requirement is raised only for those observed to answer with the
    dispatch code. A leaf command (`stats`, `web`, `ai-help`) refuses the excess
    token as misuse instead, so it is excluded by observation rather than by an
    exemption list, and a future command that starts dispatching subcommands is
    picked up the day it does.

A test that repeated the constant would pass for the wrong reason the moment the
catalogue moved; this one fails.

NON-VACUITY
-----------
Every assertion is paired with a floor, in the style of test_55 and test_60:
the catalogue must actually yield a dispatch code, at least six commands must be
observed emitting it, every parsed `Exit codes:` block must yield at least three
codes including 0, and the probe token must resolve to nothing anywhere in the
contract. A parser that matched nothing, or a probe token that accidentally
named a real subcommand, would otherwise leave the comparison green and empty.
"""

import inspect
import json
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase

RUN_TIMEOUT_SECONDS = 30

# The catalogue entry that names the dispatch-failure code. The NAME is stable
# (SPEC/ARCHITECTURE.md section "Exit Code Standards"); the number behind it is
# read from the contract so this module never transcribes it.
DISPATCH_FAILURE_CODE_NAME = "EXIT_CMD_NOT_FOUND"

# The token used to provoke a dispatch failure. A plausible subcommand an
# operator might reach for -- roadmaps do get archived -- that no family
# actually offers. test_probe_token_resolves_to_nothing proves it names no
# command, subcommand or alias, so a future `archive` subcommand cannot quietly
# turn these probes into successful invocations.
PROBE_TOKEN = "archive"

# At least the six dispatching families (roadmap, task, sprint, backlog, audit,
# graph) must answer the probe with the dispatch code. Stated as a floor rather
# than an equality so a seventh family is a pass, not a failure to update.
MIN_DISPATCHING_COMMANDS = 6

# A real `Exit codes:` block always carries 0 plus at least two failure codes;
# the thinnest today (backlog) carries five entries. Three is a deliberately
# slack floor whose only job is to catch a block parser that matched nothing.
MIN_CODES_PER_BLOCK = 3

# A code line inside an `Exit codes:` block: exactly two leading spaces, then
# the number. Continuation lines of a multi-line cause are indented seven
# spaces ("       invalid state transition, ...") and must NOT be read as codes,
# which this anchoring rules out by construction.
_EXIT_CODE_LINE = re.compile(r"^ {2}(\d+)(?:\s|$)")
_EXIT_CODES_HEADING = re.compile(r"^Exit codes:\s*$", re.IGNORECASE)


def parse_exit_codes_block(help_text, label):
    """Return the set of codes listed in the `Exit codes:` block of a help
    screen. The block runs from its heading to the first blank line (the
    family-help template in SPEC/HELP.md separates every section that way)."""
    lines = help_text.split("\n")
    for i, line in enumerate(lines):
        if not _EXIT_CODES_HEADING.match(line):
            continue
        codes = set()
        for body in lines[i + 1:]:
            if body.strip() == "":
                break
            m = _EXIT_CODE_LINE.match(body)
            if m:
                codes.add(int(m.group(1)))
        return codes
    raise AssertionError(
        f"{label}: no 'Exit codes:' block found in the help output; "
        f"SPEC/HELP.md section 'Exit codes' requires one on every help screen"
    )


class _ContractFixture:
    """Loads the AI contract once per test method, under this module's own
    HOME so neither the contract nor any probe can touch the invoking user's
    roadmaps."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path
        self.env = os.environ.copy()
        self.env["HOME"] = str(self.test.home_dir)
        self.contract = self._run(["--ai-help"], expect_zero=True).stdout
        self.contract = json.loads(self.contract)

    def teardown_method(self):
        self.test.teardown()

    def _run(self, args, expect_zero=False):
        result = subprocess.run(
            [self.cli] + args,
            capture_output=True,
            text=True,
            env=self.env,
            timeout=RUN_TIMEOUT_SECONDS,
        )
        if expect_zero:
            assert result.returncode == 0, (
                f"rmp {' '.join(args)} exited {result.returncode}, expected 0; "
                f"stderr={result.stderr!r}"
            )
        return result

    # -- derivations from the contract -------------------------------------

    def dispatch_failure_code(self):
        matches = [
            entry["code"]
            for entry in self.contract["exit_codes"]
            if entry["name"] == DISPATCH_FAILURE_CODE_NAME
        ]
        assert len(matches) == 1, (
            f"the contract's exit_codes catalogue holds {len(matches)} entries "
            f"named {DISPATCH_FAILURE_CODE_NAME}, expected exactly 1 -- this "
            f"module derives the dispatch-failure code from that entry and "
            f"cannot check anything without it"
        )
        return matches[0]

    def command_names(self):
        names = [cmd["name"] for cmd in self.contract["commands"]]
        assert len(names) >= MIN_DISPATCHING_COMMANDS, (
            f"the contract publishes only {len(names)} commands; expected at "
            f"least {MIN_DISPATCHING_COMMANDS} -- contract extraction is broken"
        )
        return names


# ---------------------------------------------------------------------------
# 1. The probe is real: the token names nothing the CLI can resolve.
# ---------------------------------------------------------------------------


class TestProbeTokenIsUnresolvable(_ContractFixture):

    def test_probe_token_resolves_to_nothing(self):
        collisions = []
        for cmd in self.contract["commands"]:
            if cmd["name"] == PROBE_TOKEN or PROBE_TOKEN in cmd.get("aliases", []):
                collisions.append(f"command {cmd['name']}")
            for sub in cmd["subcommands"]:
                if sub["name"] == PROBE_TOKEN or PROBE_TOKEN in sub.get("aliases", []):
                    collisions.append(f"subcommand {cmd['name']} {sub['name']}")
        assert not collisions, (
            f"the probe token {PROBE_TOKEN!r} now resolves to {collisions}, so "
            f"every dispatch-failure probe in this module would invoke a real "
            f"command instead of failing to resolve -- pick another token"
        )


# ---------------------------------------------------------------------------
# 2. The join: a command that emits the dispatch code must list it.
# ---------------------------------------------------------------------------


class TestHelpListsTheDispatchFailureExitCode(_ContractFixture):

    def test_every_command_that_emits_it_documents_it(self):
        expected = self.dispatch_failure_code()
        problems = []
        dispatching = []
        blocks_parsed = 0

        for name in self.command_names():
            observed = self._run([name, PROBE_TOKEN]).returncode
            if observed != expected:
                # Not a dispatcher: a leaf command refuses the excess token as
                # misuse. Nothing to require of its help.
                continue
            dispatching.append(name)

            help_result = self._run([name, "--help"], expect_zero=True)
            codes = parse_exit_codes_block(help_result.stdout, f"rmp {name} --help")
            blocks_parsed += 1
            assert len(codes) >= MIN_CODES_PER_BLOCK, (
                f"rmp {name} --help: only {len(codes)} exit code(s) parsed from "
                f"the 'Exit codes:' block ({sorted(codes)}); expected at least "
                f"{MIN_CODES_PER_BLOCK} -- the block parser is broken, which "
                f"would make this comparison vacuous"
            )
            assert 0 in codes, (
                f"rmp {name} --help: the parsed 'Exit codes:' block "
                f"{sorted(codes)} omits 0, which SPEC/HELP.md requires on every "
                f"help screen -- the block parser is reading the wrong lines"
            )

            if expected not in codes:
                problems.append(
                    f"rmp {name} --help: the 'Exit codes:' block lists "
                    f"{sorted(codes)} and omits {expected} "
                    f"({DISPATCH_FAILURE_CODE_NAME}), yet "
                    f"`rmp {name} {PROBE_TOKEN}` exits {expected}"
                )

        assert len(dispatching) >= MIN_DISPATCHING_COMMANDS, (
            f"only {len(dispatching)} command(s) ({dispatching}) were observed "
            f"exiting {expected} for an unresolved subcommand; expected at "
            f"least {MIN_DISPATCHING_COMMANDS} -- either dispatch stopped "
            f"producing {DISPATCH_FAILURE_CODE_NAME} or the probe stopped "
            f"reaching the dispatcher, and this check has gone vacuous"
        )
        assert blocks_parsed == len(dispatching), (
            f"parsed {blocks_parsed} 'Exit codes:' block(s) for "
            f"{len(dispatching)} dispatching command(s)"
        )
        assert not problems, (
            f"{len(problems)} command help screen(s) omit the exit code they "
            f"actually emit for an unresolved subcommand:\n  "
            + "\n  ".join(problems)
        )


# ---------------------------------------------------------------------------
# 3. The behavioural half, at binary level: the probe really does exit with the
#    catalogue code, and says so in the documented wording.
# ---------------------------------------------------------------------------


class TestUnresolvedSubcommandExitsWithCatalogueCode(_ContractFixture):

    def test_probe_exit_code_and_message(self):
        expected = self.dispatch_failure_code()
        dispatching = []
        problems = []

        for name in self.command_names():
            result = self._run([name, PROBE_TOKEN])
            if result.returncode != expected:
                continue
            dispatching.append(name)
            wanted = f"unknown {name} subcommand: {PROBE_TOKEN}"
            if wanted not in result.stderr:
                problems.append(
                    f"rmp {name} {PROBE_TOKEN}: stderr does not carry "
                    f"{wanted!r}; got {result.stderr.splitlines()[:1]}"
                )
            if result.stdout != "":
                problems.append(
                    f"rmp {name} {PROBE_TOKEN}: wrote {len(result.stdout)} "
                    f"byte(s) to stdout, expected none"
                )

        assert len(dispatching) >= MIN_DISPATCHING_COMMANDS, (
            f"only {len(dispatching)} command(s) ({dispatching}) exited "
            f"{expected} for an unresolved subcommand; expected at least "
            f"{MIN_DISPATCHING_COMMANDS}"
        )
        assert not problems, (
            f"{len(problems)} dispatch-failure message defect(s):\n  "
            + "\n  ".join(problems)
        )


# ---------------------------------------------------------------------------
# Runner: classes are DISCOVERED by inspecting this module, never listed, so a
# class added later cannot go unrun (rmp task #303).
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
    print(f"Family-help dispatch exit-code tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
