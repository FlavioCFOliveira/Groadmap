package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// reSummaryLineText matches any text of the retired sprint status summary line's
// form, `<n>% - P:` — the prefix Acceptance Criterion 39 names — however the
// counts after it read.
var reSummaryLineText = regexp.MustCompile(`\d+% - P:`)

// TestSprintPage_RendersNoStatusSummaryLine is the regression guard for
// Acceptance Criterion 39: the served sprint page carries no element with
// data-role="sprint-summary" and no text of the form `<pct>% - P:<p> A:<a>
// C:<c> - T:<t>`, and the sprint presentation opens with the Sprint details
// card, directly below the page header (SPEC/WEB.md § Sprint Detail
// Sub-Template, rule 2).
//
// The fixture's sprint has member tasks in all three board columns, so a line
// computed from them would carry non-zero counts and could not hide as an
// all-zero string.
func TestSprintPage_RendersNoStatusSummaryLine(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintBoardFixture(t, "settlement-platform")
	body := servePage(t, buildMux(), f.path())

	// Falsifiability control: the pattern matches the line the page used to
	// carry, so a clean sweep below means the line is absent.
	if !reSummaryLineText.MatchString(`<div class="h3 mb-3" data-role="sprint-summary">17% - P:3 A:2 C:1 - T:6</div>`) {
		t.Fatal("the summary-line pattern does not match the retired line; the sweep would be vacuous")
	}
	// And the page is the sprint page, with its board, not an error page.
	if !strings.Contains(body, `data-role="task-board"`) {
		t.Fatalf("the served page carries no member-tasks board:\n%s", body)
	}

	if strings.Contains(body, `data-role="sprint-summary"`) {
		t.Error(`the sprint page carries an element with data-role="sprint-summary"`)
	}
	if m := reSummaryLineText.FindString(body); m != "" {
		t.Errorf("the sprint page carries summary-line text %q", m)
	}

	// The presentation opens with the Sprint details card: it is the first thing
	// the page body's container holds.
	const container = `<main class="page-body">`
	start := strings.Index(body, container)
	if start < 0 {
		t.Fatalf("the sprint page carries no %s", container)
	}
	opening := `<div class="container-xl">`
	rest := body[start+len(container):]
	at := strings.Index(rest, opening)
	if at < 0 {
		t.Fatalf("the page body holds no %s", opening)
	}
	first := strings.TrimSpace(rest[at+len(opening):])
	const detailsCard = `<div class="card mb-3">
            <div class="card-header">
              <h3 class="card-title">Sprint details</h3>`
	if !strings.HasPrefix(first, detailsCard) {
		t.Errorf("the sprint presentation does not open with the Sprint details card; it opens with %q",
			first[:min(160, len(first))])
	}
}

// TestSprintDetail_FullBlockOnlyOnSprintPage asserts that the full sprint detail
// block — the metadata datagrid (Created/Started/Closed) and the member-tasks
// board with its three fixed columns — is rendered ONLY on the single Roadmap Sprint Page, and that the
// Actual tab of the roadmap sprints page does NOT render it for the OPEN sprint:
// there the OPEN sprint is shown through the shared sprint-card partial, with no
// datagrid, no member-tasks board, and no per-task modal
// (SPEC/WEB.md § Shared Sprint-Card Partial, § Sprint Detail Sub-Template;
// Acceptance Criteria 8/12/38).
//
// The board markers replaced the six <th> headers of the member-tasks table this
// board supersedes. The test's subject is unchanged — where the full detail block
// may appear, and where it may not — so the markers moved to the presentation that
// now carries it: the board container, its column, and the WAITING heading with a
// count badge, which no other page emits.
func TestSprintDetail_FullBlockOnlyOnSprintPage(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintFixture(t, "web-shared-detail")
	mux := buildMux()

	sprintsPage := servePage(t, mux, "/roadmaps/"+f.name)
	sprintPage := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.openID))

	// Slice the Actual pane out of the sprints page so the absence assertions
	// target the OPEN sprint's card, not some other tab.
	current := paneSlice(t, sprintsPage, `<div id="tab-current"`)

	datagridTitles := []string{">Created<", ">Started<", ">Closed<"}
	// The WAITING column's header carries the colour of the status that column
	// groups — SPRINT's, the canonical status of the group — so the marker is
	// built from the semantic helper rather than from a colour literal, and this
	// test keeps stating WHERE the block may appear while the colour itself is
	// pinned by TestBoardColumnBadges_CarryTheColourOfTheStatusTheyGroup
	// (Acceptance Criterion 140).
	boardMarkers := []string{
		`class="task-board task-board--bounded mb-3" data-role="task-board"`,
		`<div class="card task-board__column" data-role="task-board-column">`,
		`<h3 class="card-title">WAITING <span class="badge ` +
			taskStatusBadge(models.StatusSprint) + ` ms-2">2</span></h3>`,
	}

	// The single sprint page MUST carry the full detail block.
	for _, m := range datagridTitles {
		if !strings.Contains(sprintPage, m) {
			t.Errorf("single sprint page: detail block missing datagrid title %q", m)
		}
	}
	for _, m := range boardMarkers {
		if !strings.Contains(sprintPage, m) {
			t.Errorf("single sprint page: detail block missing member-tasks board markup %q", m)
		}
	}
	// And no table of tasks at all on this page: the board replaced it outright
	// (Acceptance Criterion 130).
	for _, absent := range []string{"<table", "<th>", "<tbody"} {
		if strings.Contains(sprintPage, absent) {
			t.Errorf("single sprint page: the member tasks are still presented in a table (%q)", absent)
		}
	}
	if !strings.Contains(sprintPage, "Build the read-only sprint page route and template") {
		t.Errorf("single sprint page: detail block missing the OPEN sprint's member task")
	}
	if !strings.Contains(sprintPage, `data-task-id="`+itoa(f.openTaskID)+`"`) {
		t.Errorf("single sprint page: member task card not wired to the task detail modal")
	}

	// The Actual tab MUST NOT carry any part of the full detail block.
	for _, m := range datagridTitles {
		if strings.Contains(current, m) {
			t.Errorf("Actual tab must not render the metadata datagrid title %q", m)
		}
	}
	for _, m := range boardMarkers {
		if strings.Contains(current, m) {
			t.Errorf("Actual tab must not render the member-tasks board markup %q", m)
		}
	}
	if strings.Contains(current, `data-bs-target="#task-modal-`) {
		t.Errorf("Actual tab must not render a per-task modal trigger")
	}
	// The OPEN sprint IS present on the Actual tab, as a card linking to its page.
	if !strings.Contains(current, "/sprints/"+itoa(f.openID)) {
		t.Errorf("Actual tab missing the OPEN sprint card link")
	}
	if !strings.Contains(current, "2 task(s)") {
		t.Errorf("Actual tab card does not show the OPEN sprint's task count")
	}
}

// TestSprintsPage_SharedCardAcrossAllTabs asserts that all three tabs of the
// roadmap sprints page render their sprints through the SINGLE shared
// sprint-card partial, so the card markup is identical across Próximos, Actual,
// and Concluídos. It checks the partial's distinctive markup (the
// "card card-sm card-link" link wrapping a "Sprint #<ID>" header and a
// "task(s)" footer) appears in every populated pane, and that the total number
// of cards equals the total number of sprints — proving no tab uses a divergent
// layout and the OPEN sprint is a card, not an expanded block (SPEC/WEB.md
// § Shared Sprint-Card Partial; Acceptance Criteria 8/12/13/38).
func TestSprintsPage_SharedCardAcrossAllTabs(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintFixture(t, "web-shared-card")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name)

	const cardMarker = `class="card card-sm card-link text-reset"`

	// The fixture seeds 5 sprints (2 PENDING, 1 OPEN, 2 CLOSED); every one must be
	// rendered as exactly one shared card.
	if got := strings.Count(body, cardMarker); got != 5 {
		t.Errorf("expected 5 shared sprint cards (one per sprint), found %d", got)
	}

	// Each populated pane uses the shared card markup, proving no tab diverges.
	panes := map[string]string{
		"Próximos":   paneSlice(t, body, `<div id="tab-upcoming"`),
		"Actual":     paneSlice(t, body, `<div id="tab-current"`),
		"Concluídos": paneSlice(t, body, `<div id="tab-closed"`),
	}
	for label, pane := range panes {
		if !strings.Contains(pane, cardMarker) {
			t.Errorf("%s tab does not render the shared sprint-card markup", label)
		}
		if !strings.Contains(pane, "task(s)") {
			t.Errorf("%s tab card does not show a task-count footer", label)
		}
	}

	// Every card carries a status badge in its header (Acceptance Criterion 13).
	// The badge colour is now the semantic Tabler variant for the sprint's status
	// (SPEC/WEB.md § Status, Priority, and Severity Badge Colours), so the count
	// is taken across all card-header status-badge variants rather than a single
	// fixed colour: the fixture seeds 2 PENDING (bg-secondary-lt), 1 OPEN
	// (bg-blue-lt), and 2 CLOSED (bg-green-lt).
	statusBadges := strings.Count(body, `<div class="card-actions"><span class="badge bg-secondary-lt">`) +
		strings.Count(body, `<div class="card-actions"><span class="badge bg-blue-lt">`) +
		strings.Count(body, `<div class="card-actions"><span class="badge bg-green-lt">`)
	if statusBadges != 5 {
		t.Errorf("expected 5 card status badges (one per card), found %d", statusBadges)
	}
	// Assert the semantic mapping is actually applied per status, not a single
	// fixed colour: PENDING -> secondary, OPEN -> blue, CLOSED -> green.
	if got := strings.Count(body, `<div class="card-actions"><span class="badge bg-secondary-lt">`); got != 2 {
		t.Errorf("expected 2 PENDING cards with bg-secondary-lt status badge, found %d", got)
	}
	if got := strings.Count(body, `<div class="card-actions"><span class="badge bg-blue-lt">`); got != 1 {
		t.Errorf("expected 1 OPEN card with bg-blue-lt status badge, found %d", got)
	}
	if got := strings.Count(body, `<div class="card-actions"><span class="badge bg-green-lt">`); got != 2 {
		t.Errorf("expected 2 CLOSED cards with bg-green-lt status badge, found %d", got)
	}
}

// TestSprintsPage_ClosedCardsShowTaskCount asserts every sprint card under the
// Concluídos tab shows the sprint's total task count (SPEC/WEB.md § Roadmap
// Sprints Page, Concluídos; Acceptance Criterion 40). The Próximos cards already
// show their task count, asserted indirectly by the existing fixture, but the
// closed cards previously showed only the closed_at date.
func TestSprintsPage_ClosedCardsShowTaskCount(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintFixture(t, "web-closed-counts")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name)
	closed := paneSlice(t, body, `<div id="tab-closed"`)

	// Both CLOSED sprints in the fixture have zero member tasks, so each card
	// must show "0 task(s)". The marker is unambiguous to the count display.
	if got := strings.Count(closed, "task(s)"); got != 2 {
		t.Errorf("Concluídos: found %d task-count displays, want 2 (one per closed card)", got)
	}
	if !strings.Contains(closed, "0 task(s)") {
		t.Errorf("Concluídos: a closed sprint card does not show its task count")
	}
}

// TestSprintsPage_UpcomingCardsShowTaskCount asserts every sprint card under the
// Próximos tab shows the sprint's total task count (SPEC/WEB.md § Roadmap
// Sprints Page, Próximos; Acceptance Criterion 40). The fixture's first PENDING
// sprint has two member tasks; the second has none.
func TestSprintsPage_UpcomingCardsShowTaskCount(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintFixture(t, "web-upcoming-counts")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name)
	upcoming := paneSlice(t, body, `<div id="tab-upcoming"`)

	if got := strings.Count(upcoming, "task(s)"); got != 2 {
		t.Errorf("Próximos: found %d task-count displays, want 2 (one per pending card)", got)
	}
	// pendingID has two member tasks; pendingID2 has none.
	if !strings.Contains(upcoming, "2 task(s)") {
		t.Errorf("Próximos: the two-task pending sprint card does not show '2 task(s)'")
	}
	if !strings.Contains(upcoming, "0 task(s)") {
		t.Errorf("Próximos: the empty pending sprint card does not show '0 task(s)'")
	}
}
