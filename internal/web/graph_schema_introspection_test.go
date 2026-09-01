package web

// Regression suite for SPEC/WEB.md Acceptance Criteria 156 and 157 — what the
// graph data endpoint answers for a schema-introspection command — and for the
// two rules it has held in succession.
//
// The original defect (rmp task #344): the endpoint executed a
// schema-introspection command and then had nowhere to put the result. Its
// response carries nodes and edges; a schema listing is tabular rows. The caller
// received {"nodes": [], "edges": []} with HTTP 200 against a store that holds
// indexes, which is indistinguishable from a statement that genuinely matched
// nothing. #344 answered that by REFUSING the whole family before execution.
//
// The refusal is withdrawn (rmp task #364). The endpoint executes what it is
// given, so the empty graph is back — and it is now the specified answer, with
// the reason stated rather than left to be inferred: it is empty because the
// response SHAPE carries nodes and edges, not because the store's schema is
// empty, and a schema listing is read from `rmp graph execute`, which returns the
// rows (Acceptance Criterion 157).
//
// That is why the store these tests seed really does hold a named index and a
// named constraint, and why that is asserted here before anything else: against
// an empty store, "the endpoint answers an empty graph" would be true for the
// wrong reason, and the distinction the criterion turns on could not be shown at
// all. Criterion 157 requires both halves together, and
// TestHandleGraphData_SchemaListingIsReadFromTheStoreNotTheEndpoint is where they
// meet.

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/store/recovery"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"
	"github.com/FlavioCFOliveira/GoGraph/store/wal"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// schemaSeedStatements declare one index and one constraint on the Spec nodes
// graphSeedQueries creates. Both carry a name the caller chose, so a listing
// that reports them cannot be a listing the engine synthesised from the data.
var schemaSeedStatements = []string{
	"CREATE INDEX spec_key FOR (n:Spec) ON (n.key)",
	"CREATE CONSTRAINT spec_key_unique FOR (n:Spec) REQUIRE n.key IS UNIQUE",
}

// seedGraphSchema commits each schema statement through the transactional write
// path, so the definitions reach the write-ahead log and are recovered when the
// store is next opened — which is how `rmp graph execute` persists them.
func seedGraphSchema(t *testing.T, name string, statements ...string) {
	t.Helper()

	graphDir := graphDirOf(t, name)
	for _, query := range statements {
		res, err := recovery.Open[string, float64](graphDir, recovery.Options[string, float64]{
			Codec:       txn.NewStringCodec(),
			WeightCodec: txn.NewFloat64WeightCodec(),
		})
		if err != nil {
			t.Fatalf("opening graph store for schema seed: %v", err)
		}
		w, err := wal.Open(filepath.Join(graphDir, "wal"))
		if err != nil {
			t.Fatalf("opening wal for schema seed: %v", err)
		}
		store := txn.NewStoreWithOptions[string, float64](res.Graph, w, txn.Options[string, float64]{
			Codec:       txn.NewStringCodec(),
			WeightCodec: txn.NewFloat64WeightCodec(),
		})
		// The recovery-aware engine is what carries the already-registered
		// schema into the transaction, so the second statement is built on top
		// of the first rather than against an empty schema.
		engine := cypher.NewEngineWithStoreAndRecovery(store, res)

		result, runErr := engine.RunInTx(context.Background(), query, nil)
		if runErr != nil {
			_ = w.Close() //nolint:errcheck // test cleanup on the failure path
			t.Fatalf("schema seed %q: %v", query, runErr)
		}
		for result.Next() { //nolint:revive // drain to allow the commit
		}
		if cerr := result.Close(); cerr != nil {
			_ = w.Close() //nolint:errcheck // test cleanup on the failure path
			t.Fatalf("committing schema seed %q: %v", query, cerr)
		}
		if cerr := w.Close(); cerr != nil {
			t.Fatalf("closing wal after %q: %v", query, cerr)
		}
	}
}

// graphDirOf returns the roadmap's graph store directory.
func graphDirOf(t *testing.T, name string) string {
	t.Helper()
	roadmapDir, err := utils.GetRoadmapDir(name)
	if err != nil {
		t.Fatalf("resolving roadmap dir: %v", err)
	}
	return filepath.Join(roadmapDir, "graph")
}

// schemaNamesOnTheStore opens the roadmap's store and runs a SHOW statement
// against it directly, returning the `name` column of every row. It is what
// `rmp graph execute` does for the caller, performed in-process so a Go test can
// read the rows the HTTP response shape cannot carry.
//
// It is what makes the empty-graph assertions non-vacuous. The store answers
// these statements with real rows, so the empty graph the endpoint returns is the
// response shape swallowing a result it cannot carry, and not a report that the
// store holds no schema.
func schemaNamesOnTheStore(t *testing.T, name, query string) []string {
	t.Helper()

	res, err := recovery.Open[string, float64](graphDirOf(t, name), recovery.Options[string, float64]{
		Codec:       txn.NewStringCodec(),
		WeightCodec: txn.NewFloat64WeightCodec(),
	})
	if err != nil {
		t.Fatalf("opening graph store: %v", err)
	}
	engine := cypher.NewEngineWithOptions(res.Graph, cypher.EngineOptions{
		RecoveredConstraints: cypher.ConstraintDefsFromRecovery(res.Constraints),
		RecoveredIndexes:     cypher.IndexDefsFromRecovery(res.Indexes),
	})

	result, err := engine.Run(context.Background(), query, nil)
	if err != nil {
		t.Fatalf("running %q on the read path: %v", query, err)
	}
	defer result.Close() //nolint:errcheck // read path; close commits nothing

	nameCol := slices.Index(result.Columns(), "name")
	if nameCol < 0 {
		t.Fatalf("%q returned columns %v, which do not include `name`: the engine's schema listing has changed shape and this helper can no longer read it",
			query, result.Columns())
	}

	var names []string
	for result.Next() {
		// serializeGraphValue is the endpoint's OWN value mapping, reused here
		// rather than reimplemented, so the helper reads a value exactly as the
		// response would have rendered it.
		if s, ok := serializeGraphValue(result.ValueAt(nameCol)).(string); ok {
			names = append(names, s)
		}
	}
	if err := result.Err(); err != nil {
		t.Fatalf("iterating %q: %v", query, err)
	}
	return names
}

// seedGraphWithSchema builds the store every test in this file runs against: the
// small semantic graph plus a declared index and a declared constraint.
func seedGraphWithSchema(t *testing.T, roadmap string) string {
	t.Helper()
	name := seedRoadmap(t, roadmap)
	seedGraph(t, name, graphSeedQueries()...)
	seedGraphSchema(t, name, schemaSeedStatements...)
	return name
}

// introspectionQueries is the schema-introspection family the endpoint refuses:
// the four target keywords, plain, and each tail the class admits.
var introspectionQueries = []string{
	"SHOW INDEXES",
	"SHOW INDEX",
	"SHOW CONSTRAINTS",
	"SHOW CONSTRAINT",
	"show indexes",
	"SHOW INDEXES YIELD name, type RETURN name",
	"SHOW CONSTRAINTS YIELD name RETURN name",
	"SHOW INDEXES WHERE type = 'hash'",
	"   SHOW INDEXES",
	"/* schema check */ SHOW CONSTRAINTS",
}

// TestHandleGraphData_StoreReallyHoldsTheSchema is the premise every test in
// this file rests on, asserted first and separately: the seeded store holds a
// named index and a named constraint, and reading the store reports both.
//
// Without it, every "the endpoint answers an empty graph" assertion below would
// still pass against a store with no schema at all, and the distinction
// Acceptance Criterion 157 turns on — an empty answer that is a property of the
// RESPONSE SHAPE and not of the store — would be unobservable.
func TestHandleGraphData_StoreReallyHoldsTheSchema(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	if names := schemaNamesOnTheStore(t, name, "SHOW INDEXES"); !slices.Contains(names, "spec_key") {
		t.Fatalf("SHOW INDEXES over the store reported %v, want it to contain the declared index %q", names, "spec_key")
	}
	if names := schemaNamesOnTheStore(t, name, "SHOW CONSTRAINTS"); !slices.Contains(names, "spec_key_unique") {
		t.Fatalf("SHOW CONSTRAINTS over the store reported %v, want it to contain the declared constraint %q", names, "spec_key_unique")
	}
}

// TestHandleGraphData_SchemaListingIsReadFromTheStoreNotTheEndpoint is
// Acceptance Criterion 157, and the criterion requires BOTH halves to be
// asserted together: every member of the schema-introspection family is answered
// HTTP 200 with exactly {"nodes": [], "edges": []}, while the same statement over
// the same store returns the rows naming the index the caller declared.
//
// Asserting that the endpoint reports the index row MUST fail this criterion, so
// the empty body is asserted exactly — a body carrying any node, any edge, or any
// `kind` fails.
func TestHandleGraphData_SchemaListingIsReadFromTheStoreNotTheEndpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	for _, query := range introspectionQueries {
		t.Run(query, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {query}})
			if rec.Code != http.StatusOK {
				kind, reason := decodeQueryError(t, rec.Body.Bytes())
				t.Fatalf("status = %d, want 200 for %q: the endpoint executes the statement and "+
					"refuses nothing (kind=%q, reason=%q)", rec.Code, query, kind, reason)
			}

			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding body: %v; body=%q", err, rec.Body.String())
			}
			if got := slices.Sorted(maps.Keys(body)); !slices.Equal(got, []string{"edges", "nodes"}) {
				t.Fatalf("body fields = %v, want exactly [edges nodes]; body=%q", got, rec.Body.String())
			}
			for _, key := range []string{"nodes", "edges"} {
				if string(body[key]) != "[]" {
					t.Errorf("%s = %s, want []: the rows a schema listing returns carry no node and "+
						"no edge, so the response shape cannot carry them", key, body[key])
				}
			}
		})
	}

	// The other half, without which the empty answers above are consistent with
	// a store that simply has no schema: the same statements over the store
	// return the declared names.
	if names := schemaNamesOnTheStore(t, name, "SHOW INDEXES"); !slices.Contains(names, "spec_key") {
		t.Errorf("SHOW INDEXES over the store reported %v, want the declared index: the endpoint's "+
			"empty answer must be a property of its response shape, not of the store", names)
	}
	if names := schemaNamesOnTheStore(t, name, "SHOW CONSTRAINTS"); !slices.Contains(names, "spec_key_unique") {
		t.Errorf("SHOW CONSTRAINTS over the store reported %v, want the declared constraint", names)
	}
}

// TestHandleGraphData_EmptyGraphAnswersAreIndistinguishable is Acceptance
// Criterion 156: four statements that produce no node and no edge for four
// different reasons are answered identically, because the endpoint publishes no
// class that separates them.
//
// The four responses are compared TO ONE ANOTHER, which is what the criterion
// asks for, and the control keeps it narrow: the default query over the same
// store returns a non-empty nodes array, so an empty answer is a property of the
// statement rather than of the endpoint.
func TestHandleGraphData_EmptyGraphAnswersAreIndistinguishable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	statements := []string{
		`MATCH (n:Absent) RETURN n`,  // matched nothing
		`MATCH (n) RETURN count(n)`,  // returned a number
		`SHOW INDEXES`,               // returned tabular rows
		`CREATE (n:Probe {key:'p'})`, // created a node, returned no columns
	}

	var first string
	for i, q := range statements {
		rec := doGraphData(t, name, url.Values{"q": {q}})
		if rec.Code != http.StatusOK {
			kind, reason := decodeQueryError(t, rec.Body.Bytes())
			t.Fatalf("%q: status = %d, want 200 (kind=%q, reason=%q)", q, rec.Code, kind, reason)
		}
		if i == 0 {
			first = rec.Body.String()
			continue
		}
		if rec.Body.String() != first {
			t.Errorf("%q answered %s, and %q answered %s. The endpoint publishes no class that "+
				"separates them, so the four must be indistinguishable (SPEC/WEB.md § Query-Bar "+
				"Error Handling, rule 9)", q, rec.Body.String(), statements[0], first)
		}
	}

	// The CREATE really created: the empty answer is the response shape, not a
	// statement that did nothing.
	if keys := nodeKeys(t, doGraphData(t, name, nil)); !slices.Contains(keys, "p") {
		t.Errorf("the CREATE that answered an empty graph did not persist (%v): the empty response "+
			"must not mean the statement was discarded", keys)
	}

	// The control: the same store answers the default query with a non-empty
	// nodes array.
	rec := doGraphData(t, name, url.Values{"q": {defaultGraphQuery}})
	if rec.Code != http.StatusOK {
		t.Fatalf("control: status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("control: decoding: %v", err)
	}
	if len(view.Nodes) == 0 {
		t.Fatal("control: the default query returned no node, so an empty answer is a property of " +
			"the endpoint rather than of the statement and the comparison above proves nothing")
	}
}

// TestHandleGraphData_OrdinaryReadUnaffected is the control that keeps the
// empty-graph answers from being mistaken for a rule about SHOW: against the SAME
// store, an ordinary reading query returns HTTP 200 and the ordinary
// node-and-edge shape, populated.
func TestHandleGraphData_OrdinaryReadUnaffected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	for _, query := range []string{
		defaultGraphQuery,
		"MATCH (n:Spec) RETURN n",
		// A read whose text merely mentions a schema listing inside a string
		// literal is an ordinary read: the injection suppression runs on the
		// masked normalization, so it cannot be tripped from a literal.
		`MATCH (n) WHERE n.key = 'SHOW INDEXES' RETURN n`,
	} {
		t.Run(query, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {query}})
			if rec.Code != http.StatusOK {
				kind, reason := decodeQueryError(t, rec.Body.Bytes())
				t.Fatalf("status = %d, want 200 for the control query %q (kind=%q, reason=%q)", rec.Code, query, kind, reason)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding body: %v; body=%q", err, rec.Body.String())
			}
			if got := slices.Sorted(maps.Keys(body)); !slices.Equal(got, []string{"edges", "nodes"}) {
				t.Fatalf("body fields = %v, want exactly [edges nodes]; body=%q", got, rec.Body.String())
			}
		})
	}

	rec := doGraphData(t, name, url.Values{"q": {defaultGraphQuery}})
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding default read: %v", err)
	}
	if len(view.Nodes) != 3 || len(view.Edges) != 2 {
		t.Errorf("default query returned %d nodes and %d edges, want 3 and 2", len(view.Nodes), len(view.Edges))
	}
}

// TestHandleGraphData_SchemaDDLThroughTheEndpointPersists pins what the endpoint
// now does with the other half of the schema surface, and it is the pair to the
// listing above: a schema statement submitted through the query bar EXECUTES,
// commits, and is found afterwards by reading the store.
//
// This is the construction the withdrawn precedence rule used as its worked
// example — a schema-introspection command carrying a DDL tail — and it is kept
// because what it establishes is now more interesting than the ordering it used
// to prove: an unauthenticated GET creates an index in the roadmap's knowledge
// graph (SPEC/WEB.md § Security and Constraints, rule 3).
func TestHandleGraphData_SchemaDDLThroughTheEndpointPersists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	if names := schemaNamesOnTheStore(t, name, "SHOW INDEXES"); slices.Contains(names, "audit_key") {
		t.Fatalf("the index exists before anything created it: %v", names)
	}

	rec := doGraphData(t, name, url.Values{"q": {"CREATE INDEX audit_key FOR (n:Audit) ON (n.key)"}})
	if rec.Code != http.StatusOK {
		kind, reason := decodeQueryError(t, rec.Body.Bytes())
		t.Fatalf("CREATE INDEX status = %d, want 200 (kind=%q, reason=%q)", rec.Code, kind, reason)
	}
	names := schemaNamesOnTheStore(t, name, "SHOW INDEXES")
	if !slices.Contains(names, "audit_key") {
		t.Fatalf("SHOW INDEXES over the store reports %v: an index created through the endpoint must "+
			"persist, under the name the caller declared", names)
	}
	// The index the store already held is still there: the checkpoint that
	// followed the DDL carried the whole registered schema, not just the new
	// definition (SPEC/GRAPH.md § Synchronous Checkpoint on Write, step 2).
	if !slices.Contains(names, "spec_key") {
		t.Errorf("SHOW INDEXES reports %v: the pre-existing index was lost by the checkpoint that "+
			"followed the DDL, which is the snapshot-without-schema defect", names)
	}
	if constraints := schemaNamesOnTheStore(t, name, "SHOW CONSTRAINTS"); !slices.Contains(constraints, "spec_key_unique") {
		t.Errorf("SHOW CONSTRAINTS reports %v: the declared constraint was lost by the checkpoint", constraints)
	}

	// And the DROP is symmetric.
	if rec := doGraphData(t, name, url.Values{"q": {"DROP INDEX audit_key"}}); rec.Code != http.StatusOK {
		t.Fatalf("DROP INDEX status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if names := schemaNamesOnTheStore(t, name, "SHOW INDEXES"); slices.Contains(names, "audit_key") {
		t.Errorf("SHOW INDEXES reports %v after a DROP through the endpoint", names)
	}
}

// TestHandleGraphData_EmptyGraphAnswerIsHTMLSafe keeps the serialization property
// under test on this family. A statement carrying markup in a WHERE tail must not
// put a raw angle bracket into the response, whatever the response is
// (SPEC/WEB.md Acceptance Criteria 35 and 157).
func TestHandleGraphData_EmptyGraphAnswerIsHTMLSafe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	const crafted = `SHOW INDEXES WHERE name = '<script>alert(1)</script>'`
	rec := doGraphData(t, name, url.Values{"q": {crafted}})
	if rec.Code != http.StatusOK && rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 200 or 400; body=%q", rec.Code, rec.Body.String())
	}
	if raw := rec.Body.String(); strings.ContainsAny(raw, "<>") {
		t.Errorf("body contains a raw angle bracket, so request-derived text is not HTML-escaped; body=%q", raw)
	}
}
