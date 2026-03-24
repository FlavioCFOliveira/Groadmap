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
	default:
		return fmt.Errorf("%w: unknown task subcommand: %s", utils.ErrInvalidInput, subcommand)
	}
}

// printTaskHelp prints task command help.
func printTaskHelp() {
	fmt.Print(`Usage: rmp task [command] [arguments] [options]

Commands:
  list, ls [OPTIONS]              List tasks
  create, new [OPTIONS]           Create a new task
  get <ids>                      Get tasks by ID(s)
  next [num]                     Get next N tasks from open sprint
  edit <id> [OPTIONS]             Edit a task
  remove, rm <ids>               Remove task(s)
  stat, set-status <ids> <status>  Set task status
  reopen <ids>                   Reopen task(s) back to BACKLOG (clears lifecycle timestamps)
  prio, set-priority <ids> <prio>    Set task priority
  sev, set-severity <ids> <sev>     Set task severity
  assign <id> <specialist>          Add specialist to task (idempotent)
  unassign <id> <specialist>        Remove specialist from task

Options:
  -r, --roadmap <name>           Roadmap name (or use default)
  -s, --status <state>            Filter by status
  -p, --priority <n>              Filter/set priority (0-9)
  --severity <n>                  Filter/set severity (0-9)
  -t, --title <text>              Task title
  -fr, --functional-requirements <text> Functional requirements (Why?)
  -tr, --technical-requirements <text>  Technical requirements (How?)
  -ac, --acceptance-criteria <text>     Acceptance criteria (How to verify?)
  -sp, --specialists <list>       Comma-separated specialists
  -l, --limit <n>                 Limit results
  --help                          Show this help message

Examples:
  rmp task list -r myproject
  rmp task create -r myproject -t "Fix bug" -fr "User can login" -tr "Update auth" -ac "Login works"
  rmp task stat 1,2,3 DOING
`)
}
