package commands

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// taskEdit modifies an existing task's fields.
//
// Parameters:
//   - args: Command-line arguments including task ID and optional flags
//
// Required arguments:
//   - task ID: The ID of the task to edit (first positional argument)
//
// Optional flags (at least one required):
//   - -t, --title: New task title (max 255 chars)
//   - -fr, --functional-requirements: New functional requirements (max 4096 chars)
//   - -tr, --technical-requirements: New technical requirements (max 4096 chars)
//   - -ac, --acceptance-criteria: New acceptance criteria (max 4096 chars)
//   - -sp, --specialists: New specialists list (max 500 chars)
//   - -p, --priority: New priority 0-9
//   - --severity: New severity 0-9
//   - -r, --roadmap: Roadmap name (uses current if not specified)
//
// Error conditions:
//   - Returns utils.ErrRequired if task ID is missing
//   - Returns utils.ErrNotFound if task doesn't exist
//   - Returns utils.ErrInvalidInput if priority/severity out of range
//   - Returns utils.ErrFieldTooLarge if text fields exceed limits
//
// Side effects:
//   - Updates task record in database
//   - Logs TASK_UPDATE audit entry
//   - Outputs updated task as JSON to stdout
//
// Complexity: O(1) - single database update
//
// Example:
//
//	rmp task edit -r myproject 42 -t "Updated title" -p 8
func taskEdit(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) == 0 {
		return fmt.Errorf("%w: task ID required", utils.ErrRequired)
	}

	taskID, err := utils.ValidateIDString(remaining[0], "task")
	if err != nil {
		return err
	}

	// Parse optional fields
	updates := make(map[string]interface{})

	for i := 1; i < len(remaining); i++ {
		switch remaining[i] {
		case "-t", "--title":
			if i+1 < len(remaining) {
				updates["title"] = remaining[i+1]
				i++
			}
		case "-fr", "--functional-requirements":
			if i+1 < len(remaining) {
				updates["functional_requirements"] = remaining[i+1]
				i++
			}
		case "-tr", "--technical-requirements":
			if i+1 < len(remaining) {
				updates["technical_requirements"] = remaining[i+1]
				i++
			}
		case "-ac", "--acceptance-criteria":
			if i+1 < len(remaining) {
				updates["acceptance_criteria"] = remaining[i+1]
				i++
			}
		case "-p", "--priority":
			if i+1 < len(remaining) {
				p, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid priority: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				updates["priority"] = p
				i++
			}
		case "--severity":
			if i+1 < len(remaining) {
				s, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid severity: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				updates["severity"] = s
				i++
			}
		case "-y", "--type":
			if i+1 < len(remaining) {
				parsed, err := models.ParseTaskType(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: %s", utils.ErrInvalidInput, err.Error())
				}
				updates["type"] = string(parsed)
				i++
			}
		case "-sp", "--specialists":
			if i+1 < len(remaining) {
				updates["specialists"] = remaining[i+1]
				i++
			}
		}
	}

	if len(updates) == 0 {
		return fmt.Errorf("%w: no fields to update", utils.ErrInvalidInput)
	}

	// Validate required fields are not empty
	requiredFields := map[string]string{
		"title":                   "title",
		"functional_requirements": "functional-requirements",
		"technical_requirements":  "technical-requirements",
		"acceptance_criteria":     "acceptance-criteria",
	}
	for field, flagName := range requiredFields {
		if value, ok := updates[field]; ok {
			if str, ok := value.(string); ok && str == "" {
				return fmt.Errorf("%w: %s cannot be empty", utils.ErrValidation, flagName)
			}
		}
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Update within transaction with audit
	return database.WithTransaction(func(tx *sql.Tx) error {
		// Update task
		setParts := []string{}
		args := []interface{}{}
		for field, value := range updates {
			setParts = append(setParts, fmt.Sprintf("%s = ?", field))
			args = append(args, value)
		}
		args = append(args, taskID)

		query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", strings.Join(setParts, ", "))
		result, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("%w: task %d not found", utils.ErrNotFound, taskID)
		}

		// Log audit
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpTaskUpdate, models.EntityTask, taskID, utils.NowISO8601(),
		)
		return err
	})
}
