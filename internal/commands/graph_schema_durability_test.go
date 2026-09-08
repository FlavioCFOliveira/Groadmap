// Regression fence for the durability of a graph's registered schema across a
// checkpoint, and for the schema a reader reports afterwards
// (SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process;
// § Engine Constructor by Path; § Recovered Schema on Every Surface; acceptance
// criteria 63 and 64).
//
// # The defect these tests close
//
// The checkpoint wrote the snapshot with a writer that persists no schema at all
// and then truncated the write-ahead log, which was where the CREATE INDEX and
// CREATE CONSTRAINT records lived. A single checkpoint was therefore enough to
// erase every index and every constraint the graph carried — not to hide them
// from the wrong engine, but to remove them from disk, leaving even the correct
// constructor nothing to recover. For a UNIQUE constraint the loss is silent and
// costs integrity: the constraint stops being enforced while the data it was
// declared to protect is still there.
//
// # Which checkpoint runs now, and why these tests changed shape rather than home
//
// The defect used to be reachable through a short-lived invocation, because every
// successful write checkpointed. No invocation checkpoints now: the server opens
// the store, and its SHUTDOWN checkpoint is what folds the log into the snapshot
// and truncates it (internal/graphserve, shutdownCloser.Close). The hazard is
// unchanged and so is the remedy — the checkpointer must be constructed with the
// engine's registered index and constraint specifications, or it writes a
// schemaless snapshot over a schema-carrying log and truncates behind it — but
// the thing that has to run for the hazard to be reachable is a server stopping.
//
// These tests therefore stop a server where they used to call a checkpoint, and
// they stay in this package because what they drive end to end is the CLI pair:
// `rmp graph client` writes the schema and reads it back, and `rmp graph serve`
// is what checkpoints in between. internal/graphserve's own suite asserts the
// server's lifecycle; nothing there asserts what its checkpoint carries, and
// nothing else in the tree does either.
//
// # The boundary every assertion is made across
//
// Asserting inside the process that created the schema establishes nothing: an
// implementation whose snapshot carries no schema passes that check and loses the
// object at the boundary. Every assertion below is made after a server has
// stopped — against the store on disk, or against a NEW server that opened it —
// so what is inspected is what survived.
package commands

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/GoGraph/store/recovery"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
)

// statementsThroughAServer runs every statement through `rmp graph client`
// against a server it starts, and then stops that server — which is what takes
// the shutdown checkpoint that folds the write-ahead log into the snapshot and
// truncates it.
//
// The stop is the point of the helper. A test that left the server running would
// be asserting about a graph held in a live process's memory; what these tests
// are about is what a checkpoint wrote to disk, and the store is not on disk in
// its folded form until the server that owns it has stopped.
func statementsThroughAServer(t *testing.T, roadmap string, statements ...string) {
	t.Helper()

	stop := serveGraph(t, roadmap)
	for _, statement := range statements {
		var err error
		captureStdStreams(t, func() {
			err = runGraphClient([]string{"-r", roadmap, "--query", statement})
		})
		if err != nil {
			stop()
			t.Fatalf("%q was refused: %v", statement, err)
		}
	}
	stop()
}

// requireFoldedLog is the non-vacuity assertion every test here needs.
//
// The shutdown checkpoint truncates the write-ahead log, so after it the log
// cannot be what carries a definition; anything recovered afterwards came from
// the snapshot. Without this assertion a change that stopped truncating would
// turn every assertion that follows into a statement about the log tail, and
// nothing would say so.
func requireFoldedLog(t *testing.T, graphDir string) {
	t.Helper()
	if size := fileSize(t, filepath.Join(graphDir, "wal")); size != 0 {
		t.Fatalf("the write-ahead log holds %d bytes after the server stopped; it was expected to be "+
			"folded and truncated, so what recovery finds below would no longer prove the SNAPSHOT "+
			"carries the schema (SPEC/GRAPH.md § Server Shutdown and the Drain, steps 4 and 5)", size)
	}
}

// reopenGraphStore reopens the store from disk, exactly as the next
// `rmp graph serve` would. Everything the stopped server held in memory is gone;
// what comes back is what is on disk.
//
// It is safe to open directly here for one reason and only that reason: no server
// is running. Every caller has stopped one first, which is also what released the
// store's exclusive advisory lock.
func reopenGraphStore(t *testing.T, graphDir string) recovery.Result[string, float64] {
	t.Helper()
	res, err := recovery.Open[string, float64](graphDir, graphstore.RecoveryOptions())
	if err != nil {
		t.Fatalf("reopening the graph store at %s: %v", graphDir, err)
	}
	return res
}

// recoveredSchemaEngine builds the engine a reader builds: the recovered graph
// plus the recovered schema, and no store and no write-ahead-log writer.
func recoveredSchemaEngine(res recovery.Result[string, float64]) *cypher.Engine {
	return cypher.NewEngineWithOptions(res.Graph, cypher.EngineOptions{
		RecoveredConstraints: cypher.ConstraintDefsFromRecovery(res.Constraints),
		RecoveredIndexes:     cypher.IndexDefsFromRecovery(res.Indexes),
	})
}

// schemaNames runs a SHOW statement against an engine and returns the value of
// its `name` column for every row, in the order the engine reported them.
//
// The cells pass through serializeValue, the CLI's own value mapping, so a name
// this reads is the string the command would print for it
// (SPEC/DATA_FORMATS.md § One Realisation of the Mapping).
func schemaNames(t *testing.T, engine *cypher.Engine, query string) []string {
	t.Helper()

	result, err := engine.Run(context.Background(), query, nil)
	if err != nil {
		t.Fatalf("%q failed: %v", query, err)
	}
	defer result.Close() //nolint:errcheck // the assertions are made on the rows already read

	columns := result.Columns()
	nameCol := -1
	for i, column := range columns {
		if column == "name" {
			nameCol = i
			break
		}
	}
	if nameCol < 0 {
		t.Fatalf("%q returned columns %v, which do not include `name`; the engine's schema listing "+
			"has changed shape and these tests can no longer read it", query, columns)
	}

	var names []string
	for result.Next() {
		raw := result.Record()[columns[nameCol]]
		cell := raw
		if v, ok := raw.(expr.Value); ok {
			cell = serializeValue(v)
		}
		name, ok := cell.(string)
		if !ok {
			t.Fatalf("%q returned a non-string in the `name` column: %#v", query, cell)
		}
		names = append(names, name)
	}
	if err := result.Err(); err != nil {
		t.Fatalf("walking the result of %q: %v", query, err)
	}
	return names
}

// containsName reports whether names holds want.
func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// TestCheckpointPreservesIndexDefinition is the criterion-63 fence for an
// index: a definition created before a checkpoint is still on disk after it.
//
// It fails against a checkpointer built without the engine's index
// specifications: the snapshot then carries no index definitions, the truncation
// that follows destroys the CREATE INDEX record in the log, and the reopen below
// reports no index at all.
func TestCheckpointPreservesIndexDefinition(t *testing.T) {
	const roadmap = "graph-schema-checkpoint-index"
	defer setupTestGraphRoadmap(t, roadmap)()

	statementsThroughAServer(t, roadmap,
		"CREATE (:Spec {key:'user-authentication'})",
		"CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")

	graphDir := testGraphDir(t, roadmap)
	requireFoldedLog(t, graphDir)

	res := reopenGraphStore(t, graphDir)

	// What recovery finds on disk. This is the assertion a schemaless snapshot
	// fails.
	recovered := make([]string, 0, len(res.Indexes))
	for _, record := range res.Indexes {
		recovered = append(recovered, record.Name)
	}
	if !containsName(recovered, "spec_key") {
		t.Fatalf("after a shutdown checkpoint and a reopen, recovery reports the indexes %v, which do "+
			"not include `spec_key`. The definition was created before the checkpoint and is now gone "+
			"from disk: the snapshot did not carry it and the truncation destroyed the log record "+
			"(SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process)", recovered)
	}

	// And what an engine built from that recovery reports, which is what a user
	// sees. Both halves matter: the first says the bytes survived, this one says
	// the engine registers them again.
	listed := schemaNames(t, recoveredSchemaEngine(res), "SHOW INDEXES")
	if !containsName(listed, "spec_key") {
		t.Errorf("SHOW INDEXES on the reopened store reported %v, which does not include `spec_key`", listed)
	}
}

// TestCheckpointPreservesConstraintEnforcement is the criterion-63 fence for a
// constraint, and it asserts ENFORCEMENT rather than presence.
//
// The distinction is the whole point. A constraint that is merely listed is not
// a constraint that is applied, and the failure mode this guards against is
// silent: against an implementation whose checkpoint dropped the constraint, the
// duplicate write below exits 0 reporting {"ok": true} and the duplicate is
// stored. Nothing errors; the graph simply stops being what it was declared to
// be.
//
// The enforcement half is driven against a SECOND server, started over the store
// the first one left, which is what makes it a statement about what survived
// rather than about what the first server still had registered in memory.
func TestCheckpointPreservesConstraintEnforcement(t *testing.T) {
	const roadmap = "graph-schema-checkpoint-constraint"
	defer setupTestGraphRoadmap(t, roadmap)()

	statementsThroughAServer(t, roadmap,
		"CREATE (:Spec {key:'user-authentication'})",
		"CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE")

	graphDir := testGraphDir(t, roadmap)
	requireFoldedLog(t, graphDir)

	res := reopenGraphStore(t, graphDir)
	recovered := make([]string, 0, len(res.Constraints))
	for _, record := range res.Constraints {
		recovered = append(recovered, record.Name)
	}
	if !containsName(recovered, "spec_key_uq") {
		t.Fatalf("after a shutdown checkpoint and a reopen, recovery reports the constraints %v, which "+
			"do not include `spec_key_uq`. The declaration was made before the checkpoint and is now "+
			"gone from disk", recovered)
	}

	// The declared name, not one the engine synthesised for a constraint it
	// found in the store without being told what it was called: a synthesised
	// name is one no DROP CONSTRAINT the caller writes can name.
	listed := schemaNames(t, recoveredSchemaEngine(res), "SHOW CONSTRAINTS")
	if !containsName(listed, "spec_key_uq") {
		t.Errorf("SHOW CONSTRAINTS on the reopened store reported %v, which does not include the "+
			"declared name `spec_key_uq`", listed)
	}

	// Enforcement, through the real command, against a server that recovered the
	// constraint rather than one that registered it: the duplicate MUST be
	// refused.
	defer serveGraph(t, roadmap)()

	var writeErr error
	stdout, _ := captureStdStreams(t, func() {
		writeErr = runGraphClient([]string{"-r", roadmap, "--query",
			"CREATE (:Spec {key:'user-authentication'})"})
	})
	if writeErr == nil {
		t.Errorf("creating a second Spec with the key `user-authentication` succeeded (stdout %q) "+
			"after a UNIQUE constraint over Spec.key survived a checkpoint. The constraint is listed "+
			"but not applied, which is the silent integrity loss criterion 63 exists to catch",
			stdout)
	}

	// And the graph must still hold one such node, not two. The refusal above
	// would be worth little if the write had partly landed.
	if got := specKeyCount(t, roadmap, "user-authentication"); got != 1 {
		t.Errorf("the graph holds %d Spec nodes keyed `user-authentication`; exactly 1 is expected, "+
			"because the constraint refused the second", got)
	}
}

// specKeyCount reads back, through `rmp graph client`, how many Spec nodes carry
// key. A server must already be running for the roadmap.
func specKeyCount(t *testing.T, roadmap, key string) int {
	t.Helper()

	var readErr error
	stdout, _ := captureStdStreams(t, func() {
		readErr = runGraphClient([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec) WHERE s.key = '" + key + "' RETURN count(s)"})
	})
	if readErr != nil {
		t.Fatalf("counting Spec nodes keyed %q: %v", key, readErr)
	}

	var out graphQueryResult
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("decoding the count result %q: %v", stdout, err)
	}
	if len(out.Rows) != 1 || len(out.Rows[0]) != 1 {
		t.Fatalf("the count query returned %d rows: %q", len(out.Rows), stdout)
	}
	count, ok := out.Rows[0][0].(float64)
	if !ok {
		t.Fatalf("the count query returned a non-numeric value: %#v", out.Rows[0][0])
	}
	return int(count)
}

// TestReadPathReportsRecoveredSchema is the criterion-64 fence: a reader reports
// the schema the store actually holds.
//
// The control is what gives the test its teeth. `cypher.NewEngine` — the
// constructor given the graph alone — answers the identical query on the
// identical store with zero rows and no error, so an exit code proves nothing
// here and the ROWS must be compared. The test asserts both sides: a server
// started over the recovered store reports the objects through the client, and
// the plain constructor on the same store reports nothing, which is what makes
// the first assertion a statement about the constructor rather than about the
// store.
//
// The subcommand table that used to sit here is gone with the subcommands it
// named. It held two entries, `query` and `search`, which had already collapsed
// onto one function: a table whose rows all call the same thing tests the same
// path twice and reports it as two. There is one statement-running subcommand.
func TestReadPathReportsRecoveredSchema(t *testing.T) {
	const roadmap = "graph-schema-read-path"
	defer setupTestGraphRoadmap(t, roadmap)()

	statementsThroughAServer(t, roadmap,
		"CREATE (:Spec {key:'user-authentication'})",
		"CREATE INDEX spec_key FOR (n:Spec) ON (n.key)",
		"CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE")

	graphDir := testGraphDir(t, roadmap)
	requireFoldedLog(t, graphDir)

	// The control, first and while nothing holds the store, so a store that
	// somehow held no schema at all could not be mistaken for a correct read
	// path further down.
	res := reopenGraphStore(t, graphDir)
	if names := schemaNames(t, cypher.NewEngine(res.Graph), "SHOW INDEXES"); len(names) != 0 {
		t.Errorf("cypher.NewEngine, given the graph alone, reports %v for SHOW INDEXES on a store "+
			"that holds `spec_key`. This test relies on that constructor reporting NOTHING: it is "+
			"the control that makes the assertions below statements about the constructor the "+
			"server builds rather than about the store. The engine has changed, and until this is "+
			"reconciled the rest of this test distinguishes a reader carrying the recovered schema "+
			"from one that does not only by accident", names)
	}

	defer serveGraph(t, roadmap)()

	if names := readSchemaNames(t, roadmap, "SHOW INDEXES"); !containsName(names, "spec_key") {
		t.Errorf("`rmp graph client --query \"SHOW INDEXES\"` reported %v, which does not include "+
			"`spec_key`. Zero rows and exit 0 is exactly what a reader constructed without the "+
			"recovered schema returns (SPEC/GRAPH.md § Recovered Schema on Every Surface)", names)
	}
	if names := readSchemaNames(t, roadmap, "SHOW CONSTRAINTS"); !containsName(names, "spec_key_uq") {
		t.Errorf("`rmp graph client --query \"SHOW CONSTRAINTS\"` reported %v, which does not include "+
			"the declared name `spec_key_uq`", names)
	}
}

// readSchemaNames drives `rmp graph client` with a SHOW statement and returns the
// `name` column of every row it printed. A server must already be running for the
// roadmap.
func readSchemaNames(t *testing.T, roadmap, query string) []string {
	t.Helper()

	var runErr error
	stdout, _ := captureStdStreams(t, func() {
		runErr = runGraphClient([]string{"-r", roadmap, "--query", query})
	})
	if runErr != nil {
		t.Fatalf("%q failed: %v", query, runErr)
	}

	var out graphQueryResult
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("decoding the result of %q (%q): %v", query, stdout, err)
	}

	nameCol := -1
	for i, column := range out.Columns {
		if column == "name" {
			nameCol = i
			break
		}
	}
	if nameCol < 0 {
		t.Fatalf("%q returned columns %v, which do not include `name`", query, out.Columns)
	}

	names := make([]string, 0, len(out.Rows))
	for _, row := range out.Rows {
		name, ok := row[nameCol].(string)
		if !ok {
			t.Fatalf("%q returned a non-string in the `name` column: %#v", query, row[nameCol])
		}
		names = append(names, name)
	}
	return names
}

// TestOrdinaryWriteCheckpointPreservesSchema is the end of the chain, and it
// covers the case that will be the common one by far: a plain data write, issued
// by a caller that knows nothing about the schema, is folded by a checkpoint over
// a graph that carries one.
//
// The two tests above register a schema and then stop the server that registered
// it. Here the schema is registered by one server, RECOVERED by a second, and the
// second is the one whose shutdown checkpoint folds an ordinary data write. The
// specifications that checkpoint carries therefore come from what the second
// server recovered and from nowhere else, so a server whose checkpointer was
// built without them would erase an index and a constraint it never mentioned.
//
// Its assertions are made through the client rather than against recovery.Result,
// so the property asserted is the user-visible one.
func TestOrdinaryWriteCheckpointPreservesSchema(t *testing.T) {
	const roadmap = "graph-schema-survives-data-write"
	defer setupTestGraphRoadmap(t, roadmap)()

	statementsThroughAServer(t, roadmap,
		"CREATE (:Spec {key:'user-authentication'})",
		"CREATE INDEX spec_key FOR (n:Spec) ON (n.key)",
		"CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE")

	graphDir := testGraphDir(t, roadmap)
	requireFoldedLog(t, graphDir)

	// A second server, and a plain data write through it. Its shutdown
	// checkpoint rewrites the snapshot and truncates the log over a graph that
	// carries a schema this server recovered rather than registered.
	statementsThroughAServer(t, roadmap, "CREATE (:Spec {key:'session-management'})")
	requireFoldedLog(t, graphDir)

	// A third, which recovered whatever the second's checkpoint left.
	defer serveGraph(t, roadmap)()

	if names := readSchemaNames(t, roadmap, "SHOW INDEXES"); !containsName(names, "spec_key") {
		t.Errorf("after an ordinary data write was folded by a shutdown checkpoint, SHOW INDEXES "+
			"reports %v, which does not include `spec_key`. A server that never mentioned the index "+
			"erased it", names)
	}
	if names := readSchemaNames(t, roadmap, "SHOW CONSTRAINTS"); !containsName(names, "spec_key_uq") {
		t.Errorf("after an ordinary data write was folded by a shutdown checkpoint, SHOW CONSTRAINTS "+
			"reports %v, which does not include `spec_key_uq`", names)
	}

	// Enforcement again, because a listed constraint is not an applied one, and
	// because this is the path on which the loss would be silent.
	var writeErr error
	stdout, _ := captureStdStreams(t, func() {
		writeErr = runGraphClient([]string{"-r", roadmap, "--query",
			"CREATE (:Spec {key:'session-management'})"})
	})
	if writeErr == nil {
		t.Errorf("creating a duplicate Spec keyed `session-management` succeeded (stdout %q) after a "+
			"shutdown checkpoint had folded an ordinary write over the UNIQUE constraint", stdout)
	}
	if got := specKeyCount(t, roadmap, "session-management"); got != 1 {
		t.Errorf("the graph holds %d Spec nodes keyed `session-management`; exactly 1 is expected", got)
	}
}
