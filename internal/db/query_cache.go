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
// The list holds exactly the operations a production builder fetches. Eight
// further keys used to sit here — the status update in its five lifecycle
// shapes, the priority and severity updates, and the sprint removal — cached
// for db-layer methods the command layer had replaced with its own inline SQL.
// Nothing fetched them, so they were a second set of statements maintained
// beside the ones that run; they were removed with the methods they served
// (task #188). Adding a key here without a builder that calls GetQuery for it
// re-creates that state.
const (
	OpGetTasks         = "get_tasks"
	OpAddTasksToSprint = "add_tasks_to_sprint"
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
//   - OpAddTasksToSprint uses a status = ? parameter (not a literal) exactly as
//     AddTasksToSprint does.
func buildTemplates(placeholders string) map[string]string {
	return map[string]string{
		// GetTasks: full task projection with dependency CSV columns,
		// identical to (*DB).GetTasks.
		OpGetTasks: fmt.Sprintf(
			`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
			        t.created_at, t.started_at, t.tested_at, t.closed_at, t.completion_summary,
			        t.commit_open, t.commit_close, t.parent_task_id,
			        t.priority, t.severity,
			        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count`+taskDepsSelect+`
			 FROM tasks t WHERE t.id IN (%s) ORDER BY t.id`,
			placeholders,
		),

		// AddTasksToSprint: status as a bound parameter (SPRINT), matching the
		// production builder.
		OpAddTasksToSprint: fmt.Sprintf(
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
