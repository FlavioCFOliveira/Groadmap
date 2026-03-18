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

// HandleTask handles task commands.
func HandleTask(args []string) error {
	if len(args) == 0 {
		printTaskHelp()
		return nil
	}

	subcommand := args[0]

	if subcommand == "-h" || subcommand == "--help" || subcommand == "help" {
		printTaskHelp()
		return nil
	}

	switch subcommand {
	case "list", "ls":
		return taskList(args[1:])
	case "create", "new":
		return taskCreate(args[1:])
	case "get":
		return taskGet(args[1:])
	case "edit":
		return taskEdit(args[1:])
	case "remove", "rm":
		return taskRemove(args[1:])
	case "stat", "set-status":
		return taskSetStatus(args[1:])
	case "prio", "set-priority":
		return taskSetPriority(args[1:])
	case "sev", "set-severity":
		return taskSetSeverity(args[1:])
	default:
		return fmt.Errorf("unknown task subcommand: %s", subcommand)
	}
}

// taskList lists tasks with optional filters.
func taskList(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	// Parse filters
	var status *models.TaskStatus
	var minPriority, minSeverity, limit *int

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
					return fmt.Errorf("invalid limit: %s", remaining[i+1])
				}
				limit = &l
				i++
			}
		}
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	tasks, err := database.ListTasks(status, minPriority, minSeverity, limit)
	if err != nil {
		return err
	}

	return utils.PrintJSON(tasks)
}

// taskCreate creates a new task in the specified or current roadmap.
//
// Parameters:
//   - args: Command-line arguments including flags and roadmap name
//
// Required flags:
//   - -d, --description: Task description (required, max 1000 chars)
//   - -a, --action: Action to be taken (required, max 2000 chars)
//   - -e, --expected-result: Expected outcome (required, max 2000 chars)
//
// Optional flags:
//   - -p, --priority: Task priority 0-9 (default: 0)
//   - --severity: Task severity 0-9 (default: 0)
//   - -s, --specialists: Comma-separated list of specialists (max 500 chars)
//   - -r, --roadmap: Roadmap name (uses current if not specified)
//
// Error conditions:
//   - Returns utils.ErrRequired if required fields are missing
//   - Returns utils.ErrInvalidInput if priority/severity are out of range
//   - Returns utils.ErrFieldTooLarge if text fields exceed limits
//   - Returns utils.ErrNoRoadmap if no roadmap specified and none selected
//
// Side effects:
//   - Creates task record in database
//   - Logs TASK_CREATE audit entry
//   - Outputs created task as JSON to stdout
//
// Complexity: O(1) - single database insert
//
// Example:
//
//	rmp task create -r myproject -d "Fix bug" -a "Update code" -e "Bug fixed" -p 5 --severity 3
func taskCreate(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	// Parse flags
	var description, action, expectedResult string
	var specialists string
	priority := 0
	severity := 0

	for i := 0; i < len(remaining); i++ {
		switch remaining[i] {
		case "-d", "--description":
			if i+1 < len(remaining) {
				description = remaining[i+1]
				i++
			}
		case "-a", "--action":
			if i+1 < len(remaining) {
				action = remaining[i+1]
				i++
			}
		case "-e", "--expected-result":
			if i+1 < len(remaining) {
				expectedResult = remaining[i+1]
				i++
			}
		case "-p", "--priority":
			if i+1 < len(remaining) {
				p, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid priority: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				priority = p
				i++
			}
		case "--severity":
			if i+1 < len(remaining) {
				s, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid severity: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				severity = s
				i++
			}
		case "-sp", "--specialists":
			if i+1 < len(remaining) {
				specialists = remaining[i+1]
				i++
			}
		}
	}

	// Validate required fields
	if description == "" {
		return fmt.Errorf("%w: missing required parameter: --description", utils.ErrRequired)
	}
	if action == "" {
		return fmt.Errorf("%w: missing required parameter: --action", utils.ErrRequired)
	}
	if expectedResult == "" {
		return fmt.Errorf("%w: missing required parameter: --expected-result", utils.ErrRequired)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	task := &models.Task{
		Priority:       priority,
		Severity:       severity,
		Status:         models.StatusBacklog,
		Description:    description,
		Action:         action,
		ExpectedResult: expectedResult,
		CreatedAt:      utils.NowISO8601(),
	}

	if specialists != "" {
		task.Specialists = &specialists
	}

	if err := task.Validate(); err != nil {
		return err
	}

	// Create task within transaction
	var taskID int
	err = database.WithTransaction(func(tx *sql.Tx) error {
		// Insert task
		result, err := tx.Exec(
			`INSERT INTO tasks (priority, severity, description, specialists, action, expected_result, created_at, status)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			task.Priority, task.Severity, task.Description, task.Specialists,
			task.Action, task.ExpectedResult, task.CreatedAt, task.Status,
		)
		if err != nil {
			return err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		taskID = int(id)

		// Log audit
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpTaskCreate, models.EntityTask, taskID, utils.NowISO8601(),
		)
		return err
	})

	if err != nil {
		return err
	}

	return utils.PrintJSON(map[string]int{"id": taskID})
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

	// Parse IDs (comma-separated)
	idStrs := strings.Split(remaining[0], ",")
	var ids []int
	for _, s := range idStrs {
		id, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("%w: invalid task ID: %s", utils.ErrInvalidInput, s)
		}
		ids = append(ids, id)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	tasks, err := database.GetTasks(ids)
	if err != nil {
		return err
	}

	return utils.PrintJSON(tasks)
}

// taskEdit modifies an existing task's fields.
//
// Parameters:
//   - args: Command-line arguments including task ID and optional flags
//
// Required arguments:
//   - task ID: The ID of the task to edit (first positional argument)
//
// Optional flags (at least one required):
//   - -d, --description: New task description (max 1000 chars)
//   - -a, --action: New action (max 2000 chars)
//   - -e, --expected-result: New expected result (max 2000 chars)
//   - -s, --specialists: New specialists list (max 500 chars)
//   - -p, --priority: New priority 0-9
//   - --severity: New severity 0-9
//   - -r, --roadmap: Roadmap name (uses current if not specified)
//
// Error conditions:
//   - Returns utils.ErrRequired if task ID is missing
//   - Returns utils.ErrNotFound if task doesn't exist
//   - Returns utils.ErrInvalidInput if priority/severity out of range
//   - Returns utils.ErrFieldTooLarge if text fields exceed limits
//
// Side effects:
//   - Updates task record in database
//   - Logs TASK_UPDATE audit entry
//   - Outputs updated task as JSON to stdout
//
// Complexity: O(1) - single database update
//
// Example:
//
//	rmp task edit -r myproject 42 -d "Updated description" -p 8
func taskEdit(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID required", utils.ErrRequired)
	}

	taskID, err := strconv.Atoi(remaining[0])
	if err != nil {
		return fmt.Errorf("%w: invalid task ID: %s", utils.ErrInvalidInput, remaining[0])
	}

	// Parse optional fields
	updates := make(map[string]interface{})

	for i := 1; i < len(remaining); i++ {
		switch remaining[i] {
		case "-d", "--description":
			if i+1 < len(remaining) {
				updates["description"] = remaining[i+1]
				i++
			}
		case "-a", "--action":
			if i+1 < len(remaining) {
				updates["action"] = remaining[i+1]
				i++
			}
		case "-e", "--expected-result":
			if i+1 < len(remaining) {
				updates["expected_result"] = remaining[i+1]
				i++
			}
		case "-p", "--priority":
			if i+1 < len(remaining) {
				p, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid priority: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				updates["priority"] = p
				i++
			}
		case "--severity":
			if i+1 < len(remaining) {
				s, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid severity: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				updates["severity"] = s
				i++
			}
		case "-sp", "--specialists":
			if i+1 < len(remaining) {
				updates["specialists"] = remaining[i+1]
				i++
			}
		}
	}

	if len(updates) == 0 {
		return fmt.Errorf("%w: no fields to update", utils.ErrInvalidInput)
	}

	// Validate required fields are not empty
	requiredFields := map[string]string{
		"description":     "description",
		"action":          "action",
		"expected_result": "expected-result",
	}
	for field, flagName := range requiredFields {
		if value, ok := updates[field]; ok {
			if str, ok := value.(string); ok && str == "" {
				return fmt.Errorf("%w: %s cannot be empty", utils.ErrValidation, flagName)
			}
		}
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Update within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		// Update task
		setParts := []string{}
		args := []interface{}{}
		for field, value := range updates {
			setParts = append(setParts, fmt.Sprintf("%s = ?", field))
			args = append(args, value)
		}
		args = append(args, taskID)

		query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", strings.Join(setParts, ", "))
		result, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("%w: task %d not found", utils.ErrNotFound, taskID)
		}

		// Log audit
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpTaskUpdate, models.EntityTask, taskID, utils.NowISO8601(),
		)
		return err
	})
}

// taskRemove removes tasks.
func taskRemove(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID(s) required", utils.ErrRequired)
	}

	// Parse IDs (comma-separated)
	idStrs := strings.Split(remaining[0], ",")
	var ids []int
	for _, s := range idStrs {
		id, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("%w: invalid task ID: %s", utils.ErrInvalidInput, s)
		}
		ids = append(ids, id)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Delete within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		for _, id := range ids {
			// Delete task
			result, err := tx.Exec("DELETE FROM tasks WHERE id = ?", id)
			if err != nil {
				return err
			}

			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected == 0 {
				return fmt.Errorf("%w: task %d not found", utils.ErrNotFound, id)
			}

			// Log audit
			_, err = tx.Exec(
				`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
				models.OpTaskDelete, models.EntityTask, id, utils.NowISO8601(),
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// taskSetStatus changes the status of one or more tasks.
//
// Parameters:
//   - args: Command-line arguments including task IDs and new status
//
// Required arguments:
//   - task IDs: Comma-separated list of task IDs to update (first positional argument)
//   - status: New status value (second positional argument)
//
// Valid status transitions:
//   - BACKLOG → SPRINT, DOING
//   - SPRINT → DOING, BACKLOG
//   - DOING → TESTING, BACKLOG
//   - TESTING → COMPLETED, DOING
//   - COMPLETED → DOING (reopen)
//
// Optional flags:
//   - -r, --roadmap: Roadmap name (uses current if not specified)
//
// Error conditions:
//   - Returns utils.ErrRequired if task IDs or status missing
//   - Returns utils.ErrNotFound if task doesn't exist
//   - Returns utils.ErrInvalidInput if status is invalid
//   - Returns error if status transition is not allowed
//
// Side effects:
//   - Updates task status in database
//   - Sets completed_at timestamp when transitioning to COMPLETED
//   - Clears completed_at when transitioning from COMPLETED
//   - Logs TASK_STATUS_CHANGE audit entry
//   - Outputs updated task IDs as JSON to stdout
//
// Complexity: O(n) where n is the number of tasks being updated
//
// Example:
//
//	rmp task set-status -r myproject 1,2,3 DOING
func taskSetStatus(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID(s) and status required", utils.ErrRequired)
	}

	// Parse IDs
	idStrs := strings.Split(remaining[0], ",")
	var ids []int
	for _, s := range idStrs {
		id, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("%w: invalid task ID: %s", utils.ErrInvalidInput, s)
		}
		ids = append(ids, id)
	}

	// Parse status
	newStatus, err := models.ParseTaskStatus(remaining[1])
	if err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Validate status transitions
	for _, id := range ids {
		task, err := database.GetTask(id)
		if err != nil {
			return err
		}
		if !task.Status.CanTransitionTo(newStatus) {
			return fmt.Errorf("%w: invalid status transition from %s to %s for task %d", utils.ErrInvalidInput, task.Status, newStatus, id)
		}
	}

	// Update within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		// Update tasks
		placeholders := make([]string, len(ids))
		args := make([]interface{}, len(ids)+2)
		args[0] = newStatus
		if newStatus == models.StatusCompleted {
			args[1] = utils.NowISO8601()
		} else {
			args[1] = nil
		}
		for i, id := range ids {
			placeholders[i] = "?"
			args[i+2] = id
		}

		query := fmt.Sprintf(
			"UPDATE tasks SET status = ?, completed_at = CASE WHEN ? = 'COMPLETED' THEN ? ELSE NULL END WHERE id IN (%s)",
			strings.Join(placeholders, ","),
		)
		// args[0] = newStatus, args[1] = newStatus (for CASE), args[2] = completed_at or nil, args[3+] = ids
		execArgs := []interface{}{newStatus, newStatus, args[1]}
		for _, id := range ids {
			execArgs = append(execArgs, id)
		}
		_, err := tx.Exec(query, execArgs...)
		if err != nil {
			return err
		}

		// Log audit
		for _, id := range ids {
			_, err = tx.Exec(
				`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
				models.OpTaskStatusChange, models.EntityTask, id, utils.NowISO8601(),
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// taskSetPriority sets task priority.
func taskSetPriority(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID(s) and priority required", utils.ErrRequired)
	}

	// Parse IDs
	idStrs := strings.Split(remaining[0], ",")
	var ids []int
	for _, s := range idStrs {
		id, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("%w: invalid task ID: %s", utils.ErrInvalidInput, s)
		}
		ids = append(ids, id)
	}

	// Parse priority
	priority, err := strconv.Atoi(remaining[1])
	if err != nil || priority < 0 || priority > 9 {
		return fmt.Errorf("%w: invalid priority: must be 0-9", utils.ErrInvalidInput)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Update within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		// Update tasks
		placeholders := make([]string, len(ids))
		args := make([]interface{}, len(ids)+1)
		args[0] = priority
		for i, id := range ids {
			placeholders[i] = "?"
			args[i+1] = id
		}

		query := fmt.Sprintf("UPDATE tasks SET priority = ? WHERE id IN (%s)", strings.Join(placeholders, ","))
		_, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}

		// Log audit
		for _, id := range ids {
			_, err = tx.Exec(
				`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
				models.OpTaskPriorityChange, models.EntityTask, id, utils.NowISO8601(),
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// taskSetSeverity sets task severity.
func taskSetSeverity(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID(s) and severity required", utils.ErrRequired)
	}

	// Parse IDs
	idStrs := strings.Split(remaining[0], ",")
	var ids []int
	for _, s := range idStrs {
		id, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("%w: invalid task ID: %s", utils.ErrInvalidInput, s)
		}
		ids = append(ids, id)
	}

	// Parse severity
	severity, err := strconv.Atoi(remaining[1])
	if err != nil || severity < 0 || severity > 9 {
		return fmt.Errorf("%w: invalid severity: must be 0-9", utils.ErrInvalidInput)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Update within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		// Update tasks
		placeholders := make([]string, len(ids))
		args := make([]interface{}, len(ids)+1)
		args[0] = severity
		for i, id := range ids {
			placeholders[i] = "?"
			args[i+1] = id
		}

		query := fmt.Sprintf("UPDATE tasks SET severity = ? WHERE id IN (%s)", strings.Join(placeholders, ","))
		_, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}

		// Log audit
		for _, id := range ids {
			_, err = tx.Exec(
				`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
				models.OpTaskSeverityChange, models.EntityTask, id, utils.NowISO8601(),
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// printTaskHelp prints task command help.
func printTaskHelp() {
	fmt.Print(`Usage: rmp task [command] [arguments] [options]

Commands:
  list, ls [OPTIONS]              List tasks
  create, new [OPTIONS]           Create a new task
  get <ids>                      Get tasks by ID(s)
  edit <id> [OPTIONS]             Edit a task
  remove, rm <ids>               Remove task(s)
  stat, set-status <ids> <status>  Set task status
  prio, set-priority <ids> <prio>    Set task priority
  sev, set-severity <ids> <sev>     Set task severity

Options:
  -r, --roadmap <name>           Roadmap name (or use default)
  -s, --status <state>            Filter by status
  -p, --priority <n>              Filter/set priority (0-9)
  --severity <n>                  Filter/set severity (0-9)
  -d, --description <text>        Task description
  -a, --action <text>             Action to take
  -e, --expected-result <text>    Expected result
  -sp, --specialists <list>       Comma-separated specialists
  -l, --limit <n>                 Limit results
  -h, --help                      Show this help message

Examples:
  rmp task list -r myproject
  rmp task create -r myproject -d "Fix bug" -a "Debug" -e "No more bugs"
  rmp task stat 1,2,3 DOING
`)
}
