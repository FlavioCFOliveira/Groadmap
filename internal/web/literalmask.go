package web

// maskLiterals returns a copy of query in which the INTERIOR characters of
// Cypher string literals, comments, and backtick-quoted identifiers are
// replaced with spaces. Delimiter characters and the overall length are
// preserved so that byte positions of every other token are unchanged; only the
// neutralized spans differ.
//
// It exists for one reason: the node-limit injection this package performs must
// decide whether a query already carries a top-level LIMIT, and whether it is a
// statement form that can carry one at all. Those are presence checks over
// clause keywords, and a LIMIT, RETURN, CALL, or SHOW keyword that appears only
// inside a string literal, a comment, or a backtick identifier must not
// influence them (SPEC/WEB.md § Graph Data Endpoint, node-limit injection;
// SPEC/GRAPH.md § Literal-Aware Normalization). The query actually executed
// against the store is always the original, unmodified string; masking affects
// the injection decision only, never the statement.
//
// It refuses nothing. Its answer decides whether five characters are appended
// to a statement, not whether the statement runs.
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
func maskLiterals(query string) string {
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
				// Enter line comment; the // marker is non-structural for the
				// injection decision, so mask it together with the comment body.
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
