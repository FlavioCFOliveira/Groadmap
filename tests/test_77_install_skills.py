#!/usr/bin/env python3
"""
Test 77: install-skills.sh, the agent-skills installer (rmp task #598).

SPEC/SKILLS.md § Skills Installer specifies a script that installs the
`roadmap-manager` and `knowledge-authority` skills of the latest release into
the invoking user's Claude Code skills directory, replacing each skill in full.
This module drives the real script end to end inside a hermetic sandbox, in the
same way test_47, test_49 and test_71 drive install.sh: a stubbed `curl` answers
the latest-release lookup and serves the release assets from local fixtures, and
PATH holds only the tools under test, so no network is touched.

The fixtures are real archives built here with the layout SPEC/SKILLS.md
§ Packaging prescribes (top-level `roadmap-manager/` and `knowledge-authority/`,
no `./`, no hidden entry) and real `sha256sum`-format checksums, so the
verification path runs against genuine digests.

What is pinned, by acceptance criterion of SPEC/SKILLS.md:

  11  a run with CLAUDE_CONFIG_DIR unset installs both skills under
      $HOME/.claude/skills/, exits 0 and ends with the published success line;
      with CLAUDE_CONFIG_DIR set to an absolute directory it installs under
      that directory's skills/ and writes nothing under $HOME/.claude/;
  12  a file of an older install that the new archive no longer ships is gone
      after the run, and a sibling skill directory is byte-identical;
  13  a skill path that is a symbolic link is refused with exit 1 before any
      release asset is requested, and is left exactly as it was (a skill path
      owned by another user cannot be produced without root and is not driven
      here; the same refusal branch is reached by a skill path that is not a
      directory, which is);
  14  a checksum mismatch, a missing checksum file, and an archive carrying a
      `..` component, an absolute name or a symbolic link each exit 1 and leave
      both installed skills byte-identical;
  15  a release that publishes no skills archive exits 1 with the published
      message and installs nothing;
  16  a failure injected at the second rename of the swap leaves both skills at
      their previous versions and exits 1 (and a rollback that itself fails
      keeps the work directory and names the copy it still holds);
  17  no exit path -- success, refusal, failure, or SIGTERM during the download
      and during the swap -- leaves the staging directory or the work directory
      behind, except the failed rollback;
  18  the script invokes no sudo, prompts for nothing, and has no flag that
      disables verification.
"""

import hashlib
import io
import os
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import uuid
from pathlib import Path

VERSION = "v9.9.9"
REPO_ROOT = Path(__file__).resolve().parent.parent
INSTALL_SKILLS_SH = REPO_ROOT / "install-skills.sh"
ARCHIVE_NAME = f"rmp-skills-{VERSION}.tar.gz"
SKILLS = ("roadmap-manager", "knowledge-authority")
SUCCESS_MESSAGE_TEMPLATE = "Installed roadmap-manager and knowledge-authority {version} in {skills_dir}"

# Tools install-skills.sh relies on. gzip is what GNU tar shells out to for -z.
REQUIRED_TOOLS = [
    "bash", "grep", "sed", "head", "mkdir", "rm", "cp", "cat", "basename",
    "tar", "gzip", "sha256sum", "mktemp", "ls", "find", "sleep",
]

# Serves the latest-release lookup and every release asset from local fixtures,
# and records each URL requested. FAKE_TERM_ON names a URL fragment on whose
# request the shim sends SIGTERM to the installer before answering.
CURL_SHIM = """#!/usr/bin/env bash
out=""; url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -*) shift ;;
    *)  url="$1"; shift ;;
  esac
done
echo "$url" >> "$FAKE_CURL_LOG"
if [ -n "${FAKE_TERM_ON:-}" ] && [[ "$url" == *"$FAKE_TERM_ON"* ]]; then
  kill -TERM "$PPID"
fi
if [[ "$url" == *api.github.com* ]]; then
  echo "{\\"tag_name\\": \\"${FAKE_VERSION}\\"}"
  exit 0
fi
fixture="${FAKE_FIXTURES}/$(basename "$url")"
[ -f "$fixture" ] || { echo "curl-shim: no fixture $fixture" >&2; exit 22; }
if [ -n "$out" ]; then cp "$fixture" "$out"; else cat "$fixture"; fi
exit 0
"""

# Wraps the real mv. A rename whose SOURCE ends with a pattern listed in
# FAKE_MV_FAIL (colon-separated) fails without moving anything; one whose source
# ends with FAKE_MV_TERM sends SIGTERM to the installer and then completes, so
# the signal lands while a swap is in progress.
MV_SHIM = """#!/usr/bin/env bash
src="$1"
IFS=':' read -r -a fails <<< "${FAKE_MV_FAIL:-}"
for f in "${fails[@]}"; do
  if [ -n "$f" ] && [[ "$src" == *"$f" ]]; then
    exit 1
  fi
done
if [ -n "${FAKE_MV_TERM:-}" ] && [[ "$src" == *"$FAKE_MV_TERM" ]]; then
  kill -TERM "$PPID"
fi
exec "$REAL_MV" "$@"
"""


def skill_files(tag):
    """A complete pair of skills whose every file carries `tag`."""
    return {
        "roadmap-manager/SKILL.md": f"---\nname: roadmap-manager\n---\nroadmap {tag}\n",
        "roadmap-manager/PDS.md": f"pds {tag}\n",
        "roadmap-manager/references/cli.md": f"cli {tag}\n",
        "knowledge-authority/SKILL.md": f"---\nname: knowledge-authority\n---\nka {tag}\n",
        "knowledge-authority/references/cypher.md": f"cypher {tag}\n",
    }


def build_archive(path, files, extra=None):
    """Write a gzip tar with every directory and file of `files`, as the release
    workflow packs skills/, plus any raw TarInfo entries in `extra`."""
    dirs = set()
    for name in files:
        parts = name.split("/")[:-1]
        for i in range(1, len(parts) + 1):
            dirs.add("/".join(parts[:i]))
    with tarfile.open(path, "w:gz", format=tarfile.GNU_FORMAT) as tf:
        for d in sorted(dirs):
            info = tarfile.TarInfo(d + "/")
            info.type = tarfile.DIRTYPE
            info.mode = 0o755
            tf.addfile(info)
        for name in sorted(files):
            data = files[name].encode()
            info = tarfile.TarInfo(name)
            info.size = len(data)
            info.mode = 0o644
            tf.addfile(info, io.BytesIO(data))
        for info, data in extra or []:
            tf.addfile(info, io.BytesIO(data) if data is not None else None)


def write_checksum(archive, name=None, digest=None):
    digest = digest or hashlib.sha256(archive.read_bytes()).hexdigest()
    target = archive.with_name(archive.name + ".sha256")
    target.write_text(f"{digest}  {name or archive.name}\n", encoding="utf-8")
    return target


def snapshot(root):
    """Map every path under root to its type and content, symlinks unfollowed."""
    out = {}
    if not root.exists() and not root.is_symlink():
        return out
    if root.is_symlink():
        return {".": ("link", os.readlink(root))}
    for dirpath, dirnames, filenames in os.walk(root):
        rel = os.path.relpath(dirpath, root)
        out[rel] = ("dir",)
        for f in filenames:
            p = Path(dirpath) / f
            if p.is_symlink():
                out[os.path.join(rel, f)] = ("link", os.readlink(p))
            else:
                out[os.path.join(rel, f)] = ("file", p.read_bytes())
        for d in list(dirnames):
            p = Path(dirpath) / d
            if p.is_symlink():
                out[os.path.join(rel, d)] = ("link", os.readlink(p))
                dirnames.remove(d)
    return out


class InstallSkillsSandbox:
    """A hermetic HOME, TMPDIR, PATH and release for one installer run."""

    def setup_method(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="rmp_skills_sim_"))
        self.bin = self.tmp / "bin"
        self.fixtures = self.tmp / "fixtures"
        self.home = self.tmp / "home"
        self.tmpdir = self.tmp / "tmp"
        self.curl_log = self.tmp / "curl.log"
        for d in (self.bin, self.fixtures, self.home, self.tmpdir):
            d.mkdir(parents=True)
        self.tmpdir.chmod(0o700)
        self.curl_log.write_text("")

        self.tag = uuid.uuid4().hex
        self.files = skill_files(self.tag)
        self.archive = self.fixtures / ARCHIVE_NAME
        build_archive(self.archive, self.files)
        write_checksum(self.archive)

        self._write_shim("curl", CURL_SHIM)
        self._write_shim("mv", MV_SHIM)
        for tool in REQUIRED_TOOLS:
            resolved = shutil.which(tool)
            assert resolved, f"the host lacks {tool}, which this module needs"
            (self.bin / tool).symlink_to(resolved)
        self.real_mv = shutil.which("mv")
        assert self.real_mv, "the host lacks mv"

    def teardown_method(self):
        for p in self.tmp.rglob("*"):
            try:
                if p.is_dir() and not p.is_symlink():
                    p.chmod(0o700)
            except OSError:
                pass
        shutil.rmtree(self.tmp, ignore_errors=True)

    def _write_shim(self, name, body):
        path = self.bin / name
        if path.exists() or path.is_symlink():
            path.unlink()
        path.write_text(body)
        path.chmod(path.stat().st_mode | stat.S_IXUSR)

    def _remove_tool(self, name):
        path = self.bin / name
        if path.exists() or path.is_symlink():
            path.unlink()

    def skills_dir(self, config_dir=None):
        return (Path(config_dir) if config_dir else self.home / ".claude") / "skills"

    def run(self, config_dir=None, extra_env=None, args=()):
        env = {
            "PATH": str(self.bin),
            "HOME": str(self.home),
            "TMPDIR": str(self.tmpdir),
            "FAKE_VERSION": VERSION,
            "FAKE_FIXTURES": str(self.fixtures),
            "FAKE_CURL_LOG": str(self.curl_log),
            "REAL_MV": self.real_mv,
        }
        if config_dir is not None:
            env["CLAUDE_CONFIG_DIR"] = str(config_dir)
        env.update(extra_env or {})
        return subprocess.run(
            [shutil.which("bash"), str(INSTALL_SKILLS_SH), *args],
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            env=env,
            timeout=60,
        )

    def install_previous(self, skills_dir, tag="previous"):
        """Lay down an older install of both skills, plus a file the release no
        longer ships, and return the snapshot of each skill path."""
        for name, content in skill_files(tag).items():
            p = skills_dir / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text(content)
        (skills_dir / "roadmap-manager" / "references" / "obsolete.md").write_text("gone in the new release\n")
        return {s: snapshot(skills_dir / s) for s in SKILLS}

    def requested_assets(self):
        return [u for u in self.curl_log.read_text().splitlines() if "/releases/download/" in u]

    def leftovers(self, skills_dir):
        staged = list(self.tmpdir.iterdir())
        work = [p for p in skills_dir.iterdir() if p.name.startswith(".rmp-skills-install.")] if skills_dir.is_dir() else []
        return staged, work

    def error_lines(self, result):
        return [line.split("ERROR:", 1)[1].split("\x1b[0m", 1)[-1].strip()
                for line in result.stderr.splitlines() if "ERROR:" in line]

    def assert_installed(self, skills_dir):
        for name, content in self.files.items():
            p = skills_dir / name
            assert p.is_file(), f"{p} was not installed"
            assert p.read_text() == content, f"{p} does not hold the release's content"
        for s in SKILLS:
            installed = {str(p.relative_to(skills_dir)) for p in (skills_dir / s).rglob("*") if p.is_file()}
            shipped = {n for n in self.files if n.startswith(s + "/")}
            assert installed == shipped, f"{s} holds {sorted(installed)}, the release ships {sorted(shipped)}"

    def assert_clean(self, skills_dir):
        staged, work = self.leftovers(skills_dir)
        assert staged == [], f"the staging directory was left behind: {staged}"
        assert work == [], f"the work directory was left behind: {work}"

    def assert_last_line_success(self, result, skills_dir):
        last = result.stderr.rstrip("\n").splitlines()[-1]
        expected = SUCCESS_MESSAGE_TEMPLATE.format(version=VERSION, skills_dir=skills_dir)
        assert "SUCCESS:" in last and last.endswith(expected), (
            f"the last line must be the success message {expected!r}; it is {last!r}\n{result.stderr}"
        )


class TestInstallSkillsSuccess(InstallSkillsSandbox):
    """Acceptance criteria 11, 12 and 17 on the success path."""

    def test_fresh_install_under_home(self):
        result = self.run()
        skills_dir = self.skills_dir()
        assert result.returncode == 0, f"exit {result.returncode}\n{result.stderr}"
        assert result.stdout == "", f"the installer must write nothing to stdout: {result.stdout!r}"
        self.assert_installed(skills_dir)
        self.assert_last_line_success(result, skills_dir)
        self.assert_clean(skills_dir)
        for created in (self.home / ".claude", skills_dir):
            mode = stat.S_IMODE(created.stat().st_mode)
            assert mode == 0o700, f"{created} was created with mode {oct(mode)}, not 0o700"

    def test_claude_config_dir_relocates_the_install(self):
        config_dir = self.tmp / "relocated" / "claude-config"
        result = self.run(config_dir=config_dir)
        skills_dir = self.skills_dir(config_dir)
        assert result.returncode == 0, f"exit {result.returncode}\n{result.stderr}"
        self.assert_installed(skills_dir)
        self.assert_last_line_success(result, skills_dir)
        assert not (self.home / ".claude").exists(), "a run with CLAUDE_CONFIG_DIR wrote under $HOME/.claude"
        for created in (self.tmp / "relocated", config_dir, skills_dir):
            mode = stat.S_IMODE(created.stat().st_mode)
            assert mode == 0o700, f"{created} was created with mode {oct(mode)}, not 0o700"
        self.assert_clean(skills_dir)

    def test_reinstall_replaces_each_skill_entirely_and_leaves_siblings_alone(self):
        skills_dir = self.skills_dir()
        self.install_previous(skills_dir)
        sibling = skills_dir / "other-skill"
        (sibling / "nested").mkdir(parents=True)
        (sibling / "SKILL.md").write_text("---\nname: other-skill\n---\nunrelated\n")
        (sibling / "nested" / "data.bin").write_bytes(os.urandom(256))
        (skills_dir / "loose-file.txt").write_text("not a skill\n")
        before_sibling = snapshot(sibling)
        before_loose = (skills_dir / "loose-file.txt").read_bytes()

        result = self.run()
        assert result.returncode == 0, f"exit {result.returncode}\n{result.stderr}"
        self.assert_installed(skills_dir)
        assert not (skills_dir / "roadmap-manager" / "references" / "obsolete.md").exists(), (
            "a file of the previous install that the release no longer ships survived the run"
        )
        assert snapshot(sibling) == before_sibling, "an unrelated skill was changed"
        assert (skills_dir / "loose-file.txt").read_bytes() == before_loose, "an unrelated entry was changed"
        self.assert_last_line_success(result, skills_dir)
        self.assert_clean(skills_dir)

    def test_a_second_run_at_the_same_release_replaces_again(self):
        first = self.run()
        assert first.returncode == 0, first.stderr
        marker = self.skills_dir() / "knowledge-authority" / "local-edit.md"
        marker.write_text("edited after install\n")
        second = self.run()
        assert second.returncode == 0, second.stderr
        assert not marker.exists(), "a run at the latest release must replace the skill all the same"
        assert len(self.requested_assets()) == 4, self.requested_assets()


class TestInstallSkillsVerification(InstallSkillsSandbox):
    """Acceptance criteria 14 and 15: nothing unverified or invalid is installed."""

    def _assert_refused_unchanged(self, result, before, fragment):
        skills_dir = self.skills_dir()
        assert result.returncode == 1, f"exit {result.returncode}\n{result.stderr}"
        assert any(fragment in line for line in self.error_lines(result)), (
            f"no ERROR line contains {fragment!r}:\n{result.stderr}"
        )
        for s in SKILLS:
            assert snapshot(skills_dir / s) == before[s], f"{s} changed although the run was refused"
        self.assert_clean(skills_dir)

    def test_checksum_mismatch(self):
        before = self.install_previous(self.skills_dir())
        write_checksum(self.archive, digest="0" * 64)
        self._assert_refused_unchanged(self.run(), before, "checksum mismatch")

    def test_missing_checksum_file(self):
        before = self.install_previous(self.skills_dir())
        (self.fixtures / (ARCHIVE_NAME + ".sha256")).unlink()
        self._assert_refused_unchanged(self.run(), before, "Failed to download the checksum")

    def test_checksum_naming_another_archive(self):
        before = self.install_previous(self.skills_dir())
        write_checksum(self.archive, name="rmp-skills-v0.0.1.tar.gz")
        self._assert_refused_unchanged(self.run(), before, "records no SHA-256 digest")

    def _rebuild(self, extra):
        build_archive(self.archive, self.files, extra=extra)
        write_checksum(self.archive)

    def test_archive_with_a_dotdot_component(self):
        before = self.install_previous(self.skills_dir())
        info = tarfile.TarInfo("roadmap-manager/../escaped.md")
        info.size = 3
        self._rebuild([(info, b"bad")])
        self._assert_refused_unchanged(self.run(), before, "'..' component")
        assert not (self.skills_dir() / "escaped.md").exists()

    def test_archive_with_an_absolute_name(self):
        before = self.install_previous(self.skills_dir())
        target = self.tmp / "absolute-target.md"
        info = tarfile.TarInfo(str(target))
        info.size = 3
        self._rebuild([(info, b"bad")])
        self._assert_refused_unchanged(self.run(), before, "absolute entry name")
        assert not target.exists()

    def test_archive_with_a_symbolic_link(self):
        before = self.install_previous(self.skills_dir())
        info = tarfile.TarInfo("knowledge-authority/references/link.md")
        info.type = tarfile.SYMTYPE
        info.linkname = str(self.home / ".bashrc")
        self._rebuild([(info, None)])
        self._assert_refused_unchanged(self.run(), before, "neither a regular file nor a directory")

    def test_archive_with_an_entry_outside_the_two_skills(self):
        before = self.install_previous(self.skills_dir())
        info = tarfile.TarInfo("./roadmap-manager/extra.md")
        info.size = 3
        self._rebuild([(info, b"bad")])
        self._assert_refused_unchanged(self.run(), before, "outside roadmap-manager/ and knowledge-authority/")

    def test_archive_missing_a_skill(self):
        before = self.install_previous(self.skills_dir())
        files = {k: v for k, v in self.files.items() if not k.startswith("knowledge-authority/")}
        build_archive(self.archive, files)
        write_checksum(self.archive)
        self._assert_refused_unchanged(self.run(), before, "does not contain the skill knowledge-authority/")

    def test_release_without_a_skills_archive(self):
        self.archive.unlink()
        result = self.run()
        assert result.returncode == 1, f"exit {result.returncode}\n{result.stderr}"
        expected = (f"release {VERSION} publishes no downloadable skills archive "
                    f"rmp-skills-{VERSION}.tar.gz. Nothing was installed.")
        assert expected in self.error_lines(result), f"missing {expected!r}:\n{result.stderr}"
        assert not (self.home / ".claude").exists(), "a refused run created the configuration directory"
        assert list(self.tmpdir.iterdir()) == [], "the staging directory was left behind"

    def test_no_flag_disables_verification(self):
        before = self.install_previous(self.skills_dir())
        write_checksum(self.archive, digest="f" * 64)
        result = self.run(args=("--no-verify", "--insecure", "--skip-checksum", "-k"))
        self._assert_refused_unchanged(result, before, "checksum mismatch")


class TestInstallSkillsDestinationRules(InstallSkillsSandbox):
    """Acceptance criterion 13 and § Destination Rules: refused before any asset."""

    def _assert_refused_before_any_request(self, result, fragment):
        assert result.returncode == 1, f"exit {result.returncode}\n{result.stderr}"
        assert any(fragment in line for line in self.error_lines(result)), (
            f"no ERROR line contains {fragment!r}:\n{result.stderr}"
        )
        assert self.curl_log.read_text() == "", (
            f"a refused destination must be refused before anything is fetched: {self.curl_log.read_text()!r}"
        )
        assert list(self.tmpdir.iterdir()) == [], "a staging directory was created"

    def test_skill_path_that_is_a_symbolic_link(self):
        skills_dir = self.skills_dir()
        skills_dir.mkdir(parents=True)
        target = self.tmp / "elsewhere"
        target.mkdir()
        (target / "keep.md").write_text("must survive\n")
        link = skills_dir / "knowledge-authority"
        link.symlink_to(target)
        result = self.run()
        self._assert_refused_before_any_request(result, "is a symbolic link")
        assert link.is_symlink() and os.readlink(link) == str(target), "the link was changed"
        assert (target / "keep.md").read_text() == "must survive\n", "the link's target was changed"

    def test_dangling_symbolic_link_skill_path(self):
        skills_dir = self.skills_dir()
        skills_dir.mkdir(parents=True)
        link = skills_dir / "roadmap-manager"
        link.symlink_to(self.tmp / "nowhere")
        result = self.run()
        self._assert_refused_before_any_request(result, "is a symbolic link")
        assert link.is_symlink(), "the dangling link was removed"

    def test_skill_path_that_is_not_a_directory(self):
        skills_dir = self.skills_dir()
        skills_dir.mkdir(parents=True)
        (skills_dir / "roadmap-manager").write_text("a file, not a skill\n")
        result = self.run()
        self._assert_refused_before_any_request(result, "is not a directory")
        assert (skills_dir / "roadmap-manager").read_text() == "a file, not a skill\n"

    def test_skills_dir_that_is_a_file(self):
        (self.home / ".claude").mkdir()
        (self.home / ".claude" / "skills").write_text("x")
        self._assert_refused_before_any_request(self.run(), "is not a directory")

    def test_world_writable_skills_dir_without_sticky_bit(self):
        skills_dir = self.skills_dir()
        skills_dir.mkdir(parents=True)
        skills_dir.chmod(0o777)
        try:
            self._assert_refused_before_any_request(self.run(), "writable by every user")
        finally:
            skills_dir.chmod(0o700)

    def test_relative_claude_config_dir(self):
        result = self.run(config_dir="relative/claude")
        self._assert_refused_before_any_request(result, "CLAUDE_CONFIG_DIR must be an absolute path")
        assert not (self.home / ".claude").exists()

    def test_relative_home_without_claude_config_dir(self):
        result = self.run(extra_env={"HOME": "relative-home"})
        self._assert_refused_before_any_request(result, "HOME is unset, empty or not an absolute path")

    def test_missing_tools_refused_before_any_request(self):
        cases = [
            ("curl", "Neither curl nor wget is available. Please install one of them."),
            ("sha256sum", "sha256sum (GNU coreutils), shasum (Perl Digest::SHA) or openssl"),
            ("mktemp", "mktemp is required"),
            ("tar", "tar is required"),
        ]
        for tool, fragment in cases:
            self._remove_tool(tool)
            self._assert_refused_before_any_request(self.run(), fragment)
            if tool == "curl":
                self._write_shim("curl", CURL_SHIM)
            else:
                (self.bin / tool).symlink_to(shutil.which(tool))


class TestInstallSkillsSwapAndSignals(InstallSkillsSandbox):
    """Acceptance criteria 16 and 17: rollback, signals and cleanup."""

    def test_failure_at_the_second_rename_rolls_back(self):
        skills_dir = self.skills_dir()
        before = self.install_previous(skills_dir)
        result = self.run(extra_env={"FAKE_MV_FAIL": "/new/knowledge-authority"})
        assert result.returncode == 1, f"exit {result.returncode}\n{result.stderr}"
        assert any("failed to move the new knowledge-authority" in line for line in self.error_lines(result)), result.stderr
        for s in SKILLS:
            assert snapshot(skills_dir / s) == before[s], f"{s} is not at its previous version after the rollback"
        self.assert_clean(skills_dir)

    def test_failure_at_the_first_rename_of_a_fresh_install_leaves_nothing(self):
        skills_dir = self.skills_dir()
        result = self.run(extra_env={"FAKE_MV_FAIL": "/new/knowledge-authority"})
        assert result.returncode == 1, result.stderr
        for s in SKILLS:
            assert not (skills_dir / s).exists(), f"{s} is installed although the swap was rolled back"
        self.assert_clean(skills_dir)

    def test_failed_rollback_keeps_the_work_directory(self):
        skills_dir = self.skills_dir()
        self.install_previous(skills_dir)
        result = self.run(extra_env={"FAKE_MV_FAIL": "/new/knowledge-authority:/old/roadmap-manager"})
        assert result.returncode == 1, result.stderr
        errors = self.error_lines(result)
        assert any("the previous copy of roadmap-manager could not be restored" in line
                   and "/old/roadmap-manager" in line for line in errors), result.stderr
        _, work = self.leftovers(skills_dir)
        assert len(work) == 1, f"the work directory must be kept after a failed rollback: {work}"
        kept = work[0] / "old" / "roadmap-manager" / "references" / "obsolete.md"
        assert kept.is_file(), "the previous copy is not where the message says it is"
        assert list(self.tmpdir.iterdir()) == [], "the staging directory was left behind"

    def test_sigterm_during_the_swap_rolls_back(self):
        skills_dir = self.skills_dir()
        before = self.install_previous(skills_dir)
        result = self.run(extra_env={"FAKE_MV_TERM": "/new/knowledge-authority"})
        assert result.returncode == 143, f"exit {result.returncode}\n{result.stderr}"
        for s in SKILLS:
            assert snapshot(skills_dir / s) == before[s], f"{s} is not at its previous version after SIGTERM"
        self.assert_clean(skills_dir)

    def test_sigterm_during_the_download_leaves_nothing(self):
        skills_dir = self.skills_dir()
        before = self.install_previous(skills_dir)
        result = self.run(extra_env={"FAKE_TERM_ON": ARCHIVE_NAME})
        assert result.returncode == 143, f"exit {result.returncode}\n{result.stderr}"
        for s in SKILLS:
            assert snapshot(skills_dir / s) == before[s]
        self.assert_clean(skills_dir)


class TestInstallSkillsScriptText:
    """Acceptance criterion 18, read from the script itself."""

    def setup_method(self):
        self.text = INSTALL_SKILLS_SH.read_text(encoding="utf-8")

    def teardown_method(self):
        pass

    def test_no_sudo_no_prompt_no_bypass(self):
        assert "sudo" not in self.text, "install-skills.sh must never invoke sudo"
        assert "PROMPT" not in self.text and "/dev/tty" not in self.text, "install-skills.sh must not prompt"
        assert '"$@"' not in self.text.replace('main "$@"', ""), "install-skills.sh must read no argument"
        for flag in ("--no-verify", "--insecure", "--skip"):
            assert flag not in self.text, f"install-skills.sh must offer no {flag} switch"


def _test_classes():
    """Every test class in this module, discovered rather than listed (rmp task #303)."""
    return [
        obj
        for name, obj in sorted(globals().items())
        if isinstance(obj, type) and name.startswith("Test")
    ]


def _run_all():
    classes = _test_classes()
    passed = 0
    failed = 0
    failures = []
    print("=" * 60)
    print("install-skills.sh agent-skills installer (task #598)")
    print(f"{len(classes)} test classes discovered: "
          f"{', '.join(cls.__name__ for cls in classes)}")
    print("=" * 60)
    for cls in classes:
        instance = cls()
        methods = sorted(m for m in dir(instance) if m.startswith("test_"))
        print(f"\n{cls.__name__} ({len(methods)} tests)")
        for m in methods:
            instance.setup_method()
            try:
                getattr(instance, m)()
                passed += 1
                print(f"  PASS {m}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((f"{cls.__name__}.{m}", exc))
                print(f"  FAIL {m}")
            finally:
                instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Agent-skills installer tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\nFAIL {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
