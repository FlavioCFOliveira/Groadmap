package web

import (
	"slices"
	"strings"
	"testing"
)

// The guards in this file cover SPEC/WEB.md § Full-Height Page Regions for the
// Kanban board of the roadmap tasks page (Acceptance Criteria 124 to 128).
//
// What they are written against. The board used to size itself with
// `height: calc(100vh - 14rem)` — the viewport height less a fixed reservation
// for "the Tabler navbar, page header, and footer". The footer had been removed
// from every page and the reservation was never reduced, and the page header's
// height is not a constant either: its search box and three filter dropdowns
// wrap onto further rows as the viewport narrows. Measured in a browser at a
// viewport 900px tall, the material above the board occupied 144px at widths of
// 1600 and 1440, 228px at 1280 and 1100, 284px at 768 and 380px at 500 — so the
// 224px reservation was correct at NO width: it left 80px of viewport carrying
// nothing on a desktop and pushed the foot of the board up to 156px below the
// fold on a phone.
//
// What these guards can and cannot establish. The Go suite has no browser, and
// SPEC/BUILD.md rules out a JavaScript toolchain, so nothing here measures a
// rendered page: Acceptance Criteria 124, 125 and 127 are measurements and are
// checked with a browser against a running server, not from here. What IS
// checkable hermetically is the MECHANISM those measurements depend on, in the
// exact bytes the binary serves: that the board declares no viewport arithmetic
// of its own, that it grows into the space its parent leaves, that every link of
// the chain carrying that space down to it is present — in the vendored Tabler
// distribution as much as in the project sheet — and that the one viewport-derived
// height at the top of the chain is declared as the ordered `vh`/`dvh` pair
// Acceptance Criterion 126 requires. A layout that satisfies all of that can in
// principle still measure wrong; a layout that fails any of it cannot measure
// right, and restoring the fixed subtraction fails the first assertion outright.

// TestTaskBoard_DeclaresNoViewportArithmetic asserts the board computes no height
// of its own: it neither declares a `height` nor mentions a viewport unit in any
// other property, and instead grows into what its parent leaves.
//
// This is the assertion the defect fails. A board that subtracts a fixed length
// from the viewport height is right at one viewport width at best — the width its
// length was chosen for — and reserves space for elements the page may no longer
// render, which is exactly how the reservation for the removed footer survived it
// (SPEC/WEB.md § Full-Height Page Regions, rules 1 to 3).
func TestTaskBoard_DeclaresNoViewportArithmetic(t *testing.T) {
	sheet := projectStyleSheet(t)
	board := soleCSSRule(t, sheet, ".task-board")

	if values := cssDeclarations(board, "height"); len(values) != 0 {
		t.Errorf(".task-board declares height: %v; a full-height region takes the height "+
			"the page body leaves through the shell chain and computes none of its own "+
			"(SPEC/WEB.md § Full-Height Page Regions, rule 3)", values)
	}
	for _, unit := range []string{"vh", "dvh", "svh", "lvh", "vmin", "vmax"} {
		if cssMentionsUnit(board, unit) {
			t.Errorf(".task-board sizes itself against the viewport unit %q; the viewport "+
				"height enters this layout once, at the top of the shell chain, and the "+
				"board's height is computed from it rather than recalculated here", unit)
		}
	}

	// The positive half: where the height DOES come from. Without a grow factor
	// the board would collapse to its flex base size and the absences above
	// would be satisfied by a board with no height at all.
	if grow := cssFlexGrow(t, board, ".task-board"); grow < 1 {
		t.Errorf(".task-board has flex-grow %v; it must claim the space its parent "+
			"leaves (flex: 1)", grow)
	}

	// The rest of what the board's own rule must keep saying. Each is a
	// requirement the height change had the opportunity to drop.
	if len(cssDeclarations(board, "min-height")) == 0 {
		t.Error(".task-board declares no min-height; the floor is what keeps the board " +
			"usable on a very short viewport, where it stops following the page body and " +
			"the page scrolls to reach it (SPEC/WEB.md § Full-Height Page Regions, rule 5, " +
			"and Acceptance Criterion 127)")
	}
	if len(cssDeclarations(board, "padding-bottom")) == 0 {
		t.Error(".task-board declares no padding-bottom; the board's own horizontal " +
			"scrollbar is drawn in that reserved space, beneath the columns rather than " +
			"over the last card (Acceptance Criterion 128)")
	}
	for prop, want := range map[string]string{"overflow-x": "auto", "overflow-y": "hidden"} {
		if got := cssDeclarations(board, prop); len(got) != 1 || got[0] != want {
			t.Errorf(".task-board declares %s: %v, want exactly %q; the BOARD scrolls "+
				"horizontally inside its own container so the page never does, and it "+
				"never scrolls vertically because its columns do that individually",
				prop, got, want)
		}
	}
}

// TestFullHeightShell_DeclaresTheOrderedViewportPair asserts the one
// viewport-derived height in the chain is declared twice, first against the large
// viewport height and then against the dynamic one (Acceptance Criterion 126).
//
// The order is the whole mechanism: a browser that implements `dvh` applies the
// later declaration, and one that does not discards it and keeps the first, which
// is how the unit ships without an @supports block. Swap them and every browser
// takes `vh`; drop the first and a browser without `dvh` gets no viewport-derived
// height at all and the region collapses to its content. Sized against `vh`
// alone, a mobile browser measures the viewport as though the address bar were
// retracted, so the foot of the region sits below the fold for exactly as long as
// the bar is on screen — on precisely the devices the mobile-first requirement is
// written for (SPEC/WEB.md § Full-Height Page Regions, rule 4).
func TestFullHeightShell_DeclaresTheOrderedViewportPair(t *testing.T) {
	sheet := projectStyleSheet(t)
	shell := soleCSSRule(t, sheet, ".full-height-page .page")

	heights := cssDeclarations(shell, "height")
	if len(heights) != 2 {
		t.Fatalf(".full-height-page .page declares height %d time(s) (%v), want exactly 2: "+
			"the large viewport height first and the dynamic viewport height second",
			len(heights), heights)
	}
	if !strings.Contains(heights[0], "vh") || strings.Contains(heights[0], "dvh") {
		t.Errorf("the FIRST height declaration is %q; it must be the large viewport "+
			"height (vh), which is what a browser without dvh keeps", heights[0])
	}
	if !strings.Contains(heights[1], "dvh") {
		t.Errorf("the SECOND height declaration is %q; it must be the dynamic viewport "+
			"height (dvh), which is what a browser showing a retracting address bar "+
			"applies", heights[1])
	}
}

// TestFullHeightShell_ChainFromTheViewportToTheBoardIsComplete asserts every link
// between the height at the top of the shell and the board is present in the
// stylesheets the binary serves.
//
// Three of the four links are Tabler's own, which is the point: the project sheet
// supplies the definite height the distribution lacks and re-implements no layout
// Tabler already provides. That also makes those three a dependency on the
// vendored bytes, and this test is where it is recorded — re-vendoring a Tabler
// that no longer declares `.page-body { flex: 1 }` would leave the board sizing
// itself to its content with nothing else failing.
func TestFullHeightShell_ChainFromTheViewportToTheBoardIsComplete(t *testing.T) {
	project := projectStyleSheet(t)
	vendored := embeddedSheet(t, "static/vendor/tabler/tabler.min.css")

	links := []struct {
		sheet    string
		where    string
		selector string
		grows    bool
	}{
		// Tabler's shell: a flex column that hands what is left to the next link.
		// `.page` itself does not grow — it is where the definite height enters.
		{vendored, "the vendored Tabler distribution", ".page", false},
		{vendored, "the vendored Tabler distribution", ".page-wrapper", true},
		{vendored, "the vendored Tabler distribution", ".page-body", true},
		// The one link Tabler does not make: the page's own Bootstrap container,
		// a block box that would otherwise size to its content and break the
		// chain one link above the board.
		{project, "the project stylesheet", ".full-height-page .page-body > .container-xl", true},
	}

	for _, link := range links {
		block := cssRuleBlocks(link.sheet, link.selector)
		if len(block) == 0 {
			t.Errorf("%s declares no unconditional rule for %q, so the chain that carries "+
				"the viewport height down to the board is broken at that link",
				link.where, link.selector)
			continue
		}
		joined := strings.Join(block, ";")
		if got := cssDeclarations(joined, "display"); !containsValue(got, "flex") {
			t.Errorf("%s: %q declares display: %v, want flex", link.where, link.selector, got)
		}
		if got := cssDeclarations(joined, "flex-direction"); !containsValue(got, "column") {
			t.Errorf("%s: %q declares flex-direction: %v, want column; a row would divide "+
				"the width instead of the height", link.where, link.selector, got)
		}
		if link.grows && cssFlexGrow(t, joined, link.selector) < 1 {
			t.Errorf("%s: %q does not grow into the space its parent leaves (flex: 1), so "+
				"the height stops being passed down at that link", link.where, link.selector)
		}
	}
}

// TestTasksPage_BoardSitsInTheFullHeightShellChain asserts the served markup is
// the markup those stylesheet rules are written against: the page carries the
// class the shell rules select on, and the board's ancestors are exactly the
// elements the chain walks through.
//
// The stylesheet half of this file is inert without this one. `.full-height-page
// .page` selects nothing if the class leaves the template; `.page-body >
// .container-xl` stops matching the moment a wrapper element is introduced
// between them, and a chain broken at that link leaves the board sizing itself to
// its content with no rule having changed.
func TestTasksPage_BoardSitsInTheFullHeightShellChain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()
	path := "/roadmaps/" + name + "/tasks"
	page := servePage(t, mux, path)

	classes := bodyClasses(t, page, path)
	if !classes["full-height-page"] {
		t.Errorf("page %s: its <body> does not carry full-height-page, so every "+
			"`.full-height-page ...` rule in the served stylesheet selects nothing and "+
			"the board falls back to sizing itself to its content", path)
	}
	if !classes["layout-fluid"] {
		t.Errorf("page %s: its <body> lost Tabler's layout-fluid class", path)
	}

	want := []string{
		"html",
		"body.full-height-page.layout-fluid",
		"div.page",
		"div.page-wrapper",
		"main.page-body",
		"div.container-xl",
	}
	got := ancestorChain(t, page, `class="task-board"`)
	if strings.Join(got, " > ") != strings.Join(want, " > ") {
		t.Errorf("page %s: the board's ancestor chain is\n  %s\nwant\n  %s\n"+
			"The shell rules are written against that chain: a link inserted or removed "+
			"leaves them selecting elements the board no longer descends from",
			path, strings.Join(got, " > "), strings.Join(want, " > "))
	}
}

// --- stylesheet reading -----------------------------------------------------

// projectStyleSheet returns the override stylesheet the binary serves.
func projectStyleSheet(t *testing.T) string {
	t.Helper()
	return embeddedSheet(t, "static/style.css")
}

// embeddedSheet reads one stylesheet out of the embedded filesystem, so the
// assertions read the bytes the binary serves rather than a file beside them.
func embeddedSheet(t *testing.T, path string) string {
	t.Helper()
	b, err := staticFS.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the embedded stylesheet %s: %v", path, err)
	}
	// Falsifiability control: an empty or truncated read would satisfy every
	// absence asserted above without proving anything.
	if len(b) < 1024 {
		t.Fatalf("embedded stylesheet %s is only %d bytes; the assertions over it "+
			"would be vacuous", path, len(b))
	}
	return string(b)
}

// soleCSSRule returns the declarations of the one unconditional rule for
// selector, failing the test when there is not exactly one. A property asserted
// absent from one of two rules for the same selector proves nothing about what
// the browser applies, so the guards above refuse the ambiguity rather than
// guess which rule wins.
func soleCSSRule(t *testing.T, sheet, selector string) string {
	t.Helper()
	blocks := cssRuleBlocks(sheet, selector)
	if len(blocks) != 1 {
		t.Fatalf("the served stylesheet carries %d unconditional rules for %q, want exactly 1",
			len(blocks), selector)
	}
	return blocks[0]
}

// cssRuleBlocks returns the declaration block of every unconditional rule whose
// selector list carries selector exactly. Rules nested in an at-rule — a media
// query, say — are skipped: they apply conditionally, and a conditional
// declaration neither establishes nor refutes what the browser applies by
// default.
func cssRuleBlocks(sheet, selector string) []string {
	var blocks []string
	for _, rule := range parseCSSRules(sheet) {
		if rule.nested {
			continue
		}
		for _, s := range strings.Split(rule.prelude, ",") {
			if normaliseSelector(s) == normaliseSelector(selector) {
				blocks = append(blocks, rule.decls)
				break
			}
		}
	}
	return blocks
}

// normaliseSelector collapses the whitespace a selector may be written with, so
// `.page-body>.container-xl` and `.page-body > .container-xl` are one selector.
func normaliseSelector(s string) string {
	var b strings.Builder
	lastSpace := false
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			lastSpace = true
		case r == '>' || r == '+' || r == '~':
			// A combinator absorbs the whitespace around it.
			for strings.HasSuffix(b.String(), " ") {
				return normaliseSelector(strings.TrimSuffix(b.String(), " ") + string(r) +
					strings.TrimSpace(s[strings.Index(s, string(r))+1:]))
			}
			b.WriteRune(r)
			lastSpace = false
		default:
			if lastSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			lastSpace = false
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cssRule is one parsed rule: its selector list, its declarations, and whether
// it sits inside an at-rule block.
type cssRule struct {
	prelude string
	decls   string
	nested  bool
}

// parseCSSRules splits a stylesheet into rules. It skips comments and quoted
// strings so a brace inside either cannot open or close a block, and it tracks
// at-rule nesting so a rule inside a media query is reported as nested rather
// than as a rule of its own. It is deliberately small: it reads the two
// stylesheets this package ships, not arbitrary CSS.
func parseCSSRules(sheet string) []cssRule {
	var (
		rules []cssRule
		depth int
		start int
	)
	for i := 0; i < len(sheet); i++ {
		switch sheet[i] {
		case '/':
			if i+1 < len(sheet) && sheet[i+1] == '*' {
				if end := strings.Index(sheet[i+2:], "*/"); end >= 0 {
					i += 2 + end + 1
				} else {
					i = len(sheet)
				}
			}
		case '"', '\'':
			quote := sheet[i]
			for i++; i < len(sheet) && sheet[i] != quote; i++ {
				if sheet[i] == '\\' {
					i++
				}
			}
		case '{':
			prelude := stripCSSComments(sheet[start:i])
			body, end := cssBlockBody(sheet, i)
			if strings.HasPrefix(strings.TrimSpace(prelude), "@") {
				// An at-rule: its body holds rules of its own, which are
				// reported as nested.
				for _, inner := range parseCSSRules(body) {
					inner.nested = true
					rules = append(rules, inner)
				}
			} else {
				rules = append(rules, cssRule{prelude: strings.TrimSpace(prelude),
					decls: body, nested: depth > 0})
			}
			i = end
			start = i + 1
		case '}':
			start = i + 1
		}
	}
	return rules
}

// cssBlockBody returns the body of the block whose opening brace is at open, and
// the index of its closing brace.
func cssBlockBody(sheet string, open int) (string, int) {
	depth := 0
	for i := open; i < len(sheet); i++ {
		switch sheet[i] {
		case '/':
			if i+1 < len(sheet) && sheet[i+1] == '*' {
				if end := strings.Index(sheet[i+2:], "*/"); end >= 0 {
					i += 2 + end + 1
				}
			}
		case '"', '\'':
			quote := sheet[i]
			for i++; i < len(sheet) && sheet[i] != quote; i++ {
				if sheet[i] == '\\' {
					i++
				}
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return sheet[open+1 : i], i
			}
		}
	}
	return sheet[open+1:], len(sheet) - 1
}

// stripCSSComments removes comments from a selector prelude, which in the
// project sheet is preceded by the comment explaining the rule.
func stripCSSComments(s string) string {
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "*/")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+j+2:]
	}
}

// cssDeclarations returns the value of every declaration of prop in a
// declaration block, in source order — the order in which a browser applies
// them, which is what an ordered pair of fallback declarations depends on.
func cssDeclarations(block, prop string) []string {
	var values []string
	for _, decl := range splitCSSDeclarations(block) {
		name, value, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		if strings.TrimSpace(name) == prop {
			values = append(values, strings.TrimSpace(value))
		}
	}
	return values
}

// splitCSSDeclarations splits a declaration block on the semicolons that
// separate declarations, ignoring those inside parentheses (a url() or a calc())
// and inside quotes.
func splitCSSDeclarations(block string) []string {
	block = stripCSSComments(block)
	var (
		out   []string
		depth int
		start int
	)
	for i := 0; i < len(block); i++ {
		switch block[i] {
		case '(':
			depth++
		case ')':
			depth--
		case '"', '\'':
			quote := block[i]
			for i++; i < len(block) && block[i] != quote; i++ {
				if block[i] == '\\' {
					i++
				}
			}
		case ';':
			if depth == 0 {
				out = append(out, block[start:i])
				start = i + 1
			}
		}
	}
	if start < len(block) {
		out = append(out, block[start:])
	}
	return out
}

// cssMentionsUnit reports whether any declaration in a block uses a CSS unit,
// matching it as a whole token so `dvh` does not answer for `vh` and a property
// name containing the letters does not answer for either.
func cssMentionsUnit(block, unit string) bool {
	for _, decl := range splitCSSDeclarations(block) {
		_, value, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		for i := 0; i+len(unit) <= len(value); i++ {
			if value[i:i+len(unit)] != unit {
				continue
			}
			// A unit follows a digit or a dot and is not part of a longer word.
			before := i > 0 && (value[i-1] == '.' || (value[i-1] >= '0' && value[i-1] <= '9'))
			after := i+len(unit) == len(value) || !isUnitByte(value[i+len(unit)])
			if before && after {
				return true
			}
		}
	}
	return false
}

// isUnitByte reports whether b can continue a CSS identifier, so a match
// followed by one of these is a longer unit than the one sought.
func isUnitByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_'
}

// cssFlexGrow returns the flex-grow factor a declaration block gives, reading it
// from the `flex` shorthand or from `flex-grow`, whichever the block declares
// last. It fails the test when the block declares neither, because a caller
// asking for the factor has already established that the element must have one.
func cssFlexGrow(t *testing.T, block, selector string) float64 {
	t.Helper()
	grow := -1.0
	for _, decl := range splitCSSDeclarations(block) {
		name, value, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(value))
		if len(fields) == 0 {
			continue
		}
		switch strings.TrimSpace(name) {
		case "flex-grow":
			grow = parseCSSNumber(fields[0])
		case "flex":
			// `flex: none` is 0; `flex: auto` and any numeric first value give
			// the grow factor directly.
			switch fields[0] {
			case "none", "initial":
				grow = 0
			case "auto":
				grow = 1
			default:
				grow = parseCSSNumber(fields[0])
			}
		}
	}
	if grow < 0 {
		t.Errorf("%s declares neither flex nor flex-grow", selector)
		return 0
	}
	return grow
}

// parseCSSNumber reads a plain CSS number, returning 0 for anything else.
func parseCSSNumber(s string) float64 {
	var (
		value float64
		frac  float64 = 1
		dot   bool
	)
	if s == "" {
		return 0
	}
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '.' && !dot:
			dot = true
		case s[i] >= '0' && s[i] <= '9':
			if dot {
				frac /= 10
				value += float64(s[i]-'0') * frac
			} else {
				value = value*10 + float64(s[i]-'0')
			}
		default:
			return 0
		}
	}
	return value
}

// containsValue reports whether one of the declared values is want, comparing
// the first token so `display: flex` matches and a shorthand carrying more does
// not accidentally miss.
func containsValue(values []string, want string) bool {
	for _, v := range values {
		if fields := strings.Fields(v); len(fields) > 0 && fields[0] == want {
			return true
		}
	}
	return false
}

// --- markup reading ---------------------------------------------------------

// voidElements are the HTML elements that never have a closing tag, so the
// walker below must not push them onto the open-element stack.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// ancestorChain returns the open elements enclosing the first element whose start
// tag carries marker, outermost first, each written as `tag.class` for the
// failure message. The element itself is not included.
//
// It walks the served page with a small open-element stack rather than a parser:
// the project has no HTML-parsing dependency and this test is not the place to
// add one. The walk fails closed — a walker that lost track of the nesting
// returns a chain that does not match the expected one and the assertion fails,
// rather than passing on a wrong answer.
func ancestorChain(t *testing.T, page, marker string) []string {
	t.Helper()
	var stack []string
	for i := 0; i < len(page); i++ {
		if page[i] != '<' {
			continue
		}
		if strings.HasPrefix(page[i:], "<!--") {
			end := strings.Index(page[i:], "-->")
			if end < 0 {
				break
			}
			i += end + 2
			continue
		}
		if strings.HasPrefix(page[i:], "<!") {
			end := strings.IndexByte(page[i:], '>')
			if end < 0 {
				break
			}
			i += end
			continue
		}
		end := htmlTagEnd(page, i)
		if end < 0 {
			break
		}
		tag := page[i : end+1]
		if strings.HasPrefix(tag, "</") {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			i = end
			continue
		}
		name := htmlTagName(tag)
		if strings.Contains(tag, marker) {
			// Found it: the stack holds its ancestors, outermost first.
			chain := make([]string, len(stack))
			copy(chain, stack)
			return chain
		}
		switch {
		case voidElements[name] || strings.HasSuffix(tag, "/>"):
			// Nothing to push.
		case name == "script" || name == "style":
			// Skip the raw text so a `<` inside it is not read as a tag.
			if closing := strings.Index(page[end:], "</"+name); closing >= 0 {
				i = end + closing - 1
				continue
			}
		default:
			stack = append(stack, htmlElementLabel(tag, name))
		}
		i = end
	}
	t.Fatalf("no element carrying %q was found in the page, so the chain assertion "+
		"would be vacuous", marker)
	return nil
}

// htmlTagEnd returns the index of the `>` closing the tag that starts at open,
// ignoring one inside a quoted attribute value.
func htmlTagEnd(page string, open int) int {
	for i := open; i < len(page); i++ {
		switch page[i] {
		case '"', '\'':
			quote := page[i]
			for i++; i < len(page) && page[i] != quote; i++ {
			}
		case '>':
			return i
		}
	}
	return -1
}

// htmlTagName returns the lower-case element name of a start or end tag.
func htmlTagName(tag string) string {
	name := strings.TrimLeft(tag, "</")
	if i := strings.IndexAny(name, " \t\r\n/>"); i >= 0 {
		name = name[:i]
	}
	return strings.ToLower(name)
}

// htmlElementLabel writes an element as `tag.class1.class2`, which is how the
// expected chain is spelled in the assertion. The classes are sorted, so the
// assertion fixes which classes an element carries without fixing the order they
// are written in.
func htmlElementLabel(tag, name string) string {
	m := classAttrRe.FindStringSubmatch(tag)
	if m == nil {
		return name
	}
	classes := strings.Fields(m[1])
	slices.Sort(classes)
	return name + "." + strings.Join(classes, ".")
}
