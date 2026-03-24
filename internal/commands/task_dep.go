package commands

import (
	"fmt"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// taskAddDep adds a dependency: the task at taskID depends on depID.
// Usage: rmp task add-dep <task-id> <dep-id> [-r <roadmap>]
func taskAddDep(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID and dependency ID required", utils.ErrRequired)
	}

	taskID, err := utils.ValidateIDString(strings.TrimSpace(remaining[0]), "task")
	if err != nil {
		return err
	}

	depID, err := utils.ValidateIDString(strings.TrimSpace(remaining[1]), "dependency task")
	if err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	// Verify both tasks exist before adding dependency.
	if _, err := database.GetTask(ctx, taskID); err != nil {
		return fmt.Errorf("task #%d not found: %w", taskID, err)
	}
	if _, err := database.GetTask(ctx, depID); err != nil {
		return fmt.Errorf("dependency task #%d not found: %w", depID, err)
	}

	return database.AddTaskDependencyWithAudit(ctx, taskID, depID)
}

// taskRemoveDep removes a dependency: taskID no longer depends on depID.
// Usage: rmp task remove-dep <task-id> <dep-id> [-r <roadmap>]
func taskRemoveDep(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID and dependency ID required", utils.ErrRequired)
	}

	taskID, err := utils.ValidateIDString(strings.TrimSpace(remaining[0]), "task")
	if err != nil {
		return err
	}

	depID, err := utils.ValidateIDString(strings.TrimSpace(remaining[1]), "dependency task")
	if err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	return database.RemoveTaskDependencyWithAudit(ctx, taskID, depID)
}

// taskBlockers lists tasks that are blocking the given task (tasks that this task depends on
// and are not yet COMPLETED).
// Usage: rmp task blockers <task-id> [-r <roadmap>]
func taskBlockers(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID required", utils.ErrRequired)
	}

	taskID, err := utils.ValidateIDString(strings.TrimSpace(remaining[0]), "task")
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

	// Verify task exists
	if _, err := database.GetTask(ctx, taskID); err != nil {
		return err
	}

	blockers, err := database.GetBlockers(ctx, taskID)
	if err != nil {
		return err
	}

	return utils.PrintJSON(blockers)
}

// taskBlocking lists tasks that this task is blocking (tasks that depend on this task).
// Usage: rmp task blocking <task-id> [-r <roadmap>]
func taskBlocking(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID required", utils.ErrRequired)
	}

	taskID, err := utils.ValidateIDString(strings.TrimSpace(remaining[0]), "task")
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

	// Verify task exists
	if _, err := database.GetTask(ctx, taskID); err != nil {
		return err
	}

	blocking, err := database.GetBlocking(ctx, taskID)
	if err != nil {
		return err
	}

	return utils.PrintJSON(blocking)
}
