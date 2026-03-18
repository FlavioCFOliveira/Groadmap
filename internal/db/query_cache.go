package db

import (
	"fmt"
	"strings"
	"sync"
)

// Operation type constants for cache keys
const (
	OpGetTasks              = "get_tasks"
	OpUpdateTaskStatus      = "update_task_status"
	OpUpdateTaskPriority    = "update_task_priority"
	OpUpdateTaskSeverity    = "update_task_severity"
	OpAddTasksToSprint      = "add_tasks_to_sprint"
	OpRemoveTasksFromSprint = "remove_tasks_from_sprint"
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
// This is called once during cache initialization.
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

		// GetTasks: SELECT ... FROM tasks WHERE id IN (...)
		qc.templates[fmt.Sprintf("%s_%d", OpGetTasks, size)] = fmt.Sprintf(
			`SELECT id, priority, severity, status, description, specialists, action, expected_result, created_at, completed_at
			 FROM tasks WHERE id IN (%s) ORDER BY id`,
			placeholders,
		)

		// UpdateTaskStatus: UPDATE tasks SET status = ?, completed_at = ? WHERE id IN (...)
		qc.templates[fmt.Sprintf("%s_%d", OpUpdateTaskStatus, size)] = fmt.Sprintf(
			"UPDATE tasks SET status = ?, completed_at = ? WHERE id IN (%s)",
			placeholders,
		)

		// UpdateTaskPriority: UPDATE tasks SET priority = ? WHERE id IN (...)
		qc.templates[fmt.Sprintf("%s_%d", OpUpdateTaskPriority, size)] = fmt.Sprintf(
			"UPDATE tasks SET priority = ? WHERE id IN (%s)",
			placeholders,
		)

		// UpdateTaskSeverity: UPDATE tasks SET severity = ? WHERE id IN (...)
		qc.templates[fmt.Sprintf("%s_%d", OpUpdateTaskSeverity, size)] = fmt.Sprintf(
			"UPDATE tasks SET severity = ? WHERE id IN (%s)",
			placeholders,
		)

		// AddTasksToSprint: UPDATE tasks SET status = 'SPRINT' WHERE id IN (...)
		qc.templates[fmt.Sprintf("%s_%d", OpAddTasksToSprint, size)] = fmt.Sprintf(
			"UPDATE tasks SET status = 'SPRINT' WHERE id IN (%s)",
			placeholders,
		)

		// RemoveTasksFromSprint: UPDATE tasks SET status = 'BACKLOG' WHERE id IN (...)
		qc.templates[fmt.Sprintf("%s_%d", OpRemoveTasksFromSprint, size)] = fmt.Sprintf(
			"UPDATE tasks SET status = 'BACKLOG' WHERE id IN (%s)",
			placeholders,
		)
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
// This is used as a fallback when the exact size is not pre-cached.
func (qc *QueryCache) generateQuery(operation string, size int) string {
	placeholders := generatePlaceholders(size)

	switch operation {
	case OpGetTasks:
		return fmt.Sprintf(
			`SELECT id, priority, severity, status, description, specialists, action, expected_result, created_at, completed_at
			 FROM tasks WHERE id IN (%s) ORDER BY id`,
			placeholders,
		)
	case OpUpdateTaskStatus:
		return fmt.Sprintf(
			"UPDATE tasks SET status = ?, completed_at = ? WHERE id IN (%s)",
			placeholders,
		)
	case OpUpdateTaskPriority:
		return fmt.Sprintf(
			"UPDATE tasks SET priority = ? WHERE id IN (%s)",
			placeholders,
		)
	case OpUpdateTaskSeverity:
		return fmt.Sprintf(
			"UPDATE tasks SET severity = ? WHERE id IN (%s)",
			placeholders,
		)
	case OpAddTasksToSprint:
		return fmt.Sprintf(
			"UPDATE tasks SET status = 'SPRINT' WHERE id IN (%s)",
			placeholders,
		)
	case OpRemoveTasksFromSprint:
		return fmt.Sprintf(
			"UPDATE tasks SET status = 'BACKLOG' WHERE id IN (%s)",
			placeholders,
		)
	default:
		return ""
	}
}

// GetPlaceholders returns a pre-generated placeholder string for the given count.
// This is useful for queries that need placeholders but don't use cached templates.
func (qc *QueryCache) GetPlaceholders(n int) string {
	if n < 0 || n >= len(qc.placeholders) {
		return generatePlaceholders(n)
	}
	return qc.placeholders[n]
}
