package web

// Web half of SPEC/WEB.md Acceptance Criterion 151: the keyword spacing of a
// schema-introspection command is the ENGINE's business, and this endpoint holds
// no opinion about it.
//
// This file has now asserted three different things, and the sequence is worth
// keeping in view because each step withdrew a rule rather than adding one:
//
//  1. The endpoint published an `invalid_keyword_spacing` class and refused a
//     badly spaced command with a message naming the accepted spelling, while
//     answering the well-spaced spelling HTTP 200.
//  2. rmp task #344 made the endpoint refuse the whole schema-introspection
//     family at every spacing, so the two spellings received the IDENTICAL
//     response and the endpoint published no spacing class at all.
//  3. The guard rail is withdrawn entirely (rmp task #364). The endpoint executes
//     what it is given, so the two spellings now differ again — and the
//     difference is the engine's routing, not a rule of this endpoint's. A
//     single space routes the statement to the engine's schema parser, which
//     runs it and returns tabular rows the response shape cannot carry, so the
//     answer is HTTP 200 with an empty graph. Any other separator is not routed
//     there and fails in the ordinary parser, so the answer is HTTP 400 with
//     `kind` `execution` and the engine's own diagnostic.
//
// The endpoint neither refuses the badly spaced form before execution nor
// repairs its spacing, and it publishes no class of its own for either
// (SPEC/GRAPH.md § What Groadmap Does Not Check, item 7).

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// spacingPairs pair each badly spaced spelling with the well-spaced spelling of
// the SAME command, so the two differ in the separator and in nothing else. All
// four target keywords are covered: the two plurals are matched by differently
// spelled patterns in the injection-suppression matcher, so a regression in
// either would drop one form silently.
var spacingPairs = []struct {
	name      string
	misspaced string
	oneSpace  string
}{
	{"two spaces before INDEXES", "SHOW  INDEXES", "SHOW INDEXES"},
	{"a tab before INDEXES", "SHOW\tINDEXES", "SHOW INDEXES"},
	{"a line break before INDEXES", "SHOW\nINDEXES", "SHOW INDEXES"},
	{"two spaces before the singular INDEX", "SHOW  INDEX", "SHOW INDEX"},
	{"a tab before the singular INDEX", "SHOW\tINDEX", "SHOW INDEX"},
	{"two spaces before CONSTRAINTS", "SHOW  CONSTRAINTS", "SHOW CONSTRAINTS"},
	{"a line break before CONSTRAINTS", "SHOW\nCONSTRAINTS", "SHOW CONSTRAINTS"},
	{"two spaces before the singular CONSTRAINT", "SHOW  CONSTRAINT", "SHOW CONSTRAINT"},
	{"a tab before the singular CONSTRAINT", "SHOW\tCONSTRAINT", "SHOW CONSTRAINT"},
	{"a lowercase statement with two spaces", "show  indexes", "show indexes"},
	{"a mixed-case statement with a tab", "sHoW\tcOnStRaInT", "sHoW cOnStRaInT"},
	{"a comment standing in for the separator", "SHOW /* which ones? */ INDEXES", "SHOW INDEXES"},
	{"a misspaced statement with a projection tail", "SHOW  INDEXES YIELD name, type RETURN name", "SHOW INDEXES YIELD name, type RETURN name"},
}

// TestHandleGraphData_KeywordSpacingIsTheEnginesVerdict is Acceptance Criterion
// 151 itself: for each pair, the well-spaced spelling is answered HTTP 200 with
// an empty graph and the badly spaced one is answered HTTP 400 with `kind`
// `execution` carrying the ENGINE's diagnostic.
//
// The `execution` kind is what makes the difference the engine's rather than the
// endpoint's: the statement reached the engine and failed there. A refusal
// decided before execution could not carry an engine diagnostic at all, which is
// why the diagnostic is asserted and not only the status.
func TestHandleGraphData_KeywordSpacingIsTheEnginesVerdict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	for _, tc := range spacingPairs {
		t.Run(tc.name, func(t *testing.T) {
			good := doGraphData(t, name, url.Values{"q": {tc.oneSpace}})
			if good.Code != http.StatusOK {
				kind, reason := decodeQueryError(t, good.Body.Bytes())
				t.Fatalf("the well-spaced spelling %q: status = %d, want 200 with an empty graph "+
					"(kind=%q, reason=%q)", tc.oneSpace, good.Code, kind, reason)
			}
			var view map[string]json.RawMessage
			if err := json.Unmarshal(good.Body.Bytes(), &view); err != nil {
				t.Fatalf("decoding the well-spaced response: %v; body=%q", err, good.Body.String())
			}
			for _, key := range []string{"nodes", "edges"} {
				if string(view[key]) != "[]" {
					t.Errorf("%s = %s, want []: a schema listing carries no node and no edge", key, view[key])
				}
			}

			bad := doGraphData(t, name, url.Values{"q": {tc.misspaced}})
			if bad.Code != http.StatusBadRequest {
				t.Fatalf("the badly spaced spelling %q: status = %d, want 400; body=%q",
					tc.misspaced, bad.Code, bad.Body.String())
			}
			kind, reason := decodeQueryError(t, bad.Body.Bytes())
			if kind != graphErrExecution {
				t.Errorf("kind = %q, want %q: the endpoint publishes no class of its own for the "+
					"spacing; the statement failed in the engine", kind, graphErrExecution)
			}
			if !strings.Contains(reason, "cypher:") {
				t.Errorf("error = %q, want the engine's own diagnostic: a message without one would "+
					"mean the statement never reached the engine", reason)
			}
		})
	}
}

// TestHandleGraphData_PublishesNoKeywordSpacingKind asserts the absence of the
// withdrawn value in the two ways it can be observed from outside the server: no
// response carries the kind, and no message prescribes a spacing correction.
//
// The endpoint has no opinion about the spacing to state, so a message that
// asked for a correction would be this endpoint speaking for the engine
// (SPEC/WEB.md Acceptance Criteria 123 and 151).
func TestHandleGraphData_PublishesNoKeywordSpacingKind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	probes := []url.Values{
		{"q": {"SHOW INDEXES"}},
		{"q": {"SHOW  INDEXES"}},
		{"q": {"SHOW\tCONSTRAINTS"}},
		{"q": {"SHOW\nINDEX"}},
		{"q": {"show  constraint"}},
		{"q": {"SHOW  INDEXES YIELD name"}},
		{"q": {"MATCH (n) DELETE n"}},
		{"q": {"MATCH (n) RETURN"}},
		{"q": {"MATCH (a)-[e]-(b) RETURN type(e)"}},
		{"limit": {"7"}},
	}

	for _, params := range probes {
		t.Run(params.Encode(), func(t *testing.T) {
			rec := doGraphData(t, name, params)
			if rec.Code == http.StatusOK {
				return
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 200 or 400; body=%q", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding error body: %v; body=%q", err, rec.Body.String())
			}
			if body["kind"] == "invalid_keyword_spacing" {
				t.Errorf("body carries kind invalid_keyword_spacing, a value this endpoint does not publish; body=%q", rec.Body.String())
			}
			reason, _ := body["error"].(string)
			for _, forbidden := range []string{"keyword spacing", "exactly one space"} {
				if strings.Contains(reason, forbidden) {
					t.Errorf("error = %q names %q: the spacing is the engine's routing rule and this "+
						"endpoint states no correction for it", reason, forbidden)
				}
			}
		})
	}
}

// TestHandleGraphData_SpacingDoesNotWidenTheSuppressedClass is the narrowness
// control for the tests above. The injection-suppression matcher recognises
// exactly the four target keywords; every other SHOW family is outside it, and
// the engine rejects those at the parser whether or not a LIMIT is appended, so
// they reach the engine and fail there like any other unsupported statement.
func TestHandleGraphData_SpacingDoesNotWidenTheSuppressedClass(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	for _, query := range []string{
		"SHOW  INDEXER",
		"SHOW  DATABASES",
		"SHOW DATABASES",
		"SHOW FUNCTIONS",
	} {
		t.Run(query, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {query}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
			}
			kind, reason := decodeQueryError(t, rec.Body.Bytes())
			if kind != graphErrExecution {
				t.Errorf("kind = %q, want %q; reason=%q", kind, graphErrExecution, reason)
			}
		})
	}
}
