package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// This file is the gate for the column collapse of the Roadmap Sprint Page's
// member-tasks board, as far as it is observable without a browser: the markup
// the server renders for the toggles and the column bodies, the script that
// carries the behaviour and the page that loads it, and the stylesheet rules the
// collapsed strip is declared by (SPEC/WEB.md § Sprint Detail Sub-Template, rule
// 3, Column collapse; Acceptance Criteria 138 and 212 to 218).
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
// It is run over three sprints: one whose three columns all hold cards, one
// whose DOING and CLOSED columns are empty, and one with no member task at all,
// so the toggle and its body are pinned around an empty state as well as around
// a card list, and the served toggle is shown to be the same whatever the sprint
// holds.
func TestSprintBoardCollapse_EachHeaderCarriesOneServedToggle(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	mux := buildMux()

	full := seedSprintBoardFixture(t, "settlement-platform")
	sparseID := seedSprintWithMembers(t, "settlement-window", 2)
	emptyID := seedSprintWithMembers(t, "chargeback-intake", 0)

	for _, path := range []string{
		full.path(),
		"/roadmaps/settlement-window/sprints/" + itoa(sparseID),
		"/roadmaps/chargeback-intake/sprints/" + itoa(emptyID),
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

// reServedColumn matches the opening tag of a column element exactly as the
// server renders it on the sprint board: the expanded column, carrying no
// modifier, and its task count (Acceptance Criterion 212).
var reServedColumn = regexp.MustCompile(`<div class="card task-board__column" data-role="task-board-column" data-task-count="\d+">`)

// TestSprintBoardCollapse_ServedMarkupIsExpandedAndNothingIsPersisted is the
// gate for the server-rendered half of Acceptance Criterion 212: the served board
// renders all three columns expanded whatever the sprint holds — no column
// carries the collapsed modifier and no body carries the hidden attribute, even
// around an empty column, which the script alone collapses — the response sets no
// cookie, and the route reads no query parameter for the state, so a request
// carrying one renders the same board as a request carrying none.
func TestSprintBoardCollapse_ServedMarkupIsExpandedAndNothingIsPersisted(t *testing.T) {
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

	// The served state is the same for every sprint: the full fixture, a sprint
	// whose DOING and CLOSED columns are empty, and a sprint with no member task.
	// The two with empty columns are the ones a server applying the initial state
	// itself would render collapsed.
	sparseID := seedSprintWithMembers(t, "settlement-window", 2)
	emptyID := seedSprintWithMembers(t, "chargeback-intake", 0)
	for _, path := range []string{
		"/roadmaps/settlement-window/sprints/" + itoa(sparseID),
		"/roadmaps/chargeback-intake/sprints/" + itoa(emptyID),
		f.path(),
	} {
		served := serve(path).Body.String()
		board := memberBoardRegion(t, served)
		if strings.Contains(served, "task-board__column--collapsed") {
			t.Errorf("%s: the served page carries the collapsed modifier; the served HTML renders "+
				"all three columns expanded whatever the sprint holds", path)
		}
		if got := len(reServedColumn.FindAllString(board, -1)); got != 3 {
			t.Errorf("%s: the board carries %d column elements served as %s, want all 3",
				path, got, reServedColumn)
		}
		for _, want := range sprintBoardToggleColumns {
			open := `<div class="card-body task-board__cards" id="` + want.bodyID + `">`
			if !strings.Contains(board, open) {
				t.Errorf("%s: the %s column's body is not served as %s; a body served with the "+
					"hidden attribute, or with any other attribute, is not the expanded state",
					path, want.heading, open)
			}
		}
	}

	rec := serve(f.path())
	page := rec.Body.String()
	region := memberBoardRegion(t, page)
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
// toggle, or carries the collapsed modifier (Acceptance Criterion 218).
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

	// Every other page: the tasks page, which carries no board (Acceptance
	// Criterion 218), and the remaining pages of the interface.
	for _, path := range pagePaths(f.name) {
		body := get(path).Body.String()
		for _, forbidden := range []string{
			"sprint-board.js", `data-role="task-board-column-toggle"`, "task-board__column--collapsed",
			"data-task-count",
		} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s carries %q; the column collapse belongs to the sprint page's board alone",
					path, forbidden)
			}
		}
	}
	// Falsifiability control: the tasks page does render its tasks, so the absence
	// of a toggle above is asserted over a populated page and not over an empty one.
	if !strings.Contains(get("/roadmaps/"+f.name+"/tasks").Body.String(), `<table class="table table-vcenter card-table">`) {
		t.Fatalf("the tasks page renders no task list, so asserting it carries no toggle is vacuous")
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

// reColumnTaskCount captures, from one column's markup as memberBoardColumns
// returns it (the text after the column's data-role hook), the column's
// data-task-count and the text of its count badge.
var reColumnTaskCount = regexp.MustCompile(`^ data-task-count="([^"]*)">\s*` +
	`(?:\{\{[^}]*\}\}\s*)?<div class="card-header">\s*<h3 class="card-title">[A-Z]+ ` +
	`<span class="badge [^"]+">(\d+)</span></h3>`)

// TestSprintBoardCollapse_EachColumnCarriesItsTaskCount is the gate for the
// markup half of the initial state (Acceptance Criterion 212; SPEC/WEB.md
// § Sprint Detail Sub-Template, rule 3, Column collapse, Task count on the
// column): each column element of the sprint board carries data-task-count, in
// ASCII decimal digits with no leading zero, equal to the text of its own count
// badge, and the three values sum to the sprint's number of member tasks. It is
// run over a sprint with every column populated, one with two empty columns, and
// one with no member task, because the zero is the value the script keys on.
// The tasks page's five-column board carries no data-task-count; that half is
// asserted by TestSprintBoardCollapse_ScriptIsServedAndLoadedBySprintPageAlone.
func TestSprintBoardCollapse_EachColumnCarriesItsTaskCount(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	mux := buildMux()

	full := seedSprintBoardFixture(t, "settlement-platform")
	sparseID := seedSprintWithMembers(t, "settlement-window", 2)
	emptyID := seedSprintWithMembers(t, "chargeback-intake", 0)

	for _, tc := range []struct {
		path string
		want []string
	}{
		{full.path(), []string{"3", "2", "1"}},
		{"/roadmaps/settlement-window/sprints/" + itoa(sparseID), []string{"2", "0", "0"}},
		{"/roadmaps/chargeback-intake/sprints/" + itoa(emptyID), []string{"0", "0", "0"}},
	} {
		columns := memberBoardColumns(t, servePage(t, mux, tc.path))
		total, wantTotal := 0, 0
		for i, column := range columns {
			heading := sprintBoardToggleColumns[i].heading
			m := reColumnTaskCount.FindStringSubmatch(column)
			if m == nil {
				t.Errorf("%s: the %s column does not open with data-task-count followed by its "+
					"header and count badge:\n%.400s", tc.path, heading, column)
				continue
			}
			count, badge := m[1], m[2]
			if !regexp.MustCompile(`^(?:0|[1-9][0-9]*)$`).MatchString(count) {
				t.Errorf("%s: the %s column's data-task-count is %q, want ASCII decimal digits "+
					"with no sign and no leading zero", tc.path, heading, count)
			}
			if count != badge {
				t.Errorf("%s: the %s column's data-task-count is %q and its badge reads %q; "+
					"the two are the same number", tc.path, heading, count, badge)
			}
			if count != tc.want[i] {
				t.Errorf("%s: the %s column's data-task-count is %q, want %q",
					tc.path, heading, count, tc.want[i])
			}
			n, _ := strconv.Atoi(count) // validated above; a failure reads as 0 and fails the sum
			total += n
			w, _ := strconv.Atoi(tc.want[i]) // a literal of this table
			wantTotal += w
		}
		if total != wantTotal {
			t.Errorf("%s: the columns' data-task-count values sum to %d, want the sprint's %d "+
				"member tasks", tc.path, total, wantTotal)
		}
		if got := strings.Count(memberBoardRegion(t, servePage(t, mux, tc.path)), "data-task-count="); got != 3 {
			t.Errorf("%s: the board carries data-task-count %d times, want once per column", tc.path, got)
		}
	}
}

// TestSprintBoardCollapse_ScriptAppliesTheInitialStateFromTheCounts is the gate
// for the script half of the initial state (Acceptance Criterion 212; SPEC/WEB.md
// § Sprint Detail Sub-Template, rule 3, Column collapse, Initialisation), read at
// the script's source: the script un-hides the toggles, THEN reads each column's
// data-task-count through a pattern that admits ASCII decimal digits alone, THEN
// — only when the counts sum above zero — collapses each column whose count is 0
// through the one state change the click handler also uses. That the change is
// shared, not duplicated, is what makes a column that starts collapsed be in
// exactly the state a click produces. Initialisation moves no focus.
func TestSprintBoardCollapse_ScriptAppliesTheInitialStateFromTheCounts(t *testing.T) {
	script := stripJSComments(readEmbeddedAsset(t, "static/sprint-board.js"))

	// The steps, in the specified order.
	steps := []string{
		`bind(toggles[i])`,
		`getAttribute("data-task-count")`,
		`if (total > 0) {`,
		`counts[e] === 0`,
		`setCollapsed(empty, true);`,
	}
	at := -1
	for _, step := range steps {
		i := strings.Index(script, step)
		if i < 0 {
			t.Errorf("static/sprint-board.js does not contain %s", step)
			continue
		}
		if i < at {
			t.Errorf("static/sprint-board.js performs %s out of the specified order", step)
		}
		at = i
	}

	// A count is read only when it is one or more ASCII decimal digits; anything
	// else leaves its column expanded.
	if !strings.Contains(script, `var COUNT = /^[0-9]+$/;`) || !strings.Contains(script, `COUNT.test(value)`) {
		t.Errorf("static/sprint-board.js does not read data-task-count through the ASCII-digit " +
			"pattern /^[0-9]+$/")
	}

	// One state change, shared by the click handler and the initial state: the
	// column's state is written in exactly one place.
	if !strings.Contains(script, `setCollapsed(bound, toggle.getAttribute("aria-expanded") === "true");`) {
		t.Errorf("static/sprint-board.js's click handler does not go through setCollapsed")
	}
	for _, write := range []string{`body.hidden =`, `classList.toggle(COLLAPSED`, `"aria-expanded", collapse`} {
		if got := strings.Count(script, write); got != 1 {
			t.Errorf("static/sprint-board.js writes %q %d times, want exactly once: the click "+
				"handler and the initial state share one state change", write, got)
		}
	}

	// Initialisation moves no keyboard focus.
	for _, banned := range []string{".focus(", ".blur(", "autofocus"} {
		if strings.Contains(script, banned) {
			t.Errorf("static/sprint-board.js uses %q; initialisation moves no keyboard focus", banned)
		}
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
			"for the toggle's keyboard focus", outline)
	}
}
