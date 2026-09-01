package web

// Regression suite for rmp task #344 and SPEC/WEB.md Acceptance Criterion 157
// (the web half of SPEC/GRAPH.md Acceptance Criterion 64).
//
// The defect: the endpoint ACCEPTED a schema-introspection command. The guard
// rail admits it — it is read-only — so the endpoint executed it against the
// engine and then had nowhere to put the result: its response carries nodes and
// edges, and a schema listing is tabular rows. The caller received
// {"nodes": [], "edges": []} with HTTP 200 against a store that holds indexes,
// which is indistinguishable from a query that genuinely matched nothing. An
// empty graph presented as the answer reports success while stating something
// false about the store.
//
// The endpoint now refuses the whole family before execution, with HTTP 400 and
// the failure class schema_introspection, and its message names `rmp graph
// query` as where a schema listing is obtained (SPEC/WEB.md § Query-Bar Error
// Handling, case 10).
//
// The store these tests seed really does hold an index and a constraint, and
// that is asserted here before any refusal is: against an empty store the old
// HTTP 200 and the new HTTP 400 would differ only in the status, and the reason
// the refusal exists — that the 200 stated something false — could not be shown
// at all.

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
// store is next opened — which is how `rmp graph update` persists them.
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

// schemaNamesOnTheEndpointsOwnReadPath opens the store and runs a SHOW statement
// on the engine loadGraphView itself builds — recovery.Open followed by
// NewEngineWithOptions carrying the recovered constraints and indexes — and
// returns the `name` column of every row.
//
// It exists to make the refusal tests non-vacuous. The endpoint's own engine
// answers these statements with real rows, so the empty graph the endpoint used
// to return was not an honest report of an empty schema; it was the response
// shape swallowing a result it could not carry.
func schemaNamesOnTheEndpointsOwnReadPath(t *testing.T, name, query string) []string {
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

// TestHandleGraphData_StoreReallyHoldsTheSchema is the premise every refusal
// test in this file rests on, asserted first and separately: the seeded store
// holds a named index and a named constraint, and the engine the ENDPOINT builds
// reports both.
//
// Without it the refusal tests would still pass against a store with no schema
// at all, and the defect they exist to close — an HTTP 200 empty graph reporting
// success while the store does hold indexes — would not be reachable by any test
// here.
func TestHandleGraphData_StoreReallyHoldsTheSchema(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	if names := schemaNamesOnTheEndpointsOwnReadPath(t, name, "SHOW INDEXES"); !slices.Contains(names, "spec_key") {
		t.Fatalf("SHOW INDEXES on the endpoint's own read path reported %v, want it to contain the declared index %q", names, "spec_key")
	}
	if names := schemaNamesOnTheEndpointsOwnReadPath(t, name, "SHOW CONSTRAINTS"); !slices.Contains(names, "spec_key_unique") {
		t.Fatalf("SHOW CONSTRAINTS on the endpoint's own read path reported %v, want it to contain the declared constraint %q", names, "spec_key_unique")
	}
}

// TestHandleGraphData_SchemaIntrospectionRefused is the core regression: every
// member of the schema-introspection family is answered HTTP 400 with `kind`
// `schema_introspection`, the body is the error shape and carries no graph, and
// the message says what the specification requires it to say.
//
// The status is deliberately not the only assertion. HTTP 200 with
// {"nodes": [], "edges": []} is what the defect returned, and a test that only
// checked for a non-200 would pass for a refusal under any class and any reason.
func TestHandleGraphData_SchemaIntrospectionRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	for _, query := range introspectionQueries {
		t.Run(query, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {query}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for %q; body=%q", rec.Code, query, rec.Body.String())
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding error body: %v; body=%q", err, rec.Body.String())
			}
			if body["kind"] != graphErrSchemaIntrospection {
				t.Errorf("kind = %v, want %q; body=%q", body["kind"], graphErrSchemaIntrospection, rec.Body.String())
			}
			// The statement was never run: the body is the error shape and
			// carries neither nodes nor edges.
			if got := slices.Sorted(maps.Keys(body)); !slices.Equal(got, []string{"error", "kind"}) {
				t.Errorf("body fields = %v, want exactly [error kind]; body=%q", got, rec.Body.String())
			}

			reason, _ := body["error"].(string)
			// What the message MUST say: this page draws a graph of nodes and
			// edges and cannot show a schema listing, and `rmp graph query` is
			// where the listing is obtained. A refusal that named no way forward
			// would leave the caller with a valid, supported statement and
			// nowhere to run it.
			for _, want := range []string{"graph", "nodes and edges", "schema listing", "rmp graph query"} {
				if !strings.Contains(reason, want) {
					t.Errorf("error = %q, want it to contain %q", reason, want)
				}
			}
			// What it MUST NOT say. The keyword spacing is not what is wrong
			// here and correcting it changes nothing, so naming it would
			// prescribe a correction that does not work. The statement writes
			// nothing, so "not read-only" would be a false classification. And
			// it never reached the engine, so there is no parse diagnostic.
			for _, forbidden := range []string{"spacing", "one space", "not read-only", "cypher: parse", `unexpected "SHOW"`} {
				if strings.Contains(reason, forbidden) {
					t.Errorf("error = %q, want it never to contain %q", reason, forbidden)
				}
			}
		})
	}

	// Nothing executed and nothing changed: a default read still returns the
	// three seeded nodes, and the schema is still there.
	rec := doGraphData(t, name, nil)
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding post-refusal read: %v", err)
	}
	if len(view.Nodes) != 3 {
		t.Errorf("after the refusals, nodes = %d, want 3", len(view.Nodes))
	}
	if names := schemaNamesOnTheEndpointsOwnReadPath(t, name, "SHOW INDEXES"); !slices.Contains(names, "spec_key") {
		t.Errorf("after the refusals, SHOW INDEXES reported %v, want the declared index still present: a refusal changes nothing in the store", names)
	}
}

// TestHandleGraphData_OrdinaryReadUnaffectedByTheRefusal is the control without
// which the refusal is not shown to be narrow: against the SAME store, an
// ordinary reading query still returns HTTP 200 and the ordinary node-and-edge
// shape. What the endpoint refuses is a class, not queries in general.
func TestHandleGraphData_OrdinaryReadUnaffectedByTheRefusal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	for _, query := range []string{
		defaultGraphQuery,
		"MATCH (n:Spec) RETURN n",
		// A read whose text merely mentions the refused class inside a string
		// literal is an ordinary read: classification runs on the masked
		// normalization, so the refusal cannot be tripped from a literal.
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

	// The default query returns the seeded graph, so the 200s above are a fact
	// about a store with content rather than about an empty one.
	rec := doGraphData(t, name, url.Values{"q": {defaultGraphQuery}})
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding default read: %v", err)
	}
	if len(view.Nodes) != 3 || len(view.Edges) != 2 {
		t.Errorf("default query returned %d nodes and %d edges, want 3 and 2", len(view.Nodes), len(view.Edges))
	}
}

// TestHandleGraphData_SchemaIntrospectionPrecedence asserts, in both directions,
// the two objections the specification says outrank the schema-introspection
// refusal (SPEC/WEB.md § Query-Bar Error Handling, rule 6; Acceptance Criteria
// 123 and 157).
//
// "Both directions" means each pair carries the combined request AND the same
// statement without the outranking half, so the precedence assertion cannot pass
// merely because the endpoint never produces schema_introspection at all.
//
// The fourth ordinal — relationship_read_direction against schema_introspection
// — is NOT asserted here, and deliberately so: a schema-introspection command
// carries no relationship pattern to orient, so no request can exhibit that pair
// and a test for it could only be written with a request that does not exist
// (SPEC/WEB.md § Query-Bar Error Handling, rule 6, "The fourth place is not
// reachable against the third"; Acceptance Criterion 123 forbids testing it).
func TestHandleGraphData_SchemaIntrospectionPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	// A schema-introspection command carrying a DDL tail. It is the ONE
	// construction that is both an introspection command and not read-only: the
	// guard rail's clause classes are independent, and this statement carries a
	// DDL clause as well as the SHOW prefix.
	const introspectionWithDDLTail = "SHOW INDEXES YIELD name CREATE INDEX audit_key FOR (n:Audit) ON (n.key)"

	// A DATA-writing tail does NOT produce that overlap and must not be used to
	// assert it: the engine reports a statement its own DDL predicate accepts as
	// carrying no writing clause, so this one classifies read-only and is itself
	// answered schema_introspection. It is asserted here as the trap it is, so a
	// later edit cannot quietly substitute it for the DDL tail above and end up
	// asserting nothing.
	const introspectionWithDataWritingTail = "SHOW INDEXES YIELD name CREATE (n:Audit)"

	cases := []struct {
		name     string
		params   url.Values
		wantKind string
	}{
		{"alone, the introspection command is refused as its own class",
			url.Values{"q": {"SHOW INDEXES"}}, graphErrSchemaIntrospection},

		{"an invalid limit outranks the introspection refusal",
			url.Values{"limit": {"7"}, "q": {"SHOW INDEXES"}}, graphErrInvalidLimit},
		{"the same statement under an allowed limit is the introspection refusal",
			url.Values{"limit": {"250"}, "q": {"SHOW INDEXES"}}, graphErrSchemaIntrospection},

		{"a DDL tail makes the statement not read-only, which outranks the refusal",
			url.Values{"q": {introspectionWithDDLTail}}, graphErrNotReadOnly},
		{"the same statement without its DDL tail is the introspection refusal",
			url.Values{"q": {"SHOW INDEXES YIELD name"}}, graphErrSchemaIntrospection},

		{"a data-writing tail is NOT the not_read_only pair: it stays the introspection refusal",
			url.Values{"q": {introspectionWithDataWritingTail}}, graphErrSchemaIntrospection},

		{"an invalid limit outranks both objections at once",
			url.Values{"limit": {"7"}, "q": {introspectionWithDDLTail}}, graphErrInvalidLimit},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doGraphData(t, name, tc.params)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
			}
			kind, reason := decodeQueryError(t, rec.Body.Bytes())
			if kind != tc.wantKind {
				t.Errorf("kind = %q, want %q; reason=%q", kind, tc.wantKind, reason)
			}
		})
	}

	// Every one of those was refused before the query ran: the DDL tail never
	// created its index and the data-writing tail never created its node.
	if names := schemaNamesOnTheEndpointsOwnReadPath(t, name, "SHOW INDEXES"); slices.Contains(names, "audit_key") {
		t.Errorf("SHOW INDEXES reported %v: the DDL tail must never have executed", names)
	}
	rec := doGraphData(t, name, nil)
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding post-precedence read: %v", err)
	}
	if len(view.Nodes) != 3 {
		t.Errorf("after the precedence probes, nodes = %d, want 3: every rejection precedes execution", len(view.Nodes))
	}
}

// TestHandleGraphData_SchemaIntrospectionRefusalIsHTMLSafe pins two properties
// of the refusal body at once.
//
// The message carries no request-derived text — it cannot, because Acceptance
// Criterion 151 requires the response to a badly spaced command to EQUAL the
// response to the well-spaced one, which a message echoing the caller's
// statement could not satisfy. That is asserted directly.
//
// And the body is serialized HTML-safe like every other response of this
// endpoint, so a statement carrying markup in a WHERE tail cannot put a raw
// angle bracket into it (SPEC/WEB.md Acceptance Criteria 35 and 157). The
// assertion has teeth for a future edit: the day the message does echo any part
// of the statement, this is the test that requires it to be escaped.
func TestHandleGraphData_SchemaIntrospectionRefusalIsHTMLSafe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	const crafted = `SHOW INDEXES WHERE name = '<script>alert(1)</script>'`
	rec := doGraphData(t, name, url.Values{"q": {crafted}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()

	if strings.ContainsAny(raw, "<>") {
		t.Errorf("body contains a raw angle bracket, so request-derived text is not HTML-escaped; body=%q", raw)
	}
	kind, reason := decodeQueryError(t, rec.Body.Bytes())
	if kind != graphErrSchemaIntrospection {
		t.Errorf("kind = %q, want %q", kind, graphErrSchemaIntrospection)
	}
	if strings.Contains(reason, "script") || strings.Contains(reason, "SHOW") {
		t.Errorf("error = %q, want it to echo no part of the submitted statement: the message is fixed so that every spelling of the class receives the identical response (Acceptance Criterion 151)", reason)
	}
}
