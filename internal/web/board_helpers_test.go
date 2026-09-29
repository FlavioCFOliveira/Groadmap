package web

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file holds the fixture and markup helpers shared by the tests of the sprint
// page's member-tasks board, of its cards, and of the pages that render tasks.

// newSprint creates a PENDING sprint through the statement `sprint create` runs
// and returns its id.
func newSprint(t *testing.T, database *db.DB, title, description string) int {
	t.Helper()

	id, err := seedSprint(database, &models.Sprint{
		Status:      models.SprintPending,
		Title:       title,
		Description: description,
		CreatedAt:   "2026-01-02T09:00:00Z",
	})
	if err != nil {
		t.Fatalf("creating sprint %q: %v", title, err)
	}
	return id
}

// boardRegion returns the page's <main> element, so slicing here keeps the page
// header and the scripts after it out of every assertion about the page's content.
func boardRegion(t *testing.T, body string) string {
	t.Helper()

	const open = `<main class="page-body">`
	start := strings.Index(body, open)
	end := strings.Index(body, "</main>")
	if start < 0 || end < start {
		t.Fatalf("the page has no <main class=\"page-body\"> region to slice")
	}
	return body[start+len(open) : end]
}

// reColumnHeader captures a column's heading, the Tabler colour variant its count
// badge carries, and the count itself. The variant is captured rather than
// pinned, because each column's badge carries the semantic colour of the status
// it groups (Acceptance Criterion 140).
var reColumnHeader = regexp.MustCompile(
	`<h3 class="card-title">([A-Z]+) <span class="badge (bg-[a-z]+-lt) ms-2">(\d+)</span></h3>`)

// columnHeader returns the heading and the count badge of one column.
func columnHeader(t *testing.T, column string) (status string, count int) {
	t.Helper()

	status, _, count = columnBadge(t, column)
	return status, count
}

// columnBadge returns the heading, the badge colour variant, and the count of one
// board column, which is what the colour guards assert over.
func columnBadge(t *testing.T, column string) (heading, variant string, count int) {
	t.Helper()

	m := reColumnHeader.FindStringSubmatch(column)
	if m == nil {
		t.Fatalf("a board column has no Tabler card-title header with a count badge")
	}
	n, err := strconv.Atoi(m[3])
	if err != nil {
		t.Fatalf("column %s: count badge %q is not a number: %v", m[1], m[3], err)
	}
	return m[1], m[2], n
}

// cardMarker identifies the card of one task: the tail of the href the card links
// to, /tasks/<id> and its closing quote. The closing quote keeps the id of one
// task from matching the prefix of another's.
func cardMarker(taskID int) string {
	return `/tasks/` + itoa(taskID) + `"`
}

// shownEmptyState reports whether a column is SHOWING its in-column empty state:
// the element is present and does not carry the hidden attribute.
func shownEmptyState(column string) bool {
	at := strings.Index(column, `data-role="task-board-column-empty"`)
	if at < 0 {
		return false
	}
	rest := column[at:]
	end := strings.Index(rest, ">")
	if end < 0 {
		return false
	}
	return !strings.Contains(rest[:end], "hidden")
}

// cardOpen is the opening markup of a board card, used both to count cards and to
// find a card's boundaries. The card is a link to the task's own page, carrying
// Tabler's card and card-link classes (see task_card_link_test.go).
const cardOpen = `<a class="card card-sm card-link text-reset task-card"`

// cardSlice returns the markup of one task's card within a column.
func cardSlice(t *testing.T, column string, taskID int) string {
	t.Helper()

	at := strings.Index(column, cardMarker(taskID))
	if at < 0 {
		t.Fatalf("task #%d has no card in this column", taskID)
	}
	start := strings.LastIndex(column[:at], cardOpen)
	if start < 0 {
		t.Fatalf("task #%d's card does not open with the Tabler card markup %s", taskID, cardOpen)
	}
	rest := column[start+len(cardOpen):]
	if next := strings.Index(rest, cardOpen); next >= 0 {
		return column[start : start+len(cardOpen)+next]
	}
	return column[start:]
}

// spanWithRole returns the whole <span> element carrying data-role="<role>",
// including its own children, or "" when the html holds none. The slice is taken
// by BALANCING the tags rather than by matching the first closing tag, which
// would cut a group after its first child.
func spanWithRole(t *testing.T, html, role string) string {
	t.Helper()

	at := strings.Index(html, `data-role="`+role+`"`)
	if at < 0 {
		return ""
	}
	start := strings.LastIndex(html[:at], "<span")
	if start < 0 {
		t.Fatalf("the element carrying data-role=%q is not a span\nhtml: %s", role, html)
	}

	rest := html[start:]
	depth := 0
	for i := 0; i < len(rest); {
		switch {
		case strings.HasPrefix(rest[i:], "<span"):
			depth++
			i += len("<span")
		case strings.HasPrefix(rest[i:], "</span>"):
			depth--
			i += len("</span>")
			if depth == 0 {
				return rest[:i]
			}
		default:
			i++
		}
	}
	t.Fatalf("the element carrying data-role=%q is not closed\nhtml: %s", role, html)
	return ""
}

// createEmptyRoadmap creates a roadmap holding no task at all.
func createEmptyRoadmap(name string) error {
	database, err := db.Open(name)
	if err != nil {
		return err
	}
	return database.Close()
}

// countRoadmapTasks returns the number of tasks a roadmap holds, read from the
// database rather than counted from a fixture's own bookkeeping.
func countRoadmapTasks(t *testing.T, name string) int {
	t.Helper()

	database, err := db.OpenReadOnly(name)
	if err != nil {
		t.Fatalf("opening roadmap %q read-only: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	n, err := database.CountTasks(context.Background())
	if err != nil {
		t.Fatalf("counting the tasks of %q: %v", name, err)
	}
	return n
}

// equalIDs reports whether two id sequences are the same, in the same order.
func equalIDs(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// roadmapTaskTitles returns every task of a roadmap keyed by id, read through the
// same read-only path the pages use. Reading a title from the roadmap rather than
// from the markup under test is what keeps an assertion about rendered text
// non-circular.
func roadmapTaskTitles(t *testing.T, roadmap string) map[int]string {
	t.Helper()

	database, err := db.OpenReadOnly(roadmap)
	if err != nil {
		t.Fatalf("opening roadmap %q read-only: %v", roadmap, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	var titles map[int]string
	if _, err := database.ReadTaskListPage(context.Background(), nil, true, func(listing []db.TaskRef) []int {
		titles = make(map[int]string, len(listing))
		for _, ref := range listing {
			titles[ref.ID] = ref.Title
		}
		return nil
	}); err != nil {
		t.Fatalf("listing the tasks of %q: %v", roadmap, err)
	}
	return titles
}

// functionBody returns the source of the function whose signature is given, up to
// its closing brace at the same indentation, so an assertion can be made about
// ONE function rather than about the file.
func functionBody(t *testing.T, script, signature string) string {
	t.Helper()

	at := strings.Index(script, signature)
	if at < 0 {
		t.Fatalf("the script carries no %q", signature)
	}
	rest := script[at:]
	end := strings.Index(rest, "\n  }")
	if end < 0 {
		t.Fatalf("the body of %q is not delimited as expected", signature)
	}
	return rest[:end]
}
