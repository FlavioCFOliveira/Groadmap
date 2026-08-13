#!/usr/bin/env python3
"""
Test 47: install.sh archive extraction contract (regression for task #138).

Restoring the Windows build targets (task #136) made the release workflow ship
`rmp-{version}-windows-{arch}.zip` for the first time. That exposed a latent bug
in install.sh: its Windows branch never extracted the archive. It moved the
downloaded file straight into place under the binary's name, with the comment
"assuming it's the binary directly". While no Windows archive existed the branch
was unreachable, because the download failed first and the script exited 1. Once
the archive existed, the download succeeded and a ZIP file was installed as the
executable -- and the script reported SUCCESS. A clean failure had become a
silent corruption.

This is the regression guard. It drives the real install.sh end to end inside a
hermetic sandbox -- stubbed `uname` and `curl`, and a PATH containing only the
tools under test -- and pins three properties:

  1. Windows, unzip available: the installed file is the archive's CONTENT, not
     the archive itself. This is the property that was violated.
  2. Windows, unzip missing: the script exits non-zero, names the missing tool,
     and installs nothing. A partial or best-effort install is never acceptable,
     because it defers the failure to first use with no explanation.
  3. Unix: unchanged and needs no unzip. The .tar.gz path must not acquire a new
     dependency just because the Windows path gained one.

No network and no Go toolchain are involved: the fixtures are small sentinel
files, so the assertions are about extraction behaviour rather than binary
format, and the suite stays fast and deterministic.
"""

import os
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import uuid
import zipfile
from pathlib import Path

VERSION = "v9.9.9"
REPO_ROOT = Path(__file__).resolve().parent.parent
INSTALL_SH = REPO_ROOT / "install.sh"

# Tools install.sh legitimately relies on. gzip/gunzip are included because GNU
# tar shells out to them for -z; that is a pre-existing requirement of the
# .tar.gz branch, not something this test introduced.
REQUIRED_TOOLS = [
    "bash", "grep", "sed", "head", "mkdir", "rm", "mv", "cp",
    "chmod", "tar", "gzip", "gunzip", "cat", "basename", "dirname",
]

UNAME_SHIM = """#!/usr/bin/env bash
case "$1" in
  -s) echo "$FAKE_UNAME_S" ;;
  -m) echo "$FAKE_UNAME_M" ;;
  *)  echo "$FAKE_UNAME_S" ;;
esac
"""

# Serves the release metadata query and the archive download from local
# fixtures, so the test never reaches the network.
CURL_SHIM = """#!/usr/bin/env bash
out=""; url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -*) shift ;;
    *)  url="$1"; shift ;;
  esac
done
if [[ "$url" == *api.github.com* ]]; then
  echo "{\\"tag_name\\": \\"${FAKE_VERSION}\\"}"
  exit 0
fi
fixture="${FAKE_FIXTURES}/$(basename "$url")"
[ -f "$fixture" ] || { echo "curl-shim: no fixture $fixture" >&2; exit 22; }
if [ -n "$out" ]; then cp "$fixture" "$out"; else cat "$fixture"; fi
exit 0
"""

# Stand-in used only when the host has no unzip binary. It asserts the calling
# convention install.sh must use, then extracts with Python, so the test pins
# the invocation contract even on a host without the real tool.
UNZIP_SHIM = '''#!{python}
import sys, zipfile
args = sys.argv[1:]
if not args or not args[0].startswith("-") or "o" not in args[0] or "q" not in args[0]:
    sys.stderr.write("unzip-shim: expected overwrite/quiet flags, got %r\\n" % (args,))
    sys.exit(2)
if "-d" not in args:
    sys.stderr.write("unzip-shim: expected -d <destdir>, got %r\\n" % (args,))
    sys.exit(2)
archive = args[1]
dest = args[args.index("-d") + 1]
with zipfile.ZipFile(archive) as zf:
    zf.extractall(dest)
'''


class InstallScriptExtractionTests:
    """Drives install.sh in a sandbox with a fully controlled environment."""

    def setup_method(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="rmp_install_sim_"))
        self.bin = self.tmp / "bin"
        self.fixtures = self.tmp / "fixtures"
        self.home = self.tmp / "home"
        for d in (self.bin, self.fixtures, self.home):
            d.mkdir(parents=True)

        # Sentinel payloads: unmistakably not archives, and unique per run so a
        # stale file cannot make an assertion pass by accident.
        token = uuid.uuid4().hex
        self.win_payload = f"GROADMAP-TEST-WINDOWS-BINARY-{token}\n".encode()
        self.unix_payload = f"GROADMAP-TEST-UNIX-BINARY-{token}\n".encode()

        self.win_archive = self.fixtures / f"rmp-{VERSION}-windows-amd64.zip"
        with zipfile.ZipFile(self.win_archive, "w") as zf:
            zf.writestr("rmp.exe", self.win_payload)

        payload_file = self.tmp / "rmp"
        payload_file.write_bytes(self.unix_payload)
        self.unix_archive = self.fixtures / f"rmp-{VERSION}-linux-amd64.tar.gz"
        with tarfile.open(self.unix_archive, "w:gz") as tf:
            tf.add(payload_file, arcname="rmp")

        self._write_shim("uname", UNAME_SHIM)
        self._write_shim("curl", CURL_SHIM)
        for tool in REQUIRED_TOOLS:
            resolved = shutil.which(tool)
            if resolved:
                (self.bin / tool).symlink_to(resolved)

    def teardown_method(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def _write_shim(self, name, body):
        path = self.bin / name
        path.write_text(body)
        path.chmod(path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    def _provide_unzip(self, available):
        path = self.bin / "unzip"
        if path.exists() or path.is_symlink():
            path.unlink()
        if not available:
            return
        real = shutil.which("unzip")
        if real:
            path.symlink_to(real)
        else:
            self._write_shim("unzip", UNZIP_SHIM.format(python=sys.executable))

    def _run(self, uname_s, unzip_available):
        self._provide_unzip(unzip_available)
        shutil.rmtree(self.home, ignore_errors=True)
        self.home.mkdir(parents=True)
        # /tmp/rmp{,.exe} is install.sh's staging path; clear any leftover so a
        # previous run cannot influence this one.
        for stale in ("/tmp/rmp", "/tmp/rmp.exe"):
            try:
                os.unlink(stale)
            except FileNotFoundError:
                pass
        result = subprocess.run(
            [shutil.which("bash"), str(INSTALL_SH)],
            input="u\n",
            capture_output=True,
            text=True,
            env={
                "PATH": str(self.bin),
                "HOME": str(self.home),
                "FAKE_UNAME_S": uname_s,
                "FAKE_UNAME_M": "x86_64",
                "FAKE_VERSION": VERSION,
                "FAKE_FIXTURES": str(self.fixtures),
            },
        )
        return result, self.home / ".local" / "bin" / "rmp"

    def test_windows_archive_is_extracted_not_installed_verbatim(self):
        """The installed file must be the archive's content, never the archive."""
        result, installed = self._run("MINGW64_NT-10.0-19045", unzip_available=True)
        assert result.returncode == 0, (
            f"Windows install must succeed when unzip is available; "
            f"exit={result.returncode}\n{result.stdout}\n{result.stderr}"
        )
        assert installed.exists(), "no binary was installed on the Windows path"
        content = installed.read_bytes()
        assert content == self.win_payload, (
            "the installed file is not the binary extracted from the archive"
        )
        assert content != self.win_archive.read_bytes(), (
            "REGRESSION (task #138): the .zip archive itself was installed as the binary"
        )
        assert not content.startswith(b"PK"), (
            "REGRESSION (task #138): the installed file carries a ZIP local-file "
            "header, so an archive was installed under the binary's name"
        )

    def test_windows_without_unzip_fails_cleanly_and_installs_nothing(self):
        """No unzip means a clean, explained refusal -- never a partial install."""
        result, installed = self._run("MINGW64_NT-10.0-19045", unzip_available=False)
        assert result.returncode != 0, (
            "install.sh must exit non-zero when it cannot extract the archive; "
            f"exit={result.returncode}\n{result.stdout}\n{result.stderr}"
        )
        combined = result.stdout + result.stderr
        assert "unzip" in combined, (
            "the failure must name the missing tool so the user can act on it; "
            f"output was:\n{combined}"
        )
        assert not installed.exists(), (
            "REGRESSION (task #138): a file was installed even though extraction "
            "was impossible; install.sh must install nothing on this path"
        )

    def test_unix_path_installs_without_unzip(self):
        """The .tar.gz path must not inherit the Windows path's dependency."""
        result, installed = self._run("Linux", unzip_available=False)
        assert result.returncode == 0, (
            "the Unix install path must not require unzip; "
            f"exit={result.returncode}\n{result.stdout}\n{result.stderr}"
        )
        assert installed.exists(), "no binary was installed on the Unix path"
        assert installed.read_bytes() == self.unix_payload, (
            "the installed file is not the binary extracted from the .tar.gz"
        )
        assert os.access(installed, os.X_OK), "the installed binary is not executable"


def _run_all():
    instance = InstallScriptExtractionTests()
    methods = [m for m in dir(instance) if m.startswith("test_")]
    passed = 0
    failed = 0
    failures = []
    print("=" * 60)
    print("install.sh archive extraction contract (task #138)")
    print("=" * 60)
    for m in sorted(methods):
        instance.setup_method()
        try:
            getattr(instance, m)()
            passed += 1
            print(f"✓ {m}")
        except AssertionError as exc:
            failed += 1
            failures.append((m, exc))
            print(f"✗ {m}")
        except Exception as exc:  # noqa: BLE001
            failed += 1
            failures.append((m, exc))
            print(f"✗ {m} (error)")
        finally:
            instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Install script extraction tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
