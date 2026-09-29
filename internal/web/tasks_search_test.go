package web

import (
	"html"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for the tasks page's text search: what it matches, and
// the trim, normalisation and folding rules the server prepares the term and every
// task's searchable text by (SPEC/WEB.md § Roadmap Tasks Page, The text search;
// Acceptance Criteria 101, 103, 105, 106, 118, 119, 121, 122, 152, 153, and 155).

// searchTask is one task of the search fixture.
type searchTask struct {
	title    string
	requires string
	id       int
}

// seedSearchFixture creates a roadmap holding the given titles, each task
// carrying its own functional requirements text, and returns the tasks in
// creation order.
func seedSearchFixture(t *testing.T, name string, tasks []searchTask) []searchTask {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	out := make([]searchTask, len(tasks))
	for i, task := range tasks {
		requires := task.requires
		if requires == "" {
			requires = "The operator can find this task from the tasks page."
		}
		id, terr := seedTask(database, &models.Task{
			Title:                  task.title,
			Type:                   models.TypeImprovement,
			Status:                 models.StatusBacklog,
			Priority:               5,
			Severity:               3,
			FunctionalRequirements: requires,
			TechnicalRequirements:  "Served read-only from the roadmap database.",
			AcceptanceCriteria:     "The search finds the task by its title or reference.",
			CreatedAt:              "2026-05-01T08:00:00.000Z",
		})
		if terr != nil {
			t.Fatalf("creating task %q: %v", task.title, terr)
		}
		task.id = id
		task.requires = requires
		out[i] = task
	}
	return out
}

// searchIDs serves the tasks page for a raw query string at the largest page
// size and returns the listed ids, sorted, failing on a status other than 200.
func searchIDs(t *testing.T, name, rawQuery string) []int {
	t.Helper()

	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/tasks?size=100&"+rawQuery, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("?%s: status = %d, want 200", rawQuery, rec.Code)
	}
	ids := parseList(t, rec.Body.String()).ids
	slices.Sort(ids)
	return ids
}

// searchIDsOf returns the ids of the fixture tasks at the given indexes, sorted.
func searchIDsOf(tasks []searchTask, indexes ...int) []int {
	ids := make([]int, 0, len(indexes))
	for _, i := range indexes {
		ids = append(ids, tasks[i].id)
	}
	slices.Sort(ids)
	return ids
}

// q encodes one search term as a query string.
func q(term string) string { return "q=" + url.QueryEscape(term) }

// TestTaskSearch_MatchesTheTitleAndTheReferenceAlone is the gate for Acceptance
// Criterion 101: a term matches a task whose title or #<id> reference contains
// it, case-insensitively and as a substring — both "7" and "#7" find task 7 — and
// matches nothing else: a word found only in a task's functional requirements, or
// only in its type, finds no task, and the end of a title joined to the start of
// the task's reference finds nothing, because the two are matched separately. A
// term empty after the trim lists every task.
func TestTaskSearch_MatchesTheTitleAndTheReferenceAlone(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	titles := make([]searchTask, 0, 12)
	for i := range 12 {
		titles = append(titles, searchTask{title: []string{
			"Invalidate the settlement CACHE after a refund",
			"Rotate the acquirer API credentials",
			"Warm the cache of merchant fee schedules",
		}[i%3]})
	}
	titles[4].requires = "Only this requirement mentions the zeppelin freight carrier."
	tasks := seedSearchFixture(t, "payments-search", titles)

	if got, want := searchIDs(t, "payments-search", q("cache")), searchIDsOf(tasks, 0, 2, 3, 5, 6, 8, 9, 11); !slices.Equal(got, want) {
		t.Errorf("cache lists %v, want the six cache tasks %v", got, want)
	}
	if got, want := searchIDs(t, "payments-search", q("CaChE")), searchIDs(t, "payments-search", q("cache")); !slices.Equal(got, want) {
		t.Errorf("the search is case-sensitive: CaChE lists %v and cache %v", got, want)
	}
	if got := searchIDs(t, "payments-search", q("zeppelin")); len(got) != 0 {
		t.Errorf("a word found only in a functional requirement lists %v, want no task", got)
	}
	if got := searchIDs(t, "payments-search", q("IMPROVEMENT")); len(got) != 0 {
		t.Errorf("a word found only in the tasks' type lists %v, want no task", got)
	}

	// The reference: 7 and #7 both find task 7; 1 finds #1, #10, #11, #12.
	seven := tasks[6].id
	for _, term := range []string{"7", "#7"} {
		if got := searchIDs(t, "payments-search", q(term)); !slices.Contains(got, seven) {
			t.Errorf("%q lists %v, which does not hold task #%d", term, got, seven)
		}
	}
	want := []int{}
	for _, task := range tasks {
		if strings.Contains("#"+itoa(task.id), "1") {
			want = append(want, task.id)
		}
	}
	if got := searchIDs(t, "payments-search", q("1")); !slices.Equal(got, want) {
		t.Errorf("1 lists %v, want the tasks whose reference contains it %v", got, want)
	}

	// No term and a term empty after the trim both list every task.
	every := searchIDsOf(tasks, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11)
	for _, raw := range []string{"", q(""), q(" \t\r\n "), q("\u0085\u2003")} {
		if got := searchIDs(t, "payments-search", raw); !slices.Equal(got, every) {
			t.Errorf("?%s lists %v, want every task", raw, got)
		}
	}

	// The title and the reference are matched separately, never as one
	// joined string: the end of a title followed by the start of the task's
	// own reference, with or without a space between them, is contained in
	// neither and finds nothing, while each half alone finds the task.
	lisboa := seedSearchFixture(t, "lisbon-onboarding", []searchTask{{title: "Open the office in Lisboa"}})
	ref := "#" + itoa(lisboa[0].id)
	for _, term := range []string{"Lisboa" + ref, "Lisboa " + ref, "boa" + ref[:1], "boa " + ref} {
		if got := searchIDs(t, "lisbon-onboarding", q(term)); len(got) != 0 {
			t.Errorf("%q, made of the title's end and the reference's start, lists %v; the two are matched separately", term, got)
		}
	}
	for _, term := range []string{"Lisboa", ref} {
		if got := searchIDs(t, "lisbon-onboarding", q(term)); !slices.Equal(got, searchIDsOf(lisboa, 0)) {
			t.Errorf("%q lists %v, want the Lisboa task", term, got)
		}
	}
}

// TestTaskSearch_NoTermIsAnError is the gate for Acceptance Criteria 103 and 105:
// every q answers 200 — a term matching nothing, a term longer than any title,
// and an undecodable q, the last treated as absent — the search input shows the q
// the request carried, and a term holding bytes that are not valid UTF-8 has each
// replaced by U+FFFD and is then matched like any other term; the input echoes it
// with the replacement, the served page carries no invalid byte, and every
// generated link carries the term as echoed, U+FFFD percent-encoded as %EF%BF%BD.
func TestTaskSearch_NoTermIsAnError(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	tasks := seedSearchFixture(t, "payments-search", []searchTask{
		{title: "Reconcile the nightly payout file"},
		{title: "Decode the acquirer file \uFFFD marker"},
		{title: "Decode the acquirer file \uFFFD\uFFFD twin marker"},
	})

	if got := searchIDs(t, "payments-search", q(strings.Repeat("reconcile the nightly payout file ", 40))); len(got) != 0 {
		t.Errorf("a term longer than any title lists %v, want nothing", got)
	}
	if got := searchIDs(t, "payments-search", "q=%zz"); !slices.Equal(got, searchIDsOf(tasks, 0, 1, 2)) {
		t.Errorf("an undecodable q lists %v, want every task, as though q were absent", got)
	}
	// One invalid byte is one U+FFFD; two are two.
	if got := searchIDs(t, "payments-search", "q=%FF"); !slices.Equal(got, searchIDsOf(tasks, 1, 2)) {
		t.Errorf("q=%%FF lists %v, want the two titles holding U+FFFD", got)
	}
	if got := searchIDs(t, "payments-search", "q=%FF%FE"); !slices.Equal(got, searchIDsOf(tasks, 2)) {
		t.Errorf("q=%%FF%%FE lists %v, want the one title holding two U+FFFD", got)
	}
	if got := foldSearchTerm("\xff\xfe"); got != "\uFFFD\uFFFD" {
		t.Errorf("the term \\xff\\xfe prepares to %+q, want two U+FFFD", got)
	}

	// A q holding one invalid byte between two ASCII letters is echoed as those
	// letters with one U+FFFD between them, the served page carries no invalid
	// UTF-8 byte, and every generated link carries the term as echoed:
	// %EF%BF%BD in place of the byte, never the byte (Acceptance Criteria 103
	// and 105).
	body := servePage(t, buildMux(), "/roadmaps/payments-search/tasks?size=10&q=a%FFb")
	if !utf8.ValidString(body) {
		t.Errorf("the page served for q=a%%FFb carries an invalid UTF-8 byte")
	}
	echoed := parseList(t, body)
	if echoed.search != "a\uFFFDb" {
		t.Errorf("q=a%%FFb is echoed as %+q, want a, U+FFFD, b", echoed.search)
	}
	// q=%FF matches the two titles holding U+FFFD, so the page generates its
	// links; each carries the echoed term.
	linkedBody := servePage(t, buildMux(), "/roadmaps/payments-search/tasks?size=10&q=%FF")
	linked := parseList(t, linkedBody)
	if len(linked.sizeLinks) != 4 {
		t.Fatalf("q=%%FF renders %d rows-per-page links, want 4", len(linked.sizeLinks))
	}
	for _, link := range linked.sizeLinks {
		if !strings.Contains(link.href, "q=%EF%BF%BD") || strings.Contains(link.href, "%FF") {
			t.Errorf("the generated link %q does not carry the echoed term as %%EF%%BF%%BD", link.href)
		}
	}
	if !strings.Contains(linkedBody, `href="/roadmaps/payments-search/tasks?q=%EF%BF%BD"`) {
		t.Errorf("the 25-rows link does not carry exactly q=%%EF%%BF%%BD in the served HTML")
	}

	// The input shows the q the request carried, surrounding spaces included.
	s := fetchList(t, buildMux(), "/roadmaps/payments-search/tasks?q="+url.QueryEscape("  payout  "))
	if s.search != "  payout  " {
		t.Errorf("the search input shows %q, want the q the request carried", s.search)
	}
	if !slices.Equal(s.ids, searchIDsOf(tasks, 0)) {
		t.Errorf("a term with surrounding spaces lists %v, want the payout task", s.ids)
	}
}

// TestTaskSearch_TermIsEscaped is the gate for Acceptance Criterion 106: a term
// holding markup is echoed as visible characters through html/template and
// introduces no element, attribute, or script; every generated link carries it
// percent-encoded. The test fails if the term reaches the page as markup.
func TestTaskSearch_TermIsEscaped(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const title = `Render the "<b>bold</b> & more" banner`
	seedSearchFixture(t, "payments-search", []searchTask{{title: title}})

	const term = `<b>bold</b> & more`
	for _, raw := range []string{term, `"><script>alert(1)</script>`} {
		body := servePage(t, buildMux(), "/roadmaps/payments-search/tasks?size=10&q="+url.QueryEscape(raw))
		if strings.Contains(body, raw) || strings.Contains(body, "<script>alert") || strings.Contains(body, "<b>bold</b>") {
			t.Errorf("the term %q reached the page as markup", raw)
		}
		if !strings.Contains(body, `name="q" placeholder="Search" value="`+html.EscapeString(raw)) {
			t.Errorf("the search input does not echo the escaped term %q", raw)
		}
	}
	s := fetchList(t, buildMux(), "/roadmaps/payments-search/tasks?size=10&q="+url.QueryEscape(term))
	if len(s.ids) != 1 {
		t.Fatalf("the markup term lists %d tasks, want the one title holding it", len(s.ids))
	}
	encoded := url.QueryEscape(term)
	for _, link := range []string{s.sizeLinks[0].href, s.pageItems[0].href} {
		if link == "" {
			continue
		}
		if !strings.Contains(link, "q="+encoded) {
			t.Errorf("the generated link %q does not carry the percent-encoded term %q", link, encoded)
		}
	}
}

// TestSearchFold_IsTheSimpleLowercaseMapping is the gate for Acceptance Criterion
// 118: the fold maps each code point on its own by the simple lowercase mapping —
// U+0130 to U+0069 alone, U+03A3 to U+03C3 in every position, U+03C2 to itself,
// letter for letter for ASCII and accented Latin, never changing the length in
// code points — and a term of οδός finds a task titled οδός while ΟΔΟΣ finds ΟΔΟΣ.
func TestSearchFold_IsTheSimpleLowercaseMapping(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"A", "a"}, {"Á", "á"}, {"\u0130", "i"},
		{"ΟΔΟΣ", "οδοσ"},
		{"ΣΟΣ", "σοσ"},
		{"οδός", "οδός"},
		{"\xff", "\uFFFD"},
	} {
		if got := foldSearch(c.in); got != c.want {
			t.Errorf("foldSearch(%+q) = %+q, want %+q", c.in, got, c.want)
		}
	}
	// One code point in, one out, unconditionally, over the whole of Unicode, and
	// equal to the simple mapping unicode.ToLower publishes.
	for cp := rune(0); cp <= unicode.MaxRune; cp++ {
		if !utf8.ValidRune(cp) {
			continue
		}
		got := foldSearch(string(cp))
		if r, size := utf8.DecodeRuneInString(got); size != len(got) || r != unicode.ToLower(cp) {
			t.Fatalf("U+%04X folds to %+q, want the single code point U+%04X", cp, got, unicode.ToLower(cp))
		}
	}

	t.Setenv("HOME", shortHome(t))
	tasks := seedSearchFixture(t, "greek-roads", []searchTask{
		{title: "Survey the οδός network"},
		{title: "Survey the ΟΔΟΣ network"},
	})
	if got := searchIDs(t, "greek-roads", q("οδός")); !slices.Equal(got, searchIDsOf(tasks, 0)) {
		t.Errorf("the term οδός lists %v, want the task titled οδός", got)
	}
	if got := searchIDs(t, "greek-roads", q("ΟΔΟΣ")); !slices.Equal(got, searchIDsOf(tasks, 1)) {
		t.Errorf("the term ΟΔΟΣ lists %v, want the task titled ΟΔΟΣ", got)
	}
}

// TestSearchFold_OneFunctionForTheTermAndTheText is the gate for Acceptance
// Criterion 119: the term and the searchable text are folded by one function, so
// substituting it changes the verdict for both alike; and no script the binary
// serves folds, trims, or normalises a term — GET /static/task-search.js is 404
// and no asset carries the search's tables.
func TestSearchFold_OneFunctionForTheTermAndTheText(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	tasks := seedSearchFixture(t, "payments-search", []searchTask{
		{title: "Invalidate the settlement cache"},
		{title: "Rotate the acquirer credentials"},
	})

	if got := searchIDs(t, "payments-search", q("kache")); len(got) != 0 {
		t.Fatalf("kache lists %v under the real fold; the substitution below would prove nothing", got)
	}
	// A substitute that also rewrites c to k: a term and a text folded by it match
	// only if BOTH went through it.
	real := searchFold
	searchFold = func(s string) string { return strings.ReplaceAll(strings.ToLower(s), "c", "k") }
	got := searchIDs(t, "payments-search", q("kache"))
	gotC := searchIDs(t, "payments-search", q("Cache"))
	searchFold = real
	if !slices.Equal(got, searchIDsOf(tasks, 0)) || !slices.Equal(gotC, searchIDsOf(tasks, 0)) {
		t.Errorf("under the substitute fold, kache lists %v and Cache %v, want the cache task for both: "+
			"the term and the searchable text are not folded by the one function", got, gotC)
	}

	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/task-search.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /static/task-search.js: status = %d, want 404", rec.Code)
	}
	assets, err := fs.Glob(staticFS, "static/*")
	if err != nil {
		t.Fatalf("listing the embedded assets: %v", err)
	}
	for _, asset := range assets {
		if strings.HasSuffix(asset, "/vendor") {
			continue
		}
		content := readEmbeddedAsset(t, asset)
		for _, marker := range []string{"FOLD_TABLE", "SPACE_TABLE", "DECOMP_TABLE", "CCC_TABLE", "COMPOSE_TABLE", ".normalize(", "task-filter"} {
			if strings.Contains(content, marker) {
				t.Errorf("the embedded asset %s carries %q; no asset prepares or reads a search term", asset, marker)
			}
		}
	}
}

// TestSearchTrim_IsTheWhiteSpaceProperty is the gate for Acceptance Criterion 121:
// the trim removes exactly the code points carrying White_Space, from the two ends
// only, before the term is normalised and folded — U+0085 is removed and U+FEFF is
// not — whitespace inside the term is matched literally, and a task's own leading
// whitespace is part of its searchable text.
func TestSearchTrim_IsTheWhiteSpaceProperty(t *testing.T) {
	for cp := rune(0); cp <= unicode.MaxRune; cp++ {
		if !utf8.ValidRune(cp) {
			continue
		}
		if got, want := isSearchSpace(cp), unicode.Is(unicode.White_Space, cp); got != want {
			t.Fatalf("U+%04X: isSearchSpace = %v, White_Space = %v", cp, got, want)
		}
	}
	for _, c := range []struct{ raw, want string }{
		{"\u0085cache\u0085", "cache"},
		{"\uFEFFcache", "\uFEFFcache"},
		{" \t\r\nsettlement  cache \n", "settlement  cache"},
		{"  CafE\u0301", "café"},
		{"\u2003\u00A0", ""},
	} {
		if got := foldSearchTerm(c.raw); got != c.want {
			t.Errorf("foldSearchTerm(%+q) = %+q, want %+q", c.raw, got, c.want)
		}
	}
	if got := searchableText(" Rotate keys "); got != " rotate keys " {
		t.Errorf("the searchable text of a title is trimmed: %+q", got)
	}

	t.Setenv("HOME", shortHome(t))
	tasks := seedSearchFixture(t, "payments-search", []searchTask{
		{title: "Invalidate the settlement cache"},
		{title: "Invalidate the settlement  cache twice"},
	})
	if got := searchIDs(t, "payments-search", q("\u0085settlement cache\u0085")); !slices.Equal(got, searchIDsOf(tasks, 0)) {
		t.Errorf("a term ending in U+0085 lists %v, want the task it names once trimmed", got)
	}
	if got := searchIDs(t, "payments-search", q("\uFEFFsettlement")); len(got) != 0 {
		t.Errorf("a term with a leading U+FEFF lists %v, want nothing: U+FEFF is not White_Space", got)
	}
	if got := searchIDs(t, "payments-search", q("settlement  cache")); !slices.Equal(got, searchIDsOf(tasks, 1)) {
		t.Errorf("a term with two inner spaces lists %v, want only the title holding them", got)
	}
}

// TestTaskSearch_ShipsNoClientCopy is the gate for Acceptance Criteria 122 and
// 155: the page references no task-search.js, the embedded asset set holds none,
// and no script of the page reads the search input or rewrites the URL.
func TestTaskSearch_ShipsNoClientCopy(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedSearchFixture(t, "payments-search", []searchTask{{title: "Invalidate the settlement cache"}})
	body := servePage(t, buildMux(), "/roadmaps/payments-search/tasks?q=cache")
	if strings.Contains(body, "task-search") {
		t.Errorf("the tasks page references task-search")
	}
	if _, err := fs.Stat(staticFS, "static/task-search.js"); err == nil {
		t.Errorf("static/task-search.js is still embedded")
	}
	scripts, err := fs.Glob(staticFS, "static/*.js")
	if err != nil {
		t.Fatalf("listing the embedded scripts: %v", err)
	}
	for _, script := range scripts {
		content := readEmbeddedAsset(t, script)
		for _, marker := range []string{"replaceState", `name="q"`, "task-filter", "data-search"} {
			if strings.Contains(content, marker) {
				t.Errorf("%s carries %q", script, marker)
			}
		}
	}
}

// TestSearchNormalisation_IsTwoPassesInThisOrder is the gate for Acceptance
// Criterion 152: over every sequence of a folding code point followed by a
// non-starter — 1,440,384 of them — one pass leaves 70 outside NFC and two passes
// leave all of them in NFC; the two orders of normalising and folding differ on no
// single code point and on 74 sequences over 32 leading code points; and of
// Unicode's code points exactly 1,117 prepare differently with the normalisation
// than without it, none of them ASCII. The figures are the specification's own,
// so a change of Unicode data fails here.
func TestSearchNormalisation_IsTwoPassesInThisOrder(t *testing.T) {
	var folding, nonStarters []rune
	changed := 0
	for cp := rune(0); cp <= unicode.MaxRune; cp++ {
		if !utf8.ValidRune(cp) {
			continue
		}
		s := string(cp)
		if foldSearch(s) != s {
			folding = append(folding, cp)
		}
		if norm.NFD.PropertiesString(s).CCC() != 0 {
			nonStarters = append(nonStarters, cp)
		}
		if searchableText(s) != foldSearch(s) {
			changed++
			if cp < utf8.RuneSelf {
				t.Errorf("the ASCII code point U+%04X prepares differently under the normalisation", cp)
			}
		}
		if foldSearch(searchNFC(s)) != searchNFC(foldSearch(s)) {
			t.Errorf("U+%04X: the two orders differ on a single code point", cp)
		}
	}

	swept, onePassOutside, orderDiffers := 0, 0, 0
	leads := map[rune]bool{}
	for _, lead := range folding {
		for _, mark := range nonStarters {
			swept++
			sequence := string(lead) + string(mark)
			onePass := foldSearch(searchNFC(sequence))
			if searchNFC(onePass) != onePass {
				onePassOutside++
			}
			if twoPasses := searchableText(sequence); searchNFC(twoPasses) != twoPasses {
				t.Fatalf("U+%04X U+%04X prepares to %+q, which is not in NFC", lead, mark, twoPasses)
			}
			if foldSearch(searchNFC(sequence)) != searchNFC(foldSearch(sequence)) {
				orderDiffers++
				leads[lead] = true
			}
		}
	}
	for what, c := range map[string][2]int{
		"swept sequences":                   {swept, 1440384},
		"sequences one pass leaves outside": {onePassOutside, 70},
		"sequences the order changes":       {orderDiffers, 74},
		"leading code points of those":      {len(leads), 32},
		"code points the rule changes":      {changed, 1117},
	} {
		if c[0] != c[1] {
			t.Errorf("%s: measured %d, the specification states %d", what, c[0], c[1])
		}
	}

	for _, c := range []struct{ raw, want string }{
		{"H\u0331", "ẖ"}, {"ẖ", "ẖ"}, {"\u0130", "i"}, {"I\u0307", "i"},
	} {
		if got := foldSearchTerm(c.raw); got != c.want {
			t.Errorf("foldSearchTerm(%+q) = %+q, want %+q", c.raw, got, c.want)
		}
	}
}

// TestSearchNormalisation_EitherSpellingFindsEither is the gate for Acceptance
// Criteria 152 and 153: a title stored decomposed and a title stored precomposed
// are both found by a term typed in either spelling — all four combinations — and
// a title spelled U+0130 and one spelled U+0049 U+0307 carry one searchable text;
// the stored bytes are unchanged and the row renders them as stored.
func TestSearchNormalisation_EitherSpellingFindsEither(t *testing.T) {
	const (
		precomposed = "Café Lisboa onboarding"
		decomposed  = "Cafe\u0301 Porto onboarding"
	)
	t.Setenv("HOME", shortHome(t))
	tasks := seedSearchFixture(t, "cafe-roadmap", []searchTask{
		{title: precomposed},
		{title: decomposed},
		{title: "\u0130stanbul settlement"},
		{title: "I\u0307zmir settlement"},
		{title: "Cafeteria rota"},
	})

	for _, term := range []string{"café", "cafe\u0301", "CAFÉ", "CAFE\u0301"} {
		if got := searchIDs(t, "cafe-roadmap", q(term)); !slices.Equal(got, searchIDsOf(tasks, 0, 1)) {
			t.Errorf("the term %+q lists %v, want both spellings of café %v", term, got, searchIDsOf(tasks, 0, 1))
		}
	}
	// NFC and not NFD: cafe does not find café.
	if got := searchIDs(t, "cafe-roadmap", q("cafe")); !slices.Equal(got, searchIDsOf(tasks, 4)) {
		t.Errorf("the term cafe lists %v, want only the unaccented Cafeteria title", got)
	}
	if searchableText("\u0130") != searchableText("I\u0307") {
		t.Errorf("U+0130 and U+0049 U+0307 prepare to %+q and %+q", searchableText("\u0130"), searchableText("I\u0307"))
	}
	for _, term := range []string{"\u0130", "I\u0307"} {
		if got := searchIDs(t, "cafe-roadmap", q(term+"stanbul")); !slices.Equal(got, searchIDsOf(tasks, 2)) {
			t.Errorf("the term %+q lists %v, want the Istanbul task", term+"stanbul", got)
		}
	}

	// Normalisation is for comparison only.
	body := servePage(t, buildMux(), "/roadmaps/cafe-roadmap/tasks")
	for _, task := range tasks {
		if got := storedTask(t, "cafe-roadmap", task.id).Title; got != task.title {
			t.Errorf("task #%d's stored title changed from %+q to %+q", task.id, task.title, got)
		}
		if !strings.Contains(body, ">"+html.EscapeString(task.title)+"</a>") {
			t.Errorf("task #%d's row does not render its stored title %+q", task.id, task.title)
		}
	}
}

// TestSearchNormalisation_IsTheModulesNFC is the gate for Acceptance Criterion
// 155: the search normalises through the one function that returns
// golang.org/x/text/unicode/norm's Normalization Form C.
func TestSearchNormalisation_IsTheModulesNFC(t *testing.T) {
	for _, s := range []string{"Cafe\u0301", "\u0958", "he\u0302\u0323", "I\u0307", "plain ascii", "ẖ", "H\u0331"} {
		if got, want := searchNFC(s), norm.NFC.String(s); got != want {
			t.Errorf("searchNFC(%+q) = %+q, want the module's %+q", s, got, want)
		}
	}
}
