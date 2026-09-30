// Package commands — per-subcommand help text for `rmp sprint`.
//
// Each printer is self-contained: usage line, required/optional flag
// split, output JSON shape, exit codes, and worked examples. Invoked
// from HandleSprint via sprintSubHelp() when --help is in argv.
package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// printSprintListHelp — `rmp sprint list`.
func printSprintListHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint list -r <roadmap> [--status <state>]

Lists every sprint in the roadmap, optionally filtered by status.

Order is by sprint order ASC: the sprint with the lowest 'order' value
first, which is the roadmap's planned execution order. 'order' is
unique across the roadmap, so the sequence is total and repeatable.
--status narrows the result; it never reorders it.

Aliases: ls.

Required:
  -r, --roadmap <name>            Target roadmap

Optional:
  --status <state>                One of: PENDING, OPEN, CLOSED

Output (stdout JSON):
  Array of sprint objects. See 'rmp sprint --help' for the full key list.

`+exitCodesBlock("sprint", "list")+`Examples:
  rmp sprint list -r myproject
  rmp sprint ls -r myproject --status OPEN
`)
}

// printSprintCreateHelp — `rmp sprint create`.
func printSprintCreateHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint create -r <roadmap> -t <title> -d <description> [--max-tasks <n>] [--order <n>]

Creates a new sprint in PENDING status. The sprint will not accept work
until it is moved to OPEN via 'rmp sprint start <id>'.

Sprints have no fixed end date. started_at is set when the sprint is
moved to OPEN; closed_at is set when it is moved to CLOSED. There is
no --start-date / --end-date flag.

Aliases: new.

Required:
  -r, --roadmap <name>            Target roadmap
  -t, --title <text>              Sprint title (max 255 chars)
  -d, --description <text>        Sprint description (max 2048 chars). REQUIRED
                                  on create. It must state the high-level (macro)
                                  goal of the development effort the sprint
                                  delivers: a new development, a fix, a
                                  refactoring, or another kind of change.
                                  Together with the title, it must give a human
                                  or an AI agent a clear macro idea of what the
                                  sprint's tasks are specifically aimed at.

Optional:
  --max-tasks <n>                 Hard cap on active tasks (range 1-10000). When
                                  set, 'sprint add-tasks' refuses to push the
                                  active task count past <n>. Cannot be removed
                                  once set, only changed to another value in range.
  --order <n>                     Sprint execution order: a positive integer
                                  (> 0), unique across the roadmap; the sprint
                                  with the lowest --order executes first. When
                                  omitted, the next available value (the highest
                                  existing order plus one; 1 for the first sprint)
                                  is auto-assigned. A value already used by
                                  another sprint is rejected (exit 5).

Output (stdout JSON):
  {"id": <new-sprint-id>}

`+exitCodesBlock("sprint", "create")+`Examples:
  rmp sprint create -r myproject -t "Auth hardening" -d "Deliver session-based authentication for every write command."
  rmp sprint new -r myproject -t "Ordering fixes" -d "Fix the task-ordering defects reported in v1.12." --max-tasks 12
  rmp sprint create -r myproject -t "Storage refactor" -d "Refactor persistence onto a single write path." --order 3
`)
}

// printSprintGetHelp — `rmp sprint get`.
func printSprintGetHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint get -r <roadmap> <sprint-id>

Returns the sprint object for <sprint-id>. For a richer summary with
counts and severity distribution, use 'rmp sprint show <sprint-id>'.
For per-status counts, burndown and velocity, use 'rmp sprint stats'.
For the task list, use 'rmp sprint tasks'.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Output (stdout JSON):
  Single sprint object (see 'rmp sprint --help').

`+exitCodesBlock("sprint", "get")+`Examples:
  rmp sprint get -r myproject 5
`)
}

// printSprintShowHelp — `rmp sprint show`.
func printSprintShowHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint show -r <roadmap> <sprint-id>

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
    sprint_title             string
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

`+exitCodesBlock("sprint", "show")+`Examples:
  rmp sprint show -r myproject 5
`)
}

// printSprintUpdateHelp — `rmp sprint update`.
func printSprintUpdateHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint update -r <roadmap> <sprint-id> [-t <title>] [-d <text>] [--max-tasks <n>] [--order <n>]

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
  -d, --description <text>        New sprint description (max 2048 chars). It
                                  must state the high-level (macro) goal of the
                                  development effort the sprint delivers: a new
                                  development, a fix, a refactoring, or another
                                  kind of change. Together with the title, it
                                  must give a human or an AI agent a clear macro
                                  idea of what the sprint's tasks are
                                  specifically aimed at.
  --max-tasks <n>                 New capacity cap; range 1-10000
  --order <n>                     New execution order; positive integer (> 0),
                                  unique across the roadmap. Allowed only while
                                  PENDING or OPEN; immutable once CLOSED.

Output: empty (exit 0 on success).

`+exitCodesBlock("sprint", "update")+`Examples:
  rmp sprint update -r myproject 5 -t "Auth + observability"
  rmp sprint update -r myproject 5 -d "Deliver authentication and request tracing for every write command."
  rmp sprint upd -r myproject 5 --max-tasks 15
  rmp sprint update -r myproject 5 --order 2
`)
}

// printSprintRemoveHelp — `rmp sprint remove`.
func printSprintRemoveHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint remove -r <roadmap> <sprint-id>

Deletes the sprint. Its member tasks (SPRINT, DOING or TESTING) leave
it and are reverted to BACKLOG, clearing started_at, tested_at,
closed_at, completion_summary and commit_close and preserving
commit_open. The tasks themselves are NOT deleted; their requirements,
priority, severity, etc. are preserved.

A sprint that holds at least one COMPLETED task is NOT removed (exit 6)
and nothing is changed: a completed task stays in the sprint it was
completed in. The refusal names every COMPLETED member. Leave such a
sprint CLOSED and create a new sprint instead.

Aliases: rm.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Output: empty (exit 0 on success).

`+exitCodesBlock("sprint", "remove")+`Examples:
  rmp sprint remove -r myproject 5
  rmp sprint rm -r myproject 9
`)
}

// printSprintStartHelp — `rmp sprint start`.
func printSprintStartHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint start -r <roadmap> <sprint-id>

Transitions a sprint from PENDING (or CLOSED, see 'reopen') to OPEN.
Only one sprint can be OPEN per roadmap at any time — enforced at the
database level via idx_one_open_sprint.

Side effect:
  Sets started_at to the current timestamp on the PENDING -> OPEN path.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Output: empty (exit 0 on success).

`+exitCodesBlock("sprint", "start")+`Examples:
  rmp sprint start -r myproject 5
`)
}

// printSprintCloseHelp — `rmp sprint close`.
func printSprintCloseHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint close -r <roadmap> <sprint-id> [--force]

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

`+exitCodesBlock("sprint", "close")+`Examples:
  rmp sprint close -r myproject 5
  rmp sprint close -r myproject 5 --force
`)
}

// printSprintReopenHelp — `rmp sprint reopen`.
func printSprintReopenHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint reopen -r <roadmap> <sprint-id>

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

`+exitCodesBlock("sprint", "reopen")+`Examples:
  rmp sprint reopen -r myproject 5
`)
}

// printSprintTasksHelp — `rmp sprint tasks`.
func printSprintTasksHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint tasks -r <roadmap> <sprint-id> [-s <state>] [--order-by-priority]

Lists every task assigned to <sprint-id>, regardless of status (so
COMPLETED tasks are included). Use 'sprint open-tasks' to exclude
COMPLETED.

Default order is by sprint position ASC. With --order-by-priority,
the result is sorted by priority DESC, then sprint position ASC.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Optional:
  -s, --status <state>            Filter by exact status: BACKLOG, SPRINT,
                                  DOING, TESTING, COMPLETED (invalid value -> exit 6)
  --order-by-priority             Sort by priority DESC, then sprint position ASC

Output (stdout JSON):
  Array of task objects.

`+exitCodesBlock("sprint", "tasks")+`Examples:
  rmp sprint tasks -r myproject 5
  rmp sprint tasks -r myproject 5 -s DOING
  rmp sprint tasks -r myproject 5 --order-by-priority
`)
}

// printSprintOpenTasksHelp — `rmp sprint open-tasks`.
func printSprintOpenTasksHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint open-tasks -r <roadmap> <sprint-id> [--order-by-priority]

Lists tasks assigned to <sprint-id> whose status is one of SPRINT,
DOING, or TESTING (i.e. all incomplete sprint work). Useful for
stand-ups and burndown.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id

Optional:
  --order-by-priority             Sort by priority DESC, then sprint position ASC

Output (stdout JSON):
  Array of task objects (excludes BACKLOG and COMPLETED).

`+exitCodesBlock("sprint", "open-tasks")+`Examples:
  rmp sprint open-tasks -r myproject 5
  rmp sprint open-tasks -r myproject 5 --order-by-priority
`)
}

// printSprintStatsHelp — `rmp sprint stats`.
func printSprintStatsHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint stats -r <roadmap> <sprint-id>

Returns the SprintStats object: per-status counts, completion
percentage, ordered task ids, burndown series (one entry per day),
velocity (tasks/day), and elapsed days while the sprint is OPEN.

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
  - days_elapsed counts the days since an OPEN sprint started, and is null
    everywhere else: for PENDING sprints, for CLOSED sprints, and for OPEN
    sprints with no started_at.
  - days_remaining is ALWAYS null. The Sprint model has no end_date
    field, so there is no target completion date to count down to.
  - burndown is empty when no tasks have been completed in the sprint.

`+exitCodesBlock("sprint", "stats")+`Examples:
  rmp sprint stats -r myproject 5
`)
}

// printSprintAddTasksHelp — `rmp sprint add-tasks`.
func printSprintAddTasksHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint add-tasks -r <roadmap> <sprint-id> <task-ids>

Atomically moves the listed tasks into <sprint-id>. A BACKLOG task joins
as SPRINT; a SPRINT, DOING or TESTING task taken from another sprint keeps
its status and every tracking field, so unfinished work is carried over
from a closed sprint this way. A COMPLETED task is rejected (exit 6): it
stays in the sprint it was completed in. Manual 'task stat <id> SPRINT' is
rejected. The capacity cap counts the named tasks that are not already
members of the sprint.

Aliases: add.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id (must not be CLOSED)
  <task-ids>                      Comma-separated integer task ids (no spaces, e.g. "1,3,5")

Output: empty (exit 0). Audits SPRINT_ADD_TASK once per added task, plus
TASK_STATUS_SPRINT for a task that joined from BACKLOG, or
SPRINT_MOVE_TASK_OUT and TASK_SPRINT_CHANGE for one taken from another
sprint.

`+exitCodesBlock("sprint", "add-tasks")+`Examples:
  rmp sprint add-tasks -r myproject 5 1
  rmp sprint add -r myproject 5 1,3,7
`)
}

// printSprintRemoveTasksHelp — `rmp sprint remove-tasks`.
func printSprintRemoveTasksHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint remove-tasks -r <roadmap> <sprint-id> <task-ids>

Removes the listed tasks from <sprint-id> and flips their status back
to BACKLOG. The tasks themselves are NOT deleted. The return to BACKLOG
clears started_at, tested_at, closed_at, completion_summary and
commit_close, and PRESERVES commit_open — the commit the work started from
stays true after the task leaves its sprint. A COMPLETED task is rejected
(exit 6): it stays in the sprint it was completed in.

Aliases: rm-tasks.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id
  <task-ids>                      Comma-separated integer task ids (no spaces, e.g. "1,3,5")

Output: empty (exit 0). Audits SPRINT_REMOVE_TASK per removed task.

`+exitCodesBlock("sprint", "remove-tasks")+`Examples:
  rmp sprint remove-tasks -r myproject 5 7
  rmp sprint rm-tasks -r myproject 5 1,3,7
`)
}

// printSprintMoveTasksHelp — `rmp sprint move-tasks`.
func printSprintMoveTasksHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint move-tasks -r <roadmap> <from-id> <to-id> <task-ids>

Moves tasks from one sprint to another in a single transaction. Task
statuses are preserved across the move (a DOING task stays DOING). A
COMPLETED task is rejected (exit 6): it stays in the sprint it was
completed in.

Aliases: mv-tasks.

Required:
  -r, --roadmap <name>            Target roadmap
  <from-id>                       Source sprint id
  <to-id>                         Destination sprint id (must not be CLOSED)
  <task-ids>                      Comma-separated integer task ids (no spaces, e.g. "1,3,5")

Output: empty (exit 0 on success).

`+exitCodesBlock("sprint", "move-tasks")+`Examples:
  rmp sprint move-tasks -r myproject 5 8 3,7
  rmp sprint mv-tasks -r myproject 5 8 12
`)
}

// printSprintReorderHelp — `rmp sprint reorder`.
func printSprintReorderHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint reorder -r <roadmap> <sprint-id> <task-ids-csv>

Sets the exact ordering of tasks within <sprint-id>. The list MUST
include every task currently in the sprint, in the desired order; the
command fails if any task is missing from the list or any unknown id
is included.

Aliases: order.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id
  <task-ids-csv>                  Comma-separated task ids in the desired order (no spaces, e.g. "3,1,7,2")

Output: a JSON success object naming the sprint and the order its members now
hold (exit 0). Audits SPRINT_REORDER_TASKS once.

`+exitCodesBlock("sprint", "reorder")+`Examples:
  rmp sprint reorder -r myproject 5 3,1,7,2
  rmp sprint order -r myproject 5 12,15,7
`)
}

// printSprintMoveToHelp — `rmp sprint move-to`.
func printSprintMoveToHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint move-to -r <roadmap> <sprint-id> <task-id> <position>

Moves a single task to an exact position within the sprint. Position
is zero-based. Other tasks shift to keep the order dense (no gaps).
If <position> >= task count, the task is placed at the end.

Aliases: mvto.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id
  <task-id>                       Integer id of the task to move
  <position>                      Zero-based target index

Output: a JSON success object naming the sprint, the task, and the position it
now holds (exit 0). Audits SPRINT_TASK_MOVE_POSITION.

`+exitCodesBlock("sprint", "move-to")+`Examples:
  rmp sprint move-to -r myproject 5 7 0    # task 7 to the top
  rmp sprint mvto -r myproject 5 12 3      # task 12 becomes position 3
`)
}

// printSprintSwapHelp — `rmp sprint swap`.
func printSprintSwapHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint swap -r <roadmap> <sprint-id> <task-id-1> <task-id-2>

Exchanges the positions of two tasks within the same sprint.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id
  <task-id-1>                     Integer id of first task
  <task-id-2>                     Integer id of second task (must differ)

Output: a JSON success object naming the sprint and the two tasks that
exchanged positions (exit 0). Audits SPRINT_TASK_SWAP.

`+exitCodesBlock("sprint", "swap")+`Examples:
  rmp sprint swap -r myproject 5 3 7
`)
}

// printSprintTopHelp — `rmp sprint top`.
func printSprintTopHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint top -r <roadmap> <sprint-id> <task-id>

Moves a single task to the top of the sprint (position 0). Other tasks
shift down by one position to make room.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id
  <task-id>                       Integer id of the task

Output: a JSON success object naming the sprint, the task, and the position it
now holds (exit 0). Audits SPRINT_TASK_MOVE_POSITION.

`+exitCodesBlock("sprint", "top")+`Examples:
  rmp sprint top -r myproject 5 7
`)
}

// printSprintBottomHelp — `rmp sprint bottom`.
func printSprintBottomHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint bottom -r <roadmap> <sprint-id> <task-id>

Moves a single task to the last position of the sprint. Other tasks
shift up by one position.

Aliases: btm.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer sprint id
  <task-id>                       Integer id of the task

Output: a JSON success object naming the sprint, the task, and the position it
now holds (exit 0). Audits SPRINT_TASK_MOVE_POSITION.

`+exitCodesBlock("sprint", "bottom")+`Examples:
  rmp sprint bottom -r myproject 5 7
  rmp sprint btm -r myproject 5 12
`)
}

// sprintCommentTypes renders the four values a sprint comment accepts, exactly as
// the rejection message and the AI Agent Contract publish them. The list is never
// re-typed in a help body: models.FormatCommentTypes is the single source, so a
// change to the accepted set cannot leave a stale list behind in the help, and the
// seven task values can never appear on a sprint subcommand
// (SPEC/HELP.md § Comment subcommand help specifics item 1).
func sprintCommentTypes() string {
	return models.FormatCommentTypes(models.ValidSprintCommentTypes)
}

// printSprintCommentAddHelp — `rmp sprint comment-add`.
func printSprintCommentAddHelp() {
	fmt.Fprintf(helpDst(), `Usage: rmp sprint comment-add -r <roadmap> <sprint-id> --type <TYPE> [--body <text>]

Adds one typed entry to a sprint's log: what was found, what was decided,
how the work progressed, and why the sprint's own definition changed. Work
carried out inside one task belongs in that task's comments, not here.

Comments are accepted in every sprint status, including CLOSED, and no
comment ever changes or gates a sprint's status.

The positional argument is the SPRINT's id — the comment's own id is
assigned by this command and printed on success.

Aliases: c-add.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer id of the sprint being commented on
  -y, --type <TYPE>               Comment type; one of the values below

Valid comment types (for -y, --type on the comment-* subcommands):
  %s

  Note: the sprint set is deliberately smaller than the task set. The
  task-only values HYPOTHESIS, TEST and NOTE are rejected here with exit 6:
  a sprint records how the sprint went, not the execution diary of its
  individual tasks. In this family -y, --type has no other meaning.

Optional:
  -b, --body <text>               Comment text, max 4096 characters. When this
                                  flag is absent the body is read from standard
                                  input under a bounded read (a heredoc, a pipe
                                  or a file redirect); supplying neither the flag
                                  nor a non-empty standard input fails with exit 2.
                                  Leading and trailing whitespace is trimmed;
                                  interior line breaks are preserved.

Validation order (a bad --type never leaves the command waiting on input):
  roadmap, then <sprint-id>, then --type presence, then the --type value,
  then the body, then the sprint's existence, then the body's length and
  control characters, then the insert and its audit entry in one transaction.

Output (stdout JSON):
  {"id": <new-comment-id>}

%sExamples:
  rmp sprint comment-add -r myproject 3 --type DECISION \
      --body "Dropped the second migration: its schema change is not settled."
  rmp sprint comment-add -r myproject 3 --type PROGRESS < progress.txt
  cat finding.txt | rmp sprint comment-add -r myproject 3 -y FINDING
  rmp sprint comment-add -r myproject 3 --type UPDATE <<'BODY'
Extended the sprint goal to cover the boundary-second regression test.
The fix alone would have shipped without a guard against reintroduction.
BODY
`, sprintCommentTypes(), exitCodesBlock("sprint", "comment-add"))
}

// printSprintCommentListHelp — `rmp sprint comment-list`.
func printSprintCommentListHelp() {
	fmt.Fprintf(helpDst(), `Usage: rmp sprint comment-list -r <roadmap> <sprint-id> [--type <TYPE>]

Returns every comment of the given sprint, oldest first: created_at
ascending with the comment id as the tie-breaker. Read top to bottom, the
log is the account of how the sprint went — what was tried, what was found
and why it went the way it did.

The positional argument is the SPRINT's id, not a comment id.

Aliases: c-ls.

Required:
  -r, --roadmap <name>            Target roadmap
  <sprint-id>                     Integer id of the sprint whose log is read

Optional:
  -y, --type <TYPE>               Return only the comments of this type. The
                                  value MUST be one of the values below; any
                                  other value fails with exit 6, including a
                                  value that is valid only on a task comment.

Valid comment types (for -y, --type on the comment-* subcommands):
  %s

  Note: the task-only values HYPOTHESIS, TEST and NOTE are not accepted as a
  filter here. In this family -y, --type has no other meaning.

Result-set size:
  Unbounded. Every matching comment is returned; there is no --limit, no
  --desc and no pagination.

Output (stdout JSON):
  Array of comment objects, oldest first. Keys: id, sprint_id, type, body,
  created_at, updated_at (null until the comment is first edited).
  Empty array (exit 0) when the sprint has no comments, or none of the
  requested type.

%sExamples:
  rmp sprint comment-list -r myproject 3
  rmp sprint comment-list -r myproject 3 --type DECISION
  rmp sprint c-ls -r myproject 3 -y FINDING
`, sprintCommentTypes(), exitCodesBlock("sprint", "comment-list"))
}

// printSprintCommentEditHelp — `rmp sprint comment-edit`.
func printSprintCommentEditHelp() {
	fmt.Fprintf(helpDst(), `Usage: rmp sprint comment-edit -r <roadmap> <comment-id> [--type <TYPE>] [--body <text>]

Changes the type and/or the body of one existing sprint comment and stamps
updated_at, so a later listing shows that the comment was altered. The
previous text is not retained anywhere and cannot be recovered: the audit
log records that an edit happened, not what it replaced.

The positional argument is the COMMENT's own id, NOT the id of the sprint it
belongs to. Sprint comment ids and task comment ids are separate sequences,
so an id that exists under 'task comment-edit' is not found here.

Aliases: c-edit.

Required:
  -r, --roadmap <name>            Target roadmap
  <comment-id>                    Integer id of the comment itself
  At least one change: a --type value, a --body value, or a body on standard
  input. Unlike 'sprint update', a request with no change is rejected
  (exit 2), not accepted as a no-op.

Valid comment types (for -y, --type on the comment-* subcommands):
  %s

  Note: the task-only values HYPOTHESIS, TEST and NOTE are rejected here
  with exit 6. In this family -y, --type has no other meaning.

Optional:
  -y, --type <TYPE>               New comment type; one of the values above
  -b, --body <text>               New comment text, max 4096 characters. When
                                  --body is absent AND --type is absent, the
                                  new body is read from standard input,
                                  so 'comment-edit <comment-id> < revised.txt'
                                  is a valid edit. Standard input is the new
                                  body only when --type is absent as well. When
                                  --type is present and --body is absent, the
                                  body is left unchanged: a terminal on standard
                                  input is not read, so a type-only edit typed
                                  at a terminal never waits for input, and a
                                  standard input that carries data is refused
                                  (exit 2) rather than ignored.

Output (stdout JSON):
  Empty (exit 0 on success), as for 'sprint update'.

%sExamples:
  rmp sprint comment-edit -r myproject 4 --type UPDATE
  rmp sprint comment-edit -r myproject 4 \
      --body "Superseded: the migration landed inside this sprint after all."
  rmp sprint comment-edit -r myproject 4 < revised.txt
  rmp sprint c-edit -r myproject 4 -y PROGRESS -b "Two of five tasks closed."
`, sprintCommentTypes(), exitCodesBlock("sprint", "comment-edit"))
}

// printSprintCommentRemoveHelp — `rmp sprint comment-remove`.
func printSprintCommentRemoveHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp sprint comment-remove -r <roadmap> <comment-id>

Deletes one sprint comment. The row is removed outright: there is no soft
delete and no recovery. The audit entry outlives the row, so the sprint's
history still records that a comment existed and was removed.

The positional argument is the COMMENT's own id, NOT the id of the sprint it
belongs to. Sprint comment ids and task comment ids are separate sequences,
so an id that exists under 'task comment-remove' is not found here.

Exactly one id is accepted: this command takes no comma-separated list, so
the batch fail-fast rules of 'task remove' do not apply.

Aliases: c-rm.

Required:
  -r, --roadmap <name>            Target roadmap
  <comment-id>                    Integer id of the comment itself

Output (stdout JSON):
  Empty (exit 0 on success).

`+exitCodesBlock("sprint", "comment-remove")+`Examples:
  rmp sprint comment-remove -r myproject 4
  rmp sprint c-rm -r myproject 4
`)
}
