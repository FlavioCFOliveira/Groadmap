// Tests for the ONE STATEMENT PER INVOCATION rule (SPEC/GRAPH.md § One
// Statement per Invocation; Acceptance Criterion 67).
//
// Three things need proving, and only the first is about Groadmap's own code:
//
//  1. a schema statement carrying a further clause is REFUSED, and a legitimate
//     schema statement whose name, label or property is spelled like a clause
//     keyword is ACCEPTED — the pair is what fixes the check as a structural one
//     rather than a keyword scan;
//  2. the engine really does discard the tail, silently and with success, so the
//     rule is load-bearing rather than defensive. That is measured here against
//     the pinned engine and not carried over from a report;
//  3. the statement extent this package computes agrees with the engine's own
//     parser on every accepted form, which is what keeps the mirror honest.
package cypherguard

import (
	"reflect"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher/ir"
)

// TestEngineSilentlyDiscardsTrailingClause measures the defect the rule exists
// for, against the pinned engine, rather than assuming it.
//
// If a future engine starts refusing a trailing clause itself, this test fails,
// and that failure is the signal to revisit whether Groadmap must still refuse
// it — not a licence to delete the rule, because the guard rail's refusal is
// exit code 6 and the engine's would be exit code 1.
func TestEngineSilentlyDiscardsTrailingClause(t *testing.T) {
	mixed := []struct {
		name  string
		whole string
		head  string
	}{
		{
			name:  "CREATE INDEX with a MATCH ... SET tail",
			whole: "CREATE INDEX spec_key FOR (n:Spec) ON (n.key) MATCH (m:Spec) SET m.reviewed = true",
			head:  "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)",
		},
		{
			name:  "DROP INDEX with a MATCH ... SET tail",
			whole: "DROP INDEX spec_key MATCH (m:Spec) SET m.reviewed = true",
			head:  "DROP INDEX spec_key",
		},
		{
			name:  "CREATE CONSTRAINT with a MATCH ... SET tail",
			whole: "CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE MATCH (m:Spec) SET m.reviewed = true",
			head:  "CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE",
		},
		{
			name:  "DROP CONSTRAINT with a MATCH ... SET tail",
			whole: "DROP CONSTRAINT spec_key_uq MATCH (m:Spec) SET m.reviewed = true",
			head:  "DROP CONSTRAINT spec_key_uq",
		},
	}

	for _, tc := range mixed {
		t.Run(tc.name, func(t *testing.T) {
			wholePlan, err := ir.ParseDDL(tc.whole)
			if err != nil {
				t.Fatalf("engine refused %q: %v — the premise of the rule no longer holds", tc.whole, err)
			}
			headPlan, err := ir.ParseDDL(tc.head)
			if err != nil {
				t.Fatalf("engine refused the head %q: %v", tc.head, err)
			}
			if !reflect.DeepEqual(wholePlan, headPlan) {
				t.Fatalf("engine parsed the mixed statement differently from its head:\n whole: %#v\n head:  %#v\n"+
					"the tail is no longer discarded, so this rule must be revisited", wholePlan, headPlan)
			}
		})
	}
}

// TestEngineRefusesTrailingClauseOnShow is the boundary the rule MUST NOT cross.
//
// SPEC/GRAPH.md § One Statement per Invocation confines the refusal to the DDL
// class because a SHOW command carrying a further clause is already refused by
// the engine, which names the unsupported clause instead of discarding it.
// Extending the rule there would change what `graph query`, `graph search` and
// the web graph data endpoint do with such a statement today.
func TestEngineRefusesTrailingClauseOnShow(t *testing.T) {
	for _, q := range []string{
		"SHOW INDEXES MATCH (m:Spec) SET m.reviewed = true",
		"SHOW CONSTRAINTS MATCH (m:Spec) SET m.reviewed = true",
	} {
		t.Run(q, func(t *testing.T) {
			if _, err := ir.ParseDDL(q); err == nil {
				t.Fatalf("engine accepted %q; SHOW is expected to refuse a trailing clause itself, "+
					"which is why this rule does not cover it", q)
			}
			if tail, found := TrailingClauseAfterSchemaStatement(q); found {
				t.Fatalf("TrailingClauseAfterSchemaStatement(%q) = %q, true; the rule must not reach the SHOW class", q, tail)
			}
		})
	}
}

// TestTrailingClauseRefusedAndTailNamed pins the refusing half of criterion 67:
// the trailing clause is detected and the text handed back is the trailing text,
// so the message that carries it can name what was refused.
func TestTrailingClauseRefusedAndTailNamed(t *testing.T) {
	cases := []struct {
		query    string
		wantTail string
	}{
		{
			query:    "CREATE INDEX spec_key FOR (n:Spec) ON (n.key) MATCH (m:Spec) SET m.reviewed = true",
			wantTail: "MATCH (m:Spec) SET m.reviewed = true",
		},
		{
			query:    "CREATE INDEX FOR (n:Spec) ON (n.title) MATCH (m:Spec) REMOVE m.reviewed",
			wantTail: "MATCH (m:Spec) REMOVE m.reviewed",
		},
		{
			query:    "CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'} MATCH (m:Spec) SET m.reviewed = true",
			wantTail: "MATCH (m:Spec) SET m.reviewed = true",
		},
		{
			query:    "DROP INDEX spec_key MATCH (m:Spec) DETACH DELETE m",
			wantTail: "MATCH (m:Spec) DETACH DELETE m",
		},
		{
			query:    "DROP INDEX spec_key IF EXISTS MATCH (m:Spec) SET m.reviewed = true",
			wantTail: "MATCH (m:Spec) SET m.reviewed = true",
		},
		{
			query:    "CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE CREATE (m:Spec {key:'smuggled'})",
			wantTail: "CREATE (m:Spec {key:'smuggled'})",
		},
		{
			query:    "CREATE CONSTRAINT spec_key_nn FOR (n:Spec) REQUIRE n.key IS NOT NULL MATCH (m) RETURN m",
			wantTail: "MATCH (m) RETURN m",
		},
		{
			query:    "DROP CONSTRAINT spec_key_uq MATCH (m:Spec) SET m.reviewed = true",
			wantTail: "MATCH (m:Spec) SET m.reviewed = true",
		},
		{
			// Lower case throughout: the walk uppercases keywords exactly as the
			// engine does, so the statement is recognised and its tail found.
			query:    "create index spec_key for (n:Spec) on (n.key) match (m:Spec) set m.reviewed = true",
			wantTail: "match (m:Spec) set m.reviewed = true",
		},
		{
			// A leading comment is trimmed before dispatch by the engine and by
			// this walk alike, so it does not hide the trailing clause.
			query:    "// register the lookup index\nCREATE INDEX spec_key FOR (n:Spec) ON (n.key) MATCH (m:Spec) SET m.reviewed = true",
			wantTail: "MATCH (m:Spec) SET m.reviewed = true",
		},
		{
			// The tail need not be a writing clause to be discarded, and a
			// discarded read is misreported just as badly as a discarded write.
			query:    "CREATE INDEX spec_key FOR (n:Spec) ON (n.key) RETURN 1",
			wantTail: "RETURN 1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			tail, found := TrailingClauseAfterSchemaStatement(tc.query)
			if !found {
				t.Fatalf("TrailingClauseAfterSchemaStatement(%q) = _, false; the engine executes the head and "+
					"discards %q with no error, so this must be refused", tc.query, tc.wantTail)
			}
			if tail != tc.wantTail {
				t.Errorf("tail = %q, want %q", tail, tc.wantTail)
			}
		})
	}
}

// TestLegitimateSchemaStatementsAccepted pins the admitting half of criterion
// 67, and it is the half a keyword scan fails.
//
// Every statement here names a schema object after a Cypher clause keyword. A
// check that searched the statement's text for `SET`, `MATCH`, `CREATE`,
// `DELETE` or `REMOVE` would refuse them all, and refusing them would deny the
// caller an index or a constraint the engine would have created.
func TestLegitimateSchemaStatementsAccepted(t *testing.T) {
	accepted := []string{
		// The exact statement SPEC/GRAPH.md § One Statement per Invocation names.
		"CREATE INDEX spec_set FOR (n:Spec) ON (n.set)",
		// A property named after every other writing clause.
		"CREATE INDEX spec_match FOR (n:Spec) ON (n.match)",
		"CREATE INDEX spec_delete FOR (n:Spec) ON (n.delete)",
		"CREATE INDEX spec_remove FOR (n:Spec) ON (n.remove)",
		"CREATE INDEX spec_merge FOR (n:Spec) ON (n.merge)",
		"CREATE INDEX spec_return FOR (n:Spec) ON (n.return)",
		// A LABEL named after a clause keyword.
		"CREATE INDEX set_key FOR (n:Set) ON (n.key)",
		"CREATE INDEX match_key FOR (n:Match) ON (n.key)",
		// A NAME that is a bare clause keyword.
		"CREATE INDEX set FOR (n:Spec) ON (n.key)",
		"DROP INDEX set",
		"DROP INDEX match IF EXISTS",
		"DROP CONSTRAINT delete",
		// Constraints over a property named after a clause keyword.
		"CREATE CONSTRAINT spec_set_uq FOR (n:Spec) REQUIRE n.set IS UNIQUE",
		"CREATE CONSTRAINT set FOR (n:Set) REQUIRE n.set IS NOT NULL",
		// Every accepted optional part of the grammar, so the walk is shown to
		// consume them rather than to mistake them for a trailing clause.
		"CREATE INDEX spec_key IF NOT EXISTS FOR (n:Spec) ON (n.key)",
		"CREATE INDEX IF NOT EXISTS spec_key FOR (n:Spec) ON (n.key)",
		"CREATE INDEX FOR (n:Spec) ON (n.title)",
		"CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}",
		"CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'hash'}",
		"CREATE CONSTRAINT spec_key_uq IF NOT EXISTS FOR (n:Spec) REQUIRE n.key IS UNIQUE",
		"CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE IF NOT EXISTS",
		"CREATE CONSTRAINT spec_key_nn FOR (n:Spec) REQUIRE n.key IS NOT NULL",
		"DROP INDEX spec_key",
		"DROP INDEX spec_key IF EXISTS",
		"DROP CONSTRAINT spec_key_uq",
		"DROP CONSTRAINT spec_key_uq IF EXISTS",
		// Inert trailing text is not a clause: whitespace, a terminator, and a
		// comment each leave the statement complete and alone.
		"CREATE INDEX spec_key FOR (n:Spec) ON (n.key)   ",
		"CREATE INDEX spec_key FOR (n:Spec) ON (n.key);",
		"CREATE INDEX spec_key FOR (n:Spec) ON (n.key) // registers the lookup path",
		"CREATE INDEX spec_key FOR (n:Spec) ON (n.key) /* registers the lookup path */",
		"DROP INDEX spec_key;",
	}

	for _, q := range accepted {
		t.Run(q, func(t *testing.T) {
			if tail, found := TrailingClauseAfterSchemaStatement(q); found {
				t.Fatalf("TrailingClauseAfterSchemaStatement(%q) = %q, true; this statement is one the engine "+
					"executes whole, and refusing it would deny the caller a schema object", q, tail)
			}
		})
	}
}

// TestExtentAgreesWithEngineOnAcceptedStatements is the differential check that
// keeps the mirror honest: for every statement the engine parses whole, the
// extent this package computes must cover the entire token stream.
//
// It is stated as "the extent reaches the end" rather than as a token count so
// that it keeps its meaning if the tokeniser changes: what matters is that
// nothing legitimate is left over to be mistaken for a trailing clause.
func TestExtentAgreesWithEngineOnAcceptedStatements(t *testing.T) {
	statements := []string{
		"CREATE INDEX spec_key FOR (n:Spec) ON (n.key)",
		"CREATE INDEX FOR (n:Spec) ON (n.title)",
		"CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'}",
		"CREATE INDEX spec_key IF NOT EXISTS FOR (n:Spec) ON (n.key)",
		"CREATE INDEX IF NOT EXISTS spec_key FOR (n:Spec) ON (n.key)",
		"CREATE INDEX spec_set FOR (n:Spec) ON (n.set)",
		"DROP INDEX spec_key",
		"DROP INDEX spec_key IF EXISTS",
		"CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE",
		"CREATE CONSTRAINT spec_key_nn FOR (n:Spec) REQUIRE n.key IS NOT NULL",
		"CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE IF NOT EXISTS",
		"CREATE CONSTRAINT spec_key_uq ON (n:Spec) ASSERT n.key IS UNIQUE",
		"DROP CONSTRAINT spec_key_uq",
		"DROP CONSTRAINT spec_key_uq IF EXISTS",
	}

	for _, q := range statements {
		t.Run(q, func(t *testing.T) {
			if _, err := ir.ParseDDL(q); err != nil {
				t.Fatalf("engine refused %q: %v — the corpus must contain only statements it accepts", q, err)
			}
			form, ok := schemaMutatingForm(q)
			if !ok {
				t.Fatalf("schemaMutatingForm(%q) = _, false, but the engine parsed it as schema DDL", q)
			}
			toks := tokenizeDDL(q)
			end, ok := form.extent(toks)
			if !ok {
				t.Fatalf("extent(%q) = _, false; the walk must reach the end of a statement the engine accepts", q)
			}
			if end != len(toks) {
				t.Fatalf("extent(%q) = %d of %d tokens; the leftover %q would be misread as a trailing clause",
					q, end, len(toks), q[toks[end].start:])
			}
		})
	}
}

// TestTokenizeDDLMatchesEngineShape pins the tokenisation the extent walk
// depends on: whitespace separates, the six punctuation characters plus the
// terminator are tokens of their own, and everything else arrives whole — which
// is why `n.set` is one token and its `set` is never a keyword position.
func TestTokenizeDDLMatchesEngineShape(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{
			in:   "CREATE INDEX spec_set FOR (n:Spec) ON (n.set)",
			want: []string{"CREATE", "INDEX", "spec_set", "FOR", "(", "n", ":", "Spec", ")", "ON", "(", "n.set", ")"},
		},
		{
			in:   "OPTIONS {indexType: 'btree'}",
			want: []string{"OPTIONS", "{", "indexType", ":", "'btree'", "}"},
		},
		{
			in:   "DROP\tINDEX\nspec_key;",
			want: []string{"DROP", "INDEX", "spec_key", ";"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			toks := tokenizeDDL(tc.in)
			got := make([]string, len(toks))
			for i, tok := range toks {
				got[i] = tok.text
				if tc.in[tok.start:tok.start+len(tok.text)] != tok.text {
					t.Errorf("token %d %q does not sit at its recorded offset %d", i, tok.text, tok.start)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("tokenizeDDL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestTrailingClauseRuleIgnoresNonSchemaQueries confirms the rule has no reach
// outside the four schema-mutating forms, so it can never refuse an ordinary
// read or write, nor a statement the engine will not route to its schema parser.
func TestTrailingClauseRuleIgnoresNonSchemaQueries(t *testing.T) {
	outside := []string{
		// Ordinary data statements, including ones whose text mentions a schema
		// keyword inside a literal.
		"MATCH (n:Spec) RETURN n.key",
		"MATCH (n:Spec {key:'user-authentication'}) SET n.status = 'implemented'",
		"CREATE (n:Memory {body:'we should CREATE INDEX later'})",
		// A statement whose keyword spacing the engine does not route to its
		// schema parser: it is admitted here and refused by the engine, which is
		// what Acceptance Criterion 69 requires. Refusing it here would give it
		// exit code 6 instead of the 1 the criterion fixes.
		"CREATE   INDEX spec_key FOR (n:Spec) ON (n.key) MATCH (m) SET m.x = 1",
		"CREATE\tINDEX spec_key FOR (n:Spec) ON (n.key)",
		"DROP\nINDEX spec_key",
		// A data-writing clause FIRST and schema text after it is not a schema
		// statement at all: the engine routes it to the general grammar, which
		// refuses it as a parse error rather than discarding anything.
		"MATCH (m:Spec) SET m.reviewed = true CREATE INDEX spec_key FOR (n:Spec) ON (n.key)",
		// Schema introspection, whose own parser refuses a trailing clause.
		"SHOW INDEXES",
		"SHOW CONSTRAINTS YIELD name",
	}
	for _, q := range outside {
		t.Run(q, func(t *testing.T) {
			if tail, found := TrailingClauseAfterSchemaStatement(q); found {
				t.Fatalf("TrailingClauseAfterSchemaStatement(%q) = %q, true; the rule must not reach outside "+
					"the four schema-mutating forms", q, tail)
			}
		})
	}
}

// TestTrailingClauseRuleRefusesOnlyWhatTheEngineDiscards is the invariant behind
// the runtime proof step, asserted over the whole corpus at once: whenever the
// rule refuses, the engine parses the head and the whole statement to the same
// plan — which is what makes the refusal a statement about the engine's
// behaviour rather than about this package's opinion.
func TestTrailingClauseRuleRefusesOnlyWhatTheEngineDiscards(t *testing.T) {
	corpus := []string{
		"CREATE INDEX spec_key FOR (n:Spec) ON (n.key) MATCH (m:Spec) SET m.reviewed = true",
		"DROP INDEX spec_key MATCH (m:Spec) SET m.reviewed = true",
		"CREATE CONSTRAINT spec_key_uq FOR (n:Spec) REQUIRE n.key IS UNIQUE MATCH (m) RETURN m",
		"DROP CONSTRAINT spec_key_uq REMOVE n.x",
		"CREATE INDEX spec_ord FOR (n:Spec) ON (n.ord) OPTIONS {indexType: 'btree'} RETURN 1",
	}
	for _, q := range corpus {
		t.Run(q, func(t *testing.T) {
			tail, found := TrailingClauseAfterSchemaStatement(q)
			if !found {
				t.Fatalf("expected %q to be refused", q)
			}
			head := strings.TrimSuffix(q, tail)
			headPlan, headErr := ir.ParseDDL(head)
			wholePlan, wholeErr := ir.ParseDDL(q)
			if headErr != nil || wholeErr != nil {
				t.Fatalf("engine errors: head %v, whole %v", headErr, wholeErr)
			}
			if !reflect.DeepEqual(headPlan, wholePlan) {
				t.Fatalf("the refusal is not backed by the engine: head %#v, whole %#v", headPlan, wholePlan)
			}
		})
	}
}
