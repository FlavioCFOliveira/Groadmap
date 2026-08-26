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

The module carries a second class, InstallScriptChecksumTests, for the integrity
gate that runs immediately before that extraction (rmp task #185). The release
workflow has always published a `<archive>.sha256` beside every archive, and
install.sh never fetched it: a substituted asset, a proxy that terminates TLS or
a truncated download was extracted and moved into /usr/local/bin with sudo,
without a word. That class drives the same real script through the same sandbox
and pins the verification: a matching digest installs, and a mismatched digest,
an absent checksum, a checksum describing a different archive and a malformed
checksum each abort with nothing installed and nothing extracted.

The module carries a third class, InstallScriptStagingTests, for the directory
that verification and extraction both happen in (rmp task #309). That directory
was `/tmp/rmp_install_$$`, created with `mkdir -p` -- a name drawn from at most
32768 PIDs, and a call that SUCCEEDS on a directory that already exists. Another
local user could pre-create the whole PID space mode 0777 and wait: the archive
was then verified and extracted inside a directory they could write to, and
since those are two separate reads of one path, the bytes tar unpacked need not
be the bytes sha256sum hashed. The install then carried that payload into
/usr/local/bin under the sudo the documented `curl ... | bash` path already
uses, so the window sat on a privilege boundary. The same file also staged the
extracted binary at the fixed path `/tmp/rmp`. That class pins the replacement:
the directory is created by `mktemp -d`, it is private (mode 0700) at the moment
the verified archive sits in it, a directory the script did not create is
refused rather than reused, a host that cannot stage privately installs nothing,
the fixed `/tmp/rmp` paths are never written, and the staging directory survives
no exit path -- not a refusal, not a failed extraction, not a signal.
"""

import hashlib
import os
import re
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
    # install.sh verifies every archive against the .sha256 the release
    # publishes before it extracts anything, so the sandbox has to provide a
    # SHA-256 tool or the script refuses to install at all. sha256sum is the
    # first of the three it accepts; InstallScriptChecksumTests withholds all
    # three deliberately, to drive the refusal.
    "sha256sum",
    # install.sh stages every download in a directory mktemp creates, and reads
    # that directory's permissions back with `ls -ld` before it writes anything
    # into it (rmp task #309). Both are withheld deliberately by
    # InstallScriptStagingTests, to drive the refusals.
    "mktemp",
    "ls",
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


def write_checksum(archive: Path, name: str = None) -> Path:
    """Write the `<archive>.sha256` the release publishes beside an archive.

    The byte layout is the one .github/workflows/release.yml produces. Its
    "Generate checksum" step runs `sha256sum <archive> > <archive>.sha256` from
    inside dist/, and GNU sha256sum in text mode writes the 64 lowercase hex
    characters of the digest, TWO spaces, the file's base name and a newline --
    verified against coreutils 9.4. The name is a parameter so a test can
    publish a checksum that describes some other archive, which install.sh must
    reject rather than match on position.
    """
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    target = archive.with_name(archive.name + ".sha256")
    target.write_text(f"{digest}  {name or archive.name}\n", encoding="utf-8")
    return target


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

        # Both archives are published with the checksum a real release carries;
        # without it install.sh refuses to install, and these extraction tests
        # would fail for a reason that has nothing to do with extraction.
        write_checksum(self.win_archive)
        write_checksum(self.unix_archive)

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


# Same contract as CURL_SHIM, plus a recording of every URL requested. The
# recording is the evidence that the no-hashing-tool guard refuses BEFORE the
# script touches the network, exactly as the platform guards in test_49 do.
LOGGING_CURL_SHIM = """#!/usr/bin/env bash
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

# Records that extraction was attempted, then extracts. The recording is what
# proves verification runs BEFORE extraction rather than after it: on a rejected
# archive this file must stay empty, whatever the exit code says.
LOGGING_UNZIP_SHIM = """#!{python}
import os, sys, zipfile
args = sys.argv[1:]
with open(os.environ["FAKE_EXTRACT_LOG"], "a", encoding="utf-8") as log:
    log.write("unzip %s\\n" % " ".join(args))
archive = args[1]
dest = args[args.index("-d") + 1]
with zipfile.ZipFile(archive) as zf:
    zf.extractall(dest)
"""

# The same idea for the .tar.gz branch: a wrapper that records the invocation
# and then delegates to the real tar, so the Unix path proves the same ordering.
LOGGING_TAR_SHIM = """#!/usr/bin/env bash
echo "tar $*" >> "$FAKE_EXTRACT_LOG"
exec {real_tar} "$@"
"""


class InstallScriptChecksumTests:
    """install.sh verifies the archive against the published .sha256 (task #185).

    The release workflow has published `<archive>.sha256` beside every archive
    since it was written -- `.github/workflows/release.yml`, step "Generate
    checksum" -- and `SPEC/DEPLOY.md` lists those checksums among the release
    assets. install.sh never fetched one: `grep -i sha256 install.sh` returned
    nothing at all, so the integrity of a binary installed into /usr/local/bin
    with sudo, through the README's documented `curl ... | bash`, rested
    entirely on TLS to github.com. A substituted release asset, a proxy that
    terminates TLS, or a download truncated by a dropped connection was
    extracted and installed without a word.

    Each test drives the real script through the same hermetic sandbox the
    extraction tests use, and asserts on what reached the disk rather than on
    the exit code alone: an installer that fails loudly and installs the file
    anyway has not been fixed.
    """

    def setup_method(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="rmp_install_sum_"))
        self.bin = self.tmp / "bin"
        self.fixtures = self.tmp / "fixtures"
        self.home = self.tmp / "home"
        for d in (self.bin, self.fixtures, self.home):
            d.mkdir(parents=True)

        self.curl_log = self.tmp / "curl.log"
        self.extract_log = self.tmp / "extract.log"

        token = uuid.uuid4().hex
        self.unix_payload = f"GROADMAP-TEST-UNIX-BINARY-{token}\n".encode()
        self.win_payload = f"GROADMAP-TEST-WINDOWS-BINARY-{token}\n".encode()
        # The payload an attacker would want installed. It exists only inside a
        # substituted archive, so finding it anywhere on disk proves the
        # substitution was installed.
        self.hostile_payload = f"GROADMAP-TEST-HOSTILE-PAYLOAD-{token}\n".encode()

        self.unix_name = f"rmp-{VERSION}-linux-amd64.tar.gz"
        self.win_name = f"rmp-{VERSION}-windows-amd64.zip"
        self.unix_archive = self.fixtures / self.unix_name
        self.win_archive = self.fixtures / self.win_name

        self._write_tar(self.unix_archive, self.unix_payload)
        with zipfile.ZipFile(self.win_archive, "w") as zf:
            zf.writestr("rmp.exe", self.win_payload)

        # The genuine checksums, written before any fixture is tampered with: a
        # release publishes the digest of the archive it built, and an attacker
        # who swaps the archive afterwards is exactly the case under test.
        write_checksum(self.unix_archive)
        write_checksum(self.win_archive)

        self._write_shim("uname", UNAME_SHIM)
        self._write_shim("curl", LOGGING_CURL_SHIM)
        for tool in REQUIRED_TOOLS:
            resolved = shutil.which(tool)
            if resolved:
                (self.bin / tool).symlink_to(resolved)
        # tar and unzip are wrapped rather than linked, so every extraction
        # attempt is recorded.
        real_tar = shutil.which("tar")
        (self.bin / "tar").unlink()
        self._write_shim("tar", LOGGING_TAR_SHIM.format(real_tar=real_tar))
        self._write_shim("unzip", LOGGING_UNZIP_SHIM.format(python=sys.executable))

    def teardown_method(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def _write_shim(self, name, body):
        path = self.bin / name
        path.write_text(body)
        path.chmod(path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    @staticmethod
    def _write_tar(path, payload):
        with tempfile.TemporaryDirectory() as staging:
            member = Path(staging) / "rmp"
            member.write_bytes(payload)
            with tarfile.open(path, "w:gz") as tf:
                tf.add(member, arcname="rmp")

    def _provide_hasher(self, available):
        """Add or withhold every SHA-256 tool install.sh accepts.

        Only sha256sum is ever linked in, but the removal covers all three
        names: a host that happened to carry shasum or openssl would otherwise
        make the refusal test vacuous.
        """
        for name in ("sha256sum", "shasum", "openssl"):
            path = self.bin / name
            if path.exists() or path.is_symlink():
                path.unlink()
        if not available:
            return
        resolved = shutil.which("sha256sum")
        assert resolved, "this host has no sha256sum, which the sandbox needs"
        (self.bin / "sha256sum").symlink_to(resolved)

    def _run(self, uname_s="Linux", hasher=True):
        self._provide_hasher(hasher)
        self.curl_log.write_text("")
        self.extract_log.write_text("")
        shutil.rmtree(self.home, ignore_errors=True)
        self.home.mkdir(parents=True)
        for stale in ("/tmp/rmp", "/tmp/rmp.exe"):
            try:
                os.unlink(stale)
            except FileNotFoundError:
                pass
        # Matches both spellings: the directory this once created,
        # `rmp_install_<pid>`, and the one mktemp creates now,
        # `rmp_install.XXXXXXXXXX`. A glob that matched only the old name would
        # quietly stop catching anything (rmp task #309).
        before = set(Path("/tmp").glob("rmp_install*"))

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
                "FAKE_CURL_LOG": str(self.curl_log),
                "FAKE_EXTRACT_LOG": str(self.extract_log),
            },
        )
        leaked = sorted(str(p) for p in set(Path("/tmp").glob("rmp_install*")) - before)
        return _Run(
            result=result,
            installed=self.home / ".local" / "bin" / "rmp",
            requests=self.curl_log.read_text(),
            extractions=self.extract_log.read_text(),
            leaked_tmpdirs=leaked,
        )

    def _assert_nothing_installed(self, run, why):
        assert not run.installed.exists(), (
            f"{why}: a file was installed at {run.installed} even though the "
            f"archive was refused; install.sh must install nothing on this path."
            f"\nstdout={run.result.stdout}\nstderr={run.result.stderr}"
        )
        for staged in (Path("/tmp/rmp"), Path("/tmp/rmp.exe")):
            assert not staged.exists(), (
                f"{why}: {staged} survives the refusal, so a rejected archive "
                f"was still unpacked into install.sh's staging path"
            )
        assert run.leaked_tmpdirs == [], (
            f"{why}: the temporary directory was left behind: {run.leaked_tmpdirs}"
        )

    # -- the archive matches its published digest ---------------------------

    def test_matching_digest_installs_the_binary(self):
        """The control: an untampered download still installs, and says so."""
        run = self._run()
        assert run.result.returncode == 0, (
            "an archive that matches its published checksum must install; "
            f"exit={run.result.returncode}\n{run.result.stdout}\n{run.result.stderr}"
        )
        assert run.installed.read_bytes() == self.unix_payload, (
            "the installed file is not the binary the verified archive carried"
        )
        assert f"{self.unix_name}.sha256" in run.requests, (
            "install.sh must fetch the .sha256 published beside the archive; "
            f"it asked for:\n{run.requests}"
        )
        combined = run.result.stdout + run.result.stderr
        assert "Checksum verified" in combined, (
            "a successful verification must be reported, so the user can see "
            f"the check ran at all; output was:\n{combined}"
        )

    def test_windows_archive_is_verified_too(self):
        """The .zip branch is not a hole in the gate."""
        run = self._run(uname_s="MINGW64_NT-10.0-19045")
        assert run.result.returncode == 0, (
            f"exit={run.result.returncode}\n{run.result.stdout}\n{run.result.stderr}"
        )
        assert run.installed.read_bytes() == self.win_payload
        assert f"{self.win_name}.sha256" in run.requests, (
            f"the Windows path must fetch its checksum too; asked for:\n{run.requests}"
        )

    # -- the archive does not match -----------------------------------------

    def test_substituted_archive_aborts_without_installing(self):
        """THE defect: a replaced release asset was installed without a word."""
        self._write_tar(self.unix_archive, self.hostile_payload)

        run = self._run()

        assert run.result.returncode != 0, (
            "an archive whose digest does not match the published checksum must "
            f"abort; exit={run.result.returncode}\n{run.result.stdout}\n{run.result.stderr}"
        )
        combined = run.result.stdout + run.result.stderr
        assert "checksum mismatch" in combined.lower(), (
            "the failure must say the checksum did not match, so the user knows "
            f"this is an integrity failure and not a network error; output was:\n{combined}"
        )
        self._assert_nothing_installed(run, "substituted archive")
        assert run.extractions.strip() == "", (
            "verification must happen BEFORE extraction; a rejected archive was "
            f"handed to an extraction tool anyway:\n{run.extractions}"
        )
        # The strongest form of the assertion: the substituted payload must not
        # exist anywhere the installer could have put it.
        for root in (self.home, Path("/tmp")):
            for path in root.rglob("rmp*"):
                if path.is_file() and path.read_bytes() == self.hostile_payload:
                    raise AssertionError(f"the substituted payload was written to {path}")

    def test_truncated_download_aborts_without_installing(self):
        """A partial download that still extracts is the quiet half of this bug."""
        full = self.unix_archive.read_bytes()
        self.unix_archive.write_bytes(full[: len(full) // 2])

        run = self._run()

        assert run.result.returncode != 0, (
            "a truncated archive must abort, not be extracted best-effort; "
            f"exit={run.result.returncode}\n{run.result.stdout}\n{run.result.stderr}"
        )
        combined = run.result.stdout + run.result.stderr
        assert "checksum mismatch" in combined.lower(), (
            "the refusal must be the checksum's, not an earlier failure that "
            f"happens to exit non-zero; output was:\n{combined}"
        )
        self._assert_nothing_installed(run, "truncated archive")
        assert run.extractions.strip() == "", (
            f"a truncated archive reached an extraction tool:\n{run.extractions}"
        )

    def test_corrupted_windows_archive_is_refused_before_unzip_runs(self):
        """Ordering, proved on the branch where the tool is separately invoked."""
        with zipfile.ZipFile(self.win_archive, "w") as zf:
            zf.writestr("rmp.exe", self.hostile_payload)

        run = self._run(uname_s="MINGW64_NT-10.0-19045")

        assert run.result.returncode != 0, (
            f"exit={run.result.returncode}\n{run.result.stdout}\n{run.result.stderr}"
        )
        combined = run.result.stdout + run.result.stderr
        assert "checksum mismatch" in combined.lower(), (
            f"the refusal must be the checksum's; output was:\n{combined}"
        )
        self._assert_nothing_installed(run, "substituted Windows archive")
        assert run.extractions.strip() == "", (
            "unzip must never see an archive that failed verification; it was "
            f"invoked as:\n{run.extractions}"
        )

    # -- the checksum itself is absent, wrong or unusable -------------------

    def test_missing_checksum_is_a_failure_not_a_downgrade(self):
        """No checksum means no install: the gate has no unverified mode."""
        (self.fixtures / f"{self.unix_name}.sha256").unlink()

        run = self._run()

        assert run.result.returncode != 0, (
            "a release with no published checksum must be refused, not installed "
            f"unverified; exit={run.result.returncode}\n{run.result.stderr}"
        )
        combined = run.result.stdout + run.result.stderr
        assert "Failed to download the checksum" in combined, (
            "the failure must name the missing checksum as the reason, so it "
            f"cannot be confused with any other refusal; output was:\n{combined}"
        )
        assert f"{self.unix_name}.sha256" in combined, (
            f"the failure must name the asset it could not fetch:\n{combined}"
        )
        self._assert_nothing_installed(run, "missing checksum")
        assert run.extractions.strip() == "", (
            f"an unverifiable archive reached an extraction tool:\n{run.extractions}"
        )

    def test_checksum_describing_another_archive_is_rejected(self):
        """The digest is matched by NAME, never taken from the first line."""
        # A well-formed checksum file carrying a digest that is genuinely
        # correct -- for a different archive. Reading field 1 of line 1 would
        # accept it; matching on the name must not.
        other = f"rmp-{VERSION}-linux-arm64.tar.gz"
        write_checksum(self.unix_archive, name=other)

        run = self._run()

        assert run.result.returncode != 0, (
            "a checksum file that does not name this archive must be refused; "
            f"exit={run.result.returncode}\n{run.result.stderr}"
        )
        combined = run.result.stdout + run.result.stderr
        assert "records no SHA-256 digest" in combined, (
            "the refusal must say the file records no digest for THIS archive, "
            f"which is the property under test; output was:\n{combined}"
        )
        self._assert_nothing_installed(run, "checksum for another archive")

    def test_malformed_checksum_is_rejected(self):
        """An error page served with a 200 is not a digest."""
        (self.fixtures / f"{self.unix_name}.sha256").write_text(
            "<!DOCTYPE html>\n<html><body>Not Found</body></html>\n",
            encoding="utf-8",
        )

        run = self._run()

        assert run.result.returncode != 0, (
            "a checksum file that carries no digest must be refused; "
            f"exit={run.result.returncode}\n{run.result.stderr}"
        )
        combined = run.result.stdout + run.result.stderr
        assert "records no SHA-256 digest" in combined, (
            f"the refusal must be the checksum parser's; output was:\n{combined}"
        )
        self._assert_nothing_installed(run, "malformed checksum")
        assert run.extractions.strip() == "", (
            f"an unverifiable archive reached an extraction tool:\n{run.extractions}"
        )

    def test_empty_checksum_file_is_rejected(self):
        """An empty expected digest must never compare equal to anything."""
        (self.fixtures / f"{self.unix_name}.sha256").write_text("", encoding="utf-8")

        run = self._run()

        assert run.result.returncode != 0, (
            "an empty checksum file must be refused; "
            f"exit={run.result.returncode}\n{run.result.stderr}"
        )
        combined = run.result.stdout + run.result.stderr
        assert "records no SHA-256 digest" in combined, (
            "an empty file must be reported as carrying no digest, never "
            f"compared as an empty digest; output was:\n{combined}"
        )
        self._assert_nothing_installed(run, "empty checksum")

    # -- the host cannot verify at all --------------------------------------

    def test_no_hashing_tool_stops_before_anything_is_downloaded(self):
        """Fail closed: no hasher means no install, and no download either.

        Continuing with a warning would be the worst of the three options. The
        documented install path is `curl ... | bash`, which is non-interactive
        and whose standard error routinely scrolls past unread, so a warning
        would leave the control in place in name only -- and the party who
        benefits from a control that fails open is precisely the attacker who
        substituted the archive.
        """
        run = self._run(hasher=False)

        assert run.result.returncode == 1, (
            "a host with no SHA-256 tool must be refused with exit 1; "
            f"exit={run.result.returncode}\n{run.result.stdout}\n{run.result.stderr}"
        )
        combined = run.result.stdout + run.result.stderr
        for tool in ("sha256sum", "shasum", "openssl"):
            assert tool in combined, (
                f"the refusal must name {tool!r} so the user can act on it; "
                f"output was:\n{combined}"
            )
        assert run.requests.strip() == "", (
            "the guard must fire before anything is fetched, like the platform "
            f"guards do; install.sh asked for:\n{run.requests}"
        )
        self._assert_nothing_installed(run, "no hashing tool")


# Logs every invocation and, in "real" mode, the directory the genuine mktemp
# created -- which is how the test learns a path it is not allowed to predict.
# The other two modes reproduce the conditions install.sh has to refuse:
#   fail      creation did not succeed, the outcome on a host whose temporary
#             directory is unusable and the outcome mktemp produces when the
#             name it tried already exists.
#   existing  a mktemp that hands back a directory it did not create -- exactly
#             what `mkdir -p` did on an attacker's pre-created path. mktemp is
#             resolved through PATH like every other tool install.sh calls, so
#             this is not a hypothetical: it is what one PATH entry buys.
MKTEMP_SHIM = """#!/usr/bin/env bash
echo "mktemp $*" >> "$FAKE_MKTEMP_LOG"
case "${FAKE_MKTEMP_MODE:-real}" in
  fail)
    exit 1
    ;;
  existing)
    echo "$FAKE_MKTEMP_EXISTING"
    exit 0
    ;;
  *)
    dir=$(@REAL_MKTEMP@ "$@") || exit 1
    echo "created $dir" >> "$FAKE_MKTEMP_LOG"
    echo "$dir"
    ;;
esac
"""

# Records the staging directory's mode and contents at the instant tar is asked
# to extract -- the instant between the checksum check and the unpacking, which
# is the whole window under test -- and then delegates to the real tar.
PROBE_TAR_SHIM = """#!/usr/bin/env bash
echo "tar $*" >> "$FAKE_EXTRACT_LOG"
dest=""; prev=""
for arg in "$@"; do
  if [ "$prev" = "-C" ]; then dest="$arg"; fi
  prev="$arg"
done
if [ -n "$dest" ]; then
  ls -ld "$dest" >> "$FAKE_STAGE_LOG"
  ls -ld "$dest"/* >> "$FAKE_STAGE_LOG" 2>/dev/null || :
fi
exec @REAL_TAR@ "$@"
"""

# Signals install.sh where a scattered `rm -rf` could never run: mid-extraction,
# with the archive and the checksum file already on disk. Exits 0, so a script
# that did not handle the signal would carry on and install successfully --
# which is what makes the assertion about the surviving directory meaningful.
SIGNAL_TAR_SHIM = """#!/usr/bin/env bash
echo "tar $*" >> "$FAKE_EXTRACT_LOG"
kill -TERM "$PPID"
exit 0
"""


class InstallScriptStagingTests:
    """install.sh stages every download in a private directory (task #309).

    Each test drives the real script through the same hermetic sandbox the two
    classes above use, with two additions: TMPDIR points at a directory the test
    owns, so "nothing was left behind" is an exact statement about an empty
    directory rather than a glob over a shared /tmp; and mktemp is a shim that
    records what install.sh asked for and can be made to fail or to misbehave.
    """

    def setup_method(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="rmp_install_stage_"))
        self.bin = self.tmp / "bin"
        self.fixtures = self.tmp / "fixtures"
        self.home = self.tmp / "home"
        # Stands in for /tmp. Private, so the sticky-bit check has nothing to
        # complain about; the world-writable case is built per test.
        self.stage_parent = self.tmp / "tmpdir"
        for d in (self.bin, self.fixtures, self.home, self.stage_parent):
            d.mkdir(parents=True)
        self.stage_parent.chmod(0o700)

        self.curl_log = self.tmp / "curl.log"
        self.extract_log = self.tmp / "extract.log"
        self.mktemp_log = self.tmp / "mktemp.log"
        self.stage_log = self.tmp / "stage.log"

        token = uuid.uuid4().hex
        self.unix_payload = f"GROADMAP-TEST-UNIX-BINARY-{token}\n".encode()
        self.win_payload = f"GROADMAP-TEST-WINDOWS-BINARY-{token}\n".encode()
        # Written to the fixed paths install.sh used to stage at. Nothing may
        # touch these bytes any more.
        self.squatted_payload = f"GROADMAP-TEST-SQUATTED-FILE-{token}\n".encode()

        self.unix_name = f"rmp-{VERSION}-linux-amd64.tar.gz"
        self.win_name = f"rmp-{VERSION}-windows-amd64.zip"
        self.unix_archive = self.fixtures / self.unix_name
        self.win_archive = self.fixtures / self.win_name
        self._write_tar(self.unix_archive, self.unix_payload)
        with zipfile.ZipFile(self.win_archive, "w") as zf:
            zf.writestr("rmp.exe", self.win_payload)
        write_checksum(self.unix_archive)
        write_checksum(self.win_archive)

        self._write_shim("uname", UNAME_SHIM)
        self._write_shim("curl", LOGGING_CURL_SHIM)
        for tool in REQUIRED_TOOLS:
            resolved = shutil.which(tool)
            if resolved:
                (self.bin / tool).symlink_to(resolved)

        real_mktemp = shutil.which("mktemp")
        assert real_mktemp, "this host has no mktemp, which install.sh requires"
        (self.bin / "mktemp").unlink()
        self._write_shim("mktemp", MKTEMP_SHIM.replace("@REAL_MKTEMP@", real_mktemp))

        self.real_tar = shutil.which("tar")
        (self.bin / "tar").unlink()
        self._write_shim("tar", PROBE_TAR_SHIM.replace("@REAL_TAR@", self.real_tar))
        self._write_shim("unzip", LOGGING_UNZIP_SHIM.format(python=sys.executable))

    def teardown_method(self):
        for stale in ("/tmp/rmp", "/tmp/rmp.exe"):
            try:
                os.unlink(stale)
            except FileNotFoundError:
                pass
        shutil.rmtree(self.tmp, ignore_errors=True)

    def _write_shim(self, name, body):
        path = self.bin / name
        path.write_text(body)
        path.chmod(path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    @staticmethod
    def _write_tar(path, payload, member="rmp"):
        with tempfile.TemporaryDirectory() as staging:
            entry = Path(staging) / member
            entry.write_bytes(payload)
            with tarfile.open(path, "w:gz") as tf:
                tf.add(entry, arcname=member)

    def _run(self, uname_s="Linux", mktemp_mode="real", existing_dir=None,
             tmpdir=None, with_mktemp=True, with_unzip=True, tar_shim="probe"):
        for log in (self.curl_log, self.extract_log, self.mktemp_log, self.stage_log):
            log.write_text("")
        shutil.rmtree(self.home, ignore_errors=True)
        self.home.mkdir(parents=True)

        mktemp_path = self.bin / "mktemp"
        if with_mktemp and not mktemp_path.exists():
            self._write_shim(
                "mktemp", MKTEMP_SHIM.replace("@REAL_MKTEMP@", shutil.which("mktemp")))
        if not with_mktemp and mktemp_path.exists():
            mktemp_path.unlink()

        unzip_path = self.bin / "unzip"
        if with_unzip and not unzip_path.exists():
            self._write_shim("unzip", LOGGING_UNZIP_SHIM.format(python=sys.executable))
        if not with_unzip and unzip_path.exists():
            unzip_path.unlink()

        body = PROBE_TAR_SHIM if tar_shim == "probe" else SIGNAL_TAR_SHIM
        self._write_shim("tar", body.replace("@REAL_TAR@", self.real_tar))

        parent = Path(tmpdir) if tmpdir else self.stage_parent
        env = {
            "PATH": str(self.bin),
            "HOME": str(self.home),
            "TMPDIR": str(parent),
            "FAKE_UNAME_S": uname_s,
            "FAKE_UNAME_M": "x86_64",
            "FAKE_VERSION": VERSION,
            "FAKE_FIXTURES": str(self.fixtures),
            "FAKE_CURL_LOG": str(self.curl_log),
            "FAKE_EXTRACT_LOG": str(self.extract_log),
            "FAKE_MKTEMP_LOG": str(self.mktemp_log),
            "FAKE_STAGE_LOG": str(self.stage_log),
            "FAKE_MKTEMP_MODE": mktemp_mode,
        }
        if existing_dir is not None:
            env["FAKE_MKTEMP_EXISTING"] = str(existing_dir)

        result = subprocess.run(
            [shutil.which("bash"), str(INSTALL_SH)],
            input="u\n",
            capture_output=True,
            text=True,
            env=env,
        )
        return _StagingRun(
            result=result,
            installed=self.home / ".local" / "bin" / "rmp",
            requests=self.curl_log.read_text(),
            extractions=self.extract_log.read_text(),
            mktemp_calls=self.mktemp_log.read_text(),
            staged=self.stage_log.read_text(),
            survivors=sorted(p.name for p in parent.iterdir()),
        )

    @staticmethod
    def _created_dirs(mktemp_calls):
        return [line.split(" ", 1)[1]
                for line in mktemp_calls.splitlines() if line.startswith("created ")]

    def _assert_staging_parent_is_empty(self, run, why):
        assert run.survivors == [], (
            f"{why}: install.sh left {run.survivors} behind in its temporary "
            f"directory. The staging directory must be removed on every exit "
            f"path.\nstdout={run.result.stdout}\nstderr={run.result.stderr}"
        )

    # -- the directory itself -----------------------------------------------

    def test_the_staging_directory_is_created_by_mktemp_and_is_unpredictable(self):
        """`mkdir -p /tmp/rmp_install_$$` is replaced by an exclusive create."""
        first = self._run()
        assert first.result.returncode == 0, (
            f"exit={first.result.returncode}\n{first.result.stdout}\n{first.result.stderr}"
        )
        second = self._run()
        assert second.result.returncode == 0

        for run in (first, second):
            assert " -d " in run.mktemp_calls, (
                "the staging directory must be created with `mktemp -d`, which "
                "fails rather than succeeding on a name that already exists; "
                f"install.sh called:\n{run.mktemp_calls}"
            )
            assert "XXXXXX" in run.mktemp_calls, (
                "the template must carry enough X characters for mktemp to draw "
                f"an unpredictable name; install.sh called:\n{run.mktemp_calls}"
            )

        made_first = self._created_dirs(first.mktemp_calls)
        made_second = self._created_dirs(second.mktemp_calls)
        assert len(made_first) == 1 and len(made_second) == 1, (
            f"one staging directory per run: {made_first} then {made_second}"
        )
        assert made_first[0] != made_second[0], (
            "REGRESSION (task #309): two runs staged in the same directory, so "
            f"the name is predictable: {made_first[0]}"
        )
        assert not re.search(r"rmp_install_\d+$", made_first[0]), (
            "REGRESSION (task #309): the staging directory is named from the "
            f"process id again, which is one of 32768 values: {made_first[0]}"
        )

    def test_the_archive_is_verified_and_extracted_inside_a_private_directory(self):
        """At the instant of extraction the directory must be mode 0700."""
        run = self._run()
        assert run.result.returncode == 0, (
            f"exit={run.result.returncode}\n{run.result.stdout}\n{run.result.stderr}"
        )
        assert "Checksum verified" in run.result.stdout + run.result.stderr, (
            "task #185's verification must still run; it is not weakened here"
        )

        lines = [ln for ln in run.staged.splitlines() if ln.strip()]
        assert lines, (
            "the tar probe recorded nothing, so the extraction directory was "
            "never observed and this test proves nothing"
        )
        assert lines[0].startswith("drwx------"), (
            "the directory holding the verified archive must be readable, "
            "writable and searchable by its owner ALONE -- a local user who "
            "cannot open the archive cannot swap it between the checksum check "
            f"and the extraction. `ls -ld` reported:\n{lines[0]}"
        )
        assert str(self.stage_parent) in lines[0], (
            f"the staging directory must sit under TMPDIR; got:\n{lines[0]}"
        )
        assert any(self.unix_name in ln for ln in lines[1:]), (
            "the archive must be inside that private directory when tar runs, "
            f"not beside it; the directory held:\n{run.staged}"
        )
        assert run.installed.read_bytes() == self.unix_payload
        self._assert_staging_parent_is_empty(run, "a successful install")

    # -- refusals ------------------------------------------------------------

    def test_a_staging_directory_that_already_exists_is_refused(self):
        """The precondition the attack needs, driven end to end.

        The mktemp shim returns a directory it did not create: world-writable,
        already populated, exactly what `mkdir -p` accepted when another local
        user pre-created `/tmp/rmp_install_<pid>` and waited. install.sh must
        refuse it rather than stage a download in it -- and must not delete it
        either, because a directory this script did not create is not this
        script's to remove.
        """
        squatted = self.stage_parent / "rmp_install.AAAAAAAAAA"
        squatted.mkdir()
        squatted.chmod(0o777)
        marker = squatted / "planted-by-another-user"
        marker.write_bytes(self.squatted_payload)

        run = self._run(mktemp_mode="existing", existing_dir=squatted)

        assert run.result.returncode != 0, (
            "install.sh must refuse a staging directory that already existed; "
            f"exit={run.result.returncode}\n{run.result.stdout}\n{run.result.stderr}"
        )
        assert "refusing to stage" in run.result.stderr, (
            "the refusal must say what it refused and why, so the user can act "
            f"on it; stderr was:\n{run.result.stderr}"
        )
        assert "releases/download" not in run.requests, (
            "nothing may be fetched once the staging directory is refused: the "
            f"point is to fail before anything is written. install.sh asked "
            f"for:\n{run.requests}"
        )
        assert run.extractions.strip() == "", (
            f"nothing may be extracted after the refusal; the log holds:\n{run.extractions}"
        )
        assert not run.installed.exists(), (
            "REGRESSION (task #309): a binary was installed after staging in a "
            "directory install.sh did not create"
        )
        assert marker.read_bytes() == self.squatted_payload, (
            "install.sh must not touch a directory it did not create"
        )
        assert sorted(p.name for p in squatted.iterdir()) == ["planted-by-another-user"], (
            "REGRESSION (task #309): install.sh wrote into a directory it did "
            f"not create; it now holds {sorted(p.name for p in squatted.iterdir())}"
        )

    def test_a_staging_directory_belonging_to_another_user_is_refused_and_kept(self):
        """The other half of the same refusal, and the one that must not delete.

        The shim hands back /tmp itself: a directory that exists, that this
        script did not create, and that belongs to root on every system the
        installer supports. install.sh must refuse to stage in it -- and must
        leave it exactly where it is. A refusal that "cleaned up" a directory it
        did not create would be a far worse defect than the one being fixed,
        since the path it removed would be whatever a hostile mktemp named.
        """
        sentinel = Path("/tmp") / f"groadmap-staging-sentinel-{uuid.uuid4().hex}"
        sentinel.write_bytes(self.squatted_payload)
        try:
            run = self._run(mktemp_mode="existing", existing_dir="/tmp")

            assert run.result.returncode != 0, (
                "install.sh must refuse to stage in a directory it does not own; "
                f"exit={run.result.returncode}\n{run.result.stdout}\n{run.result.stderr}"
            )
            assert "refusing to stage" in run.result.stderr, (
                f"the refusal must say what it refused; stderr was:\n{run.result.stderr}"
            )
            assert "releases/download" not in run.requests, (
                f"nothing may be fetched after the refusal; asked for:\n{run.requests}"
            )
            assert not run.installed.exists(), "nothing may be installed"
            assert Path("/tmp").is_dir(), (
                "install.sh removed the directory it refused. It must only ever "
                "remove a staging directory it created itself"
            )
            assert sentinel.read_bytes() == self.squatted_payload, (
                "install.sh emptied the directory it refused"
            )
        finally:
            sentinel.unlink(missing_ok=True)

    def test_a_symlinked_staging_directory_is_refused(self):
        """A symlink is the other thing `mkdir -p` used to accept.

        `mkdir -p /tmp/rmp_install_<pid>` succeeds when that path is a SYMLINK
        to a directory, and everything afterwards then writes through it into
        wherever it points -- a directory whose owner chose where it points.
        The target here is private and belongs to the test user, so ownership
        and permissions both look right and only the symlink itself can be the
        reason for the refusal.
        """
        target = self.tmp / "attacker-owned-target"
        target.mkdir()
        target.chmod(0o700)
        link = self.stage_parent / "rmp_install.SYMLINKED"
        link.symlink_to(target)

        run = self._run(mktemp_mode="existing", existing_dir=link)

        assert run.result.returncode != 0, (
            "a symlinked staging directory must be refused; "
            f"exit={run.result.returncode}\n{run.result.stdout}\n{run.result.stderr}"
        )
        assert "refusing to stage" in run.result.stderr, (
            f"the refusal must say what it refused; stderr was:\n{run.result.stderr}"
        )
        assert "releases/download" not in run.requests, (
            f"nothing may be fetched after the refusal; asked for:\n{run.requests}"
        )
        assert not run.installed.exists(), "nothing may be installed"
        assert sorted(p.name for p in target.iterdir()) == [], (
            "REGRESSION (task #309): install.sh wrote through the symlink into "
            f"{target}, which holds {sorted(p.name for p in target.iterdir())}"
        )
        assert link.is_symlink(), "install.sh must not remove what it refused"

    def test_a_staging_directory_that_cannot_be_created_stops_the_install(self):
        """Failing to create it is a refusal, never a reason to carry on."""
        run = self._run(mktemp_mode="fail")

        assert run.result.returncode != 0, (
            "install.sh must exit non-zero when it cannot create its staging "
            f"directory; exit={run.result.returncode}\n{run.result.stderr}"
        )
        assert "staging directory" in run.result.stderr, (
            f"the failure must name what could not be created; stderr was:\n{run.result.stderr}"
        )
        assert "releases/download" not in run.requests, (
            f"no archive may be requested; install.sh asked for:\n{run.requests}"
        )
        assert not run.installed.exists(), "nothing may be installed"
        self._assert_staging_parent_is_empty(run, "a staging directory that could not be created")

    def test_a_world_writable_staging_parent_without_the_sticky_bit_is_refused(self):
        """A private directory is only as private as the directory holding it.

        /tmp carries mode 1777 -- world-writable WITH the sticky bit -- so a
        local user cannot rename another user's entry out of the way. Without
        the sticky bit they can, which would put an attacker's directory back at
        the path after mktemp created ours, and the window would be open again.
        """
        exposed = self.tmp / "exposed"
        exposed.mkdir()
        exposed.chmod(0o777)

        run = self._run(tmpdir=exposed)

        assert run.result.returncode != 0, (
            "a world-writable, non-sticky temporary directory must be refused; "
            f"exit={run.result.returncode}\n{run.result.stderr}"
        )
        assert "sticky" in run.result.stderr, (
            f"the refusal must explain the condition; stderr was:\n{run.result.stderr}"
        )
        assert "releases/download" not in run.requests, (
            f"no archive may be requested; install.sh asked for:\n{run.requests}"
        )
        assert not run.installed.exists(), "nothing may be installed"
        assert sorted(p.name for p in exposed.iterdir()) == [], (
            "nothing may be staged in a directory that was refused"
        )

    def test_a_host_without_mktemp_stops_before_anything_is_downloaded(self):
        """Task #185's precedent: a missing tool aborts, it does not degrade."""
        run = self._run(with_mktemp=False)

        assert run.result.returncode == 1, (
            "a host with no mktemp must be refused with exit 1; "
            f"exit={run.result.returncode}\n{run.result.stderr}"
        )
        assert "mktemp" in run.result.stderr, (
            f"the message must name the missing tool; stderr was:\n{run.result.stderr}"
        )
        assert run.requests.strip() == "", (
            "the guard must fire before the network is touched at all, exactly "
            "as the hashing-tool guard does; install.sh asked for:\n"
            f"{run.requests}"
        )
        assert not run.installed.exists(), "nothing may be installed"

    # -- the fixed paths -----------------------------------------------------

    def test_the_fixed_staging_paths_are_never_written(self):
        """`/tmp/rmp` and `/tmp/rmp.exe` carried the same defect as the directory.

        A fixed name in a shared directory is reachable by every local user from
        the moment it appears until the moment it is installed. Nothing may be
        written through those paths any more, so a file already sitting at each
        of them must come out of a successful install untouched.
        """
        for uname_s, payload in (("Linux", self.unix_payload),
                                 ("MINGW64_NT-10.0-19045", self.win_payload)):
            for path in (Path("/tmp/rmp"), Path("/tmp/rmp.exe")):
                if path.exists() or path.is_symlink():
                    path.unlink()
                path.write_bytes(self.squatted_payload)
            before = {p: p.stat().st_ino for p in (Path("/tmp/rmp"), Path("/tmp/rmp.exe"))}

            run = self._run(uname_s=uname_s)
            assert run.result.returncode == 0, (
                f"the install must succeed on {uname_s}; "
                f"exit={run.result.returncode}\n{run.result.stdout}\n{run.result.stderr}"
            )
            assert run.installed.read_bytes() == payload, (
                f"the installed file is not the binary the archive carried ({uname_s})"
            )
            for path, inode in before.items():
                assert path.read_bytes() == self.squatted_payload, (
                    f"REGRESSION (task #309): install.sh wrote through the fixed "
                    f"staging path {path} on {uname_s}"
                )
                assert path.stat().st_ino == inode, (
                    f"REGRESSION (task #309): {path} was replaced on {uname_s}, so "
                    f"install.sh still stages at a name every local user can predict"
                )
            self._assert_staging_parent_is_empty(run, f"a successful install on {uname_s}")

    # -- cleanup on every exit path ------------------------------------------

    def test_every_abort_path_removes_the_staging_directory(self):
        """One table, one row per way out of the download (tasks #185, #309).

        The cleanup used to be a `rm -rf` repeated at each of these sites, which
        is one chance per site to forget it. It is a single EXIT trap now, and
        this is the evidence that the trap covers every row rather than most of
        them: after each run the temporary directory the script was given must
        be empty.
        """
        cases = [
            ("the archive cannot be downloaded", self._break_archive_download, {}),
            ("the checksum cannot be downloaded", self._break_checksum_download, {}),
            ("the archive was substituted", self._break_substituted_archive, {}),
            ("the checksum describes another archive", self._break_foreign_checksum, {}),
            ("the checksum is malformed", self._break_malformed_checksum, {}),
            ("the checksum file is empty", self._break_empty_checksum, {}),
            ("the archive is corrupt but verifies", self._break_corrupt_archive, {}),
            ("the archive carries no binary", self._break_archive_without_binary, {}),
            ("unzip is missing on the Windows path", lambda: None,
             {"uname_s": "MINGW64_NT-10.0-19045", "with_unzip": False}),
        ]

        for why, arrange, kwargs in cases:
            self._reset_fixtures()
            arrange()
            run = self._run(**kwargs)

            assert run.result.returncode != 0, (
                f"{why}: install.sh must exit non-zero; "
                f"exit={run.result.returncode}\n{run.result.stdout}\n{run.result.stderr}"
            )
            assert not run.installed.exists(), (
                f"{why}: a binary was installed anyway"
            )
            self._assert_staging_parent_is_empty(run, why)

        self._reset_fixtures()

    def test_a_signal_mid_extraction_still_removes_the_staging_directory(self):
        """The exit path no `rm -rf` at a failure site can ever cover.

        The tar shim sends SIGTERM to install.sh and then exits 0, so the script
        is interrupted with the archive and the checksum file already written
        into the staging directory, at a point where it believed it was about to
        succeed. Only a trap can clean that up.
        """
        run = self._run(tar_shim="signal")

        assert run.result.returncode == 143, (
            "install.sh must handle SIGTERM and exit 128+15 rather than dying "
            f"where it stands; exit={run.result.returncode}\n{run.result.stderr}"
        )
        assert not run.installed.exists(), (
            "an interrupted run must not install anything"
        )
        assert "tar" in run.extractions, (
            "the run must actually have reached the extraction, or the signal "
            "arrived somewhere this test does not claim to cover"
        )
        self._assert_staging_parent_is_empty(run, "a run interrupted by SIGTERM")

    # -- fixture arrangement --------------------------------------------------

    def _reset_fixtures(self):
        for path in self.fixtures.iterdir():
            path.unlink()
        self._write_tar(self.unix_archive, self.unix_payload)
        with zipfile.ZipFile(self.win_archive, "w") as zf:
            zf.writestr("rmp.exe", self.win_payload)
        write_checksum(self.unix_archive)
        write_checksum(self.win_archive)

    def _break_archive_download(self):
        self.unix_archive.unlink()

    def _break_checksum_download(self):
        self.unix_archive.with_name(self.unix_name + ".sha256").unlink()

    def _break_substituted_archive(self):
        self._write_tar(self.unix_archive, b"GROADMAP-TEST-SUBSTITUTED-ARCHIVE\n")

    def _break_foreign_checksum(self):
        write_checksum(self.unix_archive, name=f"rmp-{VERSION}-darwin-arm64.tar.gz")

    def _break_malformed_checksum(self):
        self.unix_archive.with_name(self.unix_name + ".sha256").write_text(
            f"not-a-digest  {self.unix_name}\n", encoding="utf-8")

    def _break_empty_checksum(self):
        self.unix_archive.with_name(self.unix_name + ".sha256").write_text("")

    def _break_corrupt_archive(self):
        self.unix_archive.write_bytes(b"this is not a gzip stream at all\n")
        write_checksum(self.unix_archive)

    def _break_archive_without_binary(self):
        self._write_tar(self.unix_archive, b"see the release notes\n", member="README.txt")
        write_checksum(self.unix_archive)


class _StagingRun:
    """What one sandboxed run left behind, for the staging assertions."""

    def __init__(self, result, installed, requests, extractions, mktemp_calls,
                 staged, survivors):
        self.result = result
        self.installed = installed
        self.requests = requests
        self.extractions = extractions
        self.mktemp_calls = mktemp_calls
        self.staged = staged
        self.survivors = survivors


class _Run:
    """What one sandboxed install.sh run left behind, for the assertions."""

    def __init__(self, result, installed, requests, extractions, leaked_tmpdirs):
        self.result = result
        self.installed = installed
        self.requests = requests
        self.extractions = extractions
        self.leaked_tmpdirs = leaked_tmpdirs


def _test_classes():
    """Every test class in this module, discovered rather than listed.

    A runner that names its classes one by one silently ignores the next class
    someone adds, which is how a test file grows coverage that never runs (rmp
    task #303). Asking the module what it holds removes that failure mode: the
    count printed below is the count that ran.
    """
    return [
        obj
        for name, obj in sorted(globals().items())
        if isinstance(obj, type) and name.endswith("Tests")
    ]


def _run_all():
    classes = _test_classes()
    passed = 0
    failed = 0
    failures = []
    print("=" * 60)
    print("install.sh extraction, checksum and staging contract "
          "(tasks #138, #185, #309)")
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
            except AssertionError as exc:
                failed += 1
                failures.append((f"{cls.__name__}.{m}", exc))
                print(f"  FAIL {m}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((f"{cls.__name__}.{m}", exc))
                print(f"  FAIL {m} (error)")
            finally:
                instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Install script tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\nFAIL {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
