package db

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for two statistics reads of SPEC/DATABASE.md: the
// average-velocity read of § Join Order of the Sprint Completion Counts, which
// limits before it counts, and the four statements of § Audit Statistics.

// planNode is one row of EXPLAIN QUERY PLAN.
type planNode struct {
	detail     string
	id, parent int
}

// planTree returns the EXPLAIN QUERY PLAN rows of query, in plan order.
func planTree(t *testing.T, database *DB, query string, args ...any) []planNode {
	t.Helper()
	rows, err := database.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN failed for %q: %v", query, err)
	}
	defer rows.Close()

	var nodes []planNode
	for rows.Next() {
		var n planNode
		var notUsed int
		if err := rows.Scan(&n.id, &n.parent, &notUsed, &n.detail); err != nil {
			t.Fatalf("scanning query plan: %v", err)
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating query plan: %v", err)
	}
	return nodes
}

// underNode reports whether the node with id descends from the node with
// ancestor.
func underNode(nodes []planNode, id, ancestor int) bool {
	parents := make(map[int]int, len(nodes))
	for _, n := range nodes {
		parents[n.id] = n.parent
	}
	for id != 0 {
		if id == ancestor {
			return true
		}
		id = parents[id]
	}
	return false
}

// TestAverageVelocityCountsOnlyTheLimitedSprints settles "The velocity read
// limits before it counts": the LIMIT belongs to the inner statement that
// selects the sprints, the outer statement carries none, and the plan runs the
// correlated completed count outside the subquery that selects and limits the
// sprints — once per sprint that subquery emits, so for at most the limit.
func TestAverageVelocityCountsOnlyTheLimitedSprints(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	seedIndexFixture(t, database)

	inner := strings.Index(averageVelocityQuery, "FROM (SELECT id, started_at, closed_at FROM sprints")
	limit := strings.Index(averageVelocityQuery, "LIMIT ?) s")
	if inner < 0 || limit < inner || strings.Count(averageVelocityQuery, "LIMIT") != 1 ||
		!strings.HasSuffix(averageVelocityQuery, "ORDER BY s.closed_at DESC") {
		t.Fatalf("the LIMIT is not applied in the inner statement alone, with the outer ORDER BY last:\n%s",
			averageVelocityQuery)
	}

	nodes := planTree(t, database, averageVelocityQuery, 5)
	plan := make([]string, 0, len(nodes))
	source, count := -1, -1
	for _, n := range nodes {
		plan = append(plan, n.detail)
		switch {
		case n.detail == "CO-ROUTINE s" || n.detail == "MATERIALIZE s":
			source = n.id
		case strings.HasPrefix(n.detail, "CORRELATED SCALAR SUBQUERY"):
			count = n.id
		}
	}
	if source < 0 || count < 0 {
		t.Fatalf("the plan has no subquery s or no correlated count.\nplan: %s", strings.Join(plan, " | "))
	}
	if underNode(nodes, count, source) {
		t.Errorf("the completed count runs inside the subquery that selects the sprints, before the LIMIT.\nplan: %s",
			strings.Join(plan, " | "))
	}
	sprintsRead := false
	for _, n := range nodes {
		if strings.Contains(n.detail, " sprints ") && underNode(nodes, n.id, source) {
			sprintsRead = true
		}
	}
	if !sprintsRead {
		t.Errorf("the sprints are not read inside the limited subquery.\nplan: %s", strings.Join(plan, " | "))
	}
}

// TestAverageVelocityUsesTheMostRecentClosedSprints checks the value of the read
// on eight closed sprints and one without a start date: the average is over the
// five most recently closed sprints that have both dates, and a sprint outside
// them changes nothing.
func TestAverageVelocityUsesTheMostRecentClosedSprints(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := testContext()

	// Sprint k (0-based) closes on day k+2 after a 1-day run with k+1 completed
	// members, so its velocity is k+1; the most recent five are k = 3..7, whose
	// average is (4+5+6+7+8)/5 = 6.
	for k := range 8 {
		sprintID := mustSeedSprint(t, database, &models.Sprint{
			Title: fmt.Sprintf("Payout batch %d", k+1), Description: "Release one payout batch.",
			Status: models.SprintPending, CreatedAt: "2026-05-01T08:00:00.000Z",
		})
		ids := make([]int, 0, k+1)
		for i := range k + 1 {
			ids = append(ids, mustSeedTask(t, database, &models.Task{
				Title:                  fmt.Sprintf("Release payout %d of batch %d", i+1, k+1),
				Status:                 models.StatusBacklog,
				FunctionalRequirements: "Every approved payout reaches the merchant account.",
				TechnicalRequirements:  "Submit the payout through the banking gateway.",
				AcceptanceCriteria:     "The gateway acknowledges the payout.",
				CreatedAt:              "2026-05-01T08:00:00.000Z",
			}))
		}
		if err := database.AddTasksToSprint(ctx, sprintID, ids); err != nil {
			t.Fatalf("adding members: %v", err)
		}
		for _, id := range ids {
			if _, err := database.Exec(`UPDATE tasks SET status = 'COMPLETED', closed_at = ? WHERE id = ?`,
				fmt.Sprintf("2026-05-%02dT10:00:00.000Z", k+2), id); err != nil {
				t.Fatalf("completing task: %v", err)
			}
		}
		if _, err := database.Exec(`UPDATE sprints SET status = 'CLOSED', started_at = ?, closed_at = ? WHERE id = ?`,
			fmt.Sprintf("2026-05-%02dT08:00:00.000Z", k+1), fmt.Sprintf("2026-05-%02dT08:00:00.000Z", k+2), sprintID); err != nil {
			t.Fatalf("closing the sprint: %v", err)
		}
	}
	// The most recently closed sprint of all has no start date, so it is not one
	// of the five.
	undated := mustSeedSprint(t, database, &models.Sprint{
		Title: "Undated payout batch", Description: "Closed without a recorded start.",
		Status: models.SprintPending, CreatedAt: "2026-05-01T08:00:00.000Z",
	})
	if _, err := database.Exec(`UPDATE sprints SET status = 'CLOSED', closed_at = ? WHERE id = ?`,
		"2026-05-30T08:00:00.000Z", undated); err != nil {
		t.Fatalf("closing the undated sprint: %v", err)
	}

	velocity, err := database.GetAverageVelocity(ctx, 5)
	if err != nil {
		t.Fatalf("GetAverageVelocity: %v", err)
	}
	if velocity != 6 {
		t.Errorf("average velocity = %v, want 6, the average over the five most recent dated sprints", velocity)
	}
}

// auditStatsFixture writes audit entries over three operations, both entity
// types, and three days, returning the number written.
func auditStatsFixture(t *testing.T, database *DB) int {
	t.Helper()
	ops := []string{"TASK_CREATE", "TASK_STATUS_CHANGE", "SPRINT_CREATE", "TASK_ADDED_TO_SPRINT"}
	n := 0
	for i := range 60 {
		op := ops[i%len(ops)]
		entity := "TASK"
		if strings.HasPrefix(op, "SPRINT") || i%7 == 0 {
			entity = "SPRINT"
		}
		at := fmt.Sprintf("2026-07-%02dT%02d:%02d:00.000Z", 10+i%3, i%24, i%60)
		if _, err := database.Exec(`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			op, entity, i+1, at); err != nil {
			t.Fatalf("writing audit entry: %v", err)
		}
		n++
	}
	return n
}

// referenceAuditStats computes the statistics with ONE grouping over the pair,
// the form the four statements replaced, as the oracle of the values.
func referenceAuditStats(t *testing.T, database *DB, since, until *string) *models.AuditStats {
	t.Helper()
	q := "SELECT operation, entity_type, COUNT(*), MIN(performed_at), MAX(performed_at) FROM audit WHERE 1=1"
	var args []any
	if since != nil {
		q += " AND performed_at >= ?"
		args = append(args, *since)
	}
	if until != nil {
		q += " AND performed_at <= ?"
		args = append(args, *until)
	}
	rows, err := database.Query(q+" GROUP BY operation, entity_type", args...)
	if err != nil {
		t.Fatalf("reference audit stats: %v", err)
	}
	defer rows.Close()
	want := &models.AuditStats{ByOperation: map[string]int{}, ByEntityType: map[string]int{}}
	for rows.Next() {
		var op, entity, first, last string
		var n int
		if err := rows.Scan(&op, &entity, &n, &first, &last); err != nil {
			t.Fatalf("scanning reference row: %v", err)
		}
		want.TotalEntries += n
		want.ByOperation[op] += n
		want.ByEntityType[entity] += n
		if want.FirstEntryAt == nil || first < *want.FirstEntryAt {
			want.FirstEntryAt = &first
		}
		if want.LastEntryAt == nil || last > *want.LastEntryAt {
			want.LastEntryAt = &last
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating reference rows: %v", err)
	}
	return want
}

// sameAuditStats reports whether two statistics carry the same values.
func sameAuditStats(a, b *models.AuditStats) bool {
	eq := func(x, y *string) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.TotalEntries == b.TotalEntries && maps.Equal(a.ByOperation, b.ByOperation) &&
		maps.Equal(a.ByEntityType, b.ByEntityType) && eq(a.FirstEntryAt, b.FirstEntryAt) && eq(a.LastEntryAt, b.LastEntryAt)
}

// TestAuditStatsMatchOneGrouping checks the four statements against the single
// grouping over (operation, entity_type) for every filter shape, including a
// range that admits no entry, whose dates are null and whose maps are empty.
func TestAuditStatsMatchOneGrouping(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	written := auditStatsFixture(t, database)

	since, until := "2026-07-11T00:00:00.000Z", "2026-07-11T23:59:59.999Z"
	early := "2026-01-01T00:00:00.000Z"
	for _, c := range []struct {
		since, until *string
		name         string
	}{
		{nil, nil, "no date bound"},
		{&since, nil, "--since only"},
		{nil, &until, "--until only"},
		{&since, &until, "both bounds"},
		{nil, &early, "a range that admits no entry"},
	} {
		got, err := database.GetAuditStats(testContext(), c.since, c.until)
		if err != nil {
			t.Fatalf("%s: GetAuditStats: %v", c.name, err)
		}
		want := referenceAuditStats(t, database, c.since, c.until)
		if !sameAuditStats(got, want) {
			t.Errorf("%s: GetAuditStats = %+v, want %+v", c.name, got, want)
		}
		sum := 0
		for _, n := range got.ByEntityType {
			sum += n
		}
		if sum != got.TotalEntries {
			t.Errorf("%s: the entity-type counts sum to %d, total_entries is %d", c.name, sum, got.TotalEntries)
		}
	}
	all, err := database.GetAuditStats(testContext(), nil, nil)
	if err != nil || all.TotalEntries < written {
		t.Fatalf("the unfiltered statistics count %v entries (%v), want at least the %d written", all, err, written)
	}
}

// TestAuditStatsPlans is SPEC/DATABASE.md § Verification for § Audit Statistics,
// planned from the production builder: with no date bound or one, the grouping
// by operation is a covering read of idx_audit_operation and the grouping by
// entity type a covering read of idx_audit_entity, neither sorting; with both
// bounds the planner may read the range through idx_audit_date and group what it
// admits; the oldest and newest entries are each a search of idx_audit_date; and
// no statement scans the audit table itself.
func TestAuditStatsPlans(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	seedIndexFixture(t, database)

	since, until := "2026-01-01T00:00:00.000Z", "2026-12-31T23:59:59.000Z"
	for _, c := range []struct {
		since, until *string
		name         string
		sortAllowed  bool
	}{
		{nil, nil, "no date bound", false},
		{&since, nil, "--since only", false},
		{nil, &until, "--until only", false},
		{&since, &until, "both bounds", true},
	} {
		q := buildAuditStatsQueries(c.since, c.until)
		for _, s := range []struct {
			name, query, index string
			grouping           bool
		}{
			{"by operation", q.byOperation, "idx_audit_operation", true},
			{"by entity type", q.byEntityType, "idx_audit_entity", true},
			{"oldest entry", q.first, "idx_audit_date", false},
			{"newest entry", q.last, "idx_audit_date", false},
		} {
			t.Run(c.name+", "+s.name, func(t *testing.T) {
				plan := queryPlan(t, database, s.query, q.args...)
				steps := strings.Split(strings.TrimSuffix(plan, " | "), " | ")
				for _, step := range steps {
					if step == "SCAN audit" {
						t.Errorf("the statement scans the audit table.\nplan: %s", plan)
					}
				}
				switch {
				case !s.grouping:
					if !strings.HasPrefix(steps[0], "SEARCH audit USING COVERING INDEX idx_audit_date") || len(steps) != 1 {
						t.Errorf("the statement is not one search of idx_audit_date.\nplan: %s", plan)
					}
				case !c.sortAllowed:
					if steps[0] != "SCAN audit USING COVERING INDEX "+s.index || strings.Contains(plan, "TEMP B-TREE") {
						t.Errorf("the grouping is not a covering read of %s with no sort step.\nplan: %s", s.index, plan)
					}
				default:
					if !strings.Contains(steps[0], "USING") || !strings.Contains(steps[0], "INDEX") {
						t.Errorf("the grouping reads the audit table through no index.\nplan: %s", plan)
					}
				}
			})
		}
	}
}

// TestAuditStatsReadOneSnapshot is the gate for "One read transaction": an entry
// committed while the statistics are being read, after the first statement, is
// counted by no statement, so the figures describe one state of the log. Read
// apart, the entity-type counts and the newest entry would include it while the
// total did not.
func TestAuditStatsReadOneSnapshot(t *testing.T) {
	database, counter, cleanup := setupCountingDB(t)
	defer cleanup()
	// WAL, as production configures it, lets the writer commit while the read
	// transaction holds its snapshot.
	if _, err := database.ExecContext(testContext(), "PRAGMA journal_mode=WAL"); err != nil {
		t.Fatalf("enabling WAL: %v", err)
	}
	auditStatsFixture(t, database)
	before, err := database.GetAuditStats(testContext(), nil, nil)
	if err != nil {
		t.Fatalf("GetAuditStats: %v", err)
	}

	q := buildAuditStatsQueries(nil, nil)
	const late = "2026-08-01T00:00:00.000Z"
	fired := false
	counter.setBeforeExecute(func(query string) {
		if query != q.byEntityType || fired {
			return
		}
		fired = true
		if _, werr := database.ExecContext(context.Background(),
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES ('SPRINT_CREATE', 'SPRINT', 999, ?)`,
			late); werr != nil {
			t.Errorf("writing an entry between two statements: %v", werr)
		}
	})
	during, err := database.GetAuditStats(testContext(), nil, nil)
	counter.setBeforeExecute(nil)
	if err != nil {
		t.Fatalf("GetAuditStats: %v", err)
	}
	if !fired {
		t.Fatal("the write between the statements never ran; the test proves nothing")
	}
	if !sameAuditStats(during, before) {
		t.Errorf("the statistics read during the write = %+v, want the snapshot %+v", during, before)
	}

	// The control: the write did land, so a later read sees it.
	after, err := database.GetAuditStats(testContext(), nil, nil)
	if err != nil {
		t.Fatalf("GetAuditStats after the write: %v", err)
	}
	if after.TotalEntries != before.TotalEntries+1 || after.LastEntryAt == nil || *after.LastEntryAt != late {
		t.Errorf("after the write the statistics are %+v; the write did not land, so the test proves nothing", after)
	}
}
