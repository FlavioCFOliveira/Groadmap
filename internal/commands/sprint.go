package commands

import (
	"database/sql"
	"fmt"
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

	// Get current sprint to validate transition
	sprint, err := database.GetSprint(ctx, sprintID)
	if err != nil {
		return err
	}

	if !canTransition(sprint.Status) {
		return fmt.Errorf("%w: "+errorMsg, utils.ErrInvalidInput, sprint.Status)
	}

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	// Update within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		var query string
		var args []interface{}

		switch newStatus {
		case models.SprintOpen:
			if sprint.Status == models.SprintClosed {
				// Reopening - clear closed_at
				query = "UPDATE sprints SET status = ?, closed_at = NULL WHERE id = ?"
			} else {
				// Starting - set started_at
				query = "UPDATE sprints SET status = ?, started_at = ? WHERE id = ?"
				args = []interface{}{newStatus, now, sprintID}
			}
		case models.SprintClosed:
			query = "UPDATE sprints SET status = ?, closed_at = ? WHERE id = ?"
			args = []interface{}{newStatus, now, sprintID}
		}

		if len(args) == 0 {
			args = []interface{}{newStatus, sprintID}
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

		// Log audit with same timestamp
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			op, models.EntitySprint, sprintID, now,
		)
		return err
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

	// Parse optional status filter
	var status *models.TaskStatus
	for i := 1; i < len(remaining); i++ {
		if remaining[i] == "--status" && i+1 < len(remaining) {
			s, err := models.ParseTaskStatus(remaining[i+1])
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

	tasks, err := database.GetSprintTasksFull(ctx, sprintID, status)
	if err != nil {
		return err
	}

	return utils.PrintJSON(tasks)
}

// sprintStats shows sprint statistics.
func sprintStats(args []string) error {
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

	// Get sprint tasks
	tasks, err := database.GetSprintTasksFull(ctx, sprintID, nil)
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

	// Add within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		now := utils.NowISO8601()

		for _, taskID := range taskIDs {
			// Add to sprint_tasks
			_, err := tx.Exec(
				`INSERT INTO sprint_tasks (sprint_id, task_id, added_at) VALUES (?, ?, ?)
				 ON CONFLICT(task_id) DO UPDATE SET sprint_id = excluded.sprint_id, added_at = excluded.added_at`,
				sprintID, taskID, now,
			)
			if err != nil {
				return err
			}

			// Update task status
			_, err = tx.Exec("UPDATE tasks SET status = 'SPRINT' WHERE id = ?", taskID)
			if err != nil {
				return err
			}

			// Log audit
			_, err = tx.Exec(
				`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
				models.OpSprintAddTask, models.EntitySprint, sprintID, now,
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
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

	// Parse and validate task IDs
	idStrs := strings.Split(remaining[2], ",")
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

	// Verify both sprints exist
	_, err = database.GetSprint(ctx, fromID)
	if err != nil {
		return fmt.Errorf("%w: from sprint: %v", utils.ErrNotFound, err)
	}
	_, err = database.GetSprint(ctx, toID)
	if err != nil {
		return fmt.Errorf("%w: to sprint: %v", utils.ErrNotFound, err)
	}

	// Move within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		now := utils.NowISO8601()

		for _, taskID := range taskIDs {
			// Update sprint_tasks
			_, err := tx.Exec(
				`INSERT INTO sprint_tasks (sprint_id, task_id, added_at) VALUES (?, ?, ?)
				 ON CONFLICT(task_id) DO UPDATE SET sprint_id = excluded.sprint_id, added_at = excluded.added_at`,
				toID, taskID, now,
			)
			if err != nil {
				return err
			}

			// Log audit
			_, err = tx.Exec(
				`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
				models.OpSprintMoveTask, models.EntitySprint, toID, now,
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// printSprintHelp prints sprint command help.
func printSprintHelp() {
	fmt.Print(`Usage: rmp sprint [command] [arguments] [options]

Commands:
  list, ls [OPTIONS]              List sprints
  create, new [OPTIONS]           Create a new sprint
  get <id>                        Get sprint details
  update, upd <id> [OPTIONS]       Update sprint description
  remove, rm <id>                 Remove sprint
  start <id>                      Start sprint
  close <id>                      Close sprint
  reopen <id>                     Reopen sprint
  tasks <id> [OPTIONS]            List tasks in sprint
  stats <id>                       Show sprint statistics
  add-tasks, add <sprint> <ids>  Add tasks to sprint
  remove-tasks, rm-tasks <sprint> <ids>  Remove tasks from sprint
  move-tasks, mv-tasks <from> <to> <ids>  Move tasks between sprints

Options:
  -r, --roadmap <name>           Roadmap name (or use default)
  -d, --description <text>      Sprint description
  --status <state>               Filter by status
  -h, --help                      Show this help message

Examples:
  rmp sprint list -r myproject
  rmp sprint create -r myproject -d "Sprint 1"
  rmp sprint start 1
  rmp sprint add-tasks 1 1,2,3
`)
}
