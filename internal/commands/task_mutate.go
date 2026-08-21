package commands

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// taskRemove removes tasks.
func taskRemove(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID(s) required", utils.ErrRequired)
	}

	ids, err := utils.ParseCommaSeparatedIDs(remaining[0], "task")
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

	// Fail-fast: verify all tasks exist and are in BACKLOG before deleting any (task #78).
	tasks, err := database.GetTasks(ctx, ids)
	if err != nil {
		return err
	}
	if len(tasks) != len(ids) {
		return fmt.Errorf("%w: some tasks not found", utils.ErrNotFound)
	}
	for i := range tasks {
		if tasks[i].Status != models.StatusBacklog {
			return fmt.Errorf("%w: task #%d cannot be deleted — status is %s, must be BACKLOG", utils.ErrValidation, tasks[i].ID, tasks[i].Status)
		}
	}

	// Guard: prevent deleting tasks that have subtasks. One bulk query.
	subtaskCounts, err := database.CountSubTasksByParents(ctx, ids)
	if err != nil {
		return err
	}
	for i := range tasks {
		if c := subtaskCounts[tasks[i].ID]; c > 0 {
			return fmt.Errorf("%w: task #%d cannot be deleted — it has %d subtask(s); remove them first", utils.ErrValidation, tasks[i].ID, c)
		}
	}

	// Delete within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		for _, id := range ids {
			// Delete task
			result, err := tx.Exec("DELETE FROM tasks WHERE id = ?", id)
			if err != nil {
				return err
			}

			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected == 0 {
				return fmt.Errorf("%w: task %d not found", utils.ErrNotFound, id)
			}

			if err := db.LogAuditTx(tx, models.OpTaskDelete, models.EntityTask, id, utils.NowISO8601()); err != nil {
				return err
			}
		}
		return nil
	})
}

// taskSetStatus changes the status of one or more tasks.
//
// Parameters:
//   - args: Command-line arguments including task IDs and new status
//
// Required arguments:
//   - task IDs: Comma-separated list of task IDs to update (first positional argument)
//   - status: New status value (second positional argument)
//
// Valid manual status transitions (this command):
//   - SPRINT → BACKLOG, DOING
//   - DOING → SPRINT, TESTING
//   - TESTING → DOING, COMPLETED
//   - COMPLETED → BACKLOG (reopen)
//
// BACKLOG → SPRINT is automatic only (via `sprint add-tasks`); manual
// `task stat <ids> SPRINT` is rejected with exit code 6.
//
// Optional flags:
//   - -r, --roadmap: Roadmap name (uses current if not specified)
//
// Error conditions:
//   - Returns utils.ErrRequired if task IDs or status missing
//   - Returns utils.ErrNotFound if task doesn't exist
//   - Returns utils.ErrValidation if status or status transition is invalid
//
// Side effects:
//   - Updates task status in database
//   - Sets started_at and commit_open when transitioning to DOING
//   - Sets tested_at when transitioning to TESTING
//   - Sets closed_at and commit_close when transitioning to COMPLETED
//   - Clears lifecycle dates and commit_close when reopening to BACKLOG,
//     preserving commit_open
//   - Logs one audit entry per task, named for the state the task entered
//     (TASK_STATUS_BACKLOG, TASK_STATUS_DOING, TASK_STATUS_TESTING or
//     TASK_STATUS_COMPLETED), carrying the supplied commit hash on the two
//     transitions that record one
//   - Outputs updated task IDs as JSON to stdout
//
// Complexity: O(n) where n is the number of tasks being updated
//
// Example:
//
//	rmp task set-status -r myproject 1,2,3 DOING --commit-open 5f93b51
func taskSetStatus(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	// Extract --summary / -s, --commit-open / -co and --commit-close / -cc
	// before positional arg parsing.
	// Fail-fast: all validation happens before any database operation.
	var completionSummary, commitOpen, commitClose *string
	filtered := make([]string, 0, len(remaining))
	for i := 0; i < len(remaining); i++ {
		switch remaining[i] {
		case "--summary", "-s":
			if i+1 >= len(remaining) {
				return fmt.Errorf("%w: --summary requires a value", utils.ErrRequired)
			}
			// Trim leading/trailing whitespace per SPEC/COMMANDS.md.
			s := strings.TrimSpace(remaining[i+1])
			completionSummary = &s
			i++ // consume the value
		case "--commit-open", "-co":
			value, valErr := commitFlagValue("--commit-open", remaining, i)
			if valErr != nil {
				return valErr
			}
			commitOpen = &value
			i++ // consume the value
		case "--commit-close", "-cc":
			value, valErr := commitFlagValue("--commit-close", remaining, i)
			if valErr != nil {
				return valErr
			}
			commitClose = &value
			i++ // consume the value
		default:
			filtered = append(filtered, remaining[i])
		}
	}
	remaining = filtered

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID(s) and status required", utils.ErrRequired)
	}

	ids, err := utils.ParseCommaSeparatedIDs(remaining[0], "task")
	if err != nil {
		return err
	}

	// Parse status — an unrecognised value is a validation failure (exit 6 /
	// ErrValidation per SPEC/ARCHITECTURE.md), not a generic failure (exit 1).
	newStatus, err := models.ParseTaskStatus(remaining[1])
	if err != nil {
		return fmt.Errorf("%w: %w", utils.ErrValidation, err)
	}

	// SPRINT is an automatic transition triggered exclusively by `sprint add-tasks`.
	// Manual `task stat <ids> SPRINT` is rejected per SPEC/STATE_MACHINE.md.
	if newStatus == models.StatusSprint {
		return fmt.Errorf("%w: status SPRINT can only be set automatically via 'sprint add-tasks'", utils.ErrValidation)
	}

	// Fail-fast validation for --summary (step 2: before ID/DB verification).
	// --summary is only meaningful on the TESTING → COMPLETED transition.
	if completionSummary != nil && newStatus != models.StatusCompleted {
		return fmt.Errorf("%w: --summary is only valid when transitioning to COMPLETED", utils.ErrValidation)
	}
	if completionSummary != nil && len(*completionSummary) > models.MaxTaskCompletionSummary {
		return utils.FieldTooLargeError(utils.FieldTaskCompletionSummary, models.MaxTaskCompletionSummary)
	}
	// Reject control / bidi / format code points (SPEC/MODELS.md § Free-Text
	// Control-Character Constraint).
	if completionSummary != nil {
		if err := utils.ValidateNoControlChars(*completionSummary, utils.FieldTaskCompletionSummary); err != nil {
			return err
		}
	}

	// Fail-fast validation for the commit flags (step 4: still before the
	// database is opened, so a rejection here leaves every task of a multi-ID
	// invocation untouched). The four presence checks run in the order
	// SPEC/COMMANDS.md § Change Status (stat) makes normative, and between them
	// they leave at most one flag in play — which is why the format check that
	// follows needs no ordering between the two flags.
	if commitOpen != nil && newStatus != models.StatusDoing {
		return utils.ValidationMessage("--commit-open flag is only allowed when transitioning to DOING")
	}
	if commitClose != nil && newStatus != models.StatusCompleted {
		return utils.ValidationMessage("--commit-close flag is only allowed when transitioning to COMPLETED")
	}
	if newStatus == models.StatusDoing && commitOpen == nil {
		return utils.ValidationMessage("--commit-open is required when transitioning to DOING")
	}
	if newStatus == models.StatusCompleted && commitClose == nil {
		return utils.ValidationMessage("--commit-close is required when transitioning to COMPLETED")
	}
	switch {
	case commitOpen != nil:
		normalised, hashErr := normalizeCommitFlag("--commit-open", *commitOpen)
		if hashErr != nil {
			return hashErr
		}
		commitOpen = &normalised
	case commitClose != nil:
		normalised, hashErr := normalizeCommitFlag("--commit-close", *commitClose)
		if hashErr != nil {
			return hashErr
		}
		commitClose = &normalised
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	// Validate status transitions using batch query (O(1) vs N+1)
	tasks, err := database.GetTasks(ctx, ids)
	if err != nil {
		return err
	}
	if len(tasks) != len(ids) {
		return fmt.Errorf("%w: some tasks not found", utils.ErrNotFound)
	}
	for i := range tasks {
		if !tasks[i].Status.CanTransitionTo(newStatus) {
			return fmt.Errorf("%w: invalid status transition from %s to %s for task %d", utils.ErrValidation, tasks[i].Status, newStatus, tasks[i].ID)
		}
	}

	// Guard: when transitioning to COMPLETED, ensure all subtasks and
	// dependencies are also COMPLETED. Two bulk queries cover all IDs.
	if newStatus == models.StatusCompleted {
		incompleteByParent, err := database.GetIncompleteSubTasksByParents(ctx, ids)
		if err != nil {
			return err
		}
		incompleteDepsByTask, err := database.GetIncompleteDependenciesByTasks(ctx, ids)
		if err != nil {
			return err
		}
		for i := range tasks {
			if blocking := incompleteByParent[tasks[i].ID]; len(blocking) > 0 {
				idStrsBlocking := make([]string, len(blocking))
				for j, id := range blocking {
					idStrsBlocking[j] = fmt.Sprintf("#%d", id)
				}
				return fmt.Errorf("%w: cannot mark task #%d as COMPLETED: incomplete subtasks: %s",
					utils.ErrValidation, tasks[i].ID, strings.Join(idStrsBlocking, ", "))
			}
			if deps := incompleteDepsByTask[tasks[i].ID]; len(deps) > 0 {
				depStrs := make([]string, len(deps))
				for j, id := range deps {
					depStrs[j] = fmt.Sprintf("#%d", id)
				}
				return fmt.Errorf("%w: cannot mark task #%d as COMPLETED: incomplete dependencies: %s",
					utils.ErrValidation, tasks[i].ID, strings.Join(depStrs, ", "))
			}
		}
	}

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	// Update within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		// Build update query based on target status for lifecycle date tracking
		// Per SPEC/STATE_MACHINE.md:
		// - DOING: set started_at and commit_open
		// - TESTING: set tested_at
		// - COMPLETED: set closed_at, completion_summary (nil → NULL) and commit_close
		// - BACKLOG: clear all tracking dates, completion_summary (task #96) and
		//   commit_close, preserving commit_open
		var query string
		var args []any

		// The audit operation names the DESTINATION state, and it is decided by
		// the same switch that decides the UPDATE, so the row and the columns
		// it describes can never disagree about where the task went
		// (SPEC/COMMANDS.md § Change Status (stat), Audit). auditOpts carries
		// the commit hash on the two transitions that record one and stays
		// empty on the others.
		var auditOp models.AuditOperation
		var auditOpts []db.AuditOption

		placeholders := database.Placeholders(len(ids))

		switch newStatus {
		case models.StatusDoing:
			// Transition to DOING: set started_at and commit_open. Validation
			// above guarantees commitOpen is non-nil and already lowercase, so
			// this statement never writes NULL to commit_open. A re-entry from
			// TESTING runs the same statement and replaces the earlier value.
			query = fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
				"UPDATE tasks SET status = ?, started_at = ?, commit_open = ? WHERE id IN (%s)",
				placeholders,
			)
			args = append([]any{newStatus, now, *commitOpen}, makeInterfaceSlice(ids)...)
			// The audit row takes the same normalised value the column takes,
			// from the same variable, so the two cannot drift apart.
			auditOp = models.OpTaskStatusDoing
			auditOpts = []db.AuditOption{db.WithCommitHash(*commitOpen)}

		case models.StatusTesting:
			// Transition to TESTING: set tested_at. Neither commit column changes.
			query = fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
				"UPDATE tasks SET status = ?, tested_at = ? WHERE id IN (%s)",
				placeholders,
			)
			args = append([]any{newStatus, now}, makeInterfaceSlice(ids)...)
			auditOp = models.OpTaskStatusTesting

		case models.StatusCompleted:
			// Transition to COMPLETED: set closed_at, completion_summary and commit_close.
			// completionSummary is *string: nil becomes SQL NULL, non-nil becomes the string value.
			// commit_close is mandatory, so it is always a value and never NULL.
			query = fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
				"UPDATE tasks SET status = ?, closed_at = ?, completion_summary = ?, commit_close = ? WHERE id IN (%s)",
				placeholders,
			)
			args = append([]any{newStatus, now, completionSummary, *commitClose}, makeInterfaceSlice(ids)...)
			auditOp = models.OpTaskStatusCompleted
			auditOpts = []db.AuditOption{db.WithCommitHash(*commitClose)}

		case models.StatusBacklog:
			// Reopening to BACKLOG: clear all tracking dates, the completion
			// summary and commit_close for a fresh cycle. commit_open is
			// deliberately absent from the SET list — the commit the work
			// started from stays a true historical fact, while the commit it
			// was concluded at is invalidated by the reopening
			// (SPEC/STATE_MACHINE.md § Commit Tracking Fields).
			query = fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
				"UPDATE tasks SET status = ?, started_at = NULL, tested_at = NULL, closed_at = NULL, completion_summary = NULL, commit_close = NULL WHERE id IN (%s)",
				placeholders,
			)
			args = append([]any{newStatus}, makeInterfaceSlice(ids)...)
			// No sprint is party to a `task stat` invocation, so this row names
			// no counterpart. The same TASK_STATUS_BACKLOG operation written by
			// `sprint remove-tasks` does name one, because there the sprint is
			// the counterpart (SPEC/DATABASE.md § The Two Entities of a
			// Relational Operation).
			auditOp = models.OpTaskStatusBacklog

		default:
			// Unreachable, and a guard rather than a fall-through. ParseTaskStatus
			// admits five values, the SPRINT target is rejected before the database
			// is opened, and the four cases above cover the rest. A generic "just
			// update the status" branch would let a sixth state reach the audit
			// write with no operation of its own and store a row that names no
			// destination, which is the one thing a destination-named catalogue
			// cannot express (SPEC/DATABASE.md § One Row per Thing That Happened).
			return fmt.Errorf("%w: no status update is defined for %s", utils.ErrValidation, newStatus)
		}

		_, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}

		// One row per task, each naming its own task and all sharing the one
		// timestamp captured for the invocation. The write is inside the same
		// transaction as the UPDATE above, so a batch that fails anywhere
		// leaves the audit table untouched.
		for _, id := range ids {
			if err := db.LogAuditTx(tx, auditOp, models.EntityTask, id, now, auditOpts...); err != nil {
				return err
			}
		}
		return nil
	})
}

// commitFlagValue returns the value written after a commit-hash flag found at
// position i of args.
//
// A flag written with nothing after it is a malformed command line, reported as
// utils.ErrRequired (exit code 2). The SPEC keeps that case distinct from an
// absent flag, which is a rejected transition (exit code 6): see the last two
// rows of SPEC/COMMANDS.md § Change Status (stat), Batch Operation Behavior.
//
// The value is taken verbatim. Unlike --summary it is not trimmed, so a value
// carrying whitespace is reported as a malformed hash rather than silently
// repaired into a valid one.
func commitFlagValue(flag string, args []string, i int) (string, error) {
	if i+1 >= len(args) {
		return "", &utils.MessageError{
			Msg:       flag + " requires a value",
			Sentinels: []error{utils.ErrRequired},
		}
	}
	return args[i+1], nil
}

// normalizeCommitFlag validates one commit-hash flag value and returns it in the
// stored, lowercase form.
//
// models.NormalizeCommitHash is the single validator for the format; this
// function only re-dresses its rejection in the message SPEC/COMMANDS.md
// § Change Status (stat) mandates verbatim, naming the flag the caller wrote.
// The rejected value is rendered with %q so a hash carrying control characters
// cannot reach the terminal raw. The original error is kept in the sentinel
// chain, so errors.Is still finds both utils.ErrValidation (exit code 6) and
// models.ErrInvalidCommitHash.
func normalizeCommitFlag(flag, value string) (string, error) {
	normalised, err := models.NormalizeCommitHash(value)
	if err != nil {
		return "", &utils.MessageError{
			Msg: fmt.Sprintf("invalid commit hash for %s: %q (expected %d to %d hexadecimal characters)",
				flag, value, models.MinCommitHashLength, models.MaxCommitHashLength),
			Sentinels: []error{utils.ErrValidation, err},
		}
	}
	return normalised, nil
}

// taskReopen transitions one or more tasks back to BACKLOG, clearing all
// lifecycle timestamps, the completion summary and commit_close, and preserving
// commit_open.
// Tasks already in BACKLOG are skipped with an informational message.
// Accepts comma-separated IDs with fail-fast on any invalid ID.
func taskReopen(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID(s) required", utils.ErrRequired)
	}

	ids, err := utils.ParseCommaSeparatedIDs(remaining[0], "task")
	if err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	tasks, err := database.GetTasks(ctx, ids)
	if err != nil {
		return err
	}
	if len(tasks) != len(ids) {
		return fmt.Errorf("%w: some tasks not found", utils.ErrNotFound)
	}

	// Separate already-BACKLOG tasks from tasks that need transition.
	// Track which tasks are in sprint-associated states so we can clean up sprint_tasks rows.
	var toReopen []int
	var toRemoveFromSprint []int
	for i := range tasks {
		if tasks[i].Status == models.StatusBacklog {
			fmt.Fprintf(os.Stderr, "task #%d is already in BACKLOG\n", tasks[i].ID)
			continue
		}
		toReopen = append(toReopen, tasks[i].ID)
		// Tasks in SPRINT, DOING, or TESTING have a row in sprint_tasks that must be removed.
		if tasks[i].Status == models.StatusSprint || tasks[i].Status == models.StatusDoing || tasks[i].Status == models.StatusTesting {
			toRemoveFromSprint = append(toRemoveFromSprint, tasks[i].ID)
		}
	}

	if len(toReopen) == 0 {
		return nil
	}

	now := utils.NowISO8601()

	return database.WithTransaction(func(tx *sql.Tx) error {
		// commit_close is cleared with the lifecycle timestamps and the
		// completion summary; commit_open is preserved, which is why it is
		// absent from the SET list (SPEC/STATE_MACHINE.md § Commit Tracking
		// Fields, rules 4 and 5).
		query := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
			"UPDATE tasks SET status = ?, started_at = NULL, tested_at = NULL, closed_at = NULL, completion_summary = NULL, commit_close = NULL WHERE id IN (%s)",
			database.Placeholders(len(toReopen)),
		)
		args := append([]any{models.StatusBacklog}, makeInterfaceSlice(toReopen)...)
		if _, err := tx.Exec(query, args...); err != nil {
			return err
		}

		// Remove sprint_tasks rows for tasks that were associated with a sprint.
		if len(toRemoveFromSprint) > 0 {
			delQuery := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
				"DELETE FROM sprint_tasks WHERE task_id IN (%s)",
				database.Placeholders(len(toRemoveFromSprint)),
			)
			if _, err := tx.Exec(delQuery, makeInterfaceSlice(toRemoveFromSprint)...); err != nil {
				return err
			}
		}

		for _, id := range toReopen {
			if err := db.LogAuditTx(tx, models.OpTaskReopen, models.EntityTask, id, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// makeInterfaceSlice converts []int to []interface{}
func makeInterfaceSlice(ids []int) []any {
	result := make([]any, len(ids))
	for i, id := range ids {
		result[i] = id
	}
	return result
}

// taskSetPriority sets task priority.
func taskSetPriority(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID(s) and priority required", utils.ErrRequired)
	}

	ids, err := utils.ParseCommaSeparatedIDs(remaining[0], "task")
	if err != nil {
		return err
	}

	priority, err := strconv.Atoi(remaining[1])
	if err != nil {
		// A non-numeric priority is a domain value-validation failure
		// (exit 6 / ErrValidation per SPEC/ARCHITECTURE.md): priority is a
		// 0-9 enum-like value, so any token that is not a valid value in
		// that range — numeric out-of-range or non-numeric — is invalid data.
		return fmt.Errorf("%w: invalid priority: must be 0-9", utils.ErrValidation)
	}
	if err := utils.ValidateNumericRange(priority, 0, 9, "priority"); err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Fail-fast: every requested ID must exist before any mutation. Without
	// this, nonexistent IDs returned exit 0, mutated valid tasks in a mixed
	// batch, and wrote phantom audit rows for IDs that do not exist
	// (SPEC/COMMANDS.md § Change Priority). Mirrors task remove/stat/reopen.
	tasks, err := database.GetTasks(ctx, ids)
	if err != nil {
		return err
	}
	if len(tasks) != len(ids) {
		return fmt.Errorf("%w: some tasks not found", utils.ErrNotFound)
	}

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	// Update within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		args := append([]any{priority}, makeInterfaceSlice(ids)...)
		query := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
			"UPDATE tasks SET priority = ? WHERE id IN (%s)", database.Placeholders(len(ids)))
		if _, err := tx.Exec(query, args...); err != nil {
			return err
		}

		// Log audit with same timestamp
		for _, id := range ids {
			if err := db.LogAuditTx(tx, models.OpTaskPriorityChange, models.EntityTask, id, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// taskSetSeverity sets task severity.
func taskSetSeverity(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID(s) and severity required", utils.ErrRequired)
	}

	ids, err := utils.ParseCommaSeparatedIDs(remaining[0], "task")
	if err != nil {
		return err
	}

	severity, err := strconv.Atoi(remaining[1])
	if err != nil {
		// A non-numeric severity is a domain value-validation failure
		// (exit 6 / ErrValidation per SPEC/ARCHITECTURE.md): severity is a
		// 0-9 enum-like value, so any token that is not a valid value in
		// that range — numeric out-of-range or non-numeric — is invalid data.
		return fmt.Errorf("%w: invalid severity: must be 0-9", utils.ErrValidation)
	}
	if err := utils.ValidateNumericRange(severity, 0, 9, "severity"); err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Fail-fast: every requested ID must exist before any mutation. Without
	// this, nonexistent IDs returned exit 0, mutated valid tasks in a mixed
	// batch, and wrote phantom audit rows for IDs that do not exist
	// (SPEC/COMMANDS.md § Change Severity). Mirrors task remove/stat/reopen.
	tasks, err := database.GetTasks(ctx, ids)
	if err != nil {
		return err
	}
	if len(tasks) != len(ids) {
		return fmt.Errorf("%w: some tasks not found", utils.ErrNotFound)
	}

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	// Update within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		args := append([]any{severity}, makeInterfaceSlice(ids)...)
		query := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
			"UPDATE tasks SET severity = ? WHERE id IN (%s)", database.Placeholders(len(ids)))
		if _, err := tx.Exec(query, args...); err != nil {
			return err
		}

		// Log audit with same timestamp
		for _, id := range ids {
			if err := db.LogAuditTx(tx, models.OpTaskSeverityChange, models.EntityTask, id, now); err != nil {
				return err
			}
		}
		return nil
	})
}
