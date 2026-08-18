package web

import (
	"io/fs"
	"strings"
	"testing"
)

// TestTablerFidelity_PageHeaderGutter is the regression guard for SPEC/WEB.md
// § UI Framework rule 11 and Acceptance Criterion 63: every page-header row
// uses Tabler's `row g-2 align-items-center` gutter and alignment classes, as
// the Tabler page-header example does. The pre-rule markup used the gutter-less
// `row align-items-center`; this test fails if any page regresses to it.
//
// The test renders every page route that carries a page-header row — the
// roadmap index, the roadmap sprints landing page, the roadmap tasks page, a
// roadmap sprint detail page, the roadmap audit log page, and the
// knowledge-graph page — and asserts the rendered HTML contains the gutter
// class on the page-header row and no longer contains the gutter-less variant.
func TestTablerFidelity_PageHeaderGutter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// seedRoadmap creates sprint 1 (for the /sprints/1 detail page);
	// seedRoadmapWithAudit adds audit entries to the same roadmap (for /audit).
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)
	mux := buildMux()

	paths := []string{
		"/",
		"/roadmaps/" + name,
		"/roadmaps/" + name + "/tasks",
		"/roadmaps/" + name + "/sprints/1",
		"/roadmaps/" + name + "/audit",
		"/roadmaps/" + name + "/graph",
	}
	for _, path := range paths {
		body := servePage(t, mux, path)
		if !strings.Contains(body, `<div class="row g-2 align-items-center">`) {
			t.Errorf("page %s: missing Tabler page-header gutter row "+
				`<div class="row g-2 align-items-center">`, path)
		}
		if strings.Contains(body, `<div class="row align-items-center">`) {
			t.Errorf("page %s: page-header row regressed to the gutter-less "+
				`<div class="row align-items-center">`, path)
		}
	}
}

// TestTablerFidelity_SidebarBrandHeading is the regression guard for
// SPEC/WEB.md § UI Framework rule 11 and Acceptance Criterion 63: the sidebar
// brand uses the Tabler `<h1 class="navbar-brand navbar-brand-autodark">`
// element, as the Tabler vertical-navbar example does. The pre-rule markup
// wrapped the brand in a `<div class="navbar-brand navbar-brand-autodark">`;
// this test fails if it regresses to a non-h1 wrapper.
//
// The shared admin-shell layout renders the sidebar on every page, so asserting
// the property on any rendered page covers the brand markup. The brand link,
// favicon image, and "Groadmap" text must remain present and unchanged.
func TestTablerFidelity_SidebarBrandHeading(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	for _, path := range pagePaths(name) {
		body := servePage(t, mux, path)
		if !strings.Contains(body, `<h1 class="navbar-brand navbar-brand-autodark">`) {
			t.Errorf("page %s: sidebar brand is not the Tabler "+
				`<h1 class="navbar-brand navbar-brand-autodark"> element`, path)
		}
		if strings.Contains(body, `<div class="navbar-brand navbar-brand-autodark">`) {
			t.Errorf("page %s: sidebar brand regressed to a "+
				`<div class="navbar-brand navbar-brand-autodark"> wrapper`, path)
		}
		// The brand content must survive the element change unchanged.
		if !strings.Contains(body, `<span class="fw-bold">Groadmap</span>`) {
			t.Errorf("page %s: sidebar brand lost its \"Groadmap\" text", path)
		}
	}
}

// TestShell_CarriesNoFooterAnywhere is the regression guard that replaces the
// one which asserted the footer's presence and structure. That footer — a full
// band whose entire content was the sentence "Read-only. The rmp CLI remains the
// sole write path." — was removed from every template, so its old guard has no
// subject left. Weakening it into something that still passes would have left
// apparent coverage over nothing; the guard is therefore inverted, and now fails
// if a footer or the notice comes back.
//
// The sweep is over EVERY page route, the knowledge-graph page included: that
// page never carried a footer, and the inverse guard is what keeps the shell
// uniform in both directions.
//
// Both sides are checked. The served pages are checked because that is what a
// user receives, and the embedded template sources are checked because a footer
// added to a template that some future route stops rendering would otherwise slip
// past a behavioural sweep alone.
func TestShell_CarriesNoFooterAnywhere(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// seedRoadmap creates sprint 1 (for the /sprints/1 detail page);
	// seedRoadmapWithAudit adds audit entries to the same roadmap (for /audit).
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)
	mux := buildMux()

	const notice = "Read-only. The rmp CLI remains the sole write path."

	for _, path := range allPagePaths(name) {
		body := servePage(t, mux, path)

		// Falsifiability control: a body that came back empty would satisfy every
		// absence below without proving anything. Every page renders the shell.
		if !strings.Contains(body, `<div class="page">`) {
			t.Fatalf("page %s: no admin shell in the response, so the assertions below "+
				"would be vacuous", path)
		}

		// The element, not merely its text: removing the sentence and leaving the
		// band would leave an empty strip at the foot of the page.
		if strings.Contains(body, "<footer") {
			t.Errorf("page %s: renders a <footer> element; the read-only footer was removed "+
				"from every page", path)
		}
		if strings.Contains(body, "</footer>") {
			t.Errorf("page %s: renders a closing </footer> tag", path)
		}
		if strings.Contains(body, notice) {
			t.Errorf("page %s: renders the read-only notice %q, which was removed with the "+
				"footer that carried it", path, notice)
		}
		// The Tabler footer classes go with it: a footer rebuilt under a different
		// element would still be the band that was removed.
		for _, class := range []string{"footer-transparent", `class="footer`} {
			if strings.Contains(body, class) {
				t.Errorf("page %s: renders %q, so the page footer is back under another element",
					path, class)
			}
		}
	}

	// The same, at the source: no template carries a footer element or the notice.
	// card-footer and modal-footer are different components — a Tabler card's own
	// footer and the modal's button row — so the check is for the ELEMENT.
	templates, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		t.Fatalf("listing the embedded templates: %v", err)
	}
	if len(templates) == 0 {
		t.Fatal("no embedded template found; the source sweep would be vacuous")
	}
	for _, name := range templates {
		content, err := templatesFS.ReadFile(name)
		if err != nil {
			t.Fatalf("reading the embedded template %s: %v", name, err)
		}
		source := string(content)
		if strings.Contains(source, "<footer") || strings.Contains(source, "</footer>") {
			t.Errorf("template %s carries a footer element", name)
		}
		if strings.Contains(source, notice) {
			t.Errorf("template %s carries the read-only notice %q", name, notice)
		}
	}
}

// The tests below are the regression guards for the admin-shell fidelity rules
// in SPEC/WEB.md § UI Framework. Each one was written against the Tabler v1.4.0
// sources that match the vendored distribution — the version banner in
// static/vendor/tabler/tabler.min.css and core/package.json on the Tabler
// repository both read 1.4.0 — and each asserts both the shape Tabler uses and
// the absence of the shape it replaced, so a divergence cannot come back
// unnoticed.

// allPagePaths is every route that renders the admin shell: the four of
// pagePaths plus the sprint detail page and the audit log page. The shell rules
// hold on all six, so the fidelity guards below sweep the complete set rather
// than the subset pagePaths covers. The caller must have seeded the roadmap with
// both seedRoadmap (which creates sprint 1) and seedRoadmapWithAudit.
func allPagePaths(name string) []string {
	return []string{
		"/",
		"/roadmaps/" + name,
		"/roadmaps/" + name + "/tasks",
		"/roadmaps/" + name + "/sprints/1",
		"/roadmaps/" + name + "/audit",
		"/roadmaps/" + name + "/graph",
	}
}

// topNavbarRegion returns the markup between the top navbar's opening <header>
// and its closing </header>, failing the test when the region is absent.
//
// Every assertion about what the navbar carries is made on this region rather
// than on the whole document. A document-wide check would be wrong in both
// directions: the roadmap index page legitimately writes "Read-only view of the
// roadmaps under ~/.roadmaps/" in its page header, and the roadmap name appears
// in the sidebar and the page header of every roadmap-scoped page, so neither
// its presence nor the absence of the word would prove anything about the
// navbar.
func topNavbarRegion(t *testing.T, path, body string) string {
	t.Helper()

	const open = `<header class="navbar navbar-expand-md d-print-none">`
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatalf("page %s: no top navbar in the response, so any assertion on it would be vacuous", path)
	}
	rest := body[start+len(open):]
	end := strings.Index(rest, "</header>")
	if end < 0 {
		t.Fatalf("page %s: the top navbar is never closed", path)
	}
	return rest[:end]
}

// TestShell_TopNavbarNamesTheSelectedRoadmap is the regression guard for
// SPEC/WEB.md § UI Framework rule 19 and Acceptance Criterion 108: the top
// navbar names the roadmap the current page belongs to, and carries no badge,
// label, or icon declaring the interface read-only.
//
// It replaces the guard that asserted the ">Read-only<" badge. That badge was
// the whole content of the navbar and restated a server guarantee — no route
// writes — that every page already shows by having no control that writes,
// while the region it occupied is the only one that identifies the page's
// subject at every viewport width: the sidebar's own per-roadmap label collapses
// behind the off-canvas menu on a small viewport.
//
// Two roadmaps are seeded, so the assertion proves the navbar names the roadmap
// the page BELONGS TO rather than any fixed string that happens to match the
// only roadmap in the fixture.
func TestShell_TopNavbarNamesTheSelectedRoadmap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// seedRoadmap creates sprint 1 (for the /sprints/1 detail page);
	// seedRoadmapWithAudit adds audit entries to the same roadmap (for /audit).
	platform := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, platform, 3)
	billing := seedRoadmap(t, "billing-migration")
	seedRoadmapWithAudit(t, billing, 2)
	mux := buildMux()

	for _, rdm := range []struct{ name, other string }{
		{platform, billing},
		{billing, platform},
	} {
		// allPagePaths[0] is "/", the roadmap index; it belongs to no roadmap
		// and is asserted separately below.
		for _, path := range allPagePaths(rdm.name)[1:] {
			nav := topNavbarRegion(t, path, servePage(t, mux, path))

			named := `<span class="h3 mb-0 text-truncate" data-role="active-roadmap">` + rdm.name + `</span>`
			if !strings.Contains(nav, named) {
				t.Errorf("page %s: top navbar does not name its roadmap as %s; navbar=%q", path, named, nav)
			}
			// The glyph that precedes the name, and the overflow-hidden that is
			// what lets text-truncate clip a long name instead of widening the
			// navbar: a flex item's automatic minimum size is its content.
			if !strings.Contains(nav, `<i class="ti ti-map me-2"></i>`) {
				t.Errorf("page %s: top navbar carries no roadmap glyph before the name; navbar=%q", path, nav)
			}
			if !strings.Contains(nav, `<div class="nav-item d-flex align-items-center overflow-hidden">`) {
				t.Errorf("page %s: the navbar's flex item lost overflow-hidden, so text-truncate cannot clip a long roadmap name; navbar=%q", path, nav)
			}
			if strings.Contains(nav, rdm.other) {
				t.Errorf("page %s: top navbar names the other roadmap %q; navbar=%q", path, rdm.other, nav)
			}
		}
	}

	// The roadmap index belongs to no roadmap, so the region is empty: no name,
	// no glyph, no placeholder standing in for the roadmap that is not selected.
	indexNav := topNavbarRegion(t, "/", servePage(t, mux, "/"))
	for _, unwanted := range []string{`data-role="active-roadmap"`, "ti-map", platform, billing, "<span", "<i "} {
		if strings.Contains(indexNav, unwanted) {
			t.Errorf("the roadmap index page's top navbar carries %q; it belongs to no roadmap and the region must render empty; navbar=%q", unwanted, indexNav)
		}
	}

	// No page's navbar restates the read-only guarantee, under the badge that
	// carried it or under any other element.
	for _, rdm := range []string{platform, billing} {
		for _, path := range allPagePaths(rdm) {
			nav := topNavbarRegion(t, path, servePage(t, mux, path))
			for _, gone := range []string{"Read-only", "read-only", "ti-lock", "badge"} {
				if strings.Contains(nav, gone) {
					t.Errorf("page %s: top navbar carries %q; the read-only indicator was replaced by the selected roadmap's name", path, gone)
				}
			}
		}
	}

	// The same at the source, on the one partial that defines the navbar. The
	// check is scoped to the `{{define "topnavbar"}}` body for two reasons: the
	// badge's lock glyph is not exclusive to it — ti-lock is also the board
	// card's "Blocks" indicator — and the words survive legitimately in the
	// template comment that records why the badge went away, which sits outside
	// the definition and, being a Go template comment, is never served.
	def := topNavbarDefinition(t)
	for _, gone := range []string{"ti-lock", "badge", "Read-only"} {
		if strings.Contains(def, gone) {
			t.Errorf("the topnavbar partial carries %q; the read-only indicator was replaced by the selected roadmap's name; definition=%q", gone, def)
		}
	}
	// Falsifiability control: an extraction that came back empty would satisfy
	// every absence above without proving anything.
	if !strings.Contains(def, "Chrome.Roadmap") {
		t.Fatalf("the extracted topnavbar partial does not read .Chrome.Roadmap, so the assertions above were made on the wrong text; definition=%q", def)
	}
}

// topNavbarDefinition returns the body of the `{{define "topnavbar"}}` partial
// in the embedded layout template: the one place the top navbar's markup is
// written, which every page renders through `{{template "topnavbar" .}}`.
//
// The body ends at the LAST `{{end}}` before the next `{{define ...}}`, because
// the partial contains a nested `{{with .Chrome.Roadmap}}` whose own `{{end}}`
// comes first.
func topNavbarDefinition(t *testing.T) string {
	t.Helper()

	content, err := templatesFS.ReadFile("templates/layout.html")
	if err != nil {
		t.Fatalf("reading the embedded layout template: %v", err)
	}
	source := string(content)

	const define = `{{define "topnavbar"}}`
	start := strings.Index(source, define)
	if start < 0 {
		t.Fatalf("the embedded layout template defines no %q partial", "topnavbar")
	}
	rest := source[start+len(define):]
	if next := strings.Index(rest, "{{define "); next >= 0 {
		rest = rest[:next]
	}
	end := strings.LastIndex(rest, "{{end}}")
	if end < 0 {
		t.Fatalf("the topnavbar partial is never closed")
	}
	return rest[:end]
}

// TestTablerFidelity_TopNavbarIsSiblingOfPageWrapper asserts the admin-shell
// element order: inside div.page, the top header.navbar is a SIBLING of
// div.page-wrapper and precedes it, rather than being nested inside it.
//
// This is the order Tabler ships in both its documented "Sample layout"
// (docs/content/ui/layout/page-layouts.mdx) and its built shell
// (shared/layouts/DefaultLayout.astro), and the vendored stylesheet is written
// for it: `.navbar-expand-lg.navbar-vertical~.navbar` and the matching
// `~.page-wrapper` rule give the header and the wrapper the same 15rem offset
// through the sibling combinator, which never applies to a nested header.
func TestTablerFidelity_TopNavbarIsSiblingOfPageWrapper(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)
	mux := buildMux()

	for _, path := range allPagePaths(name) {
		body := servePage(t, mux, path)

		header := strings.Index(body, `<header class="navbar navbar-expand-md d-print-none">`)
		closing := strings.Index(body, "</header>")
		wrapper := strings.Index(body, `<div class="page-wrapper">`)
		if header < 0 || closing < 0 || wrapper < 0 {
			t.Errorf("page %s: admin shell incomplete (header=%d, /header=%d, page-wrapper=%d)",
				path, header, closing, wrapper)
			continue
		}
		// The whole header element closes before the wrapper opens, which is
		// only possible when the two are siblings and the header comes first.
		if closing > wrapper {
			t.Errorf("page %s: the top navbar is nested inside .page-wrapper; Tabler places it "+
				"as a sibling of the wrapper, directly inside .page", path)
		}
	}
}

// TestTablerFidelity_SingleTogglerAndSingleBrand asserts each page renders
// exactly one navbar-toggler and exactly one navbar-brand, and that the toggler
// controls the sidebar collapse.
//
// The top navbar used to repeat both: a second toggler pointing at the same
// #sidebar-menu and a second `d-md-none` brand. Below the md breakpoint that
// painted two hamburger buttons driving one collapse and the word Groadmap
// twice. In Tabler's vertical layout the sidebar owns the only #sidebar-menu
// toggler (shared/components/navbar/Sidebar.astro) and the header's own toggler
// targets its own #navbar-menu (Navbar.astro), a menu this interface does not
// have.
func TestTablerFidelity_SingleTogglerAndSingleBrand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)
	mux := buildMux()

	for _, path := range allPagePaths(name) {
		body := servePage(t, mux, path)

		if got := strings.Count(body, `class="navbar-toggler"`); got != 1 {
			t.Errorf("page %s: %d navbar-toggler elements, want exactly 1 "+
				"(a second one would drive the same collapse)", path, got)
		}
		if got := strings.Count(body, `class="navbar-brand`); got != 1 {
			t.Errorf("page %s: %d navbar-brand elements, want exactly 1", path, got)
		}
		if !strings.Contains(body, `data-bs-target="#sidebar-menu"`) {
			t.Errorf("page %s: the single toggler does not control #sidebar-menu", path)
		}
		// The retired duplicate brand must not come back.
		if strings.Contains(body, `class="navbar-brand d-md-none"`) {
			t.Errorf("page %s: the duplicated top-navbar brand is back", path)
		}
	}
}

// TestTablerFidelity_SidebarCollapseIsNavLandmark asserts the sidebar collapse
// is the labelled <nav> element Tabler v1.4.0 renders
// (shared/components/navbar/Sidebar.astro: `<nav class="collapse
// navbar-collapse" id="sidebar-menu" aria-label="Sidebar">`), not a bare div.
func TestTablerFidelity_SidebarCollapseIsNavLandmark(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)
	mux := buildMux()

	for _, path := range allPagePaths(name) {
		body := servePage(t, mux, path)
		if !strings.Contains(body, `<nav class="collapse navbar-collapse" id="sidebar-menu" aria-label="Sidebar">`) {
			t.Errorf("page %s: the sidebar collapse is not Tabler's labelled <nav> landmark", path)
		}
		if strings.Contains(body, `<div class="collapse navbar-collapse" id="sidebar-menu">`) {
			t.Errorf("page %s: the sidebar collapse regressed to a bare <div>", path)
		}
	}
}

// TestTablerFidelity_ActiveSidebarLinkCarriesAriaCurrent asserts the active
// sidebar link is marked with aria-current="page" and that exactly one link per
// page carries it. Tabler sets the attribute alongside the `active` class on the
// list item (shared/components/navbar/NavbarMenu.astro); the class alone is a
// visual cue that assistive technology cannot read.
func TestTablerFidelity_ActiveSidebarLinkCarriesAriaCurrent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)
	mux := buildMux()

	// Each page's active sidebar view and the href that view links to.
	cases := map[string]string{
		"/":                            `href="/" aria-current="page"`,
		"/roadmaps/" + name:            `href="/roadmaps/` + name + `" aria-current="page"`,
		"/roadmaps/" + name + "/tasks": `href="/roadmaps/` + name + `/tasks" aria-current="page"`,
		"/roadmaps/" + name + "/audit": `href="/roadmaps/` + name + `/audit" aria-current="page"`,
		"/roadmaps/" + name + "/graph": `href="/roadmaps/` + name + `/graph" aria-current="page"`,
	}
	for path, want := range cases {
		body := servePage(t, mux, path)
		if !strings.Contains(body, want) {
			t.Errorf("page %s: active sidebar link is not marked with %q", path, want)
		}
		// Count inside the sidebar only. The audit page's pagination legitimately
		// marks its own current page with aria-current="page" as well, which is
		// Bootstrap's idiom for a pagination bar and a different set of links.
		if got := strings.Count(sidebarRegion(t, path, body), `aria-current="page"`); got != 1 {
			t.Errorf("page %s: %d sidebar links carry aria-current=\"page\", want exactly 1", path, got)
		}
	}
}

// sidebarRegion returns the markup between the sidebar collapse's opening and
// closing tags, so an assertion about navigation links cannot be satisfied — or
// broken — by a link elsewhere on the page.
func sidebarRegion(t *testing.T, path, body string) string {
	t.Helper()
	const open = `<nav class="collapse navbar-collapse" id="sidebar-menu" aria-label="Sidebar">`
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatalf("page %s: sidebar collapse not found", path)
	}
	rest := body[start+len(open):]
	end := strings.Index(rest, "</nav>")
	if end < 0 {
		t.Fatalf("page %s: sidebar collapse is not closed", path)
	}
	return rest[:end]
}

// TestTablerFidelity_PaginationIsANavLandmark asserts the audit log's pagination
// list sits inside a labelled <nav>, as Tabler's own pagination component emits
// it (shared/ui/Pagination.astro renders `<nav aria-label="Pagination"><ul
// class="pagination">`). A bare list is not announced as a navigation landmark
// and is indistinguishable from the sidebar's navigation.
func TestTablerFidelity_PaginationIsANavLandmark(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 250)
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+name+"/audit")
	nav := strings.Index(body, `<nav aria-label="Audit log pagination"`)
	list := strings.Index(body, `<ul class="pagination`)
	if nav < 0 {
		t.Fatalf("audit page: pagination is not wrapped in a labelled <nav> landmark")
	}
	if list < 0 || list < nav {
		t.Errorf("audit page: the pagination list is not inside the <nav> landmark "+
			"(nav at %d, ul at %d)", nav, list)
	}
	// The control stays read-only: the landmark introduces no form and no
	// non-GET affordance.
	if strings.Contains(body[nav:], "<form") || strings.Contains(body[nav:], "<button") {
		t.Errorf("audit page: the pagination landmark introduced a write affordance")
	}
}

// TestTablerFidelity_PageHeaderActionsAreHiddenInPrint asserts the page-header
// actions column carries d-print-none, as Tabler's PageHeader does
// (shared/components/layout/PageHeader.astro: `<div class="col-auto ms-auto
// d-print-none">`). Without it the actions print on a page header that is
// otherwise excluded from print by its own d-print-none.
func TestTablerFidelity_PageHeaderActionsAreHiddenInPrint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)
	mux := buildMux()

	// The roadmap index has no page-header actions; every roadmap page does.
	for _, path := range []string{
		"/roadmaps/" + name,
		"/roadmaps/" + name + "/tasks",
		"/roadmaps/" + name + "/sprints/1",
		"/roadmaps/" + name + "/audit",
		"/roadmaps/" + name + "/graph",
	} {
		body := servePage(t, mux, path)
		if !strings.Contains(body, `<div class="col-auto ms-auto d-print-none">`) {
			t.Errorf("page %s: the page-header actions column is missing Tabler's d-print-none", path)
		}
		if strings.Contains(body, `<div class="col-auto ms-auto">`) {
			t.Errorf("page %s: a page-header actions column regressed to the print-visible variant", path)
		}
	}
}

// TestTablerFidelity_FluidLayoutUsesContainerXl asserts the page containers use
// Tabler's fluid idiom: `<body class="layout-fluid">` plus `container-xl`
// containers. The vendored stylesheet carries `.layout-fluid .container,
// .layout-fluid [class*=" container-"], .layout-fluid [class^=container-]
// {max-width:100%}` for exactly that pairing; with `container-fluid` containers
// the body class does nothing at all.
//
// The single container-fluid Tabler itself uses — the one inside the vertical
// navbar aside — is the framework's own markup and stays, so the assertion
// counts rather than forbids.
func TestTablerFidelity_FluidLayoutUsesContainerXl(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)
	mux := buildMux()

	for _, path := range allPagePaths(name) {
		body := servePage(t, mux, path)
		if !strings.Contains(body, `<body class="layout-fluid">`) {
			t.Errorf("page %s: the body does not carry Tabler's layout-fluid class", path)
		}
		if !strings.Contains(body, `<div class="container-xl">`) {
			t.Errorf("page %s: no page container uses Tabler's container-xl", path)
		}
		// Exactly one container-fluid survives: the sidebar aside's own, which
		// is Tabler's markup.
		if got := strings.Count(body, `class="container-fluid"`); got != 1 {
			t.Errorf("page %s: %d container-fluid elements, want exactly 1 "+
				"(the sidebar aside's own); page containers must be container-xl", path, got)
		}
	}
}

// The class-attribute pattern this file scans with is classAttrRe, declared once
// for the package in comments_test.go; it captures the whitespace-separated
// token list inside a class attribute.

// styleSheetPaths are the embedded stylesheets that may define a class, in the
// order the pages load them: the vendored Tabler distribution, the vendored
// Tabler Icons webfont stylesheet, and the project's own override sheet. A class
// that resolves in none of them is styled by nothing the binary ships.
var styleSheetPaths = []string{
	"static/vendor/tabler/tabler.min.css",
	"static/vendor/tabler-icons/tabler-icons.min.css",
	"static/style.css",
}

// structuralHookClasses are the classes that legitimately carry no CSS rule.
// Each is a documented structural hook rather than a styling class, and the
// entry records which component owns it. Anything not on this list and not in a
// stylesheet is an invented class — the failure mode SPEC/WEB.md § UI Framework
// rule 10 exists to prevent, and the one that let `navbar-heading` and
// `navbar-divider` ship propped up by project CSS.
var structuralHookClasses = map[string]string{
	// Tabler's own component skeletons: shared/ui/DatagridItem.astro emits the
	// item and content wrappers, only the title of which is styled.
	"datagrid-item":    "Tabler DatagridItem structure",
	"datagrid-content": "Tabler DatagridItem structure",
	// shared/components/navbar/NavbarMenu.astro wraps every nav-link label in
	// this span; the vertical navbar styles the link, not the span.
	"nav-link-title": "Tabler NavbarMenu structure",
	// Project BEM block and element names whose styled members are the
	// children: static/style.css styles .graph-query-bar__row and
	// .labels-sidebar__heading, and these two are their unstyled hooks.
	"graph-query-bar":              "project BEM block for .graph-query-bar__*",
	"labels-sidebar__heading-text": "project BEM element inside .labels-sidebar__heading",
}

// TestTablerFidelity_NoClassOutsideTheVendoredStylesheets asserts every class
// token a served page emits resolves to a rule in one of the embedded
// stylesheets, or is one of the explicitly recorded structural hooks.
//
// This is the general guard behind SPEC/WEB.md § UI Framework rules 8 and 10.
// A class Tabler does not define is either dead markup or, worse, a bespoke
// component wearing a framework-looking name and kept alive by a rule in
// static/style.css — which is a divergence from Tabler dressed as an override.
// Both `navbar-heading` and `navbar-divider` shipped that way until this guard
// existed.
//
// Both sides are read through the go:embed filesystems, so the test gates
// exactly what the binary ships.
func TestTablerFidelity_NoClassOutsideTheVendoredStylesheets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)
	mux := buildMux()

	sheets := make([]string, 0, len(styleSheetPaths))
	for _, p := range styleSheetPaths {
		b, err := staticFS.ReadFile(p)
		if err != nil {
			t.Fatalf("reading embedded stylesheet %s: %v", p, err)
		}
		sheets = append(sheets, string(b))
	}

	// used maps each class token to the first page that emitted it.
	used := map[string]string{}
	for _, path := range allPagePaths(name) {
		for _, m := range classAttrRe.FindAllStringSubmatch(servePage(t, mux, path), -1) {
			for _, class := range strings.Fields(m[1]) {
				if _, seen := used[class]; !seen {
					used[class] = path
				}
			}
		}
	}

	// Falsifiability control: an extraction that found nothing would make the
	// assertion below pass vacuously. The shell alone emits far more than this.
	if len(used) < 50 {
		t.Fatalf("extracted only %d class tokens from the rendered pages; the "+
			"extraction is broken and the assertion below would be vacuous", len(used))
	}

	for class, path := range used {
		if _, ok := structuralHookClasses[class]; ok {
			continue
		}
		if !classDefined(sheets, class) {
			t.Errorf("page %s uses class %q, which no embedded stylesheet defines and "+
				"which is not a recorded structural hook: either use the Tabler class "+
				"that does the job, or record the hook in structuralHookClasses", path, class)
		}
	}

	// The recorded hooks must stay hooks: one that gains a rule should be
	// dropped from the list rather than left claiming it has none.
	for class, owner := range structuralHookClasses {
		if classDefined(sheets, class) {
			t.Errorf("class %q is recorded as a structural hook (%s) but a stylesheet "+
				"now defines it; remove it from structuralHookClasses", class, owner)
		}
	}
}

// classDefined reports whether any stylesheet carries a selector for class. The
// trailing check rejects a prefix match, so `.card` does not satisfy `.card-sm`.
func classDefined(sheets []string, class string) bool {
	needle := "." + class
	for _, sheet := range sheets {
		for i := 0; ; {
			j := strings.Index(sheet[i:], needle)
			if j < 0 {
				break
			}
			end := i + j + len(needle)
			if end == len(sheet) || !isClassNameByte(sheet[end]) {
				return true
			}
			i = i + j + 1
		}
	}
	return false
}

// isClassNameByte reports whether b can continue a CSS class name, so a match
// followed by one of these bytes is a longer class rather than the one sought.
func isClassNameByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '-' || b == '_':
		return true
	}
	return false
}

// TestTablerFidelity_PageBodyIsTheMainLandmark asserts the page body is the
// <main class="page-body"> landmark Tabler v1.4.0's built shell renders
// (shared/layouts/DefaultLayout.astro: `<main id="content" class="page-body">`),
// and that exactly one exists per page.
//
// The project takes the element and the class but not the id, which exists only
// to anchor a skip link whose `skip-link` class lives in Tabler's demo
// stylesheet rather than in the distributed tabler.min.css — the vendored sheet
// defines no rule for it. `.page-body` is styled by class, so the element change
// carries no visual effect; it only gives the document its main landmark.
//
// Tabler's older documented "Sample layout" snippet still shows a div here.
// Where the two disagree, the built shell of the vendored version governs, so
// this test also fails if the div comes back.
func TestTablerFidelity_PageBodyIsTheMainLandmark(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)
	mux := buildMux()

	for _, path := range allPagePaths(name) {
		body := servePage(t, mux, path)
		if got := strings.Count(body, `<main class="page-body">`); got != 1 {
			t.Errorf("page %s: %d <main class=\"page-body\"> elements, want exactly 1", path, got)
		}
		if got := strings.Count(body, "</main>"); got != 1 {
			t.Errorf("page %s: %d </main> closings, want exactly 1", path, got)
		}
		if strings.Contains(body, `<div class="page-body">`) {
			t.Errorf("page %s: the page body regressed to a <div>; Tabler's shell "+
				"makes it the <main> landmark", path)
		}
	}
}
