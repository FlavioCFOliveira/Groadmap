#!/usr/bin/env python3
"""
Test 79: `rmp skills install` refuses before any request, documents when to run
it, and is published in the AI Agent Contract (rmp task #642).

WHAT THIS MODULE COVERS, AND WHAT IT DELIBERATELY DOES NOT
----------------------------------------------------------
SPEC/SKILLS.md § Install Verification: the command reaches the network and, run
with sufficient privileges, writes the machine's managed settings directory, so
no end-to-end test runs the compiled binary past the refusals that precede the
download. This module therefore drives exactly those refusals against the
binary -- an unknown flag, a confirmation flag (`--yes`, `--force`), the roadmap
selector, a positional argument, an unresolved subcommand -- and asserts for
each the exit code, the exact published line, an empty stdout, and that nothing
was written: no staging directory in the temporary-file directory, no skills
directory under the user configuration directory, and no `.claude/` in the
working directory. It also reads the help and the contract the binary
publishes.

The download, the archive validation, the three tiers, the replacement, the
rollback and the signal handling are verified in process by
internal/skillsinstall/skillsinstall_test.go, against a local HTTPS server and
directories that test creates.

Every invocation here runs with HOME, the user configuration directory,
TMPDIR and the working directory all inside this module's own temporary
directory, so a regression that let one of them through to the download could
not write into the developer's real configuration.
"""

import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from base_test import resolve_cli  # noqa: E402

UNKNOWN_FLAG = "Error: invalid input: unknown flag: {}"
UNEXPECTED_ARGUMENT = 'Error: invalid input: unexpected argument "{}"'
SOURCE = "https://github.com/FlavioCFOliveira/Groadmap/archive/refs/heads/main.tar.gz"
CONFIG_VAR = "CLAUDE" + "_CONFIG_DIR"


class SkillsInstallFixture:
    """A private HOME, configuration directory, TMPDIR and working directory."""

    def setup_method(self):
        self.cli = resolve_cli()
        self.root = Path(tempfile.mkdtemp(prefix="groadmap_skills_"))
        self.home = self.root / "home" / "maria"
        self.config = self.root / "agent-config"
        self.tmp = self.root / "tmp"
        self.project = self.root / "checkout" / "payments-service"
        for d in (self.home, self.tmp, self.project):
            d.mkdir(parents=True)
        os.chmod(self.tmp, 0o700)

    def teardown_method(self):
        shutil.rmtree(self.root, ignore_errors=True)

    def run(self, args, config=True):
        env = os.environ.copy()
        env["HOME"] = str(self.home)
        env["TMPDIR"] = str(self.tmp)
        if config:
            env[CONFIG_VAR] = str(self.config)
        else:
            env.pop(CONFIG_VAR, None)
        r = subprocess.run([self.cli] + args, capture_output=True, text=True,
                           env=env, cwd=str(self.project), stdin=subprocess.DEVNULL,
                           timeout=60)
        return r.returncode, r.stdout, r.stderr

    def snapshot(self):
        """Every path under the fixture root, so a refusal is shown to write
        nothing anywhere the command could reach."""
        return sorted(str(p.relative_to(self.root)) for p in self.root.rglob("*"))

    def assert_refused(self, args, code, line, config=True):
        before = self.snapshot()
        rc, out, err = self.run(args, config=config)
        assert rc == code, f"rmp {' '.join(args)}: exit {rc}, want {code}; stderr {err!r}"
        first = err.splitlines()[0] if err else ""
        assert first == line, f"rmp {' '.join(args)}: stderr line {first!r}, want {line!r}"
        assert out == "", f"rmp {' '.join(args)}: a refusal wrote to stdout: {out!r}"
        after = self.snapshot()
        assert after == before, (
            f"rmp {' '.join(args)}: a refusal changed the filesystem: "
            f"{sorted(set(after) - set(before))}")
        assert not any(name.startswith("rmp-skills-install.") for name in os.listdir(self.tmp)), (
            "a staging directory was created before the refusal")
        assert not (self.project / ".claude").exists(), "a project skills directory was created"
        assert not self.config.exists(), "the user configuration directory was created"
        assert not (self.home / ".claude").exists(), "a skills directory was created under HOME"


class TestSkillsInstallRefusals(SkillsInstallFixture):
    """SPEC/COMMANDS.md § Skills Install Options: every flag but --help is
    unknown, and no positional argument is accepted; both refusals precede the
    staging directory and every request."""

    def test_confirmation_flags_are_unknown_flags(self):
        for flag in ("--yes", "--force", "-y"):
            self.assert_refused(["skills", "install", flag], 2, UNKNOWN_FLAG.format(flag))

    def test_any_other_flag_is_refused_as_itself(self):
        self.assert_refused(["skills", "install", "--dry-run"], 2, UNKNOWN_FLAG.format("--dry-run"))
        self.assert_refused(["skills", "install", "--scope=user"], 2, UNKNOWN_FLAG.format("--scope"))

    def test_the_roadmap_selector_is_not_accepted(self):
        self.assert_refused(["skills", "install", "-r", "payments-service"], 2, UNKNOWN_FLAG.format("-r"))
        self.assert_refused(["skills", "install", "--roadmap", "payments-service"], 2,
                            UNKNOWN_FLAG.format("--roadmap"))

    def test_a_positional_argument_is_refused(self):
        self.assert_refused(["skills", "install", "roadmap-manager"], 2,
                            UNEXPECTED_ARGUMENT.format("roadmap-manager"))
        self.assert_refused(["skills", "install", "knowledge-authority", "--yes"], 2,
                            UNEXPECTED_ARGUMENT.format("knowledge-authority"))

    def test_refusals_hold_without_a_configuration_directory_variable(self):
        self.assert_refused(["skills", "install", "--force"], 2, UNKNOWN_FLAG.format("--force"),
                            config=False)

    def test_an_unresolved_subcommand_is_a_dispatch_failure(self):
        before = self.snapshot()
        rc, out, err = self.run(["skills", "update"])
        assert rc == 127, f"exit {rc}; stderr {err!r}"
        assert err.splitlines()[0] == "Error: unknown skills subcommand: update", err
        assert "Usage: rmp skills <subcommand>" in err, "the family help is not the recovery help"
        assert out == ""
        assert self.snapshot() == before


class TestSkillsHelp(SkillsInstallFixture):
    """SPEC/HELP.md § Skills command help specifics."""

    def test_install_help_states_when_where_and_what(self):
        before = self.snapshot()
        rc, out, err = self.run(["skills", "install", "--help"])
        assert rc == 0 and err == "", f"exit {rc}; stderr {err!r}"
        lines = out.splitlines()
        assert lines[0] == "Usage: rmp skills install", lines[:2]
        assert lines[1].startswith("AI agents usage: run `rmp --ai-help`"), lines[:2]
        text = " ".join(out.split())
        for fragment in (
            # 1. When to run it, and when not to run it again.
            "after rmp has been installed or upgraded",
            "rmp-version a skill declares differs from the version `rmp --version` writes",
            "Run it once per such occasion",
            "rmp itself must be upgraded, so the command is not run again",
            # 2. What it replaces, and that it asks nothing.
            "the installed roadmap-manager and knowledge-authority, entirely, on every run",
            "It writes no prompt, reads nothing from standard input, and has no confirmation flag",
            "Every other skill is left untouched",
            # 3. Where it installs, in order, and no elevated privileges.
            "1. global the managed settings directory's .claude/skills",
            "2. user $" + CONFIG_VAR + "/skills, or ~/.claude/skills",
            "3. project .claude/skills in the current working directory",
            "reported with a warning on stderr",
            "never requests elevated privileges",
            # 4. Where it fetches from.
            "archive of the repository's main branch",
            "network access to github.com",
            "No checksum exists for that archive",
            # 5. No roadmap flag.
            "It does not take -r/--roadmap",
        ):
            assert fragment in text, f"the install help does not state {fragment!r}"
        assert '"scope": "global" | "user" | "project"' in out
        assert SOURCE in out
        for code in ("0 ", "1 ", "2 "):
            assert any(l.startswith("  " + code) for l in lines), f"exit code {code.strip()} missing"
        assert self.snapshot() == before, "the help wrote to the filesystem"

    def test_family_help_lists_install(self):
        for args in (["skills"], ["skills", "--help"], ["skills", "help"]):
            rc, out, err = self.run(args)
            assert rc == 0 and err == "", f"rmp {' '.join(args)}: exit {rc}; stderr {err!r}"
            assert out.startswith("Usage: rmp skills <subcommand>\n"), out[:80]
            assert "\n  install   Fetch the skills from the repository's main branch" in out, out
            assert "does not take\n-r/--roadmap" in out or "does not take -r/--roadmap" in out

    def test_global_help_lists_skills_among_commands_without_a_roadmap(self):
        rc, out, _ = self.run(["--help"])
        assert rc == 0
        assert "\n  skills           Install the agent skills" in out, out
        assert "'rmp skills'" in out, "the roadmap-flag line does not name rmp skills"


class TestSkillsInTheContract(SkillsInstallFixture):
    """SPEC/SKILLS.md acceptance criterion 20 and SPEC/DATA_FORMATS.md
    § AI Agent Contract."""

    def contract(self):
        rc, out, err = self.run(["--ai-help"])
        assert rc == 0, err
        return json.loads(out)

    def test_ai_help_lists_skills_install(self):
        contract = self.contract()
        entries = [c for c in contract["commands"] if c["name"] == "skills"]
        assert len(entries) == 1, [c["name"] for c in contract["commands"]]
        skills = entries[0]
        assert [s["name"] for s in skills["subcommands"]] == ["install"]
        assert skills["aliases"] == [] and skills["subcommands"][0]["aliases"] == []
        install = skills["subcommands"][0]
        for desc in (skills["description"], install["description"]):
            assert "after rmp has been installed or upgraded" in desc, desc
            assert "rmp-version a skill declares differs from the version `rmp --version` writes" in desc, desc
            assert "rmp itself must be upgraded, so the command is not run again" in desc, desc
        assert [f["long"] for f in install["flags"]] == ["--help"]
        assert install["idempotent"] is True
        assert len(install["prerequisites"]) == 1 and "github.com" in install["prerequisites"][0]
        assert install["side_effects"]["network"] != "None."
        assert SOURCE in install["side_effects"]["network"]
        assert [e["code"] for e in install["exit_codes"]] == [0, 1, 2]
        examples = {e["cmd"]: e for e in install["examples"]}
        assert examples["rmp skills install --yes"]["exit"] == 2
        assert examples["rmp skills install --yes"]["stderr"] == UNKNOWN_FLAG.format("--yes")

    def test_the_confirmation_flag_pitfall_is_published_and_true(self):
        pitfalls = {p["id"]: p for p in self.contract()["pitfalls"]}
        p = pitfalls["skills_install_confirmation_flag"]
        assert p["wrong_example"] == "rmp skills install --yes"
        assert p["wrong_exit"] == 2
        assert p["wrong_stderr"] == UNKNOWN_FLAG.format("--yes")
        assert p["correct_example"] == "rmp skills install"
        # The wrong example does what the pitfall says it does.
        self.assert_refused(p["wrong_example"].split()[1:], p["wrong_exit"], p["wrong_stderr"])

    def test_scoped_contract_of_skills_install(self):
        rc, out, err = self.run(["skills", "install", "--ai-help"])
        assert rc == 0, err
        scoped = json.loads(out)
        names = [(c["name"], [s["name"] for s in c["subcommands"]]) for c in scoped["commands"]]
        assert names == [("skills", ["install"])], names


def _run_all():
    passed = failed = 0
    failures = []
    module = sys.modules[__name__]
    classes = [obj for name, obj in sorted(vars(module).items())
               if isinstance(obj, type) and name.startswith("Test")]
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
    print(f"Skills install command tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if _run_all() else 1)
