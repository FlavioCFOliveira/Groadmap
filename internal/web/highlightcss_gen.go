//go:build ignore

// Command highlightcss_gen writes static/highlight.css, the one
// syntax-highlighting stylesheet of rendered Markdown: the CSS the pinned chroma
// module produces, in class-based form, for its github-dark style, with no rule
// scoped by a theme selector (SPEC/WEB.md § Markdown Rendering, rule 7).
//
// Run it with `go generate ./internal/web/` and commit the result: the stylesheet
// is a generated but COMMITTED artefact, embedded like every other static asset,
// so `go build` stays a plain Go build with no code-generation step. What keeps it
// equal to the pinned chroma is not this command but the test gate
// TestHighlightCSS_EqualsPinnedChromaOutput (markdown_test.go), which regenerates
// the CSS in memory and fails when the committed file differs — so a chroma
// upgrade that changes the CSS cannot ship without regenerating it (SPEC/BUILD.md
// § Markdown Rendering Rules, rule 4).
//
// The generation below MUST stay identical to highlightCSS in markdown_test.go.
package main

import (
	"bytes"
	"fmt"
	"os"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
)

func main() {
	var buf bytes.Buffer
	if err := chromahtml.New(chromahtml.WithClasses(true)).WriteCSS(&buf, styles.Get("github-dark")); err != nil {
		fmt.Fprintln(os.Stderr, "highlightcss_gen:", err)
		os.Exit(1)
	}
	if err := os.WriteFile("static/highlight.css", buf.Bytes(), 0o644); err != nil { // a committed, world-readable source asset
		fmt.Fprintln(os.Stderr, "highlightcss_gen:", err)
		os.Exit(1)
	}
}
