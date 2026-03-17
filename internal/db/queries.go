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
	"database/sql"
	"fmt"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== TASK QUERIES ====================

// CreateTask inserts a new task and returns its ID.
func (db *DB) CreateTask(task *models.Task) (int, error) {
	var taskID int
	err := retryWithBackoff("create task", func() error {
		result, err := db.Exec(
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
func (db *DB) GetTask(id int) (*models.Task, error) {
	var task models.Task
	var specialists sql.NullString
	var completedAt sql.NullString

	err := db.QueryRow(
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
			return nil, fmt.Errorf("task %d not found", id)
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
func (db *DB) GetTasks(ids []int) ([]models.Task, error) {
	if len(ids) == 0 {
		return []models.Task{}, nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT id, priority, severity, status, description, specialists, action, expected_result, created_at, completed_at
		 FROM tasks WHERE id IN (%s) ORDER BY id`,
		strings.Join(placeholders, ","),
	)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

// ListTasks retrieves tasks with optional filters.
func (db *DB) ListTasks(status *models.TaskStatus, minPriority, minSeverity *int, limit *int) ([]models.Task, error) {
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

	if limit != nil {
		query += " LIMIT ?"
		args = append(args, *limit)
	}

	rows, err := db.Query(query, args...)
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

// UpdateTask updates a task's fields.
// Only whitelisted fields can be updated. Use dedicated methods for status changes.
func (db *DB) UpdateTask(id int, updates map[string]interface{}) error {
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

		result, err := db.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("updating task: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking rows affected: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("task %d not found", id)
		}

		return nil
	})
}

// UpdateTaskStatus updates task status and optionally sets completed_at.
func (db *DB) UpdateTaskStatus(ids []int, status models.TaskStatus) error {
	if len(ids) == 0 {
		return nil
	}

	return retryWithBackoff("update task status", func() error {
		// Build placeholders
		placeholders := make([]string, len(ids))
		idArgs := make([]interface{}, len(ids))
		for i, id := range ids {
			placeholders[i] = "?"
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
			strings.Join(placeholders, ","),
		)

		_, err := db.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("updating task status: %w", err)
		}

		return nil
	})
}

// UpdateTaskPriority updates task priority.
func (db *DB) UpdateTaskPriority(ids []int, priority int) error {
	if len(ids) == 0 {
		return nil
	}

	return retryWithBackoff("update task priority", func() error {
		placeholders := make([]string, len(ids))
		args := make([]interface{}, len(ids)+1)
		args[0] = priority
		for i, id := range ids {
			placeholders[i] = "?"
			args[i+1] = id
		}

		query := fmt.Sprintf("UPDATE tasks SET priority = ? WHERE id IN (%s)", strings.Join(placeholders, ","))
		_, err := db.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("updating task priority: %w", err)
		}
		return nil
	})
}

// UpdateTaskSeverity updates task severity.
func (db *DB) UpdateTaskSeverity(ids []int, severity int) error {
	if len(ids) == 0 {
		return nil
	}

	return retryWithBackoff("update task severity", func() error {
		placeholders := make([]string, len(ids))
		args := make([]interface{}, len(ids)+1)
		args[0] = severity
		for i, id := range ids {
			placeholders[i] = "?"
			args[i+1] = id
		}

		query := fmt.Sprintf("UPDATE tasks SET severity = ? WHERE id IN (%s)", strings.Join(placeholders, ","))
		_, err := db.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("updating task severity: %w", err)
		}
		return nil
	})
}

// DeleteTask deletes a task by ID.
func (db *DB) DeleteTask(id int) error {
	return retryWithBackoff("delete task", func() error {
		result, err := db.Exec("DELETE FROM tasks WHERE id = ?", id)
		if err != nil {
			return fmt.Errorf("deleting task: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking rows affected: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("task %d not found", id)
		}

		return nil
	})
}

// scanTasks scans rows into a slice of Task.
func scanTasks(rows *sql.Rows) ([]models.Task, error) {
	var tasks []models.Task

	for rows.Next() {
		var task models.Task
		var specialists sql.NullString
		var completedAt sql.NullString

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
func (db *DB) CreateSprint(sprint *models.Sprint) (int, error) {
	var sprintID int
	err := retryWithBackoff("create sprint", func() error {
		result, err := db.Exec(
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
func (db *DB) GetSprint(id int) (*models.Sprint, error) {
	var sprint models.Sprint
	var startedAt sql.NullString
	var closedAt sql.NullString

	err := db.QueryRow(
		`SELECT id, status, description, created_at, started_at, closed_at FROM sprints WHERE id = ?`,
		id,
	).Scan(
		&sprint.ID,
		&sprint.Status,
		&sprint.Description,
		&sprint.CreatedAt,
		&startedAt,
		&closedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("sprint %d not found", id)
		}
		return nil, fmt.Errorf("querying sprint: %w", err)
	}

	if startedAt.Valid {
		sprint.StartedAt = &startedAt.String
	}
	if closedAt.Valid {
		sprint.ClosedAt = &closedAt.String
	}

	// Load tasks
	tasks, err := db.GetSprintTasks(id)
	if err != nil {
		return nil, err
	}
	sprint.Tasks = tasks
	sprint.TaskCount = len(tasks)

	return &sprint, nil
}

// ListSprints retrieves all sprints with optional status filter.
func (db *DB) ListSprints(status *models.SprintStatus) ([]models.Sprint, error) {
	query := `SELECT id, status, description, created_at, started_at, closed_at FROM sprints WHERE 1=1`
	args := []interface{}{}

	if status != nil {
		query += " AND status = ?"
		args = append(args, string(*status))
	}

	query += " ORDER BY created_at DESC"

	rows, err := db.Query(query, args...)
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
func (db *DB) UpdateSprint(id int, description string) error {
	return retryWithBackoff("update sprint", func() error {
		result, err := db.Exec(
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
			return fmt.Errorf("sprint %d not found", id)
		}

		return nil
	})
}

// UpdateSprintStatus updates sprint status and timestamps.
func (db *DB) UpdateSprintStatus(id int, status models.SprintStatus) error {
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

		result, err := db.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("updating sprint status: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking rows affected: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("sprint %d not found", id)
		}

		return nil
	})
}

// DeleteSprint deletes a sprint by ID.
func (db *DB) DeleteSprint(id int) error {
	return retryWithBackoff("delete sprint", func() error {
		// First reset task status for tasks in this sprint
		_, err := db.Exec(
			`UPDATE tasks SET status = 'BACKLOG' WHERE id IN (
				SELECT task_id FROM sprint_tasks WHERE sprint_id = ?
			)`,
			id,
		)
		if err != nil {
			return fmt.Errorf("resetting task statuses: %w", err)
		}

		// Delete sprint (cascade will remove sprint_tasks entries)
		result, err := db.Exec("DELETE FROM sprints WHERE id = ?", id)
		if err != nil {
			return fmt.Errorf("deleting sprint: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking rows affected: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("sprint %d not found", id)
		}

		return nil
	})
}

// GetSprintTasks retrieves all tasks in a sprint.
func (db *DB) GetSprintTasks(sprintID int) ([]int, error) {
	rows, err := db.Query(
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
func (db *DB) GetSprintTasksFull(sprintID int, status *models.TaskStatus) ([]models.Task, error) {
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

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying sprint tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

// AddTasksToSprint adds tasks to a sprint.
func (db *DB) AddTasksToSprint(sprintID int, taskIDs []int) error {
	if len(taskIDs) == 0 {
		return nil
	}

	return retryWithBackoff("add tasks to sprint", func() error {
		now := utils.NowISO8601()

		for _, taskID := range taskIDs {
			_, err := db.Exec(
				`INSERT INTO sprint_tasks (sprint_id, task_id, added_at) VALUES (?, ?, ?)
				 ON CONFLICT(task_id) DO UPDATE SET sprint_id = excluded.sprint_id, added_at = excluded.added_at`,
				sprintID, taskID, now,
			)
			if err != nil {
				return fmt.Errorf("adding task %d to sprint: %w", taskID, err)
			}
		}

		// Update task status to SPRINT
		placeholders := make([]string, len(taskIDs))
		args := make([]interface{}, len(taskIDs))
		for i, id := range taskIDs {
			placeholders[i] = "?"
			args[i] = id
		}

		query := fmt.Sprintf(
			"UPDATE tasks SET status = 'SPRINT' WHERE id IN (%s)",
			strings.Join(placeholders, ","),
		)
		_, err := db.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("updating task statuses: %w", err)
		}

		return nil
	})
}

// RemoveTasksFromSprint removes tasks from a sprint.
func (db *DB) RemoveTasksFromSprint(taskIDs []int) error {
	if len(taskIDs) == 0 {
		return nil
	}

	return retryWithBackoff("remove tasks from sprint", func() error {
		placeholders := make([]string, len(taskIDs))
		args := make([]interface{}, len(taskIDs))
		for i, id := range taskIDs {
			placeholders[i] = "?"
			args[i] = id
		}

		// Delete from sprint_tasks
		query := fmt.Sprintf("DELETE FROM sprint_tasks WHERE task_id IN (%s)", strings.Join(placeholders, ","))
		_, err := db.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("removing tasks from sprint: %w", err)
		}

		// Update task status to BACKLOG
		query = fmt.Sprintf("UPDATE tasks SET status = 'BACKLOG' WHERE id IN (%s)", strings.Join(placeholders, ","))
		_, err = db.Exec(query, args...)
		if err != nil {
			return fmt.Errorf("updating task statuses: %w", err)
		}

		return nil
	})
}

// ==================== AUDIT QUERIES ====================

// LogAuditEntry inserts a new audit entry.
func (db *DB) LogAuditEntry(entry *models.AuditEntry) (int, error) {
	var auditID int
	err := retryWithBackoff("log audit entry", func() error {
		result, err := db.Exec(
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

// GetAuditEntries retrieves audit entries with filters.
func (db *DB) GetAuditEntries(operation, entityType *string, entityID *int, since, until *string, limit, offset int) ([]models.AuditEntry, error) {
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

	rows, err := db.Query(query, args...)
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
func (db *DB) GetEntityHistory(entityType string, entityID int) ([]models.AuditEntry, error) {
	return db.GetAuditEntries(nil, &entityType, &entityID, nil, nil, 0, 0)
}

// GetAuditStats retrieves statistics for audit entries in a date range.
func (db *DB) GetAuditStats(since, until *string) (*models.AuditStats, error) {
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

	err := db.QueryRow(countQuery, countArgs...).Scan(&stats.TotalEntries)
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
	err = db.QueryRow(dateQuery, dateArgs...).Scan(&firstEntry, &lastEntry)
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

	opRows, err := db.Query(opQuery, opArgs...)
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

	entRows, err := db.Query(entQuery, entArgs...)
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
