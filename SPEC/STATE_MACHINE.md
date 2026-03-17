# Task State Machine

This document describes the state machine for Task entities in Groadmap.

## States

Tasks can be in one of the following states:

| State | Description |
|-------|-------------|
| `BACKLOG` | Task is in the backlog, not yet assigned to a sprint |
| `SPRINT` | Task is assigned to an active sprint |
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

1. **BACKLOG → SPRINT**: Task is assigned to a sprint
2. **SPRINT → BACKLOG**: Task is removed from sprint (back to backlog)
3. **SPRINT → DOING**: Work begins on the task
4. **DOING → SPRINT**: Task is paused/returned to sprint
5. **DOING → TESTING**: Task is ready for testing
6. **TESTING → DOING**: Testing failed, return to development
7. **TESTING → COMPLETED**: Testing passed, task is complete
8. **COMPLETED → BACKLOG**: Task is reopened (e.g., for rework)

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
