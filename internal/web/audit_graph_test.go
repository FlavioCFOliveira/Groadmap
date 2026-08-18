package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// auditGraphScript is the client script that draws the history tree, read from
// the embedded asset set so the assertions gate exactly what the binary ships,
// with its comments stripped: the file's own header names the sinks it avoids
// and the model it does not re-derive, and a prose mention must not read as a
// use. Both helpers are the ones the sibling script guards already use.
func auditGraphScript(t *testing.T) string {
	t.Helper()

	script := stripJSComments(readEmbeddedAsset(t, "static/audit-graph.js"))
	if strings.TrimSpace(script) == "" {
		t.Fatal("audit-graph.js is empty once comments are stripped; every assertion on it would be vacuous")
	}
	return script
}

// TestAuditPage_CarriesTheHistoryTreeAndItsModel asserts the served page holds
// what the tree is drawn from: the container, the vendored library, the drawing
// script, and one row per entry carrying that entry's path and shape
// (SPEC/WEB.md § Audit History Tree, rules 1 and 3).
func TestAuditPage_CarriesTheHistoryTreeAndItsModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)

	body := servePage(t, buildMux(), "/roadmaps/"+name+"/audit")

	for _, want := range []string{
		`<div class="audit-graph" data-role="audit-graph" data-main-path="roadmap"></div>`,
		`<script src="/static/vendor/gitgraph/gitgraph.umd.min.js"></script>`,
		`<script src="/static/audit-graph.js"></script>`,
		`data-role="audit-entry"`,
		`data-path=`,
		`data-path-label=`,
		`data-op=`,
		`data-entity=`,
		`data-entity-id=`,
		`data-at=`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the audit page does not carry %s", want)
		}
	}

	// Every rendered row carries the model, not just the first: a row without
	// it would silently vanish from the tree while still showing in the table.
	// The path is asserted on the ROW, matching data-role and data-path together:
	// the Path column's own badge also carries a data-path, so counting the bare
	// attribute would count two per row and prove nothing about the rows.
	rows := strings.Count(body, `data-role="audit-entry"`)
	rowsWithPath := strings.Count(body, `data-role="audit-entry" data-path="`)
	if rows == 0 {
		t.Fatal("the audit page rendered no entry row; the assertions above are vacuous")
	}
	if rows != rowsWithPath {
		t.Errorf("%d entry rows, but %d carry a path; every row must carry one", rows, rowsWithPath)
	}

	// The reader is told what the lanes mean, because a task's lane is its
	// CURRENT sprint (SPEC/WEB.md § Audit History Tree, rule 7).
	if !strings.Contains(body, "A task's lane is the sprint it belongs to now") {
		t.Errorf("the page does not state that a task's lane is its current sprint")
	}
}

// TestAuditPage_BranchAndMergePointsReachTheClient asserts the three sprint
// operations that shape the tree arrive marked, and that no other row claims to
// branch or merge (SPEC/WEB.md § Audit History Paths, rule 3).
func TestAuditPage_BranchAndMergePointsReachTheClient(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	// One sprint's whole lifecycle, plus a task operation that must stay an
	// ordinary point.
	lifecycle := []struct {
		op models.AuditOperation
		at string
	}{
		{models.OpSprintCreate, "2026-02-01T10:00:00Z"},
		{models.OpSprintStart, "2026-02-01T11:00:00Z"},
		{models.OpSprintClose, "2026-02-01T12:00:00Z"},
		{models.OpSprintReopen, "2026-02-01T13:00:00Z"},
	}
	for _, l := range lifecycle {
		if err := database.WithTransaction(func(tx *sql.Tx) error {
			return db.LogAuditTx(tx, l.op, models.EntitySprint, 1, l.at)
		}); err != nil {
			t.Fatalf("seeding %s: %v", l.op, err)
		}
	}

	body := servePage(t, buildMux(), "/roadmaps/"+name+"/audit")

	rowOf := func(operation models.AuditOperation) string {
		t.Helper()
		for _, row := range strings.Split(body, "<tr ") {
			if strings.Contains(row, ">"+string(operation)+"<") {
				return row
			}
		}
		t.Fatalf("no row found for %s", operation)
		return ""
	}

	for _, c := range []struct {
		op     models.AuditOperation
		opens  bool
		merges bool
	}{
		{models.OpSprintCreate, true, false},
		{models.OpSprintReopen, true, false},
		{models.OpSprintClose, false, true},
		{models.OpSprintStart, false, false},
	} {
		row := rowOf(c.op)
		if got := strings.Contains(row, `data-opens="true"`); got != c.opens {
			t.Errorf("%s row: data-opens present = %v, want %v", c.op, got, c.opens)
		}
		if got := strings.Contains(row, `data-merges="true"`); got != c.merges {
			t.Errorf("%s row: data-merges present = %v, want %v", c.op, got, c.merges)
		}
	}
}

// TestAuditPage_EmptyLogDrawsNoTree asserts an empty audit log renders the
// existing empty state and no tree container: an empty drawing area would be a
// blank region promising something the page cannot show (SPEC/WEB.md § Audit
// History Tree, rule 8).
func TestAuditPage_EmptyLogDrawsNoTree(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// A roadmap with no audit entries at all: created directly, not seeded,
	// because seeding writes audit rows.
	const name = "empty-roadmap"
	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("creating roadmap: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("closing roadmap: %v", err)
	}

	body := servePage(t, buildMux(), "/roadmaps/"+name+"/audit")

	if !strings.Contains(body, "No audit entries yet") {
		t.Errorf("an empty audit log does not render its empty state")
	}
	for _, unwanted := range []string{`data-role="audit-graph"`, `id="audit-history"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("an empty audit log still renders %s; there is nothing to draw", unwanted)
		}
	}
}

// TestAuditGraphScript_DerivesNothingItWasGiven asserts the split of
// responsibilities the model depends on: the server decides which lane an entry
// belongs to and which entries branch or merge, and this script only draws.
//
// A script that re-derived either would be a second implementation of the
// model, free to disagree with the server's — and the server's is the one under
// test everywhere else.
func TestAuditGraphScript_DerivesNothingItWasGiven(t *testing.T) {
	source := auditGraphScript(t)

	// It reads the decisions...
	for _, want := range []string{"data-path", "data-opens", "data-merges", "data-path-label"} {
		if !strings.Contains(source, want) {
			t.Errorf("audit-graph.js never reads %q, so it cannot be drawing the server's model", want)
		}
	}
	// ...and never re-derives them from operation names.
	for _, forbidden := range []string{"SPRINT_CREATE", "SPRINT_CLOSE", "SPRINT_REOPEN", "sprint/"} {
		if strings.Contains(source, forbidden) {
			t.Errorf("audit-graph.js mentions %q: it is re-deriving the path model the server already decided", forbidden)
		}
	}
}

// TestAuditGraphScript_WritesEveryValueAsText mirrors the guard the task modal
// and the board search carry: values travel from server-rendered attributes
// into the drawing, and none of them may be written as markup.
func TestAuditGraphScript_WritesEveryValueAsText(t *testing.T) {
	source := auditGraphScript(t)

	for _, forbidden := range []string{
		"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("audit-graph.js uses %s; every value must be written as text", forbidden)
		}
	}
}

// TestAuditGraphScript_TakesItsColoursFromTheTheme asserts the lane colours are
// read from the vendored Tabler palette at runtime rather than hard-coded, so
// the tree follows the theme the rest of the page renders in (SPEC/WEB.md § UI
// Framework, rule 10).
func TestAuditGraphScript_TakesItsColoursFromTheTheme(t *testing.T) {
	source := auditGraphScript(t)

	if !strings.Contains(source, "getPropertyValue") {
		t.Errorf("audit-graph.js does not read any CSS custom property; its colours cannot follow the theme")
	}
	for _, property := range []string{"--tblr-blue", "--tblr-green", "--tblr-body-color"} {
		if !strings.Contains(source, property) {
			t.Errorf("audit-graph.js does not use the Tabler custom property %s", property)
		}
	}
}

// TestAuditPage_AddsNoInlineScriptAndKeepsThePolicy asserts the tree changed
// neither the Content-Security-Policy nor the rule that no page carries an
// inline script or a style attribute (SPEC/WEB.md § Audit History Tree, rule 6).
func TestAuditPage_AddsNoInlineScriptAndKeepsThePolicy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)

	// handler(), not buildMux(): the security headers are added by the wrapper
	// around the mux, so a bare mux would report no policy at all.
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/audit", nil)
	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// Every <script> on the page is a src reference, none carries a body.
	for _, fragment := range strings.Split(body, "<script")[1:] {
		tag := fragment[:strings.Index(fragment, ">")+1]
		if !strings.Contains(tag, "src=") {
			t.Errorf("the audit page carries an inline script: <script%s", tag)
		}
		open := strings.Index(fragment, ">")
		closeTag := strings.Index(fragment, "</script>")
		if open >= 0 && closeTag > open && strings.TrimSpace(fragment[open+1:closeTag]) != "" {
			t.Errorf("a <script%s element on the audit page has a body", tag)
		}
	}
	if strings.Contains(body, "style=\"") {
		t.Errorf("the audit page carries an inline style attribute")
	}

	if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Errorf("Content-Security-Policy = %q, want the unchanged policy %q", got, contentSecurityPolicy)
	}
}

// TestAuditPage_StatesTheOrderItActuallyShows is the regression guard for the
// second half of the defect measured in task #217: the history card's subtitle
// read "Oldest first" while the drawing showed the most recent entry at the
// top, because the library places the LAST point added at the top and the
// points are added oldest first.
//
// The tree and the table are two readings of the same page, so they must state
// the same direction, and it must be the one the page shows (SPEC/WEB.md
// § Audit History Tree, rule 2).
func TestAuditPage_StatesTheOrderItActuallyShows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)

	body := servePage(t, buildMux(), "/roadmaps/"+name+"/audit")

	// Both cards state the same order...
	if got := strings.Count(body, "Most recent first"); got != 2 {
		t.Errorf(`"Most recent first" appears %d times, want 2 (the history card and the entry table)`, got)
	}
	// ...and neither claims the opposite.
	if strings.Contains(body, "Oldest first") {
		t.Errorf(`the audit page still claims "Oldest first"; the tree draws the most recent entry at the top`)
	}
}

// TestAuditGraphScript_DrawsAtItsNaturalSize is the regression guard for the
// first half of task #217. The library's `responsive` option replaces the SVG's
// width and height with a viewBox AND forces `width:100%; padding-bottom:100%`
// onto the container as an inline style — a square as tall as the column is
// wide, with the drawing laid out at the viewBox's aspect ratio. Measured in
// chromium at a 1440px viewport, that rendered the tree 16811px tall.
//
// Without the option the drawing carries a real width and height, the container
// keeps the height cap the stylesheet gives it, and nothing writes an inline
// style — which the project forbids anyway.
func TestAuditGraphScript_DrawsAtItsNaturalSize(t *testing.T) {
	source := auditGraphScript(t)

	if strings.Contains(source, "responsive") {
		t.Error("audit-graph.js passes the responsive option: it forces padding-bottom:100% onto the container and scales the drawing to a square, which measured 16811px tall at a 1440px viewport")
	}

	// The density that makes a page of history readable: roughly one table row
	// per point rather than the 61.6px the first version drew.
	for _, want := range []string{"spacing: 30", "spacing: 24", "size: 4"} {
		if !strings.Contains(source, want) {
			t.Errorf("audit-graph.js no longer sets %q; the drawing's density is what keeps it readable", want)
		}
	}

	// The stylesheet caps the card so the drawing scrolls inside it instead of
	// pushing the table and the pagination down the page.
	css := readEmbeddedAsset(t, "static/style.css")
	if !strings.Contains(css, "max-height: 60vh") {
		t.Errorf("style.css no longer caps .audit-graph's height; a page of history would push the table thousands of pixels down")
	}
}
