package cypherguard_test

import (
	"strings"
	"testing"

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
