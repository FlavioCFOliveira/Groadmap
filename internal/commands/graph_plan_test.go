package commands

// Regression tests for the three defects SPEC/GRAPH.md § Query Plans: The
// EXPLAIN and PROFILE Prefixes closes.
//
// All three had the same cause -- the engine built a plan and Groadmap threw it
// away -- and all three were silent. In increasing order of harm:
//
//  1. EXPLAIN printed {"columns": [...], "rows": []} and exit 0, which reads as
//     a query that matched nothing.
//  2. PROFILE printed the real rows and discarded the measurement, so the
//     command that exists to report cost reported none.
//  3. EXPLAIN of a WRITING statement printed {"ok": true} -- byte-identical to a
//     real committed write, over a statement that wrote nothing.
//
// The third is the one these tests guard hardest, because its output was not
// merely uninformative but false.

import (
	"encoding/json"
	"strings"
	"testing"
)

// graphPlanOutput runs a statement through `rmp graph client` and returns its
// decoded stdout, failing the test if the statement errors.
func graphPlanOutput(t *testing.T, roadmap, query string) map[string]any {
	t.Helper()
	stdout, _ := captureStdStreams(t, func() {
		if err := runGraphClient([]string{"-r", roadmap, "--query", query}); err != nil {
			t.Fatalf("statement failed: %v\nquery=%s", err, query)
		}
	})
	var decoded map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &decoded); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\nstdout=%q", err, stdout)
	}
	return decoded
}

// seedPlanFixture creates three Spec nodes, two of which pass the predicate the
// tests filter on. Two-of-three is deliberate: it makes a filter's
// rowsRemovedByFilter a non-zero number that could not have been derived from
// the row count, so a figure that merely echoed the rows would be visible.
func seedPlanFixture(t *testing.T, roadmap string) {
	t.Helper()
	for _, q := range []string{
		"CREATE (:Spec {key:'GRAPH.md', status:'implemented'})",
		"CREATE (:Spec {key:'BUILD.md', status:'implemented'})",
		"CREATE (:Spec {key:'WEB.md', status:'draft'})",
	} {
		if err := runGraphClient([]string{"-r", roadmap, "--query", q}); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
}

// TestGraphExplain_PublishesThePlan is the headline regression: the defect was
// an EXPLAIN that reported nothing at all.
func TestGraphExplain_PublishesThePlan(t *testing.T) {
	const roadmap = "graph-plan-explain"
	defer servedRoadmap(t, roadmap)()
	seedPlanFixture(t, roadmap)

	out := graphPlanOutput(t, roadmap, "EXPLAIN MATCH (s:Spec) RETURN s.key")

	plan, ok := out["plan"].(map[string]any)
	if !ok {
		t.Fatalf("an EXPLAIN must publish a `plan` member; without it the output reads "+
			"as a query that matched nothing.\ngot: %v", out)
	}
	if operator, _ := plan["operator"].(string); operator == "" {
		t.Errorf("every plan node must carry a non-empty `operator` "+
			"(SPEC/DATA_FORMATS.md § Graph Plan Node rule 1).\ngot: %v", plan)
	}
	if _, present := out["profile"]; present {
		t.Errorf("an EXPLAIN must NOT publish a `profile` member: at most one of the two "+
			"appears, which is what stops an estimate being read as a measurement.\ngot: %v", out)
	}
}

// TestGraphExplainOfAWrite_DoesNotClaimSuccess is the third and worst defect.
// The statement declares no column, so the columns discriminator used to route
// it to {"ok": true} -- announcing a successful write for a statement that never
// ran (SPEC/GRAPH.md § Query Plans, rule 8).
func TestGraphExplainOfAWrite_DoesNotClaimSuccess(t *testing.T) {
	const roadmap = "graph-plan-explain-write"
	defer servedRoadmap(t, roadmap)()

	out := graphPlanOutput(t, roadmap, "EXPLAIN CREATE (n:ShouldNeverExist)")

	if _, claimed := out["ok"]; claimed {
		t.Errorf("an EXPLAIN of a writing statement must NOT publish {\"ok\": true}: it is "+
			"byte-identical to a real committed write, over a statement that wrote "+
			"nothing (SPEC/GRAPH.md § Query Plans, rule 8).\ngot: %v", out)
	}
	if _, ok := out["plan"].(map[string]any); !ok {
		t.Errorf("an EXPLAIN of a writing statement must publish its plan.\ngot: %v", out)
	}
	// The envelope must be the columns shape even with no column of its own, and
	// both members are arrays rather than null.
	if cols, ok := out["columns"].([]any); !ok || len(cols) != 0 {
		t.Errorf("a prefixed statement with no result column publishes an empty "+
			"`columns` array, never null and never an absent key.\ngot: %v", out["columns"])
	}
	if rows, ok := out["rows"].([]any); !ok || len(rows) != 0 {
		t.Errorf("a prefixed statement with no result column publishes an empty `rows` "+
			"array.\ngot: %v", out["rows"])
	}

	// The decisive assertion: the statement must not have run. An EXPLAIN that
	// executed its CREATE would be a far worse defect than the one being fixed.
	count := graphPlanOutput(t, roadmap, "MATCH (n:ShouldNeverExist) RETURN count(n)")
	rows, _ := count["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("count query returned no row: %v", count)
	}
	first, _ := rows[0].([]any)
	if len(first) != 1 || first[0].(float64) != 0 {
		t.Errorf("EXPLAIN must execute nothing, but the node exists: the prefix reports a "+
			"plan and never runs the statement.\ncount row: %v", first)
	}
}

// TestGraphProfile_PublishesTheMeasurement pins that PROFILE reports what the
// run cost, and that the measured figures are real rather than echoes of the
// row count.
func TestGraphProfile_PublishesTheMeasurement(t *testing.T) {
	const roadmap = "graph-plan-profile"
	defer servedRoadmap(t, roadmap)()
	seedPlanFixture(t, roadmap)

	out := graphPlanOutput(t, roadmap,
		"PROFILE MATCH (s:Spec) WHERE s.status = 'implemented' RETURN s.key")

	profile, ok := out["profile"].(map[string]any)
	if !ok {
		t.Fatalf("a PROFILE must publish a `profile` member.\ngot: %v", out)
	}
	if _, present := out["plan"]; present {
		t.Errorf("a PROFILE must NOT publish a `plan` member.\ngot: %v", out)
	}
	// The statement really ran, so its rows are there too.
	if rows, _ := out["rows"].([]any); len(rows) != 2 {
		t.Errorf("PROFILE runs the statement and returns its real rows; want 2, got %v", out["rows"])
	}

	// A measured figure must appear somewhere in the tree. Searching the whole
	// tree rather than the root keeps the test independent of the plan shape the
	// planner happens to choose, which is not this test's subject.
	if !treeHasKey(profile, "rows") || !treeHasKey(profile, "timeNs") {
		t.Errorf("a profile tree must carry the measured `rows` and `timeNs` "+
			"(SPEC/DATA_FORMATS.md § Graph Plan Node rule 7).\ngot: %v", profile)
	}
	// The filter rejected exactly one of the three seeded nodes. A figure equal
	// to the emitted row count would be an echo rather than a measurement, and
	// 1 against 2 emitted rows tells the two apart.
	if got, found := treeFind(profile, "rowsRemovedByFilter"); found {
		if n, _ := got.(float64); n != 1 {
			t.Errorf("the predicate rejected one of three seeded nodes, so "+
				"rowsRemovedByFilter must be 1, got %v", got)
		}
	}
}

// TestGraphUnprefixed_OutputIsUnchanged is the compatibility guard. The envelope
// departure of rule 8 is safe only because it is confined to prefixed
// statements, and an unprefixed statement is what every existing caller parses.
func TestGraphUnprefixed_OutputIsUnchanged(t *testing.T) {
	const roadmap = "graph-plan-unprefixed"
	defer servedRoadmap(t, roadmap)()
	seedPlanFixture(t, roadmap)

	read := graphPlanOutput(t, roadmap, "MATCH (s:Spec) RETURN s.key")
	for _, key := range []string{"plan", "profile"} {
		if _, present := read[key]; present {
			t.Errorf("an unprefixed read must publish no %q member; its output is "+
				"unchanged byte for byte (SPEC/GRAPH.md § Query Plans, rule 9).\ngot: %v", key, read)
		}
	}

	write := graphPlanOutput(t, roadmap, "CREATE (:Spec {key:'DEPLOY.md', status:'draft'})")
	if ok, _ := write["ok"].(bool); !ok {
		t.Errorf("an unprefixed write still publishes {\"ok\": true}: the discriminator is "+
			"departed from only where a plan has to be carried.\ngot: %v", write)
	}
	for _, key := range []string{"plan", "profile"} {
		if _, present := write[key]; present {
			t.Errorf("an unprefixed write must publish no %q member either "+
				"(SPEC/GRAPH.md § Query Plans, rule 9).\ngot: %v", key, write)
		}
	}
	// The envelope carries `ok` and the counters of the write it just committed,
	// and nothing else. The count is asserted rather than only the two lookups
	// above so that a THIRD member cannot enter the shape unnoticed; it was 1
	// before the counters were published, and the second key is the one member
	// SPEC/DATA_FORMATS.md § Graph Write Result adds to a statement that changed
	// something. That the counters themselves are right is graph_counters_test.go's
	// subject, not this file's.
	if _, counted := write["counters"]; !counted {
		t.Errorf("a write that created a node publishes its counters beside `ok` "+
			"(SPEC/DATA_FORMATS.md § Graph Write Result).\ngot: %v", write)
	}
	if len(write) != 2 {
		t.Errorf("an unprefixed write publishes exactly `ok` and `counters`.\ngot: %v", write)
	}
}

// treeHasKey reports whether key appears on any node of a decoded plan tree.
func treeHasKey(node map[string]any, key string) bool {
	_, found := treeFind(node, key)
	return found
}

// treeFind returns the first value for key found in a pre-order walk of a
// decoded plan tree.
func treeFind(node map[string]any, key string) (any, bool) {
	if v, ok := node[key]; ok {
		return v, true
	}
	children, _ := node["children"].([]any)
	for _, child := range children {
		m, ok := child.(map[string]any)
		if !ok {
			continue
		}
		if v, found := treeFind(m, key); found {
			return v, true
		}
	}
	return nil, false
}
