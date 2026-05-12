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
	fmt.Print(`Usage: rmp sprint create -r <roadmap> -d <description> [--max-tasks <n>]

Creates a new sprint in PENDING status. The sprint will not accept work
until it is moved to OPEN via 'rmp sprint start <id>'.

Aliases: new.

Required:
  -r, --roadmap <name>            Target roadmap
  -d, --description <text>        Sprint description

Optional:
  --max-tasks <n>                 Hard cap on active tasks (n >= 1). When set,
                                  'sprint add-tasks' refuses to push the active
                                  task count past <n>.

Output (stdout JSON):
  {"id": <new-sprint-id>}

Exit codes:
  0  Success
  2  Missing -d
  3  Missing -r
  6  --max-tasks <= 0

Examples:
  rmp sprint create -r myproject -d "Sprint 1 — Auth hardening"
  rmp sprint new -r myproject -d "Capacity-bounded sprint" --max-tasks 12
`)
}

// printSprintGetHelp — `rmp sprint get`.
func printSprintGetHelp() {
	fmt.Print(`Usage: rmp sprint get -r <roadmap> <sprint-id>

Returns the sprint object for <sprint-id>. For a richer report that
includes tasks and stats, use 'rmp sprint show <sprint-id>' instead.

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

Returns a comprehensive sprint report: sprint metadata, the full task
list, computed statistics (totals/distribution/velocity), and a
severity breakdown. Equivalent to 'sprint get' + 'sprint stats' +
'sprint tasks' in one shot.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Output (stdout JSON):
  Object with keys: sprint, tasks, stats, severity_distribution.

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
	fmt.Print(`Usage: rmp sprint update -r <roadmap> <sprint-id> [-d <text>] [--max-tasks <n>]

Edits the description or capacity cap of an existing sprint. At least
one of -d or --max-tasks must be supplied.

Aliases: upd.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

At least one of:
  -d, --description <text>        New description
  --max-tasks <n>                 New capacity cap (n >= 1). Pass 0 to remove
                                  the cap entirely.

Output: empty (exit 0).

Exit codes:
  0  Success
  2  Neither -d nor --max-tasks given
  3  Missing -r
  4  Sprint not found
  6  --max-tasks < 0, or the new cap is below the current active task count

Examples:
  rmp sprint update -r myproject 5 -d "Sprint 1 — Auth + observability"
  rmp sprint upd -r myproject 5 --max-tasks 15
`)
}

// printSprintRemoveHelp — `rmp sprint remove`.
func printSprintRemoveHelp() {
	fmt.Print(`Usage: rmp sprint remove -r <roadmap> <sprint-id>

Deletes the sprint and resets every member task's status back to
BACKLOG (the tasks themselves are NOT deleted).

Aliases: rm.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Output: empty (exit 0).

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

Output: empty (exit 0).

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

Output: empty (exit 0).

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

Side effect:
  Clears closed_at (sets it to NULL).

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Output: empty (exit 0).

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
velocity (tasks/day), and elapsed/remaining days when the sprint
has been started.

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
  <task-ids>                      Comma-separated integer task ids

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
  <task-ids>                      Comma-separated integer task ids

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
  <task-ids>                      Comma-separated integer task ids

Output: empty (exit 0).

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
  <task-ids-csv>                  Comma-separated task ids — the new full order

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
