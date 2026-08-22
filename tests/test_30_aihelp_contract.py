#!/usr/bin/env python3
"""
Test 30: AI Agent Contract surface (--ai-help)

End-to-end coverage of the machine-readable contract emitted by
`rmp --ai-help` / `rmp ai-help` against the compiled binary at
./bin/rmp. Enforces SPEC/COMMANDS.md § AI Help, SPEC/DATA_FORMATS.md
§ AI Agent Contract, and SPEC/HELP.md (banner, error hint, AI_AGENT
env var, deduplication).

Scenarios:
- Flag form at root / command / subcommand levels.
- ai-help command form (top-level alias).
- Scope filtering keeps top-level shape but narrows commands array.
- schema_version stability and presence of every required top-level
  field.
- Pretty-print formatting (2-space indent, trailing newline) and
  UTF-8 encoding.
- All 6 canonical workflows and all 12 canonical pitfalls present
  with required subfields.
- --help banner: first line is the SPEC literal across root, family,
  and subcommand help; banner NOT present in --ai-help JSON.
- AI_AGENT env var: hint prepended on success; absent when unset;
  absent when value is anything other than the literal "1".
- Error-path hint appended on every error; not appended on success;
  exit code preserved.
- Deduplication: AI_AGENT=1 + error path emits the hint exactly once
  (top), not twice.
- --ai-help mixed with action flags wins (no mutation occurs).
- --ai-help with an unknown command name exits 2.
"""

import json
import os
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


HINT_LINE = "AI agents: run `rmp --ai-help` for a machine-readable command contract."

REQUIRED_TOP_LEVEL_KEYS = frozenset([
    "schema_version",
    "tool",
    "conventions",
    "exit_codes",
    "enums",
    "global_flags",
    "commands",
    "common_workflows",
    "pitfalls",
])

REQUIRED_WORKFLOWS = frozenset([
    "bootstrap_new_project",
    "plan_next_sprint",
    "close_active_sprint_and_open_next",
    "reprioritise_backlog",
    "move_task_between_sprints",
    "complete_task_with_summary",
])

REQUIRED_PITFALLS = frozenset([
    "roadmap_identified_by_name",
    "manual_sprint_status",
    "delete_non_backlog_task",
    "add_tasks_to_closed_sprint",
    "next_without_open_sprint",
    "complete_with_open_dependencies",
    "summary_on_non_completed_transition",
    "partial_reorder",
    "non_iso_date_input",
    "assume_partial_batch_success",
    "invalid_roadmap_name",
    "parse_modification_stdout",
])


def _run_raw(cli_path: str, args, env_overrides=None, input_bytes=None):
    """Run the CLI binary and return (returncode, stdout_bytes, stderr_bytes).

    Bytes are returned (not decoded) so encoding tests can inspect raw
    output without losing information to early decoding.
    """
    env = os.environ.copy()
    if env_overrides:
        env.update(env_overrides)
    # AI_AGENT must default to unset for tests that assume no hint.
    if env_overrides is None or "AI_AGENT" not in env_overrides:
        env.pop("AI_AGENT", None)
    result = subprocess.run(
        [cli_path] + list(args),
        capture_output=True,
        env=env,
        input=input_bytes,
    )
    return result.returncode, result.stdout, result.stderr


class TestAIHelpContractShape:
    """Shape and field-coverage of the whole-CLI contract."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def test_flag_form_root_emits_valid_json(self):
        code, out, err = _run_raw(self.cli, ["--ai-help"])
        assert code == 0
        assert err == b""
        doc = json.loads(out)
        assert isinstance(doc, dict)
        print("✓ rmp --ai-help emits valid JSON on stdout, exit 0")

    def test_command_form_emits_identical_payload(self):
        _, out_flag, _ = _run_raw(self.cli, ["--ai-help"])
        _, out_cmd, _ = _run_raw(self.cli, ["ai-help"])
        assert out_flag == out_cmd, "rmp ai-help payload must equal rmp --ai-help"
        print("✓ rmp ai-help payload byte-identical to rmp --ai-help")

    def test_all_required_top_level_keys_present(self):
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)
        got = set(doc.keys())
        missing = REQUIRED_TOP_LEVEL_KEYS - got
        assert not missing, f"missing top-level keys: {sorted(missing)}"
        print(f"✓ all {len(REQUIRED_TOP_LEVEL_KEYS)} required top-level keys present")

    def test_schema_version_is_stable_string(self):
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)
        sv = doc.get("schema_version")
        assert isinstance(sv, str), f"schema_version must be string, got {type(sv).__name__}"
        # SPEC pins 1.0.0 today; bumping requires a SPEC change.
        assert sv == "1.0.0", f"schema_version regressed: got {sv!r}, expected '1.0.0'"
        print(f"✓ schema_version stable at {sv}")

    def test_tool_block_well_formed(self):
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        tool = json.loads(out).get("tool", {})
        for key in ("name", "display_name", "binary_version", "description"):
            assert key in tool, f"tool.{key} missing"
        assert tool["name"] == "rmp"
        print("✓ tool block contains name/display_name/binary_version/description")

    def test_conventions_documents_ai_agent_env_var(self):
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        conv = json.loads(out).get("conventions", {})
        env_doc = conv.get("ai_agent_env_var", {})
        assert env_doc.get("name") == "AI_AGENT"
        assert env_doc.get("enable_value") == "1", (
            f"contract must document only '1' enables; got {env_doc.get('enable_value')!r}"
        )
        print("✓ conventions.ai_agent_env_var = AI_AGENT/1 per SPEC")

    def test_pretty_printed_two_space_indent_and_trailing_newline(self):
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        text = out.decode("utf-8")
        assert text.endswith("\n"), "contract must end with trailing newline"
        lines = text.splitlines()
        assert lines[0] == "{"
        # The second line of a pretty-printed JSON object starts with two
        # spaces of indent for the first member.
        assert lines[1].startswith("  ") and not lines[1].startswith("   "), (
            f"second line must start with exactly 2 spaces; got {lines[1]!r}"
        )
        # Verify every nested-object opener uses a 2-space step (no
        # tabs, no 4-space drift). Walk lines that introduce a deeper
        # object/array and assert their indent is a multiple of 2.
        for line in lines:
            stripped = line.lstrip(" ")
            indent = len(line) - len(stripped)
            assert "\t" not in line, "contract must not contain tab characters"
            assert indent % 2 == 0, (
                f"non-2-space indent on line: {line!r} (indent={indent})"
            )
        # The contract MUST round-trip through a JSON parser without loss.
        doc = json.loads(text)
        assert isinstance(doc, dict) and doc, "parsed contract must be a non-empty object"
        print("✓ pretty-printed: 2-space indent, trailing newline, parses cleanly")

    def test_output_is_valid_utf8(self):
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        # decode() raises UnicodeDecodeError if not valid UTF-8.
        out.decode("utf-8")
        print("✓ contract bytes decode as valid UTF-8")

    def test_all_six_workflows_present_with_required_fields(self):
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        wfs = json.loads(out).get("common_workflows", [])
        names = {wf["name"] for wf in wfs}
        missing = REQUIRED_WORKFLOWS - names
        assert not missing, f"missing canonical workflows: {sorted(missing)}"
        for wf in wfs:
            assert "name" in wf and "steps" in wf, f"workflow {wf} missing required fields"
            assert isinstance(wf["steps"], list) and wf["steps"], (
                f"workflow {wf['name']} has empty steps"
            )
        print(f"✓ all 6 canonical workflows present ({sorted(names)})")

    def test_all_twelve_pitfalls_present_with_required_fields(self):
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        pfs = json.loads(out).get("pitfalls", [])
        ids = {p["id"] for p in pfs}
        missing = REQUIRED_PITFALLS - ids
        assert not missing, f"missing canonical pitfalls: {sorted(missing)}"
        for p in pfs:
            for key in ("id", "description", "wrong_example", "correct_example"):
                assert key in p, f"pitfall {p.get('id')!r} missing field {key!r}"
        print(f"✓ all 12 canonical pitfalls present with required subfields")

    def test_commands_list_non_empty_and_includes_known_commands(self):
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        cmds = json.loads(out).get("commands", [])
        names = {c.get("name") for c in cmds}
        for expected in ("task", "sprint", "roadmap", "backlog", "audit", "ai-help"):
            assert expected in names, f"commands array missing {expected!r}; got {sorted(names)}"
        print(f"✓ commands array carries all top-level commands ({sorted(names)})")


class TestAIHelpScopeFiltering:
    """Subcommand scope keeps top-level shape but narrows the tree."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def test_command_scope_returns_only_that_subtree(self):
        _, out, _ = _run_raw(self.cli, ["task", "--ai-help"])
        doc = json.loads(out)
        # Top-level shape must remain.
        missing = REQUIRED_TOP_LEVEL_KEYS - set(doc.keys())
        assert not missing, f"scoped contract dropped top-level keys: {sorted(missing)}"
        # commands must be narrowed to one entry (task).
        cmds = doc.get("commands", [])
        names = [c.get("name") for c in cmds]
        assert names == ["task"], f"scoped commands expected ['task'], got {names}"
        print("✓ rmp task --ai-help narrows commands to ['task'], keeps top-level shape")

    def test_subcommand_scope_returns_single_subcommand(self):
        _, out, _ = _run_raw(self.cli, ["task", "create", "--ai-help"])
        doc = json.loads(out)
        cmds = doc.get("commands", [])
        assert len(cmds) == 1 and cmds[0]["name"] == "task"
        subs = cmds[0].get("subcommands", [])
        sub_names = [s.get("name") for s in subs]
        assert sub_names == ["create"], (
            f"subcommand scope expected only ['create'], got {sub_names}"
        )
        print("✓ rmp task create --ai-help narrows to single subcommand")

    def test_schema_version_identical_across_scopes(self):
        _, out_root, _ = _run_raw(self.cli, ["--ai-help"])
        _, out_cmd, _ = _run_raw(self.cli, ["task", "--ai-help"])
        _, out_sub, _ = _run_raw(self.cli, ["task", "create", "--ai-help"])
        sv = lambda b: json.loads(b)["schema_version"]
        assert sv(out_root) == sv(out_cmd) == sv(out_sub) == "1.0.0"
        print("✓ schema_version identical across all scopes")


class TestAIHelpHelpBanner:
    """The --help banner is a leading line on every help output."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def test_root_help_first_line_is_banner(self):
        _, out, _ = _run_raw(self.cli, ["--help"])
        first = out.decode("utf-8").splitlines()[0]
        assert first == HINT_LINE, f"root --help first line: {first!r}"
        print("✓ rmp --help first line is SPEC banner")

    def test_family_help_first_line_is_banner(self):
        _, out, _ = _run_raw(self.cli, ["task", "--help"])
        first = out.decode("utf-8").splitlines()[0]
        assert first == HINT_LINE, f"task --help first line: {first!r}"
        print("✓ rmp task --help first line is SPEC banner")

    def test_subcommand_help_first_line_is_banner(self):
        _, out, _ = _run_raw(self.cli, ["task", "create", "--help"])
        first = out.decode("utf-8").splitlines()[0]
        assert first == HINT_LINE, f"task create --help first line: {first!r}"
        print("✓ rmp task create --help first line is SPEC banner")

    def test_banner_absent_from_ai_help_json(self):
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        text = out.decode("utf-8")
        assert HINT_LINE not in text, "banner must not appear in --ai-help JSON output"
        # Sanity: the JSON parses cleanly with no leading garbage.
        json.loads(text)
        print("✓ banner absent from --ai-help JSON output")

    def test_banner_absent_from_version(self):
        _, out, _ = _run_raw(self.cli, ["--version"])
        text = out.decode("utf-8")
        assert HINT_LINE not in text, "banner must not appear in --version output"
        print("✓ banner absent from --version output")


class TestAIAgentEnvVarHint:
    """AI_AGENT environment variable prepends the hint to stderr."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path
        self.roadmap = self.test.create_roadmap()

    def teardown_method(self):
        self.test.teardown()

    def test_env_unset_emits_no_hint_on_success(self):
        code, out, err = _run_raw(
            self.cli,
            ["task", "list", "-r", self.roadmap],
            env_overrides={"HOME": str(self.test.home_dir)},
        )
        assert code == 0
        assert HINT_LINE not in err.decode("utf-8")
        print("✓ AI_AGENT unset: no hint on successful command")

    def test_env_one_emits_hint_as_first_stderr_line(self):
        code, _, err = _run_raw(
            self.cli,
            ["task", "list", "-r", self.roadmap],
            env_overrides={"HOME": str(self.test.home_dir), "AI_AGENT": "1"},
        )
        assert code == 0
        text = err.decode("utf-8")
        assert text.startswith(HINT_LINE + "\n"), (
            f"AI_AGENT=1 must prepend hint as first stderr line; got: {text!r}"
        )
        print("✓ AI_AGENT=1: hint is first stderr line on success")

    def test_env_value_must_be_literal_one(self):
        # Per SPEC/HELP.md only the exact string "1" enables. Other
        # plausibly-truthy values are silent.
        for value in ("true", "TRUE", "yes", "0", "", "2", "on"):
            code, _, err = _run_raw(
                self.cli,
                ["task", "list", "-r", self.roadmap],
                env_overrides={"HOME": str(self.test.home_dir), "AI_AGENT": value},
            )
            assert code == 0, f"unexpected exit on AI_AGENT={value!r}"
            assert HINT_LINE not in err.decode("utf-8"), (
                f"AI_AGENT={value!r} must NOT emit hint; got: {err!r}"
            )
        print("✓ AI_AGENT=non-'1' values: hint suppressed (SPEC strict match)")


class TestAIHelpErrorPathHint:
    """Error stderr appends the hint after a blank line."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def test_error_stderr_appends_hint(self):
        code, _, err = _run_raw(
            self.cli,
            ["task", "get", "99999", "-r", "nonexistent_roadmap"],
            env_overrides={"HOME": str(self.test.home_dir)},
        )
        assert code != 0
        text = err.decode("utf-8")
        assert text.startswith("Error: "), f"error stderr must start with 'Error: '; got: {text!r}"
        assert HINT_LINE in text, "hint must be appended to stderr on error"
        # Hint must come after the Error line.
        err_idx = text.index("Error: ")
        hint_idx = text.index(HINT_LINE)
        assert hint_idx > err_idx, "hint must appear after the Error line"
        # A blank line must precede the hint.
        assert "\n\n" + HINT_LINE in text, "hint must be preceded by a blank line"
        print("✓ error path appends hint after blank line")

    def test_successful_command_no_hint(self):
        roadmap = self.test.create_roadmap()
        code, _, err = _run_raw(
            self.cli,
            ["task", "list", "-r", roadmap],
            env_overrides={"HOME": str(self.test.home_dir)},
        )
        assert code == 0
        assert HINT_LINE not in err.decode("utf-8"), (
            "successful command must not emit hint when AI_AGENT unset"
        )
        print("✓ successful command emits no hint when AI_AGENT unset")

    def test_error_path_preserves_exit_code(self):
        # Exit 4 = EXIT_NOT_FOUND per the contract.
        code, _, _ = _run_raw(
            self.cli,
            ["task", "get", "99999", "-r", "nonexistent_roadmap"],
            env_overrides={"HOME": str(self.test.home_dir)},
        )
        assert code == 4, f"expected exit 4 (not found), got {code}"
        print("✓ hint emission preserves the original exit code")


class TestAIHelpHintDeduplication:
    """AI_AGENT=1 + error path emits the hint exactly once."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def test_env_plus_error_emits_hint_exactly_once(self):
        code, _, err = _run_raw(
            self.cli,
            ["task", "get", "99999", "-r", "nonexistent_roadmap"],
            env_overrides={"HOME": str(self.test.home_dir), "AI_AGENT": "1"},
        )
        assert code != 0
        text = err.decode("utf-8")
        occurrences = text.count(HINT_LINE)
        assert occurrences == 1, (
            f"hint must appear exactly once with AI_AGENT=1 + error; got {occurrences} in: {text!r}"
        )
        # And the env-var path wins: hint at the top.
        assert text.startswith(HINT_LINE + "\n"), "with AI_AGENT=1, env hint must come first"
        print("✓ AI_AGENT=1 + error: hint emitted exactly once at top")


class TestAIHelpFlagPrecedence:
    """--ai-help wins over action flags and other arguments."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path
        self.roadmap = self.test.create_roadmap()

    def teardown_method(self):
        self.test.teardown()

    def test_ai_help_with_mutating_flags_does_not_mutate(self):
        # Combine --ai-help with `task create` action flags; nothing
        # must be persisted. Pre-count tasks, run, post-count tasks.
        before = self.test.run_cmd_json(["task", "list", "-r", self.roadmap])
        before_count = len(before) if isinstance(before, list) else 0

        code, out, _ = _run_raw(
            self.cli,
            [
                "task", "create",
                "-r", self.roadmap,
                "-t", "Should never persist",
                "-fr", "User-visible impact.",
                "-tr", "Technical steps.",
                "-ac", "Pass conditions.",
                "--ai-help",
            ],
            env_overrides={"HOME": str(self.test.home_dir)},
        )
        assert code == 0, f"--ai-help mixed with action flags must exit 0; got {code}"
        # stdout must be the contract.
        doc = json.loads(out)
        assert "schema_version" in doc, "stdout must be the AI contract JSON"

        after = self.test.run_cmd_json(["task", "list", "-r", self.roadmap])
        after_count = len(after) if isinstance(after, list) else 0
        assert after_count == before_count, (
            f"--ai-help must suppress mutation; tasks went {before_count} -> {after_count}"
        )
        print("✓ --ai-help with task create flags emits contract and does NOT create the task")


class TestAIHelpInvalidScopeExit:
    """--ai-help with an unknown command name exits 2."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def test_unknown_command_with_ai_help_exits_two(self):
        code, _, err = _run_raw(
            self.cli,
            ["definitely-not-a-command", "--ai-help"],
            env_overrides={"HOME": str(self.test.home_dir)},
        )
        assert code == 2, f"unknown command + --ai-help must exit 2; got {code}"
        assert err.decode("utf-8").startswith("Error: "), (
            "unknown command must emit 'Error: ' on stderr"
        )
        print("✓ rmp <unknown> --ai-help exits 2 with Error: on stderr")

    def test_ai_help_command_with_positional_exits_two(self):
        # SPEC: `ai-help` accepts no positional args; any extras -> exit 2.
        code, _, err = _run_raw(
            self.cli,
            ["ai-help", "stray-argument"],
            env_overrides={"HOME": str(self.test.home_dir)},
        )
        assert code == 2, f"ai-help with stray positional must exit 2; got {code}"
        assert err.decode("utf-8").startswith("Error: ")
        print("✓ rmp ai-help <stray> exits 2 with Error: on stderr")


class TestAIHelpWebBindContract:
    """Regression guard: the contract must report the `web` command's
    default bind host as loopback 127.0.0.1, never 0.0.0.0.

    The runtime default (internal/web/web.go: defaultHost = "127.0.0.1")
    and SPEC/WEB.md § Bind Address and Port Selection are canonical:
    `rmp web` binds loopback by default and --host 0.0.0.0 is the
    explicit network-exposure opt-in. The machine-readable contract in
    internal/commands/registry_web.go previously hard-coded 0.0.0.0,
    diverging from real behaviour and misleading AI agents into believing
    the server is network-exposed by default. These assertions pin the
    contract to the loopback truth so the divergence cannot reappear.
    """

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def _web_command(self, scope_args):
        _, out, _ = _run_raw(self.cli, scope_args)
        doc = json.loads(out)
        cmds = doc.get("commands", [])
        web = next((c for c in cmds if c.get("name") == "web"), None)
        assert web is not None, (
            f"contract for {scope_args} must include the 'web' command; "
            f"got {[c.get('name') for c in cmds]}"
        )
        return web

    def _action_node(self, web_cmd):
        # Per SPEC/DATA_FORMATS.md § Single-action commands, the web
        # command carries its action in a one-element subcommands array
        # (name repeats "web"); the action-level fields (stdout_on_success,
        # examples, ...) live on that subcommand, not the command object.
        # Fall back to the command object for the legacy flattened shape.
        subs = web_cmd.get("subcommands") or []
        if subs:
            return subs[0]
        return web_cmd

    def _host_flag(self, web_cmd):
        # The contract collapses web's single empty-name subcommand, so the
        # --host flag is published directly on the web command object. Fall
        # back to a subcommands tree if the shape ever changes.
        flag_sources = [web_cmd.get("flags", [])]
        for sub in web_cmd.get("subcommands", []):
            flag_sources.append(sub.get("flags", []))
        for flags in flag_sources:
            for flag in flags:
                if flag.get("long") == "--host":
                    return flag
        raise AssertionError("web command must declare a --host flag")

    def test_host_default_is_loopback_in_scoped_contract(self):
        web = self._web_command(["web", "--ai-help"])
        host_flag = self._host_flag(web)
        assert host_flag.get("default") == "127.0.0.1", (
            "web --host default must be loopback 127.0.0.1 (matches "
            "internal/web/web.go and SPEC/WEB.md); got "
            f"{host_flag.get('default')!r}"
        )
        print("✓ web --ai-help: --host default is loopback 127.0.0.1")

    def test_host_default_is_loopback_in_root_contract(self):
        web = self._web_command(["--ai-help"])
        host_flag = self._host_flag(web)
        assert host_flag.get("default") == "127.0.0.1", (
            "root contract web --host default must be 127.0.0.1; got "
            f"{host_flag.get('default')!r}"
        )
        print("✓ rmp --ai-help: web --host default is loopback 127.0.0.1")

    def test_host_default_is_never_all_interfaces(self):
        # The previous bug shipped 0.0.0.0 as the --host *default*. Guard
        # that specific field: 0.0.0.0 is only ever the explicit opt-in,
        # never the default the contract advertises.
        for scope in (["web", "--ai-help"], ["--ai-help"]):
            web = self._web_command(scope)
            default = str(self._host_flag(web).get("default"))
            assert default != "0.0.0.0", (
                f"{scope}: web --host default must not be 0.0.0.0 "
                "(that is the network-exposed opt-in, not the default)"
            )
            assert default == "127.0.0.1", (
                f"{scope}: web --host default must be loopback; got {default!r}"
            )
        print("✓ web --host default is never 0.0.0.0 in any scope")

    def test_default_stdout_url_is_loopback(self):
        # The example/stdout URL samples for the default invocation must
        # use the loopback address, not the all-interfaces address.
        web = self._web_command(["web", "--ai-help"])
        action = self._action_node(web)
        default_url = str(action.get("stdout_on_success", ""))
        # The default-success sample URL (not the explicit-opt-in examples)
        # must be loopback.
        assert "127.0.0.1:8787" in default_url or "host:port" in default_url, (
            f"web stdout_on_success sample must be loopback; got {default_url!r}"
        )
        assert "http://0.0.0.0" not in default_url, (
            "web default success URL must not be the all-interfaces address"
        )
        # Examples whose command is exactly `rmp web` or `rmp web --no-open`
        # (no --host) describe the default and must print the loopback URL.
        for ex in action.get("examples", []):
            cmd = ex.get("cmd", "")
            if "--host" not in cmd and ex.get("stdout"):
                assert "http://0.0.0.0" not in ex["stdout"], (
                    f"default-invocation example {cmd!r} must not print "
                    f"0.0.0.0; got {ex['stdout']!r}"
                )
                if "http://" in ex["stdout"]:
                    assert "127.0.0.1" in ex["stdout"], (
                        f"default-invocation example {cmd!r} must print the "
                        f"loopback URL; got {ex['stdout']!r}"
                    )
        print("✓ web default-invocation URLs are loopback 127.0.0.1")



class TestAIHelpContractBinaryVersion:
    """binary_version in the contract matches the version declared in main.go."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def test_binary_version_matches_main_go(self):
        """contract tool.binary_version must match the version string in cmd/rmp/main.go."""
        import re, pathlib
        main_go = pathlib.Path(self.cli).parent.parent / "cmd" / "rmp" / "main.go"
        text = main_go.read_text()
        m = re.search(r'version\s*=\s*"([^"]+)"', text)
        assert m, "could not parse version from cmd/rmp/main.go"
        main_version = m.group(1)

        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)
        contract_version = doc.get("tool", {}).get("binary_version")
        assert contract_version == main_version, (
            f"contract tool.binary_version {contract_version!r} "
            f"does not match cmd/rmp/main.go version {main_version!r}"
        )
        print(f"✓ contract tool.binary_version == {main_version!r} (matches main.go)")

    def test_contract_ends_with_trailing_newline(self):
        """The contract bytes must end with exactly one newline character."""
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        assert out.endswith(b"\n"), "contract must end with trailing newline"
        assert not out.endswith(b"\n\n"), (
            "contract must end with exactly ONE newline, not two"
        )
        print("✓ contract ends with exactly one trailing newline")


class TestAIHelpContractSingleActionCommands:
    """Single-action commands serialize as a one-element subcommands array."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def _find_command(self, doc, name):
        for cmd in doc.get("commands", []):
            if cmd.get("name") == name:
                return cmd
        return None

    def test_stats_is_single_element_subcommands_array(self):
        """stats command: subcommands must be a one-element list, not null."""
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)
        cmd = self._find_command(doc, "stats")
        assert cmd is not None, "stats command not in contract"
        subs = cmd.get("subcommands")
        assert subs is not None, "stats.subcommands must not be null"
        assert isinstance(subs, list), f"stats.subcommands must be a list, got {type(subs).__name__}"
        assert len(subs) == 1, (
            f"stats is a single-action command: subcommands must have exactly 1 element, got {len(subs)}"
        )
        print("✓ stats.subcommands is a one-element list (single-action contract shape)")

    def test_web_is_single_element_subcommands_array(self):
        """web command: subcommands must be a one-element list, not null."""
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)
        cmd = self._find_command(doc, "web")
        assert cmd is not None, "web command not in contract"
        subs = cmd.get("subcommands")
        assert subs is not None, "web.subcommands must not be null"
        assert isinstance(subs, list), f"web.subcommands must be a list, got {type(subs).__name__}"
        assert len(subs) == 1, (
            f"web is a single-action command: subcommands must have exactly 1 element, got {len(subs)}"
        )
        print("✓ web.subcommands is a one-element list (single-action contract shape)")

    def test_ai_help_is_single_element_subcommands_array(self):
        """ai-help command: subcommands must be a one-element list, not null."""
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)
        cmd = self._find_command(doc, "ai-help")
        assert cmd is not None, "ai-help command not in contract"
        subs = cmd.get("subcommands")
        assert subs is not None, "ai-help.subcommands must not be null"
        assert isinstance(subs, list), (
            f"ai-help.subcommands must be a list, got {type(subs).__name__}"
        )
        assert len(subs) == 1, (
            f"ai-help is a single-action command: subcommands must have exactly 1 element, "
            f"got {len(subs)}"
        )
        print("✓ ai-help.subcommands is a one-element list (single-action contract shape)")


class TestAIHelpContractEmptyArrayFields:
    """Empty array fields (subcommands, aliases, prerequisites) are [] never null."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def test_no_null_empty_array_fields(self):
        """Walk the entire contract; subcommands/aliases/prerequisites must be [] not null."""
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)

        null_paths = []

        def walk(obj, path):
            if isinstance(obj, dict):
                for k, v in obj.items():
                    if k in ("subcommands", "aliases", "prerequisites") and v is None:
                        null_paths.append(f"{path}.{k}")
                    walk(v, f"{path}.{k}")
            elif isinstance(obj, list):
                for i, v in enumerate(obj):
                    walk(v, f"{path}[{i}]")

        walk(doc, "")
        assert not null_paths, (
            f"The following fields must be [] instead of null: {null_paths}"
        )
        print("✓ no subcommands/aliases/prerequisites fields are null (all are [])")


class TestAIHelpContractRanges:
    """Flag range metadata is correct: no max:0, --max-tasks is 1-10000, --order is min-only."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def _collect_flags(self, doc):
        """Return list of (path, flag_dict) for every flag in the document."""
        flags = []

        def walk(obj, path):
            if isinstance(obj, dict):
                if "long" in obj and "type" in obj:
                    flags.append((path, obj))
                    return
                for k, v in obj.items():
                    walk(v, f"{path}.{k}")
            elif isinstance(obj, list):
                for i, v in enumerate(obj):
                    walk(v, f"{path}[{i}]")

        walk(doc, "")
        return flags

    def test_no_flag_has_range_max_zero(self):
        """No flag must emit range.max == 0 (indicates a missing or zero-initialised value)."""
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)
        bad = []
        for path, flag in self._collect_flags(doc):
            r = flag.get("range")
            if isinstance(r, dict) and r.get("max") == 0:
                bad.append((path, flag.get("long"), r))
        assert not bad, (
            f"Flags with range.max==0 (should be absent or positive): {bad}"
        )
        print("✓ no flag emits range.max == 0")

    def test_max_tasks_range_is_1_to_10000(self):
        """--max-tasks range must be {min:1, max:10000} wherever it appears."""
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)
        found = False
        for _, flag in self._collect_flags(doc):
            if flag.get("long") == "--max-tasks":
                found = True
                r = flag.get("range")
                assert isinstance(r, dict), f"--max-tasks range must be a dict, got {r!r}"
                assert r.get("min") == 1, f"--max-tasks range.min must be 1, got {r.get('min')!r}"
                assert r.get("max") == 10000, (
                    f"--max-tasks range.max must be 10000, got {r.get('max')!r}"
                )
        assert found, "--max-tasks flag not found in contract"
        print("✓ --max-tasks range is {min:1, max:10000}")

    def test_order_range_is_min_only(self):
        """--order range must have min:1 and must NOT have a max key."""
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)
        found = False
        for _, flag in self._collect_flags(doc):
            if flag.get("long") == "--order":
                found = True
                r = flag.get("range")
                assert isinstance(r, dict), f"--order range must be a dict, got {r!r}"
                assert r.get("min") == 1, f"--order range.min must be 1, got {r.get('min')!r}"
                assert "max" not in r, (
                    f"--order range must be min-only (no max key); got {r!r}"
                )
        assert found, "--order flag not found in contract"
        print("✓ --order range is min-only {min:1} with no max key")


class TestAIHelpContractConventions:
    """Contract conventions block is accurate."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def test_roadmap_flag_required_for_mentions_web(self):
        """conventions.roadmap_flag.required_for must mention 'web'."""
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)
        conv = doc.get("conventions", {})
        rf = conv.get("roadmap_flag", {})
        required_for = rf.get("required_for", "")
        assert "web" in str(required_for).lower(), (
            f"conventions.roadmap_flag.required_for must mention 'web'; "
            f"got {required_for!r}"
        )
        print("✓ conventions.roadmap_flag.required_for mentions 'web'")


class TestAIHelpContractExitExamples:
    """Every subcommand with non-zero exit codes has at least one non-zero example."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def test_every_nonzero_exit_subcommand_has_nonzero_example(self):
        """Subcommands that may exit non-zero must document it with an example.

        roadmap list is explicitly exempt: its only exit code is 0, so it
        need not (and must not) have a non-zero example.
        """
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)

        failures = []
        for cmd in doc.get("commands", []):
            cmd_name = cmd.get("name", "")
            for sub in cmd.get("subcommands", []) or []:
                sub_name = sub.get("name", "")
                exit_codes = sub.get("exit_codes", []) or []
                non_zero = [ec for ec in exit_codes if ec != 0]
                if not non_zero:
                    continue  # exempt (e.g. roadmap list with exit_codes == [0])
                examples = sub.get("examples", []) or []
                non_zero_ex = [ex for ex in examples if ex.get("exit", 0) != 0]
                if not non_zero_ex:
                    failures.append(
                        f"{cmd_name} {sub_name}: exit_codes={exit_codes} but no example has exit != 0"
                    )

        assert not failures, (
            "The following subcommands declare non-zero exit codes but have no matching "
            "example with a non-zero exit field:\n" + "\n".join(f"  - {f}" for f in failures)
        )
        print(
            "✓ every subcommand with non-zero exit codes has at least one non-zero example"
        )


class TestAIHelpAuditOperationMembers:
    """Rules 3 and 4 of SPEC/DATA_FORMATS.md § enums map entry, at binary level.

    Every value of the AuditOperation enum carries an `entity_type` member
    holding TASK or SPRINT: the value an audit entry's own entity_type field
    holds on a row carrying that operation. Without it an agent composing an
    `audit list` filter cannot tell whose history the filter returns, and the
    only thing left to infer it from is the operation's name.

    Every value also carries a `legacy` boolean: true on the four values no
    command writes, false on every other value. Both members appear ONLY where
    they apply: both are absent from the values of every other enum, following
    the convention the contract already uses where `commands[].flags[]` omits
    `range`, `min_length` and `max_length` rather than publishing them as null.
    """

    # The four values the catalogue accepts but no command writes.
    LEGACY_OPERATIONS = {"TASK_STATUS_CHANGE", "TASK_UPDATE", "SPRINT_UPDATE", "SPRINT_MOVE_TASK"}

    # The marking rule 2 puts at the head of a LEGACY value's own description.
    LEGACY_DESCRIPTION_PREFIX = "LEGACY."

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path

    def teardown_method(self):
        self.test.teardown()

    def _enums(self):
        _, out, _ = _run_raw(self.cli, ["--ai-help"])
        doc = json.loads(out)
        enums = doc["enums"]
        assert isinstance(enums, dict) and enums, "the contract publishes no enums map"
        return enums

    def test_every_audit_operation_value_carries_an_entity_type(self):
        """Present on every value, never null, never empty, always TASK or SPRINT."""
        enums = self._enums()
        values = enums["AuditOperation"]["values"]
        assert len(values) >= 40, f"only {len(values)} audit operations published"

        checked = 0
        for entry in values:
            name = entry["value"]
            assert "entity_type" in entry, (
                f"enums.AuditOperation {name} carries no entity_type member; an agent cannot tell "
                f"whose history `audit list --operation {name}` returns"
            )
            got = entry["entity_type"]
            assert got in ("TASK", "SPRINT"), (
                f"enums.AuditOperation {name} publishes entity_type {got!r}; the audit table's CHECK "
                f"admits exactly TASK and SPRINT"
            )
            checked += 1

        assert checked == len(values), (
            f"only {checked} of {len(values)} values were checked; a loop that stops matching reports "
            f"success while measuring nothing"
        )
        print(f"✓ all {checked} AuditOperation values publish an entity_type of TASK or SPRINT")

    def test_entity_type_appears_on_no_other_enum(self):
        """A TaskStatus value is not recorded against an entity at all.

        Absent is the right form rather than null: a key that was null on every
        enum but this one would suggest the contract has a general notion of an
        enum value's entity, which it does not.
        """
        enums = self._enums()
        others = 0
        for name, definition in enums.items():
            if name == "AuditOperation":
                continue
            others += 1
            for entry in definition["values"]:
                assert "entity_type" not in entry, (
                    f"enums.{name} value {entry['value']!r} carries an entity_type member; the member "
                    f"belongs to AuditOperation alone"
                )
        assert others >= 5, (
            f"only {others} enums other than AuditOperation were examined, so the 'appears only where "
            f"it applies' half of rule 3 measured almost nothing"
        )
        print(f"✓ entity_type is absent from the values of all {others} other enums")

    def test_entity_type_agrees_with_the_audit_rows_actually_written(self):
        """Rule 4: a declaration states what the writer writes, observed on a row.

        The classification is a claim about stored data. This drives a spread of
        mutating commands, reads the rows back out of the audit log, and
        requires the entity_type of every row to equal the entity_type the
        contract publishes for that row's operation.
        """
        enums = self._enums()
        declared = {v["value"]: v["entity_type"] for v in enums["AuditOperation"]["values"]}

        roadmap = self.test.create_roadmap()
        task = self.test.create_task(
            roadmap, "Prove the classification against real rows",
            "A declared entity type must match the writer",
            "Drive the writers and read the rows back",
            "Every observed row agrees with the contract",
        )
        blocker = self.test.create_task(
            roadmap, "Land the shared declaration first",
            "The classification has one home",
            "Declare it beside the operation constants",
            "Both surfaces render from it",
        )
        sprint_a = self.test.create_sprint(roadmap, "Classification rollout")
        sprint_b = self.test.create_sprint(roadmap, "Classification follow-up")

        self.test.run_cmd(["task", "edit", "-r", roadmap, str(task), "-t", "Prove it against real rows"])
        self.test.run_cmd(["task", "add-dep", "-r", roadmap, str(task), str(blocker)])
        self.test.run_cmd(["task", "remove-dep", "-r", roadmap, str(task), str(blocker)])
        self.test.run_cmd(["sprint", "update", "-r", roadmap, str(sprint_a), "-d", "Classification rollout, restated"])
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_a), str(task)])
        self.test.run_cmd(["sprint", "move-tasks", "-r", roadmap, str(sprint_a), str(sprint_b), str(task)])
        self.test.run_cmd(["sprint", "remove-tasks", "-r", roadmap, str(sprint_b), str(task)])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_a)])
        self.test.run_cmd([
            "task", "comment-add", "-r", roadmap, str(task),
            "--type", "NOTE", "-b", "Observed against a real audit row.",
        ])

        rows = self.test.run_cmd_json(["audit", "list", "-r", roadmap, "-l", "500"])
        assert len(rows) >= 12, f"only {len(rows)} audit rows were written, so the sweep did not run"

        seen = set()
        for row in rows:
            op = row["operation"]
            assert op in declared, f"the audit log holds {op}, which the contract does not publish"
            assert row["entity_type"] == declared[op], (
                f"a real audit row carrying {op} holds entity_type {row['entity_type']!r} but the "
                f"contract declares {declared[op]!r}; the declaration is a claim about stored data "
                f"and this row falsifies it"
            )
            seen.add(op)

        assert len(seen) >= 10, (
            f"only {len(seen)} distinct operations were observed; the sweep is no longer exercising a "
            f"spread of writers and this check has stopped proving much"
        )
        # Both entity types must be represented, or the check could pass with a
        # classification that collapsed onto one of them.
        entities = {row["entity_type"] for row in rows}
        assert entities == {"TASK", "SPRINT"}, (
            f"the observed rows cover {sorted(entities)}; a sweep that reaches only one entity cannot "
            f"detect a classification collapsed onto it"
        )
        print(f"✓ {len(seen)} operations observed on real rows agree with the published entity_type")


    def test_every_audit_operation_value_publishes_a_legacy_flag(self):
        """Rule 4: present on every value, a real boolean, never null.

        The false half is the one worth stating. The member is published from a
        pointer precisely so that false survives serialisation; a plain boolean
        with `omitempty` would drop the key from all 39 operations still in use
        and leave their status to be inferred from an absence, which is the
        inference the member exists to remove.
        """
        enums = self._enums()
        values = enums["AuditOperation"]["values"]

        trues, falses = set(), set()
        for entry in values:
            name = entry["value"]
            assert "legacy" in entry, (
                f"enums.AuditOperation {name} carries no legacy member; a consumer filtering for the "
                f"operations still in use would have to search the description prose for the word LEGACY"
            )
            got = entry["legacy"]
            assert isinstance(got, bool), (
                f"enums.AuditOperation {name} publishes legacy={got!r} ({type(got).__name__}); the "
                f"member is a boolean and is never null"
            )
            (trues if got else falses).add(name)

        assert trues == self.LEGACY_OPERATIONS, (
            f"the contract marks {sorted(trues)} as legacy; the four values no command writes are "
            f"{sorted(self.LEGACY_OPERATIONS)}"
        )
        assert falses, (
            "no value published legacy=false, so the member is being omitted on the operations still "
            "in use — exactly what a boolean with omitempty would do"
        )
        assert len(trues) + len(falses) == len(values), (
            f"{len(trues) + len(falses)} of {len(values)} values were classified"
        )
        print(f"✓ legacy published on all {len(values)} values: {len(trues)} true, {len(falses)} false")

    def test_legacy_flag_agrees_with_the_description_marking(self):
        """Rule 4: the member and the description state the same fact and must agree.

        They exist for different readers — the prefix is prose for a human, the
        member is a field for a machine — which is exactly why they can drift. A
        value marked in one and not the other tells a person and a program
        opposite things about whether the operation is still being recorded.
        """
        enums = self._enums()
        values = enums["AuditOperation"]["values"]

        compared = 0
        marked = 0
        for entry in values:
            name, flag, desc = entry["value"], entry["legacy"], entry["description"]
            has_prefix = desc.startswith(self.LEGACY_DESCRIPTION_PREFIX)
            if has_prefix:
                marked += 1
            assert flag == has_prefix, (
                f"enums.AuditOperation {name}: legacy={flag} but the description "
                f"{'opens' if has_prefix else 'does not open'} with "
                f"{self.LEGACY_DESCRIPTION_PREFIX!r}. One of the two is wrong, and a consumer reading "
                f"the other one is being misled.\n  description: {desc}"
            )
            compared += 1

        assert compared == len(values), f"{compared} of {len(values)} values were compared"
        assert 0 < marked < len(values), (
            f"{marked} of {len(values)} descriptions carry the LEGACY marking; with none or all of "
            f"them marked the agreement above holds without ever comparing both branches"
        )
        print(f"✓ the legacy member agrees with the description marking on all {compared} values")

    def test_legacy_flag_agrees_with_what_the_audit_log_records(self):
        """Rule 4 against real rows: nothing marked legacy is actually written.

        The two checks above compare the contract with itself. This one drives a
        spread of mutating commands and reads the audit log back: an operation
        published as legacy must not appear among the rows, and operations
        published as not legacy must. Without this half, a contract could be
        perfectly self-consistent and still describe a catalogue that moved on.
        """
        enums = self._enums()
        legacy_flags = {v["value"]: v["legacy"] for v in enums["AuditOperation"]["values"]}

        roadmap = self.test.create_roadmap()
        task = self.test.create_task(
            roadmap, "Publish the LEGACY marking as a field",
            "Agents should test a field, not grep a sentence",
            "Carry the marking in the same declaration as the entity type",
            "The member and the description agree",
        )
        sprint_a = self.test.create_sprint(roadmap, "Legacy marking rollout")
        sprint_b = self.test.create_sprint(roadmap, "Legacy marking follow-up")

        self.test.run_cmd(["task", "edit", "-r", roadmap, str(task), "-t", "Publish LEGACY as a field", "-p", "6"])
        self.test.run_cmd(["sprint", "update", "-r", roadmap, str(sprint_a), "-d", "Legacy marking rollout, restated"])
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_a), str(task)])
        self.test.run_cmd(["sprint", "move-tasks", "-r", roadmap, str(sprint_a), str(sprint_b), str(task)])
        self.test.run_cmd(["sprint", "remove-tasks", "-r", roadmap, str(sprint_b), str(task)])

        rows = self.test.run_cmd_json(["audit", "list", "-r", roadmap, "-l", "500"])
        assert len(rows) >= 10, f"only {len(rows)} audit rows were written, so the sweep did not run"

        written = {row["operation"] for row in rows}
        for op in sorted(written):
            assert op in legacy_flags, f"the audit log holds {op}, which the contract does not publish"
            assert legacy_flags[op] is False, (
                f"the sweep recorded {op}, which the contract publishes as legacy=true. A command writes "
                f"it again, so every consumer filtering for current activity is now skipping live rows"
            )

        assert len(written) >= 8, (
            f"only {len(written)} distinct operations were observed; the sweep is no longer exercising a "
            f"spread of writers"
        )

        # The other direction of the same fact: a legacy value stays an ACCEPTED
        # filter, which is the whole reason it is still published.
        for op in sorted(self.LEGACY_OPERATIONS):
            code, _, _ = self.test.run_cmd(["audit", "list", "-r", roadmap, "-o", op], check=False)
            assert code == 0, (
                f"`audit list --operation {op}` exited {code}; a value published as legacy is still an "
                f"accepted filter, so the older entries carrying it stay reachable"
            )
        print(f"✓ {len(written)} operations recorded by real commands, none of them published as legacy")

    def test_legacy_appears_on_no_other_enum(self):
        """The converse half of rule 4: no TaskStatus value is LEGACY.

        Publishing "legacy": false there would suggest the contract has a general
        notion of an enum value's LEGACY status, which it has not.
        """
        enums = self._enums()
        others = 0
        for name, definition in enums.items():
            if name == "AuditOperation":
                continue
            others += 1
            for entry in definition["values"]:
                assert "legacy" not in entry, (
                    f"enums.{name} value {entry['value']!r} carries a legacy member; the member belongs "
                    f"to AuditOperation alone"
                )
        assert others >= 5, f"only {others} other enums were examined"
        print(f"✓ legacy is absent from the values of all {others} other enums")


def _run_all():
    """Run every test class sequentially and report a summary."""
    suites = [
        TestAIHelpContractShape,
        TestAIHelpScopeFiltering,
        TestAIHelpHelpBanner,
        TestAIAgentEnvVarHint,
        TestAIHelpErrorPathHint,
        TestAIHelpHintDeduplication,
        TestAIHelpFlagPrecedence,
        TestAIHelpInvalidScopeExit,
        TestAIHelpWebBindContract,
        TestAIHelpContractBinaryVersion,
        TestAIHelpContractSingleActionCommands,
        TestAIHelpContractEmptyArrayFields,
        TestAIHelpContractRanges,
        TestAIHelpContractConventions,
        TestAIHelpContractExitExamples,
        TestAIHelpAuditOperationMembers,
    ]
    passed = 0
    failed = 0
    failures = []
    for suite_cls in suites:
        suite_name = suite_cls.__name__
        method_names = [m for m in dir(suite_cls) if m.startswith("test_")]
        for m in method_names:
            instance = suite_cls()
            try:
                instance.setup_method()
            except Exception as exc:
                failed += 1
                failures.append((f"{suite_name}.{m} (setup)", exc))
                continue
            try:
                getattr(instance, m)()
                passed += 1
            except AssertionError as exc:
                failed += 1
                failures.append((f"{suite_name}.{m}", exc))
            except Exception as exc:
                failed += 1
                failures.append((f"{suite_name}.{m}", exc))
            finally:
                try:
                    instance.teardown_method()
                except Exception:
                    pass
    print("\n" + "=" * 60)
    print(f"AI-help contract tests: {passed} passed, {failed} failed")
    print("=" * 60)
    if failures:
        for name, exc in failures:
            print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
