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
	"strings"

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
	//
	// EXACTLY ONE SPACE separates the two keywords, written as a literal space
	// and not as \s+ (SPEC/GRAPH.md § Keyword Spacing in a Schema-Introspection
	// Command). That is the engine's rule, not a stylistic one: GoGraph routes a
	// statement to its introspection parser by testing it against the literal
	// prefixes "SHOW INDEX" and "SHOW CONSTRAINT", each carrying one space, after
	// trimming leading whitespace and leading comments. A statement that misses
	// those prefixes by its spacing goes to the general Cypher grammar, which has
	// no SHOW production and rejects it.
	//
	// This matcher exists to ADMIT, so matching wider than the engine admits
	// statements the engine then refuses — which is what the guard rail must not
	// do, because the user then reads a parse diagnostic that lists every clause
	// keyword except SHOW and names the wrong problem. reDDL above stays
	// whitespace-tolerant for the mirror-image reason: it exists to REFUSE, so
	// matching wider only refuses more, which is fail-closed. Do NOT align the
	// two on the ground that they treat whitespace differently.
	reIntrospect = regexp.MustCompile(`(?i)\A\s*SHOW (?:INDEX(?:ES)?|CONSTRAINTS?)\b`)
	// reIntrospectAnySpacing is reIntrospect with the single space relaxed to
	// arbitrary whitespace, and it exists ONLY to recognise the statements
	// reIntrospect must no longer admit, so they can be REFUSED deliberately.
	//
	// Narrowing reIntrospect alone refuses nothing: a statement that stops
	// classifying as Introspect carries no writing and no DDL clause either, so
	// IsReadOnly admits it from its default branch as an ordinary read and it
	// still dies in the engine's parser under the same misleading diagnostic. The
	// refusal has to be a rule of its own, and this matcher is what that rule
	// tests (see introspectSpacing and IntrospectSpacingRejection).
	//
	// It captures the target keyword so the rejection message can name the
	// accepted spelling of the statement the user meant. The rest of the shape —
	// the \A anchor, the leading-whitespace tolerance, the \b terminator, and the
	// deliberately different plural spellings — is reIntrospect's and is
	// documented there.
	reIntrospectAnySpacing = regexp.MustCompile(`(?i)\A\s*SHOW\s+(INDEX(?:ES)?|CONSTRAINTS?)\b`)
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
// query's masked normalization (MaskLiterals). The fields are independent, with
// one documented exception: a single query may, for example, be both Write and
// DDL, but Introspect and IntrospectMisspaced are mutually exclusive because
// they answer the same question about the same statement.
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
	// IntrospectMisspaced is true when the masked query reads as a
	// schema-introspection command — the SHOW INDEX(ES) / SHOW CONSTRAINT(S)
	// family — but the separator between its two keywords is not the single space
	// the engine routes on, so the engine would refuse it at the parser
	// (SPEC/GRAPH.md § Keyword Spacing in a Schema-Introspection Command).
	// Introspect and IntrospectMisspaced are mutually exclusive: a statement is
	// either spelled the way the engine accepts or it is not.
	//
	// It is a field of its own, and not an absence of Introspect, for the same
	// reason Introspect is a field: without it such a statement classifies as
	// nothing at all and is admitted by omission — which is precisely the defect
	// this rule exists to close. The surfaces that ADMIT this class use it to
	// refuse the statement with the guard rail's own message before the engine
	// ever sees it; IsReadOnly uses it to answer read-only as a recognised
	// decision, because a SHOW statement reads the schema and writes nothing
	// whatever its spacing.
	IntrospectMisspaced bool
}

// Classify masks query's literals and reports the clause classes it contains.
// The returned Classes is computed on the masked normalization, never on the
// raw string, so keywords inside string literals, comments, or backtick
// identifiers do not affect classification (SPEC/GRAPH.md § Literal-Aware
// Normalization).
//
// Every discriminator is applied TWICE: to the masked text, and to the masked
// text uppercased with strings.ToUpper — the transformation the engine itself
// falls back to when it decides whether a statement is DDL. See
// upperFoldedKeywords for why matching the masked text alone is not enough.
func Classify(query string) Classes {
	masked := MaskLiterals(query)
	upper := upperFoldedKeywords(masked)
	matches := func(re *regexp.Regexp) bool {
		return re.MatchString(masked) || re.MatchString(upper)
	}
	_, misspaced := introspectSpacing(masked, upper)
	return Classes{
		Write:               cypher.QueryHasWritingClause(masked) || cypher.QueryHasWritingClause(upper),
		Create:              matches(reCreate),
		Mutate:              matches(reMutate),
		Delete:              matches(reDelete),
		DDL:                 matches(reDDL),
		Introspect:          matches(reIntrospect),
		IntrospectMisspaced: misspaced,
	}
}

// introspectSpacing decides, from a query's masked normalization and its
// upper-folded copy, whether the query reads as a schema-introspection command
// whose keyword spacing the engine does not accept, and returns the accepted
// spelling of the command the user meant ("SHOW INDEXES", "SHOW CONSTRAINT", …).
//
// It is the single implementation behind both Classes.IntrospectMisspaced and
// IntrospectSpacingRejection, so the classification and the message a caller
// prints for it can never disagree about the same query.
//
// Both matchers are applied to the masked text AND to its upper-folded copy, for
// the reason upperFoldedKeywords documents: the engine falls back to
// strings.ToUpper the moment a non-ASCII byte appears in the prefix window, and
// Unicode uppercasing maps some non-ASCII letters onto ASCII ones. The accepted
// spelling is built by uppercasing the captured keyword rather than by echoing
// the query, so the returned text carries only the four ASCII keywords and never
// a byte of user input.
func introspectSpacing(masked, upper string) (accepted string, misspaced bool) {
	if reIntrospect.MatchString(masked) || reIntrospect.MatchString(upper) {
		// Spelled the way the engine routes to its introspection parser. There
		// is nothing to refuse; Classify reports it as Introspect.
		return "", false
	}
	m := reIntrospectAnySpacing.FindStringSubmatch(masked)
	if m == nil {
		m = reIntrospectAnySpacing.FindStringSubmatch(upper)
	}
	if m == nil {
		// Not a schema-introspection command under any spacing. A SHOW family
		// the engine does not implement (SHOW DATABASES) and a near miss on the
		// keyword (SHOW INDEXER) both land here, and both keep reaching the
		// engine, which names the real problem for them.
		return "", false
	}
	return "SHOW " + strings.ToUpper(m[1]), true
}

// IntrospectSpacingRejection reports whether query is a schema-introspection
// command whose keyword spacing the engine does not accept and, when it is,
// returns the guard rail's own explanation for refusing it. reason is empty when
// misspaced is false.
//
// It is the rejection rule the three CLI surfaces that ADMIT this class apply on
// top of the read-only contract — `graph query`, `graph search` and
// `graph update`, each of which wraps reason in utils.ErrValidation (exit code
// 6). It lives here, beside the classification it reads, so those surfaces
// cannot drift apart on which statements are refused or on what the user is
// told.
//
// The read-only web graph data endpoint does NOT call it. That endpoint refuses
// the schema-introspection class outright, at every spacing, so the spacing is
// not what is wrong there and naming it would prescribe a correction that does
// not work; it answers the single failure class schema_introspection instead.
// The two surfaces share the CLASSIFICATION above and differ only in the
// verdict, because a surface that answers the class owes the caller a working
// spelling while a surface that refuses it does not (SPEC/GRAPH.md § Keyword
// Spacing in a Schema-Introspection Command, which is canonical for the
// divergence; SPEC/WEB.md § Query-Bar Error Handling, case 10).
//
// The message names the cause the SPEC requires it to name: that the statement
// was read as a schema-introspection command, that the engine recognises it only
// with exactly one space between the two keywords, and what the accepted
// spelling is. It also states that the command writes nothing, because the
// objection is the spelling alone and a reader must not take the refusal for a
// verdict that the query is not read-only.
//
// The statement is refused, never repaired: Groadmap does not rewrite the
// separator and execute the amended query, because that would silently alter
// what the user wrote and would make Groadmap the party deciding which Cypher
// the engine ought to accept.
func IntrospectSpacingRejection(query string) (reason string, misspaced bool) {
	masked := MaskLiterals(query)
	accepted, bad := introspectSpacing(masked, upperFoldedKeywords(masked))
	if !bad {
		return "", false
	}
	return accepted + " is a schema-introspection command the engine recognises only with exactly " +
		"one space between its two keywords, and this query separates them with something else. " +
		"Rewrite it as \"" + accepted + "\". The command reads the schema and writes nothing, so its " +
		"keyword spacing is the whole of the objection.", true
}

// upperFoldedKeywords returns masked uppercased with strings.ToUpper, the exact
// transformation the engine's DDL dispatcher applies before it decides whether
// a statement is DDL.
//
// # Why the masked text alone is not enough (security-critical)
//
// The engine routes a statement to its DDL executor when cypher/ir.IsDDL is
// true. IsDDL compares the statement against the "CREATE INDEX" / "DROP INDEX" /
// "CREATE CONSTRAINT" / "DROP CONSTRAINT" prefixes byte-wise while folding ASCII
// case, but the moment a NON-ASCII byte appears inside the prefix window it
// falls back to strings.HasPrefix(strings.ToUpper(stmt), prefix) — and Unicode
// uppercasing maps some non-ASCII letters ONTO ASCII ones. U+0131 (dotless i)
// uppercases to 'I', so the engine reads "CREATE ıNDEX …" as CREATE INDEX and
// executes it through cypher/ir.ParseDDL, which uppercases the same way.
//
// Go's (?i) flag is not that transformation: it applies Unicode simple case
// FOLDING, whose orbit for 'I'/'i' does not contain U+0131. So `(?i)INDEX` does
// NOT match "ıNDEX", and the guard rail classified such a statement as an
// ordinary read while the engine executed it as schema DDL — a fail-OPEN
// divergence reachable from `rmp graph query`/`search` and, worse, from an
// unauthenticated GET on the read-only web graph data endpoint.
//
// Matching the uppercased copy as well makes the guard rail see every keyword
// the engine can see, because any code point the engine's fallback folds onto an
// ASCII keyword letter is folded the same way here. It can only ADD detections
// (strings.ToUpper never turns an ASCII keyword letter into something else), so
// no query that was correctly admitted before is rejected now: the extra matches
// are exactly the spoofed keywords the engine would have acted on.
func upperFoldedKeywords(masked string) string {
	return strings.ToUpper(masked)
}

// IsReadOnly reports whether query is read-only: it contains neither a writing
// clause (cypher.QueryHasWritingClause on the masked query) nor any
// schema-mutating DDL clause. Two shapes qualify — an ordinary reading query,
// and a schema-introspection command, which lists the registered schema without
// altering it. A schema-introspection command qualifies at ANY keyword spacing:
// the spacing rule decides whether the engine will parse it, not whether it
// writes, and it is enforced separately by the surfaces that admit the class
// (see IntrospectSpacingRejection).
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
	case c.IntrospectMisspaced:
		// A schema-introspection command the engine's spacing rule refuses is
		// STILL read-only: it lists the registered schema and alters nothing,
		// whatever separates its two keywords. Answering false here would
		// publish a classification the guard rail's own rejection message
		// contradicts, and would tell a caller the query writes when it does
		// not. The spacing objection is a separate contract, applied on top of
		// this one by the surfaces that admit the class — which is also why a
		// query that writes AND carries a badly spaced SHOW is rejected as a
		// write above, before this case is reached (SPEC/GRAPH.md § Keyword
		// Spacing in a Schema-Introspection Command; SPEC/WEB.md § Query-Bar
		// Error Handling, rule 6).
		return true
	default:
		// An ordinary reading query: no writing clause, no DDL, no introspection.
		return true
	}
}
