package commands

import "fmt"

// HandleSprint handles sprint commands via the central registry. See
// HandleTask for the rationale; the dispatch lives in
// Command.DispatchFamily.
func HandleSprint(args []string) error {
	return dispatchFamily("sprint", args)
}

// printSprintHelp prints sprint command help.
func printSprintHelp() {
	fmt.Print(`Usage: rmp sprint [command] [arguments] [options]

Valid sprint status values (for --status filter):
  PENDING (never started), OPEN (active), CLOSED

Sprint lifecycle:
  create -> start (PENDING->OPEN) -> close (OPEN->CLOSED) -> reopen (CLOSED->OPEN)
  Rules enforced:
    - At most one sprint can be OPEN at any time (idx_one_open_sprint).
    - 'sprint close' rejects (exit 6) if any task is still SPRINT/DOING/TESTING — pass --force to override.
    - 'sprint add-tasks' rejects (exit 6) if sprint is CLOSED, or if --max-tasks capacity would be exceeded.
    - 'sprint remove' resets all member task statuses back to BACKLOG.
    - 'sprint add-tasks' atomically moves tasks BACKLOG -> SPRINT (manual stat SPRINT is forbidden).

Commands:
  list, ls [OPTIONS]                       List sprints
  create, new [OPTIONS]                    Create a new sprint (PENDING)
  get <id>                                 Get sprint details (sprint object only)
  show <id>                                Stand-up-style summary (counts, distributions)
  update, upd <id> [OPTIONS]               Update sprint description or capacity cap
  remove, rm <id>                          Delete sprint (member tasks revert to BACKLOG)
  start <id>                               PENDING -> OPEN (sets started_at)
  close <id> [--force]                     OPEN -> CLOSED (--force bypasses active-task check)
  reopen <id>                              CLOSED -> OPEN (clears closed_at; started_at preserved)
  tasks <id> [OPTIONS]                     List ALL tasks in sprint (incl. COMPLETED)
  open-tasks <id> [OPTIONS]                List incomplete tasks in sprint (SPRINT, DOING, TESTING)
  stats <id>                               Per-status counts, burndown, velocity, days_*
  add-tasks, add <sprint> <ids>            Atomically add BACKLOG tasks -> SPRINT
  remove-tasks, rm-tasks <sprint> <ids>    Remove tasks from sprint (revert to BACKLOG)
  move-tasks, mv-tasks <from> <to> <ids>   Move tasks between sprints (status preserved)
  reorder, order <sprint> <ids>            Set exact full ordering (all members in CSV)
  move-to, mvto <sprint> <task> <pos>      Move one task to zero-based position
  swap <sprint> <task1> <task2>            Swap positions of two tasks
  top <sprint> <task>                      Move task to position 0
  bottom, btm <sprint> <task>              Move task to last position

Options (shared):
  -r, --roadmap <name>                     REQUIRED. Target roadmap.
  -h, --help                               Show this help message

Options (create / update):
  -d, --description <text>                 Sprint description (free text, max 2048 chars)
  --max-tasks <n>                          Capacity cap on active tasks (n >= 1; cannot be
                                           removed once set)

Options (list):
  --status <state>                         Filter by sprint status

Options (tasks / open-tasks):
  --order-by-priority                      Sort by priority DESC; default is sprint position ASC

Options (close):
  --force                                  Close even if SPRINT/DOING/TESTING tasks remain

Output (stdout JSON):
  list                                     Array of sprint objects.
  get                                      Single sprint object.
  show                                     Flat object: sprint_id, sprint_description, status,
                                           max_tasks, capacity_pct, current_load, task_order,
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
  Sprint object keys: id, status, description, created_at, started_at, closed_at,
  max_tasks, tasks (array of int), task_count.

Exit codes:
  0   Success
  2   Misuse (missing required arg, bad syntax)
  3   No roadmap specified (-r missing)
  4   Sprint not found
  6   Validation error (bad enum, --max-tasks overflow, close-without-force,
       attempting to open while another sprint is OPEN, etc.)

Examples:
  rmp sprint list -r myproject
  rmp sprint create -r myproject -d "Sprint 1"
  rmp sprint create -r myproject -d "Capacity-bounded sprint" --max-tasks 12
  rmp sprint start -r myproject 1
  rmp sprint add-tasks -r myproject 1 1,2,3
  rmp sprint open-tasks -r myproject 1
  rmp sprint reorder -r myproject 1 3,1,2
  rmp sprint move-to -r myproject 1 5 0
  rmp sprint swap -r myproject 1 3 5
  rmp sprint close -r myproject 1 --force
`)
}
