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

// searchCard is one card as the board rendered it.
type searchCard struct {
	id     int
	search string // the folded corpus the client matches against
	shown  bool
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
type boardState struct {
	columns      []searchColumn
	messageTerm  string
	messageShown bool
}

var (
	reSearchCardTag = regexp.MustCompile(`<button type="button" class="card card-sm task-card[^>]*>`)
	reSearchCardID  = regexp.MustCompile(`data-task-id="(\d+)"`)
	reSearchCorpus  = regexp.MustCompile(`data-search="([^"]*)"`)
	reSearchTerm    = regexp.MustCompile(`data-role="task-search-term">([^<]*)<`)
	reSearchInput   = regexp.MustCompile(`<input[^>]*data-role="task-search"[^>]*>`)
	reInputValue    = regexp.MustCompile(`value="([^"]*)"`)
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
			if id == nil || corpus == nil {
				t.Fatalf("a board card carries no task id or no search corpus: %s", tag)
			}
			taskID, err := strconv.Atoi(id[1])
			if err != nil {
				t.Fatalf("a board card carries a non-integer task id: %s", tag)
			}
			parsed.cards = append(parsed.cards, searchCard{
				id:     taskID,
				search: html.UnescapeString(corpus[1]),
				shown:  !strings.Contains(tag, " hidden"),
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
// the script would leave the page in for that term.
//
// It is the script's algorithm re-expressed: the term is trimmed and folded with
// a locale-independent lower-casing, and a card matches when the folded term is a
// substring of the corpus the server folded into it, or of its "#<id>" reference.
// TestTaskSearchScript_ImplementsTheSameMatchingRule pins that the served script
// is this rule, so the comparison is between the two real paths rather than
// between the server and a convenient fiction.
func (s boardState) narrow(raw string) boardState {
	term := strings.ToLower(strings.TrimSpace(raw))

	narrowed := boardState{messageTerm: raw}
	total := 0
	for _, column := range s.columns {
		result := searchColumn{status: column.status}
		for _, card := range column.cards {
			show := term == "" ||
				strings.Contains(card.search, term) ||
				strings.Contains("#"+strconv.Itoa(card.id), term)
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
	narrowed.messageShown = term != "" && total == 0
	return narrowed
}

// servedBoard requests the tasks page, optionally with a term, and reads it.
func servedBoard(t *testing.T, mux *http.ServeMux, roadmap, term string, withTerm bool) (boardState, string) {
	t.Helper()

	path := "/roadmaps/" + roadmap + "/tasks"
	if withTerm {
		path += "?q=" + url.QueryEscape(term)
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

	unnarrowed, _ := servedBoard(t, mux, f.name, "", false)
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
		state, _ := servedBoard(t, mux, f.name, c.term, true)

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
		state, _ := servedBoard(t, mux, f.name, blank, true)
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

	// specialists is deliberately outside the search: the passkey task carries
	// "go-developer, security-review" and no title contains it.
	state, _ := servedBoard(t, mux, f.name, "security-review", true)
	for _, ids := range state.shownIDs() {
		if len(ids) != 0 {
			t.Errorf("a specialists value matched task(s) %v; the search covers the title and "+
				"the #id reference only", ids)
		}
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
	unnarrowed, _ := servedBoard(t, mux, f.name, "", false)
	narrowed, _ := servedBoard(t, mux, f.name, "en", true)

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
	state, _ := servedBoard(t, mux, f.name, "settlement", true)
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
	none, body := servedBoard(t, mux, f.name, "zzz-nothing", true)
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
	empty, _ := servedBoard(t, mux, "payment-platform-empty", "", false)
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

	unnarrowed, _ := servedBoard(t, mux, f.name, "", false)

	terms := []string{
		"", " ", "settlement", "SETTLEMENT", "  settlement  ", "settlement ledger",
		"authn", "#" + itoa(f.passkey), itoa(f.passkey), "#", "e",
		"zzz-nothing-matches", "security-review",
		`<script>alert("x")</script>`, "reconcile & report", "café",
		strings.Repeat("long", 200),
	}

	for _, term := range terms {
		server, _ := servedBoard(t, mux, f.name, term, true)
		client := unnarrowed.narrow(term)

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

	state, _ := servedBoard(t, mux, f.name, "", false)
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
	if _, err := readTasks(context.Background(), src, f.name, "settlement"); err != nil {
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

	// The term is folded exactly as the server folds it: trimmed, lower-cased,
	// with the LOCALE-SENSITIVE variant absent.
	if !strings.Contains(script, "raw.trim().toLowerCase()") {
		t.Errorf("the script does not fold the term with trim().toLowerCase()")
	}
	if strings.Contains(script, "toLocaleLowerCase") || strings.Contains(script, "toLocaleUpperCase") {
		t.Errorf("the script folds with a locale-sensitive conversion; the same term would then " +
			"select different tasks for different viewers")
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
		"boardEmpty.hidden = !(term !== \"\" && shown === 0)",
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
	if !strings.Contains(script, `url.searchParams.set("q", raw)`) {
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
	if !strings.Contains(script, "boardEmptyTerm.textContent = raw") {
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
