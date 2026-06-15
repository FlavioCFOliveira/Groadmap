// Regression tests for the literal-aware guard rail (rmp task #36).
//
// They lock in SPEC/GRAPH.md § Literal-Aware Normalization and Acceptance
// Criteria 18/19: clause keywords that appear only inside Cypher string
// literals, comments, or backtick identifiers MUST NOT influence the
// operation-class classification, while genuine cross-class clauses MUST
// still be rejected with utils.ErrValidation.
package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestMaskCypherLiterals verifies the masking state machine: keywords inside
// quoted spans, comments, and backtick identifiers are neutralized to spaces,
// keywords outside are preserved, and both delimiters and overall length are
// kept so token positions never shift.
func TestMaskCypherLiterals(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "double-quoted literal masks interior keywords",
			in:   `CREATE (m:Memory {body:"discusses delete, set and detach"})`,
			want: `CREATE (m:Memory {body:"                                "})`,
		},
		{
			name: "single-quoted literal masks interior keywords",
			in:   `MATCH (m) WHERE m.t = 'delete and set' RETURN m`,
			want: `MATCH (m) WHERE m.t = '              ' RETURN m`,
		},
		{
			name: "escaped double quote does not close the literal",
			in:   `CREATE (n {v:"a \" SET b"})`,
			want: `CREATE (n {v:"          "})`,
		},
		{
			name: "escaped single quote does not close the literal",
			in:   `CREATE (n {v:'a \' DELETE b'})`,
			want: `CREATE (n {v:'             '})`,
		},
		{
			name: "escaped backslash then real closing quote",
			in:   `CREATE (n {v:"a\\"}) SET x`,
			want: `CREATE (n {v:"   "}) SET x`,
		},
		{
			name: "backtick identifier masks interior keywords",
			in:   "MATCH (n) RETURN n.`DELETE col`",
			want: "MATCH (n) RETURN n.`          `",
		},
		{
			name: "line comment masks keywords and marker to end of line",
			in:   "MATCH (n) // SET n.x = 1\nRETURN n",
			want: "MATCH (n)               \nRETURN n",
		},
		{
			name: "block comment masks keywords and markers across span",
			in:   "MATCH (n) /* DELETE everything */ RETURN n",
			want: "MATCH (n)                         RETURN n",
		},
		{
			name: "double quote inside line comment does not open a literal",
			in:   "MATCH (n) // a \" SET\nDELETE n",
			want: "MATCH (n)           \nDELETE n",
		},
		{
			name: "comment marker inside string is literal text, not a comment",
			in:   `CREATE (n {v:"// SET DELETE"})`,
			want: `CREATE (n {v:"             "})`,
		},
		{
			name: "keywords outside literals are fully preserved",
			in:   `MATCH (n:Spec {key:"x"}) SET n.status = "done"`,
			want: `MATCH (n:Spec {key:" "}) SET n.status = "    "`,
		},
		{
			name: "no literals leaves the query untouched",
			in:   `MATCH (n) DETACH DELETE n`,
			want: `MATCH (n) DETACH DELETE n`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := maskCypherLiterals(tc.in)
			if got != tc.want {
				t.Fatalf("maskCypherLiterals mismatch\n in:   %q\n got:  %q\n want: %q", tc.in, got, tc.want)
			}
			if len(got) != len(tc.in) {
				t.Fatalf("length changed: in=%d got=%d (positions must be preserved)", len(tc.in), len(got))
			}
		})
	}
}

// TestMaskCypherLiteralsPreservesDelimiters asserts that every quote/backtick
// delimiter byte survives masking verbatim at its original index.
func TestMaskCypherLiteralsPreservesDelimiters(t *testing.T) {
	in := "CREATE (n {a:'x', b:\"y\", c:`z`})"
	got := maskCypherLiterals(in)
	for i := 0; i < len(in); i++ {
		switch in[i] {
		case '\'', '"', '`':
			if got[i] != in[i] {
				t.Fatalf("delimiter at %d altered: got %q want %q", i, got[i], in[i])
			}
		}
	}
}

// TestValidateGuardRailLiteralAware exercises validateGuardRail across all five
// subcommands: literals merely mentioning other-class keywords are accepted,
// while genuine cross-class clauses are rejected with utils.ErrValidation.
func TestValidateGuardRailLiteralAware(t *testing.T) {
	tests := []struct {
		name       string
		subcmd     string
		allowed    string
		query      string
		wantReject bool
	}{
		// --- Acceptance Criteria 18 and 19 (the two reproduced symptoms) ---
		{
			name:    "AC18: create accepts literal mentioning delete/set/match",
			subcmd:  "create",
			allowed: "CREATE/MERGE",
			query:   `CREATE (m:Memory {body:"... node-delete ... MATCH...SET ..."})`,
		},
		{
			name:    "AC19: query accepts WHERE literal mentioning delete and set",
			subcmd:  "query",
			allowed: "read-only",
			query:   `MATCH (m) WHERE m.title = "mentions delete and set" RETURN m.key`,
		},
		// --- Other literal-mention acceptances per subcommand ---
		{
			name:    "search accepts literal mentioning create",
			subcmd:  "search",
			allowed: "read-only",
			query:   `MATCH p=(a)-[*1..3]-(b) WHERE b.note = "create more" RETURN p`,
		},
		{
			name:    "update accepts SET with literal mentioning create/delete",
			subcmd:  "update",
			allowed: "SET/REMOVE",
			query:   `MATCH (t:Task {key:'x'}) SET t.note = 'create or delete later'`,
		},
		{
			name:    "delete accepts DETACH DELETE with literal mentioning set",
			subcmd:  "delete",
			allowed: "DELETE/DETACH DELETE",
			query:   `MATCH (n {note:'remember to set status'}) DETACH DELETE n`,
		},
		{
			name:    "create accepts comment mentioning delete",
			subcmd:  "create",
			allowed: "CREATE/MERGE",
			query:   "CREATE (n:Spec {key:'auth'}) // TODO later DELETE this",
		},
		// --- Genuine cross-class rejections ---
		{
			name:       "create rejects real DETACH DELETE",
			subcmd:     "create",
			allowed:    "CREATE/MERGE",
			query:      `MATCH (n) DETACH DELETE n`,
			wantReject: true,
		},
		{
			name:       "create rejects read-only query",
			subcmd:     "create",
			allowed:    "CREATE/MERGE",
			query:      `MATCH (n) RETURN n`,
			wantReject: true,
		},
		{
			name:       "query rejects real SET",
			subcmd:     "query",
			allowed:    "read-only",
			query:      `MATCH (n:Spec {key:'x'}) SET n.status = 'done'`,
			wantReject: true,
		},
		{
			name:       "search rejects real CREATE",
			subcmd:     "search",
			allowed:    "read-only",
			query:      `CREATE (n:Spec {key:'x'})`,
			wantReject: true,
		},
		{
			name:       "update rejects real CREATE",
			subcmd:     "update",
			allowed:    "SET/REMOVE",
			query:      `CREATE (n:Spec {key:'x'}) SET n.y = 1`,
			wantReject: true,
		},
		{
			name:       "delete rejects real CREATE",
			subcmd:     "delete",
			allowed:    "DELETE/DETACH DELETE",
			query:      `CREATE (n:Spec {key:'x'})`,
			wantReject: true,
		},
		{
			name:       "delete rejects real SET",
			subcmd:     "delete",
			allowed:    "DELETE/DETACH DELETE",
			query:      `MATCH (n) SET n.x = 1`,
			wantReject: true,
		},
		// --- Legitimate same-class acceptances (no literals involved) ---
		{
			name:    "create accepts plain CREATE",
			subcmd:  "create",
			allowed: "CREATE/MERGE",
			query:   `CREATE (n:Spec {key:'auth'})`,
		},
		{
			name:    "query accepts plain MATCH RETURN",
			subcmd:  "query",
			allowed: "read-only",
			query:   `MATCH (n) RETURN count(n)`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateGuardRail(tc.subcmd, tc.allowed, tc.query)
			if tc.wantReject {
				if !errors.Is(err, utils.ErrValidation) {
					t.Fatalf("expected ErrValidation rejection, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected acceptance (nil error), got %v", err)
			}
		})
	}
}

// TestValidateGuardRailExecutionPathUnchanged is a guard that masking did not
// leak into anything other than classification: maskCypherLiterals must be a
// pure function that never mutates its input.
func TestMaskCypherLiteralsPure(t *testing.T) {
	in := `CREATE (n {v:"delete set"})`
	orig := strings.Clone(in)
	_ = maskCypherLiterals(in)
	if in != orig {
		t.Fatalf("maskCypherLiterals mutated its input: %q", in)
	}
}
