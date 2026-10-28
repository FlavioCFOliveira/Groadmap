package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// This file is the gate for the typography of rendered Markdown, SPEC/WEB.md
// § Markdown Rendering, rules 3, 13, and 14, and § UI Framework, rule 4:
// Acceptance Criteria 195 to 203. The stylesheet is read as served from
// /static/style.css and parsed rule by rule, so a declaration is asserted on the
// selector that carries it rather than found anywhere in the file. What only a
// browser can compute — the cascade against the vendored Tabler rules, marker
// positions, and the loaded font face — is verified in a real browser and is
// out of reach of a unit test.

// servedProjectSheet returns /static/style.css as the server serves it.
func servedProjectSheet(t *testing.T) string {
	t.Helper()
	return servePage(t, buildMux(), "/static/style.css")
}

// soleDeclaration returns the one value prop has across the unconditional rules
// for selector, failing the test when there is not exactly one declaration: two
// would leave the applied value to the order of the rules, which the assertion
// refuses to guess. A selector may appear in several rules that declare
// different properties (a list's marker rule and its margin rule).
func soleDeclaration(t *testing.T, sheet, selector, prop string) string {
	t.Helper()
	blocks := cssRuleBlocks(sheet, selector)
	values := make([]string, 0, len(blocks))
	for _, block := range blocks {
		values = append(values, cssDeclarations(block, prop)...)
	}
	if len(values) != 1 {
		t.Fatalf("%s declares %s %v, want exactly one declaration", selector, prop, values)
	}
	return values[0]
}

// assertDeclarations checks every prop: value pair on the one unconditional
// rule for each selector.
func assertDeclarations(t *testing.T, sheet string, selectors []string, want map[string]string) {
	t.Helper()
	for _, selector := range selectors {
		for prop, value := range want {
			if got := soleDeclaration(t, sheet, selector, prop); got != value {
				t.Errorf("%s { %s: %s }, want %s", selector, prop, got, value)
			}
		}
	}
}

// selectorSubject returns the last compound selector of a complex selector —
// the element the rule styles.
func selectorSubject(selector string) string {
	norm := normaliseSelector(selector)
	cut := strings.LastIndexAny(norm, " >+~")
	return norm[cut+1:]
}

// subjectIsElement reports whether a compound selector's type selector is tag.
func subjectIsElement(compound, tag string) bool {
	if !strings.HasPrefix(compound, tag) {
		return false
	}
	rest := compound[len(tag):]
	return rest == "" || strings.ContainsRune(":.[#", rune(rest[0]))
}

// markdownScoped reports whether a selector is scoped to the .markdown
// container.
func markdownScoped(selector string) bool {
	return regexp.MustCompile(`(^|[\s>+~(])\.markdown([\s>+~:.\[)]|$)`).MatchString(normaliseSelector(selector))
}

// TestMarkdownTypography_LineHeightAndBlockSpacing is the stylesheet half of
// Acceptance Criterion 195: the container carries the body line height, a
// table carries the 1rem bottom margin of every other block, and no rule scoped
// to .markdown gives a paragraph, a code block, or a definition description a
// vertical margin of its own. The dd indent of Acceptance Criterion 200 is a
// horizontal margin and is the one margin a dd rule may carry.
func TestMarkdownTypography_LineHeightAndBlockSpacing(t *testing.T) {
	sheet := servedProjectSheet(t)
	if got := soleDeclaration(t, sheet, ".markdown", "line-height"); got != "var(--tblr-body-line-height)" {
		t.Errorf(".markdown { line-height: %s }, want var(--tblr-body-line-height)", got)
	}
	if got := soleDeclaration(t, sheet, ".markdown table", "margin-bottom"); got != "1rem" {
		t.Errorf(".markdown table { margin-bottom: %s }, want 1rem", got)
	}

	vertical := []string{"margin", "margin-top", "margin-bottom", "margin-block", "margin-block-start", "margin-block-end"}
	for _, rule := range parseCSSRules(sheet) {
		for _, selector := range strings.Split(rule.prelude, ",") {
			if !markdownScoped(selector) {
				continue
			}
			subject := selectorSubject(selector)
			for _, tag := range []string{"p", "pre", "dd"} {
				if !subjectIsElement(subject, tag) {
					continue
				}
				for _, prop := range vertical {
					if values := cssDeclarations(rule.decls, prop); len(values) > 0 {
						t.Errorf("%s sets %s %v; a %s keeps the interface's base margin", normaliseSelector(selector), prop, values, tag)
					}
				}
				if tag == "dd" {
					if values := cssDeclarations(rule.decls, "margin-inline"); len(values) > 0 {
						t.Errorf("%s sets margin-inline %v", normaliseSelector(selector), values)
					}
				}
			}
		}
	}
}

// TestMarkdownTypography_Lists is the stylesheet half of Acceptance Criterion
// 196: the marker depends only on the lists of the container, and so do the
// margins — 1rem below a list inside no other list, 0 below a nested one, and a
// 0 top margin on both.
func TestMarkdownTypography_Lists(t *testing.T) {
	sheet := servedProjectSheet(t)
	assertDeclarations(t, sheet, []string{".markdown ul"}, map[string]string{"list-style-type": "disc"})
	assertDeclarations(t, sheet, []string{".markdown ul ul", ".markdown ol ul"}, map[string]string{"list-style-type": "circle"})
	assertDeclarations(t, sheet, []string{".markdown ul ul ul", ".markdown ul ol ul", ".markdown ol ul ul", ".markdown ol ol ul"},
		map[string]string{"list-style-type": "square"})

	// The margin rules: selected by the property, because the marker rules
	// above share their selectors.
	marginRules := map[string]map[string]string{}
	for _, rule := range parseCSSRules(sheet) {
		if rule.nested {
			continue
		}
		for _, prop := range []string{"margin-top", "margin-bottom"} {
			for _, v := range cssDeclarations(rule.decls, prop) {
				for _, s := range strings.Split(rule.prelude, ",") {
					s = normaliseSelector(s)
					if marginRules[s] == nil {
						marginRules[s] = map[string]string{}
					}
					marginRules[s][prop] = v
				}
			}
		}
	}
	for _, s := range []string{".markdown ul", ".markdown ol"} {
		if got := marginRules[s]; got["margin-top"] != "0" || got["margin-bottom"] != "1rem" {
			t.Errorf("%s margins %v, want margin-top 0 and margin-bottom 1rem", s, got)
		}
	}
	for _, s := range []string{".markdown ul ul", ".markdown ul ol", ".markdown ol ul", ".markdown ol ol"} {
		if got := marginRules[s]; got["margin-bottom"] != "0" {
			t.Errorf("%s margins %v, want margin-bottom 0", s, got)
		}
	}
	// A list's own margins are set on the list, never on the list item.
	for s, m := range marginRules {
		if markdownScoped(s) && subjectIsElement(selectorSubject(s), "li") {
			t.Errorf("%s sets a list-item margin %v; a list item carries none", s, m)
		}
	}
}

// TestMarkdownTypography_Headings is the stylesheet half of Acceptance Criterion
// 197: the size and line-height scale, the h6 colour, the top-level top margin of
// 0, and no size below body text. The heading levels of Acceptance Criterion 187
// are unchanged at the renderer.
func TestMarkdownTypography_Headings(t *testing.T) {
	sheet := servedProjectSheet(t)
	scale := map[string]map[string]string{
		".markdown h4": {"font-size": "1rem", "line-height": "1.5rem"},
		".markdown h5": {"font-size": ".875rem", "line-height": "1.25rem"},
		".markdown h6": {"font-size": ".875rem", "line-height": "1.25rem", "color": "var(--tblr-secondary)"},
	}
	for selector, want := range scale {
		assertDeclarations(t, sheet, []string{selector}, want)
		size := strings.TrimSuffix(want["font-size"], "rem")
		if parseCSSNumber(size) < 0.875 {
			t.Errorf("%s is smaller than body text (.875rem)", selector)
		}
	}
	assertDeclarations(t, sheet, []string{".markdown > h4", ".markdown > h5", ".markdown > h6"}, map[string]string{"margin-top": "0"})

	out := md(t, "# One\n\n## Two\n\n### Three\n\n#### Four")
	for _, want := range []string{"<h4>One</h4>", "<h5>Two</h5>", "<h6>Three</h6>", "<h6>Four</h6>"} {
		if !strings.Contains(out, want) {
			t.Errorf("the heading levels changed: %q missing from\n%s", want, out)
		}
	}
}

// relativeLuminance is the WCAG 2.2 relative luminance of an sRGB colour.
func relativeLuminance(r, g, b float64) float64 {
	channel := func(c float64) float64 {
		c /= 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

// contrastRatio is the WCAG 2.2 contrast ratio of two luminances.
func contrastRatio(a, b float64) float64 {
	return (math.Max(a, b) + 0.05) / (math.Min(a, b) + 0.05)
}

// TestMarkdownTypography_FontSize is the stylesheet half of Acceptance Criterion
// 220: the container is set at body-text size, in place of the vendored 1rem,
// and a code block at the .85714285em of the vendored pre rule, in place of the
// vendored .markdown .8125em.
func TestMarkdownTypography_FontSize(t *testing.T) {
	sheet := servedProjectSheet(t)
	assertDeclarations(t, sheet, []string{".markdown"}, map[string]string{"font-size": "var(--tblr-body-font-size)"})
	assertDeclarations(t, sheet, []string{".markdown pre"}, map[string]string{"font-size": ".85714285em"})
}

// TestMarkdownTypography_Links is the stylesheet half of Acceptance Criterion
// 198: the two link colours, the underline and its offset, the hover and
// focus-visible state, the contrast of both colours against the dark card
// surface, and no link colour set outside the .markdown container.
func TestMarkdownTypography_Links(t *testing.T) {
	sheet := servedProjectSheet(t)
	assertDeclarations(t, sheet, []string{".markdown a"}, map[string]string{
		"color": "rgb(121, 170, 231)", "text-decoration-line": "underline", "text-underline-offset": ".15em",
	})
	assertDeclarations(t, sheet, []string{".markdown a:hover", ".markdown a:focus-visible"}, map[string]string{
		"color": "rgb(148, 187, 237)", "text-decoration-line": "none",
	})

	surface := relativeLuminance(0x26, 0x26, 0x26)
	for _, tc := range []struct {
		r, g, b float64
		want    string
	}{{121, 170, 231, "6.28"}, {148, 187, 237, "7.63"}} {
		ratio := contrastRatio(relativeLuminance(tc.r, tc.g, tc.b), surface)
		if ratio < 4.5 {
			t.Errorf("rgb(%v, %v, %v) has a contrast of %.2f:1 against #262626, below 4.5:1", tc.r, tc.g, tc.b, ratio)
		}
		if got := strconv.FormatFloat(ratio, 'f', 2, 64); got != tc.want {
			t.Errorf("rgb(%v, %v, %v) has a contrast of %s:1, the SPEC states %s:1", tc.r, tc.g, tc.b, got, tc.want)
		}
	}

	for _, rule := range parseCSSRules(sheet) {
		for _, selector := range strings.Split(rule.prelude, ",") {
			if markdownScoped(selector) {
				continue
			}
			if subjectIsElement(selectorSubject(selector), "a") && len(cssDeclarations(rule.decls, "color")) > 0 {
				t.Errorf("%s sets the colour of a link outside .markdown", normaliseSelector(selector))
			}
			for _, decl := range splitCSSDeclarations(rule.decls) {
				name, _, _ := strings.Cut(decl, ":")
				if strings.HasPrefix(strings.TrimSpace(name), "--tblr-link") {
					t.Errorf("%s changes the link-colour custom property %s", normaliseSelector(selector), strings.TrimSpace(name))
				}
			}
		}
	}
}

// TestMarkdownTypography_BlockquoteRuleFootnotes is the stylesheet half of
// Acceptance Criterion 199.
func TestMarkdownTypography_BlockquoteRuleFootnotes(t *testing.T) {
	sheet := servedProjectSheet(t)
	assertDeclarations(t, sheet, []string{".markdown blockquote"}, map[string]string{
		"font-size": "var(--tblr-body-font-size)", "margin": "0 0 1rem", "padding": "1rem 1rem 1rem", "color": "var(--tblr-secondary)",
	})
	assertDeclarations(t, sheet, []string{".markdown hr"}, map[string]string{"margin": "2rem 0"})
	assertDeclarations(t, sheet, []string{".markdown .footnotes"}, map[string]string{"font-size": ".8125rem", "color": "var(--tblr-secondary)"})
	// The footnotes list the renderer emits is the <div class="footnotes"> the rule
	// styles.
	if out := md(t, "Cent[^1].\n\n[^1]: Twice."); !strings.Contains(out, `<div class="footnotes" role="doc-endnotes">`) {
		t.Errorf("the renderer does not emit the footnotes container:\n%s", out)
	}
}

// TestMarkdownTypography_TaskListDefinitionCode is the stylesheet half of
// Acceptance Criterion 200: ONE rule removes the marker of a task-list item in
// both forms, a dd is indented, and a tab is four columns wide.
func TestMarkdownTypography_TaskListDefinitionCode(t *testing.T) {
	sheet := servedProjectSheet(t)
	var markerless []string
	for _, rule := range parseCSSRules(sheet) {
		if rule.nested {
			continue
		}
		for _, v := range cssDeclarations(rule.decls, "list-style-type") {
			if v == "none" {
				markerless = append(markerless, normaliseSelector(rule.prelude))
			}
		}
	}
	if len(markerless) != 1 || markerless[0] != ".markdown .task-list-item" {
		t.Errorf("rules removing the list marker: %q, want exactly [.markdown .task-list-item]", markerless)
	}
	// The checkbox and the text marker share the one placement rule.
	placement := soleCSSRule(t, sheet, `.markdown .task-list-item input[type="checkbox"]`)
	if placement != soleCSSRule(t, sheet, ".markdown .task-list-item .task-list-marker") {
		t.Error("the checkbox and the task-list-marker span are placed by different rules")
	}
	if got := cssDeclarations(placement, "margin-left"); len(got) != 1 || !strings.HasPrefix(got[0], "-") {
		t.Errorf("the task-list marker placement is %v, want one negative margin-left into the marker box", got)
	}
	assertDeclarations(t, sheet, []string{".markdown dd"}, map[string]string{"margin-left": "1.5rem"})
	assertDeclarations(t, sheet, []string{".markdown pre"}, map[string]string{"tab-size": "4"})
}

// TestMarkdown_TaskListItemClassBothForms is the renderer half of Acceptance
// Criterion 200 and rules 3 and 14: in both forms the <li> of a task-list item
// — tight, loose, nested, or ordered — carries task-list-item, no other <li>
// does; in the non-interactive form the marker is the text of a
// task-list-marker span and the item still reads "[x] text".
func TestMarkdown_TaskListItemClassBothForms(t *testing.T) {
	src := strings.Join([]string{
		"- ordinary item",
		"- [x] tight checked",
		"- [ ] tight open",
		"  - nested ordinary with [x] inside",
		"  - [x] nested task",
		"",
		"1. [ ] ordered task",
		"2. ordered ordinary",
		"",
		"- [x] loose task",
		"",
		"- loose ordinary",
		"",
		"Text with `[x]` and [ ] in a paragraph.",
	}, "\n")
	const tasks, items = 5, 9

	liRe := regexp.MustCompile(`<li[^>]*>`)
	for _, form := range []markdownForm{markdownInteractive, markdownNonInteractive} {
		out := mdForm(t, src, "task-42-acceptance_criteria-", form)
		lis := liRe.FindAllString(out, -1)
		if len(lis) != items {
			t.Fatalf("form %d: %d <li>, want %d:\n%s", form, len(lis), items, out)
		}
		classed := 0
		for _, li := range lis {
			switch li {
			case `<li class="task-list-item">`:
				classed++
			case "<li>":
			default:
				t.Errorf("form %d: unexpected list item %s", form, li)
			}
		}
		if classed != tasks {
			t.Errorf("form %d: %d task-list items, want %d:\n%s", form, classed, tasks, out)
		}
		markers := strings.Count(out, `<span class="task-list-marker">`)
		inputs := strings.Count(out, "<input ")
		switch form {
		case markdownInteractive:
			if markers != 0 || inputs != tasks {
				t.Errorf("ordinary form: %d marker spans and %d checkboxes, want 0 and %d", markers, inputs, tasks)
			}
		case markdownNonInteractive:
			if markers != tasks || inputs != 0 {
				t.Errorf("non-interactive form: %d marker spans and %d checkboxes, want %d and 0", markers, inputs, tasks)
			}
			for _, want := range []string{
				`<li class="task-list-item"><span class="task-list-marker">[x]</span> tight checked</li>`,
				`<li class="task-list-item"><span class="task-list-marker">[ ]</span> tight open`,
				`<li class="task-list-item"><span class="task-list-marker">[ ]</span> ordered task</li>`,
				"<li class=\"task-list-item\">\n<p><span class=\"task-list-marker\">[x]</span> loose task</p>",
				"<li>nested ordinary with [x] inside</li>",
				"<code>[x]</code> and [ ] in a paragraph.",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("non-interactive form lacks %q:\n%s", want, out)
				}
			}
			// The visible text still reads "[x] text".
			text := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(out, "")
			for _, want := range []string{"[x] tight checked", "[ ] tight open", "[x] nested task", "[x] loose task"} {
				if !strings.Contains(text, want) {
					t.Errorf("the visible text lacks %q:\n%s", want, text)
				}
			}
		}
	}
}

// TestMarkdownTypography_EmphasisAndInterItalic is Acceptance Criterion 201
// outside the browser: bold is 700, the Inter stylesheet declares the upright
// and the italic face under both family names, the italic file is embedded and
// served byte for byte, and LICENSES.md records it beside the upright file.
func TestMarkdownTypography_EmphasisAndInterItalic(t *testing.T) {
	sheet := servedProjectSheet(t)
	assertDeclarations(t, sheet, []string{".markdown strong", ".markdown b"}, map[string]string{"font-weight": "700"})

	mux := buildMux()
	inter := servePage(t, mux, "/static/vendor/inter/inter.css")
	type face struct{ family, style, weight, src string }
	openings := regexp.MustCompile(`@font-face\s*\{`).FindAllStringIndex(inter, -1)
	faces := make([]face, 0, len(openings))
	for _, at := range openings {
		body, _ := cssBlockBody(inter, at[1]-1)
		one := func(prop string) string {
			v := cssDeclarations(body, prop)
			if len(v) != 1 {
				t.Fatalf("an @font-face rule declares %s %v, want exactly one", prop, v)
			}
			return v[0]
		}
		faces = append(faces, face{one("font-family"), one("font-style"), one("font-weight"), one("src")})
	}
	want := make([]face, 0, 4)
	for _, family := range []string{"'Inter Var'", "'Inter'"} {
		want = append(want,
			face{family, "normal", "100 900", "url('./files/inter-latin-wght-normal.woff2') format('woff2')"},
			face{family, "italic", "100 900", "url('./files/inter-latin-wght-italic.woff2') format('woff2')"})
	}
	if len(faces) != len(want) {
		t.Fatalf("inter.css holds %d @font-face rules, want %d: %+v", len(faces), len(want), faces)
	}
	for _, w := range want {
		found := false
		for _, f := range faces {
			found = found || f == w
		}
		if !found {
			t.Errorf("inter.css lacks the face %+v; it holds %+v", w, faces)
		}
	}

	const italicPath = "static/vendor/inter/files/inter-latin-wght-italic.woff2"
	committed, err := os.ReadFile(italicPath)
	if err != nil {
		t.Fatalf("reading the committed italic face: %v", err)
	}
	// The italic face of @fontsource-variable/inter, byte-identical in every
	// release from 5.2.6 to 5.3.0, the releases whose upright face is the
	// committed upright file.
	const italicSHA256 = "7291b5970da2237441273c03b424a504b70b18f09791473fab99687dcc314720"
	if sum := sha256.Sum256(committed); hex.EncodeToString(sum[:]) != italicSHA256 {
		t.Errorf("the committed italic face has sha256 %x, want the vendored upstream file %s", sum, italicSHA256)
	}
	if !bytes.HasPrefix(committed, []byte("wOF2")) {
		t.Error("the committed italic face is not a WOFF2 file")
	}
	embedded, err := staticFS.ReadFile(italicPath)
	if err != nil || !bytes.Equal(embedded, committed) {
		t.Fatalf("the embedded italic face differs from the committed file (err %v)", err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/vendor/inter/files/inter-latin-wght-italic.woff2", nil))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), committed) {
		t.Errorf("GET the italic face: status %d, %d bytes; want 200 and the committed %d bytes", rec.Code, rec.Body.Len(), len(committed))
	}

	licences := readEmbeddedAsset(t, "static/vendor/LICENSES.md")
	var row string
	for _, line := range strings.Split(licences, "\n") {
		if strings.Contains(line, "inter-latin-wght-italic.woff2") {
			row = line
		}
	}
	if !strings.Contains(row, "inter-latin-wght-normal.woff2") || !strings.Contains(row, "SIL Open Font License 1.1") {
		t.Errorf("LICENSES.md does not list the italic face beside the upright one under the SIL OFL 1.1: %q", row)
	}
}

// TestMarkdownTypography_FullContainerWidth is Acceptance Criterion 202 outside
// the browser. It reads every stylesheet served under /static/... and checks two
// things. First, no rule, in any media condition, whose selector has a .markdown
// element as its subject sets max-width to a value other than none; a rule whose
// subject is a descendant of the container (.markdown table, .markdown pre) does
// not style the container and is not counted. Second, every .markdown container
// the interface renders — in a sprint card of the sprints page, the sprint
// description and every sprint comment body of the sprint page, and the four
// Markdown fields and every comment body of the task page — is described with
// its served ancestors and matched against the rules at every width of
// contrastWidths, and no matching rule sets max-width to a value other than
// none. The width the container then takes is the browser's to compute.
func TestMarkdownTypography_FullContainerWidth(t *testing.T) {
	rules := servedStaticCascadeRules(t)

	for i := range rules {
		rule := &rules[i]
		for _, decl := range rule.decls {
			if decl.prop != "max-width" || strings.TrimSpace(decl.value) == "none" {
				continue
			}
			for j := range rule.selectors {
				if selectorSubjectIsMarkdown(&rule.selectors[j]) {
					t.Errorf("a rule %s (media %q) whose subject is .markdown sets max-width: %s, want none or no declaration",
						selectorClasses(&rule.selectors[j]), rule.media, decl.value)
				}
			}
		}
	}

	t.Setenv("HOME", shortHome(t))
	f := seedMarkdownFixture(t, "settlement-runbook")
	mux := buildMux()
	surfaces := []struct {
		name, path string
		want       int // the number of .markdown containers the page renders; 0 means at least one
		inside     string
	}{
		{"sprints page", "/roadmaps/" + f.name + "/sprints", 0, "card-link"},
		{"sprint page", "/roadmaps/" + f.name + "/sprints/" + itoa(f.openSprintID), 1 + len(f.sprintCommentIDs), "card-body"},
		{"task page", "/roadmaps/" + f.name + "/tasks/" + itoa(f.taskID), 4 + len(f.taskCommentIDs), "card-body"},
	}
	for _, s := range surfaces {
		containers := markdownContainers(t, servePage(t, mux, s.path))
		if (s.want == 0 && len(containers) == 0) || (s.want > 0 && len(containers) != s.want) {
			t.Errorf("%s: %d .markdown containers, want %d (0 means at least one)", s.name, len(containers), s.want)
		}
		for n, el := range containers {
			if !elementWithin(el, s.inside) {
				t.Errorf("%s: .markdown container %d is not inside a .%s", s.name, n, s.inside)
			}
			for _, width := range contrastWidths {
				matches, err := newCSSCascade(rules, width).matching(el, "")
				if err != nil {
					t.Fatalf("%s at %vpx: %v", s.name, width, err)
				}
				// Falsifiability: a container described so that no rule matches it
				// would pass the max-width check vacuously.
				if !matchesClassRule(matches, "markdown") {
					t.Fatalf("%s at %vpx: no .markdown rule matches container %d; the description is wrong", s.name, width, n)
				}
				for _, m := range matches {
					for _, decl := range m.rule.decls {
						if decl.prop == "max-width" && strings.TrimSpace(decl.value) != "none" {
							t.Errorf("%s at %vpx: container %d is matched by a rule setting max-width: %s",
								s.name, width, n, decl.value)
						}
					}
				}
			}
		}
	}
}

// servedStaticCascadeRules fetches through the server every stylesheet the
// embedded static set holds, so a sheet added under /static/... is read without
// this list being edited, and parses them into one list of rules.
func servedStaticCascadeRules(t *testing.T) []cascadeRule {
	t.Helper()
	mux := buildMux()
	var rules []cascadeRule
	sheets := 0
	err := fs.WalkDir(staticSubFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".css") {
			return err
		}
		sheets++
		var perr error
		if rules, perr = parseCascadeSheet(servePage(t, mux, "/static/"+path), rules); perr != nil {
			return fmt.Errorf("parsing /static/%s: %w", path, perr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sheets < 2 || len(rules) < 100 {
		t.Fatalf("%d stylesheets parsed to %d rules; the check over them would be vacuous", sheets, len(rules))
	}
	return rules
}

// selectorSubjectIsMarkdown reports whether a selector's subject — its last
// compound — requires the markdown class, directly or through an :is(),
// :where(), or :matches() argument. A class named only inside :not() excludes
// the container rather than selecting it.
func selectorSubjectIsMarkdown(sel *cascadeSelector) bool {
	if len(sel.compounds) == 0 {
		return false
	}
	return compoundNamesClass(&sel.compounds[len(sel.compounds)-1], "markdown")
}

func compoundNamesClass(c *cascadeCompound, class string) bool {
	if containsString(c.classes, class) {
		return true
	}
	for i := range c.pseudos {
		p := &c.pseudos[i]
		if p.name != "is" && p.name != "where" && p.name != "matches" && p.name != "-webkit-any" {
			continue
		}
		for j := range p.list {
			if selectorSubjectIsMarkdown(&p.list[j]) {
				return true
			}
		}
	}
	return false
}

// selectorClasses renders a parsed selector's compounds by tag and classes, for
// a failure message; pseudo-classes are elided.
func selectorClasses(sel *cascadeSelector) string {
	parts := make([]string, 0, len(sel.compounds))
	for i := range sel.compounds {
		c := &sel.compounds[i]
		parts = append(parts, c.tag+strings.Join(append([]string{""}, c.classes...), "."))
	}
	return strings.Join(parts, " ")
}

// matchesClassRule reports whether any matching rule has a selector whose
// subject is exactly the one class.
func matchesClassRule(matches []cascadeMatch, class string) bool {
	for _, m := range matches {
		for _, sel := range m.rule.selectors {
			if len(sel.compounds) == 1 && sel.compounds[0].tag == "" &&
				len(sel.compounds[0].classes) == 1 && sel.compounds[0].classes[0] == class {
				return true
			}
		}
	}
	return false
}

// elementWithin reports whether an ancestor of el carries class.
func elementWithin(el *cssElement, class string) bool {
	for p := el.parent; p != nil; p = p.parent {
		if p.hasClass(class) {
			return true
		}
	}
	return false
}

var (
	htmlTagPattern  = regexp.MustCompile(`(?s)<!--.*?-->|<(/?)([a-zA-Z][a-zA-Z0-9-]*)((?:[^>"']|"[^"]*"|'[^']*')*)>`)
	htmlAttrPattern = regexp.MustCompile(`([a-zA-Z_:][-a-zA-Z0-9_:.]*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+)))?`)
	htmlVoidTags    = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true,
		"img": true, "input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true}
	htmlRawTextTags = map[string]bool{"script": true, "style": true, "textarea": true, "title": true}
)

// markdownContainers walks a served page's markup with a stack of open elements
// and returns every element whose class list holds markdown, described as a
// cssElement chain carrying each ancestor's tag, id, classes, and attributes, so
// the cascade matches it in the context the page actually renders it in. The
// served pages are well formed, so an end tag closes the innermost open element
// of its name.
func markdownContainers(t *testing.T, page string) []*cssElement {
	t.Helper()
	var (
		stack []*cssElement
		found []*cssElement
	)
	for i := 0; i < len(page); {
		loc := htmlTagPattern.FindStringSubmatchIndex(page[i:])
		if loc == nil {
			break
		}
		whole := page[i+loc[0] : i+loc[1]]
		next := i + loc[1]
		if strings.HasPrefix(whole, "<!--") {
			i = next
			continue
		}
		closing := loc[3] > loc[2]
		tag := strings.ToLower(page[i+loc[4] : i+loc[5]])
		if closing {
			for k := len(stack) - 1; k >= 0; k-- {
				if stack[k].tag == tag {
					stack = stack[:k]
					break
				}
			}
			i = next
			continue
		}
		el := &cssElement{tag: tag, attrs: map[string]string{}, state: map[string]bool{}}
		for _, a := range htmlAttrPattern.FindAllStringSubmatch(page[i+loc[6]:i+loc[7]], -1) {
			name := strings.ToLower(a[1])
			value := a[2] + a[3] + a[4]
			el.attrs[name] = value
			switch name {
			case "class":
				el.classes = strings.Fields(value)
			case "id":
				el.id = value
			}
		}
		if len(stack) > 0 {
			el.parent = stack[len(stack)-1]
		}
		if el.hasClass("markdown") {
			found = append(found, el)
		}
		switch {
		case htmlVoidTags[tag] || strings.HasSuffix(whole, "/>"):
		case htmlRawTextTags[tag]:
			end := strings.Index(strings.ToLower(page[next:]), "</"+tag)
			if end < 0 {
				t.Fatalf("unterminated <%s> element", tag)
			}
			next += end
			stack = append(stack, el)
		default:
			stack = append(stack, el)
		}
		i = next
	}
	return found
}

// TestMarkdownTypography_ScopedAndMarkupUnchanged is Acceptance Criterion 203:
// every rule whose selector list names .markdown is scoped to it selector by
// selector, and the only markup the typography adds to the renderer's output is
// the task-list-item class and the task-list-marker span. Acceptance Criteria
// 180 to 194 keep their own tests, in markdown_test.go.
func TestMarkdownTypography_ScopedAndMarkupUnchanged(t *testing.T) {
	sheet := servedProjectSheet(t)
	for _, rule := range parseCSSRules(sheet) {
		if !strings.Contains(rule.prelude, ".markdown") {
			continue
		}
		for _, selector := range strings.Split(rule.prelude, ",") {
			if !markdownScoped(selector) {
				t.Errorf("%q shares a rule with .markdown selectors but is not scoped to .markdown", normaliseSelector(selector))
			}
		}
	}

	src := allConstructs("groadmap")
	strip := regexp.MustCompile(` class="task-list-item"|<span class="task-list-marker">(\[[x ]\])</span>`)
	for _, form := range []markdownForm{markdownInteractive, markdownNonInteractive} {
		out := mdForm(t, src, "task-42-acceptance_criteria-", form)
		if styleAttrRe.MatchString(out) {
			t.Errorf("form %d carries a style attribute", form)
		}
		bare := strip.ReplaceAllString(out, "$1")
		if strings.Contains(bare, "task-list") {
			t.Errorf("form %d carries a task-list class outside the task-list markup:\n%s", form, out)
		}
		if form == markdownNonInteractive && !strings.Contains(bare, "<li>[x] ") {
			t.Errorf("the non-interactive form without its span does not read as before:\n%s", bare)
		}
		if form == markdownInteractive && !strings.Contains(bare, `<li><input checked="" disabled="" type="checkbox"> `) {
			t.Errorf("the ordinary form without its class does not read as before:\n%s", bare)
		}
	}
}
