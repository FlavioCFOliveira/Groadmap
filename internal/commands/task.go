// Package commands implements CLI command handlers for Groadmap.
package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// HandleTask handles task commands.
func HandleTask(args []string) error {
	if len(args) == 0 {
		printTaskHelp()
		return nil
	}

	subcommand := args[0]

	if subcommand == "-h" || subcommand == "--help" || subcommand == "help" {
		printTaskHelp()
		return nil
	}

	// Subcommand-level help: 'rmp task <sub> --help'.
	if hasHelpFlag(args[1:]) {
		if h := taskSubHelp(subcommand); h != nil {
			h()
			return nil
		}
	}

	switch subcommand {
	case "list", "ls":
		return taskList(args[1:])
	case "create", "new":
		return taskCreate(args[1:])
	case "get":
		return taskGet(args[1:])
	case "next":
		return taskNext(args[1:])
	case "edit":
		return taskEdit(args[1:])
	case "remove", "rm":
		return taskRemove(args[1:])
	case "stat", "set-status":
		return taskSetStatus(args[1:])
	case "reopen":
		return taskReopen(args[1:])
	case "prio", "set-priority":
		return taskSetPriority(args[1:])
	case "sev", "set-severity":
		return taskSetSeverity(args[1:])
	case "assign":
		return taskAssign(args[1:])
	case "unassign":
		return taskUnassign(args[1:])
	case "subtasks":
		return taskSubtasks(args[1:])
	case "add-dep":
		return taskAddDep(args[1:])
	case "remove-dep":
		return taskRemoveDep(args[1:])
	case "blockers":
		return taskBlockers(args[1:])
	case "blocking":
		return taskBlocking(args[1:])
	default:
		return fmt.Errorf("%w: unknown task subcommand: %s", utils.ErrInvalidInput, subcommand)
	}
}

// taskSubHelp returns the help printer for a given task subcommand (long
// name or alias), or nil if the subcommand is unknown.
func taskSubHelp(sub string) func() {
	switch sub {
	case "list", "ls":
		return printTaskListHelp
	case "create", "new":
		return printTaskCreateHelp
	case "get":
		return printTaskGetHelp
	case "next":
		return printTaskNextHelp
	case "edit":
		return printTaskEditHelp
	case "remove", "rm":
		return printTaskRemoveHelp
	case "stat", "set-status":
		return printTaskStatHelp
	case "reopen":
		return printTaskReopenHelp
	case "prio", "set-priority":
		return printTaskPrioHelp
	case "sev", "set-severity":
		return printTaskSevHelp
	case "assign":
		return printTaskAssignHelp
	case "unassign":
		return printTaskUnassignHelp
	case "subtasks":
		return printTaskSubtasksHelp
	case "add-dep":
		return printTaskAddDepHelp
	case "remove-dep":
		return printTaskRemoveDepHelp
	case "blockers":
		return printTaskBlockersHelp
	case "blocking":
		return printTaskBlockingHelp
	}
	return nil
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
  list, ls [OPTIONS]              			List tasks
  create, new [OPTIONS]           			Create a new task
  get <ids>                      			Get tasks by ID(s)
  next [num]                     			Get next N tasks from open sprint
  edit <id> [OPTIONS]             			Edit a task
  remove, rm <ids>               			Remove task(s)
  stat, set-status <ids> <status>  			Set task status
  reopen <ids>                   			Reopen task(s) back to BACKLOG (clears lifecycle timestamps)
  prio, set-priority <ids> <prio>    		Set task priority
  sev, set-severity <ids> <sev>     		Set task severity
  assign <id> <specialist>          		Add specialist to task (idempotent)
  unassign <id> <specialist>        		Remove specialist from task
  subtasks <id>                     		List all direct subtasks of a task
  add-dep <id> <dep-id>             		Mark task <id> as depending on task <dep-id>
  remove-dep <id> <dep-id>          		Remove dependency of task <id> on task <dep-id>
  blockers <id>                     		List tasks blocking task <id> (dependencies not yet COMPLETED)
  blocking <id>                     		List tasks that task <id> is blocking (tasks that depend on it)

Options (shared by most subcommands):
  -r, --roadmap <name>           			REQUIRED. Target roadmap.
  -h, --help                      			Show this help message

Options (list — all filters compose with AND):
  -s, --status <state>            			Filter by exact status
  -p, --priority <min>            			Filter: priority >= min (0-9)
  --severity <min>                			Filter: severity >= min (0-9)
  -y, --type <type>               			Filter by task type
  -sp, --specialists <substring>  			Filter by specialists (case-insensitive substring)
  --created-since <date>          			Include tasks created on/after this date (RFC3339 or YYYY-MM-DD)
  --created-until <date>          			Include tasks created on/before this date (RFC3339 or YYYY-MM-DD)
  --sort <field>                  			Sort: priority (default), created, status, severity
  -l, --limit <n>                 			Maximum results (default 100)

Options (create / edit):
  -t, --title <text>              			Task title (max 255 chars)
  -fr, --functional-requirements <text> 	Functional requirements (Why? — max 4096 chars)
  -tr, --technical-requirements <text>  	Technical requirements (How? — max 4096 chars)
  -ac, --acceptance-criteria <text>     	Acceptance criteria (How to verify? — max 4096 chars)
  -y, --type <type>               			Task type (default: TASK)
  -p, --priority <n>              			Initial/new priority (0-9, default 0)
  --severity <n>                  			Initial/new severity (0-9, default 0)
  -sp, --specialists <list>       			Comma-separated specialists (max 500 chars)
  --parent <id>                   			Parent task ID (on create only — makes a sub-task)

Options (stat to COMPLETED):
  -s, --summary <text>            			Completion summary (max 4096 chars; only valid when transitioning to COMPLETED)

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
