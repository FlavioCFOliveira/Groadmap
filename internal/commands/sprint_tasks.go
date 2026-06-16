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
//   - Returns utils.ErrInvalidInput if a task ID token is malformed (non-numeric)
//   - Returns utils.ErrValidation if a task ID is out of range, or the sprint
//     is CLOSED, or the add would exceed the sprint's max-tasks capacity
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

	taskIDs, err := utils.ParseCommaSeparatedIDs(strings.Join(remaining[1:], ","), "task")
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
		return fmt.Errorf("%w: cannot add tasks to sprint #%d: sprint is CLOSED", utils.ErrValidation, sprintID)
	}

	// Fail-fast: confirm every task exists before any mutation. Without this,
	// the SQLite FOREIGN KEY constraint surfaces a generic DB error (exit 1)
	// instead of the documented utils.ErrNotFound (exit 4).
	existing, err := database.GetTasks(ctx, taskIDs)
	if err != nil {
		return err
	}
	if len(existing) != len(taskIDs) {
		found := make(map[int]struct{}, len(existing))
		for i := range existing {
			found[existing[i].ID] = struct{}{}
		}
		missing := make([]int, 0, len(taskIDs)-len(existing))
		for _, id := range taskIDs {
			if _, ok := found[id]; !ok {
				missing = append(missing, id)
			}
		}
		return fmt.Errorf("%w: task(s) not found: %v", utils.ErrNotFound, missing)
	}

	// Friendly capacity pre-check when max_tasks is set. This is a fast
	// feedback path only: the authoritative, race-free enforcement lives inside
	// AddTasksToSprint's transaction (SPEC/DATABASE.md § Transactional Atomicity
	// Guarantees #3, finding #67), which closes the TOCTOU window that this
	// standalone read cannot. The error contract here matches the transactional
	// one so the message is identical regardless of which check trips first.
	if sprint.MaxTasks != nil {
		activeTasks, activeErr := database.GetActiveSprintTasks(ctx, sprintID)
		if activeErr != nil {
			return fmt.Errorf("checking sprint capacity: %w", activeErr)
		}
		if len(activeTasks)+len(taskIDs) > *sprint.MaxTasks {
			return fmt.Errorf("%w: adding %d task(s) would exceed sprint #%d capacity (%d/%d tasks active)",
				utils.ErrValidation, len(taskIDs), sprintID, len(activeTasks), *sprint.MaxTasks)
		}
	}

	// AddTasksToSprint writes the membership change AND its SPRINT_ADD_TASK
	// audit entries inside one transaction, so the audit can never be lost
	// after a committed insert (SPEC/DATABASE.md § Transactional Atomicity
	// Guarantees #4).
	return database.AddTasksToSprint(ctx, sprintID, taskIDs)
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

	taskIDs, err := utils.ParseCommaSeparatedIDs(remaining[1], "task")
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

	// Verify sprint exists. Note: removing tasks from a CLOSED sprint is
	// intentionally allowed — the SPEC/COMMANDS.md validation order for
	// remove-tasks does not block CLOSED sprints, and the documented
	// task-carryover workflow (move incomplete tasks out of a closed sprint
	// into the next one) depends on it.
	if _, err := database.GetSprint(ctx, sprintID); err != nil {
		return err
	}

	// Fail-fast membership check: every task must currently belong to THIS
	// sprint. Previously the DELETE ignored the sprint argument and removed by
	// task_id alone, silently yanking a task out of whatever sprint it was
	// actually in (data corruption). SPEC/COMMANDS.md § Sprint Task Management
	// validation step 5 ("Task ID not in sprint -> exit 6"); finding #40.
	members, err := database.GetSprintTasks(ctx, sprintID)
	if err != nil {
		return err
	}
	memberSet := make(map[int]struct{}, len(members))
	for _, id := range members {
		memberSet[id] = struct{}{}
	}
	missing := make([]int, 0, len(taskIDs))
	for _, id := range taskIDs {
		if _, ok := memberSet[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: task(s) not in sprint #%d: %v", utils.ErrValidation, sprintID, missing)
	}

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	// Remove within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		for _, taskID := range taskIDs {
			// Remove from sprint_tasks, scoped to the named sprint.
			if _, err := tx.Exec(
				"DELETE FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?",
				sprintID, taskID,
			); err != nil {
				return err
			}

			// Reset the task to BACKLOG, clearing ALL lifecycle timestamps and
			// the completion summary. A task may have progressed to
			// DOING/TESTING/COMPLETED while in the sprint, so leaving those
			// fields populated on a BACKLOG task violates the state machine's
			// reopening invariant (SPEC/STATE_MACHINE.md Reopening Behavior;
			// finding #49). For an unstarted SPRINT task these are already NULL,
			// so the clear is a harmless no-op.
			if _, err := tx.Exec(
				`UPDATE tasks SET status = 'BACKLOG', started_at = NULL, tested_at = NULL,
				        closed_at = NULL, completion_summary = NULL WHERE id = ?`,
				taskID,
			); err != nil {
				return err
			}

			if err := db.LogAuditTx(tx, models.OpSprintRemoveTask, models.EntitySprint, sprintID, now); err != nil {
				return err
			}
		}

		// Compact the remaining positions to a contiguous 0..N-1 sequence so
		// later sprint move-to operations order correctly (finding #50).
		return db.CompactSprintPositionsTx(tx, sprintID)
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
		return fmt.Errorf("%w: cannot move tasks from sprint #%d: sprint is CLOSED", utils.ErrValidation, fromID)
	}
	to, err := database.GetSprint(ctx, toID)
	if err != nil {
		return fmt.Errorf("%w: to sprint: %v", utils.ErrNotFound, err)
	}
	if to.Status == models.SprintClosed {
		return fmt.Errorf("%w: cannot move tasks to sprint #%d: sprint is CLOSED", utils.ErrValidation, toID)
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

	taskIDs, err := utils.ParseCommaSeparatedIDs(remaining[2], "task")
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
	// Move (re-parent) the tasks from the source sprint to the destination,
	// preserving each task's status. Unlike AddTasksToSprint, this neither
	// forces status to SPRINT nor applies the destination's max-tasks cap;
	// it also validates that every task is currently in the source sprint.
	// MoveTasksBetweenSprints writes the re-parenting AND its SPRINT_MOVE_TASK
	// audit entries inside one transaction, so the audit can never be lost
	// after a committed move (SPEC/DATABASE.md § Transactional Atomicity
	// Guarantees #5).
	return database.MoveTasksBetweenSprints(ctx, fromID, toID, taskIDs)
}
