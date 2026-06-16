// Regression tests for the DDL guard rail (security finding #79).
//
// They lock in SPEC/GRAPH.md § Operation Classes and § Per-Subcommand
// Validation Rules note 5: schema-mutating DDL clauses (CREATE INDEX,
// DROP INDEX, CREATE CONSTRAINT, DROP CONSTRAINT) are not read-only and
// MUST be rejected by the read subcommands (query, search) with
// utils.ErrValidation (exit code 6) and the message
// "graph query accepts only read-only queries". DDL is also outside every
// write subcommand's accepted class, so create/update/delete reject it too.
//
// The detection runs on the literal-masked query, so a DDL keyword that
// appears only inside a string literal must NOT be misclassified as DDL.
package commands

import (
	"errors"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestValidateGuardRailRejectsDDL verifies that every DDL clause is rejected
// on the read path and on the write path, that a DDL keyword inside a string
// literal is NOT misclassified, and that ordinary read queries still pass.
func TestValidateGuardRailRejectsDDL(t *testing.T) {
	tests := []struct {
		name       string
		subcmd     string
		allowed    string
		query      string
		wantReject bool
		// wantMsg, when set, is the exact ErrValidation message expected for a
		// rejection, so the read-path contract string is pinned.
		wantMsg string
	}{
		// --- Read path (query): every DDL clause is rejected, exit 6. ---
		{
			name:       "query rejects CREATE INDEX",
			subcmd:     "query",
			allowed:    "read-only",
			query:      `CREATE INDEX idx FOR (n:Spec) ON (n.key)`,
			wantReject: true,
			wantMsg:    "graph query accepts only read-only queries",
		},
		{
			name:       "query rejects DROP INDEX",
			subcmd:     "query",
			allowed:    "read-only",
			query:      `DROP INDEX idx`,
			wantReject: true,
			wantMsg:    "graph query accepts only read-only queries",
		},
		{
			name:       "query rejects CREATE CONSTRAINT",
			subcmd:     "query",
			allowed:    "read-only",
			query:      `CREATE CONSTRAINT c FOR (n:Spec) REQUIRE n.key IS UNIQUE`,
			wantReject: true,
			wantMsg:    "graph query accepts only read-only queries",
		},
		{
			name:       "query rejects DROP CONSTRAINT",
			subcmd:     "query",
			allowed:    "read-only",
			query:      `DROP CONSTRAINT c`,
			wantReject: true,
			wantMsg:    "graph query accepts only read-only queries",
		},
		// --- Casing and whitespace must not bypass the guard (ir.IsDDL would). ---
		{
			name:       "query rejects lowercase create index",
			subcmd:     "query",
			allowed:    "read-only",
			query:      `create index idx FOR (n:Spec) ON (n.key)`,
			wantReject: true,
			wantMsg:    "graph query accepts only read-only queries",
		},
		{
			name:       "query rejects extra whitespace CREATE   INDEX",
			subcmd:     "query",
			allowed:    "read-only",
			query:      "CREATE   INDEX idx FOR (n:Spec) ON (n.key)",
			wantReject: true,
			wantMsg:    "graph query accepts only read-only queries",
		},
		// --- Read path (search): same read-only contract. ---
		{
			name:       "search rejects CREATE CONSTRAINT",
			subcmd:     "search",
			allowed:    "read-only",
			query:      `CREATE CONSTRAINT c FOR (n:Spec) REQUIRE n.key IS UNIQUE`,
			wantReject: true,
			wantMsg:    "graph search accepts only read-only queries",
		},
		{
			name:       "search rejects DROP INDEX",
			subcmd:     "search",
			allowed:    "read-only",
			query:      `DROP INDEX idx`,
			wantReject: true,
			wantMsg:    "graph search accepts only read-only queries",
		},
		// --- DDL keyword inside a string literal is NOT misclassified. ---
		{
			name:    "query accepts string literal containing CREATE INDEX",
			subcmd:  "query",
			allowed: "read-only",
			query:   `MATCH (n) WHERE n.x = 'CREATE INDEX' RETURN n`,
		},
		{
			name:    "query accepts string literal containing DROP CONSTRAINT",
			subcmd:  "query",
			allowed: "read-only",
			query:   `MATCH (n) WHERE n.note = 'we should DROP CONSTRAINT later' RETURN n.key`,
		},
		// --- Ordinary read queries still pass. ---
		{
			name:    "query accepts plain MATCH RETURN",
			subcmd:  "query",
			allowed: "read-only",
			query:   `MATCH (n:Spec) RETURN n.key`,
		},
		{
			name:    "search accepts variable-length traversal",
			subcmd:  "search",
			allowed: "read-only",
			query:   `MATCH p=(a)-[*1..3]-(b) RETURN p`,
		},
		// --- Write subcommands also reject DDL (outside every write class). ---
		{
			name:       "create rejects CREATE INDEX",
			subcmd:     "create",
			allowed:    "CREATE/MERGE",
			query:      `CREATE INDEX idx FOR (n:Spec) ON (n.key)`,
			wantReject: true,
			wantMsg:    "graph create accepts only CREATE/MERGE queries",
		},
		{
			name:       "update rejects CREATE CONSTRAINT",
			subcmd:     "update",
			allowed:    "SET/REMOVE",
			query:      `CREATE CONSTRAINT c FOR (n:Spec) REQUIRE n.key IS UNIQUE`,
			wantReject: true,
			wantMsg:    "graph update accepts only SET/REMOVE queries",
		},
		{
			name:       "delete rejects DROP CONSTRAINT",
			subcmd:     "delete",
			allowed:    "DELETE/DETACH DELETE",
			query:      `DROP CONSTRAINT c`,
			wantReject: true,
			wantMsg:    "graph delete accepts only DELETE/DETACH DELETE queries",
		},
		// --- A legitimate CREATE node write is NOT DDL (create still accepts). ---
		{
			name:    "create accepts plain CREATE node",
			subcmd:  "create",
			allowed: "CREATE/MERGE",
			query:   `CREATE (n:Spec {key:'auth'})`,
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
