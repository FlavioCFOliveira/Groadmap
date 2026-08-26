#!/usr/bin/env python3
"""
Test 13: Sprint Task Ordering
Tests all sprint task ordering commands: reorder, move-to, swap, top, bottom.
Validates actual task positions after each operation.
"""

import sys
import os
import sqlite3
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


# The ordering index exactly as schema 1.12.0 declared it: same pair of columns,
# NOT unique. Transcribed rather than derived, because a fixture for a historical
# schema must not follow later changes to the shipped DDL, or the migration it
# exercises stops being the migration that ships.
SPRINT_TASKS_ORDER_INDEX_1_12_0 = (
    "CREATE INDEX idx_sprint_tasks_order ON sprint_tasks(sprint_id, position ASC)"
)


class TestSprintTaskOrdering:
    """Test sprint task ordering operations."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def _get_task_order(self, roadmap: str, sprint_id: int) -> list:
        """Get the ordered list of task IDs in a sprint via task_order field."""
        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])
        return result.get("task_order", [])

    def _create_test_tasks(self, roadmap: str, count: int) -> list:
        """Create multiple test tasks and return their IDs."""
        task_ids = []
        for i in range(1, count + 1):
            task_id = self.test.create_task(
                roadmap,
                f"Ordering Test Task {i}",
                f"Functional requirements for task {i}",
                f"Technical implementation details for task {i}",
                f"Acceptance criteria for task {i}"
            )
            task_ids.append(task_id)
        return task_ids

    def test_reorder_sets_exact_sequence(self):
        """Test that reorder command sets exact task sequence."""
        roadmap = self.test.create_roadmap()

        # Create sprint and tasks
        sprint_id = self.test.create_sprint(roadmap, "Sprint for Reorder Testing")
        task_ids = self._create_test_tasks(roadmap, 5)

        # Add tasks to sprint in original order
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Verify initial order
        initial_order = self._get_task_order(roadmap, sprint_id)
        assert initial_order == task_ids, f"Initial order mismatch: {initial_order} vs {task_ids}"

        # Reorder with custom sequence: task5, task3, task1, task4, task2
        new_order = [task_ids[4], task_ids[2], task_ids[0], task_ids[3], task_ids[1]]
        self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id),
            ",".join(map(str, new_order))
        ])

        # Verify exact order
        actual_order = self._get_task_order(roadmap, sprint_id)
        assert actual_order == new_order, f"Reorder failed: expected {new_order}, got {actual_order}"

        # Test another reorder: reverse order
        reverse_order = list(reversed(task_ids))
        self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id),
            ",".join(map(str, reverse_order))
        ])

        actual_order = self._get_task_order(roadmap, sprint_id)
        assert actual_order == reverse_order, f"Reverse reorder failed: expected {reverse_order}, got {actual_order}"

        print("Reorder sets exact sequence test passed")

    def test_move_to_exact_position(self):
        """Test that move-to places task at exact position."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Move-To Testing")
        task_ids = self._create_test_tasks(roadmap, 5)

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Move last task to position 0 (top)
        self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id),
            str(task_ids[4]), "0"
        ])

        order = self._get_task_order(roadmap, sprint_id)
        assert order[0] == task_ids[4], f"Move-to position 0 failed: task should be first, got {order}"

        # Move first task to position 2 (middle)
        self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id),
            str(task_ids[0]), "2"
        ])

        order = self._get_task_order(roadmap, sprint_id)
        assert order[2] == task_ids[0], f"Move-to position 2 failed: task should be at index 2, got {order}"

        # Move task to last position (position 4)
        self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id),
            str(task_ids[1]), "4"
        ])

        order = self._get_task_order(roadmap, sprint_id)
        assert order[4] == task_ids[1], f"Move-to last position failed: task should be at index 4, got {order}"

        print("Move-to exact position test passed")

    def test_swap_exchanges_positions(self):
        """Test that swap command exchanges two task positions."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Swap Testing")
        task_ids = self._create_test_tasks(roadmap, 4)

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Verify initial order
        initial_order = self._get_task_order(roadmap, sprint_id)
        assert initial_order == task_ids, f"Initial order mismatch: {initial_order}"

        # Swap first and last tasks
        self.test.run_cmd([
            "sprint", "swap", "-r", roadmap, str(sprint_id),
            str(task_ids[0]), str(task_ids[3])
        ])

        order = self._get_task_order(roadmap, sprint_id)
        assert order[0] == task_ids[3], f"Swap failed: first position should be task {task_ids[3]}, got {order}"
        assert order[3] == task_ids[0], f"Swap failed: last position should be task {task_ids[0]}, got {order}"

        # Swap middle tasks
        self.test.run_cmd([
            "sprint", "swap", "-r", roadmap, str(sprint_id),
            str(task_ids[1]), str(task_ids[2])
        ])

        order = self._get_task_order(roadmap, sprint_id)
        # After two swaps: [task3, task2, task1, task0]
        expected = [task_ids[3], task_ids[2], task_ids[1], task_ids[0]]
        assert order == expected, f"Second swap failed: expected {expected}, got {order}"

        print("Swap exchanges positions test passed")

    def test_top_moves_to_first_position(self):
        """Test that top command moves task to first position (position 0)."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Top Command Testing")
        task_ids = self._create_test_tasks(roadmap, 5)

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Move last task to top
        self.test.run_cmd([
            "sprint", "top", "-r", roadmap, str(sprint_id), str(task_ids[4])
        ])

        order = self._get_task_order(roadmap, sprint_id)
        assert order[0] == task_ids[4], f"Top command failed: first position should be task {task_ids[4]}, got {order}"
        assert len(order) == 5, f"Task count should remain 5, got {len(order)}"

        # Move middle task to top
        self.test.run_cmd([
            "sprint", "top", "-r", roadmap, str(sprint_id), str(task_ids[2])
        ])

        order = self._get_task_order(roadmap, sprint_id)
        assert order[0] == task_ids[2], f"Top command failed: first position should be task {task_ids[2]}, got {order}"

        # Move first task to top (should remain first)
        self.test.run_cmd([
            "sprint", "top", "-r", roadmap, str(sprint_id), str(task_ids[2])
        ])

        order = self._get_task_order(roadmap, sprint_id)
        assert order[0] == task_ids[2], f"Top command on first task failed: should remain first, got {order}"

        print("Top moves to first position test passed")

    def test_bottom_moves_to_last_position(self):
        """Test that bottom command moves task to last position."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Bottom Command Testing")
        task_ids = self._create_test_tasks(roadmap, 5)

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Move first task to bottom
        self.test.run_cmd([
            "sprint", "bottom", "-r", roadmap, str(sprint_id), str(task_ids[0])
        ])

        order = self._get_task_order(roadmap, sprint_id)
        assert order[-1] == task_ids[0], f"Bottom command failed: last position should be task {task_ids[0]}, got {order}"
        assert len(order) == 5, f"Task count should remain 5, got {len(order)}"

        # Move middle task to bottom
        self.test.run_cmd([
            "sprint", "bottom", "-r", roadmap, str(sprint_id), str(task_ids[2])
        ])

        order = self._get_task_order(roadmap, sprint_id)
        assert order[-1] == task_ids[2], f"Bottom command failed: last position should be task {task_ids[2]}, got {order}"

        # Move last task to bottom (should remain last)
        self.test.run_cmd([
            "sprint", "bottom", "-r", roadmap, str(sprint_id), str(task_ids[2])
        ])

        order = self._get_task_order(roadmap, sprint_id)
        assert order[-1] == task_ids[2], f"Bottom command on last task failed: should remain last, got {order}"

        print("Bottom moves to last position test passed")

    def test_order_persists_after_operations(self):
        """Test that task order persists after multiple operations."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Persistence Testing")
        task_ids = self._create_test_tasks(roadmap, 4)

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Perform multiple operations
        self.test.run_cmd(["sprint", "top", "-r", roadmap, str(sprint_id), str(task_ids[3])])
        self.test.run_cmd(["sprint", "swap", "-r", roadmap, str(sprint_id), str(task_ids[0]), str(task_ids[1])])
        self.test.run_cmd(["sprint", "bottom", "-r", roadmap, str(sprint_id), str(task_ids[2])])

        # Get order
        order = self._get_task_order(roadmap, sprint_id)

        # Verify tasks are still in sprint
        assert len(order) == 4, f"Task count should be 4 after operations, got {len(order)}"
        assert set(order) == set(task_ids), f"Task set should match original tasks: expected {set(task_ids)}, got {set(order)}"

        # Verify sprint status is preserved
        result = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        assert result["status"] == "PENDING", f"Sprint status should remain PENDING, got {result.get('status')}"

        print("Order persists after operations test passed")

    def test_reorder_with_partial_list(self):
        """Test reorder requires all task IDs (partial lists are rejected)."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Partial Reorder Testing")
        task_ids = self._create_test_tasks(roadmap, 5)

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Try to reorder with subset (should fail - requires all tasks)
        partial_order = [task_ids[2], task_ids[1], task_ids[0]]
        exit_code, _, stderr = self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id),
            ",".join(map(str, partial_order))
        ], check=False)

        # Should fail because not all tasks are included
        assert exit_code != 0, f"Reorder with partial list should fail, got exit code {exit_code}"
        assert "expected 5 task ids" in stderr.lower() or "incomplete" in stderr.lower(), \
               f"Expected error about incomplete task list, got: {stderr}"

        print("Reorder with partial list test passed")

    def test_invalid_task_id_rejected(self):
        """Test that invalid task IDs are rejected."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Invalid Task Testing")
        task_ids = self._create_test_tasks(roadmap, 3)

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Get initial order for comparison
        initial_order = self._get_task_order(roadmap, sprint_id)

        # Try to reorder with non-existent task ID (999999)
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id),
            f"{task_ids[0]},999999,{task_ids[2]}"
        ], check=False)

        # Should fail with non-zero exit code
        assert exit_code != 0, f"Reorder with invalid task ID should fail, got exit code {exit_code}"

        # Verify order is unchanged
        current_order = self._get_task_order(roadmap, sprint_id)
        assert current_order == initial_order, f"Order should remain unchanged after failed reorder"

        # Try move-to with invalid task ID
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id),
            "999999", "0"
        ], check=False)

        assert exit_code != 0, f"Move-to with invalid task ID should fail, got exit code {exit_code}"

        # Try swap with invalid task ID
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "swap", "-r", roadmap, str(sprint_id),
            str(task_ids[0]), "999999"
        ], check=False)

        assert exit_code != 0, f"Swap with invalid task ID should fail, got exit code {exit_code}"

        print("Invalid task ID rejected test passed")

    def test_task_not_in_sprint_rejected(self):
        """Test that tasks not in the sprint are rejected."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Not-In-Sprint Testing")

        # Create tasks
        in_sprint_tasks = self._create_test_tasks(roadmap, 2)
        not_in_sprint_task = self.test.create_task(
            roadmap,
            "Task Not In Sprint",
            "This task is not in the sprint",
            "Technical details",
            "Acceptance criteria"
        )

        # Add only some tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, in_sprint_tasks))
        ])

        # Get initial order
        initial_order = self._get_task_order(roadmap, sprint_id)

        # Try to reorder with task not in sprint
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id),
            f"{in_sprint_tasks[0]},{not_in_sprint_task},{in_sprint_tasks[1]}"
        ], check=False)

        assert exit_code != 0, f"Reorder with task not in sprint should fail, got exit code {exit_code}"

        # Verify order is unchanged
        current_order = self._get_task_order(roadmap, sprint_id)
        assert current_order == initial_order, f"Order should remain unchanged after failed reorder"

        # Try move-to with task not in sprint
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id),
            str(not_in_sprint_task), "0"
        ], check=False)

        assert exit_code != 0, f"Move-to with task not in sprint should fail, got exit code {exit_code}"

        # Try swap with task not in sprint
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "swap", "-r", roadmap, str(sprint_id),
            str(in_sprint_tasks[0]), str(not_in_sprint_task)
        ], check=False)

        assert exit_code != 0, f"Swap with task not in sprint should fail, got exit code {exit_code}"

        # Try top with task not in sprint
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "top", "-r", roadmap, str(sprint_id), str(not_in_sprint_task)
        ], check=False)

        assert exit_code != 0, f"Top with task not in sprint should fail, got exit code {exit_code}"

        # Try bottom with task not in sprint
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "bottom", "-r", roadmap, str(sprint_id), str(not_in_sprint_task)
        ], check=False)

        assert exit_code != 0, f"Bottom with task not in sprint should fail, got exit code {exit_code}"

        print("Task not in sprint rejected test passed")

    def test_move_to_invalid_position(self):
        """Test that move-to with invalid position is rejected."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Invalid Position Testing")
        task_ids = self._create_test_tasks(roadmap, 3)

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Get initial order
        initial_order = self._get_task_order(roadmap, sprint_id)

        # Genuinely invalid positions (negative, non-numeric) must be REJECTED
        # and leave the order unchanged.
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id),
            str(task_ids[0]), "-1"
        ], check=False)
        assert exit_code != 0, f"Move-to with negative position should fail, got exit code {exit_code}"

        exit_code, _, _ = self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id),
            str(task_ids[0]), "abc"
        ], check=False)
        assert exit_code != 0, f"Move-to with non-numeric position should fail, got exit code {exit_code}"

        # Order is unchanged after the two genuinely-failed operations.
        current_order = self._get_task_order(roadmap, sprint_id)
        assert current_order == initial_order, "Order should remain unchanged after failed operations"

        # Move-to with position beyond task count CLAMPS to the end (finding #47):
        # per SPEC/COMMANDS.md "If position >= task count, task is moved to the
        # end" (consistent with `sprint bottom`). It must succeed (exit 0), not
        # fail, and the task must land in the last position.
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id),
            str(task_ids[0]), "10"
        ], check=False)
        assert exit_code == 0, f"Move-to beyond count must clamp to end (exit 0), got {exit_code}"
        clamped_order = self._get_task_order(roadmap, sprint_id)
        assert clamped_order[-1] == task_ids[0], (
            f"task moved beyond count must be last; order={clamped_order}, task={task_ids[0]}"
        )

        print("Move to invalid position rejected / out-of-range clamped test passed")

    def test_reorder_single_task(self):
        """Test reorder with single task (should be no-op)."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Single Task Reorder Testing")
        task_ids = self._create_test_tasks(roadmap, 1)

        # Add single task to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_ids[0])
        ])

        # Reorder with single task
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id), str(task_ids[0])
        ], check=False)

        # Should succeed (no-op)
        assert exit_code == 0, f"Reorder with single task should succeed, got exit code {exit_code}"

        # Verify task is still there
        order = self._get_task_order(roadmap, sprint_id)
        assert order == task_ids, f"Single task should remain, got {order}"

        print("Reorder single task test passed")

    def test_swap_same_task(self):
        """Test swapping a task with itself (should be no-op)."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Same Task Swap Testing")
        task_ids = self._create_test_tasks(roadmap, 3)

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Get initial order
        initial_order = self._get_task_order(roadmap, sprint_id)

        # Try to swap task with itself
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "swap", "-r", roadmap, str(sprint_id),
            str(task_ids[0]), str(task_ids[0])
        ], check=False)

        # This should either succeed as no-op or fail gracefully
        # Order should remain unchanged regardless
        current_order = self._get_task_order(roadmap, sprint_id)
        assert current_order == initial_order, f"Order should remain unchanged when swapping same task"

        print("Swap same task test passed")

    def test_order_with_task_status_transitions(self):
        """Test that order is maintained through task status transitions."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Status Transition Ordering Testing")
        task_ids = self._create_test_tasks(roadmap, 4)

        # Add tasks to sprint in specific order
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Reorder to custom sequence
        custom_order = [task_ids[2], task_ids[0], task_ids[3], task_ids[1]]
        self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id),
            ",".join(map(str, custom_order))
        ])

        # Verify initial custom order
        order = self._get_task_order(roadmap, sprint_id)
        assert order == custom_order, f"Custom order should be set, got {order}"

        # Transition first task through status changes
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[2]), "DOING", "--commit-open", "391cff7"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[2]), "TESTING"])

        # Verify order is maintained
        order = self._get_task_order(roadmap, sprint_id)
        assert order[0] == task_ids[2], f"Task order should persist after status change, got {order}"

        # Complete the task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[2]), "COMPLETED", "--commit-close", "8a82583"])

        # Verify task is still in sprint with correct order
        order = self._get_task_order(roadmap, sprint_id)
        assert task_ids[2] in order, f"Completed task should still be in sprint"

        print("Order with task status transitions test passed")

    def test_multiple_sprints_independent_ordering(self):
        """Test that task ordering is independent per sprint."""
        roadmap = self.test.create_roadmap()

        # Create two sprints
        sprint1_id = self.test.create_sprint(roadmap, "First Sprint for Independent Ordering")
        sprint2_id = self.test.create_sprint(roadmap, "Second Sprint for Independent Ordering")

        # Create separate tasks for each sprint
        sprint1_tasks = self._create_test_tasks(roadmap, 3)
        sprint2_tasks = self._create_test_tasks(roadmap, 3)

        # Add tasks to respective sprints
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint1_id),
            ",".join(map(str, sprint1_tasks))
        ])
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint2_id),
            ",".join(map(str, sprint2_tasks))
        ])

        # Reorder sprint1
        sprint1_order = [sprint1_tasks[2], sprint1_tasks[0], sprint1_tasks[1]]
        self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint1_id),
            ",".join(map(str, sprint1_order))
        ])

        # Verify sprint1 order
        order1 = self._get_task_order(roadmap, sprint1_id)
        assert order1 == sprint1_order, f"Sprint 1 order mismatch: expected {sprint1_order}, got {order1}"

        # Verify sprint2 order is unchanged
        order2 = self._get_task_order(roadmap, sprint2_id)
        assert order2 == sprint2_tasks, f"Sprint 2 order should be unchanged: expected {sprint2_tasks}, got {order2}"

        # Reorder sprint2 differently
        sprint2_order = [sprint2_tasks[1], sprint2_tasks[2], sprint2_tasks[0]]
        self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint2_id),
            ",".join(map(str, sprint2_order))
        ])

        # Verify both orders are independent
        order1 = self._get_task_order(roadmap, sprint1_id)
        order2 = self._get_task_order(roadmap, sprint2_id)
        assert order1 == sprint1_order, f"Sprint 1 order should remain: expected {sprint1_order}, got {order1}"
        assert order2 == sprint2_order, f"Sprint 2 order should be updated: expected {sprint2_order}, got {order2}"

        print("Multiple sprints independent ordering test passed")

    def test_combined_ordering_operations(self):
        """Test combined sequence of all ordering operations."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint for Combined Operations Testing")
        task_ids = self._create_test_tasks(roadmap, 6)

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Initial: [0, 1, 2, 3, 4, 5]

        # Operation 1: Move task 5 to position 2
        # Result: [0, 1, 5, 2, 3, 4]
        self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id), str(task_ids[5]), "2"
        ])

        # Operation 2: Swap positions 0 and 4 (tasks 0 and 3)
        # Current: [0, 1, 5, 2, 3, 4]
        # After swap: [3, 1, 5, 2, 0, 4]
        self.test.run_cmd([
            "sprint", "swap", "-r", roadmap, str(sprint_id),
            str(task_ids[0]), str(task_ids[3])
        ])

        # Operation 3: Move task 1 to top
        # Current: [3, 1, 5, 2, 0, 4]
        # After top: [1, 3, 5, 2, 0, 4]
        self.test.run_cmd([
            "sprint", "top", "-r", roadmap, str(sprint_id), str(task_ids[1])
        ])

        # Operation 4: Move task 2 to bottom
        # Current: [1, 3, 5, 2, 0, 4]
        # After bottom: [1, 3, 5, 0, 4, 2]
        self.test.run_cmd([
            "sprint", "bottom", "-r", roadmap, str(sprint_id), str(task_ids[2])
        ])

        # Verify final order
        final_order = self._get_task_order(roadmap, sprint_id)
        expected_order = [task_ids[1], task_ids[3], task_ids[5], task_ids[0], task_ids[4], task_ids[2]]

        assert final_order == expected_order, f"Combined operations failed: expected {expected_order}, got {final_order}"

        # Operation 5: Full reorder
        reorder_sequence = [
            task_ids[4], task_ids[2], task_ids[0],
            task_ids[5], task_ids[3], task_ids[1]
        ]
        self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id),
            ",".join(map(str, reorder_sequence))
        ])

        final_order = self._get_task_order(roadmap, sprint_id)
        assert final_order == reorder_sequence, f"Final reorder failed: expected {reorder_sequence}, got {final_order}"

        print("Combined ordering operations test passed")

    # ================= POSITION UNIQUENESS (rmp task #236) =================
    #
    # sprint_tasks.position is unique within one sprint and the SCHEMA is what
    # holds it that way (SPEC/DATABASE.md § Position Uniqueness Within a Sprint,
    # SPEC/VERSION.md § Migration 1.12.0 → 1.13.0). The tests above exercise the
    # ordering commands; these prove the invariant behind them -- that the
    # constraint exists, that it is enforced against a hand-written insert the
    # CLI can never produce, that a database predating it is repaired rather than
    # refused or truncated, and that the commands a plain unique index breaks
    # still work.

    def _db_path(self, roadmap: str):
        """The SQLite file the CLI created for this roadmap."""
        return self.test.roadmaps_dir / roadmap / "project.db"

    def _query(self, roadmap: str, sql: str, params=()):
        """Read the roadmap database directly, outside the CLI.

        Direct reads are what make these assertions about STORED STATE rather
        than about what a command chose to print, and direct WRITES are the only
        way to produce a colliding position at all: no shipped write path can.
        """
        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            return con.execute(sql, params).fetchall()
        finally:
            con.close()

    def _positions(self, roadmap: str, sprint_id: int) -> dict:
        """task id -> stored position, for one sprint."""
        rows = self._query(
            roadmap,
            "SELECT task_id, position FROM sprint_tasks WHERE sprint_id = ?",
            (sprint_id,),
        )
        return {task_id: position for task_id, position in rows}

    def _colliding_groups(self, roadmap: str) -> int:
        """The check SPEC/DATABASE.md states verbatim: how many (sprint_id,
        position) groups hold more than one row."""
        return self._query(roadmap, """
            SELECT COUNT(*) FROM (
                SELECT sprint_id, position
                FROM sprint_tasks
                GROUP BY sprint_id, position
                HAVING COUNT(*) > 1
            )""")[0][0]

    def _schema_version(self, roadmap: str) -> str:
        return self._query(
            roadmap, "SELECT value FROM _metadata WHERE key = 'schema_version'")[0][0]

    def _order_indexes(self, roadmap: str) -> dict:
        """Every index on sprint_tasks whose columns are exactly
        (sprint_id, position), mapped to whether it is UNIQUE.

        Read through PRAGMA rather than by matching the SQL text, so a UNIQUE
        spelled any other way is still detected and a second index over the same
        pair cannot hide.
        """
        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            found = {}
            for row in con.execute("PRAGMA index_list('sprint_tasks')").fetchall():
                name, unique = row[1], row[2]
                columns = [r[2] for r in
                           con.execute(f"PRAGMA index_info('{name}')").fetchall()]
                if columns == ["sprint_id", "position"]:
                    found[name] = bool(unique)
            return found
        finally:
            con.close()

    def _seed_sprint(self, roadmap: str, title: str, count: int):
        """Create a sprint with `count` tasks added through the CLI."""
        sprint_id = self.test.create_sprint(roadmap, title)
        task_ids = self._create_test_tasks(roadmap, count)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])
        return sprint_id, task_ids

    def test_position_uniqueness_is_enforced_by_the_schema(self):
        """A colliding position is refused by the database itself.

        The CLI cannot be asked for one -- every ordering command produces a
        permutation or an append past MAX(position) -- so the collision is
        written by hand, which is precisely the write path the constraint exists
        to catch. Three directions are checked: an UPDATE onto a taken position,
        an INSERT at one, and the same position in a DIFFERENT sprint, which must
        be ACCEPTED because the constraint is over the pair and not over position
        alone.
        """
        roadmap = self.test.create_roadmap()
        sprint_id, task_ids = self._seed_sprint(roadmap, "Settlement reconciliation", 3)
        other_sprint, other_tasks = self._seed_sprint(roadmap, "Acquirer failover", 2)

        indexes = self._order_indexes(roadmap)
        assert indexes == {"idx_sprint_tasks_order": True}, (
            f"indexes over (sprint_id, position) = {indexes}; expected exactly one, "
            f"idx_sprint_tasks_order, and it must be UNIQUE"
        )

        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            try:
                con.execute(
                    "UPDATE sprint_tasks SET position = 0 WHERE sprint_id = ? AND task_id = ?",
                    (sprint_id, task_ids[1]),
                )
                con.commit()
                raise AssertionError(
                    "UPDATE moving a task onto position 0, already held by another task of the "
                    "same sprint, SUCCEEDED; the unique index must reject it"
                )
            except sqlite3.IntegrityError as exc:
                assert "UNIQUE" in str(exc).upper(), f"unexpected rejection: {exc}"
                con.rollback()

            try:
                con.execute(
                    "INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position) "
                    "VALUES (?, ?, ?, ?)",
                    (sprint_id, other_tasks[0], "2026-08-01T09:00:00.000Z", 0),
                )
                con.commit()
                raise AssertionError(
                    "INSERT of a membership at a position the sprint already uses SUCCEEDED; "
                    "the unique index must reject it"
                )
            except sqlite3.IntegrityError as exc:
                assert "UNIQUE" in str(exc).upper(), f"unexpected rejection: {exc}"
                con.rollback()
        finally:
            con.close()

        # Two different sprints legitimately both hold position 0.
        assert self._positions(roadmap, sprint_id)[task_ids[0]] == 0
        assert self._positions(roadmap, other_sprint)[other_tasks[0]] == 0, (
            "two different sprints must both be able to use position 0: the constraint is "
            "over (sprint_id, position), not over position alone"
        )
        assert self._colliding_groups(roadmap) == 0

        print("Position uniqueness enforced by the schema test passed")

    def _downgrade_to_1_12_0(self, roadmap: str, seeds):
        """Take a CLI-created roadmap back to schema 1.12.0 and overwrite its
        sprint_tasks positions with `seeds` ((sprint_id, task_id, position)).

        Only the ordering index changed between 1.12.0 and 1.13.0, so restoring
        its non-unique form is enough to produce a faithful 1.12.0 database while
        every other table stays correct by construction. Until that index is
        downgraded the colliding seeds cannot be written at all, which is itself
        the point of the constraint.
        """
        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            con.execute("DROP INDEX IF EXISTS idx_sprint_tasks_order")
            con.execute(SPRINT_TASKS_ORDER_INDEX_1_12_0)
            for sprint_id, task_id, position in seeds:
                con.execute(
                    "UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?",
                    (position, sprint_id, task_id),
                )
            con.execute("UPDATE _metadata SET value = '1.12.0' WHERE key = 'schema_version'")
            con.commit()
        finally:
            con.close()

    def test_migration_repairs_a_database_that_violates_the_constraint(self):
        """A 1.12.0 database whose positions collide is REPAIRED, not refused
        and not truncated, on the next command that opens it.

        The fixture is the hard case: two colliding pairs in one sprint, which is
        the input on which a repair that reads its own writes was measured to
        trade one collision for another, plus a second sprint that is distinct
        but gappy. A duplicate position is an ambiguous order and not a redundant
        membership, so every row must survive (SPEC/VERSION.md § Migration
        1.12.0 → 1.13.0).
        """
        roadmap = self.test.create_roadmap()
        sprint_id, task_ids = self._seed_sprint(roadmap, "Settlement reconciliation", 4)
        gappy_sprint, gappy_tasks = self._seed_sprint(roadmap, "Ledger migration", 3)

        # Sprint 1: tasks 0,1 both at position 0; tasks 2,3 both at position 1.
        # Sprint 2: distinct, but 0/3/7 rather than 0/1/2.
        self._downgrade_to_1_12_0(roadmap, [
            (sprint_id, task_ids[0], 0),
            (sprint_id, task_ids[1], 0),
            (sprint_id, task_ids[2], 1),
            (sprint_id, task_ids[3], 1),
            (gappy_sprint, gappy_tasks[0], 0),
            (gappy_sprint, gappy_tasks[1], 3),
            (gappy_sprint, gappy_tasks[2], 7),
        ])

        before_rows = self._query(roadmap, "SELECT COUNT(*) FROM sprint_tasks")[0][0]
        before_pairs = set(self._query(roadmap, "SELECT sprint_id, task_id FROM sprint_tasks"))
        assert self._colliding_groups(roadmap) == 2, (
            "precondition: the fixture must actually violate the constraint"
        )
        assert self._schema_version(roadmap) == "1.12.0"

        # Any command opens the database, and opening it is what runs the
        # migration. A read is used deliberately: the repair must not need a
        # write to be triggered.
        exit_code, stdout, stderr = self.test.run_cmd(
            ["sprint", "list", "-r", roadmap], check=False)
        assert exit_code == 0, f"migration failed: exit {exit_code}, stderr={stderr}"

        fresh = self.test.create_roadmap()
        assert self._schema_version(roadmap) == self._schema_version(fresh), (
            "a migrated database must land on the same schema_version a fresh one is created at"
        )

        assert self._query(roadmap, "SELECT COUNT(*) FROM sprint_tasks")[0][0] == before_rows, (
            "the migration discarded a sprint_tasks row; a duplicate position is an ambiguous "
            "order, not a redundant membership, so both tasks must survive"
        )
        assert set(self._query(roadmap, "SELECT sprint_id, task_id FROM sprint_tasks")) == before_pairs
        assert self._colliding_groups(roadmap) == 0, "positions still collide after the migration"

        repaired = self._positions(roadmap, sprint_id)
        assert sorted(repaired.values()) == [0, 1, 2, 3], (
            f"sprint positions after the migration = {repaired}; a dense 0..N-1 run is required"
        )
        # The lower task id takes the lower position WITHIN a colliding pair, and
        # the pair that shared position 0 stays ahead of the pair that shared 1.
        assert [repaired[t] for t in task_ids] == [0, 1, 2, 3], (
            f"repaired order = {repaired}; the repair ranks by position first and breaks ties by "
            f"task_id, so {task_ids} must come out in that order"
        )

        gappy = self._positions(roadmap, gappy_sprint)
        assert [gappy[t] for t in gappy_tasks] == [0, 1, 2], (
            f"gappy sprint after the migration = {gappy}; a sprint whose positions were already "
            f"distinct must keep its relative order and lose its gaps"
        )

        assert self._order_indexes(roadmap) == {"idx_sprint_tasks_order": True}

        print("Migration repairs a violating database test passed")

    def test_migration_leaves_a_conforming_database_untouched(self):
        """The second data state: a 1.12.0 database that ALREADY satisfies the
        constraint and is already dense comes out of the repair holding exactly
        the values it went in with.

        The check is value by value rather than a row count, because a repair
        ranking by added_at would also produce a valid dense run while silently
        replacing the planned order with the order the tasks were added in. The
        fixture's stored order is deliberately the REVERSE of its insertion
        order, and `sprint add-tasks` stamps one added_at on the whole batch, so
        an added_at-ranked repair falls through to its task_id tie-breaker and
        restores exactly the insertion order this test reversed. Either way it
        fails here.
        """
        roadmap = self.test.create_roadmap()
        sprint_id, task_ids = self._seed_sprint(roadmap, "Chargeback automation", 4)

        # Reverse the order through the CLI, so position order and added_at order
        # disagree, then take the database back to 1.12.0 without touching the
        # positions themselves.
        reversed_ids = list(reversed(task_ids))
        self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id),
            ",".join(map(str, reversed_ids))
        ])
        before = self._positions(roadmap, sprint_id)
        assert [before[t] for t in reversed_ids] == [0, 1, 2, 3], (
            "precondition: the fixture must be dense and in reverse insertion order"
        )

        self._downgrade_to_1_12_0(roadmap, [])
        assert self._schema_version(roadmap) == "1.12.0"

        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "list", "-r", roadmap], check=False)
        assert exit_code == 0, f"migration failed: exit {exit_code}, stderr={stderr}"

        after = self._positions(roadmap, sprint_id)
        assert after == before, (
            f"positions changed from {before} to {after}; the repair must be a no-op on a database "
            f"that already satisfies the constraint and is already dense. Ranking by added_at "
            f"instead of position would restore the insertion order here"
        )
        assert self._order_indexes(roadmap) == {"idx_sprint_tasks_order": True}

        # Idempotency at the command level: opening it again changes nothing.
        self.test.run_cmd(["sprint", "list", "-r", roadmap])
        assert self._positions(roadmap, sprint_id) == before

        print("Migration leaves conforming data untouched test passed")

    def test_ordering_commands_survive_the_unique_constraint(self):
        """The five commands a plain unique index breaks, driven end to end.

        Each of reorder, move-to, top, bottom and swap assigns positions
        SEQUENTIALLY over values another row of the same sprint still holds, so
        without a parking step the very first write fails with
        "UNIQUE constraint failed: sprint_tasks.sprint_id, sprint_tasks.position".
        The inputs are the worst cases: a FULL REVERSAL (every position occupied
        by the wrong task) and an ADJACENT swap (the two rows directly contend).
        """
        roadmap = self.test.create_roadmap()
        sprint_id, task_ids = self._seed_sprint(roadmap, "Ordering under constraint", 6)

        reversed_ids = list(reversed(task_ids))
        exit_code, _, stderr = self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id),
            ",".join(map(str, reversed_ids))
        ], check=False)
        assert exit_code == 0, f"full reversal reorder failed: exit {exit_code}, stderr={stderr}"
        assert self._get_task_order(roadmap, sprint_id) == reversed_ids
        assert self._colliding_groups(roadmap) == 0

        # Adjacent swap: the two rows contend for each other's position.
        current = self._get_task_order(roadmap, sprint_id)
        exit_code, _, stderr = self.test.run_cmd([
            "sprint", "swap", "-r", roadmap, str(sprint_id),
            str(current[2]), str(current[3])
        ], check=False)
        assert exit_code == 0, f"adjacent swap failed: exit {exit_code}, stderr={stderr}"
        expected = current[:2] + [current[3], current[2]] + current[4:]
        assert self._get_task_order(roadmap, sprint_id) == expected, (
            f"order after the adjacent swap = {self._get_task_order(roadmap, sprint_id)}, "
            f"want {expected}"
        )
        assert self._colliding_groups(roadmap) == 0

        # move-to in both directions: the shift form collides going up AND down.
        current = self._get_task_order(roadmap, sprint_id)
        moved = current[5]
        exit_code, _, stderr = self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id), str(moved), "1"
        ], check=False)
        assert exit_code == 0, f"move-to (upwards) failed: exit {exit_code}, stderr={stderr}"
        expected = [current[0], moved] + current[1:5]
        assert self._get_task_order(roadmap, sprint_id) == expected

        exit_code, _, stderr = self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id), str(moved), "4"
        ], check=False)
        assert exit_code == 0, f"move-to (downwards) failed: exit {exit_code}, stderr={stderr}"
        after_down = self._get_task_order(roadmap, sprint_id)
        assert after_down.index(moved) == 4, f"move-to 4 left {moved} at {after_down.index(moved)}"
        assert self._colliding_groups(roadmap) == 0

        # top and bottom reuse move-to in full, parking step included.
        exit_code, _, stderr = self.test.run_cmd([
            "sprint", "top", "-r", roadmap, str(sprint_id), str(task_ids[0])
        ], check=False)
        assert exit_code == 0, f"top failed: exit {exit_code}, stderr={stderr}"
        assert self._get_task_order(roadmap, sprint_id)[0] == task_ids[0]

        exit_code, _, stderr = self.test.run_cmd([
            "sprint", "bottom", "-r", roadmap, str(sprint_id), str(task_ids[0])
        ], check=False)
        assert exit_code == 0, f"bottom failed: exit {exit_code}, stderr={stderr}"
        assert self._get_task_order(roadmap, sprint_id)[-1] == task_ids[0]
        assert self._colliding_groups(roadmap) == 0

        # Every command left the sprint holding a permutation of 0..N-1.
        positions = self._positions(roadmap, sprint_id)
        assert sorted(positions.values()) == list(range(len(task_ids))), (
            f"positions after the ordering commands = {positions}; each command must leave the "
            f"sprint holding a permutation of its positions"
        )

        print("Ordering commands survive the unique constraint test passed")

    def test_task_next_order_is_total_and_repeatable(self):
        """`task next` publishes the planned order as a guarantee.

        Position is unique within a sprint, so ordering on it alone already
        places every task at exactly one rank: two calls over unchanged data
        return the same tasks in the same sequence. Priority does NOT order this
        listing and cannot promote a task above another, so the fixture puts the
        HIGHEST priority task LAST in the planned order and requires it to stay
        there (SPEC/COMMANDS.md § Get Next Tasks (next)).
        """
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.create_sprint(roadmap, "Next-task ordering")

        # Ascending priority, so the last task planned is the most urgent one.
        task_ids = []
        for i, priority in enumerate([1, 4, 7, 9], start=1):
            task_ids.append(self.test.create_task(
                roadmap,
                f"Reconcile settlement window {i}",
                "Each settlement window must reconcile before the ledger closes.",
                "Replay the window against the acquirer file and report divergences.",
                "A corrupted window is reported rather than silently accepted.",
                priority=priority,
            ))
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        first = [t["id"] for t in self.test.run_cmd_json(["task", "next", "-r", roadmap, "10"])]
        assert first == task_ids, (
            f"task next returned {first}, want the planned order {task_ids}: priority must not "
            f"promote the priority-9 task above the priority-1 one"
        )

        second = [t["id"] for t in self.test.run_cmd_json(["task", "next", "-r", roadmap, "10"])]
        assert second == first, (
            f"two calls over unchanged data returned {first} then {second}; the order is total, "
            f"so it must be repeatable"
        )

        # And it follows the plan when the plan changes.
        replanned = [task_ids[3], task_ids[1], task_ids[0], task_ids[2]]
        self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint_id),
            ",".join(map(str, replanned))
        ])
        after = [t["id"] for t in self.test.run_cmd_json(["task", "next", "-r", roadmap, "10"])]
        assert after == replanned, f"task next returned {after}, want the new plan {replanned}"

        print("task next order is total and repeatable test passed")

    # ------------------------------------------------------------------
    # Audit of the ordering commands (rmp task #320)
    # ------------------------------------------------------------------

    def _move_position_entries(self, roadmap: str) -> list:
        """Return every SPRINT_TASK_MOVE_POSITION entry of a roadmap."""
        return self.test.run_cmd_json([
            "audit", "list", "-r", roadmap,
            "-o", "SPRINT_TASK_MOVE_POSITION", "-l", "500"
        ])

    def test_every_ordering_invocation_writes_one_audit_entry(self):
        """Each move-to/top/bottom invocation writes exactly one entry, no-op included.

        SPEC/COMMANDS.md, "Audit of the ordering commands", states the rule
        without qualification: the ordering commands "write one entry per
        invocation, against the sprint, with NULL related_entity_id and NULL
        commit_hash", and "A no-op move (moving a task to the position it
        already holds) still writes its entry, on the same rule that governs
        `task edit`: the audit log records the command issued, not the delta it
        produced."

        The measured defect was that a no-op move-to, top or bottom exited 0 and
        printed a success payload while writing nothing at all, so a caller was
        told the command had succeeded and the log held no trace of it having
        been issued.

        The sequence below is the reported reproduction, extended with the two
        remaining no-op forms, and every step asserts the running total rather
        than only the end state, so an implementation that skipped one form and
        double-wrote another cannot pass on the final count.
        """
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.create_sprint(
            roadmap, "Reconcile every acquirer batch before the ledger closes"
        )
        task_ids = self._create_test_tasks(roadmap, 3)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        moved = task_ids[0]
        assert self._get_task_order(roadmap, sprint_id) == task_ids, (
            "the fixture sprint does not hold the three tasks in creation order"
        )
        assert len(self._move_position_entries(roadmap)) == 0, (
            "the fixture already holds SPRINT_TASK_MOVE_POSITION entries, so this "
            "test cannot attribute the entries it counts to its own invocations"
        )

        # (invocation, expected running total, expected order afterwards).
        steps = [
            (["sprint", "move-to", "-r", roadmap, str(sprint_id), str(moved), "0"],
             1, [task_ids[0], task_ids[1], task_ids[2]]),
            (["sprint", "top", "-r", roadmap, str(sprint_id), str(moved)],
             2, [task_ids[0], task_ids[1], task_ids[2]]),
            (["sprint", "move-to", "-r", roadmap, str(sprint_id), str(moved), "1"],
             3, [task_ids[1], task_ids[0], task_ids[2]]),
            (["sprint", "move-to", "-r", roadmap, str(sprint_id), str(moved), "1"],
             4, [task_ids[1], task_ids[0], task_ids[2]]),
            (["sprint", "bottom", "-r", roadmap, str(sprint_id), str(moved)],
             5, [task_ids[1], task_ids[2], task_ids[0]]),
            (["sprint", "bottom", "-r", roadmap, str(sprint_id), str(moved)],
             6, [task_ids[1], task_ids[2], task_ids[0]]),
        ]

        for args, want_total, want_order in steps:
            invocation = " ".join(args[:2] + args[4:])
            exit_code, stdout, _ = self.test.run_cmd(args)
            assert exit_code == 0, f"`{invocation}` exited {exit_code}"
            assert '"success"' in stdout, (
                f"`{invocation}` printed no success payload: {stdout!r}"
            )

            entries = self._move_position_entries(roadmap)
            assert len(entries) == want_total, (
                f"after `{invocation}` the log holds {len(entries)} "
                f"SPRINT_TASK_MOVE_POSITION entries, want {want_total}"
            )

            order = self._get_task_order(roadmap, sprint_id)
            assert order == want_order, (
                f"`{invocation}` left the sprint holding {order}, want {want_order}"
            )

        # Every entry carries the published shape, read back field by field
        # rather than counted: against the sprint, with both nullable columns
        # NULL and no entry recorded against any task.
        for entry in self._move_position_entries(roadmap):
            assert entry["entity_type"] == "SPRINT", (
                f"entry {entry['id']} is recorded against {entry['entity_type']}, want SPRINT"
            )
            assert entry["entity_id"] == sprint_id, (
                f"entry {entry['id']} names entity {entry['entity_id']}, want sprint {sprint_id}"
            )
            assert entry["related_entity_id"] is None, (
                f"entry {entry['id']} carries related_entity_id "
                f"{entry['related_entity_id']}, want null"
            )
            assert entry["commit_hash"] is None, (
                f"entry {entry['id']} carries commit_hash {entry['commit_hash']}, want null"
            )

        print("every ordering invocation writes one audit entry test passed")



def main():
    """Run all tests."""
    test = TestSprintTaskOrdering()

    methods = [m for m in dir(test) if m.startswith("test_")]
    passed = 0
    failed = 0

    for method_name in methods:
        test.setup_method()
        try:
            getattr(test, method_name)()
            passed += 1
        except Exception as e:
            print(f"✗ {method_name} failed: {e}")
            failed += 1
        finally:
            test.teardown_method()

    print(f"\n{passed} passed, {failed} failed")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if main() else 1)
