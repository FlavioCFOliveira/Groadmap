package web

import (
	"regexp"
	"strings"
	"testing"
)

// The guards in this file cover the LAYOUT half of the Roadmap Sprint Page's
// member-tasks board: its bounded height and per-column scrolling (Acceptance
// Criterion 136), and the fact that its column and card lengths are the tasks
// board's own lengths reused rather than a second set that happens to agree today
// (Acceptance Criterion 139).
//
// What these guards can and cannot establish. There is no browser in the Go suite
// and SPEC/BUILD.md rules out a JavaScript toolchain, so nothing here measures a
// rendered board: whether 60vh presents a useful number of cards is judged against
// a running server. What IS checkable hermetically is the mechanism those
// measurements depend on, in the exact bytes the binary serves — that the board
// declares the specified height, that it reads its floor from the one place the
// length is written, that the property resolves on a page carrying no full-height
// shell, that each column scrolls its own cards, and that both boards resolve to
// one rule for every shared length. A stylesheet satisfying all of that can still
// measure badly; one failing any of it cannot measure well.

// TestSprintBoard_HeightIsBoundedAndFlooredByTheSharedProperty is the gate for the
// height half of Acceptance Criterion 136: the board's height is 60vh in the
// project override stylesheet, floored at the value of the
// `--full-height-region-floor` custom property, and it is NOT the space the page
// body leaves.
//
// The floor is asserted to be READ from that property and never restated beside
// it. That is the whole point of the property: a second copy of a length is a copy
// that can be changed on its own, and the board and the page body would then state
// different floors for the same screen. The criterion fails a stylesheet that
// writes the floor out as a literal of its own, so the literal is counted across
// the whole sheet and must appear exactly once — in the one declaration.
func TestSprintBoard_HeightIsBoundedAndFlooredByTheSharedProperty(t *testing.T) {
	sheet := projectStyleSheet(t)

	// The modifier declares the bounded height, and only that. A `min-height` here
	// would be the second copy of the floor the criterion forbids; the base
	// `.task-board` rule already carries it, and the modifier's whole job is the
	// one property that differs between the two boards.
	bounded := soleCSSRule(t, sheet, ".task-board--bounded")
	if got := cssDeclarations(bounded, "height"); len(got) != 1 || got[0] != "60vh" {
		t.Errorf(".task-board--bounded declares height: %v, want exactly %q "+
			"(SPEC/WEB.md § Sprint Detail Sub-Template, Height and scrolling; "+
			"Acceptance Criterion 136)", got, "60vh")
	}
	if got := cssDeclarations(bounded, "min-height"); len(got) != 0 {
		t.Errorf(".task-board--bounded declares min-height: %v; the floor is declared once, on "+
			"the shared .task-board rule, and reading it in two places is how two floors for "+
			"one screen begin", got)
	}

	// The floor is read from the property, with NO fallback length beside it: the
	// property is declared where every page resolves it, so a fallback would be a
	// second copy of the number that nothing ever reads and nothing keeps in step.
	board := soleCSSRule(t, sheet, ".task-board")
	floors := cssDeclarations(board, "min-height")
	if len(floors) != 1 || floors[0] != "var(--full-height-region-floor)" {
		t.Errorf(".task-board declares min-height: %v, want exactly "+
			"%q; the board reads the floor rather than restating it",
			floors, "var(--full-height-region-floor)")
	}

	// The property itself is declared by exactly ONE rule, and that rule is :root,
	// so every page resolves it — including the sprint page, which carries no
	// full-height shell to declare it on.
	declaring := cssRulesDeclaring(sheet, "--full-height-region-floor")
	if len(declaring) != 1 {
		t.Fatalf("--full-height-region-floor is declared by %d rules (%v), want exactly 1: two "+
			"declarations are two floors that can be changed apart", len(declaring), declaring)
	}
	if declaring[0] != ":root" {
		t.Errorf("--full-height-region-floor is declared on %q, want %q; the sprint page's board "+
			"reads it and that page is not a full-height page, so a property declared on the "+
			"full-height shell alone would not resolve there", declaring[0], ":root")
	}

	// The value that one declaration carries, so the assertions below are written
	// against the floor the sheet actually states rather than against a number
	// this test decided on.
	root := soleCSSRule(t, sheet, ":root")
	values := cssDeclarations(root, "--full-height-region-floor")
	if len(values) != 1 {
		t.Fatalf(":root declares --full-height-region-floor %d times (%v), want exactly 1",
			len(values), values)
	}
	floor := values[0]
	if !strings.HasSuffix(floor, "rem") {
		t.Errorf("the floor is %q; it is expressed in rem so it scales with the reader's own "+
			"text size, and a length in px does not", floor)
	}

	// No rule writes that length out for itself. The floor reaches a region
	// through `min-height`, and a definite height could be written as `height`, so
	// those are the two properties through which a second copy of the number could
	// enter — and neither may carry it anywhere in the sheet. (A coincidentally
	// equal length on an unrelated property, such as the graph detail panel's
	// width, is not a copy of the floor and is not what this forbids.)
	for _, restated := range cssDeclarationsOfValue(sheet, []string{"min-height", "height"}, floor) {
		t.Errorf("%s writes the floor out as the literal length %s; the floor is declared once, "+
			"on :root, and every region reads it from there — a second copy is a copy that can "+
			"be changed on its own (Acceptance Criterion 136)", restated, floor)
	}

	// And nothing reads it with a fallback, which would be that same second copy
	// wearing the property's name: the property is declared where every page
	// resolves it, so a fallback is a length nothing ever applies and nothing keeps
	// in step.
	if strings.Contains(stripCSSComments(sheet), "var(--full-height-region-floor,") {
		t.Errorf("a rule reads --full-height-region-floor with a fallback length; the property " +
			"is declared on :root and resolves on every page, so the fallback is an unused " +
			"second copy of the floor")
	}
}

// TestSprintBoard_ScrollsPerColumnInsideThatHeight is the gate for the scrolling
// half of Acceptance Criterion 136: each column scrolls vertically and
// independently when its cards exceed the board's height, and the column strip
// scrolls horizontally inside its own container rather than making the page do it.
//
// The three rules are the tasks board's own, unchanged: both boards give a column
// a definite maximum height, let the card list shrink below its content (which is
// what lets it scroll at all), and keep the horizontal scroll on the board rather
// than on the page.
func TestSprintBoard_ScrollsPerColumnInsideThatHeight(t *testing.T) {
	sheet := projectStyleSheet(t)

	// The strip scrolls sideways, and never up and down: the columns do that.
	board := soleCSSRule(t, sheet, ".task-board")
	for prop, want := range map[string]string{"overflow-x": "auto", "overflow-y": "hidden"} {
		if got := cssDeclarations(board, prop); len(got) != 1 || got[0] != want {
			t.Errorf(".task-board declares %s: %v, want exactly %q; the BOARD scrolls "+
				"horizontally inside its own container so the page never does, and it never "+
				"scrolls vertically because its columns do that individually", prop, got, want)
		}
	}

	// A column is bounded by the board's height rather than by its own content,
	// which is what gives its card list something to overflow.
	column := soleCSSRule(t, sheet, ".task-board__column")
	if got := cssDeclarations(column, "max-height"); len(got) != 1 || got[0] != "100%" {
		t.Errorf(".task-board__column declares max-height: %v, want exactly %q; without it a "+
			"column grows to its card list and the board's bounded height contains nothing",
			got, "100%")
	}

	// The card list scrolls, independently of the other columns. min-height: 0 is
	// what lets a flex item shrink below its content height and therefore scroll.
	cards := soleCSSRule(t, sheet, ".task-board__cards")
	if got := cssDeclarations(cards, "overflow-y"); len(got) != 1 || got[0] != "auto" {
		t.Errorf(".task-board__cards declares overflow-y: %v, want exactly %q; each column "+
			"scrolls its own cards", got, "auto")
	}
	if got := cssDeclarations(cards, "min-height"); len(got) != 1 || got[0] != "0" {
		t.Errorf(".task-board__cards declares min-height: %v, want exactly %q; a flex item's "+
			"default content-based minimum forbids it to shrink below its cards, so it never "+
			"scrolls and the column overflows instead", got, "0")
	}
}

// TestSprintBoard_IsNotAFullHeightRegion is the gate for the last clause of
// Acceptance Criterion 136: the board is bounded rather than full-height, so
// Acceptance Criteria 124 to 127 are not asserted against it and adding member
// tasks to the sprint leaves its height unchanged.
//
// The markup half is what makes the stylesheet half mean anything, in both
// directions. The sprint page carries no `full-height-page` class, which is
// precisely why its board's floor has to come from `:root`; and the tasks page's
// board carries no `--bounded` modifier, so the move of that property left the
// full-height board taking the space the page body leaves, exactly as before.
func TestSprintBoard_IsNotAFullHeightRegion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	sprintPath := f.path()
	sprintPage := servePage(t, mux, sprintPath)

	// The sprint page is not a full-height page, and says so in its <body>.
	if bodyClasses(t, sprintPage, sprintPath)["full-height-page"] {
		t.Errorf("the sprint page's <body> carries full-height-page; the page places the Sprint "+
			"details card above its board and the Comments card below it, so the board is "+
			"bounded rather than sized to the space the page body leaves (page %s)", sprintPath)
	}

	// Its board carries both classes: the shared board rules and the modifier that
	// bounds its height.
	sprintClasses := boardClassTokens(t, sprintPage, "the sprint page")
	for _, want := range []string{"task-board", "task-board--bounded"} {
		if !sprintClasses[want] {
			t.Errorf("the sprint page's board carries the classes %v, want %q among them",
				sortedKeys(sprintClasses), want)
		}
	}

	// The tasks page's board is untouched by the modifier and by the move of the
	// floor: it carries no bounded height, and its page still declares the
	// full-height shell the region's height is computed through.
	tasksPath := "/roadmaps/" + f.name + "/tasks"
	tasksPage := servePage(t, mux, tasksPath)
	tasksClasses := boardClassTokens(t, tasksPage, "the tasks page")
	if tasksClasses["task-board--bounded"] {
		t.Errorf("the tasks page's board carries task-board--bounded; its height is the space " +
			"the page body leaves, not a fraction of the viewport")
	}
	if !bodyClasses(t, tasksPage, tasksPath)["full-height-page"] {
		t.Errorf("the tasks page's <body> lost full-height-page, so every `.full-height-page ...` " +
			"rule selects nothing and its board falls back to sizing itself to its content")
	}

	// The board's height does not grow with the sprint: the same markup and the
	// same classes whatever the number of member tasks, so nothing about the
	// height depends on the data.
	empty := seedSprintWithMembers(t, "settlement-window-empty", 0)
	emptyPage := servePage(t, mux, "/roadmaps/settlement-window-empty/sprints/"+itoa(empty))
	emptyClasses := boardClassTokens(t, emptyPage, "an empty sprint's page")
	if !sameClassSet(sprintClasses, emptyClasses) {
		t.Errorf("a sprint with 6 member tasks renders the board classes %v and one with none "+
			"renders %v; the board's height must not depend on what the sprint holds",
			sortedKeys(sprintClasses), sortedKeys(emptyClasses))
	}
}

// TestSprintBoard_ReusesTheTasksBoardColumnAndCardLengths is the gate for
// Acceptance Criterion 139: each of the three columns is 19rem wide and never
// narrower than 17rem, the columns are separated by a 0.75rem gap, the body of a
// card carries 0.75rem of padding on all four sides — and every one of those is
// the length the tasks board's columns and cards already carry.
//
// The criterion requires the check to compare the TWO BOARDS' values and to fail
// when they diverge, so the comparison is made where divergence would actually
// happen: the classes each board's markup emits. Both boards select the same
// rules, so the lengths are literally one measure used twice; a board that grew a
// column class of its own would resolve to a different rule and fail here, however
// closely the two rules' values agreed on the day it was written.
//
// The values themselves are pinned as well, because the criterion fixes them, and
// soleCSSRule refuses a second unconditional rule for any of these selectors — a
// property asserted on one of two rules for one selector proves nothing about what
// the browser applies.
func TestSprintBoard_ReusesTheTasksBoardColumnAndCardLengths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	sprintPage := servePage(t, mux, f.path())
	tasksPage := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")

	// The two boards' columns and cards are the same object: the same class
	// tokens, so the same rules, so the same lengths.
	for _, part := range []struct {
		what   string
		marker string
	}{
		{"column", `data-role="task-board-column"`},
		{"card", `data-task-id="`},
	} {
		onSprint := elementClassTokens(t, sprintPage, part.marker, "the sprint page's board")
		onTasks := elementClassTokens(t, tasksPage, part.marker, "the tasks page's board")
		if !sameClassSet(onSprint, onTasks) {
			t.Errorf("the sprint board's %s carries the classes %v and the tasks board's carries "+
				"%v; the two boards must resolve to ONE column measure and ONE card measure, "+
				"not to two rules that happen to agree", part.what,
				sortedKeys(onSprint), sortedKeys(onTasks))
		}
	}

	// And the lengths those shared rules carry, each declared once and expressed
	// in rem so it scales with the reader's own text size.
	sheet := projectStyleSheet(t)

	column := soleCSSRule(t, sheet, ".task-board__column")
	for prop, want := range map[string]string{"width": "19rem", "min-width": "17rem"} {
		if got := cssDeclarations(column, prop); len(got) != 1 || got[0] != want {
			t.Errorf(".task-board__column declares %s: %v, want exactly %q (Acceptance Criteria "+
				"129 and 139)", prop, got, want)
		}
	}
	// A column stands for a state and not for a volume of work, so it neither
	// grows into a wide viewport nor shrinks on a narrow one — on either board.
	if got := cssDeclarations(column, "flex"); len(got) != 1 || got[0] != "0 0 auto" {
		t.Errorf(".task-board__column declares flex: %v, want exactly %q", got, "0 0 auto")
	}

	board := soleCSSRule(t, sheet, ".task-board")
	if got := cssDeclarations(board, "gap"); len(got) != 1 || got[0] != "0.75rem" {
		t.Errorf(".task-board declares gap: %v, want exactly %q; the columns of both boards are "+
			"separated by one gap", got, "0.75rem")
	}

	cardBody := soleCSSRule(t, sheet, ".task-card > .card-body")
	if got := cssUniformRemPadding(t, cardBody, ".task-card > .card-body"); got != 0.75 {
		t.Errorf(".task-card > .card-body declares padding %grem, want 0.75rem", got)
	}

	// Every one of those lengths is in rem, which a px length would not be.
	for selector, block := range map[string]string{
		".task-board__column":     column,
		".task-board":             board,
		".task-card > .card-body": cardBody,
	} {
		for _, prop := range []string{"width", "min-width", "gap", "padding"} {
			for _, value := range cssDeclarations(block, prop) {
				if !strings.HasSuffix(value, "rem") {
					t.Errorf("%s declares %s: %q; the board's lengths are expressed in rem so "+
						"they scale with the reader's text size, and a length in px does not",
						selector, prop, value)
				}
			}
		}
	}

	// The board emits no inline style on either page: this is a stylesheet change
	// and Acceptance Criterion 62 continues to hold.
	if region := memberBoardRegion(t, sprintPage); strings.Contains(region, "style=") {
		t.Errorf("the member-tasks board carries an inline style attribute")
	}
}

// ==================== HELPERS ====================

// reOpeningTagWithMarker finds the opening tag that carries a marker attribute,
// capturing that tag's attribute text.
func reOpeningTagWithMarker(marker string) *regexp.Regexp {
	return regexp.MustCompile(`<[a-zA-Z][a-zA-Z0-9]*\b([^>]*` + regexp.QuoteMeta(marker) + `[^>]*)>`)
}

// elementClassTokens returns the class tokens of the FIRST element carrying a
// marker attribute in a served page, failing the test when there is none.
func elementClassTokens(t *testing.T, page, marker, where string) map[string]bool {
	t.Helper()

	m := reOpeningTagWithMarker(marker).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("%s renders no element carrying %s", where, marker)
	}
	attrs := classAttrRe.FindStringSubmatch(m[1])
	if attrs == nil {
		t.Fatalf("%s: the element carrying %s has no class attribute", where, marker)
	}
	set := map[string]bool{}
	for _, class := range strings.Fields(attrs[1]) {
		set[class] = true
	}
	return set
}

// boardClassTokens returns the class tokens of a page's board container.
func boardClassTokens(t *testing.T, page, where string) map[string]bool {
	t.Helper()
	return elementClassTokens(t, page, `data-role="task-board"`, where)
}

// sameClassSet reports whether two class-token sets hold exactly the same tokens.
func sameClassSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for class := range a {
		if !b[class] {
			return false
		}
	}
	return true
}

// cssRulesDeclaring returns the selector of every unconditional rule in a
// stylesheet that declares a property, which is how "this value is written in one
// place" is checked rather than assumed.
func cssRulesDeclaring(sheet, prop string) []string {
	var selectors []string
	for _, rule := range parseCSSRules(sheet) {
		if rule.nested {
			continue
		}
		if len(cssDeclarations(rule.decls, prop)) > 0 {
			selectors = append(selectors, normaliseSelector(rule.prelude))
		}
	}
	return selectors
}

// cssDeclarationsOfValue returns a description of every unconditional declaration
// of one of the named properties whose value contains want, so a length that must
// be written in one place can be checked against the whole stylesheet rather than
// against the one rule a test happened to look at.
func cssDeclarationsOfValue(sheet string, props []string, want string) []string {
	var found []string
	for _, rule := range parseCSSRules(sheet) {
		if rule.nested {
			continue
		}
		for _, prop := range props {
			for _, value := range cssDeclarations(rule.decls, prop) {
				if strings.Contains(value, want) {
					found = append(found,
						normaliseSelector(rule.prelude)+" { "+prop+": "+value+" }")
				}
			}
		}
	}
	return found
}
