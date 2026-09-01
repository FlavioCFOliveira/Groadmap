package cypherguard

// Regression tests for rmp task #298: a Cypher property value was subject to
// neither of the two free-text content rules, so a CLI graph write
// accepted control characters verbatim and REPLACED invalid
// UTF-8 with U+FFFD while reporting success (SPEC/GRAPH.md § Property Value
// Content Rules).
//
// Two of the cases below are the ones that decide whether the check was put in
// the right place, and neither can be satisfied by looking at the query text:
//
//   - TestRefusedPropertyValue_EscapeEncodedControlCharacters supplies a query
//     whose text is PURE ASCII and whose written value carries a real control
//     character, because Cypher decodes \b, \f and \uXXXX inside a string
//     literal. A scan of the query string passes it.
//   - TestRefusedPropertyValue_EveryMalformedUTF8Shape supplies bytes that no
//     AST node ever sees, because the engine's lexer replaces them with U+FFFD
//     before the grammar runs. A scan of the parsed value passes them.
//
// Together they are why the rule is enforced against two different objects, and
// why neither object alone is enough.

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/FlavioCFOliveira/GoGraph/cypher/ast"
	"github.com/FlavioCFOliveira/GoGraph/cypher/parser"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// escapeCypher renders value as the interior of a single-quoted Cypher string
// literal. Only the backslash and the closing quote need escaping; every other
// byte, VALID OR NOT, is passed through untouched, which is the whole point when
// the value under test is malformed UTF-8.
func escapeCypher(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(escaped, "'", `\'`)
}

// refusedOnTheWritePath applies the two rules in the order the WRITE path
// applies them: the encoding rule on the raw bytes, then the control-character
// rule on the values the query writes.
//
// It is a convenience for the cases below, which are about the RULES. The order
// itself is not owned here — it lives at the call site in runGraphWrite, and
// TestGraphWrite_TheEncodingRuleIsAppliedBeforeTheControlCharacterRule in
// package commands is what pins it there, against the real handler. A helper
// that both defined the order and tested it would prove nothing about the
// binary.
func refusedOnTheWritePath(query string) (PropertyValueRefusal, bool) {
	if r, refused := RefusedQueryEncoding(query); refused {
		return r, true
	}
	return RefusedWrittenPropertyValue(query)
}

// TestRefusedPropertyValue_EveryMalformedUTF8Shape is acceptance criterion 3's
// encoding half, over the WHOLE corpus the UTF-8 rule is defined by rather than
// over shapes invented here. rmp task 180 built that corpus from the five items
// SPEC/MODELS.md § Free-Text UTF-8 Encoding Constraint enumerates, and the graph
// is now bound by the same enumeration, so it is read from the same place.
//
// Each shape is placed in a written property value AND, in a second case, the
// same query is checked to prove the refusal is not an artefact of the position.
func TestRefusedPropertyValue_EveryMalformedUTF8Shape(t *testing.T) {
	corpus := testenv.MalformedUTF8Corpus()
	if len(corpus) < 4 {
		t.Fatalf("the malformed-UTF-8 corpus holds only %d shapes; it is not the corpus the rule is defined by", len(corpus))
	}

	for _, c := range corpus {
		t.Run(c.Name, func(t *testing.T) {
			// The corpus entry must be malformed, or the case proves nothing
			// about the encoding rule. Asserting it here keeps this test honest
			// if the corpus is ever edited.
			if utf8.ValidString(c.Value) {
				t.Fatalf("%q is valid UTF-8, so it cannot exercise the encoding rule.\n  %s", c.Value, c.Why)
			}

			query := "CREATE (n:Memory {key: 'sprint-38', body: '" + escapeCypher(c.Value) + "'})"

			// The parser does NOT reject it: this is what makes the defect
			// silent, and what rules out "the engine will catch it" as an
			// answer.
			if _, err := parser.Parse(query); err != nil {
				t.Fatalf("the engine's parser refused the query, so the silent-corruption path is not what this case exercises: %v", err)
			}

			r, refused := refusedOnTheWritePath(query)
			if !refused {
				t.Fatalf("RefusedPropertyValue admitted a query whose written value is malformed UTF-8.\n  %s", c.Why)
			}
			if r.Violation != utils.FreeTextInvalidUTF8 {
				t.Errorf("violation = %v, want FreeTextInvalidUTF8", r.Violation)
			}
			if r.Property != "body" {
				t.Errorf("property = %q, want %q: the refusal must name the value it refuses", r.Property, "body")
			}
			if !r.Attributed() {
				t.Error("Attributed() = false for a refusal that names a property")
			}
			if r.Offset < 0 || r.Offset >= len(query) {
				t.Errorf("offset %d is outside the query (len %d)", r.Offset, len(query))
			}
			if query[r.Offset] != r.Byte {
				t.Errorf("the refusal reports byte %#02x at offset %d, but the query holds %#02x there",
					r.Byte, r.Offset, query[r.Offset])
			}
			if utf8.ValidString(query[r.Offset:]) {
				t.Errorf("offset %d does not begin a malformed sequence; the reported position is wrong", r.Offset)
			}
		})
	}
}

// TestRefusedPropertyValue_MalformedUTF8IsInvisibleToTheParsedValue is the
// measurement that decides WHERE the encoding half can be enforced, asserted so
// that it cannot quietly stop being true.
//
// The engine's lexer decodes the query to runes before the grammar runs and
// replaces every byte that decodes to no rune with U+FFFD. A check that read the
// parsed value would therefore find well-formed UTF-8 whatever was supplied, and
// would pass every shape above. If this test ever fails, the encoding half may
// be moved onto the AST — and until it does, it may not.
func TestRefusedPropertyValue_MalformedUTF8IsInvisibleToTheParsedValue(t *testing.T) {
	for _, c := range testenv.MalformedUTF8Corpus() {
		t.Run(c.Name, func(t *testing.T) {
			query := "CREATE (n:Memory {key: 'sprint-38', body: '" + escapeCypher(c.Value) + "'})"

			seen := false
			walkWrittenPropertyValues(query, func(key string, lit *ast.StringLiteral) bool {
				if key != "body" {
					return true
				}
				seen = true
				if !utf8.ValidString(lit.Value) {
					t.Errorf("the parsed value is still malformed UTF-8; the encoding half could be decided on the AST after all, and this rule's placement should be revisited")
				}
				if !strings.ContainsRune(lit.Value, utf8.RuneError) {
					t.Errorf("the parsed value carries no U+FFFD, so the replacement this rule reasons about did not happen: %q", lit.Value)
				}
				return true
			})
			if !seen {
				t.Fatal("the walk never reached the body property; the test is not observing what it claims to")
			}
		})
	}
}

// TestRefusedPropertyValue_EscapeEncodedControlCharacters is the crux of the
// task: the query text is pure ASCII and the value it writes is not.
//
// Cypher decodes \b, \f and \uXXXX inside a string literal (openCypher 9
// "Note on string literals"), so a control character reaches the store through a query in which no
// control character appears. Each case asserts the query text really is clean,
// so the test cannot pass by accident on a query that carries the character
// literally.
func TestRefusedPropertyValue_EscapeEncodedControlCharacters(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		property  string
		codePoint string
	}{
		{
			name:      "ESC introducing an ANSI colour sequence",
			query:     `CREATE (n:Memory {key: 'sprint-38', body: 'deploy \u001b[31mFAILED\u001b[0m'})`,
			property:  "body",
			codePoint: "U+001B",
		},
		{
			name:      "RIGHT-TO-LEFT OVERRIDE, the Trojan Source shape",
			query:     `MATCH (n:Memory {key:'sprint-38'}) SET n.summary = 'invoice\u202egpj.exe'`,
			property:  "summary",
			codePoint: "U+202E",
		},
		{
			name:      "backspace, spelled with the two-character escape",
			query:     `MATCH (n:Memory {key:'sprint-38'}) SET n += {note: 'approved\b\b\b\b\b\b\b\brejected'}`,
			property:  "note",
			codePoint: "U+0008",
		},
		{
			name:      "form feed, which is also whitespace and would survive a trim",
			query:     `CREATE (n:Memory {key: 'sprint-38'}) SET n.detail = 'page one\fpage two'`,
			property:  "detail",
			codePoint: "U+000C",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The whole point: the query the operator typed is clean text.
			if v := utils.InspectFreeText(tc.query); v != utils.FreeTextValid {
				t.Fatalf("the query TEXT itself breaks a rule (%v), so this case does not show that the check reads the VALUE", v)
			}

			r, refused := refusedOnTheWritePath(tc.query)
			if !refused {
				t.Fatal("RefusedPropertyValue admitted a query whose written value carries a control character; a check on the query text would do exactly this")
			}
			if r.Violation != utils.FreeTextControlChars {
				t.Errorf("violation = %v, want FreeTextControlChars", r.Violation)
			}
			if r.Property != tc.property {
				t.Errorf("property = %q, want %q", r.Property, tc.property)
			}
			if r.CodePoint != tc.codePoint {
				t.Errorf("code point = %q, want %q", r.CodePoint, tc.codePoint)
			}
		})
	}
}

// TestRefusedPropertyValue_RawControlCharacterInTheQuery is the other half of
// the control-character rule: the character written into the query directly,
// without an escape, must be refused just the same.
func TestRefusedPropertyValue_RawControlCharacterInTheQuery(t *testing.T) {
	query := "CREATE (n:Memory {key: 'sprint-38', body: 'deploy \x1b[31mFAILED'})"

	r, refused := refusedOnTheWritePath(query)
	if !refused {
		t.Fatal("a raw ESC byte in a written value was admitted")
	}
	if r.Violation != utils.FreeTextControlChars || r.Property != "body" || r.CodePoint != "U+001B" {
		t.Errorf("refusal = %+v, want the control-character rule naming body and U+001B", r)
	}
}

// TestRefusedPropertyValue_EveryWritePosition holds the reach of the walk: each
// shape below persists a property value, and each must be seen.
//
// It is a list of POSITIONS, not of values, so the single offending value is the
// same throughout and any case that stops being reported is a position the walk
// no longer covers.
func TestRefusedPropertyValue_EveryWritePosition(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		property string
	}{
		{
			name:     "CREATE inline node property map",
			query:    `CREATE (n:Memory {key: 'sprint-38', body: 'a\u0007b'})`,
			property: "body",
		},
		{
			name:     "CREATE inline relationship property map",
			query:    `CREATE (a:Spec {key:'graph'})-[:SEE_ALSO {note: 'a\u0007b'}]->(b:Spec {key:'web'})`,
			property: "note",
		},
		{
			name:     "MERGE inline property map",
			query:    `MERGE (n:Memory {key: 'a\u0007b'})`,
			property: "key",
		},
		{
			name:     "MERGE ON CREATE SET",
			query:    `MERGE (n:Memory {key:'sprint-38'}) ON CREATE SET n.body = 'a\u0007b'`,
			property: "body",
		},
		{
			name:     "MERGE ON MATCH SET",
			query:    `MERGE (n:Memory {key:'sprint-38'}) ON MATCH SET n.body = 'a\u0007b'`,
			property: "body",
		},
		{
			name:     "SET property assignment",
			query:    `MATCH (n:Memory {key:'sprint-38'}) SET n.body = 'a\u0007b'`,
			property: "body",
		},
		{
			name:     "SET whole-map replacement",
			query:    `MATCH (n:Memory {key:'sprint-38'}) SET n = {body: 'a\u0007b'}`,
			property: "body",
		},
		{
			name:     "SET map merge",
			query:    `MATCH (n:Memory {key:'sprint-38'}) SET n += {body: 'a\u0007b'}`,
			property: "body",
		},
		{
			name:     "FOREACH body",
			query:    `MATCH (n:Memory {key:'sprint-38'}) FOREACH (x IN [1] | SET n.body = 'a\u0007b')`,
			property: "body",
		},
		{
			name:     "list element in a property value",
			query:    `MATCH (n:Memory {key:'sprint-38'}) SET n.tags = ['clean', 'a\u0007b']`,
			property: "tags",
		},
		{
			name:     "concatenation of literals",
			query:    `MATCH (n:Memory {key:'sprint-38'}) SET n.body = 'clean ' + 'a\u0007b'`,
			property: "body",
		},
		{
			name:     "UNION branch",
			query:    `CREATE (n:Memory {key:'sprint-38'}) RETURN n.key UNION CREATE (m:Memory {key:'a\u0007b'}) RETURN m.key`,
			property: "key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parser.Parse(tc.query); err != nil {
				t.Fatalf("the query does not parse, so this case cannot show what the walk reaches: %v", err)
			}
			r, refused := refusedOnTheWritePath(tc.query)
			if !refused {
				t.Fatal("a written property value carrying U+0007 was admitted; this write position is not covered")
			}
			if r.Property != tc.property {
				t.Errorf("property = %q, want %q", r.Property, tc.property)
			}
			if r.CodePoint != "U+0007" {
				t.Errorf("code point = %q, want %q", r.CodePoint, "U+0007")
			}
		})
	}
}

// TestRefusedPropertyValue_AcceptsLegitimateValues is the other side of the
// rule. A guard rail that refuses everything is not a guard rail, and each case
// here is text a real knowledge-graph entry carries.
func TestRefusedPropertyValue_AcceptsLegitimateValues(t *testing.T) {
	queries := []string{
		`CREATE (n:Memory {key: 'sprint-38-scope', body: 'Correctness sweep: 12 recorded defects, no auditing.'})`,
		`CREATE (n:Spec {key: 'GRAPH.md', title: 'Especificação do grafo — acentuação, ç, ã'})`,
		`MATCH (n:Memory {key:'sprint-38-scope'}) SET n.body = 'line one` + "\n" + `line two` + "\t" + `tabbed'`,
		`MATCH (n:Memory {key:'sprint-38-scope'}) SET n.emoji = 'shipped 🚀 and measured 📊'`,
		`MATCH (n:Memory {key:'sprint-38-scope'}) SET n.cjk = '知識グラフのプロパティ値'`,
		`CREATE (n:Commit {key: 'cf27c57', subject: 'test(docs): derive the guard from the contract'})`,
		// A clause keyword and a control-character NAME inside a literal are
		// ordinary text, not a violation.
		`CREATE (n:Memory {key: 'sprint-38-note', body: 'the SET clause writes \u0041, not ESC'})`,
	}

	for _, q := range queries {
		if r, refused := refusedOnTheWritePath(q); refused {
			t.Errorf("legitimate query refused as %+v:\n  %s", r, q)
		}
	}
}

// TestRefusedPropertyValue_NonWritePositionsAreNotJudged pins the boundary of
// the rule: it governs the values a query WRITES, and nothing else.
//
// A MATCH pattern's inline map selects rows and persists nothing, so a control
// character in one is not this rule's business. The cases are deliberately
// control-character cases: an invalid BYTE anywhere in the query is refused by
// the encoding half, whose reach is wider and is asserted separately below.
func TestRefusedPropertyValue_NonWritePositionsAreNotJudged(t *testing.T) {
	queries := []struct {
		name  string
		query string
	}{
		{"MATCH inline property map", `MATCH (n:Memory {key: 'a\u0007b'}) SET n.body = 'clean'`},
		{"WHERE predicate", `MATCH (n:Memory) WHERE n.key = 'a\u0007b' SET n.body = 'clean'`},
		{"RETURN projection", `MATCH (n:Memory) SET n.body = 'clean' RETURN 'a\u0007b' AS label`},
		{"DELETE with a matching literal", `MATCH (n:Memory {key: 'a\u0007b'}) DELETE n`},
	}

	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			if r, refused := refusedOnTheWritePath(tc.query); refused {
				t.Errorf("a literal in a non-write position was judged as a written value: %+v", r)
			}
		})
	}
}

// TestRefusedPropertyValue_ComputedValuesAreNotSeen states the limit of the
// check as a test rather than only as a comment, so the limit is a recorded
// fact and a later reading of the code cannot mistake silence for coverage.
//
// None of these values exists until the engine runs the statement, so Groadmap
// never holds one and cannot judge it. Closing this gap means checking at the
// storage boundary, which is inside the engine.
func TestRefusedPropertyValue_ComputedValuesAreNotSeen(t *testing.T) {
	queries := []struct {
		name  string
		query string
	}{
		{"value copied from another node", `MATCH (n:Memory), (o:Memory {key:'other'}) SET n.body = o.body`},
		{"value from a function over graph content", `MATCH (a)-[e]->(b) SET a.last_type = type(e)`},
		{"value from a parameter", `MATCH (n:Memory {key:'sprint-38'}) SET n.body = $body`},
		{"value from a string function", `MATCH (n:Memory {key:'sprint-38'}) SET n.body = toUpper(n.key)`},
	}

	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parser.Parse(tc.query); err != nil {
				t.Fatalf("the query does not parse, so it cannot show what the check does with a computed value: %v", err)
			}
			if r, refused := refusedOnTheWritePath(tc.query); refused {
				t.Fatalf("the check claimed to judge a value it cannot see: %+v", r)
			}
		})
	}
}

// TestRefusedPropertyValue_EncodingHalfReachesTheWholeQuery pins the documented
// widening of the encoding half, in both directions.
//
// An invalid byte outside every written value still refuses the query, because
// the engine replaces it just the same and the statement it executes is not the
// statement the caller wrote. The refusal is then UNATTRIBUTED, and saying so is
// the honest answer: naming a property the byte does not belong to would be
// worse than naming none.
func TestRefusedPropertyValue_EncodingHalfReachesTheWholeQuery(t *testing.T) {
	cases := []struct {
		name         string
		query        string
		wantProperty string
	}{
		{
			name:         "in a written value: attributed",
			query:        "MATCH (n:Memory {key:'sprint-38'}) SET n.body = 'a\x80b'",
			wantProperty: "body",
		},
		{
			name:         "in a MATCH key: refused, unattributed",
			query:        "MATCH (n:Memory {key:'a\x80b'}) SET n.body = 'clean'",
			wantProperty: "",
		},
		{
			name:         "in a label: refused, unattributed",
			query:        "CREATE (n:Mem\x80ory {key:'sprint-38'})",
			wantProperty: "",
		},
		{
			name:         "in a comment: refused, unattributed",
			query:        "MATCH (n:Memory {key:'sprint-38'}) // note: a\x80b\nSET n.body = 'clean'",
			wantProperty: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, refused := refusedOnTheWritePath(tc.query)
			if !refused {
				t.Fatal("a query carrying a byte that decodes to no character was admitted")
			}
			if r.Violation != utils.FreeTextInvalidUTF8 {
				t.Errorf("violation = %v, want FreeTextInvalidUTF8", r.Violation)
			}
			if r.Property != tc.wantProperty {
				t.Errorf("property = %q, want %q", r.Property, tc.wantProperty)
			}
			if r.Attributed() != (tc.wantProperty != "") {
				t.Errorf("Attributed() = %v, want %v", r.Attributed(), tc.wantProperty != "")
			}
		})
	}
}

// TestRefusedPropertyValue_AttributionIsWithheldOnAGenuineReplacementCharacter
// proves the guard on attribution is real. A caller may legitimately write
// U+FFFD, and when it does, a U+FFFD in a parsed value no longer identifies a
// replaced byte. The refusal must still fire — the query IS malformed — while
// the naming is withheld.
func TestRefusedPropertyValue_AttributionIsWithheldOnAGenuineReplacementCharacter(t *testing.T) {
	// Both a genuine U+FFFD and a lone continuation byte, in the same value.
	query := "MATCH (n:Memory {key:'sprint-38'}) SET n.body = 'placeholder \uFFFD and a\x80b'"

	r, refused := refusedOnTheWritePath(query)
	if !refused {
		t.Fatal("the malformed query was admitted")
	}
	if r.Violation != utils.FreeTextInvalidUTF8 {
		t.Errorf("violation = %v, want FreeTextInvalidUTF8", r.Violation)
	}
	if r.Attributed() {
		t.Errorf("the refusal named property %q, but a genuine U+FFFD in the query makes attribution unsound", r.Property)
	}
}

// TestRefusedPropertyValue_UnparsableQueryYieldsNoContentObjection matches what
// the two relationship-direction checks do with the same input: a query the
// engine cannot parse gets the engine's own diagnostic, not a content objection
// that would mask it.
//
// The encoding half is the exception and must stay one: it is decided before the
// parse, so a malformed BYTE is still refused in a query that does not parse.
func TestRefusedPropertyValue_UnparsableQueryYieldsNoContentObjection(t *testing.T) {
	const broken = `CREATE (n:Memory {key: 'sprint-38'` // never closed

	if _, err := parser.Parse(broken); err == nil {
		t.Fatal("the fixture parses, so it cannot show what happens when parsing fails")
	}
	if r, refused := refusedOnTheWritePath(broken); refused {
		t.Errorf("a content objection was raised for a query the engine cannot parse: %+v", r)
	}

	brokenAndMalformed := broken + "\x80"
	r, refused := refusedOnTheWritePath(brokenAndMalformed)
	if !refused {
		t.Fatal("the encoding half must decide before the parse, so a malformed byte in an unparsable query is still refused")
	}
	if r.Attributed() {
		t.Errorf("an unparsable query has no property to attribute to, got %q", r.Property)
	}
}

// TestRefusedPropertyValue_ReportsTheRuleUtilsWouldReport closes the loop
// between this check and the shared rules: for every value the walk reaches, the
// verdict this package publishes is the verdict internal/utils reaches on the
// same value. If the two ever diverge, the graph is being held to a rule of its
// own, which is the defect this task exists to remove.
func TestRefusedPropertyValue_ReportsTheRuleUtilsWouldReport(t *testing.T) {
	values := []string{
		"clean text",
		"deploy \x1b[31mFAILED",
		"invoice\u202egpj.exe",
		"page one\fpage two",
		"line one\nline two\ttabbed",
		"acentuação e emoji 🚀",
	}

	for _, v := range values {
		query := "CREATE (n:Memory {key: 'sprint-38', body: '" + escapeCypher(v) + "'})"
		want := utils.InspectFreeText(v)

		r, refused := refusedOnTheWritePath(query)
		if (want != utils.FreeTextValid) != refused {
			t.Errorf("utils says %v for %q but the graph check %s it", want, v,
				map[bool]string{true: "refused", false: "admitted"}[refused])
			continue
		}
		if refused && r.Violation != want {
			t.Errorf("value %q: graph check reports %v, utils reports %v", v, r.Violation, want)
		}
	}
}

// ---------------------------------------------------------------------------
// The two reaches. Added when the encoding rule was extended to every
// subcommand that accepts a Cypher query, while the control-character rule
// stayed with the two that write property values.
// ---------------------------------------------------------------------------

// TestRefusedQueryEncoding_ReachesTheStatementsThatOnlyREAD is the extension
// itself. A read or a delete stores nothing, so the control-character rule has
// no business with it -- but the engine still replaces every byte that decodes
// to no character before the grammar runs, so the literal the statement MATCHES
// on is not the literal supplied.
//
// The `delete` shape is the one that carried the decision: it matches nothing
// and the command reports success having removed nothing, which is the failure
// the caller has no reason to check.
func TestRefusedQueryEncoding_ReachesTheStatementsThatOnlyREAD(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"a read matching on a malformed literal", "MATCH (n:Memory {key:'a\x80b'}) RETURN n.body"},
		{"a read whose WHERE predicate is malformed", "MATCH (n:Memory) WHERE n.key = 'a\x80b' RETURN n"},
		{"a delete matching on a malformed literal", "MATCH (n:Memory {key:'a\x80b'}) DELETE n"},
		{"a detach delete", "MATCH (n:Memory {key:'a\x80b'}) DETACH DELETE n"},
		{"a delete gated by a malformed WHERE", "MATCH (n:Memory) WHERE n.key = 'a\x80b' DELETE n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, refused := RefusedQueryEncoding(tc.query)
			if !refused {
				t.Fatal("a statement the engine would silently rewrite was admitted")
			}
			if r.Violation != utils.FreeTextInvalidUTF8 {
				t.Errorf("violation = %v, want FreeTextInvalidUTF8", r.Violation)
			}
			if tc.query[r.Offset] != r.Byte || r.Byte != 0x80 {
				t.Errorf("the refusal reports byte %#02x at offset %d; the query holds %#02x there",
					r.Byte, r.Offset, tc.query[r.Offset])
			}
			// None of these writes a property value, so none can be attributed.
			if r.Attributed() {
				t.Errorf("a statement that writes no property value named property %q", r.Property)
			}
		})
	}
}

// TestRefusedWrittenPropertyValue_DoesNotReachAReadOrADelete is the other side
// of the asymmetry, and the reason it exists: the store can legitimately hold a
// value carrying a control character -- everything written before this rule, and
// anything a computed expression produces -- so a read that names one must keep
// working. Refusing it would leave that data unreadable rather than merely
// unwritable.
func TestRefusedWrittenPropertyValue_DoesNotReachAReadOrADelete(t *testing.T) {
	queries := []struct {
		name  string
		query string
	}{
		{"read matching on a control character", `MATCH (n:Memory {key:'a\\u001bb'}) RETURN n.body`},
		{"read whose WHERE names one", `MATCH (n:Memory) WHERE n.body = 'deploy \\u001b[31mFAILED' RETURN n.key`},
		{"delete matching on one", `MATCH (n:Memory {key:'a\\u001bb'}) DELETE n`},
		{"detach delete matching on one", `MATCH (n:Memory {key:'a\\u001bb'}) DETACH DELETE n`},
	}

	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parser.Parse(tc.query); err != nil {
				t.Fatalf("the query does not parse, so it cannot show what the rule reaches: %v", err)
			}
			if r, refused := RefusedWrittenPropertyValue(tc.query); refused {
				t.Errorf("a statement that stores nothing was refused for the content of a value: %+v", r)
			}
		})
	}
}

// TestRefusedWrittenPropertyValue_JudgesNoEncoding keeps the split honest in the
// other direction: this half must not answer the encoding question, because it
// CANNOT -- by the time it reads a parsed value the malformed bytes are already
// U+FFFD. A version of it that appeared to catch malformed UTF-8 would be
// catching the replacement character, not the defect.
func TestRefusedWrittenPropertyValue_JudgesNoEncoding(t *testing.T) {
	for _, c := range testenv.MalformedUTF8Corpus() {
		query := "CREATE (n:Memory {key: 'sprint-38', body: '" + escapeCypher(c.Value) + "'})"
		if r, refused := RefusedWrittenPropertyValue(query); refused {
			t.Errorf("the control-character half claimed a verdict on malformed UTF-8 (%s): %+v", c.Name, r)
		}
		if _, refused := RefusedQueryEncoding(query); !refused {
			t.Errorf("the encoding half missed %s, so nothing refuses it at all", c.Name)
		}
	}
}
