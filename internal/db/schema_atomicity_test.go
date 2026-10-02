package db

import (
	"slices"
	"strings"
	"testing"
)

// The tests in this file settle SPEC/DATABASE.md § Transactional Atomicity
// Guarantees, item 10: CreateSchema creates every table, every index and the
// three _metadata rows in one transaction, so a failure leaves nothing behind.

// sqliteMasterNames returns the name of every object in sqlite_master.
func sqliteMasterNames(t *testing.T, database *DB) []string {
	t.Helper()
	rows, err := database.Query(`SELECT type || ' ' || name FROM sqlite_master ORDER BY type, name`)
	if err != nil {
		t.Fatalf("reading sqlite_master: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning sqlite_master: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating sqlite_master: %v", err)
	}
	return names
}

// TestCreateSchemaIsSchemaIdenticalToStatementByStatementCreation requires the
// transactional creation to produce exactly the schema, and the metadata keys,
// that executing the same DDL one statement at a time outside a transaction
// produces.
func TestCreateSchemaIsSchemaIdenticalToStatementByStatementCreation(t *testing.T) {
	transactional := openScratchDB(t)
	if err := transactional.CreateSchema(); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}

	reference := openScratchDB(t)
	for _, ddl := range schemaDDL() {
		if _, err := reference.Exec(ddl); err != nil {
			t.Fatalf("executing the DDL statement by statement: %v", err)
		}
	}

	if got, want := schemaSnapshot(t, transactional.DB), schemaSnapshot(t, reference.DB); got != want {
		t.Errorf("the transactional schema differs from the statement-by-statement one.\ngot:\n%s\nwant:\n%s", got, want)
	}

	rows, err := transactional.Query(`SELECT key, value FROM _metadata ORDER BY key`)
	if err != nil {
		t.Fatalf("reading _metadata: %v", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			t.Fatalf("scanning _metadata: %v", err)
		}
		keys = append(keys, key)
		if key == "schema_version" && value != SchemaVersion {
			t.Errorf("schema_version = %q, want %q", value, SchemaVersion)
		}
		if key == "application" && value != "Groadmap" {
			t.Errorf("application = %q, want Groadmap", value)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating _metadata: %v", err)
	}
	if want := []string{"application", "created_at", "schema_version"}; !slices.Equal(keys, want) {
		t.Errorf("_metadata keys = %v, want %v", keys, want)
	}
}

// TestCreateSchemaFailureLeavesNoPartialSchema makes the creation fail at three
// points — midway through the DDL, after the last DDL statement, and while the
// metadata rows are inserted — and requires each failure to leave the database
// with no table, no index, no trigger and no _metadata row.
func TestCreateSchemaFailureLeavesNoPartialSchema(t *testing.T) {
	ddl := schemaDDL()
	const broken = `CREATE TABLE settlement_batches (id INTEGER PRIMARY KEY, amount DECIMAL(`
	// The trigger is created by the last statement and refuses every metadata
	// insert, so the failure happens in insertMetadataTx, after the whole DDL
	// has run inside the transaction.
	const rejectMetadata = `CREATE TRIGGER reject_metadata BEFORE INSERT ON _metadata
		BEGIN SELECT RAISE(ABORT, 'metadata refused'); END`

	cases := []struct {
		name       string
		statements []string
		wantErr    string
	}{
		{"midway through the DDL", append(slices.Clone(ddl[:3]), broken), "executing schema DDL"},
		{"after the last DDL statement", append(slices.Clone(ddl), broken), "executing schema DDL"},
		{"while inserting the metadata", append(slices.Clone(ddl), rejectMetadata), "inserting metadata"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			database := openScratchDB(t)
			err := database.createSchema(c.statements)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("createSchema returned %v, want a %q failure", err, c.wantErr)
			}
			if names := sqliteMasterNames(t, database); len(names) != 0 {
				t.Errorf("a failed creation left %d schema objects behind: %v", len(names), names)
			}
			if _, err := database.GetSchemaVersion(); err == nil {
				t.Error("a failed creation left a schema_version behind")
			}

			// The database is left as if nothing had run: the real creation
			// still succeeds on it.
			if err := database.CreateSchema(); err != nil {
				t.Fatalf("CreateSchema after a failed creation: %v", err)
			}
		})
	}
}
