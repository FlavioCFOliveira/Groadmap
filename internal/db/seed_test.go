package db

import (
	"database/sql"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file holds the fixture seeds of this package, and it exists so that
// every one of them writes its rows with the statements the binary writes.
//
// The package used to seed through CreateTask, CreateSprint and the rest of a
// generation of db-layer write methods that the command layer had replaced
// with its own transactions. Nothing in the binary reached them, so the rows
// these tests read back were produced by SQL the product does not run, and a
// test could pass against a copy that had drifted from the shipped one — which
// is exactly what happened to the unreachable sprint delete (task #176) and to
// the unreachable sprint removal, which still deleted membership by task_id
// alone after finding #40 had fixed the shipped path. The methods are gone
// (task #188); these seeds are the replacement.
//
// Two kinds of helper live here, and the difference matters:
//
//   - seedTask and seedSprint go through InsertTaskTx / InsertSprintTx and
//     NextSprintOrderTx, which is the code `task create` and `sprint create`
//     run. A defect injected into those functions fails these fixtures, which
//     is the property that makes them worth having.
//
//   - forceTaskLifecycle and forceSprintOpen write columns directly, and are
//     used only for the states a create cannot produce. The transitions that
//     produce them live in internal/commands, inseparable from the rules that
//     admit them and the audit entries they owe, and this package cannot reach
//     the command layer: internal/commands imports internal/db. What a fixture
//     needs here is the resulting row, not the rule, so it says so in SQL
//     rather than through a second implementation of the transition.

// seedTask inserts one task through InsertTaskTx — the statement `task create`
// runs — in a transaction of its own, and returns the new id.
//
// An empty Type becomes TASK, which is what the CLI's --type default supplies;
// every other field is written exactly as given, including a status later than
// BACKLOG, so a fixture can state the state it needs in one statement.
func seedTask(database *DB, task *models.Task) (int, error) {
	if task.Type == "" {
		task.Type = models.TypeTask
	}

	var id int
	err := database.WithTransaction(func(tx *sql.Tx) error {
		var insertErr error
		id, insertErr = InsertTaskTx(tx, task)
		return insertErr
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// seedSprint inserts one sprint through InsertSprintTx, taking the execution
// order from NextSprintOrderTx when the fixture does not name one, exactly as
// `sprint create` does. The assigned order is written back to sprint.Order.
// The order is written back only after the insert commits. WithTransaction
// retries a locked transaction, and an attempt that recorded its order on the
// struct would reuse a value another creation may since have taken; recomputing
// per attempt is what `sprint create` does, for the same reason.
func seedSprint(database *DB, sprint *models.Sprint) (int, error) {
	var id, order int
	err := database.WithTransaction(func(tx *sql.Tx) error {
		order = sprint.Order
		if order <= 0 {
			next, orderErr := NextSprintOrderTx(tx)
			if orderErr != nil {
				return orderErr
			}
			order = next
		}

		insert := *sprint
		insert.Order = order

		var insertErr error
		id, insertErr = InsertSprintTx(tx, &insert)
		return insertErr
	})
	if err != nil {
		return 0, err
	}
	sprint.Order = order
	return id, nil
}

// mustSeedTask is seedTask for the fixtures that cannot carry on after a
// failed seed.
func mustSeedTask(t *testing.T, database *DB, task *models.Task) int {
	t.Helper()

	id, err := seedTask(database, task)
	if err != nil {
		t.Fatalf("seeding task %q: %v", task.Title, err)
	}
	return id
}

// mustSeedSprint is seedSprint for the fixtures that cannot carry on after a
// failed seed.
func mustSeedSprint(t *testing.T, database *DB, sprint *models.Sprint) int {
	t.Helper()

	id, err := seedSprint(database, sprint)
	if err != nil {
		t.Fatalf("seeding sprint %q: %v", sprint.Title, err)
	}
	return id
}

// forceTaskLifecycle puts the named tasks into a state a created task cannot
// be in: a status past BACKLOG together with the timestamp that status implies.
// See the note at the top of this file for why it is written as direct SQL.
//
// The timestamp column follows SPEC/STATE_MACHINE.md: DOING sets started_at,
// TESTING sets tested_at, COMPLETED sets closed_at, and BACKLOG clears all
// three.
func forceTaskLifecycle(t *testing.T, database *DB, ids []int, status models.TaskStatus) {
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
func forceSprintOpen(t *testing.T, database *DB, sprintID int) {
	t.Helper()

	if _, err := database.Exec(
		`UPDATE sprints SET status = ?, started_at = ? WHERE id = ?`,
		models.SprintOpen, utils.NowISO8601(), sprintID,
	); err != nil {
		t.Fatalf("opening sprint %d: %v", sprintID, err)
	}
}
