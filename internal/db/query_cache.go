package db

import (
	"fmt"
	"strings"
	"sync"
)

// Operation type constants for cache keys.
//
// These name the batch operations whose SQL is cached. The templates are
// reconciled to be byte-identical in semantics to the inline queries the
// production builders in queries.go would otherwise construct with
// fmt.Sprintf + strings.Join, so routing a builder through GetQuery changes
// nothing observable except eliminating per-call query-plan recompilation.
//
// UpdateTaskStatus has several SET-clause shapes depending on the target
// status (lifecycle-timestamp tracking per SPEC/STATE_MACHINE.md). Each shape
// is cached under its own key so every transition benefits from plan reuse.
const (
	OpGetTasks              = "get_tasks"
	OpUpdateTaskStatus      = "update_task_status"
	OpUpdateTaskPriority    = "update_task_priority"
	OpUpdateTaskSeverity    = "update_task_severity"
	OpAddTasksToSprint      = "add_tasks_to_sprint"
	OpRemoveTasksFromSprint = "remove_tasks_from_sprint"

	// Lifecycle-specific UpdateTaskStatus variants. The SET clause differs by
	// the transition; the WHERE id IN (...) tail is shared.
	OpUpdateTaskStatusDoing     = "update_task_status_doing"     // SET status, started_at
	OpUpdateTaskStatusTesting   = "update_task_status_testing"   // SET status, tested_at
	OpUpdateTaskStatusCompleted = "update_task_status_completed" // SET status, closed_at
	OpUpdateTaskStatusBacklog   = "update_task_status_backlog"   // SET status, clear all timestamps
)

// QueryCache stores pre-generated query templates for batch operations.
// It eliminates query plan recompilation overhead by caching prepared
// statement templates for common IN clause sizes.
type QueryCache struct {
	// templates maps operation name to cached queries
	// Key format: "{operation}_{size}"
	templates map[string]string

	// placeholders caches pre-generated placeholder strings
	// Index 0 = "", Index 1 = "?", Index 2 = "?,?", etc.
	placeholders []string

	// mu protects templates for thread-safe access
	mu sync.RWMutex
}

// NewQueryCache creates and initializes a query cache with pre-generated templates.
// It pre-computes placeholder strings for sizes 0-1000 and query templates
// for all supported batch operations.
func NewQueryCache() *QueryCache {
	qc := &QueryCache{
		templates:    make(map[string]string),
		placeholders: make([]string, 1001), // 0-1000
	}

	// Pre-generate placeholder strings
	for i := 0; i <= 1000; i++ {
		qc.placeholders[i] = generatePlaceholders(i)
	}

	// Pre-generate query templates for all operations
	qc.initializeTemplates()

	return qc
}

// generatePlaceholders creates a comma-separated string of "?" placeholders.
// Returns empty string for n=0, "?" for n=1, "?,?" for n=2, etc.
func generatePlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	placeholders := make([]string, n)
	for i := range placeholders {
		placeholders[i] = "?"
	}
	return strings.Join(placeholders, ",")
}

// initializeTemplates pre-generates query templates for all supported operations.
// Invariant: this method is called exclusively from NewQueryCache, before the
// *QueryCache pointer is returned to any caller. No other goroutine can hold a
// reference at that point, so qc.mu need not be acquired here. Any future
// caller that shares the object across goroutines must acquire qc.mu.Lock()
// before invoking this method.
func (qc *QueryCache) initializeTemplates() {
	// Define cached sizes: 1-100, 250, 500, 1000
	sizes := make([]int, 0, 103)
	for i := 1; i <= 100; i++ {
		sizes = append(sizes, i)
	}
	sizes = append(sizes, 250, 500, 1000)

	// Generate templates for each operation and size
	for _, size := range sizes {
		placeholders := qc.placeholders[size]
		for op, tmpl := range buildTemplates(placeholders) {
			qc.templates[fmt.Sprintf("%s_%d", op, size)] = tmpl
		}
	}
}

// buildTemplates returns, for a given placeholder string, the SQL template of
// every cached operation. It is the single source of truth shared by the
// pre-generation path (initializeTemplates) and the on-demand fallback
// (generateQuery), so the two can never drift apart.
//
// Each template is byte-identical in semantics to the query the corresponding
// production builder in queries.go constructs inline. In particular:
//   - OpGetTasks reproduces GetTasks: table alias t, the subtask_count
//     correlated subquery, the taskDepsSelect dependency columns, and the
//     ORDER BY t.id tail, so scanTasksWithDeps consumes an unchanged row shape.
//   - The Add/Remove sprint variants use a status = ? parameter (not a literal)
//     exactly as AddTasksToSprint / RemoveTasksFromSprint do.
func buildTemplates(placeholders string) map[string]string {
	return map[string]string{
		// GetTasks: full task projection with dependency CSV columns,
		// identical to (*DB).GetTasks.
		OpGetTasks: fmt.Sprintf(
			`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
			        t.created_at, t.specialists, t.started_at, t.tested_at, t.closed_at, t.completion_summary, t.parent_task_id,
			        t.priority, t.severity,
			        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count`+taskDepsSelect+`
			 FROM tasks t WHERE t.id IN (%s) ORDER BY t.id`,
			placeholders,
		),

		// UpdateTaskStatus default shape: status only.
		OpUpdateTaskStatus: fmt.Sprintf(
			"UPDATE tasks SET status = ? WHERE id IN (%s)",
			placeholders,
		),
		// Lifecycle variants set the appropriate timestamp column.
		OpUpdateTaskStatusDoing: fmt.Sprintf(
			"UPDATE tasks SET status = ?, started_at = ? WHERE id IN (%s)",
			placeholders,
		),
		OpUpdateTaskStatusTesting: fmt.Sprintf(
			"UPDATE tasks SET status = ?, tested_at = ? WHERE id IN (%s)",
			placeholders,
		),
		OpUpdateTaskStatusCompleted: fmt.Sprintf(
			"UPDATE tasks SET status = ?, closed_at = ? WHERE id IN (%s)",
			placeholders,
		),
		OpUpdateTaskStatusBacklog: fmt.Sprintf(
			"UPDATE tasks SET status = ?, started_at = NULL, tested_at = NULL, closed_at = NULL WHERE id IN (%s)",
			placeholders,
		),

		// UpdateTaskPriority: priority only.
		OpUpdateTaskPriority: fmt.Sprintf(
			"UPDATE tasks SET priority = ? WHERE id IN (%s)",
			placeholders,
		),
		// UpdateTaskSeverity: severity only.
		OpUpdateTaskSeverity: fmt.Sprintf(
			"UPDATE tasks SET severity = ? WHERE id IN (%s)",
			placeholders,
		),

		// AddTasksToSprint / RemoveTasksFromSprint: status as a bound parameter
		// (SPRINT / BACKLOG respectively), matching the production builders.
		OpAddTasksToSprint: fmt.Sprintf(
			"UPDATE tasks SET status = ? WHERE id IN (%s)",
			placeholders,
		),
		OpRemoveTasksFromSprint: fmt.Sprintf(
			"UPDATE tasks SET status = ? WHERE id IN (%s)",
			placeholders,
		),
	}
}

// GetQuery retrieves a cached query template for the given operation and batch size.
// If the exact size is not cached, it returns the nearest larger cached size.
// This method is thread-safe.
func (qc *QueryCache) GetQuery(operation string, size int) string {
	// Normalize size to nearest cached value
	cacheSize := qc.normalizeSize(size)

	key := fmt.Sprintf("%s_%d", operation, cacheSize)

	qc.mu.RLock()
	template, exists := qc.templates[key]
	qc.mu.RUnlock()

	if exists {
		return template
	}

	// Generate on-demand for non-standard sizes (should be rare)
	return qc.generateQuery(operation, size)
}

// normalizeSize returns the nearest cached size for a given batch size.
// Sizes 1-100 are cached individually. Larger sizes use 250, 500, or 1000.
func (qc *QueryCache) normalizeSize(size int) int {
	if size <= 0 {
		return 1
	}
	if size <= 100 {
		return size
	}
	if size <= 250 {
		return 250
	}
	if size <= 500 {
		return 500
	}
	return 1000
}

// generateQuery creates a query template on-demand for non-cached sizes.
// This is used as a fallback when the exact size is not pre-cached. It shares
// buildTemplates with the pre-generation path so a fallback template is
// guaranteed identical to its cached counterpart.
func (qc *QueryCache) generateQuery(operation string, size int) string {
	placeholders := generatePlaceholders(size)
	return buildTemplates(placeholders)[operation]
}

// GetPlaceholders returns a pre-generated placeholder string for the given count.
// This is useful for queries that need placeholders but don't use cached templates.
func (qc *QueryCache) GetPlaceholders(n int) string {
	if n < 0 || n >= len(qc.placeholders) {
		return generatePlaceholders(n)
	}
	return qc.placeholders[n]
}
