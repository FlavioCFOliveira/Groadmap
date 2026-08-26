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
	{"idx_task_comments_task_created", "task_comments"},
	{"idx_sprint_comments_sprint_created", "sprint_comments"},
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
	fixture := seedIndexFixture(t, db)

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

	// The comment listings and the grouped comment read come from the production
	// statement sets in comments.go, and the grouped form is planned with the same
	// placeholder count production builds for three parents.
	commentedTasks := fixture.commentedTaskIDs
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
		{
			name:      "task comment listing of one task, oldest first",
			query:     taskCommentStmts.selectByParent,
			args:      []any{commentedTasks[0]},
			wantIndex: "idx_task_comments_task_created",
			noScanOf:  "task_comments",
		},
		{
			name:      "task comment listing of one task, filtered by type",
			query:     taskCommentStmts.selectByParentAndType,
			args:      []any{commentedTasks[0], string(models.CommentProgress)},
			wantIndex: "idx_task_comments_task_created",
			noScanOf:  "task_comments",
		},
		{
			name:      "sprint comment listing of one sprint, oldest first",
			query:     sprintCommentStmts.selectByParent,
			args:      []any{fixture.sprintID},
			wantIndex: "idx_sprint_comments_sprint_created",
			noScanOf:  "sprint_comments",
		},
		{
			name:      "sprint comment listing of one sprint, filtered by type",
			query:     sprintCommentStmts.selectByParentAndType,
			args:      []any{fixture.sprintID, string(models.CommentProgress)},
			wantIndex: "idx_sprint_comments_sprint_created",
			noScanOf:  "sprint_comments",
		},
		{
			// SPEC/DATABASE.md § Index Design Rationale states that the same
			// index serves the grouped WHERE task_id IN (...) read the web
			// interface uses, and that the count reads no body at all. Both
			// claims are asserted here, not assumed (SPEC/DATABASE.md § Count
			// Comments for Many Parents (Grouped), Index).
			name:      "grouped task comment COUNT over three tasks",
			query:     groupedTaskCommentCountsQuery(db.Placeholders(3)),
			args:      []any{commentedTasks[0], commentedTasks[1], commentedTasks[2]},
			wantIndex: "idx_task_comments_task_created",
			noScanOf:  "task_comments",
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
			// A comment listing is ordered by the index's own trailing column
			// (created_at, then the implicit rowid, which IS the comment id), so
			// the engine must supply the order rather than sort the result. A
			// TEMP B-TREE here would mean the composite index shape earns nothing
			// over a plain index on the parent key.
			if strings.HasSuffix(tt.noScanOf, "_comments") && strings.Contains(plan, "TEMP B-TREE") {
				t.Errorf("the comment listing sorts in a temporary B-tree; the index must supply the "+
					"created_at order.\nplan: %s\nquery: %s", plan, tt.query)
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

// indexFixtureIDs names the rows seedIndexFixture created, so the plan
// assertions bind the ids that actually exist rather than assuming an
// autoincrement sequence.
type indexFixtureIDs struct {
	commentedTaskIDs []int
	sprintID         int
}

// seedIndexFixture populates the indexed tables through the production write
// paths, so the planner sees the same shape of data the tool produces.
func seedIndexFixture(t *testing.T, db *DB) indexFixtureIDs {
	t.Helper()
	ctx := testContext()

	sprintID, err := seedSprint(db, &models.Sprint{
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
		id, err := seedTask(db, &models.Task{
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

	// AddTasksToSprint already writes audit rows; add a spread of timestamps so
	// the date-range plan has something to range over.
	now := time.Now().UTC()
	for i := range 40 {
		entry := &models.AuditEntry{
			Operation:   string(models.OpTaskUpdate),
			EntityType:  string(models.EntityTask),
			EntityID:    backlogIDs[i%len(backlogIDs)],
			PerformedAt: utils.FormatISO8601(now.AddDate(0, 0, -i)),
		}
		seedAuditEntry(t, db, entry)
	}

	// Comments on several tasks and on the sprint, written through the production
	// transactional path, so the comment listings and the grouped read are planned
	// against populated tables. Three tasks carry comments: the grouped read is
	// planned over an IN list of three real parents.
	commented := backlogIDs[:3]
	for _, taskID := range commented {
		for entry := range 8 {
			addTaskComment(t, db, taskID, models.CommentProgress,
				"Audit retention now holds a full year of history for this window.",
				utils.FormatISO8601(now.AddDate(0, 0, -entry)))
		}
	}
	for entry := range 8 {
		addSprintComment(t, db, sprintID, models.CommentProgress,
			"Retention is verified for the windows closed so far.",
			utils.FormatISO8601(now.AddDate(0, 0, -entry)))
	}

	return indexFixtureIDs{commentedTaskIDs: commented, sprintID: sprintID}
}

// TestGroupedSprintResolutionNeedsNoNewIndex asserts the claim SPEC/DATABASE.md
// § Resolve the Sprint of Many Tasks (Grouped) makes about the grouped sprint
// read: it needs no index of its own, because the `WHERE st.task_id IN (...)`
// lookup is already served by an index on sprint_tasks.task_id and the join
// resolves sprints by primary key.
//
// The assertion is deliberately not tied to one index NAME: sprint_tasks.task_id
// carries a UNIQUE constraint, for which SQLite creates an implicit index, and
// the DDL also declares idx_sprint_tasks_task_id on the same column. The planner
// is free to take either, and the SPEC names both. What must hold is that it
// takes ONE of them rather than scanning the table.
func TestGroupedSprintResolutionNeedsNoNewIndex(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	fixture := seedIndexFixture(t, db)

	// The sprint's first three member tasks: real ids, so the planner sees the
	// IN list production sends.
	memberIDs, err := db.GetSprintTasks(testContext(), fixture.sprintID)
	if err != nil {
		t.Fatalf("reading the sprint's member tasks: %v", err)
	}
	if len(memberIDs) < 3 {
		t.Fatalf("the fixture sprint has %d member tasks, want at least 3", len(memberIDs))
	}
	args := []any{memberIDs[0], memberIDs[1], memberIDs[2]}

	plan := queryPlan(t, db, groupedTaskSprintsQuery(db.Placeholders(3)), args...)

	// The membership lookup is served by an index on task_id, whichever of the two
	// the planner picks.
	usesTaskIDIndex := strings.Contains(plan, "idx_sprint_tasks_task_id") ||
		strings.Contains(plan, "sqlite_autoindex_sprint_tasks_1")
	if !usesTaskIDIndex {
		t.Errorf("the grouped sprint read is not served by an index on sprint_tasks.task_id.\nplan: %s", plan)
	}
	if strings.Contains(plan, "SCAN sprint_tasks") || strings.Contains(plan, "SCAN st") {
		t.Errorf("the grouped sprint read falls back to a full scan of sprint_tasks.\nplan: %s", plan)
	}
	// The join reaches the sprint by its primary key, so widening the id set costs
	// one key lookup per task and never a scan of sprints.
	if !strings.Contains(plan, "INTEGER PRIMARY KEY") {
		t.Errorf("the grouped sprint read does not resolve sprints by primary key.\nplan: %s", plan)
	}
	if strings.Contains(plan, "SCAN sprints") || strings.Contains(plan, "SCAN s") {
		t.Errorf("the grouped sprint read falls back to a full scan of sprints.\nplan: %s", plan)
	}
	// At most one row per task means no sort is needed for the task_id ordering:
	// a temporary B-tree here would mean the index earns nothing.
	if strings.Contains(plan, "TEMP B-TREE") {
		t.Errorf("the grouped sprint read sorts in a temporary B-tree; the index must supply "+
			"the task_id order.\nplan: %s", plan)
	}

	// "No new index" is the other half of the claim: the sprint_tasks index set is
	// exactly the one the DDL declared before this read existed.
	rows, err := db.Query(`SELECT name FROM pragma_index_list('sprint_tasks') ORDER BY name`)
	if err != nil {
		t.Fatalf("listing the indexes of sprint_tasks: %v", err)
	}
	defer rows.Close()

	var indexes []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning index name: %v", err)
		}
		indexes = append(indexes, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating index names: %v", err)
	}

	want := []string{
		"idx_sprint_tasks_lookup",
		"idx_sprint_tasks_order",
		"idx_sprint_tasks_task_id",
		"sqlite_autoindex_sprint_tasks_1", // the UNIQUE constraint on task_id
		"sqlite_autoindex_sprint_tasks_2", // the (sprint_id, task_id) primary key
	}
	if strings.Join(indexes, ",") != strings.Join(want, ",") {
		t.Errorf("sprint_tasks carries the indexes %v, want exactly %v; the grouped sprint read "+
			"adds none (SPEC/DATABASE.md § Resolve the Sprint of Many Tasks (Grouped), Index)",
			indexes, want)
	}
}

// TestGroupedSprintMembershipReadIsCoveredByTheLookupIndex asserts the claim
// SPEC/DATABASE.md § Read the Membership of Many Sprints (Grouped) makes about
// the read that resolves the membership of every sprint the listing returns:
// idx_sprint_tasks_lookup covers it exactly — its columns are (sprint_id,
// task_id), the leading column serving the IN lookup and the pair serving the
// ordering — so the statement needs no sort step and reads no table row.
//
// The SQL is the production builder's, planned with the placeholder count
// production builds for the ids it is given, so a statement that drifts away from
// its index fails here rather than in a benchmark nobody runs.
func TestGroupedSprintMembershipReadIsCoveredByTheLookupIndex(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	fixture := seedSprintMembershipFixture(t, db)

	args := make([]any, len(fixture.sprintIDs))
	for i, id := range fixture.sprintIDs {
		args[i] = id
	}
	plan := queryPlan(t, db, groupedSprintMembershipQuery(db.Placeholders(len(args))), args...)

	// A COVERING index search is the whole claim: the pair (sprint_id, task_id)
	// is everything the statement selects, so it answers from the index and
	// touches no sprint_tasks row. A plain "SEARCH ... USING INDEX" would mean
	// every matched row is fetched from the table for a value the index already
	// holds.
	if !strings.Contains(plan, "COVERING INDEX idx_sprint_tasks_lookup") {
		t.Errorf("the grouped membership read is not served as a covering index search on "+
			"idx_sprint_tasks_lookup.\nplan: %s", plan)
	}
	if strings.Contains(plan, "SCAN sprint_tasks") {
		t.Errorf("the grouped membership read falls back to a full scan of sprint_tasks.\nplan: %s", plan)
	}
	// The index supplies both ordering columns in the order the statement asks
	// for, so there is no sort step. A temporary B-tree here would mean the
	// index shape earns nothing over one on sprint_id alone.
	if strings.Contains(plan, "TEMP B-TREE") {
		t.Errorf("the grouped membership read sorts in a temporary B-tree; the index must supply "+
			"the (sprint_id, task_id) order.\nplan: %s", plan)
	}
	// It reads sprint_tasks and nothing else: the answer is a set of ids, so no
	// tasks row and no sprints row is fetched to produce it.
	for _, table := range []string{"tasks", "sprints"} {
		if strings.Contains(plan, " "+table+" ") {
			t.Errorf("the grouped membership read touches %s; it must read sprint_tasks alone.\nplan: %s",
				table, plan)
		}
	}
}
