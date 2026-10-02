#!/usr/bin/env python3
"""
Test 78: the release body's CHANGELOG.md section (scripts/changelog-section.sh).

SPEC/DEPLOY.md section "GitHub Release Body" makes the body of every GitHub
Release the version's section of CHANGELOG.md. The release job extracts it with
scripts/changelog-section.sh before it creates the release, and the same script
backfills the bodies of releases published before the rule existed, so it must
be right for every section the file holds.

The rule under test: the section is the one whose heading line is exactly
`## [X.Y.Z]` or `## [X.Y.Z] - YYYY-MM-DD`, X.Y.Z being the tag without its
leading `v`. Its body is every line after the heading up to, but excluding, the
next line that starts with `## [`, or up to the end of the file, with leading
and trailing blank lines removed and the lines between them kept exactly. No
heading, an empty body, and more than one heading for the version each fail
with a message on standard error that names the version, and nothing on
standard output.

The module drives the script as a subprocess against temporary CHANGELOG
fixtures written in the shape the real file uses, and against the real
CHANGELOG.md. It runs no `rmp` command.
"""

import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
SCRIPT = REPO_ROOT / "scripts" / "changelog-section.sh"
REAL_CHANGELOG = REPO_ROOT / "CHANGELOG.md"

PREAMBLE = """# Changelog

All notable changes to **Groadmap** (`rmp`) are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
"""

# The body of each fixture section, exactly as the script must print it. Every
# body carries an interior blank line, so trimming must stop at the ends.
BODY_140 = """### Added

- `rmp sprint reorder` moves a task to a given position inside its sprint.
- `rmp task next` accepts a count, and returns the next N tasks in sprint order.

### Fixed

- `rmp audit list --since` no longer drops entries recorded in the last second."""

BODY_131 = """### Fixed

- A sprint closed while a task was DOING no longer reports it as COMPLETED.

  The task keeps its status, and `rmp sprint show` lists it under carryover."""

BODY_130 = """### Changed

- The task list orders by priority, then by creation date.

### Security

- `project.db` is created with mode 0600, and its directory with mode 0700."""


class ScriptCase:
    """Shared plumbing: a scratch directory per test and a script runner."""

    def setup_method(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="rmp_changelog_section_"))

    def teardown_method(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def write_changelog(self, text: str, name: str = "CHANGELOG.md") -> Path:
        path = self.tmp / name
        path.write_text(text, encoding="utf-8")
        return path

    def run(self, version: str, changelog: Path):
        result = subprocess.run(
            ["bash", str(SCRIPT), version, str(changelog)],
            capture_output=True, text=True, timeout=30,
        )
        return result.returncode, result.stdout, result.stderr

    def assert_body(self, version: str, changelog: Path, expected: str):
        code, out, err = self.run(version, changelog)
        assert code == 0, f"{version}: exit {code}, stderr:\n{err}"
        assert err == "", f"{version}: unexpected stderr:\n{err}"
        assert out == expected + "\n", (
            f"{version}: body differs.\n--- expected ---\n{expected}\n--- got ---\n{out}"
        )

    def assert_refused(self, version: str, changelog: Path, *fragments: str) -> str:
        code, out, err = self.run(version, changelog)
        assert code == 1, f"{version}: expected exit 1, got {code}; stderr:\n{err}"
        assert out == "", f"{version}: a refusal must print nothing on stdout, got:\n{out}"
        bare = version[1:] if version.startswith("v") else version
        assert bare in err, f"{version}: stderr does not name the version:\n{err}"
        for fragment in fragments:
            assert fragment in err, f"{version}: stderr lacks {fragment!r}:\n{err}"
        assert err.count("\n") == 1, f"{version}: expected one stderr line, got:\n{err}"
        return err


def three_release_changelog() -> str:
    """1.4.0 (dated) on top, 1.3.1 (undated) in the middle, 1.3.0 (dated) last,
    each heading surrounded by extra blank lines the body must not carry."""
    return (
        f"{PREAMBLE}\n"
        f"## [1.4.0] - 2026-06-01\n\n{BODY_140}\n\n\n"
        f"## [1.3.1]\n\n\n{BODY_131}\n\n"
        f"## [1.3.0] - 2026-05-13\n\n{BODY_130}\n\n\n"
    )


class TestSectionBody(ScriptCase):
    def test_middle_section_ends_before_the_next_heading(self):
        changelog = self.write_changelog(three_release_changelog())
        self.assert_body("1.3.1", changelog, BODY_131)

    def test_last_section_runs_to_the_end_of_the_file(self):
        changelog = self.write_changelog(three_release_changelog())
        self.assert_body("1.3.0", changelog, BODY_130)

    def test_last_section_without_a_final_newline(self):
        changelog = self.write_changelog(f"{PREAMBLE}\n## [1.3.0] - 2026-05-13\n\n{BODY_130}")
        self.assert_body("1.3.0", changelog, BODY_130)

    def test_dated_heading_is_selected(self):
        changelog = self.write_changelog(three_release_changelog())
        self.assert_body("1.4.0", changelog, BODY_140)

    def test_undated_heading_is_selected(self):
        changelog = self.write_changelog(three_release_changelog())
        code, out, _ = self.run("1.3.1", changelog)
        assert code == 0 and out.startswith("### Fixed\n"), out

    def test_blank_lines_are_trimmed_at_both_ends_only(self):
        """Whitespace-only lines at the ends go; the interior is byte-exact,
        including an interior blank line and trailing spaces on a kept line."""
        body = "### Fixed\n\n- `rmp web` serves the graph page again.  \n\n\t\n- Indented note."
        changelog = self.write_changelog(
            f"{PREAMBLE}\n## [1.8.1] - 2026-06-03\n \n\n\t\n{body}\n\n  \n\n## [1.8.0] - 2026-06-03\n\n{BODY_130}\n"
        )
        self.assert_body("1.8.1", changelog, body)

    def test_heading_line_is_not_part_of_the_body(self):
        changelog = self.write_changelog(three_release_changelog())
        _, out, _ = self.run("1.4.0", changelog)
        assert "## [" not in out, out

    def test_leading_v_is_accepted(self):
        changelog = self.write_changelog(three_release_changelog())
        self.assert_body("v1.4.0", changelog, BODY_140)
        self.assert_body("v1.3.1", changelog, BODY_131)

    def test_default_path_is_changelog_md_in_the_working_directory(self):
        self.write_changelog(three_release_changelog())
        result = subprocess.run(
            ["bash", str(SCRIPT), "v1.3.1"], cwd=self.tmp,
            capture_output=True, text=True, timeout=30,
        )
        assert result.returncode == 0, result.stderr
        assert result.stdout == BODY_131 + "\n", result.stdout


class TestPrefixSafety(ScriptCase):
    def _changelog(self) -> Path:
        return self.write_changelog(
            f"{PREAMBLE}\n"
            "## [11.1.0] - 2026-09-01\n\n- Eleven-series release.\n\n"
            "## [1.1.0-rc1] - 2026-08-20\n\n- Release candidate.\n\n"
            "## [1.1.0] - 2026-08-21\n\n- The real 1.1.0 release.\n\n"
            "## [1.1.01] - 2026-08-22\n\n- A malformed neighbour.\n\n"
            "## [1x1x0] - 2026-08-23\n\n- Dots are not wildcards.\n"
        )

    def test_exact_version_wins_over_its_prefixed_and_suffixed_neighbours(self):
        self.assert_body("1.1.0", self._changelog(), "- The real 1.1.0 release.")

    def test_release_candidate_and_eleven_series_are_selected_only_by_name(self):
        changelog = self._changelog()
        self.assert_body("1.1.0-rc1", changelog, "- Release candidate.")
        self.assert_body("11.1.0", changelog, "- Eleven-series release.")

    def test_a_version_present_only_as_a_neighbour_prefix_is_missing(self):
        changelog = self.write_changelog(
            f"{PREAMBLE}\n"
            "## [11.1.0] - 2026-09-01\n\n- Eleven-series release.\n\n"
            "## [1.1.0-rc1] - 2026-08-20\n\n- Release candidate.\n"
        )
        self.assert_refused("1.1.0", changelog, "no section")

    def test_heading_with_text_other_than_a_date_does_not_match(self):
        changelog = self.write_changelog(
            f"{PREAMBLE}\n## [1.1.0] - unreleased\n\n- Draft notes.\n"
        )
        self.assert_refused("1.1.0", changelog, "no section")


class TestRefusals(ScriptCase):
    def test_missing_heading_names_the_version(self):
        changelog = self.write_changelog(three_release_changelog())
        self.assert_refused("v1.5.0", changelog, "no section")

    def test_empty_body_names_the_version(self):
        changelog = self.write_changelog(
            f"{PREAMBLE}\n## [1.4.0] - 2026-06-01\n\n{BODY_140}\n\n"
            "## [1.3.1] - 2026-05-20\n\n \n\t\n\n"
            f"## [1.3.0] - 2026-05-13\n\n{BODY_130}\n"
        )
        self.assert_refused("1.3.1", changelog, "is empty")

    def test_empty_last_section_names_the_version(self):
        changelog = self.write_changelog(
            f"{PREAMBLE}\n## [1.4.0] - 2026-06-01\n\n{BODY_140}\n\n## [1.3.0]\n\n\n"
        )
        self.assert_refused("1.3.0", changelog, "is empty")

    def _assert_duplicate(self, text: str):
        changelog = self.write_changelog(text)
        self.assert_refused("1.3.1", changelog, "duplicate sections", "merge them into one")

    def test_two_dated_headings_are_duplicates(self):
        self._assert_duplicate(
            f"{PREAMBLE}\n## [1.3.1] - 2026-05-20\n\n{BODY_131}\n\n"
            f"## [1.3.1] - 2026-05-21\n\n{BODY_130}\n"
        )

    def test_two_undated_headings_are_duplicates(self):
        self._assert_duplicate(
            f"{PREAMBLE}\n## [1.3.1]\n\n{BODY_131}\n\n## [1.3.1]\n\n{BODY_130}\n"
        )

    def test_dated_and_undated_headings_are_duplicates(self):
        self._assert_duplicate(
            f"{PREAMBLE}\n## [1.3.1]\n\n{BODY_131}\n\n"
            f"## [1.4.0] - 2026-06-01\n\n{BODY_140}\n\n"
            f"## [1.3.1] - 2026-05-20\n\n{BODY_130}\n"
        )

    def test_duplicate_with_an_empty_copy_is_still_a_duplicate(self):
        self._assert_duplicate(
            f"{PREAMBLE}\n## [1.3.1] - 2026-05-20\n\n## [1.3.1]\n\n{BODY_131}\n"
        )


class TestRealChangelog(ScriptCase):
    @staticmethod
    def _lines():
        return REAL_CHANGELOG.read_text(encoding="utf-8").split("\n")

    def test_1_18_0_is_the_lines_between_its_heading_and_1_17_3(self):
        lines = self._lines()
        start = lines.index("## [1.18.0] - 2026-10-02")
        end = lines.index("## [1.17.3] - 2026-09-18")
        body = lines[start + 1:end]
        while body and body[0].strip() == "":
            body.pop(0)
        while body and body[-1].strip() == "":
            body.pop()
        assert body, "the 1.18.0 section of CHANGELOG.md is empty"
        self.assert_body("v1.18.0", REAL_CHANGELOG, "\n".join(body))

    def test_every_version_heading_extracts(self):
        headings = [
            m.group(1)
            for m in (re.match(r"^## \[([^\]]+)\]", line) for line in self._lines())
            if m
        ]
        assert len(headings) >= 27, f"expected the released headings, found {headings}"
        assert len(headings) == len(set(headings)), f"duplicate headings: {headings}"
        for version in headings:
            code, out, err = self.run(f"v{version}", REAL_CHANGELOG)
            assert code == 0, f"{version}: exit {code}: {err}"
            assert out.strip() != "", f"{version}: empty body"
            assert not out.startswith("\n") and not out.endswith("\n\n"), (
                f"{version}: body carries an untrimmed blank line"
            )


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
    print("CHANGELOG.md section extraction for the release body")
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
    print(f"CHANGELOG section tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\nFAIL {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
