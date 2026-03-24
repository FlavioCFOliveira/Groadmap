package commands

import (
	"database/sql"
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// taskCreate creates a new task in the specified or current roadmap.
//
// Parameters:
//   - args: Command-line arguments including flags and roadmap name
//
// Required flags:
//   - -t, --title: Task title/summary (required, max 255 chars)
//   - -fr, --functional-requirements: Functional requirements - "Why?" (required, max 4096 chars)
//   - -tr, --technical-requirements: Technical requirements - "How?" (required, max 4096 chars)
//   - -ac, --acceptance-criteria: Acceptance criteria - "How to verify?" (required, max 4096 chars)
//
// Optional flags:
//   - -p, --priority: Task priority 0-9 (default: 0)
//   - --severity: Task severity 0-9 (default: 0)
//   - -sp, --specialists: Comma-separated list of specialists (max 500 chars)
//   - -r, --roadmap: Roadmap name (uses current if not specified)
//
// Error conditions:
//   - Returns utils.ErrRequired if required fields are missing
//   - Returns utils.ErrInvalidInput if priority/severity are out of range
//   - Returns utils.ErrFieldTooLarge if text fields exceed limits
//   - Returns utils.ErrNoRoadmap if no roadmap specified and none selected
//
// Side effects:
//   - Creates task record in database
//   - Logs TASK_CREATE audit entry
//   - Outputs created task as JSON to stdout
//
// Complexity: O(1) - single database insert
//
// Example:
//
//	rmp task create -r myproject -t "Fix bug" -fr "User can login" -tr "Update auth" -ac "Login works" -p 5 --severity 3
func taskCreate(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	fp := NewFlagParser(TaskCreateFlags)
	result, err := fp.Parse(remaining)
	if err != nil {
		return err
	}

	title, _ := result.Flags["Title"].(string)
	functionalReqs, _ := result.Flags["FunctionalRequirements"].(string)
	technicalReqs, _ := result.Flags["TechnicalRequirements"].(string)
	acceptanceCriteria, _ := result.Flags["AcceptanceCriteria"].(string)
	specialists, _ := result.Flags["Specialists"].(string)
	priority, _ := result.Flags["Priority"].(int)
	severity, _ := result.Flags["Severity"].(int)
	parentIDRaw, hasParent := result.Flags["ParentID"].(int)

	// Parse task type (enum conversion after FlagParser)
	taskType := models.TypeTask
	if typeStr, ok := result.Flags["Type"].(string); ok && typeStr != "" {
		parsed, parseErr := models.ParseTaskType(typeStr)
		if parseErr != nil {
			return fmt.Errorf("%w: %s", utils.ErrInvalidInput, parseErr.Error())
		}
		taskType = parsed
	}

	// Validate required fields (preserve exact error messages for compatibility)
	if title == "" {
		return fmt.Errorf("%w: missing required parameter: --title", utils.ErrRequired)
	}
	if functionalReqs == "" {
		return fmt.Errorf("%w: missing required parameter: --functional-requirements", utils.ErrRequired)
	}
	if technicalReqs == "" {
		return fmt.Errorf("%w: missing required parameter: --technical-requirements", utils.ErrRequired)
	}
	if acceptanceCriteria == "" {
		return fmt.Errorf("%w: missing required parameter: --acceptance-criteria", utils.ErrRequired)
	}

	// Validate --parent value is a positive integer
	if hasParent && parentIDRaw < 1 {
		return fmt.Errorf("%w: --parent must be a positive integer", utils.ErrInvalidInput)
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Validate parent task exists (if --parent was supplied)
	var parentTaskID *int
	if hasParent {
		ctx, cancel := db.WithQuickTimeout()
		defer cancel()

		_, parentErr := database.GetTask(ctx, parentIDRaw)
		if parentErr != nil {
			return fmt.Errorf("%w: parent task %d not found", utils.ErrNotFound, parentIDRaw)
		}
		parentTaskID = &parentIDRaw
	}

	// Capture timestamp once for the entire operation
	now := utils.NowISO8601()

	task := &models.Task{
		Title:                  title,
		Status:                 models.StatusBacklog,
		Type:                   taskType,
		FunctionalRequirements: functionalReqs,
		TechnicalRequirements:  technicalReqs,
		AcceptanceCriteria:     acceptanceCriteria,
		CreatedAt:              now,
		Priority:               priority,
		Severity:               severity,
		ParentTaskID:           parentTaskID,
	}

	if specialists != "" {
		task.Specialists = &specialists
	}

	if err := task.Validate(); err != nil {
		return err
	}

	// Create task within transaction
	var taskID int
	err = database.WithTransaction(func(tx *sql.Tx) error {
		// Insert task
		insertResult, insertErr := tx.Exec(
			`INSERT INTO tasks (title, status, type, functional_requirements, technical_requirements, acceptance_criteria, created_at, specialists, priority, severity, parent_task_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			task.Title, task.Status, task.Type, task.FunctionalRequirements, task.TechnicalRequirements,
			task.AcceptanceCriteria, task.CreatedAt, task.Specialists, task.Priority, task.Severity,
			task.ParentTaskID,
		)
		if insertErr != nil {
			return insertErr
		}

		id, idErr := insertResult.LastInsertId()
		if idErr != nil {
			return idErr
		}
		taskID = int(id)

		// Log audit with same timestamp
		_, auditErr := tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpTaskCreate, models.EntityTask, taskID, now,
		)
		return auditErr
	})

	if err != nil {
		return err
	}

	return utils.PrintJSON(map[string]int{"id": taskID})
}
