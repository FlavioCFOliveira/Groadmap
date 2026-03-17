#!/usr/bin/env python3
"""
Test 06: Edge Cases and Error Handling
Tests error conditions, edge cases, and boundary values.
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestEdgeCasesErrors:
    """Test edge cases and error handling."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_create_roadmap_without_name(self):
        """Test creating roadmap without name fails."""
        exit_code, _, _ = self.test.run_cmd(
            ["roadmap", "create"],
            check=False
        )
        assert exit_code == 2, "Should fail with exit code 2 (misuse)"

        print("✓ Create roadmap without name test passed")

    def test_create_duplicate_roadmap(self):
        """Test creating duplicate roadmap fails."""
        name = self.test.create_roadmap("unique-project")

        # Try to create again
        exit_code, _, _ = self.test.run_cmd(
            ["roadmap", "create", name],
            check=False
        )
        assert exit_code == 5, "Should fail with exit code 5 (already exists)"

        print("✓ Create duplicate roadmap test passed")

    def test_use_nonexistent_roadmap(self):
        """Test using non-existent roadmap fails."""
        exit_code, _, _ = self.test.run_cmd(
            ["roadmap", "use", "nonexistent-roadmap-12345"],
            check=False
        )
        assert exit_code == 4, "Should fail with exit code 4 (not found)"

        print("✓ Use nonexistent roadmap test passed")

    def test_create_task_without_required_fields(self):
        """Test creating task without required fields fails."""
        roadmap = self.test.create_roadmap()

        # Missing description
        exit_code, _, _ = self.test.run_cmd(
            ["task", "create", "-r", roadmap, "-a", "Action", "-e", "Result"],
            check=False
        )
        assert exit_code != 0

        # Missing action
        exit_code, _, _ = self.test.run_cmd(
            ["task", "create", "-r", roadmap, "-d", "Description", "-e", "Result"],
            check=False
        )
        assert exit_code != 0

        # Missing expected result
        exit_code, _, _ = self.test.run_cmd(
            ["task", "create", "-r", roadmap, "-d", "Description", "-a", "Action"],
            check=False
        )
        assert exit_code != 0

        print("✓ Create task without required fields test passed")

    def test_get_nonexistent_task(self):
        """Test getting non-existent task returns empty result."""
        roadmap = self.test.create_roadmap()

        # Getting non-existent task returns empty list with exit code 0
        result = self.test.run_cmd_json(
            ["task", "get", "-r", roadmap, "99999"]
        )
        assert result == [], f"Expected empty list, got {result}"

        print("✓ Get nonexistent task test passed")

    def test_edit_nonexistent_task(self):
        """Test editing non-existent task fails."""
        roadmap = self.test.create_roadmap()

        exit_code, _, _ = self.test.run_cmd(
            ["task", "edit", "-r", roadmap, "99999", "-d", "New description"],
            check=False
        )
        assert exit_code == 4, "Should fail with exit code 4 (not found)"

        print("✓ Edit nonexistent task test passed")

    def test_invalid_priority_values(self):
        """Test invalid priority values are rejected."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(roadmap, "Task", "Action", "Result")

        # Negative priority
        exit_code, _, _ = self.test.run_cmd(
            ["task", "prio", "-r", roadmap, str(task_id), "-1"],
            check=False
        )
        assert exit_code != 0

        # Priority > 9
        exit_code, _, _ = self.test.run_cmd(
            ["task", "prio", "-r", roadmap, str(task_id), "10"],
            check=False
        )
        assert exit_code != 0

        # Non-numeric priority
        exit_code, _, _ = self.test.run_cmd(
            ["task", "prio", "-r", roadmap, str(task_id), "high"],
            check=False
        )
        assert exit_code != 0

        print("✓ Invalid priority values test passed")

    def test_invalid_severity_values(self):
        """Test invalid severity values are rejected."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(roadmap, "Task", "Action", "Result")

        # Negative severity
        exit_code, _, _ = self.test.run_cmd(
            ["task", "sev", "-r", roadmap, str(task_id), "-1"],
            check=False
        )
        assert exit_code != 0

        # Severity > 9
        exit_code, _, _ = self.test.run_cmd(
            ["task", "sev", "-r", roadmap, str(task_id), "10"],
            check=False
        )
        assert exit_code != 0

        print("✓ Invalid severity values test passed")

    def test_invalid_task_id(self):
        """Test invalid task IDs are rejected."""
        roadmap = self.test.create_roadmap()

        # Non-numeric ID
        exit_code, _, _ = self.test.run_cmd(
            ["task", "get", "-r", roadmap, "abc"],
            check=False
        )
        assert exit_code != 0

        # Zero ID
        exit_code, _, _ = self.test.run_cmd(
            ["task", "get", "-r", roadmap, "0"],
            check=False
        )
        assert exit_code != 0

        # Negative ID
        exit_code, _, _ = self.test.run_cmd(
            ["task", "get", "-r", roadmap, "-1"],
            check=False
        )
        assert exit_code != 0

        print("✓ Invalid task ID test passed")

    def test_invalid_sprint_id(self):
        """Test invalid sprint IDs are rejected."""
        roadmap = self.test.create_roadmap()

        # Non-numeric ID
        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "get", "-r", roadmap, "abc"],
            check=False
        )
        assert exit_code != 0

        print("✓ Invalid sprint ID test passed")

    def test_get_nonexistent_sprint(self):
        """Test getting non-existent sprint fails."""
        roadmap = self.test.create_roadmap()

        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "get", "-r", roadmap, "99999"],
            check=False
        )
        assert exit_code == 4, "Should fail with exit code 4 (not found)"

        print("✓ Get nonexistent sprint test passed")

    def test_start_nonexistent_sprint(self):
        """Test starting non-existent sprint fails."""
        roadmap = self.test.create_roadmap()

        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "start", "-r", roadmap, "99999"],
            check=False
        )
        assert exit_code == 4, "Should fail with exit code 4 (not found)"

        print("✓ Start nonexistent sprint test passed")

    def test_add_tasks_to_nonexistent_sprint(self):
        """Test adding tasks to non-existent sprint fails."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(roadmap, "Task", "Action", "Result")

        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", roadmap, "99999", str(task_id)],
            check=False
        )
        assert exit_code == 4, "Should fail with exit code 4 (not found)"

        print("✓ Add tasks to nonexistent sprint test passed")

    def test_add_nonexistent_task_to_sprint(self):
        """Test adding non-existent task to sprint fails."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.create_sprint(roadmap, "Sprint")

        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", roadmap, str(sprint_id), "99999"],
            check=False
        )
        assert exit_code != 0

        print("✓ Add nonexistent task to sprint test passed")

    def test_empty_roadmap_name(self):
        """Test empty roadmap name is rejected."""
        exit_code, _, _ = self.test.run_cmd(
            ["roadmap", "create", ""],
            check=False
        )
        assert exit_code != 0

        print("✓ Empty roadmap name test passed")

    def test_roadmap_name_with_special_chars(self):
        """Test roadmap name with special characters."""
        # These should be rejected or sanitized
        invalid_names = [
            "name/with/slashes",
            "name..with..dots",
            "name with spaces",
            ".hidden",
        ]

        for name in invalid_names:
            exit_code, _, _ = self.test.run_cmd(
                ["roadmap", "create", name],
                check=False
            )
            # Should either succeed with sanitization or fail gracefully
            assert exit_code in [0, 2, 5, 6], f"Unexpected exit code {exit_code} for name: {name}"

        print("✓ Roadmap name with special chars test passed")

    def test_no_roadmap_selected(self):
        """Test operations without selecting roadmap fail appropriately."""
        # Create a roadmap but don't select it
        roadmap = self.test.create_roadmap()

        # Try to create task without -r flag and no default
        exit_code, _, _ = self.test.run_cmd(
            ["task", "create", "-d", "Task", "-a", "Action", "-e", "Result"],
            check=False
        )
        assert exit_code == 3, "Should fail with exit code 3 (no roadmap selected)"

        print("✓ No roadmap selected test passed")

    def test_boundary_priority_values(self):
        """Test boundary priority values (0 and 9)."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(roadmap, "Task", "Action", "Result")

        # Minimum priority
        self.test.run_cmd(["task", "prio", "-r", roadmap, str(task_id), "0"])
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["priority"] == 0

        # Maximum priority
        self.test.run_cmd(["task", "prio", "-r", roadmap, str(task_id), "9"])
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["priority"] == 9

        print("✓ Boundary priority values test passed")

    def test_boundary_severity_values(self):
        """Test boundary severity values (0 and 9)."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(roadmap, "Task", "Action", "Result")

        # Minimum severity
        self.test.run_cmd(["task", "sev", "-r", roadmap, str(task_id), "0"])
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["severity"] == 0

        # Maximum severity
        self.test.run_cmd(["task", "sev", "-r", roadmap, str(task_id), "9"])
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["severity"] == 9

        print("✓ Boundary severity values test passed")

    def test_bulk_operations_with_mixed_valid_invalid_ids(self):
        """Test bulk operations with mix of valid and invalid IDs."""
        roadmap = self.test.create_roadmap()

        task1 = self.test.create_task(roadmap, "Task 1", "Action", "Result")
        task2 = self.test.create_task(roadmap, "Task 2", "Action", "Result")

        # Set priority with one invalid ID - valid tasks are updated
        self.test.run_cmd(
            ["task", "prio", "-r", roadmap, f"{task1},99999,{task2}", "5"]
        )

        # Verify valid tasks were updated
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task1)])
        assert result[0]["priority"] == 5
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task2)])
        assert result[0]["priority"] == 5

        print("✓ Bulk operations with mixed valid/invalid IDs test passed")

    def test_remove_already_removed_task(self):
        """Test removing an already removed task fails."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(roadmap, "Task", "Action", "Result")

        # Remove task
        self.test.run_cmd(["task", "remove", "-r", roadmap, str(task_id)])

        # Try to remove again
        exit_code, _, _ = self.test.run_cmd(
            ["task", "remove", "-r", roadmap, str(task_id)],
            check=False
        )
        assert exit_code == 4, "Should fail with exit code 4 (not found)"

        print("✓ Remove already removed task test passed")

    def test_edit_with_no_changes(self):
        """Test editing task with no changes fails."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(roadmap, "Task", "Action", "Result")

        # Try to edit with no fields
        exit_code, _, _ = self.test.run_cmd(
            ["task", "edit", "-r", roadmap, str(task_id)],
            check=False
        )
        assert exit_code != 0

        print("✓ Edit with no changes test passed")

    def test_help_commands(self):
        """Test help commands work."""
        # Main help
        exit_code, stdout, _ = self.test.run_cmd(["--help"])
        assert exit_code == 0
        assert "Usage:" in stdout

        # Command help
        exit_code, stdout, _ = self.test.run_cmd(["task", "--help"])
        assert exit_code == 0
        assert "Usage:" in stdout

        exit_code, stdout, _ = self.test.run_cmd(["sprint", "--help"])
        assert exit_code == 0
        assert "Usage:" in stdout

        exit_code, stdout, _ = self.test.run_cmd(["roadmap", "--help"])
        assert exit_code == 0
        assert "Usage:" in stdout

        exit_code, stdout, _ = self.test.run_cmd(["audit", "--help"])
        assert exit_code == 0
        assert "Usage:" in stdout

        print("✓ Help commands test passed")

    def test_version_command(self):
        """Test version command."""
        exit_code, stdout, _ = self.test.run_cmd(["--version"])
        assert exit_code == 0
        assert "Groadmap" in stdout
        assert "version" in stdout

        print("✓ Version command test passed")


def main():
    """Run all tests."""
    test = TestEdgeCasesErrors()

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
