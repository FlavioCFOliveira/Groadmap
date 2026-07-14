package db

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The composite indexes SPEC/DATABASE.md § Performance Optimization mandates,
// each paired with the table it lives on. Dropping any of them from
// CreateSchema must fail TestCompositeIndexesExistInProductionSchema.
var specComposite = []struct {
	index string
	table string
}{
	{"idx_tasks_status_priority", "tasks"},
	{"idx_tasks_priority_created", "tasks"},
	{"idx_sprint_tasks_lookup", "sprint_tasks"},
	{"idx_audit_date", "audit"},
}

// TestCompositeIndexesExistInProductionSchema asserts that every composite
// index the SPEC mandates is actually created by db.CreateSchema. This reads
// the production schema out of sqlite_master, so it fails the moment an index
// is dropped from internal/db/schema.go.
func TestCompositeIndexesExistInProductionSchema(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	for _, want := range specComposite {
		var table string
		err := db.QueryRow(
			`SELECT tbl_name FROM sqlite_master WHERE type = 'index' AND name = ?`,
			want.index,
		).Scan(&table)
		if err != nil {
			t.Errorf("composite index %s is missing from the schema created by CreateSchema; "+
				"SPEC/DATABASE.md § Performance Optimization mandates it: %v", want.index, err)
			continue
		}
		if table != want.table {
			t.Errorf("composite index %s is on table %q, want %q", want.index, table, want.table)
		}
	}
}

// TestCompositeIndexesServeTheProductionQueries asserts that the queries the
// production code actually issues are planned onto the composite indexes that
// exist for them. The SQL is taken from the production query builders, not
// rewritten here, so a query that drifts away from its index fails this test.
func TestCompositeIndexesServeTheProductionQueries(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	seedIndexFixture(t, db)

	status := models.StatusBacklog
	minPriority := 5
	since := "2026-01-01T00:00:00.000Z"
	until := "2026-12-31T23:59:59.000Z"
	auditLimit := models.MaxAuditLimit

	listByStatus := &TaskListFilter{Status: &status, Limit: models.DefaultTaskLimit}
	listByPriority := &TaskListFilter{MinPriority: &minPriority, Limit: models.DefaultTaskLimit}
	auditByDate := &AuditFilter{Since: &since, Until: &until, Limit: auditLimit}

	statusQuery, statusArgs := buildListTasksQuery(listByStatus)
	priorityQuery, priorityArgs := buildListTasksQuery(listByPriority)
	auditQuery, auditArgs := buildAuditEntriesQuery(auditByDate)

	tests := []struct {
		name      string
		query     string
		args      []any
		wantIndex string
		noScanOf  string
	}{
		{
			name:      "task list filtered by status, ordered by priority",
			query:     statusQuery,
			args:      statusArgs,
			wantIndex: "idx_tasks_status_priority",
			noScanOf:  "tasks",
		},
		{
			name:      "task list filtered by priority, ordered by priority and creation date",
			query:     priorityQuery,
			args:      priorityArgs,
			wantIndex: "idx_tasks_priority_created",
			noScanOf:  "tasks",
		},
		{
			name:      "sprint membership lookup",
			query:     sprintTasksLookupQuery,
			args:      []any{1},
			wantIndex: "idx_sprint_tasks_lookup",
			noScanOf:  "sprint_tasks",
		},
		{
			name:      "audit log over a date range, newest first",
			query:     auditQuery,
			args:      auditArgs,
			wantIndex: "idx_audit_date",
			noScanOf:  "audit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := queryPlan(t, db, tt.query, tt.args...)

			if !strings.Contains(plan, tt.wantIndex) {
				t.Errorf("the query does not use %s, the index SPEC/DATABASE.md § Performance Optimization "+
					"creates for it.\nplan: %s\nquery: %s", tt.wantIndex, plan, tt.query)
			}
			// A full scan of the target table means the index is not doing its
			// job, even if the plan mentions it somewhere else (for example in
			// a subquery).
			if strings.Contains(plan, "SCAN "+tt.noScanOf) {
				t.Errorf("the query falls back to a full scan of %s.\nplan: %s", tt.noScanOf, plan)
			}
		})
	}
}

// queryPlan returns the EXPLAIN QUERY PLAN output of query as a single line.
// The bind arguments are the ones production passes: the driver requires them
// even to plan the statement.
func queryPlan(t *testing.T, db *DB, query string, args ...any) string {
	t.Helper()

	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN failed for %q: %v", query, err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scanning query plan: %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString(" | ")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating query plan: %v", err)
	}
	return plan.String()
}

// seedIndexFixture populates the three indexed tables through the production
// write paths, so the planner sees the same shape of data the tool produces.
func seedIndexFixture(t *testing.T, db *DB) {
	t.Helper()
	ctx := testContext()

	sprintID, err := db.CreateSprint(ctx, &models.Sprint{
		Title:       "Audit retention hardening",
		Description: "Retain and index a full year of audit history.",
		Status:      models.SprintPending,
		CreatedAt:   utils.NowISO8601(),
	})
	if err != nil {
		t.Fatalf("creating sprint: %v", err)
	}

	statuses := []models.TaskStatus{models.StatusBacklog, models.StatusCompleted}
	var backlogIDs []int
	for i := range 60 {
		id, err := db.CreateTask(ctx, &models.Task{
			Title:                  fmt.Sprintf("Harden the audit retention policy, part %d", i+1),
			Type:                   models.TypeTask,
			Status:                 statuses[i%len(statuses)],
			Priority:               i % 10,
			Severity:               i % 10,
			FunctionalRequirements: "Operators must be able to audit every mutation for a full year.",
			TechnicalRequirements:  "Retain audit rows for 365 days and index them by performed_at.",
			AcceptanceCriteria:     "An audit query over a one-year window returns in under 50 ms.",
		})
		if err != nil {
			t.Fatalf("creating task %d: %v", i, err)
		}
		if statuses[i%len(statuses)] == models.StatusBacklog {
			backlogIDs = append(backlogIDs, id)
		}
	}

	if err := db.AddTasksToSprint(ctx, sprintID, backlogIDs[:10]); err != nil {
		t.Fatalf("adding tasks to sprint: %v", err)
	}

	// CreateTask and AddTasksToSprint already write audit rows; add a spread of
	// timestamps so the date-range plan has something to range over.
	now := time.Now().UTC()
	for i := range 40 {
		entry := &models.AuditEntry{
			Operation:   string(models.OpTaskUpdate),
			EntityType:  string(models.EntityTask),
			EntityID:    backlogIDs[i%len(backlogIDs)],
			PerformedAt: utils.FormatISO8601(now.AddDate(0, 0, -i)),
		}
		if _, err := db.LogAuditEntry(ctx, entry); err != nil {
			t.Fatalf("logging audit entry %d: %v", i, err)
		}
	}
}
