package cypherguard_test

// Unit half of the regression for rmp task #275: `rmp graph query --query
// "SHOW  INDEXES"` (two spaces) passed the guard rail and died in the engine's
// parser under a diagnostic that lists every clause keyword EXCEPT SHOW, so it
// read as though schema introspection were unsupported — while the identical
// statement with one space returned its result set.
//
// The rule the guard rail now enforces is the engine's own: GoGraph routes a
// statement to its introspection parser by testing it against the literal
// prefixes "SHOW INDEX" and "SHOW CONSTRAINT", each carrying exactly one space,
// after trimming leading whitespace and leading comments. Anything else between
// the two keywords is refused HERE, with the guard rail's own message, before
// the engine sees it (SPEC/GRAPH.md § Keyword Spacing in a Schema-Introspection
// Command, and Acceptance Criterion 39).
//
// This file pins the shared decision and the shared message. The surfaces that
// apply them are pinned where they live: internal/commands
// (graph_introspect_spacing_test.go) for `graph query` and `graph search`, and
// internal/web (graph_introspect_spacing_test.go) for the read-only graph data
// endpoint.

import (
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/cypherguard"
)

// misspacedSeparators are the separators a schema-introspection command must be
// refused for. Exactly one space is the only accepted spelling, so every one of
// these is a rejection, and none of them is a rejection today by accident: each
// is a spelling the engine's prefix test misses and its general grammar then
// refuses.
var misspacedSeparators = []struct {
	name string
	sep  string
}{
	{"two spaces", "  "},
	{"three spaces", "   "},
	{"a tab", "\t"},
	{"a line break", "\n"},
	{"a carriage return and line break", "\r\n"},
	{"a space and a tab", " \t"},
}

// introspectKeywords are the four target keywords the class covers. The two
// plurals are spelled differently in the matcher on purpose (INDEX(ES)? against
// CONSTRAINTS?), so each singular and each plural has to be exercised: a matcher
// written INDEXES? would silently stop covering the singular SHOW INDEX.
var introspectKeywords = []string{"INDEXES", "INDEX", "CONSTRAINTS", "CONSTRAINT"}

// TestIntrospectSpacingRejection_RefusesEverySeparatorButOneSpace is the core
// claim of Acceptance Criterion 39 at the guard-rail level: one space is
// accepted, every other separator is refused, for all four target keywords and
// in any keyword case.
//
// It fails if reIntrospect is widened back to arbitrary whitespace: every
// misspaced case would then classify as a well-formed introspection command and
// report no rejection at all.
func TestIntrospectSpacingRejection_RefusesEverySeparatorButOneSpace(t *testing.T) {
	t.Parallel()

	for _, keyword := range introspectKeywords {
		t.Run(keyword, func(t *testing.T) {
			t.Parallel()

			// Exactly one space: accepted, and recognised as the class rather
			// than admitted because nothing matched.
			accepted := "SHOW " + keyword
			if reason, misspaced := cypherguard.IntrospectSpacingRejection(accepted); misspaced {
				t.Errorf("IntrospectSpacingRejection(%q) refused the accepted spelling: %s", accepted, reason)
			}
			if got := cypherguard.Classify(accepted); !got.Introspect || got.IntrospectMisspaced {
				t.Errorf("Classify(%q) = %+v, want Introspect true and IntrospectMisspaced false", accepted, got)
			}

			for _, sep := range misspacedSeparators {
				query := "SHOW" + sep.sep + keyword
				t.Run(sep.name, func(t *testing.T) {
					t.Parallel()

					reason, misspaced := cypherguard.IntrospectSpacingRejection(query)
					if !misspaced {
						t.Fatalf("IntrospectSpacingRejection(%q) admitted a spelling the engine refuses; "+
							"the query would reach the parser and fail there with the wrong diagnostic", query)
					}
					if got := cypherguard.Classify(query); got.Introspect || !got.IntrospectMisspaced {
						t.Errorf("Classify(%q) = %+v, want Introspect false and IntrospectMisspaced true", query, got)
					}

					// The message must name the cause, and must name the
					// accepted spelling of the command the user meant.
					for _, want := range []string{"schema-introspection command", "exactly one space", "keyword spacing", accepted} {
						if !strings.Contains(reason, want) {
							t.Errorf("IntrospectSpacingRejection(%q) reason = %q, want it to contain %q", query, reason, want)
						}
					}
					// It must NOT describe the query as a write or as not
					// read-only: a SHOW statement reads the schema and writes
					// nothing whatever its spacing.
					if strings.Contains(reason, "not read-only") {
						t.Errorf("IntrospectSpacingRejection(%q) reason = %q, want it never to call the query not read-only", query, reason)
					}
				})
			}
		})
	}
}

// TestIntrospectSpacingRejection_KeywordCaseIsIrrelevant asserts the rule folds
// case exactly as the engine's prefix test does, so a lowercase or mixed-case
// statement is refused for its spacing and not admitted by a case accident.
func TestIntrospectSpacingRejection_KeywordCaseIsIrrelevant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		query    string
		accepted string
	}{
		{"show  indexes", "SHOW INDEXES"},
		{"Show\tIndex", "SHOW INDEX"},
		{"sHoW\ncOnStRaInTs", "SHOW CONSTRAINTS"},
		{"SHOW  constraint", "SHOW CONSTRAINT"},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()
			reason, misspaced := cypherguard.IntrospectSpacingRejection(tc.query)
			if !misspaced {
				t.Fatalf("IntrospectSpacingRejection(%q) admitted a spelling the engine refuses", tc.query)
			}
			// The accepted spelling is built from the canonical keywords, not
			// echoed from the query, so the message is the same whatever case
			// the user typed and carries no byte of the input back.
			if !strings.Contains(reason, tc.accepted) {
				t.Errorf("IntrospectSpacingRejection(%q) reason = %q, want it to name the accepted spelling %q", tc.query, reason, tc.accepted)
			}
		})
	}
}

// TestIntrospectSpacingRejection_OnlyTheKeywordSeparator asserts the rule
// governs the separator between the two keywords and nothing else about the
// statement's spacing: whitespace and comments BEFORE the statement are
// accepted, and so is any amount of whitespace AFTER the target keyword,
// including before a YIELD / WHERE / RETURN projection tail.
//
// The engine trims leading whitespace and leading comments before its prefix
// test, and its SHOW parser is itself whitespace-tolerant once it is reached,
// which is exactly why only the keyword separator is strict. Refusing more than
// that separator would be Groadmap inventing a grammar of its own.
func TestIntrospectSpacingRejection_OnlyTheKeywordSeparator(t *testing.T) {
	t.Parallel()

	accepted := []struct {
		name  string
		query string
	}{
		{"leading spaces", "   SHOW INDEXES"},
		{"leading line break and tab", "\n\t  SHOW INDEXES"},
		{"leading line comment", "// which indexes exist?\nSHOW INDEXES"},
		{"leading block comment", "/* schema check */ SHOW CONSTRAINTS"},
		{"two leading comments", "// first\n/* second */ SHOW INDEX"},
		{"extra whitespace before a YIELD tail", "SHOW INDEXES   YIELD name"},
		{"a line break before a WHERE tail", "SHOW CONSTRAINTS\nWHERE type = 'UNIQUE'"},
		{"a full projection tail", "SHOW INDEXES YIELD name, type WHERE type = 'RANGE' RETURN name"},
	}

	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if reason, misspaced := cypherguard.IntrospectSpacingRejection(tc.query); misspaced {
				t.Errorf("IntrospectSpacingRejection(%q) refused a spelling the engine accepts: %s", tc.query, reason)
			}
			if !cypherguard.Classify(tc.query).Introspect {
				t.Errorf("Classify(%q).Introspect = false, want true", tc.query)
			}
		})
	}
}

// TestIntrospectSpacingRejection_LeavesEverythingElseToTheEngine asserts the
// rule refuses ONLY statements that are schema-introspection commands under some
// spacing. A SHOW family the engine does not implement, a near miss on the
// keyword, and an ordinary query that merely mentions a misspaced SHOW inside a
// literal must all keep reaching the engine, which names the real problem for
// them — the division of labour SPEC/GRAPH.md § Per-Subcommand Validation Rules
// note 3 requires.
func TestIntrospectSpacingRejection_LeavesEverythingElseToTheEngine(t *testing.T) {
	t.Parallel()

	untouched := []struct {
		name  string
		query string
	}{
		{"a near miss on the keyword", "SHOW  INDEXER"},
		{"a near miss on the plural", "SHOW  CONSTRAINTX"},
		{"an unimplemented SHOW family", "SHOW  DATABASES"},
		{"SHOW with nothing after it", "SHOW"},
		{"a misspaced SHOW inside a string literal", "MATCH (n:Doc) WHERE n.body = 'SHOW  INDEXES fails' RETURN n.key"},
		{"a misspaced SHOW inside a line comment", "MATCH (n) RETURN n // try SHOW  INDEXES"},
		{"a misspaced SHOW inside a backtick identifier", "MATCH (n:`SHOW  INDEXES`) RETURN n"},
		{"a property named show", "MATCH (n:Panel) WHERE n.show = 'indexes' RETURN n"},
		{"an ordinary read", "MATCH (s:Spec) RETURN s.key"},
		{"the empty query", ""},
	}

	for _, tc := range untouched {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if reason, misspaced := cypherguard.IntrospectSpacingRejection(tc.query); misspaced {
				t.Errorf("IntrospectSpacingRejection(%q) refused a query that is not a schema-introspection "+
					"command under any spacing: %s", tc.query, reason)
			}
		})
	}
}

// TestIntrospectSpacingRejection_SpoofedKeywordIsRefusedToo asserts the spacing
// rule sees the same keywords the engine sees. The engine falls back to
// strings.ToUpper the moment a non-ASCII byte appears inside the prefix window,
// and Unicode uppercasing maps U+0131 (dotless i) onto 'I' and U+017F (long s)
// onto 'S'. Go's (?i) is case FOLDING and does not, so the masked text alone
// would miss these — the same fail-open shape upperFoldedKeywords exists to
// close for the DDL class.
//
// A spoofed keyword with a bad separator must be refused with the canonical
// spelling named, and a spoofed keyword with ONE space must still be admitted,
// because the engine parses it.
func TestIntrospectSpacingRejection_SpoofedKeywordIsRefusedToo(t *testing.T) {
	t.Parallel()

	t.Run("spoofed keyword with two spaces is refused", func(t *testing.T) {
		t.Parallel()
		const query = "SHOW  ıNDEX" // "SHOW  ıNDEX"
		reason, misspaced := cypherguard.IntrospectSpacingRejection(query)
		if !misspaced {
			t.Fatalf("IntrospectSpacingRejection(%q) admitted a spelling the engine refuses", query)
		}
		if !strings.Contains(reason, "SHOW INDEX") {
			t.Errorf("reason = %q, want it to name the accepted spelling %q", reason, "SHOW INDEX")
		}
		// The message is built from the canonical keywords, so the spoofed code
		// point is never echoed back to the caller.
		if strings.ContainsRune(reason, 'ı') {
			t.Errorf("reason = %q, want it to carry no byte of the query itself", reason)
		}
	})

	t.Run("spoofed keyword with one space is still admitted", func(t *testing.T) {
		t.Parallel()
		const query = "SHOW ıNDEX" // "SHOW ıNDEX": the engine parses this
		if reason, misspaced := cypherguard.IntrospectSpacingRejection(query); misspaced {
			t.Errorf("IntrospectSpacingRejection(%q) refused a spelling the engine accepts: %s", query, reason)
		}
	})
}

// TestIntrospectSpacingRejection_AgreesWithClassify asserts the two ways the
// package answers the same question can never disagree: the boolean
// IntrospectSpacingRejection returns is exactly Classes.IntrospectMisspaced, and
// a statement is never both Introspect and IntrospectMisspaced.
//
// They share one implementation today, and this pins that they must keep sharing
// one: a caller that refuses on the boolean and prints the message would
// otherwise be able to print an empty reason, or to refuse nothing while the
// classification says the statement is misspaced.
func TestIntrospectSpacingRejection_AgreesWithClassify(t *testing.T) {
	t.Parallel()

	queries := []string{
		"SHOW INDEXES", "SHOW  INDEXES", "SHOW\tINDEX", "SHOW\nCONSTRAINTS",
		"show  constraint", "SHOW  INDEXER", "SHOW DATABASES", "MATCH (n) RETURN n",
		"CREATE (n:Spec {key:'x'})", "/* c */ SHOW CONSTRAINTS", "",
	}

	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			t.Parallel()
			reason, misspaced := cypherguard.IntrospectSpacingRejection(query)
			c := cypherguard.Classify(query)

			if misspaced != c.IntrospectMisspaced {
				t.Errorf("IntrospectSpacingRejection(%q) misspaced = %v, but Classify reports IntrospectMisspaced = %v",
					query, misspaced, c.IntrospectMisspaced)
			}
			if misspaced && reason == "" {
				t.Errorf("IntrospectSpacingRejection(%q) refused the query with an empty reason", query)
			}
			if !misspaced && reason != "" {
				t.Errorf("IntrospectSpacingRejection(%q) returned a reason %q while admitting the query", query, reason)
			}
			if c.Introspect && c.IntrospectMisspaced {
				t.Errorf("Classify(%q) = %+v: a statement cannot be both well spelled and misspaced", query, c)
			}
		})
	}
}
