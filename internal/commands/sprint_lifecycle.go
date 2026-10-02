package commands

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// sprintStart starts a sprint.
func sprintStart(args []string) error {
	return sprintLifecycle(args, models.SprintOpen, models.OpSprintStart, false, func(s models.SprintStatus) bool {
		return s.CanStart()
	}, "cannot start sprint with status %s")
}

// sprintClose closes a sprint, blocking if tasks are still DOING or TESTING unless --force is given.
func sprintClose(args []string) error {
	// Parse --force flag before delegating to lifecycle. Like every flag, it
	// takes at most one occurrence (SPEC/COMMANDS.md § Repeated Flags).
	force := false
	var seen utils.FlagOccurrences
	filtered := args[:0:len(args)]
	for _, a := range args {
		if a == "--force" {
			if err := seen.Note("--force", a); err != nil {
				return err
			}
			force = true
		} else {
			filtered = append(filtered, a)
		}
	}
	return sprintLifecycle(filtered, models.SprintClosed, models.OpSprintClose, force, func(s models.SprintStatus) bool {
		return s.CanClose()
	}, "cannot close sprint with status %s")
}

// sprintReopen reopens a sprint.
func sprintReopen(args []string) error {
	return sprintLifecycle(args, models.SprintOpen, models.OpSprintReopen, false, func(s models.SprintStatus) bool {
		return s.CanReopen()
	}, "cannot reopen sprint with status %s")
}

// alreadyOpenError is the refusal of `sprint start` or `sprint reopen` while
// another sprint is OPEN (SPEC/COMMANDS.md § Sprint Lifecycle, State refusals).
func alreadyOpenError(openID int) error {
	return fmt.Errorf("%w: sprint #%d is already open — close it first", utils.ErrValidation, openID)
}

// buildSprintUpdateQuery builds the UPDATE query and args for sprint status change.
func buildSprintUpdateQuery(newStatus models.SprintStatus, currentStatus models.SprintStatus, now string, sprintID int) (string, []any) {
	switch newStatus {
	case models.SprintOpen:
		if currentStatus == models.SprintClosed {
			return "UPDATE sprints SET status = ?, closed_at = NULL WHERE id = ?", []any{newStatus, sprintID}
		}
		return "UPDATE sprints SET status = ?, started_at = ? WHERE id = ?", []any{newStatus, now, sprintID}
	case models.SprintClosed:
		return "UPDATE sprints SET status = ?, closed_at = ? WHERE id = ?", []any{newStatus, now, sprintID}
	}
	return "", nil
}

// execSprintUpdate executes the sprint update and audit logging in a transaction.
func execSprintUpdate(tx *sql.Tx, query string, args []any, sprintID int, op models.AuditOperation, now string) error {
	if query == "" {
		return fmt.Errorf("%w: invalid sprint status", utils.ErrValidation)
	}
	result, err := tx.Exec(query, args...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("%w: sprint %d not found", utils.ErrNotFound, sprintID)
	}
	return db.LogAuditTx(tx, op, models.EntitySprint, sprintID, now)
}

// sprintLifecycle handles sprint lifecycle state transitions (start, close, reopen).
//
// Parameters:
//   - args: Command-line arguments including sprint ID
//   - newStatus: The target status to transition to (OPEN, CLOSED)
//   - op: The audit operation type to log (OpSprintStart, OpSprintClose, OpSprintReopen)
//   - force: When true, bypass the active-task safety check on close
//   - canTransition: Function that validates if the transition is allowed from current status
//   - errorMsg: Error message template if transition is not allowed
//
// Required arguments:
//   - sprint ID: The ID of the sprint to transition (first positional argument)
//
// Valid status transitions:
//   - PENDING → OPEN (start sprint)
//   - OPEN → CLOSED (close sprint)
//   - CLOSED → OPEN (reopen sprint)
//
// Error conditions:
//   - Returns utils.ErrRequired if sprint ID is missing
//   - Returns utils.ErrNotFound if sprint doesn't exist
//   - Returns utils.ErrValidation if transition is not allowed
//   - Returns utils.ErrValidation if close attempted with DOING/TESTING tasks and force=false
//
// Side effects:
//   - Updates sprint status in database
//   - Sets started_at timestamp when transitioning to OPEN from PENDING
//   - Sets closed_at timestamp when transitioning to CLOSED
//   - Clears closed_at when reopening (transitioning CLOSED → OPEN)
//   - Logs audit entry for the operation
//   - Outputs updated sprint as JSON to stdout
//
// Complexity: O(1) - single database transaction
func sprintLifecycle(args []string, newStatus models.SprintStatus, op models.AuditOperation, force bool, canTransition func(models.SprintStatus) bool, errorMsg string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}
	remaining, strays := splitPositionals(remaining, 1)
	if len(remaining) == 0 {
		return fmt.Errorf("%w: sprint ID required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], utils.FieldSprintID)
	if err != nil {
		return err
	}
	// A "-"-prefixed token after the id stands in no slot and is refused before
	// the roadmap is opened (SPEC/COMMANDS.md § Positional Arguments).
	if err := rejectUnknownFlags(strays); err != nil {
		return err
	}
	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	sprint, err := database.GetSprint(ctx, sprintID)
	if err != nil {
		return err
	}
	if !canTransition(sprint.Status) {
		msg := fmt.Sprintf(errorMsg, sprint.Status)
		return fmt.Errorf("%w: %s", utils.ErrValidation, msg)
	}

	// Prevent opening a sprint when another is already OPEN (task #77).
	if newStatus == models.SprintOpen {
		if open, err := database.GetOpenSprint(ctx); err == nil {
			return alreadyOpenError(open.ID)
		}
	}

	// Block close when tasks are still SPRINT, DOING or TESTING unless --force is given.
	if newStatus == models.SprintClosed {
		activeTasks, err := database.GetActiveSprintTaskStates(ctx, sprintID)
		if err != nil {
			return fmt.Errorf("checking active tasks: %w", err)
		}
		if len(activeTasks) > 0 {
			ids := make([]string, len(activeTasks))
			for i := range activeTasks {
				ids[i] = fmt.Sprintf("#%d (%s)", activeTasks[i].ID, activeTasks[i].Status)
			}
			if !force {
				return fmt.Errorf("%w: sprint #%d has %d active task(s) still in progress: %s — use --force to close anyway",
					utils.ErrValidation, sprintID, len(activeTasks), strings.Join(ids, ", "))
			}
			fmt.Fprintf(os.Stderr, "warning: closing sprint #%d with %d incomplete task(s): %s\n",
				sprintID, len(activeTasks), strings.Join(ids, ", "))
		}
	}

	now := utils.NowISO8601()
	return database.WithTransaction(func(tx *sql.Tx) error {
		// The checks above ran before this transaction, so a concurrent
		// invocation can have changed the sprint, or opened another, since.
		// They are repeated here against the state this transaction reads, so
		// a lost race is refused with the line the sequential case prints for
		// the state the winner left, never with the driver's text for
		// idx_one_open_sprint (SPEC/COMMANDS.md § Sprint Lifecycle, "Concurrent
		// invocations receive the refusals of the sequential case";
		// SPEC/ARCHITECTURE.md § Classification of Database Driver Failures,
		// rule 1). A concurrent commit between this read and the write below
		// makes the write fail as busy, and the retry policy runs the whole
		// transaction again, when the read sees it.
		current, err := sprintStatusTx(tx, sprintID)
		if err != nil {
			return err
		}
		if !canTransition(current) {
			return fmt.Errorf("%w: %s", utils.ErrValidation, fmt.Sprintf(errorMsg, current))
		}
		if newStatus == models.SprintOpen {
			if openID, open, err := otherOpenSprintTx(tx, sprintID); err != nil {
				return err
			} else if open {
				return alreadyOpenError(openID)
			}
		}

		query, queryArgs := buildSprintUpdateQuery(newStatus, current, now, sprintID)
		err = execSprintUpdate(tx, query, queryArgs, sprintID, op, now)
		if err != nil && newStatus == models.SprintOpen && db.IsUniqueConstraintErr(err) {
			// The schema's backstop, idx_one_open_sprint, rejected the write:
			// another sprint became OPEN. It is translated into the refusal of
			// the rule the index guards.
			if openID, open, qerr := otherOpenSprintTx(tx, sprintID); qerr == nil && open {
				return alreadyOpenError(openID)
			}
		}
		return err
	})
}

// sprintStatusTx reads the status of sprint id inside tx, or reports the
// sprint's not-found refusal when it no longer exists.
func sprintStatusTx(tx *sql.Tx, id int) (models.SprintStatus, error) {
	var status string
	if err := tx.QueryRow("SELECT status FROM sprints WHERE id = ?", id).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: sprint %d", utils.ErrNotFound, id)
		}
		return "", fmt.Errorf("reading sprint status: %w", err)
	}
	return models.SprintStatus(status), nil
}

// otherOpenSprintTx reports the id of an OPEN sprint other than id, read inside
// tx, and whether there is one.
func otherOpenSprintTx(tx *sql.Tx, id int) (int, bool, error) {
	var openID int
	err := tx.QueryRow("SELECT id FROM sprints WHERE status = 'OPEN' AND id <> ? LIMIT 1", id).Scan(&openID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("reading open sprint: %w", err)
	}
	return openID, true, nil
}
