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

// taskAssign appends a specialist to the task's specialists field (idempotent).
//
// Usage: rmp task assign <task-id> <specialist>
//
// If the specialist is already present, the operation succeeds without modification.
// The specialists field is a nullable comma-separated string.
func taskAssign(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID and specialist name required", utils.ErrRequired)
	}

	taskID, err := utils.ValidateIDString(remaining[0], "task")
	if err != nil {
		return err
	}

	specialist := strings.TrimSpace(remaining[1])
	if specialist == "" {
		return fmt.Errorf("%w: specialist name cannot be empty", utils.ErrInvalidInput)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	tasks, err := database.GetTasks(ctx, []int{taskID})
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return fmt.Errorf("%w: task %d not found", utils.ErrNotFound, taskID)
	}
	task := tasks[0]

	// Build updated specialists list (no duplicates).
	var current []string
	if task.Specialists != nil && *task.Specialists != "" {
		current = models.ParseSpecialists(*task.Specialists)
	}

	for _, s := range current {
		if s == specialist {
			// Already present — idempotent success, no DB write needed.
			return nil
		}
	}

	current = append(current, specialist)
	newValue := models.FormatSpecialists(current)

	// Validate length constraint.
	if len(newValue) > models.MaxTaskSpecialists {
		return fmt.Errorf("%w: specialists field would exceed maximum length of %d characters", utils.ErrFieldTooLarge, models.MaxTaskSpecialists)
	}

	now := utils.NowISO8601()

	return database.WithTransaction(func(tx *sql.Tx) error {
		_, execErr := tx.Exec("UPDATE tasks SET specialists = ? WHERE id = ?", newValue, taskID)
		if execErr != nil {
			return execErr
		}

		_, auditErr := tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpTaskAssign, models.EntityTask, taskID, now,
		)
		return auditErr
	})
}

// taskUnassign removes a specialist from the task's specialists field.
//
// Usage: rmp task unassign <task-id> <specialist>
//
// If the specialist is not present, an informational message is printed and the
// command exits successfully (exit 0). If the specialists list becomes empty after
// removal, the field is set to NULL.
func taskUnassign(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: task ID and specialist name required", utils.ErrRequired)
	}

	taskID, err := utils.ValidateIDString(remaining[0], "task")
	if err != nil {
		return err
	}

	specialist := strings.TrimSpace(remaining[1])
	if specialist == "" {
		return fmt.Errorf("%w: specialist name cannot be empty", utils.ErrInvalidInput)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	tasks, err := database.GetTasks(ctx, []int{taskID})
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return fmt.Errorf("%w: task %d not found", utils.ErrNotFound, taskID)
	}
	task := tasks[0]

	// Build updated specialists list.
	var current []string
	if task.Specialists != nil && *task.Specialists != "" {
		current = models.ParseSpecialists(*task.Specialists)
	}

	found := false
	updated := current[:0]
	for _, s := range current {
		if s == specialist {
			found = true
			continue
		}
		updated = append(updated, s)
	}

	if !found {
		fmt.Fprintf(os.Stderr, "specialist %q is not assigned to task #%d\n", specialist, taskID)
		return nil
	}

	now := utils.NowISO8601()

	return database.WithTransaction(func(tx *sql.Tx) error {
		var execErr error
		if len(updated) == 0 {
			// Set to NULL when the list becomes empty.
			_, execErr = tx.Exec("UPDATE tasks SET specialists = NULL WHERE id = ?", taskID)
		} else {
			newValue := models.FormatSpecialists(updated)
			_, execErr = tx.Exec("UPDATE tasks SET specialists = ? WHERE id = ?", newValue, taskID)
		}
		if execErr != nil {
			return execErr
		}

		_, auditErr := tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpTaskUnassign, models.EntityTask, taskID, now,
		)
		return auditErr
	})
}
