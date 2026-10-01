package db

import (
	"fmt"
	"strings"
)

// Operation type constants for template keys.
//
// These name the batch operations whose SQL is generated from a template. The
// templates are reconciled to be byte-identical in semantics to the inline
// queries the production builders in queries.go would otherwise construct with
// fmt.Sprintf + strings.Join, so routing a builder through GetQuery changes
// nothing observable except presenting SQLite with a query text it has already
// compiled.
//
// The list holds exactly the operations a production builder fetches. Eight
// further keys used to sit here — the status update in its five lifecycle
// shapes, the priority and severity updates, and the sprint removal — kept for
// db-layer methods the command layer had replaced with its own inline SQL.
// Nothing fetched them, so they were a second set of statements maintained
// beside the ones that run; they were removed with the methods they served
// (task #188). Adding a key here without a builder that calls GetQuery for it
// re-creates that state.
const (
	OpGetTasks         = "get_tasks"
	OpAddTasksToSprint = "add_tasks_to_sprint"
)

// QueryCache hands out the query text of the batch operations and the
// placeholder lists of IN clauses.
//
// It holds no state, and that is the point of it. Every text is generated on
// demand, when an operation asks for it, so opening a database costs no
// template or placeholder generation, and a command that issues no batch
// operation generates none (SPEC/IMPLEMENTATION.md § Cache Strategy). An
// earlier version precomputed 1,001 placeholder strings and 206 templates in
// NewQueryCache, which every Open and OpenReadOnly paid whether or not the
// command used any of them.
//
// What SQLite's statement reuse depends on is the IDENTITY of the text, not
// where it is stored: the same operation at the same normalised size always
// yields the same string, so repeated batches of that size present SQLite with
// a query it has already compiled. Having no state also makes the type safe for
// concurrent use by construction.
type QueryCache struct{}

// NewQueryCache returns a query cache. It generates nothing: see QueryCache.
func NewQueryCache() *QueryCache {
	return &QueryCache{}
}

// generatePlaceholders creates a comma-separated string of "?" placeholders.
// Returns empty string for n <= 0, "?" for n=1, "?,?" for n=2, etc.
func generatePlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

// buildTemplate returns the SQL template of one operation for the given
// placeholder string, or the empty string for an unknown operation. It is the
// single source of truth of the template text.
//
// Each template is byte-identical in semantics to the query the corresponding
// production builder in queries.go constructs inline. In particular:
//   - OpGetTasks reproduces GetTasks: table alias t, the subtask_count
//     correlated subquery, the taskDepsSelect dependency columns, and the
//     ORDER BY t.id tail, so scanTasksWithDeps consumes an unchanged row shape.
//   - OpAddTasksToSprint uses a status = ? parameter (not a literal) exactly as
//     AddTasksToSprint does.
func buildTemplate(operation, placeholders string) string {
	switch operation {
	case OpGetTasks:
		// GetTasks: full task projection with dependency CSV columns,
		// identical to (*DB).GetTasks.
		return fmt.Sprintf(
			`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
			        t.created_at, t.started_at, t.tested_at, t.closed_at, t.completion_summary,
			        t.commit_open, t.commit_close, t.parent_task_id,
			        t.priority, t.severity,
			        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count`+taskDepsSelect+`
			 FROM tasks t WHERE t.id IN (%s) ORDER BY t.id`,
			placeholders,
		)
	case OpAddTasksToSprint:
		// AddTasksToSprint: status as a bound parameter (SPRINT), set only on
		// the named tasks that join from BACKLOG (SPEC/DATABASE.md § Add Task
		// to Sprint with Position).
		return fmt.Sprintf(
			"UPDATE tasks SET status = ? WHERE id IN (%s) AND status = 'BACKLOG'",
			placeholders,
		)
	default:
		return ""
	}
}

// GetQuery returns the query template of the given operation for a batch of the
// given size: the operation's SQL with one placeholder per unit of the
// normalised size (see normalizeSize). An unknown operation yields the empty
// string. This method is safe for concurrent use.
func (qc *QueryCache) GetQuery(operation string, size int) string {
	return buildTemplate(operation, generatePlaceholders(qc.normalizeSize(size)))
}

// normalizeSize returns the normalised size of a batch of the given size.
// Sizes 1-100 keep their own size; larger sizes use 250, 500, or 1000; a
// non-positive size normalises to 1 (SPEC/IMPLEMENTATION.md § Cache Strategy,
// Normalised sizes).
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

// GetPlaceholders returns a comma-separated list of exactly n "?" placeholders,
// or the empty string for n <= 0. It is generated on demand.
func (qc *QueryCache) GetPlaceholders(n int) string {
	return generatePlaceholders(n)
}
