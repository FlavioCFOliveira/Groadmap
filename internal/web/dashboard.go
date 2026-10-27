package web

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file holds the Roadmap Dashboard Page, the roadmap's landing page: the
// four key figures with the type-breakdown donuts of three of them, the current
// sprint card, and the four card-tables, all stated from one consistent read of
// the roadmap database (SPEC/WEB.md § Roadmap Dashboard Page). The reads are
// db.ReadDashboard's, inside one read-only transaction (SPEC/DATABASE.md
// § Dashboard Queries); this file turns their rows into the page's figures, its
// aria-label texts, and the JSON data block the page script draws the donuts from
// (SPEC/DATA_FORMATS.md § Dashboard Chart Data).

// dashboardNow returns the request instant. The handler reads it once per request
// and every time-dependent figure uses that one value (SPEC/WEB.md § Roadmap
// Dashboard Page, definition 4). Tests replace it to fix the instant; the package
// runs no test with t.Parallel, so the swap is unsynchronised.
var dashboardNow = func() time.Time { return time.Now().UTC() }

// dashboardSource is the complete read surface of the dashboard page: the one
// read-only transaction of db.ReadDashboard. *db.DB satisfies it.
type dashboardSource interface {
	ReadDashboard(ctx context.Context) (*db.Dashboard, error)
}

// dashboardData is the view model of the roadmap dashboard template. Every
// number the page states is rendered on the server from these fields, so the
// page states them without JavaScript; Charts is the JSON data block the page
// script draws the charts from (SPEC/WEB.md § Roadmap Dashboard Page, Without
// JavaScript; Chart data block).
//
// A donut label is empty when its KPI card's value is 0, and the card then
// renders no donut at all (SPEC/WEB.md § Roadmap Dashboard Page, Type-breakdown donuts, item 5).
type dashboardData struct {
	Sprint              *dashboardSprint
	Name                string
	OpenDonutLabel      string
	CompletedDonutLabel string
	BacklogDonutLabel   string
	Chrome              chrome
	InProgress          []dashboardTaskRow
	MostSevere          []dashboardTaskRow
	Upcoming            []db.DashboardSprintRef
	RecentAudit         []models.AuditEntry
	Charts              dashboardCharts
	KPI                 dashboardKPI
}

// dashboardKPI is the row of four key figures (SPEC/WEB.md § Roadmap Dashboard
// Page, Block 1). ProgressMax is the max attribute of the Completed card's
// progress bar: the task total, or 1 for a roadmap with no task, because a max of
// 0 is not a valid value of the attribute. AverageVelocity is already written
// with one decimal place.
type dashboardKPI struct {
	AverageVelocity  string
	Open             int
	InProgress       int
	Blocked          int
	Completed        int
	Total            int
	CompletedPercent int
	ProgressMax      int
	Backlog          int
}

// dashboardSprint is the Current sprint card of an OPEN sprint (SPEC/WEB.md
// § Roadmap Dashboard Page, Block 2). Elapsed is the "<N> days" text, empty when
// started_at is unset; Capacity is nil when max_tasks is unset. The card draws no
// chart.
type dashboardSprint struct {
	StartedAt       *string
	Capacity        *dashboardCapacity
	Title           string
	Elapsed         string
	Segments        []dashboardSegment
	ID              int
	CompletePercent int
}

// dashboardCapacity is the Capacity line: the sprint's active member tasks, the
// current_load of `rmp sprint show`, against its max_tasks.
type dashboardCapacity struct {
	Active int
	Max    int
}

// dashboardSegment is one status of the current sprint's status breakdown and
// its legend entry. Percent is the share written with at most two decimal places;
// it is empty for a status no member holds, which renders a legend entry and no
// bar segment. Class is the solid background utility of the status's segment and
// Legend the matching Tabler legend colour class.
type dashboardSegment struct {
	Status  models.TaskStatus
	Class   string
	Legend  string
	Percent string
	Count   int
}

// dashboardTaskRow is one row of the Work in progress or Highest-severity open
// tasks card-table. Age is the "<N> days" text of a task in progress, empty when
// its started_at is unset.
type dashboardTaskRow struct {
	Title    string
	Age      string
	Status   models.TaskStatus
	ID       int
	Severity int
	Priority int
}

// dashboardCharts is the JSON data block, in the shape and member order
// SPEC/DATA_FORMATS.md § Dashboard Chart Data publishes. Every slice is non-nil,
// so an array with nothing to hold is written [] rather than null (rule 1).
type dashboardCharts struct {
	TasksByType tasksByTypeChart `json:"tasks_by_type"`
}

// tasksByTypeChart is the data of the three type-breakdown donuts of the KPI
// row: per type with at least one task, in TaskType enum order, its open,
// COMPLETED, and BACKLOG task counts (SPEC/DATA_FORMATS.md § Dashboard Chart
// Data, rule 8).
type tasksByTypeChart struct {
	Types     []models.TaskType `json:"types"`
	Open      []int             `json:"open"`
	Completed []int             `json:"completed"`
	Backlog   []int             `json:"backlog"`
}

// sprintBreakdownStatuses are the statuses of the current sprint's status
// breakdown and legend, in their order, each with the solid counterpart of its
// status badge colour and the Tabler legend class of the same colour. A sprint member is never BACKLOG, so the breakdown names
// the four other statuses only (SPEC/WEB.md § Roadmap Dashboard Page, Block 2,
// items 4 and 5).
var sprintBreakdownStatuses = [...]struct {
	status models.TaskStatus
	class  string
	legend string
}{
	{models.StatusSprint, "bg-cyan", "legend-cyan"},
	{models.StatusDoing, "bg-blue", "legend-blue"},
	{models.StatusTesting, "bg-yellow", "legend-yellow"},
	{models.StatusCompleted, "bg-green", "legend-green"},
}

// loadDashboard reads a roadmap's dashboard read-only. It opens the roadmap
// database, hands it to readDashboard, and releases the handle; no row is written
// and no audit entry is produced (SPEC/WEB.md § Tasks and Sprints from SQLite).
// The caller validates {name} and confirms it exists (resolveRoadmap).
func loadDashboard(ctx context.Context, name string, now time.Time) (dashboardData, error) {
	database, err := db.OpenReadOnly(name)
	if err != nil {
		return dashboardData{}, err
	}
	defer database.Close() //nolint:errcheck // read-only handle; close error is non-actionable

	return readDashboard(ctx, database, name, now)
}

// readDashboard is the dashboard's entire read and the derivation of every figure
// it states, against the page's read surface rather than a concrete connection.
// now is the request instant, read once by the handler.
func readDashboard(ctx context.Context, src dashboardSource, name string, now time.Time) (dashboardData, error) {
	d, err := src.ReadDashboard(ctx)
	if err != nil {
		return dashboardData{}, err
	}

	data := dashboardData{
		Name:        name,
		Upcoming:    d.Upcoming,
		RecentAudit: d.RecentAudit,
		InProgress:  inProgressRows(d.InProgress, now),
		MostSevere:  mostSevereRows(d.MostSevere),
	}

	byType, kpi := taskCountFigures(d.TypeStatusCounts)
	kpi.Blocked = d.Blocked
	kpi.AverageVelocity = strconv.FormatFloat(d.AverageVelocity, 'f', 1, 64)
	data.KPI = kpi

	data.Charts = dashboardCharts{TasksByType: byType}

	if d.OpenSprint != nil {
		data.Sprint = currentSprint(d.OpenSprint, d.OpenSprintMembers, now)
	}

	data.OpenDonutLabel = typeDonutLabel("Open tasks by type: ", byType.Types, byType.Open, kpi.Open)
	data.CompletedDonutLabel = typeDonutLabel("Completed tasks by type: ", byType.Types, byType.Completed, kpi.Completed)
	data.BacklogDonutLabel = typeDonutLabel("Backlog tasks by type: ", byType.Types, byType.Backlog, kpi.Backlog)
	return data, nil
}

// taskCountFigures derives, from the one count by type and status, every task
// figure that depends on no dependency, sprint, or timestamp: the donuts' data
// and the KPI counts. Being readings of one result, they cannot disagree
// (SPEC/DATABASE.md § Dashboard Task Counts by Type and Status).
func taskCountFigures(rows []db.TypeStatusCount) (tasksByTypeChart, dashboardKPI) {
	openByType := map[models.TaskType]int{}
	doneByType := map[models.TaskType]int{}
	backlogByType := map[models.TaskType]int{}
	byStatus := map[models.TaskStatus]int{}
	var kpi dashboardKPI
	for _, r := range rows {
		byStatus[r.Status] += r.Count
		kpi.Total += r.Count
		switch r.Status {
		case models.StatusCompleted:
			doneByType[r.Type] += r.Count
		case models.StatusBacklog:
			backlogByType[r.Type] += r.Count
			openByType[r.Type] += r.Count
		default:
			openByType[r.Type] += r.Count
		}
	}

	kpi.Completed = byStatus[models.StatusCompleted]
	kpi.Open = kpi.Total - kpi.Completed
	kpi.InProgress = byStatus[models.StatusDoing] + byStatus[models.StatusTesting]
	kpi.Backlog = byStatus[models.StatusBacklog]
	kpi.CompletedPercent = floorPercent(kpi.Completed, kpi.Total)
	kpi.ProgressMax = max(kpi.Total, 1)

	types := tasksByTypeChart{Types: []models.TaskType{}, Open: []int{}, Completed: []int{}, Backlog: []int{}}
	for _, t := range models.ValidTaskTypes {
		if openByType[t]+doneByType[t] == 0 {
			continue
		}
		types.Types = append(types.Types, t)
		types.Open = append(types.Open, openByType[t])
		types.Completed = append(types.Completed, doneByType[t])
		types.Backlog = append(types.Backlog, backlogByType[t])
	}
	return types, kpi
}

// currentSprint derives the Current sprint card from the OPEN sprint and its
// member read, through the computations `rmp sprint stats` and `rmp sprint show`
// run over the same rows, so the days elapsed, the status counts, and the
// capacity are the CLI's own (SPEC/WEB.md § Roadmap Dashboard
// Page, Block 2; SPEC/DATABASE.md § Dashboard Open Sprint).
func currentSprint(open *db.DashboardSprint, members []db.SprintMemberState, now time.Time) *dashboardSprint {
	sprint := &models.Sprint{
		ID:        open.ID,
		Title:     open.Title,
		Status:    models.SprintOpen,
		StartedAt: open.StartedAt,
		MaxTasks:  open.MaxTasks,
	}
	tasks := make([]models.Task, len(members))
	for i := range members {
		tasks[i] = models.Task{ID: members[i].ID, Status: members[i].Status, Severity: members[i].Severity}
		if members[i].ClosedAt.Valid {
			tasks[i].ClosedAt = &members[i].ClosedAt.String
		}
	}

	stats := models.CalculateSprintStats(sprint.ID, tasks)
	// The card draws no burndown, so none is computed; a nil series leaves the
	// stats' own empty series in place.
	stats.ApplySprintMetrics(sprint, nil, utils.FormatISO8601(now))

	card := &dashboardSprint{
		ID:              open.ID,
		Title:           open.Title,
		StartedAt:       open.StartedAt,
		CompletePercent: floorPercent(stats.CompletedTasks, stats.TotalTasks),
		Segments:        make([]dashboardSegment, 0, len(sprintBreakdownStatuses)),
	}
	if stats.DaysElapsed != nil {
		card.Elapsed = daysText(*stats.DaysElapsed)
	}
	for _, b := range sprintBreakdownStatuses {
		seg := dashboardSegment{Status: b.status, Class: b.class, Legend: b.legend, Count: stats.StatusDistribution[string(b.status)]}
		if seg.Count > 0 {
			seg.Percent = sharePercent(seg.Count, stats.TotalTasks)
		}
		card.Segments = append(card.Segments, seg)
	}
	if open.MaxTasks != nil {
		show := models.CalculateSprintShowResult(sprint, tasks)
		card.Capacity = &dashboardCapacity{Active: show.CurrentLoad, Max: *open.MaxTasks}
	}
	return card
}

// inProgressRows turns the tasks in progress into card-table rows, each with its
// age in whole days at the request instant.
func inProgressRows(tasks []db.DashboardTask, now time.Time) []dashboardTaskRow {
	rows := make([]dashboardTaskRow, len(tasks))
	for i := range tasks {
		rows[i] = dashboardTaskRow{ID: tasks[i].ID, Title: tasks[i].Title, Status: tasks[i].Status}
		if tasks[i].StartedAt != nil {
			if days, ok := wholeDaysElapsed(*tasks[i].StartedAt, now); ok {
				rows[i].Age = daysText(days)
			}
		}
	}
	return rows
}

// mostSevereRows turns the most severe open tasks into card-table rows.
func mostSevereRows(tasks []db.DashboardTask) []dashboardTaskRow {
	rows := make([]dashboardTaskRow, len(tasks))
	for i := range tasks {
		rows[i] = dashboardTaskRow{
			ID: tasks[i].ID, Title: tasks[i].Title,
			Severity: tasks[i].Severity, Priority: tasks[i].Priority,
		}
	}
	return rows
}

// wholeDaysElapsed is the number of complete 24-hour periods between the stored
// timestamp and now, and 0 when the timestamp lies after now: the measure
// `rmp sprint stats` reports as days_elapsed (SPEC/WEB.md § Roadmap Dashboard
// Page, definition 5). ok is false for a timestamp that does not parse.
func wholeDaysElapsed(stored string, now time.Time) (days int, ok bool) {
	t, err := utils.ParseISO8601(stored)
	if err != nil {
		return 0, false
	}
	return max(int(now.Sub(t).Hours()/24), 0), true
}

// daysText writes a whole number of days: "1 day" for one, "<N> days" otherwise.
func daysText(n int) string {
	if n == 1 {
		return "1 day"
	}
	return strconv.Itoa(n) + " days"
}

// floorPercent is floor(100 * part / whole), and 0 when whole is 0 (SPEC/WEB.md
// § Roadmap Dashboard Page, definition 6).
func floorPercent(part, whole int) int {
	if whole <= 0 {
		return 0
	}
	return part * 100 / whole
}

// sharePercent is part * 100 / whole written with at most two decimal places,
// the share of one segment of the status breakdown.
func sharePercent(part, whole int) string {
	if whole <= 0 {
		return "0"
	}
	share := math.Round(float64(part)*10000/float64(whole)) / 100
	return strconv.FormatFloat(share, 'f', -1, 64)
}

// typeDonutLabel is a KPI type-breakdown donut's aria-label: the prefix, then
// one "<TYPE> <n> (<p>%)" per type whose count is greater than 0, in the order
// of types, separated by ", ", where p is the floor percentage of n in total,
// the card's value. It is "" when total is 0, which renders no donut
// (SPEC/WEB.md § Roadmap Dashboard Page, Type-breakdown donuts and Chart
// containers).
func typeDonutLabel(prefix string, types []models.TaskType, counts []int, total int) string {
	if total <= 0 {
		return ""
	}
	parts := make([]string, 0, len(types))
	for i, t := range types {
		if counts[i] <= 0 {
			continue
		}
		parts = append(parts, string(t)+" "+strconv.Itoa(counts[i])+" ("+strconv.Itoa(floorPercent(counts[i], total))+"%)")
	}
	return prefix + strings.Join(parts, ", ")
}
