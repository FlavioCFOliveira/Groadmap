#!/usr/bin/env python3
"""
Test 49: install.sh platform guards (regression for task #156).

SPEC/DEPLOY.md Architecture Detection specifies that 32-bit x86 is not a target
the build produces, and that the script stops with a stated message and exit
code 1. install.sh did the opposite: it mapped `i386`/`i686` to `arch="386"`,
and the guard that followed rejected only the value `unknown`. An unsupported
architecture therefore passed straight through detection and failed much later,
on an HTTP download for `rmp-<version>-linux-386.tar.gz` -- an asset no release
has ever produced. The user saw a download error instead of being told their
platform is not supported.

This is the regression guard. It drives the real install.sh end to end in a
hermetic sandbox -- stubbed `uname` and `curl`, a PATH holding only the tools
under test -- and pins four properties:

  1. An unsupported architecture (i386, i686) stops at detection with the
     specified message and exit 1.
  2. An unrecognised architecture stops the same way. detect_arch() returns
     `unsupported` for the first class and `unknown` for the second, and the
     guard must reject both -- rejecting only one is how this bug happened.
  3. Nothing is downloaded when a guard fires. This is the property that makes
     the failure early rather than late, so it is asserted directly: the curl
     stub records every invocation, and the recording must be empty. Asserting
     only the exit code would pass even if the script fetched the release
     metadata first.
  4. Every supported architecture still resolves to its documented target. A
     guard that rejects too much is as broken as one that rejects too little,
     so each mapping in the SPEC table is driven end to end and checked against
     the asset name the script actually asks for.

The download is never satisfied: the curl stub records the URL and fails, so the
supported-architecture cases assert the requested asset name rather than a
completed installation, which test_47 already covers. No network is involved.

A note on the runner below, which is itself a regression fix (rmp task #185).
This module shipped with no `if __name__ == "__main__"` block and no runner, so
`python3 tests/test_49_install_platform_guards.py` -- exactly how run_tests.py
executes it -- defined these classes, ran nothing, and exited 0. The suite
counted the module as PASSED for every one of those runs. The five tests below
all pass, and always did; they simply never ran. The runner discovers its test
classes instead of listing them, so the same silence cannot return through a
class that a hardcoded list forgets.
"""

import shutil
import stat
import subprocess
import sys
import tempfile
from pathlib import Path

VERSION = "v9.9.9"
REPO_ROOT = Path(__file__).resolve().parent.parent
INSTALL_SH = REPO_ROOT / "install.sh"

REQUIRED_TOOLS = [
    "bash", "grep", "sed", "head", "mkdir", "rm", "mv", "cp",
    "chmod", "tar", "gzip", "gunzip", "cat", "basename", "dirname",
    # install.sh refuses to install anything it cannot verify, so a host with
    # no SHA-256 tool stops at a guard of its own -- before the download these
    # cases assert on. The sandbox provides the tool; the refusal itself is
    # driven by test_47's InstallScriptChecksumTests.
    "sha256sum",
]

UNAME_SHIM = """#!/usr/bin/env bash
case "$1" in
  -s) echo "$FAKE_UNAME_S" ;;
  -m) echo "$FAKE_UNAME_M" ;;
  *)  echo "$FAKE_UNAME_S" ;;
esac
"""

# Records every URL it is asked for, then fails. The recording is the evidence
# for property 3: if a guard fires, this file must stay empty.
CURL_SHIM = """#!/usr/bin/env bash
url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) shift 2 ;;
    -*) shift ;;
    *)  url="$1"; shift ;;
  esac
done
echo "$url" >> "$FAKE_CURL_LOG"
if [[ "$url" == *api.github.com* ]]; then
  echo "{\\"tag_name\\": \\"${FAKE_VERSION}\\"}"
  exit 0
fi
exit 22
"""

# The exact messages SPEC/DEPLOY.md specifies, without the ERROR: prefix the
# error helper supplies (see SPEC/DEPLOY.md Diagnostic Output). stderr always
# carries ANSI escapes around that prefix, so assertions compare the message
# text rather than the raw line.
ARCH_MESSAGE = (
    "is not supported. Supported targets: amd64, arm64, armv6, armv7. "
    "See SPEC/BUILD.md for the build matrix."
)
OS_MESSAGE = (
    "is not supported. Supported systems: linux, darwin, freebsd, openbsd, "
    "windows. See SPEC/BUILD.md for the build matrix."
)

# SPEC/DEPLOY.md Architecture Detection, one case per row of the table.
SUPPORTED_ARCHITECTURES = [
    ("x86_64", "amd64"),
    ("amd64", "amd64"),
    ("aarch64", "arm64"),
    ("arm64", "arm64"),
    ("armv6l", "armv6"),
    ("armv7l", "armv7"),
]


class InstallPlatformGuardTests:
    """Drives install.sh with the reported platform fully under control."""

    def setup_method(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="rmp_install_guard_"))
        self.bin = self.tmp / "bin"
        self.home = self.tmp / "home"
        for d in (self.bin, self.home):
            d.mkdir(parents=True)
        self.curl_log = self.tmp / "curl.log"
        self.curl_log.write_text("")

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

    def _run(self, uname_m, uname_s="Linux"):
        self.curl_log.write_text("")
        result = subprocess.run(
            [shutil.which("bash"), str(INSTALL_SH)],
            input="u\n",
            capture_output=True,
            text=True,
            env={
                "PATH": str(self.bin),
                "HOME": str(self.home),
                "FAKE_UNAME_S": uname_s,
                "FAKE_UNAME_M": uname_m,
                "FAKE_VERSION": VERSION,
                "FAKE_CURL_LOG": str(self.curl_log),
            },
        )
        return result, self.curl_log.read_text()

    def _assert_rejected(self, uname_m, expected_fragment, message, uname_s="Linux"):
        result, requests = self._run(uname_m, uname_s=uname_s)

        assert result.returncode == 1, (
            f"{uname_m!r} on {uname_s!r} must be rejected with exit 1 per "
            f"SPEC/DEPLOY.md Platform Detection; got exit {result.returncode}\n"
            f"stdout={result.stdout}\nstderr={result.stderr}"
        )
        assert expected_fragment in result.stderr, (
            f"the error must name the raw uname output {expected_fragment!r}, "
            f"because the mapping is what failed; stderr was:\n{result.stderr}"
        )
        assert message in result.stderr, (
            f"the message must be the one SPEC/DEPLOY.md specifies; "
            f"expected to contain:\n  {message}\ngot:\n  {result.stderr}"
        )
        assert requests.strip() == "", (
            f"nothing may be requested once a platform guard fires -- the whole "
            f"point is failing at detection rather than late on a download. "
            f"install.sh asked for:\n{requests}"
        )

    def test_i386_is_rejected_before_anything_is_downloaded(self):
        """The defect: i386 mapped to 386 and failed later on a missing asset."""
        self._assert_rejected("i386", "i386", ARCH_MESSAGE)

    def test_i686_is_rejected_before_anything_is_downloaded(self):
        """The other half of the same case class."""
        self._assert_rejected("i686", "i686", ARCH_MESSAGE)

    def test_unrecognised_architecture_is_rejected(self):
        """detect_arch returns `unknown` here; the guard must reject it too."""
        self._assert_rejected("sparc64", "sparc64", ARCH_MESSAGE)

    def test_unsupported_operating_system_is_rejected(self):
        """The OS guard follows the same contract as the architecture guard."""
        self._assert_rejected("x86_64", "SunOS", OS_MESSAGE, uname_s="SunOS")

    def test_supported_architectures_still_resolve_to_their_target(self):
        """A guard that rejects too much would be just as broken."""
        for uname_m, expected_arch in SUPPORTED_ARCHITECTURES:
            result, requests = self._run(uname_m)

            assert ARCH_MESSAGE not in result.stderr, (
                f"{uname_m!r} is a supported architecture per SPEC/DEPLOY.md and "
                f"must not be rejected; stderr was:\n{result.stderr}"
            )
            expected_asset = f"rmp-{VERSION}-linux-{expected_arch}.tar.gz"
            assert expected_asset in requests, (
                f"{uname_m!r} must resolve to the {expected_arch!r} target and "
                f"request {expected_asset!r}; install.sh asked for:\n{requests}"
            )


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
    print("install.sh platform guards (task #156)")
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
    print(f"Install platform guard tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\nFAIL {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
