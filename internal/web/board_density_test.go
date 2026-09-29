package web

import (
	"strconv"
	"strings"
	"testing"
)

// The guards in this file cover the density of the sprint page's member-tasks
// board (SPEC/WEB.md § Sprint Detail Sub-Template, Height and scrolling;
// Acceptance Criterion 139): the column's minimum width and the card body's
// padding, the two lengths that decide how much of a task's own text a card can
// put on one line.
//
// There is no browser in the Go suite, so nothing here measures a rendered card.
// What IS checkable hermetically is the mechanism, in the exact bytes the binary
// serves — that the column declares the specified minimum, that the card's body
// declares less padding than the framework's own, that the override can reach the
// element it names, and that it wins the cascade without an `!important`.

// TestTaskBoardColumn_CarriesTheSpecifiedMinimumWidth asserts a column is never
// narrower than the 17rem floor, in rem, and carries no fixed width of its own:
// the three columns divide the board's width (Acceptance Criterion 139, asserted
// on the bounded override in sprint_board_layout_test.go).
func TestTaskBoardColumn_CarriesTheSpecifiedMinimumWidth(t *testing.T) {
	column := soleCSSRule(t, projectStyleSheet(t), ".task-board__column")

	if got := cssDeclarations(column, "min-width"); len(got) != 1 || got[0] != "17rem" {
		t.Errorf(".task-board__column declares min-width: %v, want exactly %q "+
			"(Acceptance Criterion 139)", got, "17rem")
	}
	for _, prop := range []string{"width", "flex"} {
		if got := cssDeclarations(column, prop); len(got) != 0 {
			t.Errorf(".task-board__column declares %s: %v; the column's width is the share "+
				"the bounded board's override gives it, and a second width on the base rule "+
				"is a length no board carries", prop, got)
		}
	}
}

// TestTaskCardBody_IsTighterThanTheVendoredSmallCard asserts the task card's
// body carries strictly less padding than the vendored Tabler distribution gives
// a small card's body.
//
// Tabler's own value is read out of the vendored stylesheet rather than written
// here, so the assertion states the RELATION the specification requires — the
// board's padding is the tighter of the two — and a Tabler upgrade that changes
// `.card-sm > .card-body` is caught by this test instead of silently inverting
// the relation. The project value is pinned as well, because the criterion fixes
// it (Acceptance Criterion 139).
func TestTaskCardBody_IsTighterThanTheVendoredSmallCard(t *testing.T) {
	vendored := embeddedSheet(t, "static/vendor/tabler/tabler.min.css")
	framework := cssUniformRemPadding(t,
		resolveCSSVarPadding(t, vendored, soleCSSRule(t, vendored, ".card-sm > .card-body")),
		".card-sm > .card-body")

	project := soleCSSRule(t, projectStyleSheet(t), ".task-card > .card-body")
	board := cssUniformRemPadding(t, project, ".task-card > .card-body")

	if board >= framework {
		t.Errorf(".task-card > .card-body declares padding %grem and the vendored "+
			".card-sm > .card-body declares %grem; the board's card body must be the "+
			"TIGHTER of the two, because the padding it does not spend is measure "+
			"returned to the card's own text (Acceptance Criterion 139)", board, framework)
	}
	if want := 0.75; board != want {
		t.Errorf(".task-card > .card-body declares padding %grem, want %grem "+
			"(Acceptance Criterion 139)", board, want)
	}

	// The override wins on source order, which only holds while it needs no help:
	// an `!important` here would mean the rule had stopped being a plain override
	// of a framework default and started fighting one.
	if strings.Contains(strings.ToLower(project), "!important") {
		t.Error(".task-card > .card-body carries an !important; the selector matches " +
			"Tabler's own specificity and the project stylesheet is the last one the " +
			"layout links, so the cascade already settles this rule in its favour " +
			"(the link order is guarded by TestPages_AssetChainOrderAndLocality)")
	}
}

// TestTaskCard_BodyIsADirectChildOfTheCard asserts the element the override
// names is the element the board actually renders.
//
// This is the link between the stylesheet and the markup, and it is the half a
// stylesheet-only assertion cannot make: `.task-card > .card-body` is a CHILD
// combinator, so wrapping the card's body in one more element — or moving the
// `card-body` class onto the card link itself — leaves the rule matching nothing
// while every assertion above still passes and the card silently returns to
// Tabler's 1rem.
func TestTaskCard_BodyIsADirectChildOfTheCard(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	sprintID := seedSprintWithMembers(t, "settlement-window", 2)
	page := servePage(t, buildMux(), "/roadmaps/settlement-window/sprints/"+itoa(sprintID))

	chain := ancestorChain(t, page, `class="card-body d-block"`)
	if len(chain) == 0 {
		t.Fatal("the card body has no ancestor at all in the served page")
	}
	parent := chain[len(chain)-1]
	if !strings.HasPrefix(parent, "a.") || !strings.Contains(parent, ".task-card") {
		t.Errorf("the card body's parent element is %q; it must be the .task-card link "+
			"itself, because `.task-card > .card-body` is a child combinator and selects "+
			"nothing once anything sits between them", parent)
	}
}

// cssUniformRemPadding returns the single `rem` length a declaration block gives
// `padding` on all four sides, failing the test when the block declares none,
// declares several, or declares a value the comparison above cannot read.
//
// The uniformity matters: the specification states one padding for all four
// sides of the card's body, and a shorthand carrying two or four lengths would
// make "the tighter of the two" a comparison between different things.
func cssUniformRemPadding(t *testing.T, block, selector string) float64 {
	t.Helper()

	values := cssDeclarations(block, "padding")
	if len(values) != 1 {
		t.Fatalf("%s declares padding %v, want exactly one declaration", selector, values)
	}
	fields := strings.Fields(strings.TrimSuffix(strings.TrimSpace(values[0]), "!important"))
	if len(fields) != 1 {
		t.Fatalf("%s declares padding %q; the comparison needs one length applying to all "+
			"four sides, not a shorthand of %d", selector, values[0], len(fields))
	}
	length, ok := strings.CutSuffix(fields[0], "rem")
	if !ok {
		t.Fatalf("%s declares padding %q, which is not a rem length; both sides of the "+
			"comparison must be in the same unit for it to mean anything", selector, fields[0])
	}
	n, err := strconv.ParseFloat(length, 64)
	if err != nil {
		t.Fatalf("%s declares padding %q, whose numeric part does not parse: %v",
			selector, fields[0], err)
	}
	return n
}

// resolveCSSVarPadding returns block with a padding written as one custom
// property reference, `padding: var(--name)`, replaced by the value sheet gives
// --name. The vendored distribution states the small card's body padding through
// --tblr-card-body-padding-sm, so the comparison reads the length that property
// carries. The property must have exactly one value across the whole sheet:
// two values would leave the applied one to the cascade, which this helper
// refuses to guess. A block whose padding is not a lone var() is returned as is.
func resolveCSSVarPadding(t *testing.T, sheet, block string) string {
	t.Helper()
	values := cssDeclarations(block, "padding")
	if len(values) != 1 {
		return block
	}
	name, ok := strings.CutPrefix(values[0], "var(")
	if !ok {
		return block
	}
	name, ok = strings.CutSuffix(name, ")")
	if !ok || !strings.HasPrefix(name, "--") || strings.ContainsAny(name, ", ") {
		return block
	}
	distinct := map[string]bool{}
	for _, rule := range parseCSSRules(sheet) {
		for _, v := range cssDeclarations(rule.decls, name) {
			distinct[v] = true
		}
	}
	if len(distinct) != 1 {
		t.Fatalf("the stylesheet gives %s %d distinct values, want exactly 1", name, len(distinct))
	}
	for v := range distinct {
		return "padding:" + v
	}
	return block
}
