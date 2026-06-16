# State Machine Specification

## Table of Contents

- [Overview](#overview)
- [Task State Machine](#task-state-machine)
  - [States](#states)
  - [State Diagram](#state-diagram)
  - [Valid Transitions](#valid-transitions)
  - [Task Deletion Precondition](#task-deletion-precondition)
  - [Transition Rules](#transition-rules)
  - [Date Tracking Fields](#date-tracking-fields)
  - [Implementation](#implementation)
  - [Error Handling](#error-handling)
  - [Design Rationale](#design-rationale)
- [Sprint State Machine](#sprint-state-machine)

## Overview

This document defines the state machines for entities that progress through discrete lifecycle states in Groadmap. It covers both Task entities (BACKLOG, SPRINT, DOING, TESTING, COMPLETED) and Sprint entities (PENDING, OPEN, CLOSED). Each state machine specifies the legal transitions, the side effects on tracking fields, and the conditions under which a transition is rejected.

## Task State Machine

### States

Tasks can be in one of the following states:

| State | Description |
|-------|-------------|
| `BACKLOG` | Task is in the backlog, not yet assigned to a sprint |
| `SPRINT` | Task is assigned to an active sprint (set automatically when added to sprint) |
| `DOING` | Task is currently being worked on |
| `TESTING` | Task is in testing/QA phase |
| `COMPLETED` | Task has been completed |

### State Diagram

```
                +-----------+
                |  BACKLOG  |<--------------------------+
                +-----+-----+                           |
                      |                                 |
        sprint add-   |  (automatic)                    |
        tasks         v                                 | task stat BACKLOG
                +-----------+   sprint remove-tasks     | (or task reopen)
                |  SPRINT   |---------------------------+
                +-----+-----+   (automatic)             |
                      |                                 |
       task stat      |     +---------------------------+
       DOING          |     |   task stat BACKLOG       |
                      v     |                           |
                +-----------+                           |
              +>|   DOING   |                           |
              | +-----+-----+                           |
              |       |                                 |
              |       |  task stat TESTING              |
   task stat  |       v                                 |
   DOING      | +-----------+                           |
              +-+  TESTING  |                           |
                +-----+-----+                           |
                      |                                 |
                      |  task stat COMPLETED            |
                      v                                 |
                +-----------+                           |
                | COMPLETED |---------------------------+
                +-----------+
```

Legend: arrows labelled with the command that triggers the transition. Transitions marked `(automatic)` are not user-callable via `task stat`; see Section "Valid Transitions" for the full rule set. For readability the diagram draws the `task reopen` return-to-BACKLOG edge only from `SPRINT` and `COMPLETED`; `task reopen` also returns a task to `BACKLOG` from `DOING` and `TESTING` (any non-BACKLOG state).

### Valid Transitions

| From State | Valid To States | How |
|------------|-----------------|-----|
| `BACKLOG` | `SPRINT` | Automatic only (via `sprint add-tasks`) |
| `SPRINT` | `BACKLOG`, `DOING` | `BACKLOG` is automatic (via `sprint remove-tasks` or `sprint remove`) or manual (via `task reopen`); `DOING` is manual (via `task stat`) |
| `DOING` | `TESTING`, `BACKLOG` | `TESTING` is manual (via `task stat`); `BACKLOG` is manual (via `task reopen`) |
| `TESTING` | `DOING`, `COMPLETED`, `BACKLOG` | `DOING` and `COMPLETED` are manual (via `task stat`; `COMPLETED` accepts optional `--summary`); `BACKLOG` is manual (via `task reopen`) |
| `COMPLETED` | `BACKLOG` | Manual (via `task stat` or `task reopen`); clears `completion_summary` |

**Rejection rule:** Manual `task stat <ids> SPRINT` is rejected with exit code 6 from any source state. The SPRINT status is set exclusively by `sprint add-tasks`, which atomically links the task to a sprint via the `sprint_tasks` table. In particular, the `DOING → SPRINT` transition is invalid: returning a task to its sprint after starting work is not supported via `task stat`.

**`task reopen`:** The `task reopen` command is a manual transition distinct from `task stat` and from the automatic `SPRINT → BACKLOG` side effect of sprint operations. It transitions a task from any non-BACKLOG state (`SPRINT`, `DOING`, `TESTING`, or `COMPLETED`) back to `BACKLOG`. It clears all lifecycle timestamps (`started_at`, `tested_at`, `closed_at`) and `completion_summary` to NULL, and removes the task's `sprint_tasks` association. See `COMMANDS.md § Reopen Task`.

### Task Deletion Precondition

A task may be removed (`task remove` / `task rm`) only while it is in `BACKLOG` status. Attempts to delete a task in any other status (`SPRINT`, `DOING`, `TESTING`, `COMPLETED`) are rejected with exit code 6 and the message `"Error: task #N cannot be deleted — status is X, must be BACKLOG"`. To delete a non-BACKLOG task, the caller MUST first transition the task back to `BACKLOG` (via `sprint remove-tasks` for `SPRINT`, or via `task stat <id> BACKLOG` from `SPRINT` or `COMPLETED`).

A task with active subtasks cannot be removed either; the subtasks must be removed first.

This rule preserves the audit trail of work that progressed past `BACKLOG`. The constraint is enforced by the application layer; the SQLite DDL does not include a `CHECK` or trigger for this rule.

### Transition Rules

#### Manual vs Automatic Status Changes

| Transition Type | How Triggered | Command |
|-----------------|---------------|---------|
| **Automatic** | Status changed as side effect of sprint operations | `sprint add-tasks`, `sprint remove-tasks`, `sprint remove` |
| **Manual** | Status changed explicitly via task command | `task stat`, `task reopen` |

#### Automatic Transitions

| Transition | Trigger | Date Tracking Behavior |
|------------|---------|----------------------|
| **BACKLOG → SPRINT** | Task added to sprint via `sprint add-tasks` | No date changes |
| **SPRINT → BACKLOG** | Task removed from sprint via `sprint remove-tasks` OR sprint deleted via `sprint remove` | No date changes |

#### Manual Transitions

| Transition | Description | Date Tracking Behavior |
|------------|-------------|----------------------|
| **SPRINT → DOING** | Work begins on the task | Set `started_at` to current timestamp |
| **DOING → TESTING** | Task is ready for testing | Set `tested_at` to current timestamp |
| **TESTING → DOING** | Testing failed, return to development | No date changes |
| **TESTING → COMPLETED** | Testing passed, task is complete | Set `closed_at` to current timestamp; optionally set `completion_summary` |
| **COMPLETED → BACKLOG** | Task is reopened for rework (via `task stat` or `task reopen`) | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary` to NULL |
| **SPRINT → BACKLOG** (via `task reopen`) | Task is reopened from a sprint without starting work | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary` to NULL; remove `sprint_tasks` association |
| **DOING → BACKLOG** (via `task reopen`) | In-progress task is reopened | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary` to NULL; remove `sprint_tasks` association |
| **TESTING → BACKLOG** (via `task reopen`) | In-testing task is reopened | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary` to NULL; remove `sprint_tasks` association |

#### Sub-task Hierarchy Guard

When transitioning any task to **COMPLETED**, the system checks whether the task has any direct subtasks (`parent_task_id` references) that are not in `COMPLETED` status. If any incomplete subtasks are found, the transition is rejected with an error listing the blocking subtask IDs.

| Scenario | Error |
|----------|-------|
| Task has incomplete subtasks | `Error: cannot mark task #N as COMPLETED: incomplete subtasks: #A, #B` |

#### Dependency Guard

When transitioning any task to **COMPLETED**, the system also checks whether the task has any declared dependencies (rows in `task_dependencies` where `task_id = N`) that are not in `COMPLETED` status. If any incomplete dependencies are found, the transition is rejected with an error listing the blocking dependency IDs.

The sub-task hierarchy guard is evaluated first; if no subtask violations are found, the dependency guard is evaluated.

| Scenario | Error |
|----------|-------|
| Task has incomplete dependencies | `Error: cannot mark task #N as COMPLETED: incomplete dependencies: #A, #B` |

### Date Tracking Fields

#### Lifecycle Tracking

The following fields track the task lifecycle and are managed automatically by the application:

| Field | Set On | Description |
|-------|--------|-------------|
| `created_at` | Task creation | Initial timestamp when task is created |
| `started_at` | SPRINT → DOING transition | When work begins on the task |
| `tested_at` | DOING → TESTING transition | When task enters testing phase |
| `closed_at` | TESTING → COMPLETED transition | When task is marked complete |
| `completion_summary` | TESTING → COMPLETED transition (optional) | Summary of work done during development; provided via `--summary` flag; NULL if not supplied |

#### Rules

1. **created_at**: Set once on task creation, never changes
2. **started_at**: Set on first transition to DOING, cleared on COMPLETED → BACKLOG
3. **tested_at**: Set on first transition to TESTING, cleared on COMPLETED → BACKLOG
4. **closed_at**: Set on transition to COMPLETED, cleared on COMPLETED → BACKLOG
5. **completion_summary**: Optionally set on TESTING → COMPLETED transition via `--summary` flag; cleared on COMPLETED → BACKLOG; cannot be set on any other transition

#### Reopening Behavior

A task is reopened to `BACKLOG` in one of two ways:
- `task stat <ids> BACKLOG`, valid from `COMPLETED` (and from `SPRINT`).
- `task reopen <ids>`, valid from any non-BACKLOG state (`SPRINT`, `DOING`, `TESTING`, or `COMPLETED`).

In both cases:
- All lifecycle dates (`started_at`, `tested_at`, `closed_at`) are reset to NULL
- `completion_summary` is reset to NULL
- `created_at` is preserved (original creation time)
- This allows the task to go through the full lifecycle again

In addition, `task reopen` removes the task's `sprint_tasks` association, fully detaching the task from any sprint.

#### Date Format

All timestamps follow ISO 8601 UTC format: `YYYY-MM-DDTHH:MM:SS.000Z`

### Implementation

The state machine is implemented in `internal/models/task.go`:

- `CanTransitionTo(newStatus TaskStatus) bool`: Checks if a transition is valid
- `ValidateStatusTransition(current, new string) error`: Validates transition with detailed error
- `GetValidTransitions(status TaskStatus) []TaskStatus`: Returns valid next states

### Error Handling

When an invalid transition is attempted, the system returns an error:

```go
if !currentStatus.CanTransitionTo(newStatus) {
    return fmt.Errorf("cannot transition from %q to %q", currentStatus, newStatus)
}
```

### Design Rationale

The state machine is designed to:

1. **Prevent invalid workflows**: Tasks must follow a logical progression
2. **Support agile practices**: Tasks can move back (e.g., from TESTING to DOING)
3. **Enable reopening**: Tasks in any non-BACKLOG state can be reopened to BACKLOG via `task reopen`; completed tasks can also be reopened via `task stat`
4. **Maintain clarity**: Each state has a clear meaning and purpose

## Sprint State Machine

Sprints follow a linear progression with reopening capability.

```
PENDING → OPEN → CLOSED
            ↑      │
            └──────┘ (reopen)
```

1. **PENDING**: Initial state upon creation.
2. **OPEN**: Active sprint (started via `rmp sprint start`).
3. **CLOSED**: Completed sprint (closed via `rmp sprint close`).
4. **REOPEN**: Moving from `CLOSED` back to `OPEN`.

### Sprint Order Immutability

Every sprint carries an `order` value (stored in the `order_index` column): a
positive integer (`> 0`), unique across the roadmap, that records the natural,
sequential execution order of sprints. The full definition of the field lives in
`MODELS.md § Sprint Field Constraints`; this section defines how its mutability
depends on the sprint lifecycle state.

| Sprint status | `order` mutable? | How |
|---------------|------------------|-----|
| `PENDING` | Yes | `sprint update --order <n>` |
| `OPEN` | Yes | `sprint update --order <n>` |
| `CLOSED` | No | Any attempt to change it is rejected with exit code 6 |

**Rules:**

1. While a sprint is `PENDING` or `OPEN`, its `order` can be changed via
   `sprint update --order <n>`. The new value MUST be a positive integer (`> 0`,
   rejected with exit code 6 otherwise) and MUST NOT collide with another
   sprint's `order` (a collision is rejected with exit code 5; see
   `COMMANDS.md § Update Sprint` and `DATABASE.md § Update Sprint Order`).
2. Once a sprint is `CLOSED`, its `order` becomes immutable: it permanently
   records the historical execution position of the sprint. Any attempt to change
   the `order` of a `CLOSED` sprint is rejected with exit code 6 and the message
   `"Error: sprint #N order cannot be changed — sprint is CLOSED"`. The constraint
   is enforced by the application layer; the SQLite DDL does not include a `CHECK`
   or trigger for this rule.
3. Reordering is a single-sprint operation. Changing one sprint's `order` does not
   cascade to other sprints; the caller chooses a free value. The unique index
   `idx_sprints_order` guarantees no two sprints ever share an `order` value (see
   `DATABASE.md § sprints Table`).
