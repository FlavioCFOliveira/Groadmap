#!/usr/bin/env python3
"""
Test 36: Correctness of the read-only query/reporting commands.

Builds a realistic payments-platform roadmap, drives a typical
task/sprint lifecycle (backlog grooming, sprint planning, execution through
DOING/TESTING/COMPLETED, sprint close, subtasks, dependencies), and then
asserts that EVERY query command reports data that matches an independently
computed ground truth.

Commands covered:
  stats
  sprint list / get / show / stats / tasks / open-tasks
  task list (status/type/priority/sort/limit filters) / get (single+multi) /
       next / subtasks / blockers / blocking
  backlog list / show-next
  audit stats / list / history
  roadmap list

Regression guards for defects found during the query-command evaluation:
  - multi-row task queries must report each task's OWN lifecycle timestamps
    (the scan-loop pointer-aliasing bug returned the last row's values for all)
  - depends_on / blocks must be populated in every view that returns a task
    object (sprint tasks / open-tasks / blockers / blocking), not just task get
  - velocity must be floored at a 1-day denominator (sub-day sprints must not
    report inflated tasks/day)
  - stats.sprints.pending must count PENDING (never-started) sprints
  - sprint list must populate task_count/tasks for EVERY sprint it returns
    exactly as sprint get does for the same sprint (rmp task #233: ListSprints
    used to leave both fields at their zero value for every row while
    GetSprint resolved them correctly for the same sprint)
"""

import sys
import os
import json
import time
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase, commit_flags_for


class TestQueryCommandsCorrectness:
    """Rigorous correctness checks for the read-only query commands."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers -----------------------------------------------------------

    def _mk_task(self, roadmap, title, ttype, priority, severity, parent=None):
        """Create a task with explicit type (base_test.create_task omits --type)."""
        cmd = ["task", "create", "-r", roadmap, "-t", title,
               "-fr", "As a user I need " + title,
               "-tr", "Implement " + title,
               "-ac", title + " verified",
               "-y", ttype, "-p", str(priority), "--severity", str(severity)]
        if parent is not None:
            cmd += ["--parent", str(parent)]
        return self.test.run_cmd_json(cmd)["id"]

    def _complete(self, roadmap, task_id, settle=0.0):
        """Drive a task DOING -> TESTING -> COMPLETED with optional settle delay
        so successive completions get distinct millisecond timestamps."""
        for st in ("DOING", "TESTING", "COMPLETED"):
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), st]
                              + commit_flags_for(st))
        if settle:
            time.sleep(settle)

    def _build(self):
        """Build the realistic scenario. Returns (roadmap, ids dict)."""
        r = self.test.create_roadmap("payments_platform")
        s1 = self.test.create_sprint(r, "Sprint 1 - Auth foundation")
        s2 = self.test.create_sprint(r, "Sprint 2 - Payment intents")
        s3 = self.test.create_sprint(r, "Sprint 3 - Refunds (planned)")
        s4 = self.test.create_sprint(r, "Sprint 4 - Reporting (planned)")

        t = {}
        # title, type, priority, severity
        spec = [
            ("t1", "Login API", "USER_STORY", 8, 2),
            ("t2", "JWT refresh", "TASK", 7, 3),
            ("t3", "Password reset defect", "BUG", 9, 8),
            ("t4", "Rate limiter", "TASK", 5, 5),
            ("t5", "OAuth provider", "USER_STORY", 6, 4),
            ("t6", "Create payment intent", "USER_STORY", 8, 6),
            ("t7", "Idempotency keys", "TASK", 7, 7),
            ("t8", "Webhook retries", "TASK", 4, 5),
            ("t9", "3DS authentication flow", "USER_STORY", 6, 9),
            ("t10", "Refund endpoint", "USER_STORY", 5, 3),
            ("t11", "Audit logging chore", "CHORE", 2, 1),
            ("t12", "Database index refactor", "REFACTOR", 3, 2),
            ("t13", "Spike: ledger model", "SPIKE", 4, 0),
            ("t14", "Settlement report", "TASK", 6, 6),
        ]
        for key, title, ty, pr, sv in spec:
            t[key] = self._mk_task(r, title, ty, pr, sv)
        # subtask of t1 (stays in backlog) and dependency t2 -> t1
        t["sub"] = self._mk_task(r, "Login API - password hashing", "SUB_TASK", 6, 2, parent=t["t1"])
        self.test.run_cmd(["task", "add-dep", "-r", r, str(t["t2"]), str(t["t1"])])

        # Sprint 1: 5 tasks; complete 3 (distinct timestamps), t1 -> DOING, close.
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(s1),
                            ",".join(str(t[k]) for k in ("t1", "t3", "t4", "t11", "t12"))])
        self.test.run_cmd(["sprint", "start", "-r", r, str(s1)])
        self._complete(r, t["t3"], settle=0.05)
        self._complete(r, t["t4"], settle=0.05)
        self._complete(r, t["t11"], settle=0.05)
        self.test.run_cmd(["task", "stat", "-r", r, str(t["t1"]), "DOING", "--commit-open", "626346d"])
        self.test.run_cmd(["sprint", "close", "-r", r, str(s1), "--force"])

        # Sprint 2: 4 tasks; complete 2, t7 -> DOING, t8 -> TESTING, stay OPEN.
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(s2),
                            ",".join(str(t[k]) for k in ("t6", "t7", "t8", "t14"))])
        self.test.run_cmd(["sprint", "start", "-r", r, str(s2)])
        self._complete(r, t["t6"], settle=0.05)
        self._complete(r, t["t14"], settle=0.05)
        self.test.run_cmd(["task", "stat", "-r", r, str(t["t7"]), "DOING", "--commit-open", "3b9289c"])
        self.test.run_cmd(["task", "stat", "-r", r, str(t["t8"]), "DOING", "--commit-open", "24262f0"])
        self.test.run_cmd(["task", "stat", "-r", r, str(t["t8"]), "TESTING"])

        # Sprint 3: PENDING with 2 tasks (move BACKLOG -> SPRINT). Sprint 4: PENDING empty.
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(s3), ",".join(str(t[k]) for k in ("t9", "t10"))])

        ids = {"s1": s1, "s2": s2, "s3": s3, "s4": s4}
        ids.update(t)
        return r, ids

    @staticmethod
    def _as_task(result):
        return result[0] if isinstance(result, list) else result

    # ---- roadmap stats -----------------------------------------------------

    def test_roadmap_stats(self):
        r, ids = self._build()
        st = self.test.run_cmd_json(["stats", "-r", r])

        assert st["roadmap"] == r, st["roadmap"]
        sp = st["sprints"]
        assert sp["current"] == ids["s2"], f"current should be the OPEN sprint {ids['s2']}, got {sp['current']}"
        assert sp["total"] == 4, sp
        assert sp["completed"] == 1, f"1 CLOSED sprint expected, got {sp['completed']}"
        # Regression: pending counts PENDING-status sprints (s3, s4), not OPEN.
        assert sp["pending"] == 2, f"pending must count PENDING sprints (2), got {sp['pending']}"

        tk = st["tasks"]
        assert tk == {"backlog": 4, "sprint": 3, "doing": 2, "testing": 1, "completed": 5}, tk
        assert sum(tk.values()) == 15, "all 15 tasks must be accounted for"

        # Only s1 is a qualifying closed sprint (3 completed), same-day => velocity 3.0.
        assert abs(st["average_velocity"] - 3.0) < 1e-9, \
            f"average_velocity should be 3.0 (3 completed / 1-day floor), got {st['average_velocity']}"
        print("✓ roadmap stats correctness")

    # ---- sprint stats ------------------------------------------------------

    def test_sprint_stats(self):
        r, ids = self._build()
        st = self.test.run_cmd_json(["sprint", "stats", "-r", r, str(ids["s1"])])
        assert st["sprint_id"] == ids["s1"]
        assert st["total_tasks"] == 5, st["total_tasks"]
        assert st["completed_tasks"] == 3, st["completed_tasks"]
        assert abs(st["progress_percentage"] - 60.0) < 1e-9, st["progress_percentage"]
        assert st["status_distribution"] == {"COMPLETED": 3, "DOING": 1, "SPRINT": 1}, st["status_distribution"]
        assert set(st["task_order"]) == {ids["t1"], ids["t3"], ids["t4"], ids["t11"], ids["t12"]}, st["task_order"]
        # Closed same-day sprint, 3 completed -> velocity floored to 3.0 (not inflated).
        assert abs(st["velocity"] - 3.0) < 1e-9, f"velocity should be 3.0, got {st['velocity']}"
        assert st["days_elapsed"] is None, "days_elapsed is null for CLOSED sprints"
        assert st["days_remaining"] is None
        print("✓ sprint stats correctness")

    def test_sprint_show(self):
        r, ids = self._build()
        sh = self.test.run_cmd_json(["sprint", "show", "-r", r, str(ids["s2"])])
        assert sh["sprint_id"] == ids["s2"]
        assert sh["status"] == "OPEN"
        # s2: t6 COMPLETED, t7 DOING, t8 TESTING, t14 COMPLETED.
        assert sh["summary"] == {"total_tasks": 4, "pending": 0, "in_progress": 2, "completed": 2}, sh["summary"]
        pr = sh["progress"]
        assert abs(pr["pending_percentage"] - 0.0) < 1e-9
        assert abs(pr["in_progress_percentage"] - 50.0) < 1e-9
        assert abs(pr["completed_percentage"] - 50.0) < 1e-9
        # Severities: t6=6, t7=7, t8=5, t14=6 -> 3-5:1, 6-7:3.
        sd = sh["severity_distribution"]
        assert sd["0-2"]["count"] == 0 and sd["3-5"]["count"] == 1, sd
        assert sd["6-7"]["count"] == 3 and sd["8-9"]["count"] == 0, sd
        cd = sh["criticality_distribution"]
        assert cd["low"]["count"] == 0 and cd["medium"]["count"] == 1, cd
        assert cd["high"]["count"] == 3 and cd["critical"]["count"] == 0, cd
        # Documented extra fields must be present.
        for f in ("max_tasks", "capacity_pct", "task_order", "current_load"):
            assert f in sh, f"sprint show must include {f}"
        assert set(sh["task_order"]) == {ids["t6"], ids["t7"], ids["t8"], ids["t14"]}, sh["task_order"]
        print("✓ sprint show correctness")

    # ---- sprint list/get/tasks membership agreement (rmp task #233) -------
    #
    # ListSprints used to leave task_count at 0 and tasks at nil for every
    # sprint it returned, whatever the sprint held, because it never resolved
    # membership the way GetSprint did for the same sprint. These tests build
    # an independently-computed ground truth for several sprints of different
    # sizes -- including one that is genuinely empty -- and check that
    # `sprint list`, `sprint get` and `sprint tasks` never disagree about it.

    def _build_membership_scenario(self):
        """Fleet-telematics roadmap with sprints of different sizes,
        including an empty one, built so the assertions in this section are
        non-vacuous:

        - s_medium's planned in-sprint position order (set via `sprint
          reorder`) is deliberately NOT ascending task-id order, so a test
          that asserts "list/get return ascending ids while sprint tasks
          returns the planned order" cannot pass by accident.
        - s_medium also parks one member (t4) back in BACKLOG status while it
          stays a sprint member, so membership counting can be shown to be
          status-independent.
        - Sprints end in different statuses (PENDING x2, OPEN, CLOSED) so the
          --status filter has something real to narrow.

        Returns (roadmap, ids, medium_position_order): ids maps mnemonic
        sprint/task keys to their integer ids, and medium_position_order is
        the exact scrambled task-id sequence s_medium was reordered to.
        """
        r = self.test.create_roadmap("fleet_telematics_platform")

        s_empty = self.test.create_sprint(
            r,
            "Reserved swing capacity for the on-call rotation; deliberately "
            "holds no work until an incident pulls engineers off their sprint.",
            title="Sprint 4 - Swing capacity (empty)")
        s_small = self.test.create_sprint(
            r,
            "Stand up the raw ingestion path for vehicle telemetry pings.",
            title="Sprint 1 - Ingestion foundations")
        s_medium = self.test.create_sprint(
            r,
            "Ship geofence breach alerting and fuel-consumption analytics on "
            "top of the ingested telemetry.",
            title="Sprint 2 - Alerting and fuel analytics")
        s_large = self.test.create_sprint(
            r,
            "Roll out the fleet visibility surface: route replay, firmware "
            "OTA, cold-chain audit, fatigue detection, sensor calibration "
            "and utilization reporting.",
            title="Sprint 3 - Fleet visibility rollout")

        t = {}
        # (key, title, type, priority, severity)
        spec = [
            ("t1", "Vehicle GPS ping ingestion service", "TASK", 5, 3),
            ("t2", "Driver behavior scoring model", "USER_STORY", 7, 4),
            ("t3", "Truck geofence breach alerting", "BUG", 8, 7),
            ("t4", "Fuel consumption analytics dashboard", "TASK", 6, 5),
            ("t5", "Route replay viewer", "USER_STORY", 5, 3),
            ("t6", "Device firmware OTA rollout", "TASK", 7, 6),
            ("t7", "Cold-chain temperature audit trail", "USER_STORY", 8, 8),
            ("t8", "Maintenance schedule predictor", "TASK", 4, 2),
            ("t9", "Driver fatigue detection alerts", "USER_STORY", 6, 6),
            ("t10", "Trailer weight sensor calibration", "TASK", 3, 4),
            ("t11", "Fleet utilization report export", "TASK", 5, 3),
            ("t12", "Depot yard camera integration", "USER_STORY", 6, 5),
        ]
        for key, title, ty, pr, sv in spec:
            t[key] = self._mk_task(r, title, ty, pr, sv)

        # s_medium first, and fully cycled to CLOSED, while no other sprint is
        # OPEN yet (only one sprint may be OPEN at a time).
        medium_order = [t["t6"], t["t3"], t["t5"], t["t4"]]
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(s_medium),
                            ",".join(str(t[k]) for k in ("t3", "t4", "t5", "t6"))])
        self.test.run_cmd(["sprint", "reorder", "-r", r, str(s_medium),
                            ",".join(str(i) for i in medium_order)])
        self.test.run_cmd(["task", "stat", "-r", r, str(t["t4"]), "BACKLOG"])
        self.test.run_cmd(["sprint", "start", "-r", r, str(s_medium)])
        # t3/t5/t6 are still SPRINT (only t4 was parked in BACKLOG), so
        # closing requires --force.
        self.test.run_cmd(["sprint", "close", "-r", r, str(s_medium), "--force"])

        # s_small: 2 members, started last so it is the sprint left OPEN.
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(s_small),
                            ",".join(str(t[k]) for k in ("t1", "t2"))])
        self.test.run_cmd(["sprint", "start", "-r", r, str(s_small)])

        # s_large: 6 members, default (ascending) order, left PENDING.
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(s_large),
                            ",".join(str(t[k]) for k in ("t7", "t8", "t9", "t10", "t11", "t12"))])

        # s_empty: no operations at all -- created and left untouched.

        ids = {"s_empty": s_empty, "s_small": s_small, "s_medium": s_medium,
               "s_large": s_large}
        ids.update(t)
        return r, ids, medium_order

    def test_sprint_membership_varied_sizes_task_count_matches_tasks(self):
        """task_count equals len(tasks) for every sprint `sprint list`
        returns, across sprints of genuinely different sizes (0, 2, 4, 6),
        and the ids in `tasks` are ascending task-id order."""
        r, ids, _ = self._build_membership_scenario()
        listing = self.test.run_cmd_json(["sprint", "list", "-r", r])
        by_id = {s["id"]: s for s in listing}

        expected_members = {
            ids["s_empty"]: set(),
            ids["s_small"]: {ids["t1"], ids["t2"]},
            ids["s_medium"]: {ids["t3"], ids["t4"], ids["t5"], ids["t6"]},
            ids["s_large"]: {ids["t7"], ids["t8"], ids["t9"],
                              ids["t10"], ids["t11"], ids["t12"]},
        }
        for sid, expected in expected_members.items():
            entry = by_id[sid]
            assert entry["task_count"] == len(entry["tasks"]), (
                f"sprint {sid}: task_count {entry['task_count']} != "
                f"len(tasks) {len(entry['tasks'])}"
            )
            assert entry["task_count"] == len(expected), (
                f"sprint {sid}: expected {len(expected)} members, "
                f"got task_count={entry['task_count']}"
            )
            assert set(entry["tasks"]) == expected, (
                f"sprint {sid}: tasks {entry['tasks']} != expected {expected}"
            )
            assert entry["tasks"] == sorted(entry["tasks"]), (
                f"sprint {sid}: tasks must be in ascending task-id order, "
                f"got {entry['tasks']}"
            )
        print("✓ sprint list: task_count == len(tasks) across differently sized sprints")

    def test_sprint_membership_three_way_agreement_list_get_tasks(self):
        """`sprint list`, `sprint get` and `sprint tasks` never disagree about
        the same sprint's membership, for sprints of every size in the
        fixture (the empty one is checked on its own in a dedicated test)."""
        r, ids, _ = self._build_membership_scenario()
        listing = self.test.run_cmd_json(["sprint", "list", "-r", r])
        by_id = {s["id"]: s for s in listing}

        for key in ("s_small", "s_medium", "s_large"):
            sid = ids[key]
            list_entry = by_id[sid]
            get_entry = self.test.run_cmd_json(["sprint", "get", "-r", r, str(sid)])
            tasks_rows = self.test.run_cmd_json(["sprint", "tasks", "-r", r, str(sid)])

            assert list_entry["task_count"] == get_entry["task_count"], (
                f"sprint {sid}: list task_count {list_entry['task_count']} != "
                f"get task_count {get_entry['task_count']}"
            )
            assert list_entry["tasks"] == get_entry["tasks"], (
                f"sprint {sid}: list tasks {list_entry['tasks']} != "
                f"get tasks {get_entry['tasks']}"
            )
            assert len(tasks_rows) == list_entry["task_count"], (
                f"sprint {sid}: sprint tasks returned {len(tasks_rows)} rows "
                f"but task_count is {list_entry['task_count']}"
            )
            assert {row["id"] for row in tasks_rows} == set(list_entry["tasks"]), (
                f"sprint {sid}: sprint tasks ids {[row['id'] for row in tasks_rows]} "
                f"!= list/get tasks {list_entry['tasks']}"
            )
        print("✓ sprint list / get / tasks agree on membership for every sprint size")

    def test_sprint_membership_empty_sprint_zero_and_empty_array_never_null(self):
        """An empty sprint reports task_count 0 and tasks [] -- an empty JSON
        array -- in BOTH sprint list and sprint get, never null."""
        r, ids, _ = self._build_membership_scenario()
        sid = ids["s_empty"]

        listing = self.test.run_cmd_json(["sprint", "list", "-r", r])
        list_entry = next(s for s in listing if s["id"] == sid)
        assert list_entry["task_count"] == 0, list_entry["task_count"]
        assert list_entry["tasks"] is not None, "sprint list: tasks must not be null"
        assert list_entry["tasks"] == [], list_entry["tasks"]

        get_entry = self.test.run_cmd_json(["sprint", "get", "-r", r, str(sid)])
        assert get_entry["task_count"] == 0, get_entry["task_count"]
        assert get_entry["tasks"] is not None, "sprint get: tasks must not be null"
        assert get_entry["tasks"] == [], get_entry["tasks"]

        # Raw-text proof, independent of any JSON-decoding convenience in the
        # test harness: the bytes on the wire must literally say "tasks": []
        # and never "tasks": null, for both commands.
        _, get_stdout, _ = self.test.run_cmd(["sprint", "get", "-r", r, str(sid)])
        assert '"tasks": null' not in get_stdout, get_stdout
        assert '"tasks": []' in get_stdout, get_stdout

        _, list_stdout, _ = self.test.run_cmd(["sprint", "list", "-r", r])
        assert '"tasks": null' not in list_stdout, "sprint list must never emit tasks: null"
        print("✓ empty sprint reports 0 / [] (never null) in both sprint list and sprint get")

    def test_sprint_membership_ascending_order_differs_from_planned_position(self):
        """`tasks` (list/get) is ascending task-id order; `sprint tasks`
        (no filter) is the planned in-sprint position order. The fixture's
        s_medium is reordered so the two are provably different sequences,
        so this assertion cannot pass by coincidence."""
        r, ids, medium_order = self._build_membership_scenario()
        sid = ids["s_medium"]
        ascending = sorted([ids["t3"], ids["t4"], ids["t5"], ids["t6"]])

        # Sanity: the fixture's planned order really is not ascending id
        # order, otherwise the assertions below would be vacuous.
        assert medium_order != ascending, (
            "fixture bug: medium_order must differ from ascending id order "
            f"for this test to be meaningful, got {medium_order}"
        )

        get_entry = self.test.run_cmd_json(["sprint", "get", "-r", r, str(sid)])
        assert get_entry["tasks"] == ascending, (
            f"sprint get tasks must be ascending id order: {get_entry['tasks']} != {ascending}"
        )

        listing = self.test.run_cmd_json(["sprint", "list", "-r", r])
        list_entry = next(s for s in listing if s["id"] == sid)
        assert list_entry["tasks"] == ascending, (
            f"sprint list tasks must be ascending id order: {list_entry['tasks']} != {ascending}"
        )

        tasks_rows = self.test.run_cmd_json(["sprint", "tasks", "-r", r, str(sid)])
        position_order = [row["id"] for row in tasks_rows]
        assert position_order == medium_order, (
            f"sprint tasks must return the planned position order {medium_order}, "
            f"got {position_order}"
        )
        assert position_order != ascending, (
            "sprint tasks position order accidentally matches ascending id "
            "order; the fixture must scramble it for this test to be non-vacuous"
        )
        print("✓ tasks (ascending id order) genuinely differs from sprint tasks (planned position order)")

    def test_sprint_membership_backlog_member_counted_and_listed(self):
        """A member task parked in BACKLOG status stays counted in
        task_count and listed in tasks -- membership is status-independent."""
        r, ids, _ = self._build_membership_scenario()
        sid = ids["s_medium"]
        t4 = ids["t4"]

        # Ground truth: t4 really is BACKLOG while remaining a sprint member.
        t4_task = self._as_task(self.test.run_cmd_json(["task", "get", "-r", r, str(t4)]))
        assert t4_task["status"] == "BACKLOG", t4_task["status"]

        get_entry = self.test.run_cmd_json(["sprint", "get", "-r", r, str(sid)])
        assert t4 in get_entry["tasks"], f"BACKLOG member {t4} missing from tasks: {get_entry['tasks']}"
        assert get_entry["task_count"] == 4, get_entry["task_count"]

        listing = self.test.run_cmd_json(["sprint", "list", "-r", r])
        list_entry = next(s for s in listing if s["id"] == sid)
        assert t4 in list_entry["tasks"], f"BACKLOG member {t4} missing from list tasks: {list_entry['tasks']}"
        assert list_entry["task_count"] == 4, list_entry["task_count"]

        # sprint tasks (unfiltered) still surfaces it, with its real status.
        unfiltered = self.test.run_cmd_json(["sprint", "tasks", "-r", r, str(sid)])
        row = next((x for x in unfiltered if x["id"] == t4), None)
        assert row is not None, "sprint tasks (unfiltered) must include the BACKLOG member"
        assert row["status"] == "BACKLOG", row["status"]

        # Status-filtered sprint tasks: BACKLOG isolates it, SPRINT excludes it,
        # neither changes task_count on a later membership read.
        backlog_only = self.test.run_cmd_json(["sprint", "tasks", "-r", r, str(sid), "-s", "BACKLOG"])
        assert {x["id"] for x in backlog_only} == {t4}, backlog_only

        sprint_only = self.test.run_cmd_json(["sprint", "tasks", "-r", r, str(sid), "-s", "SPRINT"])
        assert {x["id"] for x in sprint_only} == {ids["t3"], ids["t5"], ids["t6"]}, sprint_only

        recheck = self.test.run_cmd_json(["sprint", "get", "-r", r, str(sid)])
        assert recheck["task_count"] == 4, "status-filtered reads must not change task_count"
        print("✓ a BACKLOG member is still counted and listed as sprint membership")

    def test_sprint_membership_status_filter_does_not_alter_membership(self):
        """`--status` on `sprint list` narrows which SPRINTS are returned; it
        never changes the task_count/tasks a returned sprint carries."""
        r, ids, _ = self._build_membership_scenario()
        truth = {s["id"]: (s["task_count"], s["tasks"])
                 for s in self.test.run_cmd_json(["sprint", "list", "-r", r])}

        expected_by_status = {
            "PENDING": {ids["s_empty"], ids["s_large"]},
            "OPEN": {ids["s_small"]},
            "CLOSED": {ids["s_medium"]},
        }
        for status, expected_ids in expected_by_status.items():
            filtered = self.test.run_cmd_json(["sprint", "list", "-r", r, "--status", status])
            got_ids = {s["id"] for s in filtered}
            assert got_ids == expected_ids, (
                f"--status {status}: expected sprints {expected_ids}, got {got_ids}"
            )
            for entry in filtered:
                assert entry["status"] == status, entry
                exp_count, exp_tasks = truth[entry["id"]]
                assert entry["task_count"] == exp_count, (
                    f"--status {status}: sprint {entry['id']} task_count changed "
                    f"from {exp_count} to {entry['task_count']}"
                )
                assert entry["tasks"] == exp_tasks, (
                    f"--status {status}: sprint {entry['id']} tasks changed "
                    f"from {exp_tasks} to {entry['tasks']}"
                )
        print("✓ --status narrows the sprint listing without altering any returned sprint's membership")

    # ---- aliasing regression ----------------------------------------------

    def test_multirow_timestamps_not_aliased(self):
        """Each task in a multi-row result must carry its OWN lifecycle
        timestamps. Invariant: the values from any multi-row listing must equal
        the values returned by `task get <id>` (single row, always correct)."""
        r, ids = self._build()
        ts_fields = ("started_at", "tested_at", "closed_at", "completion_summary")

        # Ground truth: per-id single-row values.
        truth = {}
        for key in ("t3", "t4", "t6", "t11", "t14", "t1", "t7"):
            tid = ids[key]
            tk = self._as_task(self.test.run_cmd_json(["task", "get", "-r", r, str(tid)]))
            truth[tid] = {f: tk.get(f) for f in ts_fields}

        # Distinctness sanity: the three S1 completions happened at different
        # times, so their closed_at must not all be identical (that was the bug).
        closed = [truth[ids[k]]["closed_at"] for k in ("t3", "t4", "t11")]
        assert len(set(closed)) >= 2, f"completions should have distinct closed_at, got {closed}"

        # Every multi-row view must match the single-row truth for each task.
        listings = {
            "task list": ["task", "list", "-r", r],
            "task get multi": ["task", "get", "-r", r,
                               ",".join(str(ids[k]) for k in ("t3", "t4", "t11", "t6", "t14"))],
            "sprint tasks s1": ["sprint", "tasks", "-r", r, str(ids["s1"])],
            "sprint open-tasks s2": ["sprint", "open-tasks", "-r", r, str(ids["s2"])],
            "task next": ["task", "next", "-r", r, "5"],
        }
        for name, cmd in listings.items():
            rows = self.test.run_cmd_json(cmd)
            for row in rows:
                tid = row["id"]
                if tid in truth:
                    for f in ts_fields:
                        assert row.get(f) == truth[tid][f], (
                            f"{name}: task {tid} field {f} = {row.get(f)!r} "
                            f"but task get reports {truth[tid][f]!r} (aliasing regression)"
                        )
        print("✓ multi-row task timestamps are not aliased")

    # ---- dependency fields in every view ----------------------------------

    def test_dependency_fields_populated(self):
        """depends_on / blocks must be populated wherever a task object is
        returned (t2 depends on t1; t1 has subtask 'sub')."""
        r, ids = self._build()
        t1, t2 = ids["t1"], ids["t2"]

        def find(rows, tid):
            for row in rows:
                if row["id"] == tid:
                    return row
            return None

        # task get (baseline) -- already correct.
        g1 = self._as_task(self.test.run_cmd_json(["task", "get", "-r", r, str(t1)]))
        assert g1["blocks"] == [t2] and g1["depends_on"] == [], g1

        # sprint tasks (t1 is a member of s1)
        srow = find(self.test.run_cmd_json(["sprint", "tasks", "-r", r, str(ids["s1"])]), t1)
        assert srow is not None and srow["blocks"] == [t2], f"sprint tasks must populate blocks: {srow}"

        # blocking t1 should return t2, and that task object must report depends_on=[t1]
        bl = self.test.run_cmd_json(["task", "blocking", "-r", r, str(t1)])
        b2 = find(bl, t2)
        assert b2 is not None, "task blocking t1 must list t2"
        assert b2["depends_on"] == [t1], f"task blocking must populate depends_on: {b2}"

        # blockers of t2 = incomplete deps of t2 = t1 (t1 is DOING, not complete)
        brs = self.test.run_cmd_json(["task", "blockers", "-r", r, str(t2)])
        assert find(brs, t1) is not None, "t1 (incomplete) must be a blocker of t2"
        print("✓ depends_on/blocks populated in all task views")

    # ---- task list filters / sort / limit ---------------------------------

    def test_task_list_filters(self):
        r, ids = self._build()
        completed = self.test.run_cmd_json(["task", "list", "-r", r, "--status", "COMPLETED"])
        assert {x["id"] for x in completed} == {ids["t3"], ids["t4"], ids["t11"], ids["t6"], ids["t14"]}, \
            sorted(x["id"] for x in completed)
        assert all(x["status"] == "COMPLETED" for x in completed)

        backlog = self.test.run_cmd_json(["task", "list", "-r", r, "--status", "BACKLOG"])
        assert {x["id"] for x in backlog} == {ids["t2"], ids["t5"], ids["t13"], ids["sub"]}, \
            sorted(x["id"] for x in backlog)

        stories = self.test.run_cmd_json(["task", "list", "-r", r, "--type", "USER_STORY"])
        assert {x["id"] for x in stories} == {ids["t1"], ids["t5"], ids["t6"], ids["t9"], ids["t10"]}, \
            sorted(x["id"] for x in stories)
        assert all(x["type"] == "USER_STORY" for x in stories)

        # priority filter keeps priority >= min
        hi = self.test.run_cmd_json(["task", "list", "-r", r, "--priority", "8"])
        assert all(x["priority"] >= 8 for x in hi), [x["priority"] for x in hi]
        assert {x["id"] for x in hi} == {ids["t1"], ids["t3"], ids["t6"]}, sorted(x["id"] for x in hi)

        # limit caps the count
        limited = self.test.run_cmd_json(["task", "list", "-r", r, "--limit", "3"])
        assert len(limited) == 3, len(limited)
        print("✓ task list filters / sort / limit")

    # ---- task next ---------------------------------------------------------

    def test_task_next(self):
        r, ids = self._build()
        nxt = self.test.run_cmd_json(["task", "next", "-r", r, "5"])
        # OPEN sprint is s2; incomplete members are t7 (DOING, p7) and t8 (TESTING, p4).
        assert {x["id"] for x in nxt} == {ids["t7"], ids["t8"]}, sorted(x["id"] for x in nxt)
        # ordered by priority descending
        prios = [x["priority"] for x in nxt]
        assert prios == sorted(prios, reverse=True), prios
        assert nxt[0]["id"] == ids["t7"], "highest-priority incomplete task first"
        print("✓ task next from OPEN sprint")

    # ---- backlog -----------------------------------------------------------

    def test_backlog(self):
        r, ids = self._build()
        bl = self.test.run_cmd_json(["backlog", "list", "-r", r])
        assert {x["id"] for x in bl} == {ids["t2"], ids["t5"], ids["t13"], ids["sub"]}, sorted(x["id"] for x in bl)
        assert all(x["status"] == "BACKLOG" for x in bl)

        nxt = self.test.run_cmd_json(["backlog", "show-next", "-r", r, "2"])
        assert len(nxt) == 2, len(nxt)
        # priority order: t2(7), t5(6), sub(6), t13(4) -> top 2 are t2 then t5
        assert nxt[0]["id"] == ids["t2"], nxt
        assert [x["priority"] for x in nxt] == sorted([x["priority"] for x in nxt], reverse=True)
        print("✓ backlog list / show-next")

    # ---- subtasks ----------------------------------------------------------

    def test_subtasks(self):
        r, ids = self._build()
        subs = self.test.run_cmd_json(["task", "subtasks", "-r", r, str(ids["t1"])])
        assert [x["id"] for x in subs] == [ids["sub"]], subs
        assert subs[0]["parent_task_id"] == ids["t1"], subs[0]
        g1 = self._as_task(self.test.run_cmd_json(["task", "get", "-r", r, str(ids["t1"])]))
        assert g1["subtask_count"] == 1, g1["subtask_count"]
        print("✓ subtasks / parent linkage")

    # ---- sprint tasks vs open-tasks ---------------------------------------

    def test_sprint_tasks_vs_open(self):
        r, ids = self._build()
        all_tasks = self.test.run_cmd_json(["sprint", "tasks", "-r", r, str(ids["s2"])])
        open_tasks = self.test.run_cmd_json(["sprint", "open-tasks", "-r", r, str(ids["s2"])])
        assert {x["id"] for x in all_tasks} == {ids["t6"], ids["t7"], ids["t8"], ids["t14"]}, all_tasks
        # open-tasks excludes COMPLETED (t6, t14)
        assert {x["id"] for x in open_tasks} == {ids["t7"], ids["t8"]}, open_tasks
        assert all(x["status"] in ("SPRINT", "DOING", "TESTING") for x in open_tasks)
        print("✓ sprint tasks (all) vs open-tasks (incomplete)")

    # ---- audit -------------------------------------------------------------

    def test_audit_stats(self):
        r, ids = self._build()
        st = self.test.run_cmd_json(["audit", "stats", "-r", r])
        # Documented (real) schema: no 'period'.
        assert set(st.keys()) == {"by_operation", "by_entity_type",
                                  "first_entry_at", "last_entry_at", "total_entries"}, set(st.keys())
        assert "period" not in st
        # Internal consistency.
        assert st["total_entries"] == sum(st["by_operation"].values()), st
        assert st["total_entries"] == sum(st["by_entity_type"].values()), st
        # 15 tasks (14 + subtask) created, 4 sprints created.
        assert st["by_operation"].get("TASK_CREATE") == 15, st["by_operation"]
        assert st["by_operation"].get("SPRINT_CREATE") == 4, st["by_operation"]
        assert st["by_entity_type"].get("TASK", 0) > 0 and st["by_entity_type"].get("SPRINT", 0) > 0
        assert st["first_entry_at"] is not None and st["last_entry_at"] is not None
        assert st["first_entry_at"] <= st["last_entry_at"]
        print("✓ audit stats correctness")

    def test_audit_list_and_history(self):
        r, ids = self._build()
        rows = self.test.run_cmd_json(["audit", "list", "-r", r, "--limit", "10"])
        assert len(rows) <= 10 and len(rows) > 0
        # newest first
        times = [row["performed_at"] for row in rows]
        assert times == sorted(times, reverse=True), "audit list must be newest-first"

        hist = self.test.run_cmd_json(["audit", "history", "-r", r, "TASK", str(ids["t1"])])
        assert all(h["entity_type"] == "TASK" and h["entity_id"] == ids["t1"] for h in hist), hist
        ops = {h["operation"] for h in hist}
        assert "TASK_CREATE" in ops and "TASK_ADD_DEP" in ops, ops
        print("✓ audit list / history correctness")

    # ---- velocity floor (focused) -----------------------------------------

    def test_velocity_floor_subday(self):
        """A sub-day sprint must not produce an inflated velocity."""
        r = self.test.create_roadmap("velocity_check")
        s = self.test.create_sprint(r, "Same-day sprint")
        ids = [self._mk_task(r, f"Task {i}", "TASK", 5, 3) for i in range(4)]
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(s), ",".join(str(i) for i in ids)])
        self.test.run_cmd(["sprint", "start", "-r", r, str(s)])
        for tid in ids:  # complete all 4 quickly (sub-day)
            self._complete(r, tid)
        self.test.run_cmd(["sprint", "close", "-r", r, str(s)])

        sst = self.test.run_cmd_json(["sprint", "stats", "-r", r, str(s)])
        assert abs(sst["velocity"] - 4.0) < 1e-9, f"sub-day velocity should be 4.0, got {sst['velocity']}"
        rst = self.test.run_cmd_json(["stats", "-r", r])
        assert abs(rst["average_velocity"] - 4.0) < 1e-9, rst["average_velocity"]
        print("✓ velocity floored at 1 day for sub-day sprints")

    # ---- roadmap list ------------------------------------------------------

    def test_roadmap_list(self):
        r, ids = self._build()
        rows = self.test.run_cmd_json(["roadmap", "list"])
        names = {x["name"] for x in rows}
        assert r in names, names
        entry = next(x for x in rows if x["name"] == r)
        assert entry["path"].endswith(f".roadmaps/{r}/project.db"), entry["path"]
        assert entry["size"] > 0
        print("✓ roadmap list correctness")


def main():
    test = TestQueryCommandsCorrectness()
    methods = sorted(m for m in dir(test) if m.startswith("test_"))
    passed = 0
    failed = 0
    for name in methods:
        test.setup_method()
        try:
            getattr(test, name)()
            passed += 1
        except Exception as e:
            print(f"✗ {name} failed: {e}")
            failed += 1
        finally:
            test.teardown_method()
    print(f"\n{passed} passed, {failed} failed")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if main() else 1)
