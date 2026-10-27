package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The dashboard page reads one consistent state with a bounded, fixed number of
// statements (SPEC/DATABASE.md § Dashboard Queries; SPEC/WEB.md Acceptance
// Criterion 283). Both properties are measured here at the driver boundary, on
// the counting connector the comment reads are measured with: every execution
// that reaches SQLite is counted, and every one is checked to have run inside the
// one transaction ReadDashboard begins.

// dashboardWindow is a throughput window in the canonical timestamp form.
const (
	dashboardWindowStart = "2026-08-10T00:00:00.000Z"
	dashboardWindowEnd   = "2026-11-02T00:00:00.000Z"
)

// seedDashboardRoadmap fills a counting database with taskCount tasks spread
// over every status, three CLOSED sprints, one PENDING sprint, and, when
// withOpen is set, one OPEN sprint holding five of the tasks. The sprints and
// the audit entries are the same whatever taskCount is, so two roadmaps differ
// only in their number of tasks.
func seedDashboardRoadmap(t *testing.T, database *DB, taskCount int, withOpen bool) {
	t.Helper()

	statuses := models.ValidTaskStatuses
	ids := make([]int, 0, taskCount)
	err := database.WithTransaction(func(tx *sql.Tx) error {
		for i := range taskCount {
			id, err := InsertTaskTx(tx, &models.Task{
				Title:     fmt.Sprintf("Reconcile settlement batch %04d against the ledger", i+1),
				Type:      models.ValidTaskTypes[i%len(models.ValidTaskTypes)],
				Status:    models.StatusBacklog,
				Priority:  i % 10,
				Severity:  (i * 7) % 10,
				CreatedAt: fmt.Sprintf("2026-09-%02dT10:00:00.000Z", 1+i%28),
			})
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding %d tasks: %v", taskCount, err)
	}

	sprint := func(title string, order int) int {
		return mustSeedSprint(t, database, &models.Sprint{
			Title: title, Description: title, Status: models.SprintPending,
			CreatedAt: "2026-08-01T09:00:00.000Z", Order: order,
		})
	}
	members := ids[:5]
	for i := range 3 {
		id := sprint(fmt.Sprintf("Settlement hardening wave %d", i+1), i+1)
		if _, err := database.Exec(`UPDATE sprints SET status = ?, started_at = ?, closed_at = ? WHERE id = ?`,
			models.SprintClosed, "2026-09-01T09:00:00.000Z", "2026-09-08T09:00:00.000Z", id); err != nil {
			t.Fatalf("closing sprint %d: %v", id, err)
		}
	}
	sprint("Quarter-end reporting", 10)
	if withOpen {
		open := sprint("Ledger cut-over", 5)
		if err := database.AddTasksToSprint(context.Background(), open, members); err != nil {
			t.Fatalf("adding members to the open sprint: %v", err)
		}
		forceSprintOpen(t, database, open)
	}

	// Every status, so each statement has rows to read.
	for i, id := range ids {
		status := statuses[i%len(statuses)]
		closedAt := any(nil)
		if status == models.StatusCompleted {
			closedAt = "2026-09-20T10:00:00.000Z"
		}
		if _, err := database.Exec(`UPDATE tasks SET status = ?, started_at = ?, closed_at = ? WHERE id = ?`,
			status, "2026-09-15T10:00:00.000Z", closedAt, id); err != nil {
			t.Fatalf("setting task %d to %s: %v", id, status, err)
		}
	}
	if len(ids) > 3 {
		if _, err := database.Exec(`INSERT INTO task_dependencies (task_id, depends_on_task_id) VALUES (?, ?)`,
			ids[1], ids[2]); err != nil {
			t.Fatalf("adding a dependency: %v", err)
		}
	}
}

// TestReadDashboard_FixedStatementCountInOneTransaction is the statement half of
// Acceptance Criterion 283: the dashboard issues ten statements with no sprint
// OPEN and eleven with one, the same number for 10 tasks and for 1,000, every one
// of them inside the single read-only transaction ReadDashboard begins.
func TestReadDashboard_FixedStatementCountInOneTransaction(t *testing.T) {
	for _, tc := range []struct {
		withOpen bool
		want     int
	}{
		{withOpen: false, want: 10},
		{withOpen: true, want: 11},
	} {
		counts := map[int]int{}
		for _, tasks := range []int{10, 1000} {
			database, counter, cleanup := setupCountingDB(t)
			seedDashboardRoadmap(t, database, tasks, tc.withOpen)

			counter.reset()
			d, err := database.ReadDashboard(context.Background(), dashboardWindowStart, dashboardWindowEnd)
			if err != nil {
				cleanup()
				t.Fatalf("open=%v tasks=%d: ReadDashboard: %v", tc.withOpen, tasks, err)
			}
			counts[tasks] = counter.count()

			if got := counter.begins.Load(); got != 1 {
				t.Errorf("open=%v tasks=%d: %d transactions begun, want exactly 1", tc.withOpen, tasks, got)
			}
			if got := counter.outsideTx.Load(); got != 0 {
				t.Errorf("open=%v tasks=%d: %d statements ran outside the read transaction, want 0",
					tc.withOpen, tasks, got)
			}
			if (d.OpenSprint != nil) != tc.withOpen {
				t.Errorf("open=%v tasks=%d: OpenSprint = %+v", tc.withOpen, tasks, d.OpenSprint)
			}

			// Bounded lists: no read returns every task row.
			if len(d.InProgress) > dashboardInProgressCap || len(d.MostSevere) > dashboardSevereCap ||
				len(d.Upcoming) > dashboardUpcomingCap || len(d.RecentAudit) > dashboardAuditCap {
				t.Errorf("open=%v tasks=%d: a list exceeds its row cap: %d %d %d %d", tc.withOpen, tasks,
					len(d.InProgress), len(d.MostSevere), len(d.Upcoming), len(d.RecentAudit))
			}
			cleanup()
		}
		if counts[10] != tc.want || counts[1000] != tc.want {
			t.Errorf("open=%v: %d statements for 10 tasks and %d for 1,000, want %d for both",
				tc.withOpen, counts[10], counts[1000], tc.want)
		}
	}
}

// TestReadDashboard_CountsAreAggregatedInTheDatabase pins that the task figures
// come from GROUP BY rows, not from task rows: 1,000 tasks yield at most fifty
// type-and-status rows, and their sum is the task total.
func TestReadDashboard_CountsAreAggregatedInTheDatabase(t *testing.T) {
	database, _, cleanup := setupCountingDB(t)
	defer cleanup()
	seedDashboardRoadmap(t, database, 1000, true)

	d, err := database.ReadDashboard(context.Background(), dashboardWindowStart, dashboardWindowEnd)
	if err != nil {
		t.Fatalf("ReadDashboard: %v", err)
	}
	if len(d.TypeStatusCounts) > len(models.ValidTaskTypes)*len(models.ValidTaskStatuses) {
		t.Errorf("%d type-and-status rows, want at most %d", len(d.TypeStatusCounts),
			len(models.ValidTaskTypes)*len(models.ValidTaskStatuses))
	}
	total := 0
	for _, c := range d.TypeStatusCounts {
		total += c.Count
	}
	if total != 1000 {
		t.Errorf("the type-and-status rows sum to %d, want 1000", total)
	}
	if len(d.OpenSprintMembers) != 5 {
		t.Errorf("the open sprint's member read returned %d rows, want 5", len(d.OpenSprintMembers))
	}
}

// TestDashboardStatements_BindEveryValue pins the parameterisation rule: no
// status value and no row cap is written into a dashboard statement, every one
// is a bound parameter (SPEC/DATABASE.md § Dashboard Queries, Parameterised).
func TestDashboardStatements_BindEveryValue(t *testing.T) {
	statements := map[string]string{
		"type and status":  dashboardTypeStatusQuery,
		"blocked":          dashboardBlockedQuery,
		"open sprint":      dashboardOpenSprintQuery,
		"created per week": dashboardCreatedPerWeekQuery,
		"closed per week":  dashboardCompletedPerWeekQuery,
		"in progress":      dashboardInProgressQuery,
		"most severe":      dashboardSevereQuery,
		"upcoming":         dashboardUpcomingQuery,
	}
	literals := []string{"'COMPLETED'", "'OPEN'", "'CLOSED'", "'PENDING'", "'DOING'", "'TESTING'"}
	for name, q := range statements {
		for _, lit := range literals {
			if strings.Contains(q, lit) {
				t.Errorf("the %s statement writes %s into its SQL instead of binding it", name, lit)
			}
		}
		if strings.Contains(q, "LIMIT") && !strings.Contains(q, "LIMIT ?") {
			t.Errorf("the %s statement carries a LIMIT that is not a bound parameter", name)
		}
	}
}
