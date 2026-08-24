package cypherguard

// Regression tests for the relationship-read direction check (rmp task #288,
// SPEC/GRAPH.md § Relationship Read Direction).
//
// The defect being guarded is a SILENT one: on a node pair carrying edges in
// BOTH directions, GoGraph hydrates the reverse leg of an incoming or undirected
// traversal from the FORWARD pair, so the read reports another relationship's
// type and the reversed orientation, drops rows whose predicate reads that type,
// and persists the wrong value when a node write derives from it.
//
// These tests pin which query shapes the check refuses and — just as important —
// which it must keep admitting. Every admitted shape below was measured against
// a real bidirectional pair and reports the true stored type and orientation, so
// an over-broad refusal here would cost reach for nothing.

import "testing"

func TestMisreadRelationshipReferences(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantVar  string
		wantDir  Direction
		wantNone bool
	}{
		// ── Refused: the value read would be misresolved ──────────────────────
		{
			name:    "undirected pattern, type projected",
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) RETURN type(e), x.key",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name:    "incoming pattern, type projected",
			query:   "MATCH (b:Spec {key:'auth'})<-[e]-(x) RETURN type(e)",
			wantVar: "e",
			wantDir: DirectionIncoming,
		},
		{
			name: "incoming pattern narrowed by a type filter",
			// The filter selects the right edge and the engine then reports it
			// under the other one's type, so the filter does not make it safe.
			query:   "MATCH (b:Spec {key:'auth'})<-[e:VERIFIED_BY]-(x) RETURN type(e)",
			wantVar: "e",
			wantDir: DirectionIncoming,
		},
		{
			name:    "undirected pattern, whole relationship projected",
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) RETURN e",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name:    "undirected pattern, endpoints projected",
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) RETURN startNode(e).key, endNode(e).key",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name:    "undirected pattern, property projected",
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) RETURN e.last_commit",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name: "undirected pattern, star projection",
			// RETURN * projects the relationship without naming it, so a check
			// that looked only for an explicit reference would be bypassed by
			// deleting four characters.
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) RETURN *",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name: "undirected pattern, read only by a WHERE predicate",
			// The sharpest case: the engine evaluates the predicate against the
			// corrupted type and drops the row, so nothing reaches the caller to
			// look wrong. A projection-only rule would miss this entirely.
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) WHERE type(e) = 'VERIFIED_BY' RETURN x.key",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name:    "undirected pattern, read only by ORDER BY",
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) RETURN x.key ORDER BY type(e)",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name:    "undirected pattern, carried through WITH",
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) WITH type(e) AS t RETURN t",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name: "incoming pattern on the right-hand side of a node SET",
			// This is the case that escapes the read path: the misresolved type
			// is PERSISTED. The write-direction rule does not cover it, because
			// the write TARGET is a node.
			query:   "MATCH (b:Spec {key:'auth'})<-[e]-(x) SET x.last_type = type(e)",
			wantVar: "e",
			wantDir: DirectionIncoming,
		},
		{
			name:    "undirected pattern read inside a FOREACH body",
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) FOREACH (n IN [1] | SET x.t = type(e))",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name: "a name bound both ways is reported on the failing binding",
			query: "MATCH (a:Spec {key:'auth'})-[e]->(b) " +
				"MATCH (c:Test {key:'t'})-[e]-(d) RETURN type(e)",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name: "undirected DELETE gated by a predicate over the type",
			// The exemption is of the DELETE CLAUSE, not of the delete command.
			// Here the corrupted type decides WHICH edges are deleted: the engine
			// drops the row and the destructive statement reports success having
			// removed nothing.
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) WHERE type(e) = 'VERIFIED_BY' DELETE e",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name:    "incoming DELETE gated by a predicate over the type",
			query:   "MATCH (b:Spec {key:'auth'})<-[e]-(x) WHERE type(e) = 'VERIFIED_BY' DELETE e",
			wantVar: "e",
			wantDir: DirectionIncoming,
		},
		{
			name: "refused in one UNION branch alone",
			query: "MATCH (a:Spec {key:'auth'})-[e]->(x) RETURN type(e) AS t " +
				"UNION ALL MATCH (b:Test {key:'t'})<-[e]-(y) RETURN type(e) AS t",
			wantVar: "e",
			wantDir: DirectionIncoming,
		},

		// ── Admitted: resolved correctly whatever the data ────────────────────
		{
			name:     "outgoing pattern",
			query:    "MATCH (b:Spec {key:'auth'})-[e]->(x) RETURN type(e), x.key",
			wantNone: true,
		},
		{
			name: "outgoing pattern anchored on the target",
			// The rewrite the refusal offers: it reaches the edges arriving AT a
			// node without reversing the arrow.
			query:    "MATCH (x)-[e]->(b:Spec {key:'auth'}) RETURN type(e), x.key",
			wantNone: true,
		},
		{
			name: "union of the two outgoing legs",
			// The rewrite for a full undirected read. Both legs are outgoing, so
			// neither is refused.
			query: "MATCH (a:Spec {key:'auth'})-[e]->(x) RETURN type(e) AS t, x.key AS k " +
				"UNION ALL MATCH (x)-[e]->(a:Spec {key:'auth'}) RETURN type(e) AS t, x.key AS k",
			wantNone: true,
		},
		{
			name: "anonymous undirected relationship",
			// No variable is bound, so no relationship value is ever built.
			query:    "MATCH (b:Spec {key:'auth'})-[:VERIFIED_BY]-(x) RETURN x.key",
			wantNone: true,
		},
		{
			name: "undirected relationship bound but never read",
			// The traversal is used to reach x; the relationship's own value is
			// never inspected, so nothing can be misreported.
			query:    "MATCH (b:Spec {key:'auth'})-[e]-(x) RETURN x.key",
			wantNone: true,
		},
		{
			name:     "undirected relationship bound but only a NODE is written",
			query:    "MATCH (b:Spec {key:'auth'})-[e]-(x) SET x.reviewed = true",
			wantNone: true,
		},
		{
			name: "named path returned over an undirected pattern",
			// The path renderer resolves each hop through the resolver that is
			// TOLD the traversal direction, and reports the true type and
			// orientation. Returning the PATH is safe; only reading the
			// relationship variable itself is not.
			query:    "MATCH p=(b:Spec {key:'auth'})-[e]-(x) RETURN p",
			wantNone: true,
		},
		{
			name: "variable-length undirected relationship",
			// Variable-length hops go through that same correct resolver.
			query:    "MATCH (b:Spec {key:'auth'})-[e*1..2]-(x) RETURN e",
			wantNone: true,
		},
		{
			name: "bare DELETE of an undirected relationship",
			// A DELETE item names the relationship as a TARGET, not as a value:
			// the engine resolves that edge itself rather than through the
			// endpoint columns and removes the right one. This is the exemption,
			// and it is drawn by CLAUSE — the predicate-gated forms above are
			// refused.
			query:    "MATCH (b:Spec {key:'auth'})-[e]-(x) DELETE e",
			wantNone: true,
		},
		{
			name:     "bare DELETE of an incoming relationship",
			query:    "MATCH (b:Spec {key:'auth'})<-[e]-(x) DELETE e",
			wantNone: true,
		},
		{
			name: "DELETE gated by a predicate over an OUTGOING relationship",
			// The predicate reads a relationship the engine resolves correctly,
			// so there is nothing to refuse. This is the control that keeps the
			// two cases above from passing merely because DELETE plus WHERE is
			// refused wholesale.
			query:    "MATCH (b:Spec {key:'auth'})-[e]->(x) WHERE type(e) = 'VERIFIED_BY' DELETE e",
			wantNone: true,
		},
		{
			name: "DETACH DELETE of a node reached through an undirected traversal",
			// The relationship is bound to reach the node; its value is never
			// read.
			query:    "MATCH (b:Spec {key:'auth'})-[e]-(x) DETACH DELETE x",
			wantNone: true,
		},
		{
			name: "undirected relationship as a SET target only",
			// Owned by the write-direction rule; reporting it here as well would
			// refuse one query under two contracts.
			query:    "MATCH (b:Spec {key:'auth'})-[e]-(x) SET e.last_commit = 'abc123'",
			wantNone: true,
		},
		{
			name:     "undirected relationship as a REMOVE target only",
			query:    "MATCH (b:Spec {key:'auth'})-[e]-(x) REMOVE e.last_commit",
			wantNone: true,
		},
		{
			name: "WITH * carries the binding forward without reading it",
			// A star that is not the terminal RETURN delivers nothing to the
			// caller; a later read would be recorded on its own merits.
			query:    "MATCH (b:Spec {key:'auth'})-[e]-(x) WITH * RETURN x.key",
			wantNone: true,
		},
		{
			name: "an arrow inside a string literal is not a pattern",
			// Detection runs on the parsed query, so this cannot trip it.
			query:    "MATCH (b:Spec)-[e]->(x) WHERE b.note = 'see (a)<-[e]-(b)' RETURN type(e)",
			wantNone: true,
		},
		{
			name:     "a query the parser rejects yields nothing",
			query:    "MATCH (b:Spec {key:'auth'}<-[e]-(x RETURN type(e)",
			wantNone: true,
		},
		{
			name:     "no relationship at all",
			query:    "MATCH (n:Spec) RETURN n.key",
			wantNone: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MisreadRelationshipReferences(tc.query)
			if tc.wantNone {
				if len(got) != 0 {
					t.Fatalf("query must be admitted, got %+v\nquery=%s", got, tc.query)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("expected exactly one misread reference, got %+v\nquery=%s", got, tc.query)
			}
			if got[0].Variable != tc.wantVar {
				t.Errorf("variable = %q, want %q", got[0].Variable, tc.wantVar)
			}
			if got[0].Direction != tc.wantDir {
				t.Errorf("direction = %q, want %q", got[0].Direction, tc.wantDir)
			}
		})
	}
}

// TestMisreadRelationshipReferences_ReportsInFirstReadOrder pins the order the
// references come back in, because the command layer names the FIRST one in its
// refusal message and an unstable order would make that message
// nondeterministic.
func TestMisreadRelationshipReferences_ReportsInFirstReadOrder(t *testing.T) {
	got := MisreadRelationshipReferences(
		"MATCH (a:Spec {key:'auth'})-[e]-(b)<-[f]-(c) RETURN type(f), type(e)")
	if len(got) != 2 {
		t.Fatalf("expected both relationships, got %+v", got)
	}
	if got[0].Variable != "f" || got[1].Variable != "e" {
		t.Fatalf("references must come back in first-read order (f then e), got %+v", got)
	}
	if got[0].Direction != DirectionIncoming {
		t.Errorf("f direction = %q, want %q", got[0].Direction, DirectionIncoming)
	}
	if got[1].Direction != DirectionUndirected {
		t.Errorf("e direction = %q, want %q", got[1].Direction, DirectionUndirected)
	}
}
