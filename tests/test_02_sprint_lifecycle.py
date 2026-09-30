#!/usr/bin/env python3
"""
Test 02: Sprint Lifecycle
Tests sprint creation, start, close, reopen workflows.
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestSprintLifecycle:
    """Test sprint lifecycle operations."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_create_sprint(self):
        """Test creating sprints."""
        roadmap = self.test.create_roadmap()

        # Create sprint
        sprint_id = self.test.create_sprint(
            roadmap=roadmap,
            description="Sprint 1: Initial Development"
        )
        assert sprint_id > 0

        # Verify sprint was created with PENDING status
        self.test.assert_sprint_status(roadmap, sprint_id, "PENDING")

        # Get sprint details and validate the full JSON shape against MODELS.md.
        result = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        self.test.assert_sprint_shape(result)
        assert result["description"] == "Sprint 1: Initial Development"
        assert result["task_count"] == 0

        print("✓ Create sprint test passed")

    def test_sprint_lifecycle_transitions(self):
        """Test sprint state transitions."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Test Sprint")

        # Start sprint
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])
        self.test.assert_sprint_status(roadmap, sprint_id, "OPEN")

        # Close sprint
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint_id)])
        self.test.assert_sprint_status(roadmap, sprint_id, "CLOSED")

        # Reopen sprint
        self.test.run_cmd(["sprint", "reopen", "-r", roadmap, str(sprint_id)])
        self.test.assert_sprint_status(roadmap, sprint_id, "OPEN")

        print("✓ Sprint lifecycle transitions test passed")

    def test_invalid_sprint_transitions(self):
        """Test invalid sprint state transitions."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Test Sprint")

        # Cannot close a PENDING sprint
        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "close", "-r", roadmap, str(sprint_id)],
            check=False
        )
        assert exit_code != 0, "Should not be able to close PENDING sprint"

        # Start the sprint
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Cannot start an already OPEN sprint
        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "start", "-r", roadmap, str(sprint_id)],
            check=False
        )
        assert exit_code != 0, "Should not be able to start OPEN sprint"

        print("✓ Invalid sprint transitions test passed")

    def test_update_sprint(self):
        """Test updating sprint description."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Original description")

        # Update description
        self.test.run_cmd([
            "sprint", "update", "-r", roadmap, str(sprint_id),
            "-d", "Updated description"
        ])

        result = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        assert result["description"] == "Updated description"

        print("✓ Update sprint test passed")

    def test_list_sprints_with_filter(self):
        """Test listing sprints with status filter."""
        roadmap = self.test.create_roadmap()

        # Create multiple sprints
        sprint1 = self.test.create_sprint(roadmap, "Sprint 1")
        sprint2 = self.test.create_sprint(roadmap, "Sprint 2")
        sprint3 = self.test.create_sprint(roadmap, "Sprint 3")

        # Start and close sprint 1
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint1)])
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint1)])

        # Start sprint 2
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint2)])

        # List all sprints
        result = self.test.list_sprints(roadmap)
        assert len(result) == 3

        # Filter by PENDING
        result = self.test.list_sprints(roadmap, status="PENDING")
        assert len(result) == 1
        assert result[0]["id"] == sprint3

        # Filter by OPEN
        result = self.test.list_sprints(roadmap, status="OPEN")
        assert len(result) == 1
        assert result[0]["id"] == sprint2

        # Filter by CLOSED
        result = self.test.list_sprints(roadmap, status="CLOSED")
        assert len(result) == 1
        assert result[0]["id"] == sprint1

        print("✓ List sprints with filter test passed")

    def test_remove_sprint(self):
        """Test removing a sprint."""
        roadmap = self.test.create_roadmap()

        sprint_id = self.test.create_sprint(roadmap, "Sprint to remove")

        # Verify sprint exists
        result = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        assert result["id"] == sprint_id

        # Remove sprint
        self.test.run_cmd(["sprint", "remove", "-r", roadmap, str(sprint_id)])

        # Verify sprint is gone
        self.test.assert_exit_code(
            ["sprint", "get", "-r", roadmap, str(sprint_id)],
            expected_code=4
        )

        print("✓ Remove sprint test passed")

    def test_remove_sprint_accepts_every_status(self):
        """Regression: sprint remove has NO status precondition.

        README.md claimed "The sprint must be CLOSED" for `sprint remove`. The
        binary enforces no such precondition -- PENDING, OPEN and CLOSED are all
        removable -- so the claim told a reader an OPEN sprint was protected when
        it was not. This pins the real contract so the documentation cannot drift
        back to it.
        """
        roadmap = self.test.create_roadmap()

        # PENDING: removable without ever being started.
        pending = self.test.create_sprint(
            roadmap, "Retire the deprecated token endpoint"
        )
        self.test.assert_sprint_status(roadmap, pending, "PENDING")
        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "remove", "-r", roadmap, str(pending)], check=False
        )
        assert exit_code == 0, f"remove of a PENDING sprint must exit 0, got {exit_code}"
        self.test.assert_exit_code(
            ["sprint", "get", "-r", roadmap, str(pending)], expected_code=4
        )

        # OPEN: removable without being closed first.
        opened = self.test.create_sprint(
            roadmap, "Migrate the session store onto the new schema"
        )
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(opened)])
        self.test.assert_sprint_status(roadmap, opened, "OPEN")
        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "remove", "-r", roadmap, str(opened)], check=False
        )
        assert exit_code == 0, f"remove of an OPEN sprint must exit 0, got {exit_code}"
        self.test.assert_exit_code(
            ["sprint", "get", "-r", roadmap, str(opened)], expected_code=4
        )

        # CLOSED: removable, as it always was.
        closed = self.test.create_sprint(
            roadmap, "Publish the rate-limit headers on every write route"
        )
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(closed)])
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(closed)])
        self.test.assert_sprint_status(roadmap, closed, "CLOSED")
        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "remove", "-r", roadmap, str(closed)], check=False
        )
        assert exit_code == 0, f"remove of a CLOSED sprint must exit 0, got {exit_code}"

        print("\u2713 sprint remove accepts PENDING, OPEN and CLOSED alike")

    def test_remove_sprint_refuses_a_completed_member(self):
        """A sprint holding a COMPLETED task is not removed.

        A completed task stays in the sprint it was completed in, so
        `sprint remove` refuses such a sprint with exit code 6, names every
        COMPLETED member, and changes nothing: the sprint, the membership and the
        task's closing commit all survive (SPEC/COMMANDS.md § Remove Sprint).
        Once the task is reopened, the sprint holds no COMPLETED task and is
        removed, the member returning to BACKLOG with commit_close cleared and
        commit_open preserved.
        """
        roadmap = self.test.create_roadmap()
        task = self.test.create_task(
            roadmap,
            "Verify the removal path keeps a completed member in its sprint",
            "A removed sprint must not strip a completed task of the sprint it was completed in.",
            "Drive the task to COMPLETED, then try to remove the sprint that holds it.",
            "The removal is refused and the task is still COMPLETED in its sprint.",
        )
        sprint = self.test.create_sprint(
            roadmap, "Confirm what sprint remove does to a completed member"
        )
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint), str(task)])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint)])
        self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task), "DOING", "--commit-open", "5f93b51"]
        )
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task), "TESTING"])
        self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task), "COMPLETED", "--commit-close", "2578d18"]
        )
        self.test.assert_task_status(roadmap, task, "COMPLETED")

        code, out, err = self.test.run_cmd(["sprint", "remove", "-r", roadmap, str(sprint)], check=False)
        assert code == 6, f"sprint remove with a COMPLETED member must exit 6, got {code}: {err!r}"
        want = (f"Error: validation error: cannot remove sprint #{sprint}: completed tasks stay in "
                f"their sprint: #{task}")
        assert err.splitlines()[0] == want, f"got {err.splitlines()[0]!r}, want {want!r}"
        assert out == "", f"a refused removal wrote to stdout: {out!r}"

        kept = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task)])[0]
        assert kept["status"] == "COMPLETED" and kept.get("commit_close") == "2578d18", (
            f"the refused removal changed the task: {kept}"
        )
        members = [t["id"] for t in self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint)])]
        assert members == [task], f"the refused removal changed the sprint's members: {members}"

        # Reopened, the task is SPRINT again, and the sprint can be removed.
        self.test.run_cmd(["task", "reopen", "-r", roadmap, str(task)])
        self.test.run_cmd(["sprint", "remove", "-r", roadmap, str(sprint)])
        after = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task)])[0]
        assert after["status"] == "BACKLOG", f"a removed sprint's member must be BACKLOG, got {after['status']}"
        assert after.get("commit_close") in (None, ""), f"commit_close must be cleared, got {after.get('commit_close')}"
        assert after.get("commit_open") == "5f93b51", f"commit_open must survive, got {after.get('commit_open')}"

        print("\u2713 sprint remove refuses a sprint holding a COMPLETED member and changes nothing")

    def test_sprint_with_tasks_lifecycle(self):
        """Test sprint lifecycle with tasks."""
        roadmap = self.test.create_roadmap()

        # Create tasks
        task1 = self.test.create_task(
            roadmap, "Task 1", "Functional 1", "Technical 1", "Criteria 1", priority=5
        )
        task2 = self.test.create_task(
            roadmap, "Task 2", "Functional 2", "Technical 2", "Criteria 2", priority=3
        )

        # Create sprint
        sprint_id = self.test.create_sprint(roadmap, "Development Sprint")

        # Add tasks to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            f"{task1},{task2}"
        ])

        # Verify tasks are in sprint
        result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint_id)])
        assert len(result) == 2

        # Verify task statuses changed to SPRINT
        self.test.assert_task_status(roadmap, task1, "SPRINT")
        self.test.assert_task_status(roadmap, task2, "SPRINT")

        # Start sprint
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # Move one task to DOING
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "DOING", "--commit-open", "5f93b51"])
        self.test.assert_task_status(roadmap, task1, "DOING")

        # Complete task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "COMPLETED", "--commit-close", "cf507b0"])
        self.test.assert_task_status(roadmap, task1, "COMPLETED")

        # Close sprint — task2 is still SPRINT (never started), so --force is required
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint_id), "--force"])
        self.test.assert_sprint_status(roadmap, sprint_id, "CLOSED")

        print("✓ Sprint with tasks lifecycle test passed")


def main():
    """Run all tests."""
    test = TestSprintLifecycle()

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
