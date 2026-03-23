package commands

import (
	"database/sql"
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

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
			return parseErr
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

	description, _ := result.Flags["Description"].(string)
	if description == "" {
		return fmt.Errorf("%w: missing required parameter: --description", utils.ErrRequired)
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
		Description: description,
		CreatedAt:   now,
	}

	if err := sprint.Validate(); err != nil {
		return err
	}

	// Create within transaction with audit
	var sprintID int
	err = database.WithTransaction(func(tx *sql.Tx) error {
		insertResult, insertErr := tx.Exec(
			`INSERT INTO sprints (status, description, created_at) VALUES (?, ?, ?)`,
			sprint.Status, sprint.Description, sprint.CreatedAt,
		)
		if insertErr != nil {
			return insertErr
		}

		id, idErr := insertResult.LastInsertId()
		if idErr != nil {
			return idErr
		}
		sprintID = int(id)

		_, auditErr := tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpSprintCreate, models.EntitySprint, sprintID, now,
		)
		return auditErr
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

	description, _ := result.Flags["Description"].(string)
	if description == "" {
		return fmt.Errorf("%w: missing required parameter: --description", utils.ErrRequired)
	}

	// Validate description length
	if len(description) > models.MaxSprintDescription {
		return fmt.Errorf("%w: description exceeds maximum length of %d characters", utils.ErrFieldTooLarge, models.MaxSprintDescription)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	// Update within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		updateResult, updateErr := tx.Exec(
			"UPDATE sprints SET description = ? WHERE id = ?",
			description, sprintID,
		)
		if updateErr != nil {
			return updateErr
		}

		affected, affErr := updateResult.RowsAffected()
		if affErr != nil {
			return affErr
		}
		if affected == 0 {
			return fmt.Errorf("%w: sprint %d not found", utils.ErrNotFound, sprintID)
		}

		_, auditErr := tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpSprintUpdate, models.EntitySprint, sprintID, now,
		)
		return auditErr
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
		// First reset task statuses
		_, resetErr := tx.Exec(
			`UPDATE tasks SET status = 'BACKLOG' WHERE id IN (
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

		_, auditErr := tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpSprintDelete, models.EntitySprint, sprintID, now,
		)
		return auditErr
	})
}
