package db

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The propagation contract of the comment query layer (rmp task #319).
//
// SPEC/ARCHITECTURE.md § Propagation Rules gives internal/db three rows, in this
// order:
//
//	sql.ErrNoRows                -> utils.ErrNotFound       (exit code 4)
//	SQLite constraint violation  -> utils.ErrAlreadyExists  (exit code 5)
//	any other database/sql error -> utils.ErrDatabase       (exit code 1)
//
// Before this file existed, the comment layer implemented the first two and
// silently skipped the third: a dropped table reached the user as
// "Error: writing task comment: SQL logic error: no such table: task_comments (1)"
// with no sentinel at all. The exit code was still 1, but only because
// cmd/rmp/main.go falls back to 1 for an error it cannot classify -- an accident
// that would have produced the WRONG code for any class whose fallback differs.
//
// The two tests below are a matched pair and must be read together:
//
//   - TestCommentDatabaseFailuresCarryTheDatabaseSentinel proves the third row is
//     implemented, by provoking a GENUINE database failure (the tables are
//     dropped out from under the statements) rather than by injecting a fake
//     driver error.
//   - TestCommentClassificationSurvivesTheDatabaseSentinel proves the first two
//     rows still resolve ahead of it. It is the guard against the obvious wrong
//     fix: a blanket wrap applied BEFORE the classifier would turn every
//     not-found into a database error and every exit 4 into an exit 1, and would
//     leave this test red while the one above stayed green.

// openSentinelRoadmap opens a REAL roadmap database -- a file under a private
// HOME, opened through the same db.Open the CLI uses, with the production
// schema, pragmas and WAL settings -- and seeds one task and one sprint to hang
// comments off.
//
// A real file is used rather than the package's shared in-memory fixture on
// purpose: the failure these tests provoke is `DROP TABLE`, and the shared
// in-memory database (`file::memory:?cache=shared`) is one single database for
// the whole test binary, so dropping a table in it would be visible to every
// other test in the package.
func openSentinelRoadmap(t *testing.T) (database *DB, taskID, sprintID int) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	database, err := Open("comment_sentinel_probe")
	if err != nil {
		t.Fatalf("opening the probe roadmap: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	taskID, sprintID = seedCommentParents(t, database)
	return database, taskID, sprintID
}

// dropCommentTables removes both comment tables from an open database, which is
// the cheapest way to make every comment statement fail for a reason that is
// neither a missing row nor a constraint violation. SQLite reports it as
// SQLITE_ERROR (1), a code the write classifier deliberately does not
// special-case, so it takes exactly the fall-through path this file is about.
func dropCommentTables(t *testing.T, database *DB) {
	t.Helper()

	if _, err := database.Exec(`DROP TABLE task_comments`); err != nil {
		t.Fatalf("dropping task_comments: %v", err)
	}
	if _, err := database.Exec(`DROP TABLE sprint_comments`); err != nil {
		t.Fatalf("dropping sprint_comments: %v", err)
	}
}

// TestCommentDatabaseFailuresCarryTheDatabaseSentinel drives a genuine database
// failure through every comment path the CLI reaches and requires each returned
// error to satisfy BOTH halves of the rule:
//
//   - it chains utils.ErrDatabase, so cmd/rmp/main.go classifies it instead of
//     falling back, and the line the user reads starts "Error: database error: ";
//   - it still names the operation and the entity (SPEC/ARCHITECTURE.md
//     § Wrapping Rules, rule 2), so the sentinel is added IN FRONT of the
//     existing context and does not replace it.
func TestCommentDatabaseFailuresCarryTheDatabaseSentinel(t *testing.T) {
	database, taskID, sprintID := openSentinelRoadmap(t)
	ctx := testContext()
	const now = "2026-08-24T09:00:00.000Z"

	// Both comments are written while the tables still exist, so the by-id reads
	// below address ids that genuinely were rows: the failure under test is the
	// dropped table, never a missing row.
	taskCommentID := addTaskComment(t, database, taskID, models.CommentNote,
		"Reproduced against a database whose comment tables were dropped.", now)
	sprintCommentID := addSprintComment(t, database, sprintID, models.CommentProgress,
		"Reproduced against a database whose comment tables were dropped.", now)

	dropCommentTables(t, database)

	replacement := "A body the failing statement never gets to store."

	tests := []struct {
		run         func() error
		name        string
		wantContext string
	}{
		{
			name: "insert a task comment (writeError fall-through)",
			run: func() error {
				return database.WithTransaction(func(tx *sql.Tx) error {
					_, err := InsertTaskCommentTx(tx, &models.TaskComment{
						TaskID: taskID, Type: models.CommentFinding,
						Body: "The insert cannot reach a table that is gone.", CreatedAt: now,
					})
					return err
				})
			},
			wantContext: "writing task comment: ",
		},
		{
			name: "insert a sprint comment (writeError fall-through)",
			run: func() error {
				return database.WithTransaction(func(tx *sql.Tx) error {
					_, err := InsertSprintCommentTx(tx, &models.SprintComment{
						SprintID: sprintID, Type: models.CommentProgress,
						Body: "The insert cannot reach a table that is gone.", CreatedAt: now,
					})
					return err
				})
			},
			wantContext: "writing sprint comment: ",
		},
		{
			name: "edit a task comment (writeError fall-through)",
			run: func() error {
				return database.WithTransaction(func(tx *sql.Tx) error {
					return UpdateTaskCommentTx(tx, taskCommentID, &CommentUpdate{Body: &replacement}, now)
				})
			},
			wantContext: "writing task comment: ",
		},
		{
			name: "edit a sprint comment (writeError fall-through)",
			run: func() error {
				return database.WithTransaction(func(tx *sql.Tx) error {
					return UpdateSprintCommentTx(tx, sprintCommentID, &CommentUpdate{Body: &replacement}, now)
				})
			},
			wantContext: "writing sprint comment: ",
		},
		{
			name: "read one task comment by id",
			run: func() error {
				_, err := database.GetTaskComment(ctx, taskCommentID)
				return err
			},
			wantContext: "querying task comment " + strconv.Itoa(taskCommentID) + ": ",
		},
		{
			name: "read one sprint comment by id",
			run: func() error {
				_, err := database.GetSprintComment(ctx, sprintCommentID)
				return err
			},
			wantContext: "querying sprint comment " + strconv.Itoa(sprintCommentID) + ": ",
		},
		{
			name: "list a task's comments",
			run: func() error {
				_, err := database.ListTaskComments(ctx, taskID, nil)
				return err
			},
			wantContext: "querying task comments: ",
		},
		{
			name: "list a sprint's comments",
			run: func() error {
				_, err := database.ListSprintComments(ctx, sprintID, nil)
				return err
			},
			wantContext: "querying sprint comments: ",
		},
		{
			name: "list a task's comments through the type filter",
			run: func() error {
				filter := models.CommentNote
				_, err := database.ListTaskComments(ctx, taskID, &filter)
				return err
			},
			wantContext: "querying task comments: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("the statement was expected to fail against a dropped table, but it succeeded")
			}
			if !errors.Is(err, utils.ErrDatabase) {
				t.Errorf("error = %v, want it to chain utils.ErrDatabase so the line reads "+
					`"Error: database error: ..." and the exit code is classified rather than guessed`, err)
			}

			// The rendering, not just the chain: writeFailureReport prints
			// "Error: " + err.Error(), so the sentinel must be the FIRST thing
			// in the message for the published line to match.
			const prefix = "database error: "
			if !strings.HasPrefix(err.Error(), prefix) {
				t.Errorf("error message = %q, want it to begin %q", err.Error(), prefix)
			}

			// The context survives the wrap and sits immediately after the
			// sentinel: the sentinel is added in front, never in place of it.
			if !strings.Contains(err.Error(), prefix+tt.wantContext) {
				t.Errorf("error message = %q, want it to carry the operation context %q "+
					"directly after the sentinel", err.Error(), tt.wantContext)
			}

			// A database failure is not any of the classes above it.
			for _, wrong := range []error{utils.ErrNotFound, utils.ErrAlreadyExists, utils.ErrValidation, utils.ErrFieldTooLarge} {
				if errors.Is(err, wrong) {
					t.Errorf("a dropped table was classified as %v: %v", wrong, err)
				}
			}
		})
	}
}

// TestCommentClassificationSurvivesTheDatabaseSentinel is the guard on the two
// propagation rows that sit ABOVE utils.ErrDatabase. Every case here is a
// failure that must NOT be a database error, and every one of them travels
// through code the sentinel wrap sits next to:
//
//   - sql.ErrNoRows is ruled out immediately before the wrapped return in
//     getComment, so a wrap moved one line earlier would swallow it;
//   - the SQLite constraint codes are classified by the switch immediately
//     before the wrapped return in writeError, so a wrap moved above that switch
//     would swallow all of them;
//   - a UNIQUE collision must stay recognisable to IsUniqueConstraintErr, which
//     is what internal/commands turns into utils.ErrAlreadyExists (exit code 5);
//     wrapping it in a sentinel here would leave that translation unreachable.
//
// A blanket wrap of the kind this task warned against leaves the test above
// green and this one red, which is exactly the discrimination it exists for.
func TestCommentClassificationSurvivesTheDatabaseSentinel(t *testing.T) {
	database, taskID, sprintID := openSentinelRoadmap(t)
	ctx := testContext()
	const now = "2026-08-24T09:00:00.000Z"
	const missing = 88001

	oversized := strings.Repeat("s", models.MaxCommentBody+1)

	tests := []struct {
		run          func() error
		wantSentinel error
		name         string
		wantMessage  string
	}{
		{
			name: "sql.ErrNoRows on a task comment read stays not-found (exit 4)",
			run: func() error {
				_, err := database.GetTaskComment(ctx, missing)
				return err
			},
			wantSentinel: utils.ErrNotFound,
			wantMessage:  "task comment 88001 not found",
		},
		{
			name: "sql.ErrNoRows on a sprint comment read stays not-found (exit 4)",
			run: func() error {
				_, err := database.GetSprintComment(ctx, missing)
				return err
			},
			wantSentinel: utils.ErrNotFound,
			wantMessage:  "sprint comment 88001 not found",
		},
		{
			name: "a foreign-key violation stays not-found (exit 4)",
			run: func() error {
				return database.WithTransaction(func(tx *sql.Tx) error {
					_, err := InsertTaskCommentTx(tx, &models.TaskComment{
						TaskID: missing, Type: models.CommentFinding,
						Body: "This parent was never created.", CreatedAt: now,
					})
					return err
				})
			},
			wantSentinel: utils.ErrNotFound,
			wantMessage:  "task 88001 not found",
		},
		{
			name: "a CHECK violation on the type stays a validation failure (exit 6)",
			run: func() error {
				return database.WithTransaction(func(tx *sql.Tx) error {
					_, err := InsertSprintCommentTx(tx, &models.SprintComment{
						SprintID: sprintID, Type: models.CommentType("BLOCKER"),
						Body: "BLOCKER is not a sprint comment type.", CreatedAt: now,
					})
					return err
				})
			},
			wantSentinel: utils.ErrValidation,
			wantMessage:  `invalid comment type "BLOCKER" for a sprint comment`,
		},
		{
			name: "a CHECK violation on the body stays a size failure (exit 6)",
			run: func() error {
				return database.WithTransaction(func(tx *sql.Tx) error {
					_, err := InsertTaskCommentTx(tx, &models.TaskComment{
						TaskID: taskID, Type: models.CommentNote,
						Body: oversized, CreatedAt: now,
					})
					return err
				})
			},
			wantSentinel: utils.ErrFieldTooLarge,
			wantMessage:  "body exceeds maximum length of 4096 characters",
		},
		{
			name: "an UPDATE that matches no row stays not-found (exit 4)",
			run: func() error {
				body := "There is no such comment to edit."
				return database.WithTransaction(func(tx *sql.Tx) error {
					return UpdateTaskCommentTx(tx, missing, &CommentUpdate{Body: &body}, now)
				})
			},
			wantSentinel: utils.ErrNotFound,
			wantMessage:  "task comment 88001 not found",
		},
		{
			name: "a DELETE that matches no row stays not-found (exit 4)",
			run: func() error {
				return database.WithTransaction(func(tx *sql.Tx) error {
					return DeleteSprintCommentTx(tx, missing)
				})
			},
			wantSentinel: utils.ErrNotFound,
			wantMessage:  "sprint comment 88001 not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("the operation was expected to fail, but it succeeded")
			}
			if !errors.Is(err, tt.wantSentinel) {
				t.Errorf("error = %v, want it to chain %v", err, tt.wantSentinel)
			}
			if errors.Is(err, utils.ErrDatabase) {
				t.Errorf("error = %v was classified as utils.ErrDatabase (exit code 1); the row "+
					"above it in SPEC/ARCHITECTURE.md § Propagation Rules owns this failure, and a "+
					"blanket wrap applied ahead of the classifier is what causes this", err)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("error message = %q, want it to contain %q", err.Error(), tt.wantMessage)
			}
		})
	}

	// The uniqueness row of the same table, proved where a UNIQUE index actually
	// exists. idx_sprints_order is the collision internal/commands reports as
	// utils.ErrAlreadyExists (exit code 5) after asking IsUniqueConstraintErr;
	// the query layer must therefore hand the driver error back RECOGNISABLE,
	// not buried under a sentinel of its own.
	collision := database.WithTransaction(func(tx *sql.Tx) error {
		_, err := InsertSprintTx(tx, &models.Sprint{
			Title:       "Sprint sharing an execution order",
			Description: "Collide with the sprint seeded above on idx_sprints_order.",
			Status:      models.SprintPending,
			CreatedAt:   now,
			Order:       sprintOrderOf(t, database, sprintID),
		})
		return err
	})
	if collision == nil {
		t.Fatal("a duplicate sprint order was expected to violate idx_sprints_order, but the insert succeeded")
	}
	if !IsUniqueConstraintErr(collision) {
		t.Errorf("a duplicate sprint order = %v, want IsUniqueConstraintErr to recognise it so the "+
			"command layer can report utils.ErrAlreadyExists (exit code 5)", collision)
	}
	if errors.Is(collision, utils.ErrDatabase) {
		t.Errorf("a UNIQUE collision = %v was classified as utils.ErrDatabase (exit code 1); it belongs "+
			"to the utils.ErrAlreadyExists row (exit code 5)", collision)
	}
}

// sprintOrderOf reads one sprint's execution order straight out of the table, so
// the collision above is built from the value the database actually holds rather
// than from an assumption about how the seeder numbered it.
func sprintOrderOf(t *testing.T, database *DB, sprintID int) int {
	t.Helper()

	var order int
	if err := database.QueryRow(`SELECT order_index FROM sprints WHERE id = ?`, sprintID).Scan(&order); err != nil {
		t.Fatalf("reading the order of sprint %d: %v", sprintID, err)
	}
	return order
}
