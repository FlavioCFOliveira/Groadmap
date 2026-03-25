package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// HandleSprint handles sprint commands.
func HandleSprint(args []string) error {
	if len(args) == 0 {
		printSprintHelp()
		return nil
	}

	subcommand := args[0]

	if subcommand == "-h" || subcommand == "--help" || subcommand == "help" {
		printSprintHelp()
		return nil
	}

	switch subcommand {
	case "list", "ls":
		return sprintList(args[1:])
	case "create", "new":
		return sprintCreate(args[1:])
	case "get":
		return sprintGet(args[1:])
	case "show":
		return sprintShow(args[1:])
	case "update", "upd":
		return sprintUpdate(args[1:])
	case "remove", "rm":
		return sprintRemove(args[1:])
	case "start":
		return sprintStart(args[1:])
	case "close":
		return sprintClose(args[1:])
	case "reopen":
		return sprintReopen(args[1:])
	case "tasks":
		return sprintTasks(args[1:])
	case "open-tasks":
		return sprintOpenTasks(args[1:])
	case "stats":
		return sprintStats(args[1:])
	case "add-tasks", "add":
		return sprintAddTasks(args[1:])
	case "remove-tasks", "rm-tasks":
		return sprintRemoveTasks(args[1:])
	case "move-tasks", "mv-tasks":
		return sprintMoveTasks(args[1:])
	case "reorder", "order":
		return sprintReorder(args[1:])
	case "move-to", "mvto":
		return sprintMoveTo(args[1:])
	case "swap":
		return sprintSwap(args[1:])
	case "top":
		return sprintTop(args[1:])
	case "bottom", "btm":
		return sprintBottom(args[1:])
	default:
		return fmt.Errorf("%w: unknown sprint subcommand: %s", utils.ErrInvalidInput, subcommand)
	}
}

// printSprintHelp prints sprint command help.
func printSprintHelp() {
	fmt.Print(`Usage: rmp sprint [command] [arguments] [options]

Commands:
  list, ls [OPTIONS]              			List sprints
  create, new [OPTIONS]           			Create a new sprint
  get <id>                        			Get sprint details
  show <id>                       			Show comprehensive sprint report
  update, upd <id> [OPTIONS]       			Update sprint description
  remove, rm <id>                 			Remove sprint
  start <id>                      			Start sprint
  close <id> [--force]            			Close sprint (--force bypasses active-task check)
  reopen <id>                     			Reopen sprint
  tasks <id> [OPTIONS]            			List tasks in sprint (use --order-by-priority for priority ordering)
  open-tasks <id> [OPTIONS]       			List incomplete tasks in sprint (SPRINT, DOING, TESTING only)
  stats <id>                       			Show sprint statistics
  add-tasks, add <sprint> <ids>  			Add tasks to sprint
  remove-tasks, rm-tasks <sprint> <ids>  	Remove tasks from sprint
  move-tasks, mv-tasks <from> <to> <ids>  	Move tasks between sprints
  reorder, order <sprint> <ids>  			Reorder tasks in sprint (comma-separated IDs)
  move-to, mvto <sprint> <task> <pos>  		Move task to specific position
  swap <sprint> <task1> <task2>  			Swap positions of two tasks
  top <sprint> <task>           			Move task to top (position 0)
  bottom, btm <sprint> <task>   			Move task to bottom (last position)

Options:
  -r, --roadmap <name>           			Roadmap name (or use default)
  -d, --description <text>      			Sprint description
  --status <state>               			Filter by status
  --order-by-priority             			Sort by priority (highest first)
  -h, --help                      			Show this help message

Examples:
  rmp sprint list -r myproject
  rmp sprint create -r myproject -d "Sprint 1"
  rmp sprint start 1
  rmp sprint add-tasks 1 1,2,3
  rmp sprint reorder 1 3,1,2
  rmp sprint move-to 1 5 0
  rmp sprint swap 1 3 5
  rmp sprint top 1 5
  rmp sprint bottom 1 5
`)
}
