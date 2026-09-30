package db

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// CheckSprintMembershipTx is the sprint membership guard: the one function that
// checks the sprint membership invariant (SPEC/DATABASE.md § Sprint Membership
// Invariant Enforcement; SPEC/STATE_MACHINE.md § Sprint Membership and the
// BACKLOG Status is canonical for the invariant itself).
//
// The invariant has two halves, and the guard checks both:
//
//  1. A sprint member is never in BACKLOG status.
//  2. A task in SPRINT, DOING or TESTING status belongs to a sprint.
//
// It is given the ids of the tasks a write changed and runs inside that write's
// transaction, after the write's last statement that changes a status or a
// membership and before the commit. It reads the status and membership each of
// those tasks has reached THROUGH THE TRANSACTION, so it judges the state the
// write is about to commit rather than any single statement, and a write path
// may issue its statements in any order that reaches a valid state. A task id
// that names no task (one the write deleted, for instance) is judged as nothing:
// there is no row for it to break either half.
//
// The read is one set-based statement per chunk of ids, like every other IN list
// of this package, and it returns at most the first task at fault in id order.
//
// A violation is a defect in the write path, never bad input: every command
// refuses a request that would break the invariant, with its own published
// line, before it writes anything. The guard therefore reports it as a failure
// of the roadmap database's content, utils.ErrDatabase (exit code 1), with the
// line "database error: sprint membership invariant violated: <detail>", and
// the caller's transaction is rolled back, so no row of the write is committed.
//
// The business rule lives here, in application code, and nowhere in the schema:
// no trigger enforces it, for this or any other rule (SPEC/DATABASE.md
// § Business Rules Are Enforced by Application Code).
func CheckSprintMembershipTx(tx *sql.Tx, taskIDs []int) error {
	for start := 0; start < len(taskIDs); start += sprintMembershipGuardChunk {
		chunk := taskIDs[start:min(start+sprintMembershipGuardChunk, len(taskIDs))]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}

		var (
			taskID   int
			status   models.TaskStatus
			sprintID sql.NullInt64
		)
		err := tx.QueryRow(sprintMembershipGuardQuery(generatePlaceholders(len(chunk))), args...).
			Scan(&taskID, &status, &sprintID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("checking the sprint membership invariant: %w", err)
		}
		return sprintMembershipViolation(taskID, status, sprintID)
	}
	return nil
}

// sprintMembershipViolation renders the one task the guard found at fault, and
// the state the write would have left it in, as the ErrDatabase failure the
// SPEC publishes.
func sprintMembershipViolation(taskID int, status models.TaskStatus, sprintID sql.NullInt64) error {
	var detail string
	if sprintID.Valid {
		detail = fmt.Sprintf("task %d would be %s in sprint #%d; a sprint member is never in %s",
			taskID, status, sprintID.Int64, models.StatusBacklog)
	} else {
		detail = fmt.Sprintf("task %d would be %s and belong to no sprint; a task in %s, %s or %s belongs to a sprint",
			taskID, status, models.StatusSprint, models.StatusDoing, models.StatusTesting)
	}
	return fmt.Errorf("%w: sprint membership invariant violated: %s", utils.ErrDatabase, detail)
}

// sprintMembershipGuardChunk is the largest number of task ids one guard read
// binds: the batch size of every chunked IN list in this package.
const sprintMembershipGuardChunk = 100

// sprintMembershipGuardQuery returns the guard's read for an IN list of the
// given placeholders. The LEFT JOIN keeps a task with no membership row, whose
// sprint_id then reads NULL; sprint_tasks declares task_id UNIQUE, so the join
// yields at most one row per task. The WHERE clause keeps only the tasks that
// break one of the two halves, and the first of them in id order is returned.
func sprintMembershipGuardQuery(placeholders string) string {
	return fmt.Sprintf( // #nosec G201 -- only ? placeholders are interpolated; every id is bound
		`SELECT t.id, t.status, st.sprint_id
		   FROM tasks t LEFT JOIN sprint_tasks st ON st.task_id = t.id
		  WHERE t.id IN (%s)
		    AND ((st.task_id IS NOT NULL AND t.status = '`+string(models.StatusBacklog)+`')
		      OR (st.task_id IS NULL AND t.status IN `+sqlActiveTaskStatuses+`))
		  ORDER BY t.id ASC
		  LIMIT 1`,
		placeholders,
	)
}

// allTaskIDsTx reads the id of every task of the roadmap, in ascending order,
// through tx. The migration to schema 1.16.0 hands them to the guard so that it
// verifies the whole roadmap before it commits (SPEC/VERSION.md § Migration
// 1.15.0 → 1.16.0, step 3).
func allTaskIDsTx(tx *sql.Tx) ([]int, error) {
	return scanIDs(tx, "SELECT id FROM tasks ORDER BY id ASC", nil)
}

// SprintMemberIDsTx reads the ids of every member of sprint sprintID through
// tx, in ascending order. `sprint remove` reads them while the membership rows
// still exist, so that it can reset the former members by id once the rows are
// gone (SPEC/DATABASE.md § Clear All Tasks from Sprint).
func SprintMemberIDsTx(tx *sql.Tx, sprintID int) ([]int, error) {
	ids, err := scanIDs(tx, "SELECT task_id FROM sprint_tasks WHERE sprint_id = ? ORDER BY task_id ASC", []any{sprintID})
	if err != nil {
		return nil, fmt.Errorf("reading the sprint members: %w", err)
	}
	return ids, nil
}

// CompletedSprintMembersTx reads the ids of the COMPLETED members of sprint
// sprintID through tx, in ascending order. `sprint remove` refuses a sprint
// that holds any, because a completed task stays in the sprint it was
// completed in (SPEC/COMMANDS.md § Remove Sprint).
func CompletedSprintMembersTx(tx *sql.Tx, sprintID int) ([]int, error) {
	ids, err := scanIDs(tx,
		`SELECT st.task_id FROM sprint_tasks st INNER JOIN tasks t ON t.id = st.task_id
		  WHERE st.sprint_id = ? AND t.status = '`+string(models.StatusCompleted)+`'
		  ORDER BY st.task_id ASC`,
		[]any{sprintID})
	if err != nil {
		return nil, fmt.Errorf("reading the completed sprint members: %w", err)
	}
	return ids, nil
}
