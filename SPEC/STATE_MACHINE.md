# Task State Machine

This document describes the state machine for Task entities in Groadmap.

## States

Tasks can be in one of the following states:

| State | Description |
|-------|-------------|
| `BACKLOG` | Task is in the backlog, not yet assigned to a sprint |
| `SPRINT` | Task is assigned to an active sprint (set automatically when added to sprint) |
| `DOING` | Task is currently being worked on |
| `TESTING` | Task is in testing/QA phase |
| `COMPLETED` | Task has been completed |

## State Diagram

```
                    ┌──────────┐
         ┌─────────│ COMPLETED│◄────────────────┐
         │         └────┬─────┘                 │
         │              │                        │
         │         (backlog)                    │
         │              │                        │
         │              ▼                        │
┌────────┴─┐      ┌─────────┐    (sprint)   ┌──┴───────┐
│  SPRINT  │◄─────│ BACKLOG │──────────────►│  SPRINT  │
│          │─────►│         │               │          │
└────┬─────┘      └─────────┘               └────┬─────┘
     │                                           │
     │ (doing)                                   │ (backlog)
     │                                           │
     ▼                                           │
┌─────────┐    (sprint)                  ┌─────┘
│  DOING  │◄──────────────────────────────┘
│         │──────┐
└────┬────┘      │ (testing)
     │            ▼
     │      ┌───────────┐
     └─────►│  TESTING  │
            │           │──────┐
            └─────┬─────┘      │ (completed)
                  │            ▼
                  │      ┌───────────┐
                  └─────│ COMPLETED │
                        └───────────┘
```

## Valid Transitions

| From State | Valid To States |
|------------|-----------------|
| `BACKLOG` | `SPRINT` |
| `SPRINT` | `BACKLOG`, `DOING` |
| `DOING` | `SPRINT`, `TESTING` |
| `TESTING` | `DOING`, `COMPLETED` |
| `COMPLETED` | `BACKLOG` |

## Transition Rules

### Manual vs Automatic Status Changes

| Transition Type | How Triggered | Command |
|-----------------|---------------|---------|
| **Automatic** | Status changed as side effect of sprint operations | `sprint add-tasks`, `sprint remove-tasks`, `sprint remove` |
| **Manual** | Status changed explicitly via task command | `task stat` |

### Automatic Transitions

| Transition | Trigger | Date Tracking Behavior |
|------------|---------|----------------------|
| **BACKLOG → SPRINT** | Task added to sprint via `sprint add-tasks` | No date changes |
| **SPRINT → BACKLOG** | Task removed from sprint via `sprint remove-tasks` OR sprint deleted via `sprint remove` | No date changes |

### Manual Transitions

| Transition | Description | Date Tracking Behavior |
|------------|-------------|----------------------|
| **SPRINT → DOING** | Work begins on the task | Set `started_at` to current timestamp |
| **DOING → SPRINT** | Task is paused/returned to sprint | No date changes |
| **DOING → TESTING** | Task is ready for testing | Set `tested_at` to current timestamp |
| **TESTING → DOING** | Testing failed, return to development | No date changes |
| **TESTING → COMPLETED** | Testing passed, task is complete | Set `closed_at` to current timestamp; optionally set `completion_summary` |
| **COMPLETED → BACKLOG** | Task is reopened for rework | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary` to NULL |

### Sub-task Hierarchy Guard

When transitioning any task to **COMPLETED**, the system checks whether the task has any direct subtasks (`parent_task_id` references) that are not in `COMPLETED` status. If any incomplete subtasks are found, the transition is rejected with an error listing the blocking subtask IDs.

| Scenario | Error |
|----------|-------|
| Task has incomplete subtasks | `Error: cannot mark task #N as COMPLETED: incomplete subtasks: #A, #B` |

### Dependency Guard

When transitioning any task to **COMPLETED**, the system also checks whether the task has any declared dependencies (rows in `task_dependencies` where `task_id = N`) that are not in `COMPLETED` status. If any incomplete dependencies are found, the transition is rejected with an error listing the blocking dependency IDs.

The sub-task hierarchy guard is evaluated first; if no subtask violations are found, the dependency guard is evaluated.

| Scenario | Error |
|----------|-------|
| Task has incomplete dependencies | `Error: cannot mark task #N as COMPLETED: incomplete dependencies: #A, #B` |

## Date Tracking Fields

### Lifecycle Tracking

The following fields track the task lifecycle and are managed automatically by the application:

| Field | Set On | Description |
|-------|--------|-------------|
| `created_at` | Task creation | Initial timestamp when task is created |
| `started_at` | SPRINT → DOING transition | When work begins on the task |
| `tested_at` | DOING → TESTING transition | When task enters testing phase |
| `closed_at` | TESTING → COMPLETED transition | When task is marked complete |
| `completion_summary` | TESTING → COMPLETED transition (optional) | Summary of work done during development; provided via `--summary` flag; NULL if not supplied |

### Rules

1. **created_at**: Set once on task creation, never changes
2. **started_at**: Set on first transition to DOING, cleared on COMPLETED → BACKLOG
3. **tested_at**: Set on first transition to TESTING, cleared on COMPLETED → BACKLOG
4. **closed_at**: Set on transition to COMPLETED, cleared on COMPLETED → BACKLOG
5. **completion_summary**: Optionally set on TESTING → COMPLETED transition via `--summary` flag; cleared on COMPLETED → BACKLOG; cannot be set on any other transition

### Reopening Behavior

When a task is reopened (COMPLETED → BACKLOG):
- All lifecycle dates (`started_at`, `tested_at`, `closed_at`) are reset to NULL
- `completion_summary` is reset to NULL
- `created_at` is preserved (original creation time)
- This allows the task to go through the full lifecycle again

### Date Format

All timestamps follow ISO 8601 UTC format: `YYYY-MM-DDTHH:MM:SS.000Z`

## Implementation

The state machine is implemented in `internal/models/task.go`:

- `CanTransitionTo(newStatus TaskStatus) bool`: Checks if a transition is valid
- `ValidateStatusTransition(current, new string) error`: Validates transition with detailed error
- `GetValidTransitions(status TaskStatus) []TaskStatus`: Returns valid next states

## Error Handling

When an invalid transition is attempted, the system returns an error:

```go
if !currentStatus.CanTransitionTo(newStatus) {
    return fmt.Errorf("cannot transition from %q to %q", currentStatus, newStatus)
}
```

## Design Rationale

The state machine is designed to:

1. **Prevent invalid workflows**: Tasks must follow a logical progression
2. **Support agile practices**: Tasks can move back (e.g., from TESTING to DOING)
3. **Enable reopening**: Completed tasks can be reopened to BACKLOG
4. **Maintain clarity**: Each state has a clear meaning and purpose

## Future Considerations

Potential future enhancements:

- Add `BLOCKED` state for tasks that are blocked by dependencies
- Add `REVIEW` state for code review phase
- Add transition hooks (e.g., notifications on status change)
- Add time tracking per state
