package web

// Web half of SPEC/WEB.md Acceptance Criterion 151: KEYWORD SPACING CHANGES
// NOTHING about what the graph data endpoint answers.
//
// This file used to assert the opposite. The endpoint published an
// `invalid_keyword_spacing` class and refused a badly spaced
// schema-introspection command with a message naming the spacing and the
// accepted spelling, while accepting the well-spaced spelling with HTTP 200.
//
// The endpoint now refuses the whole schema-introspection class (rmp task #344,
// Acceptance Criterion 157), and once it does, a spacing complaint names a
// correction that does not work: the corrected statement is refused too, for the
// same reason. So the two spellings receive the SAME status, the SAME kind and
// the SAME message, and this endpoint publishes no keyword-spacing class at all.
//
// The CLI is untouched and the divergence is deliberate. `graph query`,
// `graph search` and `graph update` ACCEPT the class, so on those surfaces the
// spacing genuinely is the whole objection and they keep rejecting it with exit
// code 6 and a message that names it (SPEC/GRAPH.md Acceptance Criterion 39,
// which states that asserting the spacing rule HERE must fail; the CLI half is
// asserted in internal/commands/graph_introspect_spacing_test.go and end-to-end
// in tests/test_35_web_interface.py).

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
// spelled patterns in the shared classifier, so a regression in either would
// drop one form silently.
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

// TestHandleGraphData_KeywordSpacingChangesNothing is the criterion itself: for
// each pair, the two responses are compared TO EACH OTHER rather than each
// against a literal, which is what makes "the spacing changes nothing" the thing
// asserted. Status and body must be equal, byte for byte.
//
// The equality alone would be satisfied by a server that answered both spellings
// with the same wrong thing — HTTP 200 and an empty graph, for instance — so the
// class both must carry is asserted too.
func TestHandleGraphData_KeywordSpacingChangesNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	for _, tc := range spacingPairs {
		t.Run(tc.name, func(t *testing.T) {
			bad := doGraphData(t, name, url.Values{"q": {tc.misspaced}})
			good := doGraphData(t, name, url.Values{"q": {tc.oneSpace}})

			if bad.Code != good.Code {
				t.Fatalf("status differs across the spelling: %q got %d, %q got %d",
					tc.misspaced, bad.Code, tc.oneSpace, good.Code)
			}
			if bad.Body.String() != good.Body.String() {
				t.Fatalf("body differs across the spelling:\n %q -> %s\n %q -> %s",
					tc.misspaced, bad.Body.String(), tc.oneSpace, good.Body.String())
			}

			// And what they agree on is the refusal of the class.
			if bad.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for both spellings; body=%q", bad.Code, bad.Body.String())
			}
			kind, reason := decodeQueryError(t, bad.Body.Bytes())
			if kind != graphErrSchemaIntrospection {
				t.Errorf("kind = %q, want %q for both spellings", kind, graphErrSchemaIntrospection)
			}
			if !strings.Contains(reason, "rmp graph query") {
				t.Errorf("error = %q, want it to name rmp graph query as where a schema listing is obtained", reason)
			}
		})
	}
}

// TestHandleGraphData_PublishesNoKeywordSpacingKind asserts the removal of the
// value from the endpoint's published contract, in the two ways it can be
// observed from outside the server: no response carries the kind, and no message
// names the spacing or offers a spelling to correct it to.
//
// On this endpoint correcting the spacing changes nothing, so a message that
// asked for it would send the caller round a loop ending in this same refusal
// (SPEC/WEB.md § Query-Bar Error Handling, case 10, "Why one kind and not two";
// Acceptance Criteria 123 and 151).
func TestHandleGraphData_PublishesNoKeywordSpacingKind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	// Every spelling of the class, well spaced and badly spaced, plus the
	// requests that reach the other failure classes: none of them may answer
	// with the withdrawn value.
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
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding error body: %v; body=%q", err, rec.Body.String())
			}
			if body["kind"] == "invalid_keyword_spacing" {
				t.Errorf("body carries kind invalid_keyword_spacing, a value this endpoint does not publish; body=%q", rec.Body.String())
			}
			reason, _ := body["error"].(string)
			for _, forbidden := range []string{"keyword spacing", "exactly one space", "one space"} {
				if strings.Contains(reason, forbidden) {
					t.Errorf("error = %q names %q: on this endpoint the spacing is not what is wrong, and correcting it changes nothing", reason, forbidden)
				}
			}
		})
	}
}

// TestHandleGraphData_SpacingDoesNotWidenTheClass is the narrowness control for
// the two tests above. A statement that is NOT a schema-introspection command
// under any spacing must still reach the engine and fail there, so the refusal
// covers the class the classifier recognises and not every statement beginning
// with SHOW.
func TestHandleGraphData_SpacingDoesNotWidenTheClass(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedGraphWithSchema(t, "web-ui-rollout")

	cases := []struct {
		name     string
		query    string
		wantKind string
	}{
		{"a near miss on the keyword is an execution failure", "SHOW  INDEXER", graphErrExecution},
		{"an unimplemented SHOW family is an execution failure", "SHOW  DATABASES", graphErrExecution},
		{"a well-spaced unimplemented SHOW family is one too", "SHOW DATABASES", graphErrExecution},
		{"a delete is still not read-only", `MATCH (n) DELETE n`, graphErrNotReadOnly},
		{"schema-mutating DDL is still not read-only", `CREATE   INDEX spec_idx FOR (n:Spec) ON (n.key)`, graphErrNotReadOnly},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {tc.query}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
			}
			kind, reason := decodeQueryError(t, rec.Body.Bytes())
			if kind != tc.wantKind {
				t.Errorf("kind = %q, want %q; reason=%q", kind, tc.wantKind, reason)
			}
		})
	}
}
