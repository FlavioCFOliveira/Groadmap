// Package commands implements CLI command handlers for Groadmap.
package commands

import "fmt"

// HandleTask handles task commands by delegating to the central
// command registry. The dispatch (subcommand resolution, alias
// matching, --help routing) is implemented once in
// Command.DispatchFamily; this function exists only to expose the
// task-family entry point with its historic signature for callers and
// tests that resolve commands by name.
func HandleTask(args []string) error {
	return dispatchFamily("task", args)
}

// printTaskHelp prints task command help.
func printTaskHelp() {
	fmt.Print(`Usage: rmp task [command] [arguments] [options]

Valid status values (for --status filter and 'stat' setter):
  BACKLOG, SPRINT, DOING, TESTING, COMPLETED

Valid task types (for --type filter and 'create'/'edit' setter):
  USER_STORY, TASK, BUG, SUB_TASK, EPIC, REFACTOR, CHORE, SPIKE, DESIGN_UX, IMPROVEMENT

Numeric ranges:
  --priority, --severity      0-9 (0 = lowest, 9 = highest)

Status workflow (per SPEC/STATE_MACHINE.md):
  BACKLOG --[sprint add-tasks]--> SPRINT --[task stat DOING]--> DOING
        DOING --[task stat TESTING]--> TESTING
        TESTING --[task stat COMPLETED]--> COMPLETED --[task reopen / stat BACKLOG]--> BACKLOG
  Rules enforced:
    - 'task stat <id> SPRINT' is rejected (exit 6). Use 'sprint add-tasks' instead.
    - 'task remove' is only allowed while a task is in BACKLOG.
    - Marking COMPLETED is rejected (exit 6) if any subtask or dependency is not yet COMPLETED.
    - On COMPLETED transition you may attach a free-form summary with --summary / -s (max 4096 chars).
    - 'task reopen' (or 'stat BACKLOG' from COMPLETED) clears started_at, tested_at, closed_at, completion_summary.

Commands:
  list, ls [OPTIONS]                List tasks (any status; filter with --status)
  create, new [OPTIONS]             Create a new task (lands in BACKLOG)
  get <ids>                         Get tasks by id (CSV, no spaces)
  next [num]                        Get next [num] incomplete tasks from the OPEN sprint
  edit <id> [OPTIONS]               Edit fields of a task (status NOT editable here)
  remove, rm <ids>                  Remove task(s) — BACKLOG only, no active subtasks
  stat, set-status <ids> <status>   Set task status (manual transitions; SPRINT is rejected)
  reopen <ids>                      Reopen task(s) to BACKLOG, clearing lifecycle timestamps
  prio, set-priority <ids> <prio>   Set task priority (0-9) for one or many tasks
  sev, set-severity <ids> <sev>     Set task severity (0-9) for one or many tasks
  assign <id> <specialist>          Add specialist to task (idempotent; stderr note on dup)
  unassign <id> <specialist>        Remove specialist from task (idempotent)
  subtasks <id>                     List direct subtasks (one level; no grand-children)
  add-dep <id> <dep-id>             Declare task <id> depends on task <dep-id> (cycles rejected)
  remove-dep <id> <dep-id>          Remove the dependency edge created by add-dep
  blockers <id>                     List tasks blocking <id> (incomplete dependencies)
  blocking <id>                     List tasks that depend on <id> (reverse of blockers)

Options (shared by most subcommands):
  -r, --roadmap <name>              REQUIRED. Target roadmap.
  -h, --help                        Show this help message

Options (list — all filters compose with AND):
  -s, --status <state>              Filter by exact status
  -p, --priority <min>              Filter: priority >= min (0-9)
  --severity <min>                  Filter: severity >= min (0-9)
  -y, --type <type>                 Filter by task type
  -sp, --specialists <substring>    Filter by specialists (case-insensitive substring)
  --created-since <date>            Include tasks created on/after this date (RFC3339 or YYYY-MM-DD)
  --created-until <date>            Include tasks created on/before this date (RFC3339 or YYYY-MM-DD)
  --sort <field>                    Sort: priority (default), created, status, severity
  -l, --limit <n>                   Maximum results (1-100, default 100)

Options (create / edit):
  -t,  --title <text>               Task title (max 255 chars)
  -fr, --functional-requirements <text>
                                    Functional requirements — Why? (max 4096 chars)
  -tr, --technical-requirements <text>
                                    Technical requirements — How? (max 4096 chars)
  -ac, --acceptance-criteria <text>
                                    Acceptance criteria — How to verify? (max 4096 chars)
  -y,  --type <type>                Task type (default: TASK)
  -p,  --priority <n>               Initial/new priority (0-9, default 0)
       --severity <n>               Initial/new severity (0-9, default 0)
  -sp, --specialists <list>         Comma-separated specialists (max 500 chars)
       --parent <id>                Parent task ID (on create only — makes a sub-task)

Options (stat to COMPLETED):
  -s, --summary <text>              Completion summary (max 4096 chars; only valid when
                                    target status is COMPLETED)

Output (stdout JSON):
  list, get, next, subtasks, blockers, blocking   Array of task objects.
  create                                          {"id": <int>}
  edit, stat, prio, sev, reopen, remove           Empty (exit 0 on success).
  assign, unassign                                Empty (exit 0 on success).
  add-dep, remove-dep                             Empty (exit 0 on success).
  Task object keys: id, title, status, type, functional_requirements,
  technical_requirements, acceptance_criteria, created_at, specialists,
  started_at, tested_at, closed_at, completion_summary, parent_task_id,
  priority, severity, subtask_count, depends_on, blocks.

Exit codes:
  0   Success
  2   Misuse (missing required flag, bad syntax)
  3   No roadmap specified (-r missing)
  4   Task not found
  6   Validation error (bad enum, out-of-range number, oversized field,
       invalid state transition, subtask/dependency guard, etc.)

Examples:
  rmp task list -r myproject
  rmp task list -r myproject --status BACKLOG --priority 5 --sort priority
  rmp task list -r myproject --created-since 2026-01-01 --type BUG
  rmp task create -r myproject -t "Fix bug" -fr "User can login" -tr "Update auth" -ac "Login works"
  rmp task create -r myproject -t "Add metrics" --type CHORE -p 3
  rmp task edit -r myproject 42 -t "Updated title" -p 8
  rmp task stat -r myproject 1,2,3 DOING
  rmp task stat -r myproject 7 COMPLETED --summary "Shipped behind feature flag"
  rmp task prio -r myproject 1,2,3 8
  rmp task sev -r myproject 5 9
  rmp task add-dep -r myproject 10 7
  rmp task blockers -r myproject 10
  rmp task next -r myproject 5
`)
}
