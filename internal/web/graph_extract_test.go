package web

import (
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
)

// TestApplyGraphLimit_InjectsOnlyWhenAbsent covers the node-limit injection
// precedence (SPEC/WEB.md § Graph Data Endpoint, node-limit injection;
// Acceptance Criterion 48): a LIMIT is appended only when the query has no
// top-level LIMIT, a user LIMIT is respected, and a LIMIT keyword inside a
// string literal / comment / backtick identifier does NOT count as existing.
func TestApplyGraphLimit_InjectsOnlyWhenAbsent(t *testing.T) {
	cases := []struct {
		name  string
		query string
		limit int
		want  string
	}{
		{
			name:  "no limit: appended",
			query: "MATCH (n) RETURN n",
			limit: 100,
			want:  "MATCH (n) RETURN n LIMIT 100",
		},
		{
			name:  "user limit respected: not appended",
			query: "MATCH (n) RETURN n LIMIT 5",
			limit: 100,
			want:  "MATCH (n) RETURN n LIMIT 5",
		},
		{
			name:  "limit only inside string literal: still injected",
			query: `MATCH (n) WHERE n.note = "limit 5" RETURN n`,
			limit: 250,
			want:  `MATCH (n) WHERE n.note = "limit 5" RETURN n LIMIT 250`,
		},
		{
			name:  "limit only inside line comment: still injected",
			query: "MATCH (n) RETURN n // limit 5",
			limit: 50,
			want:  "MATCH (n) RETURN n // limit 5 LIMIT 50",
		},
		{
			name:  "lowercase user limit respected",
			query: "match (n) return n limit 10",
			limit: 500,
			want:  "match (n) return n limit 10",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyGraphLimit(tc.query, tc.limit); got != tc.want {
				t.Errorf("applyGraphLimit(%q, %d) = %q, want %q", tc.query, tc.limit, got, tc.want)
			}
		})
	}
}

// TestResolveGraphLimit covers limit validation: absent -> default, allowed ->
// itself, anything else -> classified invalid-limit error (no clamping).
func TestResolveGraphLimit(t *testing.T) {
	if n, err := resolveGraphLimit(""); err != nil || n != defaultGraphLimit {
		t.Errorf("empty limit = (%d, %v), want (%d, nil)", n, err, defaultGraphLimit)
	}
	for _, ok := range []int{50, 100, 250, 500, 1000, 3000} {
		if n, err := resolveGraphLimit(itoa(ok)); err != nil || n != ok {
			t.Errorf("limit %d = (%d, %v), want (%d, nil)", ok, n, err, ok)
		}
	}
	for _, bad := range []string{"7", "0", "5000", "abc", "-1"} {
		n, err := resolveGraphLimit(bad)
		if err == nil {
			t.Errorf("limit %q accepted (=%d), want invalid-limit error", bad, n)
			continue
		}
		qe, ok := asGraphQueryError(err)
		if !ok || qe.Kind != graphErrInvalidLimit {
			t.Errorf("limit %q: error = %v, want invalid-limit graphQueryError", bad, err)
		}
	}
}

// TestResolveGraphQuery covers the default-query fallback.
func TestResolveGraphQuery(t *testing.T) {
	if got := resolveGraphQuery(""); got != defaultGraphQuery {
		t.Errorf("empty q = %q, want default %q", got, defaultGraphQuery)
	}
	if got := resolveGraphQuery("   "); got != defaultGraphQuery {
		t.Errorf("whitespace q = %q, want default", got)
	}
	if got := resolveGraphQuery("  MATCH (n) RETURN n  "); got != "MATCH (n) RETURN n" {
		t.Errorf("q not trimmed: %q", got)
	}
}

// TestGraphCollector_DedupAndOrphanDrop drives the result walker directly with
// constructed expr values to assert: nodes/edges are deduplicated by id, an edge
// is kept only when BOTH endpoints were collected, and an edge with a missing
// endpoint is dropped with no synthetic node invented (SPEC/WEB.md § Graph Data
// Endpoint, result-to-graph extraction; Acceptance Criterion 49).
func TestGraphCollector_DedupAndOrphanDrop(t *testing.T) {
	n1 := expr.NodeValue{ID: 1, Labels: []string{"Spec"}, Properties: expr.MapValue{"key": expr.StringValue("auth")}}
	n2 := expr.NodeValue{ID: 2, Labels: []string{"Code"}, Properties: expr.MapValue{"path": expr.StringValue("jwt.go")}}
	// Edge between two collected nodes: kept.
	e12 := expr.RelationshipValue{ID: 10, StartID: 1, EndID: 2, Type: "IMPLEMENTED_BY"}
	// Edge to a node id (99) that is never collected: orphan, must be dropped.
	eOrphan := expr.RelationshipValue{ID: 11, StartID: 1, EndID: 99, Type: "DANGLING"}

	c := newGraphCollector()
	// Walk n1 twice (dedup), n2 once, e12 twice (dedup), the orphan once, and a
	// nested list/map/path to exercise recursion.
	c.walk(n1)
	c.walk(n1) // duplicate node
	c.walk(n2)
	c.walk(e12)
	c.walk(e12) // duplicate edge
	c.walk(eOrphan)
	// nested inside a list and a map: a node and an edge that are already seen
	// must not be re-added.
	c.walk(expr.ListValue{n1, e12})
	c.walk(expr.MapValue{"x": n2})
	// a path carrying n1, n2 and e12: still deduplicated.
	c.walk(expr.PathValue{Nodes: []expr.NodeValue{n1, n2}, Relationships: []expr.RelationshipValue{e12}})

	view := c.view()

	if len(view.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2 (deduplicated)", len(view.Nodes))
	}
	if len(view.Edges) != 1 {
		t.Fatalf("edges = %d, want 1 (orphan dropped, duplicate collapsed)", len(view.Edges))
	}
	if view.Edges[0]["id"] != uint64(10) {
		t.Errorf("kept edge id = %v, want 10 (the in-set edge)", view.Edges[0]["id"])
	}
	// No node with id 99 was invented for the orphan edge.
	for _, n := range view.Nodes {
		if n["id"] == uint64(99) {
			t.Errorf("a synthetic endpoint node (id 99) was invented for the orphan edge")
		}
	}
}

// TestGraphCollector_NestedExtraction asserts a node/edge nested only inside a
// list, a map, or a path (never returned in its own column) is still collected,
// proving the walk is exhaustive and recursive (Acceptance Criterion 49).
func TestGraphCollector_NestedExtraction(t *testing.T) {
	na := expr.NodeValue{ID: 1, Labels: []string{"A"}}
	nb := expr.NodeValue{ID: 2, Labels: []string{"B"}}
	eab := expr.RelationshipValue{ID: 5, StartID: 1, EndID: 2, Type: "REL"}

	c := newGraphCollector()
	// Everything is nested: a list holding a map holding a path.
	c.walk(expr.ListValue{
		expr.MapValue{
			"p": expr.PathValue{
				Nodes:         []expr.NodeValue{na, nb},
				Relationships: []expr.RelationshipValue{eab},
			},
		},
	})

	view := c.view()
	if len(view.Nodes) != 2 || len(view.Edges) != 1 {
		t.Fatalf("nested extraction: nodes=%d edges=%d, want 2/1", len(view.Nodes), len(view.Edges))
	}
}
