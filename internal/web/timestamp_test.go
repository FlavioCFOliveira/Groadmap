package web

import (
	"database/sql"
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// timestampDisplayCase is one row of the display-form table. The rows drive the
// one Go helper every server-rendered surface formats a timestamp through
// (SPEC/WEB.md § Date and Time Display, rule 5; Acceptance Criterion 206).
type timestampDisplayCase struct {
	name    string
	stored  string
	display string // "" when the value is not canonical
}

// timestampDisplayCases covers the canonical form, truncation, the calendar, and
// every class of non-canonical value rule 7 displays unchanged.
var timestampDisplayCases = []timestampDisplayCase{
	{"canonical", "2026-09-28T08:47:32.056Z", "2026-09-28 08:47:32"},
	{"canonical zero milliseconds", "2026-01-05T17:03:09.000Z", "2026-01-05 17:03:09"},
	{"truncated not rounded", "2026-09-28T08:47:59.999Z", "2026-09-28 08:47:59"},
	{"truncated at the end of the day", "2026-09-28T23:59:59.999Z", "2026-09-28 23:59:59"},
	{"truncated at the end of the year", "2026-12-31T23:59:59.999Z", "2026-12-31 23:59:59"},
	{"midnight", "2026-03-01T00:00:00.000Z", "2026-03-01 00:00:00"},
	{"leap day of a leap year", "2024-02-29T12:00:00.000Z", "2024-02-29 12:00:00"},
	{"leap day of a 400-year", "2000-02-29T12:00:00.000Z", "2000-02-29 12:00:00"},
	{"year 0001", "0001-01-01T00:00:00.000Z", "0001-01-01 00:00:00"},
	{"year 9999", "9999-12-31T23:59:59.999Z", "9999-12-31 23:59:59"},
	{"thirtieth of April", "2026-04-30T10:00:00.000Z", "2026-04-30 10:00:00"},

	{"leap day of a common year", "2026-02-29T12:00:00.000Z", ""},
	{"leap day of a century year", "1900-02-29T12:00:00.000Z", ""},
	{"thirty-first of April", "2026-04-31T10:00:00.000Z", ""},
	{"year 0000", "0000-01-01T00:00:00.000Z", ""},
	{"month 00", "2026-00-10T10:00:00.000Z", ""},
	{"month 13", "2026-13-10T10:00:00.000Z", ""},
	{"day 00", "2026-09-00T10:00:00.000Z", ""},
	{"day 32", "2026-01-32T10:00:00.000Z", ""},
	{"hour 24", "2026-09-28T24:00:00.000Z", ""},
	{"minute 60", "2026-09-28T08:60:00.000Z", ""},
	{"second 60", "2026-09-28T08:47:60.000Z", ""},
	{"no milliseconds", "2026-08-10T08:00:00Z", ""},
	{"two millisecond digits", "2026-09-28T08:47:32.05Z", ""},
	{"four millisecond digits", "2026-09-28T08:47:32.0567Z", ""},
	{"no zone", "2026-09-28T08:47:32.056", ""},
	{"numeric offset", "2026-09-28T08:47:32.056+00:00", ""},
	{"lower-case separator", "2026-09-28t08:47:32.056Z", ""},
	{"lower-case zone", "2026-09-28T08:47:32.056z", ""},
	{"space separator", "2026-09-28 08:47:32.056Z", ""},
	{"already the display form", "2026-09-28 08:47:32", ""},
	{"minutes only", "2026-09-28 08:47", ""},
	{"comma before the fraction", "2026-09-28T08:47:32,056Z", ""},
	{"leading space", " 2026-09-28T08:47:32.056Z", ""},
	{"trailing newline", "2026-09-28T08:47:32.056Z\n", ""},
	{"full-width digit", "2026-09-28T08:47:32.05６Z", ""},
	{"arabic-indic digit", "٢026-09-28T08:47:32.056Z", ""},
	{"markup", "<b>yesterday</b>", ""},
	{"attribute breakout", `"><script>alert(1)</script>`, ""},
	{"words", "yesterday", ""},
}

// TestCanonicalTimestampDisplay is the table gate of rules 1 to 4 and 7: the
// display form of a canonical value, truncation, and the rejection of every
// non-canonical value.
func TestCanonicalTimestampDisplay(t *testing.T) {
	for _, tc := range timestampDisplayCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := canonicalTimestampDisplay(tc.stored)
			if want := tc.display != ""; ok != want {
				t.Fatalf("canonicalTimestampDisplay(%q) canonical = %v, want %v", tc.stored, ok, want)
			}
			if got != tc.display {
				t.Fatalf("canonicalTimestampDisplay(%q) = %q, want %q", tc.stored, got, tc.display)
			}
			if ok {
				assertDisplayForm(t, got)
			}
		})
	}
}

// displayFormRe is the shape Acceptance Criterion 207 states for the text of every
// displayed timestamp.
var displayFormRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$`)

// assertDisplayForm pins Acceptance Criterion 207 on one displayed text.
func assertDisplayForm(t *testing.T, text string) {
	t.Helper()
	if !displayFormRe.MatchString(text) || strings.ContainsAny(text, "T.Z") {
		t.Errorf("displayed timestamp %q is not YYYY-MM-DD HH:mm:ss free of T, '.', and Z", text)
	}
}

// TestTimestampHTML pins the markup of the one FuncMap helper: the <time>
// element, the placeholder of an unset value, and the escaped, element-free text
// of a non-canonical value (rules 6 and 7).
func TestTimestampHTML(t *testing.T) {
	stored := "2026-09-28T08:47:32.056Z"
	empty := ""
	markup := "<b>yesterday</b>"
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"canonical string", stored, `<time datetime="2026-09-28T08:47:32.056Z">2026-09-28 08:47:32</time>`},
		{"canonical pointer", &stored, `<time datetime="2026-09-28T08:47:32.056Z">2026-09-28 08:47:32</time>`},
		{"truncation", "2026-09-28T08:47:59.999Z", `<time datetime="2026-09-28T08:47:59.999Z">2026-09-28 08:47:59</time>`},
		{"nil pointer", (*string)(nil), "&mdash;"},
		{"empty pointer", &empty, "&mdash;"},
		{"empty string", "", "&mdash;"},
		{"untyped nil", nil, "&mdash;"},
		{"unsupported type", 42, "&mdash;"},
		{"markup is escaped text", markup, "&lt;b&gt;yesterday&lt;/b&gt;"},
		{"markup pointer is escaped text", &markup, "&lt;b&gt;yesterday&lt;/b&gt;"},
		{"minutes only unchanged", "2026-09-28 08:47", "2026-09-28 08:47"},
		{"no milliseconds unchanged", "2026-08-10T08:00:00Z", "2026-08-10T08:00:00Z"},
		{"attribute breakout", `"><script>`, "&#34;&gt;&lt;script&gt;"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(timestampHTML(tc.in)); got != tc.want {
				t.Fatalf("timestampHTML(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// timeElementRe captures every <time> element of a rendered page: its datetime
// attribute and its whole text content.
var timeElementRe = regexp.MustCompile(`<time datetime="([^"]*)">([^<]*)</time>`)

// assertTimeElements pins Acceptance Criteria 207 and 209 on a rendered region:
// every <time> element carries a canonical stored value and exactly its display
// form. It returns the elements found, attribute to text.
func assertTimeElements(t *testing.T, region string) map[string]string {
	t.Helper()
	found := map[string]string{}
	for _, m := range timeElementRe.FindAllStringSubmatch(region, -1) {
		display, ok := canonicalTimestampDisplay(m[1])
		if !ok {
			t.Errorf("a <time> element carries the non-canonical datetime %q", m[1])
			continue
		}
		if m[2] != display {
			t.Errorf("<time datetime=%q> displays %q, want %q", m[1], m[2], display)
		}
		assertDisplayForm(t, m[2])
		found[m[1]] = m[2]
	}
	if got, want := len(found), strings.Count(region, "<time"); got != want {
		t.Errorf("the region carries %d <time> tags but %d well-formed elements", want, got)
	}
	return found
}

// datagridField returns the content cell of one sprint datagrid field.
func datagridField(t *testing.T, body, title string) string {
	t.Helper()
	re := regexp.MustCompile(`<div class="datagrid-title">` + regexp.QuoteMeta(title) +
		`</div>\s*<div class="datagrid-content text-secondary">(.*?)</div>`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the sprint datagrid has no %s field", title)
	}
	return m[1]
}

// Fixture timestamps of the display-form page tests.
const (
	tsSprintCreated  = "2026-09-28T08:47:32.056Z"
	tsSprintStarted  = "2026-09-29T17:03:09.000Z"
	tsCommentCreated = "2026-09-28T08:47:59.999Z"
	tsCommentUpdated = "2026-09-28T23:59:59.999Z"
)

// seedTimestampSprint creates an OPEN sprint with the fixed timestamps above, one
// comment that is edited, and returns the roadmap's database and the sprint id.
func seedTimestampSprint(t *testing.T, name string) (*db.DB, int) {
	t.Helper()
	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	t.Cleanup(func() { database.Close() }) //nolint:errcheck,gosec // test cleanup

	id, err := seedSprint(database, &models.Sprint{
		Status:      models.SprintPending,
		Title:       "Close the settlement reconciliation window",
		Description: "Reconcile the settlement windows to the cent.",
		CreatedAt:   tsSprintCreated,
	})
	if err != nil {
		t.Fatalf("creating sprint: %v", err)
	}
	if _, err := database.Exec(`UPDATE sprints SET status = ?, started_at = ? WHERE id = ?`,
		models.SprintOpen, tsSprintStarted, id); err != nil {
		t.Fatalf("opening sprint: %v", err)
	}
	commentID := addSprintCommentTo(t, database, id, models.CommentDecision,
		"Settle on the reference feed's closing rate.", tsCommentCreated)
	editSprintCommentBody(t, database, commentID,
		"Settle on the reference feed's closing rate, published at 16:00 UTC.", tsCommentUpdated)
	return database, id
}

// TestSprintPage_TimestampDisplay is the gate for Acceptance Criteria 204, 207,
// 209, and 210 on the sprint page: the datagrid's Created and Started, the unset
// Closed, and the Comments card's created_at and updated_at.
func TestSprintPage_TimestampDisplay(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const name = "settlement-display"
	_, sprintID := seedTimestampSprint(t, name)

	body := servePage(t, buildMux(), "/roadmaps/"+name+"/sprints/"+itoa(sprintID))

	if got, want := datagridField(t, body, "Created"),
		`<time datetime="2026-09-28T08:47:32.056Z">2026-09-28 08:47:32</time>`; got != want {
		t.Errorf("Created renders %q, want %q", got, want)
	}
	if got, want := datagridField(t, body, "Started"),
		`<time datetime="2026-09-29T17:03:09.000Z">2026-09-29 17:03:09</time>`; got != want {
		t.Errorf("Started renders %q, want %q", got, want)
	}
	if got := datagridField(t, body, "Closed"); got != "&mdash;" {
		t.Errorf("the unset Closed renders %q, want the em dash and no <time> element", got)
	}

	card := sprintCommentsCardSlice(t, body)
	if !strings.Contains(card, `<span class="text-secondary"><time datetime="2026-09-28T08:47:59.999Z">`+
		`2026-09-28 08:47:59</time></span>`) {
		t.Errorf("the comment's created_at is not displayed truncated inside its <time> element")
	}
	if !strings.Contains(card, `<span class="text-secondary">edited <time datetime="2026-09-28T23:59:59.999Z">`+
		`2026-09-28 23:59:59</time></span>`) {
		t.Errorf("the edited comment's updated_at is not displayed with the marker outside its <time> element")
	}

	found := assertTimeElements(t, body)
	for _, stored := range []string{tsSprintCreated, tsSprintStarted, tsCommentCreated, tsCommentUpdated} {
		if _, ok := found[stored]; !ok {
			t.Errorf("the page carries no <time> element for %s", stored)
		}
	}
	for _, stored := range []string{tsSprintCreated, tsSprintStarted, tsCommentCreated, tsCommentUpdated} {
		if strings.Count(body, stored) != 1 {
			t.Errorf("the stored value %s appears %d times, want once, as its datetime attribute",
				stored, strings.Count(body, stored))
		}
	}
}

// TestSprintPage_NonCanonicalTimestamps is the gate for Acceptance Criterion 211
// on the sprint page: a set timestamp that is not canonical is displayed as its
// stored text, escaped, with no <time> element, and the page still renders.
func TestSprintPage_NonCanonicalTimestamps(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const name = "settlement-malformed"
	database, sprintID := seedTimestampSprint(t, name)

	if _, err := database.Exec(`UPDATE sprints SET started_at = ?, closed_at = ? WHERE id = ?`,
		"2026-09-28 08:47", "<b>yesterday</b>", sprintID); err != nil {
		t.Fatalf("storing non-canonical sprint timestamps: %v", err)
	}
	if _, err := database.Exec(`UPDATE sprint_comments SET created_at = ?, updated_at = ? WHERE sprint_id = ?`,
		"<i>last week</i>", "2026-09-28 08:47", sprintID); err != nil {
		t.Fatalf("storing non-canonical comment timestamps: %v", err)
	}

	body := servePage(t, buildMux(), "/roadmaps/"+name+"/sprints/"+itoa(sprintID))

	if got := datagridField(t, body, "Started"); got != "2026-09-28 08:47" {
		t.Errorf("the non-canonical Started renders %q, want the stored text unchanged", got)
	}
	if got := datagridField(t, body, "Closed"); got != "&lt;b&gt;yesterday&lt;/b&gt;" {
		t.Errorf("the markup Closed renders %q, want the stored text escaped", got)
	}
	if got := datagridField(t, body, "Created"); !strings.HasPrefix(got, "<time ") {
		t.Errorf("the canonical Created beside them renders %q, want its <time> element", got)
	}
	if strings.Contains(body, "<b>yesterday") || strings.Contains(body, "<i>last week") {
		t.Error("a stored timestamp reached the page as markup")
	}

	card := sprintCommentsCardSlice(t, body)
	if !strings.Contains(card, `<span class="text-secondary">&lt;i&gt;last week&lt;/i&gt;</span>`) {
		t.Error("the comment's non-canonical created_at is not displayed as escaped stored text")
	}
	if !strings.Contains(card, `<span class="text-secondary">edited 2026-09-28 08:47</span>`) {
		t.Error("the comment's non-canonical updated_at is not displayed as stored text beside the marker")
	}
	if !strings.Contains(card, "Settle on the reference feed") {
		t.Error("the Comments card no longer renders the comment body")
	}

	found := assertTimeElements(t, body)
	if len(found) != 1 {
		t.Errorf("the page carries %d <time> elements, want only the canonical Created: %v", len(found), found)
	}
}

// TestAuditPage_PerformedAtDisplay is the gate for Acceptance Criteria 205, 207,
// 209, and 211 on the audit page's Performed At column.
func TestAuditPage_PerformedAtDisplay(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const name = "settlement-audit"
	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	for i, at := range []string{"2026-09-28T08:47:32.056Z", "2026-09-28T08:47:59.999Z", "2026-09-27T09:00:00.000Z"} {
		if err := database.WithTransaction(func(tx *sql.Tx) error {
			return db.LogAuditTx(tx, models.OpTaskCreate, models.EntityTask, i+1, at)
		}); err != nil {
			t.Fatalf("seeding audit row %d: %v", i, err)
		}
	}
	if _, err := database.Exec(`UPDATE audit SET performed_at = ? WHERE entity_id = 3`,
		"<b>yesterday</b>"); err != nil {
		t.Fatalf("storing a non-canonical performed_at: %v", err)
	}

	body := servePage(t, buildMux(), "/roadmaps/"+name+"/audit")
	rows := auditRowCells(t, auditTableRegion(t, body))
	if len(rows) != 3 {
		t.Fatalf("the audit table has %d rows, want 3", len(rows))
	}

	cellText := regexp.MustCompile(`(?s)^<td\b[^>]*>(.*)</td>$`)
	performedAt := func(row []string) string {
		m := cellText.FindStringSubmatch(row[auditColPerformedAt])
		if m == nil {
			t.Fatalf("unparsable Performed At cell %q", row[auditColPerformedAt])
		}
		return m[1]
	}

	// The rows stay ordered by the STORED values, performed_at descending: the
	// display form plays no part in the order (rule 9, Ordering).
	want := []string{
		"&lt;b&gt;yesterday&lt;/b&gt;",
		`<time datetime="2026-09-28T08:47:59.999Z">2026-09-28 08:47:59</time>`,
		`<time datetime="2026-09-28T08:47:32.056Z">2026-09-28 08:47:32</time>`,
	}
	for i, row := range rows {
		if got := performedAt(row); got != want[i] {
			t.Errorf("row %d Performed At renders %q, want %q", i, got, want[i])
		}
	}
	if found := assertTimeElements(t, auditTableRegion(t, body)); len(found) != 2 {
		t.Errorf("the audit table carries %d <time> elements, want 2", len(found))
	}
}

// TestTaskPage_TimestampsUseTheDisplayFormOnTheServer is the gate for Acceptance
// Criteria 206, 207, and 209 on the task page: the Details card shows the four
// lifecycle timestamps, and each Comments card entry its created_at and its
// updated_at, in the display form produced on the server by the one Go helper,
// each inside a <time> element whose datetime attribute carries the stored value
// byte for byte; and the stored values themselves are untouched (Acceptance
// Criterion 208 — the CLI half is gated end to end in tests/).
func TestTaskPage_TimestampsUseTheDisplayFormOnTheServer(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const name = "settlement-json"
	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	taskID, err := seedTask(database, seededTask("2026-09-28T08:47:32.056Z",
		"Publish the residual per settlement window"))
	if err != nil {
		t.Fatalf("creating task: %v", err)
	}
	if _, err := database.Exec(
		`UPDATE tasks SET status = ?, started_at = ?, tested_at = ?, closed_at = ? WHERE id = ?`,
		models.StatusCompleted, "2026-09-28T09:00:00.000Z", "2026-09-28T23:59:59.999Z",
		"2026-09-29T00:00:00.001Z", taskID); err != nil {
		t.Fatalf("completing task: %v", err)
	}
	commentID := addTaskCommentTo(t, database, taskID, models.CommentTest,
		"The residual is zero on every window.", "2026-09-28T08:47:59.999Z")
	editTaskCommentBody(t, database, commentID, "The residual is zero on every window of the quarter.",
		"2026-09-28T10:15:30.500Z")

	body := servePage(t, buildMux(), "/roadmaps/"+name+"/tasks/"+itoa(taskID))
	details := detailsCardSlice(t, body)
	for label, stored := range map[string]string{
		"Created": "2026-09-28T08:47:32.056Z",
		"Started": "2026-09-28T09:00:00.000Z",
		"Tested":  "2026-09-28T23:59:59.999Z",
		"Closed":  "2026-09-29T00:00:00.001Z",
	} {
		display, _ := canonicalTimestampDisplay(stored)
		want := `<div class="datagrid-content text-secondary">` + string(timestampHTML(stored)) + `</div>`
		if got := datagridContent(t, details, label); got != want {
			t.Errorf("%s = %s, want %s", label, got, want)
		}
		if !strings.Contains(want, `<time datetime="`+stored+`">`+display+`</time>`) {
			t.Errorf("%s: the helper does not produce the time element for %s", label, stored)
		}
	}
	card := taskCommentsCardSlice(t, body)
	for _, want := range []string{
		`<span class="text-secondary"><time datetime="2026-09-28T08:47:59.999Z">2026-09-28 08:47:59</time></span>`,
		`<span class="text-secondary">edited <time datetime="2026-09-28T10:15:30.500Z">2026-09-28 10:15:30</time></span>`,
	} {
		if !strings.Contains(card, want) {
			t.Errorf("the Comments card does not show %s", want)
		}
	}
	// Every <time> of the page is well formed and displays the display form of its
	// own stored value, which carries no T, no fraction, and no Z (Acceptance
	// Criterion 207): six timestamps in all.
	if found := assertTimeElements(t, regionBetween(t, body, `<main class="page-body">`, "</main>")); len(found) != 6 {
		t.Errorf("the task page carries %d <time> elements, want 6", len(found))
	}

	stored := storedTask(t, name, taskID)
	if stored.CreatedAt != "2026-09-28T08:47:32.056Z" || derefString(stored.ClosedAt) != "2026-09-29T00:00:00.001Z" {
		t.Errorf("the stored timestamps changed: created %q closed %q", stored.CreatedAt, derefString(stored.ClosedAt))
	}
}

// TestTimestamps_NoScriptFormatsATimestamp pins the other half of Date and Time
// Display, rule 5: every surface the rule governs is rendered on the server, so
// no script the interface serves formats a timestamp or builds a <time> element.
func TestTimestamps_NoScriptFormatsATimestamp(t *testing.T) {
	for _, path := range []string{"static/task-search.js", "static/sprint-board.js", "static/graph.js"} {
		script := stripJSComments(readEmbeddedAsset(t, path))
		for _, bad := range []string{"formatTimestamp", `createElement("time")`, `setAttribute("datetime"`} {
			if strings.Contains(script, bad) {
				t.Errorf("%s carries %q; no script formats a timestamp", path, bad)
			}
		}
	}
}
