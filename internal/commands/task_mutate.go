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

// taskRemove removes tasks.
func taskRemove(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID(s) required", utils.ErrRequired)
	}

	// Parse and validate IDs (comma-separated)
	idStrs := strings.Split(remaining[0], ",")
	var ids []int
	for _, s := range idStrs {
		id, err := utils.ValidateIDString(strings.TrimSpace(s), "task")
		if err != nil {
			return err
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
//   - COMPLETED → BACKLOG (reopen)
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
//   - Sets started_at when transitioning to DOING
//   - Sets tested_at when transitioning to TESTING
//   - Sets closed_at when transitioning to COMPLETED
//   - Clears lifecycle dates when reopening to BACKLOG
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

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	// Validate status transitions using batch query (O(1) vs N+1)
	tasks, err := database.GetTasks(ctx, ids)
	if err != nil {
		return err
	}
	if len(tasks) != len(ids) {
		return fmt.Errorf("%w: some tasks not found", utils.ErrNotFound)
	}
	for _, task := range tasks {
		if !task.Status.CanTransitionTo(newStatus) {
			return fmt.Errorf("%w: invalid status transition from %s to %s for task %d", utils.ErrInvalidInput, task.Status, newStatus, task.ID)
		}
	}

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	// Update within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		// Build update query based on target status for lifecycle date tracking
		// Per SPEC/STATE_MACHINE.md:
		// - DOING: set started_at
		// - TESTING: set tested_at
		// - COMPLETED: set closed_at
		// - BACKLOG: clear all tracking dates
		var query string
		var args []interface{}

		placeholders := make([]string, len(ids))
		for i := range ids {
			placeholders[i] = "?"
		}

		switch newStatus {
		case models.StatusDoing:
			// Transition to DOING: set started_at
			query = fmt.Sprintf(
				"UPDATE tasks SET status = ?, started_at = ? WHERE id IN (%s)",
				strings.Join(placeholders, ","),
			)
			args = append([]interface{}{newStatus, now}, makeInterfaceSlice(ids)...)

		case models.StatusTesting:
			// Transition to TESTING: set tested_at
			query = fmt.Sprintf(
				"UPDATE tasks SET status = ?, tested_at = ? WHERE id IN (%s)",
				strings.Join(placeholders, ","),
			)
			args = append([]interface{}{newStatus, now}, makeInterfaceSlice(ids)...)

		case models.StatusCompleted:
			// Transition to COMPLETED: set closed_at
			query = fmt.Sprintf(
				"UPDATE tasks SET status = ?, closed_at = ? WHERE id IN (%s)",
				strings.Join(placeholders, ","),
			)
			args = append([]interface{}{newStatus, now}, makeInterfaceSlice(ids)...)

		case models.StatusBacklog:
			// Reopening to BACKLOG: clear all tracking dates
			query = fmt.Sprintf(
				"UPDATE tasks SET status = ?, started_at = NULL, tested_at = NULL, closed_at = NULL WHERE id IN (%s)",
				strings.Join(placeholders, ","),
			)
			args = append([]interface{}{newStatus}, makeInterfaceSlice(ids)...)

		default:
			// Other status changes: just update status
			query = fmt.Sprintf(
				"UPDATE tasks SET status = ? WHERE id IN (%s)",
				strings.Join(placeholders, ","),
			)
			args = append([]interface{}{newStatus}, makeInterfaceSlice(ids)...)
		}

		_, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}

		// Log audit with same timestamp
		for _, id := range ids {
			_, err = tx.Exec(
				`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
				models.OpTaskStatusChange, models.EntityTask, id, now,
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// makeInterfaceSlice converts []int to []interface{}
func makeInterfaceSlice(ids []int) []interface{} {
	result := make([]interface{}, len(ids))
	for i, id := range ids {
		result[i] = id
	}
	return result
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

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

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

		query := fmt.Sprintf("UPDATE tasks SET priority = ? WHERE id IN (%s)", strings.Join(placeholders, ",")) // #nosec G201 -- only ? placeholders interpolated, values are parameterized
		_, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}

		// Log audit with same timestamp
		for _, id := range ids {
			_, err = tx.Exec(
				`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
				models.OpTaskPriorityChange, models.EntityTask, id, now,
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

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

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

		query := fmt.Sprintf("UPDATE tasks SET severity = ? WHERE id IN (%s)", strings.Join(placeholders, ",")) // #nosec G201 -- only ? placeholders interpolated, values are parameterized
		_, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}

		// Log audit with same timestamp
		for _, id := range ids {
			_, err = tx.Exec(
				`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
				models.OpTaskSeverityChange, models.EntityTask, id, now,
			)
			if err != nil {
				return err
			}
		}
		return nil
	})
}
