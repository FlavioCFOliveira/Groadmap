package cypherguard_test

import (
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/cypher/ir"
	"github.com/FlavioCFOliveira/Groadmap/internal/cypherguard"
)

// spaces returns a run of n spaces, used to build the expected masked output of
// a literal/comment span whose interior is neutralized to spaces.
func spaces(n int) string { return strings.Repeat(" ", n) }

// TestMaskLiterals verifies the exact masked output of MaskLiterals for every
// span kind it recognizes. Each case asserts the full string so that delimiter
// preservation, length preservation, and the precise neutralized span are all
// checked at once (SPEC/GRAPH.md § Literal-Aware Normalization).
func TestMaskLiterals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "empty",
			query: "",
			want:  "",
		},
		{
			name:  "no literals unchanged",
			query: "MATCH (n:Task) RETURN n",
			want:  "MATCH (n:Task) RETURN n",
		},
		{
			name:  "single quoted interior masked delimiters kept",
			query: "n.name = 'Alice'",
			want:  "n.name = '" + spaces(5) + "'",
		},
		{
			name:  "double quoted interior masked delimiters kept",
			query: `RETURN "hello world"`,
			want:  `RETURN "` + spaces(11) + `"`,
		},
		{
			name:  "backtick identifier interior masked delimiters kept",
			query: "MATCH (n:`Hot Label`) RETURN n",
			want:  "MATCH (n:`" + spaces(9) + "`) RETURN n",
		},
		{
			name:  "line comment marker and body masked newline preserved",
			query: "RETURN 1 // note\nRETURN 2",
			want:  "RETURN 1 " + spaces(7) + "\nRETURN 2",
		},
		{
			name:  "block comment markers and body masked",
			query: "y /* z */ w",
			want:  "y" + spaces(9) + "w",
		},
		{
			name:  "escaped single quote does not close literal",
			query: "'a\\'b'",
			want:  "'" + spaces(4) + "'",
		},
		{
			name:  "escaped double quote does not close literal",
			query: `"a\"b"`,
			want:  `"` + spaces(4) + `"`,
		},
		{
			name:  "adjacent single quoted literals",
			query: "'ab''cd'",
			want:  "'" + spaces(2) + "''" + spaces(2) + "'",
		},
		{
			name:  "double quote inside single quoted literal is interior",
			query: "x = 'she said \"hi\"'",
			want:  "x = '" + spaces(13) + "'",
		},
		{
			name:  "comment marker inside string literal is interior",
			query: "url = 'http://example.com'",
			want:  "url = '" + spaces(18) + "'",
		},
		{
			name:  "quote inside line comment does not open literal",
			query: "RETURN n // it's fine",
			want:  "RETURN n " + spaces(12),
		},
		{
			name:  "clause keyword inside single quoted literal is masked",
			query: "WHERE x = 'CREATE'",
			want:  "WHERE x = '" + spaces(6) + "'",
		},
		{
			name:  "unterminated single quoted literal masked to end",
			query: "n.name = 'unterminated",
			want:  "n.name = '" + spaces(12),
		},
		{
			name:  "unterminated block comment masked to end",
			query: "RETURN 1 /* open",
			want:  "RETURN 1 " + spaces(7),
		},
		{
			name:  "unterminated backtick identifier masked to end",
			query: "MATCH (n:`Open",
			want:  "MATCH (n:`" + spaces(4),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cypherguard.MaskLiterals(tc.query)
			if got != tc.want {
				t.Errorf("MaskLiterals(%q) = %q, want %q", tc.query, got, tc.want)
			}
			if len(got) != len(tc.query) {
				t.Errorf("MaskLiterals(%q) length = %d, want %d (length must be preserved)",
					tc.query, len(got), len(tc.query))
			}
		})
	}
}

// TestMaskLiteralsLengthInvariant asserts the universal contract that masking
// never changes the byte length of a query, regardless of the span kinds
// present. Byte positions of every unmasked token must stay put so that the
// classifier sees clause keywords at their original offsets.
func TestMaskLiteralsLengthInvariant(t *testing.T) {
	t.Parallel()

	queries := []string{
		"",
		"MATCH (n) RETURN n",
		"CREATE (n:Task {title: 'Ship the v2 release'})",
		"MATCH (n:Task) WHERE n.note = 'do not /* CREATE */ here' RETURN n",
		"MATCH (n:`Weird ` + `Label`) RETURN n // trailing comment with 'quote' and CREATE",
		"RETURN 'a\\'b', \"c\\\"d\", `e`",
		"/* unterminated block comment without close",
		"'unterminated string",
	}

	for _, q := range queries {
		if got := len(cypherguard.MaskLiterals(q)); got != len(q) {
			t.Errorf("MaskLiterals(%q) length = %d, want %d", q, got, len(q))
		}
	}
}

// TestClassify exercises the full Classes result for representative Cypher,
// covering every clause class and the read/write/DDL interactions, including
// the literal-aware behaviour that keeps the guard rail honest.
func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		want  cypherguard.Classes
	}{
		{
			name:  "plain read match",
			query: "MATCH (n:Task) RETURN n",
			want:  cypherguard.Classes{},
		},
		{
			name:  "read match with where and literal",
			query: "MATCH (n:Task) WHERE n.priority = 'P1' RETURN n.title",
			want:  cypherguard.Classes{},
		},
		{
			name:  "create node is a write create",
			query: "CREATE (n:Task {title: 'Implement OAuth login'})",
			want:  cypherguard.Classes{Write: true, Create: true},
		},
		{
			name:  "merge node is a write create",
			query: "MERGE (u:User {email: 'dev@example.com'})",
			want:  cypherguard.Classes{Write: true, Create: true},
		},
		{
			name:  "set property is a write mutate",
			query: "MATCH (n:Task {id: 42}) SET n.status = 'done'",
			want:  cypherguard.Classes{Write: true, Mutate: true},
		},
		{
			name:  "remove property is a write mutate",
			query: "MATCH (n:Task) WHERE n.id = 7 REMOVE n.assignee",
			want:  cypherguard.Classes{Write: true, Mutate: true},
		},
		{
			name:  "delete node is a write delete",
			query: "MATCH (n:Task {id: 99}) DELETE n",
			want:  cypherguard.Classes{Write: true, Delete: true},
		},
		{
			name:  "detach delete node is a write delete",
			query: "MATCH (s:Sprint {id: 3}) DETACH DELETE s",
			want:  cypherguard.Classes{Write: true, Delete: true},
		},
		{
			name:  "create index is DDL not a write",
			query: "CREATE INDEX task_status_idx FOR (n:Task) ON (n.status)",
			want:  cypherguard.Classes{Create: true, DDL: true},
		},
		{
			name:  "drop index is DDL not a write",
			query: "DROP INDEX task_status_idx",
			want:  cypherguard.Classes{DDL: true},
		},
		{
			name:  "create constraint is DDL not a write",
			query: "CREATE CONSTRAINT unique_task_title FOR (n:Task) REQUIRE n.title IS UNIQUE",
			want:  cypherguard.Classes{Create: true, DDL: true},
		},
		{
			// The legacy ON ... ASSERT form is still accepted and applied by the
			// GoGraph engine as an alias of the modern FOR ... REQUIRE form, so
			// the guard rail must classify it as DDL exactly the same way. This
			// case exists to fail loudly if reDDL is ever narrowed to the modern
			// spelling, which would let a legacy constraint DDL slip through a
			// read-only subcommand.
			name:  "legacy create constraint ON ASSERT form is DDL not a write",
			query: "CREATE CONSTRAINT unique_task_title ON (n:Task) ASSERT n.title IS UNIQUE",
			want:  cypherguard.Classes{Create: true, DDL: true},
		},
		{
			name:  "create constraint is not null is DDL not a write",
			query: "CREATE CONSTRAINT title_exists FOR (n:Task) REQUIRE n.title IS NOT NULL",
			want:  cypherguard.Classes{Create: true, DDL: true},
		},
		{
			// The name-before-IF-NOT-EXISTS ordering the engine accepts. NOT and
			// EXISTS are not writing keywords, so the classes are unchanged from
			// the plain create constraint form.
			name:  "create constraint if not exists is DDL not a write",
			query: "CREATE CONSTRAINT unique_task_title IF NOT EXISTS FOR (n:Task) REQUIRE n.title IS UNIQUE",
			want:  cypherguard.Classes{Create: true, DDL: true},
		},
		{
			name:  "drop constraint is DDL not a write",
			query: "DROP CONSTRAINT unique_task_title",
			want:  cypherguard.Classes{DDL: true},
		},
		{
			// IsDDL is single-space sensitive, so the multi-space form is NOT
			// recognized as DDL by GoGraph and falls through to the writing
			// keyword check (Write=true), while the whitespace-tolerant reDDL
			// still flags DDL=true. The guard rail catches the non-canonical
			// spacing that GoGraph's IsDDL would miss.
			name:  "create index with extra spaces is both write and DDL",
			query: "CREATE   INDEX task_idx FOR (n:Task) ON (n.status)",
			want:  cypherguard.Classes{Write: true, Create: true, DDL: true},
		},
		{
			name:  "lowercase create index is DDL not a write",
			query: "create index task_idx for (n:Task) on (n.status)",
			want:  cypherguard.Classes{Create: true, DDL: true},
		},
		{
			name:  "lowercase legacy create constraint is DDL not a write",
			query: "create constraint c1 on (n:Task) assert n.title is unique",
			want:  cypherguard.Classes{Create: true, DDL: true},
		},
		{
			// Same evasion shape as the multi-space CREATE INDEX case above:
			// IsDDL is single-space sensitive, so it does not recognize the
			// padded form and the query falls through to the writing keyword
			// check (Write=true), while the whitespace-tolerant reDDL still
			// flags DDL=true. Casing and padding therefore cannot smuggle a
			// legacy constraint DDL past the guard rail.
			name:  "lowercase legacy create constraint with extra spaces is both write and DDL",
			query: "create   constraint c1 on (n:Task) assert n.title is unique",
			want:  cypherguard.Classes{Write: true, Create: true, DDL: true},
		},
		{
			name:  "mixed case write create",
			query: "mAtCh (n) cReAtE (m:Subtask)",
			want:  cypherguard.Classes{Write: true, Create: true},
		},
		{
			name:  "leading and trailing whitespace around read query",
			query: "   MATCH (n:Task) RETURN n   ",
			want:  cypherguard.Classes{},
		},
		{
			name:  "leading and trailing whitespace around write query",
			query: "   CREATE (n:Task {title: 'Cut release'})   ",
			want:  cypherguard.Classes{Write: true, Create: true},
		},
		{
			name:  "write keyword only inside string literal is not a write",
			query: "MATCH (n:Task) WHERE n.description = 'we will CREATE and DELETE later' RETURN n",
			want:  cypherguard.Classes{},
		},
		{
			name:  "write keyword only inside line comment is not a write",
			query: "MATCH (n:Task) RETURN n // remember to CREATE an index",
			want:  cypherguard.Classes{},
		},
		{
			name:  "write keyword only inside block comment is not a write",
			query: "MATCH (n:Task) /* do not DELETE here */ RETURN n",
			want:  cypherguard.Classes{},
		},
		{
			name:  "write keyword only inside backtick identifier is not a write",
			query: "MATCH (n:`Pending CREATE`) RETURN n",
			want:  cypherguard.Classes{},
		},
		{
			name:  "comment marker inside literal does not hide a later write",
			query: "MATCH (n:Task) WHERE n.url = 'http://x/DELETE' CREATE (m:Task)",
			want:  cypherguard.Classes{Write: true, Create: true},
		},
		{
			name:  "keyword inside literal masked while outside keyword counts",
			query: "CREATE (n:Note {body: 'do not CREATE twice'})",
			want:  cypherguard.Classes{Write: true, Create: true},
		},
		{
			name:  "block comment before a real write does not swallow it",
			query: "/* read first */ CREATE (n:Task {title: 'Bootstrap schema'})",
			want:  cypherguard.Classes{Write: true, Create: true},
		},
		{
			name:  "set and delete together are write mutate and delete",
			query: "MATCH (n:Task {id: 5}) SET n.archived = true DELETE n",
			want:  cypherguard.Classes{Write: true, Mutate: true, Delete: true},
		},
		// ── Schema introspection ────────────────────────────────────────────
		// SHOW lists the registered schema and alters nothing, so it is
		// read-only and it is NOT Groadmap's DDL, which means schema-MUTATING
		// (SPEC/GRAPH.md § Schema Introspection). These cases pin the
		// recognition itself: without Introspect the same queries would still
		// read as read-only, but only because no discriminator matched, and the
		// assertion would prove nothing about the classification.
		{
			name:  "show indexes is schema introspection",
			query: "SHOW INDEXES",
			want:  cypherguard.Classes{Introspect: true},
		},
		{
			name:  "show constraints is schema introspection",
			query: "SHOW CONSTRAINTS",
			want:  cypherguard.Classes{Introspect: true},
		},
		{
			// The singular aliases the engine accepts. This case is the reason
			// the matcher spells the plural as INDEX(ES)? and not INDEXES?: the
			// latter parses as INDEXE plus an optional S and does not match the
			// singular at all.
			name:  "singular show index is schema introspection",
			query: "SHOW INDEX",
			want:  cypherguard.Classes{Introspect: true},
		},
		{
			name:  "singular show constraint is schema introspection",
			query: "SHOW CONSTRAINT",
			want:  cypherguard.Classes{Introspect: true},
		},
		{
			name:  "lowercase show indexes is schema introspection",
			query: "show indexes",
			want:  cypherguard.Classes{Introspect: true},
		},
		{
			name:  "mixed case show constraint is schema introspection",
			query: "sHoW cOnStRaInT",
			want:  cypherguard.Classes{Introspect: true},
		},
		{
			name:  "show indexes with yield where return tail is schema introspection",
			query: "SHOW INDEXES YIELD name, type WHERE type = 'RANGE' RETURN name",
			want:  cypherguard.Classes{Introspect: true},
		},
		{
			name:  "show constraints after a line comment is schema introspection",
			query: "// list the registered constraints\nSHOW CONSTRAINTS",
			want:  cypherguard.Classes{Introspect: true},
		},
		{
			name:  "show indexes after a block comment is schema introspection",
			query: "/* schema check */ SHOW INDEXES",
			want:  cypherguard.Classes{Introspect: true},
		},
		{
			name:  "show indexes after leading whitespace is schema introspection",
			query: "\n\t  SHOW INDEXES",
			want:  cypherguard.Classes{Introspect: true},
		},
		{
			// Anchoring at the start of the statement is what stops a property
			// named show from being read as an introspection command. The query
			// is an ordinary creating write and must classify as one.
			name:  "property named show is not schema introspection",
			query: "CREATE (n:Panel {show: 'indexes'})",
			want:  cypherguard.Classes{Write: true, Create: true},
		},
		{
			name:  "label named Show is not schema introspection",
			query: "MATCH (n:Show) RETURN n.title",
			want:  cypherguard.Classes{},
		},
		{
			name:  "show keyword inside a string literal is not schema introspection",
			query: "MATCH (n:Doc) WHERE n.body = 'run SHOW INDEXES first' RETURN n.key",
			want:  cypherguard.Classes{},
		},
		{
			// A SHOW family the pinned engine does not implement. It is not an
			// introspection command Groadmap admits, and it carries no writing
			// or DDL clause either, so it classifies as nothing and reaches the
			// engine, which rejects it as a syntax error — the division of labour
			// SPEC/GRAPH.md § Per-Subcommand Validation Rules note 3 requires.
			name:  "unimplemented show family is not schema introspection",
			query: "SHOW FUNCTIONS",
			want:  cypherguard.Classes{},
		},
		{
			// Word-boundary guard: INDEXER and INDEXE both start with INDEX but
			// are not the keyword, so neither is an introspection command.
			name:  "show indexer is not schema introspection",
			query: "SHOW INDEXER",
			want:  cypherguard.Classes{},
		},

		// ── FOREACH ─────────────────────────────────────────────────────────
		// FOREACH is a writing clause the engine gained after the version this
		// guard rail was written against. It has no discriminator of its own:
		// it is classified by the writing clauses its body contains, which is
		// sound only because a FOREACH body may contain nothing but those
		// clauses and a nested FOREACH (SPEC/GRAPH.md § Per-Subcommand
		// Validation Rules note 7). These cases pin that property so a change
		// to the classifier cannot open a write path through a read subcommand.
		{
			name:  "foreach with a set body is a write mutate",
			query: "MATCH (s:Spec) FOREACH (x IN [1] | SET s.reviewed = true)",
			want:  cypherguard.Classes{Write: true, Mutate: true},
		},
		{
			name:  "foreach with a remove body is a write mutate",
			query: "MATCH (s:Spec) FOREACH (x IN [1] | REMOVE s.draft)",
			want:  cypherguard.Classes{Write: true, Mutate: true},
		},
		{
			name:  "foreach with a create body is a write create",
			query: "FOREACH (name IN ['auth', 'crypto'] | CREATE (:Spec {key: name}))",
			want:  cypherguard.Classes{Write: true, Create: true},
		},
		{
			name:  "foreach with a merge body is a write create",
			query: "FOREACH (name IN ['auth', 'crypto'] | MERGE (:Spec {key: name}))",
			want:  cypherguard.Classes{Write: true, Create: true},
		},
		{
			name:  "foreach with a delete body is a write delete",
			query: "MATCH p = (a:Spec)-[:DEPENDS_ON*]->(b:Spec) FOREACH (r IN relationships(p) | DELETE r)",
			want:  cypherguard.Classes{Write: true, Delete: true},
		},
		{
			name:  "foreach with a detach delete body is a write delete",
			query: "MATCH p = (a:Spec)-[:DEPENDS_ON*]->(b:Spec) FOREACH (n IN nodes(p) | DETACH DELETE n)",
			want:  cypherguard.Classes{Write: true, Delete: true},
		},
		{
			name:  "nested foreach is classified by the innermost body",
			query: "MATCH (s:Spec) FOREACH (a IN [[1, 2]] | FOREACH (b IN a | SET s.depth = b))",
			want:  cypherguard.Classes{Write: true, Mutate: true},
		},
		{
			// The keyword FOREACH on its own carries no class: only the writing
			// clauses in its body do. A FOREACH mentioned inside a literal is
			// masked away like any other keyword and changes nothing.
			name:  "foreach keyword inside a string literal is not a write",
			query: "MATCH (m:Memory) WHERE m.body = 'use FOREACH to fan out' RETURN m.key",
			want:  cypherguard.Classes{},
		},
		{
			name:  "empty query classifies as nothing",
			query: "",
			want:  cypherguard.Classes{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cypherguard.Classify(tc.query)
			if got != tc.want {
				t.Errorf("Classify(%q) = %+v, want %+v", tc.query, got, tc.want)
			}
		})
	}
}

// TestIsReadOnly asserts the read-vs-write contract the read subcommands and the
// web data endpoint enforce: a query is read-only iff it contains neither a
// writing clause nor any DDL clause, evaluated on the literal-masked query.
func TestIsReadOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{name: "plain match is read-only", query: "MATCH (n:Task) RETURN n", want: true},
		{name: "match with where is read-only", query: "MATCH (n:Task) WHERE n.points > 3 RETURN n.title", want: true},
		{name: "optional match with order by is read-only", query: "MATCH (s:Sprint) OPTIONAL MATCH (s)-[:HAS_TASK]->(t) RETURN s, count(t) ORDER BY s.id", want: true},
		{name: "create is not read-only", query: "CREATE (n:Task {title: 'New work item'})", want: false},
		{name: "merge is not read-only", query: "MERGE (u:User {email: 'lead@example.com'})", want: false},
		{name: "set is not read-only", query: "MATCH (n:Task {id: 1}) SET n.status = 'in_progress'", want: false},
		{name: "remove is not read-only", query: "MATCH (n:Task {id: 1}) REMOVE n.blocked", want: false},
		{name: "delete is not read-only", query: "MATCH (n:Task {id: 1}) DELETE n", want: false},
		{name: "detach delete is not read-only", query: "MATCH (s:Sprint {id: 2}) DETACH DELETE s", want: false},
		{name: "create index DDL is not read-only", query: "CREATE INDEX task_idx FOR (n:Task) ON (n.status)", want: false},
		{name: "drop index DDL is not read-only", query: "DROP INDEX task_idx", want: false},
		{name: "create constraint DDL is not read-only", query: "CREATE CONSTRAINT c1 FOR (n:Task) REQUIRE n.title IS UNIQUE", want: false},
		{name: "legacy create constraint ON ASSERT DDL is not read-only", query: "CREATE CONSTRAINT unique_task_title ON (n:Task) ASSERT n.title IS UNIQUE", want: false},
		{name: "create constraint is not null DDL is not read-only", query: "CREATE CONSTRAINT title_exists FOR (n:Task) REQUIRE n.title IS NOT NULL", want: false},
		{name: "create constraint if not exists DDL is not read-only", query: "CREATE CONSTRAINT unique_task_title IF NOT EXISTS FOR (n:Task) REQUIRE n.title IS UNIQUE", want: false},
		{name: "lowercase legacy create constraint DDL is not read-only", query: "create constraint c1 on (n:Task) assert n.title is unique", want: false},
		{name: "multi-space legacy create constraint DDL is not read-only", query: "create   constraint c1 on (n:Task) assert n.title is unique", want: false},
		{name: "drop constraint DDL is not read-only", query: "DROP CONSTRAINT unique_task_title", want: false},
		{name: "multi-space create index DDL is not read-only", query: "CREATE   INDEX task_idx FOR (n:Task) ON (n.status)", want: false},
		{name: "case-insensitive delete is not read-only", query: "match (n:Task {id: 1}) delete n", want: false},
		{name: "leading whitespace before write is not read-only", query: "\n\t  CREATE (n:Task {title: 'Indented'})", want: false},
		{name: "write keyword inside literal stays read-only", query: "MATCH (n:Task) WHERE n.body = 'please CREATE a ticket' RETURN n", want: true},
		{name: "write keyword inside line comment stays read-only", query: "MATCH (n:Task) RETURN n // TODO DELETE stale rows", want: true},
		{name: "write keyword inside block comment stays read-only", query: "MATCH (n) /* later: SET n.flag */ RETURN n", want: true},
		{name: "DDL keyword inside literal stays read-only", query: "MATCH (n:Task) WHERE n.note = 'consider CREATE INDEX here' RETURN n", want: true},
		{name: "backtick label containing write keyword stays read-only", query: "MATCH (n:`Tasks To DELETE`) RETURN n", want: true},
		{name: "show indexes is read-only", query: "SHOW INDEXES", want: true},
		{name: "show constraints is read-only", query: "SHOW CONSTRAINTS", want: true},
		{name: "singular show index is read-only", query: "SHOW INDEX", want: true},
		{name: "singular show constraint is read-only", query: "SHOW CONSTRAINT", want: true},
		{name: "lowercase show indexes is read-only", query: "show indexes", want: true},
		{name: "show indexes with a projection tail is read-only", query: "SHOW INDEXES YIELD name, state WHERE state = 'ONLINE' RETURN name", want: true},
		{name: "show constraints after a comment is read-only", query: "/* schema check */ SHOW CONSTRAINTS", want: true},
		{name: "create index DDL is still not read-only despite the SHOW sibling", query: "CREATE INDEX spec_key_idx FOR (n:Spec) ON (n.key)", want: false},
		{name: "foreach with a set body is not read-only", query: "MATCH (s:Spec) FOREACH (x IN [1] | SET s.reviewed = true)", want: false},
		{name: "foreach with a create body is not read-only", query: "FOREACH (name IN ['auth'] | CREATE (:Spec {key: name}))", want: false},
		{name: "foreach with a detach delete body is not read-only", query: "MATCH p = (a)-[*]->(b) FOREACH (n IN nodes(p) | DETACH DELETE n)", want: false},
		{name: "nested foreach is not read-only", query: "MATCH (s:Spec) FOREACH (a IN [[1]] | FOREACH (b IN a | SET s.depth = b))", want: false},
		{name: "property named show does not make a write read-only", query: "CREATE (n:Panel {show: 'indexes'})", want: false},
		{name: "show keyword inside a literal stays an ordinary read", query: "MATCH (n:Doc) WHERE n.body = 'run SHOW INDEXES first' RETURN n.key", want: true},
		{name: "empty query is read-only", query: "", want: true},
		{name: "whitespace-only query is read-only", query: "   \n\t ", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cypherguard.IsReadOnly(tc.query); got != tc.want {
				t.Errorf("IsReadOnly(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

// spoofedDDLQueries are DDL statements whose CREATE/DROP INDEX/CONSTRAINT
// keyword pair carries a NON-ASCII letter that Unicode uppercasing maps onto the
// ASCII letter the keyword needs — U+0131 LATIN SMALL LETTER DOTLESS I
// uppercases to 'I', and U+017F LATIN SMALL LETTER LONG S uppercases to 'S'.
//
// The engine acts on exactly that transformation: cypher/ir.IsDDL falls back to
// strings.HasPrefix(strings.ToUpper(stmt), "CREATE INDEX") as soon as a
// non-ASCII byte appears in the prefix window, and cypher/ir.ParseDDL dispatches
// on strings.ToUpper(stmt) too. Go's (?i) regexp flag applies Unicode simple
// case FOLDING instead, whose 'I'/'i' orbit does not contain U+0131, so a regexp
// alone does not see these as DDL.
//
// Before the fix this divergence was a fail-OPEN guard-rail bypass: the guard
// rail reported "ordinary read", `rmp graph query` and the read-only web graph
// data endpoint admitted the statement, and the engine executed it through its
// DDL executor (proven end to end: `rmp graph query -q "CREATE <U+0131>NDEX …"`
// exited 0, and the DROP form reached exec.DropIndex).
var spoofedDDLQueries = []struct {
	name  string
	query string
}{
	{name: "create index with U+0131 in INDEX", query: "CREATE ıNDEX evil FOR (n:Spec) ON (n.key)"},
	{name: "create index if not exists with U+0131", query: "CREATE ıNDEX IF NOT EXISTS evil FOR (n:Spec) ON (n.key)"},
	{name: "drop index with U+0131 in INDEX", query: "DROP ıNDEX evil"},
	{name: "lowercase drop index with U+0131", query: "drop ındex evil"},
	{name: "create constraint with U+0131 in CONSTRAINT", query: "CREATE CONSTRAıNT c1 FOR (n:Spec) REQUIRE n.key IS UNIQUE"},
	{name: "drop constraint with U+0131 in CONSTRAINT", query: "DROP CONSTRAıNT c1"},
	{name: "create constraint with U+017F in CONSTRAINT", query: "CREATE CONſTRAINT c1 FOR (n:Spec) REQUIRE n.key IS UNIQUE"},
	{name: "spoofed keyword with extra whitespace", query: "CREATE   ıNDEX evil FOR (n:Spec) ON (n.key)"},
	{name: "spoofed keyword after a leading comment", query: "// plan\nCREATE ıNDEX evil FOR (n:Spec) ON (n.key)"},
}

// TestClassifySpoofedDDLKeywords pins that a DDL keyword spelled with a
// non-ASCII letter that uppercases onto ASCII is classified as DDL, and
// therefore refused as not read-only. Regression test for the guard-rail bypass
// described on spoofedDDLQueries.
func TestClassifySpoofedDDLKeywords(t *testing.T) {
	t.Parallel()

	for _, tc := range spoofedDDLQueries {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cypherguard.Classify(tc.query); !got.DDL {
				t.Errorf("Classify(%q).DDL = false, want true (the engine routes this to its DDL executor)", tc.query)
			}
			if cypherguard.IsReadOnly(tc.query) {
				t.Errorf("IsReadOnly(%q) = true, want false: a schema-mutating DDL statement is never read-only", tc.query)
			}
		})
	}
}

// TestClassifyKeepsUnicodeIdentifiersReadOnly is the other half of the fix: the
// uppercased second pass must not turn ordinary reads that merely CONTAIN such
// letters into rejections. Only a spoofed KEYWORD may be caught, never a label,
// property, or literal that happens to carry the same code point.
func TestClassifyKeepsUnicodeIdentifiersReadOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
	}{
		{name: "dotless i inside a property name", query: "MATCH (n:Task) WHERE n.basık = 'x' RETURN n"},
		{name: "dotless i inside a label", query: "MATCH (n:Yazılım) RETURN n"},
		{name: "spoofed keyword inside a string literal", query: "MATCH (n:Doc) WHERE n.body = 'CREATE ıNDEX evil' RETURN n"},
		{name: "spoofed keyword inside a line comment", query: "MATCH (n) RETURN n // CREATE ıNDEX evil"},
		{name: "spoofed keyword inside a backtick identifier", query: "MATCH (n:`CREATE ıNDEX`) RETURN n"},
		{name: "long s inside a label", query: "MATCH (n:Meſsage) RETURN n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cypherguard.Classify(tc.query); got.DDL {
				t.Errorf("Classify(%q).DDL = true, want false: no DDL keyword is present", tc.query)
			}
			if !cypherguard.IsReadOnly(tc.query) {
				t.Errorf("IsReadOnly(%q) = false, want true: an ordinary read must stay admissible", tc.query)
			}
		})
	}
}

// TestGuardRailCoversEngineDDLSurface binds the guard rail to the engine's REAL
// dispatch predicate rather than to its documentation: for every statement the
// engine would route to its DDL executor (cypher/ir.IsDDL), the guard rail MUST
// classify DDL.
//
// The property is stated as an implication, so it can only fail in the
// dangerous direction. A future engine release that stops treating one of these
// forms as DDL leaves the test green (the guard rail is allowed to be stricter
// than the engine); an engine that starts treating a NEW spelling as DDL while
// the guard rail still calls it an ordinary read fails the test — which is
// exactly the fail-open divergence this package exists to prevent.
//
// The second assertion pins WHY the guard rail cannot delegate to the engine's
// own writing-clause classifier: cypher.QueryHasWritingClause returns false for
// everything IsDDL accepts, so a DDL statement carries no write signal at all
// and the DDL discriminator is the only thing standing between it and execution.
func TestGuardRailCoversEngineDDLSurface(t *testing.T) {
	t.Parallel()

	ascii := []string{
		// ASCII forms, as the engine's prefix scan sees them.
		"CREATE INDEX i FOR (n:Spec) ON (n.key)",
		"create index i FOR (n:Spec) ON (n.key)",
		"DROP INDEX i",
		"drop index i",
		"CREATE CONSTRAINT c FOR (n:Spec) REQUIRE n.key IS UNIQUE",
		"DROP CONSTRAINT c",
		"/* lead */ CREATE INDEX i FOR (n:Spec) ON (n.key)",
		"// lead\nDROP INDEX i",
	}
	corpus := make([]string, 0, len(ascii)+len(spoofedDDLQueries))
	corpus = append(corpus, ascii...)
	for _, tc := range spoofedDDLQueries {
		corpus = append(corpus, tc.query)
	}

	for _, query := range corpus {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			// The implication, asserted without branching out of the test: a
			// statement the engine dispatches as DDL must be classified DDL. A
			// statement it does NOT dispatch may still be classified DDL — the
			// guard rail is deliberately stricter (it tolerates any whitespace
			// between the two keywords, where the engine's prefix scan wants
			// exactly one space).
			if ir.IsDDL(query) && !cypherguard.Classify(query).DDL {
				t.Errorf("engine dispatches %q to its DDL executor but Classify(...).DDL = false", query)
			}
			// Every entry of the corpus is DDL-shaped, so none of them may be
			// admitted as read-only whether the engine dispatches it or not.
			if cypherguard.IsReadOnly(query) {
				t.Errorf("IsReadOnly(%q) = true, want false: the statement is schema-mutating DDL", query)
			}
			// The engine's writing-clause classifier exempts everything IsDDL
			// accepts, so it carries no write signal for these statements: the
			// DDL discriminator is the only thing between them and execution.
			if ir.IsDDL(query) && cypher.QueryHasWritingClause(query) {
				t.Errorf("cypher.QueryHasWritingClause(%q) = true; the DDL exemption this guard rail compensates for has changed — re-audit Classify", query)
			}
		})
	}
}
