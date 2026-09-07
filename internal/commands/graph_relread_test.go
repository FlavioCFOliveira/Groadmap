package commands

// Tests for the relationship-read shapes SPEC/GRAPH.md § What Groadmap Does Not
// Check, item 5, is about, measured against a node pair joined in BOTH
// directions -- the only shape the item concerns.
//
// Groadmap used to REFUSE an undirected or incoming read that bound a
// relationship variable. That refusal is withdrawn along with every other, so
// the statements below now execute, and this file asserts what the engine
// answers rather than that Groadmap declined to ask it.
//
// WHAT IS ASSERTED HERE. Item 5 states that an undirected or incoming
// fixed-length read is reported CORRECTLY at the pinned engine, and acceptance
// criterion 38's fourth bullet fixes that outcome shape by shape. This file is
// the assertion that bullet demands. It fails in both of the directions the
// bullet cares about: it fails if the engine stops resolving these reads
// correctly, naming item 5 as the specification that has gone stale, and it
// fails if a refusal of the shape is reintroduced, because a refused statement
// returns an error instead of rows.
//
// The fixture is a pair joined both ways with a DIFFERENT type each way, which
// the criterion requires: a pair whose two legs share a type cannot tell a
// correctly resolved read from one that reported the other leg, because the two
// answers would be the same string.
//
// Also asserted, because they are what a caller falls back on if the pin ever
// moves and the property above stops holding: the outgoing forms and the two
// rewrites the specification publishes, which are correct whatever the data
// because nothing about the stored orientation has to be recovered; and the
// shapes item 5 names alongside the fixed-length ones -- a variable-length
// relationship, a projected named path, an anonymous relationship, and a bare
// DELETE.

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	relReadSpecKey  = "graph-read-direction"
	relReadTestKey  = "graph_relread_test.go"
	relReadCodePath = "internal/commands/graph.go"
)

// seedBidirectionalPair creates the fixture every case below shares: a Spec node
// and a Test node carrying one edge EACH WAY -- VERIFIED_BY running spec -> test
// and COVERS running test -> spec -- plus a one-way IMPLEMENTED_BY edge to a
// Code node.
//
// The two types are deliberately DIFFERENT. A pair whose two edges share a type
// cannot tell a correctly resolved read from one that reported the other edge,
// because the two answers would be the same string. The one-way IMPLEMENTED_BY
// edge is present so a case can exercise the same traversal shape on a pair
// joined in ONE direction only, which is the easy half of the problem: there,
// an implementation that inferred the relationship from the endpoint pair alone
// would answer correctly too.
func seedBidirectionalPair(t *testing.T, roadmap string) {
	t.Helper()
	for _, q := range []string{
		"CREATE (:Spec {key:'" + relReadSpecKey + "'}), (:Test {key:'" + relReadTestKey + "'}), " +
			"(:Code {path:'" + relReadCodePath + "'})",
		"MATCH (s:Spec {key:'" + relReadSpecKey + "'}), (v:Test {key:'" + relReadTestKey + "'}) " +
			"MERGE (s)-[:VERIFIED_BY]->(v)",
		"MATCH (s:Spec {key:'" + relReadSpecKey + "'}), (v:Test {key:'" + relReadTestKey + "'}) " +
			"MERGE (v)-[:COVERS]->(s)",
		"MATCH (s:Spec {key:'" + relReadSpecKey + "'}), (c:Code {path:'" + relReadCodePath + "'}) " +
			"MERGE (s)-[:IMPLEMENTED_BY]->(c)",
	} {
		if err := runGraphExecute([]string{"-r", roadmap, "--query", q}); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
}

// graphQueryRows runs a read and returns its rows, failing the test if the read
// errors or the output is not the columns/rows shape.
func graphQueryRows(t *testing.T, roadmap, query string) [][]any {
	t.Helper()
	stdout, _ := captureStdStreams(t, func() {
		if err := runGraphExecute([]string{"-r", roadmap, "--query", query}); err != nil {
			t.Fatalf("read failed: %v\nquery=%s", err, query)
		}
	})
	var parsed struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("stdout is not the columns/rows shape: %v\nstdout=%q", err, stdout)
	}
	return parsed.Rows
}

// typeCounts tallies the string in column 0 of each row.
func typeCounts(rows [][]any) map[string]int {
	got := make(map[string]int, len(rows))
	for _, r := range rows {
		s, _ := r[0].(string)
		got[s]++
	}
	return got
}

// TestGraphRead_OutgoingFormsResolveCorrectly pins the two rewrites the
// specification publishes for reading a bidirectional pair. Reading through an
// outgoing pattern is correct whatever the data, and both directions are read in
// one statement as the UNION ALL of the two outgoing legs.
func TestGraphRead_OutgoingFormsResolveCorrectly(t *testing.T) {
	const roadmap = "graph-relread-core"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedBidirectionalPair(t, roadmap)

	t.Run("the UNION ALL of the two outgoing legs reports both", func(t *testing.T) {
		got := typeCounts(graphQueryRows(t, roadmap,
			"MATCH (a:Spec {key:'"+relReadSpecKey+"'})-[e]->(x:Test {key:'"+relReadTestKey+"'}) RETURN type(e) AS t "+
				"UNION ALL "+
				"MATCH (x:Test {key:'"+relReadTestKey+"'})-[e]->(a:Spec {key:'"+relReadSpecKey+"'}) RETURN type(e) AS t"))
		if got["VERIFIED_BY"] != 1 || got["COVERS"] != 1 {
			t.Fatalf("the union of the two outgoing legs reported %v, want one VERIFIED_BY and one COVERS: "+
				"the rewrite the specification names must recover the whole undirected read", got)
		}
	})

	t.Run("the outgoing read reports the true type and orientation", func(t *testing.T) {
		rows := graphQueryRows(t, roadmap,
			"MATCH (x)-[e]->(s:Spec {key:'"+relReadSpecKey+"'}) "+
				"RETURN type(e), startNode(e).key, endNode(e).key")
		if len(rows) != 1 {
			t.Fatalf("expected exactly one edge arriving at the Spec node, got %v", rows)
		}
		if rows[0][0] != "COVERS" {
			t.Errorf("outgoing read reported type %v, want COVERS", rows[0][0])
		}
		if rows[0][1] != relReadTestKey || rows[0][2] != relReadSpecKey {
			t.Errorf("outgoing read reported orientation %v -> %v, want %s -> %s",
				rows[0][1], rows[0][2], relReadTestKey, relReadSpecKey)
		}
	})

}

// TestGraphRead_IncomingAndUndirectedResolveCorrectly is the assertion
// acceptance criterion 38's fourth bullet demands, over the fixed-length shapes
// SPEC/GRAPH.md § What Groadmap Does Not Check, item 5, states are resolved
// correctly at the pinned engine.
//
// Every case runs against a pair joined in BOTH directions, which is the shape
// that separates a resolved read from an inferred one, and every case asserts the
// ROWS rather than the exit code: a read that reported the wrong leg exits 0 just
// as a correct one does.
//
// The undirected and the incoming spelling are each exercised with the far
// endpoint bound by key and bound by label alone, because the two bind the
// pattern differently and only measuring both establishes that the resolution
// does not depend on which.
func TestGraphRead_IncomingAndUndirectedResolveCorrectly(t *testing.T) {
	const roadmap = "graph-relread-direction"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedBidirectionalPair(t, roadmap)

	// stale names the specification that must be corrected if one of these
	// assertions ever fails, so the failure arrives as a specification question
	// rather than as an unexplained red test.
	const stale = "SPEC/GRAPH.md § What Groadmap Does Not Check, item 5, and acceptance " +
		"criterion 38's fourth bullet, are stale and must be corrected"

	t.Run("the undirected projection reports each incident type exactly once", func(t *testing.T) {
		for label, q := range map[string]string{
			"far endpoint bound by key": "MATCH (s:Spec {key:'" + relReadSpecKey + "'})-[e]-(x:Test {key:'" +
				relReadTestKey + "'}) RETURN type(e)",
			"far endpoint bound by label alone": "MATCH (s:Spec {key:'" + relReadSpecKey +
				"'})-[e]-(x:Test) RETURN type(e)",
		} {
			t.Run(label, func(t *testing.T) {
				got := typeCounts(graphQueryRows(t, roadmap, q))
				if got["VERIFIED_BY"] != 1 || got["COVERS"] != 1 || len(got) != 2 {
					t.Fatalf("the undirected projection reported %v, want one VERIFIED_BY and one COVERS: "+
						"reporting the forward type twice and the reverse not at all would mean %s", got, stale)
				}
			})
		}
	})

	t.Run("the undirected projection agrees with the UNION ALL of the two outgoing legs", func(t *testing.T) {
		undirected := typeCounts(graphQueryRows(t, roadmap,
			"MATCH (s:Spec {key:'"+relReadSpecKey+"'})-[e]-(x) RETURN type(e)"))
		rewritten := typeCounts(graphQueryRows(t, roadmap,
			"MATCH (a:Spec {key:'"+relReadSpecKey+"'})-[e]->(x) RETURN type(e) AS t "+
				"UNION ALL "+
				"MATCH (x)-[e]->(a:Spec {key:'"+relReadSpecKey+"'}) RETURN type(e) AS t"))
		want := map[string]int{"VERIFIED_BY": 1, "COVERS": 1, "IMPLEMENTED_BY": 1}
		for _, c := range []struct {
			name string
			got  map[string]int
		}{{"undirected", undirected}, {"UNION ALL rewrite", rewritten}} {
			if len(c.got) != len(want) {
				t.Fatalf("the %s read reported %v, want %v: %s", c.name, c.got, want, stale)
			}
			for typ, n := range want {
				if c.got[typ] != n {
					t.Fatalf("the %s read reported %v, want %v: %s", c.name, c.got, want, stale)
				}
			}
		}
	})

	t.Run("the incoming read reports the reverse leg with the stored orientation", func(t *testing.T) {
		for label, q := range map[string]string{
			"far endpoint bound by key": "MATCH (s:Spec {key:'" + relReadSpecKey + "'})<-[e]-(x:Test {key:'" +
				relReadTestKey + "'}) RETURN type(e), startNode(e).key, endNode(e).key",
			"far endpoint bound by label alone": "MATCH (s:Spec {key:'" + relReadSpecKey +
				"'})<-[e]-(x:Test) RETURN type(e), startNode(e).key, endNode(e).key",
		} {
			t.Run(label, func(t *testing.T) {
				rows := graphQueryRows(t, roadmap, q)
				if len(rows) != 1 {
					t.Fatalf("the incoming read reported %d rows, want exactly the one reverse leg: %v", len(rows), rows)
				}
				if rows[0][0] != "COVERS" {
					t.Fatalf("the incoming read reported type %v, want COVERS: reporting the FORWARD "+
						"type here would mean %s", rows[0][0], stale)
				}
				if rows[0][1] != relReadTestKey || rows[0][2] != relReadSpecKey {
					t.Fatalf("the incoming read reported the orientation %v -> %v, want %s -> %s: "+
						"reporting the exact reverse of what storage holds would mean %s",
						rows[0][1], rows[0][2], relReadTestKey, relReadSpecKey, stale)
				}
			})
		}
	})

	t.Run("a WHERE over type(e) selects the leg it names and no row for its sibling", func(t *testing.T) {
		selected := graphQueryRows(t, roadmap,
			"MATCH (s:Spec {key:'"+relReadSpecKey+"'})-[e]-(x:Test) WHERE type(e) = 'COVERS' "+
				"RETURN type(e), startNode(e).key, endNode(e).key")
		if len(selected) != 1 || selected[0][0] != "COVERS" ||
			selected[0][1] != relReadTestKey || selected[0][2] != relReadSpecKey {
			t.Fatalf("the predicate over the undirected read selected %v, want the single COVERS leg "+
				"running %s -> %s: a row discarded inside the engine would mean %s",
				selected, relReadTestKey, relReadSpecKey, stale)
		}
		// The complementary half. A resolution that reported the forward leg for
		// the reverse traversal would return a row here, and a predicate that
		// admitted everything would too.
		crossed := graphQueryRows(t, roadmap,
			"MATCH (s:Spec {key:'"+relReadSpecKey+"'})<-[e]-(x:Test) WHERE type(e) = 'VERIFIED_BY' RETURN x.key")
		if len(crossed) != 0 {
			t.Fatalf("the incoming read matched the FORWARD type and returned %v, want no row: %s", crossed, stale)
		}
	})

	t.Run("a SET deriving its value from type(e) persists the true type", func(t *testing.T) {
		// The value is written to the NODE, deliberately: writing it to the
		// relationship would run into the write-direction hazard of item 4 and
		// could not measure the read.
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'" + relReadSpecKey + "'})<-[e]-(x:Test) SET x.resolved_type = type(e)"}); err != nil {
			t.Fatalf("the SET must execute: %v", err)
		}
		rows := graphQueryRows(t, roadmap,
			"MATCH (x:Test {key:'"+relReadTestKey+"'}) RETURN x.resolved_type")
		if len(rows) != 1 || rows[0][0] != "COVERS" {
			t.Fatalf("the SET persisted %v, want COVERS: persisting the forward type would mean %s", rows, stale)
		}
	})

	// Last, because it removes an edge the cases above depend on.
	t.Run("a predicate-gated DELETE removes the named leg and leaves the other", func(t *testing.T) {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'" + relReadSpecKey + "'})-[e]-(x:Test) WHERE type(e) = 'COVERS' DELETE e"}); err != nil {
			t.Fatalf("the predicate-gated DELETE must execute: %v", err)
		}
		gone := graphQueryRows(t, roadmap,
			"MATCH (x:Test {key:'"+relReadTestKey+"'})-[e:COVERS]->(s) RETURN type(e)")
		if len(gone) != 0 {
			t.Fatalf("the DELETE reported success and the COVERS leg survived: %v: a deletion that removes "+
				"nothing while reporting {\"ok\": true} would mean %s", gone, stale)
		}
		kept := graphQueryRows(t, roadmap,
			"MATCH (s:Spec {key:'"+relReadSpecKey+"'})-[e:VERIFIED_BY]->(x) RETURN type(e)")
		if len(kept) != 1 {
			t.Fatalf("the DELETE removed the wrong leg: VERIFIED_BY rows = %v: %s", kept, stale)
		}
	})
}

// TestGraphRead_ShapesThatResolveCorrectly pins the further forms SPEC/GRAPH.md
// § What Groadmap Does Not Check, item 5, names alongside the fixed-length ones,
// each measured against the same bidirectional pair: a variable-length
// relationship, a projected named path, an anonymous relationship that binds no
// value at all, and a bare DELETE that names the relationship as its target.
func TestGraphRead_ShapesThatResolveCorrectly(t *testing.T) {
	const roadmap = "graph-relread-scope"
	defer setupTestGraphRoadmap(t, roadmap)()
	seedBidirectionalPair(t, roadmap)

	t.Run("an anonymous undirected relationship reaches the right node", func(t *testing.T) {
		rows := graphQueryRows(t, roadmap,
			"MATCH (s:Spec {key:'"+relReadSpecKey+"'})-[:COVERS]-(x) RETURN x.key")
		if len(rows) != 1 || rows[0][0] != relReadTestKey {
			t.Fatalf("anonymous undirected read did not reach the Test node: %v", rows)
		}
	})

	t.Run("a named path over an undirected pattern reports both types", func(t *testing.T) {
		stdout, _ := captureStdStreams(t, func() {
			if err := runGraphExecute([]string{"-r", roadmap, "--query",
				"MATCH p=(s:Spec {key:'" + relReadSpecKey + "'})-[e]-(v:Test) RETURN p"}); err != nil {
				t.Fatalf("named-path read failed: %v", err)
			}
		})
		for _, want := range []string{"VERIFIED_BY", "COVERS"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("named-path read lost the %s edge:\n%s", want, stdout)
			}
		}
	})

	t.Run("a variable-length undirected relationship reports both types", func(t *testing.T) {
		stdout, _ := captureStdStreams(t, func() {
			if err := runGraphExecute([]string{"-r", roadmap, "--query",
				"MATCH (s:Spec {key:'" + relReadSpecKey + "'})-[e*1..1]-(v:Test) RETURN e"}); err != nil {
				t.Fatalf("variable-length read failed: %v", err)
			}
		})
		for _, want := range []string{"VERIFIED_BY", "COVERS"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("variable-length read lost the %s edge:\n%s", want, stdout)
			}
		}
	})

	t.Run("a bare DELETE through an undirected pattern removes the right edge", func(t *testing.T) {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec {key:'" + relReadSpecKey + "'})-[e:COVERS]-(v:Test) DELETE e"}); err != nil {
			t.Fatalf("bare undirected delete failed: %v", err)
		}
		gone := graphQueryRows(t, roadmap,
			"MATCH (v:Test {key:'"+relReadTestKey+"'})-[e:COVERS]->(s) RETURN type(e)")
		if len(gone) != 0 {
			t.Errorf("the bare DELETE reported success but the COVERS edge survived: %v", gone)
		}
		kept := graphQueryRows(t, roadmap,
			"MATCH (s:Spec {key:'"+relReadSpecKey+"'})-[e:VERIFIED_BY]->(v) RETURN type(e)")
		if len(kept) != 1 {
			t.Errorf("the bare DELETE removed the wrong edge: VERIFIED_BY rows = %v", kept)
		}
	})
}
