package web

import (
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

// TestTablerFidelity_FooterRowStructure is the regression guard for
// SPEC/WEB.md § UI Framework rule 11 and Acceptance Criterion 63: the footer
// follows Tabler's footer row structure, as the Tabler footer example does.
// The pre-rule markup placed the notice directly in the container; this test
// asserts the Tabler footer row wraps the read-only notice in a column, and
// that the exact notice text is preserved verbatim and the pages stay
// read-only (no write affordance is introduced by the structure change).
//
// The graph page intentionally has no footer (SPEC/WEB.md does not require one
// there), so it is excluded; the index, sprints, tasks, sprint detail, and
// audit pages all carry the footer.
func TestTablerFidelity_FooterRowStructure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// seedRoadmap creates sprint 1 (for the /sprints/1 detail page);
	// seedRoadmapWithAudit adds audit entries to the same roadmap (for /audit).
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)
	mux := buildMux()

	const notice = "Read-only. The rmp CLI remains the sole write path."

	paths := []string{
		"/",
		"/roadmaps/" + name,
		"/roadmaps/" + name + "/tasks",
		"/roadmaps/" + name + "/sprints/1",
		"/roadmaps/" + name + "/audit",
	}
	for _, path := range paths {
		body := servePage(t, mux, path)
		if !strings.Contains(body, `<footer class="footer footer-transparent d-print-none">`) {
			t.Errorf("page %s: missing the read-only footer element", path)
			continue
		}
		if !strings.Contains(body, `<div class="row text-center align-items-center flex-row-reverse">`) {
			t.Errorf("page %s: footer does not use Tabler's footer row structure "+
				`<div class="row text-center align-items-center flex-row-reverse">`, path)
		}
		if !strings.Contains(body, `<div class="col-12 text-secondary">`) {
			t.Errorf("page %s: footer notice is not wrapped in a Tabler column "+
				`<div class="col-12 text-secondary">`, path)
		}
		if !strings.Contains(body, notice) {
			t.Errorf("page %s: footer lost the verbatim read-only notice %q", path, notice)
		}
	}
}
