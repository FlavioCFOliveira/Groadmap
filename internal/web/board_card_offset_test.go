package web

import (
	"strings"
	"testing"
)

// The guards in this file cover the leading edge of a card on the sprint page's
// member-tasks board, the one Kanban board the interface renders.
//
// What they are written against. A board card is an <a> carrying Tabler's card
// and card-link classes (SPEC/WEB.md § Sprint Detail Sub-Template, The card is a
// link to the task page), and the
// vendored distribution carries `.card-link+.card-link { margin-inline-start:
// var(--tblr-card-spacer-x) }`, spacing meant for inline text links inside one
// card. A column's cards are adjacent siblings, so without an override every
// card after the first was indented by the card's horizontal spacer and the
// first card read wider than the ones below it — contradicting the
// specification's "making the card a link changes the element, not the
// appearance".
//
// As with the other stylesheet guards, there is no browser in the Go suite: what
// is checked is the mechanism, in the exact bytes the binary serves — that the
// vendored rule is still the one the override counters, that the override
// exists and zeroes the property, that it needs no !important, and that the
// served markup is shaped so its child and sibling combinators reach the cards.

// boardCardSiblingSelector is the project rule that neutralises the vendored
// sibling margin for board cards.
const boardCardSiblingSelector = ".task-board__cards > .task-card + .task-card"

// TestTaskCard_VendoredSiblingLinkMarginIsNeutralised asserts the override for
// the vendored `.card-link+.card-link` margin exists, zeroes the margin, and
// wins the cascade on specificity and source order alone.
//
// The vendored half is pinned too: a re-vendored Tabler that renames or drops
// the rule would leave the override countering nothing, and a Tabler that moved
// the offset onto another property would bring the indent back with the
// override still in place. Either change fails here rather than silently.
func TestTaskCard_VendoredSiblingLinkMarginIsNeutralised(t *testing.T) {
	vendored := soleCSSRule(t, embeddedSheet(t, "static/vendor/tabler/tabler.min.css"),
		".card-link + .card-link")
	if got := cssDeclarations(vendored, "margin-inline-start"); len(got) != 1 ||
		got[0] != "var(--tblr-card-spacer-x)" {
		t.Errorf("the vendored .card-link+.card-link declares margin-inline-start: %v, "+
			"want exactly %q; the project override was written against that rule and "+
			"must be revisited when it changes", got, "var(--tblr-card-spacer-x)")
	}
	for _, prop := range []string{"margin", "margin-left", "margin-right", "margin-inline"} {
		if got := cssDeclarations(vendored, prop); len(got) != 0 {
			t.Errorf("the vendored .card-link+.card-link now also declares %s: %v; the "+
				"project override zeroes margin-inline-start only and would not "+
				"neutralise it", prop, got)
		}
	}

	project := soleCSSRule(t, projectStyleSheet(t), boardCardSiblingSelector)
	if got := cssDeclarations(project, "margin-inline-start"); len(got) != 1 || got[0] != "0" {
		t.Errorf("%s declares margin-inline-start: %v, want exactly %q; without it every "+
			"board card after the first in a column is indented by the vendored "+
			".card-link+.card-link margin (SPEC/WEB.md § Sprint Detail Sub-Template, "+
			"The card is a link to the task page)",
			boardCardSiblingSelector, got, "0")
	}

	// Three classes against the vendored two: specificity alone settles the
	// cascade in the override's favour, whatever the order the sheets are linked.
	if strings.Contains(strings.ToLower(project), "!important") {
		t.Errorf("%s carries an !important; its selector outranks the vendored "+
			".card-link+.card-link and needs no help to win", boardCardSiblingSelector)
	}
}

// TestTaskCard_IsADirectChildOfTheBoardCardList asserts, on the sprint page's
// board, that a card's parent element is the column's card list, which is what the
// override's child combinator requires. The tasks page renders no board card at
// all, which is asserted too, so the guard covers every page that could carry one.
//
// This is the half a stylesheet-only assertion cannot make: wrapping each card in
// one more element would leave the override matching nothing while the test above
// still passes. (Such a wrapper would also make the cards stop being siblings and
// so drop the vendored margin, but nothing would then pin that it stays dropped.)
func TestTaskCard_IsADirectChildOfTheBoardCardList(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	sprintID := seedSprintWithMembers(t, "settlement-window", 2)
	mux := buildMux()

	const cardMarker = `class="card card-sm card-link text-reset task-card"`
	if strings.Contains(servePage(t, mux, "/roadmaps/settlement-window/tasks"), cardMarker) {
		t.Errorf("the roadmap tasks page renders a board card; it presents one list (Acceptance Criterion 81)")
	}
	for _, page := range []struct{ name, path string }{
		{"the sprint page", "/roadmaps/settlement-window/sprints/" + itoa(sprintID)},
	} {
		body := servePage(t, mux, page.path)
		chain := ancestorChain(t, body, cardMarker)
		if len(chain) == 0 {
			t.Fatalf("%s: the board card has no ancestor at all in the served page", page.name)
		}
		parent := chain[len(chain)-1]
		if !strings.HasPrefix(parent, "div.") || !strings.Contains(parent+".", ".task-board__cards.") {
			t.Errorf("%s: the board card's parent element is %q; it must be the column's "+
				".task-board__cards list, because %q is a child combinator and selects "+
				"nothing once anything sits between them", page.name, parent,
				boardCardSiblingSelector)
		}
	}
}
