// Package commands — the four comment subcommands of the `sprint` family.
//
// A sprint comment is a durable, typed log entry attached to a sprint. It records
// how the sprint itself went — findings, decisions taken, progress, and the reason
// behind a change to the sprint's own definition — and not the work carried out
// inside any one task, which belongs in that task's comments
// (SPEC/COMMANDS.md § Sprint Comments). The four subcommands are flat subcommands
// of the family, in the form `sprint comment-<verb>`.
//
// They mirror the task family exactly, so the steps they take live in comments.go
// and are not restated here. Only what differs is declared in this file:
//
//   - the accepted type set is the FOUR sprint values, through
//     models.ParseSprintCommentType; the task-only HYPOTHESIS, TEST and NOTE are
//     refused with exit code 6, by this layer and by the table's own CHECK;
//   - the rows belong to sprint_comments, whose id sequence is independent of
//     task_comments, so `sprint comment-edit 7` and `task comment-edit 7` address
//     two unrelated rows;
//   - the audit entries are SPRINT_COMMENT_CREATE / _UPDATE / _DELETE, written
//     against the parent SPRINT with entity_type SPRINT and the parent sprint's id.
//
// Comments are accepted in every sprint status, including CLOSED. No subcommand
// here reads a sprint's status as a precondition or changes it.
package commands

import (
	"context"
	"database/sql"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// sprintCommentFamily binds the shared subcommand bodies to sprint_comments.
//
// parentExists is db.GetSprint: it answers whether the sprint is there, and its
// verdict is the only thing used — the sprint's status is deliberately not
// consulted, because a comment is accepted whatever that status is.
var sprintCommentFamily = commentFamily{
	parseType: models.ParseSprintCommentType,

	parentExists: func(ctx context.Context, database *db.DB, sprintID int) error {
		_, err := database.GetSprint(ctx, sprintID)
		return err
	},

	parentOf: func(ctx context.Context, database *db.DB, commentID int) (int, error) {
		comment, err := database.GetSprintComment(ctx, commentID)
		if err != nil {
			return 0, err
		}
		return comment.SprintID, nil
	},

	insert: func(tx *sql.Tx, sprintID int, commentType models.CommentType, body, createdAt string) (int, error) {
		return db.InsertSprintCommentTx(tx, &models.SprintComment{
			Type:      commentType,
			Body:      body,
			CreatedAt: createdAt,
			SprintID:  sprintID,
		})
	},
	update: db.UpdateSprintCommentTx,
	remove: db.DeleteSprintCommentTx,

	list: func(ctx context.Context, database *db.DB, sprintID int, filter *models.CommentType) (any, error) {
		return database.ListSprintComments(ctx, sprintID, filter)
	},

	opCreate: models.OpSprintCommentCreate,
	opUpdate: models.OpSprintCommentUpdate,
	opDelete: models.OpSprintCommentDelete,

	entityType:    models.EntitySprint,
	parentIDField: utils.FieldSprintID,
}

// sprintCommentAdd adds one comment to a sprint.
//
//	rmp sprint comment-add -r <roadmap> <sprint-id> --type <TYPE> [--body <text>]
//
// Accepted in every sprint status, including CLOSED, and the status is left
// untouched. See commentAdd for the validation order, which
// SPEC/COMMANDS.md § Add Sprint Comment adopts unchanged from the task family with
// the sprint in place of the task and the four sprint values in place of the seven.
func sprintCommentAdd(args []string) error {
	return commentAdd(&sprintCommentFamily, args)
}

// sprintCommentList returns a sprint's comments, oldest first.
//
//	rmp sprint comment-list -r <roadmap> <sprint-id> [--type <TYPE>]
//
// `--type` filters and must be one of the four sprint values: a task-only value
// such as HYPOTHESIS is rejected with exit code 6, not answered with an empty
// array, so a caller is told the filter is meaningless here instead of concluding
// the sprint has no such comments. See commentList.
func sprintCommentList(args []string) error {
	return commentList(&sprintCommentFamily, args)
}

// sprintCommentEdit changes the type and/or the body of one existing sprint
// comment.
//
//	rmp sprint comment-edit -r <roadmap> <comment-id> [--type <TYPE>] [--body <text>]
//
// The positional argument is the COMMENT's own id, resolved in sprint_comments
// only: an id that exists in task_comments but not in sprint_comments is a
// not-found condition here. See commentEdit.
func sprintCommentEdit(args []string) error {
	return commentEdit(&sprintCommentFamily, args)
}

// sprintCommentRemove deletes one sprint comment, identified by the comment's own
// id.
//
//	rmp sprint comment-remove -r <roadmap> <comment-id>
//
// See commentRemove.
func sprintCommentRemove(args []string) error {
	return commentRemove(&sprintCommentFamily, args)
}
