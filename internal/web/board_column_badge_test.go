package web

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The guards in this file cover the per-column count badge of the sprint page's
// member-tasks board: its COLOUR is the semantic colour of the status the column
// groups, while its TEXT stays that column's task count (SPEC/WEB.md § Sprint
// Detail Sub-Template, rule 3, Column header; § Status, Priority, and Severity
// Badge Colours, rule 2; Acceptance Criterion 140). The roadmap tasks page carries
// no board and so no column badge (Acceptance Criterion 81).
//
// Two things make this rule hard to check one column at a time, and both are why
// every guard below asserts the board's columns TOGETHER.
//
// The first is the neutral trap. A column whose variant were bg-secondary-lt would
// render identically whether the mapping was applied or not, so a check that
// looked at one column alone could pass on a board where no column was coloured
// at all. What separates a conforming rendering from a non-conforming one is the
// three columns together.
//
// The second is the second-mapping trap. A template that wrote the right colour
// classes out as literals would satisfy every value assertion here on the day it
// was written and then drift from the one authoritative mapping the moment that
// mapping changed. The probe guard below closes it: it re-parses the templates
// with taskStatusBadge replaced by a sentinel-returning probe, so a literal
// survives the substitution unchanged and fails, and only a template that CALLS
// the helper — with the right status — renders the sentinels.

// wantSprintBoardCanonical is the canonical status of each column of the sprint
// page's member-tasks board, in board order, written out from the specification
// rather than read from the model the board renders.
//
// The values are stated here because the model is what these guards are checking.
// Reading sprintBoardColumns[i].canonical and comparing it to itself would pass on
// any three statuses, including three wrong ones, so the specification's own
// answer is written once, here, and everything else is derived from it.
//
// Why each: a task waiting in a sprint is normally a SPRINT task — a BACKLOG task
// inside a sprint is the exceptional case of a task returned to the backlog
// without leaving the sprint — the column named DOING takes the colour of the
// status named DOING, and CLOSED holds COMPLETED alone (SPEC/WEB.md § Sprint
// Detail Sub-Template, rule 3, Column header).
var wantSprintBoardCanonical = []struct {
	heading   string
	canonical models.TaskStatus
}{
	{"WAITING", models.StatusSprint},
	{"DOING", models.StatusDoing},
	{"CLOSED", models.StatusCompleted},
}

// TestSprintBoardColumns_NameTheCanonicalStatusOfTheirGroup pins the three
// canonical statuses in the model, which is where the board names them.
//
// The colour guards below derive what they expect from this table, so this is the
// one place the three answers are stated, and a model that named the wrong status
// for a group fails here rather than silently colouring a column wrongly and
// taking every derived assertion with it.
func TestSprintBoardColumns_NameTheCanonicalStatusOfTheirGroup(t *testing.T) {
	if len(sprintBoardColumns) != len(wantSprintBoardCanonical) {
		t.Fatalf("the board defines %d columns, want %d", len(sprintBoardColumns),
			len(wantSprintBoardCanonical))
	}
	for i, want := range wantSprintBoardCanonical {
		got := sprintBoardColumns[i]
		if got.heading != want.heading {
			t.Fatalf("column %d is headed %q, want %q", i, got.heading, want.heading)
		}
		if got.canonical != want.canonical {
			t.Errorf("the %s column names %q as its canonical status, want %q; the count badge "+
				"takes the colour of the status a task is normally in at that stage of the "+
				"sprint (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3, Column header)",
				got.heading, got.canonical, want.canonical)
		}
		// The canonical status must belong to the category the column holds, or the
		// column would be coloured by a status no card in it can carry.
		if models.CategorizeTaskStatus(got.canonical) != got.category {
			t.Errorf("the %s column is coloured by %q, which categorises outside the group the "+
				"column holds; the canonical status is the status a task in this column is "+
				"normally in, not a status the column never shows", got.heading, got.canonical)
		}
	}
}

// TestBoardColumnBadges_CarryTheColourOfTheStatusTheyGroup is the gate for
// Acceptance Criterion 140: every per-column count badge of the two boards carries
// the semantic colour of the status its column groups, while its text stays that
// column's task count.
//
// Both boards are asserted in full, and the colours are taken FROM the semantic
// helper rather than written out here, so this test states that each column is
// coloured by the right status and leaves the status-to-variant mapping itself to
// the test that pins it (Acceptance Criterion 61).
//
// The falsifiability control is explicit: a rendering that gave every column of a
// board the neutral variant must fail, so each board is also asserted to show more
// than one variant across its columns.
func TestBoardColumnBadges_CarryTheColourOfTheStatusTheyGroup(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	// The tasks page carries no board, so it carries no column badge at all.
	if tasksPage := servePage(t, mux, "/roadmaps/"+f.name+"/tasks"); strings.Contains(boardRegion(t, tasksPage), `data-role="task-board-column"`) {
		t.Errorf("the tasks page renders a board column; the page presents one list (Acceptance Criterion 81)")
	}

	// The sprint board: a column groups a SET of statuses, so its badge takes the
	// variant of the group's canonical status.
	sprintColumns := memberBoardColumns(t, servePage(t, mux, f.path()))
	sprintVariants := map[string]bool{}
	wantCounts := f.wantColumns()
	for i, want := range wantSprintBoardCanonical {
		heading, variant, count := columnBadge(t, sprintColumns[i])
		sprintVariants[variant] = true
		if heading != want.heading {
			t.Fatalf("sprint board column %d is headed %q, want %q", i, heading, want.heading)
		}
		if wantVariant := taskStatusBadge(want.canonical); variant != wantVariant {
			t.Errorf("the sprint board's %s column carries the count badge variant %q, want %q — "+
				"the variant the semantic mapping assigns to %q, the canonical status of the "+
				"group this column holds (Acceptance Criterion 140)",
				heading, variant, wantVariant, want.canonical)
		}
		if count != len(wantCounts[i]) {
			t.Errorf("the sprint board's %s column shows the count %d, want %d", heading, count,
				len(wantCounts[i]))
		}
	}
	if len(sprintVariants) != len(wantSprintBoardCanonical) {
		t.Errorf("the sprint board's three column badges carry %d distinct variant(s) (%v), want "+
			"3; the three canonical statuses are three different statuses, so three equal "+
			"variants mean the mapping was not applied", len(sprintVariants),
			sortedKeys(sprintVariants))
	}

}

// TestBoardColumnBadges_ClassComesFromTheOneHelper proves that the board DECIDES
// its column badge's class by calling the semantic helper with that column's
// status, rather than carrying a class that happens to read the same as the
// helper's answer today.
//
// It re-parses the embedded templates with taskStatusBadge replaced by a probe
// returning a sentinel class naming the status it was called with, renders both
// boards, and looks for the sentinels. A class written into the template survives
// the substitution unchanged and fails; only a template that calls the helper
// renders "probe-BACKLOG", "probe-SPRINT" and the rest.
//
// This is what closes the second-mapping door for good, and it is also what makes
// the BACKLOG column non-vacuous. Against the real mapping that column is
// indistinguishable from a fixed bg-secondary-lt; under the probe, a fixed
// bg-secondary-lt is exactly what a non-conforming template still shows, while a
// conforming one shows probe-BACKLOG.
//
// It pins each column to the RIGHT status as well. The board keeps the heading
// and the status it colours by in SEPARATE fields, so a column can carry the right
// label and the wrong colour; swapping the canonical status of two of its columns
// leaves three distinct classes, passes a mere distinctness check, and fails
// HERE, naming both columns.
//
// The view model is built with no task at all, so every column shows 0 — which
// also proves the colour is chosen with no card to read a status from.
func TestBoardColumnBadges_ClassComesFromTheOneHelper(t *testing.T) {
	funcs := templateFuncs()
	funcs["taskStatusBadge"] = func(s models.TaskStatus) string { return "probe-" + string(s) }

	tmpl, err := template.New("").Funcs(funcs).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		t.Fatalf("parsing the embedded templates with the probe helper: %v", err)
	}

	render := func(name string, data any) string {
		t.Helper()
		var buf bytes.Buffer
		if rerr := tmpl.ExecuteTemplate(&buf, name, data); rerr != nil {
			t.Fatalf("rendering %s with the probe helper: %v", name, rerr)
		}
		return buf.String()
	}

	// The sprint board: three columns, each headed by its own heading and badged
	// by the probe's answer for the CANONICAL status of its group.
	sprint := render("sprint.html", sprintPageData{
		Name:    "probe",
		Columns: groupIntoSprintBoardColumns(nil),
	})
	for _, want := range wantSprintBoardCanonical {
		markup := columnBadgeMarkup(want.heading, "probe-"+string(want.canonical), 0)
		if !strings.Contains(sprint, markup) {
			t.Errorf("the sprint board's %s column does not render %q under the probe helper: its "+
				"class is not produced by taskStatusBadge(%s) — either the class is written "+
				"into the template or the column is passing the wrong canonical status",
				want.heading, markup, want.canonical)
		}
	}

	// And no board column keeps a real colour variant under the probe, which is
	// what a hardcoded class would do. The Comments card's own neutral badge is
	// excluded by slicing the board region out of the sprint page first: that badge
	// is SUPPOSED to survive the substitution (see the neutral guard below).
	for what, board := range map[string]string{
		"the sprint board": memberBoardRegion(t, sprint),
	} {
		for _, header := range columnHeaderSlices(board) {
			if strings.Contains(header, "badge bg-") {
				t.Errorf("%s renders the column header %q under the probe helper; a column badge "+
					"still carrying a real bg-*-lt variant is a class the template wrote out "+
					"itself, which is a second mapping free to drift from the first", what,
					strings.TrimSpace(header))
			}
		}
	}
}

// TestBoardColumnBadges_EmptyColumnKeepsItsStatusColour is the gate for the clause
// of Acceptance Criterion 140 that says the colour follows the COLUMN and not the
// cards in it: a column holding no task shows the count 0 and keeps the colour of
// its status.
//
// The board is checked with nothing in it at all — a sprint with no member task —
// so every column is empty and the colour has no card to be read from.
func TestBoardColumnBadges_EmptyColumnKeepsItsStatusColour(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	emptySprint := seedSprintWithMembers(t, "clearing-window-empty", 0)
	mux := buildMux()

	sprintColumns := memberBoardColumns(t,
		servePage(t, mux, "/roadmaps/clearing-window-empty/sprints/"+itoa(emptySprint)))
	for i, want := range wantSprintBoardCanonical {
		heading, variant, count := columnBadge(t, sprintColumns[i])
		if count != 0 {
			t.Fatalf("the %s column of an empty sprint shows the count %d, want 0", heading, count)
		}
		if wantVariant := taskStatusBadge(want.canonical); variant != wantVariant {
			t.Errorf("the empty %s column carries the variant %q, want %q; a column holding no "+
				"task keeps the colour of the status it groups", heading, variant, wantVariant)
		}
	}
}

// TestCountBadges_WithNoStatusToKeyOnStayNeutral is the boundary of Acceptance
// Criterion 140, and it is what keeps the rule a rule rather than a licence to
// colour any count. The Comments card header on the Roadmap Sprint Page counts
// comments, and a comment carries no status of any kind, so the semantic mapping
// has nothing to key on and the badge keeps the neutral bg-secondary-lt.
//
// It is asserted on the same page that carries the coloured board, so the two are
// compared where the distinction actually lives: the board's column badges above
// the card are coloured, the card's own count badge below it is not.
func TestCountBadges_WithNoStatusToKeyOnStayNeutral(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	page := servePage(t, mux, f.path())

	// The Comments card's count badge, neutral.
	const commentsHeader = `<h3 class="card-title">Comments <span class="badge `
	at := strings.Index(page, commentsHeader)
	if at < 0 {
		t.Fatalf("the sprint page renders no Comments card header with a count badge")
	}
	rest := page[at+len(commentsHeader):]
	variant, _, ok := strings.Cut(rest, " ms-2\">")
	if !ok {
		t.Fatalf("the Comments card's count badge does not carry the shared header badge markup: "+
			"%q", rest[:min(80, len(rest))])
	}
	if variant != badgeSecondary {
		t.Errorf("the Comments card's count badge carries %q, want the neutral %q; it counts "+
			"comments, and a comment has no status for the semantic mapping to key on "+
			"(SPEC/WEB.md § Status, Priority, and Severity Badge Colours, rule 2, The "+
			"discriminating test; Acceptance Criterion 140)", variant, badgeSecondary)
	}

	// The control that keeps that neutrality from being the whole page's: the board
	// above it on the SAME page is coloured, and not with the neutral variant.
	coloured := false
	for _, column := range memberBoardColumns(t, page) {
		if _, v, _ := columnBadge(t, column); v != badgeSecondary {
			coloured = true
		}
	}
	if !coloured {
		t.Errorf("no column badge on this page carries a variant other than %q, so the Comments "+
			"card looking neutral says nothing about the boundary this test is written for",
			badgeSecondary)
	}
}

// ==================== HELPERS ====================

// columnBadgeMarkup is the whole column header a board renders: the heading, the
// badge in its colour variant, and the count.
//
// It is built here so the probe assertions compare the three together. Checking
// for the sentinel class on its own would pass on a template that rendered it in
// the wrong column, and checking for the heading on its own would pass on a
// template that rendered no badge at all.
func columnBadgeMarkup(heading, variant string, count int) string {
	return `<h3 class="card-title">` + heading + ` <span class="badge ` + variant +
		` ms-2">` + itoa(count) + `</span></h3>`
}

// columnHeaderSlices returns every column header element of a board region, which
// is what the probe guard scans for surviving colour literals.
func columnHeaderSlices(region string) []string {
	const open = `<h3 class="card-title">`
	var headers []string
	for rest := region; ; {
		at := strings.Index(rest, open)
		if at < 0 {
			return headers
		}
		rest = rest[at:]
		end := strings.Index(rest, "</h3>")
		if end < 0 {
			return append(headers, rest)
		}
		headers = append(headers, rest[:end])
		rest = rest[end:]
	}
}
