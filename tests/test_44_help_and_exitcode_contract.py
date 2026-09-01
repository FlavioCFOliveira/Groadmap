#!/usr/bin/env python3
"""
Test 44: Help output structure and empirical exit-code contract.

Validates that the compiled rmp binary honours the exit codes declared
in its own help text for a representative set of scenarios mandated by
the help/contract review (commits e901bbf, 8290fd0, 83ee2e6, 88136b6).

Coverage
--------
A. Banner invariants (E2E binary-level, complementing the Go unit tests):
   1.  Every family and a representative subcommand help starts with the SPEC
       banner as the first line.
   2.  Banner absent from rmp --ai-help, rmp --version.

B. Exit-code empirical verification (help says X → binary does X):
   3.  task get -r R abc  (non-integer id syntax)  → exit 2
   4.  sprint create with order collision           → exit 5
   5.  task stat <id> INVALID_STATUS               → exit 6 (regression guard)
   6.  task create --type INVALID_TYPE             → exit 6
   7.  task next with no open sprint               → exit 4
   8.  sprint tasks -s INVALID_STATUS              → exit 6

C. Help content structural checks (binary-level):
   9.  rmp sprint create --help and rmp sprint update --help mention
       --title, --description, --order, "CLOSED", "immutable".
   10. rmp sprint --help mentions exit code 5 (order collision).
   11. rmp sprint tasks --help mentions -s / --status.
   12. Every graph subcommand (execute) help
       contains "Output (stdout JSON):" and "-q" / "--query".
   13. No hard TAB character in any help output for any command.
   14. rmp sprint --help, rmp sprint create --help and rmp sprint update --help
       document the macro-goal semantics of the -d/--description flag.
   15. The --ai-help JSON contract carries the same --description semantics for
       both sprint create and sprint update.
"""

import json
import os
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase

BANNER_LINE = "AI agents: run `rmp --ai-help` for a machine-readable command contract."

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
# `rmp graph` publishes exactly one subcommand
# (SPEC/COMMANDS.md section "Graph Management"). The list is kept as a list
# because every use below iterates it, and because a second subcommand added
# tomorrow belongs here rather than in five places.
GRAPH_SUBS = ["execute"]

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

class TestBannerInvariantsBinary:
    """Binary-level banner checks (complement Go unit tests in banner_test.go)."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path
        self.home = str(self.test.home_dir)

    def teardown_method(self):
        self.test.teardown()

    def test_root_help_first_line_is_banner(self):
        _, out, _ = _run(self.cli, ["--help"], {"HOME": self.home})
        lines = out.splitlines()
        assert lines and lines[0] == BANNER_LINE, (
            f"rmp --help: first line must be SPEC banner; got {lines[0]!r}"
        )
        print("✓ rmp --help: first line is SPEC banner")

    def test_every_family_help_first_line_is_banner(self):
        """All top-level command families (except ai-help) start with the banner."""
        for cmd in ALL_COMMANDS:
            _, out, _ = _run(self.cli, [cmd, "--help"], {"HOME": self.home})
            lines = out.splitlines()
            assert lines and lines[0] == BANNER_LINE, (
                f"rmp {cmd} --help: first line must be SPEC banner; got {lines[0]!r}"
            )
        print(f"✓ all {len(ALL_COMMANDS)} family helps start with SPEC banner")

    def test_representative_subcommand_helps_first_line_is_banner(self):
        """A representative sample of subcommand helps start with the banner."""
        samples = [
            ("task", "create"),
            ("task", "list"),
            ("sprint", "create"),
            ("sprint", "tasks"),
            ("roadmap", "create"),
            ("backlog", "list"),
            ("audit", "history"),
            ("graph", "execute"),
        ]
        for family, sub in samples:
            _, out, _ = _run(self.cli, [family, sub, "--help"], {"HOME": self.home})
            lines = out.splitlines()
            assert lines and lines[0] == BANNER_LINE, (
                f"rmp {family} {sub} --help: first line must be SPEC banner; got {lines[0]!r}"
            )
        print(f"✓ all {len(samples)} sampled subcommand helps start with SPEC banner")

    def test_banner_second_line_is_blank(self):
        """After the banner the second line must be blank (exactly one blank line)."""
        for cmd in ["--help", "task --help", "sprint create --help"]:
            args = cmd.split()
            _, out, _ = _run(self.cli, args, {"HOME": self.home})
            lines = out.splitlines()
            assert len(lines) >= 2, f"rmp {cmd}: output has fewer than 2 lines"
            assert lines[1] == "", (
                f"rmp {cmd}: second line must be blank after banner; got {lines[1]!r}"
            )
        print("✓ banner is followed by exactly one blank line")

    def test_banner_absent_from_ai_help(self):
        code, out, _ = _run(self.cli, ["--ai-help"])
        assert code == 0
        assert BANNER_LINE not in out, "SPEC banner must not appear inside --ai-help JSON"
        print("✓ banner absent from --ai-help JSON output")

    def test_banner_absent_from_version(self):
        _, out, _ = _run(self.cli, ["--version"])
        assert BANNER_LINE not in out, "SPEC banner must not appear in --version output"
        print("✓ banner absent from --version output")


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
        """Every graph subcommand help has 'Output (stdout JSON):' and -q/--query."""
        for sub in GRAPH_SUBS:
            out = self._help(["graph", sub, "--help"])
            lower = out.lower()
            assert "output (stdout json)" in lower, (
                f"graph {sub} --help: missing 'Output (stdout JSON):' block"
            )
            assert "-q" in out, (
                f"graph {sub} --help: missing -q short form for --query"
            )
            assert "--query" in out, (
                f"graph {sub} --help: missing --query flag"
            )
        print(f"✓ all {len(GRAPH_SUBS)} graph subcommand helps: Output block and -q/--query")

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
        """Every help output contains an exit-codes block mentioning code 0."""
        subs_by_family = {
            "roadmap": ["list", "create", "remove"],
            "task": ["list", "create", "get", "next", "edit", "remove", "stat"],
            "sprint": ["list", "create", "update", "tasks", "stats"],
            "backlog": ["list", "show-next"],
            "audit": ["list", "history", "stats"],
            "graph": GRAPH_SUBS,
        }
        failures = []
        for family, subs in subs_by_family.items():
            for sub in subs:
                out = self._help([family, sub, "--help"])
                lower = out.lower()
                has_block = "exit code" in lower or "exit codes" in lower
                if not has_block:
                    failures.append(f"rmp {family} {sub} --help: missing exit-codes block")
                    continue
                # Verify code 0 appears after the heading.
                idx = lower.index("exit code")
                tail = out[idx:]
                if "0" not in tail:
                    failures.append(f"rmp {family} {sub} --help: exit-codes block missing code 0")

        assert not failures, (
            "Help outputs missing exit-codes block or code 0:\n"
            + "\n".join(f"  - {f}" for f in failures)
        )
        print("✓ every sampled help output contains an exit-codes block with code 0")


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
