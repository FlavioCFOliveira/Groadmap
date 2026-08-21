package cypherguard

// Regression tests for the relationship-write direction check (rmp task #193,
// SPEC/GRAPH.md § Relationship Write Direction).
//
// The defect being guarded is a SILENT one: GoGraph writes a relationship
// property only through an outgoing pattern, while reporting success for the
// incoming and undirected forms. These tests pin which query shapes the check
// refuses and — just as important — which it must keep admitting, because an
// over-broad refusal would make edges unreachable and a narrow one would let the
// silent no-op back through.

import "testing"

func TestUnwritableRelationshipTargets(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantVar  string
		wantDir  Direction
		wantNone bool
	}{
		// ── Refused: the write would be silently dropped ──────────────────────
		{
			name:    "undirected pattern anchored on the target",
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) SET e.last_commit = 'abc123'",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name:    "undirected pattern anchored on the source",
			query:   "MATCH (a:Spec {key:'auth'})-[e]-(x) SET e.last_commit = 'abc123'",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name:    "incoming pattern anchored on the target",
			query:   "MATCH (b:Spec {key:'auth'})<-[e:SPECIFIES]-(x) SET e.last_commit = 'abc123'",
			wantVar: "e",
			wantDir: DirectionIncoming,
		},
		{
			name:    "incoming pattern anchored on the source",
			query:   "MATCH (x)<-[e:SPECIFIES]-(a:Spec {key:'auth'}) SET e.last_commit = 'abc123'",
			wantVar: "e",
			wantDir: DirectionIncoming,
		},
		{
			name:    "REMOVE through an undirected pattern",
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) REMOVE e.last_commit",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name:    "whole-entity replacement through an undirected pattern",
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) SET e = {last_commit: 'abc123'}",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name:    "merge assignment through an incoming pattern",
			query:   "MATCH (b:Spec {key:'auth'})<-[e]-(x) SET e += {last_commit: 'abc123'}",
			wantVar: "e",
			wantDir: DirectionIncoming,
		},
		{
			name:    "unwritable leg of a multi-hop path",
			query:   "MATCH (a:Spec {key:'auth'})-[f:SPECIFIES]->(b)<-[e:VERIFIED_BY]-(c) SET e.last_commit = 'abc123'",
			wantVar: "e",
			wantDir: DirectionIncoming,
		},
		{
			name:    "FOREACH body writes through an undirected pattern",
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) FOREACH (i IN [1] | SET e.last_commit = 'abc123')",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name: "WITH does not exempt the undirected pattern",
			// WITH e happens to repair the write in the pinned engine, but that
			// rests on projection materialisation the engine is free to elide.
			// Exempting it would fail OPEN; the check must still refuse.
			query:   "MATCH (b:Spec {key:'auth'})-[e]-(x) WITH e SET e.last_commit = 'abc123'",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name:    "a second, outgoing binding does not rescue an undirected one",
			query:   "MATCH (a:Spec {key:'auth'})-[e]-(b) MATCH (c)-[e]->(d) SET e.last_commit = 'abc123'",
			wantVar: "e",
			wantDir: DirectionUndirected,
		},
		{
			name:    "UNION branch carrying the unwritable write",
			query:   "MATCH (a:Spec {key:'auth'})-[e]->(b) SET e.n = 1 RETURN e.n UNION MATCH (c:Task {key:'t'})<-[e]-(d) SET e.n = 2 RETURN e.n",
			wantVar: "e",
			wantDir: DirectionIncoming,
		},

		// ── Admitted: the write lands, or there is no relationship write ──────
		{
			name:     "outgoing pattern anchored on the source",
			query:    "MATCH (a:Spec {key:'auth'})-[e:SPECIFIES]->(x) SET e.last_commit = 'abc123'",
			wantNone: true,
		},
		{
			name: "outgoing pattern anchored on the target",
			// The documented repair for an incoming edge: keep the arrow
			// outgoing and anchor on the node the edge arrives at.
			query:    "MATCH (x)-[e:SPECIFIES]->(b:Spec {key:'auth'}) SET e.last_commit = 'abc123'",
			wantNone: true,
		},
		{
			name:     "outgoing pattern selected by WHERE on the target",
			query:    "MATCH (x)-[e:SPECIFIES]->(b) WHERE b.key = 'auth' SET e.last_commit = 'abc123'",
			wantNone: true,
		},
		{
			name: "undirected pattern writing a NODE property",
			// The relationship is only traversed; the write targets a node,
			// which is resolved by identifier and is unaffected by the defect.
			query:    "MATCH (b:Spec {key:'auth'})-[e]-(x) SET x.reviewed = true",
			wantNone: true,
		},
		{
			name:     "undirected relationship only on the right-hand side",
			query:    "MATCH (b:Spec {key:'auth'})-[e]-(x) SET b.last_type = type(e)",
			wantNone: true,
		},
		{
			name:     "anonymous undirected relationship, node write",
			query:    "MATCH (b:Spec {key:'auth'})-[]-(x) SET x.reviewed = true",
			wantNone: true,
		},
		{
			name:     "node-only pattern",
			query:    "MATCH (n:Spec {key:'auth'}) SET n.status = 'done'",
			wantNone: true,
		},
		{
			name: "a relationship arrow inside a string literal is not a pattern",
			// The parser never sees these characters as syntax, so no binding is
			// recorded and the node write is admitted.
			query:    "MATCH (n:Memory {key:'idiom'}) SET n.body = 'use (a)<-[e]-(b) SET e.x carefully'",
			wantNone: true,
		},
		{
			name: "unparsable query yields no verdict",
			// The engine rejects it too; masking that syntax error with a
			// direction error would misreport the failure.
			query:    "MATCH (b)-[e]-(x SET e.k = 1",
			wantNone: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := UnwritableRelationshipTargets(tc.query)

			if tc.wantNone {
				if len(got) != 0 {
					t.Fatalf("expected the query to be admitted, got %+v\nquery: %s", got, tc.query)
				}
				return
			}

			if len(got) != 1 {
				t.Fatalf("expected exactly one unwritable target, got %d (%+v)\nquery: %s",
					len(got), got, tc.query)
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

// TestUnwritableRelationshipTargets_DeduplicatesPerVariable pins that a variable
// written by several SET items is reported once, so the refusal message names
// each offending relationship a single time.
func TestUnwritableRelationshipTargets_DeduplicatesPerVariable(t *testing.T) {
	got := UnwritableRelationshipTargets(
		"MATCH (b:Spec {key:'auth'})-[e]-(x) SET e.last_commit = 'abc123', e.stamped_at = '2026-08-21'")
	if len(got) != 1 {
		t.Fatalf("expected one target for two SET items on the same variable, got %+v", got)
	}
	if got[0].Variable != "e" {
		t.Errorf("variable = %q, want %q", got[0].Variable, "e")
	}
}

// TestUnwritableRelationshipTargets_ReportsEveryOffendingVariable pins that two
// distinct unwritable relationships are both reported, in the order the SET
// clause names them, so no offending write is hidden behind the first.
func TestUnwritableRelationshipTargets_ReportsEveryOffendingVariable(t *testing.T) {
	got := UnwritableRelationshipTargets(
		"MATCH (a:Spec {key:'auth'})-[f]-(b), (c:Task {key:'t'})<-[g]-(d) SET g.n = 1, f.n = 2")
	if len(got) != 2 {
		t.Fatalf("expected two unwritable targets, got %+v", got)
	}
	if got[0].Variable != "g" || got[0].Direction != DirectionIncoming {
		t.Errorf("first target = %+v, want {g incoming}", got[0])
	}
	if got[1].Variable != "f" || got[1].Direction != DirectionUndirected {
		t.Errorf("second target = %+v, want {f undirected}", got[1])
	}
}

// TestClassifyUnchangedByRelWriteCheck pins that the clause-class guard rail —
// which the read-only web endpoint shares — is untouched by this write-path
// rule. A query the direction check refuses is still classified exactly as
// before: a mutating write, not a read.
func TestClassifyUnchangedByRelWriteCheck(t *testing.T) {
	const q = "MATCH (b:Spec {key:'auth'})-[e]-(x) SET e.last_commit = 'abc123'"

	c := Classify(q)
	if !c.Write || !c.Mutate {
		t.Errorf("Classify(%q) = %+v, want a mutating write", q, c)
	}
	if c.Create || c.Delete || c.DDL || c.Introspect {
		t.Errorf("Classify(%q) = %+v, want no other class set", q, c)
	}
	if IsReadOnly(q) {
		t.Errorf("IsReadOnly(%q) = true, want false", q)
	}
}
