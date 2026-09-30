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
//
// Every named task must be in BACKLOG, and a BACKLOG task belongs to no sprint
// under the sprint membership invariant (SPEC/STATE_MACHINE.md § Sprint
// Membership and the BACKLOG Status), so the ON DELETE CASCADE of sprint_tasks
// removes no membership row and no sprint's positions change (SPEC/DATABASE.md
// § Position Density Within a Sprint, `task remove`).
func taskRemove(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}
	remaining, strays := splitPositionals(remaining, 1)

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID(s) required", utils.ErrRequired)
	}

	ids, err := utils.ParseCommaSeparatedIDs(remaining[0], utils.FieldTaskID)
	if err != nil {
		return err
	}

	// A repeated id names the same task each time, so the list is reduced to the
	// set it denotes before anything downstream reads it: the membership test,
	// the UPDATE's IN clause and the audit loop all then operate once per
	// distinct task, as SPEC/COMMANDS.md § Task ID Lists (Batch Commands)
	// requires. Deduplicating here rather than inside ParseCommaSeparatedIDs is
	// deliberate -- `sprint reorder` shares that parser and a repeat is a real
	// error there, which silent deduplication would hide.
	ids = utils.DistinctIDs(ids)

	// A "-"-prefixed token after the ids stands in no slot and is refused here,
	// after the checks on the ids and before the roadmap is opened
	// (SPEC/COMMANDS.md § Positional Arguments).
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

	// Fail-fast: verify all tasks exist and are in BACKLOG before deleting any (task #78).
	tasks, err := database.GetTaskStates(ctx, ids)
	if err != nil {
		return err
	}
	if err := utils.TasksNotFoundError(utils.MissingIDs(ids, taskStateIDsOf(tasks))); err != nil {
		return err
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
		audit := db.NewAuditWriter(tx)
		defer audit.Close() //nolint:errcheck // releasing the statement; the transaction releases it too
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

			if err := audit.Log(models.OpTaskDelete, models.EntityTask, id, utils.NowISO8601()); err != nil {
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
//   - SPRINT → DOING
//   - DOING → TESTING
//   - TESTING → DOING, COMPLETED
//
// BACKLOG → SPRINT is automatic only (via `sprint add-tasks`); manual
// `task stat <ids> SPRINT` is rejected with exit code 6.
//
// The BACKLOG target is refused for every sprint member, because a sprint
// member is never in BACKLOG and this command never changes membership: from
// SPRINT, DOING or TESTING the refusal names `sprint remove-tasks`, and from
// COMPLETED it names `task reopen` (SPEC/STATE_MACHINE.md § Valid Transitions,
// § Sprint Membership and the BACKLOG Status).
//
// DOING → SPRINT used to be listed above and never worked: the guard in
// models.CanTransitionTo gives DOING the single target TESTING, and the SPRINT
// rejection below refuses that target from every source state; `task reopen`
// performs that transition. The list is pinned to the guard by
// TestTaskStatDocComment_ListsExactlyTheTransitionsAccepted, which reads it
// back out of this file and compares it against transitions the command was
// observed to accept.
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
//   - Logs one audit entry per task, named for the state the task entered
//     (TASK_STATUS_DOING, TASK_STATUS_TESTING or TASK_STATUS_COMPLETED),
//     carrying the supplied commit hash on the two transitions that record one
//   - Runs the sprint membership guard before commit
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
			// Recorded AS SUPPLIED. The trim belongs to the validation
			// sequence further down, and it must not happen here: this site
			// used to trim at extraction, so by the time the
			// control-character rule ran a leading or trailing VT (0x0B) or
			// FF (0x0C) had already been removed and the value was accepted
			// with the forbidden character silently discarded (measured:
			// `--summary $'\x0bDelivered.'` exited 0 and stored
			// "Delivered."). SPEC/MODELS.md § Free-Text Emptiness and
			// Trimming Constraint fixes the order that closes it.
			s := remaining[i+1]
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
	remaining, strays := splitPositionals(filtered, 2)

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID(s) and status required", utils.ErrRequired)
	}

	ids, err := utils.ParseCommaSeparatedIDs(remaining[0], utils.FieldTaskID)
	if err != nil {
		return err
	}

	// A repeated id names the same task each time, so the list is reduced to the
	// set it denotes before anything downstream reads it: the membership test,
	// the UPDATE's IN clause and the audit loop all then operate once per
	// distinct task, as SPEC/COMMANDS.md § Task ID Lists (Batch Commands)
	// requires. Deduplicating here rather than inside ParseCommaSeparatedIDs is
	// deliberate -- `sprint reorder` shares that parser and a repeat is a real
	// error there, which silent deduplication would hide.
	ids = utils.DistinctIDs(ids)

	// Parse status — an unrecognised value is a validation failure (exit 6 /
	// ErrValidation per SPEC/ARCHITECTURE.md), not a generic failure (exit 1).
	newStatus, err := models.ParseTaskStatus(remaining[1])
	if err != nil {
		return fmt.Errorf("%w: %w", utils.ErrValidation, err)
	}

	// SPRINT is set by `sprint add-tasks`, when a BACKLOG task joins a sprint,
	// and by `task reopen`; manual `task stat <ids> SPRINT` is rejected per
	// SPEC/STATE_MACHINE.md § Valid Transitions, Rejection rule.
	if newStatus == models.StatusSprint {
		return fmt.Errorf("%w: status SPRINT cannot be set by 'task stat'; it is set by 'sprint add-tasks' and 'task reopen'", utils.ErrValidation)
	}

	// A "-"-prefixed token between or after the positional arguments stands in
	// no slot. It is refused after steps 1 and 2 (ids, target state) and before
	// step 3, so no flag value is validated once it has been refused
	// (SPEC/COMMANDS.md § Change Status (stat), § Positional Arguments).
	if err := rejectUnknownFlags(strays); err != nil {
		return err
	}

	// Fail-fast validation for --summary (step 2: before ID/DB verification).
	// --summary is only meaningful on the TESTING → COMPLETED transition.
	if completionSummary != nil && newStatus != models.StatusCompleted {
		return fmt.Errorf("%w: --summary is only valid when transitioning to COMPLETED", utils.ErrValidation)
	}
	// The whole free-text sequence for --summary, through the one helper that
	// owns its order (rmp task 302): the LENGTH cap on the value as it will be
	// stored, then the encoding rule and then the control-character rule on the
	// value AS SUPPLIED, and only then the trim (SPEC/MODELS.md § Free-Text UTF-8
	// Encoding Constraint, § Free-Text Control-Character Constraint, and
	// § Free-Text Emptiness and Trimming Constraint).
	//
	// The cap keeps the position SPEC/COMMANDS.md § Change Status (stat) step 3
	// states for --summary — ahead of the content rules — and still measures the
	// TRIMMED value, because that is the value stored (Rule 2): a summary of
	// exactly the maximum length carrying surrounding whitespace is accepted,
	// and what is counted is what the column holds. What changed is only that
	// this command no longer says so itself; the sequence is stated once, in
	// utils.TrimFreeText, so it cannot drift from the six other write paths.
	//
	// `completion_summary` is the one free-text field Rule 1 does NOT govern: it
	// is optional, and `task stat` accepts a transition to COMPLETED that
	// supplies no --summary at all, so a value that is empty once trimmed is a
	// summary the caller chose not to write and not a violation — which is why
	// this is TrimFreeText and not RequireFreeText.
	if completionSummary != nil {
		stored, textErr := utils.TrimFreeText(*completionSummary,
			utils.FieldTaskCompletionSummary, models.MaxTaskCompletionSummary)
		if textErr != nil {
			return textErr
		}
		completionSummary = &stored
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
	tasks, err := database.GetTaskStates(ctx, ids)
	if err != nil {
		return err
	}
	if err := utils.TasksNotFoundError(utils.MissingIDs(ids, taskStateIDsOf(tasks))); err != nil {
		return err
	}
	// The BACKLOG target is refused for every sprint member, with the line that
	// names the command fitting its status, naming the first refused task in
	// the order the command line supplied them (SPEC/COMMANDS.md § Change
	// Status (stat), Transitioning to BACKLOG).
	if newStatus == models.StatusBacklog {
		if err := backlogTargetRefusal(ids, tasks); err != nil {
			return err
		}
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
		// The BACKLOG target never reaches this point: it is refused above for
		// every sprint member, and BACKLOG → BACKLOG is no transition.
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

		default:
			// Unreachable, and a guard rather than a fall-through. ParseTaskStatus
			// admits five values, the SPRINT target is rejected before the database
			// is opened, the BACKLOG target is refused for every task above, and
			// the three cases above cover the rest. A generic "just
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
		audit := db.NewAuditWriter(tx)
		defer audit.Close() //nolint:errcheck // releasing the statement; the transaction releases it too
		for _, id := range ids {
			if err := audit.Log(auditOp, models.EntityTask, id, now, auditOpts...); err != nil {
				return err
			}
		}

		// The sprint membership guard checks the result before commit
		// (SPEC/DATABASE.md § Sprint Membership Invariant Enforcement).
		return db.CheckSprintMembershipTx(tx, ids)
	})
}

// backlogTargetRefusal returns the refusal of `task stat <ids> BACKLOG` for the
// first named task, in the order ids supplies them, whose status is SPRINT,
// DOING, TESTING or COMPLETED, or nil when there is none. A task in one of
// those statuses is a sprint member, and a sprint member is never in BACKLOG
// (SPEC/STATE_MACHINE.md § Valid Transitions, "task stat BACKLOG target rule").
// The line names the command that fits the status: `sprint remove-tasks` takes
// an active task out of its sprint, and `task reopen` returns a completed task
// to SPRINT, because a completed task stays in its sprint.
func backlogTargetRefusal(ids []int, tasks []db.TaskState) error {
	status := make(map[int]models.TaskStatus, len(tasks))
	for i := range tasks {
		status[tasks[i].ID] = tasks[i].Status
	}
	for _, id := range ids {
		switch status[id] {
		case models.StatusSprint, models.StatusDoing, models.StatusTesting:
			return fmt.Errorf("%w: invalid status transition from %s to %s for task %d: a task leaves its sprint only through 'rmp sprint remove-tasks'",
				utils.ErrValidation, status[id], models.StatusBacklog, id)
		case models.StatusCompleted:
			return fmt.Errorf("%w: invalid status transition from %s to %s for task %d: a completed task is reopened with 'rmp task reopen'",
				utils.ErrValidation, models.StatusCompleted, models.StatusBacklog, id)
		}
	}
	return nil
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

// taskReopen returns one or more tasks to SPRINT inside the sprint each
// belongs to, clearing all lifecycle timestamps, the completion summary and
// commit_close, and preserving commit_open (SPEC/COMMANDS.md § Reopen Task).
// Accepts comma-separated IDs with fail-fast on any invalid ID.
//
// Only a DOING, TESTING or COMPLETED task is reopened. A task already in SPRINT
// or in BACKLOG is skipped with an informational message and no audit entry.
// The command never touches sprint_tasks: the task keeps its sprint and its
// position. A task whose sprint is CLOSED is refused, before anything is
// written, because a closed sprint takes no work back. A COMPLETED task that
// belongs to no sprint, which only data written before the sprint membership
// invariant can hold, returns to BACKLOG instead, the one status a task outside
// every sprint may hold (SPEC/STATE_MACHINE.md § Sprint Membership and the
// BACKLOG Status).
func taskReopen(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}
	remaining, strays := splitPositionals(remaining, 1)

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID(s) required", utils.ErrRequired)
	}

	ids, err := utils.ParseCommaSeparatedIDs(remaining[0], utils.FieldTaskID)
	if err != nil {
		return err
	}

	// A repeated id names the same task each time, so the list is reduced to the
	// set it denotes before anything downstream reads it: the membership test,
	// the UPDATE's IN clause and the audit loop all then operate once per
	// distinct task, as SPEC/COMMANDS.md § Task ID Lists (Batch Commands)
	// requires. Deduplicating here rather than inside ParseCommaSeparatedIDs is
	// deliberate -- `sprint reorder` shares that parser and a repeat is a real
	// error there, which silent deduplication would hide.
	ids = utils.DistinctIDs(ids)

	// See taskRemove: a stray "-"-prefixed token is refused before the roadmap.
	if err := rejectUnknownFlags(strays); err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	states, err := database.GetTaskSprintStates(ctx, ids)
	if err != nil {
		return err
	}
	if err := utils.TasksNotFoundError(utils.MissingIDs(ids, taskSprintStateIDsOf(states))); err != nil {
		return err
	}
	byID := taskSprintStatesByID(states)

	// A task in a CLOSED sprint is not reopened: the refusal names the first
	// such task in the order the command line supplied them, once every id has
	// been resolved and before anything is written.
	for _, id := range ids {
		st := byID[id]
		if reopenable(st.Status) && st.SprintID != 0 && st.SprintStatus == models.SprintClosed {
			return fmt.Errorf("%w: cannot reopen task %d: sprint #%d is CLOSED; reopen the sprint first with 'rmp sprint reopen'",
				utils.ErrValidation, id, st.SprintID)
		}
	}

	// A member returns to SPRINT; a task outside every sprint returns to
	// BACKLOG. A task already in SPRINT or in BACKLOG is reported and skipped.
	var toSprint, toBacklog, reopened []int
	for i := range states {
		st := states[i]
		if !reopenable(st.Status) {
			fmt.Fprintf(os.Stderr, "task #%d is already in %s\n", st.ID, st.Status)
			continue
		}
		reopened = append(reopened, st.ID)
		if st.SprintID != 0 {
			toSprint = append(toSprint, st.ID)
		} else {
			toBacklog = append(toBacklog, st.ID)
		}
	}

	if len(reopened) == 0 {
		return nil
	}

	now := utils.NowISO8601()

	return database.WithTransaction(func(tx *sql.Tx) error {
		// commit_close is cleared with the lifecycle timestamps and the
		// completion summary; commit_open is preserved, which is why it is
		// absent from the SET list (SPEC/STATE_MACHINE.md § Commit Tracking
		// Fields, rules 4 and 5). sprint_tasks is not touched.
		for _, group := range []struct {
			status models.TaskStatus
			ids    []int
		}{
			{models.StatusSprint, toSprint},
			{models.StatusBacklog, toBacklog},
		} {
			if len(group.ids) == 0 {
				continue
			}
			query := fmt.Sprintf( // #nosec G201 -- only ? placeholders interpolated, values are parameterized
				"UPDATE tasks SET status = ?, started_at = NULL, tested_at = NULL, closed_at = NULL, completion_summary = NULL, commit_close = NULL WHERE id IN (%s)",
				database.Placeholders(len(group.ids)),
			)
			args := append([]any{group.status}, makeInterfaceSlice(group.ids)...)
			if _, err := tx.Exec(query, args...); err != nil {
				return err
			}
		}

		// TASK_REOPEN and nothing else, one per reopened task (SPEC/COMMANDS.md
		// § Reopen Task, Audit, rule 1).
		audit := db.NewAuditWriter(tx)
		defer audit.Close() //nolint:errcheck // releasing the statement; the transaction releases it too
		for _, id := range reopened {
			if err := audit.Log(models.OpTaskReopen, models.EntityTask, id, now); err != nil {
				return err
			}
		}

		// The sprint membership guard checks the result before commit
		// (SPEC/DATABASE.md § Sprint Membership Invariant Enforcement).
		return db.CheckSprintMembershipTx(tx, reopened)
	})
}

// reopenable reports whether `task reopen` changes a task in status s: DOING,
// TESTING and COMPLETED are reopened; SPRINT and BACKLOG are already at the
// start of the lifecycle and are left as they are.
func reopenable(s models.TaskStatus) bool {
	return s == models.StatusDoing || s == models.StatusTesting || s == models.StatusCompleted
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
	remaining, strays := splitPositionals(remaining, 2)

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID(s) and priority required", utils.ErrRequired)
	}

	ids, err := utils.ParseCommaSeparatedIDs(remaining[0], utils.FieldTaskID)
	if err != nil {
		return err
	}

	// A repeated id names the same task each time, so the list is reduced to the
	// set it denotes before anything downstream reads it: the membership test,
	// the UPDATE's IN clause and the audit loop all then operate once per
	// distinct task, as SPEC/COMMANDS.md § Task ID Lists (Batch Commands)
	// requires. Deduplicating here rather than inside ParseCommaSeparatedIDs is
	// deliberate -- `sprint reorder` shares that parser and a repeat is a real
	// error there, which silent deduplication would hide.
	ids = utils.DistinctIDs(ids)

	priority, err := strconv.Atoi(remaining[1])
	if err != nil {
		// A <priority> that cannot be read as an integer — a value too large
		// for the platform's integer type included — is misuse, not a
		// validation failure: exit 2, the value echoed as supplied, and the
		// tail of the -p, --priority refusal. It is NOT the range rule below,
		// which refuses a well-formed integer outside 0-9 with exit 6
		// (SPEC/COMMANDS.md § Change Priority (prio)).
		return errNotAnInteger("priority", remaining[1], intRange(models.MinPriority, models.MaxPriority))
	}
	// The bounds and the wording of their refusal belong to the field, not to
	// this command: models.ValidatePriority is the one place either is stated,
	// and `task create` and `task edit` reach the same rule through it
	// (rmp task 318).
	if err := models.ValidatePriority(priority); err != nil {
		return err
	}

	// A "-"-prefixed token between or after the positional arguments stands in
	// no slot: refused after the checks on the ids and the priority, and before
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

	// Fail-fast: every requested ID must exist before any mutation. Without
	// this, nonexistent IDs returned exit 0, mutated valid tasks in a mixed
	// batch, and wrote phantom audit rows for IDs that do not exist
	// (SPEC/COMMANDS.md § Change Priority). Mirrors task remove/stat/reopen.
	tasks, err := database.GetTaskStates(ctx, ids)
	if err != nil {
		return err
	}
	if err := utils.TasksNotFoundError(utils.MissingIDs(ids, taskStateIDsOf(tasks))); err != nil {
		return err
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
		audit := db.NewAuditWriter(tx)
		defer audit.Close() //nolint:errcheck // releasing the statement; the transaction releases it too
		for _, id := range ids {
			if err := audit.Log(models.OpTaskPriorityChange, models.EntityTask, id, now); err != nil {
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
	remaining, strays := splitPositionals(remaining, 2)

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID(s) and severity required", utils.ErrRequired)
	}

	ids, err := utils.ParseCommaSeparatedIDs(remaining[0], utils.FieldTaskID)
	if err != nil {
		return err
	}

	// A repeated id names the same task each time, so the list is reduced to the
	// set it denotes before anything downstream reads it: the membership test,
	// the UPDATE's IN clause and the audit loop all then operate once per
	// distinct task, as SPEC/COMMANDS.md § Task ID Lists (Batch Commands)
	// requires. Deduplicating here rather than inside ParseCommaSeparatedIDs is
	// deliberate -- `sprint reorder` shares that parser and a repeat is a real
	// error there, which silent deduplication would hide.
	ids = utils.DistinctIDs(ids)

	severity, err := strconv.Atoi(remaining[1])
	if err != nil {
		// The rule taskSetPriority applies to <priority>, with the line
		// SPEC/COMMANDS.md § Change Severity (sev) publishes.
		return errNotAnInteger("severity", remaining[1], intRange(models.MinSeverity, models.MaxSeverity))
	}
	// One rule, one message: see the note in taskSetPriority above.
	if err := models.ValidateSeverity(severity); err != nil {
		return err
	}

	// See taskSetPriority: a stray "-"-prefixed token is refused before the roadmap.
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

	// Fail-fast: every requested ID must exist before any mutation. Without
	// this, nonexistent IDs returned exit 0, mutated valid tasks in a mixed
	// batch, and wrote phantom audit rows for IDs that do not exist
	// (SPEC/COMMANDS.md § Change Severity). Mirrors task remove/stat/reopen.
	tasks, err := database.GetTaskStates(ctx, ids)
	if err != nil {
		return err
	}
	if err := utils.TasksNotFoundError(utils.MissingIDs(ids, taskStateIDsOf(tasks))); err != nil {
		return err
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
		audit := db.NewAuditWriter(tx)
		defer audit.Close() //nolint:errcheck // releasing the statement; the transaction releases it too
		for _, id := range ids {
			if err := audit.Log(models.OpTaskSeverityChange, models.EntityTask, id, now); err != nil {
				return err
			}
		}
		return nil
	})
}
