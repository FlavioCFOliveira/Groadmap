package web

import (
	"bytes"
	"database/sql"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file gates the two nullable columns of the read-only audit log page —
// Related Entity ID and Commit — which the page carried in its data and omitted
// from its markup, so the interface showed less than the log held and the commit
// hash the enrichment exists to record was invisible to anyone reading the
// roadmap through a browser (SPEC/WEB.md § Roadmap Audit Log Page; Acceptance
// Criterion 10).
//
// Where the assertions are scoped, and why. The rendered page carries an em dash
// in its <title> ("Groadmap — <name> / Audit") and a `text-truncate` on the top
// navbar's roadmap name, both of them present BEFORE these columns existed. A
// document-wide `strings.Contains(body, "—")` or `…, "text-truncate"` therefore
// passes against the unfixed page and proves nothing. Every assertion below is
// made on the audit table's own region, and on exact cell markup rather than on
// a token that appears elsewhere in the shell.

// The three states an audit row can be in with respect to the two nullable
// columns. auditCommitFixtureHash is a real-shaped 40-character lowercase hex
// hash: the schema admits 7 to 64 (SPEC/DATABASE.md § audit Table), and a long
// one is what makes "rendered verbatim" a claim with something to fail on.
const (
	auditCommitFixtureHash = "9c1f0a3e7b524d68af0c31d59e6b2470f8153ac2"
	auditFixtureTaskID     = 41
	auditFixtureSprintID   = 7
)

// seedRoadmapWithNullableAudit creates a roadmap holding one audit row of each
// reachable shape, in performed_at order (oldest first):
//
//  1. TASK_CREATE — carries neither column.
//  2. SPRINT_ADD_TASK — carries a counterpart entity and no commit hash.
//  3. TASK_STATUS_DOING — carries a commit hash and no counterpart.
//
// The rows go in through db.LogAuditTx, the single audit writer every rmp
// command uses, so the page reads rows shaped exactly like a real roadmap's —
// including its refusal to put either column on an operation the catalogue does
// not allow it on.
//
// No operation carries both columns today, which is precisely why both shapes
// are seeded: a renderer exercised only against rows that carry neither, or only
// against rows that carry both, would never meet the half-populated row that is
// the common case here.
func seedRoadmapWithNullableAudit(t *testing.T, name string) string {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	base := time.Date(2026, time.August, 20, 9, 0, 0, 0, time.UTC)
	at := func(i int) string { return base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339) }

	writes := []func(tx *sql.Tx) error{
		func(tx *sql.Tx) error {
			return db.LogAuditTx(tx, models.OpTaskCreate, models.EntityTask, auditFixtureTaskID, at(0))
		},
		func(tx *sql.Tx) error {
			return db.LogAuditTx(tx, models.OpSprintAddTask, models.EntitySprint, auditFixtureSprintID, at(1),
				db.WithRelatedEntity(auditFixtureTaskID))
		},
		func(tx *sql.Tx) error {
			return db.LogAuditTx(tx, models.OpTaskStatusDoing, models.EntityTask, auditFixtureTaskID, at(2),
				db.WithCommitHash(auditCommitFixtureHash))
		},
	}
	for i, write := range writes {
		if err := database.WithTransaction(write); err != nil {
			t.Fatalf("seeding audit row %d: %v", i, err)
		}
	}

	return name
}

var (
	auditTableRe = regexp.MustCompile(`(?s)<table\b.*?</table>`)
	auditTHRe    = regexp.MustCompile(`(?s)<th\b[^>]*>(.*?)</th>`)
	auditTRRe    = regexp.MustCompile(`(?s)<tr>(.*?)</tr>`)
	auditTDRe    = regexp.MustCompile(`(?s)<td\b[^>]*>.*?</td>`)
)

// auditTableRegion returns the audit table element of a rendered audit page.
// Every assertion in this file is made on it rather than on the whole document,
// because the shell around it already carries an em dash and a text-truncate of
// its own (see the note at the head of this file).
func auditTableRegion(t *testing.T, body string) string {
	t.Helper()
	region := auditTableRe.FindString(body)
	if region == "" {
		t.Fatalf("the rendered audit page carries no <table>; every assertion below would be vacuous")
	}
	return region
}

// auditRowCells returns the data rows of an audit table as their raw <td> markup,
// in document order, so an assertion can pin the exact cell a column renders
// rather than merely the presence of a value somewhere on the page.
func auditRowCells(t *testing.T, region string) [][]string {
	t.Helper()
	var rows [][]string
	for _, tr := range auditTRRe.FindAllStringSubmatch(region, -1) {
		cells := auditTDRe.FindAllString(tr[1], -1)
		if len(cells) == 0 {
			continue // the header row, whose cells are <th>
		}
		rows = append(rows, cells)
	}
	return rows
}

// The audit table's column positions, in the order SPEC/WEB.md fixes.
const (
	auditColID = iota
	auditColOperation
	auditColEntityType
	auditColEntityID
	auditColRelatedEntityID
	auditColCommit
	auditColPerformedAt
	auditColumnCount
)

// TestHandleAudit_PresentsTheSevenColumnsInOrder pins the column set and its
// order against the seven AuditEntry fields SPEC/WEB.md § Roadmap Audit Log Page
// requires, in the order it fixes them. The page carried five.
func TestHandleAudit_PresentsTheSevenColumnsInOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmapWithNullableAudit(t, "ledger-settlement")

	rec := getAudit(t, name, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("audit status = %d, want 200", rec.Code)
	}
	region := auditTableRegion(t, rec.Body.String())

	headers := auditTHRe.FindAllStringSubmatch(region, -1)
	got := make([]string, 0, len(headers))
	for _, m := range headers {
		got = append(got, strings.TrimSpace(m[1]))
	}
	want := []string{"ID", "Operation", "Entity Type", "Entity ID", "Related Entity ID", "Commit", "Performed At"}
	if len(got) != len(want) {
		t.Fatalf("the audit table has %d columns %v, want the %d of SPEC/WEB.md %v",
			len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("column %d is %q, want %q: the order is the one SPEC/WEB.md fixes "+
				"(the counterpart entity beside the entity it is a counterpart of, and the "+
				"timestamp last)", i, got[i], want[i])
		}
	}
}

// TestHandleAudit_RendersBothNullableColumnsPerEntry is the behavioural gate: it
// serves the page and pins the exact cell each of the three row shapes renders in
// the two new columns.
//
// The commit hash is asserted whole. "Renders verbatim" is what SPEC/WEB.md
// requires of the Commit column — the page does not abbreviate it and does not
// expand it — and the assertion is written against the full 40 characters so a
// renderer that shortened the text to the customary seven would fail rather than
// pass on a prefix match.
func TestHandleAudit_RendersBothNullableColumnsPerEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmapWithNullableAudit(t, "ledger-settlement")

	region := auditTableRegion(t, getAudit(t, name, "").Body.String())
	rows := auditRowCells(t, region)
	if len(rows) != 3 {
		t.Fatalf("the audit table renders %d data rows, want the 3 seeded", len(rows))
	}
	for i, cells := range rows {
		if len(cells) != auditColumnCount {
			t.Fatalf("row %d renders %d cells %v, want %d", i, len(cells), cells, auditColumnCount)
		}
	}

	// html/template writes the em dash as the literal UTF-8 code point, not as a
	// numeric entity, so the expected cell carries the placeholder itself.
	absentCell := `<td class="` + auditAbsentClass + `">` + absentPlaceholder + `</td>`

	// Ordered performed_at DESC, so the most recent row leads: the one carrying
	// the commit hash, then the one carrying the counterpart, then the one
	// carrying neither.
	cases := []struct {
		row     int
		what    string
		related string
		commit  string
	}{
		{
			row:     0,
			what:    "TASK_STATUS_DOING, which carries a commit hash and no counterpart",
			related: absentCell,
			commit:  `<td class="font-monospace text-truncate">` + auditCommitFixtureHash + `</td>`,
		},
		{
			row:     1,
			what:    "SPRINT_ADD_TASK, which names the task added and carries no commit hash",
			related: `<td>41</td>`,
			commit:  absentCell,
		},
		{
			row:     2,
			what:    "TASK_CREATE, which carries neither column",
			related: absentCell,
			commit:  absentCell,
		},
	}
	for _, c := range cases {
		if got := rows[c.row][auditColRelatedEntityID]; got != c.related {
			t.Errorf("the Related Entity ID cell of %s is %q, want %q", c.what, got, c.related)
		}
		if got := rows[c.row][auditColCommit]; got != c.commit {
			t.Errorf("the Commit cell of %s is %q, want %q", c.what, got, c.commit)
		}
	}

	// An abbreviation applied to the TEXT, rather than by the stylesheet, would
	// leave the hash's leading characters followed by an ellipsis. The exact-cell
	// assertions above already rule it out; this states the prohibition in the
	// terms SPEC/WEB.md uses, so a future change reads the reason it fails.
	for _, shortened := range []string{
		auditCommitFixtureHash[:7] + "&hellip;",
		auditCommitFixtureHash[:7] + "…",
		auditCommitFixtureHash[:8] + "…",
	} {
		if strings.Contains(region, shortened) {
			t.Errorf("the audit table abbreviates the commit hash to %q; the stored value is "+
				"rendered verbatim and shortened by the stylesheet, never by the renderer", shortened)
		}
	}
}

// TestHandleAudit_NullableColumnsAddNoControlAndNoLink guards the read-only
// promise at the point the two columns enter the markup. A commit hash is the one
// value on this page that invites a link to a code-hosting service and a copy
// button; the modal is forbidden both, and so is the table — Groadmap contacts no
// repository and holds no repository URL from which such a link could be built
// (SPEC/WEB.md § Roadmap Audit Log Page; § Task Detail Modal, Fields shown).
func TestHandleAudit_NullableColumnsAddNoControlAndNoLink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmapWithNullableAudit(t, "ledger-settlement")

	region := auditTableRegion(t, getAudit(t, name, "").Body.String())

	// Falsifiability control: the region must actually hold the hash, or the
	// absences below would be satisfied by an empty table.
	if !strings.Contains(region, auditCommitFixtureHash) {
		t.Fatalf("the audit table does not render the seeded commit hash; the absences "+
			"below would be vacuous. region=%q", region)
	}

	for _, forbidden := range []string{"<a ", "<form", "<button", "<input", "<select",
		"github.com", "gitlab.com", "commit/", "onclick", "data-bs-toggle"} {
		if strings.Contains(region, forbidden) {
			t.Errorf("the audit table contains %q: the page renders data only — no link, no "+
				"control, no edit affordance, and no code-host link for the commit hash", forbidden)
		}
	}
}

// TestAuditTemplate_NullableCellsComeFromTheHelpers proves the template DECIDES
// each nullable cell by calling the helper, rather than carrying markup that
// happens to read the same as the helper's answer.
//
// It re-parses the embedded templates with both helpers replaced by probes
// returning sentinels, renders the audit page against an entry that carries BOTH
// real values, and looks for the sentinels in the two columns. A template that
// wrote `{{.CommitHash}}` and an inline {{if}} survives the substitution and
// still shows the real hash, which is what the control at the end fails on.
func TestAuditTemplate_NullableCellsComeFromTheHelpers(t *testing.T) {
	funcs := templateFuncs()
	funcs["auditRelatedEntityCell"] = func(*int) auditCell {
		return auditCell{Text: "probe-related-text", Class: "probe-related-class"}
	}
	funcs["auditCommitHashCell"] = func(*string) auditCell {
		return auditCell{Text: "probe-commit-text", Class: "probe-commit-class"}
	}

	tmpl, err := template.New("").Funcs(funcs).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		t.Fatalf("parsing the embedded templates with the probe helpers: %v", err)
	}

	related := auditFixtureTaskID
	hash := auditCommitFixtureHash
	view := auditData{
		Name:       "probe",
		Page:       1,
		TotalPages: 1,
		PageItems:  paginationItems(1, 1),
		Entries: []models.AuditEntry{{
			ID:              1,
			Operation:       string(models.OpSprintAddTask),
			EntityType:      string(models.EntitySprint),
			EntityID:        auditFixtureSprintID,
			RelatedEntityID: &related,
			CommitHash:      &hash,
			PerformedAt:     "2026-08-20T09:01:00Z",
		}},
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "audit.html", view); err != nil {
		t.Fatalf("rendering audit.html with the probe helpers: %v", err)
	}
	region := auditTableRegion(t, buf.String())
	rows := auditRowCells(t, region)
	if len(rows) != 1 {
		t.Fatalf("the probe render produced %d data rows, want 1", len(rows))
	}

	cases := []struct {
		column int
		name   string
		want   string
	}{
		{auditColRelatedEntityID, "Related Entity ID", `<td class="probe-related-class">probe-related-text</td>`},
		{auditColCommit, "Commit", `<td class="probe-commit-class">probe-commit-text</td>`},
	}
	for _, c := range cases {
		if got := rows[0][c.column]; got != c.want {
			t.Errorf("under the probe the %s cell is %q, want %q: neither its text nor its "+
				"class is produced by the helper, so the template decides the presentation "+
				"itself and can drift from the modal's", c.name, got, c.want)
		}
	}

	// Control: the probes replaced the real helpers, so no real value and no real
	// class may survive in the two cells they render. Either would be markup
	// written into the template rather than taken from the helper.
	//
	// The control is scoped to those two cells and not to the whole table: the
	// Performed At column has carried `text-nowrap text-secondary` since before
	// these columns existed, so a table-wide scan for the absent class would fail
	// against a conforming page.
	for _, column := range []int{auditColRelatedEntityID, auditColCommit} {
		for _, leaked := range []string{auditCommitFixtureHash, absentPlaceholder, auditHashClass, auditAbsentClass} {
			if strings.Contains(rows[0][column], leaked) {
				t.Errorf("cell %d still renders %q even though both cell helpers were "+
					"replaced; that value is written into the template", column, leaked)
			}
		}
	}
}

// TestAuditCell_PresenceRule pins the helpers themselves: what each answers for a
// present value, for NULL, and — for the hash — for the empty string the modal's
// own `if (value)` also treats as absent.
func TestAuditCell_PresenceRule(t *testing.T) {
	id := 41
	zero := 0
	hash := auditCommitFixtureHash
	empty := ""

	absent := auditCell{Text: absentPlaceholder, Class: auditAbsentClass}

	relatedCases := []struct {
		name string
		in   *int
		want auditCell
	}{
		{"a counterpart entity", &id, auditCell{Text: "41"}},
		{"no counterpart", nil, absent},
		// The schema admits only positive ids, so a zero is a stored fault. It is
		// shown rather than hidden behind a placeholder that would claim the
		// operation has no counterpart at all.
		{"a stored zero", &zero, auditCell{Text: "0"}},
	}
	for _, c := range relatedCases {
		if got := auditRelatedEntityCell(c.in); got != c.want {
			t.Errorf("auditRelatedEntityCell(%s) = %+v, want %+v", c.name, got, c.want)
		}
	}

	commitCases := []struct {
		name string
		in   *string
		want auditCell
	}{
		{"a stored hash", &hash, auditCell{Text: auditCommitFixtureHash, Class: auditHashClass}},
		{"no hash", nil, absent},
		{"an empty hash", &empty, absent},
	}
	for _, c := range commitCases {
		if got := auditCommitHashCell(c.in); got != c.want {
			t.Errorf("auditCommitHashCell(%s) = %+v, want %+v", c.name, got, c.want)
		}
	}
}

// TestAuditCell_MirrorsTheTaskModalPresentation is what makes "the audit table
// follows the presentation the task detail modal already uses" a checked claim
// rather than a comment.
//
// The modal renders the task's own commit hashes in static/task-modal.js, and its
// convention is three decisions: the placeholder an absent value takes, the class
// set a present hash takes, and the class set the placeholder takes. This test
// reads that file and fails if the Go helpers and the modal ever disagree — in
// either direction, so a change to the modal is caught as readily as a change
// here.
func TestAuditCell_MirrorsTheTaskModalPresentation(t *testing.T) {
	script := stripJSComments(readEmbeddedAsset(t, "static/task-modal.js"))

	// The modal's absent placeholder is its ABSENT constant.
	wantConst := `ABSENT = "` + absentPlaceholder + `"`
	if !strings.Contains(script, wantConst) {
		t.Errorf("the task detail modal does not declare %s; the audit table's placeholder "+
			"and the modal's have drifted apart, so one surface now says \"there is nothing "+
			"here\" differently from the other", wantConst)
	}

	// commitItem is the modal function the audit Commit column reuses. The scan is
	// scoped to its body: `datagrid-content text-secondary` also appears in
	// timestampItem, and a document-wide match would not prove the COMMIT cell
	// takes it.
	body := jsFunctionBody(t, script, "commitItem")
	for _, want := range []struct {
		classes string
		when    string
	}{
		{auditHashClass, "a present commit hash"},
		{auditAbsentClass, "an absent commit hash"},
	} {
		if !strings.Contains(body, `"datagrid-content `+want.classes+`"`) {
			t.Errorf("the modal's commitItem does not present %s with %q; the audit table "+
				"applies that class set, so the two surfaces have drifted", want.when, want.classes)
		}
	}

	// Falsifiability control: the extraction must have found a real function body.
	if !strings.Contains(body, "appendChild") {
		t.Fatalf("the extracted commitItem body carries no appendChild, so the assertions "+
			"above ran against the wrong text: %q", body)
	}
}

// jsFunctionBody returns the source of the named top-level function in script,
// from its `function <name>(` up to the closing brace at the same indentation.
func jsFunctionBody(t *testing.T, script, name string) string {
	t.Helper()
	const indent = "  " // every helper in task-modal.js sits one level inside the IIFE
	start := strings.Index(script, "function "+name+"(")
	if start < 0 {
		t.Fatalf("static/task-modal.js declares no function %s", name)
	}
	rest := script[start:]
	end := strings.Index(rest, "\n"+indent+"}")
	if end < 0 {
		t.Fatalf("cannot find the end of function %s in static/task-modal.js", name)
	}
	return rest[:end]
}

// TestAuditTable_ScrollsInsideItsOwnContainer is the mechanism guard for the
// wide-table requirement: seven columns, one of them a 64-character hash, MUST
// NOT make the document scroll sideways on a narrow viewport — the table scrolls
// inside its own container instead (SPEC/WEB.md § Roadmap Audit Log Page,
// wide-table behaviour; § Responsive and Mobile-First Design, rules 2 and 4).
//
// What is checkable here is the mechanism the measurement depends on, in the
// exact bytes the binary serves: that the table's immediate wrapper is Tabler's
// table-responsive element, that the vendored stylesheet gives that element
// overflow-x rather than leaving the overflow to the page, and that the table
// declares no width of its own that would defeat it. A layout satisfying all of
// that can in principle still measure wrong; one failing any of it cannot measure
// right. The measurement itself is made with a browser against a running server.
func TestAuditTable_ScrollsInsideItsOwnContainer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmapWithNullableAudit(t, "ledger-settlement")
	body := getAudit(t, name, "").Body.String()

	// FindStringIndex rather than a search for the extracted region: it yields the
	// table's span directly, so the markup before it needs no second lookup.
	span := auditTableRe.FindStringIndex(body)
	if span == nil {
		t.Fatal("the rendered audit page carries no <table>; every assertion below would be vacuous")
	}
	region := body[span[0]:span[1]]

	// The element immediately enclosing the table is the scrolling container.
	before := body[:span[0]]
	openAt := strings.LastIndex(before, "<div")
	if openAt < 0 {
		t.Fatal("the audit table has no enclosing element at all")
	}
	if wrapper := before[openAt:]; !strings.Contains(wrapper, `class="table-responsive"`) {
		t.Errorf("the audit table's immediate wrapper is %q, not Tabler's "+
			`<div class="table-responsive">; the seven columns would push the DOCUMENT `+
			"sideways on a narrow viewport instead of scrolling inside the card", strings.TrimSpace(wrapper))
	}

	// The wrapper scrolls because the vendored sheet says so, not because a
	// project override props it up.
	rule := soleCSSRule(t, embeddedSheet(t, "static/vendor/tabler/tabler.min.css"), ".table-responsive")
	if got := cssDeclarations(rule, "overflow-x"); len(got) != 1 || got[0] != "auto" {
		t.Errorf(".table-responsive declares overflow-x: %v, want [auto]; without it the "+
			"container does not scroll and the overflow reaches the document", got)
	}

	// A table that sizes itself defeats the container. Tabler's own .table sets
	// width:100%; the audit table must add nothing beyond the Tabler classes.
	if class := regexp.MustCompile(`<table\b[^>]*class="([^"]*)"`).FindStringSubmatch(region); class != nil {
		for _, c := range strings.Fields(class[1]) {
			switch c {
			case "table", "table-vcenter", "card-table":
			default:
				t.Errorf("the audit table carries the class %q beyond the Tabler table classes; "+
					"a width or layout of its own would defeat the scrolling container", c)
			}
		}
	} else {
		t.Fatal("the audit table carries no class attribute; the assertion above is vacuous")
	}
}

// TestHandleAudit_EmptyLogStillRendersWithTheNewColumns keeps the empty state
// covered now that the row body computes two cells per entry: a roadmap with no
// audit rows must still answer 200 with its empty-state message and its
// "Page 1 of 1" footer, and must render no table at all.
func TestHandleAudit_EmptyLogStillRendersWithTheNewColumns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Seeding zero entries writes no audit row, and creating the roadmap itself
	// writes none either.
	name := seedRoadmapWithAudit(t, "ledger-empty", 0)

	rec := getAudit(t, name, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty audit log status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"No audit entries yet", "Page 1 of 1"} {
		if !strings.Contains(body, want) {
			t.Errorf("the empty audit page does not render %q", want)
		}
	}
	if auditTableRe.MatchString(body) {
		t.Error("the empty audit page renders a table; the empty state replaces it")
	}
}
