package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
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

// pagePaths returns the three HTML page routes against a seeded roadmap so a
// test can assert a property across every page the interface serves.
func pagePaths(name string) []string {
	return []string{"/", "/roadmaps/" + name, "/roadmaps/" + name + "/graph"}
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
// also surfaces that roadmap's name and links to its Tasks/Sprints (anchors on
// the detail page) and Graph view, as required by the admin-shell layout
// (SPEC/WEB.md Functional Requirement 12, Acceptance Criterion 23). The index
// page (no active roadmap) must NOT render those roadmap-scoped links.
func TestPages_RoadmapSidebarLinks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	// Roadmap pages: the roadmap-scoped sidebar links are present.
	for _, path := range []string{"/roadmaps/" + name, "/roadmaps/" + name + "/graph"} {
		body := servePage(t, mux, path)
		for _, link := range []string{
			"/roadmaps/" + name + "#tasks",
			"/roadmaps/" + name + "#sprints",
			"/roadmaps/" + name + "/graph",
		} {
			if !strings.Contains(body, link) {
				t.Errorf("page %s missing roadmap sidebar link %q", path, link)
			}
		}
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
	// attribute and no other option does.
	if !strings.Contains(body, `<option value="patents" selected>`) {
		t.Errorf("Mobile patent suits must be the preselected default layout")
	}
	if strings.Count(body, "selected>") != 1 {
		t.Errorf("exactly one layout option must be preselected (the Mobile patent suits default)")
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

// TestDetail_PreservesAllTaskAndSprintFields asserts the reworked detail page
// still presents the full 15-column Tasks table (ID, Title, Status, Type,
// Priority, Severity, Specialists, Parent, Subtasks, Depends on, Blocks,
// Created, Started, Tested, Closed) with status as a Tabler badge, and that the
// seeded sprint is surfaced (as a clickable card linking to its page) with its
// description. The capacity/lifecycle datagrid moved to the dedicated sprint
// page; the detail page now groups sprints into three tabs (SPEC/WEB.md
// § Roadmap Detail Page, Acceptance Criterion 8).
func TestDetail_PreservesAllTaskAndSprintFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+name)

	// The seeded task is in a responsive table inside a card.
	if !strings.Contains(body, "table-responsive") {
		t.Errorf("detail page task table is not in a .table-responsive wrapper")
	}
	for _, header := range []string{
		"ID", "Title", "Status", "Type", "Priority", "Severity", "Specialists",
		"Parent", "Subtasks", "Depends on", "Blocks", "Created", "Started",
		"Tested", "Closed",
	} {
		if !strings.Contains(body, "<th>"+header+"</th>") {
			t.Errorf("detail task table missing column header %q", header)
		}
	}

	// Status as a Tabler badge.
	if !strings.Contains(body, `<span class="badge bg-blue-lt">`) {
		t.Errorf("detail page status not rendered as a Tabler badge")
	}

	// The seeded sprint (PENDING) appears as a clickable card linking to its
	// own page, with its description.
	if !strings.Contains(body, "Sprint #") {
		t.Errorf("detail page missing sprint card heading")
	}
	if !strings.Contains(body, "/roadmaps/"+name+"/sprints/") {
		t.Errorf("detail page sprint card is not a link to the sprint page")
	}
	if !strings.Contains(body, "Ship the read-only web UI for roadmap inspection") {
		t.Errorf("detail sprint card missing the seeded sprint description")
	}
}
