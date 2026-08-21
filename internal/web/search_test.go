package web

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for the roadmap tasks board's header search
// (SPEC/WEB.md § Roadmap Tasks Page, Header search control through Escaping the
// term; Acceptance Criteria 100 to 107).
//
// The property that matters is that the two paths agree: the board reached by
// typing a term and the board reached by opening the URL that carries it must be
// the same board. The server applies the term in Go and the browser applies it in
// JavaScript, so the equivalence is kept by construction rather than by two
// implementations happening to coincide — the corpus is folded once, by the
// server, into each card's data-search attribute, and the script folds only the
// term. TestTaskSearch_ServerAndClientProduceTheSameBoard compares the two, and
// TestTaskSearchScript_ImplementsTheSameMatchingRule pins that the script really
// is the rule the comparison re-expresses.

// ==================== READING A RENDERED BOARD ====================

// searchCard is one card as the board rendered it: everything the client needs
// to recompute the board's verdict on that card, and the verdict the server
// reached.
//
// search, taskType, priority and severity are the four values the card carries
// for the four header controls. Each is written ONCE, by the server, in the
// server's own spelling; the client only reads them, which is what keeps the two
// verdicts from being able to disagree about a task's own data.
type searchCard struct {
	id       int
	search   string // the folded corpus the client matches against
	taskType string // data-type: the TaskType, in the enum's own spelling
	priority string // data-priority
	severity string // data-severity
	shown    bool
}

// searchColumn is one column as the board rendered it.
type searchColumn struct {
	status     string
	cards      []searchCard
	count      int
	emptyShown bool
}

// boardState is everything the board says about itself: what each column shows,
// what it counts, which empty states are visible, and the board's own no-match
// message. Two boards are the same when their states are.
//
// messageTermShown is whether the message NAMES the term. One message covers the
// term and the three filters together, and the phrase carrying the term is shown
// only when there is a term, so a board narrowed to nothing by a filter alone
// does not quote an empty search.
type boardState struct {
	columns          []searchColumn
	messageTerm      string
	messageShown     bool
	messageTermShown bool
}

// clientControls is the state of the board's four header controls as the browser
// holds them: the raw term and one value per filter, "" meaning that dimension
// carries no filter — the same meaning the parameter's absence has in the URL.
//
// The three filter fields are strings rather than typed values on purpose: the
// browser holds exactly what the <option> carried, and the point of the
// comparison below is that neither side ever reinterprets it.
type clientControls struct {
	Term     string
	Type     string
	Priority string
	Severity string
}

// query renders the controls as a URL query string, carrying a parameter only
// for a control that holds a value.
//
// It follows the same rule the script's syncURL follows — a control on its
// no-filter option leaves no parameter behind — with one deliberate exception: a
// term that is whitespace is carried, because the SERVER must be exercised with
// one and the script would never produce that URL (it removes q for a term that
// folds to empty, which TestTaskSearchScript_KeepsTheURLInStepWithoutStackingHistory
// pins separately).
func (c clientControls) query() string {
	values := url.Values{}
	for _, pair := range []struct{ name, value string }{
		{"q", c.Term},
		{"type", c.Type},
		{"priority", c.Priority},
		{"severity", c.Severity},
	} {
		if pair.value != "" {
			values.Set(pair.name, pair.value)
		}
	}
	return values.Encode()
}

// active reports whether any control is narrowing the board, which is the
// condition for the board's no-match message.
//
// The term's emptiness is decided by CALLING foldSearchTerm rather than by
// trimming here, for the reason narrow gives below: a harness that re-expressed
// the trim rule with Go's strings.TrimSpace would be stating a second rule, and
// the two divergent code points are exactly where a second statement of it goes
// wrong — a term of U+FEFF alone is a term, and a term of U+0085 alone is not
// (SPEC/WEB.md Acceptance Criterion 121).
func (c clientControls) active() bool {
	return foldSearchTerm(c.Term) != "" || c.Type != "" || c.Priority != "" || c.Severity != ""
}

var (
	reSearchCardTag   = regexp.MustCompile(`<button type="button" class="card card-sm task-card[^>]*>`)
	reSearchCardID    = regexp.MustCompile(`data-task-id="(\d+)"`)
	reSearchCorpus    = regexp.MustCompile(`data-search="([^"]*)"`)
	reSearchCardType  = regexp.MustCompile(`data-type="([^"]*)"`)
	reSearchCardPrio  = regexp.MustCompile(`data-priority="([^"]*)"`)
	reSearchCardSev   = regexp.MustCompile(`data-severity="([^"]*)"`)
	reSearchTerm      = regexp.MustCompile(`data-role="task-search-term">([^<]*)<`)
	reSearchTermPhase = regexp.MustCompile(`data-role="task-search-term-phrase"([^>]*)>`)
	reSearchInput     = regexp.MustCompile(`<input[^>]*data-role="task-search"[^>]*>`)
	reInputValue      = regexp.MustCompile(`value="([^"]*)"`)
)

// readBoardState parses a served tasks page into the state its board presents.
func readBoardState(t *testing.T, body string) boardState {
	t.Helper()

	state := boardState{}
	for _, column := range boardColumns(t, body) {
		status, count := columnHeader(t, column)
		parsed := searchColumn{status: status, count: count, emptyShown: shownEmptyState(column)}

		for _, tag := range reSearchCardTag.FindAllString(column, -1) {
			id := reSearchCardID.FindStringSubmatch(tag)
			corpus := reSearchCorpus.FindStringSubmatch(tag)
			taskType := reSearchCardType.FindStringSubmatch(tag)
			priority := reSearchCardPrio.FindStringSubmatch(tag)
			severity := reSearchCardSev.FindStringSubmatch(tag)
			if id == nil || corpus == nil {
				t.Fatalf("a board card carries no task id or no search corpus: %s", tag)
			}
			if taskType == nil || priority == nil || severity == nil {
				t.Fatalf("a board card carries no type, priority or severity for the header "+
					"filters to compare: %s", tag)
			}
			taskID, err := strconv.Atoi(id[1])
			if err != nil {
				t.Fatalf("a board card carries a non-integer task id: %s", tag)
			}
			parsed.cards = append(parsed.cards, searchCard{
				id:       taskID,
				search:   html.UnescapeString(corpus[1]),
				taskType: html.UnescapeString(taskType[1]),
				priority: priority[1],
				severity: severity[1],
				shown:    !strings.Contains(tag, " hidden"),
			})
		}
		state.columns = append(state.columns, parsed)
	}

	// The board's own no-match message, and the term it names.
	region := boardRegion(t, body)
	at := strings.Index(region, `data-role="task-search-empty"`)
	if at < 0 {
		t.Fatalf("the board carries no no-match message element")
	}
	end := strings.Index(region[at:], ">")
	state.messageShown = end >= 0 && !strings.Contains(region[at:at+end], "hidden")
	if term := reSearchTerm.FindStringSubmatch(region); term != nil {
		state.messageTerm = html.UnescapeString(term[1])
	}
	phrase := reSearchTermPhase.FindStringSubmatch(region)
	if phrase == nil {
		t.Fatalf("the no-match message carries no term phrase element")
	}
	state.messageTermShown = !strings.Contains(phrase[1], "hidden")
	return state
}

// shownIDs lists, per column, the ids the board is showing, in render order.
func (s boardState) shownIDs() map[string][]int {
	shown := make(map[string][]int, len(s.columns))
	for _, column := range s.columns {
		ids := []int{}
		for _, card := range column.cards {
			if card.shown {
				ids = append(ids, card.id)
			}
		}
		shown[column.status] = ids
	}
	return shown
}

// narrow applies the CLIENT's rule to an unnarrowed board, producing the state
// the script would leave the page in for those controls.
//
// It is the script's algorithm re-expressed, ONE conjunction over four criteria
// exactly as static/task-search.js computes one:
//
//   - the term is folded by CALLING the server's own foldSearchTerm rather than
//     by re-expressing the folding rule here, and matches when it is a substring
//     of the corpus the server folded into the card or of the card's "#<id>"
//     reference. A harness that re-expressed the rule could agree with itself
//     while the two real paths disagreed, which is exactly how the folding
//     divergence this test guards against went unnoticed; the script folds with
//     the mapping the server ships it, which
//     TestTaskSearchScript_ShippedRuleIsTheServerRule pins to that same function,
//     together with the whitespace set it strips the term's ends by;
//   - the type criterion is an EQUALITY against the card's own data-type;
//   - the priority and severity criteria are THRESHOLDS, ">=", over the card's
//     own data-priority and data-severity;
//   - a control holding "" contributes no criterion at all.
//
// TestTaskSearchScript_ImplementsTheSameMatchingRule and
// TestTaskFilters_ScriptAppliesTheSameConjunction pin that the served script is
// this rule, so the comparison is between the two real paths rather than between
// the server and a convenient fiction.
func (s boardState) narrow(c clientControls) boardState {
	term := foldSearchTerm(c.Term)

	narrowed := boardState{messageTerm: c.Term, messageTermShown: term != ""}
	total := 0
	for _, column := range s.columns {
		result := searchColumn{status: column.status}
		for _, card := range column.cards {
			show := matchesClientTerm(&card, term) &&
				(c.Type == "" || card.taskType == c.Type) &&
				(c.Priority == "" || clientNumber(card.priority) >= clientNumber(c.Priority)) &&
				(c.Severity == "" || clientNumber(card.severity) >= clientNumber(c.Severity))
			card.shown = show
			if show {
				result.count++
			}
			result.cards = append(result.cards, card)
		}
		result.emptyShown = result.count == 0
		total += result.count
		narrowed.columns = append(narrowed.columns, result)
	}
	narrowed.messageShown = c.active() && total == 0
	return narrowed
}

// matchesClientTerm is the term criterion of narrow, kept separate so the
// conjunction above reads as four criteria rather than as one long expression.
func matchesClientTerm(card *searchCard, term string) bool {
	return term == "" ||
		strings.Contains(card.search, term) ||
		strings.Contains("#"+strconv.Itoa(card.id), term)
}

// clientNumber mirrors the script's Number(): the card attributes and the option
// values are the server's own digit strings, so this never sees anything else.
func clientNumber(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return n
}

// servedBoard requests the tasks page with the URL those controls produce, and
// reads the board it served.
func servedBoard(t *testing.T, mux *http.ServeMux, roadmap string, c clientControls) (boardState, string) {
	t.Helper()

	path := "/roadmaps/" + roadmap + "/tasks"
	if query := c.query(); query != "" {
		path += "?" + query
	}
	body := servePage(t, mux, path)
	return readBoardState(t, body), body
}

// servedBoardQuery requests the tasks page with a RAW query string, for the cases
// no control can produce: a parameter present with an empty value, a decorated or
// undecodable value, a repeated parameter, or a deliberately odd parameter order.
func servedBoardQuery(t *testing.T, mux *http.ServeMux, roadmap, query string) (boardState, string) {
	t.Helper()

	path := "/roadmaps/" + roadmap + "/tasks"
	if query != "" {
		path += "?" + query
	}
	body := servePage(t, mux, path)
	return readBoardState(t, body), body
}

// ==================== THE HEADER CONTROL ====================

// TestTaskSearch_HeaderCarriesTheSearchControl is the gate for Acceptance
// Criterion 100: the page header's actions column carries a labelled search input
// and no knowledge-graph link, and the graph stays reachable through the sidebar.
func TestTaskSearch_HeaderCarriesTheSearchControl(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")

	header := body[strings.Index(body, `<div class="page-header d-print-none">`):strings.Index(body, `<main class="page-body">`)]
	if header == "" {
		t.Fatal("the tasks page has no page header")
	}

	// The input is there, in the actions column, and it is a search input.
	input := reSearchInput.FindString(header)
	if input == "" {
		t.Fatalf("the page header carries no search input\nheader: %s", header)
	}
	for _, attr := range []string{`type="search"`, `id="task-search"`, `name="q"`} {
		if !strings.Contains(input, attr) {
			t.Errorf("the search input is missing %s\ninput: %s", attr, input)
		}
	}

	// A real label, programmatically associated. A placeholder must not stand in
	// for one: it is not an accessible name and disappears as the user types.
	if !strings.Contains(header, `<label class="form-label mb-0" for="task-search">Search tasks</label>`) {
		t.Errorf("the search input has no associated <label>\nheader: %s", header)
	}
	if strings.Contains(input, "placeholder") {
		t.Errorf("the search input uses a placeholder; a label names it instead\ninput: %s", input)
	}
	// A search input is focusable and operable from the keyboard natively, so it
	// carries no tabindex and no ARIA role of its own. (data-role is a test hook,
	// not an ARIA role, so the check is for the standalone attribute.)
	if strings.Contains(input, "tabindex=") {
		t.Errorf("the search input carries tabindex, which an input has natively\ninput: %s", input)
	}
	if regexp.MustCompile(`\srole="`).MatchString(input) {
		t.Errorf("the search input carries an ARIA role, which an input has natively\ninput: %s", input)
	}

	// The knowledge-graph link is gone from the header — and only from there.
	if strings.Contains(header, "/graph") {
		t.Errorf("the page header still links to the knowledge graph\nheader: %s", header)
	}
	if !strings.Contains(body, `href="/roadmaps/`+f.name+`/graph"`) {
		t.Errorf("the graph is no longer reachable from this page; the sidebar must still list it")
	}

	// The control submits nothing: no form, no submit, no button of its own.
	if strings.Contains(header, "<form") || strings.Contains(header, `type="submit"`) {
		t.Errorf("the search control submits something\nheader: %s", header)
	}
}

// ==================== NARROWING, ON THE SERVER ====================

// TestTaskSearch_NarrowsTheBoardAndItsCounts is the gate for Acceptance Criterion
// 101 on the server path: a term narrows the shown cards, the counts follow the
// shown set, matching is case-insensitive and by substring over the title and the
// "#<id>" reference, and no other field is searched.
func TestTaskSearch_NarrowsTheBoardAndItsCounts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	unnarrowed, _ := servedBoard(t, mux, f.name, clientControls{})
	total := 0
	for _, column := range unnarrowed.columns {
		total += column.count
	}
	if total != 9 {
		t.Fatalf("the fixture board shows %d cards, want the 9 seeded", total)
	}

	for _, c := range []struct {
		name    string
		term    string
		wantIDs []int
	}{
		{"a word of two titles", "settlement", []int{f.runbook, f.ledger}},
		{"upper case matches the same tasks", "SETTLEMENT", []int{f.runbook, f.ledger}},
		{"mixed case", "SeTtLeMeNt", []int{f.runbook, f.ledger}},
		{"surrounding whitespace is stripped", "   settlement   ", []int{f.runbook, f.ledger}},
		{"inner whitespace is literal", "settlement ledger", []int{f.ledger}},
		{"a substring inside a word", "authn", []int{f.passkey}},
		{"the reference with its hash", "#" + itoa(f.passkey), []int{f.passkey}},
		{"the bare id finds the same task", itoa(f.passkey), []int{f.passkey}},
		{"a term matching nothing", "zzz-nothing-matches-this", []int{}},
	} {
		state, _ := servedBoard(t, mux, f.name, clientControls{Term: c.term})

		got := []int{}
		for _, ids := range state.shownIDs() {
			got = append(got, ids...)
		}
		if !sameIDSet(got, c.wantIDs) {
			t.Errorf("%s (%q): the board shows %v, want %v", c.name, c.term, got, c.wantIDs)
		}

		// Every column's count is the number of cards it is showing, and the five
		// columns stay present and in order.
		if len(state.columns) != 5 {
			t.Errorf("%s: the narrowed board has %d columns, want 5", c.name, len(state.columns))
		}
		for i, column := range state.columns {
			shown := len(state.shownIDs()[column.status])
			if column.count != shown {
				t.Errorf("%s: column %s counts %d and shows %d cards",
					c.name, column.status, column.count, shown)
			}
			if column.status != string(models.ValidTaskStatuses[i]) {
				t.Errorf("%s: column %d is %s, want %s",
					c.name, i, column.status, models.ValidTaskStatuses[i])
			}
			// A narrowed-away card stays in the document: the browser must be able
			// to show it again without a round trip.
			if len(column.cards) == 0 && len(unnarrowed.columns[i].cards) > 0 {
				t.Errorf("%s: column %s dropped its cards from the document",
					c.name, column.status)
			}
		}
	}

	// An empty or whitespace-only term is no term at all: every card is shown.
	for _, blank := range []string{"", " ", "   \t  "} {
		state, _ := servedBoardQuery(t, mux, f.name, "q="+url.QueryEscape(blank))
		shown := 0
		for _, column := range state.columns {
			shown += column.count
		}
		if shown != total {
			t.Errorf("the blank term %q shows %d cards, want every one of the %d", blank, shown, total)
		}
		if state.messageShown {
			t.Errorf("the blank term %q reports no matches", blank)
		}
	}

	// Every other task field is outside the search. The exclusion is shown with
	// `functional_requirements`, the field Acceptance Criterion 101 now names: a
	// term occurring only there, and in no title and no reference, matches nothing
	// (SPEC/WEB.md § Roadmap Tasks Page, What the search matches).
	const requirementsOnlyTerm = "Operators"
	state, _ := servedBoard(t, mux, f.name, clientControls{Term: requirementsOnlyTerm})
	for _, ids := range state.shownIDs() {
		if len(ids) != 0 {
			t.Errorf("a %s value matched task(s) %v; the search covers the title and "+
				"the #id reference only", "functional_requirements", ids)
		}
	}

	// The control that keeps the absence above from being vacuous: the term really
	// is in the seeded functional_requirements, and really is in no title. Without
	// it, a term the fixture never wrote anywhere would pass the same assertion.
	detail := decodeTaskDetail(t, mux, f.name, f.passkey)
	if !strings.Contains(detail.Task.FunctionalRequirements, requirementsOnlyTerm) {
		t.Fatalf("the seeded functional_requirements %q does not contain %q, so asserting the "+
			"search ignores the field proves nothing",
			detail.Task.FunctionalRequirements, requirementsOnlyTerm)
	}
	if strings.Contains(strings.ToLower(detail.Task.Title), strings.ToLower(requirementsOnlyTerm)) {
		t.Fatalf("the seeded title %q contains %q, so the term is not exclusive to "+
			"functional_requirements", detail.Task.Title, requirementsOnlyTerm)
	}
}

// sameIDSet reports whether two id sets hold the same ids, order-insensitively.
func sameIDSet(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[int]int, len(want))
	for _, id := range want {
		seen[id]++
	}
	for _, id := range got {
		seen[id]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// TestTaskSearch_PreservesTheOrderWithinAColumn is the gate for the ordering half
// of Acceptance Criterion 101: narrowing removes cards, it does not reorder the
// ones that remain.
func TestTaskSearch_PreservesTheOrderWithinAColumn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	// The BACKLOG column's four cards are seeded so that priority order, creation
	// order and id order all differ; "en" occurs in three of the four titles and
	// not in the fourth, so the narrowed column is a proper subset.
	unnarrowed, _ := servedBoard(t, mux, f.name, clientControls{})
	narrowed, _ := servedBoard(t, mux, f.name, clientControls{Term: "en"})

	full := unnarrowed.shownIDs()[string(models.StatusBacklog)]
	kept := narrowed.shownIDs()[string(models.StatusBacklog)]
	if len(kept) < 2 || len(kept) >= len(full) {
		t.Fatalf("the term kept %d of %d BACKLOG cards; the seed no longer discriminates", len(kept), len(full))
	}

	// The kept ids appear in the same relative order they had unnarrowed.
	position := make(map[int]int, len(full))
	for i, id := range full {
		position[id] = i
	}
	for i := 1; i < len(kept); i++ {
		if position[kept[i-1]] >= position[kept[i]] {
			t.Errorf("narrowing reordered the column: #%d now precedes #%d, but did not before",
				kept[i-1], kept[i])
		}
	}
}

// ==================== THE TWO EMPTY STATES ====================

// TestTaskSearch_EmptyStates is the gate for Acceptance Criterion 102: a column
// emptied by the search shows its ordinary in-column empty state, the five
// columns stay, and a board emptied by the search says so — which a roadmap that
// simply holds no task does not.
func TestTaskSearch_EmptyStates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	if err := createEmptyRoadmap("payment-platform-empty"); err != nil {
		t.Fatalf("creating the empty roadmap: %v", err)
	}
	mux := buildMux()

	// A term that matches two tasks, in two columns: the other three columns are
	// emptied by the search and show their empty state.
	state, _ := servedBoard(t, mux, f.name, clientControls{Term: "settlement"})
	if len(state.columns) != 5 {
		t.Fatalf("the narrowed board has %d columns, want 5", len(state.columns))
	}
	for _, column := range state.columns {
		if column.emptyShown != (column.count == 0) {
			t.Errorf("column %s counts %d and %s its empty state",
				column.status, column.count,
				map[bool]string{true: "shows", false: "hides"}[column.emptyShown])
		}
	}
	if state.messageShown {
		t.Errorf("the board reports no matches while showing cards")
	}

	// A term that matches nothing: five empty columns AND the board's own message,
	// naming the term, so the user is not left to interpret five silent columns.
	none, body := servedBoard(t, mux, f.name, clientControls{Term: "zzz-nothing"})
	if !none.messageShown {
		t.Errorf("a search that matched nothing renders no no-match message")
	}
	if none.messageTerm != "zzz-nothing" {
		t.Errorf("the no-match message names %q, want the term the user typed", none.messageTerm)
	}
	for _, column := range none.columns {
		if !column.emptyShown || column.count != 0 {
			t.Errorf("column %s shows %d cards and empty=%v after a search that matched nothing",
				column.status, column.count, column.emptyShown)
		}
	}
	if !strings.Contains(body, `data-role="task-board"`) {
		t.Errorf("a search that matched nothing replaced the board instead of emptying it")
	}

	// A roadmap that holds no task is a different condition and reads differently:
	// the in-column empty states alone, with no search message.
	empty, _ := servedBoard(t, mux, "payment-platform-empty", clientControls{})
	if empty.messageShown {
		t.Errorf("a roadmap with no task reports a search that matched nothing")
	}
	for _, column := range empty.columns {
		if !column.emptyShown {
			t.Errorf("the empty roadmap's column %s renders no empty state", column.status)
		}
	}
}

// ==================== THE PROPERTY THAT MATTERS ====================

// TestTaskSearch_ServerAndClientProduceTheSameBoard is the gate for Acceptance
// Criterion 104: for any term, the board the server renders for that term and the
// board the browser produces by narrowing the unnarrowed page are the same —
// same cards, same columns, same order, same counts, same empty states.
//
// The comparison is direct: one side is the served ?q= page, the other is the
// served bare page with the script's own rule applied to it.
func TestTaskSearch_ServerAndClientProduceTheSameBoard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	unnarrowed, _ := servedBoard(t, mux, f.name, clientControls{})

	terms := []string{
		"", " ", "settlement", "SETTLEMENT", "  settlement  ", "settlement ledger",
		"authn", "#" + itoa(f.passkey), itoa(f.passkey), "#", "e",
		"zzz-nothing-matches", "security-review",
		`<script>alert("x")</script>`, "reconcile & report", "café",
		strings.Repeat("long", 200),
	}

	for _, term := range terms {
		server, _ := servedBoard(t, mux, f.name, clientControls{Term: term})
		client := unnarrowed.narrow(clientControls{Term: term})

		if len(server.columns) != len(client.columns) {
			t.Fatalf("term %q: the server board has %d columns and the browser board %d",
				term, len(server.columns), len(client.columns))
		}
		for i := range server.columns {
			sc, cc := server.columns[i], client.columns[i]
			if sc.status != cc.status {
				t.Errorf("term %q: column %d is %s on the server and %s in the browser",
					term, i, sc.status, cc.status)
			}
			if sc.count != cc.count {
				t.Errorf("term %q: column %s counts %d on the server and %d in the browser",
					term, sc.status, sc.count, cc.count)
			}
			if sc.emptyShown != cc.emptyShown {
				t.Errorf("term %q: column %s shows its empty state on the server=%v, browser=%v",
					term, sc.status, sc.emptyShown, cc.emptyShown)
			}
			if got, want := shownOf(sc), shownOf(cc); !equalIDs(got, want) {
				t.Errorf("term %q: column %s shows %v on the server and %v in the browser",
					term, sc.status, got, want)
			}
		}
		if server.messageShown != client.messageShown {
			t.Errorf("term %q: the no-match message shows on the server=%v, browser=%v",
				term, server.messageShown, client.messageShown)
		}
		if server.messageTerm != client.messageTerm {
			t.Errorf("term %q: the message names %q on the server and %q in the browser",
				term, server.messageTerm, client.messageTerm)
		}
	}
}

// shownOf lists the ids a column shows, in render order.
func shownOf(column searchColumn) []int {
	ids := []int{}
	for _, card := range column.cards {
		if card.shown {
			ids = append(ids, card.id)
		}
	}
	return ids
}

// TestTaskSearch_CorpusIsFoldedByTheServer pins the mechanism that makes the
// equivalence above structural rather than coincidental: the card carries the
// task's title already folded, so the browser never case-folds task text and a
// difference between Go's case conversion and the browser's cannot make the same
// term select different tasks.
func TestTaskSearch_CorpusIsFoldedByTheServer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	state, _ := servedBoard(t, mux, f.name, clientControls{})
	titles := roadmapTaskTitles(t, f.name)

	checked := 0
	for _, column := range state.columns {
		for _, card := range column.cards {
			title, ok := titles[card.id]
			if !ok {
				t.Errorf("the board shows a card for task #%d, which the roadmap does not hold", card.id)
				continue
			}
			// The comparison is byte-exact on purpose: a case-insensitive one
			// would pass for a corpus the server left unfolded, which is the
			// defect this test exists to catch.
			folded := strings.ToLower(title)
			if card.search != folded {
				t.Errorf("task #%d carries the corpus %q, want its title folded: %q",
					card.id, card.search, folded)
			}
			if title != folded && card.search == title {
				t.Errorf("task #%d carries its title unfolded", card.id)
			}
			checked++
		}
	}
	if checked != len(titles) {
		t.Errorf("checked %d cards against %d tasks", checked, len(titles))
	}
}

// ==================== NO TERM IS AN ERROR, AND NONE COSTS A QUERY ====================

// TestTaskSearch_NoTermIsAnErrorAndNoneAddsAQuery is the gate for Acceptance
// Criterion 105: every q value answers 200, an undecodable q is treated as
// absent, and applying a term adds no database query.
func TestTaskSearch_NoTermIsAnErrorAndNoneAddsAQuery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	srv := handler()

	for _, c := range []struct{ name, query string }{
		{"absent", ""},
		{"empty", "?q="},
		{"whitespace", "?q=%20%20"},
		{"matches nothing", "?q=zzz-nothing"},
		{"longer than any title", "?q=" + strings.Repeat("x", 4096)},
		{"undecodable percent escape", "?q=%zz"},
		{"raw percent", "?q=%"},
		{"markup", "?q=%3Cscript%3E"},
		{"a null byte", "?q=%00"},
		{"repeated parameter", "?q=one&q=two"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+f.name+"/tasks"+c.query, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("%s (%q): status = %d, want 200", c.name, c.query, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `data-role="task-board"`) {
			t.Errorf("%s (%q): the response carries no board", c.name, c.query)
		}
	}

	// An undecodable q is treated as absent: the board is the unnarrowed one.
	undecodable := httptest.NewRequest(http.MethodGet, "/roadmaps/"+f.name+"/tasks?q=%zz", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, undecodable)
	state := readBoardState(t, rec.Body.String())
	shown := 0
	for _, column := range state.columns {
		shown += column.count
	}
	if shown != 9 {
		t.Errorf("an undecodable q narrowed the board to %d cards; it must be treated as absent", shown)
	}

	// The term costs no query: the page's three reads are unchanged.
	src := openCounting(t, f.name)
	if _, err := readTasks(context.Background(), src, f.name, newBoardControls("settlement", "", 0, 0)); err != nil {
		t.Fatalf("readTasks with a term: %v", err)
	}
	if src.taskList != 1 || src.groupedCommentCounts != 1 || src.groupedTaskSprints != 1 {
		t.Errorf("a narrowed render issued %d task-list, %d comment-count and %d sprint queries; "+
			"want 1, 1 and 1", src.taskList, src.groupedCommentCounts, src.groupedTaskSprints)
	}
	if src.perTaskComments != 0 || src.boundedTaskList != 0 {
		t.Errorf("a narrowed render took a read the board must not take")
	}
}

// ==================== ESCAPING THE TERM ====================

// TestTaskSearch_TermIsEscapedWhereverItIsEchoed is the gate for Acceptance
// Criterion 106 on the server path: a term carrying markup is echoed into the
// input and into the no-match message as text, and introduces no element,
// attribute, or script.
func TestTaskSearch_TermIsEscapedWhereverItIsEchoed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	mux := buildMux()

	const hostile = `"><script>alert('x')</script><b onmouseover="steal()">`

	body := servePage(t, mux, "/roadmaps/"+f.name+"/tasks?q="+url.QueryEscape(hostile))

	// The raw term reaches the page nowhere, in any of its dangerous forms.
	for _, raw := range []string{
		hostile, "<script>alert(", "</script><b", `onmouseover="steal()"`, `"><script`,
	} {
		if strings.Contains(body, raw) {
			t.Errorf("the raw term reached the page: found %q", raw)
		}
	}
	// The page's script elements are exactly the three it loads: a term that
	// became markup would raise either count.
	if got := strings.Count(body, "<script"); got != 3 {
		t.Errorf("the page has %d <script elements, want 3", got)
	}
	if got := strings.Count(body, "</script>"); got != 3 {
		t.Errorf("the page has %d </script> closers, want 3", got)
	}

	// The input echoes the term as an attribute VALUE that decodes back to exactly
	// what the user typed: a stray quote would have closed the attribute and the
	// extraction below would come back truncated.
	input := reSearchInput.FindString(body)
	if input == "" {
		t.Fatalf("the page renders no search input")
	}
	value := reInputValue.FindStringSubmatch(input)
	if value == nil {
		t.Fatalf("the search input carries no value attribute: %s", input)
	}
	if decoded := html.UnescapeString(value[1]); decoded != hostile {
		t.Errorf("the input value decodes to %q, want the term exactly as typed", decoded)
	}

	// The no-match message echoes it as TEXT, decoding to the same term.
	state := readBoardState(t, body)
	if !state.messageShown {
		t.Fatalf("the hostile term matched something; the message must be showing")
	}
	if state.messageTerm != hostile {
		t.Errorf("the no-match message names %q, want the term exactly as typed", state.messageTerm)
	}

	// And the board itself survived: five columns, all cards still in the document.
	if len(state.columns) != 5 {
		t.Errorf("the hostile term broke the board: %d columns", len(state.columns))
	}
}

// ==================== THE SCRIPT ====================

// TestTaskSearchScript_ImplementsTheSameMatchingRule pins that the served script
// is the rule TestTaskSearch_ServerAndClientProduceTheSameBoard re-expresses, and
// that it is locale-independent.
//
// A browser is not available here, so the script is asserted at its source — but
// the assertions are specific: the folding function, the two things matched, and
// the absence of the locale-sensitive variant that would make the same term
// select different tasks for different viewers.
func TestTaskSearchScript_ImplementsTheSameMatchingRule(t *testing.T) {
	script := stripJSComments(readEmbeddedAsset(t, "static/task-search.js"))

	// The term is normalised exactly as the server normalises it, and by the
	// server's own two tables: its ends stripped through the shipped SPACE_TABLE,
	// THEN every CODE POINT walked through the shipped FOLD_TABLE. Neither step
	// is the platform's — its case conversion is Unicode's Default Case
	// Conversion rather than the folding rule, its trimming removes a different
	// set from the White_Space property, and both read tables of whatever Unicode
	// version the browser ships (SPEC/WEB.md Acceptance Criteria 118, 119, 121
	// and 122; the tables themselves are checked against the server's foldSearch
	// and isSearchSpace over the whole of Unicode by
	// TestTaskSearchScript_ShippedRuleIsTheServerRule, which also asserts that no
	// trimming function of the platform is named anywhere in the asset).
	for _, fragment := range []string{
		"var FOLD_TABLE = [",
		"var SPACE_TABLE = [",
		"function foldCodePoint(",
		"function isSpaceCodePoint(",
		"function trimTerm(",
		"var trimmed = trimTerm(raw);",
		"trimmed.codePointAt(i)",
		"String.fromCodePoint(foldCodePoint(cp))",
	} {
		if !strings.Contains(script, fragment) {
			t.Errorf("the script does not normalise the term through the server's shipped "+
				"tables: no %q", fragment)
		}
	}
	for _, conversion := range []string{
		"toLowerCase", "toLocaleLowerCase", "toUpperCase", "toLocaleUpperCase",
	} {
		if strings.Contains(script, conversion) {
			t.Errorf("the script folds with the platform's %s; the same term would then select "+
				"different tasks on the two paths, and in two browsers of different Unicode "+
				"versions", conversion)
		}
	}
	if strings.Contains(script, "localeCompare") {
		t.Errorf("the script compares with localeCompare, which is locale-sensitive")
	}

	// It matches the two things the server matches, and nothing else: the corpus
	// the server folded into the card, and the "#<id>" reference.
	for _, fragment := range []string{
		`getAttribute("data-search")`,
		`"#" + (card.getAttribute("data-task-id") || "")`,
		"title.indexOf(term) !== -1",
		"reference.indexOf(term) !== -1",
	} {
		if !strings.Contains(script, fragment) {
			t.Errorf("the script's matching rule has no %q", fragment)
		}
	}
	// It never folds the task text itself: that was folded once, by the server.
	if strings.Contains(script, "title.toLowerCase") || strings.Contains(script, "textContent.toLowerCase") {
		t.Errorf("the script folds task text; the corpus arrives folded so the two paths cannot diverge")
	}

	// It keeps the board's own statements in step with what it shows.
	for _, fragment := range []string{
		"badge.textContent = String(visible)",
		"columnEmpty.hidden = visible > 0",
		"boardEmpty.hidden = !(active(state) && shown === 0)",
	} {
		if !strings.Contains(script, fragment) {
			t.Errorf("the script does not keep %q in step with the shown set", fragment)
		}
	}
}

// TestTaskSearchScript_KeepsTheURLInStepWithoutStackingHistory is the gate for
// Acceptance Criterion 103: the term travels in q, the current history entry is
// replaced rather than a new one pushed per keystroke, and an empty term removes
// the parameter rather than leaving it empty.
func TestTaskSearchScript_KeepsTheURLInStepWithoutStackingHistory(t *testing.T) {
	script := stripJSComments(readEmbeddedAsset(t, "static/task-search.js"))

	if !strings.Contains(script, "window.history.replaceState") {
		t.Errorf("the script does not update the URL in place")
	}
	if strings.Contains(script, "pushState") {
		t.Errorf("the script pushes a history entry; typing would turn Back into an undo key")
	}
	if !strings.Contains(script, `url.searchParams.delete("q")`) {
		t.Errorf("the script does not remove q for an empty term")
	}
	if !strings.Contains(script, `url.searchParams.set("q", state.raw)`) {
		t.Errorf("the script does not carry the term in q")
	}
	// Nothing is fetched, submitted or navigated: narrowing is a DOM operation.
	for _, forbidden := range []string{"fetch(", "XMLHttpRequest", "location.href =", "location.assign",
		"location.replace", "submit(", "FormData"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("the search script reaches the network or navigates: found %q", forbidden)
		}
	}
}

// TestTaskSearchScript_WritesTheTermAsText is the gate for Acceptance Criterion
// 106 on the client path: the script writes the term only as text.
func TestTaskSearchScript_WritesTheTermAsText(t *testing.T) {
	script := stripJSComments(readEmbeddedAsset(t, "static/task-search.js"))

	for _, sink := range []string{
		"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write",
		"eval(", "new Function", "createContextualFragment", "setHTMLUnsafe",
	} {
		if strings.Contains(script, sink) {
			t.Errorf("the search script uses %q; the term must be written as text", sink)
		}
	}
	if !strings.Contains(script, "boardEmptyTerm.textContent = state.raw") {
		t.Errorf("the search script does not write the term through textContent")
	}
}

// TestTaskSearch_AddsNoInlineScriptAndKeepsThePolicy is the gate for Acceptance
// Criterion 107: the narrowing script loads from /static/ like every other client
// script and the Content-Security-Policy is unchanged.
func TestTaskSearch_AddsNoInlineScriptAndKeepsThePolicy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedBoardFixture(t, "payment-platform")
	srv := handler()

	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+f.name+"/tasks?q=settlement", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Errorf("Content-Security-Policy = %q, want the unchanged policy %q", got, contentSecurityPolicy)
	}

	body := rec.Body.String()
	scripts := regexp.MustCompile(`<script\b([^>]*)>`).FindAllStringSubmatch(body, -1)
	if len(scripts) != 3 {
		t.Errorf("the page loads %d scripts, want 3", len(scripts))
	}
	found := false
	for _, script := range scripts {
		src := regexp.MustCompile(`src="([^"]*)"`).FindStringSubmatch(script[1])
		if src == nil {
			t.Errorf("an inline <script> reached the page, which the policy forbids: %s", script[1])
			continue
		}
		if !strings.HasPrefix(src[1], "/static/") {
			t.Errorf("the page loads the script %q from outside /static/", src[1])
		}
		if src[1] == "/static/task-search.js" {
			found = true
		}
	}
	if !found {
		t.Errorf("the page does not load the narrowing script from /static/")
	}

	// And the script is actually served.
	asset := httptest.NewRequest(http.MethodGet, "/static/task-search.js", nil)
	assetRec := httptest.NewRecorder()
	srv.ServeHTTP(assetRec, asset)
	if assetRec.Code != http.StatusOK {
		t.Errorf("GET /static/task-search.js: status = %d, want 200", assetRec.Code)
	}
}
