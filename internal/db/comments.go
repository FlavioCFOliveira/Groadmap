package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== COMMENT QUERIES ====================
//
// The query layer of task_comments and sprint_comments (SPEC/DATABASE.md §
// Comments). It is organised as one statement set per table plus one shared
// engine, which is the shape SPEC/DATABASE.md § sprint_comments Table ("Two
// tables, deliberately") asks for: the two tables are independent, so each keeps
// its own statements, while everything that does not depend on the table --
// scanning, filtering, ordering, row-count checks, error classification -- exists
// once.
//
// Every statement is a compile-time literal and every VALUE travels as a ?
// placeholder. The single exception is the grouped read, whose IN list needs one
// placeholder per id; it interpolates the placeholder string produced by the
// connection's cache and never an id.
//
// Mutations take a *sql.Tx rather than opening their own transaction. The caller
// bundles the write with its audit entry (LogAuditTx) in one commit, so a
// committed comment change can never exist without its audit record and an audit
// record can never survive a rolled-back change (SPEC/DATABASE.md §
// Transactional Atomicity Guarantees #7).

// CommentUpdate carries the columns a comment edit changes. A nil field leaves
// that column untouched, which is what makes the three UPDATE shapes
// SPEC/DATABASE.md § Update Comment specifies (type and body, body only, type
// only) reachable from one call. At least one field must be set: an edit that
// requests no change is rejected, matching `comment-edit`, which fails with exit
// code 2 rather than succeeding as a no-op (SPEC/COMMANDS.md § Edit Task
// Comment).
type CommentUpdate struct {
	Type *models.CommentType
	Body *string
}

// commentRow is the row shape both comment tables select. ParentID holds task_id
// or sprint_id according to the table read; the per-entity converters below place
// it in the right field of the right model.
//
// The field order is the one fieldalignment asks for, not the projection's column
// order (dest() below carries that), so the nullable updated_at sits after the
// plain strings rather than first as in the models.
type commentRow struct {
	Type      models.CommentType
	Body      string
	CreatedAt string
	UpdatedAt sql.NullString
	ID        int
	ParentID  int
}

// dest returns the scan destinations of the row, in the projection's column
// order. Both scan sites take them from here, so the projection and the scan
// cannot drift apart.
func (r *commentRow) dest() []any {
	return []any{&r.ID, &r.ParentID, &r.Type, &r.Body, &r.CreatedAt, &r.UpdatedAt}
}

// updatedAt returns the edit timestamp as the models carry it: nil while the
// comment has never been edited. The value is copied into fresh storage, so rows
// materialised from a reused commentRow never alias each other.
func (r *commentRow) updatedAt() *string {
	if !r.UpdatedAt.Valid {
		return nil
	}
	v := r.UpdatedAt.String
	return &v
}

// toTaskComment materialises the row as a task comment.
func (r *commentRow) toTaskComment() models.TaskComment {
	return models.TaskComment{
		UpdatedAt: r.updatedAt(),
		Type:      r.Type,
		Body:      r.Body,
		CreatedAt: r.CreatedAt,
		ID:        r.ID,
		TaskID:    r.ParentID,
	}
}

// toSprintComment materialises the row as a sprint comment.
func (r *commentRow) toSprintComment() models.SprintComment {
	return models.SprintComment{
		UpdatedAt: r.updatedAt(),
		Type:      r.Type,
		Body:      r.Body,
		CreatedAt: r.CreatedAt,
		ID:        r.ID,
		SprintID:  r.ParentID,
	}
}

// commentStatements is the statement set of one comment table. The SQL is
// written out per table instead of being assembled from a table name, so no
// identifier is ever concatenated into a query at run time and every statement
// in this file can be read exactly as the database receives it.
//
// parseType and the two labels make the shared engine speak about the right
// entity: parseType produces the per-entity type rejection SPEC/COMMANDS.md pins
// (the seven task values or the four sprint values), and the labels name the row
// in not-found messages.
type commentStatements struct {
	parseType             func(string) (models.CommentType, error)
	insert                string
	selectByID            string
	selectByParent        string
	selectByParentAndType string
	updateTypeAndBody     string
	updateBody            string
	updateType            string
	deleteByID            string
	parentLabel           string // "task" / "sprint"
	commentLabel          string // "task comment" / "sprint comment"
}

// taskCommentStmts is the statement set of task_comments (SPEC/DATABASE.md §
// Comments, task form).
var taskCommentStmts = commentStatements{
	insert: `INSERT INTO task_comments (task_id, type, body, created_at) VALUES (?, ?, ?, ?)`,

	selectByID: `SELECT id, task_id, type, body, created_at, updated_at
	 FROM task_comments
	 WHERE id = ?`,

	selectByParent: `SELECT id, task_id, type, body, created_at, updated_at
	 FROM task_comments
	 WHERE task_id = ?
	 ORDER BY created_at ASC, id ASC`,

	selectByParentAndType: `SELECT id, task_id, type, body, created_at, updated_at
	 FROM task_comments
	 WHERE task_id = ? AND type = ?
	 ORDER BY created_at ASC, id ASC`,

	updateTypeAndBody: `UPDATE task_comments SET type = ?, body = ?, updated_at = ? WHERE id = ?`,
	updateBody:        `UPDATE task_comments SET body = ?, updated_at = ? WHERE id = ?`,
	updateType:        `UPDATE task_comments SET type = ?, updated_at = ? WHERE id = ?`,
	deleteByID:        `DELETE FROM task_comments WHERE id = ?`,

	parentLabel:  "task",
	commentLabel: "task comment",
	parseType:    models.ParseTaskCommentType,
}

// sprintCommentStmts is the statement set of sprint_comments. It has no grouped
// read: nothing reads the comments of several sprints at once (SPEC/DATABASE.md §
// List Comments for Many Parents (Grouped)).
var sprintCommentStmts = commentStatements{
	insert: `INSERT INTO sprint_comments (sprint_id, type, body, created_at) VALUES (?, ?, ?, ?)`,

	selectByID: `SELECT id, sprint_id, type, body, created_at, updated_at
	 FROM sprint_comments
	 WHERE id = ?`,

	selectByParent: `SELECT id, sprint_id, type, body, created_at, updated_at
	 FROM sprint_comments
	 WHERE sprint_id = ?
	 ORDER BY created_at ASC, id ASC`,

	selectByParentAndType: `SELECT id, sprint_id, type, body, created_at, updated_at
	 FROM sprint_comments
	 WHERE sprint_id = ? AND type = ?
	 ORDER BY created_at ASC, id ASC`,

	updateTypeAndBody: `UPDATE sprint_comments SET type = ?, body = ?, updated_at = ? WHERE id = ?`,
	updateBody:        `UPDATE sprint_comments SET body = ?, updated_at = ? WHERE id = ?`,
	updateType:        `UPDATE sprint_comments SET type = ?, updated_at = ? WHERE id = ?`,
	deleteByID:        `DELETE FROM sprint_comments WHERE id = ?`,

	parentLabel:  "sprint",
	commentLabel: "sprint comment",
	parseType:    models.ParseSprintCommentType,
}

// groupedTaskCommentCountsQuery returns the grouped counting read of
// SPEC/DATABASE.md § Count Comments for Many Parents (Grouped) for an IN list of
// the given placeholders. It reads no body: a surface that shows a NUMBER has no
// use for the text, and reading every comment of every task in order to display a
// count is work thrown away.
//
// The GROUP BY drops a task with no comment rather than returning a zero row, so
// the caller reads a missing key as zero. The ORDER BY is task_id ascending, so
// the result is walkable in one pass against a caller-side set of task ids.
//
// It is a function rather than a constant because the IN list has one placeholder
// per id. Assembly is separated from execution so the index tests can plan the
// exact SQL production runs, rather than a lookalike.
func groupedTaskCommentCountsQuery(placeholders string) string {
	return fmt.Sprintf( // #nosec G201 -- only ? placeholders are interpolated; every id is bound
		`SELECT task_id, COUNT(*) AS comment_count
	 FROM task_comments
	 WHERE task_id IN (%s)
	 GROUP BY task_id
	 ORDER BY task_id ASC`,
		placeholders,
	)
}

// ==================== COMMENT MUTATIONS (transactional) ====================

// InsertTaskCommentTx inserts one comment on a task inside an existing
// transaction and returns the new comment's id.
//
// created_at is taken from the comment (the caller stamps it, as with
// InsertTaskTx); updated_at starts NULL and is written only by a later edit. The
// body is stored in its canonical form: NormalizeCommentBody trims it, which is
// the form SPEC/COMMANDS.md validates and SPEC/DATABASE.md stores, so the value
// measured against the 4096-character cap and the value written are the same one.
//
// The rules the database enforces (the type CHECK, the body cap, the parent
// foreign key) are reported here as the project's error sentinels; see writeError.
// The rules it does not enforce - a non-empty body, no forbidden control
// characters - belong to models.ValidateCommentBody and are applied by the command
// layer before this call, so an empty body reaches the table only if a caller
// skipped that validation (SPEC/DATABASE.md § task_comments Table: no CHECK
// forbids it).
//
// The caller MUST write the TASK_COMMENT_CREATE audit entry against the parent
// task in this same transaction (LogAuditTx).
func InsertTaskCommentTx(tx *sql.Tx, comment *models.TaskComment) (int, error) {
	return insertComment(tx, &taskCommentStmts, comment.TaskID, comment.Type, comment.Body, comment.CreatedAt)
}

// InsertSprintCommentTx inserts one comment on a sprint inside an existing
// transaction and returns the new comment's id. The caller MUST write the
// SPRINT_COMMENT_CREATE audit entry against the parent sprint in this same
// transaction.
func InsertSprintCommentTx(tx *sql.Tx, comment *models.SprintComment) (int, error) {
	return insertComment(tx, &sprintCommentStmts, comment.SprintID, comment.Type, comment.Body, comment.CreatedAt)
}

// UpdateTaskCommentTx applies an edit to one task comment inside an existing
// transaction and stamps updated_at. created_at is never touched.
//
// Returns utils.ErrNotFound when no comment carries the id (exit code 4), and
// utils.ErrInvalidUpdate when the edit requests no change at all. The caller MUST
// write the TASK_COMMENT_UPDATE audit entry against the parent task in this same
// transaction; the parent id comes from resolving the comment beforehand with
// GetTaskComment, which is also what reports a missing comment before any write.
func UpdateTaskCommentTx(tx *sql.Tx, id int, update *CommentUpdate, updatedAt string) error {
	return updateComment(tx, &taskCommentStmts, id, update, updatedAt)
}

// UpdateSprintCommentTx applies an edit to one sprint comment inside an existing
// transaction and stamps updated_at, leaving created_at untouched. The caller
// MUST write the SPRINT_COMMENT_UPDATE audit entry against the parent sprint in
// this same transaction.
func UpdateSprintCommentTx(tx *sql.Tx, id int, update *CommentUpdate, updatedAt string) error {
	return updateComment(tx, &sprintCommentStmts, id, update, updatedAt)
}

// DeleteTaskCommentTx deletes one task comment inside an existing transaction.
// The row is removed outright: there is no soft delete and no tombstone.
//
// Returns utils.ErrNotFound when no comment carries the id. The caller MUST write
// the TASK_COMMENT_DELETE audit entry against the parent task in this same
// transaction; that entry outlives the row, so the task's history still records
// that a comment existed and was removed.
func DeleteTaskCommentTx(tx *sql.Tx, id int) error {
	return deleteComment(tx, &taskCommentStmts, id)
}

// DeleteSprintCommentTx deletes one sprint comment inside an existing
// transaction, returning utils.ErrNotFound when no comment carries the id. The
// caller MUST write the SPRINT_COMMENT_DELETE audit entry against the parent
// sprint in this same transaction.
func DeleteSprintCommentTx(tx *sql.Tx, id int) error {
	return deleteComment(tx, &sprintCommentStmts, id)
}

// insertComment is the insert both entities share.
func insertComment(tx *sql.Tx, st *commentStatements, parentID int,
	commentType models.CommentType, body, createdAt string) (int, error) {
	stored := models.NormalizeCommentBody(body)

	result, err := tx.Exec(st.insert, parentID, string(commentType), stored, createdAt)
	if err != nil {
		return 0, st.writeError(err, parentID, &commentType, &stored)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting last insert id: %w", err)
	}
	return int(id), nil
}

// updateComment is the edit both entities share. It picks the UPDATE shape from
// the fields the caller set, so only the requested columns are written.
func updateComment(tx *sql.Tx, st *commentStatements, id int, update *CommentUpdate, updatedAt string) error {
	if update == nil || (update.Type == nil && update.Body == nil) {
		return fmt.Errorf("%w: %s %d: at least one of type or body must be set",
			utils.ErrInvalidUpdate, st.commentLabel, id)
	}

	var (
		query  string
		args   []any
		stored *string
	)
	if update.Body != nil {
		// Trimmed exactly as on insert, so an edit cannot leave a body in the
		// table that a fresh insert of the same text would have stored differently.
		v := models.NormalizeCommentBody(*update.Body)
		stored = &v
	}

	switch {
	case update.Type != nil && stored != nil:
		query, args = st.updateTypeAndBody, []any{string(*update.Type), *stored, updatedAt, id}
	case stored != nil:
		query, args = st.updateBody, []any{*stored, updatedAt, id}
	default:
		query, args = st.updateType, []any{string(*update.Type), updatedAt, id}
	}

	result, err := tx.Exec(query, args...)
	if err != nil {
		// No parent id is passed: an edit never writes the parent column, so it
		// cannot fail the foreign key, and only the type and body branches of the
		// classifier are reachable from here.
		return st.writeError(err, 0, update.Type, stored)
	}
	return st.requireAffectedRow(result, id)
}

// deleteComment is the delete both entities share.
func deleteComment(tx *sql.Tx, st *commentStatements, id int) error {
	result, err := tx.Exec(st.deleteByID, id)
	if err != nil {
		return fmt.Errorf("deleting %s %d: %w", st.commentLabel, id, err)
	}
	return st.requireAffectedRow(result, id)
}

// requireAffectedRow turns "the statement matched no row" into the not-found
// condition the CLI reports as exit code 4. An UPDATE or DELETE on a missing id
// is not a database error: it succeeds and touches nothing, so without this check
// a `comment-edit` on an unknown id would report success.
func (st *commentStatements) requireAffectedRow(result sql.Result, id int) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking affected rows for %s %d: %w", st.commentLabel, id, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %s %d not found", utils.ErrNotFound, st.commentLabel, id)
	}
	return nil
}

// writeError classifies a failed comment write onto the project's error
// sentinels, using the SQLite EXTENDED result code (the primary code 19 is shared
// by every constraint kind and would say nothing).
//
// IsUniqueConstraintErr deliberately answers false for all three codes below, and
// none of them may be routed through it: a comment table has no uniqueness
// constraint, so reporting any of these as an "already in use" collision (exit
// code 5) would be wrong.
//
//   - 787 FOREIGN KEY: the parent row does not exist. That is a not-found
//     condition (exit code 4) about the parent, not about the comment. The command
//     layer checks the parent first, so reaching this means the parent was deleted
//     in the window between that check and the write.
//   - 275 CHECK: either the type is outside the set this entity accepts or the
//     body exceeds 4096 characters. Which one it was is decided by re-running the
//     domain rules over the values the statement carried, so the message is the one
//     SPEC/COMMANDS.md pins and no rule is restated here.
//   - 1299 NOT NULL: a required column arrived NULL. Unreachable through this
//     API (parentID is an int, body and created_at are strings), so it is mapped
//     rather than described: a validation failure, never a uniqueness collision.
//
// commentType and body are the values the statement wrote, or nil for a column
// the statement did not touch (a type-only edit writes no body, and vice versa),
// so a column that was not written is never blamed for the failure.
func (st *commentStatements) writeError(err error, parentID int, commentType *models.CommentType, body *string) error {
	switch extendedResultCode(err) {
	case sqliteConstraintForeignKey:
		return fmt.Errorf("%w: %s %d not found", utils.ErrNotFound, st.parentLabel, parentID)

	case sqliteConstraintCheck:
		// Type first, then body: the order SPEC/COMMANDS.md § Add Task Comment
		// pins for the same two rules at the command layer.
		if commentType != nil {
			if _, verr := st.parseType(string(*commentType)); verr != nil {
				return verr
			}
		}
		if body != nil {
			// Only the oversize verdict is surfaced. The other rules
			// ValidateCommentBody enforces (non-empty, no control characters) have
			// no CHECK behind them, so they cannot be the reason this statement
			// was rejected.
			if _, verr := models.ValidateCommentBody(*body); verr != nil &&
				errors.Is(verr, utils.ErrFieldTooLarge) {
				return verr
			}
		}

	case sqliteConstraintNotNull:
		return fmt.Errorf("%w: %s: a required column was NULL", utils.ErrValidation, st.commentLabel)
	}

	// Anything the classifier above did not recognise is "any other
	// database/sql error", which SPEC/ARCHITECTURE.md § Propagation Rules
	// assigns to utils.ErrDatabase (exit code 1) at THIS layer. The sentinel
	// is added here and nowhere earlier: the switch above owns the two rows
	// that sit ABOVE utils.ErrDatabase in that table (a foreign-key failure
	// is utils.ErrNotFound, exit code 4, and a uniqueness collision would be
	// utils.ErrAlreadyExists, exit code 5), so classifying first and wrapping
	// only what falls through is what keeps those exit codes intact. The
	// underlying error stays wrapped with %w as well, so the driver's own
	// error remains inspectable through the chain.
	return fmt.Errorf("%w: writing %s: %w", utils.ErrDatabase, st.commentLabel, err)
}

// ==================== COMMENT READS ====================

// GetTaskComment retrieves one task comment by its own id. The id space is
// per-table, so an id that exists in sprint_comments but not in task_comments is
// a not-found condition here (utils.ErrNotFound, exit code 4).
//
// The command layer resolves a comment through this call before editing or
// removing it, which is what reports a missing id before any write and what
// supplies the parent id the audit entry is written against.
func (db *DB) GetTaskComment(ctx context.Context, id int) (*models.TaskComment, error) {
	return getComment(ctx, db, &taskCommentStmts, id, (*commentRow).toTaskComment)
}

// GetSprintComment retrieves one sprint comment by its own id, returning
// utils.ErrNotFound when no row carries it.
func (db *DB) GetSprintComment(ctx context.Context, id int) (*models.SprintComment, error) {
	return getComment(ctx, db, &sprintCommentStmts, id, (*commentRow).toSprintComment)
}

// ListTaskComments returns every comment of one task, oldest first: created_at
// ascending with the comment id ascending as the tie-breaker for comments created
// within the same millisecond. The listing is the task's work log, so the order is
// the story it tells.
//
// commentType is an optional filter: nil returns every comment, a non-nil value
// returns only the comments of that type. The result set is unbounded (no LIMIT,
// no OFFSET). A task with no comments, or no comment of the requested type,
// yields an EMPTY slice and no error.
//
// The task's existence is not checked here: the command layer verifies it first,
// so a missing task is reported as exit code 4 before this query runs.
func (db *DB) ListTaskComments(ctx context.Context, taskID int, commentType *models.CommentType) ([]models.TaskComment, error) {
	return listComments(ctx, db, &taskCommentStmts, taskID, commentType, (*commentRow).toTaskComment)
}

// ListSprintComments returns every comment of one sprint, oldest first, with the
// same optional type filter and the same empty-slice-on-no-match contract as
// ListTaskComments.
func (db *DB) ListSprintComments(ctx context.Context, sprintID int, commentType *models.CommentType) ([]models.SprintComment, error) {
	return listComments(ctx, db, &sprintCommentStmts, sprintID, commentType, (*commentRow).toSprintComment)
}

// CountTaskCommentsByTasks returns how many comments each of the given tasks has,
// keyed by task id, in ONE statement whatever the number of tasks — and without
// reading a single comment body.
//
// This is the read a surface that displays a comment COUNT must use. The web
// interface's Kanban board shows a count on each card and no comment text: a
// task's comment text is read only when a user opens that task's detail modal, by
// the task detail endpoint, which reads that one task through the single-parent
// listing above (SPEC/WEB.md § Roadmap Tasks Page, read cost; § Task Detail
// Endpoint). No surface reads the comment text of several tasks at once, so no
// grouped listing exists.
//
// A task with no comment is ABSENT from the map: the GROUP BY produces no group
// for it, so the query never returns a zero row and the caller reads the missing
// key's zero value, which is already the right answer.
//
// An empty id set issues no statement at all and returns an empty map. Duplicate
// ids in the input are harmless; each task is counted once.
func (db *DB) CountTaskCommentsByTasks(ctx context.Context, taskIDs []int) (map[int]int, error) {
	counts := make(map[int]int, len(taskIDs))
	if len(taskIDs) == 0 {
		return counts, nil
	}

	args := make([]any, len(taskIDs))
	for i, id := range taskIDs {
		args[i] = id
	}

	rows, err := db.QueryContext(ctx, groupedTaskCommentCountsQuery(db.Placeholders(len(taskIDs))), args...)
	if err != nil {
		return nil, fmt.Errorf("counting task comments by task: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var taskID, count int
		if err := rows.Scan(&taskID, &count); err != nil {
			return nil, fmt.Errorf("scanning task comment count: %w", err)
		}
		counts[taskID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task comment count rows: %w", err)
	}
	return counts, nil
}

// getComment is the by-id read both entities share.
func getComment[T any](ctx context.Context, db *DB, st *commentStatements, id int,
	convert func(*commentRow) T) (*T, error) {
	var row commentRow
	err := db.QueryRowContext(ctx, st.selectByID, id).Scan(row.dest()...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s %d not found", utils.ErrNotFound, st.commentLabel, id)
	}
	if err != nil {
		// Reached only once sql.ErrNoRows has been ruled out just above, so
		// the not-found row of SPEC/ARCHITECTURE.md § Propagation Rules still
		// resolves to utils.ErrNotFound (exit code 4) and only a genuine
		// database failure lands on utils.ErrDatabase (exit code 1).
		return nil, fmt.Errorf("%w: querying %s %d: %w", utils.ErrDatabase, st.commentLabel, id, err)
	}

	comment := convert(&row)
	return &comment, nil
}

// listComments is the single-parent listing both entities share. The type filter
// selects between the two statements SPEC/DATABASE.md § List Comments for One
// Parent specifies rather than appending a clause, so both are literals.
func listComments[T any](ctx context.Context, db *DB, st *commentStatements, parentID int,
	commentType *models.CommentType, convert func(*commentRow) T) ([]T, error) {
	query, args := st.selectByParent, []any{parentID}
	if commentType != nil {
		query, args = st.selectByParentAndType, []any{parentID, string(*commentType)}
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		// A listing has no not-found and no conflict row: an empty result is
		// an empty slice (see collectComments), so every failure here is
		// "any other database/sql error" and carries utils.ErrDatabase.
		return nil, fmt.Errorf("%w: querying %ss: %w", utils.ErrDatabase, st.commentLabel, err)
	}
	defer rows.Close()

	return collectComments(rows, convert)
}

// collectComments materialises every selected row through convert. An empty
// result is an empty slice, never nil: a comment listing that returns nothing is
// a task with nothing recorded yet, not a failure.
func collectComments[T any](rows *sql.Rows, convert func(*commentRow) T) ([]T, error) {
	comments := make([]T, 0, 8)
	if err := forEachCommentRow(rows, func(row *commentRow) {
		comments = append(comments, convert(row))
	}); err != nil {
		return nil, err
	}
	return comments, nil
}

// forEachCommentRow scans every row of a comment projection and hands it to
// visit. It is the one place the projection is scanned, so the column order lives
// in a single function for both tables and every read path.
//
// The row is reused across iterations: visit must copy what it keeps, which the
// converters do (commentRow.updatedAt copies the string it points at).
func forEachCommentRow(rows *sql.Rows, visit func(*commentRow)) error {
	var row commentRow
	dest := row.dest() // The destinations are addresses into row; they never change.

	for rows.Next() {
		row = commentRow{}
		if err := rows.Scan(dest...); err != nil {
			return fmt.Errorf("scanning comment row: %w", err)
		}
		visit(&row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating comment rows: %w", err)
	}
	return nil
}
