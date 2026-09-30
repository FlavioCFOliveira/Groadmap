package db

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// TestQueryCacheGetQueryCachedSizes verifies that GetQuery returns a template
// whose interpolated placeholder count matches the requested size for sizes
// that normalise to themselves (1-100).
//
// Every test in this file that needs one batch-update template names
// OpAddTasksToSprint, because it is now the only one. The eight it used to
// spread across — the status update in its five lifecycle shapes, the priority
// and severity updates, and the sprint removal — served db-layer methods the
// command layer had replaced, and went with them (task #188).
func TestQueryCacheGetQueryCachedSizes(t *testing.T) {
	qc := NewQueryCache()

	for _, size := range []int{1, 2, 5, 50, 99, 100} {
		q := qc.GetQuery(OpAddTasksToSprint, size)
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
// nearest larger normalised size (250, 500, 1000) and that the returned template
// carries that bucket's placeholder count, not the requested count.
func TestQueryCacheNormalizeSize(t *testing.T) {
	qc := NewQueryCache()

	cases := []struct {
		requested int
		wantBkt   int
	}{
		{0, 1},     // non-positive clamps up to 1
		{101, 250}, // just above the band that keeps its own size
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
		q := qc.GetQuery(OpAddTasksToSprint, c.requested)
		if got := countINPlaceholders(t, q); got != c.wantBkt {
			t.Errorf("GetQuery(status, %d): IN has %d placeholders, want bucket %d",
				c.requested, got, c.wantBkt)
		}
	}
}

// TestQueryCacheHoldsNoPrecomputedState pins the on-demand rule of
// SPEC/IMPLEMENTATION.md § Cache Strategy: opening a database MUST NOT generate
// any template or placeholder list. Open and OpenReadOnly construct the cache
// with NewQueryCache, so a cache type that holds no field cannot carry anything
// generated at open time. A field added back to QueryCache, to hold a
// precomputed table, fails here.
func TestQueryCacheHoldsNoPrecomputedState(t *testing.T) {
	if n := reflect.TypeOf(QueryCache{}).NumField(); n != 0 {
		t.Fatalf("QueryCache declares %d field(s); it must hold no state, so that opening a "+
			"database generates no template or placeholder list", n)
	}
	if got := NewQueryCache(); *got != (QueryCache{}) {
		t.Fatalf("NewQueryCache() = %+v, want the zero value", *got)
	}
}

// TestQueryCacheTextIdentityAcrossCalls pins the identity requirement: the same
// operation at the same requested size yields the same text on every call, and
// two requested sizes with the same normalised size yield the same text.
func TestQueryCacheTextIdentityAcrossCalls(t *testing.T) {
	qc := NewQueryCache()
	for _, op := range []string{OpGetTasks, OpAddTasksToSprint} {
		for _, size := range []int{1, 7, 100} {
			if a, b := qc.GetQuery(op, size), qc.GetQuery(op, size); a != b {
				t.Errorf("%s at size %d yields two different texts", op, size)
			}
		}
		pairs := [][2]int{{-3, 1}, {0, 1}, {101, 250}, {251, 500}, {501, 1000}, {5000, 1000}}
		for _, p := range pairs {
			if a, b := qc.GetQuery(op, p[0]), qc.GetQuery(op, p[1]); a != b {
				t.Errorf("%s: sizes %d and %d normalise alike but yield different texts", op, p[0], p[1])
			}
		}
	}
	// A template for an arbitrary placeholder run agrees with buildTemplate.
	const size = 1500
	want := buildTemplate(OpAddTasksToSprint, generatePlaceholders(size))
	if n := countINPlaceholders(t, want); n != size {
		t.Fatalf("buildTemplate at %d placeholders: IN has %d", size, n)
	}
}

// TestQueryCacheUnknownOperation verifies that an unknown operation key yields
// an empty string at every size (defensive: callers
// must never pass an unregistered op).
func TestQueryCacheUnknownOperation(t *testing.T) {
	qc := NewQueryCache()
	if q := qc.GetQuery("does_not_exist", 10); q != "" {
		t.Errorf("unknown op (size 10) = %q, want empty", q)
	}
	if q := qc.GetQuery("does_not_exist", 5000); q != "" {
		t.Errorf("unknown op (size 5000) = %q, want empty", q)
	}
}

// TestQueryCacheGetPlaceholders verifies small, large and negative placeholder
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

	// A count above the largest normalised size is generated exactly.
	const big = 1200
	if got := qc.GetPlaceholders(big); strings.Count(got, "?") != big {
		t.Errorf("GetPlaceholders(%d): got %d placeholders, want %d", big, strings.Count(got, "?"), big)
	}
	// A negative count yields the empty list.
	if got := qc.GetPlaceholders(-1); got != "" {
		t.Errorf("GetPlaceholders(-1) = %q, want empty", got)
	}
}

// TestQueryCacheTemplatesMatchProductionQueries is the reconciliation guard: it
// pins each template to the exact SQL its production builder constructs.
// If a builder's query shape changes without updating buildTemplate (or vice
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
			        t.created_at, t.started_at, t.tested_at, t.closed_at, t.completion_summary,
			        t.commit_open, t.commit_close, t.parent_task_id,
			        t.priority, t.severity,
			        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count`+taskDepsSelect+`
			 FROM tasks t WHERE t.id IN (%s) ORDER BY t.id`, ph),
		OpAddTasksToSprint: fmt.Sprintf("UPDATE tasks SET status = ? WHERE id IN (%s) AND status = 'BACKLOG'", ph),
	}
	for op, want := range wants {
		if got := qc.GetQuery(op, size); got != want {
			t.Errorf("template for %q diverged from production query\n got: %q\nwant: %q", op, got, want)
		}
	}
}

// TestQueryCacheGetTasksTemplateExecutesAgainstRealSchema proves the
// OpGetTasks template is valid against the production schema and returns the
// expected rows — the column drift that previously made the template reference
// non-existent columns would surface here as a SQL error.
func TestQueryCacheGetTasksTemplateExecutesAgainstRealSchema(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ids := seedTasks(t, db, 4)

	// Execute the template directly (not via GetTasks) to isolate the
	// template's correctness against the real schema.
	query := db.queryCache.GetQuery(OpGetTasks, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := db.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("OpGetTasks template failed against real schema: %v", err)
	}
	defer rows.Close()

	tasks, err := scanTasksWithDeps(rows)
	if err != nil {
		t.Fatalf("scanning template rows: %v", err)
	}
	if len(tasks) != len(ids) {
		t.Fatalf("template returned %d tasks, want %d", len(tasks), len(ids))
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

// seedTasks inserts n minimal valid tasks via the production seeded insert path
// and returns their IDs. Shared by the query-cache and batch tests.
//
// It was named createBenchmarkTasks while this package had benchmarks. It never
// served one after they went, and the project keeps none
// (SPEC/BUILD.md § No Benchmarks and No Performance-Measurement Tests), so the
// name said something about the helper that was no longer true.
func seedTasks(t *testing.T, db *DB, n int) []int {
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
		id, err := seedTask(db, task)
		if err != nil {
			t.Fatalf("creating task %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	return ids
}
