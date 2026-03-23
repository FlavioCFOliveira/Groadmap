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

	// Parse optional status filter
	var status *models.SprintStatus
	for i := 0; i < len(remaining); i++ {
		if remaining[i] == "--status" && i+1 < len(remaining) {
			s, err := models.ParseSprintStatus(remaining[i+1])
			if err != nil {
				return err
			}
			status = &s
			i++
		}
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

	// Parse description
	var description string
	for i := 0; i < len(remaining); i++ {
		if remaining[i] == "-d" || remaining[i] == "--description" {
			if i+1 < len(remaining) {
				description = remaining[i+1]
				i++
			}
		}
	}

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
		result, err := tx.Exec(
			`INSERT INTO sprints (status, description, created_at) VALUES (?, ?, ?)`,
			sprint.Status, sprint.Description, sprint.CreatedAt,
		)
		if err != nil {
			return err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		sprintID = int(id)

		// Log audit with same timestamp
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpSprintCreate, models.EntitySprint, sprintID, now,
		)
		return err
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

	// Get sprint
	sprint, err := database.GetSprint(ctx, sprintID)
	if err != nil {
		return err
	}

	// Get all tasks in sprint
	tasks, err := database.GetSprintTasksFull(ctx, sprintID, nil, false)
	if err != nil {
		return err
	}

	// Calculate comprehensive report
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

	// Parse description
	var description string
	for i := 1; i < len(remaining); i++ {
		if remaining[i] == "-d" || remaining[i] == "--description" {
			if i+1 < len(remaining) {
				description = remaining[i+1]
				i++
			}
		}
	}

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
		result, err := tx.Exec(
			"UPDATE sprints SET description = ? WHERE id = ?",
			description, sprintID,
		)
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

		// Log audit with same timestamp
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpSprintUpdate, models.EntitySprint, sprintID, now,
		)
		return err
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
		_, err := tx.Exec(
			`UPDATE tasks SET status = 'BACKLOG' WHERE id IN (
				SELECT task_id FROM sprint_tasks WHERE sprint_id = ?
			)`,
			sprintID,
		)
		if err != nil {
			return err
		}

		// Remove sprint_tasks entries
		_, err = tx.Exec("DELETE FROM sprint_tasks WHERE sprint_id = ?", sprintID)
		if err != nil {
			return err
		}

		// Delete sprint
		result, err := tx.Exec("DELETE FROM sprints WHERE id = ?", sprintID)
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

		// Log audit with same timestamp
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpSprintDelete, models.EntitySprint, sprintID, now,
		)
		return err
	})
}
