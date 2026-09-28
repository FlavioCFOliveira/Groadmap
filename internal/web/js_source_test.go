package web

import (
	"strings"
	"testing"
)

// This file holds the helpers the package's tests use to read the embedded
// JavaScript sources and a few small value helpers shared across test files.

// derefString returns a *string's value, or "" when it is nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// scriptBlock returns the text between the delimiters of a named declaration.
func scriptBlock(t *testing.T, script, name, open, close string) string {
	t.Helper()

	at := strings.Index(script, "var "+name+" = "+open)
	if at < 0 {
		t.Fatalf("the script declares no %s table", name)
	}
	rest := script[at:]
	end := strings.Index(rest, close)
	if end < 0 {
		t.Fatalf("the script's %s table is not closed", name)
	}
	return rest[:end]
}

// stripJSComments removes // and /* */ comments from JavaScript source, leaving
// string literals intact, so a scan for a forbidden construct measures the code
// rather than the prose that describes it.
func stripJSComments(src string) string {
	var out strings.Builder
	out.Grow(len(src))

	const (
		code = iota
		lineComment
		blockComment
		doubleQuoted
		singleQuoted
	)
	state := code

	for i := 0; i < len(src); i++ {
		c := src[i]
		switch state {
		case code:
			switch {
			case c == '/' && i+1 < len(src) && src[i+1] == '/':
				state = lineComment
				i++
			case c == '/' && i+1 < len(src) && src[i+1] == '*':
				state = blockComment
				i++
			case c == '"':
				state = doubleQuoted
				out.WriteByte(c)
			case c == '\'':
				state = singleQuoted
				out.WriteByte(c)
			default:
				out.WriteByte(c)
			}
		case lineComment:
			if c == '\n' {
				state = code
				out.WriteByte(c)
			}
		case blockComment:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				state = code
				i++
			}
		case doubleQuoted, singleQuoted:
			out.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				out.WriteByte(src[i])
				continue
			}
			if (state == doubleQuoted && c == '"') || (state == singleQuoted && c == '\'') {
				state = code
			}
		}
	}
	return out.String()
}
