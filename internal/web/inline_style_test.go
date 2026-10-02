package web

import (
	"strings"
	"testing"
)

// TestPages_NoInlineStyleAttribute is the regression guard for SPEC/WEB.md
// § UI Framework rule 10 and Acceptance Criterion 62: no template the server
// renders may carry a presentational inline `style="..."` attribute. All
// styling must live in the vendored Tabler classes/utilities or in the project
// override stylesheet (static/style.css).
//
// The test renders every HTML page route the server serves — the roadmap index,
// the roadmap sprints landing page, the roadmap tasks page, a roadmap sprint
// detail page, a roadmap task page, the roadmap audit log page, and the
// knowledge-graph page — and
// asserts the rendered HTML contains no `style="` substring. Because the shared
// partials (the admin-shell sidebar/topnavbar/head, the sprint-card partial,
// the sprint-detail sub-template, and the comment timeline) are composed
// into these pages, asserting the property on the fully rendered output covers
// the partials as well.
//
// It also renders the two empty-state branches whose icons previously carried an
// inline `style="font-size:2.5rem"`: the roadmap index with no roadmaps, and the
// knowledge-graph page with no graph store. Those branches only emit their icon
// markup when there is nothing to list, so they are exercised explicitly here to
// prove the sizing moved to a stylesheet class.
func TestPages_NoInlineStyleAttribute(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	// Every populated page route, including the sprint detail page (sprint id 1
	// is the sprint seedRoadmap creates) and the audit page.
	populated := []string{
		"/",
		"/roadmaps/" + name,
		"/roadmaps/" + name + "/tasks",
		"/roadmaps/" + name + "/sprints/1",
		"/roadmaps/" + name + "/tasks/1",
		"/roadmaps/" + name + "/audit",
		"/roadmaps/" + name + "/graph",
	}
	for _, path := range populated {
		body := servePage(t, mux, path)
		assertNoInlineStyle(t, path, body)
	}
}

// TestPages_EmptyStates_NoInlineStyleAttribute renders the empty-state branches
// whose icons formerly used an inline style, so the regression guard covers the
// markup those branches emit.
func TestPages_EmptyStates_NoInlineStyleAttribute(t *testing.T) {
	// A fresh HOME with no roadmaps exercises the index empty state, whose icon
	// glyph carries the empty-icon-glyph class (was an inline style).
	t.Setenv("HOME", shortHome(t))
	mux := buildMux()
	body := servePage(t, mux, "/")
	if !strings.Contains(body, "empty-icon-glyph") {
		t.Errorf("index empty state did not render; cannot verify its icon markup")
	}
	assertNoInlineStyle(t, "/ (empty index)", body)

	// The graph page of a roadmap with no graph store renders the empty-graph
	// branch, whose icon glyph also carries empty-icon-glyph.
	name := seedRoadmap(t, "graph-empty")
	graphBody := servePage(t, mux, "/roadmaps/"+name+"/graph")
	if !strings.Contains(graphBody, "empty-icon-glyph") {
		t.Errorf("graph empty state did not render; cannot verify its icon markup")
	}
	assertNoInlineStyle(t, "/roadmaps/"+name+"/graph (empty graph)", graphBody)
}

// TestSidebarSectionLabel_UsesTablerSectionTitle proves the sidebar section
// label is Tabler's own sidebar section title rather than an inline-styled label
// (SPEC/WEB.md § UI Framework rule 10, Acceptance Criterion 62): an
// `<li class="nav-section-title">` holding the roadmap name, placed directly
// inside the sidebar's `<ul class="navbar-nav">` before the roadmap's links, with
// no divider or horizontal rule anywhere in the sidebar, and carrying neither the
// `subheader` class nor a Tabler spacing utility.
//
// The first version of this rule reached for `navbar-heading` and
// `navbar-divider`, which Tabler does not ship; the second used Tabler's
// `subheader` with a `dropdown-divider` above it and a `px-3` gutter. Tabler's
// own NavbarMenu renders a section label as `nav-section-title`, which the
// vendored stylesheet aligns with the links through its own padding.
// TestTablerFidelity_NoClassOutsideTheVendoredStylesheets is the general guard;
// this test pins the specific markup.
func TestSidebarSectionLabel_UsesTablerSectionTitle(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+name)
	start := strings.Index(body, "<aside")
	end := strings.Index(body, "</aside>")
	if start < 0 || end < start {
		t.Fatalf("the page carries no sidebar <aside>:\n%s", body)
	}
	sidebar := body[start:end]

	label := `<li class="nav-section-title">` + name + `</li>`
	if got := strings.Count(sidebar, label); got != 1 {
		t.Fatalf("the sidebar carries %d copies of %q, want exactly 1:\n%s", got, label, sidebar)
	}
	// The label sits directly inside the sidebar's navbar-nav list: the element
	// that precedes it closes the roadmap-index entry, and the element that
	// follows it opens the first of the roadmap's own links.
	list := strings.Index(sidebar, `<ul class="navbar-nav`)
	at := strings.Index(sidebar, label)
	if list < 0 || at < list || strings.Contains(sidebar[list:at], "</ul>") {
		t.Errorf("the section label is not inside the sidebar's <ul class=\"navbar-nav\">:\n%s", sidebar)
	}
	if before := strings.TrimSpace(sidebar[:at]); !strings.HasSuffix(before, "</li>") {
		t.Errorf("the section label is not preceded by the roadmap-index <li>; the markup before it ends %q",
			before[max(0, len(before)-80):])
	}
	if after := strings.TrimSpace(sidebar[at+len(label):]); !strings.HasPrefix(after, `<li class="nav-item`) {
		t.Errorf("the section label is not followed by the roadmap's first nav-item; the markup after it starts %q",
			after[:min(80, len(after))])
	}

	for _, gone := range []string{"dropdown-divider", "<hr", "subheader", "navbar-heading", "navbar-divider"} {
		if strings.Contains(sidebar, gone) {
			t.Errorf("the sidebar carries %q; the section title needs no divider and no other label class", gone)
		}
	}
	// No spacing utility on the label: the vendored rule alone aligns it.
	if strings.Contains(sidebar, `class="nav-section-title `) {
		t.Errorf("the section label carries a class beyond nav-section-title:\n%s", sidebar)
	}
}

// assertNoInlineStyle fails when body contains any inline style attribute.
func assertNoInlineStyle(t *testing.T, label, body string) {
	t.Helper()
	if idx := strings.Index(body, `style="`); idx >= 0 {
		start := idx - 40
		if start < 0 {
			start = 0
		}
		end := idx + 40
		if end > len(body) {
			end = len(body)
		}
		t.Errorf("%s contains an inline style attribute near: ...%s...", label, body[start:end])
	}
}
