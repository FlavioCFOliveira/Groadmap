package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// validSortFields holds the accepted values for the --sort flag.
var validSortFields = map[string]bool{
	"priority": true,
	"created":  true,
	"status":   true,
	"severity": true,
}

// taskList lists tasks with optional filters.
func taskList(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	fp := NewFlagParser(TaskListFlags)
	result, err := fp.Parse(remaining)
	if err != nil {
		return err
	}

	filter := db.TaskListFilter{Limit: models.DefaultTaskLimit}

	if statusStr, ok := result.Flags["Status"].(string); ok {
		s, parseErr := models.ParseTaskStatus(statusStr)
		if parseErr != nil {
			// A bad --status enum value is a value-validation failure (exit 6),
			// not a generic runtime error (exit 1). ParseTaskStatus returns a
			// model-level sentinel the exit-code mapper does not recognise, so
			// wrap it in utils.ErrValidation to land on exit 6, matching every
			// other enum filter (e.g. --type) and SPEC/COMMANDS.md.
			// The model sentinel is chained with a SECOND %w, not rendered with
			// %s, so errors.Is can still tell WHICH enum was rejected. Both verbs
			// render the same bytes, so only the chain distinguishes them, and
			// %s silently discards it (task #290).
			return fmt.Errorf("%w: %w", utils.ErrValidation, parseErr)
		}
		filter.Status = &s
	}
	if p, ok := result.Flags["Priority"].(int); ok {
		filter.MinPriority = &p
	}
	if s, ok := result.Flags["Severity"].(int); ok {
		filter.MinSeverity = &s
	}
	if l, ok := result.Flags["Limit"].(int); ok {
		// The bound is not compared here. models.ValidateTaskLimit owns it and
		// words the refusal, so this command, `backlog list` and `audit list`
		// publish one sentence differing only in the maximum
		// (SPEC/COMMANDS.md § List Tasks; rmp task 329).
		if err := models.ValidateTaskLimit(l); err != nil {
			return err
		}
		filter.Limit = l
	}
	if typeStr, ok := result.Flags["Type"].(string); ok {
		tt, parseErr := models.ParseTaskType(typeStr)
		if parseErr != nil {
			return fmt.Errorf("%w: %w", utils.ErrValidation, parseErr)
		}
		filter.TaskType = &tt
	}
	if sinceStr, ok := result.Flags["CreatedSince"].(string); ok {
		t, parseErr := ParseDateFilter("--created-since", sinceStr)
		if parseErr != nil {
			return parseErr
		}
		filter.CreatedSince = &t
	}
	if untilStr, ok := result.Flags["CreatedUntil"].(string); ok {
		t, parseErr := ParseDateFilter("--created-until", untilStr)
		if parseErr != nil {
			return parseErr
		}
		filter.CreatedUntil = &t
	}
	if sortStr, ok := result.Flags["Sort"].(string); ok {
		if !validSortFields[sortStr] {
			return fmt.Errorf("%w: --sort must be one of: priority, created, status, severity", utils.ErrValidation)
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

	// Return array of tasks directly (per SPEC)
	return utils.PrintJSON(tasks)
}

// taskNext retrieves the next N open tasks from the currently open sprint.
//
// Parameters:
//   - args: Command-line arguments including optional num parameter
//
// Optional arguments:
//   - num: Number of tasks to return (default: 1, max: 100)
//
// Error conditions:
//   - Returns utils.ErrNotFound if no sprint is currently open
//   - Returns utils.ErrValidation if num is not a positive integer
//
// Output:
//   - JSON array of Task objects ordered by sprint task position (task_order)
//   - Empty array if sprint has no open tasks
//
// Example:
//
//	rmp task next        # Returns 1 task
//	rmp task next 5      # Returns up to 5 tasks
func taskNext(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	// Parse optional num argument (default: 1). 'num' is a positive-integer
	// domain value, so any invalid form — non-numeric or out of range — is a
	// value-validation failure (exit 6 / ErrValidation per SPEC/ARCHITECTURE.md).
	limit := 1
	if len(remaining) > 0 {
		num, err := strconv.Atoi(remaining[0])
		if err != nil || num < 1 {
			return fmt.Errorf("%w: num must be a positive integer", utils.ErrValidation)
		}
		if num > models.MaxTaskLimit {
			num = models.MaxTaskLimit
		}
		limit = num
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	tasks, err := database.GetNextTasks(ctx, limit)
	if err != nil {
		return err
	}

	return utils.PrintJSON(tasks)
}

// taskGet retrieves tasks by IDs.
func taskGet(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID(s) required", utils.ErrRequired)
	}

	ids, err := utils.ParseCommaSeparatedIDs(remaining[0], "task")
	if err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	tasks, err := database.GetTasks(ctx, ids)
	if err != nil {
		return err
	}

	// Fail-fast: every requested ID must resolve to an existing task. Per
	// SPEC/COMMANDS.md § Get Task(s) (Batch Operation Behavior) and the
	// SPEC/ARCHITECTURE.md error example, any unknown ID — including the
	// all-invalid case, which previously returned null/exit 0 — must fail with
	// exit 4 (utils.ErrNotFound) rather than silently dropping the missing IDs.
	if len(tasks) != len(ids) {
		return fmt.Errorf("%w: some tasks not found", utils.ErrNotFound)
	}

	return utils.PrintJSON(tasks)
}

// taskSubtasks returns all direct subtasks of the given task ID.
func taskSubtasks(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID required", utils.ErrRequired)
	}

	id, err := utils.ValidateIDString(strings.TrimSpace(remaining[0]), "task")
	if err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Verify parent task exists first
	if _, err := database.GetTask(ctx, id); err != nil {
		return err
	}

	subtasks, err := database.GetSubTasks(ctx, id)
	if err != nil {
		return err
	}

	return utils.PrintJSON(subtasks)
}
