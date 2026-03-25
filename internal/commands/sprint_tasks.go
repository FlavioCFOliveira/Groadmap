package commands

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// sprintTasks lists tasks in a sprint.
func sprintTasks(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: sprint ID required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], "sprint")
	if err != nil {
		return err
	}

	fp := NewFlagParser(SprintTasksFlags)
	result, err := fp.Parse(remaining[1:])
	if err != nil {
		return err
	}

	var status *models.TaskStatus
	if statusStr, ok := result.Flags["Status"].(string); ok {
		s, parseErr := models.ParseTaskStatus(statusStr)
		if parseErr != nil {
			return parseErr
		}
		status = &s
	}
	orderByPriority, _ := result.Flags["OrderByPriority"].(bool)

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	tasks, err := database.GetSprintTasksFull(ctx, sprintID, status, orderByPriority)
	if err != nil {
		return err
	}

	return utils.PrintJSON(tasks)
}

// sprintOpenTasks lists incomplete tasks in a sprint (status: SPRINT, DOING, TESTING).
func sprintOpenTasks(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: sprint ID required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], "sprint")
	if err != nil {
		return err
	}

	fp := NewFlagParser(SprintTasksFlags)
	result, err := fp.Parse(remaining[1:])
	if err != nil {
		return err
	}
	orderByPriority, _ := result.Flags["OrderByPriority"].(bool)

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Verify sprint exists before querying tasks.
	if _, err = database.GetSprint(ctx, sprintID); err != nil {
		return err
	}

	tasks, err := database.GetOpenSprintTasks(ctx, sprintID, orderByPriority)
	if err != nil {
		return err
	}

	return utils.PrintJSON(tasks)
}

// sprintStats shows sprint statistics.
func sprintStats(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		// Return exit code 1 for missing roadmap in sprint stats (per SPEC/COMMANDS.md)
		// Using fmt.Errorf without sentinel error to get default exit code 1
		if utils.IsNoRoadmap(err) {
			return fmt.Errorf("%w: use -r <name> or --roadmap <name>", utils.ErrNoRoadmap)
		}
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: sprint ID required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], "sprint")
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

	// Get sprint details for velocity/days_elapsed computation.
	sprint, err := database.GetSprint(ctx, sprintID)
	if err != nil {
		return err
	}

	// Get sprint tasks
	tasks, err := database.GetSprintTasksFull(ctx, sprintID, nil, false)
	if err != nil {
		return err
	}

	// Compute burndown series from task closed_at dates.
	burndown, err := database.GetSprintBurndown(ctx, sprintID)
	if err != nil {
		return err
	}

	stats := models.CalculateSprintStats(sprintID, tasks)
	stats.ApplySprintMetrics(sprint, burndown, utils.NowISO8601())
	return utils.PrintJSON(stats)
}

// parseTaskIDs parses comma-separated task IDs from a string.
func parseTaskIDs(idStr string) ([]int, error) {
	idStrs := strings.Split(idStr, ",")
	taskIDs := make([]int, 0, len(idStrs))
	for _, s := range idStrs {
		id, err := utils.ValidateIDString(strings.TrimSpace(s), "task")
		if err != nil {
			return nil, err
		}
		taskIDs = append(taskIDs, id)
	}
	return taskIDs, nil
}

// logAuditForTasks logs audit entries for multiple tasks in a sprint using batch insert.
func logAuditForTasks(ctx context.Context, database *db.DB, sprintID int, op models.AuditOperation, count int) error {
	if count == 0 {
		return nil
	}

	entries := make([]*models.AuditEntry, count)
	now := utils.NowISO8601()
	for i := 0; i < count; i++ {
		entries[i] = &models.AuditEntry{
			Operation:   string(op),
			EntityType:  string(models.EntitySprint),
			EntityID:    sprintID,
			PerformedAt: now,
		}
	}
	return database.LogAuditEntriesBatch(ctx, entries)
}

// sprintAddTasks adds one or more tasks to a sprint and updates their status.
//
// Parameters:
//   - args: Command-line arguments including sprint ID and task IDs
//
// Required arguments:
//   - sprint ID: The ID of the sprint to add tasks to (first positional argument)
//   - task IDs: Comma-separated list of task IDs to add (second positional argument)
//
// Optional flags:
//   - -r, --roadmap: Roadmap name (uses current if not specified)
//
// Preconditions:
//   - Sprint must exist
//   - Tasks must exist and be in BACKLOG status
//
// Error conditions:
//   - Returns utils.ErrRequired if sprint ID or task IDs missing
//   - Returns utils.ErrNotFound if sprint or tasks don't exist
//   - Returns utils.ErrInvalidInput if task IDs are invalid
//
// Side effects:
//   - Creates sprint_tasks junction records linking tasks to sprint
//   - Updates task status from BACKLOG to SPRINT
//   - Logs TASK_ADDED_TO_SPRINT audit entries for each task
//   - Outputs added task IDs as JSON to stdout
//
// Complexity: O(n) where n is the number of tasks being added
//
// Example:
//
//	rmp sprint add-tasks -r myproject 1 10,11,12
func sprintAddTasks(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}
	if len(remaining) < 2 {
		return fmt.Errorf("%w: sprint ID and task ID(s) required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], "sprint")
	if err != nil {
		return err
	}

	taskIDs, err := parseTaskIDs(strings.Join(remaining[1:], ","))
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

	sprint, err := database.GetSprint(ctx, sprintID)
	if err != nil {
		return err
	}
	if sprint.Status == models.SprintClosed {
		return fmt.Errorf("%w: cannot add tasks to sprint #%d: sprint is CLOSED", utils.ErrInvalidInput, sprintID)
	}

	// Enforce capacity limit when max_tasks is set.
	if sprint.MaxTasks != nil {
		activeTasks, activeErr := database.GetActiveSprintTasks(ctx, sprintID)
		if activeErr != nil {
			return fmt.Errorf("checking sprint capacity: %w", activeErr)
		}
		if len(activeTasks)+len(taskIDs) > *sprint.MaxTasks {
			return fmt.Errorf("%w: adding %d task(s) would exceed sprint #%d capacity (%d/%d tasks active)",
				utils.ErrInvalidInput, len(taskIDs), sprintID, len(activeTasks), *sprint.MaxTasks)
		}
	}

	if err := database.AddTasksToSprint(ctx, sprintID, taskIDs); err != nil {
		return err
	}
	return logAuditForTasks(ctx, database, sprintID, models.OpSprintAddTask, len(taskIDs))
}

// sprintRemoveTasks removes tasks from a sprint.
func sprintRemoveTasks(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: sprint ID and task ID(s) required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], "sprint")
	if err != nil {
		return err
	}

	// Parse and validate task IDs
	idStrs := strings.Split(remaining[1], ",")
	var taskIDs []int
	for _, s := range idStrs {
		id, err := utils.ValidateIDString(strings.TrimSpace(s), "task")
		if err != nil {
			return err
		}
		taskIDs = append(taskIDs, id)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Verify sprint exists
	_, err = database.GetSprint(ctx, sprintID)
	if err != nil {
		return err
	}

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	// Remove within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		for _, taskID := range taskIDs {
			// Remove from sprint_tasks
			_, err := tx.Exec("DELETE FROM sprint_tasks WHERE task_id = ?", taskID)
			if err != nil {
				return err
			}

			// Update task status to BACKLOG
			_, err = tx.Exec("UPDATE tasks SET status = 'BACKLOG' WHERE id = ?", taskID)
			if err != nil {
				return err
			}

			// Log audit with same timestamp
			_, err = tx.Exec(
				`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
				models.OpSprintRemoveTask, models.EntitySprint, sprintID, now,
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// verifySprintsExist checks that both source and destination sprints exist and are not CLOSED.
// Moving tasks to or from a CLOSED sprint would corrupt historical sprint data.
func verifySprintsExist(ctx context.Context, database *db.DB, fromID, toID int) error {
	from, err := database.GetSprint(ctx, fromID)
	if err != nil {
		return fmt.Errorf("%w: from sprint: %v", utils.ErrNotFound, err)
	}
	if from.Status == models.SprintClosed {
		return fmt.Errorf("%w: cannot move tasks from sprint #%d: sprint is CLOSED", utils.ErrInvalidInput, fromID)
	}
	to, err := database.GetSprint(ctx, toID)
	if err != nil {
		return fmt.Errorf("%w: to sprint: %v", utils.ErrNotFound, err)
	}
	if to.Status == models.SprintClosed {
		return fmt.Errorf("%w: cannot move tasks to sprint #%d: sprint is CLOSED", utils.ErrInvalidInput, toID)
	}
	return nil
}

// sprintMoveTasks moves tasks between sprints.
func sprintMoveTasks(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}
	if len(remaining) < 3 {
		return fmt.Errorf("%w: from sprint ID, to sprint ID, and task ID(s) required", utils.ErrRequired)
	}

	fromID, err := utils.ValidateIDString(remaining[0], "sprint")
	if err != nil {
		return err
	}
	toID, err := utils.ValidateIDString(remaining[1], "sprint")
	if err != nil {
		return err
	}

	taskIDs, err := parseTaskIDs(remaining[2])
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

	if err := verifySprintsExist(ctx, database, fromID, toID); err != nil {
		return err
	}
	if err := database.AddTasksToSprint(ctx, toID, taskIDs); err != nil {
		return err
	}
	return logAuditForTasks(ctx, database, toID, models.OpSprintMoveTask, len(taskIDs))
}
