package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"modernc.org/sqlite"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file is the test SPEC/VERSION.md § Migration Chain Guarantee requires:
// the binary migrates a database created at every released schema version to
// the current one, and the test suite keeps one fixture database per released
// version to prove it.
//
// # The fixtures
//
// testdata/migration_chain/schema-<version>.db holds one database for each
// released schema version, and for 1.0.0, the first version of the chain. Each
// was created by the rmp binary built from a commit whose
// internal/db/schema.go declares exactly that SchemaVersion, and was then
// populated through that same binary's own CLI, so every row is one that
// version wrote itself — the audit entries included:
//
//	1.0.0   1e0b15a (the parent of e9d817f, which introduced 1.1.0)
//	1.1.0   v1.1.0
//	1.2.0   v1.0.0  (the tag was cut after the 1.2.0 schema landed)
//	1.6.0   v1.2.0
//	1.8.0   v1.10.0
//	1.12.0  v1.15.0
//	1.14.0  v1.15.1
//	1.17.0  faeaba8 (the develop head release/1.18.0 was cut from; v1.18.0 is
//	        the first release that creates databases at 1.17.0)
//
// The data is one payments-service roadmap: nine tasks, among them a subtask
// where the version has parent_task_id, in every status — BACKLOG, SPRINT,
// DOING, TESTING and COMPLETED, with a completion summary where the version
// has one; three sprints, one CLOSED, one OPEN with a --max-tasks cap where the
// version has one, and one PENDING; two dependencies where the version has
// task_dependencies; commit hashes where the version requires them; three
// comments where the version has the comment tables; and the audit entries
// every one of those commands wrote. The fixtures are immutable: a release
// that introduces a schema version adds its own fixture, and none is ever
// edited (rule 2 of the fixture list).
//
// # What is asserted, per fixture, on a copy
//
//  1. Every pending migration runs, once, in ascending order of target
//     version, starting with the first one newer than the fixture.
//  2. Each runs in its own transaction, and the version it starts from is the
//     one the previous migration committed. A migration that fails rolls back
//     its own statements, leaves the version where the previous one put it,
//     and the next open resumes from there.
//  3. A second open applies nothing, and every migration applied again to the
//     migrated database changes nothing and raises no error.
//  4. Every row of every table survives with every value of every column the
//     current schema still has, except the reclassification of the legacy
//     TASK_STATUS_CHANGE audit entries that the 1.12.0 migration states.
//  5. The chain ends at SchemaVersion with PRAGMA integrity_check returning ok
//     and PRAGMA foreign_key_check returning no row.

// chainFixtureVersions are the schema versions a fixture exists for, in
// ascending order.
var chainFixtureVersions = []string{"1.0.0", "1.1.0", "1.2.0", "1.6.0", "1.8.0", "1.12.0", "1.14.0", "1.17.0"}

// chainRoadmap is the roadmap name a fixture copy is installed under.
const chainRoadmap = "payments-chain"

// installFixture copies the fixture of version into a fresh HOME as the
// project.db of chainRoadmap and returns the copy's path. The fixture itself
// is never opened for writing.
func installFixture(t *testing.T, version string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := utils.EnsureRoadmapDir(chainRoadmap); err != nil {
		t.Fatalf("creating the roadmap home: %v", err)
	}
	dst, err := utils.GetRoadmapPath(chainRoadmap)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join("testdata", "migration_chain", "schema-"+version+".db")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, utils.DBFilePerm); err != nil {
		t.Fatalf("installing fixture: %v", err)
	}
	return dst
}

// rawConn opens path with the driver directly, outside every Groadmap open
// path, for reading a database before and after the chain.
func rawConn(t *testing.T, path string) *sql.DB {
	t.Helper()
	connector, err := sqlite.NewConnector(dsnFor(path, true))
	if err != nil {
		t.Fatal(err)
	}
	conn := sql.OpenDB(connector)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// tableSnapshot is every row of one table, in rowid order, each row a map from
// column name to value.
type tableSnapshot struct {
	columns []string
	rows    []map[string]any
}

// snapshotDatabase reads every user table of conn.
func snapshotDatabase(t *testing.T, conn *sql.DB) map[string]tableSnapshot {
	t.Helper()
	names, err := conn.Query("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for names.Next() {
		var n string
		if err := names.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	if err := names.Close(); err != nil {
		t.Fatal(err)
	}

	out := make(map[string]tableSnapshot, len(tables))
	for _, table := range tables {
		rows, err := conn.Query(fmt.Sprintf("SELECT * FROM %q ORDER BY rowid", table)) // #nosec G201 -- table names read from sqlite_master of a test fixture
		if err != nil {
			t.Fatalf("reading %s: %v", table, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		snap := tableSnapshot{columns: cols}
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			row := make(map[string]any, len(cols))
			for i, c := range cols {
				row[c] = values[i]
			}
			snap.rows = append(snap.rows, row)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		out[table] = snap
	}
	return out
}

// statusChangeReclassified reports whether the difference in column of table
// is the one the 1.12.0 migration states: a legacy TASK_STATUS_CHANGE audit
// entry given the destination-specific operation the stored data determines.
func statusChangeReclassified(table, column string, before, after any) bool {
	if table != "audit" || column != "operation" {
		return false
	}
	b, _ := before.(string)
	a, _ := after.(string)
	return b == "TASK_STATUS_CHANGE" && strings.HasPrefix(a, "TASK_STATUS_")
}

// assertDataPreserved compares the snapshot of the fixture with the snapshot of
// the migrated copy: every table the fixture had keeps every row, in the same
// order, and every column both schemas have keeps every value, apart from the
// change statusChangeReclassified names.
func assertDataPreserved(t *testing.T, before, after map[string]tableSnapshot) {
	t.Helper()
	rowsSeen := 0
	for table, b := range before {
		if table == "_metadata" {
			continue // schema_version advances by design; the other keys are compared below
		}
		a, ok := after[table]
		if !ok {
			t.Errorf("table %s did not survive the chain", table)
			continue
		}
		if len(a.rows) != len(b.rows) {
			t.Errorf("table %s holds %d rows after the chain, want the fixture's %d", table, len(a.rows), len(b.rows))
			continue
		}
		for i := range b.rows {
			rowsSeen++
			for _, col := range b.columns {
				av, present := a.rows[i][col]
				if !present {
					continue // a column the chain drops (tasks.specialists at 1.10.0)
				}
				bv := b.rows[i][col]
				if reflect.DeepEqual(av, bv) || statusChangeReclassified(table, col, bv, av) {
					continue
				}
				t.Errorf("%s row %d column %s changed across the chain: %v -> %v", table, i, col, bv, av)
			}
		}
	}
	for _, key := range []string{"created_at", "application"} {
		if !reflect.DeepEqual(metadataValue(before, key), metadataValue(after, key)) {
			t.Errorf("_metadata.%s changed across the chain: %v -> %v", key, metadataValue(before, key), metadataValue(after, key))
		}
	}
	if rowsSeen < 50 {
		t.Errorf("only %d rows were compared; the fixture is not the realistic one this test relies on", rowsSeen)
	}
}

// metadataValue returns the value of one _metadata key in a snapshot.
func metadataValue(snap map[string]tableSnapshot, key string) any {
	for _, row := range snap["_metadata"].rows {
		if row["key"] == key {
			return row["value"]
		}
	}
	return nil
}

// stepRecord is what an instrumented migration observed when it ran.
type stepRecord struct {
	tx          *sql.Tx
	version     string
	startedFrom string
}

// instrumentMigrations replaces the package's migration list, for the length
// of the test, with wrappers that record every application and the version the
// transaction started from, and that fail at the target version failAt when it
// is not empty.
func instrumentMigrations(t *testing.T, records *[]stepRecord, failAt string) {
	t.Helper()
	original := migrations
	wrapped := make([]Migration, len(original))
	for i, m := range original {
		m := m
		wrapped[i] = Migration{
			Version: m.Version,
			Name:    m.Name,
			Apply: func(tx *sql.Tx) error {
				var from string
				if err := tx.QueryRow("SELECT value FROM _metadata WHERE key = 'schema_version'").Scan(&from); err != nil {
					return err
				}
				*records = append(*records, stepRecord{tx: tx, version: m.Version, startedFrom: from})
				if err := m.Apply(tx); err != nil {
					return err
				}
				if m.Version == failAt {
					return errInjectedMigrationFailure
				}
				return nil
			},
		}
	}
	migrations = wrapped
	t.Cleanup(func() { migrations = original })
}

// errInjectedMigrationFailure is the failure instrumentMigrations injects.
var errInjectedMigrationFailure = errors.New("injected migration failure")

// pendingFrom returns the target versions newer than version, in the order
// the package declares them.
func pendingFrom(version string) []string {
	var out []string
	for _, m := range migrations {
		if compareVersions(m.Version, version) > 0 {
			out = append(out, m.Version)
		}
	}
	return out
}

// storedVersion reads _metadata.schema_version from path.
func storedVersion(t *testing.T, path string) string {
	t.Helper()
	conn := rawConn(t, path)
	var v string
	if err := conn.QueryRow("SELECT value FROM _metadata WHERE key = 'schema_version'").Scan(&v); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	return v
}

func TestMigrationChain_FromEveryReleasedSchemaVersion(t *testing.T) {
	for _, version := range chainFixtureVersions {
		t.Run(version, func(t *testing.T) {
			path := installFixture(t, version)
			if got := storedVersion(t, path); got != version {
				t.Fatalf("fixture schema-%s.db records schema version %s", version, got)
			}
			before := snapshotDatabase(t, rawConn(t, path))

			var records []stepRecord
			instrumentMigrations(t, &records, "")

			database, err := Open(chainRoadmap)
			if err != nil {
				t.Fatalf("opening the %s fixture: %v", version, err)
			}

			// 1 and 2: strict sequence, one transaction each, each starting
			// from the version the previous one committed.
			want := pendingFrom(version)
			if len(records) != len(want) {
				t.Fatalf("%d migrations ran, want the %d pending ones %v", len(records), len(want), want)
			}
			seenTx := map[*sql.Tx]bool{}
			from := version
			for i, r := range records {
				if r.version != want[i] {
					t.Errorf("step %d applied %s, want %s", i, r.version, want[i])
				}
				if r.startedFrom != from {
					t.Errorf("migration %s started from schema version %s, want %s", r.version, r.startedFrom, from)
				}
				if seenTx[r.tx] {
					t.Errorf("migration %s shared a transaction with an earlier migration", r.version)
				}
				seenTx[r.tx] = true
				from = r.version
			}

			// The migrated schema holds exactly the tables and indexes a
			// database created at SchemaVersion holds: a migration skipped
			// because the version was recorded wrongly leaves one missing. It
			// is read before step 3 re-applies every migration, which would
			// otherwise supply what the chain left out.
			assertSchemaObjectsMatchFresh(t, database.DB)

			// 3: a second run applies nothing, and every migration applied
			// again changes nothing and raises no error.
			records = records[:0]
			if err := database.RunMigrations(); err != nil {
				t.Fatalf("second RunMigrations: %v", err)
			}
			if len(records) != 0 {
				t.Errorf("a second run applied %d migrations, want none", len(records))
			}
			settled := snapshotDatabase(t, database.DB)
			for _, m := range migrations {
				tx, err := database.Begin()
				if err != nil {
					t.Fatal(err)
				}
				if err := m.Apply(tx); err != nil {
					_ = tx.Rollback()
					t.Errorf("re-applying migration %s to the migrated database failed: %v", m.Version, err)
					continue
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			if again := snapshotDatabase(t, database.DB); !reflect.DeepEqual(settled, again) {
				t.Errorf("re-applying every migration changed the migrated database")
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}

			// 4: data preserved.
			conn := rawConn(t, path)
			after := snapshotDatabase(t, conn)
			assertDataPreserved(t, before, after)

			// 5: the chain ends correct.
			var final string
			if err := conn.QueryRow("SELECT value FROM _metadata WHERE key = 'schema_version'").Scan(&final); err != nil {
				t.Fatal(err)
			}
			if final != SchemaVersion {
				t.Errorf("schema version after the chain = %s, want %s", final, SchemaVersion)
			}
			var integrity string
			if err := conn.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
				t.Fatal(err)
			}
			if integrity != "ok" {
				t.Errorf("PRAGMA integrity_check = %q, want ok", integrity)
			}
			fk, err := conn.Query("PRAGMA foreign_key_check")
			if err != nil {
				t.Fatal(err)
			}
			if fk.Next() {
				t.Errorf("PRAGMA foreign_key_check returned a row after the chain")
			}
			_ = fk.Close()
		})
	}
}

// TestMigrationChain_AFailedStepRollsBackAndTheNextOpenResumes is rule 2 of the
// guarantee for the failure path: a migration that fails rolls back its own
// statements and leaves the version where the previous migration put it, the
// migrations already committed stay committed, and the next open resumes from
// there and completes the chain.
func TestMigrationChain_AFailedStepRollsBackAndTheNextOpenResumes(t *testing.T) {
	path := installFixture(t, "1.0.0")
	pending := pendingFrom("1.0.0")
	failAt := pending[5] // 1.6.0, which creates task_dependencies
	previous := pending[4]
	if failAt != "1.6.0" {
		t.Fatalf("the sixth pending migration from 1.0.0 is %s; this test injects its failure at 1.6.0", failAt)
	}
	base := migrations

	var records []stepRecord
	instrumentMigrations(t, &records, failAt)
	if _, err := Open(chainRoadmap); !errors.Is(err, errInjectedMigrationFailure) {
		t.Fatalf("open with a failing %s migration returned %v, want the injected failure", failAt, err)
	}
	if got := storedVersion(t, path); got != previous {
		t.Errorf("after the failed %s migration the database records %s, want %s", failAt, got, previous)
	}
	conn := rawConn(t, path)
	var tables int
	if err := conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'task_dependencies'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if tables != 0 {
		t.Errorf("the failed %s migration left its table behind", failAt)
	}

	records = records[:0]
	migrations = base
	instrumentMigrations(t, &records, "")
	database, err := Open(chainRoadmap)
	if err != nil {
		t.Fatalf("the open after the failure did not resume: %v", err)
	}
	_ = database.Close()
	if len(records) == 0 || records[0].version != failAt || records[0].startedFrom != previous {
		t.Fatalf("the next open did not resume at %s from %s: %+v", failAt, previous, records)
	}
	if got := storedVersion(t, path); got != SchemaVersion {
		t.Errorf("the resumed chain ended at %s, want %s", got, SchemaVersion)
	}
}

// schemaObjects returns the "type name" of every table and index conn holds,
// apart from SQLite's own.
func schemaObjects(t *testing.T, conn *sql.DB) []string {
	t.Helper()
	rows, err := conn.Query("SELECT type || ' ' || name FROM sqlite_master WHERE type IN ('table', 'index') AND name NOT LIKE 'sqlite_%' ORDER BY 1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close() //nolint:errcheck // test read
	var out []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			t.Fatal(err)
		}
		out = append(out, o)
	}
	return out
}

// assertSchemaObjectsMatchFresh compares the tables and indexes of a migrated
// database with those of one CreateSchema builds.
func assertSchemaObjectsMatchFresh(t *testing.T, migrated *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fresh.db")
	if err := buildDatabase(path); err != nil {
		t.Fatalf("building a fresh database: %v", err)
	}
	want := schemaObjects(t, rawConn(t, path))
	got := schemaObjects(t, migrated)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the migrated schema differs from a fresh one:\n  migrated: %v\n  fresh:    %v", got, want)
	}
}

// TestMigrateV1_0_0_toV1_1_0_KeepsAPlannedOrder is the regression test for the
// migration that re-initialised sprint_tasks.position on every application:
// applied to a database that already had the column, it replaced a sprint's
// planned order with the order its tasks were added in, which item 3 of
// SPEC/VERSION.md § Migration Chain Guarantee forbids.
func TestMigrateV1_0_0_toV1_1_0_KeepsAPlannedOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "planned.db")
	if err := buildDatabase(path); err != nil {
		t.Fatal(err)
	}
	connector, err := sqlite.NewConnector(dsnFor(path, false))
	if err != nil {
		t.Fatal(err)
	}
	conn := sql.OpenDB(connector)
	defer conn.Close() //nolint:errcheck // test handle

	const now = "2026-03-01T09:00:00.000Z"
	for _, stmt := range []string{
		`INSERT INTO sprints (id, status, title, description, created_at, order_index) VALUES (1, 'PENDING', 'Refund safety', 'Make refunds safe to retry.', '` + now + `', 1)`,
		`INSERT INTO tasks (id, title, functional_requirements, technical_requirements, acceptance_criteria, created_at) VALUES
			(1, 'Add idempotency keys to the refund endpoint', 'fr', 'tr', 'ac', '` + now + `'),
			(2, 'Retry provider webhooks with exponential backoff', 'fr', 'tr', 'ac', '` + now + `'),
			(3, 'Reconcile settlement totals daily', 'fr', 'tr', 'ac', '` + now + `')`,
		`UPDATE tasks SET status = 'SPRINT'`,
		// Added in id order, planned in the reverse order.
		`INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position) VALUES
			(1, 1, '2026-03-01T09:00:01.000Z', 2),
			(1, 2, '2026-03-01T09:00:02.000Z', 1),
			(1, 3, '2026-03-01T09:00:03.000Z', 0)`,
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}

	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateV1_0_0_toV1_1_0(tx); err != nil {
		t.Fatalf("re-applying 1.1.0: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	for task, want := range map[int]int{1: 2, 2: 1, 3: 0} {
		var got int
		if err := conn.QueryRow("SELECT position FROM sprint_tasks WHERE task_id = ?", task).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("task %d is at position %d after re-applying 1.1.0, want its planned %d", task, got, want)
		}
	}
}
