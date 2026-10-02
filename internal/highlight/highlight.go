// Package highlight holds the lexer registry and the style that highlight a
// fenced code block of rendered Markdown (SPEC/WEB.md § Markdown Rendering,
// rule 6).
//
// The registry is a copy of every lexer the chroma version go.mod pins ships,
// generated from that module's source by registry_gen.go and committed: the
// lexers chroma defines in its embedded XML definitions (embedded/) and those it
// defines in Go (generated_*.go), registered in the order chroma's own global
// registry registers them, together with chroma's github-dark style
// (styles/). The generated Go source is chroma's own, rewritten so that no
// package-level initialiser constructs anything: initialising this package
// parses no lexer or style definition, compiles no regular expression, and
// constructs no lexer. The registry and the style are built once, on the first
// call of ResolveLexer, HighlightStyle, or BuiltRegistry, safely under
// concurrent callers, and are read-only from then on.
//
// This is why the rmp binary does not import chroma's lexers and styles
// packages, each of which builds its whole registry in a package-level
// initialiser that every invocation would pay (SPEC/BUILD.md § Markdown
// Rendering Rules, rules 5 and 6; SPEC/IMPLEMENTATION.md § Performance
// Considerations, item 9).
package highlight

//go:generate go run registry_gen.go

import (
	"sync"
	"sync/atomic"

	"github.com/alecthomas/chroma/v2"
)

// StyleName is the name of the one style the package carries, chroma's
// github-dark: the style the syntax-highlighting stylesheet is generated from,
// against which highlighted HTML is emitted (SPEC/WEB.md § Markdown Rendering,
// rule 7).
const StyleName = generatedStyleName

var (
	// highlightBuildOnce guards the one construction of the registry and style.
	highlightBuildOnce sync.Once
	// highlightBuilt reports that the construction has completed.
	highlightBuilt atomic.Bool
	// highlightBuilds counts constructions; it is never more than one.
	highlightBuilds atomic.Int32
	// highlightStyle is the style, set by the construction.
	highlightStyle *chroma.Style
)

// ensureBuilt builds the registry and the style on its first call and does
// nothing on every later one. A call that races the first waits for it.
func ensureBuilt() {
	highlightBuildOnce.Do(func() {
		highlightBuilds.Add(1)
		generatedBuildLexers()
		highlightStyle = generatedNewStyle()
		highlightBuilt.Store(true)
	})
}

// ResolveLexer returns the lexer the registry resolves name to, or nil when it
// resolves none. The lookup is chroma's: a lexer's name, a lexer's alias, the
// name in lower case as a name and then as an alias, and finally the file-name
// patterns of every lexer matched against "filename.<name>" and "<name>", the
// match of highest priority winning. No lexer is guessed from content and no
// fallback lexer is returned.
func ResolveLexer(name string) chroma.Lexer {
	ensureBuilt()
	return GlobalLexerRegistry.Get(name)
}

// HighlightStyle returns chroma's github-dark style.
func HighlightStyle() *chroma.Style {
	ensureBuilt()
	return highlightStyle
}

// BuiltRegistry returns the lexer registry, building it first when it has not
// been built. The registry is shared and read-only: a caller MUST NOT register
// into it.
func BuiltRegistry() *chroma.LexerRegistry {
	ensureBuilt()
	return GlobalLexerRegistry
}

// IsBuilt reports whether the registry and the style have been built. It never
// builds them.
func IsBuilt() bool {
	return highlightBuilt.Load()
}
