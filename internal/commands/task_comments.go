// Package commands — the four comment subcommands of the `task` family.
//
// A task comment is a durable, typed entry in a task's work log: what was found,
// what was tried, what was decided and why (SPEC/COMMANDS.md § Task Comments).
// The four subcommands are flat subcommands of the family, in the form
// `task comment-<verb>`; there is no `rmp comment` family and no three-level
// `rmp task comment add` form.
//
// Everything the two comment families share — the positional id, the `--type`
// flag, the `--body` flag and its standard-input fallback, the validation order,
// the transaction boundary and the output — lives in comments.go. What is
// family-specific and therefore lives here: the accepted type set (the seven task
// values, through models.ParseTaskCommentType), the table the rows belong to, the
// parent the audit entry is written against, and the three TASK_COMMENT_*
// operations that entry carries.
package commands

import (
	"context"
	"database/sql"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// taskCommentFamily binds the shared subcommand bodies to task_comments.
//
// The type parser is models.ParseTaskCommentType, so the accepted set is the seven
// task values and a sprint-only reading of `--type` is impossible here. The audit
// entity is TASK with the parent task's id, never the comment's own id.
var taskCommentFamily = commentFamily{
	parseType: models.ParseTaskCommentType,

	parentExists: func(ctx context.Context, database *db.DB, taskID int) error {
		_, err := database.GetTask(ctx, taskID)
		return err
	},

	parentOf: func(ctx context.Context, database *db.DB, commentID int) (int, error) {
		comment, err := database.GetTaskComment(ctx, commentID)
		if err != nil {
			return 0, err
		}
		return comment.TaskID, nil
	},

	insert: func(tx *sql.Tx, taskID int, commentType models.CommentType, body, createdAt string) (int, error) {
		return db.InsertTaskCommentTx(tx, &models.TaskComment{
			Type:      commentType,
			Body:      body,
			CreatedAt: createdAt,
			TaskID:    taskID,
		})
	},
	update: db.UpdateTaskCommentTx,
	remove: db.DeleteTaskCommentTx,

	list: func(ctx context.Context, database *db.DB, taskID int, filter *models.CommentType) (any, error) {
		return database.ListTaskComments(ctx, taskID, filter)
	},

	opCreate: models.OpTaskCommentCreate,
	opUpdate: models.OpTaskCommentUpdate,
	opDelete: models.OpTaskCommentDelete,

	entityType:  models.EntityTask,
	parentLabel: "task",
}

// taskCommentAdd adds one comment to a task.
//
//	rmp task comment-add -r <roadmap> <task-id> --type <TYPE> [--body <text>]
//
// Accepted in every task status, including COMPLETED; the status is neither read
// as a precondition nor changed. See commentAdd for the validation order, which
// SPEC/COMMANDS.md § Add Task Comment pins as behaviour.
func taskCommentAdd(args []string) error {
	return commentAdd(&taskCommentFamily, args)
}

// taskCommentList returns a task's comments, oldest first.
//
//	rmp task comment-list -r <roadmap> <task-id> [--type <TYPE>]
//
// `--type` filters and must be one of the seven task values, so a value valid only
// on a sprint comment is rejected with exit code 6 rather than silently matching
// nothing. See commentList.
func taskCommentList(args []string) error {
	return commentList(&taskCommentFamily, args)
}

// taskCommentEdit changes the type and/or the body of one existing task comment.
//
//	rmp task comment-edit -r <roadmap> <comment-id> [--type <TYPE>] [--body <text>]
//
// The positional argument is the COMMENT's own id: an id that exists in
// sprint_comments but not in task_comments is a not-found condition here. See
// commentEdit.
func taskCommentEdit(args []string) error {
	return commentEdit(&taskCommentFamily, args)
}

// taskCommentRemove deletes one task comment, identified by the comment's own id.
//
//	rmp task comment-remove -r <roadmap> <comment-id>
//
// See commentRemove.
func taskCommentRemove(args []string) error {
	return commentRemove(&taskCommentFamily, args)
}
