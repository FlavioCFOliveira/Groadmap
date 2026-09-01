package web

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

// seedGraph writes a small knowledge graph into the roadmap's GoGraph store via
// the engine's transactional write path, so the read-only graph data endpoint
// has genuine nodes and edges to extract. It mirrors the minimal write sequence
// commands/graph.go runGraphWrite performs (recovery.Open -> wal.Open ->
// NewStoreWithOptions -> RunInTx -> Close commits). It runs each CREATE in its
// own committed transaction. The caller must have redirected HOME first.
func seedGraph(t *testing.T, name string, queries ...string) {
	t.Helper()

	roadmapDir, err := utils.GetRoadmapDir(name)
	if err != nil {
		t.Fatalf("resolving roadmap dir: %v", err)
	}
	graphDir := filepath.Join(roadmapDir, "graph")
	if mkErr := os.MkdirAll(graphDir, 0o700); mkErr != nil {
		t.Fatalf("creating graph dir: %v", mkErr)
	}

	for _, q := range queries {
		writeGraphTx(t, graphDir, q)
	}
}

// writeGraphTx commits one write query against the store at graphDir.
func writeGraphTx(t *testing.T, graphDir, query string) {
	t.Helper()

	res, err := recovery.Open[string, float64](graphDir, recovery.Options[string, float64]{
		Codec:       txn.NewStringCodec(),
		WeightCodec: txn.NewFloat64WeightCodec(),
	})
	if err != nil {
		t.Fatalf("opening graph store for seed: %v", err)
	}

	w, err := wal.Open(filepath.Join(graphDir, "wal"))
	if err != nil {
		t.Fatalf("opening wal for seed: %v", err)
	}
	defer w.Close() //nolint:errcheck // test cleanup

	store := txn.NewStoreWithOptions[string, float64](res.Graph, w, txn.Options[string, float64]{
		Codec:       txn.NewStringCodec(),
		WeightCodec: txn.NewFloat64WeightCodec(),
	})
	engine := cypher.NewEngineWithStore(store)

	result, err := engine.RunInTx(context.Background(), query, nil)
	if err != nil {
		t.Fatalf("seed query %q: %v", query, err)
	}
	for result.Next() { //nolint:revive // drain to allow the commit
	}
	if cerr := result.Close(); cerr != nil {
		t.Fatalf("committing seed query %q: %v", query, cerr)
	}
}

// graphSeedQueries builds a tiny multi-layer graph: two Spec nodes, one Code
// node, and two typed edges between them, enough to exercise node/edge
// extraction, label/type inventory, and edge endpoint resolution.
func graphSeedQueries() []string {
	return []string{
		`CREATE (s:Spec {key:'user-authentication'})`,
		`CREATE (c:Code {path:'internal/auth/jwt.go'})`,
		`MATCH (s:Spec {key:'user-authentication'}), (c:Code {path:'internal/auth/jwt.go'}) CREATE (s)-[:IMPLEMENTED_BY]->(c)`,
		`CREATE (d:Spec {key:'payment-processing'})`,
		`MATCH (s:Spec {key:'user-authentication'}), (d:Spec {key:'payment-processing'}) CREATE (s)-[:DEPENDS_ON]->(d)`,
	}
}

// doGraphData issues a GET to the graph data endpoint with the given query
// values and returns the recorder.
func doGraphData(t *testing.T, name string, params url.Values) *httptest.ResponseRecorder {
	t.Helper()
	mux := buildMux()
	target := "/roadmaps/" + name + "/graph/data"
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestHandleGraphData_DefaultQueryBackwardCompatible asserts a request with no
// q parameter runs the default full-graph query and returns every seeded node
// and edge, exactly as the endpoint behaved before the query bar existed
// (SPEC/WEB.md § Graph Data Endpoint; Acceptance Criterion 46).
func TestHandleGraphData_DefaultQueryBackwardCompatible(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	rec := doGraphData(t, name, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}

	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding: %v; body=%q", err, rec.Body.String())
	}
	if len(view.Nodes) != 3 {
		t.Errorf("nodes = %d, want 3 (two Spec + one Code)", len(view.Nodes))
	}
	if len(view.Edges) != 2 {
		t.Errorf("edges = %d, want 2 (IMPLEMENTED_BY + DEPENDS_ON)", len(view.Edges))
	}

	// Every edge endpoint must reference a node present in nodes (the view-data
	// invariant; SPEC/DATA_FORMATS.md § Graph View Data, rule 3).
	nodeIDs := map[float64]bool{}
	for _, n := range view.Nodes {
		nodeIDs[n["id"].(float64)] = true
	}
	for _, e := range view.Edges {
		if !nodeIDs[e["startId"].(float64)] || !nodeIDs[e["endId"].(float64)] {
			t.Errorf("edge %v references an endpoint absent from nodes", e)
		}
	}
}

// TestHandleGraphData_RejectsWriteQueries asserts a query containing a writing
// or DDL clause is rejected by the read-only guard-rail BEFORE execution, the
// store is left unchanged, and the page receives the distinct "not read-only"
// classification (SPEC/WEB.md § Graph Data Endpoint; Acceptance Criterion 47).
func TestHandleGraphData_RejectsWriteQueries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	writeQueries := []string{
		`MATCH (n) DELETE n`,
		`MATCH (n) DETACH DELETE n`,
		`CREATE (x:Spec {key:'injected'})`,
		`MERGE (x:Spec {key:'injected'})`,
		`MATCH (n:Spec) SET n.status = 'done'`,
		`MATCH (n:Spec) REMOVE n.key`,
		`CREATE INDEX ON :Spec(key)`,
		`DROP CONSTRAINT spec_key`,
		// non-canonical casing/spacing must still be caught.
		`create   index spec_idx`,
		`mAtCh (n) dElEtE n`,
	}

	for _, q := range writeQueries {
		t.Run(q, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {q}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for rejected query; body=%q", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding error body: %v", err)
			}
			if body["kind"] != graphErrNotReadOnly {
				t.Errorf("kind = %v, want %q; body=%q", body["kind"], graphErrNotReadOnly, rec.Body.String())
			}
		})
	}

	// The store must be unchanged: a fresh default read still returns exactly
	// the three seeded nodes (no injected node, nothing deleted).
	rec := doGraphData(t, name, nil)
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding post-rejection read: %v", err)
	}
	if len(view.Nodes) != 3 {
		t.Errorf("after rejected writes, nodes = %d, want 3 (store must be unchanged)", len(view.Nodes))
	}
}

// TestHandleGraphData_LiteralMaskingNotFalselyRejected asserts a read-only query
// whose write keywords appear only inside a string literal is accepted and
// executed, while a genuine write clause is rejected (literal-aware masking
// regression; SPEC/WEB.md § Graph Data Endpoint; Acceptance Criterion 47).
func TestHandleGraphData_LiteralMaskingNotFalselyRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	// Write keywords only inside a string literal: must be ACCEPTED as read-only.
	accepted := `MATCH (m) WHERE m.key = "mentions delete and set and create" RETURN m`
	rec := doGraphData(t, name, url.Values{"q": {accepted}})
	if rec.Code != http.StatusOK {
		t.Fatalf("literal-only write keywords: status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}

	// A genuine write clause on the same shape: must be REJECTED.
	rejected := `MATCH (m) WHERE m.key = "mentions delete" DELETE m`
	rec = doGraphData(t, name, url.Values{"q": {rejected}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("genuine DELETE: status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["kind"] != graphErrNotReadOnly {
		t.Errorf("kind = %v, want %q", body["kind"], graphErrNotReadOnly)
	}
}

// TestHandleGraphData_InvalidLimitRejected asserts a limit outside the six
// allowed values is rejected (not clamped) with the invalid-limit
// classification, and the query is not executed (SPEC/WEB.md § Graph Data
// Endpoint; Acceptance Criterion 48).
func TestHandleGraphData_InvalidLimitRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	for _, bad := range []string{"7", "0", "-50", "5000", "100x", "abc"} {
		t.Run(bad, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"limit": {bad}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("limit %q: status = %d, want 400; body=%q", bad, rec.Code, rec.Body.String())
			}
			var body map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if body["kind"] != graphErrInvalidLimit {
				t.Errorf("limit %q: kind = %v, want %q", bad, body["kind"], graphErrInvalidLimit)
			}
		})
	}

	// Every allowed value must be accepted.
	for _, ok := range []string{"50", "100", "250", "500", "1000", "3000"} {
		rec := doGraphData(t, name, url.Values{"limit": {ok}})
		if rec.Code != http.StatusOK {
			t.Errorf("limit %q: status = %d, want 200", ok, rec.Code)
		}
	}
}

// TestHandleGraphData_ExecutionFailure asserts a query accepted as read-only but
// invalid in the engine surfaces the distinct execution-failure classification,
// not a read-only rejection (SPEC/WEB.md § Query-Bar Error Handling, rule 3;
// Acceptance Criterion 50).
func TestHandleGraphData_ExecutionFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	// Read-only (no writing/DDL clause) but syntactically invalid Cypher.
	rec := doGraphData(t, name, url.Values{"q": {`MATCH (n) RETURN`}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if body["kind"] != graphErrExecution {
		t.Errorf("kind = %v, want %q; body=%q", body["kind"], graphErrExecution, rec.Body.String())
	}
}

// TestHandleGraphData_CacheControlOnError asserts the structured error response
// still carries Cache-Control: no-store (it is a data-derived response) and the
// JSON content type (SPEC/WEB.md § Cache Policy; § Query-Bar Error Handling).
func TestHandleGraphData_CacheControlOnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	// Use the full handler chain so the security/cache middleware runs.
	srv := httptest.NewServer(handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/roadmaps/" + name + "/graph/data?q=" + url.QueryEscape("MATCH (n) DELETE n"))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, contentTypeJSON) {
		t.Errorf("Content-Type = %q, want %q", ct, contentTypeJSON)
	}
}

// TestHandleGraphData_RejectsSpoofedDDLKeywords is the HTTP-level regression for
// the guard-rail bypass a security audit proved end to end: a DDL keyword
// spelled with a non-ASCII letter that Unicode UPPERCASING maps onto ASCII
// (U+0131 dotless i uppercases to 'I') was not seen as DDL by the guard rail's
// case-insensitive regexp, while the engine's own dispatcher — which decides on
// strings.ToUpper — routed it straight to its DDL executor.
//
// The proven consequence was that an unauthenticated GET on this read-only
// endpoint executed schema DDL: "CREATE <U+0131>NDEX evil FOR (n:Bulk) ON (n.i)"
// answered 200, and the DROP form reached exec.DropIndex. Every such form must
// now be refused before execution, with the same classification as any other
// write, and the store must be untouched.
func TestHandleGraphData_RejectsSpoofedDDLKeywords(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	spoofed := []string{
		"CREATE ıNDEX evil FOR (n:Spec) ON (n.key)",
		"CREATE ıNDEX IF NOT EXISTS evil FOR (n:Spec) ON (n.key)",
		"DROP ıNDEX evil",
		"drop ındex evil",
		"CREATE CONSTRAıNT c1 FOR (n:Spec) REQUIRE n.key IS UNIQUE",
		"DROP CONSTRAıNT c1",
		"CREATE CONſTRAINT c1 FOR (n:Spec) REQUIRE n.key IS UNIQUE",
		// Combined with the trailing-comment trick that used to swallow the
		// injected LIMIT, which is how the DDL reached the engine intact.
		"CREATE ıNDEX evil FOR (n:Spec) ON (n.key) //",
	}

	for _, q := range spoofed {
		t.Run(q, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {q}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: schema DDL must never execute on the read-only endpoint; body=%q", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding error body: %v", err)
			}
			if body["kind"] != graphErrNotReadOnly {
				t.Errorf("kind = %v, want %q (rejected by the guard rail, not by the engine); body=%q", body["kind"], graphErrNotReadOnly, rec.Body.String())
			}
		})
	}

	// The store is unchanged: the default read still returns the seeded graph.
	rec := doGraphData(t, name, nil)
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding post-rejection read: %v", err)
	}
	if len(view.Nodes) != 3 {
		t.Errorf("after rejected DDL, nodes = %d, want 3 (store must be unchanged)", len(view.Nodes))
	}
}

// TestHandleGraphData_LimitAppliesDespiteTrailingComment is the HTTP-level
// regression for the node-limit bypass: the endpoint appended its LIMIT clause
// on the same line as the user's query, so a query ending in a line comment
// swallowed it and the endpoint returned the WHOLE graph instead of the resolved
// limit (proven against a 252-node store, which returned all 252 nodes).
func TestHandleGraphData_LimitAppliesDespiteTrailingComment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, `UNWIND range(1,120) AS i CREATE (:Bulk {i:i})`)

	for _, q := range []string{
		"MATCH (n) RETURN n //",
		"MATCH (n) RETURN n // show everything",
		"MATCH (n) RETURN n\n// trailing comment line",
	} {
		t.Run(q, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {q}, "limit": {"50"}})
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
			}
			var view graphView
			if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
				t.Fatalf("decoding: %v", err)
			}
			if len(view.Nodes) != 50 {
				t.Errorf("nodes = %d, want 50: the resolved limit must apply even when the query ends in a comment", len(view.Nodes))
			}
		})
	}
}

// corruptGraphWAL makes the roadmap's graph store fail to OPEN, by flipping a
// byte in the middle of its write-ahead log. The seeded graph commits each CREATE
// in its own transaction, so a byte in the middle of the log lands inside a frame
// that has committed transactions on both sides of it: that is genuine mid-WAL
// corruption, not a torn tail, and GoGraph's recovery.Open surfaces it as a hard
// error rather than truncating to the last good frame.
//
// This is the fixture for the internal-read-error side of the boundary in
// SPEC/WEB.md § Query-Bar Error Handling, rule 7. It is deliberately a data
// fault, not a permission fault: it needs no chmod, behaves identically for an
// unprivileged and a privileged test process, and is the same fault GoGraph's own
// recovery suite uses to prove Open fails hard.
func corruptGraphWAL(t *testing.T, name string) {
	t.Helper()

	roadmapDir, err := utils.GetRoadmapDir(name)
	if err != nil {
		t.Fatalf("resolving roadmap dir: %v", err)
	}
	walPath := filepath.Join(roadmapDir, "graph", "wal")

	raw, err := os.ReadFile(walPath) //nolint:gosec // path derives from t.TempDir via HOME
	if err != nil {
		t.Fatalf("reading graph WAL: %v", err)
	}
	if len(raw) < 64 {
		t.Fatalf("graph WAL is %d bytes: too small to corrupt a middle frame", len(raw))
	}
	raw[len(raw)/2] ^= 0xFF
	if err := os.WriteFile(walPath, raw, 0o600); err != nil {
		t.Fatalf("writing corrupted graph WAL: %v", err)
	}
}

// TestHandleGraphData_InvalidLimitTakesPrecedenceOverNotReadOnly pins the
// precedence between the failure classes: one request can be wrong in more than
// one way at once, and the endpoint resolves the limit BEFORE it runs the
// read-only guard rail, so a request carrying both an invalid limit and a query
// that is not read-only is answered invalid_limit, never not_read_only. The order
// in which SPEC/WEB.md § Query-Bar Error Handling lists cases 1 to 3 is an order
// of explanation, not an order of precedence; rule 6 is the order of precedence
// and this test is what holds the implementation to it (Acceptance Criterion 123).
//
// The two single-fault controls are what make the assertion non-vacuous: without
// them, an endpoint that simply never classified anything as not_read_only would
// pass the combined case.
func TestHandleGraphData_InvalidLimitTakesPrecedenceOverNotReadOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	const writeQuery = `MATCH (n) DELETE n`
	const badLimit = "7"

	// Control A: the query alone IS classified not_read_only, so the combined
	// case below cannot pass merely because that class is unreachable.
	rec := doGraphData(t, name, url.Values{"q": {writeQuery}, "limit": {"100"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("control A: status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	var control map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &control); err != nil {
		t.Fatalf("control A: decoding: %v", err)
	}
	if control["kind"] != graphErrNotReadOnly {
		t.Fatalf("control A: kind = %v, want %q: the write query must be rejected by the guard rail when the limit is valid", control["kind"], graphErrNotReadOnly)
	}

	// Control B: the limit alone IS classified invalid_limit.
	rec = doGraphData(t, name, url.Values{"limit": {badLimit}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("control B: status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &control); err != nil {
		t.Fatalf("control B: decoding: %v", err)
	}
	if control["kind"] != graphErrInvalidLimit {
		t.Fatalf("control B: kind = %v, want %q", control["kind"], graphErrInvalidLimit)
	}

	// The claim: both wrong at once resolves to invalid_limit.
	rec = doGraphData(t, name, url.Values{"q": {writeQuery}, "limit": {badLimit}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v; body=%q", err, rec.Body.String())
	}
	if body["kind"] != graphErrInvalidLimit {
		t.Errorf("kind = %v, want %q: the limit is resolved before the guard rail runs, so an invalid limit outranks a query that is not read-only (SPEC/WEB.md § Query-Bar Error Handling, rule 6)", body["kind"], graphErrInvalidLimit)
	}
	// The reason names the rejected value, not the query (rule 8; the allowed
	// values named in the same message contain no '7').
	if reason, _ := body["error"].(string); !strings.Contains(reason, badLimit) {
		t.Errorf("error = %q, want it to name the rejected limit %q", reason, badLimit)
	}

	// The query never ran: the DELETE would have emptied the store.
	rec = doGraphData(t, name, nil)
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding post-rejection read: %v", err)
	}
	if len(view.Nodes) != 3 {
		t.Errorf("after the combined rejection, nodes = %d, want 3: the request must be rejected before the query runs, so nothing is written", len(view.Nodes))
	}
}

// TestHandleGraphData_QueryBarRejectionPrecedesStoreOpen pins the two halves of
// the boundary in SPEC/WEB.md § Query-Bar Error Handling, rule 7, against a
// roadmap whose graph store cannot be opened:
//
//   - The 500 half: a request the endpoint accepts reaches the open, the open
//     fails, and that is an internal read error — HTTP 500, and NOT a 400 with
//     kind execution. The boundary is drawn at the moment the failure surfaces,
//     and a failure to open surfaces before the query runs.
//   - The 400 half: every query-bar rejection still answers 400 with its own kind
//     over the very same unopenable store, which can only be true if the limit
//     resolution and the read-only guard rail both run BEFORE the store is opened
//     (rule 6). The 500 above is what makes this non-vacuous: it proves the store
//     really is unopenable, so a 400 here cannot have come from a successful read.
func TestHandleGraphData_QueryBarRejectionPrecedesStoreOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)
	corruptGraphWAL(t, name)

	// The 500 half.
	rec := doGraphData(t, name, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("accepted request over an unopenable store: status = %d, want 500; body=%q", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"kind"`) {
		t.Errorf("the 500 of an internal read error must not carry the query-bar error shape; body=%q", rec.Body.String())
	}

	// The 400 half.
	cases := []struct {
		name     string
		params   url.Values
		wantKind string
	}{
		{"invalid limit alone", url.Values{"limit": {"7"}}, graphErrInvalidLimit},
		{"not read-only alone", url.Values{"q": {`MATCH (n) DELETE n`}}, graphErrNotReadOnly},
		{"both wrong at once", url.Values{"limit": {"7"}, "q": {`MATCH (n) DELETE n`}}, graphErrInvalidLimit},
		// The schema-introspection refusal is a guard-rail rejection of the same
		// nature and carries the same guarantee: it too is decided before the
		// store is opened, which the unopenable store proves here. Both
		// spellings are probed, because the refusal covers the class at every
		// keyword spacing and both must be decided this early.
		{"schema introspection alone", url.Values{"q": {"SHOW INDEXES"}}, graphErrSchemaIntrospection},
		{"schema introspection, badly spaced, alone", url.Values{"q": {"SHOW  INDEXES"}}, graphErrSchemaIntrospection},
		{"invalid limit outranks the schema-introspection refusal", url.Values{"limit": {"7"}, "q": {"SHOW INDEXES"}}, graphErrInvalidLimit},
		// The relationship-read-direction rejection carries the same guarantee:
		// it is decided from the PARSED query, before the store is opened, which
		// the unopenable store proves here.
		{"relationship read direction alone", url.Values{"q": {`MATCH (a)-[e]-(b) RETURN type(e)`}}, graphErrRelationshipDirection},
		{"invalid limit outranks read direction", url.Values{"limit": {"7"}, "q": {`MATCH (a)-[e]-(b) RETURN type(e)`}}, graphErrInvalidLimit},
		// A query that BOTH writes and reads a misoriented relationship is
		// answered not_read_only: the objection that it writes outranks the
		// objection that its traversal is misoriented.
		{"not read-only outranks read direction", url.Values{"q": {`MATCH (a)-[e]-(b) SET a.seen = type(e)`}}, graphErrNotReadOnly},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doGraphData(t, name, tc.params)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: the request must be rejected before the graph store is opened; body=%q", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding error body: %v; body=%q", err, rec.Body.String())
			}
			if body["kind"] != tc.wantKind {
				t.Errorf("kind = %v, want %q", body["kind"], tc.wantKind)
			}
		})
	}
}

// TestHandleGraphData_ErrorBodyShape pins the failure body's exact field set for
// every failure class the endpoint publishes — the set enumerated in
// SPEC/WEB.md § Query-Bar Error Handling, rule 5 — exactly two fields, `error`
// and `kind`, both strings and both non-empty, and neither `nodes` nor `edges`
// (SPEC/DATA_FORMATS.md § Graph View Data, Error Shape, rule 1; Acceptance
// Criterion 123).
//
// Asserting the exact field set — rather than only that `kind` is present, which
// every other test in this file does — is the point: a body that gained a third
// field, or that leaked an empty `nodes`/`edges` pair from the success shape,
// would satisfy every existing assertion and only this one would catch it.
func TestHandleGraphData_ErrorBodyShape(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	cases := []struct {
		name     string
		params   url.Values
		wantKind string
	}{
		{"not read-only", url.Values{"q": {`MATCH (n) DELETE n`}}, graphErrNotReadOnly},
		{"invalid limit", url.Values{"limit": {"7"}}, graphErrInvalidLimit},
		{"schema introspection", url.Values{"q": {"SHOW INDEXES"}}, graphErrSchemaIntrospection},
		{"relationship read direction", url.Values{"q": {`MATCH (a)-[e]-(b) RETURN type(e)`}}, graphErrRelationshipDirection},
		{"execution failure", url.Values{"q": {`MATCH (n) RETURN`}}, graphErrExecution},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doGraphData(t, name, tc.params)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding error body: %v; body=%q", err, rec.Body.String())
			}

			if got := slices.Sorted(maps.Keys(body)); !slices.Equal(got, []string{"error", "kind"}) {
				t.Errorf("body fields = %v, want exactly [error kind]; body=%q", got, rec.Body.String())
			}
			for _, field := range []string{"error", "kind"} {
				v, present := body[field]
				if !present {
					t.Errorf("body is missing the %q field", field)
					continue
				}
				s, isString := v.(string)
				if !isString {
					t.Errorf("%q = %v (%T), want a string", field, v, v)
					continue
				}
				if s == "" {
					t.Errorf("%q is the empty string, want a non-empty value", field)
				}
			}
			for _, absent := range []string{"nodes", "edges"} {
				if _, present := body[absent]; present {
					t.Errorf("failure body carries %q: a response that is not successful carries neither nodes nor edges", absent)
				}
			}
			if body["kind"] != tc.wantKind {
				t.Errorf("kind = %v, want %q", body["kind"], tc.wantKind)
			}
		})
	}
}

// TestHandleGraphData_ErrorBodySerialization pins how the failure body is
// serialized: exactly as every other response of this endpoint is — HTML-safe, so
// <, > and & are escaped, pretty-printed with two-space indentation, and
// terminated by a newline (SPEC/DATA_FORMATS.md § Graph View Data, Error Shape,
// rule 4).
//
// The HTML-safety half has teeth because the `error` of an invalid limit quotes
// the rejected value verbatim, so a crafted limit is the one place request-derived
// text reaches the response body of this endpoint.
func TestHandleGraphData_ErrorBodySerialization(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	rec := doGraphData(t, name, url.Values{"limit": {`<script>alert(1)</script>`}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()

	// HTML-safe: the angle brackets of the echoed value are escaped, and no raw
	// '<' or '>' survives anywhere in the body.
	if strings.ContainsAny(raw, "<>") {
		t.Errorf("body contains a raw angle bracket, so request-derived text is not HTML-escaped; body=%q", raw)
	}
	if !strings.Contains(raw, `\u003cscript\u003e`) {
		t.Errorf("body does not carry the escaped form of the echoed value; body=%q", raw)
	}

	// Pretty-printed with two-space indentation, and newline-terminated.
	if !strings.HasSuffix(raw, "\n") {
		t.Errorf("body is not newline-terminated; body=%q", raw)
	}
	for _, want := range []string{"{\n  \"error\": ", "\n  \"kind\": ", "\n}\n"} {
		if !strings.Contains(raw, want) {
			t.Errorf("body is not pretty-printed with two-space indentation: missing %q; body=%q", want, raw)
		}
	}

	// The escaping is a wire-format concern only: the decoded value is the
	// original text, so the page shows the user what it rejected.
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decoding error body: %v", err)
	}
	if reason, _ := body["error"].(string); !strings.Contains(reason, "<script>alert(1)</script>") {
		t.Errorf("decoded error = %q, want it to name the rejected value verbatim", reason)
	}
}
