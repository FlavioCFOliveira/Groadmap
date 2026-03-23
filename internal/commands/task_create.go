package commands

import (
	"database/sql"
	"fmt"
	"strconv"

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

	// Parse flags
	var title, functionalReqs, technicalReqs, acceptanceCriteria string
	var specialists string
	taskType := models.TypeTask
	priority := 0
	severity := 0

	for i := 0; i < len(remaining); i++ {
		switch remaining[i] {
		case "-t", "--title":
			if i+1 < len(remaining) {
				title = remaining[i+1]
				i++
			}
		case "-fr", "--functional-requirements":
			if i+1 < len(remaining) {
				functionalReqs = remaining[i+1]
				i++
			}
		case "-tr", "--technical-requirements":
			if i+1 < len(remaining) {
				technicalReqs = remaining[i+1]
				i++
			}
		case "-ac", "--acceptance-criteria":
			if i+1 < len(remaining) {
				acceptanceCriteria = remaining[i+1]
				i++
			}
		case "-y", "--type":
			if i+1 < len(remaining) {
				parsed, err := models.ParseTaskType(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: %s", utils.ErrInvalidInput, err.Error())
				}
				taskType = parsed
				i++
			}
		case "-p", "--priority":
			if i+1 < len(remaining) {
				p, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid priority: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				priority = p
				i++
			}
		case "--severity":
			if i+1 < len(remaining) {
				s, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid severity: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				severity = s
				i++
			}
		case "-sp", "--specialists":
			if i+1 < len(remaining) {
				specialists = remaining[i+1]
				i++
			}
		}
	}

	// Validate required fields
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

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

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
		result, err := tx.Exec(
			`INSERT INTO tasks (title, status, type, functional_requirements, technical_requirements, acceptance_criteria, created_at, specialists, priority, severity)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			task.Title, task.Status, task.Type, task.FunctionalRequirements, task.TechnicalRequirements,
			task.AcceptanceCriteria, task.CreatedAt, task.Specialists, task.Priority, task.Severity,
		)
		if err != nil {
			return err
		}

		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		taskID = int(id)

		// Log audit with same timestamp
		_, err = tx.Exec(
			`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
			models.OpTaskCreate, models.EntityTask, taskID, now,
		)
		return err
	})

	if err != nil {
		return err
	}

	return utils.PrintJSON(map[string]int{"id": taskID})
}
