package db

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== INDEX SET 1.15.0 ====================
//
// The tests in this file settle SPEC/VERSION.md § Migration 1.14.0 → 1.15.0 and
// the index set SPEC/DATABASE.md § DDL - Table Creation declares, on the
// production DDL, the production migration and the production query builders.

// indexedTables are the four tables whose index set schema 1.15.0 replaced.
var indexedTables = []string{"tasks", "sprint_tasks", "audit", "task_dependencies"}

// wantIndexSet1150 is the index set of those four tables, table by table, as
// "name(key columns)" with DESC marking a descending key and the implicit
// constraint indexes named by their origin. It is transcribed from
// SPEC/DATABASE.md § DDL - Table Creation.
var wantIndexSet1150 = map[string][]string{
	"tasks": {
		"idx_tasks_created_at(created_at)",
		"idx_tasks_parent_task_id(parent_task_id)",
		"idx_tasks_priority_created(priority DESC, created_at)",
		"idx_tasks_severity_priority(severity DESC, priority DESC, created_at)",
		"idx_tasks_status_priority(status, priority DESC, created_at)",
		"idx_tasks_type(type, priority DESC, created_at)",
	},
	"sprint_tasks": {
		"idx_sprint_tasks_order UNIQUE(sprint_id, position)",
		"pk(sprint_id, task_id)",
		"u(task_id)",
	},
	"audit": {
		"idx_audit_date(performed_at DESC)",
		"idx_audit_entity(entity_type, entity_id, performed_at DESC)",
		"idx_audit_operation(operation, performed_at DESC, entity_type)",
	},
	"task_dependencies": {
		"idx_task_deps_depends_on(depends_on_task_id)",
		"pk(task_id, depends_on_task_id)",
	},
}

// droppedIn1150 are the six indexes migration 1.15.0 drops for good.
var droppedIn1150 = []string{
	"idx_tasks_status", "idx_tasks_priority", "idx_sprint_tasks_task_id",
	"idx_sprint_tasks_lookup", "idx_task_deps_task_id", "idx_audit_performed_at",
}

// indexSetOf renders the index set of table from PRAGMA index_list and PRAGMA
// index_xinfo: one entry per index, sorted, each naming its key columns in
// order with their direction. An implicit constraint index is named by its
// origin ("pk" or "u") rather than by its sqlite_autoindex name, so the rendering
// does not depend on the order the constraints were declared in.
func indexSetOf(t *testing.T, sqlDB *sql.DB, table string) []string {
	t.Helper()

	type listed struct {
		name   string
		origin string
		unique bool
	}
	rows, err := sqlDB.Query(`SELECT name, "unique", origin FROM pragma_index_list(?)`, table)
	if err != nil {
		t.Fatalf("listing the indexes of %s: %v", table, err)
	}
	var indexes []listed
	for rows.Next() {
		var l listed
		if err := rows.Scan(&l.name, &l.unique, &l.origin); err != nil {
			t.Fatalf("scanning an index of %s: %v", table, err)
		}
		indexes = append(indexes, l)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the indexes of %s: %v", table, err)
	}
	rows.Close()

	out := make([]string, 0, len(indexes))
	for _, l := range indexes {
		colRows, err := sqlDB.Query(
			`SELECT IFNULL(name, ''), "desc" FROM pragma_index_xinfo(?) WHERE key = 1 ORDER BY seqno`, l.name)
		if err != nil {
			t.Fatalf("reading the columns of %s: %v", l.name, err)
		}
		var cols []string
		for colRows.Next() {
			var name string
			var desc bool
			if err := colRows.Scan(&name, &desc); err != nil {
				t.Fatalf("scanning a column of %s: %v", l.name, err)
			}
			if desc {
				name += " DESC"
			}
			cols = append(cols, name)
		}
		if err := colRows.Err(); err != nil {
			t.Fatalf("iterating the columns of %s: %v", l.name, err)
		}
		colRows.Close()

		label := l.name
		switch {
		case l.origin == "pk" || l.origin == "u":
			label = l.origin
		case l.unique:
			label += " UNIQUE"
		}
		out = append(out, label+"("+strings.Join(cols, ", ")+")")
	}
	slices.Sort(out)
	return out
}

// openScratchDB opens an on-disk database with the production DSN and no
// schema, for tests that need to build a historical schema themselves.
func openScratchDB(t *testing.T) *DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", dsnFor(filepath.Join(t.TempDir(), "project.db"), false))
	if err != nil {
		t.Fatalf("opening the scratch database: %v", err)
	}
	database := &DB{DB: sqlDB, queryCache: NewQueryCache(), batchProc: NewBatchProcessor(100)}
	t.Cleanup(func() { database.Close() })
	return database
}

// indexSet1140 is the index DDL schema 1.14.0 declared for the four tables,
// transcribed from the schema that shipped at that version. Applied to a fresh
// 1.15.0 database after downgradeTo1140 has dropped the 1.15.0 definitions, it
// reproduces the 1.14.0 index set exactly.
var indexSet1140 = []string{
	`DROP INDEX idx_tasks_status_priority`,
	`DROP INDEX idx_tasks_type`,
	`DROP INDEX idx_tasks_severity_priority`,
	`DROP INDEX idx_audit_entity`,
	`DROP INDEX idx_audit_operation`,
	`CREATE INDEX idx_tasks_status ON tasks(status)`,
	`CREATE INDEX idx_tasks_type ON tasks(type)`,
	`CREATE INDEX idx_tasks_priority ON tasks(priority)`,
	`CREATE INDEX idx_tasks_status_priority ON tasks(status, priority DESC)`,
	`CREATE INDEX idx_sprint_tasks_task_id ON sprint_tasks(task_id)`,
	`CREATE INDEX idx_sprint_tasks_lookup ON sprint_tasks(sprint_id, task_id)`,
	`CREATE INDEX idx_audit_entity ON audit(entity_type, entity_id)`,
	`CREATE INDEX idx_audit_operation ON audit(operation)`,
	`CREATE INDEX idx_audit_performed_at ON audit(performed_at)`,
	`CREATE INDEX idx_task_deps_task_id ON task_dependencies(task_id)`,
	`UPDATE _metadata SET value = '1.14.0' WHERE key = 'schema_version'`,
}

// buildRoadmapAtSchema1140 returns a database holding the 1.14.0 schema and a
// populated roadmap whose rows tie on every ordering key the listings use, so
// that a change in the order ties are returned in cannot go unnoticed.
func buildRoadmapAtSchema1140(t *testing.T) *DB {
	t.Helper()
	database := openScratchDB(t)
	if err := database.CreateSchema(); err != nil {
		t.Fatalf("creating the schema: %v", err)
	}
	for _, stmt := range indexSet1140 {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatalf("building the 1.14.0 index set (%s): %v", stmt, err)
		}
	}

	// Tasks tie in groups on priority, severity and created_at, and spread over
	// two statuses and two types, so every listing ordering has ties to break.
	const created = "2026-03-02T09:00:00.000Z"
	statuses := []models.TaskStatus{models.StatusBacklog, models.StatusBacklog, models.StatusCompleted}
	types := []models.TaskType{models.TypeBug, models.TypeTask}
	ids := make([]int, 0, 45)
	for i := range 45 {
		ids = append(ids, mustSeedTask(t, database, &models.Task{
			Title:                  fmt.Sprintf("Reconcile the settlement ledger for merchant batch %d", i+1),
			Status:                 statuses[i%len(statuses)],
			Type:                   types[i%len(types)],
			Priority:               (i / 9) % 3,
			Severity:               (i / 5) % 3,
			FunctionalRequirements: "Every settled batch must balance against the acquirer statement.",
			TechnicalRequirements:  "Compare the ledger totals with the statement totals per batch.",
			AcceptanceCriteria:     "An unbalanced batch is reported with its difference.",
			CreatedAt:              created,
		}))
	}
	sprintID := mustSeedSprint(t, database, &models.Sprint{
		Title:       "Settlement reconciliation",
		Description: "Balance every settled batch against the acquirer statement.",
		Status:      models.SprintPending,
		CreatedAt:   created,
	})
	// A COMPLETED task cannot join a sprint, so the members seeded COMPLETED
	// are returned to BACKLOG first; the addition then sets all six to SPRINT,
	// which is the state this fixture has always carried.
	for _, id := range ids[:6] {
		if _, err := database.Exec(`UPDATE tasks SET status = 'BACKLOG' WHERE id = ?`, id); err != nil {
			t.Fatalf("returning task %d to BACKLOG: %v", id, err)
		}
	}
	if err := database.AddTasksToSprint(testContext(), sprintID, ids[:6]); err != nil {
		t.Fatalf("adding tasks to the sprint: %v", err)
	}
	if err := database.AddTaskDependencyWithAudit(testContext(), ids[7], ids[8]); err != nil {
		t.Fatalf("adding a dependency: %v", err)
	}
	// Audit rows tying on performed_at, for one entity and one operation.
	for range 12 {
		seedAuditEntry(t, database, &models.AuditEntry{
			Operation:   string(models.OpTaskUpdate),
			EntityType:  string(models.EntityTask),
			EntityID:    ids[3],
			PerformedAt: "2026-03-03T10:00:00.000Z",
		})
	}
	return database
}

// tableContents renders every row of every table, in rowid order, as one
// string, so two calls differ if and only if some row changed.
func tableContents(t *testing.T, database *DB) string {
	t.Helper()
	var out strings.Builder
	for _, table := range []string{
		"tasks", "sprints", "sprint_tasks", "audit", "task_dependencies",
		"task_comments", "sprint_comments", "_metadata",
	} {
		// #nosec G202 -- the table names are the fixed literals above
		rows, err := database.Query("SELECT * FROM " + table + " ORDER BY rowid")
		if err != nil {
			t.Fatalf("reading %s: %v", table, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatalf("reading the columns of %s: %v", table, err)
		}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatalf("scanning %s: %v", table, err)
			}
			fmt.Fprintf(&out, "%s %v\n", table, vals)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterating %s: %v", table, err)
		}
		rows.Close()
	}
	return out.String()
}

// listingOrders runs every task listing and every audit read whose index
// migration 1.15.0 replaced, through the production readers, and renders the
// ids each returns in order.
func listingOrders(t *testing.T, database *DB) map[string][]int {
	t.Helper()
	ctx := testContext()
	backlog := models.StatusBacklog
	bug := models.TypeBug
	out := map[string][]int{}
	listings := map[string]*TaskListFilter{
		"status filter":     {Status: &backlog, Limit: models.MaxTaskLimit},
		"type filter":       {TaskType: &bug, Limit: models.MaxTaskLimit},
		"status ordering":   {Sort: "status", Limit: models.MaxTaskLimit},
		"severity ordering": {Sort: "severity", Limit: models.MaxTaskLimit},
		"default ordering":  {Limit: models.MaxTaskLimit},
	}
	for name, f := range listings {
		tasks, err := database.ListTasks(ctx, f)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for i := range tasks {
			out[name] = append(out[name], tasks[i].ID)
		}
	}

	op := string(models.OpTaskUpdate)
	entries, err := database.GetAuditEntries(ctx, &AuditFilter{Operation: &op, Limit: models.MaxAuditLimit})
	if err != nil {
		t.Fatalf("audit by operation: %v", err)
	}
	for _, e := range entries {
		out["audit operation filter"] = append(out["audit operation filter"], e.ID)
	}
	var taskID int
	if err := database.QueryRow(`SELECT entity_id FROM audit WHERE operation = ? LIMIT 1`, op).Scan(&taskID); err != nil {
		t.Fatalf("reading the audited task: %v", err)
	}
	history, err := database.GetEntityHistory(ctx, string(models.EntityTask), taskID)
	if err != nil {
		t.Fatalf("entity history: %v", err)
	}
	for _, e := range history {
		out["entity history"] = append(out["entity history"], e.ID)
	}
	return out
}

// TestFreshSchemaHoldsTheSpecifiedIndexSet pins the index set a fresh database
// is created with, table by table, including column order and direction.
func TestFreshSchemaHoldsTheSpecifiedIndexSet(t *testing.T) {
	database := openScratchDB(t)
	if err := database.CreateSchema(); err != nil {
		t.Fatalf("creating the schema: %v", err)
	}
	for _, table := range indexedTables {
		if got, want := indexSetOf(t, database.DB, table), wantIndexSet1150[table]; !slices.Equal(got, want) {
			t.Errorf("a fresh %s carries the indexes\n  %v\nwant exactly\n  %v\n(SPEC/DATABASE.md § DDL - Table Creation)",
				table, got, want)
		}
	}
}

// TestMigrateV1_14_0_toV1_15_0 settles the acceptance criteria of
// SPEC/VERSION.md § Migration 1.14.0 → 1.15.0 on a populated 1.14.0 roadmap.
func TestMigrateV1_14_0_toV1_15_0(t *testing.T) {
	database := buildRoadmapAtSchema1140(t)

	// The fixture really is at 1.14.0, so the migration has work to do.
	if got := indexSetOf(t, database.DB, "tasks"); slices.Equal(got, wantIndexSet1150["tasks"]) {
		t.Fatal("the 1.14.0 fixture already carries the 1.15.0 task indexes; it tests nothing")
	}
	rowsBefore := tableContents(t, database)
	ordersBefore := listingOrders(t, database)

	if err := database.RunMigrations(); err != nil {
		t.Fatalf("running the migrations: %v", err)
	}

	// Criterion 8: the version.
	version, err := database.GetSchemaVersion()
	if err != nil {
		t.Fatalf("reading the schema version: %v", err)
	}
	// The migration set runs through 1.15.0 and on to the current version.
	if version != "1.16.0" {
		t.Fatalf("schema_version = %q after the migration, want 1.16.0", version)
	}

	// Criterion 1: the same index set, columns and directions as a fresh database.
	fresh := openScratchDB(t)
	if err := fresh.CreateSchema(); err != nil {
		t.Fatalf("creating the fresh schema: %v", err)
	}
	for _, table := range indexedTables {
		got, want := indexSetOf(t, database.DB, table), indexSetOf(t, fresh.DB, table)
		if !slices.Equal(got, want) {
			t.Errorf("after the migration %s carries\n  %v\na fresh database carries\n  %v", table, got, want)
		}
	}

	// Criterion 2: the six dropped indexes are gone.
	for _, name := range droppedIn1150 {
		var n int
		if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&n); err != nil {
			t.Fatalf("looking up %s: %v", name, err)
		}
		if n != 0 {
			t.Errorf("index %s survives the migration", name)
		}
	}

	// Criterion 3: no row changed, apart from the version the migration records.
	rowsAfter := tableContents(t, database)
	if strings.Replace(rowsBefore, "1.14.0", "1.16.0", 1) != rowsAfter {
		t.Errorf("the migration changed rows other than schema_version.\nbefore:\n%s\nafter:\n%s", rowsBefore, rowsAfter)
	}

	// Criterion 5: every listing and audit read returns the same rows in the same
	// order, ties included.
	ordersAfter := listingOrders(t, database)
	for name, before := range ordersBefore {
		if len(before) == 0 {
			t.Fatalf("%s returns no row; the fixture exercises nothing", name)
		}
		if !slices.Equal(before, ordersAfter[name]) {
			t.Errorf("%s returns a different order after the migration:\n  before %v\n  after  %v",
				name, before, ordersAfter[name])
		}
	}

	// Criterion 6: running the migration set again is a no-op and raises nothing.
	indexesOnce := map[string][]string{}
	for _, table := range indexedTables {
		indexesOnce[table] = indexSetOf(t, database.DB, table)
	}
	if err := database.runMigration(Migration{Version: "1.15.0", Name: "re-apply", Apply: migrateV1_14_0_toV1_15_0}); err != nil {
		t.Fatalf("re-applying the migration: %v", err)
	}
	if err := database.RunMigrations(); err != nil {
		t.Fatalf("running the migration set a second time: %v", err)
	}
	for _, table := range indexedTables {
		if got := indexSetOf(t, database.DB, table); !slices.Equal(got, indexesOnce[table]) {
			t.Errorf("re-applying the migration changed the %s indexes: %v, then %v", table, indexesOnce[table], got)
		}
	}
	if got := tableContents(t, database); got != rowsAfter {
		t.Error("re-applying the migration changed rows")
	}
}

// TestMigrateV1_14_0_toV1_15_0_FailureRollsBack settles criterion 7: if any step
// fails, the version stays at 1.14.0 and the index set is the one the database
// held before. The failure is injected after every statement of the migration
// has run, inside the same migration transaction, which is the latest point a
// step can fail.
func TestMigrateV1_14_0_toV1_15_0_FailureRollsBack(t *testing.T) {
	database := buildRoadmapAtSchema1140(t)

	before := map[string][]string{}
	for _, table := range indexedTables {
		before[table] = indexSetOf(t, database.DB, table)
	}

	injected := errors.New("injected failure after the last step")
	err := database.runMigration(Migration{
		Version: "1.15.0",
		Name:    "fails after its last step",
		Apply: func(tx *sql.Tx) error {
			if err := migrateV1_14_0_toV1_15_0(tx); err != nil {
				return err
			}
			return injected
		},
	})
	if !errors.Is(err, injected) {
		t.Fatalf("runMigration returned %v, want the injected failure", err)
	}

	version, err := database.GetSchemaVersion()
	if err != nil {
		t.Fatalf("reading the schema version: %v", err)
	}
	if version != "1.14.0" {
		t.Errorf("schema_version = %q after a failed migration, want 1.14.0", version)
	}
	for _, table := range indexedTables {
		if got := indexSetOf(t, database.DB, table); !slices.Equal(got, before[table]) {
			t.Errorf("a failed migration left %s with\n  %v\nwant the indexes it held before\n  %v", table, got, before[table])
		}
	}
}

// topLevelPlan returns the EXPLAIN QUERY PLAN rows of query that belong to the
// outermost statement (parent 0), so a sort step inside a correlated subquery —
// the dependency columns of the task listing carry one — is not mistaken for a
// sort of the listing itself.
func topLevelPlan(t *testing.T, database *DB, query string, args ...any) []string {
	t.Helper()
	rows, err := database.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN failed for %q: %v", query, err)
	}
	defer rows.Close()

	var top []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scanning query plan: %v", err)
		}
		if parent == 0 {
			top = append(top, detail)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating query plan: %v", err)
	}
	return top
}

// TestListingAndAuditReadsNeedNoSortStep settles criterion 4 of the migration
// and the claims of SPEC/DATABASE.md § Index Design Rationale: each read is
// planned on the index the SPEC assigns to it, never scans the table, and needs
// no sort step. The SQL comes from the production builders.
func TestListingAndAuditReadsNeedNoSortStep(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	seedIndexFixture(t, database)

	status := models.StatusBacklog
	taskType := models.TypeBug
	operation := string(models.OpTaskCreate)
	entityType := string(models.EntityTask)
	entityID := 3

	type read struct {
		name      string
		query     string
		args      []any
		wantIndex string
		table     string // the plan's name for the table read
	}
	reads := make([]read, 0, 7)
	for _, l := range []struct {
		name   string
		filter *TaskListFilter
		index  string
	}{
		{"task listing filtered by status", &TaskListFilter{Status: &status, Limit: models.DefaultTaskLimit}, "idx_tasks_status_priority"},
		{"task listing filtered by type", &TaskListFilter{TaskType: &taskType, Limit: models.DefaultTaskLimit}, "idx_tasks_type"},
		{"task listing sorted by status", &TaskListFilter{Sort: "status", Limit: models.DefaultTaskLimit}, "idx_tasks_status_priority"},
		{"task listing sorted by severity", &TaskListFilter{Sort: "severity", Limit: models.DefaultTaskLimit}, "idx_tasks_severity_priority"},
	} {
		q, a := buildListTasksQuery(l.filter)
		reads = append(reads, read{l.name, q, a, l.index, "t"})
	}
	q, a := buildAuditEntriesQuery(&AuditFilter{Operation: &operation, Limit: models.MaxAuditLimit})
	reads = append(reads, read{"audit filtered by operation", q, a, "idx_audit_operation", "audit"})
	q, a = buildAuditEntriesQuery(&AuditFilter{Operation: &operation, EntityType: &entityType, Limit: models.MaxAuditLimit})
	reads = append(reads, read{"audit filtered by operation and entity type", q, a, "idx_audit_operation", "audit"})
	q, a = buildAuditEntriesQuery(&AuditFilter{EntityType: &entityType, EntityID: &entityID, Limit: models.MaxAuditLimit})
	reads = append(reads, read{"entity history", q, a, "idx_audit_entity", "audit"})

	for _, r := range reads {
		t.Run(r.name, func(t *testing.T) {
			top := topLevelPlan(t, database, r.query, r.args...)
			plan := strings.Join(top, " | ")
			if len(top) == 0 || !strings.Contains(top[0], "INDEX "+r.wantIndex+" ") &&
				!strings.HasSuffix(top[0], "INDEX "+r.wantIndex) {
				t.Errorf("the read is not planned on %s.\nplan: %s\nquery: %s", r.wantIndex, plan, r.query)
			}
			for _, step := range top {
				if strings.Contains(step, "TEMP B-TREE") {
					t.Errorf("the read sorts in a temporary B-tree; %s must supply its order.\nplan: %s",
						r.wantIndex, plan)
				}
				if step == "SCAN "+r.table {
					t.Errorf("the read scans the table.\nplan: %s", plan)
				}
			}
		})
	}
}

// TestSprintCompletionCountsDriveFromSprintTasks settles SPEC/DATABASE.md § Join
// Order of the Sprint Completion Counts: the velocity count and the member read
// of `sprint stats` each search sprint_tasks by sprint_id first — the velocity
// count through the primary-key index, the member read through
// idx_sprint_tasks_order — then look each member task up by its primary key, and
// each fixes that order with CROSS JOIN.
func TestSprintCompletionCountsDriveFromSprintTasks(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	seedIndexFixture(t, database)

	for name, c := range map[string]struct {
		query  string
		search string
		args   []any
	}{
		"average velocity": {averageVelocityQuery,
			"SEARCH st USING COVERING INDEX " + constraintIndexOf(t, database, "sprint_tasks", "pk") + " (sprint_id=?)", []any{5}},
		"member read": {sprintTaskStatesQuery, "SEARCH st USING INDEX idx_sprint_tasks_order (sprint_id=?)", []any{1}},
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(c.query, "FROM sprint_tasks st CROSS JOIN tasks t") {
				t.Errorf("the statement does not fix the join order with "+
					"`sprint_tasks st CROSS JOIN tasks t`:\n%s", c.query)
			}
			plan := queryPlan(t, database, c.query, c.args...)
			st := strings.Index(plan, c.search)
			tk := strings.Index(plan, "SEARCH t USING INTEGER PRIMARY KEY (rowid=?)")
			if st < 0 || tk < 0 || st > tk {
				t.Errorf("the read does not search sprint_tasks by sprint_id and then tasks by primary key.\nplan: %s", plan)
			}
			if strings.Contains(plan, "idx_tasks_status_priority") {
				t.Errorf("the read walks the tasks-by-status index.\nplan: %s", plan)
			}
		})
	}
}

// TestSprintCompletionCountsAreUnchanged checks the values the two counts
// return on a sprint with completed and open members, so pinning the join order
// cannot have changed a result.
func TestSprintCompletionCountsAreUnchanged(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := testContext()

	ids := make([]int, 0, 6)
	for i := range 6 {
		ids = append(ids, mustSeedTask(t, database, &models.Task{
			Title:                  fmt.Sprintf("Rotate the signing key of region %d", i+1),
			Status:                 models.StatusBacklog,
			FunctionalRequirements: "Every region signs with a key younger than ninety days.",
			TechnicalRequirements:  "Rotate through the key service and publish the new public key.",
			AcceptanceCriteria:     "No region presents a key older than ninety days.",
			CreatedAt:              utils.NowISO8601(),
		}))
	}
	sprintID := mustSeedSprint(t, database, &models.Sprint{
		Title: "Key rotation", Description: "Rotate every regional signing key.",
		Status: models.SprintPending, CreatedAt: utils.NowISO8601(),
	})
	if err := database.AddTasksToSprint(ctx, sprintID, ids[:5]); err != nil {
		t.Fatalf("adding tasks: %v", err)
	}
	// Three members completed on two days; one completed task outside the sprint.
	for i, closed := range []string{"2026-04-02T10:00:00.000Z", "2026-04-02T15:00:00.000Z", "2026-04-04T09:00:00.000Z"} {
		if _, err := database.Exec(`UPDATE tasks SET status = 'COMPLETED', closed_at = ? WHERE id = ?`, closed, ids[i]); err != nil {
			t.Fatalf("completing task: %v", err)
		}
	}
	if _, err := database.Exec(`UPDATE tasks SET status = 'COMPLETED', closed_at = ? WHERE id = ?`, "2026-04-03T10:00:00.000Z", ids[5]); err != nil {
		t.Fatalf("completing the outside task: %v", err)
	}
	if _, err := database.Exec(`UPDATE sprints SET status = 'CLOSED', started_at = ?, closed_at = ? WHERE id = ?`,
		"2026-04-01T08:00:00.000Z", "2026-04-11T08:00:00.000Z", sprintID); err != nil {
		t.Fatalf("closing the sprint: %v", err)
	}

	// The burndown is derived from the member read (SPEC/DATABASE.md § Join Order
	// of the Sprint Completion Counts).
	sprint, err := database.GetSprint(ctx, sprintID)
	if err != nil {
		t.Fatalf("reading the sprint: %v", err)
	}
	members, err := database.GetSprintTaskStates(ctx, sprintID)
	if err != nil {
		t.Fatalf("member read: %v", err)
	}
	tasks := make([]models.Task, len(members))
	for i := range members {
		tasks[i] = models.Task{ID: members[i].ID, Status: members[i].Status}
		if members[i].ClosedAt.Valid {
			tasks[i].ClosedAt = &members[i].ClosedAt.String
		}
	}
	burndown := models.CalculateSprintBurndown(sprint, tasks)
	want := []models.BurndownEntry{
		{Date: "2026-04-01", TasksRemaining: 5},
		{Date: "2026-04-02", TasksRemaining: 3},
		{Date: "2026-04-04", TasksRemaining: 2},
	}
	if !slices.Equal(burndown, want) {
		t.Errorf("burndown = %v, want %v", burndown, want)
	}

	velocity, err := database.GetAverageVelocity(ctx, 5)
	if err != nil {
		t.Fatalf("velocity: %v", err)
	}
	if velocity != 0.3 {
		t.Errorf("average velocity = %v, want 0.3 (3 completed members over 10 days)", velocity)
	}
}
