package commands

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// HandleSprint handles sprint commands.
func HandleSprint(args []string) error {
	if len(args) == 0 {
		printSprintHelp()
		return nil
	}

	subcommand := args[0]

	if subcommand == "-h" || subcommand == "--help" || subcommand == "help" {
		printSprintHelp()
		return nil
	}

	switch subcommand {
	case "list", "ls":
		return sprintList(args[1:])
	case "create", "new":
		return sprintCreate(args[1:])
	case "get":
		return sprintGet(args[1:])
	case "show":
		return sprintShow(args[1:])
	case "update", "upd":
		return sprintUpdate(args[1:])
	case "remove", "rm":
		return sprintRemove(args[1:])
	case "start":
		return sprintStart(args[1:])
	case "close":
		return sprintClose(args[1:])
	case "reopen":
		return sprintReopen(args[1:])
	case "tasks":
		return sprintTasks(args[1:])
	case "stats":
		return sprintStats(args[1:])
	case "add-tasks", "add":
		return sprintAddTasks(args[1:])
	case "remove-tasks", "rm-tasks":
		return sprintRemoveTasks(args[1:])
	case "move-tasks", "mv-tasks":
		return sprintMoveTasks(args[1:])
	case "reorder", "order":
		return sprintReorder(args[1:])
	case "move-to", "mvto":
		return sprintMoveTo(args[1:])
	case "swap":
		return sprintSwap(args[1:])
	case "top":
		return sprintTop(args[1:])
	case "bottom", "btm":
		return sprintBottom(args[1:])
	default:
		return fmt.Errorf("unknown sprint subcommand: %s", subcommand)
	}
}

// sprintList lists sprints.
func sprintList(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	// Parse optional status filter
	var status *models.SprintStatus
	for i := 0; i < len(remaining); i++ {
		if remaining[i] == "--status" && i+1 < len(remaining) {
			s, err := models.ParseSprintStatus(remaining[i+1])
			if err != nil {
				return err
			}
			status = &s
			i++
		}
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	sprints, err := database.ListSprints(ctx, status)
	if err != nil {
		return err
	}

	return utils.PrintJSON(sprints)
}

// sprintCreate creates a new sprint.
func sprintCreate(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	// Parse description
	var description string
	for i := 0; i < len(remaining); i++ {
		if remaining[i] == "-d" || remaining[i] == "--description" {
			if i+1 < len(remaining) {
				description = remaining[i+1]
				i++
			}
		}
	}

	if description == "" {
		return fmt.Errorf("%w: missing required parameter: --description", utils.ErrRequired)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: description,
		CreatedAt:   now,
	}

	if err := sprint.Validate(); err != nil {
		return err
	}

	// Create within transaction with audit
	var sprintID int
	err = database.WithTransaction(func(tx *sql.Tx) error {
		result, err := tx.Exec(
			`INSERT INTO sprints (status, description, created_at) VALUES (?, ?, ?)`,
			sprint.Status, sprint.Description, sprint.CreatedAt,
		)
		if err != nil {
			return err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		sprintID = int(id)

		// Log audit with same timestamp
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpSprintCreate, models.EntitySprint, sprintID, now,
		)
		return err
	})

	if err != nil {
		return err
	}

	return utils.PrintJSON(map[string]int{"id": sprintID})
}

// sprintGet gets a sprint.
func sprintGet(args []string) error {
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

	return utils.PrintJSON(sprint)
}

// sprintShow displays a comprehensive status report of a sprint.
//
// Parameters:
//   - args: Command-line arguments including sprint ID
//
// Required arguments:
//   - sprint ID: The ID of the sprint to show (first positional argument)
//
// Error conditions:
//   - Returns utils.ErrRequired if sprint ID is missing
//   - Returns utils.ErrNotFound if sprint doesn't exist
//
// Output:
//   - JSON object with sprint summary, progress, severity distribution, and criticality distribution
//
// Example:
//
//	rmp sprint show -r myproject 1
func sprintShow(args []string) error {
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

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Get sprint
	sprint, err := database.GetSprint(ctx, sprintID)
	if err != nil {
		return err
	}

	// Get all tasks in sprint
	tasks, err := database.GetSprintTasksFull(ctx, sprintID, nil, false)
	if err != nil {
		return err
	}

	// Calculate comprehensive report
	result := models.CalculateSprintShowResult(sprint, tasks)
	return utils.PrintJSON(result)
}

// sprintUpdate updates a sprint.
func sprintUpdate(args []string) error {
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

	// Parse description
	var description string
	for i := 1; i < len(remaining); i++ {
		if remaining[i] == "-d" || remaining[i] == "--description" {
			if i+1 < len(remaining) {
				description = remaining[i+1]
				i++
			}
		}
	}

	if description == "" {
		return fmt.Errorf("%w: missing required parameter: --description", utils.ErrRequired)
	}

	// Validate description length
	if len(description) > models.MaxSprintDescription {
		return fmt.Errorf("%w: description exceeds maximum length of %d characters", utils.ErrFieldTooLarge, models.MaxSprintDescription)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	// Update within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		result, err := tx.Exec(
			"UPDATE sprints SET description = ? WHERE id = ?",
			description, sprintID,
		)
		if err != nil {
			return err
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("%w: sprint %d not found", utils.ErrNotFound, sprintID)
		}

		// Log audit with same timestamp
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpSprintUpdate, models.EntitySprint, sprintID, now,
		)
		return err
	})
}

// sprintRemove removes a sprint.
func sprintRemove(args []string) error {
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
	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	// Delete within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		// First reset task statuses
		_, err := tx.Exec(
			`UPDATE tasks SET status = 'BACKLOG' WHERE id IN (
				SELECT task_id FROM sprint_tasks WHERE sprint_id = ?
			)`,
			sprintID,
		)
		if err != nil {
			return err
		}

		// Remove sprint_tasks entries
		_, err = tx.Exec("DELETE FROM sprint_tasks WHERE sprint_id = ?", sprintID)
		if err != nil {
			return err
		}

		// Delete sprint
		result, err := tx.Exec("DELETE FROM sprints WHERE id = ?", sprintID)
		if err != nil {
			return err
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("%w: sprint %d not found", utils.ErrNotFound, sprintID)
		}

		// Log audit with same timestamp
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpSprintDelete, models.EntitySprint, sprintID, now,
		)
		return err
	})
}

// sprintStart starts a sprint.
func sprintStart(args []string) error {
	return sprintLifecycle(args, models.SprintOpen, models.OpSprintStart, func(s models.SprintStatus) bool {
		return s.CanStart()
	}, "cannot start sprint with status %s")
}

// sprintClose closes a sprint.
func sprintClose(args []string) error {
	return sprintLifecycle(args, models.SprintClosed, models.OpSprintClose, func(s models.SprintStatus) bool {
		return s.CanClose()
	}, "cannot close sprint with status %s")
}

// sprintReopen reopens a sprint.
func sprintReopen(args []string) error {
	return sprintLifecycle(args, models.SprintOpen, models.OpSprintReopen, func(s models.SprintStatus) bool {
		return s.CanReopen()
	}, "cannot reopen sprint with status %s")
}

// sprintLifecycle handles sprint lifecycle state transitions (start, close, reopen).
//
// Parameters:
//   - args: Command-line arguments including sprint ID
//   - newStatus: The target status to transition to (OPEN, CLOSED)
//   - op: The audit operation type to log (OpSprintStart, OpSprintClose, OpSprintReopen)
//   - canTransition: Function that validates if the transition is allowed from current status
//   - errorMsg: Error message template if transition is not allowed
//
// Required arguments:
//   - sprint ID: The ID of the sprint to transition (first positional argument)
//
// Valid status transitions:
//   - PENDING → OPEN (start sprint)
//   - OPEN → CLOSED (close sprint)
//   - CLOSED → OPEN (reopen sprint)
//
// Error conditions:
//   - Returns utils.ErrRequired if sprint ID is missing
//   - Returns utils.ErrNotFound if sprint doesn't exist
//   - Returns utils.ErrInvalidInput if transition is not allowed
//
// Side effects:
//   - Updates sprint status in database
//   - Sets started_at timestamp when transitioning to OPEN from PENDING
//   - Sets closed_at timestamp when transitioning to CLOSED
//   - Clears closed_at when reopening (transitioning CLOSED → OPEN)
//   - Logs audit entry for the operation
//   - Outputs updated sprint as JSON to stdout
//
// Complexity: O(1) - single database transaction
//
// Example usage:
//
//	sprintLifecycle(args, models.SprintOpen, models.OpSprintStart,
//	    func(s models.SprintStatus) bool { return s == models.SprintPending },
//	    "cannot start sprint with status %s")
//
// buildSprintUpdateQuery builds the UPDATE query and args for sprint status change.
func buildSprintUpdateQuery(newStatus models.SprintStatus, currentStatus models.SprintStatus, now string, sprintID int) (string, []interface{}) {
	switch newStatus {
	case models.SprintOpen:
		if currentStatus == models.SprintClosed {
			return "UPDATE sprints SET status = ?, closed_at = NULL WHERE id = ?", []interface{}{newStatus, sprintID}
		}
		return "UPDATE sprints SET status = ?, started_at = ? WHERE id = ?", []interface{}{newStatus, now, sprintID}
	case models.SprintClosed:
		return "UPDATE sprints SET status = ?, closed_at = ? WHERE id = ?", []interface{}{newStatus, now, sprintID}
	}
	return "", nil
}

// execSprintUpdate executes the sprint update and audit logging in a transaction.
func execSprintUpdate(tx *sql.Tx, query string, args []interface{}, sprintID int, op models.AuditOperation, now string) error {
	if query == "" {
		return fmt.Errorf("invalid sprint status")
	}
	result, err := tx.Exec(query, args...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("%w: sprint %d not found", utils.ErrNotFound, sprintID)
	}
	_, err = tx.Exec(
		`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
		op, models.EntitySprint, sprintID, now,
	)
	return err
}

func sprintLifecycle(args []string, newStatus models.SprintStatus, op models.AuditOperation, canTransition func(models.SprintStatus) bool, errorMsg string) error {
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
	if !canTransition(sprint.Status) {
		return fmt.Errorf("%w: "+errorMsg, utils.ErrInvalidInput, sprint.Status)
	}

	now := utils.NowISO8601()
	query, queryArgs := buildSprintUpdateQuery(newStatus, sprint.Status, now, sprintID)
	return database.WithTransaction(func(tx *sql.Tx) error {
		return execSprintUpdate(tx, query, queryArgs, sprintID, op, now)
	})
}

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

	// Parse optional filters
	var status *models.TaskStatus
	orderByPriority := false
	for i := 1; i < len(remaining); i++ {
		if remaining[i] == "--status" && i+1 < len(remaining) {
			s, err := models.ParseTaskStatus(remaining[i+1])
			if err != nil {
				return err
			}
			status = &s
			i++
		} else if remaining[i] == "--order-by-priority" {
			orderByPriority = true
		}
	}

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

// sprintStats shows sprint statistics.
func sprintStats(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		// Return exit code 1 for missing roadmap in sprint stats (per SPEC/COMMANDS.md)
		// Using fmt.Errorf without sentinel error to get default exit code 1
		if utils.IsNoRoadmap(err) {
			return fmt.Errorf("Roadmap not specified. Use -r flag or set a default with 'rmp roadmap use'")
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

	// Get sprint tasks
	tasks, err := database.GetSprintTasksFull(ctx, sprintID, nil, false)
	if err != nil {
		return err
	}

	stats := models.CalculateSprintStats(sprintID, tasks)
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
//
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

	taskIDs, err := parseTaskIDs(remaining[1])
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

	if _, err = database.GetSprint(ctx, sprintID); err != nil {
		return err
	}
	if err = database.AddTasksToSprint(ctx, sprintID, taskIDs); err != nil {
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

// sprintMoveTasks moves tasks between sprints.
// verifySprintsExist checks that both source and destination sprints exist.
func verifySprintsExist(ctx context.Context, database *db.DB, fromID, toID int) error {
	if _, err := database.GetSprint(ctx, fromID); err != nil {
		return fmt.Errorf("%w: from sprint: %v", utils.ErrNotFound, err)
	}
	if _, err := database.GetSprint(ctx, toID); err != nil {
		return fmt.Errorf("%w: to sprint: %v", utils.ErrNotFound, err)
	}
	return nil
}

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

	if err = verifySprintsExist(ctx, database, fromID, toID); err != nil {
		return err
	}
	if err = database.AddTasksToSprint(ctx, toID, taskIDs); err != nil {
		return err
	}
	return logAuditForTasks(ctx, database, toID, models.OpSprintMoveTask, len(taskIDs))
}

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

// printSprintHelp prints sprint command help.
func printSprintHelp() {
	fmt.Print(`Usage: rmp sprint [command] [arguments] [options]

Commands:
  list, ls [OPTIONS]              List sprints
  create, new [OPTIONS]           Create a new sprint
  get <id>                        Get sprint details
  show <id>                       Show comprehensive sprint report
  update, upd <id> [OPTIONS]       Update sprint description
  remove, rm <id>                 Remove sprint
  start <id>                      Start sprint
  close <id>                      Close sprint
  reopen <id>                     Reopen sprint
  tasks <id> [OPTIONS]            List tasks in sprint (use --order-by-priority for priority ordering)
  stats <id>                       Show sprint statistics
  add-tasks, add <sprint> <ids>  Add tasks to sprint
  remove-tasks, rm-tasks <sprint> <ids>  Remove tasks from sprint
  move-tasks, mv-tasks <from> <to> <ids>  Move tasks between sprints
  reorder, order <sprint> <ids>  Reorder tasks in sprint (comma-separated IDs)
  move-to, mvto <sprint> <task> <pos>  Move task to specific position
  swap <sprint> <task1> <task2>  Swap positions of two tasks
  top <sprint> <task>           Move task to top (position 0)
  bottom, btm <sprint> <task>   Move task to bottom (last position)

Options:
  -r, --roadmap <name>           Roadmap name (or use default)
  -d, --description <text>      Sprint description
  --status <state>               Filter by status
  --order-by-priority             Sort by priority (highest first)
  -h, --help                      Show this help message

Examples:
  rmp sprint list -r myproject
  rmp sprint create -r myproject -d "Sprint 1"
  rmp sprint start 1
  rmp sprint add-tasks 1 1,2,3
  rmp sprint reorder 1 3,1,2
  rmp sprint move-to 1 5 0
  rmp sprint swap 1 3 5
  rmp sprint top 1 5
  rmp sprint bottom 1 5
`)
}
