package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
