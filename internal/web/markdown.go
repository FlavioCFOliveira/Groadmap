package web

//go:generate go run highlightcss_gen.go

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"strconv"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/FlavioCFOliveira/Groadmap/internal/highlight"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// markdownForm selects one of the two forms of the Markdown renderer
// (SPEC/WEB.md § Markdown Rendering, rule 14).
type markdownForm uint8

const (
	// markdownInteractive is the ordinary form, used on every surface but the
	// sprint card: links render as <a>, task-list items as disabled checkboxes.
	markdownInteractive markdownForm = iota
	// markdownNonInteractive is the form for content placed inside a link — the
	// sprint card — where HTML admits no link and no form control: every <a> the
	// ordinary form would emit renders as its text alone, and a task-list
	// checkbox renders as the text marker [x] or [ ].
	markdownNonInteractive
)

// highlightStyle names the chroma style the syntax-highlighting stylesheet is
// generated from (highlightcss_gen.go). It is the name of the one style package
// highlight carries, highlight.HighlightStyle, and that style — not a lookup of
// this name — is what highlighted HTML is emitted against. The stylesheet and the
// HTML MUST come from the same style, because which token types a highlighted
// block marks with a class depends on the style's entries (SPEC/WEB.md
// § Markdown Rendering, rule 7).
const highlightStyle = highlight.StyleName

// footnotePrefixAttr is the name of the document-level attribute that carries the
// footnote identifier prefix of the field being rendered. It is set by
// renderMarkdown on the parsed document, never by the author (the attribute
// syntax is not enabled), and read by footnoteIDPrefix. The document node renders
// nothing, so the attribute never reaches the output.
const footnotePrefixAttr = "groadmap-footnote-prefix"

// footnoteBacklinkText is the text of a footnote back-link: goldmark's default
// BacklinkHTML, rendered as-is by the ordinary form and as bare text by the
// non-interactive form.
const footnoteBacklinkText = "&#x21a9;&#xfe0e;"

// markdownRenderers holds the two configured goldmark instances, indexed by
// markdownForm. They are configuration, not state: goldmark.Markdown is safe for
// concurrent use, holds nothing between calls, and caches no rendered output
// (SPEC/WEB.md § Markdown Rendering, rule 2).
var markdownRenderers = [...]goldmark.Markdown{
	markdownInteractive:    newMarkdownRenderer(markdownInteractive),
	markdownNonInteractive: newMarkdownRenderer(markdownNonInteractive),
}

// newMarkdownRenderer builds the one Markdown renderer the web interface uses,
// in the requested form (SPEC/WEB.md § Markdown Rendering, rules 2 to 14):
//
//   - CommonMark with the GitHub Flavored Markdown extensions (tables,
//     strikethrough, autolinks, task lists), footnotes, and definition lists, and
//     no other extension. Raw HTML output is left disabled (goldmark's default),
//     so raw HTML is omitted; neither the attribute syntax nor automatic heading
//     identifiers is enabled.
//   - Hard wraps: a single newline inside a paragraph renders as <br>.
//   - Fenced code blocks are highlighted by chroma, through the renderer's own
//     codeHighlighting extension, with CSS classes and never inline styles, by
//     the declared language only.
//   - Table cells carry no alignment attribute of goldmark's own; the transformer
//     gives them the Tabler text-alignment class instead.
func newMarkdownRenderer(form markdownForm) goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(
			extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignNone)),
			extension.Strikethrough,
			extension.Linkify,
			extension.TaskList,
			extension.NewFootnote(extension.WithFootnoteIDPrefixFunction(footnoteIDPrefix)),
			extension.DefinitionList,
			codeHighlighting{},
		),
		goldmark.WithParserOptions(
			parser.WithASTTransformers(util.Prioritized(markdownTransformer{}, 100)),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			// A lower value is a higher priority in goldmark, and the renderer
			// registered with the highest priority wins a node kind: 100 takes the
			// link, autolink, and image kinds from the core renderer (1000) and,
			// in the non-interactive form, the task-checkbox and footnote-link
			// kinds from their extensions (500).
			renderer.WithNodeRenderers(util.Prioritized(markdownNodeRenderer{form: form}, 100)),
		),
	)
}

// renderMarkdown renders the stored text of one Markdown field as an HTML
// fragment, in the requested form, with every footnote identifier prefixed by
// idPrefix (SPEC/WEB.md § Markdown Rendering, rule 12). It is the single
// rendering unit of the web interface: every surface obtains a Markdown field's
// HTML from here. The empty string renders as the empty string.
//
// idPrefix MUST be built by the caller from an entity's integer id and a field's
// fixed name only (see the *IDPrefix helpers below), never from author text.
func renderMarkdown(source, idPrefix string, form markdownForm) (string, error) {
	if source == "" {
		return "", nil
	}
	md := markdownRenderers[form]
	src := []byte(source)
	doc := md.Parser().Parse(text.NewReader(src))
	doc.SetAttributeString(footnotePrefixAttr, []byte(idPrefix))
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, src, doc); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// footnoteIDPrefix returns the prefix renderMarkdown attached to the document the
// node belongs to. It is the footnote extension's IDPrefixFunction.
func footnoteIDPrefix(node ast.Node) []byte {
	doc := node.OwnerDocument()
	if doc == nil {
		return nil
	}
	v, ok := doc.AttributeString(footnotePrefixAttr)
	if !ok {
		return nil
	}
	prefix, _ := v.([]byte) // renderMarkdown only ever sets it as []byte
	return prefix
}

// The footnote identifier prefixes of SPEC/WEB.md § Markdown Rendering, rule 12.
// Each is built only from an entity's integer id and a fixed field name.

func taskFieldIDPrefix(taskID int, field string) string {
	return "task-" + strconv.Itoa(taskID) + "-" + field + "-"
}

func taskCommentIDPrefix(commentID int) string {
	return "task-comment-" + strconv.Itoa(commentID) + "-"
}

func sprintDescriptionIDPrefix(sprintID int) string {
	return "sprint-" + strconv.Itoa(sprintID) + "-description-"
}

func sprintCommentIDPrefix(commentID int) string {
	return "sprint-comment-" + strconv.Itoa(commentID) + "-"
}

// codeHighlighting is the goldmark extension that renders every fenced code
// block in place of goldmark's default fenced-code renderer (SPEC/WEB.md
// § Markdown Rendering, rule 6). It is registered at priority 200, which takes
// the fenced-code-block kind from goldmark's core renderer (1000).
type codeHighlighting struct{}

// Extend implements goldmark.Extender.
func (codeHighlighting) Extend(m goldmark.Markdown) {
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(fencedCodeRenderer{}, 200)))
}

// fencedCodeRenderer renders a fenced code block.
type fencedCodeRenderer struct{}

// RegisterFuncs implements renderer.NodeRenderer.
func (fencedCodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, renderFencedCode)
}

// renderFencedCode renders a fenced code block highlighted when its declared
// language resolves to a lexer of the generated registry, and as a bare
// <pre><code> otherwise.
//
// The language is the info string's first word, taken byte for byte:
// markdownTransformer has already cut the info string to it, and dropped it when
// it was empty or carried a brace, so no attribute of the block is read from the
// info string. A name the registry does not resolve is not highlighted: no lexer
// is guessed from the content and no fallback lexer is used.
//
// A highlighted block is the coalesced lexer's tokens of the block's text, the
// concatenation of its lines, written by chroma's HTML formatter in class-based
// form with no other option, against the style package highlight carries. The
// formatter writes its own <pre class="chroma">, and nothing is written around
// it. A block whose lexer fails to tokenise its text renders unhighlighted.
//
// The formatter's result is not checked, as it never was: the only failure it
// reports that the writer does not also hold is the iterator's, and a write
// error is sticky on the bufio-backed writer and fails the render (see
// markdownNodeRenderer).
func renderFencedCode(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.FencedCodeBlock) // registered for ast.KindFencedCodeBlock only
	lines := n.Lines()
	if language := n.Language(source); language != nil {
		if lexer := highlight.ResolveLexer(string(language)); lexer != nil {
			var code bytes.Buffer
			for i := range lines.Len() {
				seg := lines.At(i)
				code.Write(seg.Value(source))
			}
			if iterator, err := chroma.Coalesce(lexer).Tokenise(nil, code.String()); err == nil {
				_ = chromahtml.New(chromahtml.WithClasses(true)).Format(w, highlight.HighlightStyle(), iterator)
				return ast.WalkContinue, nil
			}
		}
	}
	// The unhighlighted block: no class, and no attribute taken from the info
	// string (SPEC/WEB.md § Markdown Rendering, rules 6 and 11).
	_, _ = w.WriteString("<pre><code>")
	for i := range lines.Len() {
		seg := lines.At(i)
		html.DefaultWriter.RawWrite(w, seg.Value(source))
	}
	_, err := w.WriteString("</code></pre>\n")
	return ast.WalkContinue, err
}

// markdownTransformer rewrites the parsed document before it is rendered:
//
//   - headings are demoted: level 1 to <h4>, level 2 to <h5>, and levels 3 to 6
//     to <h6> (rule 5);
//   - a table cell's alignment becomes the Tabler class text-start, text-center,
//     or text-end (rule 11);
//   - a fenced code block's info string is cut to its language word, so the
//     fenced-code renderer reads no attribute block ({...}) from it; a language
//     word that itself carries a brace is dropped, and the block renders
//     unhighlighted, as any unrecognised language does (rules 3 and 6);
//   - the <li> of a task-list item carries the fixed class task-list-item, in
//     both forms, so one stylesheet rule places its checkbox or text marker
//     where the list marker would be (rules 3, 13, and 14).
type markdownTransformer struct{}

// taskListItemClass is the fixed class of a task-list item's <li> (SPEC/WEB.md
// § Markdown Rendering, rule 3).
const taskListItemClass = "task-list-item"

// Transform implements parser.ASTTransformer.
func (markdownTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	// The callback never returns an error, so neither does the walk.
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.Heading:
			node.Level = demotedHeadingLevel(node.Level)
		case *extast.TableCell:
			if class := alignmentClass(node.Alignment); class != "" {
				node.SetAttributeString("class", []byte(class))
			}
		case *ast.FencedCodeBlock:
			trimCodeInfo(node, source)
		case *ast.ListItem:
			if isTaskListItem(node) {
				node.SetAttributeString("class", []byte(taskListItemClass))
			}
		}
		return ast.WalkContinue, nil
	})
}

// isTaskListItem reports whether a list item is a task-list item: the task-list
// extension parses a checkbox only as the first inline of the item's first
// block, so that is the one place it is looked for.
func isTaskListItem(item *ast.ListItem) bool {
	block := item.FirstChild()
	if block == nil {
		return false
	}
	_, ok := block.FirstChild().(*extast.TaskCheckBox)
	return ok
}

// demotedHeadingLevel maps a Markdown heading level to the rendered level
// (SPEC/WEB.md § Markdown Rendering, rule 5).
func demotedHeadingLevel(level int) int {
	switch level {
	case 1:
		return 4
	case 2:
		return 5
	default:
		return 6
	}
}

// alignmentClass maps a GFM table column alignment to the Tabler text-alignment
// class (SPEC/WEB.md § Markdown Rendering, rule 11).
func alignmentClass(a extast.Alignment) string {
	switch a {
	case extast.AlignLeft:
		return "text-start"
	case extast.AlignCenter:
		return "text-center"
	case extast.AlignRight:
		return "text-end"
	default:
		return ""
	}
}

// trimCodeInfo cuts a fenced code block's info string to its first word — the
// word goldmark itself takes as the language — and drops it entirely when that
// word carries a brace, so no attribute block reaches the fenced-code renderer.
func trimCodeInfo(n *ast.FencedCodeBlock, source []byte) {
	if n.Info == nil {
		return
	}
	seg := n.Info.Segment
	info := seg.Value(source)
	end := bytes.IndexByte(info, ' ')
	if end < 0 {
		end = len(info)
	}
	if bytes.IndexByte(info[:end], '{') >= 0 || end == 0 {
		n.Info = nil
		return
	}
	n.Info = ast.NewTextSegment(text.NewSegment(seg.Start, seg.Start+end))
}

// markdownNodeRenderer renders the node kinds whose HTML the Markdown rules fix
// beyond goldmark's defaults: links, autolinks, and images in both forms (rules 8
// and 9), and, in the non-interactive form only, task-list checkboxes and
// footnote references and back-links (rule 14).
//
// Write errors are deliberately not checked write by write: goldmark hands every
// render function a bufio-backed util.BufWriter, whose first write error is
// sticky — every later write and the final Flush return it — and goldmark's
// Render returns that Flush error. A failed write therefore still fails the
// render; the blank assignments below discard only duplicates of it.
type markdownNodeRenderer struct {
	form markdownForm
}

// RegisterFuncs implements renderer.NodeRenderer.
func (r markdownNodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindLink, r.renderLink)
	reg.Register(ast.KindAutoLink, r.renderAutoLink)
	reg.Register(ast.KindImage, r.renderImage)
	if r.form == markdownNonInteractive {
		reg.Register(extast.KindTaskCheckBox, renderTaskMarker)
		reg.Register(extast.KindFootnoteLink, renderFootnoteRefText)
		reg.Register(extast.KindFootnoteBacklink, renderFootnoteBacklinkText)
	}
}

// activeHref returns the escaped href a link destination renders with, and
// whether it renders as an active <a> at all: a destination goldmark's
// dangerous-URL filter rejects never does, and in the non-interactive form no
// destination does.
func (r markdownNodeRenderer) activeHref(dest []byte) ([]byte, bool) {
	escaped := util.URLEscape(dest, true)
	if html.IsDangerousURL(escaped) || r.form == markdownNonInteractive {
		return nil, false
	}
	return escaped, true
}

// renderLink renders an inline or reference link: an <a> with the target rules
// of rule 8, or the link text alone when the link may not be active.
func (r markdownNodeRenderer) renderLink(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link) // registered for ast.KindLink only
	href, active := r.activeHref(n.Destination)
	if !active {
		// The children — the link text — render as ordinary inline content.
		return ast.WalkContinue, nil
	}
	if !entering {
		_, err := w.WriteString("</a>")
		return ast.WalkContinue, err
	}
	writeAnchorOpen(w, href, n.Title)
	return ast.WalkContinue, nil
}

// renderAutoLink renders a CommonMark autolink or a GFM bare address. goldmark's
// own autolink renderer writes an empty href for a dangerous URL, which a browser
// resolves to the current page; here such an autolink is its label alone.
func (r markdownNodeRenderer) renderAutoLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.AutoLink) // registered for ast.KindAutoLink only
	url := n.URL(source)
	if n.AutoLinkType == ast.AutoLinkEmail && !bytes.HasPrefix(bytes.ToLower(url), []byte("mailto:")) {
		url = append([]byte("mailto:"), url...)
	}
	label := util.EscapeHTML(n.Label(source))
	escaped := util.URLEscape(url, false)
	if html.IsDangerousURL(escaped) || r.form == markdownNonInteractive {
		_, err := w.Write(label)
		return ast.WalkContinue, err
	}
	writeAnchorOpen(w, escaped, nil)
	_, _ = w.Write(label)
	_, err := w.WriteString("</a>")
	return ast.WalkContinue, err
}

// renderImage applies rule 9: an accepted data: image renders as <img>; any
// other accepted source renders as a link to it whose text is the alternative
// text, or the URL when that is empty; a rejected source renders as the
// alternative text alone. No remote image is ever emitted, so a Markdown image
// never makes the browser issue a request.
func (r markdownNodeRenderer) renderImage(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image) // registered for ast.KindImage only
	var alt bytes.Buffer
	altWriter := bufio.NewWriter(&alt)
	writeNodeText(altWriter, source, n)
	if err := altWriter.Flush(); err != nil {
		return ast.WalkStop, err
	}

	escaped := util.URLEscape(n.Destination, true)
	switch {
	case html.IsDangerousURL(escaped):
		_, err := w.Write(alt.Bytes())
		return ast.WalkSkipChildren, err
	case hasPrefixFold(escaped, "data:"):
		_, _ = w.WriteString(`<img src="`)
		_, _ = w.Write(util.EscapeHTML(escaped))
		_, _ = w.WriteString(`" alt="`)
		_, _ = w.Write(alt.Bytes())
		_ = w.WriteByte('"')
		writeTitle(w, n.Title)
		err := w.WriteByte('>')
		return ast.WalkSkipChildren, err
	}

	text := alt.Bytes()
	if len(text) == 0 {
		text = util.EscapeHTML(escaped)
	}
	// Inside an active link the image is already linked by that link, and an <a>
	// nested in an <a> is invalid HTML that a browser splits apart, so the image
	// renders as its text there — as it does in the non-interactive form.
	if r.form == markdownNonInteractive || r.insideActiveLink(n) {
		_, err := w.Write(text)
		return ast.WalkSkipChildren, err
	}
	writeAnchorOpen(w, escaped, n.Title)
	_, _ = w.Write(text)
	_, err := w.WriteString("</a>")
	return ast.WalkSkipChildren, err
}

// insideActiveLink reports whether a node sits inside a link that renders as an
// active <a>.
func (r markdownNodeRenderer) insideActiveLink(n ast.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if link, ok := p.(*ast.Link); ok {
			if _, active := r.activeHref(link.Destination); active {
				return true
			}
		}
	}
	return false
}

// writeAnchorOpen writes an <a> start tag for an accepted, already URL-escaped
// destination: target="_blank" and rel="noopener noreferrer" for an absolute
// http or https URL or a network-path reference, neither for anything else
// (SPEC/WEB.md § Markdown Rendering, rule 8).
func writeAnchorOpen(w util.BufWriter, href, title []byte) {
	_, _ = w.WriteString(`<a href="`)
	_, _ = w.Write(util.EscapeHTML(href))
	_ = w.WriteByte('"')
	writeTitle(w, title)
	if opensInNewTab(href) {
		_, _ = w.WriteString(` target="_blank" rel="noopener noreferrer"`)
	}
	_ = w.WriteByte('>')
}

// writeTitle writes a title attribute when the source gave one, escaped for the
// attribute context.
func writeTitle(w util.BufWriter, title []byte) {
	if title == nil {
		return
	}
	_, _ = w.WriteString(` title="`)
	html.DefaultWriter.Write(w, title)
	_ = w.WriteByte('"')
}

// opensInNewTab reports whether a destination is an absolute http or https URL,
// the scheme compared without regard to case, or a network-path reference.
func opensInNewTab(href []byte) bool {
	return hasPrefixFold(href, "http:") || hasPrefixFold(href, "https:") || bytes.HasPrefix(href, []byte("//"))
}

// hasPrefixFold reports whether s begins with the ASCII prefix, ignoring case.
func hasPrefixFold(s []byte, prefix string) bool {
	return len(s) >= len(prefix) && bytes.EqualFold(s[:len(prefix)], []byte(prefix))
}

// writeNodeText writes the escaped text content of a node's inline children:
// the alternative text of an image.
func writeNodeText(w util.BufWriter, source []byte, n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Text:
			html.DefaultWriter.Write(w, t.Segment.Value(source))
			if t.SoftLineBreak() || t.HardLineBreak() {
				_ = w.WriteByte(' ')
			}
		case *ast.String:
			if t.IsCode() {
				_, _ = w.Write(util.EscapeHTML(t.Value))
			} else {
				html.DefaultWriter.Write(w, t.Value)
			}
		default:
			writeNodeText(w, source, c)
		}
	}
}

// renderTaskMarker renders a task-list checkbox, in the non-interactive form, as
// the text marker [x] or [ ], wrapped in a <span> carrying the fixed class
// task-list-marker so the stylesheet can place it where the list marker would be
// (SPEC/WEB.md § Markdown Rendering, rules 13 and 14). The space that follows
// the marker is the one the ordinary form's checkbox is followed by.
func renderTaskMarker(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	marker := `<span class="task-list-marker">[ ]</span> `
	if node.(*extast.TaskCheckBox).IsChecked { // registered for KindTaskCheckBox only
		marker = `<span class="task-list-marker">[x]</span> `
	}
	_, err := w.WriteString(marker)
	return ast.WalkContinue, err
}

// renderFootnoteRefText renders a footnote reference, in the non-interactive
// form, as its number alone: the <sup> keeps its prefixed identifier, and no <a>
// is emitted.
func renderFootnoteRefText(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*extast.FootnoteLink) // registered for KindFootnoteLink only
	index := strconv.Itoa(n.Index)
	_, _ = w.WriteString(`<sup id="`)
	_, _ = w.Write(footnoteIDPrefix(node))
	_, _ = w.WriteString("fnref")
	if n.RefIndex > 0 {
		_, _ = w.WriteString(strconv.Itoa(n.RefIndex))
	}
	_, _ = w.WriteString(":" + index + `">` + index)
	_, err := w.WriteString("</sup>")
	return ast.WalkContinue, err
}

// renderFootnoteBacklinkText renders a footnote back-link, in the
// non-interactive form, as its arrow alone, with no <a>.
func renderFootnoteBacklinkText(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	_, err := w.WriteString("&#160;" + footnoteBacklinkText)
	return ast.WalkContinue, err
}

// markdownFuncMap is the template helpers through which a server-rendered page
// obtains a Markdown field's HTML from the one renderer, as trusted HTML
// (SPEC/WEB.md § Markdown Rendering, rules 1, 13, and 15). The template places
// each result in its <div class="markdown"> container.
var markdownFuncMap = map[string]any{
	"sprintDescriptionHTML":     sprintDescriptionHTML,
	"sprintCardDescriptionHTML": sprintCardDescriptionHTML,
	"sprintCommentBodyHTML":     sprintCommentBodyHTML,
	"commentBodyHTML":           commentBodyHTML,
}

// sprintDescriptionHTML renders a sprint's description on the roadmap sprint
// page, in the ordinary form.
//
//nolint:gocritic // html/template passes the sprint by value
func sprintDescriptionHTML(s models.Sprint) (template.HTML, error) {
	return trustedMarkdown(s.Description, sprintDescriptionIDPrefix(s.ID), markdownInteractive)
}

// sprintCardDescriptionHTML renders a sprint's description in its sprint card,
// in the non-interactive form, because the whole card is one link (SPEC/WEB.md
// § Shared Sprint-Card Partial, rule 5).
//
//nolint:gocritic // html/template passes the sprint by value
func sprintCardDescriptionHTML(s models.Sprint) (template.HTML, error) {
	return trustedMarkdown(s.Description, sprintDescriptionIDPrefix(s.ID), markdownNonInteractive)
}

// sprintCommentBodyHTML renders a sprint comment's body in the sprint Comments
// card, in the ordinary form.
//
//nolint:gocritic // html/template passes the comment by value
func sprintCommentBodyHTML(c models.SprintComment) (template.HTML, error) {
	return trustedMarkdown(c.Body, sprintCommentIDPrefix(c.ID), markdownInteractive)
}

// taskCommentBodyHTML renders a task comment's body in the Comments card of the
// Roadmap Task Page, in the ordinary form.
//
//nolint:gocritic // html/template passes the comment by value
func taskCommentBodyHTML(c models.TaskComment) (template.HTML, error) {
	return trustedMarkdown(c.Body, taskCommentIDPrefix(c.ID), markdownInteractive)
}

// errUnsupportedComment reports a value handed to commentBodyHTML that is neither
// kind of comment, which only a template wiring error produces.
var errUnsupportedComment = errors.New("commentBodyHTML: unsupported comment value")

// commentBodyHTML renders the body of either kind of comment for the one shared
// comment timeline partial, which the sprint page and the task page both invoke:
// a sprint comment through sprintCommentBodyHTML and a task comment through
// taskCommentBodyHTML, each with its own footnote identifier prefix (SPEC/WEB.md
// § Markdown Rendering, rule 12). Any other value is a template wiring error and
// fails the render rather than showing a body it cannot identify.
func commentBodyHTML(comment any) (template.HTML, error) {
	switch c := comment.(type) {
	case models.SprintComment:
		return sprintCommentBodyHTML(c)
	case models.TaskComment:
		return taskCommentBodyHTML(c)
	default:
		return "", fmt.Errorf("%w: %T", errUnsupportedComment, comment)
	}
}

// trustedMarkdown marks the renderer's output as trusted HTML. This conversion
// is the one place the web interface inserts a value into a page without
// escaping, and the value is the renderer's output and nothing else
// (SPEC/WEB.md § Markdown Rendering, rule 15).
func trustedMarkdown(source, idPrefix string, form markdownForm) (template.HTML, error) {
	out, err := renderMarkdown(source, idPrefix, form)
	if err != nil {
		return "", err
	}
	return template.HTML(out), nil // #nosec G203 -- the renderer emits no raw HTML, no author attribute, and no dangerous link (SPEC/WEB.md § Markdown Rendering)
}
