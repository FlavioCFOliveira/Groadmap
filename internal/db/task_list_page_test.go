package db

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for the web tasks page's two reads of SPEC/DATABASE.md
// § Main SQL Queries, "List All", "The web tasks page's two reads": the lean task
// listing, the page-rows read of the selected page's tasks alone, the one read
// transaction both run in. The indexes that serve them are planned in
// index_test.go (§ Verification).

// pageRowsColumns is the page-rows read's projection, in its order.
var pageRowsColumns = []string{"id", "title", "type", "status", "severity", "priority", "created_at"}

// seedPageTasks creates n tasks with varied priority, type and status and
// returns their ids in creation order.
func seedPageTasks(t *testing.T, database *DB, n int) []int {
	t.Helper()
	types := []models.TaskType{models.TypeTask, models.TypeBug, models.TypeUserStory}
	statuses := []models.TaskStatus{models.StatusBacklog, models.StatusDoing, models.StatusCompleted}
	ids := make([]int, 0, n)
	for i := range n {
		id, err := seedTask(database, &models.Task{
			Title:                  fmt.Sprintf("Reconcile the acquirer settlement file, window %d", i+1),
			Type:                   types[i%len(types)],
			Status:                 statuses[i%len(statuses)],
			Priority:               i % 10,
			Severity:               (i * 3) % 10,
			FunctionalRequirements: "Every settlement window must balance against the acquirer report.",
			TechnicalRequirements:  "Match both sides by window and report the residual.",
			AcceptanceCriteria:     "A day's windows reconcile with a zero residual.",
			CreatedAt:              fmt.Sprintf("2026-02-%02dT%02d:00:00.000Z", (i%28)+1, i%24),
		})
		if err != nil {
			t.Fatalf("creating task %d: %v", i+1, err)
		}
		ids = append(ids, id)
	}
	return ids
}

// TestReadTaskListPage_PageRowsReadBindsOnlyTheSelectedIDs is the gate for the
// page-rows half of SPEC/WEB.md Acceptance Criterion 89, measured at the driver
// boundary: one listing statement projecting id and title alone, then one
// page-rows statement projecting exactly the seven shown columns and binding
// exactly the selected ids, the rows returned in the selection's order with the
// stored values; and no page-rows statement at all for an empty selection.
func TestReadTaskListPage_PageRowsReadBindsOnlyTheSelectedIDs(t *testing.T) {
	database, counter, cleanup := setupCountingDB(t)
	defer cleanup()
	seedPageTasks(t, database, 40)

	listingSQL, _ := buildTaskListingQuery(nil)

	for _, pick := range []struct {
		name       string
		start, end int
	}{
		{"first page of 10", 0, 10},
		{"a middle slice", 13, 21},
		{"the last row", 39, 40},
		{"no row", 0, 0},
	} {
		t.Run(pick.name, func(t *testing.T) {
			counter.reset()
			var listing []TaskRef
			var selected []int
			rows, err := database.ReadTaskListPage(testContext(), nil, func(l []TaskRef) []int {
				listing = slices.Clone(l)
				for _, ref := range l[pick.start:pick.end] {
					selected = append(selected, ref.ID)
				}
				return selected
			})
			if err != nil {
				t.Fatalf("ReadTaskListPage: %v", err)
			}
			if len(listing) != 40 {
				t.Fatalf("the listing returned %d tasks, want 40", len(listing))
			}

			if got := counter.executed(listingSQL); got != 1 {
				t.Errorf("the task listing ran %d times, want once", got)
			}
			if len(selected) == 0 {
				if counter.count() != 1 || rows != nil {
					t.Errorf("an empty selection issued %d statements and returned %d rows; want the listing alone",
						counter.count(), len(rows))
				}
				return
			}

			rowsSQL, rowsArgs := buildTaskRowsQuery(selected)
			if got := counter.executed(rowsSQL); got != 1 || counter.count() != 2 {
				t.Errorf("the page-rows read ran %d times among %d statements; want once among 2", got, counter.count())
			}
			if strings.Count(rowsSQL, "?") != len(selected) {
				t.Errorf("the page-rows read carries %d placeholders for %d selected ids: %s",
					strings.Count(rowsSQL, "?"), len(selected), rowsSQL)
			}
			wantArgs := make([]any, len(selected))
			for i, id := range selected {
				wantArgs[i] = id
			}
			if !slices.Equal(rowsArgs, wantArgs) {
				t.Errorf("the page-rows read binds %v, want exactly the selected ids %v", rowsArgs, wantArgs)
			}

			gotIDs := make([]int, len(rows))
			for i := range rows {
				gotIDs[i] = rows[i].ID
				stored, gerr := database.GetTask(testContext(), rows[i].ID)
				if gerr != nil {
					t.Fatalf("GetTask(%d): %v", rows[i].ID, gerr)
				}
				want := TaskRow{ID: stored.ID, Title: stored.Title, Type: stored.Type, Status: stored.Status,
					Severity: stored.Severity, Priority: stored.Priority, CreatedAt: stored.CreatedAt}
				if rows[i] != want {
					t.Errorf("row %d = %+v, want the stored %+v", i, rows[i], want)
				}
			}
			if !slices.Equal(gotIDs, selected) {
				t.Errorf("the rows come in the order %v, want the selection's order %v", gotIDs, selected)
			}
		})
	}
}

// TestTaskListPage_StatementProjections pins each statement's projection on the
// columns SQLite reports for it: id and title for the task listing, the seven
// shown columns for the page-rows read, and nothing else.
func TestTaskListPage_StatementProjections(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()
	ids := seedPageTasks(t, database, 3)

	listingSQL, listingArgs := buildTaskListingQuery(&TaskListFilter{Statuses: []models.TaskStatus{models.StatusDoing}})
	rowsSQL, rowsArgs := buildTaskRowsQuery(ids)
	for _, c := range []struct {
		name  string
		query string
		args  []any
		want  []string
	}{
		{"task listing", listingSQL, listingArgs, []string{"id", "title"}},
		{"page-rows read", rowsSQL, rowsArgs, pageRowsColumns},
	} {
		rows, err := database.QueryContext(testContext(), c.query, c.args...)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		cols, err := rows.Columns()
		_ = rows.Close() // read-only probe; the columns are all that is used
		if err != nil {
			t.Fatalf("%s: columns: %v", c.name, err)
		}
		if !slices.Equal(cols, c.want) {
			t.Errorf("%s projects %v, want exactly %v", c.name, cols, c.want)
		}
	}
	if !strings.HasSuffix(rowsSQL, " ORDER BY t.id") || strings.Contains(rowsSQL, "subtask") ||
		strings.Contains(rowsSQL, "task_dependencies") {
		t.Errorf("the page-rows read is not the shown columns in id order: %s", rowsSQL)
	}
}

// TestReadTaskListPage_BothReadsSeeOneSnapshot is the gate for the one read
// transaction: a write committed between the task listing and the page-rows read
// is not seen by the page-rows read, which therefore renders the task exactly as
// the listing admitted it. Two separate reads would see the write.
func TestReadTaskListPage_BothReadsSeeOneSnapshot(t *testing.T) {
	database, _, cleanup := setupCountingDB(t)
	defer cleanup()
	// WAL, as production configures it, lets the writer commit while the read
	// transaction holds its snapshot.
	if _, err := database.ExecContext(testContext(), "PRAGMA journal_mode=WAL"); err != nil {
		t.Fatalf("enabling WAL: %v", err)
	}
	ids := seedPageTasks(t, database, 5)
	target := ids[2]
	const renamed = "Renamed while the page was being read"

	var listedTitle string
	rows, err := database.ReadTaskListPage(testContext(), nil, func(listing []TaskRef) []int {
		for _, ref := range listing {
			if ref.ID == target {
				listedTitle = ref.Title
			}
		}
		if _, werr := database.ExecContext(testContext(), "UPDATE tasks SET title = ? WHERE id = ?", renamed, target); werr != nil {
			t.Fatalf("renaming task %d between the two reads: %v", target, werr)
		}
		return []int{target}
	})
	if err != nil {
		t.Fatalf("ReadTaskListPage: %v", err)
	}
	if len(rows) != 1 || rows[0].Title != listedTitle || listedTitle == renamed {
		t.Errorf("the page-rows read returned %+v; want the title the listing saw, %q", rows, listedTitle)
	}

	// The control: the write did land, so a later read sees it.
	after, err := database.ReadTaskListPage(testContext(), nil, func([]TaskRef) []int { return []int{target} })
	if err != nil {
		t.Fatalf("ReadTaskListPage after the write: %v", err)
	}
	if len(after) != 1 || after[0].Title != renamed {
		t.Errorf("after the write the page-rows read returned %+v; the write did not land, so the test proves nothing", after)
	}
}
