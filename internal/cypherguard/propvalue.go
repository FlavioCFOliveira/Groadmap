package cypherguard

// propvalue.go — the Cypher content checks (SPEC/GRAPH.md § Cypher Query and
// Property Value Content Rules).
//
// # Two rules with two reaches
//
// This file holds two checks, not one, and their reaches differ because their
// causes differ. Keeping them apart is the point: each is stated by what it
// objects to, so a subcommand's exposure to each is decided once, here, and is
// visible at the single call site that enforces it.
//
//   - RefusedQueryEncoding — the Free-Text UTF-8 Encoding Constraint, on the RAW
//     QUERY BYTES. It binds EVERY subcommand that accepts a Cypher query,
//     because a byte the engine silently replaces changes the statement itself,
//     not only a value it stores.
//   - RefusedWrittenPropertyValue — the Free-Text Control-Character Constraint,
//     on the PARSED values a query WRITES. It binds only the two subcommands
//     that write property values, because a control character in a read literal
//     stores nothing and refusing it would deny reach to data the store holds.
//
// Each function documents its own reach and the reason for it.
//
// # The defect this exists to refuse
//
// Every free-text value Groadmap stores is subject to two content rules: the
// Free-Text UTF-8 Encoding Constraint and the Free-Text Control-Character
// Constraint (SPEC/MODELS.md). A Cypher property value written through
// `rmp graph create` or `rmp graph update` was subject to neither, and the graph
// is the project's own memory (CLAUDE.md § 5), so what it holds is meant to be
// the truth about the project.
//
// Both halves were measured on the shipped binary before this check was written:
//
//   - INVALID UTF-8 IS SILENTLY REPLACED. `graph create` of
//     `{body: 'a<0x80>b'}` returns {"ok": true}, exit 0, and the value reads back
//     as the three bytes EF BF BD — a real U+FFFD. The store does not hold what
//     was written and nothing reports the difference. That is data corruption,
//     not a rendering question, and it is what decided the rule.
//   - CONTROL CHARACTERS ARE STORED VERBATIM. `graph create` of
//     `{body: 'a<ESC>[31mred'}` stores a real 0x1B. Its rendering is bounded
//     TODAY — encoding/json escapes it, and the web renders through
//     html/template — but boundedness is a property of the CONSUMER, not of the
//     value, so it cannot be the guarantee.
//
// # Why the check reads TWO different objects, and why that is not a compromise
//
// A property value is not the query text, and the two halves of the rule are
// observable at two different points. Both placements were measured, and each is
// the only place its half exists:
//
//   - The CONTROL-CHARACTER half is decided on the PARSED VALUE, never on the
//     query text. Cypher decodes `\b`, `\f` and `\uXXXX` inside a string literal
//     (openCypher 9, "Note on string literals"; that document numbers no sections,
//     so it is cited by heading), so `SET n.body = 'a\u001b[31mred'` is a query whose
//     text is PURE ASCII and whose written value carries a real ESC. A scan of
//     the query string sees nothing wrong with it. That measurement is the whole
//     reason this check walks the engine's own AST rather than the string the
//     caller typed.
//   - The ENCODING half is decided on the RAW QUERY BYTES, before the parse,
//     because the parse destroys the evidence. ANTLR decodes the query to runes
//     before the grammar ever runs and replaces EACH byte that decodes to no
//     rune with one U+FFFD, so by the time an ast.StringLiteral exists its Value
//     is well-formed UTF-8 whatever was supplied. There is no later point at
//     which the malformed value can be seen. Refusing on the raw bytes is exact
//     in the direction that matters — a written property value cannot carry
//     invalid UTF-8 unless the query text does — so the check has no false
//     negatives. It is WIDER in the other direction: an invalid byte in a label,
//     a property key, a match pattern or a comment refuses the query too. That
//     widening is deliberate and is not a false positive: the engine replaces
//     those bytes just the same, so the statement it would execute is not the
//     statement the caller wrote. It is also why the encoding half reaches the
//     READING subcommands: the substitution happens before the grammar runs and
//     is indifferent to what the statement then does, so a `graph query` matches
//     on a literal it was not given, and a `graph delete` gated by one removes
//     nothing and reports success.
//
// # What the checks can and cannot see
//
// The control-character check sees the values it can KNOW: string literals in
// the property positions a query writes. It cannot see a value that does not exist until the query runs —
// `SET n.p = type(e)`, `SET n.p = other.key`, `SET n.p = toUpper(x)`,
// `SET n.p = $param`. Those are computed by the engine from graph content or from
// a binding, inside the write transaction, and Groadmap never holds them. The
// limit is written down here rather than papered over, because a rule that
// silently missed the computed case would be worse than one whose reach is
// stated: closing it means checking at the storage boundary, which is inside the
// engine and not this project's code.
//
// The one computed shape that IS covered is concatenation of literals,
// `SET n.p = 'a' + 'b'`, and it is covered without special handling: both rules
// are closed under concatenation — two values free of forbidden code points
// concatenate to one, and two well-formed UTF-8 strings concatenate to one — so
// checking each literal operand decides the result.

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/FlavioCFOliveira/GoGraph/cypher/ast"
	"github.com/FlavioCFOliveira/GoGraph/cypher/parser"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// PropertyValueRefusal is one property value a query would write that breaks a
// free-text content rule.
//
// The fields are ordered widest first so the struct packs into the smaller
// allocator size class; the two single-byte fields sit together at the end.
type PropertyValueRefusal struct {
	// Property is the property key the value is assigned to — "body" in both
	// `SET n.body = '…'` and `CREATE (n {body: '…'})`. It is EMPTY when the
	// refusal could not be attributed to one, which happens only on the
	// encoding half: see Attributed.
	Property string
	// CodePoint is the first forbidden code point the value carries, rendered
	// as "U+001B". It is set only for FreeTextControlChars, and it is what the
	// refusal names INSTEAD of echoing the value: it is bounded, it is safe to
	// print, and it identifies exactly what is wrong. Echoing the value would
	// emit the offending character into the terminal the rule exists to
	// protect, and for the encoding half the value is no longer recoverable
	// anyway.
	CodePoint string
	// Offset is the position in the query of the first byte that decodes to no
	// character, and Byte is that byte. Both are set only for
	// FreeTextInvalidUTF8.
	Offset int
	// Violation is which of the two rules the value breaks, and carries the
	// governed wording of the refusal through its Reason method.
	Violation utils.FreeTextViolation
	Byte      byte
}

// Attributed reports whether the refusal names the property the offending value
// belongs to. It is always true for a control-character refusal, which is
// decided on a parsed value and therefore always has one.
//
// It can be false for an encoding refusal, in the three cases attribution is not
// sound or not possible: the invalid byte lies outside every property value the
// query writes (in a label, a match pattern, or a comment); the query does not
// parse at all; or the query already carries a genuine U+FFFD of its own, which
// makes the replacement indistinguishable from the caller's own text. The
// refusal stands in every one of those cases — only the naming is withheld,
// because naming the wrong property would be worse than naming none.
func (r PropertyValueRefusal) Attributed() bool { return r.Property != "" }

// RefusedQueryEncoding reports whether query carries a byte that begins no
// well-formed UTF-8 sequence, applying the Free-Text UTF-8 Encoding Constraint
// (SPEC/MODELS.md) as internal/utils implements it for every other value
// Groadmap stores.
//
// # Why this half binds EVERY subcommand, and not only the writers
//
// It is decided on the RAW BYTES, before the parse, because the parse destroys
// the evidence: ANTLR replaces each byte that decodes to no rune with one
// U+FFFD before the grammar runs. That fact is not about writing. It means the
// statement the engine executes is not the statement the caller wrote, whatever
// the statement then goes on to do:
//
//   - a CREATE or SET stores a value different from the one supplied;
//   - a MATCH compares against a literal different from the one supplied, so the
//     row that should have matched does not, and the command reports success
//     having found nothing;
//   - a DELETE gated by such a literal matches nothing and reports success
//     having removed nothing — the same silent-destructive shape the
//     Relationship Read Direction rule was extended to cover, and the worst in
//     the family, because the caller has no reason to check.
//
// Keying the rule on the CAUSE rather than on the subcommand is what makes those
// three one rule instead of three. Callers reach it through one call each, so a
// subcommand's reach is visible where it is enforced.
func RefusedQueryEncoding(query string) (PropertyValueRefusal, bool) {
	offset := firstInvalidUTF8Offset(query)
	if offset < 0 {
		return PropertyValueRefusal{}, false
	}
	return PropertyValueRefusal{
		Violation: utils.FreeTextInvalidUTF8,
		Property:  attributeReplacedByte(query),
		Byte:      query[offset],
		Offset:    offset,
	}, true
}

// RefusedWrittenPropertyValue reports the first property value query would WRITE
// that carries a code point the Free-Text Control-Character Constraint forbids
// (SPEC/MODELS.md).
//
// # Why this half does NOT bind the reading subcommands
//
// The asymmetry with RefusedQueryEncoding is deliberate, and the two halves are
// separable precisely because they object to different things. The encoding rule
// objects to a query the engine would SILENTLY REWRITE. This rule objects to a
// value that would be STORED, and a control character in a read literal stores
// nothing: it is compared against what the graph already holds.
//
// The store can legitimately hold such a value — everything written before this
// rule existed, and anything a computed expression produces, which is outside
// what any of this can see. Refusing a read that names one would deny reach to
// data the store holds, and would leave it unreadable rather than merely
// unwritable. So the reading subcommands are bound by the encoding rule alone.
//
// It is decided on the PARSED value, never on the query text, for the reason the
// file comment measures: Cypher decodes `\b`, `\f` and `\uXXXX` inside a string
// literal, so a query of pure ASCII can write a real control character.
//
// The caller MUST apply RefusedQueryEncoding first. That is the order
// SPEC/MODELS.md fixes for the pair, and it is not a preference: an invalid byte
// decodes to U+FFFD, which is not a forbidden code point, so this rule would
// answer "fine" for a value the other one refuses. The order lives at the call
// site because that is also where the two reaches differ; the write path applies
// both, in that order, and TestWritePathAppliesTheTwoRulesInTheSpecifiedOrder is
// what holds it there.
func RefusedWrittenPropertyValue(query string) (PropertyValueRefusal, bool) {
	var refusal PropertyValueRefusal
	found := false
	walkWrittenPropertyValues(query, func(key string, lit *ast.StringLiteral) bool {
		if utils.InspectFreeText(lit.Value) != utils.FreeTextControlChars {
			return true
		}
		refusal = PropertyValueRefusal{
			Violation: utils.FreeTextControlChars,
			Property:  key,
			CodePoint: forbiddenCodePointIn(lit.Value),
		}
		found = true
		return false
	})
	return refusal, found
}

// firstInvalidUTF8Offset returns the byte offset of the first byte of query that
// begins no well-formed UTF-8 sequence, or -1 when the whole query is valid.
//
// The verdict itself is utils.InspectFreeText's — the offset is only WHERE, and
// it is computed here so the refusal can name the byte without echoing the text
// around it. Deciding validity with the shared rule rather than with a local
// scan is what keeps this from becoming a second definition of well-formedness.
func firstInvalidUTF8Offset(query string) int {
	if utils.InspectFreeText(query) != utils.FreeTextInvalidUTF8 {
		return -1
	}
	for i := 0; i < len(query); {
		r, size := utf8.DecodeRuneInString(query[i:])
		if r == utf8.RuneError && size <= 1 {
			return i
		}
		i += size
	}
	// Unreachable: InspectFreeText answered FreeTextInvalidUTF8, so the scan
	// above must have found the byte that made it answer so.
	return -1
}

// attributeReplacedByte returns the key of the first property value the query
// writes that the engine would fill with a replaced byte, or "" when no such
// attribution is sound.
//
// It works because the replacement is observable in the parsed value: ANTLR
// turns each byte that decodes to no rune into one U+FFFD, so a written value
// containing U+FFFD came from an invalid byte — PROVIDED the query carries no
// U+FFFD of its own. That proviso is checked rather than assumed, and when it
// fails the attribution is withheld rather than guessed at.
//
// The query is known to be invalid UTF-8 when this is called, so the refusal has
// already been decided; this only decides whether it can name a property.
func attributeReplacedByte(query string) string {
	// A genuine U+FFFD in the query makes the engine's replacement
	// indistinguishable from the caller's own text.
	if strings.Contains(query, string(utf8.RuneError)) {
		return ""
	}
	key := ""
	walkWrittenPropertyValues(query, func(k string, lit *ast.StringLiteral) bool {
		if !strings.ContainsRune(lit.Value, utf8.RuneError) {
			return true
		}
		key = k
		return false
	})
	return key
}

// forbiddenCodePointIn renders the first code point of value that the
// control-character rule rejects, as "U+001B". It returns "" when there is none.
//
// The rule applied is utils.IsForbiddenControlChar, the same one rune at a time
// that decided the refusal, so this can never disagree with the verdict about
// which character is at fault.
func forbiddenCodePointIn(value string) string {
	for _, r := range value {
		if utils.IsForbiddenControlChar(r) {
			// %04X is the Unicode convention: at least four digits, so ESC is
			// U+001B and not U+1B, and a code point that needs more takes more.
			return fmt.Sprintf("U+%04X", r)
		}
	}
	return ""
}

// walkWrittenPropertyValues calls fn for every string literal that query assigns
// to a property, in the order the query writes them, until fn returns false.
//
// The write positions, and only these, are the ones the engine persists:
//
//   - the inline property map of a node or relationship pattern in CREATE and
//     MERGE — `CREATE (n:Memory {key: 'k', body: 'b'})`;
//   - a SET assignment, in all three of its value-writing forms:
//     `SET n.body = 'b'`, `SET n = {body: 'b'}` and `SET n += {body: 'b'}`;
//   - the ON CREATE SET and ON MATCH SET actions of a MERGE;
//   - the same clauses inside a FOREACH body, which writes through the same
//     operator one element at a time.
//
// A MATCH pattern's inline map is deliberately NOT walked: it selects rows, it
// does not write, and the value it carries is never persisted. A query the
// parser rejects yields nothing, exactly as the two relationship-direction
// checks do: it cannot execute either, and reporting a content objection for it
// would mask the syntax error the engine is about to name itself.
func walkWrittenPropertyValues(query string, fn func(key string, lit *ast.StringLiteral) bool) {
	parsed, err := parser.Parse(query)
	if err != nil {
		return
	}
	w := &writeScan{fn: fn}
	switch t := parsed.(type) {
	case *ast.SingleQuery:
		w.singleQuery(t)
	case *ast.MultiQuery:
		for _, part := range t.Parts {
			w.singleQuery(part)
		}
	}
}

// writeScan walks the clauses that write property values, stopping as soon as
// the callback asks it to.
type writeScan struct {
	fn      func(key string, lit *ast.StringLiteral) bool
	stopped bool
}

// emit reports one written value and records whether the walk should continue.
func (w *writeScan) emit(key string, lit *ast.StringLiteral) {
	if w.stopped || lit == nil {
		return
	}
	if !w.fn(key, lit) {
		w.stopped = true
	}
}

// singleQuery walks one query branch's writing clauses. Reading clauses are
// walked too, because CREATE and MERGE are updating clauses that the parser may
// place in either list depending on the shape of the query, and a clause visited
// twice reports the same values twice — which the first-refusal contract makes
// harmless — while one never visited would be missed.
func (w *writeScan) singleQuery(q *ast.SingleQuery) {
	if q == nil {
		return
	}
	for _, rc := range q.ReadingClauses {
		w.clause(rc)
	}
	for _, uc := range q.UpdatingClauses {
		w.clause(uc)
	}
}

// clause dispatches one clause to the write positions it carries.
func (w *writeScan) clause(c ast.Clause) {
	if w.stopped {
		return
	}
	switch t := c.(type) {
	case *ast.Create:
		w.pattern(t.Pattern)
	case *ast.Merge:
		w.path(t.Pattern)
		w.setItems(t.OnCreate)
		w.setItems(t.OnMatch)
	case *ast.Set:
		w.setItems(t.Items)
	case *ast.Foreach:
		for _, body := range t.Body {
			w.clause(body)
		}
	}
}

// setItems walks the value side of every SET assignment.
func (w *writeScan) setItems(items []*ast.SetItem) {
	for _, item := range items {
		if w.stopped || item == nil {
			return
		}
		w.setItem(item)
	}
}

// setItem walks one SET assignment. `SET n.body = '…'` names its property in the
// target; `SET n = {…}` and `SET n += {…}` name theirs in the map on the right.
// A label-set form (`SET n:Label`) carries no value and no property.
func (w *writeScan) setItem(item *ast.SetItem) {
	switch value := item.Value.(type) {
	case nil:
		return
	case *ast.MapLiteral:
		// Whole-map assignment: every entry is a property write in its own
		// right, and the target names the node, not a property.
		w.mapLiteral(value)
	default:
		if prop, ok := item.Target.(*ast.Property); ok {
			w.propertyValue(prop.Key, item.Value)
		}
	}
}

// pattern walks every path of a comma-separated pattern.
func (w *writeScan) pattern(p *ast.Pattern) {
	if p == nil {
		return
	}
	for _, path := range p.Paths {
		w.path(path)
	}
}

// path walks the inline property maps of every element along one path — both the
// nodes and the relationships, since a CREATE writes the properties of both.
func (w *writeScan) path(path *ast.PathPattern) {
	if path == nil {
		return
	}
	for el := path.Head; el != nil && !w.stopped; el = el.Next {
		if el.Node != nil {
			w.propertyMap(el.Node.Properties)
		}
		if el.Relationship != nil {
			w.propertyMap(el.Relationship.Properties)
		}
	}
}

// propertyMap walks an inline property map, which the grammar also admits as a
// parameter — a shape this check cannot see into and does not pretend to.
func (w *writeScan) propertyMap(e ast.Expression) {
	if m, ok := e.(*ast.MapLiteral); ok {
		w.mapLiteral(m)
	}
}

// mapLiteral walks each entry of a map literal as one property write.
func (w *writeScan) mapLiteral(m *ast.MapLiteral) {
	for i, key := range m.Keys {
		if w.stopped || i >= len(m.Values) {
			return
		}
		w.propertyValue(key, m.Values[i])
	}
}

// propertyValue reports the literal string values reachable in one property
// position.
//
// A bare literal is the value itself. A LIST literal is descended into, because
// a list of strings is stored element by element and each element is a stored
// value. A concatenation is descended into on both sides, which decides the
// result for the reason the file comment gives: both rules are closed under
// concatenation. Every other expression shape is computed at execution time and
// is outside what this check can see.
func (w *writeScan) propertyValue(key string, e ast.Expression) {
	if w.stopped {
		return
	}
	switch t := e.(type) {
	case *ast.StringLiteral:
		w.emit(key, t)
	case *ast.ListLiteral:
		for _, elem := range t.Elements {
			w.propertyValue(key, elem)
		}
	case *ast.MapLiteral:
		// A nested map is stored as a map value; each entry keeps its own key.
		w.mapLiteral(t)
	case *ast.BinaryOp:
		w.propertyValue(key, t.Left)
		w.propertyValue(key, t.Right)
	}
}
