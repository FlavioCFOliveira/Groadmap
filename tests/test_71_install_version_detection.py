#!/usr/bin/env python3
"""
Test 71: install.sh installed-version detection (regression for rmp task #479).

install.sh compares the version of the `rmp` already on PATH with the latest
release so that it does not reinstall the release that is already installed.
It never managed to. `get_current_version()` searched the output of
`rmp --version` for `v[0-9]+.[0-9]+.[0-9]+`, and the binary has never written a
`v` before its version: it writes `Groadmap version 1.17.1`. The search matched
nothing, the installed version was always empty, and every run -- with the
latest release already installed -- announced `Latest version: ...` and
downloaded and reinstalled it. The update branch (`Current version`, then
`Already up to date` or `Updating from ... to ...`) was unreachable.

SPEC/DEPLOY.md section "Installed Version Detection" now fixes the rule: the
first line of `rmp --version` is split on whitespace, and the installed version
is the third word, provided the first two are `Groadmap version` and the third
is MAJOR.MINOR.PATCH; the tag is compared with it after one leading `v` is
removed, by string equality alone. The rule reads nothing past the third word,
so it reads all three build-identification shapes SPEC/COMMANDS.md section
"Version" publishes and the older line that ends after the version.

This module drives the real install.sh end to end in a hermetic sandbox, as
test_47 and test_49 do: stubbed `uname` and `curl` (the curl stub answers the
latest-release lookup with `v1.17.1` and records every URL requested, so nothing
reaches GitHub), a PATH holding only the tools under test, and the "installed"
`rmp` a script in `~/.local/bin` -- the directory a user-scope install writes
to -- that prints the line under test. The release archive carries a script of
the same kind, printing the line a released 1.17.1 prints, so the version the
script reads back after installing is observable too.

Every assertion is on the outcome the SPEC's table names, not on the exit code
alone: the helper messages in order on standard error, whether any release
asset was requested, and what sits at the installation path afterwards.
"""

import hashlib
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import uuid
from pathlib import Path

LATEST_TAG = "v1.17.1"
REPO_ROOT = Path(__file__).resolve().parent.parent
INSTALL_SH = REPO_ROOT / "install.sh"

# The tools install.sh relies on for a Unix install, as in test_47.
REQUIRED_TOOLS = [
    "bash", "grep", "sed", "head", "mkdir", "rm", "mv", "cp",
    "chmod", "tar", "gzip", "gunzip", "cat", "basename", "dirname",
    "sha256sum", "mktemp", "ls",
]

UNAME_SHIM = """#!/usr/bin/env bash
case "$1" in
  -s) echo "Linux" ;;
  -m) echo "x86_64" ;;
  *)  echo "Linux" ;;
esac
"""

# Answers the latest-release lookup and serves release assets from local
# fixtures, recording every URL it is asked for.
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
if [[ "$url" == *api.github.com* ]]; then
  echo "{\\"tag_name\\": \\"${FAKE_VERSION}\\"}"
  exit 0
fi
fixture="${FAKE_FIXTURES}/$(basename "$url")"
[ -f "$fixture" ] || { echo "curl-shim: no fixture $fixture" >&2; exit 22; }
if [ -n "$out" ]; then cp "$fixture" "$out"; else cat "$fixture"; fi
exit 0
"""

# An `rmp` that writes a chosen standard output and a chosen standard error.
# Both come from files, so the bytes are exactly what the test wrote.
RMP_SHIM = """#!/usr/bin/env bash
# token {token}
cat "{stderr}" >&2
cat "{stdout}"
"""

# The binary the release archive carries: it answers --version the way a
# released 1.17.1 does, so the version read back after installing is real.
RELEASED_RMP = """#!/usr/bin/env bash
# GROADMAP-TEST-RELEASED-BINARY {token}
echo "Groadmap version 1.17.1 (commit 994c1c7)"
"""

ANSI_ESCAPE = re.compile(r"\x1b\[[0-9;]*m")

# The four line shapes SPEC/DEPLOY.md section "Installed Version Detection"
# tabulates, each of which must read as 1.17.1.
READABLE_SHAPES = [
    "Groadmap version 1.17.1 (commit 994c1c7)",
    "Groadmap version 1.17.1 (commit 8647dae, modified)",
    "Groadmap version 1.17.1 (commit unknown)",
    "Groadmap version 1.17.1",
]


class TestInstalledVersionDetection:
    """install.sh reads the installed version by the SPEC's rule (task #479)."""

    def setup_method(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="rmp_install_version_"))
        self.bin = self.tmp / "bin"
        self.fixtures = self.tmp / "fixtures"
        self.home = self.tmp / "home"
        self.stage_parent = self.tmp / "tmpdir"
        for d in (self.bin, self.fixtures, self.home, self.stage_parent):
            d.mkdir(parents=True)
        self.stage_parent.chmod(0o700)
        self.user_bin = self.home / ".local" / "bin"
        self.installed = self.user_bin / "rmp"

        self.curl_log = self.tmp / "curl.log"
        self.rmp_stdout = self.tmp / "rmp.stdout"
        self.rmp_stderr = self.tmp / "rmp.stderr"

        self.token = uuid.uuid4().hex
        self.released_payload = RELEASED_RMP.format(token=self.token).encode()
        self.archive_name = f"rmp-{LATEST_TAG}-linux-amd64.tar.gz"
        self._write_release_archive(self.fixtures / self.archive_name)

        self._write_executable(self.bin / "uname", UNAME_SHIM)
        self._write_executable(self.bin / "curl", CURL_SHIM)
        for tool in REQUIRED_TOOLS:
            resolved = shutil.which(tool)
            if resolved:
                (self.bin / tool).symlink_to(resolved)

    def teardown_method(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    # -- sandbox ---------------------------------------------------------------

    @staticmethod
    def _write_executable(path, body):
        path.write_text(body, encoding="utf-8")
        path.chmod(path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    def _write_release_archive(self, archive):
        with tempfile.TemporaryDirectory() as staging:
            member = Path(staging) / "rmp"
            member.write_bytes(self.released_payload)
            member.chmod(0o755)
            with tarfile.open(archive, "w:gz") as tf:
                tf.add(member, arcname="rmp")
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        archive.with_name(archive.name + ".sha256").write_text(
            f"{digest}  {archive.name}\n", encoding="utf-8")

    def _install_rmp(self, stdout, stderr=""):
        """Put an `rmp` on PATH that writes exactly these two streams."""
        self.user_bin.mkdir(parents=True, exist_ok=True)
        self.rmp_stdout.write_text(stdout, encoding="utf-8")
        self.rmp_stderr.write_text(stderr, encoding="utf-8")
        self._write_executable(self.installed, RMP_SHIM.format(
            token=self.token, stdout=self.rmp_stdout, stderr=self.rmp_stderr))

    def _run(self):
        self.curl_log.write_text("")
        result = subprocess.run(
            [shutil.which("bash"), str(INSTALL_SH)],
            input="u\n",
            capture_output=True,
            text=True,
            timeout=60,
            env={
                # ~/.local/bin first: it is where the installed rmp lives and
                # where a user-scope install writes the new one.
                "PATH": f"{self.user_bin}:{self.bin}",
                "HOME": str(self.home),
                "TMPDIR": str(self.stage_parent),
                "FAKE_VERSION": LATEST_TAG,
                "FAKE_FIXTURES": str(self.fixtures),
                "FAKE_CURL_LOG": str(self.curl_log),
            },
        )
        messages = [ANSI_ESCAPE.sub("", line) for line in result.stderr.splitlines()]
        return result, messages, self.curl_log.read_text()

    # -- assertions ------------------------------------------------------------

    @staticmethod
    def _assert_in_order(messages, expected, why, result):
        """The expected lines appear on stderr consecutively, in this order."""
        for start, line in enumerate(messages):
            if line == expected[0]:
                assert messages[start:start + len(expected)] == expected, (
                    f"{why}: stderr must carry {expected} one after the other; "
                    f"it carries {messages}\nstdout={result.stdout}"
                )
                return
        raise AssertionError(
            f"{why}: stderr does not carry {expected[0]!r}; it carries {messages}"
            f"\nstdout={result.stdout}"
        )

    @staticmethod
    def _assert_absent(messages, prefix, why):
        found = [line for line in messages if line.startswith(prefix)]
        assert not found, f"{why}: stderr must not carry {prefix!r}; it carries {found}"

    def _assert_up_to_date(self, line):
        why = f"installed rmp writing {line!r}"
        installed_before = self.installed.read_bytes()

        result, messages, requests = self._run()

        assert result.returncode == 0, (
            f"{why}: install.sh must exit 0 when the latest release is already "
            f"installed; exit={result.returncode}\nstderr={result.stderr}"
        )
        self._assert_in_order(
            messages,
            ["INFO: Current version: 1.17.1", f"SUCCESS: Already up to date ({LATEST_TAG})"],
            why, result)
        self._assert_absent(messages, "INFO: Latest version:", why)
        self._assert_absent(messages, "WARNING: Updating from", why)
        assert "releases/download" not in requests, (
            f"{why}: nothing may be downloaded when the installed version is the "
            f"latest release; install.sh asked for:\n{requests}"
        )
        assert self.installed.read_bytes() == installed_before, (
            f"{why}: the installed rmp was replaced although it was up to date"
        )

    def _assert_fresh_install(self, why):
        result, messages, requests = self._run()

        assert result.returncode == 0, (
            f"{why}: the installation must complete; exit={result.returncode}"
            f"\nstderr={result.stderr}"
        )
        assert f"INFO: Latest version: {LATEST_TAG}" in messages, (
            f"{why}: with no installed version read, install.sh must announce the "
            f"latest release; stderr carries {messages}"
        )
        self._assert_absent(messages, "INFO: Current version:", why)
        self._assert_absent(messages, "SUCCESS: Already up to date", why)
        self._assert_absent(messages, "WARNING: Updating from", why)
        assert f"releases/download/{LATEST_TAG}/{self.archive_name}" in requests, (
            f"{why}: a fresh installation must download the latest release; "
            f"install.sh asked for:\n{requests}"
        )
        assert self.installed.read_bytes() == self.released_payload, (
            f"{why}: the latest release was not installed at {self.installed}"
        )
        assert "SUCCESS: Installation complete! Version: 1.17.1" in messages, (
            f"{why}: after installing, the version must be read back by the same "
            f"rule; stderr carries {messages}"
        )

    # -- the installed version is read -------------------------------------------

    def test_every_published_line_shape_reads_as_the_installed_version(self):
        """The defect: an installed 1.17.1 was never recognised, so it was reinstalled."""
        for line in READABLE_SHAPES:
            self._install_rmp(line + "\n")
            self._assert_up_to_date(line)

    def test_diagnostics_on_standard_error_do_not_disturb_the_reading(self):
        """Standard error is discarded; only standard output is read."""
        self._install_rmp(READABLE_SHAPES[0] + "\n",
                          stderr="Groadmap version 9.9.9 (commit 0a1b2c3)\n")
        self._assert_up_to_date(READABLE_SHAPES[0] + " with a different line on stderr")

    def test_an_older_installed_version_is_updated(self):
        """A different installed version reports both and installs the latest release."""
        self._install_rmp("Groadmap version 1.16.0 (commit 0a1b2c3)\n")

        result, messages, requests = self._run()

        why = "installed rmp 1.16.0"
        assert result.returncode == 0, (
            f"{why}: the update must complete; exit={result.returncode}\nstderr={result.stderr}"
        )
        self._assert_in_order(
            messages,
            ["INFO: Current version: 1.16.0", f"WARNING: Updating from 1.16.0 to {LATEST_TAG}"],
            why, result)
        self._assert_absent(messages, "INFO: Latest version:", why)
        self._assert_absent(messages, "SUCCESS: Already up to date", why)
        assert f"releases/download/{LATEST_TAG}/{self.archive_name}" in requests, (
            f"{why}: the latest release must be downloaded; install.sh asked for:\n{requests}"
        )
        assert self.installed.read_bytes() == self.released_payload, (
            f"{why}: the latest release did not replace the installed rmp"
        )
        assert "SUCCESS: Installation complete! Version: 1.17.1" in messages, (
            f"{why}: the version read back after the update must be the new one; "
            f"stderr carries {messages}"
        )

    def test_a_newer_installed_version_is_compared_by_equality_alone(self):
        """Versions are never ordered: a newer one differs exactly as an older one does."""
        self._install_rmp("Groadmap version 1.18.0 (commit 0a1b2c3)\n")

        result, messages, requests = self._run()

        why = "installed rmp 1.18.0"
        assert result.returncode == 0, (
            f"{why}: exit={result.returncode}\nstderr={result.stderr}"
        )
        self._assert_in_order(
            messages,
            ["INFO: Current version: 1.18.0", f"WARNING: Updating from 1.18.0 to {LATEST_TAG}"],
            why, result)
        assert f"releases/download/{LATEST_TAG}/{self.archive_name}" in requests, (
            f"{why}: install.sh asked for:\n{requests}"
        )

    # -- no installed version --------------------------------------------------

    def test_no_rmp_on_path_is_a_fresh_install(self):
        """No rmp at all yields no installed version."""
        assert not self.installed.exists()
        self._assert_fresh_install("no rmp on PATH")

    def test_output_that_carries_no_version_is_a_fresh_install(self):
        """Every other shape yields no installed version, and nothing is guessed."""
        cases = [
            ("empty output", ""),
            ("first two words are not `Groadmap version`", "rmp version 1.17.1\n"),
            ("an unrelated program named rmp", "rmp: remove files, version 1.17.1\n"),
            ("a leading v on the third word", "Groadmap version v1.17.1 (commit 994c1c7)\n"),
            ("a third word that is not MAJOR.MINOR.PATCH", "Groadmap version 1.17 (commit 994c1c7)\n"),
            ("the version only on the second line", "\nGroadmap version 1.17.1 (commit 994c1c7)\n"),
        ]
        for why, stdout in cases:
            self._install_rmp(stdout)
            self._assert_fresh_install(why)

    def test_a_version_written_only_to_standard_error_is_not_read(self):
        """Standard error is discarded, so a version found only there is no version."""
        self._install_rmp("", stderr="Groadmap version 1.17.1 (commit 994c1c7)\n")
        self._assert_fresh_install("version on standard error only")


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
    print("install.sh installed-version detection (task #479)")
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
    print(f"Installed-version detection tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\nFAIL {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
