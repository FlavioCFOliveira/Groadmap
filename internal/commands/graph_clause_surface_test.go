// Regression tests pinning the Cypher clause surface the guard rail is
// classified against.
//
// The guard rail decides a subcommand's accepted class from the clauses a query
// contains, so the set of clauses the pinned engine accepts is part of the
// integration surface even though no Go symbol expresses it. When the engine
// learns a new clause family, queries the previous engine rejected as a syntax
// error start executing and each subcommand's accepted class widens with no
// Groadmap code changing — invisible both to a diff of removed or re-signed
// exported symbols (nothing was removed) and to a re-run of the existing
// acceptance criteria (none of them names a clause that did not exist).
//
// Two families arrived exactly that way and are pinned here per subcommand:
//
//   - Schema introspection (SHOW INDEXES / SHOW INDEX / SHOW CONSTRAINTS /
//     SHOW CONSTRAINT, with an optional YIELD / WHERE / RETURN tail). It lists
//     the registered schema without altering it, so it is read-only: accepted by
//     query and search, rejected by create, update and delete, each of which
//     accepts only its own data-writing clause class. Note that the engine's own
//     cypher/ir.IsDDL folds SHOW in with the schema-MUTATING forms; Groadmap
//     deliberately does not (SPEC/GRAPH.md § Schema Introspection).
//
//   - FOREACH, an updating clause with no discriminator of its own. It is
//     classified by the writing clauses its body contains, which is sound only
//     because a FOREACH body may hold nothing but those clauses and a nested
//     FOREACH. That containment is an emergent property of the grammar, so it is
//     asserted here rather than assumed (SPEC/GRAPH.md § Per-Subcommand
//     Validation Rules note 7).
//
// SPEC/GRAPH.md § Dependency Maturity Risk mitigation 5 requires this file to
// exist: an upgrade that widens the surface must fail a test instead of passing
// unnoticed.
package commands

import (
	"errors"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestValidateGuardRailClauseSurface asserts the accepted and rejected class of
// every subcommand for each clause family the engine gained after the guard rail
// was first written.
func TestValidateGuardRailClauseSurface(t *testing.T) {
	tests := []struct {
		name       string
		subcmd     string
		allowed    string
		query      string
		wantReject bool
		// wantMsg, when set, is the exact ErrValidation message expected for a
		// rejection, so each subcommand's contract string is pinned.
		wantMsg string
	}{
		// --- Schema introspection is accepted by the read subcommands. ---
		{
			name:    "query accepts SHOW INDEXES",
			subcmd:  "query",
			allowed: "read-only",
			query:   `SHOW INDEXES`,
		},
		{
			name:    "query accepts SHOW CONSTRAINTS",
			subcmd:  "query",
			allowed: "read-only",
			query:   `SHOW CONSTRAINTS`,
		},
		{
			// The singular aliases the engine accepts. Their own regression
			// value is high: the first draft of the matcher spelled the plural
			// as INDEXES?, which parses as INDEXE plus an optional S and does
			// not match SHOW INDEX at all.
			name:    "query accepts singular SHOW INDEX",
			subcmd:  "query",
			allowed: "read-only",
			query:   `SHOW INDEX`,
		},
		{
			name:    "query accepts singular SHOW CONSTRAINT",
			subcmd:  "query",
			allowed: "read-only",
			query:   `SHOW CONSTRAINT`,
		},
		{
			name:    "query accepts a lowercase SHOW",
			subcmd:  "query",
			allowed: "read-only",
			query:   `show indexes`,
		},
		{
			name:    "query accepts SHOW with a YIELD WHERE RETURN tail",
			subcmd:  "query",
			allowed: "read-only",
			query:   `SHOW INDEXES YIELD name, type WHERE type = 'RANGE' RETURN name`,
		},
		{
			name:    "search accepts SHOW INDEXES",
			subcmd:  "search",
			allowed: "read-only",
			query:   `SHOW INDEXES`,
		},
		{
			name:    "search accepts SHOW CONSTRAINTS after a leading comment",
			subcmd:  "search",
			allowed: "read-only",
			query:   "// schema check\nSHOW CONSTRAINTS",
		},

		// --- Schema introspection is rejected by the write subcommands. ---
		{
			name:       "create rejects SHOW INDEXES",
			subcmd:     "create",
			allowed:    "CREATE/MERGE",
			query:      `SHOW INDEXES`,
			wantReject: true,
			wantMsg:    "graph create accepts only CREATE/MERGE queries",
		},
		{
			name:       "update rejects SHOW INDEXES",
			subcmd:     "update",
			allowed:    "SET/REMOVE",
			query:      `SHOW INDEXES`,
			wantReject: true,
			wantMsg:    "graph update accepts only SET/REMOVE queries",
		},
		{
			name:       "delete rejects SHOW CONSTRAINTS",
			subcmd:     "delete",
			allowed:    "DELETE/DETACH DELETE",
			query:      `SHOW CONSTRAINTS`,
			wantReject: true,
			wantMsg:    "graph delete accepts only DELETE/DETACH DELETE queries",
		},

		// --- SHOW must be a statement, not any occurrence of the word. ---
		{
			// A property named show is an ordinary creating write. If the
			// matcher were not anchored at the start of the statement this
			// would be misread as introspection and, being classified
			// read-only, would be REJECTED by create — a write silently lost.
			name:    "create accepts a node carrying a property named show",
			subcmd:  "create",
			allowed: "CREATE/MERGE",
			query:   `CREATE (n:Panel {show: 'indexes'})`,
		},
		{
			name:    "query accepts a match on a label named Show",
			subcmd:  "query",
			allowed: "read-only",
			query:   `MATCH (n:Show) RETURN n.title`,
		},
		{
			name:    "query accepts a SHOW keyword that appears only inside a literal",
			subcmd:  "query",
			allowed: "read-only",
			query:   `MATCH (n:Doc) WHERE n.body = 'run SHOW INDEXES first' RETURN n.key`,
		},

		// --- FOREACH is classified by the writing clauses of its body. ---
		{
			name:       "query rejects FOREACH with a SET body",
			subcmd:     "query",
			allowed:    "read-only",
			query:      `MATCH (s:Spec) FOREACH (x IN [1] | SET s.reviewed = true)`,
			wantReject: true,
			wantMsg:    "graph query accepts only read-only queries",
		},
		{
			name:       "search rejects FOREACH with a CREATE body",
			subcmd:     "search",
			allowed:    "read-only",
			query:      `FOREACH (name IN ['auth'] | CREATE (:Spec {key: name}))`,
			wantReject: true,
			wantMsg:    "graph search accepts only read-only queries",
		},
		{
			name:       "query rejects a nested FOREACH",
			subcmd:     "query",
			allowed:    "read-only",
			query:      `MATCH (s:Spec) FOREACH (a IN [[1]] | FOREACH (b IN a | SET s.depth = b))`,
			wantReject: true,
			wantMsg:    "graph query accepts only read-only queries",
		},
		{
			name:    "update accepts FOREACH with a SET body",
			subcmd:  "update",
			allowed: "SET/REMOVE",
			query:   `MATCH (s:Spec) FOREACH (x IN [1] | SET s.reviewed = true)`,
		},
		{
			name:    "update accepts FOREACH with a REMOVE body",
			subcmd:  "update",
			allowed: "SET/REMOVE",
			query:   `MATCH (s:Spec) FOREACH (x IN [1] | REMOVE s.draft)`,
		},
		{
			name:       "create rejects FOREACH with a SET body",
			subcmd:     "create",
			allowed:    "CREATE/MERGE",
			query:      `MATCH (s:Spec) FOREACH (x IN [1] | SET s.reviewed = true)`,
			wantReject: true,
			wantMsg:    "graph create accepts only CREATE/MERGE queries",
		},
		{
			name:    "create accepts FOREACH with a CREATE body",
			subcmd:  "create",
			allowed: "CREATE/MERGE",
			query:   `FOREACH (name IN ['auth', 'crypto'] | CREATE (:Spec {key: name}))`,
		},
		{
			name:    "create accepts FOREACH with a MERGE body",
			subcmd:  "create",
			allowed: "CREATE/MERGE",
			query:   `FOREACH (name IN ['auth'] | MERGE (:Spec {key: name}))`,
		},
		{
			name:       "update rejects FOREACH with a CREATE body",
			subcmd:     "update",
			allowed:    "SET/REMOVE",
			query:      `FOREACH (name IN ['auth'] | CREATE (:Spec {key: name}))`,
			wantReject: true,
			wantMsg:    "graph update accepts only SET/REMOVE queries",
		},
		{
			name:    "delete accepts FOREACH with a DETACH DELETE body",
			subcmd:  "delete",
			allowed: "DELETE/DETACH DELETE",
			query:   `MATCH p = (a:Spec)-[:DEPENDS_ON*]->(b:Spec) FOREACH (n IN nodes(p) | DETACH DELETE n)`,
		},
		{
			name:       "create rejects FOREACH with a DELETE body",
			subcmd:     "create",
			allowed:    "CREATE/MERGE",
			query:      `MATCH p = (a:Spec)-[:DEPENDS_ON*]->(b:Spec) FOREACH (r IN relationships(p) | DELETE r)`,
			wantReject: true,
			wantMsg:    "graph create accepts only CREATE/MERGE queries",
		},
		{
			// The keyword FOREACH carries no class on its own, so a query that
			// only mentions it inside a literal stays an ordinary read.
			name:    "query accepts a FOREACH keyword that appears only inside a literal",
			subcmd:  "query",
			allowed: "read-only",
			query:   `MATCH (m:Memory) WHERE m.body = 'use FOREACH to fan out' RETURN m.key`,
		},

		// --- The schema-MUTATING DDL forms remain rejected everywhere. ---
		// Adding the introspection class must not loosen the DDL rejection, and
		// the engine's own IsDDL still misses non-canonical spacing, so these
		// cases assert the guard rail catches what the engine's predicate does
		// not.
		{
			name:       "query still rejects CREATE INDEX",
			subcmd:     "query",
			allowed:    "read-only",
			query:      `CREATE INDEX spec_key_idx FOR (n:Spec) ON (n.key)`,
			wantReject: true,
			wantMsg:    "graph query accepts only read-only queries",
		},
		{
			name:       "query still rejects a multi-space CREATE INDEX",
			subcmd:     "query",
			allowed:    "read-only",
			query:      `CREATE   INDEX spec_key_idx FOR (n:Spec) ON (n.key)`,
			wantReject: true,
			wantMsg:    "graph query accepts only read-only queries",
		},
		{
			name:       "search still rejects a lowercase DROP CONSTRAINT",
			subcmd:     "search",
			allowed:    "read-only",
			query:      `drop constraint unique_spec_key`,
			wantReject: true,
			wantMsg:    "graph search accepts only read-only queries",
		},
		{
			name:       "create still rejects CREATE INDEX",
			subcmd:     "create",
			allowed:    "CREATE/MERGE",
			query:      `CREATE INDEX spec_key_idx FOR (n:Spec) ON (n.key)`,
			wantReject: true,
			wantMsg:    "graph create accepts only CREATE/MERGE queries",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateGuardRail(tc.subcmd, tc.allowed, tc.query)
			if tc.wantReject {
				if !errors.Is(err, utils.ErrValidation) {
					t.Fatalf("expected ErrValidation rejection, got %v", err)
				}
				if tc.wantMsg != "" && err.Error() != "validation error: "+tc.wantMsg {
					t.Fatalf("rejection message mismatch:\n got:  %q\n want: %q",
						err.Error(), "validation error: "+tc.wantMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected acceptance (nil error), got %v", err)
			}
		})
	}
}
