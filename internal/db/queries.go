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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// Package-level sentinel errors for static error conditions.
var (
	// ErrTasksNotInSprint indicates that one or more task IDs do not belong to the given sprint.
	ErrTasksNotInSprint = errors.New("some task IDs do not belong to sprint")
	// ErrCannotSwapSelf indicates an attempt to swap a task with itself.
	ErrCannotSwapSelf = errors.New("cannot swap a task with itself")
	// ErrSwapTasksNotFound indicates that one or both tasks were not found in the sprint.
	ErrSwapTasksNotFound = errors.New("one or both tasks not found in sprint")
)

// SQL fragments built from models constants so that renaming an enum value
// (e.g. StatusSprint -> StatusInSprint) won't silently leave a stale string
// literal in a query. Computed at package init.
var (
	// sqlActiveTaskStatuses lists the three non-terminal statuses a task can
	// hold while it sits inside a sprint: SPRINT, DOING, TESTING.
	sqlActiveTaskStatuses = "('" + string(models.StatusSprint) + "', '" +
		string(models.StatusDoing) + "', '" + string(models.StatusTesting) + "')"
	// sqlStatusCompleted and sqlSprintClosed are inlined into stats queries
	// that group by status; using parameters there would require restructuring
	// already-complex multi-clause aggregations.
	sqlStatusCompleted = "'" + string(models.StatusCompleted) + "'"
	sqlSprintClosed    = "'" + string(models.SprintClosed) + "'"
)

// ==================== TASK QUERIES ====================

// CreateTask inserts a new task and returns its ID.
func (db *DB) CreateTask(ctx context.Context, task *models.Task) (int, error) {
	var taskID int
	err := retryWithBackoff("create task", func() error {
		// Default type to TASK if not specified
		taskType := task.Type
		if taskType == "" {
			taskType = models.TypeTask
		}

		result, err := db.ExecContext(ctx,
			`INSERT INTO tasks (title, status, type, functional_requirements, technical_requirements, acceptance_criteria, created_at, specialists, priority, severity, completion_summary, parent_task_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)`,
			task.Title,
			task.Status,
			taskType,
			task.FunctionalRequirements,
			task.TechnicalRequirements,
			task.AcceptanceCriteria,
			task.CreatedAt,
			task.Specialists,
			task.Priority,
			task.Severity,
			task.ParentTaskID,
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

// GetTask retrieves a task by ID, including dependencies and subtask_count.
// Uses scanTasksWithDeps to fold depends_on / blocks into the same query.
func (db *DB) GetTask(ctx context.Context, id int) (*models.Task, error) {
	query := `SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
	        t.created_at, t.specialists, t.started_at, t.tested_at, t.closed_at, t.completion_summary, t.parent_task_id,
	        t.priority, t.severity,
	        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count` + taskDepsSelect + `
	 FROM tasks t WHERE t.id = ?`

	rows, err := db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("querying task: %w", err)
	}
	defer rows.Close()

	tasks, err := scanTasksWithDeps(rows)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("%w: task %d", utils.ErrNotFound, id)
	}
	return &tasks[0], nil
}

// GetTasks retrieves multiple tasks by IDs.
func (db *DB) GetTasks(ctx context.Context, ids []int) ([]models.Task, error) {
	if len(ids) == 0 {
		return []models.Task{}, nil
	}

	// Use cached placeholders for better performance
	placeholders := db.queryCache.GetPlaceholders(len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	query := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated
		`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
		        t.created_at, t.specialists, t.started_at, t.tested_at, t.closed_at, t.completion_summary, t.parent_task_id,
		        t.priority, t.severity,
		        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count`+taskDepsSelect+`
		 FROM tasks t WHERE t.id IN (%s) ORDER BY t.id`,
		placeholders,
	)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying tasks: %w", err)
	}
	defer rows.Close()

	return scanTasksWithDeps(rows)
}

// TaskListFilter holds all optional filter and sort parameters for ListTasks.
type TaskListFilter struct {
	Status       *models.TaskStatus
	MinPriority  *int
	MinSeverity  *int
	TaskType     *models.TaskType
	Specialists  *string    // case-insensitive partial match against the specialists field
	CreatedSince *time.Time // inclusive lower bound on created_at
	CreatedUntil *time.Time // inclusive upper bound on created_at
	Sort         string     // "priority" (default), "created", "status", "severity"
	Limit        int
}

// ListTasks retrieves tasks with optional filters.
// Filters: status, minPriority, minSeverity, taskType, specialists, createdSince, createdUntil, sort, limit.
func (db *DB) ListTasks(ctx context.Context, filter *TaskListFilter) ([]models.Task, error) {
	if filter.Limit < 1 {
		filter.Limit = models.DefaultTaskLimit
	}
	if filter.Limit > models.MaxTaskLimit {
		filter.Limit = models.MaxTaskLimit
	}

	query := `SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
		        t.created_at, t.specialists, t.started_at, t.tested_at, t.closed_at, t.completion_summary, t.parent_task_id,
		        t.priority, t.severity,
		        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count` + taskDepsSelect + `
		      FROM tasks t WHERE 1=1`
	args := make([]any, 0, 8)

	if filter.Status != nil {
		query += " AND t.status = ?"
		args = append(args, string(*filter.Status))
	}
	if filter.MinPriority != nil {
		query += " AND t.priority >= ?"
		args = append(args, *filter.MinPriority)
	}
	if filter.MinSeverity != nil {
		query += " AND t.severity >= ?"
		args = append(args, *filter.MinSeverity)
	}
	if filter.TaskType != nil {
		query += " AND t.type = ?"
		args = append(args, string(*filter.TaskType))
	}
	if filter.Specialists != nil {
		// Escape SQL LIKE wildcards in the user-supplied value to prevent accidental pattern expansion.
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(*filter.Specialists)
		query += ` AND LOWER(COALESCE(t.specialists, '')) LIKE LOWER(?) ESCAPE '\'`
		args = append(args, "%"+escaped+"%")
	}
	if filter.CreatedSince != nil {
		query += " AND t.created_at >= ?"
		args = append(args, filter.CreatedSince.UTC().Format(time.RFC3339))
	}
	if filter.CreatedUntil != nil {
		query += " AND t.created_at <= ?"
		args = append(args, filter.CreatedUntil.UTC().Format(time.RFC3339))
	}

	switch filter.Sort {
	case "created":
		query += " ORDER BY t.created_at ASC"
	case "status":
		query += " ORDER BY t.status ASC, t.priority DESC, t.created_at ASC"
	case "severity":
		query += " ORDER BY t.severity DESC, t.priority DESC, t.created_at ASC"
	default: // "priority" or empty — matches existing default behaviour
		query += " ORDER BY t.priority DESC, t.created_at ASC"
	}
	query += " LIMIT ?"
	args = append(args, filter.Limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer rows.Close()

	return scanTasksWithDeps(rows)
}

// UpdateTask updates a task's fields with the provided values.
// Uses hardcoded field names to prevent SQL injection - no dynamic field names in SQL.
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - id: The unique identifier of the task to update
//   - updates: A map of field names to new values. Only whitelisted fields can be updated.
//
// Allowed fields (whitelisted):
//   - "title": Task title (string, max 255 chars)
//   - "functional_requirements": Functional requirements (string, max 4096 chars)
//   - "technical_requirements": Technical requirements (string, max 4096 chars)
//   - "acceptance_criteria": Acceptance criteria (string, max 4096 chars)
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
// Security: Uses hardcoded field names in SQL to prevent injection attacks.
func (db *DB) UpdateTask(ctx context.Context, id int, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	return retryWithBackoff("update task", func() error {
		setParts := make([]string, 0, len(updates))
		args := make([]any, 0, len(updates)+1)

		// Iterate updates in a deterministic order so the generated SQL is
		// stable across runs — required for SQLite's prepared-statement
		// cache and for reproducible behaviour in tests.
		fields := make([]string, 0, len(updates))
		for f := range updates {
			fields = append(fields, f)
		}
		sort.Strings(fields)

		// Use hardcoded field names to prevent SQL injection
		// Field names are never dynamically inserted into SQL
		for _, field := range fields {
			value := updates[field]
			switch field {
			case "title":
				setParts = append(setParts, "title = ?")
				args = append(args, value)
			case "functional_requirements":
				setParts = append(setParts, "functional_requirements = ?")
				args = append(args, value)
			case "technical_requirements":
				setParts = append(setParts, "technical_requirements = ?")
				args = append(args, value)
			case "acceptance_criteria":
				setParts = append(setParts, "acceptance_criteria = ?")
				args = append(args, value)
			case "specialists":
				setParts = append(setParts, "specialists = ?")
				args = append(args, value)
			case "priority":
				setParts = append(setParts, "priority = ?")
				args = append(args, value)
			case "severity":
				setParts = append(setParts, "severity = ?")
				args = append(args, value)
			default:
				return fmt.Errorf("%w: field %q cannot be updated via UpdateTask (use dedicated method)", utils.ErrInvalidUpdate, field)
			}
		}

		if len(setParts) == 0 {
			return fmt.Errorf("%w: no valid fields to update", utils.ErrInvalidUpdate)
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
		// Fields are always in the same order: title, functional_requirements, technical_requirements, acceptance_criteria, specialists, priority, severity
		var setParts []string
		var args []any

		if update.Title != nil {
			setParts = append(setParts, "title = ?")
			args = append(args, *update.Title)
		}
		if update.FunctionalRequirements != nil {
			setParts = append(setParts, "functional_requirements = ?")
			args = append(args, *update.FunctionalRequirements)
		}
		if update.TechnicalRequirements != nil {
			setParts = append(setParts, "technical_requirements = ?")
			args = append(args, *update.TechnicalRequirements)
		}
		if update.AcceptanceCriteria != nil {
			setParts = append(setParts, "acceptance_criteria = ?")
			args = append(args, *update.AcceptanceCriteria)
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

// UpdateTaskStatus updates task status and manages lifecycle timestamps.
// Per SPEC/STATE_MACHINE.md:
// - SPRINT → DOING: set started_at
// - DOING → TESTING: set tested_at
// - TESTING → COMPLETED: set closed_at
// - COMPLETED → BACKLOG: clear started_at, tested_at, closed_at
func (db *DB) UpdateTaskStatus(ctx context.Context, ids []int, status models.TaskStatus) error {
	if len(ids) == 0 {
		return nil
	}

	return retryWithBackoff("update task status", func() error {
		// Use cached placeholders for better performance
		placeholders := db.queryCache.GetPlaceholders(len(ids))
		idArgs := make([]any, len(ids))
		for i, id := range ids {
			idArgs[i] = id
		}

		now := utils.NowISO8601()

		// Build query based on target status for lifecycle date tracking
		var query string
		var args []any

		switch status {
		case models.StatusDoing:
			// Transition to DOING: set started_at
			query = fmt.Sprintf(
				"UPDATE tasks SET status = ?, started_at = ? WHERE id IN (%s)",
				placeholders,
			)
			args = append([]any{status, now}, idArgs...)

		case models.StatusTesting:
			// Transition to TESTING: set tested_at
			query = fmt.Sprintf(
				"UPDATE tasks SET status = ?, tested_at = ? WHERE id IN (%s)",
				placeholders,
			)
			args = append([]any{status, now}, idArgs...)

		case models.StatusCompleted:
			// Transition to COMPLETED: set closed_at
			query = fmt.Sprintf(
				"UPDATE tasks SET status = ?, closed_at = ? WHERE id IN (%s)",
				placeholders,
			)
			args = append([]any{status, now}, idArgs...)

		case models.StatusBacklog:
			// Reopening to BACKLOG: clear all tracking dates
			query = fmt.Sprintf(
				"UPDATE tasks SET status = ?, started_at = NULL, tested_at = NULL, closed_at = NULL WHERE id IN (%s)",
				placeholders,
			)
			args = append([]any{status}, idArgs...)

		default:
			// Other status changes: just update status
			query = fmt.Sprintf(
				"UPDATE tasks SET status = ? WHERE id IN (%s)",
				placeholders,
			)
			args = append([]any{status}, idArgs...)
		}

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
		args := make([]any, len(ids)+1)
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
		args := make([]any, len(ids)+1)
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

// GetSubTasks retrieves all direct subtasks of the given parent task ID.
// Tasks are ordered by priority descending, then created_at ascending.
func (db *DB) GetSubTasks(ctx context.Context, parentID int) ([]models.Task, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
		        t.created_at, t.specialists, t.started_at, t.tested_at, t.closed_at, t.completion_summary, t.parent_task_id,
		        t.priority, t.severity,
		        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count
		 FROM tasks t WHERE t.parent_task_id = ?
		 ORDER BY t.priority DESC, t.created_at ASC`,
		parentID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying subtasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

// GetParentTask retrieves the parent task of a given task ID.
// Returns nil (no error) if the task has no parent.
func (db *DB) GetParentTask(ctx context.Context, taskID int) (*models.Task, error) {
	// First get the parent_task_id from the task
	var parentID sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT parent_task_id FROM tasks WHERE id = ?`,
		taskID,
	).Scan(&parentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: task %d", utils.ErrNotFound, taskID)
		}
		return nil, fmt.Errorf("querying parent task id: %w", err)
	}

	if !parentID.Valid {
		return nil, nil // no parent
	}

	return db.GetTask(ctx, int(parentID.Int64))
}

// GetIncompleteSubTasks returns the IDs of all direct subtasks of a given task
// that are NOT in COMPLETED status. Used to enforce the parent completion guard.
func (db *DB) GetIncompleteSubTasks(ctx context.Context, parentID int) ([]int, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM tasks WHERE parent_task_id = ? AND status != ?`,
		parentID, models.StatusCompleted,
	)
	if err != nil {
		return nil, fmt.Errorf("querying incomplete subtasks: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning subtask id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating subtask rows: %w", err)
	}
	return ids, nil
}

// HasSubTasks returns true if the given task has any direct subtasks.
func (db *DB) HasSubTasks(ctx context.Context, taskID int) (bool, int, error) {
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE parent_task_id = ?`,
		taskID,
	).Scan(&count)
	if err != nil {
		return false, 0, fmt.Errorf("counting subtasks: %w", err)
	}
	return count > 0, count, nil
}

// CountSubTasksByParents returns a map from parent_task_id to its subtask
// count, restricted to the given parent IDs. Parents with no subtasks are
// absent from the result. One round-trip regardless of the number of parents.
func (db *DB) CountSubTasksByParents(ctx context.Context, parentIDs []int) (map[int]int, error) {
	if len(parentIDs) == 0 {
		return map[int]int{}, nil
	}
	placeholders := db.queryCache.GetPlaceholders(len(parentIDs))
	args := make([]any, len(parentIDs))
	for i, id := range parentIDs {
		args[i] = id
	}
	query := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated
		`SELECT parent_task_id, COUNT(*) FROM tasks
		 WHERE parent_task_id IN (%s)
		 GROUP BY parent_task_id`,
		placeholders,
	)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("counting subtasks: %w", err)
	}
	defer rows.Close()
	counts := make(map[int]int, len(parentIDs))
	for rows.Next() {
		var pid, c int
		if err := rows.Scan(&pid, &c); err != nil {
			return nil, fmt.Errorf("scanning subtask count: %w", err)
		}
		counts[pid] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating subtask count rows: %w", err)
	}
	return counts, nil
}

// GetIncompleteSubTasksByParents returns a map from parent_task_id to the
// list of its subtask IDs that are NOT in COMPLETED status. Parents with
// no incomplete subtasks are absent from the result. One round-trip
// regardless of the number of parents.
func (db *DB) GetIncompleteSubTasksByParents(ctx context.Context, parentIDs []int) (map[int][]int, error) {
	if len(parentIDs) == 0 {
		return map[int][]int{}, nil
	}
	placeholders := db.queryCache.GetPlaceholders(len(parentIDs))
	args := make([]any, 0, len(parentIDs)+1)
	for _, id := range parentIDs {
		args = append(args, id)
	}
	args = append(args, models.StatusCompleted)
	query := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated
		`SELECT parent_task_id, id FROM tasks
		 WHERE parent_task_id IN (%s) AND status != ?
		 ORDER BY parent_task_id, id`,
		placeholders,
	)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying incomplete subtasks: %w", err)
	}
	defer rows.Close()
	result := make(map[int][]int, len(parentIDs))
	for rows.Next() {
		var pid, id int
		if err := rows.Scan(&pid, &id); err != nil {
			return nil, fmt.Errorf("scanning incomplete subtask: %w", err)
		}
		result[pid] = append(result[pid], id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating incomplete subtask rows: %w", err)
	}
	return result, nil
}

// taskDepsSelect is the SQL fragment that appends two comma-separated
// columns of dependency IDs (depends_on then blocks) to a tasks query.
// Use together with scanTasksWithDeps. The subqueries are ORDER-BY'd so
// the resulting CSV is stable for callers that depend on order.
const taskDepsSelect = `,
		(SELECT COALESCE(group_concat(d), '') FROM (
			SELECT depends_on_task_id AS d FROM task_dependencies
			WHERE task_id = t.id ORDER BY depends_on_task_id
		)) AS depends_on_csv,
		(SELECT COALESCE(group_concat(b), '') FROM (
			SELECT task_id AS b FROM task_dependencies
			WHERE depends_on_task_id = t.id ORDER BY task_id
		)) AS blocks_csv`

// parseCSVInts parses an unquoted comma-separated list of integers as
// produced by SQLite's group_concat. Returns an empty slice for "".
func parseCSVInts(s string) []int {
	if s == "" {
		return []int{}
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			continue // group_concat output is trusted; skip if somehow malformed
		}
		out = append(out, n)
	}
	return out
}

// scanTasksWithDeps is like scanTasks but expects two extra trailing
// columns (depends_on_csv, blocks_csv) produced by taskDepsSelect, and
// populates Task.DependsOn / Task.Blocks from them. This collapses what
// used to be 2N follow-up queries into the original SELECT.
func scanTasksWithDeps(rows *sql.Rows) ([]models.Task, error) {
	tasks := make([]models.Task, 0, 100)

	var specialists, startedAt, testedAt, closedAt, completionSummary sql.NullString
	var parentTaskID sql.NullInt64
	var dependsOnCSV, blocksCSV string

	for rows.Next() {
		var task models.Task
		specialists = sql.NullString{}
		startedAt = sql.NullString{}
		testedAt = sql.NullString{}
		closedAt = sql.NullString{}
		completionSummary = sql.NullString{}
		parentTaskID = sql.NullInt64{}
		dependsOnCSV = ""
		blocksCSV = ""

		err := rows.Scan(
			&task.ID,
			&task.Title,
			&task.Status,
			&task.Type,
			&task.FunctionalRequirements,
			&task.TechnicalRequirements,
			&task.AcceptanceCriteria,
			&task.CreatedAt,
			&specialists,
			&startedAt,
			&testedAt,
			&closedAt,
			&completionSummary,
			&parentTaskID,
			&task.Priority,
			&task.Severity,
			&task.SubtaskCount,
			&dependsOnCSV,
			&blocksCSV,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning task row: %w", err)
		}

		if specialists.Valid {
			task.Specialists = &specialists.String
		}
		if startedAt.Valid {
			task.StartedAt = &startedAt.String
		}
		if testedAt.Valid {
			task.TestedAt = &testedAt.String
		}
		if closedAt.Valid {
			task.ClosedAt = &closedAt.String
		}
		if completionSummary.Valid {
			task.CompletionSummary = &completionSummary.String
		}
		if parentTaskID.Valid {
			v := int(parentTaskID.Int64)
			task.ParentTaskID = &v
		}
		task.DependsOn = parseCSVInts(dependsOnCSV)
		task.Blocks = parseCSVInts(blocksCSV)

		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task rows: %w", err)
	}
	return tasks, nil
}

// scanTasks scans rows into a slice of Task.
// Optimized for memory efficiency with pre-allocated slice and reusable scan variables.
// Expected column order: id, title, status, type, functional_requirements, technical_requirements,
// acceptance_criteria, created_at, specialists, started_at, tested_at, closed_at,
// completion_summary, parent_task_id, priority, severity, subtask_count.
func scanTasks(rows *sql.Rows) ([]models.Task, error) {
	// Pre-allocate with typical batch size to avoid repeated reallocations
	tasks := make([]models.Task, 0, 100)

	// Reusable scan variables to avoid allocations per iteration
	var specialists sql.NullString
	var startedAt sql.NullString
	var testedAt sql.NullString
	var closedAt sql.NullString
	var completionSummary sql.NullString
	var parentTaskID sql.NullInt64

	for rows.Next() {
		var task models.Task

		// Reset scan variables for each row
		specialists = sql.NullString{}
		startedAt = sql.NullString{}
		testedAt = sql.NullString{}
		closedAt = sql.NullString{}
		completionSummary = sql.NullString{}
		parentTaskID = sql.NullInt64{}

		err := rows.Scan(
			&task.ID,
			&task.Title,
			&task.Status,
			&task.Type,
			&task.FunctionalRequirements,
			&task.TechnicalRequirements,
			&task.AcceptanceCriteria,
			&task.CreatedAt,
			&specialists,
			&startedAt,
			&testedAt,
			&closedAt,
			&completionSummary,
			&parentTaskID,
			&task.Priority,
			&task.Severity,
			&task.SubtaskCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning task row: %w", err)
		}

		if specialists.Valid {
			task.Specialists = &specialists.String
		}
		if startedAt.Valid {
			task.StartedAt = &startedAt.String
		}
		if testedAt.Valid {
			task.TestedAt = &testedAt.String
		}
		if closedAt.Valid {
			task.ClosedAt = &closedAt.String
		}
		if completionSummary.Valid {
			task.CompletionSummary = &completionSummary.String
		}
		if parentTaskID.Valid {
			v := int(parentTaskID.Int64)
			task.ParentTaskID = &v
		}

		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task rows: %w", err)
	}

	return tasks, nil
}

// GetOpenSprint retrieves the currently open sprint (status = OPEN).
// Returns ErrNotFound if no sprint is currently open.
func (db *DB) GetOpenSprint(ctx context.Context) (*models.Sprint, error) {
	var sprint models.Sprint
	var startedAt sql.NullString
	var closedAt sql.NullString
	var tasksJSON sql.NullString
	var maxTasks sql.NullInt64

	// Single query using JSON aggregation to get sprint data and task IDs
	err := db.QueryRowContext(ctx,
		`SELECT
			s.id, s.status, s.description, s.created_at, s.started_at, s.closed_at, s.max_tasks,
			COALESCE(json_group_array(DISTINCT st.task_id), '[]') as tasks
		 FROM sprints s
		 LEFT JOIN sprint_tasks st ON s.id = st.sprint_id
		 WHERE s.status = ?
		 GROUP BY s.id
		 LIMIT 1`,
		models.SprintOpen,
	).Scan(
		&sprint.ID,
		&sprint.Status,
		&sprint.Description,
		&sprint.CreatedAt,
		&startedAt,
		&closedAt,
		&maxTasks,
		&tasksJSON,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: no sprint is currently open. Use 'rmp sprint start <id>' to open a sprint first", utils.ErrNotFound)
		}
		return nil, fmt.Errorf("querying open sprint: %w", err)
	}

	if startedAt.Valid {
		sprint.StartedAt = &startedAt.String
	}
	if closedAt.Valid {
		sprint.ClosedAt = &closedAt.String
	}
	if maxTasks.Valid {
		v := int(maxTasks.Int64)
		sprint.MaxTasks = &v
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

// GetNextTasks retrieves the next N open tasks from the currently open sprint.
// Tasks are ordered by sprint task position (task_order) with priority as a tiebreaker.
// Only returns tasks with status SPRINT, DOING, or TESTING.
func (db *DB) GetNextTasks(ctx context.Context, limit int) ([]models.Task, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > models.MaxTaskLimit {
		limit = models.MaxTaskLimit
	}

	// First, get the open sprint ID
	var sprintID int
	err := db.QueryRowContext(ctx,
		"SELECT id FROM sprints WHERE status = ? LIMIT 1",
		models.SprintOpen,
	).Scan(&sprintID)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: no sprint is currently open. Use 'rmp sprint start <id>' to open a sprint first", utils.ErrNotFound)
		}
		return nil, fmt.Errorf("querying open sprint: %w", err)
	}

	// Get open tasks from the sprint, ordered by sprint task position
	query := `SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements,
		         t.acceptance_criteria, t.created_at, t.specialists, t.started_at, t.tested_at,
		         t.closed_at, t.completion_summary, t.parent_task_id,
		         t.priority, t.severity,
		         (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count
		      FROM tasks t
		      INNER JOIN sprint_tasks st ON t.id = st.task_id
		      WHERE st.sprint_id = ?
		        AND t.status IN ` + sqlActiveTaskStatuses + `
		      ORDER BY st.position ASC, t.priority DESC
		      LIMIT ?`

	rows, err := db.QueryContext(ctx, query, sprintID, limit)
	if err != nil {
		return nil, fmt.Errorf("querying next tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

// ==================== TASK DEPENDENCY QUERIES ====================

// AddTaskDependency adds a dependency: taskID depends on depID.
// Returns an error if the relationship already exists or would create a cycle.
func (db *DB) AddTaskDependency(ctx context.Context, taskID, depID int) error {
	// Self-dependency is forbidden.
	if taskID == depID {
		return fmt.Errorf("%w: task cannot depend on itself", utils.ErrInvalidInput)
	}

	// Circular dependency check: if depID already (transitively) depends on taskID,
	// adding taskID→depID would create a cycle.
	wouldCycle, err := db.hasTransitiveDependency(ctx, depID, taskID)
	if err != nil {
		return fmt.Errorf("checking circular dependency: %w", err)
	}
	if wouldCycle {
		return fmt.Errorf("%w: adding dependency would create a circular dependency between task #%d and task #%d",
			utils.ErrInvalidInput, taskID, depID)
	}

	_, err = db.ExecContext(ctx,
		`INSERT OR IGNORE INTO task_dependencies (task_id, depends_on_task_id) VALUES (?, ?)`,
		taskID, depID,
	)
	if err != nil {
		return fmt.Errorf("inserting task dependency: %w", err)
	}
	return nil
}

// AddTaskDependencyWithAudit adds a dependency and writes audit entries in a single transaction.
func (db *DB) AddTaskDependencyWithAudit(ctx context.Context, taskID, depID int) error {
	// Self-dependency check and circular check are performed in AddTaskDependency.
	// Run them before opening the transaction to fail fast.
	if taskID == depID {
		return fmt.Errorf("%w: task cannot depend on itself", utils.ErrInvalidInput)
	}
	wouldCycle, err := db.hasTransitiveDependency(ctx, depID, taskID)
	if err != nil {
		return fmt.Errorf("checking circular dependency: %w", err)
	}
	if wouldCycle {
		return fmt.Errorf("%w: adding dependency would create a circular dependency between task #%d and task #%d",
			utils.ErrInvalidInput, taskID, depID)
	}

	now := utils.NowISO8601()

	return db.WithTransaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO task_dependencies (task_id, depends_on_task_id) VALUES (?, ?)`,
			taskID, depID,
		); err != nil {
			return fmt.Errorf("inserting task dependency: %w", err)
		}

		for _, auditTaskID := range []int{taskID, depID} {
			if err := LogAuditTx(tx, models.OpTaskAddDep, models.EntityTask, auditTaskID, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveTaskDependencyWithAudit removes a dependency and writes audit entries in a single transaction.
func (db *DB) RemoveTaskDependencyWithAudit(ctx context.Context, taskID, depID int) error {
	now := utils.NowISO8601()

	return db.WithTransaction(func(tx *sql.Tx) error {
		result, err := tx.Exec(
			`DELETE FROM task_dependencies WHERE task_id = ? AND depends_on_task_id = ?`,
			taskID, depID,
		)
		if err != nil {
			return fmt.Errorf("deleting task dependency: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking rows affected: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("%w: dependency from task #%d to task #%d not found", utils.ErrNotFound, taskID, depID)
		}

		for _, auditTaskID := range []int{taskID, depID} {
			if err := LogAuditTx(tx, models.OpTaskRemoveDep, models.EntityTask, auditTaskID, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveTaskDependency removes a dependency: taskID no longer depends on depID.
func (db *DB) RemoveTaskDependency(ctx context.Context, taskID, depID int) error {
	result, err := db.ExecContext(ctx,
		`DELETE FROM task_dependencies WHERE task_id = ? AND depends_on_task_id = ?`,
		taskID, depID,
	)
	if err != nil {
		return fmt.Errorf("deleting task dependency: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: dependency from task #%d to task #%d not found", utils.ErrNotFound, taskID, depID)
	}
	return nil
}

// GetBlockers returns tasks that are blocking taskID (tasks that taskID depends on and are not COMPLETED).
func (db *DB) GetBlockers(ctx context.Context, taskID int) ([]models.Task, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
		        t.created_at, t.specialists, t.started_at, t.tested_at, t.closed_at, t.completion_summary, t.parent_task_id,
		        t.priority, t.severity,
		        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count
		 FROM tasks t
		 INNER JOIN task_dependencies td ON t.id = td.depends_on_task_id
		 WHERE td.task_id = ? AND t.status != ?
		 ORDER BY t.priority DESC, t.created_at ASC`,
		taskID, models.StatusCompleted,
	)
	if err != nil {
		return nil, fmt.Errorf("querying blockers: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

// GetBlocking returns tasks that depend on taskID (tasks this task is blocking).
func (db *DB) GetBlocking(ctx context.Context, taskID int) ([]models.Task, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
		        t.created_at, t.specialists, t.started_at, t.tested_at, t.closed_at, t.completion_summary, t.parent_task_id,
		        t.priority, t.severity,
		        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count
		 FROM tasks t
		 INNER JOIN task_dependencies td ON t.id = td.task_id
		 WHERE td.depends_on_task_id = ?
		 ORDER BY t.priority DESC, t.created_at ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying blocking tasks: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

// GetIncompleteDependencies returns the IDs of tasks that taskID depends on and are NOT COMPLETED.
// Used to enforce the dependency completion guard before allowing a task to be marked COMPLETED.
func (db *DB) GetIncompleteDependencies(ctx context.Context, taskID int) ([]int, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT td.depends_on_task_id
		 FROM task_dependencies td
		 INNER JOIN tasks t ON t.id = td.depends_on_task_id
		 WHERE td.task_id = ? AND t.status != ?`,
		taskID, models.StatusCompleted,
	)
	if err != nil {
		return nil, fmt.Errorf("querying incomplete dependencies: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning dependency id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating dependency rows: %w", err)
	}
	return ids, nil
}

// GetIncompleteDependenciesByTasks returns a map from task_id to the list of
// task IDs it depends on that are NOT in COMPLETED status. Tasks with no
// incomplete dependencies are absent from the result. One round-trip
// regardless of the number of tasks queried.
func (db *DB) GetIncompleteDependenciesByTasks(ctx context.Context, taskIDs []int) (map[int][]int, error) {
	if len(taskIDs) == 0 {
		return map[int][]int{}, nil
	}
	placeholders := db.queryCache.GetPlaceholders(len(taskIDs))
	args := make([]any, 0, len(taskIDs)+1)
	for _, id := range taskIDs {
		args = append(args, id)
	}
	args = append(args, models.StatusCompleted)
	query := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated
		`SELECT td.task_id, td.depends_on_task_id
		 FROM task_dependencies td
		 INNER JOIN tasks t ON t.id = td.depends_on_task_id
		 WHERE td.task_id IN (%s) AND t.status != ?
		 ORDER BY td.task_id, td.depends_on_task_id`,
		placeholders,
	)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying incomplete dependencies: %w", err)
	}
	defer rows.Close()
	result := make(map[int][]int, len(taskIDs))
	for rows.Next() {
		var tid, depID int
		if err := rows.Scan(&tid, &depID); err != nil {
			return nil, fmt.Errorf("scanning incomplete dependency: %w", err)
		}
		result[tid] = append(result[tid], depID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating incomplete dependency rows: %w", err)
	}
	return result, nil
}

// GetTaskDependsOn returns the IDs of all tasks that taskID depends on.
func (db *DB) GetTaskDependsOn(ctx context.Context, taskID int) ([]int, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT depends_on_task_id FROM task_dependencies WHERE task_id = ? ORDER BY depends_on_task_id ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying task depends_on: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning depends_on id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating depends_on rows: %w", err)
	}
	return ids, nil
}

// GetTaskBlocks returns the IDs of all tasks that depend on taskID.
func (db *DB) GetTaskBlocks(ctx context.Context, taskID int) ([]int, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT task_id FROM task_dependencies WHERE depends_on_task_id = ? ORDER BY task_id ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying task blocks: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning blocks id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating blocks rows: %w", err)
	}
	return ids, nil
}

// hasTransitiveDependency checks if fromID transitively depends on targetID.
// Returns true if there is a path fromID →...→ targetID through
// task_dependencies, computed via a single recursive CTE in SQLite.
func (db *DB) hasTransitiveDependency(ctx context.Context, fromID, targetID int) (bool, error) {
	if fromID == targetID {
		return true, nil
	}
	const query = `
		WITH RECURSIVE deps(id) AS (
			SELECT depends_on_task_id FROM task_dependencies WHERE task_id = ?
			UNION
			SELECT td.depends_on_task_id
			FROM task_dependencies td
			JOIN deps ON td.task_id = deps.id
		)
		SELECT 1 FROM deps WHERE id = ? LIMIT 1`
	var found int
	err := db.QueryRowContext(ctx, query, fromID, targetID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking transitive dependency: %w", err)
	}
	return found == 1, nil
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
	var maxTasks sql.NullInt64

	// Single query using JSON aggregation to get sprint data and task IDs
	// json_group_array returns a JSON array of task IDs
	err := db.QueryRowContext(ctx,
		`SELECT
			s.id, s.status, s.description, s.created_at, s.started_at, s.closed_at, s.max_tasks,
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
		&maxTasks,
		&tasksJSON,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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
	if maxTasks.Valid {
		v := int(maxTasks.Int64)
		sprint.MaxTasks = &v
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

	var result []int
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("parsing JSON array: %w", err)
	}

	return result, nil
}

// ListSprints retrieves all sprints with optional status filter.
func (db *DB) ListSprints(ctx context.Context, status *models.SprintStatus) ([]models.Sprint, error) {
	query := `SELECT id, status, description, created_at, started_at, closed_at, max_tasks FROM sprints WHERE 1=1`
	args := []any{}

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
		var maxTasks sql.NullInt64

		err := rows.Scan(
			&sprint.ID,
			&sprint.Status,
			&sprint.Description,
			&sprint.CreatedAt,
			&startedAt,
			&closedAt,
			&maxTasks,
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
		if maxTasks.Valid {
			v := int(maxTasks.Int64)
			sprint.MaxTasks = &v
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
		var args []any

		switch status {
		case models.SprintOpen:
			// Starting sprint
			query = "UPDATE sprints SET status = ?, started_at = ? WHERE id = ?"
			args = []any{status, utils.NowISO8601(), id}
		case models.SprintClosed:
			// Closing sprint
			query = "UPDATE sprints SET status = ?, closed_at = ? WHERE id = ?"
			args = []any{status, utils.NowISO8601(), id}
		default:
			// Other status changes
			query = "UPDATE sprints SET status = ? WHERE id = ?"
			args = []any{status, id}
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
			`UPDATE tasks SET status = ? WHERE id IN (
				SELECT task_id FROM sprint_tasks WHERE sprint_id = ?
			)`,
			models.StatusBacklog, id,
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

// GetActiveSprintTasks retrieves tasks in a sprint with status SPRINT, DOING, or TESTING.
// SPRINT tasks were assigned but never started; DOING/TESTING tasks are actively in progress.
// Used to validate sprint close safety.
func (db *DB) GetActiveSprintTasks(ctx context.Context, sprintID int) ([]models.Task, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements,
		         t.acceptance_criteria, t.created_at, t.specialists, t.started_at, t.tested_at,
		         t.closed_at, t.completion_summary, t.parent_task_id,
		         t.priority, t.severity,
		         (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count
		      FROM tasks t
		      INNER JOIN sprint_tasks st ON t.id = st.task_id
		      WHERE st.sprint_id = ? AND t.status IN `+sqlActiveTaskStatuses+`
		      ORDER BY st.position ASC`,
		sprintID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying active sprint tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

// GetSprintTasksFull retrieves full task objects for a sprint, ordered by position or priority.
func (db *DB) GetSprintTasksFull(ctx context.Context, sprintID int, status *models.TaskStatus, orderByPriority bool) ([]models.Task, error) {
	query := `SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements,
		         t.acceptance_criteria, t.created_at, t.specialists, t.started_at, t.tested_at,
		         t.closed_at, t.completion_summary, t.parent_task_id,
		         t.priority, t.severity,
		         (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count
		      FROM tasks t
		      INNER JOIN sprint_tasks st ON t.id = st.task_id
		      WHERE st.sprint_id = ?`
	args := []any{sprintID}

	if status != nil {
		query += " AND t.status = ?"
		args = append(args, string(*status))
	}

	if orderByPriority {
		query += " ORDER BY t.priority DESC, st.position ASC"
	} else {
		query += " ORDER BY st.position ASC"
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying sprint tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

// GetOpenSprintTasks retrieves tasks in a sprint that are not yet completed.
// Returns tasks with status SPRINT, DOING, or TESTING, ordered by sprint position.
// When orderByPriority is true, tasks are ordered by priority DESC then position ASC.
// Returns an empty slice (not an error) when the sprint has no open tasks.
func (db *DB) GetOpenSprintTasks(ctx context.Context, sprintID int, orderByPriority bool) ([]models.Task, error) {
	query := `SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements,
		         t.acceptance_criteria, t.created_at, t.specialists, t.started_at, t.tested_at,
		         t.closed_at, t.completion_summary, t.parent_task_id,
		         t.priority, t.severity,
		         (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count
		      FROM tasks t
		      INNER JOIN sprint_tasks st ON t.id = st.task_id
		      WHERE st.sprint_id = ?
		        AND t.status IN ` + sqlActiveTaskStatuses + ``

	if orderByPriority {
		query += " ORDER BY t.priority DESC, st.position ASC"
	} else {
		query += " ORDER BY st.position ASC"
	}

	rows, err := db.QueryContext(ctx, query, sprintID)
	if err != nil {
		return nil, fmt.Errorf("querying open sprint tasks: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

// AddTasksToSprint adds tasks to a sprint with automatic position assignment.
// Tasks are added at the end of the sprint task list (highest position + 1).
func (db *DB) AddTasksToSprint(ctx context.Context, sprintID int, taskIDs []int) error {
	if len(taskIDs) == 0 {
		return nil
	}

	return retryWithBackoff("add tasks to sprint", func() error {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck // deferred rollback, transaction error already captured

		now := utils.NowISO8601()

		// Get current max position for this sprint within the transaction
		var maxPos sql.NullInt64
		err = tx.QueryRow(
			"SELECT MAX(position) FROM sprint_tasks WHERE sprint_id = ?",
			sprintID,
		).Scan(&maxPos)
		if err != nil {
			return fmt.Errorf("querying max position: %w", err)
		}

		startPos := -1
		if maxPos.Valid {
			startPos = int(maxPos.Int64)
		}

		// Multi-row INSERT: one round-trip for all tasks.
		valueGroups := make([]string, len(taskIDs))
		insertArgs := make([]any, 0, 4*len(taskIDs))
		for i, taskID := range taskIDs {
			valueGroups[i] = "(?, ?, ?, ?)"
			insertArgs = append(insertArgs, sprintID, taskID, now, startPos+i+1)
		}
		insertQuery := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated
			`INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position) VALUES %s
			 ON CONFLICT(task_id) DO UPDATE SET sprint_id = excluded.sprint_id, added_at = excluded.added_at, position = excluded.position`,
			strings.Join(valueGroups, ","),
		)
		if _, err := tx.Exec(insertQuery, insertArgs...); err != nil {
			return fmt.Errorf("adding tasks to sprint: %w", err)
		}

		// Update task status to SPRINT using cached placeholders.
		placeholders := db.queryCache.GetPlaceholders(len(taskIDs))
		statusArgs := make([]any, len(taskIDs))
		for i, id := range taskIDs {
			statusArgs[i] = id
		}
		statusQuery := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
			"UPDATE tasks SET status = ? WHERE id IN (%s)",
			placeholders,
		)
		// Prepend the status value so the placeholder order matches: status, id, id, ...
		statusArgsWithStatus := append([]any{models.StatusSprint}, statusArgs...)
		if _, err := tx.Exec(statusQuery, statusArgsWithStatus...); err != nil {
			return fmt.Errorf("updating task statuses: %w", err)
		}

		return tx.Commit()
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
		args := make([]any, len(taskIDs))
		for i, id := range taskIDs {
			args[i] = id
		}

		// Delete from sprint_tasks
		query := fmt.Sprintf("DELETE FROM sprint_tasks WHERE task_id IN (%s)", placeholders)
		_, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("removing tasks from sprint: %w", err)
		}

		// Update task status to BACKLOG. Prepend status value to args.
		query = fmt.Sprintf("UPDATE tasks SET status = ? WHERE id IN (%s)", placeholders) // #nosec G201 -- only ? placeholders interpolated
		statusArgs := append([]any{models.StatusBacklog}, args...)
		if _, err := db.ExecContext(ctx, query, statusArgs...); err != nil {
			return fmt.Errorf("updating task statuses: %w", err)
		}

		return nil
	})
}

// ==================== AUDIT QUERIES ====================

// LogAuditTx inserts an audit row inside an existing transaction. The 21+
// transactional sites that write audit rows alongside a domain mutation
// must call this rather than spelling out the INSERT manually — it keeps
// the table layout in one place and lets writers stay terse.
func LogAuditTx(tx *sql.Tx, op models.AuditOperation, entityType models.EntityType, entityID int, performedAt string) error {
	_, err := tx.Exec(
		`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
		op, entityType, entityID, performedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting audit entry: %w", err)
	}
	return nil
}

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

// LogAuditEntriesBatch inserts multiple audit entries using a prepared statement.
// This is significantly faster than individual inserts for batch operations.
func (db *DB) LogAuditEntriesBatch(ctx context.Context, entries []*models.AuditEntry) error {
	if len(entries) == 0 {
		return nil
	}

	return retryWithBackoff("log audit entries batch", func() error {
		// Prepare the statement once for reuse
		stmt, err := db.PrepareContext(ctx,
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("preparing audit statement: %w", err)
		}
		defer stmt.Close()

		// Execute for each entry using the prepared statement
		for _, entry := range entries {
			_, err = stmt.ExecContext(ctx,
				entry.Operation,
				entry.EntityType,
				entry.EntityID,
				entry.PerformedAt,
			)
			if err != nil {
				return fmt.Errorf("executing audit insert: %w", err)
			}
		}
		return nil
	})
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
	// Build the query with strings.Builder so we don't allocate a new
	// backing string for every appended clause.
	var qb strings.Builder
	qb.Grow(256) // rough upper bound for SELECT + 7 clauses
	qb.WriteString(`SELECT id, operation, entity_type, entity_id, performed_at FROM audit WHERE 1=1`)
	args := make([]any, 0, 7)

	if operation != nil {
		qb.WriteString(" AND operation = ?")
		args = append(args, *operation)
	}
	if entityType != nil {
		qb.WriteString(" AND entity_type = ?")
		args = append(args, *entityType)
	}
	if entityID != nil {
		qb.WriteString(" AND entity_id = ?")
		args = append(args, *entityID)
	}
	if since != nil {
		qb.WriteString(" AND performed_at >= ?")
		args = append(args, *since)
	}
	if until != nil {
		qb.WriteString(" AND performed_at <= ?")
		args = append(args, *until)
	}

	qb.WriteString(" ORDER BY performed_at DESC")
	if limit > 0 {
		qb.WriteString(" LIMIT ?")
		args = append(args, limit)
	}
	if offset > 0 {
		qb.WriteString(" OFFSET ?")
		args = append(args, offset)
	}

	rows, err := db.QueryContext(ctx, qb.String(), args...)
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

	// One pass over the audit table, grouped by (operation, entity_type),
	// returns enough information to derive every field of AuditStats:
	//   total = sum(cnt)
	//   ByOperation[op]    = sum(cnt) per op
	//   ByEntityType[et]   = sum(cnt) per et
	//   FirstEntryAt       = min(min_at)
	//   LastEntryAt        = max(max_at)
	var qb strings.Builder
	qb.Grow(256)
	qb.WriteString(`SELECT operation, entity_type, COUNT(*), MIN(performed_at), MAX(performed_at) FROM audit WHERE 1=1`)
	args := make([]any, 0, 2)
	if since != nil {
		qb.WriteString(" AND performed_at >= ?")
		args = append(args, *since)
	}
	if until != nil {
		qb.WriteString(" AND performed_at <= ?")
		args = append(args, *until)
	}
	qb.WriteString(" GROUP BY operation, entity_type")

	rows, err := db.QueryContext(ctx, qb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("aggregating audit stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var op, ent string
		var cnt int
		var minAt, maxAt sql.NullString
		if err := rows.Scan(&op, &ent, &cnt, &minAt, &maxAt); err != nil {
			return nil, fmt.Errorf("scanning audit stats row: %w", err)
		}
		stats.TotalEntries += cnt
		stats.ByOperation[op] += cnt
		stats.ByEntityType[ent] += cnt
		if minAt.Valid {
			if stats.FirstEntryAt == "" || minAt.String < stats.FirstEntryAt {
				stats.FirstEntryAt = minAt.String
			}
		}
		if maxAt.Valid {
			if maxAt.String > stats.LastEntryAt {
				stats.LastEntryAt = maxAt.String
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating audit stats rows: %w", err)
	}
	return stats, nil
}

// ==================== SPRINT TASK ORDERING QUERIES ====================

// getMaxPositionInternal retrieves the maximum position value for a sprint.
// Returns -1 if sprint has no tasks, meaning first task gets position 0.
func (db *DB) getMaxPositionInternal(sprintID int) (int, error) {
	var maxPos sql.NullInt64
	err := db.QueryRow(
		"SELECT MAX(position) FROM sprint_tasks WHERE sprint_id = ?",
		sprintID,
	).Scan(&maxPos)
	if err != nil {
		return -1, fmt.Errorf("querying max position: %w", err)
	}
	if maxPos.Valid {
		return int(maxPos.Int64), nil
	}
	return -1, nil
}

// GetMaxPosition retrieves the maximum position value for a sprint.
// Returns -1 if sprint has no tasks.
func (db *DB) GetMaxPosition(sprintID int) (int, error) {
	return db.getMaxPositionInternal(sprintID)
}

// ReorderSprintTasks sets the exact order of tasks in a sprint.
// All task IDs must belong to the sprint, and the list must be complete.
// Positions are assigned sequentially starting from 0.
func (db *DB) ReorderSprintTasks(sprintID int, taskIDs []int) error {
	if len(taskIDs) == 0 {
		return nil
	}

	return retryWithBackoff("reorder sprint tasks", func() error {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck // deferred rollback, transaction error already captured

		// Verify all task IDs belong to this sprint
		placeholders := make([]string, len(taskIDs))
		args := make([]any, len(taskIDs)+1)
		args[0] = sprintID
		for i, id := range taskIDs {
			placeholders[i] = "?"
			args[i+1] = id
		}

		countQuery := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
			"SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ? AND task_id IN (%s)",
			strings.Join(placeholders, ","),
		)
		var count int
		err = tx.QueryRow(countQuery, args...).Scan(&count)
		if err != nil {
			return fmt.Errorf("verifying task membership: %w", err)
		}
		if count != len(taskIDs) {
			return fmt.Errorf("%w: sprint %d", ErrTasksNotInSprint, sprintID)
		}

		// Update positions
		for i, taskID := range taskIDs {
			_, err := tx.Exec(
				"UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?",
				i, sprintID, taskID,
			)
			if err != nil {
				return fmt.Errorf("updating position for task %d: %w", taskID, err)
			}
		}

		// Log audit entry
		now := utils.NowISO8601()
		if err := LogAuditTx(tx, models.OpSprintReorderTasks, models.EntitySprint, sprintID, now); err != nil {
			return fmt.Errorf("logging audit entry: %w", err)
		}

		return tx.Commit()
	})
}

// MoveTaskToPosition moves a single task to a specific position within a sprint,
// shifting other tasks to maintain continuous positions (0, 1, 2...).
// If position >= task count, the task is moved to the end.
func (db *DB) MoveTaskToPosition(sprintID, taskID, newPosition int) error {
	return retryWithBackoff("move task to position", func() error {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck // deferred rollback, transaction error already captured

		// Get current position of the task
		var currentPos int
		err = tx.QueryRow(
			"SELECT position FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?",
			sprintID, taskID,
		).Scan(&currentPos)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: task %d not found in sprint %d", utils.ErrNotFound, taskID, sprintID)
			}
			return fmt.Errorf("getting current position: %w", err)
		}

		// Get task count to handle position beyond range
		var taskCount int
		err = tx.QueryRow(
			"SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ?",
			sprintID,
		).Scan(&taskCount)
		if err != nil {
			return fmt.Errorf("getting task count: %w", err)
		}

		// If position >= task count, move to end
		if newPosition >= taskCount {
			newPosition = taskCount - 1
		}

		// If position unchanged, nothing to do
		if currentPos == newPosition {
			return nil
		}

		if newPosition < currentPos {
			// Moving UP: shift tasks between new_position and current_position-1 down by 1
			_, err = tx.Exec(
				`UPDATE sprint_tasks SET position = position + 1
				 WHERE sprint_id = ? AND position >= ? AND position < ?`,
				sprintID, newPosition, currentPos,
			)
		} else {
			// Moving DOWN: shift tasks between current_position+1 and new_position up by 1
			_, err = tx.Exec(
				`UPDATE sprint_tasks SET position = position - 1
				 WHERE sprint_id = ? AND position > ? AND position <= ?`,
				sprintID, currentPos, newPosition,
			)
		}
		if err != nil {
			return fmt.Errorf("shifting task positions: %w", err)
		}

		// Update the moved task to the new position
		_, err = tx.Exec(
			"UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?",
			newPosition, sprintID, taskID,
		)
		if err != nil {
			return fmt.Errorf("updating task position: %w", err)
		}

		// Log audit entry
		now := utils.NowISO8601()
		if err := LogAuditTx(tx, models.OpSprintTaskMovePosition, models.EntitySprint, sprintID, now); err != nil {
			return fmt.Errorf("logging audit entry: %w", err)
		}

		return tx.Commit()
	})
}

// SwapTasks exchanges the positions of two tasks in a sprint.
// Both tasks must belong to the same sprint.
func (db *DB) SwapTasks(sprintID, taskID1, taskID2 int) error {
	if taskID1 == taskID2 {
		return ErrCannotSwapSelf
	}

	return retryWithBackoff("swap tasks", func() error {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck // deferred rollback, transaction error already captured

		// Get positions of both tasks
		var pos1, pos2 int
		var count int

		rows, err := tx.Query(
			"SELECT task_id, position FROM sprint_tasks WHERE sprint_id = ? AND task_id IN (?, ?)",
			sprintID, taskID1, taskID2,
		)
		if err != nil {
			return fmt.Errorf("querying task positions: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var id, pos int
			if err := rows.Scan(&id, &pos); err != nil {
				return fmt.Errorf("scanning task position: %w", err)
			}
			if id == taskID1 {
				pos1 = pos
			} else {
				pos2 = pos
			}
			count++
		}

		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterating task positions: %w", err)
		}

		if count != 2 {
			return fmt.Errorf("%w: sprint %d", ErrSwapTasksNotFound, sprintID)
		}

		// Swap positions
		_, err = tx.Exec(
			"UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?",
			pos2, sprintID, taskID1,
		)
		if err != nil {
			return fmt.Errorf("updating position for task %d: %w", taskID1, err)
		}

		_, err = tx.Exec(
			"UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?",
			pos1, sprintID, taskID2,
		)
		if err != nil {
			return fmt.Errorf("updating position for task %d: %w", taskID2, err)
		}

		// Log audit entry
		now := utils.NowISO8601()
		if err := LogAuditTx(tx, models.OpSprintTaskSwap, models.EntitySprint, sprintID, now); err != nil {
			return fmt.Errorf("logging audit entry: %w", err)
		}

		return tx.Commit()
	})
}

// ==================== ROADMAP STATISTICS QUERIES ====================

// GetRoadmapStats retrieves comprehensive statistics for a roadmap.
// Returns sprint counts (total, open, closed, current), task counts by status,
// and average velocity across the last 5 closed sprints.
func (db *DB) GetRoadmapStats(ctx context.Context, roadmapName string) (*models.RoadmapStats, error) {
	stats := &models.RoadmapStats{
		Roadmap: roadmapName,
	}

	// Get sprint statistics
	sprintStats, err := db.getSprintStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting sprint stats: %w", err)
	}
	stats.Sprints = *sprintStats

	// Get task statistics by status
	taskStats, err := db.getTaskStatsByStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting task stats: %w", err)
	}
	stats.Tasks = *taskStats

	// Get average velocity across last 5 closed sprints.
	avgVelocity, err := db.GetAverageVelocity(ctx, 5)
	if err != nil {
		return nil, fmt.Errorf("getting average velocity: %w", err)
	}
	stats.AverageVelocity = avgVelocity

	return stats, nil
}

// getSprintStats retrieves sprint statistics from the database.
func (db *DB) getSprintStats(ctx context.Context) (*models.SprintStatsSummary, error) {
	stats := &models.SprintStatsSummary{}

	// Get total sprint count
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sprints").Scan(&stats.Total)
	if err != nil {
		return nil, fmt.Errorf("counting total sprints: %w", err)
	}

	// Get completed (closed) sprint count
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sprints WHERE status = ?",
		models.SprintClosed,
	).Scan(&stats.Completed)
	if err != nil {
		return nil, fmt.Errorf("counting closed sprints: %w", err)
	}

	// Get pending (open) sprint count
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sprints WHERE status = ?",
		models.SprintOpen,
	).Scan(&stats.Pending)
	if err != nil {
		return nil, fmt.Errorf("counting open sprints: %w", err)
	}

	// Get current open sprint ID (if any)
	var currentSprintID sql.NullInt64
	err = db.QueryRowContext(ctx,
		"SELECT id FROM sprints WHERE status = ? LIMIT 1",
		models.SprintOpen,
	).Scan(&currentSprintID)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("getting current sprint: %w", err)
	}

	if currentSprintID.Valid {
		id := int(currentSprintID.Int64)
		stats.Current = &id
	}

	return stats, nil
}

// getTaskStatsByStatus retrieves task counts grouped by status.
func (db *DB) getTaskStatsByStatus(ctx context.Context) (*models.TaskStatsSummary, error) {
	stats := &models.TaskStatsSummary{}

	// Query to get counts by status
	rows, err := db.QueryContext(ctx,
		"SELECT status, COUNT(*) FROM tasks GROUP BY status",
	)
	if err != nil {
		return nil, fmt.Errorf("querying task counts by status: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var statusStr string
		var count int
		if err := rows.Scan(&statusStr, &count); err != nil {
			return nil, fmt.Errorf("scanning task count: %w", err)
		}

		switch models.TaskStatus(statusStr) {
		case models.StatusBacklog:
			stats.Backlog = count
		case models.StatusSprint:
			stats.Sprint = count
		case models.StatusDoing:
			stats.Doing = count
		case models.StatusTesting:
			stats.Testing = count
		case models.StatusCompleted:
			stats.Completed = count
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task counts: %w", err)
	}

	return stats, nil
}

// ==================== SPRINT VELOCITY AND BURNDOWN QUERIES ====================

// GetSprintBurndown computes the burndown series for a sprint.
// It derives completion dates from tasks.closed_at for all tasks that belong to the sprint.
// Returns a slice of BurndownEntry ordered by date ascending, starting from the sprint start date
// with total_tasks remaining and decrementing by completions per day.
// Returns an empty slice when no tasks have been completed.
func (db *DB) GetSprintBurndown(ctx context.Context, sprintID int) ([]models.BurndownEntry, error) {
	// Get the sprint to determine total task count and start date.
	sprint, err := db.GetSprint(ctx, sprintID)
	if err != nil {
		return nil, fmt.Errorf("getting sprint for burndown: %w", err)
	}

	// Count total tasks in the sprint.
	var totalTasks int
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ?`,
		sprintID,
	).Scan(&totalTasks)
	if err != nil {
		return nil, fmt.Errorf("counting sprint tasks for burndown: %w", err)
	}

	// Query completions per day: tasks in this sprint that have a closed_at date (COMPLETED status).
	// SQLite substr extracts the date portion (YYYY-MM-DD) from the ISO 8601 timestamp.
	rows, err := db.QueryContext(ctx,
		`SELECT substr(t.closed_at, 1, 10) AS completion_date, COUNT(*) AS completed_count
		 FROM tasks t
		 INNER JOIN sprint_tasks st ON st.task_id = t.id
		 WHERE st.sprint_id = ?
		   AND t.status = `+sqlStatusCompleted+`
		   AND t.closed_at IS NOT NULL
		 GROUP BY completion_date
		 ORDER BY completion_date ASC`,
		sprintID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying burndown completions: %w", err)
	}
	defer rows.Close()

	type dailyCount struct {
		date  string
		count int
	}

	var dailyCounts []dailyCount
	for rows.Next() {
		var dc dailyCount
		if scanErr := rows.Scan(&dc.date, &dc.count); scanErr != nil {
			return nil, fmt.Errorf("scanning burndown row: %w", scanErr)
		}
		dailyCounts = append(dailyCounts, dc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating burndown rows: %w", err)
	}

	if len(dailyCounts) == 0 {
		return []models.BurndownEntry{}, nil
	}

	// Build the burndown series.
	// If sprint has a started_at, use it as the baseline; otherwise start from the first completion date.
	var startDate string
	if sprint.StartedAt != nil && *sprint.StartedAt != "" {
		startDate = (*sprint.StartedAt)[:10] // Extract YYYY-MM-DD
	} else {
		startDate = dailyCounts[0].date
	}

	entries := make([]models.BurndownEntry, 0, len(dailyCounts)+1)

	// Include start day with all tasks remaining (before any completions).
	if startDate < dailyCounts[0].date {
		entries = append(entries, models.BurndownEntry{
			Date:           startDate,
			TasksRemaining: totalTasks,
		})
	}

	remaining := totalTasks
	for _, dc := range dailyCounts {
		remaining -= dc.count
		if remaining < 0 {
			remaining = 0
		}
		entries = append(entries, models.BurndownEntry{
			Date:           dc.date,
			TasksRemaining: remaining,
		})
	}

	return entries, nil
}

// GetAverageVelocity computes the average velocity across the last N closed sprints.
// Velocity for each sprint = completed_tasks / sprint_duration_days.
// Sprints without a started_at or closed_at, or with zero duration, are excluded from the count.
// Sprints with zero completed tasks contribute 0.0 to the average.
// Returns 0.0 when no qualifying sprints exist.
func (db *DB) GetAverageVelocity(ctx context.Context, limit int) (float64, error) {
	if limit <= 0 {
		limit = 5
	}

	// Fetch the last N closed sprints that have both started_at and closed_at set.
	rows, err := db.QueryContext(ctx,
		`SELECT s.id, s.started_at, s.closed_at,
		        (SELECT COUNT(*) FROM sprint_tasks st
		         INNER JOIN tasks t ON t.id = st.task_id
		         WHERE st.sprint_id = s.id AND t.status = `+sqlStatusCompleted+`) AS completed_count
		 FROM sprints s
		 WHERE s.status = `+sqlSprintClosed+`
		   AND s.started_at IS NOT NULL
		   AND s.closed_at IS NOT NULL
		 ORDER BY s.closed_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return 0.0, fmt.Errorf("querying closed sprints for velocity: %w", err)
	}
	defer rows.Close()

	var totalVelocity float64
	var count int

	for rows.Next() {
		var sprintID, completedCount int
		var startedAt, closedAt string
		if scanErr := rows.Scan(&sprintID, &startedAt, &closedAt, &completedCount); scanErr != nil {
			return 0.0, fmt.Errorf("scanning sprint velocity row: %w", scanErr)
		}

		startTime, err1 := time.Parse("2006-01-02T15:04:05.000Z", startedAt)
		closeTime, err2 := time.Parse("2006-01-02T15:04:05.000Z", closedAt)
		// Also try RFC3339 variants for robustness.
		if err1 != nil {
			startTime, err1 = time.Parse(time.RFC3339, startedAt)
		}
		if err2 != nil {
			closeTime, err2 = time.Parse(time.RFC3339, closedAt)
		}
		if err1 != nil || err2 != nil {
			// Skip sprints with unparseable dates.
			continue
		}

		durationDays := closeTime.Sub(startTime).Hours() / 24
		if durationDays <= 0 {
			// Zero-duration sprint: exclude from count entirely (would be a data anomaly).
			continue
		}

		if completedCount > 0 {
			totalVelocity += float64(completedCount) / durationDays
		}
		// completedCount == 0: contribute 0.0 (already zero, just increment count).
		count++
	}
	if err := rows.Err(); err != nil {
		return 0.0, fmt.Errorf("iterating sprint velocity rows: %w", err)
	}

	if count == 0 {
		return 0.0, nil
	}

	return totalVelocity / float64(count), nil
}
