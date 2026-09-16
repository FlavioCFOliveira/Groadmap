package web

// The graph data endpoint refuses a statement carrying an EXPLAIN or PROFILE
// prefix before anything is sent (rmp task #416; SPEC/WEB.md § Query-Bar Error
// Handling, rule 12; Acceptance Criteria 166 to 172).
//
// # The premise every table here confirms first
//
// Recognition is the engine's. The endpoint hands the statement it resolved from
// q to the pinned engine's own parser and refuses what that parser reports as
// prefixed, so the cases below are statements of the engine's grammar rather than
// of this file's. Each table therefore confirms against the same parser that its
// cases are what it says they are before it asserts what the endpoint answers. A
// grammar change at a future pin then fails a premise instead of silently
// changing what a criterion asserts (Acceptance Criteria 166 and 169).
//
// # The line comes from the specification, not from the constant
//
// The refusal's `error` is asserted against the line SPEC/WEB.md publishes, read
// out of the specification itself. Asserting it against graphPlanPrefixLine would
// agree with a wrong constant instead of catching it.

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher/parser"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphjson"
)

// rePublishedPlanPrefixLine finds the refusal line where rule 12 publishes it: on
// a line of its own, written as one code span.
var rePublishedPlanPrefixLine = regexp.MustCompile("(?m)^[ \t]*`(query not run: [^`]+)`[ \t]*$")

// specPlanPrefixLine returns the refusal line SPEC/WEB.md § Query-Bar Error
// Handling, rule 12, publishes. The specification publishes it once; finding it
// zero times or twice is a failure, because either would leave the assertion
// comparing against something other than the one published line.
func specPlanPrefixLine(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "SPEC", "WEB.md")) //nolint:gosec // a fixed path inside the repository
	if err != nil {
		t.Fatalf("reading SPEC/WEB.md: %v", err)
	}
	found := rePublishedPlanPrefixLine.FindAllStringSubmatch(string(raw), -1)
	if len(found) != 1 {
		t.Fatalf("SPEC/WEB.md publishes %d plan-prefix refusal lines, want exactly one "+
			"(§ Query-Bar Error Handling, rule 12)", len(found))
	}
	return found[0][1]
}

// planPrefixCases are the spellings Acceptance Criterion 166 names, each written
// with both prefixes.
var planPrefixCases = []struct {
	name string
	q    string
}{
	{"EXPLAIN", "EXPLAIN MATCH (n) RETURN n"},
	{"PROFILE", "PROFILE MATCH (n) RETURN n"},
	{"EXPLAIN in lower case", "explain match (n) return n"},
	{"PROFILE in lower case", "profile match (n) return n"},
	{"EXPLAIN in mixed case", "ExPlAiN MATCH (n) RETURN n"},
	{"PROFILE in mixed case", "pRoFiLe MATCH (n) RETURN n"},
	{"EXPLAIN over several lines", "EXPLAIN\nMATCH (s:Spec)\nWHERE s.key = 'user-authentication'\nRETURN s"},
	{"PROFILE over several lines", "PROFILE\nMATCH (s:Spec)-[:IMPLEMENTED_BY]->(c:Code)\nRETURN s, c"},
	{"EXPLAIN after spaces, tabs and newlines", " \t\n\t EXPLAIN MATCH (n) RETURN n"},
	{"PROFILE after spaces, tabs and newlines", "\n  \t PROFILE MATCH (n) RETURN n"},
	{"EXPLAIN after a line comment", "// plan the full read first\nEXPLAIN MATCH (n) RETURN n"},
	{"PROFILE after a line comment", "// measure the full read\nPROFILE MATCH (n) RETURN n"},
	{"EXPLAIN after a block comment", "/* plan the full read first */ EXPLAIN MATCH (n) RETURN n"},
	{"PROFILE after a block comment", "/* measure the full read */\nPROFILE MATCH (n) RETURN n"},
	{"EXPLAIN on a statement with its own LIMIT", "EXPLAIN MATCH (n) RETURN n LIMIT 5"},
	{"PROFILE on a statement with its own LIMIT", "PROFILE MATCH (n) RETURN n LIMIT 5"},
	{"EXPLAIN on a write", "EXPLAIN MATCH (n) DETACH DELETE n"},
	{"PROFILE on a write", "PROFILE MATCH (n) DETACH DELETE n"},
	{"EXPLAIN on a projected procedure call", "EXPLAIN CALL db.labels() YIELD label RETURN label"},
	{"PROFILE on a projected procedure call", "PROFILE CALL db.labels() YIELD label RETURN label"},
}

// mustCarryPlanPrefix confirms against the pinned engine's parser that the
// statement the endpoint resolves from q carries a prefix.
func mustCarryPlanPrefix(t *testing.T, q string) {
	t.Helper()
	_, mode, err := parser.ParseStatement(resolveGraphQuery(q))
	if err != nil || mode == parser.PlanModeNone {
		t.Fatalf("premise: the pinned engine's parser does not report %q as carrying an EXPLAIN or "+
			"PROFILE prefix (mode %q, error %v). The case comes from the engine's grammar, and the "+
			"grammar has changed under it", q, mode.String(), err)
	}
}

// mustParseWithoutPlanPrefix confirms against the pinned engine's parser that
// the statement the endpoint resolves from q parses and carries no prefix.
func mustParseWithoutPlanPrefix(t *testing.T, q string) {
	t.Helper()
	_, mode, err := parser.ParseStatement(resolveGraphQuery(q))
	if err != nil || mode != parser.PlanModeNone {
		t.Fatalf("premise: the pinned engine's parser does not report %q as a statement that parses "+
			"with no prefix (mode %q, error %v)", q, mode.String(), err)
	}
}

// mustFailToParse confirms against the pinned engine's parser that the statement
// the endpoint resolves from q is text it cannot parse.
func mustFailToParse(t *testing.T, q string) {
	t.Helper()
	if _, mode, err := parser.ParseStatement(resolveGraphQuery(q)); err == nil {
		t.Fatalf("premise: the pinned engine's parser parses %q (mode %q), and the case exists for text "+
			"it cannot parse", q, mode.String())
	}
}

// assertPlanPrefixRefusal asserts the whole answer rule 12 fixes: HTTP 400, a
// JSON body of exactly two fields, kind plan_prefix, and the published line.
func assertPlanPrefixRefusal(t *testing.T, rec *httptest.ResponseRecorder, line string) {
	t.Helper()

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a statement carrying a plan prefix; body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, contentTypeJSON) {
		t.Errorf("Content-Type = %q, want %q", ct, contentTypeJSON)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding the refusal: %v; body=%q", err, rec.Body.String())
	}
	if got := slices.Sorted(maps.Keys(body)); !slices.Equal(got, []string{"error", "kind"}) {
		t.Errorf("body fields = %v, want exactly [error kind]; body=%q", got, rec.Body.String())
	}
	if body["kind"] != "plan_prefix" {
		t.Errorf("kind = %v, want plan_prefix; body=%q", body["kind"], rec.Body.String())
	}
	if body["error"] != line {
		t.Errorf("error = %q, want exactly the line SPEC/WEB.md § Query-Bar Error Handling, rule 12, "+
			"publishes: %q", body["error"], line)
	}
}

// withoutPlanPrefixRecognition runs fn with the prefix recognition left out of the
// endpoint's path, which is the reference Acceptance Criterion 168 compares the
// endpoint against. The recognition is restored before it returns.
func withoutPlanPrefixRecognition(fn func()) {
	saved := planPrefixCheck
	planPrefixCheck = func(string) error { return nil }
	defer func() { planPrefixCheck = saved }()
	fn()
}

// valuesThroughTheServer sends a statement to the roadmap's graph server through
// internal/graphclient — the mechanism `rmp graph client` reaches a server
// through, used in process because this package spawns no subprocess — and
// returns the first column of every row, mapped by the one value mapping the
// published formats share.
func valuesThroughTheServer(t *testing.T, name, query string) []any {
	t.Helper()

	socket, err := graphclient.SocketPath(name)
	if err != nil {
		t.Fatalf("deriving the socket path of roadmap %q: %v", name, err)
	}
	result, err := graphclient.Send(context.Background(), socket, query)
	if err != nil {
		t.Fatalf("sending %q to the graph server at %s: %v", query, socket, err)
	}
	values := make([]any, 0, len(result.Rows))
	for _, row := range result.Rows {
		if len(row) == 0 {
			t.Fatalf("%q returned a row with no column", query)
		}
		values = append(values, graphjson.Value(row[0], nil))
	}
	return values
}

// countThroughTheServer reads a single count back through the graph server.
func countThroughTheServer(t *testing.T, name, query string) int64 {
	t.Helper()

	values := valuesThroughTheServer(t, name, query)
	if len(values) != 1 {
		t.Fatalf("%q returned %d rows, want the one row a count returns", query, len(values))
	}
	n, ok := values[0].(int64)
	if !ok {
		t.Fatalf("%q returned %v (%T), want an integer count", query, values[0], values[0])
	}
	return n
}

// TestPlanPrefixRefusal_TheKindAndTheLineAreTheSpecifications pins the two
// constants the refusal is built from to the values the specification publishes:
// the kind rule 4 enumerates and the line rule 12 publishes.
func TestPlanPrefixRefusal_TheKindAndTheLineAreTheSpecifications(t *testing.T) {
	if graphErrPlanPrefix != "plan_prefix" {
		t.Errorf("graphErrPlanPrefix = %q, want plan_prefix (SPEC/WEB.md § Query-Bar Error Handling, rule 4)",
			graphErrPlanPrefix)
	}
	if line := specPlanPrefixLine(t); graphPlanPrefixLine != line {
		t.Errorf("graphPlanPrefixLine = %q, want the published line %q", graphPlanPrefixLine, line)
	}
}

// TestHandleGraphData_RefusesBothPlanPrefixesInEverySpelling is Acceptance
// Criterion 166: against a served roadmap, every spelling the engine's parser
// recognises is refused, with no limit and with an allowed one, and the two
// answers are the same bytes.
func TestHandleGraphData_RefusesBothPlanPrefixesInEverySpelling(t *testing.T) {
	line := specPlanPrefixLine(t)
	t.Setenv("HOME", shortHome(t))
	name := servedRoadmap(t, "identity-platform", graphSeedQueries()...)

	for _, tc := range planPrefixCases {
		t.Run(tc.name, func(t *testing.T) {
			mustCarryPlanPrefix(t, tc.q)

			bare := doGraphData(t, name, url.Values{"q": {tc.q}})
			assertPlanPrefixRefusal(t, bare, line)
			limited := doGraphData(t, name, url.Values{"q": {tc.q}, "limit": {"250"}})
			assertPlanPrefixRefusal(t, limited, line)

			if bare.Body.String() != limited.Body.String() {
				t.Errorf("the refusal differs with an allowed limit:\n without: %q\n with:    %q",
					bare.Body.String(), limited.Body.String())
			}
		})
	}
}

// TestHandleGraphData_ARefusedPrefixIsNeverSentAndNeedsNoServer is Acceptance
// Criterion 167, in its three halves.
//
// The no-write half alone asserts little: an EXPLAIN executes nothing and the
// engine refuses a PROFILE of a write wherever either runs, so an endpoint that
// sent both would leave the graph unchanged too. What establishes that the
// refusal is decided before any graph server is resolved is the kind, and the two
// pairs that set its 400 against a 503 and against a 500 for the same roadmap.
func TestHandleGraphData_ARefusedPrefixIsNeverSentAndNeedsNoServer(t *testing.T) {
	line := specPlanPrefixLine(t)
	prefixedWrites := []string{
		"EXPLAIN CREATE (n:Probe {key:'p'})",
		"PROFILE CREATE (n:Probe {key:'p'})",
	}
	prefixedReads := []string{
		"EXPLAIN MATCH (n) RETURN n",
		"PROFILE MATCH (n) RETURN n",
	}
	const unprefixedRead = "MATCH (n) RETURN n"
	for _, q := range slices.Concat(prefixedWrites, prefixedReads) {
		mustCarryPlanPrefix(t, q)
	}
	mustParseWithoutPlanPrefix(t, unprefixedRead)

	t.Run("a served roadmap is left unwritten", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		name := servedRoadmap(t, "identity-platform", graphSeedQueries()...)

		nodes := countThroughTheServer(t, name, "MATCH (n) RETURN count(n)")
		relationships := countThroughTheServer(t, name, "MATCH ()-[r]->() RETURN count(r)")

		for _, q := range prefixedWrites {
			assertPlanPrefixRefusal(t, doGraphData(t, name, url.Values{"q": {q}}), line)
		}

		if got := countThroughTheServer(t, name, "MATCH (n:Probe) RETURN count(n)"); got != 0 {
			t.Errorf("the graph holds %d Probe node(s) after two refused writes, want 0", got)
		}
		if got := countThroughTheServer(t, name, "MATCH (n) RETURN count(n)"); got != nodes {
			t.Errorf("node count = %d after two refused writes, want the %d it held before", got, nodes)
		}
		if got := countThroughTheServer(t, name, "MATCH ()-[r]->() RETURN count(r)"); got != relationships {
			t.Errorf("relationship count = %d after two refused writes, want the %d it held before",
				got, relationships)
		}

		// The control: the read-back does see a write. Without it the zero above
		// would be as true of a read-back that could not see one.
		if rec := doGraphData(t, name, url.Values{"q": {"CREATE (n:Probe {key:'p'})"}}); rec.Code != http.StatusOK {
			t.Fatalf("the unprefixed control write answered %d, want 200; body=%q", rec.Code, rec.Body.String())
		}
		if got := countThroughTheServer(t, name, "MATCH (n:Probe) RETURN count(n)"); got != 1 {
			t.Fatalf("the read-back finds %d Probe node(s) after the control write, want 1, so it cannot "+
				"tell a write that was sent from one that was not", got)
		}
	})

	t.Run("no graph server: 400 for a prefix, 503 without one", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		name := seedRoadmap(t, "identity-platform")

		for _, q := range prefixedReads {
			assertPlanPrefixRefusal(t, doGraphData(t, name, url.Values{"q": {q}}), line)
		}
		if rec := doGraphData(t, name, url.Values{"q": {unprefixedRead}}); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%q answered %d, want 503: without it the 400s above cannot show that the refusal "+
				"precedes the server lookup; body=%q", unprefixedRead, rec.Code, rec.Body.String())
		}
	})

	t.Run("a derived socket path over the bound: 400 for a prefix, 500 without one", func(t *testing.T) {
		bound := measuredSocketPathBound(t, bindDir(t))
		const roadmap = "identity-platform"
		t.Setenv("HOME", deepHome(t, bound, roadmap))
		name := seedRoadmap(t, roadmap)
		assertDerivedPathIsOverTheBound(t, name, bound)

		for _, q := range prefixedReads {
			assertPlanPrefixRefusal(t, doGraphData(t, name, url.Values{"q": {q}}), line)
		}
		if rec := doGraphData(t, name, url.Values{"q": {unprefixedRead}}); rec.Code != http.StatusInternalServerError {
			t.Errorf("%q answered %d, want 500: without it the 400s above cannot show that the refusal "+
				"precedes the socket-path check; body=%q", unprefixedRead, rec.Code, rec.Body.String())
		}
	})
}

// TestHandleGraphData_AnUnprefixedStatementIsAnsweredByteForByteAsBefore is
// Acceptance Criterion 168: each request below is answered with the status, the
// content type and the exact bytes the same request produces with the prefix
// recognition left out of the endpoint's path, against the same server holding
// the same graph.
//
// The write's read-back is what establishes that the statement sent is the one
// the endpoint resolved and not text derived from the parse: the literal carries
// an escaped quote and a pair of double quotes, which a statement re-rendered from
// a parse tree would be the first thing to lose.
func TestHandleGraphData_AnUnprefixedStatementIsAnsweredByteForByteAsBefore(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := servedRoadmap(t, "identity-platform", graphSeedQueries()...)

	// The reference must really leave the recognition out, or every comparison
	// below compares the endpoint with itself.
	const prefixed = "EXPLAIN MATCH (n) RETURN n"
	var reference *httptest.ResponseRecorder
	withoutPlanPrefixRecognition(func() {
		reference = doGraphData(t, name, url.Values{"q": {prefixed}})
	})
	if strings.Contains(reference.Body.String(), "plan_prefix") {
		t.Fatalf("with the recognition left out, %q is still refused (%d %q); the reference is not the "+
			"endpoint without the recognition", prefixed, reference.Code, reference.Body.String())
	}

	const probeKey = `it's a "probe"`
	requests := []struct {
		name   string
		params url.Values
	}{
		{"a request with no q", nil},
		{"the default query at limit 50", url.Values{"q": {defaultGraphQuery}, "limit": {"50"}}},
		{"the default query at limit 100", url.Values{"q": {defaultGraphQuery}, "limit": {"100"}}},
		{"the default query at limit 250", url.Values{"q": {defaultGraphQuery}, "limit": {"250"}}},
		{"the default query at limit 500", url.Values{"q": {defaultGraphQuery}, "limit": {"500"}}},
		{"the default query at limit 1000", url.Values{"q": {defaultGraphQuery}, "limit": {"1000"}}},
		{"the default query at limit 3000", url.Values{"q": {defaultGraphQuery}, "limit": {"3000"}}},
		{"a read carrying its own LIMIT", url.Values{"q": {"MATCH (s:Spec) RETURN s ORDER BY s.key LIMIT 1"}}},
		{"SHOW INDEXES", url.Values{"q": {"SHOW INDEXES"}}},
		{"a standalone procedure call", url.Values{"q": {"CALL db.labels()"}}},
		{"a statement that fails in the engine", url.Values{"q": {"MATCH (n) RETURN"}}},
		{"a write with no projection", url.Values{"q": {`CREATE (n:Probe {key:'it\'s a "probe"'})`}}},
	}

	for _, req := range requests {
		t.Run(req.name, func(t *testing.T) {
			if _, mode, _ := parser.ParseStatement(resolveGraphQuery(req.params.Get("q"))); mode != parser.PlanModeNone {
				t.Fatalf("premise: %q carries a %s prefix, and this criterion is about statements that "+
					"carry none", req.params.Get("q"), mode.String())
			}

			var before *httptest.ResponseRecorder
			withoutPlanPrefixRecognition(func() {
				before = doGraphData(t, name, req.params)
			})
			after := doGraphData(t, name, req.params)

			if after.Code != before.Code {
				t.Errorf("status = %d, want the %d the endpoint answers without the recognition; body=%q",
					after.Code, before.Code, after.Body.String())
			}
			if got, want := after.Header().Get("Content-Type"), before.Header().Get("Content-Type"); got != want {
				t.Errorf("Content-Type = %q, want %q", got, want)
			}
			if after.Body.String() != before.Body.String() {
				t.Errorf("the response bytes differ from the endpoint's without the recognition:\n got:  %q\n want: %q",
					after.Body.String(), before.Body.String())
			}
		})
	}

	// One write was sent by each side, and both stored exactly the value the
	// literal denotes.
	keys := valuesThroughTheServer(t, name, "MATCH (n:Probe) RETURN n.key")
	if len(keys) != 2 {
		t.Fatalf("the graph holds %d Probe node(s), want the 2 the two sides of the write comparison "+
			"created: %v", len(keys), keys)
	}
	for _, key := range keys {
		if key != probeKey {
			t.Errorf("a stored key is %q, want %q: the statement sent was not the one the endpoint resolved",
				key, probeKey)
		}
	}
}

// planPrefixLookalikes parse and carry no prefix: EXPLAIN or PROFILE appears in
// each as a variable, a string literal, a label, a trailing comment, or a property
// key and an alias.
var planPrefixLookalikes = []string{
	"MATCH (explain) RETURN explain",
	"MATCH (n) WHERE n.key = 'EXPLAIN' RETURN n",
	"MATCH (n:`PROFILE`) RETURN n",
	"MATCH (n) RETURN n // EXPLAIN",
	"MATCH (n) RETURN n.explain AS profile",
}

// planPrefixUnparseable is text the engine's parser cannot parse, a prefixed
// schema statement among it, which rule 12 leaves on the path it took before the
// rule existed.
var planPrefixUnparseable = []string{
	"EXPLAINMATCH (n) RETURN n",
	"EXPLAIN",
	"EXPLAIN EXPLAIN MATCH (n) RETURN n",
	"EXPLAIN SHOW INDEXES",
	"EXPLAIN CREATE INDEX probe_idx FOR (n:Probe) ON (n.key)",
	"EXPLAIN MATCH (n RETURN n",
}

// TestHandleGraphData_LookalikesAreSentAndUnparseableTextKeepsItsAnswer is
// Acceptance Criterion 169. With no graph server every statement of both groups
// is answered 503, which establishes that each reached server resolution rather
// than being refused.
func TestHandleGraphData_LookalikesAreSentAndUnparseableTextKeepsItsAnswer(t *testing.T) {
	for _, q := range planPrefixLookalikes {
		mustParseWithoutPlanPrefix(t, q)
	}
	for _, q := range planPrefixUnparseable {
		mustFailToParse(t, q)
	}

	t.Run("a served roadmap", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		name := servedRoadmap(t, "identity-platform", graphSeedQueries()...)

		for _, q := range planPrefixLookalikes {
			rec := doGraphData(t, name, url.Values{"q": {q}})
			if rec.Code != http.StatusOK {
				t.Errorf("%q answered %d, want 200: it carries no prefix; body=%q", q, rec.Code, rec.Body.String())
				continue
			}
			var view map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
				t.Errorf("%q: decoding the graph view: %v; body=%q", q, err, rec.Body.String())
				continue
			}
			if got := slices.Sorted(maps.Keys(view)); !slices.Equal(got, []string{"edges", "nodes"}) {
				t.Errorf("%q: body fields = %v, want the node-and-edge shape; body=%q", q, got, rec.Body.String())
			}
		}

		for _, q := range planPrefixUnparseable {
			rec := doGraphData(t, name, url.Values{"q": {q}})
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%q answered %d, want 400; body=%q", q, rec.Code, rec.Body.String())
				continue
			}
			kind, reason := decodeQueryError(t, rec.Body.Bytes())
			if kind != graphErrExecution {
				t.Errorf("%q: kind = %q, want execution: text the parser cannot parse is sent, and the "+
					"engine's diagnostic reaches the caller; reason=%q", q, kind, reason)
			}
			if !strings.Contains(reason, "cypher:") {
				t.Errorf("%q: error = %q, want the engine's own diagnostic", q, reason)
			}
		}
	})

	t.Run("no graph server", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		name := seedRoadmap(t, "identity-platform")

		for _, q := range slices.Concat(planPrefixLookalikes, planPrefixUnparseable) {
			if rec := doGraphData(t, name, url.Values{"q": {q}}); rec.Code != http.StatusServiceUnavailable {
				t.Errorf("%q answered %d with no graph server, want 503: a statement that is not refused "+
					"reaches server resolution; body=%q", q, rec.Code, rec.Body.String())
			}
		}
	})
}

// TestHandleGraphData_AnInvalidLimitOutranksAPlanPrefix is Acceptance Criterion
// 170. The control — the same prefixed statement under an allowed limit, answered
// plan_prefix — is what tells a limit that outranked the prefix from a prefix that
// was never recognised.
func TestHandleGraphData_AnInvalidLimitOutranksAPlanPrefix(t *testing.T) {
	line := specPlanPrefixLine(t)
	prefixed := []string{"EXPLAIN MATCH (n) RETURN n", "PROFILE MATCH (n) RETURN n"}
	for _, q := range prefixed {
		mustCarryPlanPrefix(t, q)
	}

	assertOrdering := func(t *testing.T, name string) {
		t.Helper()
		for _, q := range prefixed {
			assertPlanPrefixRefusal(t, doGraphData(t, name, url.Values{"q": {q}, "limit": {"250"}}), line)

			for _, bad := range []string{"7", "all"} {
				rec := doGraphData(t, name, url.Values{"q": {q}, "limit": {bad}})
				if rec.Code != http.StatusBadRequest {
					t.Errorf("%q with limit %q answered %d, want 400; body=%q", q, bad, rec.Code, rec.Body.String())
					continue
				}
				kind, reason := decodeQueryError(t, rec.Body.Bytes())
				if kind != graphErrInvalidLimit {
					t.Errorf("%q with limit %q: kind = %q, want invalid_limit: the limit is resolved before "+
						"the prefix is examined (SPEC/WEB.md § Query-Bar Error Handling, rule 5)", q, bad, kind)
				}
				if !strings.Contains(reason, bad) {
					t.Errorf("%q with limit %q: error = %q, want it to name the rejected value", q, bad, reason)
				}
			}
		}
	}

	t.Run("a served roadmap", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		assertOrdering(t, servedRoadmap(t, "identity-platform", graphSeedQueries()...))
	})
	t.Run("no graph server", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		assertOrdering(t, seedRoadmap(t, "identity-platform"))
	})
}

// TestHandleGraphData_APlanPrefixRefusalIsRecordedOnceAtWarn is Acceptance
// Criterion 171. Records are counted rather than searched for: an endpoint that
// resolved the graph server before examining the prefix would, with no server
// running, record an unavailable graph server instead, and only a count of every
// record the request produced detects that.
func TestHandleGraphData_APlanPrefixRefusalIsRecordedOnceAtWarn(t *testing.T) {
	line := specPlanPrefixLine(t)
	const q = "EXPLAIN MATCH (n) RETURN n"
	mustCarryPlanPrefix(t, q)

	assertOneWarn := func(t *testing.T, name string) {
		t.Helper()
		buf := captureLog(t)
		rec := httptest.NewRecorder()
		handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/roadmaps/"+name+"/graph/data?q="+url.QueryEscape(q), nil))
		assertPlanPrefixRefusal(t, rec, line)

		records := logLines(buf)
		errorRecords := 0
		for _, record := range records {
			if strings.Contains(record, "level=ERROR") {
				errorRecords++
			}
		}
		if errorRecords != 0 {
			t.Errorf("the refusal produced %d ERROR record(s), want 0:\n%s", errorRecords, buf.String())
		}
		if len(records) != 1 {
			t.Fatalf("the refused request produced %d record(s), want exactly 1:\n%s", len(records), buf.String())
		}
		record := records[0]
		mustContainAll(t, record,
			"level=WARN",
			`msg="graph query bar request failed"`,
			"method=GET",
			"path=/roadmaps/"+name+"/graph/data",
			"status=400",
			"kind=plan_prefix",
		)
		if !strings.HasSuffix(record, ` err="`+line+`"`) {
			t.Errorf("the record's err is not exactly the published line %q:\n%s", line, record)
		}
	}

	t.Run("a served roadmap", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		assertOneWarn(t, servedRoadmap(t, "identity-platform", graphSeedQueries()...))
	})
	t.Run("no graph server", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		assertOneWarn(t, seedRoadmap(t, "identity-platform"))
	})
}

// TestGraphPage_ShowsThePlanPrefixRefusalInPlace is Acceptance Criterion 172,
// asserted the way this package asserts the page's behaviour — over the page the
// server renders, the script it embeds and the answer the endpoint gives —
// because the suite drives no browser. Each half is one link of what the user
// sees: the endpoint answers the submission with the line, the script writes a
// failure's error into the query bar's in-place element as text and leaves the
// graph and the page where they are, and the unprefixed statement renders.
func TestGraphPage_ShowsThePlanPrefixRefusalInPlace(t *testing.T) {
	line := specPlanPrefixLine(t)
	t.Setenv("HOME", shortHome(t))
	name := servedRoadmap(t, "identity-platform", graphSeedQueries()...)

	page := httptest.NewRecorder()
	handler().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/graph", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("the graph page answered %d, want 200", page.Code)
	}
	if !strings.Contains(page.Body.String(), `<p id="query-error" class="graph-query-bar__error" role="alert" hidden></p>`) {
		t.Errorf("the graph page carries no hidden in-place query-bar message element")
	}

	// The submission the page makes: q from the query box and limit from the
	// dropdown's default.
	assertPlanPrefixRefusal(t, doGraphData(t, name, url.Values{"q": {"EXPLAIN MATCH (n) RETURN n"}, "limit": {"100"}}), line)

	raw, err := staticFS.ReadFile("static/graph.js")
	if err != nil {
		t.Fatalf("reading the embedded graph.js: %v", err)
	}
	script := string(raw)

	show := functionBody(t, script, "function showQueryError(")
	for _, want := range []string{"queryError.textContent = message;", "queryError.hidden = false;"} {
		if !strings.Contains(show, want) {
			t.Errorf("showQueryError does not contain %q, so the message is not shown as text in place:\n%s", want, show)
		}
	}
	if strings.Contains(show, "innerHTML") {
		t.Errorf("showQueryError writes markup; the message must be written as text:\n%s", show)
	}

	search := functionBody(t, script, "function runSearch(")
	if !strings.HasPrefix(strings.TrimSpace(search[strings.Index(search, "{")+1:]), "clearQueryError();") {
		t.Errorf("runSearch does not clear the previous message before it searches again, so removing the "+
			"prefix would leave the refusal shown:\n%s", search)
	}
	if !strings.Contains(search, "applyData(res.body);") {
		t.Errorf("runSearch does not render a successful answer:\n%s", search)
	}
	branch := strings.Index(search, "if (res.body && res.body.error) {")
	if branch < 0 {
		t.Fatalf("runSearch has no branch for a failure carrying an error:\n%s", search)
	}
	failure := search[branch:]
	failure = failure[:strings.Index(failure, "}")+1]
	if !strings.Contains(failure, "showQueryError(res.body.error);") || !strings.Contains(failure, "return;") {
		t.Errorf("a failure carrying an error is not shown through showQueryError:\n%s", failure)
	}
	for _, forbidden := range []string{"clearGraph(", "applyData(", "location"} {
		if strings.Contains(failure, forbidden) {
			t.Errorf("the failure branch calls %q, so the graph already shown or the page itself does not "+
				"stay in place:\n%s", forbidden, failure)
		}
	}
	if !strings.Contains(script, "event.preventDefault();\n      runSearch();") {
		t.Errorf("the query form's submit does not prevent navigation before it searches")
	}

	// Removing the prefix and searching again renders the graph.
	rec := doGraphData(t, name, url.Values{"q": {"MATCH (n) RETURN n"}, "limit": {"100"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("the unprefixed statement answered %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding the graph view: %v", err)
	}
	if len(view.Nodes) != 3 {
		t.Errorf("the unprefixed statement renders %d node(s), want the 3 seeded", len(view.Nodes))
	}
}
