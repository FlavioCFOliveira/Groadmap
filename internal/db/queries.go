// Package db provides SQLite database connectivity and operations.
//
// Resource Management Pattern:
// When querying multiple rows, always use defer at the FUNCTION level (not inside loops):
//
//	rows, err := db.Query(...)
//	if err != nil {
//	    return err
//	}
//	defer rows.Close()  // This runs when the FUNCTION returns
//	for rows.Next() {   // Loop through results
//	    // process row
//	}
//
// This pattern ensures:
// - Resources are released when the function exits
// - No resource accumulation in loops
// - Proper cleanup even if errors occur during iteration
//
// NEVER use defer inside a loop - it will accumulate defers until the function returns.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== TASK QUERIES ====================

// CreateTask inserts a new task and returns its ID.
func (db *DB) CreateTask(ctx context.Context, task *models.Task) (int, error) {
	var taskID int
	err := retryWithBackoff("create task", func() error {
		result, err := db.ExecContext(ctx,
			`INSERT INTO tasks (priority, severity, description, specialists, action, expected_result, created_at, status)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			task.Priority,
			task.Severity,
			task.Description,
			task.Specialists,
			task.Action,
			task.ExpectedResult,
			task.CreatedAt,
			task.Status,
		)
		if err != nil {
			return fmt.Errorf("inserting task: %w", err)
		}

		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("getting last insert id: %w", err)
		}

		taskID = int(id)
		return nil
	})

	if err != nil {
		return 0, err
	}

	return taskID, nil
}

// GetTask retrieves a task by ID.
func (db *DB) GetTask(ctx context.Context, id int) (*models.Task, error) {
	var task models.Task
	var specialists sql.NullString
	var completedAt sql.NullString

	err := db.QueryRowContext(ctx,
		`SELECT id, priority, severity, status, description, specialists, action, expected_result, created_at, completed_at
		 FROM tasks WHERE id = ?`,
		id,
	).Scan(
		&task.ID,
		&task.Priority,
		&task.Severity,
		&task.Status,
		&task.Description,
		&specialists,
		&task.Action,
		&task.ExpectedResult,
		&task.CreatedAt,
		&completedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: task %d", utils.ErrNotFound, id)
		}
		return nil, fmt.Errorf("querying task: %w", err)
	}

	if specialists.Valid {
		task.Specialists = &specialists.String
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.String
	}

	return &task, nil
}

// GetTasks retrieves multiple tasks by IDs.
func (db *DB) GetTasks(ctx context.Context, ids []int) ([]models.Task, error) {
	if len(ids) == 0 {
		return []models.Task{}, nil
	}

	// Use cached placeholders for better performance
	placeholders := db.queryCache.GetPlaceholders(len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT id, priority, severity, status, description, specialists, action, expected_result, created_at, completed_at
		 FROM tasks WHERE id IN (%s) ORDER BY id`,
		placeholders,
	)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

// ListTasks retrieves tasks with optional filters.
// Returns tasks and error.
// Filters: status, minPriority, minSeverity
func (db *DB) ListTasks(ctx context.Context, status *models.TaskStatus, minPriority, minSeverity *int, limit int) ([]models.Task, error) {
	if limit < 1 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}

	query := `SELECT id, priority, severity, status, description, specialists, action, expected_result, created_at, completed_at
		      FROM tasks WHERE 1=1`
	args := []interface{}{}

	if status != nil {
		query += " AND status = ?"
		args = append(args, string(*status))
	}
	if minPriority != nil {
		query += " AND priority >= ?"
		args = append(args, *minPriority)
	}
	if minSeverity != nil {
		query += " AND severity >= ?"
		args = append(args, *minSeverity)
	}

	query += " ORDER BY priority DESC, created_at ASC"
	query += " LIMIT ?"
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

// allowedTaskUpdateFields contains the whitelist of fields that can be updated via UpdateTask.
var allowedTaskUpdateFields = map[string]bool{
	"description":     true,
	"action":          true,
	"expected_result": true,
	"specialists":     true,
	"priority":        true,
	"severity":        true,
}

// UpdateTask updates a task's fields with the provided values.
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - id: The unique identifier of the task to update
//   - updates: A map of field names to new values. Only whitelisted fields can be updated.
//
// Allowed fields (whitelisted):
//   - "description": Task description (string, max 1000 chars)
//   - "action": Action to be taken (string, max 2000 chars)
//   - "expected_result": Expected outcome (string, max 2000 chars)
//   - "specialists": Comma-separated list of specialists (string, max 500 chars)
//   - "priority": Task priority 0-9 (int)
//   - "severity": Task severity 0-9 (int)
//
// Error conditions:
//   - Returns utils.ErrInvalidUpdate if a non-whitelisted field is specified
//   - Returns utils.ErrNotFound if task with given ID doesn't exist
//   - Returns wrapped database errors for connection/query failures
//
// Side effects:
//   - Updates task record in database
//   - Does NOT update status (use UpdateTaskStatus for that)
//   - Does NOT create audit entries (caller should log changes)
//
// Complexity: O(n) where n is the number of fields being updated
func (db *DB) UpdateTask(ctx context.Context, id int, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	// Validate that only whitelisted fields are being updated
	for field := range updates {
		if !allowedTaskUpdateFields[field] {
			return fmt.Errorf("%w: field %q cannot be updated via UpdateTask (use dedicated method)", utils.ErrInvalidUpdate, field)
		}
	}

	return retryWithBackoff("update task", func() error {
		setParts := []string{}
		args := []interface{}{}

		for field, value := range updates {
			setParts = append(setParts, fmt.Sprintf("%s = ?", field))
			args = append(args, value)
		}

		args = append(args, id)
		query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", strings.Join(setParts, ", "))

		result, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("updating task: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking rows affected: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("%w: task %d", utils.ErrNotFound, id)
		}

		return nil
	})
}

// UpdateTaskStruct updates a task using the type-safe TaskUpdate struct.
// This is the recommended approach over UpdateTask (map-based) as it provides:
// - Compile-time type safety
// - Deterministic SQL generation (fields always in same order)
// - No interface{} boxing overhead
// - Clear intent through pointer fields (nil = no change)
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - id: The unique identifier of the task to update
//   - update: TaskUpdate struct with pointer fields indicating which values to update
//
// Returns:
//   - nil on success
//   - utils.ErrNotFound if task doesn't exist
//   - utils.ErrInvalidUpdate if no fields are set to update
//   - Validation error if field values exceed limits
//   - Wrapped database errors for connection/query failures
func (db *DB) UpdateTaskStruct(ctx context.Context, id int, update *models.TaskUpdate) error {
	if update == nil || !update.HasChanges() {
		return fmt.Errorf("%w: no fields specified for update", utils.ErrInvalidUpdate)
	}

	if err := update.Validate(); err != nil {
		return fmt.Errorf("%w: %v", utils.ErrInvalidUpdate, err)
	}

	return retryWithBackoff("update task struct", func() error {
		// Build SQL with deterministic field ordering
		// Fields are always in the same order: description, action, expected_result, specialists, priority, severity
		var setParts []string
		var args []interface{}

		if update.Description != nil {
			setParts = append(setParts, "description = ?")
			args = append(args, *update.Description)
		}
		if update.Action != nil {
			setParts = append(setParts, "action = ?")
			args = append(args, *update.Action)
		}
		if update.ExpectedResult != nil {
			setParts = append(setParts, "expected_result = ?")
			args = append(args, *update.ExpectedResult)
		}
		if update.Specialists != nil {
			setParts = append(setParts, "specialists = ?")
			args = append(args, *update.Specialists)
		}
		if update.Priority != nil {
			setParts = append(setParts, "priority = ?")
			args = append(args, *update.Priority)
		}
		if update.Severity != nil {
			setParts = append(setParts, "severity = ?")
			args = append(args, *update.Severity)
		}

		args = append(args, id)
		query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", strings.Join(setParts, ", "))

		result, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("updating task: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking rows affected: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("%w: task %d", utils.ErrNotFound, id)
		}

		return nil
	})
}

// UpdateTaskStatus updates task status and optionally sets completed_at.
func (db *DB) UpdateTaskStatus(ctx context.Context, ids []int, status models.TaskStatus) error {
	if len(ids) == 0 {
		return nil
	}

	return retryWithBackoff("update task status", func() error {
		// Use cached placeholders for better performance
		placeholders := db.queryCache.GetPlaceholders(len(ids))
		idArgs := make([]interface{}, len(ids))
		for i, id := range ids {
			idArgs[i] = id
		}

		// Set completed_at based on status
		var completedAt interface{}
		if status == models.StatusCompleted {
			completedAt = utils.NowISO8601()
		} else {
			completedAt = nil
		}

		// Args: status, completed_at, then all IDs
		args := append([]interface{}{status, completedAt}, idArgs...)

		query := fmt.Sprintf(
			"UPDATE tasks SET status = ?, completed_at = ? WHERE id IN (%s)",
			placeholders,
		)

		_, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("updating task status: %w", err)
		}

		return nil
	})
}

// UpdateTaskPriority updates task priority.
func (db *DB) UpdateTaskPriority(ctx context.Context, ids []int, priority int) error {
	if len(ids) == 0 {
		return nil
	}

	return retryWithBackoff("update task priority", func() error {
		placeholders := db.queryCache.GetPlaceholders(len(ids))
		args := make([]interface{}, len(ids)+1)
		args[0] = priority
		for i, id := range ids {
			args[i+1] = id
		}

		query := fmt.Sprintf("UPDATE tasks SET priority = ? WHERE id IN (%s)", placeholders)
		_, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("updating task priority: %w", err)
		}
		return nil
	})
}

// UpdateTaskSeverity updates task severity.
func (db *DB) UpdateTaskSeverity(ctx context.Context, ids []int, severity int) error {
	if len(ids) == 0 {
		return nil
	}

	return retryWithBackoff("update task severity", func() error {
		placeholders := db.queryCache.GetPlaceholders(len(ids))
		args := make([]interface{}, len(ids)+1)
		args[0] = severity
		for i, id := range ids {
			args[i+1] = id
		}

		query := fmt.Sprintf("UPDATE tasks SET severity = ? WHERE id IN (%s)", placeholders)
		_, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("updating task severity: %w", err)
		}
		return nil
	})
}

// DeleteTask deletes a task by ID.
func (db *DB) DeleteTask(ctx context.Context, id int) error {
	return retryWithBackoff("delete task", func() error {
		result, err := db.ExecContext(ctx, "DELETE FROM tasks WHERE id = ?", id)
		if err != nil {
			return fmt.Errorf("deleting task: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking rows affected: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("%w: task %d", utils.ErrNotFound, id)
		}

		return nil
	})
}

// scanTasks scans rows into a slice of Task.
// Optimized for memory efficiency with pre-allocated slice and reusable scan variables.
func scanTasks(rows *sql.Rows) ([]models.Task, error) {
	// Pre-allocate with typical batch size to avoid repeated reallocations
	tasks := make([]models.Task, 0, 100)

	// Reusable scan variables to avoid allocations per iteration
	var specialists sql.NullString
	var completedAt sql.NullString

	for rows.Next() {
		var task models.Task

		// Reset scan variables for each row
		specialists = sql.NullString{}
		completedAt = sql.NullString{}

		err := rows.Scan(
			&task.ID,
			&task.Priority,
			&task.Severity,
			&task.Status,
			&task.Description,
			&specialists,
			&task.Action,
			&task.ExpectedResult,
			&task.CreatedAt,
			&completedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning task row: %w", err)
		}

		if specialists.Valid {
			task.Specialists = &specialists.String
		}
		if completedAt.Valid {
			task.CompletedAt = &completedAt.String
		}

		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task rows: %w", err)
	}

	return tasks, nil
}

// ==================== SPRINT QUERIES ====================

// CreateSprint inserts a new sprint and returns its ID.
func (db *DB) CreateSprint(ctx context.Context, sprint *models.Sprint) (int, error) {
	var sprintID int
	err := retryWithBackoff("create sprint", func() error {
		result, err := db.ExecContext(ctx,
			`INSERT INTO sprints (status, description, created_at) VALUES (?, ?, ?)`,
			sprint.Status,
			sprint.Description,
			sprint.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting sprint: %w", err)
		}

		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("getting last insert id: %w", err)
		}

		sprintID = int(id)
		return nil
	})

	if err != nil {
		return 0, err
	}

	return sprintID, nil
}

// GetSprint retrieves a sprint by ID.
// Optimized to use a single query with JSON aggregation for tasks (SQLite 3.38+).
// This eliminates the N+1 query pattern by fetching sprint and task IDs in one round-trip.
func (db *DB) GetSprint(ctx context.Context, id int) (*models.Sprint, error) {
	var sprint models.Sprint
	var startedAt sql.NullString
	var closedAt sql.NullString
	var tasksJSON sql.NullString

	// Single query using JSON aggregation to get sprint data and task IDs
	// json_group_array returns a JSON array of task IDs
	err := db.QueryRowContext(ctx,
		`SELECT
			s.id, s.status, s.description, s.created_at, s.started_at, s.closed_at,
			COALESCE(json_group_array(DISTINCT st.task_id), '[]') as tasks
		 FROM sprints s
		 LEFT JOIN sprint_tasks st ON s.id = st.sprint_id
		 WHERE s.id = ?
		 GROUP BY s.id`,
		id,
	).Scan(
		&sprint.ID,
		&sprint.Status,
		&sprint.Description,
		&sprint.CreatedAt,
		&startedAt,
		&closedAt,
		&tasksJSON,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: sprint %d", utils.ErrNotFound, id)
		}
		return nil, fmt.Errorf("querying sprint: %w", err)
	}

	if startedAt.Valid {
		sprint.StartedAt = &startedAt.String
	}
	if closedAt.Valid {
		sprint.ClosedAt = &closedAt.String
	}

	// Parse task IDs from JSON array
	if tasksJSON.Valid && tasksJSON.String != "" && tasksJSON.String != "[]" {
		tasks, err := parseJSONIntArray(tasksJSON.String)
		if err != nil {
			return nil, fmt.Errorf("parsing sprint tasks: %w", err)
		}
		sprint.Tasks = tasks
		sprint.TaskCount = len(tasks)
	} else {
		sprint.Tasks = []int{}
		sprint.TaskCount = 0
	}

	return &sprint, nil
}

// parseJSONIntArray parses a JSON array of integers into a Go []int.
// Example: '[1,2,3]' -> []int{1, 2, 3}
// Handles edge cases like '[null]' (empty result from json_group_array).
func parseJSONIntArray(jsonStr string) ([]int, error) {
	if jsonStr == "" || jsonStr == "[]" || jsonStr == "[null]" {
		return []int{}, nil
	}

	// Remove brackets and split by comma
	trimmed := strings.Trim(jsonStr, "[]")
	if trimmed == "" || trimmed == "null" {
		return []int{}, nil
	}

	parts := strings.Split(trimmed, ",")
	result := make([]int, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "null" {
			continue
		}
		val, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("parsing integer %q: %w", part, err)
		}
		result = append(result, val)
	}

	return result, nil
}

// ListSprints retrieves all sprints with optional status filter.
func (db *DB) ListSprints(ctx context.Context, status *models.SprintStatus) ([]models.Sprint, error) {
	query := `SELECT id, status, description, created_at, started_at, closed_at FROM sprints WHERE 1=1`
	args := []interface{}{}

	if status != nil {
		query += " AND status = ?"
		args = append(args, string(*status))
	}

	query += " ORDER BY created_at DESC"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing sprints: %w", err)
	}
	defer rows.Close()

	var sprints []models.Sprint
	for rows.Next() {
		var sprint models.Sprint
		var startedAt sql.NullString
		var closedAt sql.NullString

		err := rows.Scan(
			&sprint.ID,
			&sprint.Status,
			&sprint.Description,
			&sprint.CreatedAt,
			&startedAt,
			&closedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning sprint row: %w", err)
		}

		if startedAt.Valid {
			sprint.StartedAt = &startedAt.String
		}
		if closedAt.Valid {
			sprint.ClosedAt = &closedAt.String
		}

		sprints = append(sprints, sprint)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sprint rows: %w", err)
	}

	return sprints, nil
}

// UpdateSprint updates a sprint's description.
func (db *DB) UpdateSprint(ctx context.Context, id int, description string) error {
	return retryWithBackoff("update sprint", func() error {
		result, err := db.ExecContext(ctx,
			"UPDATE sprints SET description = ? WHERE id = ?",
			description, id,
		)
		if err != nil {
			return fmt.Errorf("updating sprint: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking rows affected: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("%w: sprint %d", utils.ErrNotFound, id)
		}

		return nil
	})
}

// UpdateSprintStatus updates sprint status and timestamps.
func (db *DB) UpdateSprintStatus(ctx context.Context, id int, status models.SprintStatus) error {
	return retryWithBackoff("update sprint status", func() error {
		var query string
		var args []interface{}

		switch status {
		case models.SprintOpen:
			// Starting sprint
			query = "UPDATE sprints SET status = ?, started_at = ? WHERE id = ?"
			args = []interface{}{status, utils.NowISO8601(), id}
		case models.SprintClosed:
			// Closing sprint
			query = "UPDATE sprints SET status = ?, closed_at = ? WHERE id = ?"
			args = []interface{}{status, utils.NowISO8601(), id}
		default:
			// Other status changes
			query = "UPDATE sprints SET status = ? WHERE id = ?"
			args = []interface{}{status, id}
		}

		result, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("updating sprint status: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking rows affected: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("%w: sprint %d", utils.ErrNotFound, id)
		}

		return nil
	})
}

// DeleteSprint deletes a sprint by ID.
func (db *DB) DeleteSprint(ctx context.Context, id int) error {
	return retryWithBackoff("delete sprint", func() error {
		// First reset task status for tasks in this sprint
		_, err := db.ExecContext(ctx,
			`UPDATE tasks SET status = 'BACKLOG' WHERE id IN (
				SELECT task_id FROM sprint_tasks WHERE sprint_id = ?
			)`,
			id,
		)
		if err != nil {
			return fmt.Errorf("resetting task statuses: %w", err)
		}

		// Delete sprint (cascade will remove sprint_tasks entries)
		result, err := db.ExecContext(ctx, "DELETE FROM sprints WHERE id = ?", id)
		if err != nil {
			return fmt.Errorf("deleting sprint: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking rows affected: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("%w: sprint %d", utils.ErrNotFound, id)
		}

		return nil
	})
}

// GetSprintTasks retrieves all tasks in a sprint.
func (db *DB) GetSprintTasks(ctx context.Context, sprintID int) ([]int, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT task_id FROM sprint_tasks WHERE sprint_id = ? ORDER BY task_id`,
		sprintID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying sprint tasks: %w", err)
	}
	defer rows.Close()

	var tasks []int
	for rows.Next() {
		var taskID int
		if err := rows.Scan(&taskID); err != nil {
			return nil, fmt.Errorf("scanning task id: %w", err)
		}
		tasks = append(tasks, taskID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task ids: %w", err)
	}

	return tasks, nil
}

// GetSprintTasksFull retrieves full task objects for a sprint.
func (db *DB) GetSprintTasksFull(ctx context.Context, sprintID int, status *models.TaskStatus) ([]models.Task, error) {
	query := `SELECT t.id, t.priority, t.severity, t.status, t.description, t.specialists,
		         t.action, t.expected_result, t.created_at, t.completed_at
		      FROM tasks t
		      INNER JOIN sprint_tasks st ON t.id = st.task_id
		      WHERE st.sprint_id = ?`
	args := []interface{}{sprintID}

	if status != nil {
		query += " AND t.status = ?"
		args = append(args, string(*status))
	}

	query += " ORDER BY t.priority DESC, t.severity DESC"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying sprint tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

// AddTasksToSprint adds tasks to a sprint.
func (db *DB) AddTasksToSprint(ctx context.Context, sprintID int, taskIDs []int) error {
	if len(taskIDs) == 0 {
		return nil
	}

	return retryWithBackoff("add tasks to sprint", func() error {
		now := utils.NowISO8601()

		for _, taskID := range taskIDs {
			_, err := db.ExecContext(ctx,
				`INSERT INTO sprint_tasks (sprint_id, task_id, added_at) VALUES (?, ?, ?)
				 ON CONFLICT(task_id) DO UPDATE SET sprint_id = excluded.sprint_id, added_at = excluded.added_at`,
				sprintID, taskID, now,
			)
			if err != nil {
				return fmt.Errorf("adding task %d to sprint: %w", taskID, err)
			}
		}

		// Update task status to SPRINT using cached placeholders
		placeholders := db.queryCache.GetPlaceholders(len(taskIDs))
		args := make([]interface{}, len(taskIDs))
		for i, id := range taskIDs {
			args[i] = id
		}

		query := fmt.Sprintf(
			"UPDATE tasks SET status = 'SPRINT' WHERE id IN (%s)",
			placeholders,
		)
		_, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("updating task statuses: %w", err)
		}

		return nil
	})
}

// RemoveTasksFromSprint removes tasks from a sprint.
func (db *DB) RemoveTasksFromSprint(ctx context.Context, taskIDs []int) error {
	if len(taskIDs) == 0 {
		return nil
	}

	return retryWithBackoff("remove tasks from sprint", func() error {
		// Use cached placeholders
		placeholders := db.queryCache.GetPlaceholders(len(taskIDs))
		args := make([]interface{}, len(taskIDs))
		for i, id := range taskIDs {
			args[i] = id
		}

		// Delete from sprint_tasks
		query := fmt.Sprintf("DELETE FROM sprint_tasks WHERE task_id IN (%s)", placeholders)
		_, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("removing tasks from sprint: %w", err)
		}

		// Update task status to BACKLOG
		query = fmt.Sprintf("UPDATE tasks SET status = 'BACKLOG' WHERE id IN (%s)", placeholders)
		_, err = db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("updating task statuses: %w", err)
		}

		return nil
	})
}

// ==================== AUDIT QUERIES ====================

// LogAuditEntry inserts a new audit entry.
func (db *DB) LogAuditEntry(ctx context.Context, entry *models.AuditEntry) (int, error) {
	var auditID int
	err := retryWithBackoff("log audit entry", func() error {
		result, err := db.ExecContext(ctx,
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			entry.Operation,
			entry.EntityType,
			entry.EntityID,
			entry.PerformedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting audit entry: %w", err)
		}

		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("getting last insert id: %w", err)
		}

		auditID = int(id)
		return nil
	})

	if err != nil {
		return 0, err
	}

	return auditID, nil
}

// GetAuditEntries retrieves audit entries with optional filters and pagination.
//
// Parameters (all optional, use nil to skip filter):
//   - ctx: Context for timeout and cancellation
//   - operation: Filter by operation type (e.g., "TASK_CREATE", "TASK_UPDATE")
//   - entityType: Filter by entity type (e.g., "TASK", "SPRINT")
//   - entityID: Filter by specific entity ID
//   - since: Filter entries from this timestamp (ISO 8601 format)
//   - until: Filter entries up to this timestamp (ISO 8601 format)
//   - limit: Maximum number of entries to return (0 = no limit)
//   - offset: Number of entries to skip for pagination (0 = start from beginning)
//
// Returns:
//   - Slice of AuditEntry structs, ordered by performed_at DESC (newest first)
//   - Error if database query fails
//
// Error conditions:
//   - Returns wrapped database errors for connection/query failures
//   - Returns empty slice (not error) if no entries match filters
//
// Side effects: None (read-only operation)
//
// Complexity: O(n) where n is the number of entries returned
//
// Example:
//
//	entries, err := db.GetAuditEntries(ctx,
//	    strPtr("TASK_CREATE"),  // operation filter
//	    strPtr("TASK"),         // entity type filter
//	    nil, nil, nil,          // no entity ID, since, until filters
//	    100, 0,                 // limit 100, offset 0
//	)
func (db *DB) GetAuditEntries(ctx context.Context, operation, entityType *string, entityID *int, since, until *string, limit, offset int) ([]models.AuditEntry, error) {
	query := `SELECT id, operation, entity_type, entity_id, performed_at FROM audit WHERE 1=1`
	args := []interface{}{}

	if operation != nil {
		query += " AND operation = ?"
		args = append(args, *operation)
	}
	if entityType != nil {
		query += " AND entity_type = ?"
		args = append(args, *entityType)
	}
	if entityID != nil {
		query += " AND entity_id = ?"
		args = append(args, *entityID)
	}
	if since != nil {
		query += " AND performed_at >= ?"
		args = append(args, *since)
	}
	if until != nil {
		query += " AND performed_at <= ?"
		args = append(args, *until)
	}

	query += " ORDER BY performed_at DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying audit entries: %w", err)
	}
	defer rows.Close()

	var entries []models.AuditEntry
	for rows.Next() {
		var entry models.AuditEntry
		err := rows.Scan(
			&entry.ID,
			&entry.Operation,
			&entry.EntityType,
			&entry.EntityID,
			&entry.PerformedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning audit entry: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating audit entries: %w", err)
	}

	return entries, nil
}

// GetEntityHistory retrieves audit history for a specific entity.
func (db *DB) GetEntityHistory(ctx context.Context, entityType string, entityID int) ([]models.AuditEntry, error) {
	return db.GetAuditEntries(ctx, nil, &entityType, &entityID, nil, nil, 0, 0)
}

// GetAuditStats retrieves aggregated statistics for audit entries in a date range.
//
// Parameters (all optional, use nil to skip):
//   - ctx: Context for timeout and cancellation
//   - since: Start of date range (ISO 8601 format, inclusive)
//   - until: End of date range (ISO 8601 format, inclusive)
//
// Returns:
//   - AuditStats struct containing:
//   - TotalEntries: Total count of audit entries in range
//   - ByOperation: Map of operation type to count (e.g., {"TASK_CREATE": 10, "TASK_UPDATE": 5})
//   - ByEntityType: Map of entity type to count (e.g., {"TASK": 15, "SPRINT": 3})
//
// Error conditions:
//   - Returns wrapped database errors for connection/query failures
//   - Returns empty stats (zeros) if no entries match the date range
//
// Side effects: None (read-only operation)
//
// Complexity: O(n) where n is the number of unique operations/entity types
//
// Example:
//
//	stats, err := db.GetAuditStats(ctx,
//	    strPtr("2024-01-01T00:00:00.000Z"),
//	    strPtr("2024-12-31T23:59:59.999Z"),
//	)
//	fmt.Printf("Total operations: %d\n", stats.TotalEntries)
//	for op, count := range stats.ByOperation {
//	    fmt.Printf("  %s: %d\n", op, count)
//	}
func (db *DB) GetAuditStats(ctx context.Context, since, until *string) (*models.AuditStats, error) {
	stats := &models.AuditStats{
		ByOperation:  make(map[string]int),
		ByEntityType: make(map[string]int),
	}

	// Total count
	countQuery := `SELECT COUNT(*) FROM audit WHERE 1=1`
	countArgs := []interface{}{}

	if since != nil {
		countQuery += " AND performed_at >= ?"
		countArgs = append(countArgs, *since)
	}
	if until != nil {
		countQuery += " AND performed_at <= ?"
		countArgs = append(countArgs, *until)
	}

	err := db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&stats.TotalEntries)
	if err != nil {
		return nil, fmt.Errorf("counting audit entries: %w", err)
	}

	// First and last entry dates
	dateQuery := `SELECT MIN(performed_at), MAX(performed_at) FROM audit WHERE 1=1`
	dateArgs := []interface{}{}

	if since != nil {
		dateQuery += " AND performed_at >= ?"
		dateArgs = append(dateArgs, *since)
	}
	if until != nil {
		dateQuery += " AND performed_at <= ?"
		dateArgs = append(dateArgs, *until)
	}

	var firstEntry, lastEntry sql.NullString
	err = db.QueryRowContext(ctx, dateQuery, dateArgs...).Scan(&firstEntry, &lastEntry)
	if err != nil {
		return nil, fmt.Errorf("getting date range: %w", err)
	}

	if firstEntry.Valid {
		stats.FirstEntryAt = firstEntry.String
	}
	if lastEntry.Valid {
		stats.LastEntryAt = lastEntry.String
	}

	// Count by operation
	opQuery := `SELECT operation, COUNT(*) FROM audit WHERE 1=1`
	opArgs := []interface{}{}

	if since != nil {
		opQuery += " AND performed_at >= ?"
		opArgs = append(opArgs, *since)
	}
	if until != nil {
		opQuery += " AND performed_at <= ?"
		opArgs = append(opArgs, *until)
	}
	opQuery += " GROUP BY operation"

	opRows, err := db.QueryContext(ctx, opQuery, opArgs...)
	if err != nil {
		return nil, fmt.Errorf("counting by operation: %w", err)
	}
	defer opRows.Close()

	for opRows.Next() {
		var op string
		var count int
		if err := opRows.Scan(&op, &count); err != nil {
			return nil, fmt.Errorf("scanning operation count: %w", err)
		}
		stats.ByOperation[op] = count
	}

	// Count by entity type
	entQuery := `SELECT entity_type, COUNT(*) FROM audit WHERE 1=1`
	entArgs := []interface{}{}

	if since != nil {
		entQuery += " AND performed_at >= ?"
		entArgs = append(entArgs, *since)
	}
	if until != nil {
		entQuery += " AND performed_at <= ?"
		entArgs = append(entArgs, *until)
	}
	entQuery += " GROUP BY entity_type"

	entRows, err := db.QueryContext(ctx, entQuery, entArgs...)
	if err != nil {
		return nil, fmt.Errorf("counting by entity type: %w", err)
	}
	defer entRows.Close()

	for entRows.Next() {
		var entType string
		var count int
		if err := entRows.Scan(&entType, &count); err != nil {
			return nil, fmt.Errorf("scanning entity type count: %w", err)
		}
		stats.ByEntityType[entType] = count
	}

	return stats, nil
}
