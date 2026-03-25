package commands

import (
	"database/sql"
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
	// Parse --force flag before delegating to lifecycle.
	force := false
	filtered := args[:0:len(args)]
	for _, a := range args {
		if a == "--force" {
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

// buildSprintUpdateQuery builds the UPDATE query and args for sprint status change.
func buildSprintUpdateQuery(newStatus models.SprintStatus, currentStatus models.SprintStatus, now string, sprintID int) (string, []interface{}) {
	switch newStatus {
	case models.SprintOpen:
		if currentStatus == models.SprintClosed {
			return "UPDATE sprints SET status = ?, closed_at = NULL WHERE id = ?", []interface{}{newStatus, sprintID}
		}
		return "UPDATE sprints SET status = ?, started_at = ? WHERE id = ?", []interface{}{newStatus, now, sprintID}
	case models.SprintClosed:
		return "UPDATE sprints SET status = ?, closed_at = ? WHERE id = ?", []interface{}{newStatus, now, sprintID}
	}
	return "", nil
}

// execSprintUpdate executes the sprint update and audit logging in a transaction.
func execSprintUpdate(tx *sql.Tx, query string, args []interface{}, sprintID int, op models.AuditOperation, now string) error {
	if query == "" {
		return fmt.Errorf("%w: invalid sprint status", utils.ErrInvalidInput)
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
	_, err = tx.Exec(
		`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
		op, models.EntitySprint, sprintID, now,
	)
	return err
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
//   - Returns utils.ErrInvalidInput if transition is not allowed
//   - Returns utils.ErrInvalidInput if close attempted with DOING/TESTING tasks and force=false
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
	if len(remaining) == 0 {
		return fmt.Errorf("%w: sprint ID required", utils.ErrRequired)
	}

	sprintID, err := utils.ValidateIDString(remaining[0], "sprint")
	if err != nil {
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
		return fmt.Errorf("%w: %s", utils.ErrInvalidInput, msg)
	}

	// Prevent opening a sprint when another is already OPEN (task #77).
	if newStatus == models.SprintOpen {
		if open, err := database.GetOpenSprint(ctx); err == nil {
			return fmt.Errorf("%w: sprint #%d is already open — close it first", utils.ErrInvalidInput, open.ID)
		}
	}

	// Block close when tasks are still SPRINT, DOING or TESTING unless --force is given.
	if newStatus == models.SprintClosed {
		activeTasks, err := database.GetActiveSprintTasks(ctx, sprintID)
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
					utils.ErrInvalidInput, sprintID, len(activeTasks), strings.Join(ids, ", "))
			}
			fmt.Fprintf(os.Stderr, "warning: closing sprint #%d with %d incomplete task(s): %s\n",
				sprintID, len(activeTasks), strings.Join(ids, ", "))
		}
	}

	now := utils.NowISO8601()
	query, queryArgs := buildSprintUpdateQuery(newStatus, sprint.Status, now, sprintID)
	return database.WithTransaction(func(tx *sql.Tx) error {
		return execSprintUpdate(tx, query, queryArgs, sprintID, op, now)
	})
}
