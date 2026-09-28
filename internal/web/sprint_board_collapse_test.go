package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// This file is the gate for the column collapse of the Roadmap Sprint Page's
// member-tasks board, as far as it is observable without a browser: the markup
// the server renders for the toggles and the column bodies, the script that
// carries the behaviour and the page that loads it, and the stylesheet rules the
// collapsed strip is declared by (SPEC/WEB.md § Sprint Detail Sub-Template, rule
// 4, Column collapse; Acceptance Criteria 138 and 212 to 218).
//
// What a browser alone can observe — the measured widths before and after a
// collapse, the computed writing mode, the focus staying on the toggle — is not
// asserted here. What IS asserted is everything that behaviour is built from, so
// a regression in any of the pieces fails a test rather than a manual check.

// columnToggleOpen is the opening of a column collapse toggle, exactly as the
// specification states it: a real button, Tabler's card-header action class, and
// the data-role hook the script finds it by.
const columnToggleOpen = `<button type="button" class="btn-action" data-role="task-board-column-toggle"`

// sprintBoardToggleColumns is the heading and the body id of each column, left to
// right, exactly as the specification spells them (Acceptance Criterion 213).
var sprintBoardToggleColumns = []struct {
	heading string
	bodyID  string
}{
	{"WAITING", "sprint-board-column-waiting"},
	{"DOING", "sprint-board-column-doing"},
	{"CLOSED", "sprint-board-column-closed"},
}

// servedToggle is the whole markup of one column's toggle as the server must
// render it: in its expanded state, naming its column's body, and hidden until
// the script initialises (Acceptance Criteria 213 and 218).
func servedToggle(heading, bodyID string) string {
	return columnToggleOpen + ` aria-expanded="true" aria-controls="` + bodyID +
		`" aria-label="Collapse ` + heading + ` column" hidden>` +
		`<i class="ti ti-chevron-left" aria-hidden="true"></i></button>`
}

// TestSprintBoardCollapse_EachHeaderCarriesOneServedToggle is the gate for the
// server-rendered half of Acceptance Criteria 213 and 218: every column header
// carries exactly one toggle, inside Tabler's card-actions container, after the
// heading and its count badge, in its expanded state, with the hidden attribute,
// and naming the id of its OWN column's body — an id that occurs exactly once in
// the page.
//
// It is run over two sprints: one whose three columns all hold cards, and one
// whose DOING and CLOSED columns are empty, so the toggle and its body are pinned
// around an empty state as well as around a card list.
func TestSprintBoardCollapse_EachHeaderCarriesOneServedToggle(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	mux := buildMux()

	full := seedSprintBoardFixture(t, "settlement-platform")
	sparseID := seedSprintWithMembers(t, "settlement-window", 2)

	for _, path := range []string{
		full.path(),
		"/roadmaps/settlement-window/sprints/" + itoa(sparseID),
	} {
		page := servePage(t, mux, path)
		columns := memberBoardColumns(t, page)

		for i, want := range sprintBoardToggleColumns {
			column := columns[i]

			// Exactly one toggle per column.
			if got := strings.Count(column, columnToggleOpen); got != 1 {
				t.Errorf("%s: the %s column carries %d toggles, want exactly 1",
					path, want.heading, got)
			}

			// The header, whole: the heading and its badge, THEN the actions
			// container holding the toggle, closing the header, and the body that
			// follows it carrying the id the toggle names. Pinning the sequence is
			// what proves the toggle sits at the header's trailing edge and controls
			// its own column's body rather than another's.
			header := regexp.MustCompile(`<h3 class="card-title">` + want.heading +
				` <span class="badge bg-[a-z]+-lt ms-2">\d+</span></h3>\s*` +
				`<div class="card-actions">\s*` + regexp.QuoteMeta(servedToggle(want.heading, want.bodyID)) +
				`\s*</div>\s*</div>\s*` +
				regexp.QuoteMeta(`<div class="card-body task-board__cards" id="`+want.bodyID+`">`))
			if !header.MatchString(column) {
				t.Errorf("%s: the %s column's header does not carry the specified toggle after its "+
					"heading and badge, followed by the body it controls; want the toggle\n%s\n"+
					"column:\n%s", path, want.heading, servedToggle(want.heading, want.bodyID), column)
			}

			// The body id is unique within the page, so aria-controls resolves to one
			// element and the script hides that element alone.
			if got := strings.Count(page, `id="`+want.bodyID+`"`); got != 1 {
				t.Errorf("%s: the id %q occurs %d times in the page, want exactly 1",
					path, want.bodyID, got)
			}
		}

		// Being a real button, the toggle needs no tabindex and no role; either
		// would mark an element that cannot be pressed natively.
		// The attribute is matched at an attribute boundary: data-role, which the
		// toggle carries, contains "role" and is not the attribute meant here.
		for _, toggle := range reColumnToggle.FindAllString(page, -1) {
			if attr := reTabindexOrRole.FindString(toggle); attr != "" {
				t.Errorf("%s: a column toggle carries%s: %s", path, attr, toggle)
			}
		}
		if got := len(reColumnToggle.FindAllString(page, -1)); got != 3 {
			t.Errorf("%s: the page carries %d column toggles, want 3", path, got)
		}
	}
}

// reColumnToggle captures the opening tag of every column toggle in a page.
var reColumnToggle = regexp.MustCompile(`<button[^>]*data-role="task-board-column-toggle"[^>]*>`)

// reTabindexOrRole matches a tabindex or a role attribute of an opening tag.
var reTabindexOrRole = regexp.MustCompile(`\s(?:tabindex|role)\s*=`)

// TestSprintBoardCollapse_EveryPageLoadIsExpandedAndNothingIsPersisted is the
// gate for the server-rendered half of Acceptance Criterion 212: the served board
// renders all three columns expanded — no column carries the collapsed modifier
// and no body carries the hidden attribute — the response sets no cookie, and the
// route reads no query parameter for the state, so a request carrying one
// renders the same board as a request carrying none.
func TestSprintBoardCollapse_EveryPageLoadIsExpandedAndNothingIsPersisted(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintBoardFixture(t, "settlement-platform")
	srv := handler()

	serve := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", path, rec.Code)
		}
		return rec
	}

	rec := serve(f.path())
	page := rec.Body.String()
	region := memberBoardRegion(t, page)

	if strings.Contains(page, "task-board__column--collapsed") {
		t.Errorf("the served page carries the collapsed modifier; every page load renders all " +
			"three columns expanded")
	}
	const expandedColumn = `<div class="card task-board__column" data-role="task-board-column">`
	if got := strings.Count(region, expandedColumn); got != 3 {
		t.Errorf("the board carries %d column elements served as %s, want all 3", got, expandedColumn)
	}
	for _, want := range sprintBoardToggleColumns {
		open := `<div class="card-body task-board__cards" id="` + want.bodyID + `">`
		if !strings.Contains(region, open) {
			t.Errorf("the %s column's body is not served as %s; a body served with the hidden "+
				"attribute, or with any other attribute, is not the expanded state", want.heading, open)
		}
	}
	// Falsifiability control: the board renders its cards, so "no body is hidden"
	// is asserted over a board with content in every column.
	if got := strings.Count(region, cardOpen); got != 6 {
		t.Fatalf("the board renders %d cards, want the fixture's 6", got)
	}

	if cookies := rec.Result().Cookies(); len(cookies) != 0 {
		t.Errorf("the sprint page sets %d cookies; the collapse state is carried in none", len(cookies))
	}

	// A query parameter naming a collapsed column changes nothing: the board
	// region is byte-identical to the one a bare request renders.
	for _, query := range []string{
		"?collapsed=waiting",
		"?collapse=WAITING&collapse=DOING&collapse=CLOSED",
		"?sprint-board-column-doing=collapsed",
	} {
		if got := memberBoardRegion(t, serve(f.path()+query).Body.String()); got != region {
			t.Errorf("GET %s%s renders a different board from GET %s; the route reads no query "+
				"parameter for the collapse state", f.path(), query, f.path())
		}
	}
}

// TestSprintBoardCollapse_ScriptIsServedAndLoadedBySprintPageAlone is the gate
// for the loading half of Acceptance Criteria 217 and 218: the behaviour is served
// as the embedded static/sprint-board.js, the sprint page loads it from /static/
// exactly once and inline script nowhere, the Content-Security-Policy is
// unchanged, and no other page — the tasks page above all — loads it, carries a
// toggle, or carries the collapsed modifier.
func TestSprintBoardCollapse_ScriptIsServedAndLoadedBySprintPageAlone(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintBoardFixture(t, "settlement-platform")
	srv := handler()

	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", path, rec.Code)
		}
		return rec
	}

	asset := get("/static/sprint-board.js")
	if ct := asset.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("GET /static/sprint-board.js: Content-Type = %q, want a JavaScript type", ct)
	}
	if asset.Body.String() != readEmbeddedAsset(t, "static/sprint-board.js") {
		t.Errorf("GET /static/sprint-board.js does not serve the embedded script")
	}

	const tag = `<script src="/static/sprint-board.js"></script>`
	sprint := get(f.path())
	page := sprint.Body.String()
	if got := strings.Count(page, tag); got != 1 {
		t.Errorf("the sprint page loads static/sprint-board.js %d times, want exactly once", got)
	}
	if got := sprint.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Errorf("the sprint page's Content-Security-Policy = %q, want the unchanged %q",
			got, contentSecurityPolicy)
	}
	for _, script := range regexp.MustCompile(`<script\b[^>]*>`).FindAllString(page, -1) {
		if !strings.Contains(script, `src="/static/`) {
			t.Errorf("the sprint page carries an inline or off-origin script: %s", script)
		}
	}
	if handlers := regexp.MustCompile(`(?i)\son[a-z]+\s*=`).FindAllString(memberBoardRegion(t, page), -1); len(handlers) != 0 {
		t.Errorf("the member-tasks board carries inline event-handler attributes: %v", handlers)
	}

	// Every other page: the tasks page, whose five-column board must be
	// unaffected, and the remaining pages of the interface.
	for _, path := range pagePaths(f.name) {
		body := get(path).Body.String()
		for _, forbidden := range []string{
			"sprint-board.js", `data-role="task-board-column-toggle"`, "task-board__column--collapsed",
		} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s carries %q; the column collapse belongs to the sprint page's board alone",
					path, forbidden)
			}
		}
	}
	// Falsifiability control: the tasks page does render a board, so the absence
	// of a toggle above is asserted over a board and not over an empty page.
	if !strings.Contains(get("/roadmaps/"+f.name+"/tasks").Body.String(), `data-role="task-board-column"`) {
		t.Fatalf("the tasks page renders no board column, so asserting it carries no toggle is vacuous")
	}
}

// TestSprintBoardCollapse_ScriptChangesOnlyTheSpecifiedState is the gate for the
// script half of Acceptance Criteria 212, 214, 215, and 217, read at the script's
// source: the script finds the toggles by their data-role, un-hides them, and
// changes a column's state through the body's hidden attribute, the collapsed
// modifier, and the toggle's aria-expanded, aria-label, and chevron class — and
// through nothing else. It issues no request, reads and writes no storage, no
// cookie, and no URL, and writes no style.
func TestSprintBoardCollapse_ScriptChangesOnlyTheSpecifiedState(t *testing.T) {
	script := stripJSComments(readEmbeddedAsset(t, "static/sprint-board.js"))

	for _, want := range []string{
		`'[data-role="task-board-column-toggle"]'`,
		`'[data-role="task-board-column"]'`,
		`"aria-controls"`,
		`"task-board__column--collapsed"`,
		`"ti-chevron-left"`,
		`"ti-chevron-right"`,
		`"Collapse "`,
		`"Expand "`,
		`toggle.hidden = false;`,
		`body.hidden = collapse;`,
		`addEventListener("click"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("static/sprint-board.js does not contain %s", want)
		}
	}

	// Presentation only, and not persisted: no request of any kind, no storage,
	// no cookie, no URL state, and no inline style (Acceptance Criteria 212 and
	// 217). The check is made on the script with its comments removed, so the
	// comment that explains why none of these is used cannot satisfy it.
	for _, banned := range []string{
		"localStorage", "sessionStorage", "indexedDB", "document.cookie",
		"fetch(", "XMLHttpRequest", "sendBeacon", "WebSocket", "EventSource",
		"location", "history", "import(",
		".style", "cssText", `"style"`, "innerHTML", "outerHTML", "insertAdjacentHTML",
		"document.write", "eval(", "Function(",
	} {
		if strings.Contains(script, banned) {
			t.Errorf("static/sprint-board.js uses %q; the collapse issues no request, persists "+
				"nothing, and writes no style", banned)
		}
	}

	// The only attributes the script sets are the toggle's aria-expanded and
	// aria-label; hidden is set through its property, and classes through classList.
	calls := regexp.MustCompile(`\.(setAttribute|removeAttribute|toggleAttribute)\(\s*"([^"]*)"`).
		FindAllStringSubmatch(script, -1)
	if len(calls) == 0 {
		t.Fatalf("static/sprint-board.js sets no attribute at all; the extraction is broken")
	}
	for _, call := range calls {
		if call[1] != "setAttribute" || (call[2] != "aria-expanded" && call[2] != "aria-label") {
			t.Errorf("static/sprint-board.js calls %s(%q); the only attributes it changes are the "+
				"toggle's aria-expanded and aria-label", call[1], call[2])
		}
	}
	if regexp.MustCompile(`\.className\s*=|\.setAttribute\(\s*"class"`).MatchString(script) {
		t.Errorf("static/sprint-board.js replaces a whole class list; it adds and removes the " +
			"one modifier and the one chevron class, and leaves every other class alone")
	}
}

// TestSprintBoardCollapse_StylesheetDeclaresTheStrip is the gate for the
// stylesheet half of Acceptance Criteria 213, 214, and 216: the collapsed column
// is a 3rem strip that takes no share of the board's width and is exempt from the
// 17rem floor of an expanded column; its header stacks the toggle above the
// heading; the heading and its badge read in the vertical-rl writing mode; and the
// toggle presents a touch-friendly hit target and a visible keyboard focus
// indicator that the vendored rule would otherwise remove.
func TestSprintBoardCollapse_StylesheetDeclaresTheStrip(t *testing.T) {
	sheet := projectStyleSheet(t)

	strip := soleCSSRule(t, sheet, ".task-board--bounded > .task-board__column--collapsed")
	for prop, want := range map[string]string{
		"flex":      "0 0 3rem",
		"width":     "3rem",
		"min-width": "3rem",
	} {
		if got := cssDeclarations(strip, prop); len(got) != 1 || got[0] != want {
			t.Errorf("the collapsed column declares %s: %v, want exactly %q", prop, got, want)
		}
	}

	// The strip rule must win over the expanded column rule it overrides. Both
	// carry two classes, so the one declared LATER wins; a strip rule moved above
	// the expanded one would lose its width to `flex: 1 1 0` and its floor to 17rem.
	plain := stripCSSComments(sheet)
	expanded := strings.Index(plain, ".task-board--bounded > .task-board__column {")
	collapsed := strings.Index(plain, ".task-board--bounded > .task-board__column--collapsed {")
	if expanded < 0 || collapsed < 0 || collapsed < expanded {
		t.Errorf("the collapsed column rule (at %d) must follow the expanded column rule (at %d) "+
			"it overrides", collapsed, expanded)
	}

	header := soleCSSRule(t, sheet, ".task-board__column--collapsed > .card-header")
	if got := cssDeclarations(header, "flex-direction"); len(got) != 1 || got[0] != "column" {
		t.Errorf("the collapsed column's header declares flex-direction: %v, want column", got)
	}
	actions := soleCSSRule(t, sheet, ".task-board__column--collapsed .card-actions")
	if got := cssDeclarations(actions, "order"); len(got) != 1 || got[0] != "-1" {
		t.Errorf("the collapsed column's actions declare order: %v, want -1, which puts the "+
			"toggle at the top of the strip", got)
	}

	title := soleCSSRule(t, sheet, ".task-board__column--collapsed .card-title")
	if got := cssDeclarations(title, "writing-mode"); len(got) != 1 || got[0] != "vertical-rl" {
		t.Errorf("the collapsed column's heading declares writing-mode: %v, want vertical-rl", got)
	}

	toggle := soleCSSRule(t, sheet, ".task-board__column .btn-action")
	for _, prop := range []string{"min-width", "min-height"} {
		got := cssDeclarations(toggle, prop)
		if len(got) != 1 || !strings.HasSuffix(got[0], "rem") ||
			parseCSSNumber(strings.TrimSuffix(got[0], "rem")) < 2 {
			t.Errorf("the column toggle declares %s: %v, want a touch-friendly length of at least "+
				"2rem", prop, got)
		}
	}

	focus := soleCSSRule(t, sheet, ".task-board__column .btn-action:focus-visible")
	outline := cssDeclarations(focus, "outline")
	if len(outline) != 1 || outline[0] == "0" || strings.Contains(outline[0], "none") {
		t.Errorf("the column toggle's :focus-visible declares outline: %v, want a visible outline "+
			"restoring the one the vendored .btn-action:focus rule removes", outline)
	}
}
