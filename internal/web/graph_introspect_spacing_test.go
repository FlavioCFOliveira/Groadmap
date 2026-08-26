package web

// Web half of the regression for rmp task #275, and of SPEC/WEB.md Acceptance
// Criterion 151 (the web surface of SPEC/GRAPH.md Acceptance Criterion 39).
//
// The graph data endpoint shares the CLI's guard rail, so a schema-introspection
// command whose keyword spacing the engine does not accept — `SHOW  INDEXES`
// with two spaces, with a tab, or with a line break — is refused here before it
// is executed, exactly as `rmp graph query` refuses it.
//
// The refusal carries its OWN failure class, `invalid_keyword_spacing`, and not
// `not_read_only`. A SHOW statement reads the schema and writes nothing whatever
// its spacing, so answering `not_read_only` would publish a classification the
// message printed beside it contradicts, and would tell a client the query
// writes when it does not. The two failures also have different fixes: one query
// must be rewritten to stop writing, the other only to close a gap between two
// keywords (SPEC/WEB.md § Query-Bar Error Handling, case 10).
//
// Precedence, which these tests pin because it is the part a refactor is most
// likely to reorder: `invalid_limit`, then `not_read_only`, then
// `invalid_keyword_spacing` (rule 6).

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// misspacedIntrospectionQueries are the separators the endpoint must refuse,
// paired with the accepted spelling its message has to name. One space is the
// only accepted separator, and all four target keywords are covered: the two
// plurals are matched by differently spelled patterns, so a regression in either
// would drop one form silently.
var misspacedIntrospectionQueries = []struct {
	name     string
	query    string
	accepted string
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
	{"a lowercase statement with two spaces", "show  indexes", "SHOW INDEXES"},
	{"a mixed-case statement with a tab", "sHoW\tcOnStRaInT", "SHOW CONSTRAINT"},
	{"a misspaced statement with a projection tail", "SHOW  INDEXES YIELD name, type RETURN name", "SHOW INDEXES"},
}

// TestHandleGraphData_IntrospectionKeywordSpacingRejected is the core web
// regression: every misspaced schema-introspection command is answered HTTP 400
// with `kind` `invalid_keyword_spacing`, the body carries no graph, and the
// message names the spacing and the accepted spelling without describing the
// query as not read-only or echoing the engine's parse diagnostic.
//
// It fails if cypherguard's reIntrospect is widened back to arbitrary
// whitespace: the endpoint would admit each query, hand it to the engine, and
// answer `kind` `execution` carrying the diagnostic that names the wrong problem.
func TestHandleGraphData_IntrospectionKeywordSpacingRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	for _, tc := range misspacedIntrospectionQueries {
		t.Run(tc.name, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {tc.query}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for %q; body=%q", rec.Code, tc.query, rec.Body.String())
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding error body: %v; body=%q", err, rec.Body.String())
			}
			if body["kind"] != graphErrInvalidKeywordSpacing {
				t.Errorf("kind = %v, want %q; body=%q", body["kind"], graphErrInvalidKeywordSpacing, rec.Body.String())
			}
			// The query was never run: the body is the error shape and carries
			// no graph at all.
			if got := slices.Sorted(maps.Keys(body)); !slices.Equal(got, []string{"error", "kind"}) {
				t.Errorf("body fields = %v, want exactly [error kind]; body=%q", got, rec.Body.String())
			}

			reason, _ := body["error"].(string)
			for _, want := range []string{"schema-introspection command", "exactly one space", "keyword spacing", tc.accepted} {
				if !strings.Contains(reason, want) {
					t.Errorf("error = %q, want it to contain %q", reason, want)
				}
			}
			// The message must not misdescribe the failure, and must not be the
			// engine's diagnostic: the query never reached the engine.
			for _, forbidden := range []string{"not read-only", "cypher: parse", `unexpected "SHOW"`} {
				if strings.Contains(reason, forbidden) {
					t.Errorf("error = %q, want it never to contain %q", reason, forbidden)
				}
			}
		})
	}

	// Nothing was executed and nothing changed: a default read still returns the
	// three seeded nodes.
	rec := doGraphData(t, name, nil)
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding post-rejection read: %v", err)
	}
	if len(view.Nodes) != 3 {
		t.Errorf("after the spacing rejections, nodes = %d, want 3", len(view.Nodes))
	}
}

// TestHandleGraphData_IntrospectionOneSpaceStillAccepted is the control that
// makes the test above non-vacuous: the SAME statements with exactly one space
// are accepted, executed, and answered with the normal graph shape and HTTP 200.
// The two differ in the separator and in nothing else.
//
// Without this control an endpoint that had simply stopped supporting schema
// introspection altogether would satisfy every rejection assertion.
func TestHandleGraphData_IntrospectionOneSpaceStillAccepted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	for _, query := range []string{
		"SHOW INDEXES",
		"SHOW INDEX",
		"SHOW CONSTRAINTS",
		"SHOW CONSTRAINT",
		"show indexes",
		"SHOW INDEXES   YIELD name",
		"   SHOW INDEXES",
		"/* schema check */ SHOW CONSTRAINTS",
	} {
		t.Run(query, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {query}})
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 for %q; body=%q", rec.Code, query, rec.Body.String())
			}
			// The success shape, not the error shape: a schema listing carries
			// no graph elements, so both arrays are present and empty.
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding body: %v; body=%q", err, rec.Body.String())
			}
			if got := slices.Sorted(maps.Keys(body)); !slices.Equal(got, []string{"edges", "nodes"}) {
				t.Errorf("body fields = %v, want exactly [edges nodes]; body=%q", got, rec.Body.String())
			}
		})
	}
}

// TestHandleGraphData_KeywordSpacingPrecedence pins the order in which the
// endpoint resolves a request that is wrong in more than one way:
// `invalid_limit`, then `not_read_only`, then `invalid_keyword_spacing`
// (SPEC/WEB.md § Query-Bar Error Handling, rule 6).
//
// Each combined case is preceded by the control that shows the lower-ranked
// objection really does fire on its own, so the precedence assertions cannot
// pass merely because the endpoint never produces the lower-ranked kind.
func TestHandleGraphData_KeywordSpacingPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	// A statement that opens with a badly spaced SHOW and also carries a writing
	// clause. It is nonsense Cypher, which is harmless precisely because it is
	// never executed: the guard rail classifies it as a write AND as a misspaced
	// introspection command, which is the only way to observe the precedence
	// between the two.
	const writingAndMisspaced = "SHOW  INDEXES YIELD name CREATE (n:Spec {key:'injected'})"

	kindOf := func(t *testing.T, params url.Values) string {
		t.Helper()
		rec := doGraphData(t, name, params)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding error body: %v; body=%q", err, rec.Body.String())
		}
		kind, _ := body["kind"].(string)
		return kind
	}

	t.Run("control: the misspaced statement alone is a spacing rejection", func(t *testing.T) {
		if got := kindOf(t, url.Values{"q": {"SHOW  INDEXES"}}); got != graphErrInvalidKeywordSpacing {
			t.Errorf("kind = %q, want %q", got, graphErrInvalidKeywordSpacing)
		}
	})

	t.Run("a writing query outranks the spacing objection", func(t *testing.T) {
		if got := kindOf(t, url.Values{"q": {writingAndMisspaced}}); got != graphErrNotReadOnly {
			t.Errorf("kind = %q, want %q: the objection that the query writes outranks the objection that it is misspelled", got, graphErrNotReadOnly)
		}
	})

	t.Run("an invalid limit outranks the spacing objection", func(t *testing.T) {
		if got := kindOf(t, url.Values{"limit": {"7"}, "q": {"SHOW  INDEXES"}}); got != graphErrInvalidLimit {
			t.Errorf("kind = %q, want %q: the limit is resolved before the query is classified", got, graphErrInvalidLimit)
		}
	})

	t.Run("an invalid limit outranks both other objections at once", func(t *testing.T) {
		if got := kindOf(t, url.Values{"limit": {"7"}, "q": {writingAndMisspaced}}); got != graphErrInvalidLimit {
			t.Errorf("kind = %q, want %q", got, graphErrInvalidLimit)
		}
	})

	// The store is untouched by any of it: the injected node never appeared.
	rec := doGraphData(t, name, nil)
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding post-rejection read: %v", err)
	}
	if len(view.Nodes) != 3 {
		t.Errorf("after the precedence probes, nodes = %d, want 3: every rejection precedes execution", len(view.Nodes))
	}
}

// TestHandleGraphData_KeywordSpacingKindIsDistinct asserts the new failure class
// is genuinely a class of its own and is never confused with the read-only
// rejection in either direction. A writing query keeps answering
// `not_read_only`, and a misspaced SHOW never does — which is the whole reason
// the value was added to the published contract.
func TestHandleGraphData_KeywordSpacingKindIsDistinct(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	cases := []struct {
		name     string
		query    string
		wantKind string
	}{
		{"a delete is still not read-only", `MATCH (n) DELETE n`, graphErrNotReadOnly},
		{"schema-mutating DDL is still not read-only", `CREATE   INDEX spec_idx FOR (n:Spec) ON (n.key)`, graphErrNotReadOnly},
		{"a misspaced SHOW is a spacing rejection", "SHOW  INDEXES", graphErrInvalidKeywordSpacing},
		{"a tab-spaced SHOW is a spacing rejection", "SHOW\tCONSTRAINTS", graphErrInvalidKeywordSpacing},
		// Not a schema-introspection command under any spacing, so the guard
		// rail must not answer for it: it reaches the engine and fails there.
		{"a near miss on the keyword is an execution failure", "SHOW  INDEXER", graphErrExecution},
		{"an unimplemented SHOW family is an execution failure", "SHOW  DATABASES", graphErrExecution},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {tc.query}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding error body: %v; body=%q", err, rec.Body.String())
			}
			if body["kind"] != tc.wantKind {
				t.Errorf("kind = %v, want %q; body=%q", body["kind"], tc.wantKind, rec.Body.String())
			}
		})
	}
}
