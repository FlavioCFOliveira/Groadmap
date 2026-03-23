package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// taskList lists tasks with optional filters.
func taskList(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	// Parse filters
	var status *models.TaskStatus
	var minPriority, minSeverity *int
	limit := models.DefaultTaskLimit // Default limit per SPEC

	for i := 0; i < len(remaining); i++ {
		switch remaining[i] {
		case "-s", "--status":
			if i+1 < len(remaining) {
				s, err := models.ParseTaskStatus(remaining[i+1])
				if err != nil {
					return err
				}
				status = &s
				i++
			}
		case "-p", "--priority":
			if i+1 < len(remaining) {
				p, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid priority: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				minPriority = &p
				i++
			}
		case "--severity":
			if i+1 < len(remaining) {
				s, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid severity: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				minSeverity = &s
				i++
			}
		case "-l", "--limit":
			if i+1 < len(remaining) {
				l, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid limit: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				if l < 1 || l > models.MaxTaskLimit {
					return fmt.Errorf("%w: limit must be between 1 and %d", utils.ErrInvalidInput, models.MaxTaskLimit)
				}
				limit = l
				i++
			}
		}
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	tasks, err := database.ListTasks(ctx, status, minPriority, minSeverity, limit)
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
