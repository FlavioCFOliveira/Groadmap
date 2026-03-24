#!/usr/bin/env python3
"""Test 22: Sprint Capacity Management -- Task 87

Validates acceptance criteria for Task 87:
  AC1: sprint create --max-tasks N defines capacity (max_tasks in sprint get)
  AC2: sprint add-tasks fails with descriptive error when exceeding max_tasks
  AC3: sprint show includes current_load and capacity_pct fields
  AC4: Sprint without max_tasks works as before (null fields, no limit)
  AC5: sprint update --max-tasks N alters capacity
  AC6: capacity_pct is 0.0 when sprint has max_tasks but no active tasks
  AC7: Schema migration: existing sprints retain max_tasks=null
"""

import sys
import os
import subprocess
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------


def _create_task(test, roadmap, title, priority=5):
    """Create a realistic task and return its ID."""
    return test.create_task(
        roadmap=roadmap,
        title=title,
        functional_requirements=(
            "Feature supports sprint capacity management workflow. "
            "Teams track how many tasks are actively in progress."
        ),
        technical_requirements=(
            "max_tasks field in sprints schema; enforce limit in sprint add-tasks; "
            "expose current_load and capacity_pct in sprint show output."
        ),
        acceptance_criteria=(
            "sprint get returns max_tasks=N when set; sprint show has current_load and capacity_pct; "
            "add-tasks fails with descriptive error when load would exceed max_tasks."
        ),
        priority=priority,
    )


# ---------------------------------------------------------------------------
# AC1: sprint create --max-tasks N
# ---------------------------------------------------------------------------


class TestAC1SprintCreateMaxTasks:
    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_max_tasks_stored_on_create(self):
        """sprint create --max-tasks 3 stores max_tasks=3 in sprint get output."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Sprint Alpha - OAuth2 and DB connection pool refactor",
            "--max-tasks", "3",
        ])["id"]

        sprint = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        actual = sprint.get("max_tasks")
        assert actual == 3, f"AC1 FAIL: expected max_tasks=3 in sprint get, got {actual}"
        print("AC1 PASS: sprint create --max-tasks 3 stores max_tasks=3")

    def test_min_value_1_accepted(self):
        """Minimum valid value 1 is stored correctly."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Sprint for single critical security fix",
            "--max-tasks", "1",
        ])["id"]

        sprint = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        actual = sprint.get("max_tasks")
        assert actual == 1, f"AC1 FAIL: expected max_tasks=1 (min), got {actual}"
        print("AC1 PASS: --max-tasks 1 minimum value stored correctly")

    def test_large_value_accepted(self):
        """Large values such as 100 are stored correctly."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Bulk migration sprint with large task pool",
            "--max-tasks", "100",
        ])["id"]

        sprint = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        actual = sprint.get("max_tasks")
        assert actual == 100, f"AC1 FAIL: expected max_tasks=100, got {actual}"
        print("AC1 PASS: --max-tasks 100 large value stored correctly")


# ---------------------------------------------------------------------------
# AC2: sprint add-tasks enforces capacity
# Note: sprint add-tasks automatically sets task status to SPRINT.
# Capacity uses GetActiveSprintTasks: SPRINT+DOING+TESTING statuses.
# ---------------------------------------------------------------------------


class TestAC2CapacityEnforcement:
    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def _make_capped_sprint(self, roadmap, cap):
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", f"Capped sprint capacity={cap} for rate limiting tests",
            "--max-tasks", str(cap),
        ])["id"]
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])
        return sprint_id

    def test_add_beyond_capacity_fails_with_exit_nonzero(self):
        """Adding task(s) that exceed max_tasks returns non-zero exit code."""
        roadmap = self.test.create_roadmap()
        sprint_id = self._make_capped_sprint(roadmap, 2)

        t1 = _create_task(self.test, roadmap, "Implement OAuth2 device-code flow for CLI")
        t2 = _create_task(self.test, roadmap, "Refactor SQLite connection pool with caching")
        t3 = _create_task(self.test, roadmap, "Add Prometheus metrics endpoint to API server")

        # Fill sprint to capacity (add-tasks sets tasks to SPRINT status automatically)
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), f"{t1},{t2}"])

        # Now at 2/2 -- adding t3 must fail
        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(t3)],
            check=False,
        )
        assert exit_code != 0, "AC2 FAIL: expected non-zero exit when exceeding capacity, got 0"
        err_lower = stderr.lower()
        assert any(kw in err_lower for kw in ["capacity", "exceed", "max", "limit"]), (
            f"AC2 FAIL: error not descriptive: {stderr!r}"
        )
        print(f"AC2 PASS: add-tasks blocked at 2/2 capacity, exit={exit_code}, err={stderr!r}")

    def test_filling_to_exact_capacity_succeeds(self):
        """Adding tasks up to max_tasks limit must succeed."""
        roadmap = self.test.create_roadmap()
        sprint_id = self._make_capped_sprint(roadmap, 3)

        tasks = [
            _create_task(self.test, roadmap, "Deploy API gateway with rate limiting"),
            _create_task(self.test, roadmap, "Configure mTLS for internal service mesh"),
            _create_task(self.test, roadmap, "Implement circuit breaker for payment service"),
        ]
        ids_arg = ",".join(str(t) for t in tasks)
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), ids_arg])

        show = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])
        load = show.get("current_load")
        assert load == 3, f"AC2 precondition FAIL: expected current_load=3, got {load}"
        print("AC2 PASS: sprint filled to exact capacity (3/3) without error")

    def test_bulk_add_exceeding_capacity_rejected(self):
        """Bulk add where total exceeds limit is rejected entirely."""
        roadmap = self.test.create_roadmap()
        sprint_id = self._make_capped_sprint(roadmap, 2)

        t1 = _create_task(self.test, roadmap, "Write load tests for authentication service")
        t2 = _create_task(self.test, roadmap, "Fix memory leak in WebSocket connection handler")
        t3 = _create_task(self.test, roadmap, "Upgrade Go runtime from 1.21 to 1.23")

        # Add t1 to consume 1 slot; capacity=2, active=1
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(t1)])

        # Try to add t2 and t3 together (2 tasks, only 1 slot free) -- must fail
        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", roadmap, str(sprint_id), f"{t2},{t3}"],
            check=False,
        )
        assert exit_code != 0, "AC2 FAIL: bulk add exceeding capacity should fail"
        print(f"AC2 PASS: bulk add {t2},{t3} to 1/2 capacity sprint rejected, exit={exit_code}")


# ---------------------------------------------------------------------------
# AC3: sprint show has current_load and capacity_pct
# ---------------------------------------------------------------------------


class TestAC3SprintShowFields:
    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_sprint_show_has_both_capacity_fields(self):
        """sprint show output must contain current_load and capacity_pct fields."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Sprint Beta - API rate limiting and monitoring setup",
            "--max-tasks", "4",
        ])["id"]
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        t1 = _create_task(self.test, roadmap, "Implement exponential backoff for retry logic")
        t2 = _create_task(self.test, roadmap, "Add distributed tracing with OpenTelemetry")

        # add-tasks transitions both to SPRINT status (counts as active load)
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), f"{t1},{t2}"])

        show = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])

        assert "current_load" in show, (
            f"AC3 FAIL: current_load missing. Keys: {list(show.keys())}"
        )
        assert "capacity_pct" in show, (
            f"AC3 FAIL: capacity_pct missing. Keys: {list(show.keys())}"
        )
        load = show["current_load"]
        pct = show["capacity_pct"]
        assert load == 2, f"AC3 FAIL: expected current_load=2, got {load}"
        assert pct is not None, "AC3 FAIL: capacity_pct is null with max_tasks=4"
        # 2 active tasks / 4 max = 50.0%
        assert abs(pct - 50.0) < 0.01, f"AC3 FAIL: expected capacity_pct~50.0, got {pct}"
        print(f"AC3 PASS: sprint show current_load={load}, capacity_pct={pct}")

    def test_current_load_equals_sprint_status_task_count(self):
        """current_load counts only active (SPRINT/DOING/TESTING) tasks in sprint."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Sprint Gamma - verify active task counting",
            "--max-tasks", "5",
        ])["id"]
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        t1 = _create_task(self.test, roadmap, "Document REST API with OpenAPI 3.1")
        t2 = _create_task(self.test, roadmap, "Implement DB migration rollback support")

        # Add both; both become SPRINT status (active)
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), f"{t1},{t2}"])

        # Advance t1 further to DOING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(t1), "DOING"])

        show = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])
        load = show["current_load"]
        pct = show["capacity_pct"]
        # Both t1 (DOING) and t2 (SPRINT) count toward current_load
        assert load == 2, f"AC3 FAIL: DOING+SPRINT should both count; expected load=2, got {load}"
        # 2/5 = 40.0%
        assert abs(pct - 40.0) < 0.01, f"AC3 FAIL: expected capacity_pct~40.0, got {pct}"
        print("AC3 PASS: DOING and SPRINT tasks both count toward current_load")


# ---------------------------------------------------------------------------
# AC4: Sprint without max_tasks works as before
# ---------------------------------------------------------------------------


class TestAC4UnlimitedSprint:
    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_sprint_without_max_tasks_has_null_fields(self):
        """Sprint without --max-tasks: max_tasks=null, capacity_pct=null in sprint get/show."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Sprint Delta - unlimited capacity for backlog grooming",
        ])["id"]
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        sprint = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        mt = sprint.get("max_tasks")
        assert mt is None, f"AC4 FAIL: expected max_tasks=null in sprint get, got {mt}"

        task_id = _create_task(self.test, roadmap, "Profile hot path in task ordering query")
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)])

        show = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])
        mt_show = show.get("max_tasks")
        cp_show = show.get("capacity_pct")
        cl_show = show.get("current_load")
        assert mt_show is None, f"AC4 FAIL: sprint show max_tasks should be null, got {mt_show}"
        assert cp_show is None, f"AC4 FAIL: capacity_pct should be null without max_tasks, got {cp_show}"
        assert cl_show == 1, f"AC4 FAIL: current_load should be 1, got {cl_show}"
        print("AC4 PASS: unlimited sprint has null max_tasks, null capacity_pct, correct current_load")

    def test_unlimited_sprint_accepts_many_tasks_without_error(self):
        """Sprint without max_tasks accepts any number of tasks."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Sprint Epsilon - no capacity restriction on large backlog",
        ])["id"]
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        task_ids = [
            _create_task(self.test, roadmap, f"Migrate service {i} to gRPC transport")
            for i in range(1, 6)
        ]
        ids_arg = ",".join(str(t) for t in task_ids)
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), ids_arg])

        show = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])
        load = show["current_load"]
        cp = show.get("capacity_pct")
        assert load == 5, f"AC4 FAIL: expected current_load=5, got {load}"
        assert cp is None, f"AC4 FAIL: capacity_pct should be null without max_tasks, got {cp}"
        print("AC4 PASS: unlimited sprint accepted 5 tasks without capacity error")


# ---------------------------------------------------------------------------
# AC5: sprint update --max-tasks N alters capacity
# ---------------------------------------------------------------------------


class TestAC5SprintUpdateMaxTasks:
    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_update_increases_capacity(self):
        """sprint update --max-tasks can increase the capacity limit."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Sprint Zeta - initial capacity 2, to be expanded",
            "--max-tasks", "2",
        ])["id"]

        self.test.run_cmd([
            "sprint", "update", "-r", roadmap, str(sprint_id), "--max-tasks", "5",
        ])

        sprint = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        mt = sprint.get("max_tasks")
        assert mt == 5, f"AC5 FAIL: expected max_tasks=5 after increase, got {mt}"
        print("AC5 PASS: sprint update --max-tasks 5 increased capacity from 2 to 5")

    def test_update_decreases_capacity(self):
        """sprint update --max-tasks can decrease the capacity limit."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Sprint Eta - capacity reduction scenario",
            "--max-tasks", "10",
        ])["id"]

        self.test.run_cmd([
            "sprint", "update", "-r", roadmap, str(sprint_id), "--max-tasks", "3",
        ])

        sprint = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        mt = sprint.get("max_tasks")
        assert mt == 3, f"AC5 FAIL: expected max_tasks=3 after reduction, got {mt}"
        print("AC5 PASS: sprint update --max-tasks 3 reduced capacity from 10 to 3")

    def test_update_adds_max_tasks_to_unlimited_sprint(self):
        """sprint update can add max_tasks to a sprint that had none."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Sprint Theta - originally unlimited, gaining capacity limit",
        ])["id"]

        before = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        assert before.get("max_tasks") is None, "AC5 precondition FAIL: sprint should start null"

        self.test.run_cmd([
            "sprint", "update", "-r", roadmap, str(sprint_id), "--max-tasks", "7",
        ])

        after = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        mt = after.get("max_tasks")
        assert mt == 7, f"AC5 FAIL: expected max_tasks=7 after first-time set, got {mt}"
        print("AC5 PASS: sprint update --max-tasks 7 added capacity to previously unlimited sprint")


# ---------------------------------------------------------------------------
# AC6: capacity_pct=0.0 when max_tasks set and no active tasks
# ---------------------------------------------------------------------------


class TestAC6CapacityPctZero:
    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_capacity_pct_zero_on_empty_sprint_with_max_tasks(self):
        """Freshly started sprint with max_tasks but no tasks has capacity_pct=0.0."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Sprint Iota - freshly created with capacity limit set",
            "--max-tasks", "4",
        ])["id"]
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        show = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])
        load = show.get("current_load")
        pct = show.get("capacity_pct")
        mt = show.get("max_tasks")
        assert load == 0, f"AC6 FAIL: expected current_load=0 on empty sprint, got {load}"
        assert pct == 0.0, f"AC6 FAIL: expected capacity_pct=0.0, got {pct}"
        assert mt == 4, f"AC6 FAIL: expected max_tasks=4, got {mt}"
        print(f"AC6 PASS: empty sprint with max_tasks=4 shows capacity_pct=0.0")


# ---------------------------------------------------------------------------
# AC6: capacity_pct=0.0 when max_tasks set and no active tasks
# ---------------------------------------------------------------------------


class TestAC6CapacityPctZero:
    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_capacity_pct_zero_on_empty_sprint_with_max_tasks(self):
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Sprint Iota - freshly created with capacity limit set",
            "--max-tasks", "4",
        ])["id"]
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        show = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint_id)])
        load = show.get("current_load")
        pct = show.get("capacity_pct")
        mt = show.get("max_tasks")
        assert load == 0, f"AC6 FAIL: expected current_load=0 on empty sprint, got {load}"
        assert pct == 0.0, f"AC6 FAIL: expected capacity_pct=0.0, got {pct}"
        assert mt == 4, f"AC6 FAIL: expected max_tasks=4, got {mt}"
        print(f"AC6 PASS: empty sprint with max_tasks=4 shows capacity_pct=0.0")


# ---------------------------------------------------------------------------
# AC7: Schema migration
# ---------------------------------------------------------------------------


class TestAC7SchemaMigration:
    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_sprint_without_max_tasks_shows_null_in_get(self):
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Pre-capacity sprint (omits --max-tasks, simulating pre-migration row)",
        ])["id"]

        sprint = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        mt = sprint.get("max_tasks")
        assert mt is None, f"AC7 FAIL: expected max_tasks=null, got {mt}"
        print("AC7 PASS: sprint without --max-tasks shows max_tasks=null in sprint get")

    def test_sprint_list_differentiates_capped_and_unlimited(self):
        roadmap = self.test.create_roadmap()
        capped_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Capped sprint - infrastructure security hardening",
            "--max-tasks", "6",
        ])["id"]
        unlimited_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Unlimited sprint - exploratory research tasks",
        ])["id"]

        sprints = self.test.run_cmd_json(["sprint", "list", "-r", roadmap])
        capped = next((s for s in sprints if s["id"] == capped_id), None)
        unlimited = next((s for s in sprints if s["id"] == unlimited_id), None)

        assert capped is not None, f"AC7 FAIL: capped sprint {capped_id} not in sprint list"
        assert unlimited is not None, f"AC7 FAIL: unlimited sprint {unlimited_id} not in sprint list"
        c_mt = capped["max_tasks"]
        u_mt = unlimited["max_tasks"]
        assert c_mt == 6, f"AC7 FAIL: capped sprint max_tasks should be 6, got {c_mt}"
        assert u_mt is None, f"AC7 FAIL: unlimited sprint max_tasks should be null, got {u_mt}"
        print("AC7 PASS: sprint list correctly shows max_tasks=6 and null for respective sprints")

    def test_go_migration_unit_tests_pass(self):
        project_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
        result = subprocess.run(
            ["go", "test", "-v", "-run", "TestMigration", "./internal/db/..."],
            capture_output=True, text=True, cwd=project_root,
        )
        failed_msg = f"AC7 FAIL: migration tests exit={result.returncode}"
        assert result.returncode == 0, failed_msg
        count = result.stdout.count("--- PASS:")
        print(f"AC7 PASS: {count} Go migration unit tests passed")


# ---------------------------------------------------------------------------
# Edge cases: invalid --max-tasks inputs
# ---------------------------------------------------------------------------


class TestMaxTasksInputValidation:
    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_max_tasks_zero_rejected(self):
        roadmap = self.test.create_roadmap()
        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "create", "-r", roadmap,
             "-d", "Sprint with zero capacity", "--max-tasks", "0"],
            check=False,
        )
        assert exit_code != 0, "EDGE FAIL: --max-tasks 0 should be rejected"
        print(f"EDGE PASS: --max-tasks 0 rejected, exit={exit_code}, err={stderr!r}")

    def test_max_tasks_negative_rejected(self):
        roadmap = self.test.create_roadmap()
        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "create", "-r", roadmap,
             "-d", "Sprint with negative capacity", "--max-tasks", "-5"],
            check=False,
        )
        assert exit_code != 0, "EDGE FAIL: --max-tasks negative should be rejected"
        print(f"EDGE PASS: --max-tasks -5 rejected, exit={exit_code}")

    def test_max_tasks_non_integer_rejected(self):
        roadmap = self.test.create_roadmap()
        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "create", "-r", roadmap,
             "-d", "Sprint with string capacity", "--max-tasks", "ten"],
            check=False,
        )
        assert exit_code != 0, "EDGE FAIL: --max-tasks non-integer should be rejected"
        print(f"EDGE PASS: --max-tasks non-integer rejected, exit={exit_code}, err={stderr!r}")

    def test_max_tasks_float_rejected(self):
        roadmap = self.test.create_roadmap()
        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "create", "-r", roadmap,
             "-d", "Sprint with float capacity", "--max-tasks", "2.5"],
            check=False,
        )
        assert exit_code != 0, "EDGE FAIL: --max-tasks float should be rejected"
        print(f"EDGE PASS: --max-tasks float rejected, exit={exit_code}, err={stderr!r}")

    def test_sprint_update_max_tasks_zero_rejected(self):
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.run_cmd_json([
            "sprint", "create", "-r", roadmap,
            "-d", "Sprint for update validation test", "--max-tasks", "3",
        ])["id"]
        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "update", "-r", roadmap, str(sprint_id), "--max-tasks", "0"],
            check=False,
        )
        assert exit_code != 0, "EDGE FAIL: sprint update --max-tasks 0 should be rejected"
        print(f"EDGE PASS: sprint update --max-tasks 0 rejected, exit={exit_code}")


# ---------------------------------------------------------------------------
# Entry point for direct execution
# ---------------------------------------------------------------------------


if __name__ == "__main__":
    import traceback

    suites = [
        TestAC1SprintCreateMaxTasks,
        TestAC2CapacityEnforcement,
        TestAC3SprintShowFields,
        TestAC4UnlimitedSprint,
        TestAC5SprintUpdateMaxTasks,
        TestAC6CapacityPctZero,
        TestAC7SchemaMigration,
        TestMaxTasksInputValidation,
    ]

    passed = 0
    failed = 0
    failures = []

    for suite_class in suites:
        methods = sorted(m for m in dir(suite_class) if m.startswith("test_"))
        for method_name in methods:
            suite = suite_class()
            suite.setup_method()
            try:
                getattr(suite, method_name)()
                passed += 1
            except Exception as exc:
                label = f"{suite_class.__name__}.{method_name}"
                print(f"FAIL  {label}: {exc}")
                traceback.print_exc()
                failures.append((label, str(exc)))
                failed += 1
            finally:
                suite.teardown_method()

    total = passed + failed
    print()
    print("=" * 65)
    print("Task 87 - Sprint Capacity Management - Test Results")
    print("=" * 65)
    print(f"Total: {total} | Passed: {passed} | Failed: {failed}")
    if failures:
        print("")
        print("Failed tests:")
        for label, exc_msg in failures:
            print(f"  [X] {label}")
            print(f"      -> {exc_msg}")
    print()
    if failed == 0:
        print("OVERALL: PASS")
    else:
        print(f"OVERALL: FAIL ({failed} tests failed)")
    sys.exit(0 if failed == 0 else 1)
