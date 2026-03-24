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

	switch subcommand {
	case "list", "ls":
		return backlogList(args[1:])
	case "show-next":
		return backlogShowNext(args[1:])
	default:
		return fmt.Errorf("%w: unknown backlog subcommand: %s", utils.ErrInvalidInput, subcommand)
	}
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

	tasks, err := database.ListTasks(ctx, filter)
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

	tasks, err := database.ListTasks(ctx, filter)
	if err != nil {
		return err
	}

	return utils.PrintJSON(tasks)
}

// printBacklogHelp prints backlog command help.
func printBacklogHelp() {
	fmt.Print(`Usage: rmp backlog [command] [arguments] [options]

Commands:
  list, ls [OPTIONS]         List all tasks in the backlog
  show-next [count] [OPTIONS]  Show top N backlog tasks by priority for sprint planning

Options:
  -r, --roadmap <name>       Roadmap name (or use default)
  -p, --priority <min>       Filter by minimum priority value
  -y, --type <type>          Filter by task type (TASK, BUG, FEATURE, IMPROVEMENT, SPIKE)
  --sort <field>             Sort order: priority (default), created, status, severity
  -l, --limit <n>            Maximum number of tasks to return
  -h, --help                 Show this help message

Examples:
  rmp backlog list -r groadmap
  rmp backlog list --priority 7 -r groadmap
  rmp backlog list --type BUG -r groadmap
  rmp backlog list --sort priority -r groadmap
  rmp backlog show-next 5 -r groadmap
  rmp backlog show-next 10 -r groadmap
`)
}
