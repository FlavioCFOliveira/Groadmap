package web

import (
	"regexp"
	"slices"
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
	t.Setenv("HOME", shortHome(t))
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

// datagridItemPattern captures the title and the content of one item of the
// sprint metadata datagrid, as the Sprint Detail Sub-Template writes it.
var datagridItemPattern = regexp.MustCompile(
	`<div class="datagrid-item">\s*<div class="datagrid-title">([^<]*)</div>\s*<div class="datagrid-content[^"]*">(.*?)</div>\s*</div>`)

// sprintDatagridItems returns the [title, content] pairs of the one sprint
// metadata datagrid a sprint page carries, in document order. It fails the test
// when the page carries no datagrid or more than one.
func sprintDatagridItems(t *testing.T, body string) [][2]string {
	t.Helper()
	const open = `<div class="datagrid">`
	if n := strings.Count(body, open); n != 1 {
		t.Fatalf("sprint page carries %d metadata datagrids, want exactly 1", n)
	}
	_, grid, _ := strings.Cut(body, open)
	// The member-tasks board follows the Sprint details card, so it bounds the
	// datagrid from above.
	if end := strings.Index(grid, `data-role="task-board"`); end >= 0 {
		grid = grid[:end]
	}
	matches := datagridItemPattern.FindAllStringSubmatch(grid, -1)
	items := make([][2]string, 0, len(matches))
	for _, m := range matches {
		items = append(items, [2]string{m[1], m[2]})
	}
	// Every datagrid item must have been captured: an item the pattern missed
	// would hide an extra field from the exact-shape assertion.
	if got, want := len(items), strings.Count(grid, `<div class="datagrid-item">`); got != want {
		t.Fatalf("captured %d datagrid items, but the datagrid carries %d", got, want)
	}
	return items
}

// TestSprintDetail_DatagridHoldsExactlyCreatedStartedClosed asserts the sprint
// metadata datagrid of the Sprint details card holds exactly three fields, in the
// order Created, Started, Closed, with an em dash in place of an unset
// started_at or closed_at, and carries none of the ID, Title, Status, Order,
// Capacity, or Tasks fields; the page shows neither the execution order nor the
// capacity (SPEC/WEB.md § Sprint Detail Sub-Template, rule 2; § Roadmap Sprint
// Page, "Sprint details"; Acceptance Criterion 14).
//
// The fixture gives the three sprints every combination the placeholder rule
// distinguishes: the OPEN sprint is started and not closed, the PENDING sprint is
// neither, and the lower CLOSED sprint is closed (at a fixed instant) without
// ever having been started.
func TestSprintDetail_DatagridHoldsExactlyCreatedStartedClosed(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintFixture(t, "web-sprint-detail-datagrid")
	mux := buildMux()

	const emDash = "&mdash;"
	cases := []struct {
		name    string
		id      int
		started bool   // started_at is set: a timestamp, not the em dash
		closed  string // the expected Closed content
	}{
		{"OPEN (started, not closed)", f.openID, true, emDash},
		{"PENDING (neither started nor closed)", f.pendingID, false, emDash},
		{"CLOSED (closed, never started)", f.closedLower, false, "2026-05-20T18:30:00Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(c.id))
			items := sprintDatagridItems(t, body)

			titles := make([]string, len(items))
			for i, it := range items {
				titles[i] = it[0]
			}
			if want := []string{"Created", "Started", "Closed"}; !slices.Equal(titles, want) {
				t.Fatalf("datagrid titles = %q, want exactly %q", titles, want)
			}

			created, started, closed := items[0][1], items[1][1], items[2][1]
			if created == "" || created == emDash {
				t.Errorf("Created = %q, want the sprint's created_at", created)
			}
			if c.started {
				if started == "" || started == emDash {
					t.Errorf("Started = %q, want the sprint's started_at", started)
				}
			} else if started != emDash {
				t.Errorf("Started = %q, want the em dash placeholder %q", started, emDash)
			}
			if closed != c.closed {
				t.Errorf("Closed = %q, want %q", closed, c.closed)
			}

			// None of the six removed fields, anywhere on the page: the datagrid
			// is the only place a datagrid title is written.
			for _, gone := range []string{"ID", "Title", "Status", "Order", "Capacity", "Tasks"} {
				if strings.Contains(body, `<div class="datagrid-title">`+gone+`</div>`) {
					t.Errorf("the page still carries the %q datagrid field", gone)
				}
			}
			// Neither the capacity nor its unset form is shown.
			if strings.Contains(body, "Unlimited") {
				t.Errorf("the page still shows the capacity placeholder %q", "Unlimited")
			}
		})
	}
}

// TestSprintPage_HeaderShowsTitle asserts the single Sprint Page H2 header
// presents the sprint's title together with the "Sprint #<ID>" identifier
// (SPEC/WEB.md § Roadmap Sprint Page, "Page header"; Acceptance Criterion 13).
func TestSprintPage_HeaderShowsTitle(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
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
// the H2 header on the single sprint page — so no raw markup can
// reach the browser (SPEC/WEB.md § Security and Constraints; the template marks
// the title with neither template.HTML nor a safe pipeline). This is a
// regression guard against switching the title to unescaped output.
func TestSprint_TitleIsHTMLEscaped(t *testing.T) {
	t.Setenv("HOME", shortHome(t))

	const name = "web-sprint-title-escape"
	const rawTitle = `Migrate <auth> & sessions store`
	const escapedTitle = `Migrate &lt;auth&gt; &amp; sessions store`

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	sprintID, err := seedSprint(database, &models.Sprint{
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
		"/roadmaps/" + name + "/sprints/" + itoa(sprintID), // single sprint page (H2 header)
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
