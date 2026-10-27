package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for the Roadmap Sprint Page's Sprint progress card: the
// sprint summary row it shares with the Sprint details card, the card's markup,
// its legend and accessible label, its chart data block, its page script, and
// the pages the chart assets load on (SPEC/WEB.md § Sprint Detail Sub-Template,
// rules 2 and 7; § Dashboard Chart Library; SPEC/DATA_FORMATS.md § Sprint
// Progress Chart Data; Acceptance Criteria 287 to 294).
//
// The criteria's in-browser halves — the side-by-side layout at 1440px, the
// stacking at 375px, and the centre text an arc shows while hovered — are pinned
// here on what the server serves and on the configuration the page script
// builds; the script is the only code that runs in the browser.

// sprintProgressDataTag is the opening tag of the sprint page's chart data block,
// the one <script> element of the page that carries no src (Acceptance Criterion
// 293).
const sprintProgressDataTag = `<script type="application/json" id="sprint-progress-data">`

// sprintProgressHostileTitle is a sprint title that would close the data block
// if any user text reached it unescaped (Acceptance Criterion 293).
const sprintProgressHostileTitle = `</script><script>alert(1)</script>`

// ==================== FIXTURE ====================

// sprintProgressFixture names the sprints seedSprintProgressFixture creates.
type sprintProgressFixture struct {
	name string
	// twelve holds 12 member tasks: 5 SPRINT, 2 DOING, 1 TESTING, 4 COMPLETED
	// (Acceptance Criterion 288).
	twelve int
	// one holds a single SPRINT member task (Acceptance Criterion 291).
	one int
	// thirds holds 3 member tasks, 2 of them COMPLETED: 66%, never 67%.
	thirds int
	// done holds 2 member tasks, both COMPLETED: 100%.
	done int
	// empty holds no member task (Acceptance Criterion 292).
	empty int
	// hostile is titled sprintProgressHostileTitle and holds one member task.
	hostile int
}

func (f *sprintProgressFixture) path(id int) string {
	return "/roadmaps/" + f.name + "/sprints/" + itoa(id)
}

// seedSprintProgressFixture builds one roadmap holding the six sprints the
// Sprint progress card's criteria name.
func seedSprintProgressFixture(t *testing.T, name string) sprintProgressFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	f := sprintProgressFixture{name: name}
	f.twelve = seedProgressSprint(t, database, "Cut the settlement ledger over to the new queue",
		[]models.TaskStatus{
			models.StatusSprint, models.StatusDoing, models.StatusSprint, models.StatusCompleted,
			models.StatusSprint, models.StatusTesting, models.StatusCompleted, models.StatusSprint,
			models.StatusDoing, models.StatusCompleted, models.StatusSprint, models.StatusCompleted,
		})
	f.one = seedProgressSprint(t, database, "Document the acquirer onboarding checklist",
		[]models.TaskStatus{models.StatusSprint})
	f.thirds = seedProgressSprint(t, database, "Retire the legacy reconciliation cron jobs",
		[]models.TaskStatus{models.StatusCompleted, models.StatusSprint, models.StatusCompleted})
	f.done = seedProgressSprint(t, database, "Rotate the settlement service credentials",
		[]models.TaskStatus{models.StatusCompleted, models.StatusCompleted})
	f.empty = seedProgressSprint(t, database, "Plan the next settlement window review", nil)
	f.hostile = seedProgressSprint(t, database, sprintProgressHostileTitle,
		[]models.TaskStatus{models.StatusDoing})
	return f
}

// seedProgressSprint creates a sprint whose member tasks take the given
// statuses, in sprint_tasks position order, and returns the sprint id. The sprint
// stays PENDING: a roadmap holds at most one OPEN sprint, and the card is
// rendered for a sprint in any status.
func seedProgressSprint(t *testing.T, database *db.DB, title string, statuses []models.TaskStatus) int {
	t.Helper()

	sprintID := newSprint(t, database, title, "Delivers the settlement work the operations team planned for this window.")
	ids := make([]int, len(statuses))
	for i := range statuses {
		id, err := seedTask(database, &models.Task{
			Title:                  "Settlement work item " + itoa(i+1) + " of " + title,
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			Priority:               5,
			Severity:               4,
			FunctionalRequirements: "The settlement pipeline handles the case end to end.",
			TechnicalRequirements:  "Implemented in the settlement service, read-only on the web side.",
			AcceptanceCriteria:     "The nightly reconciliation report shows no residual for the case.",
			CreatedAt:              "2026-05-01T09:00:00Z",
		})
		if err != nil {
			t.Fatalf("creating a member task of %q: %v", title, err)
		}
		ids[i] = id
	}
	if len(ids) > 0 {
		if err := database.AddTasksToSprint(context.Background(), sprintID, ids); err != nil {
			t.Fatalf("adding the member tasks to sprint %q: %v", title, err)
		}
	}
	for i, status := range statuses {
		if status != models.StatusSprint {
			forceTaskLifecycle(t, database, ids[i:i+1], status)
		}
	}
	return sprintID
}

// serveSprintProgress serves one sprint page and returns the recorder, failing
// unless it answers 200.
func serveSprintProgress(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200", path, rec.Code)
	}
	return rec
}

// ==================== MARKUP HELPERS ====================

// elementTagRe matches one start or end tag, capturing the slash of an end tag
// and the element name; void elements (voidElements) have no end tag.
var elementTagRe = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9]*)\b[^>]*>`)

// elementAt returns the outer markup of the element whose opening tag is the
// first occurrence of open in body, up to and including its matching end tag.
func elementAt(t *testing.T, body, open string) string {
	t.Helper()

	start := strings.Index(body, open)
	if start < 0 {
		t.Fatalf("the page carries no %q", open)
	}
	rest := body[start:]
	depth := 0
	for _, m := range elementTagRe.FindAllStringSubmatchIndex(rest, -1) {
		name := strings.ToLower(rest[m[4]:m[5]])
		if voidElements[name] {
			continue
		}
		if m[3] > m[2] {
			depth--
		} else {
			depth++
		}
		if depth == 0 {
			return rest[:m[1]]
		}
	}
	t.Fatalf("the element %q is never closed", open)
	return ""
}

// childElements returns the outer markup of each direct child element of the
// element outer holds, in document order. Text between the children is ignored.
func childElements(t *testing.T, outer string) []string {
	t.Helper()

	tags := elementTagRe.FindAllStringSubmatchIndex(outer, -1)
	if len(tags) < 2 {
		t.Fatalf("not an element with an end tag: %q", outer)
	}
	inner := outer[tags[0][1]:tags[len(tags)-1][0]]
	var children []string
	depth, from := 0, 0
	for _, m := range elementTagRe.FindAllStringSubmatchIndex(inner, -1) {
		name := strings.ToLower(inner[m[4]:m[5]])
		closing := m[3] > m[2]
		if voidElements[name] {
			if depth == 0 {
				children = append(children, inner[m[0]:m[1]])
			}
			continue
		}
		if closing {
			depth--
			if depth == 0 {
				children = append(children, inner[from:m[1]])
			}
			continue
		}
		if depth == 0 {
			from = m[0]
		}
		depth++
	}
	return children
}

// innerText returns the text of markup with every tag removed and the
// surrounding whitespace trimmed.
func innerText(markup string) string {
	return strings.TrimSpace(elementTagRe.ReplaceAllString(markup, ""))
}

// sprintSummaryRow returns the sprint page's sprint summary row.
func sprintSummaryRow(t *testing.T, body string) string {
	t.Helper()
	return elementAt(t, boardRegion(t, body), `<div class="row row-deck row-cards mb-3">`)
}

// sprintProgressCard returns the Sprint progress card: the one child of the
// summary row's second column.
func sprintProgressCard(t *testing.T, body string) string {
	t.Helper()

	columns := childElements(t, sprintSummaryRow(t, body))
	if len(columns) != 2 {
		t.Fatalf("the sprint summary row holds %d columns, want 2", len(columns))
	}
	cards := childElements(t, columns[1])
	if len(cards) != 1 {
		t.Fatalf("the Sprint progress column holds %d elements, want the one card", len(cards))
	}
	return cards[0]
}

// sprintProgressBody returns the four children of the Sprint progress card's
// card-body, failing unless the card holds exactly one card-body and nothing else.
func sprintProgressBody(t *testing.T, body string) []string {
	t.Helper()

	card := sprintProgressCard(t, body)
	if !strings.HasPrefix(card, `<div class="card">`) {
		t.Fatalf("the Sprint progress card opens with %q, want <div class=\"card\">", card[:min(40, len(card))])
	}
	parts := childElements(t, card)
	if len(parts) != 1 || !strings.HasPrefix(parts[0], `<div class="card-body">`) {
		t.Fatalf("the Sprint progress card holds %d elements, want exactly one card-body and no card-header", len(parts))
	}
	return childElements(t, parts[0])
}

// sprintProgressData returns the content of the sprint page's one chart data
// block.
func sprintProgressData(t *testing.T, body string) string {
	t.Helper()

	if n := strings.Count(body, sprintProgressDataTag); n != 1 {
		t.Fatalf("the sprint page carries %d chart data blocks, want 1", n)
	}
	return regionBetween(t, body, sprintProgressDataTag, "</script>")[len(sprintProgressDataTag):]
}

// decodeSprintProgressData parses a chart data block, failing unless it carries
// exactly the members of SPEC/DATA_FORMATS.md § Sprint Progress Chart Data, in
// order, each array holding three JSON integers.
func decodeSprintProgressData(t *testing.T, content string) sprintProgressColumns {
	t.Helper()

	// The member order, read token by token: encoding/json's struct decoding
	// does not see it.
	dec := json.NewDecoder(strings.NewReader(content))
	var keys []string
	// One frame per open object or array: whether it is an object, and, for an
	// object, whether its next token is a member name.
	type frame struct{ object, expectKey bool }
	var stack []frame
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		top := len(stack) - 1
		if d, ok := tok.(json.Delim); ok && (d == '}' || d == ']') {
			stack = stack[:top]
			continue
		}
		if top >= 0 && stack[top].object {
			if stack[top].expectKey {
				keys = append(keys, tok.(string))
				stack[top].expectKey = false
				continue
			}
			stack[top].expectKey = true
		}
		if d, ok := tok.(json.Delim); ok {
			stack = append(stack, frame{object: d == '{', expectKey: d == '{'})
		}
	}
	if got := strings.Join(keys, ","); got != "tasks_by_column,columns,counts,percents" {
		t.Errorf("the chart data block's members are %s, want tasks_by_column,columns,counts,percents", got)
	}

	var data struct {
		TasksByColumn *sprintProgressColumns `json:"tasks_by_column"`
	}
	strict := json.NewDecoder(bytes.NewReader([]byte(content)))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&data); err != nil {
		t.Fatalf("the chart data block does not parse as the published shape: %v\n%s", err, content)
	}
	if data.TasksByColumn == nil {
		t.Fatalf("the chart data block carries no tasks_by_column: %s", content)
	}
	got := *data.TasksByColumn
	if len(got.Columns) != 3 || len(got.Counts) != 3 || len(got.Percents) != 3 {
		t.Errorf("the chart data block's arrays hold %d, %d, and %d entries, want 3 each",
			len(got.Columns), len(got.Counts), len(got.Percents))
	}
	if regexp.MustCompile(`[0-9][.eE]`).MatchString(content) {
		t.Errorf("the chart data block carries a number that is not an integer: %s", content)
	}
	return got
}

// boardColumnCounts returns the data-task-count of each member-tasks board
// column, in column order.
func boardColumnCounts(t *testing.T, body string) []string {
	t.Helper()
	matches := regexp.MustCompile(`data-role="task-board-column" data-task-count="([0-9]+)"`).
		FindAllStringSubmatch(memberBoardRegion(t, body), -1)
	counts := make([]string, 0, len(matches))
	for _, m := range matches {
		counts = append(counts, m[1])
	}
	return counts
}

// ==================== AC 287: ONE ROW, TWO CARDS ====================

// TestSprintProgress_SharesRowWithSprintDetails is Acceptance Criterion 287: the
// sprint presentation opens with exactly one sprint summary row holding exactly
// two columns, the Sprint details card's and then the Sprint progress card's,
// followed by the member-tasks board and then the Comments card.
func TestSprintProgress_SharesRowWithSprintDetails(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintProgressFixture(t, "settlement-cutover")

	for _, id := range []int{f.twelve, f.empty} {
		body := serveSprintProgress(t, f.path(id)).Body.String()
		main := boardRegion(t, body)

		const rowTag = `<div class="row row-deck row-cards mb-3">`
		if n := strings.Count(main, rowTag); n != 1 {
			t.Fatalf("sprint %d: the page body carries %d sprint summary rows, want 1", id, n)
		}
		container := `<div class="container-xl">`
		first := strings.TrimSpace(main[strings.Index(main, container)+len(container):])
		if !strings.HasPrefix(first, rowTag) {
			t.Errorf("sprint %d: the sprint presentation does not open with the summary row: %q", id, first[:min(80, len(first))])
		}

		columns := childElements(t, sprintSummaryRow(t, body))
		if len(columns) != 2 {
			t.Fatalf("sprint %d: the summary row holds %d columns, want 2", id, len(columns))
		}
		if !strings.HasPrefix(columns[0], `<div class="col-12 col-lg">`) ||
			!strings.Contains(columns[0], `<h3 class="card-title">Sprint details</h3>`) ||
			strings.Contains(columns[0], `chart-sprint-progress`) {
			t.Errorf("sprint %d: the first column is not the Sprint details card's: %q", id, columns[0][:min(120, len(columns[0]))])
		}
		if !strings.HasPrefix(columns[1], `<div class="col-12 col-sm-6 col-lg-3">`) ||
			!strings.Contains(columns[1], `id="chart-sprint-progress"`) ||
			strings.Contains(columns[1], "Sprint details") {
			t.Errorf("sprint %d: the second column is not the Sprint progress card's: %q", id, columns[1][:min(120, len(columns[1]))])
		}

		assertOrdered(t, "sprint presentation", main, []string{
			rowTag, `<div class="col-12 col-lg">`, "Sprint details",
			`<div class="col-12 col-sm-6 col-lg-3">`, `id="chart-sprint-progress"`,
			`data-role="task-board">`, `<h3 class="card-title">Comments`,
		})
	}
}

// ==================== AC 288: THE TOTAL ====================

// TestSprintProgress_CardStatesTheTotal is Acceptance Criterion 288.
func TestSprintProgress_CardStatesTheTotal(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintProgressFixture(t, "settlement-cutover")
	body := serveSprintProgress(t, f.path(f.twelve)).Body.String()

	children := sprintProgressBody(t, body)
	if len(children) != 3 {
		t.Fatalf("the Sprint progress card-body holds %d children, want 3: %q", len(children), children)
	}
	if children[0] != `<div class="subheader">12 Tasks</div>` {
		t.Errorf("first child = %q, want the 12 Tasks subheader", children[0])
	}
	if !regexp.MustCompile(`^<div id="chart-sprint-progress" class="position-relative" role="img" aria-label="[^"]+"></div>$`).
		MatchString(children[1]) {
		t.Errorf("second child = %q, want the empty gauge container with an aria-label", children[1])
	}
	if !strings.HasPrefix(children[2], `<div class="legend-list">`) {
		t.Errorf("third child = %q, want the legend list", children[2])
	}
	card := sprintProgressCard(t, body)
	if regexp.MustCompile(`class="(?:[^"]* )?h1(?: [^"]*)?"`).MatchString(card) {
		t.Errorf("an element of the Sprint progress card carries the class h1: %q", card)
	}
	for _, trend := range []string{"ti-trending", "<i ", "text-green", "text-red", "card-header", "align-items-baseline"} {
		if strings.Contains(card, trend) {
			t.Errorf("the Sprint progress card carries %q", trend)
		}
	}

	if got := strings.Join(boardColumnCounts(t, body), ","); got != "5,3,4" {
		t.Errorf("the board's column counts are %s, want 5,3,4 (summing to the card's 12)", got)
	}

	one := sprintProgressBody(t, serveSprintProgress(t, f.path(f.one)).Body.String())
	if len(one) != 3 || one[0] != `<div class="subheader">1 Task</div>` {
		t.Errorf("the one-task sprint's card-body = %q, want three children opening with the 1 Task subheader", one)
	}
}

// ==================== AC 289: THE SERIES ====================

// TestSprintProgress_DataBlockCarriesTheThreeSeries is the server half of
// Acceptance Criterion 289: one chart data block of the published shape, with
// the columns, counts, and floor percentages of the 12-task sprint.
func TestSprintProgress_DataBlockCarriesTheThreeSeries(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintProgressFixture(t, "settlement-cutover")
	body := serveSprintProgress(t, f.path(f.twelve)).Body.String()

	got := decodeSprintProgressData(t, sprintProgressData(t, body))
	if strings.Join(got.Columns, ",") != "WAITING,DOING,CLOSED" {
		t.Errorf("columns = %v, want WAITING, DOING, CLOSED", got.Columns)
	}
	if !slices.Equal(got.Counts, []int{5, 3, 4}) {
		t.Errorf("counts = %v, want [5 3 4]", got.Counts)
	}
	if !slices.Equal(got.Percents, []int{41, 25, 33}) {
		t.Errorf("percents = %v, want [41 25 33]", got.Percents)
	}
}

// TestSprintProgress_ScriptConfiguresTheTablerGauge is the script half of
// Acceptance Criteria 289 and 290: the one configuration static/sprint-progress.js
// passes to ApexCharts carries the options every chart carries, the gauge options
// of SPEC/WEB.md § Dashboard Chart Library, rule 4, the series and labels of the
// data block in its order, the three column colours as Tabler custom
// properties, a total formatter writing the completed percentage, a value
// formatter writing the hovered series' count, and no theme.
func TestSprintProgress_ScriptConfiguresTheTablerGauge(t *testing.T) {
	js := stripJSComments(readEmbeddedAsset(t, "static/sprint-progress.js"))
	norm := strings.Join(strings.Fields(js), " ")

	for _, want := range []string{
		// Every chart.
		`fontFamily: "inherit", parentHeightOffset: 0, toolbar: { show: false }, animations: { enabled: false },`,
		// The gauge.
		`type: "radialBar", height: GAUGE_HEIGHT,`, `var GAUGE_HEIGHT = 200;`,
		`startAngle: -120, endAngle: 120, hollow: { margin: 15, size: "70%" },`,
		`name: { show: true, offsetY: -20, fontSize: "14px", fontFamily: "inherit", fontWeight: 400, color: "var(--tblr-secondary)" }`,
		`value: { show: true, fontSize: "30px", fontWeight: 700, fontFamily: "inherit", offsetY: 0, color: "var(--tblr-body-color)",`,
		`total: { show: true, label: COMPLETED_LABEL, formatter: function () { return completed + "%"; } }`,
		`var COMPLETED_LABEL = "COMPLETED";`,
		`stroke: { lineCap: "round" },`,
		// The series, in the data block's order, coloured by column.
		`labels: columns.slice(), series: byColumn.percents.slice(), colors: colours`,
		`colours.push(COLUMN_COLOURS[columns[i]]);`,
		`WAITING: "var(--tblr-cyan)", DOING: "var(--tblr-blue)", CLOSED: "var(--tblr-green)"`,
		// The centre: the CLOSED percentage, and a hovered arc's count.
		`var completed = byColumn.percents[columns.indexOf(COMPLETED_COLUMN)];`, `var COMPLETED_COLUMN = "CLOSED";`,
		`dataPointMouseEnter: function (event, chartContext, opts) { hovered = opts.dataPointIndex; }`,
		`return String(counts[hovered]);`,
		// The data block is the only data source.
		`document.getElementById("sprint-progress-data")`, `document.getElementById("chart-sprint-progress")`,
		`JSON.parse(block.textContent)`, `new ApexCharts(container, gaugeOptions(data.tasks_by_column)).render();`,
	} {
		if !strings.Contains(norm, want) {
			t.Errorf("sprint-progress.js lacks %q", want)
		}
	}
	if n := strings.Count(norm, "new ApexCharts("); n != 1 {
		t.Errorf("sprint-progress.js constructs ApexCharts at %d sites, want 1", n)
	}
	if n := strings.Count(norm, "chart: {"); n != 1 {
		t.Errorf("sprint-progress.js builds %d chart configurations, want the gauge's alone", n)
	}
	if regexp.MustCompile(`["']#[0-9a-fA-F]{3,8}["']|rgba?\(|hsla?\(`).MatchString(js) {
		t.Errorf("sprint-progress.js writes a literal colour value")
	}
	for _, forbidden := range []string{
		"theme", "fetch(", "XMLHttpRequest", "localStorage", "sessionStorage", "indexedDB", "document.cookie",
		"innerHTML", `setAttribute("style"`, ".style.", "task-board", "sprint-board", "dashboard-data",
	} {
		if strings.Contains(js, forbidden) {
			t.Errorf("sprint-progress.js uses %q", forbidden)
		}
	}
}

// ==================== AC 290: THE CENTRE ====================

// TestSprintProgress_CompletedPercentIsRoundedDown is the server half of
// Acceptance Criterion 290: the CLOSED percentage the gauge's centre shows is
// rounded down — 33% for 4 of 12, 66% (not 67%) for 2 of 3, 100% for all — and the
// group count a hovered arc shows is carried beside it.
func TestSprintProgress_CompletedPercentIsRoundedDown(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintProgressFixture(t, "settlement-cutover")

	for _, c := range []struct {
		id       int
		percents []int
		counts   []int
		label    string
	}{
		{f.twelve, []int{41, 25, 33}, []int{5, 3, 4}, "12 tasks: 5 waiting, 3 doing, 4 closed; 33% completed"},
		{f.thirds, []int{33, 0, 66}, []int{1, 0, 2}, "3 tasks: 1 waiting, 0 doing, 2 closed; 66% completed"},
		{f.done, []int{0, 0, 100}, []int{0, 0, 2}, "2 tasks: 0 waiting, 0 doing, 2 closed; 100% completed"},
	} {
		body := serveSprintProgress(t, f.path(c.id)).Body.String()
		got := decodeSprintProgressData(t, sprintProgressData(t, body))
		if !slices.Equal(got.Percents, c.percents) || !slices.Equal(got.Counts, c.counts) {
			t.Errorf("sprint %d: percents %v counts %v, want %v and %v", c.id, got.Percents, got.Counts, c.percents, c.counts)
		}
		if !strings.Contains(sprintProgressCard(t, body), `aria-label="`+c.label+`"`) {
			t.Errorf("sprint %d: the gauge container does not carry aria-label %q", c.id, c.label)
		}
	}
}

// TestSprintProgress_NewSprintProgress pins the derivation the card renders: the
// summary counters per column, the floor percentages, the completed percentage,
// the colours derived from each column's canonical status badge, and the
// singular "1 task" of the label and "1 Task" of the subheader.
func TestSprintProgress_NewSprintProgress(t *testing.T) {
	for _, c := range []struct {
		summary   models.SprintSummary
		percents  []int
		done      int
		label     string
		subheader string
	}{
		{models.SprintSummary{TotalTasks: 12, Pending: 5, InProgress: 3, Completed: 4}, []int{41, 25, 33}, 33,
			"12 tasks: 5 waiting, 3 doing, 4 closed; 33% completed", "12 Tasks"},
		{models.SprintSummary{TotalTasks: 3, Pending: 1, Completed: 2}, []int{33, 0, 66}, 66,
			"3 tasks: 1 waiting, 0 doing, 2 closed; 66% completed", "3 Tasks"},
		{models.SprintSummary{TotalTasks: 1, Pending: 1}, []int{100, 0, 0}, 0,
			"1 task: 1 waiting, 0 doing, 0 closed; 0% completed", "1 Task"},
		{models.SprintSummary{}, []int{0, 0, 0}, 0,
			"0 tasks: 0 waiting, 0 doing, 0 closed; 0% completed", "0 Tasks"},
	} {
		p := newSprintProgress(c.summary)
		if !slices.Equal(p.Chart.TasksByColumn.Percents, c.percents) {
			t.Errorf("%+v: percents = %v, want %v", c.summary, p.Chart.TasksByColumn.Percents, c.percents)
		}
		want := []int{c.summary.Pending, c.summary.InProgress, c.summary.Completed}
		if !slices.Equal(p.Chart.TasksByColumn.Counts, want) {
			t.Errorf("%+v: counts = %v, want %v", c.summary, p.Chart.TasksByColumn.Counts, want)
		}
		if p.CompletedPercent != c.done || p.Label != c.label || p.Total != c.summary.TotalTasks {
			t.Errorf("%+v: completed %d label %q total %d, want %d, %q, %d",
				c.summary, p.CompletedPercent, p.Label, p.Total, c.done, c.label, c.summary.TotalTasks)
		}
		if p.Subheader != c.subheader {
			t.Errorf("%+v: subheader %q, want %q", c.summary, p.Subheader, c.subheader)
		}
		for i, g := range p.Groups {
			if g.Heading != sprintBoardColumns[i].heading || g.Count != want[i] {
				t.Errorf("%+v: group %d = %+v", c.summary, i, g)
			}
			// The colour is the column count badge's: bg-<colour>-lt.
			if badge := taskStatusBadge(sprintBoardColumns[i].canonical); badge != "bg-"+g.Colour+"-lt" {
				t.Errorf("group %s takes colour %q, but its column badge is %q", g.Heading, g.Colour, badge)
			}
		}
		var colours []string
		for _, g := range p.Groups {
			colours = append(colours, g.Colour)
		}
		if strings.Join(colours, ",") != "cyan,blue,green" {
			t.Errorf("the group colours are %v, want cyan, blue, green", colours)
		}
	}
}

// ==================== AC 291: LEGEND AND LABEL ====================

// TestSprintProgress_LegendAndLabelStateTheCounts is Acceptance Criterion 291.
func TestSprintProgress_LegendAndLabelStateTheCounts(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintProgressFixture(t, "settlement-cutover")

	body := serveSprintProgress(t, f.path(f.twelve)).Body.String()
	children := sprintProgressBody(t, body)
	entries := childElements(t, children[2])
	want := []string{
		`<span class="legend legend-cyan"><span class="legend-dot"></span>WAITING<span class="legend-value">5</span></span>`,
		`<span class="legend legend-blue"><span class="legend-dot"></span>DOING<span class="legend-value">3</span></span>`,
		`<span class="legend legend-green"><span class="legend-dot"></span>CLOSED<span class="legend-value">4</span></span>`,
	}
	if strings.Join(entries, "\n") != strings.Join(want, "\n") {
		t.Errorf("the legend holds\n%s\nwant\n%s", strings.Join(entries, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(children[1], `aria-label="12 tasks: 5 waiting, 3 doing, 4 closed; 33% completed"`) {
		t.Errorf("the gauge container = %q", children[1])
	}

	one := serveSprintProgress(t, f.path(f.one)).Body.String()
	if !strings.Contains(sprintProgressCard(t, one), `aria-label="1 task: 1 waiting, 0 doing, 0 closed; 0% completed"`) {
		t.Errorf("the one-task sprint's gauge container does not carry the singular label")
	}
}

// ==================== AC 292: NO MEMBER TASK ====================

// TestSprintProgress_EmptySprintRendersTheCard is Acceptance Criterion 292.
func TestSprintProgress_EmptySprintRendersTheCard(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintProgressFixture(t, "settlement-cutover")
	body := serveSprintProgress(t, f.path(f.empty)).Body.String()

	children := sprintProgressBody(t, body)
	if len(children) != 3 {
		t.Fatalf("the card-body holds %d children, want 3", len(children))
	}
	if children[0] != `<div class="subheader">0 Tasks</div>` {
		t.Errorf("the card's subheader = %q, want 0 Tasks", children[0])
	}
	if children[1] != `<div id="chart-sprint-progress" class="position-relative" role="img" aria-label="0 tasks: 0 waiting, 0 doing, 0 closed; 0% completed"></div>` {
		t.Errorf("the gauge container = %q", children[1])
	}
	entries := childElements(t, children[2])
	if len(entries) != 3 {
		t.Fatalf("the legend holds %d entries, want 3", len(entries))
	}
	for i, e := range entries {
		if !strings.HasSuffix(e, `<span class="legend-value">0</span></span>`) {
			t.Errorf("legend entry %d = %q, want a count of 0", i, e)
		}
	}
	got := decodeSprintProgressData(t, sprintProgressData(t, body))
	if !slices.Equal(got.Counts, []int{0, 0, 0}) || !slices.Equal(got.Percents, []int{0, 0, 0}) {
		t.Errorf("the empty sprint's data block carries counts %v and percents %v, want all 0", got.Counts, got.Percents)
	}
}

// ==================== AC 293: NO JAVASCRIPT, SAME POLICY ====================

// TestSprintProgress_NumbersNeedNoScriptAndCSPHolds is the server half of
// Acceptance Criterion 293: the card's subheader with the total, and the legend counts, are in
// the served markup; every <script> other than the data block has a src and no
// content; the card carries no style attribute; the policy is unchanged; and a
// hostile sprint title never reaches the data block.
func TestSprintProgress_NumbersNeedNoScriptAndCSPHolds(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintProgressFixture(t, "settlement-cutover")

	rec := serveSprintProgress(t, f.path(f.twelve))
	body := rec.Body.String()
	if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Errorf("Content-Security-Policy = %q, want %q", got, contentSecurityPolicy)
	}
	card := sprintProgressCard(t, body)
	if got := strings.Join(strings.Fields(innerText(card)), " "); got != "12 Tasks WAITING5 DOING3 CLOSED4" {
		t.Errorf("the card's served text is %q", got)
	}
	if strings.Contains(card, "style=") {
		t.Errorf("an element of the Sprint progress card carries a style attribute")
	}
	scripts := regexp.MustCompile(`(?s)<script\b([^>]*)>(.*?)</script>`).FindAllStringSubmatch(body, -1)
	data := 0
	for _, s := range scripts {
		if "<script"+s[1]+">" == sprintProgressDataTag {
			data++
			continue
		}
		if !strings.Contains(s[1], `src="/static/`) || s[2] != "" {
			t.Errorf("a script other than the data block has no /static/ src or has content: <script%s>%q", s[1], s[2])
		}
	}
	if data != 1 {
		t.Errorf("the page carries %d chart data blocks, want 1", data)
	}

	hostile := serveSprintProgress(t, f.path(f.hostile)).Body.String()
	content := sprintProgressData(t, hostile)
	for _, bad := range []string{"</script", "<", ">", "&"} {
		if strings.Contains(content, bad) {
			t.Errorf("the hostile-titled sprint's chart data block carries %q: %s", bad, content)
		}
	}
	decodeSprintProgressData(t, content)
}

// ==================== AC 294: WHERE THE CHART ASSETS LOAD ====================

// TestSprintProgress_ChartAssetsLoadOnlyOnDashboardAndSprintPages is Acceptance
// Criterion 294.
func TestSprintProgress_ChartAssetsLoadOnlyOnDashboardAndSprintPages(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintProgressFixture(t, "settlement-cutover")
	mux := buildMux()

	sprint := servePage(t, mux, f.path(f.twelve))
	assertOrdered(t, "sprint page assets", sprint, []string{
		`<link rel="stylesheet" href="/static/vendor/tabler/tabler.min.css">`,
		`<link rel="stylesheet" href="/static/vendor/tabler/tabler-vendors.min.css">`,
		`<script defer src="/static/vendor/apexcharts/apexcharts.min.js"></script>`,
		`<script defer src="/static/sprint-progress.js"></script>`,
	})
	head := regionBetween(t, sprint, "<head>", "</head>")
	if !strings.Contains(head, "<link rel=\"stylesheet\" href=\"/static/vendor/tabler/tabler.min.css\">\n  <link rel=\"stylesheet\" href=\"/static/vendor/tabler/tabler-vendors.min.css\">") {
		t.Errorf("the sprint page's vendors stylesheet does not directly follow the Tabler stylesheet")
	}
	for _, asset := range []string{"apexcharts.min.js", "sprint-progress.js", "tabler-vendors.min.css"} {
		if n := strings.Count(sprint, asset); n != 1 {
			t.Errorf("the sprint page references %s %d times, want once", asset, n)
		}
	}
	if strings.Contains(sprint, "dashboard.js") {
		t.Errorf("the sprint page loads dashboard.js")
	}

	dashboard := servePage(t, mux, "/roadmaps/"+f.name)
	if strings.Contains(dashboard, "sprint-progress.js") || strings.Contains(dashboard, "sprint-progress-data") {
		t.Errorf("the dashboard page loads the sprint page's gauge script or data block")
	}

	for _, path := range []string{
		"/",
		"/roadmaps/" + f.name + "/sprints",
		"/roadmaps/" + f.name + "/tasks",
		"/roadmaps/" + f.name + "/tasks/1",
		"/roadmaps/" + f.name + "/audit",
		"/roadmaps/" + f.name + "/graph",
	} {
		page := servePage(t, mux, path)
		for _, asset := range []string{"tabler-vendors.min.css", "apexcharts.min.js", "dashboard.js", "sprint-progress.js"} {
			if strings.Contains(page, asset) {
				t.Errorf("%s loads %s", path, asset)
			}
		}
	}

	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/sprint-progress.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /static/sprint-progress.js: status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("GET /static/sprint-progress.js: Content-Type = %q", ct)
	}
	if rec.Body.String() != readEmbeddedAsset(t, "static/sprint-progress.js") {
		t.Errorf("GET /static/sprint-progress.js does not serve the embedded script")
	}
}
