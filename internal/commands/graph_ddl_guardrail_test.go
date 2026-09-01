// Regression tests for the DDL guard rail (security finding #79).
//
// They lock in SPEC/GRAPH.md § Operation Classes and § Per-Subcommand
// Validation Rules note 5: schema-mutating DDL clauses (CREATE INDEX,
// DROP INDEX, CREATE CONSTRAINT, DROP CONSTRAINT) are not read-only and
// MUST be rejected by the read subcommands (query, search) with
// utils.ErrValidation (exit code 6) and the message
// "graph query accepts only read-only queries". DDL is outside the accepted
// class of `graph create` and `graph delete` too, each of which accepts only its
// own data-writing clause.
//
// `graph update` is the ONE subcommand that accepts the class, and it does so by
// decision rather than by omission: it is the subcommand through which the
// graph's schema is managed (SPEC/GRAPH.md § Per-Subcommand Validation Rules
// note 5; § Schema Management). Its acceptance is asserted here beside the four
// refusals, so that widening the class on any of those four — or narrowing it on
// update — fails a test rather than passing unnoticed.
//
// The detection runs on the literal-masked query, so a DDL keyword that
// appears only inside a string literal must NOT be misclassified as DDL.
package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/cypherguard"
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
		// --- `graph update` ACCEPTS the DDL class: it is the schema subcommand. ---
		{
			name:    "update accepts CREATE INDEX",
			subcmd:  "update",
			allowed: "SET/REMOVE",
			query:   `CREATE INDEX idx FOR (n:Spec) ON (n.key)`,
		},
		{
			name:    "update accepts DROP INDEX",
			subcmd:  "update",
			allowed: "SET/REMOVE",
			query:   `DROP INDEX idx`,
		},
		{
			name:    "update accepts CREATE CONSTRAINT",
			subcmd:  "update",
			allowed: "SET/REMOVE",
			query:   `CREATE CONSTRAINT c FOR (n:Spec) REQUIRE n.key IS UNIQUE`,
		},
		{
			name:    "update accepts DROP CONSTRAINT",
			subcmd:  "update",
			allowed: "SET/REMOVE",
			query:   `DROP CONSTRAINT c`,
		},
		{
			// The DDL matcher stays whitespace-tolerant, so `graph update`
			// admits this spelling and the engine refuses it at the grammar with
			// exit code 1. Acceptance Criterion 69 fixes that as the whole cost
			// of the tolerance; narrowing the matcher to a single space to
			// improve the diagnostic would fail Criterion 27 instead.
			name:    "update accepts CREATE   INDEX and leaves the spacing to the engine",
			subcmd:  "update",
			allowed: "SET/REMOVE",
			query:   "CREATE   INDEX idx FOR (n:Spec) ON (n.key)",
		},
		{
			// A mutating write is still the subcommand's original class.
			name:    "update still accepts a SET write",
			subcmd:  "update",
			allowed: "SET/REMOVE",
			query:   `MATCH (n:Spec {key:'auth'}) SET n.status = 'done'`,
		},
		{
			// And a data-writing CREATE is still outside every class it accepts.
			name:       "update rejects a data-writing CREATE",
			subcmd:     "update",
			allowed:    "SET/REMOVE",
			query:      `CREATE (n:Spec {key:'auth'})`,
			wantReject: true,
			wantMsg:    "graph update accepts only SET/REMOVE, index/constraint DDL, and schema-introspection queries",
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

// TestValidateGuardRailRejectsSpoofedDDLKeywords is the CLI-side regression for
// the guard-rail bypass a security audit proved end to end: a DDL keyword whose
// letters include a non-ASCII code point that Unicode UPPERCASING maps onto the
// ASCII letter (U+0131 dotless i uppercases to 'I', U+017F long s uppercases to
// 'S') was invisible to a case-insensitive regexp, which folds rather than
// uppercases — while the engine's dispatcher decides on strings.ToUpper and
// routed the statement to its DDL executor.
//
// Proven before the fix: `rmp graph query -q "CREATE <U+0131>NDEX idx FOR
// (n:Seed) ON (n.k)"` exited 0 with the empty DDL result, and the DROP form
// reached exec.DropIndex. Both read subcommands must refuse every such form with
// the pinned read-path message, and no write subcommand may accept it either.
func TestValidateGuardRailRejectsSpoofedDDLKeywords(t *testing.T) {
	spoofed := []string{
		"CREATE ıNDEX idx FOR (n:Spec) ON (n.key)",
		"CREATE ıNDEX IF NOT EXISTS idx FOR (n:Spec) ON (n.key)",
		"DROP ıNDEX idx",
		"drop ındex idx",
		"CREATE CONSTRAıNT c1 FOR (n:Spec) REQUIRE n.key IS UNIQUE",
		"DROP CONSTRAıNT c1",
		"CREATE CONſTRAINT c1 FOR (n:Spec) REQUIRE n.key IS UNIQUE",
	}

	for _, query := range spoofed {
		for _, sub := range []struct{ name, allowed, wantMsg string }{
			{name: "query", allowed: "read-only", wantMsg: "graph query accepts only read-only queries"},
			{name: "search", allowed: "read-only", wantMsg: "graph search accepts only read-only queries"},
			{name: "create", allowed: "CREATE/MERGE", wantMsg: "graph create accepts only CREATE/MERGE queries"},
			{name: "delete", allowed: "DELETE/DETACH DELETE", wantMsg: "graph delete accepts only DELETE/DETACH DELETE queries"},
		} {
			t.Run(sub.name+": "+query, func(t *testing.T) {
				err := validateGuardRail(sub.name, sub.allowed, query)
				if err == nil {
					t.Fatalf("validateGuardRail(%q, %q) = nil, want a rejection: the engine executes this as schema DDL", sub.name, query)
				}
				if !errors.Is(err, utils.ErrValidation) {
					t.Errorf("error = %v, want utils.ErrValidation (exit code 6)", err)
				}
				if got := err.Error(); !strings.Contains(got, sub.wantMsg) {
					t.Errorf("message = %q, want it to contain %q", got, sub.wantMsg)
				}
			})
		}
	}
}

// TestValidateGuardRailSeesSpoofedDDLKeywordsOnUpdateToo is the other half of the
// spoofing regression, restated for the one subcommand that now ACCEPTS the DDL
// class.
//
// The bypass being guarded against was never "the statement runs"; it was "the
// guard rail and the engine disagree about what the statement IS". On
// `graph update` that disagreement would be just as real and just as invisible:
// the statement would be admitted as an ordinary read or refused as a data
// write, while the engine executed it as schema DDL. So what must be asserted
// here is not a refusal but that the guard rail CLASSIFIES these as DDL, which
// is exactly what admitting them under the schema subcommand means. A
// classification that missed them would surface as the class refusal below.
func TestValidateGuardRailSeesSpoofedDDLKeywordsOnUpdateToo(t *testing.T) {
	spoofed := []string{
		"CREATE ıNDEX idx FOR (n:Spec) ON (n.key)",
		"CREATE ıNDEX IF NOT EXISTS idx FOR (n:Spec) ON (n.key)",
		"DROP ıNDEX idx",
		"drop ındex idx",
		"CREATE CONSTRAıNT c1 FOR (n:Spec) REQUIRE n.key IS UNIQUE",
		"DROP CONSTRAıNT c1",
		"CREATE CONſTRAINT c1 FOR (n:Spec) REQUIRE n.key IS UNIQUE",
	}
	for _, query := range spoofed {
		t.Run(query, func(t *testing.T) {
			if !cypherguard.Classify(query).DDL {
				t.Fatalf("Classify(%q).DDL = false; the engine routes this to its schema executor, so the "+
					"guard rail must see the same statement it does", query)
			}
			if err := validateGuardRail("update", "SET/REMOVE", query); err != nil {
				t.Errorf("validateGuardRail(update, %q) = %v, want nil: `graph update` accepts the DDL class", query, err)
			}
		})
	}
}

// TestValidateGuardRailKeepsUnicodeReadsAccepted is the other half: a read whose
// identifiers merely CONTAIN those code points must still be accepted, so the
// stricter classification cannot be mistaken for a blanket ban on non-ASCII.
func TestValidateGuardRailKeepsUnicodeReadsAccepted(t *testing.T) {
	reads := []string{
		"MATCH (n:Yazılım) RETURN n",
		"MATCH (n:Spec) WHERE n.başlık = 'auth' RETURN n.key",
		"MATCH (n:Spec) WHERE n.note = 'CREATE ıNDEX idx' RETURN n",
		"MATCH (n) RETURN n // CREATE ıNDEX idx",
	}
	for _, query := range reads {
		t.Run(query, func(t *testing.T) {
			if err := validateGuardRail("query", "read-only", query); err != nil {
				t.Errorf("validateGuardRail(query, %q) = %v, want nil: an ordinary read must stay admissible", query, err)
			}
		})
	}
}
