package db

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for the web tasks page's read of SPEC/DATABASE.md § Main
// SQL Queries, "List All" — the predicates, their bound parameters, and the total
// ordering — and for § List Sprint Titles.

// filteredTask is the test's own record of one task it created.
type filteredTask struct {
	created  string
	status   models.TaskStatus
	taskType models.TaskType
	id       int
	priority int
	severity int
	sprint   int
}

// seedFilteredRoadmap creates 36 tasks varying every filtered dimension, three
// sprints' worth of membership (two sprints and none), and groups of three tasks
// equal on both priority and created_at.
func seedFilteredRoadmap(t *testing.T, database *DB) ([]filteredTask, int, int) {
	t.Helper()

	sprintA, err := seedSprint(database, &models.Sprint{Status: models.SprintPending, Title: "Checkout hardening",
		Description: "Close the checkout attack surface.", CreatedAt: "2026-02-01T09:00:00Z"})
	if err != nil {
		t.Fatalf("creating sprint A: %v", err)
	}
	sprintB, err := seedSprint(database, &models.Sprint{Status: models.SprintPending, Title: "Settlement reconciliation",
		Description: "Reconcile acquirer files nightly.", CreatedAt: "2026-02-02T09:00:00Z"})
	if err != nil {
		t.Fatalf("creating sprint B: %v", err)
	}

	tasks := make([]filteredTask, 0, 36)
	var inA, inB []int
	for i := range 36 {
		g := i / 3
		task := filteredTask{
			created:  "2026-03-01T09:" + twoDigits(g) + ":00.000Z",
			status:   models.ValidTaskStatuses[i%len(models.ValidTaskStatuses)],
			taskType: models.ValidTaskTypes[(i/2)%len(models.ValidTaskTypes)],
			priority: (g * 7) % 10,
			severity: (i * 3) % 10,
		}
		id, terr := seedTask(database, &models.Task{
			Title: "Reconcile settlement window " + strconv.Itoa(i+1), Type: task.taskType,
			Status: task.status, Priority: task.priority, Severity: task.severity,
			FunctionalRequirements: "Every window must balance against the acquirer report.",
			TechnicalRequirements:  "Match both sides by window and report the residual.",
			AcceptanceCriteria:     "A day's windows reconcile with a zero residual.",
			CreatedAt:              task.created,
		})
		if terr != nil {
			t.Fatalf("creating task %d: %v", i+1, terr)
		}
		task.id = id
		switch i % 3 {
		case 0:
			task.sprint = sprintA
			inA = append(inA, id)
		case 1:
			task.sprint = sprintB
			inB = append(inB, id)
		}
		tasks = append(tasks, task)
	}
	if err := database.AddTasksToSprint(testContext(), sprintA, inA); err != nil {
		t.Fatalf("adding to sprint A: %v", err)
	}
	if err := database.AddTasksToSprint(testContext(), sprintB, inB); err != nil {
		t.Fatalf("adding to sprint B: %v", err)
	}
	// Membership may rewrite a status; put every task back to its own.
	for _, task := range tasks {
		if _, err := database.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, task.status, task.id); err != nil {
			t.Fatalf("restoring the status of task %d: %v", task.id, err)
		}
	}
	return tasks, sprintA, sprintB
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// expectFiltered returns the ids keep admits, in the total order.
func expectFiltered(tasks []filteredTask, keep func(filteredTask) bool) []int {
	var matched []filteredTask
	for _, task := range tasks {
		if keep(task) {
			matched = append(matched, task)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if a.priority != b.priority {
			return a.priority > b.priority
		}
		if a.created != b.created {
			return a.created < b.created
		}
		return a.id < b.id
	})
	ids := make([]int, len(matched))
	for i := range matched {
		ids[i] = matched[i].id
	}
	return ids
}

// TestListAllTasks_AppliesEachPredicateInTheTotalOrder asserts every predicate of
// the web tasks page's read — status and type equalities, priority and severity
// thresholds, sprint membership and no-sprint — alone and together, over data
// holding ties on priority and created_at, returns exactly the admitted tasks in
// the order priority DESC, created_at ASC, id ASC.
func TestListAllTasks_AppliesEachPredicateInTheTotalOrder(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	tasks, sprintA, sprintB := seedFilteredRoadmap(t, database)

	doing, bug, seven, three := models.StatusDoing, models.TypeBug, 7, 3
	cases := []struct {
		filter *TaskListFilter
		keep   func(filteredTask) bool
		name   string
	}{
		{nil, func(filteredTask) bool { return true }, "no filter"},
		{&TaskListFilter{Status: &doing}, func(x filteredTask) bool { return x.status == doing }, "status"},
		{&TaskListFilter{TaskType: &bug}, func(x filteredTask) bool { return x.taskType == bug }, "type"},
		{&TaskListFilter{MinPriority: &seven}, func(x filteredTask) bool { return x.priority >= 7 }, "priority"},
		{&TaskListFilter{MinSeverity: &three}, func(x filteredTask) bool { return x.severity >= 3 }, "severity"},
		{&TaskListFilter{SprintID: &sprintA}, func(x filteredTask) bool { return x.sprint == sprintA }, "sprint A"},
		{&TaskListFilter{SprintID: &sprintB}, func(x filteredTask) bool { return x.sprint == sprintB }, "sprint B"},
		{&TaskListFilter{NoSprint: true}, func(x filteredTask) bool { return x.sprint == 0 }, "no sprint"},
		{&TaskListFilter{Status: &doing, MinSeverity: &three, NoSprint: true},
			func(x filteredTask) bool { return x.status == doing && x.severity >= 3 && x.sprint == 0 }, "combined"},
		// Sort and Limit are ignored: the order is always the total one, unbounded.
		{&TaskListFilter{Sort: "created", Limit: 2}, func(filteredTask) bool { return true }, "sort and limit ignored"},
	}
	for _, c := range cases {
		got, err := database.ListAllTasks(testContext(), c.filter)
		if err != nil {
			t.Fatalf("%s: ListAllTasks: %v", c.name, err)
		}
		ids := make([]int, len(got))
		for i := range got {
			ids[i] = got[i].ID
		}
		if want := expectFiltered(tasks, c.keep); !slices.Equal(ids, want) {
			t.Errorf("%s: ListAllTasks = %v, want %v", c.name, ids, want)
		}
	}
}

// TestListAllTasks_BindsEveryFilterValue is the SQL half of SPEC/WEB.md
// Acceptance Criterion 117: each accepted value is appended as one predicate whose
// value is a bound parameter, the SQL text carries no value, and the ordering is
// the total one with no LIMIT and no OFFSET.
func TestListAllTasks_BindsEveryFilterValue(t *testing.T) {
	doing, bug, prio, sev, sprint := models.StatusDoing, models.TypeBug, 7, 4, 913
	filter := &TaskListFilter{Status: &doing, TaskType: &bug, MinPriority: &prio, MinSeverity: &sev,
		SprintID: &sprint, Sort: sortPriorityThenID}
	query, args := buildListTasksQuery(filter)

	for _, value := range []string{"DOING", "BUG", "913"} {
		if strings.Contains(query, value) {
			t.Errorf("the SQL text carries the value %q; every value is bound", value)
		}
	}
	for _, predicate := range []string{
		"AND t.status = ?", "AND t.type = ?", "AND t.priority >= ?", "AND t.severity >= ?",
		"AND t.id IN (SELECT st.task_id FROM sprint_tasks st WHERE st.sprint_id = ?)",
	} {
		if strings.Count(query, predicate) != 1 {
			t.Errorf("the SQL text does not carry the predicate %q exactly once", predicate)
		}
	}
	wantArgs := []any{"DOING", 7, 4, "BUG", 913}
	if !slices.Equal(args, wantArgs) {
		t.Errorf("the bound arguments are %v, want %v", args, wantArgs)
	}
	if !strings.HasSuffix(query, " ORDER BY t.priority DESC, t.created_at ASC, t.id ASC") ||
		strings.Contains(query, "LIMIT") || strings.Contains(query, "OFFSET") {
		t.Errorf("the SQL text does not end in the total order with no LIMIT and no OFFSET: %s", query)
	}

	noSprint, noArgs := buildListTasksQuery(&TaskListFilter{NoSprint: true, Sort: sortPriorityThenID})
	if !strings.Contains(noSprint, "AND NOT EXISTS (SELECT 1 FROM sprint_tasks st WHERE st.task_id = t.id)") || len(noArgs) != 0 {
		t.Errorf("the no-sprint predicate is missing or binds an argument: %s %v", noSprint, noArgs)
	}
	bare, bareArgs := buildListTasksQuery(&TaskListFilter{Sort: sortPriorityThenID})
	if strings.Contains(bare, " AND ") || len(bareArgs) != 0 {
		t.Errorf("a filter with no value appends a predicate: %s %v", bare, bareArgs)
	}
}

// TestListSprintTitles returns the id and title of every sprint in ascending
// order_index, which diverges here from creation order, and an empty non-nil
// slice for a roadmap with no sprint (SPEC/DATABASE.md § List Sprint Titles).
func TestListSprintTitles(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	empty, err := database.ListSprintTitles(testContext())
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("ListSprintTitles on no sprint = %v, %v; want an empty non-nil slice", empty, err)
	}

	ids := make([]int, 0, 3)
	for _, s := range []struct {
		title string
		order int
	}{{"Settlement reconciliation", 3}, {"Checkout hardening", 1}, {"Checkout hardening", 2}} {
		id, serr := seedSprint(database, &models.Sprint{Status: models.SprintPending, Title: s.title,
			Description: "A sprint of the payments roadmap.", CreatedAt: "2026-02-01T09:00:00Z", Order: s.order})
		if serr != nil {
			t.Fatalf("creating sprint %q: %v", s.title, serr)
		}
		ids = append(ids, id)
	}
	got, err := database.ListSprintTitles(testContext())
	if err != nil {
		t.Fatalf("ListSprintTitles: %v", err)
	}
	want := []SprintRef{{ID: ids[1], Title: "Checkout hardening"}, {ID: ids[2], Title: "Checkout hardening"},
		{ID: ids[0], Title: "Settlement reconciliation"}}
	if !slices.Equal(got, want) {
		t.Errorf("ListSprintTitles = %v, want %v in ascending order_index", got, want)
	}
}

// TestListAllTasks_MultiValuePredicatesAreOrWithinAndAcross is the database half
// of SPEC/WEB.md Acceptance Criteria 113 and 248: Statuses and TaskTypes each admit
// a task whose value equals ANY one of theirs (OR within the dimension), the two
// dimensions and the sprint predicates combine by AND, a one-element list admits
// what that single value admits, a repeated value changes nothing, and an empty
// list filters nothing.
func TestListAllTasks_MultiValuePredicatesAreOrWithinAndAcross(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	tasks, sprintA, _ := seedFilteredRoadmap(t, database)

	doing, testing_, bug, epic := models.StatusDoing, models.StatusTesting, models.TypeBug, models.TypeEpic
	cases := []struct {
		filter *TaskListFilter
		keep   func(filteredTask) bool
		name   string
	}{
		{&TaskListFilter{Statuses: []models.TaskStatus{doing}}, func(x filteredTask) bool { return x.status == doing }, "one status"},
		{&TaskListFilter{Statuses: []models.TaskStatus{doing, testing_}},
			func(x filteredTask) bool { return x.status == doing || x.status == testing_ }, "two statuses"},
		{&TaskListFilter{Statuses: []models.TaskStatus{doing, doing, testing_, doing}},
			func(x filteredTask) bool { return x.status == doing || x.status == testing_ }, "repeated status"},
		{&TaskListFilter{TaskTypes: []models.TaskType{bug, epic}},
			func(x filteredTask) bool { return x.taskType == bug || x.taskType == epic }, "two types"},
		{&TaskListFilter{Statuses: []models.TaskStatus{doing, testing_}, TaskTypes: []models.TaskType{bug, epic}},
			func(x filteredTask) bool {
				return (x.status == doing || x.status == testing_) && (x.taskType == bug || x.taskType == epic)
			}, "statuses and types"},
		{&TaskListFilter{Statuses: []models.TaskStatus{doing, testing_}, SprintID: &sprintA},
			func(x filteredTask) bool { return (x.status == doing || x.status == testing_) && x.sprint == sprintA }, "statuses and sprint"},
		{&TaskListFilter{Statuses: []models.TaskStatus{}, TaskTypes: []models.TaskType{}},
			func(filteredTask) bool { return true }, "empty lists"},
	}
	nonEmpty := 0
	for _, c := range cases {
		got, err := database.ListAllTasks(testContext(), c.filter)
		if err != nil {
			t.Fatalf("%s: ListAllTasks: %v", c.name, err)
		}
		ids := make([]int, len(got))
		for i := range got {
			ids[i] = got[i].ID
		}
		want := expectFiltered(tasks, c.keep)
		if len(want) > 0 {
			nonEmpty++
		}
		if !slices.Equal(ids, want) {
			t.Errorf("%s: ListAllTasks = %v, want %v", c.name, ids, want)
		}
	}
	if nonEmpty != len(cases) {
		t.Fatalf("only %d of %d cases admit a task; the data does not exercise the predicates", nonEmpty, len(cases))
	}
}

// TestListAllTasks_BindsOnePlaceholderPerDistinctValue is the SQL half of
// SPEC/DATABASE.md § List All, Predicates, for the multi-value filters: the status
// predicate is one IN list with one bound placeholder per DISTINCT value, and the
// type predicate likewise; the SQL text carries no value, and a one-value list
// carries a one-element IN.
func TestListAllTasks_BindsOnePlaceholderPerDistinctValue(t *testing.T) {
	query, args := buildListTasksQuery(&TaskListFilter{
		Statuses:  []models.TaskStatus{models.StatusTesting, models.StatusDoing, models.StatusTesting},
		TaskTypes: []models.TaskType{models.TypeBug},
		Sort:      sortPriorityThenID,
	})
	for _, value := range []string{"TESTING", "DOING", "BUG"} {
		if strings.Contains(query, value) {
			t.Errorf("the SQL text carries the value %q; every value is bound", value)
		}
	}
	if strings.Count(query, "AND t.status IN (?, ?)") != 1 || strings.Count(query, "AND t.type IN (?)") != 1 {
		t.Errorf("the SQL text does not carry one IN list per dimension with one ? per distinct value: %s", query)
	}
	if strings.Contains(query, "t.status = ?") || strings.Contains(query, "t.type = ?") {
		t.Errorf("the multi-value filters appended an equality predicate: %s", query)
	}
	if want := []any{"TESTING", "DOING", "BUG"}; !slices.Equal(args, want) {
		t.Errorf("the bound arguments are %v, want %v", args, want)
	}
}

// TestCountTasks returns the number of tasks the roadmap holds, of any status,
// and 0 for a roadmap with no task (SPEC/DATABASE.md § Count Roadmap Tasks).
func TestCountTasks(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	if n, err := database.CountTasks(testContext()); err != nil || n != 0 {
		t.Fatalf("CountTasks on no task = %d, %v; want 0", n, err)
	}
	tasks, _, _ := seedFilteredRoadmap(t, database)
	statuses := map[models.TaskStatus]bool{}
	for _, task := range tasks {
		statuses[task.status] = true
	}
	if len(statuses) != len(models.ValidTaskStatuses) {
		t.Fatalf("the fixture spans %d statuses, want all %d", len(statuses), len(models.ValidTaskStatuses))
	}
	if n, err := database.CountTasks(testContext()); err != nil || n != len(tasks) {
		t.Errorf("CountTasks = %d, %v; want every task of every status, %d", n, err, len(tasks))
	}
}
