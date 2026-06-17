package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// vendoredAssets enumerates the vendored Tabler / Inter / D3 asset paths that
// MUST be present in the embedded static FS for the self-contained binary to
// render and operate the admin-shell and the knowledge-graph visualisation
// offline (SPEC/WEB.md § Embedded Asset Categories, § UI Framework,
// § Knowledge-Graph Visualisation Library; SPEC/BUILD.md § Vendored Web
// Assets). Paths are relative to the embedded "static" directory.
var vendoredAssets = []string{
	"static/vendor/tabler/tabler.min.css",
	"static/vendor/tabler/tabler.min.js",
	"static/vendor/tabler-icons/tabler-icons.min.css",
	"static/vendor/tabler-icons/fonts/tabler-icons.woff2",
	"static/vendor/inter/inter.css",
	"static/vendor/inter/files/inter-latin-wght-normal.woff2",
	"static/vendor/d3/d3.min.js",
	"static/vendor/d3/d3-sankey.min.js",
	"static/graph.js",
	"static/style.css",
	"static/favicon.svg",
}

// removedAssets enumerates static assets that MUST NOT be present in the
// embedded FS any more. Cytoscape.js was replaced by the vendored D3.js
// bundle; the old bundle must be gone so the binary carries no dead asset and
// no page can reference it (SPEC/WEB.md § Knowledge-Graph Visualisation
// Library).
var removedAssets = []string{
	"static/cytoscape.min.js",
}

// TestEmbeddedStaticFS_OmitsRemovedAssets proves the retired Cytoscape.js
// bundle is no longer embedded in the binary, so the graph page's switch to
// D3.js leaves nothing behind.
func TestEmbeddedStaticFS_OmitsRemovedAssets(t *testing.T) {
	for _, path := range removedAssets {
		if _, err := staticFS.ReadFile(path); err == nil {
			t.Errorf("embedded static FS still contains removed asset %q; it must be gone", path)
		}
	}
}

// TestEmbeddedStaticFS_ContainsVendoredAssets proves that `//go:embed static`
// recursively embeds the vendored asset tree into the binary: every Tabler,
// Tabler Icons, Inter, and D3 file the interface loads is present in the
// embedded FS, so the deliverable is fully self-contained (SPEC/WEB.md
// § Self-Contained Deliverable, Acceptance Criteria 18). This is the test the
// task brief requires to assert vendor/ embedding.
func TestEmbeddedStaticFS_ContainsVendoredAssets(t *testing.T) {
	for _, path := range vendoredAssets {
		data, err := staticFS.ReadFile(path)
		if err != nil {
			t.Errorf("embedded static FS missing %q: %v", path, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("embedded asset %q is empty", path)
		}
	}
}

// pagePaths returns the HTML page routes against a seeded roadmap so a test can
// assert a property across every page the interface serves: the index, the
// roadmap sprints landing page, the roadmap tasks page, and the graph page.
func pagePaths(name string) []string {
	return []string{
		"/",
		"/roadmaps/" + name,
		"/roadmaps/" + name + "/tasks",
		"/roadmaps/" + name + "/graph",
	}
}

// servePage drives one GET request through the mux and returns the 200 body,
// failing the test on a non-200 status.
func servePage(t *testing.T, mux *http.ServeMux, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200; body=%q", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestPages_DarkThemeAttribute asserts every served page carries
// data-bs-theme="dark" on the <html> element, so the interface renders in
// Tabler's dark theme with no toggle (SPEC/WEB.md Functional Requirement 12,
// Acceptance Criterion 23).
func TestPages_DarkThemeAttribute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	for _, path := range pagePaths(name) {
		body := servePage(t, mux, path)
		if !strings.Contains(body, `<html lang="en" data-bs-theme="dark">`) {
			t.Errorf("page %s missing dark-theme <html data-bs-theme=\"dark\">", path)
		}
	}
}

// TestPages_AdminShellMarkup asserts every page renders the Tabler admin-shell
// chrome: a vertical navigation sidebar that lists Roadmaps, the page-wrapper
// shell, a page header, and the read-only indicator. The off-canvas collapse
// is driven by Tabler's JS via the navbar-toggler + collapse markup, so its
// presence is the structural proof of the hamburger menu on small viewports
// (SPEC/WEB.md Acceptance Criteria 23/24).
func TestPages_AdminShellMarkup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	for _, path := range pagePaths(name) {
		body := servePage(t, mux, path)
		for _, marker := range []string{
			"navbar-vertical",   // Tabler vertical sidebar
			"page-wrapper",      // admin-shell content wrapper
			"page-header",       // per-page header
			"navbar-toggler",    // hamburger control (off-canvas collapse on small viewports)
			`id="sidebar-menu"`, // collapsible sidebar target the toggler controls
			">Roadmaps<",        // the always-present Roadmaps sidebar link
			">Read-only<",       // the read-only indicator in the top navbar
		} {
			if !strings.Contains(body, marker) {
				t.Errorf("page %s missing admin-shell marker %q", path, marker)
			}
		}
	}
}

// TestPages_RoadmapSidebarLinks asserts that, on a roadmap's pages, the sidebar
// surfaces that roadmap's name and links to its two distinct page endpoints —
// Sprints at /roadmaps/{name} (the landing page) and Tasks at
// /roadmaps/{name}/tasks — plus the Graph view, as required by the admin-shell
// layout (SPEC/WEB.md § UI Framework, Functional Requirement 16, Acceptance
// Criterion 16). The index page (no active roadmap) must NOT render those
// roadmap-scoped links.
func TestPages_RoadmapSidebarLinks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	// Roadmap pages: the roadmap-scoped sidebar links resolve to the real
	// endpoints, not to #tasks / #sprints anchors on a combined page.
	for _, path := range []string{"/roadmaps/" + name, "/roadmaps/" + name + "/tasks", "/roadmaps/" + name + "/graph"} {
		body := servePage(t, mux, path)
		for _, link := range []string{
			`href="/roadmaps/` + name + `"`,       // Sprints (landing)
			`href="/roadmaps/` + name + `/tasks"`, // Tasks
			`href="/roadmaps/` + name + `/graph"`, // Graph
		} {
			if !strings.Contains(body, link) {
				t.Errorf("page %s missing roadmap sidebar link %q", path, link)
			}
		}
		// The retired anchor-style links must be gone everywhere.
		for _, gone := range []string{"#tasks", "#sprints"} {
			if strings.Contains(body, gone) {
				t.Errorf("page %s still references retired sidebar anchor %q", path, gone)
			}
		}
		// In the sidebar, Sprints is listed before Tasks (SPEC order: Sprints,
		// Tasks, Graph).
		assertOrdered(t, path+" sidebar order", body, []string{
			">Sprints<", ">Tasks<", ">Graph<",
		})
	}

	// Index page: no active roadmap, so no roadmap-scoped Tasks/Sprints anchors.
	indexBody := servePage(t, mux, "/")
	if strings.Contains(indexBody, "#tasks") || strings.Contains(indexBody, "#sprints") {
		t.Errorf("index page must not render roadmap-scoped Tasks/Sprints sidebar links")
	}
}

// remoteOriginRe matches a stylesheet/script/font/image reference (href= or
// src= or url(...)) to an absolute or protocol-relative remote origin in the
// served HTML. The interface must reference ONLY same-origin /static/ assets;
// any match is a CDN/remote-origin leak (SPEC/WEB.md Acceptance Criterion 16).
var remoteOriginRe = regexp.MustCompile(`(?i)(href|src)\s*=\s*["'](https?:)?//`)

// TestPages_NoRemoteOrigin asserts every served page references no remote
// origin: no http(s):// or protocol-relative // asset URL, and none of the
// well-known CDN/font hosts (cdn., fonts.googleapis, fonts.gstatic, unpkg,
// jsdelivr, cdnjs). This is the offline / no-CDN guarantee — every asset is
// served from /static/ on the same server (SPEC/WEB.md Acceptance Criteria
// 16/22, Functional Requirement 10).
func TestPages_NoRemoteOrigin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	bannedSubstrings := []string{
		"cdn.",
		"fonts.googleapis",
		"fonts.gstatic",
		"unpkg",
		"jsdelivr",
		"cdnjs",
	}

	for _, path := range pagePaths(name) {
		body := servePage(t, mux, path)
		if loc := remoteOriginRe.FindString(body); loc != "" {
			t.Errorf("page %s references a remote-origin asset (%q); every asset must be served from /static/", path, loc)
		}
		low := strings.ToLower(body)
		for _, bad := range bannedSubstrings {
			if strings.Contains(low, bad) {
				t.Errorf("page %s references banned remote origin %q", path, bad)
			}
		}
	}
}

// TestPages_AssetChainOrderAndLocality asserts the <head> loads the asset
// chain in the required order — inter.css, tabler.min.css, tabler-icons CSS,
// then the reworked style.css — all from /static/..., and that the body loads
// tabler.min.js (the graph page additionally loads d3.min.js, then
// d3-sankey.min.js, then graph.js — d3 before its plugin before the viewer),
// matching the asset-wiring contract (SPEC/WEB.md § Frontend Rules, UI
// Framework, § Knowledge-Graph Visualisation Library; task asset-wiring
// requirement 4).
func TestPages_AssetChainOrderAndLocality(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	headChain := []string{
		"/static/vendor/inter/inter.css",
		"/static/vendor/tabler/tabler.min.css",
		"/static/vendor/tabler-icons/tabler-icons.min.css",
		"/static/style.css",
	}

	for _, path := range pagePaths(name) {
		body := servePage(t, mux, path)
		assertOrdered(t, path, body, headChain)
		if !strings.Contains(body, "/static/vendor/tabler/tabler.min.js") {
			t.Errorf("page %s does not load /static/vendor/tabler/tabler.min.js", path)
		}
	}

	// The graph page loads d3, then the d3-sankey plugin, then graph.js, after
	// tabler.min.js; the order matters because d3-sankey augments the global d3
	// and graph.js consumes both.
	graphBody := servePage(t, mux, "/roadmaps/"+name+"/graph")
	assertOrdered(t, "graph", graphBody, []string{
		"/static/vendor/tabler/tabler.min.js",
		"/static/vendor/d3/d3.min.js",
		"/static/vendor/d3/d3-sankey.min.js",
		"/static/graph.js",
	})
	// And cytoscape must no longer be referenced anywhere on the page.
	if strings.Contains(graphBody, "cytoscape") {
		t.Errorf("graph page still references cytoscape; the library was replaced by D3.js")
	}
}

// TestGraphPage_LayoutDropdown asserts the graph page renders the layout
// selector with the complete set of nine "Networks"-section D3 gallery layouts,
// in order, with Force-directed preselected as the default (SPEC/WEB.md
// § Roadmap Knowledge-Graph Page, Functional Requirement 7, Acceptance
// Criterion 10).
func TestGraphPage_LayoutDropdown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+name+"/graph")

	// The select control is present and wired to the viewer.
	if !strings.Contains(body, `id="layout-select"`) {
		t.Fatalf("graph page missing the layout dropdown (id=\"layout-select\")")
	}

	// All nine options appear in the required order: force, disjoint,
	// patents (default), arc, sankey, bundling, chord, chord-directed,
	// chord-dependency.
	options := []string{
		`<option value="force">Force-directed graph</option>`,
		`<option value="disjoint">Disjoint force-directed graph</option>`,
		`<option value="patents" selected>Mobile patent suits</option>`,
		`<option value="arc">Arc diagram</option>`,
		`<option value="sankey">Sankey diagram</option>`,
		`<option value="bundling">Hierarchical edge bundling</option>`,
		`<option value="chord">Chord diagram</option>`,
		`<option value="chord-directed">Directed chord diagram</option>`,
		`<option value="chord-dependency">Chord dependency diagram</option>`,
	}
	for _, opt := range options {
		if !strings.Contains(body, opt) {
			t.Errorf("graph page missing layout option %q", opt)
		}
	}
	assertOrdered(t, "layout-options", body, options)

	// The four new layouts contributed by this version are present by value, so
	// the viewer's dispatch has a matching dropdown entry for each.
	for _, newValue := range []string{"patents", "chord", "chord-directed", "chord-dependency"} {
		if !strings.Contains(body, `value="`+newValue+`"`) {
			t.Errorf("graph page missing new layout option value %q", newValue)
		}
	}

	// Mobile patent suits is the default: its option carries the selected
	// attribute and no OTHER layout option does. The page now also carries the
	// query bar's node-limit dropdown, whose default (limit 100) is likewise a
	// preselected <option ... selected> (SPEC/WEB.md § Graph Query Bar,
	// Acceptance Criterion 45), so a page-wide "selected>" count is no longer 1.
	// Scope the uniqueness check to the layout dropdown's own options instead.
	if !strings.Contains(body, `<option value="patents" selected>`) {
		t.Errorf("Mobile patent suits must be the preselected default layout")
	}
	layoutSelected := 0
	for _, opt := range options {
		if strings.Contains(opt, " selected>") && strings.Contains(body, opt) {
			layoutSelected++
		}
	}
	if layoutSelected != 1 {
		t.Errorf("exactly one layout option must be preselected (the Mobile patent suits default); got %d", layoutSelected)
	}
}

// TestGraphPage_LabelsSidebar asserts the graph page renders the labels-sidebar
// column inside the graph card, to the left of the canvas, with its two sections
// (Node labels first, then Edge types), the per-section list and empty-state
// containers the client populates, and the canvas region wrapper the sidebar
// docks beside. The inventory and counts are computed client-side from the
// already-fetched graph data, so the server renders only the empty containers;
// this is the server-side-testable surface of the feature (SPEC/WEB.md § Graph
// Labels Sidebar, Acceptance Criteria 43/44).
func TestGraphPage_LabelsSidebar(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+name+"/graph")

	// The sidebar container, its two section lists, and their empty-state
	// elements are present for the client script to populate.
	for _, marker := range []string{
		`id="labels-sidebar"`,
		`class="labels-sidebar"`,
		`id="node-labels"`,
		`id="node-labels-empty"`,
		`id="edge-types"`,
		`id="edge-types-empty"`,
		`>Node labels<`,
		`>Edge types<`,
		`class="graph-region"`, // the canvas region the sidebar docks beside
		// Per-section absolute total elements the client populates (rule 11,
		// Acceptance Criteria 43/51).
		`id="node-labels-total"`,
		`id="edge-types-total"`,
		// Collapse/expand control at the top of the sidebar (rule 12,
		// Acceptance Criterion 52), built with the page's Tabler icon font.
		`id="labels-toggle"`,
		`ti-layout-sidebar-left-collapse`,
		`aria-expanded="true"`, // default state on load is expanded
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("graph page missing labels-sidebar marker %q", marker)
		}
	}

	// Fixed section order: Node labels before Edge types, and the sidebar before
	// the graph canvas (it is the left column).
	assertOrdered(t, "labels-sidebar structure", body, []string{
		`id="labels-sidebar"`,
		`id="labels-toggle"`, // the collapse/expand control sits at the top
		`>Node labels<`,
		`id="node-labels-total"`, // the section total accompanies its header
		`id="node-labels"`,
		`>Edge types<`,
		`id="edge-types-total"`,
		`id="edge-types"`,
		`id="graph"`,
	})
}

// TestGraphAssets_LabelsSidebarLogic asserts the client assets carry the
// labels-sidebar implementation: graph.js derives the inventory and applies the
// highlight (it references the sidebar element ids, the inventory builder, and
// the highlight/dim entry points), and style.css styles the sidebar entries and
// the dimmed-element state. These are static-asset invariants; the runtime
// D3 behaviour is exercised by the browser, consistent with the existing
// server-side-only test approach for the graph page (SPEC/WEB.md § Graph Labels
// Sidebar, rule 4, rule 8, rule 10).
func TestGraphAssets_LabelsSidebarLogic(t *testing.T) {
	js, err := staticFS.ReadFile("static/graph.js")
	if err != nil {
		t.Fatalf("read embedded graph.js: %v", err)
	}
	jsText := string(js)
	for _, token := range []string{
		`getElementById("node-labels")`, // wires the node-label list
		`getElementById("edge-types")`,  // wires the edge-type list
		"buildInventory",                // client-side count derivation
		"renderSidebar",                 // populates the sidebar from the data
		"applyHighlight",                // dim/highlight entry point
		"is-dimmed",                     // the dimming class toggled on elements
		"is-active",                     // the selected-entry state
		// Section totals derived client-side (rule 11, Acceptance Criterion 51):
		// the node total is the distinct-node count and the edge total the edge
		// count, kept distinct from the per-label sums.
		`getElementById("node-labels-total")`,
		`getElementById("edge-types-total")`,
		"nodeTotal",          // the distinct-node total field
		"typeTotal",          // the edge total field
		"model.nodes.length", // node total = distinct fetched nodes, not the per-label sum
		"model.links.length", // edge total = fetched edges
		// Collapse/expand control logic (rule 12, Acceptance Criterion 52).
		`getElementById("labels-toggle")`,
		"setSidebarCollapsed",
		"is-collapsed",
		"ti-layout-sidebar-left-expand", // the icon swaps to the expand glyph when collapsed
	} {
		if !strings.Contains(jsText, token) {
			t.Errorf("graph.js missing labels-sidebar token %q", token)
		}
	}
	// The node total must be the distinct-node count, NOT the sum of per-label
	// counts. The per-label counts are accumulated into labelCounts; the node
	// total must not be derived from that map. Guard against a regression that
	// sums the per-label entries by requiring the total to come from the node
	// array length.
	if !strings.Contains(jsText, "nodeTotal: model.nodes.length") {
		t.Errorf("graph.js node total must be the distinct-node count (model.nodes.length), not the sum of per-label counts")
	}
	// The highlight must survive a layout change: render() re-applies the current
	// emphasis. Dimming is now unified through applyEmphasis(), which delegates to
	// applyHighlight() when no node is focused (SPEC/WEB.md § Graph Labels Sidebar,
	// rule 8; Acceptance Criterion 56), so the labels highlight is preserved across
	// a layout change through that single path.
	reApply := strings.Index(jsText, "function render(")
	applyAfter := strings.LastIndex(jsText, "applyEmphasis();")
	if reApply < 0 || applyAfter < reApply {
		t.Errorf("graph.js render() must call applyEmphasis() so a layout change preserves the selection/focus")
	}
	// Collapsing/expanding must not clear the highlight or run a search: the
	// toggle handler must not reset the active selection sets nor call runSearch.
	toggleStart := strings.Index(jsText, "function setSidebarCollapsed(")
	toggleEnd := strings.Index(jsText[toggleStart:], "\n  }\n")
	if toggleStart < 0 || toggleEnd < 0 {
		t.Fatalf("graph.js setSidebarCollapsed() body not found")
	}
	toggleBody := jsText[toggleStart : toggleStart+toggleEnd]
	for _, forbidden := range []string{"runSearch", "activeLabels = ", "activeTypes = ", "hidePanel", "showEmpty"} {
		if strings.Contains(toggleBody, forbidden) {
			t.Errorf("setSidebarCollapsed() must not %q: collapsing changes only sidebar visibility and canvas width, never the highlight, search, or detail panel", forbidden)
		}
	}

	css, err := staticFS.ReadFile("static/style.css")
	if err != nil {
		t.Fatalf("read embedded style.css: %v", err)
	}
	cssText := string(css)
	for _, token := range []string{
		".labels-sidebar",                 // the sidebar column
		".labels-sidebar__item",           // a touch-friendly entry
		".labels-sidebar__item.is-active", // the selected state
		".graph-svg .is-dimmed",           // the dimmed (reduced-opacity) state
		".graph-region",                   // the canvas region wrapper
		// Section-total badge in the header (rule 11).
		".labels-sidebar__total",
		// Collapse/expand control and collapsed-state rules (rule 12). The
		// collapsed sidebar hides its body and, on a wide viewport, contracts
		// the column so the canvas takes the full width.
		".labels-sidebar__toggle",
		".labels-sidebar.is-collapsed .labels-sidebar__body",
		".labels-sidebar.is-collapsed",
	} {
		if !strings.Contains(cssText, token) {
			t.Errorf("style.css missing labels-sidebar rule %q", token)
		}
	}
}

// TestGraphAssets_CtrlEnterAccelerator asserts graph.js wires the Ctrl+Enter
// keyboard accelerator on the query box and that it reuses the existing search
// trigger rather than duplicating the search logic (SPEC/WEB.md § Graph Query
// Bar, rule 5; Acceptance Criterion 53). The accelerator is a static-asset
// invariant; the runtime keydown behaviour is exercised by the browser and the
// E2E suite, consistent with the existing server-side-only test approach for
// graph.js.
func TestGraphAssets_CtrlEnterAccelerator(t *testing.T) {
	js, err := staticFS.ReadFile("static/graph.js")
	if err != nil {
		t.Fatalf("read embedded graph.js: %v", err)
	}
	jsText := string(js)

	for _, token := range []string{
		`queryInput.addEventListener("keydown"`, // the accelerator is wired on the query box
		"event.ctrlKey",                         // Ctrl+Enter is the accelerator chord
		`event.key === "Enter"`,                 // the Enter key gate
		"event.preventDefault()",                // stop the default newline insertion on the chord
	} {
		if !strings.Contains(jsText, token) {
			t.Errorf("graph.js missing Ctrl+Enter accelerator token %q", token)
		}
	}

	// The accelerator must reuse the existing Search action, not duplicate the
	// search logic. The Search button is a type="submit" control, so firing the
	// form's submit event (requestSubmit) runs the same submit handler that
	// drives runSearch(); a duplicated fetch in the keydown handler would be a
	// regression. Verify the handler body invokes the shared trigger.
	keydownStart := strings.Index(jsText, `queryInput.addEventListener("keydown"`)
	if keydownStart < 0 {
		t.Fatalf("graph.js keydown accelerator handler not found")
	}
	keydownEnd := strings.Index(jsText[keydownStart:], "\n    });")
	if keydownEnd < 0 {
		t.Fatalf("graph.js keydown accelerator handler body not delimited")
	}
	handlerBody := jsText[keydownStart : keydownStart+keydownEnd]
	if !strings.Contains(handlerBody, "requestSubmit") {
		t.Errorf("Ctrl+Enter handler must reuse the Search submit path via requestSubmit(), not duplicate the search logic")
	}
}

// TestGraphAssets_NeighborFocus asserts graph.js carries the neighbor-focus
// implementation: a single module-level focus state, an undirected first-degree
// neighbourhood computed client-side from the model's links (startId/endId
// mapped to source/target), a single unified emphasis function that gives focus
// precedence over the labels highlight, the consistent clear gestures (panel
// close, empty-canvas tap, re-select), and the layout/search coexistence (render
// reapplies the current emphasis; a search clears the focus). These are
// static-asset invariants; the runtime D3 behaviour is exercised by the browser
// and the E2E suite, consistent with the existing server-side-only test approach
// for graph.js (SPEC/WEB.md § Roadmap Knowledge-Graph Page, "Neighbor focus on
// node selection"; § Graph Labels Sidebar, rule 8; Acceptance Criteria 54-56).
func TestGraphAssets_NeighborFocus(t *testing.T) {
	js, err := staticFS.ReadFile("static/graph.js")
	if err != nil {
		t.Fatalf("read embedded graph.js: %v", err)
	}
	jsText := string(js)

	for _, token := range []string{
		// Single module-level focus state (one source of truth).
		"focusedNodeId",
		// Undirected first-degree neighbourhood computation and the unified
		// emphasis/clear/select functions.
		"function neighborSet(",
		"function applyEmphasis(",
		"function applyFocusDimming(",
		"function clearFocus(",
		"function onNodeSelected(",
		"function dismissSelection(",
		// Focus reuses the SAME dim-not-remove mechanism as the labels highlight.
		"is-dimmed",
		// Identity tags neighbor focus reads from the DOM.
		"data-node-id",
		"data-edge-source",
		"data-edge-target",
	} {
		if !strings.Contains(jsText, token) {
			t.Errorf("graph.js missing neighbor-focus token %q", token)
		}
	}

	// There must be exactly one focus state variable declaration: a single source
	// of truth, not competing state.
	if strings.Count(jsText, "var focusedNodeId") != 1 {
		t.Errorf("graph.js must declare the focus state once (single source of truth)")
	}

	// applyEmphasis must be the single dimming decision path: when a node is
	// focused it dims by neighbourhood, otherwise it delegates to applyHighlight()
	// (the labels state). Verify the precedence branch and the delegation.
	empStart := strings.Index(jsText, "function applyEmphasis(")
	if empStart < 0 {
		t.Fatalf("graph.js applyEmphasis() not found")
	}
	empBody := jsText[empStart:]
	if end := strings.Index(empBody, "\n  }\n"); end > 0 {
		empBody = empBody[:end]
	}
	if !strings.Contains(empBody, "focusedNodeId !== null") {
		t.Errorf("applyEmphasis() must branch on the focus state so neighbor focus takes precedence")
	}
	if !strings.Contains(empBody, "applyFocusDimming") || !strings.Contains(empBody, "applyHighlight()") {
		t.Errorf("applyEmphasis() must dim by neighbourhood when focused and delegate to applyHighlight() otherwise (one dimming path)")
	}

	// The neighbourhood must be UNDIRECTED: it includes a node whether the focused
	// node is the link's source OR its target. Verify both endpoint comparisons.
	nbStart := strings.Index(jsText, "function neighborSet(")
	if nbStart < 0 {
		t.Fatalf("graph.js neighborSet() not found")
	}
	nbBody := jsText[nbStart:]
	if end := strings.Index(nbBody, "\n  }\n"); end > 0 {
		nbBody = nbBody[:end]
	}
	if !strings.Contains(nbBody, "s === nodeId") || !strings.Contains(nbBody, "t === nodeId") {
		t.Errorf("neighborSet() must be undirected: include neighbours when the focused node is either the source OR the target of an edge")
	}
	// The neighbourhood must be derived from the model's links (startId/endId
	// mapped to source/target in buildModel), not from any server call.
	if !strings.Contains(nbBody, "graphModel.links") {
		t.Errorf("neighborSet() must compute the neighbourhood client-side from graphModel.links")
	}

	// render() must reapply the CURRENT emphasis (applyEmphasis), not just the
	// labels highlight, so a layout change preserves a neighbor focus too.
	renderStart := strings.Index(jsText, "function render(")
	applyEmphAfter := strings.LastIndex(jsText, "applyEmphasis();")
	if renderStart < 0 || applyEmphAfter < renderStart {
		t.Errorf("render() must call applyEmphasis() so a layout change preserves the current focus/highlight")
	}

	// The clear gestures must be unified: the panel close and the empty-canvas tap
	// both route through dismissSelection (panel close + focus clear together).
	if !strings.Contains(jsText, `panelClose.addEventListener("click", dismissSelection)`) {
		t.Errorf("closing the detail panel must clear the focus too (dismissSelection)")
	}
	// A search must clear the focus as part of rendering the new result.
	applyDataStart := strings.Index(jsText, "function applyData(")
	if applyDataStart < 0 {
		t.Fatalf("graph.js applyData() not found")
	}
	applyDataBody := jsText[applyDataStart:]
	if end := strings.Index(applyDataBody, "\n  }\n"); end > 0 {
		applyDataBody = applyDataBody[:end]
	}
	if !strings.Contains(applyDataBody, "focusedNodeId = null") {
		t.Errorf("applyData() (the search re-render path) must clear the neighbor focus")
	}
}

// assertOrdered fails if the substrings do not all appear in body in the given
// order (each found at or after the previous match's end).
func assertOrdered(t *testing.T, label, body string, want []string) {
	t.Helper()
	from := 0
	for _, sub := range want {
		idx := strings.Index(body[from:], sub)
		if idx < 0 {
			t.Errorf("%s: asset %q not found at/after offset %d (out of order or missing)", label, sub, from)
			return
		}
		from += idx + len(sub)
	}
}

// TestStatic_VendoredAssetsServed asserts the static handler serves the
// vendored CSS with the correct text/css content type and the Inter woff2
// font with 200, both from /static/... — proving the embedded vendor tree is
// reachable over HTTP and typed correctly (SPEC/WEB.md Acceptance Criteria
// 16/22; task test requirement: GET tabler.min.css 200 text/css, GET inter
// woff2 200).
func TestStatic_VendoredAssetsServed(t *testing.T) {
	mux := buildMux()

	cases := []struct {
		path            string
		wantCTSubstring string
	}{
		{"/static/vendor/tabler/tabler.min.css", "text/css"},
		{"/static/vendor/tabler/tabler.min.js", "javascript"},
		{"/static/vendor/tabler-icons/tabler-icons.min.css", "text/css"},
		{"/static/style.css", "text/css"},
		{"/static/vendor/d3/d3.min.js", "javascript"},
		{"/static/vendor/d3/d3-sankey.min.js", "javascript"},
		{"/static/graph.js", "javascript"},
		{"/static/vendor/inter/files/inter-latin-wght-normal.woff2", ""},
		{"/static/vendor/tabler-icons/fonts/tabler-icons.woff2", ""},
		{"/static/favicon.svg", "image/svg"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200", tc.path, rec.Code)
			continue
		}
		if tc.wantCTSubstring != "" {
			ct := rec.Header().Get("Content-Type")
			if !strings.Contains(ct, tc.wantCTSubstring) {
				t.Errorf("GET %s: content-type = %q, want substring %q", tc.path, ct, tc.wantCTSubstring)
			}
		}
		if rec.Body.Len() == 0 {
			t.Errorf("GET %s: empty body", tc.path)
		}
	}
}

// TestStatic_NoDirectoryListing asserts the noDirFS wrapper suppresses
// browseable directory listings: requesting a static directory (with or
// without a trailing slash) is a 404, while an individual file inside it still
// serves 200 (SPEC/WEB.md § Static Assets, finding #70).
func TestStatic_NoDirectoryListing(t *testing.T) {
	mux := buildMux()

	// The trailing-slash directory forms are what http.FileServer would
	// otherwise render as a browseable listing; noDirFS turns them into 404s.
	// (The no-slash forms are a 307 redirect to the slash form from the mux,
	// which is normal routing, not a listing, so they are not probed here.)
	dirs := []string{"/static/", "/static/vendor/"}
	for _, path := range dirs {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404 (no directory listing)", path, rec.Code)
		}
	}

	// Individual files inside those directories still resolve.
	files := []string{"/static/graph.js", "/static/vendor/d3/d3.min.js"}
	for _, path := range files {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200", path, rec.Code)
		}
	}
}

// TestSecurityHeaders asserts the outermost security-header middleware sets the
// four hardening headers on every response — including the 404 produced by the
// fallback handler and the suppressed static directory listing (SPEC/WEB.md
// § Security Headers, finding #71).
func TestSecurityHeaders(t *testing.T) {
	h := handler()

	wantHeaders := map[string]string{
		"Content-Security-Policy": contentSecurityPolicy,
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "same-origin",
	}
	// The CSP literal must match the SPEC table exactly.
	const wantCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'"
	if contentSecurityPolicy != wantCSP {
		t.Fatalf("contentSecurityPolicy = %q, want %q", contentSecurityPolicy, wantCSP)
	}

	// Cover a found path (graph file), a 404 (unknown path), a suppressed
	// directory listing, and a 405 (non-read method on a known path).
	type probe struct {
		method, path string
	}
	probes := []probe{
		{http.MethodGet, "/static/graph.js"},
		{http.MethodGet, "/no/such/page"},
		{http.MethodGet, "/static/"},
		{http.MethodPost, "/"},
	}
	for _, p := range probes {
		req := httptest.NewRequest(p.method, p.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		for k, want := range wantHeaders {
			if got := rec.Header().Get(k); got != want {
				t.Errorf("%s %s: header %s = %q, want %q", p.method, p.path, k, got, want)
			}
		}
	}
}

// TestServerTimeouts asserts the HTTP server is configured with all three
// mandatory timeouts (SPEC/WEB.md § HTTP Server Timeouts, finding #69), which
// has no dynamic HTTP probe and is verified here by inspecting the struct the
// server builds.
func TestServerTimeouts(t *testing.T) {
	srv := newServer()
	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 10s", srv.ReadHeaderTimeout)
	}
	if srv.WriteTimeout != 30*time.Second {
		t.Errorf("WriteTimeout = %v, want 30s", srv.WriteTimeout)
	}
	if srv.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s", srv.IdleTimeout)
	}
}

// TestTasksPage_PreservesAllTaskFields asserts the dedicated tasks page
// presents the full 15-column Tasks table (ID, Title, Status, Type, Priority,
// Severity, Specialists, Parent, Subtasks, Depends on, Blocks, Created,
// Started, Tested, Closed) inside a responsive table, with status as a Tabler
// badge. This is the same Tasks table the combined detail page used to carry,
// now at its own endpoint (SPEC/WEB.md § Roadmap Tasks Page, Acceptance
// Criterion 9).
func TestTasksPage_PreservesAllTaskFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+name+"/tasks")

	// The seeded task is in a responsive table inside a card.
	if !strings.Contains(body, "table-responsive") {
		t.Errorf("tasks page task table is not in a .table-responsive wrapper")
	}
	for _, header := range []string{
		"ID", "Title", "Status", "Type", "Priority", "Severity", "Specialists",
		"Parent", "Subtasks", "Depends on", "Blocks", "Created", "Started",
		"Tested", "Closed",
	} {
		if !strings.Contains(body, "<th>"+header+"</th>") {
			t.Errorf("tasks table missing column header %q", header)
		}
	}

	// Status as a Tabler badge.
	if !strings.Contains(body, `<span class="badge bg-blue-lt">`) {
		t.Errorf("tasks page status not rendered as a Tabler badge")
	}

	// The full task table is the tasks page's content; it carries no sprint
	// tabs (those live on the sprints landing page).
	if strings.Contains(body, `id="tab-current"`) {
		t.Errorf("tasks page must NOT render the sprint tabs")
	}
}

// TestSprintsPage_PreservesSprintCards asserts the sprints landing page
// surfaces the seeded sprint as a clickable card linking to its own page, with
// its description, and that it does NOT render the full tasks table (that moved
// to /roadmaps/{name}/tasks). The page groups sprints into three tabs
// (SPEC/WEB.md § Roadmap Sprints Page, Acceptance Criterion 8).
func TestSprintsPage_PreservesSprintCards(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+name)

	// The seeded sprint (PENDING) appears as a clickable card linking to its
	// own page, with its description.
	if !strings.Contains(body, "Sprint #") {
		t.Errorf("sprints page missing sprint card heading")
	}
	if !strings.Contains(body, "/roadmaps/"+name+"/sprints/") {
		t.Errorf("sprints page sprint card is not a link to the sprint page")
	}
	if !strings.Contains(body, "Ship the read-only web UI for roadmap inspection") {
		t.Errorf("sprints page sprint card missing the seeded sprint description")
	}

	// The full 15-column task table must NOT be on the sprints page. The
	// "<th>Type</th>" header is unique to the full table (the Actual tab's
	// in-sprint mini-table has only ID/Title/Status), so its absence proves the
	// full table is not rendered here.
	if strings.Contains(body, "<th>Type</th>") {
		t.Errorf("sprints page must NOT render the full tasks table (found a Type column header)")
	}
}
