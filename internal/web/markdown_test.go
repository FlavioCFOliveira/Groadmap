package web

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for SPEC/WEB.md § Markdown Rendering and its Acceptance
// Criteria 180 to 194, plus the rewritten criteria 32, 65, 72, 73, 94, and 97
// where they are about the Markdown fields. The renderer is asserted directly
// through renderMarkdown, and every surface is asserted through the served page
// or the served JSON, so a surface that still escaped its field as plain text
// would fail on that surface alone.

// md renders source in the ordinary form with a fixed prefix, failing the test on
// a render error.
func md(t *testing.T, source string) string {
	t.Helper()
	return mdForm(t, source, "task-42-acceptance_criteria-", markdownInteractive)
}

func mdForm(t *testing.T, source, prefix string, form markdownForm) string {
	t.Helper()
	out, err := renderMarkdown(source, prefix, form)
	if err != nil {
		t.Fatalf("renderMarkdown(%q): %v", source, err)
	}
	return out
}

// Attribute and element patterns the assertions scan rendered HTML with.
var (
	styleAttrRe   = regexp.MustCompile(`(?i)\sstyle\s*=`)
	eventAttrRe   = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	idAttrRe      = regexp.MustCompile(`\sid="([^"]*)"`)
	fragmentRe    = regexp.MustCompile(`\shref="#([^"]*)"`)
	anchorOpenRe  = regexp.MustCompile(`<a\s`)
	headingOpenRe = regexp.MustCompile(`<h([1-6])([^>]*)>`)
)

// ==================== THE RENDERER ====================

// TestMarkdown_ParagraphsLineBreaksAndInlineMarkup is Acceptance Criterion 180 at
// the renderer and rule 4: bold, a bulleted list, and a single newline render as
// <strong>, <ul>/<li>, and <br>, and plain text renders as paragraphs.
func TestMarkdown_ParagraphsLineBreaksAndInlineMarkup(t *testing.T) {
	out := md(t, "Settle the **ledger** first.\nThen reconcile.\n\n- export the day\n- compare the totals")
	for _, want := range []string{
		"<strong>ledger</strong>",
		"first.<br>\nThen reconcile.",
		"<ul>\n<li>export the day</li>\n<li>compare the totals</li>\n</ul>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered HTML lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "**") {
		t.Errorf("the literal asterisks are displayed:\n%s", out)
	}

	plain := md(t, "The first paragraph line.\nIts second line.\n\nThe second paragraph.")
	want := "<p>The first paragraph line.<br>\nIts second line.</p>\n<p>The second paragraph.</p>\n"
	if plain != want {
		t.Errorf("plain text renders as\n%q\nwant\n%q", plain, want)
	}

	if got := md(t, ""); got != "" {
		t.Errorf("the empty string renders as %q, want the empty string", got)
	}
}

// TestMarkdown_GFMFootnotesAndDefinitionLists is Acceptance Criterion 181.
func TestMarkdown_GFMFootnotesAndDefinitionLists(t *testing.T) {
	src := strings.Join([]string{
		"| Window | Residual | Owner |",
		"|:-------|:--------:|------:|",
		"| 09:00  | 0.00     | ops   |",
		"",
		"The ~~struck~~ plan, see https://example.org/runbook and www.example.org.",
		"",
		"- [x] export the settlement day",
		"- [ ] compare the totals",
		"",
		"The drift is one cent[^drift].",
		"",
		"[^drift]: Rounding happens twice.",
		"",
		"Residual",
		": The difference left after reconciliation.",
	}, "\n")
	out := md(t, src)

	for _, want := range []string{
		"<table>", "<thead>", "<tbody>",
		`<th class="text-start">Window</th>`, `<th class="text-center">Residual</th>`, `<th class="text-end">Owner</th>`,
		`<td class="text-center">0.00</td>`,
		"<del>struck</del>",
		`<a href="https://example.org/runbook" target="_blank" rel="noopener noreferrer">https://example.org/runbook</a>`,
		`<a href="http://www.example.org" target="_blank" rel="noopener noreferrer">www.example.org</a>`,
		`<li><input checked="" disabled="" type="checkbox"> export the settlement day</li>`,
		`<li><input disabled="" type="checkbox"> compare the totals</li>`,
		`class="footnote-ref"`, `<div class="footnotes" role="doc-endnotes">`,
		"<dl>", "<dt>Residual</dt>", "<dd>The difference left after reconciliation.</dd>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered HTML lacks %q:\n%s", want, out)
		}
	}
	if got := strings.Count(out, `<input `); got != 2 {
		t.Errorf("%d checkboxes, want 2", got)
	}
	if got := strings.Count(out, `disabled=""`); got != 2 {
		t.Errorf("%d disabled checkboxes, want both", got)
	}
	if got := strings.Count(out, `checked=""`); got != 1 {
		t.Errorf("%d checked checkboxes, want exactly the checked item's", got)
	}
	if styleAttrRe.MatchString(out) {
		t.Errorf("an element carries a style attribute:\n%s", out)
	}
}

// TestMarkdown_FencedCodeHighlightedByDeclaredLanguageOnly is Acceptance
// Criterion 182 at the renderer, and rules 3 and 6: the declared language drives
// highlighting, nothing is guessed, and no attribute block in the info string
// reaches the highlighter.
func TestMarkdown_FencedCodeHighlightedByDeclaredLanguageOnly(t *testing.T) {
	goBlock := md(t, "```go\npackage settlement\n\nfunc Residual() int { return 0 }\n```")
	for _, want := range []string{`<pre class="chroma">`, `<span class="kn">package</span>`, `<span class="kd">func</span>`} {
		if !strings.Contains(goBlock, want) {
			t.Errorf("the go block lacks %q:\n%s", want, goBlock)
		}
	}
	if styleAttrRe.MatchString(goBlock) {
		t.Errorf("the highlighted block carries a style attribute:\n%s", goBlock)
	}

	const goSource = "package settlement\n\nfunc Residual() int { return 0 }\n"
	for name, src := range map[string]string{
		"no info string":        "```\n" + goSource + "```",
		"unrecognised language": "```ledgerscript\n" + goSource + "```",
		"brace in the language": "```go{linenos=true}\n" + goSource + "```",
		"indented code block":   "    package settlement\n\n    func Residual() int { return 0 }",
	} {
		out := md(t, src)
		if !strings.HasPrefix(out, "<pre><code>package settlement") {
			t.Errorf("%s: want an unhighlighted <pre><code> with no class, got:\n%s", name, out)
		}
		if strings.Contains(out, "class=") {
			t.Errorf("%s: the unhighlighted block carries a class (a token class or the "+
				"author's language name):\n%s", name, out)
		}
	}

	// An attribute block after the language is not read: no line numbers, no
	// highlighted line, no other style — the block renders exactly as the bare
	// language does.
	withAttrs := md(t, "```go {linenos=table hl_lines=[1]}\n"+goSource+"```")
	if bare := md(t, "```go\n"+goSource+"```"); withAttrs != bare {
		t.Errorf("an attribute block in the info string changed the rendering:\n%s\nwant\n%s", withAttrs, bare)
	}
}

// TestMarkdown_RawHTMLNeverEmitted is Acceptance Criterion 183 at the renderer,
// and rules 10 and 11.
func TestMarkdown_RawHTMLNeverEmitted(t *testing.T) {
	out := md(t, hostileMarkdown)
	assertNoAuthorMarkup(t, "renderer", out)
	if !strings.Contains(out, "<code>&lt;script&gt;</code>") {
		t.Errorf("a < inside a code span is not escaped:\n%s", out)
	}
	if !strings.Contains(out, "Totals &amp; residuals") {
		t.Errorf("an & in text is not escaped:\n%s", out)
	}
}

// hostileMarkdown carries every raw-HTML form Acceptance Criterion 183 names, an
// attempt at the attribute syntax, and the escaping cases.
const hostileMarkdown = "Before <script>alert(1)</script> the table.\n\n" +
	"<img src=x onerror=alert(1)>\n\n" +
	"<iframe src=\"https://example.org/\"></iframe>\n\n" +
	"An inline <b onclick=alert(1)>bold</b> tag and the code `<script>`.\n\n" +
	"Totals & residuals.\n\n" +
	"# Heading {#injected .danger onclick=alert(1)}\n\n" +
	"<div style=\"position:fixed\">overlay</div>"

// assertNoAuthorMarkup fails when rendered HTML carries any element or attribute
// the hostile source tried to introduce. It reads the tags themselves, so the
// source's text — which the renderer rightly keeps as text — is not mistaken for
// markup.
func assertNoAuthorMarkup(t *testing.T, label, out string) {
	t.Helper()
	for _, m := range tagRe.FindAllStringSubmatch(out, -1) {
		name, attrs := strings.ToLower(m[1]), m[2]
		switch {
		case slices.Contains([]string{"script", "img", "iframe", "b", "style", "object", "embed"}, name):
			t.Errorf("%s: the author's raw element <%s%s> reached the output", label, name, attrs)
		case name == "div" && attrs != ` class="footnotes" role="doc-endnotes"`:
			t.Errorf("%s: a <div%s> the renderer does not emit reached the output", label, attrs)
		}
		if eventAttrRe.MatchString(attrs) || styleAttrRe.MatchString(attrs) {
			t.Errorf("%s: <%s%s> carries an event-handler or style attribute", label, name, attrs)
		}
		if strings.Contains(attrs, "injected") || strings.Contains(attrs, "danger") {
			t.Errorf("%s: <%s%s> carries an attribute the author wrote", label, name, attrs)
		}
	}
}

// tagRe matches a start tag, capturing its name and its attribute text.
var tagRe = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9]*)([^>]*)>`)

// TestMarkdown_DangerousLinksNeverActive is Acceptance Criterion 184.
func TestMarkdown_DangerousLinksNeverActive(t *testing.T) {
	out := md(t, "[alpha](javascript:alert(1)) [bravo](JavaScript:alert(1)) [charlie](vbscript:msgbox) "+
		"[delta](file:///etc/passwd) <javascript:alert(1)> [echo](data:text/html,<b>x</b>)")
	want := "<p>alpha bravo charlie delta javascript:alert(1) echo</p>\n"
	if out != want {
		t.Errorf("dangerous links render as\n%q\nwant their text alone\n%q", out, want)
	}
	if strings.Contains(out, "href") {
		t.Errorf("a dangerous link carries an href (an empty href resolves to the current page):\n%s", out)
	}
}

// TestMarkdown_LinkTargets is Acceptance Criterion 185.
func TestMarkdown_LinkTargets(t *testing.T) {
	out := md(t, "[a](https://example.org/) [b](HTTP://example.org/) [c](//example.org/) https://example.org/\n\n"+
		"[d](/roadmaps/groadmap/tasks) [e](#notes) [f](mailto:maintainer@example.org)")
	for _, href := range []string{"https://example.org/", "HTTP://example.org/", "//example.org/"} {
		want := `<a href="` + href + `" target="_blank" rel="noopener noreferrer">`
		if !strings.Contains(out, want) {
			t.Errorf("the link to %s does not open in a new tab: want %q in\n%s", href, want, out)
		}
	}
	if got := strings.Count(out, `target="_blank" rel="noopener noreferrer"`); got != 4 {
		t.Errorf("%d links open in a new tab, want the 4 absolute http(s) and network-path ones", got)
	}
	for _, want := range []string{
		`<a href="/roadmaps/groadmap/tasks">d</a>`,
		`<a href="#notes">e</a>`,
		`<a href="mailto:maintainer@example.org">f</a>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("a same-tab link is not rendered as %q:\n%s", want, out)
		}
	}
}

// TestMarkdown_Images is Acceptance Criterion 186 at the renderer: only a data:
// raster image is an <img>, so a Markdown image never makes the browser issue a
// request.
func TestMarkdown_Images(t *testing.T) {
	const pixel = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	out := md(t, "![deployment diagram](https://example.org/diagram.png) ![](https://example.org/chart.png) "+
		"![favicon](/static/favicon.svg) ![pixel]("+pixel+") ![vector](data:image/svg+xml,%3Csvg%2F%3E) "+
		"![payload](javascript:alert(1))")
	for _, want := range []string{
		`<a href="https://example.org/diagram.png" target="_blank" rel="noopener noreferrer">deployment diagram</a>`,
		`<a href="https://example.org/chart.png" target="_blank" rel="noopener noreferrer">https://example.org/chart.png</a>`,
		`<a href="/static/favicon.svg">favicon</a>`,
		`<img src="` + pixel + `" alt="pixel">`,
		" vector ",
		" payload</p>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered images lack %q:\n%s", want, out)
		}
	}
	if got := strings.Count(out, "<img"); got != 1 {
		t.Errorf("%d <img> elements, want only the data: raster image", got)
	}
	for _, src := range regexp.MustCompile(`<img src="([^"]*)"`).FindAllStringSubmatch(out, -1) {
		if !strings.HasPrefix(src[1], "data:image/png;") {
			t.Errorf("an <img> requests %q", src[1])
		}
	}

	// An image inside an active link is already linked by it, and an <a> nested
	// in an <a> is invalid HTML, so the image is its alternative text there.
	badge := md(t, "[![build status](https://example.org/badge.png)](https://example.org/builds)")
	if want := `<p><a href="https://example.org/builds" target="_blank" rel="noopener noreferrer">build status</a></p>` + "\n"; badge != want {
		t.Errorf("an image inside a link renders as\n%q\nwant\n%q", badge, want)
	}
}

// TestMarkdown_HeadingsDemoted is Acceptance Criterion 187 and rule 5.
func TestMarkdown_HeadingsDemoted(t *testing.T) {
	out := md(t, "# One\n\n## Two\n\n### Three\n\n#### Four\n\n##### Five\n\n###### Six\n\nSetext one\n===\n\nSetext two\n---\n")
	matches := headingOpenRe.FindAllStringSubmatch(out, -1)
	levels := make([]string, 0, len(matches))
	for _, m := range matches {
		levels = append(levels, m[1])
		if m[2] != "" {
			t.Errorf("a rendered heading carries attributes %q", m[2])
		}
	}
	if want := []string{"4", "5", "6", "6", "6", "6", "4", "5"}; !slices.Equal(levels, want) {
		t.Errorf("heading levels = %v, want %v:\n%s", levels, want, out)
	}
}

// TestMarkdown_NoAuthorAttributes is rule 11 over every construct at once: no
// style attribute, no event handler, and no id but a prefixed footnote id.
func TestMarkdown_NoAuthorAttributes(t *testing.T) {
	out := md(t, allConstructs("groadmap")+"\n\n"+hostileMarkdown)
	assertNoAuthorMarkup(t, "all constructs", out)
	for _, id := range idAttrRe.FindAllStringSubmatch(out, -1) {
		if !strings.HasPrefix(id[1], "task-42-acceptance_criteria-") {
			t.Errorf("an id outside the field's prefix reached the output: %q", id[1])
		}
	}
}

// allConstructs is a Markdown field exercising every construct the renderer
// accepts, with one footnote.
func allConstructs(roadmap string) string {
	return strings.Join([]string{
		"# Settlement runbook",
		"",
		"Reconcile the **day** before the _freeze_.",
		"Keep the ~~manual~~ export.",
		"",
		"| Window | Residual |",
		"|:------:|---------:|",
		"| 09:00  | 0.00     |",
		"",
		"- [x] export the settlement day",
		"- [ ] compare the totals",
		"",
		"See [the board](/roadmaps/" + roadmap + "/tasks), https://example.org/runbook and ![diagram](https://example.org/diagram.png).",
		"",
		"The drift is one cent[^1].",
		"",
		"[^1]: Rounding happens twice.",
		"",
		"Residual",
		": The difference left after reconciliation.",
		"",
		"```go",
		"func Residual() int { return 0 }",
		"```",
	}, "\n")
}

// TestMarkdown_NonInteractiveForm is rule 14 at the renderer: no <a> and no
// <input>, links as their text and checkboxes as [x] / [ ], and everything else
// identical to the ordinary form.
func TestMarkdown_NonInteractiveForm(t *testing.T) {
	src := allConstructs("groadmap")
	out := mdForm(t, src, "sprint-7-description-", markdownNonInteractive)
	if strings.Contains(out, "<a") || strings.Contains(out, "<input") {
		t.Errorf("the non-interactive form emits an interactive element:\n%s", out)
	}
	for _, want := range []string{
		"<li>[x] export the settlement day</li>", "<li>[ ] compare the totals</li>",
		"See the board, https://example.org/runbook and diagram.",
		`<sup id="sprint-7-description-fnref:1">1</sup>`,
		`<li id="sprint-7-description-fn:1">`,
		"<h4>Settlement runbook</h4>", `<pre class="chroma">`, `<th class="text-center">Window</th>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the non-interactive form lacks %q:\n%s", want, out)
		}
	}

	// Apart from the links and the checkboxes, the two forms are identical.
	ordinary := mdForm(t, src, "sprint-7-description-", markdownInteractive)
	strip := regexp.MustCompile(`<a [^>]*>|</a>|<input[^>]*> |\[[x ]\] `)
	if a, b := strip.ReplaceAllString(ordinary, ""), strip.ReplaceAllString(out, ""); a != b {
		t.Errorf("the forms differ beyond links and checkboxes:\nordinary: %s\nnon-interactive: %s", a, b)
	}
}

// TestMarkdown_FootnotePrefixes is rule 12 at the renderer: every id and every
// fragment of a field carries that field's prefix, built from the entity id and
// the fixed field name.
func TestMarkdown_FootnotePrefixes(t *testing.T) {
	for _, tc := range []struct {
		prefix, want string
	}{
		{taskFieldIDPrefix(42, "acceptance_criteria"), "task-42-acceptance_criteria-"},
		{taskFieldIDPrefix(42, "completion_summary"), "task-42-completion_summary-"},
		{taskCommentIDPrefix(13), "task-comment-13-"},
		{sprintDescriptionIDPrefix(7), "sprint-7-description-"},
		{sprintCommentIDPrefix(3), "sprint-comment-3-"},
	} {
		if tc.prefix != tc.want {
			t.Errorf("prefix = %q, want %q", tc.prefix, tc.want)
		}
		out := mdForm(t, "One[^a] and two[^b] and one again[^a].\n\n[^a]: First.\n[^b]: Second.", tc.prefix, markdownInteractive)
		assertFootnotesSelfContained(t, tc.prefix, out)
	}
}

// assertFootnotesSelfContained checks one field's rendered HTML: every id starts
// with the field's prefix, no id repeats, and every fragment link points at an id
// inside the same HTML.
func assertFootnotesSelfContained(t *testing.T, prefix, out string) []string {
	t.Helper()
	matches := idAttrRe.FindAllStringSubmatch(out, -1)
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		if !strings.HasPrefix(m[1], prefix) {
			t.Errorf("id %q does not carry the field prefix %q", m[1], prefix)
		}
		if slices.Contains(ids, m[1]) {
			t.Errorf("id %q repeats within one field", m[1])
		}
		ids = append(ids, m[1])
	}
	for _, m := range fragmentRe.FindAllStringSubmatch(out, -1) {
		if !slices.Contains(ids, m[1]) {
			t.Errorf("fragment #%s does not resolve within the field (ids %v)", m[1], ids)
		}
	}
	return ids
}

// TestMarkdown_DeterministicAndStateless is rule 2: the same text renders to the
// same bytes, concurrently and repeatedly, whatever was rendered in between.
func TestMarkdown_DeterministicAndStateless(t *testing.T) {
	src := allConstructs("groadmap")
	want := md(t, src)
	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for i := range 16 {
		wg.Go(func() {
			for j := range 4 {
				// Only the interleaving of another render matters here, not its result.
				_, _ = renderMarkdown(hostileMarkdown, sprintCommentIDPrefix(i*4+j), markdownNonInteractive)
				got, err := renderMarkdown(src, "task-42-acceptance_criteria-", markdownInteractive)
				if err != nil || got != want {
					errs <- "a render differed from the first one"
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ==================== THE STYLESHEET ====================

// highlightCSS is the CSS the pinned chroma produces, in class-based form, for the
// github-dark style. It MUST stay identical to the generation in
// highlightcss_gen.go.
func highlightCSS(t *testing.T) []byte {
	t.Helper()
	style := styles.Get(highlightStyle)
	if style.Name != highlightStyle {
		// styles.Get silently returns the fallback style for an unknown name.
		t.Fatalf("chroma has no %q style (got %q)", highlightStyle, style.Name)
	}
	var buf bytes.Buffer
	if err := chromahtml.New(chromahtml.WithClasses(true)).WriteCSS(&buf, style); err != nil {
		t.Fatalf("writing the chroma CSS: %v", err)
	}
	return buf.Bytes()
}

// TestHighlightCSS_EqualsPinnedChromaOutput is the test gate of rule 7 and of
// SPEC/BUILD.md § Markdown Rendering Rules, rule 4: the embedded stylesheet is
// byte for byte what the chroma version go.mod pins produces for github-dark, so
// an upgrade of chroma cannot leave the class names of the rendered HTML and of
// the stylesheet out of step. Regenerate with `go generate ./internal/web/`.
func TestHighlightCSS_EqualsPinnedChromaOutput(t *testing.T) {
	embedded, err := staticFS.ReadFile("static/highlight.css")
	if err != nil {
		t.Fatalf("the syntax-highlighting stylesheet is not embedded: %v", err)
	}
	if want := highlightCSS(t); !bytes.Equal(embedded, want) {
		t.Errorf("static/highlight.css differs from the pinned chroma's github-dark CSS; " +
			"run `go generate ./internal/web/` and commit the result")
	}
	for _, scope := range []string{"@media", "prefers-color-scheme", "data-bs-theme", "[data-", ":root"} {
		if bytes.Contains(embedded, []byte(scope)) {
			t.Errorf("the stylesheet scopes a rule by %q; it is the one fixed dark stylesheet", scope)
		}
	}
	// The renderer emits the class names the stylesheet styles. A token class the
	// style colours as plain text (github-dark's punctuation) has no rule of its own
	// and inherits the .chroma colour, so the check is on the coloured ones.
	out := md(t, "```go\nfunc Residual() int { return 0 }\n```")
	for _, class := range []string{"chroma", "kd", "nf", "kt", "k", "mi"} {
		if !strings.Contains(out, `class="`+class+`"`) {
			t.Errorf("the rendered block carries no %q class:\n%s", class, out)
		}
		if !bytes.Contains(embedded, []byte("."+class+" ")) {
			t.Errorf("the rendered token class %q is not styled by the stylesheet", class)
		}
	}
}

// TestHighlightCSS_OneStylesheetServedAndLinked is the rest of Acceptance
// Criterion 182: the stylesheet is served as CSS, no second highlighting
// stylesheet exists, and the three pages that can render a Markdown field link
// it, before the project override stylesheet.
func TestHighlightCSS_OneStylesheetServedAndLinked(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedMarkdownFixture(t, "settlement-runbook")
	mux := buildMux()

	req := httptest.NewRequest(http.MethodGet, "/static/highlight.css", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /static/highlight.css: status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("GET /static/highlight.css: Content-Type %q, want text/css", ct)
	}

	err := fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, ".css") || path == "static/highlight.css" {
			return walkErr
		}
		data, readErr := staticFS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(data, []byte(".chroma")) {
			t.Errorf("%s carries syntax-highlighting rules; highlight.css is the only one", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded assets: %v", err)
	}

	link := `<link rel="stylesheet" href="/static/highlight.css">`
	for _, path := range []string{
		"/roadmaps/" + f.name,
		"/roadmaps/" + f.name + "/tasks",
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.openSprintID),
	} {
		body := servePage(t, mux, path)
		if got := strings.Count(body, link); got != 1 {
			t.Errorf("%s links highlight.css %d times, want once", path, got)
		}
		assertOrdered(t, path, body, []string{
			"/static/vendor/tabler-icons/tabler-icons.min.css", "/static/highlight.css", "/static/style.css",
		})
	}
}

// ==================== THE SURFACES ====================

// markdownFixture is a roadmap whose every Markdown field, on every surface,
// carries Markdown: a sprint under each of the three tabs, sprint comments, and a
// completed task with two comments.
type markdownFixture struct {
	name             string
	upcomingSprintID int
	openSprintID     int
	closedSprintID   int
	sprintCommentIDs []int
	taskID           int
	taskCommentIDs   []int
}

// Markdown the fixture stores, with footnotes in every field so the page-level id
// uniqueness is exercised.
const (
	sprintMarkdownDescription = "Close the **settlement** window.\nKeep the ledger frozen.\n\n" +
		"- export the day\n- compare the totals\n\n" +
		"See [the runbook](https://example.org/runbook), https://example.org/status and " +
		"![flow chart](https://example.org/flow.png).\n\n" +
		"- [x] freeze announced\n- [ ] residual published\n\n" +
		"The drift is one cent[^1].\n\n[^1]: Rounding happens twice.\n\n" +
		"| Window | Residual |\n|:------:|---------:|\n| 09:00 | 0.00 |\n\n" +
		"The last line of the description."
	sprintCommentMarkdown = "Progress is **on track**.\nThe second line.\n\n- one\n- two\n\n" +
		"Measured at 09:00[^1].\n\n[^1]: Against the reference feed.\n\n" +
		"- [x] exported\n- [ ] compared\n\n" + hostileMarkdown
	taskFieldMarkdown = "The **ledger** must balance.\nSecond line.\n\n- first\n- second\n\n" +
		"Checked at close[^1].\n\n[^1]: By the operator.\n\n" + hostileMarkdown
	taskCommentMarkdown = "Found the **drift**.\nSecond line.\n\n- cause\n- effect\n\n" +
		"Traced[^1].\n\n[^1]: In the importer.\n\n" + hostileMarkdown
)

// seedMarkdownFixture creates the fixture roadmap under the test's HOME.
func seedMarkdownFixture(t *testing.T, name string) markdownFixture {
	t.Helper()
	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	const now = "2026-09-01T08:00:00Z"
	mkSprint := func(title string, order int) int {
		id, serr := seedSprint(database, &models.Sprint{
			Status: models.SprintPending, Title: title, Description: sprintMarkdownDescription,
			CreatedAt: now, Order: order,
		})
		if serr != nil {
			t.Fatalf("creating sprint %q: %v", title, serr)
		}
		return id
	}
	f := markdownFixture{name: name}
	f.upcomingSprintID = mkSprint("Publish the **residual** report", 30)
	f.openSprintID = mkSprint("Reconcile the settlement windows", 20)
	f.closedSprintID = mkSprint("Retire the legacy importer", 10)
	forceSprintOpen(t, database, f.openSprintID)
	setClosed(t, database, f.closedSprintID, "2026-08-30T17:00:00Z")

	f.sprintCommentIDs = []int{
		addSprintCommentTo(t, database, f.openSprintID, models.CommentProgress, sprintCommentMarkdown, "2026-09-02T09:00:00.000Z"),
		addSprintCommentTo(t, database, f.openSprintID, models.CommentDecision, sprintCommentMarkdown, "2026-09-03T09:00:00.000Z"),
	}

	task := seededTask(now, "Remove the **one-cent** drift")
	task.FunctionalRequirements = taskFieldMarkdown
	task.TechnicalRequirements = taskFieldMarkdown
	task.AcceptanceCriteria = taskFieldMarkdown
	f.taskID, err = seedTask(database, task)
	if err != nil {
		t.Fatalf("creating the task: %v", err)
	}
	if aerr := database.AddTasksToSprint(t.Context(), f.openSprintID, []int{f.taskID}); aerr != nil {
		t.Fatalf("adding the task to the sprint: %v", aerr)
	}
	if _, err := database.Exec("UPDATE tasks SET status = ?, closed_at = ?, completion_summary = ? WHERE id = ?",
		models.StatusCompleted, "2026-09-04T08:00:00Z", taskFieldMarkdown, f.taskID); err != nil {
		t.Fatalf("completing the task: %v", err)
	}
	f.taskCommentIDs = []int{
		addTaskCommentTo(t, database, f.taskID, models.CommentFinding, taskCommentMarkdown, "2026-09-02T10:00:00.000Z"),
		addTaskCommentTo(t, database, f.taskID, models.CommentDecision, taskCommentMarkdown, "2026-09-02T11:00:00.000Z"),
	}
	return f
}

// sprintCardSlice returns the card of one sprint on the sprints page — from the
// card link's start tag to its end tag — and the id of the tab pane holding it.
func sprintCardSlice(t *testing.T, body, name string, sprintID int) (card, pane string) {
	t.Helper()
	start := strings.Index(body, `<a href="/roadmaps/`+name+`/sprints/`+itoa(sprintID)+`" class="card`)
	if start < 0 {
		t.Fatalf("the sprints page renders no card for sprint #%d", sprintID)
	}
	end := strings.Index(body[start:], "</a>")
	if end < 0 {
		t.Fatalf("the card of sprint #%d is not closed", sprintID)
	}
	paneAt := strings.LastIndex(body[:start], `<div id="tab-`)
	if paneAt < 0 {
		t.Fatalf("the card of sprint #%d is in no tab pane", sprintID)
	}
	pane = body[paneAt+len(`<div id="`):]
	pane = pane[:strings.IndexByte(pane, '"')]
	return body[start : start+end+len("</a>")], pane
}

// TestMarkdownSurfaces_SprintCardsAreNonInteractive is Acceptance Criteria 180
// and 189 on the Roadmap Sprints Page: under each of the three tabs the sprint
// card renders the description as Markdown, in the non-interactive form, whole,
// with the card itself the only <a>; the title stays plain text.
func TestMarkdownSurfaces_SprintCardsAreNonInteractive(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedMarkdownFixture(t, "settlement-runbook")
	body := servePage(t, buildMux(), "/roadmaps/"+f.name)

	for sprintID, wantPane := range map[int]string{
		f.upcomingSprintID: "tab-upcoming",
		f.openSprintID:     "tab-current",
		f.closedSprintID:   "tab-closed",
	} {
		card, pane := sprintCardSlice(t, body, f.name, sprintID)
		if pane != wantPane {
			t.Errorf("sprint #%d is under %s, want %s", sprintID, pane, wantPane)
		}
		want := `<div class="markdown mb-1">` +
			mdForm(t, sprintMarkdownDescription, sprintDescriptionIDPrefix(sprintID), markdownNonInteractive) + `</div>`
		if !strings.Contains(card, want) {
			t.Errorf("sprint #%d: the card does not carry the non-interactive rendering in its markdown container:\n%s", sprintID, card)
		}
		if got := len(anchorOpenRe.FindAllString(card, -1)); got != 1 {
			t.Errorf("sprint #%d: the card holds %d <a> elements, want only the card itself", sprintID, got)
		}
		for _, bad := range []string{"<input", "<button", "<form", "<select", "<textarea"} {
			if strings.Contains(card, bad) {
				t.Errorf("sprint #%d: the card holds the interactive element %q", sprintID, bad)
			}
		}
		for _, text := range []string{
			"<strong>settlement</strong>", "window.<br>\nKeep", "<li>export the day</li>",
			"the runbook, https://example.org/status and flow chart.",
			"<li>[x] freeze announced</li>", "<li>[ ] residual published</li>",
			"The last line of the description.",
		} {
			if !strings.Contains(card, text) {
				t.Errorf("sprint #%d: the card lacks %q (the whole description is shown)", sprintID, text)
			}
		}
	}
	// A sprint title containing Markdown syntax stays plain text.
	if !strings.Contains(body, `<h4 class="card-title mb-0">Publish the **residual** report</h4>`) {
		t.Error("the sprint title is not rendered as plain text with its asterisks")
	}
}

// TestMarkdownSurfaces_SprintPage is Acceptance Criteria 180, 183 (server path),
// 189, and 190 on the Roadmap Sprint Page, and the rewritten 65, 72, and 73 for
// the Comments card: the description and every comment body render as Markdown in
// the ordinary form, raw HTML never reaches the page, the only inputs are disabled
// task-list checkboxes, and no two elements share an id.
func TestMarkdownSurfaces_SprintPage(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedMarkdownFixture(t, "settlement-runbook")
	body := servePage(t, buildMux(), "/roadmaps/"+f.name+"/sprints/"+itoa(f.openSprintID))

	fields := map[string]string{
		sprintDescriptionIDPrefix(f.openSprintID): sprintMarkdownDescription,
	}
	for _, id := range f.sprintCommentIDs {
		fields[sprintCommentIDPrefix(id)] = sprintCommentMarkdown
	}
	for prefix, source := range fields {
		rendered := mdForm(t, source, prefix, markdownInteractive)
		if !strings.Contains(body, `">`+rendered+`</div>`) || !strings.Contains(body, `<div class="markdown`) {
			t.Errorf("the sprint page does not carry the rendering of %s in a markdown container", prefix)
		}
		assertFootnotesSelfContained(t, prefix, rendered)
		assertNoAuthorMarkup(t, prefix, rendered)
	}
	if !strings.Contains(body, `<div class="markdown mb-3">`+mdForm(t, sprintMarkdownDescription,
		sprintDescriptionIDPrefix(f.openSprintID), markdownInteractive)+`</div>`) {
		t.Error("the sprint description is not in its markdown container")
	}
	if got := strings.Count(body, `<div class="markdown">`); got != len(f.sprintCommentIDs) {
		t.Errorf("%d comment bodies sit in a markdown container, want %d", got, len(f.sprintCommentIDs))
	}

	// The ordinary form: links are <a>, checkboxes are disabled inputs.
	if !strings.Contains(body, `<a href="https://example.org/runbook" target="_blank" rel="noopener noreferrer">the runbook</a>`) {
		t.Error("the sprint page does not render the description's link as <a>")
	}
	inputs := regexp.MustCompile(`<input[^>]*>`).FindAllString(body, -1)
	if len(inputs) != 2+2*len(f.sprintCommentIDs) {
		t.Errorf("%d inputs on the sprint page, want the %d task-list checkboxes", len(inputs), 2+2*len(f.sprintCommentIDs))
	}
	for _, input := range inputs {
		if !strings.Contains(input, `disabled=""`) || !strings.Contains(input, `type="checkbox"`) {
			t.Errorf("an input on the sprint page is not a disabled checkbox: %s", input)
		}
	}
	card := sprintCommentsCardSlice(t, body)
	for _, bad := range []string{"<form", "<textarea", "<select", "<button", "<script", "<iframe", "<img", "<b>", "<b "} {
		if strings.Contains(card, bad) {
			t.Errorf("the Comments card holds %q", bad)
		}
	}
	for _, m := range tagRe.FindAllStringSubmatch(card, -1) {
		if eventAttrRe.MatchString(m[2]) || styleAttrRe.MatchString(m[2]) {
			t.Errorf("the Comments card holds <%s%s>", m[1], m[2])
		}
	}

	// No two elements of the page share an id.
	assertUniqueIDs(t, "sprint page", body)
	// The sprint title stays plain text in the page header.
	if !strings.Contains(body, "Reconcile the settlement windows") || strings.Contains(body, "<strong>Reconcile") {
		t.Error("the sprint title is not plain text")
	}
}

// assertUniqueIDs fails when two elements of a document share an id.
func assertUniqueIDs(t *testing.T, label, doc string) {
	t.Helper()
	seen := map[string]bool{}
	for _, m := range idAttrRe.FindAllStringSubmatch(doc, -1) {
		if seen[m[1]] {
			t.Errorf("%s: two elements share the id %q", label, m[1])
		}
		seen[m[1]] = true
	}
}

// TestMarkdownSurfaces_TaskDetailEndpoint is Acceptance Criteria 180 (modal
// fields), 183 (JSON path), 188, and 190 (modal): the endpoint carries each
// field's rendering beside the unchanged raw field, and the six fragments the
// modal shows share no id.
func TestMarkdownSurfaces_TaskDetailEndpoint(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedMarkdownFixture(t, "settlement-runbook")
	mux := buildMux()

	view := decodeTaskDetail(t, mux, f.name, f.taskID)
	task := view.Task
	members := map[string]string{
		"functional_requirements": task.FunctionalRequirementsHTML,
		"technical_requirements":  task.TechnicalRequirementsHTML,
		"acceptance_criteria":     task.AcceptanceCriteriaHTML,
		"completion_summary":      derefString(task.CompletionSummaryHTML),
	}
	allIDs := make([]string, 0, 2*(len(members)+len(f.taskCommentIDs)))
	for field, got := range members {
		prefix := taskFieldIDPrefix(f.taskID, field)
		if want := mdForm(t, taskFieldMarkdown, prefix, markdownInteractive); got != want {
			t.Errorf("%s_html is not the renderer's output for the field:\n%s\nwant\n%s", field, got, want)
		}
		if !strings.Contains(got, "<strong>ledger</strong>") || !strings.Contains(got, "<br>") {
			t.Errorf("%s_html is not rendered Markdown: %s", field, got)
		}
		assertNoAuthorMarkup(t, field+"_html", got)
		allIDs = append(allIDs, assertFootnotesSelfContained(t, prefix, got)...)
	}
	if len(view.Comments) != len(f.taskCommentIDs) {
		t.Fatalf("%d comments, want %d", len(view.Comments), len(f.taskCommentIDs))
	}
	for i := range view.Comments {
		c := view.Comments[i]
		prefix := taskCommentIDPrefix(c.ID)
		if want := mdForm(t, taskCommentMarkdown, prefix, markdownInteractive); c.BodyHTML != want {
			t.Errorf("comment %d: body_html is not the renderer's output", c.ID)
		}
		if c.Body != taskCommentMarkdown {
			t.Errorf("comment %d: the raw body changed: %q", c.ID, c.Body)
		}
		assertNoAuthorMarkup(t, "body_html", c.BodyHTML)
		allIDs = append(allIDs, assertFootnotesSelfContained(t, prefix, c.BodyHTML)...)
	}
	// The raw fields are unchanged.
	if task.FunctionalRequirements != taskFieldMarkdown || derefString(task.CompletionSummary) != taskFieldMarkdown {
		t.Error("a raw Markdown field is not carried unchanged")
	}
	// Six fragments, one modal: no id twice, and none equal to the shell's own.
	sorted := slices.Clone(allIDs)
	slices.Sort(sorted)
	if len(slices.Compact(sorted)) != len(allIDs) {
		t.Errorf("the modal's fragments share an id: %v", allIDs)
	}
	for _, id := range allIDs {
		if strings.HasPrefix(id, "task-modal") {
			t.Errorf("a fragment id %q collides with the modal shell's namespace", id)
		}
	}
	// The task title is not rendered anywhere as Markdown.
	page := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")
	if !strings.Contains(page, "Remove the **one-cent** drift") {
		t.Error("the task title is not shown as plain text with its asterisks")
	}
}

// TestTaskDetailView_HTMLMemberConventions is the null and empty half of
// Acceptance Criterion 188: completion_summary_html is null exactly when the
// summary is null, an empty raw field has an empty member, and the CLI's own Task
// and TaskComment objects carry no _html member.
func TestTaskDetailView_HTMLMemberConventions(t *testing.T) {
	task := &models.Task{ID: 9, FunctionalRequirements: "", TechnicalRequirements: "**x**", AcceptanceCriteria: ""}
	view, err := newTaskDetailView(task, nil)
	if err != nil {
		t.Fatalf("newTaskDetailView: %v", err)
	}
	if view.Task.CompletionSummaryHTML != nil {
		t.Errorf("completion_summary_html = %q for a null summary, want null", *view.Task.CompletionSummaryHTML)
	}
	if view.Task.FunctionalRequirementsHTML != "" || view.Task.AcceptanceCriteriaHTML != "" {
		t.Error("an empty raw field has a non-empty _html member")
	}
	if view.Comments == nil {
		t.Error("comments must marshal as [], never null")
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	for _, want := range []string{`"completion_summary_html":null`, `"functional_requirements_html":""`, `"comments":[]`} {
		if !bytes.Contains(encoded, []byte(want)) {
			t.Errorf("the response lacks %s: %s", want, encoded)
		}
	}
	// The members follow the raw fields, in the order note 9 fixes.
	order := []string{`"blocks"`, `"functional_requirements_html"`, `"technical_requirements_html"`,
		`"acceptance_criteria_html"`, `"completion_summary_html"`}
	last := -1
	for _, key := range order {
		at := bytes.Index(encoded, []byte(key))
		if at <= last {
			t.Errorf("%s is out of order in %s", key, encoded)
		}
		last = at
	}

	summary := ""
	task.CompletionSummary = &summary
	view, err = newTaskDetailView(task, []models.TaskComment{{ID: 4, Body: "_noted_"}})
	if err != nil {
		t.Fatalf("newTaskDetailView: %v", err)
	}
	if view.Task.CompletionSummaryHTML == nil || *view.Task.CompletionSummaryHTML != "" {
		t.Error("an empty, non-null summary must have an empty-string member")
	}
	if view.Comments[0].BodyHTML != "<p><em>noted</em></p>\n" {
		t.Errorf("body_html = %q", view.Comments[0].BodyHTML)
	}

	for _, v := range []any{models.Task{}, models.TaskComment{}, models.SprintComment{}, models.Sprint{}} {
		cli, _ := json.Marshal(v) //nolint:errcheck // a plain struct always marshals
		if bytes.Contains(cli, []byte("_html")) {
			t.Errorf("the CLI's %T carries an _html member: %s", v, cli)
		}
	}
}

// TestMarkdownSurfaces_CSPAndScriptsUnchanged is Acceptance Criterion 192, and
// the no-parser half of 188: pages showing Markdown fields with code, a data:
// image, and links carry exactly the fixed policy, load scripts from /static/
// only, carry no inline script, and no served script parses Markdown.
func TestMarkdownSurfaces_CSPAndScriptsUnchanged(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedMarkdownFixture(t, "settlement-runbook")
	h := handler()
	scriptRe := regexp.MustCompile(`<script([^>]*)>`)
	for _, path := range []string{
		"/roadmaps/" + f.name,
		"/roadmaps/" + f.name + "/tasks",
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.openSprintID),
		"/roadmaps/" + f.name + "/tasks/" + itoa(f.taskID) + "/data",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'self'; script-src 'self'; "+
			"style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; "+
			"frame-ancestors 'none'; base-uri 'self'" {
			t.Errorf("GET %s: Content-Security-Policy = %q", path, got)
		}
		for _, m := range scriptRe.FindAllStringSubmatch(rec.Body.String(), -1) {
			if !strings.Contains(m[1], `src="/static/`) {
				t.Errorf("GET %s: a script is not loaded from /static/: <script%s>", path, m[1])
			}
		}
	}

	err := fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, ".js") {
			return walkErr
		}
		data, readErr := staticFS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		lower := strings.ToLower(string(data))
		for _, parser := range []string{"markdown-it", "markdownit", "commonmark", "showdown", "micromark", "marked.parse", "remarkable"} {
			if strings.Contains(lower, parser) {
				t.Errorf("%s contains the Markdown parser %q", path, parser)
			}
		}
		if strings.Contains(lower, "chroma") || strings.Contains(lower, "highlight.js") || strings.Contains(lower, "prism") {
			t.Errorf("%s contains a highlighting library", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded assets: %v", err)
	}
}

// TestMarkdownStyles_NoForcedHorizontalScroll is Acceptance Criterion 193 and
// rule 13 on the stylesheet: the markdown container wraps and breaks long words,
// and its tables and code blocks scroll inside their own box. The plain-text
// pre-wrap rule now covers the graph detail panel alone (rule 6; Acceptance
// Criterion 191).
func TestMarkdownStyles_NoForcedHorizontalScroll(t *testing.T) {
	css := stripSpace(embeddedSheet(t, "static/style.css"))
	for _, want := range []string{
		".markdown{min-width:0;overflow-wrap:anywhere;word-break:break-word;}",
		".markdowntable{display:block;max-width:100%;overflow-x:auto;}",
		".markdownpre{max-width:100%;overflow-x:auto;white-space:pre;overflow-wrap:normal;word-break:normal;}",
		".detail-panel__value{white-space:pre-wrap;",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css lacks %q", want)
		}
	}
	for _, gone := range []string{".task-modal__text", ".sprint-description"} {
		if strings.Contains(css, gone) {
			t.Errorf("style.css still styles %s, which no Markdown field uses any more", gone)
		}
	}
	// The container class is Tabler's own.
	if !strings.Contains(embeddedSheet(t, "static/vendor/tabler/tabler.min.css"), ".markdown{") {
		t.Error("the vendored Tabler distribution does not define the markdown class")
	}
}

// TestGraphDetailPanel_StaysPlainText is Acceptance Criterion 191 on the served
// script: the graph detail panel writes property values through textContent into
// the pre-wrap class and renders no Markdown.
func TestGraphDetailPanel_StaysPlainText(t *testing.T) {
	script := stripJSComments(readEmbeddedAsset(t, "static/graph.js"))
	if !strings.Contains(script, `dd.className = "detail-panel__value"`) || !strings.Contains(script, "dd.textContent = value") {
		t.Error("graph.js does not write detail-panel values as text into the pre-wrap class")
	}
	for _, bad := range []string{"markdown", "_html", "insertAdjacentHTML", "outerHTML"} {
		if strings.Contains(script, bad) {
			t.Errorf("graph.js names %q; the detail panel stays plain text", bad)
		}
	}
	// innerHTML is used only to empty a container, never to write a value.
	if all, clears := strings.Count(script, "innerHTML"), strings.Count(script, `innerHTML = "";`); all != clears {
		t.Errorf("graph.js assigns innerHTML %d times, of which only %d empty a container", all, clears)
	}
}

// TestMarkdownRenderer_CompiledIn is Acceptance Criterion 194 and SPEC/BUILD.md
// § Markdown Rendering Rules, rules 1 to 3: go.mod's first require block names
// the three modules at exact versions, and the renderer highlights code with the
// working directory and HOME pointed at empty directories, so no lexer or style
// is read from the host filesystem.
func TestMarkdownRenderer_CompiledIn(t *testing.T) {
	gomod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	_, block, found := strings.Cut(string(gomod), "require (")
	if !found {
		t.Fatal("go.mod has no require block")
	}
	block, _, _ = strings.Cut(block, ")")
	for _, module := range []string{"github.com/yuin/goldmark ", "github.com/yuin/goldmark-highlighting/v2 ", "github.com/alecthomas/chroma/v2 "} {
		line := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(module) + `(v\S+)\s*$`).FindStringSubmatch(block)
		if line == nil {
			t.Errorf("the first require block of go.mod does not pin %s at an exact version", module)
		}
	}
	if !strings.Contains(string(gomod), "github.com/dlclark/regexp2") {
		t.Error("go.mod does not pin chroma's regular-expression module")
	}

	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	out := md(t, "```go\nfunc Residual() int { return 0 }\n```")
	if !strings.Contains(out, `<span class="kd">func</span>`) {
		t.Errorf("the renderer does not highlight with nothing on disk:\n%s", out)
	}
}
