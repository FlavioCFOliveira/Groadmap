package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// sprintReorder reorders tasks in a sprint by defining their exact positions.
//
// Parameters:
//   - args: Command-line arguments including sprint ID and ordered task IDs
//
// Required arguments:
//   - sprint ID: The ID of the sprint to reorder (first positional argument)
//   - task IDs: Comma-separated list of task IDs in desired order (second positional argument)
//
// Validation:
//   - All task IDs must belong to the sprint
//   - No duplicate task IDs allowed
//   - List must include ALL tasks currently in the sprint
//
// Error conditions:
//   - Returns utils.ErrRequired if sprint ID or task IDs missing
//   - Returns utils.ErrNotFound if sprint doesn't exist
//   - Returns utils.ErrInvalidInput if task IDs invalid, duplicated, or incomplete
//
// Side effects:
//   - Updates position field for all tasks in the sprint
//   - Logs SPRINT_REORDER_TASKS audit entry
//   - Outputs success message to stdout
//
// Complexity: O(n) where n is the number of tasks
//
// Example:
//
//	rmp sprint reorder -r myproject 1 5,3,1,2,4
func sprintReorder(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: sprint ID and ordered task ID(s) required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], "sprint")
	if err != nil {
		return err
	}

	// Parse and validate task IDs
	idStrs := strings.Split(remaining[1], ",")
	var taskIDs []int
	seen := make(map[int]bool)
	for _, s := range idStrs {
		id, err := utils.ValidateIDString(strings.TrimSpace(s), "task")
		if err != nil {
			return err
		}
		if seen[id] {
			return fmt.Errorf("%w: duplicate task ID %d", utils.ErrInvalidInput, id)
		}
		seen[id] = true
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

	// Get current tasks in sprint
	currentTaskIDs, err := database.GetSprintTasks(ctx, sprintID)
	if err != nil {
		return err
	}

	// Validate that all sprint tasks are included
	if len(taskIDs) != len(currentTaskIDs) {
		return fmt.Errorf("%w: expected %d task IDs, got %d (must include all sprint tasks)",
			utils.ErrInvalidInput, len(currentTaskIDs), len(taskIDs))
	}

	// Validate all task IDs belong to sprint
	currentSet := make(map[int]bool)
	for _, id := range currentTaskIDs {
		currentSet[id] = true
	}
	for _, id := range taskIDs {
		if !currentSet[id] {
			return fmt.Errorf("%w: task %d does not belong to sprint %d", utils.ErrInvalidInput, id, sprintID)
		}
	}

	// Reorder tasks
	if err := database.ReorderSprintTasks(sprintID, taskIDs); err != nil {
		return err
	}

	return utils.PrintJSON(map[string]interface{}{
		"success":    true,
		"sprint_id":  sprintID,
		"task_order": taskIDs,
	})
}

// sprintMoveTo moves a task to a specific position within a sprint.
//
// Parameters:
//   - args: Command-line arguments including sprint ID, task ID, and position
//
// Required arguments:
//   - sprint ID: The ID of the sprint (first positional argument)
//   - task ID: The ID of the task to move (second positional argument)
//   - position: The target position (0-based, third positional argument)
//
// Error conditions:
//   - Returns utils.ErrRequired if any argument is missing
//   - Returns utils.ErrNotFound if sprint or task doesn't exist
//   - Returns utils.ErrInvalidInput if task doesn't belong to sprint
//
// Side effects:
//   - Updates position field for the moved task and shifted tasks
//   - Logs SPRINT_TASK_MOVE_POSITION audit entry
//
// Example:
//
//	rmp sprint move-to -r myproject 1 5 0    # Move task 5 to position 0 (top)
//	rmp sprint move-to -r myproject 1 5 3    # Move task 5 to position 3
func sprintMoveTo(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 3 {
		return fmt.Errorf("%w: sprint ID, task ID, and position required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], "sprint")
	if err != nil {
		return err
	}

	taskID, err := utils.ValidateIDString(remaining[1], "task")
	if err != nil {
		return err
	}

	position, err := strconv.Atoi(remaining[2])
	if err != nil || position < 0 {
		return fmt.Errorf("%w: position must be a non-negative integer", utils.ErrInvalidInput)
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

	// Verify task belongs to sprint and get task count for position validation
	currentTasks, err := database.GetSprintTasks(ctx, sprintID)
	if err != nil {
		return err
	}

	taskCount := len(currentTasks)
	if position >= taskCount {
		return fmt.Errorf("%w: position must be less than task count (%d)", utils.ErrInvalidInput, taskCount)
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

// sprintSwap swaps the positions of two tasks in a sprint.
//
// Parameters:
//   - args: Command-line arguments including sprint ID and two task IDs
//
// Required arguments:
//   - sprint ID: The ID of the sprint (first positional argument)
//   - task ID 1: The first task ID (second positional argument)
//   - task ID 2: The second task ID (third positional argument)
//
// Validation:
//   - Both tasks must belong to the same sprint
//   - Task IDs must be different
//
// Error conditions:
//   - Returns utils.ErrRequired if any argument is missing
//   - Returns utils.ErrNotFound if sprint doesn't exist
//   - Returns utils.ErrInvalidInput if tasks don't belong to sprint or are identical
//
// Side effects:
//   - Swaps position values of the two tasks
//   - Logs SPRINT_TASK_SWAP audit entry
//
// Example:
//
//	rmp sprint swap -r myproject 1 5 3    # Swap positions of tasks 5 and 3
func sprintSwap(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 3 {
		return fmt.Errorf("%w: sprint ID and two task IDs required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], "sprint")
	if err != nil {
		return err
	}

	taskID1, err := utils.ValidateIDString(remaining[1], "task")
	if err != nil {
		return err
	}

	taskID2, err := utils.ValidateIDString(remaining[2], "task")
	if err != nil {
		return err
	}

	if taskID1 == taskID2 {
		return fmt.Errorf("%w: cannot swap a task with itself", utils.ErrInvalidInput)
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

	// Verify both tasks belong to sprint
	currentTasks, err := database.GetSprintTasks(ctx, sprintID)
	if err != nil {
		return err
	}

	currentSet := make(map[int]bool)
	for _, id := range currentTasks {
		currentSet[id] = true
	}

	if !currentSet[taskID1] {
		return fmt.Errorf("%w: task %d does not belong to sprint %d", utils.ErrInvalidInput, taskID1, sprintID)
	}
	if !currentSet[taskID2] {
		return fmt.Errorf("%w: task %d does not belong to sprint %d", utils.ErrInvalidInput, taskID2, sprintID)
	}

	// Swap tasks
	if err := database.SwapTasks(sprintID, taskID1, taskID2); err != nil {
		return err
	}

	return utils.PrintJSON(map[string]interface{}{
		"success":   true,
		"sprint_id": sprintID,
		"task_id_1": taskID1,
		"task_id_2": taskID2,
	})
}
