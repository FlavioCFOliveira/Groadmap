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
	"math"
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
	// hold while it sits inside a sprint: SPRINT, DOING, TESTING. Used for
	// sprint capacity accounting (a merely-assigned SPRINT task still occupies
	// a slot).
	sqlActiveTaskStatuses = "('" + string(models.StatusSprint) + "', '" +
		string(models.StatusDoing) + "', '" + string(models.StatusTesting) + "')"
	// sqlStatusCompleted and sqlSprintClosed are inlined into stats queries
	// that group by status; using parameters there would require restructuring
	// already-complex multi-clause aggregations.
	sqlStatusCompleted = "'" + string(models.StatusCompleted) + "'"
	sqlSprintClosed    = "'" + string(models.SprintClosed) + "'"
)

// ==================== TASK QUERIES ====================

// InsertTaskTx inserts one task row inside an existing transaction and returns
// its id.
//
// This is the only implementation of the task INSERT. `task create` runs it
// inside the transaction that also writes the TASK_CREATE audit entry
// (SPEC/DATABASE.md § Transactional Atomicity Guarantees), and this package's
// fixtures seed through it, so no test can be built on SQL the binary does not
// run. The connection-scoped CreateTask that used to sit here was a second
// INSERT with no audit entry and no transaction to share; no command could use
// it, and only tests ever did (task #188).
//
// Only the columns a task is born with are written. The lifecycle columns —
// started_at, tested_at, closed_at, completion_summary, commit_open,
// commit_close — are left to the schema, because a new task is in BACKLOG and
// reaches them only through a transition.
//
// Errors are returned unwrapped: the caller owns the message, because the same
// failure means different things to `task create` and to a fixture.
func InsertTaskTx(tx *sql.Tx, task *models.Task) (int, error) {
	result, err := tx.Exec(
		`INSERT INTO tasks (title, status, type, functional_requirements, technical_requirements, acceptance_criteria, created_at, priority, severity, parent_task_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.Title, task.Status, task.Type, task.FunctionalRequirements, task.TechnicalRequirements,
		task.AcceptanceCriteria, task.CreatedAt, task.Priority, task.Severity,
		task.ParentTaskID,
	)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	return int(id), nil
}

// GetTask retrieves a task by ID, including dependencies and subtask_count.
// Uses scanTasksWithDeps to fold depends_on / blocks into the same query.
func (db *DB) GetTask(ctx context.Context, id int) (*models.Task, error) {
	query := `SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
	        t.created_at, t.started_at, t.tested_at, t.closed_at, t.completion_summary,
	        t.commit_open, t.commit_close, t.parent_task_id,
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

// GetTasks retrieves multiple tasks by IDs, ordered by id ascending.
//
// The id set is sorted then chunked through the BatchProcessor so it never
// exceeds SQLite's variable limit (SQLITE_LIMIT_VARIABLE_NUMBER, ~999) and each
// chunk reuses the cached OpGetTasks template (a query plan). Because the ids
// are pre-sorted and chunks are processed in order, each chunk's per-query
// "ORDER BY t.id" composes into a globally id-ascending result — identical to
// the single-query behaviour for small sets.
func (db *DB) GetTasks(ctx context.Context, ids []int) ([]models.Task, error) {
	if len(ids) == 0 {
		return []models.Task{}, nil
	}

	// Sort a copy so the caller's slice is not mutated and cross-chunk order
	// is globally ascending.
	sorted := make([]int, len(ids))
	copy(sorted, ids)
	sort.Ints(sorted)

	// The cached template is byte-identical to the projection
	// scanTasksWithDeps expects, so the row shape is unchanged.
	return ProcessChunksWithResult(sorted, db.batchProc.BatchSize(), func(chunk []int) ([]models.Task, error) {
		query := db.queryCache.GetQuery(OpGetTasks, len(chunk))
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}

		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("querying tasks: %w", err)
		}
		defer rows.Close()

		return scanTasksWithDeps(rows)
	})
}

// TaskListFilter holds all optional filter and sort parameters for ListTasks.
type TaskListFilter struct {
	Status       *models.TaskStatus
	MinPriority  *int
	MinSeverity  *int
	TaskType     *models.TaskType
	CreatedSince *time.Time // inclusive lower bound on created_at
	CreatedUntil *time.Time // inclusive upper bound on created_at
	Sort         string     // "priority" (default), "created", "status", "severity"
	Limit        int
}

// ListTasks retrieves tasks with optional filters.
// Filters: status, minPriority, minSeverity, taskType, createdSince, createdUntil, sort, limit.
//
// A nil filter means "no filter at all" and is answered with the default page, as
// in GetAuditEntries: every field of TaskListFilter is optional, so the whole
// struct is too, and a nil pointer must not crash a read.
//
// The clamping is applied to a LOCAL COPY, so the caller's struct is never
// mutated. That matters beyond tidiness: one filter value shared by concurrent
// readers would otherwise be written by each of them, which is a data race on
// caller memory rather than an internal detail.
func (db *DB) ListTasks(ctx context.Context, filter *TaskListFilter) ([]models.Task, error) {
	effective := TaskListFilter{}
	if filter != nil {
		effective = *filter
	}
	if effective.Limit < 1 {
		effective.Limit = models.DefaultTaskLimit
	}
	if effective.Limit > models.MaxTaskLimit {
		effective.Limit = models.MaxTaskLimit
	}

	query, args := buildListTasksQuery(&effective)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer rows.Close()

	return scanTasksWithDeps(rows)
}

// buildListTasksQuery assembles the SQL and bind arguments ListTasks executes.
// Assembly is separated from execution so that the index tests can take the
// query plan of the exact SQL production runs, rather than of a lookalike
// (SPEC/DATABASE.md § Performance Optimization).
//
// The caller is responsible for clamping filter.Limit beforehand.
func buildListTasksQuery(filter *TaskListFilter) (string, []any) {
	query := `SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
		        t.created_at, t.started_at, t.tested_at, t.closed_at, t.completion_summary,
		        t.commit_open, t.commit_close, t.parent_task_id,
		        t.priority, t.severity,
		        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count` + taskDepsSelect + `
		      FROM tasks t WHERE 1=1`
	// 6 filters + LIMIT = up to 7 placeholders; +1 to absorb a future
	// arg without forcing an extra grow.
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
	// The LIMIT is emitted only when the caller asked for one. SPEC/DATABASE.md
	// § Main SQL Queries, "List All" states the listing itself carries no LIMIT:
	// any bound on the row count is imposed by the caller. ListTasks always
	// clamps its limit to at least 1 before calling, so the CLI listing is
	// unchanged; ListAllTasks passes 0 to read every row.
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	return query, args
}

// ListAllTasks returns EVERY task of the roadmap, in the default listing order
// (priority DESC, created_at ASC), with no LIMIT and no pagination.
//
// It is the read the web interface's Kanban task board performs, and reading
// every row is a correctness requirement of that page rather than a performance
// choice: the board groups the tasks into five columns and prints a count on each
// column header as a statement of fact about the roadmap, so a partial read would
// not merely show fewer cards — it would publish wrong counts as true ones, with
// nothing on the page to reveal that anything was omitted (SPEC/DATABASE.md
// § Main SQL Queries, "List All"; SPEC/WEB.md § Roadmap Tasks Page, Unbounded
// read).
//
// The display default that sizes `rmp task list` output (models.DefaultTaskLimit)
// and the per-invocation cap (models.MaxTaskLimit) are deliberately NOT applied
// here. They size the output of one command invocation, where a caller who wants
// more asks for more and can see that the listing was cut; this read has no such
// affordance. ListTasks keeps both, so the CLI is unaffected.
func (db *DB) ListAllTasks(ctx context.Context) ([]models.Task, error) {
	query, args := buildListTasksQuery(&TaskListFilter{})

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing every task: %w", err)
	}
	defer rows.Close()

	return scanTasksWithDeps(rows)
}

// Task mutation has no method here on purpose, and neither have the field
// edit, the status transition, the priority and severity changes, nor the
// delete. Each is one indivisible operation with its audit entries, and each
// is decided by rules the database layer is the wrong place to hold: the state
// machine and its lifecycle timestamps and commit hashes for a transition
// (SPEC/STATE_MACHINE.md), the per-field audit operation for an edit, the
// fail-fast existence check for a batch, and the BACKLOG-only rule plus the
// subtask guard for a delete. They live in internal/commands, next to the
// validation that decides them, in the transaction that also writes what
// happened.
//
// A second copy here would be unreachable from the binary and therefore
// ungated, which is not a hypothetical: the copy of the status update that used
// to sit at this spot never wrote an audit entry, never touched commit_open or
// commit_close, and cleared started_at/tested_at/closed_at on a reopening while
// leaving completion_summary behind — three ways of being wrong that nothing
// reported, because only the shipped copy was ever exercised (task #188, after
// the same finding closed sprint deletion in task #176).
//
// The one part that is pure persistence — the INSERT itself — did move here,
// as InsertTaskTx, and `task create` runs it.

// GetSubTasks retrieves all direct subtasks of the given parent task ID.
// Tasks are ordered by priority descending, then created_at ascending.
func (db *DB) GetSubTasks(ctx context.Context, parentID int) ([]models.Task, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
		        t.created_at, t.started_at, t.tested_at, t.closed_at, t.completion_summary,
		        t.commit_open, t.commit_close, t.parent_task_id,
		        t.priority, t.severity,
		        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count`+taskDepsSelect+`
		 FROM tasks t WHERE t.parent_task_id = ?
		 ORDER BY t.priority DESC, t.created_at ASC`,
		parentID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying subtasks: %w", err)
	}
	defer rows.Close()

	return scanTasksWithDeps(rows)
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

	var startedAt, testedAt, closedAt, completionSummary sql.NullString
	var commitOpen, commitClose sql.NullString
	var parentTaskID sql.NullInt64
	var dependsOnCSV, blocksCSV string

	for rows.Next() {
		var task models.Task
		startedAt = sql.NullString{}
		testedAt = sql.NullString{}
		closedAt = sql.NullString{}
		completionSummary = sql.NullString{}
		commitOpen = sql.NullString{}
		commitClose = sql.NullString{}
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
			&startedAt,
			&testedAt,
			&closedAt,
			&completionSummary,
			&commitOpen,
			&commitClose,
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

		// Copy each nullable string into a fresh per-iteration variable before
		// taking its address. Taking the address of the loop-external scan
		// variable would make every task in a multi-row result share the same
		// backing storage and serialize the LAST row's values.
		if startedAt.Valid {
			v := startedAt.String
			task.StartedAt = &v
		}
		if testedAt.Valid {
			v := testedAt.String
			task.TestedAt = &v
		}
		if closedAt.Valid {
			v := closedAt.String
			task.ClosedAt = &v
		}
		if completionSummary.Valid {
			v := completionSummary.String
			task.CompletionSummary = &v
		}
		if commitOpen.Valid {
			v := commitOpen.String
			task.CommitOpen = &v
		}
		if commitClose.Valid {
			v := commitClose.String
			task.CommitClose = &v
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

// GetOpenSprint retrieves the currently open sprint (status = OPEN), with its
// membership resolved. Returns ErrNotFound if no sprint is currently open.
//
// It is the third read of this package that returns a models.Sprint, and it
// answers to the same two rules as the other two: both computed fields are
// populated, and the ids are published in ASCENDING TASK ID, fixed by the
// aggregate's own ORDER BY rather than by the plan that feeds it
// (SPEC/MODELS.md § Sprint Field Constraints). The statement is deliberately the
// same shape as GetSprint's, differing only in its predicate, so the two cannot
// drift apart into two different answers about one sprint.
//
// Only one sprint can be OPEN at a time, which the command layer enforces; the
// LIMIT 1 here is the read's own guard rather than the rule itself.
func (db *DB) GetOpenSprint(ctx context.Context) (*models.Sprint, error) {
	var sprint models.Sprint
	var startedAt sql.NullString
	var closedAt sql.NullString
	var tasksJSON sql.NullString
	var maxTasks sql.NullInt64

	// Single query using JSON aggregation to get sprint data and task IDs, in the
	// order the aggregate states. A sprint with no member task yields '[null]'
	// from the outer join's single NULL row, which parseJSONIntArray reads as the
	// empty set, so the empty case stays [] and never null.
	err := db.QueryRowContext(ctx,
		`SELECT
			s.id, s.status, s.title, s.description, s.created_at, s.started_at, s.closed_at, s.max_tasks, s.order_index,
			COALESCE(json_group_array(DISTINCT st.task_id ORDER BY st.task_id), '[]') as tasks
		 FROM sprints s
		 LEFT JOIN sprint_tasks st ON s.id = st.sprint_id
		 WHERE s.status = ?
		 GROUP BY s.id
		 LIMIT 1`,
		models.SprintOpen,
	).Scan(
		&sprint.ID,
		&sprint.Status,
		&sprint.Title,
		&sprint.Description,
		&sprint.CreatedAt,
		&startedAt,
		&closedAt,
		&maxTasks,
		&sprint.Order,
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
// Tasks are ordered by sprint task position (task_order) alone.
// Only returns tasks with status SPRINT, DOING, or TESTING.
//
// THERE IS NO TIEBREAKER, because there are no ties. The query reads a single
// sprint and position is unique within one sprint (SPEC/DATABASE.md § Position
// Uniqueness Within a Sprint), so ORDER BY st.position ASC already places every
// row at exactly one rank and repeating the call over unchanged data returns the
// same tasks in the same sequence. The t.priority DESC key this ORDER BY used to
// carry could never fire once the order became total, and carrying it implied a
// promotion rule that does not exist: the planned order is the answer to "what
// do I do next", and a task's priority is what the plan was built from, not a
// second chance to override it (SPEC/COMMANDS.md § Get Next Tasks (next)).
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
		         t.acceptance_criteria, t.created_at, t.started_at, t.tested_at,
		         t.closed_at, t.completion_summary,
		         t.commit_open, t.commit_close, t.parent_task_id,
		         t.priority, t.severity,
		         (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count` + taskDepsSelect + `
		      FROM tasks t
		      INNER JOIN sprint_tasks st ON t.id = st.task_id
		      WHERE st.sprint_id = ?
		        AND t.status IN ` + sqlActiveTaskStatuses + `
		      ORDER BY st.position ASC
		      LIMIT ?`

	rows, err := db.QueryContext(ctx, query, sprintID, limit)
	if err != nil {
		return nil, fmt.Errorf("querying next tasks: %w", err)
	}
	defer rows.Close()

	return scanTasksWithDeps(rows)
}

// ==================== TASK DEPENDENCY QUERIES ====================

// auditDependencyPair is one side of the mirrored pair of audit entries a
// dependency change writes: the task whose history the entry belongs to, and
// the other task of the pair, which the entry names.
type auditDependencyPair struct {
	entity  int
	related int
}

// auditDependencyPairs returns the two sides of a dependency change in the
// order they are written, dependent first. Both TASK_ADD_DEP and
// TASK_REMOVE_DEP use the same arrangement (SPEC/COMMANDS.md § Remove Task
// Dependency), so it is spelled out once rather than twice with a transposition
// that has to be read carefully to be believed.
func auditDependencyPairs(taskID, depID int) [2]auditDependencyPair {
	return [2]auditDependencyPair{
		{entity: taskID, related: depID},
		{entity: depID, related: taskID},
	}
}

// AddTaskDependencyWithAudit adds a dependency and writes audit entries in a single transaction.
func (db *DB) AddTaskDependencyWithAudit(ctx context.Context, taskID, depID int) error {
	// Self-dependency check and circular check run before opening the
	// transaction to fail fast.
	if taskID == depID {
		return fmt.Errorf("%w: task cannot depend on itself", utils.ErrValidation)
	}
	wouldCycle, err := db.hasTransitiveDependency(ctx, depID, taskID)
	if err != nil {
		return fmt.Errorf("checking circular dependency: %w", err)
	}
	if wouldCycle {
		return fmt.Errorf("%w: adding dependency would create a circular dependency between task #%d and task #%d",
			utils.ErrValidation, taskID, depID)
	}

	now := utils.NowISO8601()

	return db.WithTransaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO task_dependencies (task_id, depends_on_task_id) VALUES (?, ?)`,
			taskID, depID,
		); err != nil {
			return fmt.Errorf("inserting task dependency: %w", err)
		}

		// Two entries, one against each task of the pair, each naming the other
		// one. Naming the counterpart is what makes an entry state WHICH
		// dependency it concerns: without it the two entries of one invocation
		// are indistinguishable from the two of any other (SPEC/COMMANDS.md §
		// Add Task Dependency).
		for _, pair := range auditDependencyPairs(taskID, depID) {
			if err := LogAuditTx(tx, models.OpTaskAddDep, models.EntityTask, pair.entity, now,
				WithRelatedEntity(pair.related)); err != nil {
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

		// The same mirrored pair the addition writes; see auditDependencyPairs.
		for _, pair := range auditDependencyPairs(taskID, depID) {
			if err := LogAuditTx(tx, models.OpTaskRemoveDep, models.EntityTask, pair.entity, now,
				WithRelatedEntity(pair.related)); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetBlockers returns tasks that are blocking taskID (tasks that taskID depends on and are not COMPLETED).
func (db *DB) GetBlockers(ctx context.Context, taskID int) ([]models.Task, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
		        t.created_at, t.started_at, t.tested_at, t.closed_at, t.completion_summary,
		        t.commit_open, t.commit_close, t.parent_task_id,
		        t.priority, t.severity,
		        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count`+taskDepsSelect+`
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
	return scanTasksWithDeps(rows)
}

// GetBlocking returns tasks that depend on taskID (tasks this task is blocking).
func (db *DB) GetBlocking(ctx context.Context, taskID int) ([]models.Task, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
		        t.created_at, t.started_at, t.tested_at, t.closed_at, t.completion_summary,
		        t.commit_open, t.commit_close, t.parent_task_id,
		        t.priority, t.severity,
		        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count`+taskDepsSelect+`
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
	return scanTasksWithDeps(rows)
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

// NextSprintOrderTx returns the execution order a sprint created without
// --order takes: MAX(order_index)+1, so the first sprint of an empty roadmap
// gets 1.
//
// It takes a transaction rather than a connection on purpose. The read and the
// INSERT that consumes it must be one atomic step, or two concurrent creations
// read the same MAX and both try to write it; the idx_sprints_order unique
// index is only the final backstop, and a collision there surfaces as
// utils.ErrAlreadyExists (exit code 5). See SPEC/DATABASE.md § Create Sprint
// and § Transactional Atomicity Guarantees #6.
func NextSprintOrderTx(tx *sql.Tx) (int, error) {
	var next int
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(order_index), 0) + 1 FROM sprints`,
	).Scan(&next); err != nil {
		return 0, fmt.Errorf("computing next sprint order: %w", err)
	}
	return next, nil
}

// InsertSprintTx inserts one sprint row inside an existing transaction and
// returns its id. sprint.Order must already hold the execution order, whether
// the caller was given one or took it from NextSprintOrderTx.
//
// This is the only implementation of the sprint INSERT, for the reason recorded
// on InsertTaskTx. The error is returned unwrapped so the caller can recognise
// an idx_sprints_order collision with IsUniqueConstraintErr and name the order
// that collided.
func InsertSprintTx(tx *sql.Tx, sprint *models.Sprint) (int, error) {
	result, err := tx.Exec(
		`INSERT INTO sprints (status, title, description, created_at, max_tasks, order_index) VALUES (?, ?, ?, ?, ?, ?)`,
		sprint.Status, sprint.Title, sprint.Description, sprint.CreatedAt, sprint.MaxTasks, sprint.Order,
	)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	return int(id), nil
}

// GetSprint retrieves a sprint by ID, with its membership resolved.
//
// One round trip: the sprint row and its member task ids come back together,
// the ids aggregated into a JSON array, so reading a sprint never costs a
// second statement and never one per member task.
//
// The id order is ASCENDING TASK ID, and it is the aggregate's own ORDER BY
// that fixes it. That is the order SPEC/MODELS.md § Sprint Field Constraints
// requires of the Tasks field, and stating it here is not decoration: without
// it the result would still arrive sorted, but only because DISTINCT dedupes
// through a sorted ephemeral index and because the join happens to walk
// idx_sprint_tasks_lookup in (sprint_id, task_id) order. Both are properties of
// the current query plan, not of the statement, and a specified order may not
// rest on either. The grouped listing read states the same order for the same
// reason (see groupedSprintMembershipQuery).
//
// Ascending id is deliberately NOT the sprint's planned execution order: that
// one is sprint_tasks.position and is published by the sprint task listings
// (SPEC/DATABASE.md § List by Sprint).
func (db *DB) GetSprint(ctx context.Context, id int) (*models.Sprint, error) {
	var sprint models.Sprint
	var startedAt sql.NullString
	var closedAt sql.NullString
	var tasksJSON sql.NullString
	var maxTasks sql.NullInt64

	// Single query using JSON aggregation to get sprint data and task IDs.
	// json_group_array returns a JSON array of task IDs, ordered by the
	// aggregate's own ORDER BY rather than by the plan that feeds it. A sprint
	// with no member task yields '[null]' from the outer join's single NULL row,
	// which parseJSONIntArray reads as the empty set.
	err := db.QueryRowContext(ctx,
		`SELECT
			s.id, s.status, s.title, s.description, s.created_at, s.started_at, s.closed_at, s.max_tasks, s.order_index,
			COALESCE(json_group_array(DISTINCT st.task_id ORDER BY st.task_id), '[]') as tasks
		 FROM sprints s
		 LEFT JOIN sprint_tasks st ON s.id = st.sprint_id
		 WHERE s.id = ?
		 GROUP BY s.id`,
		id,
	).Scan(
		&sprint.ID,
		&sprint.Status,
		&sprint.Title,
		&sprint.Description,
		&sprint.CreatedAt,
		&startedAt,
		&closedAt,
		&maxTasks,
		&sprint.Order,
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

// groupedSprintMembershipQuery returns the grouped membership read of
// SPEC/DATABASE.md § Read the Membership of Many Sprints (Grouped) for an IN
// list of the given placeholders: the member task ids of several sprints at
// once, so a listing resolves every sprint it returns in ONE statement instead
// of one per sprint.
//
// It reads sprint_tasks alone and joins nothing, because membership is a
// sprint_tasks row: the answer is a set of ids per sprint, so no tasks row is
// fetched to produce it. It applies no predicate on task status either, for the
// same reason — status is a tasks column — so a member task in BACKLOG status
// is included and counted (SPEC/STATE_MACHINE.md § Sprint Membership and the
// BACKLOG Status).
//
// The ORDER BY is stated, not inherited from the plan: sprint_id ascending
// groups each sprint's rows together so the result is walkable in one pass, and
// task_id ascending fixes the order the Tasks field publishes
// (SPEC/MODELS.md § Sprint Field Constraints). Neither column is the sprint's
// planned execution order, which is sprint_tasks.position.
//
// idx_sprint_tasks_lookup covers the statement exactly — (sprint_id, task_id),
// the leading column for the IN lookup and the pair for the ordering — so it
// plans as a covering index search with no sort step and no table row read.
//
// It is a function rather than a constant because the IN list has one
// placeholder per id. Assembly is separated from execution so the index tests
// can plan the exact SQL production runs, rather than a lookalike.
func groupedSprintMembershipQuery(placeholders string) string {
	return fmt.Sprintf( // #nosec G201 -- only ? placeholders are interpolated; every id is bound
		`SELECT sprint_id, task_id
	 FROM sprint_tasks
	 WHERE sprint_id IN (%s)
	 ORDER BY sprint_id ASC, task_id ASC`,
		placeholders,
	)
}

// tasksBySprints returns the member task ids of each of the given sprints, keyed
// by sprint id, in ONE statement whatever the number of sprints.
//
// A sprint that holds no task is ABSENT from the map, exactly as in
// GetSprintsByTasks and CountTaskCommentsByTasks: it has no sprint_tasks row, so
// the absence of an entry is the answer, and the caller reads a missing key as
// the empty set. Callers publishing the value MUST turn that nil into an empty
// slice, never a JSON null (SPEC/DATA_FORMATS.md § Implementation Notes, Empty
// arrays); resolveSprintMembership below is where that happens.
//
// An empty id set issues no statement at all and returns an empty map. The id
// set is bound as one placeholder per id in a single statement, so it carries
// the same bind-variable ceiling as the sibling grouped reads and, for the same
// reason, applies no chunking.
func (db *DB) tasksBySprints(ctx context.Context, sprintIDs []int) (map[int][]int, error) {
	membership := make(map[int][]int, len(sprintIDs))
	if len(sprintIDs) == 0 {
		return membership, nil
	}

	args := make([]any, len(sprintIDs))
	for i, id := range sprintIDs {
		args[i] = id
	}

	rows, err := db.QueryContext(ctx, groupedSprintMembershipQuery(db.Placeholders(len(sprintIDs))), args...)
	if err != nil {
		return nil, fmt.Errorf("querying sprint membership: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sprintID, taskID int
		if err := rows.Scan(&sprintID, &taskID); err != nil {
			return nil, fmt.Errorf("scanning sprint membership: %w", err)
		}
		membership[sprintID] = append(membership[sprintID], taskID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sprint membership rows: %w", err)
	}
	return membership, nil
}

// resolveSprintMembership fills in the two computed fields of every sprint of a
// listing — Tasks and TaskCount — from ONE grouped read over the sprint ids the
// listing already holds.
//
// Both fields are populated for every sprint, because SPEC/MODELS.md § Sprint
// requires it of every read that returns a Sprint object: a listing that left
// them at their zero values would report `"tasks": null` and `"task_count": 0`
// for sprints that hold work, and would disagree with GetSprint about the same
// sprint read at the same moment.
//
// TaskCount is the length of the ids just read, never a COUNT(*) of its own, so
// the two fields are two readings of one result and cannot diverge. A sprint
// with no member task gets the empty slice and zero, and the empty slice is
// allocated rather than left nil so it marshals as `[]` and not `null`.
func (db *DB) resolveSprintMembership(ctx context.Context, sprints []models.Sprint) error {
	if len(sprints) == 0 {
		return nil
	}

	ids := make([]int, len(sprints))
	for i := range sprints {
		ids[i] = sprints[i].ID
	}

	membership, err := db.tasksBySprints(ctx, ids)
	if err != nil {
		return err
	}

	for i := range sprints {
		tasks := membership[sprints[i].ID]
		if tasks == nil {
			tasks = []int{}
		}
		sprints[i].Tasks = tasks
		sprints[i].TaskCount = len(tasks)
	}
	return nil
}

// ListSprints retrieves all sprints, optionally narrowed to one status, with the
// membership of every returned sprint resolved.
//
// The result is ordered by Order ascending — the roadmap's planned execution
// order, lowest first (SPEC/COMMANDS.md § List Sprints, Result Ordering). Order
// is unique and NOT NULL across the roadmap, so the sequence is total: every
// sprint sits at exactly one position, and repeating the read over unchanged
// data returns the same sequence. The --status filter narrows WHICH sprints the
// slice contains and never reorders the ones it keeps.
//
// Every Sprint object it returns carries Tasks and TaskCount populated, exactly
// as GetSprint returns them for the same sprint: same ids, same ascending order,
// same count (SPEC/COMMANDS.md § List Sprints). The --status filter selects which
// SPRINTS the array contains and never touches the membership of the sprints it
// keeps.
//
// The cost is TWO statements whatever the number of sprints: one read of
// sprints, then one grouped read of the membership of all of them. No statement
// is issued per sprint and none per returned id; a roadmap with no sprint (or
// none matching the filter) costs one, because the grouped read is skipped
// outright rather than sent with an empty IN list.
func (db *DB) ListSprints(ctx context.Context, status *models.SprintStatus) ([]models.Sprint, error) {
	query := `SELECT id, status, title, description, created_at, started_at, closed_at, max_tasks, order_index FROM sprints WHERE 1=1`
	args := []any{}

	if status != nil {
		query += " AND status = ?"
		args = append(args, string(*status))
	}

	// Sprints come back in the roadmap's PLANNED execution order: order_index
	// ascending, lowest first (SPEC/COMMANDS.md § List Sprints, Result Ordering).
	// order_index is the field a sprint carries for exactly that purpose, and it
	// is the published order of this command, not an incidental property of the
	// query — a caller may rely on it.
	//
	// The ordering is TOTAL, so no tie-break is needed and none is specified:
	// order_index is NOT NULL and unique across the roadmap (idx_sprints_order,
	// SPEC/DATABASE.md § sprints Table), so no two sprints can share a position
	// and none can lack one.
	//
	// The clause sits AFTER the optional status predicate on purpose: --status
	// narrows WHICH sprints the result contains and never reorders the ones it
	// keeps, which are returned in the same relative sequence they hold in the
	// unfiltered listing.
	query += " ORDER BY order_index ASC"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing sprints: %w", err)
	}
	defer rows.Close()

	// Initialize to a non-nil empty slice so an empty result marshals to JSON
	// `[]`, not `null`, per SPEC/DATA_FORMATS.md Implementation Notes #6
	// (finding #53).
	sprints := []models.Sprint{}
	for rows.Next() {
		var sprint models.Sprint
		var startedAt sql.NullString
		var closedAt sql.NullString
		var maxTasks sql.NullInt64

		err := rows.Scan(
			&sprint.ID,
			&sprint.Status,
			&sprint.Title,
			&sprint.Description,
			&sprint.CreatedAt,
			&startedAt,
			&closedAt,
			&maxTasks,
			&sprint.Order,
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

	// The rows are closed before the second statement is issued, so the two reads
	// never hold two connections of the pool at once.
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing sprint rows: %w", err)
	}

	if err := db.resolveSprintMembership(ctx, sprints); err != nil {
		return nil, err
	}

	return sprints, nil
}

// Sprint mutation has no method here on purpose, for the reason recorded above
// InsertTaskTx: the update of a sprint's fields and the transitions of its
// status are inseparable from the rules that admit them — the CLOSED-order
// immutability check, the --order collision mapped to exit code 5, the
// one-open-sprint rule, the active-task check behind --force — and from the
// audit entries they owe. They live in sprintUpdate, sprintStart, sprintClose
// and sprintReopen in internal/commands.
//
// The copy of the field update that used to sit here reached sprints.description
// with no validation at all, below every free-text rule the command layer
// enforces, and no caller: it was a route around those rules waiting for one
// (task #188, finding recorded on it).

// Sprint deletion has no method here on purpose. The whole operation — the
// member tasks' reset to BACKLOG, the removal of the sprint_tasks rows, the
// DELETE of the sprints row and the SPRINT_DELETE audit entry, in one
// transaction (SPEC/DATABASE.md § Transactional Atomicity Guarantees, finding
// #65) — lives in sprintRemove in internal/commands/sprint_crud.go, next to
// every other sprint mutation. A second copy here would be unreachable from
// the binary and therefore ungated: the copy that used to sit at this spot had
// silently missed the finding-#49 fix that clears started_at, tested_at,
// closed_at and completion_summary on the reset, because only the shipped copy
// was ever exercised.

// sprintTasksLookupQuery is the membership lookup idx_sprint_tasks_lookup
// exists for (SPEC/DATABASE.md § Performance Optimization). It is a named
// constant so the index tests can take the query plan of the exact SQL
// production runs, rather than of a lookalike.
const sprintTasksLookupQuery = `SELECT task_id FROM sprint_tasks WHERE sprint_id = ? ORDER BY task_id`

// GetSprintTasks retrieves all tasks in a sprint.
func (db *DB) GetSprintTasks(ctx context.Context, sprintID int) ([]int, error) {
	rows, err := db.QueryContext(ctx,
		sprintTasksLookupQuery,
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

// SprintRef identifies one sprint by the two values that name it without
// ambiguity: its title and its id. The title alone does not identify a sprint —
// SPEC/MODELS.md § Sprint requires it and caps its length but places no
// uniqueness constraint on it, so two sprints of one roadmap may carry the same
// title — while the id is the primary key.
//
// It is the value of the grouped sprint resolution below, not a domain model: it
// carries what a caller needs to name a task's sprint, and deliberately not a
// partially populated models.Sprint, which would look like a whole sprint while
// carrying only two of its fields.
type SprintRef struct {
	Title string
	ID    int
}

// groupedTaskSprintsQuery returns the grouped sprint resolution of
// SPEC/DATABASE.md § Resolve the Sprint of Many Tasks (Grouped) for an IN list of
// the given placeholders. The ORDER BY is task_id ascending, which makes the
// result walkable in one pass against a caller-side set of task ids; no
// tie-breaker is needed, because sprint_tasks.task_id is UNIQUE and the query
// therefore returns at most one row per task.
//
// It is a function rather than a constant because the IN list has one
// placeholder per id. Assembly is separated from execution so the index tests can
// plan the exact SQL production runs, rather than a lookalike.
func groupedTaskSprintsQuery(placeholders string) string {
	return fmt.Sprintf( // #nosec G201 -- only ? placeholders are interpolated; every id is bound
		`SELECT st.task_id, s.id, s.title
	 FROM sprint_tasks st
	 INNER JOIN sprints s ON s.id = st.sprint_id
	 WHERE st.task_id IN (%s)
	 ORDER BY st.task_id ASC`,
		placeholders,
	)
}

// GetSprintsByTasks returns the sprint each of the given tasks belongs to, keyed
// by task id, in ONE statement whatever the number of tasks.
//
// This is the read the web interface MUST use to name the sprint on every card of
// its Kanban board: one card per rendered task means the sprint of every rendered
// task is needed, and resolving them one task — or one board column — at a time
// would reintroduce the N+1 pattern the project has removed elsewhere
// (SPEC/WEB.md § Roadmap Tasks Page, read cost; SPEC/DATABASE.md § Resolve the
// Sprint of Many Tasks (Grouped)).
//
// A task that belongs to no sprint is ABSENT from the map, exactly as in
// CountTaskCommentsByTasks and CountSubTasksByParents: it has no sprint_tasks row,
// so the absence of an entry is the answer, and the zero value a caller reads for
// a missing key is the zero SprintRef. At most one entry exists per task, which
// the schema guarantees rather than the query: sprint_tasks.task_id carries a
// UNIQUE constraint (SPEC/DATABASE.md § sprint_tasks Table). The inner join drops
// nothing, because sprint_id is NOT NULL and carries a foreign key to sprints(id).
//
// An empty id set issues no statement at all and returns an empty map. Duplicate
// ids in the input are harmless; each row is returned once. The id set is bound as
// one placeholder per id in a single statement, so it carries the same
// bind-variable ceiling as the sibling grouped reads named above, and for the same
// reason applies no chunking.
func (db *DB) GetSprintsByTasks(ctx context.Context, taskIDs []int) (map[int]SprintRef, error) {
	sprints := make(map[int]SprintRef, len(taskIDs))
	if len(taskIDs) == 0 {
		return sprints, nil
	}

	args := make([]any, len(taskIDs))
	for i, id := range taskIDs {
		args[i] = id
	}

	rows, err := db.QueryContext(ctx, groupedTaskSprintsQuery(db.Placeholders(len(taskIDs))), args...)
	if err != nil {
		return nil, fmt.Errorf("querying sprints by task: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var taskID int
		var sprint SprintRef
		if err := rows.Scan(&taskID, &sprint.ID, &sprint.Title); err != nil {
			return nil, fmt.Errorf("scanning task sprint: %w", err)
		}
		sprints[taskID] = sprint
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task sprint rows: %w", err)
	}
	return sprints, nil
}

// GetActiveSprintTasks retrieves tasks in a sprint with status SPRINT, DOING, or TESTING.
// SPRINT tasks were assigned but never started; DOING/TESTING tasks are actively in progress.
// Used to validate sprint close safety.
func (db *DB) GetActiveSprintTasks(ctx context.Context, sprintID int) ([]models.Task, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements,
		         t.acceptance_criteria, t.created_at, t.started_at, t.tested_at,
		         t.closed_at, t.completion_summary,
		         t.commit_open, t.commit_close, t.parent_task_id,
		         t.priority, t.severity,
		         (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count`+taskDepsSelect+`
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

	return scanTasksWithDeps(rows)
}

// sprintMember is one row of sprint_tasks as the ordering routines read it: a
// member task and the position it currently holds.
type sprintMember struct {
	taskID   int
	position int
}

// sprintMembersInOrderTx reads a sprint's members in the sprint's planned order,
// each with the position it currently holds, inside an existing transaction.
//
// position is unique within one sprint (SPEC/DATABASE.md § Position Uniqueness
// Within a Sprint), so position ASC alone already places every member at exactly
// one rank; task_id ASC is kept as a second key so the read is total by its own
// terms rather than by relying on the index for it, which is what makes every
// routine built on this read deterministic on its face.
//
// The rows are fully consumed and closed before the function returns, so a
// caller may issue writes on the same transaction with no open cursor over the
// table it is writing.
func sprintMembersInOrderTx(tx *sql.Tx, sprintID int) ([]sprintMember, error) {
	rows, err := tx.Query(
		"SELECT task_id, position FROM sprint_tasks WHERE sprint_id = ? ORDER BY position ASC, task_id ASC",
		sprintID,
	)
	if err != nil {
		return nil, fmt.Errorf("reading sprint positions: %w", err)
	}
	defer rows.Close() // #nosec G104 -- rows are fully consumed; close error is not actionable

	var members []sprintMember
	for rows.Next() {
		var m sprintMember
		if err := rows.Scan(&m.taskID, &m.position); err != nil {
			return nil, fmt.Errorf("scanning sprint position: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sprint positions: %w", err)
	}
	return members, nil
}

// parkSprintPositionsTx moves every member of one sprint into the negative
// range, out of the range the assignment that follows writes into.
//
// SQLite checks a unique index per row as each row is written and has no
// deferred constraint check, so a statement sequence with a legal final state
// can still fail partway through if an intermediate state holds two equal
// positions in one sprint. Parking is what removes that possibility for any
// operation that PERMUTES existing positions: -1 - position maps distinct
// non-negative values to distinct negative ones, so the parked state satisfies
// the unique index as well, and every value the assignment then writes is
// non-negative and therefore held by nobody. Parked values never escape the
// transaction, so no reader observes one (SPEC/DATABASE.md § Position Uniqueness
// Within a Sprint, "Every write path must reach its result without a transient
// collision").
func parkSprintPositionsTx(tx *sql.Tx, sprintID int) error {
	if _, err := tx.Exec(
		"UPDATE sprint_tasks SET position = -1 - position WHERE sprint_id = ?",
		sprintID,
	); err != nil {
		return fmt.Errorf("parking sprint positions: %w", err)
	}
	return nil
}

// assignSprintPositionsTx writes the dense 0..N-1 run that puts the sprint's
// members in the sequence ordered gives, one row per member.
//
// It assumes the values it writes are free, which a caller establishes either by
// parking first (parkSprintPositionsTx) or by renumbering downwards over an
// ascending read (see CompactSprintPositionsTx).
func assignSprintPositionsTx(tx *sql.Tx, sprintID int, ordered []int) error {
	for i, taskID := range ordered {
		if _, err := tx.Exec(
			"UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?",
			i, sprintID, taskID,
		); err != nil {
			return fmt.Errorf("updating position for task %d: %w", taskID, err)
		}
	}
	return nil
}

// CompactSprintPositionsTx renumbers a sprint's task positions to a contiguous
// 0..N-1 sequence (preserving the current order), eliminating gaps left by a
// removal, so any operation that deletes sprint_tasks rows compacts afterwards.
// Runs inside an existing transaction.
//
// MoveTaskToPosition no longer needs density for its OWN correctness -- it writes
// a permutation rather than shifting a range -- but the CALLER-FACING meaning of
// a position value still rests on it: `sprint bottom` derives its target from the
// member count, and MoveTaskToPosition's no-op guard compares the moved task's
// STORED position against the TARGET RANK, so the two mean the same thing only
// while the run is dense. That assumption is not specified anywhere and remains
// unspecified: it is recorded as rmp task #304 and is deliberately NOT settled
// here, which closes only the uniqueness of position, not its density.
//
// This routine needs NO parking step, and the reason is no longer that position
// is unconstrained: since schema 1.13.0 the pair (sprint_id, position) is unique
// (SPEC/DATABASE.md § Position Uniqueness Within a Sprint). It is safe because it
// renumbers DOWNWARDS over an ascending read. The members are read in ascending
// position order and the i-th of them is assigned i, which is never greater than
// the position that row already holds: the i rows ranked before it hold i
// distinct smaller non-negative positions, so its own position is at least i.
// Every row not yet written therefore still holds a position strictly greater
// than the current row's — hence strictly greater than i — and every row already
// written holds a value strictly below i, so no write can land on a value another
// row of the same sprint still holds (SPEC/DATABASE.md § Position Uniqueness
// Within a Sprint, first alternative of "Every write path must reach its result
// without a transient collision").
func CompactSprintPositionsTx(tx *sql.Tx, sprintID int) error {
	members, err := sprintMembersInOrderTx(tx, sprintID)
	if err != nil {
		return err
	}

	ordered := make([]int, len(members))
	for i, m := range members {
		ordered[i] = m.taskID
	}
	return assignSprintPositionsTx(tx, sprintID, ordered)
}

// GetSprintTasksFull retrieves full task objects for a sprint, ordered by position or priority.
func (db *DB) GetSprintTasksFull(ctx context.Context, sprintID int, status *models.TaskStatus, orderByPriority bool) ([]models.Task, error) {
	query := `SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements,
		         t.acceptance_criteria, t.created_at, t.started_at, t.tested_at,
		         t.closed_at, t.completion_summary,
		         t.commit_open, t.commit_close, t.parent_task_id,
		         t.priority, t.severity,
		         (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count` + taskDepsSelect + `
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

	return scanTasksWithDeps(rows)
}

// GetOpenSprintTasks retrieves tasks in a sprint that are not yet completed.
// Returns tasks with status SPRINT, DOING, or TESTING, ordered by sprint position.
// When orderByPriority is true, tasks are ordered by priority DESC then position ASC.
// Returns an empty slice (not an error) when the sprint has no open tasks.
func (db *DB) GetOpenSprintTasks(ctx context.Context, sprintID int, orderByPriority bool) ([]models.Task, error) {
	query := `SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements,
		         t.acceptance_criteria, t.created_at, t.started_at, t.tested_at,
		         t.closed_at, t.completion_summary,
		         t.commit_open, t.commit_close, t.parent_task_id,
		         t.priority, t.severity,
		         (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count` + taskDepsSelect + `
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

	return scanTasksWithDeps(rows)
}

// AddTasksToSprint adds tasks to a sprint with automatic position assignment.
// Tasks are added at the end of the sprint task list (highest position + 1).
func (db *DB) AddTasksToSprint(ctx context.Context, sprintID int, taskIDs []int) error {
	if len(taskIDs) == 0 {
		return nil
	}

	return db.WithTransaction(func(tx *sql.Tx) error {
		now := utils.NowISO8601()

		// Authoritative capacity enforcement (finding #67). When max_tasks is
		// set, the current active-member count and this batch must not exceed
		// the cap. Performing the check INSIDE the insert transaction closes the
		// TOCTOU window that exists when the CLI checks capacity in a separate
		// read transaction: two concurrent `sprint add-tasks` could each pass a
		// standalone pre-check and both insert, overflowing the cap. The single
		// SQLite writer serializes these transactions, so the committed member
		// count can never exceed max_tasks (SPEC/DATABASE.md § Transactional
		// Atomicity Guarantees #3). The CLI keeps a friendly pre-check for fast
		// feedback, but this transaction is the source of truth.
		var maxTasks sql.NullInt64
		if err := tx.QueryRow(
			"SELECT max_tasks FROM sprints WHERE id = ?",
			sprintID,
		).Scan(&maxTasks); err != nil {
			return fmt.Errorf("querying sprint capacity: %w", err)
		}
		if maxTasks.Valid {
			var activeCount int
			if err := tx.QueryRow(
				`SELECT COUNT(*) FROM sprint_tasks st
				   INNER JOIN tasks t ON t.id = st.task_id
				 WHERE st.sprint_id = ? AND t.status IN `+sqlActiveTaskStatuses,
				sprintID,
			).Scan(&activeCount); err != nil {
				return fmt.Errorf("counting active sprint tasks: %w", err)
			}
			if activeCount+len(taskIDs) > int(maxTasks.Int64) {
				// Preserve the exact CLI error contract (utils.ErrValidation ->
				// exit 6) and message format so callers see identical behavior
				// whether the friendly pre-check or this authoritative check
				// trips first.
				return fmt.Errorf("%w: adding %d task(s) would exceed sprint #%d capacity (%d/%d tasks active)",
					utils.ErrValidation, len(taskIDs), sprintID, activeCount, maxTasks.Int64)
			}
		}

		// Get current max position for this sprint within the transaction
		var maxPos sql.NullInt64
		err := tx.QueryRow(
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

		// Update task status to SPRINT using the cached template
		// (OpAddTasksToSprint) and batch chunking so large id sets stay within
		// SQLite's variable limit. status is a bound parameter, so the leading
		// arg is models.StatusSprint followed by the chunk ids.
		if err := db.batchProc.ProcessChunks(taskIDs, func(chunk []int) error {
			statusQuery := db.queryCache.GetQuery(OpAddTasksToSprint, len(chunk))
			args := make([]any, 0, len(chunk)+1)
			args = append(args, models.StatusSprint)
			for _, id := range chunk {
				args = append(args, id)
			}
			if _, err := tx.Exec(statusQuery, args...); err != nil {
				return fmt.Errorf("updating task statuses: %w", err)
			}
			return nil
		}); err != nil {
			return err
		}

		// Audit: two entries per task, one against each entity the addition
		// changes, written INSIDE this same transaction so a committed
		// membership change can never exist without its audit record. Writing
		// the audit in a separate post-commit call would leave a window where
		// the insert is durable but the audit is not (SPEC/DATABASE.md §
		// Transactional Atomicity Guarantees #4; ARCHITECTURE.md § Security
		// Guarantees).
		//
		// The pair is mirrored: SPRINT_ADD_TASK belongs to the sprint's history
		// and names the task, TASK_STATUS_SPRINT belongs to the task's history
		// and names the sprint, and both carry the one `now` this transaction
		// captured. Transposed ids and a shared performed_at are what let a
		// reader of either entity's history learn the counterpart without
		// consulting the other's (SPEC/DATABASE.md § The Two Entities of a
		// Relational Operation).
		for _, taskID := range taskIDs {
			if err := LogAuditTx(tx, models.OpSprintAddTask, models.EntitySprint, sprintID, now,
				WithRelatedEntity(taskID)); err != nil {
				return err
			}
			if err := LogAuditTx(tx, models.OpTaskStatusSprint, models.EntityTask, taskID, now,
				WithRelatedEntity(sprintID)); err != nil {
				return err
			}
		}
		return nil
	})
}

// MoveTasksBetweenSprints relocates the membership of taskIDs from fromID to
// toID atomically, preserving each task's status.
//
// Unlike AddTasksToSprint (used by `sprint add-tasks`), this method DOES NOT
// modify tasks.status: a task that is DOING or TESTING in the source sprint
// keeps that status in the destination sprint. Per SPEC/COMMANDS.md, moving a
// task between sprints is a re-parenting of work, not a re-admission to the
// sprint backlog, so the lifecycle state must be carried over unchanged.
//
// Validation (SPEC/COMMANDS.md validation step 5): every task in taskIDs must
// currently be a member of fromID (a row in sprint_tasks with sprint_id =
// fromID). If any task is not a member of the source sprint, no rows are moved
// and the call returns ErrTasksNotInSprint wrapped with utils.ErrValidation so
// the CLI maps it to exit code 6 ("task not in sprint"), matching the
// task-ordering error contract. The membership check and the re-parenting run
// in the same transaction, so the move is all-or-nothing.
//
// Re-parenting (mirrors AddTasksToSprint's position/added_at conventions):
//   - sprint_id is set to toID
//   - added_at is refreshed to now
//   - position values are appended after the current max position in toID,
//     preserving the relative order of the moved tasks (taskIDs order)
//
// No capacity (max_tasks) check is applied: relocating existing work must not
// be blocked by the destination sprint's cap (SPEC requires the cap only for
// `add-tasks`).
func (db *DB) MoveTasksBetweenSprints(ctx context.Context, fromID, toID int, taskIDs []int) error {
	if len(taskIDs) == 0 {
		return nil
	}

	return db.WithTransaction(func(tx *sql.Tx) error {
		// Verify every task is currently a member of the source sprint.
		// Count matching membership rows and compare against the requested
		// count; this mirrors ReorderSprintTasks's membership guard and
		// fails the whole move if any task is absent.
		memberArgs := make([]any, 0, len(taskIDs)+1)
		memberArgs = append(memberArgs, fromID)
		for _, id := range taskIDs {
			memberArgs = append(memberArgs, id)
		}
		countQuery := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
			"SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ? AND task_id IN (%s)",
			db.queryCache.GetPlaceholders(len(taskIDs)),
		)
		var count int
		if err := tx.QueryRow(countQuery, memberArgs...).Scan(&count); err != nil {
			return fmt.Errorf("verifying task membership: %w", err)
		}
		if count != len(taskIDs) {
			// Wrap with utils.ErrValidation so the CLI maps this to exit 6
			// (SPEC/COMMANDS.md: "Task ID not in sprint" -> exit 6).
			return fmt.Errorf("%w: %w: one or more tasks are not in sprint #%d",
				utils.ErrValidation, ErrTasksNotInSprint, fromID)
		}

		// Re-parent the membership rows, appending after the destination's
		// current max position to preserve order. added_at is refreshed.
		now := utils.NowISO8601()
		var maxPos sql.NullInt64
		if err := tx.QueryRow(
			"SELECT MAX(position) FROM sprint_tasks WHERE sprint_id = ?",
			toID,
		).Scan(&maxPos); err != nil {
			return fmt.Errorf("querying max position: %w", err)
		}
		startPos := -1
		if maxPos.Valid {
			startPos = int(maxPos.Int64)
		}

		for i, taskID := range taskIDs {
			if _, err := tx.Exec(
				`UPDATE sprint_tasks SET sprint_id = ?, added_at = ?, position = ?
				 WHERE task_id = ? AND sprint_id = ?`,
				toID, now, startPos+i+1, taskID, fromID,
			); err != nil {
				return fmt.Errorf("moving task %d: %w", taskID, err)
			}
		}

		// Intentionally NOT updating tasks.status: the task keeps whatever
		// status it had (BACKLOG/SPRINT/DOING/TESTING/COMPLETED).

		// Audit: two entries per task, one against each sprint the move
		// changes, written INSIDE this same transaction as the re-parenting so
		// a committed move can never exist without its audit record
		// (SPEC/DATABASE.md § Transactional Atomicity Guarantees #5;
		// ARCHITECTURE.md § Security Guarantees).
		//
		// Both name the task, and which sprint a row belongs to is what the
		// direction in its operation states. The single SPRINT_MOVE_TASK entry
		// these replace was written against the destination alone, so the
		// source sprint's history had no record of losing the task and no entry
		// named the task at all; SPRINT_MOVE_TASK is now LEGACY and no path
		// writes it (SPEC/DATABASE.md § One Row per Thing That Happened,
		// rule 2).
		//
		// No TASK_STATUS_* entry accompanies them: the move preserves each
		// task's status, so nothing happened to the task's own lifecycle for a
		// status entry to record (SPEC/COMMANDS.md § Task Assignment).
		for _, taskID := range taskIDs {
			if err := LogAuditTx(tx, models.OpSprintMoveTaskOut, models.EntitySprint, fromID, now,
				WithRelatedEntity(taskID)); err != nil {
				return err
			}
			if err := LogAuditTx(tx, models.OpSprintMoveTaskIn, models.EntitySprint, toID, now,
				WithRelatedEntity(taskID)); err != nil {
				return err
			}
		}
		return nil
	})
}

// Removing tasks from a sprint has no method here on purpose, for the reason
// recorded above InsertTaskTx. It is sprintRemoveTasks in internal/commands,
// which deletes the sprint_tasks row scoped to the named sprint, resets the
// task with every lifecycle field it may have acquired, compacts the remaining
// positions, and writes the two audit entries the removal owes
// (SPEC/DATABASE.md § Transactional Atomicity Guarantees #2).
//
// The copy that used to sit here had drifted from all four: it deleted by
// task_id alone, so it yanked a task out of whatever sprint it was actually in
// — the corruption finding #40 fixed in the shipped path — and it set status
// to BACKLOG without clearing started_at, tested_at, closed_at,
// completion_summary or commit_close, which is finding #49. Nothing reported
// either, because no command reached it (task #188).

// ==================== AUDIT QUERIES ====================

// ErrAuditCommitHashNotAllowed is returned when a caller attaches a commit hash
// to an operation that does not carry one.
var ErrAuditCommitHashNotAllowed = errors.New("audit operation does not carry a commit hash")

// ErrAuditRelatedEntityNotAllowed is returned when a caller names a counterpart
// entity on an operation that has no counterpart.
var ErrAuditRelatedEntityNotAllowed = errors.New("audit operation does not carry a related entity")

// auditOptionalColumns holds the two nullable columns of an audit row, both NULL
// unless a caller sets them.
type auditOptionalColumns struct {
	relatedEntityID *int
	commitHash      *string
}

// AuditOption sets one of the two optional columns of the audit row LogAuditTx
// writes.
//
// The variadic-option form is deliberate, and the alternatives were weighed.
// Both columns are NULL on the great majority of the catalogue — 33 of the 43
// operations carry neither — so the ~20 existing call sites must keep reading
// as they do, naming only what they actually record. An options struct would
// have made every one of them spell out a literal with two zero fields, and a
// second constructor per column would have needed a third for the sites that
// one day set both. A trailing option list leaves the common call untouched,
// costs no allocation when none is passed, and takes a fourth column the same
// way it took these two.
type AuditOption func(*auditOptionalColumns)

// WithCommitHash records the git commit bracketing a task's development work.
// The value is the already-normalised, lowercase hash the transition also
// writes to tasks.commit_open or tasks.commit_close — the audit row copies what
// the transition was given rather than reading it back from the task
// (SPEC/DATABASE.md § The Commit Hash of an Audit Entry).
//
// Only TASK_STATUS_DOING and TASK_STATUS_COMPLETED accept it; see LogAuditTx.
func WithCommitHash(hash string) AuditOption {
	return func(row *auditOptionalColumns) { row.commitHash = &hash }
}

// WithRelatedEntity names the counterpart entity of the operation that produced
// the row: the task a sprint row is about, or the sprint a task row is about,
// or the other task of a dependency pair (SPEC/DATABASE.md § The Two Entities of
// a Relational Operation).
//
// It is what makes two rows of the same operation distinguishable. Without it
// every SPRINT_ADD_TASK row of a sprint reads identically and none of them says
// which task was added.
//
// Only the eight operations of that section accept it; see LogAuditTx. Passing
// it is a decision of the call site even among those eight, because the same
// operation can be written by a command that has a counterpart and by one that
// has none: `sprint remove-tasks` names the sprint on its TASK_STATUS_BACKLOG
// row and `task stat <ids> BACKLOG` names nothing on its own.
func WithRelatedEntity(id int) AuditOption {
	return func(row *auditOptionalColumns) { row.relatedEntityID = &id }
}

// LogAuditTx inserts an audit row inside an existing transaction. It is the
// only audit writer in the package, and every transactional site that writes
// an audit row alongside a domain mutation calls it rather than spelling out
// the INSERT: that keeps the table layout in one place and lets writers stay
// terse.
//
// It takes a *sql.Tx and not a *DB on purpose. SPEC/ARCHITECTURE.md § Security
// Guarantees requires the audit entry for a modification to be written in the
// same transaction as the modification itself, so an audit insert that opened
// its own connection could commit a record for a change that later rolled
// back. A convenience wrapper that inserted one row outside a transaction used
// to live here, reachable only from test fixtures; it is gone, and the
// fixtures now seed through this function, which is the path production runs.
//
// Being the only writer is also what makes both column rules enforceable rather
// than merely stated. SPEC/DATABASE.md allows commit_hash on exactly two
// operations and related_entity_id on exactly eight, and forbids each column
// everywhere else; both table-wide invariants are checked here, once, instead of
// being left to the discipline of each call site.
func LogAuditTx(tx *sql.Tx, op models.AuditOperation, entityType models.EntityType, entityID int, performedAt string, opts ...AuditOption) error {
	var row auditOptionalColumns
	for _, opt := range opts {
		opt(&row)
	}

	if row.commitHash != nil && !models.OperationCarriesCommitHash(op) {
		return fmt.Errorf("%w: %s", ErrAuditCommitHashNotAllowed, op)
	}
	if row.relatedEntityID != nil && !models.OperationCarriesRelatedEntity(op) {
		return fmt.Errorf("%w: %s", ErrAuditRelatedEntityNotAllowed, op)
	}

	// The nullable columns are bound as *int and *string: database/sql converts
	// a nil pointer to SQL NULL, which is what every operation that carries
	// neither must store.
	_, err := tx.Exec(
		`INSERT INTO audit (operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		op, entityType, entityID, row.relatedEntityID, row.commitHash, performedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting audit entry: %w", err)
	}
	return nil
}

// LogAuditFieldsTx writes one audit row per field a single invocation supplied,
// all of them against the same entity and all of them carrying the same
// performed_at. It is the writer for the two commands that edit fields,
// `task edit` and `sprint update`, whose audit contract is one row per supplied
// flag rather than one row per invocation (SPEC/COMMANDS.md § Edit Task and
// § Update Sprint).
//
// ops is the operations of the supplied fields, in the order the caller built
// its UPDATE statement, so the stored rows read in the same order the command
// applied the columns. A caller that supplied no field passes none and no row
// is written, which is the no-op `task edit` documents.
//
// The single performedAt parameter is the point of the function. "All entries of
// one invocation share one performed_at" is what makes them recognisable as one
// event, and a per-row timestamp is the natural way to get that wrong: with
// millisecond resolution and a handful of rows, a re-stamped write is
// indistinguishable from a correct one almost every time it runs, so the defect
// would survive testing. Taking the timestamp as a parameter makes the property
// structural — the loop has no clock to call — instead of leaving it to be
// asserted after the fact.
func LogAuditFieldsTx(tx *sql.Tx, entityType models.EntityType, entityID int, performedAt string, ops ...models.AuditOperation) error {
	for _, op := range ops {
		if err := LogAuditTx(tx, op, entityType, entityID, performedAt); err != nil {
			return err
		}
	}
	return nil
}

// AuditFilter bundles every optional knob for GetAuditEntries. A nil
// pointer in any of the *Field positions means "no filter on this field".
// Limit == 0 means "no limit"; Offset == 0 means "start from the top".
type AuditFilter struct {
	Operation  *string
	EntityType *string
	EntityID   *int
	Since      *string
	Until      *string
	Limit      int
	Offset     int
}

// GetAuditEntries retrieves audit entries matching the supplied filter,
// ordered by performed_at DESC.
//
// Returns an empty slice (no error) when no rows match.
//
// Example:
//
//	op := "TASK_CREATE"
//	entries, err := db.GetAuditEntries(ctx, &db.AuditFilter{
//	    Operation: &op,
//	    Limit:     100,
//	})
func (db *DB) GetAuditEntries(ctx context.Context, f *AuditFilter) ([]models.AuditEntry, error) {
	if f == nil {
		f = &AuditFilter{}
	}
	// Defense-in-depth server-side hard cap (finding #64). The CLI already
	// rejects out-of-range --limit values (SPEC/DATABASE.md § Audit Result
	// Limit), but a programmatic caller could pass 0 (unbounded) or a value
	// above MaxAuditLimit. Clamp here so the query is never issued with an
	// unbounded or larger-than-MaxAuditLimit LIMIT, mirroring ListTasks.
	if f.Limit <= 0 || f.Limit > models.MaxAuditLimit {
		f.Limit = models.MaxAuditLimit
	}

	query, args := buildAuditEntriesQuery(f)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying audit entries: %w", err)
	}
	defer rows.Close()

	return scanAuditEntries(rows)
}

// buildAuditEntriesQuery assembles the SQL and bind arguments GetAuditEntries
// executes. Assembly is separated from execution so that the index tests can
// take the query plan of the exact SQL production runs, rather than of a
// lookalike (SPEC/DATABASE.md § Performance Optimization).
//
// The caller is responsible for clamping f.Limit beforehand.
func buildAuditEntriesQuery(f *AuditFilter) (string, []any) {
	// Build the query with strings.Builder so we don't allocate a new
	// backing string for every appended clause.
	var qb strings.Builder
	qb.Grow(256) // rough upper bound for SELECT + 7 clauses
	// Columns are named explicitly and in the DDL's own order. A migrated audit
	// table carries related_entity_id and commit_hash appended after
	// performed_at, while a fresh one declares them before it, so no statement
	// here may use SELECT * or bind by position (SPEC/VERSION.md § Migration
	// 1.11.0 to 1.12.0).
	qb.WriteString(`SELECT id, operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at FROM audit WHERE 1=1`)
	args := make([]any, 0, 7)

	if f.Operation != nil {
		qb.WriteString(" AND operation = ?")
		args = append(args, *f.Operation)
	}
	if f.EntityType != nil {
		qb.WriteString(" AND entity_type = ?")
		args = append(args, *f.EntityType)
	}
	if f.EntityID != nil {
		qb.WriteString(" AND entity_id = ?")
		args = append(args, *f.EntityID)
	}
	if f.Since != nil {
		qb.WriteString(" AND performed_at >= ?")
		args = append(args, *f.Since)
	}
	if f.Until != nil {
		qb.WriteString(" AND performed_at <= ?")
		args = append(args, *f.Until)
	}

	qb.WriteString(" ORDER BY performed_at DESC")
	// f.Limit is always > 0 here (clamped above), so the LIMIT clause is
	// always present and bounded by MaxAuditLimit.
	qb.WriteString(" LIMIT ?")
	args = append(args, f.Limit)
	if f.Offset > 0 {
		qb.WriteString(" OFFSET ?")
		args = append(args, f.Offset)
	}

	return qb.String(), args
}

// scanAuditEntries materialises the rows GetAuditEntries selected.
func scanAuditEntries(rows *sql.Rows) ([]models.AuditEntry, error) {
	// Initialize to a non-nil empty slice so an empty result marshals to JSON
	// `[]`, not `null`, per SPEC/DATA_FORMATS.md Implementation Notes #6
	// (finding #53).
	entries := []models.AuditEntry{}

	var relatedEntityID sql.NullInt64
	var commitHash sql.NullString

	for rows.Next() {
		var entry models.AuditEntry
		relatedEntityID = sql.NullInt64{}
		commitHash = sql.NullString{}

		err := rows.Scan(
			&entry.ID,
			&entry.Operation,
			&entry.EntityType,
			&entry.EntityID,
			&relatedEntityID,
			&commitHash,
			&entry.PerformedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning audit entry: %w", err)
		}

		// Each value is copied into a fresh variable before its address is
		// taken. Pointing every entry at the loop-external scan variable would
		// make the whole result share one backing value and report the LAST
		// row's counterpart and hash on all of them.
		if relatedEntityID.Valid {
			v := int(relatedEntityID.Int64)
			entry.RelatedEntityID = &v
		}
		if commitHash.Valid {
			v := commitHash.String
			entry.CommitHash = &v
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating audit entries: %w", err)
	}

	return entries, nil
}

// CountAuditEntries returns the total number of rows in the audit table.
//
// It is a lightweight COUNT(*) used by paginating callers (for example the
// read-only web audit log page) to compute the total page count without
// reading any row. It is read-only and never writes an audit entry.
//
// Returns 0 (no error) when the audit table is empty.
func (db *DB) CountAuditEntries(ctx context.Context) (int, error) {
	var total int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit`).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("counting audit entries: %w", err)
	}
	return total, nil
}

// GetEntityHistory retrieves audit history for a specific entity.
func (db *DB) GetEntityHistory(ctx context.Context, entityType string, entityID int) ([]models.AuditEntry, error) {
	return db.GetAuditEntries(ctx, &AuditFilter{
		EntityType: &entityType,
		EntityID:   &entityID,
	})
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
			if stats.FirstEntryAt == nil || minAt.String < *stats.FirstEntryAt {
				v := minAt.String
				stats.FirstEntryAt = &v
			}
		}
		if maxAt.Valid {
			if stats.LastEntryAt == nil || maxAt.String > *stats.LastEntryAt {
				v := maxAt.String
				stats.LastEntryAt = &v
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating audit stats rows: %w", err)
	}
	return stats, nil
}

// ==================== SPRINT TASK ORDERING QUERIES ====================

// ReorderSprintTasks sets the exact order of tasks in a sprint.
// All task IDs must belong to the sprint, and the list must be complete.
// Positions are assigned sequentially starting from 0.
//
// THE THREE VALIDATIONS RUN INSIDE THE WRITE TRANSACTION, and that is the point
// of them being here at all: the CLI performs the same three checks first for a
// friendly error message, but it performs them in a SEPARATE read. Checking
// completeness in an earlier, separate read leaves a window in which another
// process adds a task to the sprint — the list is then complete when it is read
// and partial when it is applied, and a partial assignment leaves the omitted
// tasks holding positions this reorder also assigns. That race was reproduced,
// not hypothesised. Moving the checks inside follows the precedent of the
// max_tasks capacity check in AddTasksToSprint: the single SQLite writer
// serialises these transactions, so a committed reorder can only ever be a
// permutation of the sprint's membership as it stood when the reorder ran.
//
// Together the three establish exactly that: no duplicate in the list, one list
// entry per member of the sprint, and every entry a member.
func (db *DB) ReorderSprintTasks(sprintID int, taskIDs []int) error {
	if len(taskIDs) == 0 {
		return nil
	}

	return db.WithTransaction(func(tx *sql.Tx) error {
		// 1. No duplicate task ID. Costs no I/O and is what makes the two counts
		// below add up to a permutation rather than merely to a total.
		seen := make(map[int]struct{}, len(taskIDs))
		for _, id := range taskIDs {
			if _, dup := seen[id]; dup {
				return fmt.Errorf("%w: duplicate task ID %d", utils.ErrValidation, id)
			}
			seen[id] = struct{}{}
		}

		// 2. The list names EVERY member of the sprint. A partial reorder is not
		// supported (SPEC/COMMANDS.md § Reorder Tasks (Set Exact Order)).
		var memberCount int
		if err := tx.QueryRow(
			"SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ?",
			sprintID,
		).Scan(&memberCount); err != nil {
			return fmt.Errorf("counting sprint members: %w", err)
		}
		if memberCount != len(taskIDs) {
			return fmt.Errorf("%w: expected %d task IDs, got %d (must include all sprint tasks)",
				utils.ErrValidation, memberCount, len(taskIDs))
		}

		// 3. Every task ID belongs to this sprint. Wrapped with utils.ErrValidation
		// so a list invalidated by a concurrent removal maps to exit 6, the code
		// SPEC/COMMANDS.md § Reorder Tasks gives "Task ID not in sprint", and
		// matching the identical guard in MoveTasksBetweenSprints.
		args := make([]any, 0, len(taskIDs)+1)
		args = append(args, sprintID)
		for _, id := range taskIDs {
			args = append(args, id)
		}

		countQuery := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
			"SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ? AND task_id IN (%s)",
			db.queryCache.GetPlaceholders(len(taskIDs)),
		)
		var count int
		if err := tx.QueryRow(countQuery, args...).Scan(&count); err != nil {
			return fmt.Errorf("verifying task membership: %w", err)
		}
		if count != len(taskIDs) {
			return fmt.Errorf("%w: %w: sprint %d", utils.ErrValidation, ErrTasksNotInSprint, sprintID)
		}

		// A reorder is a permutation, so assigning the final positions directly
		// makes the first task of the new order claim a position another task
		// still holds and the unique index rejects the statement. Park first.
		if err := parkSprintPositionsTx(tx, sprintID); err != nil {
			return err
		}
		if err := assignSprintPositionsTx(tx, sprintID, taskIDs); err != nil {
			return err
		}

		// Log audit entry
		now := utils.NowISO8601()
		if err := LogAuditTx(tx, models.OpSprintReorderTasks, models.EntitySprint, sprintID, now); err != nil {
			return fmt.Errorf("logging audit entry: %w", err)
		}
		return nil
	})
}

// MoveTaskToPosition moves a single task to a specific position within a sprint,
// shifting other tasks to maintain continuous positions (0, 1, 2...).
// If position >= task count, the task is moved to the end.
//
// A RANGE SHIFT CANNOT EXPRESS THIS MOVE. The shift form —
// UPDATE ... SET position = position + 1 WHERE position >= ? AND position < ? —
// walks a contiguous run of rows and moves each onto the value its neighbour
// still holds, so the unique index over (sprint_id, position) rejects it on the
// first row, and it does so in BOTH directions. This routine therefore lifts the
// moved task out of the sprint's current order, re-inserts it at the target
// slot, parks the whole sprint, and writes the resulting permutation: the same
// final state, reached without ever presenting a duplicate (SPEC/DATABASE.md
// § Move Task to Position).
func (db *DB) MoveTaskToPosition(sprintID, taskID, newPosition int) error {
	return db.WithTransaction(func(tx *sql.Tx) error {
		// Get current position of the task
		var currentPos int
		err := tx.QueryRow(
			"SELECT position FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?",
			sprintID, taskID,
		).Scan(&currentPos)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: task %d not found in sprint %d", utils.ErrNotFound, taskID, sprintID)
			}
			return fmt.Errorf("getting current position: %w", err)
		}

		// Read the sprint's current order. It doubles as the task count, so no
		// separate COUNT(*) is issued.
		members, err := sprintMembersInOrderTx(tx, sprintID)
		if err != nil {
			return err
		}
		taskCount := len(members)

		// If position >= task count, move to end
		if newPosition >= taskCount {
			newPosition = taskCount - 1
		}

		// If position unchanged, nothing to do — including no audit entry, since
		// nothing happened for one to record.
		if currentPos == newPosition {
			return nil
		}

		// Lift the moved task out of the current order and re-insert it at the
		// target slot. The result is a permutation of the sprint's members, which
		// is what the assignment below writes as a dense 0..N-1 run.
		ordered := make([]int, 0, taskCount)
		for _, m := range members {
			if m.taskID != taskID {
				ordered = append(ordered, m.taskID)
			}
		}
		if newPosition < 0 {
			newPosition = 0
		}
		if newPosition > len(ordered) {
			newPosition = len(ordered)
		}
		ordered = append(ordered, 0)
		copy(ordered[newPosition+1:], ordered[newPosition:])
		ordered[newPosition] = taskID

		if err := parkSprintPositionsTx(tx, sprintID); err != nil {
			return err
		}
		if err := assignSprintPositionsTx(tx, sprintID, ordered); err != nil {
			return err
		}

		// Log audit entry
		now := utils.NowISO8601()
		if err := LogAuditTx(tx, models.OpSprintTaskMovePosition, models.EntitySprint, sprintID, now); err != nil {
			return fmt.Errorf("logging audit entry: %w", err)
		}
		return nil
	})
}

// SwapTasks exchanges the positions of two tasks in a sprint.
// Both tasks must belong to the same sprint.
func (db *DB) SwapTasks(sprintID, taskID1, taskID2 int) error {
	if taskID1 == taskID2 {
		return ErrCannotSwapSelf
	}

	return db.WithTransaction(func(tx *sql.Tx) error {
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

		// ONLY ONE ROW NEEDS PARKING HERE. A swap touches exactly two rows, and
		// once the first has left its position the second can take it, so these
		// three statements are the cheapest form that never presents a duplicate.
		// Writing the two positions directly fails on the first statement,
		// because the position it assigns is the one the other task still holds
		// (SPEC/DATABASE.md § Swap Tasks).
		if _, err := tx.Exec(
			"UPDATE sprint_tasks SET position = -1 - position WHERE sprint_id = ? AND task_id = ?",
			sprintID, taskID1,
		); err != nil {
			return fmt.Errorf("parking position for task %d: %w", taskID1, err)
		}

		if _, err := tx.Exec(
			"UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?",
			pos1, sprintID, taskID2,
		); err != nil {
			return fmt.Errorf("updating position for task %d: %w", taskID2, err)
		}

		if _, err := tx.Exec(
			"UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?",
			pos2, sprintID, taskID1,
		); err != nil {
			return fmt.Errorf("updating position for task %d: %w", taskID1, err)
		}

		// Log audit entry
		now := utils.NowISO8601()
		if err := LogAuditTx(tx, models.OpSprintTaskSwap, models.EntitySprint, sprintID, now); err != nil {
			return fmt.Errorf("logging audit entry: %w", err)
		}
		return nil
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

	// Get pending (never-started) sprint count
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sprints WHERE status = ?",
		models.SprintPending,
	).Scan(&stats.Pending)
	if err != nil {
		return nil, fmt.Errorf("counting pending sprints: %w", err)
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

		// Floor the duration at 1 day so a sub-day (or same-day) sprint does not
		// inflate velocity. The previous zero-duration skip is no longer needed:
		// with the floor, durationDays is always >= 1.0, so every qualifying
		// closed sprint stays counted.
		durationDays := math.Max(1.0, closeTime.Sub(startTime).Hours()/24)

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
