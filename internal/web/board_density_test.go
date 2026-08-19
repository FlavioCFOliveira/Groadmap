package web

import (
	"strconv"
	"strings"
	"testing"
)

// The guards in this file cover SPEC/WEB.md § Roadmap Tasks Page, Column width
// and card density (Acceptance Criterion 129): the two lengths that decide how
// much of a task's own text a Kanban card can put on one line.
//
// What they are written against. The board used to give a column 17rem and let
// the card inside it keep the 1rem of body padding the vendored Tabler
// distribution gives a small card. Between the column's own card body (1.25rem
// of horizontal padding on each side) and the task card's (1rem on each side),
// a card's running text — its reference line, its title, its two badges and its
// six metadata indicators — was set on a measure of roughly 196px, so titles
// broke over several short lines and the metadata footer wrapped one indicator
// per line.
//
// What these guards can and cannot establish. There is no browser in the Go
// suite and SPEC/BUILD.md rules out a JavaScript toolchain, so nothing here
// measures a rendered card: the legibility the criterion is written for is
// judged against a running server. What IS checkable hermetically is the
// mechanism it depends on, in the exact bytes the binary serves — that the
// column declares the specified fixed width, that the card's body declares less
// padding than the framework's own, that the override can actually reach the
// element it names, and that it wins the cascade without an `!important`. A
// stylesheet satisfying all of that can still measure badly; one failing any of
// it cannot measure well.

// TestTaskBoardColumn_CarriesTheSpecifiedFixedWidth asserts a column is the
// width the specification fixes, never narrower than the floor it fixes, and
// neither grows nor shrinks away from it.
//
// The `flex` half is what makes the two lengths mean anything. A column that
// grew would be as wide as the viewport divided by five on a wide screen and the
// measure of a title would change with the window; a column that shrank would
// give the five columns back the horizontal scroll they are meant to have and
// narrow every card to fit. `flex: 0 0 auto` is what keeps the declared width the
// width the browser applies (SPEC/WEB.md § Roadmap Tasks Page, Column width and
// card density).
func TestTaskBoardColumn_CarriesTheSpecifiedFixedWidth(t *testing.T) {
	column := soleCSSRule(t, projectStyleSheet(t), ".task-board__column")

	for prop, want := range map[string]string{
		"width":     "19rem",
		"min-width": "17rem",
	} {
		got := cssDeclarations(column, prop)
		if len(got) != 1 || got[0] != want {
			t.Errorf(".task-board__column declares %s: %v, want exactly %q "+
				"(SPEC/WEB.md § Roadmap Tasks Page, Column width and card density; "+
				"Acceptance Criterion 129)", prop, got, want)
		}
	}

	// Both lengths follow the reader's own text size, which a px length would
	// not: the criterion requires them in `rem`.
	for _, prop := range []string{"width", "min-width"} {
		for _, value := range cssDeclarations(column, prop) {
			if !strings.HasSuffix(value, "rem") {
				t.Errorf(".task-board__column declares %s: %q; the column's lengths are "+
					"expressed in rem so they scale with the reader's text size, and a "+
					"length in px does not", prop, value)
			}
		}
	}

	if got := cssDeclarations(column, "flex"); len(got) != 1 || got[0] != "0 0 auto" {
		t.Errorf(".task-board__column declares flex: %v, want exactly %q; a column that "+
			"grows makes a card's measure depend on the viewport width and one that "+
			"shrinks narrows every card instead of letting the board scroll sideways",
			got, "0 0 auto")
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
// it (SPEC/WEB.md § Roadmap Tasks Page, Column width and card density).
func TestTaskCardBody_IsTighterThanTheVendoredSmallCard(t *testing.T) {
	vendored := embeddedSheet(t, "static/vendor/tabler/tabler.min.css")
	framework := cssUniformRemPadding(t, soleCSSRule(t, vendored, ".card-sm > .card-body"),
		".card-sm > .card-body")

	project := soleCSSRule(t, projectStyleSheet(t), ".task-card > .card-body")
	board := cssUniformRemPadding(t, project, ".task-card > .card-body")

	if board >= framework {
		t.Errorf(".task-card > .card-body declares padding %grem and the vendored "+
			".card-sm > .card-body declares %grem; the board's card body must be the "+
			"TIGHTER of the two, because the padding it does not spend is measure "+
			"returned to the card's own text (Acceptance Criterion 129)", board, framework)
	}
	if want := 0.75; board != want {
		t.Errorf(".task-card > .card-body declares padding %grem, want %grem "+
			"(SPEC/WEB.md § Roadmap Tasks Page, Column width and card density)", board, want)
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
// `card-body` class onto the button itself — leaves the rule matching nothing
// while every assertion above still passes and the card silently returns to
// Tabler's 1rem.
func TestTaskCard_BodyIsADirectChildOfTheCard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	page := servePage(t, buildMux(), "/roadmaps/"+name+"/tasks")

	chain := ancestorChain(t, page, `class="card-body d-block"`)
	if len(chain) == 0 {
		t.Fatal("the card body has no ancestor at all in the served page")
	}
	parent := chain[len(chain)-1]
	if !strings.HasPrefix(parent, "button.") || !strings.Contains(parent, ".task-card") {
		t.Errorf("the card body's parent element is %q; it must be the .task-card button "+
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
