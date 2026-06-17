package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// TestSprintCard_ShowsTitle asserts the shared sprint-card partial surfaces the
// sprint's title in its header alongside the "Sprint #<ID>" identifier, so the
// sprint is identifiable at a glance in the Próximos, Actual, and Concluídos
// listings (SPEC/WEB.md § Shared Sprint-Card Partial, rule 2; Acceptance
// Criterion 13). The fixture seeds distinct, realistic titles per sprint, so a
// title appearing under a tab proves the card rendered that sprint's own title.
func TestSprintCard_ShowsTitle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-sprint-card-title")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name)

	// The fixture's titles double as descriptions; assert each distinct sprint
	// title is present on the sprints landing page (rendered by the card header).
	// The titles are intentionally unique per sprint in the fixture.
	for _, title := range []string{
		"Plan the read-only web sprint presentation",       // pendingID
		"Vendor the Tabler admin shell and dark theme",     // pendingID2
		"Ship the initial knowledge-graph viewer",          // closedLower
		"Deliver the sprint detail page and task modal",    // openID
		"Harden the web read path against malformed input", // closedHigher
	} {
		if !strings.Contains(body, title) {
			t.Errorf("sprints page card header missing sprint title %q", title)
		}
	}

	// The card header still carries both the "Sprint #<ID>" identifier and the
	// status badge, so adding the title did not displace them.
	if !strings.Contains(body, "Sprint #"+itoa(f.openID)) {
		t.Errorf("sprints page card header missing the Sprint #%d identifier", f.openID)
	}
	if !strings.Contains(body, `<span class="badge bg-blue-lt">`) {
		t.Errorf("sprints page card header missing the status badge")
	}
}

// TestSprintDetail_ShowsTitleAndOrder asserts the Sprint Detail Sub-Template's
// metadata datagrid carries both the sprint's Title (placed first, before ID)
// and its execution Order (placed after Status), with the actual values
// rendered (SPEC/WEB.md § Sprint Detail Sub-Template, rule 2; § Roadmap Sprint
// Page, "Sprint details"). The OPEN sprint was seeded with a known title and
// Order (99).
func TestSprintDetail_ShowsTitleAndOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-sprint-detail-title-order")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.openID))

	// The datagrid carries the Title and Order titles.
	for _, marker := range []string{">Title<", ">Order<"} {
		if !strings.Contains(body, marker) {
			t.Errorf("sprint detail datagrid missing %q", marker)
		}
	}

	// Title must appear before ID in the datagrid (first item), and Order must
	// appear after Status, matching the required field order.
	assertOrdered(t, "sprint detail datagrid order", body, []string{
		">Title<", ">ID<", ">Status<", ">Order<",
	})

	// The OPEN sprint's actual title and execution order value (99) are rendered.
	const openTitle = "Deliver the sprint detail page and task modal"
	if !strings.Contains(body, openTitle) {
		t.Errorf("sprint detail page missing the sprint title %q", openTitle)
	}
	// The Order value 99 is rendered in its datagrid-content. Scope the check to
	// the Order datagrid-item so an unrelated "99" elsewhere cannot satisfy it.
	orderItem := `<div class="datagrid-title">Order</div>
                  <div class="datagrid-content">99</div>`
	if !strings.Contains(body, orderItem) {
		t.Errorf("sprint detail page does not render the execution order value 99 in the Order datagrid item")
	}
}

// TestSprintPage_HeaderShowsTitle asserts the single Sprint Page H2 header
// presents the sprint's title together with the "Sprint #<ID>" identifier
// (SPEC/WEB.md § Roadmap Sprint Page, "Page header"; Acceptance Criterion 13).
func TestSprintPage_HeaderShowsTitle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-sprint-page-header-title")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.openID))

	const openTitle = "Deliver the sprint detail page and task modal"
	// The H2 page-title carries the sprint title.
	h2 := `<h2 class="page-title">` + openTitle
	if !strings.Contains(body, h2) {
		t.Errorf("sprint page H2 header missing the sprint title; expected to start with %q", h2)
	}
	// The "Sprint #<ID>" identifier remains present (now in the pretitle).
	if !strings.Contains(body, "Sprint #"+itoa(f.openID)) {
		t.Errorf("sprint page header missing the Sprint #%d identifier", f.openID)
	}
}

// TestSprint_TitleIsHTMLEscaped locks in safe rendering: a sprint title
// containing HTML metacharacters (`<`, `&`) MUST be auto-escaped by the template
// everywhere it is rendered — the card header on the sprints landing page, and
// the datagrid plus H2 header on the single sprint page — so no raw markup can
// reach the browser (SPEC/WEB.md § Security and Constraints; the template marks
// the title with neither template.HTML nor a safe pipeline). This is a
// regression guard against switching the title to unescaped output.
func TestSprint_TitleIsHTMLEscaped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const name = "web-sprint-title-escape"
	const rawTitle = `Migrate <auth> & sessions store`
	const escapedTitle = `Migrate &lt;auth&gt; &amp; sessions store`

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	sprintID, err := database.CreateSprint(context.Background(), &models.Sprint{
		Status:      models.SprintPending,
		Title:       rawTitle,
		Description: "Move the session token store behind the new auth boundary",
		CreatedAt:   now,
		Order:       7,
	})
	if err != nil {
		_ = database.Close()
		t.Fatalf("creating sprint: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("closing roadmap: %v", err)
	}

	mux := buildMux()

	for _, path := range []string{
		"/roadmaps/" + name, // sprints landing (card header)
		"/roadmaps/" + name + "/sprints/" + itoa(sprintID), // single sprint page (datagrid + H2)
	} {
		body := servePage(t, mux, path)
		if !strings.Contains(body, escapedTitle) {
			t.Errorf("page %s does not render the sprint title HTML-escaped; expected %q", path, escapedTitle)
		}
		// The raw, unescaped title must NOT appear: its presence would mean the
		// template emitted unescaped markup, an XSS regression.
		if strings.Contains(body, rawTitle) {
			t.Errorf("page %s rendered the sprint title UNESCAPED (raw %q present); the title must be auto-escaped", path, rawTitle)
		}
	}
}
