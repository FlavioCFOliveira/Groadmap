#!/usr/bin/env python3
"""
Test 06: Edge Cases and Error Handling
Tests error conditions, edge cases, and boundary values.
"""

import sys
import os
import re
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


# --------------------------------------------------------------------------
# The validation order SPEC/COMMANDS.md publishes for the three Task Assignment
# subcommands (rmp task #337).
#
# The Go gate internal/commands/sprint_assignment_validation_order_test.go
# drives the same orderings, but it reads exit codes through a COPY of the
# sentinel switch in cmd/rmp/main.go: it proves the error chain, not the number
# the shell reports. That number is the whole point of the ordering -- it is
# what a caller branches on -- so it is asserted here, against the process, from
# the same published list. Two observers, one oracle.
#
# The order is read out of the document rather than typed in here. Swapping two
# items in COMMANDS.md swaps the expectation; changing the binary while the
# document stands fails the same assertion from the other side.
# --------------------------------------------------------------------------

_SPEC_PATH = Path(__file__).resolve().parent.parent / "SPEC" / "COMMANDS.md"
_ASSIGNMENT_HEADING = "### Task Assignment"
_VALIDATION_ORDER_LABEL = "**Validation Order:**"

# Floors under the two scans. The extracted section runs to some nine thousand
# bytes and the list is longer than eight items; anything smaller means a scan
# stopped matching, and an expectation derived from a fragment is unsound.
_MIN_SECTION_BYTES = 3000
_MIN_VALIDATION_STEPS = 8
_MAX_LINES_BEFORE_FIRST_STEP = 24

_SECTION_END = re.compile(r"^#{1,3} \S", re.MULTILINE)
_NUMBERED_ITEM = re.compile(r"^(\d+)\. (.+)$")

# Each step is recognised by a narrow phrase, never by its ordinal: the list is
# renumbered whenever a step is published or removed.
_STEP_MARKERS = [
    ("sprint-id lexical", re.compile(r"format and range of every sprint id", re.I)),
    ("task-id lexical", re.compile(r"parse all task IDs and validate their format", re.I)),
    ("roadmap open", re.compile(r"open the roadmap", re.I)),
    ("sprint existence", re.compile(r"verify the sprint exists", re.I)),
    ("CLOSED sprint", re.compile(r"reject a CLOSED sprint", re.I)),
    ("task existence", re.compile(r"verify all task IDs exist in the roadmap", re.I)),
    ("capacity", re.compile(r"max_tasks")),
    ("membership", re.compile(r"currently a member of the sprint", re.I)),
    ("re-parenting", re.compile(
        r"nothing is verified about the sprint a task already belongs to", re.I)),
    ("execute", re.compile(r"execute the operation", re.I)),
    ("fail fast", re.compile(r"exit immediately without making changes", re.I)),
]


def _task_assignment_section() -> str:
    """Return the body of SPEC/COMMANDS.md section Task Assignment."""
    content = _SPEC_PATH.read_text(encoding="utf-8")
    start = content.find(_ASSIGNMENT_HEADING + "\n")
    assert start >= 0, (
        f"{_SPEC_PATH} declares no {_ASSIGNMENT_HEADING!r} heading, so this gate is not "
        f"reading the section it names"
    )
    body = content[start + len(_ASSIGNMENT_HEADING):]
    end = _SECTION_END.search(body)
    if end:
        body = body[:end.start()]
    assert len(body) >= _MIN_SECTION_BYTES, (
        f"extracted {len(body)} bytes for {_ASSIGNMENT_HEADING}, want at least "
        f"{_MIN_SECTION_BYTES}: the section scan has stopped matching"
    )
    return body


def _published_validation_order() -> list:
    """Return the published validation order as a list of step names.

    Three phases, each failing loudly: locate the label; consume a CONTIGUOUS
    run of numbered lines whose numbers are exactly 1, 2, 3, ...; classify each
    item by exactly one marker. The contiguity requirement is what keeps the
    scan off the two other numbered lists in the same section -- the four rules
    governing the audit entries, and the acceptance criteria.
    """
    section = _task_assignment_section()

    at = section.find(_VALIDATION_ORDER_LABEL)
    assert at >= 0, f"{_ASSIGNMENT_HEADING} carries no {_VALIDATION_ORDER_LABEL!r} label"
    lines = section[at + len(_VALIDATION_ORDER_LABEL):].split("\n")

    head = -1
    for i, line in enumerate(lines):
        if i > _MAX_LINES_BEFORE_FIRST_STEP:
            break
        match = _NUMBERED_ITEM.match(line)
        if match:
            assert match.group(1) == "1", (
                f"the first numbered line after {_VALIDATION_ORDER_LABEL!r} is {line!r}, so the "
                f"list does not start at 1 and is not the run this gate can read"
            )
            head = i
            break
        assert not line.startswith("#") and not line.startswith("**"), (
            f"the next thing after {_VALIDATION_ORDER_LABEL!r} is {line!r}, not an ordered list"
        )
    assert head >= 0, (
        f"no ordered list starts within {_MAX_LINES_BEFORE_FIRST_STEP} lines of "
        f"{_VALIDATION_ORDER_LABEL!r}"
    )

    items = []
    for line in lines[head:]:
        match = _NUMBERED_ITEM.match(line)
        if not match or int(match.group(1)) != len(items) + 1:
            break
        items.append(match.group(2))
    assert len(items) >= _MIN_VALIDATION_STEPS, (
        f"the run after {_VALIDATION_ORDER_LABEL!r} holds {len(items)} items, want at least "
        f"{_MIN_VALIDATION_STEPS}: the scan has stopped matching the published list"
    )

    order = []
    for position, item in enumerate(items, start=1):
        matched = [name for name, marker in _STEP_MARKERS if marker.search(item)]
        assert len(matched) == 1, (
            f"{_ASSIGNMENT_HEADING} validation order item {position} matches {len(matched)} of "
            f"this gate's step markers ({matched}); exactly one must match. The item is:\n"
            f"  {item!r}"
        )
        order.append(matched[0])
    return order


def _step_that_runs_first(order: list, left: str, right: str) -> str:
    """Return whichever of two published steps the document puts first."""
    for name in (left, right):
        assert name in order, (
            f"{_ASSIGNMENT_HEADING} publishes no step this gate reads as {name!r}, so the pair "
            f"it belongs to cannot be driven and this test would assert nothing"
        )
    return left if order.index(left) < order.index(right) else right


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

    def test_create_task_without_required_fields(self):
        """Test creating task without required fields fails with exit 2 (misuse).

        Each missing field must surface its own name in stderr so the user
        knows precisely which flag was forgotten.
        """
        roadmap = self.test.create_roadmap()

        cases = [
            # (missing field name, args)
            ("--title",                  ["-fr", "Functional", "-tr", "Technical", "-ac", "Criteria"]),
            ("--functional-requirements", ["-t", "Title", "-tr", "Technical", "-ac", "Criteria"]),
            ("--technical-requirements",  ["-t", "Title", "-fr", "Functional", "-ac", "Criteria"]),
            ("--acceptance-criteria",     ["-t", "Title", "-fr", "Functional", "-tr", "Technical"]),
        ]
        for missing, extra_args in cases:
            exit_code, _, stderr = self.test.run_cmd(
                ["task", "create", "-r", roadmap, *extra_args],
                check=False,
            )
            assert exit_code == 2, (
                f"missing {missing} must exit 2 (misuse); got {exit_code}, stderr={stderr}"
            )
            assert missing in stderr, (
                f"stderr must name the missing field {missing!r}; got {stderr!r}"
            )

        print("✓ Create task without required fields: each missing field returns exit 2 with field name")

    def test_get_nonexistent_task(self):
        """task get on an unknown ID must fail-fast with exit 4 (finding #44).

        Per SPEC/COMMANDS.md § Get Task(s), an unknown ID — including the
        all-invalid case — must return exit 4 (not found), not a null/empty
        result with exit 0.
        """
        roadmap = self.test.create_roadmap()

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "get", "-r", roadmap, "99999"], check=False
        )
        assert exit_code == 4, f"Expected exit 4 for unknown ID, got {exit_code}"
        assert "not found" in stderr.lower(), f"Expected 'not found' message, got: {stderr}"

        print("✓ Get nonexistent task fail-fast (exit 4) test passed")

    def test_edit_nonexistent_task(self):
        """Test editing non-existent task fails."""
        roadmap = self.test.create_roadmap()

        exit_code, _, _ = self.test.run_cmd(
            ["task", "edit", "-r", roadmap, "99999", "-t", "New title"],
            check=False
        )
        assert exit_code == 4, "Should fail with exit code 4 (not found)"

        print("✓ Edit nonexistent task test passed")

    def test_invalid_priority_values(self):
        """Test invalid priority values are rejected with exit 6 and a 'priority' message."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(roadmap, "Task", "Functional", "Technical", "Criteria")

        for bad in ["-1", "10", "high"]:
            exit_code, _, stderr = self.test.run_cmd(
                ["task", "prio", "-r", roadmap, str(task_id), bad],
                check=False,
            )
            assert exit_code == 6, f"priority={bad!r} must exit 6; got {exit_code}, stderr={stderr}"
            assert "priority" in stderr.lower(), (
                f"stderr must mention 'priority' for value {bad!r}; got {stderr!r}"
            )

        print("✓ Invalid priority values rejected with exit 6 + 'priority' message")

    def test_invalid_severity_values(self):
        """Test invalid severity values are rejected with exit 6 and a 'severity' message."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(roadmap, "Task", "Functional", "Technical", "Criteria")

        for bad in ["-1", "10"]:
            exit_code, _, stderr = self.test.run_cmd(
                ["task", "sev", "-r", roadmap, str(task_id), bad],
                check=False,
            )
            assert exit_code == 6, f"severity={bad!r} must exit 6; got {exit_code}, stderr={stderr}"
            assert "severity" in stderr.lower(), (
                f"stderr must mention 'severity' for value {bad!r}; got {stderr!r}"
            )

        print("✓ Invalid severity values rejected with exit 6 + 'severity' message")

    def test_invalid_task_id(self):
        """Test non-numeric task IDs are rejected with exit 2 and a 'task ID' message.

        A non-numeric ID token cannot be parsed into the declared integer
        shape: per SPEC/ARCHITECTURE.md this is a syntax/misuse error
        (EXIT_MISUSE / exit 2), not an invalid-data error (exit 6).
        """
        roadmap = self.test.create_roadmap()

        exit_code, _, stderr = self.test.run_cmd(
            ["task", "get", "-r", roadmap, "abc"],
            check=False,
        )
        assert exit_code == 2, f"non-numeric task id must exit 2; got {exit_code}, stderr={stderr}"
        assert "task ID" in stderr or "task id" in stderr.lower(), (
            f"stderr must mention task ID; got {stderr!r}"
        )

        print("✓ Invalid task ID rejected with exit 2 + 'task ID' message")

    def test_invalid_sprint_id(self):
        """Test non-numeric sprint IDs are rejected with exit 2 and a 'sprint ID' message.

        A non-numeric ID token cannot be parsed into the declared integer
        shape: per SPEC/ARCHITECTURE.md this is a syntax/misuse error
        (EXIT_MISUSE / exit 2), not an invalid-data error (exit 6).
        """
        roadmap = self.test.create_roadmap()

        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "get", "-r", roadmap, "abc"],
            check=False,
        )
        assert exit_code == 2, f"non-numeric sprint id must exit 2; got {exit_code}, stderr={stderr}"
        assert "sprint ID" in stderr or "sprint id" in stderr.lower(), (
            f"stderr must mention sprint ID; got {stderr!r}"
        )

        print("✓ Invalid sprint ID rejected with exit 2 + 'sprint ID' message")

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
        task_id = self.test.create_task(roadmap, "Task", "Functional", "Technical", "Criteria")

        exit_code, _, _ = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", roadmap, "99999", str(task_id)],
            check=False
        )
        assert exit_code == 4, "Should fail with exit code 4 (not found)"

        print("✓ Add tasks to nonexistent sprint test passed")

    def test_add_nonexistent_task_to_sprint(self):
        """Test adding non-existent task to sprint fails with exit 4 + 'not found' message."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.create_sprint(roadmap, "Sprint")

        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", roadmap, str(sprint_id), "99999"],
            check=False,
        )
        assert exit_code == 4, f"adding missing task must exit 4; got {exit_code}, stderr={stderr}"
        assert "99999" in stderr, f"stderr must reference the missing task id; got {stderr!r}"
        assert "not found" in stderr.lower(), f"stderr must say 'not found'; got {stderr!r}"

        print("✓ Add nonexistent task to sprint: exit 4 + 'not found' message naming task id")

    def test_empty_roadmap_name(self):
        """Test empty roadmap name is rejected with exit 6 + 'empty' message."""
        exit_code, _, stderr = self.test.run_cmd(
            ["roadmap", "create", ""],
            check=False,
        )
        assert exit_code == 6, f"empty roadmap name must exit 6; got {exit_code}, stderr={stderr}"
        assert "empty" in stderr.lower() or "required" in stderr.lower(), (
            f"stderr must reference empty/required; got {stderr!r}"
        )

        print("✓ Empty roadmap name rejected with exit 6 + descriptive message")

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
            ["task", "create", "-t", "Task", "-fr", "Functional", "-tr", "Technical", "-ac", "Criteria"],
            check=False
        )
        assert exit_code == 3, "Should fail with exit code 3 (no roadmap selected)"

        print("✓ No roadmap selected test passed")

    def test_boundary_priority_values(self):
        """Test boundary priority values (0 and 9)."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(roadmap, "Task", "Functional", "Technical", "Criteria")

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
        task_id = self.test.create_task(roadmap, "Task", "Functional", "Technical", "Criteria")

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
        """Bulk prio with a mixed valid/invalid batch must fail-fast (finding #45).

        Per SPEC/COMMANDS.md § Change Priority, any unknown ID in the batch must
        fail with exit 4 BEFORE any mutation: valid tasks must be left unchanged
        and no phantom audit rows written. Previously the valid tasks were
        silently mutated and the command exited 0.
        """
        roadmap = self.test.create_roadmap()

        task1 = self.test.create_task(roadmap, "Task 1", "Functional 1", "Technical 1", "Criteria 1")
        task2 = self.test.create_task(roadmap, "Task 2", "Functional 2", "Technical 2", "Criteria 2")

        # Set priority with one invalid ID - whole batch must fail-fast (exit 4).
        exit_code, _, stderr = self.test.run_cmd(
            ["task", "prio", "-r", roadmap, f"{task1},99999,{task2}", "5"],
            check=False,
        )
        assert exit_code == 4, f"mixed batch must fail with exit 4, got {exit_code}"
        assert "not found" in stderr.lower(), f"expected 'not found' message, got: {stderr}"

        # Verify the valid tasks were NOT mutated (still default priority 0).
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task1)])
        assert result[0]["priority"] == 0, "valid task must not be mutated on fail-fast"
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task2)])
        assert result[0]["priority"] == 0, "valid task must not be mutated on fail-fast"

        print("✓ Bulk prio with mixed valid/invalid IDs fail-fasts (exit 4), no mutation")

    def test_remove_already_removed_task(self):
        """Test removing an already removed task fails."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(roadmap, "Task", "Functional", "Technical", "Criteria")

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
        """Editing a task with no fields is a successful no-op (finding #48).

        Per SPEC/COMMANDS.md § Edit Task, "If no fields are specified, command
        succeeds with no changes (exit code 0)" and produces no output.
        """
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(roadmap, "Task", "Functional", "Technical", "Criteria")

        exit_code, stdout, stderr = self.test.run_cmd(
            ["task", "edit", "-r", roadmap, str(task_id)],
            check=False,
        )
        assert exit_code == 0, f"edit with no fields must be a no-op (exit 0); got {exit_code}, stderr={stderr}"
        assert stdout.strip() == "", f"no-op edit must produce no stdout; got {stdout!r}"

        print("✓ Edit with no changes is a successful no-op (exit 0, no output)")

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

    def test_sprint_add_tasks_reports_the_step_the_spec_puts_first(self):
        """`sprint add-tasks` must refuse with the code of the step that runs first.

        SPEC/COMMANDS.md, section Task Assignment, publishes a validation order.
        Two of its steps can both fail on one command line: the task-id lexical
        step, which refuses a token that is not a positive integer with exit 2,
        and the sprint-existence step, which refuses an unresolvable sprint with
        exit 4. Naming an absent sprint AND a malformed task id puts both on the
        table, and the exit code says which one ran.

        The expected code is derived from the published list, not typed in, so
        this test and the Go gate that drives the same pair cannot drift apart:
        editing the document moves both.
        """
        order = _published_validation_order()
        first = _step_that_runs_first(order, "task-id lexical", "sprint existence")
        expected = 2 if first == "task-id lexical" else 4

        roadmap = self.test.create_roadmap()

        # An id past every sprint the roadmap holds, confirmed absent rather
        # than assumed free.
        existing = [sprint["id"] for sprint in self.test.list_sprints(roadmap)]
        absent_sprint = max(existing, default=0) + 1000
        assert absent_sprint not in existing, (
            f"sprint {absent_sprint} exists in {roadmap}, and this test needs it absent"
        )

        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", roadmap, str(absent_sprint), "abc"],
            check=False,
        )
        assert exit_code == expected, (
            f"SPEC/COMMANDS.md, section Task Assignment, puts the {first} step first, so "
            f"`sprint add-tasks {absent_sprint} abc` must exit {expected}; got {exit_code}, "
            f"stderr={stderr!r}"
        )
        if expected == 2:
            assert 'invalid task ID: "abc"' in stderr, (
                f"the lexical step runs first, so the refusal must name the malformed token; "
                f"got {stderr!r}"
            )
        else:
            assert f"sprint {absent_sprint}" in stderr, (
                f"the existence step runs first, so the refusal must name the sprint; "
                f"got {stderr!r}"
            )

        # Control. Without it, the assertion above would also pass if the sprint
        # lookup simply never refused this id -- there would then be no
        # suppressed failure and no ordering on show. Driving the same absent
        # sprint with a well-formed task id proves the second failure was real.
        task_id = self.test.create_task(
            roadmap,
            "Replay the acquirer batch against the ledger",
            "Every settlement batch reconciles to the cent before the ledger closes.",
            "Replay the batch against the acquirer file and report the first divergence.",
            "A deliberately corrupted batch is reported, not silently accepted.",
        )
        exit_code, _, stderr = self.test.run_cmd(
            ["sprint", "add-tasks", "-r", roadmap, str(absent_sprint), str(task_id)],
            check=False,
        )
        assert exit_code == 4, (
            f"control: the sprint-existence step must refuse sprint {absent_sprint} on its own "
            f"with exit 4; got {exit_code}, stderr={stderr!r}"
        )
        assert f"sprint {absent_sprint}" in stderr, (
            f"control: the refusal must name the sprint; got {stderr!r}"
        )

        print("\u2713 sprint add-tasks reports the validation step SPEC/COMMANDS.md puts first")

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
