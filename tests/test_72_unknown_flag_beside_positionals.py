#!/usr/bin/env python3
"""
Test 72: an unrecognised flag written after or between a command's positional
arguments (rmp tasks #465 and #484; SPEC/COMMANDS.md § Positional Arguments,
acceptance criteria 11 to 18).

## The defects

Thirty-one subcommands read their positional arguments by position and never
looked at the tokens left after them. A `-`-prefixed token written after the
last positional argument was discarded, so the invocation did its work:

    rmp task remove -r <name> 8 --foo      the task was deleted, exit 0
    rmp task get -r <name> 8 --foo         the task was written, exit 0

and `sprint add-tasks` joined every token after the sprint id into its id list,
so the same token was refused as a malformed task id rather than as the
unknown flag it is.

Fourteen of them take two positional arguments or more, and read them as the
first tokens left once the selector was removed. A flag written BETWEEN two
positional arguments was therefore read as the second one:

    rmp task prio -r <name> 12 --foo 3     refused as an invalid priority
    rmp task add-dep -r <name> 4 --foo 5   refused as a malformed dependency id

## What this module holds

  - Criterion 12, over every subcommand that reads positional arguments: `--foo`
    appended after the last positional argument of an invocation that succeeds
    as written exits 2 with `Error: invalid input: unknown flag: --foo`, writes
    nothing to stdout and changes nothing. The same invocation without `--foo`
    is then run and must succeed, which is what makes the refusal the flag's
    doing rather than a broken fixture's; on a mutating subcommand the roadmap
    must then have changed, so a snapshot that could not see the change could
    not pass the refusal half either.
  - Criterion 17, over the fourteen subcommands whose maximum is two or more:
    `--foo` written in EVERY gap between two positional arguments.
  - Criteria 14, 15 and 18: where the refusal falls among a command's other
    checks, and the slot refusal a token keeps before the first positional
    argument or with no positional argument after it.

"Changes nothing" is observed through the CLI, not the database file: the task
list, the sprint list, each sprint's member list in position order, and the
audit log, which every write of this CLI appends to.
"""

import inspect
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


UNKNOWN_FLAG_LINE = "Error: invalid input: unknown flag: --foo"

# Each row: family, subcommand, the positional arguments of an invocation that
# succeeds on the fixture below, the preparation it needs, and whether that
# successful invocation changes the roadmap.
#
# These are the thirty-one subcommands of rmp task #465; the fourteen with two
# positional arguments or more are the subcommands of rmp task #484.
ROADMAP_SCOPED_SUBCOMMANDS = [
    ("task", "get", ["7"], None, False),
    ("task", "next", ["1"], "open_payment_sprint", False),
    ("task", "remove", ["7"], None, True),
    # Task 1 is started first, so a flagless transition (DOING -> TESTING) and a
    # reopening that changes something (DOING -> SPRINT) both succeed: under the
    # sprint membership invariant `task stat 1 BACKLOG` is refused for a sprint
    # member, and `task reopen` of a SPRINT task changes nothing.
    ("task", "stat", ["1", "TESTING"], "doing_capture_task", True),
    ("task", "reopen", ["1"], "doing_capture_task", True),
    ("task", "prio", ["7", "5"], None, True),
    ("task", "sev", ["7", "5"], None, True),
    ("task", "subtasks", ["4"], None, False),
    ("task", "add-dep", ["7", "8"], None, True),
    ("task", "remove-dep", ["4", "5"], None, True),
    ("task", "blockers", ["4"], None, False),
    ("task", "blocking", ["5"], None, False),
    ("sprint", "get", ["1"], None, False),
    ("sprint", "show", ["1"], None, False),
    ("sprint", "remove", ["2"], None, True),
    ("sprint", "start", ["1"], None, True),
    ("sprint", "close", ["2"], "open_refund_sprint", True),
    ("sprint", "reopen", ["2"], "closed_refund_sprint", True),
    ("sprint", "stats", ["1"], None, False),
    ("sprint", "add-tasks", ["2", "7"], None, True),
    ("sprint", "remove-tasks", ["1", "3"], None, True),
    ("sprint", "move-tasks", ["1", "2", "3"], None, True),
    ("sprint", "reorder", ["1", "3,2,1"], None, True),
    ("sprint", "move-to", ["1", "3", "0"], None, True),
    ("sprint", "swap", ["1", "1", "3"], None, True),
    ("sprint", "top", ["1", "3"], None, True),
    ("sprint", "bottom", ["1", "1"], None, True),
    ("backlog", "show-next", ["2"], None, False),
    ("audit", "history", ["TASK", "7"], None, False),
]

# The subcommands that read positional arguments and refused a trailing flag
# before the fix, because they build a flag parser over what follows. Criterion
# 12 binds them too, so they are driven here with the same assertions; each row
# also carries the flags its successful invocation needs, written after --foo.
FLAG_PARSING_POSITIONAL_SUBCOMMANDS = [
    ("task", "edit", ["7"], ["-t", "Localise receipt emails for Spain"], True),
    ("sprint", "tasks", ["1"], [], False),
    ("sprint", "open-tasks", ["1"], [], False),
    ("sprint", "update", ["2"], ["-d", "Deliver the refund flow for card payments"], True),
    ("task", "comment-add", ["7"], ["-y", "NOTE", "-b", "Receipt copy reviewed by the Lisbon team"], True),
    ("task", "comment-list", ["7"], [], False),
    ("task", "comment-edit", ["1"], ["-b", "Receipt copy approved by the Lisbon team"], True),
    ("task", "comment-remove", ["1"], [], True),
    ("sprint", "comment-add", ["1"], ["-y", "DECISION", "-b", "Capture scope agreed with finance"], True),
    ("sprint", "comment-list", ["1"], [], False),
    ("sprint", "comment-edit", ["1"], ["-b", "Capture scope signed off by finance"], True),
    ("sprint", "comment-remove", ["1"], [], True),
]

TASKS = [
    ("Capture card authorisations at checkout", None),
    ("Reconcile settlement batches nightly", None),
    ("Expire abandoned checkout sessions", None),
    ("Publish the refund audit trail", None),
    ("Encrypt stored cardholder tokens", None),
    ("Rotate the token encryption keys", 4),
    ("Localise receipt emails for Portugal", None),
    ("Add retry backoff to webhook delivery", None),
]


class StrayFlagBase:
    """A fresh HOME per test, and a fresh roadmap per invocation driven."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self._serial = 0

    def teardown_method(self):
        self.test.teardown()

    def rmp(self, args):
        return self.test.run_cmd(args, check=False)

    def must(self, args):
        code, out, err = self.rmp(args)
        assert code == 0, f"fixture step rmp {' '.join(args)} failed: exit={code} stderr={err!r}"
        return out

    def build_roadmap(self, preparation=None):
        """Eight tasks and two sprints:

        tasks 1-3 are members of sprint 1 (status SPRINT), task 4 depends on
        task 5, task 6 is a subtask of task 4, tasks 7 and 8 are free BACKLOG
        tasks; sprint 2 is empty. Both sprints are PENDING unless the
        preparation says otherwise. Task 7 and sprint 1 each carry one comment.
        """
        self._serial += 1
        name = f"settlement-{self._serial}"
        self.must(["roadmap", "create", name])
        for title, parent in TASKS:
            args = ["task", "create", "-r", name, "-t", title,
                    "-fr", f"Merchants rely on: {title.lower()}",
                    "-tr", "Extend the settlement service behind a feature flag",
                    "-ac", "The flow is observable end to end in staging"]
            if parent is not None:
                args += ["--parent", str(parent)]
            self.must(args)
        self.must(["sprint", "create", "-r", name, "-t", "Payment capture",
                   "-d", "Deliver the card payment capture flow"])
        self.must(["sprint", "create", "-r", name, "-t", "Refund flow",
                   "-d", "Deliver the refund flow"])
        self.must(["sprint", "add-tasks", "-r", name, "1", "1,2,3"])
        self.must(["task", "add-dep", "-r", name, "4", "5"])
        self.must(["task", "comment-add", "-r", name, "7", "-y", "NOTE",
                   "-b", "Receipt copy drafted for review"])
        self.must(["sprint", "comment-add", "-r", name, "1", "-y", "DECISION",
                   "-b", "Capture scope drafted with finance"])
        if preparation == "open_payment_sprint":
            self.must(["sprint", "start", "-r", name, "1"])
        elif preparation == "open_refund_sprint":
            self.must(["sprint", "start", "-r", name, "2"])
        elif preparation == "closed_refund_sprint":
            self.must(["sprint", "start", "-r", name, "2"])
            self.must(["sprint", "close", "-r", name, "2"])
        elif preparation == "doing_capture_task":
            self.must(["task", "stat", "-r", name, "1", "DOING", "--commit-open", "5f93b51"])
        return name

    def read(self, args):
        code, out, _err = self.rmp(args)
        return code, out

    def state(self, roadmap):
        """Everything a write of this CLI can change, read back through the CLI.

        Each read keeps its exit code beside its output instead of requiring
        success: a successful control run may remove the very task or sprint a
        read names, which is a change the snapshot must record rather than a
        fixture failure.
        """
        return {
            "tasks": self.read(["task", "list", "-r", roadmap]),
            "sprints": self.read(["sprint", "list", "-r", roadmap]),
            "sprint 1 members": self.read(["sprint", "tasks", "-r", roadmap, "1"]),
            "sprint 2 members": self.read(["sprint", "tasks", "-r", roadmap, "2"]),
            "task 7 comments": self.read(["task", "comment-list", "-r", roadmap, "7"]),
            "sprint 1 comments": self.read(["sprint", "comment-list", "-r", roadmap, "1"]),
            "audit": self.read(["audit", "list", "-r", roadmap, "-l", "500"]),
        }

    def drive(self, family, subcommand, positionals, flags, preparation, mutates, gap):
        """Refuse `--foo` written at index `gap` of the positional arguments,
        then run the invocation without it. Returns a list of problems."""
        problems = []
        roadmap = self.build_roadmap(preparation)
        written = positionals[:gap] + ["--foo"] + positionals[gap:] + flags
        label = f"rmp {family} {subcommand} -r <name> {' '.join(written)}"

        before = self.state(roadmap)
        code, out, err = self.rmp([family, subcommand, "-r", roadmap] + written)
        first = err.splitlines()[0] if err else ""
        if code != 2:
            problems.append(f"{label}: exit {code}, want 2 (stdout={out[:160]!r} stderr={err[:200]!r})")
        if first != UNKNOWN_FLAG_LINE:
            problems.append(f"{label}: first stderr line {first!r}, want {UNKNOWN_FLAG_LINE!r}")
        if out != "":
            problems.append(f"{label}: a refused invocation wrote to stdout: {out[:160]!r}")
        after = self.state(roadmap)
        for key in before:
            if before[key] != after[key]:
                problems.append(f"{label}: the refusal changed {key}")

        control = [family, subcommand, "-r", roadmap] + positionals + flags
        code, out, err = self.rmp(control)
        if code != 0:
            problems.append(f"control rmp {' '.join(control)}: exit {code}, want 0 -- the fixture does not "
                            f"make the invocation succeed as written (stderr={err[:200]!r})")
        elif mutates and self.state(roadmap) == before:
            problems.append(f"control rmp {' '.join(control)}: succeeded yet changed nothing the snapshot "
                            f"reads, so the snapshot could not have seen the refusal change anything")
        return problems


class TestAFlagAfterThePositionalArguments(StrayFlagBase):
    """Criteria 11, 12 and 13 (rmp task #465)."""

    def test_every_roadmap_scoped_positional_subcommand_refuses_it(self):
        problems = []
        for family, subcommand, positionals, preparation, mutates in ROADMAP_SCOPED_SUBCOMMANDS:
            problems += self.drive(family, subcommand, positionals, [], preparation, mutates,
                                   gap=len(positionals))
        assert not problems, "\n".join(problems)

    def test_the_flag_parsing_positional_subcommands_refuse_it_too(self):
        problems = []
        for family, subcommand, positionals, flags, mutates in FLAG_PARSING_POSITIONAL_SUBCOMMANDS:
            problems += self.drive(family, subcommand, positionals, flags, None, mutates,
                                   gap=len(positionals))
        assert not problems, "\n".join(problems)

    def test_roadmap_create_and_remove_refuse_it_and_touch_no_directory(self):
        t = self.test
        code, out, err = self.rmp(["roadmap", "create", "ledger-archive", "--foo"])
        assert (code, err.splitlines()[0] if err else "", out) == (2, UNKNOWN_FLAG_LINE, ""), (
            f"roadmap create <name> --foo: exit={code} stdout={out!r} stderr={err!r}")
        assert not (t.roadmaps_dir / "ledger-archive").exists(), (
            "a refused roadmap create left the roadmap directory behind")
        self.must(["roadmap", "create", "ledger-archive"])

        self.must(["roadmap", "create", "ledger-sandbox"])
        code, out, err = self.rmp(["roadmap", "remove", "ledger-sandbox", "--foo"])
        assert (code, err.splitlines()[0] if err else "", out) == (2, UNKNOWN_FLAG_LINE, ""), (
            f"roadmap remove <name> --foo: exit={code} stdout={out!r} stderr={err!r}")
        assert (t.roadmaps_dir / "ledger-sandbox" / "project.db").exists(), (
            "a refused roadmap remove deleted the roadmap")
        self.must(["roadmap", "remove", "ledger-sandbox"])
        assert not (t.roadmaps_dir / "ledger-sandbox").exists()

    def test_sprint_add_tasks_names_the_flag_not_a_malformed_id(self):
        roadmap = self.build_roadmap()
        code, out, err = self.rmp(["sprint", "add-tasks", "-r", roadmap, "2", "7", "--foo"])
        first = err.splitlines()[0] if err else ""
        assert (code, first, out) == (2, UNKNOWN_FLAG_LINE, ""), (
            f"criterion 13: exit={code} first stderr line={first!r} stdout={out!r}")

    def test_the_driven_set_is_every_subcommand_the_contract_publishes_with_positionals(self):
        """No positional subcommand is left out of criterion 12's sweep."""
        import json
        contract = json.loads(self.must(["--ai-help"]))
        published = {
            (command["name"], sub["name"])
            for command in contract["commands"]
            for sub in command["subcommands"]
            if sub.get("positional_arguments")
        }
        driven = {(f, s) for f, s, *_ in ROADMAP_SCOPED_SUBCOMMANDS}
        driven |= {(f, s) for f, s, *_ in FLAG_PARSING_POSITIONAL_SUBCOMMANDS}
        driven |= {("roadmap", "create"), ("roadmap", "remove")}
        assert driven == published, (
            f"published but not driven: {sorted(published - driven)}; "
            f"driven but not published: {sorted(driven - published)}")
        assert len(ROADMAP_SCOPED_SUBCOMMANDS) + 2 == 31, "rmp task #465 names thirty-one subcommands"


class TestAFlagBetweenThePositionalArguments(StrayFlagBase):
    """Criteria 16 and 17 (rmp task #484)."""

    def test_every_gap_of_every_multi_positional_subcommand(self):
        rows = [row for row in ROADMAP_SCOPED_SUBCOMMANDS if len(row[2]) >= 2]
        assert len(rows) == 14, f"rmp task #484 names fourteen subcommands, found {len(rows)}"
        problems = []
        gaps_driven = 0
        for family, subcommand, positionals, preparation, mutates in rows:
            for gap in range(1, len(positionals)):
                gaps_driven += 1
                problems += self.drive(family, subcommand, positionals, [], preparation, mutates, gap)
        assert gaps_driven == 17, f"expected 17 gaps (three subcommands take three arguments), drove {gaps_driven}"
        assert not problems, "\n".join(problems)


class TestWhereTheRefusalFalls(StrayFlagBase):
    """Criteria 14, 15 and 18, and the placement paragraph of § Positional Arguments."""

    def expect(self, args, code, line=None, not_line=None):
        got_code, out, err = self.rmp(args)
        first = err.splitlines()[0] if err else ""
        assert got_code == code, f"rmp {' '.join(args)}: exit {got_code}, want {code} (stderr={err!r})"
        if line is not None:
            assert first == line, f"rmp {' '.join(args)}: first stderr line {first!r}, want {line!r}"
        if not_line is not None:
            assert first != not_line, f"rmp {' '.join(args)}: refused with {first!r}"
        assert out == "", f"rmp {' '.join(args)}: a refused invocation wrote to stdout: {out!r}"

    def test_a_token_in_a_slot_keeps_that_slots_refusal(self):
        r = self.build_roadmap()
        self.expect(["task", "get", "-r", r, "--foo"], 2,
                    'Error: invalid input: invalid task ID: "--foo" (must be a positive integer)')
        self.expect(["task", "next", "-r", r, "--foo"], 6)
        self.expect(["task", "prio", "-r", r, "--foo", "7", "3"], 2,
                    'Error: invalid input: invalid task ID: "--foo" (must be a positive integer)')
        # A "-"-prefixed token in the <priority> slot is read as the priority,
        # which is not an integer: exit 2 (rmp task 503).
        self.expect(["task", "prio", "-r", r, "7", "--foo"], 2,
                    'Error: invalid input: invalid priority: "--foo" is not an integer in 0-9')
        self.expect(["sprint", "add-tasks", "-r", r, "2", "--foo"], 2,
                    'Error: invalid input: invalid task ID: "--foo" (must be a positive integer)')
        self.expect(["audit", "history", "-r", r, "--foo"], 2, not_line=UNKNOWN_FLAG_LINE)

    def test_the_positional_checks_come_first(self):
        r = self.build_roadmap()
        self.expect(["task", "remove", "-r", r, "0", "--foo"], 6,
                    "Error: validation error: task_id must be between 1 and 2147483647, got 0")
        self.expect(["task", "remove", "-r", r, "abc", "--foo"], 2,
                    'Error: invalid input: invalid task ID: "abc" (must be a positive integer)')
        self.expect(["task", "prio", "-r", r, "7", "--foo", "99"], 6,
                    "Error: validation error: priority must be between 0 and 9, got 99")
        self.expect(["task", "stat", "-r", r, "7", "SHIPPED", "--foo"], 6)
        self.expect(["roadmap", "create", "Ledger", "--foo"], 6, not_line=UNKNOWN_FLAG_LINE)

    def test_the_refusal_precedes_the_roadmap_and_every_flag_value(self):
        r = self.build_roadmap()
        self.expect(["task", "remove", "-r", "ghost-ledger", "7", "--foo"], 2, UNKNOWN_FLAG_LINE)
        self.expect(["task", "prio", "-r", "ghost-ledger", "7", "--foo", "3"], 2, UNKNOWN_FLAG_LINE)
        self.expect(["roadmap", "remove", "ghost-ledger", "--foo"], 2, UNKNOWN_FLAG_LINE)
        # --summary on a target other than COMPLETED is refused at step 3; the
        # flag is refused before it.
        self.expect(["task", "stat", "-r", r, "7", "BACKLOG", "--foo", "--summary", "Shipped"], 2,
                    UNKNOWN_FLAG_LINE)
        # The selector is resolved before either.
        self.expect(["task", "remove", "7", "--foo"], 3)

    def test_the_first_stray_is_named_without_its_value(self):
        r = self.build_roadmap()
        self.expect(["task", "prio", "-r", r, "7", "--bar", "5", "--foo"], 2,
                    "Error: invalid input: unknown flag: --bar")
        self.expect(["task", "get", "-r", r, "7", "--foo=1"], 2, UNKNOWN_FLAG_LINE)


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
    print(f"Unknown flag beside positionals tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if _run_all() else 1)
