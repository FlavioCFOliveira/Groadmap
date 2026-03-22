#!/usr/bin/env python3
"""
Test 13: Sprint Task Ordering
Tests all sprint task ordering commands: reorder, move-to, swap, top, bottom.
Validates actual task positions after each operation.
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


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

        # Try move-to with negative position
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id),
            str(task_ids[0]), "-1"
        ], check=False)

        assert exit_code != 0, f"Move-to with negative position should fail, got exit code {exit_code}"

        # Try move-to with position beyond task count
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id),
            str(task_ids[0]), "10"
        ], check=False)

        assert exit_code != 0, f"Move-to with position beyond count should fail, got exit code {exit_code}"

        # Try move-to with non-numeric position
        exit_code, _, _ = self.test.run_cmd([
            "sprint", "move-to", "-r", roadmap, str(sprint_id),
            str(task_ids[0]), "abc"
        ], check=False)

        assert exit_code != 0, f"Move-to with non-numeric position should fail, got exit code {exit_code}"

        # Verify order is unchanged
        current_order = self._get_task_order(roadmap, sprint_id)
        assert current_order == initial_order, f"Order should remain unchanged after failed operations"

        print("Move to invalid position rejected test passed")

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
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[2]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[2]), "TESTING"])

        # Verify order is maintained
        order = self._get_task_order(roadmap, sprint_id)
        assert order[0] == task_ids[2], f"Task order should persist after status change, got {order}"

        # Complete the task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[2]), "COMPLETED"])

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
