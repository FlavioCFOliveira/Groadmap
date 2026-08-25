package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// HandleSprint handles sprint commands via the central registry. See
// HandleTask for the rationale; the dispatch lives in
// Command.DispatchFamily.
func HandleSprint(args []string) error {
	return dispatchFamily("sprint", args)
}

// printSprintHelp prints sprint command help.
//
// One "Valid comment types" block covers -y, --type, and one is enough: unlike the
// task family, where the same spelling also carries a TaskType on list / create /
// edit, the only sprint subcommands that accept -y, --type are the four comment-*
// ones, and the flag always means a comment type there (SPEC/COMMANDS.md § Sprint
// Comments; SPEC/HELP.md § Comment subcommand help specifics item 1). The list is
// rendered from models.ValidSprintCommentTypes rather than typed out, so it cannot
// go stale and cannot accidentally show the seven task values.
func printSprintHelp() {
	fmt.Fprintf(helpDst(), `Usage: rmp sprint [command] [arguments] [options]

Valid sprint status values (for --status filter):
  PENDING (never started), OPEN (active), CLOSED

Valid comment types (for -y, --type on 'comment-add', 'comment-list' and
'comment-edit'):
  %s
  This is the only meaning -y, --type has in the sprint family. The set is
  deliberately smaller than the task one: the task-only values HYPOTHESIS,
  TEST and NOTE are rejected here (exit 6), because a sprint records how the
  sprint went and not the execution diary of its individual tasks.

Sprint lifecycle:
  create -> start (PENDING->OPEN) -> close (OPEN->CLOSED) -> reopen (CLOSED->OPEN)
  Rules enforced:
    - At most one sprint can be OPEN at any time (idx_one_open_sprint).
    - 'sprint close' rejects (exit 6) if any task is still SPRINT/DOING/TESTING — pass --force to override.
    - 'sprint add-tasks' rejects (exit 6) if sprint is CLOSED, or if --max-tasks capacity would be exceeded.
    - 'sprint remove' resets all member task statuses back to BACKLOG.
    - 'sprint add-tasks' atomically moves tasks BACKLOG -> SPRINT (manual stat SPRINT is forbidden).
    - A sprint's --order is mutable only while PENDING or OPEN; once CLOSED it is immutable
      (change rejected exit 6). An --order value already used by another sprint is rejected exit 5.

Commands:
  list, ls [OPTIONS]                       List sprints
  create, new [OPTIONS]                    Create a new sprint (PENDING)
  get <sprint-id>                          Get sprint details (sprint object only)
  show <sprint-id>                         Stand-up-style summary (counts, distributions)
  update, upd <sprint-id> [OPTIONS]        Update sprint title, description, capacity cap or order
  remove, rm <sprint-id>                   Delete sprint (member tasks revert to BACKLOG)
  start <sprint-id>                        PENDING -> OPEN (sets started_at)
  close <sprint-id> [--force]              OPEN -> CLOSED (--force bypasses active-task check)
  reopen <sprint-id>                       CLOSED -> OPEN (clears closed_at; started_at preserved)
  tasks <sprint-id> [OPTIONS]              List ALL tasks in sprint (incl. COMPLETED)
  open-tasks <sprint-id> [OPTIONS]         List incomplete tasks in sprint (SPRINT, DOING, TESTING)
  stats <sprint-id>                        Per-status counts, burndown, velocity, days_*
  add-tasks, add <sprint> <ids>            Atomically add BACKLOG tasks -> SPRINT
  remove-tasks, rm-tasks <sprint> <ids>    Remove tasks from sprint (revert to BACKLOG)
  move-tasks, mv-tasks <from> <to> <ids>   Move tasks between sprints (status preserved)
  reorder, order <sprint> <ids>            Set exact full ordering (all members in CSV)
  move-to, mvto <sprint> <task> <pos>      Move one task to zero-based position
  swap <sprint> <task1> <task2>            Swap positions of two tasks
  top <sprint> <task>                      Move task to position 0
  bottom, btm <sprint> <task>              Move task to last position
  comment-add, c-add <sprint-id>           Add one typed comment to a sprint's log
  comment-list, c-ls <sprint-id>           List a sprint's comments, oldest first
  comment-edit, c-edit <comment-id>        Change the type and/or body of one comment
  comment-remove, c-rm <comment-id>        Delete one comment (irreversible)

Options (shared):
  -r, --roadmap <name>                     REQUIRED. Target roadmap.
  -h, --help                               Show this help message

Options (create / update):
  -t, --title <text>                       Sprint title (max 255 chars). REQUIRED on create.
  -d, --description <text>                 Sprint description (max 2048 chars). REQUIRED on create.
                                           Must state the high-level (macro) goal of the development
                                           effort the sprint delivers: a new development, a fix, a
                                           refactoring, or another kind of change. Together with the
                                           title, it must give a human or an AI agent a clear macro
                                           idea of what the sprint's tasks are specifically aimed at.
  --max-tasks <n>                          Capacity cap on active tasks (range 1-10000; cannot
                                           be removed once set)
  --order <n>                              Execution order: positive integer (>0), unique across
                                           the roadmap; auto-assigned (highest existing +1) on
                                           create when omitted; mutable only while PENDING/OPEN,
                                           immutable once CLOSED.

Options (list):
  --status <state>                         Filter by sprint status

Options (tasks):
  -s, --status <state>                     Filter by exact status: BACKLOG, SPRINT, DOING,
                                           TESTING, COMPLETED (invalid value -> exit 6).
                                           Applies to 'tasks' only, not 'open-tasks'.

Options (tasks / open-tasks):
  --order-by-priority                      Sort by priority DESC; default is sprint position ASC

Options (close):
  --force                                  Close even if SPRINT/DOING/TESTING tasks remain

Options (comment-add / comment-list / comment-edit):
  -y, --type <TYPE>                        Comment type (see the comment-type list
                                           above). REQUIRED on comment-add, optional
                                           filter on comment-list, optional new value
                                           on comment-edit.
  -b, --body <text>                        Comment text (max 4096 chars). On comment-add
                                           and comment-edit the body may instead arrive
                                           on standard input, under a bounded read, when
                                           absent — on comment-edit only if --type is
                                           absent too, so a type-only edit never waits
                                           for input. Supplying neither source is an
                                           error (exit 2).

Comment rules (per SPEC/COMMANDS.md § Sprint Comments):
  - A sprint comment records the progression of the sprint: findings, decisions,
    progress, and the reason behind a change to the sprint's own definition. Work
    carried out inside one task belongs in that task's comments.
  - Comments are accepted in every status, including CLOSED, and no comment
    changes or gates a sprint's status.
  - comment-add and comment-list take the SPRINT's id; comment-edit and
    comment-remove take the COMMENT's own id. Sprint and task comment ids are
    separate sequences, so 'sprint comment-edit 7' and 'task comment-edit 7'
    address two unrelated comments.
  - comment-edit requires at least one change (--type, --body, or a body on
    standard input) and is rejected with exit 2 when none is requested.

Output (stdout JSON):
  list                                     Array of sprint objects.
  get                                      Single sprint object.
  show                                     Flat object: sprint_id, sprint_title,
                                           sprint_description, status, max_tasks,
                                           capacity_pct, current_load, task_order,
                                           summary, progress, severity_distribution,
                                           criticality_distribution.
                                           NOTE: does NOT include the task list.
  stats                                    SprintStats: sprint_id, total_tasks, completed_tasks,
                                           progress_percentage, status_distribution, task_order,
                                           burndown[], velocity, days_elapsed, days_remaining.
                                           NOTE: days_remaining is always null (no end_date).
  tasks, open-tasks                        Array of task objects (see 'rmp task --help').
  create                                   {"id": <int>}
  update, remove, start, close, reopen     Empty (exit 0 on success).
  add-tasks, remove-tasks, move-tasks,
  reorder, move-to, swap, top, bottom      Empty (exit 0 on success).
  comment-add                              {"id": <int>}
  comment-list                             Array of comment objects.
  comment-edit, comment-remove             Empty (exit 0 on success).
  Sprint object keys: id, title, status, description, order, created_at,
  started_at, closed_at, max_tasks, tasks (array of int), task_count.
  Comment object keys: id, sprint_id, type, body, created_at, updated_at
  (updated_at is null until the comment is first edited).

Exit codes:
  0   Success
  1   Database failure
  2   Misuse (missing required arg, bad syntax, no comment body supplied)
  3   No roadmap specified (-r missing)
  4   Sprint or comment not found
  5   --order value already used by another sprint (create / update)
  6   Validation error (bad enum, --max-tasks outside 1-10000, close-without-force,
       attempting to open while another sprint is OPEN, --order on a CLOSED sprint,
       oversized or control-character comment body, etc.)
  127 Unknown subcommand

Examples:
  rmp sprint list -r myproject
  rmp sprint create -r myproject -t "Auth hardening" -d "Deliver session-based authentication for every write command."
  rmp sprint create -r myproject -t "Ordering fixes" -d "Fix the task-ordering defects reported in v1.12." --max-tasks 12
  rmp sprint start -r myproject 1
  rmp sprint add-tasks -r myproject 1 1,2,3
  rmp sprint open-tasks -r myproject 1
  rmp sprint reorder -r myproject 1 3,1,2
  rmp sprint move-to -r myproject 1 5 0
  rmp sprint swap -r myproject 1 3 5
  rmp sprint close -r myproject 1 --force
  rmp sprint comment-add -r myproject 3 --type DECISION --body "Dropped the second migration from this sprint."
  rmp sprint comment-add -r myproject 3 --type PROGRESS < progress.txt
  rmp sprint comment-list -r myproject 3 --type DECISION
  rmp sprint comment-edit -r myproject 4 --type UPDATE
  rmp sprint comment-remove -r myproject 4
`, models.FormatCommentTypes(models.ValidSprintCommentTypes))
}
