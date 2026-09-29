package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for the tasks page's filter state: the multi-value status
// and type filters, the defaults, the filter-state cookie, and the route's
// caching (SPEC/WEB.md § Roadmap Tasks Page, Query parameters, Filter persistence,
// and Empty states; § Cache Policy, rule 5; Acceptance Criteria 248 to 254).
//
// Every expectation is computed from the fixture's own record of its tasks, never
// read back from the page under test.

// tasksResponse is one response of the tasks route: its status, its headers, its
// body, and the page it states when it is a 200.
type tasksResponse struct {
	header http.Header
	body   string
	list   servedList
	status int
}

// serveTasks drives one request through the fully wired handler — the security
// headers included — with the given Cookie header value, when not empty.
func serveTasks(t *testing.T, srv http.Handler, method, path, cookieHeader string, extra ...string) tasksResponse {
	t.Helper()

	req := httptest.NewRequest(method, path, nil)
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header.Set(extra[i], extra[i+1])
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	res := tasksResponse{status: rec.Code, header: rec.Header(), body: rec.Body.String()}
	if res.status == http.StatusOK && method == http.MethodGet && strings.Contains(res.body, `id="task-filter-q"`) {
		res.list = parseList(t, res.body)
	}
	return res
}

// setCookies returns the response's Set-Cookie headers.
func (r *tasksResponse) setCookies() []string { return r.header.Values("Set-Cookie") }

// cookieValueOf returns the value the response's one Set-Cookie header sets for
// the filter-state cookie, failing unless there is exactly one.
func (r *tasksResponse) cookieValueOf(t *testing.T) string {
	t.Helper()

	cookies := r.setCookies()
	if len(cookies) != 1 {
		t.Fatalf("the response carries %d Set-Cookie headers, want exactly 1: %q", len(cookies), cookies)
	}
	pair, _, _ := strings.Cut(cookies[0], ";")
	name, value, _ := strings.Cut(pair, "=")
	if name != tasksFilterCookie {
		t.Fatalf("the Set-Cookie names %q, want %q", name, tasksFilterCookie)
	}
	return value
}

// cookieHeader is a Cookie request header carrying the filter-state cookie.
func cookieHeader(value string) string { return tasksFilterCookie + "=" + value }

// reCookieValue is the character set the cookie's value may hold: ASCII letters
// and digits and - . _ ~ % + & =, each a cookie-octet of RFC 6265.
var reCookieValue = regexp.MustCompile(`^[A-Za-z0-9\-._~%+&=]*$`)

// TestTasksFilterState_MultiValueFilters is the gate for Acceptance Criterion 248:
// status and type are OR within a dimension and AND across; a repeated value
// counts once; the dropdowns check exactly the requested values and their toggles
// read the value or "2 selected"; a second value never removes a task and a first
// value never adds one.
func TestTasksFilterState_MultiValueFilters(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 60)
	mux := buildMux()

	doingOrTesting := func(x listTask) bool {
		return x.status == models.StatusDoing || x.status == models.StatusTesting
	}
	bugOrEpic := func(x listTask) bool { return x.taskType == models.TypeBug || x.taskType == models.TypeEpic }

	for _, c := range []struct {
		params             url.Values
		keep               func(listTask) bool
		statuses, types    []string
		statusText, typeTx string
	}{
		{url.Values{"status": {"DOING", "TESTING"}}, doingOrTesting,
			[]string{"DOING", "TESTING"}, nil, "2 selected", "Any type"},
		{url.Values{"status": {"DOING", "TESTING"}, "type": {"BUG", "EPIC"}},
			func(x listTask) bool { return doingOrTesting(x) && bugOrEpic(x) },
			[]string{"DOING", "TESTING"}, []string{"EPIC", "BUG"}, "2 selected", "2 selected"},
		{url.Values{"status": {"DOING", "DOING"}}, func(x listTask) bool { return x.status == models.StatusDoing },
			[]string{"DOING"}, nil, "DOING", "Any type"},
	} {
		got := allPages(t, mux, f.name, c.params, 100)
		want := f.expect(c.keep)
		if len(want) == 0 || !slices.Equal(got, want) {
			t.Errorf("%v lists %v, want the non-empty %v", c.params, got, want)
		}
		s := fetchList(t, mux, listPath(f.name, c.params))
		// The menu checks its boxes in the enum's order, whatever order the request
		// named the values in.
		wantTypes := []string{}
		for _, tt := range models.ValidTaskTypes {
			if slices.Contains(c.types, string(tt)) {
				wantTypes = append(wantTypes, string(tt))
			}
		}
		if !slices.Equal(s.checked(t, "status"), c.statuses) || s.toggleText(t, "status") != c.statusText {
			t.Errorf("%v: the status dropdown checks %v and reads %q, want %v and %q",
				c.params, s.checked(t, "status"), s.toggleText(t, "status"), c.statuses, c.statusText)
		}
		if !slices.Equal(s.checked(t, "type"), wantTypes) || s.toggleText(t, "type") != c.typeTx {
			t.Errorf("%v: the type dropdown checks %v and reads %q, want %v and %q",
				c.params, s.checked(t, "type"), s.toggleText(t, "type"), wantTypes, c.typeTx)
		}
	}
	if single, double := allPages(t, mux, f.name, url.Values{"status": {"DOING"}}, 100),
		allPages(t, mux, f.name, url.Values{"status": {"DOING", "DOING"}}, 100); !slices.Equal(single, double) {
		t.Errorf("status=DOING&status=DOING lists %v, want what status=DOING lists %v", double, single)
	}

	// Monotonicity: a second value of a dimension only grows the list, a first
	// value of a dimension only shrinks it.
	contains := func(super, sub []int) bool {
		for _, id := range sub {
			if !slices.Contains(super, id) {
				return false
			}
		}
		return true
	}
	for _, pair := range [][2]url.Values{
		{{"status": {"DOING"}}, {"status": {"DOING", "TESTING"}}},
		{{"type": {"BUG"}, "sprint": {"none"}}, {"type": {"BUG", "EPIC"}, "sprint": {"none"}}},
		{{"status": {"SPRINT"}, "type": {"TASK"}}, {"status": {"SPRINT", "BACKLOG"}, "type": {"TASK"}}},
	} {
		narrow, wide := allPages(t, mux, f.name, pair[0], 100), allPages(t, mux, f.name, pair[1], 100)
		if !contains(wide, narrow) || len(wide) <= len(narrow) {
			t.Errorf("adding a second value (%v to %v) removed a task or added none: %v then %v", pair[0], pair[1], narrow, wide)
		}
	}
	every := allPages(t, mux, f.name, url.Values{}, 100)
	for _, params := range []url.Values{{"status": {"DOING"}}, {"type": {"BUG"}}, {"sprint": {"none"}, "type": {"TASK"}}} {
		if got := allPages(t, mux, f.name, params, 100); !contains(every, got) || len(got) >= len(every) {
			t.Errorf("adding a first value %v added a task or removed none: %d of %d", params, len(got), len(every))
		}
	}
}

// TestTasksFilterState_DefaultsAndReset is the gate for Acceptance Criterion 249:
// a bare request with no cookie lists page 1 of every task that is not COMPLETED
// at 25 rows, with BACKLOG, SPRINT, DOING and TESTING checked, the status toggle
// reading "4 selected", no type checked and "Any type", Any sprint, an empty
// search input, and no Set-Cookie; following the no-match Reset link lists the
// same default list and sets the cookie to the defaults written out.
func TestTasksFilterState_DefaultsAndReset(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 60)
	srv := handler()

	notCompleted := f.expect(func(x listTask) bool { return x.status != models.StatusCompleted })
	if len(notCompleted) == len(f.tasks) || len(notCompleted) <= 25 {
		t.Fatalf("the fixture has %d tasks not COMPLETED of %d; the defaults would exclude nothing or fit one page",
			len(notCompleted), len(f.tasks))
	}
	for _, path := range []string{listPath(f.name, nil), "/roadmaps/" + f.name + "/tasks?priority=3&severity=1"} {
		res := serveTasks(t, srv, http.MethodGet, path, "")
		s := res.list
		if res.status != http.StatusOK || !slices.Equal(s.ids, notCompleted[:25]) || s.total != len(notCompleted) || s.currentPage(t) != 1 {
			t.Errorf("%s: status %d lists %v (%d in all, page %d), want page 1 of the %d tasks not COMPLETED",
				path, res.status, s.ids, s.total, s.currentPage(t), len(notCompleted))
		}
		if got := s.checked(t, "status"); !slices.Equal(got, []string{"BACKLOG", "SPRINT", "DOING", "TESTING"}) || s.toggleText(t, "status") != "4 selected" {
			t.Errorf("%s: the status dropdown checks %v and reads %q, want the four defaults and 4 selected", path, got, s.toggleText(t, "status"))
		}
		if got := s.checked(t, "type"); len(got) != 0 || s.toggleText(t, "type") != "Any type" {
			t.Errorf("%s: the type dropdown checks %v and reads %q, want nothing and Any type", path, got, s.toggleText(t, "type"))
		}
		if s.selected(t, "sprint") != "" || s.search != "" || s.hiddenSize != "25" {
			t.Errorf("%s: sprint %q, search %q, size %q; want Any sprint, empty, 25", path, s.selected(t, "sprint"), s.search, s.hiddenSize)
		}
		if got := res.setCookies(); len(got) != 0 {
			t.Errorf("%s: a bare request set the cookie: %q", path, got)
		}
	}

	noMatch := serveTasks(t, srv, http.MethodGet, listPath(f.name, url.Values{"q": {"no task is titled like this"}}), "")
	if noMatch.list.resetHref == "" {
		t.Fatalf("the no-match state carries no Reset link")
	}
	reset := serveTasks(t, srv, http.MethodGet, noMatch.list.resetHref, "")
	if !slices.Equal(reset.list.ids, notCompleted[:25]) || reset.list.total != len(notCompleted) {
		t.Errorf("following Reset lists %v (%d in all), want the default list", reset.list.ids, reset.list.total)
	}
	if got := reset.cookieValueOf(t); got != "status=BACKLOG&status=SPRINT&status=DOING&status=TESTING&size=25" {
		t.Errorf("following Reset sets the cookie %q, want the defaults written out", got)
	}
}

// TestTasksFilterState_ExplicitRequestSetsTheCookie is the gate for Acceptance
// Criterion 250: an explicit request's 200, to GET and to HEAD, carries exactly
// one Set-Cookie naming rmp_tasks_filters with exactly the attributes Path=/,
// Max-Age=31536000, HttpOnly, and SameSite=Lax, and the value of the accepted q,
// sprint, statuses and types in enum order, and size, never page; a term with a
// space, ;, , or " is percent-encoded into the value's character set; ?size=25 and
// a request of unaccepted occurrences alone set size=25; a 404 sets no cookie.
func TestTasksFilterState_ExplicitRequestSetsTheCookie(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 30)
	srv := handler()

	path := "/roadmaps/" + f.name + "/tasks?q=cache&sprint=" + itoa(f.sprintA) +
		"&status=TESTING&status=DOING&type=BUG&size=50&page=2"
	want := "q=cache&sprint=" + itoa(f.sprintA) + "&status=DOING&status=TESTING&type=BUG&size=50"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		res := serveTasks(t, srv, method, path, "")
		if res.status != http.StatusOK {
			t.Fatalf("%s %s: status %d, want 200", method, path, res.status)
		}
		if got := res.cookieValueOf(t); got != want {
			t.Errorf("%s: the cookie's value is %q, want %q", method, got, want)
		}
		attrs := strings.Split(res.setCookies()[0], "; ")[1:]
		slices.Sort(attrs)
		if wantAttrs := []string{"HttpOnly", "Max-Age=31536000", "Path=/", "SameSite=Lax"}; !slices.Equal(attrs, wantAttrs) {
			t.Errorf("%s: the cookie's attributes are %q, want exactly %q", method, attrs, wantAttrs)
		}
	}

	for _, c := range []struct{ query, want string }{
		{"size=25", "size=25"},
		{"status=doing", "size=25"},
		{"type=bug&sprint=007&page=3", "size=25"},
		{"sprint=none&size=100", "sprint=none&size=100"},
		{"q=" + url.QueryEscape(`settle; "cache", now`) + "&type=EPIC&type=BUG",
			"q=settle%3B+%22cache%22%2C+now&type=BUG&type=EPIC&size=25"},
		{"q=" + url.QueryEscape("   ") + "&size=10", "size=10"},
		{"q=%FF", "q=%EF%BF%BD&size=25"},
	} {
		res := serveTasks(t, srv, http.MethodGet, "/roadmaps/"+f.name+"/tasks?"+c.query, "")
		got := res.cookieValueOf(t)
		if got != c.want {
			t.Errorf("?%s sets the cookie %q, want %q", c.query, got, c.want)
		}
		if !reCookieValue.MatchString(got) {
			t.Errorf("?%s: the cookie's value %q holds a character outside the specified set", c.query, got)
		}
	}

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		res := serveTasks(t, srv, method, "/roadmaps/no-such-roadmap/tasks?status=DOING&size=50", "")
		if res.status != http.StatusNotFound || len(res.setCookies()) != 0 {
			t.Errorf("%s of an unknown roadmap: status %d with Set-Cookie %q, want 404 and none", method, res.status, res.setCookies())
		}
	}
}

// TestTasksFilterState_BareRequestRestoresTheCookie is the gate for Acceptance
// Criterion 251: a bare request carrying the cookie an explicit request set lists
// page 1 of that request's list at its size, every control showing that state,
// and sets no cookie; a query of unaccepted parameters alone is bare; a second
// roadmap without the cookie's sprint ignores the sprint alone; unaccepted,
// undecodable, and page parts of a cookie are ignored one by one; a cookie with no
// accepted part lists every task, not the defaults; the first of two cookies of
// the name is read.
func TestTasksFilterState_BareRequestRestoresTheCookie(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 90)
	other := seedListFixture(t, "treasury-platform", 12)
	srv := handler()

	// Sprint ids restart per roadmap, so the other roadmap's sprints are 1 and 2;
	// a third sprint here gives this roadmap an id the other roadmap lacks.
	database, err := db.Open(f.name)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	extra := newSprint(t, database, "Chargeback automation", "Automate the chargeback responses.")
	_ = database.Close()
	if extra == other.sprintA || extra == other.sprintB {
		t.Fatalf("sprint %d exists in the other roadmap too", extra)
	}

	explicit := url.Values{"q": {"the"}, "sprint": {itoa(f.sprintA)}, "status": {"SPRINT", "DOING", "TESTING", "BACKLOG"}, "size": {"10"}, "page": {"2"}}
	set := serveTasks(t, srv, http.MethodGet, listPath(f.name, explicit), "")
	stored := set.cookieValueOf(t)
	keep := func(x listTask) bool {
		return strings.Contains(strings.ToLower(x.title), "the") && x.sprint == f.sprintA && x.status != models.StatusCompleted
	}
	want := f.expect(keep)
	if len(want) <= 10 {
		t.Fatalf("the explicit list holds %d tasks; it must span more than one page", len(want))
	}

	for _, path := range []string{listPath(f.name, nil), "/roadmaps/" + f.name + "/tasks?priority=3"} {
		res := serveTasks(t, srv, http.MethodGet, path, cookieHeader(stored))
		s := res.list
		if res.status != http.StatusOK || !slices.Equal(s.ids, want[:10]) || s.total != len(want) || s.currentPage(t) != 1 {
			t.Errorf("%s with the cookie lists %v (%d in all, page %d), want page 1 of %v at 10 rows",
				path, s.ids, s.total, s.currentPage(t), want)
		}
		if s.search != "the" || s.selected(t, "sprint") != itoa(f.sprintA) || s.hiddenSize != "10" ||
			!slices.Equal(s.checked(t, "status"), []string{"BACKLOG", "SPRINT", "DOING", "TESTING"}) || s.toggleText(t, "status") != "4 selected" {
			t.Errorf("%s: the controls do not show the stored state: search %q sprint %q size %q statuses %v",
				path, s.search, s.selected(t, "sprint"), s.hiddenSize, s.checked(t, "status"))
		}
		if got := res.setCookies(); len(got) != 0 {
			t.Errorf("%s: a bare request rewrote the cookie: %q", path, got)
		}
		// Every generated link carries the restored state explicitly.
		for _, link := range generatedLinks(&s) {
			u, _ := url.Parse(link)
			if u.Query().Get("sprint") != itoa(f.sprintA) || u.Query().Get("q") != "the" || len(u.Query()["status"]) != 4 {
				t.Errorf("%s: the link %q does not carry the restored filters", path, link)
			}
		}
	}

	// The same cookie on a roadmap with no sprint of that id: sprint ignored,
	// everything else applied.
	foreign := cookieHeader("q=the&sprint=" + itoa(extra) + "&status=BACKLOG&status=SPRINT&type=TASK&type=EPIC&size=50")
	res := serveTasks(t, srv, http.MethodGet, listPath(other.name, nil), foreign)
	wantOther := other.expect(func(x listTask) bool {
		return strings.Contains(strings.ToLower(x.title), "the") &&
			(x.status == models.StatusBacklog || x.status == models.StatusSprint) &&
			(x.taskType == models.TypeTask || x.taskType == models.TypeEpic)
	})
	if !slices.Equal(res.list.ids, wantOther) || res.list.selected(t, "sprint") != "" || res.list.hiddenSize != "50" {
		t.Errorf("a foreign sprint in the cookie: lists %v with sprint %q and size %q, want %v with Any sprint at 50",
			res.list.ids, res.list.selected(t, "sprint"), res.list.hiddenSize, wantOther)
	}
	if len(wantOther) == 0 {
		t.Fatalf("the other roadmap admits no task under the cookie; the check proves nothing")
	}

	// Unaccepted, undecodable, and page parts are ignored one by one.
	doing := f.expect(func(x listTask) bool { return x.status == models.StatusDoing })
	junk := "status=doing&type=BUG,EPIC&sprint=007&size=20&page=3&q=%zz&status=DOING"
	res = serveTasks(t, srv, http.MethodGet, listPath(f.name, nil), cookieHeader(junk))
	if res.status != http.StatusOK || !slices.Equal(res.list.ids, doing[:min(25, len(doing))]) || res.list.total != len(doing) ||
		res.list.hiddenSize != "25" || res.list.currentPage(t) != 1 || res.list.search != "" {
		t.Errorf("the junk cookie lists %v (%d in all, size %s, page %d), want page 1 of the %d DOING tasks at 25",
			res.list.ids, res.list.total, res.list.hiddenSize, res.list.currentPage(t), len(doing))
	}
	// A cookie present with no accepted part gives no filter, not the defaults.
	for _, value := range []string{"", "status=doing&page=4", "assignee=alice", "%zz"} {
		res = serveTasks(t, srv, http.MethodGet, listPath(f.name, nil), cookieHeader(value))
		if res.status != http.StatusOK || res.list.total != len(f.tasks) || len(res.list.checked(t, "status")) != 0 {
			t.Errorf("the cookie %q lists %d tasks with statuses %v checked, want every one of %d and none checked",
				value, res.list.total, res.list.checked(t, "status"), len(f.tasks))
		}
	}
	// The first occurrence of the name is read, whatever its content.
	backlog := f.expect(func(x listTask) bool { return x.status == models.StatusBacklog })
	for _, header := range []string{
		cookieHeader("status=DOING&size=100") + "; " + cookieHeader("status=BACKLOG&size=100"),
		"theme=dark; " + cookieHeader(`status=DOING&q=\&size=100`) + "; " + cookieHeader("status=BACKLOG&size=100"),
		`other="x"; ` + cookieHeader(`"status=DOING&size=100"`) + "; " + cookieHeader("status=BACKLOG&size=100"),
	} {
		res = serveTasks(t, srv, http.MethodGet, listPath(f.name, nil), header)
		if !slices.Equal(res.list.checked(t, "status"), []string{"DOING"}) {
			t.Errorf("Cookie %q applies the statuses %v, want the first occurrence's DOING", header, res.list.checked(t, "status"))
		}
	}
	if len(backlog) == 0 || len(doing) == 0 {
		t.Fatalf("the fixture lacks BACKLOG or DOING tasks")
	}
}

// TestTasksFilterState_OversizedValueIsNotWritten is the gate for Acceptance
// Criterion 252: an encoded value longer than 4000 bytes is not written, the page
// still rendering the URL's list, and a following bare request with the browser's
// earlier cookie lists what that cookie states; a value of exactly 4000 bytes is
// written.
func TestTasksFilterState_OversizedValueIsNotWritten(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 30)
	srv := handler()
	earlier := cookieHeader("status=DOING&size=25")

	// "q=" + term + "&size=25" is 10 bytes plus the term, whose letters need no
	// escape.
	exact := strings.Repeat("a", maxTasksFilterCookieLen-10)
	res := serveTasks(t, srv, http.MethodGet, listPath(f.name, url.Values{"q": {exact}}), earlier)
	if got := res.cookieValueOf(t); len(got) != maxTasksFilterCookieLen {
		t.Errorf("a %d-byte value is written as %d bytes, want it written whole", maxTasksFilterCookieLen, len(got))
	}

	over := exact + "a"
	res = serveTasks(t, srv, http.MethodGet, listPath(f.name, url.Values{"q": {over}, "status": {"BACKLOG"}}), earlier)
	if res.status != http.StatusOK || len(res.setCookies()) != 0 {
		t.Errorf("a value over %d bytes: status %d with %d Set-Cookie headers, want 200 and none", maxTasksFilterCookieLen, res.status, len(res.setCookies()))
	}
	if res.list.search != over || !slices.Equal(res.list.checked(t, "status"), []string{"BACKLOG"}) || res.list.emptyTitle != "No task matches the filters" {
		t.Errorf("the oversized request does not render the list its URL names")
	}
	// A multi-byte term is measured encoded: 700 × "é" is 1400 bytes of UTF-8 and
	// 4200 bytes percent-encoded, so the value exceeds the limit.
	res = serveTasks(t, srv, http.MethodGet, listPath(f.name, url.Values{"q": {strings.Repeat("é", 700)}}), earlier)
	if len(res.setCookies()) != 0 {
		t.Errorf("a term whose ENCODED value exceeds 4000 bytes set the cookie")
	}

	doing := f.expect(func(x listTask) bool { return x.status == models.StatusDoing })
	res = serveTasks(t, srv, http.MethodGet, listPath(f.name, nil), earlier)
	if !slices.Equal(res.list.ids, doing) || len(doing) == 0 {
		t.Errorf("the following bare request lists %v, want the earlier cookie's DOING tasks %v", res.list.ids, doing)
	}
}

// TestTasksFilterState_VaryAndNoCache is the gate for Acceptance Criterion 253:
// every response of the route — explicit, bare with and without the cookie, and a
// 404 — carries Vary: Cookie and Cache-Control: no-store and no ETag or
// Last-Modified; If-None-Match and a future If-Modified-Since are answered 200 in
// full; a POST is answered 405 with no Vary header; and over a real server each HEAD response carries the header names and
// values of its GET, Set-Cookie included, and no body.
func TestTasksFilterState_VaryAndNoCache(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 30)
	srv := handler()
	future := time.Now().Add(24 * time.Hour).UTC().Format(http.TimeFormat)

	type probe struct{ path, cookie string }
	probes := []probe{
		{listPath(f.name, url.Values{"status": {"DOING", "TESTING"}, "size": {"10"}}), ""},
		{listPath(f.name, nil), ""},
		{listPath(f.name, nil), cookieHeader("status=DOING&size=25")},
		{"/roadmaps/no-such-roadmap/tasks?status=DOING", ""},
		{"/roadmaps/no-such-roadmap/tasks", cookieHeader("status=DOING&size=25")},
	}
	for _, p := range probes {
		for _, conditional := range [][]string{nil, {"If-None-Match", "*"}, {"If-Modified-Since", future}} {
			res := serveTasks(t, srv, http.MethodGet, p.path, p.cookie, conditional...)
			if got := res.header.Values("Vary"); !slices.Contains(got, "Cookie") {
				t.Errorf("%s (cookie %q, %v): Vary = %q, want Cookie", p.path, p.cookie, conditional, got)
			}
			if got := res.header.Get("Cache-Control"); got != "no-store" {
				t.Errorf("%s: Cache-Control = %q, want no-store", p.path, got)
			}
			if res.header.Get("ETag") != "" || res.header.Get("Last-Modified") != "" {
				t.Errorf("%s: the response carries a validator", p.path)
			}
			wantStatus := http.StatusOK
			if strings.Contains(p.path, "no-such-roadmap") {
				wantStatus = http.StatusNotFound
			}
			if res.status != wantStatus || (wantStatus == http.StatusOK && !strings.Contains(res.body, "</html>")) {
				t.Errorf("%s %v: status %d, want %d with the full page", p.path, conditional, res.status, wantStatus)
			}
		}
	}

	// Vary: Cookie belongs to the GET and HEAD handling alone: a POST to the path
	// is answered 405 by the fallback handler, with no Vary header and no cookie.
	for _, cookie := range []string{"", cookieHeader("status=DOING&size=25")} {
		res := serveTasks(t, srv, http.MethodPost, listPath(f.name, url.Values{"status": {"DOING"}}), cookie)
		if res.status != http.StatusMethodNotAllowed || len(res.header.Values("Vary")) != 0 || len(res.setCookies()) != 0 {
			t.Errorf("POST (cookie %q): status %d, Vary %q, Set-Cookie %q; want 405 with neither header",
				cookie, res.status, res.header.Values("Vary"), res.setCookies())
		}
	}

	server := httptest.NewServer(srv)
	defer server.Close()
	client := server.Client()
	type served struct {
		header http.Header
		body   string
		status int
	}
	do := func(method string, p probe) served {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+p.path, nil)
		if err != nil {
			t.Fatalf("building %s %s: %v", method, p.path, err)
		}
		if p.cookie != "" {
			req.Header.Set("Cookie", p.cookie)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, p.path, err)
		}
		defer resp.Body.Close() //nolint:errcheck // test response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("reading %s %s: %v", method, p.path, err)
		}
		return served{header: resp.Header, body: string(body), status: resp.StatusCode}
	}
	for _, p := range probes {
		get := do(http.MethodGet, p)
		head := do(http.MethodHead, p)
		if head.status != get.status || head.body != "" {
			t.Errorf("HEAD %s: status %d with a %d-byte body, want GET's %d and no body", p.path, head.status, len(head.body), get.status)
		}
		for _, h := range []http.Header{get.header, head.header} {
			h.Del("Date")
			h.Del("Content-Length")
		}
		for name := range get.header {
			if !slices.Equal(get.header.Values(name), head.header.Values(name)) {
				t.Errorf("%s: %s is %q on GET and %q on HEAD", p.path, name, get.header.Values(name), head.header.Values(name))
			}
		}
		for name := range head.header {
			if _, ok := get.header[name]; !ok {
				t.Errorf("%s: HEAD carries %s, which GET does not", p.path, name)
			}
		}
		if strings.Contains(p.path, "size=10") && len(get.header.Values("Set-Cookie")) != 1 {
			t.Errorf("%s: the explicit GET carries %d Set-Cookie headers, want 1", p.path, len(get.header.Values("Set-Cookie")))
		}
	}
}

// TestTasksFilterState_CookieIsDataAndGrantsNothing is the gate for Acceptance
// Criterion 254: a cookie's term is echoed escaped, introducing no element; its
// hostile parts are ignored; after every kind of request the roadmap holds the
// same tasks and the same audit entries; and the sidebar's Tasks entry and the
// task page's Back to tasks link carry no query string on every page.
func TestTasksFilterState_CookieIsDataAndGrantsNothing(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedListFixture(t, "payments-platform", 30)
	srv := handler()

	database, err := db.OpenReadOnly(f.name)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	auditBefore, err := database.CountAuditEntries(t.Context())
	if err != nil {
		t.Fatalf("counting audit entries: %v", err)
	}
	_ = database.Close()
	tasksBefore := countRoadmapTasks(t, f.name)

	markup := serveTasks(t, srv, http.MethodGet, listPath(f.name, nil), cookieHeader("q="+url.QueryEscape("<script>alert(1)</script>")+"&size=25"))
	if markup.list.search != "<script>alert(1)</script>" || strings.Contains(markup.body, "<script>alert") {
		t.Errorf("the cookie's markup term is not echoed escaped: search %q", markup.list.search)
	}
	all := f.expect(all)
	for _, hostile := range []string{
		"status=" + url.QueryEscape("DOING' OR '1'='1") + "&size=25",
		"sprint=" + url.QueryEscape("1 OR 1=1") + "&size=25",
		"type=" + url.QueryEscape("BUG;DROP TABLE tasks") + "&size=25",
	} {
		res := serveTasks(t, srv, http.MethodGet, listPath(f.name, nil), cookieHeader(hostile))
		if res.status != http.StatusOK || res.list.total != len(all) {
			t.Errorf("the hostile cookie %q: status %d lists %d tasks, want 200 and every one of %d", hostile, res.status, res.list.total, len(all))
		}
		for _, needle := range []string{"OR '1'", "OR 1=1", "DROP TABLE"} {
			if strings.Contains(res.body, needle) {
				t.Errorf("the hostile cookie %q is echoed into the page", hostile)
			}
		}
	}
	// Every kind of request of the preceding criteria, once more.
	for _, p := range []struct{ method, path, cookie string }{
		{http.MethodGet, listPath(f.name, url.Values{"status": {"DOING"}, "size": {"50"}}), ""},
		{http.MethodHead, listPath(f.name, url.Values{"q": {"cache"}}), ""},
		{http.MethodGet, listPath(f.name, nil), cookieHeader("status=DOING&size=25")},
		{http.MethodGet, listPath(f.name, nil), ""},
	} {
		serveTasks(t, srv, p.method, p.path, p.cookie)
	}
	if after := countRoadmapTasks(t, f.name); after != tasksBefore {
		t.Errorf("the roadmap held %d tasks before and %d after", tasksBefore, after)
	}
	database, err = db.OpenReadOnly(f.name)
	if err != nil {
		t.Fatalf("opening roadmap: %v", err)
	}
	auditAfter, err := database.CountAuditEntries(t.Context())
	_ = database.Close()
	if err != nil || auditAfter != auditBefore {
		t.Errorf("the roadmap held %d audit entries before and %d after (%v)", auditBefore, auditAfter, err)
	}

	// The sidebar's Tasks entry and the Back to tasks link are bare on every page.
	sidebar := `<a class="nav-link" href="/roadmaps/` + f.name + `/tasks"`
	for _, path := range []string{
		"/roadmaps/" + f.name,
		listPath(f.name, url.Values{"status": {"DOING"}}),
		"/roadmaps/" + f.name + "/tasks/" + itoa(f.tasks[0].id),
		"/roadmaps/" + f.name + "/sprints/" + itoa(f.sprintA),
		"/roadmaps/" + f.name + "/audit",
		"/roadmaps/" + f.name + "/graph",
	} {
		res := serveTasks(t, srv, http.MethodGet, path, "")
		if res.status != http.StatusOK {
			t.Fatalf("GET %s: status %d", path, res.status)
		}
		if !strings.Contains(res.body, sidebar) {
			t.Errorf("%s: the sidebar's Tasks entry is not the bare path", path)
		}
		if !strings.Contains(path, "/tasks?") && strings.Contains(res.body, "/tasks?") {
			t.Errorf("%s: a link to the tasks page carries a query string", path)
		}
	}
	task := serveTasks(t, srv, http.MethodGet, "/roadmaps/"+f.name+"/tasks/"+itoa(f.tasks[0].id), "")
	if !strings.Contains(task.body, `href="/roadmaps/`+f.name+`/tasks"><i class="ti ti-arrow-left me-1"></i>Back to tasks</a>`) {
		t.Errorf("the task page's Back to tasks link is not the bare path")
	}
}

// TestIsExplicitTasksQuery pins the explicit/bare classification: an occurrence
// of any of the six parameters, whatever its value, makes a request explicit —
// its name decoded first — and nothing else does (SPEC/WEB.md § Roadmap Tasks
// Page, Filter persistence, Explicit and bare requests).
func TestIsExplicitTasksQuery(t *testing.T) {
	for raw, want := range map[string]bool{
		"":                            false,
		"priority=3":                  false,
		"priority=3&severity=1&x=y":   false,
		"st%zzatus=DOING":             false,
		"statuses=DOING":              false,
		"&&":                          false,
		"q":                           true,
		"q=":                          true,
		"sprint=":                     true,
		"status=%zz":                  true,
		"type=bug":                    true,
		"page=0":                      true,
		"priority=3&size=abc":         true,
		"%73tatus=DOING":              true,
		"x=1&y=2&page":                true,
		"q=a;b":                       true,
		"assignee=alice&sort=title&q": true,
	} {
		if got := isExplicitTasksQuery(raw); got != want {
			t.Errorf("isExplicitTasksQuery(%q) = %v, want %v", raw, got, want)
		}
	}
}

// TestFirstCookieValue pins the cookie reader: the first occurrence of the name
// across every Cookie header, whatever its bytes, a DQUOTE-wrapped value
// unwrapped, and absence reported (SPEC/WEB.md § Roadmap Tasks Page, Filter
// persistence, Reading the cookie validates it like a URL).
func TestFirstCookieValue(t *testing.T) {
	for _, c := range []struct {
		lines []string
		value string
		found bool
	}{
		{nil, "", false},
		{[]string{"theme=dark; rmp_tasks_filter=x"}, "", false},
		{[]string{"rmp_tasks_filters=status=DOING&size=25"}, "status=DOING&size=25", true},
		{[]string{"a=1;rmp_tasks_filters=first; rmp_tasks_filters=second"}, "first", true},
		{[]string{"a=1", "rmp_tasks_filters=in-second-line"}, "in-second-line", true},
		{[]string{`rmp_tasks_filters=q=\"x; rmp_tasks_filters=later`}, `q=\"x`, true},
		{[]string{`rmp_tasks_filters="size=10"`}, "size=10", true},
		{[]string{"rmp_tasks_filters="}, "", true},
		{[]string{"  rmp_tasks_filters = size=50  "}, "size=50", true},
	} {
		header := http.Header{}
		for _, line := range c.lines {
			header.Add("Cookie", line)
		}
		value, found := firstCookieValue(header, tasksFilterCookie)
		if value != c.value || found != c.found {
			t.Errorf("%q: firstCookieValue = %q, %v; want %q, %v", c.lines, value, found, c.value, c.found)
		}
	}
}
