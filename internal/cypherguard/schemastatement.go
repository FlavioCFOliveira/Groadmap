package cypherguard

// schemastatement.go implements the ONE STATEMENT PER INVOCATION rule for
// schema-mutating DDL (SPEC/GRAPH.md § One Statement per Invocation).
//
// # Why the rule exists at all
//
// The pinned engine's schema parser stops as soon as its grammar is satisfied
// and DISCARDS the rest of the statement — without an error, without a
// notification, and without any other trace. Handed
//
//	CREATE INDEX spec_key FOR (n:Spec) ON (n.key) MATCH (m) SET m.reviewed = true
//
// it creates the index, drops the MATCH ... SET on the floor, and returns
// success. Unrefused, `graph update` would print {"ok": true} and exit 0 for a
// statement half of which never ran, and the caller has no reason to check: the
// command reported that it worked. The refusal is therefore Groadmap's own and
// cannot be delegated to the engine (SPEC/GRAPH.md § One Statement per
// Invocation; the behaviour is measured, not inferred — see the differential
// tests beside this file).
//
// # Why the check is structural and not a keyword scan
//
// A keyword scan over the statement's text answers the wrong question. The
// clause keywords it would look for are also legitimate schema-object names:
//
//	CREATE INDEX spec_set FOR (n:Spec) ON (n.set)
//
// is a valid index on a property called `set`, which the engine creates and
// which the guard rail must therefore admit. `\bset\b` matches inside `(n.set)`
// — the dot and the closing parenthesis are both word boundaries — so a scan
// refuses exactly the statement that must be accepted. Refusing what the engine
// silently discards and admitting what it executes are BOTH required, and a
// mechanism that achieves only the first is not sufficient.
//
// So the check walks the engine's own DDL grammar over the engine's own token
// stream and reports where the statement ENDS. `set` in the example above is
// consumed as the property of an `ON (n.prop)` production; it is never compared
// against a clause keyword, so its spelling cannot influence the verdict. Any
// token beyond that end point is text the engine would discard.
//
// # Fidelity to the engine, and what happens when it is lost
//
// The scanner mirrors GoGraph's `cypher/ir` DDL parser: the same tokenisation
// (whitespace separates; `( ) { } : , ;` are single-character tokens), the same
// leading-comment trimming, the same `strings.ToUpper` keyword folding, and the
// same productions including the permissive corners (an `OPTIONS` clause with no
// opening brace is a no-op, an unterminated `{` consumes to the end). A mirror
// can drift from what it mirrors, so the verdict is never taken on the mirror's
// word alone: before a statement is refused, the engine's own parser is asked to
// parse the head the scanner isolated, and the refusal stands only when that
// head parses to a plan IDENTICAL to the plan the whole statement parses to —
// which is the definition of a discarded tail. When the two disagree, the
// scanner is wrong about this statement and the query is ADMITTED, leaving the
// engine to accept or refuse it on its own terms. The rule can therefore fail to
// refuse, but it can never refuse a statement the engine would have executed
// whole, and SPEC/GRAPH.md § Dependency Maturity Risk mitigation 5 re-verifies
// the grammar on every engine upgrade.

import (
	"reflect"
	"strings"

	"github.com/FlavioCFOliveira/GoGraph/cypher/ir"
)

// TrailingClauseAfterSchemaStatement reports whether query is a schema-mutating
// DDL statement (CREATE INDEX, DROP INDEX, CREATE CONSTRAINT, DROP CONSTRAINT)
// carrying further text after the statement itself, and returns that text.
//
// found is true only when the trailing text is text the engine would silently
// discard, proven by parsing: the isolated head parses to the same plan as the
// whole statement. tail is the trailing text with surrounding whitespace
// removed; it is never empty when found is true.
//
// It answers false, with no tail, for every query that is not one of the four
// schema-mutating forms — an ordinary read or write, a SHOW schema-introspection
// command (whose own parser rejects a trailing clause with a precise message
// instead of discarding it, so this rule would add nothing and MUST NOT be
// extended to it), and a statement the engine will not route to its schema
// parser at all, such as one whose two keywords are separated by anything other
// than a single space.
//
// Trailing text that carries no clause is not a trailing clause: whitespace, a
// comment, and a single statement terminator `;` are each inert and are
// accepted. Comments are recognised through MaskLiterals, the same
// literal-aware normalization every clause-class check runs on.
func TrailingClauseAfterSchemaStatement(query string) (tail string, found bool) {
	// The engine routes a statement to its DDL executor on ir.IsDDL and then
	// dispatches inside ir.ParseDDL. Both must select a schema-mutating form for
	// this rule to have a subject, so both are asked rather than one standing in
	// for the other.
	if !ir.IsDDL(query) {
		return "", false
	}
	stmt := trimLeadingComments(query)
	form, ok := schemaMutatingForm(stmt)
	if !ok {
		return "", false
	}

	toks := tokenizeDDL(stmt)
	end, ok := form.extent(toks)
	if !ok {
		// The scanner could not walk this statement to a grammar-complete end.
		// It is malformed, or it uses a shape this mirror does not know; either
		// way the engine is the party that decides, and admitting is what leaves
		// that decision with it.
		return "", false
	}
	if end >= len(toks) {
		return "", false
	}

	rest := stmt[toks[end].start:]
	if inertTrailingText(rest) {
		return "", false
	}

	// The refusal is proven, not asserted: the head the scanner isolated must
	// parse to the same plan as the whole statement, which is exactly what "the
	// engine discards the tail" means. A disagreement means the mirror is wrong
	// about this statement, and the query is admitted.
	head := stmt[:toks[end].start]
	headPlan, headErr := ir.ParseDDL(head)
	wholePlan, wholeErr := ir.ParseDDL(stmt)
	if headErr != nil || wholeErr != nil || !reflect.DeepEqual(headPlan, wholePlan) {
		return "", false
	}

	return strings.TrimSpace(rest), true
}

// inertTrailingText reports whether text following a complete schema statement
// carries no clause. Whitespace, comments, and a single trailing `;` statement
// terminator are inert; anything else is a clause the engine would discard.
//
// Comments are neutralized by MaskLiterals rather than by a scan of their own,
// so a `//` or `/*` inside a string literal is not mistaken for one — the same
// normalization every clause-class check runs on (SPEC/GRAPH.md
// § Literal-Aware Normalization).
func inertTrailingText(rest string) bool {
	masked := strings.TrimSpace(MaskLiterals(rest))
	return masked == "" || masked == ";"
}

// ddlToken is one token of the engine's DDL token stream together with the byte
// offset at which it starts in the trimmed statement, so a head and a tail can
// be cut from the ORIGINAL text rather than rebuilt from tokens (rebuilding
// would change spacing, and the spacing is part of what the engine parses).
type ddlToken struct {
	text  string
	start int
}

// ddlPunctuation is the set of characters GoGraph's DDL tokeniser emits as
// single-character tokens; every other non-whitespace run accumulates into one
// token. Kept identical to that tokeniser: a divergence here would make every
// token index in this file describe a stream the engine never sees.
const ddlPunctuation = "(){}:,;"

// tokenizeDDL splits a trimmed DDL statement the way GoGraph's DDL parser does,
// recording each token's byte offset. Whitespace separates tokens and is not
// itself a token; each punctuation character is a token of its own; every other
// run of characters is one token, which is why `n.prop` and `'btree'` each
// arrive whole.
func tokenizeDDL(s string) []ddlToken {
	var toks []ddlToken
	start := -1
	flush := func(end int) {
		if start >= 0 {
			toks = append(toks, ddlToken{text: s[start:end], start: start})
			start = -1
		}
	}
	for i, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush(i)
		case r < 0x80 && strings.ContainsRune(ddlPunctuation, r):
			flush(i)
			toks = append(toks, ddlToken{text: string(r), start: i})
		default:
			if start < 0 {
				start = i
			}
		}
	}
	flush(len(s))
	return toks
}

// schemaMutatingKind names which of the four schema-mutating statements a query
// is, as the engine's own ParseDDL dispatch decides it.
type schemaMutatingKind struct {
	// extent walks the token stream from the start of the statement and returns
	// the index of the first token BEYOND the statement, or ok=false when the
	// stream does not form a complete statement of this kind.
	extent func([]ddlToken) (int, bool)
}

// schemaMutatingForm reports which schema-mutating statement stmt is, using the
// same dispatch ParseDDL uses: a prefix test on strings.ToUpper of the trimmed
// statement. Uppercasing rather than case-folding is deliberate and is the
// engine's own choice — it is what makes a non-ASCII letter that uppercases onto
// an ASCII one (U+0131 onto 'I') select the same branch here as it does there.
//
// A SHOW schema-introspection command reaches neither branch: ParseDDL tests the
// four schema-mutating prefixes first, and this rule covers only those.
func schemaMutatingForm(stmt string) (schemaMutatingKind, bool) {
	upper := strings.ToUpper(stmt)
	switch {
	case strings.HasPrefix(upper, "CREATE INDEX"):
		return schemaMutatingKind{extent: extentCreateIndex}, true
	case strings.HasPrefix(upper, "DROP INDEX"):
		return schemaMutatingKind{extent: extentDropByName}, true
	case strings.HasPrefix(upper, "CREATE CONSTRAINT"):
		return schemaMutatingKind{extent: extentCreateConstraint}, true
	case strings.HasPrefix(upper, "DROP CONSTRAINT"):
		return schemaMutatingKind{extent: extentDropByName}, true
	}
	return schemaMutatingKind{}, false
}

// ddlCursor is a position in a DDL token stream, with the small vocabulary the
// grammar walks below need. Every read is bounds-checked, because the statement
// is untrusted text that may stop anywhere.
type ddlCursor struct {
	toks []ddlToken
	pos  int
}

// peekUpper returns the token at the cursor, uppercased, or "" at end of input —
// matching the engine's own peekUpper, which reads "" past the end.
func (c *ddlCursor) peekUpper() string {
	if c.pos >= len(c.toks) {
		return ""
	}
	return strings.ToUpper(c.toks[c.pos].text)
}

// peek returns the token at the cursor verbatim, or "" at end of input.
func (c *ddlCursor) peek() string {
	if c.pos >= len(c.toks) {
		return ""
	}
	return c.toks[c.pos].text
}

// take consumes one token and reports whether there was one to consume.
func (c *ddlCursor) take() bool {
	if c.pos >= len(c.toks) {
		return false
	}
	c.pos++
	return true
}

// expect consumes the token at the cursor when it uppercases to want.
func (c *ddlCursor) expect(want string) bool {
	if c.peekUpper() != want {
		return false
	}
	c.pos++
	return true
}

// extentCreateIndex walks
//
//	CREATE INDEX [IF NOT EXISTS] [name] [IF NOT EXISTS] FOR (n:Label) ON (n.prop) [OPTIONS {…}]
//
// The optional IF NOT EXISTS is accepted on either side of the optional name,
// because the engine accepts both the Neo4j order and its own legacy one.
func extentCreateIndex(toks []ddlToken) (int, bool) {
	c := &ddlCursor{toks: toks}
	if !c.expect("CREATE") || !c.expect("INDEX") {
		return 0, false
	}
	ifNotExists, ok := takeIfNotExists(c)
	if !ok {
		return 0, false
	}
	// The name is present unless the next token opens the statement body. This
	// is a POSITIONAL decision, not a keyword one: whatever token sits here that
	// is neither FOR nor IF is the name, whatever it spells.
	if kw := c.peekUpper(); kw != "FOR" && kw != "IF" {
		if !c.take() {
			return 0, false
		}
	}
	if !ifNotExists {
		if _, ok := takeIfNotExists(c); !ok {
			return 0, false
		}
	}
	if !c.expect("FOR") || !takeNodePattern(c) || !c.expect("ON") || !takePropAccess(c) {
		return 0, false
	}
	if !takeIndexOptions(c) {
		return 0, false
	}
	return c.pos, true
}

// extentDropByName walks DROP INDEX / DROP CONSTRAINT <name> [IF EXISTS], which
// share one shape.
//
// The engine's own cursor is looser here — it consumes one token unconditionally
// while probing for IF, so a stray token is swallowed by the probe rather than
// left behind. This walk follows the GRAMMAR instead: a token that is not `IF`
// does not belong to the statement, so `DROP INDEX spec_key MATCH (m) SET m.x=1`
// ends after the name and the rest is a trailing clause, which is what the
// caller must be told.
func extentDropByName(toks []ddlToken) (int, bool) {
	c := &ddlCursor{toks: toks}
	if !c.expect("DROP") {
		return 0, false
	}
	if !c.expect("INDEX") && !c.expect("CONSTRAINT") {
		return 0, false
	}
	if !c.take() { // name; the engine refuses a statement without one
		return 0, false
	}
	if c.peekUpper() == "IF" {
		c.pos++
		if !c.expect("EXISTS") {
			return 0, false
		}
	}
	return c.pos, true
}

// extentCreateConstraint walks both node-property constraint grammars the engine
// accepts:
//
//	CREATE CONSTRAINT [name] [IF NOT EXISTS] FOR (n:Label) REQUIRE n.prop IS UNIQUE|NOT NULL [IF NOT EXISTS]
//	CREATE CONSTRAINT [name] [IF NOT EXISTS] ON  (n:Label) ASSERT  n.prop IS UNIQUE|NOT NULL [IF NOT EXISTS]
func extentCreateConstraint(toks []ddlToken) (int, bool) {
	c := &ddlCursor{toks: toks}
	if !c.expect("CREATE") || !c.expect("CONSTRAINT") {
		return 0, false
	}
	// Positional name, excluding the three tokens that open the body.
	if kw := c.peekUpper(); kw != "ON" && kw != "FOR" && kw != "IF" {
		if !c.take() {
			return 0, false
		}
	}
	ifNotExists, ok := takeIfNotExists(c)
	if !ok {
		return 0, false
	}

	// The connective fixes the assertion keyword: FOR pairs with REQUIRE, the
	// legacy ON pairs with ASSERT.
	var assertKW string
	switch c.peekUpper() {
	case "FOR":
		assertKW = "REQUIRE"
	case "ON":
		assertKW = "ASSERT"
	default:
		return 0, false
	}
	c.pos++

	if !takeNodePattern(c) || !c.expect(assertKW) || !takeConstraintProp(c) || !c.expect("IS") {
		return 0, false
	}
	switch c.peekUpper() {
	case "UNIQUE":
		c.pos++
	case "NOT":
		c.pos++
		if !c.expect("NULL") {
			return 0, false
		}
	default:
		// NODE KEY, a relationship key, and a property type constraint each
		// reach here; the engine refuses all three with a specific message, so
		// the statement is left to it.
		return 0, false
	}
	if !ifNotExists && c.peekUpper() == "IF" {
		if _, ok := takeIfNotExists(c); !ok {
			return 0, false
		}
	}
	return c.pos, true
}

// takeIfNotExists consumes an IF NOT EXISTS when the cursor is on one. It
// reports present=false with ok=true when there is no IF at all, and ok=false
// when an IF is not followed by NOT EXISTS — which the engine treats as a
// syntax error.
func takeIfNotExists(c *ddlCursor) (present, ok bool) {
	if c.peekUpper() != "IF" {
		return false, true
	}
	c.pos++
	if !c.expect("NOT") || !c.expect("EXISTS") {
		return false, false
	}
	return true, true
}

// takeNodePattern consumes `( n : Label )`, the node pattern of a FOR/ON clause.
//
// The engine keeps a fallback for a pattern that arrived as a single token; the
// tokeniser emits `(`, `)` and `:` as tokens of their own, so that fallback is
// unreachable through it. It is mirrored anyway, so that the walk does not
// depend on a property of the tokeniser it does not itself enforce.
func takeNodePattern(c *ddlCursor) bool {
	if c.pos >= len(c.toks) {
		return false
	}
	if !strings.EqualFold(c.peek(), "(") {
		compact := strings.TrimSuffix(strings.TrimPrefix(c.peek(), "("), ")")
		if !strings.Contains(compact, ":") {
			return false
		}
		c.pos++
		return true
	}
	c.pos++        // (
	if !c.take() { // variable
		return false
	}
	if c.peek() != ":" {
		return false
	}
	c.pos++
	if !c.take() { // label
		return false
	}
	if c.peek() != ")" {
		return false
	}
	c.pos++
	return true
}

// takePropAccess consumes `( n.prop )`, the property target of an ON clause.
//
// A comma at the property position is a composite index, which the engine
// refuses with its own message; the walk stops so the statement reaches it.
func takePropAccess(c *ddlCursor) bool {
	if c.pos >= len(c.toks) {
		return false
	}
	if !strings.EqualFold(c.peek(), "(") {
		compact := strings.TrimSuffix(strings.TrimPrefix(c.peek(), "("), ")")
		if !strings.Contains(compact, ".") {
			return false
		}
		c.pos++
		return true
	}
	c.pos++ // (
	access := c.peek()
	if !c.take() { // n.prop
		return false
	}
	if c.peek() == "," {
		return false // composite index: the engine refuses it by name
	}
	if c.peek() != ")" {
		return false
	}
	c.pos++
	return strings.Contains(access, ".")
}

// takeConstraintProp consumes the property target of a REQUIRE/ASSERT clause,
// which is a bare `n.prop` token or a parenthesised single `( n.prop )`.
func takeConstraintProp(c *ddlCursor) bool {
	if c.pos >= len(c.toks) {
		return false
	}
	if c.peek() != "(" {
		access := c.peek()
		c.pos++
		return strings.Contains(access, ".")
	}
	c.pos++ // (
	access := c.peek()
	if !c.take() {
		return false
	}
	if c.peek() == "," {
		return false // composite constraint: the engine refuses it by name
	}
	if c.peek() != ")" {
		return false
	}
	c.pos++
	return strings.Contains(access, ".")
}

// takeIndexOptions consumes an optional `OPTIONS { key : value, … }` clause.
//
// Two permissive corners of the engine's own options parser are reproduced
// exactly, because the statement's extent is wherever that parser leaves the
// cursor and not wherever a stricter reading would put it: OPTIONS with no
// opening brace consumes the keyword and nothing else, and an unterminated brace
// consumes to the end of input. An indexType the engine does not know stops the
// walk, so the statement reaches the engine and is refused there by name.
func takeIndexOptions(c *ddlCursor) bool {
	if c.peekUpper() != "OPTIONS" {
		return true
	}
	c.pos++ // OPTIONS
	if c.peek() != "{" {
		return true
	}
	c.pos++ // {
	for c.pos < len(c.toks) && c.peek() != "}" {
		key := strings.ToLower(c.peek())
		c.pos++
		if c.peek() == ":" {
			c.pos++
		}
		if c.pos >= len(c.toks) {
			break
		}
		val := strings.ToLower(strings.Trim(c.peek(), `"'`))
		c.pos++
		if key == "indextype" && val != "hash" && val != "btree" {
			return false
		}
		if c.peek() == "," {
			c.pos++
		}
	}
	if c.peek() == "}" {
		c.pos++
	}
	return true
}

// trimLeadingComments removes leading whitespace and any run of leading `//` and
// `/* … */` comments, mirroring the engine's own pre-dispatch trim so that the
// token stream this file walks starts where the engine's parser starts.
//
// Only LEADING comments are removed, so a comment marker inside a string literal
// later in the statement is never touched. An unterminated comment yields the
// empty string, exactly as it does in the engine, where it makes the statement
// non-DDL and sends it to the grammar for a syntax error.
func trimLeadingComments(query string) string {
	s := strings.TrimSpace(query)
	for {
		switch {
		case strings.HasPrefix(s, "//"):
			i := strings.IndexAny(s, "\r\n")
			if i < 0 {
				return ""
			}
			s = strings.TrimSpace(s[i+1:])
		case strings.HasPrefix(s, "/*"):
			i := strings.Index(s[2:], "*/")
			if i < 0 {
				return ""
			}
			s = strings.TrimSpace(s[2+i+2:])
		default:
			return s
		}
	}
}
