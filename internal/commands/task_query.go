package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ErrInvalidDateFormat indicates that a date string does not match any accepted format.
var ErrInvalidDateFormat = errors.New("invalid date format: expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01)")

// validSortFields holds the accepted values for the --sort flag.
var validSortFields = map[string]bool{
	"priority": true,
	"created":  true,
	"status":   true,
	"severity": true,
}

// parseFilterDate parses a date string for --created-since / --created-until.
// Accepts full ISO 8601 / RFC3339 strings and date-only strings (YYYY-MM-DD).
// Date-only values are interpreted as the start of that day in UTC.
func parseFilterDate(s string) (time.Time, error) {
	t, err := utils.ParseISO8601(s)
	if err == nil {
		return t, nil
	}
	t, dateErr := time.Parse("2006-01-02", s)
	if dateErr != nil {
		return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidDateFormat, s)
	}
	return t.UTC(), nil
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
			return parseErr
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
	if sp, ok := result.Flags["Specialists"].(string); ok {
		filter.Specialists = &sp
	}
	if sinceStr, ok := result.Flags["CreatedSince"].(string); ok {
		t, parseErr := parseFilterDate(sinceStr)
		if parseErr != nil {
			return fmt.Errorf("%w: --created-since: %v", utils.ErrInvalidInput, parseErr)
		}
		filter.CreatedSince = &t
	}
	if untilStr, ok := result.Flags["CreatedUntil"].(string); ok {
		t, parseErr := parseFilterDate(untilStr)
		if parseErr != nil {
			return fmt.Errorf("%w: --created-until: %v", utils.ErrInvalidInput, parseErr)
		}
		filter.CreatedUntil = &t
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
//   - Returns utils.ErrInvalidInput if num is not a positive integer
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

	// Parse optional num argument (default: 1)
	limit := 1
	if len(remaining) > 0 {
		num, err := strconv.Atoi(remaining[0])
		if err != nil || num < 1 {
			return fmt.Errorf("%w: num must be a positive integer", utils.ErrInvalidInput)
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

	// Parse and validate IDs (comma-separated)
	idStrs := strings.Split(remaining[0], ",")
	var ids []int
	for _, s := range idStrs {
		id, err := utils.ValidateIDString(strings.TrimSpace(s), "task")
		if err != nil {
			return err
		}
		ids = append(ids, id)
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
