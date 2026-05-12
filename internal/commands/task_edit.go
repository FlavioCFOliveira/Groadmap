package commands

import (
	"database/sql"
	"fmt"
	"sort"
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

	fp := NewFlagParser(TaskEditFlags)
	result, err := fp.Parse(remaining[1:])
	if err != nil {
		return err
	}

	updates := make(map[string]interface{})

	// Trim leading/trailing whitespace before validation per SPEC/COMMANDS.md.
	if v, ok := result.Flags["Title"]; ok {
		updates["title"] = strings.TrimSpace(v.(string))
	}
	if v, ok := result.Flags["FunctionalRequirements"]; ok {
		updates["functional_requirements"] = strings.TrimSpace(v.(string))
	}
	if v, ok := result.Flags["TechnicalRequirements"]; ok {
		updates["technical_requirements"] = strings.TrimSpace(v.(string))
	}
	if v, ok := result.Flags["AcceptanceCriteria"]; ok {
		updates["acceptance_criteria"] = strings.TrimSpace(v.(string))
	}
	if v, ok := result.Flags["Specialists"]; ok {
		updates["specialists"] = strings.TrimSpace(v.(string))
	}
	if v, ok := result.Flags["Priority"]; ok {
		updates["priority"] = v.(int)
	}
	if v, ok := result.Flags["Severity"]; ok {
		updates["severity"] = v.(int)
	}
	if typeStr, ok := result.Flags["Type"].(string); ok {
		parsed, parseErr := models.ParseTaskType(typeStr)
		if parseErr != nil {
			return fmt.Errorf("%w: %s", utils.ErrInvalidInput, parseErr.Error())
		}
		updates["type"] = string(parsed)
	}

	if len(updates) == 0 {
		return fmt.Errorf("%w: no fields to update", utils.ErrInvalidInput)
	}

	// Validate that required text fields are not set to empty
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
		// Sort field names so the generated UPDATE statement is stable
		// across runs (deterministic SQL helps the query planner cache).
		fields := make([]string, 0, len(updates))
		for f := range updates {
			fields = append(fields, f)
		}
		sort.Strings(fields)

		setParts := make([]string, 0, len(fields))
		queryArgs := make([]interface{}, 0, len(fields)+1)
		for _, field := range fields {
			setParts = append(setParts, field+" = ?")
			queryArgs = append(queryArgs, updates[field])
		}
		queryArgs = append(queryArgs, taskID)

		query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = ?", strings.Join(setParts, ", ")) // #nosec G201 -- field names are internal constants, values use parameterized ?
		updateResult, updateErr := tx.Exec(query, queryArgs...)
		if updateErr != nil {
			return updateErr
		}

		affected, affErr := updateResult.RowsAffected()
		if affErr != nil {
			return affErr
		}
		if affected == 0 {
			return fmt.Errorf("%w: task %d not found", utils.ErrNotFound, taskID)
		}

		_, auditErr := tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpTaskUpdate, models.EntityTask, taskID, utils.NowISO8601(),
		)
		return auditErr
	})
}
