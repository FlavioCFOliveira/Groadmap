# State Machine Specification

## Table of Contents

- [Overview](#overview)
- [Task State Machine](#task-state-machine)
  - [States](#states)
  - [State Diagram](#state-diagram)
  - [Valid Transitions](#valid-transitions)
  - [Sprint Membership and the BACKLOG Status](#sprint-membership-and-the-backlog-status)
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
| `BACKLOG` | Task is in the backlog. A `BACKLOG` task usually belongs to no sprint, but it can still be a member of one; see Section "Sprint Membership and the BACKLOG Status" |
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
        sprint add-   |  (automatic)                    | task stat BACKLOG
        tasks         v                                 | (or task reopen)
                +-----------+   sprint remove-tasks     |
                |  SPRINT   |---------------------------+
                +-----+-----+   or task stat BACKLOG    |
                      |                                 |
       task stat      |                                 |
       DOING          v                                 |
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
                +-----------+   task stat BACKLOG
                                (or task reopen)
```

Legend: arrows labelled with the command that triggers the transition. Transitions marked `(automatic)` are not user-callable via `task stat`; see Section "Valid Transitions" for the full rule set. The right-hand return-to-BACKLOG edge is reachable via `task stat <id> BACKLOG` only from `SPRINT` and `COMPLETED`; from `DOING` and `TESTING`, `task stat <id> BACKLOG` is rejected (exit code 6), and the only command that returns those states to `BACKLOG` is `task reopen`. For readability the diagram omits two sets of edges: the `task reopen` edges from `DOING` and `TESTING` (`task reopen` returns a task to `BACKLOG` from any non-BACKLOG state), and the `sprint remove-tasks` and `sprint remove` edges from `DOING`, `TESTING`, and `COMPLETED` (both sprint operations return every member task to `BACKLOG`, whatever its status).

The diagram shows status changes only. It does not show sprint membership, which the `sprint_tasks` table records separately: a task that reaches `BACKLOG` through `task stat <id> BACKLOG` stays a member of its sprint. See Section "Sprint Membership and the BACKLOG Status".

### Valid Transitions

| From State | Valid To States | How |
|------------|-----------------|-----|
| `BACKLOG` | `SPRINT` | Automatic only (via `sprint add-tasks`) |
| `SPRINT` | `BACKLOG`, `DOING` | `BACKLOG` is automatic (via `sprint remove-tasks` or `sprint remove`) or manual (via `task stat <ids> BACKLOG` or `task reopen`); `DOING` is manual (via `task stat`) |
| `DOING` | `TESTING`, `BACKLOG` | `TESTING` is manual (via `task stat`); `BACKLOG` is manual (via `task reopen`) |
| `TESTING` | `DOING`, `COMPLETED`, `BACKLOG` | `DOING` and `COMPLETED` are manual (via `task stat`; `COMPLETED` accepts optional `--summary`); `BACKLOG` is manual (via `task reopen`) |
| `COMPLETED` | `BACKLOG` | Manual (via `task stat` or `task reopen`); clears `completion_summary` |

**Rejection rule:** Manual `task stat <ids> SPRINT` is rejected with exit code 6 from any source state. The SPRINT status is set exclusively by `sprint add-tasks`, which atomically links the task to a sprint via the `sprint_tasks` table. In particular, the `DOING → SPRINT` transition is invalid: returning a task to its sprint after starting work is not supported via `task stat`.

**`task stat` BACKLOG target rule:** `task stat <ids> BACKLOG` is accepted only from the `SPRINT` and `COMPLETED` source states. From `DOING` and `TESTING`, `task stat <ids> BACKLOG` is rejected with exit code 6. The only command that returns a task to `BACKLOG` from `DOING` or `TESTING` is `task reopen` (see below). `task stat <ids> BACKLOG` never touches the `sprint_tasks` table: a task that belonged to a sprint before the transition still belongs to it afterwards. See Section "Sprint Membership and the BACKLOG Status".

**`task reopen`:** The `task reopen` command is a manual transition distinct from `task stat` and from the automatic `SPRINT → BACKLOG` side effect of sprint operations. It transitions a task from any non-BACKLOG state (`SPRINT`, `DOING`, `TESTING`, or `COMPLETED`) back to `BACKLOG`. It clears all lifecycle timestamps (`started_at`, `tested_at`, `closed_at`) and `completion_summary` to NULL. It removes the task's `sprint_tasks` association only when the source state is `SPRINT`, `DOING`, or `TESTING`; from the `COMPLETED` source state the association survives, and the task stays a member of its sprint. Running `task reopen` on a task that is already in `BACKLOG` changes nothing: the command reports the task on stderr, exits 0, and leaves any `sprint_tasks` association in place. See `COMMANDS.md § Reopen Task`.

### Sprint Membership and the BACKLOG Status

Sprint membership and task status are two independent facts. Membership is a row
in the `sprint_tasks` junction table (see `DATABASE.md § sprint_tasks Table (1:N Relationship)`);
status is the `tasks.status` column. No column on the `tasks` table records the
sprint a task belongs to.

1. **A `BACKLOG` task can be a member of a sprint.** The manual transition
   `task stat <ids> BACKLOG` from the `SPRINT` source state changes only
   `tasks.status`. The task's `sprint_tasks` row survives, so the task remains a
   member of its sprint while its status reads `BACKLOG`. The same state is
   reached by `task reopen` from the `COMPLETED` source state, which likewise
   leaves the `sprint_tasks` row in place.
2. **The `position` of a member task is preserved.** `task stat <ids> BACKLOG`
   does not change the `position` column of the task's `sprint_tasks` row and
   does not renumber the positions of the other member tasks. The task keeps its
   place in the sprint's planned execution order.
3. **Commands that read sprint membership still see the task.** `sprint tasks`
   returns it, `sprint get` lists it in `tasks` and counts it in `task_count`, and
   `sprint show` lists it in `task_order` and counts it in `summary.total_tasks`
   and `summary.pending`. Commands that select only the non-terminal in-sprint
   statuses do not see it: `sprint open-tasks` and the `max_tasks` capacity check
   both restrict themselves to the `SPRINT`, `DOING`, and `TESTING` statuses, so
   the task is neither returned by the first nor charged against the sprint's
   capacity by the second.
4. **Commands that list the backlog also list the task.** The `backlog`
   subcommands filter on `status == BACKLOG` alone, so they return the task even
   though it belongs to a sprint.
5. **Outgoing transitions are the ordinary `BACKLOG` ones.** Membership grants the
   task no extra transition. `task stat <ids> DOING` is rejected with exit code 6
   from `BACKLOG`, and `task stat <ids> SPRINT` is rejected from every source
   state. To resume work on the task, the caller runs `sprint add-tasks` again,
   which restores the `SPRINT` status and moves the task to the end of the
   sprint's position order.
6. **Detaching the task requires a sprint command.** `sprint remove-tasks` removes
   the `sprint_tasks` row of a `BACKLOG` member and `sprint remove` removes it with
   the sprint. `task reopen` does not detach a task that is already in `BACKLOG`.

The web sprint board depends on this state: its `WAITING` column presents the
sprint's `BACKLOG` and `SPRINT` member tasks together (see
`WEB.md § Sprint Detail Sub-Template`).

### Task Deletion Precondition

A task may be removed (`task remove` / `task rm`) only while it is in `BACKLOG` status. Attempts to delete a task in any other status (`SPRINT`, `DOING`, `TESTING`, `COMPLETED`) are rejected with exit code 6 and the message `"Error: task #N cannot be deleted — status is X, must be BACKLOG"`. To delete a non-BACKLOG task, the caller MUST first transition the task back to `BACKLOG`: via `sprint remove-tasks` or `sprint remove` from any of the four states, via `task stat <id> BACKLOG` from `SPRINT` or `COMPLETED`, or via `task reopen` from any of the four states.

The precondition tests the status alone. A task in `BACKLOG` status that is still a member of a sprint can be deleted, and the deletion removes its `sprint_tasks` row through the `ON DELETE CASCADE` on that table.

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
| **SPRINT → BACKLOG** | Task removed from sprint via `sprint remove-tasks` OR sprint deleted via `sprint remove` | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary` to NULL. On this source state all four are already NULL, so nothing changes |
| **DOING → BACKLOG** | Task removed from sprint via `sprint remove-tasks` OR sprint deleted via `sprint remove` | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary` to NULL |
| **TESTING → BACKLOG** | Task removed from sprint via `sprint remove-tasks` OR sprint deleted via `sprint remove` | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary` to NULL |
| **COMPLETED → BACKLOG** | Task removed from sprint via `sprint remove-tasks` OR sprint deleted via `sprint remove` | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary` to NULL |

Both sprint operations reset every member task they touch, whatever its status, and both remove the task's `sprint_tasks` row in the same transaction. Neither operation checks the task's status first, so a `COMPLETED` task returns to `BACKLOG` and loses its `completion_summary` along with the other member tasks.

#### Manual Transitions

| Transition | Description | Date Tracking Behavior |
|------------|-------------|----------------------|
| **SPRINT → DOING** | Work begins on the task | Set `started_at` to current timestamp |
| **DOING → TESTING** | Task is ready for testing | Set `tested_at` to current timestamp |
| **TESTING → DOING** | Testing failed, return to development | No date changes |
| **TESTING → COMPLETED** | Testing passed, task is complete | Set `closed_at` to current timestamp; optionally set `completion_summary` |
| **SPRINT → BACKLOG** (via `task stat`) | Task is returned to the backlog without starting work, while staying in its sprint | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary` to NULL (all four are already NULL on this source state); keep the `sprint_tasks` association and its `position` |
| **COMPLETED → BACKLOG** | Task is reopened for rework (via `task stat` or `task reopen`) | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary` to NULL; keep the `sprint_tasks` association and its `position` |
| **SPRINT → BACKLOG** (via `task reopen`) | Task is reopened from a sprint without starting work, and leaves the sprint | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary` to NULL; remove `sprint_tasks` association |
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
2. **started_at**: Set on first transition to DOING, cleared on every return to BACKLOG
3. **tested_at**: Set on first transition to TESTING, cleared on every return to BACKLOG
4. **closed_at**: Set on transition to COMPLETED, cleared on every return to BACKLOG
5. **completion_summary**: Optionally set on TESTING → COMPLETED transition via `--summary` flag; cleared on every return to BACKLOG; cannot be set on any other transition

"Every return to BACKLOG" covers all four routes: `task stat <ids> BACKLOG`, `task reopen`, `sprint remove-tasks`, and `sprint remove`. Each of them writes NULL to the three timestamps and to `completion_summary`, whatever the source state.

#### Reopening Behavior

A task is reopened to `BACKLOG` in one of two ways:
- `task stat <ids> BACKLOG`, valid only from `SPRINT` and `COMPLETED`. From `DOING` or `TESTING` this command is rejected with exit code 6.
- `task reopen <ids>`, valid from any non-BACKLOG state (`SPRINT`, `DOING`, `TESTING`, or `COMPLETED`). This is the only command that returns a `DOING` or `TESTING` task to `BACKLOG`.

In both cases:
- All lifecycle dates (`started_at`, `tested_at`, `closed_at`) are reset to NULL
- `completion_summary` is reset to NULL
- `created_at` is preserved (original creation time)
- This allows the task to go through the full lifecycle again

The two commands differ in what they do to sprint membership:

- `task stat <ids> BACKLOG` never touches the `sprint_tasks` table. A task that was a sprint member stays one, keeping its `position`.
- `task reopen` removes the `sprint_tasks` association when the source state is `SPRINT`, `DOING`, or `TESTING`, detaching the task from its sprint. From the `COMPLETED` source state it leaves the association in place, so the task stays a member.

Section "Sprint Membership and the BACKLOG Status" describes the resulting state.

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
3. **Enable reopening**: Tasks in any non-BACKLOG state can be reopened to BACKLOG via `task reopen`; tasks in `SPRINT` and `COMPLETED` can also be returned to BACKLOG via `task stat`, which keeps them in their sprint
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
