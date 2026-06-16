#!/usr/bin/env python3
"""
Base test class for Groadmap CLI tests.
Provides common utilities and setup/teardown functionality.
"""

import subprocess
import json
import os
import tempfile
import shutil
import uuid
from pathlib import Path
from typing import List, Optional, Dict, Any, Tuple, Union


class GroadmapTestBase:
    """Base class for all Groadmap CLI tests."""

    def __init__(self):
        self.cli_path = self._find_cli()
        self.test_dir = None
        self.home_dir = None
        self.roadmaps_dir = None

    def _find_cli(self) -> str:
        """Find the CLI binary path."""
        # Try common locations
        possible_paths = [
            Path(__file__).parent.parent / "bin" / "rmp",
            Path(__file__).parent.parent / "rmp",
            Path.cwd() / "bin" / "rmp",
            Path.cwd() / "rmp",
        ]
        for path in possible_paths:
            if path.exists():
                return str(path.absolute())
        # Try to build it
        project_root = Path(__file__).parent.parent
        result = subprocess.run(
            ["go", "build", "-o", "./bin/rmp", "./cmd/rmp"],
            cwd=project_root,
            capture_output=True,
            text=True
        )
        if result.returncode == 0:
            return str(project_root / "bin" / "rmp")
        raise RuntimeError(f"Could not find or build CLI binary: {result.stderr}")

    def setup(self):
        """Set up test environment."""
        # Create temporary directory for test isolation
        self.test_dir = tempfile.mkdtemp(prefix="groadmap_test_")
        self.home_dir = Path(self.test_dir) / "home"
        self.home_dir.mkdir()
        self.roadmaps_dir = self.home_dir / ".roadmaps"

    def teardown(self):
        """Clean up test environment."""
        if self.test_dir and os.path.exists(self.test_dir):
            shutil.rmtree(self.test_dir)

    def run_cmd(self, args: List[str], check: bool = True) -> Tuple[int, str, str]:
        """
        Run a CLI command and return (exit_code, stdout, stderr).

        Args:
            args: Command arguments (without the binary name)
            check: If True, raise AssertionError on non-zero exit

        Returns:
            Tuple of (exit_code, stdout, stderr)
        """
        env = os.environ.copy()
        env["HOME"] = str(self.home_dir)

        result = subprocess.run(
            [self.cli_path] + args,
            capture_output=True,
            text=True,
            env=env
        )

        if check and result.returncode != 0:
            raise AssertionError(
                f"Command failed: rmp {' '.join(args)}\n"
                f"Exit code: {result.returncode}\n"
                f"Stdout: {result.stdout}\n"
                f"Stderr: {result.stderr}"
            )

        return result.returncode, result.stdout, result.stderr

    def run_cmd_json(self, args: List[str], check: bool = True) -> Any:
        """Run a command and parse JSON output."""
        exit_code, stdout, stderr = self.run_cmd(args, check=check)
        if not stdout.strip():
            return {}
        try:
            result = json.loads(stdout)
            # Convert null (None) to empty list for list operations
            if result is None:
                return []
            return result
        except json.JSONDecodeError as e:
            raise AssertionError(
                f"Failed to parse JSON output: {e}\n"
                f"Output was: {stdout}"
            )

    def generate_roadmap_name(self) -> str:
        """Generate a unique roadmap name for testing."""
        return f"test_roadmap_{uuid.uuid4().hex[:8]}"

    def create_roadmap(self, name: Optional[str] = None) -> str:
        """Create a new roadmap and return its name."""
        if name is None:
            name = self.generate_roadmap_name()
        self.run_cmd(["roadmap", "create", name])
        return name

    def create_task(self, roadmap: str, title: str, functional_requirements: str,
                    technical_requirements: str, acceptance_criteria: str, **kwargs) -> int:
        """
        Create a task and return its ID.

        Args:
            roadmap: Roadmap name
            title: Task title
            functional_requirements: Functional requirements (Why?)
            technical_requirements: Technical requirements (How?)
            acceptance_criteria: Acceptance criteria (How to verify?)
            **kwargs: Optional fields (priority, severity, specialists)
        """
        cmd = [
            "task", "create",
            "-r", roadmap,
            "-t", title,
            "-fr", functional_requirements,
            "-tr", technical_requirements,
            "-ac", acceptance_criteria
        ]

        if "priority" in kwargs:
            cmd.extend(["-p", str(kwargs["priority"])])
        if "severity" in kwargs:
            cmd.extend(["--severity", str(kwargs["severity"])])
        if "specialists" in kwargs:
            cmd.extend(["-sp", kwargs["specialists"]])

        result = self.run_cmd_json(cmd)
        return result["id"]

    def create_sprint(self, roadmap: str, description: str, title: str = "") -> int:
        """Create a sprint and return its ID.

        Args:
            roadmap: Roadmap name
            description: Sprint description (also used as title when title is omitted)
            title: Sprint title; defaults to description when not supplied
        """
        sprint_title = title if title else description
        result = self.run_cmd_json([
            "sprint", "create",
            "-r", roadmap,
            "-t", sprint_title,
            "-d", description
        ])
        return result["id"]

    def move_task_to_sprint(self, roadmap: str, task_id: int, sprint_id: Optional[int] = None) -> int:
        """Move a task from BACKLOG to SPRINT via `sprint add-tasks`.

        Manual `task stat <id> SPRINT` is rejected per SPEC/STATE_MACHINE.md;
        SPRINT is an automatic transition triggered only by sprint assignment.
        Creates a new sprint if sprint_id is not provided. Returns the sprint_id used.
        """
        if sprint_id is None:
            sprint_id = self.create_sprint(roadmap, f"Test sprint for task {task_id}")
        self.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)])
        return sprint_id

    def assert_task_status(self, roadmap: str, task_id: int, expected_status: str):
        """Assert that a task has the expected status."""
        result = self.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        if isinstance(result, list) and len(result) > 0:
            actual_status = result[0].get("status")
        else:
            actual_status = result.get("status")
        assert actual_status == expected_status, (
            f"Task {task_id} expected status {expected_status}, got {actual_status}"
        )

    def assert_sprint_status(self, roadmap: str, sprint_id: int, expected_status: str):
        """Assert that a sprint has the expected status."""
        result = self.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        actual_status = result.get("status")
        assert actual_status == expected_status, (
            f"Sprint {sprint_id} expected status {expected_status}, got {actual_status}"
        )

    def assert_exit_code(self, args: List[str], expected_code: int):
        """Assert that a command returns the expected exit code."""
        exit_code, _, _ = self.run_cmd(args, check=False)
        assert exit_code == expected_code, (
            f"Expected exit code {expected_code}, got {exit_code}"
        )

    # Canonical JSON shapes per SPEC/MODELS.md. Used by assert_task_shape /
    # assert_sprint_shape to catch fields that disappear or are added
    # without an explicit decision.
    TASK_KEYS = frozenset([
        "id", "title", "status", "type",
        "functional_requirements", "technical_requirements", "acceptance_criteria",
        "created_at", "specialists",
        "started_at", "tested_at", "closed_at", "completion_summary",
        "parent_task_id", "priority", "severity",
        "subtask_count", "depends_on", "blocks",
    ])
    SPRINT_KEYS = frozenset([
        "id", "title", "status", "description",
        "created_at", "started_at", "closed_at",
        "max_tasks", "tasks", "task_count",
        "order",
    ])

    @classmethod
    def assert_task_shape(cls, task: Dict[str, Any]):
        """Validate that a task JSON object carries exactly the SPEC-defined keys.

        Catches both regressions (a field silently dropped) and accidental
        additions (a field introduced without a SPEC update).
        """
        got = set(task.keys())
        missing = cls.TASK_KEYS - got
        extra = got - cls.TASK_KEYS
        assert not missing and not extra, (
            f"task JSON shape diverges from SPEC:\n  missing: {sorted(missing)}\n  extra:   {sorted(extra)}"
        )

    @classmethod
    def assert_sprint_shape(cls, sprint: Dict[str, Any]):
        """Validate that a sprint JSON object carries exactly the SPEC-defined keys."""
        got = set(sprint.keys())
        missing = cls.SPRINT_KEYS - got
        extra = got - cls.SPRINT_KEYS
        assert not missing and not extra, (
            f"sprint JSON shape diverges from SPEC:\n  missing: {sorted(missing)}\n  extra:   {sorted(extra)}"
        )

    def list_tasks(self, roadmap: str, **filters) -> List[Dict[str, Any]]:
        """List tasks with optional filters."""
        cmd = ["task", "list", "-r", roadmap]
        if "status" in filters:
            cmd.extend(["-s", filters["status"]])
        if "priority" in filters:
            cmd.extend(["-p", str(filters["priority"])])
        if "severity" in filters:
            cmd.extend(["--severity", str(filters["severity"])])
        if "limit" in filters:
            cmd.extend(["-l", str(filters["limit"])])
        return self.run_cmd_json(cmd)

    def list_sprints(self, roadmap: str, status: Optional[str] = None) -> List[Dict[str, Any]]:
        """List sprints with optional status filter."""
        cmd = ["sprint", "list", "-r", roadmap]
        if status:
            cmd.extend(["--status", status])
        return self.run_cmd_json(cmd)
