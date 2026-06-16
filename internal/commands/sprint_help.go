// Package commands — per-subcommand help text for `rmp sprint`.
//
// Each printer is self-contained: usage line, required/optional flag
// split, output JSON shape, exit codes, and worked examples. Invoked
// from HandleSprint via sprintSubHelp() when --help is in argv.
package commands

import "fmt"

// printSprintListHelp — `rmp sprint list`.
func printSprintListHelp() {
	fmt.Print(`Usage: rmp sprint list -r <roadmap> [--status <state>]

Lists every sprint in the roadmap, optionally filtered by status.

Aliases: ls.

Required:
  -r, --roadmap <name>            Target roadmap

Optional:
  --status <state>                One of: PENDING, OPEN, CLOSED

Output (stdout JSON):
  Array of sprint objects. See 'rmp sprint --help' for the full key list.

Exit codes:
  0  Success
  3  Missing -r
  6  Invalid --status value

Examples:
  rmp sprint list -r myproject
  rmp sprint ls -r myproject --status OPEN
`)
}

// printSprintCreateHelp — `rmp sprint create`.
func printSprintCreateHelp() {
	fmt.Print(`Usage: rmp sprint create -r <roadmap> -t <title> -d <description> [--max-tasks <n>] [--order <n>]

Creates a new sprint in PENDING status. The sprint will not accept work
until it is moved to OPEN via 'rmp sprint start <id>'.

Sprints have no fixed end date. started_at is set when the sprint is
moved to OPEN; closed_at is set when it is moved to CLOSED. There is
no --start-date / --end-date flag.

Aliases: new.

Required:
  -r, --roadmap <name>            Target roadmap
  -t, --title <text>              Sprint title (max 255 chars)
  -d, --description <text>        Sprint description (max 2048 chars)

Optional:
  --max-tasks <n>                 Hard cap on active tasks (n >= 1). When set,
                                  'sprint add-tasks' refuses to push the active
                                  task count past <n>. Cannot be removed once
                                  set, only changed to another positive integer.
  --order <n>                     Sprint execution order: a positive integer
                                  (> 0), unique across the roadmap; the sprint
                                  with the lowest --order executes first. When
                                  omitted, the next available value (the highest
                                  existing order plus one; 1 for the first sprint)
                                  is auto-assigned. A value already used by
                                  another sprint is rejected (exit 5).

Output (stdout JSON):
  {"id": <new-sprint-id>}

Exit codes:
  0  Success
  2  Missing -t or -d
  3  Missing -r
  5  --order value already used by another sprint
  6  Title/description too long, --max-tasks <= 0, or --order not a positive integer

Examples:
  rmp sprint create -r myproject -t "Auth hardening" -d "Sprint 1 — Auth hardening"
  rmp sprint new -r myproject -t "Capacity sprint" -d "Capacity-bounded sprint" --max-tasks 12
  rmp sprint create -r myproject -t "Sprint 3" -d "Third sprint" --order 3
`)
}

// printSprintGetHelp — `rmp sprint get`.
func printSprintGetHelp() {
	fmt.Print(`Usage: rmp sprint get -r <roadmap> <sprint-id>

Returns the sprint object for <sprint-id>. For a richer summary with
counts and severity distribution, use 'rmp sprint show <sprint-id>'.
For per-status counts, burndown and velocity, use 'rmp sprint stats'.
For the task list, use 'rmp sprint tasks'.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Output (stdout JSON):
  Single sprint object (see 'rmp sprint --help').

Exit codes:
  0  Success
  3  Missing -r
  4  Sprint not found

Examples:
  rmp sprint get -r myproject 5
`)
}

// printSprintShowHelp — `rmp sprint show`.
func printSprintShowHelp() {
	fmt.Print(`Usage: rmp sprint show -r <roadmap> <sprint-id>

Returns a stand-up-style summary of a sprint: identification, status,
capacity, task-count totals split into pending / in-progress /
completed, the same totals as percentages, and per-severity /
per-criticality task distributions. The full task list is NOT included
— use 'rmp sprint tasks <id>' for that.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Output (stdout JSON):
  Flat object with these keys:
    sprint_id                int
    sprint_description       string
    status                   "PENDING" | "OPEN" | "CLOSED"
    max_tasks                int | null         (null = no capacity cap)
    capacity_pct             float | null       (current_load / max_tasks * 100; null when uncapped)
    current_load             int                (count of SPRINT/DOING/TESTING tasks)
    task_order               [int, ...]         (task ids in sprint position order)
    summary                  {total_tasks, pending, in_progress, completed}
                             - pending     = BACKLOG + SPRINT
                             - in_progress = DOING + TESTING
                             - completed   = COMPLETED
    progress                 {pending_percentage, in_progress_percentage, completed_percentage}
    severity_distribution    {"0-2": {count, percentage}, "3-5": ..., "6-7": ..., "8-9": ...}
    criticality_distribution {low: {count, percentage}, medium: ..., high: ..., critical: ...}

Exit codes:
  0  Success
  3  Missing -r
  4  Sprint not found

Examples:
  rmp sprint show -r myproject 5
`)
}

// printSprintUpdateHelp — `rmp sprint update`.
func printSprintUpdateHelp() {
	fmt.Print(`Usage: rmp sprint update -r <roadmap> <sprint-id> [-t <title>] [-d <text>] [--max-tasks <n>] [--order <n>]

Edits the title, description, capacity cap, or execution order of an
existing sprint. At least one of -t, -d, --max-tasks or --order must be
supplied.

The capacity cap can be changed to any positive integer; it cannot be
removed once set, and there is no validation against the sprint's
current active task count (an over-capacity cap simply blocks future
'sprint add-tasks').

The execution order (--order) can be changed only while the sprint is
PENDING or OPEN. Once the sprint is CLOSED, its order is immutable and
any change is rejected with exit code 6, because it then records the
historical execution position. The new value must be a positive integer
(> 0) and must not already be used by another sprint (a collision is
rejected with exit code 5).

Aliases: upd.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

At least one of:
  -t, --title <text>              New title (max 255 chars)
  -d, --description <text>        New description (max 2048 chars)
  --max-tasks <n>                 New capacity cap; must be >= 1
  --order <n>                     New execution order; positive integer (> 0),
                                  unique across the roadmap. Allowed only while
                                  PENDING or OPEN; immutable once CLOSED.

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  2  None of -t, -d, --max-tasks or --order given
  3  Missing -r
  4  Sprint not found
  5  --order value already used by another sprint
  6  --max-tasks < 1, title/description too long, --order not positive, or --order on a CLOSED sprint

Examples:
  rmp sprint update -r myproject 5 -t "Auth + observability"
  rmp sprint update -r myproject 5 -d "Sprint 1 — Auth + observability"
  rmp sprint upd -r myproject 5 --max-tasks 15
  rmp sprint update -r myproject 5 --order 2
`)
}

// printSprintRemoveHelp — `rmp sprint remove`.
func printSprintRemoveHelp() {
	fmt.Print(`Usage: rmp sprint remove -r <roadmap> <sprint-id>

Deletes the sprint. All member tasks — regardless of their current
status (SPRINT, DOING, TESTING, or even COMPLETED) — are reverted to
BACKLOG and their sprint association is cleared. The tasks themselves
are NOT deleted; their requirements, priority, severity, etc. are
preserved.

Removing a CLOSED sprint is allowed and follows the same cascade:
COMPLETED tasks in it are pulled back to BACKLOG. If that is not what
you want, leave the sprint CLOSED and create a new sprint instead.

Aliases: rm.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  Sprint not found

Examples:
  rmp sprint remove -r myproject 5
  rmp sprint rm -r myproject 9
`)
}

// printSprintStartHelp — `rmp sprint start`.
func printSprintStartHelp() {
	fmt.Print(`Usage: rmp sprint start -r <roadmap> <sprint-id>

Transitions a sprint from PENDING (or CLOSED, see 'reopen') to OPEN.
Only one sprint can be OPEN per roadmap at any time — enforced at the
database level via idx_one_open_sprint.

Side effect:
  Sets started_at to the current timestamp on the PENDING -> OPEN path.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  Sprint not found
  6  Current sprint state forbids starting (already OPEN, or another
     sprint is currently OPEN)

Examples:
  rmp sprint start -r myproject 5
`)
}

// printSprintCloseHelp — `rmp sprint close`.
func printSprintCloseHelp() {
	fmt.Print(`Usage: rmp sprint close -r <roadmap> <sprint-id> [--force]

Transitions an OPEN sprint to CLOSED. By default, the close is rejected
(exit 6) if any task in the sprint is still SPRINT, DOING, or TESTING —
use --force to close anyway with a warning on stderr.

Side effect:
  Sets closed_at to the current timestamp.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Optional:
  --force                         Close even when the sprint has active
                                  (SPRINT/DOING/TESTING) tasks.

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  Sprint not found
  6  Sprint not OPEN, or active tasks remain and --force was not given

Examples:
  rmp sprint close -r myproject 5
  rmp sprint close -r myproject 5 --force
`)
}

// printSprintReopenHelp — `rmp sprint reopen`.
func printSprintReopenHelp() {
	fmt.Print(`Usage: rmp sprint reopen -r <roadmap> <sprint-id>

Transitions a CLOSED sprint back to OPEN (e.g. when a follow-up
issue surfaces and you want to keep work attached to the same sprint).
Rejected if another sprint is already OPEN.

Side effects:
  - Clears closed_at (sets it to NULL).
  - started_at is preserved (the original sprint-start timestamp stays
    intact, so velocity calculations remain comparable).
  - Member tasks keep their current status (SPRINT/DOING/TESTING/COMPLETED);
    nothing is auto-reverted.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  Sprint not found
  6  Sprint not in CLOSED state, or another sprint is OPEN

Examples:
  rmp sprint reopen -r myproject 5
`)
}

// printSprintTasksHelp — `rmp sprint tasks`.
func printSprintTasksHelp() {
	fmt.Print(`Usage: rmp sprint tasks -r <roadmap> <sprint-id> [--order-by-priority]

Lists every task assigned to <sprint-id>, regardless of status (so
COMPLETED tasks are included). Use 'sprint open-tasks' to exclude
COMPLETED.

Default order is by sprint position ASC. With --order-by-priority,
the result is re-ordered priority DESC then position ASC.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Optional:
  --order-by-priority             Re-sort by priority DESC

Output (stdout JSON):
  Array of task objects.

Exit codes:
  0  Success
  3  Missing -r
  4  Sprint not found

Examples:
  rmp sprint tasks -r myproject 5
  rmp sprint tasks -r myproject 5 --order-by-priority
`)
}

// printSprintOpenTasksHelp — `rmp sprint open-tasks`.
func printSprintOpenTasksHelp() {
	fmt.Print(`Usage: rmp sprint open-tasks -r <roadmap> <sprint-id> [--order-by-priority]

Lists tasks assigned to <sprint-id> whose status is one of SPRINT,
DOING, or TESTING (i.e. all incomplete sprint work). Useful for
stand-ups and burndown.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Optional:
  --order-by-priority             Sort by priority DESC; otherwise sprint position

Output (stdout JSON):
  Array of task objects (excludes BACKLOG and COMPLETED).

Exit codes:
  0  Success
  3  Missing -r
  4  Sprint not found

Examples:
  rmp sprint open-tasks -r myproject 5
  rmp sprint open-tasks -r myproject 5 --order-by-priority
`)
}

// printSprintStatsHelp — `rmp sprint stats`.
func printSprintStatsHelp() {
	fmt.Print(`Usage: rmp sprint stats -r <roadmap> <sprint-id>

Returns the SprintStats object: per-status counts, completion
percentage, ordered task ids, burndown series (one entry per day),
velocity (tasks/day), and elapsed days when the sprint has been started.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Output (stdout JSON):
  {
    "sprint_id": <int>,
    "total_tasks": <int>,
    "completed_tasks": <int>,
    "progress_percentage": <0.0-100.0>,
    "status_distribution": {"BACKLOG": <int>, ...},
    "task_order": [<id>, ...],
    "burndown": [{"date": "YYYY-MM-DD", "tasks_remaining": <int>}, ...],
    "velocity": <float>,
    "days_elapsed": <int|null>,
    "days_remaining": <int|null>
  }

Notes for callers:
  - velocity is 0.0 for OPEN and PENDING sprints, and for CLOSED sprints
    with zero completed tasks. Only meaningful for CLOSED sprints.
  - days_elapsed is null for PENDING sprints, and for OPEN sprints with
    no started_at. For CLOSED sprints it spans started_at -> closed_at.
  - days_remaining is ALWAYS null. The Sprint model has no end_date
    field, so there is no target completion date to count down to.
  - burndown is empty when no tasks have been completed in the sprint.

Exit codes:
  0  Success
  3  Missing -r
  4  Sprint not found

Examples:
  rmp sprint stats -r myproject 5
`)
}

// printSprintAddTasksHelp — `rmp sprint add-tasks`.
func printSprintAddTasksHelp() {
	fmt.Print(`Usage: rmp sprint add-tasks -r <roadmap> <sprint-id> <task-ids>

Atomically moves the listed tasks into <sprint-id> AND flips their
status from BACKLOG to SPRINT. This is the ONLY path to SPRINT status:
manual 'task stat <id> SPRINT' is rejected.

Aliases: add.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id (must not be CLOSED)
  <task-ids>                      Comma-separated integer task ids (no spaces, e.g. "1,3,5")

Output: empty (exit 0). Audits SPRINT_ADD_TASK once per added task.

Exit codes:
  0  Success
  3  Missing -r
  4  Sprint or any task id not found
  6  Sprint is CLOSED, or adding the tasks would exceed --max-tasks

Examples:
  rmp sprint add-tasks -r myproject 5 1
  rmp sprint add -r myproject 5 1,3,7
`)
}

// printSprintRemoveTasksHelp — `rmp sprint remove-tasks`.
func printSprintRemoveTasksHelp() {
	fmt.Print(`Usage: rmp sprint remove-tasks -r <roadmap> <sprint-id> <task-ids>

Removes the listed tasks from <sprint-id> and flips their status back
to BACKLOG. The tasks themselves are NOT deleted.

Aliases: rm-tasks.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id
  <task-ids>                      Comma-separated integer task ids (no spaces, e.g. "1,3,5")

Output: empty (exit 0). Audits SPRINT_REMOVE_TASK per removed task.

Exit codes:
  0  Success
  3  Missing -r
  4  Sprint not found, or some task ids are not currently in the sprint

Examples:
  rmp sprint remove-tasks -r myproject 5 7
  rmp sprint rm-tasks -r myproject 5 1,3,7
`)
}

// printSprintMoveTasksHelp — `rmp sprint move-tasks`.
func printSprintMoveTasksHelp() {
	fmt.Print(`Usage: rmp sprint move-tasks -r <roadmap> <from-id> <to-id> <task-ids>

Moves tasks from one sprint to another in a single transaction. Task
statuses are preserved across the move (a DOING task stays DOING).

Aliases: mv-tasks.

Required:
  -r, --roadmap <name>            Target roadmap
  <from-id>                       Source sprint id
  <to-id>                         Destination sprint id (must not be CLOSED)
  <task-ids>                      Comma-separated integer task ids (no spaces, e.g. "1,3,5")

Output: empty (exit 0 on success).

Exit codes:
  0  Success
  3  Missing -r
  4  Either sprint not found, or some task ids aren't in <from-id>
  6  Destination sprint is CLOSED or would exceed its --max-tasks

Examples:
  rmp sprint move-tasks -r myproject 5 8 3,7
  rmp sprint mv-tasks -r myproject 5 8 12
`)
}

// printSprintReorderHelp — `rmp sprint reorder`.
func printSprintReorderHelp() {
	fmt.Print(`Usage: rmp sprint reorder -r <roadmap> <sprint-id> <task-ids-csv>

Sets the exact ordering of tasks within <sprint-id>. The list MUST
include every task currently in the sprint, in the desired order; the
command fails if any task is missing from the list or any unknown id
is included.

Aliases: order.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id
  <task-ids-csv>                  Comma-separated task ids in the desired order (no spaces, e.g. "3,1,7,2")

Output: empty (exit 0). Audits SPRINT_REORDER_TASKS once.

Exit codes:
  0  Success
  3  Missing -r
  4  Sprint not found, or unknown task id in list
  6  List does not match the current set of tasks in the sprint

Examples:
  rmp sprint reorder -r myproject 5 3,1,7,2
  rmp sprint order -r myproject 5 12,15,7
`)
}

// printSprintMoveToHelp — `rmp sprint move-to`.
func printSprintMoveToHelp() {
	fmt.Print(`Usage: rmp sprint move-to -r <roadmap> <sprint-id> <task-id> <position>

Moves a single task to an exact position within the sprint. Position
is zero-based. Other tasks shift to keep the order dense (no gaps).
If <position> >= task count, the task is placed at the end.

Aliases: mvto.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id
  <task-id>                       Integer id of the task to move
  <position>                      Zero-based target index

Output: empty (exit 0). Audits SPRINT_TASK_MOVE_POSITION.

Exit codes:
  0  Success
  3  Missing -r
  4  Task not in the sprint
  6  <position> is negative or non-numeric

Examples:
  rmp sprint move-to -r myproject 5 7 0    # task 7 to the top
  rmp sprint mvto -r myproject 5 12 3      # task 12 becomes position 3
`)
}

// printSprintSwapHelp — `rmp sprint swap`.
func printSprintSwapHelp() {
	fmt.Print(`Usage: rmp sprint swap -r <roadmap> <sprint-id> <task-id-1> <task-id-2>

Exchanges the positions of two tasks within the same sprint.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id
  <task-id-1>                     Integer id of first task
  <task-id-2>                     Integer id of second task (must differ)

Output: empty (exit 0). Audits SPRINT_TASK_SWAP.

Exit codes:
  0  Success
  3  Missing -r
  4  Either task not in the sprint
  6  Same id supplied twice

Examples:
  rmp sprint swap -r myproject 5 3 7
`)
}

// printSprintTopHelp — `rmp sprint top`.
func printSprintTopHelp() {
	fmt.Print(`Usage: rmp sprint top -r <roadmap> <sprint-id> <task-id>

Moves a single task to the top of the sprint (position 0). Other tasks
shift down by one position to make room.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id
  <task-id>                       Integer id of the task

Output: empty (exit 0). Audits SPRINT_TASK_MOVE_POSITION.

Exit codes:
  0  Success
  3  Missing -r
  4  Task not in the sprint

Examples:
  rmp sprint top -r myproject 5 7
`)
}

// printSprintBottomHelp — `rmp sprint bottom`.
func printSprintBottomHelp() {
	fmt.Print(`Usage: rmp sprint bottom -r <roadmap> <sprint-id> <task-id>

Moves a single task to the last position of the sprint. Other tasks
shift up by one position.

Aliases: btm.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id
  <task-id>                       Integer id of the task

Output: empty (exit 0). Audits SPRINT_TASK_MOVE_POSITION.

Exit codes:
  0  Success
  3  Missing -r
  4  Task not in the sprint

Examples:
  rmp sprint bottom -r myproject 5 7
  rmp sprint btm -r myproject 5 12
`)
}
