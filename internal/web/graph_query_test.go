package web

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
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
// the engine's transactional write path, so the graph data endpoint has genuine
// nodes and edges to extract. It mirrors the minimal write sequence
// commands/graph.go runGraphExecute performs (recovery.Open -> wal.Open ->
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

// nodeKeys decodes a graph data response and returns the `key` property of every
// node it carries, so a read-back can be asserted on content rather than on a
// count alone.
func nodeKeys(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("read-back status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding read-back: %v; body=%q", err, rec.Body.String())
	}
	keys := make([]string, 0, len(view.Nodes))
	for _, n := range view.Nodes {
		props, _ := n["properties"].(map[string]any)
		if k, ok := props["key"].(string); ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

// TestHandleGraphData_ExecutesWriteStatements is SPEC/WEB.md Acceptance
// Criterion 47, and it is the criterion this whole task exists to satisfy: a
// statement submitted to the graph data endpoint is executed whatever it does,
// its change is committed, and a LATER request finds it.
//
// The read-back is required by the criterion and is not a courtesy. The endpoint
// opens the store per request, so a write that executed against the request's
// own in-memory graph and was discarded when the request ended would answer 200
// exactly as a real write does; only a second request, over a second store open,
// can tell the two apart. That is precisely the failure an endpoint built
// without a transactional store exhibits.
//
// The status alone establishes nothing in the other direction either: before
// this change the endpoint answered 400 with kind not_read_only, and after the
// guard rail was withdrawn but before the engine moved, engine.Run answered
// "Run does not execute write or DDL statements" — a 400 with kind execution.
// Both are refusals; the criterion is met only by a 200 plus a read-back.
func TestHandleGraphData_ExecutesWriteStatements(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	before := nodeKeys(t, doGraphData(t, name, nil))
	if slices.Contains(before, "web-probe") {
		t.Fatalf("the probe node is present before anything created it: %v", before)
	}

	// CREATE: executed, committed, and found by a separate request.
	rec := doGraphData(t, name, url.Values{"q": {`CREATE (n:WebProbe {key:'web-probe'})`}})
	if rec.Code != http.StatusOK {
		t.Fatalf("CREATE status = %d, want 200: the query bar executes what it is given; body=%q", rec.Code, rec.Body.String())
	}
	after := nodeKeys(t, doGraphData(t, name, nil))
	if !slices.Contains(after, "web-probe") {
		t.Fatalf("after a CREATE through the endpoint the node is absent on a follow-up request (%v). "+
			"The statement executed against the request's own in-memory graph and was discarded, which is "+
			"a 200 reporting a write that does not exist (SPEC/WEB.md Acceptance Criterion 47)", after)
	}

	// The store is checkpointed: the snapshot exists and the log is truncated
	// (SPEC/GRAPH.md § Synchronous Checkpoint on Write).
	graphDir := webGraphDir(t, name)
	if _, err := os.Stat(filepath.Join(graphDir, "snapshot", "manifest.json")); err != nil {
		t.Errorf("no snapshot/manifest.json after a write through the endpoint: %v", err)
	}
	walInfo, err := os.Stat(filepath.Join(graphDir, "wal"))
	if err != nil {
		t.Fatalf("stat wal: %v", err)
	}
	walAfterWrite := walInfo.Size()

	// SET: the property change is committed and visible to a later request.
	rec = doGraphData(t, name, url.Values{"q": {`MATCH (n:WebProbe {key:'web-probe'}) SET n.key = 'web-probe-renamed'`}})
	if rec.Code != http.StatusOK {
		t.Fatalf("SET status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	renamed := nodeKeys(t, doGraphData(t, name, nil))
	if slices.Contains(renamed, "web-probe") || !slices.Contains(renamed, "web-probe-renamed") {
		t.Fatalf("after a SET through the endpoint the property change is not visible on a follow-up request: %v", renamed)
	}

	// DETACH DELETE: the node is gone on a later request.
	rec = doGraphData(t, name, url.Values{"q": {`MATCH (n:WebProbe) DETACH DELETE n`}})
	if rec.Code != http.StatusOK {
		t.Fatalf("DETACH DELETE status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	gone := nodeKeys(t, doGraphData(t, name, nil))
	if slices.Contains(gone, "web-probe-renamed") {
		t.Fatalf("after a DETACH DELETE through the endpoint the node is still present: %v", gone)
	}
	if !slices.Equal(gone, before) {
		t.Errorf("the seeded graph did not survive the three writes: %v, want %v", gone, before)
	}

	// Each write checkpointed, so the log never grew without bound.
	walInfo, err = os.Stat(filepath.Join(graphDir, "wal"))
	if err != nil {
		t.Fatalf("stat wal: %v", err)
	}
	if walInfo.Size() > walAfterWrite*4 {
		t.Errorf("the write-ahead log is %d bytes after three writes and was %d after the first; "+
			"a write that did not checkpoint leaves the log growing", walInfo.Size(), walAfterWrite)
	}
}

// TestHandleGraphData_StatementThatWritesNothingLeavesTheStoreByteIdentical is
// SPEC/WEB.md Acceptance Criterion 19 and the other half of the checkpoint rule:
// the checkpoint is gated on the write-ahead log having grown, so an ordinary
// read neither snapshots nor truncates.
//
// Without the gate every page load would rewrite a full snapshot of the whole
// graph — a cost proportional to the graph, paid for no change at all — and
// would shorten the history a later recovery replays.
func TestHandleGraphData_StatementThatWritesNothingLeavesTheStoreByteIdentical(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	graphDir := webGraphDir(t, name)

	// One write first, so the store HAS a snapshot: a store that never
	// checkpointed would satisfy "snapshot unchanged" vacuously.
	if rec := doGraphData(t, name, url.Values{"q": {`CREATE (n:WebProbe {key:'settle'})`}}); rec.Code != http.StatusOK {
		t.Fatalf("seeding write status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(graphDir, "snapshot", "manifest.json")); err != nil {
		t.Fatalf("the premise fails: no snapshot after a write: %v", err)
	}

	before := storeFingerprint(t, graphDir)

	for _, q := range []string{
		defaultGraphQuery,
		"MATCH (n) RETURN n",
		"MATCH (n) RETURN count(n)",
		"MATCH (n:Absent) RETURN n",
	} {
		if rec := doGraphData(t, name, url.Values{"q": {q}}); rec.Code != http.StatusOK {
			t.Fatalf("%q status = %d, want 200; body=%q", q, rec.Code, rec.Body.String())
		}
	}

	if after := storeFingerprint(t, graphDir); !maps.Equal(before, after) {
		t.Errorf("statements that wrote nothing changed the store on disk.\n before: %v\n after:  %v\n"+
			"A statement whose transaction appended nothing MUST NOT checkpoint and MUST NOT truncate "+
			"(SPEC/GRAPH.md § What a Statement That Writes Nothing Changes on Disk, rules 2 and 3)", before, after)
	}
}

// storeFingerprint records the size and content hash of the write-ahead log and
// of every file under snapshot/, which is what "byte for byte unchanged" means
// for the two artefacts a checkpoint rewrites.
func storeFingerprint(t *testing.T, graphDir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rel := range []string{"wal", "snapshot"} {
		root := filepath.Join(graphDir, rel)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			raw, readErr := os.ReadFile(path) //nolint:gosec // path derives from t.TempDir via HOME
			if readErr != nil {
				return readErr
			}
			key, relErr := filepath.Rel(graphDir, path)
			if relErr != nil {
				return relErr
			}
			out[key] = fmt.Sprintf("%d:%x", len(raw), sha256.Sum256(raw))
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("fingerprinting %s: %v", root, err)
		}
	}
	return out
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

	resp, err := http.Get(srv.URL + "/roadmaps/" + name + "/graph/data?q=" + url.QueryEscape("MATCH (n) RETURN") + "&limit=7")
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

// TestHandleGraphData_SpoofedDDLKeywordsAreNoLongerRefused is the inversion of a
// security-audit regression, kept rather than deleted because the finding it
// carries is still true and only the verdict changed.
//
// The audit proved that a DDL keyword spelled with a non-ASCII letter that
// Unicode UPPERCASING maps onto ASCII (U+0131 dotless i uppercases to 'I') was
// not seen as DDL by the withdrawn guard rail's case-insensitive regexp, while
// the engine's own dispatcher — which decides on strings.ToUpper — routed it
// straight to its DDL executor. The guard rail is gone, so there is no bypass
// left to prove: the endpoint executes schema DDL by design, in every spelling,
// over an unauthenticated GET (SPEC/WEB.md § Security and Constraints, rule 3).
//
// What the test holds is the closed kind set of Acceptance Criterion 123: each
// of these forms is answered either 200, or 400 with the ENGINE's own diagnostic
// under kind `execution`. A body carrying any withdrawn kind — not_read_only
// above all — means a refusal was reintroduced without a change to the
// specification, and that is what this test fails on.
func TestHandleGraphData_SpoofedDDLKeywordsAreNoLongerRefused(t *testing.T) {
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
			switch rec.Code {
			case http.StatusOK:
				return
			case http.StatusBadRequest:
				kind, reason := decodeQueryError(t, rec.Body.Bytes())
				if kind != graphErrExecution {
					t.Fatalf("kind = %q, want %q: this endpoint publishes exactly invalid_limit and "+
						"execution, and refuses nothing on the ground of what a statement does "+
						"(SPEC/WEB.md Acceptance Criterion 123); reason=%q", kind, graphErrExecution, reason)
				}
			default:
				t.Fatalf("status = %d, want 200 or 400; body=%q", rec.Code, rec.Body.String())
			}
		})
	}

	// The control that keeps the above from passing vacuously: an ASCII schema
	// DDL executes and PERSISTS, so the endpoint really does run DDL and the
	// spoofed spellings above are not all simply failing in the parser.
	if rec := doGraphData(t, name, url.Values{"q": {"CREATE INDEX web_spec_key FOR (n:Spec) ON (n.key)"}}); rec.Code != http.StatusOK {
		t.Fatalf("an ASCII CREATE INDEX status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if names := schemaNamesOnTheStore(t, name, "SHOW INDEXES"); !slices.Contains(names, "web_spec_key") {
		t.Fatalf("SHOW INDEXES over the store reports %v: an index created through the endpoint must persist", names)
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

// TestHandleGraphData_InvalidLimitIsResolvedBeforeTheStatementRuns pins the one
// ordering the endpoint still has between its two failure kinds: the `limit` is
// resolved first, so a request carrying both an invalid `limit` and a statement
// that would have written is answered `invalid_limit` and the statement is not
// executed (SPEC/WEB.md § Query-Bar Error Handling, rule 5; Acceptance
// Criterion 123).
//
// The write is what makes the assertion decisive. "The statement was not
// executed" is observable only through a statement whose execution leaves a
// trace, and a read leaves none — so the probe is a CREATE, and the read-back
// afterwards is what proves nothing ran. The four-deep precedence rule the
// endpoint used to publish is withdrawn with the guard rail that produced it;
// there is nothing else left to order.
func TestHandleGraphData_InvalidLimitIsResolvedBeforeTheStatementRuns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	const writeQuery = `CREATE (n:WebProbe {key:'never-created'})`
	const badLimit = "7"

	// Control A: the statement alone EXECUTES under an allowed limit, so the
	// combined case below cannot pass merely because the statement never runs.
	rec := doGraphData(t, name, url.Values{"q": {`CREATE (n:WebProbe {key:'control'})`}, "limit": {"100"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("control A: status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if keys := nodeKeys(t, doGraphData(t, name, nil)); !slices.Contains(keys, "control") {
		t.Fatalf("control A: the write did not land (%v), so this test cannot tell an unexecuted "+
			"statement from an executed one", keys)
	}

	// Control B: the limit alone IS classified invalid_limit.
	rec = doGraphData(t, name, url.Values{"limit": {badLimit}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("control B: status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	if kind, _ := decodeQueryError(t, rec.Body.Bytes()); kind != graphErrInvalidLimit {
		t.Fatalf("control B: kind = %q, want %q", kind, graphErrInvalidLimit)
	}

	// The claim: both wrong at once resolves to invalid_limit.
	rec = doGraphData(t, name, url.Values{"q": {writeQuery}, "limit": {badLimit}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	kind, reason := decodeQueryError(t, rec.Body.Bytes())
	if kind != graphErrInvalidLimit {
		t.Errorf("kind = %q, want %q: the limit is resolved before the statement runs "+
			"(SPEC/WEB.md § Query-Bar Error Handling, rule 5)", kind, graphErrInvalidLimit)
	}
	// The reason names the rejected value (the allowed values named in the same
	// message contain no '7').
	if !strings.Contains(reason, badLimit) {
		t.Errorf("error = %q, want it to name the rejected limit %q", reason, badLimit)
	}

	// The statement never ran: the node it would have created is absent.
	if keys := nodeKeys(t, doGraphData(t, name, nil)); slices.Contains(keys, "never-created") {
		t.Errorf("the CREATE executed despite the invalid limit (%v): the request must be rejected "+
			"before the statement runs", keys)
	}
}

// TestHandleGraphData_TheStoreOpenBoundary pins the two halves of the boundary
// in SPEC/WEB.md § Query-Bar Error Handling, rule 6, against a roadmap whose
// graph store cannot be opened:
//
//   - The 500 half: a request the endpoint accepts reaches the open, the open
//     fails, and that is an internal read error — HTTP 500, and NOT a 400 with
//     kind execution. The boundary is drawn at the moment the failure surfaces,
//     and a failure to open surfaces before the statement runs.
//   - The 400 half: an invalid limit still answers 400 over the very same
//     unopenable store, which can only be true if the limit is resolved BEFORE
//     the store is opened (rule 5). The 500 above is what makes this
//     non-vacuous: it proves the store really is unopenable, so the 400 cannot
//     have come from a successful read.
//
// The 400 half used to carry four more cases, each a guard-rail refusal decided
// before the open. They are withdrawn, and the endpoint now opens the store for
// every request whose limit it accepted — which is why the 500 cases below
// include a statement that would once have been refused without the store ever
// being touched.
func TestHandleGraphData_TheStoreOpenBoundary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)
	corruptGraphWAL(t, name)

	// The 500 half.
	for label, params := range map[string]url.Values{
		"the default query":       nil,
		"an ordinary read":        {"q": {"MATCH (n) RETURN n"}},
		"a statement that writes": {"q": {`CREATE (n:WebProbe {key:'p'})`}},
		"a schema statement":      {"q": {"CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"}},
	} {
		t.Run(label, func(t *testing.T) {
			rec := doGraphData(t, name, params)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500 over an unopenable store; body=%q", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), `"kind"`) {
				t.Errorf("the 500 of an internal read error must not carry the query-bar error shape; body=%q", rec.Body.String())
			}
		})
	}

	// The 400 half: the only rejection that still precedes the store open.
	t.Run("an invalid limit is still decided before the open", func(t *testing.T) {
		rec := doGraphData(t, name, url.Values{"limit": {"7"}})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: the limit is resolved before the store is opened; body=%q", rec.Code, rec.Body.String())
		}
		if kind, _ := decodeQueryError(t, rec.Body.Bytes()); kind != graphErrInvalidLimit {
			t.Errorf("kind = %q, want %q", kind, graphErrInvalidLimit)
		}
	})
}

// TestHandleGraphData_ErrorBodyShape pins the failure body's exact field set for
// every failure class the endpoint publishes — the set enumerated in
// SPEC/WEB.md § Query-Bar Error Handling, rule 4 — exactly two fields, `error`
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
		{"invalid limit", url.Values{"limit": {"7"}}, graphErrInvalidLimit},
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

// TestHandleGraphData_PublishesExactlyTwoKinds asserts the CLOSED value set of
// SPEC/WEB.md § Query-Bar Error Handling, rule 4, which Acceptance Criterion 123
// requires to be asserted as a closed set and not only as two members present:
// "a third value is exactly what an endpoint that started refusing statements
// again would publish".
//
// The corpus is deliberately made of the statements the withdrawn guard rail
// refused — a write, a DDL statement, a schema-introspection command at both
// spacings, and an undirected relationship read — plus the two that genuinely
// fail. Each is either served or fails in the engine; none of them may produce a
// kind of its own.
func TestHandleGraphData_PublishesExactlyTwoKinds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	published := map[string]bool{graphErrInvalidLimit: true, graphErrExecution: true}
	withdrawn := []string{"not_read_only", "schema_introspection", "relationship_read_direction", "invalid_keyword_spacing"}

	probes := []url.Values{
		{"q": {`MATCH (n) DETACH DELETE n`}},
		{"q": {`CREATE (n:WebProbe {key:'p'})`}},
		{"q": {"CREATE INDEX web_idx FOR (n:Spec) ON (n.key)"}},
		{"q": {"DROP INDEX web_idx"}},
		{"q": {"SHOW INDEXES"}},
		{"q": {"SHOW  INDEXES"}},
		{"q": {"SHOW\tCONSTRAINTS"}},
		{"q": {`MATCH (a)-[e]-(b) RETURN type(e)`}},
		{"q": {`MATCH (a)<-[e]-(b) RETURN type(e)`}},
		{"q": {`MATCH (n) RETURN`}},
		{"q": {"SHOW DATABASES"}},
		{"limit": {"7"}},
	}

	for _, params := range probes {
		t.Run(params.Encode(), func(t *testing.T) {
			rec := doGraphData(t, name, params)
			if rec.Code == http.StatusOK {
				return
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 200 or 400; body=%q", rec.Code, rec.Body.String())
			}
			kind, reason := decodeQueryError(t, rec.Body.Bytes())
			if !published[kind] {
				t.Fatalf("kind = %q, which is outside the closed set {invalid_limit, execution} "+
					"SPEC/WEB.md § Query-Bar Error Handling rule 4 publishes; reason=%q", kind, reason)
			}
			for _, gone := range withdrawn {
				if strings.Contains(rec.Body.String(), gone) {
					t.Errorf("the body names the withdrawn kind %q; body=%q", gone, rec.Body.String())
				}
			}
		})
	}
}
