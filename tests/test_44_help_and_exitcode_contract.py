#!/usr/bin/env python3
"""
Test 44: Help output structure and empirical exit-code contract.

Validates that the compiled rmp binary honours the exit codes declared
in its own help text for a representative set of scenarios mandated by
the help/contract review (commits e901bbf, 8290fd0, 83ee2e6, 88136b6).

Coverage
--------
A. Banner invariants (E2E binary-level, complementing the Go unit tests):
   1.  Every help -- the global help, every family help and every subcommand
       help the contract publishes -- carries the SPEC banner on the line
       immediately after its single Usage: line, followed by one blank line;
       the banner is never the first line.
   2.  The global help opens with `Groadmap v<version> - A CLI tool for
       managing technical roadmaps`, <version> being the version `rmp
       --version` prints and the contract publishes as tool.binary_version.
   3.  The recovery help after a dispatch failure is the stdout help of the
       same level with exactly the banner line removed.
   4.  Banner absent from rmp --ai-help, rmp --version.

B. Exit-code empirical verification (help says X → binary does X):
   5.  task get -r R abc  (non-integer id syntax)  → exit 2
   6.  sprint create with order collision           → exit 5
   7.  task stat <id> INVALID_STATUS               → exit 6 (regression guard)
   8.  task create --type INVALID_TYPE             → exit 6
   9.  task next with no open sprint               → exit 4
   10. sprint tasks -s INVALID_STATUS              → exit 6
   11. task edit -r R <id> with no field           → exit 0, no output, nothing
       changed, when the task exists; the help prose, the contract description
       and the contract's exit-0 condition all state that outcome, and none says
       "at least one". With no field, a task the roadmap does not hold and a
       roadmap that does not exist exit 4, and an invalid roadmap name exits 6,
       each with the line the same invocation prints when a field is supplied.

C. Help content structural checks (binary-level):
   12. rmp sprint create --help and rmp sprint update --help mention
       --title, --description, --order, "CLOSED", "immutable".
   13. rmp sprint --help mentions exit code 5 (order collision).
   14. rmp sprint tasks --help mentions -s / --status.
   15. Every graph subcommand (serve, client) help contains
       "Output (stdout JSON):"; client, the only one that takes a statement,
       also publishes "-q" / "--query", and serve publishes neither.
   16. No hard TAB character in any help output for any command.
   17. rmp sprint --help, rmp sprint create --help and rmp sprint update --help
       document the macro-goal semantics of the -d/--description flag.
   18. The --ai-help JSON contract carries the same --description semantics for
       both sprint create and sprint update.
"""

import json
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase
from tests.test_73_help_exit_codes_contract import GateError, compare_codes, read_exit_codes

BANNER_LINE = "AI agents usage: run `rmp --ai-help` for a machine-readable command contract."

# The title that opens the global help, with its version slot
# (SPEC/HELP.md section "Help structure template").
GLOBAL_TITLE_TEMPLATE = "Groadmap v{version} - A CLI tool for managing technical roadmaps"
GLOBAL_USAGE_LINE = "Usage: rmp [command] [subcommand] [arguments] [options]"

ALL_COMMANDS = [
    "roadmap",
    "task",
    "sprint",
    "backlog",
    "audit",
    "stats",
    "graph",
    "web",
]

SPRINT_SUBS = [
    "create", "get", "show", "update", "remove",
    "start", "close", "reopen",
    "tasks", "open-tasks", "stats",
    "add-tasks", "remove-tasks", "move-tasks",
    "reorder", "move-to", "swap", "top", "bottom",
]
TASK_SUBS = [
    "list", "create", "get", "next", "edit", "remove",
    "stat", "reopen", "prio", "sev",
    "subtasks",
    "add-dep", "remove-dep", "blockers", "blocking",
]
# `rmp graph` publishes exactly two subcommands, serve and client
# (SPEC/COMMANDS.md section "Graph Management"). `execute` was withdrawn: the
# graph is reachable only through a running server, spoken to by a client, so
# `rmp graph <anything else>` is now an unknown subcommand and exits 127. The
# list is kept as a list because every use below iterates it, and because a
# third subcommand added tomorrow belongs here rather than in five places.
GRAPH_SUBS = ["serve", "client"]

# The one graph subcommand that carries a Cypher statement. serve runs none of
# its own, so it publishes no --query at all, and the flag assertions below are
# keyed on this name rather than on the whole family.
GRAPH_STATEMENT_SUB = "client"

# Sentences that every surface documenting the sprint -d/--description flag
# must carry (plain-text help and the --ai-help JSON contract alike), per
# SPEC/HELP.md section "Sprint family help specifics" item 5.
DESCRIPTION_SEMANTICS_FRAGMENTS = [
    "high-level (macro) goal of the development effort the sprint delivers",
    "clear macro idea of what the sprint's tasks are specifically aimed at",
]


def _run(cli_path, args, env_overrides=None):
    env = os.environ.copy()
    env.pop("AI_AGENT", None)
    if env_overrides:
        env.update(env_overrides)
    r = subprocess.run([cli_path] + list(args), capture_output=True, env=env)
    stdout = r.stdout.decode("utf-8", errors="replace")
    stderr = r.stderr.decode("utf-8", errors="replace")
    return r.returncode, stdout, stderr


# ===========================================================================
# A. Banner invariants
# ===========================================================================

def _usage_indexes(lines):
    """Zero-based indexes of the lines that begin with "Usage:"."""
    return [i for i, line in enumerate(lines) if line.startswith("Usage:")]


def _assert_banner_after_usage(label, out, want_usage_index):
    """The placement rule of SPEC/HELP.md section "AI agent banner" on one help.

    Exactly one Usage: line, at want_usage_index; the banner on the next line;
    a blank line after the banner; the banner present once and never first.
    """
    lines = out.split("\n")
    usage = _usage_indexes(lines)
    assert len(usage) == 1, (
        f"{label}: want exactly one line beginning with 'Usage:', got {usage}\n{out[:400]}"
    )
    u = usage[0]
    assert u == want_usage_index, (
        f"{label}: the Usage: line is line {u + 1}, want line {want_usage_index + 1}"
    )
    assert len(lines) > u + 2, f"{label}: help ends before the banner and its blank line"
    assert lines[u + 1] == BANNER_LINE, (
        f"{label}: the line after Usage: must be the SPEC banner; got {lines[u + 1]!r}"
    )
    assert lines[u + 2] == "", (
        f"{label}: the line after the banner must be blank; got {lines[u + 2]!r}"
    )
    assert lines[0] != BANNER_LINE, f"{label}: the banner must not be the first line"
    assert out.count(BANNER_LINE) == 1, (
        f"{label}: the banner appears {out.count(BANNER_LINE)} times, want exactly 1"
    )


def _without_banner_line(help_text):
    """The help with exactly its banner line (and that line's newline) removed."""
    assert help_text.count(BANNER_LINE + "\n") == 1, "the help does not carry exactly one banner line"
    return help_text.replace(BANNER_LINE + "\n", "", 1)


class TestBannerInvariantsBinary:
    """Binary-level banner checks (complement Go unit tests in banner_test.go)."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path
        self.home = str(self.test.home_dir)

    def teardown_method(self):
        self.test.teardown()

    def _published_help_invocations(self):
        """Every `--help` invocation the contract publishes, except ai-help.

        Read from `rmp --ai-help` so the sweep cannot fall behind the CLI.
        ai-help is left out because its help token is intercepted by the
        contract emitter and prints JSON, not a plain-text help. Only the
        `--help` forms are driven: a bare `rmp web` would start the server.
        """
        code, out, err = _run(self.cli, ["--ai-help"], {"HOME": self.home})
        assert code == 0, f"rmp --ai-help exited {code}: {err}"
        contract = json.loads(out)
        invocations = []
        for cmd in contract["commands"]:
            if cmd["name"] == "ai-help":
                continue
            invocations.append([cmd["name"], "--help"])
            for sub in cmd.get("subcommands") or []:
                if sub.get("name"):
                    invocations.append([cmd["name"], sub["name"], "--help"])
        assert {inv[0] for inv in invocations} >= set(ALL_COMMANDS), (
            f"the contract no longer publishes every family: {sorted({inv[0] for inv in invocations})}"
        )
        assert len(invocations) >= 60, (
            f"only {len(invocations)} help invocations were derived; the sweep proves nothing"
        )
        return invocations

    def test_root_help_banner_follows_usage(self):
        """rmp --help, -h, help and bare rmp: banner is line 4, after Usage: on line 3."""
        for args in (["--help"], ["-h"], ["help"], []):
            code, out, err = _run(self.cli, args, {"HOME": self.home})
            label = "rmp " + " ".join(args)
            assert code == 0, f"{label}: exit {code}; stderr={err!r}"
            assert err == "", f"{label}: wrote to stderr: {err!r}"
            _assert_banner_after_usage(label, out, 2)
        print("✓ global help (4 forms): banner on line 4, after the Usage: line")

    def test_root_help_opens_with_versioned_title(self):
        """The first five lines of the global help, exactly, with the real version."""
        code, version_out, _ = _run(self.cli, ["--version"])
        assert code == 0
        m = re.fullmatch(r"Groadmap version (\S+) \(commit [^)]*\)\n", version_out)
        assert m, f"unexpected --version output: {version_out!r}"
        version = m.group(1)

        code, contract_out, _ = _run(self.cli, ["--ai-help"])
        assert code == 0
        assert json.loads(contract_out)["tool"]["binary_version"] == version, (
            "the contract's tool.binary_version disagrees with rmp --version"
        )

        _, out, _ = _run(self.cli, ["--help"], {"HOME": self.home})
        want = [
            GLOBAL_TITLE_TEMPLATE.format(version=version),
            "",
            GLOBAL_USAGE_LINE,
            BANNER_LINE,
            "",
            "Commands:",
        ]
        got = out.split("\n")[: len(want)]
        assert got == want, f"rmp --help opening lines:\n got: {got!r}\nwant: {want!r}"
        assert "commit" not in got[0], f"the title must carry the version alone: {got[0]!r}"
        print(f"✓ rmp --help opens with {want[0]!r}")

    def test_every_help_banner_follows_usage(self):
        """Every family and subcommand help: Usage: on line 1, banner on line 2."""
        invocations = self._published_help_invocations()
        for args in invocations:
            code, out, err = _run(self.cli, args, {"HOME": self.home})
            label = "rmp " + " ".join(args)
            assert code == 0, f"{label}: exit {code}; stderr={err!r}"
            assert out.startswith("Usage: rmp " + args[0]), (
                f"{label}: a family or subcommand help opens with its Usage: line; got {out[:80]!r}"
            )
            _assert_banner_after_usage(label, out, 0)
        print(f"✓ all {len(invocations)} family and subcommand helps carry the banner on line 2")

    def test_family_help_forms_agree(self):
        """rmp <family>, -h and help print the same bytes as rmp <family> --help.

        `web` and `stats` are leaf commands whose bare form is not a help
        request (`rmp web` starts the server), so only the families that
        dispatch subcommands are driven bare.
        """
        for family in ["roadmap", "task", "sprint", "backlog", "audit", "graph"]:
            _, reference, _ = _run(self.cli, [family, "--help"], {"HOME": self.home})
            for args in ([family], [family, "-h"], [family, "help"]):
                code, out, _ = _run(self.cli, args, {"HOME": self.home})
                assert code == 0 and out == reference, (
                    f"rmp {' '.join(args)}: output differs from rmp {family} --help"
                )
        print("✓ bare, -h and help family forms print the --help bytes")

    def test_recovery_help_is_stdout_help_minus_banner(self):
        """A dispatch failure writes the same level's help minus the banner line.

        The whole of stderr is asserted: error line, blank line, recovery help,
        blank line, hint. For an unresolved command the recovery help opens with
        the versioned title; at both levels the line after Usage: is blank.
        """
        cases = [(["reconciliacao-trimestral"], "Error: unknown command: reconciliacao-trimestral", ["--help"])]
        for family in ["roadmap", "task", "sprint", "backlog", "audit", "graph"]:
            cases.append((
                [family, "arquivar-trimestre"],
                f"Error: unknown {family} subcommand: arquivar-trimestre",
                [family, "--help"],
            ))
        for args, error_line, help_args in cases:
            label = "rmp " + " ".join(args)
            _, help_out, _ = _run(self.cli, help_args, {"HOME": self.home})
            recovery = _without_banner_line(help_out)
            code, out, err = _run(self.cli, args, {"HOME": self.home})
            assert code == 127, f"{label}: exit {code}, want 127"
            assert out == "", f"{label}: wrote to stdout: {out[:120]!r}"
            expected = error_line + "\n\n" + recovery + "\n" + BANNER_LINE + "\n\n"
            assert err == expected, (
                f"{label}: stderr is not error + blank + (help minus banner) + blank + hint\n"
                f" got: {err[:400]!r}\nwant: {expected[:400]!r}"
            )
            lines = recovery.split("\n")
            usage = _usage_indexes(lines)
            assert len(usage) == 1 and lines[usage[0] + 1] == "", (
                f"{label}: the line after Usage: in the recovery help must be blank"
            )
            if len(args) == 1:
                assert lines[0].startswith("Groadmap v") and lines[0] == help_out.split("\n")[0], (
                    f"{label}: the recovery help must open with the global title; got {lines[0]!r}"
                )
            else:
                assert lines[0].startswith(f"Usage: rmp {args[0]} "), (
                    f"{label}: the recovery help must open with the family Usage: line; got {lines[0]!r}"
                )
        print(f"✓ {len(cases)} dispatch failures: recovery help is the stdout help minus the banner line")

    def test_banner_absent_from_ai_help(self):
        for args in (["--ai-help"], ["ai-help"], ["task", "--ai-help"], ["sprint", "close", "--ai-help"]):
            code, out, err = _run(self.cli, args)
            assert code == 0, f"rmp {' '.join(args)}: exit {code}"
            assert BANNER_LINE not in out, f"rmp {' '.join(args)}: SPEC banner inside the JSON contract"
            assert BANNER_LINE not in err, f"rmp {' '.join(args)}: SPEC banner on stderr"
            json.loads(out)
        print("✓ banner absent from all four --ai-help forms")

    def test_banner_absent_from_version(self):
        for args in (["--version"], ["-v"], ["version"]):
            code, out, err = _run(self.cli, args)
            assert code == 0, f"rmp {args[0]}: exit {code}"
            assert BANNER_LINE not in out and BANNER_LINE not in err, (
                f"rmp {args[0]}: SPEC banner must not appear in version output"
            )
            assert len(out.splitlines()) == 1, f"rmp {args[0]}: version output is not one line: {out!r}"
        print("✓ banner absent from all three version forms")


# ===========================================================================
# B. Empirical exit-code contract verification
# ===========================================================================

class TestEmpiricalExitCodes:
    """Binary-level: help-declared exit codes match what the binary actually returns."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path
        self.home = str(self.test.home_dir)
        self.roadmap = self.test.create_roadmap()

    def teardown_method(self):
        self.test.teardown()

    def test_invalid_task_id_syntax_exits_2(self):
        """task get -r R abc (non-integer id) → exit 2 (misuse).

        The task get help documents exit 2 for invalid id syntax; the
        binary must honour this for non-numeric id tokens.
        """
        code, _, err = _run(
            self.cli,
            ["task", "get", "-r", self.roadmap, "abc"],
            {"HOME": self.home},
        )
        assert code == 2, (
            f"task get with non-integer id must exit 2 (misuse); got {code}, stderr={err!r}"
        )
        assert "task" in err.lower() and ("id" in err.lower() or "integer" in err.lower()), (
            f"stderr must mention task id/integer; got {err!r}"
        )
        print("✓ task get -r R abc → exit 2 (non-integer id syntax = misuse)")

    def test_sprint_order_collision_exits_5(self):
        """sprint create with duplicate --order → exit 5 (already exists).

        sprint create --help documents exit 5 for an --order value that
        is already in use; the binary must honour this.
        """
        # First sprint at order 1.
        self.test.run_cmd([
            "sprint", "create", "-r", self.roadmap,
            "-t", "Initial infrastructure sprint",
            "-d", "First sprint establishing core infrastructure",
            "--order", "1",
        ])
        # Second sprint with the same order must fail with exit 5.
        code, _, err = self.test.run_cmd(
            [
                "sprint", "create", "-r", self.roadmap,
                "-t", "Follow-up sprint",
                "-d", "Second sprint — should collide on order",
                "--order", "1",
            ],
            check=False,
        )
        assert code == 5, (
            f"sprint create with duplicate --order must exit 5 (already exists); "
            f"got {code}, stderr={err!r}"
        )
        print("✓ sprint create --order collision → exit 5 (ErrAlreadyExists)")

    def test_task_stat_invalid_status_exits_6(self):
        """task stat <id> INVALID_STATUS → exit 6 (invalid data).

        Regression guard: ParseTaskStatus previously returned an error
        that did not wrap utils.ErrValidation, causing the binary to
        exit 1 instead of the documented exit 6.
        """
        task_id = self.test.create_task(
            self.roadmap,
            title="Regression target: invalid status exit code",
            functional_requirements="task stat with an unrecognised status must exit 6",
            technical_requirements="ParseTaskStatus error must wrap utils.ErrValidation",
            acceptance_criteria="binary exits 6, not 1, on invalid status token",
        )
        code, _, err = self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, str(task_id), "DEFINITELY_NOT_A_STATUS"],
            check=False,
        )
        assert code == 6, (
            f"task stat with invalid status must exit 6 (invalid data); "
            f"got {code}, stderr={err!r}"
        )
        assert "validation" in err.lower() or "invalid" in err.lower(), (
            f"stderr must describe the validation error; got {err!r}"
        )
        print("✓ task stat INVALID_STATUS → exit 6 (regression: was exit 1)")

    def test_task_create_invalid_type_exits_6(self):
        """task create --type INVALID_TYPE → exit 6 (invalid data)."""
        code, _, err = self.test.run_cmd(
            [
                "task", "create", "-r", self.roadmap,
                "-t", "Should never be persisted",
                "-fr", "Validating --type rejection",
                "-tr", "An invalid --type token must be rejected before DB write",
                "-ac", "exit 6 on invalid type",
                "--type", "INVALID_TYPE",
            ],
            check=False,
        )
        assert code == 6, (
            f"task create --type INVALID_TYPE must exit 6 (invalid data); "
            f"got {code}, stderr={err!r}"
        )
        assert "type" in err.lower(), (
            f"stderr must mention 'type'; got {err!r}"
        )
        print("✓ task create --type INVALID_TYPE → exit 6")

    def test_task_next_no_open_sprint_exits_4(self):
        """task next with no open sprint → exit 4 (not found).

        There are no sprints in this roadmap, so task next cannot find
        an open sprint and must exit 4.
        """
        code, _, err = self.test.run_cmd(
            ["task", "next", "-r", self.roadmap],
            check=False,
        )
        assert code == 4, (
            f"task next with no open sprint must exit 4 (not found); "
            f"got {code}, stderr={err!r}"
        )
        assert "sprint" in err.lower() or "not found" in err.lower(), (
            f"stderr must mention sprint or not-found; got {err!r}"
        )
        print("✓ task next (no open sprint) → exit 4")

    def test_sprint_tasks_invalid_status_exits_6(self):
        """sprint tasks -s INVALID_STATUS → exit 6 (invalid data).

        The sprint tasks help documents exit 6 for invalid --status values
        and the sprint tasks --help shows the short form -s.
        """
        sprint_id = self.test.create_sprint(
            self.roadmap, "Feature delivery sprint"
        )
        code, _, err = self.test.run_cmd(
            [
                "sprint", "tasks", "-r", self.roadmap, str(sprint_id),
                "-s", "DEFINITELY_NOT_A_STATUS",
            ],
            check=False,
        )
        assert code == 6, (
            f"sprint tasks -s INVALID must exit 6 (invalid data); "
            f"got {code}, stderr={err!r}"
        )
        assert "status" in err.lower() or "invalid" in err.lower(), (
            f"stderr must describe the status error; got {err!r}"
        )
        print("✓ sprint tasks -s INVALID_STATUS → exit 6")

    def test_task_edit_with_no_field_exits_0_and_changes_nothing(self):
        """task edit -r R <id> with no field → exit 0, nothing changed (rmp task #491).

        SPEC/COMMANDS.md § Edit Task publishes the no-field invocation as a
        successful no-op that writes no audit entry. The help prose and the
        registry description once said "At least one option must be provided"
        while the binary accepted the invocation and the contract's exit-0
        condition said so. Every published surface is read from the binary:
        the help prose ahead of its `Exit codes:` block, and the contract's
        description and exit-0 condition for `task edit`. Each must state the
        no-field outcome in the published words, and none may say "at least
        one".
        """
        env = {"HOME": self.home}
        task_id = self.test.create_task(
            self.roadmap,
            "Reject expired JWT refresh tokens at the API gateway",
            "A refresh token past its expiry must be refused with HTTP 401",
            "Compare the exp claim against the gateway clock in UTC before minting",
            "An expired refresh token yields HTTP 401 and no new access token",
            priority=7,
            severity=5,
        )
        task_get = ["task", "get", "-r", self.roadmap, str(task_id)]
        audit_list = ["audit", "list", "-r", self.roadmap]
        task_before = self.test.run_cmd(task_get)[1]
        audit_before = self.test.run_cmd(audit_list)[1]

        code, out, err = _run(self.cli, ["task", "edit", "-r", self.roadmap, str(task_id)], env)
        assert code == 0, (
            f"task edit with no field must exit 0 (SPEC/COMMANDS.md § Edit Task, No-op); "
            f"got {code}, stderr={err!r}"
        )
        assert out == "", f"task edit with no field must write nothing to stdout; got {out!r}"
        assert err == "", f"task edit with no field must write nothing to stderr; got {err!r}"
        assert self.test.run_cmd(task_get)[1] == task_before, "task edit with no field changed the task"
        assert self.test.run_cmd(audit_list)[1] == audit_before, (
            "task edit with no field wrote to the audit log")

        # The two snapshots can see a change: an edit that supplies one field
        # moves both, so their equality above is not a comparison of outputs
        # that never differ.
        self.test.run_cmd(["task", "edit", "-r", self.roadmap, str(task_id), "-p", "2"])
        assert self.test.run_cmd(task_get)[1] != task_before, "the task snapshot missed a real edit"
        assert self.test.run_cmd(audit_list)[1] != audit_before, "the audit snapshot missed a real edit"

        stated = "supplying no field at all changes nothing, and is accepted only when the named task exists"
        code, help_out, help_err = _run(self.cli, ["task", "edit", "--help"], env)
        assert code == 0, f"rmp task edit --help exited {code}; stderr={help_err!r}"
        assert "\nExit codes:\n" in help_out, "rmp task edit --help carries no `Exit codes:` block"
        prose = help_out.split("\nExit codes:\n", 1)[0]

        code, contract_out, _ = _run(self.cli, ["--ai-help"], env)
        assert code == 0, f"rmp --ai-help exited {code}"
        edit = next(
            sub for command in json.loads(contract_out)["commands"] if command["name"] == "task"
            for sub in command["subcommands"] if sub["name"] == "edit"
        )
        exit_0 = [entry["conditions"] for entry in edit["exit_codes"] if entry["code"] == 0]
        assert len(exit_0) == 1, f"the task edit contract carries {len(exit_0)} exit-0 entries, want 1"

        surfaces = {
            "the task edit --help prose": prose,
            "the contract's task edit description": edit["description"],
            "the contract's task edit exit-0 condition": " ".join(exit_0[0]),
        }
        for surface, text in surfaces.items():
            flat = " ".join(text.split()).lower()
            assert stated in flat, (
                f"{surface} does not state the no-field outcome {stated!r}:\n{text}")
            assert "at least one" not in flat, (
                f"{surface} claims an option is required, which the binary contradicts:\n{text}")
        print("✓ task edit with no field → exit 0, nothing changed, and every surface says so")

    def test_task_edit_with_no_field_refuses_a_missing_task_exit_4(self):
        """task edit -r R <missing id> with no field → exit 4 (rmp task #492).

        SPEC/COMMANDS.md § Edit Task, criterion 6: a no-field edit looks the
        task up before the no-op, so a task the roadmap does not hold is refused
        with `Error: resource not found: task N not found`, nothing on stdout,
        and no audit entry. The binary once returned before opening the roadmap
        and exited 0 here, while the same invocation with a field exited 4.
        """
        env = {"HOME": self.home}
        task_id = self.test.create_task(
            self.roadmap,
            "Rotate the webhook signing secret without dropping deliveries",
            "Deliveries signed with the previous secret stay valid for 24 hours",
            "Keep both secrets in the verifier and retire the old one on a timer",
            "A delivery signed with either secret verifies during the overlap",
        )
        missing = task_id + 500
        audit_list = ["audit", "list", "-r", self.roadmap]
        audit_before = self.test.run_cmd(audit_list)[1]
        args = ["task", "edit", "-r", self.roadmap, str(missing)]

        code, out, err = _run(self.cli, args, env)
        want = f"Error: resource not found: task {missing} not found"
        assert code == 4, f"{args} must exit 4 (SPEC/COMMANDS.md § Edit Task); got {code}, stderr={err!r}"
        assert err.splitlines()[:1] == [want], f"{args}: stderr must open with {want!r}; got {err!r}"
        assert out == "", f"{args} must write nothing to stdout; got {out!r}"
        assert self.test.run_cmd(audit_list)[1] == audit_before, f"{args} wrote to the audit log"

        field_code, _, field_err = _run(self.cli, args + ["-p", "3"], env)
        assert (field_code, field_err.splitlines()[:1]) == (4, [want]), (
            f"the field path must refuse the same task with the same line; got {field_code}, {field_err!r}")

        # The audit snapshot can see a change: an edit of the existing task adds
        # an entry, so its equality above is not a comparison of outputs that
        # never differ.
        self.test.run_cmd(["task", "edit", "-r", self.roadmap, str(task_id), "-p", "3"])
        assert self.test.run_cmd(audit_list)[1] != audit_before, "the audit snapshot missed a real edit"
        print("✓ task edit with no field for a missing task → exit 4, the field path's line, nothing written")

    def test_task_edit_with_no_field_refuses_a_missing_roadmap_exit_4(self):
        """task edit -r <absent roadmap> <id> with no field → exit 4 (rmp task #492).

        SPEC/COMMANDS.md § Edit Task, criterion 7: the roadmap is resolved
        before the no-op, so a roadmap that does not exist is refused with
        `Error: resource not found: roadmap "X"` and nothing on stdout, and the
        refusal creates no roadmap.
        """
        env = {"HOME": self.home}
        absent = "billing-ledger-archive"
        roadmap_home = self.test.roadmaps_dir / absent
        assert not roadmap_home.exists(), f"{roadmap_home} exists before the test; the premise fails"
        args = ["task", "edit", "-r", absent, "12"]

        code, out, err = _run(self.cli, args, env)
        want = f'Error: resource not found: roadmap "{absent}"'
        assert code == 4, f"{args} must exit 4 (SPEC/COMMANDS.md § Edit Task); got {code}, stderr={err!r}"
        assert err.splitlines()[:1] == [want], f"{args}: stderr must open with {want!r}; got {err!r}"
        assert out == "", f"{args} must write nothing to stdout; got {out!r}"
        assert not roadmap_home.exists(), f"{args} created {roadmap_home}"
        print("✓ task edit with no field for a missing roadmap → exit 4, nothing created")

    def test_task_edit_with_no_field_refuses_an_invalid_roadmap_name_exit_6(self):
        """task edit -r <invalid name> <id> with no field → exit 6 (rmp task #492).

        SPEC/COMMANDS.md § Edit Task: a roadmap name that breaks § Roadmap Name
        Validation is refused with exit 6 and the line that section publishes,
        whether or not a field is supplied. A reserved name and a malformed one
        reach that section by different rules, so both are driven, and each
        no-field line is compared with the line of the same invocation carrying
        a field.
        """
        env = {"HOME": self.home}
        for name in ("con", "Bad Name!"):
            args = ["task", "edit", "-r", name, "12"]
            code, out, err = _run(self.cli, args, env)
            field_code, _, field_err = _run(self.cli, args + ["-p", "3"], env)

            assert field_code == 6, f"{args + ['-p', '3']} must exit 6; got {field_code}, stderr={field_err!r}"
            assert code == 6, f"{args} must exit 6 (SPEC/COMMANDS.md § Edit Task); got {code}, stderr={err!r}"
            assert err.splitlines()[:1] == field_err.splitlines()[:1], (
                f"{args}: the no-field line {err!r} differs from the field path's {field_err!r}")
            assert out == "", f"{args} must write nothing to stdout; got {out!r}"
        print("✓ task edit with no field for an invalid roadmap name (reserved, malformed) → exit 6")


# ===========================================================================
# C. Help content structural checks (binary-level)
# ===========================================================================

class TestHelpContentBinary:
    """Binary-level structural checks for help output content."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path
        self.home = str(self.test.home_dir)

    def teardown_method(self):
        self.test.teardown()

    def _help(self, args):
        _, out, _ = _run(self.cli, args, {"HOME": self.home})
        return out

    def test_sprint_create_help_documents_order_flags(self):
        """rmp sprint create --help must document --title, --description, --order."""
        out = self._help(["sprint", "create", "--help"])
        for flag in ("--title", "--description", "--order"):
            assert flag in out, (
                f"sprint create --help: missing flag {flag!r}"
            )
        lower = out.lower()
        assert "> 0" in lower or "positive" in lower, (
            "sprint create --help: --order must document the >0 constraint"
        )
        print("✓ sprint create --help: --title, --description, --order (with >0 rule)")

    def test_sprint_update_help_documents_order_immutability(self):
        """rmp sprint update --help must document --order CLOSED-immutable rule."""
        out = self._help(["sprint", "update", "--help"])
        for flag in ("--title", "--description", "--order"):
            assert flag in out, (
                f"sprint update --help: missing flag {flag!r}"
            )
        lower = out.lower()
        assert "closed" in lower, "sprint update --help: must mention CLOSED"
        assert "immutable" in lower, "sprint update --help: must mention 'immutable'"
        assert "> 0" in lower or "positive" in lower, (
            "sprint update --help: --order must document the >0 constraint"
        )
        print("✓ sprint update --help: --order CLOSED-immutable rule documented")

    def test_sprint_family_help_documents_exit_code_5(self):
        """rmp sprint --help must mention exit code 5 (order collision)."""
        out = self._help(["sprint", "--help"])
        lower = out.lower()
        has_5 = "exit 5" in lower or "exit code 5" in lower or "rejected exit 5" in lower
        assert has_5, (
            f"sprint --help: must document exit code 5 (order collision);\n{out}"
        )
        print("✓ sprint --help: documents exit code 5")

    def test_sprint_tasks_help_documents_status_short_form(self):
        """rmp sprint tasks --help must document -s / --status."""
        out = self._help(["sprint", "tasks", "--help"])
        assert "-s" in out, "sprint tasks --help: missing -s short form"
        assert "--status" in out, "sprint tasks --help: missing --status flag"
        print("✓ sprint tasks --help: -s, --status documented")

    def test_sprint_helps_document_description_macro_goal(self):
        """The -d/--description flag must be self-documenting on every sprint
        help surface: the family help, sprint create --help and
        sprint update --help must all state that the description carries the
        high-level (macro) goal of the development effort the sprint delivers.

        See SPEC/HELP.md section 'Sprint family help specifics' item 5 and
        SPEC/MODELS.md section 'Sprint Field Constraints'.
        """
        for argv in (["sprint", "--help"],
                     ["sprint", "create", "--help"],
                     ["sprint", "update", "--help"]):
            out = self._help(argv)
            label = "rmp " + " ".join(argv)
            assert "-d, --description" in out, (
                f"{label}: missing the -d, --description flag entry"
            )
            # The help printers wrap the sentence across aligned columns, so
            # match on whitespace-normalised text.
            normalized = " ".join(out.split())
            for fragment in DESCRIPTION_SEMANTICS_FRAGMENTS:
                assert fragment in normalized, (
                    f"{label}: -d, --description does not state its macro-goal "
                    f"semantics; missing {fragment!r}"
                )
        print("✓ sprint / sprint create / sprint update --help: --description macro-goal documented")

    def test_sprint_ai_help_description_flag_documents_macro_goal(self):
        """The --ai-help JSON contract must carry the same --description
        semantics for sprint create and sprint update, as a single-line string.
        """
        _, stdout, _ = _run(self.cli, ["--ai-help"], {"HOME": self.home})
        try:
            contract = json.loads(stdout)
        except json.JSONDecodeError as exc:
            raise AssertionError(
                f"rmp --ai-help did not return valid JSON: {exc}\n  stdout={stdout[:400]!r}"
            ) from exc

        sprint_cmd = next(
            (c for c in contract.get("commands", []) if c.get("name") == "sprint"),
            None,
        )
        assert sprint_cmd is not None, "sprint family missing from the --ai-help contract"

        for sub_name in ("create", "update"):
            sub = next(
                (s for s in sprint_cmd.get("subcommands", []) if s.get("name") == sub_name),
                None,
            )
            assert sub is not None, f"sprint {sub_name} missing from the --ai-help contract"

            flags = sub.get("flags", [])
            desc_flags = [f for f in flags if f.get("long") == "--description"]
            assert desc_flags, (
                f"sprint {sub_name}: --description missing from --ai-help flags; "
                f"flags present: {[f.get('long') for f in flags]}"
            )
            desc = desc_flags[0].get("description") or ""

            assert "\n" not in desc and "\r" not in desc, (
                f"sprint {sub_name}: --description contract text must be a single-line "
                f"string (no embedded newlines); got {desc!r}"
            )
            for fragment in DESCRIPTION_SEMANTICS_FRAGMENTS:
                assert fragment in desc, (
                    f"sprint {sub_name} --ai-help: flags[--description].description does "
                    f"not state its macro-goal semantics; missing {fragment!r}\n  got: {desc!r}"
                )
        print("✓ sprint create / sprint update --ai-help: --description macro-goal documented")

    def test_graph_subcommand_helps_have_output_block_and_query_short_form(self):
        """Every graph subcommand help has 'Output (stdout JSON):'; only the
        statement-carrying one publishes -q/--query.

        Both halves are owed. serve and client each write JSON to stdout -- the
        bound socket path for one, the statement result for the other -- so the
        Output block belongs on both. The query flag belongs to client alone:
        serve runs no statement of its own, and a --query advertised on it
        would offer back the one-shot form the family withdrew.
        """
        for sub in GRAPH_SUBS:
            out = self._help(["graph", sub, "--help"])
            lower = out.lower()
            assert "output (stdout json)" in lower, (
                f"graph {sub} --help: missing 'Output (stdout JSON):' block"
            )
            if sub == GRAPH_STATEMENT_SUB:
                assert "-q, --query" in out, (
                    f"graph {sub} --help: missing the -q short form beside --query"
                )
                assert "--query <cypher>" in out, (
                    f"graph {sub} --help: missing the --query flag and its argument"
                )
            else:
                assert "--query" not in out, (
                    f"graph {sub} --help: publishes a --query flag, but "
                    f"{GRAPH_STATEMENT_SUB} is the only subcommand that carries a statement"
                )
        print(f"✓ all {len(GRAPH_SUBS)} graph subcommand helps: Output block, "
              f"--query on {GRAPH_STATEMENT_SUB} alone")

    def test_no_hard_tab_in_any_help_output(self):
        """No help output for any command or subcommand must contain a hard TAB."""
        subs_by_family = {
            "roadmap": ["list", "create", "remove"],
            "task": TASK_SUBS,
            "sprint": SPRINT_SUBS,
            "backlog": ["list", "show-next"],
            "audit": ["list", "history", "stats"],
            "graph": GRAPH_SUBS,
        }
        tab_offenders = []

        # Family-level helps.
        for family in ALL_COMMANDS:
            _, out, _ = _run(self.cli, [family, "--help"], {"HOME": self.home})
            if "\t" in out:
                tab_offenders.append(f"rmp {family} --help")

        # Subcommand helps.
        for family, subs in subs_by_family.items():
            for sub in subs:
                _, out, _ = _run(self.cli, [family, sub, "--help"], {"HOME": self.home})
                if "\t" in out:
                    tab_offenders.append(f"rmp {family} {sub} --help")

        assert not tab_offenders, (
            "Hard TAB characters found in help outputs (use spaces):\n"
            + "\n".join(f"  - {o}" for o in tab_offenders)
        )
        print(f"✓ no hard TAB characters in any of the {len(ALL_COMMANDS) + sum(len(v) for v in subs_by_family.values())} help outputs checked")

    def test_every_help_output_contains_exit_codes_block(self):
        """Every sampled subcommand help carries one `Exit codes:` block listing
        exactly the codes its AI Agent Contract entry declares, code 0 among
        them as an entry of the block.

        SPEC/HELP.md § Exit codes requires the block, and § Agreement with the
        contract fixes what it lists: every code of the entry's `exit_codes`
        array, each once, and no code the array does not declare. The block is
        read by the shape that section publishes, through test_73's reader, so a
        `0` elsewhere in the help -- in an example, a range or a default -- is
        never taken for the entry of code 0.
        """
        contract = json.loads(self._help(["--ai-help"]))
        declared_by_subcommand = {
            (command["name"], sub["name"]): [entry["code"] for entry in sub["exit_codes"]]
            for command in contract["commands"]
            for sub in command["subcommands"]
        }
        subs_by_family = {
            "roadmap": ["list", "create", "remove"],
            "task": ["list", "create", "get", "next", "edit", "remove", "stat"],
            "sprint": ["list", "create", "update", "tasks", "stats"],
            "backlog": ["list", "show-next"],
            "audit": ["list", "history", "stats"],
            "graph": GRAPH_SUBS,
        }
        failures = []
        compared = 0
        for family, subs in subs_by_family.items():
            for sub in subs:
                label = f"rmp {family} {sub} --help"
                declared = declared_by_subcommand.get((family, sub))
                if declared is None:
                    failures.append(f"{label}: the contract publishes no `{family} {sub}` subcommand")
                    continue
                try:
                    listed = read_exit_codes(self._help([family, sub, "--help"]))
                except GateError as exc:
                    failures.append(f"{label}: {exc}")
                    continue
                compared += 1
                failures += compare_codes(label, listed, declared)
                if 0 not in listed:
                    failures.append(f"{label}: the block carries no entry for code 0")

        assert not failures, (
            "Help outputs whose exit-codes block disagrees with the contract:\n"
            + "\n".join(f"  - {f}" for f in failures)
        )
        sampled = sum(len(subs) for subs in subs_by_family.values())
        assert compared == sampled, (
            f"only {compared} of the {sampled} sampled helps were compared"
        )
        print(f"✓ all {compared} sampled help outputs list exactly their contract's exit codes, 0 included")


class TestAuditHelpClassificationBinary:
    """Binary-level checks on the two audit help surfaces.

    SPEC/HELP.md § Audit family help specifics binds exactly two surfaces, the
    `audit` family help and the `audit list` subcommand help, and
    § Audit operation entity-type classification rules 5(b) and 6 fix the shape
    of the operation block on the first of them. The Go gates in
    internal/commands render the block through a function call; these run the
    compiled binary, which is what a reader and an agent actually see.
    """

    # The four values the catalogue accepts but no command writes.
    LEGACY_OPERATIONS = ["TASK_STATUS_CHANGE", "TASK_UPDATE", "SPRINT_UPDATE", "SPRINT_MOVE_TASK"]

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path
        self.home = str(self.test.home_dir)

    def teardown_method(self):
        self.test.teardown()

    def _help(self, args):
        _, out, _ = _run(self.cli, args, {"HOME": self.home})
        return out

    def _operation_block(self, out):
        """Return the lines of the 'Valid operations' block, label lines included."""
        heading = "Valid operations (for --operation filter):"
        assert heading in out, f"`rmp audit --help` has no {heading!r} block"
        rest = out.split(heading, 1)[1].lstrip("\n")
        return [line for line in rest.split("\n\n", 1)[0].split("\n") if line.strip()]

    def _valid_operations(self):
        """Every value the contract publishes for the AuditOperation enum."""
        _, out, _ = _run(self.cli, ["--ai-help"], {"HOME": self.home})
        contract = json.loads(out)
        values = contract["enums"]["AuditOperation"]["values"]
        assert len(values) >= 40, f"the contract publishes only {len(values)} audit operations"
        return [v["value"] for v in values]

    def test_operation_block_groups_every_operation_under_an_entity_type(self):
        """Every group label names an entity type; every operation sits under one.

        Rule 5(b) forbids a catch-all group. A catch-all is what lets an
        operation nobody classified still be printed, under a heading that
        asserts nothing about it, while the block still lists everything the
        command accepts.
        """
        out = self._help(["audit", "--help"])
        lines = self._operation_block(out)

        labels = []
        placed = {}
        current = None
        for line in lines:
            stripped = line.strip()
            if ":" in stripped:
                label = stripped.split(":", 1)[0].strip()
                current = label
                if label not in labels:
                    labels.append(label)
            assert current is not None, (
                f"the block opens with a line under no group label: {line!r}"
            )
            column = line
            if ":" in column:
                column = column.rsplit(":", 1)[1]
            for token in column.replace(",", " ").split():
                placed[token] = current

        assert labels, "the 'Valid operations' block carries no group label at all"
        for label in labels:
            head = label.split(",")[0].strip()
            assert head in ("TASK", "SPRINT"), (
                f"group label {label!r} does not name an entity type. A group whose heading names no "
                f"entity is a catch-all wearing a name, and it is what rule 5(b) forbids"
            )

        published = self._valid_operations()
        for op in published:
            assert op in placed, (
                f"{op} is a published filter value but no group of `rmp audit --help` lists it; the "
                f"help publishes a subset of the catalogue"
            )
        for token in placed:
            assert token in published, (
                f"the block lists {token!r}, which `audit list --operation` does not accept"
            )
        assert len(placed) == len(published), (
            f"{len(placed)} tokens were attributed but {len(published)} operations are published; the "
            f"block scan has stopped matching and this check is measuring less than the catalogue"
        )
        print(f"✓ audit --help: {len(published)} operations grouped under {len(labels)} entity-type labels")

    def test_legacy_operations_are_grouped_and_explained(self):
        """The LEGACY values sit in their own labelled group, and the help says why.

        Rule 6 puts the marking on the GROUP LABEL and not beside each name: the
        list column of the block is checked on the basis that everything in it
        is a value the command accepts, and an inline `(LEGACY)` marker would
        put a token there that is not an operation.
        """
        out = self._help(["audit", "--help"])
        lines = self._operation_block(out)

        legacy_seen = {}
        current = None
        for line in lines:
            stripped = line.strip()
            if ":" in stripped:
                current = stripped.split(":", 1)[0].strip()
            column = line
            if ":" in column:
                column = column.rsplit(":", 1)[1]
            for token in column.replace(",", " ").split():
                if token in self.LEGACY_OPERATIONS:
                    legacy_seen[token] = current

        for op in self.LEGACY_OPERATIONS:
            assert op in legacy_seen, f"{op} is not listed in the operation block at all"
            assert "LEGACY" in legacy_seen[op], (
                f"{op} is printed under {legacy_seen[op]!r}, a label that does not say LEGACY, so a "
                f"reader cannot tell it from the operations still in use"
            )

        # Nothing else may be under a LEGACY label.
        current = None
        for line in lines:
            stripped = line.strip()
            if ":" in stripped:
                current = stripped.split(":", 1)[0].strip()
            if current is None or "LEGACY" not in current:
                continue
            column = line.rsplit(":", 1)[1] if ":" in line else line
            for token in column.replace(",", " ").split():
                assert token in self.LEGACY_OPERATIONS, (
                    f"{token} is printed under the LEGACY label {current!r} but a command still writes "
                    f"it, so the help tells readers to stop filtering on a live operation"
                )

        lowered = " ".join(out.split()).lower()
        assert "no command writes a legacy operation" in lowered, (
            "audit --help never states that no command writes the LEGACY values"
        )
        assert "remain filterable" in lowered, (
            "audit --help never states that the LEGACY values stay accepted so the older entries "
            "carrying them remain filterable"
        )
        print("✓ audit --help: LEGACY values grouped under their own label and explained")

    def test_both_bound_surfaces_explain_related_entity_id(self):
        """Rule 5 on both surfaces: the key names the operation's counterpart."""
        surfaces = {
            "rmp audit --help": self._help(["audit", "--help"]),
            "rmp audit list --help": self._help(["audit", "list", "--help"]),
        }
        assert surfaces["rmp audit --help"] != surfaces["rmp audit list --help"], (
            "the two surfaces produced identical output, so only one is being exercised"
        )
        for label, out in surfaces.items():
            flat = " ".join(out.split()).lower()
            assert "related_entity_id" in flat, f"{label}: never names related_entity_id"
            assert "counterpart" in flat, (
                f"{label}: explains related_entity_id without saying it names the COUNTERPART entity "
                f"of the operation, so the key reads as a duplicate of entity_id"
            )
            assert "null when the operation has no" in flat, (
                f"{label}: does not say the key is null when the operation has no counterpart"
            )
            assert "task_status_backlog" in flat and "sprint remove-tasks" in flat and "task stat" in flat, (
                f"{label}: omits the counter-example rule 5 requires, so the help still lets a reader "
                f"conclude that the operation name decides whether the key is set"
            )
            assert "legacy" in flat, f"{label}: no occurrence of LEGACY (rule 2)"
        print("✓ both audit help surfaces explain related_entity_id as the counterpart entity")

    def test_audit_list_schema_publishes_all_seven_keys(self):
        """The machine-readable schema of `audit list` names every audit-entry key.

        An agent reads stdout_on_success.schema instead of the help. Publishing
        five of the seven keys leaves commit_hash and related_entity_id
        undiscoverable from the contract.
        """
        _, out, _ = _run(self.cli, ["--ai-help"], {"HOME": self.home})
        contract = json.loads(out)
        keys = ["id", "operation", "entity_type", "entity_id", "performed_at",
                "related_entity_id", "commit_hash"]

        found = 0
        for cmd in contract["commands"]:
            if cmd["name"] != "audit":
                continue
            for sub in cmd["subcommands"]:
                if sub["name"] not in ("list", "history"):
                    continue
                found += 1
                schema = sub["stdout_on_success"]["schema"]
                if sub["name"] == "history" and "same shape as audit list" in schema:
                    # history publishes the shape by reference to list, which is
                    # checked in full on the very next iteration of this loop.
                    continue
                for k in keys:
                    assert k in schema, (
                        f"audit {sub['name']}: stdout_on_success.schema omits the key {k!r}, which is "
                        f"the only description of the response shape an agent gets.\n  schema: {schema}"
                    )
        assert found == 2, f"{found} of the 2 entry-returning audit subcommands were examined"
        print("✓ audit list/history publish all seven audit-entry keys in their schema")


def _run_all():
    import inspect

    suites = [
        TestBannerInvariantsBinary,
        TestEmpiricalExitCodes,
        TestHelpContentBinary,
        TestAuditHelpClassificationBinary,
    ]
    passed = 0
    failed = 0
    failures = []

    for cls in suites:
        cls_name = cls.__name__
        methods = sorted(m for m in dir(cls) if m.startswith("test_"))
        for m in methods:
            inst = cls()
            try:
                inst.setup_method()
            except Exception as exc:
                failed += 1
                failures.append((f"{cls_name}.{m} (setup)", exc))
                continue
            try:
                getattr(inst, m)()
                passed += 1
            except AssertionError as exc:
                failed += 1
                failures.append((f"{cls_name}.{m}", exc))
            except Exception as exc:
                failed += 1
                failures.append((f"{cls_name}.{m}", exc))
            finally:
                try:
                    inst.teardown_method()
                except Exception:
                    pass

    print("\n" + "=" * 60)
    print(f"Help/contract tests: {passed} passed, {failed} failed")
    print("=" * 60)
    if failures:
        for name, exc in failures:
            print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
