package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/store/recovery"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// Regression suite for the node-limit SUPPRESSION rule (SPEC/WEB.md § Graph Data
// Endpoint, node-limit injection, Suppression 2; § Graph Query Bar rule 6;
// Acceptance Criteria 48 and 111).
//
// The defect: the endpoint appended "\nLIMIT <n>" to every query without a
// top-level LIMIT of its own, including the two read forms the guard rail admits
// that can carry no LIMIT clause at all — a schema-introspection command and a
// standalone procedure call. Both failed in the PARSER instead of running, so
// the endpoint was stricter than the contract it publishes and stricter than
// `rmp graph query`, which runs both.

// distinctLabelCount is the number of extra single-node labels the suppression
// tests seed. It must exceed the smallest allowed node limit (50) so a projected
// procedure call has strictly more rows than the injected LIMIT permits: only
// then can a shrinking row count prove the limit was applied AND obeyed, rather
// than merely present in the query string.
const distinctLabelCount = 60

// seedLabelledGraph seeds the roadmap's store with the small semantic graph the
// other graph tests use plus distinctLabelCount extra nodes, each carrying a
// label of its own, and returns the total number of distinct labels in the
// store. The labels are what db.labels() enumerates, so they give the procedure
// calls below a result set large enough to be bounded.
//
// The extra nodes are created by ONE statement with distinctLabelCount patterns
// (a label cannot be parameterised, so UNWIND cannot generate them), which keeps
// the seed to a single transaction.
func seedLabelledGraph(t *testing.T, name string) int {
	t.Helper()

	patterns := make([]string, 0, distinctLabelCount)
	for i := 1; i <= distinctLabelCount; i++ {
		patterns = append(patterns, fmt.Sprintf("(:GraphLabel%02d)", i))
	}
	seeds := append(graphSeedQueries(), "CREATE "+strings.Join(patterns, ", "))
	seedGraph(t, name, seeds...)

	// graphSeedQueries creates Spec and Code nodes: two labels, plus one per
	// extra node.
	return distinctLabelCount + 2
}

// openGraphReadEngine opens the roadmap's graph store on the engine's READ path,
// exactly as loadGraphView does (recovery.Open then cypher.NewEngine, never
// RunInTx), so a test can execute the string applyGraphLimit produced and count
// the rows the endpoint's engine would have seen.
func openGraphReadEngine(t *testing.T, name string) *cypher.Engine {
	t.Helper()

	roadmapDir, err := utils.GetRoadmapDir(name)
	if err != nil {
		t.Fatalf("resolving roadmap dir: %v", err)
	}
	res, err := recovery.Open[string, float64](filepath.Join(roadmapDir, "graph"), recovery.Options[string, float64]{
		Codec:       txn.NewStringCodec(),
		WeightCodec: txn.NewFloat64WeightCodec(),
	})
	if err != nil {
		t.Fatalf("opening graph store: %v", err)
	}
	return cypher.NewEngine(res.Graph)
}

// countGraphRows runs query on the engine's read path and returns the number of
// rows it produced. A query that fails is a test failure reported with the
// engine's own message, which is what makes the parse failures this task fixes
// legible when a regression reintroduces them.
func countGraphRows(t *testing.T, engine *cypher.Engine, query string) int {
	t.Helper()

	result, err := engine.Run(context.Background(), query, nil)
	if err != nil {
		t.Fatalf("running %q: %v", query, err)
	}
	defer result.Close() //nolint:errcheck // read path; close commits nothing

	rows := 0
	for result.Next() {
		rows++
	}
	if err := result.Err(); err != nil {
		t.Fatalf("iterating %q: %v", query, err)
	}
	return rows
}

// TestApplyGraphLimit_SuppressedForNonLimitableForms asserts the injection is
// suppressed for exactly the two statement forms that admit no LIMIT clause, and
// applied to everything else — including the forms that only look like them
// (SPEC/WEB.md § Graph Data Endpoint, Suppression 2; Acceptance Criterion 111).
//
// The two recognisers run on the masked normalization and are anchored to the
// start of the statement, so the table also pins the masking and the anchoring:
// a SHOW, CALL, or RETURN keyword confined to a string literal, a comment, or a
// backtick identifier does not change the verdict, and a CALL nested inside a
// larger query is not a standalone call.
func TestApplyGraphLimit_SuppressedForNonLimitableForms(t *testing.T) {
	const unchanged = "" // want == query: the endpoint must inject nothing

	cases := []struct {
		name  string
		query string
		limit int
		want  string // unchanged, or the exact injected result
	}{
		// ── Suppression 2 (i): the schema-introspection class, whole ──────────
		{
			name:  "SHOW INDEXES admits no LIMIT",
			query: "SHOW INDEXES",
			limit: 100,
			want:  unchanged,
		},
		{
			name:  "singular SHOW INDEX admits no LIMIT",
			query: "SHOW INDEX",
			limit: 100,
			want:  unchanged,
		},
		{
			name:  "SHOW CONSTRAINTS admits no LIMIT",
			query: "SHOW CONSTRAINTS",
			limit: 250,
			want:  unchanged,
		},
		{
			name:  "singular SHOW CONSTRAINT admits no LIMIT",
			query: "SHOW CONSTRAINT",
			limit: 250,
			want:  unchanged,
		},
		{
			// A YIELD/RETURN tail does not make a LIMIT injectable: the engine's
			// SHOW parser rejects ORDER BY / SKIP / LIMIT on every SHOW form.
			name:  "SHOW with a YIELD RETURN tail is still the same class",
			query: "SHOW INDEXES YIELD name, state RETURN name",
			limit: 50,
			want:  unchanged,
		},
		{
			name:  "SHOW with a WHERE tail is still the same class",
			query: "SHOW INDEXES WHERE type = 'btree'",
			limit: 50,
			want:  unchanged,
		},
		{
			name:  "lowercase show is the same class",
			query: "show constraints",
			limit: 500,
			want:  unchanged,
		},
		{
			name:  "a leading comment does not hide the SHOW",
			query: "// which indexes exist?\nSHOW INDEXES",
			limit: 100,
			want:  unchanged,
		},
		{
			// The class is NOT widened: every other SHOW is outside it, and the
			// engine rejects those at the parser with or without a LIMIT, so the
			// endpoint must not quietly adopt them here.
			name:  "SHOW DATABASES is not the introspection class",
			query: "SHOW DATABASES",
			limit: 100,
			want:  "SHOW DATABASES\nLIMIT 100",
		},

		// ── Suppression 2 (ii): a standalone procedure call ───────────────────
		{
			name:  "standalone CALL with no YIELD",
			query: "CALL db.stats.refresh()",
			limit: 100,
			want:  unchanged,
		},
		{
			name:  "standalone CALL returning rows",
			query: "CALL db.labels()",
			limit: 50,
			want:  unchanged,
		},
		{
			name:  "standalone CALL with a YIELD but no RETURN",
			query: "CALL db.labels() YIELD label",
			limit: 50,
			want:  unchanged,
		},
		{
			name:  "standalone CALL without argument parentheses",
			query: "CALL db.labels",
			limit: 50,
			want:  unchanged,
		},
		{
			name:  "lowercase call with leading whitespace",
			query: "   call db.propertyKeys()",
			limit: 250,
			want:  unchanged,
		},
		{
			name:  "a leading block comment does not hide the CALL",
			query: "/* schema */ CALL db.relationshipTypes()",
			limit: 100,
			want:  unchanged,
		},
		{
			// Masking: the only RETURN is inside a line comment, so it does not
			// project the call and the statement stays standalone.
			name:  "RETURN only inside a line comment does not project the call",
			query: "CALL db.labels() // RETURN label",
			limit: 100,
			want:  unchanged,
		},
		{
			name:  "RETURN only inside a block comment does not project the call",
			query: "CALL db.labels() /* RETURN label */",
			limit: 100,
			want:  unchanged,
		},
		{
			name:  "RETURN only inside a backtick identifier does not project the call",
			query: "CALL db.labels() YIELD `RETURN`",
			limit: 100,
			want:  unchanged,
		},
		{
			name:  "RETURN only inside a string literal does not project the call",
			query: `CALL db.index.fulltext.queryNodes("RETURN n")`,
			limit: 100,
			want:  unchanged,
		},

		// ── Not standalone: a projected CALL is an ordinary reading query ─────
		{
			name:  "CALL projected through RETURN receives the injection",
			query: "CALL db.labels() YIELD label RETURN label",
			limit: 50,
			want:  "CALL db.labels() YIELD label RETURN label\nLIMIT 50",
		},
		{
			name:  "CALL projected through WITH and RETURN receives the injection",
			query: "CALL db.labels() YIELD label WITH label RETURN label",
			limit: 100,
			want:  "CALL db.labels() YIELD label WITH label RETURN label\nLIMIT 100",
		},
		{
			// Anchoring: CALL is not the first clause, so the statement is an
			// ordinary reading query however the call is written.
			name:  "CALL nested inside a larger query is not standalone",
			query: "MATCH (n) CALL db.labels() YIELD label RETURN n, label",
			limit: 50,
			want:  "MATCH (n) CALL db.labels() YIELD label RETURN n, label\nLIMIT 50",
		},
		{
			// The anchor, isolated: a mid-query CALL with no RETURN at all. The
			// engine rejects this query for its missing RETURN, but the case pins
			// the anchoring the spec requires independently of the RETURN check,
			// so dropping \A from the recogniser is caught here rather than by
			// whichever real query happens to trip over it later.
			name:  "a mid-query CALL is not standalone even with no RETURN",
			query: "MATCH (n) CALL db.labels() YIELD label",
			limit: 100,
			want:  "MATCH (n) CALL db.labels() YIELD label\nLIMIT 100",
		},
		{
			// Suppression 1 still wins over everything: the caller's own LIMIT is
			// respected on a projected call exactly as on a MATCH.
			name:  "a user LIMIT on a projected CALL is respected",
			query: "CALL db.labels() YIELD label RETURN label LIMIT 5",
			limit: 100,
			want:  unchanged,
		},

		// ── Masking and anchoring on ordinary reads ───────────────────────────
		{
			name:  "SHOW INDEXES only inside a string literal is an ordinary read",
			query: `MATCH (n) WHERE n.key = 'SHOW INDEXES' RETURN n`,
			limit: 100,
			want:  "MATCH (n) WHERE n.key = 'SHOW INDEXES' RETURN n\nLIMIT 100",
		},
		{
			name:  "CALL only inside a string literal is an ordinary read",
			query: `MATCH (n) WHERE n.key = 'CALL db.labels()' RETURN n`,
			limit: 100,
			want:  "MATCH (n) WHERE n.key = 'CALL db.labels()' RETURN n\nLIMIT 100",
		},
		{
			name:  "CALL only inside a backtick label is an ordinary read",
			query: "MATCH (n:`CALL`) RETURN n",
			limit: 250,
			want:  "MATCH (n:`CALL`) RETURN n\nLIMIT 250",
		},
		{
			name:  "SHOW only inside a comment is an ordinary read",
			query: "MATCH (n) RETURN n // SHOW INDEXES",
			limit: 50,
			want:  "MATCH (n) RETURN n // SHOW INDEXES\nLIMIT 50",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.want
			if want == unchanged {
				want = tc.query
			}
			if got := applyGraphLimit(tc.query, tc.limit); got != want {
				t.Errorf("applyGraphLimit(%q, %d) = %q, want %q", tc.query, tc.limit, got, want)
			}
		})
	}
}

// TestApplyGraphLimit_SuppressedFormsExecute proves the suppression is not a
// cosmetic string change: the two non-limitable forms actually RUN on the
// engine's read path once nothing is appended to them, and the injected form
// would have failed in the parser (SPEC/WEB.md Acceptance Criterion 111).
//
// It also pins the spec's consequence that a suppressed query is NOT bounded by
// the node limit: the standalone call returns every label, more than the
// resolved limit allows.
func TestApplyGraphLimit_SuppressedFormsExecute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	labels := seedLabelledGraph(t, name)
	engine := openGraphReadEngine(t, name)

	if labels <= 50 {
		t.Fatalf("seeded %d labels, need more than the smallest allowed limit (50)", labels)
	}

	t.Run("schema introspection executes", func(t *testing.T) {
		const q = "SHOW INDEXES"
		executed := applyGraphLimit(q, 100)
		if executed != q {
			t.Fatalf("applyGraphLimit(%q, 100) = %q, want it unchanged", q, executed)
		}
		countGraphRows(t, engine, executed) // fails loudly if it does not parse

		// The negative control: the clause the endpoint used to append is what
		// made this form unusable.
		if _, err := engine.Run(context.Background(), q+"\nLIMIT 100", nil); err == nil {
			t.Error("SHOW INDEXES accepted an appended LIMIT: the suppression would no longer be needed, so this test's premise must be re-examined")
		}
	})

	t.Run("standalone call executes and is not bounded by the node limit", func(t *testing.T) {
		const q = "CALL db.labels()"
		executed := applyGraphLimit(q, 50)
		if executed != q {
			t.Fatalf("applyGraphLimit(%q, 50) = %q, want it unchanged", q, executed)
		}
		if rows := countGraphRows(t, engine, executed); rows != labels {
			t.Errorf("standalone call returned %d rows, want all %d labels: a suppressed query is not bounded by the node limit", rows, labels)
		}
		if _, err := engine.Run(context.Background(), q+"\nLIMIT 50", nil); err == nil {
			t.Error("standalone CALL accepted an appended LIMIT: the suppression would no longer be needed, so this test's premise must be re-examined")
		}
	})

	// A projected call is an ordinary reading query: it receives the injection
	// AND obeys it. Asserting the row count is the point — a test that only
	// checked the query string for "LIMIT" would pass even if the clause landed
	// somewhere the engine ignores.
	t.Run("projected call receives the limit and obeys it", func(t *testing.T) {
		const q = "CALL db.labels() YIELD label RETURN label"
		unbounded := countGraphRows(t, engine, q)
		if unbounded != labels {
			t.Fatalf("unbounded projected call returned %d rows, want %d", unbounded, labels)
		}
		executed := applyGraphLimit(q, 50)
		if executed != q+"\nLIMIT 50" {
			t.Fatalf("applyGraphLimit(%q, 50) = %q, want the limit appended", q, executed)
		}
		if rows := countGraphRows(t, engine, executed); rows != 50 {
			t.Errorf("projected call returned %d rows, want 50: the injected limit must be applied and obeyed (unbounded run returned %d)", rows, unbounded)
		}
	})
}

// TestHandleGraphData_NonLimitableFormsRunThroughTheEndpoint is the end-to-end
// acceptance test: each non-limitable form the endpoint EXECUTES is answered
// with 200 and the ordinary {"nodes": [], "edges": []} shape, not the 400 parse
// failure the injected LIMIT used to produce (SPEC/WEB.md Acceptance Criterion
// 111).
//
// The empty arrays are the specified answer, not an omission: these statements
// return tabular rows, which carry no graph elements for the result walk to
// collect, and the response shape is deliberately unchanged.
//
// The schema-introspection command is deliberately absent from this list. It
// admits no LIMIT either, but this criterion does not reach it: the endpoint
// refuses the class before the injection decision is taken, and Acceptance
// Criterion 111 states that asserting HTTP 200 for it here would contradict
// Acceptance Criterion 157. The sibling test below asserts that refusal, so the
// form is covered rather than dropped.
func TestHandleGraphData_NonLimitableFormsRunThroughTheEndpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedLabelledGraph(t, name)

	queries := []string{
		// Standalone procedure calls, projected and not.
		"CALL db.stats.refresh()",
		"CALL db.labels()",
		"CALL db.propertyKeys() YIELD propertyKey",
		"CALL db.relationshipTypes()",
		"CALL db.constraints()",
		"CALL db.indexes()",
		"CALL db.schema.visualization()",
	}

	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {q}, "limit": {"100"}})
			if rec.Code != http.StatusOK {
				kind, reason := decodeQueryError(t, rec.Body.Bytes())
				t.Fatalf("status = %d, want 200: the endpoint must run this form, not fail it in the parser (kind=%q, reason=%q)", rec.Code, kind, reason)
			}

			// Decode into raw messages so an empty array is distinguishable from
			// a null: the specified shape is [], and nothing else was added.
			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding body: %v; body=%q", err, rec.Body.String())
			}
			if len(body) != 2 {
				t.Errorf("body has %d keys (%v), want exactly nodes and edges: the response shape must not change", len(body), body)
			}
			for _, key := range []string{"nodes", "edges"} {
				raw, ok := body[key]
				if !ok {
					t.Fatalf("body has no %q key; body=%q", key, rec.Body.String())
				}
				if string(raw) != "[]" {
					t.Errorf("%s = %s, want []: tabular rows carry no graph elements", key, raw)
				}
			}
		})
	}
}

// TestHandleGraphData_SchemaIntrospectionNeverReachesTheInjectionDecision is the
// other half of Suppression 2's boundary, and the reason the SHOW forms left the
// list above.
//
// The schema-introspection command admits no LIMIT clause, so before rmp task
// #344 it was suppressed here and executed. The endpoint now refuses the class
// outright, BEFORE the injection decision is taken, so the question of injecting
// into one never arises (SPEC/WEB.md § Graph Data Endpoint, Suppression 2, third
// bullet; § Query-Bar Error Handling, case 10).
//
// This test is what keeps the two rules consistent: it fails both if the refusal
// is lost (the form would come back 200) and if the refusal were moved AFTER the
// injection (the form would come back 400 with kind execution, carrying the
// engine's parse diagnostic for the appended LIMIT).
func TestHandleGraphData_SchemaIntrospectionNeverReachesTheInjectionDecision(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedLabelledGraph(t, name)

	for _, q := range []string{
		"SHOW INDEXES",
		"SHOW CONSTRAINTS",
		"SHOW INDEXES YIELD name, state RETURN name",
		"SHOW CONSTRAINTS WHERE type = 'UNIQUE'",
	} {
		t.Run(q, func(t *testing.T) {
			rec := doGraphData(t, name, url.Values{"q": {q}, "limit": {"100"}})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: the endpoint refuses this class before the injection decision; body=%q", rec.Code, rec.Body.String())
			}
			kind, reason := decodeQueryError(t, rec.Body.Bytes())
			if kind != graphErrSchemaIntrospection {
				t.Errorf("kind = %q, want %q; reason=%q", kind, graphErrSchemaIntrospection, reason)
			}
			// Not an execution failure: the statement never reached the engine,
			// so no engine diagnostic can appear in the message. That is what
			// distinguishes "refused before the injection" from "injected, then
			// failed in the parser".
			if strings.Contains(reason, "cypher:") || strings.Contains(reason, "LIMIT") {
				t.Errorf("error = %q, want no engine diagnostic and no mention of LIMIT: the statement never reached the engine", reason)
			}
		})
	}
}

// TestHandleGraphData_ProjectedCallIsNotStandalone asserts the boundary from the
// other side, through the endpoint: a CALL projected through a top-level RETURN
// is an ordinary reading query, so it receives the injection and is bounded by
// it. The call projects node values so the bound is observable in the response's
// own node array, which is the endpoint's only measurable output.
func TestHandleGraphData_ProjectedCallIsNotStandalone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, `UNWIND range(1,120) AS i CREATE (:Bulk {i:i})`)

	// db.labels() yields one row per label; the MATCH re-expands each label's
	// nodes, so the projected result carries real nodes and the endpoint can be
	// measured on the node count it returns.
	const q = "CALL db.labels() YIELD label MATCH (n) RETURN n"

	rec := doGraphData(t, name, url.Values{"q": {q}, "limit": {"50"}})
	if rec.Code != http.StatusOK {
		kind, reason := decodeQueryError(t, rec.Body.Bytes())
		t.Fatalf("status = %d, want 200 (kind=%q, reason=%q)", rec.Code, kind, reason)
	}
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(view.Nodes) != 50 {
		t.Errorf("nodes = %d, want 50: a CALL projected through RETURN must receive the node limit and obey it", len(view.Nodes))
	}
}
