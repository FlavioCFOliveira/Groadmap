package commands

import (
	"database/sql"
	"fmt"
	"os"
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

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Fail-fast: verify all tasks exist and are in BACKLOG before deleting any (task #78).
	tasks, err := database.GetTasks(ctx, ids)
	if err != nil {
		return err
	}
	if len(tasks) != len(ids) {
		return fmt.Errorf("%w: some tasks not found", utils.ErrNotFound)
	}
	for _, task := range tasks {
		if task.Status != models.StatusBacklog {
			return fmt.Errorf("%w: task #%d cannot be deleted — status is %s, must be BACKLOG", utils.ErrInvalidInput, task.ID, task.Status)
		}
	}

	// Guard: prevent deleting tasks that have subtasks.
	for _, task := range tasks {
		hasChildren, childCount, subErr := database.HasSubTasks(ctx, task.ID)
		if subErr != nil {
			return subErr
		}
		if hasChildren {
			return fmt.Errorf("%w: task #%d cannot be deleted — it has %d subtask(s); remove them first", utils.ErrInvalidInput, task.ID, childCount)
		}
	}

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

	// Extract --summary / -s flag before positional arg parsing.
	// Fail-fast: all validation happens before any database operation.
	var completionSummary *string
	filtered := make([]string, 0, len(remaining))
	for i := 0; i < len(remaining); i++ {
		if remaining[i] == "--summary" || remaining[i] == "-s" {
			if i+1 >= len(remaining) {
				return fmt.Errorf("%w: --summary requires a value", utils.ErrRequired)
			}
			s := remaining[i+1]
			completionSummary = &s
			i++ // consume the value
		} else {
			filtered = append(filtered, remaining[i])
		}
	}
	remaining = filtered

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

	// Fail-fast validation for --summary (step 2: before ID/DB verification).
	// --summary is only meaningful on the TESTING → COMPLETED transition.
	if completionSummary != nil && newStatus != models.StatusCompleted {
		return fmt.Errorf("%w: --summary is only valid when transitioning to COMPLETED", utils.ErrInvalidInput)
	}
	if completionSummary != nil && len(*completionSummary) > models.MaxTaskCompletionSummary {
		return fmt.Errorf("%w: completion_summary exceeds maximum length of %d characters", utils.ErrFieldTooLarge, models.MaxTaskCompletionSummary)
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

	// Guard: when transitioning to COMPLETED, ensure all subtasks and dependencies are also COMPLETED.
	if newStatus == models.StatusCompleted {
		for _, task := range tasks {
			incompleteIDs, subErr := database.GetIncompleteSubTasks(ctx, task.ID)
			if subErr != nil {
				return subErr
			}
			if len(incompleteIDs) > 0 {
				idStrsBlocking := make([]string, len(incompleteIDs))
				for i, id := range incompleteIDs {
					idStrsBlocking[i] = fmt.Sprintf("#%d", id)
				}
				return fmt.Errorf("%w: cannot mark task #%d as COMPLETED: incomplete subtasks: %s",
					utils.ErrInvalidInput, task.ID, strings.Join(idStrsBlocking, ", "))
			}

			// Check task dependencies: all tasks this task depends on must be COMPLETED.
			incompleteDeps, depErr := database.GetIncompleteDependencies(ctx, task.ID)
			if depErr != nil {
				return depErr
			}
			if len(incompleteDeps) > 0 {
				depStrs := make([]string, len(incompleteDeps))
				for i, id := range incompleteDeps {
					depStrs[i] = fmt.Sprintf("#%d", id)
				}
				return fmt.Errorf("%w: cannot mark task #%d as COMPLETED: incomplete dependencies: %s",
					utils.ErrInvalidInput, task.ID, strings.Join(depStrs, ", "))
			}
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
		// - COMPLETED: set closed_at and completion_summary (nil → NULL)
		// - BACKLOG: clear all tracking dates (completion_summary cleared in task #96)
		var query string
		var args []interface{}

		placeholders := make([]string, len(ids))
		for i := range ids {
			placeholders[i] = "?"
		}

		switch newStatus {
		case models.StatusDoing:
			// Transition to DOING: set started_at
			query = fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
				"UPDATE tasks SET status = ?, started_at = ? WHERE id IN (%s)",
				strings.Join(placeholders, ","),
			)
			args = append([]interface{}{newStatus, now}, makeInterfaceSlice(ids)...)

		case models.StatusTesting:
			// Transition to TESTING: set tested_at
			query = fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
				"UPDATE tasks SET status = ?, tested_at = ? WHERE id IN (%s)",
				strings.Join(placeholders, ","),
			)
			args = append([]interface{}{newStatus, now}, makeInterfaceSlice(ids)...)

		case models.StatusCompleted:
			// Transition to COMPLETED: set closed_at and completion_summary.
			// completionSummary is *string: nil becomes SQL NULL, non-nil becomes the string value.
			query = fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
				"UPDATE tasks SET status = ?, closed_at = ?, completion_summary = ? WHERE id IN (%s)",
				strings.Join(placeholders, ","),
			)
			args = append([]interface{}{newStatus, now, completionSummary}, makeInterfaceSlice(ids)...)

		case models.StatusBacklog:
			// Reopening to BACKLOG: clear all tracking dates and completion_summary for a fresh cycle
			query = fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
				"UPDATE tasks SET status = ?, started_at = NULL, tested_at = NULL, closed_at = NULL, completion_summary = NULL WHERE id IN (%s)",
				strings.Join(placeholders, ","),
			)
			args = append([]interface{}{newStatus}, makeInterfaceSlice(ids)...)

		default:
			// Other status changes: just update status
			query = fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
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

// taskReopen transitions one or more tasks back to BACKLOG, clearing all lifecycle timestamps.
// Tasks already in BACKLOG are skipped with an informational message.
// Accepts comma-separated IDs with fail-fast on any invalid ID.
func taskReopen(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID(s) required", utils.ErrRequired)
	}

	idStrs := strings.Split(remaining[0], ",")
	ids := make([]int, 0, len(idStrs))
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

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	tasks, err := database.GetTasks(ctx, ids)
	if err != nil {
		return err
	}
	if len(tasks) != len(ids) {
		return fmt.Errorf("%w: some tasks not found", utils.ErrNotFound)
	}

	// Separate already-BACKLOG tasks from tasks that need transition.
	// Track which tasks are in sprint-associated states so we can clean up sprint_tasks rows.
	var toReopen []int
	var toRemoveFromSprint []int
	for _, task := range tasks {
		if task.Status == models.StatusBacklog {
			fmt.Fprintf(os.Stderr, "task #%d is already in BACKLOG\n", task.ID)
			continue
		}
		toReopen = append(toReopen, task.ID)
		// Tasks in SPRINT, DOING, or TESTING have a row in sprint_tasks that must be removed.
		if task.Status == models.StatusSprint || task.Status == models.StatusDoing || task.Status == models.StatusTesting {
			toRemoveFromSprint = append(toRemoveFromSprint, task.ID)
		}
	}

	if len(toReopen) == 0 {
		return nil
	}

	now := utils.NowISO8601()

	return database.WithTransaction(func(tx *sql.Tx) error {
		placeholders := make([]string, len(toReopen))
		for i := range toReopen {
			placeholders[i] = "?"
		}

		query := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
			"UPDATE tasks SET status = ?, started_at = NULL, tested_at = NULL, closed_at = NULL, completion_summary = NULL WHERE id IN (%s)",
			strings.Join(placeholders, ","),
		)
		args := append([]interface{}{models.StatusBacklog}, makeInterfaceSlice(toReopen)...)
		if _, err := tx.Exec(query, args...); err != nil {
			return err
		}

		// Remove sprint_tasks rows for tasks that were associated with a sprint.
		if len(toRemoveFromSprint) > 0 {
			sprintPlaceholders := make([]string, len(toRemoveFromSprint))
			for i := range toRemoveFromSprint {
				sprintPlaceholders[i] = "?"
			}
			delQuery := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
				"DELETE FROM sprint_tasks WHERE task_id IN (%s)",
				strings.Join(sprintPlaceholders, ","),
			)
			if _, err := tx.Exec(delQuery, makeInterfaceSlice(toRemoveFromSprint)...); err != nil {
				return err
			}
		}

		for _, id := range toReopen {
			if _, err := tx.Exec(
				`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
				models.OpTaskReopen, models.EntityTask, id, now,
			); err != nil {
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
