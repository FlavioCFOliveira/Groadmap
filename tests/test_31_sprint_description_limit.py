#!/usr/bin/env python3
"""
Test 31: Sprint description length boundary (max 2048 characters).

Enforces the application-level cap on `sprints.description` raised
from 500 to 2048 characters. The cap is documented in
SPEC/MODELS.md § Sprint Field Constraints and
SPEC/DATABASE.md § Field Length Validation (Application-Level
Validation Only). There is no CHECK constraint on the column, so the
guard is enforced by the Go validation layer; this suite is the only
end-to-end backstop against a regression that would silently truncate
or expand the limit.

Coverage:
- A description of exactly 2048 characters is accepted by `sprint
  create`.
- A description of 2049 characters is rejected with a stderr error
  and a non-zero exit code (EXIT_INVALID_DATA = 6, the project's
  validation error code).
- `sprint update` enforces the same boundary.
- The AI Agent Contract (`rmp sprint create --ai-help` and
  `rmp sprint update --ai-help`) reports `max_length: 2048` for the
  `--description` flag.
- Help text on `sprint create --help` and `sprint update --help`
  mentions the new 2048 limit literally.
"""

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


EXPECTED_MAX = 2048


class TestSprintDescriptionLimit:

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()

    def teardown_method(self):
        self.test.teardown()

    def test_create_accepts_exact_max_length(self):
        desc = "a" * EXPECTED_MAX
        result = self.test.run_cmd_json([
            "sprint", "create",
            "-r", self.roadmap,
            "-t", "Boundary length description test",
            "-d", desc,
        ])
        assert "id" in result, f"expected sprint id in response, got {result!r}"
        # Round-trip via `sprint get` so we know the stored payload
        # carries the full length.
        sprint = self.test.run_cmd_json(["sprint", "get", "-r", self.roadmap, str(result["id"])])
        assert len(sprint["description"]) == EXPECTED_MAX, (
            f"stored description length: {len(sprint['description'])} (expected {EXPECTED_MAX})"
        )
        print(f"✓ sprint create accepts description of exactly {EXPECTED_MAX} chars")

    def test_create_rejects_one_over_max(self):
        desc = "a" * (EXPECTED_MAX + 1)
        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "create", "-r", self.roadmap, "-t", "Over-limit description test", "-d", desc],
            check=False,
        )
        assert exit_code != 0, "sprint create with description over limit must fail"
        # ErrFieldTooLarge -> exit 6 (EXIT_INVALID_DATA) per the
        # project's exit-code catalogue. Assert on the code and the
        # error wording so a refactor cannot silently weaken either.
        assert exit_code == 6, f"expected exit 6 (invalid data), got {exit_code}"
        assert "description" in stderr.lower(), f"stderr must name the failing field: {stderr!r}"
        assert "2048" in stderr or "maximum length" in stderr.lower(), (
            f"stderr must surface the limit: {stderr!r}"
        )
        print(f"✓ sprint create rejects description of {EXPECTED_MAX + 1} chars with exit 6")

    def test_update_accepts_exact_max_length(self):
        sprint_id = self.test.create_sprint(self.roadmap, "Initial short description")
        new_desc = "b" * EXPECTED_MAX
        self.test.run_cmd([
            "sprint", "update", str(sprint_id),
            "-r", self.roadmap,
            "-d", new_desc,
        ])
        sprint = self.test.run_cmd_json(["sprint", "get", "-r", self.roadmap, str(sprint_id)])
        assert sprint["description"] == new_desc, (
            f"sprint update did not store the full {EXPECTED_MAX}-char payload"
        )
        print(f"✓ sprint update accepts description of exactly {EXPECTED_MAX} chars")

    def test_update_rejects_one_over_max(self):
        sprint_id = self.test.create_sprint(self.roadmap, "Initial short description")
        new_desc = "c" * (EXPECTED_MAX + 1)
        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "update", str(sprint_id), "-r", self.roadmap, "-d", new_desc],
            check=False,
        )
        assert exit_code != 0, "sprint update with description over limit must fail"
        assert exit_code == 6, f"expected exit 6 (invalid data), got {exit_code}"
        assert "description" in stderr.lower(), f"stderr must name the failing field: {stderr!r}"
        print(f"✓ sprint update rejects description of {EXPECTED_MAX + 1} chars with exit 6")

    def test_ai_help_reports_new_max_length_on_create(self):
        doc = self.test.run_cmd_json(["sprint", "create", "--ai-help"])
        sprint_cmd = next(c for c in doc["commands"] if c["name"] == "sprint")
        create = next(s for s in sprint_cmd["subcommands"] if s["name"] == "create")
        desc_flag = next(f for f in create["flags"] if f.get("long") == "--description")
        assert desc_flag.get("max_length") == EXPECTED_MAX, (
            f"ai-help for sprint create reports max_length={desc_flag.get('max_length')}, "
            f"expected {EXPECTED_MAX}"
        )
        print(f"✓ ai-help (sprint create) reports max_length={EXPECTED_MAX}")

    def test_ai_help_reports_new_max_length_on_update(self):
        doc = self.test.run_cmd_json(["sprint", "update", "--ai-help"])
        sprint_cmd = next(c for c in doc["commands"] if c["name"] == "sprint")
        update = next(s for s in sprint_cmd["subcommands"] if s["name"] == "update")
        desc_flag = next(f for f in update["flags"] if f.get("long") == "--description")
        assert desc_flag.get("max_length") == EXPECTED_MAX, (
            f"ai-help for sprint update reports max_length={desc_flag.get('max_length')}, "
            f"expected {EXPECTED_MAX}"
        )
        print(f"✓ ai-help (sprint update) reports max_length={EXPECTED_MAX}")

    def test_help_text_mentions_new_max_on_create(self):
        _, stdout, _ = self.test.run_cmd(["sprint", "create", "--help"])
        assert str(EXPECTED_MAX) in stdout, (
            f"sprint create --help must mention {EXPECTED_MAX}; got:\n{stdout}"
        )
        assert "500" not in stdout, "sprint create --help still references the old 500 limit"
        print(f"✓ sprint create --help mentions {EXPECTED_MAX} and not 500")

    def test_help_text_mentions_new_max_on_update(self):
        _, stdout, _ = self.test.run_cmd(["sprint", "update", "--help"])
        assert str(EXPECTED_MAX) in stdout, (
            f"sprint update --help must mention {EXPECTED_MAX}; got:\n{stdout}"
        )
        assert "500" not in stdout, "sprint update --help still references the old 500 limit"
        print(f"✓ sprint update --help mentions {EXPECTED_MAX} and not 500")


def _run_all():
    instance_cls = TestSprintDescriptionLimit
    method_names = [m for m in dir(instance_cls) if m.startswith("test_")]
    passed = 0
    failed = 0
    failures = []
    for m in method_names:
        instance = instance_cls()
        instance.setup_method()
        try:
            getattr(instance, m)()
            passed += 1
        except AssertionError as exc:
            failed += 1
            failures.append((m, exc))
        except Exception as exc:
            failed += 1
            failures.append((m, exc))
        finally:
            instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Sprint-description-limit tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
