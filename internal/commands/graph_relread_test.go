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
// WHAT IS ASSERTED HERE, AND WHAT DELIBERATELY IS NOT. Item 5 states that an
// undirected or incoming fixed-length read "can be reported wrong", and
// acceptance criterion 38's fourth bullet fixes a specific wrong outcome for it.
// **That outcome does not reproduce at the pinned engine.** Measured against
// GoGraph v0.12.0 on this exact fixture, every one of those shapes answers
// CORRECTLY: the undirected projection reports both types, an incoming read
// reports the stored orientation, a `WHERE` over the type selects the right
// edge, a predicate-gated `DELETE` removes it, and a `SET` deriving its value
// from the relationship persists the true type. Asserting either the outcome the
// specification gives or the opposite one would be this file taking a decision
// that belongs to the specification, so it asserts neither, and the measurement
// is recorded on rmp task #362 for the owner to settle.
//
// What is asserted is the part that is true either way and is worth a
// regression: the outgoing forms, and the two rewrites the specification
// publishes as the correct way to read both directions, resolve correctly
// whatever the data; and the shapes item 5 EXEMPTS -- a variable-length
// relationship, a projected named path, an anonymous relationship, and a bare
// DELETE -- do too.

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
// edge is present so a case can exercise the same traversal shape on a pair that
// item 5 does not concern.
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

// TestGraphRead_ShapesThatResolveCorrectly pins the forms SPEC/GRAPH.md § What
// Groadmap Does Not Check, item 5, exempts, each measured against the same
// bidirectional pair. They are exempt because they are not resolved by the probe
// at all: a variable-length relationship and a projected named path are TOLD
// which way each hop was walked, and an anonymous relationship binds no value to
// resolve.
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
