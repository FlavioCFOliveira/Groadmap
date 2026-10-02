package db

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ==================== MIGRATION 1.16.0 -> 1.17.0 ====================
//
// The gates of SPEC/VERSION.md § Migration 1.16.0 → 1.17.0 (rmp task 286): the
// migration drops idx_sprints_created_at and does nothing else, so a database
// created at any earlier version ends, once opened, with exactly the index set
// a fresh 1.17.0 database is created with, every row untouched.

// createSprintsCreatedAtIndex is the declaration of idx_sprints_created_at that
// every schema before 1.17.0 carried, transcribed from the schema that shipped
// at 1.16.0.
const createSprintsCreatedAtIndex = `CREATE INDEX IF NOT EXISTS idx_sprints_created_at ON sprints(created_at)`

// restoreSprintsCreatedAtIndex puts idx_sprints_created_at back on a database
// built from the current schema, so that a fixture taken back to a version
// before 1.17.0 carries the index that version declared.
func restoreSprintsCreatedAtIndex(t *testing.T, database *DB) {
	t.Helper()
	if _, err := database.Exec(createSprintsCreatedAtIndex); err != nil {
		t.Fatalf("restoring idx_sprints_created_at for a pre-1.17.0 fixture: %v", err)
	}
}

// fullIndexSetOf renders every index of every table of database, from
// PRAGMA index_list (name, uniqueness, origin, partial flag) and
// PRAGMA index_xinfo (every column of the index, key and auxiliary alike, with
// its position, name, direction and collation). An implicit constraint index is
// named by its table and origin rather than by its sqlite_autoindex name. Two
// databases with equal renderings have the same index set, columns, order and
// direction on every table.
func fullIndexSetOf(t *testing.T, database *DB) []string {
	t.Helper()

	tables := queryStrings(t, database, `SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if len(tables) == 0 {
		t.Fatal("the database holds no table")
	}
	// A capacity hint only: a table of this schema carries at most six indexes.
	out := make([]string, 0, 6*len(tables))
	for _, table := range tables {
		type listed struct {
			name, origin    string
			unique, partial bool
		}
		rows, err := database.Query(`SELECT name, "unique", origin, partial FROM pragma_index_list(?)`, table)
		if err != nil {
			t.Fatalf("listing the indexes of %s: %v", table, err)
		}
		var indexes []listed
		for rows.Next() {
			var l listed
			if err := rows.Scan(&l.name, &l.unique, &l.origin, &l.partial); err != nil {
				t.Fatalf("scanning an index of %s: %v", table, err)
			}
			indexes = append(indexes, l)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterating the indexes of %s: %v", table, err)
		}
		rows.Close()

		for _, l := range indexes {
			colRows, err := database.Query(
				`SELECT seqno, cid, IFNULL(name, ''), "desc", IFNULL(coll, ''), key FROM pragma_index_xinfo(?) ORDER BY seqno`, l.name)
			if err != nil {
				t.Fatalf("reading the columns of %s: %v", l.name, err)
			}
			var cols []string
			for colRows.Next() {
				var seqno, cid int
				var name, coll string
				var desc, key bool
				if err := colRows.Scan(&seqno, &cid, &name, &desc, &coll, &key); err != nil {
					t.Fatalf("scanning a column of %s: %v", l.name, err)
				}
				cols = append(cols, fmt.Sprintf("%d:%d:%s:desc=%v:%s:key=%v", seqno, cid, name, desc, coll, key))
			}
			if err := colRows.Err(); err != nil {
				t.Fatalf("iterating the columns of %s: %v", l.name, err)
			}
			colRows.Close()

			label := l.name
			if l.origin != "c" {
				label = table + "." + l.origin
			}
			out = append(out, fmt.Sprintf("%s.%s unique=%v partial=%v (%s)",
				table, label, l.unique, l.partial, strings.Join(cols, ", ")))
		}
	}
	slices.Sort(out)
	return out
}

// queryStrings returns the single text column query selects, in order.
func queryStrings(t *testing.T, database *DB, query string, args ...any) []string {
	t.Helper()
	rows, err := database.Query(query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scanning %s: %v", query, err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating %s: %v", query, err)
	}
	return out
}

// countOf returns the integer a COUNT(*) query selects.
func countOf(t *testing.T, database *DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := database.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// freshIndexSet returns the index set of a database created fresh at the
// current schema.
func freshIndexSet(t *testing.T) []string {
	t.Helper()
	fresh := openScratchDB(t)
	if err := fresh.CreateSchema(); err != nil {
		t.Fatalf("creating the fresh schema: %v", err)
	}
	if version, err := fresh.GetSchemaVersion(); err != nil || version != "1.17.0" {
		t.Fatalf("a fresh database reports schema_version %q (%v), want 1.17.0", version, err)
	}
	return fullIndexSetOf(t, fresh)
}

const (
	migTS1160SprintCreated = "2026-09-01T08:00:00.000Z"
	migTS1160Started       = "2026-09-02T09:30:00.000Z"
)

// buildRoadmapAtSchema1160 creates a real on-disk roadmap under the test HOME,
// populates every table — three sprints, two of them tying on created_at, the
// column of the dropped index — and takes it back to schema 1.16.0. Schema
// 1.17.0 differs from 1.16.0 by the dropped index alone, so a current database
// with that index restored and its schema_version reset is a faithful 1.16.0
// database. It returns with the database CLOSED.
func buildRoadmapAtSchema1160(t *testing.T, roadmapName string) {
	t.Helper()

	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("creating roadmap %q: %v", roadmapName, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	sprints := make([]int, 0, 3)
	for _, s := range []struct{ title, created string }{
		{"Chargeback automation", migTS1160SprintCreated},
		{"Dispute evidence portal", migTS1160SprintCreated},
		{"Fraud rule tuning", "2026-09-08T08:00:00.000Z"},
	} {
		sprints = append(sprints, mustSeedSprint(t, database, &models.Sprint{
			Title: s.title, Description: "Cut the manual work of the disputes team in half.",
			Status: models.SprintPending, CreatedAt: s.created,
		}))
	}
	tasks := make([]int, 0, 5)
	for i, title := range []string{
		"Ingest chargeback notifications from the acquirer",
		"Match a chargeback to its original authorisation",
		"Assemble the evidence bundle for a disputed payment",
		"Submit evidence before the scheme deadline",
		"Report the monthly dispute win rate",
	} {
		tasks = append(tasks, mustSeedTask(t, database, &models.Task{
			Title: title, Type: models.TypeTask, Status: models.StatusBacklog, Priority: 5 - i%3, Severity: i % 2,
			FunctionalRequirements: "Every chargeback is answered before the scheme deadline.",
			TechnicalRequirements:  "Read the acquirer feed and the authorisation log.",
			AcceptanceCriteria:     "No chargeback is lost for want of evidence.",
			CreatedAt:              migTS1160SprintCreated,
		}))
	}
	if err := database.AddTasksToSprint(testContext(), sprints[0], tasks[:3]); err != nil {
		t.Fatalf("adding tasks to the sprint: %v", err)
	}
	if err := database.AddTaskDependencyWithAudit(testContext(), tasks[3], tasks[1]); err != nil {
		t.Fatalf("adding a dependency: %v", err)
	}
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{`UPDATE tasks SET status = 'DOING', started_at = ? WHERE id = ?`, []any{migTS1160Started, tasks[0]}},
		{`INSERT INTO task_comments (task_id, type, body, created_at) VALUES (?, 'DECISION', ?, ?)`,
			[]any{tasks[0], "Evidence is assembled from the authorisation log, not the settlement file.", migTS1160Started}},
		{`INSERT INTO sprint_comments (sprint_id, type, body, created_at) VALUES (?, 'PROGRESS', ?, ?)`,
			[]any{sprints[0], "Notification ingestion is live in staging.", migTS1160Started}},
	} {
		if _, err := database.Exec(stmt.query, stmt.args...); err != nil {
			t.Fatalf("%s: %v", stmt.query, err)
		}
	}

	restoreSprintsCreatedAtIndex(t, database)
	if _, err := database.Exec(`UPDATE _metadata SET value = '1.16.0' WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("resetting schema_version to 1.16.0: %v", err)
	}
}

// TestMigrateV1_16_0_toV1_17_0_OnNextOpen settles criteria 1 to 6 on a populated
// 1.16.0 roadmap opened through the production entry point.
func TestMigrateV1_16_0_toV1_17_0_OnNextOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const roadmapName = "dispute-handling"
	buildRoadmapAtSchema1160(t, roadmapName)

	// The 1.16.0 state, read before the migration runs.
	pre := openRawRoadmap(t, roadmapName)
	if n := countOf(t, pre, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_sprints_created_at'`); n != 1 {
		t.Fatalf("the 1.16.0 fixture holds %d idx_sprints_created_at, want 1; it tests nothing", n)
	}
	want := freshIndexSet(t)
	indexesBefore := fullIndexSetOf(t, pre)
	if slices.Equal(indexesBefore, want) {
		t.Fatal("the 1.16.0 fixture already carries the 1.17.0 index set; it tests nothing")
	}
	rowsBefore := tableContents(t, pre)
	sprintOrderBefore := queryStrings(t, pre, `SELECT id || ':' || created_at FROM sprints ORDER BY created_at, id`)
	pre.Close() //nolint:errcheck // reopened through Open below

	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("opening the 1.16.0 roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	// Criterion 1.
	if version, err := database.GetSchemaVersion(); err != nil || version != "1.17.0" {
		t.Fatalf("schema_version after open = %q (%v), want 1.17.0", version, err)
	}

	// Criterion 2: the same indexes, columns, order and direction as a fresh database.
	if got := fullIndexSetOf(t, database); !slices.Equal(got, want) {
		t.Errorf("after the migration the database carries\n  %s\na fresh database carries\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}

	// Criterion 3.
	if n := countOf(t, database, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_sprints_created_at'`); n != 0 {
		t.Errorf("idx_sprints_created_at survives the migration (%d)", n)
	}

	// Criterion 4: every row unchanged, apart from the version the migration records;
	// a read in created_at order returns the same rows in the same order.
	rowsAfter := tableContents(t, database)
	if strings.Replace(rowsBefore, "1.16.0", "1.17.0", 1) != rowsAfter {
		t.Errorf("the migration changed rows other than schema_version.\nbefore:\n%s\nafter:\n%s", rowsBefore, rowsAfter)
	}
	if after := queryStrings(t, database, `SELECT id || ':' || created_at FROM sprints ORDER BY created_at, id`); !slices.Equal(after, sprintOrderBefore) {
		t.Errorf("sprints in created_at order: before %v, after %v", sprintOrderBefore, after)
	}

	// Criterion 5.
	if n := countOf(t, database, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger'`); n != 0 {
		t.Errorf("the migration left %d trigger(s), want none", n)
	}

	// Criterion 6: re-applying the migration, then opening a second time, applies
	// nothing, changes no row and no index, and raises no error.
	indexesOnce := fullIndexSetOf(t, database)
	if err := database.runMigration(Migration{Version: "1.17.0", Name: "re-apply", Apply: migrateV1_16_0_toV1_17_0}); err != nil {
		t.Fatalf("re-applying the migration: %v", err)
	}
	database.Close() //nolint:errcheck // reopened below
	again, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("opening a second time: %v", err)
	}
	defer again.Close() //nolint:errcheck // test cleanup
	if twice := tableContents(t, again); twice != rowsAfter {
		t.Errorf("a second open changed rows.\nonce:\n%s\ntwice:\n%s", rowsAfter, twice)
	}
	if got := fullIndexSetOf(t, again); !slices.Equal(got, indexesOnce) {
		t.Errorf("a second open changed the index set: %v, then %v", indexesOnce, got)
	}
}

// TestMigrateV1_16_0_toV1_17_0_FromEarlierVersions settles criteria 1 to 3 and 5
// on databases created at earlier versions: the 1.14.0 fixture, migrated through
// RunMigrations, and the 1.15.0 fixture, migrated by the production Open, both
// end at 1.17.0 with the index set of a fresh database on every table.
func TestMigrateV1_16_0_toV1_17_0_FromEarlierVersions(t *testing.T) {
	want := freshIndexSet(t)

	check := func(t *testing.T, database *DB) {
		t.Helper()
		if version, err := database.GetSchemaVersion(); err != nil || version != "1.17.0" {
			t.Fatalf("schema_version after the migrations = %q (%v), want 1.17.0", version, err)
		}
		if got := fullIndexSetOf(t, database); !slices.Equal(got, want) {
			t.Errorf("the migrated database carries\n  %s\na fresh database carries\n  %s",
				strings.Join(got, "\n  "), strings.Join(want, "\n  "))
		}
		if n := countOf(t, database, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger'`); n != 0 {
			t.Errorf("the migrations left %d trigger(s), want none", n)
		}
	}

	t.Run("1.14.0", func(t *testing.T) {
		database := buildRoadmapAtSchema1140(t)
		if n := countOf(t, database, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_sprints_created_at'`); n != 1 {
			t.Fatalf("the 1.14.0 fixture holds %d idx_sprints_created_at, want 1", n)
		}
		if err := database.RunMigrations(); err != nil {
			t.Fatalf("running the migrations: %v", err)
		}
		check(t, database)
	})

	t.Run("1.15.0", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		const roadmapName = "acquirer-failover-index-set"
		buildRoadmapAtSchema1150(t, roadmapName)
		pre := openRawRoadmap(t, roadmapName)
		if n := countOf(t, pre, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_sprints_created_at'`); n != 1 {
			t.Fatalf("the 1.15.0 fixture holds %d idx_sprints_created_at, want 1", n)
		}
		pre.Close() //nolint:errcheck // reopened through Open below
		database, err := Open(roadmapName)
		if err != nil {
			t.Fatalf("opening the 1.15.0 roadmap: %v", err)
		}
		defer database.Close() //nolint:errcheck // test cleanup
		check(t, database)
	})
}

// TestMigrateV1_16_0_toV1_17_0_FailureRollsBack settles criterion 7: a failure
// after the drop has run leaves the version at 1.16.0, the index set the
// database held before, and every row.
func TestMigrateV1_16_0_toV1_17_0_FailureRollsBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const roadmapName = "dispute-handling-rollback"
	buildRoadmapAtSchema1160(t, roadmapName)

	rw := openRawRoadmap(t, roadmapName)
	indexesBefore := fullIndexSetOf(t, rw)
	rowsBefore := tableContents(t, rw)

	injected := errors.New("injected failure after the last step")
	err := rw.runMigration(Migration{Version: "1.17.0", Name: "fails after its last step",
		Apply: func(tx *sql.Tx) error {
			if err := migrateV1_16_0_toV1_17_0(tx); err != nil {
				return err
			}
			return injected
		}})
	if !errors.Is(err, injected) {
		t.Fatalf("runMigration returned %v, want the injected failure", err)
	}
	if version, err := rw.GetSchemaVersion(); err != nil || version != "1.16.0" {
		t.Errorf("schema_version after a failed migration = %q (%v), want 1.16.0", version, err)
	}
	if got := fullIndexSetOf(t, rw); !slices.Equal(got, indexesBefore) {
		t.Errorf("a failed migration left the index set\n  %v\nwant the one held before\n  %v", got, indexesBefore)
	}
	if after := tableContents(t, rw); after != rowsBefore {
		t.Errorf("a failed migration changed rows.\nbefore:\n%s\nafter:\n%s", rowsBefore, after)
	}
}

// TestMigrateV1_16_0_toV1_17_0_IsRegistered pins the registration: the newest
// migration targets 1.17.0 and is this one.
func TestMigrateV1_16_0_toV1_17_0_IsRegistered(t *testing.T) {
	last := migrations[len(migrations)-1]
	if last.Version != "1.17.0" || fmt.Sprintf("%p", last.Apply) != fmt.Sprintf("%p", migrateV1_16_0_toV1_17_0) {
		t.Errorf("the newest migration is %s (%s), want 1.17.0 applied by migrateV1_16_0_toV1_17_0", last.Version, last.Name)
	}
}
