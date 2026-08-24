package web

import (
	"database/sql"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file holds the fixture seeds of this package's tests.
//
// They used to run through db.CreateTask, db.CreateSprint and db.UpdateTaskStatus,
// db-layer write methods that the command layer had replaced with its own
// transactions and that the binary therefore never executed. A page rendered
// from rows written by SQL the product does not run proves less than it looks
// like it does, so the methods are gone (task #188) and the seeds now go
// through what the product runs.
//
// seedTask and seedSprint call db.InsertTaskTx / db.InsertSprintTx and
// db.NextSprintOrderTx, the code behind `task create` and `sprint create`: a
// defect injected there fails these fixtures.
//
// forceTaskLifecycle and forceSprintOpen write columns directly, and only for
// the states a create cannot produce. The transitions that produce them are in
// internal/commands, and this package cannot reach it — internal/commands
// imports internal/web for `rmp web`, so the dependency only runs the other
// way. A fixture here needs the resulting row, not the rule that admits it.

// seedTask inserts one task through db.InsertTaskTx, the statement
// `task create` runs, and returns the new id. An empty Type becomes TASK, the
// value the CLI's --type default supplies.
func seedTask(database *db.DB, task *models.Task) (int, error) {
	if task.Type == "" {
		task.Type = models.TypeTask
	}

	var id int
	err := database.WithTransaction(func(tx *sql.Tx) error {
		var insertErr error
		id, insertErr = db.InsertTaskTx(tx, task)
		return insertErr
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// seedSprint inserts one sprint through db.InsertSprintTx, taking the
// execution order from db.NextSprintOrderTx when the fixture does not name
// one, exactly as `sprint create` does.
func seedSprint(database *db.DB, sprint *models.Sprint) (int, error) {
	var id int
	err := database.WithTransaction(func(tx *sql.Tx) error {
		if sprint.Order <= 0 {
			next, orderErr := db.NextSprintOrderTx(tx)
			if orderErr != nil {
				return orderErr
			}
			sprint.Order = next
		}

		var insertErr error
		id, insertErr = db.InsertSprintTx(tx, sprint)
		return insertErr
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// forceTaskLifecycle puts the named tasks into a status past BACKLOG together
// with the timestamp that status implies, per SPEC/STATE_MACHINE.md. See the
// note at the top of this file for why it is direct SQL.
func forceTaskLifecycle(t *testing.T, database *db.DB, ids []int, status models.TaskStatus) {
	t.Helper()

	// One whole statement per status, spelled out. Assembling the timestamp
	// column name into the SQL would read more briefly and would be the shape
	// every SQL-injection rule exists to discourage, in a file whose entire
	// point is that fixtures state what they write.
	var statement string
	timestamped := true
	switch status {
	case models.StatusDoing:
		statement = `UPDATE tasks SET status = ?, started_at = ? WHERE id = ?`
	case models.StatusTesting:
		statement = `UPDATE tasks SET status = ?, tested_at = ? WHERE id = ?`
	case models.StatusCompleted:
		statement = `UPDATE tasks SET status = ?, closed_at = ? WHERE id = ?`
	case models.StatusBacklog:
		statement = `UPDATE tasks SET status = ?, started_at = NULL, tested_at = NULL, closed_at = NULL WHERE id = ?`
		timestamped = false
	default:
		statement = `UPDATE tasks SET status = ? WHERE id = ?`
		timestamped = false
	}

	now := utils.NowISO8601()
	for _, id := range ids {
		args := []any{status, id}
		if timestamped {
			args = []any{status, now, id}
		}
		if _, err := database.Exec(statement, args...); err != nil {
			t.Fatalf("forcing task %d to %s: %v", id, status, err)
		}
	}
}

// forceSprintOpen puts a sprint into the state `sprint start` leaves it in:
// status OPEN with started_at set. See the note at the top of this file.
func forceSprintOpen(t *testing.T, database *db.DB, sprintID int) {
	t.Helper()

	if _, err := database.Exec(
		`UPDATE sprints SET status = ?, started_at = ? WHERE id = ?`,
		models.SprintOpen, utils.NowISO8601(), sprintID,
	); err != nil {
		t.Fatalf("opening sprint %d: %v", sprintID, err)
	}
}
