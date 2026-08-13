// Package cypherguard is the single source of truth for Groadmap's Cypher
// guard-rail: the literal-aware masking and the clause-class classification
// that decide whether a query is read-only, a writing query, schema-mutating
// DDL, or a schema-introspection command (SPEC/GRAPH.md § Subcommands and
// Guard-Rail Validation, § Schema Introspection, and
// § Literal-Aware Normalization).
//
// The logic lives here, rather than in package commands, because two callers
// must share the exact same guard rail without one depending on the other:
//   - the CLI graph subcommands (package commands) classify a query against its
//     subcommand's accepted operation class;
//   - the read-only web graph data endpoint (package web) validates a
//     user-supplied query as read-only before executing it.
//
// The dependency direction in the codebase runs commands -> web, so the web
// package cannot import commands. Factoring the security-critical guard rail
// into this leaf package (it imports only the GoGraph cypher engine and the
// standard library) lets both import it, so the CLI and the web interface can
// never drift apart on what counts as a write — a duplication of this logic
// would be a security hazard, since a divergence could let a write slip through
// one path that the other rejects.
package cypherguard

import (
	"regexp"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
)

// Writing-clause and DDL discriminators. These are applied to the literal-masked
// normalization of a query (see MaskLiterals), so a keyword that appears only
// inside a string literal, comment, or backtick identifier cannot trip them.
var (
	// reCreate matches the creating writing clauses CREATE and MERGE.
	reCreate = regexp.MustCompile(`(?i)\b(CREATE|MERGE)\b`)
	// reMutate matches the mutating writing clauses SET and REMOVE.
	reMutate = regexp.MustCompile(`(?i)\b(SET|REMOVE)\b`)
	// reDelete matches the deleting writing clauses DELETE and DETACH DELETE.
	reDelete = regexp.MustCompile(`(?i)\b(DELETE|DETACH)\b`)
	// reDDL matches the schema-mutating DDL clauses (SPEC/GRAPH.md
	// § Operation Classes): CREATE INDEX, DROP INDEX, CREATE CONSTRAINT,
	// DROP CONSTRAINT. The CREATE/DROP keyword and the INDEX/CONSTRAINT keyword
	// may be separated by arbitrary whitespace, so the matcher is
	// whitespace-tolerant (\s+ between the two words).
	//
	// GoGraph exports cypher/ir.IsDDL, but it still cannot serve as a security
	// guard rail, because it compares the statement against the literal prefixes
	// "CREATE INDEX", "DROP INDEX", "CREATE CONSTRAINT" and "DROP CONSTRAINT",
	// each carrying exactly one space. Any other spacing between the two keywords
	// — "CREATE   INDEX", or a keyword pair split across lines — is not a prefix
	// of those literals, so IsDDL returns false and a writer could present
	// schema-mutating DDL that the engine executes and the check does not see.
	// (Its other two historical limitations are gone: it now folds ASCII case, so
	// "create index" is recognised, and it skips leading comments before testing
	// the prefix. Only the whitespace gap remains, and one gap is enough to
	// disqualify it here.) This Groadmap-local regex mirrors the writing-clause
	// discriminators and is tolerant of case and of arbitrary whitespace,
	// matching how the guard rail classifies every other clause.
	reDDL = regexp.MustCompile(`(?i)\b(CREATE|DROP)\s+(INDEX|CONSTRAINT)\b`)
	// reIntrospect matches the schema-introspection commands Groadmap admits as
	// read-only: SHOW INDEXES / SHOW INDEX / SHOW CONSTRAINTS / SHOW CONSTRAINT,
	// each optionally followed by a YIELD / WHERE / RETURN projection tail
	// (SPEC/GRAPH.md § Schema Introspection).
	//
	// It is anchored at the start of the statement (\A), because SHOW introduces
	// a statement and is not a clause that may appear anywhere in one: anchoring
	// is what keeps a label, variable, or property named "show" — as in
	// CREATE (n:Panel {show: 'indexes'}) — from being read as an introspection
	// command. Leading whitespace is allowed, which also covers a leading
	// comment: MaskLiterals neutralizes a comment to spaces before this runs.
	//
	// The two plurals are spelled differently on purpose: INDEX pluralises to
	// INDEXES, so the optional part is the two-letter "ES", whereas CONSTRAINT
	// takes a bare "S". Writing the first as INDEXES? would match INDEXE and
	// INDEXES and silently NOT match the singular SHOW INDEX.
	reIntrospect = regexp.MustCompile(`(?i)\A\s*SHOW\s+(?:INDEX(?:ES)?|CONSTRAINTS?)\b`)
)

// MaskLiterals returns a copy of query in which the INTERIOR characters of
// Cypher string literals, comments, and backtick-quoted identifiers are
// replaced with spaces. Delimiter characters and the overall length are
// preserved so that byte positions of every other token are unchanged; only the
// neutralized spans differ.
//
// It is used solely for operation-class classification (SPEC/GRAPH.md
// § Literal-Aware Normalization): a clause keyword appearing only inside a
// string literal, a comment, or a backtick identifier must not influence the
// guard rail. The query actually executed against the store is always the
// original, unmodified string; masking affects classification only.
//
// The scanner is a single left-to-right state machine so that nesting and
// precedence are handled correctly: a quote inside a comment does not open a
// literal, a comment marker inside a string is literal text, and a backslash
// escape inside a quoted span does not terminate that span.
//
// Masked spans (interior neutralized to spaces, delimiters kept):
//   - single-quoted string literals  '...'   (honors \\, \', \" escapes)
//   - double-quoted string literals  "..."   (honors \\, \', \" escapes)
//   - backtick-quoted identifiers    `...`
//   - line comments                  // ... <EOL>
//   - block comments                 /* ... */
func MaskLiterals(query string) string {
	const space = ' '
	b := []byte(query)
	n := len(b)
	out := make([]byte, n)
	copy(out, b)

	// Scanner state. Exactly one of these is active at a time.
	type state int
	const (
		stNormal   state = iota
		stSingle         // inside '...'
		stDouble         // inside "..."
		stBacktick       // inside `...`
		stLine           // inside // ... EOL
		stBlock          // inside /* ... */
	)

	st := stNormal
	for i := 0; i < n; i++ {
		c := b[i]
		switch st {
		case stNormal:
			switch {
			case c == '\'':
				st = stSingle
			case c == '"':
				st = stDouble
			case c == '`':
				st = stBacktick
			case c == '/' && i+1 < n && b[i+1] == '/':
				// Enter line comment; the // marker is non-structural for
				// classification, so mask it together with the comment body.
				st = stLine
				out[i] = space
				out[i+1] = space
				i++
			case c == '/' && i+1 < n && b[i+1] == '*':
				// Enter block comment; mask the /* marker with the body.
				st = stBlock
				out[i] = space
				out[i+1] = space
				i++
			}
		case stSingle, stDouble:
			// Backslash escapes a following character within quoted literals so
			// an escaped quote does not close the literal. Mask both the
			// backslash and the escaped character.
			if c == '\\' && i+1 < n {
				out[i] = space
				out[i+1] = space
				i++
				continue
			}
			if (st == stSingle && c == '\'') || (st == stDouble && c == '"') {
				st = stNormal // delimiter preserved
				continue
			}
			out[i] = space
		case stBacktick:
			// Backtick identifiers do not process backslash escapes in Cypher.
			if c == '`' {
				st = stNormal // delimiter preserved
				continue
			}
			out[i] = space
		case stLine:
			if c == '\n' {
				st = stNormal // newline preserved as structure
				continue
			}
			out[i] = space
		case stBlock:
			if c == '*' && i+1 < n && b[i+1] == '/' {
				// Mask the closing */ marker and return to normal.
				out[i] = space
				out[i+1] = space
				st = stNormal
				i++
				continue
			}
			out[i] = space
		}
	}

	return string(out)
}

// Classes reports which clause classes a query contains, evaluated on the
// query's masked normalization (MaskLiterals). The four fields are independent:
// a single query may, for example, be both Write and DDL.
//
// Callers build a Classes from a query with Classify and then apply their own
// per-subcommand acceptance rules, so this struct is the shared classification
// primitive that keeps the CLI guard rail and the web read-only check in lockstep.
type Classes struct {
	// Write is cypher.QueryHasWritingClause on the masked query: true when the
	// query contains any writing clause (CREATE, MERGE, SET, REMOVE, DELETE, or
	// DETACH DELETE).
	Write bool
	// Create is true when the masked query contains a CREATE or MERGE clause.
	Create bool
	// Mutate is true when the masked query contains a SET or REMOVE clause.
	Mutate bool
	// Delete is true when the masked query contains a DELETE or DETACH (DELETE)
	// clause.
	Delete bool
	// DDL is true when the masked query contains a schema-mutating DDL clause
	// (CREATE INDEX, DROP INDEX, CREATE CONSTRAINT, DROP CONSTRAINT). DDL is
	// detected independently of Write because the two-word CREATE INDEX /
	// CREATE CONSTRAINT forms would otherwise look like a CREATE write, and
	// because the read-only contract forbids schema-mutating DDL, not only
	// data-writing clauses (SPEC/GRAPH.md § Operation Classes).
	DDL bool
	// Introspect is true when the masked query is a schema-introspection command
	// (SHOW INDEXES, SHOW INDEX, SHOW CONSTRAINTS, SHOW CONSTRAINT, with an
	// optional YIELD / WHERE / RETURN projection tail). Introspection lists the
	// registered schema without altering it, so it is read-only, and it is a
	// class of its own rather than a case of DDL: DDL here means schema-MUTATING
	// (SPEC/GRAPH.md § Schema Introspection).
	//
	// The field exists so the read-only verdict for these commands is a
	// recognised decision rather than the result of no discriminator matching.
	// The engine reports introspection through its own cypher/ir.IsDDL predicate,
	// which folds SHOW in with the schema-mutating forms; because
	// cypher.QueryHasWritingClause returns false for whatever IsDDL accepts, an
	// introspection command reaches Classify as neither a write nor Groadmap's
	// DDL, and would otherwise be admitted purely by omission — a verdict that
	// cannot be reviewed and that silently absorbs whatever clause family the
	// engine gains next.
	Introspect bool
}

// Classify masks query's literals and reports the clause classes it contains.
// The returned Classes is computed on the masked normalization, never on the
// raw string, so keywords inside string literals, comments, or backtick
// identifiers do not affect classification (SPEC/GRAPH.md § Literal-Aware
// Normalization).
func Classify(query string) Classes {
	masked := MaskLiterals(query)
	return Classes{
		Write:      cypher.QueryHasWritingClause(masked),
		Create:     reCreate.MatchString(masked),
		Mutate:     reMutate.MatchString(masked),
		Delete:     reDelete.MatchString(masked),
		DDL:        reDDL.MatchString(masked),
		Introspect: reIntrospect.MatchString(masked),
	}
}

// IsReadOnly reports whether query is read-only: it contains neither a writing
// clause (cypher.QueryHasWritingClause on the masked query) nor any
// schema-mutating DDL clause. Two shapes qualify — an ordinary reading query,
// and a schema-introspection command, which lists the registered schema without
// altering it.
//
// This is the exact read-vs-write contract the read subcommands `graph query`
// and `graph search` enforce, and the contract the read-only web graph data
// endpoint reuses to validate a user-supplied query before executing it
// (SPEC/GRAPH.md § Per-Subcommand Validation Rules notes 5 and 6; SPEC/WEB.md
// § Graph Data Endpoint, read-only guard-rail). Classification runs on the
// masked normalization, so a write, DDL, or SHOW keyword that appears only
// inside a string literal, comment, or backtick identifier does not affect the
// verdict, and a real writing or DDL clause is always caught.
func IsReadOnly(query string) bool {
	c := Classify(query)
	switch {
	case c.Write, c.DDL:
		// A data-writing clause or a schema-mutating DDL clause disqualifies the
		// query outright, whatever else it contains.
		return false
	case c.Introspect:
		// Schema introspection is read-only because it was classified as such,
		// not because nothing else matched (SPEC/GRAPH.md § Schema Introspection).
		return true
	default:
		// An ordinary reading query: no writing clause, no DDL, no introspection.
		return true
	}
}
