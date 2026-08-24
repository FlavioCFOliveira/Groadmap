#!/usr/bin/env python3
"""
Test 53: E2E harness binary resolution and staleness guard (rmp task #271).

Before this fix, GroadmapTestBase._find_cli() returned the FIRST of four
candidate paths that existed -- bin/rmp and rmp under the repository root,
then the same two under the process's current working directory -- and
never compared any of them against the source tree. It built the binary
only when none of the four candidates existed. Two consequences followed:

  1. A `go build` run once, followed by ANY edit under cmd/ or internal/,
     left the suite silently exercising the pre-edit binary forever. Every
     module "passed" against code that no longer existed.
  2. Running the suite from a directory that happened to contain an
     unrelated bin/rmp (or rmp) drove the whole run against a foreign
     binary, with nothing in the output saying so.

tests/base_test.py now exposes resolve_cli(), which restricts candidates to
the repository root, rebuilds whenever the chosen binary predates the newest
source compiled into it, raises instead of falling back to a stale binary
when the rebuild itself fails, and memoizes its result (and its startup
banner) once per interpreter process.

"Newest source compiled into it" is deliberately not ".go file": rmp web
(internal/web/embed.go) embeds `templates/*.html` and `static` straight into
the binary via //go:embed, so a template or a static asset can change the
binary's behaviour while every .go file's mtime stays put. A staleness check
that only watched .go files would certify a binary whose rendered pages no
longer match the templates on disk -- exactly the kind of gap this suite
exists to close. resolve_cli() therefore compares against
_newest_source_mtime(), which walks every .go file PLUS every directory a
//go:embed directive pulls in (derived from the directive itself rather than
hardcoded, see _embedded_roots() in tests/base_test.py).

This module drives resolve_cli() directly against disposable git worktrees
-- real, buildable copies of go.mod/go.sum/cmd/internal sharing the main
worktree's object store and module cache -- so every scenario is empirical
and none of it touches this repository's own bin/rmp or the mtime of any
tracked source file.

Coverage
--------
1. No candidate binary in the module root -> resolve_cli() builds one.
2. A binary newer than every source file -> reused, never rebuilt.
3. A binary older than a touched .go source -> rebuilt before being handed
   back (THIS is the staleness branch task #271 exists to add).
4. A binary older than a touched //go:embed'd template asset, with every
   .go file left untouched -> rebuilt all the same (the embedded-asset half
   of the same staleness branch).
5. A rebuild that cannot compile -> resolve_cli() raises, naming the
   failure, and the old binary is left exactly as it was (never silently
   reused).
6. A bin/rmp (and rmp) planted in an unrelated current working directory
   -> never selected, staleness aside.
7. GroadmapTestBase() instantiated more than once in one process -> the
   startup banner (resolved path + build identity) is printed exactly once.
"""

import os
import shutil
import subprocess
import sys
import tempfile
import textwrap
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import REPO_ROOT, resolve_cli


def _new_worktree() -> Path:
    """Create a throwaway git worktree checked out at HEAD.

    Gives each scenario below its own real, buildable copy of go.mod,
    go.sum, cmd/ and internal/, isolated from this repository's actual
    bin/rmp and from the mtime of any file git tracks. Worktree creation
    shares the main worktree's object store, and `go build` inside it shares
    the host's module cache, so no network access is required.
    """
    target = Path(tempfile.mkdtemp(prefix="groadmap_staleness_wt_"))
    target.rmdir()  # `git worktree add` insists on creating the directory itself
    subprocess.run(
        ["git", "worktree", "add", "--detach", str(target), "HEAD"],
        cwd=REPO_ROOT,
        check=True,
        capture_output=True,
        text=True,
    )
    return target


def _remove_worktree(path: Path) -> None:
    """Remove a worktree created by _new_worktree().

    Tolerates a path a failed test already cleaned up (or never finished
    creating): `git worktree remove` is best-effort here, followed by an
    unconditional rmtree so no scratch directory survives the test.
    """
    subprocess.run(
        ["git", "worktree", "remove", "--force", str(path)],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
    )
    shutil.rmtree(path, ignore_errors=True)


def _any_go_source(worktree: Path) -> Path:
    """Return some .go file under internal/ in the given worktree."""
    return next((worktree / "internal").rglob("*.go"))


def _any_embedded_template(worktree: Path) -> Path:
    """Return a real //go:embed'd template file in the given worktree.

    internal/web/templates/*.html is embedded by internal/web/embed.go
    (`//go:embed templates/*.html`); index.html is one of the pages that
    directive pulls in.
    """
    path = worktree / "internal" / "web" / "templates" / "index.html"
    assert path.is_file(), f"expected embedded template not found: {path}"
    return path


class TestBuildsWhenNoCandidateBinary:
    """No bin/rmp or rmp exists in the module root: resolve_cli() must
    build one there, matching the pre-existing last-resort build path."""

    def setup_method(self):
        self.worktree = _new_worktree()

    def teardown_method(self):
        _remove_worktree(self.worktree)

    def test_builds_a_fresh_binary_in_the_module_root(self):
        assert not (self.worktree / "bin" / "rmp").exists()
        assert not (self.worktree / "rmp").exists()

        resolved = resolve_cli(repo_root=self.worktree, memoize=False)

        expected = str((self.worktree / "bin" / "rmp").resolve())
        assert resolved == expected, f"expected {expected}, got {resolved}"
        assert Path(resolved).is_file()

        proc = subprocess.run([resolved, "--version"], capture_output=True, text=True)
        assert proc.returncode == 0, proc.stderr
        assert "Groadmap" in proc.stdout, proc.stdout

        print("✓ resolve_cli() builds bin/rmp when no candidate binary exists")


class TestReusesCurrentBinary:
    """A binary newer than every .go source in the module is reused as-is:
    asking again must not trigger a second, unnecessary build."""

    def setup_method(self):
        self.worktree = _new_worktree()
        self.first = resolve_cli(repo_root=self.worktree, memoize=False)
        self.first_mtime = Path(self.first).stat().st_mtime

    def teardown_method(self):
        _remove_worktree(self.worktree)

    def test_second_resolution_does_not_rebuild(self):
        second = resolve_cli(repo_root=self.worktree, memoize=False)
        second_mtime = Path(second).stat().st_mtime

        assert second == self.first
        assert second_mtime == self.first_mtime, (
            "resolve_cli() rebuilt a binary that was already newer than every "
            f".go source: mtime moved from {self.first_mtime} to {second_mtime}"
        )

        print("✓ resolve_cli() reuses a current binary without rebuilding")


class TestRebuildsStaleBinary:
    """The staleness branch itself: task #271's central acceptance
    criterion. A .go source file touched AFTER the binary was built must
    force a rebuild before resolve_cli() hands the binary back."""

    def setup_method(self):
        self.worktree = _new_worktree()
        self.first = resolve_cli(repo_root=self.worktree, memoize=False)
        self.first_mtime = Path(self.first).stat().st_mtime

        # Bump one real .go source file's mtime well past the binary's own,
        # without touching its content -- mtime is exactly what resolve_cli()
        # compares, so this reproduces "edited a source file, forgot to
        # rebuild" deterministically regardless of filesystem clock
        # resolution.
        self.touched_source = _any_go_source(self.worktree)
        future = self.first_mtime + 120
        os.utime(self.touched_source, (future, future))

    def teardown_method(self):
        _remove_worktree(self.worktree)

    def test_stale_binary_is_rebuilt_before_use(self):
        resolved = resolve_cli(repo_root=self.worktree, memoize=False)
        rebuilt_mtime = Path(resolved).stat().st_mtime

        assert resolved == self.first, "the resolved PATH must not move, only its content"
        assert rebuilt_mtime > self.first_mtime, (
            "a binary older than a touched .go source was handed back unchanged "
            f"(mtime stayed at {self.first_mtime}); the staleness comparison did not fire"
        )
        # The touched source's mtime was deliberately pushed into the future
        # (first_mtime + 120s) so the comparison is unambiguous regardless of
        # filesystem clock resolution; the rebuild itself still runs at the
        # real wall clock, which has not reached that future instant, so the
        # meaningful assertion is "a rebuild happened" (above), not "the new
        # binary is newer than the synthetic future timestamp".

        print("✓ resolve_cli() rebuilds a binary that predates the newest .go source")


class TestRebuildsStaleEmbeddedAsset:
    """The embedded-asset half of the staleness branch: internal/web/embed.go
    compiles internal/web/templates/*.html and internal/web/static straight
    into the binary via //go:embed. A template touched AFTER the build, with
    every .go file left completely untouched, must still force a rebuild --
    otherwise the suite could certify a binary whose rendered pages no
    longer match the templates on disk, which is exactly what
    test_35_web_interface.py's 5777 lines exist to catch downstream."""

    def setup_method(self):
        self.worktree = _new_worktree()
        self.first = resolve_cli(repo_root=self.worktree, memoize=False)
        self.first_mtime = Path(self.first).stat().st_mtime

        # Bump one real embedded template's mtime well past the binary's
        # own, exactly like TestRebuildsStaleBinary does for a .go file --
        # except NO .go file is touched here, so this scenario only passes
        # if the embedded-asset half of _newest_source_mtime() is doing its
        # job, not the .go half.
        self.touched_template = _any_embedded_template(self.worktree)
        self.pre_touch_go_mtimes = {
            path: path.stat().st_mtime
            for path in (self.worktree / "internal").rglob("*.go")
        }
        future = self.first_mtime + 120
        os.utime(self.touched_template, (future, future))

    def teardown_method(self):
        _remove_worktree(self.worktree)

    def test_stale_embedded_template_is_rebuilt_before_use(self):
        # Confirm the premise: not one .go file moved.
        for path, mtime in self.pre_touch_go_mtimes.items():
            assert path.stat().st_mtime == mtime, (
                f"test setup touched a .go file ({path}); this scenario must "
                "isolate the embedded-asset path from the .go path"
            )

        resolved = resolve_cli(repo_root=self.worktree, memoize=False)
        rebuilt_mtime = Path(resolved).stat().st_mtime

        assert resolved == self.first, "the resolved PATH must not move, only its content"
        assert rebuilt_mtime > self.first_mtime, (
            "a binary older than a touched //go:embed'd template was handed back "
            f"unchanged (mtime stayed at {self.first_mtime}); the embedded-asset "
            "half of the staleness comparison did not fire"
        )

        print(
            "✓ resolve_cli() rebuilds a binary that predates a touched "
            "//go:embed'd template, with every .go file left untouched"
        )


class TestRebuildFailureFailsLoudly:
    """A module that cannot compile must make resolve_cli() raise, naming
    the failure -- never silently fall back to the stale binary."""

    def setup_method(self):
        self.worktree = _new_worktree()
        self.first = resolve_cli(repo_root=self.worktree, memoize=False)
        self.first_mtime = Path(self.first).stat().st_mtime

        # A syntactically invalid file breaks `go build ./cmd/rmp` for the
        # whole module regardless of which package it lands in, and its
        # mtime is newer than the binary so resolve_cli() attempts a rebuild.
        self.broken_source = self.worktree / "internal" / "utils" / "staleness_probe_broken.go"
        self.broken_source.write_text("package utils\n\nfunc( this is not valid Go syntax {\n")
        future = self.first_mtime + 120
        os.utime(self.broken_source, (future, future))

    def teardown_method(self):
        if self.broken_source.exists():
            self.broken_source.unlink()
        _remove_worktree(self.worktree)

    def test_broken_source_raises_instead_of_returning_stale_binary(self):
        try:
            resolve_cli(repo_root=self.worktree, memoize=False)
        except RuntimeError as exc:
            message = str(exc)
            assert "build" in message.lower(), f"failure message does not name the build: {message}"
        else:
            raise AssertionError(
                "resolve_cli() returned a path instead of raising when the module "
                "failed to compile"
            )

        # A failed `go build -o` never writes its output, so the artefact
        # from setup_method must be exactly what it was -- resolve_cli() must
        # not have substituted anything else in its place.
        assert Path(self.first).stat().st_mtime == self.first_mtime, (
            "the stale binary was modified even though the rebuild failed"
        )

        print("✓ resolve_cli() raises on a failed rebuild instead of reusing the stale binary")


class TestUnrelatedCwdBinaryNeverSelected:
    """A bin/rmp (or rmp) that happens to live in the current working
    directory -- but not in the module root -- must never be chosen."""

    def setup_method(self):
        self.worktree = _new_worktree()
        self.original_cwd = os.getcwd()
        self.foreign_dir = Path(tempfile.mkdtemp(prefix="groadmap_foreign_cwd_"))

        foreign_bin_dir = self.foreign_dir / "bin"
        foreign_bin_dir.mkdir()
        for foreign_binary in (foreign_bin_dir / "rmp", self.foreign_dir / "rmp"):
            foreign_binary.write_text("#!/bin/sh\necho FOREIGN-BINARY\nexit 0\n")
            foreign_binary.chmod(0o755)

        os.chdir(self.foreign_dir)

    def teardown_method(self):
        os.chdir(self.original_cwd)
        shutil.rmtree(self.foreign_dir, ignore_errors=True)
        _remove_worktree(self.worktree)

    def test_resolution_ignores_cwd_candidates(self):
        resolved = resolve_cli(repo_root=self.worktree, memoize=False)

        assert str(self.foreign_dir) not in resolved, (
            f"resolved to a binary planted under the foreign cwd: {resolved}"
        )
        assert resolved.startswith(str(self.worktree)), (
            f"resolved binary {resolved} is not inside the module root {self.worktree}"
        )

        proc = subprocess.run([resolved, "--version"], capture_output=True, text=True)
        assert "FOREIGN-BINARY" not in proc.stdout, (
            "the foreign cwd binary answered --version instead of the real one"
        )
        assert "Groadmap" in proc.stdout, proc.stdout

        print("✓ resolve_cli() never selects a binary from the current working directory")


class TestStartupBannerPrintedOnce:
    """The resolved path and build identity are announced once per process,
    matching every GroadmapTestBase() instance a module creates to the same
    resolution instead of repeating the staleness check (and the banner)
    per test class."""

    def test_banner_appears_exactly_once_across_three_instances(self):
        script = textwrap.dedent(
            f"""
            import sys
            sys.path.insert(0, {str(REPO_ROOT)!r})
            from tests.base_test import GroadmapTestBase
            GroadmapTestBase()
            GroadmapTestBase()
            GroadmapTestBase()
            """
        )
        proc = subprocess.run(
            [sys.executable, "-c", script], capture_output=True, text=True, cwd=str(REPO_ROOT)
        )
        assert proc.returncode == 0, proc.stderr

        occurrences = proc.stdout.count("[groadmap-tests] resolved CLI binary:")
        assert occurrences == 1, (
            "expected exactly one startup banner for three GroadmapTestBase() "
            f"instances in one process, got {occurrences}\nstdout={proc.stdout}"
        )
        assert (
            str(REPO_ROOT / "bin" / "rmp") in proc.stdout or str(REPO_ROOT / "rmp") in proc.stdout
        ), f"banner did not print the resolved path:\n{proc.stdout}"
        assert "identity:" in proc.stdout

        print("✓ the resolution banner is printed exactly once per process")


def _run_all():
    suites = [
        TestBuildsWhenNoCandidateBinary,
        TestReusesCurrentBinary,
        TestRebuildsStaleBinary,
        TestRebuildsStaleEmbeddedAsset,
        TestRebuildFailureFailsLoudly,
        TestUnrelatedCwdBinaryNeverSelected,
        TestStartupBannerPrintedOnce,
    ]
    passed = 0
    failed = 0
    failures = []

    for cls in suites:
        cls_name = cls.__name__
        methods = sorted(m for m in dir(cls) if m.startswith("test_"))
        for m in methods:
            inst = cls()
            if hasattr(inst, "setup_method"):
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
                if hasattr(inst, "teardown_method"):
                    try:
                        inst.teardown_method()
                    except Exception:
                        pass

    print("\n" + "=" * 60)
    print(f"E2E harness binary staleness tests: {passed} passed, {failed} failed")
    print("=" * 60)
    if failures:
        for name, exc in failures:
            print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
