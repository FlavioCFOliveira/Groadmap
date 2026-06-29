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
	if len(title) > models.MaxSprintTitle {
		return fmt.Errorf("%w: title exceeds maximum length of %d characters", utils.ErrFieldTooLarge, models.MaxSprintTitle)
	}
	// Reject control / bidi / format code points (SPEC/MODELS.md § Free-Text
	// Control-Character Constraint).
	if err := utils.ValidateNoControlChars(title, "title"); err != nil {
		return err
	}

	description, _ := result.Flags["Description"].(string)
	if description == "" {
		// "<sentinel>: --flag" so stderr matches the SPEC canonical shape
		// ("Error: required parameter missing: --description") without the
		// redundant doubled prefix (finding #54).
		return fmt.Errorf("%w: --description", utils.ErrRequired)
	}
	// Reject control / bidi / format code points (SPEC/MODELS.md § Free-Text
	// Control-Character Constraint).
	if err := utils.ValidateNoControlChars(description, "description"); err != nil {
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

	title, _ := result.Flags["Title"].(string)
	description, _ := result.Flags["Description"].(string)
	_, hasMaxTasks := result.Flags["MaxTasks"]
	rawOrder, hasOrder := result.Flags["Order"].(string)

	if title == "" && description == "" && !hasMaxTasks && !hasOrder {
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

	if title != "" && len(title) > models.MaxSprintTitle {
		return fmt.Errorf("%w: title exceeds maximum length of %d characters", utils.ErrFieldTooLarge, models.MaxSprintTitle)
	}
	// Reject control / bidi / format code points (SPEC/MODELS.md § Free-Text
	// Control-Character Constraint).
	if title != "" {
		if err := utils.ValidateNoControlChars(title, "title"); err != nil {
			return err
		}
	}

	if description != "" && len(description) > models.MaxSprintDescription {
		return fmt.Errorf("%w: description exceeds maximum length of %d characters", utils.ErrFieldTooLarge, models.MaxSprintDescription)
	}
	// Reject control / bidi / format code points (SPEC/MODELS.md § Free-Text
	// Control-Character Constraint).
	if description != "" {
		if err := utils.ValidateNoControlChars(description, "description"); err != nil {
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

		if title != "" {
			setParts = append(setParts, "title = ?")
			args = append(args, title)
		}
		if description != "" {
			setParts = append(setParts, "description = ?")
			args = append(args, description)
		}
		if hasMaxTasks {
			setParts = append(setParts, "max_tasks = ?")
			args = append(args, maxTasks)
		}
		if hasOrder {
			setParts = append(setParts, "order_index = ?")
			args = append(args, newOrder)
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

		return db.LogAuditTx(tx, models.OpSprintUpdate, models.EntitySprint, sprintID, now)
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
		// timestamps and the completion summary. Tasks may have progressed to
		// DOING/TESTING/COMPLETED inside the sprint, so leaving those fields set
		// on a BACKLOG task violates the state machine's reopening invariant
		// (SPEC/STATE_MACHINE.md Reopening Behavior; finding #49).
		_, resetErr := tx.Exec(
			`UPDATE tasks SET status = 'BACKLOG', started_at = NULL, tested_at = NULL,
			        closed_at = NULL, completion_summary = NULL WHERE id IN (
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
