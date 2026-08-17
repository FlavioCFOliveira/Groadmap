package db

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The tests below assert on the EXTENDED SQLite result code, not the primary
// SQLITE_CONSTRAINT (19): only the extended code proves that the row was rejected
// by the specific constraint under test (a CHECK, a foreign key, a NOT NULL)
// rather than by some other constraint that happens to fail on the same
// statement. The codes themselves (sqliteConstraintCheck, ...ForeignKey,
// ...NotNull) and the extendedResultCode helper live in connection.go: the comment
// write classifier added by rmp task #162 maps those same codes onto the project's
// error sentinels, so tests and production read one definition.
//
// assertRejected asserts that err is a SQLite constraint violation with the
// given extended result code, i.e. that the DATABASE rejected the write.
func assertRejected(t *testing.T, what string, err error, wantCode int) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: the database accepted the row; it must be rejected by the constraint", what)
		return
	}
	if got := extendedResultCode(err); got != wantCode {
		t.Errorf("%s: extended result code = %d, want %d (%v)", what, got, wantCode, err)
	}
}

// commentTablesFixture is the column contract of the two comment tables as
// SPEC/DATABASE.md § task_comments Table and § sprint_comments Table define it.
var commentTablesFixture = []struct {
	table     string
	parentCol string
	parentTbl string
	index     string
	types     []string // the exact set the type CHECK accepts
	rejected  []string // values the type CHECK must reject
}{
	{
		table:     "task_comments",
		parentCol: "task_id",
		parentTbl: "tasks",
		index:     "idx_task_comments_task_created",
		types:     []string{"FINDING", "HYPOTHESIS", "TEST", "DECISION", "PROGRESS", "UPDATE", "NOTE"},
		rejected:  []string{"", "finding", "REVIEW", "COMMENT", "BLOCKER"},
	},
	{
		table:     "sprint_comments",
		parentCol: "sprint_id",
		parentTbl: "sprints",
		index:     "idx_sprint_comments_sprint_created",
		types:     []string{"FINDING", "DECISION", "PROGRESS", "UPDATE"},
		// A sprint comment records the progression of the sprint, so the
		// task-only values are rejected by the database, not merely by Go.
		rejected: []string{"HYPOTHESIS", "TEST", "NOTE", "", "decision", "REVIEW"},
	},
}

// TestCommentTablesShape asserts that a FRESH database created by CreateSchema
// carries both comment tables exactly as specified: the six columns with their
// types and nullability, the AUTOINCREMENT primary key, the type and body
// CHECKs, the ON DELETE CASCADE foreign key, and the composite index on
// (parent_id, created_at ASC). Everything is read back out of sqlite_master and
// the pragma introspection tables, so the assertions describe the database that
// was actually built (acceptance criterion 1 of rmp task #160).
func TestCommentTablesShape(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	for _, fx := range commentTablesFixture {
		t.Run(fx.table, func(t *testing.T) {
			// The table exists and is a table.
			var createSQL string
			if err := db.QueryRow(
				`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, fx.table,
			).Scan(&createSQL); err != nil {
				t.Fatalf("table %s missing from sqlite_master: %v", fx.table, err)
			}

			// AUTOINCREMENT (not a bare INTEGER PRIMARY KEY): ids are never reused.
			if !strings.Contains(createSQL, "id INTEGER PRIMARY KEY AUTOINCREMENT") {
				t.Errorf("%s primary key is not AUTOINCREMENT:\n%s", fx.table, createSQL)
			}

			// The CHECK constraints must be in the stored DDL, so they are
			// enforced by the engine and not only by the Go layer.
			for _, want := range []string{
				"CHECK(length(body) <= 4096)",
				"ON DELETE CASCADE",
			} {
				if !strings.Contains(createSQL, want) {
					t.Errorf("%s DDL does not contain %q:\n%s", fx.table, want, createSQL)
				}
			}
			wantTypeCheck := "CHECK(type IN ('" + strings.Join(fx.types, "', '") + "'))"
			if !strings.Contains(createSQL, wantTypeCheck) {
				t.Errorf("%s type CHECK is not %q:\n%s", fx.table, wantTypeCheck, createSQL)
			}

			// Columns: name, declared type, nullability and primary-key flag.
			wantCols := []struct {
				name    string
				typ     string
				notNull int
				pk      int
			}{
				{"id", "INTEGER", 0, 1},
				{fx.parentCol, "INTEGER", 1, 0},
				{"type", "TEXT", 1, 0},
				{"body", "TEXT", 1, 0},
				{"created_at", "TEXT", 1, 0},
				{"updated_at", "TEXT", 0, 0},
			}
			gotCols := tableColumns(t, db, fx.table)
			if len(gotCols) != len(wantCols) {
				t.Fatalf("%s has %d columns (%v), want %d", fx.table, len(gotCols), gotCols, len(wantCols))
			}
			for i, want := range wantCols {
				got := gotCols[i]
				if got.name != want.name || got.typ != want.typ ||
					got.notNull != want.notNull || got.pk != want.pk {
					t.Errorf("%s column %d = %+v, want {name:%s typ:%s notNull:%d pk:%d}",
						fx.table, i, got, want.name, want.typ, want.notNull, want.pk)
				}
			}

			// Foreign key: one, onto the parent table's id, ON DELETE CASCADE.
			rows, err := db.Query(`SELECT "table", "from", "to", on_delete FROM pragma_foreign_key_list(?)`, fx.table)
			if err != nil {
				t.Fatalf("reading foreign keys of %s: %v", fx.table, err)
			}
			defer rows.Close()
			fkCount := 0
			for rows.Next() {
				var parent, from, to, onDelete string
				if err := rows.Scan(&parent, &from, &to, &onDelete); err != nil {
					t.Fatalf("scanning foreign key of %s: %v", fx.table, err)
				}
				fkCount++
				if parent != fx.parentTbl || from != fx.parentCol || to != "id" {
					t.Errorf("%s foreign key = %s.%s -> %s.%s, want %s -> %s.id",
						fx.table, fx.table, from, parent, to, fx.parentCol, fx.parentTbl)
				}
				if onDelete != "CASCADE" {
					t.Errorf("%s foreign key on_delete = %q, want CASCADE", fx.table, onDelete)
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("iterating foreign keys of %s: %v", fx.table, err)
			}
			if fkCount != 1 {
				t.Errorf("%s has %d foreign keys, want exactly 1", fx.table, fkCount)
			}

			// Index: on this table, over (parent_col, created_at) in that order,
			// both ascending. A reversed or reordered index would still exist by
			// name, so the columns and their direction are checked, not the name.
			var idxTable string
			if err := db.QueryRow(
				`SELECT tbl_name FROM sqlite_master WHERE type = 'index' AND name = ?`, fx.index,
			).Scan(&idxTable); err != nil {
				t.Fatalf("index %s missing from sqlite_master: %v", fx.index, err)
			}
			if idxTable != fx.table {
				t.Errorf("index %s is on %q, want %q", fx.index, idxTable, fx.table)
			}
			gotIdx := indexColumns(t, db, fx.index)
			wantIdx := []indexColumn{{fx.parentCol, 0}, {"created_at", 0}}
			if len(gotIdx) != len(wantIdx) {
				t.Fatalf("index %s covers %v, want %v", fx.index, gotIdx, wantIdx)
			}
			for i := range wantIdx {
				if gotIdx[i] != wantIdx[i] {
					t.Errorf("index %s position %d = %+v, want %+v", fx.index, i, gotIdx[i], wantIdx[i])
				}
			}
		})
	}
}

// TestCommentTypeCheckRejectsForeignValues proves the type CHECK is enforced by
// the DATABASE: every accepted value is inserted successfully and every value
// outside that table's subset is rejected with SQLITE_CONSTRAINT_CHECK. It is the
// gate for acceptance criterion 5 of rmp task #160, and in particular for the
// asymmetry between the two tables: HYPOTHESIS, TEST and NOTE are valid on a
// task comment and invalid on a sprint comment.
func TestCommentTypeCheckRejectsForeignValues(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, sprintID := seedCommentParents(t, db)
	parentID := map[string]int{"task_comments": taskID, "sprint_comments": sprintID}

	for _, fx := range commentTablesFixture {
		t.Run(fx.table, func(t *testing.T) {
			insert := "INSERT INTO " + fx.table +
				" (" + fx.parentCol + ", type, body, created_at) VALUES (?, ?, ?, ?)"

			for _, typ := range fx.types {
				if _, err := db.Exec(insert, parentID[fx.table], typ,
					"The retry budget was exhausted after the third attempt.", utils.NowISO8601()); err != nil {
					t.Errorf("%s rejected the specified type %q: %v", fx.table, typ, err)
				}
			}

			for _, typ := range fx.rejected {
				_, err := db.Exec(insert, parentID[fx.table], typ,
					"An unspecified comment type must never reach the table.", utils.NowISO8601())
				assertRejected(t, fx.table+" type="+typ, err, sqliteConstraintCheck)
			}
		})
	}
}

// TestCommentBodyAndParentConstraints proves the remaining database-level
// constraints on both comment tables: the 4096-character body cap (which counts
// CHARACTERS, not bytes), the NOT NULL on the body and on the parent key, and the
// foreign key that rejects a comment addressed to a parent that does not exist.
func TestCommentBodyAndParentConstraints(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, sprintID := seedCommentParents(t, db)
	parentID := map[string]int{"task_comments": taskID, "sprint_comments": sprintID}

	for _, fx := range commentTablesFixture {
		t.Run(fx.table, func(t *testing.T) {
			insert := "INSERT INTO " + fx.table +
				" (" + fx.parentCol + ", type, body, created_at) VALUES (?, ?, ?, ?)"
			validType := fx.types[0]
			now := utils.NowISO8601()

			// Exactly at the cap: accepted.
			if _, err := db.Exec(insert, parentID[fx.table], validType, strings.Repeat("a", 4096), now); err != nil {
				t.Errorf("%s rejected a 4096-character body, which is exactly the cap: %v", fx.table, err)
			}

			// One character over the cap: rejected by the CHECK.
			_, err := db.Exec(insert, parentID[fx.table], validType, strings.Repeat("a", 4097), now)
			assertRejected(t, fx.table+" body of 4097 characters", err, sqliteConstraintCheck)

			// SQLite length() on TEXT counts characters, so 4096 multi-byte
			// characters (8192 bytes) are within the cap. Tasks #161/#162 must
			// validate the same way (runes, not bytes) or the Go layer and the
			// database will disagree on the boundary.
			if _, err := db.Exec(insert, parentID[fx.table], validType, strings.Repeat("ç", 4096), now); err != nil {
				t.Errorf("%s rejected 4096 multi-byte characters; the CHECK counts characters, not bytes: %v", fx.table, err)
			}

			// Empty body: accepted by the database. The comment body is required
			// at the command layer, not by a database CHECK, and the SPEC states
			// no minimum length; this pins the current, specified behaviour so a
			// later CHECK cannot be added silently.
			if _, err := db.Exec(insert, parentID[fx.table], validType, "", now); err != nil {
				t.Errorf("%s rejected an empty body; no CHECK in SPEC/DATABASE.md forbids it: %v", fx.table, err)
			}

			// NULL body: rejected (NOT NULL).
			_, err = db.Exec(insert, parentID[fx.table], validType, nil, now)
			assertRejected(t, fx.table+" NULL body", err, sqliteConstraintNotNull)

			// NULL created_at: rejected (NOT NULL).
			_, err = db.Exec(insert, parentID[fx.table], validType, "A comment always carries its creation time.", nil)
			assertRejected(t, fx.table+" NULL created_at", err, sqliteConstraintNotNull)

			// NULL parent: rejected (NOT NULL) - a comment can never exist
			// without its parent.
			_, err = db.Exec(insert, nil, validType, "An orphan comment must be impossible.", now)
			assertRejected(t, fx.table+" NULL "+fx.parentCol, err, sqliteConstraintNotNull)

			// Non-existent parent: rejected by the foreign key.
			_, err = db.Exec(insert, 987654, validType, "This parent was never created.", now)
			assertRejected(t, fx.table+" unknown "+fx.parentCol, err, sqliteConstraintForeignKey)
		})
	}
}

// TestCommentIdsAreIndependentAutoincrementSequences proves the two properties
// SPEC/DATABASE.md § task_comments Table states about comment ids: the sequences
// are per-table (the same id value addresses two unrelated rows), and they are
// AUTOINCREMENT, so a deleted id is never handed out again.
func TestCommentIdsAreIndependentAutoincrementSequences(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, sprintID := seedCommentParents(t, db)
	now := utils.NowISO8601()

	taskFirst := insertCommentRow(t, db, "task_comments", "task_id", taskID, "FINDING",
		"The connection pool saturates at 64 concurrent readers.", now)
	sprintFirst := insertCommentRow(t, db, "sprint_comments", "sprint_id", sprintID, "PROGRESS",
		"Two of the six planned tasks are closed.", now)

	if taskFirst != 1 || sprintFirst != 1 {
		t.Errorf("first ids = task %d, sprint %d, want 1 and 1: the sequences must be independent",
			taskFirst, sprintFirst)
	}

	second := insertCommentRow(t, db, "task_comments", "task_id", taskID, "DECISION",
		"The pool size stays at 64; raising it moves the bottleneck to disk.", now)
	if _, err := db.Exec("DELETE FROM task_comments WHERE id = ?", second); err != nil {
		t.Fatalf("deleting task comment %d: %v", second, err)
	}
	third := insertCommentRow(t, db, "task_comments", "task_id", taskID, "NOTE",
		"Re-measure once the storage tier is upgraded.", now)
	if third <= second {
		t.Errorf("id after deleting %d = %d, want > %d: AUTOINCREMENT must not reuse ids", second, third, second)
	}
}

// TestCommentIndexShapeEarnsItsPlace proves WHY the index is composite. That the
// production listings use it, take no full scan and need no temporary B-tree is
// asserted on the production SQL in TestCompositeIndexesServeTheProductionQueries,
// alongside every other composite index (that is where the placeholder rmp task
// #160 left here has moved to, now that the query builders of #162 exist).
//
// What is proven here instead is the counterfactual the SPEC's rationale rests on
// (SPEC/DATABASE.md § Index Design Rationale): the trailing created_at column is
// what removes the sort step. The SAME production statement is planned twice - once
// against the schema's (parent_id, created_at) index, once against a probe index on
// the parent key alone - and only the second one has to sort. Replacing the
// composite index with a plain one on the parent key would therefore silently
// reintroduce a sort on every comment listing, and this test is what catches it.
func TestCommentIndexShapeEarnsItsPlace(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	taskID, sprintID := seedCommentParents(t, db)
	now := utils.NowISO8601()
	for range 20 {
		insertCommentRow(t, db, "task_comments", "task_id", taskID, "PROGRESS",
			"Progress entry recorded while the benchmark ran.", now)
		insertCommentRow(t, db, "sprint_comments", "sprint_id", sprintID, "PROGRESS",
			"Sprint progress entry recorded while the benchmark ran.", now)
	}

	listings := map[string]struct {
		query     string
		table     string
		index     string
		parentCol string
		parentID  int
	}{
		"task_comments": {
			query: taskCommentStmts.selectByParent, table: "task_comments",
			index: "idx_task_comments_task_created", parentCol: "task_id", parentID: taskID,
		},
		"sprint_comments": {
			query: sprintCommentStmts.selectByParent, table: "sprint_comments",
			index: "idx_sprint_comments_sprint_created", parentCol: "sprint_id", parentID: sprintID,
		},
	}

	for name, fx := range listings {
		t.Run(name, func(t *testing.T) {
			composite := queryPlan(t, db, fx.query, fx.parentID)
			if !strings.Contains(composite, fx.index) || strings.Contains(composite, "TEMP B-TREE") {
				t.Fatalf("with %s in place the listing must be planned onto it with no sort.\nplan: %s",
					fx.index, composite)
			}

			// Swap the composite index for one on the parent key alone. The test
			// database is disposable, so this is a safe way to ask the planner what
			// the other shape would cost.
			if _, err := db.Exec("DROP INDEX " + fx.index); err != nil {
				t.Fatalf("dropping %s: %v", fx.index, err)
			}
			probe := "probe_" + fx.table + "_parent_only"
			if _, err := db.Exec("CREATE INDEX " + probe + " ON " + fx.table + "(" + fx.parentCol + ")"); err != nil {
				t.Fatalf("creating the parent-key-only probe index: %v", err)
			}

			parentOnly := queryPlan(t, db, fx.query, fx.parentID)
			if !strings.Contains(parentOnly, "TEMP B-TREE") {
				t.Errorf("with only a parent-key index the listing plans without a sort (%s), so the "+
					"trailing created_at column of %s would earn nothing and this test proves nothing; "+
					"the assertion needs revisiting", parentOnly, fx.index)
			}
		})
	}
}

// ==================== MIGRATION 1.8.0 -> 1.9.0 ====================

// TestMigrateV1_8_0_toV1_9_0_OnNextOpen is the gate for acceptance criteria 2
// and 4 of rmp task #160. It builds a REAL on-disk roadmap database at schema
// 1.8.0 holding real tasks and sprints, then opens it the way any rmp command
// does and asserts that:
//
//   - the database reaches 1.9.0 with no user action;
//   - both comment tables exist and are empty;
//   - not one task or sprint row was lost or altered;
//   - ON DELETE CASCADE actually fires at runtime, which is only true because
//     the DSN carries _foreign_keys=1 on every connection (see dsnFor).
func TestMigrateV1_8_0_toV1_9_0_OnNextOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmapName = "payment-reconciliation"
	taskIDs, sprintIDs := buildRoadmapAtSchema180(t, roadmapName)

	// The next open of the database must migrate it. This is the production
	// entry point every command goes through.
	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("opening the 1.8.0 database: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	version, err := database.GetSchemaVersion()
	if err != nil {
		t.Fatalf("reading schema version after open: %v", err)
	}
	if version != "1.9.0" {
		t.Fatalf("schema_version after open = %q, want 1.9.0", version)
	}

	// No data loss: every seeded row is still there, unchanged.
	assertRowCount(t, database, len(taskIDs), "tasks survive the migration", "SELECT COUNT(*) FROM tasks")
	assertRowCount(t, database, len(sprintIDs), "sprints survive the migration", "SELECT COUNT(*) FROM sprints")
	var title string
	if err := database.QueryRow("SELECT title FROM tasks WHERE id = ?", taskIDs[0]).Scan(&title); err != nil {
		t.Fatalf("re-reading task %d: %v", taskIDs[0], err)
	}
	if title != "Reconcile the settlement ledger against the acquirer report" {
		t.Errorf("task %d title = %q after migration; the migration must not touch existing rows", taskIDs[0], title)
	}

	// Both comment tables exist and start empty.
	for _, fx := range commentTablesFixture {
		assertRowCount(t, database, 0, fx.table+" exists and is empty after the migration",
			"SELECT COUNT(*) FROM "+fx.table)
	}

	// Comments are writable on the migrated database, and the CHECK the
	// migration installed is enforced there too.
	now := utils.NowISO8601()
	insertCommentRow(t, database, "task_comments", "task_id", taskIDs[0], "HYPOTHESIS",
		"The acquirer report is one settlement window behind.", now)
	insertCommentRow(t, database, "task_comments", "task_id", taskIDs[0], "TEST",
		"Replayed yesterday's window: the totals match with a one-day shift.", now)
	insertCommentRow(t, database, "task_comments", "task_id", taskIDs[1], "FINDING",
		"Two ledger entries carry the same external reference.", now)
	insertCommentRow(t, database, "sprint_comments", "sprint_id", sprintIDs[0], "DECISION",
		"Reconciliation runs after the acquirer window closes, not at midnight.", now)
	_, err = database.Exec(
		`INSERT INTO sprint_comments (sprint_id, type, body, created_at) VALUES (?, 'NOTE', ?, ?)`,
		sprintIDs[0], "A sprint comment must not accept a task-only type.", now)
	assertRejected(t, "migrated sprint_comments type=NOTE", err, sqliteConstraintCheck)

	// Acceptance criterion 4: deleting the parent deletes its comments, counted
	// before and after. The count is taken per parent so the assertion cannot be
	// satisfied by an unrelated row.
	assertRowCount(t, database, 2, "task comments before deleting the task",
		"SELECT COUNT(*) FROM task_comments WHERE task_id = ?", taskIDs[0])
	assertRowCount(t, database, 3, "all task comments before the delete",
		"SELECT COUNT(*) FROM task_comments")
	if _, err := database.Exec("DELETE FROM tasks WHERE id = ?", taskIDs[0]); err != nil {
		t.Fatalf("deleting task %d: %v", taskIDs[0], err)
	}
	assertRowCount(t, database, 0, "task comments after deleting the task (ON DELETE CASCADE)",
		"SELECT COUNT(*) FROM task_comments WHERE task_id = ?", taskIDs[0])
	assertRowCount(t, database, 1, "the comments of the other task must survive",
		"SELECT COUNT(*) FROM task_comments")

	assertRowCount(t, database, 1, "sprint comments before deleting the sprint",
		"SELECT COUNT(*) FROM sprint_comments WHERE sprint_id = ?", sprintIDs[0])
	if _, err := database.Exec("DELETE FROM sprints WHERE id = ?", sprintIDs[0]); err != nil {
		t.Fatalf("deleting sprint %d: %v", sprintIDs[0], err)
	}
	assertRowCount(t, database, 0, "sprint comments after deleting the sprint (ON DELETE CASCADE)",
		"SELECT COUNT(*) FROM sprint_comments WHERE sprint_id = ?", sprintIDs[0])
}

// TestCommentCascadeOnFreshDatabase is the fresh-schema half of acceptance
// criterion 4: the CASCADE must hold on a database created at 1.9.0, not only on
// a migrated one. It runs against the production Open path so the foreign-key
// enforcement it relies on is the one real commands get.
func TestCommentCascadeOnFreshDatabase(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	database, err := Open("fraud-detection-rules")
	if err != nil {
		t.Fatalf("creating a fresh roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	// The CASCADE is enforced only while foreign_keys is on. Assert it directly:
	// without this PRAGMA the deletions below would leave the comments orphaned
	// and the test would be vacuous.
	var fk int
	if err := database.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("reading foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d on a connection from Open; ON DELETE CASCADE cannot fire", fk)
	}

	taskID, sprintID := seedCommentParents(t, database)
	now := utils.NowISO8601()
	insertCommentRow(t, database, "task_comments", "task_id", taskID, "FINDING",
		"The velocity rule fires on refunds, which are not purchases.", now)
	insertCommentRow(t, database, "task_comments", "task_id", taskID, "DECISION",
		"Refunds are excluded from the velocity window.", now)
	insertCommentRow(t, database, "sprint_comments", "sprint_id", sprintID, "FINDING",
		"Two of the five rules share a threshold that must be tuned together.", now)

	assertRowCount(t, database, 2, "task comments before the delete", "SELECT COUNT(*) FROM task_comments")
	assertRowCount(t, database, 1, "sprint comments before the delete", "SELECT COUNT(*) FROM sprint_comments")

	if _, err := database.Exec("DELETE FROM tasks WHERE id = ?", taskID); err != nil {
		t.Fatalf("deleting task %d: %v", taskID, err)
	}
	if _, err := database.Exec("DELETE FROM sprints WHERE id = ?", sprintID); err != nil {
		t.Fatalf("deleting sprint %d: %v", sprintID, err)
	}

	assertRowCount(t, database, 0, "task comments after deleting the task (ON DELETE CASCADE)",
		"SELECT COUNT(*) FROM task_comments")
	assertRowCount(t, database, 0, "sprint comments after deleting the sprint (ON DELETE CASCADE)",
		"SELECT COUNT(*) FROM sprint_comments")
}

// TestMigrateV1_8_0_toV1_9_0_DoubleRunIsANoOp is the gate for acceptance
// criterion 3: applying the migration a second time over the same database must
// change nothing. The proof is a full sqlite_master snapshot taken after the
// first apply and compared byte for byte after the second, plus a comment row
// written between the two applies that must still be there afterwards.
func TestMigrateV1_8_0_toV1_9_0_DoubleRunIsANoOp(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening in-memory database: %v", err)
	}
	defer sqlDB.Close() //nolint:errcheck // test cleanup

	database := &DB{DB: sqlDB, roadmapName: "double-apply", queryCache: NewQueryCache(), batchProc: NewBatchProcessor(100)}
	if err := database.CreateSchema(); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	taskID, _ := seedCommentParents(t, database)

	apply := func(stage string) {
		t.Helper()
		tx, err := sqlDB.Begin()
		if err != nil {
			t.Fatalf("[%s] begin: %v", stage, err)
		}
		if err := migrateV1_8_0_toV1_9_0(tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("[%s] migrateV1_8_0_toV1_9_0: %v", stage, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("[%s] commit: %v", stage, err)
		}
	}

	// The tables already exist (CreateSchema built them), so the very first
	// apply is itself a re-apply: this is exactly the state a database that was
	// created fresh at 1.9.0 and then handed to the migration runner is in.
	apply("first")
	before := schemaSnapshot(t, sqlDB)

	commentID := insertCommentRow(t, database, "task_comments", "task_id", taskID, "UPDATE",
		"Recorded between the two migration applies.", utils.NowISO8601())

	apply("second")
	after := schemaSnapshot(t, sqlDB)

	if before != after {
		t.Errorf("re-applying the migration changed the schema.\nbefore:\n%s\nafter:\n%s", before, after)
	}

	// The re-apply must not touch data either.
	var body string
	if err := sqlDB.QueryRow("SELECT body FROM task_comments WHERE id = ?", commentID).Scan(&body); err != nil {
		t.Fatalf("re-reading the comment written between applies: %v", err)
	}
	if body != "Recorded between the two migration applies." {
		t.Errorf("comment body = %q after the second apply; the migration must not touch rows", body)
	}
}

// TestRunMigrationsTwiceIsANoOpOnDisk is the double-run proof at the level a
// user can reach: opening the same on-disk 1.8.0 database twice. The second open
// finds the schema already at 1.9.0, so no migration runs, and the full
// sqlite_master snapshot is identical.
func TestRunMigrationsTwiceIsANoOpOnDisk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmapName = "settlement-window"
	buildRoadmapAtSchema180(t, roadmapName)

	first, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	firstSnapshot := schemaSnapshot(t, first.DB)
	firstVersion, err := first.GetSchemaVersion()
	if err != nil {
		t.Fatalf("reading version after first open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("closing after first open: %v", err)
	}

	second, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer second.Close() //nolint:errcheck // test cleanup

	// Explicitly run the migration set again on the already-migrated database.
	if err := second.RunMigrations(); err != nil {
		t.Fatalf("re-running migrations on a 1.9.0 database: %v", err)
	}
	secondSnapshot := schemaSnapshot(t, second.DB)
	secondVersion, err := second.GetSchemaVersion()
	if err != nil {
		t.Fatalf("reading version after second open: %v", err)
	}

	if firstVersion != "1.9.0" || secondVersion != "1.9.0" {
		t.Errorf("schema versions = %q then %q, want 1.9.0 both times", firstVersion, secondVersion)
	}
	if firstSnapshot != secondSnapshot {
		t.Errorf("re-opening and re-running the migrations changed the schema.\nfirst:\n%s\nsecond:\n%s",
			firstSnapshot, secondSnapshot)
	}
}

// TestMigratedAndFreshCommentTablesAreIdentical pins the guarantee stated in
// SPEC/VERSION.md § Migration 1.8.0 → 1.9.0: because the migration creates whole
// tables rather than retrofitting columns onto populated ones, a migrated
// database and a freshly created one end up with IDENTICAL comment tables,
// CHECK constraints included. The DDL is intentionally duplicated between
// CreateSchema and migrateV1_8_0_toV1_9_0 (a migration is a historical record),
// so this test is what stops the two copies drifting apart.
func TestMigratedAndFreshCommentTablesAreIdentical(t *testing.T) {
	commentSchemaOf := func(t *testing.T, build func(*DB) error) string {
		t.Helper()
		sqlDB, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("opening in-memory database: %v", err)
		}
		defer sqlDB.Close() //nolint:errcheck // test cleanup
		database := &DB{DB: sqlDB, roadmapName: "shape", queryCache: NewQueryCache(), batchProc: NewBatchProcessor(100)}
		if err := build(database); err != nil {
			t.Fatalf("building schema: %v", err)
		}
		return commentSchemaSnapshot(t, sqlDB)
	}

	fresh := commentSchemaOf(t, func(d *DB) error { return d.CreateSchema() })

	migrated := commentSchemaOf(t, func(d *DB) error {
		// A 1.8.0 database is the current schema without the comment tables:
		// migration 1.9.0 adds nothing else. Building it by CreateSchema + DROP
		// keeps this fixture correct by construction instead of restating the
		// whole 1.8.0 DDL, which would rot on the next schema change.
		if err := d.CreateSchema(); err != nil {
			return err
		}
		for _, table := range []string{"task_comments", "sprint_comments"} {
			if _, err := d.Exec("DROP TABLE " + table); err != nil {
				return err
			}
		}
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if err := migrateV1_8_0_toV1_9_0(tx); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	})

	if fresh != migrated {
		t.Errorf("the migration builds different comment tables than CreateSchema does.\n"+
			"fresh:\n%s\nmigrated:\n%s", fresh, migrated)
	}
}

// ==================== HELPERS ====================

// column is one row of pragma_table_info.
type column struct {
	name    string
	typ     string
	notNull int
	pk      int
}

// tableColumns returns the columns of table in declaration order.
func tableColumns(t *testing.T, db *DB, table string) []column {
	t.Helper()
	rows, err := db.Query(`SELECT name, type, "notnull", pk FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatalf("reading columns of %s: %v", table, err)
	}
	defer rows.Close()

	var cols []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.typ, &c.notNull, &c.pk); err != nil {
			t.Fatalf("scanning column of %s: %v", table, err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating columns of %s: %v", table, err)
	}
	return cols
}

// indexColumn is one indexed column: its name and its sort direction (0 = ASC).
type indexColumn struct {
	name string
	desc int
}

// indexColumns returns the columns of index in index order, excluding the
// rowid pragma_index_xinfo appends after the declared columns.
func indexColumns(t *testing.T, db *DB, index string) []indexColumn {
	t.Helper()
	rows, err := db.Query(
		`SELECT name, desc FROM pragma_index_xinfo(?) WHERE key = 1 ORDER BY seqno`, index)
	if err != nil {
		t.Fatalf("reading columns of index %s: %v", index, err)
	}
	defer rows.Close()

	var cols []indexColumn
	for rows.Next() {
		var c indexColumn
		if err := rows.Scan(&c.name, &c.desc); err != nil {
			t.Fatalf("scanning column of index %s: %v", index, err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating columns of index %s: %v", index, err)
	}
	return cols
}

// schemaSnapshot renders the whole schema (every table, index and trigger, with
// its stored DDL) as one deterministic string. Two snapshots differ if and only
// if the schema differs.
func schemaSnapshot(t *testing.T, sqlDB *sql.DB) string {
	t.Helper()
	rows, err := sqlDB.Query(
		`SELECT type, name, tbl_name, IFNULL(sql, '') FROM sqlite_master ORDER BY type, name`)
	if err != nil {
		t.Fatalf("reading sqlite_master: %v", err)
	}
	defer rows.Close()

	var out strings.Builder
	for rows.Next() {
		var typ, name, tbl, ddl string
		if err := rows.Scan(&typ, &name, &tbl, &ddl); err != nil {
			t.Fatalf("scanning sqlite_master row: %v", err)
		}
		out.WriteString(typ + " " + name + " ON " + tbl + "\n" + ddl + "\n---\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating sqlite_master: %v", err)
	}
	return out.String()
}

// commentSchemaSnapshot is schemaSnapshot restricted to the two comment tables
// and their indexes.
func commentSchemaSnapshot(t *testing.T, sqlDB *sql.DB) string {
	t.Helper()
	rows, err := sqlDB.Query(
		`SELECT type, name, tbl_name, IFNULL(sql, '') FROM sqlite_master
		 WHERE tbl_name IN ('task_comments', 'sprint_comments') ORDER BY type, name`)
	if err != nil {
		t.Fatalf("reading comment schema: %v", err)
	}
	defer rows.Close()

	var out strings.Builder
	for rows.Next() {
		var typ, name, tbl, ddl string
		if err := rows.Scan(&typ, &name, &tbl, &ddl); err != nil {
			t.Fatalf("scanning comment schema row: %v", err)
		}
		out.WriteString(typ + " " + name + " ON " + tbl + "\n" + ddl + "\n---\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating comment schema: %v", err)
	}
	return out.String()
}

// seedCommentParents creates one task and one sprint and returns their ids, so
// comments have real parents to hang off.
func seedCommentParents(t *testing.T, db *DB) (taskID, sprintID int) {
	t.Helper()
	ctx := testContext()

	sprintID, err := db.CreateSprint(ctx, &models.Sprint{
		Title:       "Comment storage foundation",
		Description: "Give tasks and sprints a durable, typed record of what was learned.",
		Status:      models.SprintPending,
		CreatedAt:   utils.NowISO8601(),
	})
	if err != nil {
		t.Fatalf("creating sprint: %v", err)
	}

	taskID, err = db.CreateTask(ctx, &models.Task{
		Title:                  "Create the comment tables and the 1.9.0 migration",
		Type:                   models.TypeTask,
		Status:                 models.StatusBacklog,
		FunctionalRequirements: "Comments need somewhere to live in every existing roadmap database.",
		TechnicalRequirements:  "Two tables, one index each, one idempotent migration.",
		AcceptanceCriteria:     "A fresh database and a migrated one carry identical comment tables.",
	})
	if err != nil {
		t.Fatalf("creating task: %v", err)
	}
	return taskID, sprintID
}

// deleteParentRow removes one row from a comment parent table by id, so a test
// that is about the schema's ON DELETE CASCADE triggers it directly instead of
// through a helper whose own behaviour would then be under test too. The table
// name is a test-supplied literal, never user input.
func deleteParentRow(t *testing.T, db *DB, table string, id int) {
	t.Helper()

	// The table name is concatenated because SQL has no parameter for an
	// identifier; it is a literal chosen by the caller in this file, never user
	// input, and the id is bound.
	result, err := db.ExecContext(testContext(), "DELETE FROM "+table+" WHERE id = ?", id)
	if err != nil {
		t.Fatalf("deleting %s %d: %v", table, id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("rows affected deleting %s %d: %v", table, id, err)
	}
	if affected != 1 {
		t.Fatalf("deleting %s %d removed %d rows, want 1", table, id, affected)
	}
}

// insertComment inserts one comment and returns its id.
func insertCommentRow(t *testing.T, db *DB, table, parentCol string, parentID int, typ, body, createdAt string) int {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO "+table+" ("+parentCol+", type, body, created_at) VALUES (?, ?, ?, ?)",
		parentID, typ, body, createdAt)
	if err != nil {
		t.Fatalf("inserting %s row: %v", table, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("reading the id of the inserted %s row: %v", table, err)
	}
	return int(id)
}

// assertRowCount asserts that query, which must return a single integer, yields
// want. what names the property under test in the failure message; the bind
// arguments are passed through, never interpolated into the SQL.
func assertRowCount(t *testing.T, db *DB, want int, what, query string, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRow(query, args...).Scan(&got); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if got != want {
		t.Errorf("%s: count = %d, want %d", what, got, want)
	}
}

// buildRoadmapAtSchema180 creates a real on-disk roadmap under the test HOME,
// seeds it with tasks and sprints through the production write paths, and then
// takes it back to schema 1.8.0 by dropping the two comment tables and rewriting
// _metadata. A 1.8.0 database is exactly the current schema without those tables
// (migration 1.9.0 adds nothing else), so building the fixture this way keeps it
// correct by construction rather than restating the entire 1.8.0 DDL.
//
// It returns the ids of the seeded tasks and sprints.
func buildRoadmapAtSchema180(t *testing.T, roadmapName string) (taskIDs, sprintIDs []int) {
	t.Helper()
	ctx := testContext()

	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("creating roadmap %q: %v", roadmapName, err)
	}

	sprintID, err := database.CreateSprint(ctx, &models.Sprint{
		Title:       "Settlement reconciliation",
		Description: "Reconcile the internal ledger with the acquirer settlement report.",
		Status:      models.SprintPending,
		CreatedAt:   utils.NowISO8601(),
	})
	if err != nil {
		t.Fatalf("creating sprint: %v", err)
	}
	sprintIDs = append(sprintIDs, sprintID)

	for _, title := range []string{
		"Reconcile the settlement ledger against the acquirer report",
		"Alert on any settlement window that fails to balance",
	} {
		id, err := database.CreateTask(ctx, &models.Task{
			Title:                  title,
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			FunctionalRequirements: "Every settlement window must balance to the cent.",
			TechnicalRequirements:  "Compare the ledger totals with the acquirer report per window.",
			AcceptanceCriteria:     "An unbalanced window raises an alert within one hour.",
		})
		if err != nil {
			t.Fatalf("creating task %q: %v", title, err)
		}
		taskIDs = append(taskIDs, id)
	}

	// Take the database back to 1.8.0.
	for _, table := range []string{"task_comments", "sprint_comments"} {
		if _, err := database.Exec("DROP TABLE " + table); err != nil {
			t.Fatalf("dropping %s to build the 1.8.0 fixture: %v", table, err)
		}
	}
	if _, err := database.Exec(
		"UPDATE _metadata SET value = '1.8.0' WHERE key = 'schema_version'"); err != nil {
		t.Fatalf("setting schema_version to 1.8.0: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("closing the 1.8.0 fixture: %v", err)
	}

	return taskIDs, sprintIDs
}
