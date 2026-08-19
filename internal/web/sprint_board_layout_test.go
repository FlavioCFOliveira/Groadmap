package web

import (
	"regexp"
	"strings"
	"testing"
)

// The guards in this file cover the LAYOUT half of the Roadmap Sprint Page's
// member-tasks board: its bounded height and per-column scrolling (Acceptance
// Criterion 136), its three columns dividing the board's width equally instead of
// carrying the tasks board's fixed column width, and the three lengths the two
// boards do still share — the 17rem minimum column, the 0.75rem gap, and the
// 0.75rem card body padding (Acceptance Criterion 139).
//
// The two boards' column widths are deliberately NOT one value. A board of three
// columns read as one sprint at a glance fills the width it is given; a board of
// five columns that is a view of a whole roadmap has a natural width of its own,
// and dividing a viewport among five would cut the measure a card's title is read
// on. So the guards below assert the SPLIT as carefully as they used to assert the
// agreement: what each board carries alone, and what neither may restate.
//
// What these guards can and cannot establish. There is no browser in the Go suite
// and SPEC/BUILD.md rules out a JavaScript toolchain, so nothing here measures a
// rendered board: whether 60vh presents a useful number of cards, and whether three
// divided columns are comfortable at a given width, are judged against a running
// server. What IS checkable hermetically is the mechanism those measurements depend
// on, in the exact bytes the binary serves — that the board declares the specified
// height, that it reads its floor from the one place the length is written, that
// the property resolves on a page carrying no full-height shell, that each column
// scrolls its own cards, that the three columns are sized to divide and floored so
// they cannot divide away to nothing, and that both boards resolve to one rule for
// every length they share. A stylesheet satisfying all of that can still measure
// badly; one failing any of it cannot measure well.

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

// TestSprintBoard_ColumnsDivideTheBoardWidthEqually is the gate for the first
// half of Acceptance Criterion 139: the board's three columns divide its width
// equally, all three carry the same width whatever number of tasks each holds,
// that width grows with the viewport and leaves no unused space beside them, and
// no column is ever narrower than 17rem.
//
// The criterion asks for the columns to be measured at a viewport wide enough for
// the equal division and again at one too narrow for it, "because a board measured
// at one width alone passes as readily on columns that never grow as on columns
// that never stop growing". There is no browser in the Go suite, so what is
// asserted here is the MECHANISM those two measurements would read, in the exact
// bytes the binary serves, and it is asserted at both ends deliberately:
//
//   - the wide end is `flex: 1 1 0` — a flex basis of zero and a positive grow
//     factor, declared once for all three columns, which is what makes the three
//     shares equal and makes them absorb everything the board leaves once the gaps
//     are taken out. A basis of `auto` would size each column to its cards and a
//     grow factor of `0` would leave the space beside them empty;
//   - the narrow end is `min-width: 17rem` on the shared rule, a floor the flex
//     layout may not shrink past, together with `overflow-x: auto` on the board,
//     which is what turns the excess into the strip's own scroll rather than into
//     narrower columns or a scrolling `<body>`.
//
// A stylesheet satisfying both can still measure badly in a browser; one failing
// either cannot measure well at the end it fails.
func TestSprintBoard_ColumnsDivideTheBoardWidthEqually(t *testing.T) {
	sheet := projectStyleSheet(t)

	const overrideSelector = ".task-board--bounded > .task-board__column"
	override := soleCSSRule(t, sheet, overrideSelector)

	// The wide end: one rule, so one flex sizing for all three columns — equal by
	// construction rather than by three values that happen to agree.
	grow, shrink, basis := cssFlexShorthand(t, override, overrideSelector)
	if grow == "0" {
		t.Errorf("%s declares flex-grow %q; a column that does not grow leaves the space beside "+
			"the three empty instead of dividing the board's width (Acceptance Criterion 139)",
			overrideSelector, grow)
	}
	if !isCSSZeroLength(basis) {
		t.Errorf("%s declares flex-basis %q, want a zero basis; a basis of `auto` sizes each "+
			"column to its own cards, so the three widths would follow the data instead of "+
			"the viewport", overrideSelector, basis)
	}
	if shrink == "" {
		t.Errorf("%s declares no flex-shrink; the shorthand states all three factors so the "+
			"column's sizing is not half inherited from the rule it overrides", overrideSelector)
	}

	// And it does not carry the tasks board's fixed width, in either of the two
	// ways it could: not in this rule, and not by leaving the 19rem of the rule it
	// overrides standing in the cascade.
	if got := cssDeclarations(override, "width"); len(got) != 1 || got[0] != "auto" {
		t.Errorf("%s declares width: %v, want exactly %q; the tasks board's 19rem is what this "+
			"rule exists to override, and a flex item that stopped being one would apply it",
			overrideSelector, got, "auto")
	}

	// The narrow end: the floor, and the container that scrolls once the floor
	// stops the columns from shrinking any further.
	column := soleCSSRule(t, sheet, ".task-board__column")
	if got := cssDeclarations(column, "min-width"); len(got) != 1 || got[0] != "17rem" {
		t.Errorf(".task-board__column declares min-width: %v, want exactly %q; it is the width "+
			"below which a card's text stops being legible, and it floors the division above",
			got, "17rem")
	}
	board := soleCSSRule(t, sheet, ".task-board")
	if got := cssDeclarations(board, "overflow-x"); len(got) != 1 || got[0] != "auto" {
		t.Errorf(".task-board declares overflow-x: %v, want exactly %q; at the floor the excess "+
			"has to become the STRIP's scroll, or it becomes the page's (Acceptance Criteria 27 "+
			"and 139)", got, "auto")
	}

	// The override restates none of the lengths the two boards share. Restating one
	// would be a second copy free to be changed on its own, and the boards would
	// then meet at a different minimum column, gap, or card measure.
	for _, shared := range []string{"min-width", "gap", "padding"} {
		if got := cssDeclarations(override, shared); len(got) != 0 {
			t.Errorf("%s declares %s: %v; that length is shared by both boards and is declared "+
				"once, on the rule this one overrides — a second copy is a copy that can be "+
				"changed on its own", overrideSelector, shared, got)
		}
	}

	// It wins the cascade on its own terms: it is the more specific selector AND
	// the later rule, so no !important is needed and none is used.
	base := cssRulePositions(sheet, ".task-board__column")
	after := cssRulePositions(sheet, overrideSelector)
	if len(base) != 1 || len(after) != 1 {
		t.Fatalf("the sheet carries %d rules for %q and %d for %q, want exactly one of each",
			len(base), ".task-board__column", len(after), overrideSelector)
	}
	if after[0] <= base[0] {
		t.Errorf("%s is declared before the rule it overrides; equal specificity would then "+
			"leave the fixed width standing", overrideSelector)
	}
	if strings.Contains(strings.ToLower(override), "!important") {
		t.Errorf("%s carries an !important; it is one class more specific than the rule it "+
			"overrides and comes after it, so the cascade already settles this", overrideSelector)
	}

	// Every length in either rule is in rem or is the unitless zero of the flex
	// basis, so the board follows the reader's own text size.
	for _, value := range append(cssDeclarations(column, "min-width"), cssDeclarations(column, "width")...) {
		if !strings.HasSuffix(value, "rem") {
			t.Errorf(".task-board__column declares the length %q; the board's lengths are in "+
				"rem so they scale with the reader's text size, and a length in px does not",
				value)
		}
	}
}

// TestSprintBoard_OverrideReachesTheSprintBoardAndOnlyIt is the markup half of
// Acceptance Criterion 139, and it is the half a stylesheet-only assertion cannot
// make. The width rule above is keyed on the `--bounded` modifier through a CHILD
// combinator, so it selects nothing at all if the sprint board stops emitting that
// class or wraps its columns in one more element — and every assertion above still
// passes while the three columns silently return to the tasks board's fixed width.
//
// The criterion also requires the tasks board to be unchanged, which is the other
// direction of the same check: that board must NOT carry the modifier, or its five
// columns would divide the viewport too and lose the 19rem the measure of a card's
// title depends on (Acceptance Criterion 129 continues to hold).
func TestSprintBoard_OverrideReachesTheSprintBoardAndOnlyIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	sprintPage := servePage(t, mux, f.path())
	tasksPage := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")

	// The modifier the rule is keyed on, on the board that must be reached.
	if !boardClassTokens(t, sprintPage, "the sprint page")["task-board--bounded"] {
		t.Fatalf("the sprint page's board does not carry task-board--bounded, so the column " +
			"width rule keyed on it selects nothing and the three columns keep the tasks " +
			"board's fixed 19rem")
	}
	// And not on the board that must not.
	if boardClassTokens(t, tasksPage, "the tasks page")["task-board--bounded"] {
		t.Errorf("the tasks page's board carries task-board--bounded; its five columns would " +
			"then divide the viewport as well, and each would be narrow enough to hurt the " +
			"measure a card's title is read on (Acceptance Criterion 129)")
	}

	// The child combinator: a column is a DIRECT child of the board container.
	// Anything between the two leaves the rule matching nothing.
	region := memberBoardRegion(t, sprintPage)
	gap := region[len(`data-role="task-board">`):]
	firstColumn := strings.Index(gap, `<div class="card task-board__column"`)
	if firstColumn < 0 {
		t.Fatalf("the sprint board renders no `.task-board__column` element at all")
	}
	if strings.Contains(gap[:firstColumn], "<") {
		t.Errorf("the sprint board wraps its columns in %q; `.task-board--bounded > "+
			".task-board__column` is a CHILD combinator and selects nothing once anything sits "+
			"between the board and its columns", strings.TrimSpace(gap[:firstColumn]))
	}

	// The board emits no inline style on either page: this is a stylesheet change
	// and Acceptance Criterion 62 continues to hold.
	if strings.Contains(region, "style=") {
		t.Errorf("the member-tasks board carries an inline style attribute")
	}
}

// TestBoards_ShareTheMinimumGapAndCardPadding is the gate for the last half of
// Acceptance Criterion 139: the two boards' column widths are deliberately NOT one
// value any more, and what they still share is the `17rem` minimum, the `0.75rem`
// gap, and the `0.75rem` card body padding. The criterion requires those three to
// be compared across the two boards and the check to fail when they diverge.
//
// The comparison is made where divergence would actually happen: the classes each
// board's markup emits. Both boards emit the same board class and the same column
// and card classes, so the three shared lengths are literally one declaration read
// twice; a board that grew a column class of its own would resolve to a different
// rule and fail here, however closely the two rules' values agreed on the day it
// was written.
//
// The tasks board's own two lengths are pinned here as well, because the criterion
// requires them unchanged: `19rem` with `flex: 0 0 auto`, on the shared rule, which
// is what the sprint board's rule overrides and what every other board keeps.
func TestBoards_ShareTheMinimumGapAndCardPadding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	sprintPage := servePage(t, mux, f.path())
	tasksPage := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")

	// The column and the card are the same object on both boards: the same class
	// tokens, so the same rules, so the same shared lengths.
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
				"%v; the two boards keep ONE minimum column, ONE gap and ONE card measure, and "+
				"they keep them by selecting the same rules rather than by two rules that "+
				"happen to agree", part.what, sortedKeys(onSprint), sortedKeys(onTasks))
		}
	}

	// The board containers differ by exactly one token — the modifier that carries
	// the sprint board's bounded height and its fluid columns — so the two boards
	// still resolve to the same `.task-board` rule for everything else, the gap
	// included.
	sprintBoard := boardClassTokens(t, sprintPage, "the sprint page")
	tasksBoard := boardClassTokens(t, tasksPage, "the tasks page")
	for class := range tasksBoard {
		if !sprintBoard[class] {
			t.Errorf("the tasks board carries %q and the sprint board does not; the rules the "+
				"two share are the rules both boards select", class)
		}
	}
	for class := range sprintBoard {
		if !tasksBoard[class] && class != "task-board--bounded" && class != "mb-3" {
			t.Errorf("the sprint board carries the extra class %q; its only declared departure "+
				"from the tasks board is task-board--bounded, on which the stylesheet keys "+
				"both its bounded height and its fluid columns", class)
		}
	}

	// The three shared lengths, each declared once, and each on a rule both boards
	// select.
	sheet := projectStyleSheet(t)

	column := soleCSSRule(t, sheet, ".task-board__column")
	if got := cssDeclarations(column, "min-width"); len(got) != 1 || got[0] != "17rem" {
		t.Errorf(".task-board__column declares min-width: %v, want exactly %q; both boards read "+
			"this one minimum (Acceptance Criterion 139)", got, "17rem")
	}
	board := soleCSSRule(t, sheet, ".task-board")
	if got := cssDeclarations(board, "gap"); len(got) != 1 || got[0] != "0.75rem" {
		t.Errorf(".task-board declares gap: %v, want exactly %q; the columns of both boards are "+
			"separated by one gap", got, "0.75rem")
	}
	cardBody := soleCSSRule(t, sheet, ".task-card > .card-body")
	if got := cssUniformRemPadding(t, cardBody, ".task-card > .card-body"); got != 0.75 {
		t.Errorf(".task-card > .card-body declares padding %grem, want 0.75rem; the card measure "+
			"is one measure on both boards", got)
	}

	// The tasks board's own width, unchanged, on the rule the sprint board
	// overrides. A board that moved it elsewhere would leave the sprint board's
	// override selecting a property nothing declares.
	for prop, want := range map[string]string{"width": "19rem", "flex": "0 0 auto"} {
		if got := cssDeclarations(column, prop); len(got) != 1 || got[0] != want {
			t.Errorf(".task-board__column declares %s: %v, want exactly %q; the tasks board's "+
				"five columns are unchanged by the sprint board's division of its own width "+
				"(Acceptance Criteria 129 and 139)", prop, got, want)
		}
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

// cssFlexShorthand returns the three factors of a declaration block's single
// `flex` shorthand: the grow factor, the shrink factor, and the basis.
//
// The shorthand is required to state all three, so a rule that overrides another
// rule's `flex` replaces the whole sizing rather than half of it and leaves
// nothing to be inherited from the declaration it is meant to supersede.
func cssFlexShorthand(t *testing.T, block, selector string) (grow, shrink, basis string) {
	t.Helper()

	values := cssDeclarations(block, "flex")
	if len(values) != 1 {
		t.Fatalf("%s declares flex %v, want exactly one declaration", selector, values)
	}
	fields := strings.Fields(values[0])
	if len(fields) != 3 {
		t.Fatalf("%s declares flex %q; the shorthand must state the grow factor, the shrink "+
			"factor and the basis, not %d of the three", selector, values[0], len(fields))
	}
	return fields[0], fields[1], fields[2]
}

// isCSSZeroLength reports whether a flex basis is a zero of any of the forms CSS
// accepts in the shorthand, so the assertion states the SIZING rather than the
// spelling the sheet happened to use for it.
func isCSSZeroLength(basis string) bool {
	switch basis {
	case "0", "0px", "0%", "0rem":
		return true
	default:
		return false
	}
}

// cssRulePositions returns the document position of every unconditional rule
// whose selector list carries selector exactly, in source order, which is what
// "the override comes after the rule it overrides" is read from.
func cssRulePositions(sheet, selector string) []int {
	var at []int
	for i, rule := range parseCSSRules(sheet) {
		if rule.nested {
			continue
		}
		for _, s := range strings.Split(rule.prelude, ",") {
			if normaliseSelector(s) == normaliseSelector(selector) {
				at = append(at, i)
				break
			}
		}
	}
	return at
}
