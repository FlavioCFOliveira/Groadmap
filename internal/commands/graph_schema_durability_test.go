// Regression fence for the durability of a graph's registered schema across
// the synchronous checkpoint, and for the schema the read path reports
// (SPEC/GRAPH.md § Synchronous Checkpoint on Write, step 2; § Engine
// Constructor by Path; § Recovered Schema on Both Paths; acceptance criteria
// 63 and 64).
//
// The defect these tests close. The checkpoint wrote the snapshot with a
// writer that persists no schema at all and then truncated the write-ahead
// log, which was where the CREATE INDEX and CREATE CONSTRAINT records lived.
// Every successful write checkpoints, so a single write was enough to erase
// every index and every constraint the graph carried — not to hide them from
// the wrong engine, but to remove them from disk, leaving even the correct
// constructor nothing to recover. For a UNIQUE constraint the loss is silent
// and costs integrity: the constraint stops being enforced while the data it
// was declared to protect is still there.
//
// Why these tests drive the Go entry points rather than the compiled binary.
// The guard rail still refuses every DDL clause on `graph update`, so no `rmp`
// invocation can create an index yet; widening it is a separate task, and the
// end-to-end coverage of criteria 63 and 64 belongs with it. What is testable
// now — and what the defect actually is — lives below the guard rail: the
// checkpoint, the constructors, and what recovery finds afterwards. The write
// sequence used here is assembled from the same entry points runGraphWrite
// uses, in the same order, so a change to that sequence that broke this
// property would break these tests too.
//
// Each test crosses a process-equivalent boundary. Asserting inside the
// invocation that created the schema establishes nothing: an implementation
// whose snapshot carries no schema passes that check and loses the object at
// the boundary. Every assertion below is therefore made against a store the
// checkpoint has already truncated the log of, reopened from scratch.
package commands

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/store/recovery"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
)

// runSchemaStatement executes one statement through the production store
// sequence — graphstore.Open (the advisory hold, recovery, the write-ahead-log
// writer, the transactional store and cypher.NewEngineWithStoreAndRecovery),
// RunInTx, the commit performed by Result.Close, and finally
// graphstore.Store.Checkpoint — and returns the engine's error, or nil.
//
// It bypasses only the guard rail, which today refuses the DDL these tests
// need and is widened by a separate task. Everything after the guard rail is
// the production sequence, and the checkpoint is the production method: a
// test that reimplemented the checkpoint would prove nothing about the one the
// command runs. That is why this helper was rewired rather than kept when the
// sequence moved into internal/graphstore (rmp task #375); a copy left here
// would have been a third implementation of exactly what that task removed.
//
// An engine refusal (a constraint violation, a duplicate index name) is
// returned so the caller can assert on it. An infrastructure failure — the
// store not opening, the checkpoint failing or declining to run — fails the
// test, because it means the harness, not the behaviour under test, is broken.
func runSchemaStatement(t *testing.T, graphDir, query string) error {
	t.Helper()

	st, err := graphstore.Open(graphDir)
	if err != nil {
		t.Fatalf("opening the graph store at %s: %v", graphDir, err)
	}
	defer st.Close() //nolint:errcheck // test cleanup; the assertions are made against the reopened store

	result, runErr := st.Engine().RunInTx(context.Background(), query, nil)
	if runErr != nil {
		return runErr
	}
	// Draining the result is what allows Close to commit.
	for result.Next() {
	}
	if iterErr := result.Err(); iterErr != nil {
		_ = result.Close() //nolint:errcheck // rolling back; the commit error is moot once iteration failed
		return iterErr
	}
	// Close is the durability boundary: it applies and commits the transaction.
	if cerr := result.Close(); cerr != nil {
		return cerr
	}

	// The checkpoint under test. Its failure is non-fatal in production
	// (SPEC/GRAPH.md FR7) but is fatal here: these tests exist to assert what
	// the checkpoint persisted, and a checkpoint that did not run persisted
	// nothing to assert about. Checkpoint reports whether it ran, so "did not
	// run" is now a distinguishable outcome rather than a silent one: every
	// statement these tests pass are schema DDL, which appends, so a false here
	// means the write-ahead-log gate has stopped seeing what a write does.
	ran, cperr := st.Checkpoint()
	if cperr != nil {
		t.Fatalf("checkpoint after %q: %v", query, cperr)
	}
	if !ran {
		t.Fatalf("the checkpoint after %q declined to run: the write-ahead log did not grow, so the "+
			"statement appended nothing and there is nothing on disk for this test to assert about", query)
	}
	return nil
}

// checkpointedSchemaStatement runs a statement that is expected to succeed and
// fails the test if the engine refuses it.
func checkpointedSchemaStatement(t *testing.T, graphDir, query string) {
	t.Helper()
	if err := runSchemaStatement(t, graphDir, query); err != nil {
		t.Fatalf("%q was refused by the engine: %v", query, err)
	}
}

// reopenGraphStore reopens the store from disk, exactly as the next `rmp`
// invocation would. Everything the previous invocation left in memory is gone;
// what comes back is what is on disk.
func reopenGraphStore(t *testing.T, graphDir string) recovery.Result[string, float64] {
	t.Helper()
	res, err := recovery.Open[string, float64](graphDir, graphstore.RecoveryOptions())
	if err != nil {
		t.Fatalf("reopening the graph store at %s: %v", graphDir, err)
	}
	return res
}

// recoveredSchemaEngine builds the engine the read path builds: the recovered
// graph plus the recovered schema, and no store and no write-ahead-log writer.
func recoveredSchemaEngine(res recovery.Result[string, float64]) *cypher.Engine {
	return cypher.NewEngineWithOptions(res.Graph, cypher.EngineOptions{
		RecoveredConstraints: cypher.ConstraintDefsFromRecovery(res.Constraints),
		RecoveredIndexes:     cypher.IndexDefsFromRecovery(res.Indexes),
	})
}

// schemaNames runs a SHOW statement and returns the value of its `name` column
// for every row, in the order the engine reported them.
func schemaNames(t *testing.T, engine *cypher.Engine, query string) []string {
	t.Helper()

	result, err := engine.Run(context.Background(), query, nil)
	if err != nil {
		t.Fatalf("%q failed: %v", query, err)
	}
	defer result.Close() //nolint:errcheck

	out, err := serializeGraphResult(result)
	if err != nil {
		t.Fatalf("serialising the result of %q: %v", query, err)
	}

	nameCol := -1
	for i, column := range out.Columns {
		if column == "name" {
			nameCol = i
			break
		}
	}
	if nameCol < 0 {
		t.Fatalf("%q returned columns %v, which do not include `name`; the engine's schema listing "+
			"has changed shape and these tests can no longer read it", query, out.Columns)
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
// It fails against the writer this task replaced. With
// WriteSnapshotFullWithMapperCodec the snapshot carries no index definitions,
// the truncation that follows destroys the CREATE INDEX record in the log, and
// the reopen below reports no index at all.
func TestCheckpointPreservesIndexDefinition(t *testing.T) {
	const roadmap = "graph-schema-checkpoint-index"
	defer setupTestGraphRoadmap(t, roadmap)()

	// Seed through the ordinary write path, which creates the store directory
	// and leaves a checkpointed graph behind.
	captureStdStreams(t, func() {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"CREATE (:Spec {key:'user-authentication'})"}); err != nil {
			t.Fatalf("seeding the graph: %v", err)
		}
	})

	graphDir := testGraphDir(t, roadmap)
	walPath := filepath.Join(graphDir, "wal")

	checkpointedSchemaStatement(t, graphDir, "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")

	// Non-vacuity. The checkpoint truncates the write-ahead log, so after it
	// the log cannot be what carries the definition; anything recovered below
	// came from the snapshot. Without this assertion a change that stopped
	// truncating would turn every assertion that follows into a statement
	// about the log tail, and nothing would say so.
	if size := fileSize(t, walPath); size != 0 {
		t.Fatalf("the write-ahead log holds %d bytes after the checkpoint; it was expected to be "+
			"truncated, so what the reopen recovers below would no longer prove the SNAPSHOT carries "+
			"the schema", size)
	}

	res := reopenGraphStore(t, graphDir)

	// What recovery finds on disk. This is the assertion the old writer fails.
	recovered := make([]string, 0, len(res.Indexes))
	for _, record := range res.Indexes {
		recovered = append(recovered, record.Name)
	}
	if !containsName(recovered, "spec_key") {
		t.Fatalf("after a checkpoint and a reopen, recovery reports the indexes %v, which do not "+
			"include `spec_key`. The definition was created before the checkpoint and is now gone "+
			"from disk: the snapshot did not carry it and the truncation destroyed the log record "+
			"(SPEC/GRAPH.md § Synchronous Checkpoint on Write, step 2)", recovered)
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
// silent: against an implementation whose checkpoint dropped the constraint,
// the duplicate write below exits 0 reporting {"ok": true} and the duplicate is
// stored. Nothing errors; the graph simply stops being what it was declared to
// be.
func TestCheckpointPreservesConstraintEnforcement(t *testing.T) {
	const roadmap = "graph-schema-checkpoint-constraint"
	defer setupTestGraphRoadmap(t, roadmap)()

	captureStdStreams(t, func() {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"CREATE (:Spec {key:'user-authentication'})"}); err != nil {
			t.Fatalf("seeding the graph: %v", err)
		}
	})

	graphDir := testGraphDir(t, roadmap)
	walPath := filepath.Join(graphDir, "wal")

	checkpointedSchemaStatement(t, graphDir,
		"CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE")

	if size := fileSize(t, walPath); size != 0 {
		t.Fatalf("the write-ahead log holds %d bytes after the checkpoint; it was expected to be "+
			"truncated, so what the reopen recovers below would no longer prove the SNAPSHOT carries "+
			"the schema", size)
	}

	res := reopenGraphStore(t, graphDir)

	recovered := make([]string, 0, len(res.Constraints))
	for _, record := range res.Constraints {
		recovered = append(recovered, record.Name)
	}
	if !containsName(recovered, "spec_key_uq") {
		t.Fatalf("after a checkpoint and a reopen, recovery reports the constraints %v, which do not "+
			"include `spec_key_uq`. The declaration was made before the checkpoint and is now gone "+
			"from disk (SPEC/GRAPH.md § Synchronous Checkpoint on Write, step 2)", recovered)
	}

	// The declared name, not one the engine synthesised for a constraint it
	// found in the store without being told what it was called: a synthesised
	// name is one no DROP CONSTRAINT the caller writes can name.
	listed := schemaNames(t, recoveredSchemaEngine(res), "SHOW CONSTRAINTS")
	if !containsName(listed, "spec_key_uq") {
		t.Errorf("SHOW CONSTRAINTS on the reopened store reported %v, which does not include the "+
			"declared name `spec_key_uq`", listed)
	}

	// Enforcement, through the real command, in what is a separate invocation
	// as far as the store is concerned: the duplicate MUST be refused.
	var writeErr error
	stdout, _ := captureStdStreams(t, func() {
		writeErr = runGraphExecute([]string{"-r", roadmap, "--query",
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

// specKeyCount reads back, through the production read subcommand, how many
// Spec nodes carry key.
func specKeyCount(t *testing.T, roadmap, key string) int {
	t.Helper()

	var readErr error
	stdout, _ := captureStdStreams(t, func() {
		readErr = runGraphExecute([]string{"-r", roadmap, "--query",
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

// TestReadPathReportsRecoveredSchema is the criterion-64 fence: the read
// subcommands report the schema the store actually holds.
//
// The control is what gives the test its teeth. `cypher.NewEngine` — the
// constructor both read paths used before this task — answers the identical
// query on the identical store with zero rows and no error, so an exit code
// proves nothing here and the ROWS must be compared. The test asserts both
// sides: the production read path reports the object, and the old constructor
// on the same store reports nothing, which is what makes the first assertion a
// statement about the constructor rather than about the store.
func TestReadPathReportsRecoveredSchema(t *testing.T) {
	const roadmap = "graph-schema-read-path"
	defer setupTestGraphRoadmap(t, roadmap)()

	captureStdStreams(t, func() {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"CREATE (:Spec {key:'user-authentication'})"}); err != nil {
			t.Fatalf("seeding the graph: %v", err)
		}
	})

	graphDir := testGraphDir(t, roadmap)
	checkpointedSchemaStatement(t, graphDir, "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
	checkpointedSchemaStatement(t, graphDir,
		"CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE")

	// The control, first, so a store that somehow held no schema at all could
	// not be mistaken for a correct read path further down.
	res := reopenGraphStore(t, graphDir)
	t.Run("the plain constructor reports an empty schema", func(t *testing.T) {
		if names := schemaNames(t, cypher.NewEngine(res.Graph), "SHOW INDEXES"); len(names) != 0 {
			t.Errorf("cypher.NewEngine, given the graph alone, reports %v for SHOW INDEXES on a store "+
				"that holds `spec_key`. This test relies on that constructor reporting NOTHING: it is "+
				"the control that makes the assertions below statements about the read path's "+
				"constructor rather than about the store. The engine has changed, and until this is "+
				"reconciled the rest of this test distinguishes a read path carrying the recovered "+
				"schema from one that does not only by accident", names)
		}
	})

	for _, subcommand := range []struct {
		name string
		run  func(args []string) error
	}{
		{"query", runGraphExecute},
		{"search", runGraphExecute},
	} {
		t.Run(subcommand.name+" reports the index", func(t *testing.T) {
			names := readSchemaNames(t, subcommand.run, roadmap, "SHOW INDEXES")
			if !containsName(names, "spec_key") {
				t.Errorf("`graph %s --query \"SHOW INDEXES\"` reported %v, which does not include "+
					"`spec_key`. Zero rows and exit 0 is exactly what a read path constructed without "+
					"the recovered schema returns (SPEC/GRAPH.md § Recovered Schema on Both Paths)",
					subcommand.name, names)
			}
		})

		t.Run(subcommand.name+" reports the constraint under its declared name", func(t *testing.T) {
			names := readSchemaNames(t, subcommand.run, roadmap, "SHOW CONSTRAINTS")
			if !containsName(names, "spec_key_uq") {
				t.Errorf("`graph %s --query \"SHOW CONSTRAINTS\"` reported %v, which does not include "+
					"the declared name `spec_key_uq`", subcommand.name, names)
			}
		})
	}
}

// readSchemaNames drives a read subcommand with a SHOW statement and returns
// the `name` column of every row it printed.
func readSchemaNames(t *testing.T, run func(args []string) error, roadmap, query string) []string {
	t.Helper()

	var runErr error
	stdout, _ := captureStdStreams(t, func() {
		runErr = run([]string{"-r", roadmap, "--query", query})
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

// TestOrdinaryWriteCheckpointPreservesSchema is the end of the chain, and the
// one test here that runs entirely through production entry points.
//
// The two tests above establish that the checkpoint persists the schema when
// the statement that ran WAS the schema statement. This one asserts the case
// that will be the common one by far: a plain data write, issued by a command
// that knows nothing about the schema, checkpoints over a graph that carries
// one. `runGraphWrite` builds its own engine, so the schema that engine holds
// registered — and hands to the checkpoint — comes from the recovery result it
// was constructed with and from nowhere else. A write path constructed without
// that result would checkpoint a schema it never re-registered, and the index
// and constraint would be erased by a command that never mentioned them.
//
// Its assertions are deliberately made through the read subcommand rather than
// against recovery.Result, so the property asserted is the user-visible one.
func TestOrdinaryWriteCheckpointPreservesSchema(t *testing.T) {
	const roadmap = "graph-schema-survives-data-write"
	defer setupTestGraphRoadmap(t, roadmap)()

	captureStdStreams(t, func() {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"CREATE (:Spec {key:'user-authentication'})"}); err != nil {
			t.Fatalf("seeding the graph: %v", err)
		}
	})

	graphDir := testGraphDir(t, roadmap)
	checkpointedSchemaStatement(t, graphDir, "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
	checkpointedSchemaStatement(t, graphDir,
		"CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE")

	// A plain data write, through the real command. It succeeds, so it
	// checkpoints, and its checkpoint rewrites the snapshot and truncates the
	// log over a graph that carries a schema this statement never names.
	captureStdStreams(t, func() {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"CREATE (:Spec {key:'session-management'})"}); err != nil {
			t.Fatalf("writing a second Spec node: %v", err)
		}
	})

	// Non-vacuity: that write must actually have checkpointed, or this test is
	// a second copy of the two above.
	if size := fileSize(t, filepath.Join(graphDir, "wal")); size != 0 {
		t.Fatalf("the write-ahead log holds %d bytes after an ordinary write; the write did not "+
			"checkpoint, so this test asserts nothing about a checkpoint that ran over a schema it "+
			"did not create", size)
	}

	if names := readSchemaNames(t, runGraphExecute, roadmap, "SHOW INDEXES"); !containsName(names, "spec_key") {
		t.Errorf("after an ordinary data write checkpointed, SHOW INDEXES reports %v, which does not "+
			"include `spec_key`. A command that never mentioned the index erased it", names)
	}
	if names := readSchemaNames(t, runGraphExecute, roadmap, "SHOW CONSTRAINTS"); !containsName(names, "spec_key_uq") {
		t.Errorf("after an ordinary data write checkpointed, SHOW CONSTRAINTS reports %v, which does "+
			"not include `spec_key_uq`", names)
	}

	// Enforcement again, because a listed constraint is not an applied one, and
	// because this is the path on which the loss would be silent.
	var writeErr error
	stdout, _ := captureStdStreams(t, func() {
		writeErr = runGraphExecute([]string{"-r", roadmap, "--query",
			"CREATE (:Spec {key:'session-management'})"})
	})
	if writeErr == nil {
		t.Errorf("creating a duplicate Spec keyed `session-management` succeeded (stdout %q) after an "+
			"ordinary write had checkpointed over the UNIQUE constraint", stdout)
	}
	if got := specKeyCount(t, roadmap, "session-management"); got != 1 {
		t.Errorf("the graph holds %d Spec nodes keyed `session-management`; exactly 1 is expected", got)
	}
}
