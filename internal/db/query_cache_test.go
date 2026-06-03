package db

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// TestQueryCacheGetQueryCachedSizes verifies that GetQuery returns a template
// whose interpolated placeholder count matches the requested size for sizes
// that are pre-cached individually (1-100).
func TestQueryCacheGetQueryCachedSizes(t *testing.T) {
	qc := NewQueryCache()

	for _, size := range []int{1, 2, 5, 50, 99, 100} {
		q := qc.GetQuery(OpUpdateTaskPriority, size)
		if q == "" {
			t.Fatalf("size %d: empty template", size)
		}
		// The IN clause must contain exactly `size` placeholders.
		got := countINPlaceholders(t, q)
		if got != size {
			t.Errorf("size %d: IN clause has %d placeholders, want %d", size, got, size)
		}
	}
}

// TestQueryCacheNormalizeSize verifies that out-of-band sizes normalize to the
// nearest larger cached bucket (250, 500, 1000) and that the returned template
// carries that bucket's placeholder count, not the requested count.
func TestQueryCacheNormalizeSize(t *testing.T) {
	qc := NewQueryCache()

	cases := []struct {
		requested int
		wantBkt   int
	}{
		{0, 1},     // non-positive clamps up to 1
		{101, 250}, // just above the individually-cached band
		{250, 250},
		{300, 500},
		{500, 500},
		{750, 1000},
		{1000, 1000},
	}
	for _, c := range cases {
		if got := qc.normalizeSize(c.requested); got != c.wantBkt {
			t.Errorf("normalizeSize(%d) = %d, want %d", c.requested, got, c.wantBkt)
		}
		q := qc.GetQuery(OpUpdateTaskStatus, c.requested)
		if got := countINPlaceholders(t, q); got != c.wantBkt {
			t.Errorf("GetQuery(status, %d): IN has %d placeholders, want bucket %d",
				c.requested, got, c.wantBkt)
		}
	}
}

// TestQueryCacheOnDemandFallback verifies the generateQuery fallback path. For
// a known operation every size normalizes onto a pre-cached bucket, so the
// fallback is reached only defensively; we therefore exercise generateQuery
// directly with an arbitrary (uncached) placeholder count and assert it yields
// the exact same SQL as the shared buildTemplates source of truth.
func TestQueryCacheOnDemandFallback(t *testing.T) {
	qc := NewQueryCache()

	const size = 1500 // an arbitrary, non-bucket size
	got := qc.generateQuery(OpUpdateTaskSeverity, size)
	if n := countINPlaceholders(t, got); n != size {
		t.Fatalf("on-demand size %d: IN has %d placeholders, want %d", size, n, size)
	}

	// The fallback must agree with buildTemplates for the same placeholder run.
	want := buildTemplates(generatePlaceholders(size))[OpUpdateTaskSeverity]
	if got != want {
		t.Errorf("on-demand template diverges from buildTemplates output\n got: %q\nwant: %q", got, want)
	}

	// An unknown operation returns empty from the fallback.
	if q := qc.generateQuery("nope", size); q != "" {
		t.Errorf("generateQuery(unknown) = %q, want empty", q)
	}
}

// TestQueryCacheUnknownOperation verifies that an unknown operation key yields
// an empty string from both the cached and on-demand paths (defensive: callers
// must never pass an unregistered op).
func TestQueryCacheUnknownOperation(t *testing.T) {
	qc := NewQueryCache()
	if q := qc.GetQuery("does_not_exist", 10); q != "" {
		t.Errorf("unknown op (cached size) = %q, want empty", q)
	}
	if q := qc.GetQuery("does_not_exist", 5000); q != "" {
		t.Errorf("unknown op (fallback size) = %q, want empty", q)
	}
}

// TestQueryCacheGetPlaceholders verifies cached and out-of-range placeholder
// generation.
func TestQueryCacheGetPlaceholders(t *testing.T) {
	qc := NewQueryCache()

	cases := map[int]string{
		0: "",
		1: "?",
		3: "?,?,?",
	}
	for n, want := range cases {
		if got := qc.GetPlaceholders(n); got != want {
			t.Errorf("GetPlaceholders(%d) = %q, want %q", n, got, want)
		}
	}

	// Above the pre-generated range (1000), fall back to generation.
	const big = 1200
	if got := qc.GetPlaceholders(big); strings.Count(got, "?") != big {
		t.Errorf("GetPlaceholders(%d): got %d placeholders, want %d", big, strings.Count(got, "?"), big)
	}
	// Negative count falls back to the empty generator.
	if got := qc.GetPlaceholders(-1); got != "" {
		t.Errorf("GetPlaceholders(-1) = %q, want empty", got)
	}
}

// TestQueryCacheTemplatesMatchProductionQueries is the reconciliation guard: it
// pins each cached template to the exact SQL its production builder constructs.
// If a builder's query shape changes without updating buildTemplates (or vice
// versa), this test fails — preventing the silent template/schema drift that
// originally left the cache referencing non-existent columns.
func TestQueryCacheTemplatesMatchProductionQueries(t *testing.T) {
	qc := NewQueryCache()
	const size = 3
	ph := generatePlaceholders(size)

	// Each want string is built from the same fragments the production
	// builders in queries.go use, so this asserts byte-identical SQL.
	wants := map[string]string{
		OpGetTasks: fmt.Sprintf(
			`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
			        t.created_at, t.specialists, t.started_at, t.tested_at, t.closed_at, t.completion_summary, t.parent_task_id,
			        t.priority, t.severity,
			        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count`+taskDepsSelect+`
			 FROM tasks t WHERE t.id IN (%s) ORDER BY t.id`, ph),
		OpUpdateTaskStatus:          fmt.Sprintf("UPDATE tasks SET status = ? WHERE id IN (%s)", ph),
		OpUpdateTaskStatusDoing:     fmt.Sprintf("UPDATE tasks SET status = ?, started_at = ? WHERE id IN (%s)", ph),
		OpUpdateTaskStatusTesting:   fmt.Sprintf("UPDATE tasks SET status = ?, tested_at = ? WHERE id IN (%s)", ph),
		OpUpdateTaskStatusCompleted: fmt.Sprintf("UPDATE tasks SET status = ?, closed_at = ? WHERE id IN (%s)", ph),
		OpUpdateTaskStatusBacklog:   fmt.Sprintf("UPDATE tasks SET status = ?, started_at = NULL, tested_at = NULL, closed_at = NULL WHERE id IN (%s)", ph),
		OpUpdateTaskPriority:        fmt.Sprintf("UPDATE tasks SET priority = ? WHERE id IN (%s)", ph),
		OpUpdateTaskSeverity:        fmt.Sprintf("UPDATE tasks SET severity = ? WHERE id IN (%s)", ph),
		OpAddTasksToSprint:          fmt.Sprintf("UPDATE tasks SET status = ? WHERE id IN (%s)", ph),
		OpRemoveTasksFromSprint:     fmt.Sprintf("UPDATE tasks SET status = ? WHERE id IN (%s)", ph),
	}
	for op, want := range wants {
		if got := qc.GetQuery(op, size); got != want {
			t.Errorf("template for %q diverged from production query\n got: %q\nwant: %q", op, got, want)
		}
	}
}

// TestQueryCacheGetTasksTemplateExecutesAgainstRealSchema proves the cached
// OpGetTasks template is valid against the production schema and returns the
// expected rows — the column drift that previously made the template reference
// non-existent columns would surface here as a SQL error.
func TestQueryCacheGetTasksTemplateExecutesAgainstRealSchema(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ids := createBenchmarkTasks(t, db, 4)

	// Execute the cached template directly (not via GetTasks) to isolate the
	// template's correctness against the real schema.
	query := db.queryCache.GetQuery(OpGetTasks, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := db.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("cached OpGetTasks template failed against real schema: %v", err)
	}
	defer rows.Close()

	tasks, err := scanTasksWithDeps(rows)
	if err != nil {
		t.Fatalf("scanning cached-template rows: %v", err)
	}
	if len(tasks) != len(ids) {
		t.Fatalf("cached template returned %d tasks, want %d", len(tasks), len(ids))
	}
}

// countINPlaceholders extracts the IN (...) clause from a query and returns the
// number of "?" placeholders inside it. Fails the test if no IN clause exists.
func countINPlaceholders(t *testing.T, query string) int {
	t.Helper()
	const marker = "IN ("
	idx := strings.LastIndex(query, marker)
	if idx < 0 {
		t.Fatalf("query has no IN clause: %q", query)
	}
	rest := query[idx+len(marker):]
	end := strings.Index(rest, ")")
	if end < 0 {
		t.Fatalf("unterminated IN clause: %q", query)
	}
	return strings.Count(rest[:end], "?")
}

// createBenchmarkTasks inserts n minimal valid tasks via the production
// CreateTask path and returns their IDs. Shared by query-cache and batch tests.
func createBenchmarkTasks(t *testing.T, db *DB, n int) []int {
	t.Helper()
	ids := make([]int, 0, n)
	for i := 0; i < n; i++ {
		task := &models.Task{
			Title:                  fmt.Sprintf("Task %d", i),
			Status:                 models.StatusBacklog,
			Type:                   models.TypeTask,
			FunctionalRequirements: "functional",
			TechnicalRequirements:  "technical",
			AcceptanceCriteria:     "criteria",
			CreatedAt:              time.Now().UTC().Format(time.RFC3339),
			Priority:               i % 10,
			Severity:               i % 10,
		}
		id, err := db.CreateTask(context.Background(), task)
		if err != nil {
			t.Fatalf("creating task %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	return ids
}
