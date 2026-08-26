#!/usr/bin/env python3
"""
Test 57: the positional-argument arity contract (rmp task #293).

End-to-end backstop for SPEC/COMMANDS.md § Positional Arguments, driven against
the compiled ./bin/rmp.

The recorded defect: FlagParser.Parse collected every unrecognised positional
argument into ParseResult.Args and no caller inspected the slice, so a token the
user meant to matter was accepted and thrown away.

    rmp roadmap create alpha-service beta-service
    {"name": "alpha-service"}                                    exit 0

`beta-service` vanished, and the roadmap the user asked for did not exist.
Eleven commands behaved that way: task list, sprint show, sprint list,
sprint tasks, backlog list, audit list, audit stats, stats, roadmap list,
version, and roadmap create.

The rule that closes it declares, for every command, the maximum number of
positional arguments it accepts, and refuses an invocation that exceeds it with
exit code 2 and the line

    Error: invalid input: unexpected argument "X"

naming the FIRST offending token. What the module holds, and why each half is
needed:

  - The refusals. Every recorded command, plus one command per declared arity,
    is driven one token over its maximum and must exit 2 writing nothing to
    stdout.
  - The acceptances. Commands of declared arity 1, 2 and 3 are driven at their
    FULL arity and must succeed. This half is what tells the rule apart from a
    blanket refusal of everything past the first positional argument, which
    would pass every refusal test above and break `sprint move-tasks`,
    `sprint move-to`, `sprint swap`, `task stat`, `task prio` and `task sev`.
  - The outcomes. A refused invocation must have done NOTHING: no roadmap
    directory appears, no task row disappears, and the audit log does not
    grow. An exit-code-only assertion would keep passing if the refusal moved
    to after the work it exists to prevent.
  - The precedence. A refusal on a roadmap that does not exist still exits 2,
    not 4, which can only happen if the refusal precedes opening the store.
  - The exemptions. The four command families that already refused an excess
    positional argument before the rule was written — the `graph`
    subcommands, the eight comment subcommands, `rmp web` and `rmp ai-help` —
    must be unchanged, and three of them publish wording of their own that the
    shared enforcement point must not override.
  - The global forms. `rmp help`, `rmp --help`, `rmp -h`, `rmp version`,
    `rmp --version` and `rmp -v` resolve before any command lookup, so they are
    enforced at a second site and are the forms most likely to be forgotten.
"""

import inspect
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


# The canonical refusal, as SPEC/COMMANDS.md § Positional Arguments publishes
# it. The offending token is echoed exactly as the user supplied it, in double
# quotes.
def canonical_line(token):
    return f'Error: invalid input: unexpected argument "{token}"'


class TestPositionalArityRefusals:
    """The refusal itself, across every family and every declared arity."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("settlement-platform")
        self.tasks = [
            self.test.create_task(
                self.roadmap,
                "Capture authorisation on the payment gateway",
                "Merchants must see an authorisation outcome before shipping",
                "Extend the gateway client with a capture call and retries",
                "A captured payment is visible in the settlement report"),
            self.test.create_task(
                self.roadmap,
                "Reconcile settlement batches nightly",
                "Finance needs a nightly reconciliation to close the books",
                "Add a batch job comparing acquirer files against ledger rows",
                "A mismatched batch raises an alert within one hour"),
            self.test.create_task(
                self.roadmap,
                "Expire abandoned checkout sessions",
                "Abandoned sessions hold inventory that other buyers need",
                "Add a sweeper that releases reservations after 30 minutes",
                "An abandoned session releases its reservation"),
        ]
        self.sprints = [
            self.test.create_sprint(self.roadmap, "Deliver the payment capture flow", "Payment capture"),
            self.test.create_sprint(self.roadmap, "Deliver the refund flow", "Refund flow"),
        ]

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers ------------------------------------------------------

    def assert_refused(self, args, offending):
        code, stdout, stderr = self.test.run_cmd(args, check=False)
        assert code == 2, (
            f"rmp {' '.join(args)}: exit={code}, want 2 (EXIT_MISUSE); "
            f"stdout={stdout!r} stderr={stderr!r}")
        first = stderr.splitlines()[0] if stderr else ""
        assert first == canonical_line(offending), (
            f"rmp {' '.join(args)}: stderr first line={first!r}, "
            f"want {canonical_line(offending)!r}")
        assert stdout == "", (
            f"rmp {' '.join(args)}: a refused invocation wrote to stdout: {stdout!r}")

    def assert_accepted(self, args):
        code, stdout, stderr = self.test.run_cmd(args, check=False)
        assert code == 0, (
            f"rmp {' '.join(args)}: exit={code}, want 0 — the invocation is within its "
            f"declared arity; stdout={stdout!r} stderr={stderr!r}")

    # ---- the eleven recorded commands ---------------------------------

    def test_the_named_defect_is_refused(self):
        # The invocation the task records, verbatim.
        self.assert_refused(
            ["roadmap", "create", "alpha-service", "beta-service"], "beta-service")

    def test_every_recorded_command_is_refused(self):
        r = self.roadmap
        recorded = [
            (["task", "list", "-r", r, "unscheduled"], "unscheduled"),
            (["sprint", "show", "-r", r, str(self.sprints[0]), "unscheduled"], "unscheduled"),
            (["sprint", "list", "-r", r, "unscheduled"], "unscheduled"),
            (["sprint", "tasks", "-r", r, str(self.sprints[0]), "unscheduled"], "unscheduled"),
            (["backlog", "list", "-r", r, "unscheduled"], "unscheduled"),
            (["audit", "list", "-r", r, "unscheduled"], "unscheduled"),
            (["audit", "stats", "-r", r, "unscheduled"], "unscheduled"),
            (["stats", "-r", r, "unscheduled"], "unscheduled"),
            (["roadmap", "list", "unscheduled"], "unscheduled"),
        ]
        for args, offending in recorded:
            self.assert_refused(args, offending)

    def test_backlog_show_next_follows_the_general_rule(self):
        # `backlog show-next` declares one positional argument, the count. A
        # stray token after it used to be ignored, leaving the invocation to
        # succeed; it is now refused like every other excess argument.
        self.assert_refused(
            ["backlog", "show-next", "-r", self.roadmap, "5", "unscheduled"], "unscheduled")
        self.assert_accepted(["backlog", "show-next", "-r", self.roadmap, "5"])

    # ---- one command per declared arity -------------------------------

    def test_arity_one_commands_are_refused_at_two(self):
        r = self.roadmap
        first, second = str(self.tasks[0]), str(self.tasks[1])
        for args, offending in [
            (["task", "get", "-r", r, first, second], second),
            (["task", "remove", "-r", r, first, second], second),
            (["task", "subtasks", "-r", r, first, "unscheduled"], "unscheduled"),
            (["sprint", "stats", "-r", r, str(self.sprints[0]), "unscheduled"], "unscheduled"),
            (["roadmap", "remove", "alpha-service", "beta-service"], "beta-service"),
        ]:
            self.assert_refused(args, offending)

    def test_arity_two_commands_are_refused_at_three(self):
        r = self.roadmap
        first, second = str(self.tasks[0]), str(self.tasks[1])
        for args, offending in [
            (["task", "stat", "-r", r, first, "SPRINT", "unscheduled"], "unscheduled"),
            (["task", "prio", "-r", r, first, "7", "unscheduled"], "unscheduled"),
            (["task", "sev", "-r", r, first, "3", "unscheduled"], "unscheduled"),
            (["task", "add-dep", "-r", r, first, second, "unscheduled"], "unscheduled"),
            (["audit", "history", "-r", r, "TASK", first, "unscheduled"], "unscheduled"),
            (["sprint", "add-tasks", "-r", r, str(self.sprints[0]), first, second], second),
        ]:
            self.assert_refused(args, offending)

    def test_arity_three_commands_are_refused_at_four(self):
        r = self.roadmap
        first, second = str(self.tasks[0]), str(self.tasks[1])
        a, b = str(self.sprints[0]), str(self.sprints[1])
        for args, offending in [
            (["sprint", "move-tasks", "-r", r, a, b, first, "unscheduled"], "unscheduled"),
            (["sprint", "move-to", "-r", r, a, first, "1", "unscheduled"], "unscheduled"),
            (["sprint", "swap", "-r", r, a, first, second, "unscheduled"], "unscheduled"),
        ]:
            self.assert_refused(args, offending)

    # ---- the rule's own clauses ---------------------------------------

    def test_only_the_first_offending_token_is_named(self):
        self.assert_refused(
            ["roadmap", "create", "alpha-service", "beta-service", "gamma-service"],
            "beta-service")

    def test_the_position_of_the_offending_token_does_not_matter(self):
        r = self.roadmap
        self.assert_refused(
            ["task", "list", "-r", r, "unscheduled", "--limit", "5"], "unscheduled")
        self.assert_refused(
            ["task", "list", "-r", r, "--limit", "5", "unscheduled"], "unscheduled")

    def test_a_comma_separated_list_is_one_positional_argument(self):
        r = self.roadmap
        first, second = str(self.tasks[0]), str(self.tasks[1])
        self.assert_accepted(["task", "get", "-r", r, f"{first},{second}"])
        self.assert_refused(["task", "get", "-r", r, first, second], second)

    def test_a_dash_prefixed_token_is_a_flag_not_a_positional_argument(self):
        # Rule 5: an unrecognised "-"-prefixed token is refused as an unknown
        # flag, under the same exit code 2, and never as an excess positional
        # argument. Counting it as one would swap one message for the other.
        code, stdout, stderr = self.test.run_cmd(
            ["task", "list", "-r", self.roadmap, "--unscheduled"], check=False)
        assert code == 2, f"exit={code}, want 2; stderr={stderr!r}"
        assert "unknown flag: --unscheduled" in stderr, (
            f"a '-'-prefixed token must be reported as an unknown flag; got {stderr!r}")
        assert "unexpected argument" not in stderr, (
            f"a '-'-prefixed token was counted as a positional argument: {stderr!r}")
        assert stdout == ""

    def test_a_dispatch_failure_stays_a_dispatch_failure(self):
        # An unresolved subcommand name is resolved before any arity check, so
        # excess arguments must not convert exit 127 into exit 2.
        code, stdout, stderr = self.test.run_cmd(
            ["task", "nadadisto", "1", "2", "3"], check=False)
        assert code == 127, (
            f"exit={code}, want 127; the arity rule must never take over a "
            f"dispatch failure; stderr={stderr!r}")
        assert "unknown task subcommand: nadadisto" in stderr, stderr
        assert stdout == ""

    def test_an_excess_argument_beats_a_value_that_would_fail_with_six(self):
        r = self.roadmap
        first = str(self.tasks[0])
        # The value alone really is an exit-6 verdict, so the assertion below
        # distinguishes two live outcomes rather than one.
        code, _stdout, _stderr = self.test.run_cmd(
            ["task", "prio", "-r", r, first, "99"], check=False)
        assert code == 6, f"`task prio {first} 99` exit={code}, want 6"
        self.assert_refused(["task", "prio", "-r", r, first, "99", "unscheduled"], "unscheduled")

    def test_an_excess_argument_beats_a_roadmap_that_would_fail_with_four(self):
        code, _stdout, _stderr = self.test.run_cmd(
            ["task", "get", "-r", "roadmap-that-does-not-exist", "3"], check=False)
        assert code == 4, f"a missing roadmap alone exits {code}, want 4"
        self.assert_refused(
            ["task", "remove", "-r", "roadmap-that-does-not-exist", "3", "4"], "4")

    def test_no_help_follows_the_refusal(self):
        # An excess positional argument is not a dispatch failure, so stderr
        # carries the error line and the AI-agent hint alone.
        _code, _stdout, stderr = self.test.run_cmd(
            ["task", "list", "-r", self.roadmap, "unscheduled"], check=False)
        assert "Usage:" not in stderr, (
            f"help was written after an excess-argument refusal: {stderr!r}")


class TestPositionalArityAcceptances:
    """The other half: an invocation at its full declared arity is accepted.

    A blanket refusal of everything past the first positional argument passes
    every refusal test in this file and fails every test in this class.
    """

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("logistics-network")
        self.tasks = [
            self.test.create_task(
                self.roadmap,
                "Route parcels through the northern hub",
                "Northern deliveries miss the overnight cut-off",
                "Add hub selection to the routing planner",
                "A northern parcel is routed through the northern hub"),
            self.test.create_task(
                self.roadmap,
                "Track vehicle telemetry per leg",
                "Dispatchers cannot see where a delayed leg is",
                "Ingest telemetry events and attach them to the leg",
                "A delayed leg reports its last known position"),
            self.test.create_task(
                self.roadmap,
                "Publish a carrier performance report",
                "Contract reviews need measured carrier performance",
                "Aggregate on-time rates per carrier per week",
                "The weekly report lists every active carrier"),
        ]
        self.sprints = [
            self.test.create_sprint(self.roadmap, "Deliver hub-based routing", "Hub routing"),
            self.test.create_sprint(self.roadmap, "Deliver telemetry ingestion", "Telemetry"),
        ]

    def teardown_method(self):
        self.test.teardown()

    def accept(self, args):
        code, stdout, stderr = self.test.run_cmd(args, check=False)
        assert code == 0, (
            f"rmp {' '.join(args)}: exit={code}, want 0 — the invocation supplies exactly "
            f"its declared maximum; stdout={stdout!r} stderr={stderr!r}")
        return stdout

    def test_arity_one_at_full_arity(self):
        r = self.roadmap
        self.accept(["task", "get", "-r", r, str(self.tasks[0])])
        self.accept(["sprint", "get", "-r", r, str(self.sprints[0])])
        self.accept(["backlog", "show-next", "-r", r, "5"])

    def test_arity_two_at_full_arity(self):
        r = self.roadmap
        first, second = str(self.tasks[0]), str(self.tasks[1])
        self.accept(["task", "prio", "-r", r, first, "7"])
        self.accept(["task", "sev", "-r", r, first, "4"])
        self.accept(["audit", "history", "-r", r, "TASK", first])
        self.accept(["sprint", "add-tasks", "-r", r, str(self.sprints[0]), f"{first},{second}"])
        self.accept(["task", "add-dep", "-r", r, first, second])

    def test_arity_three_at_full_arity(self):
        r = self.roadmap
        first, second = str(self.tasks[0]), str(self.tasks[1])
        a, b = str(self.sprints[0]), str(self.sprints[1])
        self.accept(["sprint", "add-tasks", "-r", r, a, f"{first},{second}"])
        self.accept(["sprint", "move-to", "-r", r, a, first, "1"])
        self.accept(["sprint", "swap", "-r", r, a, first, second])
        self.accept(["sprint", "move-tasks", "-r", r, a, b, second])

    def test_the_full_arity_invocations_really_did_their_work(self):
        # Non-vacuity: an acceptance test that only read the exit code would
        # pass against a build in which the commands did nothing.
        r = self.roadmap
        first, second = str(self.tasks[0]), str(self.tasks[1])
        self.accept(["task", "prio", "-r", r, first, "7"])
        task = self.test.run_cmd_json(["task", "get", "-r", r, first])
        record = task[0] if isinstance(task, list) else task
        assert record["priority"] == 7, f"priority was not applied: {record!r}"

        self.accept(["sprint", "add-tasks", "-r", r, str(self.sprints[0]), f"{first},{second}"])
        members = self.test.run_cmd_json(["sprint", "tasks", "-r", r, str(self.sprints[0])])
        ids = {str(t["id"]) for t in members}
        assert {first, second} <= ids, f"add-tasks did not add both tasks: {members!r}"


class TestPositionalArityOutcomes:
    """A refused invocation must have done nothing at all."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_a_refused_roadmap_create_creates_neither_roadmap(self):
        code, stdout, stderr = self.test.run_cmd(
            ["roadmap", "create", "alpha-service", "beta-service"], check=False)
        assert code == 2, f"exit={code}, want 2; stderr={stderr!r}"
        assert stdout == "", f"a refused invocation wrote to stdout: {stdout!r}"

        roadmaps_dir = self.test.home_dir / ".roadmaps"
        for name in ("alpha-service", "beta-service"):
            assert not (roadmaps_dir / name).exists(), (
                f"the roadmap home for {name!r} exists after a refused create; "
                f"the defect created the first name and discarded the second")

        listed = self.test.run_cmd_json(["roadmap", "list"])
        names = {entry["name"] for entry in listed} if listed else set()
        assert not ({"alpha-service", "beta-service"} & names), (
            f"a refused create is visible in `roadmap list`: {listed!r}")

    def test_a_refused_task_remove_deletes_nothing(self):
        roadmap = self.test.create_roadmap("inventory-service")
        ids = [
            self.test.create_task(
                roadmap,
                "Reserve stock at checkout",
                "Oversold items generate refunds and complaints",
                "Reserve on checkout and release on abandonment",
                "A reserved item cannot be sold twice"),
            self.test.create_task(
                roadmap,
                "Backfill warehouse counts",
                "Counts drifted after the migration",
                "Import the audited counts and reconcile deltas",
                "Every SKU matches the audited count"),
        ]

        code, stdout, _stderr = self.test.run_cmd(
            ["task", "remove", "-r", roadmap, str(ids[0]), str(ids[1])], check=False)
        assert code == 2, f"exit={code}, want 2"
        assert stdout == ""

        surviving = {t["id"] for t in self.test.run_cmd_json(["task", "list", "-r", roadmap])}
        assert set(ids) <= surviving, (
            f"a refused `task remove` deleted rows: expected {ids} to survive, got {surviving}")

    def test_a_refused_mutation_writes_no_audit_entry(self):
        roadmap = self.test.create_roadmap("billing-service")
        task_id = self.test.create_task(
            roadmap,
            "Prorate mid-cycle plan changes",
            "Customers are billed a full cycle after upgrading mid-month",
            "Compute a prorated amount from the change timestamp",
            "A mid-cycle upgrade is billed pro rata")

        before = self.test.run_cmd_json(["audit", "list", "-r", roadmap])
        code, _stdout, _stderr = self.test.run_cmd(
            ["task", "prio", "-r", roadmap, str(task_id), "7", "unscheduled"], check=False)
        assert code == 2, f"exit={code}, want 2"
        after = self.test.run_cmd_json(["audit", "list", "-r", roadmap])

        assert len(after) == len(before), (
            f"the audit log grew from {len(before)} to {len(after)} entries after a refused "
            f"invocation; a refusal writes no audit entry")

    def test_the_refusal_precedes_opening_the_store(self):
        # The roadmap does not exist, so the invocation would fail with exit 4
        # on its own. Exit 2 is only possible if the refusal landed first.
        code, stdout, stderr = self.test.run_cmd(
            ["task", "remove", "-r", "roadmap-that-does-not-exist", "3", "4"], check=False)
        assert code == 2, (
            f"exit={code}, want 2; a refusal that followed the store open would exit 4; "
            f"stderr={stderr!r}")
        assert stdout == ""
        assert not (self.test.home_dir / ".roadmaps" / "roadmap-that-does-not-exist").exists()


class TestPositionalArityGlobalForms:
    """`help`, `--help`, `-h`, `version`, `--version`, `-v` — arity 0.

    These six resolve in cmd/rmp/main.go before any command lookup, so they
    never reach the shared enforcement point and are enforced at a second site.
    Measured before this work: `rmp version foo`, `rmp help foo` and
    `rmp --version foo` all exited 0 and discarded the token.
    """

    FORMS = ["help", "--help", "-h", "version", "--version", "-v"]

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_each_global_form_refuses_a_stray_positional_argument(self):
        for form in self.FORMS:
            code, stdout, stderr = self.test.run_cmd([form, "settlement-platform"], check=False)
            assert code == 2, (
                f"rmp {form} settlement-platform: exit={code}, want 2; "
                f"stdout={stdout!r} stderr={stderr!r}")
            first = stderr.splitlines()[0] if stderr else ""
            assert first == canonical_line("settlement-platform"), (
                f"rmp {form} settlement-platform: stderr first line={first!r}, "
                f"want {canonical_line('settlement-platform')!r}")
            assert stdout == "", (
                f"rmp {form} settlement-platform wrote to stdout: {stdout!r}")

    def test_each_global_form_still_works_on_its_own(self):
        # The other direction: the refusal must not have broken the six forms.
        for form in self.FORMS:
            code, stdout, stderr = self.test.run_cmd([form], check=False)
            assert code == 0, f"rmp {form}: exit={code}, want 0; stderr={stderr!r}"
            assert stdout.strip() != "", f"rmp {form} wrote nothing to stdout"

    def test_bare_rmp_still_prints_help(self):
        code, stdout, _stderr = self.test.run_cmd([], check=False)
        assert code == 0, f"bare `rmp`: exit={code}, want 0"
        assert "Usage:" in stdout, f"bare `rmp` did not print help: {stdout!r}"


class TestPositionalArityAlreadyCompliantFamilies:
    """The four families that already refused, one invocation of each.

    Three of them publish wording of their own, which the shared enforcement
    point defers to instead of overriding.
    """

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("observability-platform")
        self.task_id = self.test.create_task(
            self.roadmap,
            "Emit structured request logs",
            "Incident review cannot correlate requests across services",
            "Add a request id to every log line and propagate it",
            "A request can be followed across three services")

    def teardown_method(self):
        self.test.teardown()

    def test_graph_keeps_its_parenthetical_hint(self):
        code, stdout, stderr = self.test.run_cmd(
            ["graph", "query", "-r", self.roadmap, "MATCH (n:Incident) RETURN n"], check=False)
        assert code == 2, f"exit={code}, want 2; stderr={stderr!r}"
        assert stderr.splitlines()[0] == (
            'Error: invalid input: unexpected argument "MATCH (n:Incident) RETURN n" '
            '(graph queries use --query or stdin)'), stderr
        assert stdout == ""

    def test_web_keeps_its_colon_and_unquoted_token(self):
        code, stdout, stderr = self.test.run_cmd(
            ["web", "monitoring-dashboard", "--no-open"], check=False)
        assert code == 2, f"exit={code}, want 2; stderr={stderr!r}"
        assert stderr.splitlines()[0] == (
            "Error: invalid input: unexpected argument: monitoring-dashboard"), stderr
        assert stdout == ""

    def test_ai_help_keeps_its_own_line(self):
        code, stdout, stderr = self.test.run_cmd(["ai-help", "settlement"], check=False)
        assert code == 2, f"exit={code}, want 2; stderr={stderr!r}"
        assert stderr.splitlines()[0] == (
            "Error: ai-help accepts no positional arguments or flags other than --help"), stderr
        assert stdout == ""

    def test_the_comment_subcommands_keep_the_canonical_line(self):
        # The eight comment subcommands were closed by rmp task #184 and
        # already published the canonical wording, so the shared enforcement
        # point produces the same line they did.
        code, stdout, stderr = self.test.run_cmd(
            ["task", "comment-list", "-r", self.roadmap, str(self.task_id), "unscheduled"],
            check=False)
        assert code == 2, f"exit={code}, want 2; stderr={stderr!r}"
        assert stderr.splitlines()[0] == canonical_line("unscheduled"), stderr
        assert stdout == ""

    def test_the_comment_subcommands_still_report_a_missing_body(self):
        # A regression this rule can cause and must not: `--body` followed by
        # another flag has no value. Reading that flag as the body's value
        # would push its own operand into the positional count and refuse the
        # invocation as over-arity instead of naming the missing body.
        code, stdout, stderr = self.test.run_cmd(
            ["task", "comment-add", "-r", self.roadmap, str(self.task_id), "--body", "--type", "NOTE"],
            check=False)
        assert code == 2, f"exit={code}, want 2; stderr={stderr!r}"
        assert "required parameter missing: no comment body supplied" in stderr, stderr
        assert stdout == ""

    def test_the_comment_subcommands_still_treat_minus_one_as_a_flag(self):
        code, _stdout, stderr = self.test.run_cmd(
            ["task", "comment-add", "-r", self.roadmap, str(self.task_id), "-1",
             "--type", "NOTE", "--body", "Recorded during triage"], check=False)
        assert code == 2, f"exit={code}, want 2; stderr={stderr!r}"
        assert "unknown flag: -1" in stderr, (
            f"on the comment subcommands every '-'-prefixed token is a flag, digits "
            f"included; got {stderr!r}")


def _run_all():
    passed = 0
    failed = 0
    failures = []
    # Classes are DISCOVERED by inspecting this module, never listed, so a
    # suite added later cannot be silently skipped (rmp task #303).
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
    print(f"Positional arity tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
