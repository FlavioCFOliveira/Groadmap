package web

import (
	"strings"
	"testing"
)

// maskSpaces returns a run of n spaces, used to build the expected masked output
// of a literal/comment span whose interior is neutralized to spaces.
func maskSpaces(n int) string { return strings.Repeat(" ", n) }

// TestMaskLiterals verifies the exact masked output of maskLiterals for every
// span kind it recognizes. Each case asserts the full string so that delimiter
// preservation, length preservation, and the precise neutralized span are all
// checked at once (SPEC/GRAPH.md § Literal-Aware Normalization, which
// SPEC/WEB.md § Graph Data Endpoint cites for this endpoint's two suppression
// checks).
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
			want:  "n.name = '" + maskSpaces(5) + "'",
		},
		{
			name:  "double quoted interior masked delimiters kept",
			query: `RETURN "hello world"`,
			want:  `RETURN "` + maskSpaces(11) + `"`,
		},
		{
			name:  "backtick identifier interior masked delimiters kept",
			query: "MATCH (n:`Hot Label`) RETURN n",
			want:  "MATCH (n:`" + maskSpaces(9) + "`) RETURN n",
		},
		{
			name:  "line comment marker and body masked newline preserved",
			query: "RETURN 1 // note\nRETURN 2",
			want:  "RETURN 1 " + maskSpaces(7) + "\nRETURN 2",
		},
		{
			name:  "block comment markers and body masked",
			query: "y /* z */ w",
			want:  "y" + maskSpaces(9) + "w",
		},
		{
			name:  "escaped single quote does not close literal",
			query: "'a\\'b'",
			want:  "'" + maskSpaces(4) + "'",
		},
		{
			name:  "escaped double quote does not close literal",
			query: `"a\"b"`,
			want:  `"` + maskSpaces(4) + `"`,
		},
		{
			name:  "adjacent single quoted literals",
			query: "'ab''cd'",
			want:  "'" + maskSpaces(2) + "''" + maskSpaces(2) + "'",
		},
		{
			name:  "double quote inside single quoted literal is interior",
			query: "x = 'she said \"hi\"'",
			want:  "x = '" + maskSpaces(13) + "'",
		},
		{
			name:  "comment marker inside string literal is interior",
			query: "url = 'http://example.com'",
			want:  "url = '" + maskSpaces(18) + "'",
		},
		{
			name:  "quote inside line comment does not open literal",
			query: "RETURN n // it's fine",
			want:  "RETURN n " + maskSpaces(12),
		},
		{
			name:  "clause keyword inside single quoted literal is masked",
			query: "WHERE x = 'CREATE'",
			want:  "WHERE x = '" + maskSpaces(6) + "'",
		},
		{
			name:  "unterminated single quoted literal masked to end",
			query: "n.name = 'unterminated",
			want:  "n.name = '" + maskSpaces(12),
		},
		{
			name:  "unterminated block comment masked to end",
			query: "RETURN 1 /* open",
			want:  "RETURN 1 " + maskSpaces(7),
		},
		{
			name:  "unterminated backtick identifier masked to end",
			query: "MATCH (n:`Open",
			want:  "MATCH (n:`" + maskSpaces(4),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := maskLiterals(tc.query)
			if got != tc.want {
				t.Errorf("maskLiterals(%q) = %q, want %q", tc.query, got, tc.want)
			}
			if len(got) != len(tc.query) {
				t.Errorf("maskLiterals(%q) length = %d, want %d (length must be preserved)",
					tc.query, len(got), len(tc.query))
			}
		})
	}
}

// TestMaskLiteralsLengthInvariant asserts the universal contract that masking
// never changes the byte length of a query, regardless of the span kinds
// present. Byte positions of every unmasked token must stay put so that the
// suppression matchers see clause keywords at their original offsets.
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
		if got := len(maskLiterals(q)); got != len(q) {
			t.Errorf("maskLiterals(%q) length = %d, want %d", q, got, len(q))
		}
	}
}
