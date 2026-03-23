#!/usr/bin/env python3
"""
E2E tests for task --type flag.

Covers:
- All 10 valid type values can be created
- Default type is TASK when not specified
- task edit --type changes the type
- Invalid type values are rejected with appropriate exit code
- type field appears in task get and task list output
"""

import pytest
from tests.base_test import GroadmapTestBase


ALL_VALID_TYPES = [
    "USER_STORY",
    "TASK",
    "BUG",
    "SUB_TASK",
    "EPIC",
    "REFACTOR",
    "CHORE",
    "SPIKE",
    "DESIGN_UX",
    "IMPROVEMENT",
]


class TestTaskTypeFlag:
    @pytest.fixture(autouse=True)
    def setup(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        yield
        self.test.teardown()

    def test_default_type_is_task(self):
        """Task created without --type defaults to TASK."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            "Default type task",
            "Verify default type behaviour",
            "Omit --type flag on creation",
            "Task type must default to TASK",
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["type"] == "TASK"

    def test_all_valid_types_can_be_created(self):
        """Every valid type value is accepted by task create."""
        roadmap = self.test.create_roadmap()
        for task_type in ALL_VALID_TYPES:
            exit_code, stdout, stderr = self.test.run_cmd(
                [
                    "task", "create", "-r", roadmap,
                    "-t", f"Task of type {task_type}",
                    "-fr", f"Functional requirement for {task_type}",
                    "-tr", f"Technical requirement for {task_type}",
                    "-ac", f"Acceptance criteria for {task_type}",
                    "-y", task_type,
                ],
                check=False,
            )
            assert exit_code == 0, (
                f"Expected exit 0 for type {task_type}, got {exit_code}. stderr: {stderr}"
            )
            import json as _json
            task_id = _json.loads(stdout)["id"]

            result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
            assert result[0]["type"] == task_type, (
                f"Expected type {task_type}, got {result[0]['type']}"
            )

    def test_type_persists_in_task_list(self):
        """Type field is present and correct in task list output."""
        roadmap = self.test.create_roadmap()
        import json as _json

        exit_code, stdout, _ = self.test.run_cmd(
            [
                "task", "create", "-r", roadmap,
                "-t", "Bug report task",
                "-fr", "System crashes under load",
                "-tr", "Investigate memory allocations",
                "-ac", "No crash under 10k concurrent requests",
                "-y", "BUG",
            ],
            check=False,
        )
        assert exit_code == 0
        task_id = _json.loads(stdout)["id"]

        tasks = self.test.run_cmd_json(["task", "list", "-r", roadmap])
        matching = [t for t in tasks if t["id"] == task_id]
        assert len(matching) == 1
        assert matching[0]["type"] == "BUG"

    def test_edit_changes_type(self):
        """task edit --type updates the task type correctly."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            "Initial task",
            "Initial functional requirement",
            "Initial technical requirement",
            "Initial acceptance criteria",
        )

        # Confirm initial type is TASK
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["type"] == "TASK"

        # Change type to REFACTOR
        self.test.run_cmd(["task", "edit", "-r", roadmap, str(task_id), "-y", "REFACTOR"])

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["type"] == "REFACTOR"

    def test_edit_type_cycles_through_all_values(self):
        """task edit --type accepts every valid type as an update."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            "Type cycling task",
            "Verify all types are editable",
            "Call task edit with each type in sequence",
            "Each edit persists the new type",
        )

        for task_type in ALL_VALID_TYPES:
            self.test.run_cmd(["task", "edit", "-r", roadmap, str(task_id), "-y", task_type])
            result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
            assert result[0]["type"] == task_type, (
                f"After editing to {task_type}, got {result[0]['type']}"
            )

    def test_invalid_type_rejected_on_create(self):
        """task create rejects unknown type values with non-zero exit code."""
        roadmap = self.test.create_roadmap()
        invalid_types = ["FEATURE", "RESEARCH", "DOCUMENTATION", "TESTING", "invalid"]

        for bad_type in invalid_types:
            args = [
                "task", "create", "-r", roadmap,
                "-t", "Should fail",
                "-fr", "FR", "-tr", "TR", "-ac", "AC",
                "-y", bad_type,
            ]
            exit_code, _, stderr = self.test.run_cmd(args, check=False)
            assert exit_code != 0, (
                f"Expected non-zero exit for invalid type {bad_type!r}, got 0"
            )

    def test_invalid_type_rejected_on_edit(self):
        """task edit rejects unknown type values with non-zero exit code."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap, "Stable task", "FR", "TR", "AC"
        )

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "edit", "-r", roadmap, str(task_id), "-y", "NONEXISTENT_TYPE"],
            check=False,
        )
        assert exit_code != 0, "Expected non-zero exit for invalid type on edit"

        # Type must remain unchanged
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["type"] == "TASK"

    def test_type_case_sensitivity(self):
        """Type values are case-sensitive; lowercase variants are rejected."""
        roadmap = self.test.create_roadmap()
        exit_code, _, _ = self.test.run_cmd(
            [
                "task", "create", "-r", roadmap,
                "-t", "Lowercase type test",
                "-fr", "FR", "-tr", "TR", "-ac", "AC",
                "-y", "bug",
            ],
            check=False,
        )
        assert exit_code != 0, "Expected rejection of lowercase 'bug' type"
