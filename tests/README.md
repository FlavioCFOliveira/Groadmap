# Groadmap CLI Test Suite

Comprehensive end-to-end test suite for the Groadmap CLI tool.

## Overview

This test suite validates all CLI functionality by invoking the actual `rmp` binary and verifying its behavior. Tests are written in Python and organized by functional area.

## Test Structure

| Test File | Description |
|-----------|-------------|
| `test_01_basic_crud.py` | Basic create, read, update, delete operations for roadmaps and tasks |
| `test_02_sprint_lifecycle.py` | Sprint creation, start, close, reopen workflows |
| `test_03_task_state_machine.py` | Task status transitions following the state machine rules |
| `test_04_sprint_task_management.py` | Adding, removing, and moving tasks between sprints |
| `test_05_audit_reporting.py` | Audit log functionality and reporting features |
| `test_06_edge_cases_errors.py` | Error handling, boundary values, and edge cases |
| `test_07_backup_export_import.py` | Backup/restore and export/import functionality |
| `test_08_complex_workflow.py` | Realistic multi-sprint development scenarios |
| `test_09_stress_load.py` | Performance tests with 10,000+ tasks and 200+ sprints |

## Running Tests

### Run All Tests

```bash
python tests/run_tests.py
```

### Run Stress Tests (Large Data Volumes)

```bash
# Run only stress tests (10,000+ tasks, 200+ sprints)
python tests/run_tests.py --stress

# Run all tests including stress tests
python tests/run_tests.py --all
```

### Run Individual Test File

```bash
python tests/test_01_basic_crud.py
python tests/test_03_task_state_machine.py
python tests/test_09_stress_load.py  # Stress tests
```

### Run with pytest (optional)

```bash
pip install pytest
pytest tests/ -v
```

## Test Design

### Isolation
- Each test runs in a temporary directory with isolated HOME
- Roadmaps are created with unique names to avoid conflicts
- Cleanup happens automatically after each test

### CLI Invocation
- Tests invoke the actual `rmp` binary via subprocess
- Environment variables (HOME) are controlled for isolation
- Exit codes and output are validated

### Data Validation
- JSON output is parsed and validated
- Task/sprint statuses are verified after operations
- Audit logs are checked for expected entries

## Test Scenarios Covered

### Roadmap Management
- Create, list, use, and remove roadmaps
- Duplicate name handling
- Default roadmap selection

### Task Management
- Create tasks with all fields
- Edit task properties
- Set priority (0-9) and severity (0-9)
- Remove tasks (single and bulk)
- List with filters (status, priority, severity, limit)

### Sprint Management
- Create sprints with descriptions
- Lifecycle: PENDING → OPEN → CLOSED → OPEN
- Update sprint descriptions
- Remove sprints
- Statistics calculation

### Task State Machine
- Valid transitions: BACKLOG ↔ SPRINT ↔ DOING ↔ TESTING → COMPLETED → BACKLOG
- Invalid transitions are rejected
- Bulk status changes
- Completed tasks can be reopened

### Sprint Task Management
- Add tasks to sprint (status changes to SPRINT)
- Remove tasks from sprint (status returns to BACKLOG)
- Move tasks between sprints
- Filter sprint tasks by status
- Sprint statistics with mixed progress

### Audit and Reporting
- Audit entries created for all operations
- Filter audit log by operation, entity type, entity ID, date range
- Entity history tracking
- Audit statistics

### Edge Cases
- Missing required parameters
- Invalid IDs (non-numeric, zero, negative)
- Invalid priority/severity values
- Operations on non-existent resources
- Empty inputs
- Help commands

### Backup/Export/Import
- Create and list backups
- Restore from backup
- Export roadmap to JSON
- Export with audit log
- Import roadmap
- Import with new name
- Duplicate prevention

### Complex Workflows
- Full development cycle with 4 sprints
- Parallel sprint management
- Task reassignment during sprints
- Reopening completed tasks
- Mixed task progress in sprint
- Finding next pending task
- Open vs closed task identification

### Stress Tests (test_09_stress_load.py)
- **10,000 tasks creation**: Tests bulk creation performance (~160 tasks/s)
- **200 sprints creation**: Tests sprint scalability
- **Task distribution**: 1000 tasks across 50 sprints
- **Bulk status updates**: Update 1000 tasks simultaneously
- **Filter performance**: Query 5000 tasks with various filters
- **Sprint statistics**: Calculate stats for 500 tasks
- **Concurrent operations**: 20 sprints with 50 tasks each
- **Audit log volume**: 500+ audit entries
- **Memory efficiency**: List operations on 2000+ tasks
- **Mixed entities**: Roadmap with 1000 tasks and 50 sprints

## Exit Codes

Tests validate the following exit codes:
- `0` - Success
- `2` - Misuse (invalid arguments)
- `3` - No roadmap selected
- `4` - Resource not found
- `5` - Resource already exists
- `6` - Invalid data

## Requirements

- Python 3.7+
- Groadmap CLI binary (`./bin/rmp` or built from source)
- Go toolchain (to build CLI if binary not found)

## Adding New Tests

1. Create a new file `test_XX_description.py`
2. Inherit from the base test class pattern
3. Implement `setup_method()` and `teardown_method()`
4. Write test methods starting with `test_`
5. Add the module to `TEST_MODULES` in `run_tests.py`

Example:

```python
def test_my_feature(self):
    """Test description."""
    roadmap = self.test.create_roadmap()
    # ... test code ...
    print("✓ My feature test passed")
```
