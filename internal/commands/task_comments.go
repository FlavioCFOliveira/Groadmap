// Package commands — the four comment subcommands of the `task` family.
//
// A task comment is a durable, typed entry in a task's work log: what was found,
// what was tried, what was decided and why (SPEC/COMMANDS.md § Task Comments).
// The four subcommands are flat subcommands of the family, in the form
// `task comment-<verb>`; there is no `rmp comment` family and no three-level
// `rmp task comment add` form.
//
// The input handling every comment subcommand shares — the positional id, the
// `--type` flag, the `--body` flag and its standard-input fallback — lives in
// comments.go. What is family-specific and therefore lives here: the accepted
// type set (the seven task values, through models.ParseTaskCommentType), the
// parent the audit entry is written against, and the table the rows belong to.
package commands

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// taskCommentAdd adds one comment to a task.
//
// Usage:
//
//	rmp task comment-add -r <roadmap> <task-id> --type <TYPE> [--body <text>]
//
// The validation order is the one SPEC/COMMANDS.md § Add Task Comment pins, and
// the order is behaviour, not style: `--type` is checked for presence and for
// value BEFORE the body is resolved, so a missing or invalid type never leaves
// the command waiting on standard input for a body it is going to reject anyway.
//
//  1. roadmap                     exit 3
//  2. positional task-id          exit 2
//  3. --type present              exit 2
//  4. --type value (7 task values) exit 6
//  5. body from --body or stdin   exit 2
//  6. task exists                 exit 4
//  7. body length / control chars exit 6
//  8. INSERT + TASK_COMMENT_CREATE audit entry, one transaction
//
// Side effects: one row in task_comments and one audit entry against the PARENT
// task, committed together. Prints {"id": <new-comment-id>} on success.
func taskCommentAdd(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	taskID, rest, err := requireCommentPositionalID(remaining, "task")
	if err != nil {
		return err
	}

	// Lexical only: the body is resolved at step 5, after the type verdict.
	rest, body := extractCommentBody(rest)

	typeRaw, typePresent, err := parseCommentTypeFlag(rest)
	if err != nil {
		return err
	}
	if !typePresent {
		return fmt.Errorf("%w: --type", utils.ErrRequired)
	}
	commentType, err := models.ParseTaskCommentType(typeRaw)
	if err != nil {
		return err
	}

	// On comment-add no other change is ever possible, so an absent --body always
	// means "read standard input" (SPEC precedence rule 2).
	raw, supplied, err := resolveCommentBody(body, true)
	if err != nil {
		return err
	}
	if !supplied {
		return errNoCommentBody()
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := requireTaskExists(database, taskID); err != nil {
		return err
	}

	// The body as supplied, not the trimmed form: strings.TrimSpace strips VT and
	// FF, both forbidden, so trimming before this call would drop them instead of
	// rejecting them. The value to persist is the one this returns.
	stored, err := models.ValidateCommentBody(raw)
	if err != nil {
		return err
	}

	// One timestamp for the row and its audit entry, so the two agree exactly.
	now := utils.NowISO8601()
	comment := &models.TaskComment{
		Type:      commentType,
		Body:      stored,
		CreatedAt: now,
		TaskID:    taskID,
	}

	var commentID int
	if err := database.WithTransaction(func(tx *sql.Tx) error {
		id, insertErr := db.InsertTaskCommentTx(tx, comment)
		if insertErr != nil {
			return insertErr
		}
		commentID = id

		// Recorded against the PARENT task, with the parent's id and
		// entity_type TASK — never the comment's own id
		// (SPEC/DATA_FORMATS.md § Audit Entry).
		return db.LogAuditTx(tx, models.OpTaskCommentCreate, models.EntityTask, taskID, now)
	}); err != nil {
		return err
	}

	return utils.PrintJSON(map[string]int{"id": commentID})
}

// taskCommentList returns a task's comments, oldest first.
//
// Usage:
//
//	rmp task comment-list -r <roadmap> <task-id> [--type <TYPE>]
//
// The order is the story the log tells, so it is created_at ascending with the
// comment id as the tie-breaker (SPEC/COMMANDS.md § List Task Comments). The
// result set is unbounded: there is no --limit, no --desc and no pagination.
//
// `--type` filters; its value MUST be one of the seven task values, so a value
// valid only on a sprint comment is rejected with exit code 6 rather than
// silently matching nothing. Writes no audit entry: listing is a read.
func taskCommentList(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	taskID, rest, err := requireCommentPositionalID(remaining, "task")
	if err != nil {
		return err
	}

	typeRaw, typePresent, err := parseCommentTypeFlag(rest)
	if err != nil {
		return err
	}
	var filter *models.CommentType
	if typePresent {
		commentType, parseErr := models.ParseTaskCommentType(typeRaw)
		if parseErr != nil {
			return parseErr
		}
		filter = &commentType
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := requireTaskExists(database, taskID); err != nil {
		return err
	}

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	comments, err := database.ListTaskComments(ctx, taskID, filter)
	if err != nil {
		return err
	}

	// An empty log is an empty array, never null: ListTaskComments returns an
	// empty slice for a task with nothing recorded yet.
	return utils.PrintJSON(comments)
}

// taskCommentEdit changes the type and/or the body of one existing task comment.
//
// Usage:
//
//	rmp task comment-edit -r <roadmap> <comment-id> [--type <TYPE>] [--body <text>]
//
// The positional argument is the COMMENT's own id, not the id of the task it
// belongs to, and the two id spaces are independent: an id that exists in
// sprint_comments but not in task_comments is a not-found condition here.
//
// At least one change is required, so unlike `task edit` this command does not
// succeed as a no-op. A change is requested by a `--type` value, by a `--body`
// value, or by a body arriving on standard input — which is why the flagless form
// `comment-edit <comment-id> < revised.txt` is a valid edit and the decision is
// made only after standard input has been resolved. Standard input is read ONLY
// when `--type` is absent as well, so a type-only edit never waits for input.
//
// The edit replaces the body in place and stamps updated_at; the previous text is
// not retained anywhere. Produces no output on success.
func taskCommentEdit(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	commentID, rest, err := requireCommentPositionalID(remaining, "comment")
	if err != nil {
		return err
	}

	rest, body := extractCommentBody(rest)

	typeRaw, typePresent, err := parseCommentTypeFlag(rest)
	if err != nil {
		return err
	}
	var newType *models.CommentType
	if typePresent {
		// Before standard input is considered, so an invalid type cannot leave the
		// command blocked on a terminal.
		commentType, parseErr := models.ParseTaskCommentType(typeRaw)
		if parseErr != nil {
			return parseErr
		}
		newType = &commentType
	}

	raw, supplied, err := resolveCommentBody(body, !typePresent)
	if err != nil {
		return err
	}
	if !supplied && newType == nil {
		return errNoCommentChange()
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Resolving the comment reports a missing id before any write and supplies the
	// parent task id the audit entry is written against.
	comment, err := database.GetTaskComment(ctx, commentID)
	if err != nil {
		return err
	}

	var newBody *string
	if supplied {
		stored, valErr := models.ValidateCommentBody(raw)
		if valErr != nil {
			return valErr
		}
		newBody = &stored
	}

	now := utils.NowISO8601()
	return database.WithTransaction(func(tx *sql.Tx) error {
		if updErr := db.UpdateTaskCommentTx(tx, commentID,
			&db.CommentUpdate{Type: newType, Body: newBody}, now); updErr != nil {
			return updErr
		}
		return db.LogAuditTx(tx, models.OpTaskCommentUpdate, models.EntityTask, comment.TaskID, now)
	})
}

// taskCommentRemove deletes one task comment.
//
// Usage:
//
//	rmp task comment-remove -r <roadmap> <comment-id>
//
// The positional argument is the COMMENT's own id. Exactly one id is accepted: no
// comma-separated list, so the batch fail-fast rules of `task remove` do not
// apply here.
//
// The row is removed outright — no soft delete, no recovery — but the audit entry
// outlives it, so the task's history still records that a comment existed and was
// removed. Produces no output on success.
func taskCommentRemove(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	commentID, rest, err := requireCommentPositionalID(remaining, "comment")
	if err != nil {
		return err
	}
	if err := rejectUnknownFlags(rest); err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	comment, err := database.GetTaskComment(ctx, commentID)
	if err != nil {
		return err
	}

	now := utils.NowISO8601()
	return database.WithTransaction(func(tx *sql.Tx) error {
		if delErr := db.DeleteTaskCommentTx(tx, commentID); delErr != nil {
			return delErr
		}
		return db.LogAuditTx(tx, models.OpTaskCommentDelete, models.EntityTask, comment.TaskID, now)
	})
}

// requireTaskExists reports a missing task as the exit-4 condition
// SPEC/COMMANDS.md pins for the comment subcommands.
//
// db.GetTask's own message ("task 42") is deliberately not reused: the SPEC pins
// "task 42 not found" here. Any other failure is passed through untouched, so a
// genuine database error stays a database error (exit code 1) instead of being
// reported as a missing task.
func requireTaskExists(database *db.DB, taskID int) error {
	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	if _, err := database.GetTask(ctx, taskID); err != nil {
		if errors.Is(err, utils.ErrNotFound) {
			return fmt.Errorf("%w: task %d not found", utils.ErrNotFound, taskID)
		}
		return err
	}
	return nil
}
