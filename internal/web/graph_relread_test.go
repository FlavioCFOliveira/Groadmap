package web

// Regression tests for rmp task #288 at the web graph data endpoint.
//
// The endpoint builds its own cypher.NewEngine and runs caller-supplied Cypher,
// so the misresolved relationship read the CLI now refuses was reachable from
// the query bar by the identical mechanism: on a node pair carrying edges BOTH
// ways, GoGraph hydrates the reverse leg of an incoming or undirected traversal
// from the FORWARD pair, reporting another relationship's type and the reversed
// orientation. Task #288's recorded functional requirements name this endpoint
// alongside `rmp graph query`, so closing only the CLI would have left the same
// wrong data one HTTP request away.
//
// The classifier is the SAME one the CLI uses
// (cypherguard.MisreadRelationshipReferences). These tests exist to keep it
// wired in here, and to keep the refusal from over-reaching: every fixture is a
// bidirectional pair with DIFFERENT types each way, because a one-way pair
// resolves correctly with or without the rule and could not tell them apart.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// relreadSeedQueries builds a pair of nodes carrying one edge EACH WAY, plus a
// one-way edge to a third node. The two directions carry different types, so a
// misresolved read is visible rather than masked by a coincidental match.
func relreadSeedQueries() []string {
	return []string{
		`CREATE (s:Spec {key:'user-authentication'})`,
		`CREATE (v:Test {key:'auth-token-expiry'})`,
		`CREATE (c:Code {path:'internal/auth/jwt.go'})`,
		`MATCH (s:Spec {key:'user-authentication'}), (v:Test {key:'auth-token-expiry'}) CREATE (s)-[:VERIFIED_BY]->(v)`,
		`MATCH (s:Spec {key:'user-authentication'}), (v:Test {key:'auth-token-expiry'}) CREATE (v)-[:COVERS]->(s)`,
		`MATCH (s:Spec {key:'user-authentication'}), (c:Code {path:'internal/auth/jwt.go'}) CREATE (s)-[:IMPLEMENTED_BY]->(c)`,
	}
}

// TestHandleGraphData_RefusesMisresolvedRelationshipReads is the core
// regression: every shape that reads a relationship bound by an incoming or
// undirected pattern is answered 400 with the relationship_read_direction kind,
// and nothing from the success shape leaks into the body.
func TestHandleGraphData_RefusesMisresolvedRelationshipReads(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, relreadSeedQueries()...)

	for label, q := range map[string]string{
		"undirected, type projected":  `MATCH (s:Spec)-[e]-(x) RETURN type(e), x.key`,
		"incoming, type projected":    `MATCH (s:Spec)<-[e]-(x) RETURN type(e)`,
		"incoming with a type filter": `MATCH (s:Spec)<-[e:COVERS]-(x) RETURN type(e)`,
		"undirected, whole value":     `MATCH (s:Spec)-[e]-(x) RETURN e`,
		"undirected, endpoints":       `MATCH (s:Spec)-[e]-(x) RETURN startNode(e).key, endNode(e).key`,
		"undirected, star projection": `MATCH (s:Spec)-[e]-(x) RETURN *`,
		"undirected, WHERE only":      `MATCH (s:Spec)-[e]-(x) WHERE type(e) = 'COVERS' RETURN x.key`,
		"undirected, ORDER BY only":   `MATCH (s:Spec)-[e]-(x) RETURN x.key ORDER BY type(e)`,
	} {
		t.Run(label, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {q}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding error body: %v; body=%q", err, rec.Body.String())
			}
			if body["kind"] != graphErrRelationshipDirection {
				t.Errorf("kind = %v, want %q; body=%q", body["kind"], graphErrRelationshipDirection, rec.Body.String())
			}
			reason, _ := body["error"].(string)
			for _, want := range []string{`"e"`, "outgoing", "UNION ALL"} {
				if !strings.Contains(reason, want) {
					t.Errorf("the reason must name %q so the caller can act on it; got %q", want, reason)
				}
			}
			if _, leaked := body["nodes"]; leaked {
				t.Errorf("a refused query must not leak the success shape; body=%q", rec.Body.String())
			}
		})
	}
}

// TestHandleGraphData_RefusalDoesNotReachCorrectReads pins the other half. Each
// query here resolves correctly on the very same bidirectional pair, so
// refusing it would cost reach for nothing — and the endpoint's DEFAULT query
// is among them, which matters most: it is an OPTIONAL MATCH over an outgoing
// pattern with a bound relationship variable, and a rule keyed on the variable
// rather than on the direction would have broken the graph page outright.
func TestHandleGraphData_RefusalDoesNotReachCorrectReads(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, relreadSeedQueries()...)

	t.Run("the endpoint default query still runs", func(t *testing.T) {
		rec := doGraphData(t, name, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("the default query must not be refused: status = %d; body=%q", rec.Code, rec.Body.String())
		}
		var view graphView
		if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
			t.Fatalf("decoding view: %v", err)
		}
		if len(view.Nodes) != 3 || len(view.Edges) != 3 {
			t.Errorf("default query returned %d nodes / %d edges, want 3 / 3", len(view.Nodes), len(view.Edges))
		}
	})

	for label, q := range map[string]string{
		"outgoing pattern":            `MATCH (s:Spec)-[e]->(x) RETURN type(e), x.key`,
		"outgoing anchored on target": `MATCH (x)-[e]->(s:Spec) RETURN type(e), x.key`,
		"anonymous undirected":        `MATCH (s:Spec)-[:COVERS]-(x) RETURN x.key`,
		"bound but never read":        `MATCH (s:Spec)-[e]-(x) RETURN x.key`,
		"named path over undirected":  `MATCH p=(s:Spec)-[e]-(x:Test) RETURN p`,
		"variable-length undirected":  `MATCH (s:Spec)-[e*1..1]-(x:Test) RETURN e`,
		"union of two outgoing legs":  `MATCH (s:Spec)-[e]->(x) RETURN type(e) AS t UNION ALL MATCH (x)-[e]->(s:Spec) RETURN type(e) AS t`,
	} {
		t.Run(label, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {q}})
			if rec.Code != http.StatusOK {
				t.Fatalf("this shape resolves correctly and must not be refused: status = %d; body=%q",
					rec.Code, rec.Body.String())
			}
		})
	}
}

// TestHandleGraphData_RelationshipDirectionReasonEscapesTheVariableName covers
// the untrusted-text path this refusal opens. The message names the relationship
// variable it rejected, and Cypher admits a backtick-quoted identifier holding
// arbitrary characters, so the echo is caller-controlled text reaching the
// response body — the same class of exposure the invalid_limit echo already
// carries.
//
// The assertion is on the WIRE bytes (no raw angle bracket survives) and on the
// DECODED value (still the original text), so the escaping is proven to be a
// serialization concern only and the query bar still shows the caller exactly
// what was rejected.
func TestHandleGraphData_RelationshipDirectionReasonEscapesTheVariableName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, relreadSeedQueries()...)

	const hostile = "<script>alert(1)</script>"
	rec := doGraphData(t, name, url.Values{
		"q": {"MATCH (s:Spec)-[`" + hostile + "`]-(x) RETURN type(`" + hostile + "`)"},
	})
	// Asserted, never skipped: the engine's parser is verified to admit a
	// backtick-quoted identifier here, so this refusal genuinely does carry
	// caller-controlled text into the body. A future engine that stopped
	// admitting it would change what this test proves, and must fail loudly
	// rather than quietly stop testing anything.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a backtick-quoted relationship variable must still reach the read-direction "+
			"refusal, so the echo it puts in the body stays under test: status = %d; body=%q",
			rec.Code, rec.Body.String())
	}

	raw := rec.Body.String()
	if strings.ContainsAny(raw, "<>") {
		t.Errorf("a raw angle bracket reached the response body: %q", raw)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding error body: %v; body=%q", err, raw)
	}
	if body["kind"] != graphErrRelationshipDirection {
		t.Fatalf("kind = %v, want %q", body["kind"], graphErrRelationshipDirection)
	}
	if reason, _ := body["error"].(string); !strings.Contains(reason, hostile) {
		t.Errorf("the DECODED reason must still show the caller the identifier it rejected; got %q", reason)
	}
}
