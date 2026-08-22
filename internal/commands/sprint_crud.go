package commands

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// parseSprintOrderFlag parses and validates the raw --order flag value. It
// returns the parsed order on success. A non-integer value or a value <= 0 is
// rejected with exit code 6 (ErrValidation), matching SPEC/COMMANDS.md
// § Create Sprint / § Update Sprint.
func parseSprintOrderFlag(raw string) (int, error) {
	order, convErr := strconv.Atoi(strings.TrimSpace(raw))
	if convErr != nil {
		return 0, fmt.Errorf("%w: --order must be a positive integer greater than zero", utils.ErrValidation)
	}
	if order <= 0 {
		return 0, fmt.Errorf("%w: --order must be a positive integer greater than zero (got %d)", utils.ErrValidation, order)
	}
	return order, nil
}

// sprintList lists sprints.
func sprintList(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	fp := NewFlagParser(SprintListFlags)
	result, err := fp.Parse(remaining)
	if err != nil {
		return err
	}

	var status *models.SprintStatus
	if statusStr, ok := result.Flags["Status"].(string); ok {
		s, parseErr := models.ParseSprintStatus(statusStr)
		if parseErr != nil {
			// A bad --status enum value is a value-validation failure (exit 6),
			// not a generic runtime error (exit 1). ParseSprintStatus returns a
			// model-level sentinel the exit-code mapper does not recognise, so
			// wrap it in utils.ErrValidation to land on exit 6, matching every
			// other enum filter and SPEC/COMMANDS.md.
			return fmt.Errorf("%w: %s", utils.ErrValidation, parseErr.Error())
		}
		status = &s
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	sprints, err := database.ListSprints(ctx, status)
	if err != nil {
		return err
	}

	return utils.PrintJSON(sprints)
}

// sprintCreate creates a new sprint.
func sprintCreate(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	fp := NewFlagParser(SprintCreateFlags)
	result, err := fp.Parse(remaining)
	if err != nil {
		return err
	}

	title, _ := result.Flags["Title"].(string)
	if title == "" {
		// "<sentinel>: --flag" so stderr matches the SPEC canonical shape
		// ("Error: required parameter missing: --title") without the
		// redundant doubled prefix (finding #54).
		return fmt.Errorf("%w: --title", utils.ErrRequired)
	}
	// The cap keeps the position it has always had on this field — ahead of the
	// content rules, which is what rmp task 302 measures and this task does not
	// move — but it now measures strings.TrimSpace of the value, because that is
	// the value stored (SPEC/MODELS.md § Free-Text Emptiness and Trimming
	// Constraint, Rule 2). Measuring the value as supplied is what made a title
	// of exactly 255 real characters carrying surrounding whitespace refused
	// here and accepted by `task create`, for a value the column would have held.
	if len(strings.TrimSpace(title)) > models.MaxSprintTitle {
		return utils.FieldTooLargeError(utils.FieldSprintTitle, models.MaxSprintTitle)
	}
	// The encoding rule and the control-character rule on the value AS SUPPLIED,
	// then the trim, then the emptiness judgement on the trimmed value — the one
	// order SPEC/MODELS.md § Free-Text Emptiness and Trimming Constraint fixes,
	// owned by utils.RequireFreeText so this command cannot get it wrong on its
	// own. `title` is rebound to the trimmed value, which is what is stored.
	//
	// This path previously did not trim at all. That is precisely why a title of
	// three spaces created a sprint no reader surface could name, and equally why
	// the control-character rule was intact here: adding the trim WITHOUT the
	// order would have refused the three spaces and, in the same change, let a
	// leading VT or FF through with the character silently discarded.
	title, err = utils.RequireFreeText(title, utils.FieldSprintTitle)
	if err != nil {
		return err
	}

	description, _ := result.Flags["Description"].(string)
	if description == "" {
		// "<sentinel>: --flag" so stderr matches the SPEC canonical shape
		// ("Error: required parameter missing: --description") without the
		// redundant doubled prefix (finding #54).
		return fmt.Errorf("%w: --description", utils.ErrRequired)
	}
	// Same sequence as the title above. No inline cap runs on this field: for
	// `sprint create` the description's length is checked by sprint.Validate()
	// inside the transaction, and that position is deliberately left where it is
	// (rmp task 302 settles it). Validate() now receives the trimmed value, so
	// what the cap measures is what the column holds either way.
	description, err = utils.RequireFreeText(description, utils.FieldSprintDescription)
	if err != nil {
		return err
	}

	var maxTasks *int
	if mt, ok := result.Flags["MaxTasks"].(int); ok {
		if mt < 1 || mt > models.MaxSprintMaxTasks {
			return fmt.Errorf("%w: --max-tasks must be between 1 and %d (got %d)", utils.ErrValidation, models.MaxSprintMaxTasks, mt)
		}
		maxTasks = &mt
	}

	// --order is optional on create: when supplied it must be a positive integer
	// and unique; when omitted the next value MAX(order_index)+1 is auto-assigned.
	explicitOrder := 0
	if rawOrder, ok := result.Flags["Order"].(string); ok {
		explicitOrder, err = parseSprintOrderFlag(rawOrder)
		if err != nil {
			return err
		}
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Title:       title,
		Description: description,
		CreatedAt:   now,
		MaxTasks:    maxTasks,
		Order:       explicitOrder, // 0 means auto-assign; Validate runs after assignment
	}

	// Create within transaction with audit. The next_order SELECT, the INSERT,
	// and the audit row run in one transaction so concurrent creations cannot
	// share an order; the idx_sprints_order unique index is the final backstop
	// (SPEC/DATABASE.md § Transactional Atomicity Guarantees #6).
	var sprintID int
	err = database.WithTransaction(func(tx *sql.Tx) error {
		orderIndex := explicitOrder
		if orderIndex <= 0 {
			if selErr := tx.QueryRow(
				`SELECT COALESCE(MAX(order_index), 0) + 1 FROM sprints`,
			).Scan(&orderIndex); selErr != nil {
				return fmt.Errorf("computing next sprint order: %w", selErr)
			}
		}
		sprint.Order = orderIndex

		// Validate the fully-populated sprint (order is now assigned) before insert.
		if vErr := sprint.Validate(); vErr != nil {
			return vErr
		}

		insertResult, insertErr := tx.Exec(
			`INSERT INTO sprints (status, title, description, created_at, max_tasks, order_index) VALUES (?, ?, ?, ?, ?, ?)`,
			sprint.Status, sprint.Title, sprint.Description, sprint.CreatedAt, sprint.MaxTasks, orderIndex,
		)
		if insertErr != nil {
			if db.IsUniqueConstraintErr(insertErr) {
				return fmt.Errorf("%w: sprint order %d is already in use", utils.ErrAlreadyExists, orderIndex)
			}
			return insertErr
		}

		id, idErr := insertResult.LastInsertId()
		if idErr != nil {
			return idErr
		}
		sprintID = int(id)

		return db.LogAuditTx(tx, models.OpSprintCreate, models.EntitySprint, sprintID, now)
	})

	if err != nil {
		return err
	}

	return utils.PrintJSON(map[string]int{"id": sprintID})
}

// sprintGet gets a sprint.
func sprintGet(args []string) error {
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

	return utils.PrintJSON(sprint)
}

// sprintShow displays a comprehensive status report of a sprint.
//
// Parameters:
//   - args: Command-line arguments including sprint ID
//
// Required arguments:
//   - sprint ID: The ID of the sprint to show (first positional argument)
//
// Error conditions:
//   - Returns utils.ErrRequired if sprint ID is missing
//   - Returns utils.ErrNotFound if sprint doesn't exist
//
// Output:
//   - JSON object with sprint summary, progress, severity distribution, and criticality distribution
//
// Example:
//
//	rmp sprint show -r myproject 1
func sprintShow(args []string) error {
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

	tasks, err := database.GetSprintTasksFull(ctx, sprintID, nil, false)
	if err != nil {
		return err
	}

	result := models.CalculateSprintShowResult(sprint, tasks)
	return utils.PrintJSON(result)
}

// sprintUpdate updates a sprint.
func sprintUpdate(args []string) error {
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

	fp := NewFlagParser(SprintCreateFlags)
	result, err := fp.Parse(remaining[1:])
	if err != nil {
		return err
	}

	// Every decision below — the at-least-one-flag requirement, the field
	// validation, the SET clause and the audit operations — keys on whether the
	// flag was SUPPLIED, never on whether its value is non-empty. The two are not
	// the same thing: `-t ""` supplies a flag carrying an empty value, and reading
	// the empty string as "absent" made the command report a missing parameter for
	// an invocation that supplied one, and silently drop the field when another
	// flag kept the update alive. `task edit` already keys on presence
	// (SPEC/COMMANDS.md § Update Sprint, § Edit Task).
	title, hasTitle := result.Flags["Title"].(string)
	description, hasDescription := result.Flags["Description"].(string)
	_, hasMaxTasks := result.Flags["MaxTasks"]
	rawOrder, hasOrder := result.Flags["Order"].(string)

	if !hasTitle && !hasDescription && !hasMaxTasks && !hasOrder {
		return fmt.Errorf("%w: at least one of --title, --description, --max-tasks or --order is required", utils.ErrRequired)
	}

	// --order, when supplied, must be a positive integer greater than zero.
	newOrder := 0
	if hasOrder {
		newOrder, err = parseSprintOrderFlag(rawOrder)
		if err != nil {
			return err
		}
	}

	// A supplied --title must carry a value: unlike `sprint create`, where
	// --title is a required parameter and the literal empty string means the
	// parameter is missing (exit code 2), here it is an optional flag whose
	// value is rejected (exit code 6). Same wrapper and same phrasing as
	// `task edit`, so both commands answer `-t ""` identically — and, since this
	// task, answer `-t "   "` identically too.
	if hasTitle {
		// The cap keeps its position ahead of the content rules and now measures
		// the trimmed value, the value stored (SPEC/MODELS.md § Free-Text
		// Emptiness and Trimming Constraint, Rule 2).
		if len(strings.TrimSpace(title)) > models.MaxSprintTitle {
			return utils.FieldTooLargeError(utils.FieldSprintTitle, models.MaxSprintTitle)
		}
		// Content rules on the value AS SUPPLIED, then the trim, then the
		// emptiness judgement on the trimmed value. `-t ""` still reaches
		// FieldEmptyError — the empty string survives both content rules and
		// trims to itself — so this command answers the literal empty string
		// exactly as it did, and now answers a whitespace-only value the same
		// way instead of storing it.
		title, err = utils.RequireFreeText(title, utils.FieldSprintTitle)
		if err != nil {
			return err
		}
	}

	if hasDescription {
		// Same sequence, same reasons, as the title above.
		if len(strings.TrimSpace(description)) > models.MaxSprintDescription {
			return utils.FieldTooLargeError(utils.FieldSprintDescription, models.MaxSprintDescription)
		}
		description, err = utils.RequireFreeText(description, utils.FieldSprintDescription)
		if err != nil {
			return err
		}
	}

	var maxTasks *int
	if hasMaxTasks {
		mt := result.Flags["MaxTasks"].(int)
		if mt < 1 || mt > models.MaxSprintMaxTasks {
			return fmt.Errorf("%w: --max-tasks must be between 1 and %d (got %d)", utils.ErrValidation, models.MaxSprintMaxTasks, mt)
		}
		maxTasks = &mt
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	// Build dynamic SET clause based on provided flags. Columns are collected
	// in a stable order (title, description, max_tasks, order_index) so the
	// generated SQL is deterministic. Column names are hardcoded literals — only
	// values are bound as parameters — so the assembled query is injection-safe.
	return database.WithTransaction(func(tx *sql.Tx) error {
		// When --order is requested, the sprint's status must not be CLOSED: a
		// CLOSED sprint's order is immutable (SPEC/STATE_MACHINE.md § Sprint Order
		// Immutability). Read the status inside the transaction so the precondition
		// and the UPDATE are atomic. Doubling as the existence check.
		if hasOrder {
			var status string
			statusErr := tx.QueryRow("SELECT status FROM sprints WHERE id = ?", sprintID).Scan(&status)
			if errors.Is(statusErr, sql.ErrNoRows) {
				return fmt.Errorf("%w: sprint %d not found", utils.ErrNotFound, sprintID)
			}
			if statusErr != nil {
				return statusErr
			}
			if status == string(models.SprintClosed) {
				return fmt.Errorf("%w: sprint #%d order cannot be changed — sprint is CLOSED", utils.ErrValidation, sprintID)
			}
		}

		setParts := make([]string, 0, 4)
		args := make([]any, 0, 5)
		// One operation per supplied field, collected alongside the column it
		// belongs to so the entries are written in the same order the UPDATE
		// applies them (SPEC/COMMANDS.md § Update Sprint).
		ops := make([]models.AuditOperation, 0, 4)

		if hasTitle {
			setParts = append(setParts, "title = ?")
			args = append(args, title)
			ops = append(ops, models.OpSprintTitleChange)
		}
		if hasDescription {
			setParts = append(setParts, "description = ?")
			args = append(args, description)
			ops = append(ops, models.OpSprintDescriptionChange)
		}
		if hasMaxTasks {
			setParts = append(setParts, "max_tasks = ?")
			args = append(args, maxTasks)
			ops = append(ops, models.OpSprintMaxTasksChange)
		}
		if hasOrder {
			setParts = append(setParts, "order_index = ?")
			args = append(args, newOrder)
			ops = append(ops, models.OpSprintOrderChange)
		}
		args = append(args, sprintID)
		query := fmt.Sprintf("UPDATE sprints SET %s WHERE id = ?", strings.Join(setParts, ", ")) // #nosec G201 -- setParts are hard-coded literal column clauses ("title = ?", "description = ?", "max_tasks = ?", "order_index = ?"); every user value is bound via tx.Exec parameters, no user data is concatenated into SQL

		updateResult, updateErr := tx.Exec(query, args...)
		if updateErr != nil {
			// An order_index collision fails idx_sprints_order; surface it as
			// ErrAlreadyExists (exit code 5).
			if hasOrder && db.IsUniqueConstraintErr(updateErr) {
				return fmt.Errorf("%w: sprint order %d is already in use", utils.ErrAlreadyExists, newOrder)
			}
			return updateErr
		}

		affected, affErr := updateResult.RowsAffected()
		if affErr != nil {
			return affErr
		}
		if affected == 0 {
			return fmt.Errorf("%w: sprint %d not found", utils.ErrNotFound, sprintID)
		}

		// One entry per supplied field, all carrying the timestamp captured
		// above, inside the transaction that performed the UPDATE: a rejected
		// update — an out-of-range bound, a CLOSED sprint, an order collision —
		// writes no entry at all. The trigger is the presence of the flag and
		// not a difference in value: nothing above compares a supplied value
		// against the stored one, so `sprint update --order 3` on a sprint
		// already at 3 still records the command that was issued
		// (SPEC/COMMANDS.md § Update Sprint).
		return db.LogAuditFieldsTx(tx, models.EntitySprint, sprintID, now, ops...)
	})
}

// sprintRemove removes a sprint.
func sprintRemove(args []string) error {
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

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	// Delete within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		// First reset task statuses to BACKLOG, clearing ALL lifecycle
		// timestamps, the completion summary and commit_close. Tasks may have
		// progressed to DOING/TESTING/COMPLETED inside the sprint, so leaving
		// those fields set on a BACKLOG task violates the state machine's
		// reopening invariant (SPEC/STATE_MACHINE.md Reopening Behavior;
		// finding #49). commit_open is deliberately NOT cleared: a task whose
		// sprint is deleted keeps the record of where its work started
		// (SPEC/STATE_MACHINE.md § Commit Tracking Fields).
		_, resetErr := tx.Exec(
			`UPDATE tasks SET status = 'BACKLOG', started_at = NULL, tested_at = NULL,
			        closed_at = NULL, completion_summary = NULL, commit_close = NULL WHERE id IN (
				SELECT task_id FROM sprint_tasks WHERE sprint_id = ?
			)`,
			sprintID,
		)
		if resetErr != nil {
			return resetErr
		}

		// Remove sprint_tasks entries
		_, deleteTasksErr := tx.Exec("DELETE FROM sprint_tasks WHERE sprint_id = ?", sprintID)
		if deleteTasksErr != nil {
			return deleteTasksErr
		}

		// Delete sprint
		deleteResult, deleteErr := tx.Exec("DELETE FROM sprints WHERE id = ?", sprintID)
		if deleteErr != nil {
			return deleteErr
		}

		affected, affErr := deleteResult.RowsAffected()
		if affErr != nil {
			return affErr
		}
		if affected == 0 {
			return fmt.Errorf("%w: sprint %d not found", utils.ErrNotFound, sprintID)
		}

		return db.LogAuditTx(tx, models.OpSprintDelete, models.EntitySprint, sprintID, now)
	})
}
