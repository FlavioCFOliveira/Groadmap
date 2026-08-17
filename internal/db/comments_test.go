package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The comment persistence layer (rmp task #162), tested against the database
// rather than against the values the functions return: every assertion about a
// stored row is made by reading that row back with raw SQL, so a function that
// returns the right value while writing the wrong row fails here.

// storedComment is one comment as the DATABASE holds it, read with raw SQL.
type storedComment struct {
	updatedAt *string
	typ       string
	body      string
	createdAt string
	parentID  int
}

// readStoredComment reads one comment straight out of table with raw SQL. It
// deliberately does not go through the production read path: the point is to
// describe the row that was written, independently of the code that wrote it.
func readStoredComment(t *testing.T, db *DB, table, parentCol string, id int) (storedComment, bool) {
	t.Helper()

	var (
		got       storedComment
		updatedAt sql.NullString
	)
	err := db.QueryRow(
		"SELECT "+parentCol+", type, body, created_at, updated_at FROM "+table+" WHERE id = ?", id,
	).Scan(&got.parentID, &got.typ, &got.body, &got.createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return storedComment{}, false
	}
	if err != nil {
		t.Fatalf("reading %s row %d: %v", table, id, err)
	}
	if updatedAt.Valid {
		v := updatedAt.String
		got.updatedAt = &v
	}
	return got, true
}

// countAuditEntries counts the audit rows carrying one operation against one
// entity. Comment operations are recorded against the PARENT, so entityID is the
// task's or the sprint's id, never the comment's.
func countAuditEntries(t *testing.T, db *DB, op models.AuditOperation, entityType models.EntityType, entityID int) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit WHERE operation = ? AND entity_type = ? AND entity_id = ?`,
		string(op), string(entityType), entityID,
	).Scan(&n); err != nil {
		t.Fatalf("counting %s audit entries: %v", op, err)
	}
	return n
}

// addTaskComment writes one task comment the way the command layer will: the
// insert and its audit entry in a single transaction (SPEC/DATABASE.md §
// Transactional Atomicity Guarantees #7).
func addTaskComment(t *testing.T, db *DB, taskID int, commentType models.CommentType, body, createdAt string) int {
	t.Helper()
	var id int
	err := db.WithTransaction(func(tx *sql.Tx) error {
		var err error
		id, err = InsertTaskCommentTx(tx, &models.TaskComment{
			TaskID:    taskID,
			Type:      commentType,
			Body:      body,
			CreatedAt: createdAt,
		})
		if err != nil {
			return err
		}
		return LogAuditTx(tx, models.OpTaskCommentCreate, models.EntityTask, taskID, createdAt)
	})
	if err != nil {
		t.Fatalf("adding task comment: %v", err)
	}
	return id
}

// addSprintComment is the sprint form of addTaskComment.
func addSprintComment(t *testing.T, db *DB, sprintID int, commentType models.CommentType, body, createdAt string) int {
	t.Helper()
	var id int
	err := db.WithTransaction(func(tx *sql.Tx) error {
		var err error
		id, err = InsertSprintCommentTx(tx, &models.SprintComment{
			SprintID:  sprintID,
			Type:      commentType,
			Body:      body,
			CreatedAt: createdAt,
		})
		if err != nil {
			return err
		}
		return LogAuditTx(tx, models.OpSprintCommentCreate, models.EntitySprint, sprintID, createdAt)
	})
	if err != nil {
		t.Fatalf("adding sprint comment: %v", err)
	}
	return id
}

// ==================== CRITERION 1: round trip, both entities ====================

// TestTaskCommentRoundTrip walks insert -> get -> list -> update -> delete for a
// task comment and verifies each step by reading the row back out of
// task_comments with raw SQL.
func TestTaskCommentRoundTrip(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, _ := seedCommentParents(t, db)
	const created = "2026-08-17T09:12:44.000Z"
	const body = "The connection pool saturates at 64 concurrent readers; beyond that, latency doubles."

	id := addTaskComment(t, db, taskID, models.CommentFinding, body, created)
	if id <= 0 {
		t.Fatalf("InsertTaskCommentTx returned id %d, want a positive id", id)
	}

	// The stored row, read with raw SQL.
	stored, ok := readStoredComment(t, db, "task_comments", "task_id", id)
	if !ok {
		t.Fatalf("no task_comments row with id %d after the insert committed", id)
	}
	if stored.parentID != taskID {
		t.Errorf("stored task_id = %d, want %d", stored.parentID, taskID)
	}
	if stored.typ != string(models.CommentFinding) {
		t.Errorf("stored type = %q, want %q", stored.typ, models.CommentFinding)
	}
	if stored.body != body {
		t.Errorf("stored body = %q, want %q", stored.body, body)
	}
	if stored.createdAt != created {
		t.Errorf("stored created_at = %q, want %q", stored.createdAt, created)
	}
	if stored.updatedAt != nil {
		t.Errorf("stored updated_at = %q, want NULL on a comment that was never edited", *stored.updatedAt)
	}

	// The audit entry landed against the PARENT task, with the task's id.
	if n := countAuditEntries(t, db, models.OpTaskCommentCreate, models.EntityTask, taskID); n != 1 {
		t.Errorf("TASK_COMMENT_CREATE entries against task %d = %d, want 1", taskID, n)
	}

	// Get returns exactly what was stored.
	got, err := db.GetTaskComment(testContext(), id)
	if err != nil {
		t.Fatalf("GetTaskComment(%d): %v", id, err)
	}
	if got.ID != id || got.TaskID != taskID || got.Type != models.CommentFinding ||
		got.Body != body || got.CreatedAt != created || got.UpdatedAt != nil {
		t.Errorf("GetTaskComment = %+v, want id %d, task %d, FINDING, the stored body, %s and a nil updated_at",
			got, id, taskID, created)
	}

	// List returns the one comment.
	list, err := db.ListTaskComments(testContext(), taskID, nil)
	if err != nil {
		t.Fatalf("ListTaskComments: %v", err)
	}
	if len(list) != 1 || list[0].ID != id {
		t.Fatalf("ListTaskComments returned %d comments (%+v), want exactly comment %d", len(list), list, id)
	}

	// Update: both columns at once.
	const edited = "Re-measured with the pool at 128: latency is flat, so the bottleneck is the disk, not the pool."
	const editedAt = "2026-08-17T11:03:10.000Z"
	newType := models.CommentDecision
	newBody := edited
	err = db.WithTransaction(func(tx *sql.Tx) error {
		if err := UpdateTaskCommentTx(tx, id, &CommentUpdate{Type: &newType, Body: &newBody}, editedAt); err != nil {
			return err
		}
		return LogAuditTx(tx, models.OpTaskCommentUpdate, models.EntityTask, taskID, editedAt)
	})
	if err != nil {
		t.Fatalf("UpdateTaskCommentTx: %v", err)
	}

	stored, ok = readStoredComment(t, db, "task_comments", "task_id", id)
	if !ok {
		t.Fatal("the comment disappeared after the update")
	}
	if stored.typ != string(models.CommentDecision) || stored.body != edited {
		t.Errorf("stored type/body after the edit = %q/%q, want %q and the edited body",
			stored.typ, stored.body, models.CommentDecision)
	}
	if stored.updatedAt == nil || *stored.updatedAt != editedAt {
		t.Errorf("stored updated_at after the edit = %v, want %q", stored.updatedAt, editedAt)
	}
	if stored.createdAt != created {
		t.Errorf("the edit changed created_at to %q; it must stay %q", stored.createdAt, created)
	}
	if n := countAuditEntries(t, db, models.OpTaskCommentUpdate, models.EntityTask, taskID); n != 1 {
		t.Errorf("TASK_COMMENT_UPDATE entries against task %d = %d, want 1", taskID, n)
	}

	// Delete: the row is gone, the audit trail is not.
	err = db.WithTransaction(func(tx *sql.Tx) error {
		if err := DeleteTaskCommentTx(tx, id); err != nil {
			return err
		}
		return LogAuditTx(tx, models.OpTaskCommentDelete, models.EntityTask, taskID, "2026-08-17T12:00:00.000Z")
	})
	if err != nil {
		t.Fatalf("DeleteTaskCommentTx: %v", err)
	}
	if _, ok := readStoredComment(t, db, "task_comments", "task_id", id); ok {
		t.Errorf("task_comments row %d still exists after the delete committed", id)
	}
	if n := countAuditEntries(t, db, models.OpTaskCommentDelete, models.EntityTask, taskID); n != 1 {
		t.Errorf("TASK_COMMENT_DELETE entries against task %d = %d, want 1: the audit entry outlives the row", taskID, n)
	}

	// The listing of a task whose only comment was removed is empty, not nil.
	list, err = db.ListTaskComments(testContext(), taskID, nil)
	if err != nil {
		t.Fatalf("ListTaskComments after the delete: %v", err)
	}
	if list == nil {
		t.Error("ListTaskComments returned a nil slice; an empty log is an empty slice")
	}
	if len(list) != 0 {
		t.Errorf("ListTaskComments after the delete returned %d comments, want 0", len(list))
	}
}

// TestSprintCommentRoundTrip is the sprint form of TestTaskCommentRoundTrip. The
// two tables are independent (SPEC/DATABASE.md § sprint_comments Table, "Two
// tables, deliberately"), so the sprint path is proven on its own table, its own
// audit operations and its own four-value type set.
func TestSprintCommentRoundTrip(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	_, sprintID := seedCommentParents(t, db)
	const created = "2026-08-17T09:30:00.000Z"
	const body = "Four of the six planned tasks are closed; the storage task slipped to the next sprint."

	id := addSprintComment(t, db, sprintID, models.CommentProgress, body, created)

	stored, ok := readStoredComment(t, db, "sprint_comments", "sprint_id", id)
	if !ok {
		t.Fatalf("no sprint_comments row with id %d after the insert committed", id)
	}
	if stored.parentID != sprintID || stored.typ != string(models.CommentProgress) ||
		stored.body != body || stored.createdAt != created || stored.updatedAt != nil {
		t.Errorf("stored sprint comment = %+v, want sprint %d, PROGRESS, the stored body, %s and a NULL updated_at",
			stored, sprintID, created)
	}
	if n := countAuditEntries(t, db, models.OpSprintCommentCreate, models.EntitySprint, sprintID); n != 1 {
		t.Errorf("SPRINT_COMMENT_CREATE entries against sprint %d = %d, want 1", sprintID, n)
	}

	got, err := db.GetSprintComment(testContext(), id)
	if err != nil {
		t.Fatalf("GetSprintComment(%d): %v", id, err)
	}
	if got.ID != id || got.SprintID != sprintID || got.Type != models.CommentProgress || got.Body != body {
		t.Errorf("GetSprintComment = %+v, want id %d on sprint %d with PROGRESS and the stored body", got, id, sprintID)
	}

	// Type-only edit: the body must survive it untouched.
	newType := models.CommentDecision
	const editedAt = "2026-08-17T14:20:00.000Z"
	err = db.WithTransaction(func(tx *sql.Tx) error {
		if err := UpdateSprintCommentTx(tx, id, &CommentUpdate{Type: &newType}, editedAt); err != nil {
			return err
		}
		return LogAuditTx(tx, models.OpSprintCommentUpdate, models.EntitySprint, sprintID, editedAt)
	})
	if err != nil {
		t.Fatalf("UpdateSprintCommentTx: %v", err)
	}

	stored, _ = readStoredComment(t, db, "sprint_comments", "sprint_id", id)
	if stored.typ != string(models.CommentDecision) {
		t.Errorf("stored type after the type-only edit = %q, want DECISION", stored.typ)
	}
	if stored.body != body {
		t.Errorf("the type-only edit rewrote the body to %q; it must stay %q", stored.body, body)
	}
	if stored.updatedAt == nil || *stored.updatedAt != editedAt {
		t.Errorf("stored updated_at = %v, want %q: updated_at is stamped on every edit, whichever columns it touches",
			stored.updatedAt, editedAt)
	}

	err = db.WithTransaction(func(tx *sql.Tx) error {
		if err := DeleteSprintCommentTx(tx, id); err != nil {
			return err
		}
		return LogAuditTx(tx, models.OpSprintCommentDelete, models.EntitySprint, sprintID, "2026-08-17T15:00:00.000Z")
	})
	if err != nil {
		t.Fatalf("DeleteSprintCommentTx: %v", err)
	}
	if _, ok := readStoredComment(t, db, "sprint_comments", "sprint_id", id); ok {
		t.Errorf("sprint_comments row %d still exists after the delete committed", id)
	}
}

// TestCommentIDSpacesAreIndependentInTheQueryLayer proves the query layer honours
// the per-table id spaces: an id that exists in sprint_comments is not found in
// task_comments and vice versa, so `task comment-edit 7` and `sprint comment-edit
// 7` can never address each other's row (SPEC/COMMANDS.md § Task Comments).
func TestCommentIDSpacesAreIndependentInTheQueryLayer(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, sprintID := seedCommentParents(t, db)
	taskCommentID := addTaskComment(t, db, taskID, models.CommentNote,
		"Re-measure once the storage tier is upgraded.", "2026-08-17T09:00:00.000Z")
	sprintCommentID := addSprintComment(t, db, sprintID, models.CommentUpdate,
		"The sprint goal now excludes the storage upgrade.", "2026-08-17T09:00:00.000Z")

	if taskCommentID != sprintCommentID {
		t.Fatalf("fixture assumption broken: the first id of each table should be the same value, got %d and %d",
			taskCommentID, sprintCommentID)
	}

	if _, err := db.GetSprintComment(testContext(), sprintCommentID); err != nil {
		t.Fatalf("GetSprintComment(%d): %v", sprintCommentID, err)
	}

	// Deleting the task comment must leave the sprint comment with the same id
	// alone, and the id must then be not-found in task_comments only.
	if err := db.WithTransaction(func(tx *sql.Tx) error {
		return DeleteTaskCommentTx(tx, taskCommentID)
	}); err != nil {
		t.Fatalf("DeleteTaskCommentTx: %v", err)
	}
	if _, err := db.GetSprintComment(testContext(), sprintCommentID); err != nil {
		t.Errorf("deleting task comment %d also removed sprint comment %d: %v",
			taskCommentID, sprintCommentID, err)
	}
	if _, err := db.GetTaskComment(testContext(), taskCommentID); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("GetTaskComment(%d) after the delete = %v, want utils.ErrNotFound", taskCommentID, err)
	}
}

// ==================== CRITERION 2: order and type filter ====================

// TestCommentListingIsOldestFirst proves the listing is a chronology: created_at
// ascending, with the comment id ascending as the tie-breaker for comments that
// share a created_at. Comments are inserted out of order so a listing that
// happened to return insertion order would fail.
func TestCommentListingIsOldestFirst(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, sprintID := seedCommentParents(t, db)

	const (
		earliest  = "2026-08-15T08:00:00.000Z"
		middle    = "2026-08-16T08:00:00.000Z"
		latest    = "2026-08-17T08:00:00.000Z"
		sameMilli = "2026-08-18T08:00:00.000Z"
	)

	// Inserted latest-first, so the ids run counter to the chronology.
	third := addTaskComment(t, db, taskID, models.CommentDecision, "Third: the pool size stays at 64.", latest)
	first := addTaskComment(t, db, taskID, models.CommentFinding, "First: the pool saturates at 64 readers.", earliest)
	second := addTaskComment(t, db, taskID, models.CommentTest, "Second: the load test reproduces it in 40 seconds.", middle)
	// Two comments written within the same millisecond: only the id can order them.
	tieFirst := addTaskComment(t, db, taskID, models.CommentProgress, "Fourth: the fix is on the branch.", sameMilli)
	tieSecond := addTaskComment(t, db, taskID, models.CommentProgress, "Fifth: the fix is merged.", sameMilli)

	comments, err := db.ListTaskComments(testContext(), taskID, nil)
	if err != nil {
		t.Fatalf("ListTaskComments: %v", err)
	}
	wantOrder := []int{first, second, third, tieFirst, tieSecond}
	gotOrder := make([]int, 0, len(comments))
	for i := range comments {
		gotOrder = append(gotOrder, comments[i].ID)
	}
	if fmt.Sprint(gotOrder) != fmt.Sprint(wantOrder) {
		t.Errorf("listing order = %v, want %v (created_at ASC, id ASC)", gotOrder, wantOrder)
	}

	// The sprint listing carries the same contract on its own table.
	sprintSecond := addSprintComment(t, db, sprintID, models.CommentUpdate, "Second: the goal was narrowed.", latest)
	sprintFirst := addSprintComment(t, db, sprintID, models.CommentProgress, "First: two tasks are closed.", earliest)
	sprintComments, err := db.ListSprintComments(testContext(), sprintID, nil)
	if err != nil {
		t.Fatalf("ListSprintComments: %v", err)
	}
	if len(sprintComments) != 2 || sprintComments[0].ID != sprintFirst || sprintComments[1].ID != sprintSecond {
		t.Errorf("sprint listing = %+v, want %d then %d", sprintComments, sprintFirst, sprintSecond)
	}
}

// TestCommentListingTypeFilter proves the optional filter narrows the listing to
// one type, keeps the chronological order inside that type, and returns an EMPTY
// SLICE - never nil, and never an error - for a type no comment carries.
func TestCommentListingTypeFilter(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, sprintID := seedCommentParents(t, db)

	firstFinding := addTaskComment(t, db, taskID, models.CommentFinding,
		"The retry loop swallows the context cancellation.", "2026-08-15T08:00:00.000Z")
	addTaskComment(t, db, taskID, models.CommentTest,
		"A cancelled context now fails the new regression test.", "2026-08-15T09:00:00.000Z")
	secondFinding := addTaskComment(t, db, taskID, models.CommentFinding,
		"The same swallowing exists in the sprint close path.", "2026-08-15T10:00:00.000Z")

	finding := models.CommentFinding
	filtered, err := db.ListTaskComments(testContext(), taskID, &finding)
	if err != nil {
		t.Fatalf("ListTaskComments with a type filter: %v", err)
	}
	if len(filtered) != 2 || filtered[0].ID != firstFinding || filtered[1].ID != secondFinding {
		t.Errorf("FINDING filter returned %+v, want comments %d then %d", filtered, firstFinding, secondFinding)
	}
	for i := range filtered {
		if filtered[i].Type != models.CommentFinding {
			t.Errorf("the FINDING filter returned a %s comment", filtered[i].Type)
		}
	}

	// A valid type that no comment carries: empty, not nil, no error.
	note := models.CommentNote
	unmatched, err := db.ListTaskComments(testContext(), taskID, &note)
	if err != nil {
		t.Fatalf("an unmatched type filter must not be an error: %v", err)
	}
	if unmatched == nil {
		t.Error("an unmatched type filter returned a nil slice; it must return an empty slice")
	}
	if len(unmatched) != 0 {
		t.Errorf("an unmatched type filter returned %d comments, want 0", len(unmatched))
	}

	// A task with no comments at all: same contract.
	emptyTaskID := newTestTask(t, db, "Audit the retry loop of the sprint close path")
	none, err := db.ListTaskComments(testContext(), emptyTaskID, nil)
	if err != nil {
		t.Fatalf("listing the comments of a task that has none: %v", err)
	}
	if none == nil || len(none) != 0 {
		t.Errorf("a task with no comments returned %v, want an empty non-nil slice", none)
	}

	// The sprint listing filters on its own four-value set.
	addSprintComment(t, db, sprintID, models.CommentProgress, "Two tasks are closed.", "2026-08-15T08:00:00.000Z")
	addSprintComment(t, db, sprintID, models.CommentDecision, "The storage task moves to the next sprint.", "2026-08-15T09:00:00.000Z")
	decision := models.CommentDecision
	sprintFiltered, err := db.ListSprintComments(testContext(), sprintID, &decision)
	if err != nil {
		t.Fatalf("ListSprintComments with a type filter: %v", err)
	}
	if len(sprintFiltered) != 1 || sprintFiltered[0].Type != models.CommentDecision {
		t.Errorf("DECISION filter on the sprint listing returned %+v, want exactly one DECISION comment", sprintFiltered)
	}
}

// ==================== CRITERION 3 (shape): the grouped read ====================
//
// The single-statement proof lives in comments_stmtcount_test.go, which counts the
// statements the connection actually sends. This test covers what the grouped read
// returns.

// TestGroupedTaskCommentsRead proves the grouped read keys every comment by its
// parent, orders each parent's comments oldest first, leaves a parent with no
// comments out of the map (whose zero value ranges as empty), and returns an empty
// map for an empty id set.
func TestGroupedTaskCommentsRead(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	withComments := newTestTask(t, db, "Reconcile the settlement ledger against the acquirer report")
	alsoWithComments := newTestTask(t, db, "Alert on any settlement window that fails to balance")
	withoutComments := newTestTask(t, db, "Document the settlement reconciliation runbook")

	firstOfFirst := addTaskComment(t, db, withComments, models.CommentFinding,
		"Window 2026-08-14 is short by 0.02 EUR: a rounding difference on refunds.", "2026-08-15T08:00:00.000Z")
	secondOfFirst := addTaskComment(t, db, withComments, models.CommentDecision,
		"Refund rounding moves to banker's rounding, applied at the window boundary.", "2026-08-15T09:00:00.000Z")
	onlyOfSecond := addTaskComment(t, db, alsoWithComments, models.CommentProgress,
		"The alert fires on the staging ledger; production wiring is next.", "2026-08-16T08:00:00.000Z")

	grouped, err := db.ListTaskCommentsByTasks(testContext(),
		[]int{withoutComments, alsoWithComments, withComments})
	if err != nil {
		t.Fatalf("ListTaskCommentsByTasks: %v", err)
	}

	if len(grouped) != 2 {
		t.Errorf("the grouped read returned %d keys (%v), want 2: a parent with no comments is absent",
			len(grouped), grouped)
	}
	if _, present := grouped[withoutComments]; present {
		t.Errorf("task %d has no comments but is present in the map", withoutComments)
	}
	// The absent key still behaves as an empty result for the caller.
	if len(grouped[withoutComments]) != 0 {
		t.Errorf("grouped[%d] = %+v, want an empty result", withoutComments, grouped[withoutComments])
	}

	first := grouped[withComments]
	if len(first) != 2 || first[0].ID != firstOfFirst || first[1].ID != secondOfFirst {
		t.Errorf("grouped[%d] = %+v, want comments %d then %d (oldest first)",
			withComments, first, firstOfFirst, secondOfFirst)
	}
	for i := range first {
		if first[i].TaskID != withComments {
			t.Errorf("grouped[%d] contains a comment whose task_id is %d", withComments, first[i].TaskID)
		}
	}
	if second := grouped[alsoWithComments]; len(second) != 1 || second[0].ID != onlyOfSecond {
		t.Errorf("grouped[%d] = %+v, want exactly comment %d", alsoWithComments, second, onlyOfSecond)
	}

	// An empty id set: an empty map, no error. (That it also issues no statement
	// is proven in comments_stmtcount_test.go.)
	empty, err := db.ListTaskCommentsByTasks(testContext(), nil)
	if err != nil {
		t.Fatalf("ListTaskCommentsByTasks with no ids: %v", err)
	}
	if empty == nil {
		t.Error("ListTaskCommentsByTasks returned a nil map for an empty id set; it must return an empty map")
	}
	if len(empty) != 0 {
		t.Errorf("ListTaskCommentsByTasks with no ids returned %d keys, want 0", len(empty))
	}

	// Duplicate ids are harmless: each row is still returned once.
	duplicated, err := db.ListTaskCommentsByTasks(testContext(),
		[]int{withComments, withComments, alsoWithComments})
	if err != nil {
		t.Fatalf("ListTaskCommentsByTasks with duplicate ids: %v", err)
	}
	if len(duplicated[withComments]) != 2 {
		t.Errorf("a duplicated id yielded %d comments, want 2", len(duplicated[withComments]))
	}
}

// TestGroupedTaskCommentsReadScalesBeyondThePlaceholderCache exercises the grouped
// read with more ids than the connection's placeholder cache pre-generates (1000),
// so both the cached and the generated placeholder paths are covered. Without this,
// a page rendering more than a thousand tasks would be the first thing to try the
// on-demand path.
func TestGroupedTaskCommentsReadScalesBeyondThePlaceholderCache(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, _ := seedCommentParents(t, db)
	addTaskComment(t, db, taskID, models.CommentFinding,
		"The grouped read is issued once per page, whatever the page holds.", "2026-08-17T07:00:00.000Z")

	for _, parents := range []int{1000, 1500} {
		ids := make([]int, 0, parents)
		ids = append(ids, taskID)
		// The remaining ids need not exist: the read is a lookup, and ids with no
		// comments are simply absent from the result.
		for candidate := taskID + 1; len(ids) < parents; candidate++ {
			ids = append(ids, candidate)
		}

		grouped, err := db.ListTaskCommentsByTasks(testContext(), ids)
		if err != nil {
			t.Fatalf("ListTaskCommentsByTasks over %d ids: %v", parents, err)
		}
		if len(grouped) != 1 || len(grouped[taskID]) != 1 {
			t.Errorf("the grouped read over %d ids returned %d keys, want exactly the one commented task",
				parents, len(grouped))
		}
	}
}

// ==================== CRITERION 4: updated_at ====================

// TestCommentUpdateStampsUpdatedAtAndPreservesCreatedAt covers all three UPDATE
// shapes SPEC/DATABASE.md § Update Comment specifies. Each is checked against the
// stored row: the requested columns changed, updated_at carries the edit's
// timestamp, and created_at is exactly the value the insert wrote.
func TestCommentUpdateStampsUpdatedAtAndPreservesCreatedAt(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, _ := seedCommentParents(t, db)
	const created = "2026-08-10T07:00:00.000Z"

	newType := models.CommentDecision
	newBody := "The index on (task_id, created_at) removes the sort step entirely."

	tests := []struct {
		name      string
		update    *CommentUpdate
		wantType  models.CommentType
		wantBody  string
		updatedAt string
	}{
		{
			name:      "type and body",
			update:    &CommentUpdate{Type: &newType, Body: &newBody},
			wantType:  models.CommentDecision,
			wantBody:  newBody,
			updatedAt: "2026-08-11T07:00:00.000Z",
		},
		{
			name:      "body only",
			update:    &CommentUpdate{Body: &newBody},
			wantType:  models.CommentFinding,
			wantBody:  newBody,
			updatedAt: "2026-08-12T07:00:00.000Z",
		},
		{
			name:      "type only",
			update:    &CommentUpdate{Type: &newType},
			wantType:  models.CommentDecision,
			wantBody:  "The listing query falls back to a full scan without the composite index.",
			updatedAt: "2026-08-13T07:00:00.000Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := addTaskComment(t, db, taskID, models.CommentFinding,
				"The listing query falls back to a full scan without the composite index.", created)

			if err := db.WithTransaction(func(tx *sql.Tx) error {
				return UpdateTaskCommentTx(tx, id, tt.update, tt.updatedAt)
			}); err != nil {
				t.Fatalf("UpdateTaskCommentTx: %v", err)
			}

			stored, ok := readStoredComment(t, db, "task_comments", "task_id", id)
			if !ok {
				t.Fatalf("comment %d disappeared", id)
			}
			if stored.typ != string(tt.wantType) {
				t.Errorf("stored type = %q, want %q", stored.typ, tt.wantType)
			}
			if stored.body != tt.wantBody {
				t.Errorf("stored body = %q, want %q", stored.body, tt.wantBody)
			}
			if stored.updatedAt == nil || *stored.updatedAt != tt.updatedAt {
				t.Errorf("stored updated_at = %v, want %q", stored.updatedAt, tt.updatedAt)
			}
			if stored.createdAt != created {
				t.Errorf("stored created_at = %q, want %q: an edit never touches created_at", stored.createdAt, created)
			}
		})
	}
}

// TestCommentReadsCarryUpdatedAtWithoutAliasing proves the read path materialises
// updated_at correctly for a MULTI-ROW result. The scan reuses one row struct
// across iterations, so a converter that handed out the address of that struct's
// field would make every comment in the listing share one string and report the
// LAST row's timestamp - the exact hazard scanTasksWithDeps documents for the task
// projection. Each edited comment therefore gets a distinct timestamp, and both the
// values and the pointer identities are checked.
func TestCommentReadsCarryUpdatedAtWithoutAliasing(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, _ := seedCommentParents(t, db)
	editedAt := map[int]string{}

	for i := range 3 {
		created := fmt.Sprintf("2026-08-1%dT07:00:00.000Z", i)
		id := addTaskComment(t, db, taskID, models.CommentFinding,
			fmt.Sprintf("Finding %d: the residual appears only on refunded windows.", i+1), created)

		stamp := fmt.Sprintf("2026-08-2%dT07:00:00.000Z", i)
		newBody := fmt.Sprintf("Finding %d, revised: the residual is a rounding difference on refunds.", i+1)
		if err := db.WithTransaction(func(tx *sql.Tx) error {
			return UpdateTaskCommentTx(tx, id, &CommentUpdate{Body: &newBody}, stamp)
		}); err != nil {
			t.Fatalf("editing comment %d: %v", id, err)
		}
		editedAt[id] = stamp
	}
	// One comment that was never edited: its updated_at must stay nil even though
	// it is scanned through the same reused row as the edited ones.
	pristine := addTaskComment(t, db, taskID, models.CommentNote,
		"Leave this one untouched.", "2026-08-14T07:00:00.000Z")

	comments, err := db.ListTaskComments(testContext(), taskID, nil)
	if err != nil {
		t.Fatalf("ListTaskComments: %v", err)
	}
	if len(comments) != 4 {
		t.Fatalf("the listing returned %d comments, want 4", len(comments))
	}

	seen := make(map[*string]bool, len(comments))
	for i := range comments {
		c := comments[i]
		if c.ID == pristine {
			if c.UpdatedAt != nil {
				t.Errorf("the unedited comment reports updated_at = %q, want nil", *c.UpdatedAt)
			}
			continue
		}
		if c.UpdatedAt == nil {
			t.Errorf("comment %d was edited but reports a nil updated_at", c.ID)
			continue
		}
		if *c.UpdatedAt != editedAt[c.ID] {
			t.Errorf("comment %d reports updated_at = %q, want %q: the rows are sharing one string",
				c.ID, *c.UpdatedAt, editedAt[c.ID])
		}
		if seen[c.UpdatedAt] {
			t.Errorf("comment %d shares its updated_at storage with another row", c.ID)
		}
		seen[c.UpdatedAt] = true
	}

	// The grouped read materialises the same column through the same converter.
	grouped, err := db.ListTaskCommentsByTasks(testContext(), []int{taskID})
	if err != nil {
		t.Fatalf("ListTaskCommentsByTasks: %v", err)
	}
	for _, c := range grouped[taskID] {
		if c.ID == pristine {
			continue
		}
		if c.UpdatedAt == nil || *c.UpdatedAt != editedAt[c.ID] {
			t.Errorf("the grouped read reports updated_at = %v for comment %d, want %q",
				c.UpdatedAt, c.ID, editedAt[c.ID])
		}
	}

	// And the by-id read.
	for id, stamp := range editedAt {
		got, err := db.GetTaskComment(testContext(), id)
		if err != nil {
			t.Fatalf("GetTaskComment(%d): %v", id, err)
		}
		if got.UpdatedAt == nil || *got.UpdatedAt != stamp {
			t.Errorf("GetTaskComment(%d) reports updated_at = %v, want %q", id, got.UpdatedAt, stamp)
		}
	}
}

// TestCommentUpdateWithNoChangeIsRejected proves an edit that requests nothing is
// refused rather than silently stamping updated_at. `comment-edit` requires at
// least one change (SPEC/COMMANDS.md § Edit Task Comment, "No-op is not
// accepted"), so the persistence layer must not accept one either.
func TestCommentUpdateWithNoChangeIsRejected(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, sprintID := seedCommentParents(t, db)
	taskCommentID := addTaskComment(t, db, taskID, models.CommentFinding,
		"The retry loop swallows the context cancellation.", "2026-08-10T07:00:00.000Z")
	sprintCommentID := addSprintComment(t, db, sprintID, models.CommentProgress,
		"Two tasks are closed.", "2026-08-10T07:00:00.000Z")

	for _, tt := range []struct {
		name   string
		update *CommentUpdate
	}{
		{"nil update", nil},
		{"both fields unset", &CommentUpdate{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := db.WithTransaction(func(tx *sql.Tx) error {
				return UpdateTaskCommentTx(tx, taskCommentID, tt.update, "2026-08-11T07:00:00.000Z")
			})
			if !errors.Is(err, utils.ErrInvalidUpdate) {
				t.Errorf("UpdateTaskCommentTx with no change = %v, want utils.ErrInvalidUpdate", err)
			}
			err = db.WithTransaction(func(tx *sql.Tx) error {
				return UpdateSprintCommentTx(tx, sprintCommentID, tt.update, "2026-08-11T07:00:00.000Z")
			})
			if !errors.Is(err, utils.ErrInvalidUpdate) {
				t.Errorf("UpdateSprintCommentTx with no change = %v, want utils.ErrInvalidUpdate", err)
			}
		})
	}

	// Nothing was stamped.
	stored, _ := readStoredComment(t, db, "task_comments", "task_id", taskCommentID)
	if stored.updatedAt != nil {
		t.Errorf("a rejected edit stamped updated_at = %q", *stored.updatedAt)
	}
}

// TestCommentMutationOnMissingIDIsNotFound proves an UPDATE or DELETE that matches
// no row is reported as a not-found condition. Both statements succeed at the SQL
// level while touching nothing, so without the affected-row check a `comment-edit`
// on an unknown id would report success.
func TestCommentMutationOnMissingIDIsNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	seedCommentParents(t, db)
	const missing = 4242
	newBody := "This comment does not exist."

	cases := map[string]func(tx *sql.Tx) error{
		"update task comment": func(tx *sql.Tx) error {
			return UpdateTaskCommentTx(tx, missing, &CommentUpdate{Body: &newBody}, "2026-08-17T07:00:00.000Z")
		},
		"update sprint comment": func(tx *sql.Tx) error {
			return UpdateSprintCommentTx(tx, missing, &CommentUpdate{Body: &newBody}, "2026-08-17T07:00:00.000Z")
		},
		"delete task comment":   func(tx *sql.Tx) error { return DeleteTaskCommentTx(tx, missing) },
		"delete sprint comment": func(tx *sql.Tx) error { return DeleteSprintCommentTx(tx, missing) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			err := db.WithTransaction(mutate)
			if !errors.Is(err, utils.ErrNotFound) {
				t.Errorf("%s on a missing id = %v, want utils.ErrNotFound", name, err)
			}
		})
	}

	// The by-id reads report the same condition.
	if _, err := db.GetTaskComment(testContext(), missing); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("GetTaskComment on a missing id = %v, want utils.ErrNotFound", err)
	}
	if _, err := db.GetSprintComment(testContext(), missing); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("GetSprintComment on a missing id = %v, want utils.ErrNotFound", err)
	}
}

// ==================== CRITERION 5: atomicity ====================

// TestFailedCommentMutationLeavesNoRowAndNoAuditEntry is the gate for acceptance
// criterion 5. Three ways a comment transaction can fail are covered, and in every
// one both the comment row and the audit entry must be absent afterwards: a
// committed comment change can never exist without its audit record, and an audit
// record can never exist for a change that was rolled back (SPEC/DATABASE.md §
// Transactional Atomicity Guarantees #7).
func TestFailedCommentMutationLeavesNoRowAndNoAuditEntry(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, sprintID := seedCommentParents(t, db)
	const now = "2026-08-17T07:00:00.000Z"

	countRows := func(table, parentCol string, parentID int) int {
		t.Helper()
		var n int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM "+table+" WHERE "+parentCol+" = ?", parentID).Scan(&n); err != nil {
			t.Fatalf("counting %s rows: %v", table, err)
		}
		return n
	}

	t.Run("the insert succeeds and a later step of the same transaction fails", func(t *testing.T) {
		err := db.WithTransaction(func(tx *sql.Tx) error {
			id, err := InsertTaskCommentTx(tx, &models.TaskComment{
				TaskID:    taskID,
				Type:      models.CommentFinding,
				Body:      "The audit entry and the comment must land together or not at all.",
				CreatedAt: now,
			})
			if err != nil {
				return err
			}
			if id <= 0 {
				t.Errorf("the insert returned id %d inside the transaction", id)
			}
			if err := LogAuditTx(tx, models.OpTaskCommentCreate, models.EntityTask, taskID, now); err != nil {
				return err
			}
			// Whatever fails after the write - a validation the command layer
			// performs inside the transaction, a second statement, a cancelled
			// context - must take the whole thing with it.
			return errIntentional
		})
		if !errors.Is(err, errIntentional) {
			t.Fatalf("WithTransaction = %v, want the intentional error", err)
		}
		if n := countRows("task_comments", "task_id", taskID); n != 0 {
			t.Errorf("task_comments holds %d rows after the rollback, want 0: no partial row may survive", n)
		}
		if n := countAuditEntries(t, db, models.OpTaskCommentCreate, models.EntityTask, taskID); n != 0 {
			t.Errorf("the audit log holds %d TASK_COMMENT_CREATE entries after the rollback, want 0", n)
		}
	})

	t.Run("the audit entry is written first and the insert is rejected", func(t *testing.T) {
		err := db.WithTransaction(func(tx *sql.Tx) error {
			if err := LogAuditTx(tx, models.OpSprintCommentCreate, models.EntitySprint, sprintID, now); err != nil {
				return err
			}
			// HYPOTHESIS is a task-only type: the sprint_comments CHECK rejects it.
			_, err := InsertSprintCommentTx(tx, &models.SprintComment{
				SprintID:  sprintID,
				Type:      models.CommentHypothesis,
				Body:      "A sprint comment cannot carry a task-only type.",
				CreatedAt: now,
			})
			return err
		})
		if !errors.Is(err, utils.ErrValidation) {
			t.Fatalf("inserting a task-only type on a sprint comment = %v, want utils.ErrValidation", err)
		}
		if n := countRows("sprint_comments", "sprint_id", sprintID); n != 0 {
			t.Errorf("sprint_comments holds %d rows after the rejected insert, want 0", n)
		}
		if n := countAuditEntries(t, db, models.OpSprintCommentCreate, models.EntitySprint, sprintID); n != 0 {
			t.Errorf("the audit log holds %d SPRINT_COMMENT_CREATE entries after the rollback, want 0: "+
				"an audit record may never outlive a rolled-back change", n)
		}
	})

	t.Run("the parent disappears before the insert", func(t *testing.T) {
		doomedID := newTestTask(t, db, "Delete the stale reconciliation windows")
		if _, err := db.Exec("DELETE FROM tasks WHERE id = ?", doomedID); err != nil {
			t.Fatalf("deleting the parent task: %v", err)
		}

		err := db.WithTransaction(func(tx *sql.Tx) error {
			if err := LogAuditTx(tx, models.OpTaskCommentCreate, models.EntityTask, doomedID, now); err != nil {
				return err
			}
			_, err := InsertTaskCommentTx(tx, &models.TaskComment{
				TaskID:    doomedID,
				Type:      models.CommentFinding,
				Body:      "The parent was deleted between the existence check and this write.",
				CreatedAt: now,
			})
			return err
		})
		if !errors.Is(err, utils.ErrNotFound) {
			t.Fatalf("inserting against a deleted parent = %v, want utils.ErrNotFound", err)
		}
		if n := countAuditEntries(t, db, models.OpTaskCommentCreate, models.EntityTask, doomedID); n != 0 {
			t.Errorf("the audit log holds %d entries for the failed insert, want 0", n)
		}
	})
}

// ==================== error classification ====================

// TestCommentWriteErrorClassification pins the mapping of the three SQLite
// constraint failures a comment write can produce. IsUniqueConstraintErr answers
// true only for 2067/1555, so none of these may be routed through it: a comment
// table has no uniqueness constraint, and reporting any of them as an "already in
// use" collision (exit code 5) would be wrong.
func TestCommentWriteErrorClassification(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, sprintID := seedCommentParents(t, db)
	const now = "2026-08-17T07:00:00.000Z"
	oversized := strings.Repeat("m", models.MaxCommentBody+1)

	tests := []struct {
		name         string
		mutate       func(tx *sql.Tx) error
		wantSentinel error
		wantMessage  string
	}{
		{
			name: "foreign key (787): the parent task does not exist",
			mutate: func(tx *sql.Tx) error {
				_, err := InsertTaskCommentTx(tx, &models.TaskComment{
					TaskID: 987654, Type: models.CommentFinding,
					Body: "This parent was never created.", CreatedAt: now,
				})
				return err
			},
			wantSentinel: utils.ErrNotFound,
			wantMessage:  "task 987654 not found",
		},
		{
			name: "foreign key (787): the parent sprint does not exist",
			mutate: func(tx *sql.Tx) error {
				_, err := InsertSprintCommentTx(tx, &models.SprintComment{
					SprintID: 987654, Type: models.CommentProgress,
					Body: "This parent was never created.", CreatedAt: now,
				})
				return err
			},
			wantSentinel: utils.ErrNotFound,
			wantMessage:  "sprint 987654 not found",
		},
		{
			name: "check (275): a type outside the enum on a task comment",
			mutate: func(tx *sql.Tx) error {
				_, err := InsertTaskCommentTx(tx, &models.TaskComment{
					TaskID: taskID, Type: models.CommentType("BLOCKER"),
					Body: "BLOCKER is not a comment type.", CreatedAt: now,
				})
				return err
			},
			wantSentinel: utils.ErrValidation,
			wantMessage:  `invalid comment type "BLOCKER" for a task comment`,
		},
		{
			name: "check (275): a task-only type on a sprint comment",
			mutate: func(tx *sql.Tx) error {
				_, err := InsertSprintCommentTx(tx, &models.SprintComment{
					SprintID: sprintID, Type: models.CommentTest,
					Body: "TEST is valid on a task comment and invalid here.", CreatedAt: now,
				})
				return err
			},
			wantSentinel: utils.ErrValidation,
			wantMessage:  `invalid comment type "TEST" for a sprint comment`,
		},
		{
			name: "check (275): a body over the 4096-character cap",
			mutate: func(tx *sql.Tx) error {
				_, err := InsertTaskCommentTx(tx, &models.TaskComment{
					TaskID: taskID, Type: models.CommentNote,
					Body: oversized, CreatedAt: now,
				})
				return err
			},
			wantSentinel: utils.ErrFieldTooLarge,
			wantMessage:  "body exceeds maximum length of 4096 characters",
		},
		{
			name: "check (275): an oversized body on an edit",
			mutate: func(tx *sql.Tx) error {
				id := addTaskComment(t, db, taskID, models.CommentNote, "A body worth replacing.", now)
				return UpdateTaskCommentTx(tx, id, &CommentUpdate{Body: &oversized}, now)
			},
			wantSentinel: utils.ErrFieldTooLarge,
			wantMessage:  "body exceeds maximum length of 4096 characters",
		},
		{
			name: "check (275): an invalid type on an edit",
			mutate: func(tx *sql.Tx) error {
				id := addTaskComment(t, db, taskID, models.CommentNote, "A type worth replacing.", now)
				bad := models.CommentType("REVIEW")
				return UpdateTaskCommentTx(tx, id, &CommentUpdate{Type: &bad}, now)
			},
			wantSentinel: utils.ErrValidation,
			wantMessage:  `invalid comment type "REVIEW" for a task comment`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.WithTransaction(tt.mutate)
			if err == nil {
				t.Fatal("the write was expected to fail, but it succeeded")
			}
			if !errors.Is(err, tt.wantSentinel) {
				t.Errorf("error = %v, want it to chain %v", err, tt.wantSentinel)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("error message = %q, want it to contain %q", err.Error(), tt.wantMessage)
			}
			if IsUniqueConstraintErr(err) {
				t.Errorf("IsUniqueConstraintErr answered true for %v; a comment table has no "+
					"uniqueness constraint, so this would surface as exit code 5 with the wrong message", err)
			}
		})
	}
}

// TestCommentWriteErrorNotNullMapping covers the NOT NULL (1299) branch of the
// classifier. It is unreachable through the typed API - the parent id is an int and
// the body and created_at are strings, so no column can arrive NULL - which is
// exactly why it is asserted directly rather than through a write: an unreachable
// branch that is never exercised is an unverified branch.
func TestCommentWriteErrorNotNullMapping(t *testing.T) {
	err := taskCommentStmts.writeError(&mockSQLiteErr{code: sqliteConstraintNotNull}, 7, nil, nil)
	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf("a NOT NULL violation maps to %v, want utils.ErrValidation", err)
	}
	if IsUniqueConstraintErr(err) {
		t.Error("a NOT NULL violation must never be reported as a uniqueness collision")
	}

	// A code the classifier does not special-case is wrapped, not swallowed, and
	// stays distinguishable from a validation failure.
	other := &mockSQLiteErr{code: sqliteBusy}
	wrapped := sprintCommentStmts.writeError(other, 7, nil, nil)
	if !errors.Is(wrapped, other) {
		t.Errorf("an unclassified failure = %v, want it to wrap the driver error", wrapped)
	}
	if errors.Is(wrapped, utils.ErrValidation) || errors.Is(wrapped, utils.ErrNotFound) {
		t.Errorf("an unclassified failure was mapped onto a domain sentinel: %v", wrapped)
	}
}

// TestCommentBodyIsStoredTrimmed proves the stored body is the value the domain
// layer validates: NormalizeCommentBody's output. Storing the untrimmed text after
// validating the trimmed one would let a body whose trimmed form fits the
// 4096-character cap fail the schema CHECK, surfacing as an opaque database error.
func TestCommentBodyIsStoredTrimmed(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, _ := seedCommentParents(t, db)
	const trimmed = "The pool saturates at 64 readers.\nRe-measure after the storage upgrade."
	padded := "\n\t  " + trimmed + "   \n\n"

	// A body whose trimmed form is exactly at the cap, but whose raw form is over
	// it: the insert must store the trimmed form and succeed.
	atCap := strings.Repeat("ç", models.MaxCommentBody)
	paddedAtCap := "  " + atCap + "  "

	insertID := addTaskComment(t, db, taskID, models.CommentFinding, padded, "2026-08-17T07:00:00.000Z")
	stored, _ := readStoredComment(t, db, "task_comments", "task_id", insertID)
	if stored.body != trimmed {
		t.Errorf("stored body = %q, want the trimmed form %q", stored.body, trimmed)
	}

	capID := addTaskComment(t, db, taskID, models.CommentNote, paddedAtCap, "2026-08-17T07:30:00.000Z")
	stored, _ = readStoredComment(t, db, "task_comments", "task_id", capID)
	if stored.body != atCap {
		t.Errorf("a body at the cap with surrounding whitespace was stored as %d characters, want %d",
			len([]rune(stored.body)), models.MaxCommentBody)
	}

	// The same rule on an edit.
	newBody := padded
	if err := db.WithTransaction(func(tx *sql.Tx) error {
		return UpdateTaskCommentTx(tx, capID, &CommentUpdate{Body: &newBody}, "2026-08-17T08:00:00.000Z")
	}); err != nil {
		t.Fatalf("UpdateTaskCommentTx: %v", err)
	}
	stored, _ = readStoredComment(t, db, "task_comments", "task_id", capID)
	if stored.body != trimmed {
		t.Errorf("stored body after the edit = %q, want the trimmed form %q", stored.body, trimmed)
	}
}

// TestCommentReadsWorkOnAReadOnlyConnection proves the comment reads are usable
// from the path the web interface opens (OpenReadOnly), which is what rmp task #166
// will load its pages through, and that the same connection rejects a comment
// WRITE. OpenReadOnly runs no migrations and opens every connection with
// query_only, so this covers both halves of the contract the web relies on:
// the reads work, and no read path can turn into a write path.
func TestCommentReadsWorkOnAReadOnlyConnection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmapName = "settlement-reconciliation"
	writable, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("creating roadmap %q: %v", roadmapName, err)
	}

	taskID, sprintID := seedCommentParents(t, writable)
	taskCommentID := addTaskComment(t, writable, taskID, models.CommentFinding,
		"Window 2026-08-14 is short by 0.02 EUR: a rounding difference on refunds.", "2026-08-17T07:00:00.000Z")
	addSprintComment(t, writable, sprintID, models.CommentProgress,
		"Four of the six planned tasks are closed.", "2026-08-17T07:30:00.000Z")
	if err := writable.Close(); err != nil {
		t.Fatalf("closing the writable connection: %v", err)
	}

	readOnly, err := OpenReadOnly(roadmapName)
	if err != nil {
		t.Fatalf("OpenReadOnly(%q): %v", roadmapName, err)
	}
	defer readOnly.Close()

	if comments, err := readOnly.ListTaskComments(testContext(), taskID, nil); err != nil {
		t.Errorf("ListTaskComments on a read-only connection: %v", err)
	} else if len(comments) != 1 {
		t.Errorf("the read-only listing returned %d comments, want 1", len(comments))
	}
	if comments, err := readOnly.ListSprintComments(testContext(), sprintID, nil); err != nil {
		t.Errorf("ListSprintComments on a read-only connection: %v", err)
	} else if len(comments) != 1 {
		t.Errorf("the read-only sprint listing returned %d comments, want 1", len(comments))
	}
	if grouped, err := readOnly.ListTaskCommentsByTasks(testContext(), []int{taskID}); err != nil {
		t.Errorf("ListTaskCommentsByTasks on a read-only connection: %v", err)
	} else if len(grouped[taskID]) != 1 {
		t.Errorf("the read-only grouped read returned %d comments for task %d, want 1",
			len(grouped[taskID]), taskID)
	}
	if _, err := readOnly.GetTaskComment(testContext(), taskCommentID); err != nil {
		t.Errorf("GetTaskComment on a read-only connection: %v", err)
	}

	// The same connection cannot write one.
	err = readOnly.WithTransaction(func(tx *sql.Tx) error {
		_, insertErr := InsertTaskCommentTx(tx, &models.TaskComment{
			TaskID: taskID, Type: models.CommentNote,
			Body: "The web interface must never be able to write this.", CreatedAt: "2026-08-17T08:00:00.000Z",
		})
		return insertErr
	})
	if err == nil {
		t.Error("a read-only connection accepted a comment insert; query_only must reject every write")
	}
}

// TestCommentsCascadeThroughTheQueryLayer proves the reads observe ON DELETE
// CASCADE: deleting a parent removes its comments, so a listing of a deleted
// parent's comments is empty rather than returning rows whose parent is gone.
func TestCommentsCascadeThroughTheQueryLayer(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, sprintID := seedCommentParents(t, db)
	taskCommentID := addTaskComment(t, db, taskID, models.CommentFinding,
		"Deleting the task must take this comment with it.", "2026-08-17T07:00:00.000Z")
	sprintCommentID := addSprintComment(t, db, sprintID, models.CommentProgress,
		"Deleting the sprint must take this comment with it.", "2026-08-17T07:00:00.000Z")

	if err := db.DeleteTask(testContext(), taskID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if err := db.DeleteSprint(testContext(), sprintID); err != nil {
		t.Fatalf("DeleteSprint: %v", err)
	}

	if _, err := db.GetTaskComment(testContext(), taskCommentID); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("the task comment survived its task: %v", err)
	}
	if _, err := db.GetSprintComment(testContext(), sprintCommentID); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("the sprint comment survived its sprint: %v", err)
	}

	list, err := db.ListTaskComments(testContext(), taskID, nil)
	if err != nil {
		t.Fatalf("ListTaskComments on a deleted task: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("the listing of a deleted task returned %d comments, want 0", len(list))
	}

	grouped, err := db.ListTaskCommentsByTasks(testContext(), []int{taskID})
	if err != nil {
		t.Fatalf("ListTaskCommentsByTasks on a deleted task: %v", err)
	}
	if len(grouped) != 0 {
		t.Errorf("the grouped read of a deleted task returned %d keys, want 0", len(grouped))
	}
}
