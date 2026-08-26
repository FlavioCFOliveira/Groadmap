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
//   - -r, --roadmap: Roadmap name (uses current if not specified)
//
// Error conditions:
//   - Returns utils.ErrRequired if required fields are missing
//   - Returns utils.ErrValidation if priority/severity/type are out of range
//   - Returns utils.ErrFieldTooLarge if text fields exceed limits
//   - Returns utils.ErrNoRoadmap if no roadmap specified via -r flag
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
	priority, _ := result.Flags["Priority"].(int)
	severity, _ := result.Flags["Severity"].(int)
	parentIDRaw, hasParent := result.Flags["ParentID"].(int)

	// Parse task type (enum conversion after FlagParser)
	taskType := models.TypeTask
	if typeStr, ok := result.Flags["Type"].(string); ok && typeStr != "" {
		parsed, parseErr := models.ParseTaskType(typeStr)
		if parseErr != nil {
			return fmt.Errorf("%w: %w", utils.ErrValidation, parseErr)
		}
		taskType = parsed
	}

	// Was the flag supplied with any text at all? The message is
	// "<sentinel>: --flag" so the rendered stderr matches the SPEC canonical
	// exactly — e.g. "Error: required parameter missing: --title"
	// (SPEC/HELP.md, SPEC/DATA_FORMATS.md). Previously it embedded a redundant
	// "missing required parameter:" prefix, doubling the sentinel text
	// (finding #54).
	//
	// The test is against the value AS SUPPLIED, and that is the whole of the
	// distinction SPEC/COMMANDS.md § Emptiness Constraint (All Required
	// Free-Text Fields) draws: a required flag that is absent, or that carries
	// the literal empty string, is a flag that was never supplied, so the
	// refusal names the FLAG and exits 2. A flag carrying text that turns out
	// to name nothing — spaces, TAB, a no-break space — did reach the
	// application, so it is a rejected VALUE, and the loop below refuses it
	// with exit 6 naming the FIELD. This site used to trim first, which
	// collapsed the two cases into the first and reported a whitespace-only
	// value as a missing flag.
	if title == "" {
		return fmt.Errorf("%w: --title", utils.ErrRequired)
	}
	if functionalReqs == "" {
		return fmt.Errorf("%w: --functional-requirements", utils.ErrRequired)
	}
	if technicalReqs == "" {
		return fmt.Errorf("%w: --technical-requirements", utils.ErrRequired)
	}
	if acceptanceCriteria == "" {
		return fmt.Errorf("%w: --acceptance-criteria", utils.ErrRequired)
	}

	// Apply the whole free-text sequence to each of the four fields, through the
	// one helper that owns its order: the LENGTH cap on the value as it will be
	// stored, then the encoding rule and then the control-character rule on the
	// value AS SUPPLIED, then the trim, then the emptiness judgement on the
	// TRIMMED value (SPEC/MODELS.md § Free-Text Emptiness and Trimming
	// Constraint, and SPEC/COMMANDS.md for the cap's position). What is bound
	// back into the local is the trimmed value, so it is also what the INSERT
	// below writes (Rule 2).
	//
	// The order is the point, not a detail. Trimming first would remove a
	// leading or trailing VT or FF — forbidden control characters that
	// strings.TrimSpace also treats as whitespace — and the check would then
	// examine a value they had already vanished from. This site used to trim
	// first and had exactly that hole.
	//
	// The rules are applied field by field rather than in sweeps, so each field
	// is settled before the next is looked at and the precedence BETWEEN fields
	// — title, then functional, then technical, then acceptance — is exactly
	// what it was.
	//
	// The field is identified by a utils.Field, so a refusal carries the
	// published name of SPEC/COMMANDS.md § Published Field Names in Validation
	// Messages and cannot carry anything else. This is the site that used to pass
	// the four HYPHENATED FLAG names, which is why `task create` refused
	// `functional-requirements` while `task edit` refused
	// `functional_requirements` for the identical value and the identical rule.
	//
	// The length cap runs here too, ahead of the content rules, because
	// utils.RequireFreeText is the one place the whole order is stated (rmp task
	// 302). This command used to leave the cap to task.Validate() below, which
	// runs AFTER the content rules, so a title at once over-long and carrying a
	// BEL was refused here as a control character while `task edit -t` refused
	// the identical value for its length. Passing the maximum in is what puts
	// this command on the order every other write path already had.
	//
	// task.Validate() still applies the same caps, through the same helper. It
	// is the model's own invariant and no longer the first thing to answer for
	// these four values, so it now only ever confirms what this loop settled.
	for _, f := range []struct {
		value *string
		field utils.Field
		limit int
	}{
		{&title, utils.FieldTaskTitle, models.MaxTaskTitle},
		{&functionalReqs, utils.FieldTaskFunctionalRequirements, models.MaxTaskFunctionalRequirements},
		{&technicalReqs, utils.FieldTaskTechnicalRequirements, models.MaxTaskTechnicalRequirements},
		{&acceptanceCriteria, utils.FieldTaskAcceptanceCriteria, models.MaxTaskAcceptanceCriteria},
	} {
		stored, textErr := utils.RequireFreeText(*f.value, f.field, f.limit)
		if textErr != nil {
			return textErr
		}
		*f.value = stored
	}

	// Validate --parent value is a positive integer. The flag parser has
	// already parsed the token as an int, so a value < 1 is an out-of-range
	// value, not a syntax error: ErrValidation (exit 6) per SPEC.
	if hasParent && parentIDRaw < 1 {
		return fmt.Errorf("%w: --parent must be a positive integer", utils.ErrValidation)
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

	if err := task.Validate(); err != nil {
		return err
	}

	// Create task within transaction
	var taskID int
	err = database.WithTransaction(func(tx *sql.Tx) error {
		// Insert task
		id, insertErr := db.InsertTaskTx(tx, task)
		if insertErr != nil {
			return insertErr
		}
		taskID = id

		return db.LogAuditTx(tx, models.OpTaskCreate, models.EntityTask, taskID, now)
	})

	if err != nil {
		return err
	}

	return utils.PrintJSON(map[string]int{"id": taskID})
}
