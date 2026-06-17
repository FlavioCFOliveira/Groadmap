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
   12. Every graph subcommand (create/query/update/delete/search) help
       contains "Output (stdout JSON):" and "-q" / "--query".
   13. No hard TAB character in any help output for any command.
"""

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
    "assign", "unassign", "subtasks",
    "add-dep", "remove-dep", "blockers", "blocking",
]
GRAPH_SUBS = ["create", "query", "update", "delete", "search"]


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
            ("graph", "query"),
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


def _run_all():
    import inspect

    suites = [
        TestBannerInvariantsBinary,
        TestEmpiricalExitCodes,
        TestHelpContentBinary,
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
