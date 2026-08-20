package web

import (
	"slices"
	"strings"
	"testing"
)

// The guards in this file cover SPEC/WEB.md § Full-Height Page Regions for BOTH
// of the regions that section names — the Kanban board of the roadmap tasks page
// and the graph card of the knowledge-graph page (Acceptance Criteria 124 to
// 128). The two obey one mechanism, so they are guarded together: a rule that
// held for one of them alone would be a second mechanism waiting to drift.
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
// The graph card carried the same kind of arithmetic and failed it the other way
// round. It took `height: calc(100vh - 9rem)`, and the comment beside it said the
// 9rem left room for the navbar and the page header — which is the defect written
// out, because the QUERY BAR sits between the page header and the card and was
// reserved for nowhere. Measured with the Chrome DevTools Protocol at a viewport
// 900px tall, the material above the card stood 298px against that 144px
// reservation, so the card's foot fell 154px below the fold at 1280, 1600 and
// 1920 and the document scrolled vertically at all three; the same measurement
// gives 354px at 768, 406px at 500 and 426px at 400, because the header wraps and
// the query bar stacks its own controls as the viewport narrows. With the card in
// the shell chain the foot of the card meets the foot of the page body at every
// one of those widths (0px apart, 24px above the fold — the page body's own
// bottom margin) and the document does not scroll.
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

// TestGraphCard_DeclaresNoViewportArithmetic asserts the knowledge-graph card
// computes no height of its own either, and takes its floor from the one property
// every bounded region of this interface reads.
//
// This is the assertion the defect fails, and it fails it on the exact byte that
// carried the defect: `height: calc(100vh - 9rem)`. The card gives up, at the top,
// what the shell and the page actually place above it — the navbar, the page
// header AND the query bar — and no length written here can state that, because
// the page header wraps and the query bar stacks its own controls as the viewport
// narrows (SPEC/WEB.md § Full-Height Page Regions, rules 2 and 3; § Roadmap
// Knowledge-Graph Page, Graph card layout).
//
// The floor is asserted to be READ from the shared property rather than restated.
// What stood here was `min-height: 320px`: not a copy of the floor but a second
// floor for the same screen, 32px away from the one the board, the page body and
// the sprint page's bounded board all read, and free to be changed on its own
// (rule 5; Acceptance Criterion 136).
func TestGraphCard_DeclaresNoViewportArithmetic(t *testing.T) {
	sheet := projectStyleSheet(t)
	card := soleCSSRule(t, sheet, ".graph-card")

	if values := cssDeclarations(card, "height"); len(values) != 0 {
		t.Errorf(".graph-card declares height: %v; a full-height region takes the height "+
			"the page body leaves through the shell chain and computes none of its own. "+
			"The reservation this replaced forgot the query bar entirely and put the foot "+
			"of the card 154px below the fold (SPEC/WEB.md § Full-Height Page Regions, "+
			"rules 2 and 3)", values)
	}
	for _, unit := range []string{"vh", "dvh", "svh", "lvh", "vmin", "vmax"} {
		if cssMentionsUnit(card, unit) {
			t.Errorf(".graph-card sizes itself against the viewport unit %q; the viewport "+
				"height enters this layout once, at the top of the shell chain, and the "+
				"card's height is computed from it rather than recalculated here", unit)
		}
	}

	// The positive half: where the height DOES come from. Without a grow factor the
	// card would collapse to its flex base size — Tabler's `.card` gives it no
	// height of its own — and the absences above would be satisfied by a card with
	// no height at all.
	if grow := cssFlexGrow(t, card, ".graph-card"); grow < 1 {
		t.Errorf(".graph-card has flex-grow %v; it must claim the space the page body "+
			"leaves once the query bar above it is placed (flex: 1)", grow)
	}

	// The floor, read from the one place it is declared and with no fallback length
	// beside it. TestSprintBoard_HeightIsBoundedAndFlooredByTheSharedProperty owns
	// the property itself — that it is declared once, on :root, and never written
	// out as a literal; this is the graph card's own end of the same contract.
	floors := cssDeclarations(card, "min-height")
	if len(floors) != 1 || floors[0] != "var(--full-height-region-floor)" {
		t.Errorf(".graph-card declares min-height: %v, want exactly %q; below that height "+
			"the canvas is a strip, and the card reads the floor every other bounded region "+
			"reads rather than stating a second one (SPEC/WEB.md § Full-Height Page Regions, "+
			"rule 5, and Acceptance Criterion 127)", floors, "var(--full-height-region-floor)")
	}

	// The rest of what the card's own rule must keep saying. The card clips: the
	// canvas, the detail panel and the empty-graph overlay are positioned against
	// it, and a region that is allowed to overflow is a region whose bottom edge is
	// not where the layout says it is.
	if got := cssDeclarations(card, "overflow"); len(got) != 1 || got[0] != "hidden" {
		t.Errorf(".graph-card declares overflow: %v, want exactly %q; the canvas, the "+
			"detail panel and the empty-graph overlay are anchored to this box and are "+
			"clipped by it", got, "hidden")
	}
	if got := cssDeclarations(card, "display"); !containsValue(got, "flex") {
		t.Errorf(".graph-card declares display: %v, want flex; the labels sidebar and the "+
			"canvas region are its flex children", got)
	}
}

// TestFullHeightShell_DeclaresTheOrderedViewportPair asserts the one
// viewport-derived height in the chain is declared twice, first against the large
// viewport height and then against the dynamic one (Acceptance Criterion 126).
//
// One rule answers for both regions. The viewport enters this stylesheet exactly
// once, here, and the board and the graph card each obtain it by belonging to the
// chain below rather than by restating a unit of their own — which is why the two
// guards above forbid either of them to name a viewport unit at all. A region
// added to a page without joining this chain would have to declare the pair for
// itself, and this assertion would not notice; the markup guards below are what
// establish that each region is in fact under this rule.
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
// The same four links carry the height to the graph card: the two regions sit at
// the foot of one chain, and the graph page's own chain is asserted element by
// element in TestGraphPage_CardSitsInTheFullHeightShellChain below.
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

// TestGraphPage_CardSitsInTheFullHeightShellChain asserts the knowledge-graph
// page is the page those shell rules are written against, and that the query bar
// lands where the mechanism needs it: inside the chain, above the region, as the
// card's own sibling.
//
// The stylesheet half is inert without this one, and on this page in two ways.
// `.full-height-page .page` selects nothing if the class leaves the template —
// which is the state this page was in, and why its card had to compute a height
// for itself in the first place. And the query bar has to sit INSIDE the last
// link, beside the card: moved above the page body it would stop being material
// the page body's own height accounts for, and wrapped in an element of its own it
// would break `.page-body > .container-xl` exactly as an inserted wrapper would.
func TestGraphPage_CardSitsInTheFullHeightShellChain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()
	path := "/roadmaps/" + name + "/graph"
	page := servePage(t, mux, path)

	classes := bodyClasses(t, page, path)
	if !classes["full-height-page"] {
		t.Errorf("page %s: its <body> does not carry full-height-page, so every "+
			"`.full-height-page ...` rule in the served stylesheet selects nothing and the "+
			"graph card falls back to sizing itself — which is the defect, in the one byte "+
			"that causes it", path)
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
	got := ancestorChain(t, page, `class="card graph-card"`)
	if strings.Join(got, " > ") != strings.Join(want, " > ") {
		t.Errorf("page %s: the graph card's ancestor chain is\n  %s\nwant\n  %s\n"+
			"The shell rules are written against that chain: a link inserted or removed "+
			"leaves them selecting elements the card no longer descends from",
			path, strings.Join(got, " > "), strings.Join(want, " > "))
	}

	// The tasks board's chain, element for element, class for class. The two
	// regions are sized by one mechanism, and this is where that stops being a
	// claim: the container carried `h-100` here, a second statement of the
	// container's height sitting exactly where the chain now computes it.
	bar := ancestorChain(t, page, `class="graph-query-bar card mb-3"`)
	if strings.Join(bar, " > ") != strings.Join(want, " > ") {
		t.Errorf("page %s: the query bar's ancestor chain is\n  %s\nwant\n  %s\n"+
			"The bar is the card's sibling inside the last link of the chain: that is what "+
			"makes it material the page body places above the region rather than something "+
			"the region has to reserve space for itself (SPEC/WEB.md § Full-Height Page "+
			"Regions, rule 2)", path, strings.Join(bar, " > "), strings.Join(want, " > "))
	}
	if i, j := strings.Index(page, `class="graph-query-bar card mb-3"`), strings.Index(page, `class="card graph-card"`); i > j {
		t.Errorf("page %s: the graph card is emitted before the query bar; the bar is the "+
			"chrome above the region and the region takes what is left below it", path)
	}
}

// TestGraphPage_QueryBarIsPlacedByItsOwnContent asserts the one thing the graph
// page's chain needs that the tasks page's does not: that the query bar keeps its
// own height while the card gives way.
//
// Nothing in the project sheet declares that, and this test is where the reason is
// recorded rather than left to be rediscovered. The bar is a Tabler `.card`, and
// the distribution gives the card no `min-height`, so the bar keeps the automatic
// minimum every flex item has — its own content — and the flex algorithm may not
// take height from it however short the page body is. Measured with the DevTools
// Protocol at 1280x420 and 500x360, where the page body is far shorter than the
// bar and the card together, the bar kept exactly the 118px and 194px it occupies
// at those widths on a 900px-tall viewport.
//
// That makes the placement a dependency on the vendored bytes, like the three
// Tabler links in the chain test above, and gives it the same kind of guard:
// re-vendoring a Tabler whose `.card` carries a `min-height` — which is precisely
// what makes `.page-header` shrinkable and puts it in the `flex-shrink: 0` rule —
// would let the bar collapse under its own controls with nothing else failing.
func TestGraphPage_QueryBarIsPlacedByItsOwnContent(t *testing.T) {
	vendored := embeddedSheet(t, "static/vendor/tabler/tabler.min.css")

	blocks := cssRuleBlocks(vendored, ".card")
	if len(blocks) == 0 {
		t.Fatalf("the vendored Tabler distribution declares no unconditional rule for %q, "+
			"so this assertion would be vacuous", ".card")
	}
	joined := strings.Join(blocks, ";")
	for _, prop := range []string{"min-height", "flex", "flex-shrink"} {
		if got := cssDeclarations(joined, prop); len(got) != 0 {
			t.Errorf("the vendored Tabler `.card` declares %s: %v. The knowledge-graph "+
				"page's query bar is a card, and it stays the height of its own content "+
				"because a card carries no minimum of its own; a Tabler that gives it one "+
				"lets the flex algorithm take height from the bar instead of from the "+
				"region below it, exactly as `.page-header`'s own min-height does "+
				"(SPEC/WEB.md § Full-Height Page Regions, rule 2)", prop, got)
		}
	}

	// The project sheet adds no sizing of its own to the bar. It carries no rule
	// for `.graph-query-bar` at all today — its rules dress the controls inside the
	// bar, not the bar — so this half guards the rule someone may add rather than
	// one that exists now: a height, a grow factor or a viewport unit on the bar
	// itself would make it a second region competing with the card for the space
	// the page body leaves, and the card would stop being what gives way.
	project := projectStyleSheet(t)
	for _, block := range cssRuleBlocks(project, ".graph-query-bar") {
		for _, prop := range []string{"height", "flex-grow"} {
			if got := cssDeclarations(block, prop); len(got) != 0 {
				t.Errorf(".graph-query-bar declares %s: %v; the bar is placed and takes the "+
					"room its content needs, and the card below it is what gives way", prop, got)
			}
		}
		for _, unit := range []string{"vh", "dvh", "svh", "lvh"} {
			if cssMentionsUnit(block, unit) {
				t.Errorf(".graph-query-bar sizes itself against the viewport unit %q; the "+
					"viewport enters this layout once, at the top of the shell chain", unit)
			}
		}
	}
}

// TestNormaliseSelector_AbsorbsWhitespaceOnBothSidesOfACombinator is the
// regression guard for a defect in the helper below: it dropped the whitespace
// BEFORE a combinator but kept the whitespace AFTER it, so `.card-sm>.card-body`
// normalised to itself while `.card-sm > .card-body` normalised to
// `.card-sm> .card-body`. The two are one selector, and the helper's own contract
// says so, but every guard looking up a rule the vendored distribution writes
// minified while spelling the lookup with spaces found no rule at all.
//
// The descendant combinator is the other half of the contract: it is written as
// whitespace and MUST survive, or `.a .b` and `.a.b` — two different selectors —
// would normalise to the same string and a lookup would match the wrong rule.
func TestNormaliseSelector_AbsorbsWhitespaceOnBothSidesOfACombinator(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{".card-sm>.card-body", ".card-sm>.card-body"},
		{".card-sm > .card-body", ".card-sm>.card-body"},
		{".card-sm >.card-body", ".card-sm>.card-body"},
		{".card-sm> .card-body", ".card-sm>.card-body"},
		{"  .card-sm\t>\n.card-body  ", ".card-sm>.card-body"},
		{".a + .b ~ .c", ".a+.b~.c"},
		// The descendant combinator is whitespace and stays one space.
		{".full-height-page  .page", ".full-height-page .page"},
		{".full-height-page .page-body > .container-xl", ".full-height-page .page-body>.container-xl"},
		// Falsifiability control: a compound selector carries no combinator at
		// all and must not gain one.
		{".card.card-sm", ".card.card-sm"},
	} {
		if got := normaliseSelector(tc.in); got != tc.want {
			t.Errorf("normaliseSelector(%q) = %q, want %q", tc.in, got, tc.want)
		}
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
//
// A combinator absorbs the whitespace on BOTH sides of it: the space before it is
// dropped because it is never written, and the space after it is dropped because
// the run that follows a combinator opens no descendant relation of its own. The
// space that IS significant — the descendant combinator, written as whitespace
// alone — is preserved as a single space.
func normaliseSelector(s string) string {
	var (
		b       strings.Builder
		spaced  bool // whitespace has been seen since the last rune written
		afterOp bool // the last rune written was a combinator
	)
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			spaced = true
		case r == '>' || r == '+' || r == '~':
			spaced, afterOp = false, true
			b.WriteRune(r)
		default:
			if spaced && b.Len() > 0 && !afterOp {
				b.WriteByte(' ')
			}
			spaced, afterOp = false, false
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
