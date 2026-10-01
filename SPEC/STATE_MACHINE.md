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
  - [Commit Tracking Fields](#commit-tracking-fields)
  - [Implementation](#implementation)
  - [Error Handling](#error-handling)
  - [Design Rationale](#design-rationale)
- [Sprint State Machine](#sprint-state-machine)

## Overview

This document defines the state machines for entities that progress through discrete lifecycle states in Groadmap. It covers both Task entities (BACKLOG, SPRINT, DOING, TESTING, COMPLETED) and Sprint entities (PENDING, OPEN, CLOSED). Each state machine specifies the legal transitions, the side effects on tracking fields, the values a transition requires the caller to supply, and the conditions under which a transition is rejected.

Every error string this document publishes is the complete line the user reads on stderr, following the convention stated in `COMMANDS.md § Published Error Strings Are Exact`.

## Task State Machine

### States

Tasks can be in one of the following states:

| State | Description |
|-------|-------------|
| `BACKLOG` | Task is in the backlog. A `BACKLOG` task belongs to no sprint; see Section "Sprint Membership and the BACKLOG Status" |
| `SPRINT` | Task is a member of a sprint and work on it has not started (set automatically when a `BACKLOG` task joins a sprint, and by `task reopen`) |
| `DOING` | Task is currently being worked on |
| `TESTING` | Task is in testing/QA phase |
| `COMPLETED` | Task has been completed. It stays in the sprint it was completed in |

### State Diagram

```
                +-----------+
                |  BACKLOG  |<--------------------------+
                +-----+-----+                           |
                      |                                 | sprint remove-tasks
        sprint add-   |  (automatic)                    | or sprint remove
        tasks         v                                 | (automatic)
                +-----------+                           |
           +--->|  SPRINT   |---------------------------+
           |    +-----+-----+
           |          |
           |          |  task stat DOING
           |          v
           |    +-----------+
           |    |   DOING   |<---------+
           |    +-----+-----+          |
           |          |                |
           |          |  task stat     |  task stat
           |          |  TESTING       |  DOING
           |          v                |
           |    +-----------+          |
           |    |  TESTING  |----------+
           |    +-----+-----+
           |          |
           |          |  task stat COMPLETED
           |          v
           |    +-----------+
           +----| COMPLETED |
    task reopen +-----------+
```

Legend: arrows labelled with the command that triggers the transition. Transitions marked `(automatic)` are side effects of a sprint command and are not user-callable via `task stat`; see Section "Valid Transitions" for the full rule set. For readability the diagram omits two sets of edges: the `task reopen` edges from `DOING` and `TESTING` to `SPRINT` (`task reopen` returns a task to `SPRINT` in its sprint from `DOING`, `TESTING` and `COMPLETED`), and the `sprint remove-tasks` and `sprint remove` edges from `DOING` and `TESTING` to `BACKLOG` (both sprint operations take a `SPRINT`, `DOING` or `TESTING` member out of its sprint and set it to `BACKLOG`). No edge leads from `COMPLETED` to `BACKLOG`: a `COMPLETED` task stays in its sprint, and no command sets its status to `BACKLOG`.

The diagram labels each edge with the command alone and omits the flags that command requires. Two edges into `DOING` (`task stat DOING`, from `SPRINT` and from `TESTING`) require `--commit-open`, and the edge into `COMPLETED` requires `--commit-close`; see Section "Commit Tracking Fields".

The diagram shows status changes. Sprint membership, which the `sprint_tasks` table records separately, follows the status by the invariant of Section "Sprint Membership and the BACKLOG Status": every status but `BACKLOG` is the status of a sprint member, and `BACKLOG` is the status of a task that belongs to no sprint.

### Valid Transitions

| From State | Valid To States | How |
|------------|-----------------|-----|
| `BACKLOG` | `SPRINT` | Automatic only: `sprint add-tasks`, when the task joins a sprint |
| `SPRINT` | `DOING`, `BACKLOG` | `DOING` is manual (via `task stat`, which requires `--commit-open`); `BACKLOG` is automatic only, when the task leaves its sprint (via `sprint remove-tasks` or `sprint remove`) |
| `DOING` | `TESTING`, `SPRINT`, `BACKLOG` | `TESTING` is manual (via `task stat`); `SPRINT` is manual (via `task reopen`), and the task stays in its sprint; `BACKLOG` is automatic only, when the task leaves its sprint (via `sprint remove-tasks` or `sprint remove`) |
| `TESTING` | `DOING`, `COMPLETED`, `SPRINT`, `BACKLOG` | `DOING` and `COMPLETED` are manual (via `task stat`; `DOING` requires `--commit-open`, `COMPLETED` requires `--commit-close` and accepts optional `--summary`); `SPRINT` is manual (via `task reopen`), and the task stays in its sprint; `BACKLOG` is automatic only, when the task leaves its sprint (via `sprint remove-tasks` or `sprint remove`) |
| `COMPLETED` | `SPRINT` | Manual only (via `task reopen`); the task stays in the sprint it was completed in. Clears `started_at`, `tested_at`, `closed_at`, `completion_summary` and `commit_close`, and preserves `commit_open` |

**Changing sprint keeps the status.** `sprint add-tasks` naming a task that belongs to another sprint, and `sprint move-tasks`, move a `SPRINT`, `DOING` or `TESTING` task to another sprint and leave its status as it was. The only status a sprint command sets on a task that joins a sprint is `SPRINT`, and only on a task that was in `BACKLOG`, which is the one status a task that belongs to no sprint holds.

**Rejection rule:** Manual `task stat <ids> SPRINT` is rejected with exit code 6 from any source state. The `SPRINT` status is set only by `sprint add-tasks`, when a `BACKLOG` task joins a sprint, and by `task reopen`, which returns a task to the start of the lifecycle inside its sprint. In particular, `task stat` cannot perform the `DOING → SPRINT` transition; `task reopen` performs it.

**`task stat` BACKLOG target rule:** `task stat <ids> BACKLOG` is rejected with exit code 6 for every task that is a member of a sprint, because a sprint member is never in `BACKLOG` status and `task stat` never changes membership. The refusal names the command that fits the source state:

| Source state | stderr Output |
|--------------|---------------|
| `SPRINT`, `DOING` or `TESTING` | `Error: validation error: invalid status transition from X to BACKLOG for task N: a task leaves its sprint only through 'rmp sprint remove-tasks'` |
| `COMPLETED` | `Error: validation error: invalid status transition from COMPLETED to BACKLOG for task N: a completed task is reopened with 'rmp task reopen'` |

`X` is the task's current status. `COMPLETED` has a line of its own because a completed task cannot be removed from its sprint (Section "Sprint Membership and the BACKLOG Status", rule 4); `task reopen` returns it to `SPRINT`, after which `sprint remove-tasks` can take it out.

**`task reopen`:** The `task reopen` command is a manual transition distinct from `task stat` and from the automatic transitions of the sprint commands. It returns a task from `DOING`, `TESTING` or `COMPLETED` to `SPRINT`, and the task stays in its sprint at the `position` it holds. It clears all lifecycle timestamps (`started_at`, `tested_at`, `closed_at`), `completion_summary`, and `commit_close` to NULL, and preserves `commit_open` (see Section "Commit Tracking Fields"). It never touches the `sprint_tasks` table. It is refused, with exit code 6 and no change, while the task's sprint is `CLOSED`, because a closed sprint takes no work back: `Error: validation error: cannot reopen task N: sprint #M is CLOSED; reopen the sprint first with 'rmp sprint reopen'`, where `N` is the first such task in the order the command line supplied them and `M` its sprint; `sprint reopen` reopens the sprint first. Running `task reopen` on a task that is already in `SPRINT`, or in `BACKLOG`, changes nothing: the command reports the task on stderr, exits 0, and writes no audit entry. See `COMMANDS.md § Reopen Task`.

### Sprint Membership and the BACKLOG Status

Sprint membership and task status are two facts stored in two places. Membership
is a row in the `sprint_tasks` junction table (see
`DATABASE.md § sprint_tasks Table (1:N Relationship)`); status is the
`tasks.status` column. No column on the `tasks` table records the sprint a task
belongs to. The two are nevertheless bound by one invariant, and this section is
canonical for it.

**The membership invariant** has two halves, and no committed state of a roadmap
database breaks either:

1. **A sprint member is never in `BACKLOG`.** A member's status is always
   `SPRINT`, `DOING`, `TESTING` or `COMPLETED`, so a task in `BACKLOG` status
   belongs to no sprint. No `sprint_tasks` row names a task in `BACKLOG` status.
2. **An active task belongs to a sprint.** A task in `SPRINT`, `DOING` or
   `TESTING` status is a member of a sprint. No task in one of those three statuses
   is without a `sprint_tasks` row.

`COMPLETED` is the one status the two halves leave open: a completed task stays in
its sprint, and one without a sprint exists only in data written before these rules
(see below).

Every write path respects it:

1. **`task create`** creates a task in `BACKLOG` status and never with a sprint.
2. **`sprint add-tasks`** sets a named `BACKLOG` task to `SPRINT` as it joins the
   sprint, in the same transaction. A named `SPRINT`, `DOING` or `TESTING` task,
   whether it belongs to another sprint or already to this one, keeps its status.
3. **`sprint move-tasks`** changes the sprint of each named task and keeps its
   status. It never sets `BACKLOG`.
4. **A `COMPLETED` task is bound to the sprint it was completed in.**
   `sprint add-tasks` naming it, to any sprint including its own, `sprint move-tasks`
   naming it, and `sprint remove-tasks` naming it are each refused with exit code 6
   and one line, the same on all three commands:
   `Error: validation error: task N is COMPLETED in sprint #M; a completed task stays in the sprint it was completed in`.
   `N` is the task and `M` the sprint it belongs to; when several named tasks are
   `COMPLETED`, `N` is the first of them in the order the command line supplied
   them. Nothing is changed. `task reopen` returns such a task to `SPRINT` in the
   same sprint, after which the sprint commands accept it.
5. **`sprint remove-tasks`** takes each named task out of the sprint and sets it to
   `BACKLOG`, in one transaction. It is the only command that takes a single task
   out of its sprint and the only command, with `sprint remove`, that sets
   `BACKLOG`.
6. **`sprint remove`** takes every member out of the sprint and sets each to
   `BACKLOG` before the sprint row is deleted, in one transaction. It is refused
   with exit code 6, and changes nothing, when the sprint holds at least one
   `COMPLETED` task (`COMMANDS.md § Remove Sprint` publishes the line).
7. **`task reopen`** returns a `DOING`, `TESTING` or `COMPLETED` task to `SPRINT`
   and leaves it in its sprint. It is refused while that sprint is `CLOSED`.
8. **`task stat <ids> BACKLOG`** is refused for every sprint member (Section
   "Valid Transitions").

**Application code enforces both halves.** Every write path above goes through one
guard in `internal/db`, inside its transaction, which checks the resulting status
and membership of every task the write changed and refuses a violation of either
half before commit. The schema uses no trigger, for this or any other rule.
`DATABASE.md § Sprint Membership Invariant Enforcement` is canonical for the guard,
the write paths that call it, and how a violation is reported: it can only be
reached by a defect in a write path, never by bad input, and fails the command as a
database failure (exit code 1).

**A roadmap created before the invariant was enforced** may break either half:
members in `BACKLOG` status, or active tasks that belong to no sprint. The migration
that introduces the enforcement repairs both: a member in
`BACKLOG` becomes `SPRINT` in its sprint, and an active task outside every sprint
returns to `BACKLOG`, as removal from a sprint would return it
(`VERSION.md § Migration 1.15.0 → 1.16.0`).

**A `COMPLETED` task that belongs to no sprint** cannot be produced by any
command under these rules, because a task reaches `COMPLETED` only from `TESTING`,
and only a sprint member is in `TESTING`. Such a task can exist only in data
written before these rules. `sprint add-tasks` refuses it with exit code 6 and
`Error: validation error: task N is COMPLETED and belongs to no sprint; a completed task cannot join a sprint`,
and `task reopen` returns it to `BACKLOG`, the one status a task outside every
sprint may hold, clearing what it clears on every reopening.

**What the invariant makes of the readers.**

1. **Commands that read sprint membership and commands that read status agree.**
   `sprint tasks`, `sprint get`, `sprint list` and `sprint show` list a sprint's
   members, and none of them is in `BACKLOG` status. The `backlog` subcommands
   filter on `status == BACKLOG` alone, and every task they return belongs to no
   sprint.
2. **The capacity of a sprint counts `SPRINT`, `DOING` and `TESTING` members.**
   `sprint open-tasks` and the `max_tasks` capacity check both restrict themselves
   to those three statuses, so a `COMPLETED` member is neither returned by the first
   nor charged against the sprint's capacity by the second.
3. **A task outside every sprint is in `BACKLOG`.** Joining a sprint is the only
   way out of `BACKLOG`, and leaving a sprint is the only way into it.

The web sprint board presents a sprint's members by status (see
`WEB.md § Sprint Detail Sub-Template`); under the invariant, no member it
presents is in `BACKLOG` status.

### Task Deletion Precondition

A task may be removed (`task remove` / `task rm`) only while it is in `BACKLOG` status. Attempts to delete a task in any other status (`SPRINT`, `DOING`, `TESTING`, `COMPLETED`) are rejected with exit code 6 and the message `"Error: validation error: task #N cannot be deleted — status is X, must be BACKLOG"`. To delete a non-BACKLOG task, the caller MUST first take it out of its sprint, which returns it to `BACKLOG`: via `sprint remove-tasks` or `sprint remove` from `SPRINT`, `DOING` or `TESTING`. A `COMPLETED` task is first returned to `SPRINT` with `task reopen`, because no sprint command takes a `COMPLETED` task out of its sprint.

The precondition tests the status alone. Under the membership invariant (Section "Sprint Membership and the BACKLOG Status") a `BACKLOG` task belongs to no sprint, so a deletion never removes a `sprint_tasks` row.

A task with active subtasks cannot be removed either; the subtasks must be removed first.

This rule preserves the audit trail of work that progressed past `BACKLOG`. The constraint is enforced by the application layer; the SQLite DDL does not include a `CHECK` or trigger for this rule.

### Transition Rules

#### Manual vs Automatic Status Changes

| Transition Type | How Triggered | Command |
|-----------------|---------------|---------|
| **Automatic** | Status changed as side effect of sprint operations | `sprint add-tasks` (a `BACKLOG` task joining a sprint), `sprint remove-tasks`, `sprint remove` |
| **Manual** | Status changed explicitly via task command | `task stat`, `task reopen` |

#### Automatic Transitions

| Transition | Trigger | Tracking Field Behavior |
|------------|---------|----------------------|
| **BACKLOG → SPRINT** | A `BACKLOG` task joins a sprint via `sprint add-tasks` | No tracking field changes |
| **SPRINT → BACKLOG** | Task removed from sprint via `sprint remove-tasks` OR sprint deleted via `sprint remove` | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary`, `commit_close` to NULL; preserve `commit_open`. On this source state all five cleared fields are already NULL, so only `commit_open` can hold a value here, and it is untouched |
| **DOING → BACKLOG** | Task removed from sprint via `sprint remove-tasks` OR sprint deleted via `sprint remove` | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary`, `commit_close` to NULL; preserve `commit_open` |
| **TESTING → BACKLOG** | Task removed from sprint via `sprint remove-tasks` OR sprint deleted via `sprint remove` | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary`, `commit_close` to NULL; preserve `commit_open` |

Both sprint operations remove the task's `sprint_tasks` row and set `BACKLOG` in the same transaction, and the sprint membership guard checks the result before commit (`DATABASE.md § Sprint Membership Invariant Enforcement`). Neither operation reaches a `COMPLETED` task: `sprint remove-tasks` refuses one it names, and `sprint remove` refuses a sprint that holds one (Section "Sprint Membership and the BACKLOG Status", rules 4 and 6). Neither operation clears `commit_open`: a task detached from its sprint keeps the record of where its work started.

A task that changes sprint through `sprint add-tasks` or `sprint move-tasks` keeps its status and every tracking field; no automatic transition applies to it.

#### Manual Transitions

| Transition | Description | Tracking Field Behavior |
|------------|-------------|----------------------|
| **SPRINT → DOING** | Work begins on the task | Set `started_at` to current timestamp; set `commit_open` to the mandatory `--commit-open` value |
| **DOING → TESTING** | Task is ready for testing | Set `tested_at` to current timestamp; no commit field changes |
| **TESTING → DOING** | Testing failed, return to development | No date changes; set `commit_open` to the mandatory `--commit-open` value, replacing the value stored on the previous entry into `DOING` |
| **TESTING → COMPLETED** | Testing passed, task is complete | Set `closed_at` to current timestamp; set `commit_close` to the mandatory `--commit-close` value; optionally set `completion_summary` |
| **COMPLETED → SPRINT** (via `task reopen`) | Task is reopened for rework inside the sprint it was completed in | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary`, `commit_close` to NULL; preserve `commit_open`; keep the `sprint_tasks` association and its `position` |
| **DOING → SPRINT** (via `task reopen`) | In-progress task is reopened inside its sprint | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary`, `commit_close` to NULL; preserve `commit_open`; keep the `sprint_tasks` association and its `position` |
| **TESTING → SPRINT** (via `task reopen`) | In-testing task is reopened inside its sprint | Clear `started_at`, `tested_at`, `closed_at`, `completion_summary`, `commit_close` to NULL; preserve `commit_open`; keep the `sprint_tasks` association and its `position` |

#### Mandatory Values on Entry into DOING and COMPLETED

Two manual transitions require the caller to supply a value; the application
cannot produce it and does not try. A `task stat` invocation that omits the
required flag is rejected with exit code 6 and changes nothing, including in a
multi-ID invocation whose other IDs were valid.

| Transition | Required flag | Written to |
|------------|---------------|------------|
| **SPRINT → DOING** | `--commit-open` | `commit_open` |
| **TESTING → DOING** | `--commit-open` | `commit_open` |
| **TESTING → COMPLETED** | `--commit-close` | `commit_close` |

These are the only transitions that require a supplied value, and each flag is
rejected on every transition other than the ones listed here. `COMMANDS.md
§ Change Status (stat)` is canonical for the flags, their validation order, and
the exact error messages.

#### Sub-task Hierarchy Guard

When transitioning any task to **COMPLETED**, the system checks whether the task has any direct subtasks (`parent_task_id` references) that are not in `COMPLETED` status. If any incomplete subtasks are found, the transition is rejected with an error listing the blocking subtask IDs.

| Scenario | Error |
|----------|-------|
| Task has incomplete subtasks | `Error: validation error: cannot mark task #N as COMPLETED: incomplete subtasks: <id-list>` |

#### Dependency Guard

When transitioning any task to **COMPLETED**, the system also checks whether the task has any declared dependencies (rows in `task_dependencies` where `task_id = N`) that are not in `COMPLETED` status. If any incomplete dependencies are found, the transition is rejected with an error listing the blocking dependency IDs.

The sub-task hierarchy guard is evaluated first; if no subtask violations are found, the dependency guard is evaluated.

| Scenario | Error |
|----------|-------|
| Task has incomplete dependencies | `Error: validation error: cannot mark task #N as COMPLETED: incomplete dependencies: <id-list>` |

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
2. **started_at**: Set on first transition to DOING, cleared on every return to BACKLOG and on every reopening
3. **tested_at**: Set on first transition to TESTING, cleared on every return to BACKLOG and on every reopening
4. **closed_at**: Set on transition to COMPLETED, cleared on every return to BACKLOG and on every reopening
5. **completion_summary**: Optionally set on TESTING → COMPLETED transition via `--summary` flag; cleared on every return to BACKLOG and on every reopening; cannot be set on any other transition

"Every return to BACKLOG" covers the two routes that take a task out of its sprint: `sprint remove-tasks` and `sprint remove`. "Every reopening" is `task reopen`, which returns the task to `SPRINT` in its sprint. Each of the three writes NULL to the three timestamps and to `completion_summary`, whatever the source state. The same three also clear `commit_close` and preserve `commit_open`; see Section "Commit Tracking Fields". A change of sprint through `sprint add-tasks` or `sprint move-tasks` is neither, and clears nothing.

#### Reopening Behavior

A task is reopened by `task reopen <ids>`, and by no other command. The command is
valid from `DOING`, `TESTING` and `COMPLETED`, and returns the task to `SPRINT`
inside the sprint it belongs to:

- All lifecycle dates (`started_at`, `tested_at`, `closed_at`) are reset to NULL
- `completion_summary` is reset to NULL
- `commit_close` is reset to NULL
- `commit_open` is preserved
- `created_at` is preserved (original creation time)
- The `sprint_tasks` association and its `position` are kept
- This allows the task to go through the full lifecycle again, in the same sprint

On a task already in `SPRINT`, and on a task in `BACKLOG`, the command changes
nothing. `task stat <ids> BACKLOG` reopens nothing: it is refused for every sprint
member (Section "Valid Transitions"). A task leaves its sprint, and so returns to
`BACKLOG`, only through `sprint remove-tasks` or `sprint remove`.

#### Date Format

All timestamps follow ISO 8601 UTC format: `YYYY-MM-DDTHH:MM:SS.000Z`

### Commit Tracking Fields

Two fields record the git commits that bracket the work on a task, for
version-control traceability. They are the counterpart of the lifecycle timestamps
above: a timestamp says *when* a phase began or ended, a commit hash says *where in
the repository's history* it began or ended.

| Field | Set On | Description |
|-------|--------|-------------|
| `commit_open` | Every transition into `DOING` (`SPRINT → DOING` and `TESTING → DOING`) | The git commit the task was started from; supplied through the mandatory `--commit-open` flag |
| `commit_close` | The `TESTING → COMPLETED` transition | The git commit the task was concluded at; supplied through the mandatory `--commit-close` flag |

#### Rules

1. **The caller supplies both values.** Groadmap invokes no git command, reads no
   working directory, and inspects no repository. It validates the format of the
   supplied hash and stores it; it never derives, guesses, or verifies a hash
   against a repository. `MODELS.md § Task` (Commit Hash Constraint) is canonical
   for the format.
2. **`commit_open`**: mandatory on every transition into `DOING`. A transition into
   `DOING` without `--commit-open` is rejected with exit code 6 and changes nothing.
   On a re-entry into `DOING` from `TESTING`, the supplied value replaces the
   previous one; no history of earlier values is kept.
3. **`commit_close`**: mandatory on the transition into `COMPLETED`. A transition
   into `COMPLETED` without `--commit-close` is rejected with exit code 6 and
   changes nothing.
4. **`commit_close` is cleared on every return to BACKLOG and on every
   reopening**, by all three routes: `sprint remove-tasks`, `sprint remove`, and
   `task reopen`.
5. **`commit_open` is preserved on every return to BACKLOG and on every
   reopening**, by all three of those routes. No command clears it, and no command other than a transition into
   `DOING` writes it.
6. **Neither field can be set by any other command.** `task create` accepts neither
   value, because a task is created in `BACKLOG`. `task edit` cannot change either
   value. The state machine is the only writer.

#### The Asymmetry on Reopening

Rules 4 and 5 are deliberately asymmetric, and this is the one place where the
commit fields diverge from the lifecycle timestamps and `completion_summary`, all
of which a return to `BACKLOG` and a reopening clear without exception.

- The commit a task's work was **started from** is a fact about history. Reopening
  the task does not make that commit a different commit, and it does not make the
  earlier work stop having started there. Clearing `commit_open` would discard a
  true record in exchange for nothing.
- The commit a task was **concluded at** is a claim that the task was finished at
  that point. Reopening the task withdraws exactly that claim: the task is no
  longer concluded, so it has no conclusion point. Keeping `commit_close` would
  leave a stale assertion that contradicts the task's own status.

A task that has been through one or more cycles therefore carries the
`commit_open` of its most recent entry into `DOING`, and a `commit_close` that is
non-NULL only while the task is `COMPLETED`.

#### Reachable Combinations

| `commit_open` | `commit_close` | When |
|---------------|----------------|------|
| NULL | NULL | The task has not entered `DOING` since the columns were introduced. This covers every newly created task, and every task that was in `BACKLOG`, `SPRINT`, or `COMPLETED` when the columns were introduced |
| set | NULL | The task has entered `DOING` at least once since the columns were introduced, and is not currently `COMPLETED`. This is the state in `DOING`, in `TESTING`, in `SPRINT` after a reopening, and in `BACKLOG` after the task left its sprint |
| set | set | The task is `COMPLETED` and entered `DOING` at least once since the columns were introduced. Every task completed under the current rules is in this combination |
| NULL | set | The task is `COMPLETED`, and it was in `DOING` or in `TESTING` when the columns were introduced. It reached `COMPLETED` from `TESTING` without re-entering `DOING`, so it acquired a `commit_close` while its `commit_open` stayed NULL |

The last row is the reason neither field may be used as a proxy for the other. It
is the only combination the current rules cannot produce for a task that starts
its lifecycle under them, and it is one a task leaves permanently: as soon as such
a task is reopened, its `commit_close` is cleared and it becomes a
NULL/NULL task, and it can never return to the combination, because any later
completion must pass through `DOING` and so must supply a `commit_open`.

A task that was already `COMPLETED` when the columns were introduced carries NULL
in both, because no truthful value for either could be recovered (see
`VERSION.md § Migration 1.10.0 → 1.11.0`). Consumers MUST therefore treat both
fields as absent-capable in every status, and MUST NOT infer a task's status from
whether either value is set.

### Implementation

The state machine is implemented in `internal/models/task.go`:

- `CanTransitionTo(newStatus TaskStatus) bool`: Checks if a transition is valid
- `ValidateStatusTransition(current, new string) error`: Validates transition with detailed error
- `GetValidTransitions(status TaskStatus) []TaskStatus`: Returns valid next states

The table these functions read holds the transitions of `Valid Transitions` above
that change no sprint membership and do not go through `task reopen`: every
transition that table lists except those to `BACKLOG`, which only leaving a sprint
performs, and those to `SPRINT` that `task reopen` performs. It is
`BACKLOG → SPRINT`, `SPRINT → DOING`, `DOING → TESTING`, `TESTING → DOING` and
`TESTING → COMPLETED`, and `COMPLETED` has no target in it.

That table is declared once, in `internal/models/task.go`, and `CanTransitionTo`
and `GetValidTransitions` both read that one declaration; neither declares a table
of its own. The two therefore agree by construction: for every pair of statuses
`s` and `t`, `s.CanTransitionTo(t)` returns `true` exactly when `t` is a member of
`GetValidTransitions(s)`. A change to the transitions is made in the one
declaration and reaches both functions.

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
3. **Enable reopening**: Tasks in `DOING`, `TESTING` and `COMPLETED` can be reopened to `SPRINT` via `task reopen`, and stay in their sprint; only leaving a sprint returns a task to `BACKLOG`, so a sprint member is never in `BACKLOG`
4. **Maintain clarity**: Each state has a clear meaning and purpose
5. **Tie the lifecycle to version control**: The transitions that open and close the work require the caller to name the commit at which each happened, so a task's record states not only when the work ran but where in the repository's history it began and ended. Requiring the value at the transition, rather than offering an optional field to fill in later, is what makes the record complete for every task from this point on

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
   `"Error: validation error: sprint #N order cannot be changed — sprint is CLOSED"`. The constraint
   is enforced by the application layer; the SQLite DDL does not include a `CHECK`
   or trigger for this rule.
3. Reordering is a single-sprint operation. Changing one sprint's `order` does not
   cascade to other sprints; the caller chooses a free value. The unique index
   `idx_sprints_order` guarantees no two sprints ever share an `order` value (see
   `DATABASE.md § sprints Table`).
