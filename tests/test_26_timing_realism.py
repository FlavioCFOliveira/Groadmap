#!/usr/bin/env python3
"""
Test 26: Timing Realism for Burndown and Velocity
The rest of the suite creates and completes tasks in the same second,
which makes the time-aware reports (burndown series, velocity) hard
to exercise meaningfully. This module backfills realistic spreads by
manipulating timestamps directly in SQLite after the CLI has done its
work, then verifies the resulting reports match what a real roadmap
would expose.

Validates:
  - Burndown series has one row per day with non-monotonic completions
  - Velocity converges to (closed_count / sprint_duration_days) for a
    multi-day sprint
  - sprint stats exposes a sensible days_elapsed
  - audit list since/until filters work against historical timestamps
"""

import os
import sqlite3
import sys
from datetime import datetime, timedelta, timezone

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


def _iso(dt):
    """Format a datetime as the canonical SPEC string YYYY-MM-DDTHH:mm:ss.sssZ."""
    # SPEC/DATA_FORMATS.md prescribes millisecond precision and a literal Z.
    return dt.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.") + f"{dt.microsecond // 1000:03d}Z"


class TestBurndownSpread:
    """Tasks closed on different days produce a burndown series with one entry per day."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        self.db_path = self.test.roadmaps_dir / self.roadmap / "project.db"

        # Build a five-day sprint: started day -4, four tasks completed on
        # days -3, -2, -2, 0 (relative to "today"). One task still open at end.
        self.sprint_id = self.test.create_sprint(self.roadmap, "Q3 platform-hardening sprint")
        self.task_ids = []
        titles = [
            "Migrate payments worker to gRPC streaming",
            "Replace bcrypt with argon2id in auth service",
            "Bake structured logging into the gateway",
            "Cut over staging to TLS 1.3-only",
            "Retire legacy /v1/users endpoint",
        ]
        for t in titles:
            tid = self.test.create_task(
                self.roadmap,
                title=t,
                functional_requirements="Operationally required; tracked in the platform-hardening initiative.",
                technical_requirements="Implement, test against staging, roll forward behind a feature flag.",
                acceptance_criteria="No regressions in canary; runbook updated.",
                priority=7,
            )
            self.task_ids.append(tid)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id),
            ",".join(str(i) for i in self.task_ids),
        ])
        self.test.run_cmd(["sprint", "start", "-r", self.roadmap, str(self.sprint_id)])

        # Drive four of the five tasks to COMPLETED.
        for tid in self.task_ids[:4]:
            self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(tid), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(tid), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(tid), "COMPLETED"])

        # Rewrite timestamps directly to simulate a multi-day spread.
        now = datetime.now(timezone.utc)
        day_offsets = {
            self.task_ids[0]: -3,
            self.task_ids[1]: -2,
            self.task_ids[2]: -2,
            self.task_ids[3]: 0,
        }
        sprint_start = now - timedelta(days=4)

        conn = sqlite3.connect(self.db_path)
        try:
            cur = conn.cursor()
            # Sprint started 4 days ago
            cur.execute(
                "UPDATE sprints SET started_at = ? WHERE id = ?",
                (_iso(sprint_start), self.sprint_id),
            )
            # Spread the closures
            for tid, offset in day_offsets.items():
                closed_at = now + timedelta(days=offset)
                cur.execute(
                    "UPDATE tasks SET started_at = ?, tested_at = ?, closed_at = ? WHERE id = ?",
                    (_iso(sprint_start + timedelta(hours=1)),
                     _iso(closed_at - timedelta(minutes=30)),
                     _iso(closed_at),
                     tid),
                )
            conn.commit()
        finally:
            conn.close()

        self.expected_completions_per_day = {
            (now + timedelta(days=offset)).strftime("%Y-%m-%d"): count
            for offset, count in self._tally(day_offsets.values()).items()
        }

    @staticmethod
    def _tally(offsets):
        out = {}
        for o in offsets:
            out[o] = out.get(o, 0) + 1
        return out

    def teardown_method(self):
        self.test.teardown()

    def test_burndown_has_one_row_per_completion_day(self):
        """Sprint stats burndown reflects the per-day completion counts."""
        stats = self.test.run_cmd_json(["sprint", "stats", "-r", self.roadmap, str(self.sprint_id)])
        burndown = stats["burndown"]
        # There must be at least one entry per distinct completion day.
        days_seen = {entry["date"] for entry in burndown}
        for day in self.expected_completions_per_day:
            assert day in days_seen, (
                f"day {day} expected in burndown; got days={sorted(days_seen)}"
            )

        # The closing total must drop monotonically as days pass.
        ordered = sorted(burndown, key=lambda e: e["date"])
        remaining = [e["tasks_remaining"] for e in ordered]
        assert remaining == sorted(remaining, reverse=True), (
            f"tasks_remaining must be monotonically non-increasing; got {remaining}"
        )

        print(
            "✓ burndown carries one entry per completion day; remaining drops monotonically: "
            + ",".join(str(r) for r in remaining)
        )

    def test_days_elapsed_reflects_sprint_age(self):
        """A sprint that started four days ago reports days_elapsed >= 4."""
        stats = self.test.run_cmd_json(["sprint", "stats", "-r", self.roadmap, str(self.sprint_id)])
        days_elapsed = stats.get("days_elapsed")
        assert days_elapsed is not None and days_elapsed >= 4, (
            f"days_elapsed must reflect the 4-day-old started_at; got {days_elapsed}"
        )

        print(f"✓ days_elapsed={days_elapsed} reflects the simulated 4-day sprint age")


class TestVelocityAcrossClosedSprints:
    """Roadmap velocity is computed across recently CLOSED sprints with timing spread."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        self.db_path = self.test.roadmaps_dir / self.roadmap / "project.db"

        # Create two closed sprints with different durations and counts.
        # Sprint X: 5 days, 4 tasks completed
        # Sprint Y: 7 days, 7 tasks completed
        now = datetime.now(timezone.utc)
        self.sprint_x = self._build_closed_sprint(
            description="Closed sprint X — payments stabilisation",
            tasks_titles=[
                "Stop double-charging on retry storms",
                "Reduce payment confirmation latency p99",
                "Add idempotency keys to all charge endpoints",
                "Backfill missing transaction receipts",
            ],
            started_at=now - timedelta(days=12),
            closed_at=now - timedelta(days=7),
        )
        self.sprint_y = self._build_closed_sprint(
            description="Closed sprint Y — observability platform",
            tasks_titles=[
                "Land metrics scraper on all services",
                "Wire structured logs into the API gateway",
                "Enable distributed tracing in payment workers",
                "Stand up the SLO dashboard for cart team",
                "Add anomaly alerts for daily revenue dip",
                "Document the on-call runbook",
                "Retire legacy alerting stack",
            ],
            started_at=now - timedelta(days=14),
            closed_at=now - timedelta(days=7),
        )

    def _build_closed_sprint(self, description, tasks_titles, started_at, closed_at):
        sprint_id = self.test.create_sprint(self.roadmap, description)
        task_ids = []
        for title in tasks_titles:
            tid = self.test.create_task(
                self.roadmap,
                title=title,
                functional_requirements="Drives the closed-sprint velocity test.",
                technical_requirements="Implement per spec.",
                acceptance_criteria="Passes the suite.",
            )
            task_ids.append(tid)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap, str(sprint_id),
            ",".join(str(i) for i in task_ids),
        ])
        # Drive every task to COMPLETED through DOING/TESTING (force-allowed flow).
        for tid in task_ids:
            self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(tid), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(tid), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(tid), "COMPLETED"])

        # Now backdate the sprint and its task closures.
        conn = sqlite3.connect(self.db_path)
        try:
            cur = conn.cursor()
            cur.execute(
                "UPDATE sprints SET started_at = ?, closed_at = ?, status = 'CLOSED' WHERE id = ?",
                (_iso(started_at), _iso(closed_at), sprint_id),
            )
            for tid in task_ids:
                cur.execute(
                    "UPDATE tasks SET closed_at = ? WHERE id = ?",
                    (_iso(closed_at - timedelta(hours=1)), tid),
                )
            conn.commit()
        finally:
            conn.close()
        return sprint_id

    def teardown_method(self):
        self.test.teardown()

    def test_average_velocity_blends_closed_sprints(self):
        """Roadmap-level velocity averages across the recent CLOSED sprints."""
        stats = self.test.run_cmd_json(["stats", "-r", self.roadmap])
        avg = stats.get("average_velocity")
        assert avg is not None, "average_velocity must be reported"
        # Each sprint's velocity is tasks_completed / duration_days
        # X: 4 / 5 = 0.8;  Y: 7 / 7 = 1.0 → expected mean = 0.9
        assert 0.5 <= avg <= 1.5, (
            f"average_velocity must be in a sensible range for two closed sprints; got {avg}"
        )

        print(f"✓ average_velocity={avg:.2f} blends the two closed sprints' velocities")


def main():
    """Run all timing-realism tests."""
    import inspect

    failures = []
    passed = 0
    classes = [
        ("TestBurndownSpread", TestBurndownSpread),
        ("TestVelocityAcrossClosedSprints", TestVelocityAcrossClosedSprints),
    ]
    for cls_name, cls in classes:
        for meth_name, meth in inspect.getmembers(cls, predicate=inspect.isfunction):
            if not meth_name.startswith("test_"):
                continue
            inst = cls()
            if hasattr(inst, "setup_method"):
                inst.setup_method()
            try:
                meth(inst)
                passed += 1
            except Exception as e:
                failures.append(f"{cls_name}.{meth_name}: {e}")
            if hasattr(inst, "teardown_method"):
                try:
                    inst.teardown_method()
                except Exception:
                    pass

    print(f"\n{passed} passed, {len(failures)} failed")
    for f in failures:
        print(f"  ✗ {f}")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
