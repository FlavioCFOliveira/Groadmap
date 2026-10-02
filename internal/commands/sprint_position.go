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
	remaining, strays := splitPositionals(remaining, 2)

	if len(remaining) < 2 {
		return fmt.Errorf("%w: sprint ID and task ID required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], utils.FieldSprintID)
	if err != nil {
		return err
	}

	taskID, err := utils.ValidateIDString(remaining[1], utils.FieldTaskID)
	if err != nil {
		return err
	}

	// A "-"-prefixed token between or after the positional arguments stands in
	// no slot and is refused before the roadmap is opened
	// (SPEC/COMMANDS.md § Positional Arguments).
	if err := rejectUnknownFlags(strays); err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Verify sprint exists first, mirroring sprintMoveToPosition (used by
	// `top`/`move-to`). Without this, a non-existent sprint yields an empty
	// task set from GetSprintTasks and the membership check below would
	// report a misleading "task is not in sprint" (exit 6) instead
	// of the correct "sprint not found" (exit 4). Keeping the check here
	// makes `top` and `bottom` behave identically for a missing sprint.
	if err := database.CheckSprintExists(ctx, sprintID); err != nil {
		return err
	}

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
		return utils.TasksNotInSprintError([]int{taskID}, sprintID)
	}

	// Move to bottom (position = count - 1, or use a large number)
	bottomPosition := len(currentTasks) - 1
	if bottomPosition < 0 {
		bottomPosition = 0
	}

	if err := database.MoveTaskToPosition(sprintID, taskID, bottomPosition); err != nil {
		return err
	}

	return utils.PrintJSON(sprintPositionResult{Position: bottomPosition, SprintID: sprintID, Success: true, TaskID: taskID})
}

// sprintMoveToPosition is a helper that moves a task to a specific position.
func sprintMoveToPosition(args []string, position int) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}
	remaining, strays := splitPositionals(remaining, 2)

	if len(remaining) < 2 {
		return fmt.Errorf("%w: sprint ID and task ID required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], utils.FieldSprintID)
	if err != nil {
		return err
	}

	taskID, err := utils.ValidateIDString(remaining[1], utils.FieldTaskID)
	if err != nil {
		return err
	}

	// See sprintBottom: a stray "-"-prefixed token is refused before the roadmap.
	if err := rejectUnknownFlags(strays); err != nil {
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
	err = database.CheckSprintExists(ctx, sprintID)
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
		return utils.TasksNotInSprintError([]int{taskID}, sprintID)
	}

	// Move task to position
	if err := database.MoveTaskToPosition(sprintID, taskID, position); err != nil {
		return err
	}

	return utils.PrintJSON(sprintPositionResult{Position: position, SprintID: sprintID, Success: true, TaskID: taskID})
}
