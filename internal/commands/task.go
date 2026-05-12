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

// printTaskHelp prints task command help.
func printTaskHelp() {
	fmt.Print(`Usage: rmp task [command] [arguments] [options]

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
