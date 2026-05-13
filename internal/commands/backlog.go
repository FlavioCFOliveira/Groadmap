package commands

import (
	"fmt"
	"strconv"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// BacklogListFlags defines flags for backlog list.
var BacklogListFlags = []FlagDef{
	{Name: "--priority", Short: "-p", Field: "Priority", Type: "int"},
	{Name: "--type", Short: "-y", Field: "Type", Type: "string"},
	{Name: "--sort", Field: "Sort", Type: "string"},
	{Name: "--limit", Short: "-l", Field: "Limit", Type: "int"},
}

// HandleBacklog handles backlog commands.
func HandleBacklog(args []string) error {
	if len(args) == 0 {
		printBacklogHelp()
		return nil
	}

	subcommand := args[0]

	if subcommand == "-h" || subcommand == "--help" || subcommand == "help" {
		printBacklogHelp()
		return nil
	}

	// Subcommand-level help.
	if hasHelpFlag(args[1:]) {
		switch subcommand {
		case "list", "ls":
			printBacklogListHelp()
			return nil
		case "show-next":
			printBacklogShowNextHelp()
			return nil
		}
	}

	switch subcommand {
	case "list", "ls":
		return backlogList(args[1:])
	case "show-next":
		return backlogShowNext(args[1:])
	default:
		return fmt.Errorf("%w: unknown backlog subcommand: %s", utils.ErrInvalidInput, subcommand)
	}
}

// printBacklogListHelp — `rmp backlog list`.
func printBacklogListHelp() {
	fmt.Print(`Usage: rmp backlog list -r <roadmap> [filters]

Lists every task currently in BACKLOG status. Equivalent to
'rmp task list -r <roadmap> --status BACKLOG' but with a focused option
set for the planning view.

Aliases: ls.

Required:
  -r, --roadmap <name>            Target roadmap

Filters:
  -p, --priority <min>            priority >= <min> (0-9)
  -y, --type <type>               One of: USER_STORY, TASK, BUG, SUB_TASK,
                                  EPIC, REFACTOR, CHORE, SPIKE, DESIGN_UX, IMPROVEMENT

Sorting and paging:
  --sort <field>                  priority (default) | created | status | severity
  -l, --limit <n>                 Maximum entries returned (1-100, default 100;
                                  out-of-range values fail with exit 6)

Output (stdout JSON):
  Array of task objects; status is always BACKLOG. See 'rmp task --help'
  for the full task-object key list.

Exit codes:
  0  Success
  3  Missing -r
  6  Bad --type, --sort, --priority, or --limit value

Examples:
  rmp backlog list -r myproject
  rmp backlog list -r myproject --priority 7
  rmp backlog list -r myproject --type BUG --sort severity
  rmp backlog list -r myproject --sort created --limit 50
`)
}

// printBacklogShowNextHelp — `rmp backlog show-next`.
func printBacklogShowNextHelp() {
	fmt.Print(`Usage: rmp backlog show-next -r <roadmap> [count]

Returns the top-<count> BACKLOG tasks by priority DESC, then created_at
ASC for ties. Designed for sprint planning ("what should we pull in next?").

Compared to:
  - 'rmp task next': top tasks from the OPEN sprint (not from BACKLOG).
  - 'rmp backlog list --sort priority': same ordering but no implicit
    limit; useful when filtering and paging are needed.

Required:
  -r, --roadmap <name>            Target roadmap

Optional:
  [count]                         Maximum tasks to return (default 5, max 100;
                                  values above 100 are silently clamped to 100)

Output (stdout JSON):
  Array of task objects (status BACKLOG, ordered by priority DESC).

Exit codes:
  0  Success
  3  Missing -r
  6  Non-positive or non-numeric <count>

Examples:
  rmp backlog show-next -r myproject
  rmp backlog show-next -r myproject 10
`)
}

// backlogList lists tasks with status BACKLOG, with optional filters.
func backlogList(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	fp := NewFlagParser(BacklogListFlags)
	result, err := fp.Parse(remaining)
	if err != nil {
		return err
	}

	backlogStatus := models.StatusBacklog
	filter := db.TaskListFilter{
		Status: &backlogStatus,
		Limit:  models.DefaultTaskLimit,
	}

	if p, ok := result.Flags["Priority"].(int); ok {
		filter.MinPriority = &p
	}
	if l, ok := result.Flags["Limit"].(int); ok {
		if l < 1 || l > models.MaxTaskLimit {
			return fmt.Errorf("%w: limit must be between 1 and %d", utils.ErrInvalidInput, models.MaxTaskLimit)
		}
		filter.Limit = l
	}
	if typeStr, ok := result.Flags["Type"].(string); ok {
		tt, parseErr := models.ParseTaskType(typeStr)
		if parseErr != nil {
			return parseErr
		}
		filter.TaskType = &tt
	}
	if sortStr, ok := result.Flags["Sort"].(string); ok {
		if !validSortFields[sortStr] {
			return fmt.Errorf("%w: --sort must be one of: priority, created, status, severity", utils.ErrInvalidInput)
		}
		filter.Sort = sortStr
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	tasks, err := database.ListTasks(ctx, &filter)
	if err != nil {
		return err
	}

	return utils.PrintJSON(tasks)
}

// backlogShowNext returns the top N BACKLOG tasks ordered by priority for sprint planning.
func backlogShowNext(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	// Parse optional count argument (default: 5)
	count := 5
	if len(remaining) > 0 {
		n, parseErr := strconv.Atoi(remaining[0])
		if parseErr != nil || n < 1 {
			return fmt.Errorf("%w: count must be a positive integer", utils.ErrInvalidInput)
		}
		if n > models.MaxTaskLimit {
			n = models.MaxTaskLimit
		}
		count = n
	}

	backlogStatus := models.StatusBacklog
	filter := db.TaskListFilter{
		Status: &backlogStatus,
		Sort:   "priority",
		Limit:  count,
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	tasks, err := database.ListTasks(ctx, &filter)
	if err != nil {
		return err
	}

	return utils.PrintJSON(tasks)
}

// printBacklogHelp prints backlog command help.
func printBacklogHelp() {
	fmt.Print(`Usage: rmp backlog [command] [arguments] [options]

Commands:
  list, ls [OPTIONS]         	List all tasks in the backlog
  show-next [count] [OPTIONS]  	Show top N backlog tasks by priority for sprint planning

Options:
  -r, --roadmap <name>       	REQUIRED. Target roadmap.
  -p, --priority <min>       	Filter: keep tasks with priority >= <min> (range 0-9)
  -y, --type <type>          	Filter by task type. Valid: USER_STORY, TASK, BUG, SUB_TASK,
                             	EPIC, REFACTOR, CHORE, SPIKE, DESIGN_UX, IMPROVEMENT
  --sort <field>             	Sort order: priority (default), created, status, severity
  -l, --limit <n>            	Maximum number of tasks to return (1-100, default 100;
                             	applies to 'list'. 'show-next' uses its own [count]
                             	positional, default 5, max 100 silently clamped)
  -h, --help                 	Show this help message

Output (stdout JSON):
  Both subcommands return an array of task objects with status == BACKLOG.
  Task object shape matches 'rmp task --help' (Output section).

Exit codes:
  0   Success
  3   No roadmap specified (-r missing)
  6   Validation error (bad type/sort/limit/count value)

Examples:
  rmp backlog list -r groadmap
  rmp backlog list --priority 7 -r groadmap
  rmp backlog list --type BUG -r groadmap
  rmp backlog list --sort priority -r groadmap
  rmp backlog show-next 5 -r groadmap
  rmp backlog show-next 10 -r groadmap
`)
}
