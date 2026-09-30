package web

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv/graphserver"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// fuzzWebRoadmap is the roadmap every request of FuzzWebQueryParameters names.
// The name is valid and the roadmap exists, so the only untrusted input a request
// carries is its query string.
const fuzzWebRoadmap = "query-parameter-fuzzing"

// fuzzGraphBudget is the statement budget the fuzz fixture's graph server
// enforces. A fuzzed statement can be arbitrarily expensive; the production 5 s
// budget would spend most of a run waiting, while this one still exercises the
// cancellation path the budget publishes (SPEC/WEB.md § Graph Query Time Budget).
const fuzzGraphBudget = time.Second

// The route selector of FuzzWebQueryParameters: the three handlers that read a
// query parameter (SPEC/WEB.md § Routes and Pages).
const (
	fuzzRouteTasks = iota
	fuzzRouteAudit
	fuzzRouteGraphData
	fuzzRouteCount
)

// fuzzTasksParams are the six parameters the tasks page accepts (SPEC/WEB.md
// § Roadmap Tasks Page, Query parameters).
var fuzzTasksParams = []string{"q", "sprint", "status", "type", "page", "size"}

// fuzzGraphLimits are the six values the graph data endpoint's `limit` accepts
// (SPEC/WEB.md § Graph Data Endpoint).
var fuzzGraphLimits = []string{"50", "100", "250", "500", "1000", "3000"}

// fuzzGraphKinds is the closed set of `kind` values a graph data 400 carries
// (SPEC/WEB.md § Query-Bar Error Handling, rule 4).
var fuzzGraphKinds = []string{"invalid_limit", "plan_prefix", "execution"}

// fuzzSearchInput captures the value attribute of the tasks page's search input.
var fuzzSearchInput = regexp.MustCompile(`id="task-filter-q" name="q" placeholder="Search" value="([^"]*)"`)

// fuzzCookieOctets is the character set SPEC/WEB.md § Roadmap Tasks Page, The
// cookie's value, publishes for the filter-state cookie's value.
var fuzzCookieOctets = regexp.MustCompile(`^[A-Za-z0-9._~%+&=-]*$`)

// fuzzCookieAttributes is the attribute list the filter-state cookie is set with
// (SPEC/WEB.md § Roadmap Tasks Page, The cookie).
const fuzzCookieAttributes = "; Path=/; Max-Age=31536000; HttpOnly; SameSite=Lax"

// seedFuzzWebFixture builds, under a fresh short HOME, a roadmap with tasks of
// every type, sprint membership, and a multi-page audit log, and starts a graph
// server on the roadmap's derived socket, so every handler the fuzz target drives
// reads real data and the graph data endpoint reaches a statement.
//
// It takes a *testing.F rather than reusing the package's *testing.T helpers,
// because a fuzz target's fixture is built once per worker process, before
// f.Fuzz, where only the *testing.F exists.
func seedFuzzWebFixture(f *testing.F) {
	f.Helper()

	home, remove, err := testenv.ShortHome()
	if err != nil {
		f.Fatalf("creating a short HOME: %v", err)
	}
	f.Cleanup(remove)
	f.Setenv("HOME", home)

	previousLogger := logger
	logger = newLogger(io.Discard)
	f.Cleanup(func() { logger = previousLogger })

	database, err := db.Open(fuzzWebRoadmap)
	if err != nil {
		f.Fatalf("opening roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test fixture

	titles := []string{
		"Evict stale entries from the cache", "Café menu sync", "ｃａｃｈｅ warm-up",
		"Rotate the signing key", "Fix the N+1 query on the tasks page", "#7 regression",
		"監査ログの検証", "Ship the release \U0001F680", "Trim the\u0085whitespace", "Plain task",
	}
	types := []models.TaskType{
		models.TypeUserStory, models.TypeTask, models.TypeBug, models.TypeSubTask, models.TypeEpic,
		models.TypeRefactor, models.TypeChore, models.TypeSpike, models.TypeDesignUX, models.TypeImprovement,
	}
	now := time.Now().UTC().Format(time.RFC3339)
	const taskCount = 30
	taskIDs := make([]int, 0, taskCount)
	for i := 0; i < taskCount; i++ {
		id, seedErr := seedTask(database, &models.Task{
			Priority:               i % 10,
			Severity:               (i * 3) % 10,
			Status:                 models.StatusBacklog,
			Type:                   types[i%len(types)],
			Title:                  titles[i%len(titles)],
			FunctionalRequirements: "Serve the task list",
			TechnicalRequirements:  "net/http handler",
			AcceptanceCriteria:     "The page renders",
			CreatedAt:              now,
		})
		if seedErr != nil {
			f.Fatalf("seeding task %d: %v", i, seedErr)
		}
		taskIDs = append(taskIDs, id)
	}
	sprintID, err := seedSprint(database, &models.Sprint{
		Status:      models.SprintPending,
		Title:       "Harden the query parameters",
		Description: "Fuzz every parameter the web server reads",
		CreatedAt:   now,
	})
	if err != nil {
		f.Fatalf("seeding sprint: %v", err)
	}
	if err := database.AddTasksToSprint(context.Background(), sprintID, taskIDs[:5]); err != nil {
		f.Fatalf("adding tasks to the sprint: %v", err)
	}
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 60; i++ {
		performedAt := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		if err := database.WithTransaction(func(tx *sql.Tx) error {
			return db.LogAuditTx(tx, models.OpTaskCreate, models.EntityTask, i+1, performedAt)
		}); err != nil {
			f.Fatalf("seeding audit entry %d: %v", i, err)
		}
	}

	previousBudget := graphlock.StatementBudget
	graphlock.StatementBudget = fuzzGraphBudget
	f.Cleanup(func() { graphlock.StatementBudget = previousBudget })

	roadmapDir, err := utils.GetRoadmapDir(fuzzWebRoadmap)
	if err != nil {
		f.Fatalf("resolving the roadmap directory: %v", err)
	}
	socket, err := graphclient.SocketPath(fuzzWebRoadmap)
	if err != nil {
		f.Fatalf("deriving the socket path: %v", err)
	}
	server, err := graphserver.Start(graphserver.Options{
		GraphDir:        filepath.Join(roadmapDir, "graph"),
		Socket:          socket,
		RoadmapName:     fuzzWebRoadmap,
		CaptureDir:      f.TempDir(),
		StatementBudget: fuzzGraphBudget,
	})
	if err != nil {
		f.Fatalf("starting the graph server: %v", err)
	}
	f.Cleanup(func() {
		if stopErr := server.Stop(); stopErr != nil {
			f.Errorf("stopping the graph server: %v", stopErr)
		}
	})
}

// fuzzEchoedSearch is the value the search input carries for an active term,
// per SPEC/WEB.md § Roadmap Tasks Page: each byte that is not part of a valid
// UTF-8 sequence replaced by U+FFFD. A NUL is replaced by U+FFFD too, because
// that is the value an HTML parser reads for a NUL in an attribute value (HTML
// Living Standard § 13.2.5.36-38, unexpected-null-character), so no page can
// carry anything else for it.
func fuzzEchoedSearch(q string) string {
	var b strings.Builder
	for len(q) > 0 {
		r, size := utf8.DecodeRuneInString(q)
		if r == 0 {
			r = utf8.RuneError
		}
		b.WriteRune(r)
		q = q[size:]
	}
	return b.String()
}

// fuzzIsExplicitTasksQuery reports whether a raw query string names at least one
// of the six parameters, whatever its value (SPEC/WEB.md § Roadmap Tasks Page,
// Explicit and bare requests).
func fuzzIsExplicitTasksQuery(rawQuery string) bool {
	for _, part := range strings.Split(rawQuery, "&") {
		key, _, _ := strings.Cut(part, "=")
		name, err := url.QueryUnescape(key)
		if err == nil && slices.Contains(fuzzTasksParams, name) {
			return true
		}
	}
	return false
}

// FuzzWebQueryParameters drives the three handlers of `rmp web` that read a URL
// query parameter — the tasks page (`q`, `sprint`, `status`, `type`, `page`,
// `size`), the audit log page (`page`), and the graph data endpoint (`q`,
// `limit`) — with arbitrary raw query strings, through the real handler chain
// and a request parsed by net/http exactly as a server parses one.
//
// Invariants (SPEC/WEB.md § Routes and Pages, § Roadmap Tasks Page, § Roadmap
// Audit Log Page, § Graph Data Endpoint, § Query-Bar Error Handling;
// SPEC/DATA_FORMATS.md § Graph View Data):
//
//  1. No query string produces a 5xx.
//  2. The tasks and audit pages answer 200 with an HTML body that is valid UTF-8.
//  3. On the tasks page the search input's value is the active `q` with each
//     invalid byte replaced by U+FFFD; an explicit request whose term is short
//     sets the filter-state cookie with the published attributes and a value of
//     cookie-octets at most 4000 bytes long that never carries `page`, and a bare
//     request sets no cookie.
//  4. The graph data endpoint answers 200 with a JSON graph view whose `nodes` and
//     `edges` are arrays, or 400 with a JSON error whose `kind` is one of the
//     three published values; a `limit` that is not one of the six allowed values
//     is `invalid_limit` before anything else, and an allowed one never is.
func FuzzWebQueryParameters(f *testing.F) {
	seedFuzzWebFixture(f)

	for _, seed := range []string{
		"", "q=", "q=cache", "q=#7", "q=a;b", "q=%FF", "q=%FF%FE", "q=%EF%BF%BD", "q=%zz",
		"q=+++&status=DOING", "q=cache&sprint=", "q=cache&status=DOING", "q=the&sprint=",
		"q=settle%3B+%22cache%22%2C+now&type=BUG&type=EPIC&size=25", "q=%00", "q=%C0%80",
		"status=doing&type=BUG,EPIC&sprint=007&size=20&page=3&q=%zz&status=DOING",
		"status=BACKLOG&status=SPRINT&status=DOING&status=TESTING&size=25", "sprint=none&size=100",
		"sprint=1", "sprint=2147483648", "page=0", "page=-5", "page=99999", "page=abc", "page=1",
		"size=10", "size=100", "size=0100", "priority=9", "%71=cache", "q=%E2%80%AE",
	} {
		f.Add(uint8(fuzzRouteTasks), seed)
	}
	for _, seed := range []string{
		"", "page=", "page=0", "page=1", "page=2", "page=3", "page=-5", "page=99999", "page=abc",
		"page=%zz", "page=9223372036854775808", "page=1&page=2",
	} {
		f.Add(uint8(fuzzRouteAudit), seed)
	}
	for _, seed := range []string{
		"", "limit=50", "limit=3000", "limit=0", "limit=abc", "limit=+100", "limit=0100", "limit=",
		"q=MATCH+(n)+RETURN+n", "q=MATCH+(n)+RETURN+n&limit=250", "q=EXPLAIN+MATCH+(n)+RETURN+n",
		"q=PROFILE+MATCH+(n)+RETURN+n", "q=MATCH+(explain)+RETURN+explain", "q=SHOW+INDEXES",
		"q=CALL+db.labels()", "q=RETURN+1", "q=MATCH+(", "q=CREATE+(:Fuzz+{k:'v'})",
		"q=MATCH+(a),(b),(c)+RETURN+count(*)", "q=%FF", "q=RETURN+'%FF'", "q=%zz&limit=100",
		"q=UNWIND+range(1,100000000)+AS+x+RETURN+x", "q=RETURN+1+LIMIT+1&limit=50",
	} {
		f.Add(uint8(fuzzRouteGraphData), seed)
	}

	// The request below names the host localhost with no port, so the handler is
	// the one of a loopback listener on port 80: the one bound port under which
	// a host without a port is served (SPEC/WEB.md § Security and Constraints,
	// rule 13). The guard admits every input, and the fuzzing reaches the routes.
	h := newHandler(newHostPolicy("localhost", httpDefaultPort))

	f.Fuzz(func(t *testing.T, route uint8, rawQuery string) {
		path := "/roadmaps/" + fuzzWebRoadmap
		switch route % fuzzRouteCount {
		case fuzzRouteTasks:
			path += "/tasks"
		case fuzzRouteAudit:
			path += "/audit"
		case fuzzRouteGraphData:
			path += "/graph/data"
		}

		// Parse the request as net/http's server does. A request line it cannot
		// parse is refused by the server before any handler runs, so it is not an
		// input of this surface.
		wire := "GET " + path + "?" + rawQuery + " HTTP/1.1\r\nHost: localhost\r\n\r\n"
		req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(wire)))
		if err != nil || req.URL.Path != path || req.URL.RawQuery != rawQuery {
			return
		}
		req.RemoteAddr = "192.0.2.10:49152"

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		body := rec.Body.String()
		status := rec.Code

		if status >= 500 {
			t.Fatalf("GET %s?%s answered %d; hostile input never produces a 5xx. body=%q",
				path, rawQuery, status, body)
		}
		if !utf8.ValidString(body) {
			t.Fatalf("GET %s?%s answered a body that is not valid UTF-8", path, rawQuery)
		}

		switch route % fuzzRouteCount {
		case fuzzRouteTasks:
			checkFuzzTasksResponse(t, rawQuery, rec)
		case fuzzRouteAudit:
			if status != http.StatusOK || rec.Header().Get("Content-Type") != contentTypeHTML {
				t.Fatalf("GET %s?%s answered %d %q, want 200 HTML (the audit page never errors "+
					"on its page parameter)", path, rawQuery, status, rec.Header().Get("Content-Type"))
			}
		case fuzzRouteGraphData:
			checkFuzzGraphDataResponse(t, rawQuery, rec)
		}
	})
}

// checkFuzzTasksResponse applies invariants 2 and 3 of FuzzWebQueryParameters to
// one tasks page response.
func checkFuzzTasksResponse(t *testing.T, rawQuery string, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != contentTypeHTML {
		t.Fatalf("tasks?%s answered %d %q, want 200 HTML whatever the parameters carry",
			rawQuery, rec.Code, rec.Header().Get("Content-Type"))
	}

	explicit := fuzzIsExplicitTasksQuery(rawQuery)
	activeQ := ""
	if explicit {
		values, _ := url.ParseQuery(rawQuery) //nolint:errcheck // a malformed pair is absent (rule 5), which ParseQuery's partial result already reflects
		if v := values["q"]; len(v) > 0 {
			activeQ = v[0]
		}
	}

	match := fuzzSearchInput.FindStringSubmatch(rec.Body.String())
	if match == nil {
		t.Fatalf("tasks?%s rendered no search input", rawQuery)
	}
	if got, want := html.UnescapeString(match[1]), fuzzEchoedSearch(activeQ); got != want {
		t.Fatalf("tasks?%s: the search input carries %q, want the active q %q with invalid bytes "+
			"replaced by U+FFFD", rawQuery, got, want)
	}

	cookies := rec.Header().Values("Set-Cookie")
	if !explicit {
		if len(cookies) != 0 {
			t.Fatalf("tasks?%s is a bare request but set %q; a bare request never rewrites the cookie",
				rawQuery, cookies)
		}
		return
	}
	if len(cookies) > 1 {
		t.Fatalf("tasks?%s set %d cookies, want at most one", rawQuery, len(cookies))
	}
	if len(cookies) == 0 {
		// Only an encoded value over 4000 bytes withholds the cookie, and only a
		// long term can produce one; every other part fits in a few hundred bytes.
		if len(url.QueryEscape(fuzzEchoedSearch(activeQ))) < 3500 {
			t.Fatalf("tasks?%s is an explicit 200 with a short term but set no cookie", rawQuery)
		}
		return
	}
	value, ok := strings.CutPrefix(cookies[0], tasksFilterCookie+"=")
	if !ok || !strings.HasSuffix(value, fuzzCookieAttributes) {
		t.Fatalf("tasks?%s set %q, want %s=<value>%s", rawQuery, cookies[0], tasksFilterCookie, fuzzCookieAttributes)
	}
	value = strings.TrimSuffix(value, fuzzCookieAttributes)
	if len(value) > 4000 || !fuzzCookieOctets.MatchString(value) {
		t.Fatalf("tasks?%s set the cookie value %q: %d bytes, want at most 4000 bytes of %s",
			rawQuery, value, len(value), fuzzCookieOctets)
	}
	parts, err := url.ParseQuery(value)
	if err != nil {
		t.Fatalf("tasks?%s set the cookie value %q, which is not a query string: %v", rawQuery, value, err)
	}
	if _, hasPage := parts["page"]; hasPage {
		t.Fatalf("tasks?%s set the cookie value %q, which carries page", rawQuery, value)
	}
}

// checkFuzzGraphDataResponse applies invariant 4 of FuzzWebQueryParameters to one
// graph data response.
func checkFuzzGraphDataResponse(t *testing.T, rawQuery string, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Header().Get("Content-Type") != contentTypeJSON {
		t.Fatalf("graph/data?%s answered %d with Content-Type %q, want %q",
			rawQuery, rec.Code, rec.Header().Get("Content-Type"), contentTypeJSON)
	}

	values, _ := url.ParseQuery(rawQuery) //nolint:errcheck // the handler reads the same partial result through r.URL.Query
	limit := values.Get("limit")
	mustAccept, mustRefuse := fuzzLimitVerdict(limit)

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("graph/data?%s answered %d with a body that is not a JSON object: %v", rawQuery, rec.Code, err)
	}

	switch rec.Code {
	case http.StatusOK:
		if mustRefuse {
			t.Fatalf("graph/data?%s answered 200 for a limit %q that is not one of the six values", rawQuery, limit)
		}
		for _, key := range []string{"nodes", "edges"} {
			var elements []json.RawMessage
			if err := json.Unmarshal(payload[key], &elements); err != nil || elements == nil {
				t.Fatalf("graph/data?%s answered 200 whose %q is not an array: %s", rawQuery, key, payload[key])
			}
		}
	case http.StatusBadRequest:
		var kind, reason string
		if json.Unmarshal(payload["kind"], &kind) != nil || json.Unmarshal(payload["error"], &reason) != nil ||
			!slices.Contains(fuzzGraphKinds, kind) || reason == "" {
			t.Fatalf("graph/data?%s answered 400 with %s, want {\"error\": <text>, \"kind\": one of %v}",
				rawQuery, rec.Body.String(), fuzzGraphKinds)
		}
		if mustRefuse && kind != "invalid_limit" {
			t.Fatalf("graph/data?%s: limit %q is not one of the six values, so the answer is "+
				"invalid_limit before anything else; got kind %q", rawQuery, limit, kind)
		}
		if mustAccept && kind == "invalid_limit" {
			t.Fatalf("graph/data?%s: limit %q is allowed but was refused as invalid_limit", rawQuery, limit)
		}
	default:
		t.Fatalf("graph/data?%s answered %d, want 200 or 400 with a graph server running", rawQuery, rec.Code)
	}
}

// fuzzIntegerSpelling matches a base-10 integer, signed or not, leading zeros
// included.
var fuzzIntegerSpelling = regexp.MustCompile(`^[+-]?[0-9]+$`)

// fuzzLimitVerdict states what SPEC/WEB.md § Graph Data Endpoint requires of a
// `limit` value: absent (or empty) and each of the six allowed values must be
// accepted, and every other value must be refused as an invalid limit. The one
// case the SPEC leaves open is a decorated spelling of an allowed value — `+100`,
// `0100` — because it does not say whether such a spelling is that value; the
// oracle constrains neither verdict for it.
func fuzzLimitVerdict(limit string) (mustAccept, mustRefuse bool) {
	if limit == "" || slices.Contains(fuzzGraphLimits, limit) {
		return true, false
	}
	if fuzzIntegerSpelling.MatchString(limit) {
		digits := strings.TrimLeft(strings.TrimLeft(limit, "+"), "0")
		if !strings.HasPrefix(limit, "-") && slices.Contains(fuzzGraphLimits, digits) {
			return false, false
		}
	}
	return false, true
}
