package commands

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// sprintTop moves a task to the top of the sprint (position 0).
//
// Parameters:
//   - args: Command-line arguments including sprint ID and task ID
//
// Required arguments:
//   - sprint ID: The ID of the sprint (first positional argument)
//   - task ID: The ID of the task to move (second positional argument)
//
// Example:
//
//	rmp sprint top -r myproject 1 5    # Move task 5 to top (position 0)
func sprintTop(args []string) error {
	return sprintMoveToPosition(args, 0)
}

// sprintBottom moves a task to the bottom of the sprint (last position).
//
// Parameters:
//   - args: Command-line arguments including sprint ID and task ID
//
// Required arguments:
//   - sprint ID: The ID of the sprint (first positional argument)
//   - task ID: The ID of the task to move (second positional argument)
//
// Example:
//
//	rmp sprint bottom -r myproject 1 5    # Move task 5 to bottom
func sprintBottom(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: sprint ID and task ID required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], "sprint")
	if err != nil {
		return err
	}

	taskID, err := utils.ValidateIDString(remaining[1], "task")
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

	// Get task count to determine bottom position
	currentTasks, err := database.GetSprintTasks(ctx, sprintID)
	if err != nil {
		return err
	}

	// Verify task belongs to sprint
	found := false
	for _, id := range currentTasks {
		if id == taskID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: task %d does not belong to sprint %d", utils.ErrInvalidInput, taskID, sprintID)
	}

	// Move to bottom (position = count - 1, or use a large number)
	bottomPosition := len(currentTasks) - 1
	if bottomPosition < 0 {
		bottomPosition = 0
	}

	if err := database.MoveTaskToPosition(sprintID, taskID, bottomPosition); err != nil {
		return err
	}

	return utils.PrintJSON(map[string]interface{}{
		"success":   true,
		"sprint_id": sprintID,
		"task_id":   taskID,
		"position":  bottomPosition,
	})
}

// sprintMoveToPosition is a helper that moves a task to a specific position.
func sprintMoveToPosition(args []string, position int) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: sprint ID and task ID required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], "sprint")
	if err != nil {
		return err
	}

	taskID, err := utils.ValidateIDString(remaining[1], "task")
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

	// Verify sprint exists
	_, err = database.GetSprint(ctx, sprintID)
	if err != nil {
		return err
	}

	// Verify task belongs to sprint
	currentTasks, err := database.GetSprintTasks(ctx, sprintID)
	if err != nil {
		return err
	}

	found := false
	for _, id := range currentTasks {
		if id == taskID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: task %d does not belong to sprint %d", utils.ErrInvalidInput, taskID, sprintID)
	}

	// Move task to position
	if err := database.MoveTaskToPosition(sprintID, taskID, position); err != nil {
		return err
	}

	return utils.PrintJSON(map[string]interface{}{
		"success":   true,
		"sprint_id": sprintID,
		"task_id":   taskID,
		"position":  position,
	})
}
