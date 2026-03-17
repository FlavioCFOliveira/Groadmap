package commands

import (
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

	sprints, err := database.ListSprints(status)
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

	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: description,
		CreatedAt:   utils.NowISO8601(),
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

		// Log audit
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpSprintCreate, models.EntitySprint, sprintID, utils.NowISO8601(),
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

	sprintID, err := strconv.Atoi(remaining[0])
	if err != nil {
		return fmt.Errorf("%w: invalid sprint ID: %s", utils.ErrInvalidInput, remaining[0])
	}
	// Validate ID is positive and within bounds
	if err := utils.ValidateID(sprintID, "sprint"); err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	sprint, err := database.GetSprint(sprintID)
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

	sprintID, err := strconv.Atoi(remaining[0])
	if err != nil {
		return fmt.Errorf("%w: invalid sprint ID: %s", utils.ErrInvalidInput, remaining[0])
	}
	// Validate ID is positive and within bounds
	if err := utils.ValidateID(sprintID, "sprint"); err != nil {
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

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

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

		// Log audit
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpSprintUpdate, models.EntitySprint, sprintID, utils.NowISO8601(),
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

	sprintID, err := strconv.Atoi(remaining[0])
	if err != nil {
		return fmt.Errorf("%w: invalid sprint ID: %s", utils.ErrInvalidInput, remaining[0])
	}
	// Validate ID is positive and within bounds
	if err := utils.ValidateID(sprintID, "sprint"); err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

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

		// Log audit
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpSprintDelete, models.EntitySprint, sprintID, utils.NowISO8601(),
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

// sprintLifecycle handles sprint lifecycle transitions.
func sprintLifecycle(args []string, newStatus models.SprintStatus, op models.AuditOperation, canTransition func(models.SprintStatus) bool, errorMsg string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: sprint ID required", utils.ErrRequired)
	}

	sprintID, err := strconv.Atoi(remaining[0])
	if err != nil {
		return fmt.Errorf("%w: invalid sprint ID: %s", utils.ErrInvalidInput, remaining[0])
	}
	// Validate ID is positive and within bounds
	if err := utils.ValidateID(sprintID, "sprint"); err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Get current sprint to validate transition
	sprint, err := database.GetSprint(sprintID)
	if err != nil {
		return err
	}

	if !canTransition(sprint.Status) {
		return fmt.Errorf("%w: "+errorMsg, utils.ErrInvalidInput, sprint.Status)
	}

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
				args = []interface{}{newStatus, utils.NowISO8601(), sprintID}
			}
		case models.SprintClosed:
			query = "UPDATE sprints SET status = ?, closed_at = ? WHERE id = ?"
			args = []interface{}{newStatus, utils.NowISO8601(), sprintID}
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

		// Log audit
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			op, models.EntitySprint, sprintID, utils.NowISO8601(),
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

	sprintID, err := strconv.Atoi(remaining[0])
	if err != nil {
		return fmt.Errorf("%w: invalid sprint ID: %s", utils.ErrInvalidInput, remaining[0])
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

	tasks, err := database.GetSprintTasksFull(sprintID, status)
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

	sprintID, err := strconv.Atoi(remaining[0])
	if err != nil {
		return fmt.Errorf("%w: invalid sprint ID: %s", utils.ErrInvalidInput, remaining[0])
	}
	// Validate ID is positive and within bounds
	if err := utils.ValidateID(sprintID, "sprint"); err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Get sprint tasks
	tasks, err := database.GetSprintTasksFull(sprintID, nil)
	if err != nil {
		return err
	}

	stats := models.CalculateSprintStats(sprintID, tasks)
	return utils.PrintJSON(stats)
}

// sprintAddTasks adds tasks to a sprint.
func sprintAddTasks(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: sprint ID and task ID(s) required", utils.ErrRequired)
	}

	sprintID, err := strconv.Atoi(remaining[0])
	if err != nil {
		return fmt.Errorf("%w: invalid sprint ID: %s", utils.ErrInvalidInput, remaining[0])
	}

	// Parse task IDs
	idStrs := strings.Split(remaining[1], ",")
	var taskIDs []int
	for _, s := range idStrs {
		id, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("%w: invalid task ID: %s", utils.ErrInvalidInput, s)
		}
		taskIDs = append(taskIDs, id)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Verify sprint exists
	_, err = database.GetSprint(sprintID)
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

	sprintID, err := strconv.Atoi(remaining[0])
	if err != nil {
		return fmt.Errorf("%w: invalid sprint ID: %s", utils.ErrInvalidInput, remaining[0])
	}

	// Parse task IDs
	idStrs := strings.Split(remaining[1], ",")
	var taskIDs []int
	for _, s := range idStrs {
		id, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("%w: invalid task ID: %s", utils.ErrInvalidInput, s)
		}
		taskIDs = append(taskIDs, id)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Verify sprint exists
	_, err = database.GetSprint(sprintID)
	if err != nil {
		return err
	}

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

			// Log audit
			_, err = tx.Exec(
				`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
				models.OpSprintRemoveTask, models.EntitySprint, sprintID, utils.NowISO8601(),
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

	fromID, err := strconv.Atoi(remaining[0])
	if err != nil {
		return fmt.Errorf("%w: invalid from sprint ID: %s", utils.ErrInvalidInput, remaining[0])
	}
	// Validate ID is positive and within bounds
	if err := utils.ValidateID(fromID, "from sprint"); err != nil {
		return err
	}

	toID, err := strconv.Atoi(remaining[1])
	if err != nil {
		return fmt.Errorf("%w: invalid to sprint ID: %s", utils.ErrInvalidInput, remaining[1])
	}
	// Validate ID is positive and within bounds
	if err := utils.ValidateID(toID, "to sprint"); err != nil {
		return err
	}

	// Parse task IDs
	idStrs := strings.Split(remaining[2], ",")
	var taskIDs []int
	for _, s := range idStrs {
		id, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("%w: invalid task ID: %s", utils.ErrInvalidInput, s)
		}
		taskIDs = append(taskIDs, id)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Verify both sprints exist
	_, err = database.GetSprint(fromID)
	if err != nil {
		return fmt.Errorf("%w: from sprint: %v", utils.ErrNotFound, err)
	}
	_, err = database.GetSprint(toID)
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
