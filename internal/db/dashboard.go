package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file holds the reads of the web interface's dashboard page
// (SPEC/DATABASE.md § Dashboard Queries; SPEC/WEB.md § Roadmap Dashboard Page).
//
// The page states every figure it shows from the eight statements below and from
// three reads defined elsewhere in this package — the average-velocity read and
// the sprint-statistics member read of § Join Order of the Sprint Completion
// Counts, and the audit read of § Query Audit Entries — all issued inside ONE
// read-only transaction, so every figure reads the same committed state. The
// number of statements is fixed: ten with no sprint OPEN and eleven with one,
// whatever the number of tasks, sprints, or audit entries. Every status value,
// every bound of the throughput window, and every row cap is a bound parameter.
//
// The statements are package-level so a test can plan and count the exact SQL
// production runs.

// The row caps of the dashboard's bounded reads (SPEC/WEB.md § Roadmap Dashboard
// Page, blocks 4, 5, 6, and 7) and the number of closed sprints the average
// velocity covers (SPEC/COMMANDS.md § Get Roadmap Statistics).
const (
	dashboardInProgressCap    = 10
	dashboardSevereCap        = 5
	dashboardUpcomingCap      = 5
	dashboardAuditCap         = 10
	dashboardVelocitySprintsN = 5
)

const (
	// dashboardTypeStatusQuery is § Dashboard Task Counts by Type and Status.
	dashboardTypeStatusQuery = `SELECT type, status, COUNT(*) AS task_count
FROM tasks
GROUP BY type, status`

	// dashboardBlockedQuery is § Dashboard Blocked Task Count. Both parameters
	// are 'COMPLETED'.
	dashboardBlockedQuery = `SELECT COUNT(*)
FROM tasks t
WHERE t.status <> ?
  AND EXISTS (
    SELECT 1
    FROM task_dependencies d CROSS JOIN tasks b ON b.id = d.depends_on_task_id
    WHERE d.task_id = t.id AND b.status <> ?
  )`

	// dashboardOpenSprintQuery is § Dashboard Open Sprint. Its parameter is
	// 'OPEN'.
	dashboardOpenSprintQuery = `SELECT id, title, started_at, max_tasks
FROM sprints
WHERE status = ?`

	// dashboardCreatedPerWeekQuery is the first statement of § Dashboard Weekly
	// Throughput. Its parameters are the window start and the window end.
	dashboardCreatedPerWeekQuery = `SELECT date(created_at, '-6 days', 'weekday 1') AS week_start, COUNT(*) AS task_count
FROM tasks
WHERE created_at >= ? AND created_at < ?
GROUP BY week_start
ORDER BY week_start ASC`

	// dashboardCompletedPerWeekQuery is the second statement of § Dashboard
	// Weekly Throughput. Its parameters are 'COMPLETED', the window start, and
	// the window end.
	dashboardCompletedPerWeekQuery = `SELECT date(closed_at, '-6 days', 'weekday 1') AS week_start, COUNT(*) AS task_count
FROM tasks
WHERE status = ?
  AND closed_at >= ? AND closed_at < ?
GROUP BY week_start
ORDER BY week_start ASC`

	// dashboardInProgressQuery is § Dashboard Tasks in Progress. Its parameters
	// are 'DOING', 'TESTING', and the row cap.
	dashboardInProgressQuery = `SELECT id, title, status, started_at
FROM tasks
WHERE status IN (?, ?)
ORDER BY started_at IS NULL, started_at ASC, id ASC
LIMIT ?`

	// dashboardSevereQuery is § Dashboard Highest-Severity Open Tasks. Its
	// parameters are 'COMPLETED' and the row cap.
	dashboardSevereQuery = `SELECT id, title, severity, priority
FROM tasks
WHERE status <> ?
ORDER BY severity DESC, priority DESC, id ASC
LIMIT ?`

	// dashboardUpcomingQuery is § Dashboard Upcoming Sprints. Its parameters are
	// 'PENDING' and the row cap.
	dashboardUpcomingQuery = `SELECT s.id, s.title,
       (SELECT COUNT(*) FROM sprint_tasks st WHERE st.sprint_id = s.id) AS task_count
FROM sprints s
WHERE s.status = ?
ORDER BY s.order_index ASC
LIMIT ?`
)

// TypeStatusCount is one row of the dashboard's count by type and status: the
// number of tasks holding that combination. A combination no task holds has no
// row.
type TypeStatusCount struct {
	Type   models.TaskType
	Status models.TaskStatus
	Count  int
}

// DashboardSprint is the roadmap's OPEN sprint as the dashboard reads it.
// StartedAt and MaxTasks are nil when unset.
type DashboardSprint struct {
	StartedAt *string
	MaxTasks  *int
	Title     string
	ID        int
}

// WeekCount is the number of tasks of one ISO 8601 week, keyed by the date of
// the week's Monday, YYYY-MM-DD. A week no task falls in has no row.
type WeekCount struct {
	WeekStart string
	Count     int
}

// DashboardTask is one row of the dashboard's Work in progress or
// Highest-severity open tasks card-table. Each read fills the columns its card
// shows: Status and StartedAt for the tasks in progress, Severity and Priority
// for the most severe open tasks.
type DashboardTask struct {
	StartedAt *string
	Title     string
	Status    models.TaskStatus
	ID        int
	Severity  int
	Priority  int
}

// DashboardSprintRef is one row of the dashboard's Upcoming sprints card-table:
// a PENDING sprint and its membership count.
type DashboardSprintRef struct {
	Title     string
	ID        int
	TaskCount int
}

// Dashboard is everything the dashboard page reads, from one consistent state of
// the roadmap database. Every slice is non-nil. OpenSprint is nil when no sprint
// is OPEN, and OpenSprintMembers is then empty.
type Dashboard struct {
	OpenSprint        *DashboardSprint
	TypeStatusCounts  []TypeStatusCount
	OpenSprintMembers []SprintMemberState
	CreatedPerWeek    []WeekCount
	CompletedPerWeek  []WeekCount
	InProgress        []DashboardTask
	MostSevere        []DashboardTask
	Upcoming          []DashboardSprintRef
	RecentAudit       []models.AuditEntry
	AverageVelocity   float64
	Blocked           int
}

// ReadDashboard performs every read of the dashboard page inside ONE read-only
// transaction (SPEC/DATABASE.md § Dashboard Queries). windowStart and windowEnd
// bound the throughput window: the Monday that begins the oldest of the twelve
// weeks and the Monday after the week holding the request instant, each a
// canonical timestamp at 00:00:00.000 UTC.
//
// The transaction is begun ReadOnly, so the driver begins it with a plain BEGIN
// even on a read-write open (SPEC/IMPLEMENTATION.md § Transaction Lock Mode),
// and it is ended, releasing the snapshot, when the last statement has been
// read.
func (db *DB) ReadDashboard(ctx context.Context, windowStart, windowEnd string) (*Dashboard, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("beginning the dashboard read: %w", err)
	}
	// A no-op once Commit has run; on an error path it releases the snapshot, and
	// the error that caused it is the one returned.
	defer func() { _ = tx.Rollback() }()

	d, err := readDashboard(ctx, tx, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("ending the dashboard read: %w", err)
	}
	return d, nil
}

// readDashboard is ReadDashboard's statements on a given queryer.
func readDashboard(ctx context.Context, q queryer, windowStart, windowEnd string) (*Dashboard, error) {
	d := &Dashboard{}
	var err error

	if d.TypeStatusCounts, err = dashboardTypeStatusCounts(ctx, q); err != nil {
		return nil, err
	}
	if d.Blocked, err = dashboardBlockedCount(ctx, q); err != nil {
		return nil, err
	}
	if d.OpenSprint, err = dashboardOpenSprint(ctx, q); err != nil {
		return nil, err
	}
	d.OpenSprintMembers = []SprintMemberState{}
	if d.OpenSprint != nil {
		if d.OpenSprintMembers, err = sprintTaskStates(ctx, q, d.OpenSprint.ID); err != nil {
			return nil, err
		}
	}
	if d.AverageVelocity, err = averageVelocity(ctx, q, dashboardVelocitySprintsN); err != nil {
		return nil, err
	}
	if d.CreatedPerWeek, err = dashboardWeekCounts(ctx, q, dashboardCreatedPerWeekQuery,
		windowStart, windowEnd); err != nil {
		return nil, err
	}
	if d.CompletedPerWeek, err = dashboardWeekCounts(ctx, q, dashboardCompletedPerWeekQuery,
		models.StatusCompleted, windowStart, windowEnd); err != nil {
		return nil, err
	}
	if d.InProgress, err = dashboardInProgress(ctx, q); err != nil {
		return nil, err
	}
	if d.MostSevere, err = dashboardMostSevere(ctx, q); err != nil {
		return nil, err
	}
	if d.Upcoming, err = dashboardUpcoming(ctx, q); err != nil {
		return nil, err
	}
	// The audit read of § Query Audit Entries with no predicate, LIMIT 10, and no
	// OFFSET: the head of page 1 of the audit log page, in its order.
	if d.RecentAudit, err = auditEntries(ctx, q, &AuditFilter{Limit: dashboardAuditCap}); err != nil {
		return nil, err
	}
	return d, nil
}

// dashboardTypeStatusCounts runs § Dashboard Task Counts by Type and Status.
func dashboardTypeStatusCounts(ctx context.Context, q queryer) ([]TypeStatusCount, error) {
	rows, err := q.QueryContext(ctx, dashboardTypeStatusQuery)
	if err != nil {
		return nil, fmt.Errorf("counting tasks by type and status: %w", err)
	}
	defer rows.Close()

	counts := []TypeStatusCount{}
	for rows.Next() {
		var c TypeStatusCount
		if err := rows.Scan(&c.Type, &c.Status, &c.Count); err != nil {
			return nil, fmt.Errorf("scanning task count by type and status: %w", err)
		}
		counts = append(counts, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task counts by type and status: %w", err)
	}
	return counts, nil
}

// dashboardBlockedCount runs § Dashboard Blocked Task Count.
func dashboardBlockedCount(ctx context.Context, q queryer) (int, error) {
	rows, err := q.QueryContext(ctx, dashboardBlockedQuery, models.StatusCompleted, models.StatusCompleted)
	if err != nil {
		return 0, fmt.Errorf("counting blocked tasks: %w", err)
	}
	defer rows.Close()

	var n int
	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			return 0, fmt.Errorf("scanning blocked task count: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterating blocked task count: %w", err)
	}
	return n, nil
}

// dashboardOpenSprint runs § Dashboard Open Sprint and returns nil when no
// sprint is OPEN. idx_one_open_sprint admits at most one row.
func dashboardOpenSprint(ctx context.Context, q queryer) (*DashboardSprint, error) {
	rows, err := q.QueryContext(ctx, dashboardOpenSprintQuery, models.SprintOpen)
	if err != nil {
		return nil, fmt.Errorf("reading the open sprint: %w", err)
	}
	defer rows.Close()

	var sprint *DashboardSprint
	if rows.Next() {
		var s DashboardSprint
		var startedAt sql.NullString
		var maxTasks sql.NullInt64
		if err := rows.Scan(&s.ID, &s.Title, &startedAt, &maxTasks); err != nil {
			return nil, fmt.Errorf("scanning the open sprint: %w", err)
		}
		if startedAt.Valid {
			v := startedAt.String
			s.StartedAt = &v
		}
		if maxTasks.Valid {
			v := int(maxTasks.Int64)
			s.MaxTasks = &v
		}
		sprint = &s
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating the open sprint: %w", err)
	}
	return sprint, nil
}

// dashboardWeekCounts runs one statement of § Dashboard Weekly Throughput.
func dashboardWeekCounts(ctx context.Context, q queryer, query string, args ...any) ([]WeekCount, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("counting the tasks per week: %w", err)
	}
	defer rows.Close()

	out := []WeekCount{}
	for rows.Next() {
		var w WeekCount
		if err := rows.Scan(&w.WeekStart, &w.Count); err != nil {
			return nil, fmt.Errorf("scanning the tasks of a week: %w", err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating the tasks per week: %w", err)
	}
	return out, nil
}

// dashboardInProgress runs § Dashboard Tasks in Progress.
func dashboardInProgress(ctx context.Context, q queryer) ([]DashboardTask, error) {
	rows, err := q.QueryContext(ctx, dashboardInProgressQuery,
		models.StatusDoing, models.StatusTesting, dashboardInProgressCap)
	if err != nil {
		return nil, fmt.Errorf("listing the tasks in progress: %w", err)
	}
	defer rows.Close()

	out := []DashboardTask{}
	for rows.Next() {
		var task DashboardTask
		var startedAt sql.NullString
		if err := rows.Scan(&task.ID, &task.Title, &task.Status, &startedAt); err != nil {
			return nil, fmt.Errorf("scanning a task in progress: %w", err)
		}
		if startedAt.Valid {
			v := startedAt.String
			task.StartedAt = &v
		}
		out = append(out, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating the tasks in progress: %w", err)
	}
	return out, nil
}

// dashboardMostSevere runs § Dashboard Highest-Severity Open Tasks.
func dashboardMostSevere(ctx context.Context, q queryer) ([]DashboardTask, error) {
	rows, err := q.QueryContext(ctx, dashboardSevereQuery, models.StatusCompleted, dashboardSevereCap)
	if err != nil {
		return nil, fmt.Errorf("listing the most severe open tasks: %w", err)
	}
	defer rows.Close()

	out := []DashboardTask{}
	for rows.Next() {
		var task DashboardTask
		if err := rows.Scan(&task.ID, &task.Title, &task.Severity, &task.Priority); err != nil {
			return nil, fmt.Errorf("scanning a severe open task: %w", err)
		}
		out = append(out, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating the most severe open tasks: %w", err)
	}
	return out, nil
}

// dashboardUpcoming runs § Dashboard Upcoming Sprints.
func dashboardUpcoming(ctx context.Context, q queryer) ([]DashboardSprintRef, error) {
	rows, err := q.QueryContext(ctx, dashboardUpcomingQuery, models.SprintPending, dashboardUpcomingCap)
	if err != nil {
		return nil, fmt.Errorf("listing the upcoming sprints: %w", err)
	}
	defer rows.Close()

	out := []DashboardSprintRef{}
	for rows.Next() {
		var s DashboardSprintRef
		if err := rows.Scan(&s.ID, &s.Title, &s.TaskCount); err != nil {
			return nil, fmt.Errorf("scanning an upcoming sprint: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating the upcoming sprints: %w", err)
	}
	return out, nil
}
