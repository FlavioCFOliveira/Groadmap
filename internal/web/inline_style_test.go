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
// detail page, the roadmap audit log page, and the knowledge-graph page — and
// asserts the rendered HTML contains no `style="` substring. Because the shared
// partials (the admin-shell sidebar/topnavbar/head, the sprint-card partial,
// the sprint-detail sub-template, and the per-task detail modal) are composed
// into these pages, asserting the property on the fully rendered output covers
// the partials as well.
//
// It also renders the two empty-state branches whose icons previously carried an
// inline `style="font-size:2.5rem"`: the roadmap index with no roadmaps, and the
// knowledge-graph page with no graph store. Those branches only emit their icon
// markup when there is nothing to list, so they are exercised explicitly here to
// prove the sizing moved to a stylesheet class.
func TestPages_NoInlineStyleAttribute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	// Every populated page route, including the sprint detail page (sprint id 1
	// is the sprint seedRoadmap creates) and the audit page.
	populated := []string{
		"/",
		"/roadmaps/" + name,
		"/roadmaps/" + name + "/tasks",
		"/roadmaps/" + name + "/sprints/1",
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
	t.Setenv("HOME", t.TempDir())
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

// TestSidebarSectionLabel_UsesTablerSubheaderIdiom proves the sidebar section
// label uses Tabler's vertical-navbar subheader + hr divider idiom instead of an
// inline-styled label (SPEC/WEB.md § UI Framework rule 10, Acceptance Criterion
// 62). The label must carry Tabler's `subheader` class (which supplies the small
// uppercase letter-spaced muted look) and be accompanied by an hr divider, and
// must no longer use the previous inline-styled nav-link span.
func TestSidebarSectionLabel_UsesTablerSubheaderIdiom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+name)
	for _, marker := range []string{
		`class="navbar-heading subheader"`, // Tabler subheader idiom for the label
		`class="navbar-divider"`,           // the hr divider that precedes it
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("sidebar section label missing Tabler subheader marker %q", marker)
		}
	}
	// The roadmap name is rendered as the subheader text.
	if !strings.Contains(body, ">"+name+"</h2>") {
		t.Errorf("sidebar subheader does not render the roadmap name %q as its text", name)
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
