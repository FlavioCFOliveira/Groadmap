#!/usr/bin/env python3
"""
Test 45: `rmp audit stats` JSON key-set contract (regression for task #22).

End-to-end backstop against the compiled ./bin/rmp guarding the SPEC <-> code
agreement on the exact top-level key set emitted by `rmp audit stats`.

The bug behind task #22 was a divergence between the documented JSON keys and
the keys the command actually emitted. The fix is already in code; this test is
the regression guard its acceptance criteria require. It pins the contract so a
future rename, addition, or removal of a key fails loudly.

`rmp audit stats -r <name>` MUST emit EXACTLY these five top-level keys, no more
and no fewer (struct internal/models/audit.go AuditStats; documented in
SPEC/COMMANDS.md § "audit stats"):

  - by_operation     (object: operation name -> count)
  - by_entity_type   (object: entity type -> count)
  - first_entry_at   (ISO 8601 UTC string, or null when no entry matches)
  - last_entry_at    (ISO 8601 UTC string, or null when no entry matches)
  - total_entries    (integer)

The test exercises two shapes:

  - populated: a roadmap with real audit history -> all five keys present, the
    timestamps are non-empty strings, and the counters are coherent.
  - empty:     a --since/--until window that excludes every entry -> the empty
    shape {by_operation:{}, by_entity_type:{}, first_entry_at:null,
    last_entry_at:null, total_entries:0}.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


# The complete, SPEC-documented top-level key set for `audit stats`.
AUDIT_STATS_KEYS = frozenset(
    {
        "by_operation",
        "by_entity_type",
        "first_entry_at",
        "last_entry_at",
        "total_entries",
    }
)


class TestAuditStatsKeys:
    """Regression guard for the `audit stats` JSON key-set contract."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()

    def teardown_method(self):
        self.test.teardown()

    def _seed_history(self):
        """Generate realistic audit history: a prioritised task plus a sprint."""
        task_id = self.test.create_task(
            self.roadmap,
            "Refine flag-like detection in graph query parsing",
            "Operators must be able to query the graph with negative numeric literals",
            "Replace the leading-dash heuristic in readQuery with a precise flag-like check",
            "rmp graph query --query '-1 RETURN 1' reaches the engine instead of failing as a missing value",
            priority=7,
        )
        self.test.run_cmd(["task", "prio", "-r", self.roadmap, str(task_id), "8"])
        self.test.create_sprint(self.roadmap, "Audit remediation sprint")
        return task_id

    @staticmethod
    def _assert_exact_keys(stats):
        got = set(stats.keys())
        missing = AUDIT_STATS_KEYS - got
        extra = got - AUDIT_STATS_KEYS
        assert not missing and not extra, (
            "audit stats JSON key set diverges from SPEC/COMMANDS.md:\n"
            f"  missing: {sorted(missing)}\n"
            f"  extra:   {sorted(extra)}"
        )

    def test_populated_stats_has_exactly_documented_keys(self):
        """A roadmap with history yields exactly the five documented keys."""
        self._seed_history()

        stats = self.test.run_cmd_json(["audit", "stats", "-r", self.roadmap])

        self._assert_exact_keys(stats)

        # Beyond the key set, the populated shape must carry coherent values.
        assert isinstance(stats["by_operation"], dict)
        assert isinstance(stats["by_entity_type"], dict)
        assert isinstance(stats["total_entries"], int)
        assert stats["total_entries"] >= 3, (
            f"expected at least 3 audit entries, got {stats['total_entries']}"
        )
        # With entries present the timestamps are real, ordered ISO strings.
        assert isinstance(stats["first_entry_at"], str) and stats["first_entry_at"] != ""
        assert isinstance(stats["last_entry_at"], str) and stats["last_entry_at"] != ""
        assert stats["first_entry_at"] <= stats["last_entry_at"]
        # The per-bucket counts must sum to total_entries (no orphan buckets).
        assert sum(stats["by_operation"].values()) == stats["total_entries"]
        assert sum(stats["by_entity_type"].values()) == stats["total_entries"]

        print("✓ audit stats populated key-set contract test passed")

    def test_empty_window_has_exactly_documented_keys_and_empty_shape(self):
        """A window that excludes every entry yields the exact empty shape."""
        self._seed_history()

        # A past window guaranteed to contain none of the just-created entries.
        stats = self.test.run_cmd_json(
            [
                "audit", "stats", "-r", self.roadmap,
                "--since", "2000-01-01T00:00:00.000Z",
                "--until", "2000-01-02T00:00:00.000Z",
            ]
        )

        # Exactly the documented keys, no more and no fewer.
        self._assert_exact_keys(stats)

        # The empty shape is fully specified: empty maps, null timestamps, zero.
        assert stats["by_operation"] == {}, stats["by_operation"]
        assert stats["by_entity_type"] == {}, stats["by_entity_type"]
        assert stats["first_entry_at"] is None
        assert stats["last_entry_at"] is None
        assert stats["total_entries"] == 0

        print("✓ audit stats empty-window key-set and shape test passed")

    def test_fresh_roadmap_stats_has_exactly_documented_keys(self):
        """Even with no extra activity, the key set never drifts."""
        stats = self.test.run_cmd_json(["audit", "stats", "-r", self.roadmap])

        self._assert_exact_keys(stats)
        assert isinstance(stats["total_entries"], int)

        print("✓ audit stats fresh-roadmap key-set contract test passed")


def _run_all():
    instance_cls = TestAuditStatsKeys
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
            print(f"✗ {m}")
        except Exception as exc:  # noqa: BLE001
            failed += 1
            failures.append((m, exc))
            print(f"✗ {m} (error)")
        finally:
            instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Audit stats key-set tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
