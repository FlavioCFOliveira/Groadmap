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

// taskEditTextFields ties each free-text column `task edit` can write to the
// published name its validation messages use for that column and to the maximum
// the column accepts. One table, so the rules applied below cannot disagree
// about which columns are free text and — the point of it — cannot disagree
// about what to call one of them.
//
// The order is the order the fields are declared in SPEC/COMMANDS.md, and every
// sweep below walks the table in that order, so an invocation that breaks one
// rule on two fields always names the same one. The two maps this replaced were
// iterated in Go's randomised map order, so which of two offending fields got
// named varied between runs of the identical command.
var taskEditTextFields = []struct {
	column string
	limit  int
	field  utils.Field
}{
	{"title", models.MaxTaskTitle, utils.FieldTaskTitle},
	{"functional_requirements", models.MaxTaskFunctionalRequirements, utils.FieldTaskFunctionalRequirements},
	{"technical_requirements", models.MaxTaskTechnicalRequirements, utils.FieldTaskTechnicalRequirements},
	{"acceptance_criteria", models.MaxTaskAcceptanceCriteria, utils.FieldTaskAcceptanceCriteria},
}

// taskEditFieldOperations maps every column `task edit` can set to the audit
// operation that records a change to it. The keys are the column names the
// UPDATE statement is built from, not the flag spellings, because the statement
// is what the audit rows follow: they are written in the same order the columns
// are applied.
//
// Two of the seven reuse the operation of a dedicated setter command rather than
// declaring one of their own. `task edit -p 5` and `task prio <id> 5` perform
// the identical mutation, so recording them under different operations would
// have meant a filter on TASK_PRIORITY_CHANGE missed half the priority changes
// in the roadmap; --severity pairs with `task sev` the same way
// (SPEC/COMMANDS.md § Edit Task).
var taskEditFieldOperations = map[string]models.AuditOperation{
	"title":                   models.OpTaskTitleChange,
	"type":                    models.OpTaskTypeChange,
	"functional_requirements": models.OpTaskFunctionalRequirementsChange,
	"technical_requirements":  models.OpTaskTechnicalRequirementsChange,
	"acceptance_criteria":     models.OpTaskAcceptanceCriteriaChange,
	"priority":                models.OpTaskPriorityChange,
	"severity":                models.OpTaskSeverityChange,
}

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
//   - -p, --priority: New priority 0-9
//   - --severity: New severity 0-9
//   - -r, --roadmap: Roadmap name (uses current if not specified)
//
// Error conditions:
//   - Returns utils.ErrRequired if task ID is missing
//   - Returns utils.ErrNotFound if task doesn't exist
//   - Returns utils.ErrValidation if priority/severity/type out of range
//   - Returns utils.ErrFieldTooLarge if text fields exceed limits
//
// Side effects:
//   - Updates task record in database
//   - Logs one audit entry per supplied field, all sharing one performed_at
//   - Produces no stdout on success (exit 0), per SPEC/COMMANDS.md § Edit Task
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

	updates := make(map[string]any)

	// Each free-text flag is recorded AS SUPPLIED. The trim that used to happen
	// on this line now happens inside utils.RequireFreeText below, once the
	// encoding and control-character rules have seen the value the caller
	// actually sent (SPEC/MODELS.md § Free-Text Emptiness and Trimming
	// Constraint, step 2 before step 3). Trimming here is what let a leading or
	// trailing VT or FF through with the character silently discarded: both are
	// forbidden control characters AND whitespace to strings.TrimSpace, so the
	// check that ran later examined a value they had already vanished from
	// (CWE-150).
	if v, ok := result.Flags["Title"]; ok {
		updates["title"] = v.(string)
	}
	if v, ok := result.Flags["FunctionalRequirements"]; ok {
		updates["functional_requirements"] = v.(string)
	}
	if v, ok := result.Flags["TechnicalRequirements"]; ok {
		updates["technical_requirements"] = v.(string)
	}
	if v, ok := result.Flags["AcceptanceCriteria"]; ok {
		updates["acceptance_criteria"] = v.(string)
	}
	// Validate priority/severity range BEFORE the UPDATE. Without this, an
	// out-of-range value reached the SQLite CHECK constraint and surfaced as a
	// generic constraint error (exit 1) instead of the documented validation
	// error (exit 6) per SPEC/COMMANDS.md § Edit Task (finding #46).
	if v, ok := result.Flags["Priority"]; ok {
		p := v.(int)
		if err := utils.ValidateNumericRange(p, 0, 9, "priority"); err != nil {
			return err
		}
		updates["priority"] = p
	}
	if v, ok := result.Flags["Severity"]; ok {
		s := v.(int)
		if err := utils.ValidateNumericRange(s, 0, 9, "severity"); err != nil {
			return err
		}
		updates["severity"] = s
	}
	if typeStr, ok := result.Flags["Type"].(string); ok {
		parsed, parseErr := models.ParseTaskType(typeStr)
		if parseErr != nil {
			return fmt.Errorf("%w: %s", utils.ErrValidation, parseErr.Error())
		}
		updates["type"] = string(parsed)
	}

	// No-op: per SPEC/COMMANDS.md § Edit Task ("If no fields are specified,
	// command succeeds with no changes, exit code 0"), an edit with no fields
	// is a successful no-op that produces no output and writes no audit entry —
	// not a validation error (finding #48).
	if len(updates) == 0 {
		return nil
	}

	// The whole free-text sequence, through the one helper that owns its order
	// (rmp task 302): the LENGTH cap on the value as it will be stored, then the
	// encoding rule and then the control-character rule on the value AS
	// SUPPLIED, then the trim, then the emptiness judgement on the TRIMMED
	// value. The map entry is rebound to the trimmed value, so it is also what
	// the UPDATE below writes (SPEC/MODELS.md § Free-Text Emptiness and Trimming
	// Constraint, Rule 2).
	//
	// The cap keeps the position it has always had on this command — ahead of
	// the content rules — but it no longer runs as a sweep of its own with its
	// own strings.TrimSpace. That sweep was one of the seven statements of the
	// order the codebase carried, and having seven is how two of them came to
	// disagree; the cap is now the first rule inside utils.RequireFreeText and
	// this command states nothing about the order at all. Without this cap an
	// oversized value would reach SQLite and surface as a generic "constraint
	// failed" (exit 1) instead of the documented utils.ErrFieldTooLarge (exit 6).
	//
	// The refusal names the FIELD, by its published name (SPEC/COMMANDS.md
	// § Published Field Names in Validation Messages); utils.Field is what makes
	// that the only name it can carry.
	for _, f := range taskEditTextFields {
		str, ok := updates[f.column].(string)
		if !ok {
			continue
		}
		stored, textErr := utils.RequireFreeText(str, f.field, f.limit)
		if textErr != nil {
			return textErr
		}
		updates[f.column] = stored
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Capture timestamp once for the entire operation: every entry this
	// invocation writes carries it, which is what makes the entries of one
	// `task edit` recognisable as one event.
	now := utils.NowISO8601()

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
		queryArgs := make([]any, 0, len(fields)+1)
		ops := make([]models.AuditOperation, 0, len(fields))
		for _, field := range fields {
			op, known := taskEditFieldOperations[field]
			if !known {
				// Unreachable from the CLI: every key of `updates` is one of
				// the seven literals above. Refusing the whole edit rather
				// than writing the column silently is what keeps "one entry
				// per field the invocation supplies" true of a future field
				// too — an unmapped column fails loudly instead of changing
				// the task with nothing in its history to show for it.
				return fmt.Errorf("%w: no audit operation is declared for field %q", utils.ErrInvalidUpdate, field)
			}
			setParts = append(setParts, field+" = ?")
			queryArgs = append(queryArgs, updates[field])
			ops = append(ops, op)
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

		// One entry per field the invocation supplied, in the order the UPDATE
		// applied them, inside the transaction that performed it: a rejected
		// edit therefore writes no entry at all, and a committed one is never
		// recorded for only some of the fields it changed. The trigger is the
		// presence of the flag, not a difference in value — nothing above
		// compares a supplied value against the stored one, so `task edit`
		// records the command issued exactly as `task prio` already does
		// (SPEC/COMMANDS.md § Edit Task).
		return db.LogAuditFieldsTx(tx, models.EntityTask, taskID, now, ops...)
	})
}
