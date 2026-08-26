package db

import (
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file holds the schema half of rmp task 296: the application counts a free
// text field's length in code points, and the CHECK constraint that guards the
// same column must reach the identical verdict at the identical boundary.
//
// It matters because the two counts are computed by different engines. The
// application asks Go for utf8.RuneCountInString; SQLite's length() counts the
// bytes of a TEXT value that are not UTF-8 continuation bytes. Those are the
// same number for every well-formed UTF-8 value, which the encoding rule
// guarantees is the only kind that reaches storage (SPEC/MODELS.md § Free-Text
// UTF-8 Encoding Constraint) — but "should be the same" is an argument, and
// SPEC/MODELS.md requires the relation to hold rather than to be assumed. These
// tests measure it.
//
// Before the fix the two disagreed in the direction that could only ever refuse
// valid data: the application counted 765 for a 255-character CJK title and
// refused it, while the column would have accepted it at 255.

// checkedColumn is one TEXT column carrying a CHECK(length(<column>) <= N),
// together with the maximum the application enforces for it.
type checkedColumn struct {
	insert func(t *testing.T, database *DB, value string) error
	name   string
	limit  int
}

// TestSQLiteLengthCountsCharactersNotBytes measures what SQLite's length()
// actually returns, for a value of each of the four scripts, and holds it
// against what the application counts.
//
// This is the evidence, not the assumption: if a future SQLite build, driver or
// column affinity made length() answer in bytes, this test names the column and
// the script it happened on.
func TestSQLiteLengthCountsCharactersNotBytes(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	for _, script := range testenv.LengthProbeScripts() {
		t.Run(script.Name, func(t *testing.T) {
			const runes = 40
			value := script.Repeat(runes)
			if got := utils.FieldLength(value); got != runes {
				t.Fatalf("the probe is %d code points, want %d", got, runes)
			}
			wantBytes := runes * script.BytesPerRune
			if got := len(value); got != wantBytes {
				t.Fatalf("the %s probe is %d bytes, want %d; the case would prove nothing about the two units",
					script.Name, got, wantBytes)
			}

			var sqliteLength int
			if err := database.QueryRow("SELECT length(?)", value).Scan(&sqliteLength); err != nil {
				t.Fatalf("measuring the value with SQLite: %v", err)
			}
			if sqliteLength != runes {
				t.Errorf("SQLite length() = %d for %d %s characters (%d bytes); the application counts %d, "+
					"so the two layers disagree and a value the application accepts could trip the CHECK",
					sqliteLength, runes, script.Name, len(value), utils.FieldLength(value))
			}
		})
	}
}

// TestTheCheckConstraintAgreesWithTheApplicationAtTheBoundary drives the actual
// columns: a value of exactly the maximum must be storable, and one code point
// more must be refused by the CHECK.
//
// The second half is what proves the constraint is doing anything at all. The
// first half is what the defect broke: the CHECK would have taken the value, and
// the application refused it before it ever got there.
func TestTheCheckConstraintAgreesWithTheApplicationAtTheBoundary(t *testing.T) {
	columns := []checkedColumn{
		{name: "tasks.title", limit: models.MaxTaskTitle, insert: insertTaskColumn("title")},
		{name: "tasks.functional_requirements", limit: models.MaxTaskFunctionalRequirements,
			insert: insertTaskColumn("functional_requirements")},
		{name: "tasks.technical_requirements", limit: models.MaxTaskTechnicalRequirements,
			insert: insertTaskColumn("technical_requirements")},
		{name: "tasks.acceptance_criteria", limit: models.MaxTaskAcceptanceCriteria,
			insert: insertTaskColumn("acceptance_criteria")},
		{name: "tasks.completion_summary", limit: models.MaxTaskCompletionSummary,
			insert: insertTaskColumn("completion_summary")},
		{name: "sprints.title", limit: models.MaxSprintTitle, insert: insertSprintTitle},
		{name: "task_comments.body", limit: models.MaxCommentBody, insert: insertTaskCommentBody},
		{name: "sprint_comments.body", limit: models.MaxCommentBody, insert: insertSprintCommentBody},
	}

	for _, column := range columns {
		for _, script := range testenv.LengthProbeScripts() {
			t.Run(column.name+"/"+script.Name, func(t *testing.T) {
				database, cleanup := setupTestDB(t)
				defer cleanup()

				at := script.Repeat(column.limit)
				if err := column.insert(t, database, at); err != nil {
					t.Errorf("%s refused %d %s characters (%d bytes), which the application accepts: %v",
						column.name, column.limit, script.Name, len(at), err)
				}

				over := script.Repeat(column.limit + 1)
				err := column.insert(t, database, over)
				if err == nil {
					t.Errorf("%s accepted %d %s characters, one over its CHECK; the constraint is not "+
						"measuring what this test thinks it is", column.name, column.limit+1, script.Name)
					return
				}
				if !strings.Contains(err.Error(), "CHECK") && !strings.Contains(err.Error(), "constraint") {
					t.Errorf("%s refused the oversize value for something other than its CHECK: %v",
						column.name, err)
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Direct inserts. They bypass the application's own validation on purpose: what
// is under test is the column, not the code path that normally reaches it.
// ---------------------------------------------------------------------------

const (
	seedTaskText  = "Placeholder text well within every maximum"
	seedTimestamp = "2026-08-24T09:00:00Z"
)

func insertTaskTitle(t *testing.T, database *DB, value string) error {
	t.Helper()

	return insertTaskColumn("title")(t, database, value)
}

// taskColumns are the NOT NULL text columns every task insert must supply, in a
// fixed order. completion_summary is not among them: it is nullable, and it is
// added to the list only when it is the column under test.
var taskColumns = []string{"title", "functional_requirements", "technical_requirements", "acceptance_criteria"}

// insertTaskColumn builds an insert that puts the probe in one named task column
// and ordinary text in the others.
//
// The column list is assembled rather than concatenated blindly, so a column
// that is already required is not named twice — SQLite accepts a duplicate
// column in an INSERT and keeps the LAST value bound to it, which silently made
// an earlier version of this helper store the seed text and test nothing.
//
// Every column name comes from taskColumns or from the fixed set spelled at the
// call sites in this file, never from outside it, so the interpolation cannot
// carry anything a caller supplied.
func insertTaskColumn(column string) func(t *testing.T, database *DB, value string) error {
	return func(t *testing.T, database *DB, value string) error {
		t.Helper()

		columns := make([]string, 0, len(taskColumns)+2)
		args := make([]any, 0, len(taskColumns)+2)
		found := false
		for _, c := range taskColumns {
			columns = append(columns, c)
			if c == column {
				found = true
				args = append(args, value)
				continue
			}
			args = append(args, seedTaskText)
		}
		if !found {
			columns = append(columns, column)
			args = append(args, value)
		}
		columns = append(columns, "created_at")
		args = append(args, seedTimestamp)

		placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")
		stmt := `INSERT INTO tasks (` + strings.Join(columns, ", ") + `) VALUES (` + placeholders + `)`
		_, err := database.Exec(stmt, args...)
		return err
	}
}

func insertSprintTitle(t *testing.T, database *DB, value string) error {
	t.Helper()

	_, err := database.Exec(
		`INSERT INTO sprints (status, title, description, created_at, order_index)
		 VALUES ('PENDING', ?, ?, ?, (SELECT COALESCE(MAX(order_index), 0) + 1 FROM sprints))`,
		value, seedTaskText, seedTimestamp)
	return err
}

func insertTaskCommentBody(t *testing.T, database *DB, value string) error {
	t.Helper()

	if err := insertTaskTitle(t, database, seedTaskText); err != nil {
		t.Fatalf("seeding the parent task: %v", err)
	}
	_, err := database.Exec(
		`INSERT INTO task_comments (task_id, type, body, created_at)
		 VALUES ((SELECT MAX(id) FROM tasks), 'NOTE', ?, ?)`,
		value, seedTimestamp)
	return err
}

func insertSprintCommentBody(t *testing.T, database *DB, value string) error {
	t.Helper()

	if err := insertSprintTitle(t, database, seedTaskText); err != nil {
		t.Fatalf("seeding the parent sprint: %v", err)
	}
	_, err := database.Exec(
		`INSERT INTO sprint_comments (sprint_id, type, body, created_at)
		 VALUES ((SELECT MAX(id) FROM sprints), 'DECISION', ?, ?)`,
		value, seedTimestamp)
	return err
}
