package web

// What became of rmp task #288 at the web graph data endpoint.
//
// #288 added a refusal here: a statement that read a relationship bound by an
// incoming or undirected fixed-length pattern was answered HTTP 400 with the
// kind relationship_read_direction, because on a node pair carrying edges BOTH
// ways GoGraph was reported to hydrate the reverse leg from the FORWARD pair.
// The refusal is withdrawn with the rest of the guard rail: this endpoint
// executes the statement it is given and refuses nothing on the ground of what
// the statement does (SPEC/WEB.md § Graph Data Endpoint).
//
// The claim the refusal rested on does not hold at the pinned engine, and
// SPEC/GRAPH.md § What Groadmap Does Not Check, item 5, now states the opposite:
// these reads resolve CORRECTLY, measured rather than assumed. Acceptance
// criterion 38's fourth bullet fixes the outcome, and this file is that
// criterion's assertion at the endpoint, as internal/commands/graph_relread_test.go
// is at the CLI.
//
// This endpoint is where the resolution is most directly observable. Its
// response carries each edge's type together with its startId and endId, so a
// reverse leg hydrated from the forward pair would show up here as the wrong
// type, or as the exact reverse of the orientation storage holds, rather than
// having to be inferred from a projected scalar.
//
// What is asserted, therefore:
//
//   - the incoming and undirected reads resolve to the right relationships, with
//     the right types and the right orientation;
//   - every shape the refusal used to reject EXECUTES and is answered 200, which
//     is the endpoint's contract and holds whatever the engine resolves;
//   - the outgoing forms return the graph, and the published UNION ALL rewrite
//     of the two outgoing legs returns both edges -- the forms that are correct
//     whatever the data, and so the fallback if the pin ever moves;
//   - the endpoint's own DEFAULT query still runs, which matters most: it is an
//     OPTIONAL MATCH over an outgoing pattern with a bound relationship variable,
//     and the withdrawn rule keyed on the variable rather than on the direction
//     would have broken the graph page outright.
//
// Every fixture is a bidirectional pair with DIFFERENT types each way, because a
// one-way pair resolves correctly with or without the disputed behaviour and
// could not tell the two readings apart.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
)

// relreadSeedQueries builds a pair of nodes carrying one edge EACH WAY, plus a
// one-way edge to a third node. The two directions carry different types, so a
// misresolved read would be visible rather than masked by a coincidental match.
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

// TestHandleGraphData_MisresolvedRelationshipReadsAreNoLongerRefused drives every
// shape the withdrawn refusal rejected and asserts each one now EXECUTES.
//
// It asserts the status and the kind set only. The corpus is deliberately wide
// and several of its entries project scalars, which the endpoint's result-to-graph
// extraction discards, so there is nothing in those responses to assert an
// outcome on; the outcome is asserted next door, on the shapes whose response
// carries the relationship itself. A 400 carrying a kind of its own is what a
// reintroduced refusal would look like, and is what this test fails on.
func TestHandleGraphData_MisresolvedRelationshipReadsAreNoLongerRefused(t *testing.T) {
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
			if rec.Code != http.StatusOK {
				kind, reason := decodeQueryError(t, rec.Body.Bytes())
				t.Fatalf("status = %d, want 200: this endpoint refuses nothing on the ground of what "+
					"a statement does (kind=%q, reason=%q)", rec.Code, kind, reason)
			}
			if strings.Contains(rec.Body.String(), "relationship_read_direction") {
				t.Errorf("the response names the withdrawn kind relationship_read_direction; body=%q", rec.Body.String())
			}
		})
	}
}

// resolvedEdges renders the view's edges as "TYPE source->target" strings, with
// each endpoint id resolved to the key or path the corresponding node carries.
//
// Resolving the ids is the whole point: the orientation is what a read that
// hydrated the reverse leg from the forward pair would get backwards, and an
// opaque numeric id would not show it. Every edge the endpoint returns has both
// endpoints among the returned nodes, because it drops orphan edges, so the
// lookup below cannot silently miss one.
func resolvedEdges(t *testing.T, view graphView) []string {
	t.Helper()
	names := make(map[float64]string, len(view.Nodes))
	for _, n := range view.Nodes {
		id, ok := n["id"].(float64)
		if !ok {
			t.Fatalf("node carries no numeric id: %v", n)
		}
		props, _ := n["properties"].(map[string]any)
		name, _ := props["key"].(string)
		if name == "" {
			name, _ = props["path"].(string)
		}
		if name == "" {
			t.Fatalf("node %v carries neither a key nor a path to name it by", n)
		}
		names[id] = name
	}
	out := make([]string, 0, len(view.Edges))
	for _, e := range view.Edges {
		typ, _ := e["type"].(string)
		start, okS := e["startId"].(float64)
		end, okE := e["endId"].(float64)
		if !okS || !okE {
			t.Fatalf("edge carries no numeric endpoints: %v", e)
		}
		out = append(out, typ+" "+names[start]+"->"+names[end])
	}
	sort.Strings(out)
	return out
}

// TestHandleGraphData_IncomingAndUndirectedReadsResolveCorrectly is acceptance
// criterion 38's fourth bullet asserted at the endpoint.
//
// Each case projects the relationship together with both its endpoints, so the
// response carries the edge itself rather than a scalar derived from it, and the
// assertion is on the resolved type AND the resolved orientation. A read that
// reported the forward leg for a reverse traversal fails on the type; one that
// inverted the endpoint pair fails on the orientation.
func TestHandleGraphData_IncomingAndUndirectedReadsResolveCorrectly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, relreadSeedQueries()...)

	const (
		forward = "VERIFIED_BY user-authentication->auth-token-expiry"
		reverse = "COVERS auth-token-expiry->user-authentication"
		oneWay  = "IMPLEMENTED_BY user-authentication->internal/auth/jwt.go"
		stale   = "SPEC/GRAPH.md § What Groadmap Does Not Check, item 5, and acceptance " +
			"criterion 38's fourth bullet, are stale and must be corrected"
	)

	for label, c := range map[string]struct {
		query string
		want  []string
	}{
		"undirected, far endpoint bound by key": {
			query: `MATCH (s:Spec {key:'user-authentication'})-[e]-(x:Test {key:'auth-token-expiry'}) RETURN s, e, x`,
			want:  []string{reverse, forward},
		},
		"undirected, far endpoint bound by label alone": {
			query: `MATCH (s:Spec {key:'user-authentication'})-[e]-(x:Test) RETURN s, e, x`,
			want:  []string{reverse, forward},
		},
		"undirected, far endpoint unbound": {
			query: `MATCH (s:Spec {key:'user-authentication'})-[e]-(x) RETURN s, e, x`,
			want:  []string{reverse, oneWay, forward},
		},
		"incoming, far endpoint bound by key": {
			query: `MATCH (s:Spec {key:'user-authentication'})<-[e]-(x:Test {key:'auth-token-expiry'}) RETURN s, e, x`,
			want:  []string{reverse},
		},
		"incoming, far endpoint bound by label alone": {
			query: `MATCH (s:Spec {key:'user-authentication'})<-[e]-(x:Test) RETURN s, e, x`,
			want:  []string{reverse},
		},
		"incoming, with a relationship type filter": {
			query: `MATCH (s:Spec)<-[e:COVERS]-(x:Test) RETURN s, e, x`,
			want:  []string{reverse},
		},
		"undirected, gated by a WHERE over type(e)": {
			query: `MATCH (s:Spec)-[e]-(x:Test) WHERE type(e) = 'COVERS' RETURN s, e, x`,
			want:  []string{reverse},
		},
		"undirected, gated by a WHERE selecting the forward leg": {
			query: `MATCH (s:Spec)-[e]-(x:Test) WHERE type(e) = 'VERIFIED_BY' RETURN s, e, x`,
			want:  []string{forward},
		},
	} {
		t.Run(label, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {c.query}})
			if rec.Code != http.StatusOK {
				kind, reason := decodeQueryError(t, rec.Body.Bytes())
				t.Fatalf("status = %d, want 200 (kind=%q, reason=%q)", rec.Code, kind, reason)
			}
			var view graphView
			if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
				t.Fatalf("decoding view: %v", err)
			}
			got := resolvedEdges(t, view)
			want := append([]string(nil), c.want...)
			sort.Strings(want)
			if strings.Join(got, " | ") != strings.Join(want, " | ") {
				t.Fatalf("the read resolved to %v, want %v: %s", got, want, stale)
			}
		})
	}
}

// TestHandleGraphData_OutgoingAndRewrittenFormsReturnTheGraph pins the forms that
// are correct whatever the data and whatever the engine, because nothing about
// the stored orientation has to be recovered: the outgoing anchorings and the
// published UNION ALL rewrite return the edges they name, and the endpoint's
// default query returns the whole seeded graph. They are the fallback the
// specification names if the pinned engine is ever replaced by one that resolves
// the undirected reads differently.
func TestHandleGraphData_OutgoingAndRewrittenFormsReturnTheGraph(t *testing.T) {
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

	// The UNION ALL of the two outgoing legs is the rewrite SPEC/GRAPH.md
	// publishes for an undirected traversal, and it is the one shape that must
	// keep working whatever engine is pinned. Both endpoints are projected
	// alongside the relationship, so neither edge is dropped as an orphan and the
	// two types are observable in the response.
	t.Run("the UNION ALL rewrite returns both legs", func(t *testing.T) {
		const q = `MATCH (s:Spec {key:'user-authentication'})-[e]->(x:Test) RETURN s, e, x ` +
			`UNION ALL MATCH (x:Test)-[e]->(s:Spec {key:'user-authentication'}) RETURN s, e, x`
		rec := doGraphData(t, name, url.Values{"q": {q}})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
		}
		var view graphView
		if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
			t.Fatalf("decoding view: %v", err)
		}
		types := map[string]bool{}
		for _, e := range view.Edges {
			if s, ok := e["type"].(string); ok {
				types[s] = true
			}
		}
		if !types["VERIFIED_BY"] || !types["COVERS"] {
			t.Errorf("the rewrite returned edge types %v, want both VERIFIED_BY and COVERS: "+
				"the union of the two outgoing legs is what recovers an undirected traversal", types)
		}
	})

	for label, q := range map[string]string{
		"outgoing pattern":            `MATCH (s:Spec)-[e]->(x) RETURN s, e, x`,
		"outgoing anchored on target": `MATCH (x)-[e]->(s:Spec) RETURN x, e, s`,
		"anonymous undirected":        `MATCH (s:Spec)-[:COVERS]-(x) RETURN x.key`,
		"bound but never read":        `MATCH (s:Spec)-[e]-(x) RETURN x.key`,
		"named path over undirected":  `MATCH p=(s:Spec)-[e]-(x:Test) RETURN p`,
		"variable-length undirected":  `MATCH (s:Spec)-[e*1..1]-(x:Test) RETURN e`,
	} {
		t.Run(label, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {q}})
			if rec.Code != http.StatusOK {
				kind, reason := decodeQueryError(t, rec.Body.Bytes())
				t.Fatalf("status = %d, want 200 (kind=%q, reason=%q)", rec.Code, kind, reason)
			}
		})
	}
}

// TestHandleGraphData_BacktickIdentifiersReachTheEngine keeps under test the
// input class the withdrawn refusal's message used to echo: Cypher admits a
// backtick-quoted identifier holding arbitrary characters, including markup.
//
// Nothing echoes it any more — the endpoint's only caller-derived echo is the
// invalid-limit message — so what is asserted is that such a statement reaches
// the ENGINE rather than a rule of this endpoint's, and that whatever comes back
// carries no raw angle bracket. The HTML-safety property of the body is a
// property of every response this endpoint writes, and this is the statement most
// likely to put markup into one.
func TestHandleGraphData_BacktickIdentifiersReachTheEngine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, relreadSeedQueries()...)

	const hostile = "<script>alert(1)</script>"
	rec := doGraphData(t, name, url.Values{
		"q": {"MATCH (s:Spec)-[`" + hostile + "`]-(x) RETURN type(`" + hostile + "`)"},
	})
	if rec.Code != http.StatusOK {
		kind, reason := decodeQueryError(t, rec.Body.Bytes())
		if kind != graphErrExecution {
			t.Fatalf("status = %d, kind = %q: a backtick-quoted relationship variable must reach the "+
				"engine, and any failure it causes is the engine's (reason=%q)", rec.Code, kind, reason)
		}
	}
	if strings.ContainsAny(rec.Body.String(), "<>") {
		t.Errorf("a raw angle bracket reached the response body: %q", rec.Body.String())
	}
}
